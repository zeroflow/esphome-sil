# Sim-bus protocol, version 1

The sim-bus connects the firmware (ESPHome `host:` build, component `sim_bus`) to the simulator (`sil`, Go). This file is the contract. Two codecs implement it and one file pins it:

| Piece | Where |
|-------|-------|
| Go codec | `internal/bus/codec.go` |
| C++ codec (header-only, no ESPHome includes) | `components/sim_bus/sim_protocol.h` |
| Golden vectors, read by both tests | `protocol/vectors/frames.tsv` |
| Go test / C++ test | `internal/bus/codec_test.go` / `tests/cpp/test_protocol.cpp` |

The protocol is internal while the project is `0.x`: it may change, but only together with both codecs and the vectors. The session part (hello, chan, snapshot, sync) is implemented by plans 01-07 (Go server, engine) and 01-10 (firmware hub); this file is what they implement.

## Transport

- A Unix stream socket at `<run dir>/bus.sock` (mode 0600 in a 0700 directory). The path comes from `SIL_BUS` (firmware) and the instance run directory (simulator). A socket path is at most 107 bytes (`sun_path`).
- The simulator listens, the firmware connects. The firmware restarts and must not own the rendezvous; it retries with backoff while the socket is absent.
- A new connection while one is active replaces the older one (the older is closed). That is what a firmware restart looks like from the simulator.

## Line grammar

One message is one line: a flat JSON object terminated by `\n`.

- A line is at most **4096** bytes including the newline (4095 bytes of content). The length is checked before anything is parsed.
- Between tokens only the ASCII space (0x20) may appear; tabs, carriage returns and every other byte are errors. Spaces before the opening brace and after the closing brace are allowed.
- Keys match `^[a-z][a-z0-9_]*$` and are unique within the line.
- Values are **strings** or **numbers**. There are no booleans (send 0 or 1), no `null`, no arrays and no nested objects.
- Strings contain printable ASCII (0x20 to 0x7E). The only escapes are `\"` and `\\`. Every other backslash sequence (`\n`, `é`, `\/`) is an error, and so is any control byte, DEL (0x7F) or byte >= 0x80 (no UTF-8).
- Numbers follow the JSON grammar `-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?`: no leading `+`, no leading zeros, no `1.` or `.5`, at most 32 characters, and the value must be inside the float32 range (`1e39` is an error).
- `k` (the message kind) is required, is a non-empty string, and may appear at any position. Encoders emit `k` first and no spaces at all.
- A decoder never recurses: a `{` or `[` where a value is expected is rejected as `nested` without descending.

### Error classes

Both decoders return the first problem found while scanning left to right, named by one of these classes (they are the `invalid` rows of the vectors, and `DecodeError.Class` / `proto::err_name()` use the same lowercase words):

| Class | Meaning |
|-------|---------|
| `empty` | zero-length line |
| `too_long` | more than 4095 bytes of content |
| `not_object` | no leading `{`, or a structural token (`:` `,` `}`, closing quote) is missing or the line ends early |
| `nested` | an object or array as a value |
| `bad_key` | a key that is not `[a-z][a-z0-9_]*`, is empty, is unquoted, or a trailing comma |
| `bad_value` | a value that starts with anything other than a quote, a brace/bracket or a number character (`true`, `null`, `NaN`) |
| `bad_escape` | a backslash followed by anything but `"` or `\`, or at the end of the line |
| `bad_char` | a control character, DEL or a byte >= 0x80 inside a string |
| `dup_key` | the same key twice |
| `missing_k` | no `k` key (including `{}`) |
| `bad_k` | `k` is not a string, or is the empty string |
| `bad_number` | a number that breaks the grammar, is longer than 32 characters or is out of float32 range |
| `trailing` | anything but spaces after the closing brace |

Checks run in this order: length, structure and values while scanning, the end of the line (`trailing`), then `missing_k` / `bad_k`.

**A decode error closes the connection.** A side that receives a bad line may first send `err` (below) and then closes; it does not skip the line and read on. (Plan 01-10 implements this for the firmware hub; the Go server counts and logs it.)

## Numbers

- Channel values (`v`) are **float32** on both sides. Go reads the wire value with `strconv.ParseFloat(raw, 32)` (`Frame.GetF32`), C++ with `strtof`. The two roundings agree.
- `Frame.GetNum` in Go re-expresses that float32 as its shortest decimal at float64 (`0.4` reads as exactly `0.4`, not `0.4000000059604645`), so exact comparisons against the commanded decimal hold downstream.
- Integers (`t`, `proto`, `chans`) are int64 on both sides and written without fraction or exponent.
- Encoders write floats with the shortest round-trip form: Go `strconv.FormatFloat(float64(float32(v)), 'g', -1, 32)`, C++ `std::to_chars(float)`. The notation can differ between the two for some magnitudes (`1.2345679e+08` against `123456792`, `1e-04` against `0.0001`) while the digits agree. For that reason the vectors compare **decoded values**, and only a restricted set (`0`, `0.4`, `-0.5`, `1234.5`, `1e+06`, `65536`, `1e-05`, int64 timestamps) is pinned byte for byte.
- An encoder never emits NaN or Inf: a non-finite value (or, in Go, a float64 outside the float32 range) leaves the field out of the frame.
- An encoder writes any byte of a string outside 0x20 to 0x7E as `?`, so it cannot produce a line the peer rejects.

## Channels

A channel is a named float32 signal. Names match `^[a-z0-9_]+(\.[a-z0-9_]+)*$` (checked by the ESPHome component schema, so a name on the wire never needs more than the two string escapes). Each sim platform in the firmware YAML binds to a channel with a required `channel:` key (IO-06).

Phase 1 channels:

| Channel | Direction | Meaning |
|---------|-----------|---------|
| `portN.duty` (N = 1..4) | `out` (firmware to simulator) | PWM duty of fan port N, 0..1 |
| `portN.rpm` (N = 1..4) | `in` (simulator to firmware) | tach speed of fan port N in RPM |

## Messages

| From | `k` | Fields |
|------|-----|--------|
| firmware | `hello` | `proto` (int, 1), `name` (string), `chans` (int N), `t` (int, firmware monotonic microseconds, diagnostics only) |
| firmware | `chan` (N times) | `ch`, `dir` (`out` = firmware to simulator, `in` = simulator to firmware), `type` (`f32`) |
| firmware | `out` (M times) then `sync` | `ch`, `v`, `t`: the last value of every out channel |
| simulator | `welcome` | `proto` (int, 1), `t` (int, simulator microseconds of sim time) |
| simulator | `in` (K times) then `sync` | `ch`, `v`, `t`: every in channel with a known value |
| both | `out` / `in` | steady state, `ch`, `v`, `t` |
| both | `sync` | `t`; ends a snapshot |
| either | `err` | `msg` (string); the sender closes right after |
| either | `bye` | no fields; orderly close |

Session:

1. The firmware connects and sends `hello`, then `chan` for each of its channels, then an `out` snapshot of the last known value of every out channel, then `sync`.
2. The simulator checks `proto`, answers `welcome`, then sends an `in` snapshot of every in channel that has a value, then `sync`.
3. Steady state: `out` frames from the firmware and `in` frames from the simulator, each carrying `t`.
4. A `proto` mismatch is answered with `err` and a close; status shows `bus.error = "proto_mismatch"`.
5. A manifest that does not match the model's ports (unknown or missing channels) produces warnings in status, not a failure.

Timestamps: the `t` of simulator frames is microseconds of **sim time** (the injectable clock, not the wall clock). The `t` of firmware frames is its monotonic microseconds and is recorded for diagnostics only.

Dedupe: `out` and `in` are state frames. An identical consecutive value on a channel is not sent again, except inside a snapshot (`hello` replay, `welcome` replay), which always sends every channel so that either side restarting converges.

## Limits and robustness

- 4096 bytes per line, 32 characters per number, no recursion: a hostile local peer cannot make a decoder allocate or loop beyond the line it was handed.
- The socket lives in a 0700 run directory; only the user's own processes can connect. The protocol has no authentication beyond that.

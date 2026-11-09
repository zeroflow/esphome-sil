# Sim-bus protocol, version 1

The sim-bus connects the firmware (ESPHome `host:` build, component `sim_bus`) to the simulator (`sil`, Go). This file is the contract. Two codecs implement it and one file pins it:

| Piece | Where |
|-------|-------|
| Go codec | `internal/bus/codec.go` |
| C++ codec (header-only, no ESPHome includes) | `components/sim_bus/sim_protocol.h` |
| Golden vectors, read by both tests | `protocol/vectors/frames.tsv` |
| Go test / C++ test | `internal/bus/codec_test.go` / `tests/cpp/test_protocol.cpp` |

The protocol is internal while the project is `0.x`: it may change, but only together with both codecs and the vectors. The session part (hello, chan, snapshot, sync) is implemented by plans 01-07 (Go server, engine) and 01-10 (firmware hub); this file is what they implement. Phase 2 adds channels and the `rgb` type on top of version 1; the additions are backward compatible, so the version stays 1.

## Transport

- A Unix stream socket at `<run dir>/bus.sock` (mode 0600 in a 0700 directory). The path comes from `SIL_BUS` (firmware) and the instance run directory (simulator). A socket path is at most 107 bytes (`sun_path`).
- The simulator listens, the firmware connects. The firmware restarts and must not own the rendezvous; it retries with backoff while the socket is absent.
- A connection becomes the active session when its first frame is a valid `hello`; it then replaces the older session, which receives `bye` and is closed. That is what a firmware restart looks like from the simulator. A connection that never says hello (silent for 2 s, garbage, a debugging `socat`) is closed and does not disturb the active session.

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

**A decode error closes the connection.** A side that receives a bad line first sends `err` (below) and then closes; it does not skip the line and read on. Both the firmware hub and the Go server do this.

## Numbers

- Channel values (`v`) are **float32** on both sides. Go reads the wire value with `strconv.ParseFloat(raw, 32)` (`Frame.GetF32`), C++ with `strtof`. The two roundings agree.
- `Frame.GetNum` in Go re-expresses that float32 as its shortest decimal at float64 (`0.4` reads as exactly `0.4`, not `0.4000000059604645`), so exact comparisons against the commanded decimal hold downstream.
- Integers (`t`, `proto`, `chans`) are int64 on both sides and written without fraction or exponent.
- Encoders write floats with the shortest round-trip form: Go `strconv.FormatFloat(float64(float32(v)), 'g', -1, 32)`, C++ `std::to_chars(float)`. The notation can differ between the two for some magnitudes (`1.2345679e+08` against `123456792`, `1e-04` against `0.0001`) while the digits agree. For that reason the vectors compare **decoded values**, and only a restricted set (`0`, `0.4`, `-0.5`, `1234.5`, `1e+06`, `65536`, `1e-05`, int64 timestamps) is pinned byte for byte.
- An encoder never emits NaN or Inf: a non-finite value (or, in Go, a float64 outside the float32 range) leaves the field out of the frame.
- An encoder writes any byte of a string outside 0x20 to 0x7E as `?`, so it cannot produce a line the peer rejects.

## Channels

A channel is a named signal. Most are float32 (`type` `f32`, value in `v`); the LED strip is the one `rgb` channel (pixels in `px`). Names match `^[a-z0-9_]+(\.[a-z0-9_]+)*$` (checked by the ESPHome component schema, so a name on the wire never needs more than the two string escapes). Each sim platform in the firmware YAML binds to a channel with a required `channel:` key (IO-06).

Phase 1 channels (the ports; **required**):

| Channel | Direction | Meaning |
|---------|-----------|---------|
| `portN.duty` (N = 1..4) | `out` (firmware to simulator) | PWM duty of fan port N, 0..1 |
| `portN.rpm` (N = 1..4) | `in` (simulator to firmware) | tach speed of fan port N in RPM |

Phase 2 channels (the rest of the Rev 3.3 IO surface; **optional**):

| Channel | Direction | Type | Meaning |
|---------|-----------|------|---------|
| `usr1`, `usr2`, `usr3` | `in` | `f32` | button level, `0` released and `1` pressed. The simulator sends levels only; the firmware measures hold time itself |
| `temperature` | `in` | `f32` | ambient temperature in degrees Celsius (default 22) |
| `humidity` | `in` | `f32` | relative humidity in percent (default 45) |
| `leds` | `out` | `rgb` | the addressable status strip: `n` LEDs, pixels in `px` instead of `v` |

Required and optional channels: a firmware must declare the ports (`portN.duty` out, `portN.rpm` in) or the simulator warns `missing channel ...`. `usr1`..`usr3`, `temperature`, `humidity` and `leds` are optional: leaving one out never warns, it only means the matching operation (`sil press`, `sil set`, the LED strip in `sil status`) is unavailable for that firmware. A declared channel with the wrong direction or type, and any channel the simulator has no model for, is one warning in status (`channel X has direction ...`, `channel X has type ...`, `unknown channel X (ignored)`), never a failure. The hub on the firmware side likewise ignores an `in` frame for a channel it does not have.

## Messages

| From | `k` | Fields |
|------|-----|--------|
| firmware | `hello` | `proto` (int, 1), `name` (string), `chans` (int N), `t` (int, firmware monotonic microseconds, diagnostics only) |
| firmware | `chan` (N times) | `ch`, `dir` (`out` = firmware to simulator, `in` = simulator to firmware), `type` (`f32` or `rgb`). An `rgb` chan also carries `n` (int, pixel count 1..255) and `seg` (string, `label:from:to,...`); no other type carries them |
| firmware | `out` (M times) then `sync` | `ch`, `v`, `t`: the last value of every f32 out channel, then one `px` frame for `leds` |
| simulator | `welcome` | `proto` (int, 1), `t` (int, simulator microseconds of sim time) |
| simulator | `in` (K times) then `sync` | `ch`, `v`, `t`: every in channel with a known value |
| both | `out` / `in` | steady state, `ch`, `v`, `t`. An `out` frame on an `rgb` channel carries `px` (string) instead of `v` |
| both | `sync` | `t`; ends a snapshot |
| either | `err` | `msg` (string); the sender closes right after |
| either | `bye` | no fields; orderly close |

Session:

1. The firmware connects and sends `hello`, then `chan` for each of its channels, then an `out` snapshot of the last known value of every out channel, then `sync`.
2. The simulator checks `proto`, answers `welcome`, then sends an `in` snapshot of every in channel that has a value, then `sync`.
3. Steady state: `out` frames from the firmware and `in` frames from the simulator, each carrying `t`.
4. A `proto` mismatch is answered with `err` (`proto mismatch: sim 1, firmware N`) and a close; status shows a warning and `bus_connected` false, and the firmware logs the error and retries with backoff.
5. A manifest that does not match the model's ports (unknown or missing channels, wrong direction or type) produces warnings in status, not a failure. A firmware with zero channels (`chans` 0) still completes hello, welcome and sync.

Order guarantees:

- Firmware snapshot: `hello`, `chan` in registration order, the f32 `out` frames in first-write order, the `px` out frame of `leds` (when the firmware has a strip), `sync`. A connection made before any output was written (the very first one, in `setup()`) carries no `out` frames in the snapshot; the first values follow as ordinary `out` frames.
- Simulator snapshot: `welcome` first, then one `in` per port in port order 1..4 (`portN.rpm`), then temperature, humidity, usr1, usr2, usr3, then `sync`. The simulator sends an `in` frame for every input it knows, also when the firmware's manifest lacks the channel (the hub ignores channels it does not have); a button is sent at its current level, so a freshly connected firmware always sees released buttons as `0`. Any steady-state frame the simulator produces while it prepares the snapshot is held back and follows the `sync`.
- The simulator answers a hello within the 2 s hello deadline (the firmware's first frame must arrive within 2 s of connecting) and accepts at most 1024 `chan` frames per session.
- The firmware accepts `in` frames at any time after connecting; `welcome` only resets its reconnect backoff.

Reconnect (firmware): after a connection ends or fails it retries after 100 ms, doubling per attempt that did not reach a `welcome` (100, 200, 400, 800, 1600, then 2000 ms), and a `welcome` resets the delay. The hub drops the connection when more than 65536 bytes are queued for a simulator that does not read.

Session end reasons reported to status (`warnings`) by the simulator: `proto_mismatch`, `hello_timeout`, `no_hello`, `bad_hello`, `bad_frame: <class>`, `peer_error: <msg>`, `send_queue_full`; a plain close, `bye` and a replacement by a newer firmware are not warnings.

Timestamps: the `t` of simulator frames is microseconds of **sim time** (the injectable clock, not the wall clock). The `t` of firmware frames is its monotonic microseconds and is recorded for diagnostics only.

Environment re-send: `temperature` and `humidity` are re-sent every 10 s of sim time, each from its own deadline (a changed value is sent at once and re-arms only its own timer), so a firmware that consumes them as a sliding window or a watchdog keeps seeing fresh samples. Port rpm and button levels are sent on change only (plus the snapshot).

LED frames (`leds`, type `rgb`):

- `px` is lowercase hex, 6 characters per pixel in RGB byte order, so 6 x `n` characters for a full strip (`0000ff000000` is pixel 0 blue and pixel 1 off). The simulator stores the bytes exactly as sent and converts nothing.
- `n` is at most 255 so that a full `px` line (1530 characters) stays far below the 4096-byte line cap. `seg` names the sub-ranges of the strip; each entry is `label:from:to` with `0 <= from <= to < n` (both inclusive), entries are comma separated, and labels match `^[a-z][a-z0-9_]*$` and are unique. The firmware derives `seg` at codegen time from the stock `partition` lights that sit on the sim light, so the YAML stays the single source of truth; a config that breaks these rules does not pass `esphome config`. A light that no partition uses sends an empty `seg`.
- Dedupe and rate: the firmware sends a `px` frame only when the pixel content changed, and at least 50 ms apart by its own clock; a change inside the gap stays pending and is flushed from `loop()`.
- Replay: after the f32 `out` snapshot and before `sync`, the firmware sends one `px` frame whose pixels are the strip state at reconnect. The hub stores the pixels also while the bus is down, so changes made during the gap are included; the frame equals the previous connection's last `px` only if the strip did not change in between. A new session starts with the simulator's LED state empty; a disconnect keeps the last frame.

Dedupe: `out` and `in` are state frames. An identical consecutive value on a channel is not sent again, except inside a snapshot (`hello` replay, `welcome` replay), which always sends every channel so that either side restarting converges.

## Limits and robustness

- 4096 bytes per line, 32 characters per number, no recursion: a hostile local peer cannot make a decoder allocate or loop beyond the line it was handed.
- The socket lives in a 0700 run directory; only the user's own processes can connect. The protocol has no authentication beyond that.

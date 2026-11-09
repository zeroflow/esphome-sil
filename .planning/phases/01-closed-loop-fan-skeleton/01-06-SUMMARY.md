---
phase: 01-closed-loop-fan-skeleton
plan: 06
subsystem: sim-bus-protocol
tags: [protocol, ndjson, golden-vectors, go, cpp, header-only, float32, makefile]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 02)
    provides: "lenient internal/bus codec API (Frame, NewFrame, Encode, Decode, GetNum/GetF32/GetInt), engine and server that call it"
  - phase: 01-closed-loop-fan-skeleton (plan 04)
    provides: "exact duty assertions in engine and e2e tests that rely on the GetNum decimal normalization"
provides:
  - "protocol/PROTOCOL.md: sim-bus wire grammar, 13 error classes, float32 rules, channel naming, session messages (hello/chan/out/sync/welcome/in/err/bye)"
  - "protocol/vectors/frames.tsv: 108 golden rows (valid, invalid, encode) with %HH byte escapes, shared by Go and C++"
  - "internal/bus: strict single-pass Decode returning *DecodeError{Class,Offset}, string-sanitizing Encode, non-finite refusal"
  - "components/sim_bus/sim_protocol.h: header-only esphome::sim_bus::proto codec (Frame, Err, err_name, parse, encode), standard library only"
  - "tests/cpp/test_protocol.cpp and Makefile targets test-cpp (in check) and test-cpp-image (pinned image gcc 14)"
affects: [01-07, 01-08, 01-10, 01-13]

actuals:
  tokens: 19400
  tasks: 2
  commits: 4
plan_head_before: ce68b2bf9726f4143972af9478af5e1dde6d6eed
plan_head_after: 0b66d8144592162412ecc7e0748171afc9305bf0

tech-stack:
  added: []
  patterns:
    - "One TSV of golden vectors read by two languages; valid rows compare decoded values (float32), encode rows pin bytes only on a restricted set"
    - "%HH byte escapes in the vector line column so a TSV can carry TAB, NUL and UTF-8 bytes"
    - "Decoder error precedence is part of the contract: length, scan order, trailing, then missing_k/bad_k"
    - "Header-only codec in the component directory, test outside it (ESPHome compiles every .cpp under components/)"

key-files:
  created:
    - protocol/PROTOCOL.md
    - protocol/vectors/frames.tsv
    - internal/bus/codec_test.go
    - components/sim_bus/sim_protocol.h
    - tests/cpp/test_protocol.cpp
  modified:
    - internal/bus/codec.go
    - Makefile

key-decisions:
  - "Line cap: 4096 bytes including the newline, so Decode/parse reject content longer than 4095 as too_long (boundary pinned by max_len_4095 valid and too_long_4096 invalid rows)"
  - "Structural breakage (missing opening brace, colon, comma, closing brace or closing quote, line ending early) is not_object; a trailing comma is bad_key; only spaces count as whitespace (tab or CR between tokens is bad_key, CR after the object is trailing)"
  - "Numbers are tokenized as the maximal run of [-+.0-9eE], then validated against the JSON grammar, the 32-character cap and float32 range (1e39 is bad_number; underflow such as 1e-50 is accepted and reads as 0)"
  - "Both encoders write any string byte outside 0x20-0x7E as '?' so an encoder can never produce a line its peer rejects; Encode still does not truncate to MaxLine"
  - "test-cpp-image is not a prerequisite of check (needs docker and the image); it is declared next to check in the Makefile with a comment"
  - "Vector-file %HH escape (literal percent is %25) applies to the line column only; tuple columns are literal"

patterns-established:
  - "Adding a protocol rule means: PROTOCOL.md sentence, vector row(s), then both codecs; the two tests fail on drift"
  - "C++ parse() leaves the output frame empty on any error"

requirements-completed: [BUS-01, BUS-04, IO-06]

coverage:
  - id: D1
    description: "protocol/PROTOCOL.md specifies the transport, line grammar, error classes, float32 and integer rules, channel naming for the component channel key, and the session messages"
    requirement: "BUS-01"
    verification:
      - kind: other
        ref: "grep -n '4096' protocol/PROTOCOL.md; grep -n 'portN.duty' protocol/PROTOCOL.md"
        status: pass
    human_judgment: true
    rationale: "A specification document: tests pin the grammar but whether the prose is complete and clear for 01-07 and 01-10 is a reader's judgment"
  - id: D2
    description: "Strict Go codec decodes and encodes all 108 golden-vector rows identically, with the 01-02 API and GetNum decimal normalization preserved"
    requirement: "BUS-04"
    verification:
      - kind: unit
        ref: "make test-pkg PKG=./internal/bus/ (TestVectors, TestRoundTrip, TestGetNumNormalization, TestNonFiniteIsRefused, TestDecodeNeverPanics)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Header-only C++ codec passes the same vectors on host g++ 12 and the pinned image gcc 14 with -std=c++20 -Wall -Wextra -Werror"
    requirement: "BUS-04"
    verification:
      - kind: unit
        ref: "make test-cpp (112 passed, 0 failed); make test-cpp-image (112 passed, 0 failed)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Channel naming rule and float32-on-both-sides precision (no NaN/Inf, value-level comparison) are specified for the component schema to implement in 01-07"
    requirement: "IO-06"
    verification:
      - kind: unit
        ref: "frames.tsv rows num_*, enc_*; codec_test.go TestNonFiniteIsRefused; test_protocol.cpp num_non_finite"
        status: pass
    human_judgment: true
    rationale: "The channel: key itself is implemented by 01-07; this plan only specifies the rule, so IO-06 is complete only once the schema enforces it"
  - id: D5
    description: "Example firmware still builds and runs the tracer and loop e2e with the header present in the component directory and the strict Go server decoder"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTracer; make e2e E2E_RUN='TestLoop|TestFirmwareBus|TestModel'"
        status: pass
    human_judgment: false

duration: 8min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 6: Sim-bus protocol spec, golden vectors and strict codecs Summary

**Frozen sim-bus wire grammar (4096-byte flat NDJSON, 13 error classes, float32 values) pinned by one 108-row TSV that a strict Go codec and a header-only C++ codec both pass on host g++ 12 and image gcc 14.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-11-09T01:21:53Z
- **Completed:** 2026-11-09T01:30Z
- **Tasks:** 2 (each as RED then GREEN)
- **Files modified:** 7 (5 created, 2 modified)

## Accomplishments
- `protocol/PROTOCOL.md` fixes the transport (simulator listens, firmware connects, newest connection wins), the line grammar, error precedence, number rules, channel names and the session message table that 01-07 and 01-10 implement.
- `protocol/vectors/frames.tsv` holds 108 rows: every message kind, escapes, the float formatting traps (`1e-05`, `1.2345679e+08`, `123456792`, `0.0001`), a 4095-byte valid line against 4096 and 4097-byte invalid ones, all thirteen error classes, and 11 byte-for-byte encode rows.
- `internal/bus/codec.go` is now a single-pass strict decoder returning `*DecodeError{Class, Offset}`; the public API, `GetNum` normalization (0.4, 0.08, 0.2, 0.6, 0.5, 1 decode to exactly those float64 values) and `server.go` are unchanged.
- `components/sim_bus/sim_protocol.h` (namespace `esphome::sim_bus::proto`, no ESPHome includes) mirrors it; `tests/cpp/test_protocol.cpp` runs every row plus round trip, non-finite refusal, sanitizing, strict `get_int` and a prefix/deletion no-crash sweep (112 checks).
- `make test-cpp` is part of `make check`; `make test-cpp-image` runs the same test with the pinned image's gcc 14.

## Task Commits

1. **Task 1: PROTOCOL.md, golden vectors and strict Go codec (TDD)**
   - RED `4739db0` (test): frames.tsv + codec_test.go; fails to compile because `DecodeError` does not exist yet
   - GREEN `906d1ad` (feat): codec.go + PROTOCOL.md (also corrects the unicode-escape vector, see Issues)
2. **Task 2: Header-only C++ codec (TDD)**
   - RED `d4080c6` (test): tests/cpp/test_protocol.cpp; fails because `sim_protocol.h` does not exist yet
   - GREEN `0b66d81` (feat): sim_protocol.h + Makefile targets

**Plan metadata:** committed with this SUMMARY (docs: complete plan)

## Files Created/Modified
- `protocol/PROTOCOL.md` - wire grammar and session spec
- `protocol/vectors/frames.tsv` - golden vectors (valid, invalid, encode)
- `internal/bus/codec.go` - strict decoder, `DecodeError`, sanitizing encoder
- `internal/bus/codec_test.go` - TSV-driven subtests (one per row), round trip, normalization, non-finite, never-panic
- `components/sim_bus/sim_protocol.h` - header-only C++ codec
- `tests/cpp/test_protocol.cpp` - C++ golden-vector test
- `Makefile` - `test-cpp`, `test-cpp-image`, `check` now includes `test-cpp`

## Decisions Made
See `key-decisions` above. The ones later plans must know: the 4095-byte content cap; `?` substitution for non-printable string bytes in both encoders; a decode error is specified to close the connection but is not yet enforced (server.go still skips a bad line, as in 01-02).

## Deviations from Plan

None - plan executed exactly as written.

Notes within plan scope: `Frame.GetInt` now also rejects a leading `+` (the plan asked for `-?[0-9]+`); the plan's line "server.go not in the diff" holds.

## Issues Encountered
- The first frames.tsv had a literal `é` in the `bad_escape_unicode` row (the generator input was converted when it was written), so the Go test correctly reported `bad_char` instead of `bad_escape`. Regenerated with an explicit backslash-u sequence before the GREEN commit; no other row was affected.
- `go test` RED for task 1 is a compile failure (`undefined: DecodeError`), not a behavioural failure; the lenient 01-02 decoder accepted several invalid rows, which the GREEN run then confirmed fixed.

## Needs Human
- `EDGE-BUS-04-unclassified` (flagged in the plan frontmatter, status unresolved): the edge probe could not classify BUS-04. Coverage added here: shared TSV with valid/invalid/encode rows, value-level float32 comparison, host and image compilers. A manual edge review of PROTOCOL.md error precedence (for example, tab between tokens is `bad_key`, a trailing CR is `trailing`) is still open. Nothing blocks later plans.

## Known Stubs
None.

## Threat Flags
None. T-01-09 (DoS: line cap before parsing, single pass, 32-character numbers) and T-01-10 (parser differential: one TSV, two compilers) are mitigated as planned; the "decode error closes the connection" half of T-01-09 is enforced by 01-10 and the 01-07 server.

## Next Phase Readiness
- 01-07 can implement the Go session (hello/welcome/snapshot/sync, error frame then close) on `bus.Decode`/`DecodeError`; `server.go` needs to stop skipping bad lines there.
- 01-10 can switch the firmware hub from the prototype `get_str`/`get_num` in `sim_bus.cpp` to `sim_protocol.h`; the header is already in the component directory and builds with the firmware (TestTracer, TestLoop, TestFirmwareBus, TestModelThreshold pass).

## Self-Check: PASSED

- Files: protocol/PROTOCOL.md, protocol/vectors/frames.tsv, internal/bus/codec.go, internal/bus/codec_test.go, components/sim_bus/sim_protocol.h, tests/cpp/test_protocol.cpp, Makefile all exist.
- Commits 4739db0, 906d1ad, d4080c6, 0b66d81 exist on claude/main.
- `make check` (includes test-cpp), `make test-cpp-image`, `make e2e E2E_RUN=TestTracer` pass; TestVectors lists 108 PASS subtests for 108 non-comment rows.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

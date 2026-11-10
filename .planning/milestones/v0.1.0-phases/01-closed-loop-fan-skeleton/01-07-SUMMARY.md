---
phase: 01-closed-loop-fan-skeleton
plan: 07
subsystem: firmware-io
tags: [esphome, sim-bus, codegen-validation, polling-component, windowed-mean, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 01)
    provides: "components/sim_bus hub, output and sensor platforms, e2e harness"
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "sil up/status, Env helpers used by TestRPMMean"
provides:
  - "Config-time errors for a missing, malformed or duplicate channel and for an empty or over-long hub path"
  - "Platform split: sim_output.* behind USE_OUTPUT, sim_sensor.* behind USE_SENSOR, platform-free hub header"
  - "SimBus::set_out with per-channel dedupe, non-finite filter and once-per-connect snapshot replay"
  - "SimSensor as a PollingComponent publishing a 1 s time-weighted mean, silent and reset while the bus is down"
  - "Hub drops a connection on a line over 4096 bytes"
affects: [01-08, 01-10, 01-11, 01-12, 01-13]

actuals:
  tokens: 7900
  tasks: 2
  commits: 2
plan_head_before: e1b62c4d7086d7231a6dea4ecb72b387c72d38e2
plan_head_after: 536f00e8a13a0cac2827d512fab976358382e590

tech-stack:
  added: []
  patterns:
    - "Hub FINAL_VALIDATE_SCHEMA walks fv.full_config for platform: sim_bus entries; platforms need no extra wiring"
    - "Platform code lives in its own header/cpp pair guarded by USE_<DOMAIN>; the hub header includes no platform header"
    - "Hub keeps last_out_ (value per channel in first-write order); while connected it equals what the peer last received, so dedupe is a comparison and replay is a loop"
    - "Inline YAML fixtures written by the test into a short temp dir, validated or compiled in the pinned image with --network none"

key-files:
  created:
    - components/sim_bus/sim_output.h
    - components/sim_bus/sim_output.cpp
    - components/sim_bus/sim_sensor.h
    - components/sim_bus/sim_sensor.cpp
    - e2e/codegen_test.go
    - e2e/io_test.go
  modified:
    - components/sim_bus/__init__.py
    - components/sim_bus/sensor.py
    - components/sim_bus/sim_bus.h
    - components/sim_bus/sim_bus.cpp

key-decisions:
  - "RPM sensor is a PollingComponent (recorded exception to the push-only rule); update_interval defaults to 1s"
  - "Hub path keeps its /tmp/sil.sock default (plan: 'default stays a short /tmp path'); SIL_BUS always overrides it at runtime and sil/e2e always set it"
  - "A complete line over 4096 bytes drops the connection too, not only an unfinished one, matching proto::MAX_LINE (4096 including the newline)"
  - "No force_update on the RPM sensor: ESPHome 2026.10.1 publishes an unchanged mean every update_interval anyway (observed), so a steady 500 RPM still reports once per second"

requirements-completed: [IO-01, IO-02, IO-06, BUS-03]

coverage:
  - id: D1
    description: "Missing, malformed (uppercase, empty, leading/trailing dot, double dot) or duplicate channel, and empty or over-long (UTF-8 bytes) hub path fail esphome config with the documented messages; a one-character channel and examples/fan-skeleton.yaml pass; the repo tree is not written"
    requirement: "IO-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestCodegenValidation"
        status: pass
    human_judgment: false
  - id: D2
    description: "Socket path validation counts UTF-8 bytes (60 x e-acute reports 125 bytes) and rejects over 107 at config time"
    requirement: "BUS-03"
    verification:
      - kind: e2e
        ref: "e2e/codegen_test.go#TestCodegenValidation/long-path"
        status: pass
    human_judgment: false
  - id: D3
    description: "A config with only sim_bus outputs and a config with only sim_bus sensors both compile in the pinned image and connect; an empty SIL_BUS falls back to the YAML path"
    requirement: "BUS-03"
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestGuardsCompile"
        status: pass
    human_judgment: false
  - id: D4
    description: "Two identical Fan 1 speed 40 commands put exactly one port1.duty 0.4 frame on the bus; after a reconnect the snapshot carries port1.duty 0.4 exactly once and the other ports once"
    requirement: "IO-01"
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestFakePeer"
        status: pass
    human_judgment: false
  - id: D5
    description: "Fan 1 Speed is the 1 s time-weighted mean: 4 to 8 updates in 6 s with non-decreasing values during real spin-up, and ramp means sit mid-window against a 20 Hz peer"
    requirement: "IO-02"
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestRPMMean and #TestFakePeer"
        status: pass
    human_judgment: false
  - id: D6
    description: "Without a peer the RPM sensor does not publish; after reconnect it publishes only means of new values"
    requirement: "IO-02"
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestFakePeer"
        status: pass
    human_judgment: true
    rationale: "Disconnect semantics are assumption A4 (Home Assistant keeps the last value); whether that is the right tach-loss behaviour needs a live Home Assistant in Phase 2"
  - id: D7
    description: "Hub drops the connection on more than 4096 bytes without a newline, and on a complete line that long, then reconnects"
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestFakePeer (connections 3 and 4)"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 07: sim_bus IO hardening Summary

**sim_bus now rejects bad `channel:` and socket paths at config time, compiles with any subset of output/sensor platforms, sends each duty change once with snapshot replay, and reports fan RPM as a 1 s time-weighted mean that goes quiet when the simulator is gone.**

## Performance

- **Duration:** about 15 min
- **Completed:** 2026-11-09
- **Tasks:** 2
- **Files:** 6 created, 4 modified
- **Test wall times** (pinned image, warm cache): TestCodegenValidation 8.7 s, TestGuardsCompile 7.8 s, TestRPMMean 10 s, TestFakePeer 22 s

## Accomplishments

- `validate_path` (non-empty, at most 107 UTF-8 bytes), `validate_channel` (`^[a-z0-9_]+(\.[a-z0-9_]+)*$`, still `cv.Required`) and a hub-level `FINAL_VALIDATE_SCHEMA` that rejects duplicate channels across output and sensor platforms, all proven against the pinned image with inline fixtures and no repo writes.
- Platform split: `sim_bus.h` is platform-free; `SimOutput` and `SimSensor` sit in their own files behind `USE_OUTPUT` / `USE_SENSOR`. Output-only and sensor-only configs compile and run.
- `SimBus::set_out` ignores non-finite values, sends on change only, and `last_out_` is replayed once per connection (hello, then snapshot, then `on_connected`).
- `SimSensor` is a `PollingComponent` (default 1 s) publishing the zero-order-hold mean of the received values; no publish while disconnected or without a value; `on_connected` and `on_disconnected` reset the window.
- Hub read path is bounded (64 reads per loop) and drops the connection on an over-long line (T-01-11).

## Task Commits

1. **Task 1: Config-time validation of channel and socket path** - `e774614` (feat)
2. **Task 2: Platform split, output dedupe, 1 s RPM mean, disconnect semantics** - `536f00e` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `components/sim_bus/__init__.py` - validate_path, validate_channel, check_unique_channels, FINAL_VALIDATE_SCHEMA
- `components/sim_bus/sensor.py` - PollingComponent class, `cv.polling_component_schema("1s")`
- `components/sim_bus/sim_bus.h`, `sim_bus.cpp` - SimChild direction/on_disconnected, SimBus connected/set_out/children/last_out_/kMaxLine
- `components/sim_bus/sim_output.{h,cpp}` - SimOutput behind USE_OUTPUT
- `components/sim_bus/sim_sensor.{h,cpp}` - SimSensor behind USE_SENSOR
- `e2e/codegen_test.go` - TestCodegenValidation (five failing fixtures plus boundaries and the example)
- `e2e/io_test.go` - TestGuardsCompile, TestRPMMean, TestFakePeer, local `ioPeer` that records frames with their connection number

## Decisions Made

See key-decisions. Notable measured fact: with no `force_update`, a constant mean (peer sending 500 steady) still reaches the API every second (`[0.004 500] [0.8 500] [1.8 500] [2.8 500]`), so IO-02's "once per second" holds for steady RPM as well as spin-up.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] Complete over-long line also drops the connection**
- **Found during:** Task 2 (oversize test)
- **Issue:** The plan's rule (drop when more than 4096 bytes arrive without a newline) did not cover a 5000-byte line whose newline arrived in the same read batch; it was parsed instead of rejected, and the first version of the check never fired.
- **Fix:** Reject a line whose length including the newline exceeds `kMaxLine`, in addition to the unfinished-buffer check.
- **Files modified:** components/sim_bus/sim_bus.cpp
- **Verification:** TestFakePeer sends 5000 bytes without a newline (reconnect as connection 3) and a 5000-byte line (connection 4)
- **Committed in:** 536f00e

### Plan wording not followed literally

- Hub `path:` default kept (the 01-01 suggestion to drop it was considered; the plan states the default stays and removing it would force edits to examples and sil-generated configs outside this plan's files). See Needs Human.
- `TestGuardsCompile` additionally runs both compiled fixtures and checks the hello on the YAML path with `SIL_BUS` empty (covers the "empty SIL_BUS is ignored" truth, which no other test exercises).
- `TestFakePeer` uses a rising ramp rather than a constant 1000 so the 1 s mean is distinguishable from the last frame; the values still sit around 1000 (1004 to 1055).

---

**Total deviations:** 1 auto-fixed (1 missing critical)
**Impact on plan:** No scope change.

## Issues Encountered

None.

## Threat Flags

None. T-01-11 (rx flood) and T-01-12 (path truncation) are mitigated and tested.

## Known Stubs

None.

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocking. Open assumptions carried forward by the plan:
- **A4 / EDGE-IO-02-unclassified:** on bus loss the RPM sensor stops publishing and Home Assistant keeps the last value. Confirm with a live Home Assistant in Phase 2 whether unavailable/NaN is preferred. Window with zero samples is handled (no publish), millis wrap is handled by unsigned subtraction (not exercised by a test).
- **Hub path default:** `/tmp/sil.sock` remains the YAML default (01-01 threat flag). It is only reachable when firmware runs without `SIL_BUS`, which `sil` and the harness never do. Decide whether 01-10 or a later plan should make `path:` required.

## Next Phase Readiness

Ready for 01-08 and 01-10. For 01-10: the hub still uses the old hand-rolled parser and the fixed 500 ms retry; `kMaxLine` should become `proto::MAX_LINE`, and the snapshot replay in `try_connect_` is where HELLO manifest, welcome and sync slot in. `SimChild::direction()` is in place for the manifest.

## Self-Check: PASSED

- All 6 created and 4 modified files exist; commits `e774614` and `536f00e` found in `git log`.
- `make check` passes (112 Go tests plus the C++ codec test).
- `make e2e E2E_RUN='TestGuardsCompile|TestRPMMean|TestFakePeer|TestCodegenValidation|TestTracer|TestFirmwareBus'` prints PASS for all six; TestLoop also passes.
- Acceptance greps: `sun_path limit is 107`, `FINAL_VALIDATE_SCHEMA`, `check_unique_channels`, `USE_OUTPUT`, `USE_SENSOR`, `PollingComponent`, `polling_component_schema` match; no `components/sensor|output` include in sim_bus.h; no tests/fixtures directory.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

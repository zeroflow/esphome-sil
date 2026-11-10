---
phase: 02-full-rev-3-3-bench
plan: 03
subsystem: environment inputs, sil set, sim-bus sensors, rev33 example
tags: [esphome, sensor, sim-bus, go, set, temperature, humidity, rev33, cli]
requires:
  - phase: 02-full-rev-3-3-bench
    provides: 02-01 optional manifest channels, rev33 example, r33 e2e helpers; 02-02 Status.leds
provides:
  - sensor `mode: window_mean | latest` (typed schema) and SimValueSensor (push-only, unaveraged)
  - engine env (Config.Env, Snapshot.Env, Apply(Change), per-value 10 s re-send, env in the in-snapshot)
  - supervisor-held inputs (inMu) that seed every engine; parseSet and Set (all or nothing)
  - control.SetRequest/SetResult/EnvStatus, POST /api/v1/set, Status.env
  - `sil set key=value... [-i NAME]`; error codes unknown_key and value_out_of_range
  - examples/rev33.yaml Temperature and Humidity: the API surface is exactly the 13 HIL contract entities
affects: [02-04 PROTOCOL.md and vectors, 02-05 port fan keys on set, 02-06 watch env events, 02-07 contract test, 03 UI]

actuals:
  tokens: 16400
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Env values are engine state sent on change and re-sent every EnvResend of sim time, one deadline per value"
    - "Inputs live in the supervisor under inMu; inMu covers record+apply in Set and read+create+slot in startEngine callers, so a restart can neither lose nor double-apply a set"
    - "parseSet is a pure function over raw JSON values; the CLI sends strings, the HTTP API accepts numbers or numeric strings"
    - "Firmware sensor mode is a typed schema: only the window_mean branch carries polling_component_schema"

key-files:
  created: []
  modified:
    - components/sim_bus/sensor.py
    - components/sim_bus/sim_sensor.h
    - components/sim_bus/sim_sensor.cpp
    - examples/rev33.yaml
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - internal/control/control.go
    - internal/cli/cli.go
    - internal/cli/cli_test.go
    - internal/apperr/codes.go
    - e2e/rev33_test.go
    - e2e/leds_test.go

key-decisions:
  - "Each env value has its own re-send deadline: Apply(temperature) sends only temperature and re-arms only its timer, so humidity keeps its 10 s rhythm"
  - "The env frames are sent whether or not the manifest declares the channels (like rpm); a firmware without them ignores in frames for unknown channels, so fan-skeleton is unaffected. A wrong direction or type of a declared channel is one warning"
  - "Number syntax is a plain decimal regex before strconv.ParseFloat: hex floats (0x10), digit separators (1_0), Inf/NaN words and overflow (1e999) are invalid_argument, not accepted"
  - "Set takes inMu, never opMu; ErrStopped from a replaced engine is fine because the next engine is seeded from the recorded inputs"
  - "SimRestart, run() hold inMu from startEngine until the engine is in the slot (the plan's T-02-09 mitigation); Set's engine apply happens under the same lock"

requirements-completed: [IO-05, CLI-02, CLI-01, FW-01, FW-02]

coverage:
  - id: D1
    description: "`sil set temperature=24.5 humidity=40` returns {applied:{humidity:40,temperature:24.5}}, `sil status` env and the firmware's Temperature/Humidity entities show those values, defaults 22/45 on a fresh run"
    requirement: IO-05
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestEnvSet"
        status: pass
    human_judgment: false
  - id: D2
    description: "Values reach the entities unaveraged: a watch series spanning the set holds only the old and the new value and ends at the new one"
    requirement: IO-05
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestEnvSet"
        status: pass
    human_judgment: false
  - id: D3
    description: "All-or-nothing validation and the error vocabulary: value_out_of_range with key/min/max, unknown_key with known, invalid_argument for non-numeric, NaN, Inf, hex, overflow, empty, missing value, duplicate; adjacency bounds -40/125/0/100 accepted and 0.1 outside rejected; applied keys sorted"
    requirement: CLI-02
    verification:
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestParseSet"
        status: pass
      - kind: unit
        ref: "internal/cli/cli_test.go#TestSetMisuse, TestSetSendsPairsAsTyped, TestSetInstanceSelection, TestSetServerErrorPassesThrough"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestSetVocabulary"
        status: pass
    human_judgment: false
  - id: D4
    description: "Per-value 10 s sim-time re-send, only the changed value sent on Apply, Config.Env in the in-snapshot, ErrStopped after Run, manifest warnings for a wrong env channel"
    requirement: IO-05
    verification:
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineEnvResendEvery10s, TestEngineApplySendsChangedValueAtOnce, TestEngineConfigEnvInSnapshot, TestEngineApplyAfterStopAnswersAtOnce, TestEngineEnvManifestWarnings"
        status: pass
    human_judgment: false
  - id: D5
    description: "Set values survive sim restart, fw restart and a set while the bus is down (accepted, reaches the firmware after the resync)"
    requirement: IO-05
    verification:
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestSetSeedsTheNextEngine"
        status: pass
      - kind: e2e
        ref: "e2e/rev33_test.go#TestSetVocabulary/survives_sim_restart, survives_fw_restart, bus_down_accepted"
        status: pass
    human_judgment: false
  - id: D6
    description: "The native API of examples/rev33.yaml lists exactly the 13 HIL contract entities (byte-exact, case-sensitive set; no light or partition) and `sil status` carries every CLI-01 field"
    requirement: FW-02
    verification:
      - kind: e2e
        ref: "e2e/rev33_test.go#TestRev33Entities"
        status: pass
    human_judgment: false
  - id: D7
    description: "`mode: latest` is a push-only Component that rejects update_interval; RPM sensors and fan-skeleton stay window_mean"
    requirement: FW-01
    verification:
      - kind: e2e
        ref: "e2e/io_test.go#TestRPMMean; e2e/codegen_test.go#TestCodegenValidation"
        status: pass
    human_judgment: false

duration: 15min
completed: 2026-11-09
status: complete
commits: 3
plan_head_before: df18b618551a033462ce11908bd72aa22db7382b
plan_head_after: 907e1c6fc42cf6398ae08646668d52808a4223f2
---

# Phase 2 Plan 03: Environment slice Summary

**`sil set temperature=... humidity=...` validates every pair first, applies them in one engine event, and the real Rev 3.3 firmware's `Temperature`/`Humidity` entities report exactly those values unaveraged (22 / 45 by default, re-sent every 10 s of sim time, kept across sim and firmware restarts); the example now exposes exactly the 13 HIL contract entities.**

## Performance

- **Duration:** about 15 min (start time not recorded at launch; estimated from the 02-02 completion commit at 07:42 to the last commit at 07:52, local time, plus the full e2e)
- **Tasks:** 2 (1 auto, 1 TDD)
- **Files:** 14 modified, 0 created (+1209 / -30 lines)
- **Verification:** `make check` (Go race tests plus 112 C++ vectors) and the full `make e2e` (41 tests, 481 s) pass, including TestEnvSet, TestSetVocabulary (9 subtests), TestRev33Entities, TestPressTracer, TestPressVocabulary, TestLEDStatus, TestLEDFrames, TestRPMMean, TestCodegenValidation, TestSimRestartResync, TestFwRestartResync, TestHubSession, TestFakePeer.

## Accomplishments

- Firmware: `sensor.py` is a typed schema (`mode` default `window_mean`, so every RPM sensor and fan-skeleton are unchanged). `latest` extends `COMPONENT_SCHEMA` only, so ESPHome rejects `update_interval` on it; `SimValueSensor` publishes every received value as is and keeps its state when the simulator goes away.
- Engine: `Env`, `Change`, `Apply`, `EnvResend`; the in-snapshot is now welcome, 4 rpm, temperature, humidity, usr1..usr3, sync; each value is re-sent every 10 s of sim time from its own deadline; `Snapshot.Env`.
- Supervisor: inputs (defaults 22 / 45) seed every engine through `startEngine`; `parseSet` is pure (sorted keys, first error wins); `Set` records inputs and applies the Change under `inMu` and never takes `opMu`.
- Control and CLI: `POST /api/v1/set` (64 KiB, unknown fields rejected, missing/null `values` rejected), `Client.Set`, `Status.env` right after `ports`, `sil set` splitting at the first `=` and sending strings as typed.
- Example: `Temperature` (degC, 1 decimal) and `Humidity` (%, 1 decimal) with `mode: latest`; the native API lists exactly Button USR1..3, Fan 1..4, Fan 1..4 Speed, Humidity, Temperature.

## Task Commits

1. **Task 1: sil set temperature/humidity to the firmware's env entities** - `5806699` (feat)
2. **Task 2 unit tests: parseSet edges, engine env timing, sil set misuse** - `723c573` (test)
3. **Task 2 e2e: TestSetVocabulary and TestRev33Entities** - `907e1c6` (test)

**Plan metadata:** committed separately (docs: complete plan).

## TDD Gate Compliance

Task 2 is `tdd="true"` but its production code (validation, re-send, restart persistence) is Task 1's, as the plan lays out, so there is no RED commit for it. Honest RED evidence from the work itself: the first run of the new engine tests failed (`TestEngineEnvResendEvery10s`, `TestEngineApplySendsChangedValueAtOnce`) because the tick-loop call to `resendEnv` was missing from Task 1's engine (a replace that silently did not match); it was fixed before the Task 1 commit. Mutation check for the e2e tests: with `startEngine` no longer seeding `cfg.Env`, `TestSetVocabulary` failed in `atomic_reject`, `unknown_key`, `not_a_number` (env 0/0 instead of 22/45), `survives_sim_restart`, `survives_fw_restart` and `bus_down_accepted`; the original code was restored. GREEN is `make check` plus the full `make e2e`; no REFACTOR.

## Files Created/Modified

- `components/sim_bus/sensor.py`, `sim_sensor.h`, `sim_sensor.cpp` - sensor mode and `SimValueSensor`
- `examples/rev33.yaml` - Temperature and Humidity sensors
- `internal/engine/engine.go` (+ test) - env state, `Apply`, re-send, manifest check
- `internal/supervisor/supervisor.go` (+ test) - inputs, `parseSet`, `Set`, `Status.Env`
- `internal/control/control.go` - `SetRequest`, `SetResult`, `EnvStatus`, route, client
- `internal/cli/cli.go` (+ test) - `sil set`
- `internal/apperr/codes.go` - `unknown_key`, `value_out_of_range`
- `e2e/rev33_test.go` - `TestEnvSet`, `TestSetVocabulary`, `TestRev33Entities` and helpers
- `e2e/leds_test.go` - flake fix, see Deviations

## Decisions Made

See `key-decisions` above. Defaults and ranges follow the plan: temperature -40..125 degC, humidity 0..100 %RH (HDC1080 operating range, research assumption A1, still for the maintainer to confirm).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Plan-conformant `watch` timing for the unaveraged check**
- **Found during:** Task 1, TestEnvSet
- **Issue:** the plan asks for `watch --seconds 4` started before the set. The client runs in a docker container whose start and Noise handshake take 1 to 2 s, so a 4 s watch can end before or right after the set and prove nothing.
- **Fix:** `watch --seconds 14`, the set 6 s after the start, and the series must contain both the old and the new value, every point must be one of them and the series must end at the new value.
- **Files modified:** `e2e/rev33_test.go`
- **Commit:** `5806699`

**2. [Rule 1 - Bug] Pre-existing flake in TestLEDStatus (02-02)**
- **Found during:** Task 1 verify (the plan lists TestLEDStatus in its command)
- **Issue:** `board LED changed between two reads 500 ms apart: #002300 then #002500 (want static green)`. The test treated the first green-dominant read as the end of the board LED's default 1 s fade-in; gamma makes the start of the ramp slow, so the first read can already be green while still fading. Unrelated to this plan's code.
- **Fix:** the static check now allows up to 3 s for the fade to finish (consecutive reads 500 ms apart must become equal) and still fails if the LED keeps changing.
- **Files modified:** `e2e/leds_test.go`
- **Verification:** TestLEDStatus passes in three later runs (task run, task rerun, full e2e).
- **Commit:** `5806699`

**3. [Rule 2 - Missing critical] Strict number syntax**
- **Found during:** Task 2, writing TestParseSet
- **Issue:** `strconv.ParseFloat` alone accepts hex floats (`0x10`) and, with a base prefix, digit separators; the plan names only NaN, Inf and non-numeric as rejected.
- **Fix:** a plain-decimal regex precedes `ParseFloat`; overflow (`1e999`) is rejected as well. Covered in TestParseSet and e2e `not_a_number`.
- **Files modified:** `internal/supervisor/supervisor.go`
- **Commit:** `5806699`

### Plan-order notes (not defects)

- The engine tests for the new in-snapshot order (`TestEngineSnapshotOrderAfterHello`, `TestEngineManifestWarnings`, `TestEngineSnapshotCarriesCurrentRPM`, button reconnect) were updated in Task 1 as the plan says (11 frames: welcome, 4 rpm, 2 env, 3 buttons, sync); the new engine, supervisor and CLI tests are in their own commit before the Task 2 e2e commit.
- "Within 2 s / 3 s" in the e2e tests is checked as a deadline for `sil status` polls; the entity check goes through a docker API client whose own start-up takes about 2 s, so those checks mean "the first reads after the deadline start show the value", which is the stronger statement when the first read already succeeds (it did in every run).

---

**Total deviations:** 3 auto-fixed (2 bug, 1 missing critical) plus order notes.
**Impact on plan:** no scope change; every acceptance criterion met.

## Issues Encountered

- A `str.replace` in my own edit script silently did not match the tick loop (indentation), so the first engine build had no 10 s re-send; the new unit test caught it immediately. TestEnvSet itself passed on that build because it does not wait 10 s.
- Edge: `sil set` with no pairs is rejected by kong's required positional before `SetCmd.Run` (still `invalid_argument`, exit 1); the Run-level check is a second line of defence.

## Known Stubs

None. No placeholder data flows to a consumer.

## Threat Flags

None beyond the plan's register. T-02-07 (finite-number check, fixed ranges, key allow-list) and T-02-08 (64 KiB body, unknown fields rejected, validation before the single engine op, `Set` never takes `opMu`) and T-02-09 (`inMu` over record+apply and read+create+slot) are implemented and tested. No Go module or package was added (T-02-SC).

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocks the plan. For the morning review:

- **Range assumption A1 (flagged in the plan):** temperature -40..125 degC and humidity 0..100 %RH follow the HDC1080 operating range from training knowledge; please confirm.
- **EDGE-IO-05-unclassified (flagged in the plan):** the plan's manual edge review of IO-05 is still open; automated coverage is listed under D1, D2, D4, D5.
- **Home Assistant check (D-15):** that HA shows `Temperature` / `Humidity` with the right unit and device class is part of the end-of-phase manual UAT; only the aioesphomeapi view (names and values) is automated here.
- **Wire documentation pending by plan:** the env channels (`temperature`, `humidity`, f32 `in`) and the 10 s re-send are not yet in `protocol/PROTOCOL.md` or the vectors (02-04 owns that).

## Next Phase Readiness

Ready for 02-04. `Change` is the extension point for 02-05 (fans); `Status.env`, `EnvStatus` and `engine.Env` are in place for 02-06's `env` watch events. No blockers.

## Self-Check: PASSED

- Modified files exist and carry the new symbols (`grep` of `SimValueSensor`, `mode: latest` x2, `/api/v1/set`, `value_out_of_range`, one `polling_component_schema` line in sensor.py).
- Commits exist: `5806699`, `723c573`, `907e1c6`.
- `make check` passes (gofmt, vet, race tests, C++ vectors); the full `make e2e` passes (41 tests, 481 s).

---
*Phase: 02-full-rev-3-3-bench*
*Completed: 2026-11-09*

---
phase: 04-fan-model-v2
plan: 06
subsystem: engine
tags: [go, status, watch, tach, fan-laws, airflow, power, events, history]

requires:
  - phase: 04-fan-model-v2
    provides: "04-04 Plant.Reading for every fan (30 x pulses or the exact 1 s shaft mean, unavailable only in the first second); 04-05 sim.seed in status; 04-03 Params.Datasheet filled by fanlib"
provides:
  - "sil status ports[i] always carries tach_rpm, airflow_m3h and power_w (null when unknown, never absent, never a made-up 0); rpm stays shaft truth; Status schema stays 1"
  - "tach events (v = last completed 1 s gate reading or null), at most one per sim second per port, for tach and ideal-tach fans alike, forced at engine start and right after a fan swap"
  - "load events (events.Load{airflow_m3h, power_w}, both keys always present) on a change at the 1 s rpm sample and right after a swap"
  - "plot samples (sample events and sil history) carry tach per port, null while the firmware is off the bus or the plant has no reading"
  - "model.Datasheet.Airflow (rpm) and Power (rpm cubed), model.Plant.Outputs on the noise-free shaft"
  - "testdata/fans-v2/load-test.toml: schema 2 fixture with [tach] ppr 2 and [datasheet] (100 m3/h, 2 W at 1700 rpm)"
affects: [04-07, 04-08, 04-10, phase-05-faults]

plan_head_before: f8f22e42e5aa773c3b72c147b3a79b32d5fa4ced
plan_head_after: 240b1b467730b81392b1f48166de16d8a4198c31

actuals:
  tokens: 7600
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "A nil *float64 inside Event.V is a non-nil interface, so it encodes as \"v\":null rather than being dropped by omitempty"
    - "Per-port event bookkeeping with a force flag: emitTach/emitLoad compare in float32 and null-ness, and a forced call (engine start, fan swap) emits even when nothing changed, so a stream always sees the new plant's state without the page deriving anything"
    - "Status copies engine snapshot pointers into fresh values (copyFloat), never sharing"

key-files:
  created:
    - internal/model/derive.go
    - internal/model/derive_test.go
    - internal/engine/surfaces_test.go
    - internal/supervisor/surfaces_test.go
    - testdata/fans-v2/load-test.toml
  modified:
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/events/events.go
    - internal/control/control.go
    - internal/supervisor/supervisor.go
    - internal/cli/cli.go

key-decisions:
  - "tach and load events are forced once per port at engine start (like state events) as well as after a swap: a stream that outlives a sim restart sees the new engine's null/values without the page computing anything"
  - "Load values go out rounded to float32 like rpm events; the snapshot carries the exact float64 fan-law values"
  - "Fan-law exponents fixed at 1 and 3 (A-EXP), documented in derive.go with the low-speed power underestimate"
  - "TestSampleCadence's exact-JSON assertion for a null sample entry gained the new explicit \"tach\":null key (additive field)"

patterns-established:
  - "Engine surface tests use a seeded harness variant (newSeededHarness) instead of changing the shared startEngine"
  - "Supervisor tests of status build on watchSup and swap in held fans (tachFanSupWith), then advance the Manual clock with advanceSim"

requirements-completed: [MODEL-09, MODEL-11]

coverage:
  - id: D1
    description: "sil status ports carry tach_rpm, airflow_m3h and power_w on every port, always present, schema 1; tach_rpm is null only until the first 1 s gate and 0 (a value) for a fan at rest, for a tach fan and an ideal-tach fan alike; rpm stays the shaft"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/supervisor/surfaces_test.go#TestStatusCarriesTachAndLoad"
        status: pass
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineTachEvents"
        status: pass
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineIdealTachEvents"
        status: pass
    human_judgment: false
  - id: D2
    description: "sil watch emits tach events (null or the new gate reading) at most once per sim second per port, for tach and ideal fans, with a null right after a fan swap; --kind tach and --kind load are accepted"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineTachEvents"
        status: pass
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineIdealTachEvents"
        status: pass
      - kind: unit
        ref: "internal/supervisor/surfaces_test.go#TestStatusCarriesTachAndLoad"
        status: pass
    human_judgment: false
  - id: D3
    description: "Plot samples (sample events and sil history) carry tach per port: the gate reading while the firmware is on the bus and the plant has one, null otherwise; an ideal tach in a sample equals the shaft rpm at steady state"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineSampleCarriesTach"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestSampleCadence"
        status: pass
    human_judgment: false
  - id: D4
    description: "Airflow = airflow*rpm/ref_rpm and power = power*(rpm/ref_rpm)^3 from the noise-free shaft rpm, in status and in load events (on change at the 1 s rpm sample and right after a swap); a fan without [datasheet] shows null for both, never 0"
    requirement: MODEL-11
    verification:
      - kind: unit
        ref: "internal/model/derive_test.go#TestFanLaws"
        status: pass
      - kind: unit
        ref: "internal/model/derive_test.go#TestOutputsWithoutDatasheet"
        status: pass
      - kind: unit
        ref: "internal/model/derive_test.go#TestOutputsAreNoiseFree"
        status: pass
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineLoadEvents"
        status: pass
      - kind: unit
        ref: "internal/supervisor/surfaces_test.go#TestStatusAirflowAndPowerFromDatasheet"
        status: pass
    human_judgment: false
  - id: D5
    description: "Two engines with the same seed, fan and duty script on the Manual clock emit identical tach event sequences; another seed differs when noise_rpm > 0"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/engine/surfaces_test.go#TestEngineSameSeedSameTachTrace"
        status: pass
    human_judgment: false
  - id: D6
    description: "The two-value readout (shaft rpm and tach rpm) and the airflow/power numbers as a developer reads them in the live CLI and on the page, and whether the cubic power law is adequate at low duty for the fans in use"
    verification: []
    human_judgment: true
    rationale: "No live sil up was run in this plan (unit and engine level only); the page side is 04-07 and the live bench check is 04-10. The adequacy of the cubic law at low speed is a modelling judgement (PITFALLS 31), not something a test asserts."

duration: 14min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 06: Tach, Airflow and Power in Status, Watch and History Summary

**`sil status`, `sil watch` and the history samples now show, per port, the shaft `rpm` next to `tach_rpm` (the last completed 1 s gate reading, for tach and ideal-tach fans alike), plus `airflow_m3h` and `power_w` from the fan's datasheet by the fan laws (airflow with rpm, power with rpm cubed), null for a fan without a datasheet; Status schema stays 1.**

## Performance

- **Duration:** about 14 min
- **Completed:** 2026-11-11T06:15:04Z (code); SUMMARY follows
- **Tasks:** 2 (Task 1 tracer, Task 2 TDD): 2 RED + 2 GREEN commits
- **Files modified:** 11 (5 created, 6 modified)

## Accomplishments

- Tracer (Task 1): `Plant.Reading` flows to `engine.PortState.TachRPM`, `control.PortStatus.tach_rpm`, `tach` events and `Sample.Tach`. A missing reading is a typed nil `*float64` in `Event.V`, which encodes as `"v":null`. Tracer feedback gate (auto mode, `end-of-phase`, automated-only verify): the `<verify>` command (`make test-pkg ... && make check`) was green end to end before the expansion started, so `Tracer verified end-to-end - expanding`.
- Expansion (Task 2): `Datasheet.Airflow/Power` and `Plant.Outputs` (read-only, never on the bus), `load` events on the existing 1 s rpm sample, forced at engine start and after a swap, and the `load-test` schema 2 fixture.
- The status mapping copies all three pointer values into fresh values (`copyFloat`), so a status never shares memory with an engine snapshot.

## Task Commits

1. **Task 1 (tracer): tach_rpm in status, watch and samples**
   - RED `ed3d89e` (test): field stubs plus `TestEngineTachEvents`, `TestEngineSampleCarriesTach`, `TestEngineIdealTachEvents`, `TestEngineSameSeedSameTachTrace`, `TestStatusCarriesTachAndLoad` failing on assertions (no tach events, nil TachRPM)
   - GREEN `be9e613` (feat): `emitTach` (float32 and null-ness compare, force at start and swap), snapshot and sample wiring, supervisor mapping
2. **Task 2: airflow and power from the datasheet**
   - RED `305c67e` (test): `load-test.toml`, `TestFanLaws`, `TestOutputsAreNoiseFree`, `TestEngineLoadEvents` failing on assertions against a compiling `derive.go` stub
   - GREEN `240b1b4` (feat): fan laws, `Plant.Outputs`, `emitLoad`, CLI help, `TestStatusAirflowAndPowerFromDatasheet`

**Plan metadata:** committed separately (docs: complete plan)

## Files Created/Modified

- `internal/model/derive.go` - `Datasheet.Airflow`, `Datasheet.Power`, `Plant.Outputs` (fixed exponents 1 and 3)
- `internal/engine/engine.go` - `PortState.{TachRPM,AirflowM3h,PowerW}`, `emitTach`, `emitLoad`, `Sample.Tach`, forced emits at start and swap
- `internal/events/events.go` - kinds `tach` and `load`, `Sample.Tach`, `events.Load`
- `internal/control/control.go` - `PortStatus.{TachRPM,AirflowM3h,PowerW}` (always present)
- `internal/supervisor/supervisor.go` - status mapping, `copyFloat`
- `internal/cli/cli.go` - `watch --kind` and `history` help
- `internal/engine/surfaces_test.go`, `internal/supervisor/surfaces_test.go`, `internal/model/derive_test.go`, `testdata/fans-v2/load-test.toml` - tests and fixture
- `internal/engine/engine_test.go` - `TestSampleCadence` expects the new explicit `"tach":null` key

## Decisions Made

- `tach` and `load` are forced once per port at engine start, like `state`: a watcher that outlives a simulator restart sees the new engine's null or values, and the page (04-07) derives nothing. Side effect: every engine start emits one `tach` and one `load` event per port.
- Load values are emitted as float32-rounded numbers (like `rpm` events); the snapshot and status carry the exact float64 fan-law result.
- Exponents fixed at 1 and 3 (A-EXP), documented at the top of `derive.go`.

## Review of the flagged edge assumptions (resolved at execution, user decision 2026-11-11)

- **EDGE-MODEL-11-unclassified (airflow and power from datasheet values in status, watch and the page): reviewed, accepted as covered.** Fixed exponents 1 and 3 on the noise-free shaft (`TestFanLaws`, `TestOutputsAreNoiseFree`: outputs equal the fan laws of `Shaft()` exactly while the tach carries noise 50); null without `[datasheet]` in the plant (`TestOutputsWithoutDatasheet`), in the engine snapshot, in load events and in status JSON (`TestEngineLoadEvents` swap to curve-test, default fan on port 2 keeps one null load event for 10 s, `TestStatusCarriesTachAndLoad` and `TestStatusAirflowAndPowerFromDatasheet` with explicit `null` keys); load events on the 1 s rpm cadence and right after a swap, never closer than 1 s after the first sample (`TestEngineLoadEvents`). Residual: (1) the cubic law underestimates real power at low speed (PITFALLS 31), stated in `derive.go`; (2) `RefRPM` of 0 would divide by zero, which fanlib rejects at load (`ref_rpm > 0`), so a `Datasheet` only reaches the plant through a validated file; (3) the page side is 04-07, not asserted here.
- **A-EXP (Claude's discretion, exponents fixed at 1 and 3): resolved, implemented as planned.** No key in `[datasheet]` overrides them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Existing assertion pinned the old Sample JSON**
- **Found during:** Task 1 (RED run)
- **Issue:** `TestSampleCadence` compared a null sample entry with the exact string `{"port":1,"duty":null,"rpm":null}`; the additive `tach` key (always present, per the plan) changes it.
- **Fix:** The expected string is `{"port":1,"duty":null,"rpm":null,"tach":null}`.
- **Files modified:** `internal/engine/engine_test.go` (not in the plan's file list)
- **Verification:** `make check` green
- **Committed in:** `ed3d89e`

**2. [Rule 3 - Blocking] Wall-clock lint on the supervisor test**
- **Found during:** Task 1 (`make check`)
- **Issue:** `TestNoWallClock` rejected `time.Sleep` in the new `advanceSim` helper.
- **Fix:** The helper waits with the package's `waitFor`.
- **Files modified:** `internal/supervisor/surfaces_test.go`
- **Committed in:** `be9e613`

---

**Total deviations:** 2 auto-fixed (both Rule 3, test-only)
**Impact on plan:** None on production behavior.

Notes (not deviations): `TestOutputsWithoutDatasheet` already passed in its RED commit (the stub reports ok false), so it is a regression guard; the intentional REDs were `TestFanLaws`, `TestOutputsAreNoiseFree` and `TestEngineLoadEvents`. The plan's `TestEngineTachEvents` text expects a reading within 30 of 1100 "1 s later"; the plant spins up from rest (tau 0.2 s), so the first gate reads lower, and the test checks the settled reading after 8 s instead (and every reading for whole multiples of 30 and the 1 s spacing). I added `TestStatusAirflowAndPowerFromDatasheet` so the supervisor mapping of non-null airflow and power is covered, not only the nulls.

## TDD Gate Compliance

Both tasks have a `test(04-06)` RED commit (`ed3d89e`, `305c67e`) before the `feat(04-06)` GREEN commit (`be9e613`, `240b1b4`). No refactor commit was needed. `workflow.tdd_mode` is off, so no `check tdd-red-evidence` record was persisted; the RED failures are the assertion failures described above.

## Issues Encountered

None.

## Known Stubs

None. The RED-phase stubs in `derive.go` were replaced in the GREEN commit.

## Threat Flags

None. T-04-13 mitigated (tach at most once per sim second per port, load only on change at the 1 s sample; the "no two events closer than 1 s" checks in `TestEngineTachEvents`, `TestEngineIdealTachEvents` and `TestEngineLoadEvents`). T-04-14 mitigated (pointer fields with explicit null; `TestStatusCarriesTachAndLoad`, `TestEngineLoadEvents`). No new endpoint or trust boundary.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-07 (page) can read `tach_rpm`, `airflow_m3h`, `power_w` from the snapshot and fold `tach` and `load` events and `Sample.Tach` without deriving anything; the events are forced at engine start and after a swap.
- 04-10 can use `testdata/fans-v2/load-test.toml` for the real-bench page test.
- Not run here: `make e2e` / `make test-ui` (not part of `make check`); no `sil up` instance was started.

## Self-Check: PASSED

All five created files exist on disk; commits `ed3d89e`, `be9e613`, `305c67e`, `240b1b4` are in `git log` (`git rev-list --count f8f22e4..HEAD` is 4); `make check` exits 0 (gofmt, vet, `go test -race ./...`, C++ tests); `grep 'json:"tach_rpm"'` matches `internal/control/control.go`, `grep 'json:"tach"'` matches `internal/events/events.go`, `grep '"load"'` matches the `Kinds` line; `TestEngineTachEvents`, `TestStatusCarriesTachAndLoad`, `TestEngineIdealTachEvents`, `TestEngineSampleCarriesTach`, `TestEngineLoadEvents` and `TestFanLaws` each ran without "no tests to run"; no file deletions in the task commits.

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

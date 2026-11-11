---
phase: 04-fan-model-v2
plan: 04
subsystem: model
tags: [go, fan-model, tach, pulse-accumulator, seeded-noise, keyed-hash, spike-s7a, lint]

requires:
  - phase: 04-fan-model-v2
    provides: "04-02 model.advance and Integral (exact rpm area and moving seconds), Plant seam with seed and port from 04-01"
provides:
  - "model.Tach pulse accumulator and 1 s gate state; BoardPPR 2, TachGate 1 s, NoiseBucket 100 ms"
  - "tach fans send whole-pulse rpm frames (600 rpm per pulse per 50 ms frame at ppr 2) over the unchanged sim-bus protocol 1"
  - "Plant.Reading for every fan: 30 x pulses of the last gate (tach) or the exact 1 s shaft mean (ideal tach), unavailable only in the first second"
  - "stateless keyed-hash noise z(seed, port, 100 ms bucket) on the tach path only; tach.noise_rpm is the std of one 1 s reading"
  - "hermetic spike S7a: sim_sensor's 1 s window over the real plant frames keeps the 30 rpm steps (lattice and toggle gates)"
  - "lint: any global math/rand or math/rand/v2 source use under internal/model fails make check"
affects: [04-05, 04-06, 04-08, 04-10, phase-05-faults]

plan_head_before: e8accf5b6f24a993bdf5ffdf4dfd731d73c0c04c
plan_head_after: 58d9ad1eb3a099be3b8b266d23483d04a1686598

actuals:
  tokens: 11400
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Tach path of Plant.Step splits dt at 100 ms bucket edges and counts whole pulses from advance's exact Integral.Area; a phase accumulator carries the fractional pulse, with a 1e-9 pulse tolerance for on-grid rates"
    - "Ideal tach keeps the single v0.1.0 advance call; the 1 s shaft mean comes from a second advance call on the step's start state whose State is discarded"
    - "Noise = splitmix64 Irwin-Hall hash of (seed, port, bucket), added to the pulse count only, over Integral.MovingS so a fan at rest never pulses"
    - "Frame normalises by elapsed plant time; Current returns the last Frame value and never drains"

key-files:
  created:
    - internal/model/tach.go
    - internal/model/noise.go
    - internal/model/tach_test.go
    - internal/model/s7_test.go
    - internal/engine/tach_test.go
  modified:
    - internal/model/plant.go
    - internal/engine/engine.go
    - internal/lint/wallclock_test.go

key-decisions:
  - "Pulse count uses floor(phase + 1e-9) so a fan on the pulse grid (1200 rpm = 40 pulses/s) gives the same whole pulses on every tick pattern despite float rounding; the phase may sit up to 1e-9 below zero after a count, harmless"
  - "checkGlobalRand flags any package-level name of math/rand and math/rand/v2 outside the constructors and types (not only calls), so a function value like g := rand.Int63 is caught too"
  - "S7a applies the engine's change-only rule to the emulated frames and keeps publishing after the last frame (a constant 1800 rpm sends one frame only)"
  - "EDGE-MODEL-09 accepted as bounded: the first Frame after a bus gap is the true mean pulse rate over the gap (TestFrameAfterGapIsMeanRate); no code change"

patterns-established:
  - "Tick-pattern invariance tests compare gate readings and total pulses across 10 ms, 7 ms, cycled 10/30/3 ms, 1 s and 3 s steps"
  - "Spike emulations of firmware arithmetic live in model/s7_test.go and log their numbers with t.Logf for the SUMMARY"

requirements-completed: [MODEL-09, MODEL-10]

coverage:
  - id: D1
    description: "A fan with [tach] ppr P counts whole pulses from the exact rpm integral; every port rpm frame is pulses*60/(2*elapsed), a multiple of 600 rpm per 50 ms frame at ppr 2, and the 10 s time-weighted mean of the frames equals the shaft rpm (within 15 rpm); protocol, rpmFrameStep and the C++ side are unchanged"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/model/tach_test.go#TestTachPulsesFromIntegral"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestFrameUnits"
        status: pass
      - kind: integration
        ref: "internal/engine/tach_test.go#TestEngineTachFrames"
        status: pass
      - kind: other
        ref: "git diff --stat v0.1.0..HEAD -- protocol/ components/ (empty)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The hello in-snapshot carries the last frame value without draining pulses; a frame after a multi-second bus gap is the true mean pulse rate"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/model/tach_test.go#TestCurrentDoesNotDrain"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestFrameAfterGapIsMeanRate"
        status: pass
      - kind: integration
        ref: "internal/engine/tach_test.go#TestEngineTachFrames"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every plant has a 1 s gate Reading on whole-second plant time: 30 x pulses of the last gate for a tach fan, the exact time-weighted shaft mean for an ideal-tach fan (analytic spin-up mean, tick-independent, 0 at rest), unavailable only during the first second after NewPlant"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/model/tach_test.go#TestGateReading"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestIdealReading"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestIdealTachIsShaft"
        status: pass
    human_judgment: false
  - id: D4
    description: "Schema 1 (ideal tach) shaft, Running, Frame and Current stay bit-identical to v0.1.0 although the plant now computes a gate reading with gate edges inside 7 ms, 30 ms and 1 s steps"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/model/fan_v010_test.go#TestPlantMatchesV010ForLegacyParams"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestIdealTachIsShaft"
        status: pass
    human_judgment: false
  - id: D5
    description: "Deterministic seeded noise on the tach path only: keyed hash with unit moments, identical gate readings and pulse totals for any tick pattern (constant duty and a duty script on a common grid), different traces for different seeds or ports, no pulses at rest; noise_rpm is the std of a 1 s reading (20 gives 22-24 rpm over 600 gates, 5 levels; 0 gives the single level 1200)"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/model/tach_test.go#TestNoiseMoments"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestNoiseIsKeyed"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestNoiseAmplitudeIsGateStd"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestTachIgnoresTickTiming"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestTachDutyScriptOnCommonGridIsTickInvariant"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestSeedChangesTheTrace"
        status: pass
      - kind: unit
        ref: "internal/model/tach_test.go#TestNoPulsesAtRest"
        status: pass
    human_judgment: false
  - id: D6
    description: "Spike S7a: the firmware's 1 s time-weighted window keeps the 30 rpm steps: no bias, bounded spread, lattice (at most three residues mod 30, 0 among them) at zero jitter and toggle between levels at off-grid rates"
    requirement: MODEL-09
    verification:
      - kind: unit
        ref: "internal/model/s7_test.go#TestS7aFirmwareWindowKeepsSteps"
        status: pass
    human_judgment: false
  - id: D7
    description: "A global-source call of math/rand or math/rand/v2 under internal/model fails make check"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/lint/wallclock_test.go#TestGlobalRandSelfTest"
        status: pass
      - kind: unit
        ref: "internal/lint/wallclock_test.go#TestNoWallClock"
        status: pass
    human_judgment: false
  - id: D8
    description: "The 30 rpm step signature as the firmware and Home Assistant show it on the live contract bench, and the Rev 3.3 board constants (multiply 0.5, 1 s window)"
    requirement: MODEL-09
    verification: []
    human_judgment: true
    rationale: "Live S7b runs in 04-10 through aioesphomeapi; A11 (Rev 3.3 pulse_counter constants) needs hardware in Phase 7/8 and is not verifiable in this plan"

duration: 7min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 04: Pulse Tach, Seeded Noise and Spike S7a Summary

**A schema 2 fan with `[tach]` now sends the firmware pulse-quantised rpm frames (600 rpm per pulse per 50 ms at ppr 2, so the firmware's 1 s mean steps by 30 rpm) with splitmix64 keyed-hash noise whose `noise_rpm` is the std of a 1 s reading, independent of tick timing; every plant has a 1 s gate `Reading`, and hermetic spike S7a proves the steps survive the firmware's time-weighted window.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-11-11T05:54:10Z
- **Completed:** 2026-11-11T06:01:19Z (code); SUMMARY follows
- **Tasks:** 2 (TDD: 2 RED + 3 GREEN/test commits)
- **Files modified:** 8 (5 created, 3 modified)

## Accomplishments

- `model.Tach` pulse accumulator: the tach path of `Plant.Step` splits dt at 100 ms bucket edges, feeds `advance`'s exact `Integral.Area` into a phase accumulator and counts whole pulses; `Frame` drains them as `pulses*60/(2*elapsed)`, `Current` returns the last value without draining. Protocol 1, `rpmFrameStep` (50 ms) and the C++ side are untouched (`git diff --stat v0.1.0..HEAD -- protocol/ components/` is empty).
- `Plant.Reading` for every fan on a whole-second sim-time gate: 30 x pulses for a tach fan, the exact time-weighted shaft mean for an ideal-tach fan (D-13 as amended, A5), unavailable only in the first second. The ideal path keeps its single `advance` call, so `TestPlantMatchesV010ForLegacyParams` stays green with gate edges inside 7 ms, 30 ms and 1 s steps.
- Noise: `z(seed, port, bucket)` (Irwin-Hall over splitmix64), scaled by `noiseScale = sqrt(10)` and applied over `MovingS` of each bucket piece. On a flat 1200 rpm fan `noise_rpm` 20 gives a std of 22-24 rpm over 600 gate readings on five levels (seeds 1, 2, 7, 42: 23.1, 23.9, 22.4, 22.1); `noise_rpm` 0 gives the single level 1200.
- Lint: `checkGlobalRand` plus `TestGlobalRandSelfTest`; `TestNoWallClock` applies it to every `.go` file under `internal/model` (verified to fail on a probe file with `rand.Intn`).

## S7a result (spike, hermetic emulation of sim_sensor.cpp arithmetic, integer ms)

Steady rpm at ppr 2, noise 0, TauUp 0.05, 40 window phases (0..975 ms step 25) x 60 readings per phase (first two dropped), frames from the real plant at 50 ms with the engine's change-only rule. All gates pass at every rpm and jitter (nothing loosened).

| rpm | jitter ms | bias rpm | spread min..max rpm | levels (max per phase) | residues mod 30 at J=0 | min toggle share | on-grid fraction |
|-----|-----------|----------|---------------------|------------------------|------------------------|------------------|------------------|
| 500 | 0 | +0.00 | 15.0..30.0 | 2 | 2 | 0.33 | 0.83 |
| 500 | 2 | +0.02 | 17.4..34.2 | 14 | - | 0.32 | 0.25 |
| 500 | 5 | +0.04 | 21.0..41.4 | 30 | - | 0.32 | 0.09 |
| 500 | 10 | -0.08 | 27.6..58.2 | 42 | - | 0.33 | 0.05 |
| 1000 | 0 | -0.00 | 15.0..30.0 | 2 | 2 | 0.33 | 0.67 |
| 1000 | 2 | -0.00 | 18.0..36.0 | 19 | - | 0.33 | 0.14 |
| 1000 | 5 | -0.15 | 24.0..48.6 | 36 | - | 0.33 | 0.05 |
| 1000 | 10 | +0.07 | 34.8..61.2 | 46 | - | 0.35 | 0.02 |
| 1100 | 0 | +0.00 | 15.0..30.0 | 2 | 2 | 0.33 | 0.83 |
| 1100 | 2 | +0.02 | 17.4..34.2 | 14 | - | 0.32 | 0.25 |
| 1100 | 5 | +0.04 | 21.0..41.4 | 30 | - | 0.32 | 0.09 |
| 1100 | 10 | -0.08 | 27.6..58.2 | 42 | - | 0.33 | 0.05 |
| 1500 | 0 | +0.00 | 0.0..0.0 | 1 | 1 | - | 1.00 |
| 1500 | 2 | +0.01 | 4.8..9.0 | 14 | - | - | 0.18 |
| 1500 | 5 | +0.19 | 12.6..21.6 | 27 | - | - | 0.06 |
| 1500 | 10 | -0.13 | 27.6..45.6 | 41 | - | - | 0.03 |
| 1800 | 0..10 | +0.00 | 0.0..0.0 | 1 | 1 (J=0) | - | 1.00 |

Gate results: (a) bias at J <= 2 is within 0.02 rpm (limit 1); (b) worst spread 48.6 rpm at J = 5 (limit 60) and 61.2 rpm at J = 10 (limit 90); (c) 1100 rpm shows up to 42 distinct readings (>= 2); (d) lattice: at most 2 residues mod 30 at J = 0 for every phase, 0 always among them (limit 3); (e) toggle at 500/1000/1100: minimum peak-to-peak 15.0 rpm (limit 12), minimum smaller-side share 0.32 (limit 0.25). 1500 and 1800 rpm put whole pulses into every 1 s window (1800 rpm is one constant frame, so jitter has nothing to blur) and are exempt from (e). The on-grid fraction is reported only (RESEARCH Pitfall 1). The frame cadence did not need grid alignment (criterion (b) holds), so the fallback in the plan was not triggered. The numbers match the plan-revision scratch model (minimum spread 15, minimum share 0.33).

## Task Commits

1. **Task 1 (tracer): a tach fan's frames carry pulse-quantised values**
   - RED `fd6fe43` (test): tests plus a compiling stub of `Plant.Reading`; 7 model tests and `TestEngineTachFrames` failed on their assertions (pulses 0, Reading unavailable, only 3 frames in 10 s)
   - GREEN `d930c72` (feat): tach path, Frame/Current/Reading, ideal-tach gate area, engine doc comment, `TestFrameAfterGapIsMeanRate`
2. **Task 2: seeded noise, S7a, global rand lint**
   - RED `3879596` (test): `z` and `checkGlobalRand` stubs; `TestNoiseMoments`, `TestNoiseIsKeyed`, `TestNoiseAmplitudeIsGateStd`, `TestSeedChangesTheTrace`, `TestGlobalRandSelfTest` failed on assertions
   - GREEN `b71eba6` (feat): `noise.go`, noise term in the tach path, `checkGlobalRand` and its wiring into `TestNoWallClock`
   - `58d9ad1` (test): `TestTachDutyScriptOnCommonGridIsTickInvariant` for the EDGE-MODEL-10 review

**Plan metadata:** committed separately (docs: complete plan)

## Files Created/Modified

- `internal/model/tach.go` - `BoardPPR`, `TachGate`, `NoiseBucket`, the `Tach` accumulator and gate state
- `internal/model/noise.go` - `mix`, `unit`, `z`, `noiseScale`
- `internal/model/plant.go` - `stepIdeal`, `stepTach`, `Frame`, `Current`, `Reading`
- `internal/engine/engine.go` - doc comment at `sendRPM` only
- `internal/model/tach_test.go`, `internal/model/s7_test.go`, `internal/engine/tach_test.go` - the plan's test lists plus the two edge-review tests
- `internal/lint/wallclock_test.go` - `checkGlobalRand`, fixtures, `TestGlobalRandSelfTest`, `TestNoWallClock` extension

## Decisions Made

- `floor(phase + 1e-9)` for the pulse count: a fan on the pulse grid (1200 rpm = 40 pulses/s, noise 0) must read the single level 1200 on every tick pattern, and plain `floor` flips with float rounding at gate edges.
- `checkGlobalRand` flags any non-constructor, non-type name of the rand packages, not only calls, so function values cannot smuggle the global source in.
- S7a models the engine's change-only rule and keeps the firmware publishing after the last frame (otherwise a constant rate has no readings).

## Review of the flagged edge assumptions (resolved at execution, user decision 2026-11-11)

- **EDGE-MODEL-09 (bus disconnect mid-window): accepted, bounded, no code change.** The engine sends no frames while the bus is down, so `nextRPM` is stale on reconnect and the first `Frame` covers the whole gap; it is normalised by the elapsed plant time, so it is the true mean pulse rate over the gap (resolution of one pulse over the gap), never a burst or a zero (`TestFrameAfterGapIsMeanRate`, 7 s gap, mean 1100 within 30/7 rpm). The hello in-snapshot carries the last pre-gap value (`Current`, no drain, `TestCurrentDoesNotDrain`, `TestEngineTachFrames`). Residual: if the fan changed speed during the gap, that one 50 ms frame (5 % of the firmware's next window) shows the gap mean rather than the current rate. A possible later tightening (re-align `lastFrameT` at hello) is an engine API change outside this plan.
- **EDGE-MODEL-10 (duty change lands on the observing tick): accepted, as designed.** Invariance is proven for constant duty on every tick pattern (`TestTachIgnoresTickTiming`) and for a duty script on a common time grid (`TestTachDutyScriptOnCommonGridIsTickInvariant`, step at 10 s; 10 ms, 20 ms, 250 ms and 1 s steps give identical readings and pulses). A duty change at different tick offsets legitimately differs, exactly as in v0.1.0 for the ideal path.
- **A4 (resolved 2026-11-10): confirmed by test.** `noise_rpm` 20 gives a gate-reading std of about 23 rpm on five levels (`TestNoiseAmplitudeIsGateStd`).
- **A5 (resolved 2026-11-11): implemented.** Ideal-tach `Reading` is the exact 1 s shaft mean on the same gate, unavailable only in the first second (`TestIdealReading`).
- **A11 (unresolved, carried to Phase 7/8): unchanged.** `BoardPPR = 2` and the 1 s gate follow the Rev 1.0-3.1 YAML and `sim_sensor.h`; the Rev 3.3 YAML has no `pulse_counter`. A different multiplier or window is a one-line change to `BoardPPR`/`TachGate` plus the S7 thresholds.

## Deviations from Plan

None - plan executed exactly as written.

Notes (not deviations): the plan's `TestTachIgnoresTickTiming` pattern list was extended with a 3 s step and a reordered 3/30/10 ms cycle (more coverage); two extra tests were added for the edge review above. In the Task 2 RED commit `TestS7aFirmwareWindowKeepsSteps`, `TestNoPulsesAtRest` and `TestTachIgnoresTickTiming` already passed (S7a exercises the Task 1 plant; the other two hold trivially while noise is a zero stub), so only the noise, seed and lint tests were the intentional REDs.

## TDD Gate Compliance

Task 1 and Task 2 each have a `test(04-04)` RED commit (`fd6fe43`, `3879596`) before the `feat(04-04)` GREEN commit (`d930c72`, `b71eba6`). No refactor commit was needed. `workflow.tdd_mode` is off, so no `check tdd-red-evidence` record was persisted; the RED failures are the assertion failures listed under Task Commits.

## Issues Encountered

- First S7a run failed with "0 readings" at 1800 rpm: with change-only frames a constant 3-pulses-per-frame rate sends one frame, so the emulation stopped flushing updates. The emulation now keeps publishing to the end of the run, as the firmware does. Test code only.

## Known Stubs

None. The RED-phase stubs (`z`, `checkGlobalRand`, `Plant.Reading`) were replaced in the GREEN commits.

## Threat Flags

None. No new network endpoint, auth path or trust boundary; T-04-11 (frame cadence) is mitigated by the unchanged 50 ms cadence and change-only rule (`rpmFrameStep` grep still matches); T-04-12 accepted (the hash is test noise).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-05 can read `Config.Seed` as before; the plants already take it. 04-06 can expose `Plant.Reading` as `tach_rpm` (never null after the first second, every fan). 04-08 ships `noise_rpm` 20 in the default fan.
- 04-10 runs the live S7b on the contract bench; its (d)/(e) gates start from the modelled pass above.
- Open: A11 (Rev 3.3 tach constants) stays with Phase 7/8.

## Self-Check: PASSED

All created files exist on disk, all five task commits are in `git log`, `make check` exits 0, `rpmFrameStep = 50 * time.Millisecond` still matches, and `git diff --stat v0.1.0..HEAD -- protocol/ components/` is empty.

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

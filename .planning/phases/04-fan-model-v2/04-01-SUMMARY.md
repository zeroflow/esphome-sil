---
phase: 04-fan-model-v2
plan: 01
subsystem: model
tags: [go, fan-model, fan.toml, schema-2, provenance, toml, curves]

requires:
  - phase: 03-live-web-ui
    provides: engine, status and watch paths that read the fan model
provides:
  - model.Curve/Curves with clamped piecewise-linear At (v0.1.0 operation order) and legacy curve synthesis
  - model.Plant, the engine's only seam into the model (NewPlant, Step, Shaft, Running, Params, Frame, Current)
  - fanlib schema probe and strict schema 2 loader with per-value provenance (rawV2, loadV2, validateV2)
  - sticky-direction choice between curve.up and curve.down
  - testdata/fans-v2/curve-test.toml fixture and a real-firmware up/down sweep test
affects: [04-02, 04-03, 04-04, 04-05, 04-06, 04-07, 04-08, 04-09, 04-10]

plan_head_before: b2a217d296b6d6b5c01bde9323f0e15c81ee2e0b
plan_head_after: fc276464b1f848920cab16be45c4358207f2ca3b

actuals:
  tokens: 14700
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Zero value of every schema 2 Params field is the v0.1.0 behaviour; legacy fields are also filled by schema 2 files"
    - "Engine reads the model only through model.Plant"
    - "Lenient schema probe selects a strict decoder (schema 1 flat, schema 2 tables)"

key-files:
  created:
    - internal/model/curve.go
    - internal/model/plant.go
    - internal/model/curve_test.go
    - internal/fanlib/v2.go
    - internal/fanlib/v2_test.go
    - internal/engine/curve_test.go
    - testdata/fans-v2/curve-test.toml
    - e2e/model_v2_test.go
  modified:
    - internal/model/fan.go
    - internal/fanlib/fanlib.go
    - internal/fanlib/fanlib_test.go
    - internal/engine/engine.go

key-decisions:
  - "Direction (LastDuty, Falling) is tracked in State only for a fan with a separate down curve, so a legacy fan's State is exactly the v0.1.0 State and fan_test.go passes unmodified"
  - "Schema 2 loader errors use a table-aware lineOf (dotted key in its table, header line for a table value) instead of the first-match keyLine, because duty and rpm repeat across tables"
  - "Provenance entry typos are reported before missing entries, so the root cause is not hidden behind the value that now lacks an entry"
  - "Params is no longer comparable with ==; tests use reflect.DeepEqual"

patterns-established:
  - "Curve.At: a.RPM + (duty-a.Duty)/(b.Duty-a.Duty)*(b.RPM-a.RPM), clamped at both ends, never extrapolates"
  - "Provenance entry value is kind or kind:source_id; kinds in model.Kinds, ids in [sources]"

requirements-completed: [MODEL-05, MODEL-08]

coverage:
  - id: D1
    description: "A schema 2 fan file loads through fanlib.Load into model.Params (Schema 2, up and down curves, thresholds, taus, MaxRPM/MinRPM from the curves, derived BelowMin, provenance in ValuePaths order)"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2LoadsIntoParams"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2NeverStopsFormDerivesHold"
        status: pass
    human_judgment: false
  - id: D2
    description: "Schema 2 loading is strict: unknown keys (including a v1 source string and a start-kick key) are located, missing keys are named, curve shape, scalar ranges, provenance completeness, kinds and source ids are enforced, schema other than 2 is rejected"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2UnknownKeysAreLocated"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2MissingKeysAreNamed"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2ProvenanceRules"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2CurveShapeRules"
        status: pass
      - kind: unit
        ref: "internal/fanlib/v2_test.go#TestV2SchemaProbe"
        status: pass
    human_judgment: false
  - id: D3
    description: "Piecewise-linear interpolation between table points, clamped at both ends and monotone; the synthesised legacy curve is bit-identical to the v0.1.0 targets"
    requirement: MODEL-05
    verification:
      - kind: unit
        ref: "internal/model/curve_test.go#TestCurveAtInterpolatesAndClamps"
        status: pass
      - kind: unit
        ref: "internal/model/curve_test.go#TestCurveAtIsMonotone"
        status: pass
      - kind: unit
        ref: "internal/model/curve_test.go#TestLegacyCurveIsBitIdentical"
        status: pass
      - kind: unit
        ref: "internal/model/fan_test.go (unmodified v0.1.0 guard, git diff v0.1.0 empty)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The model follows the up curve while the duty rises and the down curve after it falls (sticky direction); the engine drives a port's rpm, bus frames and events through model.Plant"
    requirement: MODEL-05
    verification:
      - kind: unit
        ref: "internal/model/curve_test.go#TestDirectionPicksCurve"
        status: pass
      - kind: unit
        ref: "internal/model/curve_test.go#TestStepStateCarriesDirection"
        status: pass
      - kind: integration
        ref: "internal/engine/curve_test.go#TestEngineCurveFan"
        status: pass
    human_judgment: false
  - id: D5
    description: "Sweeping Fan 1 of the real firmware up then down through its fan entity shows a lower rpm on the way down than on the way up at the same duty in sil status and sil watch (roadmap criterion 1 on a fixture fan)"
    requirement: MODEL-05
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestModelV2Curves (e2e/model_v2_test.go#TestModelV2Curves)"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 01: Curves Tracer Summary

**Schema 2 fan files with separate up and down duty to RPM curves and per-value provenance load strictly through fanlib, drive a port through a new model.Plant seam in the engine, and show hysteresis on the real firmware (600/1000/1150/1300/1550 rpm up, 1200/900 down at the same duties).**

## Performance

- **Duration:** about 20 min (start time not recorded at launch; first file written 05:26Z)
- **Completed:** 2026-11-11T05:33Z
- **Tasks:** 2 (1 tracer, 1 TDD)
- **Files modified:** 12 (8 created, 4 modified)

## Accomplishments

- One path from `fan.toml` to the bus: `fanlib.loadV2` produces `model.Params` with `Curve.Up/Down`, the engine steps a `model.Plant` every 10 ms tick, `Frame()` feeds the 50 ms `port<N>.rpm` frames, `Current()` the hello in-snapshot and `Shaft()` status, events and samples.
- Schema 1 fans are unchanged: their running target comes from a synthesised legacy curve with the v0.1.0 operation order. `TestLegacyCurveIsBitIdentical` compares 10 001 duties per fan with `==`, and `internal/model/fan_test.go` is byte-identical to v0.1.0.
- Strict schema 2 loading: unknown keys located (go-toml position), named missing keys, finite numbers, curve shape (>= 2 points, strictly increasing duty, non-decreasing rpm), D-10 floor rule, and complete provenance (kind known, source id defined, one entry per present value, no entry for an absent value).
- Real-firmware proof: `TestModelV2Curves` sweeps Fan 1 up through speeds 20, 40, 50, 60, 80 and down to 60, 40; rpm at speed 60 is 1299.29 up and 1200.96 down; the watch stream carried the rpm event after the fall.

## Task Commits

1. **Task 1: schema 2 curve fan drives a port through the Plant seam (tracer)** - `18d6530` (feat)
2. **Task 2 RED: failing direction tests plus loader and sweep tests** - `18108cb` (test)
3. **Task 2 GREEN: sticky direction picks curve.up or curve.down** - `fc27646` (feat)

**Plan metadata:** committed with this SUMMARY (docs).

The tracer feedback gate (auto mode) re-ran the tracer `<verify>` (`make test-pkg` on model, fanlib, engine, then `make check`) and passed before Task 2 began.

## Files Created/Modified

- `internal/model/curve.go` - Point, Curve, Curves, `Curve.At/Max/Min`, `Params.upCurve/downCurve` with the legacy synthesis (hold fan with MinDuty > 0 gets (0, MinRPM))
- `internal/model/plant.go` - `Plant` and its seam methods; seed, port and plant time are held for 04-04
- `internal/model/fan.go` - schema 2 Params fields, TachParams, Datasheet, ProvEntry, Kinds, ValuePaths, State fields, curve-driven and direction-aware Step
- `internal/fanlib/fanlib.go` - lenient schema probe and dispatch
- `internal/fanlib/v2.go` - rawV2, loadV2, validateV2, checkCurve, checkProvenance, table-aware lineOf
- `internal/engine/engine.go` - `portRuntime.plant`, `Config.Seed`, `plant.Step`, `Frame`, `Current`
- `testdata/fans-v2/curve-test.toml` - fixture with distinct up and down curves, ideal tach
- tests: `internal/model/curve_test.go`, `internal/fanlib/v2_test.go`, `internal/engine/curve_test.go`, `e2e/model_v2_test.go`

## Decisions Made

- A1 direction rule implemented as planned (sticky sign of the last duty change, initial branch up, no deadband), with the tracking restricted as described under deviation 1.
- `lineOf` is table-aware (see deviation 3); 04-03 owns hardening and may keep or extend it.
- `Plant.Params()` returns a copy of the Params the plant was built with; the engine still keeps `cfg.Fan` for FanInfo-style readers.

## Edge Assumption Review (execution-time, user decision 2026-11-11)

- **EDGE-MODEL-05-unclassified** (reversal inside one table segment): reviewed against the listed coverage. `dirFan` in `curve_test.go` has a single segment ([0.1, 1]) and `TestDirectionPicksCurve` runs rise to 0.5, fall to 0.3 and rise to 0.5 inside it; each settles on its own branch. On a reversal the target switches branch instantly at the current duty and the shaft rpm moves continuously toward it with tau_up/tau_down, so there is no rpm jump and no extrapolation. Resolved: behaviour accepted as designed.
- **A1** (sticky direction, no deadband): reviewed. Residual sensitivity: duty jitter of a few thousandths flips the branch every step, so the target alternates between the two curves and the tau low-pass settles between them (bounded by the gap between the curves). That is acceptable for the simulator now. Status stays unresolved for real fans: carried to Phase 8, where measured sweeps decide whether a deadband is needed (cost: one model iteration).
- **A3**: already resolved by the user on 2026-11-10; implemented (legacy synthesis, derived BelowMin, hold iff stop.duty == 0).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Direction tracking only for fans with a down curve**
- **Found during:** Task 2 (GREEN design)
- **Issue:** The plan has Step return a State that always carries LastDuty and Falling. `TestTauSelectionEqualTargetLeavesRPMUnchanged` in the unmodified `fan_test.go` compares `got != s` with `s = State{Running: true, RPM: target}`, so writing LastDuty = 0.5 into the state would fail that guard test, contradicting "fan_test.go passes unmodified".
- **Fix:** Step tracks and writes LastDuty and Falling only when `len(p.Curve.Down) > 0`. A fan with no down curve has no direction to track (both branches are the same curve), so its State stays bit-identical to v0.1.0. `TestStepStateCarriesDirection` covers both cases.
- **Files modified:** internal/model/fan.go, internal/model/curve_test.go
- **Committed in:** `fc27646`

**2. [Rule 3 - Blocking] Params no longer comparable**
- **Found during:** Task 1
- **Issue:** `Params` now holds slices and a map, so `fanlib_test.go` `TestLoadValidFile` (`p != want`) no longer compiles.
- **Fix:** compare with `reflect.DeepEqual` (assertion unchanged). `fan_test.go` was not touched.
- **Files modified:** internal/fanlib/fanlib_test.go
- **Committed in:** `18d6530`

**3. [Rule 1 - Bug] Table-aware error lines for schema 2**
- **Found during:** Task 1 (validateV2)
- **Issue:** the plan says errors take "the line keyLine finds", but keyLine returns the first `key =` anywhere, and `duty` and `rpm` repeat in [stop], [start], [curve.up] and [curve.down], so the reported line would be wrong (e.g. a bad curve.up.duty pointing at [stop]).
- **Fix:** small `lineOf` (dotted key within its table, table header for a table value, keyLine fallback) and `entryLine` for [provenance] entries. Schema 1 keeps keyLine untouched. Plan 04-03 still owns range caps and the [tach]/[datasheet]/[measured] mapping.
- **Files modified:** internal/fanlib/v2.go
- **Committed in:** `18d6530`

**4. [Rule 3 - Blocking] Test harness order in TestEngineCurveFan**
- **Found during:** Task 1
- **Issue:** `newHarness` loads the default fan through fanlib, so setting `SIL_FAN_DIR` before it made the harness fail to find the Arctic.
- **Fix:** `newHarness` first, then `t.Setenv(fanlib.EnvFanDir, ...)`, then replug (which loads at call time).
- **Files modified:** internal/engine/curve_test.go
- **Committed in:** `18d6530`

---

**Total deviations:** 4 auto-fixed (2 Rule 1, 2 Rule 3)
**Impact on plan:** none on scope. Deviation 1 keeps the v0.1.0 regression guard honest; 2 to 4 are mechanical.

## TDD Gate Compliance

Task 2 is the only `tdd="true"` task. RED commit `18108cb` `test(04-01)` precedes GREEN commit `fc27646` `feat(04-01)`. Because the tracer (Task 1) already shipped the loader and `Curve.At`, only the new behaviour was red at that point: `TestDirectionPicksCurve` and `TestStepStateCarriesDirection` failed on their assertions for the planned behaviour (rpm stayed on the up curve; `LastDuty` and `Falling` never written). The curve, legacy bit-identity and `TestV2*` loader tests passed at RED by design, as they exercise tracer code; they are regression and acceptance tests for it, not a GREEN gate violation. The `gsd_run check tdd-red-evidence` record was not produced separately; the failing output is as listed above. No REFACTOR commit.

## Issues Encountered

None beyond the deviations above.

## Known Stubs

Intentional unwired interface fields, each owned by a named later plan; none blocks this plan's goal and none flows into the UI:

- `Params.StartDelay`, `Params.CoastTau` are loaded but have no behaviour yet (04-02).
- `Params.Tach`, `Params.Datasheet`: the loader decodes `[tach]`, `[datasheet]` and `[measured]` and counts them for provenance but does not map them into Params (04-03); the tach has no effect until 04-04.
- `Plant.Frame()` and `Plant.Current()` return the shaft rpm; the pulse accumulator lands in 04-04. `Config.Seed` has no effect until then and is set by the supervisor in 04-05.
- `State.StartElapsed` is declared, never written (04-02).

## Threat Flags

None. The only new surface is the schema 2 file loader, covered by T-04-01..T-04-03 (finite numbers, strictly increasing duty so no zero-width division, strict decode with provenance never defaulted).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-02 (start delay, coast, v0.1.0 equivalence) and 04-03 (provenance in `sil fans`, loader hardening) can run in parallel: every new Params and State field is declared, and they touch different files.
- 04-03 should look at `lineOf` before adding its own table-aware helper.
- Verification run: `make check` green, `make e2e E2E_RUN=TestModelV2Curves` green (37 s), `git diff v0.1.0 -- internal/model/fan_test.go` empty, `git diff --stat v0.1.0..HEAD -- protocol/ components/` empty.

## Self-Check: PASSED

All created files exist on disk; commits `18d6530`, `18108cb`, `fc27646` exist; `p.plant.Step(` and `p.plant.Frame()` each match one line in engine.go; `curve-test.toml` has exactly one `schema = 2` line; the acceptance-criteria test commands exit 0 without "no tests to run".

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

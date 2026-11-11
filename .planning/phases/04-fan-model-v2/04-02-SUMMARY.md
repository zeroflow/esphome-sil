---
phase: 04-fan-model-v2
plan: 02
subsystem: model
tags: [go, fan-model, start-delay, coast, step-integral, v0.1.0-equivalence, fixtures]

requires:
  - phase: 04-fan-model-v2
    provides: "04-01 Plant seam, curve-driven Step, State.StartElapsed and Params.StartDelay/CoastTau declared and loaded"
provides:
  - "model.advance and model.Integral: Step with the exact rpm integral of the step (Area in rpm*s, MovingS), the base 04-04's pulse accumulator counts from"
  - "start delay: a stopped fan starts after duty has stayed at or above start.duty for start.delay_s, the step split at the expiry, a dip re-arms it"
  - "immediate stop below stop.duty and coast-down with coast_tau_s (tau_down_s when zero)"
  - "frozen v0.1.0 Step reference and 2000-set differential proof that schema 1 fans are bit-identical"
  - "testdata/fans-v0.1.0 byte copies of the two v0.1.0 fan files, sha256 pinned, and testdata/fans-v2/startstop-test.toml"
affects: [04-04, 04-08, 04-09]

plan_head_before: f7dd8e604c0f7ac6032cac3ca6eeff42cea68911
plan_head_after: c777f392b2227b07fdf93ada71e37d9907378bfc

actuals:
  tokens: 8800
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "advance = per-segment exponential (v0.1.0 expression unchanged) + exact analytic integral; a zero-length segment is never applied"
    - "Integral of a snapped segment stops at the analytic crossing time tau*ln(rpm0/0.5), so the area does not depend on tick boundaries"
    - "Frozen reference model in a _test file compared with == on randomised legacy scripts (rand.New(rand.NewPCG(...)), never the global source)"

key-files:
  created:
    - internal/model/startstop_test.go
    - internal/model/fan_v010_test.go
    - internal/engine/startstop_test.go
    - internal/fanlib/fixtures_test.go
    - testdata/fans-v2/startstop-test.toml
    - testdata/fans-v0.1.0/arctic-p12-pwm-pst.toml
    - testdata/fans-v0.1.0/noctua-nf-a12x25-pwm.toml
  modified:
    - internal/model/fan.go

key-decisions:
  - "Start boundary is at or above (duty >= StartDuty, the v0.1.0 operator) and the stop boundary is duty < MinDuty; both exercised exactly at the boundary and at +-1e-9"
  - "A step that contains the start-delay expiry is split there; the part after is applied only when it has positive length, so a delay that expires exactly on a tick leaves rpm 0 until the next tick"
  - "Coast tau applies only to a segment that is not running; a running fan slowing down (duty in the band or a lower curve value) still uses tau_down_s"
  - "Snap stays at segment end as in v0.1.0; only the integral uses the analytic crossing time"

patterns-established:
  - "Plan 04-04 integrates advance's Integral (Area/60 = revolutions) rather than rectangle sums"
  - "Exact-rpm tests of later plans point SIL_FAN_DIR at testdata/fans-v0.1.0 and rely on the pinned hashes"

requirements-completed: [MODEL-06, MODEL-07, MODEL-08]

coverage:
  - id: D1
    description: "A schema 2 fan stays at exactly 0 rpm until its duty has been at or above start.duty for start.delay_s, then spins up from that instant; a dip below start.duty re-arms the delay"
    requirement: MODEL-06
    verification:
      - kind: unit
        ref: "internal/model/startstop_test.go#TestStartDelaySplitsTheStep"
        status: pass
      - kind: unit
        ref: "internal/model/startstop_test.go#TestStartDelayRearms"
        status: pass
      - kind: integration
        ref: "internal/engine/startstop_test.go#TestEngineStartDelayAndCoast"
        status: pass
    human_judgment: false
  - id: D2
    description: "The start boundary is at or above: duty == start.duty starts a stopped fan after the delay (at once with delay 0), start.duty - 1e-9 never starts it; duty == stop.duty keeps a running fan running and only below it stops"
    requirement: MODEL-06
    verification:
      - kind: unit
        ref: "internal/model/startstop_test.go#TestStartAtExactStartDuty"
        status: pass
      - kind: unit
        ref: "internal/model/startstop_test.go#TestZeroDelayIsV010Order"
        status: pass
    human_judgment: false
  - id: D3
    description: "A fan below stop.duty counts as stopped at once and coasts down with coast_tau_s (not tau_down_s); duty back at or above start.duty re-arms the delay and the fan re-accelerates from its coast rpm; the band keeps the previous state"
    requirement: MODEL-07
    verification:
      - kind: unit
        ref: "internal/model/startstop_test.go#TestStopIsImmediateAndCoasts"
        status: pass
      - kind: unit
        ref: "internal/model/startstop_test.go#TestCoastReArmsAndReaccelerates"
        status: pass
      - kind: unit
        ref: "internal/model/startstop_test.go#TestBandKeepsState"
        status: pass
      - kind: integration
        ref: "internal/engine/startstop_test.go#TestEngineStartDelayAndCoast"
        status: pass
    human_judgment: false
  - id: D4
    description: "The never-stops form (stop.duty 0 plus a curve point at duty 0) runs from plant creation and holds its floor rpm at 0 % duty, with the identical rpm trace to the v1 hold fan"
    requirement: MODEL-07
    verification:
      - kind: unit
        ref: "internal/model/fan_v010_test.go#TestNeverStopsForm"
        status: pass
    human_judgment: false
  - id: D5
    description: "Steps compose (ten 100 ms equal one 1 s, also across a delay expiry and a coast snap) and the exact rpm integral matches a 10 000-substep sum within 1e-6 relative"
    requirement: MODEL-06
    verification:
      - kind: unit
        ref: "internal/model/startstop_test.go#TestComposableAcrossDelayAndSnap"
        status: pass
      - kind: unit
        ref: "internal/model/startstop_test.go#TestAdvanceIntegralIsExact"
        status: pass
    human_judgment: false
  - id: D6
    description: "Every v0.1.0 fan file behaves exactly as before: 2000 random legacy parameter sets x 400 steps give == RPM and Running against a frozen copy of the v0.1.0 Step, through Step and through Plant; the v0.1.0 files are kept byte for byte, pinned by sha256 and load as the v0.1.0 parameters"
    requirement: MODEL-08
    verification:
      - kind: unit
        ref: "internal/model/fan_v010_test.go#TestStepMatchesV010"
        status: pass
      - kind: unit
        ref: "internal/model/fan_v010_test.go#TestPlantMatchesV010ForLegacyParams"
        status: pass
      - kind: unit
        ref: "internal/fanlib/fixtures_test.go#TestV010FixturesArePinned"
        status: pass
      - kind: unit
        ref: "internal/fanlib/fixtures_test.go#TestV010FixturesLoadAsV010"
        status: pass
      - kind: unit
        ref: "internal/model/fan_test.go (unmodified v0.1.0 guard, git diff v0.1.0 empty)"
        status: pass
    human_judgment: false

duration: 3 min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 02: Start/Stop Behaviour and v0.1.0 Equivalence Summary

**A schema 2 fan now waits its start delay (at or above start.duty), stops at once below stop.duty and coasts with coast_tau_s, with an exact per-step rpm integral for the tach, while a frozen v0.1.0 Step and pinned fixtures prove every schema 1 fan is bit-identical to before.**

## Performance

- **Duration:** 3 min (2026-11-11T05:37Z to 05:40Z)
- **Tasks:** 2 (1 tracer, 1 auto/TDD)
- **Files modified:** 8 (7 created, 1 modified)

## Accomplishments

- `Step` is now `advance` minus the integral. `advance` splits the step at the start-delay expiry (stopped part first, running part after), never applies a zero-length segment (so zero delay is exactly the v0.1.0 order), selects tau per segment (`tau_up_s` rising, `tau_down_s` when running and falling, `coast_tau_s` when stopped and falling) and keeps the v0.1.0 exponential expression and end-of-segment snap verbatim.
- Exact integral: `Integral.Area` (rpm*s) is the analytic integral of the exponential per segment; a segment that snapped integrates only up to `tau*ln(rpm0/0.5)`, so the area is independent of tick boundaries. `MovingS` is the part of the step with rpm > 0. Matches a 10 000-substep midpoint sum within 1e-6 relative across a rising step, a falling step, a delay expiry, a coast while waiting and a snap.
- Engine: with `startstop-test` on port 1, the `running` state event comes 400 ms (within one 10 ms tick) after the duty event, rpm is exactly 0 at 390 ms, the fan settles near 440 rpm (curve.up at 0.12), duty 0.03 gives a `stopped` event within one tick, and the rpm 1 s later is e^(-1/3) of the rpm at the stop (not e^(-1/0.8)).
- v0.1.0 equivalence: `stepV010` (verbatim copy, own types) vs `Step` on 2000 random legacy parameter sets x 400-step scripts (edge duties at +-1e-9 of the thresholds, NaN, out-of-range, dt 0 and negative) plus the degenerate tau 0 case, with == on RPM and Running; the same through `NewPlant(...).Step`. `internal/model/fan_test.go` is still byte-identical to v0.1.0.
- `testdata/fans-v0.1.0` holds the two v0.1.0 files byte for byte; `TestV010FixturesArePinned` fails on any byte change or extra file, `TestV010FixturesLoadAsV010` on any parameter change.

## Task Commits

1. **Task 1 RED: failing start delay, coast and step integral tests (tracer)** - `b84137c` (test)
2. **Task 1 GREEN: start delay, immediate stop and coast tau** - `119683c` (feat)
3. **Task 2: frozen v0.1.0 reference, never-stops form, pinned fixtures** - `c777f39` (test)

**Plan metadata:** committed with this SUMMARY (docs).

The tracer feedback gate (auto mode) re-ran the tracer `<verify>` (`make test-pkg` on model and engine, then `make check`) after GREEN: passed, so expansion went ahead (`Tracer verified end-to-end - expanding`).

## Files Created/Modified

- `internal/model/fan.go` - `Integral`, `Step` (wrapper), `advance`, `segment`; rewritten `Step` doc comment as the schema 2 semantics list
- `internal/model/startstop_test.go` - delay, re-arm, boundary, coast, band, composability and integral tests
- `internal/engine/startstop_test.go` - `TestEngineStartDelayAndCoast`
- `internal/model/fan_v010_test.go` - frozen `stepV010`, differential tests, `TestNeverStopsForm`
- `internal/fanlib/fixtures_test.go` - fixture pin and load tests
- `testdata/fans-v2/startstop-test.toml` - 0.4 s delay, 3 s coast, up curve only
- `testdata/fans-v0.1.0/{arctic-p12-pwm-pst,noctua-nf-a12x25-pwm}.toml` - byte copies

## Edge Assumption Review (execution-time, user decision 2026-11-11)

- **EDGE-MODEL-06-unclassified** (start boundary): already resolved by the user (at or above). Implemented with the v0.1.0 operators and covered by `TestStartAtExactStartDuty` (exact start.duty after 39 and 40 steps, start.duty - 1e-9 for 2 s, zero delay, exact stop.duty, stop.duty - 1e-9) and by `TestStepMatchesV010`, which draws both thresholds and their +-1e-9 neighbours. Status: resolved.
- **EDGE-MODEL-07-unclassified** (coast-down with its own dynamics; never-stops holds the floor at 0 %): reviewed against the listed coverage. `coast_tau_s` is required by the loader (D-09, 04-01 `TestV2MissingKeysAreNamed`); coast applies only to a non-running segment (`TestStopIsImmediateAndCoasts` also asserts a running fan in the band slows with `tau_down_s`); the snap below 0.5 rpm is unchanged and composes (`TestComposableAcrossDelayAndSnap`, integral across a snap); the never-stops form runs from `Initial`, reaches 450 rpm within 1e-6 at duty 0 and gives a trace `==` to the v1 hold fan at duties 0, 0.1, 0.2 and 0.6. One consequence worth knowing: a never-stops fan never stops, so its `coast_tau_s` is required by the file but never used. Resolved: behaviour accepted as designed.
- **A3**: already resolved by the user on 2026-11-10; this plan supplies the trace-identity proof.

## Decisions Made

See `key-decisions` in the frontmatter. The segment helper keeps the v0.1.0 expression `rpm += (target - rpm) * (1 - math.Exp(-sec/tau))` unchanged (not `Expm1`) so legacy results stay bit-identical; `Expm1` is used only in the area, which has no legacy counterpart.

## Deviations from Plan

None - plan executed exactly as written.

## TDD Gate Compliance

- **Task 1 (tracer, tdd=true):** RED `b84137c` `test(04-02)` precedes GREEN `119683c` `feat(04-02)`. The RED commit carries an `advance`/`Integral` stub (returning `Step` and a zero Integral) so the tests compile; they then failed on their assertions for the planned behaviour (e.g. "after 390 ms: Running:true ... want stopped, rpm 0, StartElapsed 390 ms", "Area = 0, want 1009.58", engine "port 1 ... want stopped at exactly 0 rpm"). The separate `gsd_run check tdd-red-evidence` record was not produced; the failing output above is the evidence. No REFACTOR commit.
- **Task 2 (tdd=true):** no RED commit. Its tests (`TestStepMatchesV010`, `TestPlantMatchesV010ForLegacyParams`, `TestNeverStopsForm`, fixture tests) prove that behaviour Task 1 already implemented is unchanged for legacy files, so they pass on first run by design (the plan frames them as a proof, not a new feature). To rule out vacuous tests I mutation-checked fan.go: changing `duty >= p.StartDuty` to `>` made `TestStepMatchesV010` and `TestStartAtExactStartDuty` fail, and disabling the coast-tau branch made `TestStopIsImmediateAndCoasts` fail; both mutations were reverted (`git checkout -- internal/model/fan.go`) before the commit.

## Issues Encountered

None.

## Known Stubs

None. The RED-commit `advance` stub was replaced in the GREEN commit. `Plant.Frame()`/`Current()` still return the shaft rpm until 04-04 switches the tach path to `advance`'s `Integral`; that was already recorded in 04-01.

## Threat Flags

None. T-04-04 (fixture tampering) is mitigated by `TestV010FixturesArePinned`; T-04-05 (huge delay or coast tau) is accepted as planned (04-03 caps them at load time).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-04 can integrate `advance`'s `Integral` (`Area/60` revolutions, `MovingS`) for the pulse accumulator; `Plant.Step` still calls `Step`.
- 04-08, when it switches the shipped fan files, should point its exact-rpm tests at `testdata/fans-v0.1.0` via `SIL_FAN_DIR`.
- Verification run: `make check` green (119 C++ protocol checks and 66 txcap checks included), `git diff --exit-code v0.1.0 -- internal/model/fan_test.go` empty, `grep -n 'func advance' internal/model/fan.go` one match, `sha256sum` of the Arctic fixture equals the v0.1.0 digest, `ls testdata/fans-v0.1.0` lists exactly the two files.

## Self-Check: PASSED

All created files exist on disk; commits `b84137c`, `119683c`, `c777f39` exist; the acceptance-criteria test commands (`TestEngineStartDelayAndCoast`, `TestAdvanceIntegralIsExact`, `TestStartAtExactStartDuty`, `TestStepMatchesV010`, `TestV010FixturesArePinned`) exit 0 without "no tests to run".

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

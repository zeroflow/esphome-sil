---
phase: 01-closed-loop-fan-skeleton
plan: 05
subsystem: plant-model
tags: [go, fan-model, hysteresis, exponential-lag, go-toml, strict-decoding, fan-library]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 02)
    provides: "model.Params/State/Initial/Step signature, fanlib.Load/Source, embedded fans.FS, engine calling model.Step every tick"
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "sil up/status, e2e harness (newEnv, Env.Up, WaitStatus, APIClient), lifecycle.Up mapping fanlib errors to fan_toml_invalid"
  - phase: 01-closed-loop-fan-skeleton (plan 04)
    provides: "Manual-clock engine tests that recompute from model.Step, so the model change cannot break them"
provides:
  - "model.Step with start hysteresis, below_min stop/hold, asymmetric exact-exponential lag and snap-to-zero (D-11..D-13)"
  - "fanlib strict loader: fanlib.Error{File,Line,Col,Key,Msg}, IDs(), unexported loadFrom, SIL_FAN_DIR override, 64 KiB cap (D-14)"
  - "fans/arctic-p12-pwm-pst.toml with cited sources and ASSUMED labels on start_duty, tau_up, tau_down (D-10)"
  - "e2e TestModelThreshold and TestFanTomlUnknownKey"
affects: [01-06, 01-09, 01-10]

actuals:
  tokens: 10400
  tasks: 2
  commits: 5
plan_head_before: 6bc6a4674f640c60e5bb3f5dc17a1f51ff406751
plan_head_after: c79e52cb4507cbc32b22ccaa7ce1d9badb8e84a4

tech-stack:
  added: []
  patterns:
    - "Pointer-field raw struct decoded with DisallowUnknownFields so missing keys are detectable; semantic errors located by scanning for `^\\s*<key>\\s*=`"
    - "Located error type renders <file>:<line>:<col>: <msg> and drops the parts it does not know"
    - "Fan files served through fs.FS (embed, os.DirFS, fstest.MapFS) so tests need no fixture files"
    - "Negated positive range conditions (!(x >= 0 && x < 1)) so NaN is rejected without a special case per key"

key-files:
  created:
    - internal/model/fan_test.go
    - internal/fanlib/fanlib_test.go
    - e2e/model_test.go
  modified:
    - internal/model/fan.go
    - internal/fanlib/fanlib.go
    - fans/arctic-p12-pwm-pst.toml

key-decisions:
  - "start_duty == min_duty is accepted (zero-width hysteresis band); only start_duty < min_duty is rejected (planner decision under D-14, implemented and tested)"
  - "A missing required key has no line, so its Error renders `<file>: missing required key <key>` (no :line); every other semantic error carries the key's line"
  - "Unknown-key reporting uses the first StrictMissingError entry (file:line:col: unknown field <key>) and wins over the resulting missing-key error"
  - "Non-finite numbers (nan, inf) are rejected with `<key> (<v>) must be finite`; NaN duty fed to model.Step is treated as 0"
  - "Fan id must match ^[A-Za-z0-9][A-Za-z0-9._-]*$ and contain no `..`; files are opened only through fs.FS"

patterns-established:
  - "Model tests keep the Arctic constants as a literal in fan_test.go so the pure model package does not import fanlib"
  - "e2e helpers local to a file are prefixed `model` (modelAPI, modelSetFan, modelObserve, modelPortState, modelSimTime); shared jget and tracerPortValue are reused"

requirements-completed: [MODEL-01, MODEL-02, MODEL-04]

coverage:
  - id: D1
    description: "Fan model: a stopped fan starts only at duty >= start_duty, a running fan stalls below min_duty (stop) or holds min_rpm (hold), the dead band keeps the previous state, hysteresis follows duty only, zero-width band accepted"
    requirement: "MODEL-01"
    verification:
      - kind: unit
        ref: "internal/model/fan_test.go#TestStartThresholdFromRest, TestStartDutyEqualsMinDutyIsZeroWidthBand, TestStallBelowMinDutyStopsAndTargetsZero, TestHoldBelowMinDutyKeepsMinRPM, TestHysteresisBandKeepsPreviousState, TestInitialState"
        status: pass
    human_judgment: false
  - id: D2
    description: "Linear target and exact first-order lag: 63.2 % of target after tau_up, tau_down when falling, equal target leaves rpm unchanged, snap to exactly 0 below 0.5 rpm"
    requirement: "MODEL-01"
    verification:
      - kind: unit
        ref: "internal/model/fan_test.go#TestLinearTarget, TestTauUpReaches63PercentAfterTauUp, TestTauDownIsUsedWhenFalling, TestTauSelectionEqualTargetLeavesRPMUnchanged, TestSnapToZeroBelowHalfRPM"
        status: pass
    human_judgment: false
  - id: D3
    description: "Model is pure and integrates by dt only: dt <= 0 is a no-op, duty clamped to [0,1] (NaN as 0), identical inputs equal, ten 100 ms steps equal one 1 s step within 1e-9"
    requirement: "MODEL-04"
    verification:
      - kind: unit
        ref: "internal/model/fan_test.go#TestNonPositiveDtReturnsInputState, TestDutyIsClampedToUnitRange, TestStepIsPureAndComposable; internal/engine tests and internal/lint still pass"
        status: pass
    human_judgment: false
  - id: D4
    description: "Strict fan.toml: unknown key fails with <file>:<line>:<col>: unknown field <key>; missing key, bad below_min, ranges, ordering, non-finite, empty name/source, syntax and type errors fail naming the key; 64 KiB cap"
    requirement: "MODEL-02"
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestUnknownKeyIsLocated, TestMissingRequiredKey, TestSemanticErrorsCarryTheLineOfTheKey, TestSyntaxAndTypeErrorsAreLocated, TestOversizedFileIsRejected"
        status: pass
    human_judgment: false
  - id: D5
    description: "Embedded library sweep loads cleanly; SIL_FAN_DIR replaces the library; unknown or unsafe ids are rejected naming the id and location"
    requirement: "MODEL-02"
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestEmbeddedLibraryLoadsCleanly, TestSilFanDirOverridesTheEmbeddedLibrary, TestLoadUnknownIDNamesIDAndLocation, TestLoadRejectsUnsafeIDs"
        status: pass
    human_judgment: false
  - id: D6
    description: "On the running bench Fan 2 stays at rpm 0 at duty 0.08 for 3 s, reaches 50 % of target within 1.5 s of sim time at duty 0.4, the firmware Fan 2 Speed sensor goes above 0, the dead band keeps it running and duty 0.04 stalls it with falling rpm"
    requirement: "MODEL-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestModelThreshold|TestFanTomlUnknownKey|TestTracer' (TestModelThreshold PASS)"
        status: pass
    human_judgment: false
  - id: D7
    description: "`sil up` with a typo in a SIL_FAN_DIR fan file exits 1 with error.code fan_toml_invalid naming max_rmp, its line and the file, and starts no build"
    requirement: "MODEL-02"
    verification:
      - kind: e2e
        ref: "e2e/model_test.go#TestFanTomlUnknownKey"
        status: pass
    human_judgment: false
  - id: D8
    description: "The shipped Arctic constants start_duty 0.10, tau_up 1.0 s, tau_down 2.5 s are assumptions, not datasheet values; the file labels them as such (a test pins the labels), but the numbers themselves need a maintainer decision or a measurement"
    requirement: "MODEL-02"
    verification: []
    human_judgment: true
    rationale: "No datasheet value exists; only a maintainer can confirm or replace them (assumption A1)"

duration: 6min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 05: Fan Model and Strict Fan Library Summary

**The simulated fan now starts at start_duty, holds or stalls below min_duty, lags with an exact asymmetric exponential and snaps to 0, while a strict go-toml loader rejects any wrong fan.toml with file:line:col before `sil up` builds anything.**

## Performance

- **Duration:** about 6 min (start time was not recorded to the second; 01-04 finished at 01:10:48Z, this plan's last task commit was at about 01:17Z)
- **Completed:** 2026-11-09T01:18Z
- **Tasks:** 2 (both TDD)
- **Files:** 3 created, 3 modified
- **Commits:** 5 task commits (`plan_head_before` and `plan_head_after` in the frontmatter)

## Accomplishments

- `model.Step` implements D-11..D-13 exactly: stopped fan starts at `duty >= start_duty`; running fan stalls below `min_duty` (`stop`, target 0) or holds `min_rpm` (`hold`); the dead band keeps the previous state and follows duty only; target is linear from `min_rpm` to `max_rpm`; `tau_up` when rising, `tau_down` when falling, no change when equal; `rpm` snaps to 0 when the target is 0 and `rpm < 0.5`. The exported signatures are unchanged, so the engine and the 01-04 tests needed no edit. The package imports only `math` and `time`.
- `fanlib` decodes into a pointer-field struct with `DisallowUnknownFields`. Unknown keys give `<file>:<line>:<col>: unknown field <key>` (verified live: `.../arctic-p12-pwm-pst.toml:<line>:1: unknown field max_rmp`). Missing keys, `below_min`, ranges, `start_duty >= min_duty`, rpm ordering, tau, non-finite numbers, empty name/source, syntax and type errors all stop the load naming the key; semantic errors carry the key's line. A file over 64 KiB is rejected.
- `SIL_FAN_DIR` swaps the embedded library for a directory (`Source()`, `IDs()` and `Load` all follow it; errors name the absolute path). The supervisor inherits the variable because `sil up` spawns it without an explicit environment.
- `fans/arctic-p12-pwm-pst.toml` cites arctic.de for 200-1800 rpm and 0 rpm below 5 % PWM, cites the TechPowerUp cross-check as low confidence, and carries `# ASSUMED (no datasheet value)` comments on the `start_duty`, `tau_up` and `tau_down` lines. A unit test pins those labels.
- Bench evidence (`make e2e`): at duty 0.08 Fan 2 stayed at rpm 0 and `stopped` for 3 s; after speed 40 it reached 50 % of its 789 rpm target within the 1.5 s sim-time bound (observed 0.21 s between the first status that showed duty 0.4 and the first that showed half target); the firmware `Fan 2 Speed` peaked at 777.6; duty 0.07 stayed `running`; duty 0.04 turned `stopped` with falling rpm.

## Task Commits

1. **Task 1: Fan model (TDD)**
   - RED: `1d344e2` (test) - 31 matches for start_duty/hold/snap in `fan_test.go`; failed against the 01-02 thin model on five tests
   - GREEN: `91cbae9` (feat) - `fan.go`
2. **Task 2: Strict fan library, labelled Arctic file, bench e2e (TDD)**
   - RED: `7c3e810` (test) - `fanlib_test.go`, did not compile (`undefined: loadFrom`, `Error`, `maxFileBytes`)
   - GREEN: `3b2e9c8` (feat) - `fanlib.go` and the Arctic file
   - e2e: `c79e52c` (test) - `e2e/model_test.go`

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `internal/model/fan.go` - full fan model behind the unchanged `Step` signature
- `internal/model/fan_test.go` - 15 table/scenario tests with the Arctic constants and a synthetic hold fan
- `internal/fanlib/fanlib.go` - strict loader, `Error`, `IDs`, `SIL_FAN_DIR`
- `internal/fanlib/fanlib_test.go` - inline fixtures through `fstest.MapFS`, library sweep, override tests
- `fans/arctic-p12-pwm-pst.toml` - cited and labelled parameters
- `e2e/model_test.go` - `TestModelThreshold`, `TestFanTomlUnknownKey` and `model*` helpers

## Decisions Made

See `key-decisions`. The planner's zero-width-band decision (`start_duty == min_duty` accepted) is implemented and tested in both packages.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] NaN duty guard in model.Step**
- **Found during:** Task 1
- **Issue:** `math.Min(1, math.Max(0, NaN))` is NaN, so one bad duty from the bus would turn the rpm state into NaN permanently.
- **Fix:** NaN duty is treated as 0 (tested in `TestDutyIsClampedToUnitRange`).
- **Files modified:** internal/model/fan.go, internal/model/fan_test.go
- **Committed in:** `91cbae9`

**2. [Rule 2 - Missing Critical] Non-finite numbers rejected by the fan loader**
- **Found during:** Task 2
- **Issue:** TOML allows `nan` and `inf`; `nan` slips through ordinary `<`/`>` range checks and would poison the model.
- **Fix:** every numeric key is checked for finiteness first (`tau_up (NaN) must be finite`), and range checks are written as negated positive conditions.
- **Files modified:** internal/fanlib/fanlib.go, internal/fanlib/fanlib_test.go
- **Committed in:** `3b2e9c8`

### Interpretation notes (not deviations in behavior)

- The plan's truth says required-key failures read `<file>:<line>: <message>`. A missing key has no line, so its error is `<file>: missing required key <key>` (the `:line` part is dropped when unknown; `:col` already was by the plan). The key is named in all cases.
- The plan's task 1 acceptance grep is satisfied (31 matches); I added tests beyond the listed cases (NaN, integer rpm values, syntax/type errors, size cap at the exact boundary, unsafe ids).

---

**Total deviations:** 2 auto-fixed (both Rule 2)
**Impact on plan:** Both are correctness guards on untrusted numeric input; no scope creep, no new modules, go.mod and go.sum unchanged.

## Issues Encountered

None blocking. Two things worth knowing for later plans:

- `lifecycle.Up` (owned by 01-09) calls `instance.LoadOrCreate` before `fanlib.Load`, so a rejected fan file still leaves `identity.json` in the state dir. Nothing is built and no run dir is created, which is what this plan's test asserts, but the "failing commands leave no state" property of `TestCLIErrors` does not extend to `fan_toml_invalid`. 01-09 may want to swap the two calls.
- The e2e lag assertion is a bound, not a curve: `sil status` polling plus the `set-fan` round trip means the observed duty-to-half-target delta (0.21 s) is smaller than the model's true 0.69 s. The first-order shape (63.2 % at tau_up, tau_down, snap) is proven by the unit tests; the e2e test proves the bench runs the new model end to end (threshold, bound, dead band, stall).

## Threat Surface

T-01-07 mitigated (strict decode, required keys, range and ordering checks; `sil up` exits 1 before building). T-01-08 mitigated (64 KiB cap, ids validated, files opened only through `fs.FS`). T-01-SC: go.mod and go.sum unchanged. `SIL_FAN_DIR` is the surface named in the plan's threat model; no new network, auth or file-write surface.

## Known Stubs

None.

## User Setup Required

None - no external service configuration required.

## Needs Human

- **A1 (assumption, flagged `unresolved`):** confirm or replace `start_duty = 0.10`, `tau_up = 1.0 s`, `tau_down = 2.5 s` in `fans/arctic-p12-pwm-pst.toml`. They are labelled ASSUMED in the file and in `source`; the datasheet only gives 200-1800 rpm and 0 rpm below 5 % PWM. Changing them needs no code change (the unit tests pin the loader, not the numbers, except `TestShippedArcticFan`, which pins the current values and must be updated with them).
- **EDGE-MODEL-02-unclassified (flagged):** the planner's edge probe could not classify MODEL-02; the strict-decode, required-key, range, library-sweep and `SIL_FAN_DIR` e2e coverage is in place, but the manual edge review is still the maintainer's call.

## Next Phase Readiness

Ready for 01-06 onward. The engine still calls `model.Step` with the same signature; 01-10 can extend the session around it. Criterion 2 (threshold and lag) and criterion 5 (unknown fan.toml key) are observable through `sil`. `make check` and `make e2e E2E_RUN='TestModelThreshold|TestFanTomlUnknownKey|TestTracer'` pass.

## Self-Check: PASSED

- Files exist: `internal/model/fan.go`, `internal/model/fan_test.go`, `internal/fanlib/fanlib.go`, `internal/fanlib/fanlib_test.go`, `fans/arctic-p12-pwm-pst.toml`, `e2e/model_test.go`.
- Commits present in `git log`: `1d344e2`, `91cbae9`, `7c3e810`, `3b2e9c8`, `c79e52c`; test commits precede implementation commits in both packages.
- `make test-pkg PKG=./internal/model/`, `./internal/fanlib/`, `./internal/engine/`, `./internal/lint/` print `ok`; `make check` passes (gofmt, vet incl. e2e tag, `go test -race ./...`).
- `make e2e E2E_RUN='TestModelThreshold|TestFanTomlUnknownKey|TestTracer'` printed `--- PASS` for TestModelThreshold, TestFanTomlUnknownKey and TestTracer.
- `grep` acceptance checks: `DisallowUnknownFields` and `SIL_FAN_DIR` in fanlib.go; `ASSUMED` on the start_duty, tau_up and tau_down lines and `arctic.de` in the fan file; `git diff HEAD -- go.mod go.sum` empty.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

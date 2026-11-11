---
phase: 04-fan-model-v2
plan: 05
subsystem: cli
tags: [go, seed, noise, spec-json, run-json, status, kong, e2e]

requires:
  - phase: 04-fan-model-v2
    provides: "04-01 engine.Config.Seed passed to every model.NewPlant; 04-04 seeded tach noise keyed on (seed, port, 100 ms bucket)"
provides:
  - "sil up --seed N (0..2^53-1) sets the noise seed of the instance; without the flag lifecycle.DefaultSeed = 0x5EED = 24301"
  - "seed path: UpCmd.Seed -> UpOptions.Seed -> specSeed -> spec.json seed -> supervisor engCfg.Seed -> every engine and NewPlant"
  - "sil status shows sim.seed (always present, Status schema stays 1); run.json records seed"
  - "seed survives sil sim restart and sil fw restart"
  - "a seed above 2^53-1 is refused with invalid_argument (exit 1) before anything is built, locked or written"
affects: [04-06, 04-08, 04-10, phase-05-faults]

plan_head_before: fb3dba89d52d866fcdb0268f4e2f77a07b8aab47
plan_head_after: 641a516dd1ae84d02157413f8b4c033e0cd0bf9b

actuals:
  tokens: 4100
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Per-run option that must reach the supervisor travels in spec.json (written by sil up, read by Serve) and is copied into run.json and status by the supervisor"
    - "Pure pre-write option checks (checkWebOptions, checkSeed) run before config resolution, lock and build"
    - "Optional numeric flag as *uint64 so that 0 and 'not given' differ"

key-files:
  created:
    - e2e/seed_test.go
  modified:
    - internal/cli/cli.go
    - internal/cli/cli_test.go
    - internal/lifecycle/lifecycle.go
    - internal/lifecycle/lifecycle_test.go
    - internal/instance/instance.go
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - internal/control/control.go

key-decisions:
  - "Seed travels in spec.json and is copied into run.json by the supervisor, because sil up cannot write run.json (A-SEED)"
  - "Default seed lives in lifecycle (DefaultSeed = 0x5EED), not in internal/model: the default belongs to the sil up flag, NewPlant takes a seed without a default"
  - "Seed capped at 2^53-1 so browser and Python JSON readers see it exactly; the cap reuses the existing invalid_argument code"
  - "Serve sets s.engCfg.Seed with an explicit assignment after the supervisor literal, so the wiring is greppable and every engine (including a SimRestart's) inherits it"

patterns-established:
  - "e2e tests that share instances across subtests bring them up with the parent t and register an extra sil down for instances that must never start"

requirements-completed: [MODEL-10]

coverage:
  - id: D1
    description: "sil up --seed N reaches every plant of the instance through spec.json and the supervisor's engine config; sil status shows it as sim.seed (always present, schema 1); without the flag the seed is the fixed default 24301"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestUpSeedFlagReachesUpOptions"
        status: pass
      - kind: unit
        ref: "internal/lifecycle/lifecycle_test.go#TestSpecSeed"
        status: pass
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestStatusShowsSeed"
        status: pass
      - kind: e2e
        ref: "e2e/seed_test.go#TestUpSeed/flag"
        status: pass
      - kind: e2e
        ref: "e2e/seed_test.go#TestUpSeed/default"
        status: pass
    human_judgment: false
  - id: D2
    description: "The seed is written to run.json and stays the same across sil sim restart and sil fw restart"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestSeedSurvivesSimRestart"
        status: pass
      - kind: e2e
        ref: "e2e/seed_test.go#TestUpSeed/run_json"
        status: pass
      - kind: e2e
        ref: "e2e/seed_test.go#TestUpSeed/restarts"
        status: pass
    human_judgment: false
  - id: D3
    description: "A seed above 2^53-1 is refused with invalid_argument (exit 1) naming --seed before any build, lock or write; negative and non-numeric values fail kong's uint64 parse on the same code"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/lifecycle/lifecycle_test.go#TestCheckSeed"
        status: pass
      - kind: e2e
        ref: "e2e/seed_test.go#TestUpSeed/refused"
        status: pass
      - kind: other
        ref: "sil up seedx --seed=-1 | abc | 1.5 -> invalid_argument, exit 1 (manual run, nothing written)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Same seed and duty sequence give an identical tach trace across runs (criterion 3, seed half)"
    requirement: MODEL-10
    verification: []
    human_judgment: true
    rationale: "This plan only plumbs and reports the seed; the trace equality itself is proven in 04-04 (TestSeedChangesTheTrace, tick-invariance tests) and checked live in 04-10. No test here compares two real runs' traces."

duration: 5min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 05: Noise Seed Plumbing Summary

**`sil up --seed N` now carries the noise seed from the flag through spec.json and the supervisor's engine config to every plant, shows it as `sim.seed` in `sil status` and run.json, keeps it across simulator and firmware restarts, and refuses a seed above 2^53-1 before anything is written; without the flag the seed is the fixed constant 24301.**

## Performance

- **Duration:** 5 min
- **Started:** 2026-11-11T06:03:29Z
- **Completed:** 2026-11-11T06:08:16Z (code)
- **Tasks:** 2 (TDD: 2 RED + 2 GREEN commits)
- **Files modified:** 9 (1 created, 8 modified)

## Accomplishments

- Tracer (Task 1): `UpCmd.Seed *uint64` -> `UpOptions.Seed` -> `lifecycle.specSeed` (flag value or `DefaultSeed`) -> `instance.Spec.Seed` -> `Serve` sets `s.engCfg.Seed` -> every engine and `model.NewPlant`; `control.SimStatus.Seed` carries it into status. Verified end to end with `TestUpSeed/flag` (42) and `TestUpSeed/default` (24301) before the expansion started (tracer feedback gate, auto mode: `Tracer verified end-to-end - expanding`).
- Expansion (Task 2): pure `checkSeed` next to `checkWebOptions` (limit 9007199254740991, `invalid_argument`, exit 1, message names `--seed`), `RunFile.Seed` written by `run()`, and a comment at `startEngine` that restarts keep the seed.
- Negative, non-numeric and fractional values were run by hand against the built binary with scratch XDG dirs: kong refuses them with `invalid_argument` (exit 1) and nothing is written.

## Task Commits

1. **Task 1 (tracer): flag -> spec.json -> supervisor -> engine -> status**
   - RED `a78f734` (test): field/stub scaffolding plus `TestSpecSeed` and `TestStatusShowsSeed` failing on assertions
   - GREEN `00b2ea1` (feat): `specSeed`, spec.json seed, `engCfg.Seed`, `Sim.Seed`
2. **Task 2: range check, run.json, restart guarantees**
   - RED `2769253` (test): `checkSeed`/`RunFile.Seed` stubs; `TestCheckSeed`, e2e `run_json` and `refused` failing on assertions
   - GREEN `641a516` (feat): `checkSeed` wired into `Up`, `RunFile.Seed` written, restart comment

**Plan metadata:** committed separately (docs: complete plan)

## Files Created/Modified

- `internal/cli/cli.go` - `UpCmd.Seed` (`--seed`), passed in `options()`
- `internal/lifecycle/lifecycle.go` - `DefaultSeed`, `maxSeed`, `specSeed`, `checkSeed`, `UpOptions.Seed`, seed in `WriteSpec`
- `internal/instance/instance.go` - `Spec.Seed`, `RunFile.Seed`
- `internal/control/control.go` - `SimStatus.Seed`
- `internal/supervisor/supervisor.go` - `engCfg.Seed`, `RunFile.Seed`, `Sim.Seed`, restart comment
- `internal/cli/cli_test.go`, `internal/lifecycle/lifecycle_test.go`, `internal/supervisor/supervisor_test.go` - the plan's unit tests
- `e2e/seed_test.go` - `TestUpSeed` (flag, default, run_json, restarts, refused)

## Decisions Made

- `s.engCfg.Seed = spec.Seed` is an explicit assignment after the supervisor literal rather than a field in the literal: it matches the plan's wording and its `engCfg\.Seed` acceptance grep, and a reader finds the wiring with one search.
- `maxSeed` is an unexported constant in lifecycle (`1<<53 - 1`), the error detail carries the offending value.
- The `refused` subtest registers a `sil down` cleanup of its instance, so a regression that lets a bad seed start an instance cannot leak a supervisor on the test machine (it did once during the RED run, see Issues Encountered).

## Review of the flagged assumption (resolved at execution, user decision 2026-11-11)

- **A-SEED (D-15: the seed lives in run.json): resolved, implemented as planned.** `sil up` cannot write run.json (the supervisor owns it), so the seed travels in spec.json and the supervisor copies it into run.json (`TestUpSeed/run_json`: `"seed": 42`) and status (`TestUpSeed/flag`, `TestStatusShowsSeed`). The default is `lifecycle.DefaultSeed = 0x5EED` (`TestSpecSeed`, `TestUpSeed/default` reads 24301); `model.NewPlant` has no default of its own. The 2^53-1 cap keeps the value exact for JSON readers in a browser or Python (`TestCheckSeed`, `TestUpSeed/refused`); 0 is a valid seed (`TestUpSeedFlagReachesUpOptions` with `--seed 0` gives a pointer to 0, `TestCheckSeed/zero`). Restart behaviour: a `sil sim restart` builds the engine from the same `engCfg` (`TestSeedSurvivesSimRestart`, `TestUpSeed/restarts`), `sil fw restart` never touches the engine (`TestUpSeed/restarts` reads 42 after each). RESEARCH Open Question 2 is closed by this mechanism. Residual: a spec.json written by an older `sil` has no `seed` key and reads as seed 0, not the default; a run directory is rebuilt by every `sil up`, so this only concerns a supervisor started against an old spec, which cannot happen through the CLI.

## Deviations from Plan

None - plan executed exactly as written.

Notes (not deviations): `TestUpSeedFlagReachesUpOptions` and `TestSeedSurvivesSimRestart` already passed in their RED commits. The first because adding the `UpCmd.Seed` field and its pass-through in `options()` is needed for the test to compile at all; the second because the restart guarantee follows from Task 1 (the new engine is built from `engCfg` by construction), so it is a regression guard rather than a driver. The intentional REDs were `TestSpecSeed`, `TestStatusShowsSeed`, `TestCheckSeed` and the e2e `run_json` and `refused` subtests. The second RED e2e run showed that an out-of-range seed started an instance (`exit 0`), which is exactly the missing-check failure the test targets.

## TDD Gate Compliance

Both tasks have a `test(04-05)` RED commit (`a78f734`, `2769253`) before the `feat(04-05)` GREEN commit (`00b2ea1`, `641a516`). No refactor commit was needed. `workflow.tdd_mode` is off, so no `check tdd-red-evidence` record was persisted; the RED failures are the assertion failures named above.

## Issues Encountered

- During the Task 2 RED e2e run the unguarded `refused` subtest let `sil up e2e-seedbad --seed 9007199254740992` succeed and the instance outlived the test (no cleanup was registered for it). I stopped that supervisor with SIGTERM and added the cleanup to the test. No other process or file was left behind.

## Known Stubs

None. The RED-phase stubs (`specSeed` returning 0, `checkSeed` returning nil) were replaced in the GREEN commits.

## Threat Flags

None. T-04-10 is mitigated (`TestCheckSeed`, `TestUpSeed/refused`); the seed is a read-only number in status and run.json and adds no endpoint or trust boundary.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-06 can expose tach readings next to `sim.seed`; the seed is already in every status.
- 04-08 pins `newEnv` to the v0.1.0 fixtures; `TestUpSeed` reads only the seed and does not depend on fan files.
- 04-10 can assert reproducible traces live by bringing two instances up with the same `--seed`.

## Self-Check: PASSED

`e2e/seed_test.go` exists; commits `a78f734`, `00b2ea1`, `2769253`, `641a516` are in `git log` (`git rev-list --count fb3dba8..HEAD` is 4); `make check` exits 0; `make e2e E2E_RUN=TestUpSeed` passes flag, default, run_json, restarts and refused; `grep 'engCfg.Seed'` matches `supervisor.go:220` and `grep 'json:"seed"'` matches `control.go`, `instance.go` (Spec, RunFile); no deletions in the task commits; no process of the e2e instances left running.

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

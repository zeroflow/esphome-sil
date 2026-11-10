---
phase: 02-full-rev-3-3-bench
plan: 05
subsystem: fan library, per-port fan choice, sil set / up / fans
tags: [go, engine, supervisor, fanlib, noctua, cli, control-api, e2e]
requires:
  - phase: 02-full-rev-3-3-bench
    provides: 02-03 engine.Change/Apply, supervisor-held inputs under inMu, parseSet, sil set
provides:
  - fans/noctua-nf-a12x25-pwm.toml, a second shipped fan (below_min hold) with ASSUMED constants labelled
  - engine.Change.Fans: per-port replug inside the atomic Apply (rpm 0 at once, new fan starts from rest)
  - supervisor-held per-port fans (the single source of every engine's Config.Ports) and `portN.fan` keys in parseSet
  - `sil up --fan ID`, `sil fans [-i NAME]`, GET /api/v1/fans, error code unknown_fan
affects: [02-06 watch events, 02-07 contract test, 03 UI fan picker]

actuals:
  tokens: 16400
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A replug is an unplug and plug: Initial(new params) in the same inbox event as the env fields, published before Apply replies"
    - "The fan id is checked for membership in fanlib.IDs() before fanlib.Load, so a user string is never turned into a path"
    - "parseSet takes the port count and a FanLibrary interface, so the unit tests use a fake library"

key-files:
  created:
    - fans/noctua-nf-a12x25-pwm.toml
    - e2e/fans_test.go
  modified:
    - internal/engine/engine.go
    - internal/engine/engine_test.go
    - internal/supervisor/supervisor.go
    - internal/supervisor/supervisor_test.go
    - internal/control/control.go
    - internal/control/control_test.go
    - internal/cli/cli.go
    - internal/cli/cli_test.go
    - internal/lifecycle/lifecycle.go
    - internal/lifecycle/lifecycle_test.go
    - internal/apperr/codes.go

key-decisions:
  - "Assumption-delta decision taken as planned: the per-port fan list is held by the supervisor and is the only source of engine ports; DefaultFan is only the default of `sil up --fan`"
  - "Apply publishes its snapshot before it replies, so a `sil status` issued after `sil set` returned can never still show the old fan"
  - "A port number the engine does not have is ignored by Apply; validation (unknown_key for port5.fan) lives in parseSet"
  - "A fan library that cannot be listed is exit 2 `internal`; a single library file that does not load is exit 1 `fan_toml_invalid`"

requirements-completed: [MODEL-03, CLI-02]

coverage:
  - id: D1
    description: "`sil set port2.fan=noctua-nf-a12x25-pwm` swaps the fan of a running port: applied echo, rpm below 300 in the very next status, rising above 1900 within 8 s while the firmware's Fan 2 Speed follows; other ports keep their fan"
    requirement: MODEL-03
    verification:
      - kind: e2e
        ref: "e2e/fans_test.go#TestFanSwap/arctic_to_noctua_at_full_speed"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineReplugSwapsFanAndResetsToRest"
        status: pass
    human_judgment: false
  - id: D2
    description: "Replug is an unplug/replug from rest at the current duty: a hold fan (Noctua) restarts from 0 and climbs back to 430..470 rpm, also for the same fan; a stop fan (Arctic) below start_duty stays at exactly 0 and stopped, over a Noctua and over a running Arctic in its dead band"
    requirement: MODEL-03
    verification:
      - kind: e2e
        ref: "e2e/fans_test.go#TestFanSwap/noctua_holds_and_replug_restarts_it, stop_fan_below_start_duty_stays_stopped, same_fan_replug_of_a_stop_fan_in_its_dead_band"
        status: pass
      - kind: unit
        ref: "internal/engine/engine_test.go#TestEngineReplugHoldFanClimbsToMinRPM, TestEngineReplugStopFanBelowStartDutyStaysStopped"
        status: pass
    human_judgment: false
  - id: D3
    description: "Validation is all or nothing: unknown fan (exit 1 unknown_fan, detail.known sorted), mixed temperature + bad fan applies nothing, port5.fan is unknown_key, non-string value is invalid_argument, path-like ids never reach Load"
    requirement: CLI-02
    verification:
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestParseSetFanKeys, TestParseSet"
        status: pass
      - kind: e2e
        ref: "e2e/fans_test.go#TestFanSwap/rejections_change_nothing"
        status: pass
    human_judgment: false
  - id: D4
    description: "`sil up NAME --fan ID` plugs ID into all four ports; an unknown id fails exit 1 unknown_fan before any identity or run dir is written"
    requirement: MODEL-03
    verification:
      - kind: e2e
        ref: "e2e/fans_test.go#TestUpFan"
        status: pass
      - kind: unit
        ref: "internal/lifecycle/lifecycle_test.go#TestResolveFan; internal/cli/cli_test.go#TestUpFanFlagReachesUpOptions"
        status: pass
    human_judgment: false
  - id: D5
    description: "`sil fans -i NAME` prints {fans:[{id,name,min_rpm,max_rpm,below_min}]} sorted by id from the instance's own library through GET /api/v1/fans"
    requirement: CLI-02
    verification:
      - kind: e2e
        ref: "e2e/fans_test.go#TestFanChoice/fans_lists_the_library"
        status: pass
      - kind: unit
        ref: "internal/cli/cli_test.go#TestFansPrintsTheLibrary; internal/control/control_test.go#TestFansRoute; internal/supervisor/supervisor_test.go#TestFansListsTheLibrary"
        status: pass
    human_judgment: false
  - id: D6
    description: "The fan choice survives `sil sim restart` and `sil fw restart` (the invariant: status equals the held choice after a set and after each restart), and the chosen model is live in the new engine"
    requirement: MODEL-03
    verification:
      - kind: e2e
        ref: "e2e/fans_test.go#TestFanChoice"
        status: pass
      - kind: unit
        ref: "internal/supervisor/supervisor_test.go#TestSetFanSeedsTheNextEngine"
        status: pass
    human_judgment: false
  - id: D7
    description: "The Noctua file's constants are honestly labelled: 2000 rpm max and 450 rpm at 20 % PWM cited to noctua.at (not fetched), everything else marked ASSUMED"
    requirement: MODEL-03
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestEmbeddedLibraryLoadsCleanly"
        status: pass
    human_judgment: true
    rationale: "Whether the cited values match the real datasheet and whether the ASSUMED constants are plausible is a judgment about a physical fan; no test can assert it (assumption A2 of the plan)"

duration: 21min
completed: 2026-11-09
status: complete
commits: 3
plan_head_before: 0a50169ec12f15e6b936f488338b4de3a6ee04ca
plan_head_after: 9df9265c72b154feb6ef80e56aedaca17cd952d5
---

# Phase 2 Plan 05: Per-port fan choice Summary

**`sil set portN.fan=ID` unplugs and replugs a library fan on a running port (rpm 0 at once, the new fan then follows its own file from rest), `sil up --fan` and `sil fans` choose and list the library, a second shipped fan (Noctua NF-A12x25 PWM, below_min hold) makes swaps visible, and the supervisor-held choice survives sim and firmware restarts.**

## Performance

- **Duration:** about 21 min (07:09 to 07:30 UTC, plus a 9.5 min full e2e run in the background while this file was written)
- **Tasks:** 2 (1 auto, 1 TDD), 3 commits
- **Files:** 13 changed (2 created, 11 modified), +1122 / -33 lines
- **Verification:** `make check` (Go race tests, vet, gofmt, 119 C++ vectors) and the full `make e2e` (45 tests, 567 s) pass, including TestFanSwap (5 subtests), TestFanChoice, TestUpFan, TestEnvSet, TestModelThreshold, TestFanTomlUnknownKey, TestTracer, TestSimRestartResync, TestFwRestartResync.

## Accomplishments

- Second shipped fan `fans/noctua-nf-a12x25-pwm.toml`: 2000 rpm max and 450 rpm at 20 % PWM cited to noctua.at, `min_duty`, `start_duty`, `below_min = "hold"`, `tau_up`, `tau_down` each labelled ASSUMED (10 ASSUMED mentions). The Noctua holds about 450 rpm at duty 0.10 where the Arctic would run at about 284.
- Engine: `Change.Fans` replaces a port's `PortConfig` and resets its state with `model.Initial(new params)` in the same event as the env fields; the snapshot is published before Apply replies. The model is unchanged.
- Supervisor: held per-port fans seed every engine in `startEngine`; `parseSet(values, ports, lib)` validates `portN.fan` (string value, membership in `IDs()` before `Load`, `unknown_fan` / `fan_toml_invalid`, `unknown_key` for ports out of range) with the rest of D-09; `Fans` lists the library.
- Control and CLI: `GET /api/v1/fans`, `Client.Fans`, `sil fans [-i]`, `sil up --fan`, `unknown_fan` registered (exit 1). `up --fan nope` fails before any identity or run dir exists.

## Task Commits

1. **Task 1: `sil set portN.fan=ID` swaps the fan on a running port** - `80be74a` (feat)
2. **Task 2 RED: e2e TestUpFan and TestFanChoice** - `d2394fa` (test)
3. **Task 2 GREEN: `sil up --fan`, `sil fans`, GET /api/v1/fans** - `9df9265` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## Files Created/Modified

- `fans/noctua-nf-a12x25-pwm.toml` - second shipped fan
- `internal/engine/engine.go` (+ test) - `Change.Fans`, replug in `handleApply`; 5 new engine tests
- `internal/supervisor/supervisor.go` (+ test) - held fans, `FanLibrary`, `parseSet` fan keys, `Fans`
- `internal/control/control.go` (+ test) - `FanInfo`, `FansResult`, `Control.Fans`, route, client
- `internal/cli/cli.go` (+ test) - `up --fan`, `fans`, `UpCmd.options` seam
- `internal/lifecycle/lifecycle.go` (+ test) - `UpOptions.Fan`, `resolveFan`
- `internal/apperr/codes.go` - `unknown_fan`
- `e2e/fans_test.go` - TestFanSwap, TestUpFan, TestFanChoice

## Decisions Made

See `key-decisions`. The assumption-delta invariant test the plan asks for is `TestFanChoice` (status fan per port equals the held choice after a set, a sim restart and a fw restart) plus the unit test `TestSetFanSeedsTheNextEngine`; both shipped fans are exercised across the e2e (Arctic and Noctua on different ports).

## TDD Gate Compliance

Task 2 (`tdd="true"`): the RED commit `d2394fa` holds the e2e tests only (unit tests that need new symbols would not compile at that commit). Intentional RED evidence, run before any production code of the task:

- `TestUpFan/unknown_fan_writes_nothing` failed on its assertion: `up --fan nope: exit 1, {code: invalid_argument, message: unknown flag --fan}, want exit 1 unknown_fan`.
- `TestFanChoice/fans_lists_the_library` failed on its assertion: `sil fans: exit 1: {code: invalid_argument, message: unexpected argument fans}`. The set / sim restart / fw restart steps of TestFanChoice already passed at RED because Task 1 had delivered them.

GREEN `9df9265` makes both pass (plus the new unit tests, written with the implementation). No REFACTOR commit. The `gsd_run check tdd-red-evidence` record file was not produced (not needed for an unattended run; the evidence above is the output of the e2e run).

Mutation check for Task 1's engine tests: with the `p.state = model.Initial(pc.Fan)` line removed, `TestEngineReplugSwapsFanAndResetsToRest`, `TestEngineReplugHoldFanClimbsToMinRPM` and `TestEngineReplugStopFanBelowStartDutyStaysStopped` fail; the line was restored.

## Deviations from Plan

None - plan executed exactly as written, with these choices inside the plan's latitude:

- `supervisor.Fans` (Task 2 scope) was written together with Task 1's supervisor change and held back from the Task 1 commit, so each commit builds.
- `Apply` publishes before replying (one added line) to make "the next status shows the new fan" exact instead of racing the run loop's publish.
- The `--fan` seam test uses a small `UpCmd.options(env)` method (the plan allowed "a lifecycle-free seam or the parsed command struct").

**Total deviations:** 0 auto-fixed.

## Issues Encountered

- A first draft of the `--fan` help text contained backticks, which cannot sit inside a Go struct tag; replaced with plain text (`change one port later with: sil set portN.fan=ID`).
- `fmt-check` / `vet` caught nothing else; no flaky test showed up in the three full runs of the affected e2e set.

## Known Stubs

None. No placeholder data flows to a consumer.

## Threat Flags

None beyond the plan's register. T-02-12 (id must be a member of `IDs()` before `Load`; tested with `../`, absolute, `.toml` suffix, leading space and case variants, at unit and e2e level and for `up --fan`) and T-02-13 (fanlib's 64 KiB cap applies to every `Load`, unchanged) are implemented. No Go module or package was added (T-02-SC).

## User Setup Required

None - no external service configuration required.

## Needs Human

Nothing blocks the plan. For the morning review:

- **Assumption A2 (flagged in the plan):** the Noctua NF-A12x25 PWM constants. `max_rpm` 2000 and `min_rpm` 450 at 20 % PWM come from the noctua.at specification as quoted by retail listings and were not fetched in this run; `min_duty` 0.20, `start_duty` 0.20, `below_min = "hold"`, `tau_up` 1.0 s and `tau_down` 2.5 s are ASSUMED and labelled so in the file. Please confirm against the datasheet or measure.
- **EDGE-MODEL-03-unclassified (flagged in the plan):** the manual edge review of MODEL-03 is still open; automated coverage is D1 to D6 (same-fan replug, below-start replug, unknown fan, unknown port, atomic mixed set, persistence across both restarts, `--fan` at up).
- **Behaviour to be aware of:** a replug resets the port to rest even when the same fan is set again (the plan's rule); a running Arctic in its dead band therefore stops when re-set.

## Next Phase Readiness

Ready for 02-06. `Status.ports[].fan` already reflects the held choice, so `watch` events can carry it; `FansResult` is available for a future UI fan picker. No blockers.

## Self-Check: PASSED

- Created files exist: `fans/noctua-nf-a12x25-pwm.toml`, `e2e/fans_test.go`.
- Commits exist: `80be74a`, `d2394fa`, `9df9265`.
- Acceptance greps: `ASSUMED` count 10 (>= 4), `below_min  = "hold"` matches, `"unknown_fan"` in `internal/apperr/codes.go`, `/api/v1/fans` in `internal/control/control.go`, `bin/sil up --help` mentions `--fan`.
- `make check` passes; the full `make e2e` passes (45 tests).

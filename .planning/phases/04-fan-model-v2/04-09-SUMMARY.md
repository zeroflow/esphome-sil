---
phase: 04-fan-model-v2
plan: 09
subsystem: testing
tags: [e2e, fan-model, schema-2, start-boundary, coast, tach, go]

requires:
  - phase: 04-fan-model-v2
    provides: "04-08 shipped schema 2 Arctic and Noctua, the TestLibraryV2 skeleton (fans_schema2, status_v2) on the embedded library; 04-06 status and watch fields tach_rpm, airflow_m3h, power_w and the tach/load events"
provides:
  - "TestLibraryV2/curves: on the shipped Arctic through the real firmware, a speed sweep 40, 60, 80, 60, 40 gives 820, 1150, 1480 up then 1100, 780 down within 2 rpm in sil status, and sil watch carries an rpm event within 3 of 1100 after the falling step (criterion 1)"
  - "TestLibraryV2/start_stop: 8 % stays at rest, exactly start.duty (10 %, duty event v == 0.1 guarded) starts after the 0.4 s delay, 6 % keeps running, 3 % stops at once and coasts with coast_tau_s 6, the Noctua on port 3 holds 450 rpm running at duty 0 (criterion 2, D-10, D-11 at or above)"
  - "TestLibraryV2/loads_and_tach: airflow_m3h and power_w follow the fan laws to 1e-6, tach_rpm is a multiple of 30 within 120 rpm of rpm, load and tach events carry their fields (criteria 3 status side and 4)"
affects: [04-10, phase-05-faults, phase-08-bench]

plan_head_before: 1a4fb60ae210e1ceae29fd6108a638d54e769e71
plan_head_after: 85e5410eb42991844d4cb0e13791a87af3319d83

actuals:
  tokens: 2900
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Sim-time windows between watch events are measured against engine ticks: an event stamped from a bus frame is exact, one stamped by the model is on the 10 ms tick grid, so a window around a delay is one tick wide on each side"
    - "A boundary test guards its own premise: start_stop fails with 'the start boundary is not under test' if the duty event is not exactly 0.1"

key-files:
  created: []
  modified:
    - e2e/library_v2_test.go

key-decisions:
  - "The start-delay window asserted from the watch events is 390 to 410 ms, not the planned 400 to 420 ms: the duty event carries the bus frame time while the model counts the delay in whole 10 ms ticks from the tick that applies the duty (observed 397 ms)"
  - "curves checks the falling rpm event against sim time: the event must be newer than the falling step, because the rising sweep also passes 1100 on its way to 1150"

patterns-established:
  - "libv2WaitRPM: a status poll that names the step, the expected rpm, the curve and the shown rpm in its failure message"

requirements-completed: [MODEL-05, MODEL-06, MODEL-07, MODEL-09, MODEL-11]

coverage:
  - id: D1
    description: "On the shipped Arctic the rpm in sil status follows the up curve on a rising sweep and the separate down curve on the way back (820, 1150, 1480, then 1100, 780), and the watch rpm stream carries the falling approach to 1100"
    requirement: MODEL-05
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/curves"
        status: pass
    human_judgment: false
  - id: D2
    description: "The shipped Arctic stays at rest at 8 %, starts at exactly its start duty 10 % after 0.4 s (within one engine tick), keeps running at 6 %, and stops at once at 3 % with a coast ratio matching exp(-dsim/6)"
    requirement: MODEL-06
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/start_stop"
        status: pass
    human_judgment: false
  - id: D3
    description: "The shipped Noctua on port 3 holds 450 rpm and stays running at duty 0 (never stops)"
    requirement: MODEL-07
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/start_stop"
        status: pass
    human_judgment: false
  - id: D4
    description: "Status shows airflow_m3h and power_w by the fan laws and a pulse-grid tach_rpm; the watch stream delivers load events with both fields and tach events that are multiples of 30"
    requirement: MODEL-09
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/loads_and_tach"
        status: pass
    human_judgment: false
  - id: D5
    description: "The expected numbers (curve points, delays, taus, coast tau) are the plausible assumed values of the shipped files, not measurements; the subtests prove the model behaves as the files say"
    requirement: MODEL-05
    verification: []
    human_judgment: true
    rationale: "Assumption A9 stays flagged by design: whether the placeholder numbers are plausible is a maintainer judgment until Phase 8 measures the Arctic; the numbers asserted here follow the shipped file"

duration: 25min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 09: Shipped library behaviour through the real firmware Summary

**Three e2e subtests on the shipped schema 2 library, driven through the firmware's fan entity: separate up and down curves, a start at exactly start.duty after 0.4 s, run-down to stop.duty with a 6 s coast, the Noctua's 450 rpm floor, and fan-law airflow and power plus a 30 rpm-grid tach in status and watch.**

## Performance

- **Duration:** about 25 min wall clock (two `TestLibraryV2` runs of about 110 s each plus one of 50 s)
- **Completed:** 2026-11-11
- **Tasks:** 2 of 2
- **Files modified:** 1 (`e2e/library_v2_test.go`, +225 lines)

## Accomplishments

- `curves`: Fan 1 steps 40, 60, 80, 60, 40 through the firmware's fan entity (modelSetFan); status reaches 820, 1150, 1480, 1100 and 780 within 2 rpm, so 60 % reads 1150 going up and 1100 coming down. The watch rpm stream has an event within 3 of 1100 newer than the falling step.
- `start_stop`: Fan 2 from rest. 8 % gives rpm 0 and stopped for 3 s. Exactly 10 % arrives as the duty event v 0.1 (guarded; the subtest fails with "the start boundary is not under test" otherwise), the running event follows after the start delay, and a poll right after the duty event shows rpm 0. 6 % keeps running. 3 % produces a stopped event within 100 ms of sim time, and two statuses 2 s apart show the coast ratio within 3 % of exp(-dsim/6). Port 3 is swapped to the Noctua at duty 0 and shows 450 rpm (within 2) running after 20 s.
- `loads_and_tach`: Fan 1 at 60 %, one status snapshot: airflow 95.7*rpm/1800 and power 1.2*(rpm/1800)^3 within 1e-6, tach_rpm a whole multiple of 30 within 120 of rpm; a `--kind tach,load` watch started before the step delivers a load event with both numeric fields and a tach event that is a multiple of 30.
- All five subtests share the one `e2e-libv2` instance (empty SIL_FAN_DIR, seed 42); nothing but `sil status` and `sil watch` observes the plant.

## Task Commits

1. **Task 1: curves (criterion 1, tracer)** - `79ef08b` (test)
2. **Task 2: start_stop and loads_and_tach** - `85e5410` (test)

## Files Created/Modified

- `e2e/library_v2_test.go` - helper `libv2WaitRPM`, the `up` result and API port/key kept in the function body, subtests `curves`, `start_stop`, `loads_and_tach`

## Review of the flagged-unresolved assumption (A9)

A9 ("the curve points, delays, taus and coast taus these subtests assert are the plausible assumed values of the shipped files (D-03), not measurements") was reviewed at execution time against its coverage, as the user asked on 2026-11-11.

- **What covers it:** the numbers asserted here are the ones of `fans/arctic-p12-pwm-pst.toml` and `fans/noctua-nf-a12x25-pwm.toml`, which 04-08 pins (`TestShippedArcticFan`, `TestShippedNoctuaFan`) and whose provenance it pins as `assumed` (`TestShippedProvenanceMatchesEvidence`). These subtests prove the running firmware and engine behave as those files say, end to end.
- **What does not cover it:** nothing here tests whether the placeholders are plausible physics; that was not this plan's claim.
- **Resolution:** accepted as designed, unchanged from 04-08. Phase 8 replaces the Arctic values by measurement and these expected numbers then follow the new file (a single place per number in the test: the `steps` table, `startSpeed`, the 450 and 6 s constants). Recorded as coverage item D5 with `human_judgment: true`.

## Decisions Made

- The start-delay window is 390 to 410 ms (see deviation 1), the other planned numbers stand.
- No other decision; D-09, D-10, D-11 (at or above) and D-16 are followed as recorded.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The planned start-delay window [400, 420] ms contradicted how the engine stamps events**
- **Found during:** Task 2, first `TestLibraryV2` run (`start_stop`: "port 2 started 397.373ms of sim time after the duty event")
- **Issue:** the duty event is stamped at the exact bus frame time, but the model applies the duty to the whole 10 ms engine tick after it and the state event is stamped at the tick that ends the 0.4 s accumulation. The measured delay is therefore start.delay_s less up to one tick, (390, 400] ms, never 400 to 420. The engine is right (model steps are tick-pure, `Step` is tested); the test window was derived wrongly in the plan.
- **Fix:** the assertion is 390 to 410 ms (start.delay_s within one 10 ms engine tick), with a comment saying why. It still fails for a missing delay (0 ms), a wrong delay (for example 0.5 s) and a start that did not wait.
- **Files modified:** `e2e/library_v2_test.go`
- **Verification:** `make e2e E2E_RUN=TestLibraryV2` green with all five subtests
- **Committed in:** `85e5410`

**Total deviations:** 1 auto-fixed (1 test expectation corrected). **Impact:** the must_haves line "400 to 420 ms" is relaxed to 390 to 410 ms; the engine and the shipped files are unchanged.

## Authentication Gates

None.

## Issues Encountered

None open. No `sil up` instance or firmware process was left behind (each test registers `sil down`; `pgrep` after the runs found none). `make check` passes (119 Go-side suites, 66 txcap checks). The full gates (`make test-ui`, whole `make e2e`, `make test-contract`) are 04-10's, as the plan says.

## Known Stubs

None.

## Threat Flags

None. No new listener, endpoint or file access: the subtests use the same per-instance API port and key as every other e2e instance (T-04-21, accepted).

## Next Phase Readiness

- 04-10 can add the firmware-visible tach steps (S7b) and the real-bench page check, and run the full gates on the merged phase.
- `TestLibraryV2` now takes about 110 s on a warm firmware cache (50 s of it in curves and 44 s in start_stop), which counts toward the whole `make e2e` time.

## Self-Check: PASSED

- `e2e/library_v2_test.go`: FOUND; `grep -n '"curves"'`, `grep -n 'startSpeed = 10'` and `grep -n 'not under test'` all match
- Commits `79ef08b` and `85e5410`: FOUND; `git rev-list --count 1a4fb60..HEAD` is 2
- `make e2e E2E_RUN=TestLibraryV2`: `--- PASS` for TestLibraryV2 and fans_schema2, status_v2, curves, start_stop, loads_and_tach
- `make check`: exit 0

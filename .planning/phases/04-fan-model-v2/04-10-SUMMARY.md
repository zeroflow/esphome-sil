---
phase: 04-fan-model-v2
plan: 10
subsystem: testing
tags: [contract-test, aioesphomeapi, tach, e2e, readme, fan-model, schema-2]

requires:
  - phase: 04-fan-model-v2
    provides: "04-04 S7a (hermetic lattice half of the tach step signature), 04-07 page readouts and tach trace, 04-08 shipped schema 2 library and e2e pin, 04-09 shipped-library behaviour acceptance"
provides:
  - "test_firmware_sees_tach_steps (spike S7b): through the native API Home Assistant uses, Fan 3 Speed of a ppr 2, noise 0, flat 1100 rpm fan moves in pulse steps (roadmap criterion 3, D-02), with the amended step signature"
  - "Contract bench session fan library: the pinned v0.1.0 fixtures plus s7-steady in the bench's private runtime dir; Api.collect(name, seconds) records every state update"
  - "TestWebLoadReadouts on the real bench: AIRFLOW 1 decimal, POWER 2 decimals, whole TACH RPM, tach trace in scope 1, em dash for AIRFLOW and POWER of a fan without datasheet and [tach], a number for its TACH RPM"
  - "README: schema 2 fan files, provenance, the datasheet evidence rule, measured.tolerance, noise_rpm, rpm versus tach_rpm, airflow/power null rule, watch kinds tach and load, history tach, sil up --seed, page readouts"
affects: [phase-05-faults, phase-07-instrument, phase-08-bench]

plan_head_before: d70f4b0e1e5261e5325a9b5ec4f2f53badf56c22
plan_head_after: e676890ba3f8e37486f4867031071df415d2f6ae

actuals:
  tokens: 6900
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Firmware-visible observation through aioesphomeapi is a named, single-place exception to 'plant observation goes through sil' (docstring of test_tach_steps.py); the plant is sampled through sil status alongside"
    - "Contract bench builds one 0700 session fan library in its runtime dir and exports SIL_FAN_DIR for every sil call; a caller's SIL_FAN_DIR is dropped first"

key-files:
  created:
    - tests/test_tach_steps.py
    - testdata/fans-v2/s7-steady.toml
  modified:
    - tests/silbench.py
    - e2e/web_test.go
    - README.md

key-decisions:
  - "S7b passed on the first live runs, so the analytic quantisation fallback was not applied: internal/model/plant.go, internal/model/s7_test.go and .planning/PROJECT.md are untouched"
  - "test_tach_steps opens its own api connection (bench.api) instead of using the session api_main, because test_identity restarts ct-main and drops the session connection before this file runs"

patterns-established:
  - "Step signature assertion for the firmware-visible tach: spread >= 12 rpm and both sides of the mean at >= 25 %, rather than exact multiples of 30"

requirements-completed: [MODEL-09, MODEL-10, MODEL-11, MODEL-12]

coverage:
  - id: D1
    description: "The firmware-visible Fan 3 Speed of a pulse-tach fan shows the 30 rpm steps through aioesphomeapi: at least 20 readings, mean within 5 rpm of 1100, spread at most 90 rpm, at least 2 levels, spread at least 12 rpm and a toggle share of at least 25 % per side; status tach_rpm only 1080 or 1110, sim.bus_proto 1"
    requirement: MODEL-09
    verification:
      - kind: integration
        ref: "tests/test_tach_steps.py#test_firmware_sees_tach_steps"
        status: pass
    human_judgment: false
  - id: D2
    description: "On the real bench the page shows AIRFLOW, POWER, TACH RPM and the tach trace for a datasheet fan, and the em dash for AIRFLOW and POWER (with a numeric TACH RPM) for a fan without datasheet and [tach]"
    requirement: MODEL-11
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebLoadReadouts"
        status: pass
    human_judgment: false
  - id: D3
    description: "The README documents schema 2, provenance, tach, airflow/power and the seed"
    requirement: MODEL-12
    verification:
      - kind: other
        ref: "grep acceptance criteria of Task 3 (tach_rpm, provenance, --seed, [provenance], schema = 2, measured.tolerance, noise_rpm all match)"
        status: pass
    human_judgment: true
    rationale: "Whether the README reads well to a developer is a judgment call; the greps only prove the topics are present. Facts were checked against the code (sim.seed, watch kinds, fan laws, shipped provenance, plot.js tach trace)."
  - id: D4
    description: "All four gates pass on the merged phase: make check, make test-ui, make e2e, make test-contract"
    verification:
      - kind: other
        ref: "make check && make test-ui && make e2e && make test-contract"
        status: pass
    human_judgment: false

duration: 45min (continuation session; the first session ended mid-Task 3)
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 10: S7b firmware-visible tach steps, real-bench page check, README Summary

**The contract suite now proves through aioesphomeapi that the firmware's own 1 s mean of a ppr 2 tach fan moves in 30 rpm pulse steps (spike S7b passed, no fallback), the real-bench page shows tach, airflow and power with em dashes for missing datasheet values, and the README documents schema 2 with provenance, tach and the seed.**

## Performance

- **Duration:** about 45 min in this continuation session; the first session (Tasks 1 and 2) was interrupted at about 08:18 with Task 3 half written
- **Completed:** 2026-11-11
- **Tasks:** 3
- **Files modified:** 5 (tests/silbench.py, e2e/web_test.go, README.md; created tests/test_tach_steps.py, testdata/fans-v2/s7-steady.toml)

## Accomplishments

- S7b: `Fan 3 Speed` read through the native API, with the plant observed through `sil status` in the same window, shows the pulse step signature (criterion 3 as amended).
- The contract bench has one session fan library; `test_contract.py` and `test_api_smoke.py` stayed unmodified and exact.
- `TestWebLoadReadouts` runs the page against the real firmware and checks the two user decisions (D-16 em dash for AIRFLOW/POWER, D-13 numeric TACH RPM for an ideal tach).
- README rewritten for schema 2 and the new status, watch, history, `--seed` and page behaviour.

## S7b numbers (measured, 25 s window, 1100 rpm flat, ppr 2, noise 0)

| Run | Readings | Mean (rpm) | Spread (rpm) | Levels | Minority share | On-grid fraction |
|-----|----------|-----------|--------------|--------|----------------|------------------|
| 1 (task commit bf8b2da) | 25 | 1100.47 | 62.1 | 25 | 0.44 | not recorded in the commit message |
| 2 (task commit bf8b2da) | 25 | 1099.25 | 40.4 | 25 | 0.48 | not recorded in the commit message |
| 3 (this session, `pytest -s`) | 25 | 1098.29 | 52.12 | 25 | 0.480 | 0.000 |

All runs: (a) mean within 5 rpm, (b) spread at most 90, (c) at least 2 levels, (d) spread at least 12 and minority share at least 25 %. `status` tach_rpm took only 1080 or 1110, `sim.bus_proto` was 1. The `make test-contract` run of this session passed 7 of 7 (52.5 s, budget 60 s). The on-grid fraction is logged only (RESEARCH Pitfall 1): the firmware window phase blurs readings between grid levels, which is the expected result, not a failure.

**Fallback decision: S7b passed, no fallback.** `internal/model/plant.go`, `internal/model/s7_test.go` and `.planning/PROJECT.md` are untouched, so the bus value of a tach fan is still the 50 ms pulse drain and protocol 1 is unchanged.

## Resolution of the flagged assumption

- **A6 (frame-arrival jitter at most 5-10 ms, unresolved, flagged):** holds on this machine as far as the live thresholds can show. Three runs gave spreads of 40-62 rpm against the 90 rpm limit (which allows 10 ms), so the (b) trigger never fired and the fallback was never needed. This is evidence from an idle developer host, not a guarantee for a loaded one; if (b) fails in three consecutive runs on another machine with (a), (c), (d) and S7a passing, the plan's fallback rule applies. Nothing is carried forward other than that existing rule.
- **A11 (Rev 3.3 tach counting, `multiply: 0.5`, 1 s window, 30 rpm per pulse):** not touched here; stays carried to Phase 7/8 as instructed.

## Task Commits

1. **Task 1 (tracer): S7b through aioesphomeapi, session fan library** - `bf8b2da` (test) - tests/silbench.py, tests/test_tach_steps.py, testdata/fans-v2/s7-steady.toml
2. **Task 2: TestWebLoadReadouts on the real bench** - `f5ed7a0` (test) - e2e/web_test.go
3. **Task 3: README for schema 2, provenance, tach, airflow/power, seed** - `e676890` (docs) - README.md

Plan metadata commit: follows this summary.

## Continuation and verification of the interrupted session

The first executor was interrupted with Tasks 1 and 2 committed and a +110/-17 README diff uncommitted. In this session:

- Tasks 1 and 2 were checked against their acceptance criteria: the grep criteria match (`aioesphomeapi` in the docstring, `sil status` sampling, `s7-steady` in silbench and the test), the fixture is per the plan, and the S7b test file asserts every criterion (a)-(d) plus the status sampling.
- The README diff was reviewed against the code: `sim.seed` is a status field, `sil watch` kinds include `tach` and `load`, `sil fans --verbose` exists, power follows rpm cubed and airflow follows rpm (derive.go), the shipped provenance (Arctic stop.duty and the datasheet values, Noctua datasheet values, everything else assumed) matches the fan files, and plot.js draws the tach as a stepped dimmer line. The content was kept as written. Only two over-long lines were rewrapped to the file's existing style.
- Each gate was run in full afterwards.

## Gates on the merged phase

| Gate | Result |
|------|--------|
| `make check` | pass (txcap 66 passed, 0 failed) |
| `make test-ui` | pass |
| `make e2e` | pass (848 s, no `--- FAIL`; TestWebLoadReadouts 13.6 s, TestWebPlot 9.2 s, TestWebAcceptance 26.8 s all PASS) |
| `make test-contract` | pass, 7 of 7 incl. test_firmware_sees_tach_steps |

## Deviations from Plan

None - plan executed as written. The one adaptation inside the plan's latitude: `test_firmware_sees_tach_steps(bench)` takes a fresh `bench.api("ct-main")` connection instead of the `api_main` fixture, because `test_identity` restarts ct-main before this file runs (the same ordering the plan notes call out); the exception docstring and the `sil status` sampling are as planned.

## Issues Encountered

- The first session ended mid-Task 3 (handled above). The foreground `make test-ui`/`make e2e` calls were moved to the background by the harness after 180 s; they ran to completion and were polled from a log.

## Known Stubs

None.

## Threat Flags

None. T-04-19 is mitigated as planned (session fan dir in the 0700 runtime dir, removed at teardown, caller's SIL_FAN_DIR dropped); no new listener or file access outside the plan.

## Next Phase Readiness

Phase 4 plans are all executed; the phase-level verification, and Phases 5 and 6 (independent of each other) are next. A6 stays a note for loaded hosts; A11 is carried to Phase 7/8.

## Self-Check: PASSED

- tests/test_tach_steps.py, testdata/fans-v2/s7-steady.toml, tests/silbench.py, e2e/web_test.go, README.md exist; commits bf8b2da, f5ed7a0 and e676890 exist on claude/main.

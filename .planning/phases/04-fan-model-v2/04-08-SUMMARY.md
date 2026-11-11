---
phase: 04-fan-model-v2
plan: 08
subsystem: fan-library
tags: [fan-model, schema-2, provenance, test-pinning, e2e, go]

requires:
  - phase: 04-fan-model-v2
    provides: "04-06 status fields tach_rpm/airflow_m3h/power_w and events; 04-07 page tach/airflow/power readouts; 04-02 hash-pinned testdata/fans-v0.1.0 fixtures; 04-03 schema 2 loader and provenance registry"
provides:
  - "Both shipped fans (fans/arctic-p12-pwm-pst.toml, fans/noctua-nf-a12x25-pwm.toml) are schema 2 with [sources], separate up/down curves, start delay, coast tau, tach (ppr 2, noise_rpm 20), datasheet values and a complete [provenance]; a default `sil up` runs them (D-01)"
  - "Provenance that claims only its evidence: Arctic datasheet 4 / assumed 9, Noctua datasheet 3 / assumed 10, nothing measured; pinned by TestShippedProvenanceMatchesEvidence"
  - "TestShippedFansShowNoise: the default seed (0x5EED) gives at least 3 distinct 1 s readings and sd above 15 rpm at duty 0.6 for both fans; noise_rpm 0 gives at most 2 values"
  - "Every exact-v0.1.0 suite runs on testdata/fans-v0.1.0: TestMain in internal/engine and internal/supervisor, the e2e newEnv default pin, e2e/loop_test.go, and the Python bench (SIL_FAN_DIR); assertions unchanged"
  - "e2e TestLibraryV2 skeleton (fans_schema2, status_v2) on the embedded library, ready for 04-09's behaviour subtests"
  - "TestFanTomlUnknownKey in two subtests: schema2_shipped (noise_rmp) and schema1_fixture (max_rmp), each a located fan_toml_invalid before any build"
affects: [04-09, 04-10, phase-05-faults, phase-08-bench]

plan_head_before: 1a9556d66d12e3c54ec5316b1a611f8d55ae14b2
plan_head_after: 4ebfb4bd4ef8c9adc674b91d8a8ed398e9b0efe6

actuals:
  tokens: 7500
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "A TestMain that sets fanlib.EnvFanDir pins a whole package's exact-rpm tests to the v0.1.0 fixtures; a test that wants another library overrides it with t.Setenv"
    - "e2e Env.Extra starts with the SIL_FAN_DIR pin; a later SIL_FAN_DIR entry wins, and an empty value selects the embedded library"

key-files:
  created:
    - internal/engine/main_test.go
    - internal/supervisor/main_test.go
    - e2e/library_v2_test.go
  modified:
    - fans/arctic-p12-pwm-pst.toml
    - fans/noctua-nf-a12x25-pwm.toml
    - internal/fanlib/fanlib_test.go
    - e2e/harness_test.go
    - e2e/loop_test.go
    - e2e/model_test.go
    - e2e/web_test.go
    - tests/silbench.py

key-decisions:
  - "uitest (including uitest/parity_test.go) stays unpinned by design (D-02 exemption, accepted by the user 2026-11-11): the fake bench has no engine, parity asserts only literals the test wrote, and the fanlib fields it reads (id, name, min_rpm, max_rpm, below_min) are equal in the v0.1.0 and schema 2 files. make test-ui ran green on the shipped schema 2 library"
  - "The shipped files record their evidence in comments next to the values: the Arctic power_w derivation (0.1 A x 12 V = 1.2 W), the SKU deferral to Phase 8, the Noctua 450 rpm floor kept `assumed` because it is from a search summary and not from the PDF"
  - "TestShippedFansShowNoise uses a population standard deviation above 15 rpm as the bar (the most a noise-free tach toggling between two levels 30 rpm apart can reach), so the check cannot pass on an ideal tach"

patterns-established:
  - "Provenance tests assert the exact path to kind:source map, so an upgrade of a kind without new evidence fails a test rather than a review"

requirements-completed: [MODEL-05, MODEL-06, MODEL-07, MODEL-08, MODEL-10, MODEL-11, MODEL-12]

coverage:
  - id: D1
    description: "Both shipped fans are schema 2 with the prescribed values, [sources] and a 13-entry provenance each, and a default `sil up` runs them"
    requirement: MODEL-05
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestShippedArcticFan"
        status: pass
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestShippedNoctuaFan"
        status: pass
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestEmbeddedLibraryLoadsCleanly"
        status: pass
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/fans_schema2"
        status: pass
    human_judgment: false
  - id: D2
    description: "Shipped provenance is no stronger than its evidence: exact path to kind:source map for both files, every source an https URL, every cited source defined"
    requirement: MODEL-12
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestShippedProvenanceMatchesEvidence"
        status: pass
    human_judgment: false
  - id: D3
    description: "`sil fans` on the embedded library shows schema 2 with the provenance counts Arctic {datasheet 4, assumed 9} and Noctua {datasheet 3, assumed 10}, and stop.duty of the Arctic as datasheet:arctic_page"
    requirement: MODEL-12
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/fans_schema2"
        status: pass
    human_judgment: false
  - id: D4
    description: "The default sim visibly shows tach noise on both shipped fans (at least 3 distinct 1 s readings, sd above 15 rpm; none beyond 2 values at noise_rpm 0)"
    requirement: MODEL-10
    verification:
      - kind: unit
        ref: "internal/fanlib/fanlib_test.go#TestShippedFansShowNoise"
        status: pass
    human_judgment: false
  - id: D5
    description: "A default `sil up --seed 42` shows sim.seed 42 and every port carries tach_rpm, airflow_m3h and power_w"
    requirement: MODEL-11
    verification:
      - kind: e2e
        ref: "e2e/library_v2_test.go#TestLibraryV2/status_v2"
        status: pass
    human_judgment: false
  - id: D6
    description: "Every exact v0.1.0 test passes unchanged on the pinned fixtures: engine and supervisor unit tests, the whole e2e suite, and the Python contract and smoke tests"
    requirement: MODEL-08
    verification:
      - kind: command
        ref: "make check"
        status: pass
      - kind: command
        ref: "make e2e (61 top-level tests PASS, 710 s)"
        status: pass
      - kind: command
        ref: "make test-contract (6 passed)"
        status: pass
    human_judgment: false
  - id: D7
    description: "A typo in the shipped schema 2 Arctic (noise_rmp) and in the schema 1 fixture (max_rmp) each stop `sil up` with fan_toml_invalid, the typo name, file:line and no build"
    requirement: MODEL-12
    verification:
      - kind: e2e
        ref: "e2e/model_test.go#TestFanTomlUnknownKey/schema2_shipped"
        status: pass
      - kind: e2e
        ref: "e2e/model_test.go#TestFanTomlUnknownKey/schema1_fixture"
        status: pass
    human_judgment: false
  - id: D8
    description: "The page runs on the shipped schema 2 library (uitest, unpinned by the recorded D-02 exemption)"
    requirement: MODEL-11
    verification:
      - kind: command
        ref: "make test-ui"
        status: pass
    human_judgment: false
  - id: D9
    description: "The assumed numbers of both shipped files (curve interiors, down curve, start delay, taus, coast tau, ppr, noise) are plausible placeholders, not sourced; whether they are plausible enough is a maintainer judgment until Phase 8 measures the Arctic"
    requirement: MODEL-05
    verification: []
    human_judgment: true
    rationale: "Assumption A9 stays flagged by design: the values are labelled `assumed` and no test can say whether a placeholder is plausible; Phase 8 replaces the Arctic ones by measurement, the Noctua stays assumed until v0.3"
---

# Phase 4 Plan 08: Shipped fans as schema 2 Summary

**Arctic P12 and Noctua NF-A12x25 rewritten as schema 2 with separate up/down curves, start delay, coast tau, a 20 rpm tach noise and datasheet values, each number tagged with the evidence it really has; every exact-v0.1.0 suite pinned to the byte-for-byte fixtures, so `make check`, `make e2e`, `make test-ui` and `make test-contract` are all green.**

## Performance

- **Duration:** about 35 min wall clock, of which two full e2e runs took 12 min each
- **Tasks:** 2 of 2
- **Files:** 11 (3 created, 8 modified, excluding planning files)
- **Commits:** 2 task commits (`4ec2c38`, `4ebfb4b`)

## Accomplishments

- Both shipped fans are schema 2. The Arctic keeps its page-confirmed facts (200 rpm at 5 %, 1800 rpm at 100 %, 0 rpm below 5 %, 95.70 m3/h, max_rpm 1800) and gains a down curve slightly below the up curve, a 0.4 s start delay, coast tau 6 s, ppr 2 and noise_rpm 20. The Noctua is a never-stops fan in the D-10 form (stop.duty 0, curve point (0, 450)), max_rpm 2000, datasheet 102.1 m3/h, 1.68 W at 2000 rpm.
- Provenance is exactly as evidenced. Arctic `datasheet` 4 (stop.duty, airflow, power_w, ref_rpm), all from arctic_page, power_w as the recorded derivation 0.1 A x 12 V = 1.2 W; `assumed` 9. Noctua `datasheet` 3 (airflow, power_w, ref_rpm from the official datasheet PDF, URL in [sources]); `assumed` 10, including the 450 rpm floor. No value is `measured`.
- Default `sil up` shows the new model: `TestLibraryV2` starts a default instance with `--seed 42` on the embedded library and checks `sil fans` (schema 2, counts, stop.duty provenance, max_rpm, below_min) and status (sim.seed 42, tach_rpm/airflow_m3h/power_w on every port).
- Exact tests are pinned (D-02): TestMain in `internal/engine` and `internal/supervisor`, the e2e `newEnv` default `Extra`, `e2e/loop_test.go`, and `tests/silbench.py`. Their assertions did not change.
- Criterion 5 on the shipped library: `TestFanTomlUnknownKey` now covers a schema 2 typo (`noise_rmp` in the shipped Arctic) and a schema 1 typo (`max_rmp` in the v0.1.0 fixture), each a located `fan_toml_invalid` with no build.

## Task Commits

1. **Task 1: schema 2 shipped fans, shipped-fan unit tests, TestMain pins, TestLibraryV2** - `4ec2c38` (feat)
2. **Task 2: e2e and contract pins, two-subtest typo check** - `4ebfb4b` (test)

## Review of the flagged-unresolved assumption (A9)

A9 ("the assumed curve points, delays, taus and coast taus are plausible placeholders within D-03, not sourced") was reviewed at execution time against its coverage, as the user asked on 2026-11-11.

- **What covers it:** every one of those values is tagged `assumed` in [provenance] (the curve.up interior is `assumed:arctic_page` / `assumed:noctua_datasheet`, naming only where its end points come from). `TestShippedProvenanceMatchesEvidence` pins that exact map, so none can be promoted without a failing test. `TestShippedArcticFan` pins the numbers themselves, and the down-below-up relation of the Arctic interior. `TestShippedFansShowNoise` proves the noise value does what D-03 intends.
- **What does not cover it:** nothing tests whether the placeholder numbers are plausible physics. The curve interiors (for example 820 rpm at 40 % duty for the Arctic) are a smooth fit between two page-confirmed end points, and the Noctua interior is a smooth fit between the 450 rpm floor and the 2000 rpm maximum.
- **Resolution:** accepted as designed. The risk is only transparency, and it is closed by the `assumed` tag plus the evidence-pinning test; plausibility is the maintainer's call and Phase 8 replaces the Arctic values by measurement (Noctua stays assumed until v0.3). Recorded as coverage item D9 with `human_judgment: true`. No value was invented for airflow or power, and no source URL was fetched.

## Files Created/Modified

- `fans/arctic-p12-pwm-pst.toml`, `fans/noctua-nf-a12x25-pwm.toml` - schema 2, with evidence comments next to the values
- `internal/fanlib/fanlib_test.go` - TestEmbeddedLibraryLoadsCleanly (schema 2), TestShippedArcticFan, TestShippedNoctuaFan, TestShippedProvenanceMatchesEvidence, TestShippedFansShowNoise
- `internal/engine/main_test.go`, `internal/supervisor/main_test.go` - TestMain pin to testdata/fans-v0.1.0
- `e2e/library_v2_test.go` - TestLibraryV2 (fans_schema2, status_v2)
- `e2e/harness_test.go`, `e2e/loop_test.go` - default pin of the e2e Env and the in-process engine
- `e2e/model_test.go` - TestFanTomlUnknownKey, two subtests
- `e2e/web_test.go` - parity_real tolerance for tach_rpm (see deviation)
- `tests/silbench.py` - Bench.create sets SIL_FAN_DIR to the v0.1.0 fixtures

## Decisions Made

See key-decisions above. Nothing contradicts a prior decision; D-01..D-04 (with the 2026-11-10 amendments) and the D-02 uitest exemption are followed as recorded.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] parity_real compared tach_rpm exactly between the page store and GET /api/v1/status**
- **Found during:** Task 2, the first full `make e2e` run (the only failure of the run: `TestWebAcceptance/parity_real`)
- **Issue:** the page store holds `tach_rpm` as the float32 value of the tach event (1799.996337890625) and status holds the float64 gate reading (1799.996343097604), so the exact comparison failed on all four ports even at rest. The test only allowed ports.*.rpm to differ by 1 rpm. It is not caused by the pin or the new files: the 04-06 summary records that `make e2e` was not run there, and 04-07 added the page's tach readout, so the failure was latent until a whole-suite run. It blocked the plan's acceptance criterion that the whole `make e2e` passes.
- **Fix:** add `"ports.*.tach_rpm": 1` to the `near` map of parity_real, with a comment naming the float32 rounding. No other assertion of the test changed.
- **Files modified:** `e2e/web_test.go` (a file 04-10 also edits; the change is one map entry and a comment, so there is nothing to merge)
- **Verification:** `TestWebAcceptance` alone, then the whole `make e2e` again: 61 top-level tests PASS, `ok`
- **Commit:** `4ebfb4b`

**Total deviations:** 1 auto-fixed (1 blocking). **Impact:** none on scope; one test tolerance, and it removes a failure that would have appeared in 04-09 or 04-10 anyway.

### Notes

- The acceptance line `grep -n 'fans-v0.1.0' e2e/harness_test.go` is "one line in each file". The first comment draft added a second match, so the comment was reworded and the file now has exactly one.
- The plan says TestShippedFansShowNoise collects readings "from 10 s on (60 readings)". Whole seconds 10..70 are 61 readings, so the code collects seconds 11..70 (60 readings).

## Authentication Gates

None.

## Issues Encountered

None open. `make test-ui` and the whole `make e2e` take 70 s and 12 min respectively; no `sil up` instance was left behind (each test registers `sil down`, and no sil or firmware process was running afterwards).

## Known Stubs

None. No placeholder values flow to the UI: the assumed fan numbers are intentional, labelled `assumed`, and listed above under A9.

## Threat Flags

None. No new network endpoint, auth path or file access pattern; the only external references are source URLs recorded as text in [sources] and never fetched.

## Next Phase Readiness

- 04-09 can add its behaviour subtests (curves, start_stop, loads_and_tach) to `TestLibraryV2` in `e2e/library_v2_test.go`; `env.Up` and the instance name `e2e-libv2` are in the function body.
- 04-10 owns the rest of `e2e/web_test.go` (TestWebLoadReadouts); the parity_real tach tolerance is already in place.
- The Arctic SKU (ACFAN00120A vs ACFAN00170A) is still deferred to the Phase 8 bench setup, as decided.

## Self-Check: PASSED

- `fans/arctic-p12-pwm-pst.toml`, `fans/noctua-nf-a12x25-pwm.toml`, `internal/engine/main_test.go`, `internal/supervisor/main_test.go`, `e2e/library_v2_test.go`: FOUND
- Commits `4ec2c38` and `4ebfb4b`: FOUND
- Acceptance greps: `^schema = 2` one line per fan file; `^noise_rpm = 20.0` one line per fan file; `fans-v0.1.0` one line in e2e/harness_test.go and one in tests/silbench.py
- `make check`, `make test-ui`, `make e2e` (whole suite), `make test-contract` (tests/test_contract.py and tests/test_api_smoke.py unmodified): all exit 0

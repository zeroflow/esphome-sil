---
phase: 04-fan-model-v2
verified: 2026-11-11T16:10:00Z
status: passed
score: 5/5 must-haves verified
covered_files:
  - .planning/phases/04-fan-model-v2/04-01-PLAN.md
  - .planning/phases/04-fan-model-v2/04-01-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-02-PLAN.md
  - .planning/phases/04-fan-model-v2/04-02-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-03-PLAN.md
  - .planning/phases/04-fan-model-v2/04-03-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-04-PLAN.md
  - .planning/phases/04-fan-model-v2/04-04-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-05-PLAN.md
  - .planning/phases/04-fan-model-v2/04-05-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-06-PLAN.md
  - .planning/phases/04-fan-model-v2/04-06-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-07-PLAN.md
  - .planning/phases/04-fan-model-v2/04-07-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-08-PLAN.md
  - .planning/phases/04-fan-model-v2/04-08-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-09-PLAN.md
  - .planning/phases/04-fan-model-v2/04-09-SUMMARY.md
  - .planning/phases/04-fan-model-v2/04-10-PLAN.md
  - .planning/phases/04-fan-model-v2/04-10-SUMMARY.md
  - README.md
  - e2e/fans_test.go
  - e2e/harness_test.go
  - e2e/library_v2_test.go
  - e2e/loop_test.go
  - e2e/model_test.go
  - e2e/model_v2_test.go
  - e2e/seed_test.go
  - e2e/web_test.go
  - fans/arctic-p12-pwm-pst.toml
  - fans/noctua-nf-a12x25-pwm.toml
  - internal/cli/cli.go
  - internal/cli/cli_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/engine/curve_test.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/engine/main_test.go
  - internal/engine/startstop_test.go
  - internal/engine/surfaces_test.go
  - internal/engine/tach_test.go
  - internal/events/events.go
  - internal/fanlib/fanlib.go
  - internal/fanlib/fanlib_test.go
  - internal/fanlib/fixtures_test.go
  - internal/fanlib/v1.go
  - internal/fanlib/v2.go
  - internal/fanlib/v2_test.go
  - internal/instance/instance.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/lint/wallclock_test.go
  - internal/model/curve.go
  - internal/model/curve_test.go
  - internal/model/derive.go
  - internal/model/derive_test.go
  - internal/model/fan.go
  - internal/model/fan_test.go
  - internal/model/fan_v010_test.go
  - internal/model/noise.go
  - internal/model/plant.go
  - internal/model/s7_test.go
  - internal/model/startstop_test.go
  - internal/model/tach.go
  - internal/model/tach_test.go
  - internal/supervisor/main_test.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - internal/supervisor/surfaces_test.go
  - testdata/fans-v0.1.0/arctic-p12-pwm-pst.toml
  - testdata/fans-v0.1.0/noctua-nf-a12x25-pwm.toml
  - testdata/fans-v2/curve-test.toml
  - testdata/fans-v2/load-test.toml
  - testdata/fans-v2/s7-steady.toml
  - testdata/fans-v2/startstop-test.toml
  - tests/silbench.py
  - tests/test_tach_steps.py
  - ui/web/app.css
  - ui/web/index.html
  - ui/web/js/app.js
  - ui/web/js/plot.js
  - ui/web/js/ring.js
  - ui/web/js/store.js
  - ui/web/js/view.js
  - uitest/bench_test.go
  - uitest/layout_test.go
  - uitest/live_test.go
  - uitest/parity_test.go
  - uitest/plot_test.go
covered_digest: "v2:sha256:90b9332a7f961dfed4858d4b4c686c009681ef7f5adde257168925b58d7697d3"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: human_needed
  previous_score: 5/5
  gaps_closed: []
  gaps_remaining: []
  regressions: []
  warnings_closed:
    - "WR-01 never-stops floor rule now also applied to curve.down (6ebeb3f)"
    - "WR-02 control characters rejected by the loader and quoted by sil fans (fdac7ae)"
    - "WR-03 locate follows escapes and multi-line strings (34ddfd9)"
  human_items_closed_by: "04-UAT.md (status complete, 4 of 4 passed, 0 issues)"
---

# Phase 4: Fan Model v2 Verification Report

**Phase Goal:** A simulated fan behaves like a real one: separate up and down duty to RPM curves, distinct start and stop thresholds with start delay and coast-down, a tach that quantises and jitters like the firmware's `pulse_counter` window, and airflow and power from datasheet values. All of it driven by an explicit `fan.toml` schema 2 with per-value provenance, while every v0.1.0 fan file still behaves exactly as before.
**Verified:** 2026-11-11
**Status:** passed
**Re-verification:** Yes. The prior report (human_needed, 14:30Z) was made stale by six code-review fixes (6ebeb3f..62091f8). It had no `gaps:` section, so the five truths were re-derived from ROADMAP and every one was re-checked against HEAD, with full scrutiny on the three fix areas called out below.

No truth failed, no regression was found, and no human item is outstanding. The four human items of the prior report were run as UAT and all passed. The three review warnings (WR-01..03) and three infos are fixed and covered by tests that pass at HEAD.

## Goal Achievement

### Observable Truths (ROADMAP Phase 4 success criteria, amended wording)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A schema 2 fan sweeps up then down through the firmware's fan entity; `sil status` / `sil watch` show separate, monotone, interpolated up and down curves, lower on the way down at the same duty | VERIFIED | Unchanged code paths (`model/fan.go` direction-sticky `upCurve()`/`downCurve()`, `curve.go` interpolation, loader monotonicity rules). Re-ran at HEAD against the real firmware: `TestModelV2Curves` and `TestLibraryV2` (curves subtest on the shipped Arctic) PASS. `TestStepMatchesV010` and the model suite pass with the new `Params.Clone` in `NewPlant` |
| 2 | From rest 0 rpm until duty is at or above start duty, spin-up after the start delay; once running it keeps turning down to stop duty, below that stops and coasts with its own dynamics; a "never stops" fan holds its floor rpm at 0 % duty | VERIFIED | Runtime evidence: `TestLibraryV2` (start_stop subtest: start at exactly 0.10 after the delay, runs at 0.06, stops at 0.03 and coasts, Noctua holds 450 rpm at 0 % duty) PASS at HEAD. The WR-01 hole noted last time is closed: `validateV2` rule (e) now requires the first `curve.down` point at duty 0 with rpm > 0 when `stop.duty = 0` and a down curve exists (`internal/fanlib/v2.go`, error key `curve.down.duty`). `TestV2NeverStopsFormDerivesHold` covers three rejected down curves plus a never-stops file without a down curve; `TestShippedNoctuaFan` shows the shipped never-stops fan (down curve starts at 450) still loads |
| 3 | Firmware-reported RPM moves in 30 rpm steps with the defined step signature; noise seeded, amplitude from `noise_rpm`; same seed and duty sequence give identical traces regardless of tick timing; bus protocol still 1 | VERIFIED | `plant.go`, `tach.go`, `noise.go` untouched in logic by the fixes (only `NewPlant`/`Params()` clone). S7a (`TestS7aFirmwareWindowKeepsSteps`), `TestTachIgnoresTickTiming`, `TestNoiseIsKeyed`, `TestNoiseAmplitudeIsGateStd` pass in `go test -count=1 ./internal/model/`. `TestLibraryV2` loads_and_tach subtest PASS (tach whole multiples of 30). The S7b firmware contract test (`tests/test_tach_steps.py`) was not re-run (15+ min Docker); no fix touched the tach path, bus, or firmware components, and S7a covers the same window hermetically |
| 4 | Each port shows airflow (m3/h, proportional to rpm) and power (W, proportional to rpm^3) from datasheet values in `sil status`, `sil watch` and the page | VERIFIED | `derive.go`, engine `load` events, `control.PortStatus` unchanged. `TestLibraryV2` loads_and_tach (airflow and power match the fan laws to 1e-6) PASS at HEAD. Page readouts and the stepped tach trace were checked by a human in UAT test 1 (pass) |
| 5 | Every v0.1.0 schema 1 file loads and behaves exactly as in v0.1.0; shipped files are schema 2; v0.1.0 files hash-pinned; schema 2 unknown key rejected with file:line:col; `sil fans` lists schema and provenance of every value | VERIFIED | sha256 of `testdata/fans-v0.1.0/*.toml` still equals `git show v0.1.0:fans/<file>` (`d9fb55c7...`, `d7dacb32...`); `TestV010FixturesArePinned`, `TestV010FixturesLoadAsV010`, `TestStepMatchesV010`, `TestPlantMatchesV010ForLegacyParams` PASS (`go test -count=1`). The IN-03 change to `loadFrom` only touches the no-`schema`-key branch and only appends a hint to an already-failing error; a valid schema 1 file takes the same `loadV1` path and a typo such as `max_rmp` keeps its old message. A freshly built scratchpad `sil fans` prints both shipped fans as `schema: 2` with provenance counts (Arctic 4 datasheet / 9 assumed, Noctua 3 / 10) and a per-value `values` map. e2e `TestFansProvenance` and `TestFanTomlUnknownKey` PASS at HEAD |

**Score:** 5/5 truths verified (0 present-but-behavior-unverified)

### Regression Check of the Six Review Fixes

| Fix | Change | Regression risk | Result |
|-----|--------|-----------------|--------|
| WR-01 (6ebeb3f) | Rule (e) also validates `curve.down[0]` for `stop.duty = 0` | Rejects a shipped or fixture file | No: shipped Noctua (the only never-stops file) satisfies it; schema 1 never reaches `validateV2`. `TestShippedNoctuaFan`, `TestShippedProvenanceMatchesEvidence`, `TestV2NeverStopsFormDerivesHold` PASS |
| WR-02 (fdac7ae) | `hasControl` (`unicode.IsControl`) on `name`, `[sources]`, `[measured]` strings; `terminalSafe` quoting in `sil fans` | Rejects legitimate text or changes normal output | No: shipped files contain no control characters and load; output is byte-identical for clean text (existing summary tests unchanged and green). `TestV2RejectsControlCharacters`, `TestFansSummaryQuotesControlCharacters` PASS |
| WR-03 (34ddfd9) | `bracketDelta` replaced by `valueScan` (escapes, literal strings, multi-line strings) | Wrong line numbers or an infinite or missed header on a normal file | Read the state machine: the escape, quote, triple-quote and comment cases are ordered correctly and a `]` with depth 0 cannot underflow. `TestLocateStringsAndEscapes` (end-to-end case plus nine direct `locate` cases) PASS, as do the pre-existing located-error tests for shipped-style multi-line arrays |
| IN-01 (39c2f09) | Comment only | none | n/a |
| IN-02 (a9760fe) | `Params.Clone`, `NewPlant` clones, `Plant.Params()` returns a clone | Missing a reference field in `Clone`, behaviour drift in the plant | `Params` fields are scalars, `Curve` (two slices of value-type `Point`), `Provenance` slice, `Sources` map and `*Datasheet` (flat value struct); all are copied, so no alias remains. The v0.1.0 differential tests and `TestPlantOwnsItsParams` PASS, and the real-firmware e2e (`TestModelV2Curves`, `TestLibraryV2`) PASS through the cloned plant |
| IN-03 (62091f8) | Hint appended for a schema 2 body without `schema = 2` | Alters v0.1.0 semantics | No, message only (see Truth 5). `TestMissingSchemaKeyHintsAtSchema2` PASS |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/model/{fan,curve,plant,tach,noise,derive}.go` | Plant seam, curves, start/stop/coast, pulse tach, keyed noise, fan laws | VERIFIED | Substantive and wired into `engine.go`; `Clone` added without changing step logic |
| `internal/fanlib/{fanlib,v1,v2}.go` | Strict schema 1/2 loader, provenance, located errors | VERIFIED | Stricter validation from WR-01 and WR-02, sturdier `locate` from WR-03 |
| `fans/*.toml` | Shipped fans as schema 2 with complete provenance | VERIFIED | Both load at HEAD under the stricter rules |
| `testdata/fans-v0.1.0/*` | Byte-identical v0.1.0 fixtures | VERIFIED | Hashes match `v0.1.0` |
| `internal/control`, `events`, `supervisor`, `cli`, `lifecycle` | tach_rpm / airflow_m3h / power_w, `--seed`, `sil fans` summary | VERIFIED | Unchanged apart from `terminalSafe` in `cli.go` |
| `ui/web/*` | Stepped tach trace, TACH RPM / M3/H / W readouts | VERIFIED | Not touched by the fixes; human-checked in UAT test 1 |
| `tests/test_tach_steps.py` | S7b contract test | VERIFIED (exists) | Not re-run |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| fan.toml schema 2 | `model.Params` | `fanlib.loadV2` | WIRED | Real-firmware e2e at HEAD |
| `model.Plant` | engine tick and bus frames | `NewPlant` (now cloning), `Step`, `Frame` | WIRED | Clone happens only at engine start and on a replug; e2e green |
| `--seed` | every plant | `lifecycle.specSeed` to `engCfg.Seed` to `NewPlant` | WIRED | Unchanged; IN-01 only reworded the comment |
| Plant outputs | status / watch / page | `PortStatus`, `tach` and `load` events | WIRED | e2e and UAT |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| Page TACH RPM / AIRFLOW / POWER | `tach_rpm`, `airflow_m3h`, `power_w` | `Plant.Reading()` / `Plant.Outputs()` | Yes (e2e: fan laws to 1e-6, tach on the 30 rpm grid) | FLOWING |
| Scope tach trace | `ring.tach` | sample frames and `tach` events | Yes (human-viewed in UAT test 1) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Static analysis | `go vet ./...` (also compiles e2e and uitest) and `gofmt -l .` | clean, no files listed | PASS |
| Unit suite | `go test ./internal/...` | all 16 packages `ok` | PASS |
| Fix areas uncached | `go test -count=1 ./internal/fanlib/ ./internal/model/ ./internal/engine/ ./internal/cli/ ./internal/lint/` | all `ok` | PASS |
| Named fix tests | `-run 'TestV2NeverStops\|TestLocate\|TestV2RejectsControl\|TestMissingSchemaKey\|TestShipped\|TestV010Fixtures\|TestFansSummaryQuotes\|TestPlantOwns\|TestStepMatchesV010\|TestPlantMatchesV010'` | 14 tests PASS | PASS |
| v0.1.0 fixture bytes | `sha256sum` against `git show v0.1.0:fans/...` | identical | PASS |
| `sil fans` on the shipped library | scratchpad build (not `bin/sil`) run outside the repo | both fans `schema: 2` with provenance maps | PASS |
| Loader e2e | `go test -tags sil_e2e -run 'TestFansProvenance\|TestFanTomlUnknownKey' ./e2e/` | ok, 6 s | PASS |
| Real-firmware e2e on the shipped library | `go test -tags sil_e2e -run 'TestLibraryV2\|TestModelV2Curves' ./e2e/` | ok, 143 s | PASS |
| S7b firmware tach steps | `make test-contract` | not re-run (15+ min, nothing in the tach, bus or firmware path changed) | SKIP, covered by S7a and the unchanged e2e tach assertions |

The `uat` instance (`bin/sil serve uat`) was still running after all checks and was not touched. It runs the pre-fix loader, as the fix report notes; that only matters after the maintainer rebuilds `bin/sil`.

### Probe Execution

Step 7c: SKIPPED. The phase declares no `scripts/*/tests/probe-*.sh` probes.

### Requirements Coverage

Union of PLAN `requirements:` across 04-01..04-10 is exactly MODEL-05..MODEL-12. REQUIREMENTS.md maps the same eight IDs to Phase 4 (all marked Complete) and no other, so there are no orphans.

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| MODEL-05 | 04-01, 04-08, 04-09 | Separate up/down curves, monotone, interpolated | SATISFIED | Truth 1 |
| MODEL-06 | 04-02, 04-08, 04-09 | Start at or above start duty after delay; stop below separate stop duty | SATISFIED | Truth 2 |
| MODEL-07 | 04-02, 04-08, 04-09 | Coast-down with own dynamics; never-stops floor at 0 % | SATISFIED | Truth 2; the floor rule is now enforced on both curves (WR-01) |
| MODEL-08 | 04-01, 04-02, 04-03, 04-08 | Explicit `schema = 2`; schema 1 unchanged; unknown keys rejected | SATISFIED | Truth 5 |
| MODEL-09 | 04-04, 04-06, 04-07, 04-09, 04-10 | Pulse accumulator, 30 rpm steps, protocol stays 1 | SATISFIED | Truth 3 |
| MODEL-10 | 04-04, 04-05, 04-08, 04-10 | Deterministic seeded noise, amplitude from fan.toml | SATISFIED | Truth 3 |
| MODEL-11 | 04-06, 04-07, 04-08, 04-09, 04-10 | Airflow and power in status, watch, page | SATISFIED | Truth 4 |
| MODEL-12 | 04-03, 04-08, 04-10 | Provenance on every value; `sil fans` shows schema and provenance | SATISFIED | Truth 5 |

### Anti-Patterns Found

Debt-marker scan (`TBD`, `FIXME`, `XXX`, `TODO`, `HACK`) over the non-test lines added by the six fix commits and over `internal/`, `fans/`, `ui/`, `README.md`: no hits. No stub returns feed any output. The earlier WR-01..03 and IN-01..03 entries are resolved; none remains.

### Human Verification

None outstanding. The four items of the prior report were executed as `04-UAT.md` (status complete, 4 of 4 passed, 0 issues):

| Prior item | UAT result |
|------------|-----------|
| Web page look of the stepped tach trace and readout row | pass (UAT 1) |
| Plausibility of the assumed values in the shipped fan files | pass (UAT 2) |
| README readability | pass (UAT 3) |
| Disposition of the review findings | pass (UAT 4); `04-REVIEW-DISPOSITION.md` shows 6 fixed, 0 open |

Standing assumption, not a verification gap: A11 (Rev 3.3 counts its tach at 30 rpm per pulse) is explicitly carried to Phase 7/8 in ROADMAP, and the assumed fan values stay labelled `assumed` until Phase 8 measures them.

### Gaps Summary

No gaps. All five ROADMAP success criteria and all eight requirements hold at HEAD (f644525) after the review fixes. The three fix areas the re-run was asked to scrutinise (WR-01 curve.down validation, WR-03 `locate` rewrite, IN-02 plant parameter cloning) were read in full, are covered by tests that pass uncached, and cause no regression in the v0.1.0 differential tests, the shipped-fan loader tests, or the real-firmware e2e acceptance tests. The one check not re-run is the slow S7b firmware contract test, which exercises code the fixes did not touch.

---

_Verified: 2026-11-11T16:10:00Z_
_Verifier: Claude (gsd-verifier)_

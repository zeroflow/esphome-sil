---
phase: 04-fan-model-v2
verified: 2026-11-11T14:30:00Z
status: human_needed
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
covered_digest: "v2:sha256:1e1ce6ba09252d0ee0a9b8274205fe1b8a89553e1d6050396138a6cfee287c41"
behavior_unverified: 0
overrides_applied: 0
warnings:
  - id: WR-01
    summary: "D-10 never-stops floor rule (loader rule e) checks curve.up only; a curve.down that drops to 0 rpm is accepted for a stop.duty = 0 fan"
    file: "internal/fanlib/v2.go:283-288"
    impact: "Does not affect either shipped fan or any tested form; a contradictory hand-written (or, later, fit-written) file would give a fan reported 'running' at 0 rpm"
  - id: WR-02
    summary: "Control characters in name/[sources]/[measured] strings are accepted and printed raw by the `sil fans` stderr summary"
    file: "internal/fanlib/v2.go, internal/cli/cli.go (writeFansSummary)"
  - id: WR-03
    summary: "locate() loses the line number after a string holding an escaped quote followed by a bracket"
    file: "internal/fanlib/v2.go:568-590"
human_verification:
  - test: "Open the web page (`sil up`, then the printed URL) on the shipped Arctic fan and sweep Fan 1 up and down"
    expected: "Per port scope: solid smooth shaft RPM trace plus a dimmer stepped tach trace in the same port colour; the readout row shows TACH RPM / M3/H / W without crowding the existing readouts, and shows an em dash for null values"
    why_human: "Visual look and layout of the stepped trace and the readout row (coverage item D5) cannot be judged from DOM assertions"
  - test: "Read fans/arctic-p12-pwm-pst.toml and fans/noctua-nf-a12x25-pwm.toml against the vendor pages and decide whether the assumed numbers (interior curve points, down curve, start delay 0.4 s, tau values, coast_tau 6 s, noise_rpm 20, Noctua 450 rpm floor) are plausible enough to ship until Phase 8 measures them"
    expected: "Values labelled `assumed` look plausible; `datasheet` entries are backed by the recorded sources"
    why_human: "Plausibility of assumed physical constants is a judgment call (D-03); the code only proves they are labelled and honoured"
  - test: "Skim README.md schema 2 / provenance / tach / airflow-power / --seed sections"
    expected: "Reads clearly to a developer and matches `sil fans`, `sil status` and `sil watch` output"
    why_human: "Readability is judgment; presence of the topics was checked mechanically"
  - test: "Decide whether to close WR-01 (and optionally WR-02/WR-03) before Phase 6 `fit` starts writing schema 2 files"
    expected: "Maintainer disposition recorded in 04-REVIEW-DISPOSITION.md (all six findings are still `open`)"
    why_human: "Scope/priority decision, not a verifiable fact"
---

# Phase 4: Fan Model v2 Verification Report

**Phase Goal:** A simulated fan behaves like a real one: separate up and down duty to RPM curves, distinct start and stop thresholds with start delay and coast-down, a tach that quantises and jitters like the firmware's `pulse_counter` window, and airflow and power from datasheet values. All of it driven by an explicit `fan.toml` schema 2 with per-value provenance, while every v0.1.0 fan file still behaves exactly as before.
**Verified:** 2026-11-11
**Status:** human_needed
**Re-verification:** No, initial verification

All five ROADMAP success criteria (with the amended wording for 2, 3 and 5) are met in the code. Nothing failed. The status is `human_needed` because a few items are judgment calls (visual look, plausibility of assumed numbers, README) and because the three open code-review warnings need a maintainer disposition. One of them (WR-01) is a real validation hole worth closing before Phase 6.

## Goal Achievement

### Observable Truths (ROADMAP Phase 4 success criteria, amended wording)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A schema 2 fan sweeps up then down through the firmware's fan entity; `sil status` / `sil watch` show separate, monotone, interpolated up and down curves, lower on the way down at the same duty | VERIFIED | `model.Step`/`advance` picks `upCurve()` or `downCurve()` by sticky direction (`internal/model/fan.go:212-241`); `Curve.At` interpolates linearly (`curve.go:28-42`); loader enforces strictly increasing duty and non-decreasing rpm (`v2.go:404-433`). Re-ran e2e this session: `TestModelV2Curves` PASS (rpm 599.18 at 20 %, 999.15 at 40 %, interpolated 1150 at 50 %, down 1200 at 60 % vs 1300 up) and `TestLibraryV2/curves` PASS on the shipped Arctic (820/1150/1480 up, 1100/780 down, rpm event on `watch`) |
| 2 | From rest: 0 rpm until duty is at or above start duty, spin-up after the start delay; once running it keeps turning down to stop duty, below that stops and coasts with its own dynamics; a "never stops" fan holds its floor rpm at 0 % duty | VERIFIED (see WR-01 caveat) | `advance` start/stop branch: `duty >= p.StartDuty`, delay accumulated in `State.StartElapsed`, step split at expiry; stop immediate, `segment` uses `CoastTau` for a stopped fan (`fan.go:184-203, 258-266`); never-stops = `stop.duty = 0` with a curve point at duty 0 (`v2.go:283-288, 318-320`). Model tests PASS (`TestStartDelaySplitsTheStep`, `TestStartAtExactStartDuty`, `TestStopIsImmediateAndCoasts`, `TestNeverStopsForm`). e2e `TestLibraryV2/start_stop` PASS: rest at duty 0.08, start at exactly 0.10 after 390-410 ms, keeps running at 0.06, stops at once at 0.03 and coasts with ratio exp(-dt/6), Noctua holds 450 rpm at duty 0 |
| 3 | The firmware-reported RPM moves in 30 rpm pulse steps with the defined step signature; noise seeded and set by `noise_rpm`; same seed and duty sequence give identical traces regardless of tick timing; bus protocol still 1 | VERIFIED | `Plant.stepTach` counts whole pulses from the exact rpm integral; `Frame()` sends pulses*60/(BoardPPR*elapsed) (`plant.go:83-152`). Noise is a stateless keyed hash of (seed, port, 100 ms bucket) (`noise.go`), no `math/rand` in non-test model code. S7a (hermetic firmware-window emulation) `TestS7aFirmwareWindowKeepsSteps` PASS, asserting mean bias, spread 12..90, side share >= 25 %, and at most three residues mod 30 with one 0. `TestTachIgnoresTickTiming`, `TestNoiseIsKeyed`, `TestNoiseAmplitudeIsGateStd` PASS. S7b through the real firmware (`tests/test_tach_steps.py`) asserts mean within 5, spread 12..90, >= 2 levels, both sides >= 25 %, status tach only 1080/1110, `bus_proto == 1`; the executor reported it green in `make test-contract` 7/7 (not re-run here, see Behavioral Spot-Checks) |
| 4 | Each port shows airflow (m3/h, proportional to rpm) and power (W, proportional to rpm^3) from datasheet values in `sil status`, `sil watch` and the page | VERIFIED | `Datasheet.Airflow/Power` and `Plant.Outputs` (`derive.go`); engine fills `PortState.AirflowM3h/PowerW` (`engine.go:356`) and a `load` event (`engine.go:425-437`); `control.PortStatus` carries `tach_rpm`, `airflow_m3h`, `power_w` as always-present nullable fields (`control.go:140-148`); supervisor copies them (`supervisor.go:740`); page has TACH RPM / M3/H / W readouts with `—` for null (`ui/web/index.html:62-64`, `view.js`). e2e `TestLibraryV2/loads_and_tach` PASS: airflow = 95.7*rpm/1800 and power = 1.2*(rpm/1800)^3 to 1e-6 |
| 5 | Every v0.1.0 schema 1 file loads and behaves exactly as in v0.1.0 (shipped files become schema 2, v0.1.0 files hash-pinned in `testdata/fans-v0.1.0`, existing assertions pass against them); a schema 2 unknown key is rejected with file:line:col; `sil fans` lists schema version and provenance of every value | VERIFIED | Independently confirmed: sha256 of both `testdata/fans-v0.1.0/*.toml` equals `git show v0.1.0:fans/<file>` (`d9fb55c7...`, `d7dacb32...`), and `TestV010FixturesArePinned` guards them. `internal/model/fan_v010_test.go` holds a frozen copy of the v0.1.0 `Step` (I diffed its body against `git show v0.1.0:internal/model/fan.go`: identical logic) and `TestStepMatchesV010` / `TestPlantMatchesV010ForLegacyParams` compare with `==` over random parameters, scripts, dts: PASS. Shipped fans are `schema = 2` with a complete `[provenance]` table. Loader is strict (`DisallowUnknownFields`, errors carry line/col via `decodeError`). e2e `TestFanTomlUnknownKey` (schema 2 shipped and schema 1 fixture) PASS, `TestFansProvenance` PASS, `TestLibraryV2/fans_schema2` PASS (schema 2, provenance counts: Arctic 4 datasheet / 9 assumed, Noctua 3 / 10) |

**Score:** 5/5 truths verified (0 present-but-behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/model/{fan,curve,plant,tach,noise,derive}.go` | Plant seam, curves, start/stop/coast, tach accumulator, keyed noise, fan laws | VERIFIED | Substantive (hundreds of lines of logic, no stubs), wired through `engine.go` (`model.NewPlant`, `plant.Step/Frame/Current/Reading/Outputs`) |
| `internal/fanlib/v2.go`, `v1.go`, `fanlib.go` | Strict schema 1/2 loader with provenance and located errors | VERIFIED | Schema probe dispatches v1/v2; required keys, finiteness, ranges, curve shape, floor rule, provenance in fixed order |
| `fans/*.toml` | Shipped fans as schema 2 with provenance | VERIFIED | Both `schema = 2`, `[sources]`, `[provenance]` for every value, `noise_rpm = 20` |
| `testdata/fans-v0.1.0/*` | Byte-identical v0.1.0 fixtures | VERIFIED | Hashes match `git show v0.1.0:...` |
| `internal/control/control.go`, `internal/events/events.go`, `internal/supervisor/supervisor.go` | tach_rpm / airflow_m3h / power_w in status, events | VERIFIED | Additive nullable fields, Status schema stays 1 |
| `internal/cli/cli.go`, `internal/lifecycle/lifecycle.go` | `sil fans` summary/verbose, `sil up --seed`, DefaultSeed | VERIFIED | `--seed` flag, `specSeed`, `DefaultSeed = 0x5EED`, supervisor puts it into every engine config (`supervisor.go:220`) and into `sim.seed` in status |
| `ui/web/{index.html,js/*}` | Tach trace, three readouts | VERIFIED | Ring has a `tach` array, plot has a fourth series, readouts use `data-src` bindings. `TestScopeTach`, `TestTachFixedAxis`, `TestTachReadoutFollowsEvents` PASS under headless Chrome this session |
| `tests/test_tach_steps.py` | S7b contract test | VERIFIED (exists, substantive) | Runs the real firmware via aioesphomeapi; not re-run here |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| fan.toml (schema 2) | `model.Params` | `fanlib.loadV2` | WIRED | Used by `Load`/`loadFrom`; e2e proves a schema 2 file drives a port |
| `model.Plant` | engine tick and bus frames | `p.plant.Step(p.duty, dt)` (`engine.go:627`), `p.plant.Frame()` (`engine.go:1093`), `Current()` for hello (`engine.go:763`) | WIRED | The old `model.Step`/`model.Initial` call sites are gone from non-test code |
| `--seed` | every plant | `lifecycle.specSeed` -> spec.json -> `supervisor.engCfg.Seed` -> `model.NewPlant(..., cfg.Seed)` (also on fan swap, `engine.go:867`) | WIRED | Kept across SimRestart (same `engCfg`) |
| Plant outputs | status / watch / page | `PortState` -> `control.PortStatus`; `tach` and `load` events -> `store.js` -> `view.js` | WIRED | e2e and uitest cover all three surfaces |
| Pulse tach | firmware | Bus frame value at 50 ms cadence, protocol still v1 | WIRED | S7a hermetic and S7b firmware contract tests |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| Page TACH RPM readout | `ports.{i}.tach_rpm` | `Plant.Reading()` (1 s gate of the pulse count) via `tach` events | Yes (e2e: whole multiples of 30) | FLOWING |
| Page AIRFLOW / POWER | `airflow_m3h`, `power_w` | `Plant.Outputs()` from `[datasheet]` and shaft rpm via `load` events | Yes (e2e: matches fan laws to 1e-6); null for a fan without datasheet | FLOWING |
| Scope tach trace | `ring.tach` | `sample` frames carrying tach, live `tach` events | Yes (uitest `TestScopeTach`) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Fast gate (fmt, vet, race unit tests, C++ tests) | `make check` | exit 0; all packages `ok`, txcap 66 passed | PASS |
| Model start/stop/curve/tach/noise/S7a/v0.1.0-differential tests | `go test -run 'S7\|Quant\|Step\|Noise\|Tach\|Curve\|Coast\|Start\|NeverStops\|V010' ./internal/model/` | all PASS | PASS |
| Real-firmware e2e on shipped library | `go test -tags sil_e2e -run 'TestLibraryV2\|TestModelV2Curves\|TestFansProvenance\|TestFanTomlUnknownKey' ./e2e/` | ok, 149 s | PASS |
| Page tach trace and readout | `go test -tags sil_ui -run 'TestScopeTach\|TestTachReadoutFollowsEvents\|TestTachFixedAxis' ./uitest/` | ok | PASS |
| v0.1.0 fixture bytes | `sha256sum` vs `git show v0.1.0:fans/...` | identical | PASS |
| Firmware-visible steps (S7b) | `make test-contract` | not re-run (15+ min); executor reports 7/7 green. The hermetic S7a emulation of the same firmware window passes here | SKIP (covered by S7a plus executor evidence) |

Spawned instances were checked afterwards: no leftover `sil` or firmware processes.

### Probe Execution

Step 7c: SKIPPED. The phase declares no `scripts/*/tests/probe-*.sh` probes.

### Requirements Coverage

Every requirement ID from the PLAN frontmatter is accounted for in REQUIREMENTS.md, and REQUIREMENTS.md maps no additional Phase 4 ID that no plan claimed (no orphans).

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| MODEL-05 | 04-01, 04-08, 04-09 | Separate up/down curves, monotone, interpolated | SATISFIED | Truth 1 |
| MODEL-06 | 04-02, 04-08, 04-09 | Start at or above start duty after delay; stop below separate stop duty (amended wording) | SATISFIED | Truth 2; `TestStartAtExactStartDuty`, e2e start at exactly 0.10 |
| MODEL-07 | 04-02, 04-08, 04-09 | Coast-down with own dynamics; never-stops floor at 0 % | SATISFIED | Truth 2; coast ratio e2e, Noctua 450 rpm e2e |
| MODEL-08 | 04-01, 04-02, 04-03, 04-08 | Explicit `schema = 2`; schema 1 unchanged; unknown keys rejected | SATISFIED | Truth 5 |
| MODEL-09 | 04-04, 04-06, 04-07, 04-09, 04-10 | Pulse accumulator, 30 rpm steps, protocol stays 1 | SATISFIED | Truth 3 |
| MODEL-10 | 04-04, 04-05, 04-08, 04-10 | Deterministic seeded noise, amplitude from fan.toml | SATISFIED | Truth 3; `sil up --seed`, `sim.seed` in status |
| MODEL-11 | 04-06, 04-07, 04-08, 04-09, 04-10 | Airflow and power in status, watch, page | SATISFIED | Truth 4 |
| MODEL-12 | 04-03, 04-08, 04-10 | Provenance on every value; `sil fans` shows schema and provenance | SATISFIED | Truth 5; `TestFansProvenance`, loader rule (f) |

### CONTEXT Decisions Honoured (spot check of amendments)

| Decision | Status | Evidence |
|----------|--------|----------|
| D-01 shipped fans schema 2, v0.1.0 files kept as fixtures | Honoured | See Truth 5 |
| D-03 amended: noise on tach path only, `noise_rpm = 20` (std of a 1 s reading) | Honoured | `plant.go:92-99` adds noise only to the pulse count; shaft stays noise-free (`TestOutputsAreNoiseFree`); both files `noise_rpm = 20.0` |
| D-09 `coast_tau_s` required, no fallback | Honoured | `v2.go:174` required key; schema 2 never leaves `CoastTau` zero |
| D-10 never-stops = `stop.duty = 0` plus curve point at 0, no mode flag | Honoured for the up curve (see WR-01) | `v2.go:283-288` |
| D-11 amended: start boundary at or above | Honoured | `duty >= p.StartDuty`; e2e uses exactly 0.10 |
| D-13 amended: `tach_rpm` for ideal tach is the 1 s shaft mean, null only in the first second | Honoured | `Plant.stepIdeal`, `Reading()` doc; `TestIdealTachIsShaft` |
| D-15 default seed constant mixed with port, `--seed` override, kept across restarts | Honoured | `DefaultSeed`, `z(seed, port, bucket)` |
| D-16 amended: `—` only for the three new readouts, `--` elsewhere | Honoured | `view.js` `NULL_TEXT` has only tach/flow/watt |
| D-12 no kick | Honoured | No kick keys in the schema (rejected as unknown) |

### Anti-Patterns Found

Scanned the 69 files changed since `v0.1.0` (code, tests, fans, UI, README, testdata).

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | - | No `TBD`/`FIXME`/`XXX`, no `TODO`/`HACK`, no stub returns feeding output | - | - |
| `internal/fanlib/v2.go` | 283-288 | WR-01: floor rule only on `curve.up` | Warning | See below |
| `internal/fanlib/v2.go`, `internal/cli/cli.go` | 239-243, 469-494 | WR-02: control characters accepted, printed raw to stderr | Warning | Low impact, terminal escape injection via a hostile fan file |
| `internal/fanlib/v2.go` | 568-590 | WR-03: `locate` loses line numbers after an escaped quote plus bracket | Warning | Errors still reject the file; only the `:line` is lost |
| `internal/engine/engine.go` | 132-134 | IN-01 stale comment on `Config.Seed` | Info | None |
| `internal/model/plant.go` | 127-128 | IN-02 `Params()` aliases slices | Info | None today |
| `internal/fanlib/fanlib.go` | 139-141 | IN-03 schema key missing gives a v1 "unknown field" error | Info | Message quality only |

#### Weighing WR-01 against the must-haves

I confirmed by reading `validateV2` that rule (e) tests only `upCurve[0]`. A file with `stop.duty = 0`, `curve.up = [(0,450),(1,2000)]` and `curve.down = [(0.5,0),(1,2000)]` loads and gives `BelowMin = "hold"`, `Running = true`, but the falling branch targets `downCurve().At(duty) = 0`, so the fan decays to 0 rpm while reporting `running`. That contradicts "a fan marked never stops holds its floor RPM at 0 % duty" for that file.

It does not fail Truth 2 as delivered, because:
- both shipped fans (Noctua: `curve.down` starts at 450) and every tested never-stops form satisfy the rule on both curves, and the e2e Noctua check passes;
- the defect is a missing validation on a contradictory, hand-written input, not a wrong behaviour of a valid fan.

It is a loader gap worth closing before Phase 6, because `fan-analyze fit` will start writing schema 2 files with both curves. I classify it as a WARNING with a maintainer decision requested, not a BLOCKER. The review's suggested fix (apply the duty-0 floor check to `curve.down` when present, plus a v2 test) is small. All six review findings are still `open` in `04-REVIEW-DISPOSITION.md`.

### Human Verification Required

1. **Page look of the stepped tach trace and readout row (coverage item D5).** Run `sil up`, open the page, sweep Fan 1. Expect a solid shaft trace plus a dimmer stepped tach trace in the port colour, and a readout row (TACH RPM, M3/H, W) that fits next to the existing readouts. Why human: visual layout and legibility.
2. **Plausibility of assumed fan numbers.** Interior curve points, the down curve, start delay 0.4 s, taus, `coast_tau_s = 6`, `noise_rpm = 20` and the Noctua 450 rpm floor are all `assumed` (D-03) and will be replaced by Phase 8 measurements (Arctic) or v0.3 (Noctua). Why human: judgment about physical plausibility. Assumption A11 (Rev 3.3 counts its tach at 30 rpm per pulse) is explicitly carried to Phase 7/8.
3. **README readability** for the new schema 2, provenance, tach, airflow/power and `--seed` sections. Why human: topics were checked mechanically only.
4. **Disposition of the review warnings** (WR-01 recommended before Phase 6; WR-02 and WR-03 optional). Why human: scope decision.

### Gaps Summary

No gaps: every ROADMAP success criterion and all eight requirements are met, verified against the code and by tests I re-ran (`make check`, the model suite, the shipped-library and curve e2e tests against the real firmware, and the page tach tests). Items left for a human are judgment calls only: the visual look, the plausibility of assumed numbers, README readability and the triage of the three review warnings. The status is `human_needed`, not `passed`, for that reason.

---

_Verified: 2026-11-11T14:30:00Z_
_Verifier: Claude (gsd-verifier)_

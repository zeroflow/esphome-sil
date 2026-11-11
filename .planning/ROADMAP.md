# Roadmap: esphome-sil

## Milestones

- ✅ **v0.1.0 MVP** — Phases 1-3.1 (shipped 2026-11-10) — [archive](milestones/v0.1.0-ROADMAP.md)
- 🚧 **v0.2.0 Real Fans** — Phases 4-8 (in progress)

## Phases

<details>
<summary>✅ v0.1.0 MVP (Phases 1-3.1) — SHIPPED 2026-11-10</summary>

- [x] Phase 1: Closed-Loop Fan Skeleton (13/13 plans) — completed 2026-11-10
- [x] Phase 2: Full Rev 3.3 Bench (8/8 plans) — completed 2026-11-09
- [x] Phase 3: Live Web UI (12/12 plans) — completed 2026-11-10
- [x] Phase 3.1: Address tech debt (INSERTED) (3/3 plans) — completed 2026-11-10

Full phase details: `milestones/v0.1.0-ROADMAP.md`. Requirements: `milestones/v0.1.0-REQUIREMENTS.md`. Phase directories: `milestones/v0.1.0-phases/`.

</details>

## 🚧 v0.2.0 Real Fans

**Milestone Goal:** The simulated fan behaves like a real, measured fan, including when it breaks, and its parameters come from measuring real fans on a Rev 3.3 board instead of assumptions.

**Overview:** v0.2.0 moves from an assumed fan to a measured one in five vertical slices. Phase 4 replaces the linear model with fan model v2 behind one `model.Plant` and an explicit `fan.toml` schema 2, so every later phase builds on the same deterministic code path while schema 1 files keep behaving as in v0.1.0. Phase 5 puts runtime fault injection on top of that model, end to end from CLI/JSON to the web page. Phase 6 proves the whole analysis chain (recording, replay, fit, verify, report) offline against synthetic fans, in parallel with Phase 5, so no bench time is spent before the acceptance mechanism works. Phase 7 brings in the real board: hardware spikes open the phase and settle the instrument design, then the fail-safe measuring firmware, the Go native API client and `sil fan-analyze run` follow, with the run-and-fit chain first proven against the host firmware. Phase 8 is the human-gated calibration campaign: the Arctic P12 is measured, fitted and verified on a held-out recording, closing assumption A1 (Noctua deferred to v0.3).

**Phase Numbering:**
- Integer phases (4, 5, 6): Planned milestone work, continuing from v0.1.0 (last phase 3.1)
- Decimal phases (5.1, 5.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 4: Fan Model v2** - Nonlinear up/down curves, start/stop and coast-down, quantised noisy tach, airflow and power, on an explicit `fan.toml` schema 2 with provenance; schema 1 unchanged
- [ ] **Phase 5: Fault Injection** - Stuck, frozen, late/no start, tach faults, unplug/replug and degraded fans, switched at runtime from CLI/JSON and the web page, visible in status, watch and the scopes
- [ ] **Phase 6: fan-analyze Offline Core** - Versioned recordings, model replay, fit to schema 2, held-out verify verdict and a self-contained `report.html`, all proven on synthetic fans with no hardware
- [ ] **Phase 7: Measuring Firmware and Board Link** - Hardware spikes, fail-safe Rev 3.3 measuring firmware, Go native API client and `sil fan-analyze run`, with synthetic-fan recovery through the host firmware
- [ ] **Phase 8: Arctic P12 Calibration** - Arctic P12 measured on the real board, fitted, verified on a held-out recording within the stated tolerance and promoted into the library; A1 closed

## Phase Details

### Phase 4: Fan Model v2

**Goal**: A simulated fan behaves like a real one: separate up and down duty→RPM curves, distinct start and stop thresholds with start delay and coast-down, a tach that quantises and jitters like the firmware's `pulse_counter` window, and airflow and power from datasheet values. All of it is driven by an explicit `fan.toml` schema 2 with per-value provenance, while every v0.1.0 fan file still behaves exactly as before.
**Mode:** mvp
**Depends on**: v0.1.0 (Phase 3.1); first phase of this milestone
**Requirements**: MODEL-05, MODEL-06, MODEL-07, MODEL-08, MODEL-09, MODEL-10, MODEL-11, MODEL-12
**Gate**: none (offline, automated; no hardware)
**Research**: standard patterns. One in-phase check (spike S7): confirm that the 30 rpm steps from the Go pulse accumulator survive the firmware's 1 s mean; analytic quantisation is the fallback
**Open assumption (A11, to be confirmed on hardware in Phase 7/8)**: 30 rpm per pulse assumes that Rev 3.3 counts its tach like Rev 1.0-3.1 (`pulse_counter` with `multiply: 0.5`, `update_interval: 1s`). The Rev 3.3 hardware YAML read at plan revision (2026-11-10) has no `pulse_counter`. The sim side follows `sim_sensor.h` (1 s time-weighted mean), and the board constants are single values in `internal/model`.
**Success Criteria** (what must be TRUE):
  1. With a schema 2 fan on a port, sweeping duty up and then down through the firmware's fan entity shows RPM in `sil status` and `sil watch` following the fan's separate up and down curves: monotone in each direction, interpolated between table points, and different on the way down than on the way up at the same duty.
  2. From standstill the fan stays at 0 rpm until duty reaches its start duty (at or above it; amended 2026-11-11 at plan revision), then spins up after its start delay; once running it keeps turning down to its lower stop duty and below that coasts to 0 with its own dynamics. A fan marked "never stops" holds its floor RPM at 0 % duty.
  3. The RPM the firmware reports (and Home Assistant shows) moves in the fan's pulse quantisation steps (30 rpm per pulse at ppr 2), shown by a defined step signature (amended 2026-11-10 at plan revision). With a noise-free tach at a steady speed between pulse-grid points, the firmware's 1 s readings average to the speed within 5 rpm, spread at least 12 and at most 90 rpm peak to peak, and split around their mean with at least a quarter of them on each side; an unquantised tach would show one constant value. In the hermetic emulation of the firmware window without arrival jitter, every reading lies on the 30 rpm grid or on one phase-offset copy of it (at most three residues modulo 30, one of them 0). The noise is seeded, with the amplitude set in `fan.toml` (`noise_rpm`, the standard deviation of a 1 s reading), and two runs with the same seed and duty sequence give identical traces even when tick timing differs. The sim-bus protocol is still version 1, and the contract test shows the firmware-visible steps.
  4. Each port shows airflow (m³/h, ∝ RPM) and power (W, ∝ RPM³) derived from the fan's datasheet values in `sil status`, `sil watch` and on the web page.
  5. Every v0.1.0 schema 1 `fan.toml` loads and behaves exactly as in v0.1.0 (amended 2026-11-10 at plan revision). The shipped files become schema 2 (D-01), and the v0.1.0 files live on byte for byte as hash-pinned fixtures in `testdata/fans-v0.1.0`. The existing v0.1.0 test assertions pass unchanged against those pinned fixtures (D-02). A schema 2 file with an unknown key is rejected with its file:line:col, and `sil fans` lists each fan's schema version and the provenance (assumed / datasheet / measured / unobservable) of every value.

**Plans**: 10 plans (8 waves: 01 → 02 ∥ 03 → 04 ∥ 05 → 06 → 07 → 08 → 09 → 10)

Plans:
**Wave 1**
- [ ] 04-01-PLAN.md — Tracer: a schema 2 curve fan from fan.toml drives a port through model.Plant and the engine; separate up/down curves; strict v2 loader with provenance

**Wave 2** *(blocked on Wave 1 completion)*
- [ ] 04-02-PLAN.md — Start delay, immediate stop, coast tau, never-stops form; frozen v0.1.0 differential test and pinned v0.1.0 fixtures
- [ ] 04-03-PLAN.md — `sil fans` shows schema and per-value provenance (JSON + stderr summary/--verbose); loader errors located per table, tach/datasheet/measured tables (measured tolerance carries provenance)

**Wave 3** *(blocked on Wave 2 completion)*
- [ ] 04-04-PLAN.md — Pulse-accumulator tach in the bus frames, keyed-hash seeded noise (amplitude = std of a 1 s reading), spike S7a with the step signature, math/rand lint
- [ ] 04-05-PLAN.md — `sil up --seed`: seed through spec.json to every plant, sim.seed in status, run.json, kept across restarts

**Wave 4** *(blocked on Wave 3 completion)*
- [ ] 04-06-PLAN.md — tach_rpm (1 s gate reading; the 1 s shaft mean for an ideal tach), airflow_m3h and power_w in status, watch and history (null when unknown)

**Wave 5** *(blocked on Wave 4 completion)*
- [ ] 04-07-PLAN.md — Page: stepped tach trace, TACH RPM / AIRFLOW / POWER readouts (`—` for null), parity and layout tests

**Wave 6** *(blocked on Wave 5 completion)*
- [ ] 04-08-PLAN.md — Shipped fans rewritten as schema 2 with provenance and 20 rpm tach noise; exact tests pinned to v0.1.0 fixtures; schema 2 library and typo checks on a default `sil up`

**Wave 7** *(blocked on Wave 6 completion)*
- [ ] 04-09-PLAN.md — e2e acceptance on the shipped library through the real firmware: up/down curves, start at start duty after the delay, coast, never-stops floor, loads and tach in status (split from 04-08 at plan revision iteration 2)

**Wave 8** *(blocked on Wave 7 completion)*
- [ ] 04-10-PLAN.md — Spike S7b in the contract test (firmware-visible steps with the step signature via aioesphomeapi); real-bench page check; README; all gates

**UI hint**: yes

### Phase 5: Fault Injection

**Goal**: Developer can break a simulated fan the ways real fans break, switch each fault on and off at runtime from the CLI, the JSON API and the web page, and see when and where it was active, under fixed lifecycle rules that never leave a stale fault behind
**Mode:** mvp
**Depends on**: Phase 4
**Requirements**: FAULT-02, FAULT-03, FAULT-04, FAULT-05, FAULT-06, FAULT-07, FAULT-08, FAULT-09, FAULT-10, FAULT-11
**Gate**: none (offline, automated; no hardware)
**Research**: standard patterns. Extracting the supervisor's shared `applyChange` path must keep the Phase 3.1 Set/SimRestart concurrency fixes intact
**Success Criteria** (what must be TRUE):
  1. `sil fault set <port> <kind>` makes the firmware see the fault on that port: stuck/blocked (0 rpm at any duty), frozen (RPM held at its current value), late start (raised start duty, longer start delay), no start from standstill, and degraded (lower max RPM, slower response, with the given parameters).
  2. Tach and wiring faults behave like the real failure. A dead tach reports 0 rpm while the fan keeps spinning (airflow and power stay non-zero), wrong ppr scales the firmware's RPM by ppr/2, and intermittent pulse loss drops counts. An unplugged fan shows no RPM and no load, and replugging restores it.
  3. `sil fault set/clear/list` and the matching JSON API rows (new `control.Operations` entries, covered by the existing parity and CSRF checks) set, clear and list faults per port. Refused input (unknown kind, bad port, out-of-range parameter) answers with a registered error code. Active faults appear in `sil status`, and every change emits a `fault` event in `sil watch`.
  4. Faults follow the fixed lifecycle: kept across `sil fw restart`, cleared by `sil sim restart` and `sil down`, and cleared with a `fault` event when that port's fan is swapped.
  5. On the web page the developer sets and clears faults per port through the same API the CLI uses, and each port's uPlot scope marks the intervals in which a fault was active on that port.

**Plans**: TBD
**UI hint**: yes

### Phase 6: fan-analyze Offline Core

**Goal**: The whole analysis chain (recording, replay, fit, verify, report) works offline against synthetic fans, so the acceptance mechanism is proven on the sim before any bench time is spent
**Mode:** mvp
**Depends on**: Phase 4 (can run in parallel with Phase 5)
**Requirements**: ANLZ-02, ANLZ-03, ANLZ-04, ANLZ-05, ANLZ-06
**Gate**: none (offline, automated; no hardware)
**Research**: deeper research needed. Open questions: which parameters (tau, start delay, coast-down) can be identified from counts, whether Nelder-Mead holds up (LM with numeric Jacobian as fallback), and the shared estimator plus the `max(floor, k × repeatability)` tolerance method
**Success Criteria** (what must be TRUE):
  1. A recording of a synthetic fan taken with the model-backed fake board is a versioned NDJSON file (sequence, device-stamped samples, metadata) that `fit`, `verify` and `report` all read offline; a recording with an unknown version or a malformed line is refused with a located error.
  2. Replaying a recording's duty sequence through the model gives the same trace, sample for sample, as the running engine on the Manual clock (one shared `model.Plant`).
  3. `sil fan-analyze fit <recording>` writes a schema 2 `fan.toml` from a template that the strict loader accepts, marks the parameters the recording cannot observe as `unobservable`, and never writes into the shipped library; for a noisy synthetic fan the fitted parameters land close to the known ones.
  4. `sil fan-analyze verify <fan.toml> <held-out recording>` prints a JSON pass/fail verdict against the stated tolerance `max(floor, k × repeatability)`. Measured and replayed series go through the same estimator, so quantisation alone never fails a run, and a deliberately wrong `fan.toml` fails.
  5. `sil fan-analyze report` writes one self-contained `report.html` (vendored uPlot inlined, no network reference) with measured vs. model plots for sweep, step and start/stop, the fitted parameters and the verdict, and it opens offline in a browser.

**Plans**: TBD
**UI hint**: no

### Phase 7: Measuring Firmware and Board Link

**Goal**: A real Rev 3.3 board can be measured safely and repeatably. Hardware spikes settle the instrument design, a measuring firmware fails safe at 100 %, a Go native API client drives it, and `sil fan-analyze run` writes recordings. The whole run-and-fit chain is first proven against the host firmware on synthetic fans.
**Mode:** mvp
**Depends on**: Phase 6 (recording format, fit). The MEAS-01 spikes and the native API client have no dependency on Phases 5 or 6 and may start early when the bench is free
**Requirements**: MEAS-01, MEAS-02, MEAS-03, MEAS-04, MEAS-05, MEAS-06, ANLZ-07
**Gate**: hardware + human. A real Rev 3.3 board must be on the bench for the MEAS-01 spikes (which open the phase), the first flash and the fail-safe checks. The one-time toolchain fetch is the documented locality exception (accepted 2026-11-10)
**Research**: deeper research needed. Open questions: instrument design (cumulative counts first, ISR edge timestamps only if spike S1 shows counts are insufficient), ESP32-S2 PCNT unit count and ISR latency, device-stamped sample delivery over the native API, native API message IDs from the pinned 2026.10.1 `api.proto`, and how Rev 3.3 counts its tach (Phase 4 assumption A11: `multiply: 0.5`, 1 s window, 30 rpm per pulse)
**Success Criteria** (what must be TRUE):
  1. The spike results from a real Rev 3.3 board are recorded (tach count fidelity under Wi-Fi load, achievable sample rate, cold-cache ESP32-S2 build, fail-safe behaviour), and the measuring firmware's design follows from them.
  2. `make analyze-firmware` builds the measuring firmware (`analyze/firmware/`) through the pinned ESPHome image, and after the documented one-time toolchain fetch a rebuild with the network off succeeds. On the board, the firmware sets all fans to 100 % on boot and whenever the client disconnects, with no host attached and no flash writes.
  3. On command the board runs step, start and stop sequences itself and delivers device-timestamped, sequence-numbered tach counts at the rate the spikes established, with one stock `pulse_counter` channel alongside as a cross-check.
  4. The Go native API client (Noise only, answers keepalive pings, entities by name) passes a contract test against the host firmware `sil up` runs. `sil fan-analyze run` refuses to start unless preflight sees plausible RPM on the chosen port and 0 on the others, then runs the sweep/step/start-stop campaign and writes a recording; aborting a run at any point leaves every fan at 100 %.
  5. Synthetic fans measured against the sim, on the fake board and through the host firmware over the native API, fit back to their known parameters before any real-fan recording is fitted.

**Plans**: TBD
**UI hint**: no

### Phase 8: Arctic P12 Calibration

**Goal**: The Arctic P12 PWM PST in the fan library is measured, not assumed. Its fitted schema 2 `fan.toml` replays a held-out real recording within the stated tolerance, which closes assumption A1 and meets the milestone's done criterion.
**Mode:** mvp
**Depends on**: Phase 7
**Requirements**: CAL-01, CAL-02
**Gate**: hardware + human checkpoints: (1) bench setup, with the fan SKU recorded (repo cites ACFAN00120A, research fetched ACFAN00170A; both pages are identical, so Phase 4 deferred the SKU here, D-04 amendment) and a single fan on the chosen port; (2) preflight; (3) device-side run including the repeatability pass; (4) maintainer review of the report
**Research**: none beyond Phases 6 and 7. Budget one model iteration, inserted as a decimal phase, in case the real fan shows behaviour the v2 model cannot express
**Success Criteria** (what must be TRUE):
  1. The Arctic P12 is measured on the real Rev 3.3 board with a repeatability pass, and the tolerance (`floor` and `k` in `max(floor, k × repeatability)`) is recorded before the held-out recording is taken.
  2. `fans/arctic-p12-pwm-pst.toml` is replaced by the measured schema 2 file through a reviewed change (never auto-promoted). `sil fans` shows every value the campaign could observe as `measured` and the rest as `datasheet` or `unobservable`. The test suite and `make test-contract` pass with the new file, and A1 is closed in STATE.md.
  3. `sil fan-analyze verify` on a held-out recording that was not used in the fit returns pass within the stated tolerance.
  4. The maintainer checks `report.html` (measured vs. model for sweep, step and start/stop, fitted parameters, verdict) and accepts it; the Noctua NF-A12x25 entry stays marked `assumed` (CAL-03 deferred to v0.3).

**Plans**: TBD
**UI hint**: no

## Progress

**Execution Order:**
Phases execute in numeric order: 4 → 5 → 6 → 7 → 8. After Phase 4, Phases 5 and 6 are independent and may run in parallel. Phase 7 needs Phase 6, and Phase 8 needs Phase 7.

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 4. Fan Model v2 | v0.2.0 | 0/10 | Planned | - |
| 5. Fault Injection | v0.2.0 | 0/TBD | Not started | - |
| 6. fan-analyze Offline Core | v0.2.0 | 0/TBD | Not started | - |
| 7. Measuring Firmware and Board Link | v0.2.0 | 0/TBD | Not started | - |
| 8. Arctic P12 Calibration | v0.2.0 | 0/TBD | Not started | - |

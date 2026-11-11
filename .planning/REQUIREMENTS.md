# Requirements: esphome-sil

**Defined:** 2026-11-10
**Milestone:** v0.2.0 Real Fans
**Core Value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.

## v0.2.0 Requirements

### Fan Model (MODEL)

- [x] **MODEL-05**: A fan's duty→RPM relation follows separate measured up and down curves (monotone, interpolated) instead of a straight line
- [x] **MODEL-06**: A stopped fan starts only at or above its start duty, after an optional start delay, and a running fan stops only below its separate stop duty (amended 2026-11-11 at plan revision: "only above" → "only at or above")
- [x] **MODEL-07**: A fan coasts down to 0 with its own dynamics when duty drops below the stop duty, and a fan marked "never stops" holds its floor RPM at 0 % duty
- [x] **MODEL-08**: `fan.toml` has an explicit `schema = 2` (curves, start/stop, dynamics, tach, datasheet, measured tables); every shipped schema 1 file still loads and behaves exactly as in v0.1.0; unknown keys stay rejected
- [ ] **MODEL-09**: The RPM the firmware sees comes from a pulse accumulator with the fan's ppr, so the 1 s `pulse_counter`-style window shows the real quantisation steps (30 rpm per pulse at ppr 2); sim-bus protocol stays version 1
- [ ] **MODEL-10**: RPM carries deterministic seeded noise (same seed and inputs give the same trace regardless of tick timing); noise amplitude comes from `fan.toml`
- [ ] **MODEL-11**: Developer sees per-port airflow (m³/h, ∝ RPM) and power (W, ∝ RPM³) derived from datasheet values, in status, watch and the web page
- [ ] **MODEL-12**: Every `fan.toml` value carries provenance (assumed / datasheet / measured / unobservable), and `sil fans` shows schema and provenance

### Fault Injection (FAULT)

- [ ] **FAULT-02**: Developer can make a fan stuck/blocked (RPM 0 at any duty) or frozen at its current RPM
- [ ] **FAULT-03**: Developer can give a fan a late start (raised start duty, longer start delay) or no start at all from standstill
- [ ] **FAULT-04**: Developer can inject tach faults: dead tach while the fan spins, wrong ppr (firmware RPM scaled by ppr/2), intermittent pulse loss
- [ ] **FAULT-05**: Developer can unplug a fan (no RPM, no load) and replug it at runtime
- [ ] **FAULT-06**: Developer can degrade a fan (lower max RPM, slower response) with parameters
- [ ] **FAULT-07**: Developer can set, clear and list faults per port with `sil fault set/clear/list` and the JSON API (new `control.Operations` rows); refused input gets a registered error code
- [ ] **FAULT-08**: Active faults appear in `sil status`, and every fault change emits a `fault` event in `sil watch`
- [ ] **FAULT-09**: Faults follow fixed lifecycle rules: cleared by `sil sim restart` and `sil down`, kept across a firmware restart, cleared (with an event) when that port's fan is swapped
- [ ] **FAULT-10**: Developer can switch faults per port from the web page, which stays a pure client of the same API
- [ ] **FAULT-11**: The page's uPlot scopes mark when a fault was active on that port

### fan-analyze Core (ANLZ)

- [ ] **ANLZ-02**: A measurement is stored as a versioned NDJSON recording (sequence, device-stamped samples, metadata) that fit, replay and report read offline
- [ ] **ANLZ-03**: Replaying a recording's duty sequence through the model gives the same trace as the running engine on the Manual clock (one shared `model.Plant`)
- [ ] **ANLZ-04**: `sil fan-analyze fit` turns a recording into a schema 2 `fan.toml` (written from a template, validated by the strict loader, never auto-promoted into the library), marking unobservable parameters
- [ ] **ANLZ-05**: `sil fan-analyze verify` replays a held-out recording against a fitted `fan.toml` and gives a JSON pass/fail verdict against a stated tolerance of the form `max(floor, k × repeatability)`, comparing both series through the same estimator
- [ ] **ANLZ-06**: `sil fan-analyze report` writes one self-contained `report.html` (inlined vendored uPlot, no CDN) with measured vs. model plots for sweep, step and start/stop, fitted parameters and the verdict
- [ ] **ANLZ-07**: The fitter recovers known parameters from recordings of synthetic fans measured against the sim (model-backed fake board and the host firmware), before any real hardware is used

### Measuring Firmware and Board Link (MEAS)

- [ ] **MEAS-01**: Before the firmware is built out, short spikes on a real Rev 3.3 board answer: tach count fidelity under Wi-Fi load, achievable sample rate, cold-cache ESP32-S2 build, fail-safe behaviour; results recorded and acted on
- [ ] **MEAS-02**: A measuring firmware for the Rev 3.3 board (`analyze/firmware/`) sets all fans to 100 % on boot and when the client disconnects, without the host and without flash writes
- [ ] **MEAS-03**: The measuring firmware runs step, start and stop sequences on the device and delivers fast, device-timestamped tach counts with sequence numbers; one stock `pulse_counter` channel stays as cross-check
- [ ] **MEAS-04**: `make analyze-firmware` builds it through the pinned ESPHome image; the one-time toolchain fetch is a documented locality exception, later builds work offline
- [ ] **MEAS-05**: A Go native API client (Noise only, answers keepalive pings, entities by name) talks to the board and is contract-tested against the host firmware `sil up` runs
- [ ] **MEAS-06**: `sil fan-analyze run` preflights the board (plausible RPM on the chosen port, 0 on the others), runs a sweep/step/start-stop campaign and writes a recording; an aborted run leaves the fans at 100 %

### Calibration Campaign (CAL)

- [ ] **CAL-01**: The Arctic P12 PWM PST is measured on the real board (with a repeatability pass), fitted, and its library `fan.toml` replaced by the measured schema 2 file; assumption A1 closed
- [ ] **CAL-02**: The fitted Arctic P12 passes `fan-analyze verify` on a held-out recording within the stated tolerance, and the report is reviewed by the maintainer

## Future Requirements

Deferred. Tracked but not in the current roadmap.

- **CAL-03**: Noctua NF-A12x25 measured and fitted, A2 closed (v0.3; entry stays `assumed`, research suggests it stops ~14 % and restarts ~18 %)
- **ANLZ-08**: Fit uncertainty (confidence intervals per parameter)
- **ANLZ-09**: Wear check: re-measure a fan and diff against its baseline
- **MEAS-07**: ISR edge-timestamp tach component (only if MEAS-01 shows counts are insufficient)
- **REAL-01**: Product modules on the sim: fan control core and curves (v0.3)
- **THERM-01**: Rack thermal model and stimulus profiles: noise, ramp, recorded lists (v0.4)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Fault profile files (`fans/_faults/`), Lua | Runtime-switched faults are enough for v0.2.0 |
| Scheduled faults ("stuck at t=30 s") | Belongs to the v0.4 stimulus work |
| Measured airflow and power | Rev 3.3 has no current or airflow sensing; datasheet + fan laws |
| Plaintext native API | Noise only, matches the product |
| Pulse-level tach simulation on the sim-bus | Pulse accumulator in Go reproduces the firmware-visible quantisation without a protocol change |
| Go SVG report renderer | Inline vendored uPlot instead |
| Time factor | Not needed for v0.2.0 |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| MODEL-05 | Phase 4 | Complete |
| MODEL-06 | Phase 4 | Complete |
| MODEL-07 | Phase 4 | Complete |
| MODEL-08 | Phase 4 | Complete |
| MODEL-09 | Phase 4 | Pending |
| MODEL-10 | Phase 4 | Pending |
| MODEL-11 | Phase 4 | Pending |
| MODEL-12 | Phase 4 | Pending |
| FAULT-02 | Phase 5 | Pending |
| FAULT-03 | Phase 5 | Pending |
| FAULT-04 | Phase 5 | Pending |
| FAULT-05 | Phase 5 | Pending |
| FAULT-06 | Phase 5 | Pending |
| FAULT-07 | Phase 5 | Pending |
| FAULT-08 | Phase 5 | Pending |
| FAULT-09 | Phase 5 | Pending |
| FAULT-10 | Phase 5 | Pending |
| FAULT-11 | Phase 5 | Pending |
| ANLZ-02 | Phase 6 | Pending |
| ANLZ-03 | Phase 6 | Pending |
| ANLZ-04 | Phase 6 | Pending |
| ANLZ-05 | Phase 6 | Pending |
| ANLZ-06 | Phase 6 | Pending |
| ANLZ-07 | Phase 7 | Pending |
| MEAS-01 | Phase 7 | Pending |
| MEAS-02 | Phase 7 | Pending |
| MEAS-03 | Phase 7 | Pending |
| MEAS-04 | Phase 7 | Pending |
| MEAS-05 | Phase 7 | Pending |
| MEAS-06 | Phase 7 | Pending |
| CAL-01 | Phase 8 | Pending |
| CAL-02 | Phase 8 | Pending |

**Coverage:**
- v0.2.0 requirements: 32 total
- Mapped to phases: 32 ✓
- Unmapped: 0

ANLZ-07 sits in Phase 7, not Phase 6: its host-firmware leg needs the Go native API client (MEAS-05). The fake-board recovery already runs in Phase 6 as the acceptance test of ANLZ-04.

---
*Requirements defined: 2026-11-10*
*Last updated: 2026-11-10 after v0.2.0 roadmap creation (traceability mapped to Phases 4-8)*

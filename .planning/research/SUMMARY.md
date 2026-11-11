# Project Research Summary

**Project:** esphome-sil — milestone v0.2.0 "Real Fans"
**Domain:** SiL fan plant model, fault injection, real-fan characterisation (fan-analyze)
**Researched:** 2026-11-10
**Confidence:** MEDIUM (HIGH for repo integration and ESPHome facts; MEDIUM/LOW for on-board timing and real fan behaviour)

## Executive Summary

v0.2.0 makes the simulated fan behave like a measured one. All model behaviour lives in `internal/model` behind one `model.Plant` used by both the engine and an offline `internal/replay`, so fitting and "sim replay within tolerance" share one deterministic code path. `fan.toml` moves to an explicit `schema = 2` (curves up/down, start/stop, dynamics, tach, datasheet, measured provenance) while schema 1 files keep loading unchanged. Faults are a model-level `Faults` value applied through the supervisor's existing change path. The companion `sil fan-analyze` is a CLI verb group exchanging a versioned NDJSON `Recording`, so fit, replay and report are built and tested offline against a model-backed fake board before any hardware time.

No new Go dependencies: the native API client grows out of `internal/apiclient` (Noise already done) with a hand-rolled protobuf wire helper and golden vectors from the image's `aioesphomeapi`; fitting is stdlib (isotonic regression + Nelder-Mead over the model replay); `report.html` inlines the vendored uPlot. The main risks are on the hardware side: the stock Rev 3.3 tach quantises at 30 rpm per pulse in a 1 s window, the native API carries no device timestamps, and the measuring firmware must be safe without the host.

## Key Findings

### Recommended Stack

- Native API client: extend `internal/apiclient` (extract `Conn` from `probe.go`), ~12 messages, IDs verified against 2026.10.1 `api.proto`; reject `mycontroller-org/esphome_api` (stale, heavy deps). Must answer pings (60 s ping / 150 s disconnect).
- Fitting: hand-rolled `internal/fit`; gonum v0.17.0 has no LM or isotonic regression. LM with numeric Jacobian is the fallback if Nelder-Mead is not robust on real data.
- `fan.toml` writer via `text/template` (go-toml encoder drops comments), validated through the strict loader.
- Report: one self-contained HTML inlining vendored uPlot 1.6.32 from `ui.FS()`.
- Measuring firmware: ESP32-S2 Arduino build via the pinned image needs a one-time network fetch then offline cache — **documented locality exception** (maintainer sign-off).
- Net new Go modules: zero.

### Expected Features

**Must have (table stakes):**
- Nonlinear duty→RPM with separate up/down curves; distinct start and stop thresholds; start delay / kick; coast-down; fans that do not stop at 0 %
- Tach realism: ppr, 1 s window quantisation (30 rpm/pulse at ppr 2), deterministic noise
- Airflow (∝ RPM) and power (∝ RPM³, underestimates below ~30-40 % speed) from datasheet
- Faults: stuck/blocked, late/no start, dead tach, wrong ppr (factor ppr/2 on firmware value), intermittent tach, unplug, degraded
- fan-analyze: duty sweep up/down with dwell, step response, start/stop threshold, ppr plausibility check against rated rpm (ppr cannot be detected from tach alone), fit, report, replay verdict

**Should have:** per-parameter provenance including explicit `unobservable`; fit uncertainty; repeatability pass.

**Defer / anti-features:** fault profile files, scheduled faults (v0.4 stimulus), measured airflow/power, Go SVG renderer, plaintext API.

### Architecture Approach

1. `model.Plant` (params, state, faults; zero values reproduce v0.1.0 behaviour so existing tests are the regression guard)
2. `model.Tach` integer pulse accumulator in Go — no sim-bus or firmware change, protocol stays v1; firmware's 1 s mean then reproduces `pulse_counter` quantisation (approximately; contract test documents it)
3. Faults: `FaultKinds` table; three new `control.Operations` rows (`faults`, `fault.set`, `fault.clear`); supervisor `Set` logic extracted into shared `applyChange` so the 03.1 concurrency fixes are not duplicated; Status gains `tach_rpm`, `faults`, `airflow_m3h`, `power_w`; events gain `fault`
4. `internal/analyze/{recording,measure,fit,compare,report}`, `internal/replay`; `Board` interface with `NativeBoard` and model-backed `FakeBoard`
5. Measuring firmware in `analyze/firmware/rev33-fan-analyze.yaml` with its own minimal pin table, built by `make analyze-firmware`

### Critical Pitfalls

1. **Tach too coarse for dynamics** — fit on cumulative counts (`total:` at 100-250 ms, device-stamped); custom ISR edge-timestamp component only if spike S1 shows the count path is insufficient; keep one stock `pulse_counter` as cross-check
2. **No device timestamps over the native API** — device owns transient timing (step/start/stop scripts with sequence numbers and device stamps); host drives steady sweeps; commands only via API actions
3. **Hardware safety without host** — `on_boot` script sets 100 % in RAM plus a client-disconnect watchdog; no prefs writes; preflight gates on plausible RPM on the chosen port and 0 elsewhere
4. **Nondeterministic noise** — stateless keyed hash of (seed, port, sim-time bucket), not a stateful stream, so golden tests survive tick-count changes; PCG only for offline fit bootstrap
5. **Overfit / false tolerance claims** — monotone low-parameter curves; tolerance `max(floor, k × repeatability)` frozen after a repeatability pass and before held-out data; done criterion checked on a **held-out** sequence
6. **Schema break** — `schema = 2` explicit; schema 1 loads unchanged; never auto-promote a measured file into the library
7. **Comparison domain mismatch** — measured and replayed series pass through one shared estimator, so quantisation is not counted as error

### Resolved conflicts between research files

| # | Topic | Decision |
|---|-------|----------|
| C1 | Tach quantisation location (C++ sensor vs Go accumulator) | Go `model.Tach` accumulator, no protocol change |
| C2 | Measurement instrument | Cumulative counts first; ISR timestamps gated by spike S1 |
| C3 | Sequencing owner | Device owns transients, host owns steady sweeps |
| C5 | Noise | Stateless keyed hash over sim-time buckets |
| C6/C7 | Fault lifecycle | Clear on sim restart and `sil down`, keep across firmware restart, clear on fan swap (with event); unplug is a fault, replug = clear — **maintainer to confirm** |
| C8 | Noctua A2 | Probably wrong (one review sample: stops ~14 %, restarts ~18 %); model supports both behaviours, measurement decides |
| C9/C10 | Tolerance / done | `max(floor, k × repeatability)`, held-out sequence |
| C11 | Report | Inline vendored uPlot |
| C13 | Boot safety | `on_boot` 100 % in RAM + disconnect watchdog |
| C14 | Locality | Narrow documented exception for the measuring firmware build |
| C16 | API | Noise only |
| C17/C18 | Schema | Per-parameter provenance with `unobservable`; drop `below_min` for `start_duty`/`stop_duty` + curve |

## Implications for Roadmap

Numbering continues from v0.1.0 (last phase 3.1).

### Phase 4: Hardware spikes and locked decisions
**Rationale:** de-risk the board side early; runs in parallel with Phase 5.
**Delivers:** S1 counter fidelity under Wi-Fi load, S2 sample stream rate, S4 cold-cache ESP32-S2 build, S6 boot/disconnect safety, S7 sim tach fidelity (does 30 rpm stepping survive the firmware's 1 s mean). Also ISR + PCNT on the same pin, API action with response.

### Phase 5: Fan model v2 and schema 2
**Delivers:** `model.Plant`, curves, start/stop, dynamics, tach accumulator, noise, fan laws, schema 2 loader/adapter, `sil fans` schema/measured fields.

### Phase 6: Fault injection
**Depends on:** 5. **Delivers:** fault kinds, Operations rows, CLI, status/events, web faults panel, lifecycle rules.

### Phase 7: fan-analyze offline core
**Depends on:** 5 (parallel to 6). **Delivers:** Recording format, replay, count-domain fit, compare with hold-out, report.html, synthetic recovery tests on FakeBoard.

### Phase 8: Measuring firmware and native API client
**Delivers:** `analyze/firmware/rev33-fan-analyze.yaml`, fail-safe, device scripts, Go native API client (contract-tested against the host firmware `sil up` runs), `fan-analyze run`. The `apiclient` work can start during 5.

### Phase 9: Measurement campaign and library migration
**Human checkpoints:** setup, instrument self-check (PWM-to-tach bridge), preflight, device-side run, report review. Arctic P12 and Noctua NF-A12x25 measured, repeatability pass, held-out verify, library updated, A1/A2 closed.

### Phase Ordering Rationale

Model and schema gate everything (the fitter's output is the loader's input). Faults sit on top of the new model, not inside `Step`. The offline analyze core is testable with no hardware, so hardware time is spent only once the pipeline is proven against the sim.

### Research Flags

Needs deeper phase research: Phase 4/8 (instrument design, PCNT limits, timestamped delivery), Phase 7 (identifiability of tau and start delay in the count domain, uncertainty method). Standard patterns: Phases 5 and 6.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Module proxy, pinned image source read |
| Features | MEDIUM | Intel spec and Arctic page read; Noctua and reviews from search summaries |
| Architecture | HIGH | Integration points read in code; tach accumulator MEDIUM |
| Pitfalls | MEDIUM-HIGH | Instrument/API facts verified in image; fan physics reasoned |

**Overall confidence:** MEDIUM

### Gaps to Address

- Arctic P12 SKU: repo cites ACFAN00120A, research fetched ACFAN00170A
- Arctic PST daisy-chain topology and tach semantics: measure a single fan, record it
- ESP32-S2 PCNT unit count and ISR latency unverified
- Rev 3.3 schematic (tach pull-up, supply) unchecked
- Maintainer decisions: fault lifecycle policy, locality exception, tolerance numbers

## Sources

### Primary (HIGH confidence)
- Pinned image `ghcr.io/esphome/esphome:2026.10.1`: `pulse_counter`, `pulse_meter`, `api.proto`, API keepalive, `aioesphomeapi`
- This repo: `internal/model`, `fanlib`, `engine`, `supervisor`, `control`, `apiclient`
- Go module proxy, gonum v0.17.0 source, Go randv2 blog

### Secondary (MEDIUM confidence)
- Intel 4-wire PWM fan specification (2 ppr, 25 kHz, start-up current ≤ 1 s)
- Arctic P12 PWM PST product page

### Tertiary (LOW confidence)
- Search summaries of Noctua NF-A12x25 spec/white paper, TechPowerUp and HWCooling reviews

---
*Research completed: 2026-11-10*
*Ready for roadmap: yes*

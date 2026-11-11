# Architecture Research: v0.2.0 "Real Fans" integration

**Domain:** Closed-loop SiL bench for ESPHome firmware; v0.2.0 adds a measured fan model, runtime fault injection and a real-board measurement pipeline (`sil fan-analyze`)
**Researched:** 2026-11-10
**Confidence:** HIGH for integration points in existing code (read in full: engine, model, fanlib, control, supervisor, events, sim_bus sensor, protocol); MEDIUM for the tach phase-accumulator and the measuring-firmware sampling design (needs a spike); MEDIUM for the hand-rolled native-API client (message set is small, exact IDs to be read from the pinned image)

Scope: only what the three new features need. The v0.1.0 architecture (`.planning/milestones/v0.1.0-research/ARCHITECTURE.md`) stands unchanged: single-writer engine actor, supervisor-owned broker and recorded inputs, `control.Operations` as API identity, injectable clock, NDJSON sim-bus over UDS.

## Verdict in one paragraph

Put every new *behaviour* in the pure `internal/model` package behind one `model.Plant` object that both the engine and a new `internal/replay` driver call, so the sim, the fitter and the "sim replay vs measurement" check share a single code path. Faults are model-level semantics (a `model.Faults` value on the plant, one `model.FaultKinds` table as their identity) whose *active set* is engine/supervisor state, recorded in the supervisor next to `fans` and applied through the same one-event `Change` channel that `portN.fan` already uses. The sim-bus does **not** change (protocol stays version 1, no firmware or C++ change): tach quirks and "unplugged" are all expressible as the RPM value the Go side puts on `portN.rpm`. `fan.toml` becomes `schema = 2` with tables; schema 1 files still load through an adapter and behave exactly as today. `sil fan-analyze` is a CLI-only verb group (not in `control.Operations`, no instance needed) built from five small packages under `internal/analyze/` plus a hand-extended `internal/apiclient`; its stages communicate through a versioned NDJSON `Recording` file so fit, replay and report are developed and tested offline, deterministically, before any hardware is touched.

## System Overview

```
 EXISTING (unchanged)                                   NEW / MODIFIED in v0.2.0
 ====================                                   ========================

 firmware (host:, sim_bus)  <== sim-bus v1, NDJSON ==>  engine ──► model.Plant (NEW, pure)
   SimSensor 1 s window mean   (no protocol change)       │          ├─ mechanical Step (curve up/down, start/stop)  [MOD fan.go]
   SimOutput duty                                         │          ├─ Faults (FaultKinds table)                    [NEW faults.go]
                                                          │          ├─ Tach (ppr, quantise, noise, dropout)         [NEW tach.go]
                                                          │          └─ Outputs (airflow, power, fan laws)           [NEW derive.go]
 supervisor (records fans, inputs) ── seeds ──► engine    │
   + records faults (NEW, same inMu path)                 ├─► snapshot → Status.Ports[]: rpm, tach_rpm, faults, airflow, power
 control.Operations (identity)                            └─► events: "fault" (NEW kind), sample gains "tach"
   + faults / fault.set / fault.clear (3 NEW rows)
 web page = client of the same rows (+ faults panel)

 sil fan-analyze (NEW, CLI-only, no instance)
   board (Rev 3.3, measuring firmware)                    internal/replay (NEW) ◄── fit + verify use the same Plant
     ⇅ native API (Noise)  internal/apiclient (MOD: transport split + messages + Conn)
   internal/analyze/measure  ──► Recording (NDJSON file) ──► fit ──► fanlib.Marshal ──► fans/<id>.toml (schema 2)
                                                         └─► replay.Run ──► compare.Verdict ──► report.html
```

## Component Responsibilities

| Component | Responsibility | New / Modified |
|-----------|----------------|----------------|
| `internal/model` | All fan physics and tach semantics, pure (no clock, no I/O, deterministic RNG state inside values) | Modified (`fan.go`) + New (`plant.go`, `curve.go`, `tach.go`, `faults.go`, `derive.go`, `rng.go`) |
| `internal/fanlib` | Load/validate `fan.toml` (schema 1 and 2), write schema 2 | Modified (`fanlib.go`) + New (`v1.go`, `marshal.go`) |
| `internal/engine` | Owns one `Plant` per port, feeds it ticks, sends frames, publishes snapshot and events | Modified (`engine.go`) |
| `internal/supervisor` | Records per-port fault set with fans/inputs, seeds every engine, implements new Control methods | Modified (`supervisor.go`) + New (`faults.go`) |
| `internal/control` | Fault types, 3 Operations rows, Client methods, Status fields | Modified (`control.go`, `operations.go`) |
| `internal/events` | `fault` kind, `tach` in Sample, `Params` on Event | Modified (`events.go`) |
| `internal/cli` | `sil fault ...`, `sil faults`, `sil fan-analyze ...` | Modified (`cli.go`) + New (`faults.go`, `analyze.go`) |
| `internal/replay` | Drive a Plant with a timed duty script on a fixed tick, return a trace | New |
| `internal/analyze/{recording,measure,fit,compare,report}` | Measurement file format, measurement runner + Board seam, fitter, tolerance verdict, HTML report | New |
| `internal/apiclient` | Native API transport (Noise + plaintext) split out of the probe, plus the ~12 messages fan-analyze needs | Modified (`probe.go` split) + New (`transport.go`, `messages.go`, `conn.go`) |
| `analyze/firmware/` | Measuring firmware YAML for the real Rev 3.3 board | New (outside `examples/`) |
| `ui/web` | Fault chips and controls per port, `fault` event in the store | Modified + New (`js/faults.js`) |
| `components/sim_bus`, `protocol/` | Nothing | **Unchanged** |

## Question-by-question decisions

### 1. Where does the fan model change live?

In `internal/model`, extended in a zero-value-compatible way, and wrapped by a new `model.Plant`.

- `model.Params` gains optional sub-structs: `Curve{Up, Down}` (duty/rpm point tables), `Start{Delay, Kick...}`, `Tach{PPR, Noise, Quantise}`, `Datasheet{AirflowM3h, PowerW, RefRPM, AirflowExp, PowerExp}`. **A zero value means today's behaviour**: no `Curve` means the existing linear `MinRPM..MaxRPM` target, `Start.Delay == 0` means no delay, `Tach` zero means an ideal (continuous, noiseless) tach. Consequence: the existing `internal/model/fan_test.go` (D-11..D-13 semantics, dt-composability test) passes unchanged and is the regression guard for "legacy behaviour is preserved".
- `model.State` gains `StartElapsed time.Duration` (start delay timer) and the tach phase accumulator; it stays a comparable value type with an RNG state `uint64` (own splitmix64, **not** `math/rand`, whose objects are pointers and whose global is auto-seeded). `Step` stays a pure function of `(Params, State, duty, dt)`; the "ten 100 ms steps equal one 1 s step" property keeps holding for the deterministic part because the start delay is a timer in `State` and noise is **not** drawn in `Step`.
- `model.Plant` (new) bundles `{Params, State, Tach, Faults}` and exposes `Step(duty, dt)`, `Frame(dt) float64` (the RPM value for one bus frame, see 4), `Reading()` (the last completed 1 s gate value, for status), `Outputs()` (airflow, power). The engine's `p.state = model.Step(p.cfg.Fan, p.state, p.duty, dt)` becomes `p.plant.Step(p.duty, dt)`; `sendRPM` sends `p.plant.Frame(...)` instead of `float32(p.state.RPM)`.
- Airflow/power from datasheet: `Outputs()` computes `Q = Q_ref * (rpm/RefRPM)^1` and `P = P_ref * (rpm/RefRPM)^3` (exponents overridable in `[datasheet]`; fan laws). They are read-only derived fields in the snapshot, never on the bus (Rev 3.3 has no sensing; PROJECT.md Out of Scope keeps measured airflow/power out).
- The model reads no clock: noise cadence is tied to `Frame`/gate calls, which the engine makes at its own constants (`tick` 10 ms, `rpmFrameStep` 50 ms), so a replay at the same constants draws the same random numbers.

### 2. How does `fan.toml` evolve? (versioned, backward compatible)

Explicit `schema = 2` key; absent key means schema 1. Both load; the loader always returns the same in-memory `model.Params`.

```toml
schema = 2
name   = "Arctic P12 PWM PST"
source = "https://arctic.de/..."            # datasheet provenance, kept

below_min = "stop"                          # "stop" | "hold" (unchanged meaning)
stop_duty = 0.05                            # v1 "min_duty"

[curve.up]                                  # duty rising: parallel arrays, strictly increasing duty
duty = [0.05, 0.10, 0.20, 0.40, 0.70, 1.00]
rpm  = [ 215,  330,  540,  980, 1450, 1820]
[curve.down]                                # optional; absent = same as up (no hysteresis loop)
duty = [...]
rpm  = [...]

[start]
duty    = 0.10                              # v1 "start_duty"
delay_s = 0.4                               # time from threshold crossing to first rotation
kick_duty = 0.30                            # optional: below this a stopped fan needs kick_ms of drive
kick_ms   = 500

[dynamics]
tau_up_s = 1.0                              # v1 "tau_up"
tau_down_s = 2.5                            # v1 "tau_down" (regulated down)
coast_tau_s = 6.0                           # optional: coast-down after stall/stop, defaults to tau_down_s

[tach]
ppr = 2                                     # pulses per revolution the fan emits
noise_rpm = 4.0                             # sigma of shaft speed ripple
quantise = true                             # count pulses per 1 s gate (30 rpm steps at ppr 2)

[datasheet]
airflow_m3h = 114.1
power_w     = 1.2
ref_rpm     = 1800

[measured]                                  # written by fan-analyze; absent = datasheet/assumed only
date = "2026-11-20"
board = "rev3.3"
esphome = "2026.10.1"
recording_sha256 = "..."
tolerance = "steady 3 %, tau 15 %, trace 5 % of max_rpm"
```

Rules that follow the existing loader's contract (D-14: strict, located errors, never default silently):

- Unknown keys still fail with `file:line:col`. A file with `schema = 3` fails with "needs a newer sil". Old sil reading a schema 2 file fails loudly at `schema` (unknown field), which is the desired failure mode.
- Every table is optional *as a table*; inside a present table every key is required (no silent defaults). Absent `[tach]` means ideal tach; absent `[start]` fails only for schema 2 (start duty is required there).
- `fanlib/v1.go` adapter: a schema 1 file is decoded with today's `rawFan`, validated with today's rules, and converted: `Curve.Up` is the linear two-point table `(min_duty, min_rpm)..(1, max_rpm)`, `Start.Duty = start_duty`, `Start.Delay = 0`, `Tach` zero (ideal). Zero-value compat in `model` makes the conversion trivially equivalent. v1 stays a supported input so users' `SIL_FAN_DIR` files do not break; `fan-analyze` and the shipped library write schema 2 only.
- `fanlib.Marshal(Params) []byte` (go-toml v2 `Marshal` plus a provenance comment header) is the only writer; `fan-analyze fit` uses it so a fitted file round-trips through `Load` (test: `Load(Marshal(p)) == p`).
- `control.FanInfo` gains `schema` and `measured` (bool, from `[measured]`) so `sil fans` shows assumed vs measured (this is how A1/A2 visibly close). Additive, no schema bump of Status.
- Document the schema in a new `docs/fan-toml.md`; the fan files' ASSUMED comments become `[measured]` provenance.

### 3. Where do faults live: engine state or model wrapper?

Both, with a strict split: **semantics in the model, active set in the engine/supervisor.**

- `model.Faults` is a plain value struct (zero = healthy): `Unplugged bool`, `Stuck bool`, `NoStart bool`, `StartDelayS float64`, `TachDead bool`, `TachPPR float64`, `TachDropout float64`, `Wear float64`. `Plant.Step` applies them in a fixed precedence: unplugged, then stuck, then start delay/no start, then wear (a pure copy of `Params` with scaled max_rpm, raised start duty, longer taus, higher noise), then the normal curve. Tach faults act only on the tach path (4). Because they are pure functions of `(Params, State, Faults)` they are unit-testable without an engine and are replayable (`replay` takes the same `Faults` timeline).
- `model.FaultKinds` is a table (`Name`, `Params []ParamSpec{Name, Default, Min, Max}`, `Apply func(*Faults, params)`), the fault analogue of `control.Operations`: validation, the `faults` catalog reply, CLI help and the page's controls all read it, so a new fault is one row. Initial kinds: `stuck`, `no_start`, `start_delay{seconds}`, `tach_dead`, `tach_ppr{ppr}`, `tach_intermittent{dropout,period_s}`, `unplugged`, `degraded{wear}`.
- Engine: `portRuntime.plant` holds the live `Faults`. `engine.Change` gains `Faults map[int]FaultOp` (port to set/clear list), applied in `handleApply` in the same single inbox event as temperature/humidity/fans, emitting one `fault` event per change (`ID` = kind, `V` = true/false, new `Params` field) before `publish()` and before the reply, preserving the "status read after Apply returned shows the change" guarantee (02-05).
- Supervisor: `engine.PortConfig` gains `Faults model.Faults` and `Seed uint64`; the supervisor's existing `fans []engine.PortConfig` record (single source of every engine's `Config.Ports`, guarded by `inMu`) therefore records faults for free. **Faults survive `sil sim restart` and firmware restart** (they are plant wiring, like fans; the fan's *state* still resets to rest). A `portN.fan` replug clears that port's faults (a new physical fan is healthy); this is a decision to confirm.
- Rejected alternative: faults as post-hoc output hacks in the engine (e.g. zeroing the frame value). Not replayable, not testable without sockets, and it cannot express degraded/late-start which change dynamics.

### 4. How do faults surface in Status / watch / Operations / web?

- `control.PortStatus` gains (additive, "extended, never renamed", `Schema` stays 1): `tach_rpm` (last completed gate reading, what the firmware's tach path reports), `faults` (`[{kind, params}]`, sorted), `airflow_m3h`, `power_w`. `rpm` keeps meaning shaft RPM; `state` stays `running|stopped` (a stuck or unplugged fan is `stopped`; the `faults` list is the truth, avoiding a new enum value for every consumer).
- `events.Kinds` gains `fault`; `events.Sample` gains `Tach *float64` (null while bus down, like Duty/RPM) so the plot can draw true vs reported RPM; `Event` gains `Params map[string]float64 omitempty`. `store.js` folds `fault` into `ports[i].faults` (the snapshot already carries the full list).
- `control.Operations` gets three rows, all `LAN: true` (they change only the simulated plant, same trust level as `set` and `press`):

| Name | Method Path | CLI |
|------|-------------|-----|
| `faults` | `GET /api/v1/faults` | `faults` (catalog from `FaultKinds` + active per port) |
| `fault.set` | `POST /api/v1/ports/{port}/faults/{kind}` body `{"params":{...}}` | `fault set 1 tach_dead`, `fault set 1 start_delay --param seconds=3` |
| `fault.clear` | `POST /api/v1/ports/{port}/faults/{kind}/clear` (`kind` may be `all`) | `fault clear 1 [KIND]` |

  `Control` interface and `Client` gain `Faults`, `SetFault`, `ClearFault`. The existing guards cover them with no new code: `TestOperationsCLI` (row has a working `sil` command), `TestCSRFCoversEveryMutatingRoute` (every non-GET row). The `control_test` fake and the `uitest` fake bench must learn the rows.
- Why dedicated rows and not `set portN.fault.X=...`: fault params and a catalog do not fit `key=value`, and agents need `sil faults` for discovery. **But** implement them through a shared supervisor helper `applyChange(ctx, change)` extracted from `Set` (record under `inMu`, `Apply`, rollback, gap/stopping handling). Phase 03.1 spent a whole review on that logic; duplicating it would duplicate its races.
- New `apperr` codes (registry test forces call sites): `unknown_fault` (1), `fault_param_invalid` (1; or reuse `invalid_argument`/`value_out_of_range`, prefer reuse), `port_unknown` only if not already covered by `invalid_argument`. Keep new codes minimal.
- Web: `ui/web/js/faults.js` (new) renders, per port panel, active-fault chips plus a menu built from `GET /api/v1/faults` (mirrors `initFanSelects`/`GET /api/v1/fans`); `index.html` port template gets a faults strip; `view.js` formats; `app.css` styles (CSP `style-src 'self'`, no inline style, fixed-width rule for header cells unaffected). The page derives nothing: a chip shows what `ports[i].faults` says.

### 5. Tach quirks given the bus carries an RPM value

The bus value `portN.rpm` is defined as "what the Rev 3.3 tach path would report": pulse_counter pulses per gate, times 60, times the product's `multiply: 0.5` (verified in `wifi-fancontroller/hardware-rev-3.1.yaml`: `pulse_counter`, `multiply: 0.5`, `update_interval: 1s`). So `BoardPPR = 2` and `Gate = 1 s` are constants of the Rev 3.3 board in `model`, and the fan's own `[tach] ppr` is what the fan emits.

Recommended representation (spike first, see Open Questions): a **pulse accumulator inside `model.Tach`**, not edges on the wire.

- `Tach` integrates `phase += rpm * fanPPR/60 * dt` every tick, with speed ripple (`noise_rpm`, AR(1), seeded) applied to the rpm before integration. Whole crossings of `phase` are integer pulses; the remainder carries over.
- Every 50 ms frame the engine asks `Frame()`, which drains the `n` pulses counted since the last frame and returns them in the board's reading units: `reading = n * 60 / (BoardPPR * frameDt)`. Wrong-ppr needs no special case: `reading = rpm * fanPPR / BoardPPR`, so the `tach_ppr` fault just overrides `fanPPR`.
- The firmware's existing `SimSensor` 1 s time-weighted window mean of those frame values equals pulses counted in its window divided by the window, so the *published* sensor shows exactly the real pulse_counter character: steps of `60/(BoardPPR*1 s)` = 30 rpm at ppr 2, ±1 count jitter, zero at low speed. No C++ change, the "RPM 1 s mean over the wire" decision is what makes this work. This is a counter, not a pulse simulation: no edges, no timing on the wire, drivers remain HIL's job (decision "tach is an RPM value" stands).
- `tach_dead`: pulses forced to 0 (reading 0, shaft keeps turning). `tach_intermittent`: seeded dropout windows drain 0 pulses. `unplugged`: shaft 0 and tach 0 (floating pin with pull-up reads no pulses). Status `tach_rpm` is the last completed gate reading (what the sensor would publish), `rpm` is shaft truth.
- Default is ideal tach (`Frame()` returns the continuous rpm) for schema 1 files and for any file without `[tach]`, so existing e2e/contract tests that compare RPM exactly (`e2e/model_test.go`, `io_test.go`, `uitest/parity_test.go`, `tests/test_contract.py`) keep passing; tests that need exact equality after the shipped fans become quantised should use a test fan via `SIL_FAN_DIR` or compare against `tach_rpm`.
- Fallback if the spike shows arrival jitter on the firmware side blurs the steps too much: analytic quantisation of the held 1 s gate value on the Go side (simpler, slightly wrong at gate boundaries).

### 6. Does "unplugged" need a bus or firmware change?

**No.** From the firmware's point of view an unplugged fan is indistinguishable from a dead tach: the Rev 3.3 has no presence detect, the PWM pin drives nothing observable, and an unconnected tach input with `INPUT_PULLUP` reports zero pulses, i.e. `portN.rpm = 0`. The `portN.duty` out channel keeps flowing unchanged. Sim-side `unplugged` differs only in what the simulator shows (airflow and power 0, shaft 0, no coast-down). `replug` is `fault clear` (plant restarts from rest at the current duty, same as today's fan replug), and swapping to a different library fan stays `set portN.fan=ID`. Bus protocol stays 1; `protocol/PROTOCOL.md`, vectors and `components/sim_bus` are untouched. State this explicitly in the roadmap so nobody opens a protocol change.

### 7. How is `fan-analyze` structured?

A verb group in the existing kong grammar (`internal/cli/analyze.go`, new `FanAnalyze FanAnalyzeCmd` field next to `Sim`/`Fw`), **not** rows of `control.Operations` (it talks to a board, not to a `sil` instance, and needs no `sil up`). Stages are separate subcommands that exchange files, because measurement is slow and hardware-bound while fit/replay/report are fast and deterministic:

| Verb | Does | Hardware |
|------|------|----------|
| `sil fan-analyze run --device HOST:PORT --key KEY --port N --plan default --out run.ndjson` | execute the measurement plan, write the Recording | yes |
| `sil fan-analyze fit run.ndjson --out fans/ID.toml [--datasheet fans/old.toml]` | fit curves/start/taus/tach, write schema 2 fan file | no |
| `sil fan-analyze verify run.ndjson fans/ID.toml` | replay + tolerance verdict as JSON; exit 1 `tolerance_exceeded` on failure | no |
| `sil fan-analyze report run.ndjson fans/ID.toml --out report.html` | self-contained HTML | no |
| `sil fan-analyze all ...` | run, fit, verify, report in one go | yes |

Output follows the CLI contract: result JSON on stdout, progress via slog on stderr, errors through `apperr` (new registered codes `board_unreachable` infra, `measurement_invalid` failed, `fit_failed` failed, `tolerance_exceeded` failed). The wall-clock lint holds: the runner takes a `clock.Clock` (`clock.NewReal()` in the CLI, `clock.Manual` in tests).

Package layout (dependency direction downward; `analyze/*` never imports `engine` or `supervisor`):

```
internal/
├── apiclient/
│   ├── probe.go          # MOD: Probe keeps its API, exchange() now uses transport.go
│   ├── transport.go      # NEW: Noise_NNpsk0 + plaintext framing, Dial(addr, key) -> *Conn (moved out of probe.go)
│   ├── messages.go       # NEW: hand-rolled protobuf for Hello, Ping, Disconnect, ListEntities{Sensor,TextSensor,Number}, SubscribeStates, *State, NumberCommand
│   └── conn.go           # NEW: Conn.Entities(), Subscribe(), SetNumber(); reads frames, answers pings
├── replay/replay.go      # NEW: Run(Params, Faults timeline, Script, Options) Trace  (model.Plant on engine tick constants)
└── analyze/
    ├── recording/        # NEW: NDJSON format: header, cmd, sample lines; rec_schema 1; sha256
    ├── measure/          # NEW: Plan (hold, sweep up/down, step, start-stop, kick), Runner, Board interface, NativeBoard (apiclient), FakeBoard (model.Plant on clock)
    ├── fit/              # NEW: curve from dwell medians, start/stall thresholds, tau_up/down + start delay by least squares through replay, tach noise
    ├── compare/          # NEW: Tolerance + Verdict (shared estimator applied to both measured and replayed series)
    └── report/           # NEW: html/template + server-side inline SVG, no JS, no CDN
```

Decisions inside:

- **Extend the hand-rolled `apiclient` instead of adding a protobuf dependency or a third-party library.** The Noise transport already exists and is proven (`flynn/noise`, real handshake against the firmware in `Probe`); the message set is about a dozen small messages, consistent with the project's hand-written codec ethos (C++ JSON parser). The only Go ESPHome client found (`mycontroller-org/esphome_api`) was last pushed September 2024 and its Noise support is unconfirmed. Message IDs and field numbers must be read from `api.proto` of the pinned image (ESPHome 2026.10.1), not from memory. Support plaintext as well as Noise so a lab board needs no key (the probe's `plaintext_api` rejection stays a probe-only rule).
- **`Board` is the seam**: `SetDuty(ctx, port, duty) (stampedAt, error)` and a `Samples() <-chan Sample` of firmware-stamped samples. `NativeBoard` implements it over `apiclient.Conn`; `FakeBoard` implements it by stepping a `model.Plant` on a `clock.Manual`. The runner is tested against the fake, so the whole measure to fit to verify chain runs in CI with no hardware, and a fit of a synthetic recording must recover the known parameters (round-trip test).
- **Fit through the model.** The steady curve comes from sweep dwell medians (up and down separately), thresholds from the start-stop test, and `tau_up`/`tau_down`/`delay` by least squares on the step segments with a small hand-written Nelder-Mead evaluating `replay.Run` (no gonum dependency). Because the fit objective is the sim itself, "fit then replay" matches by construction up to the model's expressiveness, and any shortfall is visible as residuals in the report.
- **Datasheet values are not measurable** on Rev 3.3 (no airflow, no current sensing): `fit` copies the `[datasheet]` table from the existing file (`--datasheet`) and says so in the report.
- Report is a single HTML file with inline SVG charts generated in Go (printable, opens anywhere, no vendored JS to maintain, no build step); reusing uPlot is possible later but not needed.

### 8. Where does the measuring firmware YAML live?

`analyze/firmware/rev33-fan-analyze.yaml` (+ `analyze/README.md`), deliberately **not** in `examples/` and not an input to `sil up`: it is a real ESP32-S2 (esp-idf) build, so `fwbuild`'s host lint and hermetic offline toolcache do not apply, and the ESP-IDF toolchain download needs network (documented, one-time, via the pinned `esphome/esphome:2026.10.1` image).

- **Own minimal pin table, not the product hardware package.** `hardware-rev-3.1.yaml` brings `fan: speed` entities with `RESTORE_DEFAULT_ON`, a boot script that turns all fans to 100 %, status LEDs and WiFi callbacks; those would fight the measurement. The YAML re-declares only: `ledc` outputs GPIO12 to 15 at 25 kHz, `pulse_counter` on GPIO16/17/18/21 with `INPUT_PULLUP`. A small test compares those numbers with `../wifi-fancontroller/hardware-rev-3.1.yaml` when that file is present (skip otherwise) to catch pin drift.
- **Dumb sampler, host sequences everything.** Per port: a `number` (duty 0 to 100 %, step 0.1) whose `set_action` calls `output.set_level` (bypasses the product's `speed_count: 100` quantisation); the product-equivalent `rpm` sensor (`pulse_counter`, `multiply: 0.5`, 1 s, same path the product firmware uses, for tach-quirk characterisation); and the raw `total` pulse counter. To avoid Wi-Fi jitter ruining transient timing, the firmware also publishes one `text_sensor` per port every 100 ms with a firmware-stamped line `t_us,total_pulses,applied_duty` (MEDIUM confidence: spike to confirm publish rate and that `total` keeps up on the S2). The host orders and interprets by firmware time, never by arrival time.
- Safety: on API disconnect the firmware returns all outputs to 100 % (fans must not be left stopped); `api` encryption key and Wi-Fi from `secrets.yaml`, never in the repo.
- Build and flash are manual steps wrapped by `make analyze-firmware` (docker, network allowed); not part of `make check`.

### 9. How does "sim replay vs measurement" run deterministically on the Manual clock?

Two layers, one code path (`model.Plant`):

1. **`internal/replay.Run`** is a plain loop: for each 10 ms tick (the engine's `tick`) apply the due duty command from the script, call `plant.Step`, and every 50 ms (`rpmFrameStep`) call `plant.Frame`; record shaft rpm, pulses and reading. No goroutines, no sockets, no clock, seeded RNG, so it is bit-reproducible and fast (milliseconds), which is what the fitter needs for thousands of evaluations and what `verify`/`report` use.
2. **Engine-level equivalence test** (`internal/engine`, using the existing harness: engine on `clock.Manual` plus the fake firmware socket peer): feed the same script as `out portN.duty` frames, advance the Manual clock in 10 ms steps, and wait on the `duty` event before each `Advance` (ordering between a socket frame and the next tick is otherwise racy; the existing tests use the same wait). Assert the engine's emitted samples equal `replay.Run`'s trace. This proves the sim as shipped, not only the pure model, reproduces the measurement, and guards against engine/replay drift. Seeds come from `PortConfig.Seed` (default: port number), so noise is identical run to run.
- **Comparing like with like.** `compare` applies one estimator (pulses in a sliding gate / gate) to both the measured pulse counts and the replayed pulse counts, so quantisation and the 1 s gate are not counted as error. The measured script is the firmware-stamped command timeline from the Recording; commands are placed on the 10 ms grid (at most one tick of skew, negligible against a 1 s tau).
- **Tolerance lives in code and is stated everywhere**: `compare.DefaultTolerance` (starting proposal: per-sweep-point steady rpm within max(3 % of reading, 1.5 quanta); fitted tau_up/tau_down within 15 % of the measured transient; whole-trace RMSE within 5 % of max_rpm; start/stall duty within one sweep step), printed in `report.html` and recorded in `[measured].tolerance` of the fan file. Acceptance for the milestone is `verify` exiting 0 for both Arctic P12 and Noctua NF-A12x25.

## Data Flow

```
control: sil fault set 1 tach_dead
  -> client.SetFault -> POST /api/v1/ports/1/faults/tach_dead   (api.sock or LAN)
  -> supervisor.SetFault: FaultKinds validate -> applyChange (inMu: record in s.fans[0].Faults; engine.Apply; rollback on failure)
  -> engine.handleApply (one inbox event): plant.Faults = ...; emit {k:fault, port:1, id:tach_dead, v:true}; publish(); reply
  -> next ticks: plant.Step / plant.Frame -> reading 0 on portN.rpm frames (sim-bus unchanged)
  -> firmware SimSensor 1 s mean -> 0 RPM published to HA; Status.ports[0] {rpm: <shaft>, tach_rpm: 0, faults:[tach_dead]}
  -> watch / ws stream carries the fault event; page chip appears

measurement:
  board <-native API-> apiclient.Conn <- NativeBoard <- measure.Runner(plan, clock) -> run.ndjson (firmware-stamped)
  run.ndjson -> fit (replay.Run inside the optimiser) -> fanlib.Marshal -> fans/ID.toml (schema 2)
  run.ndjson + fans/ID.toml -> replay.Run -> compare.Verdict -> report.html / exit code
```

## Patterns to Follow

### Pattern 1: Table is identity (extend to faults)
**What:** `model.FaultKinds` drives validation, catalog, CLI help and UI the way `control.Operations` does for the API.
**When:** every new fault, every new CLI-visible parameter.
**Example:**
```go
var FaultKinds = []FaultKind{
	{Name: "tach_ppr", Params: []ParamSpec{{Name: "ppr", Default: 4, Min: 1, Max: 16}},
		Apply: func(f *Faults, p map[string]float64) { f.TachPPR = p["ppr"] }},
}
```

### Pattern 2: Zero value is legacy
**What:** every new `Params`/`State`/`Faults` field's zero value reproduces v0.1.0 behaviour.
**When:** extending `model`. Gives free regression coverage from the existing model tests and keeps schema 1 conversion trivial.

### Pattern 3: One Change, one inbox event, publish before reply
**What:** all runtime plant mutations go through `engine.Change` and `handleApply`.
**When:** faults, replug, env. Do not add a second mutation path.

### Pattern 4: Seam interface with a model-backed fake
**What:** `measure.Board` with `NativeBoard` and `FakeBoard`; `supervisor.FanLibrary` fakes already exist in supervisor tests.
**When:** anything that needs hardware or a network peer.

## Anti-Patterns to Avoid

### Anti-Pattern 1: Faults as engine-side output hacks
**Why bad:** not replayable, not testable without sockets, cannot change dynamics (degraded, late start).
**Instead:** `model.Faults` applied inside `Plant.Step`/`Frame`.

### Anti-Pattern 2: Protocol or firmware change for tach/unplug
**Why bad:** breaks the shared golden vectors, forces a C++ and Go codec change for no observable gain.
**Instead:** shape the RPM value in Go; leave `bus.Proto` 1.

### Anti-Pattern 3: Randomness or time outside the model's explicit inputs
**Why bad:** global `math/rand`, per-tick draws or wall-clock reads break replay determinism and the wall-clock lint.
**Instead:** splitmix64 state in `State`, draws only at frame/gate cadence, `clock.Clock` everywhere else.

### Anti-Pattern 4: `fan-analyze` coupled to a running instance
**Why bad:** needs docker and firmware build for a pure curve fit; slow, flaky tests.
**Instead:** file-based stages (Recording in, fan.toml/report out); `analyze/*` imports only `model`, `fanlib`, `apiclient`, `clock`, `replay`.

### Anti-Pattern 5: Treating the ESP32 measuring firmware as a host/hermetic build
**Why bad:** `--network none` and the toolcache cover only `host:`; an esp-idf build downloads a toolchain.
**Instead:** separate documented non-hermetic make target; keep the YAML out of `examples/` and out of `sil up`.

## Existing tests and files that change as a side effect

- `internal/engine/engine_test.go`: `portRuntime.state` becomes a `Plant`; helper builders adapt. Behavioural assertions with schema 1 fans stay valid (ideal tach).
- `internal/control/control_test.go` and the supervisor fake `Control`/`FanLibrary`: new methods.
- `internal/cli/operations_test.go`: now iterates 16 rows; new rows need working CLI strings.
- `uitest/` fake bench: must serve the three new rows and the new status fields; add `uitest/faults_test.go`.
- `e2e/`: add `faults_test.go`; RPM-exactness tests keep using ideal-tach fans.
- `internal/apperr/codes.go`: registry grows; the AST scan fails `make check` on any code added without a call site.
- `.claude/CLAUDE.md`/`protocol/PROTOCOL.md`: only note "no protocol change"; add `docs/fan-toml.md`.

## Scalability Considerations

| Concern | Now (4 ports) | If ports grow (8) | Notes |
|---------|---------------|-------------------|-------|
| Bus frames with pulse-accumulator tach | ~20 frames/s/port, ~80/s total, ~4 KB/s | 160/s, still far under the 64 KiB hub TX cap | Frames change nearly every 50 ms; ideal-tach fans keep today's change-only rate |
| Snapshot size | per-port faults slice copied each `build()` | negligible | Keep `Faults` list small, sorted |
| History ring | 3000 samples x ports, `Tach` adds one float | linear | Page plots `tach` only on demand |
| Recording size | 10 samples/s x ~15 min x 1 port ~ 9k lines | per port file | NDJSON appended line by line so a Wi-Fi drop keeps partial data |
| Fit cost | thousands of `replay.Run` evaluations of ~10 min sim time | n/a | Pure loop, ms each; no engine, no sockets |

## Suggested Build Order

Dependencies: model v2 gates everything; faults and the offline analyze core are independent of each other once it exists; the hardware path needs the Recording/plan from the offline core; the measurement campaign needs all of it.

1. **Fan model v2 and `fan.toml` schema 2** (`model` Plant/Curve/Start/Tach/Outputs, `fanlib` schema 2 + v1 adapter + Marshal, engine wiring with ideal-tach default, Status `tach_rpm`/`airflow_m3h`/`power_w`, `docs/fan-toml.md`). Pure and offline, largest blast radius (engine, status, tests), so first. Includes the tach accumulator spike (1 s mean of frame rates reproduces 30 rpm steps).
2. **Fault injection** (depends on 1): `model.Faults` + `FaultKinds`, `engine.Change.Faults`, supervisor `applyChange` extraction and fault record, Operations rows + Client + `sil fault/faults`, events `fault`, store/page faults panel, e2e and uitest. Ends with faults visible in status, watch and the page.
3. **fan-analyze offline core** (depends on 1; parallel with 2): `recording`, `replay`, `fit`, `compare`, `report`, `fanlib.Marshal` usage, `FakeBoard` round-trip tests (fit recovers known params), engine-vs-replay equivalence test on `clock.Manual`. Proves the acceptance mechanism with zero hardware.
4. **fan-analyze hardware path** (depends on 3's Recording and plan; apiclient work can start earlier): `apiclient` transport split + messages + Conn with a fake native-API server for tests, `measure.NativeBoard` and Runner against real timing, `analyze/firmware/rev33-fan-analyze.yaml`, `sil fan-analyze run/all`, `make analyze-firmware`. Contains the two hardware spikes (S2 `total` pulse rate and firmware-stamped sample stream; first real flash).
5. **Measurement campaign and library update** (human checkpoint, depends on 1 to 4): measure Arctic P12 and Noctua NF-A12x25 on a Rev 3.3 board, fit, replace the ASSUMED values in `fans/*.toml` with schema 2 `[measured]` files, `verify` both within the stated tolerance, record in STATE.md that A1/A2 are closed. Budget an iteration loop back into the model if real fans show behaviour the v2 model cannot express (see below).

## Open Questions

- **Tach accumulator vs analytic quantisation** (MEDIUM): confirm by spike that firmware-side arrival jitter into `SimSensor`'s time-weighted mean keeps the 30 rpm steps recognisable. If not, use the analytic fallback.
- **Model expressiveness** (MEDIUM): real fans may show rpm-dependent tau, hysteresis loops beyond up/down tables, PWM-frequency effects or hunting at low duty. The curve tables are generic on purpose; if the campaign finds gaps, they cost a model iteration, not an architecture change.
- **Faults after replug** (decision to confirm): clear all of a port's faults on `portN.fan` replug (recommended) vs keep them.
- **Native API message IDs/fields** (MEDIUM): read from `api.proto` in the pinned image; also confirm no `ConnectRequest`/password step is needed in 2026.10.1 for an encrypted API without password.
- **Firmware-stamped sample stream** (MEDIUM): confirm 100 ms `text_sensor` publish rate is sustainable over the API on the S2 alongside `total` counting; fallback is host-stamped `total` at the cost of Wi-Fi jitter in transient timing.
- **Fault persistence across `sil sim restart`**: recommended yes (plant wiring); confirm with the maintainer alongside the open F7 design question.

## Sources

- Code read in this repository (HIGH): `internal/model/fan.go`, `internal/fanlib/fanlib.go`, `internal/engine/engine.go`, `internal/control/{control,operations}.go`, `internal/supervisor/supervisor.go`, `internal/events/events.go`, `internal/apiclient/probe.go`, `internal/clock/*`, `internal/lint/wallclock_test.go`, `internal/cli/cli.go`, `components/sim_bus/{sim_sensor.*,sensor.py}`, `protocol/PROTOCOL.md`, `fans/*.toml`, `examples/rev33.yaml`, `Makefile`, `.planning/{PROJECT,STATE}.md`
- `../wifi-fancontroller/hardware-rev-3.3.yaml` and `hardware-rev-3.1.yaml` (HIGH): ledc GPIO12 to 15 at 25 kHz, `pulse_counter` GPIO16/17/18/21, `multiply: 0.5`, `update_interval: 1s`, product boot script that sets fans to 100 %
- [ESPHome Pulse Counter docs](https://new.esphome.io/components/sensor/pulse_counter) (MEDIUM, via search summary): `total` sensor, `update_interval`, `internal_filter`; ESP32-S2 specifics not confirmed
- [mycontroller-org/esphome_api](https://awesome.ecosyste.ms/projects/github.com%2Fmycontroller-org%2Fesphome_api) (LOW-MEDIUM): only Go ESPHome client found, last pushed Sept 2024, Noise support unconfirmed
- [ESPHome native API encryption description](https://ubihome.github.io/esphome-native-api/native_api/encryption) (MEDIUM): Noise_NNpsk0_25519_ChaChaPoly_SHA256, matches the existing probe

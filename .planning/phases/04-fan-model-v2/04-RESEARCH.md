# Phase 4: Fan Model v2 - Research

**Researched:** 2026-11-10
**Domain:** Go plant model (curves, start/stop state machine, pulse-accumulator tach, seeded noise), strict TOML schema with provenance, additive status/event/UI plumbing
**Confidence:** HIGH for integration points and design (code read this session, key numerics executed); MEDIUM for how the firmware-visible tach looks on a real run (S7 is modelled here, not measured); MEDIUM for Noctua vendor numbers (spec page unreachable, datasheet PDF read)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Shipped fan library**
- **D-01:** Both shipped fans (`fans/arctic-p12-pwm-pst.toml`, `fans/noctua-nf-a12x25-pwm.toml`) are rewritten as **schema 2 in Phase 4**, so the default `sil up` shows v2 behaviour from now on. The exact v0.1.0 schema 1 files are kept byte for byte as test fixtures (testdata). The tests prove success criterion 5 ("every shipped schema 1 file behaves exactly as in v0.1.0") on those fixtures. — **Reversibility:** costly — the default fan's behaviour changes for every consumer, and the product repo's pytest scenarios run against the default fan.
- **D-02:** Existing tests that compare RPM exactly (`e2e/model_test.go`, `io_test.go`, `uitest/parity_test.go`, `tests/test_contract.py` and similar) **pin an ideal test fan via `SIL_FAN_DIR`**: a schema 1 fixture, or schema 2 with an ideal tach. Their assertions stay exact. New tests cover the shipped v2 fans. The contract test asserts the firmware-visible 30 rpm steps explicitly.
- **D-03:** Values with no source yet (down curve, start delay, coast tau, noise amplitude) get **plausible assumed values** marked `assumed`: a down curve slightly below the up curve, a start delay of about 0.4 s, and noise of a few rpm. The point is that the default sim visibly shows hysteresis, delay and noise. Measuring is deliberately not done here: the tooling arrives in Phases 6-7, and Phase 8 replaces the Arctic values with measured ones. The Noctua stays `assumed` until v0.3.
- **D-04:** The researcher **fetches the Arctic and Noctua vendor spec pages** for airflow (m³/h), power (W) and reference rpm, and records each URL. A value that a fetched page does not confirm is marked `assumed`, not `datasheet`. The same fetch settles the Arctic SKU (ACFAN00120A vs ACFAN00170A) that the file cites.

**fan.toml provenance**
- **D-05:** Provenance goes in a **parallel `[provenance]` table** keyed by dotted value paths (`"start.duty" = "assumed"`, `"curve.up" = "datasheet"`), and values stay plain numbers. The four provenance kinds are `assumed`, `datasheet`, `measured` and `unobservable`. A curve table counts as one value. — **Reversibility:** costly — Phase 6 `fit` writes this format, and Phase 8 commits a measured file in it.
- **D-06:** A provenance entry is **required for every value**. A missing entry, an entry for a value that does not exist, or an unknown kind is a located load error. Provenance has no silent default (consistent with 01-05 D-14).
- **D-07:** Sources live in a **`[sources]` table of named ids** (`arctic_page = "https://..."`). A provenance entry may reference one, e.g. `"datasheet:arctic_page"`. The loader rejects a reference to an undefined source id. Comments stay free for human notes. The v1 `source` string is not used in schema 2.
- **D-08:** By default, `sil fans` shows a **summary per fan**: id, name, schema and provenance counts (e.g. `measured 0 · datasheet 3 · assumed 9`). `--verbose` lists every value with its provenance and source. JSON output always carries the full per-value map. `control.FanInfo` changes are additive.

**Start / stop / coast semantics**
- **D-09:** Coast-down after a stop uses its own **`coast_tau_s`, required** in `[dynamics]`. Unpowered coast-down (inertia and friction only) is distinct from the regulated `tau_down_s`. There is no fallback to `tau_down_s`, so schema 2 has no silent default. The shipped files get an assumed value.
- **D-10:** "Never stops" is expressed as **`stop_duty = 0` plus a curve point at duty 0** carrying the floor rpm. There is no mode flag, and v1's `below_min` does not exist in schema 2. The v1 adapter maps `below_min = "hold"` onto this form so that v1 behaviour is identical.
- **D-11:** Once a fan drops below `stop_duty` it **counts as stopped, even while it is still coasting**. When duty rises above `start_duty` again, the start delay re-arms, and after the delay the fan re-accelerates from its current coast rpm. This continues v0.1.0's "hysteresis follows duty" rule.
- **D-12:** **No kick in v2.** Start duty plus start delay is enough. MODEL-06 and Phase 4 success criterion 2 are amended to drop "and kick" / "(with a kick if configured)". Kick is a deferred idea, to revisit if Phase 8 data shows a start boost the model cannot express. — **Reversibility:** reversible — adding an optional `[start]` sub-block later is additive.

**RPM vs tach readout**
- **D-13:** `sil status` and `sil watch` carry **both values**: `rpm` keeps meaning shaft truth (field unchanged), and the new `tach_rpm` is the last completed 1 s gate reading, i.e. what the firmware's tach path reports. Both changes are additive, and Status `schema` stays 1. Phase 5's dead-tach and wrong-ppr faults rely on seeing both.
- **D-14:** The port scope on the web page draws **two traces**: shaft RPM solid, and tach RPM as a stepped trace in the same port colour, dimmer (port colour per 03 D-03). The readout shows both numbers. History samples gain a tach value. Axes stay fixed from 0 to `max_rpm` (03 D-13). For a schema 2 fan, `max_rpm` is the curve maximum.
- **D-15:** The noise seed defaults to a **fixed constant mixed with the port number**, so every run is reproducible out of the box. `sil up --seed N` overrides it, and the seed is stored in run.json and shown in status. The seed survives sim restart and firmware restart within one instance, like the fan list does.
- **D-16:** Airflow and power for a fan with no datasheet values (such as a schema 1 test fan) are **JSON `null`**, and the page shows `—`. There is never a made-up zero (same rule as the stale values in 03 D-10). The fields are always present.

**Carried forward (already decided, not re-asked)**
- The tach is a Go `model.Tach` pulse accumulator. No sim-bus or firmware change, and the protocol stays 1. Spike S7 in this phase confirms that the 30 rpm steps survive the firmware's 1 s time-weighted mean, with analytic quantisation as the fallback.
- Noise is a stateless keyed hash of (seed, port, sim-time bucket), not a stateful stream, so traces do not depend on tick timing.
- Loading is strict: unknown keys fail with file:line:col, missing keys are named errors, and non-finite numbers are rejected (01-05).
- Airflow ∝ RPM and power ∝ RPM³ (fan laws) from the `[datasheet]` reference values, read-only in the snapshot and never on the bus.

### Claude's Discretion
- Curve interpolation (piecewise linear vs monotone cubic), as long as each direction stays monotone.
- Exact schema 2 key names and table layout beyond the decisions above (the research sketch in ARCHITECTURE.md §2 is the starting point, minus `kick_*` and `below_min`).
- Airflow and power exponents: fixed 1 and 3, or overridable in `[datasheet]`.
- Exact assumed numbers for the migrated files, within D-03.
- Whether `fanlib.Marshal` lands in this phase or in Phase 6 (it is needed by `fit`).

### Deferred Ideas (OUT OF SCOPE)
- **Start kick** (a fan-internal start boost at kick_duty for kick_ms): dropped from v2 (D-12). Revisit if the Phase 8 Arctic data shows a start transient the model cannot express, or for the Noctua in v0.3.
- **Catching a coasting rotor** (re-engaging without a start delay while RPM > 0): not modelled. D-11 re-arms the delay instead.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MODEL-05 | Separate measured up and down curves, monotone, interpolated | Piecewise-linear `Curve` with direction tracked in `State` (sticky sign of last duty change); loader enforces strictly increasing duty and non-decreasing rpm per curve (Architecture Patterns 1-2) |
| MODEL-06 | Start only above start duty after optional delay; stop only below separate stop duty (kick dropped, D-12) | State machine with `StartElapsed` timer, delay-expiry step splitting, `>=` start / `<` stop to stay bit-equal with v0.1.0 (Pattern 2) |
| MODEL-07 | Coast-down with own dynamics; "never stops" holds floor RPM | Required `coast_tau_s`; `stop_duty = 0` plus curve point at duty 0 (D-10); derived `BelowMin` keeps `FanInfo.below_min` meaningful (Pattern 2, Pitfall 4) |
| MODEL-08 | `schema = 2`, strict, v1 files unchanged | Schema probe then strict decode; table-aware line locator; v1 path untouched and frozen as differential reference (Pattern 3, Code Examples 4-5) |
| MODEL-09 | Pulse accumulator tach with the fan's ppr; 30 rpm steps; protocol stays 1 | `model.Tach` integrating exact rpm integral into pulses, drained into 50 ms bus frames; S7 plan with modelled result (Pattern 4, S7 section) |
| MODEL-10 | Deterministic seeded noise independent of tick timing | Keyed hash of (seed, port, 100 ms bucket), integrated per bucket on the exact-integral path; executed proof of tick invariance (Pattern 5, Code Example 2) |
| MODEL-11 | Airflow and power per port in status, watch, web page | `Plant.Outputs()` from datasheet table; null when absent (D-16); event and page-store plumbing gap identified (Architecture: Status/Events/UI) |
| MODEL-12 | Provenance per value; `sil fans` shows schema and provenance | `[provenance]` + `[sources]` grammar `kind[:source_id]`, value-path registry, `FanInfo` additive fields; CLI-contract conflict with D-08 flagged (Open Question 1) |
</phase_requirements>

## Summary

Phase 4 is a standard-patterns phase: no new Go module is needed (go-toml v2.4.3 already does everything, verified below), the engine seam is small (`model.Step` is called in one place and `state` is read in about eight), and the hard parts are numerical and test-hygiene problems, not library problems. The design that fits all locked decisions and keeps "schema 1 behaves exactly as v0.1.0" cheap is: keep the existing `model.Params` legacy fields and `Step` signature, add optional curve/start-delay/coast/tach/datasheet fields whose zero value is the v0.1.0 behaviour, synthesise the linear curve (and the D-10 "hold" form) inside the model when no curve is present, and prove equality with a differential test against a frozen copy of the v0.1.0 `Step`. A new `model.Plant` wraps state, tach and noise and is the only thing the engine calls.

Three findings change how the planner should write tasks. (1) **Tach fidelity (S7):** a modelled run (Python emulation of `sim_sensor.cpp`'s arithmetic fed with the proposed 50 ms accumulator frames) shows the firmware's 1 s mean is unbiased (|bias| < 0.5 rpm) and shows 1-2 quantisation levels at a steady non-integer pulse rate, but it is **not exactly on the 30 rpm grid at every window phase**: the firmware window is not aligned to the 50 ms bins, so the edge bins are weighted fractionally and readings can land on half steps (15 rpm). Peak-to-peak spread was 30-45 rpm with up to 5 ms arrival jitter and 57-79 rpm at 10-20 ms. The analytic 1 s-gate fallback has the same phase blur plus roughly one extra second of lag, so it is not a real improvement; the contract test and S7 must assert a statistical step signature, not exact multiples of 30. (2) **Page plumbing gap:** the page store folds only `duty/rpm/state/fan` events into the status mirror and derives nothing, so `tach_rpm`, `airflow_m3h` and `power_w` need event kinds (or fields) of their own, plus rows in the browser parity test. (3) **Test pinning is larger than D-02 lists:** the engine tests, supervisor watch test, `uitest` fake bench, `e2e/fans_test.go`, `e2e/web_test.go`, `e2e/model_test.go::TestFanTomlUnknownKey` (it edits a `max_rpm` line that schema 2 does not have) and both Python contract tests load shipped fans by id and assert v0.1.0 numbers; one shared `testdata/fans-v0.1.0/` directory used through `SIL_FAN_DIR` covers all of them, but `tests/silbench.py` currently *drops* `SIL_FAN_DIR`.

**Primary recommendation:** Build `model.Plant` (curve + start/stop state machine + exact-integral tach + keyed-hash noise) as pure, dt-composable Go behind the existing `Params` zero-value contract; load schema 2 through a schema probe plus strict decode with table-aware error locations; wire engine, status, events and page additively; pin every exact-RPM test to a frozen v0.1.0 fixture directory; run S7 as a hermetic Go emulation test first and a live host-firmware check second, with pass criteria stated as spread/bias/levels, not grid equality.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Duty→RPM curves, start/stop, coast | Simulator model (`internal/model`) | — | Pure physics, no clock or I/O; shared later with `internal/replay` (Phase 6) |
| Pulse accumulator tach, noise, gate reading | Simulator model (`internal/model`) | Engine (cadence) | Model owns the arithmetic; engine owns the 10 ms tick and 50 ms frame cadence it calls it on |
| Tach value on the wire | Engine → sim-bus frame (`port<N>.rpm`, protocol 1) | Firmware `sim_sensor` (1 s mean, unchanged C++) | The firmware window is the "pulse counter gate"; nothing changes on the C++ side |
| `fan.toml` parsing, schema probe, provenance validation | `internal/fanlib` | — | Single strict loader; also the future writer (Phase 6) |
| Airflow/power, tach reading in snapshot | Engine snapshot (`PortState`) | Supervisor (`Status` mapping) | Read-only derived fields, never on the bus |
| `sil fans` schema and provenance | Supervisor `Fans` / `control.FanInfo` | CLI `FansCmd` | Library of the instance's own supervisor (`SIL_FAN_DIR` of that instance) |
| Noise seed | `sil up` flag → `spec.json` → supervisor → engine `Config` | Status (`sim`) | Survives sim and firmware restart like `fans` does |
| Readouts, tach trace | Browser page (`ui/web`) | — | Pure client of status/events/history; derives nothing |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib (`math`, `math/bits`, `time`, `sort`) | 1.27.3 | Curves, exact-integral integration, splitmix64 keyed hash | Project constraint: stdlib first; `math/rand` global is banned for determinism |
| `github.com/pelletier/go-toml/v2` | v2.4.3 (already in `go.mod`) | Schema 1 and 2 decoding, strict mode | Already the loader; nested-table strictness and positions verified below |

[VERIFIED: /home/claude/esphome-sil/go.mod:9] `github.com/pelletier/go-toml/v2 v2.4.3`. Go toolchain [VERIFIED: `go version` → `go version go1.27.3 linux/amd64`].

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/pelletier/go-toml/v2/unstable` | v2.4.3 (same module) | Parser with node ranges | Only if the table-aware line scanner proves too fragile; `go doc` shows `Parser`, `Reset`, `NextExpression`, `Range` [VERIFIED: `go doc .../unstable Parser`] |
| uPlot 1.6.32 (vendored) | 1.6.32 | Third (stepped) series for the tach trace | Already vendored; `uPlot.paths.stepped({ align: 1 })` already used for the duty trace [VERIFIED: ui/web/js/plot.js series 2] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Piecewise-linear curve | Monotone cubic (PCHIP) | Smoother, but not bit-equal to the v0.1.0 linear target, harder to inspect, and Phase 6's fitter writes point tables; linear is exactly monotone by construction. Prescribe linear. |
| Fixed fan-law exponents 1 and 3 | Overridable in `[datasheet]` | Extra keys need provenance and tests, Pitfall 31 says power-law underestimates at low speed anyway. Prescribe fixed. |
| Go gate-held "analytic" tach | 50 ms accumulator frames | Gate-held doubles the reading lag (Go gate plus firmware window) and has the same phase blur; keep accumulator (see S7). |
| Stateful PRNG | Keyed hash | Stream is consumed per call and breaks tick-independence (PITFALLS 23); locked out by CONTEXT. |

**Installation:** none. No new package is installed in this phase.

**Version verification:** go-toml version read from `go.mod`; behaviour exercised in a scratch module against the module cache (see Code Examples 4). No registry lookup is needed because no dependency is added.

## Package Legitimacy Audit

No external packages are added or upgraded in this phase (Go stdlib plus the already-pinned go-toml v2.4.3, and the already-vendored uPlot 1.6.32). Audit not applicable.

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
 fan.toml (schema 1 or 2)                      sil up --seed N
   │  fanlib.Load: probe `schema` → v1 path | v2 path (strict, located,        │
   │  provenance + sources validated) → model.Params{legacy fields + Curve,    ▼
   │  StartDelay, CoastTau, Tach, Datasheet, Schema, Provenance}          spec.json{seed}
   ▼                                                                            │
 supervisor.fans []PortConfig ── seeds ──► engine.New(Config{Seed}) ◄───────────┘
                                              │ per port: model.NewPlant(params, port, seed)
   firmware ──out port<N>.duty──► engine ─────┤
                                              ▼  every 10 ms tick (dt may be larger when late)
                                 plant.Step(duty, dt)
                                   ├─ direction (sticky sign of last duty change) → up|down curve
                                   ├─ run/stop decision (start ≥, stop <, delay timer, step split at expiry)
                                   ├─ target = curve(duty) or 0; tau = up | down | coast
                                   ├─ shaft rpm  (smooth, noise-free = status.rpm)
                                   └─ Tach: exact ∫rpm dt (+ keyed-hash ripple per 100 ms bucket)
                                        → pulses (phase accumulator, remainder carried)
                                        → 1 s gate in sim time → tach_rpm (null until first gate)
                              every 50 ms: plant.Frame() → value = n·60/(2·frameDt)  (ideal tach: shaft rpm)
                                              │ change-only `in port<N>.rpm` frame (protocol 1, unchanged)
                                              ▼
                       firmware sim_sensor: time-weighted mean over its own 1 s window → HA / aioesphomeapi
 snapshot/Status.ports[i]: rpm, tach_rpm|null, airflow_m3h|null, power_w|null  (additive, schema stays 1)
 events: rpm (unchanged) + new tach / load kinds; sample gains tach  ──► store.js / plot.js (second stepped series)
```

### Recommended Project Structure
```
internal/model/
├── fan.go          # MOD: Params gains optional fields; Step becomes curve-driven; legacy Params still work
├── fan_test.go     # UNCHANGED (regression guard)
├── fan_v010_test.go# NEW: frozen copy of the v0.1.0 Step used as differential reference
├── curve.go        # NEW: Point, Curve, At(duty) clamp, synthesised legacy curve
├── plant.go        # NEW: Plant (Step, Frame, Current, Shaft, Reading, Outputs, Running)
├── tach.go         # NEW: pulse accumulator, 1 s gate, board constants
├── noise.go        # NEW: splitmix64 keyed hash, z(seed, port, bucket)
└── derive.go       # NEW: airflow ∝ rpm, power ∝ rpm³
internal/fanlib/
├── fanlib.go       # MOD: schema probe, dispatch; Error/keyLine kept
├── v1.go           # NEW (moved): rawFan decode and validate, untouched semantics
├── v2.go           # NEW: raw schema 2 structs, validate, provenance, value-path registry
├── provenance.go   # NEW: kind[:source] grammar, counts
└── marshal.go      # OPTIONAL here, needed by Phase 6 `fit` (Claude's discretion)
fans/*.toml         # MOD: schema 2 files; testdata/fans-v0.1.0/*.toml byte-for-byte copies (new, repo root)
```

### Pattern 1: Zero value is legacy (extend `Params`, do not replace it)
**What:** `Params` keeps its seven legacy fields (read this session, [VERIFIED: internal/model/fan.go:10-19]):
```go
type Params struct {
	Name, Source string
	MinDuty      float64
	StartDuty    float64
	BelowMin     string // "stop" or "hold"
	MinRPM       float64
	MaxRPM       float64
	TauUp        float64
	TauDown      float64
}
```
Add optional fields (all zero = v0.1.0): `Curve{Up, Down []Point}`, `StartDelay float64` (s), `CoastTau float64` (0 means TauDown), `Tach TachParams{PPR int, NoiseRPM float64}`, `Datasheet *Datasheet{AirflowM3h, PowerW, RefRPM}`, `Schema int`, and metadata `Provenance []ProvEntry` + `Sources map[string]string` (metadata on `Params` keeps the supervisor's `FanLibrary` interface and its test fakes unchanged). For a schema 2 file the loader also fills the legacy fields: `MinDuty = stop.duty`, `StartDuty = start.duty`, `MaxRPM = max over both curves`, `MinRPM = lowest curve rpm`, `TauUp/TauDown`, and **`BelowMin = "hold"` iff `stop.duty == 0` and the first up-curve point is at duty 0 with rpm > 0** (D-10), else `"stop"`. That keeps `FanInfo.min_rpm/max_rpm/below_min`, the page's fixed axis and `Initial()` working with no change.
**When to use:** every model extension. The existing `fan_test.go` (dt-composability, hysteresis, hold) passes unchanged because it builds legacy-only `Params`.
**Why not convert v1 into curve-only form in the loader:** a v1 `min_duty = 0, below_min = "stop"` file is a fan that never stops once running but starts from rest; D-10's form (`stop_duty = 0` + point at 0) would turn it into a "never stops" fan that `Initial()` starts running. Keeping `BelowMin` as the explicit derived marker avoids that corner. D-10's mapping is then realised by the model-side normalisation (`Params.curveUp()` synthesises `(0,min_rpm),(min_duty,min_rpm),(1,max_rpm)` for hold and `(min_duty,min_rpm),(1,max_rpm)` for stop). The planner should confirm this reading of D-10 (Assumption A3).

### Pattern 2: Step v2 semantics (state machine and numerics)
`State` gains `StartElapsed time.Duration`, `LastDuty float64`, `Dir int8` (+1 up, -1 down). `Step(p, s, duty, dt)` stays a pure function; `dt <= 0` returns `s`; NaN duty → 0, clamp to [0,1] (as today).
1. **Direction:** if `duty > s.LastDuty` → up, `duty < s.LastDuty` → down, else keep `Dir`; initial `Dir = up`, `LastDuty = 0`. Curve = `Down` when `Dir == down` and a down curve exists, else `Up`. (Sticky direction; a sweep up uses up, the reverse sweep uses down, a steady duty stays on whichever curve it arrived by.) [ASSUMED design, A1]
2. **Run decision** (keep v0.1.0 comparison operators): stopped and `duty >= StartDuty` → accumulate `StartElapsed += dt`; when it reaches `StartDelay` the fan is running, and the step is **split** at the expiry instant (the part before is "stopped, coasting", the part after spins up). `StartDelay == 0` means running in this same step, exactly as v0.1.0. Stopped and `duty < StartDuty` → `StartElapsed = 0`. Running and `duty < MinDuty` (the stop duty) → stopped, timer cleared. Between stop and start the previous state is kept (hysteresis follows duty, D-11).
3. **Target:** running → `curve.At(duty)` (clamped to the end points, never extrapolated); not running → 0. Tau: target above rpm → `TauUp`; target below rpm → `TauDown` if running, `CoastTau` (or `TauDown` when `CoastTau == 0`, the legacy zero value) if stopped. Snap to 0 when `target == 0 && rpm < 0.5`. Update with the unchanged exact exponential `rpm += (target - rpm) * (1 - exp(-dt/tau))`.
4. **Interpolation must use the v0.1.0 operation order** `y0 + (x-x0)/(x1-x0)*(y1-y0)` so legacy parameters produce bit-identical targets: `MinRPM + (duty-MinDuty)/(1-MinDuty)*(MaxRPM-MinRPM)` is exactly this with `x1 = 1`; the hold plateau gives `min + k*0 == min`.
**Proof obligation:** a differential test that replays randomised duty scripts and `dt` mixes through a frozen copy of the v0.1.0 `Step` (kept in `_test.go`) and the new `Step` on legacy-only and on adapted `Params`, asserting `==` on `RPM` and `Running`. This, plus the unchanged `fan_test.go`, is the evidence for success criterion 5.

### Pattern 3: Schema probe, then strict decode, with table-aware locations
Decode the file first into `struct{ Schema *int `toml:"schema"` }` **without** `DisallowUnknownFields` (verified: tolerates every other key; a syntactically broken file still errors, which maps to the existing located `decodeError`). Absent → v1 path (today's `rawFan` untouched; add `Schema *int` to it so an explicit `schema = 1` is accepted). `2` → v2 path with `DisallowUnknownFields`. Any other value → located error ("needs a newer sil" for greater than 2). Verified behaviours (scratch module, go-toml v2.4.3): unknown keys in nested tables are reported with line:col via `StrictMissingError` (`unknown start.delay at 4:1`, `unknown curve.up.rmp at 4:1`), wrong types give a `DecodeError` with position, duplicate keys are `DecodeError`, TOML integers decode into `[]float64`, and `nan`/`inf` parse successfully so the loader **must** reject non-finite values itself.
The existing `keyLine` finds the first line starting `key =` anywhere, which is wrong for schema 2 where `duty` appears under `[start]`, `[stop]`, `[curve.up]` and `[curve.down]`. Replace it for v2 with a small scanner that tracks the current `[table]` header and matches `table.key` (and quoted dotted keys inside `[provenance]`). Semantic errors then point at the real line.

### Pattern 4: Tach accumulator (exact integral, 50 ms drain, 1 s gate)
Constants of the Rev 3.3 board: `BoardPPR = 2`, gate `1 s` (`pulse_counter multiply: 0.5`, `update_interval: 1s`; [CITED: .planning/research/ARCHITECTURE.md §5, PITFALLS.md table "Production tach"]). The fan's `[tach] ppr` is what it emits; wrong-ppr behaviour (Phase 5) is just a different `ppr`.
- Each `Step` adds the **exact integral** of rpm over the step to a phase: for constant target, `∫rpm dt = target·dt + (rpm0 − target)·tau·(1 − e^(−dt/tau))` (rpm·s). Revolutions = that / 60; pulses = revolutions · `ppr`; whole pulses are counted and the fractional remainder carries over.
- `Frame()` (called by the engine every ≥ 50 ms) drains the pulses counted since the last frame and returns `n · 60 / (BoardPPR · frameDt)` where `frameDt` is the sim time actually elapsed (so a late tick stays correct). At ppr 2 a pulse in a 50 ms frame is 600 rpm; the firmware's 1 s mean of 20 such frames is 30 rpm per pulse counted. Ideal tach (no `[tach]` table): `Frame()` returns the shaft rpm unchanged, so the v0.1.0 change-only frame rate and exact values stay.
- A separate sim-time 1 s gate (count of pulses in the last completed gate · 30) gives `Reading()` = `tach_rpm`; it is `null` until the first gate completes and again after a sim restart; for an ideal-tach fan it is `null` (nothing is quantised; recommendation, A5).
- `Current()` returns the last frame value without draining, for the hello `in` snapshot (`inSnapshot` currently sends `float32(p.state.RPM)`), so a firmware reconnect does not steal pulses.
**Executed proof:** with a constant target, total phase after 20 s was identical to 9 digits for tick patterns `[10 ms]`, `[7 ms]`, `[10, 30, 3 ms]` and `[1 s]` (scratch Go program, bucket-split exact integral + keyed-hash ripple). This is what makes "two runs with the same seed and duty sequence give identical traces even when tick timing differs" true for constant-duty segments; a duty change still lands on the tick that observes it (as in v0.1.0), so tests must hold the duty script on a common time grid.

### Pattern 5: Noise as a keyed hash on the tach path
`z(seed, port, bucket)` = splitmix64 finaliser over `seed ^ rot(port·K1) ^ bucket·K2`, four draws summed and scaled (Irwin-Hall, mean ≈ 0, variance ≈ 1: executed, mean 0.0016, variance 0.9989 over 200 000 buckets). Bucket width 100 ms (discretion). The ripple (`noise_rpm · z`) is added to the speed **only inside the tach integration**, and the integration loop is split at bucket edges so it is independent of `dt`. `rpm`/`status.rpm`, `airflow`, `power` and the running/stopped decision stay noise-free shaft truth. Reasons: D-13 defines `rpm` as shaft truth; Phase 5 faults and Phase 6 fitting want a clean truth; PITFALLS 18 shows noise added to a 10 ms shaft value is averaged away by the 1 s window anyway, so noise belongs where counts are made. Consequence to tell the user: with `noise_rpm` of "a few rpm" the noise is visible only as occasional ±1-pulse flips; to make it plainly visible choose around 1 % of rpm (Assumption A4).

### Pattern 6: Status, events, control: all additive
- `control.PortStatus` gains `tach_rpm *float64`, `airflow_m3h *float64`, `power_w *float64` (always present, `null` when unknown, D-16), JSON tags exactly so. `SimStatus` gains `seed` (D-15). Status `SchemaVersion` stays 1 [VERIFIED: internal/control/control.go:33 `const SchemaVersion = 1`].
- `control.FanInfo` today is [VERIFIED: internal/control/control.go:229-235]:
```go
type FanInfo struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	MinRPM   float64 `json:"min_rpm"`
	MaxRPM   float64 `json:"max_rpm"`
	BelowMin string  `json:"below_min"`
}
```
  Add `schema int`, `provenance` (counts per kind, `null` for schema 1: v1 files record none) and `values` (full per-value map with kind and source, `null` for schema 1). Build it in **one** place (e.g. `control.NewFanInfo(id, model.Params)`): `supervisor.Fans` and the `uitest` fake bench both construct `FanInfo` from `Params` today ([VERIFIED: internal/supervisor/supervisor.go:1269 and uitest/bench_test.go:396, both `control.FanInfo{ID: id, Name: p.Name, MinRPM: p.MinRPM, MaxRPM: p.MaxRPM, BelowMin: p.BelowMin}`]) and would otherwise drift.
- `events.Sample` is [VERIFIED: internal/events/events.go:37-41] `Port int`, `Duty *float64`, `RPM *float64`; add `Tach *float64 json:"tach"` (null while the bus is down, like Duty/RPM). `events.Kinds` is [VERIFIED: internal/events/events.go:24] `var Kinds = []string{"duty", "rpm", "state", "fan", "button", "env", "leds", "firmware", "sim"}`; the page store applies only `duty/rpm/state/fan` to `ports[i]` and derives nothing ([VERIFIED: ui/web/js/store.js:37-40 `case "duty": case "rpm": case "state": case "fan": {`]), so the snapshot's new fields would go stale. Add two kinds: `tach` (`V` = reading, emitted when the 1 s gate value changes) and `load` (`V` = `{airflow_m3h, power_w}` with explicit nulls, emitted with the 1 s rpm sample on change), fold them in `store.js`, and reset them to null on a `fan` event (a swap can remove the datasheet). `app.js` `KINDS` string and `uitest/parity_test.go::sessionSteps` need rows for both.

### Anti-Patterns to Avoid
- **Rewriting `Step` without a frozen reference:** "exactly as v0.1.0" then rests on eyeballing; always keep the differential test.
- **Noise drawn per tick or from a stateful stream:** breaks tick independence and floods the bus (PITFALLS 23).
- **Noise on the shaft value the state machine reads:** it would make start/stop flicker and break dt-composability.
- **`keyLine` for schema 2:** wrong line for repeated key names (see Pattern 3).
- **Making provenance a silent default:** any value without an entry is a load error (D-06).
- **Adding `go-toml` `Marshal` for the shipped files:** it drops comments; the shipped files are hand-written (a `text/template` writer is Phase 6).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| TOML parsing, nested strictness, error positions | A custom parser | go-toml v2 `Decoder.DisallowUnknownFields` + `StrictMissingError.Position()` | Verified to locate nested unknown keys at line:col |
| Random numbers | `math/rand` or a stream | splitmix64 finaliser over a key (stdlib `math/bits`) | Stateless, tick-independent, no global state; ~10 lines |
| Integer pulse counting with remainder | Float rounding per frame | Phase accumulator carrying the fractional remainder | Unbiased counts (|bias| < 0.5 rpm in the S7 model) |
| Error codes | Ad-hoc strings | Existing `apperr.Registry` (`fan_toml_invalid`, `invalid_argument`) | AST-checked in `make check`; schema 2 errors need no new code |
| Duplicated FanInfo construction | Two copies | One `NewFanInfo` | Supervisor and uitest bench already diverge risk |

**Key insight:** the work is making the *existing* seams carry more data without changing their old answers; every new behaviour hangs off a zero value that reproduces v0.1.0.

## Runtime State Inventory

Not a rename/refactor/migration phase. The only "migration" is rewriting the two shipped fan files and keeping v0.1.0 copies as fixtures. Nothing is stored outside git: fan files are embedded (`fans/embed.go` `//go:embed *.toml`), run state is recreated on every `sil up` (`ResetRunDir`), and the seed lives in the per-run `spec.json`. None — verified by reading `fans/embed.go`, `internal/instance/instance.go` (`Spec`, `ResetRunDir`).

## Common Pitfalls

### Pitfall 1: Expecting the firmware reading to sit exactly on 30 rpm multiples
**What goes wrong:** a contract/S7 assertion such as "every reading is a multiple of 30" fails on some runs.
**Why it happens:** `SimSensor::update()` publishes the time-weighted mean of whatever frames arrived in its own window ([VERIFIED: components/sim_bus/sim_sensor.cpp:20-43] `this->area_ += static_cast<double>(this->last_) * static_cast<uint32_t>(now - this->last_t_);` and `const float mean = window > 0 ? static_cast<float>(this->area_ / window) : this->last_;`). That window starts at firmware setup, not on the Go 50 ms grid, so edge bins are weighted by their overlap; at some phases readings fall on half steps.
**How to avoid:** assert spread, bias and level count (S7 criteria below); an on-grid fraction is informational only.
**Warning signs:** a test that passes on one machine/run and fails on the next with a constant offset (the phase is fixed per run).

### Pitfall 2: Smaller frame bins make it worse
**What goes wrong:** sending a frame every tick (10 ms) to "improve resolution".
**Why it happens:** modelled on-grid fraction at 10 ms bins was 0.10-0.12 versus 0.42-0.67 at 50 ms; each 1-pulse bin carries 3000 rpm and a partially weighted edge bin moves the mean by up to half a step with high probability. Bias also grows (about 1 rpm versus about 0.2).
**How to avoid:** keep `rpmFrameStep = 50 ms` ([VERIFIED: internal/engine/engine.go:31] `rpmFrameStep = 50 * time.Millisecond`).

### Pitfall 3: Arrival jitter blurs the steps
**What goes wrong:** spread grows beyond two steps on a loaded machine. Modelled steady spread (1000 rpm, 60 s, window phase swept): jitter up to 0.5 ms → 30 rpm; 2 ms → median 34/max 36; 5 ms → 40/45; 10 ms → 48/57; 20 ms → 67/79. Two frames processed in one firmware loop pass (same `millis()`) lose the first frame's area entirely, which is the mechanism.
**How to avoid:** keep frame spacing regular (the engine already sends at `simNow ≥ nextRPM`); S7b measures the real jitter; thresholds below.

### Pitfall 4: v1 corner `min_duty = 0, below_min = "stop"` mapped to "never stops"
**What goes wrong:** the adapter turns a fan that must start from rest into one that starts running.
**How to avoid:** `BelowMin` stays the explicit derived marker (Pattern 1); add this corner to the differential test's random parameters (`min_duty ∈ {0, small}`, both `below_min` values, `start_duty == min_duty`).

### Pitfall 5: Existing tests silently change meaning when the shipped files become schema 2
**What goes wrong:** `e2e/model_test.go::TestFanTomlUnknownKey` reads `fans/arctic-p12-pwm-pst.toml` and looks for a line starting `max_rpm` ([VERIFIED: e2e/model_test.go:169,176] `if strings.HasPrefix(l, "max_rpm") {` ... `t.Fatal("fans/arctic-p12-pwm-pst.toml has no max_rpm line")`); the schema 2 file has no such line. `e2e/fans_test.go` asserts `{fansArctic, "Arctic P12 PWM PST", "stop", 200, 1800}` and Noctua `"hold", 450, 2000` for `sil fans`, Noctua holding "about 450" and "above 1900"; `e2e/web_test.go` reads the Arctic `max_rpm` for the axis; `uitest/plot_test.go` asserts the RPM scale max `1800`; engine tests load `fanlib.DefaultFan` and expect reference rpm from `model.Step`; both Python tests expect 789.5 rpm within 2 %.
**How to avoid:** one repo-root `testdata/fans-v0.1.0/` (byte-for-byte v0.1.0 `arctic-p12-pwm-pst.toml` and `noctua-nf-a12x25-pwm.toml`, same ids) used through `SIL_FAN_DIR` by every exact-RPM test. Because `SIL_FAN_DIR` replaces the whole library, same ids keep `sil set portN.fan=noctua-…` working. For schema 2 unknown-key coverage use a v2 typo such as `noise_rpm` → `noise_rmp` on the shipped file. [VERIFIED: tests/silbench.py:33] `_DROP_ENV = ("SIL_INSTANCE", "SIL_API_KEY", "SIL_FAN_DIR")`: the Bench must set `SIL_FAN_DIR` itself (only `sil up` needs it; `fans` and `set` read the supervisor's library).

### Pitfall 6: `SIL_FAN_DIR` hides the shipped library
New tests for the shipped v2 fans must run with `SIL_FAN_DIR` unset (`t.Setenv("SIL_FAN_DIR", "")`, as `TestShippedArcticFan` already does). `TestShippedArcticFan` and `TestEmbeddedLibraryLoadsCleanly` assert v0.1.0 constants and "ASSUMED" comment lines; they must be rewritten for schema 2 (provenance table, not comments).

### Pitfall 7: Noisy fan on a fixed axis
The scope axis is fixed 0..max_rpm; quantisation steps and ripple near full speed can exceed `max_rpm` by one step (30 rpm). Either clamp the plotted tach to the axis or accept clipping; do not autoscale (03 D-13). Mention in the UI task.

### Pitfall 8: Provenance count semantics
`down` curve is optional; "a curve table counts as one value" so counts are per present value. A fan without `[tach]` or `[datasheet]` simply has fewer values; do not count absent tables.

### Pitfall 9: D-08 versus the CLI output contract
See Open Question 1; the e2e `fans_lists_the_library` test requires exactly one top-level key (`len(out) != 1` fails) so new data must live inside each fan entry.

## Code Examples

### 1. Curve evaluation with v0.1.0 arithmetic (design sketch, not compiled)
```go
// At returns rpm at duty: clamped to the end points, never extrapolated.
func (c Curve) At(duty float64) float64 {
	n := len(c)
	if duty <= c[0].Duty { return c[0].RPM }
	for i := 1; i < n; i++ {
		if duty <= c[i].Duty {
			a, b := c[i-1], c[i]
			return a.RPM + (duty-a.Duty)/(b.Duty-a.Duty)*(b.RPM-a.RPM) // same op order as v0.1.0
		}
	}
	return c[n-1].RPM
}
```

### 2. Keyed-hash noise (executed in scratch module; moments verified)
```go
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }
func z(seed uint64, port int, bucket int64) float64 { // ~N(0,1)
	k := mix(seed ^ bits.RotateLeft64(uint64(port)*0xa24baed4963ee407, 17) ^ uint64(bucket)*0x9fb21c651e98df25)
	s := 0.0
	for i := 0; i < 4; i++ { k = mix(k); s += unit(k) - 0.5 }
	return s * math.Sqrt(3)
}
```
Source: splitmix64 finaliser constants (public domain algorithm) [ASSUMED: from training knowledge; numerical behaviour verified by execution this session, mean 0.0016, variance 0.9989 over 200 000 buckets]. Extend the wall-clock lint (`internal/lint/wallclock_test.go`) to ban top-level `math/rand` calls in `internal/model`.

### 3. Exact-integral step split at noise bucket edges (executed; invariant to tick pattern)
```go
for rem > 1e-12 {
	bi := int64(math.Floor(p.t/bucket.Seconds() + 1e-12))
	step := math.Min(rem, float64(bi+1)*bucket.Seconds()-p.t)
	a := 1 - math.Exp(-step/tau)
	area := target*step + (p.rpm-target)*tau*a           // ∫rpm dt over the step (rpm·s)
	revs := (area + noiseRPM*z(seed, port, bi)*step) / 60 // ripple constant within a bucket
	p.phase += revs * ppr; n := math.Floor(p.phase); p.phase -= n; p.pulses += int64(n)
	p.rpm += (target - p.rpm) * a; p.t += step; rem -= step
}
```
Result for 20 s at constant target 1000, tau 1, noise 6, ppr 2: `633.061734368` total pulses·phase for every tick pattern tried. In the real `Step` the same split must also happen at the start-delay expiry and at a coast-end snap.

### 4. Schema probe and strict v2 decode (executed against go-toml v2.4.3)
```go
var probe struct{ Schema *int `toml:"schema"` }
if err := toml.Unmarshal(data, &probe); err != nil { return decodeError(file, err) } // lenient, syntax errors only
switch {
case probe.Schema == nil: return loadV1(file, data)           // today's rawFan path, unchanged
case *probe.Schema == 2:  dec := toml.NewDecoder(bytes.NewReader(data)); dec.DisallowUnknownFields() ...
default:                  return &Error{File: file, Line: keyLine(data, "schema"), Msg: "schema must be 2 (omit the key for the flat v0.1.0 format)"}
}
```
Observed locations: `unknown start.delay at 4:1`, `unknown foo at 2:1`, `unknown curve.up.rmp at 4:1`; wrong type `decode error ... at 3:8 key=[start duty]`. `StrictMissingError.Errors[0].Position()` and `.Key()` give them (same calls the existing `decodeError` already makes [VERIFIED: internal/fanlib/fanlib.go:149-157]).

### 5. Proposed schema 2 layout (Claude's discretion, prescriptive)
```toml
schema = 2
name   = "Arctic P12 PWM PST"

[sources]                       # named ids, referenced by provenance entries (D-07)
arctic_page = "https://arctic.de/it/P12-PWM-PST/ACFAN00120A"

[stop]
duty = 0.05                     # v1 min_duty; 0 together with a curve point at duty 0 = never stops (D-10)
[start]
duty    = 0.10
delay_s = 0.4
[dynamics]
tau_up_s    = 1.0
tau_down_s  = 2.5
coast_tau_s = 6.0               # required (D-09)

[curve.up]                      # parallel arrays, strictly increasing duty, non-decreasing rpm
duty = [0.05, 0.10, 0.20, 0.40, 0.60, 0.80, 1.00]
rpm  = [200, 300, 480, 820, 1150, 1480, 1800]
[curve.down]                    # optional; absent = same as up
duty = [0.05, 0.10, 0.20, 0.40, 0.60, 0.80, 1.00]
rpm  = [200, 280, 450, 780, 1100, 1440, 1800]

[tach]                          # optional table; absent = ideal tach (no quantisation, no noise)
ppr       = 2
noise_rpm = 6.0

[datasheet]                     # optional; absent = airflow/power null (D-16)
airflow_m3h = 95.7
power_w     = 1.2
ref_rpm     = 1800

[measured]                      # optional metadata written by Phase 6/8 (MODEL-08 names "measured tables"); all keys required when present, no provenance entries
# date, board, esphome, recording_sha256, tolerance (strings)

[provenance]                    # exactly one entry per value present; kind[:source_id]
"stop.duty"            = "datasheet:arctic_page"
"start.duty"           = "assumed"
"start.delay_s"        = "assumed"
"dynamics.tau_up_s"    = "assumed"
"dynamics.tau_down_s"  = "assumed"
"dynamics.coast_tau_s" = "assumed"
"curve.up"             = "assumed:arctic_page"
"curve.down"           = "assumed"
"tach.ppr"             = "assumed"
"tach.noise_rpm"       = "assumed"
"datasheet.airflow_m3h"= "datasheet:arctic_page"
"datasheet.power_w"    = "datasheet:arctic_page"
"datasheet.ref_rpm"    = "datasheet:arctic_page"
```
Value-path registry (the legal provenance keys): `curve.up`, `curve.down`, `start.duty`, `start.delay_s`, `stop.duty`, `dynamics.tau_up_s`, `dynamics.tau_down_s`, `dynamics.coast_tau_s`, `tach.ppr`, `tach.noise_rpm`, `datasheet.airflow_m3h`, `datasheet.power_w`, `datasheet.ref_rpm`. `schema`, `name`, `[sources]` and `[measured]` are metadata and take no entry. Validation rules (each a located error): kinds are exactly `assumed|datasheet|measured|unobservable`; the optional `:id` must be a key of `[sources]`; every present value has an entry and every entry names a present value; all numbers finite; `stop.duty ∈ [0,1)`, `start.duty ∈ [0,1]` and `≥ stop.duty`; taus `> 0`; `delay_s ≥ 0`; `ppr` integer ≥ 1; `noise_rpm ≥ 0`; curves have ≥ 2 and at most 64 points, equal-length arrays, duty in [0,1] strictly increasing, rpm ≥ 0 non-decreasing; `stop.duty == 0` requires an up-curve point at duty 0 with rpm > 0; datasheet values `> 0`; file ≤ 64 KiB ([VERIFIED: internal/fanlib/fanlib.go:37] `const maxFileBytes = 64 << 10`).

### 6. S7a: hermetic emulation of the firmware window (design sketch)
Copy the arithmetic of `SimSensor::on_value/update` (millisecond integer clock, `area_ += last_ * (now - last_t_)`, mean over `window = now - win_start_`) into a Go test; feed it the frames from a real `Plant` at 50 ms bins for a steady rpm, sweeping the window phase 0..1000 ms in 25 ms steps and arrival jitter 0..5 ms from a seeded generator; collect ≥ 20 readings per phase. This needs no Docker and no firmware.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Linear duty→RPM, one tau pair, ideal continuous tach (v0.1.0) | Up/down curves, start delay, coast tau, pulse-accumulator tach, seeded noise | This phase | Default sim shows hysteresis, delay, quantisation |
| Flat nine-key TOML with `# ASSUMED` comments | `schema = 2` tables with machine-readable `[provenance]` and `[sources]` | This phase | Phase 6 `fit` writes the same format; Phase 8 commits measured values |

**Deprecated/outdated:** the ARCHITECTURE.md §2 sketch keys `below_min`, `kick_duty`, `kick_ms` and `[start] duty/delay_s` under a v1-style `source` string are superseded by D-10/D-12/D-07. Its sample datasheet numbers (`airflow_m3h = 114.1`) do not match the vendor page (95.70, below).

## Vendor data (D-04)

| Item | Result | Tag |
|------|--------|-----|
| Arctic page `https://arctic.de/it/P12-PWM-PST/ACFAN00120A` (the URL the file already cites) | Title "P12 PWM PST \| 120 mm PWM-Lüfter mit Kabelsplitter \| ACFAN00120A \| ARCTIC"; "200—1800 rpm PWM-gesteuert (0 rpm unter 5 % PWM)"; "95,70 m³/h" and "56,30 cfm"; "0,1 A \| 12 V"; "Typische Spannung: 12 V DC"; power in W **not stated** | [CITED: arctic.de/it/P12-PWM-PST/ACFAN00120A, fetched 2026-11-10] |
| Arctic page `https://www.arctic.de/en/P12-PWM-PST/ACFAN00170A` | Same values (200-1800 rpm, 0 rpm below 5 % PWM, 95.70 m³/h, 56.30 cfm, 0.1 A / 12 V), plus starting voltage "3,9 V", static pressure "2,20 mmH2O", noise "28 dB(A)"; pulses per revolution **not stated** | [CITED: arctic.de/en/P12-PWM-PST/ACFAN00170A, fetched 2026-11-10] |
| SKU | Both ACFAN00120A and ACFAN00170A are live pages with identical rows; the page the repo cites is ACFAN00120A (with cable splitter in the title). Keep ACFAN00120A as `arctic_page`; the real SKU is to be read off the box at Phase 8 bench setup (already a Phase 8 concern) | [CITED] |
| Noctua spec pages `noctua.at/en/nf-a12x25-pwm/specification`, `…/products/fan/nf-a12x25-pwm/specification`, `www.noctua.at/en/products/nf-a12x25-pwm/specifications` | **Not fetched: HTTP 429** from both WebFetch and curl (three URLs) | [VERIFIED: tool output this session] |
| Noctua datasheet PDF (retailer-hosted copy, PDF title "NOCTUA NF-A12x25 PWM Datasheet EN", created 2021-05-05) `https://123-3D.nl/pdf/DMO00072_info.pdf` | "Max. rotational speed (+/–10%) 2000 RPM"; "Max. airflow 102.1 m³/h"; "Max. input power / voltage 1.68W / 12V"; LNA variant 1700 RPM / 84.5 m³/h. **No minimum-speed row** | [CITED: 123-3D.nl/pdf/DMO00072_info.pdf, text extracted with pdftotext] |
| Noctua minimum 450 rpm at 20 % PWM | A search summary says the product page gives 450 RPM ±20 % and another subpage 460 rpm; not read from a fetched page | [ASSUMED] |

**Prescribed provenance for the shipped files (per D-04):** Arctic `stop.duty`, `datasheet.airflow_m3h`, `datasheet.ref_rpm` → `datasheet:arctic_page`; Arctic `datasheet.power_w = 1.2` is 0.1 A × 12 V, both page-confirmed, the product is a derived value: mark it `datasheet:arctic_page` and say "0.1 A x 12 V" in a comment (Assumption A2, flag for the user). Noctua `datasheet.airflow_m3h = 102.1`, `ref_rpm = 2000`, `power_w = 1.68` → `datasheet:noctua_datasheet` (source id URL = the PDF, plus an unfetched `noctua_page` id for reference); the 1.68 W is the *maximum* input power, consistent with `ref_rpm` = max speed. Everything else (curves, delay, taus, coast, noise, ppr, Noctua stop/start/floor) → `assumed`. `tach.ppr = 2` is assumed (Arctic page silent; Intel 4-wire spec says 2 per FEATURES research [CITED: .planning/research/SUMMARY.md "Intel 4-wire PWM fan specification (2 ppr...)"]).

## Spike S7 scheduling

**What it decides:** whether the Go pulse accumulator's 30 rpm steps remain recognisable after the firmware's 1 s time-weighted mean, or the analytic fallback is needed.

**Modelled prior (Python emulation of the `sim_sensor.cpp` arithmetic; not a measurement) [ASSUMED for real-world jitter]:** mean bias ≤ 0.4 rpm at ≤ 2 ms jitter, ≤ 2.3 rpm at 10 ms; steady spread and thresholds above; at 1800 rpm (60 pulses/s, exactly 3 per bin) readings are exactly on grid; at awkward pulse rates the on-grid fraction is 0.4-0.65 depending on window phase. Analytic fallback (gate-held 1 s count): same blur proportional to the gate/window phase offset and about one extra second of lag; therefore **fallback does not buy exactness**, only removes frame-arrival-jitter sensitivity.

**Schedule (two steps, in this order, both inside the phase):**
1. **S7a, hermetic, right after `model.Tach` exists (before engine wiring):** Go test as in Code Example 6. Pass when, for steady 500, 1000, 1100, 1500 rpm at ppr 2 (include a non-integer pulse rate): (a) |mean(reading) − 30·pulses/s| ≤ 1 rpm over ≥ 20 readings; (b) peak-to-peak spread ≤ 60 rpm for arrival jitter ≤ 5 ms and ≤ 90 rpm at 10 ms; (c) at a non-integer pulse rate at least 2 distinct levels appear (quantisation visible; an ideal tach gives 1). Report the on-grid fraction but do not gate on it. Fail → fix the frame cadence (grid-align `nextRPM` to multiples of 50 ms sim time) before considering the fallback.
2. **S7b, live, after engine wiring (folds into the contract test):** pin a schema 2 fixture with a flat curve at a known steady rpm and `[tach] ppr = 2`, `noise_rpm = 0`; on `ct-main` set a port to it; read `Fan N Speed` states for ≥ 20 s through aioesphomeapi (what Home Assistant sees) and assert the same (a)-(c). This is the "contract test shows the firmware-visible steps" part of criterion 3.
**Decision rule:** if S7b fails (b) at ≤ 5 ms measured arrival jitter while S7a passed, the cause is firmware-side processing jitter; switch to the gate-held analytic variant for the *bus value* only (status `tach_rpm` unchanged), accept the extra lag, and record it in PROJECT.md Key Decisions. Protocol stays 1 either way.

## Test Impact and Verification Map

(`workflow.nyquist_validation` is `false` in `.planning/config.json`, so no Validation Architecture section; this is the planning-relevant test map instead.)

| Area | Files | Change |
|------|-------|--------|
| Frozen reference | `internal/model/fan_v010_test.go` (new) | copy of v0.1.0 `Step`; randomised differential test with `==` |
| Model | `internal/model/*_test.go` (new) | dt-composability for the new state (`Step(2dt)` = two `Step(dt)` incl. across delay expiry), monotone curve property, direction hysteresis, start/stop/coast, never-stops, tach pulse totals, noise moments and tick independence, `Frame` unit conversion, S7a |
| Loader | `internal/fanlib/fanlib_test.go` | existing v1 tests stay (the `base` fixture is v1); add v2 table-driven errors with line:col (unknown key, missing provenance, extra provenance, bad kind, undefined source, NaN, non-monotone curve, `stop.duty = 0` without floor point, `schema = 3`); rewrite `TestShippedArcticFan` and `TestEmbeddedLibraryLoadsCleanly` for schema 2; round-trip test if `Marshal` lands |
| Fixtures | `testdata/fans-v0.1.0/*.toml` (new, byte for byte) | `git show v0.1.0:fans/arctic-p12-pwm-pst.toml` (tag `v0.1.0` exists) copies; add a test that fails if they change (hash pin) |
| Engine | `internal/engine/engine_test.go` | `fanParams`, `fanlib.DefaultFan` call sites (lines ~32, 175, 1300-1475) must load the fixtures dir; new test: engine on `clock.Manual` vs direct `Plant` run gives identical frame values; hello snapshot uses `Current()`; fan swap makes a fresh plant |
| Supervisor | `internal/supervisor/watch_test.go` (loads `fanlib.DefaultFan`), `supervisor_test.go` (`libFake` returns legacy `Params`: still valid), status mapping test for the new fields | pin or keep |
| Control/events | `internal/control/control_test.go`, `internal/events/events_test.go`, `internal/cli/*_test.go` (`operations_test.go` unaffected: no new Operations rows) | new fields, `--verbose`, `--seed` parse |
| uitest (needs Chrome, present: `/usr/bin/google-chrome`) | `bench_test.go` (second `FanInfo` builder), `parity_test.go::sessionSteps` (+`tach`, `load`), `plot_test.go` (axis 1800 assumption), `layout_test.go` (row gets more readouts) | extend; run `make test-ui` |
| e2e (Docker, image `ghcr.io/esphome/esphome:2026.10.1` present) | `model_test.go` (pin + `TestFanTomlUnknownKey` typo on a v2 key), `fans_test.go` (pin; add a shipped-v2 `sil fans` schema/provenance test), `io_test.go` (`TestRPMMean`, pin), `loop_test.go` (loads `DefaultFan`), `web_test.go` (pin) | `make e2e` |
| Python | `tests/silbench.py` (set `SIL_FAN_DIR`), `tests/test_contract.py` (789.5 rpm), `tests/test_api_smoke.py` (789.5 within 2 %), new S7b assertion | `make test-contract` |
| Lint | `internal/lint/wallclock_test.go` | forbid top-level `math/rand` in `internal/model` |
| Gate | `make check` (fmt-check vet test test-cpp) | must stay green; C++ untouched, `protocol/` untouched |

The existing `e2e/web_test.go` and `uitest` suites assert the page against the *shipped* library; after D-01 those that do not need exact v0.1.0 numbers should still run on the shipped v2 fans (that is what "new tests cover the shipped v2 fans" means), the rest pin the fixtures.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Curve direction = sticky sign of the most recent duty change (up on rising, down on falling, initial up), no deadband | Pattern 2 | Real fans may switch branches differently (e.g. by rpm vs target); Phase 8 data decides, cost is a model iteration |
| A2 | Arctic `power_w = 1.2` (0.1 A × 12 V) tagged `datasheet`, although the page states current and voltage, not watts; D-04 read literally could demand `assumed` | Vendor data | Provenance overstates; user may prefer `assumed`. **Resolved 2026-11-10 (user, D-04 amended): derived values stay `datasheet` with the derivation recorded.** |
| A3 | D-10 ("v1 adapter maps hold onto stop_duty = 0 + floor point") is realised by model-side normalisation of legacy fields, with `BelowMin` as the explicit derived marker, rather than the loader emitting curve-only Params | Pattern 1 | If the user wants the loader to emit curve form literally, the v1 corner (`min_duty = 0`, stop) needs a separate initial-state flag. **Resolved 2026-11-10 (user, D-10 mechanism amended, deviation accepted).** |
| A4 | Noise acts on the tach path only; `status.rpm`, airflow and power stay noise-free; "a few rpm" noise is only subtly visible after quantisation (about 1 % of rpm would be obvious) | Pattern 5 | User expects visible jitter on the shaft trace; changing it later alters `rpm` events and the Phase 5/6 truth. **Resolved 2026-11-10 (user, D-03 amended): tach-only noise, default about 15-20 rpm. The plans define `noise_rpm` as the standard deviation of a 1 s reading (per-bucket ripple scaled by √10) and ship 20; under the per-bucket reading of Pattern 5, 20 would average down to about 6 rpm at the 1 s reading.** |
| A5 | `tach_rpm` is `null` for ideal-tach fans (no `[tach]`) and until the first 1 s gate completes | Pattern 4 | Consumers may prefer `tach_rpm == rpm` for ideal fans; Phase 5's dead-tach fault must then still produce a non-null value |
| A6 | Real-world frame arrival jitter on the dev machine is ≤ 5-10 ms; the S7 prior is a model, not a measurement | S7 | S7b may fail thresholds on a loaded host; fallback and thresholds are stated |
| A7 | `[measured]` accepted as an optional metadata table now (MODEL-08 names "measured tables"; CONTEXT silent) | Code Example 5 | If omitted, Phase 6/8 would need a schema 2 amendment |
| A8 | Noctua values `102.1 m³/h`, `2000 rpm`, `1.68 W` come from a retailer-hosted copy of the Noctua datasheet, not from noctua.at (429); minimum 450 rpm is from a search summary | Vendor data | D-04 says "vendor spec pages"; the user may want a manual confirmation of the Noctua page. **Resolved 2026-11-10 (user, D-04 amended): an official vendor PDF counts wherever hosted.** |
| A9 | The assumed curve/coast/noise numbers in Code Example 5 and the Noctua analogue are plausible-looking placeholders, not sourced | Code Example 5 | None beyond D-03's own intent; Phase 8 replaces the Arctic ones |
| A10 | splitmix64 constants and the Irwin-Hall construction are standard public algorithms | Code Example 2 | Numerically checked here, so low risk |
| A11 | Gate reading unit: `reading = pulses_in_gate · 30` at `BoardPPR = 2`, from earlier research of the product YAML (`multiply: 0.5`, `update_interval: 1s`); the product YAML was not re-read this session | Pattern 4 | A different board multiplier changes the step size. **Still open (plan revision 2026-11-10): `multiply 0.5` and `update_interval 1s` appear only in the Rev 1.0-3.1 hardware YAML; the Rev 3.3 hardware YAML has no `pulse_counter`. To be confirmed on hardware in Phase 7/8 (recorded in ROADMAP).** |

## Open Questions (RESOLVED)

All five questions were resolved at plan-phase revision on 2026-11-10. The original text is kept, with the resolution added inline.

1. **D-08 versus the one-JSON-value CLI contract**
   - What we know: stdout of every `sil` verb is exactly one JSON value (enforced by `tests/silbench.py` and `writeJSON`); `e2e/fans_test.go` requires the top-level object to have exactly one key.
   - What's unclear: "summary by default, `--verbose` every value, JSON always carries the full map" cannot all be true for stdout.
   - Recommendation: JSON always carries `provenance` counts and the full `values` map (in each fan entry, additive); the human rendering (one summary line per fan, or the full list with `--verbose`) goes to stderr. Alternative: `--verbose` only trims/expands the JSON, which contradicts "always full". Planner should confirm with the user.
   - **RESOLVED (user decision, 2026-11-10; D-08 amended in 04-CONTEXT.md):** the recommendation holds. stdout JSON always carries schema, provenance counts and the full per-value map in each fan entry, and the human summary (and the `--verbose` per-value list) goes to stderr. Planned in 04-03; assumption A-D08 is resolved.
2. **Where the seed is stored.** D-15 says run.json; the flag is read by `sil up` and run.json is written later by the supervisor ([VERIFIED: internal/supervisor/supervisor.go:248-251] `rf := instance.RunFile{ Schema: 1, Instance: s.id.Name, PID: os.Getpid(), ...`), which does not see CLI flags. Recommendation: put `seed` in `spec.json` (`instance.Spec`, [VERIFIED: internal/instance/instance.go:488-499] `type Spec struct { ... Fans []string ...}`, written by `lifecycle.Up` next to `Fans`) and echo it in status `sim.seed`; optionally mirror it into run.json.
   - **RESOLVED (planner, recorded as assumption A-SEED in 04-05):** the seed travels in spec.json (written by `sil up`, read by the supervisor). The supervisor copies it into run.json, so D-15's "stored in run.json" holds literally, and into status `sim.seed`. The range is 0..2^53−1, and the default is `lifecycle.DefaultSeed = 0x5EED`.
3. **Empty-value glyph.** D-16 says the page shows `—`; the page uses `const NONE = "--";` ([VERIFIED: ui/web/js/view.js:10]) everywhere for null. Recommendation: reuse the existing `--` for consistency unless the user wants the em dash.
   - **RESOLVED (planner default, recorded as assumption A-GLYPH in 04-07):** the new readouts use the page's existing `--` for null. Changing the glyph would change every readout of the page. A-GLYPH stays flagged, so the user can still ask for the em dash at review.
4. **UI contract.** The phase has `UI hint: yes` and no `UI-SPEC.md`; the port row gains tach, airflow and power readouts and a third series, and `layout_test.go` pins fixed widths. Either run `/gsd-ui-phase 4` first or give the UI plan an explicit layout acceptance check.
   - **RESOLVED (planner, recorded as assumption A-NOUISPEC in 04-07):** no UI-SPEC. The UI plan 04-07 carries explicit, testable layout criteria instead: at 1280×720 no page scroll, each scope at least 128 px high, the new readout row below the first row and inside its panel, fixed readout widths, no sideways overflow at 390×844, and one column at 1000 px, all in uitest/layout_test.go. An end-of-phase human check covers the look of the stepped trace.
5. **Noctua page.** noctua.at was rate-limited (429) on every attempt; a retry later (or a human glance) would let the Noctua min-speed row and the power figure be confirmed against the vendor page.
   - **RESOLVED (user decision, 2026-11-10; D-04 amended):** an official vendor datasheet PDF counts as `datasheet` wherever it is hosted, with the source URL recorded. The Noctua 102.1 m³/h, 1.68 W and 2000 rpm from the retailer-hosted official PDF stay `datasheet`, and the Noctua page is not needed. The 450 rpm floor is not in the PDF and stays `assumed`. Planned in 04-08; assumption A8 is resolved.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | everything | ✓ | 1.27.3 (`/home/claude/.local/bin/go`) | — |
| Docker + pinned ESPHome image | `make e2e`, `make test-contract`, S7b | ✓ | Docker 29.9.3; `ghcr.io/esphome/esphome:2026.10.1` (also 2026.8.0) present | S7a runs without it |
| Chrome | `make test-ui` | ✓ | `/usr/bin/google-chrome` | — |
| uv / Python 3 | `make test-contract` | ✓ (`uv` in `~/.local/bin`) | — | — |
| g++ | `make test-cpp` (in `make check`) | not probed; C++ is untouched this phase | — | — |
| pdftotext | one-off vendor datasheet read | ✓ | — | not needed in the build |

**Missing dependencies with no fallback:** none. **Missing with fallback:** none.

## Project Constraints (from CLAUDE.md)

From `/home/claude/esphome-sil/.claude/CLAUDE.md` and `/home/claude/CLAUDE.md`:
- Simulator, CLI and web server in **Go**; no Python in the simulator; product-repo tests may be pytest driving `sil` via CLI/JSON.
- Stdlib first: `testing`/`synctest` (no testify), `net/http` `ServeMux`, `log/slog` to **stderr only** (stdout is the CLI's one JSON value), kong CLI, go-toml v2 with `DisallowUnknownFields`.
- **Web UI: no build step, no external CDN**, vendored uPlot 1.6.32 and (per the stack note) Alpine.js; keep uPlot instances and ring buffers out of reactive state; system font stack.
- Sim-bus protocol, web UI, `fan.toml` schema and Lua API are internal while `0.x`; **protocol stays version 1, `components/sim_bus` and `protocol/` untouched** this phase.
- Chip-agnostic sim layer; ESPHome pinned 2026.10.1.
- Before editing, start through a GSD command (this research feeds `/gsd-plan-phase`); no direct repo edits outside a GSD workflow.
- Project conventions from the repo: all errors through the `apperr.Registry` (AST-checked in `make check`); wall-clock lint bans `time.Now`/timers in non-infrastructure code; `make check` = `fmt-check vet test test-cpp` must stay green; git branch model `claude/main` ← `claude/feature-*`.

## Security Domain

`security_enforcement` is enabled (ASVS level 1, block on high). No new network surface, endpoint, listener or credential is added; the `fans` row is read-only and already LAN-visible.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | unchanged (control API is a 0600 Unix socket; the LAN web listener is unauthenticated by existing decision) |
| V3 Session Management | no | — |
| V4 Access Control | minimal | no new `control.Operations` rows; `fans` stays `LAN: true` read-only ([VERIFIED: internal/control/operations.go:52] `{Name: "fans", Method: "GET", Path: "/api/v1/fans", ... LAN: true, ...}`) and now also carries source URLs (public vendor links) |
| V5 Input Validation | **yes** | strict TOML decode, 64 KiB file cap, finite-number checks, array length cap (suggest 64 points per curve, a cap on provenance entries), integer ppr range, kind/source grammar, `--seed` parsed as uint64 by kong |
| V6 Cryptography | no | the keyed-hash noise is **not** security-relevant randomness; no secrets involved |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| NaN/Inf or huge numbers in a user-edited `SIL_FAN_DIR` file poison the plant | Tampering / DoS | reject non-finite and out-of-range values at load (go-toml accepts `nan`/`inf`, verified); model already treats NaN duty as 0 |
| Oversized or degenerate curve arrays (millions of points, equal duties) | DoS | file size cap plus point-count cap plus strict-increase check (avoids divide-by-zero in interpolation) |
| Unbounded provenance/source maps or very long strings echoed into status and the page | DoS / Info | cap entry count and string length; the page renders text with `textContent` (existing pattern), no HTML interpolation |
| Path traversal via fan id | Tampering | unchanged `idPattern` and `..` check in `fanlib.Load` |
| Source URLs shown on the page as links | Spoofing | render as plain text, not `href`, or keep out of the page (the page only needs the readouts) |

## Sources

### Primary (HIGH confidence)
- Repository code read this session: `internal/model/fan.go`, `internal/model/fan_test.go`, `internal/fanlib/fanlib.go` + test, `internal/engine/engine.go` (lines 25-100, 100-343, 355-575, 680-800, 1000-1020), `internal/control/control.go`, `internal/control/operations.go`, `internal/events/events.go`, `internal/supervisor/supervisor.go` (120-260, 345-375, 700-760, 1025-1090, 1180-1290), `internal/instance/instance.go` (Spec, RunFile), `internal/lifecycle/lifecycle.go`, `internal/cli/cli.go`, `components/sim_bus/sim_sensor.{h,cpp}`, `ui/web/{index.html,js/*.js}`, `e2e/{model,fans,io}_test.go`, `uitest/{bench,parity,inputs}_test.go`, `tests/{silbench.py,test_contract.py,test_api_smoke.py}`, `Makefile`, `examples/rev33.yaml`, `fans/*.toml`
- Executed in the scratchpad: go-toml v2.4.3 strict/probe/position behaviour; tick-invariance and noise-moment program; Python emulation of the firmware window for S7
- `.planning/phases/04-fan-model-v2/04-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `.planning/ROADMAP.md`

### Secondary (MEDIUM confidence)
- `.planning/research/{ARCHITECTURE,SUMMARY,PITFALLS}.md` (v0.2.0 milestone research; product YAML facts are cited second-hand, A11)
- https://arctic.de/it/P12-PWM-PST/ACFAN00120A and https://www.arctic.de/en/P12-PWM-PST/ACFAN00170A (fetched; spec rows quoted above)
- https://123-3D.nl/pdf/DMO00072_info.pdf (Noctua datasheet PDF, retailer copy; text extracted)

### Tertiary (LOW confidence)
- Web search summary for the Noctua minimum speed (450 vs 460 rpm); Noctua pages themselves returned HTTP 429

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH, no new dependency, go-toml behaviour executed
- Architecture: HIGH for seams and file lists (read in code); MEDIUM for the S7 outcome (modelled, to be measured)
- Pitfalls: HIGH for test-pinning and loader pitfalls (read in code); MEDIUM for fan-physics choices (A1, A4)
- Vendor data: HIGH for Arctic, MEDIUM for Noctua (datasheet copy, page unreachable)

**Research date:** 2026-11-10
**Valid until:** 2026-12-10 (stable domain; re-check only if the vendor pages or ESPHome pin change)

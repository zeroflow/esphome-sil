# Phase 4: Fan Model v2 - Context

**Gathered:** 2026-11-10
**Status:** Ready for planning

<domain>
## Phase Boundary

Fan model v2 replaces the linear fan behind a single `model.Plant` that the engine uses now and an offline `internal/replay` will use later. It adds separate up and down duty→RPM curves, distinct start and stop duties with a start delay, coast-down with its own dynamics, fans that never stop, a pulse-accumulator tach with the fan's ppr (30 rpm steps in the firmware's 1 s window at ppr 2), deterministic seeded noise, and airflow and power from datasheet values. It is driven by an explicit `fan.toml` schema 2 with per-value provenance. Schema 1 files keep loading and behaving exactly as in v0.1.0. The sim-bus stays protocol version 1, and nothing changes on the C++ side.

Not in this phase: fault injection (Phase 5), recording, fit, verify and report (Phase 6), any hardware measurement (Phases 7-8), and the start kick (deferred, see D-12).

</domain>

<decisions>
## Implementation Decisions

### Shipped fan library
- **D-01:** Both shipped fans (`fans/arctic-p12-pwm-pst.toml`, `fans/noctua-nf-a12x25-pwm.toml`) are rewritten as **schema 2 in Phase 4**, so the default `sil up` shows v2 behaviour from now on. The exact v0.1.0 schema 1 files are kept byte for byte as test fixtures (testdata). The tests prove success criterion 5 ("every shipped schema 1 file behaves exactly as in v0.1.0") on those fixtures. — **Reversibility:** costly — the default fan's behaviour changes for every consumer, and the product repo's pytest scenarios run against the default fan.
- **D-02:** Existing tests that compare RPM exactly (`e2e/model_test.go`, `io_test.go`, `uitest/parity_test.go`, `tests/test_contract.py` and similar) **pin an ideal test fan via `SIL_FAN_DIR`**: a schema 1 fixture, or schema 2 with an ideal tach. Their assertions stay exact. New tests cover the shipped v2 fans. The contract test asserts the firmware-visible 30 rpm steps explicitly.
  - *Amendments (2026-11-11, plan-phase checker iteration 3; user decision):* the user accepts a narrower D-02 scope for `uitest/parity_test.go`. It stays on the shipped library and is not pinned via `SIL_FAN_DIR`, because the uitest fake bench builds a literal `control.Status`, parity asserts only literals it wrote, and the fanlib fields it reads (id, name, min_rpm, max_rpm, below_min) are equal in v0.1.0 and schema 2 (verified by the plan checker; 04-08 records the exemption). The user also accepts the 14 flagged-unresolved edge assumptions (EDGE-*-unclassified, A11 and the others) as execution-time review items: each executor reviews its plan's flagged edges against the listed coverage and resolves them in its SUMMARY. A11 (Rev 3.3 tach at 30 rpm per pulse) stays carried to Phase 7/8 per ROADMAP.
- **D-03:** Values with no source yet (down curve, start delay, coast tau, noise amplitude) get **plausible assumed values** marked `assumed`: a down curve slightly below the up curve, a start delay of about 0.4 s, and noise of a few rpm. The point is that the default sim visibly shows hysteresis, delay and noise. Measuring is deliberately not done here: the tooling arrives in Phases 6-7, and Phase 8 replaces the Arctic values with measured ones. The Noctua stays `assumed` until v0.3.
  - *Amendments (2026-11-10, plan-phase revision; user decision):* Noise stays on the tach path only. Shaft `rpm` stays noise-free physics, so D-13's meaning is unchanged. The shipped default amplitude becomes about 15-20 rpm instead of "a few rpm", chosen against the 30 rpm quantum so that the firmware-visible trace visibly jitters. Plan realisation (04-04, 04-08): `tach.noise_rpm = 20` in both shipped files, where `noise_rpm` is the standard deviation of the noise in one 1 s tach reading (two thirds of a 30 rpm step).
- **D-04:** The researcher **fetches the Arctic and Noctua vendor spec pages** for airflow (m³/h), power (W) and reference rpm, and records each URL. A value that a fetched page does not confirm is marked `assumed`, not `datasheet`. The same fetch settles the Arctic SKU (ACFAN00120A vs ACFAN00170A) that the file cites.
  - *Amendments (2026-11-10, plan-phase revision; user decision):* `datasheet` provenance is also allowed for (a) values derived arithmetically from datasheet figures, with the derivation recorded (Arctic power_w = 0.1 A × 12 V = 1.2 W), and (b) values from an official vendor datasheet PDF wherever it is hosted, with the source URL recorded (Noctua 102.1 m³/h and 1.68 W stay `datasheet`; the same PDF's 2000 rpm falls under the same rule). The Arctic SKU (ACFAN00120A vs ACFAN00170A) is explicitly deferred to Phase 8, where it is read off the box. The fetch cannot settle it because both pages are identical.

### fan.toml provenance
- **D-05:** Provenance goes in a **parallel `[provenance]` table** keyed by dotted value paths (`"start.duty" = "assumed"`, `"curve.up" = "datasheet"`), and values stay plain numbers. The four provenance kinds are `assumed`, `datasheet`, `measured` and `unobservable`. A curve table counts as one value. — **Reversibility:** costly — Phase 6 `fit` writes this format, and Phase 8 commits a measured file in it.
- **D-06:** A provenance entry is **required for every value**. A missing entry, an entry for a value that does not exist, or an unknown kind is a located load error. Provenance has no silent default (consistent with 01-05 D-14).
- **D-07:** Sources live in a **`[sources]` table of named ids** (`arctic_page = "https://..."`). A provenance entry may reference one, e.g. `"datasheet:arctic_page"`. The loader rejects a reference to an undefined source id. Comments stay free for human notes. The v1 `source` string is not used in schema 2.
- **D-08:** By default, `sil fans` shows a **summary per fan**: id, name, schema and provenance counts (e.g. `measured 0 · datasheet 3 · assumed 9`). `--verbose` lists every value with its provenance and source. JSON output always carries the full per-value map. `control.FanInfo` changes are additive.
  - *Amendments (2026-11-10, plan-phase revision; user decision):* stdout JSON always carries schema, provenance counts and the full per-value map in each fan entry. The human summary (and the `--verbose` per-value list) goes to stderr. stdout stays exactly one JSON value (the CLI contract). Assumption A-D08 is resolved.

### Start / stop / coast semantics
- **D-09:** Coast-down after a stop uses its own **`coast_tau_s`, required** in `[dynamics]`. Unpowered coast-down (inertia and friction only) is distinct from the regulated `tau_down_s`. There is no fallback to `tau_down_s`, so schema 2 has no silent default. The shipped files get an assumed value.
- **D-10:** "Never stops" is expressed as **`stop_duty = 0` plus a curve point at duty 0** carrying the floor rpm. There is no mode flag, and v1's `below_min` does not exist in schema 2. The v1 adapter maps `below_min = "hold"` onto this form so that v1 behaviour is identical.
  - *Amendments (2026-11-10, plan-phase revision; user decision):* the mechanism wording is amended, and the deviation is accepted. The v1 adapter does not rewrite a hold file into the curve form in the loader. The model normalises the legacy fields internally: a v1 hold fan gets the synthesised curve (0, min_rpm), (min_duty, min_rpm), (1, max_rpm), and `BelowMin` stays a derived marker (for schema 2: hold iff `stop_duty = 0`). The behaviour is identical to the curve form. Schema 2 still has no `below_min` key and no mode flag. Assumption A3 is resolved.
- **D-11:** Once a fan drops below `stop_duty` it **counts as stopped, even while it is still coasting**. When duty rises above `start_duty` again, the start delay re-arms, and after the delay the fan re-accelerates from its current coast rpm. This continues v0.1.0's "hysteresis follows duty" rule.
  - *Amendments (2026-11-11, plan-phase revision iteration 2; user decision):* the start boundary is **at or above**. "rises above `start_duty`" reads "reaches or exceeds `start_duty`": a duty exactly equal to `start_duty` starts a stopped fan after the start delay, and the delay re-arms the same way. v0.1.0's `duty >= StartDuty` (and `duty < MinDuty` for the stop) are kept. ROADMAP Phase 4 criterion 2 ("exceeds" → "reaches") and REQUIREMENTS MODEL-06 ("only above" → "only at or above") are reworded to match. Assumption EDGE-MODEL-06-unclassified is resolved into an explicit truth in 04-02.
- **D-12:** **No kick in v2.** Start duty plus start delay is enough. MODEL-06 and Phase 4 success criterion 2 are amended to drop "and kick" / "(with a kick if configured)". Kick is a deferred idea, to revisit if Phase 8 data shows a start boost the model cannot express. — **Reversibility:** reversible — adding an optional `[start]` sub-block later is additive.

### RPM vs tach readout
- **D-13:** `sil status` and `sil watch` carry **both values**: `rpm` keeps meaning shaft truth (field unchanged), and the new `tach_rpm` is the last completed 1 s gate reading, i.e. what the firmware's tach path reports. Both changes are additive, and Status `schema` stays 1. Phase 5's dead-tach and wrong-ppr faults rely on seeing both.
  - *Amendments (2026-11-11, plan-phase revision iteration 2; user decision):* for an **ideal-tach fan** (no `[tach]`, including every schema 1 fan) `tach_rpm` is the **1 s mean of the shaft rpm**, which is what `sim_sensor` reports for the ideal frames. It uses the same sim-time 1 s gate as the pulse reading. The rule for null: `tach_rpm` is null only while the port's plant has not yet completed its first 1 s gate, i.e. during the first second of sim time after sim start, sim restart or a fan swap on that port. From then on it is a number for every fan, running or not (0 for a fan at rest). Shaft `rpm` and everything the firmware sees for schema 1 fans (bus frames, hello in-snapshot) stay bit-identical to v0.1.0, and `tach_rpm` is a new additive status field. Assumption A5 is resolved (04-04, 04-06).
- **D-14:** The port scope on the web page draws **two traces**: shaft RPM solid, and tach RPM as a stepped trace in the same port colour, dimmer (port colour per 03 D-03). The readout shows both numbers. History samples gain a tach value. Axes stay fixed from 0 to `max_rpm` (03 D-13). For a schema 2 fan, `max_rpm` is the curve maximum.
- **D-15:** The noise seed defaults to a **fixed constant mixed with the port number**, so every run is reproducible out of the box. `sil up --seed N` overrides it, and the seed is stored in run.json and shown in status. The seed survives sim restart and firmware restart within one instance, like the fan list does.
- **D-16:** Airflow and power for a fan with no datasheet values (such as a schema 1 test fan) are **JSON `null`**, and the page shows `—`. There is never a made-up zero (same rule as the stale values in 03 D-10). The fields are always present.
  - *Amendments (2026-11-11, plan-phase revision iteration 2; user decision):* the em dash `—` applies to the **three new readouts only**: TACH RPM, AIRFLOW and POWER show `—` when their value is null. Every existing readout of the page keeps its `--`. This reverses the orchestrator default recorded as assumption A-GLYPH ("reuse `--`"), which is resolved by the user (04-07, 04-10).

### Carried forward (already decided, not re-asked)
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

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Scope and requirements
- `.planning/ROADMAP.md` §Phase 4 — goal, success criteria (criterion 2 to be amended per D-12), spike S7
- `.planning/REQUIREMENTS.md` — MODEL-05..MODEL-12 (MODEL-06 to be amended per D-12)
- `.planning/PROJECT.md` — constraints, Key Decisions table

### Research (v0.2.0)
- `.planning/research/SUMMARY.md` — resolved conflicts C1 (tach in Go), C5 (noise hash), C17/C18 (schema, provenance, drop `below_min`)
- `.planning/research/ARCHITECTURE.md` §1-2, §5 — `model.Plant`, schema 2 sketch, v1 adapter, tach accumulator and the frame/gate maths, test impact
- `.planning/research/PITFALLS.md` — nondeterministic noise, schema break, tach quantisation
- `.planning/research/FEATURES.md` — fan behaviour table stakes (curves, thresholds, coast, fan laws)
- `.planning/research/STACK.md` — no new Go modules; `text/template` writer note

### Prior phase decisions
- `.planning/milestones/v0.1.0-phases/01-closed-loop-fan-skeleton/01-CONTEXT.md` — original model semantics D-11..D-14
- `.planning/milestones/v0.1.0-phases/02-full-rev-3-3-bench/02-CONTEXT.md` — per-port fan swap (D-10)
- `.planning/milestones/v0.1.0-phases/03-live-web-ui/03-CONTEXT.md` — scope look, port colours, fixed axes, stale rule (D-01..D-13)

### Wire / firmware (must stay unchanged)
- `protocol/PROTOCOL.md` — sim-bus v1
- `components/sim_bus/sim_sensor.h` — 1 s time-weighted RPM window the tach design relies on

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/model/fan.go`: pure `Step(Params, State, duty, dt)`. Its tests (`fan_test.go`, including the dt-composability test) are the regression guard for v1 behaviour via the adapter.
- `internal/fanlib/fanlib.go`: strict go-toml loader with `DisallowUnknownFields`, located `Error{File,Line,Col}`, `SIL_FAN_DIR` override and embedded `fans.FS`. Schema 2 extends it, and v1 decoding (`rawFan`) becomes the adapter path.
- `ui` vendored uPlot scope per port (Phase 3): it gains a second, stepped series.

### Established Patterns
- Zero values mean v0.1.0 behaviour (no curve gives the linear target, no tach gives an ideal continuous tach), so the v1 adapter conversion is trivially equivalent.
- Status and event fields are additive only ("extended, never renamed"), and Status `schema` stays 1.
- The model reads no clock. Cadence comes from the engine constants (`tick` 10 ms, `rpmFrameStep` 50 ms).
- All errors go through the `apperr.Registry` (AST-checked in `make check`). Any new load or seed error needs a registered code.

### Integration Points
- `internal/engine/engine.go:554`: `p.state = model.Step(...)` becomes `p.plant.Step(...)`, and `sendRPM` sends `plant.Frame()`.
- `internal/engine/engine.go:294,793`: `model.Initial` on start and on fan swap.
- `control.PortStatus` / `control.FanInfo`, `events.Sample` (tach), and `internal/cli` `FansCmd` (`cli.go:448`) for D-08.
- `sil up` flags and run.json for `--seed` (D-15).

</code_context>

<specifics>
## Specific Ideas

- The tach trace on the scope should make the 30 rpm stepping visible at a glance, next to the smooth shaft trace.
- The user asked "why not measure?" for the unknown values. The answer was recorded: measuring happens in Phases 7-8 with proper tooling. Phase 4 ships plausible assumed values so the behaviour is visible now.

</specifics>

<deferred>
## Deferred Ideas

- **Start kick** (a fan-internal start boost at kick_duty for kick_ms): dropped from v2 (D-12). Revisit if the Phase 8 Arctic data shows a start transient the model cannot express, or for the Noctua in v0.3.
- **Catching a coasting rotor** (re-engaging without a start delay while RPM > 0): not modelled. D-11 re-arms the delay instead.
- **Arctic SKU confirmation** (ACFAN00120A vs ACFAN00170A; added 2026-11-10, plan-phase revision, D-04 amendment): deferred to the Phase 8 bench setup, where it is read off the box.

</deferred>

---

*Phase: 04-fan-model-v2*
*Context gathered: 2026-11-10*
*Amended: 2026-11-10 (plan-phase revision: D-03, D-04, D-08, D-10; user decisions)*
*Amended: 2026-11-11 (plan-phase revision iteration 2: D-11, D-13, D-16; user decisions)*

# Feature Research: v0.2.0 "Real Fans"

**Domain:** Fan plant modelling, fault injection and fan characterisation for a local SiL bench (ESPHome `host:` firmware, 4-pin PWM 120 mm case fans on a Rev 3.3 board)
**Researched:** 2026-11-10
**Scope:** ONLY the new v0.2.0 features (better fan model, fault injection, `sil fan-analyze`). The v0.1.0 feature set is in `.planning/milestones/v0.1.0-research/FEATURES.md`.
**Overall confidence:** MEDIUM. The Intel 4-wire spec (partial page read), the Arctic product page and Noctua spec figures (via search extracts, the Noctua site returned 429 and a security checkpoint on direct fetch) are solid for datasheet-level claims. Dynamic behaviour (step response shape, kick-start, retry cycles, tach faults) rests on review data plus engineering reasoning and is tagged LOW where it is not sourced. Everything marked **[measure]** is meant to be settled by the fan-analyze runs, not by more desk research.

Confidence tags: **[H]** primary source read, **[M]** search extract of a primary or reputable review, **[L]** engineering reasoning or training knowledge, not verified.

---

## 1. Physical Reference: What Real 4-Pin PWM Fans Do

### 1.1 Intel 4-Wire PWM Fan Spec (rev 1.2/1.3), the interface contract

| Fact | Value | Confidence | Consequence for the sim |
|------|-------|------------|-------------------------|
| Supply | 12 V +/- 1.2 V | H (spec 2.1) | Supply voltage is a constant, not a model input |
| Start-up current | Peak <= 2.0 A at 13.2 V; spike > 1.0 A allowed for <= 1.0 s | H (spec 2.2) | A start kick lasts on the order of up to 1 s; start delay/kick durations beyond ~1 s are the fan's own logic, not the spec |
| Tach | 2 pulses per revolution, open-collector/open-drain, pulled up on the board (12 V max 13.2 V in the spec; Rev 3.3 uses ESP32 `INPUT_PULLUP`, Noctua says the fan pulls up internally) | H (spec 2.3), M (Noctua) | Default `ppr = 2`. Rev 3.3 firmware hard-codes `multiply: 0.5` on `pulse_counter` (pulses/min x 0.5 = RPM), so a fan with another ppr reads wrong by a factor ppr/2 |
| PWM input | 25 kHz target, 21-28 kHz allowed, VIL 0.8 V, 5 V open-circuit max | H (spec 2.4) | Rev 3.3 `ledc` 25 kHz is in spec. Duty resolution is not a limit (80 MHz/25 kHz = 3200 steps) |
| Minimum speed / behaviour at low duty | Set by the fan design, the controller cannot override. Operation below 20 % duty is "not officially supported" by the spec (Noctua's reading); many fans keep spinning at 0 %, many (Arctic PST, Noctua with zero-RPM IC) stop | M (Noctua white paper via search) | The model must support both "holds a floor" and "stops" as first-class, and a floor that is not tied to a datasheet "min rpm at 20 %" point |
| No PWM signal | Internal pull-up on the PWM pin makes the fan run at full speed | L (training knowledge, not on the page read) | Candidate fault: "PWM line open -> fan at 100 %". Verify before shipping it as a named fault |

### 1.2 Datasheet values for the two library fans

| Parameter | Arctic P12 PWM PST | Noctua NF-A12x25 PWM | Confidence |
|-----------|--------------------|----------------------|------------|
| Speed range | 200-1800 rpm | max 2000 rpm (+/-10 %); 450 rpm (+/-20 %) at 20 % PWM (some sources 460) | H / M |
| Behaviour at low duty | "0 rpm below 5 % PWM" (zero-RPM mode) | "0 % PWM -> stops" per spec; datasheet min figure is the 20 % point, not a floor | H / M |
| Max airflow | 56.3 CFM = 95.7 m3/h | 102.1 m3/h = 60.09 CFM | H / M |
| Max static pressure | 2.2 mmH2O | 2.34 mmH2O | H / M |
| Current / power | 0.1 A at 12 V (about 1.2 W, derived, page lists current only) | 1.68 W max (0.14 A max); sibling variant lists typical 1.2 W / 0.1 A | M / M |
| Starting voltage | 3.9 V | not found | H / - |
| Tach ppr | not listed (assume 2, Intel) | 2 (Noctua PWM white paper) | L / M |
| Independent measurements | TechPowerUp: ~215 rpm at 5 %, ~1829 rpm at 100 % (one sample). Igor's Lab (Rev 4 unit): "start-up 220 rpm, shut-down 53 rpm" (meaning ambiguous, treat as LOW) | TechPowerUp: ~1941 rpm at 100 %, ~226 rpm at 14 % then shut-off, restart at ~18 % duty, "fairly linear" curve, little hysteresis (one sample) | M / M |
| Sample tolerance | not stated | +/-10 % max rpm, +/-20 % min rpm | M |

**Important finding that changes the model and the library.** `fans/noctua-nf-a12x25-pwm.toml` assumes `below_min = "hold"` at `min_rpm 450` with `min_duty 0.20` (assumption A2). The independent measurement says the opposite: RPM keeps falling linearly below 20 % (226 rpm at 14 %), the fan then shuts off, and it restarts at about 18 %. So the 450 rpm "minimum" is a datasheet point on the curve, not a clamp, and this fan stops. This only works in the sim if the model has a real duty-to-RPM curve plus separate start and stop thresholds. The same review calls the curve fairly linear, so a piecewise-linear table with a handful of points is enough. **[measure]** will settle it on the actual sample.

### 1.3 Dynamics of a real fan (what the model has to reproduce)

| Phenomenon | What is known | Confidence |
|------------|---------------|------------|
| Start threshold vs hold threshold | Fans start at a higher duty than the duty at which they stop (hysteresis). Noctua sample: stops ~14 %, restarts ~18 %. HWCooling: start-up speeds are typically above minimum running speeds | M |
| Start-up | Controller applies a kick (spec allows > 1 A for up to 1 s). A stopped fan below the start threshold twitches or does nothing; a stalled-rotor fan typically retries periodically | M (spec current) / L (retry period) |
| Spin-up shape | Torque-limited: close to a ramp at first (constant acceleration), then settling toward target. A first-order lag is a reasonable envelope but misses the initial dead time and ramp. Aerodynamic drag grows with rpm squared | L (physics reasoning) |
| Coast-down | No drive: drag-dominated, decelerates like `d(rpm)/dt = -k rpm^2 - c`, so it looks hyperbolic with a linear tail to zero rather than a clean exponential. Throttling from a higher duty to a lower one (still driven) is faster than a full coast | L (physics reasoning) |
| Dead time | PWM-input ICs measure duty over a window; at 25 kHz the window is sub-millisecond, so dead time between a duty change and the speed starting to move is likely tens of ms or less. Some fans add their own filtering | L **[measure]**; invisible to a 1 s tach sample |
| Tach seen by the ESP32 | `pulse_counter` counts edges in `update_interval` (1 s). At 2 ppr one pulse in 1 s is 30 rpm, so readings are quantised to **30 rpm steps**. At 200 rpm that is +/-15 % per sample. Phase dithers across windows so long averages converge, single samples do not | H (arithmetic from the firmware YAML) |
| Tach when stalled | Line stays flat high or flat low depending on rotor position, no edges | M (Noctua microcontroller guide via search) |
| Warm-up drift | Speed at a fixed duty drifts as the motor warms; reviews record after a warm-up | L (HWCooling page title, content not read) |

### 1.4 Fan laws

| Law | Form | Where it holds | Confidence |
|-----|------|----------------|------------|
| Flow | Q ~ n | Fixed system, constant air density | H |
| Pressure | P ~ n^2 | Same | H |
| Shaft power | W ~ n^3 | Shaft power only. Electrical input deviates below ~30-40 % speed (bearing drag, windage, motor losses dominate), so the cube law **underestimates** electrical power at low speed | M |
| Caveats | Free-air curve only: real operating point depends on system impedance (heatsink, filters). Axial fans stall at high static pressure / low flow | M |

Implication: `airflow = Qmax * (n/nmax)`, `pressure = Pmax * (n/nmax)^2`, `power = P_floor + (Pmax - P_floor) * (n/nmax)^3` with an optional exponent and idle-power term so the low-end deviation can be fitted later. v0.2.0 has no electrical measurement (Rev 3.3 has no current sense), so power is datasheet-derived and must be labelled as such in status and UI.

### 1.5 Failure modes and how they look on the tach

| Failure | Physical cause | What the tach does | What PWM does | Sim handle |
|---------|----------------|--------------------|---------------|------------|
| Blocked / locked rotor | Object in blades, seized bearing | No edges, line flat. Fan may retry every few seconds (L) | Ignored | `stuck`: true rpm 0 regardless of duty, optional retry twitches |
| Worn bearing / degraded | Lubricant loss: hard starts, lower speed, noise, eventually stops | Lower rpm for a given duty, higher start threshold, longer spin-up, more jitter | Works but less effective | `degraded` severity: scales max rpm, raises start_duty, lengthens taus, raises noise |
| Late / no start | Weak start, dry bearing, marginal driver | Fan does not move until duty is held well above the normal threshold or after several seconds; or twitches and stalls | Ignored for a while | `late_start` (delay s) and `no_start` (needs duty >= threshold + margin or never) |
| Dead tach | Broken tach wire, bad Hall output, open collector stuck | Zero edges while the fan actually spins | Fan works | Reported rpm 0 (firmware sees stall) while plant rpm is non-zero. Flat low and flat high look the same to a counter |
| Wrong ppr | Different fan type (1 or 4 ppr), or board assumes 2 | rpm reads x(ppr/2) | Fan works | Report factor `ppr_actual/2` on the value the firmware sees |
| Intermittent tach | Loose connector, marginal signal | Dropouts: bursts of zero or low counts in a window, otherwise correct | Fan works | Random or periodic dropouts with seeded RNG |
| Unplug / replug | Cable out | Tach flat (pulled up by the board), no rotation, coasts down from last rpm if unplugged while spinning; on replug restarts through the start logic | n/a | Port enters "no fan" state and back; ties into per-port fan swap |
| PWM line open | Broken PWM wire | Fan runs at full speed | n/a | Candidate (L, see 1.1); not in the requested list, add only if cheap |

Sources for failure symptoms are general repair and vendor material (bearing wear, restart attempts, tach vs fan mismatch) and are MEDIUM-LOW. The sim does not need to be physically right about why, only plausible on the tach and consistent with what `stall_guard` (v0.3) will have to recognise.

### 1.6 What a fan characterisation measures

| Quantity | Procedure | Pitfall that limits it |
|----------|-----------|------------------------|
| Steady-state curve up | Duty 0 to 100 % in steps, dwell until settled (>= 5 tau, or stop on |d rpm/dt| below threshold), average rpm over the second half of the dwell | Quantisation 30 rpm at 1 s window, so average many windows |
| Steady-state curve down | Same, 100 % to 0 % | Up and down differ near the thresholds (hysteresis) |
| Start threshold | From confirmed standstill (wait until rpm 0 for several seconds), apply duty d, hold N s, spin or not; bisect d to 1 % | Retry behaviour of the fan and bearing warm/cold state change the result; repeat |
| Stop (hold) threshold | Slow ramp down from a spinning state, find duty at which rpm collapses to 0 | Ramp rate matters; coast-down tail makes the "stop moment" ambiguous |
| Step response | Steps at several amplitudes (low-to-mid, mid-to-high, up and down), 10 Hz or better sampling, fit first order with optional dead time. Report tau_up, tau_down, delay, residual | 1 s tach sampling cannot resolve a 0.5-1 s tau; needs a faster tach path (see fan-analyze) |
| Start/stop test | 0 to X: time to first pulse, time to 90 % of target, overshoot from kick. 100 % to 0: time to below 5 % of max | Same sampling issue |
| ppr detection | tach frequency at 100 % / 60 * datasheet max_rpm ratio rounds to 1, 2 or 4. **ppr cannot be derived from the tach alone**, only checked against a known rated speed (+/-10 % sample tolerance still separates 2x steps) | Needs a datasheet rpm as reference |
| Noise | Steady state at 3-5 duty levels, 30-60 s each; sigma and peak-to-peak of rpm after subtracting the known quantisation floor | Quantisation noise and real jitter are mixed in 1 s windows |
| Repeatability | Repeat one sweep point and one step test; run-to-run spread sets the lower bound of any replay tolerance | Skipped by most quick characterisations, mandatory for a "stated tolerance" |

---

## 2. Feature Landscape

Complexity is for this codebase: Go simulator (`internal/model`, `internal/fanlib`, `internal/control`, `internal/events`, web UI), C++ `sim_*` components, ESPHome YAML, Claude Code as implementer.

### 2.1 Table Stakes (v0.2.0 is not done without these)

#### Better fan model

| Feature | Why expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Duty-to-RPM steady-state curve as a piecewise-linear table, separate up and down curves (hysteresis) | The Noctua finding in 1.2 makes the linear-from-min_duty model wrong for a library fan; the fitter output is a curve | MEDIUM | New `[[curve.up]]`/`[[curve.down]]` (duty, rpm) arrays, optional. Keep the v0.1.0 scalar keys as the simple form that expands to a 2-point curve so the two existing files keep loading. Strict loader (`DisallowUnknownFields`, located errors) must stay strict; monotonic and range checks on the tables |
| Distinct start threshold and stop (hold) threshold; fans that never stop at duty 0 via `rpm_at_zero` or equivalent | Hysteresis is the core real behaviour; today `min_duty` serves as both and `below_min` is a two-value switch | LOW-MEDIUM | `start_duty`, `stop_duty`, and a floor rpm for "does not stop" fans. Replaces `below_min` as the concept; migrate the two library files |
| Start delay and kick-start | Real fans take time from enough duty to first rotation; `stall_guard` (v0.3) will be tuned against it | MEDIUM | `start_delay_s`, optional kick (duty-independent boost for `kick_s`, <= ~1 s per Intel spec). Model state machine: stopped, starting, running, coasting. `Step` stays pure: new state fields, no clock |
| Coast-down distinct from throttle-down | A zero-RPM fan stops by drag, a throttled fan is still driven | LOW-MEDIUM | `tau_coast` (or a drag-law decay `k`, `c`) used only when the fan is stopped/undriven; `tau_down` stays for driven deceleration |
| RPM jitter (noise) with seeded RNG | Control loops (`rpm_pi_control`) must be tuned against noisy rpm, not clean rpm | LOW-MEDIUM | `noise_sigma` (abs rpm or % of rpm). RNG state is an explicit part of the model state so `Step` stays a pure function of (params, state, duty, dt). Seed from the existing seed plumbing (verify `--seed` exists; v0.1.0 research recommended it) |
| Tach quirks: ppr, quantisation | Fan firmware reads a pulse counter, the sim hands it a float, so quantisation and ppr errors are invisible without them | MEDIUM | `ppr` in fan.toml. Quantisation must emulate pulse counting over the firmware window (1 s, 30 rpm at 2 ppr), which lives where the window is: the C++ RPM `PollingComponent` (sim-bus message may need an extra field, golden vectors update). Doing it Go-side with an assumed 1 s window is cheaper but wrong if `update_interval` changes |
| Airflow, static pressure and power from datasheet via fan laws | Listed in the milestone; v0.4 thermals will consume airflow | LOW | New keys `airflow_m3h_max`, `pressure_mmh2o_max`, `power_w_max` (+ optional `power_w_idle`, exponents). Computed in the status snapshot, shown in UI/`sil status`, clearly flagged "datasheet-derived, not measured" |
| Fan.toml provenance retained | Existing files document ASSUMED values per key; the whole point of v0.2.0 is to replace them | LOW | Keep `source`; add `measured = true`, measurement date, sample id, board and firmware version, so ASSUMED vs measured is machine-visible and the loader can warn on ASSUMED |

#### Fault injection

| Feature | Why expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Per-port fault overlay on the fan model: stuck/blocked, late start, no start, degraded (severity), unplug/replug | Core of the milestone | MEDIUM | Faults are an overlay (`FaultSet` per port) applied inside/around `Step`, not edits of the loaded fan params, so clear restores exactly the previous behaviour |
| Tach faults: dead tach, wrong ppr, intermittent | Core of the milestone; they exercise firmware stall detection while the fan is actually fine | MEDIUM | Applied to the value the firmware sees, plant rpm stays true (status shows both `rpm` and `tach_rpm`). Dropouts use the seeded RNG |
| Runtime control from CLI/JSON API: `sil fault <port> <kind> [--param ...]`, `sil fault <port> clear`, list in `sil status` | CLI/JSON is first citizen (project rule); web UI is only a client | LOW-MEDIUM | New control operation plus one status field (`ports[n].faults`). Bump the status schema. Exit codes and JSON errors per existing convention (unknown kind, bad port, bad severity) |
| Fault events in `sil watch`/events and logs | "visible in status/watch" is in the milestone; a test must be able to wait for or assert an inject/clear | LOW | `fault` events with sim timestamp, same event stream the UI uses |
| Web UI fault controls and indicator per port | Milestone says "web UI" | MEDIUM | Alpine + existing WS state. Selector plus severity slider plus clear; warning badge on the port card; same API as the CLI (page has no logic of its own) |
| Fault clear restores normal behaviour including replug from rest | Replug must go through the start logic, otherwise unplug/replug is not a test of restart | LOW | Falls out of the state machine if unplug zeroes the plant and replug leaves it stopped |
| Behaviour on fan swap and sim restart defined | Per-port swap exists already; faults must not leak silently | LOW | Decision for requirements: clear faults on swap, drop all faults on sim restart (they are runtime state, no persistence). Document |

#### fan-analyze

| Feature | Why expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Open-loop measurement firmware for Rev 3.3 (own YAML, no product modules): per-port duty control with fine resolution, tach read-out, ESP32-S2 `ledc` 25 kHz, `pulse_counter` as baseline | Measurement needs duty set to the percent or finer and no other logic fighting it | MEDIUM | Duty as `number` entities rather than the 100-step `fan` entity. Same repo, separate example YAML. Must also build for `host:` with `sim_*` (see differentiator "self-test against the sim") |
| Fast tach path for step/start/stop tests | Stock `pulse_counter` at 1 s and 30 rpm resolution cannot fit a 0.5-1 s tau or a start delay | HIGH | Custom ESPHome component timestamping edges (ESP-IDF GPIO ISR or PCNT plus esp_timer), publishing period-based rpm at 10 Hz or batched (t_us, rpm) records, because native API state messages carry no device timestamp and Wi-Fi adds tens of ms jitter. Biggest firmware-side risk of the milestone. Fallback: steady-state sweeps from stock `pulse_counter` only, steps with a coarse tau estimate (state the limit in the report) |
| Go native API client in `sil fan-analyze` | The host CLI drives sweeps over the native API | HIGH | There is no first-party Go client. Candidates: `mycontroller-org/esphome_api` (v1.3.0, last push 2024-09, depends on protobuf, Noise encryption support not checked, M), or implement the small subset (hello, connect, list entities, subscribe states, number/switch commands) against `api.proto` directly. Plaintext API only is fine for a bench. Pins the stack decision, hand to STACK research |
| Test procedures: duty sweep up/down with settle detection, step response, start/stop test, start threshold search, steady-state noise, ppr sanity check, repeatability pass | The procedures in 1.6; without them nothing can be fitted | MEDIUM-HIGH | Procedure engine is a sequence runner with settle criteria and a per-step timeout. Results stored raw (JSON/CSV) so fits and replay can be re-run offline |
| Fit to `fan.toml`: curves (up, down), start/stop thresholds, tau_up/tau_down/tau_coast, start delay, noise sigma, ppr, datasheet fields entered by the user (airflow, pressure, power) | The product of the whole companion | MEDIUM | First-order + optional delay least squares on step data; curves taken directly from settled means (smooth/monotonise, drop points below quantisation noise). Emits the same strict schema the loader reads and re-loads its own output as a check |
| `report.html`: self-contained, offline, no CDN | Locality constraint; one file the maintainer can open and attach | MEDIUM | Inline SVG or inlined vendored uPlot. Content: setup (board, firmware, fan, date), raw traces, up/down curves with datasheet points overlaid, step fits with residuals, thresholds, noise, repeatability, pass/fail vs tolerance, the fan.toml diff |
| Sim replay of the measured sequence and a stated tolerance | "Done when" criterion of the milestone | MEDIUM | Deterministic model-only replay: feed the recorded duty-vs-time sequence through `model.Step` with the injected clock and compare to the recorded rpm (no firmware needed, runs in milliseconds). Also an end-to-end replay through `sil up` as an optional second check. See tolerance proposal in section 3 |
| Measured Arctic P12 and Noctua NF-A12x25 library entries replacing ASSUMED values | Closes assumptions A1/A2 | LOW (given tooling), but needs the real board and the two fans | Human-in-the-loop step, cannot be done by the agent alone; plan the hardware window as a checkpoint |

### 2.2 Differentiators (valuable, not needed for the milestone to be "done")

| Feature | Value proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Self-test of fan-analyze against the sim ("SiL on SiL") | Run the identical measurement firmware built for `host:` against `sil up` with a known-truth fan.toml and check the fitter recovers the parameters. De-risks the fitter, the Go API client and the procedures before any hardware time, and is a permanent regression test | MEDIUM | Needs the measurement YAML to build with `sim_*` components. Also makes the measurement chain (quantisation, faster tach) testable. Recommended as the first fan-analyze phase |
| Fault parameters beyond on/off (severity ramp, `degraded` as a time-varying wear curve) | Tests that `stall_guard` reacts to gradual degradation, not just step failures | MEDIUM | Severity-only is table stakes; a ramp over sim time is cheap with the injectable clock but a scheduler belongs to v0.4 stimulus. Defer unless trivial |
| Second-order or ramp-then-settle spin-up shape | Closer match to real spin-up (torque-limited ramp) than first order, better for start-up/stall-guard tests | MEDIUM | Only add if the replay of the measured data exceeds tolerance with first order + delay. Let measured residuals decide |
| `sil fan-analyze compare` of two measurement JSONs (baseline vs re-measure for wear) | Initial-idea P4 wanted wear tracking; raw measurements are saved anyway | LOW-MEDIUM | Diff of fitted parameters with thresholds. Cheap given raw data format; not needed to close the milestone |
| Low-speed power deviation term in fan laws (`power_w_idle`, exponent fit) | Matches 1.4: cube law underestimates electrical power at low speed | LOW | Add keys now, default off. Value arrives when a power measurement exists (not in v0.2.0) |
| Multiple fans characterised in one session (ports 1-4 in parallel) | Throughput with 2 fans to measure and 4 ports available | MEDIUM | Shared 12 V supply sag and cross-talk are untested; start single-port, parallel only after the single-port repeatability is proven |
| PWM-line-open fault (fan runs at 100 %) | Real, simple, nasty failure for a controller whose firmware thinks it set 30 % | LOW | Pending verification of the Intel behaviour (1.1) |
| Warm-up pre-conditioning step in the procedure | Removes drift between first and last sweep points | LOW | A 60 s at mid duty step before measuring; make it a procedure option and log it |

### 2.3 Anti-Features

| Feature | Why requested | Why problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Pulse-level tach simulation (edge trains, glitches) | "Tach quirks" sounds like waveform work | Contradicts the existing decision (tach as RPM value; drivers are HIL's job); needs a timing path the host platform cannot guarantee; the firmware only sees counts per window anyway | Model tach faults and quantisation at the value level and emulate pulse counting over the firmware's window |
| Physics-based BLDC model (torque, back-EMF, inertia, commutation) | "Real fans" | Parameters unobservable from a tach-only measurement; fit would be unconstrained; no consumer needs it | Empirical curves + first-order/delay + optional ramp, fitted from the tach |
| System impedance / operating-point solver (fan curve vs heatsink curve) | Airflow numbers imply it | Needs P-Q curves and system resistance, neither is in datasheet scalars or measurable on Rev 3.3 | Fan laws on scalar maxima; real thermal coupling is v0.4 |
| Measured airflow or power draw in fan-analyze | Datasheet fans sound improvable | Rev 3.3 has no current or airflow sensing (PROJECT out of scope) | Datasheet fields entered by the user, labelled "datasheet" in the report |
| Fault scheduling, fault profile files, Lua fault scripts | Natural next step | Explicit out-of-scope for v0.2.0; belongs to v0.4 stimulus | Runtime-switched faults only, via CLI/JSON/UI |
| Persisting faults across sim restart | Convenience | Hidden state across runs makes tests unrepeatable | Faults are runtime state; tests set them explicitly |
| Closing the loop in the measurement firmware (use `rpm_pi_control` while measuring) | Reuse product modules | Controller dynamics contaminate the plant measurement | Strictly open-loop measurement firmware |
| Python (scipy/numpy) in fan-analyze | Fitting is easier there | Constraint: no Python in the simulator toolchain; fan-analyze is a Go CLI | Small hand-written least squares in Go (first order, linear regression on log residual), no heavy numerics needed |
| Production-tolerance randomisation (Monte Carlo fan-to-fan spread) | Datasheets quote +/-10 % / +/-20 % | Not asked for; the milestone matches one measured sample per fan | Document that a fit describes one sample; parameter jitter is a possible later `--fan-spread` |
| Temperature-, back-pressure-, supply-voltage-dependent fan behaviour | Real fans do vary | Needs a measurement rig this milestone does not have | State the free-air, constant-12 V, room-temperature validity in the report and `fan.toml` provenance |
| Auto-fitting without a repeatability pass | Faster | A "stated tolerance" has no basis without run-to-run spread | Make the repeatability step part of the standard procedure |

---

## 3. Fit Outputs and Reasonable Tolerances

These are proposals for the requirements stage (LOW-MEDIUM): derived from the datasheet tolerances and from the measurement chain limits above, not from a published standard.

| Output | What to report | Proposed replay tolerance (sim vs the same measured fan) |
|--------|----------------|----------------------------------------------------------|
| Steady-state rpm at each duty step (up and down) | mean, std, n windows | max(2 % of max_rpm, 2 x run-to-run repeatability); never tighter than one quantisation step (30 rpm) for single-window comparisons |
| Start threshold, stop threshold | duty +/- search resolution | within the search step (1-2 duty-percent points) |
| Start delay | seconds | within max(0.5 s, 25 %) given a 10 Hz sampling path; with 1 s tach only report "< 1 s" |
| tau_up, tau_down, tau_coast | seconds, fit residual | within 25 % and trace NRMSE <= 5 % of rpm span excluding noise |
| Noise sigma | rpm after subtracting quantisation floor | within 30 % (statistical, n-dependent) |
| Overall replay | NRMSE of rpm trace, time to 90 % of each step, max absolute error | pass/fail line in `report.html`, thresholds stored in the report so the claim is reproducible |

Rule: set the tolerance after the repeatability pass (measure the same sequence twice on the same fan). A tolerance tighter than the fan's own run-to-run spread cannot be met by any model and is a false claim. Datasheet sample tolerance (+/-10 % max rpm, +/-20 % min rpm for Noctua) is why the library entries describe one sample, not "the" fan.

---

## 4. Feature Dependencies

```
fan.toml schema v2 (curves, thresholds, delay, ppr, datasheet fields)
    |--requires--> strict fanlib loader extension + migration of 2 library files
    |--requires--> model state machine (stopped / starting / running / coasting), still pure Step(dt)
    |        |--requires--> seeded RNG in model state (noise, intermittent dropout)
    |--enables---> airflow / pressure / power in status + UI (fan laws, pure functions of rpm)

Tach layer (ppr, quantisation, dead, intermittent)
    |--requires--> decision where pulse counting is emulated (C++ RPM PollingComponent vs Go)
    |--requires--> sim-bus field for ppr/tach state if C++ side (golden vectors in Go + C++)

Fault overlay (stuck, late/no start, degraded, unplug/replug)
    |--requires--> model state machine
    |--requires--> "no fan" port state (related to existing per-port fan swap)
    |--requires--> control API op + status schema bump + events
        |--enables--> web UI fault controls (client of the same API)
Tach faults ---requires--> tach layer

fan-analyze
    |--requires--> measurement firmware (open-loop, fine duty) 
    |        |--requires (for step tests)--> fast tach component (HIGH risk)
    |--requires--> Go native API client
    |--requires--> fan.toml schema v2 (fitter output = loader input)
    |--enables---> sim replay (model-only, deterministic) --requires--> injectable clock (exists)
    |--enables---> measured Arctic / Noctua entries --requires--> real Rev 3.3 board + fans (human checkpoint)
fan-analyze self-test vs sim ---requires--> measurement YAML buildable for host: with sim_* + tach layer
```

### Dependency notes

- **Schema v2 comes first.** The fitter emits what the loader reads, and the model state machine is what the schema parameters drive. Settle both before building the fitter.
- **Faults depend on the new state machine,** not the other way round. Build late-start/no-start/degraded on top of start_delay and thresholds rather than as special cases inside `Step`.
- **Tach layer decision affects the sim-bus.** If quantisation is emulated in the C++ RPM sensor, the protocol (internal, versioned `proto`) and golden vectors change; if in Go, the 1 s window is an assumption baked into the simulator. Needs an architecture decision before the tach phase.
- **fan-analyze can be built and tested without hardware** up to the fitter and replay if it targets the sim first. Only the final two library measurements need the board.
- **Existing assets reused:** pure `model.Step` with injected `dt`; strict `fanlib` loader (extend, do not loosen); `clock.Manual` for deterministic tests (fault timing, start delay, replay); control API, `status` schema and events; per-port fan swap; web UI with Alpine and uPlot; `SIL_FAN_DIR` override (lets fan-analyze write a fit and test it without rebuilding `sil`).

---

## 5. MVP Definition

### Launch With (v0.2.0 done)

- [ ] fan.toml schema v2 with curves (up/down), start and stop thresholds, `rpm_at_zero`/floor, start delay, kick, `tau_coast`, noise, ppr, datasheet fields, and the two library files migrated
- [ ] Model state machine reproducing start/stop dynamics, seeded noise, still pure and unit-tested with `clock.Manual`
- [ ] Airflow, pressure, power via fan laws in status and UI
- [ ] Tach layer: ppr factor, 30 rpm quantisation over the firmware window, dead and intermittent tach
- [ ] Fault overlay with the seven requested faults, CLI/JSON, events, status, web UI
- [ ] Measurement firmware + Go native API client + sweep/step/start-stop procedures + fit + `report.html` (with fast tach component OR a documented coarse fallback)
- [ ] Repeatability pass, replay with stated tolerance
- [ ] Self-test of fan-analyze against the sim (recommended, it is the de-risking step)
- [ ] Arctic P12 and Noctua NF-A12x25 measured, fitted, committed, A1/A2 closed

### Add After Validation (v0.2.x)

- [ ] `fan-analyze compare` (wear baseline) once a second measurement of the same fan exists
- [ ] Second-order / ramp spin-up shape if first order + delay misses the tolerance
- [ ] Parallel multi-port measurement after single-port repeatability is proven
- [ ] PWM-open fault after the Intel behaviour is verified

### Future Consideration

- [ ] Fault scheduling, profile files, Lua (v0.4 stimulus)
- [ ] Production-tolerance spread, temperature/back-pressure dependence
- [ ] Measured power/airflow (needs sensors, not on Rev 3.3)

---

## 6. Feature Prioritization Matrix

| Feature | User value | Implementation cost | Priority |
|---------|-----------|---------------------|----------|
| Schema v2 + model state machine + curves/thresholds | HIGH | MEDIUM | P1 |
| Fan laws: airflow/pressure/power | MEDIUM | LOW | P1 |
| Noise (seeded) | HIGH | LOW-MEDIUM | P1 |
| Tach layer (ppr, quantisation) | HIGH | MEDIUM | P1 |
| Fault overlay + CLI/JSON + events | HIGH | MEDIUM | P1 |
| Tach faults (dead, wrong ppr, intermittent) | HIGH | MEDIUM | P1 |
| Web UI fault controls | MEDIUM | MEDIUM | P1 (stated in milestone) |
| Measurement firmware (stock tach, fine duty) | HIGH | MEDIUM | P1 |
| Go native API client | HIGH | HIGH | P1 |
| Sweep, start-threshold, noise, repeatability procedures | HIGH | MEDIUM | P1 |
| Fit + `report.html` + replay with tolerance | HIGH | MEDIUM | P1 |
| Fast timestamped tach component | HIGH (step/start fits) | HIGH | P1 with documented coarse fallback |
| Self-test against the sim | HIGH | MEDIUM | P1 (recommended) |
| Measure Arctic and Noctua | HIGH | LOW tool, hardware-gated | P1 |
| Second-order spin-up shape | LOW-MEDIUM | MEDIUM | P2 (data-driven) |
| `compare` for wear baselines | LOW-MEDIUM | LOW-MEDIUM | P2 |
| Parallel multi-port analysis | LOW | MEDIUM | P3 |
| Severity ramps, scheduled faults | LOW now | MEDIUM | P3 |

---

## 7. Comparable Tools

| Feature | Real fan characterisation (reviewers: TechPowerUp, HWCooling) | Fan-control software (fan curve + RPM scan) | Our approach |
|---------|------------------------------------------------------------------|----------------------------------------------|--------------|
| Duty sweep with RPM per step | Fixed steps with a settle/dwell, one sample, up direction mostly | Quick scan to find min/max, no dwell science | Up and down sweep, settle detection, repeated runs |
| Start/stop hysteresis | Sometimes reported ("restarts at 18 %") | Detected as "min start duty" | Bisection search with hold time, up/down curves |
| Dynamics | Rarely measured | Not measured | Step and start/stop tests, fitted tau and delay |
| Output | Charts in a review | Profile in a GUI | Strict `fan.toml` + self-contained `report.html` + raw data + sim replay verdict |

---

## Sources

- Intel, 4-Wire Pulse Width Modulation (PWM) Controlled Fans, Specification Rev 1.2 public copy (page 9, section 2 Electrical Specifications read in full via a copy at mikrocontroller.net/attachment/414199/4-Wire_PWM_Controlled_Fans_Specification.pdf): [H]
- Noctua PWM specifications white paper (noctua.at/pub/media/wysiwyg/Noctua_PWM_specifications_white_paper.pdf) and Noctua microcontroller guide (noctua.at/en/support/faqs/microcontroller-guide-pwm-setup-and-rpm-monitoring): content via search extracts only, direct fetch blocked (429 / security checkpoint): [M]
- Noctua NF-A12x25 PWM specification (noctua.at/en/products/fan/nf-a12x25-pwm/specification), values via search extract; sibling chromax.black.swap page for 1.2 W / 0.1 A: [M]
- Arctic P12 PWM PST product page (arctic.de, fetched; the fetched SKU was ACFAN00170A, the repo file cites ACFAN00120A, same product family, confirm which SKU is the shipped fan): [H-M]
- TechPowerUp reviews of Arctic P12 PWM PST and Noctua NF-A12x25 PWM (page fetches returned 403, figures via search extracts): [M]
- HWCooling and Igor's Lab fan reviews (search extracts, Igor's start/shut-down figures ambiguous): [L-M]
- Fan affinity laws: Wikipedia "Affinity laws", EASA trade press, HPAC (search extracts), including the low-speed deviation of electrical power from the cube law: [M]
- Fan failure symptoms: general repair/vendor articles (bearing wear, retries, tach vs fan mismatch): [L-M]
- Go ESPHome client options: `mycontroller-org/esphome_api` (v1.3.0, last push Sep 2024), `athurg/go-esphome-api` (2020), `rockavoldy/esphome_api` (2023) via pkg.go.dev search; Noise/encryption support not checked: [M]
- Repo facts read directly: `fans/*.toml`, `internal/model/fan.go`, `internal/fanlib/fanlib.go`, `/home/claude/wifi-fancontroller/hardware-rev-3.1.yaml` (ledc 25 kHz, `pulse_counter` 1 s with `multiply: 0.5`), `docs/initial-idea.md`, `.planning/PROJECT.md`: [H]

---
*Feature research for: v0.2.0 Real Fans (esphome-sil)*
*Researched: 2026-11-10*

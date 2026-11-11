# Pitfalls Research: milestone v0.2.0 "Real Fans"

**Domain:** Adding a measured, faultable fan model and a real-board measurement pipeline (`sil fan-analyze`) to the existing esphome-sil SiL bench (ESPHome 2026.10.1, Go simulator, sim-bus, Rev 3.3 / ESP32-S2)
**Researched:** 2026-11-10
**Overall confidence:** HIGH for facts read from the pinned 2026.10.1 image source and from this repo; MEDIUM for fan physics, PST behaviour, estimation theory and HIL practice (engineering judgment, to be confirmed by the owning phase); LOW where stated.

Archived v0.1.0 pitfalls (host-platform matrix, static link, MAC/prefs identity, sim-bus framing, clock discipline) are in `.planning/milestones/v0.1.0-research/PITFALLS.md` and are not repeated. They still apply. This file covers only what the v0.2.0 features add.

**Evidence tags**
- `[VERIFIED]` read in this repo, or in the pinned `ghcr.io/esphome/esphome:2026.10.1` image source, during this research.
- `[REASONED]` engineering judgment from the domain; the owning phase must confirm it.
- `[UNVERIFIED]` recalled, not checked here; treat as a hypothesis.

**Suggested phase labels** (no v0.2.0 roadmap exists yet; numbering continues at 4, the synthesizer should rename)
- **A** Fan model v2 and `fan.toml` schema evolution (curves, start/stop, noise, tach quirks, fan laws)
- **B** Fault injection (engine, CLI/JSON, web UI)
- **C** Measurement firmware for the real Rev 3.3 board
- **D** Go native-API client and `sil fan-analyze` orchestration
- **E** Fitting and `report.html`
- **F** Measurement campaign (Arctic P12, Noctua NF-A12x25) and sim replay validation

---

## What the existing system already fixes (facts the pitfalls below build on)

| Fact | Evidence | Consequence for v0.2.0 |
|------|----------|------------------------|
| Production tach is `pulse_counter` on GPIO16/17/18/21, `INPUT_PULLUP`, `multiply: 0.5`, `update_interval: 1s`; ledc 25 kHz on GPIO12-15 | `[VERIFIED]` `wifi-fancontroller/hardware-rev-3.1.yaml` lines 95-206 | Rev 3.3 already uses all four ESP32-S2 hardware PCNT units (S2 has four `[UNVERIFIED]`); the measurement instrument is the production sensor unless a different one is added. |
| `PulseCounterSensor::update()`: first call only stores `last_time_` and publishes nothing; later `value = 60000 * raw / (now - last_time_)` pulses/min with integer-ms interval; default counts rising edges only | `[VERIFIED]` image `pulse_counter_sensor.cpp:194-210`, `sensor.py` defaults | 1 pulse per window is 60 pulses/min, so 30 RPM after `multiply: 0.5` at ppr 2. First sample after boot is missing. |
| PCNT glitch filter `internal_filter` is capped at 13 us on ESP32 hardware PCNT, default 13 us | `[VERIFIED]` `sensor.py:58` | No hardware debounce beyond 13 us is available; a 25 kHz PWM edge pair is 20 us per half period, just above it. |
| Sim `sim_sensor` publishes a **time-weighted mean of the values the simulator pushed** over `update_interval`, as float | `[VERIFIED]` `components/sim_bus/sim_sensor.cpp:38-42` | The sim RPM is a continuous mean; the real RPM is a quantised count. They are different operators (Pitfall 2). |
| Engine ticks at 10 ms; sim-bus dedupes outbound state and caps rate | `[VERIFIED]` `internal/engine/engine.go:30`; v0.1.0 research | Per-tick noise defeats dedupe and floods the bus (Pitfall 8). |
| `fanlib` loads a flat nine-key TOML with `DisallowUnknownFields`, no defaults ("nothing is ever defaulted or corrected silently", D-14); loaded from the embedded library or `SIL_FAN_DIR` | `[VERIFIED]` `internal/fanlib/fanlib.go`, `fans/*.toml` | Schema growth must keep every existing file loading and must not introduce silent defaults (Pitfall 6). |
| Existing `internal/apiclient` speaks only Noise handshake + HelloRequest, then closes | `[VERIFIED]` `internal/apiclient/probe.go` | `fan-analyze` needs a real client (entity list, subscribe, commands, keepalive). That is new code, not an extension of a mature one (Pitfall 9). |
| API keepalive: firmware pings after 60 s of silence and disconnects after 150 s without reply; ExecuteService supports responses | `[VERIFIED]` `api_connection.cpp:55,73,368-389`, `api.proto:1176,1192,1205` | The client must answer pings itself. |
| First-boot product firmware sets all four fans to 100 % | `[VERIFIED]` v0.1.0 research on `init_done`; hardware-rev-3.1 `on_boot` | Existing fail-safe direction is "fans up"; the measurement firmware should keep it. |
| HIL rig: autouse `supervisor` fixture releases all contract lines before and after the session; a tach bridge QC test reads PWM frequency x 30 within +-10 %; rig credentials are published and non-secret by decision; local builds need a private `HIL_LOCAL_DUT_API_KEY` | `[VERIFIED]` `wifi-fancontroller/hil/tests/rig/conftest.py`, `test_22_qc_fans.py`, `hil-network.yaml` | Reuse the safety-fixture pattern and the bridge as an instrument check; never put a published key on a board on the developer's own Wi-Fi (Pitfall 11). |

---

## Critical Pitfalls

### Pitfall 17: The tach instrument is too coarse for the dynamics you are fitting

**What goes wrong:** With the production setting (`update_interval: 1s`, ppr 2, rising edges), one pulse is 30 RPM and a reading is a 1 s boxcar average. A spin-up with `tau_up` ~1 s yields 3 to 5 usable points, each with +-30 RPM count error; at the Arctic P12 minimum (200 RPM) that is +-15 %. Shortening `update_interval` to 100 ms makes it worse if you keep fitting RPM values: 100 ms at 200 RPM is 0.66 counts per window, i.e. readings of 0 or 300. The first sample after boot is also missing, and any step issued mid-window is smeared by up to 1 s. `[VERIFIED]` for the arithmetic and first-sample behaviour; `[REASONED]` for the consequences.
**Why it happens:** The instrument was built for "is the fan turning", not for system identification. Reading `Fan N Speed` through the API also gives host-arrival time, not measurement time.
**Consequences:** `tau_up`/`tau_down`/start delay fitted from 1 s RPM values are biased (a boxcar adds ~T/2 delay) and noisy; the fit looks fine and the sim replay then matches only because both share the same blur (see Pitfall 2).
**Prevention:**
1. Fit on **counts, not RPM**. In the measurement firmware add `total:` to each `pulse_counter` (cumulative pulses, `[VERIFIED]` in schema) and `update_interval` 100 to 250 ms. The host differences the cumulative counter and fits a forward model that **integrates** model RPM into expected counts over each window. Count quantisation then stops being an error source, and a lost or late API message loses no pulses.
2. Keep a separate, production-equivalent 1 s series (computed host-side from the totals on a 1 s grid) for the sim comparison in phase F.
3. Do not change `rising_edge`/`falling_edge` or `internal_filter` relative to production without recording it in the report; the point is to measure what the product firmware will see.
4. Discard the first sample after boot and any window with a gap in the sequence number (Pitfall 5).
5. All four ESP32-S2 PCNT units are taken by the four tachs; a "second, finer instrument" (period measurement) can only be added as a software ISR timestamp capture on one port. Prefer the cumulative-counter route first; research the ISR route only if counts at 100 ms cannot identify `tau`. `[UNVERIFIED]` PCNT unit count.
**Warning signs:** fitted `tau` clusters near the update interval; fit residuals show sawtooth at 1 s period; changing `update_interval` changes the fitted `tau`.
**Phase:** C (instrument design), D (totals and sequence numbers), E (count-domain fit). **Needs deeper phase research.**

---

### Pitfall 18: The 1 s mean over the wire hides fast dynamics, and sim and measurement are compared in different domains

**What goes wrong:** Three separate mismatches:
1. **Operator mismatch.** The sim publishes a float time-weighted mean of true RPM; the board publishes `floor`-quantised counts per window. Comparing sim RPM series to measured RPM series puts a +-30 RPM quantisation floor into every residual, and the first ~1 s of any step looks "wrong" in both directions.
2. **Window phase.** The real window grid starts at firmware boot; a step at an arbitrary time lands at an arbitrary offset inside a window. The sim's window grid starts at bus connect. Un-aligned grids produce up to 1 s apparent lead/lag in step response, which is exactly the size of the quantity being validated.
3. **Noise placed at the wrong layer.** If the Go model adds jitter to true RPM every 10 ms tick, the 1 s mean averages it down by about sqrt(100), so published noise is far smaller than intended; the noise that matters on the real board is count quantisation and window phase, which the Go side cannot produce because it never sees pulses. `[VERIFIED]` for the window location (C++ `sim_sensor.cpp`); `[REASONED]` for the consequences.
**Prevention:**
- Define the comparison domain once: **production-equivalent 1 s counts**. Replay applies the recorded command times (in device milliseconds) to the sim and compares window-by-window; align the sim's window start to the measurement's (offset recorded in the report) or compare after marginalising the phase.
- Put quantisation and tach quirks (ppr, dropouts, quantisation to `60/(ppr*window)`) where the window lives, i.e. in the C++ tach sensor, driven by values Go sends (`ppr`, `window` from the YAML `update_interval`), not in the Go plant. The Go plant produces a physical RPM; the sim sensor turns it into what a pulse counter would report. This also keeps the bus carrying RPM, honouring the existing "no pulse simulation" decision.
- Keep an unquantised "true RPM" in `sil status`/history for debugging so the hidden physics stays visible to the developer even though the firmware cannot see it.
- State the tolerance as "within X RPM or Y % of the measured window, excluding the first window after a step", decided **before** looking at the data (Pitfall 15).
**Warning signs:** sim RPM has no quantisation steps; replay residual is a sawtooth; shifting the sim start by 500 ms changes the pass/fail.
**Phase:** A (where quantisation lives), F (alignment and tolerance).

---

### Pitfall 19: Ill-conditioned, overfit or unidentifiable fits written into `fan.toml` as facts

**What goes wrong:**
- `tau`, start delay and `start_duty` are strongly correlated when sampled at 1 s; a least-squares optimiser returns a number anyway, often at a bound.
- A polynomial or free-form fit of the duty-to-RPM curve through ~10 points oscillates between points (Runge), breaks monotonicity, and can produce RPM below `min_rpm` or above `max_rpm`.
- Up and down sweeps differ (hysteresis, and `start_duty` vs hold threshold). Fitting one curve to both averages away the effect the milestone exists to model.
- Parameters that were **not observable** get fitted anyway. The Noctua NF-A12x25 is documented to keep spinning at low PWM (the current `fans/noctua-*.toml` says "hold", marked ASSUMED), so `start_duty`, start delay and coast-to-zero cannot be measured by PWM alone, and Rev 3.3 has no per-fan power switch to force a start from rest. `[REASONED]`; the repo file confirms the assumption, not the fan behaviour.
- Fixed settling times (e.g. 5 s) underestimate steady state at low RPM where `tau_down` is large, so the "steady" curve is a transient.
**Prevention:**
- Use a **monotone, low-parameter curve** (monotone piecewise-linear or PCHIP on measured knots; knot count and spacing fixed by the protocol, denser near `min_duty`/`start_duty`) and reject any fit that is non-monotone. Fit `tau` separately for up and down, and only from steps where the signal-to-quantisation ratio is high (large steps, count-domain model, Pitfall 17).
- Hold-out validation: measure extra duty points and one extra step that are **not** used in the fit and report their residuals separately.
- Report parameter uncertainty (bootstrap over windows or profile likelihood). If the interval touches a bound or exceeds a stated width, write the parameter as `unmeasured` (omit it, keep the datasheet/assumed value with provenance) instead of a precise-looking number.
- Detect settling from slope over a trailing window (for example drift below 0.5 % per second for three consecutive windows) with a maximum timeout, and record settling time per point.
- Emit a per-parameter status in the report: `measured`, `not observable`, `assumed`.
**Warning signs:** fit parameter at a bound; residuals structured, not white; refit with one point removed changes parameters by more than their stated interval; `start_duty` fitted for a fan that never stopped.
**Phase:** E, with protocol design in C/F.

---

### Pitfall 20: Safety, leaving a real fan stopped or low after an aborted run

**What goes wrong:** `fan-analyze` drives real fans through sweeps to 0 % and low duty, then the host crashes, Ctrl-C arrives, Wi-Fi drops, the laptop sleeps or the API connection is lost mid-sweep. The board keeps the last commanded duty. For the Arctic P12 that is a stopped fan (0 % below 5 %), which in a thermally loaded rack means no cooling until someone notices. Related: a low-duty start test that commands below the start threshold leaves the motor energised without rotating (not a deliberate stall, but a long dwell at a duty the fan cannot start at is wasted heat in the driver; `[REASONED]`, magnitudes `[UNVERIFIED]`). Also: a stuck probe on the wrong port, a mis-wired tach, or a different fan than assumed produces a reading that looks wrong only to a human.
**Prevention (defence in depth, none of it depends on the host staying alive):**
1. **On-device fail-safe.** The measurement firmware owns a watchdog: `api: on_client_disconnected` and a no-command timer (for example 5 to 10 s without a host "keepalive" action) set every fan to a safe duty (100 %, matching the product first-boot behaviour). Boot default is also safe (restore mode forcing 100 %). `reboot_timeout` interplay: set it deliberately, do not inherit 15 min blindly.
2. **Run the sequence on the device where possible** (Pitfall 21) and have the final step restore the safe state; the host only observes. A host abort then cancels at worst the remaining measurement, never the safe state.
3. **Host cleanup** with a deferred "restore safe" on every exit path (signal handler, panic recovery, `defer`), plus a final read-back verification that the board reports the safe duty; non-zero exit and JSON error if verification fails. This mirrors the HIL `supervisor` autouse fixture that always releases lines in a finalizer.
4. **Preflight gate before any sweep:** at a modest duty (for example 30-50 %) the selected port's tach must read within a plausibility band of the expected RPM and **all other ports** must read 0 (or their own fan); otherwise abort. This catches wrong port, wrong fan, disconnected tach, wrong ppr (RPM about 2x or 0.5x the datasheet).
5. **Hard limits in the tool, not the user:** maximum dwell below the fan's observed start duty, maximum total run time, no duty above 100 %, no "block the fan" tests (stuck/blocked is simulated only; the real fan is never mechanically obstructed by the tool).
6. Single fan under test; any unused port is either unconnected or left at the safe duty and listed in the report.
**Warning signs:** a fan found stopped after a run that exited non-zero; the board still at a test duty after `sil fan-analyze` returned.
**Phase:** C (on-device fail-safe), D (host cleanup and preflight), F (human checkpoint, Pitfall 24). **Safety-critical: must be done before the first real fan test.**

---

### Pitfall 21: Host-driven timing over Wi-Fi corrupts the dynamics

**What goes wrong:** The native API sensor state carries no device timestamp. Steps commanded and samples observed from the host are separated by Wi-Fi/TCP latency of tens to hundreds of milliseconds with occasional multi-second stalls; messages can be batched or delayed; an ESP32 loop stalled by Wi-Fi can shift `update()`. A host loop that says "set duty, then record RPM until settled" inherits all of it. `[REASONED]`; keepalive facts `[VERIFIED]`.
**Prevention:**
- The measurement firmware runs the **step schedule autonomously** (a script table or sequence driven by device `millis()`), stamps each sample with a **sequence number and device time**, and the host only starts it, collects, and reconciles gaps. Use cumulative totals so a dropped message loses nothing. Deliver samples as one structured string/sensor per window or via an API action with a response (`supports_response` exists in 2026.10.1, `[VERIFIED]`); choose in phase C research.
- Never infer step time from host arrival; use the device time at which the duty was written (sample the same device clock).
- If the host must control timing, bound and record latency (ping round trip) and reject runs whose jitter exceeds a stated limit.
- Do not run `fan-analyze` over a busy network or with another API client (Home Assistant) subscribed to the same board; the ESP32-S2 has a small connection budget (`max_connections`), and HA state traffic adds load. `[REASONED]`
**Warning signs:** fitted dead time equals mean Wi-Fi latency; repeat runs show different dead times with the same fan; gaps in the sequence.
**Phase:** C, D.

---

### Pitfall 22: Fault state leaks across sim restart, firmware restart, fan swap and unplug/replug

**What goes wrong:** Runtime faults are new mutable state in a system whose v0.1.0 restart semantics were fixed with care (engine stop releases held buttons; `Set` rollback; restart gap race F7 still open as a design question, `[VERIFIED]` PROJECT.md). Typical leaks:
- A fault set in one run survives `sil sim-restart` or reappears in the next run because it was written to the same state store as `fan.toml` selection or instance state.
- A fan swapped on a port (`fan.toml` reselect) keeps the previous fan's fault (for example "stuck" or "wrong ppr 1") and the new fan appears broken.
- Firmware restart (the sim stays up) is confused with fan repair: a stuck fan must remain stuck when the firmware reboots, because the plant did not change; but the resync on reconnect must still replay the faulted tach (dead tach reads 0 after reboot, not the pre-fault value).
- **Unplug/replug is modelled as a flag only.** Real behaviour: on unplug RPM coasts down (or is cut, depending on what you model), PWM output is unaffected, tach reads 0 pulses; on replug the fan restarts **from rest** and must pass `start_duty` again, with the kick-start/start-delay dynamics; the stuck/late-start state must reset. If the model keeps its running state across unplug, replug shows an instant return to the old RPM.
- Faults combined (stuck + degraded + dead tach) applied in an order-dependent way.
**Prevention:**
- Decide and document the lifecycle table once: faults are **runtime-only plant state**, per port, **cleared on sim restart and `sil down`, preserved across firmware restart, cleared on fan swap** (with an explicit event), never written to `fan.toml`, instance files or STATE. Put them in `status` JSON per port (`faults: [...]`) and in `history`/`watch` events with timestamps on the sim clock, so a leaked fault is visible.
- Fault state lives in one struct owned by the engine, applied as pure functions in a fixed order (plant faults, then tach faults, then connection faults), covered by table tests on `clock.Manual`.
- Unplug resets model `Running=false`, clears start-delay timers; replug re-enters via the start path. Add a test that unplug/replug at 100 % duty ends at the post-start RPM only after the start delay.
- Faults must be idempotent (set the same fault twice, clear a fault that is not set) and `clear all` must exist.
- Surface active faults prominently in the UI and CLI watch output; the existing debt "page does not surface `Status.error`" means do not rely on the page showing errors for faults. `[VERIFIED]` PROJECT.md.
- A fault that makes the firmware behave differently (dead tach) must show up in the same place the tester looks (RPM = 0 via the API), while `status` keeps the true physical RPM so the closed loop remains inspectable.
**Warning signs:** a fresh `sil up` shows a non-empty fault list; swapping the fan on a port does not change `status.faults`; replug returns to previous RPM without a start delay.
**Phase:** B (lifecycle and model), A (model start path must expose a reset hook).

---

### Pitfall 23: Nondeterministic noise breaks tests and replay

**What goes wrong:** Noise from a global `math/rand`, from a PRNG consumed once per tick, from `time.Now()` seeding, or from Go map iteration order makes tests flaky or runs unreproducible. Subtler: a PRNG **stream consumed per call** makes the noise sequence depend on how many `Step` calls occurred, which differs between `clock.Manual` tests (exact dt) and real-time runs (late ticks, behind-real-time catch-up). A change of tick period or one extra Step shifts every later noise sample, so golden tests rewrite wholesale. `[REASONED]` plus `[VERIFIED]` for the 10 ms tick and the "behind real time" flag.
**Prevention:**
- Noise is a **pure function of (seed, port, sim-time bucket)**, e.g. counter-based hashing of a bucket index (fixed bucket width such as 10 ms or the window), not a stateful stream. Same seed and same sim time give the same value regardless of call count.
- Seed explicit: `--seed` / run setting recorded in `status`; default seed deterministic per instance (not time). Tests set it; an explicit `noise = 0` setting switches noise off for model golden tests.
- Split noise generation from the deterministic model so all v0.1.0 model tests stay bit-identical with noise disabled. The wall-clock lint (v0.1.0) already forbids `time.Now`; extend it to forbid `math/rand` top-level functions in the model package (use `math/rand/v2` with a local source or a hash).
- Quantisation and dropout decisions (intermittent tach, jitter) use the same keyed hash so replays reproduce them.
- Statistical tests assert **moments over many buckets with fixed seed** (mean, std within tolerance), never single draws.
- Remember bus dedupe: continuous noise changes the value every tick; apply noise after the 1 s window so the bus rate does not rise from "on change" to every tick (4 ports x 100 msg/s). If noise must be sent per tick, raise the dedupe epsilon and keep the rate cap. `[REASONED]`
**Warning signs:** a golden test changes when tick period changes; same seed gives different series on different runs; bus message rate jumps with noise on.
**Phase:** A.

---

### Pitfall 24: `fan.toml` schema evolution breaks existing files or erodes strictness

**What goes wrong:** The v0.1.0 contract is nine required flat keys with unknown-key rejection and no silent defaults. v0.2.0 needs curves (arrays of points), up/down variants, noise and tach parameters, airflow and power datasheet values, provenance per parameter. Mistakes: add new **required** keys (every existing file, embedded and user-edited via `SIL_FAN_DIR`, stops loading); add new keys with **silent defaults** (breaks D-14 and makes "measured" indistinguishable from "defaulted"); drop `DisallowUnknownFields` to be lenient; make curve and scalar keys both authoritative (`min_rpm` and a curve disagree); or write the analyzer's output with `toml.Marshal`, which cannot emit comments or preserve the `source`/ASSUMED annotations the current files rely on. `[VERIFIED]` loader and files; `[REASONED]` consequences.
**Prevention:**
- Add an explicit `schema = 2` (absent means 1). Schema 1 files keep loading with identical behaviour (linear curve + lag); a schema 1 file is never auto-upgraded. Version-gated key sets: unknown keys for the declared schema are still rejected, with the file/line error.
- New optional groups are **absent-means-not-modelled**, not defaulted numbers: `[curve]`, `[noise]`, `[tach]`, `[datasheet]`. Absent `[noise]` means exactly zero noise by documented semantics, which is the v0.1.0 behaviour, not a hidden default. Cross-field validation: monotone curves, curve endpoints consistent with `min_rpm`/`max_rpm`, `tau > 0`, noise non-negative, ppr integer >= 1.
- Mark each parameter's provenance machine-readably (`measured`/`datasheet`/`assumed`), so a later tool can list what is still assumed (assumptions A1/A2 close here).
- Emit `fan.toml` from a **template writer with deterministic key order and fixed numeric formatting** (few significant digits, no locale), with comments and a provenance block, then round-trip it through the loader in a test (emit, load, compare). Keep volatile data (date, board id, firmware sha, ambient, run id) in `report.html` and a short header comment, not in keys, so diffs stay reviewable.
- Golden test: the two shipped v0.1.0 files, unchanged, load to the same `model.Params` as before; schema 2 versions are added as new files or in place only after the replay test passes.
- Never let the analyzer overwrite a library file silently; it writes `fan.measured.toml` (or a path argument) and a human promotes it.
**Warning signs:** a user's custom fan stops loading after upgrade; emitted file loses its comments; a defaulted value shown as measured.
**Phase:** A (schema), E (emitter).

---

### Pitfall 25: Native API client protocol drift (hand-rolled protobuf on ESPHome 2026.10.1)

**What goes wrong:** The Go client must speak Noise-framed protobuf messages for entity listing, state subscription, fan/number/button commands, pings and, if chosen, service calls with response. Risks:
- Field numbers and message ids are hand-copied from `api.proto`; one wrong tag silently yields zero values.
- ESPHome has deprecated/changed entity identity fields before (`object_id` still present in 2026.10.1 `ListEntities*Response`, `[VERIFIED]`, but do not key on it); entity `key` is a hash of the name; match entities by **name from the listing**, never a hard-coded key.
- Keepalive: the firmware pings after 60 s of silence and disconnects 150 s later without a reply (`[VERIFIED]`). A client that only reads will be dropped silently in a long run.
- API version negotiation: a firmware built with a different ESPHome than the client's `api.proto` snapshot silently changes semantics. The product repo pins 2026.10.1 but the board may carry whatever was flashed.
- Plain vs encrypted: the HIL rig board uses a published key; a private board needs its own key; the existing probe refuses plaintext with a distinct error (`KindPlaintextAPI`), good, keep it.
- Reconnect storms and state-flood replays after reconnect (the firmware re-sends all states on `SubscribeStates`), which look like new samples if the client does not de-duplicate by sequence number.
**Prevention:**
- Generate Go message code from a **vendored copy of `api.proto` for 2026.10.1** (committed, with sha256 recorded), or hand-code only the ~12 messages needed and **golden-test every one against bytes captured from the pinned image**. Fail closed on `HelloResponse` API version outside a tested range and on ESPHome version string not equal to the pin, with an explicit `--allow-version-mismatch`.
- **Contract-test the client against the host firmware this repo already runs** (`sil up` speaks the same native API from the same ESPHome version, `[VERIFIED]` v0.1.0): entity listing, state subscription, fan command, ping handling. That catches drift without hardware. Add an e2e case in `make test-contract`.
- Own the keepalive loop (client pings every 20 to 30 s, answer `PingRequest`/`DisconnectRequest`), reconnect with backoff and a hard cap, treat disconnect during a run as "firmware watchdog took over" (Pitfall 20) and mark the run incomplete.
- Evaluate off-the-shelf Go ESPHome API libraries only against these tests; dependency policy so far prefers few modules. Do not add `aioesphomeapi`/Python to the simulator side (constraint); pytest may still use it in the product repo.
**Warning signs:** entity "not found" after a firmware rebuild; samples stop after 60 to 150 s; values constantly 0; decoding errors on `ListEntities`.
**Phase:** D. **Needs deeper phase research** (library vs generated code).

---

### Pitfall 26: Measurement conditions masquerade as fan properties (thermal drift, supply, mounting, ambient, PST)

**What goes wrong:** A fitted `fan.toml` is only valid for the conditions it was measured in. Typical contamination, all `[REASONED]` with magnitudes `[UNVERIFIED]`:
- **Warm-up/thermal drift:** motor and driver heat shifts RPM at fixed duty slightly over minutes; the first sweep (cold fan) differs from the last. Sweeping low to high in one pass puts drift on top of the curve; fan bearings also have run-in behaviour on a fresh fan.
- **Supply:** four fans on one 12 V supply; supply droop and ripple at high current or when several ports change; measuring with other ports active changes the result. Rev 3.3 supply, regulation and tach pull-up are not documented here; check the schematic before trusting absolute thresholds.
- **Mounting/impedance:** free air vs in a rack vs against a filter changes max RPM and hold thresholds a little and the start behaviour more. Write the mounting into the report.
- **Ambient temperature** and air density (minor for RPM, relevant to airflow claims).
- **PST (Arctic P12 PWM PST):** PWM Sharing Technology lets a second fan ride on the first fan's connector; the first fan's tach carries the speed signal (`[UNVERIFIED]` details; confirm on the Arctic page). Measuring one fan alone vs one that has a follower attached changes load and tach meaning. The library entry is "P12 PWM PST", so say explicitly in the report which topology was measured; the tach of a chain with a mismatched follower can be wrong without any fault.
- **ppr:** 2 pulses per revolution is typical and baked into `multiply: 0.5` in the product YAML; fans exist with other values. A wrong ppr shows as max RPM at about 2x or 0.5x the datasheet.
**Prevention:**
- Protocol: fixed warm-up (for example 5 minutes at 50 %) before measuring, **randomised or interleaved duty order and both directions**, repeat one anchor point (for example 50 %) at the start, middle and end of the run and report the drift between them; abort or flag when it exceeds a limit.
- Record ambient, supply voltage if measurable, mounting, fan serial/lot, topology (single, PST follower attached or not), measurement firmware sha, board id and date in the report header.
- Treat ppr as a measured/declared parameter: the analyzer derives implied max RPM from the datasheet and warns on a factor-of-two mismatch; `fan.toml` stores the **physical** ppr and the sim applies the tach multiply separately (also the base of the "wrong ppr" fault).
- Measure run-to-run repeatability (same sequence twice, different day if possible); this is the noise floor the replay tolerance is built from (Pitfall 28).
**Warning signs:** anchor point drifts monotonically through the run; up-sweep and down-sweep disagree at the same duty beyond their combined noise; max RPM at exactly 1/2 or 2x datasheet.
**Phase:** F (protocol), C (report metadata), E (drift reporting).

---

### Pitfall 27: HIL flakiness and humans in the loop

**What goes wrong:** Real-board runs have failure sources that the SiL never had: a board on the wrong Wi-Fi, an old firmware image, a fan plugged into a different port, a fan not seated on the tach pin, a cable loose in the middle of a sweep, a laptop sleep, the shared HIL rig in use by someone else. If the pipeline is "start, wait, read the report", every such cause becomes a plausible-looking wrong number or a flaky red test. Separate trap: using the rig's **published** `hil_dut_api_key` for a board on the developer's own Wi-Fi (the rig docs forbid it; local builds need a private `HIL_LOCAL_DUT_API_KEY`). `[VERIFIED]` `hil-network.yaml`, `conftest.py`.
**Prevention:** structure as **explicit checkpoints with machine-checkable preconditions**, the same shape the hil-rig phases already use for maintainer checkpoints:
1. *Setup checkpoint (human):* fan model, topology, port, mounting, ambient noted; tool prints a checklist and refuses to continue without `--confirm-setup`.
2. *Instrument self-check (automated, no fan, uses the PWM-to-tach bridge from the rig QC):* reads PWM frequency x 30 within +-10 % (the existing QC-02 acceptance) before any real fan is trusted, proving the pulse counter, filter and window chain. Skipped only with an explicit flag recorded in the report.
3. *Preflight (automated, fan attached):* plausibility gate of Pitfall 20.
4. *Run (automated, device-side sequence):* no human; abort path leaves the board safe.
5. *Review checkpoint (human):* the human reads `report.html` (fit, residuals, drift, "not observable" list) and decides to promote the fan file. Do not auto-commit or auto-promote.
- Never retry a failed measurement silently; the HIL QC note ("reruns hide the faults the rig exists to show") applies: a failed run is kept and reported, a rerun is a new, labelled run.
- Use distinct credentials for non-rig boards; refuse a build or run whose API key equals the published HIL key unless the board is on the isolated HIL network (check the configured SSID).
- Keep HIL tests out of the default `make check`; a gated `make hil-fan` with a clear "needs hardware" skip, like the rig's capability markers.
**Warning signs:** the same sequence gives different fits on different days; a report with no environment block; rerunning until green.
**Phase:** C, D, F.

---

## Moderate Pitfalls

### Pitfall 28: A replay tolerance chosen after seeing the data, or tighter than the measurement itself

**What goes wrong:** "Within tolerance" is the milestone's done criterion. If the tolerance is set after looking at residuals it is circular; if it is tighter than run-to-run repeatability the sim fails against reality for no reason; if only the fitted sequence is replayed it proves nothing about generalisation.
**Prevention:** pre-register the metric (for example per-window RPM error, max and RMS, excluding the first window after each step; steady-state error per duty point; time-to-90 % on step), the tolerance rule (`max(fixed floor, k x measured repeatability)`), and replay **a sequence not used in the fit** (hold-out steps and an unseen duty sequence). Record both numbers in the report. Close A1/A2 only when the hold-out passes. Also replay with the sim noise **enabled and seeded** and noise **disabled**, to separate bias from noise.
**Phase:** F.

### Pitfall 29: Tach noise from PWM coupling, and stopped-fan phantom readings

**What goes wrong:** The 25 kHz PWM and the tach share a 4-wire cable; capacitive/inductive coupling adds spurious edges. The PCNT filter tops out at 13 us (`[VERIFIED]`), so spikes wider than that count. Symptoms: a stopped fan or unplugged tach reads nonzero, or readings inflate at certain duty values (where PWM edge timing beats the filter); the weak `INPUT_PULLUP` (internal, tens of kilohms) makes the line slow and noise-prone. The hil QC documents that at 0 % or 100 % duty a LEDC output has no edges, and 25 kHz edges only just clear the filter. `[VERIFIED]` docstring; `[REASONED]` for cable coupling.
**Prevention:** the analyzer applies a **plausibility gate** (reject windows with RPM above a margin over the datasheet maximum, and readings nonzero while the model/duty says the fan is stopped for longer than coast-down), reports gate rejections instead of dropping them silently, and **does not add software debounce into the measurement path** (it would change what the product firmware sees). Whether to improve the product tach (external pull-up, shielded cable, filter) is a product finding written into the report, not something this milestone fixes. Model it in the sim as the `tach` quirk "spurious pulses" only if measured.
**Phase:** C (gate in firmware or host), E (reporting).

### Pitfall 30: Start/stop dynamics are under-observed and mis-modelled

**What goes wrong:** Kick-start vs hold hysteresis, start delay, coast-down and "fans that do not stop at duty 0" require **long, low-RPM, slow transients** the 1 s counter resolves worst. Coast-down from 1800 RPM with `tau_down` ~2.5 s passes through 200 RPM, where counts are single digits; ESPHome-style fans also may spin a few revolutions and stop (non-exponential at low speed; static friction). A single exponential `tau_down` cannot represent that. Starting from rest needs the full stop first (wait many `tau_down`), so a start test costs minutes; with no fan-power switch the only way to reach rest is 0 % PWM on a fan that supports it (Arctic yes, Noctua no).
**Prevention:** model start/stop as explicit phases (start delay, kick-start threshold `start_duty` vs hold threshold `min_duty`, spin-up lag, coast-down with a stop speed below which RPM snaps to 0), each with its own measurability flag; fit coast-down in the count domain (Pitfall 17); allow a `below_min` behaviour per fan; keep the v0.1.0 linear + lag as the schema-1 path (Pitfall 24). Test the model against synthetic data generated by itself first (fit recovers known parameters with noise), before any real data.
**Phase:** A, E, F.

### Pitfall 31: Fan laws and datasheet airflow/power presented as measurement

**What goes wrong:** Airflow ~ N, pressure ~ N^2, power ~ N^3 are idealised scaling laws; real fan power exponent is usually below 3 at low speed (bearing and electronics overhead) and airflow in a system depends on impedance, not only RPM. Datasheet gives a single operating point (max RPM) in free air. Scaling from that point and displaying it as "airflow" or "power" will be read as measurement; Rev 3.3 has no current or airflow sensing (out of scope in PROJECT.md). Values at 0 RPM (leakage, standby power) and clamping near `min_rpm` are easy to get wrong. `[REASONED]`
**Prevention:** store datasheet points with their conditions, scale in one function with a documented exponent (configurable, default per laws), label airflow and power in API/UI as **derived**, include a "derived" flag in the JSON, add a unit test that scaling at the datasheet point returns the datasheet value, and never route derived values into the fit or the replay comparison.
**Phase:** A.

### Pitfall 32: Fault semantics that do not match what the firmware can observe

**What goes wrong:** Each fault must act at the layer the hardware fault acts at. "Dead tach" must keep the physical fan running (airflow continues) but publish 0; "wrong ppr" scales the published value only; "intermittent tach" drops or zeroes whole windows (deterministically, Pitfall 23), not individual noise; "stuck/blocked" drives RPM toward zero while PWM stays commanded; "late/no start" delays or inhibits the start path and must interact with the start-duty hysteresis; "degraded" reduces max RPM and slows `tau`, not just a constant offset. Implementing all of them as multipliers on the published RPM makes `status` truth and firmware-visible value indistinguishable, and makes `stall_guard` (v0.3) un-testable.
**Prevention:** three layers in the engine (physical state, tach observation, connection), each fault tagged with its layer; `status`/`history` expose both physical and observed RPM; table tests for fault x state; web UI and CLI show faults from the same API (the UI stays a pure client, v0.1.0 acceptance rule); an `after`-style scheduled fault is out of scope (v0.4), so no timers in the fault API.
**Phase:** B.

### Pitfall 33: Measurement firmware drifts from the product firmware

**What goes wrong:** The measurement firmware is a separate YAML. If it copies tach and ledc config instead of including the product hardware package (`hardware-rev-3.3.yaml` -> `3.1`), measurements stop matching what the product does (different `update_interval`, `multiply`, filter, ledc frequency or `speed_count`). Conversely, including the whole product firmware drags in `ota`, `http_request`, `update`, Wi-Fi behaviours that confound the run. Also the arduino/ESP32 build is **not** hermetic (PlatformIO and registry access), unlike the host builds; the "no cg.add_library" rule applies to the SiL host path only. `[VERIFIED]` hardware files; `[REASONED]` for the remainder.
**Prevention:** build the measurement firmware as `packages:` including the product hardware package with minimal overrides (cumulative `total:`, faster `update_interval`, API actions, fail-safe), pin ESPHome 2026.10.1 via the Docker image, record the firmware sha and the diff vs product settings in the report, and treat tach/ledc keys as read-only references to the hardware package, not copies. Decide where the YAML lives (this repo vs `wifi-fancontroller`) early; it needs a real-board build path and secrets handling (Pitfall 27).
**Phase:** C.

### Pitfall 34: `report.html` breaks the no-CDN, single-file, reproducible rule

**What goes wrong:** Plots via CDN or `file://` references violate the locality constraint; a report that embeds only plots and not the data cannot be re-fitted; non-deterministic content (timestamps in the body, map-ordered tables) makes diffs noisy.
**Prevention:** one self-contained HTML (vendored uPlot or inline SVG, data embedded as JSON), a CI-style check for `http(s)://` references (as for the UI), deterministic ordering, a content block with tool version, fit settings, seeds, and the raw count series, so `sil fan-analyze --refit report.json` can reproduce the fit offline. Write progress to stderr, the JSON result to stdout (existing CLI convention).
**Phase:** E.

### Pitfall 35: Nonlinear curve in the Go model breaks the integrator or existing behaviour

**What goes wrong:** Interpolating a curve between knots with direction-dependent hysteresis introduces state; extrapolation outside the knots; discontinuity at `start_duty` or `min_duty` giving a target jump that, with a small `tau`, `dt`-dependent Euler steps then overshoot; tau as function of RPM. The v0.1.0 `Step` is an exact exponential update (`1 - exp(-dt/tau)`), which is dt-independent only for a constant target; a target that changes inside one `dt` is dt-dependent, so behind-real-time catch-up ticks can change results. `[VERIFIED]` model comment; `[REASONED]`.
**Prevention:** keep the model a pure function of `(params, state, duty, dt)`; test that one step of `2*dt` equals two steps of `dt` for constant duty, and that results with ticks split differently stay within a stated tolerance for changing duty; clamp, never extrapolate beyond the endpoints; add a monotone-curve property test; keep schema-1 golden tests bit-identical.
**Phase:** A.

---

## Minor Pitfalls

### Pitfall 36: `fan-analyze` conflated with the instance machinery
It does not need `sil up`, run dirs, `ESPHOME_PREFDIR`, MACs or the sim-bus. Do not create an instance for it or reuse the instance port allocation; keep it a standalone subcommand with its own state dir for run records. Share only the API client and the model for replay (`sil` already uses the model for replay in phase F).

### Pitfall 37: Units and conventions between `multiply: 0.5`, ppr, RPM and pulses/min
The product reports RPM after `multiply: 0.5`; the measurement firmware should expose raw pulses/min and totals and let the host apply ppr (Pitfall 26), otherwise the host cannot tell a ppr change from a speed change. Say "pulses" or "RPM" in every field name.

### Pitfall 38: Duty resolution and quantisation of the command
The product drives fans through `speed` fans with `speed_count: 100` (1 % steps) and ledc at 25 kHz (an 11-bit depth class on ESP32-S2, `[UNVERIFIED]`). The sweep's duty grid must use the same quantisation, and the fitted curve is a function of `fan.speed` percent, not ledc counts.

### Pitfall 39: Sample-rate and bus-rate load from richer tach quirks
Dropout and noise features add messages; keep the existing outbound cap and dedupe in the C++ sim sensor, and assert the bus rate in a contract test with noise enabled.

### Pitfall 40: Documentation of what was assumed
After the campaign, `fans/*.toml` still carry `ASSUMED` comments for unmeasured values (for example Noctua start behaviour). Make "unmeasured" a first-class, machine-listed state so closing A1/A2 is verifiable rather than a manual edit.

---

## Phase-Specific Warnings

| Phase topic | Likely pitfall | Mitigation |
|-------------|----------------|------------|
| A: schema 2 and model | 24 (schema break / silent defaults), 23 (noise determinism), 18 (where quantisation lives), 31, 35 | `schema = 2`, absent-means-unmodelled; keyed-hash noise; quantisation in the C++ tach sensor; derived flag; dt-split tests |
| A: start/stop | 30 | Explicit phases and per-parameter measurability; self-recovery test on synthetic data |
| B: fault injection | 22 (leaks), 32 (layering) | Lifecycle table, three layers, idempotent set/clear, faults in status and history, unplug resets running state |
| B: UI and CLI | 22 | Faults visible in the page and in `watch`; do not depend on `Status.error` display |
| C: measurement firmware | 17, 20, 21, 33, 27 | Totals, 100 to 250 ms interval, on-device schedule with seq numbers, fail-safe on disconnect and boot, package-include of the product hardware YAML, private keys |
| D: API client | 25, 21, 20 | Vendored proto and golden bytes, contract test against the host firmware, keepalive, cleanup and verification of safe state |
| D: orchestration | 20, 27 | Preflight gate, hard limits, checkpoints with machine-checkable preconditions |
| E: fitting | 19, 17, 30 | Count-domain forward model, monotone low-parameter curves, hold-out, uncertainty, `not observable` status |
| E: report | 34, 24 | Self-contained deterministic HTML, template TOML writer, round-trip test |
| F: measurement | 26, 27, 29 | Warm-up, interleaved order, anchor repeats, environment block, instrument self-check on the bridge, plausibility gate |
| F: replay | 18, 28 | Same-domain (1 s counts) comparison, aligned windows, pre-registered tolerance, hold-out sequence |

**Research flags**
- Phase C: **needs deeper research** (cumulative-counter vs ISR instrument; how to deliver time-stamped samples over the API; ESP32-S2 PCNT constraints; on-device sequencing in ESPHome YAML; fail-safe hooks).
- Phase D: **needs deeper research** (client library vs generated code; keepalive and reconnect semantics; ExecuteService response usage).
- Phase E: **needs deeper research** (identifiability of tau and start delay in the count domain; uncertainty method; curve family).
- Phase A (model, schema), B (faults): standard patterns, mostly design; no external research needed beyond Pitfalls 22 to 24.
- Phase F: protocol design review with the maintainer (human checkpoints), not technical research.

## Sources

- Repo: `.planning/PROJECT.md`, `.planning/milestones/v0.1.0-research/PITFALLS.md`, `internal/fanlib/fanlib.go`, `internal/model/fan.go`, `internal/engine/engine.go`, `internal/apiclient/probe.go`, `components/sim_bus/sim_sensor.{h,cpp}`, `fans/*.toml`: HIGH (read directly)
- `ghcr.io/esphome/esphome:2026.10.1` image source: `components/pulse_counter/{pulse_counter_sensor.cpp,sensor.py}` (update formula, first-sample drop, rising-edge default, 13 us PCNT filter cap, `total:` sensor), `components/api/{api_connection.cpp,api_connection.h,api.proto}` (60 s keepalive, 2.5x disconnect, `object_id`, ExecuteService response support): HIGH (read in the pinned image)
- `wifi-fancontroller/hardware-rev-3.1.yaml`, `wifi-fancontroller/hil/tests/rig/{conftest.py,test_22_qc_fans.py}`, `hil/hil-network.yaml`: HIGH (read directly; the hil-rig private supervisor repo not read)
- Fan physics (hysteresis, fan laws, PST topology, thermal drift, PCNT unit count, ledc bit depth): training knowledge and engineering judgment, MEDIUM to LOW; confirm in phases A, C, F. Arctic and Noctua datasheet pages were not re-fetched here.

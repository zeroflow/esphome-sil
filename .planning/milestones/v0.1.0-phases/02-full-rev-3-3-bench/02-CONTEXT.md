# Phase 2: Full Rev 3.3 Bench - Context

**Gathered:** 2026-11-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Widen the Phase 1 skeleton to the full Rev 3.3 IO surface and make it drivable from both the CLI and the user's own Home Assistant. New sim platforms: buttons USR1-3 (`binary_sensor`), the 5-LED status strip as an addressable light carrying stock `partition` lights (board + per-fan), temperature/humidity (HDC1080 replacement). A shipped Rev 3.3 example host config with HIL contract entity names, consumed via `external_components:`. Per-port fan choice. Full CLI vocabulary: `sil status` (full JSON), `sil set`, `sil press`, `sil watch`, `sil logs`. HA control with stable device identity. One command runs the contract test and the aioesphomeapi smoke test.

Requirements: IO-03, IO-04, IO-05, FW-01, FW-02, FW-03, MODEL-03, CLI-01, CLI-02, CLI-03, CLI-04, HA-01, HA-02, TEST-01, TEST-02.

Not in this phase: web UI (Phase 3), time factor, scenario files, fault models/plug-unplug faults (P2), real product modules (REAL-01), `sil rebuild` (RUN-02).

</domain>

<decisions>
## Implementation Decisions

### Carried forward from Phase 1 (not re-discussed)
- Run lifecycle, supervisor/engine split, auto-restart, `sil fw restart` / `sil sim restart` (01-CONTEXT D-01..D-05).
- XDG identity/run/cache dirs, sticky API port from 6054, derived `02:` MAC, stored 0600 API key, `sil-<name>` / `SiL <name>` naming (D-06..D-09). These are what make HA-01 "same device on restart, separate device per instance" hold; Phase 2 verifies them, it does not redesign them.
- Strict `fan.toml`, Arctic P12 PWM PST as shipped fan, hysteresis/asymmetric-lag model (D-10..D-14).
- Build skip-by-hash, banned-component lint after `esphome config` (D-15..D-18). The lint list still applies to the Rev 3.3 example (no `wifi`, `ledc`, `hdc1080`/`i2c`, `esp32_rmt_led_strip`, nothing auto-loading `json`).
- stdout = JSON result, stderr = progress; exit codes 0 / 1 command failed / 2 infrastructure (CLI-05).

### Button press model
- **D-01:** `sil press usr1` holds the button **200 ms** of sim time; `sil press usr1 --long` holds **1500 ms** (well past the product's 1000 ms USR2 long-press threshold, and clear of its 50 ms button poll). `--hold <dur>` overrides either preset.
- **D-02:** `sil press` **blocks until the release edge** has been sent, then returns its JSON result, so a script can assert right after it.
- **D-03:** Raw control: `sil press usr1 --down` / `--up` set the level and return immediately, for holds of any length and chords across other commands.
- **D-04:** A press (preset, `--hold` or `--down`) on a button that is already held is **rejected with exit 1 and a JSON error** naming the button; no queueing. `--up` on a released button is likewise an error.
- Buttons are sent as levels over the bus (as research decided); the firmware measures hold time itself.

### LED state in status/watch
- **D-05:** `sil status` reports LEDs as **raw strip + named segments**:
  `"leds": {"strip": ["#rrggbb", ... 5 entries], "segments": {"board":[0], "fan1":[1], ...}}`. Strip colors are the **final pixel colors** as written to the strip (brightness, color correction and effects already applied by ESPHome), so assertions compare what a person would see.
- **D-06:** The index → segment-name mapping comes from the **firmware's HELLO manifest** (the YAML is the single source of truth). The researcher/planner picks the mechanism: derive from the `partition` lights at codegen, or a `segments:` key on the `sim_bus` light. It must not be a separate simulator-side config.
- **D-07:** LED frames go over the bus **only on change, capped at 20 Hz**. Idle firmware sends nothing. Status always shows the latest frame.
- **D-08:** `sil watch` emits **every LED frame received from the bus** (≤ 20 Hz); no extra throttling. Consumers filter.

### set / watch vocabulary
- **D-09:** `sil set` takes **`key=value` pairs**: `sil set temperature=24.5 humidity=40 port2.fan=arctic-p12-pwm-pst`. Several keys in one call are validated first and applied together (all or nothing). Unknown keys, out-of-range values and unknown fan names fail with exit 1 and a JSON error. The result echoes what was applied. Keys follow the IO-06 channel naming style; the same keys are used by the HTTP API (CLI-06).
- **D-10:** Swapping the fan on a port (`portN.fan=<library name>`) is an **instant unplug/replug**: RPM drops to 0 and the new fan spins up from rest at the current duty, using its own `fan.toml`. Real plug/unplug as a fault (timed, partial) stays in P2.
- **D-11:** Temperature and humidity are **constant values** set by the user. They are sent on change and re-sent about every 10 s (mirrors the HDC1080's `update_interval: 10s`). No noise, drift or ramps in M1. Defaults: **22 °C / 45 %RH**.
- **D-12:** `sil watch` streams **typed change events as NDJSON**. The first line is a full `snapshot` (same shape as `sil status`). Then one line per change, each with sim time `t` (µs) and kind `k`: e.g. `{"t":..,"k":"duty","port":2,"v":0.4}`, `{"k":"rpm",...}` (at the firmware's 1 s publish), `{"k":"button","id":"usr1","v":true}`, `{"k":"leds","strip":[...]}`, plus env and firmware-connected/disconnected events. The planner fixes the exact event kinds; they are internal while 0.x.

### Example firmware logic
- **D-13:** The Rev 3.3 example carries **thin, product-like logic** (written in the example YAML, not the real product modules):
  - Board LED (`partition` segment 0): blue `pulse` on boot. Green when an API client connects (replaces `wifi.on_connect`; uses `api` client-connected hooks / `on_boot`).
  - Per-fan LEDs (segments 1-4) show each fan's running state (e.g. green when its RPM > 0, off when stopped). The planner picks the exact colors and threshold.
  - USR1 short press = all fans +10 %, USR3 short press = all fans −10 %, USR2 long press = all fans to 100 %.
  - This is the observable surface the contract test asserts on (button → duty change → RPM change → LED change).
- Entity names follow the HIL contract (`Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`); status LEDs and partitions stay `internal: true` (FW-02).
- Components are consumed via `external_components:` with a local path now (FW-03), ready to switch to `github://zeroflow/esphome-sil@vX.Y`.

### Contract & smoke tests
- **D-14:** TEST-01 (contract) and TEST-02 (aioesphomeapi smoke) are **pytest under `tests/`**, run by **`make test-contract`**. They drive `sil` through its CLI/JSON (subprocess), and TEST-02 uses aioesphomeapi directly against the firmware API with the key from `sil status`. Python is fine here (tests, not simulator). Mirror the product repo's HIL tooling where sensible (uv-managed env, as in `wifi-fancontroller/hil/pyproject.toml`).
- **D-15:** The live Home Assistant check (success criterion 4: add by host:port + key, control fans, see RPM/temp/humidity/buttons, restart → same device, second instance → separate device) is a **manual UAT checklist** at end-of-phase verification. The automated tests cover the parts aioesphomeapi can see: entity set and names, fan command round trip, and that MAC/name/key are stable across `sil down` / `sil up`.
- **D-16:** Budget: **< 60 s warm** (build cached per Phase 1 D-15), headless. Tests use unique instance names so they run in parallel with a developer's own instance and clean up with `sil down`.

### Autonomous-run additions (2026-11-09, recorded during plan revision, pending morning review)
- **D-17 (2026-11-09):** The example's "green when an API client connects" (D-13) **ignores sil's own readiness probe**, which identifies itself with `client_info` `"sil"` (`internal/apiclient/probe.go`, `clientInfo`). After `sil up` the board keeps its blue boot `pulse` until a real client (Home Assistant, aioesphomeapi) connects. Implemented as the condition of the example's `api: on_client_connected` hook; the probe's client_info stays unchanged. Source: Research Pitfall 3; plan 02-02.
- **D-18 (2026-11-09):** **TEST-01 drives and observes only through `sil`**, as D-14 and the `.claude/CLAUDE.md` aioesphomeapi row require: its known start comes from `sil fw restart` (restore_mode ALWAYS_OFF), its duty changes from `sil press`, and duty, RPM, LEDs and events are read from `sil status` / `sil watch`; it sends no fan command through the native API. The firmware-side arrival of `sil set temperature=… humidity=…` (the "set temperature → `Temperature` entity follows" step of the Specific Ideas chain, and "a temperature change arrives" in success criterion 5) is asserted in **TEST-02** through the `Temperature`/`Humidity` entity states, which is entity visibility, the API's stated role. `make test-contract` runs both, so criterion 5 is still proven by the one command. No exception to `.claude/CLAUDE.md` is needed. Plan 02-07.
- **Planning acceptance (2026-11-09, not a product decision):** five plans exceed the plan checker's 10-file warning threshold: 02-01 (13 files), 02-02 (13), 02-03 (13), 02-06 (12) and 02-05 (11). They stay as planned, all below this run's 15-file cap. Each is one vertical slice through component, example, engine, supervisor, control API, CLI and e2e, and some test files are listed only because `make check` must stay green (fake Control embedding, snapshot-order and struct-literal tests). Splitting at the task boundary would make single-task plans, against the configured coarse granularity (2-3 tasks per plan), and the largest tasks (13, 12 and 11 files) would still be over the threshold; splitting across layers would break tracer-first. Every estimate (70k-95k tokens) is below the 100k smart-zone budget, and `confidence: low` comes from zero calibration samples (`estimate-calibration`), not from a self-rating. If execution shows context pressure, split 02-01 (tracer / press vocabulary) and 02-06 (watch / logs) first.

### Claude's Discretion
- `sil logs` format and options (follow, firmware vs simulator, tail).
- `sil watch` filters and exit behaviour (e.g. `--kind`, `--until`, exit when the instance stops).
- Whether `sil set` values (env, fan per port) survive `sil sim restart` / `sil fw restart`. The leaning is that they live in supervisor-held run state and are replayed on resync, but the planner decides.
- How a fan is chosen at `sil up` time (flag or default all ports to the shipped fan) and how the fan library is located/listed.
- Example file name/location and how the Phase 1 example relates to it (replace or keep both).
- Exact JSON shapes beyond the examples above.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope and requirements
- `.planning/PROJECT.md` — core value, constraints, key decisions
- `.planning/REQUIREMENTS.md` — IO-03..05, FW-01..03, MODEL-03, CLI-01..04, HA-01..02, TEST-01..02
- `.planning/ROADMAP.md` §Phase 2 — goal and the 5 success criteria (acceptance gate)
- `.planning/STATE.md` §Blockers/Concerns — Phase 2 risks: `partition` + `pulse` on the sim addressable light and the LED frame rate are unverified at runtime; live HA behaviour with the stable-MAC scheme is unverified
- `.planning/phases/01-closed-loop-fan-skeleton/01-CONTEXT.md` — Phase 1 decisions this phase builds on (lifecycle, identity, build, fan model)

### Research
- `.planning/research/SUMMARY.md`, `.planning/research/STACK.md` (ESPHome-side component table: binary_sensor, AddressableLight `get_view_internal`, dedupe/rate cap), `.planning/research/ARCHITECTURE.md`, `.planning/research/PITFALLS.md` (HA identity, LED frame flood), `.planning/research/FEATURES.md`

### Product reference (Rev 3.3 surface and HIL contract)
- `../wifi-fancontroller/hardware-rev-3.3.yaml` → `../wifi-fancontroller/hardware-rev-3.1.yaml` — buttons (inverted gpio, names `Button USRn`), `hdc1080` with `update_interval: 10s`, `esp32_rmt_led_strip` 5 LEDs + `partition` board/fan1-4 with effect list, boot blue `pulse` / green on connect
- `../wifi-fancontroller/modules/usr_buttons_mode.yaml` — product long-press semantics (USR2 1000 ms threshold, 50 ms poll); reference for D-01 only, not to be included
- `../wifi-fancontroller/hil/hil/contract.py`, `../wifi-fancontroller/hil/hil/entities.py` — HIL contract entity naming the example must match (FW-02)
- `../wifi-fancontroller/hil/pyproject.toml` — Python test tooling pattern (uv, aioesphomeapi) for D-14

### Prototype
- `prototype/components/sim_bus/binary_sensor.py`, `prototype/components/sim_bus/light.py`, `prototype/README.md` — verified starting points for IO-03/IO-04

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `prototype/components/sim_bus/binary_sensor.py` and `light.py` (+ `sim_bus.{h,cpp}`): binary_sensor and AddressableLight platforms that compiled against 2026.10.1 and carried `partition` lights. Move to `components/sim_bus/` alongside the Phase 1 output/sensor platforms; add `channel:`, HELLO manifest entries (incl. LED segment names), on-change + 20 Hz cap for the light.
- Phase 1's Go engine, bus server, HTTP/JSON API and `run.json` discovery: Phase 2 adds operations to the same vocabulary rather than new transports.

### Established Patterns
- Product YAML: buttons are `binary_sensor` with `name: "Button USRn"`, LEDs `internal: True`, temp/humidity `name: "Temperature"` / `"Humidity"`.
- The prototype once sent 66 identical all-black LED frames in ~5 s idle: dedupe is mandatory (D-07).

### Integration Points
- `components/` at the repo root for `external_components:` (FW-03).
- `tests/` (new) + `Makefile` target `test-contract` (D-14).
- `fans/` library: a second fan file may be added to exercise MODEL-03 swaps (see deferred).

</code_context>

<specifics>
## Specific Ideas

- Long press 1500 ms was chosen to sit safely past the product's 1000 ms USR2 threshold, so the real `usr_buttons_mode` module will behave the same later.
- The fan swap being a hard unplug/replug is deliberately a useful stimulus for the future `stall_guard`.
- The contract test's chain is: press USR1 → duty +10 % → RPM rises → per-fan LED turns on; set temperature → `Temperature` entity follows.

</specifics>

<deferred>
## Deferred Ideas

- Temperature ramps (`--ramp 60s`) and sensor noise: later, with scenarios (SCEN-01) or the thermal model (P5).
- Throwaway Home Assistant container for automated HA tests: not in M1; manual UAT instead.
- Pulling in real product modules (`usr_buttons`, `rpm_status_leds`): REAL-01 (v2).
- A second shipped fan with `below_min = "hold"` (e.g. Noctua NF-A12x25), carried from Phase 1: fits here if the planner needs it to test MODEL-03 swaps; otherwise later.

</deferred>

---

*Phase: 02-full-rev-3-3-bench*
*Context gathered: 2026-11-08*

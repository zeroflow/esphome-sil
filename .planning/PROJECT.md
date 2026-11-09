# esphome-sil

## What This Is

A software-in-the-loop test bench ("labcar") for ESPHome firmware. The ESPHome `host:` platform runs firmware on Linux; simulated IO components (`sim_*`) connect it over a local sim-bus to a plant simulator, so control loops can be exercised closed-loop, locally and fast, before firmware reaches real hardware. It is for the developer and for coding agents; the first domain is the WiFi Fan Controller (Rev 3.3 as reference).

## Core Value

A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.

## Requirements

### Validated

(None yet — ship to validate)

### Active

Milestone 1 — "the plumbing works end to end":

- [ ] ESPHome `host:` firmware with `sim_*` external components builds (pinned ESPHome 2026.10.1, Docker) and runs on Linux
- [ ] Sim hardware mirrors Rev 3.3: 4 fan ports (PWM out, RPM in, no pulse simulation), status LED strip as addressable light so `partition` lights work, buttons USR1-3, temperature/humidity sensor (HDC1080 replacement)
- [ ] Firmware and simulator talk over a local sim-bus socket
- [ ] Linear fan model with first-order lag (min duty, start threshold, max RPM, inertia), parameters from `fan.toml`
- [ ] Minimal example firmware YAML (Rev 3.3 IO surface, not the real product modules) lives in this repo as example and contract test
- [ ] The firmware's native API is reachable by any Home Assistant instance the user points at it; fan speed control, RPM, LEDs, buttons, temperature are usable from HA
- [ ] Simple local web UI (served by the simulator, live over WebSocket, no build step, no runtime CDN): per fan port duty, RPM, live plot; virtual LEDs; short/long button press; run info (sim time, firmware connected)
- [ ] CLI/JSON API is first citizen (`sil up`, `sil set`, `sil press`, status as JSON); the web UI is only a client of the same API
- [ ] Simulator uses an injectable clock, `dt`-based model integration and sim timestamps on the sim-bus, so the time factor can be added later without a rewrite

### Out of Scope

- Time factor (x100+) in M1 — planned for a later milestone; HA in the loop forces x1 anyway, so sped-up runs will be headless test runs
- Real wifi-fancontroller modules in the sim (`rpm_pi_control`, `stall_guard`, ...) in M1 — M1 proves the plumbing with a minimal example; the real modules follow
- Rev 3.3 SiL YAML in the product repo in M1 — starts here, moves to `wifi-fancontroller` once the consumer interface settles (0.x → 1.0)
- Fault models, Lua, plug/unplug, virtual supervisor — P2
- Web UI eye candy, fan photos, drag & drop — P3
- `fan-analyze` companion tool — P4
- Rack thermal model and control-quality KPIs — P5
- Lockstep / virtual time, CI runner — later
- Cloud simulators (Wokwi etc.) and QEMU — local-only requirement; QEMU has no ESP32-S2 machine and would need custom peripheral models
- Testing drivers, framework (arduino/esp-idf), Wi-Fi, OTA, `update: http_request`, ISRs, heap — not possible on `host:`, stays the HIL's job
- Revisions other than 3.3 — later; Rev 1.0 irrelevant

## Context

- Test pyramid: host SiL (this repo) → HIL rig with tach/PWM loopback → HIL with real fans. HIL lives in `wifi-fancontroller/hil/` (pytest/uv, aioesphomeapi fixtures).
- Rev 3.3 hardware (`wifi-fancontroller/hardware-rev-3.3.yaml` → includes `hardware-rev-3.1.yaml`): `ledc` PWM GPIO12-15 → `speed` fans fan1-4; `pulse_counter` tach GPIO16/17/18/21; `hdc1080` temp/humidity; gpio buttons USR1-3 (GPIO38/37/36); `esp32_rmt_led_strip` with `partition` lights (board status + per-fan status).
- `ota`, `http_request` and `update` sit in the top-level firmware YAML, not the hardware package; the SiL build needs its own top-level host config, swapping only the hardware package will not compile.
- ESPHome itself is Python; building the host firmware needs the ESPHome toolchain (contained in the pinned Docker image). A host binary built in the container may need matching glibc or must run in the container too — to be researched.
- Product-repo modules for later: `rpm_pi_control`, `stall_guard`, `rpm_status_leds`, `usr_buttons(_mode)`, `fallback_mode`, `temperature_curve*`, `dual_curve`. Existing harness pattern: `fallback_mode_harness_dev.yaml`.
- Time-factor design (later): small build-time patch on ESPHome 2026.10.1 `components/host/hal.cpp` (`millis`, `micros`, `millis_64`, `arch_get_cpu_cycle_count`, `delay`, `delayMicroseconds`) and the `select()` timeout in `core/wake/wake_host.cpp::wakeable_delay` (2026.10.1).
- Original design notes: `docs/initial-idea.md`.
- Built with Claude Code (Opus 5.5) as primary implementer; fast compile/run loops matter.

## Constraints

- **Locality**: everything runs on own hardware and own code — a vendor shutdown must not kill the pipeline.
- **Tech stack**: simulator, CLI and web server in Go (single static binary, embedded UI); no Python in the simulator. Product-repo tests may stay pytest and drive `sil` via CLI/JSON.
- **ESPHome version**: pinned 2026.10.1 (matches the product repo); build via `esphome/esphome:2026.10.1` Docker image.
- **Web UI**: no build step, no external CDN at runtime; vendored plot library (uPlot leaning).
- **Chip-agnostic sim layer**: ESP32-S2 is the core, ESP32-C6 upcoming.
- **Consumer interface**: semver tags, `0.x` while settling; sim-bus protocol, web UI, `fan.toml` schema and Lua API are internal.
- **License**: GPL-3.0.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go for simulator/CLI/web server | Single static binary, stdlib HTTP + `embed`, gopher-lua later, fast iteration loop for agent-driven development; no hard real-time need that would justify Rust | — Pending |
| Rev 3.3 as reference revision | Current core hardware; other revisions later | — Pending |
| M1 = minimal example firmware on Rev 3.3 IO surface | Proves sim-bus, components, model, UI and HA plumbing without coupling to product modules | — Pending |
| Sim YAML lives in this repo first | Interface still moving; move to product repo at 1.0 | — Pending |
| HA: sim only exposes native API, user picks the HA instance | No coupling to a specific HA setup; production HA stays untouched unless the user chooses it | — Pending |
| Fan model M1 = linear + first-order lag from `fan.toml` | Enough dynamics to make a closed loop meaningful; noise and faults later | — Pending |
| Time factor deferred, but injectable clock from day one | HA in the loop requires x1; retrofitting a clock abstraction is expensive | — Pending |
| Tach as RPM value, no pulse simulation | Drivers are HIL's job; SiL tests logic | — Pending |
| Firmware build via pinned ESPHome Docker image | Nothing to install on the host, matches `compile.sh` | — Pending |
| RPM sensor is a `PollingComponent` (exception to the push-only sensor rule) | IO-02 requires a 1 s windowed mean published at `update_interval`; temp/humidity/buttons stay push-only `Component`s. Decided autonomously 2026-11-09 during Phase 1 planning (plan-checker blocker); needs maintainer review | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-11-08 after initialization*

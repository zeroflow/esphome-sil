# esphome-sil

## What This Is

A software-in-the-loop test bench ("labcar") for ESPHome firmware. The ESPHome `host:` platform runs firmware on Linux; simulated IO components (`sim_*`) connect it over a local sim-bus to a plant simulator, so control loops can be exercised closed-loop, locally and fast, before firmware reaches real hardware. It is for the developer and for coding agents; the first domain is the WiFi Fan Controller (Rev 3.3 as reference).

## Core Value

A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.

## Requirements

### Validated

- ✓ Sim hardware mirrors Rev 3.3: 4 fan ports (PWM out, RPM in, no pulse simulation), status LED strip as addressable light so `partition` lights work, buttons USR1-3, temperature/humidity sensor (HDC1080 replacement) — Phase 2
- ✓ Minimal example firmware YAML (Rev 3.3 IO surface, not the real product modules) lives in this repo as example and contract test — Phase 2
- ✓ The firmware's native API is reachable by any Home Assistant instance the user points at it; fan speed control, RPM, LEDs, buttons, temperature are usable from HA — Phase 2 (accepted on aioesphomeapi proxy; live HA UAT open)
- ✓ CLI/JSON API is first citizen (`sil up`, `sil set`, `sil press`, status as JSON); the web UI is only a client of the same API — Phase 2
- ✓ ESPHome `host:` firmware with `sim_*` external components builds (pinned ESPHome 2026.10.1, Docker, static ELF, offline toolcache) and runs on Linux — v0.1.0
- ✓ Firmware and simulator talk over a local sim-bus socket (NDJSON, golden vectors shared by Go and C++, resync after either side restarts) — v0.1.0
- ✓ Fan model with start/min duty hysteresis and asymmetric first-order lag, parameters from a strict `fan.toml` library (Arctic P12, Noctua NF-A12x25) — v0.1.0
- ✓ Local web UI served by the simulator, live over WebSocket, no build step, no runtime CDN: per-port duty/RPM with uPlot scopes, virtual LEDs, USR holds, environment controls, run info — v0.1.0
- ✓ Injectable clock, `dt`-based model integration and sim timestamps on the sim-bus (wall-clock lint enforced), so the time factor can be added without a rewrite — v0.1.0

### Active

Next milestone: not yet defined (`/gsd-new-milestone`). Candidates from Out of Scope and carried-forward debt: time factor for headless runs, real wifi-fancontroller modules in the sim, fault models / Lua (P2), live Home Assistant UAT.

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
- ESPHome itself is Python; building the host firmware needs the ESPHome toolchain (contained in the pinned Docker image). Resolved in v0.1.0: the firmware is linked `-static` in the image and runs directly on the host (run-in-image stays the fallback if a product module ever needs NSS/DNS).
- Product-repo modules for later: `rpm_pi_control`, `stall_guard`, `rpm_status_leds`, `usr_buttons(_mode)`, `fallback_mode`, `temperature_curve*`, `dual_curve`. Existing harness pattern: `fallback_mode_harness_dev.yaml`.
- Time-factor design (later): small build-time patch on ESPHome 2026.10.1 `components/host/hal.cpp` (`millis`, `micros`, `millis_64`, `arch_get_cpu_cycle_count`, `delay`, `delayMicroseconds`) and the `select()` timeout in `core/wake/wake_host.cpp::wakeable_delay` (2026.10.1).
- Original design notes: `docs/initial-idea.md`.
- Built with Claude Code (Opus 5.5) as primary implementer; fast compile/run loops matter.
- **Current state (v0.1.0 MVP, shipped 2026-11-10):** ~37k lines Go (simulator, CLI, web server), ~1.9k C++ (`sim_bus` components and host tests), ~1.4k Python (ESPHome codegen, contract tests), ~1.6k first-party JS (page). 4 phases, 36 plans in 3 days. Gates: `make check`, e2e, `make test-cpp`, `make test-contract`, `make test-ui`.
- **Known debt:** see `.planning/MILESTONES.md` (v0.1.0) and STATE.md Needs Human — live HA UAT, assumed fan constants, Phase 1/2 open review info items, page does not surface `Status.error`.

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
| Go for simulator/CLI/web server | Single static binary, stdlib HTTP + `embed`, gopher-lua later, fast iteration loop for agent-driven development; no hard real-time need that would justify Rust | ✓ Good (v0.1.0) |
| Rev 3.3 as reference revision | Current core hardware; other revisions later | ✓ Good (v0.1.0) |
| M1 = minimal example firmware on Rev 3.3 IO surface | Proves sim-bus, components, model, UI and HA plumbing without coupling to product modules | ✓ Good (v0.1.0) |
| Sim YAML lives in this repo first | Interface still moving; move to product repo at 1.0 | — Pending (move at 1.0) |
| HA: sim only exposes native API, user picks the HA instance | No coupling to a specific HA setup; production HA stays untouched unless the user chooses it | ✓ Good (v0.1.0); live HA run still unverified |
| Fan model M1 = linear + first-order lag from `fan.toml` | Enough dynamics to make a closed loop meaningful; noise and faults later | ✓ Good (v0.1.0); Arctic/Noctua constants still assumed (A1/A2) |
| Time factor deferred, but injectable clock from day one | HA in the loop requires x1; retrofitting a clock abstraction is expensive | ✓ Good (v0.1.0; clock.Manual made every timing test deterministic) |
| Tach as RPM value, no pulse simulation | Drivers are HIL's job; SiL tests logic | ✓ Good (v0.1.0) |
| Firmware build via pinned ESPHome Docker image | Nothing to install on the host, matches `compile.sh` | ✓ Good (v0.1.0; static `-static` ELF runs on Debian 12) |
| RPM sensor is a `PollingComponent` (exception to the push-only sensor rule) | IO-02 requires a 1 s windowed mean published at `update_interval`; temp/humidity/buttons stay push-only `Component`s. Decided autonomously 2026-11-09 during Phase 1 planning (plan-checker blocker); needs maintainer review | ⚠️ Revisit (works in v0.1.0; maintainer review open) |
| Contract tests drive and observe only via `sil` (D-18); Python test lock hash-gated against the product HIL lock (`make test-contract` depends on `lock-check`) | Keeps the CLI/JSON surface the single contract; hash identity stands in for a dependency review | ✓ Good (Phase 2) |
| `examples/rev33.yaml` is the default `sil up` config; `fan-skeleton.yaml` stays as the minimal option | The Rev 3.3 bench is what users and contract tests run | ✓ Good (Phase 2) |
| `api.lan_host` from a default-route UDP probe, shown in status and the `sil up` hint (mDNS is a stub on host) | HA must be added manually by host:port | ✓ Good (Phase 2); LAN reachability from a real HA unverified |
| Phase 2 verification accepted on automated evidence (4/5; HA criterion proxied by aioesphomeapi) | User decision 2026-11-09 to proceed to Phase 3; live HA UAT tracked in STATE.md Needs Human | ⚠️ Revisit (02-UAT complete; live HA run still open) |
| A stopping engine releases every held USR button itself (`button` event, `reason: engine_stopped`, final all-released snapshot) | One change in `Engine.stopped()` covers sim restart, `sil down` and engine failure; no firmware, protocol or page change. Known limit (user-approved 2026-11-10): a page that misses the frame still sends one refused best-effort `button.up` | ✓ Good (Phase 03.1) |
| A failing `Set` rolls back, including when a shutdown cancels its Apply, and a `Set` racing shutdown answers `stopping` | Concurrency re-review F1/F2/F6 (user decision 2026-11-10); F7 (Set in a restart gap with shutdown already started answers success) recorded as a design question | ✓ Good (Phase 03.1) |

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
*Last updated: 2026-11-10 after v0.1.0 milestone*

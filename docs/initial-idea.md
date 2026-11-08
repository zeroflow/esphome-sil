
# Initial idea: SiL "Labcar" for wifi-fancontroller on the ESPHome host platform

> Captured 2026-11-08 during the HIL rig work, before this repo existed. It is the
> starting point for discussion, not a decided design. Since then decided: this
> simulator lives in its own public repo (`esphome-sil`); the product repo keeps
> `hardware-sim-*.yaml`, its SiL scenarios and tests, and pins a simulator tag.

## Why This Matters

The HIL rig is the final acceptance, but it is slow, single and needs real
hardware. Developers and agents need a local closed-loop test bench (like a
Bosch labcar) to test module logic in seconds before anything reaches the rig:
fan closed loop, LED outputs, button inputs, faults, later rack thermals.

Test pyramid: host-SiL (PRs, new features, agent self-checks) -> HIL with
tach<->PWM loopback -> HIL with real fans.

Hard requirement: everything runs **locally** (own hardware, own code). No
Wokwi or other cloud simulator: if a vendor shuts down, the pipeline must not
die. QEMU is out too: the Espressif fork has no ESP32-S2 machine, and closed
loop would need custom LEDC/PCNT/GPIO models.

Chip relevance: Rev 2.0-3.3 (ESP32-S2, arduino) is the core; Rev 1.0 is
irrelevant; ESP32-C6 is upcoming, so the sim layer must be chip-agnostic.

## Scope Estimate

**Large**: a full milestone, five phases as sketched below.

## Design (agreed in discussion 2026-11-08)

- **Architecture, three processes:**
  1. Firmware: ESPHome `host:` binary. Real modules unchanged; a
     `hardware-sim.yaml` replaces `hardware-rev-*.yaml` with `sim_*`
     components (PWM out, tach in, LED out, button in, temperature sensor).
  2. Simulator: separate process (Python core, Lua models via `lupa`), linked
     to the firmware over a local sim-bus socket.
  3. Test automation: pytest + aioesphomeapi against 127.0.0.1, reusing the
     HIL fixtures where possible.
- **Tach:** no edge or pulse simulation. The simulator delivers RPM directly.
- **CLI/API (JSON) is the first citizen.** A simple web UI ships in P1 as a
  thin client of the same API: no logic of its own, and nothing it can do that
  the CLI cannot. Eye candy follows in P3. Primary use is as a dev and agent
  tool: `sil up` (build from working tree, start
  firmware + simulator), `sil run scenarios/x.toml` (JSON verdict, metrics,
  logs), `sil set port2 fan=blocked`, `sil press usr1 --long`, `pytest -m sil`.
- **Time:** a fixed, configurable time factor per run (not dynamic): x1 for
  normal tests, x100+ for thermal or long-run tests. Implemented as a small
  build-time patch on the pinned ESPHome 2026.10.1.
  - `components/host/hal.cpp`: `millis`, `micros`, `millis_64`,
    `arch_get_cpu_cycle_count` (all `clock_gettime(CLOCK_MONOTONIC)`), and
    `delay`/`delayMicroseconds` (`nanosleep`).
  - The `select()` timeout of the main loop in `core/application.cpp`.
  - Lockstep / virtual time comes later.
  - Everything that talks to the firmware must share the time base. A
    virtual supervisor in the simulator drives `fallback_mode`, and scenario
    timestamps are in sim time.
- **Fan library:** one folder per fan, holding `fan.toml` (ppr, duty->RPM
  curve, inertia, min duty, start threshold, max RPM, noise), an own photo
  (no vendor images), `measured/` and a generated `report.html`. Fault profiles
  live in `fans/_faults/` (worn bearing, blocked, tach dead but spinning, wrong
  ppr) and apply to any fan. Lua handles time-dependent behaviour.

## Phase sketch

| Phase | Content |
|---|---|
| P1 | sim-bus, `sim_*` IO, fan closed loop from `fan.toml`, LED out, button in, time factor, CLI/API, **simple web UI** (see below) |
| P2 | Fault models: fault profiles, Lua, plug/unplug fans, virtual supervisor (`fallback_mode`, `stall_guard`); web UI gets fault switches and plug/unplug |
| P3 | Web UI eye candy: fan library with photos, plug via drag & drop, polished plots and board view |
| P4 | `fan-analyze` companion tool: ppr detection, duty sweep up/down, step response, noise. Fit -> `fan.toml` + `report.html`; re-measure later to compare against the fan's baseline for wear. Runs on the rig stage 3 or a bench board. |
| P5 | Rack thermals: lumped zone model `C·dT/dt = P_in − k(airflow)·(T − T_amb)`; heat input constant, profile, seeded random or live; dual/triple curves; control-quality KPIs (overshoot, settling, error, mean RPM) |
| later | Lockstep time, CI job on an isolated, ephemeral self-hosted runner (no LAN reach, no secrets, label `sil`, separate from the HIL runner; fork PRs not a focus) |

### Simple web UI in P1 (minimum)

- Served locally by the simulator process (e.g. `http://localhost:8080`),
  live over WebSocket, no build step, no external CDN at runtime (stays local).
- Per fan port: chosen fan from the library (dropdown), duty, RPM, and a small
  live plot of duty and RPM.
- LEDs as virtual LEDs with their current state.
- Buttons: short and long press.
- Run info: time factor, sim time, firmware connected yes/no, "simulator
  behind real time" warning.
- Acceptance: everything the page shows or does is also reachable via the
  CLI/API with the same JSON (the page is just a client).

## Repo Split and Consumer Interface (decided)

| Repo | Contents |
|---|---|
| `esphome-sil` (this) | simulator core, sim-bus, the `sim_*` ESPHome external components, the host HAL time patch, CLI `sil`, web UI, fan library with photos, fault profiles, Lua models, `fan-analyze`, rack thermals |
| `wifi-fancontroller` | `hardware-sim-*.yaml` mapping to sim ids, SiL scenarios and tests for its modules, a pinned `esphome-sil` tag (`external_components: github://zeroflow/esphome-sil@vX.Y`) |
| `hil-rig` | unchanged; possibly the isolated SiL runner setup later |

Because the `sim_*` components live here, the sim-bus protocol is internal and
free to change. The consumer interface is only what the product repo touches,
documented as a README section and versioned with semver tags:

1. YAML interface of the `sim_*` components (names and config keys).
2. Test interface: scenario file format, JSON result of `sil run`, and the
   pytest fixture/plugin if tests are written in Python.
3. Time semantics: what the time factor means; all timestamps are in sim time.
4. Compatibility: which ESPHome version the time patch supports (initially
   exactly 2026.10.1, matching the product repo pin; a check enforces it).

Not part of it: sim-bus wire protocol, web UI, `fan.toml` schema (library
lives here), Lua API (scenarios reference fault profiles by name only).
A contract test here builds an example `hardware-sim.yaml` and plays an
example scenario. `0.x` while P1 settles; `1.0` once the product repo builds
real tests on it.

## Open Questions

- Reference revision for P1 (Rev 3.3?), other revisions later.
- Web stack, now needed in P1. Lean toward FastAPI with WebSocket plus a
  static page and a vendored plot library. The page needs no build
  step.
- CPU limit of the time factor: measure in P1. The simulator reports when it
  falls behind.
- `host:` has no arduino/esp-idf: SiL tests YAML logic and lambdas, not
  drivers or `framework:`. That stays the HIL's job.

## Breadcrumbs

- `wifi-fancontroller/modules/`: `rpm_pi_control.yaml`, `stall_guard.yaml`,
  `rpm_status_leds.yaml`, `usr_buttons.yaml`, `usr_buttons_mode.yaml`,
  `fallback_mode.yaml`, `temperature_curve*.yaml`, `dual_curve.yaml`.
  Sensor modules to replace with sim sensors: `ds2484_onewire`, `bme680`,
  `scd41`.
- `wifi-fancontroller/hardware-rev-3.3.yaml` and the others: the hardware
  packages that `hardware-sim.yaml` mirrors.
- `wifi-fancontroller/fallback_mode_harness_dev.yaml`: existing harness
  pattern (module + vars via `!include`).
- `wifi-fancontroller/hil/`: pytest/uv setup and fixtures to reuse.
- ESPHome 2026.10.1 image: `esphome/components/host/hal.cpp`,
  `esphome/core/application.cpp` (host select loop).

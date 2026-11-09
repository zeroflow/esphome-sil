# Requirements: esphome-sil

**Defined:** 2026-11-08
**Core Value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.

## v1 Requirements

Milestone 1: simulation running (ESPHome + linear fan model), interaction via Home Assistant, status visible in the web UI.

### Build & Run

- [ ] **BUILD-01**: Developer can build the example firmware from the working tree with `sil up`, using the pinned `esphome/esphome:2026.10.1` image (non-root `--user`, own `HOME`, no root-owned files left behind)
- [ ] **BUILD-02**: The firmware is linked statically (`esphome: build_flags: [-static]`) and runs directly on the host (Debian 12, glibc 2.36)
- [ ] **BUILD-03**: Build fails early with a clear message when the config uses components that do not work on `host:` (`wifi`, `ledc`, `pulse_counter`, `esp32_rmt_led_strip`, `i2c`, `ota`, `http_request`, `update`, `captive_portal`, `web_server`, `sntp`, `esp32`)
- [x] **BUILD-04**: Each run has its own run directory holding sim-bus socket, `ESPHOME_PREFDIR`, logs; runs start fresh by default (no preferences carried over)
- [ ] **BUILD-05**: Each run gets its own API port, a stable locally administered MAC and device name derived from the instance name, and a stable per-instance API encryption key; two runs can coexist without colliding
- [ ] **BUILD-06**: Firmware does not exit without an API client (`reboot_timeout: 0s`); `sil up` reports ready only after a real API handshake, and fails clearly if the port is taken
- [x] **BUILD-07**: Developer can stop a run with `sil down`; firmware and simulator processes never outlive the run (no orphans when the simulator dies)

### Sim-Bus & IO Components

- [ ] **BUS-01**: Firmware and simulator talk over a Unix domain socket in the run dir using newline-delimited flat JSON; simulator listens, firmware connects
- [ ] **BUS-02**: On every (re)connect both sides exchange HELLO (protocol version, channel manifest) and a full state snapshot; restarting either side recovers without manual steps
- [ ] **BUS-03**: The `sim_bus` hub component is non-blocking in the ESPHome loop, reconnects with backoff, and validates the socket path length at codegen
- [x] **BUS-04**: Wire format is pinned by golden vectors tested from both Go and C++
- [ ] **IO-01**: `sim_bus` float output platform delivers PWM duty for each of the 4 fan ports, so stock `fan: speed` works on top
- [ ] **IO-02**: `sim_bus` sensor platform delivers fan RPM per port as a 1 s windowed mean, published at `update_interval`
- [ ] **IO-03**: `sim_bus` binary_sensor platform delivers buttons USR1-3 (short and long press)
- [ ] **IO-04**: `sim_bus` addressable light platform carries the status LED strip so stock `light: partition` (board + per-fan status) works; frames are sent only on change and rate-capped
- [ ] **IO-05**: `sim_bus` sensor platform delivers temperature and humidity (HDC1080 replacement)
- [ ] **IO-06**: Every sim platform is bound by a required `channel:` key (e.g. `port1.duty`), independent of the ESPHome `id:`

### Plant Model

- [x] **MODEL-01**: Each fan port runs a linear duty→RPM model with min duty, start threshold (with hysteresis against stall), max RPM and first-order lag (exact exponential update)
- [x] **MODEL-02**: Fan parameters come from a `fan.toml` in the fan library; unknown keys are rejected; at least one fan ships in M1
- [ ] **MODEL-03**: Developer can choose which library fan is plugged into which port
- [x] **MODEL-04**: The simulator reads time only from an injectable clock and integrates by `dt`; sim-bus messages carry sim timestamps; a lint bans wall-clock calls outside the clock (time factor stays fixed at 1 in M1)

### Example Firmware

- [ ] **FW-01**: The repo ships an example top-level host config mirroring the Rev 3.3 IO surface (4 fans, LED strip with partitions, USR1-3, temp/humidity), without `wifi:` (uses `api`/`on_boot` hooks instead of `wifi.on_connect`)
- [ ] **FW-02**: Example entities use the HIL contract names (`Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`); status LEDs stay internal like the product
- [ ] **FW-03**: The example consumes the components via `external_components:` the same way the product repo will (local path now, `github://zeroflow/esphome-sil@vX.Y` later)

### CLI / JSON API

- [ ] **CLI-01**: Developer can get the full run status as JSON (`sil status`): instance, ports (fan, duty, RPM), LEDs, buttons, temp/humidity, sim time, firmware connected, API host/port/key
- [ ] **CLI-02**: Developer can set inputs via `sil set` (temperature, humidity, fan per port) and press buttons via `sil press usr1 [--long]`
- [ ] **CLI-03**: Developer can follow live events as NDJSON with `sil watch`
- [ ] **CLI-04**: Developer can read firmware and simulator logs of a run with `sil logs`
- [x] **CLI-05**: Every command can emit JSON and uses exit codes 0 ok, 1 command failed, 2 infrastructure error
- [x] **CLI-06**: CLI and web UI use one HTTP/JSON API with one operation vocabulary; the CLI is a client of it (except `up`/`down`)

### Web UI

- [ ] **UI-01**: The simulator serves a local web page (embedded in the binary, no build step, vendored libs, no runtime CDN), updated live over WebSocket
- [ ] **UI-02**: Per fan port the page shows chosen fan (selectable), duty, RPM and a live duty/RPM plot (bounded buffer)
- [ ] **UI-03**: The page shows virtual LEDs with their current color/state
- [ ] **UI-04**: The page offers short and long press for USR1-3 and setting temperature/humidity
- [ ] **UI-05**: The page shows run info: sim time, time factor, firmware connected, "simulator behind real time" warning
- [ ] **UI-06**: Everything the page shows or does is reachable via the CLI/API with the same JSON (verified by a parity check)

### Home Assistant

- [ ] **HA-01**: User can add a running sim instance to any Home Assistant (manual host:port + key from `sil status`); restarting the instance reuses the same HA device, no duplicates
- [ ] **HA-02**: From HA the user can control fan speed per port and see RPM, temperature, humidity and button states change accordingly

### Contract & Tests

- [ ] **TEST-01**: A contract test in this repo builds and runs the example firmware with the simulator and plays a short scripted interaction (duty → RPM settles, button press, temperature change)
- [ ] **TEST-02**: An aioesphomeapi smoke test checks the HA-visible entity set and a fan command round trip

## v2 Requirements

Deferred. Tracked but not in current roadmap.

### Run & Release

- **REL-01**: ESPHome version pin guard refusing versions other than the supported one
- **REL-02**: Semver tag v0.1.0 with consumer-interface README section
- **RUN-01**: Per-run event trace file
- **RUN-02**: `sil rebuild` in place without restarting the simulator
- **RUN-03**: `sil list` / instance registry and selection by name

### Time & Scenarios

- **TIME-01**: Configurable time factor (x1 … x100+) via host HAL patch (`hal.cpp`, `core/wake/wake_host.cpp::wakeable_delay`)
- **SCEN-01**: Scenario files (`sil run scenario.toml`) with JSON verdicts and metrics
- **SCEN-02**: pytest plugin/fixtures for consumer repos

### Later Phases (from idea doc)

- **FAULT-01**: Fault profiles, Lua, plug/unplug, virtual supervisor (P2)
- **UIX-01**: Web UI eye candy, fan photos, drag & drop (P3)
- **ANLZ-01**: `fan-analyze` companion tool (P4)
- **THERM-01**: Rack thermal model and control-quality KPIs (P5)
- **REAL-01**: Real wifi-fancontroller modules running on the sim; SiL YAML moves to the product repo

## Out of Scope

| Feature | Reason |
|---------|--------|
| Time factor in M1 | HA in the loop requires x1; planned for later |
| Pulse/edge-accurate tach, ISR simulation | Drivers are the HIL's job; sim delivers RPM |
| Drivers, framework, Wi-Fi, OTA, `update`, heap | Not possible on `host:`; stays HIL |
| Bundled / auto-configured Home Assistant | User points any HA at the sim; no coupling |
| Status LEDs exposed to HA | Product keeps them internal; visible in UI/CLI |
| UI-only capabilities | UI is a pure API client |
| Auth / remote access for web UI | Local bench only |
| File-watch auto-rebuild, always-on daemon | Explicit `sil up`/`down` lifecycle |
| Silent fallbacks (unknown `fan.toml` keys, wrong ESPHome) | Must fail loudly |
| Cloud simulators, QEMU | Local-only; no ESP32-S2 QEMU machine |
| Bit-deterministic runs | Needs lockstep time (later) |
| Revisions other than 3.3 | Later |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| BUILD-01 | Phase 1 | Pending |
| BUILD-02 | Phase 1 | Pending |
| BUILD-03 | Phase 1 | Pending |
| BUILD-04 | Phase 1 | Complete |
| BUILD-05 | Phase 1 | Pending |
| BUILD-06 | Phase 1 | Pending |
| BUILD-07 | Phase 1 | Complete |
| BUS-01 | Phase 1 | Pending |
| BUS-02 | Phase 1 | Pending |
| BUS-03 | Phase 1 | Pending |
| BUS-04 | Phase 1 | Complete |
| IO-01 | Phase 1 | Pending |
| IO-02 | Phase 1 | Pending |
| IO-03 | Phase 2 | Pending |
| IO-04 | Phase 2 | Pending |
| IO-05 | Phase 2 | Pending |
| IO-06 | Phase 1 | Pending |
| MODEL-01 | Phase 1 | Complete |
| MODEL-02 | Phase 1 | Complete |
| MODEL-03 | Phase 2 | Pending |
| MODEL-04 | Phase 1 | Complete |
| FW-01 | Phase 2 | Pending |
| FW-02 | Phase 2 | Pending |
| FW-03 | Phase 2 | Pending |
| CLI-01 | Phase 2 | Pending |
| CLI-02 | Phase 2 | Pending |
| CLI-03 | Phase 2 | Pending |
| CLI-04 | Phase 2 | Pending |
| CLI-05 | Phase 1 | Complete |
| CLI-06 | Phase 1 | Complete |
| UI-01 | Phase 3 | Pending |
| UI-02 | Phase 3 | Pending |
| UI-03 | Phase 3 | Pending |
| UI-04 | Phase 3 | Pending |
| UI-05 | Phase 3 | Pending |
| UI-06 | Phase 3 | Pending |
| HA-01 | Phase 2 | Pending |
| HA-02 | Phase 2 | Pending |
| TEST-01 | Phase 2 | Pending |
| TEST-02 | Phase 2 | Pending |

**Coverage:**
- v1 requirements: 40 total
- Mapped to phases: 40
- Unmapped: 0 ✓

---
*Requirements defined: 2026-11-08*
*Last updated: 2026-11-08 after roadmap creation*

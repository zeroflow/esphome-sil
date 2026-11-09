# Roadmap: esphome-sil

## Overview

Milestone 1 proves the plumbing end to end in three vertical slices. Phase 1 is a walking skeleton through every risky layer at once: static `host:` build in the pinned ESPHome image, per-instance run isolation, the sim-bus in C++ and Go, the fan model behind an injectable clock, and `sil up/status/down` on the HTTP/JSON API. Fan duty from real firmware turns into simulated RPM that flows back. Phase 2 widens that skeleton to the full Rev 3.3 IO surface (buttons, LED strip with `partition`, temperature/humidity) with the complete CLI vocabulary and Home Assistant in the loop, guarded by the contract and aioesphomeapi smoke tests. Phase 3 adds the live web UI as a pure client of the same API, with a parity check that keeps it that way.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Closed-Loop Fan Skeleton** - `sil up` builds static host firmware whose fan duty drives simulated fans over the sim-bus, with RPM flowing back; isolated, resilient, stoppable
- [ ] **Phase 2: Full Rev 3.3 Bench** - Buttons, LED strip, temperature/humidity, full CLI vocabulary and Home Assistant control on the Rev 3.3 example, guarded by contract tests
- [ ] **Phase 3: Live Web UI** - Embedded local page with live per-port plots, virtual LEDs, button and environment controls, and run info, all through the same API as the CLI

## Phase Details

### Phase 1: Closed-Loop Fan Skeleton

**Goal**: Developer runs `sil up` and real ESPHome firmware, built in the pinned image and running directly on the host, drives simulated fans whose RPM flows back over the sim-bus; the loop is observable via `sil status`, survives restarts of either side, and stops cleanly with `sil down`
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: BUILD-01, BUILD-02, BUILD-03, BUILD-04, BUILD-05, BUILD-06, BUILD-07, BUS-01, BUS-02, BUS-03, BUS-04, IO-01, IO-02, IO-06, MODEL-01, MODEL-02, MODEL-04, CLI-05, CLI-06
**Success Criteria** (what must be TRUE):
  1. On a clean checkout, `sil up <name>` builds the example firmware in the pinned `esphome/esphome:2026.10.1` image as a statically linked binary, runs it directly on the host next to the simulator, and reports ready only after a real encrypted API handshake. The firmware keeps running with no API client attached, and no root-owned files are left behind.
  2. Using the key from `sil status` (served by the simulator's HTTP/JSON API), an API client such as a short aioesphomeapi script sets the speed of any of the four fans. `sil status` shows the new duty, and the RPM the firmware reports (1 s mean) rises toward the shipped `fan.toml` target with first-order lag, staying at 0 below the start threshold. The model advances only through the injectable clock, and a lint fails on any wall-clock call outside it.
  3. Restarting the simulator or the firmware, in either order, re-syncs full state without manual steps. The firmware's API stays responsive while the simulator is gone, and the wire format passes the same golden vectors in both the Go and the C++ tests.
  4. Two named instances run side by side, each with its own run dir, API port, MAC, device name and key. `sil up` on a taken port fails with exit code 2 and a JSON error. `sil down` stops both processes, killing the simulator leaves no orphaned firmware, and each new run starts with fresh preferences.
  5. Misconfigurations fail loudly before anything runs: a host-incompatible component (e.g. `wifi:`, `ledc`, `i2c`), a sim platform without `channel:`, an over-long sim-bus socket path, or an unknown key in `fan.toml` each stop the build or start with a message naming the problem.

**Plans:** 13/13 plans executed

Plans:
**Wave 1**
- [x] 01-01-PLAN.md — Walking skeleton link 1 (tracer): repo sim_bus component + example build statically in the pinned image, run on the host, duty/RPM cross the sim-bus on four ports via the encrypted API (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 01-02-PLAN.md — Walking skeleton link 2 (tracer): Go engine + fan model + fan library close the loop with real firmware; exact duty decimals; wall-clock lint; pinned, provenance-checked modules (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 01-03-PLAN.md — Walking skeleton link 3 (tracer): `sil up` -> detached supervisor -> `sil status` over api.sock -> `sil down`; four ports; CLI error JSON; e2e Env helpers (wave 3)
- [x] 01-04-PLAN.md — Manual clock and deterministic engine tests (exact step composition, last-wins, race-free snapshots) (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 01-05-PLAN.md — Fan model (hysteresis, below_min, asymmetric lag) and strict fan.toml library with located errors (wave 4)
- [x] 01-06-PLAN.md — Sim-bus wire format: PROTOCOL.md, golden vectors, strict Go codec and header-only C++ codec (wave 4)
- [x] 01-07-PLAN.md — Firmware IO platforms: channel/path codegen validation, USE_* guards, output dedupe, 1 s RPM mean (wave 4)
- [x] 01-08-PLAN.md — Build guards: image preflight, two-stage lint, identity assertions, build-hash skip, toolcache, static-ELF check (wave 4)
- [x] 01-09-PLAN.md — Isolated instances: names, XDG paths, sticky ports, locks, fresh run dirs, real Noise handshake readiness (wave 4)

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 01-10-PLAN.md — Resync after restarts: HELLO manifest/snapshot session both sides, `sil sim restart [--gap]`, `sil fw restart` (wave 5)

**Wave 6** *(blocked on Wave 5 completion)*
- [x] 01-11-PLAN.md — Supervised firmware: auto-restart, crash-loop guard, orphan-proof shutdown, `sil down` fallback (wave 6)
- [x] 01-12-PLAN.md — CLI contract: instance resolution, one JSON value per call, `--quiet`, client-only commands (wave 6)

**Wave 7** *(blocked on Wave 6 completion)*
- [x] 01-13-PLAN.md — Exit-class registry, dependency tidy and the full Phase 1 acceptance run (wave 7)

### Phase 2: Full Rev 3.3 Bench

**Goal**: The complete Rev 3.3 IO surface runs in the sim and can be driven both from the CLI and from the user's own Home Assistant, with a contract test that proves the whole loop
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: IO-03, IO-04, IO-05, FW-01, FW-02, FW-03, MODEL-03, CLI-01, CLI-02, CLI-03, CLI-04, HA-01, HA-02, TEST-01, TEST-02
**Success Criteria** (what must be TRUE):
  1. The shipped example host config mirrors Rev 3.3 (4 fans, LED strip with board and per-fan `partition` lights, USR1-3, temperature/humidity). It compiles without `wifi:`, pulls the components in through `external_components:` the same way the product repo will, and exposes the HIL contract entity names (`Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`) with status LEDs kept internal.
  2. `sil status` returns the full run status as JSON (ports with fan/duty/RPM, LED colors, buttons, temperature/humidity, sim time, firmware connected, API host/port/key). `sil set` changes temperature, humidity and which library fan is plugged into a port; `sil press usr1 [--long]` produces a short or long press the firmware reacts to; `sil watch` streams the resulting changes as NDJSON; `sil logs` shows firmware and simulator logs.
  3. LED changes made by the firmware, including `partition` lights and effects such as `pulse`, appear with correct colors in `sil status` and `sil watch`. An idle firmware sends LED frames only on change and at a capped rate.
  4. The user adds a running instance to any Home Assistant using host:port and key from `sil status`, controls fan speed per port, and sees RPM, temperature, humidity and button states follow. Restarting the instance reuses the same HA device with no duplicates, and a second instance appears as a separate device.
  5. One command runs the contract test (builds and runs the example with the simulator; duty to RPM settles, a button press registers, a temperature change arrives) and the aioesphomeapi smoke test (HA-visible entity set, fan command round trip), and both pass.

**Plans:** 4/8 plans executed

Plans:
**Wave 1**
- [x] 02-01-PLAN.md — Tracer: `sil press` -> sim_bus binary_sensor -> firmware on_click -> fan duty -> `sil status`; examples/rev33.yaml (fans, RPM, USR1-3); full D-01..D-04 press vocabulary (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 02-02-PLAN.md — LED strip: sim_bus addressable light under stock `partition` lights, codegen-derived segments, dedupe + 20 Hz cap + replay, `leds` in `sil status`, D-13 board/fan LED logic (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 02-03-PLAN.md — Temperature/humidity (`mode: latest`), supervisor-held inputs, atomic `sil set`, `env` in status, exact HIL entity set (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 02-04-PLAN.md — Phase 2 wire contract in PROTOCOL.md and golden vectors (Go + C++); LED and cross-domain channel misconfigurations fail at config time (wave 4)
- [ ] 02-05-PLAN.md — Fan per port: `sil set portN.fan=` replug, `sil up --fan`, `sil fans`, second shipped fan (Noctua, ASSUMED constants) (wave 4)

**Wave 5** *(blocked on Wave 4 completion)*
- [ ] 02-06-PLAN.md — `sil watch` (supervisor event broker, snapshot-then-delta NDJSON) and `sil logs` (tail, follow, raw, redaction) (wave 5)

**Wave 6** *(blocked on Wave 5 completion)*
- [ ] 02-07-PLAN.md — pytest under tests/ (uv, lock hash-gated against the product HIL lock): TEST-02 smoke, TEST-01 contract chain, HA-01 identity proxy, `make test-contract` (wave 6)

**Wave 7** *(blocked on Wave 6 completion)*
- [ ] 02-08-PLAN.md — HA reachability (`api.lan_host`, README HA guide), Rev 3.3 as the default bench, docs, full Phase 1 + 2 acceptance run (wave 7)

### Phase 3: Live Web UI

**Goal**: The developer watches and drives a running bench from a local web page that is a pure client of the same API the CLI uses
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: UI-01, UI-02, UI-03, UI-04, UI-05, UI-06
**Success Criteria** (what must be TRUE):
  1. Opening the simulator's local URL shows a page served from the `sil` binary that keeps working with the network blocked (no build step, vendored libs, no runtime CDN) and updates live over WebSocket.
  2. For each fan port the page shows the plugged-in fan (changeable from the page), duty, RPM and a live duty/RPM plot whose history stays bounded during a long run.
  3. The page shows the virtual LEDs with their current colors, offers short and long press for USR1-3 and setting temperature/humidity, and the firmware (and HA, if connected) reacts exactly as it does to the same CLI commands.
  4. The page shows sim time, the time factor (fixed at 1), whether the firmware is connected, and a warning when the simulator falls behind real time.
  5. An automated parity check confirms that every value the page displays and every action it performs is available through the CLI/API with the same JSON.

**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Closed-Loop Fan Skeleton | 13/13 | In Progress|  |
| 2. Full Rev 3.3 Bench | 4/8 | In Progress|  |
| 3. Live Web UI | 0/TBD | Not started | - |

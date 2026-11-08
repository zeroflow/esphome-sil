# Project Research Summary

**Project:** esphome-sil
**Domain:** Software-in-the-loop test bench ("labcar") for ESPHome firmware: ESPHome `host:` binary + `sim_*` external components + Go plant simulator + CLI/JSON API + embedded web UI, Home Assistant via native API
**Researched:** 2026-11-08
**Confidence:** MEDIUM-HIGH (HIGH for ESPHome/build findings reproduced against the pinned image; MEDIUM for Go internals, protocol, UI and HA behaviour read from source only)

## Executive Summary

esphome-sil is a local closed-loop test bench: real ESPHome firmware compiled for the `host:` platform, with simulated IO components that talk over a local socket to a plant simulator. Research built and ran a working prototype against the pinned `esphome/esphome:2026.10.1` image (hub + float output + sensor + binary sensor + addressable light, with the stock `fan: speed` and `light: partition` running on top, driven from aioesphomeapi). The approach is feasible; the risk is concentrated in the build/run foundation, not in the simulator.

Recommended approach: build in the pinned image with `esphome: build_flags: [-static]`, run the static binary directly on the host (the dynamic binary needs GLIBC 2.38 / GLIBCXX 3.4.31 and will not start on this Debian 12 host). Sim-bus is a Unix domain socket carrying newline-delimited flat JSON, simulator listens, firmware connects, state-based and self-resyncing (HELLO with manifest + full snapshot on every connect). The Go simulator is a single-goroutine engine actor with an injectable clock (`time.Now` banned outside it), pure dt-based fan model, a transport-neutral Control interface with HTTP adapter, CLI as HTTP client, and a vanilla-JS + uPlot page embedded in the binary.

Main risks: `host:` platform gaps (`wifi:`, `ledc`, `esp32_rmt_led_strip`, `i2c`, `ota`, `http_request`, `update`, `captive_portal`, `web_server`, `sntp` unavailable; `wifi:` passes `config` but crashes codegen), HA identity collisions (constant default MAC, manual add re-points existing entries), state leakage via persisted preferences, and the API's default 15 min `reboot_timeout` silently exiting the firmware. All have concrete mitigations below.

## Key Findings

### Recommended Stack

Go simulator in a single static binary, ESPHome firmware built in the pinned image. See [STACK.md](STACK.md).

**Core technologies:**
- Go 1.27.3: simulator, CLI, HTTP/WS server — static binary, `embed`, fast agent iteration loop. **Not yet installed on this host** (checksum in STACK.md).
- kong v1.17.0: CLI — struct-tag based, maps cleanly onto JSON operation structs.
- stdlib `net/http` ServeMux (1.22+ patterns): routing — no router dependency needed.
- coder/websocket v1.8.15: live push to UI — context-aware, maintained (gorilla is not preferred).
- pelletier/go-toml/v2 v2.4.3 with `DisallowUnknownFields`: `fan.toml` — strict, no silent ignores.
- `log/slog`: structured logging.
- gopher-lua v1.1.2: P2 only, one-day spike vs alternatives then.
- uPlot 1.6.32 (vendored, sha256 recorded): live plots. UI in vanilla JS for M1 (Alpine 3.17.5 was STACK's suggestion; vanilla recommended by ARCHITECTURE/FEATURES; avoid htmx — it creates a second surface the CLI cannot reach).
- ESPHome 2026.10.1 image `ghcr.io/esphome/esphome:2026.10.1`: firmware build, run with `--user` and own `HOME` to avoid root-owned `.esphome/`. Use `esphome: build_flags:` (the `platformio_options.build_flags` key is deprecated, removed in 2026.12.0).
- No ESPHome `json:` component on the C++ side: it pulls ArduinoJson from the PlatformIO registry and breaks hermetic (`--network none`) builds; use a small hand-written flat-JSON parser.

### Expected Features

See [FEATURES.md](FEATURES.md).

**Must have (table stakes, M1):**
- `sil up` / `down` / rebuild from working tree; firmware + simulator lifecycle with logs captured
- Status as JSON; `sil set` (inputs, fan params) and `sil press usr1 [--long]`
- Per-instance isolation: state dir, `ESPHOME_PREFDIR`, API port, stable locally administered MAC, name, API key; fresh-by-default start (`--fresh` semantics)
- Fan model params from `fan.toml`
- Web UI as pure client: per port duty/RPM/plot, virtual LEDs incl. color, button short/long press, run info (sim time, firmware connected)
- HA reachability: user adds host:port manually (no mDNS on host); entity names match the HIL contract (`Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`) so aioesphomeapi fixtures can be reused
- Exit codes and JSON errors: 0 ok, 1 command failed, 2 infrastructure

**Should have (differentiators, cheap in M1):**
- `sil watch` NDJSON event stream (reuses the WS feed)
- Event trace file per run (seed for later scenarios)
- One shared operation vocabulary for CLI, UI and the later scenario engine

**Defer:**
- Scenario files and JSON verdicts, time factor, fault models/Lua, `sil list`/registry UX, bit-deterministic runs (needs lockstep)

**Anti-features:** pulse-accurate tach/ISR simulation, generic HW emulator, UI-only capabilities, bundled/auto-configured HA, file-watch auto-rebuild, always-on daemon, UI auth/remote access, silent fallbacks (unknown `fan.toml` keys, mismatched ESPHome version).

### Architecture Approach

Three processes: firmware (`host:` binary with `sim_*` components), Go simulator (bus server, engine, API, web), and clients (CLI, web page, HA, pytest). Signals are levels, not events; the simulator is source of truth for plant outputs (RPM, buttons, temperature/humidity), the firmware for actuator commands (duty, LED frames). See [ARCHITECTURE.md](ARCHITECTURE.md).

**Major components:**
1. `sim_bus` hub (C++): non-blocking Unix-socket client in `loop()`, `setup_priority::BUS`, drains to `EAGAIN`, reconnects, optional `wake_register_fd()` wakeup; socket path overridable via `SIL_BUS` env; codegen validates path length (107-byte `sun_path` limit).
2. `sim_*` platforms: float output (duty), `PollingComponent` sensor (RPM/temp/humidity at `update_interval`), binary_sensor (buttons), addressable light (so `partition` works) with change detection + rate cap. Each has a required `channel:` key (e.g. `port1.duty`) independent of `id:`; names exchanged once in the HELLO manifest.
3. Go engine: single-goroutine actor owning all state, immutable snapshots out, `Clock` interface, sim-time timer queue and empty per-port hook lists as seams for time factor and Lua faults.
4. Control interface + HTTP adapter; CLI as HTTP client (except `up`/`down`); WS hub for UI and `sil watch`.
5. Lifecycle manager: Docker build (`--user`, own `HOME`), run static binary with `Pdeathsig`, per-run dir (socket, prefs, logs), health check = real API handshake.
6. Repo layout: `components/` at repo root (so `github://zeroflow/esphome-sil@vX.Y` works without `path:`), `sim_protocol.h` inside `components/sim_bus/`, tests outside `components/`, `protocol/vectors/` golden vectors shared by Go and C++ tests, `fans/`, `examples/`.

### Critical Pitfalls

See [PITFALLS.md](PITFALLS.md) (24 pitfalls, phase-mapped).

1. **Dynamic binary won't run on host** — static link via `build_flags: [-static]`; fallback: run binary in image with bind-mounted run dir.
2. **`esphome config` passing ≠ working** — `wifi:` crashes codegen, `pulse_counter`/`gpio` compile but never change on host. Lint that rejects banned components; contract test must compile and run, not just validate.
3. **API `reboot_timeout` 15 min → firmware exits code 0 with no client** — set `reboot_timeout: 0s`; API binds `0.0.0.0` and a busy port only logs `errno 98` → health check via real handshake, per-instance ports.
4. **HA identity collisions** — constant default MAC; manual add with existing MAC re-points the entry. Stable per-instance locally administered MAC, distinct name, own key.
5. **Persisted prefs make runs unrepeatable** (and leak into `~/.esphome`) — always set `ESPHOME_PREFDIR` per run, fresh by default; the product's `init_done` first-boot path depends on it.
6. **Wall-clock coupling blocks the time factor** — lint banning `time.Now`/`Sleep`/`Ticker` outside the clock; exact exponential lag update; hysteresis between start threshold and stall.
7. **LED frame flood** (66 identical frames in 5 s idle) — change detection + rate cap in `sim_led_strip`.

## Implications for Roadmap

Suggested phase structure (coarse granularity):

### Phase 1: Toolchain and wire proof
**Rationale:** highest risk sits here (static build, Docker wrapper, host gaps, socket).
**Delivers:** static host build in the pinned image, banned-component lint, run dir isolation (prefs/port/MAC/key), `reboot_timeout: 0s`, process supervision; sim-bus protocol with HELLO/snapshot, Go bus stub, C++ `sim_bus` + one output + one sensor round trip; golden vectors.
**Avoids:** pitfalls 1-5.

### Phase 2: Engine and fan model
**Rationale:** pure Go, can overlap Phase 1.
**Delivers:** Clock interface + lint, engine actor, linear + first-order-lag fan model, strict `fan.toml`.
**Avoids:** pitfall 6.

### Phase 3: Full Rev 3.3 IO surface + example firmware
**Delivers:** 4 ports, buttons, sim addressable light with `partition` working, temp/humidity, example top-level host YAML (no `wifi:`; `api.on_client_connected`/`on_boot` instead of `wifi.on_connect`), HIL-compatible entity names, HA manual-add check.
**Avoids:** pitfall 7; validates partition-on-sim-light at runtime.

### Phase 4: CLI/JSON API and lifecycle
**Delivers:** Control interface, HTTP adapter, kong CLI (`up/down/status/set/press/watch`), exit-code convention, logs, event trace.

### Phase 5: Web UI
**Delivers:** embedded vanilla JS + uPlot page, WS push with backpressure and bounded plot buffers, parity check that every UI action is an API call.

### Phase 6: Contract test, pin guard, release v0.1.0
**Delivers:** contract test that builds and runs the example, plays a short scripted interaction, aioesphomeapi smoke test; ESPHome version pin check; consumer-interface README section; tag.

### Phase Ordering Rationale

- Foundation first because every later piece runs on the static build and the bus.
- Engine is independent and can be parallel to Phase 1.
- Status JSON / API must precede the UI (UI is only a client); instance isolation must precede HA.
- With coarse granularity, Phases 1+2, and 4+5 are natural merge candidates for the roadmapper.

### Research Flags

Needs deeper research during planning:
- **Phase 1:** static build with `api: encryption:` (Noise libs fetched at build time; hermetic build cache), same-path bind mounts for local external components, incremental rebuild timings.
- **Phase 3:** `partition` + `pulse` effects at runtime on the sim addressable light; LED frame rate.
- **Phase 6:** live HA behaviour with the stable-MAC scheme.

Standard patterns (research optional):
- **Phase 2, 4, 5:** Go engine/CLI/UI follow well-known patterns.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions from Go module proxy, go.dev, npm; ESPHome side built and run in pinned image |
| Features | MEDIUM-HIGH | Derived from PROJECT, idea doc, Rev 3.1/3.3 YAML, HIL contract files; prior art (Wokwi, Renode) from docs |
| Architecture | HIGH (ESPHome side) / MEDIUM (Go internals, protocol) | ESPHome integration from 2026.10.1 source + Docker spikes |
| Pitfalls | HIGH (host/build) / MEDIUM (bus, model, UI, HA) | Host matrix reproduced; HA from integration source, no live HA run |

**Overall confidence:** MEDIUM-HIGH

### Gaps to Address — decisions for the maintainer

1. **Where the firmware runs.** ARCHITECTURE: inside the image (bind-mounted run dir). STACK + PITFALLS: static binary on host (verified, 3.4-5.5 MB). **Recommendation:** static on host as primary, run-in-image as fallback if later product modules break static linking.
2. **Bus wire format.** ARCHITECTURE proposed binary frames, STACK NDJSON. **Recommendation:** NDJSON (debuggable, protocol is internal).
3. **`sim_*` layout.** Separate component dirs (ARCHITECTURE, idea doc naming) vs one `sim_bus` dir with platforms (STACK). **Recommendation:** one dir; base classes are identical either way.
4. **Tach semantics.** RPM over the wire with `sim_sensor` doing a 1 s windowed mean (PROJECT decision) vs pulses/min keeping the product's `multiply: 0.5` filter (PITFALLS). Affects `fan.toml` and the P2 wrong-ppr fault.
5. **API encryption.** Plaintext acceptable (STACK, FEATURES) vs always-on with stable per-instance key (PITFALLS). **Recommendation:** encryption on, pending the Phase 1 static-build spike.
6. **LEDs in HA.** Product marks `partition` lights `internal: True`. **Recommendation:** expose them in the example firmware.
7. **Multi-instance depth in M1.** **Recommendation:** isolate everything per instance now; defer `sil list`/registry UX.
8. **Correction to idea doc/PROJECT:** host `select()` timeout lives in `core/wake/wake_host.cpp::wakeable_delay` in 2026.10.1, not `core/application.cpp`.
9. **Static linking with real product modules** (DNS, `json:`) is unverified; fallback above.

## Sources

### Primary (HIGH confidence)
- `ghcr.io/esphome/esphome:2026.10.1` image and its source tree — host platform matrix, build flags, glibc requirements, prefs, API behaviour (built and run)
- Prototype in session scratchpad (`proto/components/sim_bus/`) — round trip firmware ↔ peer ↔ aioesphomeapi
- Go module proxy, go.dev, npm registry, Context7 — library versions
- `wifi-fancontroller/hardware-rev-3.1.yaml`, `fancontroller-rev3.3-esp32s2.yaml`, `hil/` — reference IO and entity contract

### Secondary (MEDIUM confidence)
- Home Assistant ESPHome integration source — MAC-keyed identity, manual add behaviour
- Wokwi scenario and Renode tester docs — scenario/assertion prior art

### Tertiary (LOW confidence)
- General automotive labcar conventions — not re-verified

---
*Research completed: 2026-11-08*
*Ready for roadmap: yes*

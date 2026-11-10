# Feature Research

**Domain:** Local software-in-the-loop (SiL) test bench ("labcar") for ESPHome firmware on the `host:` platform; first domain is the WiFi Fan Controller (Rev 3.3). Users are one developer and coding agents.
**Researched:** 2026-11-08
**Scope:** Milestone 1 ("the plumbing works end to end") per `.planning/PROJECT.md`. Later-milestone features are listed only so M1 does not close doors on them.
**Confidence:** MEDIUM-HIGH. Project facts, the ESPHome host-platform docs and the Rev 3.1/3.3 hardware package were read directly (HIGH). Renode and Wokwi feature sets come from their docs pages (MEDIUM). Automotive labcar/HIL feature conventions come from general domain knowledge (MEDIUM, not re-verified).

## How to Read This

- "M1" means in Milestone 1 Active scope. "Later" means PROJECT.md Out of Scope or the P2-P5 phase sketch in `docs/initial-idea.md`.
- Complexity is for a Go simulator plus the `sim_*` ESPHome external components, with Claude Code as the implementer.
- The one structural rule that shapes many rows below: **the CLI/JSON API is the only product surface; the web UI and the future scenario engine are clients of the same command vocabulary.** Every feature is specified as "operation + JSON shape" first.

## Feature Landscape

### Table Stakes (Users Expect These)

Missing any of these and the bench is not usable for its stated purpose (closed-loop firmware check in seconds, no hardware, no cloud, drivable by an agent).

#### Lifecycle

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| `sil up` builds firmware from the working tree, starts simulator + firmware, blocks until ready, prints JSON | The bench's whole promise is "edit YAML, run, observe". Readiness (firmware connected on the sim-bus and API port accepting) must be a hard gate, otherwise agents race the boot. | MEDIUM | Wraps `esphome compile` in the pinned `esphome/esphome:2026.10.1` image against the working tree. Output JSON: instance name, pid(s), API host/port, web URL, MAC, state dir. `--no-build` to reuse the binary. Open item from PROJECT.md: whether the host binary built in the container runs on the host (glibc) or must also run in the container. If in container, port mapping and the sim-bus socket mount become part of `up`. |
| `sil down` (clean stop of firmware + simulator, remove pidfile/socket) | Agents leave orphans otherwise; ports then block the next `up`. | LOW | SIGTERM, grace period, SIGKILL. Idempotent: `down` on a stopped instance exits 0. |
| `sil rebuild` (= build + restart, one command) | The inner loop. An agent edits a lambda and wants one command. | LOW | Reuses `up` code path. Skip the compile when the config + component hash is unchanged. ESPHome's ninja build is incremental, so most rebuilds are seconds. |
| Per-instance state directory (`.sil/<name>/`: prefs dir, logs, socket, instance.json) | Host-platform firmware persists preferences to `ESPHOME_PREFDIR` (default `~/.esphome/prefs`, keyed by device name). Without isolation, runs leak state into each other and into other host-platform devices. | LOW | Set `ESPHOME_PREFDIR` per instance. Matters concretely: the Rev 3.1 package has `init_done` as a `restore_value` global and `RESTORE_DEFAULT_ON` fans, so boot behaviour depends on prefs. |
| Fresh state by default, opt-in to keep (`up --keep-state`) | Repeatable runs need a known start. Persistence tests (reboot with restored fan state) need the opposite. | LOW | Default fresh. |
| Crash/exit detection of the firmware process | Host binaries can segfault or `exit()`; a "connected: false" with no reason wastes agent turns. | LOW | Status JSON carries `firmware.exited` with exit code/signal and last log lines. |
| ESPHome version check (exactly 2026.10.1) | Required by the Constraints. The image is pinned, but a mismatch between image and any local override must fail loudly, not compile silently. | LOW | Check at `up`; hard error with a clear JSON error. Later also guards the time-factor patch. |
| Non-zero exit codes + JSON error objects | Agents branch on exit codes. | LOW | Convention: 0 ok, 1 command failed (bad arg, no such port), 2 infrastructure (build failed, firmware died, timeout). Errors are JSON on stdout with `error.code`, `error.message`. |

#### Status and introspection

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| `sil status` emits one complete JSON snapshot | Core agent observability. | LOW | Versioned (`"schema": 1`). Content: instance info; run info (`sim_time_s`, `clock: "realtime"`, `time_factor: 1`, `firmware.connected`, `firmware.exited`); `ports[1..4]` (`duty`, `rpm`, `target_rpm`, `fan` name, model state `stalled/spinning`); `leds[0..4]` (`on`, `rgb`, `brightness`, `effect` if known); `buttons`; `temperature`, `humidity`. |
| Web UI and CLI show identical JSON | Acceptance rule in `docs/initial-idea.md`: the page is only a client. | LOW if enforced early | WS pushes the same snapshot struct, plus deltas. One Go type, two encoders. Cheap to do on day one, expensive to retrofit. |
| `sil watch` (NDJSON stream of state changes with sim timestamps) | Agents cannot poll a 1 s UI; the WebSocket stream already exists for the UI, so exposing it on the CLI is nearly free. | LOW | Same event stream as the UI. This is also the seed for the later scenario engine's "wait until" conditions. |

#### Setting inputs

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| `sil press usr1\|usr2\|usr3 [--long]` | Stated in PROJECT.md. Buttons drive the firmware's user-facing logic. | LOW | Short and long durations are configurable and measured in sim time (defaults e.g. 100 ms / 1500 ms; check what the product `usr_buttons` module treats as long). Blocks until release by default, `--no-wait` to return early. |
| `sil hold` / `sil release` primitives | Needed to test double press, chords, and hold-while-doing-something; `press` is just hold + sim-time delay + release. | LOW | Build `press` on these. |
| `sil set temperature <C>` and `sil set humidity <%>` | The HDC1080 replacement is an input the firmware reacts to (temperature curves). Without it the sensor is dead data. | LOW | Plain value injection in M1. Ramps/profiles are scenario/thermal territory (later). |
| Fan-to-port assignment from the fan library (`fan.toml` name per port) | "Plant model params from `fan.toml`" has no use if the user cannot pick which fan sits on which port. | LOW | `sil up --fan 1=generic-120` or a small `sil.toml` per run; `sil set port2 fan=<name>` for runtime swap is optional in M1 (swap is closer to plug/unplug, which is P2). Ship 1-2 example fans. |

#### Plant model

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Linear duty->RPM with first-order lag, per port | Project decision. | LOW-MEDIUM | `rpm_target = max_rpm * f(duty)` where f is 0 below `min_duty`, and linear from `min_duty` to 100% up to `max_rpm`. `dRPM/dt = (target - rpm)/tau`. Integrate with the injected `dt`, never wall-clock reads inside the model. Exponential (exact) update form `rpm += (target-rpm)*(1-exp(-dt/tau))` is stable for any `dt`, which matters when the time factor arrives. |
| Start threshold with hysteresis (stopped fan needs `start_threshold` duty to begin spinning; running fan keeps spinning down to `min_duty`) | Real PC/server fans behave this way and the product's `stall_guard` and `rpm_pi_control` modules will exist precisely because of it. Cheap to include now, awkward to bolt on after the model API is fixed. | LOW | Two parameters already in the `fan.toml` field list. Model state: `spinning` bool. |
| `fan.toml` per fan with strict schema validation | Agents write these files. Unknown keys silently ignored = silent wrong results. | LOW | Fields M1: `name`, `ppr` (informational only, no pulse simulation), `min_duty`, `start_threshold`, `max_rpm`, `inertia`/`tau_s`. `noise` accepted, must be 0 or absent in M1. Reject unknown keys and out-of-range values with file/line errors. Schema is internal (not part of the consumer interface), so it may change freely during 0.x. |
| Four independent fan ports mirroring Rev 3.3 (duty from PWM out, RPM back as a value) | Rev 3.3 IO surface; no pulse counting. | LOW | RPM is delivered to the `sim_*` tach component as a float value, which the firmware sees where `pulse_counter` would report RPM. Keep the existing `multiply: 0.5` filter out of the sim path; the sim component outputs RPM directly. |

#### Web UI (minimum)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Per-port panel: fan name, duty, RPM numeric readout | Stated minimum. | LOW | Plain HTML + a small JS file, `embed`-ed in the Go binary. |
| Per-port live plot of duty and RPM (sim-time x axis, rolling ~60 s window) | The first-order lag and later PI-loop behaviour are only understandable as curves. | MEDIUM | Vendored uPlot; two series on two y-axes (duty 0-100 %, RPM 0-max). The plot must tolerate WS gaps and keep a client-side ring buffer. Server sends a short history on connect so a page reload does not blank the plot. |
| Virtual LEDs with color and brightness | Stated minimum. Status LEDs carry the firmware's user feedback (boot blue-pulse, wifi green, per-fan status). | MEDIUM | 5 LEDs (board + fan 1-4). Render the RGB the firmware wrote to the strip. Effects such as `pulse` run in firmware on `host:`, so the UI only displays pixels; no effect logic in the UI. Apply brightness so a dim "idle green" (0.1) is visibly dimmer. Consider that WS2812 output is perceptually non-linear; a simple gamma on display is enough. |
| Buttons USR1-3 with short press and long press | Stated minimum. | LOW | Two buttons per USR, or press-and-hold with a threshold. Calls the same `press` operation as the CLI. |
| Run info: sim time, firmware connected yes/no, time factor (shows "x1"), instance name/API port | Stated minimum. Without "firmware connected" a dead bench looks like a quiet one. | LOW | The "simulator behind real time" warning from the initial idea is meaningless at x1 with a wall-clock-driven sim; render the field now (hidden or "ok") so the layout does not change when the time factor arrives. |
| Reconnect-tolerant WebSocket (auto-reconnect, full snapshot on connect) | Rebuilds restart the simulator process; the open tab must recover without a manual refresh. | LOW | Snapshot-then-deltas. |
| Temperature/humidity input fields | Parity with `sil set`; trivially small. | LOW | Optional for M1 acceptance (not in the PROJECT.md UI list) but nearly free once the operation exists. |
| No build step, no CDN at runtime, localhost by default | Constraint. | LOW | Vendor uPlot and any CSS into the repo. Bind the web server to 127.0.0.1 by default. |

#### Home Assistant integration

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Firmware exposes the ESPHome native API on a configurable per-instance port | This is what "controllable via Home Assistant" means. The `host:` platform supports `api:` natively. | LOW | Default port 6053 collides with the HIL supervisor port (6053 in `hil/contract.py`) and with any other host-platform device, so `sil up` chooses a free port (starting at 6053 is fine) and prints it. |
| Manual-add instructions printed by `up` and returned in JSON (`ha: {host, port}`) | The host platform has no working mDNS, and Home Assistant does not discover host-platform instances; the user adds the device by IP and port. | LOW | Print the host's LAN IP, not 127.0.0.1, when HA is another machine. HA's ESPHome config flow takes host and port, so a non-default port works. A `docs/home-assistant.md` section covers the manual add and the firewall note (API port must be reachable). |
| Stable, per-instance device name and MAC | HA keys ESPHome config entries by MAC address and derives entity IDs from device name + entity name. Two instances with the same MAC, or one instance whose MAC changes on every `up`, produce duplicate or zombie devices and `_2` entity IDs in the user's HA. | LOW | `host: mac_address:` is the supported dummy-MAC option. Derive it deterministically from the instance name (locally administered prefix `02:`), so rebuild and restart keep the same HA device. `esphome.name` and `friendly_name` come from the instance name via substitutions. Reject duplicate names across running instances. |
| Entities use the product contract names | The HIL fixtures in `wifi-fancontroller/hil/hil/entities.py` use `Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`. Keeping these identical makes SiL and HIL tests interchangeable and lets the same aioesphomeapi fixtures run against both. | LOW | Treat the names as the contract; the example firmware mirrors the Rev 3.1 entity declarations. |
| Fan speed control from HA works and moves the plant | Core value: HA command -> firmware -> PWM -> plant -> RPM back to HA. | LOW (given the pieces) | `fan: platform: speed` with `speed_count: 100`. Verified in the contract test (see Testing row below). |
| RPM, temperature, humidity, button state visible in HA | Listed in M1 Active. | LOW | Plain sensors / binary sensors. |
| LEDs visible/usable from HA | Listed in M1 Active ("LEDs ... usable from HA"). | LOW, but needs a decision | In the real Rev 3.1 package all five `partition` lights are `internal: True`, so they do NOT reach HA. For HA visibility the minimal example firmware must declare them non-internal (or add a second, exposed light). Decide this explicitly in requirements; do not let it surprise the roadmap. |
| Multiple simultaneous instances (name, MAC, API port, web port, sim-bus socket all per-instance) | Parallel agents, A/B comparison of two firmware versions, two fan controllers in one HA. Costly to retrofit if singletons creep in. | MEDIUM | Port allocation by probing; an instance registry file (`.sil/registry.json` or per-dir `instance.json`); `sil list`; every command takes `--instance` (default: the only running one, error if ambiguous). |

#### Logs

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Firmware stdout/stderr captured to `firmware.log` in the instance dir | The ESPHome logger writes to stdout on `host:`. When something misbehaves, the log is the first thing an agent reads. | LOW | `logger: level: DEBUG` in the example firmware. |
| Build log kept separately (`build.log`) and surfaced on build failure | Compile errors are long; the JSON error should point to the file and include the last N lines. | LOW | |
| Simulator log (sim-bus connect/disconnect, model events, API commands) | Distinguishing "firmware did not command PWM" from "sim ignored it". | LOW | Structured NDJSON, one event per line, with sim time. |
| `sil logs [--source fw\|sim\|build] [--follow] [--since]` | Agents read logs through the CLI, not by guessing paths. | LOW | Bounded size / rotate so a long run does not fill the disk. |
| Sim-time prefix/correlation on log lines | The firmware's log timestamps are its own `millis()`; the simulator's events are sim time. At x1 these roughly coincide, but the correlation convention must exist before the time factor does. | LOW | Prefix simulator lines with sim time; document how firmware `[hh:mm:ss]` relates (firmware uptime). |

#### Determinism and time (what M1 can honestly offer)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Injectable clock interface in the simulator; `dt`-based integration; sim timestamps on every sim-bus message | Project requirement. | LOW-MEDIUM | M1 implements only a real-time clock; design the interface so a scaled/lockstep clock can be added. All time-dependent code (model, press duration, plot x-axis, logs) reads the clock object; a lint-style test greps for `time.Now`/`time.Sleep` outside the clock package. |
| Repeatable start conditions (fresh prefs, fixed MAC, fixed model init, pinned ESPHome, no RNG in the M1 model) | Same inputs should give the same curve, within timing jitter. | LOW | State the guarantee precisely in docs: **repeatable, not bit-deterministic.** The firmware reads its own wall clock on `host:` (`millis()` from `CLOCK_MONOTONIC`) and the main loop is driven by `select()`, so sim-bus message arrival order and loop timing jitter at x1. Real determinism (Renode-style: virtual time owns execution) requires the later lockstep work. Tests written in M1 should assert with tolerance windows, not exact sample values. |
| Seed parameter plumbed through (unused by the M1 model) | Noise (later) must be seeded and reproducible; adding the knob later changes the `fan.toml` and CLI surface. | LOW | `--seed N`, logged in the status JSON `run.seed`. |

#### Testing the bench itself (contract)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Contract test: build the example firmware, `sil up`, connect with aioesphomeapi on the API port, set a fan to 50 %, assert RPM converges within tolerance, press a button and see the binary sensor flip, check LED state, `sil down` | Project requirement ("example and contract test"). Also the only automatable stand-in for Home Assistant in M1. | MEDIUM | pytest + aioesphomeapi is acceptable in tests (Go-only constraint applies to the simulator). Gate in CI-less manual runs for M1. |

### Differentiators (Competitive Advantage)

What this bench offers that Wokwi, Renode, or the bare ESPHome `host:` platform do not. These align with the Core Value (real ESPHome firmware, closed loop, local, observable and controllable).

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Real ESPHome firmware + real native API + real Home Assistant, on a laptop | Neither Wokwi (cloud, hardware-level emulation) nor Renode (CPU emulation, no ESPHome native-API story) gives you the user's actual HA dashboard talking to a simulated fan controller. This is the headline. | Already in M1 via `host:` | The differentiator is the combination: product YAML logic + plant + HA, no hardware. |
| Closed loop with a plant model (not just IO stubs) | The bare `host:` platform runs the firmware but has no fans, so PI and stall logic cannot be exercised. The plant is what makes it a "labcar". | M1: LOW-MEDIUM | Fidelity grows later (noise, faults, thermals). |
| Agent-first surface: every operation is a JSON-in/JSON-out CLI call with stable schemas and exit codes | Wokwi CLI and Renode Robot tests are scenario-file centric; interactive agent loops (`set`, `watch`, `status`) are not their design point. | LOW-MEDIUM | This is a design discipline more than a feature. It also keeps the web UI honest. |
| SiL/HIL name parity (same entity names, same aioesphomeapi fixtures) | One test can run against SiL (seconds, local) and HIL (rig) unchanged, implementing the stated test pyramid. | LOW | Comes from reusing `entities.py` names; no extra code if the example YAML follows the contract. |
| Single static Go binary + embedded UI; build via pinned Docker image; no host installs | Matches the locality constraint and "vendor shutdown must not kill the pipeline". | LOW (decided) | |
| `sil reboot` (restart firmware only, keep prefs and sim plant state) | Exercises `restore_mode`, `init_done`-style boot logic and the boot LED sequence without a rebuild. Hardware needs a power cycle for this. | LOW | Not in the Active list; cheap and high-value for the Rev 3.1 boot logic. Recommend adding to M1 as stretch. |
| Event trace (`trace.ndjson`: every PWM change, LED write, button edge, sensor update with sim timestamps) | Gives agents a complete record to diagnose "why did the fan not spin up" after the fact. The same record becomes the input to scenario assertions and metrics. | LOW-MEDIUM | M1: write it. M1 need not analyse it. |
| Shared operation vocabulary between CLI, web UI and (later) scenario engine | A scenario step is just a serialized CLI operation. Avoids a second, divergent API later. | LOW if designed now | Define operations as Go structs with JSON tags in one package. |

### Later (explicitly outside M1; listed so M1 designs do not block them)

These are valuable, and mostly standard in the domain, but PROJECT.md scopes them out of M1.

| Feature | Home | Why it matters | Complexity | M1 hook that keeps it cheap |
|---------|------|----------------|------------|-----------------------------|
| Scenario files (`sil run scenarios/x.toml`): ordered steps with sim-time timestamps | next milestone / P2 | Repeatable, reviewable tests; the pytest `-m sil` flow. Wokwi scenarios are the closest prior art: `delay`, `set-control`, `wait-serial`, `expect-pin`, plus screenshots. Renode adds testers with timeouts (`Assert LED State`, "assert and hold", "is blinking", `Wait For Line On UART`) and, in newer versions, deterministic pause on a match. | MEDIUM | CLI operation vocabulary; `watch` event stream; sim-time stamps; trace file. |
| Step vocabulary to copy: `set`, `press`, `wait <duration>`, `wait-until <condition> [timeout]`, `expect <condition> [within]`, `expect-hold <condition> for <duration>`, `expect-blinking led` | with scenarios | Matches what the best prior art offers; "assert and hold" matters for LED/stall logic. | | |
| JSON verdict from `sil run` (`verdict: pass\|fail\|error`, per-step result with sim time, metrics, log paths, ESPHome/sim versions) | with scenarios | Agents need machine-readable pass/fail. Keep infra errors (build died) distinct from test failures, as in the exit code convention above. | LOW once scenarios exist | Exit-code convention and JSON error objects from M1. |
| Time factor (x100+) | later per PROJECT.md | Thermal and long-run tests. Forces headless runs because HA needs x1. | HIGH | Injectable clock, `dt` integration, sim timestamps. Needs the `hal.cpp` + `application.cpp` patch. |
| Lockstep / virtual time | later | Real determinism. | HIGH | Same hooks. |
| Fault models: blocked, worn bearing, tach dead, wrong ppr; plug/unplug | P2 | Needed to test `stall_guard`. | MEDIUM | `fan.toml` schema allows a future `faults` layer; `sil set port2 fan=blocked` syntax is reserved. |
| Lua models, virtual supervisor (drives `fallback_mode`) | P2 | | MEDIUM-HIGH | Keep sim-bus messages typed and versioned. |
| Real product modules in the sim firmware | after M1 | `rpm_pi_control`, `stall_guard`, etc. | MEDIUM | M1 example firmware is the stand-in. |
| Web UI eye candy, fan photos, drag and drop | P3 | | MEDIUM | |
| `fan-analyze` | P4 | | HIGH | `fan.toml` is the output format. |
| Rack thermal model, control-quality KPIs | P5 | | HIGH | |
| CI runner (isolated, ephemeral) | later | | MEDIUM | Headless, non-interactive, JSON-only operation from day one. |
| Compare-two-runs / plot export (CSV/PNG) | after scenarios | Metrics, regressions. | MEDIUM | Trace file. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Pulse / edge-accurate tach simulation, ISR timing | "More realistic" | `host:` cannot run ISRs or the `pulse_counter` driver. Only drivers (a HIL concern) would benefit. Large effort, zero value for logic tests. Already decided: tach as RPM value. | RPM float over the sim-bus; HIL tach/PWM loopback covers drivers. |
| Wi-Fi, OTA, `update: http_request`, heap/ISR simulation | Rev 3.3 YAML contains them | Not possible on `host:`; networking is the host's. | Example firmware omits them; HIL covers them. Note: `host:` has its own OTA on port 8082 that can overwrite the binary; disable it or keep it off by default. |
| Generic hardware emulator (arbitrary ESPHome platforms, GPIO-level, I2C devices like the real HDC1080) | Sounds general-purpose | A fixed small set of `sim_*` components is tractable; a general emulator is Renode/QEMU territory, and QEMU has no ESP32-S2 machine. | Replace sensors at the component level (`sim_temperature`), as decided. Add new `sim_*` only when a product module needs it (`ds2484_onewire`, `bme680`, `scd41` come later). |
| Any UI-only capability (a button the CLI cannot do) | Quick wins in the page | Breaks the "UI is a client" rule; agents cannot reproduce what a human did. | Add the operation to the JSON API first. |
| Frontend build pipeline (npm, bundlers) or runtime CDN | Familiar tooling | Violates Constraints; slows agent loops; adds a dependency that can disappear. | Static HTML + vanilla JS + vendored uPlot, `embed`-ed. |
| Cloud simulators or cloud-hosted dashboards | Zero setup | Locality requirement. | Local only. |
| Bundling or auto-configuring a Home Assistant instance (compose file with HA, auto-pairing, writing to the user's HA) | "One-command demo" | Heavy, hard to keep current, and risks touching the user's production HA. Decision: the sim exposes the API; the user picks the HA. | Documentation of the manual add; optional separate doc for a throwaway HA container, not part of the product. |
| Dynamic time factor changes mid-run | Convenient fast-forward | Every participant must share one time base; the design decision is a fixed factor per run. | Fixed factor chosen at `up`/`run` time (later). |
| File-watch auto-rebuild/hot reload | Faster loop | A compile+restart changes firmware state (prefs, boot sequence), so silent rebuilds produce confusing observations; agents prefer explicit commands. | Explicit `sil rebuild`, fast via incremental build. |
| Always-on daemon or systemd service | Convenience | Hidden state, port conflicts, harder to reason about. | Per-run processes tracked by the instance dir; `sil down` cleans up. |
| Interactive TUI | Nice terminal experience | Agents cannot use it; extra surface. | `sil watch` NDJSON and the web UI. |
| Auth, user accounts, remote access for the web UI | "Share the bench" | Single-user local tool; auth adds attack surface and state. | Bind 127.0.0.1; if remote access is needed, use the user's own tunnel. Note: the firmware's native API port is reachable from the LAN by necessity for HA; document that it is unauthenticated unless an encryption key is set. |
| Persistent run database, long-term metrics store | "Track history" | Storage, schemas, retention for no M1 value. | Files per instance/run directory (logs, trace). |
| High-fidelity physical fan modelling in M1 (CFD, full curves, wear, noise) | "Realism" | Linear+lag already makes the loop meaningful; fidelity is the job of `fan-analyze` fits (P4) and fault/noise layers. | Keep `fan.toml` extensible; start linear. |
| Silent fallbacks (ignoring unknown `fan.toml` keys, different ESPHome version, missing pinned image) | "Be lenient" | Silent drift produces untrustworthy results, the worst outcome for a test bench. | Fail loudly with JSON errors. |
| Supporting revisions other than 3.3 in M1; real product modules in M1 | Completeness | Explicitly out of scope; couples the plumbing proof to moving product code. | Revisit after the consumer interface settles. |

## Feature Dependencies

```
Pinned ESPHome Docker build (sil up --build)
    └──requires──> sim_* external components (PWM out, tach in, LED strip, buttons, temp/humidity)
                       └──requires──> sim-bus protocol (typed msgs, sim timestamps, versioned)
                                          └──requires──> Simulator core (clock interface, dt loop)

Plant model (fan.toml, linear + lag, start threshold)
    └──requires──> Simulator core (clock, dt)
    └──requires──> fan.toml loader/validator + fan library (1-2 example fans)

Status JSON snapshot ──requires──> Simulator core + sim-bus state
    └──enables──> Web UI (same struct over WebSocket)
    └──enables──> sil watch (event stream) ──enables──> scenarios, wait-until (later)

sil set / press / hold / release ──requires──> sim-bus (input messages) + clock (sim-time press duration)
    └──enables──> Web UI buttons / temp fields (same operations)

HA integration
    ├──requires──> firmware `api:` + `host: mac_address` (from instance name)
    ├──requires──> example firmware YAML (entity names = product contract; LEDs non-internal)
    └──requires──> per-instance port allocation (API port)

Multiple instances ──requires──> per-instance state dir, name, MAC, API port, web port, bus socket
    └──enhances──> HA integration (several devices in one HA)

Logs ──requires──> per-instance state dir; Event trace ──enhances──> logs, scenarios

Contract test ──requires──> all of the above (it exercises the full path via aioesphomeapi)

Scenario engine, verdict JSON (later) ──requires──> operation vocabulary + watch stream + trace + exit-code convention
Time factor (later) ──requires──> injectable clock + sim timestamps + hal.cpp patch
Fault models (later) ──requires──> plant model extension points

Auto-rebuild on file change ──conflicts──> deterministic, explicit-state lifecycle (anti-feature)
Time factor > 1 ──conflicts──> HA in the loop (HA forces x1)
```

### Dependency Notes

- **Everything observable requires the sim-bus first.** Nothing in the UI, HA test or CLI can be proven before one message round-trips firmware <-> simulator. This dictates M1 ordering: sim-bus + one fan port closed loop headless, then the rest of IO, then UI, then HA/multi-instance hardening.
- **Status JSON before UI.** If the UI is built first, it grows its own state model. Build the snapshot/event types, serve them over CLI, then add WS.
- **Instance isolation before HA.** HA's identity model (MAC) and the host platform's prefs directory both require per-instance naming from the start; adding "instance" late means touching ports, paths, sockets, and names everywhere.
- **Example firmware before contract test, and it fixes the LED question.** Whether LEDs reach HA is decided in the example YAML, not in the simulator.
- **Press durations depend on the clock**, not on `sleep`; otherwise the time factor later breaks long-press semantics.
- **HA excludes time factor > 1**: any run with HA connected is x1, so scaled runs are headless. Surface this in `up` (reject `--time-factor` with HA exposure, later).

## MVP Definition

### Launch With (M1 acceptance set)

- [ ] `sil up` / `down` / `rebuild` / `status` / `logs`, per-instance state dir, JSON everywhere, exit-code convention, ESPHome version check
- [ ] Sim-bus + `sim_*` components for 4 fan ports (PWM in, RPM out), LED strip, buttons USR1-3, temperature/humidity; example firmware YAML on the Rev 3.3 IO surface with product-contract entity names
- [ ] Plant model: linear + first-order lag + start threshold, params from validated `fan.toml`; injectable clock, `dt` integration, sim timestamps
- [ ] `sil set temperature|humidity`, `sil press [--long]` (+ `hold`/`release`), fan-per-port assignment
- [ ] Native API exposure with per-instance port, deterministic MAC/name, printed HA manual-add info; LEDs made visible to HA by the example firmware (decision needed)
- [ ] Web UI: per-port duty/RPM/plot, virtual RGB LEDs, short/long buttons, run info; WS with snapshot-on-connect; embedded, no CDN
- [ ] Logs captured (firmware, sim, build) with `sil logs`
- [ ] Contract test with aioesphomeapi covering fan speed, RPM, button, LED, temperature
- [ ] Documentation of the determinism guarantee ("repeatable, not bit-exact") and the HA manual-add flow

### Add After Validation (M1.x / next milestone)

- [ ] `sil watch` NDJSON stream - trigger: first agent that polls status in a loop (M1 stretch; near-free given the WS)
- [ ] `sil reboot` - trigger: first test of restore/boot sequence (M1 stretch)
- [ ] Event trace file - trigger: first "why did it not spin up" debugging session (M1 stretch)
- [ ] Multiple instances beyond "design for it" (`sil list`, registry) - trigger: second parallel agent or a second device in HA
- [ ] Scenario files + JSON verdict (`sil run`) - trigger: M1 plumbing proven and the first product-repo test is written
- [ ] Seeded noise in the model, then fault profiles

### Future Consideration (v2+)

- [ ] Time factor, lockstep virtual time, CI runner
- [ ] Lua, virtual supervisor, plug/unplug, fan photos / eye candy
- [ ] `fan-analyze`, rack thermals and KPIs
- [ ] Real product modules in sim; Rev 3.3 SiL YAML moves to the product repo at 1.0

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Sim-bus + `sim_*` components + one fan loop | HIGH | HIGH | P1 |
| `sil up/down/rebuild/status` with JSON | HIGH | MEDIUM | P1 |
| Plant model + `fan.toml` validation | HIGH | LOW-MEDIUM | P1 |
| `sil set`, `sil press [--long]` | HIGH | LOW | P1 |
| HA: API port, MAC/name, manual-add info, product entity names | HIGH | LOW | P1 |
| Web UI minimum (plots, LEDs, buttons, run info) | MEDIUM-HIGH | MEDIUM | P1 |
| Logs capture + `sil logs` | HIGH | LOW | P1 |
| Injectable clock / `dt` / sim timestamps | HIGH (enabler) | LOW-MEDIUM | P1 |
| Contract test (aioesphomeapi) | HIGH | MEDIUM | P1 |
| Per-instance isolation (state dir, ports, socket) | HIGH | MEDIUM | P1 |
| `sil watch` stream | MEDIUM-HIGH | LOW | P2 |
| `sil reboot` | MEDIUM | LOW | P2 |
| Event trace file | MEDIUM | LOW-MEDIUM | P2 |
| `sil list` / full multi-instance UX | MEDIUM | LOW | P2 |
| Scenario files + JSON verdicts | HIGH (later) | MEDIUM | P2 (next milestone) |
| Temp/humidity fields in the UI | LOW-MEDIUM | LOW | P2 |
| Seed plumbing | LOW now | LOW | P2 |
| Time factor | HIGH (later) | HIGH | P3 |
| Faults, Lua, eye candy, `fan-analyze`, thermals | HIGH (later) | MEDIUM-HIGH | P3 |

**Priority key:** P1 must have for M1; P2 should have, add when cheap or once M1 is proven; P3 later milestones.

## Competitor / Prior Art Feature Analysis

| Capability | Wokwi (cloud/browser, ESP32 sim) | Renode (CPU/peripheral emulation) | ESPHome `host:` alone | Automotive labcar / HIL tools (dSPACE, NI) | Our Approach |
|------------|--------------------------------|-----------------------------------|-----------------------|--------------------------------------------|--------------|
| Run real firmware | Yes, compiled binary on emulated MCU | Yes, binary on emulated CPU | Yes, but a host-compiled build (not the shipping binary) | Real ECU hardware | Host-compiled ESPHome firmware; logic only, no drivers |
| Plant / environment model | Virtual parts, limited | Peripheral models, user-written | None | Core feature (model-in-the-loop, Simulink-class) | Linear+lag fan model from `fan.toml`; thermal later |
| Virtual LEDs / buttons | Yes, rich GUI | LED/button models plus testers | None (SDL display only) | Signal panels, breakout boxes | Minimal UI + same via CLI |
| Scripted stimulus + verdict | YAML scenarios (`delay`, `set-control`, `wait-serial`, `expect-pin`); CI action | Robot Framework testers with timeouts, assert-and-hold, blink detection | None | Test automation (stimulus, recording, pass/fail) | CLI ops now; scenarios + JSON verdict next |
| Determinism | Not documented | Strong (virtual time owns execution; pause on match) | None (wall clock) | Real-time deterministic | M1 repeatable at x1; lockstep later |
| Time scaling | Not primary | Virtual time | None | Real time mostly | Fixed factor per run, later |
| Logs | Serial monitor | UART/log testers | stdout | Data logging/measurement files | Per-source logs + trace |
| HA / native API | Wi-Fi gateway for network sims; no ESPHome-API story | Not applicable | Native API available; manual add to HA (no mDNS discovery) | n/a | Native API exposed, deterministic MAC/name, instance-scoped ports |
| Local-only, no vendor dependency | No | Yes | Yes | Yes but commercial | Yes (hard requirement) |
| Agent-friendliness | CLI exists, scenario-based | Scriptable, heavy | Process only | GUI-centric | JSON CLI first |

## Open Questions for Requirements Definition

1. **LEDs in HA:** Rev 3.1 declares all status lights `internal: True`. Should the M1 example expose them (non-internal) so "LEDs usable from HA" is true, accepting the example diverges from the product package?
2. **Where does the firmware binary run?** Host or inside the ESPHome container (glibc match)? Decides port mapping and socket mounting for `up`.
3. **Press timing defaults:** what long-press threshold does the product's `usr_buttons` module use? Align `--long` default with it (read `wifi-fancontroller/modules/usr_buttons.yaml` when writing the requirement).
4. **API bind address and trust:** the `host:` API is reachable from the LAN for HA. Confirm what the host build binds to (not verified here) and whether M1 sets an API encryption key or documents the open port.
5. **Multi-instance depth in M1:** isolate everything per instance now (recommended) but defer `sil list`/registry UX.
6. **Fan swap at runtime in M1?** Recommended no (fixed per `up`); swap belongs with plug/unplug in P2.

## Sources

- `/home/claude/esphome-sil/.planning/PROJECT.md` and `/home/claude/esphome-sil/docs/initial-idea.md` (project scope, M1 Active and Out of Scope lists; HIGH)
- `/home/claude/wifi-fancontroller/hardware-rev-3.3.yaml`, `hardware-rev-3.1.yaml` (IO surface, entity names, internal LEDs, restore behaviour; HIGH)
- `/home/claude/wifi-fancontroller/hil/hil/entities.py` and `contract.py` (entity-name contract, API port 6053; HIGH)
- ESPHome Host Platform docs, https://esphome.io/components/host/ (dummy `mac_address`, no mDNS discovery so manual add by IP, prefs in `ESPHOME_PREFDIR`, host OTA port 8082, no Wi-Fi; HIGH)
- Home Assistant ESPHome integration docs, https://home-assistant.io/integrations/esphome and community threads on MAC-keyed device identity and duplicates (MEDIUM; community posts are anecdotal)
- Wokwi automation scenarios, https://docs.wokwi.com/wokwi-ci/automation-scenarios (step types `delay`, `expect-pin`, `set-control`, `wait-serial`, `write-serial`, `take-screenshot`; exit codes not documented there; MEDIUM)
- Renode testing API, https://renode.readthedocs.io/en/latest/basic/renode-testing-api.html (LED/button/UART/log testers; HIGH for what is listed, virtual-time control not covered on that page) and Renode changelog / Antmicro posts on deterministic pause and deterministic testing (MEDIUM)
- Automotive labcar/HIL conventions (stimulus, fault insertion, recording, verdicts): general domain knowledge, not re-verified (MEDIUM-LOW)

---
*Feature research for: local SiL test bench for ESPHome firmware (esphome-sil)*
*Researched: 2026-11-08*

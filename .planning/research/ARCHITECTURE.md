# Architecture Research

**Domain:** Software-in-the-loop (SiL) test bench for ESPHome firmware (`host:` binary + `sim_*` external components + Go plant simulator)
**Researched:** 2026-11-08
**Confidence:** HIGH for the ESPHome-side integration (read from the pinned 2026.10.1 source and checked with two Docker spikes); MEDIUM for the Go internals and wire protocol (sound patterns, not yet built); LOW-MEDIUM for the later time-factor and Lua seams (deliberately only seams).

Method note: no web research was needed. The pinned ESPHome 2026.10.1 source (the `hil/.venv` of `wifi-fancontroller`) and the `ghcr.io/esphome/esphome:2026.10.1` image were inspected directly. Statements marked **[verified]** were checked against that source or by running the image.

---

## Standard Architecture

### System Overview

Three processes plus an optional fourth observer. Dependencies point left to right on the control path; the sim-bus is the only coupling between firmware and simulator.

```
                         developer / coding agent / pytest
                                      │  (CLI: sil up|set|press|status|logs|down)
                                      ▼
┌────────────────────────────  sil daemon (one Go process)  ───────────────────────────┐
│                                                                                      │
│  ┌────────┐   JSON/HTTP    ┌───────────────┐  commands   ┌──────────────────────┐    │
│  │ CLI    │───────────────▶│ API layer     │────────────▶│ ENGINE (single       │    │
│  │ client │  (same binary, │ Control iface │◀── result ──│ goroutine / actor)   │    │
│  └────────┘   `sil set`)   │ HTTP + WS     │             │  ports, fan models,  │    │
│  ┌────────┐   WebSocket    │               │  snapshots  │  env, button timers  │    │
│  │ Web UI │◀───────────────│ state.Hub     │◀────────────│  injectable Clock    │    │
│  │(static)│  JSON push     └───────────────┘             └───────▲──────┬───────┘    │
│  └────────┘                                       inbox events   │      │ outbound    │
│                                                   (duty, LED)    │      ▼ values      │
│  ┌──────────────────────┐                                ┌───────┴──────────────┐    │
│  │ Firmware lifecycle   │ docker build / run / logs      │ bus.Server           │    │
│  │ manager              │─────────────┐                  │ (listener, framing,  │    │
│  └──────────────────────┘             │                  │  channel registry)   │    │
└───────────────────────────────────────┼──────────────────┴───────▲──────────────┘────┘
                                        │                          │ sim-bus (unix socket,
                                        ▼                          │ binary frames, state-based)
┌─────────────────────────  Docker container (esphome:2026.10.1)  ──┴──────────────────┐
│  ESPHome `host:` binary ("program")                                                  │
│   sim_bus (client, non-blocking, in main loop) ◀──▶ sim_output / sim_sensor /        │
│                                                      sim_binary_sensor / sim_led_strip│
│   real product logic: fan speed, partition lights, scripts, lambdas, globals        │
│   `api:` native API on :6053  ◀────────────────────────────────────────────────────┐ │
└──────────────────────────────────────────────────────────────────────────────────┼─┘
                                                                                   │
                                          Home Assistant (user-chosen instance) ───┘
                                          talks to the FIRMWARE directly, never to the simulator
```

### Component Responsibilities

| Component | Responsibility (what it owns) | Typical Implementation |
|-----------|-------------------------------|------------------------|
| `components/sim_bus` | Single socket client for the whole firmware; channel registry; frame codec; reconnect; full-state resync on connect | C++ `Component` (`setup_priority::BUS`), POSIX non-blocking socket, `wake_register_fd` for the main-loop `select()` |
| `components/sim_output` | Replaces `ledc`: receives `FloatOutput::write_state(duty)`, forwards to bus | `output::FloatOutput`, no `loop()` |
| `components/sim_sensor` | Replaces `pulse_counter` (RPM) and `hdc1080` (temp/humidity): publishes latest bus value at `update_interval` | `PollingComponent` + `sensor::Sensor` |
| `components/sim_binary_sensor` | Replaces `gpio` buttons: publishes the logical level the sim sends | `binary_sensor::BinarySensor`, publishes from bus callback |
| `components/sim_led_strip` | Replaces `esp32_rmt_led_strip`: a real `light::AddressableLight` so `partition` lights and effects work; mirrors the pixel buffer to the bus | `AddressableLight` subclass, dirty-flag + send from `loop()` |
| `internal/bus` (Go) | Accept firmware connection, frame codec, channel name ↔ index registry, per-session state; knows nothing about fans | `net.Listener` (unix), one reader goroutine per conn, writes serialized |
| `internal/clock` | The only source of time: `Now()`, tickers, timers; real (with factor) and manual implementations | Interface + two impls; `time.Now` banned elsewhere |
| `internal/model` | Pure, deterministic plant math: fan model (first-order lag), env model; `Step(dt, in) out` | No I/O, no goroutines, no clock |
| `internal/fanlib` + `fans/` | Load/validate `fan.toml`; embedded default library + override dir | `go:embed` + TOML decode |
| `internal/engine` | **Owns all mutable simulation state.** Applies bus events and API commands in order, ticks models with `dt`, runs the sim-time timer queue (button release), emits snapshots and outbound values | Single goroutine actor; channels in, snapshots out |
| `internal/state` | Immutable `Snapshot` type, latest-snapshot pointer, subscriber hub, history ring buffer | `atomic.Pointer[Snapshot]`, fan-out with drop-oldest per subscriber |
| `internal/api` | Transport-neutral `Control` interface (Status, Set, Press, History, Subscribe, Logs) plus HTTP/JSON and WebSocket adapters; versioned JSON types (`/api/v1`) | `net/http` ServeMux + one WebSocket lib (`github.com/coder/websocket`) |
| `internal/cli` | `sil` subcommands. Control commands are an HTTP client of the daemon; `up`/`down` manage the daemon | Plain `flag` subcommands or cobra; client implements `Control` |
| `internal/lifecycle` | Build firmware (Docker, pinned image), start/stop the container, capture and parse logs, report phases (building → starting → connected → exited) | `os/exec` driving the `docker` CLI behind a `Runner` interface |
| `ui/` | Static page; zero logic; renders `Snapshot` JSON, posts commands | `index.html` + vanilla JS + vendored uPlot, `go:embed` |

---

## Recommended Project Structure

```
esphome-sil/
├── go.mod                       # module github.com/zeroflow/esphome-sil
├── Makefile                     # build, test, e2e (docker), lint
├── cmd/sil/main.go              # thin: wires internal/cli
├── internal/
│   ├── bus/                     # wire codec + server (no model knowledge)
│   ├── clock/                   # Clock, RealClock{factor}, ManualClock
│   ├── model/                   # fan, env: pure functions of (state, params, dt, inputs)
│   ├── fanlib/                  # fan.toml loader/validator
│   ├── engine/                  # actor: world, ports, tick loop, timer queue, hook chain (empty in M1)
│   ├── state/                   # Snapshot, Hub, history ring
│   ├── api/                     # Control iface, v1 JSON types, http+ws adapters
│   ├── lifecycle/               # docker build/run/stop/logs, run dir management
│   ├── cli/                     # subcommands, run.json discovery, HTTP client
│   └── config/                  # bench.toml (ports, fan assignment, channel naming)
├── ui/
│   ├── embed.go                 # //go:embed static  (embed cannot reach parent dirs)
│   └── static/{index.html,app.js,style.css,vendor/uplot.*}
├── fans/
│   ├── embed.go                 # //go:embed */fan.toml  (default library baked into binary)
│   └── generic-120mm/fan.toml   # one folder per fan; photos/measured/report.html later
│   # fans/_faults/ arrives with P2 (Lua); not created in M1
├── components/                  # ESPHome external components (what consumers point at)
│   ├── sim_bus/{__init__.py,sim_bus.h,sim_bus.cpp,sim_protocol.h}
│   ├── sim_output/{output.py,sim_output.h,sim_output.cpp}
│   ├── sim_sensor/{sensor.py,sim_sensor.h,sim_sensor.cpp}
│   ├── sim_binary_sensor/{binary_sensor.py,...}
│   └── sim_led_strip/{light.py,sim_led_strip.h,sim_led_strip.cpp}
├── protocol/
│   ├── PROTOCOL.md              # wire spec (internal, free to change)
│   └── vectors/*.hex            # golden frames, consumed by BOTH Go and C++ tests
├── examples/
│   └── minimal-rev33/           # firmware.yaml + hardware-sim.yaml + bench.toml (contract test input)
├── tests/
│   ├── cpp/                     # host-g++ codec test (includes ../../components/sim_bus/sim_protocol.h)
│   └── e2e/                     # Go tests behind build tag `sil_e2e`: build example in Docker, run closed loop
└── docs/                        # consumer-interface README, initial-idea.md
```

### Structure Rationale

- **`components/` at repo root:** ESPHome's git external-component source finds a top-level `components/` folder automatically **[verified: `external_components/__init__.py` checks `esphome/components` then `components`]**, so `github://zeroflow/esphome-sil@vX.Y` works with no `path:` key.
- **`sim_protocol.h` lives inside `components/sim_bus/`:** ESPHome copies only `components/<name>/` into the build tree, so the shared header must be there. The Go side mirrors it by hand and both are pinned by `protocol/vectors/`.
- **No C++ tests inside `components/`:** ESPHome compiles every `.cpp` it copies, so a test `.cpp` in a component dir would end up in the firmware. Keep tests under `tests/cpp/`.
- **`embed.go` in `ui/` and `fans/`:** `go:embed` cannot reference parent directories, so each embedded asset tree carries its own tiny package. Result: single static binary; `--ui-dir` / `--fans-dir` flags override from disk for fast iteration.
- **`internal/model` is pure:** no clock, no I/O, no goroutines. This is what makes `dt`-based integration and the later time factor free, and what makes model tests millisecond-fast.
- **`internal/bus` vs `internal/engine` split:** the bus layer deals in channels and values; the engine deals in ports and fans. Replacing or faking the bus (tests, a future in-process firmware stub) never touches the models.

### How `external_components:` points at it

```yaml
# In this repo (examples/, dev loop): local path, relative to the YAML in the container mount
external_components:
  - source: {type: local, path: ../../components}

# In wifi-fancontroller (consumer), pinned tag
external_components:
  - source: github://zeroflow/esphome-sil@v0.1.0
    components: [sim_bus, sim_output, sim_sensor, sim_binary_sensor, sim_led_strip]
```

Mount rule for the Docker build: mount the **git top-level of the YAML at the same absolute path** inside the container and set the working directory to the YAML's directory. Relative `type: local` paths and `!include`s then resolve identically on host and in the container (MEDIUM: not yet tried with an external component; proven only for config-dir mounts).

---

## Signal Ownership and Data Flow

### Source of truth per signal

| Signal (channel name) | Direction on bus | Source of truth | Consumers | Notes |
|-----------------------|------------------|-----------------|-----------|-------|
| `portN.duty` | firmware → sim | **Firmware** (value the `ledc` output would drive) | plant model, UI, API (read-only) | Float 0..1. Sim never writes it. |
| `portN.rpm` | sim → firmware | **Sim plant model** | firmware `sim_sensor` → HA | Delivered as RPM, no pulses (decided). Firmware republishes at `update_interval`. |
| `led.strip` pixels | firmware → sim | **Firmware** (post colour-correction buffer) | UI, API (read-only) | Sim only mirrors; never computes LED state. |
| `btn.usr1..3` level | sim → firmware | **Sim** (virtual operator: CLI/API/UI) | firmware `sim_binary_sensor` | Level, not event. Short/long press = sim-time-timed down/up pair scheduled by the engine. |
| `env.temperature`, `env.humidity` | sim → firmware | **Sim** (M1: settable constant; later thermal model) | firmware `sim_sensor` | |
| Fan assignment, `fan.toml` params | none (sim-internal) | **Sim** (`bench.toml` + runtime `set`) | engine | Firmware never learns which fan is attached. |
| Sim time, `firmware_connected`, behind-realtime flag | none | **Sim** | API, UI | "Connected" means a valid Hello was received, not that the container is running. |
| Fan on/off and speed %, light state as HA sees them | none (HA ↔ firmware native API) | **Firmware** | Home Assistant | The sim is not in this path and must not proxy it. |
| Firmware logs | stdout → lifecycle | **Firmware** | API/CLI/UI | Captured from `docker run` stdout, ANSI stripped, ring-buffered. |

Rule of thumb: **whoever physically produces the signal on real hardware owns it in the sim.** Firmware owns everything a GPIO output drives; the sim owns everything the outside world feeds into a GPIO/I2C input.

### Key Data Flows

1. **Closed loop (the core path):**
   ```
   HA/automation → firmware logic → fan.speed → sim_output.write_state(duty)
        → sim_bus frame OUT_F32(port1.duty, t) ──▶ bus.Server ──▶ engine inbox
        → fan model Step(dt): rpm += (target − rpm)·(1−e^(−dt/τ)) ──▶ rpm changed beyond ε (or 1 Hz refresh)
        → bus.Server.Send IN_F32(port1.rpm) ──▶ sim_bus cache ──▶ sim_sensor.update() (every 1 s) → publish_state → HA
   ```
2. **LED path:** `partition` light effect → `sim_led_strip` buffer → dirty flag → one OUT_PIXELS frame per `loop()` at most → engine → `Snapshot.leds` → WebSocket → UI renders.
3. **Button path:** UI click or `sil press usr1 --long` → API `Press` → engine schedules `down@t`, `up@t+hold` on the **sim-time timer queue** → bus IN_BOOL → `sim_binary_sensor.publish_state` → firmware `on_press`/`on_click`.
4. **Control path (CLI/UI):** always `client → HTTP/WS → api.Control → engine.Do(cmd)` (synchronous request/response through the actor, so validation errors return to the caller). There is no second path that mutates state.
5. **Observation path:** engine emits an immutable `Snapshot` every ~50 ms (and immediately on LED/button change) → `state.Hub` → each WebSocket subscriber has a small buffered channel with drop-oldest. `GET /api/v1/status` reads the latest snapshot pointer without touching the engine. `GET /api/v1/history` serves the ring buffer so a reloaded page repaints plots.
6. **Resync path (restart of either side):** state-based protocol. On every (re)connect the firmware sends `HELLO` with its channel manifest then a full snapshot of its outputs; the sim answers `WELCOME` then a full snapshot of all inputs. Either side can restart independently; no delta is ever lost.

### State Management

```
bus events ─┐
API cmds  ──┼─▶ engine inbox (ordered) ─▶ [engine goroutine owns World] ─▶ Snapshot (immutable)
timer queue ┘                                                              ├─▶ atomic latest ─▶ GET /status
                                                                           ├─▶ Hub ─▶ WS clients
                                                                           └─▶ history ring ─▶ GET /history
```

No mutex on simulation state: one goroutine mutates, everything else reads immutable snapshots.

---

## ESPHome `host:` Integration (the sim-bus client)

All items below are **[verified]** against 2026.10.1 unless noted.

### Main loop facts that drive the design

- `Application::loop()` runs the scheduler, then the **component phase** gated by `loop_interval_` (default 16 ms), then sleeps in `wakeable_delay()` which on host is a `select()` over registered fds plus an internal UDP wake socket.
- `esphome::wake_register_fd(int fd)` / `wake_unregister_fd(int fd)` (in `esphome/core/wake.h`, host only, main loop only) add an fd to that `select()`. When `select()` returns `> 0` the loop calls `wake_request_set()`, which opens the component-phase gate, so data on the bus socket is serviced on the same tick instead of up to 16 ms later.
- Everything is single-threaded. No threads in `sim_*`; do not call `wake_loop_threadsafe` from anywhere.
- `loop_component_start_time_` / blocking-warning machinery flags components whose `loop()` blocks more than ~50 ms. A blocking socket call would trigger this and stall the whole firmware.

### Client design

| Concern | Decision |
|---------|----------|
| Socket | `socket(AF_UNIX, SOCK_STREAM \| SOCK_NONBLOCK \| SOCK_CLOEXEC)`. `connect()` on a unix socket completes or fails immediately; TCP-loopback fallback uses `EINPROGRESS` + writability check. Address string `unix:/path` or `tcp:host:port`. |
| Address config | YAML key `sim_bus: address:` as compiled default, **overridden at runtime by env `SIL_BUS`**. One compiled binary works in any run dir (builds are slow, runs must be cheap). |
| Setup priority | `sim_bus::get_setup_priority() = setup_priority::BUS` (1000): it sets up before IO/HARDWARE components and, because loop order follows the sorted component list, its `loop()` runs first each tick, so fresh inputs are in the cache before consumers run. Channel components keep default priorities. |
| Channel registration | Done at codegen time (`cg.add(bus.register_channel(kind, "port1.duty", extra))`), i.e. pure data, before any `setup()`. Setup never touches the network except one non-blocking connect attempt. |
| State machine | `DISCONNECTED → (backoff timer 100 ms → 2 s, via `millis()`) → CONNECTED → send HELLO → RUNNING`. EOF or `EPIPE` → `wake_unregister_fd`, close, back to `DISCONNECTED`. |
| Reading | In `loop()`: `read()` until `EAGAIN` into a fixed buffer, parse complete frames, update the per-channel cache and call channel listeners. **Always drain to EAGAIN**, otherwise the registered fd stays readable and `select()` spins at 100% CPU. |
| Writing | Outbound is **state-coalesced**: each output channel has `latest` + `dirty`; `flush()` serializes dirty channels with `send(MSG_NOSIGNAL \| MSG_DONTWAIT)`. A partial write leaves at most one frame in a small residual buffer. No unbounded queue, no dropped final state. `write_state()` tries an immediate flush (lowest latency), `loop()` retries. |
| Not-connected behaviour | Firmware must boot and run without a sim. Outputs just mark dirty; sensors publish nothing (`NaN`/no state, like an unplugged sensor, which is realistic); buttons stay released. Optional `wait_for_sim: <duration>` implements `can_proceed()` so scenario runs get a deterministic start. Default off. |
| Fd registration | Optional optimization. Phase 1 can poll in `loop()` at 16 ms and works correctly; adding `wake_register_fd` is an incremental latency/CPU improvement. It is an internal ESPHome API, acceptable only because the version is pinned and checked. |
| Version guard | `sim_bus/__init__.py` compares `esphome.const.__version__` with the supported pin during config validation and fails with a clear message (consumer-interface item 4). Cheap, and the same check later guards the HAL time patch. |

### Sensor and binary-sensor semantics

- **`sim_sensor` is a `PollingComponent`.** It does not publish on every bus message. It caches the latest value and publishes in `update()` at `update_interval` (1 s for tach, 10 s for temperature, mirroring `pulse_counter` and `hdc1080`). This keeps real publishing cadence, filters (`multiply`, `sliding_window_moving_average`) and API traffic identical to hardware, and it automatically scales with a later time factor because `update_interval` is in firmware time.
- **Tach is delivered as RPM.** The original YAML has `filters: - multiply: 0.5` because `pulse_counter` yields pulses/min at 2 PPR. The sim variant omits that filter; the product repo's `hardware-sim-*.yaml` owns that difference.
- **`sim_binary_sensor` publishes from the bus callback** (no polling, no own `loop()`), logical level after any inversion. Debounce/`delayed_on` filters stay in YAML as on hardware.

### `sim_led_strip` specifics

- Subclass `light::AddressableLight` with its own RGB(W) buffer and `effect_data_`, copying the shape of `esp32_rmt_led_strip` (`size()`, `get_traits()`, `clear_effect_data()`, `get_view_internal()` returning an `ESPColorView` into the buffer, `write_state()`). That is what makes `partition` lights, `addressable_*` effects and `light.turn_on` lambdas behave identically to the product.
- `write_state()` only sets `dirty_`; `loop()` sends **at most one OUT_PIXELS frame per tick**. Pixels sent are the post-correction buffer (what the LED would show). Wire order (GRB vs RGB) is irrelevant to the sim and is not modelled.

### YAML config key ↔ sim ID mapping

- Each `sim_*` platform has a **required `channel:` string**, e.g. `port1.duty`, `port1.rpm`, `led.strip`, `btn.usr1`, `env.temperature`. The ESPHome `id:` stays whatever the product YAML wants (`pwm_fan1`, `fan1_speed`); the two namespaces are deliberately independent so product refactors do not break the sim.
- Convention: `<port>.<signal>` for per-port signals, `led.*`, `btn.*`, `env.*` for the rest. The Rev 3.3 sim bench is *data*, not code: `bench.toml` lists ports and which fan is attached; channel names derive from the convention.
- The wire carries a numeric `u16` channel index. Names are exchanged once in `HELLO` (manifest: index, kind, name, extra such as `num_leds`). The simulator validates the manifest: a firmware channel with no model binding or a bench port with no firmware channel is reported in `status.warnings`, not fatal. This catches YAML typos on the first run.
- Python-side: `FINAL_VALIDATE_SCHEMA` in `sim_bus` rejects duplicate `channel:` strings and wrong kinds at `esphome config` time, not at runtime.

### Hardware-package consequences (important for the example YAML)

Verified by running the image:

- `output: platform: ledc` fails with "requires component esp32". `i2c`, `pulse_counter`, `hdc1080`, `esp32_rmt_led_strip` are equally out.
- `wifi:` passes `esphome config` on host but **fails at `compile`** (`KeyError: 'power_save_mode'` in `wifi/__init__.py to_code`). Rev 3.1's `wifi.on_connect/on_disconnect` status-LED hooks therefore cannot be reused; the example needs an `api.on_client_connected/on_client_disconnected` or `on_boot` equivalent. This confirms the PROJECT.md note that swapping only the hardware package does not compile.
- `mdns_host.cpp` exists, so with host networking Home Assistant can discover the sim firmware; otherwise point HA at `ip:6053` manually.
- Preferences persist to `$ESPHOME_PREFDIR/<name>.prefs` (default `~/.esphome/prefs`). `globals: restore_value` and fan `RESTORE_DEFAULT_ON` therefore behave like a real device across runs. The lifecycle manager must set `ESPHOME_PREFDIR` to a per-run directory and offer `--fresh` to wipe it, otherwise "first boot" logic (`init_done`) runs once and never again.

---

## Firmware Lifecycle Manager

**Where the firmware runs: inside the Docker image, not on the host.** Verified by spike: a trivial `host:` binary built in `esphome:2026.10.1` (Debian 13, glibc 2.41, GCC 14) needs `GLIBC_2.38` and `GLIBCXX_3.4.31`; this machine (glibc 2.36) refuses to run it. The same binary started via `docker run --entrypoint <binary>` came up in about 0.3 s. So the lifecycle manager runs build **and** run in the pinned image. Static linking (`-static-libstdc++ -static-libgcc`, glibc still dynamic) is a possible later optimization but is not needed and shifts risk onto the build.

| Step | Command shape | Notes |
|------|---------------|-------|
| Build | `docker run --rm -u UID:GID -v <repo>:<repo> -w <yamldir> -e HOME=/tmp ghcr.io/esphome/esphome:2026.10.1 compile fw.yaml` | **[verified]** minimal host config compiles with `--user` and the output is `<build_path>/.pioenvs/<name>/program` (ninja). Keep the build dir on a persistent mount so rebuilds are incremental. |
| Stage | copy `program` to `<rundir>/fw/program` | Decouples the run from the build tree; reproducible per run. |
| Start | `docker run --rm --name sil-<run> --network host -v <rundir>:/run/sil -e SIL_BUS=unix:/run/sil/bus.sock -e ESPHOME_PREFDIR=/run/sil/prefs --entrypoint /run/sil/fw/program <image>` | Attached child of the daemon (not `-d`), so stdout is the log stream and lifetime is tied to the daemon. `--network host` gives HA reach and mDNS on a Linux dev box; `-p 6053:6053` is the fallback. |
| Logs | scan child stdout, strip ANSI, parse `[HH:MM:SS][L][tag:line]`, ring buffer | Exposed as `GET /logs`, a WS topic, `sil logs`. |
| Stop | SIGTERM to the docker CLI, then `docker kill sil-<run>` after a timeout; at start, remove stale `sil-*` containers | Never leave orphans. |
| Restart firmware only | `sil fw restart [--rebuild] [--fresh]` | Simulator keeps running; reconnect + resync handles it. |
| Attach mode | `sil up --no-firmware` | Daemon only; firmware started by something else (pytest, manual). Decouples simulator development from the slow build. |

Unix socket gotchas: the path limit is 108 bytes, so the run dir is `$XDG_RUNTIME_DIR/sil/<run>/` (fallback `/tmp/sil-$UID/<run>/`) and the in-container mount point is a short `/run/sil`. Use the Docker CLI via `os/exec` behind a small `Runner` interface; do not pull in the Docker SDK (heavy dependency, harder to fake).

**Daemon discovery:** `sil up` writes `<rundir>/run.json` (pid, API address, bus path, container name) and a stable pointer (`.sil/current`). `sil set/press/status/logs/down` read it, so agents never pass ports around. The API listens on `127.0.0.1` only (no auth in M1).

---

## Architectural Patterns

### Pattern 1: Single-writer engine (actor)

**What:** One goroutine owns the whole `World`. Bus frames, API commands and timers arrive as messages; the engine processes them in order, steps models, and publishes immutable snapshots.
**When to use:** Always here; the state is small (4 ports, ~10 signals) and determinism matters more than parallelism.
**Trade-offs:** + no locks, deterministic replay, trivial to test with a manual clock. − API calls pay one channel round trip (microseconds).

```go
type Engine struct {
    clk   clock.Clock
    world *World                // only touched by run()
    inbox chan Event            // bus.Received | api.Command | timer.Fired
    snap  atomic.Pointer[state.Snapshot]
}

func (e *Engine) run(ctx context.Context) {
    tick := e.clk.NewTicker(10 * time.Millisecond) // in SIM time
    last := e.clk.Now()
    for {
        select {
        case ev := <-e.inbox:    e.apply(ev)
        case now := <-tick.C():  e.step(now - last); last = now; e.publish(now)
        case <-ctx.Done():       return
        }
    }
}
```

### Pattern 2: Injectable clock, `dt` passed explicitly

**What:** Nothing outside `internal/clock` calls `time.Now`/`time.Sleep` (enforce with `forbidigo`). Models implement `Step(dt time.Duration, in Inputs) Outputs`. First-order lag uses the exact discretization `rpm += (target−rpm)·(1−exp(−dt/τ))`, which is stable for any `dt` (needed when the factor is x100 and `dt` is large).
**When to use:** Every time-dependent piece, including the button-release timer.
**Trade-offs:** Slight ceremony now; avoids the expensive retrofit the PROJECT.md warns about. Tests advance a `ManualClock` and assert exact trajectories without sleeping.

### Pattern 3: State-based, self-resyncing bus protocol

**What:** Frames carry *levels* (duty, rpm, pressed, pixels), not events or deltas; both sides keep a last-value cache; (re)connect triggers a full snapshot both ways.
**When to use:** Always for this bus. It makes restarts, late joiners and dropped connections non-events.
**Trade-offs:** Slightly more bytes than deltas (irrelevant on a unix socket). Genuine events (none in M1) would get an explicit sequence number later.

Illustrative frame layout (finalised in Phase 1 in `protocol/PROTOCOL.md`; hand-rolled little-endian binary, no schema tooling on the C++ side):

```
u16 len | u8 type | u8 flags | u64 t_us | payload
type: HELLO(fw) WELCOME(sim) OUT_F32 OUT_PIXELS IN_F32 IN_BOOL PING/PONG   (+ reserved range)
```

Pin it with golden hex vectors in `protocol/vectors/` that a Go test and a plain-`g++` C++ test both decode. A debug flag (`sil up --bus-trace`) logs decoded frames as JSON so the binary format stays debuggable.

### Pattern 4: Declare in firmware, bind in simulator

**What:** The firmware manifest lists what exists; `bench.toml` + the built-in Rev 3.3 profile say what plant is behind each port. No Rev 3.3 constants in Go code.
**When to use:** From the start; it is what keeps the sim layer chip- and revision-agnostic (ESP32-C6 upcoming).
**Trade-offs:** One more config file; avoided hard-coding is worth it.

### Pattern 5: One `Control` interface, many adapters

**What:** `api.Control` (Status, Set, Press, History, Subscribe, Logs) is implemented by the engine in-process, by the HTTP handlers (server side) and by the HTTP client (CLI side). The UI is the same JSON.
**When to use:** Resolves "CLI as API client vs in-process": the CLI is **always a client** for control commands against a running daemon; only `up`/`down` start/stop the daemon. Go e2e tests and the later `sil run <scenario>` can bind the in-process `Control` to a headless engine without HTTP.
**Trade-offs:** The `api/v1` structs become the single schema; the acceptance rule "everything the page shows is reachable via CLI with the same JSON" holds by construction.

---

## Designing for the Later Time Factor (do not build now)

| Seam | Built in M1 | Added later |
|------|-------------|-------------|
| Sim clock | `Clock` interface; `RealClock{factor=1}` computes `sim = (mono − t0)·factor` | Factor > 1; lockstep clock |
| Models | `Step(dt, …)`, exact discretization, `dt` clamp + sub-stepping hook in engine | Substeps for the thermal model |
| Timers | Engine sim-time timer queue (min-heap), used for button release | Scenario steps, fault injection at `t` |
| Bus timestamps | Every frame header has `t_us`; M1: sender's time, sim records receive time on its own clock and the firmware's `t` only for diagnostics | Firmware `t` becomes authoritative once firmware time is virtual |
| Handshake | `WELCOME` reserves `time_base`/`factor` fields | Carries anchor + factor |
| Firmware clock | Real `CLOCK_MONOTONIC` | HAL patch for `millis/micros/millis_64/arch_get_cpu_cycle_count/delay/delayMicroseconds` and the `select()` timeout in `wake_host.cpp` |
| Build image | `lifecycle.Image` is a config value | Locally derived image (`FROM esphome:2026.10.1` + patch). An external component that overrides the built-in `host` component can replace `hal.cpp`, but `application`/`wake_host.cpp` cannot be overridden that way. |
| Rate | Dirty-flag coalescing in `sim_bus` and `sim_led_strip` | Matters at x100: firmware frame rate per wall second rises by the factor, so coalescing must stay in place |

Note: the 2026.10.1 host select loop lives in `core/wake/wake_host.cpp` (`wakeable_delay`) and `Application::loop()` in `core/application.h`, not only in `application.cpp` as `initial-idea.md` assumed. The patch surface for the time factor must be re-derived from this source in that milestone.

Because both sides will derive time from the same `(anchor, factor)` pair (passed by env var or `WELCOME`), no bus time synchronisation protocol is needed.

## Designing for Later Lua Faults (do not build now)

- **Port pipeline with empty hook lists.** Each port runs `inbound hooks → plant model → outbound hooks`. In M1 the lists are empty. Three tap points cover the planned fault catalogue: *pre-plant* (duty clamp, stuck PWM), *plant parameters* (blocked, worn bearing: mutate `Params`, never the model code), *post-plant/outbound* (tach dead but spinning, wrong PPR: transform the reported RPM).
- **`Hook` interface** `Apply(t SimTime, dt time.Duration, sig *Signals)`. A gopher-lua hook is just another implementation, running on the engine goroutine (an `LState` is not goroutine-safe), seeing time only via `t` (no `os.time`), so Lua stays deterministic and time-factor safe.
- **Port attachment as data:** `Port.Fan *FanInstance` is nilable and params are a value struct, so plug/unplug and "swap fan" are later just `Set` commands.
- **`fans/_faults/` and the virtual supervisor** need no M1 code; the supervisor is an engine component that issues the same `Control` commands as the CLI.
- Scenario files are out of M1; when they arrive they compile to timer-queue entries plus `Control` commands.

---

## Scaling Considerations

Scale here means rate and time factor, not users.

| Scale | Architecture Adjustments |
|-------|--------------------------|
| M1: x1, 4 ports, 1 firmware | Everything above as designed. Bus traffic is tens of small frames per second; 10 ms engine tick is far below any CPU concern. |
| Later: x100 thermal/long runs (headless) | Engine tick cost and bus frame rate scale with the factor. Keep coalescing; engine reports `behind_realtime` / achieved factor; drop the UI/WS for headless runs. |
| Later: lockstep / virtual time | Replace async coupling with a `TIME_GRANT` handshake where the firmware blocks in its patched loop until the sim grants a step. The bus header already carries `t_us`; this is the one place a protocol revision is expected, and the protocol is internal, so it is free to change. |

### Scaling Priorities

1. **First bottleneck:** wall-clock cost of the ESPHome build (ninja compile of a full host binary). Mitigate with a persistent build cache mount, `ccache`, and attach mode so simulator work never waits on a build.
2. **Second bottleneck:** at high factors, firmware chatter per wall second (LED frames, duty writes). Mitigate with the coalescing already in the design.

---

## Anti-Patterns

### Anti-Pattern 1: Blocking socket I/O in setup or loop

**What people do:** `connect()` and `recv()` blocking, or `can_proceed()` waiting forever for the simulator.
**Why it's wrong:** One stalled sim stalls the whole ESPHome main loop (API, scheduler); ESPHome logs blocking warnings and HA sees the device drop.
**Do this instead:** Non-blocking socket, backoff reconnect, drain to `EAGAIN`; boot without a sim; opt-in bounded `wait_for_sim`.

### Anti-Pattern 2: Event/delta protocol

**What people do:** "button pressed" / "duty changed by x" messages.
**Why it's wrong:** A lost or pre-connect event leaves both sides permanently out of sync; restarts need special handling.
**Do this instead:** Level/state frames plus full snapshot on (re)connect (Pattern 3).

### Anti-Pattern 3: Logic or state in the UI or CLI

**What people do:** Hold-time of a "long press" or plot history only in the browser.
**Why it's wrong:** Violates "UI is only a client"; agents and pytest cannot reproduce it; hold time must be in sim time to survive a time factor.
**Do this instead:** Engine owns press timing and history; UI renders `Snapshot` JSON.

### Anti-Pattern 4: `time.Now()` / `time.Sleep()` in models or engine

**What people do:** Quick `time.Since(last)` for `dt`.
**Why it's wrong:** Cannot be tested deterministically; the time factor becomes a rewrite.
**Do this instead:** `clock.Clock` only; lint-ban the `time` functions outside `internal/clock`.

### Anti-Pattern 5: Running the container-built binary on the host

**What people do:** `docker cp program` then `./program`.
**Why it's wrong:** Fails on any host with an older glibc/libstdc++ (verified here: needs `GLIBC_2.38`).
**Do this instead:** Run in the pinned image with a bind-mounted run dir.

### Anti-Pattern 6: Tests or shared code outside the component directory

**What people do:** `components/sim_bus/test_codec.cpp`, or `#include "../../protocol/..."`.
**Why it's wrong:** ESPHome copies only the component dir and compiles every `.cpp` in it.
**Do this instead:** Header-only `sim_protocol.h` inside the component, tests under `tests/cpp/`.

### Anti-Pattern 7: Hard-coding Rev 3.3 or product IDs in the sim

**What people do:** Go constants for `fan1..fan4`, wire keys equal to ESPHome `id:`s.
**Why it's wrong:** Breaks on Rev 3.4/C6 and on any product-YAML refactor; couples consumer interface to internals.
**Do this instead:** Explicit `channel:` names, bench as data, manifest validation.

### Anti-Pattern 8: Simulating tach pulses or driver behaviour

**What people do:** Generate edges to feed `pulse_counter`.
**Why it's wrong:** Already decided out of scope; `host:` cannot run those drivers and it is the HIL's job.
**Do this instead:** Deliver RPM through `sim_sensor`.

### Anti-Pattern 9: Making the simulator a proxy for Home Assistant

**What people do:** Route HA traffic or entity state through the simulator.
**Why it's wrong:** Two sources of truth; breaks "HA connects to the firmware like a real device".
**Do this instead:** HA ↔ native API ↔ firmware only; the sim sees hardware-level signals.

---

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Docker (`esphome/esphome:2026.10.1`) | CLI via `os/exec`, `Runner` interface for tests | Single pinned tag; check it at startup. Needs the image already pulled (local-only constraint). |
| Home Assistant | Firmware's native API on `:6053`, user-chosen instance | Host networking for mDNS discovery; otherwise manual IP. Sim never contacts HA. |
| `wifi-fancontroller` consumer | `external_components` git source pinned by semver tag | Consumer interface = YAML keys of `sim_*`, JSON result of CLI, time semantics, ESPHome pin. |
| pytest / `aioesphomeapi` (product repo) | Drive `sil` via CLI/JSON; talk to firmware via native API on 127.0.0.1 | Attach mode lets pytest own firmware startup if wanted. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `sim_*` components ↔ `sim_bus` | In-process C++ calls (`register_channel`, `set_out`, listener callbacks) | All main-thread; listeners called from `sim_bus::loop()`. |
| `sim_bus` ↔ `internal/bus` | Unix socket, binary frames | Protocol internal and versioned by a `proto_ver` in HELLO/WELCOME; mismatch is refused with a clear log. |
| `internal/bus` ↔ `engine` | Go channels (`Received` events in, `Send` calls out) | Bus never reads `World`. Engine never touches sockets. |
| `engine` ↔ `api` / `state` | Command channel in (`Do`); immutable `Snapshot` out | The only cross-goroutine data is messages and immutable snapshots. |
| `api` ↔ UI / CLI | HTTP JSON + WebSocket on `127.0.0.1` | `/api/v1`, one schema. |
| `lifecycle` ↔ `engine` | `lifecycle` publishes phase/log events; connection state comes from the bus | "Firmware connected" is a bus fact, not a process fact. |

---

## Suggested Build Order (dependencies between components)

```
protocol spec + golden vectors ─┬─▶ C++ sim_bus client + codec ──▶ sim_output, sim_sensor ──▶ sim_binary_sensor, sim_led_strip
                                └─▶ Go bus.Server ──────────────┐
clock ──▶ model (fan, env) ──▶ engine ◀────────────────────────┘
                                 │                            lifecycle (Docker build/run/logs) ── independent after Docker proof
                                 ├──▶ state (Snapshot, Hub, history) ──▶ api Control + HTTP ──▶ CLI ──▶ WS ──▶ static UI
                                 └──▶ example firmware + e2e contract test (needs components + engine + lifecycle)
```

Coarse phases the roadmap can adopt (each leaves something runnable):

1. **Wire and toolchain proof.** Protocol doc + golden vectors; Go `bus.Server` with a logging stub; C++ `sim_bus` + `sim_output` + `sim_sensor` with one port; `lifecycle` can build and run the example in the pinned image; `sil up --no-firmware` plus a trace. *Exit:* firmware logs "connected", a duty change shows in the trace, a value injected from Go appears as a sensor state (read via aioesphomeapi). Retires the biggest unknowns (Docker mounts, UDS across the mount, external component build) first.
2. **Engine and fan model.** `clock`, `model`, `fanlib`/`fan.toml`, `config` (bench), engine actor, snapshots; wired to the bus. Pure Go, can run **in parallel** with phase 1 against a fake bus. *Exit:* headless closed loop duty → RPM with deterministic `ManualClock` tests plus a live loop on the real bus.
3. **Full Rev 3.3 IO surface.** 4 ports, `sim_binary_sensor` (USR1-3), `sim_led_strip` (5 LEDs, `partition` works), env sensors, complete example `hardware-sim.yaml` (without `wifi:`), prefs/fresh handling, HA connection check. *Exit:* the example firmware compiles and exercises every channel; HA can see fans, RPM, LEDs, buttons, temperature.
4. **API and CLI (first citizen).** `Control`, `/api/v1`, `run.json` discovery, `sil up|down|status|set|press|logs`, log capture, `--fresh`/`fw restart`, button timers on the sim-time queue. *Exit:* an agent can run a full closed-loop session without any UI.
5. **Web UI.** `state.Hub` + WebSocket, history endpoint, static page (ports, uPlot plots, virtual LEDs, short/long press, run info, behind-realtime warning). *Exit:* acceptance rule "everything on the page is reachable via CLI with the same JSON" checked by a test.
6. **Contract hardening and release.** `sil_e2e` contract test (build example, closed loop, press, LED assert), ESPHome-pin guard, consumer-interface README, `v0.1.0` tag.

Ordering rationale: phase 1 front-loads the integration risks (container, socket mount, external component build). Phase 2 is independent compute and can overlap. API before UI because the UI must be a pure client. The `Control` interface should be sketched during phase 2 so the engine's command shape is not retrofitted. Time and Lua seams (clock injection, timer queue, empty hook lists, `t_us` header) are phase 2/1 work items, not separate phases.

### Research flags for phases

- **Phase 1:** needs a short spike on three items: same-path bind mount with `type: local` external components inside the container; unix socket through the bind mount with `--network host`; incremental rebuild time with a persistent build dir (measure it).
- **Phase 3:** `sim_led_strip` against `AddressableLight`/`ESPColorView` internals (read `esp32_rmt_led_strip/led_strip.cpp` as the template); `wifi:` replacement hooks in the example; `ESPHOME_PREFDIR` semantics with `restore_value`.
- **Phase 5:** standard patterns; WebSocket library choice is the only decision.
- **Time-factor milestone (later):** needs real research; the patch surface moved (`core/wake/wake_host.cpp`), plus clock-anchor sharing across the Docker boundary.
- **Phases 2 and 4:** standard Go patterns, unlikely to need research.

---

## Sources

- ESPHome 2026.10.1 source (local, `wifi-fancontroller/.worktrees/hil/hil/.venv/.../esphome`): `core/application.h` (`loop()`), `core/wake.h`, `core/wake/wake_host.h|.cpp` (`wake_register_fd`, `wakeable_delay`), `core/component.h` (`setup_priority`), `components/host/{__init__.py,hal.cpp,preferences.cpp}`, `host/toolchain.py`, `components/external_components/__init__.py`, `components/light/addressable_light.h`, `components/esp32_rmt_led_strip/led_strip.h`, `components/partition/light.py`, `components/wifi/__init__.py`, `components/mdns/__init__.py` (HIGH)
- Docker spikes with `ghcr.io/esphome/esphome:2026.10.1` run on 2026-11-08: minimal host build and run; glibc/libstdc++ incompatibility with the host; `ledc` rejected on host; `wifi:` passes `config` but fails `compile` (HIGH; scratch files in the session scratchpad, not in the repo)
- `/home/claude/esphome-sil/.planning/PROJECT.md`, `docs/initial-idea.md`, `/home/claude/wifi-fancontroller/hardware-rev-3.1.yaml` (project intent and IO surface; HIGH)
- Go actor, snapshot and `go:embed` patterns, WebSocket library choice (`github.com/coder/websocket`), `forbidigo` lint rule: general practice, not re-verified against current docs (MEDIUM)

---
*Architecture research for: SiL test bench for ESPHome firmware*
*Researched: 2026-11-08*

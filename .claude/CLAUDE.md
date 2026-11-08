<!-- GSD:project-start source:PROJECT.md -->

## Project

**esphome-sil**

A software-in-the-loop test bench ("labcar") for ESPHome firmware. The ESPHome `host:` platform runs firmware on Linux; simulated IO components (`sim_*`) connect it over a local sim-bus to a plant simulator, so control loops can be exercised closed-loop, locally and fast, before firmware reaches real hardware. It is for the developer and for coding agents; the first domain is the WiFi Fan Controller (Rev 3.3 as reference).

**Core Value:** A simulated fan controller runs closed-loop on the developer's own machine: real ESPHome firmware drives simulated fans, and the result is observable and controllable, with no hardware and no cloud.

### Constraints

- **Locality**: everything runs on own hardware and own code — a vendor shutdown must not kill the pipeline.
- **Tech stack**: simulator, CLI and web server in Go (single static binary, embedded UI); no Python in the simulator. Product-repo tests may stay pytest and drive `sil` via CLI/JSON.
- **ESPHome version**: pinned 2026.10.1 (matches the product repo); build via `esphome/esphome:2026.10.1` Docker image.
- **Web UI**: no build step, no external CDN at runtime; vendored plot library (uPlot leaning).
- **Chip-agnostic sim layer**: ESP32-S2 is the core, ESP32-C6 upcoming.
- **Consumer interface**: semver tags, `0.x` while settling; sim-bus protocol, web UI, `fan.toml` schema and Lua API are internal.
- **License**: GPL-3.0.

<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->

## Technology Stack

## How this was verified

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| Go | **1.27.3** (go.mod: `go 1.27`) | Simulator, plant models, sim-bus server, CLI `sil`, HTTP/WS server | Current stable (1.26.10 is the other supported line). Static single binary with `CGO_ENABLED=0`, stdlib covers HTTP routing, `embed`, `slog`, JSON, Unix sockets, process control. Go 1.27 makes `encoding/json` v2-backed with the same API (stricter on duplicate keys and invalid UTF-8, fine for an internal protocol). **Go is not installed on this machine**: first task is installing it (see Installation). | HIGH |
| ESPHome | **2026.10.1** (image `ghcr.io/esphome/esphome:2026.10.1`, digest `sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64`, amd64) | Firmware build toolchain and `host:` runtime | Pinned by constraint. 2026.10.2 already exists on PyPI; do not follow it, the pin matches the product repo and the future time-factor patch. Pin by digest as well as tag in `compile.sh`. The same name exists on Docker Hub (`esphome/esphome:2026.10.1`, verified present); the ghcr.io one is what is verified locally. | HIGH |
| ESPHome `host:` platform | (part of 2026.10.1) | Runs real firmware logic on Linux | **[source]** In 2026.10.1 the host build no longer uses PlatformIO: `esphome/host/toolchain.py` + `build_gen/host.py` generate a `build.ninja` and use the machine compiler (gcc 14.2 in the image). Output is `.esphome/build/<name>/.pioenvs/<name>/program`. Works with `--network none` when no registry libraries are needed **[verified]**. | HIGH |
| Unix domain socket + NDJSON | n/a | Sim-bus between C++ components and Go | See "Sim-bus protocol decision" below. | HIGH |
| uPlot | **1.6.32** (MIT) | Live time-series plots in the browser | ~51 KB min JS + 1.9 KB CSS, canvas, built for streaming; dual y-axis (duty % vs RPM) is native. npm last modified 2025-03 (stable, low-churn, not abandoned). | HIGH |
| Alpine.js | **3.17.5** (MIT) | Declarative DOM binding for the UI | One vendored file (~56 KB), no build, no virtual DOM, good fit for "snapshot arrives over WS, bind to cards". See rationale and the pitfall about reactive proxies below. | MEDIUM |

### Supporting Libraries (Go)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/alecthomas/kong` | **v1.17.0** (2026-10-30) | CLI (`sil up/down/status/set/press/run/logs`) | Always. Struct-tag grammar, `Run(ctx)` per command, `env:"SIL_ADDR"` binding, positional args and `key=value` maps. The CLI is a thin JSON client of ~10 commands; a declarative struct is the least code and trivially testable via `kong.Must(&cli).Parse(args)`. |
| `github.com/coder/websocket` | **v1.8.15** (2026-06-15; repo pushed 2026-11-06) | `/ws` live state stream to the UI | Always. Context-first API, zero dependencies, **concurrent writes are safe** (internal `writeFrameMu`), `CloseRead(ctx)` for write-only fan-out connections, same-origin check by default (`OriginPatterns` to widen). |
| `net/http` stdlib `ServeMux` | Go 1.27 | Routing: `GET /api/v1/status`, `POST /api/v1/ports/{port}/fan`, `GET /ws`, static UI | Always. Method + wildcard patterns (since 1.22) cover everything needed; no router dependency. |
| `github.com/pelletier/go-toml/v2` | **v2.4.3** (2026-07-05) | `fan.toml`, scenario files (later) | Always. `NewDecoder(r).DisallowUnknownFields()` rejects typos like `max_rmp` instead of silently ignoring them; `DecodeError` carries a human-readable contextualized message with line and underline. Fan files are edited by humans and agents, so strict, located errors matter. Supports TOML 1.1. |
| `log/slog` stdlib | Go 1.27 | Logging | Always. JSON handler when `--json` / non-TTY, text handler on a TTY. **stderr only**: stdout is reserved for the CLI's JSON result. |
| `embed` stdlib | Go 1.27 | Ship UI in the binary | `//go:embed all:web` + `fs.Sub(webFS, "web")`. Add a `--ui-dir` dev flag that serves from disk so UI edits need no Go rebuild. |
| `testing` + `testing/synctest` stdlib | Go 1.27 | Unit tests; deterministic time tests for the injectable clock | Always. No testify needed. `synctest` gives a fake-time bubble for goroutine/timer tests (Go 1.27 adds `synctest.Sleep`). |
| `os/exec`, `os/signal`, `net` stdlib | Go 1.27 | Spawn/stop the firmware `program`, Unix socket listen | `exec.CommandContext` + `SysProcAttr{Setpgid: true}` so `sil down` kills the process group; SIGTERM first (firmware flushes prefs on SIGTERM/SIGINT **[source]**), SIGKILL after a grace period. |
| `github.com/yuin/gopher-lua` | v1.1.2 (2026-04-01) | Lua fault/behaviour scripts | **P2 only. Do not add in M1.** Lua 5.1 semantics, mature, context cancellation and memory limit hooks. Last push April 2026, 122 open issues: stable-quiet. Alternative `arnodel/golua` v0.4.1 (2026-11-03, very active) implements Lua 5.3 with CPU/memory quotas but labels its safe-execution mode alpha. Decide with a one-day spike at the start of P2. |

### Supporting Libraries (browser, vendored)

| Library | Version | Files to vendor | Notes |
|---------|---------|-----------------|-------|
| uPlot | 1.6.32 | `dist/uPlot.iife.min.js` (51,081 B), `dist/uPlot.min.css` (1,857 B) | Verified present on jsDelivr. Use ring buffers of `Float64Array` (e.g. 60 s window) and `u.setData()` on a `requestAnimationFrame` cadence, not per WS message. |
| Alpine.js | 3.17.5 | `dist/cdn.min.js` (55,935 B) | Load with `<script defer src="/vendor/alpine.min.js">`. |

### ESPHome-side components (what to write)

| Entity (Rev 3.3) | C++ base classes | Python schema/helpers | Status |
|------------------|------------------|-----------------------|--------|
| Hub (socket client) | `Component` | `cv.Schema({GenerateID(): declare_id(SimBus), Optional("path")}).extend(cv.COMPONENT_SCHEMA)` | **[verified]** |
| PWM out `ledc` replacement (fan1-4) | `output::FloatOutput` + `Component`; override `write_state(float)` | `output.FLOAT_OUTPUT_SCHEMA`, `output.register_output(var, config)` | **[verified]** Drives built-in `fan: platform: speed` with `speed_count: 100`; HA command "speed 40" arrived on the bus as `fan1_pwm 0.4`. `inverted`, `min_power`, `max_power` come free from the base class. |
| RPM, temperature, humidity | `sensor::Sensor` + `Component` (not `PollingComponent`; the simulator pushes) | `sensor.sensor_schema(Cls, ...)`, `await sensor.new_sensor(config)` | **[verified]** `publish_state(v)` reached HA API as `SensorState 1234.0`. Sensor `filters:` still work (they live in the base class). |
| Buttons USR1-3 | `binary_sensor::BinarySensor` + `Component`; `publish_state(bool)` | `binary_sensor.binary_sensor_schema(Cls)`, `await binary_sensor.new_binary_sensor(config)` | **[verified]** `on_click`, `on_multi_click`, `delayed_on` filters live in the base class. The simulator sends *level* (pressed/released); the firmware measures hold time itself, so "long press" = hold for N sim-seconds. |
| Status LED strip (5 LEDs) | `light::AddressableLight` (already a `Component`): implement `size()`, `clear_effect_data()`, `get_traits()`, `write_state(LightState*)`, `get_view_internal(i)` returning `ESPColorView{r,g,b,nullptr,&effect[i],&correction_}` | `light.ADDRESSABLE_LIGHT_SCHEMA`, `light.register_light(var, config)`, `cg.register_component` | **[verified]** Built-in `partition` lights (board status with `pulse` and `addressable_rainbow` effects, per-fan segments) compile and attach on top of it unchanged. Keep an internal RGB buffer; no `channel_colors`, `chipset`, `pin`, `rmt_symbols` in the sim schema (hardware-only). |
- Raw POSIX `AF_UNIX`, `SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC`. Non-blocking `connect()` on a Unix socket completes or fails immediately, so there is no connect state machine: try once in `setup()`, retry every 500 ms from `loop()` while disconnected. ESPHome's `socket::` wrapper is IP-only (`socket_ip`, v4/v6 helpers); do not use it.
- Register the fd with the host main loop so idle `select()` wakes on inbound data instead of waiting for the loop interval:
- `send(..., MSG_NOSIGNAL | MSG_DONTWAIT)` with an application TX buffer; on `EAGAIN` keep the remainder. (Host `main()` already sets `SIGPIPE` to ignore **[source]**, so this is belt and braces.)
- On (re)connect, send `hello` and replay the last known value of every output and light so a simulator restart or firmware restart re-converges.
- **Dedupe outbound state.** In the prototype, 66 identical all-black LED frames were sent in about 5 s of idle. The light (and outputs) must send only on change, and cap the rate.
- Validate in Python codegen that the socket path is at most 107 bytes (`sun_path` limit). The prototype's `strncpy` truncates silently, which would produce a confusing connect failure for deep project paths.

## Sim-bus protocol decision

| Question | Decision | Why |
|----------|----------|-----|
| UDS vs TCP localhost | **UDS** at `$XDG_RUNTIME_DIR/sil/<run>/bus.sock` (fallback `/tmp/sil-<uid>/<run>/`) | No port collisions between parallel runs or agents, filesystem permissions instead of a network listener, works on the host and across a Docker bind mount (same kernel) if the firmware ever runs in a container. TCP would need `--network host` or port mapping in that case. Keep the path a config string. |
| Who listens | **Simulator listens, firmware connects** | The firmware restarts (crash, host OTA re-exec, test restart) and must not own the rendezvous. Reconnect with 500 ms retry was **[verified]**. |
| NDJSON vs protobuf vs binary | **NDJSON**, flat objects only | Traffic is tiny (a handful of channels, change-driven). NDJSON is debuggable with `socat - UNIX-CONNECT:bus.sock` and `jq`, readable by agents, and free to evolve. Protobuf adds protoc, a Go plugin and a C++ runtime (ESPHome's own protobuf is internal to its API component and not reusable). Fixed binary frames save nothing measurable here and lose inspectability. |
| JSON on the C++ side | **Hand-written flat parser/writer (~100 lines)**, not ESPHome's `json:` component | **[verified]** `json:` pulls ArduinoJson from the PlatformIO registry at build time and the offline build (`--network none`) **fails** with an HTTP client error. A hermetic, vendor-independent build is a project constraint. The grammar is restricted so a trivial parser is correct: one object per line, string and number scalars only, no nesting, no arrays, no escapes beyond `\"` and `\\`. LED pixel data is a hex string (`"rgb":"ff000000ff00..."`). |
| Go side | `encoding/json` with `bufio.Scanner`/`json.Decoder` | Full JSON on the Go side is fine; Go only emits the restricted subset. |
| Timestamps | Every message carries `t` (microseconds, int64): firmware monotonic clock on fw->sim, sim time on sim->fw | Satisfies the injectable-clock and sim-timestamp requirement from day one. At x1 they are equal; the time factor later only changes the mapping. |
| Framing/versioning | `{"k":"hello","proto":1}` both ways on connect; kinds `hello`, `out`, `in`, `bye`; max line 4 KiB | Internal protocol, free to change; the version integer lets a mismatched firmware fail loudly. |
| Conformance | Shared `testdata/bus_vectors.ndjson` exercised by Go tests and by a small host-compiled C++ test of the parser | The only real risk of a hand-rolled parser is drift between the two sides. |

## Build and runtime: where the firmware runs

| Build | Result on the host |
|-------|--------------------|
| Default (dynamic) `program` from the image | **Fails**: `libm.so.6: version 'GLIBC_2.38' not found` and `libstdc++.so.6: version 'GLIBCXX_3.4.31' not found`. A dynamically linked binary from the image does not run on a Debian 12 host, and would not on older Ubuntu LTS either. |
| `esphome: build_flags: [-static]` | **Runs** (3.4 MB, fully static ELF). `-static` is routed to both compile and link lines by `build_gen/host.py` **[source]**. |

# -> .esphome/build/<name>/.pioenvs/<name>/program

- The firmware's native API listens on `0.0.0.0:6053` **[verified with `ss`]**. Any HA instance on the LAN reaches `<sim-host-ip>:6053` directly. No Docker port mapping involved.
- **mDNS is a stub on host** (`mdns_host.cpp` setup does nothing **[source]**), so HA will not auto-discover it. In HA: Settings, Devices, Add Integration, ESPHome, enter host and port manually. `sil up` should print this hint and the `host:port`.
- The simulator socket is a local path; no networking at all.
- The native API is plaintext unless `api: encryption:` is set, and it is bound to all interfaces. Acceptable for the stated use; say so in docs and let `sil up` take `--api-port`. CI runners must firewall it.
- If static linking ever breaks (e.g. a future component needing NSS/DNS): fallback is to run `program` inside the same image with `-v` of the run directory (UDS works across a bind mount) and `-p <port>:<port>` or `--network host`. Do not build a custom Debian 12 builder image; the pinned ESPHome image is the contract.
- The same approach should hold on aarch64 hosts (the image is multi-arch; static output matches the Docker platform). Linux only: the host platform refuses to build on Windows **[source]** and a Linux container cannot produce a macOS binary.
- Preferences are written to `$ESPHOME_PREFDIR` or else `$HOME/.esphome/prefs/<name>.prefs` **[source]**; my test runs created `~/.esphome/prefs` in the real home directory. `sil up` must set `ESPHOME_PREFDIR=<run dir>` so runs do not leak state into each other or into `$HOME` (restore_mode fans and `init_done` globals depend on this).
- `api: port:` and `host: mac_address:` must be unique per concurrent instance. The default MAC is the constant `98:35:69:ab:f6:79`, so two sims (or a sim restarted with a different name) collide in HA's device registry.

## Browser stack decision

- The UI is a pure client of the JSON API: `GET /api/v1/state` for the first paint, `WS /ws` for state snapshots, `POST` for set/press. It has no logic of its own, which is the project's acceptance rule.
- Alpine fits "bind a snapshot object to cards" with one file and no build. The first plot data path is imperative uPlot anyway.
- **Pitfall:** do not keep uPlot instances or the ring buffers inside Alpine `x-data`; Alpine wraps data in reactive proxies, which slows and can break canvas libraries. Keep charts and typed arrays in module scope (or `Alpine.raw`) and put only display values in reactive state.
- Fonts: system font stack only; no Google Fonts, no CDN anywhere (check the final HTML for any `http(s)://` references in CI).
- If P3 (board view, drag and drop, photos) outgrows Alpine, move to **Preact + htm via an import map** (still no build; Preact is at 11.2.0, htm 3.1.1). That is a UI-only swap behind the same API.

## Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `go vet`, `gofmt`, `staticcheck` (optional) | Lint/format | Go 1.27 changed `gofmt` column alignment slightly; run `gofmt` once after the toolchain bump. |
| `esphome config <yaml>` via the pinned image | Fast YAML/codegen validation | ~0.8 s; run it in the contract test before `compile`. |
| `socat`, `jq` | Inspect the bus by hand | `socat - UNIX-CONNECT:<run>/bus.sock` shows the NDJSON live. |
| `aioesphomeapi` (pytest side, Python) | Product-repo and contract tests talk to the firmware's native API | Image ships 46.12.0; PyPI latest is 46.28.0. Matches HA's behaviour closely enough (HA uses the same library). Use it only for "entities are visible and commandable"; all stimulus and observation of the plant goes through `sil` CLI/JSON, not through the API. Relevance is deliberately small. |
| Contract test | Build example YAML, `sil up`, drive a scenario, assert JSON verdict | The M1 acceptance gate; run it in CI-like mode headless. |

## Installation

# Go toolchain (not currently installed on this host; amd64, verified checksum from go.dev/dl JSON)

# arm64: go1.27.3.linux-arm64.tar.gz  a06e99309dbcbdca1a8ccd2282c3ee146e55930886e26b4f1193269587165f49

# Go module

# P2 only: go get github.com/yuin/gopher-lua@v1.1.2

# Firmware toolchain

# Browser vendoring (deliberate, one-off per bump; writes web/vendor/ + VENDOR.md)

# Release build of the simulator

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| kong v1.17.0 | cobra v1.10.2 (last release 2025-12) | If shell completion out of the box and the broadest example coverage matter more than less boilerplate. Perfectly acceptable; the CLI grammar is small enough that switching costs an hour. |
| kong | urfave/cli v3.14.0 | v3 is a recent rewrite with flag/API churn between minors; no reason to prefer it here. |
| coder/websocket v1.8.15 | gorilla/websocket v1.5.3 (last release 2024-06, last push 2025-03) | Only if code must be copied from an existing gorilla-based project. It is not broken, but it has no context API and needs a hand-written write mutex per connection. |
| stdlib `ServeMux` | chi v5.5.0 (2026-10-14) | If middleware stacks, route groups, or sub-routers become real. For ~10 routes, `mux.HandleFunc("POST /api/v1/ports/{port}/fan", h)` is enough. |
| go-toml v2 v2.4.3 | BurntSushi/toml v1.6.0 (2025-12) | Equally solid TOML 1.0 parser; its `MetaData.Undecoded()` can also flag unknown keys but without the contextual error rendering. Choose it only if `toml.Primitive` deferred decoding is needed for polymorphic fault profiles (possible in P2). |
| uPlot | Chart.js 4.6.1 | Heavier, needs a time adapter for time axes; better when you want many chart types and animations (P3 eye candy). Not for a 20 Hz streaming plot. |
| uPlot | dygraphs 2.2.2, smoothie 1.36.1 | dygraphs is old and heavy; smoothie is tiny but has weak axes and no dual-scale. |
| Alpine.js | vanilla JS only | Fine for M1 if you want zero UI dependencies besides uPlot; gets messy as soon as P2 adds fault switches and plug/unplug state. |
| Alpine.js | Preact + htm (import map) | P3, when the UI becomes componentised. |
| Hand-rolled flat NDJSON in C++ | ESPHome `json:` component (ArduinoJson 7.4.3) | Only if the build is allowed network access to the PlatformIO registry; **verified to fail offline**. Or vendor ArduinoJson's single header into the component folder if the protocol ever needs nested JSON. |
| Static host binary | Run `program` in the container | If static linking breaks; bind-mount the run dir and use `--network host`. |
| gopher-lua (P2) | arnodel/golua v0.4.1 | If CPU/memory quotas on user scripts matter more than Lua 5.1 familiarity; decide in a P2 spike. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| Dynamic `program` copied out of the image | **[verified]** needs GLIBC 2.38 / GLIBCXX 3.4.31, fails on Debian 12 | `build_flags: [-static]` |
| `esphome: platformio_options: build_flags:` | Deprecated in 2026.10.1, removed in 2026.12.0 (warning printed during the test build) | `esphome: build_flags:` |
| ESPHome `json:` component or any component that `cg.add_library()`s | **[verified]** requires network at build; breaks the hermetic/local constraint. Watch AUTO_LOAD chains when real product modules are added later (e.g. `web_server`, `mqtt` pull `json`). | Hand-rolled flat parser; keep the SiL top-level YAML free of components that auto-load `json` |
| `PollingComponent` for sim sensors | Sim values are pushed; `update_interval` is rejected on plain components **[verified]** | `sensor::Sensor` + `Component`, `publish_state` from the bus handler |
| ESPHome `socket::` wrapper for the bus | IP-only helpers (`socket_ip`, v4/v6 sockaddr) | Raw `AF_UNIX` sockets + `wake_register_fd` |
| TCP localhost for the bus | Port collisions in parallel runs, needless network surface | Unix domain socket |
| Protobuf / gRPC / FlatBuffers for the bus | Toolchain weight for ~dozens of messages/s; loses `socat`/`jq` debuggability | NDJSON |
| htmx for the UI | Its model is server-rendered HTML fragments; the Go server would then own UI rendering, giving the UI a second surface that the CLI cannot reach, violating "the page is only a client of the same API" | Alpine + JSON/WS |
| petite-vue | Unmaintained (0.4.1) | Alpine |
| Any CDN, Google Fonts, `unpkg` at runtime | Locality constraint | Vendored files with recorded sha256 |
| `testify`, Gin/Echo/Fiber, Viper, Cobra+Viper | Weight and indirection with no benefit for this scope | stdlib `testing`, `ServeMux`, kong + env tags |
| Fixed `fw` port 6053 and fixed MAC across runs | Collides in HA and in parallel tests | Per-run `api: port:` and `host: mac_address:` |
| Python in the simulator | Constraint | Go |
| Wokwi / QEMU | Constraint (cloud / no ESP32-S2 machine) | `host:` platform |

## Stack Patterns by Variant

- `sil up --headless --no-ui` (or the UI simply unused), unique `--api-port`, `ESPHOME_PREFDIR` and bus path under a per-run temp dir, `sil down` kills the process group.
- Because the clock is injectable and `t` is on every bus message, the time factor is a later swap of the clock plus the ESPHome patch, not a rewrite.
- `sil up --api-port 6053 --mac <stable>`; add the device manually in HA by host:port (no mDNS). Use a stable MAC so HA keeps one device across restarts.
- `components/host/hal.cpp`: `millis`, `millis_64`, `micros`, `arch_get_cpu_cycle_count` (all `clock_gettime(CLOCK_MONOTONIC)`), `delay`, `delayMicroseconds` (`nanosleep`), as listed in PROJECT.md. **[source]**
- **Correction to PROJECT.md:** the main-loop `select()` timeout is **no longer in `core/application.cpp`**. In 2026.10.1 it is `esphome::internal::wakeable_delay()` in `esphome/core/wake/wake_host.cpp`, called from the application loop, and `Application::loop()` reads time through `MillisInternal::get()` which delegates to `millis()` on host. The patch must scale that `select()` timeout and `time_t`-based wall clock (`core/time.cpp` `::time(nullptr)`) deliberately.
- Apply the patch by a derived image: `FROM ghcr.io/esphome/esphome:2026.10.1` + `patch`; keep the version check that refuses any other ESPHome version.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| Go 1.27.3 | all listed Go modules | `coder/websocket`, `kong`, `go-toml/v2` are plain modules with no cgo; `CGO_ENABLED=0` static build works. |
| Go 1.27 `net.UnixConn` | sim-bus reader | Read methods now return `io.EOF` directly (not wrapped in `*net.OpError`); use `errors.Is(err, io.EOF)`. |
| Go 1.27 `encoding/json` (v2-backed) | NDJSON bus | Stricter: rejects duplicate object names and invalid UTF-8. Fine; the C++ side never emits them. Do not set `GOEXPERIMENT=nojsonv2` (documented as going away). |
| ESPHome 2026.10.1 | `external_components` `type: local` / `github://zeroflow/esphome-sil@vX.Y` | Component Python must use 2026.10 helper names: `output.register_output`, `sensor.new_sensor`, `binary_sensor.new_binary_sensor`, `light.register_light`, `light.ADDRESSABLE_LIGHT_SCHEMA`. All **[verified]** to compile. |
| ESPHome 2026.10.1 `light` | `esp32_rmt_led_strip` `channel_colors` | `channel_colors` needs 2026.8.0+ (hardware YAML `min_version`); the sim light has no channel order. `light.migrate_channel_colors` removal is scheduled for 2027.3.0. |
| `aioesphomeapi` 46.12.0 (image) / 46.28.0 (PyPI) | ESPHome 2026.10.1 native API | Connected, listed entities, subscribed states and sent a fan command **[verified]** with the image's 46.12.0. |
| uPlot 1.6.32 | Alpine 3.17.5 | No interaction; keep uPlot outside Alpine reactivity. |

## Sources

- Local image `ghcr.io/esphome/esphome:2026.10.1` (source read and built against): `esphome/host/toolchain.py`, `esphome/build_gen/host.py`, `components/host/{__init__.py,hal.cpp,core.cpp,preferences.cpp}`, `core/wake.h`, `core/wake/wake_host.{h,cpp}`, `core/millis_internal.h`, `components/{output,light,partition,sensor,binary_sensor,esp32_rmt_led_strip,json,mdns,socket}` : HIGH (primary source plus execution)
- Prototype runs (scratchpad): minimal host build, static build, `sim_bus` component set, runtime round trip via `aioesphomeapi`, offline (`--network none`) incremental build, offline failure of `json:` : HIGH (observed)
- Go module proxy `proxy.golang.org/<module>/@latest` for cobra v1.10.2, urfave/cli v3.14.0, kong v1.17.0, coder/websocket v1.8.15, gorilla/websocket v1.5.3, chi v5.5.0, go-toml v2.4.3, BurntSushi/toml v1.6.0, gopher-lua v1.1.2, golua v0.4.1 : HIGH
- `https://go.dev/dl/?mode=json` (Go 1.27.3 stable, 1.26.10; tarball sha256) and `https://go.dev/doc/go1.27` (json v2 backing, `UnixConn` EOF change, `synctest.Sleep`, removed GODEBUGs) : HIGH
- `https://registry.npmjs.org/<pkg>/latest` for uplot 1.6.32, alpinejs 3.17.5, htmx.org 2.0.11, preact 11.2.0, htm 3.1.1, chart.js 4.6.1, petite-vue 0.4.1; jsDelivr file sizes for the vendored files : HIGH
- Context7 `/coder/websocket` (Accept options, `CloseRead`, concurrency guarantees) and `/alecthomas/kong` (`Run` methods, `env` tags, positional args) : HIGH
- `https://raw.githubusercontent.com/pelletier/go-toml/v2/README.md` (strict mode via `DisallowUnknownFields`, contextualized `DecodeError`, TOML 1.1.0) : MEDIUM (type names `StrictMissingError` not confirmed from this page)
- `https://raw.githubusercontent.com/arnodel/golua/master/README.md` (Lua 5.3, alpha quotas) : MEDIUM
- BurntSushi/toml `Undecoded()` behaviour, cobra completion claims, Lua 5.1 specifics of gopher-lua : training knowledge, LOW-MEDIUM; none of them drive an M1 decision

<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->

## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->

## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->

## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->

## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->

## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->

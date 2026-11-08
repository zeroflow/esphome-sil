# Pitfalls Research

**Domain:** Software-in-the-loop test bench for ESPHome firmware (ESPHome 2026.10.1 `host:` platform, `sim_*` external components, Go simulator + sim-bus, HA via native API, embedded web UI)
**Researched:** 2026-11-08
**Confidence:** HIGH for ESPHome host-platform, build and Docker findings (reproduced against the pinned `ghcr.io/esphome/esphome:2026.10.1` image and its exact source tree). MEDIUM for sim-bus, model and web-UI findings (domain reasoning, not yet prototyped). MEDIUM for HA findings (HA integration source read, no live HA run).

**Evidence tags used below**
- `[VERIFIED]` reproduced here: compiled, ran or `config`-checked against the pinned 2026.10.1 image or its source.
- `[SOURCE]` read in the 2026.10.1 ESPHome source (or HA `dev` branch source), not executed.
- `[REASONED]` engineering judgment from the domain, to be confirmed by the phase that owns it.

**Phase labels.** The roadmap does not exist yet, so the mapping uses these suggested labels. The synthesizer should rename them to the real phases.
- **A** Build and run foundation: pinned image, host build, run harness
- **B** Sim-bus plus `sim_*` components
- **C** Plant model, injectable clock, CLI/JSON API
- **D** HA integration
- **E** Web UI
- **F** Contract test and example firmware hardening
- **Later** Time factor, product-module coverage

---

## Host-platform compatibility matrix (verified on 2026.10.1)

This answers "what is unavailable or different on `host:`". The key lesson: **`esphome config` passing means almost nothing on host.** Several things pass `config` and then crash codegen, fail the compile, or compile and silently do nothing. The only trustworthy gate is a full `esphome compile` plus a run.

| Component (Rev 3.3 usage) | `config` | `compile` | Runtime on host | Consequence for SiL |
|---|---|---|---|---|
| `esp32:` block | fails: "Found multiple target platform blocks: host, esp32" | n/a | n/a | SiL top-level YAML cannot reuse any firmware YAML that declares `esp32:`. |
| `output: ledc` | fails: "requires component esp32" | n/a | n/a | Replace with `sim_output` (float output). |
| `sensor: pulse_counter` | passes | compiles | **Silent zero.** Software counter, ISR is never fired. Logs `Retrieved counter: 0.00 pulses/min` forever. | Looks like a stalled fan, not a missing driver. Must be replaced, never left in. |
| `binary_sensor: gpio` | passes | compiles | **Frozen.** `HostGPIOPin::digital_read()` returns the constant `inverted_`. `attach_interrupt` only logs. | Buttons never change. Replace with `sim_binary_sensor`. |
| `light: esp32_rmt_led_strip` | fails: "requires component esp32" | n/a | n/a | Replace with a `sim_*` addressable light. |
| `light: partition` | no platform restriction found in schema | not tested | not tested | Expected to work on any `AddressableLight` parent. Verify early, see Pitfall 7. |
| `i2c:` with `sda`/`scl` | fails: "'sda' and 'scl' are not supported on host platform; use 'device' instead" | n/a | `device:` form compiles, would open a `/dev/i2c-*` node | `hdc1080` cannot work. Replace temperature/humidity with `sim_sensor`. |
| `wifi:` | **passes** | **crashes codegen**: `KeyError: 'power_save_mode'` in `wifi/__init__.py:710` | n/a | Hardware-rev-3.1 contains `wifi: on_connect/on_disconnect` that drive the status LED. The package cannot be included. |
| `captive_portal`, `esp32_improv` | fail: "requires component wifi" | n/a | n/a | Omit. |
| `improv_serial` | fails: "requires uart_id" on host | n/a | n/a | Omit. |
| `web_server` | fails: "only available on esp32/esp8266/bk72xx/..." | n/a | n/a | Omit. `web_server_base` passes `config` but fails compile (`ESPAsyncWebServer.h` missing). |
| `time: sntp` | fails: not available on host | n/a | n/a | Use `time: platform: host` (works, uses real wall time). |
| `ota: esphome` | passes | **fails**: `openssl/evp.h: No such file or directory` (via `md5`/`sha256`). The image has `libssl3t64` but no `/usr/include/openssl`. | n/a | Cannot be in the SiL top-level. Host-OTA re-exec exists in core but is unusable in this image. |
| `ota: http_request` | fails: "incompatible with HOST using host framework" | n/a | n/a | Omit. |
| `update: http_request` | fails: "requires component ota" | n/a | n/a | Omit (any `ota:` pulls in the failing md5/openssl path). |
| `http_request:` alone | passes | compiles (httplib) | works | Allowed but pointless. Leave out. |
| `dashboard_import` | fails without `esphome.project` | n/a | n/a | Omit. |
| `mdns:` | passes | compiles | **No-op.** `mdns_host.cpp`: "Host platform doesn't have actual mDNS implementation" | HA cannot auto-discover the sim. Manual add by host:port is the only path. |
| `api:` (plain and Noise) | passes | compiles (downloads `noise-c`, `libsodium`) | works; custom `port:` works | Listens on **0.0.0.0** (see Pitfall 9). |
| `logger:` | passes | compiles | works, ANSI colors on stdout | Strip ANSI when parsing logs. |
| preferences | automatic | compiles | File-backed, see Pitfall 6 | Non-hermetic by default. |
| `safe_mode:`, `status_led:`, `time: host`, `speed` fan, `template` output/sensor | pass | compile | work | Usable. |

Also found while reading hardware-rev-3.1.yaml (included by 3.3): `esphome: on_boot` lambdas call `id(status_led)`, `wifi: on_connect` has `delay: 60s` then dims the LED, and `globals: init_done` uses `restore_value: yes`. None of this can be inherited unchanged on host (see Pitfalls 2 and 6).

---

## Critical Pitfalls

### Pitfall 1: The binary built in the ESPHome image does not run on the host

**What goes wrong:** The 2026.10.1 image is Debian 13 (glibc 2.41, GCC 14-era libstdc++). The dynamically linked `program` requires `GLIBC_2.38`, `GLIBCXX_3.4.31` and `GLIBCXX_3.4.32`. On this server (Debian 12, glibc 2.36) it fails to start: `version 'GLIBC_2.38' not found`. `[VERIFIED]`
**Why it happens:** `esphome compile` on host uses the machine's own gcc through ninja (no PlatformIO, no pinned toolchain, `CC`/`CXX` from PATH). "Build in Docker, run on host" silently couples the run target to the image's libc. Every future image bump can raise the floor again.
**Consequences:** "Works in CI container, dead on developer machine" or the reverse. Agents waste cycles on a loader error that looks unrelated to their change.
**How to avoid:** Decide the run location in phase A and never revisit it ad hoc. Recommended order of preference:
1. **Static link.** A `sil_core` external component calls `cg.add_build_flag("-static")`. Verified: produces a 5.5 MB `statically linked` ELF that runs anywhere on x86-64 Linux. `-l`/`-Wl,` and everything else routes to the link line, so no PlatformIO options are needed (`platformio_options` is ignored on host except `lib_ignore`). The linker warns about `getaddrinfo` in static binaries; harmless because the firmware never resolves names. `[VERIFIED]`
2. Fallback: run the binary inside the same container (`--network host` or published port, `--user`).
3. Do not try `-static-libstdc++` alone; the libc floor (`GLIBC_2.38`) remains.
**Warning signs:** `ldd program` prints `not found`; `file program` says `dynamically linked`.
**Phase to address:** A. Add a build-time assertion `file program | grep -q 'statically linked'` and run it in the contract test (F).

---

### Pitfall 2: Trusting `esphome config`, or reusing the product hardware package

**What goes wrong:** `wifi:` passes `config` and crashes in codegen. `pulse_counter` and `gpio` binary_sensor pass, compile and run, but produce constants (0 RPM, never-pressed). `ledc`, `esp32_rmt_led_strip`, `i2c sda/scl`, `esp32:` fail. A SiL YAML that "validates" can still exercise a dead fan reading 0 RPM. `[VERIFIED]`
**Why it happens:** Host stubs exist so ESPHome's own integration tests compile; they were never meant to simulate behavior. PROJECT.md already notes "swapping only the hardware package will not compile". That is true, and the deeper issue is that some swaps compile and are wrong.
**Consequences:** `stall_guard` or `fallback_mode` fire in the sim because a stub reads 0 RPM. Agents then "fix" product logic that is not broken.
**How to avoid:**
- The contract test must run `esphome compile` and then run the binary and assert real behavior (duty written, RPM read back, button event seen). Never `config` only.
- Provide a lint in `sil` (or the contract test) that rejects, in a SiL top-level, any of: `platform: ledc`, `pulse_counter`, `gpio` binary_sensor, `esp32_rmt_led_strip`, `hdc1080`, `wifi:`, `ota:`, `update:`, `captive_portal`, `improv_serial`, `web_server`. Fail with the replacement named.
- Match **types and ids**, not just names. Product modules reference `pwm_fan1` (FloatOutput), `fanN_speed` (Sensor), `btn_usr1` (BinarySensor), `status_leds` (AddressableLight), `fancontroller_temperature`. Any module that uses a pulse_counter-specific action breaks.
- The Rev 3.1 `wifi: on_connect` status-LED behavior (blue pulse, green after connect, dim after 60 s) cannot run on host. Decide explicitly: re-create it in the example from `api` client-connected triggers, or declare status-LED boot behavior HIL-only. Do not let the sim silently show a different LED story than hardware.
**Warning signs:** Fans read exactly `0.00` forever; buttons never toggle; `[W]` or `[E]` lines about the platform in the first second of the run.
**Phase to address:** A (lint), B (replacement components), F (contract test).

---

### Pitfall 3: OTA, `update`, `http_request`, `mdns`, `captive_portal` assumed to degrade gracefully

**What goes wrong:** `ota: esphome` fails the host compile (no OpenSSL headers in the image). `ota: http_request` is explicitly incompatible with host. `update: http_request` requires an `ota` component, so it cannot be kept either. `mdns` compiles and does nothing. `[VERIFIED]`
**Why it happens:** ESPHome host support targets integration tests, not product parity.
**Consequences:** A product YAML shared "as is" will not build. The mDNS no-op means HA never offers the sim in its "Discovered" list, so first-time users think the API is broken.
**How to avoid:** SiL top-level is its own file (already planned). Document in the README that HA needs "Add device -> host:port (+ encryption key)". Do not attempt to install `libssl-dev` into the image: it breaks the "nothing to install" promise and the pin. Do not rely on host-OTA re-exec.
**Warning signs:** `fatal error: openssl/evp.h` in compile output.
**Phase to address:** A (top-level YAML), D (HA setup doc).

---

### Pitfall 4: Firmware exits after 15 minutes with no API client

**What goes wrong:** `api:` defaults to `reboot_timeout: 15min`. With no client for that long, `APIServer::loop()` logs `No clients; rebooting` and calls `App.reboot()`. On host `arch_restart()` is `exit(0)` (or `execv` if a re-exec is armed). A headless test run, or a dev who closes HA and walks away, loses the firmware. `[SOURCE]`
**Why it happens:** Embedded safety net inherited unchanged. Compounding factor: it counts `millis()`. When the time factor arrives, 15 minutes of sim time is 9 s of wall time at x100.
**Consequences:** Mysterious "firmware disconnected" after exactly 15 minutes. A clean exit code 0 looks like a deliberate stop to a naive supervisor.
**How to avoid:** Set `api: reboot_timeout: 0s` in the SiL top-level and assert it in the lint. The supervisor must treat any firmware exit as an event: log it, and surface an unexpected exit as a failure (distinguish from `sil down`). Related: firmware modules such as `fallback_mode` or safe-mode may legitimately reboot. Define the restart policy (auto-restart and re-sync, see Pitfall 11) up front.
**Warning signs:** `[E][api] No clients; rebooting` in logs; firmware PID changes.
**Phase to address:** A (YAML), B (supervisor behavior).

---

### Pitfall 5: Identity collisions in Home Assistant (fixed MAC, shared names, shared prefs)

**What goes wrong:** `host:` has `mac_address` defaulting to the constant `98:35:69:ab:f6:79`. Every default host build in the world, and every instance on one machine, shares it. `name_add_mac_suffix: true` (used by the product YAML) derives the name from the MAC's last 3 bytes, so name, prefs filename and hostname collide too. Verified: MAC `02:00:00:00:00:01` produced `sil-test-000001`. `[VERIFIED]`
HA keys config entries and entity unique_ids on the **MAC** (entity `unique_id = build_device_unique_id(mac, static_info)`). Adding a second device with an existing MAC through the manual flow does not create a second entry: the flow finishes as `already_configured_updates` and **rewrites the existing entry's host/port/PSK**. `[SOURCE: HA config_flow/entity.py]`
**Why it happens:** Defaults are fine for a single throwaway test; this project explicitly wants repeatable and multiple instances.
**Consequences:** Starting a second sim re-points the first sim's HA device. A restarted sim with a *random* MAC creates a fresh HA device each time (orphaned "unavailable" entities pile up in the user's HA). Using the product's real device name/MAC can collide with a real controller in a production HA.
**How to avoid:**
- Generate a **stable, locally administered MAC** (`02:xx:...`) per instance, derived from the instance name and persisted in the instance dir. Never the default, never random per run.
- Name prefix: `esphome.name: sil-<instance>`, `friendly_name` with "SIL". Never reuse the product `esphome.name` or `project.name`.
- Per-instance API port, per-instance Noise key stored with the instance (stable, so HA does not need re-auth).
- Keep entity names and object ids stable across sim releases. A rename means orphaned old entities in HA (old unique_id not found, entity re-added as new).
- Keep the sim off any HA unless the user points it there (already a PROJECT decision). Document how to delete the sim device cleanly.
**Warning signs:** HA shows "unavailable" duplicates; adding instance 2 flips instance 1 to the wrong port; the same `.prefs` file is opened by two processes.
**Phase to address:** A (instance model, MAC/port/key allocation), D (HA doc and verification).

---

### Pitfall 6: Persistent preferences make runs non-hermetic

**What goes wrong:** Host prefs live in `$ESPHOME_PREFDIR` or `$HOME/.esphome/prefs/<App name>.prefs`. They are flushed only on graceful SIGINT/SIGTERM (SIGKILL loses them). Values are limited to 255 bytes. If neither variable is set, preferences are silently disabled (`unable to save preferences`). `[VERIFIED/SOURCE]`
The product hardware package has `globals: init_done` (`restore_value: yes`) and fans with `RESTORE_DEFAULT_ON`. First boot (no prefs) runs `delay: 5s` then `fan.turn_on speed: 100` for all four fans; later boots skip it.
**Why it happens:** Prefs semantics are invisible until the same scenario gives different results on the second run.
**Consequences:** "Passes on first run, fails on second". Two distinct boot paths exist and only one is tested. Two instances sharing `$HOME` share a prefs file when names collide (Pitfall 5).
**How to avoid:** `sil up` gives every run a fresh `ESPHOME_PREFDIR` (tmpdir) by default, with an explicit `--keep-state` for tests of restore behavior. Stop firmware with SIGTERM and a grace period before SIGKILL so `--keep-state` actually persists. Test both boot paths (first boot and restored boot) in the contract test.
**Warning signs:** Different fan state at t=0 across runs; `Preferences filename not set` in logs.
**Phase to address:** A (run harness), F (contract test covers both boot paths).

---

### Pitfall 7: Addressable-light output implemented loosely (partition lights break)

**What goes wrong:** `light: partition` only works on an `AddressableLight` parent. The contract requires `size()`, `get_view_internal()`, `clear_effect_data()`, a `get_traits()`, and `write_state()` that **must call `mark_shown_()`** (the RMT reference does this first). If `write_state` does not, `should_show_` stays set: effects (`pulse`, `addressable_*`) re-trigger every loop, burning CPU and flooding the sim-bus. `[SOURCE]`
Other traps:
- Product uses `channel_colors: GRB`. The ESPColorView layer already applies channel order; if the sim reads raw bytes it shows swapped colors. Store logical RGB and ignore chipset/channel order.
- The partition `internal: True` parent still receives brightness/gamma-scaled values. A "green at 10% brightness" frame is `(0, ~25, 0)`; the UI must render what the strip would emit, not the nominal color.
- Effects update at loop rate (16 ms). Forwarding every frame yields roughly 60 frames/s per strip to bus and WebSocket.
**Why it happens:** The light stack looks simple until effects and partitions stress it.
**Consequences:** Pegged CPU, wrong colors in the UI, effects that never settle, flaky LED assertions.
**How to avoid:** Mirror `esp32_rmt_led_strip`'s structure but with the transport instead of RMT; accept (and ignore) `pin`, `chipset`, `channel_colors`, `rmt_symbols`, `num_leds` so product-like YAML maps over with minimal edits. Coalesce frames: send on change, rate-limit (e.g. max 20-30 Hz), always send the final frame. First task of phase B: a 5-LED strip with a `pulse` effect on a partition, asserted end to end.
**Warning signs:** Sim-bus message rate stays high with a static LED; `pulse` effect colors wrong; partition segment shows the wrong index.
**Phase to address:** B.

---

### Pitfall 8: Output semantics: what the sim sees vs what the fan model expects

**What goes wrong:** `FloatOutput::set_level()` applies, in order: clamp to [0,1]; **min/max power scaling** (only if `min_power`/`max_power` are configured; zero stays zero only if `zero_means_zero`); **inversion** (`1 - level`); then calls `write_state(float)`. A `sim_output` hooked at `write_state` receives the *pin-level duty*, i.e. what LEDC would emit. `[SOURCE]`
Fan `platform: speed` with `speed_count: 100` converts percent to level = `speed / speed_count`. Fan "off" calls the output with 0.
**Why it happens:** Two places can plausibly own "min duty": the firmware's `min_power` and the fan model's `min duty` / `start threshold` in `fan.toml`.
**Consequences:** Double application (firmware min_power 0.2 plus model min duty 0.2). An inverted output makes the model run at `1 - duty`. Hysteresis is missed: a real fan needs a higher duty to start than to keep running; a linear map with a single threshold makes control loops look better than hardware.
**How to avoid:** Rule: `sim_output` forwards the raw pin duty; the model owns all physical thresholds; firmware-side scaling stays in the firmware YAML. Document this in the `sim_*` consumer interface. Model needs separate `start_duty` and `stall_duty` with hysteresis from the first model version. Contract test: set `fan.turn_on speed:50`, assert pin duty 0.5; add `inverted: true` and `min_power` in a second case and assert.
**Warning signs:** Duty seen in the UI differs from `speed/100`; RPM "starts" at 1% duty.
**Phase to address:** B (component), C (model).

---

### Pitfall 9: Native API exposed on all interfaces, plaintext, default port

**What goes wrong:** The host API socket binds `0.0.0.0` (verified `LISTEN 0.0.0.0:16053`). There is no bind-address option. Without `encryption:` anyone on the LAN can control the fans. If a second instance starts on a busy port, the log shows `Socket bind: errno 98`, `api was marked as failed`, and **the process keeps running**. `[VERIFIED]`
**Why it happens:** Embedded defaults. The Noise-enabled client also fails to connect without the key (`RequiresEncryptionAPIError`), which is a good sign but not a bind restriction.
**Consequences:** Orphaned firmware from a crashed `sil` holds port 6053; the next `sil up` believes it started, but talks to the stale process, or has a "running" firmware with a dead API.
**How to avoid:**
- Always set `api: encryption:` (verified working); allocate a per-instance port; select ports dynamically and verify before start.
- Health check = an actual API handshake (aioesphomeapi or a minimal Go Noise/plaintext hello), not "process is alive". Also treat the `api was marked as failed` log line as fatal.
- Supervise with `Pdeathsig` (Go `SysProcAttr.Pdeathsig = SIGKILL`) and a process group so a crashed `sil` does not leave firmware behind. Consider a firewall note if the host is exposed.
**Warning signs:** `errno 98`; `ss -ltnp` shows an old firmware PID on the port; HA connects to stale state.
**Phase to address:** A (supervisor), D (HA connection check).

---

### Pitfall 10: Blocking or naive socket I/O in the firmware-side component

**What goes wrong:** ESPHome's host main loop is single-threaded, driven by `select()` with a 16 ms `loop_interval` and a loopback wake socket. A `connect()`/`recv()` that blocks stalls everything including the API. Using plain non-blocking sockets that are **not registered** with the loop means data is only noticed on the next 16 ms tick, quantizing each sim-bus round trip to 62 Hz. Reconnect attempts every `loop()` spin at tick rate. `[SOURCE: wake_host.cpp, application.h]`
**Why it happens:** The loop looks like Arduino's `loop()`, so authors poll.
**Consequences:** Jittery control loops that only show up at higher sim rates; a stalled firmware when the simulator is not yet up; log floods.
**How to avoid:**
- Non-blocking sockets always (`O_NONBLOCK`, `connect()` returning `EINPROGRESS`, `MSG_DONTWAIT`).
- Register the fd with `wake_register_fd(fd)` (declared in `esphome/core/wake.h`) so `select()` wakes on data; that is also how the stock `socket::socket_*_loop_monitored` helpers work. Alternatively a reader thread plus `App.wake_loop_threadsafe()`. Note `FD_SETSIZE` bound.
- Reconnect with exponential backoff and jitter (cap about 2 s), driven from the scheduler or a timestamp. **Use wall time (`clock_gettime`) for transport timers, not `millis()`**: once the time factor patch lands, `millis()` is scaled and backoff timers would collapse or explode.
- Never write to the bus from `write_state()` synchronously with blocking semantics: queue, coalesce, drain in `loop()`.
**Warning signs:** `[W]` component took too long; CPU at 100% with the simulator absent; API pings fail while the sim is down.
**Phase to address:** B.

---

### Pitfall 11: Sim-bus framing, lost updates and startup order

**What goes wrong:** Typical failures in this class of bench `[REASONED]`:
1. **Framing:** TCP stream treated as messages; partial reads and `EAGAIN` on write. No max frame size, so a corrupt length prefix allocates gigabytes.
2. **Lost state on reconnect:** The firmware writes outputs once during `setup()` (verified: `o1=0.000000` logged at setup) and then only on change. A simulator that connects later never learns the current duty. The converse: a restarted firmware forgets button/sensor state the sim thought it had set.
3. **Edge-based events:** "button pressed" sent as an edge is lost if the other side is down; the long-press timing then depends on delivery.
4. **Sensors reading 0 before the first sample:** `sim_sensor` that publishes 0 until the sim connects looks like a stalled fan to `stall_guard`.
5. **Startup order assumed:** "sim first, then firmware" works in the happy path; either order, either side restarting, must work.
6. **Stale firmware binary** talks protocol v2 to a v3 simulator.
7. **Time mapping:** firmware `millis()` restarts at 0 with every process; sim time continues. A bare timestamp from the firmware is meaningless across a restart.
**How to avoid:**
- Length-prefixed frames with a hard maximum, explicit protocol version and `build id` in a `hello`; refuse on mismatch with a clear message.
- **Full-state snapshot on every (re)connect, both directions**, then deltas. Make state level-based (button is a level with a sequence number) rather than edge-based.
- `sim_output` caches its last value and replays on connect. `sim_sensor` publishes NaN (or no state) until valid, and a separate "sim connected" binary_sensor lets the example firmware gate stall logic if desired.
- Per-session `epoch`; the hello carries the firmware's `millis()` and the sim's clock so offsets are computed per session.
- Writes: bounded queue, state messages latest-wins per key, never block; drop-with-counter and surface the counter in status.
- Test matrix in the contract test: sim-first, firmware-first, sim restart mid-run, firmware restart mid-run.
**Warning signs:** UI shows stale duty after a sim restart; fan model idle until the first duty change; `stall_guard` triggers at boot.
**Phase to address:** B (protocol, components), F (restart matrix test).

---

### Pitfall 12: Wall-clock coupling in the simulator that blocks the later time factor

**What goes wrong:** Time sources spread through the Go code: `time.Now()`, `time.Sleep`, `time.Ticker`, `context.WithTimeout`, rate limits in the UI, model `dt` assumed from tick period. Adding a virtual clock later then means hunting down every call site. `[REASONED]` plus PROJECT.md already names this risk.
**Why it happens:** `time.Now()` is the path of least resistance, especially for agent-written code.
**Consequences:** The "injectable clock from day one" requirement becomes decoration. Go's `Ticker` also drops ticks under load, so a `dt = tickPeriod` model silently runs slow.
**How to avoid:**
- One `Clock` interface (`Now()`, `After()`, `NewTicker()`); the model, scenario engine, bus timestamps and status all use it. Enforce by lint: ban `time.Now`, `time.Sleep`, `time.After`, `time.NewTicker` outside the single `clock` package (a `forbidigo` golangci rule or a grep test).
- `dt` is always computed from the clock (`now - last`), never assumed. Clamp `dt` (laptop suspend, debugger pause) and substep.
- Keep wall-time concerns (transport backoff, WebSocket write deadlines, UI refresh) in an explicitly named `walltime` helper, separate from sim time.
- Know where the firmware side stands: only `hal.cpp` (`millis`, `millis_64`, `micros`, `delay`, `delayMicroseconds`, `arch_get_cpu_cycle_count`) and the **`select()` timeout in `core/wake/wake_host.cpp::wakeable_delay`** (not `core/application.cpp`, which is where PROJECT.md and the idea doc place it; in 2026.10.1 `application.cpp` only calls into `wake`). `core/time.cpp` and `components/time` (`time: host`, `ESPTime`) use real wall time directly and would be unscaled, so any `on_time` cron in firmware does not follow the time factor. `[SOURCE]`
**Warning signs:** `grep time.Now` outside the clock package; model tests that use `time.Sleep`.
**Phase to address:** C (clock, lint). **Later** (time factor): re-verify patch points against the pinned source with a failing check when the ESPHome version differs.

---

### Pitfall 13: Time factor and HA/API incompatibility (why HA forces x1)

**What goes wrong:** `[SOURCE]` API keepalive is `KEEPALIVE_TIMEOUT_MS = 60000` measured by `millis()`; a client that does not ping within 2.5x is disconnected. Real clients (HA, aioesphomeapi) ping in wall seconds. At x100, the firmware thinks 60 s passed after 0.6 s wall and drops the client. The 15 min `reboot_timeout` also becomes 9 s. `millis()` is uint32 and wraps at 49.7 days sim time: at x100 that is roughly 11.9 h wall. `arch_get_cpu_cycle_count()` on host is seconds*1e9 and wraps every 4.29 s by construction.
**Consequences:** "Time factor works, HA mysteriously disconnects"; long thermal runs break after about 12 h wall time.
**How to avoid:** Keep the PROJECT.md position: x1 with HA, sped-up runs are headless. Make the supervisor refuse (or warn on) `factor != 1` when an external API client is configured. Run headless at any factor with `reboot_timeout: 0s`. Do not rely on `millis()` outside ESPHome's own wrap-safe `millis_64` paths. Mark this as a Later-phase pitfall, but record it now so the interface (timestamps in sim time) does not assume otherwise.
**Phase to address:** Later; interface decisions in C.

---

## Moderate Pitfalls

### Pitfall 14: Model integration stability and PWM semantics

**What goes wrong:** Explicit-Euler first-order lag `y += dt/tau * (target - y)` goes unstable or oscillates when `dt >= 2*tau` (e.g. after a long pause, or a very small `tau` in a fan with low inertia).
**Prevention:** Use the exact discretization `y += (target - y) * (1 - exp(-dt/tau))` (unconditionally stable) and clamp/substep `dt`. Keep the model pure and table-testable: given duty trace and `dt` trace, expect RPM trace; no goroutines in the model. Include start/stall hysteresis (Pitfall 8) and a `max_rpm` clamp. Decide `duty` semantics once: model input is pin duty in [0,1]; speed fan percent is a firmware concern.
**Phase:** C.

### Pitfall 15: Tach semantics: RPM value versus pulses per minute

**What goes wrong:** The product pulse_counter reports **pulses/min** and the YAML applies `filters: multiply: 0.5` (2 pulses per revolution). If `sim_sensor` emits already-converted RPM *and* the filter is retained, readings are halved; if the filter is dropped from the sim package, the wrong-ppr fault planned for P2 cannot be expressed. `[VERIFIED from hardware-rev-3.1.yaml, pulse_counter log unit "pulses/min"]`
Second: the real sensor reports the **average over `update_interval: 1s`**, which adds up to about 0.5 s of effective lag and a first sample only after 1 s. A PI loop tuned against an instantaneous sim RPM can oscillate on hardware.
**Prevention:** Position `sim_sensor` where pulse_counter sits in the chain: emit pulses/min (`rpm * ppr`), and keep the product `multiply: 0.5` filter in the hardware-sim YAML. That is still "no pulse simulation". Make `sim_sensor` a `PollingComponent` with `update_interval` that reports the windowed mean of the incoming RPM, as the hardware does. State both choices in the consumer interface.
**Phase:** B (component semantics), C (model).

### Pitfall 16: Build directory and Docker ergonomics

**What goes wrong:** `[VERIFIED]`
- Running the image without `--user` creates a root-owned `.esphome/` and `.gitignore` in the mounted config dir; running with `--user` also needs `-e HOME=/tmp` or ESPHome warns about the cache dir.
- Build output lives in `<config>/.esphome/` (about 108 MB per config for a tiny firmware). Parallel agents in separate worktrees each rebuild and download their own copy; two builds in one directory corrupt each other.
- External components resolved by `source: type: local, path: ...` must be inside the Docker mount. Symlinks pointing outside the mount silently break.
- The build path is embedded in `/config/.esphome/...`: always mount at the same container path or incremental builds degrade.
- Libraries (`ArduinoJson`, `noise-c`, `libsodium`) are **downloaded from the network on first build**; a cold CI runner with no network, or a registry outage, fails the build. This conflicts with the locality constraint.
- Measured timings: cold about 12 s, no-op 1.4 s, a lambda edit about 2 s, changing the component set or core config about 10 s (`cleaning build files`). Fast enough that the speed pitfall is *not* the build, it is accidental cache invalidation (e.g. a config flag toggled per run).
**Prevention:** One wrapper (`sil build`) that always passes `--user $(id -u):$(id -g) -e HOME=/tmp`, mounts the whole repo at a fixed path, uses a per-worktree build dir, never builds the same dir concurrently (file lock), and caches `.esphome/pio_components` (and checks it into a CI cache). Keep per-run variability out of the compiled YAML: instance MAC, name, port and key are better passed as substitutions that are stable per instance than regenerated each run. Pin the image by digest in addition to the tag.
**Phase:** A.

### Pitfall 17: Codegen and component-schema mistakes in the `sim_*` components

**What goes wrong:** `[REASONED]`, with the verified base that ESPHome reports a failure for unmet `requires component X`:
- Missing `DEPENDENCIES`/`AUTO_LOAD`: a platform component using the transport without `sim_core` in `DEPENDENCIES` yields `undeclared id` or linker errors that read as C++ problems.
- Not restricting to host: add `cv.only_on(PLATFORM_HOST)` (as stock `i2c` does) so the same YAML cannot be pointed at an ESP32 build accidentally.
- Setup order: the hub must be set up before dependents (`setup_priority`, `get_setup_priority()`); stock fans restore state in `setup()`, and the first `write_state` happens then (verified in the log), so the transport must exist and queue before connecting.
- Unknown config keys: product-like YAML contains `frequency`, `pin`, `rmt_symbols`, `chipset`, `channel_colors`, `num_leds`. Rejecting them forces diverging YAML; accepting and ignoring them keeps `hardware-sim-*.yaml` a near-copy of the hardware package.
- `PollingComponent` versus push components: pick deliberately (Pitfall 15).
- Every source file must compile for the host target only; no `#include <driver/...>`.
**Prevention:** Develop the components against a tiny golden YAML set in the repo and compile them in CI (Pitfall 2). Add one negative test (missing hub) expecting a specific, readable error.
**Phase:** B.

### Pitfall 18: Home Assistant side effects

**What goes wrong:** `[SOURCE]`
- Entity key hashing and unique_id come from MAC and static info; renaming entities or changing object ids orphans them.
- Product `internal: True` partitions are not exposed. The sim will not show them in HA, and the UI must not claim otherwise.
- Without `Noise` key stability, HA's reauth is triggered every run.
- HA on another machine cannot reach `127.0.0.1`; the sim host needs a reachable interface (which, with 0.0.0.0, is also the exposure from Pitfall 9).
- Zeroconf discovery is absent (Pitfall 3), so the first HA flow must be manual.
**Prevention:** Stable names/ids across releases; stable key; README with the exact HA steps and how to delete the device; never auto-register anything in a user's HA.
**Phase:** D.

### Pitfall 19: Web UI: backpressure, memory and drift

**What goes wrong:** `[REASONED]`
- **WebSocket backpressure:** one slow tab (or a background tab with throttled timers) blocks a broadcast loop writing synchronously, and then the model lock if the broadcast happens under it. Memory grows in the server's per-client buffers.
- **Plot memory:** an ever-growing array behind uPlot (several series at 10-60 Hz) grows without bound; background tabs batch frames and then dump them.
- **UI-only logic:** derived values (firmware connected, "behind real time", run state) computed in JS, or UI actions (e.g. long-press timing, fan selection) implemented in JS, so the CLI/JSON API cannot reproduce them. PROJECT.md requires the opposite.
- **Dev loop:** `go:embed` serves stale assets during UI edits.
- **Local security:** a localhost HTTP/WS API with state-changing endpoints is reachable from any web page the developer opens (CSRF, cross-origin WebSocket, DNS rebinding).
**Prevention:**
- Per-client bounded send queue; slow client gets coalesced latest-state frames or is dropped; write deadlines; ping/pong to reap dead tabs; fixed UI tick (10-20 Hz) with server-side decimation; never hold the model lock while writing.
- Fixed-size ring buffers client-side (e.g. 5 min window); server keeps a ring for history so reloads work (`/api/v1/history`).
- Single JSON schema for status/events used by CLI, WS and tests; CI check that every UI `fetch`/WS action maps to an API route documented and exercised by the CLI; all derived values computed server-side.
- Serve from disk with a `-dev` flag; embed in release.
- Bind `127.0.0.1` by default; check `Origin` on WebSocket upgrade and on mutating requests; consider a per-run token.
- Vendored uPlot with pinned version and license file; CSP `default-src 'self'`; a test that loads the page with the network blocked.
**Phase:** E.

### Pitfall 20: Process supervision and log handling

**What goes wrong:** `[VERIFIED/REASONED]` Firmware logs include ANSI escapes and are DEBUG by default (`HostGPIOPin::digital_write` and `pulse_counter` log constantly); the child can outlive `sil`; exit code 0 on reboot is ambiguous; `SIGTERM` is the only graceful stop and the only path that flushes prefs. SIGPIPE is ignored in the firmware (`std::signal(SIGPIPE, SIG_IGN)`).
**Prevention:** Strip ANSI; set `logger: level:` for SiL explicitly; use a process group and `Pdeathsig`; classify exits (`stop requested` / `rebooted` / `crashed`); SIGTERM with timeout then SIGKILL; ring-buffer the logs in the simulator and expose them over the API.
**Phase:** A.

---

## Minor Pitfalls

### Pitfall 21: Flaky scenarios from fixed sleeps
`[REASONED]` At x1 with a 16 ms loop and Go scheduling jitter, `sleep(5s)` assertions are racy. Provide `wait_until(condition, timeout)` in the CLI/pytest layer from the start, with sim-time timeouts. **Phase:** C/F.

### Pitfall 22: Optimistic sim creates false confidence
`[REASONED]` Ideal sensors, zero latency, no noise, perfect PWM: a pass means "logic is plausible", not "safe on hardware". Keep the README honest (driver, Wi-Fi, ISR, heap, PCNT stay HIL's job as in PROJECT.md) and record it in the consumer interface. **Phase:** F (docs).

### Pitfall 23: Unchecked ESPHome version
The time patch and the stub behaviors are specific to 2026.10.1 (the local `esphome` checkout is 2026.11.0-dev and already differs, e.g. an RMT warning change). A bump can change host stubs without any warning. Enforce a version check in `sil` (already in the consumer interface) and re-run this matrix on a bump. **Phase:** A, Later.

### Pitfall 24: Go build specifics
`[REASONED]` Build with `CGO_ENABLED=0` to keep the "single static binary" promise; run `go test -race` on the bus/WS code; avoid Docker as a runtime dependency of the simulator itself (only the firmware build needs it). **Phase:** A/C.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Dynamic-link the host binary and "just run it in the container" | No linker flag work | Run location leaks into every command; breaks on image bumps | Only as a documented fallback |
| Default `host:` MAC and a fixed API port | Works first time | Collisions in HA and between instances (Pitfall 5, 9) | Never past the first smoke test |
| Polling the sim-bus in `loop()` without `wake_register_fd` | Simple | 16 ms round-trip quantization, hard to find later | Spike only |
| `time.Now()` in the model | Fast to write | Time factor requires a rewrite | Never (lint-banned) |
| UI computes derived state in JS | Faster page | Breaks "UI is a client of the CLI/API" | Never |
| Sim emits RPM, drops the product `multiply: 0.5` filter | Looks simpler | Wrong-ppr fault cannot be expressed; semantics drift from hardware | M1 only if documented as temporary |
| Reusing prefs dir across runs | Realistic restore | Non-hermetic tests | Only behind `--keep-state` |
| `esphome config` as the "does it build" check | Fast | Misses codegen crash and silent stubs | Never as sole gate |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| ESPHome Docker image | Run as root; lose `HOME`; mount subdir | `--user`, `-e HOME=/tmp`, mount repo root at a fixed path |
| ESPHome `host:` | Assume stock components simulate | They are stubs; swap to `sim_*` (matrix above) |
| ESPHome `api:` | Keep defaults | `reboot_timeout: 0s`, encryption, per-instance port |
| ESPHome loop | Block or poll | Non-blocking, `wake_register_fd`, wall-clock backoff |
| HA native API | Treat as auto-discovered | Manual add by host:port + key; stable MAC and key |
| aioesphomeapi (tests) | Fixed sleeps | Subscribe to state and wait on conditions |
| Go WebSocket | Broadcast inline under model lock | Per-client queue, deadlines, coalescing |
| uPlot | Append forever | Fixed ring window |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| LED effect frame spam | High bus rate with static LEDs | `mark_shown_()`, coalesce, rate-limit | Immediately with `pulse` effect |
| Per-write logging of GPIO | Log floods | Set logger level | Any output toggled at rate |
| Unbounded plot arrays | Browser memory growth | Ring buffers | Minutes at 10+ Hz x several series |
| Per-client unbounded WS queues | Server RSS grows | Bounded queues, drop slow clients | One idle background tab |
| Sim integrating at wall tick | Runs slow under load | `dt` from clock, substep | Loaded CI host |
| Build dir duplication per agent | Disk growth (about 100 MB each) | Shared library cache, per-worktree build | Many parallel agents |
| Full clean when component set flips | About 10 s rebuild each time | Keep compiled YAML stable per instance | Frequently toggled configs |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| API on 0.0.0.0 plaintext | LAN control of the sim and the HA entities it feeds | Always Noise, per-instance key |
| Web UI/API on 0.0.0.0, no Origin check | CSRF/WebSocket hijack from visited pages | Bind 127.0.0.1, Origin check, token |
| Stale firmware holds the port | Tests run against old binary | `Pdeathsig`, handshake health check, hello build id |
| Sim pointed at production HA | Orphaned entities, MAC collisions | Distinct names, stable MAC, doc, user opt-in only |
| Future CI runner (noted in idea doc) | Secrets/LAN reach | Isolated runner as already planned; no secrets in SiL |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| "Firmware connected" shown from process liveness | UI says OK with dead API/bus | Show bus and API handshake status separately |
| Sensor shows 0 before first sample | Looks like a stalled fan | Show "no data" |
| LED color shown at nominal | Differs from actual strip | Render brightness/gamma-applied values |
| Errors surface as C++ compile noise | Agent/dev cannot tell the cause | Pre-lint YAML with named replacements |
| Silent restart on firmware exit | Hidden resets | Show restart count and reason |

## "Looks Done But Isn't" Checklist

- [ ] **Host build:** `file program` says `statically linked`; binary runs on a clean Debian 12 container, not only in the build image.
- [ ] **Contract test:** uses `esphome compile` plus a run, not `config`; asserts duty written, RPM read back, button event, LED frame.
- [ ] **Partition LEDs:** a partition with `pulse` effect produces bounded frame rate and correct colors.
- [ ] **Restart matrix:** sim-first, firmware-first, sim restart, firmware restart all re-sync full state.
- [ ] **Prefs:** both first-boot and restored-boot paths tested; default run is hermetic.
- [ ] **API:** `reboot_timeout: 0s`, encryption on, port unique, handshake-based health check.
- [ ] **HA:** stable MAC/name/key; adding a second instance does not rewrite the first; delete procedure documented.
- [ ] **Clock:** no `time.Now`/`Sleep`/`Ticker` outside the clock package (lint passes).
- [ ] **Tach:** pulses/min versus RPM decision documented; windowed average implemented.
- [ ] **Web UI:** every UI action has a CLI equivalent with the same JSON; page works with network blocked; memory flat over an hour.
- [ ] **WebSocket:** a stalled client does not slow others or grow memory.
- [ ] **ESPHome pin:** version check blocks any other version; build image pinned by digest.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Dynamic binary choice made late | LOW | Add `-static` build flag in `sim_core`; rebuild |
| Wall-clock calls spread through Go code | HIGH | Introduce `Clock`, replace call sites, add lint; the sooner the cheaper |
| Wrong tach semantics shipped | MEDIUM | Move conversion to the YAML filter; version the consumer interface |
| HA entity ids renamed | MEDIUM | Remove device in HA, re-add; document migration |
| Protocol without version/snapshot | MEDIUM | Add `hello` with version and full-state on connect; bump major |
| UI logic drift | MEDIUM | Move logic server-side, add API parity test |
| Orphaned firmware processes | LOW | Kill by port owner; add `Pdeathsig` and group kill |
| Prefs pollution | LOW | Wipe prefdir; default to per-run tmp |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 Dynamic binary / glibc | A | `file program`; run in a clean Debian 12 container |
| 2 `config` vs real behavior, product package reuse | A, B, F | Lint rejects banned components; compile+run contract test |
| 3 OTA/update/mdns/captive assumptions | A, D | Top-level YAML compiles; README documents manual HA add |
| 4 API `reboot_timeout` exit | A, B | Run 20 min sim-time headless without exit; lint checks key |
| 5 HA identity collisions | A, D | Two instances, one HA: both stay distinct; restart keeps entities |
| 6 Prefs persistence | A, F | Same scenario twice gives identical results; `--keep-state` test |
| 7 Addressable light contract | B | Partition + `pulse` test, bounded frame rate |
| 8 Output semantics | B, C | min_power/inverted cases assert pin duty |
| 9 API exposure / stale process | A, D | Handshake health check; kill `sil` leaves no child |
| 10 Blocking I/O in loop | B | Sim absent: firmware API stays responsive, CPU low |
| 11 Framing / resync / order | B, F | Restart matrix; oversize-frame fuzz test |
| 12 Wall-clock coupling | C | Lint passes; model tests run with a fake clock |
| 13 Time factor vs API | Later (note in C) | Supervisor refuses factor != 1 with API client |
| 14 Model stability | C | Large-`dt` test stays bounded |
| 15 Tach semantics | B, C | Windowed-mean test; ppr configurable |
| 16 Docker/build ergonomics | A | Wrapper tests: ownership, concurrent build lock, cached libs |
| 17 Component schema mistakes | B | Golden YAML compile set; negative test |
| 18 HA side effects | D | HA walkthrough doc executed once |
| 19 Web UI backpressure/memory/drift | E | Slow-client test; 1 h soak; API parity test |
| 20 Process/log handling | A | Exit classification test |
| 21-24 Minor | C/F/A | As listed |

## Sources

- Reproduced against `ghcr.io/esphome/esphome:2026.10.1` (Debian 13, glibc 2.41): `esphome config` and `esphome compile` on host for each component in the matrix; ELF/`ldd`/`objdump` inspection; run of the binary with `aioesphomeapi` (Noise handshake, device_info, entity list); bind-conflict run; ownership test with and without `--user`; timing runs (cold, no-op, lambda edit, component-set change); static link via `cg.add_build_flag("-static")`.
- ESPHome 2026.10.1 source (exact tree from the HIL venv at `/home/claude/wifi-fancontroller/.worktrees/hil/hil/.venv/lib/python3.12/site-packages/esphome/`): `components/host/{__init__.py,hal.cpp,gpio.cpp,core.cpp,preferences.cpp,helpers.cpp}`, `host/toolchain.py`, `build_gen/host.py`, `core/wake/wake_host.cpp`, `core/application.h`, `components/api/{__init__.py,api_server.cpp,api_connection.cpp}`, `components/output/float_output.cpp`, `components/pulse_counter/pulse_counter_sensor.cpp`, `components/mdns/mdns_host.cpp`, `components/esp32_rmt_led_strip/led_strip.{h,cpp}`, `components/light/addressable_light.h`.
- Home Assistant `dev` branch: `homeassistant/components/esphome/entity.py` (unique_id from MAC plus static info, orphaning on rename) and `config_flow.py` (MAC-keyed config entries, `already_configured_updates`).
- Project inputs: `/home/claude/esphome-sil/.planning/PROJECT.md`, `/home/claude/esphome-sil/docs/initial-idea.md`, `/home/claude/wifi-fancontroller/hardware-rev-3.1.yaml`, `/home/claude/wifi-fancontroller/fancontroller-rev3.3-esp32s2.yaml`.
- Not verified (explicit gaps): behavior of `light: partition` on a host addressable parent (no `sim_*` light exists yet); live HA behavior (HA not run, source read only); web UI and sim-bus findings are domain judgment pending prototypes. The GSD research-plan/classify-confidence seam was not used because every claim here was checked directly against primary sources or reproduced.

---
*Pitfalls research for: SiL test bench for ESPHome firmware (host platform, Go simulator, HA native API)*
*Researched: 2026-11-08*

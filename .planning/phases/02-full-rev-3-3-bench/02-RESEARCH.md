# Phase 2: Full Rev 3.3 Bench - Research

**Researched:** 2026-11-09
**Domain:** ESPHome `host:` external components (binary_sensor, addressable light, sensor), sim-bus protocol extension, Go simulator (engine, event stream, control API, CLI), aioesphomeapi/pytest contract tests, Home Assistant identity
**Confidence:** HIGH for everything that was built and run on this host (components, example YAML, LED frame behaviour, button path, API entity set, uv/pytest lock); MEDIUM for Go-side designs (patterns follow Phase 1 code that was read, nothing new was run); LOW only where tagged `[ASSUMED]`.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Carried forward from Phase 1 (not re-discussed)
- Run lifecycle, supervisor/engine split, auto-restart, `sil fw restart` / `sil sim restart` (01-CONTEXT D-01..D-05).
- XDG identity/run/cache dirs, sticky API port from 6054, derived `02:` MAC, stored 0600 API key, `sil-<name>` / `SiL <name>` naming (D-06..D-09). These are what make HA-01 "same device on restart, separate device per instance" hold; Phase 2 verifies them, it does not redesign them.
- Strict `fan.toml`, Arctic P12 PWM PST as shipped fan, hysteresis/asymmetric-lag model (D-10..D-14).
- Build skip-by-hash, banned-component lint after `esphome config` (D-15..D-18). The lint list still applies to the Rev 3.3 example (no `wifi`, `ledc`, `hdc1080`/`i2c`, `esp32_rmt_led_strip`, nothing auto-loading `json`).
- stdout = JSON result, stderr = progress; exit codes 0 / 1 command failed / 2 infrastructure (CLI-05).

#### Button press model
- **D-01:** `sil press usr1` holds the button **200 ms** of sim time; `sil press usr1 --long` holds **1500 ms** (well past the product's 1000 ms USR2 long-press threshold, and clear of its 50 ms button poll). `--hold <dur>` overrides either preset.
- **D-02:** `sil press` **blocks until the release edge** has been sent, then returns its JSON result, so a script can assert right after it.
- **D-03:** Raw control: `sil press usr1 --down` / `--up` set the level and return immediately, for holds of any length and chords across other commands.
- **D-04:** A press (preset, `--hold` or `--down`) on a button that is already held is **rejected with exit 1 and a JSON error** naming the button; no queueing. `--up` on a released button is likewise an error.
- Buttons are sent as levels over the bus (as research decided); the firmware measures hold time itself.

#### LED state in status/watch
- **D-05:** `sil status` reports LEDs as **raw strip + named segments**:
  `"leds": {"strip": ["#rrggbb", ... 5 entries], "segments": {"board":[0], "fan1":[1], ...}}`. Strip colors are the **final pixel colors** as written to the strip (brightness, color correction and effects already applied by ESPHome), so assertions compare what a person would see.
- **D-06:** The index → segment-name mapping comes from the **firmware's HELLO manifest** (the YAML is the single source of truth). The researcher/planner picks the mechanism: derive from the `partition` lights at codegen, or a `segments:` key on the `sim_bus` light. It must not be a separate simulator-side config.
- **D-07:** LED frames go over the bus **only on change, capped at 20 Hz**. Idle firmware sends nothing. Status always shows the latest frame.
- **D-08:** `sil watch` emits **every LED frame received from the bus** (≤ 20 Hz); no extra throttling. Consumers filter.

#### set / watch vocabulary
- **D-09:** `sil set` takes **`key=value` pairs**: `sil set temperature=24.5 humidity=40 port2.fan=arctic-p12-pwm-pst`. Several keys in one call are validated first and applied together (all or nothing). Unknown keys, out-of-range values and unknown fan names fail with exit 1 and a JSON error. The result echoes what was applied. Keys follow the IO-06 channel naming style; the same keys are used by the HTTP API (CLI-06).
- **D-10:** Swapping the fan on a port (`portN.fan=<library name>`) is an **instant unplug/replug**: RPM drops to 0 and the new fan spins up from rest at the current duty, using its own `fan.toml`. Real plug/unplug as a fault (timed, partial) stays in P2.
- **D-11:** Temperature and humidity are **constant values** set by the user. They are sent on change and re-sent about every 10 s (mirrors the HDC1080's `update_interval: 10s`). No noise, drift or ramps in M1. Defaults: **22 °C / 45 %RH**.
- **D-12:** `sil watch` streams **typed change events as NDJSON**. The first line is a full `snapshot` (same shape as `sil status`). Then one line per change, each with sim time `t` (µs) and kind `k`: e.g. `{"t":..,"k":"duty","port":2,"v":0.4}`, `{"k":"rpm",...}` (at the firmware's 1 s publish), `{"k":"button","id":"usr1","v":true}`, `{"k":"leds","strip":[...]}`, plus env and firmware-connected/disconnected events. The planner fixes the exact event kinds; they are internal while 0.x.

#### Example firmware logic
- **D-13:** The Rev 3.3 example carries **thin, product-like logic** (written in the example YAML, not the real product modules):
  - Board LED (`partition` segment 0): blue `pulse` on boot. Green when an API client connects (replaces `wifi.on_connect`; uses `api` client-connected hooks / `on_boot`).
  - Per-fan LEDs (segments 1-4) show each fan's running state (e.g. green when its RPM > 0, off when stopped). The planner picks the exact colors and threshold.
  - USR1 short press = all fans +10 %, USR3 short press = all fans −10 %, USR2 long press = all fans to 100 %.
  - This is the observable surface the contract test asserts on (button → duty change → RPM change → LED change).
- Entity names follow the HIL contract (`Fan N`, `Fan N Speed`, `Temperature`, `Humidity`, `Button USRn`); status LEDs and partitions stay `internal: true` (FW-02).
- Components are consumed via `external_components:` with a local path now (FW-03), ready to switch to `github://zeroflow/esphome-sil@vX.Y`.

#### Contract & smoke tests
- **D-14:** TEST-01 (contract) and TEST-02 (aioesphomeapi smoke) are **pytest under `tests/`**, run by **`make test-contract`**. They drive `sil` through its CLI/JSON (subprocess), and TEST-02 uses aioesphomeapi directly against the firmware API with the key from `sil status`. Python is fine here (tests, not simulator). Mirror the product repo's HIL tooling where sensible (uv-managed env, as in `wifi-fancontroller/hil/pyproject.toml`).
- **D-15:** The live Home Assistant check (success criterion 4: add by host:port + key, control fans, see RPM/temp/humidity/buttons, restart → same device, second instance → separate device) is a **manual UAT checklist** at end-of-phase verification. The automated tests cover the parts aioesphomeapi can see: entity set and names, fan command round trip, and that MAC/name/key are stable across `sil down` / `sil up`.
- **D-16:** Budget: **< 60 s warm** (build cached per Phase 1 D-15), headless. Tests use unique instance names so they run in parallel with a developer's own instance and clean up with `sil down`.

### Claude's Discretion
- `sil logs` format and options (follow, firmware vs simulator, tail).
- `sil watch` filters and exit behaviour (e.g. `--kind`, `--until`, exit when the instance stops).
- Whether `sil set` values (env, fan per port) survive `sil sim restart` / `sil fw restart`. The leaning is that they live in supervisor-held run state and are replayed on resync, but the planner decides.
- How a fan is chosen at `sil up` time (flag or default all ports to the shipped fan) and how the fan library is located/listed.
- Example file name/location and how the Phase 1 example relates to it (replace or keep both).
- Exact JSON shapes beyond the examples above.

### Deferred Ideas (OUT OF SCOPE)
- Temperature ramps (`--ramp 60s`) and sensor noise: later, with scenarios (SCEN-01) or the thermal model (P5).
- Throwaway Home Assistant container for automated HA tests: not in M1; manual UAT instead.
- Pulling in real product modules (`usr_buttons`, `rpm_status_leds`): REAL-01 (v2).
- A second shipped fan with `below_min = "hold"` (e.g. Noctua NF-A12x25), carried from Phase 1: fits here if the planner needs it to test MODEL-03 swaps; otherwise later.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| IO-03 | `sim_bus` binary_sensor platform delivers buttons USR1-3 | Built and run: `SimBinarySensor` over `in` channel, level 0/1. The first published state is swallowed by ESPHome callbacks, so the simulator MUST send level 0 in its `in` snapshot (Pitfall 1). Press/release/long-press chain verified at runtime. |
| IO-04 | `sim_bus` addressable light platform carries the LED strip so stock `partition` works; frames only on change and rate-capped | Built and run: `SimLight` with content dedupe + 50 ms minimum gap, `pulse` effect and `partition` verified; 0 frames in 9 s idle, ≤ 16 frames/s while a transition runs. New `rgb` chan type + `px` hex string on the wire. |
| IO-05 | `sim_bus` sensor platform delivers temperature and humidity | Reuse `SimSensor` with a new `mode: latest` (publish every received value). The default 1 s window mean blends old and new values (22 → 25.5 published 22.64) and is wrong for env values. Verified. |
| FW-01 | Example top-level host config mirrors Rev 3.3 IO surface, no `wifi:` | `examples/rev33.yaml` verified: compiles, passes the lint, runs, 13 API entities. Full file in Code Examples. |
| FW-02 | HIL contract entity names, status LEDs internal | Verified with aioesphomeapi: exactly Button USR1-3, Fan 1-4, Fan 1-4 Speed, Temperature, Humidity; no light entities exposed. |
| FW-03 | Components via `external_components:` | `type: local`, `path: ../components`, `components: [sim_bus]` (as in `examples/fan-skeleton.yaml`); works for the new platforms. |
| MODEL-03 | Choose which library fan is plugged into which port | Engine op `SetFan(port, id)` replaces the port's `model.Params` and resets `model.State`; names validated with `fanlib.Load`/`fanlib.IDs`; supervisor-held inputs so a sim restart keeps the choice. Optional `sil up --fan`, `sil fans` list. |
| CLI-01 | Full run status as JSON | Extend `control.Status` additively: `env`, `buttons`, `leds`; ports already carry fan/duty/rpm; sim time, bus_connected, api host/port/key exist. |
| CLI-02 | `sil set`, `sil press` | New API operations `POST /api/v1/set`, `POST /api/v1/buttons/{id}/press|down|up`; variadic `key=value` args need an `--instance` flag (positional name is taken). |
| CLI-03 | `sil watch` NDJSON | Chunked NDJSON on `api.sock` (`http.Flusher`) fed by a supervisor-owned event broker that survives engine replacement; snapshot-then-delta with sequence numbers. |
| CLI-04 | `sil logs` | `firmware.log` (ANSI colours, `=== firmware start N ===` separators) and `sil.log` (slog JSON) are plain files in the run dir; serve them through the API (tail, follow by polling on the injectable clock, ANSI stripped). |
| HA-01 | Add to any HA with host:port + key; restart keeps one device | MAC/name/key stability verified (same MAC and key across `sil down`/`sil up`); HA keys entities on MAC + entity static info. `api.host` is `127.0.0.1`, which a remote HA cannot reach: add a LAN address to status/`sil up` hint (Pitfall 9). Live HA = manual UAT. |
| HA-02 | HA controls fan speed and sees RPM/temp/humidity/buttons | Verified end to end with aioesphomeapi against the real firmware: fan command, RPM, `Temperature`/`Humidity` follow `in` frames within a loop tick, button states True/False. |
| TEST-01 | Contract test of the whole loop | pytest under `tests/`, drives `sil` via subprocess; chain and timings in Validation notes below (Common Pitfalls 12, 13). |
| TEST-02 | aioesphomeapi smoke test | Same suite; `aioesphomeapi==46.12.0` (matches the pinned image and the product HIL lock, hash-identical). |
</phase_requirements>

## Summary

Phase 2 is an extension of working Phase 1 code, and the riskiest unknowns were resolved by experiment on this host instead of by argument. In a scratch copy of the repo (`/tmp/p2-exp`, nothing was changed in the repo) I added the three missing sim platforms, wrote the full Rev 3.3 example, built it with `sil up` in the pinned image (compile + start in 8.4 s with a warm toolcache, 1.5 s warm), and drove it with a Python bus peer plus `aioesphomeapi`. Result: stock `partition` lights with the `pulse` effect work on top of a custom `AddressableLight`; the LED frame flood is solved by content dedupe plus a 50 ms minimum gap (about 15.6 Hz while a transition runs, zero frames when idle); buttons produce the exact product behaviour (USR1 +10 %, USR3 −10 %, USR2 1.5 s long press to 100 %); the API exposes exactly the 13 HIL contract entities and no light.

The wire protocol needs only additive changes: a new channel type `rgb` (with `n` and `seg` fields in the `chan` manifest frame, which carries the segment names derived from the `partition` lights at codegen) and a `px` hex string on LED `out` frames. Buttons, temperature and humidity are plain `f32` `in` channels. The Go engine grows from "4 fan ports" to a small World (ports, env, buttons, LEDs); its single-writer inbox design takes new operations naturally. Two things must move out of the engine so they survive `sil sim restart`: the user's inputs (fan per port, env values) and the event stream for `sil watch`; both belong in the supervisor.

Three non-obvious traps were found and are fixed in the recommended design: (1) ESPHome binary_sensor callbacks do not fire on the first published state, so the simulator must always send the released level first or the first press is lost; (2) the 1 s window-mean RPM sensor is wrong for temperature/humidity (blends values), so add `mode: latest`; (3) sil's own readiness probe is an API client, so a naive "green on client connected" turns the boot pulse green at once; filtering `client_info != "sil"` fixes it and makes the boot state observable and the contract test deterministic. Live Home Assistant behaviour cannot be tested here; the automated proxy is aioesphomeapi (device info, entity keys, fan round trip, identity across restart) and the rest is a manual UAT checklist (included).

**Primary recommendation:** Build bottom-up in this order: (1) sim_bus platforms + protocol/vectors + engine World, (2) supervisor inputs/event broker + control API + CLI verbs, (3) `examples/rev33.yaml` + default switch + README/HA docs, (4) `tests/` pytest project + `make test-contract`, then the full Phase 1 e2e as the regression gate. Copy the verified code from the Code Examples section instead of re-deriving it.

## Project Constraints (from CLAUDE.md)

From `/home/claude/CLAUDE.md` and `/home/claude/esphome-sil/.claude/CLAUDE.md`:

- Simulator, CLI and web server are **Go**, single static binary; **no Python in the simulator**. Python is allowed for product-repo style tests that drive `sil` via CLI/JSON (this phase's `tests/`).
- ESPHome pinned **2026.10.1**, image `ghcr.io/esphome/esphome:2026.10.1` (digest-pinned in `compile.sh`/fwbuild); never follow 2026.10.2.
- Hermetic build: no component that `cg.add_library()`s or auto-loads `json` (no `json:`, `web_server`, `mqtt`). The new platforms use none.
- Hand-written flat NDJSON parser in C++ (`sim_protocol.h`), no ArduinoJson. Sensors are pushed (`Component`), except the RPM sensor which is a recorded `PollingComponent` exception.
- Stack choices already fixed: kong CLI, stdlib `ServeMux`, go-toml, slog (stderr only), `testing` (no testify); no new Go dependency is needed or wanted for this phase.
- Locality: no CDN/cloud; the Web UI is Phase 3 (out of scope here).
- Wall-clock lint (MODEL-04): no `time.Now/Sleep/After/...`, `context.WithTimeout`, or stdlib timeout fields outside `internal/clock` (applies to all Go code including new streaming handlers and `sil press` hold timing).
- Error codes must be string literals registered in `apperr.Registry` with a fixed exit class, or `make check` fails (AST scan).
- Branch model: work on `claude/*`; commits by `claude-bot`; `claude: ` prefix only for coordinator-style projects. Do not push (maintainer checkpoints); do not touch port 8001 or other projects.
- GSD workflow enforcement: file changes go through GSD commands (execute-phase).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Button hold timing (200 ms / 1500 ms) | Supervisor / control handler (sim clock) | Engine (holds level) | The hold is a sim-time wait between two level changes; the firmware only sees levels and measures hold time itself. |
| Button click/long-press interpretation | Firmware (ESPHome `on_click`) | — | Same as hardware: the sim never classifies presses. |
| LED strip pixel buffer, gamma, effects, `partition` | Firmware (stock ESPHome) | — | Final pixels are produced by ESPHome; the sim only mirrors them. |
| LED frame dedupe and 20 Hz cap | Firmware (`SimLight`) | Hub replay on reconnect | Idle firmware must send nothing (D-07); doing it at the source keeps the bus and the engine quiet. |
| Segment names (`board`, `fan1`...) | Firmware YAML (codegen) → HELLO manifest | Engine (parses `seg`) | D-06: YAML is the single source of truth; no simulator-side config. |
| Temperature/humidity values, 10 s re-send | Engine (World) | Firmware (`mode: latest` publishes) | Sim owns the plant inputs; firmware just republishes what it receives. |
| Fan choice per port, plug/swap | Engine (model state reset) | Supervisor (holds the choice for restarts) | Plant model lives in the engine; run-state that must survive an engine replacement lives in the supervisor. |
| Event stream for `watch` | Supervisor (broker) | Engine (emits) | The engine is replaced on `sim restart`; the stream must not break. |
| Atomic multi-key `set` validation | Supervisor (validate) | Engine (apply in one event) | All-or-nothing: validate everything first, then one engine op. |
| Logs | Supervisor (reads run-dir files) | CLI (client) | CLI-06: CLI is a client of the API; files are on the supervisor's host. |
| Entity names, HA identity | Firmware YAML + Phase 1 identity (MAC/name/key) | HA | HA keys on MAC + entity static info. |
| Contract/smoke tests | `tests/` (pytest) | `sil` CLI/JSON, aioesphomeapi | Black-box: only CLI/JSON and the native API. |

## Experiment Results (what was built and run on this host)

All runs: scratch copy `/tmp/p2-exp` (Phase 1 tree + new files), isolated `XDG_*` dirs under `/tmp/p2x`, instances `p2a/p2b/p2c`, API ports 6054-6056, pinned image, toolcache copied from the e2e cache. Port 8001 and other projects untouched. [VERIFIED: executed on this host, 2026-11-09]

| # | Question | Result | Evidence |
|---|----------|--------|----------|
| E1 | Does a custom `AddressableLight` carry stock `partition` lights with `pulse`? | Yes. Compiles, runs, `chan` manifest lists it, pixels change as the effect runs. | `sil up p2a --config examples/rev33.yaml` ready in 8.4 s; peer log: `{"k":"chan","ch":"leds","dir":"out","type":"rgb","n":5,"seg":"status:0:0,fan1:1:1,fan2:2:2,fan3:3:3,fan4:4:4"}` |
| E2 | LED frame rate | While a transition/pulse runs: one frame every ~64 ms (≈ 15.6 Hz, i.e. 50 ms minimum gap rounded up to 4 main-loop ticks of ~16 ms), 73 frames in 6 s of continuous pulse. Idle (client connected, fans steady, RPM republishing every 1 s): **0 frames from t=3 s to t=12 s**. | peer frame log; per-second histogram `[(0,12),(1,16),(2,7)]` |
| E3 | Pixel colours are final colours? | Yes, and they are gamma-corrected: green at brightness 0.5 is `#002500`, not `#008000`; the `pulse` effect overrides brightness to 100 % and peaks at `#0000ff`. RGB byte order (blue = third byte). | frames `002500…`, `0000ff…` |
| E4 | Button chain | USR1 200 ms press → all duties 0.1 (from 0); USR3 → 0; USR2 1.5 s → all 1.0; click fires on release (duty at +11 ms after release, long press at 7.531 s for release at 7.5 s). | peer `out` frames |
| E5 | First-state swallow | If the first `in` for a button is `1`, `on_press`/`on_click` never fire; only the later release shows (log `usr1 release` without `usr1 press`). With a level-0 `in` first, everything fires. Source: `StatefulEntityBase::set_new_state` fires state callbacks only if `had_state \|\| get_trigger_on_initial_state()`. | firmware log; ESPHome source `entity_base.h` read in the image |
| E6 | API entity set | 13 entities, exactly the contract names; object ids `button_usr1`, `fan_1`, `fan_1_speed`, `temperature`, `humidity`; the sim light and all partitions are not listed. | aioesphomeapi 46.12.0 `list_entities_services` |
| E7 | HA-style round trip | `fan_command(Fan 1, speed_level=40)` → bus `port1.duty` change; RPM `in` frame → `Fan 1 Speed` 900.0 republished every second; USR1 press moved Fan 1 40 → 50 and others 0 → 10. | api_probe output |
| E8 | Windowed mean vs latest for env | Mean mode: `Temperature` 22.0 then 22.64 after a 22 → 25.5 step (blend). `mode: latest`: 22.0, 25.5, 26.5 exactly at the three changes. | api_probe output |
| E9 | `on_client_connected` and sil's probe | A handshake-only client (aioesphomeapi `login=False`) **does** fire `on_client_connected` (HelloRequest suffices). sil's probe sends `client_info = "sil"` (`internal/apiclient/probe.go:51`). Filtering `client_info != "sil"` keeps the boot pulse blue until a real client connects; a real client turns the board LED green 65 ms after connecting. | frame timestamps vs client wall clock |
| E10 | Phase 1 skeleton unaffected | `examples/fan-skeleton.yaml` still builds and runs on the extended hub; status `warnings: []`. | `sil up p2c` |
| E11 | Identity stability | `sil down` + `sil up` of the same name: same MAC `02:fe:43:f2:6b:4a`, same key, same port; warm `sil up` 1.5 s (build skip). Firmware listens on `0.0.0.0:6054`. | `sil up/status`, `ss -ltn` |
| E12 | Test tooling | `uv lock` + `uv sync --frozen` + `uv run --frozen pytest` work on Python 3.12 (uv-managed); the resolved `aioesphomeapi 46.12.0`, `pytest 9.2.0`, `pytest-timeout 2.4.0` have **identical artifact hash sets** to `wifi-fancontroller/hil/uv.lock`. | hash comparison script |

## Standard Stack

### Core (no new Go dependencies)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `net/http` (`ServeMux`, `http.Flusher`) | Go 1.27.3 [VERIFIED: `go version`] | New control endpoints, NDJSON stream for `watch`/`logs --follow` | Already used by `internal/control`; chunked responses over the unix socket need only `Flusher`. |
| `github.com/alecthomas/kong` | v1.17.0 (go.mod, Phase 1) | `set`, `press`, `watch`, `logs`, `fans` commands | Already the CLI; `[]string` arg for `key=value` pairs. |
| `go.yaml.in/yaml/v3`, `flynn/noise`, `pelletier/go-toml/v2` | as in Phase 1 `go.mod` | unchanged | Not touched. |
| ESPHome components `output`, `sensor`, `binary_sensor`, `light`, `light: partition`, `script`, `api` | 2026.10.1 (pinned image) | Rev 3.3 surface | Stock; only the `sim_bus` platforms are custom. |

### Supporting (test tooling only)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `aioesphomeapi` | 46.12.0 | TEST-02 and API observation in TEST-01/HA-01 proxy | Pinned: ships in the ESPHome image, pinned in the product HIL lock |
| `pytest` | 9.2.0 | Test runner | Same as product HIL |
| `pytest-timeout` | 2.4.0 | Hard per-test deadline (`timeout_method = "signal"`) | Same as product HIL |
| `uv` | 0.10.2 [VERIFIED: `uv --version`] with managed Python 3.12.12 | Locked env: `tests/pyproject.toml` + committed `tests/uv.lock` | D-14 |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| New engine event channel per consumer | Poll `GET /status` in `watch` | Misses transient LED frames and press edges; D-08 wants every frame. Use the broker. |
| SSE / WebSocket for `watch` | NDJSON over chunked HTTP | WebSocket is Phase 3 (UI). NDJSON matches D-12 and `curl`/`jq`. Do not add `coder/websocket` now. |
| fsnotify for `logs --follow` | Poll file size every 200 ms on `clock.Sleep` | No new dependency; trivially lint-clean. |
| Python inside the ESPHome image (Phase 1 `e2e/api_client.py`) | uv + pytest on host | D-14 mandates uv/pytest; keep `e2e/api_client.py` for the Go e2e only. |

**Installation (tests only):**
```bash
cd tests && uv lock && uv sync --frozen        # first time; commit tests/pyproject.toml + tests/uv.lock
uv run --frozen pytest                         # what make test-contract wraps
```

**Version verification:** `go version` → go1.27.3; `uv --version` → 0.10.2; `aioesphomeapi` 46.12.0 is the version in the pinned image and in `wifi-fancontroller/hil/uv.lock` (PyPI latest is 46.28.0; do not bump, it must match the product repo and HA's behaviour per `.claude/CLAUDE.md`). [VERIFIED: executed on this host]

## Package Legitimacy Audit

`gsd-tools query package-legitimacy check --ecosystem pypi aioesphomeapi pytest pytest-timeout` returned `SUS` for all three with reasons `too-new` (the seam looks at the latest release date, within days of today) and `unknown-downloads` (PyPI gives no download counts). Those are artifacts of the heuristic, not of these pins: the exact pinned versions are hash-identical to the maintainer's own reviewed `wifi-fancontroller/hil/uv.lock` (E12), `aioesphomeapi 46.12.0` is shipped inside the pinned ESPHome image, and pytest/pytest-timeout are the standard test stack.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| aioesphomeapi 46.12.0 | PyPI | years (latest 46.28.0 published 2026-11) | n/a (not reported) | esphome.io / github.com/esphome/aioesphomeapi | SUS (too-new, unknown-downloads) | Approved with automated gate: planner adds a task that diffs `tests/uv.lock` hashes of the three packages against `../wifi-fancontroller/hil/uv.lock` (script in this research, E12) instead of a human checkpoint. |
| pytest 9.2.0 | PyPI | years | n/a | github.com/pytest-dev/pytest | SUS (too-new, unknown-downloads) | Same gate. |
| pytest-timeout 2.4.0 | PyPI | years | n/a | pytest-dev | SUS (too-new, unknown-downloads) | Same gate. |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** the three above. All three are pre-existing in the user's own lockfile; if the hash diff ever fails (a bump, a different transitive set), fall back to the `checkpoint:human-verify` the seam asks for. No new Go modules are introduced.

## Architecture Patterns

### System Architecture Diagram

```
                        +-------------------- sil serve <name> (supervisor) --------------------+
 sil set / press /      |                                                                          |
 watch / logs / status  |  api.sock (HTTP)      +--------------------+     +------------------+    |
 ----(CLI client)------>|  /status /set /press->| control.Handler   |---->| inputs (held):   |    |
                        |  /watch /logs /fans   |  validate (all-or- |     |  fan per port,   |    |
                        |                       |  nothing)          |     |  temp, humidity  |    |
                        |                       +----------+---------+     +--------+---------+    |
                        |                  ops (reply chan)|  events                | replay on   |
                        |                                  v      ^ Emit            v new engine  |
                        |                       +----------+------+----------------------------+   |
                        |                       | engine (single-writer goroutine, fresh World) |   |
                        |   event broker <------| ports[model]  env  buttons[3]  leds[strip+seg]|   |
                        |   (survives sim       +----+------------------------------------^-----+   |
                        |    restart, seq#)          | in: portN.rpm, temperature,        | out:    |
                        |                            |     humidity, usr1..3 (levels)     | portN.  |
                        +----------------------------|------------------------------------|duty,leds|
                                                     v   bus.sock (NDJSON over UDS)       |         |
                        +----------------------------+------------------------------------+---------+
                        | firmware process (ESPHome host, static)                                    |
                        |  sim_bus hub: hello+chan manifest(+rgb n/seg) -> out snapshot -> sync       |
                        |  SimOutput  SimSensor(mean|latest)  SimBinarySensor  SimLight(dedupe+50ms)  |
                        |  stock: fan:speed, partition lights + pulse, script, api on_client_connected|
                        +-------------------------------+--------------------------------------------+
                                                        | native API :port (Noise, 0.0.0.0)
                              aioesphomeapi tests / Home Assistant (13 contract entities)
```

Primary use case, button to LED: `sil press usr1` → control handler → engine level 1 (+ `in usr1 1` on the bus) → handler waits 200 ms of sim clock → engine level 0 (+ `in usr1 0`) → returns JSON. Firmware: `on_click` (50 ms ≤ hold ≤ 1000 ms) → script → fans +10 % → `out portN.duty` → engine model → `in portN.rpm` (every change, 50 ms) → firmware sensor publishes 1 s mean → `on_value` → fan LED `turn_on` green → `out leds px=...` → engine LED state → event `leds` → `watch` and `status`.

### Recommended Project Structure
```
components/sim_bus/
├── __init__.py            # hub schema; extend check_unique_channels domains; label/segment helpers
├── output.py sensor.py    # sensor.py gains  mode: window_mean | latest
├── binary_sensor.py       # NEW  (IO-03)
├── light.py               # NEW  (IO-04)  derive segments from partition lights
├── sim_bus.{h,cpp}        # hub: type()/pixels()/segments() in manifest; set_out_px + replay
├── sim_binary_sensor.h    # NEW
├── sim_light.{h,cpp}      # NEW
└── sim_protocol.h         # unchanged codec (generic); PROTO stays 1
examples/
├── rev33.yaml             # NEW, becomes the default of `sil up`
└── fan-skeleton.yaml      # kept: minimal Phase 1 regression config
fans/                      # + optional second fan (see Open Questions)
internal/
├── bus/                   # Channel gains N, Seg; vectors for rgb chan + px out
├── engine/                # World: ports, env, buttons, leds; ops; Emit
├── events/                # NEW  broker (seq, bounded subscribers)
├── control/               # Status additions; set/press/watch/logs/fans handlers + client
├── supervisor/            # inputs (held run state); press timing; logs reader; LAN host
└── cli/                   # set, press, watch, logs, fans (+ --instance flag)
protocol/{PROTOCOL.md,vectors/frames.tsv}   # rgb chan, px out
tests/                     # NEW  pyproject.toml, uv.lock, conftest.py, test_contract.py, test_api_smoke.py
Makefile                   # + test-contract
```

### Pattern 1: Additive wire extension (no protocol bump)
**What:** Keep `PROTO = 1`. Buttons/env are ordinary `f32` `in` channels. The LED strip is one `out` channel of `type:"rgb"`; its `chan` frame carries `n` (pixel count, int) and `seg` (string `label:from:to,...`); its `out` frames carry `px` (hex string, 2 chars per byte, RGB order, `3*n` bytes) instead of `v`.
**When to use:** Always for this phase; both codecs are generic flat-object codecs, so only vectors and the consumers change. Add vectors (`chan_rgb`, `out_px`) to `protocol/vectors/frames.tsv` so Go and C++ tests pin them (BUS-04).
**Example (verified on the wire):**
```json
{"k":"chan","ch":"leds","dir":"out","type":"rgb","n":5,"seg":"status:0:0,fan1:1:1,fan2:2:2,fan3:3:3,fan4:4:4"}
{"k":"out","ch":"leds","px":"000025000000000000000000000000","t":410649349000}
```
Limits: a line is ≤ 4095 bytes, so reject `num_leds` > 255 at codegen; the hex string for 5 LEDs is 30 chars.
Engine rule changes: ports stay *required* channels (warning when missing); every other channel (`temperature`, `humidity`, `usr1..3`, `leds`) is *optional*: its presence in the manifest enables the matching status fields and operations; a wrong direction/type is a warning (as today). This keeps `examples/fan-skeleton.yaml` warning-free (verified E10) and makes `sil press usr2` on a firmware without buttons a loud error (`input_not_available`, exit 1), not a silent no-op.

### Pattern 2: `SimLight` (dedupe + 50 ms gap + replay)
**What:** Mirror the ESPHome-written buffer (`buf_`) as hex; send only if the content differs from the last sent frame and at least 50 ms passed since the last send; a pending change is flushed from `loop()`. The hub stores the last pixel string and replays it in the connect snapshot (before `sync`), exactly like float outputs. See Code Examples for the verified source.
**Why in the firmware:** idle firmware sends nothing (D-07); `pulse` calls `write_state` on every main-loop tick during a transition, the dedupe alone does not cap it, the gap does.
**Segment names (D-06 resolved):** derive from the stock `partition` lights at codegen: `light.py` scans `CORE.config["light"]` for `platform: partition` entries whose segment `id` is this light and emits `label:from:to` with the partition's from/to (single source of truth for ranges). Label = partition `id` minus a trailing `_led` (`fan1_led` → `fan1`, `status_led` → `status`). Add an optional `segment_labels: {status_led: board}` map on the sim light so the product's ids (`status_led`) can stay while the status says `board` as in D-05; labels must match `^[a-z][a-z0-9_]*$` and be unique; an id in the map that is not a partition of this light is a codegen error.

### Pattern 3: Buttons as levels, initial level mandatory
**What:** `SimBinarySensor::on_value(v)` → `publish_state(v > 0.5f)`. The simulator's `in` snapshot MUST include `usr1..3 = 0` (it sends every in channel with a known value, so initialise the World with released buttons). The state that arrives first is the "initial state": ESPHome does not run `on_press`/`on_click` for it.
**Press timing:** hold is a sim-clock wait between two engine ops: `Button(id, true)` → `clk.Sleep(ctx, hold)` → `Button(id, false)`. The result JSON carries `held_ms`, `t_down_us`, `t_up_us` (sim time). If the engine was replaced during the hold, the release op is applied to the new engine and treated as idempotent for the press flow only.

### Pattern 4: Environment values: engine owns, firmware `mode: latest`
**What:** Engine keeps `temperature`, `humidity` (defaults 22 / 45); sends `in` on change and re-sends both every 10 s of sim time (tick in the same loop as `nextRPM`); the firmware sensor uses `mode: latest` (publish every received value). ESPHome republishes unchanged values to the API (Phase 1 note), mirroring a polled HDC1080. Validation bounds for `sil set`: temperature −40..125 °C, humidity 0..100 % (HDC1080 operating range) `[ASSUMED]`, finite numbers only.

### Pattern 5: Supervisor-held inputs and event broker
**What:** `supervisor` keeps `inputs{fans [4]string, temperature, humidity}` (guarded by `mu`), seeded from `spec.json` (`Spec.Fans`) and defaults; `engine.Config` is built from it at every `startEngine`, so `sil sim restart` replays the user's choices. `sil fw restart` never touches the engine. Buttons are *not* held state: a fresh World has them released, so a sim restart releases a held button (document this).
A new `internal/events` broker (publish with monotonically increasing `seq`, subscribers with bounded buffers; a slow subscriber gets a `{"k":"lagged","dropped":N}` marker rather than blocking the engine) lives in the supervisor and is passed to `engine.Config` as an `Emit func(Event)`. `watch` = subscribe first, then take a status snapshot with its `seq`, write the snapshot line, then stream events with `seq` greater than the snapshot's (no gap, no duplicate).
**Event kinds (planner may rename):** `snapshot`, `duty{port,v}`, `rpm{port,v}` (sampled every 1 s of sim time per port, only when changed; the sim cannot see the firmware's publish instant), `state{port,v}`, `fan{port,fan}`, `button{id,v}`, `leds{strip}`, `env{key,v}`, `firmware{connected}`, `lagged`. Every event carries `t` (µs sim time) and `k`.

### Pattern 6: Control API surface (one vocabulary for CLI and the later UI)
| Method path | Body | Result / errors |
|-------------|------|-----------------|
| `GET /api/v1/status` | — | `Status` + `env`, `buttons`, `leds` (additive, schema stays 1) |
| `POST /api/v1/set` | `{"values":{"temperature":24.5,"port2.fan":"…"}}` | `{"applied":{…}}`; `invalid_argument`/`unknown_key`/`value_out_of_range`/`unknown_fan` exit 1, nothing applied |
| `POST /api/v1/buttons/{id}/press` | `{"hold_ms":200}` | `{"button","held_ms","t_down_us","t_up_us"}`; `button_busy` exit 1 if held |
| `POST /api/v1/buttons/{id}/down` / `up` | — | `{"button","level"}`; `button_busy` / `button_not_held` exit 1 |
| `GET /api/v1/watch?kind=a,b` | — | NDJSON stream, snapshot first |
| `GET /api/v1/logs?source=firmware\|sim&tail=N&follow=0\|1` | — | JSON `{"source","lines":[…]}`, or NDJSON `{"src","line"}` when following |
| `GET /api/v1/fans` | — | library listing (also usable offline: `sil fans` can read `fanlib` directly) |

Reject press/down/up with `firmware_not_connected` (exit 1) while the bus is down (the edge would be lost or replayed stale); `set` is accepted anytime (replayed on resync). Cap `hold_ms` at 60 s with `invalid_argument`. `Status` is built without taking `opMu` (keep that property).

### Pattern 7: CLI shape
`sil set <key=value>... [--instance NAME]`, `sil press <usr1|usr2|usr3> [--long | --hold DUR | --down | --up] [--instance NAME]`, `sil watch [--kind k1,k2] [--instance NAME]`, `sil logs [--source firmware|sim|all] [--tail N] [--follow] [--raw] [--instance NAME]`, `sil fans`, and `sil up --fan NAME` (all four ports; per-port stays `sil set portN.fan=`). `--instance` falls back to `SIL_INSTANCE`, then the single live instance (existing `resolveInstance`). One JSON value per command (CLI-05) except `watch` and `logs --follow`, which are NDJSON streams that end with exit 0 when the instance stops or on SIGINT; connection loss mid-stream after a successful start is exit 2 `control_unreachable`. The control `Client.do` caps bodies at 1 MiB and reads all of it: streaming needs a separate client method returning a reader.

### Pattern 8: Test layout
`tests/conftest.py` builds nothing itself: it requires `SIL_BIN` (default `bin/sil`, built by `make build`), creates a short temp dir (`tempfile.mkdtemp(prefix="sil-ct-", dir="/tmp")`, mode 0700) used as `XDG_RUNTIME_DIR` and `XDG_STATE_HOME` (keeps socket paths ≤ 107 bytes and leaves no identity dirs behind), keeps the real `XDG_CACHE_HOME` (shared toolcache; an instance name is a build-cache key), and starts instance `ct-<4 random hex>` through `sil up --quiet`. Session-scoped fixture, `sil down` in finalizer. `sil watch` runs as a `Popen` with a reader thread feeding a queue; assertions poll with deadlines (no fixed sleeps). `make test-contract`: `build` then `uv run --project tests --frozen pytest tests`.

### Anti-Patterns to Avoid
- **Sending `usr*` only on press:** the first state is swallowed; always include released levels in the snapshot (Pitfall 1).
- **Window-mean for env sensors:** blends values; use `mode: latest`.
- **Reading LED state from the API (HA):** status LEDs are internal and not exposed; only `sil status`/`watch` show them.
- **Holding user inputs in the engine:** lost on `sim restart`.
- **A second state source for `watch`:** do not poll; emit from the engine.
- **Exact colour equality in tests:** values are gamma-corrected; classify by dominant channel and threshold.
- **Using `context.WithTimeout`/`time.After` in streaming handlers or `press`:** the wall-clock lint fails the build; use `clock.WithTimeout`, `clk.Sleep`, `clk.NewTimer`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| LED strip colour correction, brightness, effects | Own colour pipeline | ESPHome `AddressableLight` + `ESPColorView` (`correction_`) | The buffer already holds final corrected pixels (E3); mirror it. |
| LED sub-ranges | Own segment config in Go | Stock `light: partition` + codegen-derived `seg` | D-06; single source of truth. |
| Click/long-press logic | Press classification in the sim | ESPHome `binary_sensor` `on_click: min_length/max_length` | Firmware measures hold time; the sim sends levels. |
| Fan on/off/speed to duty | Anything custom | stock `fan: speed` over `SimOutput` | Phase 1. |
| Native API client in tests | Own protobuf/Noise client | `aioesphomeapi==46.12.0` | The same library HA uses. |
| NDJSON framing, event ordering | Ad-hoc per-handler writers | One `events` broker + one `writeNDJSON(w, flusher)` helper | Ordering, snapshot/delta join and slow-consumer policy are easy to get subtly wrong. |
| Log tailing | Shell out to `tail` | Read the last N lines from the end of the file in 64 KiB blocks + poll size for follow | No process spawn, lint-clean, testable. |
| ANSI colour stripping | Terminal parsing | One regexp `\x1b\[[0-9;]*m` on the firmware log | ESPHome's logger only emits SGR sequences. |
| Argument parsing for `key=value` | Custom tokenizer | kong `[]string` arg + `strings.Cut(arg, "=")` | Values are numbers or fan ids; no quoting rules needed. |
| Python env management | pip/venv scripts | `uv` project with committed `uv.lock` | D-14, identical to the product HIL. |

**Key insight:** every hard part of this phase is already done by ESPHome or by Phase 1's engine/bus; the new code is thin glue plus the discipline of "dedupe at the source, state in the supervisor".

## Common Pitfalls

### Pitfall 1: First button state is swallowed
**What goes wrong:** `sil press usr1` does nothing the very first time.
**Why it happens:** ESPHome `StatefulEntityBase` does not fire state callbacks for the first state of an entity (E5). If the first `in usr1` is `1`, `on_press`/`on_click` miss it.
**How to avoid:** Initialise World buttons as released and include them in the `inSnapshot` (every in channel with a known value is sent). A unit test: snapshot contains `usr1=0..usr3=0` before any press.
**Warning signs:** `usr1 release` in the firmware log without `usr1 press`.

### Pitfall 2: Temperature blends under the window mean
**What goes wrong:** `Temperature` shows 22.64 after setting 25.5.
**Why it happens:** `SimSensor` publishes the time-weighted mean per `update_interval` (right for RPM, wrong for env).
**How to avoid:** `mode: latest` for `Temperature` and `Humidity` in the example; `update()` is a no-op in that mode.

### Pitfall 3: Boot pulse turns green immediately
**What goes wrong:** the board LED is green right after `sil up`; the blue boot state is never seen.
**Why it happens:** sil's readiness probe is an API client (HelloRequest triggers `on_client_connected`).
**How to avoid:** `lambda: 'return client_info != "sil";'` in the hook (E9). Keep `clientInfo = "sil"` in `internal/apiclient/probe.go:51`; a test should break if either side changes (assert board LED blue after `sil up`).

### Pitfall 4: LED frame flood or stale last frame
**What goes wrong:** 60 Hz `leds` events during `pulse`, or a missing final colour after a rate-limited burst.
**How to avoid:** dedupe by content, 50 ms gap, flush pending from `loop()` so the final frame always leaves (Code Examples). Replay the last frame on reconnect.

### Pitfall 5: Pulse overrides brightness; colours are gamma-corrected
**What goes wrong:** a test expects `#0000ff` at 50 % or `#008000` for 50 % green.
**How to avoid:** assert on dominant channel + threshold: board pulse blue = `R==G==0 and B>0x20` in at least one frame within 2 s; green = `G>=0x20, R==B==0`. Static green at 0.5 brightness is `#002500` (E3).

### Pitfall 6: Error codes not registered
**What goes wrong:** `make check` fails in `TestRegistryMatchesCallSites`.
**How to avoid:** add every new literal code to `apperr.Registry` with its class in the same task: exit 1 (user-fixable): `unknown_key`, `value_out_of_range`, `unknown_fan`, `button_busy`, `button_not_held`, `input_not_available`, `firmware_not_connected`, `unknown_source`; reuse `invalid_argument`, `not_running`. Exit 2 only for infrastructure (`logs_unreadable`). Codes must be string literals at the call site; a registry entry nobody uses also fails.

### Pitfall 7: Positional instance name vs variadic args
**What goes wrong:** `sil set temperature=24.5` parses `temperature=24.5` as the instance name.
**How to avoid:** `--instance`/`-i` flag for set/press/watch/logs; keep optional positional name for the Phase 1 commands.

### Pitfall 8: Streaming through the existing client
**What goes wrong:** `watch` hangs or hits the 1 MiB cap; a handler without `Flush` buffers the whole stream.
**How to avoid:** new `Client.Stream(ctx, path) (io.ReadCloser, error)`; server handler checks `http.Flusher`, writes a line, flushes, exits on `r.Context().Done()` and on instance shutdown (the broker closes subscribers); `ServeListener`'s `Shutdown` waits for handlers, so a never-ending stream would delay `sil down`: close all subscribers in `stopAll` before shutting the server down. `http.Server` timeouts stay unset (lint).

### Pitfall 9: `127.0.0.1` is useless for a remote Home Assistant
**What goes wrong:** the user copies `host 127.0.0.1` into HA on another machine; the firmware (bound to `0.0.0.0`, E11) is reachable only by its LAN address.
**How to avoid:** add `api.lan_host` to status and to the `sil up` hint: the IPv4 address of the interface holding the default route (UDP `connect` to a public address without sending, read `LocalAddr`), `null` when unknown; keep `api.host` as is. This host has several candidates (`eth0 192.168.4.39`, docker bridges), so do not pick "first non-loopback". Mention the plaintext-vs-Noise note: the key is required, the port is exposed to the LAN.

### Pitfall 10: Sim restart loses inputs or releases a held button
**How to avoid:** inputs live in the supervisor and seed the new engine (Pattern 5); buttons restart released (documented); the firmware sees the release through the snapshot.

### Pitfall 11: Fan swap semantics
**What goes wrong:** RPM jumps or the old running state leaks into the new fan.
**How to avoid:** `SetFan` replaces `cfg.Fan` and sets `state = model.Initial(newParams)`; RPM is 0 at once (an `in portN.rpm 0` is sent by the normal change rule); the new fan starts from the current duty at the next tick via `model.Step` (hysteresis start rule applies). Emit `fan{port}` and `rpm`/`state` events.

### Pitfall 12: Boundary values in contract assertions
**What goes wrong:** Fans 2-4 do not start after one USR1 press.
**Why it happens:** +10 % from off is duty 0.1, exactly the shipped `start_duty = 0.10` (`fans/arctic-p12-pwm-pst.toml`: `start_duty = 0.10`, ASSUMED constant from Phase 1). `float32(0.1)` round-trips through `GetNum` as `0.1`, so it works today, but it rests on a boundary.
**How to avoid:** assert RPM/LED effects on Fan 1 (first set to 40 % through the API, USR1 → 50 %); assert only duty (0.1) on Fans 2-4. If an assertion on their start is wanted, press twice.

### Pitfall 13: Test isolation and time
**What goes wrong:** pytest runs collide with a developer instance, leave state dirs, or exceed the socket path limit.
**How to avoid:** Pattern 8 (short `XDG_RUNTIME_DIR`, throw-away `XDG_STATE_HOME`, unique name, `sil down` in teardown, `--api-port` unspecified so Phase 1's sticky/free-port logic applies). Expected timings from this host: cold `sil up` of a new name ≈ 8-10 s, warm 1.5 s, RPM settle to ±2 % at Fan 1 40 % about 4-5 s (`tau_up = 1.0`), LED fan colour about 2 s after RPM > 0 (1 s publish + 1 s default transition).

### Pitfall 14: Default config switch breaks Phase 1 e2e
**What goes wrong:** `sil up` without `--config` now builds the Rev 3.3 firmware; Phase 1 e2e tests that rely on the default silently change.
**How to avoid:** switch `defaultConfig` (`internal/lifecycle/lifecycle.go:30`) and the `--config` help text in the last plan and gate it on the full `make e2e` (34 tests, about 4.6 min at Phase 1 close). Keep `fan-skeleton.yaml` and its explicit uses (`e2e/harness_test.go`, `e2e/codegen_test.go`, `internal/fwbuild/lint_test.go`). If the e2e run shows breakage that is not cheap to fix, keep the default and pass `--config examples/rev33.yaml` in `make test-contract` and the README.

### Pitfall 15: Firmware log has ANSI colours; sil.log is JSON
**How to avoid:** `sil logs` strips ANSI by default (`--raw` keeps it), labels `=== firmware start N ===` separator lines as plain lines, and passes `sil.log` lines through as strings (they are slog JSON when the supervisor's stderr is not a TTY). Never serve `build.log` through `logs` unless it goes through the same key redaction as `withLogTail`.

## Code Examples

Verified by build + run on this host unless noted. Source of truth for the rest of the plan: copy these.

### `SimLight` (components/sim_bus/sim_light.h / .cpp)
```cpp
// sim_light.h  (guarded by USE_LIGHT; platform-free hub header stays unchanged except for the manifest hooks)
class SimLight : public light::AddressableLight, public SimChild {
 public:
  static constexpr uint32_t kMinGapMs = 50;  // 20 Hz cap (effective ~15.6 Hz with a ~16 ms main loop)
  const char *direction() const override { return "out"; }
  const char *type() const override { return "rgb"; }
  int pixels() const override { return this->n_; }
  const std::string &segments() const override { return this->segs_; }
  void set_segments(const std::string &s) { this->segs_ = s; }
  void set_num_leds(uint16_t n) { this->n_ = n; }
  void setup() override { this->buf_.assign(this->n_ * 3, 0); this->effect_.assign(this->n_, 0); }
  void loop() override;                       // flushes a pending (rate-limited) frame
  float get_setup_priority() const override { return setup_priority::HARDWARE; }
  int32_t size() const override { return this->n_; }
  void clear_effect_data() override { std::fill(this->effect_.begin(), this->effect_.end(), 0); }
  light::LightTraits get_traits() override {
    auto t = light::LightTraits();
    t.set_supported_color_modes({light::ColorMode::RGB});
    return t;
  }
  void write_state(light::LightState *state) override;
  void on_connected() override { this->pending_ = true; }
 protected:
  light::ESPColorView get_view_internal(int32_t i) const override {
    uint8_t *p = const_cast<uint8_t *>(this->buf_.data()) + i * 3;
    return {p + 0, p + 1, p + 2, nullptr, const_cast<uint8_t *>(&this->effect_[i]), &this->correction_};
  }
  void maybe_send_();
  uint16_t n_{0}; std::string segs_, last_sent_;
  std::vector<uint8_t> buf_, effect_;
  bool pending_{false}; uint32_t last_send_ms_{0};
};

// sim_light.cpp
void SimLight::write_state(light::LightState *) { this->pending_ = true; this->maybe_send_(); }
void SimLight::loop() { if (this->pending_) this->maybe_send_(); }
void SimLight::maybe_send_() {
  if (this->bus_ == nullptr) return;
  std::string cur = hex(this->buf_);                       // 2 hex chars per byte, RGB order
  if (cur == this->last_sent_ && this->bus_->connected()) { this->pending_ = false; return; }
  const uint32_t now = millis();
  if (this->last_send_ms_ != 0 && now - this->last_send_ms_ < kMinGapMs) return;   // loop() retries
  this->last_send_ms_ = now ? now : 1;
  this->last_sent_ = cur; this->pending_ = false;
  this->bus_->set_out_px(this->channel_, cur);             // stores for replay, sends if connected
}
```
Hub additions (`sim_bus.h/.cpp`): `SimChild::type()` (default `"f32"`), `pixels()`, `segments()`; in `try_connect_` the `chan` frame becomes `f.str("k","chan").str("ch",…).str("dir",…).str("type", c->type()); if (c->pixels() > 0) f.i64("n", c->pixels()).str("seg", c->segments());`; `last_px_` vector of `(channel, hex)` replayed after `last_out_` and before `sync`; `set_out_px` mirrors `set_out` (dedupe, store, send+flush when connected); `send_px_` encodes `k,ch,px,t`.

### `light.py` segment derivation (codegen)
```python
from esphome.const import CONF_ID, CONF_NUM_LEDS, CONF_OUTPUT_ID, CONF_SEGMENTS, CONF_FROM, CONF_TO
from esphome.core import CORE

def derive_segments(config):
    """label:from:to for every stock partition light that has this light as a segment source."""
    me = config[CONF_ID].id
    out = []
    for p in CORE.config.get("light", []):
        if p.get("platform") != "partition":
            continue
        for seg in p.get(CONF_SEGMENTS, []):
            sid = seg.get(CONF_ID)
            if sid is not None and getattr(sid, "id", str(sid)) == me:
                label = p[CONF_ID].id
                label = label[:-4] if label.endswith("_led") else label   # then apply segment_labels override, validate charset/uniqueness
                out.append(f"{label}:{seg[CONF_FROM]}:{seg[CONF_TO]}")
    return ",".join(out)
```
Schema: `light.ADDRESSABLE_LIGHT_SCHEMA.extend({cv.GenerateID(CONF_OUTPUT_ID): cv.declare_id(SimLight), cv.Required(CONF_NUM_LEDS): cv.positive_not_null_int})` + `CHILD_SCHEMA` + `cv.COMPONENT_SCHEMA`; `to_code`: `new_Pvariable(CONF_OUTPUT_ID)`, `register_component`, `light.register_light`, `set_num_leds`, `set_segments`, `register_child`. Extend `check_unique_channels` to scan `("output","sensor","binary_sensor","light")`. (Unverified extension: the `segment_labels` override and the charset/uniqueness validation; the rest is the executed code.)

### Binary sensor and sensor mode
```cpp
class SimBinarySensor : public binary_sensor::BinarySensor, public Component, public SimChild {
 public:
  const char *direction() const override { return "in"; }
  void on_value(float v) override { this->publish_state(v > 0.5f); }
};
// SimSensor: void set_publish_latest(bool b); on_value(): if (latest_) { publish_state(v); return; }  update(): if (latest_) return;
```
```python
# sensor.py: add  cv.Optional("mode", default="window_mean"): cv.one_of("window_mean", "latest", lower=True)
# to_code: cg.add(var.set_publish_latest(config["mode"] == "latest"))
```

### `examples/rev33.yaml` (verified: builds, lint-clean, runs; channels use the names below)
```yaml
substitutions: {sil_name: dev, sil_mac: "02:00:00:00:00:01", sil_port: "6054"}   # defaults only; sil injects with -s
esphome:
  name: sil-${sil_name}
  friendly_name: SiL ${sil_name}
  build_flags: [-static]
  on_boot:
    - then:
        - lambda: |-
            auto call = id(status_led).turn_on();
            call.set_rgb(0.0, 0.0, 1.0); call.set_brightness(0.5); call.set_effect("pulse"); call.perform();
host: {mac_address: "${sil_mac}"}
logger: {level: INFO}
api:
  port: ${sil_port}
  reboot_timeout: 0s
  encryption: {key: !env_var SIL_API_KEY}
  on_client_connected:
    # sil's own readiness probe identifies itself as "sil"; only a real client (HA, a test) counts.
    - if:
        condition: {lambda: 'return client_info != "sil";'}
        then:
          - lambda: |-
              auto call = id(status_led).turn_on();
              call.set_rgb(0.0, 1.0, 0.0); call.set_brightness(0.5); call.set_effect("none"); call.perform();
external_components:
  - source: {type: local, path: ../components}
    components: [sim_bus]
sim_bus: {id: bus}
script:
  - id: step_fans
    mode: queued
    parameters: {delta: int}
    then:
      - lambda: |-
          auto step = [&](fan::Fan *f) {
            int cur = f->state ? f->speed : 0;
            int n = std::max(0, std::min(100, cur + delta));
            auto call = f->make_call();
            if (n == 0) { call.set_state(false); } else { call.set_state(true); call.set_speed(n); }
            call.perform();
          };
          step(id(fan1)); step(id(fan2)); step(id(fan3)); step(id(fan4));
output:                                   # N = 1..4
  - {platform: sim_bus, id: pwm_fanN, channel: portN.duty}
fan:                                      # N = 1..4
  - {platform: speed, output: pwm_fanN, name: "Fan N", id: fanN, speed_count: 100, restore_mode: ALWAYS_OFF}
sensor:
  - platform: sim_bus                     # N = 1..4: RPM, 1 s window mean (Phase 1 platform)
    channel: portN.rpm
    name: "Fan N Speed"
    id: fanN_speed
    unit_of_measurement: RPM
    accuracy_decimals: 0
    on_value:
      - if:
          condition: {lambda: 'return x > 0;'}
          then: [{light.turn_on: {id: fanN_led, red: 0%, green: 100%, blue: 0%, brightness: 50%}}]
          else: [{light.turn_off: {id: fanN_led}}]
  - {platform: sim_bus, channel: temperature, name: "Temperature", id: fancontroller_temperature,
     unit_of_measurement: "°C", device_class: temperature, accuracy_decimals: 1, mode: latest}
  - {platform: sim_bus, channel: humidity, name: "Humidity", id: fancontroller_humidity,
     unit_of_measurement: "%", device_class: humidity, accuracy_decimals: 1, mode: latest}
binary_sensor:
  - platform: sim_bus
    channel: usr1
    name: "Button USR1"
    id: btn_usr1
    on_click: {min_length: 50ms, max_length: 1000ms, then: [{script.execute: {id: step_fans, delta: 10}}]}
  - platform: sim_bus
    channel: usr2
    name: "Button USR2"
    id: btn_usr2
    on_click:
      min_length: 1000ms
      max_length: 10s
      then:
        - lambda: |-
            for (auto *f : {id(fan1), id(fan2), id(fan3), id(fan4)}) {
              auto call = f->make_call(); call.set_state(true); call.set_speed(100); call.perform();
            }
  - platform: sim_bus
    channel: usr3
    name: "Button USR3"
    id: btn_usr3
    on_click: {min_length: 50ms, max_length: 1000ms, then: [{script.execute: {id: step_fans, delta: -10}}]}
light:
  - {platform: sim_bus, id: status_leds, channel: leds, num_leds: 5, name: "Status LEDs", internal: true}
  - platform: partition
    name: "Board Status"
    id: status_led
    internal: true
    segments: [{id: status_leds, from: 0, to: 0}]
    effects: [{pulse: }, {addressable_rainbow: }]
  - {platform: partition, name: "Fan N Status", id: fanN_led, internal: true,
     segments: [{id: status_leds, from: N, to: N}]}      # N = 1..4
```
Notes: the experiment used the channel names `env.temperature`, `env.humidity`, `button.usrN`; this section uses the shorter names `temperature`, `humidity`, `usr1..3` so the bus channel equals the `sil set` / `sil press` key (D-09, IO-06 style). Both are valid per the channel regex; pick one in the plan and keep it in Go constants, YAML, vectors and docs. The file was verified in its expanded (non-flow) YAML form; the flow-style shorthand above is for brevity, write it in the repo's block style. The experiment YAML did not set `segment_labels`; with it the status would say `board` instead of `status`.

### Atomic `set` (Go sketch)
```go
// supervisor.Set: validate everything first, apply with one engine op, then record inputs
func (s *supervisor) Set(ctx context.Context, kv map[string]string) (SetResult, error) {
    ops, err := parseSet(kv)           // unknown_key / value_out_of_range / unknown_fan (fanlib.Load), no side effects
    if err != nil { return SetResult{}, err }
    if err := s.engine().Apply(ctx, ops); err != nil { return SetResult{}, err } // one inbox event = atomic
    s.recordInputs(ops)                // under s.mu; seeds the next engine on sim restart
    return SetResult{Applied: echo(ops)}, nil
}
```

### Streaming handler skeleton
```go
mux.HandleFunc("GET /api/v1/watch", func(w http.ResponseWriter, r *http.Request) {
    fl, ok := w.(http.Flusher); if !ok { reply(w, nil, apperr.Infra("internal", "no flusher", nil)); return }
    sub := c.Subscribe(parseKinds(r))              // subscribe BEFORE the snapshot
    defer sub.Close()
    snap, seq := c.SnapshotWithSeq(r.Context())
    w.Header().Set("Content-Type", "application/x-ndjson"); writeLine(w, snapEvent(snap, seq)); fl.Flush()
    for { select {
        case ev, ok := <-sub.C(): if !ok { return }; if ev.Seq <= seq { continue }; writeLine(w, ev); fl.Flush()
        case <-r.Context().Done(): return } }
})
```

### Test sketch (pytest, TEST-01 chain)
```python
def test_loop(sil, api):                      # sil = CLI wrapper (subprocess, JSON); api = aioesphomeapi helper
    w = sil.watch()                           # first line is the snapshot
    assert w.first()["k"] == "snapshot"
    api.set_fan(1, 40)                        # HA-style command
    sil.wait(lambda s: s["ports"][0]["duty"] == 0.4)
    sil.wait(lambda s: abs(s["ports"][0]["rpm"] - 789.5) < 20, timeout=15)   # target at duty 0.4 for the shipped fan
    sil.press("usr1")                         # blocks until release
    sil.wait(lambda s: s["ports"][0]["duty"] == 0.5 and s["ports"][1]["duty"] == 0.1)
    sil.wait(lambda s: green(s["leds"]["strip"][1]), timeout=6)               # fan1 LED
    sil.set(temperature="24.5"); assert api.wait_state("Temperature", 24.5)
```
Target RPM at duty 0.4 with `min_duty 0.05`, `min_rpm 200`, `max_rpm 1800`: `200 + (0.4-0.05)/0.95*1600 = 789.5` (formula from `internal/model/fan.go` `Step`, values from `fans/arctic-p12-pwm-pst.toml`).

## In-repo facts relied on (read this session)

- Phase 1 channels, verbatim from `protocol/PROTOCOL.md`: `` | `portN.duty` (N = 1..4) | `out` (firmware to simulator) | PWM duty of fan port N, 0..1 | `` and `` | `portN.rpm` (N = 1..4) | `in` (simulator to firmware) | tach speed of fan port N in RPM | ``. `Proto = 1` at `internal/bus/codec.go:13`; `constexpr int PROTO = 1;` at `components/sim_bus/sim_protocol.h:19`.
- `internal/bus/server.go:36`: `type Channel struct{ Ch, Dir, Type string }` (add `N int64`, `Seg string`); the `chan` case at `server.go` ~L296 reads `ch`, `dir`, `type` only.
- `internal/engine/engine.go:306` `func (e *Engine) checkManifest(chs []bus.Channel)` builds `want` from the ports only and warns `"unknown channel %s (ignored)"` (L332, L353) for anything else.
- `internal/control/control.go:41-51`: `Status{Schema, Instance, Phase, API, Firmware, Sim, Ports, Warnings, Error}`; the comment says `Fields are extended by later phases, never renamed.` `PortStatus{Port, Fan, Duty, RPM, State}` at L89-95.
- `internal/instance/instance.go:145-148`: `FirmwareLog()` = `filepath.Join(p.RunDir, "firmware.log")`, `SilLog()` = `…"sil.log"`, `BuildLog()` = `…"build.log"`; `Spec{Schema, Instance, Program, Fans []string}` at L406-412.
- `internal/lifecycle/lifecycle.go:30`: `defaultConfig = "examples/fan-skeleton.yaml"`; L194: `Fans: []string{fan, fan, fan, fan},`; `internal/fanlib/fanlib.go:30`: `const DefaultFan = "arctic-p12-pwm-pst"`, `IDs()` at L106, `EnvFanDir = "SIL_FAN_DIR"` L34 (replaces the embedded library entirely, it is not a union).
- `fans/arctic-p12-pwm-pst.toml`: `min_duty   = 0.05`, `start_duty = 0.10`, `below_min  = "stop"`, `min_rpm    = 200.0`, `max_rpm    = 1800.0`, `tau_up     = 1.0`, `tau_down   = 2.5`.
- `internal/fwbuild/lint.go` banned hint: `case "esp32_rmt_led_strip": return "use light platform sim_bus (Phase 2)", true`.
- Product contract names, `wifi-fancontroller/hil/hil/entities.py`: `FAN = {n: "Fan %d" % n …}`, `FAN_SPEED = {n: "Fan %d Speed" % n …}`, `TEMPERATURE = "Temperature"`, `HUMIDITY = "Humidity"`, `BUTTON_USR = {n: "Button USR%d" % n for n in (1, 2, 3)}`.
- Product LED/buttons (`hardware-rev-3.1.yaml`): five-LED strip with `partition` ids `status_led`, `fan1_led`..`fan4_led`; boot `set_effect("pulse")` blue 0.5; wifi connect green 0.5; buttons `GPIO38/37/36` inverted with names `Button USR1..3`; `hdc1080 … update_interval: 10s`.
- `.planning/config.json` has `workflow.nyquist_validation: false`, so no Validation Architecture section is emitted; `security_enforcement: true`, ASVS level 1.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `wifi.on_connect` for "connected" LED | `api: on_client_connected` with `client_info`/`client_address` lambdas | ESPHome 2026.10.1 [VERIFIED: `components/api/__init__.py`, `[(cg.std_string, "client_info"), (cg.std_string, "client_address")]`] | host builds have no Wi-Fi; filter sil's own probe. |
| PlatformIO-based host build | generated `build.ninja` + machine gcc | 2026.10 | irrelevant here except it keeps builds fast (8 s compile). |
| `mean` sensor semantics everywhere | per-sensor `mode` | this phase | env values are exact. |

**Deprecated/outdated:** `esphome: platformio_options: build_flags` (use `esphome: build_flags`, already done). `light.migrate_channel_colors` is irrelevant (the sim light has no channel order).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | HDC1080-like bounds for `sil set`: temperature −40..125 °C, humidity 0..100 % | Pattern 4 | Too tight/loose validation only; no firmware impact. |
| A2 | A second shipped fan (Noctua NF-A12x25 PWM) would use `max_rpm = 2000`, `min_rpm ≈ 460` at 20 % PWM (Noctua spec page via search: "2000 rpm", "460 rpm at 20 % PWM"; retail listings 450 ± 20 %); `below_min`, `start_duty`, `tau_*` would be unsourced | Open Questions | Wrong plant constants; only matters if shipped (recommendation: ship it flagged ASSUMED like the Arctic constants, or use a test fixture). |
| A3 | HA derives the config entry and entity unique ids from the device MAC plus entity static info (key/object id) | Pitfall 9 / HA-01 | If HA also uses the name, renames orphan entities; mitigated by stable names. Source: HA `esphome/entity.py` call sites `build_device_unique_id(mac, info)` (the function itself lives in aioesphomeapi, formula not read). |
| A4 | Other `partition`-driven effects (`addressable_rainbow`, `strobe`, ...) behave like `pulse` on the sim light | Pattern 2 | Only `pulse` and plain `turn_on` colours were exercised. Effects write the buffer through `ESPColorView`, so dedupe and the gap apply identically, but not run. |
| A5 | Go `http.Flusher` streaming works over the unix-socket `http.Server` with no timeouts and the `DisableKeepAlives` client | Pattern 6, Pitfall 8 | Standard library behaviour, not run in this repo; a 20-line spike test in the first plan removes the doubt. |
| A6 | The full Phase 1 e2e suite passes with `rev33.yaml` as the default config | Pitfall 14 | Fallback documented (keep the old default). |
| A7 | HA sends a `client_info` other than `"sil"` (any string works for the filter) | E9 | Practically certain; aioesphomeapi's default and a custom value were tested. |
| A8 | Default-route interface detection by UDP `connect` works on the user's hosts without internet | Pitfall 9 | `lan_host: null` then; the user reads the IP themselves. |
| A9 | `sil press` hold timing jitter (a few ms on a loaded host) never moves a 200 ms / 1500 ms press across the 50/1000 ms firmware thresholds | Pattern 3 | Margins are 150 ms and 500 ms; low risk. |

## Open Questions (RESOLVED)

All eight questions are resolved; the last one (8) was decided at planning time.

1. **RESOLVED: How are segment names carried (D-06)?**
   - Resolution: derive `label:from:to` from the stock `partition` lights at codegen (ranges from the YAML), optional `segment_labels` override on the sim light for naming (`status_led` → `board`), delivered in the `chan` frame's `seg` field. No simulator-side config.
2. **RESOLVED: Does the protocol version change?**
   - Resolution: no. Additive channel type `rgb` + `n`/`seg`/`px` fields; ports required, other channels optional. Vectors added so both codecs pin it.
3. **RESOLVED: Do `sil set` values survive restarts (discretion)?**
   - Resolution: env values and fan-per-port are supervisor-held and seed every new engine (`sim restart`); `fw restart` never touches the engine. Buttons are not held state (released on sim restart).
4. **RESOLVED: How is the fan chosen at `sil up` and listed?**
   - Resolution: `sil up --fan NAME` (all ports, default `arctic-p12-pwm-pst`), per-port later through `sil set portN.fan=NAME`; `sil fans` lists `fanlib.IDs()` with name/min/max RPM; unknown names are `unknown_fan` (exit 1) before anything is written.
5. **RESOLVED: Example file naming and relation to Phase 1's.**
   - Resolution: `examples/rev33.yaml` added, `examples/fan-skeleton.yaml` kept as the minimal regression config; `defaultConfig` switches to `rev33.yaml` in the last plan behind the full e2e (fallback in Pitfall 14).
6. **RESOLVED: `watch` filters and exit behaviour; `logs` format.**
   - Resolution: `--kind k1,k2` filter; ends with exit 0 when the instance stops or SIGINT; `logs` returns `{"source","lines":[...]}` by default, `--follow` NDJSON `{"src","line"}`, `--raw` plain text, ANSI stripped by default, `--tail` default 100, sources `firmware` (firmware.log) and `sim` (sil.log).
7. **RESOLVED: What about the live Home Assistant?**
   - Resolution: not available in this run. Automated proxy = aioesphomeapi (device info, MAC, entity keys, fan round trip, identity across `down`/`up`, second instance distinct); the rest is the manual UAT checklist below. HA-side item to confirm: HA on another machine needs the `lan_host`, not `127.0.0.1`.
8. **RESOLVED: Ship the Noctua fan now?**
   - What we know: partial spec (A2), `below_min = "hold"` semantics are Noctua-specific and unsourced; `SIL_FAN_DIR` replaces the library so a test-only fixture needs both files.
   - Recommendation: ship `fans/noctua-nf-a12x25-pwm.toml` with `source` text marking `start_duty`, `tau_*` and `below_min` ASSUMED (same convention as the Arctic file) so MODEL-03 swaps have two visibly different fans (1800 vs 2000 max RPM); if the planner prefers no new unsourced constants, use a `tests/fixtures/fans/` copy of the Arctic file with `max_rpm = 1200` and run the swap test with `SIL_FAN_DIR` set for that instance only.
   - Resolution (planner, 2026-11-09, unattended): take the recommendation. Plan 02-05 ships `fans/noctua-nf-a12x25-pwm.toml` (max_rpm 2000, min_rpm 450 at min_duty 0.20, below_min hold; start_duty, below_min, tau_up, tau_down labelled ASSUMED) and records it as flagged assumption A2 for maintainer review. No test-only fixture library.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | build, unit tests | yes | 1.27.3 (`~/.local/bin/go`) | — |
| Docker + pinned image | `sil up`, e2e | yes | 29.9.3; `ghcr.io/esphome/esphome:2026.10.1` present | — |
| `uv` + Python 3.12 | `tests/` env | yes | uv 0.10.2, cpython 3.12.12/3.13.12 (uv-managed), system 3.11.2 | — |
| g++ / make | `make test-cpp`, Makefile | yes | system | `make test-cpp-image` |
| jq | manual inspection | yes | — | — |
| socat | manual bus inspection | no | — | not needed; Python peer script used |
| Home Assistant | HA-01/02 live check | no | — | manual UAT checklist (D-15) |
| Network (PyPI) for `uv lock` | first test env creation | yes (worked) | — | committed `uv.lock` + wheel cache |
| PlatformIO toolcache (offline builds) | `sil up` | yes (seeded from the e2e cache) | — | first build fetches libraries once (Phase 1 behaviour) |

**Missing dependencies with no fallback:** none for automated work.
**Missing dependencies with fallback:** Home Assistant (manual UAT), socat (not needed).

## Security Domain

`security_enforcement: true`, ASVS level 1, block on high.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (unchanged) | Native API: Noise PSK from Phase 1 (do not hand-roll); control API: unix socket 0600 in a 0700 dir |
| V3 Session Management | no | — |
| V4 Access Control | yes | New mutating endpoints (`set`, `press`) and `logs`/`watch` only on `<run>/api.sock`; keep the `checkPrivateDir` gate in the CLI client; no TCP listener in this phase |
| V5 Input Validation | yes | `set` keys against a fixed allow-list (`temperature`, `humidity`, `portN.fan` for configured ports); finite numbers only, range checks; fan ids validated with `fanlib.Load` (the id pattern is already restricted by the library file name regexp); `button` id from the manifest set; `hold_ms` capped; `source` enum for logs; request bodies limited (`io.LimitReader`, `DisallowUnknownFields`, as in `sim/restart`) |
| V6 Cryptography | no new | Noise via existing `flynn/noise`; keys are never logged |
| V7 Error Handling and Logging | yes | `logs` serves only `firmware.log` and `sil.log` (no `build.log`, which can carry the key unless redacted); verify the API key never appears in either file (grep of this session's runs: 0 occurrences) |
| V12 Files and Resources | yes | log paths come from `instance.Paths`, never from request input |
| V13 API | yes | stream endpoints bounded: max subscribers per instance, bounded buffers, subscriber dropped with a `lagged` marker instead of unbounded memory |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via log source or fan name | Tampering / Info disclosure | enums / library lookup only, never join request strings into paths |
| Slow or stuck watch client exhausting memory | DoS | per-subscriber bounded channel, drop + `lagged`, close on shutdown |
| NaN/Inf/huge numbers into the model or the bus | Tampering | reject non-finite and out-of-range before they reach the engine (`Frame.Num` also drops non-finite) |
| API key leak through status/logs | Info disclosure | key only in `status` (socket 0600) and `sil up` output as in Phase 1; never in logs or events |
| Native API reachable on the LAN (`0.0.0.0`) | Spoofing / Tampering | Noise PSK required; documented; firewall for CI runners (Phase 1 note) |
| Held-button / unbounded hold abuse | DoS | `hold_ms` cap; busy-button rejection (D-04) |

## Manual UAT checklist for end-of-phase verification (HA-01, HA-02, D-15)

Prepared here so the verifier can lift it into `02-UAT.md`:

1. `make build && bin/sil up ha-a` (default config). Note `api.lan_host`, `api.port`, `api.key` from `bin/sil status ha-a`.
2. In HA: Settings → Devices & Services → Add Integration → ESPHome → host = `lan_host`, port = port, enter the encryption key. Expect: device "SiL ha-a" with 13 entities (Fan 1-4, Fan 1-4 Speed, Temperature, Humidity, Button USR1-3) and no light entities.
3. Set Fan 1 to 40 % in HA → `sil status` shows `ports[0].duty 0.4`; RPM entity rises to about 790.
4. `sil set temperature=27 humidity=60` → HA entities follow within a second. `sil press usr1` → Fan 1 speed goes to 50 %; `sil press usr1 --down` / `--up` → `Button USR1` toggles on/off in HA.
5. `sil down ha-a && sil up ha-a` → HA shows the same device (no "SiL ha-a (2)", no duplicate entities) and reconnects by itself.
6. `sil up ha-b` → second device "SiL ha-b" appears separately (different port, MAC), and device ha-a stays on its own port.
7. Cleanup: delete both devices in HA (Device → Delete), `sil down ha-a`, `sil down ha-b`.

## Suggested plan decomposition (for the planner, not binding)

1. **Wire + firmware platforms:** `sim_bus` binary_sensor, light, sensor `mode`; hub manifest hooks and px replay; `check_unique_channels`; protocol doc + vectors (Go and C++ tests); `make check`, `make test-cpp-image`.
2. **Engine World:** optional channels, buttons/env/LED parsing from the manifest, `rgb` px handling, ops (`SetFan`, `Button`, `Env`), 10 s env re-send, snapshot additions, `Emit`; unit tests with the manual clock.
3. **Supervisor + API + CLI:** inputs, event broker, status additions, `set/press/watch/logs/fans`, streaming client, error codes in the registry, `--instance`, `sil up --fan`, `lan_host`; unit tests; extend an e2e (`TestRev33*`).
4. **Example + docs:** `examples/rev33.yaml`, lint hint text, README (status, HA steps, delete procedure, host/port/key), default switch (last).
5. **tests/ project:** `pyproject.toml`, `uv.lock` (with the hash-diff gate task), conftest, TEST-01, TEST-02, HA-01 proxy test (identity across `down`/`up`, second instance), `make test-contract`.
6. **Acceptance:** `make check`, `make test-cpp-image`, full `make e2e`, `make test-contract` (< 60 s warm), UAT checklist file.

## Sources

### Primary (HIGH confidence)
- Executed experiments on this host (section Experiment Results): builds in `ghcr.io/esphome/esphome:2026.10.1`, runtime traces of the firmware with a Python bus peer and `aioesphomeapi 46.12.0`.
- ESPHome 2026.10.1 source inside the pinned image: `components/partition/{light.py,light_partition.h}`, `components/light/{addressable_light.h,addressable_light.cpp,esp_color_correction.h,base_light_effects.h,effects.py}`, `components/binary_sensor/binary_sensor.cpp`, `core/entity_base.h` (`StatefulEntityBase::set_new_state`), `components/api/__init__.py` (`on_client_connected` arguments).
- Phase 1 sources read this session: `internal/{engine,bus,control,cli,supervisor,lifecycle,instance,fanlib,model,apperr,lint,apiclient}`, `components/sim_bus/*`, `protocol/*`, `examples/fan-skeleton.yaml`, `e2e/harness_test.go`, `Makefile`.
- Product repo: `../wifi-fancontroller/hardware-rev-3.1.yaml`, `hardware-rev-3.3.yaml`, `modules/usr_buttons_mode.yaml`, `hil/hil/entities.py`, `hil/pyproject.toml`, `hil/uv.lock`.
- `gsd-tools query package-legitimacy check` (pypi) and the lockfile hash comparison.

### Secondary (MEDIUM confidence)
- Home Assistant `homeassistant/components/esphome/entity.py` (dev branch): `build_device_unique_id(mac, info)` call sites. https://raw.githubusercontent.com/home-assistant/core/dev/homeassistant/components/esphome/entity.py
- Noctua NF-A12x25 PWM specification pages (via web search summary; official page: noctua.at/en/nf-a12x25-pwm/specification): 2000 rpm max, 460 rpm at 20 % PWM. Not fetched directly.
- `.planning/research/PITFALLS.md` (Pitfalls 3, 5, 9, 18) for HA identity background (source-read, no live HA).

### Tertiary (LOW confidence)
- HDC1080 operating ranges (training knowledge, A1); Go `Flusher` over unix sockets (A5).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH, no new Go dependencies; test stack lock hash-identical to the product's.
- Architecture: HIGH for firmware/wire/LED/button/env (executed), MEDIUM for engine/supervisor/API/CLI design (follows the read Phase 1 code, not executed).
- Pitfalls: HIGH for 1-5, 9, 11-13 (observed or measured), MEDIUM for 6-8, 10, 14, 15 (derived from code reading).

**Research date:** 2026-11-09
**Valid until:** 2026-12-09 (ESPHome is pinned; stable). Re-check only if the ESPHome pin or `aioesphomeapi` lock changes.

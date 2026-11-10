# Phase 1: Closed-Loop Fan Skeleton - Context

**Gathered:** 2026-11-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Walking skeleton through every risky layer: `sil up <name>` builds static `host:` firmware in the pinned ESPHome 2026.10.1 image, runs it directly on the host next to a per-instance `sil` process (supervisor + simulator engine). Fan duty from the firmware (4 ports, `sim_bus` float output) drives the linear first-order-lag fan model behind an injectable clock; RPM (1 s mean, `sim_bus` sensor) flows back. Observable via `sil status` on the HTTP/JSON API, re-syncs after a restart of either side, isolated per instance, stopped cleanly with `sil down`, and misconfigurations fail loudly.

Requirements: BUILD-01..07, BUS-01..04, IO-01, IO-02, IO-06, MODEL-01, MODEL-02, MODEL-04, CLI-05, CLI-06.

Not in this phase: buttons, LED strip, temp/humidity (IO-03..05), choosing a fan per port (MODEL-03), the full Rev 3.3 example and HIL entity names (FW-01..03), `sil set/press/watch/logs` (CLI-01..04), HA verification, contract tests (Phase 2), web UI (Phase 3).

</domain>

<decisions>
## Implementation Decisions

### Run lifecycle
- **D-01:** `sil up <name>` **detaches**. It builds (or reuses the build), starts a background per-instance `sil` process, waits for a real encrypted API handshake, prints result JSON (instance, API host:port, key, run dir) on stdout and exits 0. Later commands (`status`, `down`, `sim restart`, `fw restart`) find the instance through `run.json` in its run dir.
- **D-02:** The per-instance process is a **thin supervisor plus a restartable simulator engine**. The supervisor owns the firmware process (process group + `Pdeathsig`) and the bus listener lifecycle. The simulator engine (model, bus server side, state) can be restarted on its own via `sil sim restart`, and the firmware stays up and reconnects (BUS-02 resync). Killing the supervisor kills everything, so no orphaned firmware is left. This is how success criterion 3 ("restart the simulator, firmware keeps its API responsive") and criterion 4 ("killing the simulator leaves no orphan") hold together. The planner decides how the engine is separated (goroutine with a fresh state vs a child process), as long as a sim restart really drops engine and bus state.
- **D-03:** When the firmware exits unexpectedly, the supervisor **auto-restarts it with the same prefs** (behaves like a real device reboot). It records restart count and last reason (`rebooted` exit 0 / `crashed` + exit code or signal) in status. A crash-loop guard (about 5 restarts in 30 s) stops restarting and marks the instance failed. Exits requested by `sil down` / `sil fw restart` are not counted as failures.
- **D-04:** `sil up` output: human progress lines on **stderr** (build step, cache hit, waiting for handshake), the final result JSON on **stdout**; `--quiet` suppresses stderr. The full ESPHome build log goes to the run dir, and its tail is shown on failure.
- **D-05:** `sil fw restart` ships in Phase 1 (restart only, same binary, same prefs; it is needed for the restart matrix). No in-place rebuild: RUN-02 `sil rebuild` stays in v2.

### Instance identity
- **D-06:** **XDG split.** Persistent identity (API key, sticky port, metadata) lives in `$XDG_STATE_HOME/sil/<name>/` (default `~/.local/state/sil/<name>/`). The ephemeral run dir (bus socket, `ESPHOME_PREFDIR`, logs, `run.json`) lives in `$XDG_RUNTIME_DIR/sil/<name>/`, falling back to `/tmp/sil-$UID/<name>/`. The run dir is recreated on every `sil up`, so preferences start fresh (BUILD-04). Neither depends on the cwd. — **Reversibility:** costly — consumer scripts and the product repo's pytest will locate instances by these paths.
- **D-07:** **API port: auto, then sticky.** The first `sil up <name>` picks a free port starting at **6054** (6053 is the HIL supervisor port and stays free) and stores it in the identity. Later runs reuse it, so the HA entry keeps working. `--api-port` overrides and updates the stored port. If the sticky port is taken: exit 2 with a JSON error naming the port and its holder if known; never silently move.
- **D-08:** **MAC is derived, the key is stored.** MAC = `02:` + 5 bytes from a hash of the instance name (locally administered, reproducible on any machine). The API encryption key is 32 random bytes (base64) generated on the first `sil up` and stored in the identity dir with mode 0600. Deleting the identity dir (a later `sil down --forget` is fine, not required in Phase 1) resets it.
- **D-09:** Firmware naming: `esphome.name: sil-<name>`, `friendly_name: SiL <name>`, injected by `sil` (substitutions / `-s` at build time). The prefix keeps a sim away from a real `fancontroller-*` in a production HA. Instance names must be valid ESPHome names (validate early, fail loudly).

### Fan model & shipped fan
- **D-10:** The first shipped fan is the **Arctic P12 PWM PST** (`fans/arctic-p12-pwm-pst.toml`), with parameters from the datasheet (about 200-1800 RPM, 0 RPM mode at low duty). The researcher verifies the actual figures and records the source in the file. All 4 ports use this fan in Phase 1 (per-port choice is MODEL-03, Phase 2).
- **D-11:** Behaviour below `min_duty` is a **per-fan parameter**: `below_min = "stop"` (target 0 RPM) or `"hold"` (target stays `min_rpm`). The Arctic uses `stop`.
- **D-12:** **Hysteresis semantics (confirmed):** a stopped fan starts only when duty ≥ `start_duty`; a running fan keeps spinning down to `min_duty` (`min_duty` < `start_duty`) and stalls below it. Between the two thresholds the current state is kept. When running, the target is linear from `min_rpm` at `min_duty` to `max_rpm` at duty 1.0.
- **D-13:** **Asymmetric lag:** `tau_up` (spin-up) and `tau_down` (coast-down), both using the exact exponential update `rpm += (target - rpm) * (1 - exp(-dt/tau))`. This extends MODEL-01's single lag; `fan.toml` is internal, so the schema can still change.
- **D-14:** Indicative `fan.toml` field set (the planner may rename, but must keep it strict via `DisallowUnknownFields` and validate ranges/ordering): `name`, `source` (datasheet URL/notes), `min_duty`, `start_duty`, `below_min`, `min_rpm`, `max_rpm`, `tau_up`, `tau_down`. Invalid combinations (e.g. `start_duty < min_duty`, `min_rpm > max_rpm`) are rejected with a located error.

### Build experience
- **D-15:** **Skip the build when nothing changed.** `sil` hashes the resolved config inputs (YAML, injected substitutions, component sources, ESPHome image digest). On a match it reuses the binary, otherwise it runs the incremental ESPHome build. `--rebuild` forces a build.
- **D-16:** Build output and cache go to **`$XDG_CACHE_HOME/sil/build/<name>/`** (ESPHome build dir plus container `HOME`), one per instance because name, MAC, port and key are compiled in. The working tree stays clean. Wiping the cache does not touch identity.
- **D-17:** **Firmware config:** `sil up <name>` builds the repo's Phase 1 example by default (4 fan ports: `sim_bus` output + `fan: speed` + RPM sensor; `api` with encryption and `reboot_timeout: 0s`; `-static`). `--config path.yaml` builds any other host YAML. Identity is always injected by `sil`, never hand-written in the YAML. The planner picks the example file name/location (research suggests `examples/`).
- **D-18:** **Banned-component lint runs after `esphome config`** in the pinned image. The Go side parses the resolved YAML, so packages, `!include` and remote sources are covered, and rejects the BUILD-03 list (`wifi`, `ledc`, `pulse_counter`, `esp32_rmt_led_strip`, `i2c`, `ota`, `http_request`, `update`, `captive_portal`, `web_server`, `sntp`, `esp32`) plus anything that auto-loads `json`. It also asserts `api.reboot_timeout: 0s`, the `-static` build flag and `api.encryption`. Each failure names the component and the file. Codegen-level checks (missing `channel:`, socket path > 107 bytes) stay in the component Python.

### Claude's Discretion
- How the supervisor and engine are separated (D-02), the JSON shapes for `run.json`, the `sil up` result and errors, the Go package layout, and how tests drive the restart matrix. These follow ARCHITECTURE.md / STACK.md unless there is a reason not to.
- Encrypted API handshake for the ready check (STATE blocker): pick the approach during research/planning (e.g. a Go Noise client vs a minimal probe). It must be a real handshake, not a TCP connect.
- How much of `prototype/` is reused vs rewritten when moving to `components/sim_bus/`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope and requirements
- `.planning/PROJECT.md` — core value, constraints (Go, locality, pinned ESPHome, GPL-3.0), key decisions
- `.planning/REQUIREMENTS.md` — BUILD/BUS/IO/MODEL/CLI requirement text for this phase
- `.planning/ROADMAP.md` §Phase 1 — goal and the 5 success criteria (acceptance gate)
- `.planning/STATE.md` §Blockers/Concerns — open Phase 1 spikes (static build with `api: encryption:`, Noise handshake, same-path bind mounts, rebuild timing; Go not installed)

### Research (stack, architecture, pitfalls)
- `.planning/research/SUMMARY.md` — consolidated recommendations and resolved maintainer decisions
- `.planning/research/STACK.md` — versions, Go libraries, ESPHome host build facts, sim-bus protocol decision (also mirrored in `.claude/CLAUDE.md`)
- `.planning/research/ARCHITECTURE.md` — process model, engine actor, Control interface, run.json discovery, repo layout, resync protocol
- `.planning/research/PITFALLS.md` — pitfalls 1-6, 11, 20 apply directly (static link, config ≠ working, reboot_timeout, HA identity, prefs leakage, wall-clock, resync, supervision)
- `.planning/research/FEATURES.md` — table-stakes behaviour for `up`/`down`/status, isolation, exit codes

### Design origin
- `docs/initial-idea.md` — original design notes (note the `wakeable_delay` correction in STATE.md)

### Prototype
- `prototype/README.md` — what the spike proved and its known gaps (the Phase 1 to-do list for the components)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `prototype/components/sim_bus/` (`__init__.py`, `sim_bus.{h,cpp}`, `output.py`, `sensor.py`, `binary_sensor.py`, `light.py`): working hub (non-blocking AF_UNIX client, flat NDJSON) and platforms verified against 2026.10.1. This is the starting point for `components/sim_bus/` at the repo root; output and sensor are what Phase 1 needs.
- `prototype/sim.yaml`: minimal host config showing `-static`, `external_components` local path and `fan: speed` on `sim_bus` output, the base for the Phase 1 example.
- `prototype/tools/fake_sim.py`, `api_client.py`: reference peer and aioesphomeapi client; useful as a manual check, not a deliverable (no Python in the simulator).

### Established Patterns
- No Go code yet; Go 1.27.3 is not installed (install first, checksum in STACK.md).
- Known prototype gaps to close in Phase 1: HELLO/manifest/snapshot, `PollingComponent` + 1 s RPM mean, string escaping, socket path validation, per-instance path (`SIL_BUS` env override), `setup_priority::BUS`, `reboot_timeout: 0s`, encryption, MAC. Channel names move from `fan1_pwm` to the `port1.duty` style (IO-06).

### Integration Points
- `components/` at the repo root so `github://zeroflow/esphome-sil@vX.Y` works later (FW-03, Phase 2).
- Shared golden vectors (e.g. `protocol/vectors/`) exercised by both Go and C++ tests (BUS-04).

</code_context>

<specifics>
## Specific Ideas

- Port 6053 is reserved for the HIL supervisor; sims start at 6054.
- The `sil-` name prefix exists so a sim never collides with a real fan controller in the user's production HA.
- The Arctic P12 PWM PST was picked because it stops at low duty, which exercises the start-threshold/stall path that `stall_guard` will need later.

</specifics>

<deferred>
## Deferred Ideas

- `sil fw restart --rebuild` / `sil rebuild` in place: RUN-02 (v2).
- `sil down --forget` to wipe an instance's identity: nice-to-have, not required in Phase 1.
- A second shipped fan with `below_min = "hold"` (e.g. Noctua NF-A12x25): Phase 2 alongside MODEL-03, or with fan-analyze (P4).

</deferred>

---

*Phase: 01-closed-loop-fan-skeleton*
*Context gathered: 2026-11-08*

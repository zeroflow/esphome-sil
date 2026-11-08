# Phase 1: Closed-Loop Fan Skeleton - Research

**Researched:** 2026-11-08
**Domain:** Go supervisor/simulator + ESPHome 2026.10.1 `host:` static firmware + Unix-socket NDJSON sim-bus + Noise-encrypted ESPHome native API
**Confidence:** HIGH for the build/run/handshake foundation (every open Phase 1 spike was reproduced on this host); MEDIUM for Go internals, protocol details and fan constants (designed here, not yet built)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Run lifecycle**
- **D-01:** `sil up <name>` **detaches**. It builds (or reuses the build), starts a background per-instance `sil` process, waits for a real encrypted API handshake, prints result JSON (instance, API host:port, key, run dir) on stdout and exits 0. Later commands (`status`, `down`, `sim restart`, `fw restart`) find the instance through `run.json` in its run dir.
- **D-02:** The per-instance process is a **thin supervisor plus a restartable simulator engine**. The supervisor owns the firmware process (process group + `Pdeathsig`) and the bus listener lifecycle. The simulator engine (model, bus server side, state) can be restarted on its own via `sil sim restart`, and the firmware stays up and reconnects (BUS-02 resync). Killing the supervisor kills everything, so no orphaned firmware is left. This is how success criterion 3 ("restart the simulator, firmware keeps its API responsive") and criterion 4 ("killing the simulator leaves no orphan") hold together. The planner decides how the engine is separated (goroutine with a fresh state vs a child process), as long as a sim restart really drops engine and bus state.
- **D-03:** When the firmware exits unexpectedly, the supervisor **auto-restarts it with the same prefs** (behaves like a real device reboot). It records restart count and last reason (`rebooted` exit 0 / `crashed` + exit code or signal) in status. A crash-loop guard (about 5 restarts in 30 s) stops restarting and marks the instance failed. Exits requested by `sil down` / `sil fw restart` are not counted as failures.
- **D-04:** `sil up` output: human progress lines on **stderr** (build step, cache hit, waiting for handshake), the final result JSON on **stdout**; `--quiet` suppresses stderr. The full ESPHome build log goes to the run dir, and its tail is shown on failure.
- **D-05:** `sil fw restart` ships in Phase 1 (restart only, same binary, same prefs; it is needed for the restart matrix). No in-place rebuild: RUN-02 `sil rebuild` stays in v2.

**Instance identity**
- **D-06:** **XDG split.** Persistent identity (API key, sticky port, metadata) lives in `$XDG_STATE_HOME/sil/<name>/` (default `~/.local/state/sil/<name>/`). The ephemeral run dir (bus socket, `ESPHOME_PREFDIR`, logs, `run.json`) lives in `$XDG_RUNTIME_DIR/sil/<name>/`, falling back to `/tmp/sil-$UID/<name>/`. The run dir is recreated on every `sil up`, so preferences start fresh (BUILD-04). Neither depends on the cwd. — **Reversibility:** costly — consumer scripts and the product repo's pytest will locate instances by these paths.
- **D-07:** **API port: auto, then sticky.** The first `sil up <name>` picks a free port starting at **6054** (6053 is the HIL supervisor port and stays free) and stores it in the identity. Later runs reuse it, so the HA entry keeps working. `--api-port` overrides and updates the stored port. If the sticky port is taken: exit 2 with a JSON error naming the port and its holder if known; never silently move.
- **D-08:** **MAC is derived, the key is stored.** MAC = `02:` + 5 bytes from a hash of the instance name (locally administered, reproducible on any machine). The API encryption key is 32 random bytes (base64) generated on the first `sil up` and stored in the identity dir with mode 0600. Deleting the identity dir (a later `sil down --forget` is fine, not required in Phase 1) resets it.
- **D-09:** Firmware naming: `esphome.name: sil-<name>`, `friendly_name: SiL <name>`, injected by `sil` (substitutions / `-s` at build time). The prefix keeps a sim away from a real `fancontroller-*` in a production HA. Instance names must be valid ESPHome names (validate early, fail loudly).

**Fan model & shipped fan**
- **D-10:** The first shipped fan is the **Arctic P12 PWM PST** (`fans/arctic-p12-pwm-pst.toml`), with parameters from the datasheet (about 200-1800 RPM, 0 RPM mode at low duty). The researcher verifies the actual figures and records the source in the file. All 4 ports use this fan in Phase 1 (per-port choice is MODEL-03, Phase 2).
- **D-11:** Behaviour below `min_duty` is a **per-fan parameter**: `below_min = "stop"` (target 0 RPM) or `"hold"` (target stays `min_rpm`). The Arctic uses `stop`.
- **D-12:** **Hysteresis semantics (confirmed):** a stopped fan starts only when duty >= `start_duty`; a running fan keeps spinning down to `min_duty` (`min_duty` < `start_duty`) and stalls below it. Between the two thresholds the current state is kept. When running, the target is linear from `min_rpm` at `min_duty` to `max_rpm` at duty 1.0.
- **D-13:** **Asymmetric lag:** `tau_up` (spin-up) and `tau_down` (coast-down), both using the exact exponential update `rpm += (target - rpm) * (1 - exp(-dt/tau))`. This extends MODEL-01's single lag; `fan.toml` is internal, so the schema can still change.
- **D-14:** Indicative `fan.toml` field set (the planner may rename, but must keep it strict via `DisallowUnknownFields` and validate ranges/ordering): `name`, `source` (datasheet URL/notes), `min_duty`, `start_duty`, `below_min`, `min_rpm`, `max_rpm`, `tau_up`, `tau_down`. Invalid combinations (e.g. `start_duty < min_duty`, `min_rpm > max_rpm`) are rejected with a located error.

**Build experience**
- **D-15:** **Skip the build when nothing changed.** `sil` hashes the resolved config inputs (YAML, injected substitutions, component sources, ESPHome image digest). On a match it reuses the binary, otherwise it runs the incremental ESPHome build. `--rebuild` forces a build.
- **D-16:** Build output and cache go to **`$XDG_CACHE_HOME/sil/build/<name>/`** (ESPHome build dir plus container `HOME`), one per instance because name, MAC, port and key are compiled in. The working tree stays clean. Wiping the cache does not touch identity.
- **D-17:** **Firmware config:** `sil up <name>` builds the repo's Phase 1 example by default (4 fan ports: `sim_bus` output + `fan: speed` + RPM sensor; `api` with encryption and `reboot_timeout: 0s`; `-static`). `--config path.yaml` builds any other host YAML. Identity is always injected by `sil`, never hand-written in the YAML. The planner picks the example file name/location (research suggests `examples/`).
- **D-18:** **Banned-component lint runs after `esphome config`** in the pinned image. The Go side parses the resolved YAML, so packages, `!include` and remote sources are covered, and rejects the BUILD-03 list (`wifi`, `ledc`, `pulse_counter`, `esp32_rmt_led_strip`, `i2c`, `ota`, `http_request`, `update`, `captive_portal`, `web_server`, `sntp`, `esp32`) plus anything that auto-loads `json`. It also asserts `api.reboot_timeout: 0s`, the `-static` build flag and `api.encryption`. Each failure names the component and the file. Codegen-level checks (missing `channel:`, socket path > 107 bytes) stay in the component Python.

### Claude's Discretion
- How the supervisor and engine are separated (D-02), the JSON shapes for `run.json`, the `sil up` result and errors, the Go package layout, and how tests drive the restart matrix. These follow ARCHITECTURE.md / STACK.md unless there is a reason not to.
- Encrypted API handshake for the ready check (STATE blocker): pick the approach during research/planning (e.g. a Go Noise client vs a minimal probe). It must be a real handshake, not a TCP connect.
- How much of `prototype/` is reused vs rewritten when moving to `components/sim_bus/`.

### Deferred Ideas (OUT OF SCOPE)
- `sil fw restart --rebuild` / `sil rebuild` in place: RUN-02 (v2).
- `sil down --forget` to wipe an instance's identity: nice-to-have, not required in Phase 1.
- A second shipped fan with `below_min = "hold"` (e.g. Noctua NF-A12x25): Phase 2 alongside MODEL-03, or with fan-analyze (P4).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BUILD-01 | Build example firmware from working tree with `sil up`, pinned image, non-root `--user`, own `HOME`, no root-owned files | Verified docker invocation (Pattern 1); `find -user root` returned nothing; repo mounted `:ro` |
| BUILD-02 | Static link, runs on Debian 12 host | `build_flags: [-static]` with API encryption verified: 3.4 MB static ELF, ran on host glibc 2.36; assert via `debug/elf` (no `PT_INTERP`) |
| BUILD-03 | Fail early on host-incompatible components | Two-stage lint (Pattern 3); matrix of what `esphome config` rejects vs silently passes (`wifi`, `ota`, `http_request`, `json` pass) |
| BUILD-04 | Per-run run dir with socket, `ESPHOME_PREFDIR`, logs; fresh prefs | Dir layout (Pattern 2); prefs verified writable path via env |
| BUILD-05 | Per-instance port, stable MAC/name, stable key; coexisting runs | Identity derivation (Pattern 2); two instances built with different `-s` values |
| BUILD-06 | `reboot_timeout: 0s`; ready only after real API handshake; clear failure if port taken | Go Noise client verified against real firmware (Pattern 4); busy-port behaviour verified (api marked FAILED but process lives) |
| BUILD-07 | `sil down`; no orphans when simulator dies | `Pdeathsig` + `Setpgid` verified; supervisor SIGKILL kills firmware (Pattern 5) |
| BUS-01 | UDS, NDJSON flat JSON, simulator listens | Protocol spec (Pattern 6) |
| BUS-02 | HELLO (version, manifest) + snapshot on every (re)connect | Protocol spec sequence + restart matrix |
| BUS-03 | Non-blocking hub, backoff reconnect, codegen path validation | Prototype hub reused; `validate_path` verified to reject 130-byte path at config time |
| BUS-04 | Golden vectors tested from Go and C++ | Vector format + float-formatting finding (Pattern 6, Pitfall 9); host g++ 12 and image gcc 14 available |
| IO-01 | Float output platform per fan port, stock `fan: speed` on top | Verified round trip: speed 40 -> bus `{"k":"out","ch":"port1.duty","v":0.4}` |
| IO-02 | RPM sensor, 1 s windowed mean at `update_interval` | `PollingComponent` + `cv.polling_component_schema("1s")` compiled; time-weighted mean design (Pattern 7) |
| IO-06 | Required `channel:` key on every sim platform | Verified: omitting it fails config with `'channel' is a required option for [sensor.sim_bus]` |
| MODEL-01 | Linear duty->RPM, min duty, start threshold with hysteresis, max RPM, exact exponential lag | Model spec (Pattern 8) incl. snap-to-zero |
| MODEL-02 | `fan.toml` strict, one fan shipped | go-toml strict mode verified (located errors); Arctic constants with sources and assumptions |
| MODEL-04 | Injectable clock, dt integration, sim timestamps, wall-clock lint | Clock design + AST lint (Pattern 9) |
| CLI-05 | JSON output, exit codes 0/1/2 | Error/exit-code table (Pattern 10) |
| CLI-06 | CLI and UI share one HTTP/JSON API; CLI is a client except `up`/`down` | Control API on a Unix socket now, TCP listener later (Pattern 10, Assumption A3) |
</phase_requirements>

## Summary

Every open Phase 1 spike from STATE.md was resolved by experiment on this host (Debian 12, glibc 2.36, 12 cores, Go 1.27.3, image `ghcr.io/esphome/esphome:2026.10.1`). A statically linked `host:` binary **with `api: encryption:`** builds in the pinned image (Noise libs `esphome/noise-c` and `esphome/libsodium` are fetched at build time into ESPHome's data dir) and runs on the host. The build is cheap: cold 7.0 s, a second instance with warm ccache 5.7 s, no-op 1.3 s, component-file edit 1.6 s, identity-only change (port) 1.9 s. After the library archives are present, a build runs fully offline (`--network none`, 6.6 s). The real encrypted ready check is a ~90-line Go client (`github.com/flynn/noise` v1.1.0, pattern NN + psk0) that performs the Noise handshake and a `HelloRequest/HelloResponse` exchange by hand-encoding two tiny protobuf messages; it also reads the server's name and MAC from the pre-handshake hello, which doubles as an identity check against "another process holds my port". A wrong key is rejected with `Handshake MAC failure`. aioesphomeapi 46.12.0 inside the pinned image works offline with `--network host` and `noise_psk`, which gives the e2e tests a Python client with no host install.

Several non-obvious facts will break a naive plan: (1) the prototype's `sim_bus.h` fails to compile unless `binary_sensor` and `light` are in the config (ESPHome compiles every `.cpp` in the component dir), so platform code must be guarded by `USE_*` defines; (2) `esphome config` does **not** reject `wifi`, `ota: esphome`, `http_request` or `json:`, so the Go lint is load-bearing, and auto-loaded components are only visible after `compile --only-generate` (storage JSON `loaded_integrations`); (3) a busy API port does not stop the firmware, it logs `Socket bind: errno 98` and `api was marked as failed` and keeps running, so the port must be test-bound before start; (4) libraries are fetched into the ESPHome data dir, so a brand-new instance cannot build offline unless `pio_components` is seeded from a shared tool cache; (5) C++ `std::to_chars` and Go `strconv.FormatFloat` disagree on fixed-versus-exponent for some magnitudes (`1.2345679e+08` vs `123456792`, `1e-04` vs `0.0001`), so golden vectors must pin decoded values, not encoder bytes, outside a restricted value set.

**Primary recommendation:** Build the phase as five layers in dependency order: (a) protocol spec + golden vectors + clock/lint scaffolding, (b) C++ `components/sim_bus` (hub, output, polling sensor) and Go `bus` + `model` + `engine` against a fake peer, (c) build pipeline (docker wrapper, two-stage lint, identity, hash cache), (d) supervisor/daemon (`sil serve`, firmware runner with `Pdeathsig`, Noise ready check, control API on a Unix socket), (e) CLI (`up/down/status/sim restart/fw restart`) and the build-tagged e2e restart matrix using aioesphomeapi from the pinned image.

## Architectural Responsibility Map

This is not a web application; the tiers are the processes in the bench.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Firmware build (compile, static link) | Docker container (pinned ESPHome image) | `sil` CLI (orchestration, lint, cache) | Toolchain lives only in the image; CLI wraps and validates |
| Banned-component lint, identity assertions | `sil` CLI (Go) | Component Python (codegen checks) | D-18: Go parses resolved YAML; `channel:`/path checks stay in component schema |
| Firmware execution | Host process (static `program`) supervised by `sil serve` | — | Runs directly on host; supervisor owns process group and `Pdeathsig` |
| Duty output, RPM sensor (IO surface) | Firmware (`components/sim_bus`) | Go `bus` (peer side) | Firmware owns actuator commands; sim owns plant outputs |
| Fan plant model (duty -> RPM) | Go `model` package (pure) | Go `engine` (ticks, state) | Whoever produces the signal on real hardware owns it in the sim |
| Sim time | Go `clock` package | `engine` (consumer) | Single injectable source; wall clock banned elsewhere |
| Bus listener, HELLO/snapshot | Go `bus` + engine (restartable) | Firmware hub (reconnect) | Simulator listens, firmware connects; either side restartable |
| Supervision (restart policy, crash-loop guard, orphan prevention) | `sil serve` supervisor | OS (`Pdeathsig`, process group) | D-02/D-03 |
| Encrypted API readiness | `sil serve` (Noise client) | `sil up` (waits on status) | Handshake needs key and identity; daemon owns it so `status` can show it |
| Status / control surface | Control API (HTTP/JSON over Unix socket) | CLI as client | CLI-06: one vocabulary; UI listener added in Phase 3 |
| Home Assistant / aioesphomeapi access | Firmware native API (TCP 0.0.0.0:port) | — | HA talks to firmware directly, never through the sim |
| Persistent identity (port, key) | `$XDG_STATE_HOME/sil/<name>/` | — | D-06/D-07/D-08 |
| Ephemeral run state (bus socket, prefs, logs, run.json) | `$XDG_RUNTIME_DIR/sil/<name>/` | — | D-06; recreated each `up` |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.27.3 (installed at `~/.local/bin/go`, `go.mod`: `go 1.27`) | Everything Go | Installed and working [VERIFIED: `go version` -> `go1.27.3 linux/amd64`]. Not on the default PATH of non-interactive shells: scripts and Makefile must `export PATH=$HOME/.local/bin:$PATH` or set `GO=`. |
| ESPHome image | `ghcr.io/esphome/esphome:2026.10.1` (ID `sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64`) | Firmware toolchain | Pinned [VERIFIED: `docker image inspect` -> RepoDigests `ghcr.io/esphome/esphome@sha256:eab8cd6b...`]. Reference it as `ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64`. Never auto-pull. |
| `github.com/alecthomas/kong` | v1.17.0 | CLI grammar | In STACK.md (Context7-verified there); proxy: v1.17.0 published 2026-10-30 [VERIFIED: proxy.golang.org] |
| `github.com/pelletier/go-toml/v2` | v2.4.3 | Strict `fan.toml` | [VERIFIED: spike] `DisallowUnknownFields` returns `*toml.StrictMissingError` with key, line and column |
| `github.com/flynn/noise` | v1.1.0 (+ `golang.org/x/crypto` v0.58.0) | Noise_NNpsk0_25519_ChaChaPoly_SHA256 client for the ready check | [VERIFIED: spike] handshake + HelloResponse against the real firmware. v1.1.0 pins x/crypto from 2021 by default; run `go get golang.org/x/crypto@v0.58.0` (worked, handshake still passes). |
| `go.yaml.in/yaml/v3` | v3.0.6 (2026-10-13) | Parse `esphome config` stdout for the lint | Maintained fork under the YAML org (`github.com/yaml/go-yaml`) [VERIFIED: proxy origin]; `gopkg.in/yaml.v3` last release 2022. Not yet exercised in a spike (A5). |
| stdlib | Go 1.27 | `net/http`, `os/exec`, `debug/elf`, `crypto/*`, `log/slog`, `embed`, `testing`, `go/parser` | No router, no testify, no cobra |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/coder/websocket` | v1.8.15 | UI live feed | **Not in Phase 1** (Phase 3). Do not add. |
| aioesphomeapi | 46.12.0 (shipped in the image) | e2e test client: set fan speed, read RPM | Run **inside the pinned image**: `docker run --network host --entrypoint python3 -v scripts:/s:ro IMAGE /s/client.py PORT KEY` [VERIFIED: ran against firmware, got `Fan 1` state speed_level=40 and `Fan 1 Speed` 1234.0] |
| g++ | 12.2 on host, 14.2 in image | C++ golden-vector test | `g++ -std=c++20` host compile of header-only `sim_protocol.h` (no ESPHome includes) [VERIFIED: g++ 12.2.0 present] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| flynn/noise | Hand-rolled Noise over `x/crypto` | Never; Noise has subtle prologue/psk-placement rules. flynn/noise worked first try. |
| flynn/noise + hand protobuf | `google.golang.org/protobuf` + ESPHome `api.proto` | Heavy dependency for two 9-byte messages. Only if Phase 2 needs entity listing from Go (it should use aioesphomeapi in tests instead). |
| TCP-connect ready check | — | Rejected by BUILD-06: a TCP connect succeeds on a busy port and before encryption is up [VERIFIED: plain `/dev/tcp` connect succeeded while handshake MAC failure still occurred] |
| Shared `ESPHOME_DATA_DIR` for all instances | Per-instance data dir + seeded `pio_components` | Shared dir keys `storage/<yaml>.json` by yaml filename, so concurrent instances of the same YAML would race. Per-instance is safe. |
| `-s` substitution for the API key | `!env_var SIL_API_KEY` + `docker run -e SIL_API_KEY` (no value on argv) | `!env_var` works and fails loudly when unset [VERIFIED]; keeps the key out of `ps`. Recommended for the key; `-s` for name/mac/port. |

**Installation:**
```bash
export PATH=$HOME/.local/bin:$PATH     # go 1.27.3 lives here
go mod init github.com/zeroflow/esphome-sil
go get github.com/alecthomas/kong@v1.17.0 github.com/pelletier/go-toml/v2@v2.4.3 \
       github.com/flynn/noise@v1.1.0 golang.org/x/crypto@v0.58.0 go.yaml.in/yaml/v3@v3.0.6
CGO_ENABLED=0 go build -trimpath -o bin/sil ./cmd/sil
```

**Version verification:** versions and publish dates were read from `https://proxy.golang.org/<module>/@latest` and `/@v/list` on 2026-11-08 (kong 2026-10-30, go-toml 2026-07-05, noise v1.1.0 2024-02-02, yaml 2026-10-13, x/crypto 2026-10-09).

## Package Legitimacy Audit

`gsd-tools package-legitimacy check` supports only `npm|pypi|crates`; it rejects `--ecosystem go` ("Usage: ... --ecosystem <npm|pypi|crates>") [VERIFIED: ran it]. The Go module proxy was used instead (existence, origin repo, tags). Per the provenance rule, a package not confirmed by the seam and by Context7/official docs stays `[ASSUMED]` for name provenance even though it exists on the proxy and was exercised.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| github.com/alecthomas/kong | Go proxy | first tag 2018-09 | n/a (proxy has no counts) | github.com/alecthomas/kong | not seam-checkable; in STACK.md via Context7 | Approved (prior research) |
| github.com/pelletier/go-toml/v2 | Go proxy | v2 line, latest 2026-07 | n/a | github.com/pelletier/go-toml | not seam-checkable; in STACK.md | Approved (prior research) |
| github.com/flynn/noise | Go proxy | v1.0.0 2021-04, v1.1.0 2024-02 | n/a | github.com/flynn/noise (BSD-style license text seen) | not seam-checkable; name from training knowledge `[ASSUMED]`, but exercised against real firmware | Approved with `checkpoint:human-verify` before `go get` |
| golang.org/x/crypto | Go proxy | official `go.googlesource.com/crypto` | n/a | Go project | not seam-checkable; Go team module | Approved |
| go.yaml.in/yaml/v3 | Go proxy | v3.0.0 2022-05, v3.0.6 2026-10 | n/a | github.com/yaml/go-yaml | not seam-checkable; name from training knowledge `[ASSUMED]` | Approved with `checkpoint:human-verify` before `go get` |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none by tooling; `flynn/noise` and `go.yaml.in/yaml/v3` carry `[ASSUMED]` name provenance, so the planner inserts one `checkpoint:human-verify` task before the first `go get` of each (cheap: the maintainer glances at the two repo URLs). All five licenses are compatible with GPL-3.0 as far as read (flynn/noise is BSD-style).

## Architecture Patterns

### System Architecture Diagram

```
 developer / agent / pytest
        │  sil up|status|down|sim restart|fw restart   (JSON on stdout, progress on stderr)
        ▼
 ┌──────────────────────────────────── sil CLI (short-lived) ────────────────────────────────────┐
 │  up: validate name ─▶ identity (state dir) ─▶ port test-bind ─▶ run dir (fresh)                │
 │      ─▶ BUILD PIPELINE ─▶ spawn `sil serve <name>` (Setsid) ─▶ poll status until ready ─▶ JSON │
 │  other cmds: read run.json ─▶ HTTP/JSON over api.sock ─────────────────────────┐               │
 └──────────────┬────────────────────────────────────────────────────────────────┼───────────────┘
                │ docker run (compile)                                            │
                ▼                                                                 ▼
 ┌──────── pinned ESPHome container ───────┐        ┌────────── sil serve <name> (one detached process) ──────────┐
 │ esphome -s … config      (1.0 s, offline)│        │ SUPERVISOR                                                   │
 │ esphome … compile --only-generate (0.9 s)│        │  • control API (HTTP/JSON, Unix socket api.sock)             │
 │ esphome … compile        (1.3-7 s)       │        │  • firmware runner goroutine (LockOSThread, Pdeathsig,       │
 │ ▶ build/sil-<n>/.pioenvs/sil-<n>/program │        │    Setpgid, SIGTERM→SIGKILL, restart policy, crash-loop)     │
 └──────────────────────────────────────────┘        │  • Noise ready probe  ──────────────┐                       │
        two lint stages run on its outputs (Go)       │  ┌─ ENGINE (restartable) ────────┐ │                       │
                                                      │  │ bus.Server (bus.sock listener)│ │                       │
                                                      │  │ World: 4 × fan model, Clock   │ │                       │
                                                      │  │ snapshots ──▶ status          │ │                       │
                                                      │  └───────────────▲───────────────┘ │                       │
                                                      └──────────────────┼─────────────────┼───────────────────────┘
                                          NDJSON over unix socket (bus.sock)               │ TCP 127.0.0.1:<api_port>
                                                      ┌──────────────────┴───────────────┐ │ Noise-encrypted native API
                                                      │ firmware `program` (static host) │◀┘ (also HA / aioesphomeapi)
                                                      │ sim_bus hub ─ output ×4 ─ sensor ×4 (1 s mean)             │
                                                      │ fan: speed (stock) · api (encryption, reboot_timeout 0s)   │
                                                      └──────────────────────────────────┘
 data path: API fan command → fan:speed → SimOutput.write_state(duty) → {"k":"out","ch":"port1.duty"} → engine
            → model.Step(dt) → {"k":"in","ch":"port1.rpm"} → SimSensor window mean → update() (1 s) → API state
```

### Recommended Project Structure
```
esphome-sil/
├── go.mod                        # module github.com/zeroflow/esphome-sil, go 1.27
├── Makefile                      # build, test, lint, test-cpp, e2e
├── cmd/sil/main.go               # thin: kong parse -> internal/cli
├── internal/
│   ├── clock/                    # ONLY package allowed to call time.Now/Sleep/After/NewTicker/…
│   ├── model/                    # pure fan model Step(dt, duty)
│   ├── fanlib/                   # fan.toml loader/validator (+ embed.go in fans/)
│   ├── bus/                      # codec (flat NDJSON) + Server (unix listener, HELLO/snapshot)
│   ├── engine/                   # actor: World, ticks, inbox, snapshots; restartable
│   ├── supervisor/               # sil serve: firmware runner, restart policy, ready probe, control API
│   ├── apiclient/                # Noise native-API probe (HelloRequest only)
│   ├── build/                    # docker wrapper, lint stages, hash cache, identity injection
│   ├── identity/                 # state dir, port/mac/key derivation
│   ├── rundir/                   # run dir, run.json (atomic write)
│   ├── control/                  # Control interface + v1 JSON types + http handlers (UDS) 
│   └── cli/                      # kong commands; client of control API (except up/down)
├── components/sim_bus/           # ESPHome external component (repo-root `components/` for github:// later)
│   ├── __init__.py  output.py  sensor.py
│   ├── sim_protocol.h            # header-only codec, std-only, shared by firmware and C++ test
│   ├── sim_bus.h/.cpp  sim_output.h  sim_sensor.h
├── protocol/
│   ├── PROTOCOL.md
│   └── vectors/frames.tsv        # golden vectors for Go and C++
├── tests/cpp/test_protocol.cpp   # g++ test; NEVER under components/
├── fans/{embed.go, arctic-p12-pwm-pst.toml}
├── examples/fan-skeleton.yaml    # Phase 1 example (4 ports)
└── e2e/                          # //go:build sil_e2e; python client script for the image
```
`components/` stays at the repo root so `github://zeroflow/esphome-sil@vX.Y` works later (FW-03). C++ tests and Go tests never live in `components/` because ESPHome compiles every `.cpp` it copies.

### Pattern 1: Docker build invocation [VERIFIED: ran on this host]
**What:** One fixed `docker run` shape for `config`, `compile --only-generate` and `compile`.
**When to use:** All three build-pipeline steps. Only the trailing subcommand and `--network` differ.
```bash
# Source: experiment 2026-11-08 (build2.sh); ESPHome entrypoint /entrypoint.sh in image
docker run --rm --network none \                # "bridge" only when pio_components is not seeded
  --user "$(id -u):$(id -g)" \
  -e HOME=/cache/home -e ESPHOME_DATA_DIR=/build/data -e SIL_API_KEY \  # key value comes from cmd.Env, never argv
  -v "$REPO_ROOT:$REPO_ROOT:ro" -w "$YAML_DIR" \   # same absolute path; repo is read-only so tree stays clean
  -v "$CACHE/build:/build" -v "$CACHE/cache:/cache" \
  ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64 \
  -s sil_name a -s sil_mac 02:aa:aa:aa:aa:01 -s sil_port 6101 \
  compile fan-skeleton.yaml
# program: $CACHE/build/sil-a/.pioenvs/sil-a/program   (host path of /build/sil-a/...)
```
Facts behind each flag: the entrypoint exports `ESPHOME_BUILD_PATH=/build` iff `/build` is a directory, so the build lands in `/build/<name>`; `/cache` holds platformio dirs and `/cache/host/ccache`; `ESPHOME_DATA_DIR` moves ESPHome's `.esphome` (storage JSON and the fetched `pio_components`) out of the config dir, otherwise it is created next to the YAML in the repo (first spike run left `examples/.esphome` and a `.gitignore`) [VERIFIED]. With the repo mounted `:ro` no file appears in the working tree. Run dirs, cache dirs and the identity dir are created with mode 0700 because `main.cpp` in the build dir contains the API key in cleartext.

### Pattern 2: Identity, directories, naming
- **Instance name:** regex `^[a-z0-9][a-z0-9-]{0,26}$` (the firmware name is `sil-` + name; ESPHome allows `abcdefghijklmnopqrstuvwxyz0123456789-_` [VERIFIED: image `esphome/const.py:9` `ALLOWED_NAME_CHARS = "abcdefghijklmnopqrstuvwxyz0123456789-_"`] and a 31-character maximum [VERIFIED: image `esphome/core/config.py:130` `max_length = 31`]; underscores are discouraged for hostnames, so exclude them).
- **MAC:** `02:` + first 5 bytes of `sha256("sil-mac:" + name)` as hex pairs. Note `esphome config` prints it upper-cased (`02:AA:AA:AA:AA:01`): compare case-insensitively.
- **Key:** `crypto/rand` 32 bytes, `base64.StdEncoding`, file `identity.json` mode 0600 in a 0700 dir. Generated once.
- **Port:** at first `up`, pick the lowest port >= 6054 that (a) is not claimed by another `identity.json` under `$XDG_STATE_HOME/sil/*/` and (b) can be test-bound on `0.0.0.0`. Persist. Sticky afterwards; if the sticky port cannot be bound, exit 2 `port_in_use` (holder best-effort via `/proc/net/tcp` inode -> `/proc/*/fd`, only your own processes are visible).
- **Run dir** `$XDG_RUNTIME_DIR/sil/<name>/` (here `/run/user/1002`, exists [VERIFIED]) with fallback `/tmp/sil-$UID/<name>/`: `run.json` (atomic tmp+rename, written only once the control socket serves), `bus.sock`, `api.sock`, `prefs/` (`ESPHOME_PREFDIR`), `build.log`, `firmware.log`, `sil.log`. `sil up` refuses (exit 1 `already_running`) when `run.json` names a live PID; otherwise it wipes and recreates the dir.
- **Unix socket length:** compute `len(path)` for both sockets at `up` time; reject > 107 bytes with exit 2 `socket_path_too_long` naming the path (STACK.md: `sun_path` limit). The firmware also reads `SIL_BUS` at runtime, so the compiled-in YAML `path:` is only a default.
- **Cache** `$XDG_CACHE_HOME/sil/build/<name>/{build,cache}` per D-16, plus a shared `$XDG_CACHE_HOME/sil/toolcache/pio_components` that is copied into `build/data/pio_components` before a build [VERIFIED: seeded copy -> `--network none` build succeeded in 6.6 s; an unseeded new instance fails offline with `Failed to download from all mirrors` after ~13 s]. After any online build, copy `build/data/pio_components` back to the toolcache if it is missing.

### Pattern 3: Two-stage lint [VERIFIED matrix]
Stage 1: `esphome config` (1.0 s, `--network none`) -> stdout is plain YAML of the resolved config (packages and `!include` expanded, substitutions applied). Parse with `go.yaml.in/yaml/v3`; walk top-level keys and every `platform:` value. Reject names from the BUILD-03 list with a message naming the component and a replacement hint. Assert `api.reboot_timeout == "0s"`, `api.encryption.key` present, `"-static" in esphome.build_flags`, and that `esphome.name`, `host.mac_address`, `api.port` equal the injected identity.
Stage 2: `esphome ... compile --only-generate` (0.9 s offline) writes `<ESPHOME_DATA_DIR>/storage/<yamlfile>.json`; read `loaded_integrations` and `loaded_platforms` and reject `json` and any banned name auto-loaded by another component. (`config` alone never shows auto-loads.)

What `esphome config` does on its own for each banned item in a `host:` config [VERIFIED 2026-11-08]:

| Component | `esphome config` result | So the Go lint must catch it? |
|-----------|------------------------|-------------------------------|
| `wifi` | passes (and crashes later in codegen per ARCHITECTURE.md) | **yes** |
| `ota: esphome` | passes | **yes** |
| `http_request` | passes | **yes** |
| `json:` | passes | **yes** (also auto-load via stage 2) |
| `time: homeassistant` | passes (not on the ban list; allowed) | no |
| `web_server` | rejects: "only available on [ESP32, ESP8266, BK72XX, ...]" | friendlier message only |
| `captive_portal` | rejects: "requires component wifi" | friendlier message only |
| `time: sntp` | rejects: not available on host | friendlier message only |
| `update: http_request` | rejects: "requires component ota" | friendlier message only |
| `i2c` (sda/scl) | rejects: "'sda' and 'scl' are not supported on host platform" | friendlier message only |
| `output: ledc`, `light: esp32_rmt_led_strip` | reject: "requires component esp32" | friendlier message only |
| `esp32:` | rejects: "Found multiple target platform blocks: host, esp32" | friendlier message only |
| `mqtt` | rejects (not on the list; pulls `json`) | covered by the `json` rule |
| `sensor: pulse_counter` | rejected in my probe only for missing `id`/`name`, i.e. **not proven** to be rejected | **yes** |

Because `esphome config` may fail on its own with these messages, the CLI must map any non-zero `config` exit into a JSON error (`config_invalid`, exit 1) that includes the ESPHome message and source line, and run the Go checks on stage-1 output only when `config` succeeded.

### Pattern 4: Real encrypted ready check [VERIFIED against real firmware]
**What:** Frame format and handshake, reproduced from aioesphomeapi `_frame_helper/noise.py` in the image and confirmed by a Go client.
- Client sends `01 00 00` (empty hello), then a frame `01 <len16 BE> 00 <noise msg1>`.
- Server answers a hello frame `01 <len16> 01 <name>\0 <mac>\0` (verified payload: `sil-a\0 02aaaaaaaa01\0`), then a handshake frame `01 <len16> 00 <noise msg2>`; on a wrong key the second frame is `01 <len16> 01 "Handshake MAC failure"`.
- Noise: `Noise_NNpsk0_25519_ChaChaPoly_SHA256`, prologue `"NoiseAPIInit\x00\x00"`, PSK = base64-decoded 32-byte key, psk placement 0, initiator.
- Transport frames: `01 <len16> <ciphertext>`; plaintext is `type(2 BE) len(2 BE) data`. `HelloRequest` is type 1 (`client_info`=1 string, `api_version_major`=2, `api_version_minor`=3), `HelloResponse` type 2 (`api_version_major`=1, `minor`=2, `server_info`=3, `name`=4).
Verified result: `type=2 len=26 body="\b\x01\x10\x13\x1a\t2026.10.1\"\tsil-spike"` (API 1.19, server_info `2026.10.1`, name).
**Ready = handshake ok AND hello name == `sil-<name>` AND hello MAC == derived MAC AND HelloResponse name matches.** That catches "another instance holds my port" even though the firmware itself keeps running.
```go
// Source: spike /tmp/sil-spike/gospike/main.go (flynn/noise v1.1.0), condensed
cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
hs, _ := noise.NewHandshakeState(noise.Config{
    CipherSuite: cs, Pattern: noise.HandshakeNN, Initiator: true,
    Prologue: []byte("NoiseAPIInit\x00\x00"), PresharedKey: psk, PresharedKeyPlacement: 0,
})
m1, _, _, _ := hs.WriteMessage(nil, nil)
// write: 01 00 00 | 01 len(m1)+1 00 | m1 ; read server-hello frame, then handshake frame (first byte 0 = ok)
_, csW, csR, err := hs.ReadMessage(nil, hf[1:])          // csW encrypts client->server, csR decrypts
ct, _ := csW.Encrypt(nil, nil, []byte{0, 1, 0, 9, 0x0a, 3, 's', 'i', 'l', 0x10, 1, 0x18, 10})
```
Probe policy: retry every 250 ms for up to 20 s after each firmware start; per-attempt deadline 2 s; close the connection after HelloResponse (no `ConnectRequest`/login needed, but the ESPHome server tolerated closing right after Hello [VERIFIED: log showed only a benign "Reading failed CONNECTION_CLOSED"]). Run the probe from the supervisor and expose `api.ready` in status; `sil up` polls status.

### Pattern 5: Supervisor, detach, orphans [VERIFIED]
- `sil up` builds in the foreground, then execs itself as `sil serve <name>` with `SysProcAttr{Setsid: true}`, stdout/stderr -> `sil.log`, stdin /dev/null, and waits for `phase == "ready"` (or `"failed"`) from the control socket, timeout 60 s excluding the build. On failure it prints the error JSON, exits 2 for infrastructure failures and 1 otherwise, and includes the tail of `firmware.log`/`sil.log`.
- Firmware start: `exec.Command(program)` with `Env = os.Environ() + ESPHOME_PREFDIR=<run>/prefs + SIL_BUS=<run>/bus.sock`, `SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}`, stdout+stderr -> `firmware.log`. **Start it from a dedicated goroutine that calls `runtime.LockOSThread()` and never unlocks until the firmware is gone**: `Pdeathsig` fires when the *thread* that spawned the child exits, and Go can retire unlocked threads (Go issue pattern; in my spike both locked and unlocked variants killed the child on supervisor SIGKILL, so the lock is hardening, not a hard requirement [VERIFIED both variants; the thread-retire risk is `[ASSUMED]`]).
- Stop: SIGTERM to the process group, wait 3 s on the `cmd.Wait` channel, then SIGKILL the group (firmware flushes prefs on SIGTERM/SIGINT, STACK.md). `sil down`: `POST /api/v1/down` -> supervisor stops firmware, closes engine, removes run dir contents it owns, exits 0; if the control socket is dead but `run.json` PID exists, send SIGTERM to that PID, wait, SIGKILL, then remove the run dir. Down on a non-running instance: exit 0 with `{"stopped":false}`.
- Restart policy (D-03): `cmd.Wait` returns -> classify: requested stop (flag set by down/fw restart) vs `rebooted` (exit 0) vs `crashed` (code or signal). Auto-restart immediately with the same prefs dir and same `SIL_BUS`. Guard: more than 5 unrequested exits within 30 s (injected `clock`, not `time.Now`, so it is unit-testable with a manual clock) -> phase `failed`, stop restarting. Record `restarts`, `last_exit{kind, code, signal, at}` in status.
- `sil sim restart` -> `POST /api/v1/sim/restart` -> supervisor cancels the engine context, waits for its goroutine to finish (this closes the bus listener and the firmware connection; Go's `UnixListener.Close` unlinks the socket), then builds a **new** engine with a fresh `World` and a new listener on the same path. The firmware sees EOF, retries within 500 ms (prototype hub), sends HELLO and its snapshot. Because the engine is a value created from scratch, all model state (rpm, hysteresis) is dropped, which is the observable proof for criterion 3.
- `sil fw restart` -> `POST /api/v1/fw/restart` -> flagged stop + start; not counted by the crash guard.
- Kill matrix to automate (restart order matters): sim restart; fw restart; `kill -9 firmware` (auto-restart); sim restart *while* fw is restarting; supervisor `kill -9` (firmware must disappear; a following `sil up` must work, see Pitfall 12 for the stale `run.json`).

### Pattern 6: Sim-bus protocol (BUS-01/02/04) — recommended spec
Grammar (frozen by golden vectors): one JSON object per line, UTF-8, `\n` terminated, <= 4096 bytes including the newline, keys unique, no nesting, no arrays, values are numbers or strings only (booleans as 0/1). String escapes: only `\"` and `\\`; any other backslash sequence or control character is a parse error. `k` is the first key by convention (encoders emit a fixed key order). Channel names match `^[a-z0-9_]+(\.[a-z0-9_]+)*$` (checked in the component schema, so strings on the wire never need other escapes).

| Direction | Message | Fields |
|-----------|---------|--------|
| fw -> sim | `hello` | `proto` (int, 1), `name` (string), `chans` (int N), `t` (int µs, firmware monotonic) |
| fw -> sim | `chan` × N | `ch`, `dir` (`out` = fw->sim, `in` = sim->fw), `type` (`f32`) |
| fw -> sim | `out` × M (snapshot of last values) then `sync` | `ch`, `v`, `t` |
| sim -> fw | `welcome` | `proto`, `t` (sim µs) |
| sim -> fw | `in` × K (every in-channel with a known value) then `sync` | `ch`, `v`, `t` |
| both | steady state `out` / `in` | `ch`, `v`, `t` (state/level frames, dedupe unchanged values) |
| either | `err` then close | `msg` |
| either | `bye` | — |

Rules: protocol mismatch -> `err` + close, status shows `bus.error="proto_mismatch"`. The Go server validates the manifest against the model's ports (4 duty channels `portN.duty` dir `out`; 4 rpm channels `portN.rpm` dir `in`); unknown or missing channels become `warnings[]` in status, not fatal. Both sides must be state-based: a second connection while one is active replaces it (close the older one), which is what a firmware restart looks like. The firmware must dedupe identical consecutive `out` values (the prototype sent `port1.duty 0` twice on connect [VERIFIED in fake_sim output]).

Golden vectors: `protocol/vectors/frames.tsv`, columns `name<TAB>valid|invalid<TAB>line<TAB>expect` where for `valid` the `expect` is the decoded tuple in a trivial text form (`k=out;ch=port1.duty;v=0.4;t=1000`) and for `invalid` a short error class. Both the Go test and `tests/cpp/test_protocol.cpp` read the same file with ordinary file I/O (no JSON library in C++ or the test). Cases to include: every message kind, escaped quote/backslash, `\u00e9`/bad escape (invalid), duplicate key (invalid), nested object (invalid), over-length line (invalid), missing `k` (invalid), exponent floats `1e-05`, `1.2345679e+08`, `123456792`, `0.0001`, negative numbers, integers vs floats, trailing garbage. Compare decoded **values** (floats with `==` after the same `strtof`/`ParseFloat(…,32)` rounding), not re-encoded bytes (Pitfall 9). Encoders may additionally be pinned byte-for-byte on a small set where both formatters coincide (`0.4`, `1234.5`, `1e+06`, `65536`).

### Pattern 7: Firmware components (BUS-03, IO-01, IO-02, IO-06)
- **Reuse the prototype hub**, with these changes (all unverified items are flagged in Pitfalls): `setup_priority::BUS`; read `SIL_BUS` from `getenv` at `setup()` (fallback to YAML `path`); `validate_path` in `__init__.py` (verified, 107-byte limit); exponential reconnect backoff 100 ms -> 2 s using `millis()`; HELLO/`chan`/snapshot/`sync` on connect; per-channel last-sent dedupe; tx buffer cap (drop and reconnect if the residual exceeds ~64 KiB); `send_text` escaping goes away because Phase 1 sends no strings other than the fixed `hello.name`.
- **Platform code must be guarded and split by header.** Keep the hub header free of platform includes; put `SimOutput` in `sim_output.h` and `SimSensor` in `sim_sensor.h`, and wrap their `.cpp` bodies in `#ifdef USE_OUTPUT` / `#ifdef USE_SENSOR`. The generated `defines.h` of a config with `fan`+`output`+`sensor` contained `#define USE_OUTPUT` and `#define USE_SENSOR` [VERIFIED: grep of the generated defines.h]. Without guards the build fails with `esphome/components/binary_sensor/binary_sensor.h: No such file or directory` when `binary_sensor` is not in the config [VERIFIED: first spike build]. Phase 2's binary_sensor/light platforms add `USE_BINARY_SENSOR` / `USE_LIGHT` guards the same way (guarded variant compiled [VERIFIED]).
- **Output:** `output.FLOAT_OUTPUT_SCHEMA.extend({Required(CONF_ID): declare_id(SimOutput)}).extend(CHILD_SCHEMA).extend(COMPONENT_SCHEMA)`; `write_state(float)` -> `bus_->set_out(channel_, v)` (dedupe, flush). Verified round trip `fan: speed` 40 -> `"v":0.4`.
- **Sensor (1 s mean):** `sensor.sensor_schema(SimSensor, accuracy_decimals=0).extend(CHILD_SCHEMA).extend(cv.polling_component_schema("1s"))`; class derives `sensor::Sensor, PollingComponent, SimChild` [VERIFIED: compiled]. Time-weighted zero-order-hold mean over the polling window:
```cpp
// on_value(v): area_ += last_ * (now - last_t_); last_t_ = now; last_ = v; have_ = true;
// update():    if (!have_ || !bus_->connected()) return;       // tach lost: stop publishing, HA keeps last state
//              area_ += last_ * (now - last_t_); float mean = area_ / (now - win_start_);
//              area_ = 0; last_t_ = win_start_ = now; publish_state(mean);
// now = micros() (uint32 wrap handled by unsigned subtraction) or millis_64()
```
  On (re)connect, reset the window so a stale `last_` is not averaged across the outage. Configure `unit_of_measurement: RPM`, `accuracy_decimals: 0`, `name: Fan N Speed` in the example (HIL contract names matter in Phase 2, but use them now to avoid renames).
- **Example firmware** `examples/fan-skeleton.yaml` (4 ports) uses substitutions that `sil` always overrides, keeps `name: sil-${sil_name}`, `friendly_name: SiL ${sil_name}`, `host: mac_address: ${sil_mac}`, `api: port: ${sil_port}, reboot_timeout: 0s, encryption: key: !env_var SIL_API_KEY`, `esphome: build_flags: [-static]`, `external_components: [{source: {type: local, path: ../components}, components: [sim_bus]}]`. The Go lint verifies the resolved values equal the injected identity, which is how "identity is never hand-written" is enforced for `--config` YAMLs.

### Pattern 8: Fan model (MODEL-01/02, D-10..D-14)
Pure function of `(params, state, duty, dt)`; no clock, no I/O.
```
duty  = clamp(duty, 0, 1)
running: if !running && duty >= start_duty → running = true
         if  running && duty <  min_duty   → running = (below_min == "hold")
target = !running ? 0
       : duty < min_duty (hold only) ? min_rpm
       : min_rpm + (duty - min_duty) / (1 - min_duty) * (max_rpm - min_rpm)
tau    = target > rpm ? tau_up : tau_down
rpm   += (target - rpm) * (1 - exp(-dt/tau))
if target == 0 && rpm < 0.5 → rpm = 0           // exponential never reaches 0; snap or the 1 s mean shows 0.0003
```
Initial state: `running = (below_min == "hold")`, `rpm = 0`. Hysteresis follows **duty only** (a coasting fan with duty back in the dead band stays "stopped"). `dt` is whatever the clock delivers; the exact form is stable for any `dt` (needed for later time factors). Unit tests on a `ManualClock`: after `tau_up` seconds rpm is 63.2 % of target; stays 0 for duty in [0, start_duty) from rest; starts at `start_duty`; keeps running at a duty between `min_duty` and `start_duty` once running; stalls below `min_duty`; invalid dt (0, negative) is a no-op.

**Arctic P12 PWM PST constants** (`fans/arctic-p12-pwm-pst.toml`; `source` field records all of this):
- `min_rpm = 200`, `max_rpm = 1800`: ARCTIC product page "200—1800 rpm PWM-gesteuert (0 rpm unter 5 % PWM)" [CITED: https://arctic.de/it/P12-PWM-PST/ACFAN00120A]. (The A-RGB sibling is 200-2000 rpm [CITED: https://www.arctic.de/en/P12-PWM-PST-A-RGB-0dB], do not use that figure.)
- `below_min = "stop"`, `min_duty = 0.05`: "0 rpm below 5 % PWM" [CITED: same page]. An independent review measured about 215 RPM at 5 % and about 1829 RPM at 100 % [CITED via search result summary: techpowerup.com/review/arctic-p12-pwm-pst-120-mm-fan, page itself returned HTTP 403]; consistent with `min_rpm = 200`.
- `start_duty`, `tau_up`, `tau_down`: **no datasheet value exists** (the page lists only a 3.9 V starting voltage). Use `start_duty = 0.10`, `tau_up = 1.0` s, `tau_down = 2.5` s `[ASSUMED]` (A1) and say so in the `source` field and in a comment so nobody mistakes them for measurements.

### Pattern 9: Injectable clock and the wall-clock lint (MODEL-04)
- `internal/clock`: `type Clock interface { Now() time.Duration /*since run start*/; NewTicker(d) Ticker; NewTimer(d) Timer; Sleep(ctx, d) }`, with `Real` (monotonic via the only `time.Now()` call, factor fixed 1) and `Manual` (advance in tests). Provide a `Deadline(d) time.Time` helper for `net.Conn.SetDeadline`/process-kill timeouts so non-sim code never needs `time.Now`.
- The supervisor, probe and crash-loop guard take a `Clock` too (testable restart policy with `Manual`). `context.WithTimeout` is fine (no direct banned call).
- **Lint = a Go test** in `internal/lint` using `go/parser` over all `.go` files (including `_test.go`) that fails on selector expressions `time.{Now,Since,Until,Sleep,After,AfterFunc,Tick,NewTimer,NewTicker}` anywhere except `internal/clock/` (resolve the import alias; flag dot-imports). No golangci-lint/forbidigo (not installed). Runs under plain `go test ./...`, so it gates every task. Success criterion 2 is "a lint fails on any wall-clock call outside it": include a self-test with a fixture file proving the lint fires.
- Engine tick: 10 ms of sim time from `Clock.NewTicker`; `dt = clock.Now() - last`. Bus messages carry sim time `t` from `clock.Now()`; the firmware's `t` is recorded for diagnostics only.

### Pattern 10: CLI, control API, exit codes (CLI-05/06)
- **Commands (kong):** `sil up <name> [--config PATH] [--api-port N] [--rebuild] [--quiet]`, `sil down [<name>]`, `sil status [<name>]`, `sil sim restart [<name>]`, `sil fw restart [<name>]`, hidden `sil serve <name>`. Name resolution: positional, else `SIL_INSTANCE` env (kong `env:` tag), else the single directory under `$XDG_RUNTIME_DIR/sil/` containing a live `run.json`; zero or several -> exit 1 `instance_ambiguous` with candidates. (`sil list` is v2, RUN-03.)
- **stdout is always one JSON object** per command (success or error); human text only on stderr; slog on stderr. `--quiet` silences stderr.
- **Error object:** `{"error":{"code":"port_in_use","message":"…","detail":{…}}}`.
- **Exit codes (decision):** 0 ok. 1 = command failed / user-fixable (lint rejection, `config_invalid`, `already_running`, `instance_ambiguous`, `not_running`, firmware crash-loop failed, `fan_toml_invalid`, name invalid). 2 = infrastructure (docker missing or image not present, build failed in compile, cannot create dirs, `port_in_use` — required by criterion 4, `socket_path_too_long`, ready timeout, control socket unreachable for a run that claims to be live).
- **Control API v1 (Phase 1 surface):** `GET /api/v1/status`, `POST /api/v1/sim/restart`, `POST /api/v1/fw/restart`, `POST /api/v1/down`. Status shape (all fields are the contract Phase 2/3 extend, never renamed):
```json
{"schema":1,"instance":"a","phase":"starting|ready|failed|stopping",
 "api":{"device_name":"sil-a","mac":"02:aa:aa:aa:aa:01","host":"127.0.0.1","port":6101,"key":"<b64>","ready":true},
 "firmware":{"pid":123,"running":true,"bus_connected":true,"restarts":0,"last_exit":null},
 "sim":{"time_s":12.34,"time_factor":1,"engine_restarts":0,"bus_proto":1},
 "ports":[{"port":1,"fan":"arctic-p12-pwm-pst","duty":0.4,"rpm":1203.1,"state":"running"}],
 "warnings":[]}
```
  `ports[].rpm` is the model's instantaneous RPM, not the firmware's 1 s mean.
- **Transport (Assumption A3):** serve HTTP on a Unix socket `api.sock` (mode 0600 inside the 0700 run dir) now. Reason: status carries the API encryption key, and an unauthenticated `127.0.0.1` TCP port would expose it to any local user. The CLI uses `http.Client{Transport: &http.Transport{DialContext: dial-unix}}`; `curl --unix-socket` works for humans/agents. Phase 3 adds a second `http.Server` on `127.0.0.1` with the same mux and a status view that omits `api.key` unless the request arrived on the Unix socket. Put the handler behind a `control.Control` interface now so both transports and a future in-process test harness share it.

### Anti-Patterns to Avoid
- **Importing platform headers from the hub header** (breaks builds whose config lacks that platform).
- **Blocking socket calls in `setup()`/`loop()`** (prototype is non-blocking; keep it).
- **`time.Now()` anywhere outside `internal/clock`**, including supervisor timeouts.
- **Treating TCP connect to the API port as ready.**
- **Letting ESPHome write into the repo** (always `:ro` mount + `ESPHOME_DATA_DIR`).
- **Holding engine state in the supervisor** — a sim restart must construct a new `World`.
- **Sharing one `ESPHOME_DATA_DIR` between instances.**
- **Printing the key on stderr/logs.** Only `status` JSON (and `up` result JSON) carry it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Noise handshake for the native API | Custom Noise state machine on x/crypto | `github.com/flynn/noise` v1.1.0 | psk0 placement, prologue and cipher-state split are easy to get subtly wrong; library worked first try |
| TOML parsing + unknown-key rejection | Manual key checking | go-toml/v2 `DisallowUnknownFields` | Gives key, line, column and a rendered excerpt [VERIFIED] |
| YAML parsing of `esphome config` output | Regex on stdout | `go.yaml.in/yaml/v3` | Resolved config is nested YAML; regex misses lists/anchors |
| CLI grammar, env binding | Hand-rolled flag parsing | kong | Struct tags, `env:`, subcommands |
| Process-group kill and orphan prevention | pid files + polling | `SysProcAttr{Setpgid, Pdeathsig}` + `cmd.Wait` | Kernel enforces it; verified |
| Static-link verification | Parsing `file` output | `debug/elf`: no `.interp` section and no `DT_NEEDED` | stdlib, no shelling out |
| ESPHome `json:` for the bus | ArduinoJson | hand-written flat codec in `sim_protocol.h` | `json:` pulls ArduinoJson from the registry at build time (breaks hermetic builds), per STACK.md |
| Lint tooling | golangci-lint/forbidigo install | `go/parser` test in-tree | Zero extra tools, runs in `go test` |
| Float formatting parity across C++/Go | Byte-equal encoders for all magnitudes | Value-level vectors + restricted byte-pinned set | The two formatters disagree for some magnitudes [VERIFIED] |
| Python Noise/API client for e2e | Writing one | aioesphomeapi inside the pinned image | Already there, offline, matches HA's library |
| Process listing for "who holds the port" | Parsing `ss`/`lsof` output | `/proc/net/tcp` inode + `/proc/*/fd` scan, best effort | No external tool dependency; holder may be invisible (other user), so it is optional in the error |

**Key insight:** the risky parts of this phase are integration seams (docker flags, ESPHome side effects, process lifetimes, two-language wire format), not algorithms. Every seam above has a verified command or a verified failure mode; reuse them literally.

## Runtime State Inventory

Not a rename/refactor/migration phase. Omitted. (The prototype's channel names `fan1_pwm` -> `port1.duty` are a rename inside code that is being rewritten into `components/`; `prototype/` has no external runtime state. Its `.esphome/` build outputs and `__pycache__` are untracked-by-intent per `prototype/.gitignore`; leave them.)

## Common Pitfalls

### Pitfall 1: Platform headers break the build when the platform is not configured
**What goes wrong:** `fatal error: esphome/components/binary_sensor/binary_sensor.h: No such file or directory` [VERIFIED].
**Why it happens:** ESPHome compiles every `.cpp` in `components/sim_bus/` regardless of which platforms the YAML uses; the prototype header includes all platform headers.
**How to avoid:** hub header platform-free; per-platform headers; `#ifdef USE_<X>` around platform `.cpp` bodies and class definitions (guarded variant compiled).
**Warning signs:** any Phase 1 example that omits `binary_sensor`/`light` failing at the first `sim_bus.cpp` compile step.

### Pitfall 2: `esphome config` passing is not validation
**What goes wrong:** `wifi`, `ota: esphome`, `http_request`, `json` all pass `config` [VERIFIED]; `wifi` then dies in codegen, `ota` in compile.
**How to avoid:** Pattern 3 two-stage lint; contract-level test that builds and runs.

### Pitfall 3: Busy API port is silent
**What goes wrong:** second firmware on the same port logged `[W][api:057]: Socket bind: errno 98` and `api is marked FAILED` yet kept running [VERIFIED]; a TCP connect then reaches the *other* process.
**How to avoid:** test-bind `0.0.0.0:<port>` before `serve` (exit 2 `port_in_use`) AND identity-check name/MAC in the Noise hello. Sticky port + other instances' identities guard against two stopped instances claiming the same port.

### Pitfall 4: New instances cannot build offline
**What goes wrong:** `Failed to download from all mirrors` (13 s) with `--network none` for a never-built instance [VERIFIED]; reason: `noise-c` and `libsodium` archives are fetched into `<ESPHOME_DATA_DIR>/pio_components`.
**How to avoid:** shared `toolcache/pio_components` seeded after the first online build and copied into each instance's data dir (Pattern 2). First-ever build needs network; say so in the error message. Run `config`/`--only-generate` with `--network none` always; give `compile` network only when the seed is missing.

### Pitfall 5: ESPHome writes into the config dir
**What goes wrong:** `examples/.esphome/` and `examples/.gitignore` appeared in the first spike run (rw mount, default data dir) [VERIFIED].
**How to avoid:** `:ro` mount + `ESPHOME_DATA_DIR=/build/data`; assert `git status --porcelain` clean in the e2e test.

### Pitfall 6: Secrets in ESPHome output
**What goes wrong:** `esphome config` prints the key wrapped in literal `\033[8m…\033[28m` text (not real escape bytes) and substitutions are echoed; `-s sil_key` puts the key on the docker/`ps` command line; `main.cpp` stores it in cleartext.
**How to avoid:** `!env_var` + `docker run -e SIL_API_KEY` with the value only in `cmd.Env`; never log stage-1 output at info level; 0700 cache/run dirs; strip the key from `build.log` tails shown on failure (replace the exact key string); `status`/`up` JSON are the only places it is intentionally printed.

### Pitfall 7: `Pdeathsig` and OS threads
**What goes wrong:** the signal is tied to the spawning thread, not the process.
**How to avoid:** spawn from a `LockOSThread` goroutine that lives as long as the child (Pattern 5). Also remember `Pdeathsig` is cleared on `exec` of setuid binaries (not relevant) and does not protect against a *supervisor* that exits normally with the firmware running, so the supervisor's own shutdown path must always stop the firmware first (defer in `main`, signal handler for SIGTERM/SIGINT/SIGHUP).

### Pitfall 8: Exponential decay never reaches zero
**What goes wrong:** the 1 s mean shows tiny non-zero RPM forever after a stall and `rpm > 0` checks fail.
**How to avoid:** snap to 0 when `target == 0 && rpm < 0.5` (Pattern 8); test it.

### Pitfall 9: Float formatting differs between C++ and Go
**What goes wrong:** `std::to_chars(float)` prints `123456792` where Go `FormatFloat(…,'g',-1,32)` prints `1.2345679e+08`, and `1e-04` vs `0.0001` [VERIFIED]. Digits agree, notation does not.
**How to avoid:** golden vectors compare decoded values; byte-pin only a restricted set; the Go encoder uses `strconv.FormatFloat(float64(v),'g',-1,32)`; the C++ encoder uses `std::to_chars` (GCC >= 11, in both host g++ 12 and image gcc 14). Go 1.27's `encoding/json` is v2-backed and rejects duplicate keys and invalid UTF-8; the bus Go codec should not use `encoding/json` for the hot path anyway (flat grammar, simple hand parser is fine and shares the vectors), and must never emit `NaN`/`Inf` (omit instead).

### Pitfall 10: Sim restart must not leave stale firmware assumptions
**What goes wrong:** the firmware's cached input (`rpm`) is held across the outage, so the 1 s mean averages a stale value into the first post-resync window.
**How to avoid:** on disconnect mark inputs unknown and stop publishing (Pattern 7), reset window on reconnect; sim sends all `in` values in its snapshot before `sync`. Test: after `sim restart` RPM returns to the same trajectory within ~2 s.

### Pitfall 11: Reconnect storms and duplicate frames
**What goes wrong:** reconnect every loop tick, duplicate `out` on connect (observed twice in the prototype).
**How to avoid:** exponential backoff 100 ms -> 2 s with `millis()`, per-channel dedupe, snapshot built from a single `last_sent_` table.

### Pitfall 12: Stale `run.json` after a supervisor SIGKILL
**What goes wrong:** `run.json` and `bus.sock`/`api.sock` remain; `sil status` hangs or `sil up` thinks the instance is live.
**How to avoid:** liveness = PID alive **and** `/proc/<pid>/cmdline` matches `sil serve <name>` (PID reuse) **and** control socket answers; otherwise treat as dead, report `phase:"dead"` for status (exit 1 `not_running`), and let `up` wipe the run dir. Remove a stale `bus.sock` before listening.

### Pitfall 13: Crash-loop guard time source
**What goes wrong:** using `time.Now()` in the guard breaks the lint and makes it untestable.
**How to avoid:** inject `Clock`; unit test with `Manual` (5 exits in 30 s -> failed; 5 exits over 60 s -> ok).

### Pitfall 14: Docker image or user environment
**What goes wrong:** image not pulled, user not in `docker` group, or docker daemon down produce opaque errors; running as root leaves root-owned files.
**How to avoid:** preflight `docker image inspect <pinned ref>` (exit 2 with the exact `docker pull` hint, never auto-pull), always `--user`, e2e asserts no file under cache/run/state is root-owned.

### Pitfall 15: Mount scope for arbitrary `--config`
**What goes wrong:** a `--config` YAML whose relative `external_components` or `!include` paths leave the mounted tree fails inside the container with a path-not-found.
**How to avoid:** mount the YAML's `git rev-parse --show-toplevel` (else the YAML's directory) at the same absolute path, read-only; the Phase 1 default example lives inside the repo so this is verified only for that case [VERIFIED: local `../components` resolved]. Fail with a message naming the unreachable path rather than widening the mount to `/`.

### Pitfall 16: Hash cache false hits
**What goes wrong:** skipping a build because only the YAML text is unchanged while a component source or the image changed.
**How to avoid:** D-15 hash = sha256 over: image ID; stage-1 `esphome config` stdout (includes substitutions, resolved packages, local external-component paths); contents of every file under each local `external_components` source dir; files named in `esphome.includes`/`includes_c`; the `sil` build-recipe version constant. Store `{hash, program_path}` in `build/sil-build.json`; require the binary to exist.

## Code Examples

### go-toml strict decoding with located errors
```go
// Source: spike /tmp/sil-spike/tomlspike (go-toml v2.4.3) — output verified
err := toml.NewDecoder(r).DisallowUnknownFields().Decode(&fan)
var sm *toml.StrictMissingError
if errors.As(err, &sm) {
    for _, e := range sm.Errors { row, col := e.Position(); /* "fan.toml:3:1: unknown field max_rmp" */ }
}
var de *toml.DecodeError
if errors.As(err, &de) { row, col := de.Position() }
```
Semantic validation errors (e.g. `start_duty < min_duty`) have no parser position: find the line of the offending key by scanning the file for `^\s*<key>\s*=` and report `file:line: message`.

### Supervisor firmware spawn
```go
// Source: spike /tmp/sil-spike/pdeath (verified: child dies on SIGKILL of the supervisor)
go func() {
    runtime.LockOSThread() // never unlocked; thread lives as long as this goroutine
    cmd := exec.Command(program)
    cmd.Env = append(os.Environ(), "ESPHOME_PREFDIR="+prefs, "SIL_BUS="+busPath)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
    cmd.Stdout, cmd.Stderr = logFile, logFile
    _ = cmd.Start()
    res <- cmd.Wait() // restart/guard logic runs in the supervisor loop, on a Clock
}()
```

### Static-link assertion
```go
f, _ := elf.Open(program)             // debug/elf
if f.Section(".interp") != nil { /* dynamically linked → BUILD-02 violation */ }
```

### e2e client inside the pinned image
```bash
# Source: experiment — works offline; prints entity states
docker run --rm --network host --entrypoint python3 -v "$PWD/e2e:/s:ro" \
  ghcr.io/esphome/esphome:2026.10.1 /s/api_client.py 6101 "$KEY"
```
Script uses `aioesphomeapi.APIClient("127.0.0.1", port, None, noise_psk=key)`, `fan_command(key, state=True, speed_level=40)`, `subscribe_states`.

### Codegen validations (verified messages)
- Missing channel: `'channel' is a required option for [sensor.sim_bus].`
- Long path: `sim_bus path is 130 bytes; sun_path limit is 107.` (`validate_path` in `components/sim_bus/__init__.py`).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `esphome: platformio_options: build_flags:` | `esphome: build_flags:` | deprecated 2026.10.1, removed 2026.12.0 | use the new key (STACK.md) |
| PlatformIO host build | ninja + machine gcc via `esphome/host/toolchain.py` | 2026.10 | build lives in `.pioenvs/<name>/program`, libs fetched as archives into `pio_components` [VERIFIED log: "Downloading 2 library archive(s)"] |
| `application.cpp` select timeout | `core/wake/wake_host.cpp::wakeable_delay` | 2026.10.1 | time-factor milestone only |
| Dynamic host binary | static (`-static`) | project decision | verified with encryption linked in |

**Deprecated/outdated:** `ota: esphome`/`wifi` on host (not usable), mDNS on host (stub; HA needs manual host:port + key, `sil up` should print both).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Arctic P12 `start_duty = 0.10`, `tau_up = 1.0 s`, `tau_down = 2.5 s` (datasheet has none of them; only 0 rpm < 5 % PWM and 200-1800 rpm are cited) | Pattern 8 | Model dynamics feel wrong; harmless to Phase 1 acceptance (relative behaviour only), but maintainer should confirm or measure later |
| A2 | Go may retire the OS thread that forked a `Pdeathsig` child, hence `LockOSThread` | Pattern 5, Pitfall 7 | Hardening only; both variants passed in the spike, so wrong = a few redundant lines |
| A3 | Control API on a Unix socket in Phase 1, TCP listener only in Phase 3 | Pattern 10 | If the maintainer wants `curl http://127.0.0.1:PORT` immediately, add a TCP listener (small); the `Control` interface makes it additive |
| A4 | Tach loss semantics: on bus disconnect the RPM sensor stops publishing and HA keeps its last value | Pattern 7 | Alternative is publishing NaN/unavailable; affects only the outage window; flag for Phase 2 HA verification |
| A5 | `go.yaml.in/yaml/v3` v3.0.6 parses `esphome config` output (not spiked); name from training knowledge | Standard Stack | If it chokes, `gopkg.in/yaml.v3` v3.0.1 is a drop-in; checkpoint before install covers the name risk |
| A6 | `flynn/noise` name/legitimacy beyond proxy existence | Package audit | Mitigated by `checkpoint:human-verify` and by having exercised it against real firmware |
| A7 | 60 s ready timeout and 20 s probe window are sensible | Pattern 4/5 | Spurious `up` failures on very slow hosts; tune later |
| A8 | Engine tick 10 ms and bus `in` rate <= 20 Hz per channel are adequate for a 1 s mean | Pattern 6/9 | Slightly noisy mean; both are constants to tune |
| A9 | Repo-root mount at the same absolute path is enough for all Phase 1 configs | Pitfall 15 | Only the default example was exercised |
| A10 | `sensor: pulse_counter` is not rejected by `esphome config` on host (probe was inconclusive) | Pattern 3 | If it is rejected, lint message is merely redundant |

## Open Questions

1. **Does the maintainer want the control API reachable over TCP in Phase 1?**
   - Known: criterion 2 only says "HTTP/JSON API"; `status` carries the encryption key.
   - Unclear: whether quick `curl` access outweighs local-user key exposure.
   - Recommendation: Unix socket now (A3); revisit at Phase 3.
2. **Which build mode does `sil up` use when the toolcache is empty?**
   - Known: first-ever build needs network for 2 library archives; afterwards fully offline.
   - Recommendation: allow network automatically for that one case and print "fetching noise-c/libsodium (one-time)" on stderr; refuse silently-offline surprises with the explicit error.
3. **Where does the example YAML's default resolve from when `sil` runs outside the repo?**
   - Recommendation: locate repo root by walking up for `go.mod` with the module path, or `SIL_REPO`; otherwise require `--config`.
4. **Does `sil status` need the LAN IP for the HA hint?**
   - Known: API binds 0.0.0.0; mDNS is a stub.
   - Recommendation: Phase 1 prints `127.0.0.1`; Phase 2 (HA) adds a LAN hint.
5. **Behaviour of `fan: speed` with `restore_mode` for the example.**
   - Recommendation: `restore_mode: ALWAYS_OFF` (as prototype) so fresh prefs and restored runs behave the same in Phase 1.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | all Go code | ✓ (`~/.local/bin`, not on default PATH) | 1.27.3 | export PATH in Makefile |
| Docker | build pipeline | ✓ (user in `docker` group) | 29.9.3 | — (blocking if absent) |
| `ghcr.io/esphome/esphome:2026.10.1` | build + e2e client | ✓ local | ID `sha256:eab8cd6b…` | — (no pull; exit 2 with hint) |
| g++ | C++ golden-vector test | ✓ | 12.2.0 (host), 14.2 (image) | run the test in the image |
| Network (first build only) | `noise-c`, `libsodium` archives | ✓ today | — | seeded `pio_components` |
| `XDG_RUNTIME_DIR` | run dir | ✓ `/run/user/1002` | — | `/tmp/sil-$UID` |
| jq, python3, uv | ad-hoc checks only | ✓ | — | not required by code |
| socat | manual bus inspection | ✗ | — | not required |
| ccache | speed | inside image | — | not required |

**Missing dependencies with no fallback:** none for this host.
**Missing dependencies with fallback:** socat (optional, debugging only).

## Security Domain

`security_enforcement` is enabled (`security_asvs_level: 1`).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | partial | Native API uses a per-instance 32-byte PSK (Noise); control API is not authenticated, protected by Unix socket permissions (0600 in a 0700 dir) |
| V3 Session Management | no | no sessions |
| V4 Access Control | yes | filesystem permissions: state/cache/run dirs 0700, key file 0600, sockets 0600 |
| V5 Input Validation | yes | instance-name regex, `fan.toml` strict decode + range validation, bus line grammar (<= 4096 B, flat), port range checks, `--config` path resolved before mount |
| V6 Cryptography | yes | `crypto/rand` for the key; Noise via `flynn/noise`; never hand-rolled |
| V7 Error handling/logging | yes | never log the key; redact it from build-log tails |
| V12 Files/resources | yes | no path from the bus or API is used to open files; run-dir paths built from the validated name only |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Another local user reads the API key from an open TCP status endpoint | Information disclosure | Unix-socket control API (A3); key file 0600 |
| API key visible in process listing (`docker run -s key …`) | Information disclosure | `!env_var` + `-e VAR` without value on argv |
| Instance name used to build paths/args (`../x`, `a;b`) | Tampering | strict regex; `exec.Command` with arg slices, never a shell |
| Malformed or oversized bus line from a rogue local process | DoS | listener socket in 0700 dir; 4 KiB line cap; drop connection on parse error; one active connection |
| Stray process holds the API port, sim "ready" against the wrong device | Spoofing | name+MAC check in Noise hello + HelloResponse |
| Firmware API bound to `0.0.0.0` on a LAN | Information disclosure / tampering | Noise encryption always on; docs warn; CI runners must firewall it (STACK.md) |
| Orphaned firmware keeps listening after the supervisor dies | DoS / resource | `Pdeathsig`, `Setpgid`, signal-handler shutdown path |
| Unpinned image drift | Tampering | image reference with digest; hash includes image ID |

## Sources

### Primary (HIGH confidence)
- Experiments on this host, 2026-11-08 (files in `/tmp/sil-spike`, not in the repo): static build with `api: encryption:` (`phase1.yaml`), cold/warm/no-op/incremental/offline timings (`build2.sh`), seeded-offline build, `config` and `compile --only-generate` outputs, banned-component matrix, `!env_var` test, Go Noise probe (`gospike/main.go`), busy-port behaviour, `Pdeathsig` spike (`pdeath/`), go-toml strict spike (`tomlspike/`), float formatting comparison (`fmt/`, `t_fmt.cpp`), aioesphomeapi-in-image run with fake sim.
- Image `ghcr.io/esphome/esphome:2026.10.1` source: `/entrypoint.sh`, `esphome/core/config.py:126-136,501`, `esphome/core/__init__.py:835`, `esphome/writer.py:688`, `esphome/const.py:9`, `esphome/host/toolchain.py`, `aioesphomeapi/_frame_helper/noise.py`.
- `proxy.golang.org` for module versions/dates/origins (kong, go-toml, flynn/noise, go.yaml.in/yaml/v3, x/crypto).
- Repo files read this session: `.planning/phases/01-closed-loop-fan-skeleton/01-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `.planning/ROADMAP.md`, `.planning/PROJECT.md`, `.planning/research/{SUMMARY,ARCHITECTURE,PITFALLS}.md`, `prototype/` (README, sim.yaml, components, tools), `.claude/CLAUDE.md`.

### Secondary (MEDIUM confidence)
- ARCTIC product page for P12 PWM PST (`https://arctic.de/it/P12-PWM-PST/ACFAN00120A`): "200—1800 rpm PWM-gesteuert (0 rpm unter 5 % PWM)", "Anlaufspannung: 3,9 V" — fetched; the PDF spec sheet was unreadable through the fetcher.
- ARCTIC A-RGB variant page (`https://www.arctic.de/en/P12-PWM-PST-A-RGB-0dB`): 200-2000 rpm (different product, cited only to exclude it).

### Tertiary (LOW confidence)
- TechPowerUp P12 PWM PST review measurements (215 RPM at 5 %, 1829 RPM at 100 %): taken from a web-search summary; the page returned HTTP 403 to the fetcher.

## Project Constraints (from CLAUDE.md)

From `/home/claude/esphome-sil/.claude/CLAUDE.md` (project) and `/home/claude/CLAUDE.md` (workspace):
- Go for simulator, CLI and server; single static binary (`CGO_ENABLED=0`); **no Python in the simulator** (Python allowed in product-repo/e2e test clients).
- ESPHome pinned to 2026.10.1 via `esphome/esphome:2026.10.1` image (pin the digest too); no auto-update.
- Locality: no cloud, no vendor dependencies at runtime; **no CDN** (applies to Phase 3, but vendor nothing remote now).
- Logging via `log/slog` to **stderr only**; stdout is reserved for the CLI's JSON result.
- Libraries per STACK.md: kong, stdlib `ServeMux`, go-toml/v2 strict, stdlib `testing` (no testify), no Cobra/Viper/Gin.
- Use `build_flags` under `esphome:`, not `platformio_options.build_flags`.
- No ESPHome `json:` component or anything that `add_library()`s in the SiL top-level.
- Prefs: `ESPHOME_PREFDIR` per run; unique `api: port:` and `host: mac_address:` per instance.
- `sil up` must warn that the native API is plaintext-bound to all interfaces if encryption is off (not applicable: encryption is mandatory here).
- License GPL-3.0; semver tags `0.x`; sim-bus, `fan.toml` schema internal.
- Follow GSD workflow for edits; work from `claude/main` (branch model `claude/*`), git user `claude-bot`; **never touch port 8001 (watermeter production) or other projects under `/home/claude`; do not push**.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions from the proxy, key libraries exercised in spikes (noise, go-toml); yaml lib not spiked (A5).
- Architecture: HIGH for build/run/handshake/orphan behaviour (reproduced); MEDIUM for the Go package split, protocol details and status shape (designed here).
- Pitfalls: HIGH — each listed failure mode was reproduced or read from the image source this session, except thread-retirement of `Pdeathsig` (A2) and `pulse_counter` rejection (A10).
- Fan constants: MEDIUM for range and 0-RPM threshold (manufacturer page), LOW for start threshold and time constants (A1).

**Research date:** 2026-11-08
**Valid until:** 2026-12-08 (stable; re-check if the pinned image digest changes or ESPHome 2026.12.0 bumps are adopted)

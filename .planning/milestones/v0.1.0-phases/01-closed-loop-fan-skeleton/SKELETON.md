# Walking Skeleton — esphome-sil

**Phase:** 1
**Generated:** 2026-11-09
**Built by:** a three-link tracer chain, each link led by a tracer task with its own end-to-end check: `01-01` (firmware seam: pinned-image static build -> program on the host -> sim-bus), `01-02` (closed loop through the Go engine and fan model), `01-03` (`sil up` / `sil status` / `sil down` in front of it). The skeleton's ~30 files do not fit one plan's 15-file budget, so the thinnest end-to-end path is built in three sequential links instead of one oversized task. Every later Phase 1 plan expands it without changing the decisions below.

## Capability Proven End-to-End

`sil up <name>` builds the repo example firmware in the pinned ESPHome image as a static host binary, runs it next to a detached `sil serve <name>` supervisor, an aioesphomeapi client sets Fan 1 speed to 40, the duty arrives over the sim-bus, the Go fan model turns it into RPM that flows back to the firmware's `Fan 1 Speed` sensor, `sil status` shows duty 0.4 and rising RPM, and `sil down <name>` leaves no process behind.

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Language / binary | Go 1.27.3, one static binary `bin/sil` (`CGO_ENABLED=0 go build -trimpath ./cmd/sil`), module `github.com/zeroflow/esphome-sil` | Project constraint (Go, no Python in the simulator, single static binary) |
| CLI grammar | `github.com/alecthomas/kong` v1.17.0; `sil up/status/down` + hidden `sil serve`; Phase 1 later adds `sil sim restart`, `sil fw restart` | STACK.md; struct-tag grammar, `env:` binding |
| Process model | `sil up` builds in the foreground, then spawns `sil serve <name>` detached (`Setsid`); serve = thin supervisor (firmware process with `Setpgid` + `Pdeathsig: SIGKILL`, control API, engine slot) + restartable engine goroutine | D-01, D-02; killing the supervisor kills the firmware |
| Firmware build | `docker run` of `ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64`, `--user uid:gid`, repo mounted read-only at its own absolute path, `ESPHOME_DATA_DIR=/build/data`, `HOME=/cache/home`, `-s sil_name/sil_mac/sil_port`, key via `SIL_API_KEY` env only | BUILD-01, Pattern 1 (verified); working tree stays clean |
| Firmware runtime | Static `program` (`esphome: build_flags: [-static]`) runs directly on the host with `ESPHOME_PREFDIR=<run>/prefs`, `SIL_BUS=<run>/bus.sock` | BUILD-02, BUILD-04 |
| Sim-bus | Unix domain socket `<run>/bus.sock`, newline-delimited flat JSON (string/number scalars, max 4096 B/line), simulator listens, firmware connects; channels named `portN.duty` (out) / `portN.rpm` (in), bound by `channel:` | BUS-01, IO-06; spec frozen in `protocol/PROTOCOL.md` by plan 01-06; Go `Frame.GetNum` returns the wire float32 as its shortest decimal so duty 0.4 reads as exactly 0.4 |
| Firmware components | ESPHome external component `components/sim_bus/` at the repo root (hub + `output` + `sensor` platforms), consumed via `external_components: type: local` | IO-01, IO-02; `github://zeroflow/esphome-sil@vX.Y` later (FW-03) |
| Simulator core | Single-writer engine actor (`internal/engine`), pure fan model (`internal/model`), fan library `fans/*.toml` embedded via `fans.FS` and loaded by `internal/fanlib` | ARCHITECTURE Pattern 1; MODEL-01/02 |
| Time | `internal/clock` is the only package allowed to read wall time; `clock.Real` (factor 1) in production, `clock.Manual` in tests; infrastructure timeouts also go through the clock (`clock.WithTimeout` for context/request timeouts, `Clock.Deadline` for socket deadlines); AST lint test bans wall-clock calls, context timers and standard-library timeout fields everywhere else | MODEL-04 |
| Control surface | HTTP/JSON `GET /api/v1/status`, `POST /api/v1/down` (later `/api/v1/sim/restart`, `/api/v1/fw/restart`) on the Unix socket `<run>/api.sock` (0600); `control.Control` interface, CLI is a client (except `up`/`down` orchestration) | CLI-06; Assumption A3 (TCP listener added in Phase 3 behind the same handler) |
| Status contract | `control.Status` schema 1: `schema, instance, phase, api{device_name,mac,host,port,key,ready}, firmware{pid,running,bus_connected,restarts,last_exit}, sim{time_s,time_factor,engine_restarts,bus_proto}, ports[]{port,fan,duty,rpm,state}, warnings[]` — fields are extended, never renamed | Phase 2/3 extend it |
| CLI output contract | stdout = exactly one JSON object per command; errors `{"error":{"code","message","detail"}}`; exit 0 ok / 1 user-fixable / 2 infrastructure; progress and slog on stderr; `--quiet` silences stderr | CLI-05, D-04 |
| Directory layout | identity `$XDG_STATE_HOME/sil/<name>/identity.json` (0600, key + sticky port), run dir `$XDG_RUNTIME_DIR/sil/<name>/` (fallback `/tmp/sil-$UID/<name>/`: `run.json`, `spec.json`, `bus.sock`, `api.sock`, `prefs/`, `build.log`, `firmware.log`, `sil.log`), build cache `$XDG_CACHE_HOME/sil/build/<name>/{build,cache}` | D-06, D-16 |
| Instance identity | name regex `^[a-z0-9]([a-z0-9-]{0,25}[a-z0-9])?$`, firmware name `sil-<name>`, friendly `SiL <name>`, MAC `02:` + first 5 bytes of `sha256("sil-mac:"+name)`, 32-byte random key (base64) | D-07, D-08, D-09 |
| Go package layout | `cmd/sil`, `internal/{apperr,cli,lifecycle,instance,fwbuild,supervisor,engine,bus,model,fanlib,clock,control,apiclient,lint,deps}` (`deps` is a temporary pin file, removed by 01-13), `fans/`, `components/sim_bus/`, `examples/fan-skeleton.yaml`, `protocol/`, `tests/cpp/`, `tests/fixtures/`, `e2e/` (build tag `sil_e2e`) | Package `fwbuild`, not `build`: the root `.gitignore` ignores every `build/` directory |
| Test strategy | `make check` (gofmt, vet, `go test -race ./...` incl. the wall-clock lint), `make test-cpp` (golden vectors in C++), `make e2e` (docker-backed end-to-end tests driving the real `sil` binary, the firmware directly, and aioesphomeapi inside the pinned image); e2e build cache per worktree (`$HOME/.cache/sil-e2e/<repo hash>` or `SIL_E2E_CACHE`) and a sticky free API port per instance name, so parallel executors never share build dirs, toolcache or ports | No testify, no golangci-lint |
| Deployment target | Local developer machine (Debian 12, glibc 2.36); documented run command `make build && bin/sil up dev` | Locality constraint, no cloud |

## Stack Touched in Phase 1

- [x] Project scaffold — `go.mod`, `Makefile` (`build`, `test`, `vet`, `fmt-check`, `check`, `e2e`, `test-pkg`), kong CLI, `.gitignore` (`/bin/`)
- [x] Routing — `GET /api/v1/status`, `POST /api/v1/down` on `api.sock`
- [x] Persistence — real write and read of `identity.json` (key, port), `run.json`, `spec.json`; firmware preferences in the per-run `prefs/`
- [x] Interactive element wired to the API — `sil status` / `sil down` through the control client; firmware fan command via the native API changes simulated duty and RPM
- [x] Local full-stack run command — `make build && bin/sil up dev && bin/sil status dev && bin/sil down dev`; automated as `make e2e E2E_RUN='TestFirmwareBus|TestLoop|TestTracer'` (one test per tracer link)

## Out of Scope (Deferred to Later Slices)

Inside Phase 1 (expansion plans, same architecture):
- Manual clock and deterministic engine tests (01-04)
- Hysteresis, `below_min`, strict `fan.toml` with located errors (01-05)
- Strict wire grammar, golden vectors, C++ codec (01-06); HELLO manifest, snapshots, resync, `sil sim restart` / `sil fw restart` (01-10)
- 1 s windowed RPM mean, output dedupe, codegen validation of `channel:` and socket path, `USE_*` guards (01-07)
- Two-stage lint, image preflight, build-hash skip, toolcache, static-ELF assertion, `--config` mount scope (01-08)
- Sticky port allocation, port test-bind and holder, instance locks, liveness, real Noise handshake readiness (01-09)
- Auto-restart with crash-loop guard, orphan-proof shutdown, `sil down` fallback (01-11); name resolution and final CLI contract (01-12); exit-class registry, dependency tidy and full acceptance run (01-13)

Later phases (not Phase 1):
- Buttons, LED strip with `partition`, temperature/humidity, per-port fan choice, `sil set/press/watch/logs`, Rev 3.3 example with HIL names, contract and smoke tests (Phase 2)
- Embedded web UI, WebSocket feed, TCP listener for the control API (Phase 3)
- `sil rebuild`, `sil down --forget`, `sil list`, time factor, scenarios (v2)

## Subsequent Slice Plan

Each later phase adds one vertical slice on top of this skeleton without altering its architectural decisions:

- Phase 2: the full Rev 3.3 IO surface (buttons, LED strip, temperature/humidity) driven from the CLI (`set/press/watch/logs`) and from the user's own Home Assistant, guarded by contract tests
- Phase 3: a live local web page served from `sil` that is a pure client of the same control API, with a parity check

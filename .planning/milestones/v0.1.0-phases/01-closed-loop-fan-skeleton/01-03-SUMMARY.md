---
phase: 01-closed-loop-fan-skeleton
plan: 03
subsystem: cli-lifecycle
tags: [go, kong, cli, supervisor, unix-socket, control-api, xdg, detach, pdeathsig, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 01)
    provides: "fwbuild.Build, sim_bus firmware component, e2e harness helpers"
  - phase: 01-closed-loop-fan-skeleton (plan 02)
    provides: "engine.New/Run/Snapshot, fanlib, kong v1.17.0 pinned, wall-clock lint"
provides:
  - "bin/sil with up/status/down and hidden serve; stdout is exactly one JSON value, errors are {error:{code,message,detail}} with exit 1 (user-fixable) or 2 (infrastructure)"
  - "internal/control: Status schema 1, Control interface, HTTP handler (GET /api/v1/status, POST /api/v1/down), Unix-socket Client"
  - "internal/instance: name validation, XDG paths, identity.json (0600 key, sticky port), run.json, spec.json, ResetRunDir"
  - "internal/supervisor: sil serve - firmware process group owner, engine slot, control API on api.sock"
  - "internal/lifecycle: Up (build, detach, wait for ready) and Down, RepoRoot"
  - "e2e Env helpers (newEnv, Sil, Up, UpAutoPort, Status, WaitStatus, APIClient) and TestTracer, TestCLIErrors"
affects: [01-04, 01-05, 01-07, 01-08, 01-09, 01-10, 01-11, 01-12, 01-13]

actuals:
  tokens: 16900
  tasks: 2
  commits: 3
plan_head_before: ad7a9ca4a5655f90f1aedcaf8c7a3f9230d7aa5b
plan_head_after: 47431e88269153ee1f8e6891820c3728549ba831

tech-stack:
  added: [github.com/alecthomas/kong v1.17.0 (now imported; its blank import in internal/deps/pin.go is removed)]
  patterns:
    - "Detach by re-exec: sil up builds in the foreground, then starts os.Executable() `serve <name>` with Setsid, stdio to sil.log, and polls the control socket"
    - "Firmware spawned from a LockOSThread goroutine with Setpgid + Pdeathsig SIGKILL; one stopAll path (down, signal, startup failure) stops the firmware group first, then the engine"
    - "Control API over a 0600 Unix socket; the CLI reads engine state only through control.Client"
    - "run.json is written only after the control socket listens; atomic tmp + rename for all state files"
    - "Error contract: *apperr.Error everywhere; Main prints one JSON object and maps the exit class"

key-files:
  created:
    - cmd/sil/main.go
    - internal/cli/cli.go
    - internal/control/control.go
    - internal/instance/instance.go
    - internal/instance/instance_test.go
    - internal/lifecycle/lifecycle.go
    - internal/supervisor/supervisor.go
    - e2e/tracer_test.go
  modified:
    - .gitignore
    - e2e/harness_test.go
    - internal/deps/pin.go

key-decisions:
  - "D-02 engine and listener ownership: the engine is a goroutine in `sil serve` with fresh state per engine.New (no child process). engine.Run owns bus.Listen and the socket unlink on the bus path; the supervisor owns the lifecycle of both through the engine slot (start, cancel, wait for Run to return) together with the firmware process group. Consequence for 01-10: a sim restart drops listener, connection and engine state and re-listens on the same path while the firmware PID stays."
  - "The engine runs on its own background context, not the signal context, so shutdown can stop the firmware group before the engine goes away"
  - "control gained Listen and ServeListener next to Serve so the supervisor can write run.json strictly after the socket listens; Serve is Listen + ServeListener"
  - "Graceful http Shutdown on a clock timer finishes the response to POST /api/v1/down before the process exits"
  - "Status phase: starting until the bus is connected, ready while running and connected, failed on an unexpected firmware exit or a missing process, stopping during shutdown; api.ready equals bus connected until 01-09 adds the Noise probe"
  - "Kong parse errors report error code `usage` (exit 1); the hidden `serve` prints nothing on stdout because its stdout is sil.log"

requirements-completed: [BUILD-04, BUILD-07, CLI-05, CLI-06]

coverage:
  - id: D1
    description: "sil up builds the example in the pinned image, starts a detached supervisor, waits until the firmware is on the bus and prints one JSON object (instance, api.{host,port,key,device_name,mac}, run_dir, build); progress goes to stderr"
    requirement: "BUILD-04"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTracer#TestTracer"
        status: pass
    human_judgment: false
  - id: D2
    description: "Identity and run dir: identity.json 0600 in a 0700 dir, device sil-<name>, MAC 02:+sha256 derivation, run dir recreated on every up, 0700 sockets, refusal of a symlinked or foreign-mode run-dir parent"
    requirement: "BUILD-04"
    verification:
      - kind: unit
        ref: "internal/instance/instance_test.go#TestLoadOrCreate"
        status: pass
      - kind: unit
        ref: "internal/instance/instance_test.go#TestResetRunDirRefusesUnsafeParent"
        status: pass
    human_judgment: false
  - id: D3
    description: "An aioesphomeapi client setting Fan 1 speed 40 makes sil status show ports[0].duty == 0.4 and rpm > 0, and the firmware Fan 1 Speed sensor rises above 0; fans 2-4 at 20/60/100 show duty 0.2/0.6/1.0"
    requirement: "CLI-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTracer#TestTracer and TestTracer/four_ports"
        status: pass
    human_judgment: false
  - id: D4
    description: "sil down stops firmware and supervisor (both PIDs gone) and later sil status exits 1 not_running; killing the supervisor with SIGKILL takes the firmware down (Pdeathsig)"
    requirement: "BUILD-07"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTracer#TestTracer (PIDs gone after down)"
        status: pass
      - kind: other
        ref: "manual smoke: kill -9 <serve pid>, firmware pid gone within 1 s"
        status: pass
    human_judgment: false
  - id: D5
    description: "Failing commands print one {error:{code,message}} object on stdout, exit 1 for user-fixable errors, no JSON on stderr, and leave no state behind"
    requirement: "CLI-05"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestCLIErrors#TestCLIErrors"
        status: pass
    human_judgment: false
  - id: D6
    description: "Control API is reachable only on a 0600 Unix socket in the 0700 run dir; sil status goes through control.Client"
    requirement: "CLI-06"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN=TestTracer#TestTracer"
        status: pass
    human_judgment: true
    rationale: "Socket modes were observed in a manual smoke (api.sock and bus.sock 0600, run and state dirs 0700) but there is no automated mode assertion yet; the TCP listener decision (assumption A3) is flagged for maintainer review"

duration: 10min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 03: CLI Lifecycle and Supervisor Summary

**`sil up`, `sil status` and `sil down` now front the closed loop: `up` builds the pinned-image firmware, detaches a `sil serve` supervisor that owns the firmware process group and the engine, and waits until the firmware is on the bus; `status` is answered over a 0600 Unix socket and `down` leaves no process behind, even when the supervisor is SIGKILLed.**

## Performance

- **Duration:** about 10 min
- **Completed:** 2026-11-09
- **Tasks:** 2 (1 tracer, 1 auto), plus one hardening commit
- **Files:** 8 created, 3 modified; 3 commits (2 tasks, 1 fix)
- **Measured `sil up` time:** 1.4 s wall with a warm build cache (`smoke`, second run in the same cache); 2.0 s in `TestTracer` with a warm cache; 6.8 s to 7.2 s for the first build of a new instance name (docker compile). Full run of `TestTracer|TestCLIErrors|TestLoop|TestFirmwareBus` took 31 s.

## Accomplishments

- Walking skeleton complete: `make build && bin/sil up dev && bin/sil status dev && bin/sil down dev` works; the tracer e2e proves it automatically (up, native-API fan command, status duty 0.4 and RPM above 0, firmware sensor above 0, down, PIDs gone, status `not_running`).
- Process model per D-01/D-02: `up` detaches with `Setsid`; the supervisor starts the firmware from a `LockOSThread` goroutine with `Setpgid` and `Pdeathsig: SIGKILL`; one `stopAll` path (down request, SIGTERM/SIGINT/SIGHUP, startup failure) sends SIGTERM to the firmware group, waits 3 s on a clock timer, then SIGKILLs the group, then cancels the engine and waits for `Run` to return.
- Control API v1 with the `Status` schema 1 contract, an HTTP handler that maps `*apperr.Error` to 4xx/5xx with the `{"error":...}` body, and a client that maps them back to exit classes.
- CLI contract: kong grammar, one JSON value per command, `--quiet`, text or JSON slog on stderr only, `SIL_INSTANCE` binding for the name of `status` and `down`.
- Identity per D-06..D-09: sticky port (default 6054, `--api-port` overrides and persists), 32-byte random key, derived MAC, `sil-<name>` and `SiL <name>`.

## Task Commits

1. **Task 1: Tracer, sil up -> detached supervisor -> sil status over api.sock -> sil down** - `6f05fd5` (feat)
2. **Hardening: refuse an unsafe run-dir parent** - `b50543d` (fix, see deviation 1)
3. **Task 2: Four fan ports through sil and the CLI error contract** - `47431e8` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## D-02 engine and listener ownership

The engine is a goroutine in `sil serve` with fresh state per `engine.New` (no child process). `engine.Run` owns `bus.Listen` and the socket unlink on the bus path, while the supervisor owns the lifecycle of both through the engine slot (start, cancel, wait for `Run` to return) together with the firmware process group. Consequence for 01-10: a sim restart drops listener, connection and engine state and re-listens on the same path while the firmware PID stays.

## Files Created/Modified

- `cmd/sil/main.go` - entry point, `os.Exit(cli.Main(os.Args[1:]))`
- `internal/cli/cli.go` - kong grammar, slog setup, JSON stdout, exit codes
- `internal/control/control.go` - Status schema 1, Control, handler, `Listen`/`ServeListener`/`Serve`, `Dial`/`Client`
- `internal/instance/instance.go` (+ `instance_test.go`) - names, XDG paths, identity, run.json, spec.json, `ResetRunDir`, `ensurePrivateDir`
- `internal/supervisor/supervisor.go` - `sil serve`
- `internal/lifecycle/lifecycle.go` - `Up`, `Down`, `RepoRoot`
- `e2e/harness_test.go` - TestMain builds `sil`; `Env` helpers
- `e2e/tracer_test.go` - `TestTracer` (+ `four_ports`), `TestCLIErrors`
- `.gitignore` (`/bin/`), `internal/deps/pin.go` (kong blank import removed)

## Decisions Made

See `key-decisions`. The D-02 separation choice is recorded above under its own heading as the plan requires.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] Run-dir parent was not verified (security review finding)**
- **Found during:** Task 1 commit (automated security review of `instance.go`)
- **Issue:** the D-06 fallback `/tmp/sil-$UID/<name>` is created in a world-writable directory; another local user could pre-create `/tmp/sil-$UID` or plant a symlink, and `api.sock` serves the API key. The plan's `ResetRunDir` used `MkdirAll` + `Chmod` without checking ownership.
- **Fix:** `ensurePrivateDir` on the directory above the run dir: create with `Mkdir 0700`, then `Lstat` and require a real directory, owned by the current user, mode 0700, else `run_dir_unsafe` (exit 2); the run dir and `prefs/` are created with `Mkdir` (no `MkdirAll`).
- **Files modified:** internal/instance/instance.go; tests added in internal/instance/instance_test.go (loose-mode parent and symlink parent are refused)
- **Verification:** `make check` passes with the new unit tests
- **Committed in:** b50543d

**2. [Rule 2 - Missing critical] `already_running` guard in Up**
- **Found during:** Task 1 design
- **Issue:** the plan wipes the run dir on every `sil up`; a second `up` of a live instance would delete its sockets and `run.json` under a running supervisor. The research pattern (Pattern 2) specifies exit 1 `already_running`; 01-09 hardens locking.
- **Fix:** `Up` returns Failed `already_running` when `run.json` names a live PID; a stale `run.json` with a dead PID is overwritten.
- **Files modified:** internal/lifecycle/lifecycle.go
- **Verification:** observed in a manual probe (second `up` of a running instance prints `already_running`, exit 1)
- **Committed in:** 6f05fd5

**3. [Rule 3 - Blocking] `control.Listen` and `ServeListener` added next to `Serve`**
- **Found during:** Task 1 (supervisor)
- **Issue:** the plan requires `run.json` to be written only after the control socket accepts, but `Serve(ctx, sockPath, h)` gives no signal when it is listening.
- **Fix:** `Serve` is now `Listen` + `ServeListener`; the supervisor calls them separately. The contract names in `<interfaces>` are unchanged.
- **Files modified:** internal/control/control.go
- **Committed in:** 6f05fd5

**4. [Plan wording] No `Process.Release()` after the serve spawn**
- **Issue:** a goroutine calls `cmd.Wait` instead, so a serve that dies while `Up` is polling is detected (`serve_exited` with the `sil.log` tail) and a long-lived caller leaves no zombie. The CLI process exits right after printing, so the detached supervisor is re-parented as intended.
- **Files modified:** internal/lifecycle/lifecycle.go

**5. [Additions] Small unplanned pieces**
- `usage` error code for kong parse errors, and `sil: <code>: <message>` one-liner on stderr for failed commands (a text line, never JSON).
- `instance_test.go` unit tests (names, MAC, identity modes and stickiness, unsafe parent).
- When `--config` is given outside any checkout, the config's own directory becomes the read-only mount root (01-08 owns the real mount-scope rules).

---

**Total deviations:** 3 auto-fixed (2 missing critical, 1 blocking), 2 small plan-wording/addition notes. **Impact:** none on scope; 1 and 2 close local-safety gaps the plan left to 01-09.

## Issues Encountered

- A hand-written probe of the unsafe-parent check was wrong (it exercised `<XDG_RUNTIME_DIR>/sil`, which sil creates itself) and started a real `probe` instance; it was stopped with SIGTERM and verified gone. The check is now covered by a proper unit test.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: stale_run_json | internal/instance/instance.go | After a supervisor SIGKILL, `run.json` remains; `sil status` reports `control_unreachable` and `sil down` returns that error until the next `sil up` overwrites the stale file (the firmware is already gone through Pdeathsig). Orphan-proof shutdown and the `sil down` fallback are 01-11 (Pitfall 12). |

Plan threat register: T-01-01 (name regex before any path or argv use, argument slices only), T-01-03 (control API only on `api.sock`, 0600; no TCP listener), T-01-06 (Setpgid + Pdeathsig from a locked thread; shutdown path stops the firmware first; verified by `kill -9` of the supervisor in a manual smoke), T-01-27 (identity.json 0600 in a 0700 dir by tmp + rename; key only in `cmd.Env` for docker; log tails redacted) are mitigated. T-01-30 is accepted as planned.

## Known Stubs

None. `api.ready` equals bus-connected until 01-09 adds the Noise probe (documented in code and the plan).

## User Setup Required

None - no external service configuration required.

## Needs Human

- Carried over from 01-02 (still pending, not done by this run): maintainer provenance review of https://github.com/flynn/noise and https://github.com/yaml/go-yaml.
- Assumption A3 (control API on a Unix socket only in Phase 1) is resolved in the plan but remains flagged for a maintainer glance.
- Nothing blocked this plan.

## Next Phase Readiness

Wave 4 plans can expand the packages without changing the `<interfaces>` contracts. Stable names: `control.{Status,Control,NewHandler,Listen,ServeListener,Serve,Dial}`, `instance.{...}`, `lifecycle.{Up,Down,RepoRoot}`, `supervisor.Serve`, `cli.Main`, e2e `Env`. Hooks left for later plans: `recordExit` in the supervisor (01-11 auto-restart and crash-loop guard), the engine slot (`engCancel`/`engDone`, 01-10 restart), `api.ready` (01-09 Noise probe), `resolve` in cli.go (01-12 name resolution), `LoadOrCreate` (01-09 port allocation), `deps/pin.go` (remaining blank imports for yaml/v3 and flynn/noise).

## Self-Check: PASSED

- All 8 created files exist on disk; commits `6f05fd5`, `b50543d`, `47431e8` present in `git log`.
- `make check` passes (gofmt, vet incl. e2e tag, `go test -race ./...` including the wall-clock lint over the new packages and the new instance tests).
- `make e2e E2E_RUN='TestTracer|TestCLIErrors|TestLoop|TestFirmwareBus'` prints `--- PASS: TestTracer`, `--- PASS: TestTracer/four_ports`, `--- PASS: TestCLIErrors`, `--- PASS: TestLoop`, `--- PASS: TestFirmwareBus`.
- Acceptance greps match: `Pdeathsig` and `LockOSThread` in supervisor.go, `Setsid` in lifecycle.go, `/bin/` in .gitignore; `bin/sil --help` lists up, status and down (serve hidden).
- No `sil serve` or firmware processes left after the test runs.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

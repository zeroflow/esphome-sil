---
phase: 01-closed-loop-fan-skeleton
plan: 02
subsystem: simulator-core
tags: [go, sim-bus, unix-socket, engine, fan-model, go-toml, ast-lint, go-modules]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 01)
    provides: "firmware seam: sim_bus component, pinned-image static build, e2e harness, internal/clock"
provides:
  - "internal/bus: flat NDJSON Frame codec (decimal-normalized GetNum, raw GetF32) and Unix-socket Server (newest connection wins, never-blocking Send)"
  - "internal/engine: single-writer actor, 10 ms clock ticks, atomic.Pointer Snapshot, welcome/in frames stamped with sim-time microseconds"
  - "internal/model (thin Step), internal/fanlib, fans/arctic-p12-pwm-pst.toml embedded via fans.FS"
  - "internal/lint: repo-wide wall-clock lint (time functions, context timers, stdlib timeout fields) with inline-fixture self-test"
  - "go.mod pinning kong v1.17.0, go-toml/v2 v2.4.3, yaml/v3 v3.0.6, flynn/noise v1.1.0, x/crypto v0.58.0 plus internal/deps/pin.go"
  - "e2e TestLoop: real firmware closed loop against the in-process engine"
affects: [01-03, 01-04, 01-05, 01-06, 01-08, 01-09, 01-10, 01-13]

actuals:
  tokens: 9700
  tasks: 2
  commits: 2
plan_head_before: cd4541e4801c783064862ce27c34bee26185f394
plan_head_after: 09c47ef50720bb81031ad298c686a00128236d4d

tech-stack:
  added: [github.com/pelletier/go-toml/v2 v2.4.3, github.com/alecthomas/kong v1.17.0 (pinned only), go.yaml.in/yaml/v3 v3.0.6 (pinned only), github.com/flynn/noise v1.1.0 (pinned only), golang.org/x/crypto v0.58.0 (indirect)]
  patterns:
    - "Engine as a value: all model state owned by the Run goroutine, bus callbacks only enqueue to an inbox, observers read an immutable Snapshot"
    - "Bus Send never blocks: per-connection writer goroutine behind a 1024-frame queue; a full queue is an error, not a stall"
    - "Wire floats are float32 shortest decimals; GetNum re-expresses them so exact == duty assertions hold"
    - "Wall-clock lint is pure AST with import-alias resolution and local-shadow detection via ast.Ident.Obj"

key-files:
  created:
    - internal/bus/codec.go
    - internal/bus/server.go
    - internal/engine/engine.go
    - internal/model/fan.go
    - internal/fanlib/fanlib.go
    - fans/embed.go
    - fans/arctic-p12-pwm-pst.toml
    - internal/lint/wallclock_test.go
    - internal/deps/pin.go
    - e2e/loop_test.go
    - go.sum
  modified:
    - go.mod

key-decisions:
  - "bus.Frame.Num omits the field for NaN/Inf (no wire form), per Pitfall 9"
  - "bus.Server.Send is queue-based and non-blocking so a stalled firmware cannot freeze the engine goroutine; the plan's Send(f) error signature is unchanged"
  - "Handler.Disconnected fires only for a connection that ended while still the active one; a replaced connection produces no Disconnected, the new Connected follows"
  - "Lint also flags dot imports of net, net/http and os/exec (field checks cannot see through them), beyond the plan's time/context dot-import rule"

requirements-completed: [BUS-01, IO-01, IO-02, MODEL-04]

coverage:
  - id: D1
    description: "Real firmware closes the loop through the Go engine: Fan 1 speed 40 gives engine Ports[0].Duty == 0.4 exactly and RPM > 0 within 10 s, and the firmware's Fan 1 Speed rises above 0"
    requirement: "IO-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestLoop|TestFirmwareBus'#TestLoop"
        status: pass
    human_judgment: false
  - id: D2
    description: "Sim-bus server listens on a Unix socket (0600), engine returns nil from Run on context cancel and unlinks the socket; firmware connect/hello/welcome handshake works"
    requirement: "BUS-01"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestLoop|TestFirmwareBus'#TestLoop"
        status: pass
    human_judgment: false
  - id: D3
    description: "RPM from the fan model flows back as in frames on portN.rpm and shows up as the firmware's Fan 1 Speed sensor"
    requirement: "IO-02"
    verification:
      - kind: e2e
        ref: "make e2e E2E_RUN='TestLoop|TestFirmwareBus'#TestLoop (watch shows Fan 1 Speed peak 776 rpm)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Engine steps the model only on injected-clock ticks and stamps every frame it sends with t = sim time in microseconds; a repo-wide AST lint fails on wall-clock calls, context timers and stdlib timeout fields outside internal/clock"
    requirement: "MODEL-04"
    verification:
      - kind: unit
        ref: "internal/lint/wallclock_test.go#TestLintSelfTest"
        status: pass
      - kind: unit
        ref: "internal/lint/wallclock_test.go#TestNoWallClock"
        status: pass
    human_judgment: true
    rationale: "Lint is proven; the Manual-clock engine determinism test belongs to 01-04, and the flagged EDGE-MODEL-04-unclassified assumption still needs manual edge review"
  - id: D5
    description: "Phase dependency set pinned at exact versions, checksum-verified, GOSUMDB on, proxy Origin URLs match the expected upstreams"
    verification:
      - kind: other
        ref: "make mod-verify (all modules verified) and go list -m -json origin check"
        status: pass
    human_judgment: true
    rationale: "Automated provenance passed; maintainer review of flynn/noise and go.yaml.in/yaml/v3 is pending (see Needs Human)"

duration: 20min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 02: Closed Loop With the Go Simulator Summary

**A single-writer Go engine listens on the Unix sim-bus, steps an exponential-lag fan model on injected-clock ticks with Arctic P12 parameters from an embedded TOML library, and turns the real firmware's duty into RPM that returns to its Fan 1 Speed sensor; a repo-wide AST lint now bars wall-clock use outside internal/clock and every phase dependency is pinned and checksum-verified.**

## Performance

- **Duration:** about 20 min
- **Completed:** 2026-11-09T00:54Z
- **Tasks:** 2 (1 tracer, 1 auto)
- **Files:** 11 created (incl. go.sum), 1 modified (go.mod); 2 task commits
- **Measured:** `make e2e E2E_RUN='TestLoop|TestFirmwareBus'` about 20-26 s wall (warm image); TestLoop engine showed port 1 duty exactly 0.4 and 356 rpm about 1.3 s after the command, firmware Fan 1 Speed peaked at 776 rpm (model target for duty 0.4 is about 789 rpm).

## Accomplishments

- Tracer proven on real firmware: duty crosses the Unix socket, `GetNum` returns exactly 0.4 (not 0.4000000059604645), `model.Step` produces RPM, `in portN.rpm` frames return it, and the firmware's native-API sensor reports it.
- Engine is a self-contained value: `New(cfg).Run(ctx)` listens, closes `Ready`, publishes immutable `Snapshot`s via `atomic.Pointer`, and on cancel closes the listener, unlinks `bus.sock` and returns nil.
- Wall-clock lint covers the nine `time` functions, four `context` timer constructors, `net.DialTimeout`, `http.TimeoutHandler` and the Client/Server/Transport/Dialer/Cmd timeout fields (literal and assignment forms), through default names, aliases, dot imports and method values. Verified quiet on clean code and loud on a planted probe file in the real tree.
- Dependencies pinned: kong, go-toml/v2, yaml/v3, flynn/noise, x/crypto at exact versions; `make mod-verify` prints `all modules verified`.

## Task Commits

1. **Task 1: Tracer, firmware duty to engine to fan model to RPM and back** - `4550ed4` (feat)
2. **Task 2: Wall-clock lint and pinned, provenance-checked modules** - `09c47ef` (feat)

**Plan metadata:** committed separately (docs: complete plan).

## Module provenance (Task 2, automated)

| Check | Result |
|-------|--------|
| `go env GOSUMDB` | `sum.golang.org` |
| GONOSUMDB, GONOSUMCHECK, GOPRIVATE, GOFLAGS, GOINSECURE | all empty; nothing disables the checksum DB |
| GOPROXY | `https://proxy.golang.org,direct` |
| `go.yaml.in/yaml/v3@v3.0.6` Origin.URL | `https://github.com/yaml/go-yaml` (ref `refs/tags/v3.0.6`, hash `5a62aba6`) |
| `github.com/flynn/noise@v1.1.0` Origin.URL | `https://github.com/flynn/noise` (ref `refs/tags/v1.1.0`, hash `4d9f71cd`) |
| `golang.org/x/crypto@v0.58.0` Origin.URL | `https://go.googlesource.com/crypto` (ref `refs/tags/v0.58.0`, hash `8fdf07b3`) |
| `go list -deps` of noise, yaml/v3, kong | succeeds (go.sum complete) |
| `make mod-verify` | `all modules verified` |
| Licenses seen in module cache | flynn/noise: BSD-style (trademark notice + redistribution terms); yaml/v3: MIT and Apache dual notice |

All three origins match the expected upstreams, so no stop was needed. The maintainer provenance review of flynn/noise and go.yaml.in/yaml/v3 is **pending, not done** (see Needs Human).

## Files Created/Modified

- `internal/bus/codec.go`, `internal/bus/server.go` - Frame codec and Unix-socket server
- `internal/engine/engine.go` - actor engine, snapshot, tick loop, rpm pacing
- `internal/model/fan.go` - pure `Initial`/`Step` (thin; 01-05 completes semantics)
- `internal/fanlib/fanlib.go`, `fans/embed.go`, `fans/arctic-p12-pwm-pst.toml` - fan library; strict TOML (unknown keys rejected), start_duty/tau_up/tau_down marked ASSUMED in `source`
- `internal/lint/wallclock_test.go` - lint and inline-fixture self-test (no testdata directory)
- `internal/deps/pin.go` - `sil_pin`-tagged blank imports, removed by 01-13
- `e2e/loop_test.go` - `TestLoop`
- `go.mod`, `go.sum`

## Decisions Made

See `key-decisions` above. In short: NaN/Inf omitted on the wire; Send is queue-based and non-blocking; replaced connections do not emit Disconnected; lint additionally rejects dot imports of net, net/http, os/exec.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Lint walk handed the file path to the parser as source text**
- **Found during:** Task 2, first run of TestNoWallClock
- **Issue:** `checkSource(rel, path)` passed the path string as the `src` argument of `parser.ParseFile`, so every file failed with "expected 'package'". Self-test (inline fixtures) was unaffected.
- **Fix:** read the file with `os.ReadFile` and pass the bytes.
- **Files modified:** internal/lint/wallclock_test.go
- **Verification:** TestNoWallClock passes on the real tree; a planted `time.Now` probe file under `internal/zz_probe` made it fail with file:line, then was removed.
- **Commit:** 09c47ef (never committed in the broken form)

**2. [Rule 2 - Missing critical] Lint dot-import rule extended to net, net/http, os/exec**
- **Found during:** Task 2 design
- **Issue:** the timeout-field and function checks match qualified names, so a dot import of net/http or net/os/exec would silently bypass them (only time and context dot imports were specified).
- **Fix:** dot imports of those three packages are findings too.
- **Files modified:** internal/lint/wallclock_test.go
- **Commit:** 09c47ef

**Total deviations:** 2 auto-fixed (1 bug, 1 missing critical). **Impact:** none on scope; the second tightens MODEL-04 coverage.

### Process note

`TDD_APPLICABLE` was false for this plan, so Task 2 (marked `tdd="true"` in the plan) was implemented as one commit: the lint checker lives in the test file itself, so a separate failing-test commit has no meaning. The fixture self-test was written alongside the checker and the real-tree probe confirmed the lint can fail.

## Issues Encountered

None beyond the first lint-walk bug above.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: local_ipc_socket | internal/bus/server.go | `bus.Listen` creates the socket with a umask-dependent mode first and chmods it 0600 right after `Listen` returns; a brief window exists in which another local user could connect if the parent directory is not 0700. Mitigated in practice by 01-03 placing the socket in a 0700 run dir (e2e uses a fresh `/tmp/sil-*` MkdirTemp 0700 dir). Not a plan T-01-26 gap, noted for 01-03. |

T-01-26 mitigations present: per-connection Scanner buffer capped at `MaxLine` (an over-long line ends the connection), one active connection (newest replaces older), socket chmod 0600.

## Known Stubs

None. `model.Step` is intentionally thin (documented in code and plan; 01-05 adds hysteresis, below_min and snap-to-zero behind the same signature), and the `welcome`/`in` frames omit `sync` and manifest handling until 01-06/01-10.

## User Setup Required

None.

## Needs Human

- **Pending maintainer provenance review (morning item):** open https://github.com/flynn/noise and https://github.com/yaml/go-yaml and confirm they are the expected upstream projects. Automated checks (table above) passed; this run does not claim the human review happened.
- Edge assumption `EDGE-MODEL-04-unclassified` (injectable clock, dt integration, sim timestamps, lint) remains flagged for manual edge review; Manual-clock engine test is 01-04.
- Optional (from 01-01): decide in 01-07 whether the hub `path` default should be removed.

## Next Phase Readiness

Ready for 01-03 (CLI/supervisor in front of the engine). Stable names: `engine.{New,Config,PortConfig,Snapshot,PortState}`, `bus.{Listen,Server,Handler,Frame}`, `fanlib.{Load,DefaultFan,Source}`, `make test-pkg PKG=... RUN=...`. 01-03, 01-08 and 01-09 should replace the matching blank imports in `internal/deps/pin.go` as they import kong, yaml/v3 and flynn/noise for real.

## Self-Check: PASSED

- All 12 key files exist on disk (`[ -f ]` checked); commits `4550ed4` and `09c47ef` present in `git log`.
- `make check` (gofmt, vet incl. e2e tag, `go test -race ./...` including the lint) passes; `make e2e E2E_RUN='TestLoop|TestFirmwareBus'` prints `--- PASS: TestLoop` and `--- PASS: TestFirmwareBus`; `make mod-verify` prints `all modules verified`.
- Acceptance greps match: `FormatFloat` and `GetF32` in codec.go, `model.Step` and `NewTicker` in engine.go, `WithDeadlineCause`/`ReadHeaderTimeout`/`WaitDelay` in the lint test, all four pinned versions plus go-toml v2.4.3 in go.mod; `internal/lint/testdata` does not exist.

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

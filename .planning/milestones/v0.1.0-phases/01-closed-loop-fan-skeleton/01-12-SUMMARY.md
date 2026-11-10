---
phase: 01-closed-loop-fan-skeleton
plan: 12
subsystem: cli
tags: [go, kong, cli-contract, instance-resolution, json, exit-codes, e2e]

requires:
  - phase: 01-closed-loop-fan-skeleton (plan 03)
    provides: "kong grammar, apperr exit classes, control client, e2e Env harness"
  - phase: 01-closed-loop-fan-skeleton (plan 10)
    provides: "sim restart / fw restart commands and control client calls"
  - phase: 01-closed-loop-fan-skeleton (plan 11)
    provides: "instance.LoadRunFile / ProcessAlive liveness semantics, dead-supervisor run.json"
provides:
  - "resolveInstance: positional name, else SIL_INSTANCE, else the single live instance under the runtime base; none or several -> instance_ambiguous (exit 1) with detail.candidates sorted ([] when empty)"
  - "cli.Execute(args, stdout, stderr): one JSON writer (marshal fully, then write once); every failure incl. kong parse errors is {error:{code,message,detail}} on stdout; human text only on stderr; --help goes to stderr"
  - "global --quiet: drops progress lines and slog; success leaves stderr empty"
  - "status / sim restart / fw restart as pure control-API clients; dead PID -> not_running detail {instance, phase: dead, run_dir} (exit 1); live PID with unreachable socket -> control_unreachable (exit 2); 4xx control answer -> exit 1, 5xx -> exit 2"
  - "e2e TestCLIContract against real instances"
affects: [01-13]

actuals:
  tokens: 9970
  tasks: 2
  commits: 3
plan_head_before: b5c9c2817a914c0958f8813faac23f45c023502b
plan_head_after: 8ffe2043fe25550760f945672fa8b2d965ad3a5d

tech-stack:
  added: []
  patterns:
    - "Execute(args, stdout, stderr) beside Main(args): the CLI is testable with captured streams and a fake control server (control.Serve on a temp unix socket), no firmware"
    - "run.json decides whether an instance is live, never where to connect: the client dials <run dir>/api.sock derived from the instance name"
    - "Unit tests fake a supervisor with `sh -c 'sleep 120 & wait' serve <name>` so instance.ProcessAlive's /proc cmdline check passes without a real supervisor"

key-files:
  created:
    - internal/cli/resolve.go
    - internal/cli/cli_test.go
    - e2e/cli_test.go
  modified:
    - internal/cli/cli.go
    - e2e/tracer_test.go

key-decisions:
  - "`sil status` exits 0 whenever the control API answers, including phase failed (e.g. firmware_crash_loop): status reports state, the failure is data in phase/error. Resolves the open item from 01-11"
  - "--help output goes to stderr (stdout is JSON only); it is the one invocation that prints no JSON value on stdout (exit 0)"
  - "--quiet drops progress and slog but a failing command still echoes its one-line `sil: code: message` on stderr; the key is never on stderr or in any error path (T-01-25)"
  - "Per-command env:SIL_INSTANCE kong tags removed; resolveInstance owns the precedence (positional > SIL_INSTANCE > single live instance), an empty SIL_INSTANCE counts as unset"
  - "The client dials p.APISock() (own run dir) instead of run.json's api_sock, and refuses a runtime base that is not a real 0700 directory owned by the caller (unsafe_runtime_dir, exit 2): read-only counterpart of sil up's ensurePrivateDir for the world-reachable /tmp/sil-$UID fallback"
  - "Open item from 01-09 (sticky stored --api-port that turned out taken) needed no CLI change: it is already port_in_use (exit 2, never silently moved) in lifecycle.Up; the explicit --api-port flag remains the way to move it"

patterns-established:
  - "Error shape for parse failures is identical to runtime failures, so a consumer needs one decoder"

requirements-completed: [CLI-05, CLI-06]

coverage:
  - id: D1
    description: "Instance resolution: positional > SIL_INSTANCE > single live instance; none -> instance_ambiguous with candidates []; several -> candidates sorted; dead-PID leftovers do not count"
    requirement: CLI-05
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestResolveInstance, TestNoInstanceIsAmbiguousJSON"
        status: pass
      - kind: e2e
        ref: "e2e/cli_test.go#TestCLIContract"
        status: pass
    human_judgment: false
  - id: D2
    description: "Exactly one JSON value on stdout for every outcome (success, runtime error, kong parse error), human text only on stderr, --quiet leaves stderr empty on success, struct field order"
    requirement: CLI-05
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestParseErrorsAreJSON, TestQuiet, TestHelpStaysOffStdout, TestCommandsAreControlClients"
        status: pass
      - kind: e2e
        ref: "e2e/cli_test.go#TestCLIContract, e2e/tracer_test.go#TestCLIErrors"
        status: pass
    human_judgment: false
  - id: D3
    description: "status, sim restart and fw restart are pure control-API clients (GET status, POST sim/restart with gap_s, POST fw/restart) verified against a fake control server"
    requirement: CLI-06
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestCommandsAreControlClients, TestRunFileDoesNotChooseTheSocket"
        status: pass
    human_judgment: false
  - id: D4
    description: "Exit classes: dead PID -> exit 1 not_running phase dead; live PID without a control socket -> exit 2 control_unreachable; control 4xx -> 1, 5xx -> 2 with the same error JSON; unsafe runtime base -> exit 2"
    requirement: CLI-05
    verification:
      - kind: unit
        ref: "internal/cli/cli_test.go#TestStatusFailureClasses, TestControlErrorsKeepTheirExitClass, TestUnsafeRuntimeBaseIsRefused"
        status: pass
      - kind: e2e
        ref: "e2e/cli_test.go#TestCLIContract (kill -9 of the supervisor)"
        status: pass
    human_judgment: false

duration: ~25 min
completed: 2026-11-09
status: complete
---

# Phase 1 Plan 12: CLI Contract Summary

**Every `sil` invocation now prints exactly one JSON value (success, runtime error and kong parse error alike) with exact exit classes, resolves its instance from positional name, `SIL_INSTANCE` or the single live instance (`instance_ambiguous` with sorted candidates otherwise), and `status` / `sim restart` / `fw restart` are proven pure control-API clients against a fake control server.**

## Performance

- **Duration:** about 25 min (start time not recorded at the start of the run; estimated)
- **Completed:** 2026-11-09T03:16Z
- **Tasks:** 2
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments

- `internal/cli/resolve.go`: `resolveInstance` with the plan's precedence; the live check is `instance.ProcessAlive` over each `<runtime base>/sil/*/run.json`, candidates sorted and never nil (`[]` on the wire).
- `internal/cli/cli.go`: `Execute(args, stdout, stderr)` is the single output path (marshal fully, write once, so an unencodable value never leaves half a value); kong parse errors become `invalid_argument` (exit 1); global `--quiet`; `connect()` maps a dead supervisor to `not_running` with `detail {instance, phase: "dead", run_dir}`.
- `internal/cli/cli_test.go`: 10 test functions drive `Execute` with temp XDG dirs, a stand-in supervisor process (`serve <name>` in its cmdline) and `control.Serve` on a temp socket: resolution, 4xx/5xx exit mapping, dead PID, unreachable socket, parse errors, `--quiet`, help-off-stdout, key never in error paths.
- `e2e/cli_test.go` `TestCLIContract`: real instances `e2e-cli-a/b/c`: empty candidates, sorted candidates, `SIL_INSTANCE`, positional beats env, single live instance after `down b`, `up --quiet` prints nothing on stderr, unknown flag is `invalid_argument`, `kill -9` of a's supervisor gives `not_running` phase dead and a is no longer counted live.

## Task Commits

1. **Task 1: Instance resolution, one-JSON writer, --quiet and client-only commands (TDD-style unit tests against a fake control server)** - `b3ec551` (feat)
   - follow-up hardening - `d2c5fc0` (fix): non-private runtime base refused, client dials its own `api.sock`
2. **Task 2: CLI contract e2e against real instances** - `8ffe204` (test)

**Plan metadata:** the `docs(01-12)` commit that adds this SUMMARY, STATE.md and ROADMAP.md follows.

## Files Created/Modified

- `internal/cli/resolve.go` - instance resolution, runtime-base scan, read-only private-dir check
- `internal/cli/cli.go` - `Execute`, one-JSON writer, `--quiet`, `connect()`, client-only status/restart commands
- `internal/cli/cli_test.go` - CLI tests with a fake control server
- `e2e/cli_test.go` - `TestCLIContract`
- `e2e/tracer_test.go` - `TestCLIErrors` row for "status without a name" now expects `instance_ambiguous`

## Decisions Made

See `key-decisions` above. The two that consumers will notice: `status` exits 0 for `phase: failed` (read `phase`/`error` in the JSON), and `--help` prints to stderr only.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] TestCLIErrors expected the replaced error code**
- **Found during:** Task 1
- **Issue:** `e2e/tracer_test.go` (not in the plan's file list) asserts `instance_required` for `sil status` without a name; the plan replaces that code with `instance_ambiguous`.
- **Fix:** one-word change of the expected code in that table row.
- **Files modified:** e2e/tracer_test.go
- **Verification:** `make e2e E2E_RUN='TestCLIContract|TestCLIErrors|TestTracer'` all PASS
- **Committed in:** b3ec551

**2. [Rule 2 - Missing critical] Client trusted run.json and the runtime base too much**
- **Found during:** Task 1 (post-commit security review of resolve.go)
- **Issue:** instance scanning and the dial path came from files under `/tmp/sil-$UID` (world-reachable fallback); `sil up` guards that directory, the client side did not, and the client dialed the `api_sock` path run.json names.
- **Fix:** `checkPrivateDir` (Lstat: real directory, own uid, mode 0700) before scanning and in `connect()` (`unsafe_runtime_dir`, exit 2); the client dials `p.APISock()`.
- **Files modified:** internal/cli/resolve.go, internal/cli/cli.go, internal/cli/cli_test.go
- **Verification:** `TestUnsafeRuntimeBaseIsRefused`, `TestRunFileDoesNotChooseTheSocket`
- **Committed in:** d2c5fc0

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 missing critical)
**Impact on plan:** no scope creep. A second reviewer note (a missing runtime base returns nil instead of failing) is by design: nothing is trusted when the directory is absent, later reads fail as `not_running` / `instance_ambiguous`, and exploiting the check-then-read window would need a hostile local user to win a directory-creation race and also run a live `sil serve <name>` process.

## Issues Encountered

- One full `make e2e` run reported `TestRestartEitherOrder` failing (31 s instead of the usual 7 s) with its output filtered away. It passed alone, in the restart group (twice) and in a second full run (all 34 tests PASS, 274 s). Not reproduced and nothing in this plan touches the restart path beyond `connect()`; treated as a timing flake under load. If it recurs, capture the full `-v` output of that test.

## Needs Human

None.

## User Setup Required

None - no external service configuration required.

## Known Stubs

None.

## Threat Flags

None. T-01-25 holds: the key appears only in `status`/`up` JSON on stdout (test asserts it is absent from stdout and stderr of every error path); no new modules (`go.mod`/`go.sum` unchanged).

## Next Phase Readiness

- 01-13 (exit-class registry and final acceptance) can rely on the codes introduced here: `instance_ambiguous` (1), `invalid_argument` (1, parse errors), `not_running` with `phase: dead` (1), `control_unreachable` (2), `unsafe_runtime_dir` (2, now also from status/restart), `runtime_dir_unreadable` (2).
- Registry note: `status` exit 0 with `phase: failed` is intentional (see decisions).

## Self-Check: PASSED

- Files present: internal/cli/resolve.go, internal/cli/cli_test.go, e2e/cli_test.go
- Commits present: b3ec551, d2c5fc0, 8ffe204
- `grep -n instance_ambiguous internal/cli/resolve.go` matches; `make check` 112 passed; `make e2e` (full) PASS

---
*Phase: 01-closed-loop-fan-skeleton*
*Completed: 2026-11-09*

---
phase: 03-live-web-ui
plan: 04
subsystem: web
tags: [go, control-api, operation-table, parity, chrome, cdp]

requires:
  - phase: 03-live-web-ui
    provides: "03-01 web.NewHandler and /ws; 03-02 browser harness (cdptest Requests, bench, openPage); 03-03 web.Limit, web.Serve, web.LimitBody"
provides:
  - "control.Operations: the control API as a 12-row table; control.NewHandler (api.sock) and control.NewLANHandler (web) are built from it"
  - "control.MatchOperation / OperationByName: request-to-row matching through a ServeMux built from the table"
  - "TestOperationsTable, TestOperationsCLI, TestParityRequests, TestSnapshotEqualsStatus, TestParityDisplayedValues, TestParityPageState"
  - "ui scans in make check: TestEmbedContents, TestNoExternalRefs, TestNoUnsafeDOM"
  - "uitest helpers for later page plans: pageOps, uiOnlyText, fmtOracle, lookup, diffPaths, sessionSteps/play, sweepData, observerJS"
affects: [03-05, 03-06, 03-08, 03-10, 03-11]

actuals:
  tokens: 13300
  tasks: 2
  commits: 2
plan_head_before: 15d0f94e07ed84c7bc81aee4c6e6be34d444474a
plan_head_after: 14afa8d228dd4f7ad7193cc3f6fac81b598a45b3
commits: 2

tech-stack:
  added: []
  patterns:
    - "one table, three consumers: socket handler, LAN handler and the CLI/page checks all read control.Operations; a listener's policy is the row's LAN flag, never a route registered elsewhere"
    - "parity by independent oracle: fmtOracle re-implements the UI-SPEC formats in Go, so a formatter bug in view.js cannot cancel itself out"
    - "text-change observer with a named allow-list (uiOnlyText) and an in-test self-check that proves the rule fires"

key-files:
  created:
    - internal/control/operations.go
    - internal/cli/operations_test.go
    - internal/supervisor/web_test.go
    - ui/ui_test.go
    - uitest/parity_test.go
    - .planning/phases/03-live-web-ui/deferred-items.md
  modified:
    - internal/control/control.go
    - internal/control/control_test.go
    - internal/web/web.go

key-decisions:
  - "Operation rows keep an unexported handler field, so only package control can add a served route; the ws row is the only row without one (checked at init and in TestOperationsTable)"
  - "The refusal message names the command twice (`sil down` ... run `sil down` on this machine) and the detail is {operation, cli}; the old {operation, listener} detail is gone with the hard-coded refusals"
  - "MatchOperation asks a ServeMux built once from the table instead of re-implementing wildcard matching"
  - "state step of the parity session starts from a bench whose port 2 is stopped, so 'state -> running' is a real change"

patterns-established:
  - "New API operation = new row in control.Operations with its LAN flag; TestOperationsTable, TestOperationsCLI and TestParityRequests then cover it"
  - "A page element whose text changes without data-src must be added to uiOnlyText by name with a reason"

requirements-completed: [UI-06, UI-01]

duration: 35min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "Every operation is one row of control.Operations; api.sock serves all json and ndjson rows, the web listener serves exactly the LAN rows and answers each other row 403 operation_not_allowed naming its sil command without calling the Control; no route exists outside the table"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "internal/control/control_test.go#TestOperationsTable"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/lifecycle_refused"
        status: pass
    human_judgment: false
  - id: D2
    description: "Every row's CLI field is a working sil invocation that selects the command its leading words name"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "internal/cli/operations_test.go#TestOperationsCLI"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every request the page makes (HTTP and WebSocket) is same-origin and is a page file or a LAN row with a CLI command; every operation in pageOps is requested"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "uitest/parity_test.go#TestParityRequests"
        status: pass
    human_judgment: false
  - id: D4
    description: "The first /ws frame minus t, k, seq equals GET status; socket and TCP status differ in api.key alone"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "internal/supervisor/web_test.go#TestSnapshotEqualsStatus"
        status: pass
    human_judgment: false
  - id: D5
    description: "Every data-src element shows the UI-SPEC format of its status path after each step of a scripted session; only data-src elements and the named allow-list (#link, #message) change text"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
    human_judgment: false
  - id: D6
    description: "After the session the page store deep-equals GET status"
    requirement: UI-06
    verification:
      - kind: automated
        ref: "uitest/parity_test.go#TestParityPageState"
        status: pass
    human_judgment: false
  - id: D7
    description: "First-party page files hold no external URL and no HTML-string DOM API or code-from-string call; both scans run in make check"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "ui/ui_test.go#TestNoExternalRefs, TestNoUnsafeDOM, TestEmbedContents"
        status: pass
    human_judgment: false
---

# Phase 3 Plan 04: Operation Table and Value Parity Summary

**The control API is now one table (`control.Operations`, 12 rows) that builds both the api.sock handler and the LAN handler (D-19 and D-20 as the rows' LAN flag) and names the `sil` command of every operation; four automated parity checks plus two first-party asset scans make any drift between the page and the CLI/API fail a test.**

## Performance

- **Duration:** about 35 min
- **Tasks:** 2 (tracer, test-only TDD task), 2 task commits
- **Files:** 9 (6 created including deferred-items.md, 3 modified)
- `make check` 119 passed (twice); `make test-ui` all 8 tests PASS; `make e2e E2E_RUN='TestWebTracer|TestCLIContract'` PASS (lifecycle_refused included).

## Final Operations rows

| Name | Method Path | Transport | CLI | LAN |
|------|-------------|-----------|-----|-----|
| status | GET /api/v1/status | json | `status` | true |
| down | POST /api/v1/down | json | `down` | false |
| sim.restart | POST /api/v1/sim/restart | json | `sim restart` | false |
| fw.restart | POST /api/v1/fw/restart | json | `fw restart` | false |
| press | POST /api/v1/buttons/{id}/press | json | `press usr1` | true |
| button.down | POST /api/v1/buttons/{id}/down | json | `press usr1 --down` | true |
| button.up | POST /api/v1/buttons/{id}/up | json | `press usr1 --up` | true |
| set | POST /api/v1/set | json | `set temperature=22` | true |
| fans | GET /api/v1/fans | json | `fans` | true |
| watch | GET /api/v1/watch | ndjson | `watch` | true |
| logs | GET /api/v1/logs | json (ndjson with follow=1) | `logs` | false (D-20) |
| watch.ws | GET /ws | ws | `watch` | true |

## Accomplishments

- `internal/control/operations.go`: `Operation`, `Operations`, `NewLANHandler`, `MatchOperation`, `OperationByName`, and the eleven handler constructors (`opStatus` ... `opLogs`) moved verbatim out of `NewHandler`. An `init` check panics on duplicate names or patterns and on a handler that is missing exactly when the transport is not `ws`. `NewHandler` is a loop over the table; `grep -c 'HandleFunc("'` outside comments in control.go is 0.
- `internal/web/web.go`: `/api/v1/` is `control.NewLANHandler(redacting{c})`; the three hard-coded refusals and `refuse` are removed. Logs (tail and follow) are now refused on the LAN as well (D-20).
- `TestOperationsTable` exercises every row through both handlers with a counting fake: socket serves all non-ws rows, LAN serves exactly `status,press,button.down,button.up,set,fans,watch,watch.ws`, refused rows return 403 `operation_not_allowed` with zero Control calls and the command named, unknown routes and the ws row 404/405, and `MatchOperation` maps wildcard paths.
- `TestOperationsCLI` parses each row's CLI string with the grammar Execute builds.
- `TestParityRequests` (Chrome): requests of the page and a reconnect are all same-origin, `/ws` and `/api/` ones are LAN rows with a CLI, the rest are files of `ui.FS()`; pageOps `watch.ws` seen through the WebSocket entry.
- `TestSnapshotEqualsStatus` on a real engine (`watchSup`): snapshot minus t, k, seq == TCP status; socket status key is real, TCP's is `<redacted>`, otherwise equal; no second frame with the Manual clock still.
- `TestParityDisplayedValues`: 20 data-src elements swept against `fmtOracle` before the session and after each of 9 steps (duty, rpm 640.5, state, fan, button, env, leds, firmware down, firmware up); a MutationObserver counts text changes and flags any element that is neither data-src nor on `uiOnlyText` (`#link`, `#message`); a built-in self-check changes a header label and requires exactly that element to be reported.
- `TestParityPageState`: `getState()` of the page store equals GET status after the same session, printing the differing JSON paths.
- `ui/ui_test.go`: embed contents plus the two scans, each checked against a synthetic tree that contains violations (and a vendor/ subtree that must be skipped), so neither can pass by finding nothing.

## Mutation checks (the tests are not vacuous)

Run once against the final tests and reverted: `rpm` formatted with `Math.floor` fails TestParityDisplayedValues (640 vs 641) at every step; a timer rewriting a header label fails it with the element path; `env` event stored as `v + 1` fails TestParityPageState at `.env.temperature`; `pageOps` containing `set` fails TestParityRequests ("page operation set was never requested").

## Task Commits

1. **Task 1 (tracer): operation table, LAN handler, CLI mapping, page requests matched in Chrome** - `12e4327` (feat)
2. **Task 2: value parity tests and first-party scans** - `14afa8d` (test)

## TDD Gate Compliance

Task 2 (`tdd="true"`, plan `type: execute`) adds only tests over behaviour 03-01 and 03-02 already implement, and touches no production file, so there is no GREEN commit and a RED that fails for lack of an implementation cannot exist. The substitute evidence is the four mutation checks above: each production-side break makes the matching test fail. One `test(03-04)` commit. No `check tdd-red-evidence` record was persisted.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug in own test] Read timeout detection in TestSnapshotEqualsStatus**
- **Found during:** Task 2
- **Issue:** coder/websocket reports its own `context canceled` when a read context times out, so checking the error (or `ctx.Err()`) for DeadlineExceeded failed; `clock.WithTimeout` stores the deadline as the context cause.
- **Fix:** `clock.TimedOut(rctx)`.
- **Files modified:** internal/supervisor/web_test.go
- **Commit:** 14afa8d

**2. [Interpretation] Refusal text and detail**
- The refusal message is "`sil X` is only available on the instance's control socket: run `sil X` on this machine" with detail `{operation, cli}` (the plan's wording). The earlier detail key `listener: "tcp"` is dropped. e2e `lifecycle_refused` only checks the code and the backticked command and still passes.

**3. [Interpretation] `state` step**
- The plan's "state port 2 -> running" is a no-op on a bench whose ports already run; `liveBench` therefore starts port 2 as `stopped`.

**Total deviations:** 3 (1 bug in a new test, 2 interpretations). **Impact:** none on interfaces.

## Authentication Gates

None.

## Issues Encountered

- Pre-existing flake, not caused by this plan: `internal/web` `TestWSPingReaps` (03-03) fails intermittently with `want a going-away close` (reproduced with `-count=15`, and identically on commit 15d0f94 in a scratch worktree). `make check` passed on both full reruns. Logged in `.planning/phases/03-live-web-ui/deferred-items.md`; not fixed (out of scope).

## Known Stubs

None.

## Threat Flags

None. The LAN surface shrank (logs refused); no new endpoints, no new dependencies.

## Threat model

| Threat ID | Disposition | Status |
|-----------|-------------|--------|
| T-03-07 routes outside the policy | mitigate | NewHandler and NewLANHandler register only rows; TestOperationsTable |
| T-03-08 UI-only behaviour drifting from the CLI | mitigate | TestParityRequests, TestParityDisplayedValues, TestParityPageState, TestSnapshotEqualsStatus |
| T-03-09 logs from the LAN | mitigate | logs row LAN false, 403 on the web listener |
| T-03-10 external references in assets | mitigate | TestNoExternalRefs, TestNoUnsafeDOM in make check |

## Next Phase Readiness

- Later page plans (03-08, 03-10, 03-11) append the operations they use to `pageOps` and extend `sessionSteps`/the request session; any element whose text changes without data-src needs a named `uiOnlyText` entry.
- 03-05 wraps the web handler from outside (Guard); the LAN handler it wraps is `NewLANHandler` behind `web.NewHandler`, signature unchanged.
- `TestNoExternalRefs` flags any `http://` or `https://` in first-party files, SVG namespaces included; use `data:` or namespace-free markup in the page.

## Self-Check: PASSED

- Files exist: internal/control/operations.go, internal/cli/operations_test.go, internal/supervisor/web_test.go, ui/ui_test.go, uitest/parity_test.go (all FOUND).
- Commits exist: 12e4327, 14afa8d (FOUND).
- Acceptance: `range Operations` in operations.go and control.go; 0 inline `HandleFunc("` in control.go; `NewLANHandler` in web.go; TestOperationsTable, TestOperationsCLI, TestSnapshotEqualsStatus, TestEmbedContents, TestNoExternalRefs, TestNoUnsafeDOM `ok`; TestParityRequests, TestParityDisplayedValues (20 elements), TestParityPageState `--- PASS`; TestWebTracer/lifecycle_refused PASS.

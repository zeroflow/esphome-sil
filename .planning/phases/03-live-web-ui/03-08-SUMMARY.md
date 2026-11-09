---
phase: 03-live-web-ui
plan: 08
subsystem: ui
tags: [vanilla-js, fetch, set, fans, verbatim-errors, dirty-fields, headless-chrome, parity]

requires:
  - phase: 03-live-web-ui
    provides: "03-04 control.Operations table, parity helpers (pageOps, uiOnlyText, fmtOracle); 03-06 stylesheet tokens, port panel top row, bottom strip; 03-02 browser harness and bench; set and fans operations from Phase 2"
provides:
  - "ui/web/js/api.js: OPS table (fans, set), call(), ApiError carrying only the server's words"
  - "ui/web/js/message.js: showError/clearMessage, the 5 s server-text message line (title, role alert)"
  - "ui/web/js/inputs.js: per-port fan select (initFanSelects, onFans), ENVIRONMENT fields and APPLY VALUES (initEnv), renderInputs"
  - "markup: select.fan per port panel (data-fmt select), #temperature/#humidity (data-fmt num), #apply; CSS dirty and error classes"
  - "uitest: bench Fans/Set, TestFanSelect, TestFanSelectError, TestFansFetchError, TestEnvSet, TestEnvError, TestEnvDirty, TestErrorLine; ui: TestJSOpsInOperations; e2e: TestWebFanSelect"
affects: [03-09, 03-10, 03-11, 03-12]

actuals:
  tokens: 11500
  tasks: 2
  commits: 3
plan_head_before: 533a8517f87435c44eb6e42554f68f810c1c003c
plan_head_after: 7477187839b90d178c3161d53863d7f8ae8346a4
commits: 3

tech-stack:
  added: []
  patterns:
    - "one OPS table in api.js of [method, path] pairs, each a LAN row of control.Operations (TestJSOpsInOperations) and each exercised by the parity session (pageOps)"
    - "a control shows what the server reports, never what was asked: the fan select keeps the user's choice only while the request is in flight (pending), then follows ports[i].fan"
    - "form controls are painted by inputs.js, not by the data-src textContent sweep; dirty/focus protection and a `following` set for the field whose commit just succeeded"
    - "the message line has one writer (showError) and no text of its own: JSON error message, else the response body text, else nothing"

key-files:
  created:
    - ui/web/js/api.js
    - ui/web/js/inputs.js
    - ui/web/js/message.js
    - uitest/inputs_test.go
  modified:
    - ui/web/index.html
    - ui/web/app.css
    - ui/web/js/app.js
    - ui/web/js/view.js
    - uitest/bench_test.go
    - uitest/parity_test.go
    - uitest/live_test.go
    - ui/ui_test.go
    - e2e/web_test.go

key-decisions:
  - "A non-JSON error shows its trimmed response body, or nothing when the body is empty, as the plan (D-17) says; UI-SPEC UIC-29 and Component 7 still describe an `HTTP {status} {statusText}` line, which the plan supersedes. A rejected fetch shows nothing and does not outline the field (LINK reports the connection)"
  - "On a successful commit the field takes the value in the set response's `applied` echo (the server's parsed number) and then follows the server state, so it neither flashes the old value nor stays stale while focused"
  - "Enter on a field that is not dirty does nothing; APPLY VALUES with no dirty field does nothing"
  - "A field that is only focused is protected from WS values while focused and takes the server value on blur (blur handler), so a half-seen update is never lost"
  - "The message line is no longer cleared by a new snapshot: a refusal stays its 5 s even if the stream reconnects in between"
  - "bench FailNext and enter key operations case-insensitively, so FailNext(\"set\") and the recorded `Set` call agree"

patterns-established:
  - "New page request = new entry in OPS (a LAN Operations row), a name in pageOps and a step in the parity session"
  - "Input-like elements are skipped by view.js's data-src text sweep and owned by inputs.js; the connecting rule (`--` as value) and sweepData (value of INPUT/SELECT) already cover them"
  - "Tests that arm a transport failure wrap the real guard+handler stack in a test-only handler (newFlakySite) instead of changing the bench"

requirements-completed: [UI-02, UI-04, UI-06]

coverage:
  - id: D1
    description: "Each port has a native fan select (aria-label `Fan on port N`) filled once per connect from GET /api/v1/fans (text the library name, value and title the id); changing it sends one POST /api/v1/set at once and the select then shows the server-reported fan"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestFanSelect"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebFanSelect"
        status: pass
    human_judgment: false
  - id: D2
    description: "A failed fan change shows the API message verbatim in the message line, outlines that select in red for 5 s and reverts it; a failed fans fetch leaves each select with only its current fan, disabled, and the error in the message line"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestFanSelectError"
        status: pass
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestFansFetchError"
        status: pass
    human_judgment: false
  - id: D3
    description: "TEMPERATURE and HUMIDITY inputs (inputmode decimal, right aligned, 96 px) rest at the server value; Enter commits only that field and APPLY VALUES all dirty fields, each as one set with the text as typed, never per keystroke; the controls are disabled unless LINK is LIVE"
    requirement: UI-04
    verification:
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestEnvSet"
        status: pass
      - kind: automated_ui
        ref: "uitest/live_test.go#TestConnectingShowsDashes"
        status: pass
    human_judgment: false
  - id: D4
    description: "Dirty or focused fields are never overwritten by a WS value; Escape reverts to the server value; a refused commit outlines the field named by error.detail.key (every committed field when none is named) and keeps its text until the field is edited again"
    requirement: UI-04
    verification:
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestEnvDirty"
        status: pass
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestEnvError"
        status: pass
    human_judgment: false
  - id: D5
    description: "The message line shows only server text, verbatim, for 5 s (newer error replaces and restarts the timer; title holds the whole text; role alert while showing); non-JSON body shown trimmed, empty body shows nothing"
    requirement: UI-04
    verification:
      - kind: automated_ui
        ref: "uitest/inputs_test.go#TestErrorLine"
        status: pass
    human_judgment: false
  - id: D6
    description: "Every request the page makes goes through one OPS table whose entries are LAN rows of control.Operations; pageOps holds watch.ws, fans and set, each requested in the parity session; displayed values still match GET status including the new select and inputs"
    requirement: UI-06
    verification:
      - kind: unit
        ref: "ui/ui_test.go#TestJSOpsInOperations"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityRequests"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
    human_judgment: false
  - id: D7
    description: "The new controls sit well in the instrument front panel (visual fit of the ENVIRONMENT group and the select in the top row)"
    requirement: UI-04
    verification: []
    human_judgment: true
    rationale: "Look and feel is taste; geometry (44 px controls, no overflow at 1280, 1024-, 640- and 390-wide layouts) is covered by TestLayoutDesktop/Narrow/Phone, and a 1280x720 screenshot was checked during execution"

duration: 10min
completed: 2026-11-09
status: complete
---

# Phase 3 Plan 08: Fan Select, Environment Fields and Verbatim Errors Summary

**The page's first write path: a native fan select per port and TEMPERATURE/HUMIDITY fields with APPLY VALUES, all through one OPS table of existing LAN operations, with the API's own error words in the message line.**

## Performance

- **Duration:** about 10 min (2026-11-09T14:21:55Z to 14:31:58Z)
- **Tasks:** 2 (a tracer and a TDD task)
- **Files:** 4 created, 9 modified (13 including live_test.go and ui_test.go beyond the plan's list, see Deviations)

## Accomplishments

- Tracer (task 1): `api.js` (OPS, `call`, `ApiError`), `message.js`, and the fan select. GET /fans once per connection fills the options; a change sends one `set {"portN.fan": id}` with no confirmation; the select keeps the choice only while in flight and then follows the server; a refusal shows the server message, outlines the select for 5 s and reverts it. The tracer was verified end to end against a real Rev 3.3 instance (`TestWebFanSelect`) before the expansion task began.
- ENVIRONMENT group (task 2, TDD: RED `f325232`, GREEN `7477187`): inputs with dirty/focus protection, Enter/APPLY VALUES/Escape, per-field red outline from `error.detail.key`.
- Parity: `pageOps` is now `watch.ws, fans, set`, the session performs a fan change from the select, `fmtOracle` knows `select`, and `TestJSOpsInOperations` ties the OPS table to `control.Operations`.

## Task Commits

1. Task 1 (tracer): `0bbe716` feat(03-08): fan select per port - api wrapper with verbatim errors, message line, set and fans through one OPS table
2. Task 2 RED: `f325232` test(03-08): RED - temperature and humidity fields ...
3. Task 2 GREEN: `7477187` feat(03-08): temperature and humidity fields ...

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestOfflineLoadAndLive read the fan through textContent**
- **Found during:** Task 2 (full `make test-ui`; it had passed after task 1 only because the check ran before the fan list arrived)
- **Issue:** `[data-src="ports.0.fan"]` is now a select, whose textContent is the concatenated option names once the library is loaded, so the old expectation (the fan id as text) became a race.
- **Fix:** the test waits for the select's `value` to equal the fan id.
- **Files modified:** uitest/live_test.go (not in the plan's file list)
- **Commit:** 7477187

**2. [Rule 3 - Blocking] bench failure keys were case-sensitive**
- **Found during:** Task 1
- **Issue:** the plan fixes `FailNext("set")`/`FailNext("fans")`, while existing bench operations record `Status`, `Watch`; a recorded `Set` could not be armed by `"set"`.
- **Fix:** `enter` and `FailNext` lower-case the key. Existing callers are unaffected.
- **Files modified:** uitest/bench_test.go
- **Commit:** 0bbe716

### Plan-directed choices worth flagging

- UI-SPEC (UIC-29, Component 7) still says a non-JSON failure shows `HTTP {status} {statusText}`. The plan (revision 1, D-17) replaces that with the body text or nothing, and that is what is built. The UI-SPEC text is stale on this point.
- `TestEmbedContents` in ui/ui_test.go now also lists api.js, inputs.js and message.js.

**Total deviations:** 2 auto-fixed (1 bug, 1 blocking). **Impact:** none on behaviour; both are test-side.

## Authentication Gates

None.

## Verification Results

- `make check` (fmt-check, vet, race tests, C++ codec test): pass (its last line: 119 passed, 0 failed).
- `make test-ui` (whole suite, run twice after the last change): pass, including TestLayoutDesktop/Narrow/Phone with the new controls and TestConnectingShowsDashes with the env inputs.
- `make test-pkg PKG=./ui/ RUN=TestJSOpsInOperations`: ok.
- `make e2e E2E_RUN='TestWebFanSelect|TestWebTracer'`: pass.
- Acceptance greps: `"/api/v1/set"` in api.js, `aria-label` in inputs.js, `inputmode="decimal"` in index.html: all match.

## Known Stubs

None added by this plan. The existing broken-windows entry 2 (the empty `.scope` well per port panel, filled by 03-10) is unchanged and still open.

## Threat Flags

None. The plan adds no endpoint; T-03-24 (server strings through textContent and attributes only, enforced by `TestNoUnsafeDOM` in `make check`), T-03-26 (OPS table checked by `TestJSOpsInOperations` and `TestParityRequests`) are covered; T-03-25 is accepted as planned (the page validates nothing).

## Issues Encountered

None. All planned tests passed on their first GREEN run apart from a test-side helper bug (WaitFor cannot use `await`; replaced by an async poll in `waitStore`).

## Next Phase Readiness

Ready for the next plan of the phase. New operations (history, press) extend `OPS` in api.js, `pageOps` and the parity session; the message line and `ApiError` are reusable by 03-11's buttons.

## Self-Check: PASSED

- Files exist: ui/web/js/api.js, ui/web/js/inputs.js, ui/web/js/message.js, uitest/inputs_test.go, this SUMMARY.
- Commits exist: 0bbe716, f325232, 7477187 (`git log --grep 03-08` returns all three).
- Acceptance criteria and the plan-level verification commands re-run green (see Verification Results).

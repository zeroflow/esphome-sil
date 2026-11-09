---
phase: 03-live-web-ui
plan: 02
subsystem: ui
tags: [javascript, headless-chrome, cdp, coder-websocket, offline, reconnect]

requires:
  - phase: 03-live-web-ui
    provides: "03-01 web.NewHandler, /ws stream, store.js, app.js reconnect loop, data-src/data-fmt page shell"
provides:
  - "ui/web/js/view.js: render(state, linkState) via requestAnimationFrame, formatters text/chip/rpm/pct/simtime/factor/firmware, `--` before the first snapshot"
  - "internal/cdptest: headless Chrome launcher and CDP client recording HTTP and WebSocket requests, SameOrigin, ErrNoChrome"
  - "uitest (build tag sil_ui): fake bench behind the real web handler, make test-ui"
  - "offline proof (UI-01) and reconnect/resync/stopped proofs (D-10) in a real browser"
affects: [03-03, 03-04, 03-05, 03-06, 03-07, 03-08, 03-10, 03-11]

actuals:
  tokens: 12000
  tasks: 2
  commits: 2
plan_head_before: 4aa1cfc44a05912c2fec1081043f90266d51e385
plan_head_after: a5876512956dfeea00875d84f5befb76ca7c5864
commits: 2

tech-stack:
  added: []
  patterns:
    - "the page paints only through view.render(store.getState(), linkState); linkState is connecting|live|retrying and view.js owns the LINK text"
    - "browser tests drive Chrome through cdptest over the real web handler; the bench mirrors supervisor.Watch (subscribe, then status and seq under one lock)"
    - "a request recorder that sees WebSockets: Network.requestWillBeSent for http, Network.webSocketCreated for ws"

key-files:
  created:
    - ui/web/js/view.js
    - internal/cdptest/cdptest.go
    - uitest/harness_test.go
    - uitest/bench_test.go
    - uitest/live_test.go
  modified:
    - ui/web/js/app.js
    - Makefile
    - e2e/web_test.go

key-decisions:
  - "cdptest.Eval wraps its argument as `(async () => (js))()` instead of using replMode: replMode returned an unresolved promise as `{}`; the wrapper gives top-level await and promise flattening with plain Runtime.evaluate. Eval therefore takes one expression, not statements"
  - "linkState tokens (connecting, live, retrying) live in app.js and the display strings live in view.js, so LINK text has one owner"
  - "simtime rounds to whole microseconds before truncating the tenth, so float noise never moves a digit"
  - "TestReconnectBackoff holds the next snapshot (HoldSnapshot) and changes the status silently (Mutate) so the retrying state is inspected deterministically and the replaced state is provably the new snapshot"

patterns-established:
  - "Pre-snapshot rule: every [data-src] shows `--` (no aria-pressed for data-fmt pressed) until the first snapshot; TestConnectingShowsDashes sweeps the whole document"
  - "Offline browser: --host-resolver-rules=MAP * ~NOTFOUND , EXCLUDE 127.0.0.1; every recorded request must pass SameOrigin and an outside fetch must fail"

requirements-completed: [UI-01, UI-02, UI-05]

duration: 25min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "The page shows instance, SIM TIME HH:MM:SS.s, TIME FACTOR x1, FIRMWARE CONNECTED and one panel per port with fan, RPM integer and duty with one decimal, updating from WS events"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/live_test.go#TestOfflineLoadAndLive"
        status: pass
    human_judgment: false
  - id: D2
    description: "Before the first snapshot every [data-src] element shows `--` and LINK reads CONNECTING"
    requirement: UI-05
    verification:
      - kind: automated_ui
        ref: "uitest/live_test.go#TestConnectingShowsDashes"
        status: pass
    human_judgment: false
  - id: D3
    description: "With every host except 127.0.0.1 unresolvable the page loads and stays live; every request including the /ws WebSocket is same-origin; an outside fetch fails"
    requirement: UI-01
    verification:
      - kind: automated_ui
        ref: "uitest/live_test.go#TestOfflineLoadAndLive"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/browser"
        status: pass
    human_judgment: false
  - id: D4
    description: "A lost WebSocket keeps the last values with LINK DISCONNECTED, RETRYING, reconnects by itself with 0.5 s doubling to a 5 s cap and replaces the whole state from the new snapshot; sim restarted, lagged and stopped frames re-snapshot without a reload"
    requirement: UI-05
    verification:
      - kind: automated_ui
        ref: "uitest/live_test.go#TestReconnectBackoff, TestResyncFrames, TestStoppedRetries"
        status: pass
    human_judgment: false
  - id: D5
    description: "make test-ui runs the sil_ui browser tests and fails loudly without Chrome; make check vets the sil_ui package"
    verification:
      - kind: other
        ref: "make check && make test-ui"
        status: pass
    human_judgment: false
---

# Phase 3 Plan 02: Readout Renderer and Browser Harness Summary

**`view.js` renders the instance, sim time, time factor, firmware link and per-port fan/RPM/duty from the store, and a CDP-driven offline headless Chrome (recording HTTP and WebSocket requests) proves the page is live, same-origin only, and reconnects by itself.**

## Performance

- **Duration:** about 25 min (the start time was not captured; taken from the 03-01 docs commit at 14:01 to the last task commit at 14:08 local plus SUMMARY work)
- **Tasks:** 2 (tracer + test-heavy auto task)
- **Files:** 8 (5 created, 3 modified)
- **Chrome used:** Google Chrome 157.0.8103.38
- **Warm `make test-ui`:** 15.6 s wall (15.3 s test time, of which TestStoppedRetries is 12.6 s by design: it waits for the 0.5, 1, 2, 4, 5 s backoff steps); the other four tests take 0.2 to 1.1 s. Chrome starts once in TestMain.
- **`make e2e E2E_RUN=TestWebTracer`:** 6.6 s wall including the Rev 3.3 bring-up; `browser` subtest 0.38 s.

## Accomplishments

- `view.js`: `render(state, linkState)` coalesced through `requestAnimationFrame`; builds one panel per port from the template (`{i}` in data-src replaced by the index) only when the port count changes; `--` for a null state or a missing path; sets `document.title` to `sil — <instance>` and the LINK text.
- `app.js` calls `refresh()` (render of `getState()` and the link state) on every applied frame and every link change; the WS, backoff and resync logic of 03-01 is unchanged.
- `internal/cdptest`: Chrome discovery (`$SIL_CHROME`, then google-chrome, chromium, chromium-browser), launch with a temporary profile, `--remote-debugging-port=0`, Setpgid and process-group kill, flattened target sessions, ordered request recorder (`http` and `ws`), console/exception/log error collection, `Eval`, `WaitFor`, `Mouse`, `Touch`, `Key`, `SetViewport`, `Center`, `SameOrigin`. All waits go through the injected clock. `go list -deps ./cmd/sil` contains no cdptest.
- `uitest`: `TestMain` launches one offline Chrome and exits 1 with `uitest: Chrome not found; install google-chrome or set SIL_CHROME` if there is none; the fake bench mirrors `supervisor.Watch` and offers `Emit`, `Mutate`, `DropStreams`, `Stop`, `HoldSnapshot`/`ReleaseSnapshot`, `Calls`, `FailNext`.
- `make test-ui` (`UI_RUN`), and `make check` now vets `-tags sil_ui ./uitest/`.
- e2e `TestWebTracer/browser`: offline Chrome on the real instance reaches LIVE, shows `e2e-web` and four panels, its store mirrors `sil set humidity=50`, and every request (the `/ws` WebSocket included) is same-origin.

## Task Commits

1. **Task 1 (tracer): view.js, cdptest, bench, offline and dashes tests, Makefile** - `40980be` (feat)
2. **Task 2: reconnect, resync and stopped tests plus TestWebTracer/browser** - `a587651` (test)

## TDD Gate Compliance

Task 2 carries `tdd="true"`, but its tests exercise reconnect and resync logic that 03-01 already shipped, so all three new uitest tests and the e2e subtest passed on their first run; there was no RED commit. To show the tests are not vacuous, two mutations were run and reverted without being committed: removing the backoff doubling in `app.js` failed `TestStoppedRetries` (retries 2 to 5 came after 0.5 s instead of 1, 2, 4, 5 s), and making `store.js` ignore `sim restarted` failed `TestResyncFrames` (no second Watch call). The same kind of check on `view.js` (showing `0` before the first snapshot) failed `TestConnectingShowsDashes`. The plan is `type: execute`, so no `check tdd-red-evidence` record was persisted.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Stale server error message stayed on the page after a reconnect**
- **Found during:** Task 1, while wiring app.js to render
- **Issue:** the error frame text that the server sends before closing (for example "the instance is shutting down") went to `#message` and was never cleared, so a page that reconnected to a healthy instance would keep showing it.
- **Fix:** app.js clears `#message` when a snapshot arrives.
- **Files modified:** `ui/web/js/app.js`
- **Commit:** `40980be`

**2. [Rule 3 - Blocking] `Runtime.evaluate` with `replMode` returned an unawaited promise as `{}`**
- **Found during:** Task 1, first run of TestOfflineLoadAndLive (the outside-fetch probe)
- **Fix:** `Eval` wraps the expression as `(async () => (js))()`; top-level `await` and promise results still work, and `Eval` takes one expression.
- **Files modified:** `internal/cdptest/cdptest.go`
- **Commit:** `40980be`

**3. [Rule 2 - Missing critical] Console errors are checked before the outside-fetch probe**
- The plan lists the probe before the console check; the probe's own failed request can log a console error, so the check runs on the page as loaded, before the probe. The probe itself still runs and must fail.

**Total deviations:** 3 (1 bug, 1 blocker, 1 ordering choice). **Impact:** none on scope; the interfaces in the plan are unchanged. `bench.Mutate` and `bench.callsOf`/`waitCalls` are small additions to the planned bench interface.

## Authentication Gates

None.

## Issues Encountered

None open. `TestOfflineLoadAndLive` records exactly five requests (the document, three scripts, one `/ws` WebSocket); the `data:,` favicon does not appear in the recorder.

## Known Stubs

None. The placeholder stub recorded by 03-01 in `.planning/WINDOWS.md` (readout elements show `--`, `#ports` empty until view.js) is resolved by this plan and marked fixed.

## Threat Flags

None. T-03-33: `view.js` writes only through `textContent` and `dataset`; no `innerHTML` or `eval` in first-party JS. T-03-34: Chrome uses `--remote-debugging-port=0`, a fresh temporary profile and a process-group kill; cdptest is not linked into `sil`. No new dependencies.

## Next Phase Readiness

- Later page plans can add `data-src` elements and formats (`hex`, `num`, `select`, `pressed`, `realtime`) to `view.js`; the pre-snapshot sweep already holds them to `--`.
- 03-03 touches only listener.go, ws.go and supervisor wiring; this plan's browser tests go through `web.NewHandler` and the real supervisor listener, and neither depends on keep-alive behaviour.
- `bench` embeds a nil `control.Control`: a page action that calls a method the bench does not implement panics in the handler goroutine until the plan that adds that action implements it.

## Self-Check: PASSED

- Files exist: ui/web/js/view.js, internal/cdptest/cdptest.go, uitest/harness_test.go, uitest/bench_test.go, uitest/live_test.go (all FOUND).
- Commits exist: 40980be, a587651 (both FOUND).
- Acceptance: `test-ui:` and `vet -tags sil_ui ./uitest/` in Makefile, host-resolver-rules and `Network.webSocketCreated` in cdptest.go, `//go:build sil_ui` first line of all three uitest files, `go list -deps ./cmd/sil | grep -c cdptest` prints 0, no `innerHTML`, `eval(` or `http(s)://` in first-party web files.
- Verification: `make check` green (119 passed), `make test-ui` prints PASS for all five tests, `make e2e E2E_RUN=TestWebTracer` prints PASS for all nine subtests including `browser`.

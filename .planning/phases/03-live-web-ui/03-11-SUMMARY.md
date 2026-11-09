---
phase: 03-live-web-ui
plan: 11
subsystem: ui
tags: [vanilla-js, press-and-hold, pointer-events, stale-overlay, real-time-cell, headless-chrome, e2e]

requires:
  - phase: 03-live-web-ui
    provides: "03-07 button down/up with max_hold_ms, sim behind/in_sync events and status sim.behind_real_time; 03-10 scopes and placeholder panels; 03-02 reconnect and resync; 03-04 parity allow-list"
provides:
  - "ui/web/js/buttons.js: USR1-3 press-and-hold caps (MAX_HOLD_MS 10000, initButtons, renderButtons, onHeld, onReleased) with every release path"
  - "api.js OPS button.down / button.up and {id} path parameters in call()"
  - "REAL TIME header cell (IN SYNC / orange BEHIND chip on the data-src element), store folding of sim behind / in_sync"
  - "stale overlay (body.stale): dimmed readouts, grey LED lenses, frozen scopes, disabled controls; final /ws error frame into the normal backoff"
  - "uitest hold and resilience tests, bench SetButton / AutoRelease / HoldButtonEvents / FailWatchAfterSnapshot, pageOps += button.down, button.up; e2e TestWebHold"
affects: [03-12]

actuals:
  tokens: 14200
  tasks: 2
  commits: 3
plan_head_before: 9ec2fabb17f62ece98961ed477ae3aba240e13ef
plan_head_after: 6dbec4396f4f92415a09e48885fb794acf81b77a
commits: 3

tech-stack:
  added: []
  patterns:
    - "a cap is lit only by the server report (aria-pressed from the status mirror); the page keeps `holding` only to know whether it owes an up"
    - "release() waits for the answer to the down it ends, so up cannot overtake down on separate connections"
    - "the stale class is the link state after the page has been LIVE once; plot.js freezes on setStale and thaws with one forced redraw"

key-files:
  created:
    - ui/web/js/buttons.js
    - uitest/hold_test.go
    - uitest/resilience_test.go
  modified:
    - ui/web/index.html
    - ui/web/app.css
    - ui/web/js/api.js
    - ui/web/js/app.js
    - ui/web/js/store.js
    - ui/web/js/view.js
    - ui/web/js/plot.js
    - uitest/bench_test.go
    - uitest/parity_test.go
    - e2e/web_test.go

key-decisions:
  - "release() chains the up behind the promise of the down it ends, so a quick tap cannot send up before down is processed; a failed down sends no up"
  - "A hold still open when the link is lost is released in renderButtons on a best-effort basis (a disabled cap sees no pointerup); the engine's 10 s max hold stays the backstop"
  - "Own capture release is remembered per pointer id (ownLoss) so a lostpointercapture caused by the page itself does not end the hold it starts; Chrome 157 fires no such event for a touch, so the guard is proven with a synthetic event"
  - "The REAL TIME cell is 112 px wide; BEHIND is the data-src element with class warn and aria-label, so the parity sweep and the displayed-value sweep see it like any readout"
  - "The stale class is set only after the page has been LIVE once (A-03-08); the stale look dims readout values, the LED well and the scope plot area, and leaves FIRMWARE and LINK at full strength"

patterns-established:
  - "bench.SetButton follows the engine's strict rules (button_busy, button_not_held) and records button.down / button.up with the max hold; HoldButtonEvents shows what the page does between request and report"
  - "A new cap-like control: data-src plus a data-fmt the parity oracle knows, text kept out of the textContent sweep"

requirements-completed: [UI-04, UI-05, UI-06]

duration: 13min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "Holding USR1-3 on the page sends one button.down with max_hold_ms 10000 and one button.up on release; the cap lights only from the server's report; the firmware reacts as to `sil press`"
    requirement: UI-04
    verification:
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRHold"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebHold"
        status: pass
    human_judgment: false
  - id: D2
    description: "A hold cannot stay stuck because of the page: touch slide-off (capture released), window blur, tab hidden, keyboard Space/Enter without repeat, pointercancel, pointerleave, lostpointercapture, cap blur, link loss; no redundant up after an engine max-hold release; a rejected down shows the API message and sends no up"
    requirement: UI-04
    verification:
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRTouchLeave"
        status: pass
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRBlurHidden"
        status: pass
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRKeyboard"
        status: pass
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRAutoRelease"
        status: pass
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRRejected"
        status: pass
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestUSRDisabledWhenStale"
        status: pass
    human_judgment: false
  - id: D3
    description: "REAL TIME cell of fixed width: IN SYNC, or the orange #ff8a1f BEHIND chip with aria-label, following the sim behind / in_sync events"
    requirement: UI-05
    verification:
      - kind: automated_ui
        ref: "uitest/hold_test.go#TestHeaderRealTime"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
    human_judgment: false
  - id: D4
    description: "Stale overlay and self-healing reconnect: last values kept at 0.4 opacity, grey lenses, frozen scopes, disabled controls, red LINK, FIRMWARE separate; fresh snapshot and history on return; sim restarted resyncs without reload; a final error frame shows its message and reconnects"
    requirement: UI-06
    verification:
      - kind: automated_ui
        ref: "uitest/resilience_test.go#TestStaleAndReconnect"
        status: pass
      - kind: automated_ui
        ref: "uitest/resilience_test.go#TestResyncOnSimRestart"
        status: pass
      - kind: automated_ui
        ref: "uitest/resilience_test.go#TestErrorFrame"
        status: pass
    human_judgment: false
  - id: D5
    description: "Parity: button.down and button.up are requested by the parity session, caps and REAL TIME cell are in the displayed-value sweep, no new uiOnlyText entry"
    requirement: UI-06
    verification:
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityRequests"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
    human_judgment: false
  - id: D6
    description: "A hold from a real phone finger and a hold against a live Home Assistant"
    requirement: UI-04
    verification: []
    human_judgment: true
    rationale: "Chrome's emulated touch is tested; a real phone browser (Safari, Firefox) and its capture behaviour is the 03-12 UAT"
  - id: D7
    description: "The 50 ms / 2 s behind-real-time thresholds feel right on a loaded host (UIC-30)"
    requirement: UI-05
    verification: []
    human_judgment: true
    rationale: "Thresholds are planner assumptions A-03-05 and A-03-06; the page renders the engine's flag only"
---

# Phase 3 Plan 11: USR Holds, REAL TIME and Stale State Summary

**USR1-3 press-and-hold caps that send down with a 10 s max hold and up on every release path, lit only from the server's report, plus the REAL TIME cell fed by the engine's behind flag and a stale overlay that keeps the last values and heals by itself, proven in headless Chrome and with a 300 ms page hold on the real Rev 3.3 firmware.**

## Performance

- **Duration:** about 13 min
- **Started:** 2026-11-09T14:56Z (estimate; the start time was not captured by variable)
- **Completed:** 2026-11-09T15:10Z
- **Tasks:** 2 (1 tracer, 1 TDD)
- **Files modified:** 13 (3 created)

## Accomplishments

- Task 1 (tracer): BUTTONS group, `buttons.js`, `OPS` entries and `{id}` parameters, bench `SetButton` with the strict busy / not-held rules, `TestUSRHold` (shows the cap stays dark while the server has not reported the hold), parity additions, and e2e `TestWebHold`: a 300 ms mouse hold on USR1 moved the Rev 3.3 duties from 0.4/0/0/0 to 0.5/0.1/0.1/0.1, exactly as `sil press usr1`; the page made one `POST .../usr1/down` with body `{"max_hold_ms":10000}` and one `POST .../usr1/up` without a body.
- Task 2: every release path, Space/Enter, the REAL TIME cell, the `stale` look and the freeze of the scopes, the final error frame into the backoff. Eleven new behaviour tests pass; the whole `make test-ui` (43 tests) passes twice in a row, `make check` 119 passed, `make e2e` for TestWebHold, TestWebTracer, TestWebPlot and TestWebFanSelect passes.
- Mutation checks (to show the tests can fail): no `releasePointerCapture` fails `TestUSRTouchLeave` (no up until the finger lifts); disabling the own-capture guard fails the synthetic-event part of `TestUSRTouchLeave`; removing the stale freeze in `plot.js` fails `TestStaleAndReconnect` ("scope 1 was redrawn 1 times while stale").

## Task Commits

1. **Task 1 (tracer): USR caps, TestUSRHold, TestWebHold** - `090c388` (feat)
2. **Task 2 RED: release paths, REAL TIME, stale and resync tests** - `1cb9e16` (test)
3. **Task 2 GREEN: release paths, REAL TIME cell, stale overlay** - `6dbec43` (feat)

Tracer gate: the tracer `<verify>` is automated-only; `make check`, the full `make test-ui` and `TestWebHold` passed before expansion (end-of-phase mode, no checkpoint).

## Files Created/Modified

- `ui/web/js/buttons.js` - caps, down / release with ordering, all release paths, `renderButtons` (aria-pressed from state, disabled unless LIVE)
- `ui/web/js/api.js` - `button.down`, `button.up`, `call(name, {params})`
- `ui/web/js/app.js` - `initButtons`, button frames call `onHeld` / `onReleased`, error frame calls `lost()`
- `ui/web/js/store.js` - sim `behind` / `in_sync` set `sim.behind_real_time`
- `ui/web/js/view.js` - `realtime` formatter, `paintRealTime`, `stale` body class, caps kept out of the text sweep
- `ui/web/js/plot.js` - `setStale` (no drawing while stale, one forced redraw on thaw)
- `ui/web/index.html`, `ui/web/app.css` - BUTTONS group, REAL TIME cell, `.value.warn` chip, `body.stale` rules
- `uitest/bench_test.go` - `SetButton`, `AutoRelease`, `HoldButtonEvents` / `ReleaseButtonEvents`, `FailWatchAfterSnapshot`
- `uitest/hold_test.go`, `uitest/resilience_test.go` (new), `uitest/parity_test.go`, `e2e/web_test.go`

## Decisions Made

See `key-decisions`. The two worth restating: the up waits for the answer to its down (a tap cannot be reordered on the wire), and a hold still open when the link is lost is released best-effort because a disabled cap never sees its pointerup.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Up could overtake down; own capture release could end a hold**
- **Found during:** Task 1 and Task 2 design
- **Issue:** a quick tap sends down and up on separate connections, so the server could see up first and answer button_not_held; and a browser that reports the page's own `releasePointerCapture` as `lostpointercapture` would end the hold it just started.
- **Fix:** `release()` chains the up behind the down's promise (a failed down sends no up); `ownLoss` per pointer id swallows the page's own capture loss once.
- **Files modified:** `ui/web/js/buttons.js`
- **Commits:** `090c388`, `6dbec43`

**2. [Rule 2 - Missing Critical] Hold open at link loss**
- **Found during:** Task 2 (UIC-18, the caps are disabled while stale)
- **Issue:** a disabled cap receives no pointerup, so a hold open when the link dropped would only end by the engine's 10 s max hold.
- **Fix:** `renderButtons` releases a holding cap as soon as it is disabled (best effort, errors shown verbatim).
- **Files modified:** `ui/web/js/buttons.js`
- **Commit:** `6dbec43`

**3. [Interface variation] extra export and test hooks**
- `buttons.js` also exports `onHeld(id)` (the plan puts the v true marking in app.js; it needs a function to reach the cap). The e2e records the POST bodies with an in-page `fetch` wrapper because the CDP request log has no bodies; `cdptest` is untouched.

**4. [TDD note] TestResyncOnSimRestart and TestErrorFrame passed at RED**
- The 03-02 resync and the existing error-frame handling already satisfied them; they are kept as pinned behaviour. The GREEN change for the error frame only adds the explicit `lost(ws)` so the backoff does not depend on the server's close.

**Total deviations:** 2 auto-fixed (Rule 2), 2 notes. **Impact:** none on scope; both fixes close ways a hold could stay stuck.

## Issues Encountered

- Chrome 157 fires no `lostpointercapture` for the implicit touch capture the page releases, so the own-loss guard is exercised with a synthetic event; a real Safari or Firefox phone is part of the 03-12 UAT.
- `uitest` stale checks need the next snapshot held (`HoldSnapshot`) so the page stays stale long enough to read; the same pattern as TestReconnectBackoff.

## Known Stubs

None.

## Threat Flags

None. T-03-30 mitigated (every release path plus the 10 s max hold on each page down; TestUSRTouchLeave, TestUSRBlurHidden, TestUSRKeyboard, TestUSRAutoRelease); T-03-31 mitigated (TestStaleAndReconnect); T-03-32 accepted (AR-03-01); T-03-SC/03-11 no install, no dependency added.

## Next Phase Readiness

- 03-12 can run the five success criteria against the real firmware: the long press (hold 1.5 s via the page), environment and fan swap, and the real-phone / Home Assistant UAT items listed under D6.
- EDGE-UI-04-unclassified and A-03-08 stay flagged for the manual review: the 10 s max hold, the caps' `data-src` / `pressed` parity and the stale-after-first-LIVE rule are implemented as assumed.

## Self-Check: PASSED

- Files exist: `ui/web/js/buttons.js`, `uitest/hold_test.go`, `uitest/resilience_test.go`.
- Commits `090c388`, `1cb9e16`, `6dbec43` exist on `claude/main`.
- Acceptance: `grep -n 'MAX_HOLD_MS = 10000'`, `releasePointerCapture`, `visibilitychange` and `lostpointercapture` match in `ui/web/js/buttons.js`; `make check` 119 passed; `make test-ui` all 43 pass (TestUSRHold, TestUSRTouchLeave, TestUSRBlurHidden, TestUSRKeyboard, TestUSRAutoRelease, TestUSRRejected, TestUSRDisabledWhenStale, TestHeaderRealTime, TestStaleAndReconnect, TestResyncOnSimRestart, TestErrorFrame, the parity tests, TestConnectingShowsDashes); `make e2e E2E_RUN='TestWebHold|TestWebTracer|TestWebPlot|TestWebFanSelect'` all pass.

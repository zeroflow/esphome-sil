---
phase: 03-live-web-ui
plan: 10
subsystem: ui
tags: [uplot, vanilla-js, scope-screen, history-join, null-gaps, headless-chrome, csp]

requires:
  - phase: 03-live-web-ui
    provides: "03-07 sample events, gap marker and GET /api/v1/history; 03-08 fans list and api.js; 03-09 vendored uPlot 1.6.32; 03-06 panel layout and empty .scope well; 03-04 parity allow-list"
provides:
  - "ui/web/js/ring.js: Ring, a bounded plain-array series with null gaps that renders the server's gap marker"
  - "ui/web/js/plot.js: one uPlot scope per port panel (fixed axes, graticule, empty state, rAF redraw), initScopes, pushSample, loadHistory, setFanMax, setLink, resetScopes, scopeFor, ringFor"
  - "app.js history join on every (re)connect: /ws?kind=...,sample, samples held until GET /api/v1/history?window_s=62 answers, then history, then held samples above its seq, then live"
  - "api.js OPS.history and call(name, {body, query}); view.js placeholder panels while CONNECTING"
  - "uitest plot tests, bench History/HoldHistory/ReleaseHistory/SetHistory/Inject, parity pageOps += history and two scope empty-state allow-list entries; e2e TestWebPlot"
affects: [03-11, 03-12]

actuals:
  tokens: 14300
  tasks: 2
  commits: 3
plan_head_before: 1ea23be6ade98e38ddb877088af2462c1e6dbb89
plan_head_after: 6eb33f979a9d6821fcf4d7c4a4b6202a02b3f060
commits: 3

tech-stack:
  added: []
  patterns:
    - "uPlot instances and rings live in plot.js module scope; the store state never holds them"
    - "samples are appended in the WebSocket handler (hidden tabs lose nothing) and drawn in a throttled requestAnimationFrame (at most 20 a second)"
    - "a gap is only what the server sent: a null value or the gap marker; the page does no arithmetic on sample times"
    - "a scale the page cannot know (unknown fan) hides the RPM trace instead of guessing one"

key-files:
  created:
    - ui/web/js/ring.js
    - ui/web/js/plot.js
    - uitest/plot_test.go
  modified:
    - ui/web/index.html
    - ui/web/app.css
    - ui/web/js/app.js
    - ui/web/js/api.js
    - ui/web/js/view.js
    - uitest/bench_test.go
    - uitest/parity_test.go
    - uitest/live_test.go
    - e2e/web_test.go

key-decisions:
  - "The history request starts after the snapshot, so its seq is at least the snapshot's and no sample can fall between the history and the stream (a request before the snapshot could lose samples)"
  - "Sample frames also go through store.apply, so SIM TIME follows the 10 Hz heartbeat on an idle bench (Research Pattern 3); they are not held for the store, only for the plot"
  - "A fan that is not in the fans list (or no list) leaves the RPM scale unknown: the RPM trace and labels are hidden, nothing is autoscaled or invented"
  - "A scope's empty state ends at the port's first sample with a non-null value; a sample whose values are all null (firmware off the bus) is not a first sample"
  - "The restart gap null point at the new sample's own x works in real Chrome: uPlot breaks the line at the repeated x (TestPlotRestartGap, marked), so no other placement was needed"
  - "No CSP fallback needed: with four live scopes style-src 'self' alone gives an empty console (Research A6 holds)"

patterns-established:
  - "New plot data = ring.js push semantics plus a test that shows it can fail (mutation of ring.js, plot.js or app.js)"
  - "bench.Inject delivers a frame past the kind filter of a subscription, for the lagged marker a real subscription makes itself"

requirements-completed: [UI-02, UI-01, UI-06]

duration: 15min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "Each port panel draws a uPlot scope: RPM 2 px solid on the left axis, duty 1 px dashed [4,4] stepped on the right axis, channel colours, 10 x 4 graticule with centre lines, static legend and `6 s/div`, uPlot legend and cursor off"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestScopeStyle"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestScopeLive"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebPlot"
        status: pass
    human_judgment: false
  - id: D2
    description: "Fixed axes: RPM 0..max_rpm of the plugged fan, duty 0..100, nothing autoscales, a fan swap re-scales once"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestFixedAxes"
        status: pass
    human_judgment: false
  - id: D3
    description: "History join on every (re)connect: samples held until GET /api/v1/history answers, no sample twice or lost, a reconnect replaces the rings"
    requirement: UI-06
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestHistoryJoin"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityRequests"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebPlot"
        status: pass
    human_judgment: false
  - id: D4
    description: "Honest gaps: null values and server-marked restart samples break the line, an unmarked time jump does not, null stays null in the ring"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestPlotGap"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestPlotRestartGap"
        status: pass
    human_judgment: false
  - id: D5
    description: "Bounded client memory: rings of 620 + 64 points, appends outside rAF, redraw at most 20 a second and once on becoming visible"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestRingBounded"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestHiddenAppend"
        status: pass
    human_judgment: false
  - id: D6
    description: "Empty state copy per port and its connecting variant, ended by the port's first non-null sample; parity allow-list names the two texts"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestEmptyScope"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
    human_judgment: false
  - id: D7
    description: "uPlot loads from the binary and four live scopes run under the CSP with an empty console"
    requirement: UI-01
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestNoCSPViolations"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebPlot"
        status: pass
    human_judgment: false
  - id: D8
    description: "Plot history stays flat during a long run (UIC-10 backstop soak check)"
    requirement: UI-02
    verification: []
    human_judgment: true
    rationale: "UIC-10 is a held-out backstop for the verifier: the rings are proven bounded after 1,000,000 pushes and 3000 live samples, but a minutes-long soak with a heap reading is the verifier's check"
  - id: D9
    description: "The scope looks like a bench-instrument screen (colour, weight and legibility of the traces on a real display)"
    requirement: UI-02
    verification: []
    human_judgment: true
    rationale: "Pixel presence and style attributes are asserted; whether the look is right is taste, checked in the 03-12 UAT"
---

# Phase 3 Plan 10: Scope Screens Summary

**One uPlot 1.6.32 scope per port, drawn from `GET /api/v1/history` plus live `sample` frames: fixed 0..max_rpm and 0..100 axes, a 10 x 4 graticule, null and server-marked gaps that break the line, bounded plain-array rings, an exact seq join on every reconnect, and the empty state, proven in headless Chrome under the CSP and on the real Rev 3.3 firmware.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-11-09T14:37:46Z
- **Completed:** 2026-11-09T14:53:00Z
- **Tasks:** 2 (1 tracer, 1 TDD)
- **Files modified:** 12 (3 created)

## Accomplishments

- Task 1 (tracer): ring.js and plot.js, the uPlot script and stylesheet tags, the scope markup (legend, `6 s/div`, empty-state text), the join protocol in app.js, `OPS.history` and `call(..., {query})`, bench `History`, `pageOps += history`, the two `uiOnlyText` entries, and e2e `TestWebPlot`: on the real firmware at 40 % duty the scope held 50 non-null RPM points, last duty 40, RPM scale 0..1800 (the Arctic max_rpm from `sil fans`), one history request per `/ws` connection, empty console.
- Task 2: nine behaviour tests (`TestScopeLive`, `TestScopeStyle`, `TestRingBounded`, `TestPlotGap`, `TestPlotRestartGap`, `TestFixedAxes`, `TestHistoryJoin`, `TestEmptyScope`, `TestNoCSPViolations`) plus `TestHiddenAppend` for the hidden-tab promise. All pass and are stable over 3 repeated runs.
- Measured: a redraw happens at most every 50 ms (68 draws per 1127 ms of 200 samples a second without the throttle, 23-limit respected with it); a Ring(620, 64) never exceeds 684 points over 1,000,000 pushes; 3000 live samples leave every port at 600..684 points.
- **CSP outcome: no fallback needed.** With four live scopes and an empty console `style-src 'self'` suffices (Research A6 confirmed). `internal/web/guard.go` and `guard_test.go` are untouched.
- **Restart-gap placement:** the null point at the new sample's own x (orchestrator's revision) works in the real browser. uPlot breaks the line at the repeated x: `TestPlotRestartGap` marked leaves xs `[1, 4, 4]` with nulls at index 1 and no channel-colour pixel in the middle of a 3 s jump, the unmarked case joins the line. A mutation removing the marker's null point fails that test, so it is a real check. No alternative placement and no deviation.

## Task Commits

1. **Task 1 (tracer): scope screens, history join, TestWebPlot** - `ff36f64` (feat)
2. **Task 2 tests: scope correctness** - `8dce9cd` (test)
3. **Task 2 tidy: schedule a redraw only when a scope is dirty** - `6eb33f9` (refactor)

Tracer gate: the tracer `<verify>` is automated-only; `make check`, `TestVendorHashes`, the parity, offline and layout tests and `TestWebPlot` passed end to end before expansion (end-of-phase mode, no checkpoint).

## Files Created/Modified

- `ui/web/js/ring.js` - Ring(cap, slack): parallel plain arrays xs (seconds), rpm, duty (percent), null for a null value, `gap` marker as one null point at the new sample's own x, trim to cap beyond cap + slack
- `ui/web/js/plot.js` - scopes per panel, fixed scales, centre lines in a `drawAxes` hook, channel colours read lazily from `--ch`, empty-state painting, `setFanMax` (one `setScale` per change), throttled rAF redraw, visibilitychange redraw
- `ui/web/js/app.js` - kinds list with `sample`, history join (held list capped at 3000 frames), fan max sync, join dropped on connection loss
- `ui/web/js/api.js`, `view.js`, `index.html`, `app.css` - `OPS.history` and query support; placeholder panels, port number on each panel; vendored uPlot tags and scope markup; overlay styles
- `uitest/plot_test.go` (new), `bench_test.go`, `parity_test.go`, `live_test.go`; `e2e/web_test.go` (TestWebPlot)

## Decisions Made

See `key-decisions`. In short: the history request waits for the snapshot (no lost samples), samples also feed SIM TIME, an unknown fan hides the RPM trace rather than guess a scale, and a null-only sample does not end the empty state.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Placeholder panels while CONNECTING**
- **Found during:** Task 1 (writing TestEmptyScope's connecting case)
- **Issue:** before the first snapshot `#ports` was empty, so there was no scope on which to show the connecting copy that UIC-07/UIC-08 and the plan's truth require.
- **Fix:** view.js builds four placeholder panels (the reference board's four ports, every value `--`, selects disabled) until the snapshot, then keeps them when the port count matches and rebuilds otherwise. Layout, dashes and parity tests were re-run and pass.
- **Files modified:** `ui/web/js/view.js`
- **Commit:** `ff36f64`

**2. [Rule 3 - Blocking] TestResyncFrames emulated the lagged marker through Publish**
- **Found during:** Task 1 (full `make test-ui`)
- **Issue:** the page now subscribes with an explicit kind list; a `lagged` event published to the broker is dropped by a kind-filtered subscription, whereas a real instance makes the marker inside the subscription. The existing test therefore waited for a resync that could not come.
- **Fix:** `bench.Inject` delivers a frame to every running Watch past the filter; `TestResyncFrames` uses it for the lagged frame.
- **Files modified:** `uitest/bench_test.go`, `uitest/live_test.go`
- **Commit:** `ff36f64`

**3. [Interface variation] exports**
- `initScopes(panels)` takes only the panels (the fan max reaches plot.js through `setFanMax`, kept per port so a scope created later picks it up); `ringFor(port)` and `setLink(linkState)` were added for the tests and the empty-state body. No behaviour of the plan changes.

**4. [TDD note] no RED phase for Task 2**
- The tracer already implemented the behaviour, so the Task 2 tests passed on their first run. To show they can fail, each was run against a one-line mutation: no gap null point (`TestPlotRestartGap` fails), null mapped to 0 (`TestPlotGap` fails), no ring trim (`TestRingBounded` fails), no seq filter on held samples (`TestHistoryJoin` fails), setScale on every call (`TestFixedAxes` fails), no redraw throttle (`TestScopeLive` fails after strengthening it to a 200 samples/s flood). The `TestEmptyScope` mutation I tried changed an unreachable path and did not fail; the per-port behaviour is asserted directly on the DOM.

**Total deviations:** 2 auto-fixed (1 Rule 2, 1 Rule 3), 2 notes. **Impact:** none on the plan's behaviour or scope; the placeholder panels are what makes the connecting copy visible.

## Issues Encountered

None open. The stale dimming of the canvas on WebSocket loss (UIC-09 first half) is not part of this plan; 03-11 owns it.

## Known Stubs

None. The empty `.scope` placeholder logged in `.planning/WINDOWS.md` (entry 2) is resolved by this plan and marked fixed.

## Threat Flags

None. T-03-28 mitigated (rings 620 + 64, appends outside rAF, held list capped, TestRingBounded); T-03-29 not triggered (CSP untouched); T-03-SC/03-10 no install in this plan, TestVendorHashes still passes in `make check`.

## Next Phase Readiness

- 03-11 can add the stale overlay to `.scope .plot` and the REAL TIME cell; scopes keep their last data on a lost link (the join and rings are replaced only when the new history answers).
- UIC-10 (long soak) stays for the verifier; the unit-level bounds are proven.

## Self-Check: PASSED

- Files exist: `ui/web/js/ring.js`, `ui/web/js/plot.js`, `uitest/plot_test.go`.
- Commits `ff36f64`, `8dce9cd`, `6eb33f9` exist on `claude/main`.
- Acceptance: `grep -n '/vendor/uPlot.iife.min.js' ui/web/index.html` matches; `grep -n GAP_FACTOR ui/web/js/ring.js ui/web/js/plot.js` prints nothing; `make check` 119 passed; `make test-ui` all pass (including TestParityRequests with history in pageOps, and every test of the behaviour list); `make e2e E2E_RUN='TestWebPlot|TestWebTracer|TestWebFanSelect|TestWebReach'` all pass.

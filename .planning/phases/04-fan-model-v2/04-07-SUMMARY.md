---
phase: 04-fan-model-v2
plan: 07
subsystem: ui
tags: [web-ui, uplot, alpine-free-store, tach, airflow, power, parity, layout]

requires:
  - phase: 04-fan-model-v2
    provides: "04-06 status fields tach_rpm/airflow_m3h/power_w, tach and load event kinds (null as \"v\":null, forced at engine start and fan swap), plot samples carrying tach"
provides:
  - "Each port scope draws a third, stepped TACH trace (port colour at 0.5 alpha, width 1) on the fixed 0..max_rpm axis next to the unchanged shaft RPM and duty traces"
  - "Ring.tach parallel array; push(t, rpm, duty, gap, tach); null and absent tach are gaps, the gap marker breaks all three series"
  - "Each port panel shows TACH RPM, AIRFLOW (M3/H, 1 decimal) and POWER (W, 2 decimals) in a second readout row; the three show the em dash for no value, every existing readout keeps --"
  - "store applies tach and load events exactly as reported; /ws?kind= asks for tach and load"
  - "view.js NULL_TEXT: the null text is a property of the format (tach, flow, watt -> em dash)"
  - "uitest: tach/load parity steps with nulls, tach/flow/watt oracles with an independent null oracle, loads-row layout checks, bench with tach/airflow/power values and nulls and FanInfo through control.NewFanInfo"
affects: [04-08, 04-10, phase-05-faults]

plan_head_before: 585fd9df8a23e9200b26e2ef15d8d9d8ea0539a6
plan_head_after: 6071489f47afa3efe2f7ab4030aa1df161f5ef29

actuals:
  tokens: 5500
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Null text per format: view.js NULL_TEXT maps a format name to its no-value text; shown() uses NULL_TEXT[fmt] ?? NONE, so a new readout can choose its glyph without touching an existing one"
    - "The ring appends to four parallel arrays (xs, rpm, duty, tach) through one add(); the gap marker is one all-null point"
    - "The bottom strip sits above the port grid in the stacking order (z-index) so its controls stay reachable when a window is shorter than the panels need"

key-files:
  created: []
  modified:
    - ui/web/js/ring.js
    - ui/web/js/plot.js
    - ui/web/js/store.js
    - ui/web/js/app.js
    - ui/web/js/view.js
    - ui/web/index.html
    - ui/web/app.css
    - uitest/plot_test.go
    - uitest/parity_test.go
    - uitest/layout_test.go
    - uitest/live_test.go
    - uitest/bench_test.go

key-decisions:
  - "Em dash for null on the three new readouts only (D-16 as amended 2026-11-11, A-GLYPH resolved): carried by the format (NULL_TEXT), so no existing readout and no existing parity row changes"
  - "The loads row is 28 px (not the 44 px of the control row) and the new readout values are 16 px; the page's font-size set grows from 12/14/20/28 to 12/14/16/20/28"
  - "The strip gets position: relative; z-index: 1 so that a window shorter than the panels need covers the panels, not the USR caps and fields"

patterns-established:
  - "A readout with a no-value glyph other than -- declares it in NULL_TEXT and mirrors it in the parity oracle (fmtOracle handles nil before its switch)"

requirements-completed: [MODEL-09, MODEL-11]

coverage:
  - id: D1
    description: "A port scope draws the tach RPM as a stepped trace in the port colour at 0.5 alpha, width 1, on the fixed rpm axis, next to the unchanged shaft and duty traces; a tach above max_rpm does not rescale the axis"
    requirement: MODEL-09
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestScopeTach"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestTachFixedAxis"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestScopeStyle"
        status: pass
    human_judgment: false
  - id: D2
    description: "The ring keeps a tach array parallel to xs, rpm and duty; null and absent tach are gaps; the gap marker and history replay cover all three series"
    requirement: MODEL-09
    verification:
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestTachGaps"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestPlotRestartGap"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestHistoryJoin"
        status: pass
    human_judgment: false
  - id: D3
    description: "Each panel shows TACH RPM, AIRFLOW (M3/H, one decimal) and POWER (W, two decimals) from ports.N.tach_rpm / airflow_m3h / power_w, with an em dash for null and the page deriving nothing; every data-src element equals the oracle format of GET status after every step of a session with tach and load steps including nulls"
    requirement: MODEL-11
    verification:
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityDisplayedValues"
        status: pass
      - kind: automated_ui
        ref: "uitest/parity_test.go#TestParityPageState"
        status: pass
      - kind: automated_ui
        ref: "uitest/plot_test.go#TestTachReadoutFollowsEvents"
        status: pass
      - kind: automated_ui
        ref: "uitest/live_test.go#TestConnectingShowsDashes"
        status: pass
    human_judgment: false
  - id: D4
    description: "Layout contract: at 1280x720 no scroll, scopes >= 128 px, the loads row below the first row and inside its panel, readout widths fixed under value changes; one column below 1024 px, no sideways overflow and no overlap in the loads row at 390 px"
    requirement: MODEL-11
    verification:
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutDesktop"
        status: pass
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutNarrow"
        status: pass
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutPhone"
        status: pass
    human_judgment: false
  - id: D5
    description: "How the stepped tach trace and the TACH RPM / AIRFLOW / POWER row look on desktop and phone with a shipped schema 2 fan running (a visual judgment; also the A-NOUISPEC arrangement)"
    verification: []
    human_judgment: true
    rationale: "No UI-SPEC exists for Phase 4; the arrangement is fixed by measurable criteria only. Whether the dimmer stepped line reads well next to the solid trace, and whether the row looks right, is for the end-of-phase human check (workflow.human_verify_mode = end-of-phase). The real-bench check with the firmware in the loop is 04-10."

duration: 15min
completed: 2026-11-11
status: complete
---

# Phase 4 Plan 07: Tach Trace, TACH RPM, AIRFLOW and POWER on the Page Summary

**Each port scope now draws a stepped, half-alpha TACH trace next to the shaft RPM on the fixed axis, and each panel gains a second row with TACH RPM, AIRFLOW (m3/h) and POWER (W) that show an em dash for no value, while the page derives nothing and every existing readout keeps `--`.**

## Performance

- **Duration:** about 15 min (07:15 to 07:30 local)
- **Tasks:** 2 (Task 1 tracer, Task 2 expansion), both TDD: 2 RED + 2 GREEN commits
- **Files modified:** 12 (7 page files, 5 uitest files), none created, none deleted

## Accomplishments

- **Tracer (Task 1), end to end for one series and one readout:** `Ring.tach`, uPlot series 3 (stepped, `withAlpha(color, 0.5)`, width 1, `spanGaps: false`, hidden together with series 1 while max_rpm is unknown), store cases `tach` and `load`, KINDS with both, the `tach` format with its em dash null text, the second readout row with TACH RPM and the TACH legend key. Tracer feedback gate (auto mode active, automated-only verify): the `<verify>` command was re-run green end to end (`TestScopeTach` and the rest of the list) before the expansion started, so `Tracer verified end-to-end - expanding`.
- **Expansion (Task 2):** AIRFLOW and POWER readouts (`flow` 1 decimal, `watt` 2 decimals, both with the em dash), 6ch fixed slots, the bench with values and nulls, the parity session with `tach`, `tach null`, `load`, `load null` steps and an independently written oracle, the layout checks.
- **Stepped trace proven on pixels, not only on options:** `TestScopeTach` publishes a 500 reading at 10 s and a 1000 reading at 12 s and checks that the canvas column at 11 s holds the 500 level and nothing at 750 (a straight line would cross it).
- **Null is never a number:** `MUST NOT show a made-up airflow or power value` is covered by the parity steps `tach null` and `load null`, by the bench's null ports 2 to 4 at the first paint, and by `TestTachReadoutFollowsEvents` (null after a value shows the em dash while the RPM readout keeps its number).

## Task Commits

1. **Task 1 (tracer): tach trace and TACH RPM readout**
   - RED `05fe2ff` (test): `TestScopeTach`, `TestTachGaps`, `TestTachFixedAxis`, `TestTachReadoutFollowsEvents`; failures were assertions on the planned behavior (no series 3, no `tach` array, no tach readout), the scope-style checks stayed green
   - GREEN `1fb2209` (feat): ring, plot, store, app, view, template, CSS, plus the test updates the new rows forced (see Deviations)
2. **Task 2: AIRFLOW and POWER readouts, parity rows, layout contract**
   - RED `8914af8` (test): parity steps and presence check of the three readouts on every port, layout checks of the loads row, widths under tach and load changes, phone overlap, bench values
   - GREEN `6071489` (feat): `flow` and `watt` formats, the two readouts, 6ch slots, narrow-block gap

**Plan metadata:** committed separately (docs: complete plan)

## Files Created/Modified

- `ui/web/js/ring.js` - `tach` array, `push(t, rpm, duty, gap, tach)`, `add` with a tach argument
- `ui/web/js/plot.js` - series 3 TACH, four-array data, fixed-axis comment (Pitfall 7), `setFanMax` toggles series 3
- `ui/web/js/store.js` - `tach` and `load` cases
- `ui/web/js/app.js` - KINDS with `tach` and `load`
- `ui/web/js/view.js` - `EM_DASH` beside `NONE`, `NULL_TEXT`, formats `tach`, `flow`, `watt`
- `ui/web/index.html` - `.row.loads` with three readouts, TACH legend key
- `ui/web/app.css` - `.row.loads`, 16 px values, 6ch slots, strip above the grid
- `uitest/plot_test.go`, `parity_test.go`, `layout_test.go`, `live_test.go`, `bench_test.go` - the new and extended tests

## Decisions Made

- The null text is a property of the format (`NULL_TEXT`), not of the element, so the template placeholders and the parity oracle both follow from one rule and no existing readout can flip glyphs.
- The loads row is 28 px high. At 1280x720 a panel then needs about 237 px and the grid offers 244; a 44 px row (the control row height) made the panels overflow into the strip.
- The strip stays above the grid (z-index) so its controls remain clickable in a window shorter than the panels need.

## Review of the flagged edge assumptions (resolved at execution, user decision 2026-11-11)

- **A-NOUISPEC (no UI-SPEC for Phase 4, flagged unresolved): reviewed, accepted as covered by checkable criteria; the look stays a human judgment.** Layout coverage: `TestLayoutDesktop` (no scroll at 1280x720, each scope >= 128 px, loads row below the first row and inside the panel, units and data-src of the row, widths unchanged when tach, airflow and power change from em dash to 4 digits to 1 digit), `TestLayoutNarrow` (one column), `TestLayoutPhone` (no sideways overflow at 390 px, three loads readouts that do not overlap or leave the row). A screenshot of 1280x720 was read by eye once: row sits cleanly under the main readouts, legend shows three keys. Residual: whether a designer would arrange it differently is a taste call, carried as coverage item D5 (human check at end of phase, and 04-10 with the real firmware).
- **A-GLYPH (resolved by the user before execution): implemented as locked.** The three new readouts show the em dash; the oracle's null text is the em dash for tach, flow and watt and `--` for every other format; `const NONE = "--"` is untouched.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The loads row CSS had to land in Task 1, not Task 2**
- **Found during:** Task 1 (full `make test-ui` after the template change)
- **Issue:** The new `.row` inherits the control row's 44 px minimum; the two panel rows then overflowed the grid by 9 px at 1280x720 and every test that clicks the strip (USR caps, environment fields) failed. The plan keeps all CSS in Task 2.
- **Fix:** `.row.loads` (28 px, gap 16) and the 16 px tach value moved into Task 1; Task 2 added the flow/watt rules and the 6ch slots.
- **Files modified:** `ui/web/app.css`
- **Committed in:** `1fb2209`

**2. [Rule 3 - Blocking] The strip must stay clickable in a short window**
- **Found during:** Task 1
- **Issue:** The test browser's default window has an inner height of about 633 px, below the 720 px the layout is specified for. The baseline panels (about 209 px) already overlapped the strip by 17 px without touching a control; with the loads row they cover the USR caps and fields, and 14 existing tests (hold, inputs) failed on clicks that landed on the scope.
- **Fix:** `#strip { position: relative; z-index: 1; }` with a comment: in a window shorter than the panels need, the strip covers the panels, not the other way round. No effect at 1280x720 (no overlap).
- **Files modified:** `ui/web/app.css`
- **Committed in:** `1fb2209`

**3. [Rule 3 - Blocking] Existing assertions that pinned the old page**
- **Found during:** Task 1
- **Issue:** `TestScopeStyle` expected the legend text `RPM DUTY` (now `RPM DUTY TACH`); `TestLayoutDesktop` allowed only 12/14/20/28 px (the plan's 16 px readouts are a new size); `TestConnectingShowsDashes` required `--` in every `[data-src]` element before the first snapshot (the three new readouts show the em dash by D-16); `fmtOracle` had no null handling and no tach format.
- **Fix:** Legend and font-size expectations updated; the connecting sweep expects the em dash for the formats tach, flow and watt and `--` otherwise; `fmtOracle` handles nil before its switch (em dash for tach/flow/watt, `--` otherwise) and gained tach, flow, watt. `live_test.go` is not in the plan's file list.
- **Files modified:** `uitest/plot_test.go`, `uitest/layout_test.go`, `uitest/live_test.go`, `uitest/parity_test.go`
- **Committed in:** `1fb2209`

---

**Total deviations:** 3 auto-fixed (all Rule 3, CSS ordering, a stacking rule, and test expectations the planned change forces)
**Impact on plan:** None on the delivered behavior. The oracle's null handling and the tach/flow/watt cases, planned for Task 2, arrived in Task 1 because the tracer's template change already made the parity test read a tach element.

## TDD Gate Compliance

Both tasks have a `test(04-07)` RED commit (`05fe2ff`, `8914af8`) before the `feat(04-07)` GREEN commit (`1fb2209`, `6071489`). The REDs failed on assertions for the planned behavior (missing series 3, missing tach array, missing readouts and data-src elements, loads row with one readout), not on compile or load errors. No refactor commit was needed. `workflow.tdd_mode` is off, so no `check tdd-red-evidence` record was persisted.

## Issues Encountered

- The whole-suite run takes about 68 s; the targeted `UI_RUN` runs take under 10 s. `make test-ui` is not part of `make check` (both green at the end).
- `TestUSRSimRestartRelease` logs a pinned known limit (A-03.1-09) on every run; it is pre-existing and passes.

## Known Stubs

None. No placeholder data flows to a readout: the template's em dash is the format's own null text and is replaced on the first snapshot.

## Threat Flags

None. T-04-15 mitigated (the new formats call `toFixed` and `Math.round` on numbers only, all readouts still go through `textContent`; no `innerHTML`; the template text is static). T-04-16 mitigated (`make check` and the `TestNoVendorMarks` / `TestNoCSPViolations` / external-reference tests green; no new external reference, uPlot stays the vendored 1.6.32). T-04-SC accepted (no package touched). No new endpoint or trust boundary.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 04-08 (shipped library) can run `make test-ui` on the schema 2 library: the bench reads ids, names and max_rpm from the shipped library through `control.NewFanInfo`, so a changed library flows into these tests.
- 04-10 owns the real-bench page checks (`TestWebLoadReadouts`, `TestWebPlot`, `TestWebAcceptance`) and, with the end-of-phase human check, the look of the stepped trace on a running instance.
- Not run here: `make e2e` (needs Docker and the firmware). No `sil up` instance was started or left behind.

## Self-Check: PASSED

All twelve modified files exist; commits `05fe2ff`, `1fb2209`, `8914af8`, `6071489` are in `git log` (`git rev-list --count 585fd9d..HEAD` is 4, measured from the on-disk ledger); `make test-ui` (all suites, about 68 s) and `make check` exit 0; the targeted verify commands of both tasks print `--- PASS` for `TestScopeTach`, `TestParityDisplayedValues`, `TestParityPageState`, `TestLayoutDesktop`, `TestLayoutNarrow` and `TestLayoutPhone`; `grep` finds `tach,state,fan,load` in `app.js`, `data-fmt="tach"`, `data-fmt="flow"` and `data-fmt="watt"` once each in `index.html`, `NULL_TEXT` and `const NONE = "--";` in `view.js`, and an em dash in `parity_test.go`; no file deletions in the task commits.

---
*Phase: 04-fan-model-v2*
*Completed: 2026-11-11*

---
phase: 03-live-web-ui
plan: 06
subsystem: ui
tags: [css, vanilla-js, leds, display-gamma, layout, breakpoints, csp, headless-chrome]

requires:
  - phase: 03-live-web-ui
    provides: "03-01/03-02 page with data-src/data-fmt readouts, view.js render(), store.js, uitest harness and bench; 03-05 web.Guard CSP (style-src 'self')"
provides:
  - "ui/web/app.css: UI-SPEC palette as custom properties, system font stack, 12/14/20/28 px and 400/600 only, 2 px corners, 44 px controls, focus ring, 64 px header, 2x2 port grid, 128 px strip, LED lenses, 1024 and 640 px breakpoints"
  - "view.js: ledColor(hex) (display gamma 2.2), ledCaption(i, segments), lens columns from leds.strip, connecting placeholder without data-src, ch1..ch4 and bad classes"
  - "index.html: stylesheet link, footer#strip with the LEDS group, template#led, header cell and value class hooks, an empty .scope well per port panel"
  - "uitest: TestLEDs, TestLEDCount, TestLEDsConnecting, TestLayoutDesktop, TestLayoutNarrow, TestLayoutPhone, TestNoVendorMarks; cdptest.Page.Screenshot"
affects: [03-07, 03-08, 03-10, 03-11, 03-12]

actuals:
  tokens: 11400
  tasks: 2
  commits: 4
plan_head_before: 0e4ba7ee74e3f64ecbf8a9ab35135c65b6fbed67
plan_head_after: f424e0930f336ae893b8c6a85b6681ae5a35a923
commits: 4

tech-stack:
  added: []
  patterns:
    - "computed colours reach the page only as CSSOM custom properties (el.style.setProperty --lens/--glow) with the look in the one stylesheet; no style attribute, no inline style element"
    - "a placeholder that stands in for values is not a value: the connecting LED lens is cloned from the template with data-src/data-fmt removed, so every [data-src] element reading `--` stays the single pre-snapshot rule"
    - "layout contracts are proven by measuring the real page in headless Chrome (geometry, computed style sweeps), not by reading the CSS"

key-files:
  created:
    - ui/web/app.css
    - uitest/leds_test.go
    - uitest/layout_test.go
    - .planning/phases/03-live-web-ui/03-06-layout-1280x720.png
    - .planning/phases/03-live-web-ui/03-06-layout-390x844.png
  modified:
    - ui/web/index.html
    - ui/web/js/view.js
    - ui/ui_test.go
    - internal/cdptest/cdptest.go

key-decisions:
  - "The LED lens look lives in the stylesheet (.lens, .lens.lit); view.js sets only --lens and --glow per lens through the CSSOM and toggles `lit` for the 10% highlight gradient, so the page runs under style-src 'self' with an empty console"
  - "Captions join the segment labels in sorted label order (the Go side marshals the map sorted, so API order and page order agree)"
  - "A strip of length 0 shows a dim `no LED strip` note instead of an unexplained empty well (UIC-17 zero case)"
  - "The panel template carries an empty `.scope` well (screen black, flex 1, min 128 px) so the 2x2 vertical budget is real now and 03-10 only has to draw into it"
  - "Header cell widths are fixed in px (instance 168, SIM TIME 144, TIME FACTOR 112, FIRMWARE 152, LINK 240) and the message line takes the rest; widths were sized for the widest fallback system font, and TestLayoutDesktop asserts no header value is clipped"
  - "Below 1024 px the message line's flex-basis is calc(100% - 260px) so it wraps with LINK onto the second header row"

patterns-established:
  - "SIL_UI_SHOTS=<dir> make test-ui writes layout screenshots (cdptest.Page.Screenshot)"
  - "TestLayoutDesktop's D-01 sweep (font sizes, weights, corner radius, shadow/gradient/filter outside .lens) covers every element later plans add to the page"

requirements-completed: [UI-03, UI-02, UI-05]

coverage:
  - id: D1
    description: "LEDS group shows one gamma-corrected, glowing lens per leds.strip entry with segment caption and the raw #rrggbb, repainting on leds events; dark lens for #000000"
    requirement: UI-03
    verification:
      - kind: automated_ui
        ref: "uitest/leds_test.go#TestLEDs"
        status: pass
      - kind: automated_ui
        ref: "uitest/leds_test.go#TestLEDCount"
        status: pass
    human_judgment: false
  - id: D2
    description: "Connecting state: one dark lens with em dash and #------ carrying no data-src/data-fmt; first snapshot switches to strip-length lenses with data-src leds.strip.<i>"
    requirement: UI-03
    verification:
      - kind: automated_ui
        ref: "uitest/leds_test.go#TestLEDsConnecting"
        status: pass
      - kind: automated_ui
        ref: "uitest/live_test.go#TestConnectingShowsDashes"
        status: pass
    human_judgment: false
  - id: D3
    description: "One-screen instrument front panel at 1280x720: 64 px header with fixed cells, 2x2 grid in channel colours, 128 px strip, UI-SPEC type scale, red/primary status colours, no effects outside the lenses"
    requirement: UI-03
    verification:
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutDesktop"
        status: pass
    human_judgment: false
  - id: D4
    description: "Below 1024 px one column and scroll with a two-row header; at 390 px the top row splits, nothing overflows sideways, controls are at least 44x44"
    requirement: UI-02
    verification:
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutNarrow"
        status: pass
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutPhone"
        status: pass
    human_judgment: false
  - id: D5
    description: "No vendor name or trademark in the page text, title or first-party files"
    requirement: UI-03
    verification:
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestNoVendorMarks"
        status: pass
    human_judgment: false
  - id: D6
    description: "Overall look reads as a sober bench-instrument front panel (taste, not measurable by the geometry tests)"
    requirement: UI-03
    verification: []
    human_judgment: true
    rationale: "Aesthetic fit to D-01 (restraint, no neon) is a judgment; screenshots 03-06-layout-1280x720.png and 03-06-layout-390x844.png are attached for review"

duration: 8min
completed: 2026-11-09
status: complete
---

# Phase 3 Plan 06: Instrument front panel and virtual LEDs Summary

**Dark bench-instrument front panel in one hand-written stylesheet (UI-SPEC palette, 12/14/20/28 px type, 2x2 channel-coloured port grid, 64/128 px header and strip, phone breakpoints) plus virtual LED lenses with display gamma 2.2, glow, segment captions and the raw hex, all under the CSP**

## Performance

- **Duration:** 8 min
- **Started:** 2026-11-09T13:50:07Z
- **Completed:** 2026-11-09T13:58:08Z
- **Tasks:** 2
- **Files modified:** 9 (plus 2 screenshots)

## Accomplishments

- LEDs: one round lens per `leds.strip` entry (0, 1 or many), lit with `round(255*(c/255)^(1/2.2))` per channel and a `0 0 calc(4px+12px*L) calc(2px*L) rgba(.., 0.6*L)` glow; `#000000` is the dark lens `#121416` with rim `#2a2e33` and no glow; a dim `#100800` renders visibly lit. Captions are the segment names covering the index (joined `, `, em dash for none), the hex line is the raw string, aria-label is `<caption> <hex>`. Leds events repaint through the existing requestAnimationFrame render.
- Connecting: one dark placeholder lens with `—` and `#------` and no `data-src`/`data-fmt`, so TestConnectingShowsDashes holds unchanged; the first snapshot swaps it for strip-length lenses.
- Front panel: dark-only palette as custom properties, system font stack, tabular numerals on every number via one rule, exactly four sizes and two weights, 2 px corners, 44 px minimum on button/select/input at every width, 2 px focus ring. 64 px one-row header with fixed-width cells (no cell moves between CONNECTING, LIVE and `DISCONNECTED, RETRYING`), 2x2 grid with the channel colour only on chip, 2 px top edge and the RPM/duty numerals, 128 px strip. FIRMWARE DISCONNECTED and LINK `DISCONNECTED, RETRYING` are red text; CONNECTED and LIVE stay primary.
- Breakpoints: below 1024 px one column, page scrolls, header wraps to two rows, strip stacks; below 640 px a panel's top row splits into chip + fan above the readouts.
- Test support: `cdptest.Page.Screenshot`; `SIL_UI_SHOTS=<dir> make test-ui` writes screenshots (attached: `03-06-layout-1280x720.png`, `03-06-layout-390x844.png`).

## Task Commits

TDD (RED test commit before each implementation):

1. **Task 1: Tracer, LED lenses** - `1cbfe81` (test, RED), `f7fe449` (feat)
2. **Task 2: Instrument front panel** - `3da3d64` (test, RED), `f424e09` (feat)

**Plan metadata:** recorded in the docs commit that follows this file.

## Files Created/Modified

- `ui/web/app.css` - palette, typography, header, port grid, panel, scope well, strip, LED lenses, breakpoints
- `ui/web/index.html` - stylesheet link, header cell classes, `footer#strip` with LEDS, `template#led`, `.scope` in the panel template
- `ui/web/js/view.js` - `ledColor`, `ledCaption`, lens build/paint, channel and `bad` classes
- `ui/ui_test.go` - `app.css` added to the embed contents check
- `internal/cdptest/cdptest.go` - `Page.Screenshot`
- `uitest/leds_test.go`, `uitest/layout_test.go` - the browser tests listed above

## Decisions Made

See `key-decisions` above. The tracer feedback gate: Task 1's `<verify>` (`make check` and the LED, offline and connecting tests) was re-run end to end before expanding and passed (auto/end-of-phase mode, automated-only verify).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added `cdptest.Page.Screenshot`**
- **Found during:** Task 2 (the plan asks to attach screenshots "the layout tests can capture with Page.captureScreenshot", but cdptest had no such method and `Page.send` is unexported)
- **Fix:** a 10-line `Screenshot` method; the layout tests write PNGs only when `SIL_UI_SHOTS` names a directory
- **Files modified:** internal/cdptest/cdptest.go
- **Committed in:** 3da3d64

**2. [Rule 2 - Missing critical] Added TestLEDCount and the zero-LED note**
- **Found during:** Task 1 (EDGE-UI-03 asks for zero/one/many lenses from the strip length; the plan listed no test for it)
- **Fix:** TestLEDCount covers strips of 0, 1 and 8; a zero strip shows a dim `no LED strip` note
- **Files modified:** uitest/leds_test.go, ui/web/js/view.js
- **Committed in:** 1cbfe81, f7fe449

**3. [Rule 3 - Blocking] `ui/ui_test.go` lists app.css**
- **Found during:** Task 1 (the embed-contents check would not notice a missing stylesheet)
- **Committed in:** f7fe449

---

**Total deviations:** 3 auto-fixed (2 blocking, 1 missing critical)
**Impact on plan:** additive only, no scope creep.

## Issues Encountered

None. Both tasks went green on their first implementation run; three repeated runs of the layout, LED and parity tests were stable.

## Known Stubs

- `ui/web/index.html` (port-panel template): the empty `.scope` well is a placeholder so the 2x2 vertical budget is real; 03-10 draws the scope into it. Recorded in `.planning/WINDOWS.md`.
- Header REAL TIME cell and the USR/ENVIRONMENT strip groups are absent by design; 03-08 and 03-11 add them (the stylesheet already carries the softkey, input, focus and 44 px rules they need).

## Threat Flags

None. No new endpoint, auth path or file access; API strings reach the DOM only through textContent, title and aria-label (T-03-19), and the page uses one external stylesheet plus CSSOM properties (T-03-20: TestCSPIsEnforced and every browser test run under the guard's CSP with an empty console).

## Edge and prohibition notes

Prohibition (no vendor marks) is now backed by TestNoVendorMarks. Edge assumptions: A-03-04 implemented as planned (one connecting lens, no data-src); EDGE-UI-03 covered for lens count, off/dim colours, segments covering none/one/several indexes and updates at the incoming rate; caption ellipsis with full name in `title` is in place but not measured by a test (long labels are firmware-codegen restricted). Stale greying remains 03-11.

## Self-Check: PASSED

All created files exist, all four task commits are in the log; `make check` exit 0 and `make test-ui` (16 tests) pass; acceptance greps (`1 / 2.2`, `href="/app.css"`, 4x `--chN:`, `min-height: 44px`) match.

## Next Phase Readiness

- 03-07 to 03-12 can build on `#strip` groups, `.scope`, the softkey/input/focus/disabled rules and the `ch1..ch4` panel classes.
- Concern for 03-08/03-11: new header cells must keep a fixed width (the 1024 px viewport leaves the message line about 220 px with today's cells); TestLayoutDesktop will catch a cell that moves.

---
*Phase: 03-live-web-ui*
*Completed: 2026-11-09*

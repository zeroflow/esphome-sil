---
phase: 03-live-web-ui
plan: 12
subsystem: testing
tags: [e2e, headless-chrome, acceptance, layout, readme, uat]

requires:
  - phase: 03-live-web-ui
    provides: "03-01..03-11: web listener, /ws, page, plots, inputs, USR holds, REAL TIME cell, stale overlay, cdptest, uitest bench, e2e helpers"
provides:
  - "e2e TestWebAcceptance: the whole page, offline, against the real Rev 3.3 firmware (9 subtests)"
  - "uitest TestLayoutDesktop / TestLayoutPhone measuring the real controls, strip groups and scopes"
  - "README Web UI section (URL, unauthenticated LAN default, --listen 127.0.0.1, --allow-host, sticky --web-port, --ui-dir, key redaction, socket-only operations)"
  - "REQUIREMENTS Out-of-Scope row 'Auth for the web UI | Trusted LAN by decision', PITFALLS 19 superseded note, UI-SPEC pointer-capture rule"
  - "Phase gate record and the UAT hand-over for /gsd-verify-work 3"
affects: [verify-work-03]

actuals:
  tokens: 6500
  tasks: 2
  commits: 2
plan_head_before: cd49620e3d12bdbf1ccacad39c9d053f96c7f798
plan_head_after: 7b55298e4f7df7aa648ce119217ec9971241f594
commits: 2

tech-stack:
  added: []
  patterns:
    - "one browser and one page shared by ordered subtests on one real instance; every CLI call names the instance"
    - "page store vs GET /api/v1/status compared by dotted path with skip and numeric-tolerance maps; RPM compared only after it has settled"

key-files:
  created: []
  modified:
    - e2e/web_test.go
    - uitest/layout_test.go
    - README.md
    - .planning/REQUIREMENTS.md
    - .planning/research/PITFALLS.md
    - .planning/phases/03-live-web-ui/03-UI-SPEC.md

key-decisions:
  - "parity_real waits until every RPM has settled (two status reads 1 s apart within 0.5 rpm) before comparing store and status within 1 rpm, because the fan swapped one subtest earlier is still spinning up"
  - "The LED group is asserted by its natural height (children plus gaps, measured 108 px) against the strip content box (110 px), not by the stretched group box"

patterns-established:
  - "Acceptance of a UI against the real stack: drive with real mouse and key events through CDP, observe the firmware through `sil status` and the native-API client, never through the page"

requirements-completed: [UI-01, UI-02, UI-03, UI-04, UI-05, UI-06]

duration: 19min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "Page loads offline against the real firmware: LIVE, four panels, five LED lenses, empty console, every recorded request same-origin including /ws"
    requirement: UI-01
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/loads_offline"
        status: pass
    human_judgment: false
  - id: D2
    description: "LED hex texts equal sil status leds.strip"
    requirement: UI-03
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/leds_match"
        status: pass
    human_judgment: false
  - id: D3
    description: "Holding USR1 about 300 ms and USR2 about 1.5 s on the page moves the firmware's duties exactly as `sil press usr1` and `sil press usr2 --long` (0.4/0/0/0 to 0.5/0.1/0.1/0.1, then all 1.0)"
    requirement: UI-04
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/usr1_like_cli"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/usr2_long_like_cli"
        status: pass
    human_judgment: false
  - id: D4
    description: "Temperature 26 typed on the page reaches `sil status` and the firmware's Temperature entity over the native API (the Home Assistant proxy)"
    requirement: UI-04
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/env_reaches_firmware"
        status: pass
    human_judgment: false
  - id: D5
    description: "A fan swap from the page shows in sil status and re-scales that port's plot to the new fan's max_rpm"
    requirement: UI-02
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/fan_swap"
        status: pass
    human_judgment: false
  - id: D6
    description: "Page store equals GET /api/v1/status on the web listener (except sim.time_s, sim.behind_real_time, RPM within 1), REAL TIME cell is the realtime format of the store value, every data-src path exists in the JSON"
    requirement: UI-06
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/parity_real"
        status: pass
    human_judgment: false
  - id: D7
    description: "The open page recovers without a reload across sil sim restart (new history request, LIVE) and sil down + sil up of the same instance (DISCONNECTED, RETRYING, then LIVE, same instance name)"
    requirement: UI-05
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/sim_restart"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebAcceptance/down_up"
        status: pass
    human_judgment: false
  - id: D8
    description: "At 1280 x 720 the page does not scroll and every strip group, the LED group included, fits the strip content; at 390 x 844 all ten real controls are at least 44 x 44 and every scope at least 128 px high"
    requirement: UI-01
    verification:
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutDesktop"
        status: pass
      - kind: automated_ui
        ref: "uitest/layout_test.go#TestLayoutPhone"
        status: pass
    human_judgment: false
  - id: D9
    description: "README documents the web UI and what it exposes; REQUIREMENTS and PITFALLS 19 no longer claim localhost-only"
    requirement: UI-06
    verification:
      - kind: command
        ref: "grep -n 'unauthenticated' README.md; grep -n 'Trusted LAN' .planning/REQUIREMENTS.md; grep -n 'Superseded (Phase 3' .planning/research/PITFALLS.md"
        status: pass
    human_judgment: false
  - id: D10
    description: "Overall visual character: dark sober bench-instrument look, channel colours, only glow on the LEDs, no vendor names (D-01); screenshots 03-06-layout-1280x720.png and 03-06-layout-390x844.png"
    requirement: UI-01
    verification: []
    human_judgment: true
    rationale: "Aesthetic fit to D-01 is a judgment; UAT item 6"
  - id: D11
    description: "The 50 ms / 2 s behind-real-time thresholds (A-03-05, A-03-06) feel right on a loaded host"
    requirement: UI-05
    verification: []
    human_judgment: true
    rationale: "Planner assumptions; the flip logic is proven on the Manual clock (03-07 TestBehindRealTime), the feel on a loaded machine is a person's call; UAT item 8"
  - id: D12
    description: "A real phone browser: touch hold, slide-off release, own-capture guard, layout, LAN reachability from another machine, Home Assistant following the page"
    requirement: UI-04
    verification: []
    human_judgment: true
    rationale: "Chrome's emulated touch is tested; Safari/Firefox capture behaviour and the LAN path need a phone; UAT items 1-5 and 9"
  - id: D13
    description: "UIC-10 minutes-long soak: plots stay flat in memory"
    requirement: UI-02
    verification: []
    human_judgment: true
    rationale: "Rings are proven bounded by unit tests (1,000,000 pushes); a heap reading over minutes is the verifier's check; UAT item 7"
---

# Phase 3 Plan 12: Acceptance of the Whole Page Summary

**One e2e acceptance test drives the page offline in headless Chrome against the real Rev 3.3 firmware (LEDs, USR holds, temperature to the Home Assistant proxy, fan swap, store-vs-status parity, sim restart, down/up) next to a layout test over the real controls; the README now says plainly that the LAN page and API are unauthenticated; the full phase gate is green.**

## Performance

- **Duration:** about 19 min wall (of which the unfiltered `make e2e` took 11 min)
- **Started:** 2026-11-09T15:10Z (estimate; the start time was not captured by variable)
- **Completed:** 2026-11-09T15:29Z
- **Tasks:** 2 (1 tracer, 1 auto)
- **Files modified:** 6 (none created)

## Accomplishments

- **TestWebAcceptance** (instance `e2e-webacc`, rev33, offline Chrome 157). Warm run: 26.7 s for the test, 22 s after the instance was up. All nine subtests pass, twice in a row on the targeted run and once more inside the full `make e2e`: loads_offline 0.06 s, leds_match 3.0 s, usr1_like_cli 1.5 s, usr2_long_like_cli 1.6 s, env_reaches_firmware 4.4 s, fan_swap 0.02 s, parity_real 9.1 s, sim_restart 0.6 s, down_up 1.6 s.
- **Layout over the real controls.** Desktop 1280 x 720: no scroll; strip content box 110 px (128 minus 2 x 1 border minus 2 x 8 padding); three groups (leds, usr, env) each inside it; the LED group's natural height measured 108 px (children plus gaps), the box itself does not scroll; five lenses and six strip controls. Phone 390 x 844: exactly ten controls on the page (4 fan selects, 3 USR caps, TEMPERATURE, HUMIDITY, APPLY VALUES), all at least 44 x 44 and inside the viewport, four scopes at least 128 px high, no sideways overflow.
- **Docs.** README "Web UI" section and status line; REQUIREMENTS row now "Auth for the web UI | Trusted LAN by decision (Phase 3 D-08) ..."; PITFALLS 19 "Superseded (Phase 3 D-07/D-08)" note naming Host allowlist, Origin checks, key redaction, socket-only lifecycle and logs, connection and stream caps; UI-SPEC rule says no `setPointerCapture` and to release the touch's implicit capture in `pointerdown` (matches `ui/web/js/buttons.js`).
- **Threat model.** T-03-35 mitigated (README states it plainly, same wording as the `sil up` notice from 03-05). T-03-SC/03-12: no dependency added; `make lock-check` and `make mod-verify` pass.

## Task Commits

1. **Task 1 (tracer): TestWebAcceptance and the final layout tests** - `f2bdf5d` (test)
2. **Task 2: README, REQUIREMENTS, PITFALLS 19, UI-SPEC** - `7b55298` (docs)

Tracer gate: the tracer `<verify>` carries only automated checks (end-of-phase mode, no checkpoint). `make check`, `make test-ui UI_RUN=TestLayout` and `make e2e E2E_RUN=TestWebAcceptance` passed before the docs task; the tracer verified end-to-end, expanding.

## Acceptance Record: Phase Gate

Run after both commits, each command on its own, from `/home/claude/esphome-sil` on `claude/main`:

| Command | Result |
|---------|--------|
| `make check` | exit 0: fmt-check, vet (incl. both tag builds), `go test -race ./...` all packages ok, C++ codec test `119 passed, 0 failed` |
| `make test-cpp-image` | exit 0: g++ 14.2.0 of the pinned image, `119 passed, 0 failed` |
| `make test-ui` | exit 0: 44 tests `--- PASS`, 0 `--- FAIL` (58 s); `TestLayoutDesktop`, `TestLayoutNarrow`, `TestLayoutPhone` pass |
| `make e2e` (full, unfiltered) | exit 0: `ok github.com/zeroflow/esphome-sil/e2e 662.626s`, 57 top-level `--- PASS`, 0 `--- FAIL`; `--- PASS: TestWebAcceptance/down_up` present; all Phase 1 and Phase 2 e2e tests green |
| `make lock-check` | exit 0: `"missing": []`, `"mismatched": []` |
| `make test-contract` | exit 0: 6 passed in 23.2 s (`test_api_smoke` 3, `test_contract` 1, `test_identity` 2); contract session 23.2 s against the 60 s warm budget |
| `make mod-verify` | exit 0: `all modules verified` |

## Human UAT Checklist (hand-over for /gsd-verify-work 3, record results in 03-UAT.md)

Collected from the plan and from the human-judgment items of 03-06, 03-07, 03-10 and 03-11.

1. `sil up uat` on the bench machine; open the `web.url` it prints on a phone on the same LAN (also proves LAN reachability from another device; optionally also from a second PC): the page fits the phone width, scrolls in one column, plots move and LINK reads LIVE.
2. Touch and hold USR2 for about 2 s: all four fans go to 100 % (and, with Home Assistant connected to the instance, its Fan N Speed entities follow).
3. Touch USR1, slide the finger off the cap and keep it on the screen: the cap goes dark within a moment (no stuck hold). Do this in the phone's real browser (Safari and/or Firefox as well as Chrome): Chrome 157's emulated touch is tested, a real browser's capture behaviour is not (03-11). Also touch the cap, then bring up a system overlay or switch app: it must not stay lit (the 10 s max hold is the backstop).
4. Set TEMPERATURE to 26 on the phone and press Enter: Home Assistant's Temperature entity shows 26.
5. Run `sil sim restart uat` on the bench machine: the phone page recovers by itself without a reload.
6. Look at the desktop page at 1280 x 720 or larger (screenshots `03-06-layout-1280x720.png` and `03-06-layout-390x844.png` are in the phase directory): dark, sober bench-instrument look, channel colours per port, the only glow on the LED lenses, no vendor names (D-01). Overall visual character is the 03-06 judgment item.
7. UIC-10 soak (03-10): leave the page open for several minutes (10 or more) with a fan running; watch the browser tab's memory (Chrome task manager, or DevTools Memory): it stays flat and the plots keep their 60 s window.
8. Behind-real-time thresholds (03-07, A-03-05 / A-03-06): load the host (for example a parallel `make e2e` or a compile) and watch the REAL TIME cell: BEHIND appears when the simulator is late by 50 ms or more and returns to IN SYNC about 2 s after it caught up, without flickering; say whether the thresholds feel right or should change.
9. From a second machine, try `sil up uat --listen 127.0.0.1` followed by the same URL: the page must not be reachable from the LAN (README claim).

## Files Created/Modified

- `e2e/web_test.go` - `TestWebAcceptance`, helpers `accLookup`, `accDiff`, `accPathMatch`
- `uitest/layout_test.go` - strip-group and LED-group measurement in `TestLayoutDesktop`; real controls and scopes in `TestLayoutPhone`
- `README.md` - Web UI section, status line, test note
- `.planning/REQUIREMENTS.md`, `.planning/research/PITFALLS.md`, `.planning/phases/03-live-web-ui/03-UI-SPEC.md` - wording changes only

## Decisions Made

See `key-decisions`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] parity_real compared a still-moving RPM**
- **Found during:** Task 1, first run of `TestWebAcceptance`
- **Issue:** after the fan swap of the previous subtest, port 3's RPM was still spinning up to the new fan's top speed (page store 1976.9, status 1986.7 about 20 ms later); a 1 rpm tolerance cannot hold on a ramp, so the 5 s quiet period of the plan was not quiet for that port.
- **Fix:** keep the 5 s wait and then wait until every RPM has settled (two `sil status` reads 1 s apart within 0.5 rpm, up to 30 reads) before the comparison. Tolerance of the comparison stays 1 rpm.
- **Files modified:** `e2e/web_test.go`
- **Commit:** `f2bdf5d`

**2. [Plan figure] LED group height**
- The plan states the LED group as 106 px in a 112 px strip content. Measured: strip content 110 px (border and padding of the 128 px strip), LED group natural height 108 px, and the group's own box is stretched to 110. The test asserts the real relation (natural height at most the content box, group inside the strip, no inner scroll) and logs the numbers; the CSS did not change.

**Total deviations:** 1 auto-fixed (Rule 1), 1 figure correction. **Impact:** none on scope; test-only changes.

## Issues Encountered

- Waiting loops that match their own command line with `pgrep -f` never end; the full e2e run was polled with a bounded file check instead. No effect on the results.
- `fan_swap` takes 0.02 s: the select change, the POST, the status flip and the rescale of the scope are all local; the subtest still asserts the new fan in `sil status`, the select value and the scope's RPM axis at the new `max_rpm`, so it is not vacuous.

## Known Stubs

None.

## Threat Flags

None. No new network endpoint or trust boundary; this plan adds tests and documentation only.

## Next Phase Readiness

- Phase 3 is ready for `/gsd-verify-work 3`: all automated success criteria are shown against the real firmware, and the nine UAT items above are the open human checks (phone, Home Assistant, visual character, soak, thresholds, LAN reachability).
- `.planning/WINDOWS.md` has no open entries (2 fixed); this plan adds none.

## Self-Check: PASSED

- Files exist: `e2e/web_test.go`, `uitest/layout_test.go`, `README.md`, `.planning/REQUIREMENTS.md`, `.planning/research/PITFALLS.md`, `.planning/phases/03-live-web-ui/03-UI-SPEC.md`.
- Commits `f2bdf5d` and `7b55298` exist on `claude/main`; `git rev-list --count cd49620..HEAD` was 2 at SUMMARY time.
- Acceptance: `grep -n 'unauthenticated' README.md` (line 92) and `grep -n -- '--listen 127.0.0.1' README.md` (lines 104, 112) match; `grep -n 'Trusted LAN' .planning/REQUIREMENTS.md` (line 110) and `grep -n 'Superseded (Phase 3' .planning/research/PITFALLS.md` (line 304) match; `--- PASS` for all nine TestWebAcceptance subtests and for TestLayoutDesktop and TestLayoutPhone; the full gate exits 0 command by command.

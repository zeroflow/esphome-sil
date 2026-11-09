---
phase: 03-live-web-ui
verified: 2026-11-09T17:30:00Z
status: human_needed
score: 9/9 must-haves verified
covered_files:
  - .planning/phases/03-live-web-ui/03-01-PLAN.md
  - .planning/phases/03-live-web-ui/03-01-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-02-PLAN.md
  - .planning/phases/03-live-web-ui/03-02-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-03-PLAN.md
  - .planning/phases/03-live-web-ui/03-03-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-04-PLAN.md
  - .planning/phases/03-live-web-ui/03-04-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-05-PLAN.md
  - .planning/phases/03-live-web-ui/03-05-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-06-PLAN.md
  - .planning/phases/03-live-web-ui/03-06-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-07-PLAN.md
  - .planning/phases/03-live-web-ui/03-07-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-08-PLAN.md
  - .planning/phases/03-live-web-ui/03-08-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-09-PLAN.md
  - .planning/phases/03-live-web-ui/03-09-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-10-PLAN.md
  - .planning/phases/03-live-web-ui/03-10-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-11-PLAN.md
  - .planning/phases/03-live-web-ui/03-11-SUMMARY.md
  - .planning/phases/03-live-web-ui/03-12-PLAN.md
  - .planning/phases/03-live-web-ui/03-12-SUMMARY.md
  - README.md
  - e2e/web_test.go
  - internal/control/control.go
  - internal/control/operations.go
  - internal/engine/engine.go
  - internal/events/events.go
  - internal/supervisor/history.go
  - internal/supervisor/supervisor.go
  - internal/web/guard.go
  - internal/web/listener.go
  - internal/web/web.go
  - internal/web/ws.go
  - ui/web/app.css
  - ui/web/index.html
  - ui/web/js/api.js
  - ui/web/js/app.js
  - ui/web/js/buttons.js
  - ui/web/js/inputs.js
  - ui/web/js/message.js
  - ui/web/js/plot.js
  - ui/web/js/ring.js
  - ui/web/js/store.js
  - ui/web/js/view.js
  - uitest/parity_test.go
covered_digest: "v2:sha256:3bbfe3ba6054391a1371d23ebb0fc6494879f1137c1a4ec71cffadff89fecde5"
behavior_unverified: 0
overrides_applied: 0
human_verification:
  - test: "Phone on the LAN. `sil up uat`, open the printed `web.url` on a phone on the same LAN (optionally a second PC too)."
    expected: "The page loads from another device, fits the phone width, scrolls in one column, plots move, LINK reads LIVE."
    why_human: "Reach from another machine needs a real LAN path and device. Automated proxy only: TestWebReach / TestWebAcceptance reach the listener from the same host."
  - test: "Touch and hold USR2 for about 2 s on the phone page (UAT 2), with Home Assistant connected to the instance if possible."
    expected: "All four fans go to 100 %, and HA's Fan N Speed entities follow, exactly as for `sil press usr2 --long`."
    why_human: "Real touch and real HA. Automated proxy only: TestWebAcceptance/usr2_long_like_cli uses a CDP mouse hold and the native-API client, not a phone or HA."
  - test: "Touch slide-off on a real phone browser (UAT 3): touch USR1, slide the finger off the cap and keep it on the screen; also touch the cap and then bring up a system overlay or switch app."
    expected: "The cap goes dark within a moment (no stuck hold); after a system overlay it does not stay lit (the 10 s max hold is the backstop). Try Safari and/or Firefox as well as Chrome."
    why_human: "Pointer-capture behaviour of real mobile browsers cannot be exercised by Chrome 157 emulated touch (TestUSRTouchLeave)."
  - test: "Temperature reaches HA (UAT 4): set TEMPERATURE to 26 on the phone page and press Enter."
    expected: "Home Assistant's Temperature entity shows 26."
    why_human: "Automated proxy only: TestWebAcceptance/env_reaches_firmware reads the firmware's Temperature entity over the native API (the same library HA uses), not a real HA instance."
  - test: "Sim restart recovery on a real phone (UAT 5): run `sil sim restart uat` on the bench machine while the phone page is open."
    expected: "The phone page recovers by itself without a reload (stale overlay, then LIVE, plots repaint)."
    why_human: "Automated proxy only: TestWebAcceptance/sim_restart and TestResyncOnSimRestart use headless Chrome on the same host; a phone on WiFi (sleep, throttled timers) differs."
  - test: "Visual character (UAT 6): look at the desktop page at 1280 x 720 or larger (screenshots 03-06-layout-1280x720.png, 03-06-layout-390x844.png are in the phase directory)."
    expected: "Dark, sober bench-instrument look (D-01), channel colours per port (yellow, green, blue, magenta), the only glow on the LED lenses, no vendor names."
    why_human: "Aesthetic fit to D-01 is a judgment. Automated: palette tokens, TestNoVendorMarks, layout measurements only."
  - test: "UIC-10 minutes-long soak (UAT 7): leave the page open 10 minutes or more with a fan running; watch the tab's memory (Chrome task manager or DevTools Memory)."
    expected: "Memory stays flat and the plots keep their 60 s window."
    why_human: "Automated proxy only: ring bounds are unit-tested (TestRingBounded, 1,000,000 pushes; TestHistorySoak on the server ring); a heap reading over minutes was not taken."
  - test: "Behind-real-time thresholds A-03-05/06 on a loaded host (UAT 8): load the host (parallel `make e2e` or a compile) and watch the REAL TIME cell."
    expected: "BEHIND appears when the simulator is 50 ms or more late and returns to IN SYNC about 2 s after it caught up, without flicker; judge whether 50 ms / 2 s feel right."
    why_human: "Thresholds are planner assumptions; the flip logic is proven only on the Manual clock (TestBehindRealTime), and TestHeaderRealTime proves the cell, not the feel under load."
  - test: "`--listen 127.0.0.1` reachability (UAT 9): from a second machine, `sil up uat --listen 127.0.0.1` and open the same URL."
    expected: "The page is not reachable from the LAN (README claim)."
    why_human: "Needs a second machine. Automated proxy only: the listener binds the given address (internal/instance ListenWeb, webport tests); no cross-machine probe was run."
  - test: "Semantics of a timed-out button down (review WR-01 fix bfbb808, IN-06, IN-07): stall or delay a button.down on a real host and observe the cap and the next press."
    expected: "A down that times out after 3 s counts as never registered: no up is sent, the cap shows only the server's report, the engine's 10 s max hold releases a late-applied hold; a late-applied down (> 3 s) can make the next press fail `button_busy`. Decide whether that is acceptable."
    why_human: "Design semantics, not a defect either way. Automated proxy only: TestUSRStalledRequestTimesOut against the fake bench."
---

# Phase 3: Live Web UI Verification Report

**Phase Goal:** The developer watches and drives a running bench from a local web page that is a pure client of the same API the CLI uses.
**Verified:** 2026-11-09T17:30:00Z
**Status:** human_needed
**Re-verification:** No, initial verification
**HEAD:** 5ae4ac2 (claude/main)

## Goal Achievement

The goal is achieved in the codebase. Every roadmap success criterion and every user decision I was asked to check (D-08, D-18, D-19, D-20, plus D-07, D-12 to D-15 spot-checked) is backed by code I read and by tests I ran myself. No gap was found. The status is `human_needed` only because ten judgment items remain open, none of which an automated test can settle. For each, an automated proxy exists and is named below; none is counted as passed.

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC1: Opening the simulator's local URL shows a page served from the `sil` binary; it keeps working with the network blocked and updates live over WebSocket | VERIFIED | `ui/embed.go` has `//go:embed all:web` and `ui.FS()`; the supervisor serves it through `web.NewHandler` (`supervisor.go:301-304`, with `--ui-dir` override at `:237`). `index.html` references only `/vendor/*`, `/app.css` and `/js/app.js`. I grepped for `http(s)://`, `@import` and `url(` outside `vendor/` and found none. uPlot 1.6.32 sha256 matches `VENDOR.md` (19c8d4c6.../df630c6a.../8f989229...). `app.js` opens `new WebSocket(scheme + "//" + location.host + "/ws?kind=...")` with 500 ms to 5 s backoff. I ran `TestOfflineLoadAndLive`, `TestNoCSPViolations`, `TestParityRequests` (offline headless Chrome, same-origin-only request check, CSP enforced): pass. I also ran `TestWebAcceptance/loads_offline` against the real firmware: pass. |
| 2 | SC2: Per fan port the page shows the plugged fan (changeable from the page), duty, RPM, and a live duty/RPM plot whose history stays bounded | VERIFIED | `index.html` port template: `select.fan` (`data-src ports.{i}.fan`), RPM and duty readouts, `.scope` with RPM solid and DUTY dashed legend (D-11). `inputs.js` fills the select from `GET /api/v1/fans` and applies a swap through `POST /api/v1/set` immediately (D-16). `plot.js` fixes the axes at 0..`max_rpm` and 0..100 (D-13) over a 60 s window (D-12). Page rings are bounded: `RING_CAP 620 + RING_SLACK 64`, trimmed in `ring.js`. Server ring is bounded: `events.HistoryCap = 3000` (5 min at 10 Hz), a fixed-size array with wraparound, served by `GET /api/v1/history` (`supervisor/history.go`). Ran `TestFanSelect`, `TestFixedAxes`, `TestRingBounded`, `TestHistorySoak` and `TestWebAcceptance/fan_swap` (swap visible in `sil status`, scope re-scaled to the new `max_rpm`): pass. The multi-minute soak is human item 7. |
| 3 | SC3: The page shows the virtual LEDs with current colours, short and long press for USR1-3, and temperature/humidity setting; the firmware (and HA) react exactly as to the same CLI commands | VERIFIED (HA and phone aspects are human items) | LED lenses and `#rrggbb` captions are in the `#led` template, driven by `leds.strip.{i}` and the manifest. USR1-3 are press-and-hold caps (`buttons.js`: pointerdown posts `button.down` with `max_hold_ms: 10000`, release paths post `button.up` with `keepalive`); there are no preset softkeys (D-14). `inputs.js` commits temperature/humidity on Enter or APPLY VALUES as one `set` (D-16), and errors are shown verbatim (D-17). `TestWebAcceptance` ran independently against the real Rev 3.3 firmware: `leds_match` (hex equals `sil status leds.strip`), `usr1_like_cli`, `usr2_long_like_cli` (page hold moves the firmware duties exactly as `sil press usr1` / `sil press usr2 --long`), `env_reaches_firmware` (26 reaches `sil status` and the firmware Temperature entity over the native API): all pass. Real HA, real phone touch: human items 1 to 4. |
| 4 | SC4: The page shows sim time, the time factor (fixed at 1), whether the firmware is connected, and a warning when the simulator falls behind real time | VERIFIED | The header has SIM TIME (`sim.time_s`), TIME FACTOR (`sim.time_factor`), FIRMWARE (`firmware.bus_connected`) and REAL TIME (`sim.behind_real_time`) cells, all `data-src` bound to status paths. `engine.go` computes `behind` from tick lateness (`behindLate 50 ms`, `behindClear 2 s`) and emits `sim behind/in_sync`; `store.js` only folds the server's event and derives nothing. Ran `TestBehindRealTime` and `TestBehindRealTimeManualClockStaysInSync` (engine, manual clock) and `TestHeaderRealTime` (browser): pass. `TestWebAcceptance/parity_real` checks the REAL TIME cell against the store value. Thresholds on a loaded host: human item 8. |
| 5 | SC5: An automated parity check confirms that every value the page displays and every action it performs is available through the CLI/API with the same JSON | VERIFIED | Four layers. (a) `control.Operations` is the single table behind api.sock, the LAN handler and the CLI; `internal/cli/operations_test.go` proves every row has a working `sil` command. (b) `ui/ui_test.go TestJSOpsInOperations` checks each `[method, path]` pair in `js/api.js` OPS is a LAN row of the table, with a self-check that it cannot pass by finding nothing. (c) `uitest/parity_test.go` `TestParityRequests` records every request of a real Chrome session and requires each to be a page file or a LAN row, and each of `pageOps` to be seen; `TestParityDisplayedValues` and `TestParityPageState` compare displayed text and store with `GET /api/v1/status`. (d) `TestWebAcceptance/parity_real` on the real firmware diffs the page store against `GET /api/v1/status` on the web listener by dotted path, and confirms every `data-src` path exists in the JSON. I ran all of these: pass. The one documented JSON difference is `api.key` (D-18). |
| 6 | D-08: No auth or token; Origin check on the WS upgrade and on mutating requests; unauthenticated LAN documented in README and `sil up` output | VERIFIED | `guard.go`: Host allowlist (DNS-rebinding), CSP, nosniff, DENY framing, and `http.CrossOriginProtection` returning 403 `origin_rejected` for cross-site unsafe methods. The `/ws` upgrade uses `websocket.Accept(w, r, nil)`, whose default origin check is on. Wiring at `supervisor.go:301-304`: `Serve(Limit(ln), LimitBody(Guard(NewHandler)))`. There is no token or auth anywhere. README line 92 says "The page and the HTTP API are unauthenticated"; `lifecycle.go:253` prints the same in `sil up`; the CLI `--listen` help says so too. `guard_test.go` includes `TestCSRFCoversEveryMutatingRoute`. `internal/web` tests: pass. |
| 7 | D-18: `api.key` is redacted on the TCP/web listener and shown only over `api.sock` | VERIFIED | `web.go` wraps the Control in `redacting`, which replaces `API.Key` with `<redacted>` in `Status`, `SimRestart`, `FwRestart` and the snapshot line of `Watch`; both `/ws` and the LAN API are built over `rc`. Only `Status` carries a key field (`control.go:97`). Unit tests in `internal/web` and `internal/supervisor` (run by me: pass) cover the wrapper. The e2e assertions that TCP `api.key` and the snapshot key equal `<redacted>` (`e2e/web_test.go:180-231`, subtest `status_redacted`) and that the socket returns the real key (`:760`) live in `TestWebTracer`/`TestWebReach`, which I did not re-run; they are green in the orchestrator's full `make e2e`. |
| 8 | D-19: Lifecycle operations (`down`, `sim restart`, `fw restart`) are socket-only; status, watch/ws, history, fans, set and press (incl. down/up/max-hold) are reachable on TCP | VERIFIED | `operations.go`: `down`, `sim.restart` and `fw.restart` have `LAN: false`; `NewLANHandler` answers them 403 `operation_not_allowed` without ever calling the Control; the other rows are `LAN: true`. Tests: `control_test.go:605` and `internal/web/web_test.go:147` (run by me: pass); the real-instance check at `e2e/web_test.go:309` is in the orchestrator's full `make e2e` run, not re-run by me. |
| 9 | D-20: `logs` (tail and follow) is socket-only on TCP, refused with `operation_not_allowed` | VERIFIED | `operations.go`: `{Name: "logs", ... LAN: false}`; the doc comment names D-20. The same tests cover it (the e2e loop iterates all non-LAN rows). `sil logs` still goes over api.sock (`cli.go:61`). |

**Score:** 9/9 truths verified (0 present, behavior-unverified).

Other decisions spot-checked in the code:

- D-07: the `--listen` default is `0.0.0.0` (`cli.go:71`, `supervisor.go:55`).
- D-09: sticky web port from 8054 (`instance.go:35`, `AllocateWebPort`, `ListenWeb` fail-closed with `port_in_use`). The web listener is a separate TCP listener from `api.sock`, but both serve the same `control.Operations` table, as D-19/D-20 prescribe. This is not a violation of "no second listener" in spirit.
- D-12: server history ring is bounded at 3000 samples.
- D-14, D-15: no preset softkeys; max hold is in the API (`DownRequest.max_hold_ms`), auto-released by the engine (`TestMaxHold` passes), and the page always sends 10000 ms.
- D-10: stale overlay and reconnect (`TestStaleAndReconnect`, `TestResyncOnSimRestart`, `TestReconnectBackoff`).
- D-01, D-02, D-03: dark only, no `prefers-color-scheme`; channel colours `--ch1..4` are yellow, green, blue and magenta; `TestNoVendorMarks`.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `ui/embed.go` | `go:embed` of `web/` | VERIFIED | `//go:embed all:web`, `FS()` |
| `ui/web/index.html`, `app.css`, `js/*.js` (9 modules, 2293 lines) | Page, no build step | VERIFIED | Substantive and wired from `app.js` as an ES module. No TODO, HACK, FIXME or placeholder markers. |
| `ui/web/vendor/*` | Vendored uPlot 1.6.32 | VERIFIED | Hashes re-computed, equal to `VENDOR.md`; `TestVendorHashes` runs in `make check`. |
| `internal/web/{web,guard,listener,ws}.go` | Redaction, guard, caps, WS | VERIFIED | Read in full; all wired in `supervisor.go`. |
| `internal/control/operations.go` | Operation table with LAN flags | VERIFIED | 13 rows, init-time uniqueness check. |
| `internal/events/events.go` and `internal/supervisor/history.go` | Bounded history ring and API | VERIFIED | `HistoryCap = 3000`, ring array. |
| `internal/engine/engine.go` | Samples, behind-real-time, max hold | VERIFIED | `sampleStep`, `behindLate`, `behindClear` and `noteTick` present and tested. |
| `uitest/*`, `e2e/web_test.go` | Browser and real-firmware tests | VERIFIED | Ran the named subsets below. |
| `README.md` Web UI section | Unauthenticated LAN documented | VERIFIED | Line 92. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `app.js` | `/ws` | `new WebSocket` | WIRED | Server side: `web.go` mounts `GET /ws` with `newWS` |
| `app.js`, `inputs.js`, `buttons.js` | LAN rows of the operation table | `api.js` `call()` over the OPS table | WIRED | `TestJSOpsInOperations`, `TestParityRequests` |
| `supervisor.go` | `web.NewHandler` | `Serve(Limit(LimitBody(Guard(...))))` | WIRED | `supervisor.go:301-304` |
| `web.redacting` | `/ws` and LAN API | handler built over `rc` | WIRED | `web.go:42-50` |
| `buttons.js` | engine max-hold release | `max_hold_ms` in the `button.down` body | WIRED | `opButtonDown` to `SetButton`; `TestMaxHold` |
| `app.js` | `GET /api/v1/history` | `call("history")` on every (re)connect | WIRED | `supervisor/history.go`; `TestHistoryJoin` |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| Port readouts | `ports[i].rpm`, `duty`, `fan` | WS snapshot and events from `Control.Watch`, from the engine | Yes (`TestWebAcceptance/parity_real` equals `sil status` on the real firmware) | FLOWING |
| Scopes | `Ring` | `sample` frames plus `GET /api/v1/history` | Yes (`TestHistoryJoin`, `TestScopeLive`, `TestWebPlot` in e2e) | FLOWING |
| LEDs | `leds.strip` | `leds` events | Yes (`leds_match` against real firmware) | FLOWING |
| Header run info | `sim.time_s`, `behind_real_time`, `firmware.bus_connected` | Engine and broker | Yes | FLOWING |
| USR caps | `buttons.usr*` | Server-reported held state | Yes (never local state) | FLOWING |

### Behavioral Spot-Checks (run by this verifier at HEAD 5ae4ac2)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Go packages of the phase | `go test -count=1 ./internal/web ./internal/control ./ui ./internal/supervisor ./internal/engine ./internal/cli` | all `ok` | PASS |
| Page in headless Chrome, offline, parity, plot, layout, holds, fan select, env | `go test -tags sil_ui -run 'TestParity\|TestOfflineLoadAndLive\|TestRingBounded\|TestFixedAxes\|TestLEDs$\|TestLayoutDesktop\|TestHeaderRealTime\|TestUSRHold$\|TestEnvSet\|TestFanSelect$\|TestNoCSPViolations' ./uitest/` | `ok` 9.95 s | PASS |
| Whole page against the real Rev 3.3 firmware | `go test -tags sil_e2e -run TestWebAcceptance ./e2e/` | `ok` 27.4 s | PASS |
| Engine behind-real-time and max hold; server history ring | `go test -run 'TestBehindRealTime\|MaxHold\|TestHistory\|TestRing' -v ./internal/engine ./internal/events` | `TestBehindRealTime`, `TestBehindRealTimeManualClockStaysInSync`, `TestMaxHold`, `TestHistoryEmptyRing`, `TestHistorySoak` PASS | PASS |

I did not re-run the full `make e2e` (668 s), `make check`, `make test-ui` in full or `make test-contract`. I rely on the orchestrator's evidence for those (green at HEAD). The targeted subsets above are my own runs.

### Probe Execution

Step 7c: SKIPPED. The phase declares no `probe-*.sh` scripts.

### Requirements Coverage

Every requirement ID in the PLAN frontmatter is accounted for in `REQUIREMENTS.md` (marked `[x]` and mapped "Phase 3 / Complete"). No requirement mapped to Phase 3 is missing from the plans, so there are no orphaned requirements.

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| UI-01 | 03-01, 02, 03, 04, 05, 09, 10, 12 | Local page embedded in the binary, no build, vendored, no CDN, live over WS | SATISFIED | Truth 1 |
| UI-02 | 03-02, 06, 07, 08, 09, 10, 12 | Per port: fan select, duty, RPM, bounded live plot | SATISFIED | Truth 2 (soak is a human item) |
| UI-03 | 03-06, 12 | Virtual LEDs with current colour | SATISFIED | Truth 3, `TestLEDs`, `leds_match` |
| UI-04 | 03-07, 08, 11, 12 | Short/long press USR1-3, set temperature/humidity | SATISFIED | Truth 3, `usr1_like_cli`, `usr2_long_like_cli`, `env_reaches_firmware` (phone/HA are human items) |
| UI-05 | 03-01, 02, 06, 07, 11, 12 | Run info: sim time, factor, firmware connected, behind-real-time warning | SATISFIED | Truth 4 (thresholds feel is a human item) |
| UI-06 | 03-04, 07, 08, 10, 11, 12 | Everything the page shows or does is reachable by CLI/API with the same JSON, with a parity check | SATISFIED | Truth 5 |

### Anti-Patterns Found

Scanned the files changed in the phase. There are no `TBD`, `FIXME` or `XXX` markers and no `TODO`, `HACK` or `PLACEHOLDER` in the page or in `internal/web`. No stub or empty handlers were found. No debt-marker blocker.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/engine/engine.go` | 548 | `e.simNow = now` assigned unconditionally (review IN-05) | Info | SIM TIME can step back by up to one tick. Cosmetic; open in `03-REVIEW-DISPOSITION.md`. |
| `internal/web/web.go` | static handler | Directory listing and `--ui-dir` served on the LAN bind (IN-02) | Info | Accepted risk under D-08; open. |
| `ui/web/js/inputs.js` | `commit()` | Edits typed while the request is in flight are discarded (IN-01) | Info | Minor UX; open. |
| `ui/web/js/buttons.js` | `down()`/`release()` | Tab closed with a down in flight loses the release until the 10 s max hold (IN-06); a down applied more than 3 s late makes the next press fail `button_busy` (IN-07) | Info | Backstop is the engine max hold. Human item 10. |
| various | | Unused `maxRestartGap`, duplicate `lanHostPtr`, odd scheme string in `WebURL` (IN-03) | Info | Cleanup only. |

The review disposition lists six open info items (IN-01, 02, 03, 05, 06, 07). That matches `03-REVIEW.md` (critical 0, warning 0, info 6, status clean). I confirmed the five fixed findings (CR-01 and WR-01 to WR-03, plus the second-round WR-01) by commit and test name (`TestFailedInstanceSnapshotRenders`, `TestLANWatchSharesTheBrowserBudget`, `TestUSRUpSurvivesPageTeardown`, `TestUSRRepressWaitsForUp`, `TestUSRStalledRequestTimesOut`).

### Human Verification Required

These are the items the plans deferred to a person, plus the one from the review (see the `human_verification` frontmatter for the full text). None is counted as passed; the automated proxy that exists for each is named.

1. **Phone on the LAN** (UAT 1): LAN reachability and layout on a real phone. Proxy: same-host Chrome (`TestWebReach`, `TestLayoutPhone`).
2. **USR2 long press from the page** (UAT 2): all four fans go to 100 %, HA follows. Proxy: `usr2_long_like_cli` with a CDP mouse hold.
3. **Touch slide-off on a real phone** (UAT 3): no stuck hold in Safari, Firefox or Chrome. Proxy: Chrome emulated touch (`TestUSRTouchLeave`).
4. **Temperature reaching HA** (UAT 4): the real HA entity shows 26. Proxy: native-API client in `env_reaches_firmware`.
5. **Sim restart recovery** on the phone (UAT 5). Proxy: headless Chrome (`sim_restart`).
6. **Visual character** (UAT 6): the D-01 look. Proxy: palette tokens, `TestNoVendorMarks`, layout measurements, and the two screenshots.
7. **UIC-10 minutes-long soak** (UAT 7): flat tab memory over 10 or more minutes. Proxy: ring and history unit tests.
8. **Behind-real-time thresholds A-03-05/06 on a loaded host** (UAT 8). Proxy: manual-clock tests.
9. **`--listen 127.0.0.1` reachability** from a second machine (UAT 9). Proxy: the bind address is tested, no cross-machine probe.
10. **Semantics of a timed-out button down** (review WR-01 fix, IN-06/IN-07). Proxy: `TestUSRStalledRequestTimesOut`.

### Gaps Summary

No gaps. The phase goal is achieved, and the evidence is independent of the SUMMARY claims. The page is embedded and offline-safe, and shows and drives every required thing. It uses only rows of the one operation table that the CLI shares, and a four-layer automated parity check enforces that. The security decisions D-08, D-18, D-19 and D-20 are implemented in code and covered by tests. The overall status is `human_needed` because of the ten items above. Nothing is blocked on code changes.

---

_Verified: 2026-11-09T17:30:00Z_
_Verifier: Claude (gsd-verifier)_

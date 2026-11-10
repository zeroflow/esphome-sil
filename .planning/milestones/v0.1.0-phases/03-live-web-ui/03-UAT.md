---
status: complete
phase: 03-live-web-ui
source: [03-VERIFICATION.md]
started: 2026-11-09T18:00:00Z
updated: 2026-11-10T10:44:15Z
---

## Current Test

[testing complete]

## Tests

### 1. Phone on the LAN. `sil up uat`, open the printed `web.url` on a phone on the same LAN (optionally a second PC too).
expected: The page loads from another device, fits the phone width, scrolls in one column, plots move, LINK reads LIVE.
result: pass
note: "plots moving did not work with the command, but with usr1 button" - the suggested `sil set port1.duty=0.5` was a wrong instruction (duty is a firmware output; sil set only takes inputs), not a product defect"

### 2. Touch and hold USR2 for about 2 s on the phone page (UAT 2), with Home Assistant connected to the instance if possible.
expected: All four fans go to 100 %, and HA's Fan N Speed entities follow, exactly as for `sil press usr2 --long`.
result: pass

### 3. Touch slide-off on a real phone browser (UAT 3): touch USR1, slide the finger off the cap and keep it on the screen; also touch the cap and then bring up a system overlay or switch app.
expected: The cap goes dark within a moment (no stuck hold); after a system overlay it does not stay lit (the 10 s max hold is the backstop). Try Safari and/or Firefox as well as Chrome.
result: pass

### 4. Temperature reaches HA (UAT 4): set TEMPERATURE to 26 on the phone page and press Enter.
expected: Home Assistant's Temperature entity shows 26.
result: pass

### 5. Sim restart recovery on a real phone (UAT 5): run `sil sim restart uat` on the bench machine while the phone page is open.
expected: The phone page recovers by itself without a reload (stale overlay, then LIVE, plots repaint).
result: pass

### 6. Visual character (UAT 6): look at the desktop page at 1280 x 720 or larger (screenshots 03-06-layout-1280x720.png, 03-06-layout-390x844.png are in the phase directory).
expected: Dark, sober bench-instrument look (D-01), channel colours per port (yellow, green, blue, magenta), the only glow on the LED lenses, no vendor names.
result: pass

### 7. UIC-10 minutes-long soak (UAT 7): leave the page open 10 minutes or more with a fan running; watch the tab's memory (Chrome task manager or DevTools Memory).
expected: Memory stays flat and the plots keep their 60 s window.
result: pass
evidence: "start 11:23 125M, 11:26 209M, end 11:39 97M (16 min soak; peak reclaimed by GC, no growth trend)"

### 8. Behind-real-time thresholds A-03-05/06 on a loaded host (UAT 8): load the host (parallel `make e2e` or a compile) and watch the REAL TIME cell.
expected: BEHIND appears when the simulator is 50 ms or more late and returns to IN SYNC about 2 s after it caught up, without flicker; judge whether 50 ms / 2 s feel right.
result: pass
note: "pass, ok" - 50 ms / 2 s thresholds accepted"

### 9. `--listen 127.0.0.1` reachability (UAT 9): from a second machine, `sil up uat --listen 127.0.0.1` and open the same URL.
expected: The page is not reachable from the LAN (README claim).
result: pass

### 10. Semantics of a timed-out button down (review WR-01 fix bfbb808, IN-06, IN-07): stall or delay a button.down on a real host and observe the cap and the next press.
expected: A down that times out after 3 s counts as never registered: no up is sent, the cap shows only the server's report, the engine's 10 s max hold releases a late-applied hold; a late-applied down (> 3 s) can make the next press fail `button_busy`. Decide whether that is acceptable.
result: pass
note: "pass" - timed-out-down semantics (IN-06, IN-07) accepted for a local bench"

## Summary

total: 10
passed: 10
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]

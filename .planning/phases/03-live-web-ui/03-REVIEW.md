---
phase: 03-live-web-ui
reviewed: 2026-11-09T18:00:00Z
depth: standard
files_reviewed: 62
files_reviewed_list:
  - Makefile
  - README.md
  - e2e/harness_test.go
  - e2e/signals_test.go
  - e2e/watch_test.go
  - e2e/web_test.go
  - go.mod
  - internal/apperr/codes.go
  - internal/cdptest/cdptest.go
  - internal/cli/cli.go
  - internal/cli/cli_test.go
  - internal/cli/operations_test.go
  - internal/control/control.go
  - internal/control/control_test.go
  - internal/control/operations.go
  - internal/engine/engine.go
  - internal/engine/engine_test.go
  - internal/events/events.go
  - internal/events/events_test.go
  - internal/instance/instance.go
  - internal/instance/instance_test.go
  - internal/instance/port.go
  - internal/instance/webport_test.go
  - internal/lifecycle/lifecycle.go
  - internal/lifecycle/lifecycle_test.go
  - internal/supervisor/history.go
  - internal/supervisor/supervisor.go
  - internal/supervisor/supervisor_test.go
  - internal/supervisor/watch_test.go
  - internal/supervisor/web_test.go
  - internal/web/guard.go
  - internal/web/guard_test.go
  - internal/web/listener.go
  - internal/web/listener_test.go
  - internal/web/web.go
  - internal/web/web_test.go
  - internal/web/ws.go
  - internal/web/ws_test.go
  - ui/embed.go
  - ui/ui_test.go
  - ui/vendor_test.go
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
  - uitest/bench_test.go
  - uitest/harness_test.go
  - uitest/hold_test.go
  - uitest/inputs_test.go
  - uitest/layout_test.go
  - uitest/leds_test.go
  - uitest/live_test.go
  - uitest/parity_test.go
  - uitest/plot_test.go
  - uitest/resilience_test.go
findings:
  critical: 0
  warning: 0
  info: 6
  total: 6
status: clean
---

# Phase 3: Code Review Report

**Reviewed:** 2026-11-09T18:00:00Z
**Depth:** standard
**Files Reviewed:** 62
**Status:** clean (no critical or warning findings; six info items remain)

## Summary

Iteration 3, the final pass. I read api.js, buttons.js and app.js in full, with the bfbb808 diff and the
tests that go with it (uitest/hold_test.go, uitest/bench_test.go). I then re-read ws.go and web.go and
checked that the engine and bench code they depend on is consistent.

**WR-01 (one stalled button request blocks the cap) is resolved.** Points checked:

- **Timer cleanup.** `call()` (api.js:51-61) clears the timer in a `finally` around `await perform(...)`.
  The `await` matters: `perform` returns `res.json()` for a success, so the timer also covers the answer
  body. Every exit clears it: success, ApiError, network error and abort. When no `timeoutMs` is given,
  no signal and no timer are created, so the other callers behave as before.
- **Abort during the body read.** An abort while `res.json()` or `res.text()` is pending rejects the
  promise with an AbortError, and `finally` still runs. It is not an ApiError, so it needs no special
  handling.
- **AbortError kept out of the message line.** All four consumers filter by `instanceof ApiError`: `say`
  (buttons.js:26), `failJoin` (app.js:107), `loadFans` and `commit`. The abort never reaches
  `showError`.
- **History timeout feeds `failJoin`.** `startJoin` passes `timeoutMs: HISTORY_TIMEOUT_MS`; the abort
  rejection goes to the second `.then` handler, which calls `failJoin`. That resets the scopes, replays
  the held samples and sets `done`. A timed-out request is aborted, so there is no late `finishJoin`
  that could overwrite the live plot (and `join !== j` guards it anyway). **IN-04 is resolved.**
- **Semantics after a timed-out down.**
  - The down resolves `false`: it is the error branch of the `.then(ok, err)` pair, which also covers
    the AbortError.
  - `holding` is cleared only if `cap.gen === gen`, so a timed-out old down cannot clear a newer
    press.
  - `release` then sends no up, so there is no orphan up. `started` is `false`, and the chain
    continues with `undefined`.
  - The cap is never lit by the page, so a timed-out down lights nothing. If the server did apply
    that down late, the cap shows the server's report and the engine's 10 s max hold ends it. The
    comment in buttons.js says so.
  - Nothing is shown, which matches D-17.
- **The chain cannot wedge.** `downDone` never rejects (both branches of the pair resolve), and the up's
  tail ends in `.catch(say)`, where `say` cannot throw. Each link is bounded by 3 s, so the longest
  wait for a cap's next press is one stalled request. `call()` itself is `async`, so even a synchronous
  TypeError becomes a rejection that the down/up handlers absorb.
- **The new test is sound.** `TestUSRStalledRequestTimesOut` relies on the bench's `StallNext`, which
  blocks on the request context and so ends when the client aborts (the LAN handler passes
  `r.Context()`; the listener serves one request per connection, so the abort closes the connection and
  cancels the context). The stall is consumed once (`delete`) under the mutex, so the second down is
  not stalled. The gap assertion uses server-side `At` stamps with a 2.5 s lower bound against a 3 s
  timeout, so it is not tied to the browser's own timing. The only timing sensitivity is the 1 s
  `settle` against the 3 s timeout (see IN-07).

**Confirmation pass.** ws.go, web.go and the fixes behind them still hold. One browser stream takes one
slot, because `/ws` is built over `rc` and the LAN API over `cappedWatch{rc}`. `release` is idempotent
through `sync.Once`. A refused 9th page returns before taking a slot. Every Watch exit gives the slot
back. The error-frame rule (`error` and no `k`) is consistent with `errorFrame` being the only frame
without `k`. I found nothing new there. The `IN-03` re-wrapped comment and double blank line in
api.js/buttons.js are fixed.

Still valid and carried forward: IN-01, IN-02, IN-03 (narrowed), IN-05 (narrowed), IN-06. New: IN-07.

## Info

### IN-01: `commit()` throws away edits typed while the request is in flight

**File:** `ui/web/js/inputs.js:126-156`
**Issue:** After `await call("set", ...)` succeeds, every field of the commit has its `dirty` class
removed and `el.value` overwritten with the applied value. If the user typed again in the same field
between pressing Enter and the answer, that edit is lost and the field no longer shows as dirty.
**Fix:** Remember `values[el.id]` and reset only a field whose `el.value` still equals it; leave the
others dirty.

### IN-02: The static handler lists directories, and `--ui-dir` exposes a whole directory tree to the LAN

**File:** `internal/web/web.go:60`, `internal/supervisor/supervisor.go:236-241`, `internal/web/listener.go:105-109`
**Issue:** `http.FileServerFS` renders a listing for `/js/` and `/vendor/`. With `--ui-dir` the same
handler serves `os.DirFS(dir)` on the LAN bind, dotfiles and symlink targets included. `web.Serve` also
keeps the default 1 MiB `MaxHeaderBytes` (no override in `internal/web`).
**Fix:** Wrap the file system so a directory without `index.html` answers 404 and dotfiles are refused;
set `MaxHeaderBytes` to a few KiB; warn when `--ui-dir` is combined with a LAN bind.

### IN-03: Small leftovers and duplication

**File:** `internal/supervisor/supervisor.go:74`, `internal/supervisor/supervisor.go:1382`, `internal/lifecycle/lifecycle.go:648`, `internal/instance/port.go:213`
**Issue:**
- `maxRestartGap` (supervisor.go:74) has no use anywhere in the module; `control.MaxRestartGap` is the
  real limit.
- `lanHostPtr` is defined in both `supervisor` and `lifecycle`.
- `scheme := "http" + ":" + "//"` in `WebURL` is a constant written in an odd way. If it exists to
  dodge a grep for `http://`, say so in a comment; otherwise use a plain literal.
- `web.Serve` copies `control.ServeListener` apart from the keep-alive line.

**Fix:** Delete the constant, move `lanHostPtr` to `instance`, simplify or comment the scheme, and let
`control.ServeListener` take a keep-alive option.

### IN-05: The engine tick can still set `simNow` back after an `Apply` or a button edge

**File:** `internal/engine/engine.go:545-549`, `internal/engine/engine.go:751-753`, `internal/engine/engine.go:883-885`
**Issue:** The two handlers are now monotonic (`if now > e.simNow`), which fixes half of the earlier
finding. The tick still assigns `e.simNow = now` unconditionally (line 548), where `now` was taken when
the ticker goroutine forwarded the tick. If a handler ran in between and moved `simNow` forward, the
tick moves it back, so SIM TIME and the `x` of the next sample can step back by up to one tick.
**Fix:** `if now > e.simNow { e.simNow = now }` at line 548 (keep `dt := now - last` as it is).

### IN-06: The up of a hold is queued behind its own down, so a teardown before the down is answered loses the release

**File:** `ui/web/js/buttons.js:79-83`
**Issue:** The up is created only after `cap.downDone` settles. If the tab is hidden or closed while
the down is still in flight, the up is never sent. The engine's 10 s max hold is the backstop, so the
effect is a button that stays pressed for up to 10 s. The window is one round trip (3 s at worst
now that the down is bounded).
**Fix:** When `document.hidden` or the page is unloading, send the up at once with `keepalive` without
waiting for the down; the server refuses an up with no hold harmlessly. Or send the down with
`keepalive` too.

### IN-07: A down that timed out but was applied late makes the next press fail as `button_busy`; the new test has a 2 s margin

**File:** `ui/web/js/buttons.js:45-67`, `uitest/hold_test.go:513-519`
**Issue:** Two small points about the WR-01 fix.
- The comment in `down()` says a late-applied down shows as the server's hold and ends at the max hold.
  A second press queued behind it, sent after the 3 s timeout, then meets a held button, and the
  server answers `button_busy`. That text is shown, and the press is lost until the max hold ends the
  first. The path needs a server that applies a request more than 3 s late, which a local sim does not
  do; no test covers it.
- `TestUSRStalledRequestTimesOut` asserts that only one down exists after `settle(1s)`. The page's
  timeout is 3 s, so the margin is 2 s minus CDP latency for the release and press events. On a very
  loaded machine this could flake. Exporting the timeout to the test, or lengthening `settle` only as
  far as the margin allows, would not help; a larger `REQUEST_TIMEOUT_MS` in a test build would.
**Fix:** Optional. Document the `button_busy` case next to the existing comment. For the test, read
`REQUEST_TIMEOUT_MS` from the page (`import("/js/buttons.js")`) and assert the "one down" window
relative to it, or accept the margin.

---

_Reviewed: 2026-11-09T18:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_

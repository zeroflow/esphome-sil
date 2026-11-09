---
phase: 03-live-web-ui
reviewed: 2026-11-09T00:00:00Z
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
  critical: 1
  warning: 3
  info: 5
  total: 9
status: issues_found
---

# Phase 3: Code Review Report

**Reviewed:** 2026-11-09
**Depth:** standard
**Files Reviewed:** 62
**Status:** issues_found

## Summary

I read the non-test source in full: `internal/web`, `internal/control`, `internal/supervisor`
(including `watch.go`, which is outside the diff but carries the stream contract), `internal/engine`,
`internal/events`, `internal/instance`, `internal/lifecycle`, `internal/cli`, every file under
`ui/web/js`, `index.html`, the Makefile and `go.mod`. I skimmed the tests (`cdptest`, the listener
and websocket tests, the `uitest` helpers) for reliability and flake risk.

The required protections hold up under tracing:

- **api.key redaction (D-18).** `redacting` covers `Status`, `SimRestart`, `FwRestart` and the
  snapshot line of `Watch`, both as a value and as a pointer. Nothing else in `Status`, `Warnings`
  or the error details carries the key.
- **Socket-only operations (D-19, D-20).** `NewLANHandler` refuses the `LAN: false` rows before
  they reach the `Control`. `init()` makes a duplicate row or a missing handler a panic.
- **Host allowlist, CSP and CSRF.** `requestHost` rejects an empty host, a trailing dot, a bad port
  and a name that is not allowed. A host that passes is the only text interpolated into the CSP.
  `CrossOriginProtection` and the library's origin check on `/ws` cover the browser side.
- **Connection and header limits.** I checked the premise of `Limit` against the Go 1.27.3
  `net/http/server.go` in this environment. `readRequest` sets a read deadline only at line 1103,
  after the headers are parsed. `startBackgroundRead` clears it once there is no body left to read,
  which is why `LimitBody` re-arms it for requests that have a body. The reasoning in the comment
  is correct.
- **No XSS path.** The JS writes server strings only through `textContent`, `title`,
  `setAttribute`, and `option.textContent`. LED colours reach `style.setProperty` only after the
  `#rrggbb` regex. There is no `innerHTML`, no `eval` and no `document.write`.
- **No unbounded growth in the browser.** Rings are capped (620 plus 64 slack), the `held` buffer is
  capped at 3000, and `WeakMap`s hold the per-element state.
- **Engine.** Sample emission, `behind_real_time` and max-hold release are correct on the
  engine goroutine. The history ring is filled under the same lock that assigns `seq`, and the
  samples it hands out share no mutable memory with the engine.

There is one functional bug in the page (CR-01). The rest are gaps against stated intent
(WR-01, WR-02, WR-03) and small defects.

## Critical Issues

### CR-01: A snapshot of a failed instance is mistaken for a stream-error frame, so the page never renders it

**File:** `ui/web/js/app.js:163-168`, fed by `internal/control/control.go:293-298` and `internal/supervisor/watch.go:79`
**Issue:** `SnapshotLine` embeds `Status`, and `Status.Error` is tagged `json:"error,omitempty"`.
The embedded fields are promoted to the top level, so the snapshot of any instance with a failure
has a top-level `"error": {code, message, ...}` key. That happens for `firmware_crash_loop`, a
`ready_timeout`, `plaintext_api`, `handshake_rejected`, `api_identity_mismatch` and `engine_failed`.
`onFrame` takes that key for the final `{"error": ...}` frame that `ws.go` sends when a stream
fails, shows the message, and calls `lost(ws)` before `apply(frame)` ever runs. The effects:

- The page never shows the state of a failed instance (ports, buttons, LEDs, the fan list), which is
  exactly when the developer opens it.
- LINK shows "DISCONNECTED, RETRYING" and the page reconnects forever. The backoff is not reset,
  because the reset is on the snapshot branch that is never reached. Every attempt takes a Watch slot
  and a browser slot for a moment.
- The 5 s message line flashes the instance's failure message, which looks like a transport failure.

The tests do not cover it. `TestErrorFrame` sends a frame without `k`, and no bench snapshot carries
`Status.Error`.

**Fix:** The server's error frame is the only frame with `error` and no `k`. Test for that, and let
snapshots with an `error` field through to the store:
```js
if (frame && frame.k === undefined && frame.error) {
  showError(String(frame.error.message));
  lost(ws);
  return;
}
```
Add a `uitest` case whose bench snapshot has `phase: "failed"` and `error: {code, message}`. It should
assert LINK is LIVE and the ports are rendered. A mirror test in `internal/web/ws_test.go` should pin the
frame shape: a snapshot with a failure has `k: "snapshot"` and `error`.

## Warnings

### WR-01: `GET /api/v1/watch` on the LAN listener bypasses the browser stream cap

**File:** `internal/control/operations.go:54`, `internal/web/web.go:46`, `internal/web/ws.go:24-31`
**Issue:** The cap of `maxBrowserStreams` (8) is counted only inside the `/ws` handler. The `watch`
row of the operations table has `LAN: true`, so the same listener serves it as an NDJSON stream with
no counter. The page does not use it (`OPS` in `api.js` has no `watch` entry). Any LAN client can open
16 `GET /api/v1/watch` streams. The header deadline does not end them, because it only covers the
headers. That uses all 16 Watch slots of the instance, so `sil watch` and `sil logs --follow` on the
machine get `too_many_streams`. The comment at `ws.go:24-26` ("so `sil watch` and `sil logs --follow`
always keep the rest") and the README ("limits on connections and streams") promise exactly what this
route defeats. The listener is open by decision (D-08); the stated stream limit is still not met.

**Fix:** Count the LAN NDJSON watch against the same budget, or take it off the LAN. Wrap the LAN
`Control` handed to `NewLANHandler` so that `Watch` takes a browser slot:
```go
type capped struct{ control.Control; acquire func() bool; release func() }

func (c capped) Watch(ctx context.Context, r control.WatchRequest, send func(any) error) error {
	if !c.acquire() {
		return errTooManyBrowsers()
	}
	defer c.release()
	return c.Control.Watch(ctx, r, send)
}
```
Share the counter between `newWS` and this wrapper. D-19 requires `/ws`, not the NDJSON route. If a LAN
`watch` is not needed, the simpler fix is `LAN: false` on that row, updating `operations_test.go` and
the parity test to match.

### WR-02: The release on tab hide or page unload can be cancelled before it is sent

**File:** `ui/web/js/api.js:47`, `ui/web/js/buttons.js:55-58`, `ui/web/js/buttons.js:148-152`
**Issue:** `visibilitychange` to hidden and `window.blur` call `releaseAll()`. That is the moment a
tab is being closed or navigated away from, and the only moment the page still has to send `up`.
`call()` uses a plain `fetch(path, init)` without `keepalive: true`, and `release()` sends the up in
a promise continuation after the `down` has been answered. A request that is in flight when the page
is torn down may be cancelled by the browser. The hold then lasts until the engine's 10 s max hold.
That is the designed backstop (D-15) and it limits the damage, but a cheap mitigation is missing: the
"firmware sees a 10 s press" case is exactly what the page-side release is there to avoid.

**Fix:** Let `call()` take a `keepalive` option and set it for `button.up`:
```js
export async function call(name, { body, query, params, keepalive } = {}) {
  ...
  const init = { method, headers: {}, keepalive: keepalive === true };
```
```js
call("button.up", { params: { id: cap.id }, keepalive: true }).catch(say);
```
`keepalive` bodies are limited to 64 KiB, which a body-less up is far below.

### WR-03: A fast re-press can send `down` before the previous `up` has arrived

**File:** `ui/web/js/buttons.js:28-45`, `ui/web/js/buttons.js:49-60`
**Issue:** `release()` clears `cap.holding` at once and fires the `up` in the background. It does
not wait for the up to complete. A new `pointerdown` then passes the `cap.holding` guard in `down()`
and posts a second `button.down`. The web listener serves one request per connection (keep-alives are
off), so the two requests travel on separate connections and nothing orders them. If `down#2` reaches
the supervisor first, it fails with `button_busy` (the message is shown, the cap is never held, and
`holding` is cleared). If `up#1` is delayed past `down#2`, it releases the new hold. Either way, a
press that is physically held is not held on the bench. The window is one network round trip, so it
needs a re-press within a few milliseconds, which makes it rare. A stuck or lost hold is the main
failure mode this page exists to prevent.

**Fix:** Keep the in-flight `up` promise on the cap and chain the next `down` after it:
```js
// in release(): remember the up
cap.upDone = cap.downDone.then((started) => (started ? call("button.up", {...}).catch(say) : undefined));
// in down(): start after the previous up settles
cap.downDone = cap.upDone.then(() => call("button.down", {...})).then(() => true, (err) => {...});
```
Initialise `upDone` to `Promise.resolve()` in `initButtons`.

## Info

### IN-01: `commit()` throws away edits typed while the request is in flight

**File:** `ui/web/js/inputs.js:126-156`
**Issue:** After `await call("set", ...)` succeeds, every field that was in the commit has its `dirty`
class removed and `el.value` overwritten with the applied value. If the user typed again in the same
field between pressing Enter and the answer, that edit is lost silently and the field no longer shows
as dirty.
**Fix:** Snapshot `el.value` into `values` as now, and after the answer touch only the fields whose
current value still equals the committed text. For a field that changed, leave it dirty and keep its
text.

### IN-02: The static handler lists directories, and `--ui-dir` exposes a whole directory tree to the LAN

**File:** `internal/web/web.go:57`, `internal/supervisor/supervisor.go:236-241`, `internal/web/listener.go:105-107`
**Issue:** `http.FileServerFS` renders a listing for `/js/` and `/vendor/`. That is harmless for the
embedded page. With `--ui-dir`, the same handler serves `os.DirFS(dir)` on 0.0.0.0, including dotfiles
and symlink targets, so a dev checkout can leak its contents to the LAN. `web.Serve` also keeps the
default 1 MiB `MaxHeaderBytes`, which is large for a listener whose other limits are tight.
**Fix:** Wrap the file system so that a directory without `index.html` answers 404, and set
`srv.MaxHeaderBytes = 16 << 10` in `web.Serve`. Say in the `--ui-dir` help that the directory is
served as is.

### IN-03: Small leftovers and duplication

**File:** `internal/supervisor/supervisor.go:74`, `internal/supervisor/supervisor.go:1382`, `internal/lifecycle/lifecycle.go:648`, `internal/instance/port.go:213`
**Issue:**

- `maxRestartGap` (supervisor.go:74) is unused; `control.MaxRestartGap` is the real limit.
- `lanHostPtr` is defined in both `supervisor` and `lifecycle` (a repeat of IN-04 of the phase 2 review).
- `scheme := "http" + ":" + "//"` in `WebURL` is a constant written in an unusual way. If it exists to
  dodge a grep for `http://`, say so in a comment; if not, use a plain literal.
- `web.Serve` copies `control.ServeListener` apart from the keep-alive line.

**Fix:** Delete the constant, move the helper to `instance`, and either comment or simplify the
scheme. Let `control.ServeListener` take a `keepAlives bool` or an `*http.Server` configurator so
that `web.Serve` can reuse it.

### IN-04: The history request has no timeout, so a hung request leaves the plot empty until the next reconnect

**File:** `ui/web/js/app.js:73-80`, `ui/web/js/api.js:47`
**Issue:** `startJoin` waits for `GET /api/v1/history` and holds every sample frame until it answers.
`fetch` has no timeout. If the request hangs while the WebSocket stays up (the ping keeps it alive),
the scopes stay empty and the live samples pile up in `held`. The cap of 3000 stops memory growth, but
the plot never starts. `HELD_MAX` is described as "a request that long has failed", and nothing ends
that request.
**Fix:** Abort the fetch after a few seconds with `AbortSignal.timeout(5000)` (passed to `fetch` from
`call()`), so `failJoin` runs and the page continues with live samples.

### IN-05: Event times are not monotonic around `Apply` and `SetButton`, so SIM TIME can step back

**File:** `internal/engine/engine.go:751-753`, `internal/engine/engine.go:883-885`, `internal/engine/engine.go:545-549`, `ui/web/js/store.js:31-33`
**Issue:** `handleApply` and `handleButton` set `e.simNow` to `Clock.Now()`. The ticker, however,
queues `r.Now()` taken when its goroutine forwarded the tick, and the engine sets
`e.simNow = now` from that older value when it picks the tick up. After an `Apply` or a button edge,
the next tick can therefore carry a smaller time than the event before it. The sample `x` values come
from tick times only, so the plots are unaffected. `store.apply` writes `state.sim.time_s = frame.t / 1e6`
for every frame, so the SIM TIME readout can step back by up to a tick (about 10 ms) after an input.
It is cosmetic, but it contradicts "t is strictly the stream's sim time".
**Fix:** In the tick case, use `now = max(now, e.simNow)`. In the store, only move `time_s` forward
(`if (frame.t / 1e6 > state.sim.time_s)`), unless the frame is a snapshot.

---

_Reviewed: 2026-11-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_

---
phase: 03-live-web-ui
plan: 03
subsystem: web
tags: [go, net-http, websocket, dos, listener-limits]

requires:
  - phase: 03-live-web-ui
    provides: "03-01 web.NewHandler, /ws stream, supervisor web listener; 03-02 browser harness"
provides:
  - "web.Limit / web.ListenerOptions: 64-connection cap and a 10 s header deadline from the injected clock at Accept"
  - "web.Serve: control.ServeListener's shape with keep-alives off"
  - "web.LimitBody: body read deadline from the injected clock for requests that carry a body"
  - "/ws ping every 15 s with a 10 s pong budget; 8-stream browser cap with a final too_many_streams frame"
affects: [03-05, 03-04]

actuals:
  tokens: 7700
  tasks: 2
  commits: 4
plan_head_before: 35d4938eee31755b5c14839307301b1ef6f8978c
plan_head_after: c2939596d07a59ce65ceeb02b170ba3bf8f4b249
commits: 4

tech-stack:
  added: []
  patterns:
    - "listener hardening without http.Server timeout fields: an Accept-time SetReadDeadline(clk.Deadline(d)) that net/http clears after the headers"
    - "ping goroutine per WS connection on clk.NewTicker; failure cancels the stream context with a cause, the handler then closes with going-away"

key-files:
  created:
    - internal/web/listener.go
    - internal/web/listener_test.go
    - internal/web/ws_test.go
  modified:
    - internal/web/ws.go
    - internal/supervisor/supervisor.go
    - e2e/web_test.go

key-decisions:
  - "The web listener is served as web.Serve(web.Limit(webLn), web.LimitBody(web.NewHandler(...))); api.sock still uses control.ServeListener(control.NewHandler(s)) and is unchanged"
  - "Slots (Watch and browser) are released before the close handshake, so a dead peer that makes conn.Close wait for its 5 s library timeout no longer holds them"
  - "The browser counter is per newWS call (per NewHandler), so tests and the one production handler each have their own"

requirements-completed: [UI-01]

duration: 40min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "The web listener serves one request per connection, closes a connection whose headers are not complete within 10 s (also when they trickle), holds at most 64 connections, and lets /ws and NDJSON streams outlive the header budget"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/listener_test.go#TestSlowHeader, TestTrickledHeaderIsClosed, TestIdleConnectionIsClosed, TestStreamOutlivesHeaderBudget, TestConnCap, TestKeepAlivesOff, TestLimitReleasesOnce"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/ws_outlives_header_budget"
        status: pass
    human_judgment: false
  - id: D2
    description: "A request body trickled after the headers is cut off by a deadline from the injected clock; requests without a body and streams are untouched"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/listener_test.go#TestStalledBodyIsClosed, TestLimitBodyLeavesGoodRequestsAlone"
        status: pass
    human_judgment: false
  - id: D3
    description: "/ws pings every 15 s and closes a connection with no pong within 10 s, freeing its slots; a client that answers is never reaped"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/ws_test.go#TestWSPingReaps, TestWSPingKeepsAnsweringClients"
        status: pass
    human_judgment: false
  - id: D4
    description: "At most 8 browser streams run at once; the 9th gets one too_many_streams frame and a try-again-later close without reaching Watch; a freed slot is reusable"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/ws_test.go#TestBrowserStreamCap"
        status: pass
    human_judgment: false
  - id: D5
    description: "Each /ws text frame plus a newline equals the matching NDJSON line of GET /api/v1/watch"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/ws_test.go#TestWSFramesEqualWatchLines"
        status: pass
    human_judgment: false
---

# Phase 3 Plan 03: Bounded Web Listener and WS Stream Slots Summary

**The LAN web listener now caps connections at 64, cuts off slow or trickled headers and bodies with deadlines from the injected clock, serves one request per connection, pings `/ws` clients and reaps the silent ones, and keeps browsers to 8 of the 16 shared stream slots so `sil watch` and `sil logs --follow` always have room.**

## Performance

- **Duration:** about 40 min
- **Tasks:** 2 (tracer + TDD auto task) plus the coordinator's follow-up (LimitBody), 4 commits
- **Files:** 6 (3 created, 3 modified)
- `make check` 119 passed; `make e2e E2E_RUN='TestWebTracer|TestWatchStream|TestCLIContract'` green (TestWebTracer now 16 s because of the new 11 s subtest); `make test-ui` green.

## Accomplishments

- `web.Limit`: at most `MaxConns` (64) open connections (an extra one is closed at Accept); each accepted connection gets `SetReadDeadline(clk.Deadline(HeaderBudget))` (10 s); the slot is released once on Close.
- `web.Serve`: `control.ServeListener` with `SetKeepAlivesEnabled(false)` and the same 2 s graceful shutdown.
- `web.LimitBody`: `http.NewResponseController(w).SetReadDeadline(clk.Deadline(10 s))` for requests that carry a body.
- `/ws`: per-handler CAS counter for browsers (`maxBrowserStreams = 8`); the 9th upgrades, gets `{"error":{"code":"too_many_streams",...,"detail":{"limit":8}}}` and a try-again-later close, never calling Watch. A ping goroutine on `Clock.NewTicker(15 s)` runs `conn.Ping` under `clock.WithTimeout(10 s)`; a failure cancels the Watch context with cause `errPongTimeout`, both slots are released, then the connection is closed with going-away.
- Supervisor: the web listener is `web.Serve(web.Limit(...), web.LimitBody(web.NewHandler(...)))`; `api.sock` is untouched.
- e2e `TestWebTracer/ws_outlives_header_budget` (real firmware): a stalled raw client is closed by the 10 s header budget while a `/ws` stream older than 10 s still receives an env event.

## net/http clears the Accept-time deadline after the headers (as TestStreamOutlivesHeaderBudget showed)

Go 1.27.3 `net/http/server.go`: `conn.serve` arms a deadline only if `ReadHeaderTimeout` is set (line 2058), so the Accept-time deadline is the one in force while the first request is read; `readRequest` replaces it with the whole-request deadline, zero without `ReadTimeout`, once the headers are parsed (line 1103). That is why `TestStreamOutlivesHeaderBudget` (a `/ws` stream and an NDJSON watch both still receiving ticks at 1.1 s with a 200 ms budget) passes and the e2e subtest holds a stream past 10 s. The code that clears the deadline before reading the next request (lines 2176 to 2200) runs only for kept-alive connections, so the header defence depends on keep-alives staying off; `Serve` and `Limit` now say so in comments. Mutations confirmed the tests are not vacuous: dropping the Accept-time `SetReadDeadline` fails `TestSlowHeader` and `TestIdleConnectionIsClosed`; dropping `SetKeepAlivesEnabled(false)` fails `TestKeepAlivesOff`; dropping the body deadline fails `TestStalledBodyIsClosed`.

## Task Commits

1. **Task 1 (tracer): Limit, Serve, supervisor wiring, listener tests, e2e subtest** - `260f62f` (feat)
2. **Coordinator follow-up: LimitBody, net/http citation, trickle and stalled-body tests** - `7d84f4e` (fix)
3. **Task 2 RED: failing WS tests** - `712d1e9` (test)
4. **Task 2 GREEN: ping reaping and the 8-stream browser cap** - `c293959` (feat)

## TDD Gate Compliance

Task 2 (`tdd="true"`, plan `type: execute`): RED `712d1e9` (TestBrowserStreamCap failed because the 9th stream was served; TestWSPingReaps hung because nothing pinged; TestWSFramesEqualWatchLines already passed and is a characterization of 03-01 behaviour), then GREEN `c293959`. No refactor commit. No `check tdd-red-evidence` record was persisted.

## Deviations from Plan

### Auto-fixed Issues

**1. [Coordinator request, security review] Header-defence comment, trickle test, body deadline**
- **Found during:** Task 2, from a security review relayed by the coordinator ("the Accept-time deadline is cleared by net/http").
- **Issue:** the claim was checked against `server.go` and found not to hold while keep-alives are off (see above); two real gaps remained: no comment tying the design to the stdlib behaviour, and a body trickled after the headers was unbounded (deadline zero once the headers are parsed).
- **Fix:** (1) `SetKeepAlivesEnabled(false)` kept and commented, with the server.go citation on `Limit` and `Serve`. (2) `TestSlowHeader` already drove the production `Serve(Limit(...))` path with a stalled partial request line over real TCP; added `TestTrickledHeaderIsClosed` (byte-by-byte headers, closed near the budget). The Manual clock cannot drive these: `Manual.Deadline` returns the zero time, so the budget is a `ListenerOptions` parameter (200 to 300 ms) on the real clock, which passes the wall-clock lint. (3) Residual closed rather than only documented: `web.LimitBody` re-arms a read deadline from the injected clock for requests with a body. (4) Per-remote-IP cap: not added (see below).
- **Files modified:** `internal/web/listener.go`, `internal/web/listener_test.go`, `internal/supervisor/supervisor.go`
- **Commit:** `7d84f4e`

**2. [Rule 2 - Missing critical] Reap path frees slots before the close handshake**
- **Found during:** Task 2 design, reading coder/websocket `Close`.
- **Issue:** `conn.Close` waits up to 5 s (a library-internal timer) for the peer's close reply, and the peer here is by definition not answering; releasing the slots after it would keep a reaped stream's slots for 5 s.
- **Fix:** the Watch slot (Watch returns on the cancelled context) and the browser slot (explicit `release()`) are freed before `Close(StatusGoingAway, ...)`.
- **Files modified:** `internal/web/ws.go`
- **Commit:** `c293959`

**3. [Scope] Added e2e subtest outside the plan's file list**
- `e2e/web_test.go` gained `ws_outlives_header_budget` because the plan's acceptance says a live `/ws` stream outlives the 10 s header budget on the real stack, which no existing e2e subtest held for 10 s. It adds 11 s to `TestWebTracer`.

**4. [Test addition] Extra tests beyond the plan's list:** `TestIdleConnectionIsClosed`, `TestLimitReleasesOnce`, `TestServeLimitedRoutes`, `TestTrickledHeaderIsClosed`, `TestStalledBodyIsClosed`, `TestLimitBodyLeavesGoodRequestsAlone`, `TestWSPingKeepsAnsweringClients` (the negative control for reaping).

**Total deviations:** 4 (1 coordinator-requested, 1 missing-critical, 2 scope/test additions). **Impact:** `LimitBody` is a new exported function and a new link in the supervisor chain; 03-05's `web.Guard` goes inside it. No interface in the plan changed.

## Threat model

| Threat ID | Disposition | Status |
|-----------|-------------|--------|
| T-03-14 slow header and connection exhaustion | mitigate | web.Limit (header deadline, 64 cap), web.Serve (keep-alives off); tests above |
| T-03-15 zombie browser streams | mitigate | 15 s ping, 10 s pong budget, 8-stream browser share; TestWSPingReaps, TestBrowserStreamCap |
| T-03-14b trickled request body after the headers (added by the review) | mitigate | web.LimitBody deadline (10 s), the 64 KiB `io.LimitReader` on every control body read, the 64-connection cap, trusted LAN per D-08 |

**Residual, accepted:** no per-remote-IP connection cap. One LAN host can still occupy all 64 connections for up to 10 s each (header or body budget) and, with `/ws`, up to 8 browser streams (each pinged). It needs a per-IP counter keyed on the peer address in `Limit`, which is cheap but is policy beyond the plan's "cheap protections that cost no UX" (a household behind one NAT address would share a cap); left for a decision with 03-05's Host and Origin rules. Also not bounded: a client that connects, sends full headers and then reads its response slowly (write side); `/ws` writes have a 10 s budget, other responses are small.

## Authentication Gates

None.

## Issues Encountered

- `net/http` answers a stalled partial request with a 408 before closing, so the stalled-client tests read to EOF instead of expecting an empty close.
- An attempt to run the RED tests in the background hung (TestWSPingReaps waits on `BlockUntil`), which is the expected RED behaviour; it was killed and rerun with `-timeout`.

## Known Stubs

None.

## Threat Flags

None beyond the rows above; no new endpoints. No new dependencies (stdlib and coder/websocket as pinned by 03-01).

## Next Phase Readiness

- 03-05 wraps the handler in `web.Guard` inside `web.LimitBody(...)` in the supervisor chain (`web.Serve(web.Limit(...), web.LimitBody(web.Guard(web.NewHandler(...))))` or similar).
- The `too_many_streams` browser frame reuses the registered exit-2 code; the page (03-04 onward) can show its message as it shows other error frames.

## Self-Check: PASSED

- Files exist: internal/web/listener.go, internal/web/listener_test.go, internal/web/ws_test.go (FOUND).
- Commits exist: 260f62f, 7d84f4e, 712d1e9, c293959 (all FOUND).
- Acceptance: `web.Limit` in supervisor.go, `SetKeepAlivesEnabled(false)` in listener.go, `maxBrowserStreams = 8` in ws.go (all match); `make check` 119 passed; e2e `TestWebTracer/ws_live` and `TestWebTracer/stopped_on_down` PASS; `make test-ui` PASS.

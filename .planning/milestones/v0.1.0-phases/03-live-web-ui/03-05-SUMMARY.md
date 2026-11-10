---
phase: 03-live-web-ui
plan: 05
subsystem: web
tags: [go, net-http, csrf, dns-rebinding, csp, websocket-origin, sticky-ports, sil-up]

requires:
  - phase: 03-live-web-ui
    provides: "03-01 web listener, sticky web port, WebURL/ListenWeb/TestBindWeb; 03-02 browser harness; 03-03 web.Limit/Serve/LimitBody; 03-04 control.Operations table and NewLANHandler"
provides:
  - "sil up --listen IP --web-port N --allow-host H (repeatable) --ui-dir DIR; web {url, port, listen} in the up JSON; stderr line `web UI: <url> (unauthenticated: ...)`"
  - "instance.PortRequest and LoadOrCreate(p, PortRequest); Spec.Listen/AllowHosts/UIDir"
  - "web.Guard / web.GuardOptions: Host allowlist (host_rejected), security headers incl. CSP, http.CrossOriginProtection with JSON origin_rejected"
  - "error codes host_rejected and origin_rejected (exit 1)"
  - "TestCSRFCoversEveryMutatingRoute: the guard is proven over the whole control.Operations table"
affects: [03-06, 03-12]

actuals:
  tokens: 20700
  tasks: 2
  commits: 4
plan_head_before: d44b8976ea179eaea03b570436240d782b045a1b
plan_head_after: 818547412cb844b18fd913c91ba76a969f15ceba
commits: 4

tech-stack:
  added: []
  patterns:
    - "guard outermost on the LAN handler, inside LimitBody: Host check, then headers, then CrossOriginProtection; the ResponseWriter is never wrapped so Hijacker/Flusher reach /ws"
    - "table-driven security test: every non-GET row of control.Operations must refuse a cross-origin request, every GET row must be on a reviewed read-only list"
    - "a refused port is never stored: an explicit busy --web-port fails port_in_use before identity.json is written, like --api-port"

key-files:
  created:
    - internal/web/guard.go
    - internal/web/guard_test.go
  modified:
    - internal/cli/cli.go
    - internal/lifecycle/lifecycle.go
    - internal/lifecycle/lifecycle_test.go
    - internal/instance/instance.go
    - internal/instance/instance_test.go
    - internal/instance/webport_test.go
    - internal/supervisor/supervisor.go
    - internal/apperr/codes.go
    - internal/web/ws_test.go
    - uitest/harness_test.go
    - e2e/harness_test.go
    - e2e/web_test.go
    - .planning/phases/03-live-web-ui/deferred-items.md

key-decisions:
  - "web.Guard sits inside web.LimitBody and outside web.NewHandler: a request with a body still gets its read deadline, and a foreign Host is refused before any route, the page and /ws included"
  - "The refusals carry the security headers too; a host_rejected response names no WebSocket host in its CSP because r.Host is not yet trusted (a test feeds a Host that tries to inject `script-src *`)"
  - "--listen is canonicalised with net.ParseIP(...).String() (0:0:0:0:0:0:0:0 becomes ::); host names are lower-cased, a trailing dot dropped, and labels validated; --ui-dir must hold a regular index.html"
  - "An explicit --web-port equal to the instance's own API port (or --api-port equal to its web port) is invalid_argument naming both flags; the check also covers a stored port on either side"
  - "TestWSPingReaps was a test race, not a ws.go bug; fixed in the test (see Deviations)"

patterns-established:
  - "A new control.Operations row that is a GET must be added to the read-only list in TestCSRFCoversEveryMutatingRoute after review, because net/http lets safe methods through the CSRF check"
  - "Tests that jump a Manual clock must not do so while a coder/websocket write is in flight (the library closes the connection hard when a write context expires)"

requirements-completed: [UI-01]

duration: 17min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "sil up takes --listen, --web-port, --allow-host and --ui-dir; invalid values fail exit 1 invalid_argument before anything is built or locked"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/lifecycle/lifecycle_test.go#TestCheckWebOptions"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/ui_dir"
        status: pass
    human_judgment: false
  - id: D2
    description: "The sticky web port follows Phase 1's rules: a taken sticky port is port_in_use exit 2 naming port, listen address and holder and is never moved; an explicit port another instance holds is port_claimed; an explicit busy port is not stored; web == api is invalid_argument"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/instance/webport_test.go#TestAllocateWebPort, TestLoadOrCreateWebPort, TestListenWebInUse, TestWebURL"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/claimed, TestWebReach/in_use, TestWebReach/sticky"
        status: pass
    human_judgment: false
  - id: D3
    description: "sil up prints web {url,port,listen} and the unauthenticated notice with the URL on stderr; sil status shows the same web block; --listen 127.0.0.1 answers on loopback only; --ui-dir serves the page from disk on every request"
    requirement: UI-01
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/default_lan, TestWebReach/listen_loopback, TestWebReach/ui_dir"
        status: pass
    human_judgment: false
  - id: D4
    description: "A request for a Host that is not an IP literal, localhost, this machine, <machine>.local or an --allow-host value answers 403 host_rejected before any handler (page, API and /ws alike); a trailing dot never passes"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/guard_test.go#TestHostGuard"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/host_guard"
        status: pass
    human_judgment: false
  - id: D5
    description: "A cross-site browser request cannot change the bench: every non-GET row of the control table (the LAN rows and the refused ones) answers 403 origin_rejected for Sec-Fetch-Site cross-site/same-site or a foreign/null Origin and never reaches the Control, while requests without those headers pass; every GET row is on a reviewed read-only list"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/guard_test.go#TestCSRF, TestCSRFCoversEveryMutatingRoute"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/csrf"
        status: pass
    human_judgment: false
  - id: D6
    description: "A /ws upgrade from a foreign or null Origin, or for a foreign Host, is refused with 403 and never reaches Watch; no Origin or the page's own origin is accepted"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/guard_test.go#TestWebSocketOrigin"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/csrf"
        status: pass
    human_judgment: false
  - id: D7
    description: "Every web-listener response, refusals included, carries the CSP (connect-src names ws://<validated Host>), nosniff, X-Frame-Options DENY and Referrer-Policy no-referrer; the page loads under the policy with an empty console and the policy blocks an inline script"
    requirement: UI-01
    verification:
      - kind: automated
        ref: "internal/web/guard_test.go#TestSecurityHeaders"
        status: pass
      - kind: automated_ui
        ref: "uitest/live_test.go#TestOfflineLoadAndLive, uitest/harness_test.go#TestCSPIsEnforced"
        status: pass
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/headers"
        status: pass
    human_judgment: false
  - id: D8
    description: "api.sock keeps its 0600 mode, the full key and every operation; sil status, sil watch --count 1 and sil down work as before"
    requirement: UI-01
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebReach/socket_untouched, TestWebTracer/lifecycle_refused"
        status: pass
    human_judgment: false
---

# Phase 3 Plan 05: Reach Flags and the Guarded LAN Listener Summary

**`sil up` now takes `--listen`, `--web-port`, `--allow-host` and `--ui-dir` with Phase 1's sticky-port conflict rules and prints the page URL with an unauthenticated notice, and the LAN web listener sits behind `web.Guard`: a Host allowlist against DNS rebinding, `http.CrossOriginProtection` with a JSON deny on every mutating route, the websocket origin check, and a CSP plus three more security headers on every response.**

## Performance

- **Duration:** about 17 min (13:31 to 13:48 UTC)
- **Tasks:** 2 (tracer, TDD auto task) plus one Rule 1 fix, 4 commits
- **Files:** 15 (2 created, 13 modified)
- `make check` green (119 passed); `make test-ui` all PASS; `make e2e E2E_RUN='TestWebReach|TestWebTracer|TestIsolation|TestCLIContract'` all PASS (`TestIsolation` matches no test name, the isolation tests are TestPortInUse/TestPortClaimed etc., which I ran separately and passed).

## Accomplishments

- **Task 1 (tracer, f0056a4):** `instance.PortRequest{API, Web, Listen}` replaces `LoadOrCreate`'s `apiPort`; an explicit `--web-port` is stored only after `TestBindWeb(listen, port)` succeeds, `--web-port == API port` (either direction, stored or explicit) is `invalid_argument` naming both flags. `lifecycle.checkWebOptions` runs with the pure checks before `EnsureRunParent` and the instance lock. `TestBindWeb` runs after the API test-bind and again right before `startServe`. Spec carries `listen`, `allow_hosts`, `ui_dir`; the supervisor binds `spec.Listen` and serves `os.DirFS(spec.UIDir)` when set. `UpResult.Web` and the `web UI: <url> (unauthenticated: ...)` stderr line. e2e `Env.Up` injects `--listen 127.0.0.1` and a free `--web-port` per name.
- **Task 2 (RED 53fa916, GREEN 2834da2):** `web.Guard` as specified; wiring `web.LimitBody(web.Guard(web.NewHandler(...)))` in the supervisor; `newSite` in uitest runs behind the guard so every browser test runs under the production CSP.
- **The earlier security finding** (no CSRF/origin check on state-changing endpoints, no Host check): closed for the whole table, not for the routes someone remembered. `TestCSRFCoversEveryMutatingRoute` walks `control.Operations`: each of the 7 non-GET rows (down, sim.restart, fw.restart, press, button.down, button.up, set) refuses a `Sec-Fetch-Site: cross-site` request and a foreign-Origin request with 403 `origin_rejected` and the recording Control sees 0 changes; each GET row (status, fans, watch, logs, watch.ws) must be on a named read-only list, because `CrossOriginProtection` lets safe methods through. A rebound request (foreign Host with matching Origin and `Sec-Fetch-Site: same-origin`) is refused by the Host check. `/ws` is refused for a foreign Origin (`websocket.Accept`'s default check, `InsecureSkipVerify` never set) and for a foreign Host.
- **Mutation checks:** replacing `cop.Handler(h)` by `h` fails TestCSRF, TestCSRFCoversEveryMutatingRoute and TestSecurityHeaders; allowing every host fails TestHostGuard and TestWebSocketOrigin; dropping `setSecurityHeaders` fails TestSecurityHeaders.

## CSP and the browser console

No CSP violation appeared. `TestOfflineLoadAndLive` asserts an empty `ConsoleErrors()` (CSP violations arrive there through `Log.entryAdded`) with the page served behind the guard, and the other seven uitest tests, including the parity tests, pass behind it. To show that the empty console means the policy is active, `TestCSPIsEnforced` reads the delivered policy through `fetch('/')`, injects an inline script and requires both that it does not run and that a "Content Security Policy" error reaches the console. `connect-src` names `ws://<validated Host>` exactly as in the plan (lower-cased, IPv6 in brackets).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestWSPingReaps` was flaky (a test race in 03-03's test, not a bug in `ws.go`)**
- **Found during:** Task 2 (internal/web, flagged by 03-04 in deferred-items.md and by the dispatch notes)
- **Issue:** `go test -race -count=50 -run TestWSPingReaps ./internal/web/` failed about one run in five with `failed to read frame header: EOF, want a going-away close`. A TCP/net.Conn spy showed the server writing the 3-byte ping and then closing the socket without ever writing the close frame. Cause: coder/websocket v1.8.15 registers `context.AfterFunc(ctx, c.close)` for the context of every in-flight write (`setupWriteTimeout`), and the test advanced the Manual clock (15 s, then 10 s) while a write was still finishing, which expired the snapshot write budget (10 s) or the ping's pong context and made the library close the connection hard. In production the budgets are real time and a write never takes that long, so `ws.go` is correct.
- **Fix:** the test script now signals after the snapshot `send` returned; time moves only after that. For the ping, a listener tap on the server side sees the ping frame (so the ping's write lock is held), then the script sends one more frame (which needs the same write lock) and signals; only then is the pong budget advanced. The test reads the ping, that frame and the going-away close. `BlockUntil(2)` was dropped because it counts "at least 2" waiters and a not-yet-stopped write-budget timer could satisfy it. `TestWSPingKeepsAnsweringClients` got the snapshot wait too.
- **Files modified:** internal/web/ws_test.go, .planning/phases/03-live-web-ui/deferred-items.md
- **Verification:** `go test -race -count=100 -run TestWSPingReaps ./internal/web/` ok; `-count=30 -cpu 1,2,8` ok; `TestWSPingKeepsAnsweringClients -count=50` ok; `-count=3 ./internal/web/` ok; before the fix the same command failed repeatedly.
- **Commit:** 8185474

**2. [Rule 2 - Missing critical functionality] Tests beyond the plan's list**
- **Found during:** Task 2
- **Issue:** The plan's behavior list covered a fixed set of routes. The dispatch notes asked for the guard to cover every mutating route of `NewLANHandler` and the `/ws` upgrade, and the CSP claim needed proof that it is enforced, not just delivered.
- **Fix:** added `TestCSRFCoversEveryMutatingRoute`, `TestWebSocketOrigin` (a real server: foreign/null Origin refused, own origin and no Origin accepted, foreign Host refused, Watch never reached) and `uitest.TestCSPIsEnforced`; Host cases include trailing dots, malformed ports and CSP-injection attempts.
- **Commit:** 53fa916, 2834da2

**3. [Rule 3 - Blocking] Wall-clock lint in my own e2e code**
- **Found during:** Task 1 (`make check`, `TestNoWallClock`)
- **Issue:** `net.DialTimeout` in the new `listen_loopback` check arms a wall-clock timer, which the repository forbids.
- **Fix:** `net.Dialer.DialContext` under `clock.WithTimeout(ctx, sysClock, 3s)`.
- **Commit:** f0056a4

**Total deviations:** 3 (1 bug, 1 added coverage, 1 lint). **Impact:** none on scope; the flake fix removes a known intermittent `make check` failure.

Other notes (not deviations): the plan names `TestWebURL` and `TestListenWebInUse`; 03-01's `TestListenWebAndURL` was split into those two.

## Residual risk (accepted, per D-08)

- A cross-site request that carries neither `Sec-Fetch-Site` nor `Origin` is treated as non-browser and served; every browser of the last years sends one of them on a cross-origin POST. The CLI, curl and scripts rely on this.
- The listener stays unauthenticated for anyone on the LAN who uses an allowed Host (AR-03-01). `sil up` prints that with the URL; `--listen 127.0.0.1` narrows it. The README text is 03-12.

## Known Stubs

None.

## Threat Flags

None. The surface added here is the mitigation itself (T-03-11, T-03-12, T-03-13, T-03-16, T-03-18). `--ui-dir` serves a developer-chosen directory through `http.FileServerFS(os.DirFS(...))` (T-03-17, accepted in the plan); it is per run, validated to hold `index.html`, and the Host/CSRF guard applies to it like to the embedded page.

## Self-Check: PASSED

- Created files exist: internal/web/guard.go, internal/web/guard_test.go, this SUMMARY.
- Commits found in `git log`: f0056a4 (Task 1), 53fa916 (RED), 2834da2 (GREEN), 8185474 (flake fix).
- Acceptance criteria re-run: `make test-pkg` for TestHostGuard|TestCSRF|TestSecurityHeaders, TestAllocateWebPort|TestLoadOrCreateWebPort|TestWebURL|TestListenWebInUse and TestCheckWebOptions print `ok`; `./bin/sil up --help | grep -c` prints 4; `grep unauthenticated internal/lifecycle/lifecycle.go` matches; `grep -c host_rejected|origin_rejected internal/apperr/codes.go` prints 2; TestWebReach prints PASS for default_lan, listen_loopback, sticky, claimed, in_use, ui_dir, host_guard, csrf, headers and socket_untouched.
- Plan verification re-run after the last commit: `make check`, `make test-ui`, `make e2e E2E_RUN='TestWebReach|TestWebTracer|TestIsolation|TestCLIContract'` all pass.

---
phase: 03-live-web-ui
plan: 01
subsystem: ui
tags: [go, websocket, coder-websocket, embed, http, sticky-port, redaction]

requires:
  - phase: 02-full-rev-3-3-bench
    provides: control.Control with Watch, the supervisor, api.sock handler, sticky API port, e2e harness and r33Up
provides:
  - sticky web_port per instance (8054..9053) in identity.json, run.json web_port/web_listen, Status.web {listen, port, url}
  - internal/web: NewHandler (embedded page, /api/v1/* behind a key-redacting wrapper, /ws), RedactedKey, Options
  - /ws frames identical to `sil watch` lines (snapshot first, events, stopped), kind filter, error frame after upgrade
  - ui package with the embedded page shell, store.js reducer and app.js WebSocket link with reconnect
  - lifecycle operations (down, sim restart, fw restart) refused on the web listener with operation_not_allowed (403)
affects: [03-02, 03-03, 03-04, 03-05, 03-06, 03-07, 03-08, 03-10, 03-11]

actuals:
  tokens: 13500
  tasks: 2
  commits: 3
plan_head_before: a4084916cbee48d63dc9ef8690849b40cadc701e
plan_head_after: 36de129715d5bf70745d88865f5477c1d97c98d0
commits: 3

tech-stack:
  added: ["github.com/coder/websocket v1.8.15"]
  patterns:
    - "one more front door: the web mux mounts control.NewHandler over a struct embedding control.Control that overrides only Status, SimRestart, FwRestart and Watch"
    - "WS frame = the json.Encoder(SetEscapeHTML(false)) line of sil watch without its newline"
    - "no ResponseWriter wrapping anywhere (streaming and the WS hijack need the original)"

key-files:
  created:
    - internal/web/web.go
    - internal/web/ws.go
    - internal/web/web_test.go
    - ui/embed.go
    - ui/web/index.html
    - ui/web/js/app.js
    - ui/web/js/store.js
    - internal/instance/webport_test.go
    - e2e/web_test.go
  modified:
    - go.mod
    - go.sum
    - internal/instance/instance.go
    - internal/instance/port.go
    - internal/control/control.go
    - internal/supervisor/supervisor.go
    - internal/apperr/codes.go

key-decisions:
  - "AllocateWebPort receives the API port of the same LoadOrCreate call through its bindable closure, so a first-use identity never gets web_port == api_port even though api_port is not stored yet"
  - "LoadOrCreate only test-binds an explicit --api-port when that port actually changes; an identity that merely gains a web_port keeps its (possibly in-use) API port untouched"
  - "The redacting wrapper also redacts SimRestart and FwRestart even though D-19 refuses those routes, so a later change to the refusals cannot leak the key"
  - "The static handler answers 405 as JSON invalid_argument (no new error code) with an Allow header"

patterns-established:
  - "Task-ordered D-19: the refusal routes are registered before the /api/v1/ mount; method-qualified patterns beat the prefix pattern in ServeMux"
  - "Web e2e helpers (webDo, webDial, webRead, webReadUntil) in e2e/web_test.go use sysClock and clock.WithTimeout only"

requirements-completed: [UI-01, UI-05]

duration: 40min
completed: 2026-11-09
status: complete

coverage:
  - id: D1
    description: "sil serve binds a sticky LAN web port (first free from 8054, claimed-port scan over api and web ports, legacy identities migrated) and sil status shows web {listen, port, url}"
    requirement: UI-01
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/status_web"
        status: pass
      - kind: unit
        ref: "internal/instance/webport_test.go#TestAllocateWebPort, TestLoadOrCreateWebPort, TestLoadOrCreateMigratesLegacyIdentity, TestListenWebAndURL"
        status: pass
    human_judgment: false
  - id: D2
    description: "GET / serves the embedded page, /js/app.js and /js/store.js as javascript, other methods answer 405"
    requirement: UI-01
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/page"
        status: pass
      - kind: unit
        ref: "internal/web/web_test.go#TestStaticIsReadOnly"
        status: pass
    human_judgment: false
  - id: D3
    description: "/ws streams the sil watch snapshot first (with sim.time_s, sim.time_factor and firmware.bus_connected), then events, then stopped; unknown kind is a 400 before the upgrade"
    requirement: UI-05
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/ws_snapshot, ws_live, ws_bad_kind, stopped_on_down"
        status: pass
    human_judgment: false
  - id: D4
    description: "api.key is <redacted> in TCP status and the /ws snapshot while api.sock still returns the 32-byte key"
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/status_redacted"
        status: pass
      - kind: unit
        ref: "internal/web/web_test.go#TestStatusKeyIsRedacted, TestWatchSnapshotKeyIsRedacted, TestRestartStatusKeyIsRedacted"
        status: pass
    human_judgment: false
  - id: D5
    description: "down, sim restart and fw restart answer 403 operation_not_allowed on the web listener and leave the instance untouched"
    verification:
      - kind: e2e
        ref: "e2e/web_test.go#TestWebTracer/lifecycle_refused"
        status: pass
      - kind: unit
        ref: "internal/web/web_test.go#TestLifecycleRefusedAndNeverReachesControl"
        status: pass
    human_judgment: false
  - id: D6
    description: "app.js/store.js reconnect loop and state mirror behave correctly in a real browser"
    verification: []
    human_judgment: true
    rationale: "No browser runs in this plan; the offline headless-Chrome proof of app.js and store.js is 03-02 (TestOfflineLoadAndLive)"
---

# Phase 3 Plan 01: Live Web UI Tracer Summary

**`sil serve` now answers on a sticky LAN web port with the embedded page shell and a `/ws` stream of `sil watch` lines, the firmware key redacted and down/sim restart/fw restart refused on that listener, proven end to end on the real Rev 3.3 firmware.**

## Performance

- **Duration:** 40 min
- **Started:** 2026-11-09T12:20:00Z
- **Completed:** 2026-11-09T13:01:00Z
- **Tasks:** 2 (tracer + TDD task)
- **Files modified:** 16 (9 created, 7 modified)
- **Web port the e2e run got:** 8054 (the first of the range; the host had it free)
- **Warm TestWebTracer:** 4.6 s total including the Rev 3.3 instance bring-up (9.5 s on the first run); the subtests themselves take under 0.1 s

## Accomplishments

- Sticky web port in `identity.json` next to `api_port`: claimed-port scan now covers every other instance's api and web ports, an identity from Phase 1/2 gets a web port on the next `sil up` with key and api_port unchanged, `ListenWeb` never moves to another port (`port_in_use` exit 2 / `port_bind_failed`).
- `internal/web`: one mux with the embedded page, `/api/v1/` through `control.NewHandler` over a redacting wrapper (D-18), `GET /ws` over `Control.Watch` (frames byte-equal to `sil watch` lines, `kind=` filter, 400 before upgrade, final `{"error":...}` frame after it).
- Embedded page shell (`ui/web/index.html`, `app.js`, `store.js`): header cells and port-panel template with the `data-src`/`data-fmt` contract, reducer that mirrors status from snapshot plus typed events, reconnect loop 500 ms doubling to 5 s.
- D-19: the three lifecycle routes answer 403 `operation_not_allowed` before the control handler and never reach the Control; `sil down`, `sil sim restart`, `sil fw restart` over api.sock are unaffected.

## Task Commits

1. **Task 1: Tracer, sticky web port to TCP listener to page shell and /ws** - `e847058` (feat)
2. **Task 2 RED: failing e2e for refused lifecycle operations** - `910879c` (test)
3. **Task 2 GREEN: refuse down, sim restart and fw restart on the web listener** - `36de129` (feat)

**Plan metadata:** the docs commit that carries this file (docs: complete plan)

## TDD Gate Compliance

Task 2 (`tdd="true"`): RED `910879c` made `TestWebTracer/lifecycle_refused` fail on its own assertion (`POST /api/v1/down = 200, want 403`, and the real instance was stopped), GREEN `36de129` made it pass; no refactor commit. The plan is `type: execute`, so no `check tdd-red-evidence` record was persisted. Task 1 is a tracer, not a `tdd` task, and shipped with its e2e test in the same commit by design.

## Files Created/Modified

- `internal/web/web.go` - `NewHandler`, `Options`, `RedactedKey`, redacting Control wrapper, lifecycle refusals, read-only static handler
- `internal/web/ws.go` - `newWS`: kind parsing, `websocket.Accept`, `CloseRead`, per-frame write budget on the injected clock, error frame and close codes
- `internal/web/web_test.go` - redaction, static read-only, pre-upgrade 400, lifecycle never reaches the Control
- `ui/embed.go` - `//go:embed all:web`, `FS()`
- `ui/web/index.html`, `ui/web/js/app.js`, `ui/web/js/store.js` - page shell, WS link with reconnect, state mirror
- `internal/instance/instance.go`, `port.go` - `Identity.WebPort`, `DefaultWebPort`, `AllocateWebPort`, `ListenWeb`, `TestBindWeb`, `WebURL`, claims over both ports, `RunFile.WebPort/WebListen`
- `internal/instance/webport_test.go` - allocation, migration, corrupt web_port, listen and URL tests
- `internal/control/control.go` - `WebStatus`, `Status.Web`, `WriteError`, `WriteErrorStatus`
- `internal/supervisor/supervisor.go` - `defaultWebListen`, bind before the engine, second `ServeListener`, `Status.Web`
- `internal/apperr/codes.go` - `operation_not_allowed` (exit 1)
- `e2e/web_test.go` - `TestWebTracer` (8 subtests)
- `go.mod`, `go.sum` - `github.com/coder/websocket v1.8.15` (go.sum hashes match the research values; `go mod verify` passes)

## Decisions Made

- First-use identity: the API port decided in the same `LoadOrCreate` call is excluded from web allocation through the `bindable` closure, because it is not yet stored for `AllocateWebPort` to read.
- `LoadOrCreate` re-tests an explicit `--api-port` only when it actually changes the stored port, so adding a web port to a running instance's identity never trips over its own API port being in use.
- The wrapper redacts `SimRestart`/`FwRestart` even though their routes are refused: defence in depth for T-03-01.
- 405 on static paths reuses `invalid_argument` rather than minting a code.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Redaction unit test relied on routes that Task 2 refuses**
- **Found during:** Task 2 GREEN
- **Issue:** `TestStatusKeyIsRedacted` (written in Task 1) POSTed the restart routes through the handler and expected 200; after D-19 they answer 403.
- **Fix:** test `GET /api/v1/status` through the handler and test the wrapper's `SimRestart`/`FwRestart` directly.
- **Files modified:** `internal/web/web_test.go`
- **Committed in:** `36de129`

**2. [Rule 2 - Missing critical] Unit tests added beyond the plan's e2e-only list**
- **Found during:** Task 1
- **Issue:** the truths for legacy identity migration, claimed-port scan and `WebURL` host choice are cheap to prove without docker and were otherwise covered only by the single-instance e2e.
- **Fix:** `internal/instance/webport_test.go` and `internal/web/web_test.go`.
- **Committed in:** `e847058`, `36de129`

---

**Total deviations:** 2 auto-fixed (1 bug in my own test, 1 added coverage)
**Impact on plan:** no scope creep; no production behaviour differs from the plan.

## Authentication Gates

None.

## Issues Encountered

None. A first draft of Task 1 included the D-19 refusals; they were removed again so Task 2 has a real RED step and the commit history matches the plan's task order.

## Known Stubs

- `ui/web/index.html`: value elements show the placeholder `--` and `<main id="ports">` is empty. Intentional: the readout renderer `view.js` (formatters, panel cloning, LINK through render) is 03-02. Recorded in `.planning/WINDOWS.md`.

## Threat Flags

None beyond the plan's threat model: the new unauthenticated LAN listener is the surface the plan registered (T-03-01, T-03-02, T-03-04..06); the accepted-risk row T-03-03 lands in 03-05.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 03-02 can build `view.js` and the headless-Chrome harness on `ui.FS()`, `web.NewHandler(c, Options)` and the `data-src`/`data-fmt` contract; `store.apply/getState/reset` are in place.
- The `web.NewHandler` signature is fixed; 03-03 replaces `control.ServeListener` for the web listener with its limited server.
- Note for 03-05: `sil up` does not yet test-bind the web port or print the page URL; a taken sticky web port currently surfaces as the supervisor failing with `port_in_use` (exit 2) through the existing serve-exited path.

## Self-Check: PASSED

- Created files exist: internal/web/web.go, internal/web/ws.go, internal/web/web_test.go, ui/embed.go, ui/web/index.html, ui/web/js/app.js, ui/web/js/store.js, internal/instance/webport_test.go, e2e/web_test.go (all FOUND).
- Commits exist: e847058, 910879c, 36de129 (all FOUND).
- Acceptance: go.sum hash matches, `make mod-verify` prints `all modules verified`, `make check` green (119 passed), `CGO_ENABLED=0 go build ./cmd/sil` exits 0, `go:embed all:web` present, `data-src=` count 8, first-party web files contain no http(s) URL, innerHTML or eval.
- Verify: `make e2e E2E_RUN='TestWebTracer|TestTracer|TestCLIContract|TestWatchStream'` passed, with `--- PASS: TestWebTracer/stopped_on_down` and `lifecycle_refused`; the restart e2e tests (TestSimRestartResync, TestFwRestartResync, TestRestartEitherOrder, TestStatusDuringRestart) also pass.

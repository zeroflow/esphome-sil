# Phase 3: Live Web UI - Pattern Map

**Mapped:** 2026-11-09 (file table refreshed 2026-11-09 against the files_modified lists of 03-01..03-12)
**Files analyzed:** 66 new/modified, which is exactly the union of the `files_modified` lists of the twelve plans
**Analogs found:** 51 / 66. The 11 front-end page files and the 4 vendored files have no in-repo analog; see "No Analog Found".

Scope rule: the table below lists only files that a plan creates or modifies, and every such file has a row. Files that serve only as analogs, such as `internal/supervisor/watch.go`, `internal/instance/lanhost.go` and `internal/lint/wallclock_test.go`, appear in the analog column and are not edited by any plan.

All analog paths below were checked against `git ls-files` and are tracked. None is a gitignored mirror (`.gsd/` is untracked and never named). `prototype/` is frozen and not used here. Line numbers refer to the tree at commit 12985ae.

Key design constraint carried from RESEARCH: the web phase is "one more front door to the same handler" plus four engine/API additions (`sample` event + history ring, `behind_real_time`, `max_hold`, `Operations` table). Almost everything has an in-repo analog; the risk is bypassing existing mechanisms (wall-clock lint, apperr registry, broker, sticky ports).

## File Classification

| New/Modified File (plans) | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `ui/embed.go` (new: 03-01) | embed package | file-I/O (static) | `fans/embed.go` | exact |
| `ui/web/index.html` (new: 03-01; 03-06, 03-08, 03-10, 03-11), `ui/web/app.css` (new: 03-06; 03-08, 03-10, 03-11) | static page | request-response | none | none |
| `ui/web/js/app.js`, `store.js` (new: 03-01), `view.js` (new: 03-02), `api.js`, `inputs.js`, `message.js` (new: 03-08), `ring.js`, `plot.js` (new: 03-10), `buttons.js` (new: 03-11); later plans extend app.js, store.js, view.js, api.js, plot.js | browser modules | event-driven (WS) | none (spec: 03-RESEARCH Code Examples) | none |
| `ui/web/vendor/uPlot.iife.min.js`, `uPlot.min.css`, `LICENSE.uplot`, `VENDOR.md` (new: 03-09) | vendored assets | file-I/O | none (hash recorded in RESEARCH) | none |
| `ui/ui_test.go` (new: 03-04; 03-08), `ui/vendor_test.go` (new: 03-09) | test | transform | `internal/lint/wallclock_test.go` (repo walk style) | role-match |
| `internal/web/web.go` (new: 03-01; 03-04): `NewHandler` composition plus the redacting `control.Control` wrapper | HTTP compose + Control wrapper | request-response | `internal/control/control.go` `NewHandler` L357 + `control_test.go` `fakeControl` embed idiom L19 | role-match |
| `internal/web/guard.go` (new: 03-05; CSP fallback only if needed: 03-10) | middleware | request-response | `control.go` `reply` L531 + `errorBody` L350 | role-match |
| `internal/web/ws.go` (new: 03-01; 03-03) | WS adapter | streaming | `control.go` `streamNDJSON` L500 + `supervisor/watch.go` | exact (same shape) |
| `internal/web/listener.go` (new: 03-03) | net.Listener wrapper + server | batch (accept) | `control.go` `Listen`/`ServeListener` L545-595 | role-match |
| `internal/web/listener_test.go`, `ws_test.go` (new: 03-03), `guard_test.go` (new: 03-05; 03-10) | test | request-response | `internal/control/control_test.go` | exact |
| `internal/control/operations.go` (new: 03-04; 03-07) | route table | request-response | `control.go` `NewHandler` L357-492 (inline routes) | exact |
| `internal/control/control.go` (mod: 03-01, 03-04, 03-07), `control_test.go` (mod: 03-04, 03-07) | control API | request-response | itself | exact |
| `internal/events/events.go`, `events_test.go` (mod: 03-07: `SampleKind`, ring, `Reason`, `Gap`) | broker | pub-sub | itself (`Publish` L97, `Kinds` L23, `UnknownKind` L47) | exact |
| `internal/engine/engine.go`, `engine_test.go` (mod: 03-07: sample with gap marker, behind, max-hold) | engine | event-driven | itself (tick branch L443-470, `handleButton` L756) | exact |
| `internal/supervisor/supervisor.go` (mod: 03-01, 03-03, 03-05, 03-07) | supervisor | request-response + streaming | itself (L252-258 listener start; `statusFrom` L631+) | exact |
| `internal/supervisor/history.go` (new: 03-07) | Control method | request-response | `supervisor.go` `Fans` L1178 (a read-only Control method building a result) + `watch.go` (broker access) | role-match |
| `internal/supervisor/supervisor_test.go` (mod: 03-07), `internal/supervisor/web_test.go` (new: 03-04) | test | n/a | `internal/supervisor/watch_test.go` `watchSup` L21-50 | exact |
| `internal/instance/instance.go` (mod: 03-01, 03-05), `port.go` (mod: 03-01), `instance_test.go` (mod: 03-05) | identity/config | CRUD | itself (`Identity` L152, `AllocatePort`, `TestBind`; `instance_test.go` `TestAllocatePort`) | exact |
| `internal/lifecycle/lifecycle.go`, `lifecycle_test.go` (mod: 03-05) | lifecycle | CRUD | itself L162-227 | exact |
| `internal/cli/cli.go` (mod: 03-05, 03-07), `cli_test.go` (mod: 03-07) | CLI | request-response | `PressCmd` L105 / `FansCmd` L120 / `UpCmd` L62 | exact |
| `internal/cli/operations_test.go` (new: 03-04) | test | transform | `cli.go` `Execute` (kong.New options, L171) + `cli_test.go` `TestCommandsAreControlClients` L387 | role-match |
| `internal/apperr/codes.go` (mod: 03-01, 03-05) | registry | n/a | itself | exact |
| `internal/cdptest/cdptest.go` (new: 03-02) | test helper package (Chrome launcher + CDP client) | request-response over WS | `e2e/harness_test.go` (child process, `TestMain`, cleanup) + `e2e/watch_test.go` (line-reader goroutine) | role-match |
| `uitest/harness_test.go` (new: 03-02; 03-05), `bench_test.go` (new: 03-02; 03-08, 03-10, 03-11), `live_test.go` (new: 03-02), `parity_test.go` (new: 03-04; 03-08, 03-10, 03-11), `leds_test.go`, `layout_test.go` (new: 03-06; 03-12), `inputs_test.go` (new: 03-08), `plot_test.go` (new: 03-10), `hold_test.go`, `resilience_test.go` (new: 03-11) | browser tests (build tag `sil_ui`) | request-response over WS | `e2e/harness_test.go` (tag, TestMain, helpers) + `control_test.go` `fakeControl` | role-match |
| `e2e/web_test.go` (new: 03-01; 03-02, 03-05, 03-08, 03-10, 03-11, 03-12), `e2e/signals_test.go` (new: 03-07), `e2e/harness_test.go` (mod: 03-05) | e2e tests (build tag `sil_e2e`) | request-response + streaming | `e2e/rev33_test.go` (r33Up, setFan, waitDuties) + `e2e/harness_test.go` (Env.Up, waitFor) + `e2e/watch_test.go` | exact |
| `Makefile` (mod: 03-02: `test-ui`, `vet -tags sil_ui`) | build | n/a | itself (`e2e` L41, `vet` L19) | exact |
| `go.mod`, `go.sum` (mod: 03-01: `github.com/coder/websocket v1.8.15`) | module manifest | n/a | itself (Phase 1 `flynn/noise` addition) | exact |
| `README.md` (mod: 03-07, 03-12), `.planning/REQUIREMENTS.md`, `.planning/research/PITFALLS.md` (mod: 03-12), `.planning/phases/03-live-web-ui/03-UI-SPEC.md` (mod: 03-09, 03-12) | docs | n/a | themselves | exact |

## Pattern Assignments

### `ui/embed.go` (embed package)

**Analog:** `fans/embed.go` (whole file, 9 lines):
```go
// Package fans embeds the shipped fan library: one TOML file per fan model.
package fans

import "embed"

// FS holds the shipped fan parameter files.
//
//go:embed *.toml
var FS embed.FS
```
Phase 3 variant: `//go:embed all:web` then `func FS() fs.FS { sub, _ := fs.Sub(webFS, "web"); return sub }`. Placing `embed.go` in `ui/` is mandatory (`go:embed` cannot reach parent dirs). `--ui-dir` swaps in `os.DirFS(dir)` as `web.Options.Static` at the call site in the supervisor (03-05), so `internal/web` and the tests take either.

---

### `internal/web/web.go` (HTTP compose and redacting Control wrapper, request-response)

One file holds both the handler composition (`NewHandler`, `Options`, `RedactedKey`; 03-01, remounted on `control.NewLANHandler` by 03-04) and the redacting wrapper. No plan splits them into separate files.

**Analog (composition):** `internal/control/control.go` `NewHandler` (L357). The web handler mounts the control handler under `/api/v1/` rather than copying routes:
```go
func NewHandler(c Control) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := c.Status(r.Context())
		reply(w, st, err)
	})
```
Compose rule (RESEARCH Pattern 1): `mux.Handle("/api/v1/", control.NewHandler(redacting{c}))` in 03-01, replaced by `control.NewLANHandler(redacting{c})` in 03-04; `mux.Handle("GET /ws", newWS(redacting{c}, o))` (ws.go); `mux.Handle("/", http.FileServerFS(o.Static))` for GET and HEAD. The Host, header and CSRF guard is not in web.go. It is `web.Guard` in `guard.go` (03-05), wrapped around `NewHandler` from the outside in the supervisor and in `uitest/harness_test.go`. Do not wrap `ResponseWriter` without preserving `Flusher`/`Hijacker` (streamNDJSON at L501 type-asserts `http.Flusher`).

**Server start:** in 03-01 the web listener reuses `control.ServeListener(ctx, ln, h)` (L567) unchanged; it deliberately sets only `Handler` because the wall-clock lint bans `ReadTimeout`/`ReadHeaderTimeout`/`WriteTimeout`/`IdleTimeout` (lint `bannedFields`, `wallclock_test.go` L36-44). 03-03 replaces it for the web listener with `web.Serve` (same shape plus `srv.SetKeepAlivesEnabled(false)`) over `web.Limit` (see `internal/web/listener.go`); the client side already does `DisableKeepAlives: true` (control.go ~L608).

**Listener wiring in the supervisor** (`internal/supervisor/supervisor.go` L252-258), the exact place to add the second listener:
```go
ln, err := control.Listen(s.paths.APISock())
if err != nil {
	return err
}
srvCtx, srvCancel := context.WithCancel(context.Background())
srvDone := make(chan error, 1)
go func() { srvDone <- control.ServeListener(srvCtx, ln, control.NewHandler(s)) }()
```
Add a sibling `webDone` goroutine over the listener from `instance.ListenWeb(s.webListen, s.id.WebPort)` (port.go, 03-01). It serves `web.NewHandler(s, web.Options{...})`, from 03-03 on through `web.Serve(srvCtx, web.Limit(webLn, s.clk, web.ListenerOptions{}), ...)` and from 03-05 on wrapped in `web.Guard(...)`. `<-srvDone` at L270 must also wait on `webDone`. The socket handler keeps `control.NewHandler(s)` (full Status incl. key). Only the TCP handler gets the redactor (D-18) and the lifecycle and logs refusals (D-19, D-20; table rows from 03-04).

**Analog (redacting wrapper):** embedding the interface in a struct so only some methods are overridden, as in `control_test.go` L19-22:
```go
type fakeControl struct {
	Control  // later additions to the interface compile without touching this fake
	sim      []SimRestartRequest
```
Production wrapper (unexported, in web.go): `type redacting struct{ control.Control }` overriding `Status`, `SimRestart`, `FwRestart` and `Watch`. Its `Watch` wraps `send` and copies a `control.SnapshotLine` with the key replaced; `Down`'s result carries no key. The key lives at `Status.API.Key` (set at `supervisor.go` ~L669 `Key: s.id.Key`) and is replaced by `RedactedKey` = `<redacted>` (D-18). The evidence is e2e `TestWebTracer/status_redacted` and `ws_snapshot` (03-01) and `TestSnapshotEqualsStatus` in `internal/supervisor/web_test.go` (03-04): the socket Status has the key, and the TCP Status and snapshot carry `<redacted>`.

---

### `internal/web/guard.go` (middleware)

**Analog:** JSON error shape from `control.go` L350-352 and L531-546:
```go
type errorBody struct {
	Error *apperr.Error `json:"error"`
}
...
func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		ae := apperr.AsError(err)
		code := http.StatusInternalServerError
		if ae.Exit == apperr.ExitFailed {
			code = http.StatusBadRequest
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(errorBody{Error: ae})
		return
	}
```
`reply` is unexported in `control`; the plans export `control.WriteError(w, err)` and `control.WriteErrorStatus(w, status, err)` (03-01) so guard.go and web.go write the same body. Rejections (`host_rejected`, `origin_rejected` here; `operation_not_allowed` for the D-19/D-20 refusals) must use `apperr.Failed("<literal>", msg, detail)` with a literal code and be added to `Registry` (see apperr section). Guard order: host allowlist, security headers, `http.NewCrossOriginProtection()` with `SetDenyHandler` (RESEARCH "Guard middleware"). The D-19/D-20 refusals do not live in guard.go. They are three hard-coded routes in web.go in 03-01, and from 03-04 on the `LAN false` rows of the `Operations` table, answered by `control.NewLANHandler` (see below).

---

### `internal/web/ws.go` (WS adapter, streaming)

**Analog:** `streamNDJSON` (`control.go` L500-529) and the supervisor `Watch` (`supervisor/watch.go` L54-98). The WS handler is the same shape with `c.Write` instead of `w.Write`. Copy the per-line encoder exactly so frames are byte-identical to `sil watch` lines:
```go
send := func(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
```
The data source is `Control.Watch(ctx, req, send)`, whose first send is `control.SnapshotLine{T, K:"snapshot", Seq, Status}` (`watch.go` L79), then events (L91), then `StoppedLine` (L86):
```go
snap, seq := s.joinSnapshot(ctx, sub)
st := s.statusFrom(&snap)
if err := send(control.SnapshotLine{T: snap.SimTime.Microseconds(), K: "snapshot", Seq: seq, Status: st}); err != nil {
	return err
}
```
Stream-slot semantics to respect (`watch.go` L13-34): `acquireStream` + `events.MaxSubscribers` (16) are shared with CLI `watch` and `logs --follow`; a returned error before the first send turns into a normal error reply. For WS, send errors after upgrade as a final `{"error":{...}}` text frame then close (RESEARCH Pattern 2). Use `clock.WithTimeout(ctx, clk, d)` for write/ping budgets and `clk.NewTicker(15*time.Second)` for pings, never `context.WithTimeout`/`time.NewTicker` (lint).

Redaction on the TCP path applies to the `SnapshotLine` passed to `send` (it embeds `Status`), so the redacting wrapper in `web.go` wraps `Watch`'s `send` func and replaces `API.Key` when `v` is `control.SnapshotLine`. 03-03 adds the ping goroutine and the 8-stream browser counter (`maxBrowserStreams`), copying the CAS loop of `supervisor.acquireStream` (`watch.go` L21-34).

---

### `internal/web/listener.go` (accept wrapper: conn cap + header deadline; `Serve`)

**Analog:** `control.Listen` / `control.ServeListener` (`control.go` L545-595): the error style (`apperr.Infra("control_listen_failed", ...)` with `path`/`error` detail) and the server shape that `web.Serve` copies (Handler only, 2 s graceful shutdown on `clock.WithTimeout`), plus `srv.SetKeepAlivesEnabled(false)`. `web.Limit(ln, clk, ListenerOptions{MaxConns, HeaderBudget})` wraps `Accept`. A connection over the cap is closed at once. Otherwise `Accept` calls `conn.SetReadDeadline(clk.Deadline(o.HeaderBudget))` (the `Clock.Deadline` method exists exactly for socket deadlines, `clock.go` L25-26) and returns a conn whose `Close` releases the slot once (`sync.Once`). The port-in-use mapping is not here: binding the web port is `instance.ListenWeb` / `TestBindWeb` (see the instance section).

---

### `internal/web/listener_test.go`, `ws_test.go`, `guard_test.go`

**Analog:** `internal/control/control_test.go`: `httptest` + embedded-interface fake (L19-57), `post(h, path, body)` helper (L59-64), table tests (L66+), and the streaming harness `serveStream`/`streamControl` (L219-259). For WS parity (`TestSnapshotEqualsStatus` in `internal/supervisor/web_test.go`, 03-04; the same harness serves the 03-07 additions to `internal/supervisor/supervisor_test.go`) reuse `watchSup` from `internal/supervisor/watch_test.go` L21-50 (Manual clock + broker + engine, no firmware):
```go
clk := clock.NewManual()
b := events.NewBroker()
s := &supervisor{clk: clk, log: slog.New(slog.NewTextHandler(io.Discard, nil)), broker: b,
	engCfg: engine.Config{Clock: clk, BusPath: filepath.Join(dir, "bus.sock"), Emit: b.Publish, Seq: b.Seq}, ...
```
Temp dirs under `/tmp` via `os.MkdirTemp("/tmp", "sil-")` (socket path length).

---

### `internal/control/operations.go` (route table, UI-06 spine)

**Analog:** the inline registrations in `NewHandler` (`control.go` L359-492). Each becomes a row `{Name, Method, Path, Transport, CLI, LAN, handler}` (03-04; Transport `json` | `ndjson` | `ws`). `NewHandler` iterates the table, and `NewLANHandler` serves the `LAN true` rows and answers the others 403 `operation_not_allowed`. Keep every existing handler body as is. Existing routes to table: `GET /api/v1/status`, `POST /api/v1/down`, `POST /api/v1/sim/restart`, `POST /api/v1/fw/restart`, `POST /api/v1/buttons/{id}/press|down|up`, `POST /api/v1/set`, `GET /api/v1/fans`, `GET /api/v1/watch`, `GET /api/v1/logs`; new: `GET /api/v1/history`. Body-decoding idiom to copy for the new `down` body and `history` params (limit 64 KiB, strict fields, empty body tolerated):
```go
body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
if err != nil { reply(w, nil, apperr.Failed("invalid_argument", "cannot read the request body: "+err.Error(), nil)); return }
if len(bytes.TrimSpace(body)) > 0 {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil { reply(w, nil, apperr.Failed("invalid_argument", "bad request body: "+err.Error(), nil)); return }
}
if err := req.Validate(); err != nil { reply(w, nil, err); return }
```
(This is the `sim/restart` handler, L370-388: the template for `down` with optional `{"max_hold_ms":N}`.) `PressRequest.Validate` (bounds `1..MaxHold`) is the model for `max_hold_ms` validation.

`TestOperationsCLI` in `internal/cli/operations_test.go` (03-04) asserts that every row's `CLI` string parses with kong, using a parser built exactly like `Execute` (`kong.New(&CLI{}, kong.Name("sil"), ...)`, `cli.go` L171), and that it selects the command its leading words name. The test style follows `cli_test.go` `TestCommandsAreControlClients` (L387). `TestOperationsTable` sits in `internal/control/control_test.go`.

---

### `internal/control/control.go` (mod)

Additive only, follow "Fields are extended by later phases, never renamed" (L45). Patterns:
- `SimStatus` (L108-113): add `BehindRealTime bool \`json:"behind_real_time"\`` next to `TimeFactor`.
- `Control` interface (L322-346): add `History(ctx, HistoryRequest) (HistoryResult, error)`; the `fakeControl` idiom embeds `Control`, so existing fakes keep compiling, but `internal/cli/cli_test.go` fakes need checking (RESEARCH Pitfall 11).
- `WatchRequest` / `Validate` / `WatchPath` (L231-251): `kind=` query plus `sample`. `Validate` uses `events.UnknownKind`, so `UnknownKind` must accept `SampleKind`, while default (empty kinds) excludes it.
- `Client` methods: copy `Client.Fans` / `SetButton` shape (`c.do(ctx, method, path, body, &res)`), including `url.PathEscape(id)` (L~680).
- `SetButton(ctx, id, down bool)` gains a request struct: `SetButton(ctx, id string, down bool, r DownRequest)` with `DownRequest{MaxHoldMS *int64}` (03-07); update `supervisor`, `Client`, `cli.PressCmd`, `fakeControl.SetButton` (control_test.go L49) and the cli_test fake.

---

### `internal/events/events.go` (mod: `sample` kind + history ring)

**Analog:** itself. Core points to copy:
```go
// Kinds ... (L23)
var Kinds = []string{"duty", "rpm", "state", "fan", "button", "env", "leds", "firmware", "sim"}

func (b *Broker) Publish(ev Event) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed { return b.seq }
	b.seq++
	ev.Seq = b.seq
	for s := range b.subs { s.offer(ev) }
	return ev.Seq
}
```
Append `sample` events to a fixed ring **inside the same critical section** that assigns `seq` (so `History` is consistent with every subscriber). Keep `Kinds` unchanged and add `SampleKind = "sample"`; `UnknownKind` (L47) must accept it; `Subscribe` with empty kinds currently means "all": make "all" exclude `sample` explicitly (`s.kinds == nil` path in `offer`, L113) so default `sil watch` is not flooded. `Event.V any` (L35) carries the per-port slice; `null` while disconnected must be encodable (use `*float64` or `any(nil)` elements). Tests: `events_test.go` `UnknownKind(Kinds)` interaction.

---

### `internal/engine/engine.go` (mod: sample, behind-real-time, max-hold)

**Analog:** itself. The tick branch (L443-470) is where all three hang:
```go
case now := <-ticker.C():
	dt := now - last
	last = now
	e.simNow = now
	e.inTick = true
	for i := range e.ports { ... model.Step(...); e.emitState(p) }
	e.emitRPM()
	e.inTick = false
	if e.connected && e.simNow >= e.nextRPM {
		e.nextRPM = e.simNow + rpmFrameStep
		e.sendRPM(false)
	}
	if e.connected { e.resendEnv() }
	e.publish()
```
- Sample emission: mimic the `nextRPM`/`rpmFrameStep` schedule (`const rpmFrameStep = 50 * time.Millisecond`, L31) with a `nextSample += 100ms` of **sim** time; emit via `e.emit(events.Event{K: "sample", V: ...})`. The first sample an engine emits carries `Gap: true` (fresh `sampled` field; `Run` L416 starts `simNow` from the shared supervisor clock, so sim time runs on across a sim restart and the marker shows where it broke).
- Behind-real-time: derive from `dt` and `clk.Now() - now` in this branch; constants next to `tick` (L30); expose through the snapshot struct that `statusFrom` reads (`supervisor.go` `Status` builder L583+); emit `events.Event{K:"sim", V:"behind"|"in_sync"}` on transitions (the existing `sim` kind carries `"restarted"`). Test via a small hook, not Manual per-tick delivery (RESEARCH Pattern 4).
- Max-hold: `handleButton` (L756-790) is the place. Existing release edge code to reuse for the auto-release:
```go
if e.buttons[op.id] != op.level {
	e.emit(events.Event{K: "button", ID: op.id, V: op.level})
}
e.buttons[op.id] = op.level
if e.connected {
	e.send(e.buttonFrame(op.id))
}
```
Store `holdUntil[id] = simNow + max`, check in the tick branch, release with the same three statements plus additive `Event.Reason: "max_hold"`. Note `buttonOp` (L184) gains the max-hold field. Strict-rule errors `ErrButtonBusy` / `ErrButtonNotHeld` are unchanged (a redundant `up` after auto-release must still answer `button_not_held`).
- Tests: copy the `clock.Manual` style of `internal/engine/engine_test.go`.

---

### `internal/instance/instance.go` + `port.go` (mod: sticky web port)

**Analog:** itself. Identity (L152-157):
```go
type Identity struct {
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	APIPort int    `json:"api_port"`
	Key     string `json:"key"`
}
```
Add `WebPort int \`json:"web_port,omitempty"\``; absent in older identities means allocate under the same `LockAlloc` flock in `LoadOrCreate` (L194-240). `AllocatePort`/`claimedPorts`/`storedPort` (`port.go` L26-96) read the field by name. 03-01 generalizes them so that `claimedPorts` returns every `api_port` and every `web_port`, adds `AllocateWebPort`, and keeps the web range `8054..9053` disjoint from `6054..7053`. `instance.TestBind` (`port.go` L98-117) is the analog for the new `ListenWeb(listen, port)` / `TestBindWeb(listen, port)`, which take the listen address instead of the hard-coded `0.0.0.0` (RESEARCH Pitfall 12):
```go
ln, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
if err == nil {
	return ln.Close()
}
if !errors.Is(err, syscall.EADDRINUSE) {
	return apperr.Infra("port_bind_failed", fmt.Sprintf("cannot test-bind API port %d: %v", port, err), ...)
}
...
return apperr.Infra("port_in_use", msg, detail)
```
Tests (`instance_test.go`, 03-05) follow `TestAllocatePort` and `TestLoadOrCreateBusyOverrideIsNotPersisted`. Sticky and conflict semantics to copy verbatim: explicit `--web-port` validated then `port_claimed` if another instance holds it, stored only after a successful `TestBind` (comment L216-218), taken sticky port reported by `TestBind` (`port_in_use`, exit 2) and never moved.

`RunFile` (L312-319): additive `web_port`, `web_listen` fields (decoded without `DisallowUnknownFields`, so older readers stay fine). `Spec` (L416-421) gains `Listen`, `AllowHosts`, `UIDir`, `WebPort`; `ReadSpec` validation (L436+) stays strict only on the required fields (`Program`, `Fans`).

URL for output uses the LAN host helper: `instance/lanhost.go` (`lanHostPtr(lan)` use in `lifecycle.go` ~L222-227). Reuse it so `sil up`/`sil status` print `http://<lan-ip>:<web_port>/`.

---

### `internal/lifecycle/lifecycle.go` (mod: web port flow, stderr hint)

**Analog:** itself L162-227. Sequence to extend: `instance.LoadOrCreate(p, o.APIPort)` (L167) -> `instance.TestBind(id.APIPort)` (L171, L205) -> `instance.WriteSpec(...)` (L197) -> progress hint (L222):
```go
fmt.Fprintf(progress, "add to Home Assistant: host %s port %d, encryption key from this JSON (no mDNS on host; ...)\n", hint, id.APIPort)
```
Add an equivalent stderr line for the page URL plus the "API is unauthenticated on the LAN" statement (D-08). The result JSON gains `web: {url, port, listen}` (`UpResult.Web`, 03-05). Add `WebPort`, `Listen`, `AllowHosts`, `UIDir` to the options struct (field at L45 `APIPort int`); `lifecycle_test.go` gains `TestCheckWebOptions`, a table test of the pure option checks in the style of its `TestResolveFan` (L44) and `TestResolveConfigRefusesBroadMount` (L15).

---

### `internal/cli/cli.go` (mod: new flags, `history`)

**Analog:** `UpCmd` (L62-69) for flag declaration style, `PressCmd` (L105-113), `FansCmd` (L120-122), `WatchCmd` (L127-131):
```go
type UpCmd struct {
	Name    string `arg:"" help:"Instance name (a-z, 0-9, '-')."`
	...
	APIPort int    `name:"api-port" help:"Firmware native API port (default: the stored port, first 6054)."`
```
```go
type PressCmd struct {
	Button   string         `arg:"" help:"Button: usr1, usr2 or usr3."`
	Down     bool           `xor:"mode" help:"Hold the button down and return at once."`
	Up       bool           `xor:"mode" help:"Release the button and return at once."`
	Instance string         `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}
```
- `UpCmd`: add `WebPort int name:"web-port"`, `Listen string`, `AllowHost []string name:"allow-host"`, `UIDir string name:"ui-dir"`.
- `PressCmd`: add `MaxHold *time.Duration name:"max-hold"` (valid only with `--down`; enforce in `Run` with `apperr.Failed("invalid_argument", ...)` like `WatchCmd.Run` L440-443).
- New `HistoryCmd` modeled on `FansCmd.Run` (L425-437): `connect(c.Instance)`, `client.History(env.context, req)`, `env.result = res`. Register in the root struct next to `Fans`. Window param via query string like `LogsRequest.LogsPath`. The instance selector follows the existing grammar: positional `Name` for status, down, sim restart and fw restart; `--instance/-i` for press, set, fans, watch, logs and the new history.
- Error pattern for all `Run`: `apperr.Failed("invalid_argument", msg, map[string]any{...})`.
- stdout = JSON result only, human text to stderr (CLI-05).

---

### `internal/apperr/codes.go` (mod)

**Analog:** itself. Registry is a literal map; `TestRegistryMatchesCallSites` (`codes_test.go`) scans `internal/` and `cmd/` and fails on an unregistered/duplicated/non-literal code or an unused registered code. Add new codes under the right class, alphabetical-ish as the file is:
```go
"button_not_held": ExitFailed,
...
"port_in_use":     ExitInfra,
```
Candidates: `host_rejected` (ExitFailed), `origin_rejected` (ExitFailed), `operation_not_allowed` or similar for D-19 refusals (ExitFailed). `too_many_streams` already exists (ExitInfra) and is reused for the browser WS cap. Every call site must pass a literal first arg to `apperr.Failed`/`Infra` and use only one class per code.

---

### `internal/cdptest/cdptest.go` (Chrome launcher + CDP client, shared by uitest and e2e)

**Analog:** `e2e/harness_test.go` (package-level `sysClock = clock.NewReal()` L24-25, `clock.WithTimeout(context.Background(), sysClock, 60*time.Second)` L37, a clear failure when a host tool is missing) and `e2e/watch_test.go` (child-process wrapper with goroutine line reader, `t.Cleanup` kill, L19-60):
```go
cmd := exec.Command(env.Bin, args...)
...
t.Cleanup(func() { _ = cmd.Process.Kill() })
```
Mirror those for the Chrome launcher (`--headless=new --remote-debugging-port=0 --user-data-dir=<tmp> --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE 127.0.0.1"`, read `DevToolsActivePort`, `Setpgid` so `Close` kills the group). It is a normal package, not a `_test.go` file, so that `uitest` and `e2e` can share it. It takes its waits from the `clock.Clock` passed to `Launch`, and `cmd/sil` never imports it. The CDP transport is `github.com/coder/websocket` (Dial, JSON read/write). Requests are recorded from `Network.requestWillBeSent` (kind http) and `Network.webSocketCreated` (kind ws).

---

### `uitest/*_test.go` (build tag `sil_ui`: browser tests over a fake bench)

Files: `harness_test.go`, `bench_test.go`, `live_test.go` (03-02), `parity_test.go` (03-04), `leds_test.go`, `layout_test.go` (03-06), `inputs_test.go` (03-08), `plot_test.go` (03-10), `hold_test.go`, `resilience_test.go` (03-11).

**Analog:** `e2e/harness_test.go` (tag line, package doc, `TestMain` that fails fast with a clear message, `sysClock`) for `harness_test.go`; the `fakeControl` embed idiom (`control_test.go` L19-22) and `supervisor.Watch`'s subscribe-then-snapshot join (`watch.go` L54-98) for the fake bench in `bench_test.go`. **The wall-clock lint scans test files too**: all polling uses `sysClock.Sleep(ctx, d)` / `clock.WithTimeout`, never `time.Sleep`/`context.WithTimeout`. Fixture server: `web.NewHandler` over the fake bench (from 03-05 wrapped in `web.Guard`), no docker.

---

### `e2e/web_test.go`, `e2e/signals_test.go`, `e2e/harness_test.go` (real-firmware tests, build tag `sil_e2e`)

**Analog:** `e2e/rev33_test.go` (`r33Up`, `setFan`, `waitDuties`, `press`) and `e2e/harness_test.go` (`Env.Up`, `Env.Status`, `waitFor`, `freePort`). `web_test.go` (new in 03-01) gains subtests or tests in 03-02, 03-05, 03-08, 03-10, 03-11 and 03-12. `signals_test.go` (new in 03-07) holds `TestHistory`, `TestBehindField` and `TestMaxHold`. `harness_test.go` (03-05) makes `Env.Up` inject `--listen 127.0.0.1` and a free `--web-port` next to `--api-port`. CLI calls follow the grammar of `internal/cli/cli.go` (positional name for status, down, sim restart, fw restart; `-i` for press, set, fans, watch, logs, history).

---

### `internal/supervisor/history.go` (Control method, new in 03-07)

**Analog:** `supervisor.go` `Fans` (L1178): a read-only `control.Control` method that builds its result without `opMu` and maps failures to `apperr`. `History` validates the request, answers `apperr.Infra("internal", ...)` when there is no broker, and returns `HistoryResult` from `s.broker.History(window)`, the ring filled under the publish lock (see events.go). The broker access mirrors `watch.go`.

---

### `ui/ui_test.go`, `ui/vendor_test.go` (no-external-refs + DOM scans; vendor hashes)

**Analog:** `internal/lint/wallclock_test.go` for walking the repo and reporting file:line failures (`repoRoot(t)` L190, `TestNoWallClock` L210). `ui_test.go` (03-04; 03-08 adds `TestJSOpsInOperations`) scans only first-party files (`index.html`, `app.css`, `js/*.js`) for `https?://`, because vendored files legitimately contain URLs (RESEARCH Pitfall 5). It also asserts no `innerHTML`/`eval(` in first-party JS. `vendor_test.go` (03-09) holds `TestVendorHashes`, which reads `ui/web/vendor/VENDOR.md` and re-hashes with `crypto/sha256`, and `TestVendorServed`. The expected hashes are in RESEARCH "Vendored files": `uPlot.iife.min.js` 19c8d4c6..., `uPlot.min.css` df630c6a...

---

### `go.mod`, `go.sum` (mod: 03-01)

**Analog:** the Phase 1 addition of `github.com/flynn/noise v1.1.0`: `go get <module>@<version>`, `go mod tidy`, `go mod verify`, and the `h1:` hashes checked against the values the sum DB returned during research.

---

### `Makefile` (mod)

**Analog:** itself:
```make
.PHONY: build test test-pkg vet fmt-check check mod-verify e2e test-cpp test-cpp-image lock-check test-contract

vet:
	$(GO) vet ./...
	$(GO) vet -tags sil_e2e ./e2e/

e2e:
	$(GO) test -tags sil_e2e -count=1 -timeout 30m -v -run '$(E2E_RUN)' ./e2e/
```
Add `$(GO) vet -tags sil_ui ./uitest/` to `vet`, a `test-ui` target modeled on `e2e` (`-tags sil_ui`), and extend `.PHONY`. `fmt-check` excludes only `.git prototype .planning .gsd`; `ui/` Go files and `uitest/` are checked, JS is not.

---

## Shared Patterns

### Wall-clock discipline (applies to every new Go file, including tests)
**Source:** `internal/clock/clock.go` L10-27, L123-145; lint `internal/lint/wallclock_test.go` L28-44
```go
rctx, cancel := clock.WithTimeout(ctx, clk, budget)   // not context.WithTimeout
defer cancel()
...
if clock.TimedOut(rctx) { ... }
conn.SetReadDeadline(clk.Deadline(10 * time.Second))   // not time.Now().Add
```
Banned: `time.Now/Since/Sleep/After/Tick/NewTimer/NewTicker`, `context.WithTimeout/WithDeadline`, `http.Server{ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout}`, `net.Dialer{Timeout}`, `http.Client{Timeout}`, `exec.Cmd.WaitDelay`. The lint walks every `.go` file except `.git`, `testdata`, `prototype`, `internal/clock`. Injected `clock.Clock` for production; `clock.Manual` for tests; `clock.NewReal()` only at process edges (as in `ServeListener` L574).

### Error construction and reply
**Source:** `internal/apperr` (+ `control.go` `reply` L531)
```go
apperr.Failed("invalid_argument", fmt.Sprintf("tail must be between 1 and %d lines, got %d", MaxLogTail, r.Tail),
	map[string]any{"tail": r.Tail, "min": 1, "max": MaxLogTail})
apperr.Infra("too_many_streams", msg, map[string]any{"limit": events.MaxSubscribers})
```
Literal code, one exit class, detail map; registry entry required. HTTP mapping: `ExitFailed` -> 400, else 500, body `{"error":{...}}`. The page shows `error.message` verbatim (D-17).

### Request body decoding for POST routes
**Source:** `control.go` L370-388 (`sim/restart`, empty body allowed) and L474-492 (`set`, body required)
Limit `1<<16`, `DisallowUnknownFields`, then `req.Validate()`; validators live as methods on the request type (`PressRequest.Validate`, `SimRestartRequest.Validate`, `LogsRequest.Validate`) and return `apperr.Failed("invalid_argument", ...)`.

### NDJSON / frame encoding
**Source:** `control.go` `streamNDJSON` L500-529: `json.NewEncoder(&buf)`, `SetEscapeHTML(false)`, one value per line. The WS frame payload equals one such line without the trailing newline (or with; decide once and test against `sil watch`).

### Sticky identity-stored values and exit class for conflicts
**Source:** `instance.LoadOrCreate` L194-240 + `AllocatePort` L69-96: stored value wins, explicit flag overrides only after validation and a successful bind, taken sticky port is `port_in_use` (exit 2) and never moves.

### Test fakes
**Source:** `control_test.go` L19-22 embedded-interface fake, `post(...)` helper L59-64, `serveStream` L235-257 (real Unix-socket server + `Client` for streaming tests), `supervisor/watch_test.go` `watchSup` L21-50 (Manual clock, broker, real engine without firmware).

### Run-dir / transport security posture
**Source:** `control.Listen` (L546-561): 0600 `api.sock` inside the 0700 run dir remains the only place the full Status (with `api.key`) is served. TCP listener: key redacted (D-18), lifecycle operations refused (D-19) and logs refused (D-20) as `LAN false` rows, Host allowlist and CSRF check (RESEARCH Pitfall 3), no auth by decision D-08.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `ui/web/index.html`, `ui/web/app.css` | static page | request-response | No HTML/CSS exists in the repo; use `03-UI-SPEC.md` (tokens, layout, copy) and RESEARCH "Recommended Project Structure". Favicon `data:,`; `data-src`/`data-fmt` on every readout (parity sweep). |
| `ui/web/js/*.js` (app, store, view, api, inputs, message, ring, plot, buttons) | browser modules | event-driven (WS) | No JS in the repo (`prototype/` has none). Use RESEARCH "Code Examples" (fixed-scale uPlot config, button hold handler) and Pitfalls 4, 6, 9. Plain arrays with `null` for series (not `Float64Array`); gaps come only from the server (`null` values and the `gap` marker of 03-07); `textContent` only; `releasePointerCapture` in `pointerdown`. |
| `ui/web/vendor/{uPlot.iife.min.js,uPlot.min.css,LICENSE.uplot,VENDOR.md}` | vendored assets | file-I/O | One-off `npm pack uplot@1.6.32`; sha256 values listed in RESEARCH Standard Stack. |

Partial analogs, listed in the table with their closest match:

| File | Role | Data Flow | Note |
|------|------|-----------|------|
| `internal/cdptest/cdptest.go` | test helper | request-response over WS | No browser automation exists; `e2e/harness_test.go` and `e2e/watch_test.go` supply only structure (child process, cleanup, line reader). The CDP protocol handling is new. |
| `internal/web/ws.go` ping/cap logic (8-of-16 browser WS cap) | stream governance | event-driven | Closest is `supervisor.acquireStream` (`watch.go` L21-34), but a browser-only counter is new; copy its CAS loop and error constructor. |

## Metadata

**Analog search scope:** `internal/{control,events,engine,supervisor,instance,lifecycle,cli,apperr,clock,lint}`, `fans/`, `e2e/`, `cmd/sil`, `Makefile`, `.planning/phases/02-full-rev-3-3-bench/02-PATTERNS.md` (format).
**Files scanned:** about 30 read in whole or in targeted ranges; `control.go` (867 lines) and `engine.go` (1004 lines) read by section.
**Pattern extraction date:** 2026-11-09

package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/model"
)

// fakeControl records the restart requests the handler forwards.
type fakeControl struct {
	Control  // later additions to the interface compile without touching this fake
	sim      []SimRestartRequest
	fw       int
	press    []PressRequest
	pressID  string
	edges    []bool // SetButton calls: true = down
	edgeID   string
	downReqs []DownRequest // the DownRequest of each SetButton call
	pressErr error
}

func (f *fakeControl) Status(context.Context) (Status, error) { return Status{Phase: PhaseReady}, nil }
func (f *fakeControl) Down(context.Context) (DownResult, error) {
	return DownResult{Stopped: true}, nil
}
func (f *fakeControl) SimRestart(_ context.Context, r SimRestartRequest) (Status, error) {
	f.sim = append(f.sim, r)
	return Status{Phase: PhaseReady}, nil
}
func (f *fakeControl) FwRestart(context.Context) (Status, error) {
	f.fw++
	return Status{Phase: PhaseReady}, nil
}

func (f *fakeControl) Press(_ context.Context, id string, r PressRequest) (PressResult, error) {
	f.pressID = id
	f.press = append(f.press, r)
	return PressResult{Button: id, Action: "press", HoldMS: r.HoldMS, TDownUS: 1000, TUpUS: 1000 + r.HoldMS*1000}, f.pressErr
}

func (f *fakeControl) SetButton(_ context.Context, id string, down bool, r DownRequest) (PressResult, error) {
	f.edgeID = id
	f.edges = append(f.edges, down)
	f.downReqs = append(f.downReqs, r)
	action := "up"
	if down {
		action = "down"
	}
	return PressResult{Button: id, Action: action, Level: down}, nil
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSimRestartRoute(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantCode   int
		wantGap    float64
		errCode    string
	}{
		{"empty body", "", 200, 0, ""},
		{"zero gap", `{"gap_s":0}`, 200, 0, ""},
		{"three seconds", `{"gap_s":3}`, 200, 3, ""},
		{"upper bound", `{"gap_s":30}`, 200, 30, ""},
		{"too long", `{"gap_s":30.5}`, 400, 0, "invalid_argument"},
		{"negative", `{"gap_s":-1}`, 400, 0, "invalid_argument"},
		{"unknown field", `{"gap":3}`, 400, 0, "invalid_argument"},
		{"not json", `gap`, 400, 0, "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeControl{}
			rec := post(NewHandler(fc), "/api/v1/sim/restart", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if tc.errCode != "" {
				var eb struct{ Error *apperr.Error }
				if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || eb.Error == nil || eb.Error.Code != tc.errCode {
					t.Fatalf("body %s, want error %s", rec.Body, tc.errCode)
				}
				if len(fc.sim) != 0 {
					t.Fatalf("an invalid request reached the supervisor: %v", fc.sim)
				}
				return
			}
			if len(fc.sim) != 1 || fc.sim[0].GapS != tc.wantGap {
				t.Fatalf("forwarded %v, want one request with gap %v", fc.sim, tc.wantGap)
			}
		})
	}
}

func TestFwRestartRoute(t *testing.T) {
	fc := &fakeControl{}
	if rec := post(NewHandler(fc), "/api/v1/fw/restart", ""); rec.Code != 200 || fc.fw != 1 {
		t.Fatalf("status %d, calls %d; body %s", rec.Code, fc.fw, rec.Body)
	}
}

func TestPressRoute(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantCode   int
		wantHold   int64
		errCode    string
	}{
		{"lower bound", `{"hold_ms":1}`, 200, 1, ""},
		{"short preset", `{"hold_ms":200}`, 200, 200, ""},
		{"upper bound", `{"hold_ms":60000}`, 200, 60000, ""},
		{"zero", `{"hold_ms":0}`, 400, 0, "invalid_argument"},
		{"too long", `{"hold_ms":60001}`, 400, 0, "invalid_argument"},
		{"negative", `{"hold_ms":-5}`, 400, 0, "invalid_argument"},
		{"empty body", ``, 400, 0, "invalid_argument"},
		{"unknown field", `{"hold_ms":200,"hold":1}`, 400, 0, "invalid_argument"},
		{"not json", `hold`, 400, 0, "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeControl{}
			rec := post(NewHandler(fc), "/api/v1/buttons/usr1/press", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if tc.errCode != "" {
				var eb struct{ Error *apperr.Error }
				if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || eb.Error == nil || eb.Error.Code != tc.errCode {
					t.Fatalf("body %s, want error %s", rec.Body, tc.errCode)
				}
				if len(fc.press) != 0 {
					t.Fatalf("an invalid request reached the supervisor: %v", fc.press)
				}
				return
			}
			if len(fc.press) != 1 || fc.press[0].HoldMS != tc.wantHold || fc.pressID != "usr1" {
				t.Fatalf("forwarded %v for %q, want one press of %d ms on usr1", fc.press, fc.pressID, tc.wantHold)
			}
			var res PressResult
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Button != "usr1" || res.Action != "press" || res.HoldMS != tc.wantHold {
				t.Fatalf("result %s (%v)", rec.Body, err)
			}
		})
	}
}

func TestPressRouteKeepsTheErrorClass(t *testing.T) {
	fc := &fakeControl{pressErr: apperr.Failed("button_busy", "held", map[string]any{"button": "usr2"})}
	rec := post(NewHandler(fc), "/api/v1/buttons/usr2/press", `{"hold_ms":200}`)
	var eb struct{ Error *apperr.Error }
	if rec.Code != 400 || json.Unmarshal(rec.Body.Bytes(), &eb) != nil || eb.Error == nil || eb.Error.Code != "button_busy" || eb.Error.Detail["button"] != "usr2" {
		t.Fatalf("status %d, body %s, want 400 button_busy with detail.button usr2", rec.Code, rec.Body)
	}
}

func TestButtonDownUpRoutes(t *testing.T) {
	fc := &fakeControl{}
	h := NewHandler(fc)
	if rec := post(h, "/api/v1/buttons/usr3/down", ""); rec.Code != 200 {
		t.Fatalf("down: %d %s", rec.Code, rec.Body)
	}
	if rec := post(h, "/api/v1/buttons/usr3/up", ""); rec.Code != 200 {
		t.Fatalf("up: %d %s", rec.Code, rec.Body)
	}
	if len(fc.edges) != 2 || !fc.edges[0] || fc.edges[1] || fc.edgeID != "usr3" {
		t.Fatalf("edges %v on %q, want [true false] on usr3", fc.edges, fc.edgeID)
	}
}

type fansControl struct {
	Control
	res FansResult
	err error
}

func (f fansControl) Fans(context.Context) (FansResult, error) { return f.res, f.err }

// GET /api/v1/fans carries exactly the FansResult; POST is not routed; an error keeps its class.
func TestFansRoute(t *testing.T) {
	want := FansResult{Fans: []FanInfo{{ID: "a", Name: "A", MinRPM: 1, MaxRPM: 2, BelowMin: "stop"}}}
	h := NewHandler(fansControl{res: want})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fans", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/v1/fans = %d: %s", rec.Code, rec.Body)
	}
	if got, w := strings.TrimSpace(rec.Body.String()), `{"fans":[{"id":"a","name":"A","min_rpm":1,"max_rpm":2,"below_min":"stop","schema":0,"provenance":null,"values":null,"sources":null}]}`; got != w {
		t.Fatalf("body %s, want %s", got, w)
	}
	if rec := post(h, "/api/v1/fans", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/fans = %d, want 405", rec.Code)
	}
	bad := NewHandler(fansControl{err: apperr.Failed("fan_toml_invalid", "bad file", nil)})
	rec = httptest.NewRecorder()
	bad.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fans", nil))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "fan_toml_invalid") {
		t.Fatalf("failing Fans = %d %s, want 400 fan_toml_invalid", rec.Code, rec.Body)
	}
}

// streamControl is a fake whose Watch sends one line and then holds the stream open until the
// request ends (or release is closed), like a real watch between two events.
type streamControl struct {
	Control
	watchErr error
	release  chan struct{}
	gotKinds []string
}

func (f *streamControl) Watch(ctx context.Context, r WatchRequest, send func(any) error) error {
	f.gotKinds = r.Kinds
	if f.watchErr != nil {
		return f.watchErr
	}
	if err := send(SnapshotLine{T: 5, K: "snapshot", Seq: 7, Status: Status{Schema: SchemaVersion, Instance: "x", Phase: PhaseReady}}); err != nil {
		return err
	}
	select {
	case <-f.release:
		return send(NewStoppedLine(0))
	case <-ctx.Done():
		return ctx.Err()
	}
}

func serveStream(t *testing.T, fc Control) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "silctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "api.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeListener(ctx, ln, NewHandler(fc)) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return Dial(sock)
}

// The first line must reach the client while the handler is still running: the stream is flushed
// over api.sock, not buffered until the handler returns (Research A5).
func TestWatchStreamsOverTheSocket(t *testing.T) {
	fc := &streamControl{release: make(chan struct{})}
	c := serveStream(t, fc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rc, err := c.Stream(ctx, WatchRequest{Kinds: []string{"duty", "button"}}.WatchPath())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	if !sc.Scan() {
		t.Fatalf("no first line while the handler runs: %v", sc.Err())
	}
	var snap map[string]any
	if err := json.Unmarshal(sc.Bytes(), &snap); err != nil || snap["k"] != "snapshot" || snap["seq"] != float64(7) || snap["instance"] != "x" {
		t.Fatalf("first line %q: want the snapshot with the Status fields at the top level (%v)", sc.Text(), err)
	}
	if strings.Join(fc.gotKinds, ",") != "duty,button" {
		t.Fatalf("Watch got kinds %v, want duty,button", fc.gotKinds)
	}
	close(fc.release)
	if !sc.Scan() || !IsStoppedLine(sc.Bytes()) {
		t.Fatalf("second line %q, want the stopped line", sc.Text())
	}
	if sc.Scan() {
		t.Fatalf("extra line after stopped: %q", sc.Text())
	}
}

func TestWatchResponseType(t *testing.T) {
	fc := &streamControl{release: make(chan struct{})}
	close(fc.release)
	rec := httptest.NewRecorder()
	NewHandler(fc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/watch", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("Content-Type %q, want application/x-ndjson", ct)
	}
	if lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n"); len(lines) != 2 {
		t.Fatalf("%d lines, want snapshot and stopped: %q", len(lines), rec.Body.String())
	}
}

func TestWatchErrorBeforeTheFirstLineIsAJSONError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		exit int
	}{
		{"infra", apperr.Infra("too_many_streams", "full", nil), 500, apperr.ExitInfra},
		{"user", apperr.Failed("stopping", "going down", nil), 400, apperr.ExitFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewHandler(&streamControl{watchErr: tc.err}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/watch", nil))
			if rec.Code != tc.code || rec.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("status %d type %q, want %d application/json", rec.Code, rec.Header().Get("Content-Type"), tc.code)
			}
			c := serveStream(t, &streamControl{watchErr: tc.err})
			_, err := c.Stream(context.Background(), "/api/v1/watch")
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Code != apperr.AsError(tc.err).Code || ae.Exit != tc.exit {
				t.Fatalf("Client.Stream error %v, want %s with exit class %d", err, apperr.AsError(tc.err).Code, tc.exit)
			}
		})
	}
}

func TestWatchUnknownKindIsInvalidArgument(t *testing.T) {
	fc := &streamControl{release: make(chan struct{})}
	rec := httptest.NewRecorder()
	NewHandler(fc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/watch?kind=duty,nope", nil))
	var body struct {
		Error *apperr.Error `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 400 || body.Error == nil || body.Error.Code != "invalid_argument" {
		t.Fatalf("status %d body %s, want 400 invalid_argument", rec.Code, rec.Body.String())
	}
	if body.Error.Detail["kind"] != "nope" {
		t.Fatalf("detail %v, want kind nope", body.Error.Detail)
	}
	if fc.gotKinds != nil {
		t.Fatal("Watch ran for an invalid kind")
	}
}

// Client.Watch hands each line over as a raw message and reports a stream that ends without the
// stopped line as control_unreachable.
func TestClientWatchRelayAndLostStream(t *testing.T) {
	fc := &streamControl{release: make(chan struct{})}
	close(fc.release)
	c := serveStream(t, fc)
	var lines []string
	err := c.Watch(context.Background(), WatchRequest{}, func(v any) error {
		lines = append(lines, string(v.(json.RawMessage)))
		return nil
	})
	if err != nil || len(lines) != 2 || !IsStoppedLine([]byte(lines[1])) {
		t.Fatalf("Watch: %v, lines %q, want snapshot and stopped, no error", err, lines)
	}

	// A fake that returns without a stopped line: the connection simply ends.
	lost := serveStream(t, &lostControl{})
	err = lost.Watch(context.Background(), WatchRequest{}, func(any) error { return nil })
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "control_unreachable" || ae.Exit != apperr.ExitInfra {
		t.Fatalf("lost stream: %v, want control_unreachable (exit 2)", err)
	}
}

type lostControl struct{ Control }

func (lostControl) Watch(_ context.Context, _ WatchRequest, send func(any) error) error {
	return send(map[string]any{"k": "duty", "seq": 1})
}

// logsControl records the logs requests and answers canned lines.
type logsControl struct {
	Control
	got    []LogsRequest
	follow bool
}

func (f *logsControl) Logs(_ context.Context, r LogsRequest) (LogsResult, error) {
	f.got = append(f.got, r)
	return LogsResult{Source: r.Source, Lines: []LogLine{{Src: "firmware", Line: "=== firmware start 1 ==="}}}, nil
}

func (f *logsControl) FollowLogs(_ context.Context, r LogsRequest, send func(any) error) error {
	f.got = append(f.got, r)
	f.follow = true
	if err := send(LogLine{Src: "firmware", Line: "tail"}); err != nil {
		return err
	}
	return send(NewStoppedLine(0))
}

func TestLogsRoute(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		code        int
		want        LogsRequest
		errCode     string
	}{
		{"defaults", "", 200, LogsRequest{Source: "firmware", Tail: 100}, ""},
		{"all", "source=all&tail=7&raw=1", 200, LogsRequest{Source: "all", Tail: 7, Raw: true}, ""},
		{"upper bound", "tail=10000", 200, LogsRequest{Source: "firmware", Tail: 10000}, ""},
		{"unknown source", "source=nope", 400, LogsRequest{}, "invalid_argument"},
		{"build log is not a source", "source=build", 400, LogsRequest{}, "invalid_argument"},
		{"a path is not a source", "source=../firmware.log", 400, LogsRequest{}, "invalid_argument"},
		{"tail zero", "tail=0", 400, LogsRequest{}, "invalid_argument"},
		{"tail too big", "tail=10001", 400, LogsRequest{}, "invalid_argument"},
		{"tail not a number", "tail=x", 400, LogsRequest{}, "invalid_argument"},
		{"raw not 0 or 1", "raw=yes", 400, LogsRequest{}, "invalid_argument"},
		{"follow not 0 or 1", "follow=2", 400, LogsRequest{}, "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &logsControl{}
			rec := httptest.NewRecorder()
			NewHandler(fc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/logs?"+tc.query, nil))
			if rec.Code != tc.code {
				t.Fatalf("status %d (%s), want %d", rec.Code, rec.Body.String(), tc.code)
			}
			if tc.errCode != "" {
				var body struct {
					Error *apperr.Error `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil || body.Error.Code != tc.errCode || len(fc.got) != 0 {
					t.Fatalf("body %s (requests reaching the control: %v), want %s without reaching it", rec.Body.String(), fc.got, tc.errCode)
				}
				return
			}
			if len(fc.got) != 1 || fc.got[0] != tc.want {
				t.Fatalf("control got %+v, want %+v", fc.got, tc.want)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("Content-Type %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestLogsFollowStreamsNDJSON(t *testing.T) {
	fc := &logsControl{}
	rec := httptest.NewRecorder()
	NewHandler(fc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/logs?follow=1&source=sim", nil))
	if rec.Header().Get("Content-Type") != "application/x-ndjson" || !fc.follow {
		t.Fatalf("type %q follow %v, want application/x-ndjson from FollowLogs", rec.Header().Get("Content-Type"), fc.follow)
	}
	if lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n"); len(lines) != 2 || !IsStoppedLine([]byte(lines[1])) {
		t.Fatalf("body %q, want the tail line and the stopped line", rec.Body.String())
	}
}

func TestClientLogsAndFollow(t *testing.T) {
	fc := &logsControl{}
	c := serveStream(t, fc)
	res, err := c.Logs(context.Background(), LogsRequest{Source: "all", Tail: 3, Raw: true})
	if err != nil || res.Source != "all" || len(res.Lines) != 1 || fc.got[0] != (LogsRequest{Source: "all", Tail: 3, Raw: true}) {
		t.Fatalf("Client.Logs = %+v, %v (control got %+v)", res, err, fc.got)
	}
	var lines []string
	err = c.FollowLogs(context.Background(), LogsRequest{Source: "firmware", Tail: 5}, func(v any) error {
		lines = append(lines, string(v.(json.RawMessage)))
		return nil
	})
	if err != nil || len(lines) != 2 || lines[0] != `{"src":"firmware","line":"tail"}` {
		t.Fatalf("Client.FollowLogs = %q, %v", lines, err)
	}
	if _, err := c.Logs(context.Background(), LogsRequest{Source: "nope", Tail: 3}); err == nil {
		t.Fatal("an invalid source reached the control")
	}
}

// tableFake implements every Control method and counts the calls, so a handler built from the
// operation table can be exercised for every row.
type tableFake struct{ calls int }

func (f *tableFake) Status(context.Context) (Status, error)   { f.calls++; return Status{}, nil }
func (f *tableFake) Down(context.Context) (DownResult, error) { f.calls++; return DownResult{}, nil }
func (f *tableFake) SimRestart(context.Context, SimRestartRequest) (Status, error) {
	f.calls++
	return Status{}, nil
}
func (f *tableFake) FwRestart(context.Context) (Status, error) { f.calls++; return Status{}, nil }
func (f *tableFake) Press(context.Context, string, PressRequest) (PressResult, error) {
	f.calls++
	return PressResult{}, nil
}
func (f *tableFake) SetButton(context.Context, string, bool, DownRequest) (PressResult, error) {
	f.calls++
	return PressResult{}, nil
}
func (f *tableFake) Set(context.Context, SetRequest) (SetResult, error) {
	f.calls++
	return SetResult{}, nil
}
func (f *tableFake) Fans(context.Context) (FansResult, error) { f.calls++; return FansResult{}, nil }
func (f *tableFake) History(context.Context, HistoryRequest) (HistoryResult, error) {
	f.calls++
	return HistoryResult{}, nil
}
func (f *tableFake) Watch(_ context.Context, _ WatchRequest, send func(any) error) error {
	f.calls++
	return send(map[string]string{"k": "snapshot"})
}
func (f *tableFake) Logs(context.Context, LogsRequest) (LogsResult, error) {
	f.calls++
	return LogsResult{Lines: []LogLine{}}, nil
}
func (f *tableFake) FollowLogs(_ context.Context, _ LogsRequest, send func(any) error) error {
	f.calls++
	return send(LogLine{})
}

// tableRequest builds a valid request for the row.
func tableRequest(op Operation) *http.Request {
	body := ""
	switch op.Name {
	case "press":
		body = `{"hold_ms":200}`
	case "set":
		body = `{"values":{"temperature":22}}`
	}
	path := strings.ReplaceAll(op.Path, "{id}", "usr1")
	return httptest.NewRequest(op.Method, path, strings.NewReader(body))
}

func TestOperationsTable(t *testing.T) {
	names := map[string]bool{}
	patterns := map[string]bool{}
	for _, op := range Operations {
		pat := op.Method + " " + op.Path
		if names[op.Name] || patterns[pat] {
			t.Errorf("row %s (%s) is a duplicate", op.Name, pat)
		}
		names[op.Name], patterns[pat] = true, true
		if op.CLI == "" {
			t.Errorf("row %s has no CLI command", op.Name)
		}
		switch op.Transport {
		case "json", "ndjson", "ws":
		default:
			t.Errorf("row %s has transport %q", op.Name, op.Transport)
		}
		if (op.handler == nil) != (op.Name == "watch.ws") {
			t.Errorf("row %s: handler nil = %v, want nil exactly for watch.ws", op.Name, op.handler == nil)
		}
	}
	for _, want := range []string{"status", "down", "sim.restart", "fw.restart", "press", "button.down", "button.up", "set", "fans", "history", "watch", "logs", "watch.ws"} {
		if _, ok := OperationByName(want); !ok {
			t.Errorf("row %s is missing", want)
		}
	}
	if _, ok := OperationByName("nope"); ok {
		t.Error("OperationByName found a row that does not exist")
	}

	t.Run("socket handler serves every json and ndjson row", func(t *testing.T) {
		for _, op := range Operations {
			if op.Transport == "ws" {
				continue
			}
			f := &tableFake{}
			rec := httptest.NewRecorder()
			NewHandler(f).ServeHTTP(rec, tableRequest(op))
			if rec.Code != 200 || f.calls != 1 {
				t.Errorf("%s: status %d, %d calls; body %s", op.Name, rec.Code, f.calls, rec.Body)
			}
		}
	})

	t.Run("no route outside the table", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{"GET", "/ws"}, {"GET", "/api/v1/nope"}, {"GET", "/api/v1/down"}, {"POST", "/api/v1/status"},
		} {
			for name, h := range map[string]http.Handler{"socket": NewHandler(&tableFake{}), "lan": NewLANHandler(&tableFake{})} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
				if rec.Code != 404 && rec.Code != 405 {
					t.Errorf("%s handler: %s %s answered %d, want 404 or 405", name, tc.method, tc.path, rec.Code)
				}
			}
		}
	})

	t.Run("lan handler serves LAN rows and refuses the others", func(t *testing.T) {
		for _, op := range Operations {
			if op.Transport == "ws" {
				continue
			}
			f := &tableFake{}
			rec := httptest.NewRecorder()
			NewLANHandler(f).ServeHTTP(rec, tableRequest(op))
			if op.LAN {
				if rec.Code != 200 || f.calls != 1 {
					t.Errorf("%s: LAN row answered %d with %d calls; body %s", op.Name, rec.Code, f.calls, rec.Body)
				}
				continue
			}
			var eb struct{ Error *apperr.Error }
			if rec.Code != 403 || json.Unmarshal(rec.Body.Bytes(), &eb) != nil || eb.Error == nil || eb.Error.Code != "operation_not_allowed" {
				t.Errorf("%s: socket-only row answered %d %s, want 403 operation_not_allowed", op.Name, rec.Code, rec.Body)
				continue
			}
			if f.calls != 0 {
				t.Errorf("%s: a refused row reached the Control (%d calls)", op.Name, f.calls)
			}
			if !strings.Contains(eb.Error.Message, "`sil "+op.CLI+"`") || eb.Error.Detail["operation"] != op.Name || eb.Error.Detail["cli"] != op.CLI {
				t.Errorf("%s: refusal does not name its command: %+v", op.Name, eb.Error)
			}
		}
		// The exact LAN set the plan fixes: nothing is exposed by accident.
		var lan []string
		for _, op := range Operations {
			if op.LAN {
				lan = append(lan, op.Name)
			}
		}
		if got := strings.Join(lan, ","); got != "status,press,button.down,button.up,set,fans,history,watch,watch.ws" {
			t.Errorf("LAN rows = %s", got)
		}
	})

	t.Run("MatchOperation", func(t *testing.T) {
		for _, tc := range []struct {
			method, path, want string
		}{
			{"POST", "/api/v1/buttons/usr2/down", "button.down"},
			{"POST", "/api/v1/buttons/usr1/press", "press"},
			{"GET", "/ws", "watch.ws"},
			{"GET", "/api/v1/status", "status"},
			{"GET", "/api/v1/logs", "logs"},
			{"GET", "/nope", ""},
			{"POST", "/api/v1/status", ""},
			{"GET", "/api/v1/buttons/usr1/press", ""},
		} {
			op, ok := MatchOperation(tc.method, tc.path)
			if (tc.want == "") == ok || op.Name != tc.want {
				t.Errorf("MatchOperation(%s, %s) = %q, %v; want %q", tc.method, tc.path, op.Name, ok, tc.want)
			}
		}
	})
}

// historyControl records the requests its History receives.
type historyControl struct {
	Control
	got []HistoryRequest
	res HistoryResult
	err error
}

func (f *historyControl) History(_ context.Context, r HistoryRequest) (HistoryResult, error) {
	f.got = append(f.got, r)
	return f.res, f.err
}

func getPath(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHistoryRoute(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		wantCode    int
		wantWindow  float64
	}{
		{"default window", "", 200, 300},
		{"fractional window", "?window_s=2.5", 200, 2.5},
		{"upper bound", "?window_s=300", 200, 300},
		{"zero", "?window_s=0", 400, 0},
		{"negative", "?window_s=-1", 400, 0},
		{"too long", "?window_s=301", 400, 0},
		{"not a number", "?window_s=NaN", 400, 0},
		{"text", "?window_s=abc", 400, 0},
		{"empty value", "?window_s=", 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &historyControl{}
			rec := getPath(NewHandler(fc), "/api/v1/history"+tc.query)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if tc.wantCode != 200 {
				var eb struct{ Error *apperr.Error }
				if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || eb.Error == nil || eb.Error.Code != "invalid_argument" {
					t.Fatalf("body %s, want invalid_argument", rec.Body)
				}
				if len(fc.got) != 0 {
					t.Fatalf("an invalid window reached the control: %v", fc.got)
				}
				return
			}
			if len(fc.got) != 1 || fc.got[0].WindowS != tc.wantWindow {
				t.Fatalf("forwarded %v, want one request with window_s %v", fc.got, tc.wantWindow)
			}
		})
	}

	t.Run("an empty ring answers samples []", func(t *testing.T) {
		rec := getPath(NewHandler(&historyControl{}), "/api/v1/history")
		if !strings.Contains(rec.Body.String(), `"samples":[]`) {
			t.Fatalf("body %s, want samples as an empty array and not null", rec.Body)
		}
	})

	t.Run("the LAN handler serves it", func(t *testing.T) {
		fc := &historyControl{res: HistoryResult{Seq: 9, T: 5, WindowS: 1, Samples: []events.Event{{T: 5, K: "sample", Seq: 9}}}}
		rec := getPath(NewLANHandler(fc), "/api/v1/history?window_s=1")
		if rec.Code != 200 || len(fc.got) != 1 || fc.got[0].WindowS != 1 {
			t.Fatalf("LAN history: %d %s (requests %v)", rec.Code, rec.Body, fc.got)
		}
		var res HistoryResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Seq != 9 || len(res.Samples) != 1 {
			t.Fatalf("body %s (%v), want the HistoryResult unchanged", rec.Body, err)
		}
		if op, ok := OperationByName("history"); !ok || !op.LAN || op.CLI != "history" || op.Transport != "json" {
			t.Fatalf("history row = %+v, %v, want a LAN json row with CLI history", op, ok)
		}
	})

	t.Run("an error keeps its class", func(t *testing.T) {
		rec := getPath(NewHandler(&historyControl{err: apperr.Infra("internal", "no broker", nil)}), "/api/v1/history")
		if rec.Code != 500 {
			t.Fatalf("status %d, want 500", rec.Code)
		}
	})
}

func TestHistoryRequestPath(t *testing.T) {
	if p := (HistoryRequest{}).HistoryPath(); p != "/api/v1/history" {
		t.Fatalf("HistoryPath() = %q, want no window_s when 0", p)
	}
	if p := (HistoryRequest{WindowS: 60}).HistoryPath(); p != "/api/v1/history?window_s=60" {
		t.Fatalf("HistoryPath() = %q", p)
	}
	if p := (HistoryRequest{WindowS: 2.5}).HistoryPath(); p != "/api/v1/history?window_s=2.5" {
		t.Fatalf("HistoryPath() = %q", p)
	}
}

func TestClientHistory(t *testing.T) {
	fc := &historyControl{res: HistoryResult{Seq: 3, T: 7, WindowS: 60, Samples: []events.Event{{T: 7, K: "sample", Seq: 3}}}}
	c := serveStream(t, fc)
	res, err := c.History(context.Background(), HistoryRequest{WindowS: 60})
	if err != nil || res.Seq != 3 || len(res.Samples) != 1 || len(fc.got) != 1 || fc.got[0].WindowS != 60 {
		t.Fatalf("Client.History = %+v, %v (control got %v)", res, err, fc.got)
	}
	if _, err := c.History(context.Background(), HistoryRequest{}); err != nil || fc.got[1].WindowS != 300 {
		t.Fatalf("Client.History with no window = %v, control got %v, want the default 300", err, fc.got)
	}
}

func ptr[T any](v T) *T { return &v }

// POST .../down takes an optional {"max_hold_ms": N}; up takes no body at all.
func TestDownBody(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantCode   int
		wantMS     *int64
	}{
		{"empty body", "", 200, nil},
		{"empty object", "{}", 200, nil},
		{"max hold", `{"max_hold_ms":1500}`, 200, ptr(int64(1500))},
		{"lower bound", `{"max_hold_ms":1}`, 200, ptr(int64(1))},
		{"upper bound", `{"max_hold_ms":60000}`, 200, ptr(int64(60000))},
		{"zero", `{"max_hold_ms":0}`, 400, nil},
		{"too long", `{"max_hold_ms":60001}`, 400, nil},
		{"negative", `{"max_hold_ms":-5}`, 400, nil},
		{"not a number", `{"max_hold_ms":"abc"}`, 400, nil},
		{"fraction", `{"max_hold_ms":1.5}`, 400, nil},
		{"unknown field", `{"hold_ms":1500}`, 400, nil},
		{"not json", `max`, 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeControl{}
			rec := post(NewHandler(fc), "/api/v1/buttons/usr2/down", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if tc.wantCode != 200 {
				var eb struct{ Error *apperr.Error }
				if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil || eb.Error == nil || eb.Error.Code != "invalid_argument" {
					t.Fatalf("body %s, want invalid_argument", rec.Body)
				}
				if len(fc.edges) != 0 {
					t.Fatalf("an invalid body reached the control: %v", fc.edges)
				}
				return
			}
			if len(fc.downReqs) != 1 || !fc.edges[0] {
				t.Fatalf("control calls %v / %v, want one down", fc.edges, fc.downReqs)
			}
			got := fc.downReqs[0].MaxHoldMS
			if (got == nil) != (tc.wantMS == nil) || (got != nil && *got != *tc.wantMS) {
				t.Fatalf("forwarded max_hold_ms %v, want %v", got, tc.wantMS)
			}
		})
	}

	t.Run("up refuses a body", func(t *testing.T) {
		fc := &fakeControl{}
		for _, body := range []string{`{"max_hold_ms":1500}`, `{}`, `x`} {
			rec := post(NewHandler(fc), "/api/v1/buttons/usr2/up", body)
			var eb struct{ Error *apperr.Error }
			if rec.Code != 400 || json.Unmarshal(rec.Body.Bytes(), &eb) != nil || eb.Error == nil || eb.Error.Code != "invalid_argument" {
				t.Fatalf("up with body %q: %d %s, want 400 invalid_argument", body, rec.Code, rec.Body)
			}
		}
		if rec := post(NewHandler(fc), "/api/v1/buttons/usr2/up", ""); rec.Code != 200 || len(fc.edges) != 1 || fc.edges[0] {
			t.Fatalf("up without a body: %d %s, edges %v", rec.Code, rec.Body, fc.edges)
		}
	})

	t.Run("the result carries max_hold_ms", func(t *testing.T) {
		res := PressResult{Button: "usr1", Action: "down", Level: true, MaxHoldMS: 1500}
		raw, _ := json.Marshal(res)
		if !strings.Contains(string(raw), `"max_hold_ms":1500`) {
			t.Fatalf("PressResult marshals as %s", raw)
		}
		raw, _ = json.Marshal(PressResult{Button: "usr1", Action: "down"})
		if strings.Contains(string(raw), "max_hold_ms") {
			t.Fatalf("PressResult without a max hold marshals as %s, want the member omitted", raw)
		}
	})
}

func TestClientSetButtonBody(t *testing.T) {
	fc := &fakeControl{}
	c := serveStream(t, fc)
	if _, err := c.SetButton(context.Background(), "usr1", true, DownRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetButton(context.Background(), "usr1", true, DownRequest{MaxHoldMS: ptr(int64(1500))}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetButton(context.Background(), "usr1", false, DownRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(fc.downReqs) != 3 || fc.downReqs[0].MaxHoldMS != nil || fc.downReqs[1].MaxHoldMS == nil || *fc.downReqs[1].MaxHoldMS != 1500 || fc.downReqs[2].MaxHoldMS != nil {
		t.Fatalf("server saw %+v, want no max hold, 1500, none", fc.downReqs)
	}
}

// fanInfoKeys are the JSON keys of a FanInfo (MODEL-12): the five of v0.1.0 and the four additions.
var fanInfoKeys = []string{"id", "name", "min_rpm", "max_rpm", "below_min", "schema", "provenance", "values", "sources"}

func fanInfoObject(t *testing.T, fi FanInfo) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(fi)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != len(fanInfoKeys) {
		t.Errorf("FanInfo JSON %s, want exactly the keys %v", b, fanInfoKeys)
	}
	for _, k := range fanInfoKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("FanInfo JSON %s has no key %q", b, k)
		}
	}
	return m
}

func TestNewFanInfo(t *testing.T) {
	t.Run("schema 1 records no provenance and says so with null", func(t *testing.T) {
		fi := NewFanInfo("old", model.Params{Name: "Old", MinRPM: 200, MaxRPM: 1800, BelowMin: "stop"})
		if fi.Schema != 1 {
			t.Errorf("Schema = %d, want 1 (0 counts as 1)", fi.Schema)
		}
		m := fanInfoObject(t, fi)
		for _, k := range []string{"provenance", "values", "sources"} {
			if string(m[k]) != "null" {
				t.Errorf("%s = %s, want null", k, m[k])
			}
		}
		if string(m["schema"]) != "1" || string(m["id"]) != `"old"` {
			t.Errorf("schema %s id %s", m["schema"], m["id"])
		}
	})

	t.Run("schema 2 counts per kind and fills the value map", func(t *testing.T) {
		p := model.Params{
			Name: "New", MinRPM: 300, MaxRPM: 1700, BelowMin: "stop", Schema: 2,
			Provenance: []model.ProvEntry{
				{Path: "stop.duty", Kind: model.KindDatasheet, Source: "page"},
				{Path: "start.duty", Kind: model.KindMeasured},
				{Path: "curve.up", Kind: model.KindAssumed},
			},
			Sources: map[string]string{"page": "vendor page"},
		}
		fi := NewFanInfo("new", p)
		if fi.Provenance == nil || *fi.Provenance != (ProvenanceCounts{Measured: 1, Datasheet: 1, Assumed: 1}) {
			t.Fatalf("Provenance = %+v, want measured 1 datasheet 1 assumed 1 unobservable 0", fi.Provenance)
		}
		if got := fi.Values["stop.duty"]; got.Kind != "datasheet" || got.Source == nil || *got.Source != "page" {
			t.Errorf("stop.duty = %+v, want datasheet:page", got)
		}
		if got := fi.Values["start.duty"]; got.Kind != "measured" || got.Source != nil {
			t.Errorf("start.duty = %+v, want measured without a source", got)
		}
		if len(fi.Values) != 3 || fi.Sources["page"] != "vendor page" || len(fi.Sources) != 1 {
			t.Errorf("Values %v Sources %v", fi.Values, fi.Sources)
		}
		m := fanInfoObject(t, fi)
		if string(m["provenance"]) != `{"measured":1,"datasheet":1,"assumed":1,"unobservable":0}` {
			t.Errorf("provenance JSON %s", m["provenance"])
		}
		if want := `{"kind":"measured","source":null}`; !strings.Contains(string(m["values"]), want) {
			t.Errorf("values JSON %s lacks %s", m["values"], want)
		}
	})

	t.Run("schema 2 without sources shows an empty object", func(t *testing.T) {
		fi := NewFanInfo("new", model.Params{Schema: 2, Provenance: []model.ProvEntry{{Path: "stop.duty", Kind: model.KindAssumed}}})
		if m := fanInfoObject(t, fi); string(m["sources"]) != "{}" {
			t.Errorf("sources = %s, want {}", m["sources"])
		}
	})
}

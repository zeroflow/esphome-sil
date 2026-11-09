package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zeroflow/esphome-sil/internal/control"
)

// keyControl is a Control that holds a real-looking key, like the supervisor.
type keyControl struct {
	control.Control
}

const secretKey = "c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0LTEyMzQ="

func (keyControl) status() control.Status {
	return control.Status{Schema: 1, Instance: "t", API: control.APIStatus{Key: secretKey}}
}

func (k keyControl) Status(context.Context) (control.Status, error) { return k.status(), nil }

func (k keyControl) SimRestart(context.Context, control.SimRestartRequest) (control.Status, error) {
	return k.status(), nil
}

func (k keyControl) FwRestart(context.Context) (control.Status, error) { return k.status(), nil }

func (k keyControl) Watch(_ context.Context, _ control.WatchRequest, send func(any) error) error {
	return send(control.SnapshotLine{K: "snapshot", Seq: 1, Status: k.status()})
}

func testHandler() http.Handler {
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "js/app.js": {Data: []byte("1")}}
	return NewHandler(keyControl{}, Options{Static: static})
}

func TestStatusKeyIsRedacted(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/v1/status = %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secretKey) {
		t.Fatalf("GET /api/v1/status leaks the key: %s", rec.Body)
	}
	var st control.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.API.Key != RedactedKey {
		t.Fatalf("api.key = %q (%v), want %q", st.API.Key, err, RedactedKey)
	}
}

// The restart routes are refused on the web handler, but the wrapper redacts their Status anyway,
// so a later change to the refusals cannot open a leak.
func TestRestartStatusKeyIsRedacted(t *testing.T) {
	r := redacting{keyControl{}}
	sim, err := r.SimRestart(context.Background(), control.SimRestartRequest{})
	if err != nil || sim.API.Key != RedactedKey {
		t.Fatalf("SimRestart api.key = %q (%v), want %q", sim.API.Key, err, RedactedKey)
	}
	fw, err := r.FwRestart(context.Background())
	if err != nil || fw.API.Key != RedactedKey {
		t.Fatalf("FwRestart api.key = %q (%v), want %q", fw.API.Key, err, RedactedKey)
	}
}

func TestWatchSnapshotKeyIsRedacted(t *testing.T) {
	var got []any
	err := redacting{keyControl{}}.Watch(context.Background(), control.WatchRequest{}, func(v any) error {
		got = append(got, v)
		return nil
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("Watch = %v, %d values", err, len(got))
	}
	line, ok := got[0].(control.SnapshotLine)
	if !ok || line.API.Key != RedactedKey {
		t.Fatalf("snapshot = %#v, want a SnapshotLine with api.key %q", got[0], RedactedKey)
	}
	if (keyControl{}).status().API.Key != secretKey {
		t.Fatal("redaction changed the underlying status")
	}
}

func TestStaticIsReadOnly(t *testing.T) {
	h := testHandler()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/", 200},
		{http.MethodHead, "/", 200},
		{http.MethodGet, "/js/app.js", 200},
		{http.MethodGet, "/nope.js", 404},
		{http.MethodPost, "/", 405},
		{http.MethodPut, "/js/app.js", 405},
		{http.MethodDelete, "/index.html", 405},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

func TestWSRejectsUnknownKindBeforeUpgrade(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws?kind=nope", nil))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"invalid_argument"`) {
		t.Fatalf("GET /ws?kind=nope = %d %s, want 400 invalid_argument", rec.Code, rec.Body)
	}
}

// countingControl counts the lifecycle calls that reach it.
type countingControl struct {
	keyControl
	calls int
}

func (c *countingControl) Down(context.Context) (control.DownResult, error) {
	c.calls++
	return control.DownResult{}, nil
}

func (c *countingControl) SimRestart(context.Context, control.SimRestartRequest) (control.Status, error) {
	c.calls++
	return control.Status{}, nil
}

func (c *countingControl) FwRestart(context.Context) (control.Status, error) {
	c.calls++
	return control.Status{}, nil
}

func TestLifecycleRefusedAndNeverReachesControl(t *testing.T) {
	c := &countingControl{}
	h := NewHandler(c, Options{})
	for _, path := range []string{"/api/v1/down", "/api/v1/sim/restart", "/api/v1/fw/restart"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"gap_s":1}`)))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"operation_not_allowed"`) {
			t.Fatalf("POST %s = %d %s, want 403 operation_not_allowed", path, rec.Code, rec.Body)
		}
	}
	if c.calls != 0 {
		t.Fatalf("%d lifecycle calls reached the Control, want 0", c.calls)
	}
}

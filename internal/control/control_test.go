package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
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

func (f *fakeControl) SetButton(_ context.Context, id string, down bool) (PressResult, error) {
	f.edgeID = id
	f.edges = append(f.edges, down)
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
	if got, w := strings.TrimSpace(rec.Body.String()), `{"fans":[{"id":"a","name":"A","min_rpm":1,"max_rpm":2,"below_min":"stop"}]}`; got != w {
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

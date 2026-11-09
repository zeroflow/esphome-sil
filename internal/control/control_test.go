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
	Control // later additions to the interface compile without touching this fake
	sim     []SimRestartRequest
	fw      int
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

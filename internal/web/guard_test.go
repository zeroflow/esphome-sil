package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// recControl counts every call that changes the bench, so a test can tell a refused request from
// one that reached the Control.
type recControl struct {
	keyControl
	changes atomic.Int32
}

func (c *recControl) Down(context.Context) (control.DownResult, error) {
	c.changes.Add(1)
	return control.DownResult{}, nil
}

func (c *recControl) SimRestart(context.Context, control.SimRestartRequest) (control.Status, error) {
	c.changes.Add(1)
	return control.Status{}, nil
}

func (c *recControl) FwRestart(context.Context) (control.Status, error) {
	c.changes.Add(1)
	return control.Status{}, nil
}

func (c *recControl) Press(context.Context, string, control.PressRequest) (control.PressResult, error) {
	c.changes.Add(1)
	return control.PressResult{}, nil
}

func (c *recControl) SetButton(context.Context, string, bool) (control.PressResult, error) {
	c.changes.Add(1)
	return control.PressResult{}, nil
}

func (c *recControl) Set(context.Context, control.SetRequest) (control.SetResult, error) {
	c.changes.Add(1)
	return control.SetResult{}, nil
}

func (c *recControl) Fans(context.Context) (control.FansResult, error) {
	return control.FansResult{}, nil
}

func (c *recControl) History(context.Context, control.HistoryRequest) (control.HistoryResult, error) {
	return control.HistoryResult{}, nil
}

// guarded is the production chain around a recording Control: the guard outside web.NewHandler.
func guarded(c control.Control, o GuardOptions) http.Handler {
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "js/app.js": {Data: []byte("1")}}
	return Guard(NewHandler(c, Options{Static: static}), o)
}

// do sends one request with the given Host and headers (pairs) straight to h.
func do(h http.Handler, method, target, host, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = host
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// errCode is error.code of a JSON error body ("" when the body is not one).
func errCode(rec *httptest.ResponseRecorder) string {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		return ""
	}
	return body.Error.Code
}

const setBody = `{"values":{"temperature":"23"}}`

// A request for a host name the bench does not know is the shape of DNS rebinding (T-03-13): it is
// refused before any handler, with a message that tells the developer the way out.
func TestHostGuard(t *testing.T) {
	c := &recControl{}
	h := guarded(c, GuardOptions{AllowHosts: []string{"bench.test"}, Hostname: "TestBox"})

	for _, host := range []string{
		"127.0.0.1:8054", "127.0.0.1", "[::1]:8054", "[::1]", "192.168.4.20:8054", "localhost:8054", "localhost",
		"testbox:8054", "testbox", "testbox.local:8054", "TESTBOX.LOCAL:8054", "bench.test:8054", "Bench.Test",
	} {
		if rec := do(h, http.MethodGet, "/api/v1/status", host, ""); rec.Code != 200 {
			t.Errorf("Host %q = %d %s, want 200", host, rec.Code, rec.Body)
		}
	}

	for _, host := range []string{
		"evil.example:8054", "evil.example", "", "evil.example.:8054", "localhost.:8054", "127.0.0.1.evil.example:8054",
		"testbox.evil.example", "testbox.local.:8054", "bench.test.:8054", "[::1:8054", "evil.example:8054:1",
		"127.0.0.1@evil.example", "bench.test,evil.example", "bench.test evil.example", "127.0.0.1:abc", "127.0.0.1:",
		"localhost:80\"; script-src *",
	} {
		for _, path := range []string{"/api/v1/status", "/", "/ws", "/js/app.js"} {
			rec := do(h, http.MethodGet, path, host, "")
			if rec.Code != http.StatusForbidden || errCode(rec) != "host_rejected" {
				t.Errorf("GET %s with Host %q = %d %s, want 403 host_rejected", path, host, rec.Code, rec.Body)
				continue
			}
			if !strings.Contains(rec.Body.String(), "--allow-host") {
				t.Errorf("Host %q: the message does not name --allow-host: %s", host, rec.Body)
			}
		}
	}

	// Rebinding turns a POST into a same-origin request in the browser's eyes: it carries no
	// cross-site header at all, so only the Host check stands between the page and the bench.
	rec := do(h, http.MethodPost, "/api/v1/set", "evil.example:8054", setBody, "Origin", "http://evil.example:8054", "Sec-Fetch-Site", "same-origin")
	if rec.Code != http.StatusForbidden || errCode(rec) != "host_rejected" {
		t.Errorf("rebound POST /api/v1/set = %d %s, want 403 host_rejected", rec.Code, rec.Body)
	}
	if n := c.changes.Load(); n != 0 {
		t.Errorf("%d changes reached the Control through a rejected Host, want 0", n)
	}

	// Without --allow-host only the built-in names pass.
	plain := guarded(&recControl{}, GuardOptions{Hostname: "testbox"})
	if rec := do(plain, http.MethodGet, "/api/v1/status", "bench.test:8054", ""); rec.Code != http.StatusForbidden {
		t.Errorf("bench.test without --allow-host = %d, want 403", rec.Code)
	}
}

// A browser marks a cross-site request itself (Sec-Fetch-Site, Origin): none of them may change the
// bench (T-03-11). A request without those headers (the CLI, curl, a script) is not a browser and
// passes.
func TestCSRF(t *testing.T) {
	c := &recControl{}
	h := guarded(c, GuardOptions{Hostname: "testbox"})
	const host = "127.0.0.1:8054"

	for name, hdr := range map[string][]string{
		"sec-fetch cross-site":     {"Sec-Fetch-Site", "cross-site"},
		"sec-fetch same-site":      {"Sec-Fetch-Site", "same-site"},
		"foreign origin":           {"Origin", "http://evil.example"},
		"foreign origin and port":  {"Origin", "http://127.0.0.1.evil.example:8054"},
		"null origin":              {"Origin", "null"},
		"origin on another host":   {"Origin", "http://localhost:8054"},
		"cross-site wins":          {"Sec-Fetch-Site", "cross-site", "Origin", "http://127.0.0.1:8054"},
		"foreign origin, no fetch": {"Origin", "https://evil.example", "Sec-Fetch-Site", ""},
	} {
		rec := do(h, http.MethodPost, "/api/v1/set", host, setBody, hdr...)
		if rec.Code != http.StatusForbidden || errCode(rec) != "origin_rejected" {
			t.Errorf("%s: POST /api/v1/set = %d %s, want 403 origin_rejected", name, rec.Code, rec.Body)
		}
	}
	if n := c.changes.Load(); n != 0 {
		t.Fatalf("%d changes reached the Control through a cross-origin request, want 0", n)
	}

	for name, hdr := range map[string][]string{
		"no browser headers": nil,
		"same-origin fetch":  {"Sec-Fetch-Site", "same-origin"},
		"user-initiated":     {"Sec-Fetch-Site", "none"},
		"same origin header": {"Origin", "http://127.0.0.1:8054"},
		"same-origin both":   {"Sec-Fetch-Site", "same-origin", "Origin", "http://127.0.0.1:8054"},
	} {
		before := c.changes.Load()
		rec := do(h, http.MethodPost, "/api/v1/set", host, setBody, hdr...)
		if rec.Code != 200 || c.changes.Load() != before+1 {
			t.Errorf("%s: POST /api/v1/set = %d %s (changes %d -> %d), want 200 and one change", name, rec.Code, rec.Body, before, c.changes.Load())
		}
	}
}

// The guard covers the whole control table, not the routes someone remembered: every row that is not
// a read changes the bench and must refuse a cross-origin request, the LAN rows and the rows the
// web listener refuses alike; every GET row is a read (net/http lets safe methods through, so a
// GET row that changed something would be open to CSRF).
func TestCSRFCoversEveryMutatingRoute(t *testing.T) {
	reads := map[string]bool{"status": true, "fans": true, "history": true, "watch": true, "logs": true, "watch.ws": true}
	c := &recControl{}
	h := guarded(c, GuardOptions{Hostname: "testbox"})
	mutating := 0
	for _, op := range control.Operations {
		path := strings.ReplaceAll(op.Path, "{id}", "usr1")
		if op.Method == http.MethodGet {
			if !reads[op.Name] {
				t.Errorf("row %s is a GET that is not on the read-only list: a GET is never checked for CSRF, make it a POST or list it here after review", op.Name)
			}
			continue
		}
		mutating++
		rec := do(h, op.Method, path, "127.0.0.1:8054", `{}`, "Sec-Fetch-Site", "cross-site")
		if rec.Code != http.StatusForbidden || errCode(rec) != "origin_rejected" {
			t.Errorf("cross-site %s %s (%s) = %d %s, want 403 origin_rejected", op.Method, path, op.Name, rec.Code, rec.Body)
		}
		rec = do(h, op.Method, path, "127.0.0.1:8054", `{}`, "Origin", "http://evil.example")
		if rec.Code != http.StatusForbidden || errCode(rec) != "origin_rejected" {
			t.Errorf("foreign-origin %s %s (%s) = %d %s, want 403 origin_rejected", op.Method, path, op.Name, rec.Code, rec.Body)
		}
	}
	if mutating < 6 {
		t.Fatalf("only %d mutating rows in control.Operations, want at least 6 (down, sim.restart, fw.restart, press, button.down, button.up, set)", mutating)
	}
	if n := c.changes.Load(); n != 0 {
		t.Fatalf("%d changes reached the Control, want 0", n)
	}
}

const wantCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' ws://%s; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func TestSecurityHeaders(t *testing.T) {
	h := guarded(&recControl{}, GuardOptions{Hostname: "testbox"})
	for _, tc := range []struct{ host, wsHost string }{
		{"127.0.0.1:8054", "127.0.0.1:8054"},
		{"192.168.4.20:8054", "192.168.4.20:8054"},
		{"[::1]:8054", "[::1]:8054"},
		{"TestBox.local:8054", "testbox.local:8054"},
	} {
		for _, path := range []string{"/", "/api/v1/status", "/js/app.js"} {
			rec := do(h, http.MethodGet, path, tc.host, "")
			if rec.Code != 200 {
				t.Fatalf("GET %s with Host %q = %d", path, tc.host, rec.Code)
			}
			want := strings.Replace(wantCSP, "%s", tc.wsHost, 1)
			for k, v := range map[string]string{
				"Content-Security-Policy": want,
				"X-Content-Type-Options":  "nosniff",
				"X-Frame-Options":         "DENY",
				"Referrer-Policy":         "no-referrer",
			} {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("GET %s (Host %q): %s = %q, want %q", path, tc.host, k, got, v)
				}
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("GET %s: Cache-Control = %q, want no-cache", path, got)
			}
		}
	}

	// The refusals carry the headers too, and never put an unvalidated Host into the policy.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"host_rejected":   do(h, http.MethodGet, "/", "evil.example:8054\"; script-src *", ""),
		"origin_rejected": do(h, http.MethodPost, "/api/v1/set", "127.0.0.1:8054", setBody, "Sec-Fetch-Site", "cross-site"),
	} {
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = %d, want 403", name, rec.Code)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "evil.example") || strings.Contains(csp, "script-src *") {
			t.Errorf("%s: Content-Security-Policy = %q", name, csp)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing X-Frame-Options or X-Content-Type-Options", name)
		}
	}

	// A handler's own Cache-Control wins over the default.
	own := Guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
	}), GuardOptions{Hostname: "testbox"})
	if got := do(own, http.MethodGet, "/", "127.0.0.1:1", "").Header().Get("Cache-Control"); got != "max-age=60" {
		t.Errorf("handler Cache-Control = %q, want its own max-age=60", got)
	}
}

// The websocket upgrade of a foreign origin is refused (T-03-12), one without an Origin header (a
// script) or from the page's own origin is accepted, and the guard leaves the ResponseWriter
// hijackable.
func TestWebSocketOrigin(t *testing.T) {
	c := &watchControl{script: snapshotThenBlock}
	srv := httptest.NewServer(Guard(NewHandler(c, Options{}), GuardOptions{Hostname: "testbox"}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	dial := func(origin string) (*websocket.Conn, *http.Response, error) {
		ctx, cancel := within(5 * time.Second)
		defer cancel()
		opts := &websocket.DialOptions{}
		if origin != "" {
			opts.HTTPHeader = http.Header{"Origin": {origin}}
		}
		return websocket.Dial(ctx, wsURL(srv), opts)
	}
	for _, origin := range []string{"http://evil.example", "https://evil.example:8054", "http://127.0.0.1.evil.example", "null"} {
		conn, res, err := dial(origin)
		if err == nil {
			_ = conn.CloseNow()
			t.Errorf("upgrade with Origin %q succeeded, want it refused", origin)
			continue
		}
		if res == nil || res.StatusCode != http.StatusForbidden {
			t.Errorf("upgrade with Origin %q: response %v, want 403", origin, res)
		}
	}
	if n := c.calls.Load(); n != 0 {
		t.Fatalf("%d streams reached Watch through a refused origin, want 0", n)
	}
	for _, origin := range []string{"", "http://" + host} {
		conn, _, err := dial(origin)
		if err != nil {
			t.Errorf("upgrade with Origin %q: %v, want it accepted", origin, err)
			continue
		}
		ctx, cancel := within(5 * time.Second)
		if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), `"snapshot"`) {
			t.Errorf("Origin %q: first frame %q (%v), want the snapshot", origin, data, err)
		}
		cancel()
		_ = conn.CloseNow()
	}

	// A rebound page asks a foreign Host: the upgrade is refused before the origin is even looked at.
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, wsURL(srv), &websocket.DialOptions{Host: "evil.example"})
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("upgrade with Host evil.example succeeded, want it refused")
	}
	if res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("upgrade with Host evil.example: response %v, want 403", res)
	}
}

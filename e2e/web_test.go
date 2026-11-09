//go:build sil_e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/clock"
)

// webBase is the page origin of the web listener on the local machine.
func webBase(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// webDo sends one request to the web listener and returns the status, the headers and the body.
func webDo(t *testing.T, port int, method, path string) (int, http.Header, []byte) {
	t.Helper()
	return webDoBody(t, port, method, path, "")
}

func webDoBody(t *testing.T, port int, method, path, body string) (int, http.Header, []byte) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, webBase(port)+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return res.StatusCode, res.Header, raw
}

// webJSON decodes one JSON object.
func webJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	return m
}

// webDial opens /ws (query is the raw query string without "?", may be empty).
func webDial(t *testing.T, port int, query string) *websocket.Conn {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 10*time.Second)
	defer cancel()
	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	if query != "" {
		url += "?" + query
	}
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// webRead reads one text frame as a JSON object within budget.
func webRead(t *testing.T, c *websocket.Conn, budget time.Duration) (map[string]any, error) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, budget)
	defer cancel()
	typ, raw, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	if strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("frame ends in a newline: %q", raw)
	}
	return webJSON(t, raw), nil
}

// webReadUntil reads frames until match accepts one (returned) or the budget ends.
func webReadUntil(t *testing.T, c *websocket.Conn, budget time.Duration, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, budget)
	defer cancel()
	for {
		typ, raw, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("frame type = %v, want text", typ)
		}
		f := webJSON(t, raw)
		if match(f) {
			return f
		}
	}
}

// TestWebTracer proves the web front door end to end on a real Rev 3.3 instance: the sticky web
// port, the embedded page, the /ws stream whose frames are `sil watch` lines, and the firmware key
// kept off the network listener. The subtests share one instance and run in order.
func TestWebTracer(t *testing.T) {
	in := r33Up(t, "e2e-web")
	start := sysClock.Now()
	st := in.env.Status(t, in.name)
	port := int(jget(st, "web", "port").(float64))
	t.Logf("web port %d", port)

	t.Run("status_web", func(t *testing.T) {
		if got := jget(st, "web", "listen"); got != "0.0.0.0" {
			t.Fatalf("web.listen = %v, want 0.0.0.0", got)
		}
		if port < 8054 || port > 9053 {
			t.Fatalf("web.port = %d, want within 8054..9053", port)
		}
		if url, _ := jget(st, "web", "url").(string); !strings.HasSuffix(url, fmt.Sprintf(":%d/", port)) {
			t.Fatalf("web.url = %q, want it to end with :%d/", url, port)
		}
		raw, err := os.ReadFile(filepath.Join(in.env.State, "sil", in.name, "identity.json"))
		if err != nil {
			t.Fatalf("identity.json: %v", err)
		}
		id := webJSON(t, raw)
		if got, _ := id["web_port"].(float64); int(got) != port {
			t.Fatalf("identity.json web_port = %v, want %d", id["web_port"], port)
		}
		if got, _ := id["api_port"].(float64); int(got) != in.port {
			t.Fatalf("identity.json api_port = %v, want %d", id["api_port"], in.port)
		}
	})

	t.Run("page", func(t *testing.T) {
		code, hdr, body := webDo(t, port, http.MethodGet, "/")
		if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") {
			t.Fatalf("GET / = %d %q, want 200 text/html", code, hdr.Get("Content-Type"))
		}
		if !strings.Contains(string(body), "/js/app.js") {
			t.Fatalf("GET / does not reference /js/app.js:\n%s", body)
		}
		for _, p := range []string{"/js/app.js", "/js/store.js"} {
			code, hdr, _ := webDo(t, port, http.MethodGet, p)
			if ct := hdr.Get("Content-Type"); code != 200 || !strings.Contains(ct, "javascript") {
				t.Fatalf("GET %s = %d %q, want 200 with a javascript content type", p, code, ct)
			}
		}
		if code, _, _ := webDo(t, port, http.MethodPost, "/"); code != http.StatusMethodNotAllowed {
			t.Fatalf("POST / = %d, want 405", code)
		}
	})

	t.Run("status_redacted", func(t *testing.T) {
		code, _, raw := webDo(t, port, http.MethodGet, "/api/v1/status")
		if code != 200 {
			t.Fatalf("GET /api/v1/status = %d: %s", code, raw)
		}
		tcp := webJSON(t, raw)
		if got := jget(tcp, "api", "key"); got != "<redacted>" {
			t.Fatalf("TCP api.key = %v, want <redacted>", got)
		}
		sock := in.env.Status(t, in.name)
		key, _ := jget(sock, "api", "key").(string)
		if dec, err := base64.StdEncoding.DecodeString(key); err != nil || len(dec) != 32 {
			t.Fatalf("socket api.key does not decode to 32 bytes: %q (%v)", key, err)
		}
		for _, path := range [][]any{{"instance"}, {"api", "port"}, {"api", "mac"}, {"web"}} {
			if a, b := jget(tcp, path...), jget(sock, path...); !reflect.DeepEqual(a, b) {
				t.Fatalf("%v differs: tcp %v, socket %v", path, a, b)
			}
		}
		for i := range jgetSlice(sock, "ports") {
			if a, b := jget(tcp, "ports", i, "fan"), jget(sock, "ports", i, "fan"); !reflect.DeepEqual(a, b) {
				t.Fatalf("ports[%d].fan differs: tcp %v, socket %v", i, a, b)
			}
		}
	})

	t.Run("ws_snapshot", func(t *testing.T) {
		c := webDial(t, port, "")
		f, err := webRead(t, c, 5*time.Second)
		if err != nil {
			t.Fatalf("first frame: %v", err)
		}
		if f["k"] != "snapshot" {
			t.Fatalf("first frame k = %v, want snapshot: %v", f["k"], f)
		}
		if seq, ok := f["seq"].(float64); !ok || seq != float64(int64(seq)) {
			t.Fatalf("seq = %v, want an integer", f["seq"])
		}
		if f["instance"] != in.name {
			t.Fatalf("instance = %v, want %s", f["instance"], in.name)
		}
		if n := len(jgetSlice(f, "ports")); n != 4 {
			t.Fatalf("ports = %d, want 4", n)
		}
		if got := jget(f, "sim", "time_factor"); got != float64(1) {
			t.Fatalf("sim.time_factor = %v, want 1", got)
		}
		if _, ok := jget(f, "firmware", "bus_connected").(bool); !ok {
			t.Fatalf("firmware.bus_connected = %v, want a boolean", jget(f, "firmware", "bus_connected"))
		}
		if got := jget(f, "api", "key"); got != "<redacted>" {
			t.Fatalf("snapshot api.key = %v, want <redacted>", got)
		}
	})

	t.Run("ws_live", func(t *testing.T) {
		c := webDial(t, port, "")
		if f, err := webRead(t, c, 5*time.Second); err != nil || f["k"] != "snapshot" {
			t.Fatalf("first frame = %v (%v), want the snapshot", f, err)
		}
		if out, code := in.set(t, "temperature=23.5"); code != 0 {
			t.Fatalf("sil set temperature=23.5: exit %d: %v", code, out)
		}
		webReadUntil(t, c, 5*time.Second, "an env frame for temperature 23.5", func(f map[string]any) bool {
			return f["k"] == "env" && f["id"] == "temperature" && f["v"] == 23.5
		})
	})

	t.Run("ws_bad_kind", func(t *testing.T) {
		ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 10*time.Second)
		defer cancel()
		c, res, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws?kind=nope", port), nil)
		if err == nil {
			_ = c.CloseNow()
			t.Fatal("dial with kind=nope succeeded, want a refused upgrade")
		}
		if res == nil || res.StatusCode != http.StatusBadRequest {
			t.Fatalf("response = %v, want 400", res)
		}
		raw, _ := io.ReadAll(res.Body)
		if code := jget(webJSON(t, raw), "error", "code"); code != "invalid_argument" {
			t.Fatalf("error code = %v, want invalid_argument: %s", code, raw)
		}
	})

	t.Run("lifecycle_refused", func(t *testing.T) {
		before := in.env.Status(t, in.name)
		for _, op := range []struct{ path, cli string }{
			{"/api/v1/down", "sil down"},
			{"/api/v1/sim/restart", "sil sim restart"},
			{"/api/v1/fw/restart", "sil fw restart"},
		} {
			code, _, raw := webDo(t, port, http.MethodPost, op.path)
			if code != http.StatusForbidden {
				t.Fatalf("POST %s = %d, want 403: %s", op.path, code, raw)
			}
			body := webJSON(t, raw)
			if got := jget(body, "error", "code"); got != "operation_not_allowed" {
				t.Fatalf("POST %s error code = %v, want operation_not_allowed: %s", op.path, got, raw)
			}
			if msg, _ := jget(body, "error", "message").(string); !strings.Contains(msg, "`"+op.cli+"`") {
				t.Fatalf("POST %s message = %q, want it to name `%s`", op.path, msg, op.cli)
			}
		}
		after := in.env.Status(t, in.name)
		if after["phase"] != "ready" {
			t.Fatalf("phase after the refused operations = %v, want ready", after["phase"])
		}
		for _, path := range [][]any{{"firmware", "pid"}, {"sim", "engine_restarts"}, {"firmware", "restarts"}} {
			if a, b := jget(before, path...), jget(after, path...); a != b {
				t.Fatalf("%v changed from %v to %v: a refused operation must do nothing", path, a, b)
			}
		}
	})

	t.Run("browser", func(t *testing.T) {
		lctx, lcancel := clock.WithTimeout(context.Background(), sysClock, 60*time.Second)
		defer lcancel()
		b, err := cdptest.Launch(lctx, sysClock, cdptest.LaunchOptions{Offline: true, Width: 1280, Height: 720})
		if errors.Is(err, cdptest.ErrNoChrome) {
			t.Fatal("Chrome not found; install google-chrome or set SIL_CHROME")
		}
		if err != nil {
			t.Fatalf("launch Chrome: %v", err)
		}
		defer b.Close()
		ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 60*time.Second)
		defer cancel()
		page, err := b.NewPage(ctx)
		if err != nil {
			t.Fatalf("new page: %v", err)
		}
		defer page.Close()
		base := webBase(port) + "/"
		if err := page.Navigate(ctx, base); err != nil {
			t.Fatalf("navigate to %s: %v", base, err)
		}
		wait := func(what, pred string, budget time.Duration) {
			t.Helper()
			wctx, wcancel := clock.WithTimeout(context.Background(), sysClock, budget)
			defer wcancel()
			if err := page.WaitFor(wctx, pred); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		}
		wait("LINK LIVE", `document.getElementById('link').textContent === 'LIVE'`, 15*time.Second)
		wait("instance name", `document.querySelector('[data-src="instance"]').textContent === 'e2e-web'`, 5*time.Second)
		var panels int
		if err := page.Eval(ctx, `document.querySelectorAll('#ports .port').length`, &panels); err != nil || panels != 4 {
			t.Fatalf("port panels = %d (%v), want 4", panels, err)
		}
		if out, code := in.set(t, "humidity=50"); code != 0 {
			t.Fatalf("sil set humidity=50: exit %d: %v", code, out)
		}
		hctx, hcancel := clock.WithTimeout(context.Background(), sysClock, 5*time.Second)
		defer hcancel()
		for {
			var humidity float64
			if err := page.Eval(hctx, `(await import('/js/store.js')).getState().env.humidity`, &humidity); err != nil {
				t.Fatalf("read the store: %v", err)
			}
			if humidity == 50 {
				break
			}
			if err := sysClock.Sleep(hctx, 50*time.Millisecond); err != nil {
				t.Fatalf("the store never mirrored humidity=50 (last %v): %v", humidity, err)
			}
		}

		reqs := page.Requests()
		sawWS := false
		for _, r := range reqs {
			if !cdptest.SameOrigin(base, r.URL) {
				t.Errorf("request %s %s (%s) is not same-origin with %s", r.Method, r.URL, r.Kind, base)
			}
			if u, err := url.Parse(r.URL); r.Kind == "ws" && err == nil && u.Path == "/ws" {
				sawWS = true
			}
		}
		if !sawWS {
			t.Errorf("no WebSocket entry for /ws among the recorded requests: %v", reqs)
		}
	})

	t.Run("stopped_on_down", func(t *testing.T) {
		c := webDial(t, port, "")
		if f, err := webRead(t, c, 5*time.Second); err != nil || f["k"] != "snapshot" {
			t.Fatalf("first frame = %v (%v), want the snapshot", f, err)
		}
		if out, code, stderr := in.env.Sil(t, "down", in.name); code != 0 {
			t.Fatalf("sil down: exit %d: %v\n%s", code, out, stderr)
		}
		webReadUntil(t, c, 5*time.Second, "the stopped frame", func(f map[string]any) bool { return f["k"] == "stopped" })
		if f, err := webRead(t, c, 5*time.Second); err == nil {
			t.Fatalf("frame after stopped: %v, want the connection closed", f)
		}
	})
	t.Logf("TestWebTracer took %s after the instance was up", sysClock.Now()-start)
}

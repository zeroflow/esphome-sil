//go:build sil_e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
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
		if got := jget(st, "web", "listen"); got != "127.0.0.1" {
			t.Fatalf("web.listen = %v, want 127.0.0.1 (the harness narrows it)", got)
		}
		if want := in.env.webPorts[in.name]; port != want {
			t.Fatalf("web.port = %d, want the injected %d", port, want)
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

	// T-03-14: the web listener gives a connection 10 s to deliver its headers. A stalled client is
	// closed; a /ws stream that sent its headers in time is not.
	t.Run("ws_outlives_header_budget", func(t *testing.T) {
		stalled, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatal(err)
		}
		defer stalled.Close()
		if _, err := io.WriteString(stalled, "GET /api/v1/status HT"); err != nil {
			t.Fatal(err)
		}
		c := webDial(t, port, "")
		if f, err := webRead(t, c, 5*time.Second); err != nil || f["k"] != "snapshot" {
			t.Fatalf("first frame = %v (%v), want the snapshot", f, err)
		}
		sctx, scancel := clock.WithTimeout(context.Background(), sysClock, 20*time.Second)
		defer scancel()
		if err := sysClock.Sleep(sctx, 11*time.Second); err != nil {
			t.Fatal(err)
		}
		_ = stalled.SetReadDeadline(sysClock.Deadline(5 * time.Second))
		if _, err := io.Copy(io.Discard, stalled); err != nil {
			t.Fatalf("the stalled connection is still open 11 s after it connected: %v", err)
		}
		if out, code := in.set(t, "humidity=51.5"); code != 0 {
			t.Fatalf("sil set humidity=51.5: exit %d: %v", code, out)
		}
		webReadUntil(t, c, 5*time.Second, "an env frame for humidity 51.5 on a stream older than the header budget", func(f map[string]any) bool {
			return f["k"] == "env" && f["id"] == "humidity" && f["v"] == 51.5
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

// reachUp runs `sil up name args...` without any injected flag, registers a cleanup `sil down`, and
// returns the decoded result, the exit code and stderr, whatever the exit code is.
func reachUp(t *testing.T, env *Env, name string, args ...string) (map[string]any, int, string) {
	t.Helper()
	t.Cleanup(func() { _, _, _ = env.run(t, "down", name) })
	return env.Sil(t, append([]string{"up", name}, args...)...)
}

// reachDown runs `sil down name` and requires success.
func reachDown(t *testing.T, env *Env, name string) {
	t.Helper()
	if out, code, stderr := env.Sil(t, "down", name); code != 0 {
		t.Fatalf("sil down %s: exit %d: %v\n%s", name, code, out, stderr)
	}
}

// reachIdentity reads a field of the stored identity.json of an instance.
func reachIdentity(t *testing.T, env *Env, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(env.State, "sil", name, "identity.json"))
	if err != nil {
		t.Fatalf("identity.json of %s: %v", name, err)
	}
	return webJSON(t, raw)
}

// TestWebReach proves the reach controls of `sil up` (D-07, D-09) on real Rev 3.3 instances: the
// LAN default and the URL printed for it, --listen narrowing the listener, the sticky web port with
// its conflict rules, and --ui-dir serving the page from disk. The subtests run in order and share
// two instance names and one Env.
func TestWebReach(t *testing.T) {
	env := newEnv(t)
	apiPort := freePort(t)
	var webPort int
	for webPort = freePort(t); webPort == apiPort; webPort = freePort(t) {
	}

	t.Run("default_lan", func(t *testing.T) {
		up, exit, stderr := reachUp(t, env, "e2e-reach", "--api-port", fmt.Sprint(apiPort))
		if exit != 0 {
			t.Fatalf("sil up: exit %d: %v\n%s", exit, up, stderr)
		}
		if got := jget(up, "web", "listen"); got != "0.0.0.0" {
			t.Fatalf("up web.listen = %v, want 0.0.0.0", got)
		}
		port := int(jget(up, "web", "port").(float64))
		if port < 8054 || port > 9053 {
			t.Fatalf("up web.port = %d, want within 8054..9053", port)
		}
		wantHost := "127.0.0.1"
		if lan, ok := jget(up, "api", "lan_host").(string); ok && lan != "" {
			wantHost = lan
		}
		url, _ := jget(up, "web", "url").(string)
		if want := fmt.Sprintf("http://%s:%d/", wantHost, port); url != want {
			t.Fatalf("up web.url = %q, want %q", url, want)
		}
		if !strings.Contains(stderr, "web UI: "+url) || !strings.Contains(stderr, "unauthenticated") {
			t.Fatalf("stderr lacks the `web UI: <url>` line with the unauthenticated notice:\n%s", stderr)
		}
		if st := env.Status(t, "e2e-reach"); !reflect.DeepEqual(jget(st, "web"), jget(up, "web")) {
			t.Fatalf("status web = %v, up web = %v, want them equal", jget(st, "web"), jget(up, "web"))
		}
		reachDown(t, env, "e2e-reach")
	})

	t.Run("listen_loopback", func(t *testing.T) {
		up, exit, stderr := reachUp(t, env, "e2e-reach", "--listen", "127.0.0.1", "--web-port", fmt.Sprint(webPort))
		if exit != 0 {
			t.Fatalf("sil up: exit %d: %v\n%s", exit, up, stderr)
		}
		wantURL := fmt.Sprintf("http://127.0.0.1:%d/", webPort)
		if got := jget(up, "web", "url"); got != wantURL {
			t.Fatalf("up web.url = %v, want %s", got, wantURL)
		}
		if !strings.Contains(stderr, "web UI: "+wantURL) {
			t.Fatalf("stderr lacks the web UI line for %s:\n%s", wantURL, stderr)
		}
		if code, _, _ := webDo(t, webPort, http.MethodGet, "/"); code != 200 {
			t.Fatalf("GET / on 127.0.0.1:%d = %d, want 200", webPort, code)
		}
		if lan, ok := jget(up, "api", "lan_host").(string); ok && lan != "" {
			dctx, dcancel := clock.WithTimeout(context.Background(), sysClock, 3*time.Second)
			defer dcancel()
			var d net.Dialer
			conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(lan, fmt.Sprint(webPort)))
			if err == nil {
				_ = conn.Close()
				t.Fatalf("a TCP dial to %s:%d succeeded, want it refused with --listen 127.0.0.1", lan, webPort)
			}
		}
		if got := jget(env.Status(t, "e2e-reach"), "web", "listen"); got != "127.0.0.1" {
			t.Fatalf("status web.listen = %v, want 127.0.0.1", got)
		}
		reachDown(t, env, "e2e-reach")
	})

	t.Run("sticky", func(t *testing.T) {
		up, exit, stderr := reachUp(t, env, "e2e-reach", "--listen", "127.0.0.1")
		if exit != 0 {
			t.Fatalf("sil up: exit %d: %v\n%s", exit, up, stderr)
		}
		if got := jget(up, "web", "port"); got != float64(webPort) {
			t.Fatalf("web.port after down and up = %v, want the sticky %d", got, webPort)
		}
		if got := jget(reachIdentity(t, env, "e2e-reach"), "web_port"); got != float64(webPort) {
			t.Fatalf("identity web_port = %v, want %d", got, webPort)
		}
		reachDown(t, env, "e2e-reach")
	})

	t.Run("claimed", func(t *testing.T) {
		out := isoExpectError(t, env, 1, "port_claimed", "up", "e2e-reach2", "--listen", "127.0.0.1", "--web-port", fmt.Sprint(webPort))
		if got := jget(out, "error", "detail", "instance"); got != "e2e-reach" {
			t.Fatalf("detail.instance = %v, want e2e-reach", got)
		}
		if msg, _ := jget(out, "error", "message").(string); !strings.Contains(msg, "e2e-reach") {
			t.Fatalf("message does not name the owner: %q", msg)
		}
		if _, err := os.Stat(filepath.Join(env.State, "sil", "e2e-reach2", "identity.json")); err == nil {
			t.Fatal("a refused --web-port left an identity behind")
		}
		// The web port and the API port of one instance must differ.
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach2", "--api-port", fmt.Sprint(webPort+1), "--web-port", fmt.Sprint(webPort+1))
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach", "--web-port", fmt.Sprint(apiPort))
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach", "--api-port", fmt.Sprint(webPort))
	})

	t.Run("in_use", func(t *testing.T) {
		ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", webPort))
		if err != nil {
			t.Fatalf("hold port %d: %v", webPort, err)
		}
		defer ln.Close()
		out := isoExpectError(t, env, 2, "port_in_use", "up", "e2e-reach", "--listen", "127.0.0.1")
		if got := jget(out, "error", "detail", "port"); got != float64(webPort) {
			t.Fatalf("detail.port = %v, want %d", got, webPort)
		}
		if got := jget(out, "error", "detail", "listen"); got != "127.0.0.1" {
			t.Fatalf("detail.listen = %v, want 127.0.0.1", got)
		}
		if pid, _ := jget(out, "error", "detail", "holder", "pid").(float64); int(pid) != os.Getpid() {
			t.Fatalf("detail.holder = %v, want this test (pid %d)", jget(out, "error", "detail", "holder"), os.Getpid())
		}
		if got := jget(reachIdentity(t, env, "e2e-reach"), "web_port"); got != float64(webPort) {
			t.Fatalf("identity web_port = %v after a port_in_use, want the unmoved %d", got, webPort)
		}
		// An explicit busy --web-port is refused and not stored either.
		busy := freePort(t)
		ln2, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", busy))
		if err != nil {
			t.Fatalf("hold port %d: %v", busy, err)
		}
		defer ln2.Close()
		isoExpectError(t, env, 2, "port_in_use", "up", "e2e-reach", "--listen", "127.0.0.1", "--web-port", fmt.Sprint(busy))
		if got := jget(reachIdentity(t, env, "e2e-reach"), "web_port"); got != float64(webPort) {
			t.Fatalf("identity web_port = %v after a busy --web-port, want %d", got, webPort)
		}
	})

	t.Run("ui_dir", func(t *testing.T) {
		dir := t.TempDir()
		index := filepath.Join(dir, "index.html")
		write := func(marker string) {
			t.Helper()
			if err := os.WriteFile(index, []byte("<!doctype html><title>"+marker+"</title>"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("marker-one")
		up, exit, stderr := reachUp(t, env, "e2e-reach", "--listen", "127.0.0.1", "--ui-dir", dir)
		if exit != 0 {
			t.Fatalf("sil up --ui-dir: exit %d: %v\n%s", exit, up, stderr)
		}
		get := func() string {
			t.Helper()
			code, _, body := webDo(t, webPort, http.MethodGet, "/")
			if code != 200 {
				t.Fatalf("GET / = %d: %s", code, body)
			}
			return string(body)
		}
		if body := get(); !strings.Contains(body, "marker-one") {
			t.Fatalf("GET / = %q, want the marker of --ui-dir", body)
		}
		write("marker-two")
		if body := get(); !strings.Contains(body, "marker-two") {
			t.Fatalf("GET / after rewriting index.html = %q, want marker-two without a restart", body)
		}
		// The API is the same one.
		if code, _, _ := webDo(t, webPort, http.MethodGet, "/api/v1/status"); code != 200 {
			t.Fatalf("GET /api/v1/status with --ui-dir = %d, want 200", code)
		}
		reachDown(t, env, "e2e-reach")

		// A file is not a UI directory; nothing is built or locked for it.
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach", "--listen", "127.0.0.1", "--ui-dir", index)
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach", "--listen", "localhost")
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-reach", "--allow-host", "bad_host!")
	})

	// The guard (D-08 cheap protections): one instance, started with --allow-host, serves the four
	// subtests below in order; the last one stops it.
	var guardPort int
	var guardName = "e2e-reach"
	parent := t
	t.Run("guarded_up", func(t *testing.T) {
		// The instance outlives this subtest, so its cleanup `sil down` belongs to the parent.
		parent.Cleanup(func() { _, _, _ = env.run(parent, "down", guardName) })
		up, exit, stderr := env.Sil(t, "up", guardName, "--listen", "127.0.0.1", "--allow-host", "Bench.Test")
		if exit != 0 {
			t.Fatalf("sil up --allow-host: exit %d: %v\n%s", exit, up, stderr)
		}
		guardPort = int(jget(up, "web", "port").(float64))
		if guardPort != webPort {
			t.Fatalf("web.port = %d, want the sticky %d", guardPort, webPort)
		}
	})

	t.Run("host_guard", func(t *testing.T) {
		for _, host := range []string{"evil.example", fmt.Sprintf("evil.example:%d", guardPort), fmt.Sprintf("127.0.0.1.evil.example:%d", guardPort), fmt.Sprintf("bench.test.:%d", guardPort)} {
			code, hdr, raw := webReq(t, guardPort, http.MethodGet, "/api/v1/status", host, "")
			if code != http.StatusForbidden || jget(webJSON(t, raw), "error", "code") != "host_rejected" {
				t.Fatalf("GET /api/v1/status with Host %q = %d %s, want 403 host_rejected", host, code, raw)
			}
			if msg, _ := jget(webJSON(t, raw), "error", "message").(string); !strings.Contains(msg, "--allow-host") {
				t.Fatalf("Host %q: message %q does not name --allow-host", host, msg)
			}
			if hdr.Get("X-Frame-Options") != "DENY" {
				t.Fatalf("Host %q: the refusal lacks X-Frame-Options", host)
			}
		}
		// The page and the stream are refused for a foreign Host too, not only the API.
		for _, path := range []string{"/", "/js/app.js", "/ws"} {
			if code, _, _ := webReq(t, guardPort, http.MethodGet, path, "evil.example", ""); code != http.StatusForbidden {
				t.Fatalf("GET %s with Host evil.example = %d, want 403", path, code)
			}
		}
		for _, host := range []string{
			fmt.Sprintf("bench.test:%d", guardPort), fmt.Sprintf("Bench.Test:%d", guardPort),
			fmt.Sprintf("127.0.0.1:%d", guardPort), fmt.Sprintf("localhost:%d", guardPort), fmt.Sprintf("[::1]:%d", guardPort),
		} {
			if code, _, raw := webReq(t, guardPort, http.MethodGet, "/api/v1/status", host, ""); code != 200 {
				t.Fatalf("GET /api/v1/status with Host %q = %d %s, want 200", host, code, raw)
			}
		}
		if code, _, _ := webReq(t, guardPort, http.MethodGet, "/", "bench.test", ""); code != 200 {
			t.Fatalf("GET / with Host bench.test = %d, want 200", code)
		}
	})

	t.Run("csrf", func(t *testing.T) {
		temperature := func() float64 {
			t.Helper()
			v, _ := jget(env.Status(t, guardName), "env", "temperature").(float64)
			return v
		}
		before := temperature()
		target := before + 7
		body := fmt.Sprintf(`{"values":{"temperature":"%g"}}`, target)
		for name, hdr := range map[string][]string{
			"foreign origin":  {"Origin", "http://evil.example"},
			"cross-site":      {"Sec-Fetch-Site", "cross-site"},
			"null origin":     {"Origin", "null"},
			"sibling origin":  {"Origin", fmt.Sprintf("http://localhost:%d", guardPort)},
			"press":           {"Origin", "http://evil.example"},
			"foreign + fetch": {"Origin", "http://evil.example", "Sec-Fetch-Site", "cross-site"},
		} {
			path, payload := "/api/v1/set", body
			if name == "press" {
				path, payload = "/api/v1/buttons/usr1/press", `{}`
			}
			code, _, raw := webReq(t, guardPort, http.MethodPost, path, "", payload, hdr...)
			if code != http.StatusForbidden || jget(webJSON(t, raw), "error", "code") != "origin_rejected" {
				t.Fatalf("%s: POST %s = %d %s, want 403 origin_rejected", name, path, code, raw)
			}
		}
		if got := temperature(); got != before {
			t.Fatalf("temperature = %v after the refused requests, want unchanged %v", got, before)
		}
		// The page's own origin and a client that sends no browser headers are served.
		for name, hdr := range map[string][]string{
			"own origin":   {"Origin", fmt.Sprintf("http://127.0.0.1:%d", guardPort), "Sec-Fetch-Site", "same-origin"},
			"no origin":    nil,
			"curl-like ua": {"User-Agent", "curl/8"},
		} {
			code, _, raw := webReq(t, guardPort, http.MethodPost, "/api/v1/set", "", body, hdr...)
			if code != 200 {
				t.Fatalf("%s: POST /api/v1/set = %d %s, want 200", name, code, raw)
			}
			if got := temperature(); got != target {
				t.Fatalf("%s: temperature = %v, want %v", name, got, target)
			}
			target++
			body = fmt.Sprintf(`{"values":{"temperature":"%g"}}`, target)
		}
		// A foreign page cannot open the stream either.
		ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 10*time.Second)
		defer cancel()
		c, res, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", guardPort),
			&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.example"}}})
		if err == nil {
			_ = c.CloseNow()
			t.Fatal("a /ws upgrade with Origin http://evil.example succeeded, want it refused")
		}
		if res == nil || res.StatusCode != http.StatusForbidden {
			t.Fatalf("foreign-origin upgrade: response %v, want 403", res)
		}
		c2, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", guardPort),
			&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {fmt.Sprintf("http://127.0.0.1:%d", guardPort)}}})
		if err != nil {
			t.Fatalf("a /ws upgrade from the page's own origin: %v", err)
		}
		_ = c2.CloseNow()
	})

	t.Run("headers", func(t *testing.T) {
		for _, path := range []string{"/", "/js/app.js", "/api/v1/status"} {
			code, hdr, _ := webReq(t, guardPort, http.MethodGet, path, "", "")
			if code != 200 {
				t.Fatalf("GET %s = %d, want 200", path, code)
			}
			csp := hdr.Get("Content-Security-Policy")
			for _, want := range []string{
				"default-src 'none'", "script-src 'self'", "style-src 'self'", "img-src 'self' data:",
				fmt.Sprintf("connect-src 'self' ws://127.0.0.1:%d", guardPort),
				"base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'",
			} {
				if !strings.Contains(csp, want) {
					t.Fatalf("GET %s: Content-Security-Policy %q lacks %q", path, csp, want)
				}
			}
			for k, v := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"} {
				if got := hdr.Get(k); got != v {
					t.Fatalf("GET %s: %s = %q, want %q", path, k, got, v)
				}
			}
		}
	})

	// The control socket is the owner's: no Host rule, no CSRF rule, no headers, and the full key.
	t.Run("socket_untouched", func(t *testing.T) {
		sock := filepath.Join(env.Runtime, "sil", guardName, "api.sock")
		fi, err := os.Stat(sock)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("api.sock: %v %v, want mode 0600", fi, err)
		}
		st := env.Status(t, guardName)
		if key, _ := jget(st, "api", "key").(string); key == "" || key == "<redacted>" {
			t.Fatalf("socket status api.key = %q, want the full key", key)
		}
		watched := make(chan string, 1)
		go func() {
			so, _, _ := isoRun(env, "watch", "--count", "1", "-i", guardName)
			watched <- so
		}()
		ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 30*time.Second)
		defer cancel()
	waiting:
		for n := 0; ; n++ {
			select {
			case so := <-watched:
				lines := strings.Split(strings.TrimSpace(so), "\n")
				if len(lines) != 2 || webJSON(t, []byte(lines[0]))["k"] != "snapshot" {
					t.Fatalf("sil watch --count 1 printed %q, want a snapshot line and one event line", so)
				}
				break waiting
			default:
			}
			if out, code, stderr := env.Sil(t, "set", fmt.Sprintf("humidity=%d", 30+n%40), "-i", guardName); code != 0 {
				t.Fatalf("sil set: exit %d: %v\n%s", code, out, stderr)
			}
			if err := sysClock.Sleep(ctx, 300*time.Millisecond); err != nil {
				t.Fatalf("sil watch --count 1 never finished: %v", err)
			}
		}
		reachDown(t, env, guardName)
	})
}

// webReq sends one request to the web listener on 127.0.0.1:port with an explicit Host header
// (host "" keeps the address) and extra headers as key/value pairs.
func webReq(t *testing.T, port int, method, path, host, body string, hdr ...string) (int, http.Header, []byte) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, webBase(port)+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if host != "" {
		req.Host = host
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatalf("%s %s (Host %q): %v", method, path, host, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return res.StatusCode, res.Header, raw
}

// TestWebFanSelect (UI-02) proves the page's first write path on a real Rev 3.3 instance: choosing
// another fan in the select of port 2 sends exactly one POST /api/v1/set, the instance then reports
// that fan in `sil status`, and the select shows it.
func TestWebFanSelect(t *testing.T) {
	in := r33Up(t, "e2e-webfan")
	st := in.env.Status(t, in.name)
	port := int(jget(st, "web", "port").(float64))
	const want = "noctua-nf-a12x25-pwm"
	if cur := jget(st, "ports", 1, "fan"); cur == want {
		t.Fatalf("ports[1].fan is already %v: the test needs another fan to switch to", cur)
	}

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
	const sel = `document.querySelector('select[aria-label="Fan on port 2"]')`
	wait("LINK LIVE", `document.getElementById('link').textContent === 'LIVE'`, 15*time.Second)
	wait("the fan list", sel+`.options.length >= 2 && !`+sel+`.disabled`, 10*time.Second)

	var got string
	choose := fmt.Sprintf(`(() => { const s = %s; s.value = %q; s.dispatchEvent(new Event('change', {bubbles: true})); return s.value; })()`, sel, want)
	if err := page.Eval(ctx, choose, &got); err != nil || got != want {
		t.Fatalf("choosing %s: got %q (%v)", want, got, err)
	}
	wait("the select showing the new fan", sel+`.value === `+fmt.Sprintf("%q", want), 3*time.Second)

	deadline := sysClock.Now() + 3*time.Second
	for {
		if fan := jget(in.env.Status(t, in.name), "ports", 1, "fan"); fan == want {
			break
		}
		if sysClock.Now() > deadline {
			t.Fatalf("sil status never showed ports[1].fan = %s", want)
		}
		if err := sysClock.Sleep(ctx, 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}

	posts := 0
	for _, r := range page.Requests() {
		if r.Kind == "http" && r.Method == http.MethodPost {
			posts++
			if u, err := url.Parse(r.URL); err != nil || u.Path != "/api/v1/set" {
				t.Errorf("unexpected POST %s", r.URL)
			}
		}
	}
	if posts != 1 {
		t.Errorf("%d POST requests from the page, want exactly one POST /api/v1/set: %v", posts, page.Requests())
	}
}

// TestWebPlot (UI-02, UI-01, UI-06) proves the scope screen on a real Rev 3.3 instance: with port 1
// driven to 40 % by the firmware's native API, the page (offline) draws port 1's scope from the
// instance's history and live samples. Within 10 s the scope holds at least 50 non-null RPM points,
// its last duty point is 40, its RPM axis ends at the Arctic fan's max_rpm from `sil fans`, the page
// asked for one history per connection, and the console is empty.
func TestWebPlot(t *testing.T) {
	in := r33Up(t, "e2e-webplot")
	in.setFan(t, 1, 40)
	in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
	st := in.env.Status(t, in.name)
	port := int(jget(st, "web", "port").(float64))

	out, code, _ := in.env.Sil(t, "fans", "-i", in.name)
	if code != 0 {
		t.Fatalf("sil fans: exit %d: %v", code, out)
	}
	var maxRPM float64
	for _, f := range jgetSlice(out, "fans") {
		if jget(f, "id") == fansArctic {
			maxRPM, _ = jget(f, "max_rpm").(float64)
		}
	}
	if maxRPM == 0 {
		t.Fatalf("sil fans = %v has no max_rpm for %s", out, fansArctic)
	}

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
	if err := page.Eval(ctx, `(window.__plot = await import('/js/plot.js'), true)`, nil); err != nil {
		t.Fatalf("importing plot.js: %v", err)
	}
	wait("50 non-null RPM points on scope 1", `__plot.ringFor(1).rpm.filter((v) => v !== null).length >= 50`, 10*time.Second)

	var got struct {
		Points  int     `json:"points"`
		Duty    float64 `json:"duty"`
		ScaleLo float64 `json:"lo"`
		ScaleHi float64 `json:"hi"`
		DutyMax float64 `json:"dutyMax"`
		Drawn   int     `json:"drawn"`
	}
	const read = `(() => { const r = __plot.ringFor(1), u = __plot.scopeFor(1);
	  return {points: r.rpm.filter((v) => v !== null).length, duty: r.duty[r.duty.length - 1],
	    lo: u.scales.rpm.min, hi: u.scales.rpm.max, dutyMax: u.scales.duty.max, drawn: u.data[1].length}; })()`
	wait("the scope drawn and scaled", `(() => { const u = __plot.scopeFor(1); return u && u.scales.rpm && u.scales.rpm.max !== null && u.data[1].length >= 50; })()`, 10*time.Second)
	if err := page.Eval(ctx, read, &got); err != nil {
		t.Fatalf("reading the scope: %v", err)
	}
	t.Logf("scope 1: %+v", got)
	if got.Points < 50 {
		t.Errorf("scope 1 holds %d non-null RPM points, want at least 50", got.Points)
	}
	if got.Duty < 39.999 || got.Duty > 40.001 {
		t.Errorf("scope 1's last duty point = %v, want 40", got.Duty)
	}
	if got.ScaleLo != 0 || got.ScaleHi != maxRPM || got.DutyMax != 100 {
		t.Errorf("scope 1 scales: rpm %v..%v, duty max %v; want rpm 0..%v (the %s max_rpm) and duty max 100", got.ScaleLo, got.ScaleHi, got.DutyMax, maxRPM, fansArctic)
	}

	history, sockets := 0, 0
	for _, r := range page.Requests() {
		u, err := url.Parse(r.URL)
		if err != nil {
			continue
		}
		switch {
		case r.Kind == "http" && r.Method == http.MethodGet && u.Path == "/api/v1/history":
			history++
		case r.Kind == "ws" && u.Path == "/ws":
			sockets++
		}
	}
	if sockets == 0 || history != sockets {
		t.Errorf("%d GET /api/v1/history requests for %d /ws connections, want one history per connection: %v", history, sockets, page.Requests())
	}
	if errs := page.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// TestWebLoadReadouts (MODEL-11, D-13, D-16) proves the three load readouts and the tach trace on a
// real Rev 3.3 instance with the firmware in the loop. All four ports carry the load-test fixture
// fan (a pulse tach with datasheet values); Fan 1 runs at 60 % through the native API. Within 10 s
// the page's AIRFLOW readout of port 1 shows the status airflow_m3h with 1 decimal, POWER the status
// power_w with 2 decimals, TACH RPM a whole number, and scope 1 holds tach points. After
// `sil set port2.fan=curve-test` (no [datasheet], no [tach]) port 2 shows the em dash for AIRFLOW and
// POWER (status null) and a whole number for TACH RPM, the 1 s mean of the shaft (never null after
// the first second). The console stays empty.
func TestWebLoadReadouts(t *testing.T) {
	const name = "e2e-webload"
	env := newEnv(t)
	// Wins over the default v0.1.0 pin (later entries win): the schema 2 test fixtures.
	env.Extra = append(env.Extra, "SIL_FAN_DIR="+filepath.Join(repoRoot(t), "testdata", "fans-v2"))
	up := env.Up(t, name, "--fan", "load-test")
	apiPort, key := modelAPI(t, up)
	modelSetFan(t, env, apiPort, key, 1, 60)
	modelSetFan(t, env, apiPort, key, 2, 60)
	st := env.WaitStatus(t, name, 20*time.Second, func(st map[string]any) bool {
		rpm, _ := jget(st, "ports", 0, "rpm").(float64)
		tach, _ := jget(st, "ports", 0, "tach_rpm").(float64)
		return rpm > 1000 && tach > 0
	})
	port := int(jget(st, "web", "port").(float64))

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
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 90*time.Second)
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
	if err := page.Eval(ctx, `(window.__plot = await import('/js/plot.js'), true)`, nil); err != nil {
		t.Fatalf("importing plot.js: %v", err)
	}
	readout := func(field string, i int) string {
		t.Helper()
		var text string
		js := fmt.Sprintf(`document.querySelector('[data-src="ports.%d.%s"]').textContent`, i, field)
		if err := page.Eval(ctx, js, &text); err != nil {
			t.Fatalf("reading the %s readout of port %d: %v", field, i+1, err)
		}
		return text
	}
	wholeNumber := func(text string) bool {
		if text == "" {
			return false
		}
		for _, r := range text {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	// until polls cond every 200 ms of real time for budget; cond returns a failure note, "" for success.
	until := func(what string, budget time.Duration, cond func() string) {
		t.Helper()
		wctx, wcancel := clock.WithTimeout(context.Background(), sysClock, budget)
		defer wcancel()
		for {
			note := cond()
			if note == "" {
				return
			}
			if err := sysClock.Sleep(wctx, 200*time.Millisecond); err != nil {
				t.Fatalf("%s within %s: %s", what, budget, note)
			}
		}
	}

	// Port 1: the readouts are the status values, formatted. The status is re-read right before every
	// comparison and the loop allows the next 1 s value.
	until("port 1 AIRFLOW, POWER and TACH RPM readouts match the status", 10*time.Second, func() string {
		st := env.Status(t, name)
		flow, okF := jget(st, "ports", 0, "airflow_m3h").(float64)
		watt, okW := jget(st, "ports", 0, "power_w").(float64)
		if !okF || !okW {
			return fmt.Sprintf("status airflow_m3h/power_w of port 1 = %v/%v, want numbers", jget(st, "ports", 0, "airflow_m3h"), jget(st, "ports", 0, "power_w"))
		}
		gotF, gotW, gotT := readout("airflow_m3h", 0), readout("power_w", 0), readout("tach_rpm", 0)
		if gotF != fmt.Sprintf("%.1f", flow) || gotW != fmt.Sprintf("%.2f", watt) || !wholeNumber(gotT) {
			return fmt.Sprintf("readouts airflow %q power %q tach %q, status airflow %.4f power %.4f", gotF, gotW, gotT, flow, watt)
		}
		return ""
	})
	wait("5 non-null tach points on scope 1", `__plot.ringFor(1).tach.filter((v) => v !== null).length >= 5`, 10*time.Second)

	// Port 2 swapped to a fan with no datasheet and no [tach].
	out, code, stderr := env.Sil(t, "set", "port2.fan=curve-test", "-i", name)
	if code != 0 {
		t.Fatalf("sil set port2.fan=curve-test: exit %d: %v\nstderr: %s", code, out, stderr)
	}
	until("port 2 AIRFLOW and POWER readouts show the em dash", 5*time.Second, func() string {
		st := env.Status(t, name)
		if jget(st, "ports", 1, "fan") != "curve-test" {
			return fmt.Sprintf("status port 2 fan = %v", jget(st, "ports", 1, "fan"))
		}
		if jget(st, "ports", 1, "airflow_m3h") != nil || jget(st, "ports", 1, "power_w") != nil {
			return fmt.Sprintf("status airflow_m3h/power_w of port 2 = %v/%v, want null", jget(st, "ports", 1, "airflow_m3h"), jget(st, "ports", 1, "power_w"))
		}
		if f, w := readout("airflow_m3h", 1), readout("power_w", 1); f != "\u2014" || w != "\u2014" {
			return fmt.Sprintf("port 2 AIRFLOW %q POWER %q, want U+2014", f, w)
		}
		return ""
	})
	// The ideal tach reads the 1 s mean of the shaft: a number within 1 s of the swap, equal to rpm once steady.
	until("port 2 TACH RPM shows a whole number", 3*time.Second, func() string {
		if got := readout("tach_rpm", 1); !wholeNumber(got) {
			return fmt.Sprintf("port 2 TACH RPM readout %q", got)
		}
		return ""
	})
	until("status port 2 tach_rpm is the 1 s shaft mean", 10*time.Second, func() string {
		st := env.Status(t, name)
		tach, ok := jget(st, "ports", 1, "tach_rpm").(float64)
		rpm, _ := jget(st, "ports", 1, "rpm").(float64)
		if !ok {
			return "status port 2 tach_rpm is null"
		}
		if rpm < 1000 || math.Abs(tach-rpm) > 1 {
			return fmt.Sprintf("tach_rpm %.3f, rpm %.3f", tach, rpm)
		}
		return ""
	})
	if errs := page.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// TestWebHold (UI-04, D-14, D-15) proves the USR caps on a real Rev 3.3 instance: holding USR1 on
// the page for about 300 ms changes the firmware's duties exactly as `sil press usr1` does (TestPressTracer:
// 0.4/0/0/0 to 0.5/0.1/0.1/0.1). The page sends one POST .../usr1/down whose body is
// {"max_hold_ms": 10000} and one POST .../usr1/up; the cap lights from the instance's report
// (`sil status` buttons.usr1 true) and goes dark after the release.
func TestWebHold(t *testing.T) {
	in := r33Up(t, "e2e-webhold")
	in.setFan(t, 1, 40)
	in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
	st := in.env.Status(t, in.name)
	port := int(jget(st, "web", "port").(float64))

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
	const cap = `document.querySelector('button.usr[data-id="usr1"]')`
	wait("LINK LIVE", `document.getElementById('link').textContent === 'LIVE'`, 15*time.Second)
	wait("the cap enabled and dark", `!`+cap+`.disabled && `+cap+`.getAttribute('aria-pressed') === 'false'`, 10*time.Second)

	// The recorder keeps the bodies of the page's POST requests (the request log has none).
	const record = `(() => { window.__posts = []; const f = window.fetch.bind(window);
	  window.fetch = (u, init) => { if (init && init.method === 'POST') { __posts.push({url: String(u), body: init.body === undefined ? null : init.body}); } return f(u, init); };
	  return true; })()`
	if err := page.Eval(ctx, record, nil); err != nil {
		t.Fatalf("installing the recorder: %v", err)
	}

	x, y, err := page.Center(ctx, `button.usr[data-id="usr1"]`)
	if err != nil {
		t.Fatalf("centre of USR1: %v", err)
	}
	if err := page.Mouse(ctx, "mousePressed", x, y); err != nil {
		t.Fatalf("mouse press: %v", err)
	}
	in.env.WaitStatus(t, in.name, 5*time.Second, func(st map[string]any) bool {
		level, _ := r33Buttons(st, "usr1")
		return level
	})
	wait("the cap lit from the instance's report", cap+`.getAttribute('aria-pressed') === 'true'`, 5*time.Second)
	if err := sysClock.Sleep(ctx, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := page.Mouse(ctx, "mouseReleased", x, y); err != nil {
		t.Fatalf("mouse release: %v", err)
	}

	st = in.waitDuties(t, [4]float64{0.5, 0.1, 0.1, 0.1})
	if level, ok := r33Buttons(st, "usr1"); !ok || level {
		t.Errorf("buttons.usr1 = %v (present %v) after the release, want false", level, ok)
	}
	wait("the cap dark again", cap+`.getAttribute('aria-pressed') === 'false'`, 5*time.Second)

	var posts []struct {
		URL  string  `json:"url"`
		Body *string `json:"body"`
	}
	if err := page.Eval(ctx, `__posts`, &posts); err != nil {
		t.Fatalf("reading the recorder: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("the page made %d POST requests, want exactly a down and an up: %+v", len(posts), posts)
	}
	if posts[0].URL != "/api/v1/buttons/usr1/down" || posts[0].Body == nil {
		t.Fatalf("first POST = %+v, want /api/v1/buttons/usr1/down with a body", posts[0])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(*posts[0].Body), &body); err != nil || body["max_hold_ms"] != float64(10000) || len(body) != 1 {
		t.Errorf("down body = %s (%v), want only max_hold_ms 10000", *posts[0].Body, err)
	}
	if posts[1].URL != "/api/v1/buttons/usr1/up" || posts[1].Body != nil {
		t.Errorf("second POST = %+v, want /api/v1/buttons/usr1/up without a body", posts[1])
	}
	for _, r := range page.Requests() {
		if r.Kind == "http" && r.Method == http.MethodPost {
			if u, err := url.Parse(r.URL); err != nil || !strings.HasPrefix(u.Path, "/api/v1/buttons/usr1/") {
				t.Errorf("unexpected POST %s", r.URL)
			}
		}
	}
	if errs := page.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// accLookup walks a dotted path through decoded JSON (object keys, array indexes as numbers) and
// reports whether every step exists.
func accLookup(v any, path string) (any, bool) {
	for _, key := range strings.Split(path, ".") {
		switch c := v.(type) {
		case map[string]any:
			next, ok := c[key]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			v = c[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// accDiff lists the paths where a and b differ. A path in skip is not compared; a path in near is
// compared as numbers within the given tolerance (the paths use the form `ports.0.rpm`).
func accDiff(path string, a, b any, skip map[string]bool, near map[string]float64) []string {
	if skip[path] {
		return nil
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{path}
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		var out []string
		for k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			out = append(out, accDiff(child, av[k], bv[k], skip, near)...)
		}
		sort.Strings(out)
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return []string{path}
		}
		var out []string
		for i := range av {
			out = append(out, accDiff(fmt.Sprintf("%s.%d", path, i), av[i], bv[i], skip, near)...)
		}
		return out
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return []string{path}
		}
		tol := 0.0
		for pat, tolerance := range near {
			if accPathMatch(pat, path) {
				tol = tolerance
			}
		}
		if math.Abs(av-bv) > tol {
			return []string{path}
		}
		return nil
	}
	if fmt.Sprintf("%T:%v", a, a) != fmt.Sprintf("%T:%v", b, b) {
		return []string{path}
	}
	return nil
}

// accPathMatch reports whether path matches pattern, where a `*` segment matches any one segment.
func accPathMatch(pattern, path string) bool {
	ps, xs := strings.Split(pattern, "."), strings.Split(path, ".")
	if len(ps) != len(xs) {
		return false
	}
	for i := range ps {
		if ps[i] != "*" && ps[i] != xs[i] {
			return false
		}
	}
	return true
}

// TestWebAcceptance (UI-01..UI-06, Phase 3 success criteria 1-5) runs the whole page, offline, against
// the real Rev 3.3 firmware: the page shows what `sil status` reports, driving the page does to the
// firmware and to an API client (the Home Assistant proxy) what the CLI does, the page store equals
// the status JSON, and the open page survives a simulator restart and a `sil down` + `sil up`
// without a reload (D-10). The subtests share one instance and one page and run in order.
func TestWebAcceptance(t *testing.T) {
	const name = "e2e-webacc"
	const noctua = "noctua-nf-a12x25-pwm"
	in := r33Up(t, name)
	start := sysClock.Now()
	st0 := in.env.Status(t, name)
	port := int(jget(st0, "web", "port").(float64))

	lctx, lcancel := clock.WithTimeout(context.Background(), sysClock, 10*time.Minute)
	defer lcancel()
	b, err := cdptest.Launch(lctx, sysClock, cdptest.LaunchOptions{Offline: true, Width: 1280, Height: 720})
	if errors.Is(err, cdptest.ErrNoChrome) {
		t.Fatal("Chrome not found; install google-chrome or set SIL_CHROME")
	}
	if err != nil {
		t.Fatalf("launch Chrome: %v", err)
	}
	defer b.Close()
	nctx, ncancel := clock.WithTimeout(context.Background(), sysClock, 60*time.Second)
	defer ncancel()
	page, err := b.NewPage(nctx)
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()
	base := webBase(port) + "/"

	// ctx bounds one call of a subtest; wait polls a page predicate.
	ctx := func(d time.Duration) context.Context {
		c, cancel := clock.WithTimeout(context.Background(), sysClock, d)
		t.Cleanup(cancel)
		return c
	}
	wait := func(t *testing.T, what, pred string, budget time.Duration) {
		t.Helper()
		if err := page.WaitFor(ctx(budget), pred); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	eval := func(t *testing.T, js string, out any) {
		t.Helper()
		if err := page.Eval(ctx(20*time.Second), js, out); err != nil {
			t.Fatalf("page eval %.80q: %v", js, err)
		}
	}
	const linkIs = `document.getElementById('link').textContent === `
	historyRequests := func() (history, sockets int) {
		for _, r := range page.Requests() {
			u, err := url.Parse(r.URL)
			if err != nil {
				continue
			}
			switch {
			case r.Kind == "http" && r.Method == http.MethodGet && u.Path == "/api/v1/history":
				history++
			case r.Kind == "ws" && u.Path == "/ws":
				sockets++
			}
		}
		return history, sockets
	}
	// hold presses the cap of a USR button with the mouse for d and releases it.
	hold := func(t *testing.T, id string, d time.Duration) {
		t.Helper()
		sel := `button.usr[data-id="` + id + `"]`
		wait(t, "the "+id+" cap enabled and dark", `(() => { const c = document.querySelector('`+sel+`'); return !c.disabled && c.getAttribute('aria-pressed') === 'false'; })()`, 10*time.Second)
		x, y, err := page.Center(ctx(10*time.Second), sel)
		if err != nil {
			t.Fatalf("centre of %s: %v", id, err)
		}
		if err := page.Mouse(ctx(10*time.Second), "mousePressed", x, y); err != nil {
			t.Fatalf("mouse press on %s: %v", id, err)
		}
		in.env.WaitStatus(t, name, 5*time.Second, func(st map[string]any) bool {
			level, _ := r33Buttons(st, id)
			return level
		})
		if err := sysClock.Sleep(ctx(d+5*time.Second), d); err != nil {
			t.Fatal(err)
		}
		if err := page.Mouse(ctx(10*time.Second), "mouseReleased", x, y); err != nil {
			t.Fatalf("mouse release on %s: %v", id, err)
		}
	}

	if err := page.Navigate(ctx(60*time.Second), base); err != nil {
		t.Fatalf("navigate to %s: %v", base, err)
	}

	t.Run("loads_offline", func(t *testing.T) {
		wait(t, "LINK LIVE", linkIs+`'LIVE'`, 15*time.Second)
		wait(t, "the instance name", `document.querySelector('[data-src="instance"]').textContent === '`+name+`'`, 5*time.Second)
		var n struct{ Panels, Lenses int }
		eval(t, `({Panels: document.querySelectorAll('#ports .port').length, Lenses: document.querySelectorAll('#leds .lens').length})`, &n)
		if n.Panels != 4 || n.Lenses != 5 {
			t.Errorf("%d port panels and %d LED lenses, want 4 and 5", n.Panels, n.Lenses)
		}
		if errs := page.ConsoleErrors(); len(errs) != 0 {
			t.Errorf("console errors: %v", errs)
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
		if len(reqs) < 5 {
			t.Errorf("only %d requests recorded: the recorder is not seeing the page: %v", len(reqs), reqs)
		}
	})

	t.Run("leds_match", func(t *testing.T) {
		if err := sysClock.Sleep(ctx(10*time.Second), 3*time.Second); err != nil {
			t.Fatal(err)
		}
		st := in.env.Status(t, name)
		strip := jgetSlice(st, "leds", "strip")
		if len(strip) != 5 {
			t.Fatalf("sil status leds.strip = %v, want 5 entries", strip)
		}
		var hexes []string
		eval(t, `[...document.querySelectorAll('#leds .led .led-hex')].map((e) => e.textContent)`, &hexes)
		if len(hexes) != 5 {
			t.Fatalf("%d LED hex texts on the page, want 5: %v", len(hexes), hexes)
		}
		for i := range hexes {
			if hexes[i] != strip[i] {
				t.Errorf("LED %d: the page shows %q, sil status leds.strip[%d] = %v", i, hexes[i], i, strip[i])
			}
		}
		t.Logf("leds on the page %v, in sil status %v", hexes, strip)
	})

	t.Run("usr1_like_cli", func(t *testing.T) {
		in.setFan(t, 1, 40)
		in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
		hold(t, "usr1", 300*time.Millisecond)
		st := in.waitDuties(t, [4]float64{0.5, 0.1, 0.1, 0.1})
		if level, ok := r33Buttons(st, "usr1"); !ok || level {
			t.Errorf("buttons.usr1 = %v (present %v) after the release, want false", level, ok)
		}
		wait(t, "the cap dark again", `document.querySelector('button.usr[data-id="usr1"]').getAttribute('aria-pressed') === 'false'`, 5*time.Second)
	})

	t.Run("usr2_long_like_cli", func(t *testing.T) {
		hold(t, "usr2", 1500*time.Millisecond)
		in.waitDuties(t, [4]float64{1, 1, 1, 1})
		wait(t, "the cap dark again", `document.querySelector('button.usr[data-id="usr2"]').getAttribute('aria-pressed') === 'false'`, 5*time.Second)
	})

	t.Run("env_reaches_firmware", func(t *testing.T) {
		st := in.env.Status(t, name)
		humidity, ok := r33Env(st, "humidity")
		if !ok {
			t.Fatalf("sil status has no env.humidity: %v", st["env"])
		}
		var focused bool
		eval(t, `(() => { const e = document.getElementById('temperature'); e.focus(); e.select(); return document.activeElement === e; })()`, &focused)
		if !focused {
			t.Fatal("could not focus the TEMPERATURE field")
		}
		for _, key := range []string{"2", "6", "Enter"} {
			if err := page.Key(ctx(10*time.Second), "keyDown", key); err != nil {
				t.Fatalf("key %q down: %v", key, err)
			}
			if err := page.Key(ctx(10*time.Second), "keyUp", key); err != nil {
				t.Fatalf("key %q up: %v", key, err)
			}
		}
		in.env.WaitStatus(t, name, 5*time.Second, func(st map[string]any) bool {
			v, ok := r33Env(st, "temperature")
			return ok && math.Abs(v-26) < 1e-3
		})
		// The Home Assistant proxy: the firmware's own Temperature entity, over the native API.
		in.waitFwEnv(t, 45*time.Second, 26, humidity)
		wait(t, "the field reading 26 clean", `(() => { const e = document.getElementById('temperature'); return e.value === '26' && !e.classList.contains('dirty'); })()`, 5*time.Second)
	})

	t.Run("fan_swap", func(t *testing.T) {
		out, code, _ := in.env.Sil(t, "fans", "-i", name)
		if code != 0 {
			t.Fatalf("sil fans: exit %d: %v", code, out)
		}
		var maxRPM float64
		for _, f := range jgetSlice(out, "fans") {
			if jget(f, "id") == noctua {
				maxRPM, _ = jget(f, "max_rpm").(float64)
			}
		}
		if maxRPM == 0 {
			t.Fatalf("sil fans = %v has no max_rpm for %s", out, noctua)
		}
		if cur := jget(in.env.Status(t, name), "ports", 2, "fan"); cur == noctua {
			t.Fatalf("ports[2].fan is already %v: the test needs another fan to switch to", cur)
		}
		const sel = `document.querySelector('select[aria-label="Fan on port 3"]')`
		wait(t, "the fan list", sel+`.options.length >= 2 && !`+sel+`.disabled`, 10*time.Second)
		var got string
		choose := fmt.Sprintf(`(() => { const s = %s; s.value = %q; s.dispatchEvent(new Event('change', {bubbles: true})); return s.value; })()`, sel, noctua)
		eval(t, choose, &got)
		if got != noctua {
			t.Fatalf("choosing %s: the select reads %q", noctua, got)
		}
		in.env.WaitStatus(t, name, 5*time.Second, func(st map[string]any) bool { return jget(st, "ports", 2, "fan") == noctua })
		eval(t, `(window.__plot = await import('/js/plot.js'), true)`, nil)
		wait(t, "scope 3 re-scaled to the new fan", fmt.Sprintf(`(() => { const u = __plot.scopeFor(3); return u && u.scales.rpm && u.scales.rpm.min === 0 && u.scales.rpm.max === %v; })()`, maxRPM), 10*time.Second)
		wait(t, "the select showing the new fan", sel+`.value === `+fmt.Sprintf("%q", noctua), 3*time.Second)
	})

	t.Run("parity_real", func(t *testing.T) {
		if err := sysClock.Sleep(ctx(15*time.Second), 5*time.Second); err != nil {
			t.Fatal(err)
		}
		// The fan swapped in the previous subtest is still spinning up to its own top speed, and a
		// moving RPM cannot be compared to 1 rpm between two reads: wait until every RPM has settled
		// (two status reads 1 s apart within 0.5 rpm), as a page left quiet would be.
		settledAt := func() [4]float64 {
			var v [4]float64
			for i := range v {
				v[i], _ = jget(in.env.Status(t, name), "ports", i, "rpm").(float64)
			}
			return v
		}
		for prev, tries := settledAt(), 0; ; tries++ {
			if err := sysClock.Sleep(ctx(5*time.Second), time.Second); err != nil {
				t.Fatal(err)
			}
			cur := settledAt()
			settled := true
			for i := range cur {
				settled = settled && math.Abs(cur[i]-prev[i]) <= 0.5
			}
			if settled {
				break
			}
			if tries >= 30 {
				t.Fatalf("the RPMs never settled: %v then %v", prev, cur)
			}
			prev = cur
		}
		skip := map[string]bool{"sim.time_s": true, "sim.behind_real_time": true}
		// tach_rpm reaches the page as a float32 event value and status holds the float64 gate reading,
		// so the two differ in the sixth digit even at rest.
		near := map[string]float64{"ports.*.rpm": 1, "ports.*.tach_rpm": 1}
		var state map[string]any
		eval(t, `(await import('/js/store.js')).getState()`, &state)
		code, _, raw := webDo(t, port, http.MethodGet, "/api/v1/status")
		if code != 200 {
			t.Fatalf("GET /api/v1/status = %d: %s", code, raw)
		}
		status := webJSON(t, raw)
		if d := accDiff("", state, status, skip, near); len(d) != 0 {
			t.Errorf("the page store and GET /api/v1/status differ at %v\nstore:  %v\nstatus: %v", d, state, status)
		}
		for what, doc := range map[string]map[string]any{"store": state, "status": status} {
			if _, ok := jget(doc, "sim", "behind_real_time").(bool); !ok {
				t.Errorf("%s sim.behind_real_time = %v, want a boolean", what, jget(doc, "sim", "behind_real_time"))
			}
		}
		// The REAL TIME cell is the realtime format of the store's own value; the value itself is not
		// compared with status on this real clock (a loaded host may flip it between the two reads).
		var cell struct {
			Behind bool   `json:"behind"`
			Text   string `json:"text"`
		}
		for attempt := 0; attempt < 5; attempt++ {
			eval(t, `(async () => { const s = (await import('/js/store.js')).getState();
			  return {behind: s.sim.behind_real_time === true, text: document.querySelector('[data-src="sim.behind_real_time"]').textContent}; })()`, &cell)
			want := "IN SYNC"
			if cell.Behind {
				want = "BEHIND"
			}
			if cell.Text == want {
				break
			}
			if attempt == 4 {
				t.Errorf("REAL TIME cell shows %q for a store value of behind=%v, want %q", cell.Text, cell.Behind, want)
			}
			if err := sysClock.Sleep(ctx(5*time.Second), 200*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		// Every displayed value has its path in the JSON.
		var srcs []string
		eval(t, `[...document.querySelectorAll('[data-src]')].map((e) => e.dataset.src)`, &srcs)
		if len(srcs) < 20 {
			t.Errorf("only %d data-src elements on the page, want at least 20", len(srcs))
		}
		for _, src := range srcs {
			if _, ok := accLookup(status, src); !ok {
				t.Errorf("data-src %q is not a path of GET /api/v1/status", src)
			}
		}
		t.Logf("%d data-src elements checked, sim.behind_real_time in status = %v", len(srcs), jget(status, "sim", "behind_real_time"))
	})

	t.Run("sim_restart", func(t *testing.T) {
		before, _ := historyRequests()
		if out, code, stderr := in.env.Sil(t, "sim", "restart", name); code != 0 {
			t.Fatalf("sil sim restart: exit %d: %v\n%s", code, out, stderr)
		}
		deadline := sysClock.Now() + 15*time.Second
		for {
			after, _ := historyRequests()
			if after > before {
				break
			}
			if sysClock.Now() > deadline {
				t.Fatalf("no new history request within 15 s of the simulator restart (still %d)", before)
			}
			if err := sysClock.Sleep(ctx(20*time.Second), 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		wait(t, "LINK LIVE again", linkIs+`'LIVE'`, 15*time.Second)
	})

	t.Run("down_up", func(t *testing.T) {
		if out, code, stderr := in.env.Sil(t, "down", name); code != 0 {
			t.Fatalf("sil down: exit %d: %v\n%s", code, out, stderr)
		}
		wait(t, "LINK DISCONNECTED, RETRYING", linkIs+`'DISCONNECTED, RETRYING'`, 5*time.Second)
		up := in.env.Up(t, name, "--config", filepath.Join(repoRoot(t), "examples", "rev33.yaml"))
		if got, _ := jget(up, "web", "port").(float64); int(got) != port {
			t.Fatalf("sil up web.port = %v, want the same %d", jget(up, "web", "port"), port)
		}
		wait(t, "LINK LIVE again, without a reload", linkIs+`'LIVE'`, 90*time.Second)
		wait(t, "the same instance name", `document.querySelector('[data-src="instance"]').textContent === '`+name+`'`, 10*time.Second)
		for _, r := range page.Requests() {
			if !cdptest.SameOrigin(base, r.URL) {
				t.Errorf("request %s %s (%s) is not same-origin with %s", r.Method, r.URL, r.Kind, base)
			}
		}
	})
	t.Logf("TestWebAcceptance took %s after the instance was up", sysClock.Now()-start)
}

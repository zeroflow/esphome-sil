//go:build sil_ui

package uitest

import (
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/ui"
)

// pageOps names the operations of control.Operations the page uses, by row name. The page watches
// through its WebSocket; plans that make the page do more (press, set, fans) append the rows they
// use and extend the session of TestParityRequests.
var pageOps = []string{"watch.ws"}

// TestParityRequests (UI-06 check a): every request the page makes, HTTP and WebSocket alike, is
// same-origin and is either a file of the embedded page or a row of control.Operations that the
// LAN serves and the CLI offers; every operation in pageOps is requested at least once.
func TestParityRequests(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)

	// The session: one event, then a lost stream so the reconnect request is observed too.
	b.Emit(events.Event{K: "env", ID: "temperature", V: 24.5}, func(st *control.Status) { st.Env.Temperature = 24.5 })
	b.DropStreams()
	b.waitCalls(t, "Watch", 2, 10*time.Second)
	waitText(t, p, "#link", "LIVE", 10*time.Second)

	reqs := p.Requests()
	t.Logf("requests: %v", reqs)
	if len(reqs) < 2 {
		t.Fatalf("only %d requests recorded: %v", len(reqs), reqs)
	}
	seen := map[string]bool{}
	sawWS := false
	for _, r := range reqs {
		if !cdptest.SameOrigin(base, r.URL) {
			t.Errorf("request %s %s (%s) is not same-origin with %s", r.Method, r.URL, r.Kind, base)
			continue
		}
		u, err := url.Parse(r.URL)
		if err != nil {
			t.Errorf("request URL %q: %v", r.URL, err)
			continue
		}
		if r.Kind == "ws" {
			sawWS = true
		}
		if u.Path == "/ws" || strings.HasPrefix(u.Path, "/api/") {
			op, ok := control.MatchOperation(r.Method, u.Path)
			switch {
			case !ok:
				t.Errorf("%s %s is not in control.Operations", r.Method, u.Path)
			case !op.LAN || op.CLI == "":
				t.Errorf("%s %s is row %s (LAN %v, CLI %q): the page may only use LAN rows with a `sil` command", r.Method, u.Path, op.Name, op.LAN, op.CLI)
			default:
				seen[op.Name] = true
			}
			continue
		}
		name := strings.TrimPrefix(u.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(ui.FS(), name); err != nil {
			t.Errorf("%s %s is neither an operation nor a file of the embedded page: %v", r.Method, u.Path, err)
		}
	}
	if !sawWS {
		t.Errorf("no WebSocket entry was recorded: the recorder misses WebSocket connections (%v)", reqs)
	}
	for _, name := range pageOps {
		if !seen[name] {
			t.Errorf("page operation %s was never requested; seen %v", name, seen)
		}
	}
}

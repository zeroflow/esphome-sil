//go:build sil_ui

package uitest

import (
	"net/url"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// TestOfflineLoadAndLive: with the network blocked, the page loads, reaches LIVE, shows the
// readouts of the API and follows an event; every request it made, its WebSocket included, stays
// on its own origin, and a request to anywhere else fails (UI-01).
func TestOfflineLoadAndLive(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)

	waitText(t, p, "#link", "LIVE", 15*time.Second)
	for sel, want := range map[string]string{
		`[data-src="instance"]`:               "ui-bench",
		`[data-src="sim.time_s"]`:             "00:00:12.3",
		`[data-src="sim.time_factor"]`:        "x1",
		`[data-src="firmware.bus_connected"]`: "CONNECTED",
		`[data-src="ports.0.port"]`:           "P1",
		`[data-src="ports.0.fan"]`:            "arctic-p12-pwm-pst",
		`[data-src="ports.0.rpm"]`:            "1100",
		`[data-src="ports.0.duty"]`:           "40.0",
		`[data-src="ports.3.rpm"]`:            "1400",
	} {
		waitText(t, p, sel, want, 5*time.Second)
	}
	var panels int
	if err := p.Eval(within(t, 5*time.Second), `document.querySelectorAll('#ports .port').length`, &panels); err != nil || panels != 4 {
		t.Fatalf("port panels = %d (%v), want 4", panels, err)
	}
	var title string
	if err := p.Eval(within(t, 5*time.Second), `document.title`, &title); err != nil || title != "sil — ui-bench" {
		t.Fatalf("document.title = %q (%v), want %q", title, err, "sil — ui-bench")
	}

	b.Emit(events.Event{K: "rpm", Port: 1, V: 812.4}, func(st *control.Status) { st.Ports[0].RPM = 812.4 })
	waitText(t, p, `[data-src="ports.0.rpm"]`, "812", 5*time.Second)

	reqs := p.Requests()
	t.Logf("requests: %v", reqs)
	sawWS := false
	for _, r := range reqs {
		if r.Kind == "ws" {
			if u, err := url.Parse(r.URL); err == nil && u.Path == "/ws" {
				sawWS = true
			}
		}
		if !cdptest.SameOrigin(base, r.URL) {
			t.Errorf("request %s %s (%s) is not same-origin with %s", r.Method, r.URL, r.Kind, base)
		}
	}
	if !sawWS {
		t.Errorf("no WebSocket entry for /ws among the recorded requests: %v", reqs)
	}
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}

	// The outside world is unreachable in this browser: the probe must fail, not just be absent.
	var outcome string
	probe := `fetch('https://example.com/', {mode: 'no-cors'}).then(() => 'reached', () => 'failed')`
	if err := p.Eval(within(t, 15*time.Second), probe, &outcome); err != nil || outcome != "failed" {
		t.Fatalf("outside fetch = %q (%v), want it to fail with the network blocked", outcome, err)
	}
}

// TestConnectingShowsDashes: before the first snapshot every element that carries data-src shows no
// value, and LINK reads CONNECTING; after the snapshot they fill in. The rule is read over the whole
// document, so a data-src element a later plan adds is held to it too.
func TestConnectingShowsDashes(t *testing.T) {
	b := newBench(t)
	b.HoldSnapshot()
	base := newSite(t, b)
	p := openPage(t, base)

	b.waitCalls(t, "Watch", 1, 10*time.Second)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	const sweep = `(() => {
	  const els = [...document.querySelectorAll('[data-src]')];
	  const bad = els.filter((e) => {
	    if (e.dataset.fmt === 'pressed') { return e.hasAttribute('aria-pressed'); }
	    if (e.tagName === 'INPUT' || e.tagName === 'SELECT') { return e.value !== '--'; }
	    return e.textContent !== '--';
	  }).map((e) => e.dataset.src);
	  return {n: els.length, bad};
	})()`
	var got struct {
		N   int      `json:"n"`
		Bad []string `json:"bad"`
	}
	if err := p.Eval(within(t, 5*time.Second), sweep, &got); err != nil {
		t.Fatal(err)
	}
	if got.N == 0 {
		t.Fatal("no [data-src] element in the document")
	}
	if len(got.Bad) != 0 {
		t.Fatalf("elements showing a value before the first snapshot: %v", got.Bad)
	}
	if link := textOf(t, p, "#link"); link != "CONNECTING" {
		t.Fatalf("LINK = %q, want CONNECTING", link)
	}

	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	waitText(t, p, `[data-src="instance"]`, "ui-bench", 5*time.Second)
	waitText(t, p, `[data-src="ports.1.rpm"]`, "1200", 5*time.Second)
}

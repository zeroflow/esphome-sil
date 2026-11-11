//go:build sil_ui

package uitest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/ui"
)

// pageOps names the operations of control.Operations the page uses, by row name. The page watches
// through its WebSocket, lists the fan library and sets values; plans that make the page do more
// (press) append the rows they use and extend the session of TestParityRequests. The history is
// requested on every (re)connect, so the session sees it without a step of its own.
var pageOps = []string{"watch.ws", "fans", "set", "history", "button.down", "button.up"}

// TestParityRequests (UI-06 check a): every request the page makes, HTTP and WebSocket alike, is
// same-origin and is either a file of the embedded page or a row of control.Operations that the
// LAN serves and the CLI offers; every operation in pageOps is requested at least once.
func TestParityRequests(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)

	// The session: one fan change from the select (a set), one event, then a lost stream so the
	// reconnect request is observed too.
	if err := p.WaitFor(within(t, 10*time.Second), `document.querySelector('select[aria-label="Fan on port 1"]').options.length === 2`); err != nil {
		t.Fatalf("the fan list never arrived: %v", err)
	}
	chooseFan(t, p, 1, noctua)
	b.waitCalls(t, "Set", 1, 5*time.Second)
	// A hold on USR2: one down and one up.
	ux, uy := capCentre(t, p, "usr2")
	mouse(t, p, "mousePressed", ux, uy)
	b.waitCalls(t, "button.down", 1, 5*time.Second)
	waitPressed(t, p, "usr2", "true", 5*time.Second)
	mouse(t, p, "mouseReleased", ux, uy)
	b.waitCalls(t, "button.up", 1, 5*time.Second)
	waitPressed(t, p, "usr2", "false", 5*time.Second)
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

// uiOnly names an element whose text may change without data-src, and why. The list is the whole
// allow-list of TestParityDisplayedValues: a later plan adds an entry only by name, with a reason.
type uiOnly struct{ selector, reason string }

var uiOnlyText = []uiOnly{
	{"#link", "link state of the page's own WebSocket (UI-SPEC copy)"},
	{"#message", "API error text shown verbatim (D-17)"},
	{".scope .empty-head", "fixed UI-SPEC empty-state copy, not an API value"},
	{".scope .empty-body", "fixed UI-SPEC empty-state copy, not an API value"},
}

// fmtOracle is the UI-SPEC number format of kind, written independently of view.js, for the JSON
// value v of a status path (a float64, string or bool as encoding/json decodes them).
func fmtOracle(kind string, v any) string {
	num := func() float64 {
		f, _ := v.(float64)
		return f
	}
	if v == nil {
		// A JSON null: the readouts of tach, airflow and power show an em dash (D-16 as amended
		// 2026-11-11), every other readout the page's `--`.
		switch kind {
		case "tach", "flow", "watt":
			return "\u2014"
		}
		return "--"
	}
	switch kind {
	case "text", "hex":
		s, _ := v.(string)
		return s
	case "chip":
		return "P" + strconv.FormatFloat(num(), 'f', -1, 64)
	case "rpm":
		return strconv.FormatFloat(math.Round(num()), 'f', 0, 64)
	case "tach":
		return strconv.FormatFloat(math.Round(num()), 'f', 0, 64)
	case "flow":
		return strconv.FormatFloat(num(), 'f', 1, 64)
	case "watt":
		return strconv.FormatFloat(num(), 'f', 2, 64)
	case "pct":
		return strconv.FormatFloat(num()*100, 'f', 1, 64)
	case "simtime":
		tenths := int64(math.Round(num()*1e6)) / 100000
		whole := tenths / 10
		return fmt.Sprintf("%02d:%02d:%02d.%d", whole/3600, whole%3600/60, whole%60, tenths%10)
	case "factor":
		return "x" + strconv.FormatFloat(num(), 'f', -1, 64)
	case "firmware":
		if b, _ := v.(bool); b {
			return "CONNECTED"
		}
		return "DISCONNECTED"
	case "num":
		return strconv.FormatFloat(num(), 'f', -1, 64)
	case "select":
		s, _ := v.(string)
		return s
	case "pressed":
		b, _ := v.(bool)
		return strconv.FormatBool(b)
	case "realtime":
		if b, _ := v.(bool); b {
			return "BEHIND"
		}
		return "IN SYNC"
	}
	return "<no oracle for data-fmt " + kind + ">"
}

// lookup walks a dotted path through decoded JSON (map keys, array indexes as numbers) and reports
// whether every step exists.
func lookup(status any, path string) (any, bool) {
	cur := status
	for _, key := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[key]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(c) {
				return nil, false
			}
			cur = c[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// diffPaths lists the JSON paths where a and b differ.
func diffPaths(path string, a, b any) []string {
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
			out = append(out, diffPaths(path+"."+k, av[k], bv[k])...)
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
			out = append(out, diffPaths(fmt.Sprintf("%s.%d", path, i), av[i], bv[i])...)
		}
		return out
	}
	if fmt.Sprintf("%T:%v", a, a) != fmt.Sprintf("%T:%v", b, b) {
		return []string{path}
	}
	return nil
}

// getStatus decodes GET /api/v1/status of the site.
func getStatus(t *testing.T, base string) map[string]any {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	if res.StatusCode != 200 || json.Unmarshal(raw, &m) != nil {
		t.Fatalf("GET status = %d %s", res.StatusCode, raw)
	}
	return m
}

// sessionStep is one bench change of the scripted session.
type sessionStep struct {
	name   string
	ev     events.Event
	mutate func(*control.Status)
}

// sessionSteps are the bench events of the parity session: duty, rpm, state, fan, button, env,
// leds, firmware and the sim behind / in_sync pair. Each carries an explicit sim time (13.0 s, 13.1 s, ...) and sets the bench's
// sim.time_s to the same value, because the page takes sim.time_s from the t of every frame.
func sessionSteps() []sessionStep {
	steps := []sessionStep{
		{"duty", events.Event{K: "duty", Port: 2, V: 0.35}, func(st *control.Status) { st.Ports[1].Duty = 0.35 }},
		{"rpm", events.Event{K: "rpm", Port: 2, V: 640.5}, func(st *control.Status) { st.Ports[1].RPM = 640.5 }},
		{"state", events.Event{K: "state", Port: 2, V: "running"}, func(st *control.Status) { st.Ports[1].State = "running" }},
		{"fan", events.Event{K: "fan", Port: 3, V: "noctua-nf-a12x25-pwm"}, func(st *control.Status) { st.Ports[2].Fan = "noctua-nf-a12x25-pwm" }},
		{"button", events.Event{K: "button", ID: "usr1", V: true}, func(st *control.Status) { st.Buttons["usr1"] = true }},
		{"env", events.Event{K: "env", ID: "temperature", V: 24.5}, func(st *control.Status) { st.Env.Temperature = 24.5 }},
		{"leds", events.Event{K: "leds", Strip: []string{"#ff0000", "#00ff00", "#0000ff", "#ffff00", "#00ffff"}},
			func(st *control.Status) {
				st.LEDs.Strip = []string{"#ff0000", "#00ff00", "#0000ff", "#ffff00", "#00ffff"}
			}},
		{"firmware down", events.Event{K: "firmware", V: false}, func(st *control.Status) { st.Firmware.BusConnected = false }},
		{"firmware up", events.Event{K: "firmware", V: true}, func(st *control.Status) { st.Firmware.BusConnected = true }},
		{"behind", events.Event{K: "sim", V: "behind"}, func(st *control.Status) { st.Sim.BehindRealTime = true }},
		{"in sync", events.Event{K: "sim", V: "in_sync"}, func(st *control.Status) { st.Sim.BehindRealTime = false }},
	}
	for i := range steps {
		steps[i].ev.T = 13_000_000 + int64(i)*100_000
	}
	return steps
}

// liveBench opens the page on a Rev 3.3-shaped bench (four ports, three buttons, five LEDs) whose
// port 2 is stopped, and waits for LIVE.
func liveBench(t *testing.T) (*bench, *cdptest.Page, string) {
	t.Helper()
	b := newBench(t)
	b.Mutate(func(st *control.Status) { st.Ports[1].State = "stopped" })
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	return b, p, base
}

// play runs the steps: each is emitted, awaited in the page (the sim-time readout repaints together
// with every other readout, so it shows the frame has been folded in and painted) and then passed
// to after.
func play(t *testing.T, b *bench, p *cdptest.Page, after func(sessionStep)) {
	t.Helper()
	for _, s := range sessionSteps() {
		tUS := s.ev.T
		b.Emit(s.ev, func(st *control.Status) {
			st.Sim.TimeS = float64(tUS) / 1e6
			s.mutate(st)
		})
		waitText(t, p, `[data-src="sim.time_s"]`, fmtOracle("simtime", float64(tUS)/1e6), 5*time.Second)
		if after != nil {
			after(s)
		}
	}
}

// sweepData checks every [data-src] element against GET status: its path exists there and its text
// (value for form fields) is the UI-SPEC format of the value. It returns how many it checked.
func sweepData(t *testing.T, p *cdptest.Page, base, when string) int {
	t.Helper()
	status := getStatus(t, base)
	var els []struct{ Src, Fmt, Text string }
	const js = `[...document.querySelectorAll('[data-src]')].map((e) => ({Src: e.dataset.src, Fmt: e.dataset.fmt,
	  Text: e.dataset.fmt === 'pressed' ? (e.getAttribute('aria-pressed') || '') :
	    (e.tagName === 'INPUT' || e.tagName === 'SELECT' || e.tagName === 'TEXTAREA') ? e.value : e.textContent}))`
	if err := p.Eval(within(t, 10*time.Second), js, &els); err != nil {
		t.Fatalf("%s: reading the data-src elements: %v", when, err)
	}
	for _, e := range els {
		v, ok := lookup(status, e.Src)
		if !ok {
			t.Errorf("%s: data-src %q is not a path of GET status", when, e.Src)
			continue
		}
		if want := fmtOracle(e.Fmt, v); e.Text != want {
			t.Errorf("%s: data-src %q (data-fmt %q) shows %q, GET status holds %v which formats as %q", when, e.Src, e.Fmt, e.Text, v, want)
		}
	}
	return len(els)
}

// observerJS installs the text-change observer (selectors: the uiOnlyText selectors as a JSON
// array). Every body element's text is recorded at install. A mutation names the element whose text
// may have changed (the parent of a changed text node, the target of a childList change); it counts
// when that text differs from the last one recorded for the element, and it is allowed when the
// element carries data-src itself or matches an allow-list selector. An ancestor that merely
// contains such an element is not enough. window.__parity.flush() returns the offenders so far.
const observerJS = `(() => {
  const allow = %s;
  const last = new WeakMap();
  for (const el of document.body.querySelectorAll('*')) { last.set(el, el.textContent); }
  last.set(document.body, document.body.textContent);
  const bad = [];
  let changes = 0;
  const pathOf = (el) => {
    const parts = [];
    for (let e = el; e && e !== document.documentElement; e = e.parentElement) {
      let s = e.tagName.toLowerCase();
      if (e.id) { s += '#' + e.id; } else if (e.classList.length) { s += '.' + [...e.classList].join('.'); }
      parts.unshift(s);
    }
    return parts.join(' > ');
  };
  const touched = (el) => {
    if (!el || el.nodeType !== 1) { return; }
    const now = el.textContent;
    const was = last.has(el) ? last.get(el) : '';
    last.set(el, now);
    if (now === was) { return; }
    changes++;
    if (el.hasAttribute('data-src') || allow.some((s) => el.matches(s))) { return; }
    bad.push({path: pathOf(el), old: was, now});
  };
  const handle = (recs) => {
    for (const r of recs) { touched(r.type === 'characterData' ? r.target.parentElement : r.target); }
  };
  const obs = new MutationObserver(handle);
  obs.observe(document.body, {subtree: true, childList: true, characterData: true});
  window.__parity = { pathOf, flush() { handle(obs.takeRecords()); return {bad, changes}; } };
  return true;
})()`

type observed struct {
	Bad []struct {
		Path, Old, Now string
	} `json:"bad"`
	Changes int `json:"changes"`
}

// TestParityDisplayedValues (UI-06 check c): every data-src element shows the UI-SPEC format of the
// value at its path in GET status, after every step of the session; every element whose text
// changes during the session carries data-src itself or is on the named allow-list uiOnlyText.
func TestParityDisplayedValues(t *testing.T) {
	b, p, base := liveBench(t)

	sels := make([]string, len(uiOnlyText))
	for i, u := range uiOnlyText {
		if u.reason == "" {
			t.Fatalf("uiOnlyText %s has no reason", u.selector)
		}
		sels[i] = u.selector
	}
	if len(sels) != 4 || sels[0] != "#link" || sels[1] != "#message" || sels[2] != ".scope .empty-head" || sels[3] != ".scope .empty-body" {
		t.Fatalf("uiOnlyText selectors = %v, want exactly #link, #message and the two scope empty-state texts in this plan", sels)
	}
	raw, _ := json.Marshal(sels)
	if err := p.Eval(within(t, 10*time.Second), fmt.Sprintf(observerJS, raw), nil); err != nil {
		t.Fatalf("installing the observer: %v", err)
	}

	checked := sweepData(t, p, base, "before the session")
	play(t, b, p, func(s sessionStep) {
		if n := sweepData(t, p, base, "after "+s.name); n != checked {
			t.Errorf("after %s: %d data-src elements, want %d", s.name, n, checked)
		}
	})
	if checked < 20 {
		t.Errorf("checked %d data-src elements, want at least 20", checked)
	}
	t.Logf("checked %d data-src elements after each of %d steps", checked, len(sessionSteps()))

	var got observed
	if err := p.Eval(within(t, 10*time.Second), `window.__parity.flush()`, &got); err != nil {
		t.Fatal(err)
	}
	if got.Changes < 9 {
		t.Errorf("the observer counted %d text changes in a session of %d steps: it is not seeing the page", got.Changes, len(sessionSteps()))
	}
	for _, e := range got.Bad {
		t.Errorf("element %s changed its text from %q to %q without data-src: give it data-src, or add a named uiOnlyText entry with a reason", e.Path, e.Old, e.Now)
	}

	// Self-check: a header label has no data-src and is not allow-listed, so changing its text must
	// be reported, and only it.
	var label string
	if err := p.Eval(within(t, 10*time.Second), `(() => { const e = document.querySelector('#header .label'); e.textContent = 'TAMPERED'; return window.__parity.pathOf(e); })()`, &label); err != nil {
		t.Fatal(err)
	}
	if err := p.Eval(within(t, 10*time.Second), `window.__parity.flush()`, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Bad) != 1 || got.Bad[0].Path != label || got.Bad[0].Now != "TAMPERED" {
		t.Errorf("self-check: the rule reported %+v, want exactly %s", got.Bad, label)
	}
}

// TestParityPageState (UI-06 check d): after the session the page store holds exactly what GET
// status returns.
func TestParityPageState(t *testing.T) {
	b, p, base := liveBench(t)
	play(t, b, p, nil)

	var state map[string]any
	if err := p.Eval(within(t, 10*time.Second), `(await import('/js/store.js')).getState()`, &state); err != nil {
		t.Fatalf("reading the page store: %v", err)
	}
	if state == nil {
		t.Fatal("the page store is empty after the session")
	}
	status := getStatus(t, base)
	if d := diffPaths("", state, status); len(d) != 0 {
		t.Errorf("page state and GET status differ at %v\npage:   %v\nstatus: %v", d, state, status)
	}
	if env, _ := state["env"].(map[string]any); env["temperature"] != 24.5 {
		t.Errorf("the page store has env %v, want the session's temperature 24.5: events were not folded in", env)
	}
	// The comparison is able to fail: it names a difference when there is one.
	if d := diffPaths("", map[string]any{"a": []any{1.0, 2.0}}, map[string]any{"a": []any{1.0, 3.0}}); len(d) != 1 || d[0] != ".a.1" {
		t.Errorf("self-check: diffPaths = %v, want [.a.1]", d)
	}
}

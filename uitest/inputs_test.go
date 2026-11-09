//go:build sil_ui

package uitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
	"github.com/zeroflow/esphome-sil/internal/web"
	"github.com/zeroflow/esphome-sil/ui"
)

// fanSelect is what the page shows for the fan select of one port.
type fanSelect struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
	Error    bool   `json:"error"`
	Options  []struct {
		Value string `json:"value"`
		Text  string `json:"text"`
		Title string `json:"title"`
	} `json:"options"`
}

// fanSelects reads the four selects in panel order.
func fanSelects(t *testing.T, p *cdptest.Page) []fanSelect {
	t.Helper()
	const js = `[...document.querySelectorAll('#ports select.fan')].map((s) => ({
	  label: s.getAttribute('aria-label'), value: s.value, disabled: s.disabled, error: s.classList.contains('error'),
	  options: [...s.options].map((o) => ({value: o.value, text: o.text, title: o.title}))}))`
	var out []fanSelect
	if err := p.Eval(within(t, 10*time.Second), js, &out); err != nil {
		t.Fatalf("reading the fan selects: %v", err)
	}
	return out
}

// chooseFan picks the option id of the select of port n the way a user does: set the value, then the
// browser's change event.
func chooseFan(t *testing.T, p *cdptest.Page, n int, id string) {
	t.Helper()
	js := fmt.Sprintf(`(() => { const s = document.querySelector('select[aria-label="Fan on port %d"]'); s.value = %q;
	  s.dispatchEvent(new Event('change', {bubbles: true})); return s.value; })()`, n, id)
	var got string
	if err := p.Eval(within(t, 10*time.Second), js, &got); err != nil || got != id {
		t.Fatalf("choosing %s on port %d: got %q (%v)", id, n, got, err)
	}
}

// libraryNames maps each shipped fan id to its library name.
func libraryNames(t *testing.T) map[string]string {
	t.Helper()
	ids, err := fanlib.IDs()
	if err != nil || len(ids) < 2 {
		t.Fatalf("fan library ids %v (%v), want at least two", ids, err)
	}
	out := map[string]string{}
	for _, id := range ids {
		p, err := fanlib.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		out[id] = p.Name
	}
	return out
}

// setValues decodes the JSON values map the bench recorded for a Set call.
func setValues(t *testing.T, args string) map[string]string {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		t.Fatalf("Set args %q: %v", args, err)
	}
	out := map[string]string{}
	for k, v := range raw {
		out[k] = fmt.Sprintf("%T:%v", v, v)
	}
	return out
}

const (
	arctic = "arctic-p12-pwm-pst"
	noctua = "noctua-nf-a12x25-pwm"
)

// TestFanSelect (UI-02, D-16): each port has a native select with an option per library fan (text
// the fan's name, value and title its id), which shows the server's fan; choosing another sends one
// set at once, with no confirmation, and the select then shows what the server reports.
func TestFanSelect(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	if err := p.WaitFor(within(t, 10*time.Second), `[...document.querySelectorAll('#ports select.fan')].every((s) => !s.disabled && s.options.length === 2)`); err != nil {
		t.Fatalf("the selects never filled: %v (%+v)", err, fanSelects(t, p))
	}

	names := libraryNames(t)
	sels := fanSelects(t, p)
	if len(sels) != 4 {
		t.Fatalf("%d fan selects, want 4", len(sels))
	}
	for i, s := range sels {
		if want := fmt.Sprintf("Fan on port %d", i+1); s.Label != want {
			t.Errorf("select %d aria-label = %q, want %q", i, s.Label, want)
		}
		if s.Value != arctic {
			t.Errorf("select %d shows %q, want the server's fan %q", i, s.Value, arctic)
		}
		if len(s.Options) != len(names) {
			t.Errorf("select %d has %d options, want %d", i, len(s.Options), len(names))
		}
		for _, o := range s.Options {
			if names[o.Value] == "" || o.Text != names[o.Value] || o.Title != o.Value {
				t.Errorf("select %d option %+v: want text = library name %q and title = value", i, o, names[o.Value])
			}
		}
	}
	if got := len(b.callsOf("Fans")); got != 1 {
		t.Errorf("GET fans was called %d times, want once for the connection", got)
	}

	chooseFan(t, p, 2, noctua)
	calls := b.waitCalls(t, "Set", 1, 5*time.Second)
	if got := setValues(t, calls[0].Args); len(got) != 1 || got["port2.fan"] != "string:"+noctua {
		t.Errorf("Set values = %v, want only port2.fan = %s", got, noctua)
	}
	if err := p.WaitFor(within(t, 5*time.Second), fmt.Sprintf(`document.querySelector('select[aria-label="Fan on port 2"]').value === %q`, noctua)); err != nil {
		t.Fatalf("select 2 never showed the new fan: %v", err)
	}
	var stateFan string
	if err := p.Eval(within(t, 10*time.Second), `(await import('/js/store.js')).getState().ports[1].fan`, &stateFan); err != nil || stateFan != noctua {
		t.Errorf("the page store reports fan %q (%v), want %s", stateFan, err, noctua)
	}
	sels = fanSelects(t, p)
	if sels[0].Value != arctic || sels[2].Value != arctic || sels[1].Error {
		t.Errorf("after the swap: %+v, want only port 2 changed and no error outline", sels)
	}
	if msg := textOf(t, p, "#message"); msg != "" {
		t.Errorf("message line = %q after a successful change, want it empty", msg)
	}
	if n := len(b.callsOf("Set")); n != 1 {
		t.Errorf("%d Set calls, want exactly one", n)
	}
}

// TestFanSelectError (D-17, UIC-04): a refused fan change shows the API's message verbatim in the
// message line, puts a red outline on that select for about 5 s and puts the select back on the
// server's fan.
func TestFanSelectError(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	if err := p.WaitFor(within(t, 10*time.Second), `document.querySelector('select[aria-label="Fan on port 2"]').options.length === 2`); err != nil {
		t.Fatal(err)
	}

	const msg = `port2.fan=<b>"noctua"</b> & co is outside what this bench accepts`
	b.FailNext("set", apperr.Failed("value_out_of_range", msg, map[string]any{"key": "port2.fan"}))
	chooseFan(t, p, 2, noctua)
	waitText(t, p, "#message", msg, 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), fmt.Sprintf(`document.querySelector('select[aria-label="Fan on port 2"]').value === %q`, arctic)); err != nil {
		t.Fatalf("the select never went back to the server fan: %v", err)
	}
	if sels := fanSelects(t, p); !sels[1].Error || sels[0].Error {
		t.Errorf("error outline: %+v, want it on select 2 only", sels)
	}
	var rt []string
	if err := p.Eval(within(t, 5*time.Second), `[document.getElementById('message').getAttribute('role'), document.getElementById('message').title]`, &rt); err != nil || len(rt) != 2 {
		t.Fatalf("reading the message line: %v (%v)", rt, err)
	}
	if rt[0] != "alert" || rt[1] != msg {
		t.Errorf("message line role %q title %q, want alert and the whole message", rt[0], rt[1])
	}
	if n := len(b.callsOf("Set")); n != 1 {
		t.Errorf("%d Set calls, want one (no retry)", n)
	}
	if err := p.WaitFor(within(t, 9*time.Second), `!document.querySelector('select[aria-label="Fan on port 2"]').classList.contains('error')`); err != nil {
		t.Fatalf("the outline never went away: %v", err)
	}
}

// TestFansFetchError (UIC-04): when the fan list cannot be fetched, every select shows only its
// current fan, disabled, and the server's error is in the message line.
func TestFansFetchError(t *testing.T) {
	b := newBench(t)
	const msg = "cannot list the fan library: boom"
	b.FailNext("fans", apperr.Infra("internal", msg, nil))
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitText(t, p, "#message", msg, 10*time.Second)

	sels := fanSelects(t, p)
	if len(sels) != 4 {
		t.Fatalf("%d selects, want 4", len(sels))
	}
	for i, s := range sels {
		if !s.Disabled || len(s.Options) != 1 || s.Value != arctic || s.Options[0].Value != arctic {
			t.Errorf("select %d = %+v, want disabled with only the current fan %s", i, s, arctic)
		}
	}
}

// envField is what the page shows for one environment input.
type envField struct {
	Value    string `json:"value"`
	Dirty    bool   `json:"dirty"`
	Error    bool   `json:"error"`
	Disabled bool   `json:"disabled"`
	Focused  bool   `json:"focused"`
}

// envFieldOf reads the input with the given id (temperature or humidity).
func envFieldOf(t *testing.T, p *cdptest.Page, id string) envField {
	t.Helper()
	js := fmt.Sprintf(`(() => { const e = document.getElementById(%q); return {value: e.value, dirty: e.classList.contains('dirty'),
	  error: e.classList.contains('error'), disabled: e.disabled, focused: document.activeElement === e}; })()`, id)
	var f envField
	if err := p.Eval(within(t, 10*time.Second), js, &f); err != nil {
		t.Fatalf("reading #%s: %v", id, err)
	}
	return f
}

// waitField waits until the input id shows value and is not dirty.
func waitField(t *testing.T, p *cdptest.Page, id, value string, d time.Duration) {
	t.Helper()
	pred := fmt.Sprintf(`(() => { const e = document.getElementById(%q); return e.value === %q && !e.classList.contains('dirty'); })()`, id, value)
	if err := p.WaitFor(within(t, d), pred); err != nil {
		t.Fatalf("#%s never read %q clean (now %+v): %v", id, value, envFieldOf(t, p, id), err)
	}
}

// waitStore waits until the page store holds want (a number) at the dotted path.
func waitStore(t *testing.T, p *cdptest.Page, path string, want float64) {
	t.Helper()
	poll := fmt.Sprintf(`(async () => { const store = await import('/js/store.js');
	  for (let i = 0; i < 100; i++) {
	    const v = %q.split('.').reduce((o, k) => (o == null ? o : o[k]), store.getState());
	    if (v === %v) { return true; }
	    await new Promise((r) => setTimeout(r, 50));
	  }
	  return false; })()`, path, want)
	var ok bool
	if err := p.Eval(within(t, 10*time.Second), poll, &ok); err != nil || !ok {
		t.Fatalf("the page store never held %v at %s: %v", want, path, err)
	}
}

// typeInto focuses the input id, selects its text and types text key by key, like a user.
func typeInto(t *testing.T, p *cdptest.Page, id, text string) {
	t.Helper()
	var ok bool
	js := fmt.Sprintf(`(() => { const e = document.getElementById(%q); e.focus(); e.select(); return document.activeElement === e; })()`, id)
	if err := p.Eval(within(t, 10*time.Second), js, &ok); err != nil || !ok {
		t.Fatalf("focusing #%s: %v (focused %v)", id, err, ok)
	}
	for _, r := range text {
		pressKey(t, p, string(r))
	}
}

// pressKey sends a key down and up to the focused element.
func pressKey(t *testing.T, p *cdptest.Page, key string) {
	t.Helper()
	ctx := within(t, 10*time.Second)
	if err := p.Key(ctx, "keyDown", key); err != nil {
		t.Fatalf("key %q down: %v", key, err)
	}
	if err := p.Key(ctx, "keyUp", key); err != nil {
		t.Fatalf("key %q up: %v", key, err)
	}
}

// clickApply clicks APPLY VALUES with the mouse.
func clickApply(t *testing.T, p *cdptest.Page) {
	t.Helper()
	ctx := within(t, 10*time.Second)
	x, y, err := p.Center(ctx, "#apply")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Mouse(ctx, "mousePressed", x, y); err != nil {
		t.Fatal(err)
	}
	if err := p.Mouse(ctx, "mouseReleased", x, y); err != nil {
		t.Fatal(err)
	}
}

// liveEnv opens the page on a fresh bench and waits until LIVE with the fields reading 22 and 45.
func liveEnv(t *testing.T) (*bench, *cdptest.Page) {
	t.Helper()
	b := newBench(t)
	p := openPage(t, newSite(t, b))
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitField(t, p, "temperature", "22", 10*time.Second)
	waitField(t, p, "humidity", "45", 10*time.Second)
	return b, p
}

// TestEnvSet (UI-04, D-16): once live the fields read the server's 22 and 45; typing sends nothing,
// Enter commits only that field with the text as typed, APPLY VALUES commits all dirty fields in one
// set, and after the env event the fields read the server value without the dirty marker.
func TestEnvSet(t *testing.T) {
	b, p := liveEnv(t)
	for _, id := range []string{"temperature", "humidity"} {
		if f := envFieldOf(t, p, id); f.Disabled {
			t.Errorf("#%s is disabled while LIVE", id)
		}
	}

	typeInto(t, p, "temperature", "24.5")
	if f := envFieldOf(t, p, "temperature"); f.Value != "24.5" || !f.Dirty {
		t.Fatalf("after typing: %+v, want the typed text and the dirty marker", f)
	}
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if n := len(b.callsOf("Set")); n != 0 {
		t.Fatalf("%d Set calls while typing, want none until a commit", n)
	}
	pressKey(t, p, "Enter")
	calls := b.waitCalls(t, "Set", 1, 5*time.Second)
	if got := setValues(t, calls[0].Args); len(got) != 1 || got["temperature"] != "string:24.5" {
		t.Errorf("Set values = %v, want only temperature as the string 24.5", got)
	}
	waitField(t, p, "temperature", "24.5", 5*time.Second)
	if f := envFieldOf(t, p, "humidity"); f.Value != "45" || f.Dirty {
		t.Errorf("humidity = %+v, want it untouched by the other field's commit", f)
	}

	typeInto(t, p, "temperature", "25")
	typeInto(t, p, "humidity", "50")
	clickApply(t, p)
	calls = b.waitCalls(t, "Set", 2, 5*time.Second)
	if got := setValues(t, calls[1].Args); len(got) != 2 || got["temperature"] != "string:25" || got["humidity"] != "string:50" {
		t.Errorf("APPLY VALUES sent %v, want both fields as typed in one Set", got)
	}
	waitField(t, p, "temperature", "25", 5*time.Second)
	waitField(t, p, "humidity", "50", 5*time.Second)
	if n := len(b.callsOf("Set")); n != 2 {
		t.Errorf("%d Set calls, want two (one per commit)", n)
	}
}

// TestEnvError (D-17, UIC-23): a refused commit shows the API's message verbatim, the field named by
// error.detail.key keeps the typed text and gets the red outline, the other field does not, and the
// outline stays until that field is edited again.
func TestEnvError(t *testing.T) {
	b, p := liveEnv(t)
	const msg = "humidity=999 is outside 0..100"
	b.FailNext("set", apperr.Failed("value_out_of_range", msg, map[string]any{"key": "humidity"}))
	typeInto(t, p, "temperature", "30")
	typeInto(t, p, "humidity", "999")
	clickApply(t, p)
	waitText(t, p, "#message", msg, 5*time.Second)
	hum, temp := envFieldOf(t, p, "humidity"), envFieldOf(t, p, "temperature")
	if hum.Value != "999" || !hum.Error {
		t.Errorf("humidity = %+v, want the typed text and the red outline", hum)
	}
	if temp.Error || temp.Value != "30" {
		t.Errorf("temperature = %+v, want the typed text and no outline (the error names humidity)", temp)
	}
	if err := sysClock.Sleep(within(t, 10*time.Second), 5500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, p, "#message"); got != "" {
		t.Errorf("message line = %q after 5 s, want it cleared", got)
	}
	if hum = envFieldOf(t, p, "humidity"); !hum.Error {
		t.Errorf("humidity = %+v: the outline must stay until the field is edited again", hum)
	}
	typeInto(t, p, "humidity", "5")
	if hum = envFieldOf(t, p, "humidity"); hum.Error || hum.Value != "5" {
		t.Errorf("humidity after editing = %+v, want no outline", hum)
	}
}

// TestEnvDirty (D-16, UIC-24): while a field is focused or dirty a server value never changes its
// text; Escape puts the server value back and clears dirty and the outline; a field that is only
// focused takes the server value once it loses focus.
func TestEnvDirty(t *testing.T) {
	b, p := liveEnv(t)
	typeInto(t, p, "temperature", "1")
	b.Emit(events.Event{K: "env", ID: "temperature", V: 30.0}, func(st *control.Status) { st.Env.Temperature = 30 })
	waitStore(t, p, "env.temperature", 30)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if f := envFieldOf(t, p, "temperature"); f.Value != "1" || !f.Dirty {
		t.Fatalf("dirty temperature = %+v after a server value, want the user's text kept", f)
	}
	pressKey(t, p, "Escape")
	if f := envFieldOf(t, p, "temperature"); f.Value != "30" || f.Dirty || f.Error {
		t.Errorf("after Escape: %+v, want the server value 30, clean and without outline", f)
	}

	// Focused but not edited: protected while focused, current after blur.
	var ok bool
	if err := p.Eval(within(t, 5*time.Second), `(() => { const e = document.getElementById('humidity'); e.focus(); return document.activeElement === e; })()`, &ok); err != nil || !ok {
		t.Fatalf("focusing humidity: %v", err)
	}
	b.Emit(events.Event{K: "env", ID: "humidity", V: 55.0}, func(st *control.Status) { st.Env.Humidity = 55 })
	waitStore(t, p, "env.humidity", 55)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if f := envFieldOf(t, p, "humidity"); f.Value != "45" {
		t.Errorf("focused humidity = %+v, want 45 kept while focused", f)
	}
	if err := p.Eval(within(t, 5*time.Second), `document.getElementById('humidity').blur()`, nil); err != nil {
		t.Fatal(err)
	}
	waitField(t, p, "humidity", "55", 5*time.Second)
}

// flakySite is newSite with a hook that answers the next POST /api/v1/set with a 502 text/plain
// response instead of reaching the bench.
type flakySite struct {
	mu   sync.Mutex
	body *string
}

// fail arms the hook for the next POST /api/v1/set with the given response body.
func (f *flakySite) fail(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body = &body
}

func (f *flakySite) take() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.body == nil {
		return "", false
	}
	b := *f.body
	f.body = nil
	return b, true
}

func newFlakySite(t *testing.T, c control.Control) (string, *flakySite) {
	t.Helper()
	hook := &flakySite{}
	inner := web.Guard(web.NewHandler(c, web.Options{Static: ui.FS(), Clock: sysClock}), web.GuardOptions{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/set" {
			if body, ok := hook.take(); ok {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(body))
				return
			}
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hook
}

// TestErrorLine (D-17, UIC-02, UIC-29): the message line shows only what the server said. An empty
// non-JSON answer shows nothing but still outlines the committed field; a non-JSON answer shows its
// body; a second error replaces the first and restarts the 5 s timer; a long message is cut with an
// ellipsis and carried whole in title.
func TestErrorLine(t *testing.T) {
	b := newBench(t)
	base, hook := newFlakySite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitField(t, p, "temperature", "22", 10*time.Second)

	hook.fail("")
	typeInto(t, p, "temperature", "31")
	pressKey(t, p, "Enter")
	if err := p.WaitFor(within(t, 5*time.Second), `document.getElementById('temperature').classList.contains('error')`); err != nil {
		t.Fatalf("an empty 502 did not outline the field: %v", err)
	}
	if got := textOf(t, p, "#message"); got != "" {
		t.Fatalf("message line = %q after an empty 502 body, want nothing", got)
	}

	hook.fail("upstream down")
	typeInto(t, p, "temperature", "32")
	pressKey(t, p, "Enter")
	waitText(t, p, "#message", "upstream down", 5*time.Second)
	first := sysClock.Now()

	if err := sysClock.Sleep(within(t, 10*time.Second), 3*time.Second); err != nil {
		t.Fatal(err)
	}
	long := "temperature=" + strings.Repeat("9", 400) + " is outside 0..100"
	b.FailNext("set", apperr.Failed("value_out_of_range", long, map[string]any{"key": "temperature"}))
	typeInto(t, p, "temperature", "33")
	pressKey(t, p, "Enter")
	waitText(t, p, "#message", long, 5*time.Second)
	second := sysClock.Now()

	var cut struct {
		Title    string `json:"title"`
		Overflow string `json:"overflow"`
		Scroll   int    `json:"scroll"`
		Client   int    `json:"client"`
		Role     string `json:"role"`
	}
	const cutJS = `(() => { const e = document.getElementById('message'); const c = getComputedStyle(e);
	  return {title: e.title, overflow: c.textOverflow, scroll: e.scrollWidth, client: e.clientWidth, role: e.getAttribute('role')}; })()`
	if err := p.Eval(within(t, 5*time.Second), cutJS, &cut); err != nil {
		t.Fatal(err)
	}
	if cut.Title != long || cut.Overflow != "ellipsis" || cut.Scroll <= cut.Client || cut.Role != "alert" {
		t.Errorf("long message: %+v, want the whole text in title, an ellipsis, a cut line and role alert", cut)
	}

	// The first error's timer would have ended 2 s ago; the second restarted it.
	if d := first + 5*time.Second + 500*time.Millisecond - sysClock.Now(); d > 0 {
		if err := sysClock.Sleep(within(t, 10*time.Second), d); err != nil {
			t.Fatal(err)
		}
	}
	if got := textOf(t, p, "#message"); got != long {
		t.Fatalf("message line = %q shortly after the first error's 5 s, want the second error still shown", got)
	}
	waitText(t, p, "#message", "", 6*time.Second)
	if took := sysClock.Now() - second; took < 4500*time.Millisecond || took > 6500*time.Millisecond {
		t.Errorf("the line cleared %s after the last error, want about 5 s", took)
	}
	var rt []string
	if err := p.Eval(within(t, 5*time.Second), `[document.getElementById('message').getAttribute('role'), document.getElementById('message').title]`, &rt); err != nil || len(rt) != 2 {
		t.Fatalf("reading the message line: %v (%v)", rt, err)
	}
	if rt[0] != "status" || rt[1] != "" {
		t.Errorf("cleared line has role %q and title %q, want status and no title", rt[0], rt[1])
	}
}

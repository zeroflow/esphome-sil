//go:build sil_ui

package uitest

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
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

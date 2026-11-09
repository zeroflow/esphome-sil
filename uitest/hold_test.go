//go:build sil_ui

package uitest

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
)

// capSel is the selector of the USR cap of button id.
func capSel(id string) string { return `button.usr[data-id="` + id + `"]` }

// pressed reads aria-pressed of a cap: "true", "false" or "" when the attribute is absent.
func pressed(t *testing.T, p *cdptest.Page, id string) string {
	t.Helper()
	var s string
	js := `(() => { const e = document.querySelector('` + capSel(id) + `'); return e.hasAttribute('aria-pressed') ? e.getAttribute('aria-pressed') : ''; })()`
	if err := p.Eval(within(t, 10*time.Second), js, &s); err != nil {
		t.Fatalf("aria-pressed of %s: %v", id, err)
	}
	return s
}

// waitPressed waits until the cap shows aria-pressed want.
func waitPressed(t *testing.T, p *cdptest.Page, id, want string, d time.Duration) {
	t.Helper()
	pred := `document.querySelector('` + capSel(id) + `').getAttribute('aria-pressed') === '` + want + `'`
	if err := p.WaitFor(within(t, d), pred); err != nil {
		t.Fatalf("%s never showed aria-pressed=%s (shows %q): %v", id, want, pressed(t, p, id), err)
	}
}

// capCentre returns the viewport centre of a cap.
func capCentre(t *testing.T, p *cdptest.Page, id string) (float64, float64) {
	t.Helper()
	x, y, err := p.Center(within(t, 10*time.Second), capSel(id))
	if err != nil {
		t.Fatalf("centre of %s: %v", id, err)
	}
	return x, y
}

func mouse(t *testing.T, p *cdptest.Page, typ string, x, y float64) {
	t.Helper()
	if err := p.Mouse(within(t, 10*time.Second), typ, x, y); err != nil {
		t.Fatalf("mouse %s: %v", typ, err)
	}
}

// settle waits d on the system clock; used to show that something does not happen.
func settle(t *testing.T, d time.Duration) {
	t.Helper()
	if err := sysClock.Sleep(within(t, d+5*time.Second), d); err != nil {
		t.Fatal(err)
	}
}

// TestUSRHold (UI-04, D-14, D-15): pressing the primary mouse button on USR1 sends one button.down
// with the 10 s max hold, the cap lights only when the server reports the button held (never on the
// pointer), and releasing sends one button.up and the cap goes dark when the server says so.
func TestUSRHold(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `!document.querySelector('`+capSel("usr1")+`').disabled`); err != nil {
		t.Fatalf("the cap stays disabled on a live link: %v", err)
	}

	// The accessible name and the size of the caps (UI-SPEC component 5).
	var geo []struct {
		ID, Label, Text string
		W, H            float64
	}
	const geoJS = `[...document.querySelectorAll('button.usr')].map((e) => { const r = e.getBoundingClientRect();
	  return {ID: e.dataset.id, Label: e.getAttribute('aria-label'), Text: e.textContent, W: r.width, H: r.height}; })`
	if err := p.Eval(within(t, 10*time.Second), geoJS, &geo); err != nil || len(geo) != 3 {
		t.Fatalf("caps: %v (%v), want three", geo, err)
	}
	for i, g := range geo {
		id := "usr" + strconv.Itoa(i+1)
		if g.ID != id || g.Label != "Hold USR"+strconv.Itoa(i+1) || g.Text != strings.ToUpper(id) || g.H != 48 || g.W < 72 {
			t.Errorf("cap %d = %+v, want %s, label Hold USR%d, 48 px high, at least 72 px wide", i, g, id, i+1)
		}
	}

	x, y := capCentre(t, p, "usr1")
	b.HoldButtonEvents()
	mouse(t, p, "mousePressed", x, y)
	down := b.waitCalls(t, "button.down", 1, 5*time.Second)
	if down[0].Args != "usr1 10000" {
		t.Errorf("button.down args = %q, want %q (id and the 10 s max hold)", down[0].Args, "usr1 10000")
	}
	// The server has not reported the hold yet: the cap must not light on its own.
	settle(t, 300*time.Millisecond)
	if got := pressed(t, p, "usr1"); got != "false" {
		t.Errorf("cap aria-pressed = %q while the server has not reported the hold, want false (no local state)", got)
	}
	b.ReleaseButtonEvents()
	waitPressed(t, p, "usr1", "true", 5*time.Second)

	var look struct{ Fill, Text, Border string }
	const lookJS = `(() => { const cs = getComputedStyle(document.querySelector('` + `button.usr[data-id="usr1"]` + `'));
	  return {Fill: cs.backgroundColor, Text: cs.color, Border: cs.borderTopColor}; })()`
	if err := p.Eval(within(t, 10*time.Second), lookJS, &look); err != nil {
		t.Fatal(err)
	}
	if look.Fill != "rgb(217, 221, 226)" || look.Text != "rgb(21, 23, 26)" || look.Border != "rgb(141, 148, 156)" {
		t.Errorf("held cap look = %+v, want fill #d9dde2, text #15171a, border #8d949c", look)
	}

	mouse(t, p, "mouseReleased", x, y)
	up := b.waitCalls(t, "button.up", 1, 5*time.Second)
	if up[0].Args != "usr1" {
		t.Errorf("button.up args = %q, want usr1", up[0].Args)
	}
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	settle(t, 300*time.Millisecond)
	if n, m := len(b.callsOf("button.down")), len(b.callsOf("button.up")); n != 1 || m != 1 {
		t.Errorf("%d downs and %d ups, want one of each", n, m)
	}
	if msg := textOf(t, p, "#message"); msg != "" {
		t.Errorf("message line = %q after a plain hold, want it empty", msg)
	}
}

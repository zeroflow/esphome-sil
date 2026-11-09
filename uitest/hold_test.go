//go:build sil_ui

package uitest

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
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

// evalOn runs js in the page and fails the test on an error.
func evalOn(t *testing.T, p *cdptest.Page, js string, out any) {
	t.Helper()
	if err := p.Eval(within(t, 10*time.Second), js, out); err != nil {
		t.Fatalf("eval: %v", err)
	}
}

// holdFresh presses the cap of id with the mouse and waits for the button.down and the lit cap; it
// returns the cap centre. wantDowns is how many downs the bench should have seen in total.
func holdFresh(t *testing.T, b *bench, p *cdptest.Page, id string, wantDowns int) (float64, float64) {
	t.Helper()
	x, y := capCentre(t, p, id)
	mouse(t, p, "mousePressed", x, y)
	b.waitCalls(t, "button.down", wantDowns, 5*time.Second)
	waitPressed(t, p, id, "true", 5*time.Second)
	return x, y
}

// liveWithCaps opens the page on a fresh bench and waits for LIVE and the enabled caps.
func liveWithCaps(t *testing.T) (*bench, *cdptest.Page) {
	t.Helper()
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `[...document.querySelectorAll('button.usr')].every((e) => !e.disabled && e.getAttribute('aria-pressed') === 'false')`); err != nil {
		t.Fatalf("the caps never became enabled and dark: %v", err)
	}
	return b, p
}

// TestUSRTouchLeave (D-15, Research Pitfall 6): a finger that goes down on USR1 and slides well off
// the cap releases the hold at once, while the finger is still down. A touch pointer is captured by
// the cap it started on, which would hide the leave; the page lets the capture go.
func TestUSRTouchLeave(t *testing.T) {
	b, p := liveWithCaps(t)
	x, y := capCentre(t, p, "usr1")
	ctx := within(t, 10*time.Second)
	if err := p.Touch(ctx, "touchStart", x, y); err != nil {
		t.Fatal(err)
	}
	b.waitCalls(t, "button.down", 1, 5*time.Second)
	waitPressed(t, p, "usr1", "true", 5*time.Second)
	// Letting go of the implicit capture must not end the hold that is starting (the lostpointercapture
	// it causes is the page's own).
	settle(t, 400*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 0 || pressed(t, p, "usr1") != "true" {
		t.Fatalf("%d ups and aria-pressed %q while the finger rests on the cap, want no up and a lit cap", n, pressed(t, p, "usr1"))
	}
	if err := p.Touch(ctx, "touchMove", x, y-300); err != nil {
		t.Fatal(err)
	}
	up := b.waitCalls(t, "button.up", 1, 5*time.Second)
	if up[0].Args != "usr1" {
		t.Errorf("button.up args = %q, want usr1", up[0].Args)
	}
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	if err := p.Touch(ctx, "touchEnd", x, y-300); err != nil {
		t.Fatal(err)
	}
	settle(t, 300*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 1 {
		t.Errorf("%d ups after the finger lifted, want the one sent when it left the cap", n)
	}

	// A browser that reports the capture the page let go of (Chrome does not) must not end the new
	// hold with it: the first lostpointercapture of the touch is the page's own, a second one is real.
	evalOn(t, p, `(() => { window.__pid = null; document.querySelector('`+capSel("usr1")+`').addEventListener('pointerdown', (ev) => { window.__pid = ev.pointerId; }, true); return true; })()`, nil)
	if err := p.Touch(ctx, "touchStart", x, y); err != nil {
		t.Fatal(err)
	}
	b.waitCalls(t, "button.down", 2, 5*time.Second)
	waitPressed(t, p, "usr1", "true", 5*time.Second)
	lost := `(() => { document.querySelector('` + capSel("usr1") + `').dispatchEvent(new PointerEvent('lostpointercapture', {pointerId: window.__pid, bubbles: true})); return true; })()`
	evalOn(t, p, lost, nil)
	settle(t, 300*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 1 {
		t.Fatalf("%d ups after the page's own capture release was reported, want still 1", n)
	}
	evalOn(t, p, lost, nil)
	b.waitCalls(t, "button.up", 2, 5*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	if err := p.Touch(ctx, "touchEnd", x, y); err != nil {
		t.Fatal(err)
	}
}

// TestUSRBlurHidden (D-15): a window blur, and the tab becoming hidden, each release a held cap.
func TestUSRBlurHidden(t *testing.T) {
	b, p := liveWithCaps(t)
	x, y := holdFresh(t, b, p, "usr1", 1)
	evalOn(t, p, `window.dispatchEvent(new Event('blur'))`, nil)
	b.waitCalls(t, "button.up", 1, 5*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	mouse(t, p, "mouseReleased", x, y)

	x, y = holdFresh(t, b, p, "usr1", 2)
	evalOn(t, p, `(() => { Object.defineProperty(document, 'hidden', {value: true, configurable: true});
	  document.dispatchEvent(new Event('visibilitychange')); return true; })()`, nil)
	b.waitCalls(t, "button.up", 2, 5*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	mouse(t, p, "mouseReleased", x, y)
	evalOn(t, p, `(delete document.hidden, true)`, nil)
	settle(t, 300*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 2 {
		t.Errorf("%d ups, want exactly one per hold (the later pointerup must not send another)", n)
	}
}

// TestUSRKeyboard (D-15): on a focused cap, Space and Enter hold like a pointer (one down on the first
// keydown, nothing on a repeated one, one up on keyup); pointercancel, pointerleave, lostpointercapture
// and the cap losing focus each release a held cap.
func TestUSRKeyboard(t *testing.T) {
	b, p := liveWithCaps(t)
	ctx := within(t, 20*time.Second)
	evalOn(t, p, `(document.querySelector('`+capSel("usr3")+`').focus(), true)`, nil)

	downs, ups := 0, 0
	for _, key := range []string{" ", "Enter"} {
		if err := p.Key(ctx, "keyDown", key); err != nil {
			t.Fatal(err)
		}
		downs++
		b.waitCalls(t, "button.down", downs, 5*time.Second)
		waitPressed(t, p, "usr3", "true", 5*time.Second)
		evalOn(t, p, `(document.querySelector('`+capSel("usr3")+`').dispatchEvent(new KeyboardEvent('keydown', {key: `+strconvQuote(key)+`, repeat: true, bubbles: true, cancelable: true})), true)`, nil)
		settle(t, 200*time.Millisecond)
		if n := len(b.callsOf("button.down")); n != downs {
			t.Fatalf("key %q: %d downs after a repeated keydown, want %d", key, n, downs)
		}
		if err := p.Key(ctx, "keyUp", key); err != nil {
			t.Fatal(err)
		}
		ups++
		b.waitCalls(t, "button.up", ups, 5*time.Second)
		waitPressed(t, p, "usr3", "false", 5*time.Second)
	}
	if got := b.callsOf("button.down")[0].Args; got != "usr3 10000" {
		t.Errorf("button.down args = %q, want %q", got, "usr3 10000")
	}

	for _, ev := range []string{
		`new PointerEvent('pointercancel', {pointerId: 1, bubbles: true})`,
		`new PointerEvent('pointerleave', {pointerId: 1})`,
		`new PointerEvent('lostpointercapture', {pointerId: 1, bubbles: true})`,
		`new FocusEvent('blur')`,
	} {
		downs++
		x, y := holdFresh(t, b, p, "usr3", downs)
		evalOn(t, p, `(document.querySelector('`+capSel("usr3")+`').dispatchEvent(`+ev+`), true)`, nil)
		ups++
		b.waitCalls(t, "button.up", ups, 5*time.Second)
		waitPressed(t, p, "usr3", "false", 5*time.Second)
		mouse(t, p, "mouseReleased", x, y)
	}
	settle(t, 300*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != ups {
		t.Errorf("%d ups, want %d (one per hold)", n, ups)
	}
}

// strconvQuote quotes s as a JavaScript string literal (Go syntax is enough for these keys).
func strconvQuote(s string) string { return strconv.Quote(s) }

// TestUSRAutoRelease (D-15): when the engine releases the button after its max hold (button v false),
// the cap goes dark and the page's later pointerup sends no redundant up.
func TestUSRAutoRelease(t *testing.T) {
	b, p := liveWithCaps(t)
	x, y := holdFresh(t, b, p, "usr1", 1)
	b.AutoRelease("usr1")
	waitPressed(t, p, "usr1", "false", 5*time.Second)
	mouse(t, p, "mouseReleased", x, y)
	settle(t, 500*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 0 {
		t.Errorf("%d ups after the engine's max-hold release, want none", n)
	}
	if msg := textOf(t, p, "#message"); msg != "" {
		t.Errorf("message line = %q, want it empty (no button_not_held error)", msg)
	}
	// The cap works again afterwards.
	x, y = holdFresh(t, b, p, "usr1", 2)
	mouse(t, p, "mouseReleased", x, y)
	b.waitCalls(t, "button.up", 1, 5*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)
}

// TestUSRRejected (D-17, UIC-19): a refused down shows the API's message verbatim, the cap stays as
// the server reports it (dark), and the release that follows sends no up.
func TestUSRRejected(t *testing.T) {
	b, p := liveWithCaps(t)
	const msg = `usr1 is already held by <b>"the CLI"</b> & more`
	b.FailNext("button.down", apperr.Failed("button_busy", msg, map[string]any{"button": "usr1"}))
	x, y := capCentre(t, p, "usr1")
	mouse(t, p, "mousePressed", x, y)
	waitText(t, p, "#message", msg, 5*time.Second)
	if got := pressed(t, p, "usr1"); got != "false" {
		t.Errorf("cap aria-pressed = %q after a refused down, want false", got)
	}
	mouse(t, p, "mouseReleased", x, y)
	settle(t, 300*time.Millisecond)
	if n := len(b.callsOf("button.up")); n != 0 {
		t.Errorf("%d ups after a refused down, want none", n)
	}
}

// TestUSRDisabledWhenStale (UIC-18): while the link is down the caps are disabled and a press sends
// nothing.
func TestUSRDisabledWhenStale(t *testing.T) {
	b, p := liveWithCaps(t)
	b.HoldSnapshot()
	b.DropStreams()
	waitText(t, p, "#link", "DISCONNECTED, RETRYING", 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `[...document.querySelectorAll('button.usr')].every((e) => e.disabled)`); err != nil {
		t.Fatalf("the caps are not disabled on a lost link: %v", err)
	}
	x, y := capCentre(t, p, "usr1")
	mouse(t, p, "mousePressed", x, y)
	mouse(t, p, "mouseReleased", x, y)
	settle(t, 500*time.Millisecond)
	if n := len(b.callsOf("button.down")) + len(b.callsOf("button.up")); n != 0 {
		t.Errorf("%d button calls from a disabled cap, want none", n)
	}
	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `[...document.querySelectorAll('button.usr')].every((e) => !e.disabled)`); err != nil {
		t.Fatalf("the caps never came back: %v", err)
	}
}

// TestHeaderRealTime (UI-05, UIC-30): the REAL TIME cell is always there and reads IN SYNC; a sim
// behind event makes it the orange BEHIND chip with the accessible name, in_sync restores it, and the
// cell never changes width. The element carrying data-src holds the text itself.
func TestHeaderRealTime(t *testing.T) {
	b, p := liveWithCaps(t)
	const sel = `[data-src="sim.behind_real_time"]`
	waitText(t, p, sel, "IN SYNC", 5*time.Second)

	read := func() (r struct {
		Text, Label, Class, Fill, Color, Weight string
		Kids                                    int
		CellW                                   float64
	}) {
		t.Helper()
		evalOn(t, p, `(() => { const e = document.querySelector('`+sel+`'), cs = getComputedStyle(e);
		  return {Text: e.textContent, Label: e.getAttribute('aria-label') || '', Class: e.className, Fill: cs.backgroundColor, Color: cs.color,
		    Weight: cs.fontWeight, Kids: e.children.length, CellW: e.closest('.cell').getBoundingClientRect().width}; })()`, &r)
		return r
	}
	calm := read()
	if calm.Label != "" || strings.Contains(calm.Class, "warn") || calm.Kids != 0 {
		t.Errorf("IN SYNC cell = %+v, want no label, no warn class, no child element", calm)
	}
	if calm.Color != "rgb(230, 232, 235)" {
		t.Errorf("IN SYNC colour = %s, want the primary text colour", calm.Color)
	}
	var label string
	evalOn(t, p, `document.querySelector('[data-src="sim.behind_real_time"]').closest('.cell').querySelector('.label').textContent`, &label)
	if label != "REAL TIME" {
		t.Errorf("cell label = %q, want REAL TIME", label)
	}

	b.Emit(events.Event{K: "sim", V: "behind"}, func(st *control.Status) { st.Sim.BehindRealTime = true })
	waitText(t, p, sel, "BEHIND", 5*time.Second)
	behind := read()
	if behind.Label != "Simulator behind real time" || !strings.Contains(behind.Class, "warn") || behind.Kids != 0 ||
		behind.Fill != "rgb(255, 138, 31)" || behind.Color != "rgb(7, 8, 10)" {
		t.Errorf("BEHIND cell = %+v, want the orange #ff8a1f chip, #07080a text, the label, no child element", behind)
	}
	if behind.CellW != calm.CellW {
		t.Errorf("cell width %v with BEHIND, %v with IN SYNC: it must not change", behind.CellW, calm.CellW)
	}

	b.Emit(events.Event{K: "sim", V: "in_sync"}, func(st *control.Status) { st.Sim.BehindRealTime = false })
	waitText(t, p, sel, "IN SYNC", 5*time.Second)
	if back := read(); back.Label != "" || strings.Contains(back.Class, "warn") || back.Fill != calm.Fill || back.CellW != calm.CellW {
		t.Errorf("after in_sync the cell = %+v, want the calm look %+v", back, calm)
	}
}

// TestUSRUpSurvivesPageTeardown (WR-02): the up of a hold is sent with fetch keepalive, so the browser
// does not cancel it when the page goes away (tab closed or hidden, window blurred), which is exactly
// when the page-side release is sent. The down is an ordinary request.
func TestUSRUpSurvivesPageTeardown(t *testing.T) {
	b, p := liveWithCaps(t)
	evalOn(t, p, `(() => { window.__fetches = []; const f = window.fetch.bind(window);
	  window.fetch = (url, init) => { window.__fetches.push({url: String(url), keepalive: !!(init && init.keepalive)}); return f(url, init); };
	  return true; })()`, nil)
	holdFresh(t, b, p, "usr1", 1)
	// The window loses focus while the cap is held: releaseAll sends the up.
	evalOn(t, p, `(window.dispatchEvent(new Event('blur')), true)`, nil)
	b.waitCalls(t, "button.up", 1, 5*time.Second)
	waitPressed(t, p, "usr1", "false", 5*time.Second)

	var got []struct {
		URL       string `json:"url"`
		Keepalive bool   `json:"keepalive"`
	}
	evalOn(t, p, `window.__fetches`, &got)
	seenUp := false
	for _, f := range got {
		switch {
		case strings.HasSuffix(f.URL, "/buttons/usr1/up"):
			seenUp = true
			if !f.Keepalive {
				t.Errorf("the up request %s has no keepalive: the browser may cancel it while the page unloads", f.URL)
			}
		case strings.HasSuffix(f.URL, "/buttons/usr1/down"):
			if f.Keepalive {
				t.Errorf("the down request %s has keepalive, want an ordinary request", f.URL)
			}
		}
	}
	if !seenUp {
		t.Errorf("no up request among %+v", got)
	}
}

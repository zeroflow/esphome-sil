//go:build sil_ui

package uitest

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// openSized opens a tab at the given viewport (set before the page loads) on base.
func openSized(t *testing.T, base string, w, h int, mobile bool) *cdptest.Page {
	t.Helper()
	ctx := within(t, 20*time.Second)
	p, err := chrome.NewPage(ctx)
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.SetViewport(ctx, w, h, mobile); err != nil {
		t.Fatalf("set viewport: %v", err)
	}
	if err := p.Navigate(ctx, base+"/"); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	return p
}

// shoot writes a screenshot of the page to $SIL_UI_SHOTS/<name>.png when that variable names a
// directory (the plan's evidence screenshots); otherwise it does nothing.
func shoot(t *testing.T, p *cdptest.Page, name string) {
	t.Helper()
	dir := os.Getenv("SIL_UI_SHOTS")
	if dir == "" {
		return
	}
	png, err := p.Screenshot(within(t, 10*time.Second))
	if err != nil {
		t.Errorf("screenshot %s: %v", name, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), png, 0o644); err != nil {
		t.Errorf("write screenshot %s: %v", name, err)
	}
}

type box struct {
	L float64 `json:"l"`
	T float64 `json:"t"`
	W float64 `json:"w"`
	H float64 `json:"h"`
	R float64 `json:"r"`
	B float64 `json:"b"`
}

const rectJS = `const rect = (el) => { const b = el.getBoundingClientRect(); return {l: b.left, t: b.top, w: b.width, h: b.height, r: b.right, b: b.bottom}; };`

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

var channelRGB = []string{"rgb(255, 212, 0)", "rgb(47, 211, 107)", "rgb(61, 139, 255)", "rgb(255, 95, 210)"}

const (
	textRGB  = "rgb(230, 232, 235)"
	errorRGB = "rgb(255, 77, 77)"
)

func styleOf(t *testing.T, p *cdptest.Page, selector, prop string) string {
	t.Helper()
	var s string
	js := fmt.Sprintf("getComputedStyle(document.querySelector(%q))[%q]", selector, prop)
	if err := p.Eval(within(t, 10*time.Second), js, &s); err != nil {
		t.Fatalf("style %s of %s: %v", prop, selector, err)
	}
	return s
}

// headerBoxes returns the rectangles of the header's cells and the message line.
func headerBoxes(t *testing.T, p *cdptest.Page) []box {
	t.Helper()
	var out []box
	js := `(() => { ` + rectJS + ` return [...document.getElementById('header').children].map(rect); })()`
	if err := p.Eval(within(t, 10*time.Second), js, &out); err != nil {
		t.Fatalf("header boxes: %v", err)
	}
	return out
}

// TestLayoutDesktop: at 1280 x 720 the page is one screen: a 64 px header, a 2 x 2 grid of panels
// and a 128 px strip, in the instrument palette with the UI-SPEC type scale (D-01..D-05).
func TestLayoutDesktop(t *testing.T) {
	b := newBench(t)
	b.HoldSnapshot()
	base := newSite(t, b)
	p := openSized(t, base, 1280, 720, false)
	b.waitCalls(t, "Watch", 1, 10*time.Second)
	waitText(t, p, "#link", "CONNECTING", 5*time.Second)
	connecting := headerBoxes(t, p)
	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitText(t, p, `[data-src="ports.3.rpm"]`, "1400", 5*time.Second)

	var g struct {
		ScrollH, InnerH float64
		ScrollW, InnerW float64
		Header, Strip   box
		Panels          []box
		Chips           []string
		Edges           []string
		Rpm, Duty       []struct {
			Size, Weight, Num, Color string
			W                        float64
		}
		Family string
		Loads  []struct {
			Row, First, Panel, Scope box
			Units                    []string
			Srcs                     []string
		}
	}
	js := `(() => { ` + rectJS + `
	  const panels = [...document.querySelectorAll('#ports .port')];
	  const val = (el) => { const cs = getComputedStyle(el); return {size: cs.fontSize, weight: cs.fontWeight, num: cs.fontVariantNumeric, color: cs.color, w: el.getBoundingClientRect().width}; };
	  const cap = (o) => ({Size: o.size, Weight: o.weight, Num: o.num, Color: o.color, W: o.w});
	  return {
	    ScrollH: document.documentElement.scrollHeight, InnerH: window.innerHeight,
	    ScrollW: document.documentElement.scrollWidth, InnerW: window.innerWidth,
	    Header: rect(document.getElementById('header')), Strip: rect(document.getElementById('strip')),
	    Panels: panels.map(rect),
	    Chips: panels.map((q) => getComputedStyle(q.querySelector('.chip')).backgroundColor),
	    Edges: panels.map((q) => getComputedStyle(q).borderTopColor),
	    Rpm: panels.map((q) => cap(val(q.querySelector('.readout .value[data-fmt="rpm"]')))),
	    Duty: panels.map((q) => cap(val(q.querySelector('.readout .value[data-fmt="pct"]')))),
	    Family: getComputedStyle(document.body).fontFamily,
	    Loads: panels.map((q) => { const row = q.querySelector('.row.loads');
	      return {Row: row ? rect(row) : rect(q), First: rect(q.querySelector('.row')), Panel: rect(q), Scope: rect(q.querySelector('.scope')),
	        Units: row ? [...row.querySelectorAll('.readout .unit')].map((e) => e.textContent) : [],
	        Srcs: row ? [...row.querySelectorAll('.readout .value')].map((e) => e.dataset.src) : []}; }),
	  };
	})()`
	if err := p.Eval(within(t, 10*time.Second), js, &g); err != nil {
		t.Fatal(err)
	}
	if g.ScrollH > g.InnerH || g.ScrollW > g.InnerW {
		t.Errorf("the page scrolls at 1280 x 720: scroll %v x %v, viewport %v x %v", g.ScrollW, g.ScrollH, g.InnerW, g.InnerH)
	}
	if !near(g.Header.H, 64, 1) {
		t.Errorf("header height = %v, want 64", g.Header.H)
	}
	if !near(g.Strip.H, 128, 1) {
		t.Errorf("bottom strip height = %v, want 128", g.Strip.H)
	}

	// The strip holds every group (LEDs, buttons, environment) inside its content box, and the LED
	// group (the tallest) fits that content with no inner scrolling (UIC-16, 03-12 final check).
	var sg struct {
		Content box
		Groups  []struct {
			ID string `json:"id"`
			box
		}
		LedsScroll, LedsClient float64
		LedsNatural            float64
		Lenses                 int
		Controls               int
	}
	const stripJS = `(() => { ` + rectJS + `
	  const strip = document.getElementById('strip'), r = strip.getBoundingClientRect(), cs = getComputedStyle(strip);
	  const f = (v) => parseFloat(v);
	  const content = {l: r.left + f(cs.borderLeftWidth) + f(cs.paddingLeft), t: r.top + f(cs.borderTopWidth) + f(cs.paddingTop),
	    r: r.right - f(cs.borderRightWidth) - f(cs.paddingRight), b: r.bottom - f(cs.borderBottomWidth) - f(cs.paddingBottom)};
	  content.w = content.r - content.l; content.h = content.b - content.t;
	  const leds = document.getElementById('leds'), lg = document.getElementById('leds-group');
	  // The group's natural height: its children and the gaps between them (the group itself is stretched to the strip).
	  const kids = [...lg.children], gap = f(getComputedStyle(lg).rowGap) || 0;
	  const natural = kids.reduce((n, k) => n + (k === leds ? leds.scrollHeight + f(getComputedStyle(leds).borderTopWidth) + f(getComputedStyle(leds).borderBottomWidth) : k.getBoundingClientRect().height), 0) + gap * (kids.length - 1);
	  return {Content: content, LedsNatural: natural,
	    Groups: [...strip.querySelectorAll(':scope > .group')].map((e) => Object.assign({id: e.id}, rect(e))),
	    LedsScroll: leds.scrollHeight, LedsClient: leds.clientHeight,
	    Lenses: leds.querySelectorAll('.lens').length,
	    Controls: strip.querySelectorAll('button, input').length};
	})()`
	if err := p.Eval(within(t, 10*time.Second), stripJS, &sg); err != nil {
		t.Fatal(err)
	}
	t.Logf("strip content %+v", sg.Content)
	for _, grp := range sg.Groups {
		t.Logf("strip group %s: %+v", grp.ID, grp.box)
	}
	ids := make([]string, 0, len(sg.Groups))
	for _, grp := range sg.Groups {
		ids = append(ids, grp.ID)
		if grp.T < sg.Content.T-0.5 || grp.B > sg.Content.B+0.5 || grp.L < sg.Content.L-0.5 || grp.R > sg.Content.R+0.5 {
			t.Errorf("strip group %s %+v sticks out of the strip content %+v", grp.ID, grp.box, sg.Content)
		}
	}
	if strings.Join(ids, ",") != "leds-group,usr-group,env-group" {
		t.Errorf("strip groups = %v, want leds-group, usr-group, env-group", ids)
	}
	t.Logf("LED group natural height %v, strip content %v", sg.LedsNatural, sg.Content.H)
	if sg.LedsNatural > sg.Content.H+0.5 {
		t.Errorf("the LED group needs %v px, the strip content is only %v", sg.LedsNatural, sg.Content.H)
	}
	if sg.Lenses != 5 {
		t.Errorf("%d LED lenses in the strip, want 5", sg.Lenses)
	}
	if sg.Controls != 6 {
		t.Errorf("%d buttons and inputs in the strip, want 6 (USR1-3, TEMPERATURE, HUMIDITY, APPLY VALUES)", sg.Controls)
	}
	if sg.LedsScroll > sg.LedsClient {
		t.Errorf("the LED box scrolls inside the strip: scroll height %v > %v", sg.LedsScroll, sg.LedsClient)
	}
	if len(g.Panels) != 4 {
		t.Fatalf("%d panels, want 4", len(g.Panels))
	}
	if !near(g.Panels[0].T, g.Panels[1].T, 1) || !near(g.Panels[2].T, g.Panels[3].T, 1) || g.Panels[2].T < g.Panels[0].B-1 {
		t.Errorf("panels are not a 2 x 2 grid (ports 1 and 2 on top): %+v", g.Panels)
	}
	if !near(g.Panels[0].L, g.Panels[2].L, 1) || !near(g.Panels[1].L, g.Panels[3].L, 1) || g.Panels[1].L <= g.Panels[0].R-1 {
		t.Errorf("panel columns do not line up: %+v", g.Panels)
	}
	if g.Strip.T < g.Panels[2].B-1 || g.Panels[0].T < g.Header.B-1 {
		t.Errorf("header, grid and strip overlap: header %+v panels %+v strip %+v", g.Header, g.Panels, g.Strip)
	}
	for i := 0; i < 4; i++ {
		if g.Chips[i] != channelRGB[i] || g.Edges[i] != channelRGB[i] {
			t.Errorf("port %d chip %s, top edge %s, want %s for both", i+1, g.Chips[i], g.Edges[i], channelRGB[i])
		}
		for name, v := range map[string]struct {
			Size, Weight, Num, Color string
			W                        float64
		}{"RPM": g.Rpm[i], "duty": g.Duty[i]} {
			want := map[string]string{"RPM": "28px", "duty": "20px"}[name]
			if v.Size != want || v.Weight != "600" || !strings.Contains(v.Num, "tabular-nums") || v.Color != channelRGB[i] {
				t.Errorf("port %d %s value = %+v, want %s 600 tabular-nums %s", i+1, name, v, want, channelRGB[i])
			}
		}
	}
	// The second readout row (TACH RPM, AIRFLOW, POWER) sits below the main row, inside its panel, and
	// leaves the scope its 128 px.
	for i, l := range g.Loads {
		if got := strings.Join(l.Units, "|"); got != "TACH RPM|M³/H|W" {
			t.Errorf("panel %d loads row units = %q, want TACH RPM|M³/H|W", i+1, got)
		}
		want := fmt.Sprintf("ports.%[1]d.tach_rpm|ports.%[1]d.airflow_m3h|ports.%[1]d.power_w", i)
		if got := strings.Join(l.Srcs, "|"); got != want {
			t.Errorf("panel %d loads row data-src = %q, want %q", i+1, got, want)
		}
		if l.Row.T < l.First.B-0.5 || l.Row.R > l.Panel.R+0.5 || l.Row.B > l.Panel.B+0.5 || l.Row.L < l.Panel.L-0.5 {
			t.Errorf("panel %d: loads row %+v is not below the first row %+v and inside the panel %+v", i+1, l.Row, l.First, l.Panel)
		}
		if l.Scope.H < 128 {
			t.Errorf("panel %d: scope is %v px high at 1280 x 720, want at least 128", i+1, l.Scope.H)
		}
	}
	if !strings.HasPrefix(g.Family, "system-ui") {
		t.Errorf("font-family = %q, want the system stack starting with system-ui", g.Family)
	}
	if got := styleOf(t, p, "html", "colorScheme"); got != "dark" {
		t.Errorf("color-scheme = %q, want dark", got)
	}

	// D-01 sweep: four sizes, two weights, 2 px corners, and no shadow, glow or gradient except on the lenses.
	var sweep struct {
		Sizes, Weights, Bad []string
	}
	const sweepJS = `(() => {
	  const sizes = new Set(), weights = new Set(), bad = [];
	  for (const el of document.body.querySelectorAll('*')) {
	    const cs = getComputedStyle(el);
	    if ([...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim() !== '')) { sizes.add(cs.fontSize); weights.add(cs.fontWeight); }
	    if (el.classList.contains('lens')) { continue; }
	    const name = el.tagName.toLowerCase() + (el.className ? '.' + String(el.className).split(' ').join('.') : '');
	    if (cs.boxShadow !== 'none') { bad.push(name + ' box-shadow ' + cs.boxShadow); }
	    if (cs.textShadow !== 'none') { bad.push(name + ' text-shadow ' + cs.textShadow); }
	    if (cs.backgroundImage !== 'none') { bad.push(name + ' background-image ' + cs.backgroundImage); }
	    if (cs.filter !== 'none') { bad.push(name + ' filter ' + cs.filter); }
	    for (const c of ['borderTopLeftRadius', 'borderTopRightRadius', 'borderBottomLeftRadius', 'borderBottomRightRadius']) {
	      if (parseFloat(cs[c]) > 2) { bad.push(name + ' ' + c + ' ' + cs[c]); }
	    }
	  }
	  return {Sizes: [...sizes].sort(), Weights: [...weights].sort(), Bad: bad};
	})()`
	if err := p.Eval(within(t, 10*time.Second), sweepJS, &sweep); err != nil {
		t.Fatal(err)
	}
	for _, s := range sweep.Sizes {
		if s != "12px" && s != "14px" && s != "16px" && s != "20px" && s != "28px" {
			t.Errorf("font size %s is not one of 12, 14, 16, 20, 28 px (all: %v)", s, sweep.Sizes)
		}
	}
	for _, w := range sweep.Weights {
		if w != "400" && w != "600" {
			t.Errorf("font weight %s is not 400 or 600 (all: %v)", w, sweep.Weights)
		}
	}
	if len(sweep.Bad) != 0 {
		t.Errorf("effects outside the LED lenses: %v", sweep.Bad)
	}

	// The readout slots keep their width whatever the number: a value change moves no neighbour.
	widths := func() []float64 {
		var w []float64
		js := `[...document.querySelectorAll('.readout .value')].map((e) => e.getBoundingClientRect().width)`
		if err := p.Eval(within(t, 10*time.Second), js, &w); err != nil {
			t.Fatal(err)
		}
		return w
	}
	before := widths()
	b.Emit(events.Event{K: "rpm", Port: 1, V: 5.0}, func(st *control.Status) { st.Ports[0].RPM = 5 })
	b.Emit(events.Event{K: "duty", Port: 2, V: 1.0}, func(st *control.Status) { st.Ports[1].Duty = 1 })
	waitText(t, p, `[data-src="ports.0.rpm"]`, "5", 5*time.Second)
	waitText(t, p, `[data-src="ports.1.duty"]`, "100.0", 5*time.Second)
	after := widths()
	for i := range before {
		if !near(before[i], after[i], 0.5) {
			t.Errorf("readout %d changed width from %v to %v with its value", i, before[i], after[i])
		}
	}
	// The same for the TACH RPM, AIRFLOW and POWER readouts: a number replaces the em dash, and a
	// wider number replaces a shorter one, with no width change.
	if len(before) < 4*5 {
		t.Errorf("%d readout values on the page, want 5 per panel (RPM, DUTY, TACH RPM, AIRFLOW, POWER)", len(before))
	}
	b.Emit(events.Event{K: "tach", Port: 2, V: f64(1110)}, func(st *control.Status) { st.Ports[1].TachRPM = f64(1110) })
	b.Emit(events.Event{K: "load", Port: 2, V: events.Load{AirflowM3h: f64(61.2), PowerW: f64(0.32)}},
		func(st *control.Status) { st.Ports[1].AirflowM3h, st.Ports[1].PowerW = f64(61.2), f64(0.32) })
	waitText(t, p, `[data-src="ports.1.tach_rpm"]`, "1110", 5*time.Second)
	waitText(t, p, `[data-src="ports.1.airflow_m3h"]`, "61.2", 5*time.Second)
	b.Emit(events.Event{K: "tach", Port: 2, V: f64(9)}, func(st *control.Status) { st.Ports[1].TachRPM = f64(9) })
	b.Emit(events.Event{K: "load", Port: 2, V: events.Load{AirflowM3h: f64(5.5), PowerW: f64(0.01)}},
		func(st *control.Status) { st.Ports[1].AirflowM3h, st.Ports[1].PowerW = f64(5.5), f64(0.01) })
	waitText(t, p, `[data-src="ports.1.tach_rpm"]`, "9", 5*time.Second)
	waitText(t, p, `[data-src="ports.1.power_w"]`, "0.01", 5*time.Second)
	for i, w := range widths() {
		if i < len(before) && !near(before[i], w, 0.5) {
			t.Errorf("readout %d changed width from %v to %v with a tach or load value", i, before[i], w)
		}
	}

	// FIRMWARE reads red when the firmware is gone and primary text when it is back (never green).
	const fw = `[data-src="firmware.bus_connected"]`
	if got := styleOf(t, p, fw, "color"); got != textRGB {
		t.Errorf("FIRMWARE CONNECTED colour = %s, want %s", got, textRGB)
	}
	b.Emit(events.Event{K: "firmware", V: false}, func(st *control.Status) { st.Firmware.BusConnected = false })
	waitText(t, p, fw, "DISCONNECTED", 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), fmt.Sprintf("getComputedStyle(document.querySelector(%q)).color === %q", fw, errorRGB)); err != nil {
		t.Errorf("FIRMWARE DISCONNECTED colour = %s, want %s", styleOf(t, p, fw, "color"), errorRGB)
	}
	b.Emit(events.Event{K: "firmware", V: true}, func(st *control.Status) { st.Firmware.BusConnected = true })
	waitText(t, p, fw, "CONNECTED", 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), fmt.Sprintf("getComputedStyle(document.querySelector(%q)).color === %q", fw, textRGB)); err != nil {
		t.Errorf("FIRMWARE CONNECTED colour = %s, want %s", styleOf(t, p, fw, "color"), textRGB)
	}

	// LINK: primary text when LIVE, red when retrying, and no header cell moves or resizes in any state.
	if got := styleOf(t, p, "#link", "color"); got != textRGB {
		t.Errorf("LINK LIVE colour = %s, want %s", got, textRGB)
	}
	live := headerBoxes(t, p)
	b.HoldSnapshot()
	b.DropStreams()
	waitText(t, p, "#link", "DISCONNECTED, RETRYING", 10*time.Second)
	if got := styleOf(t, p, "#link", "color"); got != errorRGB {
		t.Errorf("LINK retrying colour = %s, want %s", got, errorRGB)
	}
	retrying := headerBoxes(t, p)
	var clipped []string
	if err := p.Eval(within(t, 10*time.Second), `[...document.querySelectorAll('#header .value')].filter((e) => !e.matches('[data-src="instance"]') && e.scrollWidth > e.clientWidth).map((e) => e.textContent)`, &clipped); err != nil || len(clipped) != 0 {
		t.Errorf("header values clipped: %v (%v)", clipped, err)
	}
	for name, got := range map[string][]box{"CONNECTING": connecting, "retrying": retrying} {
		if len(got) != len(live) {
			t.Errorf("%s: %d header children, LIVE has %d", name, len(got), len(live))
			continue
		}
		for i := range live {
			if !near(got[i].L, live[i].L, 0.5) || !near(got[i].W, live[i].W, 0.5) || !near(got[i].T, live[i].T, 0.5) {
				t.Errorf("header child %d moves or resizes between LIVE %+v and %s %+v", i, live[i], name, got[i])
			}
		}
	}
	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	shoot(t, p, "layout-1280x720")
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// TestLayoutNarrow: below 1024 px the panels stack in one column and the page scrolls.
func TestLayoutNarrow(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openSized(t, base, 1000, 800, false)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitText(t, p, `[data-src="ports.3.rpm"]`, "1400", 5*time.Second)
	var g struct {
		ScrollH, InnerH, ScrollW, InnerW float64
		Panels                           []box
		Header                           box
		Rows                             int
	}
	js := `(() => { ` + rectJS + `
	  const rows = new Set([...document.getElementById('header').children].map((c) => Math.round(c.getBoundingClientRect().top)));
	  return {ScrollH: document.documentElement.scrollHeight, InnerH: window.innerHeight,
	    ScrollW: document.documentElement.scrollWidth, InnerW: window.innerWidth,
	    Panels: [...document.querySelectorAll('#ports .port')].map(rect), Header: rect(document.getElementById('header')), Rows: rows.size};
	})()`
	if err := p.Eval(within(t, 10*time.Second), js, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Panels) != 4 {
		t.Fatalf("%d panels, want 4", len(g.Panels))
	}
	for i, q := range g.Panels {
		if !near(q.L, g.Panels[0].L, 1) {
			t.Errorf("panel %d left = %v, want the shared column at %v", i, q.L, g.Panels[0].L)
		}
		if i > 0 && q.T < g.Panels[i-1].B-1 {
			t.Errorf("panel %d does not sit below panel %d: %+v", i, i-1, g.Panels)
		}
	}
	if g.ScrollH <= g.InnerH {
		t.Errorf("the page cannot scroll at 1000 x 800 (scroll height %v, viewport %v)", g.ScrollH, g.InnerH)
	}
	if g.ScrollW > g.InnerW {
		t.Errorf("horizontal overflow at 1000 px: %v > %v", g.ScrollW, g.InnerW)
	}
	if g.Rows != 2 {
		t.Errorf("the header uses %d rows at 1000 px, want 2", g.Rows)
	}
}

// TestLayoutPhone: at 390 x 844 (mobile) the page is one column without sideways overflow, every
// real control (fan selects, USR caps, the two inputs, APPLY VALUES) is at least 44 x 44, every
// scope is at least 128 px high, and a panel's top row splits into chip + fan above the readouts.
func TestLayoutPhone(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openSized(t, base, 390, 844, true)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitText(t, p, `[data-src="ports.3.rpm"]`, "1400", 5*time.Second)

	var g struct {
		ScrollW, InnerW float64
		Panels          []box
		Chip, Fan, Rpm  box
		Duty            box
		Controls        []struct {
			Name string `json:"name"`
			box
		}
		Scopes []box
		Loads  []box
		Row    box
	}
	js := `(() => { ` + rectJS + `
	  const q = document.querySelector('#ports .port');
	  const name = (el) => el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + (el.getAttribute('aria-label') ? '[' + el.getAttribute('aria-label') + ']' : '');
	  return {ScrollW: document.documentElement.scrollWidth, InnerW: window.innerWidth,
	    Panels: [...document.querySelectorAll('#ports .port')].map(rect),
	    Chip: rect(q.querySelector('.chip')), Fan: rect(q.querySelector('.fan')),
	    Rpm: rect(q.querySelector('.readout .value[data-fmt="rpm"]')), Duty: rect(q.querySelector('.readout .value[data-fmt="pct"]')),
	    Controls: [...document.querySelectorAll('button, select, input')].map((el) => Object.assign({name: name(el)}, rect(el))),
	    Scopes: [...document.querySelectorAll('#ports .scope')].map(rect),
	    Row: rect(q.querySelector('.row.loads')), Loads: [...q.querySelectorAll('.row.loads .readout')].map(rect)};
	})()`
	if err := p.Eval(within(t, 10*time.Second), js, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Loads) != 3 {
		t.Errorf("%d readouts in the loads row, want 3 (TACH RPM, AIRFLOW, POWER)", len(g.Loads))
	}
	for i, a := range g.Loads {
		if a.R > g.InnerW+0.5 || a.R > g.Row.R+0.5 {
			t.Errorf("loads readout %d runs past the viewport or its row: %+v row %+v", i, a, g.Row)
		}
		for j := i + 1; j < len(g.Loads); j++ {
			c := g.Loads[j]
			if a.L < c.R-0.5 && c.L < a.R-0.5 && a.T < c.B-0.5 && c.T < a.B-0.5 {
				t.Errorf("loads readouts %d and %d overlap: %+v %+v", i, j, a, c)
			}
		}
	}
	for i, q := range g.Panels {
		if !near(q.L, g.Panels[0].L, 1) || !near(q.W, g.Panels[0].W, 1) {
			t.Errorf("panel %d is not in the one column: %+v", i, q)
		}
		if q.R > g.InnerW+0.5 {
			t.Errorf("panel %d runs past the %v px viewport: %+v", i, g.InnerW, q)
		}
	}
	if g.ScrollW > g.InnerW {
		t.Errorf("sideways overflow at 390 px: scroll width %v > %v", g.ScrollW, g.InnerW)
	}
	// 4 fan selects + USR1-3 + TEMPERATURE + HUMIDITY + APPLY VALUES.
	if len(g.Controls) != 10 {
		t.Errorf("%d controls on the page, want 10 (4 selects, 3 USR caps, 2 inputs, APPLY VALUES): %+v", len(g.Controls), g.Controls)
	}
	for _, c := range g.Controls {
		if c.H < 44 || c.W < 44 {
			t.Errorf("%s is %v x %v, want at least 44 x 44", c.Name, c.W, c.H)
		}
		if c.R > g.InnerW+0.5 {
			t.Errorf("%s runs past the %v px viewport: %+v", c.Name, g.InnerW, c.box)
		}
	}
	if len(g.Scopes) != 4 {
		t.Errorf("%d scopes, want 4", len(g.Scopes))
	}
	for i, s := range g.Scopes {
		if s.H < 128 {
			t.Errorf("scope %d is %v px high, want at least 128", i+1, s.H)
		}
	}
	if g.Chip.B > g.Rpm.T+0.5 || g.Fan.B > g.Rpm.T+0.5 || g.Chip.B > g.Duty.T+0.5 {
		t.Errorf("the top row did not split: chip %+v fan %+v rpm %+v duty %+v", g.Chip, g.Fan, g.Rpm, g.Duty)
	}
	if g.Fan.T >= g.Chip.B || g.Chip.T >= g.Fan.B {
		t.Errorf("fan is not on the chip's row: chip %+v fan %+v", g.Chip, g.Fan)
	}
	shoot(t, p, "layout-390x844")
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// TestNoVendorMarks: no vendor name or trademark appears in the rendered text, the title or any
// first-party file the page loads (D-01: the bench-oscilloscope look is a style reference only).
func TestNoVendorMarks(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)

	seen := map[string]bool{"/": true}
	for _, r := range p.Requests() {
		u, err := url.Parse(r.URL)
		if err != nil || r.Kind != "http" || !cdptest.SameOrigin(base, r.URL) || strings.HasPrefix(u.Path, "/vendor/") || strings.HasPrefix(u.Path, "/api/") {
			continue
		}
		seen[u.Path] = true
	}
	for _, want := range []string{"/app.css", "/js/app.js", "/js/store.js", "/js/view.js"} {
		if !seen[want] {
			t.Errorf("the page did not load %s", want)
		}
	}
	var corpus string
	for path := range seen {
		var body string
		js := fmt.Sprintf("fetch(%q).then((r) => r.text())", path)
		if err := p.Eval(within(t, 10*time.Second), js, &body); err != nil {
			t.Fatalf("fetch %s: %v", path, err)
		}
		corpus += "\n" + body
	}
	var rendered string
	if err := p.Eval(within(t, 10*time.Second), `document.title + '\n' + document.documentElement.innerText`, &rendered); err != nil {
		t.Fatal(err)
	}
	corpus = strings.ToLower(corpus + "\n" + rendered)
	for _, banned := range []string{"keysight", "agilent", "hewlett", "infiniivision"} {
		if strings.Contains(corpus, banned) {
			t.Errorf("the page or its files mention %q", banned)
		}
	}
}

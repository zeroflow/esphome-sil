//go:build sil_ui

package uitest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

func f64(v float64) *float64 { return &v }

// us is seconds of sim time as the microseconds of an event.
func us(sec float64) int64 { return int64(sec*1e6 + 0.5) }

// sampleEvent is a sample event at sec seconds with the same duty (0..1) and rpm on all four
// ports; a nil duty or rpm is the null of a firmware that is off the bus. gap is the engine's
// first-sample marker.
func sampleEvent(sec float64, gap bool, duty, rpm *float64) events.Event {
	v := make([]events.Sample, 4)
	for i := range v {
		v[i] = events.Sample{Port: i + 1, Duty: duty, RPM: rpm}
	}
	return events.Event{T: us(sec), K: events.SampleKind, V: v, Gap: gap}
}

// publish puts ev on the bench's broker (and into its history ring) and returns its seq.
func (b *bench) publish(ev events.Event) uint64 {
	return b.broker.Publish(ev)
}

// plotPage opens the page on b, waits for LIVE and loads the plot and ring modules as window.__plot
// and window.__ring.
func plotPage(t *testing.T, b *bench) *cdptest.Page {
	t.Helper()
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	importPlot(t, p)
	return p
}

func importPlot(t *testing.T, p *cdptest.Page) {
	t.Helper()
	const js = `(async () => { window.__plot = await import('/js/plot.js'); window.__ring = await import('/js/ring.js'); return true; })()`
	if err := p.Eval(within(t, 10*time.Second), js, nil); err != nil {
		t.Fatalf("importing the plot modules: %v", err)
	}
}

// evalPlot evaluates js (an expression) in the page and decodes the JSON result into out.
func evalPlot(t *testing.T, p *cdptest.Page, js string, out any) {
	t.Helper()
	if err := p.Eval(within(t, 10*time.Second), js, out); err != nil {
		t.Fatalf("eval %s: %v", js, err)
	}
}

func waitPlot(t *testing.T, p *cdptest.Page, what, pred string, d time.Duration) {
	t.Helper()
	if err := p.WaitFor(within(t, d), pred); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// ringXs returns the x values (seconds) of a port's ring.
func ringXs(t *testing.T, p *cdptest.Page, port int) []float64 {
	t.Helper()
	var xs []float64
	evalPlot(t, p, fmt.Sprintf(`__plot.ringFor(%d).xs.slice()`, port), &xs)
	return xs
}

// waitDrawn waits until port's scope has been drawn with its ring's newest point at the right edge.
func waitDrawn(t *testing.T, p *cdptest.Page, port int) {
	t.Helper()
	waitPlot(t, p, fmt.Sprintf("scope %d drawn", port), fmt.Sprintf(`(() => { const u = __plot.scopeFor(%[1]d), r = __plot.ringFor(%[1]d);
	  return u && u.scales.x && r.xs.length > 0 && u.scales.x.max === r.xs[r.xs.length - 1]; })()`, port), 10*time.Second)
}

// hasColour reports whether the canvas column of x seconds on port's scope holds a pixel of the
// given colour (rgb, tolerance 40 per channel, opaque).
func hasColour(t *testing.T, p *cdptest.Page, port int, sec float64, rgb [3]int) bool {
	t.Helper()
	js := fmt.Sprintf(`(() => { const u = __plot.scopeFor(%d); const x = Math.round(u.valToPos(%v, 'x', true));
	  const {top, height} = u.bbox; const d = u.ctx.getImageData(x, top, 1, height).data;
	  for (let i = 0; i < d.length; i += 4) {
	    if (d[i + 3] > 200 && Math.abs(d[i] - %d) < 40 && Math.abs(d[i + 1] - %d) < 40 && Math.abs(d[i + 2] - %d) < 40) { return true; }
	  }
	  return false; })()`, port, sec, rgb[0], rgb[1], rgb[2])
	var found bool
	evalPlot(t, p, js, &found)
	return found
}

var (
	yellow = [3]int{0xff, 0xd4, 0x00} // --ch1, the colour of port 1
	green  = [3]int{0x2f, 0xd3, 0x6b}
	blue   = [3]int{0x3d, 0x8b, 0xff}
	pink   = [3]int{0xff, 0x5f, 0xd2}
)

// TestScopeLive: bench samples at 10 Hz for port 1 reach the scope within a second, and the x range
// ends at the newest sample's seconds and spans 60 s. The scope is redrawn at most 20 times a
// second even when samples arrive faster.
func TestScopeLive(t *testing.T) {
	b := newBench(t)
	p := plotPage(t, b)
	waitPlot(t, p, "scope 1 exists", `__plot.scopeFor(1) !== null`, 10*time.Second)

	for i := 0; i < 3; i++ {
		b.publish(sampleEvent(20+0.1*float64(i), false, f64(0.4), f64(900)))
		if err := sysClock.Sleep(within(t, 5*time.Second), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.WaitFor(within(t, time.Second), `__plot.scopeFor(1).data[1].some((v) => v !== null)`); err != nil {
		t.Fatalf("no sample reached scope 1 within a second: %v", err)
	}
	waitDrawn(t, p, 1)
	var x struct{ Min, Max float64 }
	evalPlot(t, p, `({Min: __plot.scopeFor(1).scales.x.min, Max: __plot.scopeFor(1).scales.x.max})`, &x)
	if want := float64(us(20.2)) / 1e6; x.Max != want || x.Max-x.Min != 60 {
		t.Errorf("x range = %v..%v, want %v-60..%v (the newest sample's seconds, a 60 s window)", x.Min, x.Max, want, want)
	}

	// 200 samples a second for a second or more: the redraws stay at or below 20 a second.
	evalPlot(t, p, `(() => { window.__draws = 0; window.__t0 = performance.now(); for (let n = 1; n <= 4; n++) { const u = __plot.scopeFor(n), set = u.setData.bind(u);
	  u.setData = (d, r) => { if (n === 1) { window.__draws++; } return set(d, r); }; } return true; })()`, nil)
	for i := 0; i < 200; i++ {
		b.publish(sampleEvent(21+0.005*float64(i), false, f64(0.4), f64(900)))
		if err := sysClock.Sleep(within(t, 5*time.Second), 5*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	var rate struct {
		Draws int
		Ms    float64
	}
	evalPlot(t, p, `({Draws: window.__draws, Ms: performance.now() - window.__t0})`, &rate)
	if limit := int(rate.Ms/50) + 1; rate.Draws == 0 || rate.Draws > limit {
		t.Errorf("scope 1 was drawn %d times in %.0f ms of 200 samples a second, want between 1 and %d (at most 20 a second)", rate.Draws, rate.Ms, limit)
	}
}

// TestScopeStyle: every scope has the UI-SPEC look: RPM a 2 px solid trace and duty a 1 px dashed
// [4,4] trace in the port's channel colour, a 10 x 4 graticule, the duty axis at 70 % alpha, uPlot's
// own legend and cursor off, the static `6 s/div` and RPM / DUTY texts, and the 1 px #3a4047 well.
func TestScopeStyle(t *testing.T) {
	b := newBench(t)
	p := plotPage(t, b)
	waitPlot(t, p, "four scopes", `[1, 2, 3, 4].every((n) => __plot.scopeFor(n) !== null)`, 10*time.Second)
	b.publish(sampleEvent(20, false, f64(0.4), f64(900)))
	waitDrawn(t, p, 4)

	type look struct {
		Panel                string
		Colour, Stroke1      string
		Width1, Width2       float64
		Dash1                any
		Dash2                []float64
		DutyAxis             string
		XSplits, RPMSplits   int
		GridX, GridRPM       string
		LegendShown, Cursor  bool
		Legend, Timebase     string
		Border, BorderColour string
		Overlay              bool
	}
	hex := map[int]string{1: "#ffd400", 2: "#2fd36b", 3: "#3d8bff", 4: "#ff5fd2"}
	for n := 1; n <= 4; n++ {
		var l look
		evalPlot(t, p, fmt.Sprintf(`(() => { const u = __plot.scopeFor(%d); const val = (f) => (typeof f === 'function' ? f(u, 0) : f); const panel = document.querySelectorAll('#ports .port')[%d];
		  const scope = panel.querySelector('.scope'); const cs = getComputedStyle(scope);
		  return {Panel: panel.className, Colour: getComputedStyle(panel).getPropertyValue('--ch').trim(),
		    Stroke1: u.series[1].stroke(u, 1), Width1: u.series[1].width, Width2: u.series[2].width,
		    Dash1: u.series[1].dash === undefined ? null : u.series[1].dash, Dash2: u.series[2].dash,
		    DutyAxis: u.axes[2].stroke(u, 2), XSplits: u.axes[0].splits(u, 0, 0, 60).length, RPMSplits: u.axes[1].splits(u, 1, 0, 1800).length,
		    GridX: val(u.axes[0].grid.stroke), GridRPM: val(u.axes[1].grid.stroke),
		    LegendShown: !!u.root.querySelector('.u-legend'), Cursor: !!u.root.querySelector('.u-cursor-x, .u-cursor-y'),
		    Legend: scope.querySelector('.legend').textContent.replace(/\s+/g, ' ').trim(), Timebase: scope.querySelector('.timebase').textContent,
		    Border: cs.borderTopWidth, BorderColour: cs.borderTopColor, Overlay: u.root.closest('.scope') === scope}; })()`, n, n-1), &l)
		if l.Colour != hex[n] || l.Stroke1 != hex[n] {
			t.Errorf("port %d: channel colour %q, RPM stroke %q, want %s", n, l.Colour, l.Stroke1, hex[n])
		}
		if l.Width1 != 2 || l.Dash1 != nil {
			t.Errorf("port %d: RPM width %v dash %v, want 2 and no dash", n, l.Width1, l.Dash1)
		}
		if l.Width2 != 1 || len(l.Dash2) != 2 || l.Dash2[0] != 4 || l.Dash2[1] != 4 {
			t.Errorf("port %d: duty width %v dash %v, want 1 and [4 4]", n, l.Width2, l.Dash2)
		}
		if !strings.HasPrefix(l.DutyAxis, "rgba(") || !strings.HasSuffix(l.DutyAxis, ", 0.7)") {
			t.Errorf("port %d: duty axis colour %q, want the channel colour at 70%% alpha", n, l.DutyAxis)
		}
		if l.XSplits != 11 || l.RPMSplits != 5 || l.GridX != "#20252b" || l.GridRPM != "#20252b" {
			t.Errorf("port %d: graticule %d x %d splits, colours %s / %s; want 11 x 5 (10 x 4 divisions) in #20252b", n, l.XSplits, l.RPMSplits, l.GridX, l.GridRPM)
		}
		if l.LegendShown || l.Cursor {
			t.Errorf("port %d: uPlot's own legend (%v) or cursor (%v) is on", n, l.LegendShown, l.Cursor)
		}
		if l.Legend != "RPM DUTY" || l.Timebase != "6 s/div" {
			t.Errorf("port %d: legend %q and timebase %q, want `RPM DUTY` and `6 s/div`", n, l.Legend, l.Timebase)
		}
		if l.Border != "1px" || l.BorderColour != "rgb(58, 64, 71)" || !l.Overlay {
			t.Errorf("port %d: well border %s %s, want 1px rgb(58, 64, 71)", n, l.Border, l.BorderColour)
		}
	}

	// The graticule is drawn: the centre vertical line (#2e343b) is on the canvas of port 1.
	var centre bool
	evalPlot(t, p, `(() => { const u = __plot.scopeFor(1); const {left, top, width} = u.bbox; const cx = Math.round(left + width / 2);
	  const d = u.ctx.getImageData(cx - 2, top + 5, 5, 1).data;
	  for (let i = 0; i < d.length; i += 4) { if (Math.abs(d[i] - 0x2e) < 3 && Math.abs(d[i + 1] - 0x34) < 3 && Math.abs(d[i + 2] - 0x3b) < 3) { return true; } }
	  return false; })()`, &centre)
	if !centre {
		t.Errorf("no #2e343b centre line on the canvas of port 1")
	}
}

// TestRingBounded: a Ring(620, 64) never holds more than 684 points however many are pushed and
// ends with exactly the newest points; and after 3000 live bench samples the page's rings hold at
// most 684 points per port.
func TestRingBounded(t *testing.T) {
	b := newBench(t)
	p := plotPage(t, b)

	var res struct {
		Max, Len int
		Bad      string
	}
	evalPlot(t, p, `(() => { const N = 1000000, r = new __ring.Ring(620, 64); let max = 0;
	  for (let i = 0; i < N; i++) { r.push(i * 100000, i, 0.5, false); if (r.xs.length > max) { max = r.xs.length; } }
	  const first = N - r.xs.length; let bad = '';
	  for (let j = 0; j < r.xs.length && bad === ''; j++) {
	    const i = first + j;
	    if (r.xs[j] !== (i * 100000) / 1e6 || r.rpm[j] !== i || r.duty[j] !== 50 || r.rpm.length !== r.xs.length || r.duty.length !== r.xs.length) { bad = 'index ' + j; }
	  }
	  return {Max: max, Len: r.xs.length, Bad: bad}; })()`, &res)
	if res.Max > 684 || res.Len < 620 || res.Bad != "" {
		t.Errorf("Ring(620, 64) after 1,000,000 pushes: max %d, final %d points, mismatch %q; want at most 684, at least 620 and exactly the newest points", res.Max, res.Len, res.Bad)
	}

	// 3000 samples through the real stream, in batches the subscriber queue takes without lagging.
	for i := 0; i < 3000; i++ {
		b.publish(sampleEvent(1+0.1*float64(i), false, f64(0.5), f64(float64(i))))
		if i%50 == 49 {
			if err := sysClock.Sleep(within(t, 5*time.Second), 20*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
	}
	last := float64(us(1+0.1*2999)) / 1e6
	waitPlot(t, p, "the last sample in the rings", fmt.Sprintf(`[1, 2, 3, 4].every((n) => { const r = __plot.ringFor(n); return r.xs.length > 0 && r.xs[r.xs.length - 1] === %v; })`, last), 15*time.Second)
	var lens []int
	evalPlot(t, p, `[1, 2, 3, 4].map((n) => __plot.ringFor(n).xs.length)`, &lens)
	for i, n := range lens {
		if n > 684 || n < 600 {
			t.Errorf("port %d holds %d points after 3000 samples, want between 600 and 684", i+1, n)
		}
	}
	var data []int
	evalPlot(t, p, `[1, 2, 3, 4].map((n) => __plot.scopeFor(n).data[0].length)`, &data)
	for i, n := range data {
		if n > 684 {
			t.Errorf("port %d's scope holds %d points, want at most 684", i+1, n)
		}
	}
}

// plotHistory is the history of a scope test: runs of (duty 0.5, rpm 900) samples every 100 ms from
// a to b seconds.
func publishRun(b *bench, from, to float64, first bool, null bool) {
	n := int((to-from)*10 + 0.5)
	for i := 0; i < n; i++ {
		sec := from + 0.1*float64(i)
		gap := first && i == 0
		if null {
			b.publish(sampleEvent(sec, gap, nil, nil))
			continue
		}
		b.publish(sampleEvent(sec, gap, f64(0.5), f64(900)))
	}
}

// TestPlotGap: samples whose duty and rpm are null (the firmware is off the bus) are a gap in both
// traces, never a zero: the canvas column in the middle of the gap has no pixel of the channel
// colour, the columns 2 s before and after it do.
func TestPlotGap(t *testing.T) {
	b := newBench(t)
	publishRun(b, 0, 10, false, false)
	publishRun(b, 10, 15, false, true)
	publishRun(b, 15, 25, false, false)
	p := plotPage(t, b)
	waitPlot(t, p, "the history in the rings", `__plot.ringFor(1).xs.length >= 250`, 10*time.Second)
	waitDrawn(t, p, 1)
	waitPlot(t, p, "fan list and scale", `__plot.scopeFor(1).scales.rpm.max === 1800`, 10*time.Second)

	for _, c := range []struct {
		sec  float64
		want bool
		what string
	}{{8, true, "2 s before the gap"}, {12.5, false, "the middle of the gap"}, {17, true, "2 s after the gap"}} {
		if got := hasColour(t, p, 1, c.sec, yellow); got != c.want {
			t.Errorf("column at %v s (%s): channel colour present = %v, want %v", c.sec, c.what, got, c.want)
		}
	}
	// The null samples are in the ring as nulls, not zeros.
	var zeros int
	evalPlot(t, p, `__plot.ringFor(1).rpm.filter((v) => v === 0).length + __plot.ringFor(1).duty.filter((v) => v === 0).length`, &zeros)
	if zeros != 0 {
		t.Errorf("the ring holds %d zero values, want none: null samples must stay null", zeros)
	}
}

// TestPlotRestartGap: a sample the server marked `gap: true` (the first sample of every engine)
// breaks both traces before it; the same time jump without the marker is joined by a line, because
// the page derives no gap from sample timing. Ring.push renders the marker as one null point at the
// new sample's own x.
func TestPlotRestartGap(t *testing.T) {
	for _, marked := range []bool{true, false} {
		name := map[bool]string{true: "marked", false: "unmarked"}[marked]
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			publishRun(b, 0, 10, false, false)
			publishRun(b, 13, 23, marked, false)
			p := plotPage(t, b)
			waitPlot(t, p, "the history in the rings", `__plot.ringFor(1).xs.length >= 200`, 10*time.Second)
			waitDrawn(t, p, 1)
			waitPlot(t, p, "fan list and scale", `__plot.scopeFor(1).scales.rpm.max === 1800`, 10*time.Second)

			if got := hasColour(t, p, 1, 5, yellow); !got {
				t.Errorf("no channel colour in the first run")
			}
			if got := hasColour(t, p, 1, 18, yellow); !got {
				t.Errorf("no channel colour in the second run")
			}
			if got := hasColour(t, p, 1, 11.5, yellow); got == marked {
				t.Errorf("column in the middle of the 3 s jump: channel colour present = %v with the marker %v, want %v (a marked sample breaks the line, an unmarked jump does not)", got, marked, !marked)
			}
		})
	}

	t.Run("ring", func(t *testing.T) {
		b := newBench(t)
		p := plotPage(t, b)
		var res struct {
			Marked, Unmarked struct {
				Xs, Rpm, Duty []*float64
			}
		}
		evalPlot(t, p, `(() => { const run = (gap) => { const r = new __ring.Ring(620, 64); r.push(1e6, 800, 0.4, false); r.push(4e6, 900, 0.5, gap);
		  return {Xs: r.xs, Rpm: r.rpm, Duty: r.duty}; }; return {Marked: run(true), Unmarked: run(false)}; })()`, &res)
		if got := fmt.Sprint(deref(res.Marked.Xs)); got != "[1 4 4]" {
			t.Errorf("marked xs = %s, want [1 4 4]", got)
		}
		if m := res.Marked; len(m.Rpm) != 3 || m.Rpm[1] != nil || m.Duty[1] != nil || m.Rpm[0] == nil || m.Rpm[2] == nil || *m.Rpm[2] != 900 || *m.Duty[2] != 50 {
			t.Errorf("marked rpm %v duty %v, want null at index 1 and the real values at 0 and 2", show(res.Marked.Rpm), show(res.Marked.Duty))
		}
		if got := fmt.Sprint(deref(res.Unmarked.Xs)); got != "[1 4]" {
			t.Errorf("unmarked xs = %s, want [1 4]", got)
		}
	})
}

// show prints a series with null for a nil point.
func show(v []*float64) []string {
	out := make([]string, len(v))
	for i, p := range v {
		out[i] = "null"
		if p != nil {
			out[i] = fmt.Sprint(*p)
		}
	}
	return out
}

func deref(v []*float64) []float64 {
	out := make([]float64, len(v))
	for i, p := range v {
		if p != nil {
			out[i] = *p
		}
	}
	return out
}

// TestFixedAxes: the RPM scale of a port is 0..max_rpm of its plugged fan and the duty scale 0..100,
// whatever the samples say; a fan swap re-scales the RPM axis once, and only that port's.
func TestFixedAxes(t *testing.T) {
	arcticMax, noctuaMax := fanMax(t, arctic), fanMax(t, noctua)
	b := newBench(t)
	p := plotPage(t, b)
	waitPlot(t, p, "port 1 scaled to the Arctic", fmt.Sprintf(`(() => { const u = __plot.scopeFor(1); return u.scales.rpm && u.scales.rpm.max === %v && u.scales.rpm.min === 0; })()`, arcticMax), 10*time.Second)
	var dutyMax float64
	evalPlot(t, p, `__plot.scopeFor(1).scales.duty.max`, &dutyMax)
	if dutyMax != 100 {
		t.Errorf("duty scale max = %v, want 100", dutyMax)
	}

	// Count the setScale calls on every scope from here on.
	evalPlot(t, p, `(() => { window.__scales = []; for (let n = 1; n <= 4; n++) { const u = __plot.scopeFor(n), set = u.setScale.bind(u);
	  u.setScale = (k, o) => { window.__scales.push(n + ':' + k + ':' + o.min + ':' + o.max); return set(k, o); }; } return true; })()`, nil)

	// A sample far above the axis leaves both scales alone.
	b.publish(sampleEvent(20, false, f64(1), f64(arcticMax*3)))
	waitDrawn(t, p, 1)
	var after struct {
		RPM, Duty float64
		Calls     []string
	}
	evalPlot(t, p, `({RPM: __plot.scopeFor(1).scales.rpm.max, Duty: __plot.scopeFor(1).scales.duty.max, Calls: window.__scales})`, &after)
	if after.RPM != arcticMax || after.Duty != 100 || len(after.Calls) != 0 {
		t.Errorf("after an rpm sample of %v: rpm max %v, duty max %v, setScale calls %v; want %v, 100 and none (nothing autoscales)", arcticMax*3, after.RPM, after.Duty, after.Calls, arcticMax)
	}

	// A fan event for port 1 changes its scale, once.
	b.Emit(events.Event{K: "fan", Port: 1, V: noctua}, func(st *control.Status) { st.Ports[0].Fan = noctua })
	waitPlot(t, p, "port 1 scaled to the Noctua", fmt.Sprintf(`__plot.scopeFor(1).scales.rpm.max === %v`, noctuaMax), 10*time.Second)
	b.publish(sampleEvent(20.1, false, f64(0.5), f64(900)))
	waitDrawn(t, p, 1)
	var calls []string
	evalPlot(t, p, `window.__scales`, &calls)
	want := fmt.Sprintf("1:rpm:0:%v", noctuaMax)
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("setScale calls after the fan swap = %v, want exactly [%s]", calls, want)
	}
	var others []float64
	evalPlot(t, p, `[2, 3, 4].map((n) => __plot.scopeFor(n).scales.rpm.max)`, &others)
	for i, m := range others {
		if m != arcticMax {
			t.Errorf("port %d rpm max = %v after port 1's swap, want %v", i+2, m, arcticMax)
		}
	}
}

// fanMax is the max_rpm of a library fan.
func fanMax(t *testing.T, id string) float64 {
	t.Helper()
	prof, err := fanlib.Load(id)
	if err != nil {
		t.Fatalf("fanlib.Load(%s): %v", id, err)
	}
	return prof.MaxRPM
}

// TestHistoryJoin: with the history answer held, live samples arrive whose seq lies below, at and
// above the history's seq; after the answer every sim time is in the ring exactly once and none is
// missing. A reconnect then replaces the rings with the new history instead of merging.
func TestHistoryJoin(t *testing.T) {
	b := newBench(t)
	// Samples 1..7 exist before the page connects: the snapshot is at seq 7.
	var evs []events.Event
	at := func(i int) float64 { return float64(i) }
	for i := 1; i <= 7; i++ {
		ev := sampleEvent(at(i), false, f64(0.5), f64(100*float64(i)))
		ev.Seq = b.publish(ev)
		evs = append(evs, ev)
	}
	b.HoldHistory()
	// The history answers with seq N = 10: samples 1..10.
	const n = 10
	b.SetHistory(func(r control.HistoryRequest) control.HistoryResult {
		return control.HistoryResult{Seq: evs[n-1].Seq, T: evs[n-1].T, WindowS: r.WindowS, Samples: append([]events.Event{}, evs[:n]...)}
	})
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	importPlot(t, p)
	b.waitCalls(t, "History", 1, 10*time.Second)

	// Live samples N-2 .. N+5 (seq 8..15) arrive while the history is held.
	for i := 8; i <= 15; i++ {
		ev := sampleEvent(at(i), false, f64(0.5), f64(100*float64(i)))
		ev.Seq = b.publish(ev)
		evs = append(evs, ev)
	}
	// A frame after the samples that the page applies at once: when it shows, the samples were received.
	b.Emit(events.Event{K: "env", ID: "temperature", V: 31.5}, func(st *control.Status) { st.Env.Temperature = 31.5 })
	waitStore(t, p, "env.temperature", 31.5)
	var held int
	evalPlot(t, p, `__plot.ringFor(1).xs.length`, &held)
	if held != 0 {
		t.Fatalf("the ring holds %d points while the history is held, want 0: samples must be held back", held)
	}

	b.ReleaseHistory()
	waitPlot(t, p, "15 points", `__plot.ringFor(1).xs.length >= 15`, 10*time.Second)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for port := 1; port <= 4; port++ {
		xs := ringXs(t, p, port)
		if len(xs) != 15 {
			t.Fatalf("port %d ring = %v, want sim times 1..15 once each", port, xs)
		}
		for i, x := range xs {
			if x != float64(i+1) {
				t.Errorf("port %d ring[%d] = %v, want %d", port, i, x, i+1)
			}
		}
	}
	var rpm []float64
	evalPlot(t, p, `__plot.ringFor(1).rpm`, &rpm)
	for i, v := range rpm {
		if v != 100*float64(i+1) {
			t.Errorf("rpm[%d] = %v, want %v", i, v, 100*float64(i+1))
		}
	}

	// A reconnect: the new history is a different epoch; the rings take it as it is.
	b.SetHistory(func(r control.HistoryRequest) control.HistoryResult {
		var samples []events.Event
		for i := 0; i < 3; i++ {
			ev := sampleEvent(100+float64(i), false, f64(0.2), f64(50))
			ev.Seq = 1
			samples = append(samples, ev)
		}
		return control.HistoryResult{Seq: b.broker.Seq(), T: samples[2].T, WindowS: r.WindowS, Samples: samples}
	})
	b.DropStreams()
	b.waitCalls(t, "History", 2, 10*time.Second)
	waitPlot(t, p, "the rings replaced", `__plot.ringFor(1).xs.length === 3`, 10*time.Second)
	if got := fmt.Sprint(ringXs(t, p, 1)); got != "[100 101 102]" {
		t.Errorf("ring after the reconnect = %s, want [100 101 102] (replaced, not merged)", got)
	}
}

// TestEmptyScope: with no samples every scope shows the heading and its port's body copy; while the
// link is CONNECTING the body is the connecting copy; a sample for one port hides only that port's
// text, and a sample whose values are all null hides nothing.
func TestEmptyScope(t *testing.T) {
	const head = "WAITING FOR FIRST SAMPLE"
	const connecting = "Connecting to the simulator. This screen fills when the first snapshot arrives."
	waiting := func(n int) string {
		return fmt.Sprintf("The simulator has not reported port %d yet. The trace starts with the first sample; nothing to do.", n)
	}
	type empty struct {
		Head, Body string
		Shown      bool
	}
	read := func(p *cdptest.Page) []empty {
		var out []empty
		evalPlot(t, p, `[...document.querySelectorAll('#ports .scope')].map((s) => ({Head: s.querySelector('.empty-head').textContent,
		  Body: s.querySelector('.empty-body').textContent, Shown: s.querySelector('.empty').getClientRects().length > 0}))`, &out)
		return out
	}

	b := newBench(t)
	b.HoldSnapshot()
	base := newSite(t, b)
	p := openPage(t, base)
	b.waitCalls(t, "Watch", 1, 10*time.Second)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, p, "#link"); got != "CONNECTING" {
		t.Fatalf("LINK = %q, want CONNECTING", got)
	}
	got := read(p)
	if len(got) != 4 {
		t.Fatalf("%d scopes while connecting, want 4", len(got))
	}
	for i, e := range got {
		if e.Head != head || e.Body != connecting || !e.Shown {
			t.Errorf("scope %d while CONNECTING = %+v, want the heading, the connecting body, shown", i+1, e)
		}
	}

	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	importPlot(t, p)
	waitPlot(t, p, "the waiting copy", `document.querySelector('#ports .scope .empty-body').textContent.startsWith('The simulator has not')`, 5*time.Second)
	for i, e := range read(p) {
		if e.Head != head || e.Body != waiting(i+1) || !e.Shown {
			t.Errorf("scope %d without samples = %+v, want the heading, %q, shown", i+1, e, waiting(i+1))
		}
	}

	// A sample of only null values (firmware off the bus) is not a first sample.
	b.publish(events.Event{T: us(30), K: events.SampleKind, V: []events.Sample{{Port: 3}}})
	// One sample for port 2 only.
	b.publish(events.Event{T: us(30.1), K: events.SampleKind, V: []events.Sample{{Port: 2, Duty: f64(0.4), RPM: f64(800)}}})
	waitPlot(t, p, "port 2 hidden", `document.querySelectorAll('#ports .scope .empty')[1].getClientRects().length === 0`, 10*time.Second)
	for i, e := range read(p) {
		wantShown := i != 1
		if e.Shown != wantShown {
			t.Errorf("scope %d shown = %v after one sample for port 2 (and a null sample for port 3), want %v", i+1, e.Shown, wantShown)
		}
	}
}

// TestHiddenAppend: samples that arrive while the page cannot draw (a hidden tab runs no animation
// frames) are all kept; the scope shows them in the next frame.
func TestHiddenAppend(t *testing.T) {
	b := newBench(t)
	p := plotPage(t, b)
	waitPlot(t, p, "scope 1", `__plot.scopeFor(1) !== null`, 10*time.Second)
	evalPlot(t, p, `(() => { window.__frames = []; window.__raf = window.requestAnimationFrame;
	  window.requestAnimationFrame = (cb) => { window.__frames.push(cb); return window.__frames.length; }; return true; })()`, nil)
	// A frame requested before the stub is installed may still be pending; let it run first.
	if err := sysClock.Sleep(within(t, 5*time.Second), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		b.publish(sampleEvent(40+0.1*float64(i), false, f64(0.5), f64(900)))
	}
	waitPlot(t, p, "30 points in the ring", `__plot.ringFor(1).xs.length >= 30`, 10*time.Second)
	var drawn float64
	evalPlot(t, p, `__plot.scopeFor(1).scales.x.max`, &drawn)
	if drawn > 41 {
		t.Fatalf("scope 1 was drawn up to %v s although no animation frame ran", drawn)
	}
	evalPlot(t, p, `(() => { window.requestAnimationFrame = window.__raf; const q = window.__frames.splice(0);
	  document.dispatchEvent(new Event('visibilitychange')); q.forEach((cb) => window.__raf(cb)); return q.length; })()`, nil)
	waitDrawn(t, p, 1)
}

// TestNoCSPViolations: four live scopes run for 3 s under the page's Content-Security-Policy with an
// empty console (Research A6: uPlot sets its styles through the CSSOM).
func TestNoCSPViolations(t *testing.T) {
	b := newBench(t)
	p := plotPage(t, b)
	waitPlot(t, p, "four scopes", `[1, 2, 3, 4].every((n) => __plot.scopeFor(n) !== null)`, 10*time.Second)
	for i := 0; i < 30; i++ {
		b.publish(sampleEvent(50+0.1*float64(i), false, f64(0.4), f64(float64(800+10*i))))
		if err := sysClock.Sleep(within(t, 5*time.Second), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	for n, c := range map[int][3]int{1: yellow, 2: green, 3: blue, 4: pink} {
		waitDrawn(t, p, n)
		if !hasColour(t, p, n, 51, c) {
			t.Errorf("scope %d shows no trace in its channel colour", n)
		}
	}
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		raw, _ := json.Marshal(errs)
		t.Errorf("console errors with four live scopes: %s", raw)
	}
}

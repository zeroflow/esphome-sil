//go:build sil_ui

package uitest

import (
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// staleLook is what the page shows of the stale state.
type staleLook struct {
	Stale      bool      `json:"stale"`
	LinkColor  string    `json:"linkColor"`
	RPMText    string    `json:"rpmText"`
	ReadoutOp  []string  `json:"readoutOp"`
	LedsOp     string    `json:"ledsOp"`
	LensFilter []string  `json:"lensFilter"`
	PlotOp     []string  `json:"plotOp"`
	Enabled    []string  `json:"enabled"`
	Draws      int       `json:"draws"`
	Opacity    []float64 `json:"-"`
}

const staleJS = `(() => {
  const cs = (e) => getComputedStyle(e);
  return {
    stale: document.body.classList.contains('stale'),
    linkColor: cs(document.getElementById('link')).color,
    rpmText: document.querySelector('[data-src="ports.0.rpm"]').textContent,
    readoutOp: [...document.querySelectorAll('.readout .value')].map((e) => cs(e).opacity),
    ledsOp: cs(document.getElementById('leds')).opacity,
    lensFilter: [...document.querySelectorAll('#leds .lens')].map((e) => cs(e).filter),
    plotOp: [...document.querySelectorAll('.scope .plot')].map((e) => cs(e).opacity),
    enabled: [...document.querySelectorAll('select.fan, #strip input, #strip button')].filter((e) => !e.disabled).map((e) => e.id || e.className || e.tagName),
    draws: window.__draws || 0,
  };
})()`

func readStale(t *testing.T, p *cdptest.Page) staleLook {
	t.Helper()
	var got staleLook
	evalOn(t, p, staleJS, &got)
	return got
}

// allEqual reports whether every element of list is want.
func allEqual(list []string, want string) bool {
	for _, s := range list {
		if s != want {
			return false
		}
	}
	return len(list) > 0
}

// TestStaleAndReconnect (D-10, UIC-09, UIC-13, UIC-18, UIC-22, UIC-26, UIC-27): a FIRMWARE loss is not
// a stale page; a lost WebSocket is: values stay (no fake zeros) at 0.4 opacity, LED lenses go grey,
// the plots freeze, every control is disabled and LINK reads DISCONNECTED, RETRYING in red. The page
// reconnects by itself and a fresh snapshot and history replace everything.
func TestStaleAndReconnect(t *testing.T) {
	b, p := liveWithCaps(t)
	if err := p.WaitFor(within(t, 10*time.Second), `[...document.querySelectorAll('#ports select.fan')].every((s) => !s.disabled)`); err != nil {
		t.Fatalf("the fan selects never enabled: %v", err)
	}
	evalOn(t, p, `(window.__plot = await import('/js/plot.js'), true)`, nil)
	waitText(t, p, `[data-src="ports.0.rpm"]`, "1100", 5*time.Second)

	// A firmware loss while the link is fine changes the FIRMWARE cell and nothing else.
	b.Emit(events.Event{K: "firmware", V: false}, func(st *control.Status) { st.Firmware.BusConnected = false })
	waitText(t, p, `[data-src="firmware.bus_connected"]`, "DISCONNECTED", 5*time.Second)
	fw := readStale(t, p)
	if fw.Stale || !allEqual(fw.ReadoutOp, "1") || len(fw.Enabled) < 8 || textOf(t, p, "#link") != "LIVE" {
		t.Errorf("with the firmware disconnected the page looks stale: %+v (LINK %q)", fw, textOf(t, p, "#link"))
	}
	b.Emit(events.Event{K: "firmware", V: true}, func(st *control.Status) { st.Firmware.BusConnected = true })
	waitText(t, p, `[data-src="firmware.bus_connected"]`, "CONNECTED", 5*time.Second)

	// Count the redraws of scope 1 from here on.
	evalOn(t, p, `(() => { window.__draws = 0; const u = __plot.scopeFor(1), set = u.setData.bind(u);
	  u.setData = (d, r) => { window.__draws++; return set(d, r); }; return true; })()`, nil)

	// The WebSocket is lost; the next snapshot is held, so the page stays stale long enough to look.
	b.Mutate(func(st *control.Status) { st.Ports[0].RPM = 900 })
	b.HoldSnapshot()
	b.DropStreams()
	waitText(t, p, "#link", "DISCONNECTED, RETRYING", 5*time.Second)
	b.waitCalls(t, "Watch", 2, 10*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `document.body.classList.contains('stale')`); err != nil {
		t.Fatalf("body never got the stale class: %v", err)
	}
	st := readStale(t, p)
	if st.RPMText != "1100" {
		t.Errorf("RPM while stale = %q, want the last value 1100 (no fake zero)", st.RPMText)
	}
	if st.LinkColor != "rgb(255, 77, 77)" {
		t.Errorf("LINK colour while stale = %s, want red #ff4d4d", st.LinkColor)
	}
	if !allEqual(st.ReadoutOp, "0.4") || st.LedsOp != "0.4" || !allEqual(st.PlotOp, "0.4") || len(st.PlotOp) != 4 {
		t.Errorf("opacity while stale: readouts %v, LEDs %s, plots %v, want 0.4 everywhere", st.ReadoutOp, st.LedsOp, st.PlotOp)
	}
	if !allEqual(st.LensFilter, "grayscale(1)") || len(st.LensFilter) != 5 {
		t.Errorf("lens filters while stale = %v, want grayscale(1) on all five", st.LensFilter)
	}
	if len(st.Enabled) != 0 {
		t.Errorf("controls still enabled while stale: %v", st.Enabled)
	}
	if got := textOf(t, p, `[data-src="firmware.bus_connected"]`); got != "CONNECTED" {
		t.Errorf("FIRMWARE while stale = %q, want its own last report CONNECTED (never merged with stale)", got)
	}

	// The plots are frozen: a sample that reaches the rings while stale is not drawn.
	evalOn(t, p, `(__plot.pushSample({t: 40000000, k: 'sample', seq: 99999, v: [{port: 1, duty: 0.5, rpm: 700}]}), true)`, nil)
	settle(t, 400*time.Millisecond)
	if d := readStale(t, p).Draws; d != 0 {
		t.Errorf("scope 1 was redrawn %d times while stale, want 0", d)
	}

	// The reconnect: a fresh snapshot and a fresh history replace everything.
	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	waitText(t, p, `[data-src="ports.0.rpm"]`, "900", 5*time.Second)
	b.waitCalls(t, "History", 2, 5*time.Second)
	if err := p.WaitFor(within(t, 5*time.Second), `!document.body.classList.contains('stale') && __draws > 0`); err != nil {
		t.Fatalf("the stale look stayed or the plot never redrew after the reconnect: %v (%+v)", err, readStale(t, p))
	}
	st = readStale(t, p)
	if !allEqual(st.ReadoutOp, "1") || st.LedsOp != "1" || !allEqual(st.PlotOp, "1") || !allEqual(st.LensFilter, "none") || len(st.Enabled) < 8 {
		t.Errorf("after the reconnect: %+v, want full opacity, no filter and the controls enabled", st)
	}
	if st.LinkColor != "rgb(230, 232, 235)" {
		t.Errorf("LINK colour after the reconnect = %s, want primary text", st.LinkColor)
	}
}

// TestResyncOnSimRestart (D-10): a sim restarted event makes the page open a new stream and ask for a
// new history, without reloading.
func TestResyncOnSimRestart(t *testing.T) {
	b, p := liveWithCaps(t)
	evalOn(t, p, `(window.__marker = 'same document', true)`, nil)
	b.waitCalls(t, "History", 1, 5*time.Second)

	b.Mutate(func(st *control.Status) { st.Ports[1].RPM = 2500 })
	b.Emit(events.Event{K: "sim", V: "restarted"}, nil)
	b.waitCalls(t, "Watch", 2, 10*time.Second)
	b.waitCalls(t, "History", 2, 10*time.Second)
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	waitText(t, p, `[data-src="ports.1.rpm"]`, "2500", 5*time.Second)
	var marker string
	evalOn(t, p, `window.__marker || ''`, &marker)
	if marker != "same document" {
		t.Errorf("the window marker is %q: the page was reloaded", marker)
	}
}

// TestErrorFrame (D-17): a final {"error": ...} frame on /ws (for instance too_many_streams) shows its
// message verbatim in the message line, and the page reconnects with its normal backoff.
func TestErrorFrame(t *testing.T) {
	b := newBench(t)
	const msg = `too many <i>event</i> streams & pages (limit 8)`
	b.FailWatchAfterSnapshot(apperr.Failed("too_many_streams", msg, nil))
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#message", msg, 10*time.Second)
	b.waitCalls(t, "Watch", 2, 10*time.Second)
	waitText(t, p, "#link", "LIVE", 10*time.Second)
}

// TestFailedInstanceSnapshotRenders (CR-01): the snapshot of a failed instance carries a top-level
// "error" key (Status.Error is promoted by the embedded struct) and "k": "snapshot". It is not the
// final {"error": ...} frame of a failed stream, which has no k: the page must render the state, show
// LIVE and keep the one stream, not reconnect in a loop.
func TestFailedInstanceSnapshotRenders(t *testing.T) {
	b := newBench(t)
	b.Mutate(func(st *control.Status) {
		st.Phase = control.PhaseFailed
		st.Error = apperr.Infra("firmware_crash_loop", "the firmware crashed 3 times in 10 s", nil)
		st.Ports[0].RPM = 1234
	})
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	waitText(t, p, `[data-src="ports.0.rpm"]`, "1234", 5*time.Second)
	// A reconnect loop would open a second stream within the first backoff (500 ms).
	settle(t, 1500*time.Millisecond)
	if n := len(b.callsOf("Watch")); n != 1 {
		t.Errorf("Watch was called %d times, want 1: the snapshot of a failed instance made the page reconnect", n)
	}
	if got := textOf(t, p, "#link"); got != "LIVE" {
		t.Errorf("LINK = %q, want LIVE", got)
	}
	if got := textOf(t, p, "#message"); got != "" {
		t.Errorf("message line = %q, want it empty: the instance's failure is not a stream error", got)
	}
}

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// tachPlug is a literal schema 2 fan that holds 1100 rpm at any duty >= 0.05 and has a pulse tach
// of ppr 2 (noise 0): its sim-bus rpm frames are whole pulses per 50 ms frame.
func tachPlug(port int) PortConfig {
	return PortConfig{Port: port, FanID: "tach-literal", Fan: model.Params{
		Name: "tach", Schema: 2, MinDuty: 0.05, StartDuty: 0.1, BelowMin: "stop",
		TauUp: 0.2, TauDown: 0.2, CoastTau: 0.2,
		Curve:  model.Curves{Up: model.Curve{{Duty: 0.05, RPM: 1100}, {Duty: 1, RPM: 1100}}},
		MinRPM: 1100, MaxRPM: 1100,
		Tach: model.TachParams{PPR: 2},
	}}
}

// rpmPoint is the sim time and value of one rpm frame.
type rpmPoint struct {
	t time.Duration
	v float64
}

// rpmSeries is every in frame of channel ch that fw received, in arrival order.
func rpmSeries(fw *firmware, ch string) []rpmPoint {
	var out []rpmPoint
	for _, f := range fw.rpmFrames(ch) {
		v, _ := f.GetF32("v")
		us, _ := f.GetInt("t")
		out = append(out, rpmPoint{time.Duration(us) * time.Microsecond, float64(v)})
	}
	return out
}

// TestEngineTachFrames: a fan with a pulse tach sends whole-pulse rpm frames over the unchanged
// sim-bus protocol (multiples of 600 rpm per 50 ms frame at ppr 2), their time-weighted mean is
// the shaft rpm, the hello in-snapshot carries the last frame value without draining pulses, and
// the shaft in the snapshot stays exact.
func TestEngineTachFrames(t *testing.T) {
	h := newHarness(t)
	if err := applyChange(t, h.eng, Change{Fans: map[int]PortConfig{1: tachPlug(1)}}); err != nil {
		t.Fatalf("replug port 1: %v", err)
	}
	h.setDuty(1, 0.5)
	h.advanceTo(3 * time.Second) // settle
	t0 := h.eng.Snapshot().SimTime
	h.advanceTo(10 * time.Second)
	t1 := h.eng.Snapshot().SimTime

	// A second connection replaces the first; its hello is answered with the in-snapshot, and the
	// old connection receives bye after every frame sent before, so its recording is complete.
	fw2 := h.dial()
	fw2.session(fullManifest())
	fw2.waitKinds(11)
	waitUntil(t, "the old connection to receive bye", func() bool {
		ks := h.fw.kinds()
		return len(ks) > 0 && ks[len(ks)-1] == "bye"
	})

	series := rpmSeries(h.fw, "port1.rpm")
	var window []rpmPoint
	held := 0.0
	for _, p := range series {
		if p.t <= t0 {
			held = p.v
			continue
		}
		window = append(window, p)
	}
	if len(window) < 20 {
		t.Fatalf("only %d port1.rpm frames in the 10 s window; the test would prove nothing", len(window))
	}
	area, at := 0.0, t0
	for _, p := range window {
		if p.t > t1 {
			break
		}
		area += held * (p.t - at).Seconds()
		held, at = p.v, p.t
		if q := p.v / 600; math.Abs(q-math.Round(q)) > 1e-3/600 {
			t.Fatalf("frame at %v = %v rpm, want a whole multiple of 600", p.t, p.v)
		}
	}
	area += held * (t1 - at).Seconds()
	if mean := area / (t1 - t0).Seconds(); math.Abs(mean-1100) > 15 {
		t.Fatalf("time-weighted mean of the frames over 10 s = %v, want 1100 +- 15", mean)
	}

	// The in-snapshot of the second connection is the last frame value, not a drained count.
	last := window[len(window)-1].v
	snap := fw2.received()[1]
	if ch, _ := snap.GetStr("ch"); ch != "port1.rpm" {
		t.Fatalf("first snapshot frame is %s, want port1.rpm", snap)
	}
	if v, _ := snap.GetF32("v"); float64(v) != last {
		t.Fatalf("snapshot port1.rpm = %v, want the last frame value %v", v, last)
	}
	h.advanceTo(time.Second)
	waitUntil(t, "a steady frame on the second connection", func() bool { return len(rpmSeries(fw2, "port1.rpm")) >= 2 })
	next := rpmSeries(fw2, "port1.rpm")
	for _, p := range next {
		if q := p.v / 600; math.Abs(q-math.Round(q)) > 1e-3/600 {
			t.Fatalf("frame on the second connection = %v rpm, want a whole multiple of 600", p.v)
		}
	}
	if got := h.eng.Snapshot().Ports[0].RPM; math.Abs(got-1100) > 0.01 {
		t.Fatalf("Snapshot().Ports[0].RPM = %v, want the shaft 1100 +- 0.01", got)
	}
}

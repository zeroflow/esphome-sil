package engine

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// tachVal is the value of a tach event: ok false means the event carries null (a nil *float64).
func tachVal(t *testing.T, ev events.Event) (v float64, ok bool) {
	t.Helper()
	p, isPtr := ev.V.(*float64)
	if !isPtr {
		t.Fatalf("tach event V is %T, want *float64 (nil encodes as \"v\":null)", ev.V)
	}
	if p == nil {
		return 0, false
	}
	return *p, true
}

// tachOf is the tach events of one port.
func tachOf(rec *recorder, port int) []events.Event {
	var out []events.Event
	for _, ev := range rec.of("tach") {
		if ev.Port == port {
			out = append(out, ev)
		}
	}
	return out
}

// lastIndex is the index of the last event of kind k on port in evs, or -1.
func lastIndex(evs []events.Event, k string, port int) int {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].K == k && evs[i].Port == port {
			return i
		}
	}
	return -1
}

// newSeededHarness is newHarness with Config.Seed set.
func newSeededHarness(t *testing.T, seed uint64) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	busPath := filepath.Join(dir, "bus.sock")
	fan := fanParams(t)
	var ports []PortConfig
	for n := 1; n <= 4; n++ {
		ports = append(ports, PortConfig{Port: n, FanID: fanlib.DefaultFan, Fan: fan})
	}
	clk := clock.NewManual()
	rec := &recorder{}
	eng := New(Config{Clock: clk, BusPath: busPath, Ports: ports, Emit: rec.emit, Seed: seed})
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- eng.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		tm := clock.NewReal().NewTimer(guard)
		defer tm.Stop()
		select {
		case <-runErr:
		case <-tm.C():
			t.Error("Run did not return after cancel")
		}
	})
	select {
	case <-eng.Ready():
	case err := <-runErr:
		t.Fatalf("engine stopped before it was ready: %v", err)
	}
	clk.BlockUntil(1)
	h := &harness{t: t, eng: eng, clk: clk, busPath: busPath, cancel: cancel, rec: rec}
	h.fw = h.dial()
	h.fw.session(fullManifest())
	waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
	return h
}

// TestEngineTachEvents: a tach port emits a null tach event after the fan event of a swap, then a
// reading (30 x pulses) once per sim second at most; the snapshot carries the last reading next to
// the shaft rpm.
func TestEngineTachEvents(t *testing.T) {
	h := newHarness(t)
	h.setDuty(1, 0.5)
	if err := applyChange(t, h.eng, Change{Fans: map[int]PortConfig{1: tachPlug(1)}}); err != nil {
		t.Fatalf("replug port 1: %v", err)
	}
	h.advanceTo(8 * time.Second)

	all := h.rec.all()
	fanAt := lastIndex(all, "fan", 1)
	if fanAt < 0 {
		t.Fatal("no fan event for port 1")
	}
	var after []events.Event
	for _, ev := range all[fanAt+1:] {
		if ev.K == "tach" && ev.Port == 1 {
			after = append(after, ev)
		}
	}
	if len(after) < 2 {
		t.Fatalf("%d tach events for port 1 after the swap, want a null then readings: %+v", len(after), after)
	}
	if _, ok := tachVal(t, after[0]); ok {
		t.Fatalf("first tach event after the swap = %v, want null", after[0].V)
	}
	if after[0].Seq <= all[fanAt].Seq {
		t.Fatalf("tach null (seq %d) is not after the fan event (seq %d)", after[0].Seq, all[fanAt].Seq)
	}
	for i := 1; i < len(after); i++ {
		if d := after[i].T - after[i-1].T; d < time.Second.Microseconds() {
			t.Fatalf("tach events %d and %d are %d us apart, want at least 1 s", i-1, i, d)
		}
		v, ok := tachVal(t, after[i])
		if !ok {
			t.Fatalf("tach event %d is null after a reading", i)
		}
		if q := v / 30; math.Abs(q-math.Round(q)) > 1e-4 {
			t.Fatalf("tach reading %v is not a whole multiple of 30 rpm (ppr 2, 1 s gate)", v)
		}
	}
	last, ok := tachVal(t, after[len(after)-1])
	if !ok || math.Abs(last-1100) > 30 {
		t.Fatalf("settled tach reading = %v (ok %v), want 1100 within 30", last, ok)
	}
	p := h.eng.Snapshot().Ports[0]
	if p.TachRPM == nil || float32(*p.TachRPM) != float32(last) {
		t.Fatalf("Snapshot().Ports[0].TachRPM = %v, want the last event value %v", p.TachRPM, last)
	}
	if math.Abs(p.RPM-1100) > 0.01 {
		t.Fatalf("Snapshot().Ports[0].RPM = %v, want the shaft 1100 within 0.01", p.RPM)
	}
}

// TestEngineSampleCarriesTach: the plot samples carry the tach reading per port, null in the first
// second of a plant and while the firmware is away.
func TestEngineSampleCarriesTach(t *testing.T) {
	h := newHarness(t)
	h.setDuty(1, 0.5)
	if err := applyChange(t, h.eng, Change{Fans: map[int]PortConfig{1: tachPlug(1)}}); err != nil {
		t.Fatalf("replug port 1: %v", err)
	}
	h.advanceTo(4 * time.Second)
	samples := samplesOf(t, h.rec.of(events.SampleKind))
	if len(samples) != 40 {
		t.Fatalf("%d samples after 4 s, want 40", len(samples))
	}
	for i, s := range samples {
		early, late := i < 9, i > 10 // sample i is at (i+1) x 100 ms; 1.0 s is the boundary
		for _, port := range []int{0, 1} {
			if early && s[port].Tach != nil {
				t.Fatalf("sample %d port %d tach = %v in the first second, want null", i, port+1, *s[port].Tach)
			}
			if late && s[port].Tach == nil {
				t.Fatalf("sample %d port %d tach is null after the first second", i, port+1)
			}
		}
	}
	if v := samples[39][0].Tach; v == nil || math.Abs(*v-1100) > 60 {
		t.Fatalf("tach port 1 at 4 s = %v, want about 1100", v)
	}

	h.setDuty(2, 0.5)
	h.advanceTo(15 * time.Second)
	s := samplesOf(t, h.rec.of(events.SampleKind))
	end := s[len(s)-1][1]
	if end.Tach == nil || end.RPM == nil || *end.RPM < 100 {
		t.Fatalf("port 2 sample = %+v, want a spinning fan with a tach reading", end)
	}
	if relDiff(*end.Tach, *end.RPM) > 1e-4 {
		t.Fatalf("ideal tach %v != shaft %v in the same sample (rel %g)", *end.Tach, *end.RPM, relDiff(*end.Tach, *end.RPM))
	}
}

// TestEngineIdealTachEvents: an ideal-tach port (no [tach]) emits tach events too: null at the
// start, numbers at most once per sim second, null again after a swap and a number a second later.
func TestEngineIdealTachEvents(t *testing.T) {
	h := newHarness(t)
	ev0 := tachOf(h.rec, 2)
	if len(ev0) != 1 {
		t.Fatalf("%d tach events for port 2 at the start, want one null", len(ev0))
	}
	if _, ok := tachVal(t, ev0[0]); ok {
		t.Fatalf("first tach event = %v, want null", ev0[0].V)
	}
	h.advanceTo(500 * time.Millisecond)
	if p := h.eng.Snapshot().Ports[1]; p.TachRPM != nil {
		t.Fatalf("TachRPM at 0.5 s = %v, want nil (no gate completed yet)", *p.TachRPM)
	}
	h.advanceTo(700 * time.Millisecond) // 1.2 s
	if p := h.eng.Snapshot().Ports[1]; p.TachRPM == nil || *p.TachRPM != 0 {
		t.Fatalf("TachRPM at 1.2 s = %v, want 0 (a fan at rest reads 0, not null)", p.TachRPM)
	}
	h.setDuty(2, 0.5)
	h.advanceTo(6 * time.Second)
	evs := tachOf(h.rec, 2)
	if len(evs) < 4 {
		t.Fatalf("%d tach events for port 2 over a spin-up, want about one per second: %+v", len(evs), evs)
	}
	for i := 1; i < len(evs); i++ {
		if d := evs[i].T - evs[i-1].T; d < time.Second.Microseconds() {
			t.Fatalf("tach events %d and %d are %d us apart, want at least 1 s", i-1, i, d)
		}
		if _, ok := tachVal(t, evs[i]); !ok {
			t.Fatalf("tach event %d is null; an ideal reading never goes back to null while the plant lives", i)
		}
	}
	if p := h.eng.Snapshot().Ports[1]; p.TachRPM == nil {
		t.Fatal("TachRPM nil after 6 s")
	}

	n := len(evs)
	replug(t, h, 2, fanlib.DefaultFan)
	h.advanceTo(1500 * time.Millisecond)
	evs = tachOf(h.rec, 2)[n:]
	if len(evs) < 2 {
		t.Fatalf("%d tach events after the swap, want a null and a number: %+v", len(evs), evs)
	}
	if _, ok := tachVal(t, evs[0]); ok {
		t.Fatalf("tach event right after the swap = %v, want null", evs[0].V)
	}
	if _, ok := tachVal(t, evs[1]); !ok {
		t.Fatalf("second tach event after the swap = %v, want a number", evs[1].V)
	}
	if d := evs[1].T - evs[0].T; d < time.Second.Microseconds() || d > 1100*time.Millisecond.Microseconds() {
		t.Fatalf("the reading came %d us after the swap, want about 1 s", d)
	}
}

// TestEngineSameSeedSameTachTrace: the same seed, fan and duty script on the Manual clock give the
// same tach events; another seed gives another trace when noise_rpm > 0.
func TestEngineSameSeedSameTachTrace(t *testing.T) {
	type pt struct {
		t int64
		v float64
		n bool
	}
	run := func(seed uint64) []pt {
		h := newSeededHarness(t, seed)
		pc := tachPlug(1)
		pc.Fan.Tach.NoiseRPM = 10
		if err := applyChange(t, h.eng, Change{Fans: map[int]PortConfig{1: pc}}); err != nil {
			t.Fatalf("replug: %v", err)
		}
		h.setDuty(1, 0.5)
		h.advanceTo(8 * time.Second)
		h.setDuty(1, 0.2)
		h.advanceTo(7 * time.Second)
		h.setDuty(1, 0.7)
		h.advanceTo(5 * time.Second)
		var out []pt
		for _, ev := range tachOf(h.rec, 1) {
			v, ok := tachVal(t, ev)
			out = append(out, pt{ev.T, v, !ok})
		}
		return out
	}
	a, b, c := run(7), run(7), run(8)
	if len(a) < 15 {
		t.Fatalf("only %d tach events over 20 s, the test would prove nothing", len(a))
	}
	if len(a) != len(b) {
		t.Fatalf("same seed: %d vs %d events", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same seed, event %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
	same := len(a) == len(c)
	for i := 0; same && i < len(a); i++ {
		same = a[i] == c[i]
	}
	if same {
		t.Fatal("seed 7 and seed 8 gave the same tach trace with noise_rpm 10")
	}
}

// loadOf is the load events of one port, with their values.
func loadOf(t *testing.T, rec *recorder, port int) []events.Event {
	t.Helper()
	var out []events.Event
	for _, ev := range rec.of("load") {
		if ev.Port != port {
			continue
		}
		if _, ok := ev.V.(events.Load); !ok {
			t.Fatalf("load event V is %T, want events.Load", ev.V)
		}
		out = append(out, ev)
	}
	return out
}

// TestEngineLoadEvents: a fan with a datasheet shows airflow and power by the fan laws in load
// events (the swap first, then once per sim second on change) and in the snapshot; a fan without
// one shows null, never 0.
func TestEngineLoadEvents(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fans-v2"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t) // loads the embedded default fan, so the override comes after it
	t.Setenv(fanlib.EnvFanDir, dir)
	h.setDuty(1, 0.4)
	replug(t, h, 1, "load-test")
	h.advanceTo(10 * time.Second)

	air := func(rpm float64) float64 { return 100 * rpm / 1700 }
	pow := func(rpm float64) float64 { return 2 * math.Pow(rpm/1700, 3) }

	loads := loadOf(t, h.rec, 1)
	fanAt := lastIndex(h.rec.all(), "fan", 1)
	var after []events.Event
	for _, ev := range loads {
		if ev.Seq > h.rec.all()[fanAt].Seq {
			after = append(after, ev)
		}
	}
	if len(after) < 4 {
		t.Fatalf("%d load events after the swap over a 10 s spin-up, want the swap one and about one per second: %+v", len(after), after)
	}
	first := after[0].V.(events.Load)
	if first.AirflowM3h == nil || first.PowerW == nil || *first.AirflowM3h != 0 || *first.PowerW != 0 {
		t.Fatalf("load right after the swap = %+v, want airflow 0 and power 0 (values, since the fan has a datasheet)", first)
	}
	rpmAt := map[int64]float64{}
	for _, ev := range h.rec.of("rpm") {
		if ev.Port == 1 {
			rpmAt[ev.T] = ev.V.(float64)
		}
	}
	for i := 1; i < len(after); i++ {
		if i > 1 { // the first sampled event may follow the swap event closely
			if d := after[i].T - after[i-1].T; d < time.Second.Microseconds() {
				t.Fatalf("load events %d and %d are %d us apart, want at least 1 s", i-1, i, d)
			}
		}
		l := after[i].V.(events.Load)
		rpm, ok := rpmAt[after[i].T]
		if !ok {
			t.Fatalf("load event %d at %d us has no rpm event at the same time", i, after[i].T)
		}
		if l.AirflowM3h == nil || l.PowerW == nil {
			t.Fatalf("load event %d = %+v, want values", i, l)
		}
		if relDiff(*l.AirflowM3h, air(rpm)) > 1e-5 || relDiff(*l.PowerW, pow(rpm)) > 1e-5 {
			t.Fatalf("load event %d = %v m3/h %v W at rpm %v, want %v and %v", i, *l.AirflowM3h, *l.PowerW, rpm, air(rpm), pow(rpm))
		}
	}
	p := h.eng.Snapshot().Ports[0]
	if p.AirflowM3h == nil || p.PowerW == nil {
		t.Fatalf("snapshot airflow/power = %v / %v, want values for a fan with a datasheet", p.AirflowM3h, p.PowerW)
	}
	if relDiff(*p.AirflowM3h, air(p.RPM)) > 1e-9 || relDiff(*p.PowerW, pow(p.RPM)) > 1e-9 {
		t.Fatalf("snapshot airflow %v / power %v at shaft %v, want %v / %v", *p.AirflowM3h, *p.PowerW, p.RPM, air(p.RPM), pow(p.RPM))
	}

	// A fan without datasheet values: null, at the start and after a swap, and no further events.
	if p2 := loadOf(t, h.rec, 2); len(p2) != 1 {
		t.Fatalf("%d load events for the default fan on port 2, want only the one null at the start: %+v", len(p2), p2)
	} else if l := p2[0].V.(events.Load); l.AirflowM3h != nil || l.PowerW != nil {
		t.Fatalf("load of a fan without datasheet = %+v, want null for both", l)
	}
	if q := h.eng.Snapshot().Ports[1]; q.AirflowM3h != nil || q.PowerW != nil {
		t.Fatalf("snapshot of the default fan has airflow/power %v / %v, want nil", q.AirflowM3h, q.PowerW)
	}
	n := len(loadOf(t, h.rec, 1))
	replug(t, h, 1, "curve-test")
	sw := loadOf(t, h.rec, 1)
	if len(sw) != n+1 {
		t.Fatalf("%d load events after the swap to curve-test, want exactly one more", len(sw)-n)
	}
	if l := sw[n].V.(events.Load); l.AirflowM3h != nil || l.PowerW != nil {
		t.Fatalf("load after the swap to a fan without datasheet = %+v, want null for both", l)
	}
	if p := h.eng.Snapshot().Ports[0]; p.AirflowM3h != nil || p.PowerW != nil {
		t.Fatalf("snapshot after the swap has airflow/power %v / %v, want nil", p.AirflowM3h, p.PowerW)
	}
	h.advanceTo(5 * time.Second)
	if got := len(loadOf(t, h.rec, 1)); got != n+1 {
		t.Fatalf("%d load events while curve-test spins up, want none (null stays null)", got-n-1)
	}
}

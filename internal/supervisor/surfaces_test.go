package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
	"github.com/zeroflow/esphome-sil/internal/model"
)

// tachFanSup is watchSup with two ports: port 1 a literal pulse-tach fan (ppr 2, flat 1100 rpm),
// port 2 the default fan (schema 1, ideal tach, no datasheet).
func tachFanSup(t *testing.T) *supervisor {
	t.Helper()
	s := watchSup(t)
	def, err := fanlib.Load(fanlib.DefaultFan)
	if err != nil {
		t.Fatal(err)
	}
	s.inMu.Lock()
	s.fans = []engine.PortConfig{
		{Port: 1, FanID: "tach-literal", Fan: model.Params{
			Name: "tach", Schema: 2, MinDuty: 0.05, StartDuty: 0.1, BelowMin: "stop",
			TauUp: 0.2, TauDown: 0.2, CoastTau: 0.2,
			Curve:  model.Curves{Up: model.Curve{{Duty: 0.05, RPM: 1100}, {Duty: 1, RPM: 1100}}},
			MinRPM: 1100, MaxRPM: 1100,
			Tach: model.TachParams{PPR: 2},
		}},
		{Port: 2, FanID: fanlib.DefaultFan, Fan: def},
	}
	s.inMu.Unlock()
	s.stopEngine(s.slot)
	s.inMu.Lock()
	sl, err := s.startEngine()
	if err != nil {
		s.inMu.Unlock()
		t.Fatal(err)
	}
	s.slot = sl
	s.inMu.Unlock()
	t.Cleanup(func() { s.stopEngine(sl) })
	return s
}

// advanceSim moves the Manual clock by d and waits until the engine has handled the last tick.
func advanceSim(t *testing.T, s *supervisor, d time.Duration) {
	t.Helper()
	eng := s.currentEngine()
	target := eng.Snapshot().SimTime + d
	s.clk.(*clock.Manual).BlockUntil(1)
	s.clk.(*clock.Manual).Advance(d)
	tm := realClock.NewTimer(5 * time.Second)
	defer tm.Stop()
	for eng.Snapshot().SimTime != target {
		select {
		case <-tm.C():
			t.Fatalf("engine did not reach sim time %v", target)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// statusPorts is the ports array of the status JSON, decoded generically so that an absent key and
// a null differ.
func statusPorts(t *testing.T, s *supervisor) []map[string]any {
	t.Helper()
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Schema int              `json:"schema"`
		Ports  []map[string]any `json:"ports"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Schema != 1 {
		t.Fatalf("status schema %d, want 1 (the new fields are additive)", back.Schema)
	}
	if len(back.Ports) != 2 {
		t.Fatalf("%d ports in status, want 2", len(back.Ports))
	}
	return back.Ports
}

// wantKeyVal checks that key is present in m and equals want (nil = JSON null).
func wantKeyVal(t *testing.T, m map[string]any, key string, want any) {
	t.Helper()
	got, present := m[key]
	if !present {
		t.Fatalf("port %v has no %q key; it must always be present (null when unknown): %v", m["port"], key, m)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("port %v %s = %v, want %v", m["port"], key, got, want)
	}
}

// TestStatusCarriesTachAndLoad: status ports always carry tach_rpm, airflow_m3h and power_w (null
// when unknown, never absent, never a made-up 0), tach_rpm is null only until the first 1 s gate and
// a number from then on for a tach fan and an ideal-tach fan alike, and the tach event reaches a
// watcher.
func TestStatusCarriesTachAndLoad(t *testing.T) {
	s := tachFanSup(t)
	w := startWatch(t, s, "tach")
	if snap := w.next(t); snap["k"] != "snapshot" {
		t.Fatalf("first line %v, want the snapshot", snap)
	}

	for _, at := range []time.Duration{0, 500 * time.Millisecond} {
		if at > 0 {
			advanceSim(t, s, at)
		}
		for _, p := range statusPorts(t, s) {
			wantKeyVal(t, p, "tach_rpm", nil)
			wantKeyVal(t, p, "airflow_m3h", nil)
			wantKeyVal(t, p, "power_w", nil)
		}
	}

	advanceSim(t, s, 500*time.Millisecond) // 1 s of sim time: the first gate of both plants
	for _, p := range statusPorts(t, s) {
		wantKeyVal(t, p, "tach_rpm", float64(0)) // a fan at rest reads 0: a value, not null
		wantKeyVal(t, p, "airflow_m3h", nil)     // no datasheet: null, not 0
		wantKeyVal(t, p, "power_w", nil)
		wantKeyVal(t, p, "rpm", float64(0))
	}

	for _, port := range []float64{1, 2} {
		ev := w.next(t)
		if ev["k"] != "tach" || ev["port"] != port {
			t.Fatalf("line %v, want a tach event for port %v", ev, port)
		}
		if v, present := ev["v"]; !present || v != float64(0) {
			t.Fatalf("tach event %v, want v 0", ev)
		}
	}
}

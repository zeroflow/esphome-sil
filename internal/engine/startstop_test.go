package engine

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// TestEngineStartDelayAndCoast drives the startstop-test fixture (start.duty 0.10, delay 0.4 s,
// stop.duty 0.05, coast_tau_s 3, tau_down_s 0.8) from its file to the state events: the fan stays at
// exactly 0 rpm for the start delay, runs 400 ms after the duty event, stops at once below stop.duty
// and coasts with its own time constant.
func TestEngineStartDelayAndCoast(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fans-v2"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t) // loads the embedded default fan, so the override comes after it
	t.Setenv(fanlib.EnvFanDir, dir)
	replug(t, h, 1, "startstop-test")

	h.setDuty(1, 0.12)
	duties := h.rec.of("duty")
	if len(duties) == 0 {
		t.Fatal("no duty event after setDuty")
	}
	t0 := time.Duration(duties[len(duties)-1].T) * time.Microsecond

	h.advanceTo(390 * time.Millisecond)
	if p := h.eng.Snapshot().Ports[0]; p.RPM != 0 || p.State != "stopped" {
		t.Fatalf("390 ms after the duty event port 1 = %+v, want stopped at exactly 0 rpm", p)
	}
	h.advanceTo(110 * time.Millisecond)
	if p := h.eng.Snapshot().Ports[0]; p.RPM <= 0 || p.State != "running" {
		t.Fatalf("500 ms after the duty event port 1 = %+v, want running with rpm > 0", p)
	}
	var runningAt []time.Duration
	for _, ev := range h.rec.of("state") {
		if ev.Port == 1 && ev.V == "running" {
			runningAt = append(runningAt, time.Duration(ev.T)*time.Microsecond)
		}
	}
	if len(runningAt) != 1 {
		t.Fatalf("got %d running state events for port 1, want 1", len(runningAt))
	}
	if d := runningAt[0] - t0; d < 400*time.Millisecond || d > 410*time.Millisecond {
		t.Fatalf("running event %v after the duty event, want 400 ms (+ one 10 ms tick)", d)
	}

	h.advanceTo(10 * time.Second)
	if got := h.eng.Snapshot().Ports[0].RPM; math.Abs(got-440) > 1 {
		t.Fatalf("rpm after 10 s at duty 0.12 = %v, want near 440 (curve.up at 0.12)", got)
	}

	nStates := len(h.rec.of("state"))
	h.setDuty(1, 0.03)
	h.advanceTo(10 * time.Millisecond)
	if p := h.eng.Snapshot().Ports[0]; p.State != "stopped" {
		t.Fatalf("one tick below stop.duty port 1 = %+v, want stopped at once", p)
	}
	states := h.rec.of("state")
	if len(states) != nStates+1 || states[len(states)-1].V != "stopped" || states[len(states)-1].Port != 1 {
		t.Fatalf("state events after the stop: %+v, want exactly one new stopped event for port 1", states[nStates:])
	}
	r0 := h.eng.Snapshot().Ports[0].RPM
	h.advanceTo(time.Second)
	r1 := h.eng.Snapshot().Ports[0].RPM
	ratio := r1 / r0
	if want := math.Exp(-1.0 / 3); math.Abs(ratio-want)/want > 0.01 {
		t.Fatalf("rpm ratio after 1 s of coast = %v, want e^(-1/3) = %v (coast_tau_s), not e^(-1/0.8) = %v", ratio, want, math.Exp(-1/0.8))
	}
}

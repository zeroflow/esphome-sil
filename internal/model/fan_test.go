package model

import (
	"math"
	"slices"
	"testing"
	"time"
)

// arctic mirrors fans/arctic-p12-pwm-pst.toml (kept literal so the model
// tests do not depend on the fan library).
var arctic = Params{
	Name: "Arctic P12 PWM PST", MinDuty: 0.05, StartDuty: 0.10, BelowMin: "stop",
	MinRPM: 200, MaxRPM: 1800, TauUp: 1.0, TauDown: 2.5,
}

// holdFan never stops: below min_duty it holds min_rpm.
var holdFan = Params{
	Name: "hold", MinDuty: 0.20, StartDuty: 0.30, BelowMin: "hold",
	MinRPM: 300, MaxRPM: 1500, TauUp: 1.0, TauDown: 2.0,
}

// linear is the D-13 target at a running duty >= MinDuty.
func linear(p Params, duty float64) float64 {
	return p.MinRPM + (duty-p.MinDuty)/(1-p.MinDuty)*(p.MaxRPM-p.MinRPM)
}

func relDiff(a, b float64) float64 {
	if a == b {
		return 0
	}
	return math.Abs(a-b) / math.Max(math.Abs(a), math.Abs(b))
}

// settle steps s with duty for d in 10 ms steps.
func settle(p Params, s State, duty float64, d time.Duration) State {
	for t := time.Duration(0); t < d; t += 10 * time.Millisecond {
		s = Step(p, s, duty, 10*time.Millisecond)
	}
	return s
}

func TestInitialState(t *testing.T) {
	if s := Initial(arctic); s.Running || s.RPM != 0 {
		t.Errorf("Initial(stop fan) = %+v, want stopped at 0", s)
	}
	if s := Initial(holdFan); !s.Running || s.RPM != 0 {
		t.Errorf("Initial(hold fan) = %+v, want running at rpm 0", s)
	}
}

func TestStartThresholdFromRest(t *testing.T) {
	// start_duty - 0.001: stays stopped for 10 s.
	s := settle(arctic, Initial(arctic), arctic.StartDuty-0.001, 10*time.Second)
	if s.Running || s.RPM != 0 {
		t.Errorf("start_duty - 0.001 for 10 s: %+v, want stopped at exactly 0", s)
	}
	// duty == start_duty: starts and rpm rises.
	s = Step(arctic, Initial(arctic), arctic.StartDuty, 10*time.Millisecond)
	if !s.Running || s.RPM <= 0 {
		t.Errorf("duty == start_duty: %+v, want running with rpm > 0", s)
	}
	// From rest at duty 0 the rpm stays exactly 0.
	s = settle(arctic, Initial(arctic), 0, 5*time.Second)
	if s.Running || s.RPM != 0 {
		t.Errorf("rest at duty 0: %+v, want exactly 0", s)
	}
}

func TestStartDutyEqualsMinDutyIsZeroWidthBand(t *testing.T) {
	p := arctic
	p.StartDuty = p.MinDuty
	s := Step(p, Initial(p), p.MinDuty-0.001, 10*time.Millisecond)
	if s.Running {
		t.Errorf("below start_duty == min_duty: %+v, want stopped", s)
	}
	s = Step(p, Initial(p), p.MinDuty, 10*time.Millisecond)
	if !s.Running || s.RPM <= 0 {
		t.Errorf("at start_duty == min_duty: %+v, want started", s)
	}
	s = Step(p, s, p.MinDuty-0.001, 10*time.Millisecond)
	if s.Running {
		t.Errorf("stall at the same threshold: %+v, want stopped", s)
	}
}

func TestStallBelowMinDutyStopsAndTargetsZero(t *testing.T) {
	run := State{Running: true, RPM: 1000}
	// duty == min_duty keeps running, target is min_rpm.
	s := Step(arctic, run, arctic.MinDuty, 10*time.Millisecond)
	if !s.Running {
		t.Errorf("duty == min_duty: %+v, want still running", s)
	}
	if s.RPM >= run.RPM {
		t.Errorf("duty == min_duty: rpm %v did not fall towards min_rpm", s.RPM)
	}
	// min_duty - 0.001: stalls, target 0, rpm falls with tau_down.
	s = Step(arctic, run, arctic.MinDuty-0.001, time.Second)
	if s.Running {
		t.Errorf("min_duty - 0.001: %+v, want stalled (stop)", s)
	}
	want := 1000 + (0-1000)*(1-math.Exp(-1/arctic.TauDown))
	if relDiff(s.RPM, want) > 1e-12 {
		t.Errorf("stall rpm = %v, want %v (target 0, tau_down)", s.RPM, want)
	}
}

func TestHoldBelowMinDutyKeepsMinRPM(t *testing.T) {
	// hold fan: running at rest, target is min_rpm even at duty 0.
	s := settle(holdFan, Initial(holdFan), 0, 30*time.Second)
	if !s.Running || relDiff(s.RPM, holdFan.MinRPM) > 1e-9 {
		t.Errorf("hold fan at duty 0 settled at %+v, want running at min_rpm %v", s, holdFan.MinRPM)
	}
	s = settle(holdFan, s, holdFan.MinDuty-0.001, 30*time.Second)
	if !s.Running || relDiff(s.RPM, holdFan.MinRPM) > 1e-9 {
		t.Errorf("hold fan at min_duty - 0.001: %+v, want running at min_rpm", s)
	}
	// duty == min_duty also gives min_rpm (continuous with the hold plateau).
	s = settle(holdFan, s, holdFan.MinDuty, 30*time.Second)
	if relDiff(s.RPM, holdFan.MinRPM) > 1e-9 {
		t.Errorf("hold fan at min_duty: rpm %v, want %v", s.RPM, holdFan.MinRPM)
	}
	s = settle(holdFan, s, 1, 60*time.Second)
	if relDiff(s.RPM, holdFan.MaxRPM) > 1e-9 {
		t.Errorf("hold fan at duty 1: rpm %v, want max_rpm", s.RPM)
	}
}

func TestHysteresisBandKeepsPreviousState(t *testing.T) {
	band := (arctic.MinDuty + arctic.StartDuty) / 2 // 0.075
	// Running fan in the band keeps running at the linear target.
	run := settle(arctic, Initial(arctic), 0.5, 20*time.Second)
	s := settle(arctic, run, band, 60*time.Second)
	if !s.Running {
		t.Fatalf("running fan in the dead band: %+v, want still running", s)
	}
	if relDiff(s.RPM, linear(arctic, band)) > 1e-9 {
		t.Errorf("band rpm = %v, want linear target %v", s.RPM, linear(arctic, band))
	}
	// A stopped fan whose duty returns into the band stays stopped, even
	// while it is still coasting down (hysteresis follows duty only).
	coast := Step(arctic, State{Running: true, RPM: 800}, 0, 10*time.Millisecond)
	if coast.Running {
		t.Fatalf("duty 0 on a running fan: %+v, want stalled", coast)
	}
	again := Step(arctic, coast, band, 10*time.Millisecond)
	if again.Running {
		t.Errorf("coasting stopped fan with duty back in the band: %+v, want stopped", again)
	}
	if again.RPM >= coast.RPM {
		t.Errorf("stopped fan in the band must keep falling: %v -> %v", coast.RPM, again.RPM)
	}
}

func TestLinearTarget(t *testing.T) {
	cases := []struct {
		duty float64
		want float64
	}{
		{1.0, 1800},
		{arctic.MinDuty, 200},
		{0.4, 200 + 0.35/0.95*1600},
		{0.7, 200 + 0.65/0.95*1600},
	}
	for _, c := range cases {
		// Start the fan running and let the lag run out (exp(-4000) is 0).
		s := Step(arctic, State{Running: true}, c.duty, 10000*time.Second)
		if relDiff(s.RPM, c.want) > 1e-9 {
			t.Errorf("duty %v: settled rpm %v, want %v", c.duty, s.RPM, c.want)
		}
	}
}

func TestTauUpReaches63PercentAfterTauUp(t *testing.T) {
	target := linear(arctic, 0.4)
	s := Step(arctic, Initial(arctic), 0.4, time.Duration(arctic.TauUp*float64(time.Second)))
	want := target * (1 - 1/math.E)
	if relDiff(s.RPM, want) > 1e-6 {
		t.Errorf("rpm after tau_up = %v, want %v (63.2%% of %v)", s.RPM, want, target)
	}
}

func TestTauDownIsUsedWhenFalling(t *testing.T) {
	s := State{Running: true, RPM: 1000}
	// duty 0.5 -> target ~ 200 + 0.45/0.95*1600 = 957.9 < 1000: falling.
	target := linear(arctic, 0.5)
	got := Step(arctic, s, 0.5, time.Second)
	want := s.RPM + (target-s.RPM)*(1-math.Exp(-1/arctic.TauDown))
	if relDiff(got.RPM, want) > 1e-12 {
		t.Errorf("falling rpm = %v, want %v (tau_down)", got.RPM, want)
	}
	// Rising uses tau_up.
	s = State{Running: true, RPM: 500}
	got = Step(arctic, s, 0.5, time.Second)
	want = s.RPM + (target-s.RPM)*(1-math.Exp(-1/arctic.TauUp))
	if relDiff(got.RPM, want) > 1e-12 {
		t.Errorf("rising rpm = %v, want %v (tau_up)", got.RPM, want)
	}
}

func TestTauSelectionEqualTargetLeavesRPMUnchanged(t *testing.T) {
	target := linear(arctic, 0.5)
	s := State{Running: true, RPM: target}
	got := Step(arctic, s, 0.5, 3*time.Second)
	if got != s {
		t.Errorf("rpm == target: %+v, want unchanged %+v", got, s)
	}
}

func TestSnapToZeroBelowHalfRPM(t *testing.T) {
	// A single step below 0.5 rpm with target 0 lands on exactly 0.
	s := Step(arctic, State{Running: false, RPM: 0.4}, 0, 10*time.Millisecond)
	if s.RPM != 0 {
		t.Errorf("snap: rpm = %v, want exactly 0", s.RPM)
	}
	// A coasting fan reaches exactly 0 after a finite time and never shows a
	// residual like 0.0003.
	s = State{Running: true, RPM: 1800}
	var settled time.Duration
	for settled < 120*time.Second {
		s = Step(arctic, s, 0, 10*time.Millisecond)
		settled += 10 * time.Millisecond
		if s.RPM == 0 {
			break
		}
		if s.RPM > 0 && s.RPM < 0.5 {
			// This is the only way to be here: the step that produced it
			// must have snapped. Anything in (0, 0.5) is a missed snap.
			t.Fatalf("rpm %v in (0, 0.5) with target 0 after %v: snap missed", s.RPM, settled)
		}
	}
	if s.RPM != 0 {
		t.Errorf("rpm after %v at duty 0 = %v, want exactly 0", settled, s.RPM)
	}
	// Target not zero: no snap, even for a small rpm.
	s = Step(arctic, State{Running: true, RPM: 0.1}, 0.5, 10*time.Millisecond)
	if s.RPM <= 0.1 {
		t.Errorf("rising from 0.1 rpm: %v", s.RPM)
	}
}

func TestNonPositiveDtReturnsInputState(t *testing.T) {
	in := State{Running: true, RPM: 700}
	for _, dt := range []time.Duration{0, -time.Millisecond, -time.Hour} {
		if got := Step(arctic, in, 0.9, dt); got != in {
			t.Errorf("dt %v: %+v, want unchanged", dt, got)
		}
		// Even a start request does nothing with dt <= 0.
		if got := Step(arctic, Initial(arctic), 0.9, dt); got != Initial(arctic) {
			t.Errorf("dt %v from rest: %+v, want unchanged", dt, got)
		}
	}
}

func TestDutyIsClampedToUnitRange(t *testing.T) {
	// -0.5 behaves as 0: a stopped fan stays stopped, a running one stalls.
	if got, want := Step(arctic, Initial(arctic), -0.5, time.Second), Step(arctic, Initial(arctic), 0, time.Second); got != want {
		t.Errorf("duty -0.5 = %+v, duty 0 = %+v", got, want)
	}
	run := State{Running: true, RPM: 900}
	if got, want := Step(arctic, run, -0.5, time.Second), Step(arctic, run, 0, time.Second); got != want {
		t.Errorf("running fan duty -0.5 = %+v, duty 0 = %+v", got, want)
	}
	// 1.7 behaves as 1.
	if got, want := Step(arctic, run, 1.7, time.Second), Step(arctic, run, 1, time.Second); got != want {
		t.Errorf("duty 1.7 = %+v, duty 1 = %+v", got, want)
	}
	// NaN must not poison the state; it is treated as duty 0.
	if got := Step(arctic, run, math.NaN(), time.Second); math.IsNaN(got.RPM) || got != Step(arctic, run, 0, time.Second) {
		t.Errorf("duty NaN = %+v, want same as duty 0", got)
	}
}

func TestStepIsPureAndComposable(t *testing.T) {
	a := Step(arctic, State{Running: true, RPM: 300}, 0.6, 123*time.Millisecond)
	b := Step(arctic, State{Running: true, RPM: 300}, 0.6, 123*time.Millisecond)
	if a != b {
		t.Errorf("identical inputs gave %+v and %+v", a, b)
	}
	// Ten 100 ms steps equal one 1 s step while target and tau are constant
	// (rising, running the whole time).
	start := State{Running: true, RPM: 300}
	one := Step(arctic, start, 0.6, time.Second)
	ten := start
	for i := 0; i < 10; i++ {
		ten = Step(arctic, ten, 0.6, 100*time.Millisecond)
	}
	if relDiff(one.RPM, ten.RPM) > 1e-9 || one.Running != ten.Running {
		t.Errorf("1 x 1 s = %+v, 10 x 100 ms = %+v", one, ten)
	}
	// Same from rest, including the start transition.
	one = Step(arctic, Initial(arctic), 0.4, time.Second)
	ten = Initial(arctic)
	for i := 0; i < 10; i++ {
		ten = Step(arctic, ten, 0.4, 100*time.Millisecond)
	}
	if relDiff(one.RPM, ten.RPM) > 1e-9 {
		t.Errorf("from rest: 1 x 1 s = %v, 10 x 100 ms = %v", one.RPM, ten.RPM)
	}
}

// IN-02: a plant owns its parameters. Neither the Params it was built from nor the Params it hands
// out alias them, so the engine goroutine's model cannot be changed from outside.
func TestPlantOwnsItsParams(t *testing.T) {
	src := tachFan(1000, 2, 0)
	src.Curve.Down = Curve{{0.05, 900}, {1, 1000}}
	src.Provenance = []ProvEntry{{Path: "stop.duty", Kind: KindAssumed}}
	src.Sources = map[string]string{"x": "text"}
	src.Datasheet = &Datasheet{AirflowM3h: 50, PowerW: 1, RefRPM: 1000}
	want := src.Clone()
	pl := NewPlant(src, 1, 7)

	same := func(got Params, what string) {
		t.Helper()
		if !slices.Equal(got.Curve.Up, want.Curve.Up) || !slices.Equal(got.Curve.Down, want.Curve.Down) ||
			!slices.Equal(got.Provenance, want.Provenance) || got.Sources["x"] != "text" ||
			got.Datasheet == nil || *got.Datasheet != *want.Datasheet {
			t.Errorf("%s: params changed to %+v, want %+v", what, got, want)
		}
	}

	// Changing the source after NewPlant does not reach the plant.
	src.Curve.Up[0].RPM = 1
	src.Curve.Down[0].RPM = 1
	src.Provenance[0].Kind = KindMeasured
	src.Sources["x"] = "changed"
	src.Datasheet.PowerW = 99
	same(pl.Params(), "after mutating the source")

	// Changing what Params returns does not reach the plant.
	got := pl.Params()
	got.Curve.Up[0].RPM = 2
	got.Curve.Down[0].RPM = 2
	got.Provenance[0].Kind = KindMeasured
	got.Sources["x"] = "changed"
	got.Datasheet.PowerW = 98
	same(pl.Params(), "after mutating the result of Params")
	if r := pl.Params().Curve.Up.At(0.5); r != 1000 {
		t.Errorf("curve.up at 0.5 = %v after the mutations, want 1000", r)
	}

	// Nil stays nil: a schema 1 fan has no curves, provenance, sources or datasheet.
	flat := NewPlant(Params{Name: "flat", MinDuty: 0.05, StartDuty: 0.1, BelowMin: "stop", MinRPM: 200, MaxRPM: 1800, TauUp: 1, TauDown: 1}, 1, 0).Params()
	if flat.Curve.Up != nil || flat.Curve.Down != nil || flat.Provenance != nil || flat.Sources != nil || flat.Datasheet != nil {
		t.Errorf("a schema 1 params clone is not all nil: %+v", flat)
	}
}

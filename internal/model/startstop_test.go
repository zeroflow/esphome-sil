package model

import (
	"math"
	"testing"
	"time"
)

const tick = 10 * time.Millisecond

// ssFan is a schema 2 style fan: a curve, a 400 ms start delay and a 3 s coast.
var ssFan = Params{
	Name: "ss", MinDuty: 0.05, StartDuty: 0.10, BelowMin: "stop",
	MinRPM: 300, MaxRPM: 1700, TauUp: 0.5, TauDown: 0.8,
	Curve: Curves{Up: Curve{
		{0.05, 300}, {0.20, 600}, {0.40, 1000}, {0.60, 1300}, {0.80, 1550}, {1.00, 1700},
	}},
	StartDelay: 400 * time.Millisecond, CoastTau: 3, Schema: 2,
}

// ssNoDelay is ssFan with no start delay: the reference for the running segment.
func ssNoDelay() Params {
	p := ssFan
	p.StartDelay = 0
	return p
}

// stepN applies n steps of dt.
func stepN(p Params, s State, duty float64, dt time.Duration, n int) State {
	for i := 0; i < n; i++ {
		s = Step(p, s, duty, dt)
	}
	return s
}

func TestStartDelaySplitsTheStep(t *testing.T) {
	s := stepN(ssFan, Initial(ssFan), 0.5, tick, 39)
	if s.Running || s.RPM != 0 || s.StartElapsed != 390*time.Millisecond {
		t.Fatalf("after 390 ms: %+v, want stopped, rpm 0, StartElapsed 390 ms", s)
	}
	got := Step(ssFan, s, 0.5, 2*tick)
	want := Step(ssNoDelay(), State{Running: true}, 0.5, tick)
	if !got.Running || got.RPM != want.RPM || got.RPM <= 0 || got.StartElapsed != 0 {
		t.Errorf("20 ms step across the expiry: %+v, want running with rpm %v (a 10 ms spin-up from 0)", got, want.RPM)
	}
}

func TestStartDelayRearms(t *testing.T) {
	s := stepN(ssFan, Initial(ssFan), 0.5, tick, 30)
	s = Step(ssFan, s, 0.08, tick) // below start.duty, above stop.duty
	if s.Running || s.StartElapsed != 0 {
		t.Fatalf("dip below start.duty: %+v, want stopped with StartElapsed 0", s)
	}
	s = stepN(ssFan, s, 0.5, tick, 39)
	if s.Running || s.RPM != 0 {
		t.Fatalf("390 ms after the second rise: %+v, want still stopped", s)
	}
	s = Step(ssFan, s, 0.5, tick)
	if !s.Running {
		t.Fatalf("400 ms after the second rise: %+v, want running", s)
	}
}

func TestZeroDelayIsV010Order(t *testing.T) {
	p := ssNoDelay()
	coasting := State{Running: false, RPM: 0.3}
	got := Step(p, coasting, 0.5, tick)
	want := Step(p, State{Running: true, RPM: 0.3}, 0.5, tick)
	if got != want {
		t.Errorf("zero delay restart = %+v, want the single running segment %+v", got, want)
	}
	if got.RPM <= 0.3 {
		t.Errorf("rpm %v: the coasting 0.3 rpm must not be snapped before the restart", got.RPM)
	}
}

func TestStopIsImmediateAndCoasts(t *testing.T) {
	run := State{Running: true, RPM: 1000}
	s := Step(ssFan, run, 0.03, tick)
	if s.Running {
		t.Fatalf("duty below stop.duty: %+v, want stopped after one step", s)
	}
	s = Step(ssFan, run, 0.03, time.Second)
	if want := 1000 * math.Exp(-1.0/3); relDiff(s.RPM, want) > 1e-9 {
		t.Errorf("coast rpm after 1 s = %v, want %v (coast_tau_s 3)", s.RPM, want)
	}
	legacy := ssFan
	legacy.CoastTau = 0
	s = Step(legacy, run, 0.03, time.Second)
	if want := 1000 * math.Exp(-1.0/0.8); relDiff(s.RPM, want) > 1e-9 {
		t.Errorf("legacy coast rpm after 1 s = %v, want %v (tau_down_s)", s.RPM, want)
	}
	// A running fan that is slowed but not stopped uses tau_down, not the coast tau.
	s = Step(ssFan, run, 0.07, time.Second)
	if !s.Running {
		t.Fatalf("duty in the band: %+v, want still running", s)
	}
	target := ssFan.upCurve().At(0.07)
	if want := target + (1000-target)*math.Exp(-1.0/0.8); relDiff(s.RPM, want) > 1e-9 {
		t.Errorf("running slow-down rpm = %v, want %v (tau_down_s)", s.RPM, want)
	}
}

func TestCoastReArmsAndReaccelerates(t *testing.T) {
	s := State{Running: false, RPM: 600}
	s = stepN(ssFan, s, 0.5, tick, 39)
	if s.Running {
		t.Fatalf("during the delay: %+v, want still stopped", s)
	}
	if want := 600 * math.Exp(-0.39/3); relDiff(s.RPM, want) > 1e-9 {
		t.Errorf("coast rpm after 390 ms = %v, want %v", s.RPM, want)
	}
	coast := s.RPM
	s = Step(ssFan, s, 0.5, tick) // the delay expires on this tick
	if !s.Running || relDiff(s.RPM, 600*math.Exp(-0.4/3)) > 1e-9 {
		t.Errorf("at the expiry: %+v, want running at the coast rpm %v", s, 600*math.Exp(-0.4/3))
	}
	s = stepN(ssFan, s, 0.5, tick, 50)
	if s.RPM <= coast {
		t.Errorf("rpm %v after the delay, want it to rise from the coast rpm %v", s.RPM, coast)
	}
}

func TestBandKeepsState(t *testing.T) {
	run := stepN(ssFan, State{Running: true, RPM: 340}, 0.07, tick, 100)
	if !run.Running || run.StartElapsed != 0 {
		t.Errorf("running fan in the band: %+v, want running", run)
	}
	stopped := stepN(ssFan, Initial(ssFan), 0.07, tick, 100)
	if stopped.Running || stopped.RPM != 0 || stopped.StartElapsed != 0 {
		t.Errorf("stopped fan in the band: %+v, want stopped, rpm 0, StartElapsed 0", stopped)
	}
}

func TestStartAtExactStartDuty(t *testing.T) {
	p := ssFan // StartDuty 0.10, StartDelay 400 ms, MinDuty 0.05
	s := stepN(p, Initial(p), p.StartDuty, tick, 39)
	if s.Running || s.RPM != 0 || s.StartElapsed != 390*time.Millisecond {
		t.Fatalf("39 steps at start.duty: %+v, want stopped, rpm 0, StartElapsed 390 ms", s)
	}
	s = Step(p, s, p.StartDuty, tick)
	if !s.Running || s.RPM != 0 {
		t.Fatalf("40th step at start.duty: %+v, want running with rpm still 0 (zero-length segment)", s)
	}
	s = Step(p, s, p.StartDuty, tick)
	if s.RPM <= 0 {
		t.Fatalf("41st step: %+v, want rpm > 0", s)
	}

	below := stepN(p, Initial(p), p.StartDuty-1e-9, tick, 200)
	if below.Running || below.RPM != 0 || below.StartElapsed != 0 {
		t.Errorf("start.duty - 1e-9 for 2 s: %+v, want never started", below)
	}

	nd := ssNoDelay()
	if s := Step(nd, Initial(nd), nd.StartDuty, tick); !s.Running || s.RPM <= 0 {
		t.Errorf("zero delay at start.duty: %+v, want started within the first step", s)
	}

	keep := stepN(p, State{Running: true, RPM: 400}, p.MinDuty, tick, 200)
	if !keep.Running {
		t.Errorf("duty == stop.duty for 2 s: %+v, want still running", keep)
	}
	if s := Step(p, State{Running: true, RPM: 400}, p.MinDuty-1e-9, tick); s.Running {
		t.Errorf("stop.duty - 1e-9: %+v, want stopped in one step", s)
	}
}

func TestComposableAcrossDelayAndSnap(t *testing.T) {
	one := Step(ssFan, Initial(ssFan), 0.5, time.Second)
	ten := stepN(ssFan, Initial(ssFan), 0.5, 100*time.Millisecond, 10)
	if relDiff(one.RPM, ten.RPM) > 1e-9 || one.Running != ten.Running || one.StartElapsed != ten.StartElapsed {
		t.Errorf("1 x 1 s = %+v, 10 x 100 ms = %+v: want equal across the delay expiry", one, ten)
	}
	if one.RPM <= 0 || !one.Running {
		t.Errorf("1 s at duty 0.5 = %+v, want running with rpm > 0", one)
	}

	p := ssFan
	p.CoastTau = 1
	coast := State{Running: false, RPM: 2}
	a := Step(p, coast, 0, 2*time.Second)
	b := stepN(p, coast, 0, tick, 200)
	if a != b || a.RPM != 0 {
		t.Errorf("coast from 2 rpm: 1 x 2 s = %+v, 200 x 10 ms = %+v, want the same snapped state", a, b)
	}
}

// refArea is the reference for Integral.Area: n equal sub-steps through advance, each summed as
// a rectangle at the rpm of its midpoint.
func refArea(p Params, s State, duty float64, dt time.Duration, n int) (State, float64) {
	h := dt / time.Duration(n)
	area := 0.0
	for i := 0; i < n; i++ {
		mid, _ := advance(p, s, duty, h/2)
		area += mid.RPM * h.Seconds()
		s, _ = advance(p, s, duty, h)
	}
	return s, area
}

func TestAdvanceIntegralIsExact(t *testing.T) {
	fastCoast := ssFan
	fastCoast.CoastTau = 0.5 // snaps at 0.5 * ln(2000) = 3.8 s, inside the 4 s step
	cases := []struct {
		name   string
		p      Params
		s      State
		duty   float64
		dt     time.Duration
		moving func(dt time.Duration) float64 // wanted MovingS; nil means "> 0 and <= dt"
	}{
		{"rising", ssNoDelay(), State{Running: true, RPM: 300}, 0.8, time.Second, nil},
		{"falling", ssNoDelay(), State{Running: true, RPM: 1500}, 0.2, time.Second, nil},
		{"across the delay expiry", ssFan, State{Running: false, StartElapsed: 200 * time.Millisecond}, 0.5, time.Second, nil},
		{"coasting while waiting", ssFan, State{Running: false, RPM: 800, StartElapsed: 100 * time.Millisecond}, 0.5, time.Second, nil},
		{"across the snap", fastCoast, State{Running: true, RPM: 1000}, 0, 4 * time.Second, nil},
		{"at rest", ssFan, State{}, 0, time.Second, func(time.Duration) float64 { return 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, in := advance(c.p, c.s, c.duty, c.dt)
			ref, want := refArea(c.p, c.s, c.duty, c.dt, 10000)
			if relDiff(in.Area, want) > 1e-6 && !(in.Area == 0 && want == 0) {
				t.Errorf("Area = %v, want %v (10 000 sub-steps)", in.Area, want)
			}
			if got.Running != ref.Running || relDiff(got.RPM, ref.RPM) > 1e-9 {
				t.Errorf("final state %+v, want %+v", got, ref)
			}
			if st := Step(c.p, c.s, c.duty, c.dt); st != got {
				t.Errorf("Step = %+v, want advance's state %+v", st, got)
			}
			if c.moving != nil {
				if in.MovingS != c.moving(c.dt) {
					t.Errorf("MovingS = %v, want %v", in.MovingS, c.moving(c.dt))
				}
			} else if in.MovingS <= 0 || in.MovingS > c.dt.Seconds() {
				t.Errorf("MovingS = %v, want in (0, %v]", in.MovingS, c.dt.Seconds())
			}
		})
	}

	// MovingS is the whole step while the fan turns, and the crossing time when it snaps.
	_, in := advance(ssNoDelay(), State{Running: true, RPM: 300}, 0.8, time.Second)
	if in.MovingS != 1 {
		t.Errorf("rising step MovingS = %v, want 1", in.MovingS)
	}
	_, in = advance(ssFan, State{Running: true, RPM: 1000}, 0, 30*time.Second)
	if want := 3 * math.Log(1000/0.5); relDiff(in.MovingS, want) > 1e-12 {
		t.Errorf("snapped coast MovingS = %v, want tau*ln(rpm0/0.5) = %v", in.MovingS, want)
	}
	if want := 1000*3 - 0.5*3; relDiff(in.Area, want) > 1e-12 {
		t.Errorf("snapped coast Area = %v, want rpm0*tau - 0.5*tau = %v", in.Area, want)
	}
	if st, in := advance(ssFan, State{}, 0.5, 0); st != (State{}) || in != (Integral{}) {
		t.Errorf("dt 0 returned %+v %+v, want the state unchanged and a zero Integral", st, in)
	}
}

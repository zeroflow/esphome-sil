package model

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func TestCurveAtInterpolatesAndClamps(t *testing.T) {
	c := Curve{{0.2, 600}, {0.4, 1000}, {0.8, 1400}}
	cases := []struct{ duty, want float64 }{
		{0.3, 800},  // midway between the first two points
		{0.6, 1200}, // midway between the last two points
		{0, 600},    // below the first point: its rpm, no extrapolation
		{-3, 600},   // far below
		{1, 1400},   // above the last point: its rpm, no extrapolation
		{50, 1400},  // far above
		{0.2, 600},  // on a table point
		{0.4, 1000}, // on a table point
		{0.8, 1400}, // on the last point
		{0.25, 700}, // quarter of the first segment
		{0.7, 1300}, // three quarters of the last segment
		{math.Inf(1), 1400},
	}
	for _, tc := range cases {
		if got := c.At(tc.duty); relDiff(got, tc.want) > 1e-12 {
			t.Errorf("At(%v) = %v, want %v", tc.duty, got, tc.want)
		}
	}
	if got := (Curve{}).At(0.5); got != 0 {
		t.Errorf("empty curve At = %v, want 0", got)
	}
	if c.Max() != 1400 || c.Min() != 600 {
		t.Errorf("Max/Min = %v/%v, want 1400/600", c.Max(), c.Min())
	}
}

func TestCurveAtIsMonotone(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 200; n++ {
		points := 2 + rng.IntN(7)
		c := make(Curve, points)
		duty, rpm := 0.0, 0.0
		for i := range c {
			duty += 0.001 + rng.Float64()*(1-duty)/float64(points-i+1)
			rpm += float64(rng.IntN(3)) * rng.Float64() * 500 // sometimes a flat segment
			c[i] = Point{Duty: math.Min(duty, 1), RPM: rpm}
		}
		prev := math.Inf(-1)
		for i := 0; i <= 10000; i++ {
			v := c.At(float64(i) / 10000)
			if v < prev {
				t.Fatalf("curve %v: At(%v) = %v fell below %v", c, float64(i)/10000, v, prev)
			}
			prev = v
		}
	}
}

// legacy fans synthesise their curve; its targets must equal the v0.1.0 expression bit for bit.
func TestLegacyCurveIsBitIdentical(t *testing.T) {
	for _, p := range []Params{arctic, holdFan} {
		curve := p.upCurve()
		for i := 0; i <= 10000; i++ {
			duty := float64(i) / 10000
			var want float64
			if duty < p.MinDuty {
				if p.BelowMin != "hold" {
					continue // a stop fan never targets a rpm below min_duty
				}
				want = p.MinRPM
			} else {
				want = linear(p, duty)
			}
			if got := curve.At(duty); got != want {
				t.Fatalf("%s: At(%v) = %v, want %v (v0.1.0)", p.Name, duty, got, want)
			}
		}
		if p.downCurve() == nil || len(p.downCurve()) != len(curve) {
			t.Errorf("%s: a legacy fan's down curve must be its up curve", p.Name)
		}
	}
}

// dirFan has distinct up and down curves and fast taus, so it settles within a few seconds.
var dirFan = Params{
	Name: "dir", MinDuty: 0.1, StartDuty: 0.1, BelowMin: "stop", MinRPM: 100, MaxRPM: 1000,
	TauUp: 0.1, TauDown: 0.1,
	Curve: Curves{
		Up:   Curve{{0.1, 200}, {1, 1000}},
		Down: Curve{{0.1, 100}, {1, 800}},
	},
}

func settled(p Params, s State, duty float64) State { return settle(p, s, duty, 30*time.Second) }

func TestDirectionPicksCurve(t *testing.T) {
	up, down := dirFan.Curve.Up, dirFan.Curve.Down
	check := func(what string, s State, want float64) {
		t.Helper()
		if relDiff(s.RPM, want) > 1e-9 {
			t.Errorf("%s: rpm = %v, want %v", what, s.RPM, want)
		}
	}
	s := settled(dirFan, Initial(dirFan), 0.5) // rising from rest
	check("rising to 0.5 settles on Up", s, up.At(0.5))
	s = settled(dirFan, s, 0.3) // falling
	check("falling to 0.3 settles on Down", s, down.At(0.3))
	s = settled(dirFan, s, 0.3) // unchanged duty keeps the branch it arrived on
	check("unchanged duty after a fall stays on Down", s, down.At(0.3))
	s = settled(dirFan, s, 0.5) // rising again
	check("rising again returns to Up", s, up.At(0.5))
	s = settled(dirFan, s, 0.5)
	check("unchanged duty after a rise stays on Up", s, up.At(0.5))

	// Rising and falling at the same duty give different rpm, falling lower.
	rise := settled(dirFan, settled(dirFan, Initial(dirFan), 0.2), 0.6)
	fall := settled(dirFan, settled(dirFan, Initial(dirFan), 1.0), 0.6)
	if !(fall.RPM < rise.RPM) {
		t.Errorf("duty 0.6: falling rpm %v is not below rising rpm %v", fall.RPM, rise.RPM)
	}

	// No down curve: both directions use Up.
	noDown := dirFan
	noDown.Curve.Down = nil
	s = settled(noDown, settled(noDown, Initial(noDown), 0.9), 0.3)
	check("no down curve, falling", s, up.At(0.3))
}

func TestStepStateCarriesDirection(t *testing.T) {
	step := func(s State, duty float64) State { return Step(dirFan, s, duty, 10*time.Millisecond) }
	s := step(State{Running: true}, 0.5)
	if s.LastDuty != 0.5 || s.Falling {
		t.Errorf("after rising to 0.5: %+v, want LastDuty 0.5, not falling", s)
	}
	s = step(s, 0.3)
	if s.LastDuty != 0.3 || !s.Falling {
		t.Errorf("after falling to 0.3: %+v, want LastDuty 0.3, falling", s)
	}
	s = step(s, 0.3)
	if s.LastDuty != 0.3 || !s.Falling {
		t.Errorf("unchanged duty: %+v, want the sticky direction kept", s)
	}
	s = step(s, 0.31)
	if s.Falling {
		t.Errorf("after a rise to 0.31: %+v, want rising", s)
	}
	// The clamped duty is stored: out of range clamps, NaN is duty 0.
	if got := step(State{Running: true}, 1.7); got.LastDuty != 1 {
		t.Errorf("duty 1.7: LastDuty = %v, want 1", got.LastDuty)
	}
	if got := step(State{Running: true, LastDuty: 0.4}, math.NaN()); got.LastDuty != 0 || !got.Falling {
		t.Errorf("duty NaN: %+v, want LastDuty 0 and falling", got)
	}
	// A non-positive dt returns the state untouched.
	in := State{Running: true, RPM: 700, LastDuty: 0.4, Falling: true}
	if got := Step(dirFan, in, 0.9, 0); got != in {
		t.Errorf("dt 0: %+v, want %+v", got, in)
	}
	// A fan without a down curve has no direction to track: its state stays the v0.1.0 state.
	got := Step(arctic, State{Running: true, RPM: 500}, 0.5, time.Second)
	if got.LastDuty != 0 || got.Falling {
		t.Errorf("legacy fan: %+v, want LastDuty 0 and Falling false", got)
	}
}

package model

// Frozen copy of the v0.1.0 fan model (git show v0.1.0:internal/model/fan.go), never edit.
// It is the reference of the "every v0.1.0 fan file behaves exactly as before" proof (D-01,
// roadmap criterion 5): the differential tests below drive the current Step and this copy with
// the same scripts and compare with ==.

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// v010Params are the nine legacy fields of the v0.1.0 Params.
type v010Params struct {
	Name, Source string
	MinDuty      float64
	StartDuty    float64
	BelowMin     string // "stop" or "hold"
	MinRPM       float64
	MaxRPM       float64
	TauUp        float64
	TauDown      float64
}

// v010State is the v0.1.0 State.
type v010State struct {
	Running bool
	RPM     float64
}

func initialV010(p v010Params) v010State {
	return v010State{Running: p.BelowMin == "hold"}
}

// stepV010 is the v0.1.0 Step, verbatim apart from the type names.
func stepV010(p v010Params, s v010State, duty float64, dt time.Duration) v010State {
	if dt <= 0 {
		return s
	}
	switch {
	case math.IsNaN(duty): // never let a bad input poison the state
		duty = 0
	case duty < 0:
		duty = 0
	case duty > 1:
		duty = 1
	}

	running := s.Running
	switch {
	case !running && duty >= p.StartDuty:
		running = true
	case running && duty < p.MinDuty:
		running = p.BelowMin == "hold"
	}

	target := 0.0
	switch {
	case !running:
	case duty < p.MinDuty: // only reachable for "hold"
		target = p.MinRPM
	default:
		target = p.MinRPM + (duty-p.MinDuty)/(1-p.MinDuty)*(p.MaxRPM-p.MinRPM)
	}

	rpm := s.RPM
	if target != rpm {
		tau := p.TauDown
		if target > rpm {
			tau = p.TauUp
		}
		if tau > 0 {
			rpm += (target - rpm) * (1 - math.Exp(-dt.Seconds()/tau))
		} else {
			rpm = target // degenerate params; fanlib rejects tau <= 0
		}
	}
	if target == 0 && rpm < 0.5 {
		rpm = 0
	}
	return v010State{Running: running, RPM: rpm}
}

// params converts to the current Params, legacy fields only.
func (p v010Params) params() Params {
	return Params{
		Name: p.Name, Source: p.Source, MinDuty: p.MinDuty, StartDuty: p.StartDuty,
		BelowMin: p.BelowMin, MinRPM: p.MinRPM, MaxRPM: p.MaxRPM, TauUp: p.TauUp, TauDown: p.TauDown,
	}
}

// drawV010 draws one legacy parameter set.
func drawV010(r *rand.Rand) v010Params {
	p := v010Params{Name: "random"}
	if r.Float64() < 0.2 {
		p.MinDuty = 0
	} else {
		p.MinDuty = 0.01 + r.Float64()*0.49
	}
	if r.Float64() < 0.2 {
		p.StartDuty = p.MinDuty
	} else {
		p.StartDuty = p.MinDuty + r.Float64()*(1-p.MinDuty)
	}
	p.BelowMin = "stop"
	if r.IntN(2) == 0 {
		p.BelowMin = "hold"
	}
	p.MinRPM = r.Float64() * 800
	p.MaxRPM = p.MinRPM + 1 + r.Float64()*(3000-p.MinRPM-1)
	p.TauUp = 0.05 + r.Float64()*4.95
	p.TauDown = 0.05 + r.Float64()*4.95
	return p
}

var v010Dts = []time.Duration{10 * time.Millisecond, 7 * time.Millisecond, 30 * time.Millisecond, time.Second, 0, -time.Millisecond}

// scriptV010 is a duty/dt sequence of n steps, with repeats and the edge duties of p.
func scriptV010(r *rand.Rand, p v010Params, n int) (duties []float64, dts []time.Duration) {
	edges := []float64{p.MinDuty, p.StartDuty, p.MinDuty - 1e-9, p.StartDuty - 1e-9, 0, 1, math.NaN(), -0.1, 1.2}
	duty := r.Float64()
	for i := 0; i < n; i++ {
		switch {
		case r.Float64() < 0.4: // repeat the previous duty
		case r.Float64() < 0.5:
			duty = r.Float64()
		default:
			duty = edges[r.IntN(len(edges))]
		}
		duties = append(duties, duty)
		dts = append(dts, v010Dts[r.IntN(len(v010Dts))])
	}
	return duties, dts
}

func TestStepMatchesV010(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 1))
	check := func(p v010Params, duties []float64, dts []time.Duration) {
		t.Helper()
		np := p.params()
		old, cur := initialV010(p), Initial(np)
		for i := range duties {
			old = stepV010(p, old, duties[i], dts[i])
			cur = Step(np, cur, duties[i], dts[i])
			if cur.RPM != old.RPM || cur.Running != old.Running {
				t.Fatalf("step %d (duty %v, dt %v) of %+v: Step = {Running %v, RPM %v}, v0.1.0 = %+v",
					i, duties[i], dts[i], p, cur.Running, cur.RPM, old)
			}
		}
	}
	for n := 0; n < 2000; n++ {
		p := drawV010(r)
		duties, dts := scriptV010(r, p, 400)
		check(p, duties, dts)
	}

	// The degenerate branch: tau 0 gives rpm = target, in both models.
	p := v010Params{Name: "tau0", MinDuty: 0.05, StartDuty: 0.10, BelowMin: "stop", MinRPM: 200, MaxRPM: 1800}
	duties, dts := scriptV010(r, p, 400)
	check(p, duties, dts)
	q := p
	q.BelowMin = "hold"
	check(q, duties, dts)
}

func TestPlantMatchesV010ForLegacyParams(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 2))
	for n := 0; n < 2000; n++ {
		p := drawV010(r)
		duties, dts := scriptV010(r, p, 400)
		pl := NewPlant(p.params(), 1, 0)
		old := initialV010(p)
		for i := range duties {
			old = stepV010(p, old, duties[i], dts[i])
			pl.Step(duties[i], dts[i])
			if pl.Shaft() != old.RPM || pl.Running() != old.Running {
				t.Fatalf("step %d (duty %v, dt %v) of %+v: Plant = {Running %v, RPM %v}, v0.1.0 = %+v",
					i, duties[i], dts[i], p, pl.Running(), pl.Shaft(), old)
			}
		}
	}
}

// TestNeverStopsForm: the schema 2 never-stops form (stop.duty 0 with a curve point at duty 0)
// counts as running from plant creation, holds the floor rpm at 0 %, and gives the identical trace
// to the v1 hold fan with the same numbers (D-10 as amended 2026-11-10).
func TestNeverStopsForm(t *testing.T) {
	form := Params{
		Name: "never stops", MinDuty: 0, StartDuty: 0, BelowMin: "hold", MinRPM: 450, MaxRPM: 2000,
		TauUp: 1, TauDown: 2.5, Schema: 2,
		Curve: Curves{Up: Curve{{0, 450}, {0.2, 450}, {1, 2000}}},
	}
	v1 := Params{
		Name: "v1 hold", MinDuty: 0.2, StartDuty: 0.2, BelowMin: "hold", MinRPM: 450, MaxRPM: 2000,
		TauUp: 1, TauDown: 2.5,
	}
	if s := Initial(form); !s.Running {
		t.Fatalf("Initial(never-stops form) = %+v, want running from plant creation", s)
	}
	s := settle(form, Initial(form), 0, 30*time.Second)
	if !s.Running || math.Abs(s.RPM-450) > 1e-6 {
		t.Errorf("never-stops form at duty 0 for 30 s: %+v, want running at 450", s)
	}
	s = settle(form, s, 0, 10*time.Second)
	if !s.Running || math.Abs(s.RPM-450) > 1e-6 {
		t.Errorf("never-stops form holds: %+v, want running at 450", s)
	}

	a, b := Initial(form), Initial(v1)
	for _, duty := range []float64{0, 0.1, 0.2, 0.6} {
		for i := 0; i < 100; i++ {
			a = Step(form, a, duty, 10*time.Millisecond)
			b = Step(v1, b, duty, 10*time.Millisecond)
			if a.RPM != b.RPM || a.Running != b.Running {
				t.Fatalf("duty %v step %d: never-stops form %+v, v1 hold fan %+v, want identical traces",
					duty, i, a, b)
			}
		}
	}
	if a.RPM <= 450 {
		t.Errorf("rpm %v at the end of the trace, want it above the floor (duty 0.6 was applied)", a.RPM)
	}
}

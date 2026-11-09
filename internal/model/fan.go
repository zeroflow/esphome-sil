// Package model holds the pure fan model: no clock, no I/O.
package model

import (
	"math"
	"time"
)

// Params describes one fan model. Taus are in seconds.
type Params struct {
	Name, Source string
	MinDuty      float64
	StartDuty    float64
	BelowMin     string // "stop" or "hold"
	MinRPM       float64
	MaxRPM       float64
	TauUp        float64
	TauDown      float64
}

// State is the fan's dynamic state.
type State struct {
	Running bool
	RPM     float64
}

// Initial is the state of a fan at rest. A "hold" fan never stops, so it
// counts as running (at rpm 0) until the first Step spins it up to min_rpm.
func Initial(p Params) State {
	return State{Running: p.BelowMin == "hold"}
}

// Step advances s by dt under the commanded duty. It is a pure function of
// its arguments: no clock, no I/O, so ten 100 ms steps equal one 1 s step
// while target and tau stay constant. A non-positive dt returns s unchanged.
//
// Semantics (D-11..D-13):
//   - A stopped fan starts when duty >= StartDuty. A running fan keeps
//     running down to MinDuty; below MinDuty it stalls (BelowMin "stop",
//     target 0) or holds MinRPM (BelowMin "hold"). Between MinDuty and
//     StartDuty the previous running state is kept. Hysteresis follows duty
//     only, so a coasting stopped fan with duty back in the band stays
//     stopped.
//   - While running the target is linear from MinRPM at MinDuty to MaxRPM at
//     duty 1. rpm follows it with TauUp when rising and TauDown when falling
//     via rpm += (target - rpm) * (1 - exp(-dt/tau)); an rpm equal to the
//     target is left alone.
//   - The exponential never reaches 0, so rpm snaps to 0 when the target is 0
//     and rpm < 0.5.
func Step(p Params, s State, duty float64, dt time.Duration) State {
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
	return State{Running: running, RPM: rpm}
}

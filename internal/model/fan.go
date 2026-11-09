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

// Initial is the state of a fan at rest.
func Initial(p Params) State {
	return State{}
}

// Step advances s by dt under the commanded duty. It is a pure function of
// its arguments. A non-positive dt returns s unchanged.
//
// This version is deliberately thin: it runs when duty >= MinDuty and relaxes
// exponentially towards the linear target. Start hysteresis, below_min "hold"
// and snap-to-zero arrive in plan 01-05 behind this same signature.
func Step(p Params, s State, duty float64, dt time.Duration) State {
	if dt <= 0 {
		return s
	}
	duty = math.Min(1, math.Max(0, duty))
	running := duty >= p.MinDuty
	target := 0.0
	if running {
		target = p.MinRPM + (duty-p.MinDuty)/(1-p.MinDuty)*(p.MaxRPM-p.MinRPM)
	}
	tau := p.TauDown
	if target > s.RPM {
		tau = p.TauUp
	}
	rpm := s.RPM
	if tau > 0 {
		rpm += (target - rpm) * (1 - math.Exp(-dt.Seconds()/tau))
	} else {
		rpm = target
	}
	return State{Running: running, RPM: rpm}
}

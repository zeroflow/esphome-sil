package model

import "time"

// Plant is one port's fan: the engine's only seam into the model (and the one
// an offline replay uses later). It owns the parameters, the dynamic state and
// the plant time, and exposes the shaft truth plus what the firmware's tach
// path sees.
//
// seed and port are kept for the tach and noise of plan 04-04: the noise is a
// keyed hash of (seed, port, sim-time bucket), so it needs no stream state and
// does not depend on the tick timing. For an ideal tach (Tach.PPR == 0) Frame
// and Current are the shaft rpm, which keeps the v0.1.0 bus frames unchanged.
type Plant struct {
	params Params
	state  State
	port   int
	seed   uint64
	t      time.Duration // plant time: the sum of every positive dt stepped so far
}

// NewPlant returns a plant at rest (Initial) for port with noise seed seed.
func NewPlant(p Params, port int, seed uint64) *Plant {
	return &Plant{params: p, state: Initial(p), port: port, seed: seed}
}

// Step advances the plant by dt under the commanded duty. A non-positive dt
// changes nothing.
func (pl *Plant) Step(duty float64, dt time.Duration) {
	if dt <= 0 {
		return
	}
	pl.state = Step(pl.params, pl.state, duty, dt)
	pl.t += dt
}

// State is the dynamic state of the shaft.
func (pl *Plant) State() State { return pl.state }

// Shaft is the noise-free shaft rpm: what status "rpm" shows.
func (pl *Plant) Shaft() float64 { return pl.state.RPM }

// Running reports whether the fan counts as running (not stopped, not coasting
// after a stop).
func (pl *Plant) Running() bool { return pl.state.Running }

// Params are the parameters the plant was built with.
func (pl *Plant) Params() Params { return pl.params }

// Frame is the value of the next port<N>.rpm bus frame. Plan 04-04 puts the
// pulse accumulator behind it; for an ideal tach it is the shaft rpm.
func (pl *Plant) Frame() float64 { return pl.state.RPM }

// Current is the value of the rpm entry of a hello in-snapshot. Plan 04-04
// puts the pulse accumulator behind it; for an ideal tach it is the shaft rpm.
func (pl *Plant) Current() float64 { return pl.state.RPM }

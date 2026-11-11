package model

import (
	"math"
	"time"
)

// Plant is one port's fan: the engine's only seam into the model (and the one
// an offline replay uses later). It owns the parameters, the dynamic state and
// the plant time, and exposes the shaft truth plus what the firmware's tach
// path sees.
//
// A fan with a [tach] table (Params.Tach.PPR > 0) counts whole pulses from the
// exact rpm integral of every step and reports them through Frame, Current and
// Reading. A fan without it (ideal tach) sends the shaft rpm exactly as v0.1.0:
// its Step is the one advance call of the model, so the shaft, Running, Frame
// and Current of every schema 1 fan are bit-identical to v0.1.0. Both kinds
// share the 1 s gate of Reading.
//
// seed and port key the noise of the tach path (noise.go): a stateless hash of
// (seed, port, 100 ms bucket), so it does not depend on the tick timing.
type Plant struct {
	params Params
	state  State
	port   int
	seed   uint64
	t      time.Duration // plant time: the sum of every positive dt stepped so far
	tach   Tach
}

// pulseEps absorbs the float rounding of the phase accumulator when a pulse
// falls exactly on a step or gate edge (a fan on the pulse grid): a phase within
// 1e-9 pulse below a whole pulse counts as that pulse, on every tick pattern.
const pulseEps = 1e-9

// NewPlant returns a plant at rest (Initial) for port with noise seed seed. The plant works on its
// own deep copy of p (Params.Clone), so a later change to p's curves cannot reach a running model.
func NewPlant(p Params, port int, seed uint64) *Plant {
	p = p.Clone()
	return &Plant{params: p, state: Initial(p), port: port, seed: seed, tach: Tach{gateEnd: TachGate}}
}

// Step advances the plant by dt under the commanded duty. A non-positive dt
// changes nothing.
func (pl *Plant) Step(duty float64, dt time.Duration) {
	if dt <= 0 {
		return
	}
	if pl.params.Tach.PPR == 0 {
		pl.stepIdeal(duty, dt)
		return
	}
	pl.stepTach(duty, dt)
}

// stepIdeal is the ideal-tach Step: exactly one advance call whose State becomes
// the plant state (no split at gate edges, so the shaft is the v0.1.0 shaft bit
// for bit). The 1 s gate reading is bookkeeping beside it: the step's rpm
// integral goes into the running gate, and where a gate edge falls inside the
// step, the area up to the edge comes from a second advance call on the step's
// start state whose State is discarded. The rest of the step counts towards the
// next gate.
func (pl *Plant) stepIdeal(duty float64, dt time.Duration) {
	t0, start := pl.t, pl.state
	next, in := advance(pl.params, start, duty, dt)
	pl.state = next
	pl.t += dt

	g := &pl.tach
	c := 0.0 // the part of this step's area already assigned to a gate
	for g.gateEnd <= pl.t {
		_, part := advance(pl.params, start, duty, g.gateEnd-t0)
		g.reading = (g.gateArea + part.Area - c) / TachGate.Seconds()
		g.haveReading = true
		g.gateArea, c = 0, part.Area
		g.gateEnd += TachGate
	}
	g.gateArea += in.Area - c
}

// stepTach is the Step of a fan with a pulse tach. It splits dt at the
// NoiseBucket edges of plant time (every gate edge is a bucket edge) so that the
// pulse count does not depend on how the ticks fall, advances the shaft over each
// piece and counts the revolutions of its exact integral.
func (pl *Plant) stepTach(duty float64, dt time.Duration) {
	g := &pl.tach
	ppr := float64(pl.params.Tach.PPR)
	for rem := dt; rem > 0; {
		bucket := int64(pl.t / NoiseBucket)
		piece := min(rem, time.Duration(bucket+1)*NoiseBucket-pl.t)
		next, in := advance(pl.params, pl.state, duty, piece)
		pl.state = next

		// The noise is a ripple of noise_rpm*noiseScale*z rpm over the part of the piece the shaft
		// turns (MovingS), so a fan at rest never emits a pulse. It enters only the pulse count:
		// the shaft, Running, airflow and power stay noise-free (D-13, D-03 as amended). A fan
		// never runs backwards: the pulse phase never decreases.
		area := in.Area
		if n := pl.params.Tach.NoiseRPM; n > 0 {
			area += n * noiseScale * z(pl.seed, pl.port, bucket) * in.MovingS
		}
		g.phase += max(area, 0) / 60 * ppr
		n := math.Floor(g.phase + pulseEps)
		g.phase -= n
		g.pulses += int64(n)
		g.gatePulses += int64(n)

		pl.t += piece
		rem -= piece
		for pl.t >= g.gateEnd {
			g.reading = float64(g.gatePulses) * 60 / (BoardPPR * TachGate.Seconds())
			g.haveReading = true
			g.gatePulses = 0
			g.gateEnd += TachGate
		}
	}
}

// State is the dynamic state of the shaft.
func (pl *Plant) State() State { return pl.state }

// Shaft is the noise-free shaft rpm: what status "rpm" shows.
func (pl *Plant) Shaft() float64 { return pl.state.RPM }

// Running reports whether the fan counts as running (not stopped, not coasting
// after a stop).
func (pl *Plant) Running() bool { return pl.state.Running }

// Params are the parameters the plant was built with. The result is a deep copy: changing it does
// not change the plant, whose parameters are immutable once built.
func (pl *Plant) Params() Params { return pl.params.Clone() }

// Frame is the value of the next port<N>.rpm bus frame, in rpm.
//
// For an ideal tach it is the shaft rpm, exactly as v0.1.0. For a tach fan it
// drains the whole pulses counted since the previous Frame and returns
// pulses*60/(BoardPPR*elapsed), where elapsed is the plant time since that
// Frame: at the 50 ms frame cadence and board ppr 2 one pulse is 600 rpm, and the
// firmware's 1 s mean of such frames moves in 30 rpm steps. Normalising by the
// elapsed plant time keeps a late tick or a stretch without frames (the bus was
// down) a true mean pulse rate. With no time elapsed it returns the last value.
func (pl *Plant) Frame() float64 {
	if pl.params.Tach.PPR == 0 {
		return pl.state.RPM
	}
	g := &pl.tach
	elapsed := pl.t - g.lastFrameT
	if elapsed <= 0 {
		return g.last
	}
	g.last = float64(g.pulses) * 60 / (BoardPPR * elapsed.Seconds())
	g.pulses = 0
	g.lastFrameT = pl.t
	return g.last
}

// Current is the value of the rpm entry of a hello in-snapshot, in rpm: the shaft
// rpm for an ideal tach, the last Frame value for a tach fan. It never drains
// pulses, so a firmware reconnect does not steal them from the next Frame.
func (pl *Plant) Current() float64 {
	if pl.params.Tach.PPR == 0 {
		return pl.state.RPM
	}
	return pl.tach.last
}

// Reading is the tach reading of the last completed 1 s gate, in rpm: 30 times
// the pulses counted in the gate for a tach fan (board ppr 2), the exact
// time-weighted mean of the shaft rpm over the gate for an ideal tach (what the
// firmware's 1 s window reports for the ideal frames, up to its window phase). A
// fan at rest reads 0. The gate edges are at whole seconds of plant time, so ok is
// false only for the first TachGate after NewPlant (sim start, sim restart, fan
// swap).
func (pl *Plant) Reading() (rpm float64, ok bool) { return pl.tach.reading, pl.tach.haveReading }

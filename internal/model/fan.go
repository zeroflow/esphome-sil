// Package model holds the pure fan model: no clock, no I/O.
package model

import (
	"maps"
	"math"
	"slices"
	"time"
)

// Params describes one fan model. Taus are in seconds.
//
// The flat legacy fields (MinDuty .. TauDown) are the whole of a schema 1 file.
// The fields below them belong to schema 2 and their zero value is the v0.1.0
// behaviour, so a schema 1 Params needs none of them. A schema 2 file fills the
// legacy fields too (MinDuty = stop.duty, StartDuty = start.duty, MaxRPM and
// MinRPM from the curves, BelowMin derived), which keeps everything that reads
// them (fan info, the page's fixed axis, Initial) working unchanged.
//
// Params holds slices and a map, so it is not comparable with ==.
type Params struct {
	Name, Source string
	MinDuty      float64
	StartDuty    float64
	BelowMin     string // "stop" or "hold"
	MinRPM       float64
	MaxRPM       float64
	TauUp        float64
	TauDown      float64

	// Curve is the duty to rpm table of each direction. Empty means the legacy
	// linear relation synthesised from the flat fields.
	Curve Curves
	// StartDelay is how long duty must stay at or above StartDuty before a
	// stopped fan starts (plan 04-02). Zero means no delay.
	StartDelay time.Duration
	// CoastTau is the time constant, in seconds, of the unpowered coast-down
	// after a stop (plan 04-02). Zero means TauDown.
	CoastTau float64
	// Tach describes the tachometer (plan 04-04). The zero value is an ideal
	// continuous tach.
	Tach TachParams
	// Datasheet holds the reference airflow and power (plan 04-06). Nil means
	// the file has no datasheet values.
	Datasheet *Datasheet
	// Schema is the file format: 0 and 1 are the flat v0.1.0 format, 2 is schema 2.
	Schema int
	// Provenance is the per-value provenance of a schema 2 file, in ValuePaths
	// order. Nil for schema 1.
	Provenance []ProvEntry
	// Sources are the named source ids of a schema 2 file ([sources]). Nil for schema 1.
	Sources map[string]string
}

// Clone returns a deep copy of p: the curves, the provenance, the sources and the datasheet are
// copied, so nothing in the result aliases p. A nil slice, map or pointer stays nil.
func (p Params) Clone() Params {
	p.Curve = Curves{Up: slices.Clone(p.Curve.Up), Down: slices.Clone(p.Curve.Down)}
	p.Provenance = slices.Clone(p.Provenance)
	p.Sources = maps.Clone(p.Sources)
	if p.Datasheet != nil {
		d := *p.Datasheet
		p.Datasheet = &d
	}
	return p
}

// TachParams describes the tachometer of a fan.
type TachParams struct {
	// PPR is the pulses per revolution. 0 means an ideal tach.
	PPR int
	// NoiseRPM is the standard deviation, in rpm, of the noise in one 1 s tach reading.
	NoiseRPM float64
}

// Datasheet holds the vendor reference values of a fan at RefRPM.
type Datasheet struct {
	AirflowM3h float64
	PowerW     float64
	RefRPM     float64
}

// ProvEntry is the provenance of one value of a schema 2 file.
type ProvEntry struct {
	// Path is the dotted value path, one of ValuePaths.
	Path string
	// Kind is one of Kinds.
	Kind string
	// Source is the id of a [sources] entry; empty means no source.
	Source string
}

// The provenance kinds of D-05.
const (
	KindAssumed      = "assumed"
	KindDatasheet    = "datasheet"
	KindMeasured     = "measured"
	KindUnobservable = "unobservable"
)

// Kinds lists the valid provenance kinds.
var Kinds = []string{KindAssumed, KindDatasheet, KindMeasured, KindUnobservable}

// ValuePaths is the ordered registry of the dotted value paths that take a
// provenance entry. A curve table counts as one value. measured.tolerance is
// the one [measured] key that is a value; the other [measured] keys identify
// the measurement record and are not values. The order is the one `sil fans
// --verbose` prints.
var ValuePaths = []string{
	"stop.duty", "start.duty", "start.delay_s",
	"dynamics.tau_up_s", "dynamics.tau_down_s", "dynamics.coast_tau_s",
	"curve.up", "curve.down",
	"tach.ppr", "tach.noise_rpm",
	"datasheet.airflow_m3h", "datasheet.power_w", "datasheet.ref_rpm",
	"measured.tolerance",
}

// State is the fan's dynamic state. Every field is comparable and the zero
// value is a fan at rest on the rising branch.
type State struct {
	Running bool
	RPM     float64
	// LastDuty is the clamped duty of the previous Step. It is only tracked
	// for a fan with a separate down curve (see Step).
	LastDuty float64
	// Falling is the sticky direction: true after the duty fell, false after it
	// rose. Only tracked together with LastDuty.
	Falling bool
	// StartElapsed is how long the duty has been at or above StartDuty while
	// the fan waits to start (plan 04-02).
	StartElapsed time.Duration
}

// Initial is the state of a fan at rest. A "hold" fan never stops, so it
// counts as running (at rpm 0) until the first Step spins it up to min_rpm.
func Initial(p Params) State {
	return State{Running: p.BelowMin == "hold"}
}

// Integral is what advance reports besides the new state: the exact integral of
// the shaft rpm over the step.
type Integral struct {
	// Area is the exact integral of rpm over the step, in rpm*s (revolutions = Area/60).
	Area float64
	// MovingS is how many seconds of the step the shaft turns (rpm > 0).
	MovingS float64
}

// Step advances s by dt under the commanded duty. It is a pure function of
// its arguments: no clock, no I/O, so ten 100 ms steps equal one 1 s step
// (also across a start-delay expiry and a coast-to-zero snap). A non-positive
// dt returns s unchanged. It is advance without the integral.
//
// Semantics (D-09..D-13); the zero value of every schema 2 field is v0.1.0:
//   - A running fan keeps running down to MinDuty. Below MinDuty it stops at
//     once (BelowMin "stop", target 0) or holds the floor rpm (BelowMin "hold",
//     the never-stops form).
//   - A stopped fan starts only when duty has been at or above StartDuty for
//     StartDelay (duty >= StartDuty, never >). The delay accumulates in
//     State.StartElapsed; a duty below StartDuty re-arms it. A step that
//     contains the expiry is split there: the part before runs stopped, the
//     part after runs running. With StartDelay 0 every start is one segment,
//     exactly the v0.1.0 order. There is no kick and a coasting rotor is not
//     caught: the fan re-accelerates from its current coast rpm.
//   - Between MinDuty and StartDuty the previous running state is kept.
//     Hysteresis follows duty only, so a coasting stopped fan with duty back in
//     the band stays stopped.
//   - While running the target is read from the duty to rpm curve (Params.Curve,
//     or the linear relation from MinRPM at MinDuty to MaxRPM at duty 1 that
//     the legacy fields synthesise): the up curve while the duty rises, the down
//     curve after it fell (sticky). rpm follows it with TauUp when rising and
//     TauDown when falling via rpm += (target - rpm) * (1 - exp(-dt/tau)); an
//     rpm equal to the target is left alone.
//   - A stopped fan has target 0 and coasts down with CoastTau (TauDown when
//     CoastTau is 0).
//   - The exponential never reaches 0, so rpm snaps to 0 at the end of a
//     segment when the target is 0 and rpm < 0.5.
func Step(p Params, s State, duty float64, dt time.Duration) State {
	out, _ := advance(p, s, duty, dt)
	return out
}

// advance is Step with the exact integral of rpm over the step. The pulse
// accumulator of the tach (plan 04-04) counts revolutions from it, so counts
// stay exact across a delay expiry and a coast snap.
func advance(p Params, s State, duty float64, dt time.Duration) (State, Integral) {
	if dt <= 0 {
		return s, Integral{}
	}
	switch {
	case math.IsNaN(duty): // never let a bad input poison the state
		duty = 0
	case duty < 0:
		duty = 0
	case duty > 1:
		duty = 1
	}

	// Start and stop. pre is the part of the step that still runs stopped before a
	// delayed start takes effect.
	running := s.Running
	elapsed := s.StartElapsed
	var pre time.Duration
	switch {
	case running:
		if duty < p.MinDuty {
			running = p.BelowMin == "hold" // stop is immediate (D-11)
		}
		elapsed = 0
	case duty >= p.StartDuty:
		if elapsed+dt >= p.StartDelay {
			pre = max(p.StartDelay-elapsed, 0)
			running = true
			elapsed = 0
		} else {
			elapsed += dt
		}
	default:
		elapsed = 0
	}

	// Direction (Assumption A1): the sign of the most recent duty change picks the
	// curve, rising uses Up and falling uses Down, an unchanged duty keeps the
	// branch it arrived on, and a fan starts on Up. There is no deadband. Only a
	// fan with a separate down curve needs the direction, so only its state
	// carries it; a legacy fan's state stays exactly the v0.1.0 state.
	tracked := len(p.Curve.Down) > 0
	falling := s.Falling
	if tracked {
		switch {
		case duty > s.LastDuty:
			falling = false
		case duty < s.LastDuty:
			falling = true
		}
	}

	rpm := s.RPM
	var in Integral
	rest := dt
	if pre > 0 { // the stopped part before the start; a zero-length segment is never applied
		var area, moving float64
		rpm, area, moving = segment(p, rpm, 0, false, pre)
		in.Area += area
		in.MovingS += moving
		rest -= pre
	}
	if rest > 0 {
		target := 0.0
		switch {
		case !running:
		case falling:
			target = p.downCurve().At(duty)
		default:
			target = p.upCurve().At(duty)
		}
		var area, moving float64
		rpm, area, moving = segment(p, rpm, target, running, rest)
		in.Area += area
		in.MovingS += moving
	}

	out := State{Running: running, RPM: rpm, StartElapsed: elapsed}
	if tracked {
		out.LastDuty, out.Falling = duty, falling
	}
	return out, in
}

// segment runs one stretch of length h at a constant target. It returns the rpm
// at its end (the v0.1.0 exponential and snap, unchanged), the exact integral
// of rpm over it in rpm*s and the seconds in it with rpm > 0.
func segment(p Params, rpm0, target float64, running bool, h time.Duration) (rpm, area, movingS float64) {
	sec := h.Seconds()
	rpm = rpm0
	tau := p.TauDown
	switch {
	case target > rpm0:
		tau = p.TauUp
	case !running && p.CoastTau > 0:
		tau = p.CoastTau // unpowered coast-down (D-09)
	}
	if target != rpm {
		if tau > 0 {
			rpm += (target - rpm) * (1 - math.Exp(-sec/tau))
		} else {
			rpm = target // degenerate params; fanlib rejects tau <= 0
		}
	}
	snapped := false
	if target == 0 && rpm < 0.5 {
		snapped = rpm0 > 0
		rpm = 0
	}

	// The integral follows the unsnapped exponential. A segment that snapped
	// integrates only up to the crossing time of 0.5 rpm, so the area does not
	// depend on where tick boundaries fall.
	upTo := sec
	if snapped {
		upTo = 0
		if tau > 0 {
			upTo = min(max(tau*math.Log(rpm0/0.5), 0), sec)
		}
		movingS = upTo
	} else if rpm0 > 0 || target > 0 {
		movingS = sec
	}
	switch {
	case target == rpm0:
		area = rpm0 * upTo
	case tau <= 0:
		area = target * upTo
	default:
		area = target*upTo - (rpm0-target)*tau*math.Expm1(-upTo/tau)
	}
	return rpm, area, movingS
}

package model

import "time"

// The tach constants of the board the simulator stands in for.
const (
	// BoardPPR is the pulses per revolution the board's tach input assumes: the
	// pulse_counter multiply of 0.5 turns pulses per minute into rpm, so one
	// counted pulse in a 1 s gate reads 30 rpm. Assumption A11: confirmed on
	// Rev 1.0-3.1 YAML only, to be confirmed on Rev 3.3 hardware (Phase 7/8).
	BoardPPR = 2
	// TachGate is the length of the tach reading window: Reading is the value of
	// the last completed gate.
	TachGate = time.Second
	// NoiseBucket is the width of one noise bucket (plan 04-04, MODEL-10): the
	// ripple is constant within a bucket, so it does not depend on the tick timing.
	NoiseBucket = 100 * time.Millisecond
)

// Tach is the pulse accumulator and the 1 s gate state of one plant. Its fields
// are unexported and it has no constructor: the Plant owns it.
//
// A fan with a [tach] table (Params.Tach.PPR > 0) counts whole pulses from the
// exact rpm integral: phase carries the fractional pulse between steps, pulses
// are the whole ones since the last Frame, gatePulses the ones of the running
// 1 s gate. A fan without it (ideal tach) keeps the exact rpm integral of the
// running gate in gateArea instead. The gate is shared by both kinds: its edges
// are at whole seconds of plant time, and Reading is unavailable only until the
// first gate has ended.
type Tach struct {
	phase       float64 // fractional pulses carried between steps
	pulses      int64   // whole pulses since the last Frame
	gatePulses  int64   // whole pulses in the running gate (tach fan)
	gateArea    float64 // rpm*s of the running gate (ideal tach)
	gateEnd     time.Duration
	reading     float64 // value of the last completed gate, in rpm
	haveReading bool
	last        float64 // the last Frame value
	lastFrameT  time.Duration
}

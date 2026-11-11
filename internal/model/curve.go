package model

// Point is one table point of a duty to rpm curve.
type Point struct {
	Duty, RPM float64
}

// Curve is a duty to rpm table: duty strictly increasing, rpm non-decreasing
// (the fan file loader enforces both, so At never divides by zero and is
// monotone by construction).
type Curve []Point

// Curves are the two directions of the duty to rpm relation. Up applies while
// the duty rises, Down while it falls. An empty Down means both directions use
// Up. Both empty means the legacy linear relation of the flat v0.1.0 fields.
type Curves struct {
	Up, Down Curve
}

// At is the steady-state rpm at duty: the straight line between the two
// neighbouring points, the first point's rpm below the first point and the
// last point's rpm above the last point. It never extrapolates. An empty curve
// is 0.
//
// The interpolation is written a.RPM + (duty-a.Duty)/(b.Duty-a.Duty)*(b.RPM-a.RPM),
// the operation order of the v0.1.0 linear target, so the curve synthesised
// from legacy fields gives bit-identical results.
func (c Curve) At(duty float64) float64 {
	if len(c) == 0 {
		return 0
	}
	if duty <= c[0].Duty {
		return c[0].RPM
	}
	for i := 1; i < len(c); i++ {
		if duty <= c[i].Duty {
			a, b := c[i-1], c[i]
			return a.RPM + (duty-a.Duty)/(b.Duty-a.Duty)*(b.RPM-a.RPM)
		}
	}
	return c[len(c)-1].RPM
}

// Max is the highest rpm of the curve (0 for an empty curve).
func (c Curve) Max() float64 {
	m := 0.0
	for i, p := range c {
		if i == 0 || p.RPM > m {
			m = p.RPM
		}
	}
	return m
}

// Min is the lowest rpm of the curve (0 for an empty curve).
func (c Curve) Min() float64 {
	m := 0.0
	for i, p := range c {
		if i == 0 || p.RPM < m {
			m = p.RPM
		}
	}
	return m
}

// upCurve is the curve the fan follows while its duty rises: Curve.Up when
// set, else the legacy relation synthesised from the flat fields (Assumption
// A3 / D-10 amendment). A "hold" fan with MinDuty > 0 holds MinRPM below
// MinDuty, which is the extra point (0, MinRPM); every other legacy fan is the
// straight line from (MinDuty, MinRPM) to (1, MaxRPM).
func (p Params) upCurve() Curve {
	if len(p.Curve.Up) > 0 {
		return p.Curve.Up
	}
	if p.BelowMin == "hold" && p.MinDuty > 0 {
		return Curve{{0, p.MinRPM}, {p.MinDuty, p.MinRPM}, {1, p.MaxRPM}}
	}
	return Curve{{p.MinDuty, p.MinRPM}, {1, p.MaxRPM}}
}

// downCurve is the curve the fan follows while its duty falls: Curve.Down when
// set, else the up curve.
func (p Params) downCurve() Curve {
	if len(p.Curve.Down) > 0 {
		return p.Curve.Down
	}
	return p.upCurve()
}

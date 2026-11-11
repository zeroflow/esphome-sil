package model

import (
	"math"
	"testing"
	"time"
)

func TestFanLaws(t *testing.T) {
	d := Datasheet{AirflowM3h: 95.7, PowerW: 1.2, RefRPM: 1800}
	cases := []struct{ rpm, air, pow float64 }{
		{1800, 95.7, 1.2},
		{900, 47.85, 0.15}, // airflow with rpm, power with rpm cubed: 1.2 / 8
		{0, 0, 0},          // a value, since the fan has a datasheet
		{3600, 191.4, 9.6}, // above the reference the laws keep going
	}
	for _, tc := range cases {
		if got := d.Airflow(tc.rpm); math.Abs(got-tc.air) > 1e-9 {
			t.Errorf("Airflow(%v) = %v, want %v", tc.rpm, got, tc.air)
		}
		if got := d.Power(tc.rpm); math.Abs(got-tc.pow) > 1e-9 {
			t.Errorf("Power(%v) = %v, want %v", tc.rpm, got, tc.pow)
		}
	}
}

// steadyPlant is a plant with a flat 1000 rpm curve above the stop duty, stepped to its steady state.
func steadyPlant(ds *Datasheet, noise float64) *Plant {
	p := Params{
		Name: "outputs", Schema: 2, MinDuty: 0.05, StartDuty: 0.1, BelowMin: "stop",
		TauUp: 0.2, TauDown: 0.2, CoastTau: 0.2,
		Curve:  Curves{Up: Curve{{Duty: 0.05, RPM: 1000}, {Duty: 1, RPM: 1000}}},
		MinRPM: 1000, MaxRPM: 1000,
		Tach:      TachParams{PPR: 2, NoiseRPM: noise},
		Datasheet: ds,
	}
	pl := NewPlant(p, 1, 7)
	for i := 0; i < 2000; i++ {
		pl.Step(0.5, 10*time.Millisecond)
	}
	return pl
}

func TestOutputsWithoutDatasheet(t *testing.T) {
	pl := steadyPlant(nil, 0)
	if a, w, ok := pl.Outputs(); ok || a != 0 || w != 0 {
		t.Fatalf("Outputs without a datasheet = %v, %v, %v, want ok false", a, w, ok)
	}
}

func TestOutputsAreNoiseFree(t *testing.T) {
	ds := &Datasheet{AirflowM3h: 100, PowerW: 2, RefRPM: 2000}
	pl := steadyPlant(ds, 50)
	a, w, ok := pl.Outputs()
	if !ok {
		t.Fatal("Outputs ok false for a fan with a datasheet")
	}
	shaft := pl.Shaft()
	if shaft < 999 || shaft > 1001 {
		t.Fatalf("shaft %v, want about 1000", shaft)
	}
	if wantA, wantW := ds.Airflow(shaft), ds.Power(shaft); a != wantA || w != wantW {
		t.Fatalf("Outputs = %v, %v, want exactly the fan laws of the shaft rpm: %v, %v", a, w, wantA, wantW)
	}
	// The tach reading does carry noise at this setting; the outputs must not follow it.
	if r, ok := pl.Reading(); !ok || r == pl.Shaft() {
		t.Logf("tach reading %v (ok %v) happens to equal the shaft; outputs are still derived from Shaft()", r, ok)
	}
}

package model

import (
	"math"
	"testing"
	"time"
)

// tachFan is a literal schema 2 fan that holds rpm at any duty >= 0.05, with the given tach.
// The short taus make the spin-up settle within a second.
func tachFan(rpm float64, ppr int, noise float64) Params {
	return Params{
		Name: "tach", Schema: 2, MinDuty: 0.05, StartDuty: 0.1, BelowMin: "stop",
		MinRPM: rpm, MaxRPM: rpm, TauUp: 0.05, TauDown: 0.05, CoastTau: 0.05,
		Curve: Curves{Up: Curve{{0.05, rpm}, {1, rpm}}},
		Tach:  TachParams{PPR: ppr, NoiseRPM: noise},
	}
}

// idealFan is tachFan without a [tach] table, with the spin-up time constant tau.
func idealFan(rpm, tau float64) Params {
	p := tachFan(rpm, 0, 0)
	p.TauUp, p.TauDown, p.CoastTau = tau, tau, tau
	return p
}

// run steps pl under a constant duty for total plant time in steps of dt.
func run(pl *Plant, duty float64, total, dt time.Duration) {
	for done := time.Duration(0); done < total; done += dt {
		pl.Step(duty, min(dt, total-done))
	}
}

// gateLog maps a completed gate (1 is the first second) to the reading the plant showed right
// after the step that completed it. A step that completes several gates logs only the last.
type gateLog map[int]float64

// runPattern steps pl under a constant duty through the cycled step pattern until exactly total
// plant time has passed (the last step is cut short) and logs the gates that completed.
func runPattern(pl *Plant, duty float64, pattern []time.Duration, total time.Duration) gateLog {
	log := gateLog{}
	seen := 0
	for done, i := time.Duration(0), 0; done < total; i++ {
		dt := min(pattern[i%len(pattern)], total-done)
		pl.Step(duty, dt)
		done += dt
		if g := int(pl.tach.gateEnd/TachGate) - 1; g > seen {
			seen = g
			if r, ok := pl.Reading(); ok {
				log[g] = r
			}
		}
	}
	return log
}

var tickPatterns = map[string][]time.Duration{
	"10ms":     {10 * time.Millisecond},
	"7ms":      {7 * time.Millisecond},
	"10/30/3":  {10 * time.Millisecond, 30 * time.Millisecond, 3 * time.Millisecond},
	"1s":       {time.Second},
	"3s":       {3 * time.Second},
	"10/30/3b": {3 * time.Millisecond, 30 * time.Millisecond, 10 * time.Millisecond},
}

func TestTachPulsesFromIntegral(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 0), 1, 1)
	run(pl, 0.5, 10*time.Second, 10*time.Millisecond) // spin up and settle
	before := pl.tach.pulses
	run(pl, 0.5, 10*time.Second, 10*time.Millisecond)
	got := pl.tach.pulses - before
	// 1100 rpm at ppr 2 is 36.67 pulses/s: 366.67 in 10 s, so 366 or 367 whole pulses.
	if got != 366 && got != 367 {
		t.Fatalf("pulses in 10 s at 1100 rpm, ppr 2 = %d, want 366 or 367", got)
	}
}

func TestFrameUnits(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 0), 1, 1)
	run(pl, 0.5, 5*time.Second, 10*time.Millisecond)
	pl.Frame() // aligns the frame clock
	run(pl, 0.5, 50*time.Millisecond, 10*time.Millisecond)
	n := pl.tach.pulses
	got := pl.Frame()
	if n < 1 || math.Abs(got-float64(n)*600) > 1e-6 {
		t.Fatalf("Frame after 50 ms = %v with %d pulses, want %d x 600", got, n, n)
	}
	if math.Mod(got/600, 1) > 1e-9 && math.Mod(got/600, 1) < 1-1e-9 {
		t.Fatalf("Frame %v is not a whole multiple of 600", got)
	}

	run(pl, 0.5, 120*time.Millisecond, 10*time.Millisecond)
	n = pl.tach.pulses
	got = pl.Frame()
	want := float64(n) * 60 / (BoardPPR * 0.12)
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("Frame after a 120 ms gap = %v with %d pulses, want %v", got, n, want)
	}
	if again := pl.Frame(); again != got {
		t.Fatalf("Frame with no time elapsed = %v, want the last value %v", again, got)
	}
}

func TestCurrentDoesNotDrain(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 0), 1, 1)
	run(pl, 0.5, 5*time.Second, 10*time.Millisecond)
	last := pl.Frame()
	run(pl, 0.5, 50*time.Millisecond, 10*time.Millisecond)
	n := pl.tach.pulses
	if n < 1 {
		t.Fatalf("no pulse counted in 50 ms at 1100 rpm; the test would prove nothing")
	}
	if c := pl.Current(); c != last {
		t.Fatalf("Current() = %v, want the last Frame value %v", c, last)
	}
	if pl.tach.pulses != n {
		t.Fatalf("Current drained pulses: %d before, %d after", n, pl.tach.pulses)
	}
	if got := pl.Frame(); math.Abs(got-float64(n)*600) > 1e-6 {
		t.Fatalf("Frame after Current = %v, want %d x 600 (every pulse since the last Frame)", got, n)
	}
}

func TestGateReading(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 0), 1, 1)
	var cum []int64 // pulses at every whole second of plant time
	for i := 0; i < 600; i++ {
		pl.Step(0.5, 10*time.Millisecond)
		sec := time.Duration(i+1) * 10 * time.Millisecond
		r, ok := pl.Reading()
		if sec < TachGate && ok {
			t.Fatalf("Reading available at %v, want unavailable before the first second", sec)
		}
		if sec%TachGate == 0 {
			cum = append(cum, pl.tach.pulses)
			k := len(cum)
			prev := int64(0)
			if k > 1 {
				prev = cum[k-2]
			}
			if want := 30 * float64(cum[k-1]-prev); !ok || r != want {
				t.Fatalf("Reading after second %d = %v (ok %v), want 30 x %d pulses = %v", k, r, ok, cum[k-1]-prev, want)
			}
		} else if ok {
			if want := 30 * float64(cum[len(cum)-1]-pulsesBefore(cum, len(cum)-1)); r != want {
				t.Fatalf("Reading at %v = %v, want it to hold the last gate's %v", sec, r, want)
			}
		}
	}
	if len(cum) != 6 {
		t.Fatalf("recorded %d gates, want 6", len(cum))
	}
}

func pulsesBefore(cum []int64, i int) int64 {
	if i == 0 {
		return 0
	}
	return cum[i-1]
}

func TestIdealTachIsShaft(t *testing.T) {
	for name, pattern := range tickPatterns {
		pl := NewPlant(idealFan(1100, 0.5), 1, 1)
		for done, i := time.Duration(0), 0; done < 4*time.Second; i++ {
			dt := pattern[i%len(pattern)]
			pl.Step(0.5, dt)
			done += dt
			if pl.Frame() != pl.Shaft() || pl.Current() != pl.Shaft() {
				t.Fatalf("%s: Frame %v / Current %v != Shaft %v", name, pl.Frame(), pl.Current(), pl.Shaft())
			}
			if _, ok := pl.Reading(); ok != (pl.t >= TachGate) {
				t.Fatalf("%s: Reading available %v at plant time %v, want %v", name, ok, pl.t, pl.t >= TachGate)
			}
		}
	}
}

func TestIdealReading(t *testing.T) {
	const total = 6 * time.Second
	t.Run("steady equals the shaft", func(t *testing.T) {
		for name, pattern := range tickPatterns {
			pl := NewPlant(idealFan(1100, 0.5), 1, 1)
			pl.state = State{Running: true, RPM: 1100}
			log := runPattern(pl, 0.5, pattern, total)
			if len(log) == 0 {
				t.Fatalf("%s: no gate completed", name)
			}
			for g, r := range log {
				if relDiff(r, 1100) > 1e-9 {
					t.Errorf("%s: reading of gate %d = %v, want 1100", name, g, r)
				}
			}
		}
	})
	t.Run("spin-up mean is analytic and independent of the ticks", func(t *testing.T) {
		ref := runPattern(NewPlant(idealFan(1100, 0.5), 1, 1), 0.5, tickPatterns["10ms"], total)
		if len(ref) != 6 {
			t.Fatalf("reference logged %d gates, want 6", len(ref))
		}
		want := 1100 * (1 - 0.5*(1-math.Exp(-2)))
		if relDiff(ref[1], want) > 1e-9 {
			t.Fatalf("first reading = %v, want the analytic mean %v", ref[1], want)
		}
		for name, pattern := range tickPatterns {
			log := runPattern(NewPlant(idealFan(1100, 0.5), 1, 1), 0.5, pattern, total)
			if len(log) == 0 {
				t.Fatalf("%s: no gate completed", name)
			}
			for g, r := range log {
				if relDiff(r, ref[g]) > 1e-9 {
					t.Errorf("%s: reading of gate %d = %v, want %v", name, g, r, ref[g])
				}
			}
		}
	})
	t.Run("a fan at rest reads 0", func(t *testing.T) {
		pl := NewPlant(idealFan(1100, 0.5), 1, 1)
		run(pl, 0, 2*time.Second, 10*time.Millisecond)
		if r, ok := pl.Reading(); !ok || r != 0 {
			t.Fatalf("Reading of a fan at rest = %v (ok %v), want exactly 0 and available", r, ok)
		}
	})
	t.Run("a new plant is unavailable for its first second", func(t *testing.T) {
		pl := NewPlant(idealFan(1100, 0.5), 1, 1)
		run(pl, 0.5, 2*time.Second, 10*time.Millisecond)
		if _, ok := pl.Reading(); !ok {
			t.Fatal("Reading unavailable after 2 s")
		}
		pl = NewPlant(idealFan(1100, 0.5), 1, 1) // a swap
		run(pl, 0.5, 990*time.Millisecond, 10*time.Millisecond)
		if _, ok := pl.Reading(); ok {
			t.Fatal("Reading available 990 ms after NewPlant")
		}
		pl.Step(0.5, 10*time.Millisecond)
		if _, ok := pl.Reading(); !ok {
			t.Fatal("Reading unavailable once the first second is complete")
		}
	})
}

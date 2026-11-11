package model

import (
	"math"
	"slices"
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

// EDGE-MODEL-09: the engine sends no frames while the bus is down, so the first Frame after a
// reconnect covers the whole gap. It is the true mean pulse rate over the gap (normalised by the
// elapsed plant time), with a resolution of one pulse over the gap, not a burst or a zero.
func TestFrameAfterGapIsMeanRate(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 0), 1, 1)
	run(pl, 0.5, 5*time.Second, 10*time.Millisecond)
	pl.Frame()
	run(pl, 0.5, 7*time.Second, 10*time.Millisecond)
	n := pl.tach.pulses
	got := pl.Frame()
	if want := float64(n) * 60 / (BoardPPR * 7); math.Abs(got-want) > 1e-9 {
		t.Fatalf("Frame after a 7 s gap = %v with %d pulses, want %v", got, n, want)
	}
	if math.Abs(got-1100) > 30.0/7 {
		t.Fatalf("Frame after a 7 s gap = %v, want the mean rate 1100 within one pulse over the gap (%v)", got, 30.0/7)
	}
}

// gateReadings logs the first n gate readings of pl under a constant duty with 10 ms steps and
// returns those after the first drop gates.
func gateReadings(pl *Plant, duty float64, n, drop int) []float64 {
	log := runPattern(pl, duty, tickPatterns["10ms"], time.Duration(n)*TachGate)
	var out []float64
	for g := drop + 1; g <= n; g++ {
		out = append(out, log[g])
	}
	return out
}

func stdDev(v []float64) (mean, sd float64) {
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	for _, x := range v {
		sd += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(sd / float64(len(v)))
}

func TestNoiseMoments(t *testing.T) {
	const n = 200000
	var sum, sq float64
	for b := int64(0); b < n; b++ {
		v := z(1, 1, b)
		sum += v
		sq += v * v
	}
	mean := sum / n
	variance := sq/n - mean*mean
	if math.Abs(mean) >= 0.01 || math.Abs(variance-1) >= 0.02 {
		t.Fatalf("z over %d buckets: mean %v, variance %v, want mean 0 (+-0.01) and variance 1 (+-0.02)", n, mean, variance)
	}
}

func TestNoiseIsKeyed(t *testing.T) {
	base := z(7, 2, 100)
	if again := z(7, 2, 100); again != base {
		t.Fatalf("z is not a pure function: %v then %v", base, again)
	}
	for name, v := range map[string]float64{
		"seed": z(8, 2, 100), "port": z(7, 3, 100), "bucket": z(7, 2, 101),
	} {
		if v == base {
			t.Errorf("changing the %s left z unchanged at %v", name, base)
		}
	}
}

// tach.noise_rpm is the standard deviation of the noise in one 1 s tach reading (D-03 as amended):
// the per-bucket ripple is scaled by noiseScale so the ten buckets of a gate average back to it.
func TestNoiseAmplitudeIsGateStd(t *testing.T) {
	// 1200 rpm at ppr 2 is 40 pulses/s: on the pulse grid, so without noise every reading is 1200.
	r := gateReadings(NewPlant(tachFan(1200, 2, 20), 1, 1), 0.5, 610, 10)
	if len(r) != 600 {
		t.Fatalf("got %d readings, want 600", len(r))
	}
	_, sd := stdDev(r)
	levels := map[float64]bool{}
	for _, v := range r {
		levels[v] = true
	}
	if sd < 20 || sd > 27 || len(levels) < 4 {
		t.Fatalf("noise_rpm 20: std of 600 gate readings = %v with %d levels, want [20, 27] and >= 4 levels", sd, len(levels))
	}
	for _, v := range gateReadings(NewPlant(tachFan(1200, 2, 0), 1, 1), 0.5, 610, 10) {
		if v != 1200 {
			t.Fatalf("noise_rpm 0: reading %v, want the single level 1200", v)
		}
	}
}

func TestTachIgnoresTickTiming(t *testing.T) {
	fan := tachFan(1000, 2, 6)
	const total = 20 * time.Second
	ref := NewPlant(fan, 1, 7)
	refLog := runPattern(ref, 0.5, tickPatterns["10ms"], total)
	levels := map[float64]bool{}
	for _, v := range refLog {
		levels[v] = true
	}
	if len(refLog) != 20 || len(levels) < 2 {
		t.Fatalf("reference run: %d gates, %d levels; the test would prove nothing", len(refLog), len(levels))
	}
	for name, pattern := range tickPatterns {
		pl := NewPlant(fan, 1, 7)
		log := runPattern(pl, 0.5, pattern, total)
		if len(log) == 0 {
			t.Fatalf("%s: no gate completed", name)
		}
		for g, v := range log {
			if v != refLog[g] {
				t.Errorf("%s: reading of gate %d = %v, want %v (10 ms pattern)", name, g, v, refLog[g])
			}
		}
		if pl.tach.pulses != ref.tach.pulses {
			t.Errorf("%s: %d pulses in 20 s, want %d (10 ms pattern)", name, pl.tach.pulses, ref.tach.pulses)
		}
	}
}

func TestSeedChangesTheTrace(t *testing.T) {
	trace := func(seed uint64, port int) []float64 {
		return gateReadings(NewPlant(tachFan(1000, 2, 20), port, seed), 0.5, 40, 0)
	}
	same := func(a, b []float64) bool {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	base := trace(1, 1)
	if !same(base, trace(1, 1)) {
		t.Error("seed 1 port 1 gave two different traces")
	}
	if same(base, trace(2, 1)) {
		t.Error("seeds 1 and 2 gave the same trace")
	}
	if same(base, trace(1, 2)) {
		t.Error("ports 1 and 2 gave the same trace")
	}
}

func TestNoPulsesAtRest(t *testing.T) {
	pl := NewPlant(tachFan(1100, 2, 50), 1, 1)
	run(pl, 0, 10*time.Second, 10*time.Millisecond)
	if pl.tach.pulses != 0 {
		t.Fatalf("a stopped fan counted %d pulses in 10 s with noise_rpm 50, want 0", pl.tach.pulses)
	}
	if r, ok := pl.Reading(); !ok || r != 0 {
		t.Fatalf("Reading of a stopped fan = %v (ok %v), want 0", r, ok)
	}
}

// EDGE-MODEL-10: a duty change lands on the tick that observes it, so the invariance holds for duty
// scripts on a common time grid. Here the duty drops at 10 s, a boundary every step below hits.
func TestTachDutyScriptOnCommonGridIsTickInvariant(t *testing.T) {
	fan := tachFan(1000, 2, 6)
	fan.Curve = Curves{Up: Curve{{0.05, 200}, {1, 2000}}}
	fan.TauUp, fan.TauDown = 0.5, 0.8
	type result struct {
		readings []float64
		pulses   int64
	}
	runScript := func(dt time.Duration) result {
		pl := NewPlant(fan, 1, 3)
		var res result
		seen := 0
		for step := 0; step < int(20*time.Second/dt); step++ {
			duty := 0.9
			if time.Duration(step)*dt >= 10*time.Second {
				duty = 0.3
			}
			pl.Step(duty, dt)
			if g := int(pl.tach.gateEnd/TachGate) - 1; g > seen {
				seen = g
				r, _ := pl.Reading()
				res.readings = append(res.readings, r)
			}
		}
		res.pulses = pl.tach.pulses
		return res
	}
	ref := runScript(10 * time.Millisecond)
	if len(ref.readings) != 20 || ref.readings[0] == ref.readings[19] {
		t.Fatalf("reference run: %d readings, first %v, last %v; the test would prove nothing", len(ref.readings), ref.readings[0], ref.readings[19])
	}
	for _, dt := range []time.Duration{time.Second, 250 * time.Millisecond, 20 * time.Millisecond} {
		got := runScript(dt)
		if got.pulses != ref.pulses || !slices.Equal(got.readings, ref.readings) {
			t.Errorf("step %v: %d pulses, readings %v; want %d pulses, readings %v (10 ms steps)", dt, got.pulses, got.readings, ref.pulses, ref.readings)
		}
	}
}

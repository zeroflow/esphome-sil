package model

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// fwWindow is components/sim_bus/sim_sensor.cpp (SimSensor::on_value and update) with integer
// millisecond times: the firmware's 1 s time-weighted mean of the rpm frames. Two frames in the same
// millisecond lose the first one's area (its width is 0), which is the mechanism behind RESEARCH
// Pitfall 3, so it is kept.
type fwWindow struct {
	have     bool
	last     float32
	lastT    int64
	winStart int64
	area     float64
}

func (w *fwWindow) onValue(v float32, now int64) {
	if w.have {
		w.area += float64(w.last) * float64(now-w.lastT)
	} else {
		w.winStart = now
		w.area = 0
	}
	w.lastT, w.last, w.have = now, v, true
}

// update returns the published mean; ok is false while no value has arrived (the firmware publishes
// nothing then).
func (w *fwWindow) update(now int64) (mean float32, ok bool) {
	if !w.have {
		return 0, false
	}
	w.area += float64(w.last) * float64(now-w.lastT)
	window := now - w.winStart
	if window > 0 {
		mean = float32(w.area / float64(window))
	} else {
		mean = w.last
	}
	w.area = 0
	w.winStart, w.lastT = now, now
	return mean, true
}

// s7Frames steps a plant of the given steady rpm (ppr 2, noise 0, TauUp 0.05) through a 5 s settle and
// then 62 s of 10 ms steps, calling Frame every 50 ms of plant time like the engine, and returns the
// frames the engine would send: change-only on the float32 value, with their plant time in ms.
type s7Frame struct {
	ms int64
	v  float32
}

// s7Run is the plant time covered by the frames of s7Frames.
const s7Run = 62 * time.Second

func s7Frames(rpm float64) []s7Frame {
	pl := NewPlant(tachFan(rpm, 2, 0), 1, 1)
	run(pl, 0.5, 5*time.Second, 10*time.Millisecond)
	pl.Frame()
	base := pl.t
	var out []s7Frame
	var sent float32
	sentOnce := false
	for i := 1; i <= int(s7Run/(50*time.Millisecond)); i++ {
		for k := 0; k < 5; k++ {
			pl.Step(0.5, 10*time.Millisecond)
		}
		v := float32(pl.Frame())
		if sentOnce && v == sent {
			continue
		}
		sent, sentOnce = v, true
		out = append(out, s7Frame{ms: (pl.t - base).Milliseconds(), v: v})
	}
	return out
}

// s7Readings is what the sensor publishes from frames arriving with jitter j ms (uniform in [0, j],
// seeded by j and the phase), with update() firing at phase + k*1000 ms. The first two readings are
// dropped (the first window is partial).
func s7Readings(frames []s7Frame, j, phase int64) []float64 {
	r := rand.New(rand.NewPCG(uint64(j), uint64(phase)))
	var w fwWindow
	var out []float64
	next := phase
	flush := func(until int64) {
		for next <= until {
			if m, ok := w.update(next); ok {
				out = append(out, float64(m))
			}
			next += 1000
		}
	}
	for _, f := range frames {
		arrive := f.ms
		if j > 0 {
			arrive = int64(math.Floor(float64(f.ms) + r.Float64()*float64(j)))
		}
		flush(arrive - 1) // an update in the same millisecond runs after the frame
		w.onValue(f.v, arrive)
	}
	flush(s7Run.Milliseconds()) // the firmware keeps publishing after the last change-only frame
	if len(out) < 22 {
		return nil
	}
	return out[2:]
}

func minMax(v []float64) (lo, hi float64) {
	lo, hi = v[0], v[0]
	for _, x := range v {
		lo, hi = min(lo, x), max(hi, x)
	}
	return lo, hi
}

// residue30 is v modulo 30 rpm, with a residue within 0.01 of 30 counted as 0.
func residue30(v float64) float64 {
	r := math.Mod(v, 30)
	if r > 30-0.01 {
		return 0
	}
	return r
}

// TestS7aFirmwareWindowKeepsSteps is spike S7a: the hermetic emulation of the firmware's 1 s window
// over the real plant's frames. The 30 rpm steps (MODEL-09) must survive it: (a) no bias, (b) bounded
// spread, (c) quantisation visible at an off-grid rate, and the step signature of roadmap criterion 3
// that holds although the window phase moves the readings off the 30 rpm grid: (d) the readings lie on
// the grid or on one phase-offset copy of it, (e) they toggle between neighbouring levels. The
// numbers are logged for the SUMMARY.
func TestS7aFirmwareWindowKeepsSteps(t *testing.T) {
	jitters := []int64{0, 2, 5, 10}
	var phases []int64
	for p := int64(0); p < 1000; p += 25 {
		phases = append(phases, p)
	}
	for _, rpm := range []float64{500, 1000, 1100, 1500, 1800} {
		frames := s7Frames(rpm)
		offGrid := rpm == 500 || rpm == 1000 || rpm == 1100
		for _, j := range jitters {
			var all []float64
			maxSpread, minSpread := 0.0, math.Inf(1)
			maxLevels, maxResidues := 0, 0
			minToggle, onGrid, total := 1.0, 0, 0
			for _, phase := range phases {
				r := s7Readings(frames, j, phase)
				if len(r) < 20 {
					t.Fatalf("%v rpm jitter %d phase %d: %d readings, want >= 20", rpm, j, phase, len(r))
				}
				all = append(all, r...)
				lo, hi := minMax(r)
				spread := hi - lo
				maxSpread, minSpread = max(maxSpread, spread), min(minSpread, spread)

				levels := map[float64]bool{}
				for _, v := range r {
					levels[math.Round(v*100)/100] = true
					if x := residue30(v); x < 0.01 {
						onGrid++
					}
					total++
				}
				maxLevels = max(maxLevels, len(levels))

				if j == 0 { // (d) lattice
					var residues []float64
					for _, v := range r {
						x := residue30(v)
						if !slices.ContainsFunc(residues, func(y float64) bool { return math.Abs(x-y) <= 0.01 }) {
							residues = append(residues, x)
						}
					}
					maxResidues = max(maxResidues, len(residues))
					if len(residues) > 3 || !slices.ContainsFunc(residues, func(y float64) bool { return y == 0 }) {
						t.Errorf("(d) %v rpm phase %d: residues mod 30 = %v, want at most three values, one of them 0", rpm, phase, residues)
					}
				}
				if offGrid && j <= 10 { // (e) toggle
					mean, _ := stdDev(r)
					below := 0
					for _, v := range r {
						if v < mean {
							below++
						}
					}
					share := math.Min(float64(below), float64(len(r)-below)) / float64(len(r))
					minToggle = math.Min(minToggle, share)
					if spread < 12 || share < 0.25 {
						t.Errorf("(e) %v rpm jitter %d phase %d: peak-to-peak %v, smaller-side share %v, want >= 12 and >= 0.25", rpm, j, phase, spread, share)
					}
				}
				limit := 60.0
				if j >= 10 {
					limit = 90
				}
				if spread > limit { // (b)
					t.Errorf("(b) %v rpm jitter %d phase %d: peak-to-peak %v, want <= %v", rpm, j, phase, spread, limit)
				}
			}
			bias, _ := stdDev(all)
			bias -= rpm
			if j <= 2 && math.Abs(bias) > 1 { // (a)
				t.Errorf("(a) %v rpm jitter %d: mean reading off by %v rpm, want within 1", rpm, j, bias)
			}
			if rpm == 1100 && maxLevels < 2 { // (c)
				t.Errorf("(c) 1100 rpm jitter %d: %d distinct readings, want >= 2", j, maxLevels)
			}
			res := "-"
			if j == 0 {
				res = fmt.Sprint(maxResidues)
			}
			tog := "-"
			if offGrid {
				tog = fmt.Sprintf("%.2f", minToggle)
			}
			t.Logf("S7a %4.0f rpm jitter %2d ms: bias %+.2f spread %.1f..%.1f levels<=%d residues(J=0) %s min-toggle %s on-grid %.2f",
				rpm, j, bias, minSpread, maxSpread, maxLevels, res, tog, float64(onGrid)/float64(total))
		}
	}
}

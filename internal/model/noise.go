package model

import (
	"math"
	"math/bits"
)

// The tach noise is a stateless keyed hash of (seed, port, 100 ms bucket): no stream, no
// math/rand, so a trace depends only on those three numbers and never on how the ticks fall
// (MODEL-10). The global source of math/rand is banned under internal/model by the lint in
// internal/lint.

// noiseScale scales the per-bucket ripple so that tach.noise_rpm is the standard deviation of the
// noise in one 1 s tach reading, the unit the firmware and Home Assistant show: the ten independent
// buckets of a gate average back to noise_rpm only if each carries sqrt(10) times that.
var noiseScale = math.Sqrt(float64(TachGate / NoiseBucket))

// mix is the splitmix64 finaliser (a public domain algorithm).
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// unit maps a hash to [0, 1) with 53 bits of resolution.
func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// z is the unit-variance noise of one bucket of one port: the Irwin-Hall sum of four uniforms drawn
// from the hash of (seed, port, bucket), scaled to mean 0 and variance 1. Mixing the port in gives
// each port its own stream (D-15). There is no default seed here: the default of `sil up` is
// lifecycle.DefaultSeed.
func z(seed uint64, port int, bucket int64) float64 {
	k := mix(seed ^ bits.RotateLeft64(uint64(port)*0xa24baed4963ee407, 17) ^ uint64(bucket)*0x9fb21c651e98df25)
	s := 0.0
	for i := 0; i < 4; i++ {
		k = mix(k)
		s += unit(k) - 0.5
	}
	return s * math.Sqrt(3)
}

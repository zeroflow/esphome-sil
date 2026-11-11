//go:build sil_e2e

package e2e

import (
	"testing"
)

// seedOf is sim.seed of a status as a JSON number; ok is false when it is absent or not a number.
func seedOf(st map[string]any) (float64, bool) {
	v, ok := jget(st, "sim", "seed").(float64)
	return v, ok
}

// TestUpSeed follows the noise seed from `sil up --seed` through spec.json and the supervisor to
// `sil status` (D-15). The seed is only read, so the test asserts nothing about fans. 42 and 24301
// are exact in a float64, as is every seed up to 2^53-1.
func TestUpSeed(t *testing.T) {
	env := newEnv(t)
	// Both instances are brought up with the parent's t: env.Up registers its `sil down` on the t it
	// gets, and the later subtests reuse "e2e-seed".
	env.Up(t, "e2e-seed", "--seed", "42")
	env.Up(t, "e2e-seed-default")

	t.Run("flag", func(t *testing.T) {
		st := env.Status(t, "e2e-seed")
		if got, ok := seedOf(st); !ok || got != 42 {
			t.Fatalf("sim.seed = %v (present %v), want 42: %v", got, ok, st["sim"])
		}
		if st["schema"] != float64(1) {
			t.Errorf("status schema = %v, want 1", st["schema"])
		}
	})

	t.Run("default", func(t *testing.T) {
		st := env.Status(t, "e2e-seed-default")
		if got, ok := seedOf(st); !ok || got != 24301 {
			t.Fatalf("sim.seed = %v (present %v), want the default 24301: %v", got, ok, st["sim"])
		}
	})
}

//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	t.Run("run_json", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(env.Runtime, "sil", "e2e-seed", "run.json"))
		if err != nil {
			t.Fatal(err)
		}
		var rf map[string]any
		if err := json.Unmarshal(raw, &rf); err != nil {
			t.Fatalf("run.json is not JSON: %v: %s", err, raw)
		}
		if rf["seed"] != float64(42) {
			t.Fatalf("run.json seed = %v, want 42: %s", rf["seed"], raw)
		}
	})

	t.Run("restarts", func(t *testing.T) {
		for _, args := range [][]string{{"sim", "restart", "e2e-seed"}, {"fw", "restart", "e2e-seed"}} {
			out, exit, stderr := env.Sil(t, args...)
			if exit != 0 {
				t.Fatalf("sil %s: exit %d: %v\nstderr: %s", strings.Join(args, " "), exit, out, stderr)
			}
			if got, ok := seedOf(out); !ok || got != 42 {
				t.Fatalf("after sil %s sim.seed = %v (present %v), want 42: %v", strings.Join(args, " "), got, ok, out["sim"])
			}
		}
	})

	t.Run("refused", func(t *testing.T) {
		const name = "e2e-seedbad"
		// A refused up leaves nothing to stop; this only catches an up that wrongly succeeded.
		t.Cleanup(func() { env.run(t, "down", name) })
		out, exit, stderr := env.Sil(t, "up", name, "--seed", "9007199254740992")
		if exit != 1 {
			t.Fatalf("sil up --seed 2^53: exit %d, want 1: %v", exit, out)
		}
		if jget(out, "error", "code") != "invalid_argument" {
			t.Fatalf("error.code = %v, want invalid_argument: %v", jget(out, "error", "code"), out)
		}
		if msg, _ := jget(out, "error", "message").(string); !strings.Contains(msg, "--seed") {
			t.Errorf("message %q does not name --seed", msg)
		}
		if strings.Contains(stderr, "building firmware") {
			t.Errorf("a firmware build was started: stderr %q", stderr)
		}
		for _, f := range []string{"build.log", "spec.json"} {
			if _, err := os.Stat(filepath.Join(env.Runtime, "sil", name, f)); !os.IsNotExist(err) {
				t.Errorf("%s exists after a refused up (stat error %v)", f, err)
			}
		}
	})
}

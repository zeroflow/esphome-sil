//go:build sil_e2e

package e2e

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// libv2WaitRPM polls `sil status` every 200 ms until ports[port].rpm is within tol of want and returns that
// status. The failure message names the step, the expected value, the curve it came from and the rpm shown.
func libv2WaitRPM(t *testing.T, env *Env, name string, port int, step string, want, tol float64, curve string, timeout time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, timeout)
	defer cancel()
	for {
		st := env.Status(t, name)
		if math.Abs(tracerPortValue(st, port, "rpm")-want) <= tol {
			return st
		}
		if err := sysClock.Sleep(ctx, 200*time.Millisecond); err != nil {
			t.Fatalf("%s: rpm did not reach %v (+-%v, the %s curve) within %s; shown rpm %v at duty %v",
				step, want, tol, curve, timeout, tracerPortValue(st, port, "rpm"), tracerPortValue(st, port, "duty"))
		}
	}
}

// TestLibraryV2 runs a default `sil up` on the shipped schema 2 fan library (D-01). One instance is shared by
// all subtests; plan 04-09 adds the behaviour subtests (curves, start_stop, loads_and_tach) to this function,
// so env.Up and the instance name stay in the function body. --seed 42 makes the noise reproducible.
func TestLibraryV2(t *testing.T) {
	const name = "e2e-libv2"
	env := newEnv(t)
	// An empty SIL_FAN_DIR means the embedded library; it wins over the default pin to testdata/fans-v0.1.0
	// because a later entry of a duplicate key is the one os/exec passes on.
	env.Extra = append(env.Extra, "SIL_FAN_DIR=")
	up := env.Up(t, name, "--seed", "42")
	apiPort, apiKey := modelAPI(t, up)

	// fans_schema2: the embedded library is schema 2 and `sil fans` shows where every number comes from.
	t.Run("fans_schema2", func(t *testing.T) {
		out, code, _ := env.Sil(t, "fans", "-i", name)
		if code != 0 {
			t.Fatalf("sil fans: exit %d: %v", code, out)
		}
		list := jgetSlice(out, "fans")
		byID := map[string]any{}
		for _, f := range list {
			byID[jget(f, "id").(string)] = f
		}
		if len(list) != 2 || byID["arctic-p12-pwm-pst"] == nil || byID["noctua-nf-a12x25-pwm"] == nil {
			t.Fatalf("sil fans = %v, want exactly the arctic-p12-pwm-pst and noctua-nf-a12x25-pwm", out)
		}
		cases := []struct {
			id                   string
			datasheet, assumed   float64
			maxRPM               float64
			belowMin             string
			stopKind, stopSource any
		}{
			{"arctic-p12-pwm-pst", 4, 9, 1800, "stop", "datasheet", "arctic_page"},
			{"noctua-nf-a12x25-pwm", 3, 10, 2000, "hold", "assumed", nil},
		}
		for _, c := range cases {
			f := byID[c.id]
			if jget(f, "schema") != 2.0 {
				t.Errorf("%s schema = %v, want 2", c.id, jget(f, "schema"))
			}
			for k, want := range map[string]float64{"measured": 0, "datasheet": c.datasheet, "assumed": c.assumed, "unobservable": 0} {
				if jget(f, "provenance", k) != want {
					t.Errorf("%s provenance.%s = %v, want %v", c.id, k, jget(f, "provenance", k), want)
				}
			}
			if jget(f, "max_rpm") != c.maxRPM || jget(f, "below_min") != c.belowMin {
				t.Errorf("%s max_rpm %v below_min %v, want %v %s", c.id, jget(f, "max_rpm"), jget(f, "below_min"), c.maxRPM, c.belowMin)
			}
			if jget(f, "values", "stop.duty", "kind") != c.stopKind || jget(f, "values", "stop.duty", "source") != c.stopSource {
				t.Errorf("%s values[stop.duty] = %v, want {%v %v}", c.id, jget(f, "values", "stop.duty"), c.stopKind, c.stopSource)
			}
		}
	})

	// status_v2: the seed of the run and the three new status fields of every port.
	t.Run("status_v2", func(t *testing.T) {
		st := env.Status(t, name)
		if got, ok := seedOf(st); !ok || got != 42 {
			t.Errorf("sim.seed = %v (present %v), want 42: %v", got, ok, st["sim"])
		}
		ports := jgetSlice(st, "ports")
		if len(ports) == 0 {
			t.Fatalf("status has no ports: %v", st)
		}
		for i, p := range ports {
			m, _ := p.(map[string]any)
			for _, k := range []string{"tach_rpm", "airflow_m3h", "power_w"} {
				if _, ok := m[k]; !ok {
					t.Errorf("port %d has no %s: %v", i+1, k, m)
				}
			}
		}
	})

	// curves (criterion 1, D-01): the shipped Arctic follows its up curve on a rising sweep and its separate,
	// lower down curve on the way back. Port 1 is driven through the firmware's fan entity; the plant is read
	// from `sil status` and `sil watch`. The subtest leaves Fan 1 at 40 % and does not touch Fans 2 and 3.
	t.Run("curves", func(t *testing.T) {
		w := watchStart(t, env, "watch", "-i", name, "--kind", "rpm")
		steps := []struct {
			speed int
			want  float64
			curve string
		}{
			{40, 820, "up"}, {60, 1150, "up"}, {80, 1480, "up"}, {60, 1100, "down"}, {40, 780, "down"},
		}
		var fallT float64 // sim time (s) of the falling step to 60 %
		for i, s := range steps {
			if i == 3 {
				fallT = modelSimTime(env.Status(t, name))
			}
			modelSetFan(t, env, apiPort, apiKey, 1, s.speed)
			libv2WaitRPM(t, env, name, 0, fmt.Sprintf("step %d (speed %d)", i+1, s.speed), s.want, 2, s.curve, 25*time.Second)
		}
		// The stream carries the falling approach to the down curve: an rpm event within 3 of 1100 that is
		// newer than the falling step (the rising sweep also passed 1100 on its way to 1150).
		w.until(t, 10*time.Second, "an rpm event of port 1 within 3 of 1100 after the falling step to 60 %", func(m map[string]any) bool {
			v, _ := m["v"].(float64)
			ts, _ := m["t"].(float64)
			return m["k"] == "rpm" && m["port"] == 1.0 && ts >= fallT*1e6 && math.Abs(v-1100) <= 3
		})
	})
}

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

	// start_stop (criterion 2): Fan 2 (port 2) from rest on the shipped Arctic. Below start.duty it stays at
	// rest; at exactly start.duty it starts after start.delay_s (the boundary is at or above, D-11 as amended);
	// between stop.duty and start.duty it keeps running; below stop.duty it stops at once and coasts with
	// coast_tau_s. Port 3 then holds the Noctua's never-stops floor (D-10).
	t.Run("start_stop", func(t *testing.T) {
		// exactly start.duty 0.10: the start boundary is at or above, D-11 as amended
		const startSpeed = 10
		w := watchStart(t, env, "watch", "-i", name, "--kind", "duty,state")
		duty2 := func(want float64) func(map[string]any) bool {
			return func(m map[string]any) bool {
				v, _ := m["v"].(float64)
				return m["k"] == "duty" && m["port"] == 2.0 && math.Abs(v-want) <= 1e-6
			}
		}
		state2 := func(m map[string]any) bool { return m["k"] == "state" && m["port"] == 2.0 }
		waitDuty := func(want float64) {
			t.Helper()
			env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
				return math.Abs(tracerPortValue(s, 1, "duty")-want) <= 1e-6
			})
		}

		// Speed 8, below start.duty 0.10: stays at rest.
		modelSetFan(t, env, apiPort, apiKey, 2, 8)
		waitDuty(0.08)
		n := modelObserve(t, env, name, 3*time.Second, func(st map[string]any) {
			if rpm := tracerPortValue(st, 1, "rpm"); rpm != 0 {
				t.Fatalf("Fan 2 at duty 0.08 from rest has rpm %v, want exactly 0", rpm)
			}
			if s := modelPortState(st, 1); s != "stopped" {
				t.Fatalf("Fan 2 at duty 0.08 from rest is %q, want stopped", s)
			}
		})
		if n < 5 {
			t.Fatalf("only %d status samples in 3 s", n)
		}

		// Exactly start.duty: it starts, after the start delay.
		modelSetFan(t, env, apiPort, apiKey, 2, startSpeed)
		de, _ := w.until(t, 10*time.Second, "the duty event of port 2 at 0.1", duty2(0.1))
		if v, _ := de["v"].(float64); v != 0.1 {
			t.Fatalf("the start boundary is not under test: duty v=%v, want exactly 0.1 (start.duty 0.10): %v", de["v"], de)
		}
		justAfter := env.Status(t, name) // one poll right after the duty event
		re, _ := w.until(t, 10*time.Second, "the state event of port 2 after the duty event", state2)
		if re["v"] != "running" {
			t.Fatalf("the state event after the duty event at start.duty is %v, want running: %v", re["v"], re)
		}
		dt := time.Duration((re["t"].(float64) - de["t"].(float64)) * float64(time.Microsecond))
		// The duty event is stamped at the exact bus frame time, but the model applies the duty to the whole
		// 10 ms engine tick that follows it and the state event is stamped at the tick that ends the delay, so
		// the measured delay is start.delay_s 0.4 less up to one tick: (390, 400] ms. The window is one engine
		// tick either side of 0.4 s.
		if dt < 390*time.Millisecond || dt > 410*time.Millisecond {
			t.Errorf("port 2 started %s of sim time after the duty event, want 390 to 410 ms (start.delay_s 0.4 within one 10 ms engine tick): %v then %v", dt, de, re)
		}
		if modelSimTime(justAfter) < re["t"].(float64)/1e6 {
			if rpm := tracerPortValue(justAfter, 1, "rpm"); rpm != 0 {
				t.Errorf("port 2 had rpm %v at sim %.3f s, before its running event at %.3f s, want 0 during the delay",
					rpm, modelSimTime(justAfter), re["t"].(float64)/1e6)
			}
		}

		// Spun up, then speed 6, between stop.duty 0.05 and start.duty 0.10: it keeps running.
		if err := sysClock.Sleep(context.Background(), 10*time.Second); err != nil {
			t.Fatal(err)
		}
		modelSetFan(t, env, apiPort, apiKey, 2, 6)
		waitDuty(0.06)
		modelObserve(t, env, name, 3*time.Second, func(st map[string]any) {
			if s := modelPortState(st, 1); s != "running" {
				t.Fatalf("Fan 2 at duty 0.06 (between stop.duty and start.duty) is %q, want running", s)
			}
		})

		// Speed 3, below stop.duty: it stops at once and coasts with coast_tau_s 6 (not tau_down_s 2.5).
		modelSetFan(t, env, apiPort, apiKey, 2, 3)
		de, _ = w.until(t, 10*time.Second, "the duty event of port 2 at 0.03", duty2(0.03))
		se, _ := w.until(t, 10*time.Second, "the state event of port 2 after duty 0.03", state2)
		if se["v"] != "stopped" {
			t.Fatalf("the state event after duty 0.03 is %v, want stopped: %v", se["v"], se)
		}
		if gap := time.Duration((se["t"].(float64) - de["t"].(float64)) * float64(time.Microsecond)); gap > 100*time.Millisecond {
			t.Errorf("port 2 stopped %s of sim time after duty 0.03, want at once (<= 100 ms)", gap)
		}
		first := env.WaitStatus(t, name, 5*time.Second, func(s map[string]any) bool { return modelPortState(s, 1) == "stopped" })
		if err := sysClock.Sleep(context.Background(), 2*time.Second); err != nil {
			t.Fatal(err)
		}
		second := env.Status(t, name)
		r1, r2 := tracerPortValue(first, 1, "rpm"), tracerPortValue(second, 1, "rpm")
		dsim := modelSimTime(second) - modelSimTime(first)
		if r1 <= 0 || r2 <= 0 {
			t.Fatalf("the coast-down is not observable: rpm %v then %v", r1, r2)
		}
		want := math.Exp(-dsim / 6)
		if got := r2 / r1; math.Abs(got/want-1) > 0.03 {
			t.Errorf("coast ratio %.4f over %.3f s of sim time (rpm %v -> %v), want exp(-dsim/6) = %.4f within 3 %% (coast_tau_s 6, not tau_down_s 2.5)",
				got, dsim, r1, r2, want)
		}

		// Port 3, never stops (D-10): the Noctua holds 450 rpm at duty 0.
		out, code, stderr := env.Sil(t, "set", "port3.fan=noctua-nf-a12x25-pwm", "-i", name)
		if code != 0 {
			t.Fatalf("set port3.fan=noctua-nf-a12x25-pwm: exit %d: %v\nstderr: %s", code, out, stderr)
		}
		modelSetFan(t, env, apiPort, apiKey, 3, 0)
		if err := sysClock.Sleep(context.Background(), 20*time.Second); err != nil {
			t.Fatal(err)
		}
		st := env.Status(t, name)
		if rpm := tracerPortValue(st, 2, "rpm"); math.Abs(rpm-450) > 2 {
			t.Errorf("Noctua on port 3 at duty %v has rpm %v, want 450 within 2 (never stops)", tracerPortValue(st, 2, "duty"), rpm)
		}
		if s := modelPortState(st, 2); s != "running" {
			t.Errorf("Noctua on port 3 at duty 0 is %q, want running", s)
		}
	})

	// loads_and_tach (criterion 3 status side, criterion 4): Fan 1 at speed 60 for 10 s on the shipped Arctic.
	t.Run("loads_and_tach", func(t *testing.T) {
		w := watchStart(t, env, "watch", "-i", name, "--kind", "tach,load")
		modelSetFan(t, env, apiPort, apiKey, 1, 60)
		libv2WaitRPM(t, env, name, 0, "speed 60", 1150, 2, "up", 25*time.Second)
		if err := sysClock.Sleep(context.Background(), 10*time.Second); err != nil {
			t.Fatal(err)
		}
		st := env.Status(t, name)
		rpm := tracerPortValue(st, 0, "rpm")
		air, pow, tach := tracerPortValue(st, 0, "airflow_m3h"), tracerPortValue(st, 0, "power_w"), tracerPortValue(st, 0, "tach_rpm")
		near := func(got, want float64) bool { return math.Abs(got-want) <= 1e-6*math.Abs(want) }
		if want := 95.7 * rpm / 1800; !near(air, want) {
			t.Errorf("airflow_m3h = %v at rpm %v, want 95.7*rpm/1800 = %v", air, rpm, want)
		}
		if want := 1.2 * math.Pow(rpm/1800, 3); !near(pow, want) {
			t.Errorf("power_w = %v at rpm %v, want 1.2*(rpm/1800)^3 = %v", pow, rpm, want)
		}
		if _, ok := jget(st, "ports", 0, "tach_rpm").(float64); !ok {
			t.Errorf("tach_rpm is %v, want a number: %v", jget(st, "ports", 0, "tach_rpm"), jget(st, "ports", 0))
		} else if math.Abs(tach/30-math.Round(tach/30)) > 1e-6 || math.Abs(tach-rpm) > 120 {
			t.Errorf("tach_rpm = %v at rpm %v, want a whole multiple of 30 within 120 of rpm", tach, rpm)
		}

		var load, tachEv map[string]any
		w.until(t, 10*time.Second, "a load event and a tach event with a value for port 1", func(m map[string]any) bool {
			if m["port"] != 1.0 {
				return false
			}
			switch m["k"] {
			case "load":
				if v, _ := m["v"].(map[string]any); v != nil {
					_, a := v["airflow_m3h"].(float64)
					_, p := v["power_w"].(float64)
					if a && p {
						load = m
					}
				}
			case "tach":
				if _, ok := m["v"].(float64); ok {
					tachEv = m
				}
			}
			return load != nil && tachEv != nil
		})
		if v := tachEv["v"].(float64); math.Abs(v/30-math.Round(v/30)) > 1e-6 {
			t.Errorf("tach event v = %v, want a whole multiple of 30: %v", v, tachEv)
		}
	})
}

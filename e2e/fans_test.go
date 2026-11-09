//go:build sil_e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	fansArctic = "arctic-p12-pwm-pst"
	fansNoctua = "noctua-nf-a12x25-pwm"
)

// fansPort is ports[i] of a status object.
func fansPort(st map[string]any, i int) (fan string, rpm, duty float64, state string) {
	fan, _ = jget(st, "ports", i, "fan").(string)
	rpm, _ = jget(st, "ports", i, "rpm").(float64)
	duty, _ = jget(st, "ports", i, "duty").(float64)
	state, _ = jget(st, "ports", i, "state").(string)
	return fan, rpm, duty, state
}

// fansWait waits until pred holds for ports[i] and returns that status.
func (in *r33Inst) fansWait(t *testing.T, i int, within time.Duration, what string, pred func(fan string, rpm, duty float64, state string) bool) map[string]any {
	t.Helper()
	return in.env.WaitStatus(t, in.name, within, func(st map[string]any) bool {
		fan, rpm, duty, state := fansPort(st, i)
		return pred(fan, rpm, duty, state)
	})
}

// fansReplugged requires the very next status after a swap to show fan at rest on ports[i].
func (in *r33Inst) fansReplugged(t *testing.T, i int, fan, state string) {
	t.Helper()
	st := in.env.Status(t, in.name)
	if f, rpm, _, s := fansPort(st, i); f != fan || rpm >= 300 || s != state {
		t.Fatalf("right after the swap ports[%d] = %v, want fan %s with rpm below 300 and state %s", i, jget(st, "ports", i), fan, state)
	}
}

// fansStaysAtRest requires ports[i] to be exactly rpm 0 and stopped now and after 2 s.
func (in *r33Inst) fansStaysAtRest(t *testing.T, i int, fan string) {
	t.Helper()
	for round := 0; round < 2; round++ {
		st := in.env.Status(t, in.name)
		if f, rpm, _, s := fansPort(st, i); f != fan || rpm != 0 || s != "stopped" {
			t.Fatalf("round %d: ports[%d] = %v, want fan %s at exactly rpm 0 and stopped", round, i, jget(st, "ports", i), fan)
		}
		if round == 0 {
			r33Sleep(2 * time.Second)
		}
	}
}

func fansInRange(rpm, lo, hi float64) bool { return rpm >= lo && rpm <= hi }

// TestFanSwap is MODEL-03 end to end: `sil set portN.fan=ID` unplugs the fan of a running port and
// plugs another in; the new fan starts from rest at the current duty and follows its own file,
// and the firmware sees it.
func TestFanSwap(t *testing.T) {
	in := r33Up(t, "e2e-fans")
	inRange := func(lo, hi float64) func(string, float64, float64, string) bool {
		return func(_ string, rpm, _ float64, _ string) bool { return fansInRange(rpm, lo, hi) }
	}
	swap := func(t *testing.T, id string) {
		t.Helper()
		out, code := in.set(t, "port2.fan="+id)
		applied, _ := out["applied"].(map[string]any)
		if code != 0 || len(out) != 1 || len(applied) != 1 || applied["port2.fan"] != id {
			t.Fatalf("set port2.fan=%s: exit %d: %v, want exit 0 and {applied: {port2.fan: %s}}", id, code, out, id)
		}
	}

	t.Run("arctic_to_noctua_at_full_speed", func(t *testing.T) {
		in.setFan(t, 2, 100)
		st := in.fansWait(t, 1, 20*time.Second, "arctic at full speed", func(fan string, rpm, _ float64, _ string) bool {
			return fan == fansArctic && rpm > 1700
		})
		if fan, _, _, _ := fansPort(st, 0); fan != fansArctic {
			t.Fatalf("ports[0].fan = %q before the swap, want %s", fan, fansArctic)
		}

		swap(t, fansNoctua)
		in.fansReplugged(t, 1, fansNoctua, "running")
		st = in.env.Status(t, in.name)
		if fan, _, _, _ := fansPort(st, 0); fan != fansArctic {
			t.Fatalf("ports[0].fan = %q after swapping port 2, want %s untouched", fan, fansArctic)
		}
		// Toward the Noctua's 2000 rpm max, which the Arctic (1800) can never reach.
		in.fansWait(t, 1, 8*time.Second, "noctua above 1900", func(fan string, rpm, _ float64, _ string) bool {
			return fan == fansNoctua && rpm > 1900
		})
		out := ioWatch(t, in.port, in.key, 3)
		samples := ioSamples(t, out, "Fan 2 Speed")
		if len(samples) == 0 || samples[len(samples)-1][1] <= 1800 {
			t.Fatalf("Fan 2 Speed series %v, want it to end above 1800 (the firmware follows the Noctua)", samples)
		}
	})

	t.Run("noctua_holds_and_replug_restarts_it", func(t *testing.T) {
		in.setFan(t, 2, 10)
		in.fansWait(t, 1, 20*time.Second, "noctua holding about 450", inRange(430, 470))

		swap(t, fansNoctua) // same-fan replug of a hold fan
		in.fansReplugged(t, 1, fansNoctua, "running")
		in.fansWait(t, 1, 10*time.Second, "noctua back at about 450", inRange(430, 470))

		// The other ports are not touched by any of this.
		st := in.env.Status(t, in.name)
		if fan, _, _, _ := fansPort(st, 3); fan != fansArctic {
			t.Fatalf("ports[3].fan = %q, want %s", fan, fansArctic)
		}
	})

	t.Run("stop_fan_below_start_duty_stays_stopped", func(t *testing.T) {
		in.setFan(t, 2, 8)
		in.fansWait(t, 1, 5*time.Second, "duty 0.08", func(_ string, _, duty float64, _ string) bool { return duty > 0.079 && duty < 0.081 })
		st := in.env.Status(t, in.name)
		if fan, rpm, _, _ := fansPort(st, 1); fan != fansNoctua || !fansInRange(rpm, 430, 470) {
			t.Fatalf("ports[1] = %v, want the Noctua still holding about 450 rpm at duty 0.08", jget(st, "ports", 1))
		}

		swap(t, fansArctic)
		in.fansReplugged(t, 1, fansArctic, "stopped")
		in.fansStaysAtRest(t, 1, fansArctic)
	})

	t.Run("same_fan_replug_of_a_stop_fan_in_its_dead_band", func(t *testing.T) {
		in.setFan(t, 2, 20)
		in.fansWait(t, 1, 10*time.Second, "arctic running above 300", func(fan string, rpm, _ float64, _ string) bool {
			return fan == fansArctic && rpm > 300
		})
		in.setFan(t, 2, 8)
		in.fansWait(t, 1, 5*time.Second, "duty 0.08", func(_ string, _, duty float64, _ string) bool { return duty > 0.079 && duty < 0.081 })
		r33Sleep(3 * time.Second)
		st := in.env.Status(t, in.name)
		if fan, rpm, _, state := fansPort(st, 1); fan != fansArctic || state != "running" || rpm < 150 {
			t.Fatalf("ports[1] = %v, want the Arctic still running in its dead band", jget(st, "ports", 1))
		}

		swap(t, fansArctic)
		in.fansReplugged(t, 1, fansArctic, "stopped")
		in.fansStaysAtRest(t, 1, fansArctic)
	})

	t.Run("rejections_change_nothing", func(t *testing.T) {
		out, code := in.set(t, "port2.fan=nope")
		r33ExpectErr(t, "set port2.fan=nope", out, code, "unknown_fan")
		known := jgetSlice(out, "error", "detail", "known")
		if !reflect.DeepEqual(known, []any{fansArctic, fansNoctua}) {
			t.Fatalf("detail.known = %v, want [%s %s]", known, fansArctic, fansNoctua)
		}

		// All or nothing across keys: the good temperature is not applied either.
		out, code = in.set(t, "temperature=25", "port2.fan=nope")
		r33ExpectErr(t, "set temperature=25 port2.fan=nope", out, code, "unknown_fan")
		st := in.env.Status(t, in.name)
		if !r33EnvIs(st, 22, 45) {
			t.Fatalf("status env = %v after a rejected mixed set, want 22 and 45", st["env"])
		}
		if fan, _, _, _ := fansPort(st, 1); fan != fansArctic {
			t.Fatalf("ports[1].fan = %q after a rejected set, want %s", fan, fansArctic)
		}

		out, code = in.set(t, "port5.fan="+fansArctic)
		r33ExpectErr(t, "set port5.fan", out, code, "unknown_key")

		// A fan id is never a path (T-02-12).
		for _, v := range []string{"../arctic-p12-pwm-pst", "/etc/passwd"} {
			out, code = in.set(t, "port2.fan="+v)
			r33ExpectErr(t, fmt.Sprintf("set port2.fan=%s", v), out, code, "unknown_fan")
		}
	})
}

// fansFour is the fan id on every port of a status object.
func fansFour(st map[string]any) [4]string {
	var out [4]string
	for i := range out {
		out[i], _, _, _ = fansPort(st, i)
	}
	return out
}

// TestUpFan: `sil up --fan ID` plugs ID into all four ports; an unknown id fails before anything is
// written for the instance.
func TestUpFan(t *testing.T) {
	cfg := filepath.Join(repoRoot(t), "examples", "rev33.yaml")

	t.Run("unknown_fan_writes_nothing", func(t *testing.T) {
		env := newEnv(t)
		out, code, _ := env.Sil(t, "up", "e2e-upbad", "--fan", "nope", "--config", cfg)
		r33ExpectErr(t, "up --fan nope", out, code, "unknown_fan")
		known := jgetSlice(out, "error", "detail", "known")
		if !reflect.DeepEqual(known, []any{fansArctic, fansNoctua}) {
			t.Fatalf("detail.known = %v, want [%s %s]", known, fansArctic, fansNoctua)
		}
		for _, dir := range []string{filepath.Join(env.State, "sil", "e2e-upbad"), filepath.Join(env.Runtime, "sil", "e2e-upbad")} {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("%s exists after a rejected `up --fan nope` (stat error %v), want nothing written", dir, err)
			}
		}
	})

	t.Run("fan_on_all_four_ports", func(t *testing.T) {
		env := newEnv(t)
		env.Up(t, "e2e-upfan", "--config", cfg, "--fan", fansNoctua)
		st := env.Status(t, "e2e-upfan")
		if got := fansFour(st); got != [4]string{fansNoctua, fansNoctua, fansNoctua, fansNoctua} {
			t.Fatalf("ports fans = %v, want the Noctua on every port", got)
		}
	})
}

// TestFanChoice: the fan choice is run state held by the supervisor, so it survives both kinds of
// restart; `sil fans` lists the library. This is the assumption-delta invariant test: every port's
// fan in `sil status` equals the held choice after a set, a sim restart and a fw restart.
func TestFanChoice(t *testing.T) {
	in := r33Up(t, "e2e-fanchoice")
	want := [4]string{fansArctic, fansArctic, fansNoctua, fansArctic}
	holds := func(t *testing.T, step string) {
		t.Helper()
		st := in.env.Status(t, in.name)
		if got := fansFour(st); got != want {
			t.Fatalf("%s: ports fans = %v, want %v", step, got, want)
		}
	}

	out, code := in.set(t, "port3.fan="+fansNoctua)
	if code != 0 {
		t.Fatalf("set port3.fan: exit %d: %v", code, out)
	}
	holds(t, "after the set")

	sim, code, _ := in.env.Sil(t, "sim", "restart", in.name)
	if code != 0 {
		t.Fatalf("sim restart: exit %d: %v", code, sim)
	}
	holds(t, "after sim restart")
	if fan, _, _, _ := fansPort(sim, 2); fan != fansNoctua {
		t.Fatalf("the status printed by sim restart has ports[2].fan %q, want %s", fan, fansNoctua)
	}

	fw, code, _ := in.env.Sil(t, "fw", "restart", in.name)
	if code != 0 {
		t.Fatalf("fw restart: exit %d: %v", code, fw)
	}
	holds(t, "after fw restart")

	// The chosen model is live in the new engine: only the Noctua gets above the Arctic's 1800.
	in.setFan(t, 3, 100)
	in.fansWait(t, 2, 20*time.Second, "noctua above 1900 after the restarts", func(fan string, rpm, _ float64, _ string) bool {
		return fan == fansNoctua && rpm > 1900
	})

	t.Run("fans_lists_the_library", func(t *testing.T) {
		out, code, _ := in.env.Sil(t, "fans", "-i", in.name)
		if code != 0 {
			t.Fatalf("sil fans: exit %d: %v", code, out)
		}
		list := jgetSlice(out, "fans")
		if len(out) != 1 || len(list) != 2 {
			t.Fatalf("sil fans = %v, want exactly {fans: [two entries]}", out)
		}
		for i, w := range []struct {
			id, name, below string
			min, max        float64
		}{
			{fansArctic, "Arctic P12 PWM PST", "stop", 200, 1800},
			{fansNoctua, "Noctua NF-A12x25 PWM", "hold", 450, 2000},
		} {
			if jget(list, i, "id") != w.id || jget(list, i, "name") != w.name || jget(list, i, "below_min") != w.below ||
				jget(list, i, "min_rpm") != w.min || jget(list, i, "max_rpm") != w.max {
				t.Errorf("fans[%d] = %v, want %+v", i, list[i], w)
			}
		}
	})
}

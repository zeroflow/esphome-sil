//go:build sil_e2e

package e2e

import (
	"fmt"
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

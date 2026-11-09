//go:build sil_e2e

package e2e

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// r33Inst is a running Rev 3.3 instance and what its tests need to talk to it.
type r33Inst struct {
	env  *Env
	name string
	port int
	key  string
}

// r33Up brings up examples/rev33.yaml as instance name.
func r33Up(t *testing.T, name string) *r33Inst {
	t.Helper()
	env := newEnv(t)
	up := env.Up(t, name, "--config", filepath.Join(repoRoot(t), "examples", "rev33.yaml"))
	api, _ := up["api"].(map[string]any)
	port, _ := api["port"].(float64)
	key, _ := api["key"].(string)
	if port == 0 || key == "" {
		t.Fatalf("incomplete api object: %v", up)
	}
	return &r33Inst{env: env, name: name, port: int(port), key: key}
}

// setFan drives one fan through the firmware's native API, like Home Assistant.
func (in *r33Inst) setFan(t *testing.T, fan, speed int) {
	t.Helper()
	out, code := in.env.APIClient(t, in.port, in.key, "set-fan", "--fan", fmt.Sprint(fan), "--speed", fmt.Sprint(speed))
	if code != 0 {
		t.Fatalf("set-fan %d %d: exit %d: %v", fan, speed, code, out)
	}
}

// press runs `sil press args... -i name` and returns the decoded JSON and the exit code.
func (in *r33Inst) press(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	out, code, _ := in.env.Sil(t, append(append([]string{"press"}, args...), "-i", in.name)...)
	return out, code
}

// r33Duty is ports[i].duty of a status object.
func r33Duty(st map[string]any, i int) float64 {
	f, _ := jget(st, "ports", i, "duty").(float64)
	return f
}

// r33Duties reports whether the four duties match want (within 1e-3).
func r33Duties(st map[string]any, want [4]float64) bool {
	for i, w := range want {
		if math.Abs(r33Duty(st, i)-w) > 1e-3 {
			return false
		}
	}
	return true
}

// waitDuties waits until the four duties match want.
func (in *r33Inst) waitDuties(t *testing.T, want [4]float64) map[string]any {
	t.Helper()
	return in.env.WaitStatus(t, in.name, 5*time.Second, func(st map[string]any) bool { return r33Duties(st, want) })
}

// r33Buttons returns buttons.<id> of a status object, and whether it is present.
func r33Buttons(st map[string]any, id string) (level, present bool) {
	level, present = jget(st, "buttons", id).(bool)
	return level, present
}

// r33ErrCode is error.code of a failed result.
func r33ErrCode(out map[string]any) string {
	c, _ := jget(out, "error", "code").(string)
	return c
}

// TestPressTracer is the phase tracer: `sil press usr1` reaches the real
// firmware as a button level, the firmware's own on_click logic changes the
// fan duty, `sil status` shows it, and the firmware carries sil's identity.
func TestPressTracer(t *testing.T) {
	const name = "e2e-r33"
	in := r33Up(t, name)

	in.setFan(t, 1, 40)
	in.waitDuties(t, [4]float64{0.4, 0, 0, 0})

	start := sysClock.Now()
	out, code := in.press(t, "usr1")
	wall := sysClock.Now() - start
	if code != 0 {
		t.Fatalf("sil press usr1: exit %d: %v", code, out)
	}
	if out["button"] != "usr1" || out["action"] != "press" || jget(out, "hold_ms") != float64(200) {
		t.Fatalf("press result %v, want button usr1, action press, hold_ms 200", out)
	}
	down, _ := out["t_down_us"].(float64)
	up, _ := out["t_up_us"].(float64)
	if up-down < 200000 {
		t.Errorf("t_up_us - t_down_us = %v, want >= 200000", up-down)
	}
	if wall < 200*time.Millisecond {
		t.Errorf("sil press took %s of wall time, want at least the 200 ms hold", wall)
	}

	st := in.waitDuties(t, [4]float64{0.5, 0.1, 0.1, 0.1})
	for _, id := range []string{"usr1", "usr2", "usr3"} {
		if level, ok := r33Buttons(st, id); !ok || level {
			t.Errorf("buttons.%s = %v (present %v), want false", id, level, ok)
		}
	}
	if w, _ := st["warnings"].([]any); len(w) != 0 {
		t.Errorf("warnings = %v, want none", w)
	}

	// Identity (FW-01): the hand-run defaults of the YAML never reach the firmware.
	apiSt, _ := st["api"].(map[string]any)
	mac, _ := apiSt["mac"].(string)
	if apiSt["device_name"] != "sil-"+name {
		t.Errorf("status api.device_name = %v, want sil-%s", apiSt["device_name"], name)
	}
	if mac == "" || strings.EqualFold(mac, "02:00:00:00:00:01") {
		t.Errorf("status api.mac = %q, want the derived MAC and not the default", mac)
	}
	info, code := in.env.APIClient(t, in.port, in.key, "info")
	if code != 0 {
		t.Fatalf("api info: exit %d: %v", code, info)
	}
	if info["device_name"] != "sil-"+name {
		t.Errorf("firmware device_info name = %v, want sil-%s", info["device_name"], name)
	}
	if got, _ := info["mac_address"].(string); !strings.EqualFold(got, mac) {
		t.Errorf("firmware device_info mac = %q, want %q (the MAC sil derived)", got, mac)
	}
}

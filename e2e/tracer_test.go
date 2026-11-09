//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// jget walks nested JSON objects and arrays by key or index.
func jget(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// tracerPortValue is ports[i].<field> of a status object as a float.
func tracerPortValue(st map[string]any, i int, field string) float64 {
	f, _ := jget(st, "ports", i, field).(float64)
	return f
}

// pidGone reports whether no process with this PID exists.
func pidGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// TestTracer is the walking skeleton end to end: sil up builds and detaches
// the supervisor, the firmware's native API drives Fan 1, sil status shows
// duty and RPM, sil down leaves nothing behind.
func TestTracer(t *testing.T) {
	const name = "e2e-tracer"
	env := newEnv(t)
	start := sysClock.Now()
	up := env.Up(t, name)
	t.Logf("sil up %s took %s", name, (sysClock.Now() - start).Round(100*time.Millisecond))

	api, _ := up["api"].(map[string]any)
	port := int(jget(api, "port").(float64))
	if port != env.ports[name] {
		t.Errorf("api.port = %d, want the injected %d", port, env.ports[name])
	}
	key, _ := api["key"].(string)
	if key == "" {
		t.Fatalf("api.key is empty: %v", up)
	}
	if api["device_name"] != "sil-"+name {
		t.Errorf("api.device_name = %v, want sil-%s", api["device_name"], name)
	}
	if mac, _ := api["mac"].(string); !strings.HasPrefix(mac, "02:") {
		t.Errorf("api.mac = %q, want a 02: prefix", mac)
	}
	if api["host"] != "127.0.0.1" {
		t.Errorf("api.host = %v", api["host"])
	}

	st := env.Status(t, name)
	if st["phase"] != "ready" || jget(st, "firmware", "bus_connected") != true {
		t.Fatalf("status after up: phase %v, bus_connected %v", st["phase"], jget(st, "firmware", "bus_connected"))
	}
	if ports, _ := st["ports"].([]any); len(ports) != 4 {
		t.Fatalf("status has %d ports, want 4", len(ports))
	}

	out, code := env.APIClient(t, port, key, "set-fan", "--fan", "1", "--speed", "40")
	if code != 0 {
		t.Fatalf("set-fan exit %d: %v", code, out)
	}
	st = env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 0, "duty") == 0.4 && tracerPortValue(s, 0, "rpm") > 0
	})
	t.Logf("sil status: port 1 duty %v rpm %.1f state %v", jget(st, "ports", 0, "duty"), tracerPortValue(st, 0, "rpm"), jget(st, "ports", 0, "state"))

	out, code = env.APIClient(t, port, key, "watch", "--seconds", "3")
	if code != 0 {
		t.Fatalf("watch exit %d: %v", code, out)
	}
	var best float64
	for _, s := range jget(out, "series", "Fan 1 Speed").([]any) {
		if v, ok := jget(s, 1).(float64); ok && v > best {
			best = v
		}
	}
	if best <= 0 {
		t.Fatalf("Fan 1 Speed never rose above 0: %v", out)
	}

	raw, err := os.ReadFile(filepath.Join(env.Runtime, "sil", name, "run.json"))
	if err != nil {
		t.Fatalf("run.json: %v", err)
	}
	var rf struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil || rf.PID == 0 {
		t.Fatalf("run.json has no pid: %s (%v)", raw, err)
	}
	fwPID := int(jget(env.Status(t, name), "firmware", "pid").(float64))
	if fwPID == 0 || fwPID == rf.PID {
		t.Fatalf("firmware pid %d, serve pid %d", fwPID, rf.PID)
	}

	down, code, stderr := env.Sil(t, "down", name)
	if code != 0 || down["stopped"] != true {
		t.Fatalf("sil down: exit %d: %v\nstderr: %s", code, down, stderr)
	}
	waitFor(t, 5*time.Second, "serve and firmware processes to be gone", func() bool {
		return pidGone(rf.PID) && pidGone(fwPID)
	})
	gone, code, _ := env.Sil(t, "status", name)
	if code != 1 || jget(gone, "error", "code") != "not_running" {
		t.Fatalf("sil status after down: exit %d: %v", code, gone)
	}
}

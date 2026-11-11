//go:build sil_e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// modelAPI returns the firmware API port and key out of a `sil up` result.
func modelAPI(t *testing.T, up map[string]any) (int, string) {
	t.Helper()
	port, _ := jget(up, "api", "port").(float64)
	key, _ := jget(up, "api", "key").(string)
	if port == 0 || key == "" {
		t.Fatalf("sil up result has no api.port/api.key: %v", up)
	}
	return int(port), key
}

// modelSetFan sets Fan n to a speed level through the firmware's native API.
func modelSetFan(t *testing.T, env *Env, port int, key string, fan, speed int) {
	t.Helper()
	out, code := env.APIClient(t, port, key, "set-fan", "--fan", fmt.Sprint(fan), "--speed", fmt.Sprint(speed))
	if code != 0 {
		t.Fatalf("set-fan --fan %d --speed %d: exit %d: %v", fan, speed, code, out)
	}
}

// modelPortState is ports[i].state of a status object.
func modelPortState(st map[string]any, i int) string {
	s, _ := jget(st, "ports", i, "state").(string)
	return s
}

// modelSimTime is sim.time_s of a status object.
func modelSimTime(st map[string]any) float64 {
	f, _ := jget(st, "sim", "time_s").(float64)
	return f
}

// modelObserve calls check on a fresh status every 200 ms of real time for d.
func modelObserve(t *testing.T, env *Env, name string, d time.Duration, check func(st map[string]any)) int {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, d)
	defer cancel()
	samples := 0
	for {
		st := env.Status(t, name)
		check(st)
		samples++
		if err := sysClock.Sleep(ctx, 200*time.Millisecond); err != nil {
			return samples
		}
	}
}

// TestModelThreshold drives Fan 2 of the real bench through the Arctic fan
// model (start_duty 0.10, min_duty 0.05, stop below it, tau_up 1 s): a fan
// at rest ignores duty below start_duty, spins up with a first-order lag
// above it, keeps running in the dead band and stalls below min_duty.
func TestModelThreshold(t *testing.T) {
	const name = "e2e-model"
	env := newEnv(t)
	up := env.Up(t, name)
	port, key := modelAPI(t, up)

	// Fan 2 at speed 8 (duty 0.08 < start_duty 0.10): stays at rest.
	modelSetFan(t, env, port, key, 2, 8)
	env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 1, "duty") == 0.08
	})
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

	// Speed 40 (duty 0.4): reaches half of its target within 1.5 s of sim time.
	const target = 200 + 0.35/0.95*1600
	modelSetFan(t, env, port, key, 2, 40)
	seen := env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 1, "duty") == 0.4
	})
	t0 := modelSimTime(seen)
	half := env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 1, "rpm") >= 0.5*target
	})
	if dt := modelSimTime(half) - t0; dt > 1.5 {
		t.Errorf("Fan 2 took %.2f s of sim time to reach %.0f rpm (50 %% of %.0f), want <= 1.5 s", dt, 0.5*target, target)
	} else {
		t.Logf("Fan 2 reached 50 %% of target after %.2f s of sim time (duty seen at sim %.2f s)", dt, t0)
	}

	// The firmware's own `Fan 2 Speed` sensor sees the spin.
	out, code := env.APIClient(t, port, key, "watch", "--seconds", "3")
	if code != 0 {
		t.Fatalf("watch exit %d: %v", code, out)
	}
	series, _ := jget(out, "series", "Fan 2 Speed").([]any)
	var best float64
	for _, s := range series {
		if v, ok := jget(s, 1).(float64); ok && v > best {
			best = v
		}
	}
	if best <= 0 {
		t.Fatalf("Fan 2 Speed never rose above 0: %v", out)
	}
	t.Logf("firmware Fan 2 Speed peaked at %.1f", best)

	// Speed 7 (duty 0.07, dead band between min_duty and start_duty): a
	// running fan keeps running.
	modelSetFan(t, env, port, key, 2, 7)
	env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 1, "duty") == 0.07
	})
	modelObserve(t, env, name, 2*time.Second, func(st map[string]any) {
		if s := modelPortState(st, 1); s != "running" {
			t.Fatalf("Fan 2 in the dead band (duty 0.07) is %q, want running", s)
		}
	})
	if rpm := tracerPortValue(env.Status(t, name), 1, "rpm"); rpm <= 0 {
		t.Errorf("Fan 2 in the dead band has rpm %v, want > 0", rpm)
	}

	// Speed 4 (duty 0.04 < min_duty): stalls, rpm coasts down.
	modelSetFan(t, env, port, key, 2, 4)
	first := env.WaitStatus(t, name, 10*time.Second, func(s map[string]any) bool {
		return tracerPortValue(s, 1, "duty") == 0.04 && modelPortState(s, 1) == "stopped"
	})
	r1 := tracerPortValue(first, 1, "rpm")
	if r1 <= 0 {
		t.Fatalf("stalled Fan 2 already at rpm %v; coast-down not observable", r1)
	}
	if err := sysClock.Sleep(context.Background(), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	second := env.Status(t, name)
	if r2 := tracerPortValue(second, 1, "rpm"); !(r2 < r1) || modelPortState(second, 1) != "stopped" {
		t.Errorf("after stall rpm %v -> %v (state %s), want decreasing and stopped", r1, r2, modelPortState(second, 1))
	}
}

// TestFanTomlUnknownKey proves criterion 5: a typo in a fan file stops `sil up` with a located
// message before any firmware build starts, in the shipped schema 2 library and in a schema 1 file.
func TestFanTomlUnknownKey(t *testing.T) {
	cases := []struct {
		name  string
		file  string // path of the original, relative to the repo root
		key   string // the key the typo is made in, and the start of its line
		typo  string // the misspelt key
		name2 string // instance name
	}{
		{"schema2_shipped", "fans/arctic-p12-pwm-pst.toml", "noise_rpm", "noise_rmp", "e2e-badfan2"},
		{"schema1_fixture", "testdata/fans-v0.1.0/arctic-p12-pwm-pst.toml", "max_rpm", "max_rmp", "e2e-badfan1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(c.file)))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(raw), "\n")
			typoLine := 0
			for i, l := range lines {
				if strings.HasPrefix(l, c.key) {
					lines[i] = strings.Replace(l, c.key, c.typo, 1)
					typoLine = i + 1
					break
				}
			}
			if typoLine == 0 {
				t.Fatalf("%s has no line starting with %s", c.file, c.key)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "arctic-p12-pwm-pst.toml"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			env.Extra = append(env.Extra, "SIL_FAN_DIR="+dir)

			out, exit, stderr := env.Sil(t, "up", c.name2)
			if exit != 1 {
				t.Fatalf("sil up with a typo in the fan file: exit %d, want 1: %v", exit, out)
			}
			if jget(out, "error", "code") != "fan_toml_invalid" {
				t.Fatalf("error.code = %v, want fan_toml_invalid: %v", jget(out, "error", "code"), out)
			}
			msg, _ := jget(out, "error", "message").(string)
			if !strings.Contains(msg, c.typo) {
				t.Errorf("message %q does not name %s", msg, c.typo)
			}
			if want := fmt.Sprintf(":%d:", typoLine); !strings.Contains(msg, want) {
				t.Errorf("message %q does not carry the line (%s)", msg, want)
			}
			if !strings.Contains(msg, filepath.Join(dir, "arctic-p12-pwm-pst.toml")) {
				t.Errorf("message %q does not name the file under SIL_FAN_DIR", msg)
			}
			if strings.Contains(stderr, "building firmware") {
				t.Errorf("a firmware build was started: stderr %q", stderr)
			}
			if _, err := os.Stat(filepath.Join(env.Runtime, "sil", c.name2, "build.log")); !os.IsNotExist(err) {
				t.Errorf("build.log exists after a rejected fan file (stat err: %v)", err)
			}
		})
	}
}

//go:build sil_e2e

package e2e

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// TestModelV2Curves sweeps Fan 1 of the real firmware up and then down through its fan entity with
// the schema 2 curve-test fixture plugged in (separate up and down curves, ideal tach). `sil status`
// and `sil watch` show a lower rpm on the way down than on the way up at the same duty.
func TestModelV2Curves(t *testing.T) {
	const name = "e2e-curves"
	env := newEnv(t)
	env.Extra = append(env.Extra, "SIL_FAN_DIR="+filepath.Join(repoRoot(t), "testdata", "fans-v2"))
	up := env.Up(t, name, "--fan", "curve-test")
	port, key := modelAPI(t, up)

	w := watchStart(t, env, "watch", "-i", name, "--kind", "duty,rpm")

	steps := []struct {
		speed int
		want  float64 // settled rpm of Fan 1
		what  string
	}{
		{20, 600, "up, curve.up at 0.2"},
		{40, 1000, "up, curve.up at 0.4"},
		{50, 1150, "up, interpolated between 1000 and 1300"},
		{60, 1300, "up, curve.up at 0.6"},
		{80, 1550, "up, curve.up at 0.8"},
		{60, 1200, "down, curve.down at 0.6"},
		{40, 900, "down, curve.down at 0.4"},
	}
	var rpmUp60, rpmDown60 float64
	var log []float64
	for _, s := range steps {
		modelSetFan(t, env, port, key, 1, s.speed)
		st := env.WaitStatus(t, name, 15*time.Second, func(st map[string]any) bool {
			return math.Abs(tracerPortValue(st, 0, "rpm")-s.want) <= 1 &&
				math.Abs(tracerPortValue(st, 0, "duty")-float64(s.speed)/100) < 1e-6
		})
		rpm := tracerPortValue(st, 0, "rpm")
		log = append(log, rpm)
		t.Logf("speed %d (%s): rpm %.2f, want %.0f", s.speed, s.what, rpm, s.want)
		if s.speed == 60 && s.want == 1300 {
			rpmUp60 = rpm
		}
		if s.speed == 60 && s.want == 1200 {
			rpmDown60 = rpm
		}
	}
	if rpmUp60-rpmDown60 < 90 {
		t.Errorf("rpm at speed 60: up %.1f, down %.1f, want the down branch at least 90 lower", rpmUp60, rpmDown60)
	}
	t.Logf("observed sequence: %v", log)

	// The event stream carried the fall: an rpm event within 2 of 1200 after the duty went from 0.8
	// back to 0.6 (not the earlier rise through 1200 on the way up).
	saw80, fell := false, false
	w.until(t, 20*time.Second, "an rpm event within 2 of 1200 after the fall to speed 60", func(m map[string]any) bool {
		if m["port"] != float64(1) {
			return false
		}
		v, _ := m["v"].(float64)
		switch m["k"] {
		case "duty":
			if math.Abs(v-0.8) < 1e-6 {
				saw80 = true
			}
			if saw80 && math.Abs(v-0.6) < 1e-6 {
				fell = true
			}
		case "rpm":
			return fell && math.Abs(v-1200) <= 2
		}
		return false
	})
}

package engine

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// TestEngineCurveFan drives a schema 2 fan (curve-test fixture, ideal tach) from its file to the
// snapshot, the rpm events and the bus frames: the port follows the up curve, interpolated between
// two table points.
func TestEngineCurveFan(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fans-v2"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t) // loads the embedded default fan, so the override comes after it
	t.Setenv(fanlib.EnvFanDir, dir)
	replug(t, h, 1, "curve-test")

	h.setDuty(1, 0.4)
	h.advanceTo(10 * time.Second)
	p := h.eng.Snapshot().Ports[0]
	if p.Fan != "curve-test" || p.State != "running" {
		t.Fatalf("port 1 = %+v, want the running curve-test fan", p)
	}
	if math.Abs(p.RPM-1000) > 0.01 {
		t.Fatalf("rpm at duty 0.4 = %v, want 1000 (curve.up at 0.4)", p.RPM)
	}
	want := float32(p.RPM)
	waitUntil(t, "the settled port1.rpm frame", func() bool {
		frames := h.fw.rpmFrames("port1.rpm")
		if len(frames) == 0 {
			return false
		}
		last, ok := frames[len(frames)-1].GetNum("v")
		return ok && float32(last) == want
	})

	h.setDuty(1, 0.5)
	h.advanceTo(10 * time.Second)
	if p := h.eng.Snapshot().Ports[0]; math.Abs(p.RPM-1150) > 0.01 {
		t.Errorf("rpm at duty 0.5 = %v, want 1150 (interpolated between 1000 and 1300)", p.RPM)
	}
}

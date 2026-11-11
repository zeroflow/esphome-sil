//go:build sil_e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// loopPorts is the four-port configuration of the skeleton firmware.
func loopPorts(t testing.TB) []engine.PortConfig {
	t.Helper()
	// The in-process engine runs the exact v0.1.0 numbers (D-02): pin the library to the fixtures.
	t.Setenv(fanlib.EnvFanDir, filepath.Join(repoRoot(t), "testdata", "fans-v0.1.0"))
	fan, err := fanlib.Load(fanlib.DefaultFan)
	if err != nil {
		t.Fatalf("load default fan: %v", err)
	}
	var ports []engine.PortConfig
	for n := 1; n <= 4; n++ {
		ports = append(ports, engine.PortConfig{Port: n, FanID: fanlib.DefaultFan, Fan: fan})
	}
	return ports
}

// TestLoop closes the loop on real firmware: fan duty from the encrypted
// native API reaches the in-process engine over the sim-bus, the fan model
// turns it into RPM, and the RPM comes back as the firmware's Fan 1 Speed.
func TestLoop(t *testing.T) {
	id := newFWIdentity(t, "e2e-loop")
	program := buildFirmware(t, id)

	dir := shortTempDir(t)
	busPath := filepath.Join(dir, "bus.sock")

	eng := engine.New(engine.Config{Clock: clock.NewReal(), BusPath: busPath, Ports: loopPorts(t)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- eng.Run(ctx) }()
	select {
	case <-eng.Ready():
	case err := <-runErr:
		t.Fatalf("engine stopped before it was ready: %v", err)
	}

	startFirmware(t, program, busPath, filepath.Join(dir, "prefs"))
	waitFor(t, 10*time.Second, "the firmware to connect to the engine", func() bool {
		return eng.Snapshot().BusConnected
	})

	out, code := runAPIClient(t, id.Port, id.Key, "set-fan", "--fan", "1", "--speed", "40")
	if code != 0 {
		t.Fatalf("set-fan exit %d: %v", code, out)
	}
	waitFor(t, 10*time.Second, "engine Ports[0].Duty == 0.4 and RPM > 0", func() bool {
		p := eng.Snapshot().Ports[0]
		return p.Duty == 0.4 && p.RPM > 0
	})
	snap := eng.Snapshot()
	t.Logf("engine after fan 1 speed 40: sim %s, port 1 duty %v rpm %.1f state %s", snap.SimTime, snap.Ports[0].Duty, snap.Ports[0].RPM, snap.Ports[0].State)
	if snap.Ports[1].Duty != 0 || snap.Ports[1].RPM != 0 {
		t.Errorf("port 2 moved without a command: %+v", snap.Ports[1])
	}

	out, code = runAPIClient(t, id.Port, id.Key, "watch", "--seconds", "3")
	if code != 0 {
		t.Fatalf("watch exit %d: %v", code, out)
	}
	series, _ := out["series"].(map[string]any)
	samples, _ := series["Fan 1 Speed"].([]any)
	var best float64
	for _, s := range samples {
		if pair, ok := s.([]any); ok && len(pair) == 2 {
			if v, ok := pair[1].(float64); ok && v > best {
				best = v
			}
		}
	}
	if best <= 0 {
		t.Fatalf("Fan 1 Speed never rose above 0: %v", series)
	}
	t.Logf("firmware Fan 1 Speed peaked at %.0f rpm", best)

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("engine Run returned %v, want nil", err)
		}
	case <-sysClock.NewTimer(5 * time.Second).C():
		t.Fatalf("engine did not stop after its context was cancelled")
	}
	if _, err := os.Stat(busPath); !os.IsNotExist(err) {
		t.Errorf("bus socket still present after Run returned: %v", err)
	}
}

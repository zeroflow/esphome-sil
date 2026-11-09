package supervisor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apiclient"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

// The readiness identity check: only the instance's own firmware (device
// name and MAC, case and colon insensitive) counts as ready.
func TestMismatch(t *testing.T) {
	id := instance.Identity{Name: "a", APIPort: 6054}
	mac := instance.MAC("a")
	own := apiclient.Hello{ServerName: "sil-a", Name: "sil-a", MAC: "02" + mac[3:5] + mac[6:8] + mac[9:11] + mac[12:14] + mac[15:17]}
	if bad := mismatch(own, id); bad != nil {
		t.Fatalf("own firmware rejected: %v", bad)
	}
	upper := own
	upper.MAC = "02:" + mac[3:]
	if bad := mismatch(apiclient.Hello{ServerName: "sil-a", Name: "sil-a", MAC: upper.MAC}, id); bad != nil {
		t.Errorf("colon form rejected: %v", bad)
	}

	for name, h := range map[string]apiclient.Hello{
		"other instance": {ServerName: "sil-b", Name: "sil-b", MAC: own.MAC},
		"other mac":      {ServerName: "sil-a", Name: "sil-a", MAC: "02aaaaaaaa01"},
		"real device":    {ServerName: "fancontroller", Name: "fancontroller", MAC: "a0b1c2d3e4f5"},
		"response name":  {ServerName: "sil-a", Name: "sil-b", MAC: own.MAC},
	} {
		bad := mismatch(h, id)
		if bad == nil || bad.Code != "api_identity_mismatch" || bad.Exit != apperr.ExitInfra {
			t.Errorf("%s: %v, want api_identity_mismatch (exit 2)", name, bad)
		}
	}
}

// buttonSupervisor is a supervisor with a running engine and no firmware.
func buttonSupervisor(t *testing.T) *supervisor {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	clk := clock.NewManual()
	s := &supervisor{
		clk:     clk,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		engCfg:  engine.Config{Clock: clk, BusPath: filepath.Join(dir, "bus.sock")},
		engDied: make(chan error, 1),
		downReq: make(chan struct{}),
	}
	sl, err := s.startEngine()
	if err != nil {
		t.Fatal(err)
	}
	s.slot = sl
	t.Cleanup(func() { s.stopEngine(sl) })
	return s
}

func errCode(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// The button operations map engine sentinels to their exit-1 codes and never
// queue anything; Status lists every button.
func TestButtonOperationErrors(t *testing.T) {
	s := buttonSupervisor(t)
	ctx := context.Background()

	st, err := s.Status(ctx)
	if err != nil || len(st.Buttons) != 3 || st.Buttons["usr1"] || st.Buttons["usr2"] || st.Buttons["usr3"] {
		t.Fatalf("Status.Buttons = %v (%v), want usr1..usr3 all false", st.Buttons, err)
	}

	// No firmware on the bus.
	if _, err := s.Press(ctx, "usr1", control.PressRequest{HoldMS: 200}); errCode(err) != "firmware_not_connected" {
		t.Fatalf("press without a firmware: %v, want firmware_not_connected", err)
	}
	for _, down := range []bool{true, false} {
		if _, err := s.SetButton(ctx, "usr1", down); errCode(err) != "firmware_not_connected" {
			t.Fatalf("SetButton(down=%v) without a firmware: %v, want firmware_not_connected", down, err)
		}
	}

	// Argument errors come before the bus is asked.
	_, err = s.Press(ctx, "usr4", control.PressRequest{HoldMS: 200})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "invalid_argument" || ae.Exit != apperr.ExitFailed || ae.Detail["button"] != "usr4" {
		t.Fatalf("unknown button: %v, want invalid_argument naming usr4", err)
	}
	if known, _ := ae.Detail["known"].([]string); len(known) != 3 {
		t.Fatalf("detail.known = %v, want usr1..usr3", ae.Detail["known"])
	}
	if _, err := s.SetButton(ctx, "usr4", true); errCode(err) != "invalid_argument" {
		t.Fatalf("SetButton on usr4: %v, want invalid_argument", err)
	}
	if _, err := s.Press(ctx, "usr1", control.PressRequest{HoldMS: 60001}); errCode(err) != "invalid_argument" {
		t.Fatalf("hold above the cap: %v, want invalid_argument", err)
	}
}

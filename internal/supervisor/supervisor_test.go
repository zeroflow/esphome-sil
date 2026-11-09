package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func rawVals(kv map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(kv))
	for k, v := range kv {
		out[k] = json.RawMessage(v)
	}
	return out
}

// parseSet is pure: every pair is checked before anything is returned, so a bad pair never leaves
// half a change behind.
func TestParseSet(t *testing.T) {
	t.Run("bounds accepted", func(t *testing.T) {
		for _, kv := range []map[string]string{
			{"temperature": "-40"}, {"temperature": "125"}, {"humidity": "0"}, {"humidity": "100"},
			{"temperature": `"-40"`, "humidity": `"100"`},
		} {
			if _, _, err := parseSet(rawVals(kv)); err != nil {
				t.Errorf("%v: %v, want accepted", kv, err)
			}
		}
	})
	t.Run("just outside is value_out_of_range", func(t *testing.T) {
		for _, tc := range []struct{ key, val string }{
			{"temperature", "-40.1"}, {"temperature", "125.1"}, {"humidity", "-0.1"}, {"humidity", "100.1"},
			{"temperature", `"125.1"`},
		} {
			_, _, err := parseSet(rawVals(map[string]string{tc.key: tc.val}))
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Code != "value_out_of_range" || ae.Exit != apperr.ExitFailed || ae.Detail["key"] != tc.key {
				t.Errorf("%s=%s: %v, want value_out_of_range naming %s", tc.key, tc.val, err, tc.key)
				continue
			}
			if _, ok := ae.Detail["min"]; !ok {
				t.Errorf("%s=%s: detail %v has no min", tc.key, tc.val, ae.Detail)
			}
			if _, ok := ae.Detail["max"]; !ok {
				t.Errorf("%s=%s: detail %v has no max", tc.key, tc.val, ae.Detail)
			}
		}
	})
	t.Run("not a number", func(t *testing.T) {
		for _, v := range []string{`"warm"`, `"NaN"`, `"Inf"`, `"-Inf"`, `"+Infinity"`, `"0x10"`, `"1_0"`, `""`, `" "`, `"1e999"`, `true`, `null`, `[1]`, `{}`} {
			_, _, err := parseSet(rawVals(map[string]string{"temperature": v}))
			if errCode(err) != "invalid_argument" {
				t.Errorf("temperature=%s: %v, want invalid_argument", v, err)
			}
		}
	})
	t.Run("number and string forms agree", func(t *testing.T) {
		for _, v := range []string{"24.5", `"24.5"`, `" 24.5 "`, "2.45e1", `"+24.5"`} {
			c, applied, err := parseSet(rawVals(map[string]string{"temperature": v}))
			if err != nil || c.Temperature == nil || *c.Temperature != 24.5 || c.Humidity != nil || applied["temperature"] != 24.5 {
				t.Errorf("temperature=%s: change %+v, echo %v, err %v, want temperature 24.5 only", v, c, applied, err)
			}
		}
	})
	t.Run("all or nothing", func(t *testing.T) {
		c, applied, err := parseSet(rawVals(map[string]string{"temperature": "30", "humidity": "101"}))
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "value_out_of_range" || ae.Detail["key"] != "humidity" {
			t.Fatalf("err = %v, want value_out_of_range naming humidity", err)
		}
		if c.Temperature != nil || c.Humidity != nil || applied != nil {
			t.Fatalf("change %+v and echo %v returned with an error, want nothing", c, applied)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		_, _, err := parseSet(rawVals(map[string]string{"temperature": "20", "fanspeed": "3"}))
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "unknown_key" || ae.Detail["key"] != "fanspeed" {
			t.Fatalf("err = %v, want unknown_key naming fanspeed", err)
		}
		if known, _ := ae.Detail["known"].([]string); fmt.Sprint(known) != "[humidity temperature]" {
			t.Fatalf("detail.known = %v, want [humidity temperature]", ae.Detail["known"])
		}
	})
	t.Run("empty map", func(t *testing.T) {
		for _, m := range []map[string]json.RawMessage{nil, {}} {
			if _, _, err := parseSet(m); errCode(err) != "invalid_argument" {
				t.Errorf("parseSet(%v): %v, want invalid_argument", m, err)
			}
		}
	})
	t.Run("echo keys equal input keys", func(t *testing.T) {
		_, applied, err := parseSet(rawVals(map[string]string{"humidity": "40", "temperature": `"24.50"`}))
		if err != nil || len(applied) != 2 || applied["humidity"] != 40.0 || applied["temperature"] != 24.5 {
			t.Fatalf("echo %v (%v), want humidity 40 and temperature 24.5", applied, err)
		}
	})
	t.Run("first error in sorted key order", func(t *testing.T) {
		// humidity sorts before temperature, so its error wins regardless of map order.
		for i := 0; i < 20; i++ {
			_, _, err := parseSet(rawVals(map[string]string{"temperature": "999", "humidity": `"x"`}))
			if errCode(err) != "invalid_argument" {
				t.Fatalf("err = %v, want the humidity invalid_argument first", err)
			}
		}
	})
}

// Set records the inputs, applies them to the engine in the slot and seeds the next engine.
func TestSetSeedsTheNextEngine(t *testing.T) {
	s := buttonSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	ctx := context.Background()

	res, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"temperature": `"24.5"`, "humidity": "40"})})
	if err != nil || res.Applied["temperature"] != 24.5 || res.Applied["humidity"] != 40.0 {
		t.Fatalf("Set: %v, %v", res, err)
	}
	waitFor(t, "the engine to show the set values", func() bool {
		e := s.currentEngine().Snapshot().Env
		return e.Temperature == 24.5 && e.Humidity == 40
	})
	if st, _ := s.Status(ctx); st.Env.Temperature != 24.5 || st.Env.Humidity != 40 {
		t.Fatalf("Status.Env = %+v, want 24.5 and 40", st.Env)
	}

	// A failed Set changes nothing, in the inputs or in the engine.
	if _, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"temperature": "30", "humidity": "101"})}); errCode(err) != "value_out_of_range" {
		t.Fatalf("Set out of range: %v", err)
	}
	if s.inputs.Temperature != 24.5 || s.inputs.Humidity != 40 {
		t.Fatalf("inputs %+v after a rejected Set, want 24.5 and 40", s.inputs)
	}

	// A replaced engine starts from the recorded inputs; a Set while no engine runs is accepted.
	old := s.currentEngine()
	s.stopEngine(s.slot)
	if _, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"humidity": "55"})}); err != nil {
		t.Fatalf("Set with the engine stopped: %v, want accepted", err)
	}
	if old.Snapshot().Env.Humidity == 55 {
		t.Fatalf("the stopped engine took the new value")
	}
	sl, err := s.startEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.stopEngine(sl) })
	if e := sl.eng.Snapshot().Env; e.Temperature != 24.5 || e.Humidity != 55 {
		t.Fatalf("new engine env = %+v, want 24.5 and 55", e)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	rc := clock.NewReal()
	deadline := rc.Now() + 5*time.Second
	for !cond() {
		if rc.Now() > deadline {
			t.Fatalf("timed out waiting for %s", what)
		}
		_ = rc.Sleep(context.Background(), 5*time.Millisecond)
	}
}

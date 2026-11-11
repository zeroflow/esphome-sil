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
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apiclient"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/instance"
	"github.com/zeroflow/esphome-sil/internal/model"
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
		if _, err := s.SetButton(ctx, "usr1", down, control.DownRequest{}); errCode(err) != "firmware_not_connected" {
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
	if _, err := s.SetButton(ctx, "usr4", true, control.DownRequest{}); errCode(err) != "invalid_argument" {
		t.Fatalf("SetButton on usr4: %v, want invalid_argument", err)
	}
	if _, err := s.Press(ctx, "usr1", control.PressRequest{HoldMS: 60001}); errCode(err) != "invalid_argument" {
		t.Fatalf("hold above the cap: %v, want invalid_argument", err)
	}
	// A max hold only applies to down, and is range-checked, before the bus is asked.
	one, over := int64(1000), int64(60001)
	if _, err := s.SetButton(ctx, "usr1", false, control.DownRequest{MaxHoldMS: &one}); errCode(err) != "invalid_argument" {
		t.Fatalf("up with a max hold: %v, want invalid_argument", err)
	}
	if _, err := s.SetButton(ctx, "usr1", true, control.DownRequest{MaxHoldMS: &over}); errCode(err) != "invalid_argument" {
		t.Fatalf("max hold above the cap: %v, want invalid_argument", err)
	}
	if _, err := s.SetButton(ctx, "usr1", true, control.DownRequest{MaxHoldMS: &one}); errCode(err) != "firmware_not_connected" {
		t.Fatalf("a valid max hold without a firmware: %v, want firmware_not_connected", err)
	}
}

// libFake is a fan library with a fixed id list; Load answers a distinguishable Params per id and
// records every id it was asked for.
type libFake struct {
	ids    []string
	bad    map[string]error // ids whose Load fails
	loaded *[]string
}

func (l libFake) IDs() ([]string, error) { return append([]string{}, l.ids...), nil }
func (l libFake) Load(id string) (model.Params, error) {
	if l.loaded != nil {
		*l.loaded = append(*l.loaded, id)
	}
	if err := l.bad[id]; err != nil {
		return model.Params{}, err
	}
	return model.Params{Name: "fake " + id, MinRPM: 100, MaxRPM: 1000, BelowMin: "stop", MinDuty: 0.05, StartDuty: 0.1, TauUp: 1, TauDown: 1}, nil
}

var fakeLib FanLibrary = libFake{ids: []string{"alpha-fan", "beta-fan"}}

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
			if _, _, err := parseSet(rawVals(kv), 4, fakeLib); err != nil {
				t.Errorf("%v: %v, want accepted", kv, err)
			}
		}
	})
	t.Run("just outside is value_out_of_range", func(t *testing.T) {
		for _, tc := range []struct{ key, val string }{
			{"temperature", "-40.1"}, {"temperature", "125.1"}, {"humidity", "-0.1"}, {"humidity", "100.1"},
			{"temperature", `"125.1"`},
		} {
			_, _, err := parseSet(rawVals(map[string]string{tc.key: tc.val}), 4, fakeLib)
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
			_, _, err := parseSet(rawVals(map[string]string{"temperature": v}), 4, fakeLib)
			if errCode(err) != "invalid_argument" {
				t.Errorf("temperature=%s: %v, want invalid_argument", v, err)
			}
		}
	})
	t.Run("number and string forms agree", func(t *testing.T) {
		for _, v := range []string{"24.5", `"24.5"`, `" 24.5 "`, "2.45e1", `"+24.5"`} {
			c, applied, err := parseSet(rawVals(map[string]string{"temperature": v}), 4, fakeLib)
			if err != nil || c.Temperature == nil || *c.Temperature != 24.5 || c.Humidity != nil || applied["temperature"] != 24.5 {
				t.Errorf("temperature=%s: change %+v, echo %v, err %v, want temperature 24.5 only", v, c, applied, err)
			}
		}
	})
	t.Run("all or nothing", func(t *testing.T) {
		c, applied, err := parseSet(rawVals(map[string]string{"temperature": "30", "humidity": "101"}), 4, fakeLib)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "value_out_of_range" || ae.Detail["key"] != "humidity" {
			t.Fatalf("err = %v, want value_out_of_range naming humidity", err)
		}
		if c.Temperature != nil || c.Humidity != nil || applied != nil {
			t.Fatalf("change %+v and echo %v returned with an error, want nothing", c, applied)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		_, _, err := parseSet(rawVals(map[string]string{"temperature": "20", "fanspeed": "3"}), 4, fakeLib)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "unknown_key" || ae.Detail["key"] != "fanspeed" {
			t.Fatalf("err = %v, want unknown_key naming fanspeed", err)
		}
		if known, _ := ae.Detail["known"].([]string); fmt.Sprint(known) != "[humidity port1.fan port2.fan port3.fan port4.fan temperature]" {
			t.Fatalf("detail.known = %v, want the env keys and port1.fan..port4.fan sorted", ae.Detail["known"])
		}
	})
	t.Run("empty map", func(t *testing.T) {
		for _, m := range []map[string]json.RawMessage{nil, {}} {
			if _, _, err := parseSet(m, 4, fakeLib); errCode(err) != "invalid_argument" {
				t.Errorf("parseSet(%v): %v, want invalid_argument", m, err)
			}
		}
	})
	t.Run("echo keys equal input keys", func(t *testing.T) {
		_, applied, err := parseSet(rawVals(map[string]string{"humidity": "40", "temperature": `"24.50"`}), 4, fakeLib)
		if err != nil || len(applied) != 2 || applied["humidity"] != 40.0 || applied["temperature"] != 24.5 {
			t.Fatalf("echo %v (%v), want humidity 40 and temperature 24.5", applied, err)
		}
	})
	t.Run("first error in sorted key order", func(t *testing.T) {
		// humidity sorts before temperature, so its error wins regardless of map order.
		for i := 0; i < 20; i++ {
			_, _, err := parseSet(rawVals(map[string]string{"temperature": "999", "humidity": `"x"`}), 4, fakeLib)
			if errCode(err) != "invalid_argument" {
				t.Fatalf("err = %v, want the humidity invalid_argument first", err)
			}
		}
	})
}

// portN.fan keys: a known fan builds a replug, everything else is an error with nothing applied.
func TestParseSetFanKeys(t *testing.T) {
	t.Run("known fan", func(t *testing.T) {
		c, applied, err := parseSet(rawVals(map[string]string{"port2.fan": `"beta-fan"`}), 4, fakeLib)
		if err != nil {
			t.Fatal(err)
		}
		pc, ok := c.Fans[2]
		if !ok || len(c.Fans) != 1 || pc.Port != 2 || pc.FanID != "beta-fan" || pc.Fan.Name != "fake beta-fan" {
			t.Fatalf("Change.Fans = %+v, want only port 2 with the loaded beta-fan", c.Fans)
		}
		if c.Temperature != nil || c.Humidity != nil || applied["port2.fan"] != "beta-fan" || len(applied) != 1 {
			t.Fatalf("change %+v, echo %v, want the fan only", c, applied)
		}
	})
	t.Run("fan and env together", func(t *testing.T) {
		c, applied, err := parseSet(rawVals(map[string]string{"port1.fan": `"alpha-fan"`, "temperature": "25", "port4.fan": `"beta-fan"`}), 4, fakeLib)
		if err != nil || c.Temperature == nil || *c.Temperature != 25 || len(c.Fans) != 2 || len(applied) != 3 {
			t.Fatalf("change %+v echo %v err %v, want temperature and two replugs", c, applied, err)
		}
	})
	t.Run("unknown fan", func(t *testing.T) {
		_, _, err := parseSet(rawVals(map[string]string{"port1.fan": `"nope"`}), 4, libFake{ids: []string{"zeta", "alpha-fan", "beta-fan"}})
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "unknown_fan" || ae.Exit != apperr.ExitFailed || ae.Detail["key"] != "port1.fan" || ae.Detail["fan"] != "nope" {
			t.Fatalf("err = %v, want unknown_fan naming port1.fan and nope", err)
		}
		// detail.known is the library's id list (fanlib.IDs returns it sorted).
		if known, _ := ae.Detail["known"].([]string); len(known) != 3 {
			t.Fatalf("detail.known = %v, want the library ids", ae.Detail["known"])
		}
	})
	t.Run("ids that are paths never reach Load", func(t *testing.T) {
		var loaded []string
		lib := libFake{ids: []string{"alpha-fan"}, loaded: &loaded}
		for _, id := range []string{"../alpha-fan", "../../etc/passwd", "alpha-fan/../alpha-fan", "/etc/passwd", "alpha-fan.toml", "", " alpha-fan", "ALPHA-FAN"} {
			raw, _ := json.Marshal(id)
			_, _, err := parseSet(rawVals(map[string]string{"port1.fan": string(raw)}), 4, lib)
			if errCode(err) != "unknown_fan" {
				t.Errorf("id %q: %v, want unknown_fan", id, err)
			}
		}
		if len(loaded) != 0 {
			t.Fatalf("Load was called for %v, want no call for an id outside the library (T-02-12)", loaded)
		}
	})
	t.Run("port outside the configured ports", func(t *testing.T) {
		for _, k := range []string{"port5.fan", "port0.fan", "port04.fan", "port1.fan ", "Port1.fan", "port1", "port1.duty"} {
			_, _, err := parseSet(rawVals(map[string]string{k: `"alpha-fan"`}), 4, fakeLib)
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Code != "unknown_key" || ae.Detail["key"] != k {
				t.Errorf("%q: %v, want unknown_key", k, err)
			}
		}
		// With two ports, port3.fan is unknown and port2.fan is fine.
		if _, _, err := parseSet(rawVals(map[string]string{"port3.fan": `"alpha-fan"`}), 2, fakeLib); errCode(err) != "unknown_key" {
			t.Errorf("port3.fan with 2 ports: %v, want unknown_key", err)
		}
		if _, _, err := parseSet(rawVals(map[string]string{"port2.fan": `"alpha-fan"`}), 2, fakeLib); err != nil {
			t.Errorf("port2.fan with 2 ports: %v, want accepted", err)
		}
	})
	t.Run("value must be a string", func(t *testing.T) {
		for _, v := range []string{`1`, `true`, `null`, `["alpha-fan"]`, `{}`, ``} {
			_, _, err := parseSet(rawVals(map[string]string{"port1.fan": v}), 4, fakeLib)
			if errCode(err) != "invalid_argument" {
				t.Errorf("port1.fan=%s: %v, want invalid_argument", v, err)
			}
		}
	})
	t.Run("a fan file that fails to load", func(t *testing.T) {
		lib := libFake{ids: []string{"alpha-fan"}, bad: map[string]error{"alpha-fan": errors.New("fans/alpha-fan.toml:3: bad")}}
		_, _, err := parseSet(rawVals(map[string]string{"port1.fan": `"alpha-fan"`}), 4, lib)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "fan_toml_invalid" || ae.Detail["fan"] != "alpha-fan" {
			t.Fatalf("err = %v, want fan_toml_invalid naming alpha-fan", err)
		}
	})
	t.Run("mixed env and bad fan returns an error and no change", func(t *testing.T) {
		c, applied, err := parseSet(rawVals(map[string]string{"temperature": "25", "port2.fan": `"nope"`}), 4, fakeLib)
		if errCode(err) != "unknown_fan" {
			t.Fatalf("err = %v, want unknown_fan", err)
		}
		if c.Temperature != nil || c.Humidity != nil || c.Fans != nil || applied != nil {
			t.Fatalf("change %+v and echo %v returned with an error, want nothing", c, applied)
		}
		// And the other way round: the fan is fine, the env value is not.
		c, applied, err = parseSet(rawVals(map[string]string{"temperature": "999", "port2.fan": `"alpha-fan"`}), 4, fakeLib)
		if errCode(err) != "value_out_of_range" || c.Fans != nil || applied != nil {
			t.Fatalf("err %v change %+v echo %v, want value_out_of_range and nothing", err, c, applied)
		}
	})
}

// fanSupervisor is buttonSupervisor with four held fans and a fake library.
func fanSupervisor(t *testing.T) *supervisor {
	t.Helper()
	s := buttonSupervisor(t)
	s.lib = fakeLib
	s.inMu.Lock()
	for n := 1; n <= 4; n++ {
		s.fans = append(s.fans, engine.PortConfig{Port: n, FanID: "alpha-fan", Fan: model.Params{Name: "fake alpha-fan", BelowMin: "stop", MinDuty: 0.05, StartDuty: 0.1, MinRPM: 100, MaxRPM: 1000, TauUp: 1, TauDown: 1}})
	}
	s.inMu.Unlock()
	// Restart the engine so it is built from the held fans.
	s.stopEngine(s.slot)
	s.inMu.Lock()
	sl, err := s.startEngine()
	if err != nil {
		s.inMu.Unlock()
		t.Fatal(err)
	}
	s.slot = sl
	s.inMu.Unlock()
	t.Cleanup(func() { s.stopEngine(sl) })
	return s
}

func portFans(s *supervisor) []string {
	var out []string
	for _, p := range s.currentEngine().Snapshot().Ports {
		out = append(out, p.Fan)
	}
	return out
}

// The held fan choice is the single source: Set records it, the engine in the slot shows it, and a
// replaced engine is built from it (the assumption-delta invariant, MODEL-03).
func TestSetFanSeedsTheNextEngine(t *testing.T) {
	s := fanSupervisor(t)
	ctx := context.Background()
	if got := fmt.Sprint(portFans(s)); got != "[alpha-fan alpha-fan alpha-fan alpha-fan]" {
		t.Fatalf("initial fans %s", got)
	}

	res, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"port3.fan": `"beta-fan"`, "temperature": "26"})})
	if err != nil || res.Applied["port3.fan"] != "beta-fan" || res.Applied["temperature"] != 26.0 {
		t.Fatalf("Set: %v, %v", res, err)
	}
	if got := fmt.Sprint(portFans(s)); got != "[alpha-fan alpha-fan beta-fan alpha-fan]" {
		t.Fatalf("fans after Set = %s, want port3 swapped", got)
	}
	if st, _ := s.Status(ctx); st.Ports[2].Fan != "beta-fan" || st.Ports[0].Fan != "alpha-fan" {
		t.Fatalf("Status.Ports = %+v, want beta-fan on port 3 only", st.Ports)
	}

	// A rejected set changes neither the held fans nor the engine.
	if _, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"port1.fan": `"beta-fan"`, "port2.fan": `"nope"`})}); errCode(err) != "unknown_fan" {
		t.Fatalf("Set with an unknown fan: %v", err)
	}
	if got := fmt.Sprint(portFans(s)); s.fans[0].FanID != "alpha-fan" || got != "[alpha-fan alpha-fan beta-fan alpha-fan]" {
		t.Fatalf("after a rejected Set: held %q, engine %s, want nothing changed", s.fans[0].FanID, got)
	}

	// A new engine (sim restart) is built with the chosen fans, also when the Set came while no engine ran.
	s.mu.Lock()
	s.engGap = true // what SimRestart holds between the old engine's stop and the new one's start
	s.mu.Unlock()
	s.stopEngine(s.slot)
	if _, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"port4.fan": `"beta-fan"`})}); err != nil {
		t.Fatalf("Set with the engine stopped: %v, want accepted", err)
	}
	s.inMu.Lock()
	sl, err := s.startEngine()
	s.inMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.stopEngine(sl) })
	var got []string
	for _, p := range sl.eng.Snapshot().Ports {
		got = append(got, p.Fan)
	}
	if fmt.Sprint(got) != "[alpha-fan alpha-fan beta-fan beta-fan]" {
		t.Fatalf("new engine fans = %v, want the held choice", got)
	}
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
	s.mu.Lock()
	s.engGap = true // what SimRestart holds between the old engine's stop and the new one's start
	s.mu.Unlock()
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

// Fans lists every library fan with its summary fields, in the library's (sorted) order, and names a
// file that fails to load.
func TestFansListsTheLibrary(t *testing.T) {
	s := &supervisor{lib: fakeLib}
	res, err := s.Fans(context.Background())
	if err != nil || len(res.Fans) != 2 {
		t.Fatalf("Fans = %+v, %v, want two entries", res, err)
	}
	if f := res.Fans[0]; f.ID != "alpha-fan" || f.Name != "fake alpha-fan" || f.MinRPM != 100 || f.MaxRPM != 1000 || f.BelowMin != "stop" {
		t.Fatalf("Fans[0] = %+v", f)
	}
	if res.Fans[1].ID != "beta-fan" {
		t.Fatalf("Fans[1] = %+v, want beta-fan after alpha-fan", res.Fans[1])
	}

	s.lib = libFake{ids: []string{"alpha-fan", "broken"}, bad: map[string]error{"broken": errors.New("fans/broken.toml:2: bad")}}
	_, err = s.Fans(context.Background())
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "fan_toml_invalid" || ae.Detail["fan"] != "broken" {
		t.Fatalf("Fans with a broken file: %v, want fan_toml_invalid naming it", err)
	}
}

// WR-02: a Set whose caller is gone fails before anything is recorded, so the record and the
// engine never disagree about a request that was reported as failed.
func TestSetWithACancelledCallerRecordsNothing(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	engineBefore := s.currentEngine().Snapshot().Env
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 20; i++ {
		_, err := s.Set(ctx, control.SetRequest{Values: rawVals(map[string]string{"temperature": "31", "port2.fan": `"beta-fan"`})})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Set with a cancelled caller: %v, want context.Canceled", err)
		}
	}
	if s.inputs.Temperature != DefaultTemperature || s.fans[1].FanID != "alpha-fan" {
		t.Fatalf("record changed by a failed Set: inputs %+v, port2 fan %q", s.inputs, s.fans[1].FanID)
	}
	if e := s.currentEngine().Snapshot().Env; e != engineBefore {
		t.Fatalf("engine changed by a failed Set: %+v, was %+v", e, engineBefore)
	}
}

// WR-02: an engine that failed for good never takes a value, so Set reports engine_failed and keeps
// the record equal to what is running. A restart gap (no failure) still accepts the value.
func TestSetOnAFailedEngineFailsAndRollsBack(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	s.stopEngine(s.slot)
	s.mu.Lock()
	s.failure = apperr.Infra("engine_failed", "the simulator engine did not start", nil)
	s.mu.Unlock()

	_, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31", "port1.fan": `"beta-fan"`})})
	if errCode(err) != "engine_failed" {
		t.Fatalf("Set on a failed engine: %v, want engine_failed", err)
	}
	if s.inputs.Temperature != DefaultTemperature || s.fans[0].FanID != "alpha-fan" {
		t.Fatalf("record kept a value the engine never took: inputs %+v, port1 fan %q", s.inputs, s.fans[0].FanID)
	}
}

// F1 (03.1 re-review): Set reads len(s.fans) outside inMu while a rolling-back Set assigned the
// slice header under it, a data race the Phase 2 reviews missed. On a dead engine with no gap every
// Set rolls back, so two concurrent Sets exercise exactly that pair. Run under -race.
func TestSetRollbackDoesNotRaceSet(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	s.stopEngine(s.slot)

	const callers, calls = 2, 200
	bad := make(chan string, callers*calls)
	var wg sync.WaitGroup
	for g := 0; g < callers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				_, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31"})})
				if code := errCode(err); code != "engine_failed" {
					bad <- fmt.Sprintf("Set on a dead engine answered %q (%v), want engine_failed", code, err)
				}
			}
		}()
	}
	wg.Wait()
	close(bad)
	for msg := range bad {
		t.Error(msg)
	}
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if s.inputs.Temperature != DefaultTemperature {
		t.Fatalf("record kept a value the engine never took: inputs %+v", s.inputs)
	}
	for i, pc := range s.fans {
		if pc.FanID != "alpha-fan" {
			t.Fatalf("port %d fan %q after rollbacks, want alpha-fan", i+1, pc.FanID)
		}
	}
}

// raceWait waits up to 5 s of real time for ch to deliver and reports whether it did.
func raceWait[T any](ch <-chan T) (v T, ok bool) {
	tm := realClock.NewTimer(5 * time.Second)
	defer tm.Stop()
	select {
	case v = <-ch:
		return v, true
	case <-tm.C():
		return v, false
	}
}

// F2 (03.1 re-review, audit IN-09): shutdown() closes downReq before it can take opMu, and stopAll sets
// s.stopping only after that. In between, a SimRestart whose gap sleep is aborted has cleared engGap
// while the slot still holds the stopped engine, so a Set used to find "no gap, not stopping" and
// answer engine_failed (exit class 2) for an instance that is shutting down. It must answer
// stopping and leave the record as it was.
func TestSetInTheShutdownWindowIsStopping(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	old := s.slot

	restarted := make(chan error, 1)
	go func() {
		_, err := s.SimRestart(context.Background(), control.SimRestartRequest{GapS: 5})
		restarted <- err
	}()
	// The old engine is stopped, then the gap sleep is registered on the manual clock.
	if _, ok := raceWait(old.done); !ok {
		t.Fatal("timed out waiting for SimRestart to stop the old engine")
	}
	s.clk.(*clock.Manual).BlockUntil(1)

	// The first step of shutdown(): the request is made, stopAll has not run yet (s.stopping is false).
	s.downOnce.Do(func() { close(s.downReq) })
	err, ok := raceWait(restarted)
	if !ok || errCode(err) != "stopping" {
		t.Fatalf("SimRestart after the shutdown request = %v (returned %v), want stopping", err, ok)
	}

	_, err = s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31"})})
	if errCode(err) != "stopping" {
		t.Fatalf("Set in the shutdown window: %v, want stopping", err)
	}
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if s.inputs.Temperature != DefaultTemperature {
		t.Fatalf("record kept a value of a Set that answered stopping: %+v", s.inputs)
	}
}

// raceIdleSlot is a slot whose engine is built the way startEngine builds it but never runs: nothing
// reads its inbox, so an Apply sent to it waits for a reply that never comes until its context ends.
// Its cancel does nothing and its done is already closed, so a stopEngine on it returns at once.
func raceIdleSlot(s *supervisor) *engineSlot {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	cfg := s.engCfg
	cfg.Env = s.inputs
	cfg.Ports = append([]engine.PortConfig(nil), s.fans...)
	done := make(chan struct{})
	close(done)
	return &engineSlot{eng: engine.New(cfg), cancel: func() {}, done: done}
}

// F6 (03.1 re-review, user decision 2026-11-10): a Set whose Apply is cancelled by a shutdown
// (the actx.Err() arm) answers stopping. An error return must leave the record as it was, so
// the change recorded before Apply has to be rolled back. The engine in the slot never answers, so
// the cancellation is the only way out, whatever the scheduling.
func TestSetCancelledByShutdownRollsBack(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	idle := raceIdleSlot(s)
	s.mu.Lock()
	s.slot = idle
	s.mu.Unlock()

	// The first step of shutdown(); s.stopping stays false, so Set reaches Apply.
	s.downOnce.Do(func() { close(s.downReq) })

	answered := make(chan error, 1)
	go func() {
		_, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31", "port1.fan": `"beta-fan"`})})
		answered <- err
	}()
	err, ok := raceWait(answered)
	if !ok {
		t.Fatal("Set did not return: a shutdown must cancel its Apply")
	}
	if errCode(err) != "stopping" {
		t.Fatalf("Set cancelled by a shutdown: %v, want stopping", err)
	}
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if s.inputs.Temperature != DefaultTemperature || s.fans[0].FanID != "alpha-fan" {
		t.Fatalf("record kept a change of a Set that answered stopping: temperature %v, port1 fan %q, want %v and alpha-fan",
			s.inputs.Temperature, s.fans[0].FanID, DefaultTemperature)
	}
}

// The record/engine rule under concurrency: four goroutines Set while one restarts the engine over
// and over; afterwards the record (s.inputs, s.fans) equals the engine in the slot. Every Set must
// succeed: there is no firmware, so SimRestart answers firmware_not_running, but it always installs
// a new engine first. A Set that meets a stopped engine during a restart reads engGap true, because
// SimRestart needs inMu, which the Set holds, to seed and install the next engine, and it clears
// engGap in the same section that installs it. So a gap Set is recorded, seeds the next engine,
// and is never mistaken for a dead engine. Run under -race.
func TestSetAndSimRestartKeepRecordAndEngineEqual(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	// Every SimRestart installs a new engine, so stop the one that is current at the end.
	t.Cleanup(func() {
		s.mu.Lock()
		sl := s.slot
		s.mu.Unlock()
		s.stopEngine(sl)
	})
	const setters, sets, restarts = 4, 50, 20

	bad := make(chan string, setters*sets+restarts)
	var wg sync.WaitGroup
	for g := 0; g < setters; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < sets; i++ {
				vals := map[string]string{"temperature": fmt.Sprint(20 + (g*sets+i)%20)}
				if i%3 == 0 {
					vals["humidity"] = fmt.Sprint(30 + (g*sets+i)%30)
				}
				fan := "alpha-fan"
				if i%2 == 0 {
					fan = "beta-fan"
				}
				vals[fmt.Sprintf("port%d.fan", g+1)] = `"` + fan + `"`
				if _, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(vals)}); err != nil {
					bad <- fmt.Sprintf("Set %d/%d answered %v, want nil", g, i, err)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < restarts; i++ {
			if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); errCode(err) != "firmware_not_running" {
				bad <- fmt.Sprintf("SimRestart %d answered %v, want firmware_not_running", i, err)
			}
		}
	}()
	wg.Wait()
	close(bad)
	for msg := range bad {
		t.Error(msg)
	}

	s.inMu.Lock()
	defer s.inMu.Unlock()
	snap := s.currentEngine().Snapshot()
	if snap.Env != s.inputs {
		t.Fatalf("engine env %+v, record %+v: they must be equal after quiescence", snap.Env, s.inputs)
	}
	if len(snap.Ports) != len(s.fans) {
		t.Fatalf("engine has %d ports, record %d", len(snap.Ports), len(s.fans))
	}
	for i := range s.fans {
		if snap.Ports[i].Fan != s.fans[i].FanID {
			t.Errorf("port %d: engine fan %q, record %q", i+1, snap.Ports[i].Fan, s.fans[i].FanID)
		}
	}
}

// WR-02 (second review): a SimRestart that cannot start the new engine while another failure is held
// records no engine_failed (the held diagnosis wins), yet the slot keeps a stopped engine. Set must
// decide on the real state, not on the failure code: it reports engine_failed and keeps the record
// equal to what is running.
func TestSetAfterAFailedRestartWithAHeldFailureFails(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	s.mu.Lock()
	s.failure = apperr.Failed("firmware_crash_loop", "the firmware keeps crashing", nil)
	s.mu.Unlock()

	s.engCfg.BusPath = filepath.Join(t.TempDir(), "missing", "bus.sock")
	if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); errCode(err) != "engine_failed" {
		t.Fatalf("failing SimRestart = %v, want engine_failed", err)
	}
	s.mu.Lock()
	code, gap := s.failure.Code, s.engGap
	s.mu.Unlock()
	if code != "firmware_crash_loop" || gap {
		t.Fatalf("after the failed restart: failure %q, engGap %v, want the held failure and no gap", code, gap)
	}

	_, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31", "port1.fan": `"beta-fan"`})})
	if errCode(err) != "engine_failed" {
		t.Fatalf("Set on a dead engine with another failure held: %v, want engine_failed", err)
	}
	if s.inputs.Temperature != DefaultTemperature || s.fans[0].FanID != "alpha-fan" {
		t.Fatalf("record kept a value no engine took: inputs %+v, port1 fan %q", s.inputs, s.fans[0].FanID)
	}
}

// While a SimRestart is replacing the engine (engGap) a stopped engine is a gap, not a death: the Set
// stands and seeds the next engine.
func TestSetInARestartGapIsKept(t *testing.T) {
	s := fanSupervisor(t)
	s.inputs = engine.Env{Temperature: DefaultTemperature, Humidity: DefaultHumidity}
	s.mu.Lock()
	s.engGap = true
	s.mu.Unlock()
	s.stopEngine(s.slot)

	res, err := s.Set(context.Background(), control.SetRequest{Values: rawVals(map[string]string{"temperature": "31"})})
	if err != nil || len(res.Applied) != 1 {
		t.Fatalf("Set in a restart gap = %+v, %v, want applied", res, err)
	}
	if s.inputs.Temperature != 31 {
		t.Fatalf("gap Set not recorded: %+v", s.inputs)
	}
}

// Status shows the seed spec.json carried, which Serve also put into engCfg (D-15); it is always
// present in the JSON.
func TestStatusShowsSeed(t *testing.T) {
	s := fanSupervisor(t)
	s.spec.Seed = 42
	s.engCfg.Seed = 42
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Sim.Seed != 42 {
		t.Fatalf("Status.Sim.Seed = %d, want 42", st.Sim.Seed)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Sim map[string]any `json:"sim"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got, ok := back.Sim["seed"].(float64); !ok || got != 42 {
		t.Fatalf("status JSON sim.seed = %v, want 42 (sim = %v)", back.Sim["seed"], back.Sim)
	}
	if st.Schema != control.SchemaVersion || control.SchemaVersion != 1 {
		t.Fatalf("status schema %d (const %d), want 1", st.Schema, control.SchemaVersion)
	}
}

// A simulator restart builds the new engine from the same engCfg: the seed stays in the config and in
// status (D-15), and the slot holds a new engine.
func TestSeedSurvivesSimRestart(t *testing.T) {
	s := fanSupervisor(t)
	s.spec.Seed = 42
	s.engCfg.Seed = 42
	before := s.currentEngine()

	// No firmware runs in this fixture, so the resync step ends with firmware_not_running; by then the
	// new engine is in the slot.
	if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); errCode(err) != "firmware_not_running" {
		t.Fatalf("SimRestart = %v, want firmware_not_running", err)
	}
	t.Cleanup(func() { s.stopEngine(s.slot) })
	if s.currentEngine() == before {
		t.Fatal("the slot still holds the old engine")
	}
	if s.engCfg.Seed != 42 {
		t.Fatalf("engCfg.Seed after SimRestart = %d, want 42", s.engCfg.Seed)
	}
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Sim.Seed != 42 || st.Sim.EngineRestarts != 1 {
		t.Fatalf("Status.Sim = %+v, want seed 42 after one engine restart", st.Sim)
	}
}

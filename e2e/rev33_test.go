//go:build sil_e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
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

// r33Sleep waits d on the real clock.
func r33Sleep(d time.Duration) { _ = sysClock.Sleep(context.Background(), d) }

// r33Async runs sil in a goroutine and hands back its decoded stdout and exit code.
type r33Result struct {
	out  map[string]any
	exit int
	err  error
}

func (in *r33Inst) async(args ...string) <-chan r33Result {
	ch := make(chan r33Result, 1)
	go func() {
		so, exit, err := isoRun(in.env, args...)
		r := r33Result{exit: exit, err: err}
		if err == nil {
			if jerr := json.Unmarshal([]byte(so), &r.out); jerr != nil {
				r.err = fmt.Errorf("stdout is not a JSON object: %v: %q", jerr, so)
			}
		}
		ch <- r
	}()
	return ch
}

// r33Wait waits for an async sil call.
func r33Wait(t *testing.T, what string, ch <-chan r33Result) r33Result {
	t.Helper()
	tm := sysClock.NewTimer(2 * time.Minute)
	defer tm.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("%s: %v", what, r.err)
		}
		return r
	case <-tm.C():
		t.Fatalf("%s: did not finish", what)
		return r33Result{}
	}
}

// mustPress runs sil press and requires exit 0.
func (in *r33Inst) mustPress(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out, code := in.press(t, args...)
	if code != 0 {
		t.Fatalf("sil press %v: exit %d: %v", args, code, out)
	}
	return out
}

// expectErr requires exit 1 with the given error code and returns the result.
func r33ExpectErr(t *testing.T, what string, out map[string]any, code int, want string) map[string]any {
	t.Helper()
	if code != 1 || r33ErrCode(out) != want {
		t.Fatalf("%s: exit %d, %v, want exit 1 %s", what, code, out, want)
	}
	return out
}

// TestPressVocabulary drives every press form of D-01..D-04 against the real
// firmware on one Rev 3.3 instance; the subtests run in order and build on the
// duties the previous one left behind.
func TestPressVocabulary(t *testing.T) {
	in := r33Up(t, "e2e-r33-voc")

	// Fan 1 at 40 %, then one USR1 press: Fan 1 50 %, Fans 2-4 10 %.
	in.setFan(t, 1, 40)
	in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
	in.mustPress(t, "usr1")
	in.waitDuties(t, [4]float64{0.5, 0.1, 0.1, 0.1})

	t.Run("usr3_lowers", func(t *testing.T) {
		in.mustPress(t, "usr3")
		in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
	})
	t.Run("usr1_long_is_noop", func(t *testing.T) {
		in.mustPress(t, "usr1", "--long")
		r33Sleep(1500 * time.Millisecond)
		if st := in.env.Status(t, in.name); !r33Duties(st, [4]float64{0.4, 0, 0, 0}) {
			t.Fatalf("a long USR1 press changed the duties: %v", jget(st, "ports"))
		}
	})
	t.Run("usr2_short_is_noop", func(t *testing.T) {
		in.mustPress(t, "usr2")
		r33Sleep(500 * time.Millisecond)
		if st := in.env.Status(t, in.name); !r33Duties(st, [4]float64{0.4, 0, 0, 0}) {
			t.Fatalf("a short USR2 press changed the duties: %v", jget(st, "ports"))
		}
	})
	t.Run("usr2_long_full", func(t *testing.T) {
		out := in.mustPress(t, "usr2", "--long")
		down, _ := out["t_down_us"].(float64)
		up, _ := out["t_up_us"].(float64)
		if jget(out, "hold_ms") != float64(1500) || up-down < 1500000 {
			t.Fatalf("long press result %v, want hold_ms 1500 and t_up_us - t_down_us >= 1500000", out)
		}
		in.waitDuties(t, [4]float64{1, 1, 1, 1})
	})
	t.Run("status_during_long_press", func(t *testing.T) {
		done := in.async("press", "usr2", "--long", "-i", in.name)
		sawHeld, polls := false, 0
	loop:
		for {
			select {
			case r := <-done:
				if r.err != nil || r.exit != 0 {
					t.Fatalf("press during the status polls: exit %d, err %v, %v", r.exit, r.err, r.out)
				}
				break loop
			default:
			}
			st := in.env.Status(t, in.name) // fails the test on a non-zero exit or invalid JSON
			polls++
			if level, _ := r33Buttons(st, "usr2"); level {
				sawHeld = true
			}
			r33Sleep(200 * time.Millisecond)
		}
		if !sawHeld {
			t.Errorf("none of %d status calls during the 1.5 s press showed buttons.usr2 true", polls)
		}
	})
	t.Run("back_to_back", func(t *testing.T) {
		in.mustPress(t, "usr3")
		in.mustPress(t, "usr3") // started as soon as the first returned
		in.waitDuties(t, [4]float64{0.8, 0.8, 0.8, 0.8})
	})
	t.Run("down_up_and_busy", func(t *testing.T) {
		type watchRes struct {
			out  map[string]any
			code int
		}
		watch := make(chan watchRes, 1)
		go func() {
			out, code := runAPIClient(t, in.port, in.key, "watch", "--seconds", "14")
			watch <- watchRes{out, code}
		}()
		r33Sleep(4 * time.Second)

		out := in.mustPress(t, "usr1", "--down")
		downAt := sysClock.Now()
		if out["action"] != "down" || out["level"] != true || out["button"] != "usr1" {
			t.Fatalf("--down result %v", out)
		}
		st := in.env.Status(t, in.name)
		if level, _ := r33Buttons(st, "usr1"); !level {
			t.Fatalf("buttons.usr1 is not true after --down: %v", st["buttons"])
		}
		o, c := in.press(t, "usr1")
		if d, _ := r33ExpectErr(t, "press on a held button", o, c, "button_busy")["error"].(map[string]any)["detail"].(map[string]any); d["button"] != "usr1" {
			t.Fatalf("button_busy detail %v, want button usr1", d)
		}
		o, c = in.press(t, "usr1", "--down")
		r33ExpectErr(t, "--down on a held button", o, c, "button_busy")

		// Hold past the 1000 ms click window so the release is not a click.
		for sysClock.Now()-downAt < 1200*time.Millisecond {
			r33Sleep(50 * time.Millisecond)
		}
		out = in.mustPress(t, "usr1", "--up")
		if out["action"] != "up" || out["level"] != false {
			t.Fatalf("--up result %v", out)
		}
		o, c = in.press(t, "usr1", "--up")
		r33ExpectErr(t, "--up on a released button", o, c, "button_not_held")
		if st := in.env.Status(t, in.name); !r33Duties(st, [4]float64{0.8, 0.8, 0.8, 0.8}) {
			t.Fatalf("duties changed during the busy checks: %v", jget(st, "ports"))
		}

		w := <-watch
		if w.code != 0 {
			t.Fatalf("watch: exit %d: %v", w.code, w.out)
		}
		series, _ := jget(w.out, "series", "Button USR1").([]any)
		sawTrue, falseAfter := false, false
		for _, p := range series {
			v := jget(p, 1)
			if v == true {
				sawTrue = true
			}
			if v == false && sawTrue {
				falseAfter = true
			}
		}
		if !sawTrue || !falseAfter {
			t.Fatalf("Button USR1 series %v, want true and later false", series)
		}
	})
	t.Run("hold_bounds", func(t *testing.T) {
		for _, hold := range []string{"61s", "60001ms", "0s"} {
			o, c := in.press(t, "usr1", "--hold", hold)
			r33ExpectErr(t, "--hold "+hold, o, c, "invalid_argument")
		}
	})
	t.Run("unknown_button", func(t *testing.T) {
		o, c := in.press(t, "usr4")
		r33ExpectErr(t, "press usr4", o, c, "invalid_argument")
		known, _ := jget(o, "error", "detail", "known").([]any)
		if fmt.Sprint(known) != "[usr1 usr2 usr3]" {
			t.Fatalf("detail.known = %v, want [usr1 usr2 usr3]", known)
		}
	})
	t.Run("bus_down", func(t *testing.T) {
		restart := in.async("sim", "restart", "--gap", "3s", in.name)
		in.env.WaitStatus(t, in.name, 10*time.Second, func(st map[string]any) bool {
			return jget(st, "firmware", "bus_connected") == false
		})
		o, c := in.press(t, "usr1")
		r33ExpectErr(t, "press while the bus is down", o, c, "firmware_not_connected")
		if r := r33Wait(t, "sim restart", restart); r.exit != 0 {
			t.Fatalf("sim restart: exit %d, %v", r.exit, r.out)
		}
		in.env.WaitStatus(t, in.name, 20*time.Second, func(st map[string]any) bool {
			return st["phase"] == "ready" && r33Duties(st, [4]float64{0.8, 0.8, 0.8, 0.8})
		})
		// The first press after the resync counts like any other.
		in.mustPress(t, "usr3")
		in.waitDuties(t, [4]float64{0.7, 0.7, 0.7, 0.7})
	})
	t.Run("first_press_after_fw_restart", func(t *testing.T) {
		if out, code, stderr := in.env.Sil(t, "fw", "restart", in.name); code != 0 {
			t.Fatalf("fw restart: exit %d: %v\nstderr: %s", code, out, stderr)
		}
		in.env.WaitStatus(t, in.name, 20*time.Second, func(st map[string]any) bool {
			return st["phase"] == "ready" && jget(st, "firmware", "bus_connected") == true && r33Duties(st, [4]float64{0, 0, 0, 0})
		})
		in.mustPress(t, "usr1")
		in.waitDuties(t, [4]float64{0.1, 0.1, 0.1, 0.1})
	})
	t.Run("input_not_available", func(t *testing.T) {
		// The Phase 1 skeleton firmware has no buttons.
		env := newEnv(t)
		env.Up(t, "e2e-r33-skel")
		out, code, _ := env.Sil(t, "press", "usr1", "-i", "e2e-r33-skel")
		r33ExpectErr(t, "press on firmware without buttons", out, code, "input_not_available")
	})
}

// r33Env is env.<key> of a status object.
func r33Env(st map[string]any, key string) (float64, bool) {
	v, ok := jget(st, "env", key).(float64)
	return v, ok
}

// r33EnvIs reports whether the status env equals temperature and humidity (within 1e-3).
func r33EnvIs(st map[string]any, temperature, humidity float64) bool {
	tv, ok1 := r33Env(st, "temperature")
	hv, ok2 := r33Env(st, "humidity")
	return ok1 && ok2 && math.Abs(tv-temperature) < 1e-3 && math.Abs(hv-humidity) < 1e-3
}

// fwState is the state of one entity as the firmware's native API reports it right now.
func (in *r33Inst) fwState(t *testing.T, name string) (float64, bool) {
	t.Helper()
	info, code := in.env.APIClient(t, in.port, in.key, "info")
	if code != 0 {
		t.Fatalf("api info: exit %d: %v", code, info)
	}
	v, ok := jget(info, "states", name).(float64)
	return v, ok
}

// waitFwEnv waits until the firmware's Temperature and Humidity entities report the wanted values
// (within 0.05), asking through the native API like Home Assistant would.
func (in *r33Inst) waitFwEnv(t *testing.T, within time.Duration, temperature, humidity float64) {
	t.Helper()
	deadline := sysClock.Now() + within
	var gt, gh float64
	var okT, okH bool
	for {
		gt, okT = in.fwState(t, "Temperature")
		gh, okH = in.fwState(t, "Humidity")
		if okT && okH && math.Abs(gt-temperature) <= 0.05 && math.Abs(gh-humidity) <= 0.05 {
			return
		}
		if sysClock.Now() > deadline {
			t.Fatalf("firmware entities: Temperature %v (present %v), Humidity %v (present %v), want %v and %v",
				gt, okT, gh, okH, temperature, humidity)
		}
		r33Sleep(300 * time.Millisecond)
	}
}

// set runs `sil set pairs... -i name`.
func (in *r33Inst) set(t *testing.T, pairs ...string) (map[string]any, int) {
	t.Helper()
	out, code, _ := in.env.Sil(t, append(append([]string{"set"}, pairs...), "-i", in.name)...)
	return out, code
}

// seriesOnly reports whether every value of a watch series is one of want (within 0.05) and the
// last one is want[len-1]; the series must contain every wanted value.
func seriesOnly(series []any, want ...float64) (bool, string) {
	seen := make([]bool, len(want))
	var last float64
	for _, p := range series {
		v, ok := jget(p, 1).(float64)
		if !ok {
			return false, fmt.Sprintf("non-numeric point %v", p)
		}
		match := false
		for i, w := range want {
			if math.Abs(v-w) <= 0.05 {
				match, seen[i] = true, true
			}
		}
		if !match {
			return false, fmt.Sprintf("value %v is none of %v", v, want)
		}
		last = v
	}
	for i, ok := range seen {
		if !ok {
			return false, fmt.Sprintf("value %v never appeared", want[i])
		}
	}
	if math.Abs(last-want[len(want)-1]) > 0.05 {
		return false, fmt.Sprintf("series ends at %v, want %v", last, want[len(want)-1])
	}
	return true, ""
}

// TestEnvSet is the environment slice end to end: `sil set` reaches the firmware's Temperature and
// Humidity entities exactly (never a mean of old and new) and `sil status` shows it.
func TestEnvSet(t *testing.T) {
	in := r33Up(t, "e2e-env")

	st := in.env.Status(t, in.name)
	if !r33EnvIs(st, 22, 45) {
		t.Fatalf("fresh status env = %v, want temperature 22 and humidity 45", st["env"])
	}
	in.waitFwEnv(t, 5*time.Second, 22, 45)

	type watchRes struct {
		out  map[string]any
		code int
	}
	watch := make(chan watchRes, 1)
	go func() {
		out, code := runAPIClient(t, in.port, in.key, "watch", "--seconds", "14")
		watch <- watchRes{out, code}
	}()
	r33Sleep(6 * time.Second) // the client container is up and subscribed

	out, code := in.set(t, "temperature=24.5", "humidity=40")
	if code != 0 {
		t.Fatalf("sil set: exit %d: %v", code, out)
	}
	applied, _ := out["applied"].(map[string]any)
	if len(out) != 1 || len(applied) != 2 || applied["temperature"] != 24.5 || applied["humidity"] != float64(40) {
		t.Fatalf("sil set result %v, want exactly {applied: {humidity: 40, temperature: 24.5}}", out)
	}
	in.env.WaitStatus(t, in.name, 2*time.Second, func(st map[string]any) bool { return r33EnvIs(st, 24.5, 40) })
	in.waitFwEnv(t, 2*time.Second, 24.5, 40)

	w := <-watch
	if w.code != 0 {
		t.Fatalf("watch: exit %d: %v", w.code, w.out)
	}
	if ok, why := seriesOnly(jgetSlice(w.out, "series", "Temperature"), 22, 24.5); !ok {
		t.Errorf("Temperature series %v: %s", jget(w.out, "series", "Temperature"), why)
	}
	if ok, why := seriesOnly(jgetSlice(w.out, "series", "Humidity"), 45, 40); !ok {
		t.Errorf("Humidity series %v: %s", jget(w.out, "series", "Humidity"), why)
	}
}

// jgetSlice is jget as a slice.
func jgetSlice(v any, path ...any) []any {
	s, _ := jget(v, path...).([]any)
	return s
}

// r33ErrDetail is error.detail of a failed result.
func r33ErrDetail(out map[string]any) map[string]any {
	d, _ := jget(out, "error", "detail").(map[string]any)
	return d
}

// TestSetVocabulary drives every shape of `sil set` of D-09 against one Rev 3.3 instance: strict,
// atomic, a named error for each misuse, and the values survive both kinds of restart. The
// subtests run in order and build on the state the previous one left.
func TestSetVocabulary(t *testing.T) {
	in := r33Up(t, "e2e-setv")
	unchanged := func(t *testing.T, temperature, humidity float64) {
		t.Helper()
		if st := in.env.Status(t, in.name); !r33EnvIs(st, temperature, humidity) {
			t.Fatalf("status env = %v, want %v and %v (a failed set changes nothing)", st["env"], temperature, humidity)
		}
	}

	t.Run("atomic_reject", func(t *testing.T) {
		out, code := in.set(t, "temperature=30", "humidity=101")
		r33ExpectErr(t, "set temperature=30 humidity=101", out, code, "value_out_of_range")
		d := r33ErrDetail(out)
		if d["key"] != "humidity" || d["min"] != float64(0) || d["max"] != float64(100) {
			t.Fatalf("detail %v, want key humidity, min 0, max 100", d)
		}
		unchanged(t, 22, 45)
	})
	t.Run("unknown_key", func(t *testing.T) {
		out, code := in.set(t, "fanspeed=3")
		r33ExpectErr(t, "set fanspeed=3", out, code, "unknown_key")
		known := fmt.Sprint(jgetSlice(out, "error", "detail", "known"))
		if !strings.Contains(known, "temperature") || !strings.Contains(known, "humidity") {
			t.Fatalf("detail.known = %s, want temperature and humidity", known)
		}
		unchanged(t, 22, 45)
	})
	t.Run("not_a_number", func(t *testing.T) {
		for _, v := range []string{"warm", "NaN", "Inf", "-Inf", "0x10", "1e999"} {
			out, code := in.set(t, "temperature="+v)
			r33ExpectErr(t, "set temperature="+v, out, code, "invalid_argument")
		}
		unchanged(t, 22, 45)
	})
	t.Run("bounds", func(t *testing.T) {
		for _, tc := range [][2]string{{"temperature", "-40"}, {"temperature", "125"}, {"humidity", "0"}, {"humidity", "100"}} {
			out, code := in.set(t, tc[0]+"="+tc[1])
			if code != 0 {
				t.Fatalf("set %s=%s: exit %d: %v, want accepted", tc[0], tc[1], code, out)
			}
		}
		unchanged(t, 125, 100)
		for _, tc := range [][2]string{{"temperature", "-40.1"}, {"temperature", "125.1"}, {"humidity", "-0.1"}, {"humidity", "100.1"}} {
			out, code := in.set(t, tc[0]+"="+tc[1])
			r33ExpectErr(t, "set "+tc[0]+"="+tc[1], out, code, "value_out_of_range")
			if r33ErrDetail(out)["key"] != tc[0] {
				t.Fatalf("detail %v, want key %s", r33ErrDetail(out), tc[0])
			}
		}
		unchanged(t, 125, 100)
		if out, code := in.set(t, "temperature=22", "humidity=45"); code != 0 {
			t.Fatalf("restoring the defaults: exit %d: %v", code, out)
		}
	})
	t.Run("empty_and_duplicate", func(t *testing.T) {
		for _, pairs := range [][]string{nil, {"temperature="}, {"temperature"}, {"=5"}, {"temperature=1", "temperature=2"}} {
			out, code := in.set(t, pairs...)
			r33ExpectErr(t, fmt.Sprintf("sil set %q", pairs), out, code, "invalid_argument")
		}
		unchanged(t, 22, 45)
	})
	t.Run("argument_order", func(t *testing.T) {
		a, ca := in.set(t, "humidity=41", "temperature=23")
		b, cb := in.set(t, "temperature=23", "humidity=41")
		if ca != 0 || cb != 0 || fmt.Sprint(a) != fmt.Sprint(b) || fmt.Sprint(a) != "map[applied:map[humidity:41 temperature:23]]" {
			t.Fatalf("results %v (exit %d) and %v (exit %d), want the same applied map for both orders", a, ca, b, cb)
		}
		so, _, _ := in.env.run(t, "set", "temperature=23", "humidity=41", "-i", in.name)
		if !strings.HasPrefix(so, `{"applied":{"humidity":41,"temperature":23}}`) {
			t.Fatalf("stdout %q, want applied keys sorted", so)
		}
		if out, code := in.set(t, "temperature=22", "humidity=45"); code != 0 {
			t.Fatalf("restoring the defaults: exit %d: %v", code, out)
		}
	})
	t.Run("survives_sim_restart", func(t *testing.T) {
		if out, code := in.set(t, "temperature=27.5"); code != 0 {
			t.Fatalf("set: exit %d: %v", code, out)
		}
		if out, code, stderr := in.env.Sil(t, "sim", "restart", in.name); code != 0 {
			t.Fatalf("sim restart: exit %d: %v\nstderr: %s", code, out, stderr)
		}
		in.env.WaitStatus(t, in.name, 3*time.Second, func(st map[string]any) bool { return r33EnvIs(st, 27.5, 45) })
		in.waitFwEnv(t, 3*time.Second, 27.5, 45)
	})
	t.Run("survives_fw_restart", func(t *testing.T) {
		if out, code, stderr := in.env.Sil(t, "fw", "restart", in.name); code != 0 {
			t.Fatalf("fw restart: exit %d: %v\nstderr: %s", code, out, stderr)
		}
		in.env.WaitStatus(t, in.name, 3*time.Second, func(st map[string]any) bool {
			return st["phase"] == "ready" && r33EnvIs(st, 27.5, 45)
		})
		in.waitFwEnv(t, 3*time.Second, 27.5, 45)
	})
	t.Run("bus_down_accepted", func(t *testing.T) {
		restart := in.async("sim", "restart", "--gap", "3s", in.name)
		in.env.WaitStatus(t, in.name, 10*time.Second, func(st map[string]any) bool {
			return jget(st, "firmware", "bus_connected") == false
		})
		if out, code := in.set(t, "humidity=55"); code != 0 {
			t.Fatalf("set while the bus is down: exit %d: %v, want accepted", code, out)
		}
		if r := r33Wait(t, "sim restart", restart); r.exit != 0 {
			t.Fatalf("sim restart: exit %d, %v", r.exit, r.out)
		}
		in.env.WaitStatus(t, in.name, 20*time.Second, func(st map[string]any) bool {
			return st["phase"] == "ready" && r33EnvIs(st, 27.5, 55)
		})
		in.waitFwEnv(t, 5*time.Second, 27.5, 55)
	})
}

// r33Contract is the entity set of the product's HIL contract (FW-02), exactly.
var r33Contract = []string{
	"Button USR1", "Button USR2", "Button USR3",
	"Fan 1", "Fan 1 Speed", "Fan 2", "Fan 2 Speed", "Fan 3", "Fan 3 Speed", "Fan 4", "Fan 4 Speed",
	"Humidity", "Temperature",
}

// TestRev33Entities pins what Home Assistant sees of the Rev 3.3 example: exactly the 13 entities
// of the HIL contract, no light and nothing else, and `sil status` with every field of CLI-01.
func TestRev33Entities(t *testing.T) {
	const name = "e2e-ents"
	in := r33Up(t, name)

	info, code := in.env.APIClient(t, in.port, in.key, "info")
	if code != 0 {
		t.Fatalf("api info: exit %d: %v", code, info)
	}
	var got []string
	for _, e := range jgetSlice(info, "entities") {
		s, _ := e.(string)
		got = append(got, s)
	}
	if !slices.Equal(got, r33Contract) { // got is sorted by the client; r33Contract is in sorted order
		t.Fatalf("native API entities = %q, want exactly %q", got, r33Contract)
	}
	if info["device_name"] != "sil-"+name {
		t.Errorf("device_name = %v, want sil-%s", info["device_name"], name)
	}

	st := in.env.WaitStatus(t, name, 10*time.Second, func(st map[string]any) bool {
		t_, _ := jget(st, "sim", "time_s").(float64)
		return st["phase"] == "ready" && t_ > 0
	})
	if st["instance"] != name {
		t.Errorf("instance = %v, want %s", st["instance"], name)
	}
	if host, _ := jget(st, "api", "host").(string); host == "" {
		t.Errorf("api.host = %v, want a host", jget(st, "api", "host"))
	}
	if p, _ := jget(st, "api", "port").(float64); int(p) != in.port {
		t.Errorf("api.port = %v, want %d", jget(st, "api", "port"), in.port)
	}
	if k, _ := jget(st, "api", "key").(string); k != in.key {
		t.Errorf("api.key = %v, want the key of sil up", jget(st, "api", "key"))
	}
	if _, ok := jget(st, "firmware", "pid").(float64); !ok {
		t.Errorf("firmware.pid = %v, want a number", jget(st, "firmware", "pid"))
	}
	if jget(st, "firmware", "bus_connected") != true {
		t.Errorf("firmware.bus_connected = %v, want true", jget(st, "firmware", "bus_connected"))
	}
	ports, _ := st["ports"].([]any)
	if len(ports) != 4 {
		t.Fatalf("ports = %v, want four", st["ports"])
	}
	for i, p := range ports {
		m, _ := p.(map[string]any)
		for _, f := range []string{"port", "fan", "duty", "rpm", "state"} {
			if _, ok := m[f]; !ok {
				t.Errorf("ports[%d] has no %q: %v", i, f, m)
			}
		}
	}
	if !r33EnvIs(st, 22, 45) {
		t.Errorf("env = %v, want temperature 22 and humidity 45", st["env"])
	}
	for _, id := range []string{"usr1", "usr2", "usr3"} {
		if _, ok := r33Buttons(st, id); !ok {
			t.Errorf("buttons.%s missing: %v", id, st["buttons"])
		}
	}
	if strip := jgetSlice(st, "leds", "strip"); len(strip) != 5 {
		t.Errorf("leds.strip = %v, want 5 entries", strip)
	}
	if _, ok := jget(st, "leds", "segments").(map[string]any); !ok {
		t.Errorf("leds.segments = %v, want an object", jget(st, "leds", "segments"))
	}
}

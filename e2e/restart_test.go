//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const rsName = "e2e-rs"

// rsInstance is a running instance and what its tests need to talk to it.
type rsInstance struct {
	env  *Env
	name string
	port int
	key  string
}

// rsUp brings up the instance and returns its API coordinates.
func rsUp(t *testing.T) *rsInstance {
	t.Helper()
	env := newEnv(t)
	up := env.Up(t, rsName)
	api, _ := up["api"].(map[string]any)
	port, _ := api["port"].(float64)
	key, _ := api["key"].(string)
	if port == 0 || key == "" {
		t.Fatalf("incomplete api object: %v", up)
	}
	return &rsInstance{env: env, name: rsName, port: int(port), key: key}
}

// setFan drives one fan through the encrypted API.
func (in *rsInstance) setFan(t *testing.T, fan, speed int) {
	t.Helper()
	out, code := in.env.APIClient(t, in.port, in.key, "set-fan", "--fan", fmt.Sprint(fan), "--speed", fmt.Sprint(speed))
	if code != 0 {
		t.Fatalf("set-fan %d %d: exit %d: %v", fan, speed, code, out)
	}
}

// rsPortVal is ports[i].<field> of a status object.
func rsPortVal(st map[string]any, i int, field string) float64 {
	f, _ := jget(st, "ports", i, field).(float64)
	return f
}

// rsFWPID is firmware.pid of a status object.
func rsFWPID(st map[string]any) int {
	f, _ := jget(st, "firmware", "pid").(float64)
	return int(f)
}

// rsExpectReady fails unless st is a fully resynced status: phase ready, bus
// connected, firmware running, no warnings.
func rsExpectReady(t *testing.T, what string, st map[string]any) {
	t.Helper()
	if st["phase"] != "ready" || jget(st, "firmware", "bus_connected") != true || jget(st, "firmware", "running") != true {
		t.Fatalf("%s: phase %v, bus_connected %v, running %v; status %v",
			what, st["phase"], jget(st, "firmware", "bus_connected"), jget(st, "firmware", "running"), st)
	}
	if w, _ := st["warnings"].([]any); len(w) != 0 {
		t.Fatalf("%s: warnings %v, want none", what, w)
	}
	if jget(st, "api", "ready") != true {
		t.Fatalf("%s: api.ready is not true: %v", what, st)
	}
}

// restart runs a restart command, requiring exit 0, and returns the status it prints.
func (in *rsInstance) restart(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out, code, stderr := in.env.Sil(t, append(args, in.name)...)
	if code != 0 {
		t.Fatalf("sil %s: exit %d: %v\nstderr: %s", strings.Join(args, " "), code, out, stderr)
	}
	return out
}

// rsAsync runs sil in a goroutine and hands back its outcome.
type rsResult struct {
	stdout string
	exit   int
	err    error
}

func (in *rsInstance) async(args ...string) <-chan rsResult {
	ch := make(chan rsResult, 1)
	go func() {
		so, exit, err := isoRun(in.env, args...)
		ch <- rsResult{so, exit, err}
	}()
	return ch
}

// rsWait waits for an async sil call and requires exit 0 with one JSON status.
func rsWait(t *testing.T, what string, ch <-chan rsResult) map[string]any {
	t.Helper()
	tm := sysClock.NewTimer(2 * time.Minute)
	defer tm.Stop()
	select {
	case r := <-ch:
		if r.err != nil || r.exit != 0 {
			t.Fatalf("%s: exit %d, err %v, stdout %s", what, r.exit, r.err, r.stdout)
		}
		var st map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &st); err != nil {
			t.Fatalf("%s: stdout is not JSON: %v: %q", what, err, r.stdout)
		}
		return st
	case <-tm.C():
		t.Fatalf("%s: did not finish", what)
		return nil
	}
}

// TestSimRestartResync restarts the simulator while a fan spins (criterion 3):
// the firmware keeps its pid, the commanded duty comes back from the
// firmware's snapshot and the rpm starts again from 0.
func TestSimRestartResync(t *testing.T) {
	in := rsUp(t)
	in.setFan(t, 1, 40)
	st := in.env.WaitStatus(t, in.name, 20*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 0, "duty") == 0.4 && rsPortVal(s, 0, "rpm") > 700
	})
	pid, before := rsFWPID(st), rsPortVal(st, 0, "rpm")

	out := in.restart(t, "sim", "restart")
	if got := jget(out, "sim", "engine_restarts"); got != float64(1) {
		t.Fatalf("sim.engine_restarts = %v, want 1", got)
	}
	if got := rsFWPID(out); got != pid {
		t.Fatalf("firmware pid %d after a simulator restart, want the unchanged %d", got, pid)
	}
	// Within 5 s the duty is back and the rpm has restarted below its old value.
	st = in.env.WaitStatus(t, in.name, 5*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 0, "duty") == 0.4
	})
	first := rsPortVal(st, 0, "rpm")
	if first >= before*0.95 {
		t.Fatalf("rpm right after the restart = %.1f, want below the old %.1f (a fresh World spins up from 0)", first, before)
	}
	t.Logf("rpm before %.1f, first after the restart %.1f", before, first)
	st = in.env.WaitStatus(t, in.name, 10*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 0, "rpm") > first+50
	})
	rsExpectReady(t, "after the simulator restart", st)
	if rsFWPID(st) != pid {
		t.Fatalf("firmware pid changed to %d", rsFWPID(st))
	}
	if jget(st, "firmware", "restarts") != float64(0) {
		t.Fatalf("firmware.restarts = %v, want 0", jget(st, "firmware", "restarts"))
	}
}

// TestSimGapAPIResponsive keeps the simulator away for 3 s: the firmware's
// encrypted API keeps answering, status says the bus is down, and afterwards
// the loop resyncs without manual steps.
func TestSimGapAPIResponsive(t *testing.T) {
	in := rsUp(t)
	res := in.async("sim", "restart", in.name, "--gap", "3s")

	st := in.env.WaitStatus(t, in.name, 3*time.Second, func(s map[string]any) bool {
		return jget(s, "firmware", "bus_connected") == false
	})
	if st["phase"] == "ready" || jget(st, "api", "ready") != true || jget(st, "firmware", "running") != true {
		t.Fatalf("status during the gap: phase %v, api.ready %v, running %v", st["phase"], jget(st, "api", "ready"), jget(st, "firmware", "running"))
	}
	if jget(st, "sim", "engine_restarts") != float64(1) {
		t.Fatalf("sim.engine_restarts during the gap = %v, want 1", jget(st, "sim", "engine_restarts"))
	}
	select {
	case r := <-res:
		t.Fatalf("the restart finished (exit %d) before the API check could run inside the gap", r.exit)
	default:
	}
	out, code := in.env.APIClient(t, in.port, in.key, "info")
	if code != 0 {
		t.Fatalf("firmware API during the simulator gap: exit %d: %v", code, out)
	}
	t.Logf("API answered during the gap: %v", out["device_name"])

	end := rsWait(t, "sil sim restart --gap 3s", res)
	rsExpectReady(t, "status printed by the restart", end)
	rsExpectReady(t, "status after the gap", in.env.Status(t, in.name))
}

// TestFwRestartResync restarts the firmware while a fan spins (D-05): new
// pid, last_exit requested, the simulator's rpm snapshot reaches the new
// firmware, and the duty starts again from the firmware's ALWAYS_OFF.
func TestFwRestartResync(t *testing.T) {
	in := rsUp(t)
	in.setFan(t, 2, 60)
	st := in.env.WaitStatus(t, in.name, 20*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 1, "duty") == 0.6 && rsPortVal(s, 1, "rpm") > 300
	})
	pid := rsFWPID(st)

	out := in.restart(t, "fw", "restart")
	rsExpectReady(t, "status printed by the restart", out)
	if got := rsFWPID(out); got == pid || got == 0 {
		t.Fatalf("firmware pid %d after a restart, want a new one (was %d)", got, pid)
	}
	if jget(out, "firmware", "last_exit", "kind") != "requested" {
		t.Fatalf("last_exit = %v, want kind requested", jget(out, "firmware", "last_exit"))
	}
	if jget(out, "firmware", "restarts") != float64(1) {
		t.Fatalf("firmware.restarts = %v, want 1", jget(out, "firmware", "restarts"))
	}
	if !pidGone(pid) {
		t.Fatalf("old firmware pid %d is still alive", pid)
	}
	// The restarted firmware boots with its fans off.
	in.env.WaitStatus(t, in.name, 5*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 1, "duty") == 0
	})
	// The fan is still coasting in the simulator: its rpm snapshot reached the firmware.
	watch, code := in.env.APIClient(t, in.port, in.key, "watch", "--seconds", "3")
	if code != 0 {
		t.Fatalf("watch exit %d: %v", code, watch)
	}
	var best float64
	for _, smp := range ioSamples(t, watch, "Fan 2 Speed") {
		if smp[1] > best {
			best = smp[1]
		}
	}
	if best <= 0 {
		t.Fatalf("Fan 2 Speed never rose above 0 after the firmware restart: %v", watch["series"])
	}
	t.Logf("Fan 2 Speed after the restart peaked at %.0f rpm (coasting)", best)
}

// TestRestartEitherOrder restarts both sides in every order, including a
// simulator restart issued while a firmware restart is in flight.
func TestRestartEitherOrder(t *testing.T) {
	in := rsUp(t)
	in.setFan(t, 3, 50)
	in.env.WaitStatus(t, in.name, 10*time.Second, func(s map[string]any) bool { return rsPortVal(s, 2, "duty") == 0.5 })

	t.Run("sim_then_fw", func(t *testing.T) {
		st := in.restart(t, "sim", "restart")
		rsExpectReady(t, "after sim restart", st)
		if got := rsPortVal(st, 2, "duty"); got != 0.5 {
			// The duty comes back with the firmware's snapshot; it may land just after the answer.
			st = in.env.WaitStatus(t, in.name, 5*time.Second, func(s map[string]any) bool { return rsPortVal(s, 2, "duty") == 0.5 })
		}
		st = in.restart(t, "fw", "restart")
		rsExpectReady(t, "after fw restart", st)
	})
	t.Run("fw_then_sim", func(t *testing.T) {
		rsExpectReady(t, "after fw restart", in.restart(t, "fw", "restart"))
		rsExpectReady(t, "after sim restart", in.restart(t, "sim", "restart"))
	})
	t.Run("sim_during_fw", func(t *testing.T) {
		fwRes := in.async("fw", "restart", in.name)
		// Let the firmware restart get going, then ask for a simulator restart on top of it.
		if err := sysClock.Sleep(t.Context(), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		simRes := in.async("sim", "restart", in.name)
		rsExpectReady(t, "fw restart in flight", rsWait(t, "sil fw restart", fwRes))
		rsExpectReady(t, "sim restart queued behind it", rsWait(t, "sil sim restart", simRes))
	})

	// After all of that: one bus connection, a consistent loop that still follows commands.
	st := in.env.WaitStatus(t, in.name, 10*time.Second, func(s map[string]any) bool { return s["phase"] == "ready" })
	rsExpectReady(t, "after the matrix", st)
	if got := jget(st, "sim", "engine_restarts"); got != float64(3) {
		t.Errorf("sim.engine_restarts = %v, want 3", got)
	}
	if got := jget(st, "firmware", "restarts"); got != float64(3) {
		t.Errorf("firmware.restarts = %v, want 3", got)
	}
	in.setFan(t, 4, 100)
	st = in.env.WaitStatus(t, in.name, 10*time.Second, func(s map[string]any) bool {
		return rsPortVal(s, 3, "duty") == 1 && rsPortVal(s, 3, "rpm") > 300
	})
	rsExpectReady(t, "driving the fan after the matrix", st)
}

// TestStatusDuringRestart polls `sil status` every 100 ms through a 2 s
// simulator gap (CLI-06): every call exits 0 with valid JSON.
func TestStatusDuringRestart(t *testing.T) {
	in := rsUp(t)
	res := in.async("sim", "restart", in.name, "--gap", "2s")
	calls, sawDown := 0, false
	var final map[string]any
loop:
	for {
		select {
		case r := <-res:
			if r.err != nil || r.exit != 0 {
				t.Fatalf("sil sim restart: exit %d, err %v, stdout %s", r.exit, r.err, r.stdout)
			}
			if err := json.Unmarshal([]byte(r.stdout), &final); err != nil {
				t.Fatalf("restart stdout is not JSON: %v", err)
			}
			break loop
		default:
		}
		st := in.env.Status(t, in.name) // fails the test on a non-zero exit or invalid JSON
		calls++
		if jget(st, "firmware", "bus_connected") == false {
			sawDown = true
		}
		if err := sysClock.Sleep(t.Context(), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d status calls during the restart", calls)
	if calls < 10 {
		t.Errorf("only %d status calls completed during a 2 s gap, want at least 10", calls)
	}
	if !sawDown {
		t.Errorf("no status call saw bus_connected false during the gap")
	}
	rsExpectReady(t, "status printed by the restart", final)
}

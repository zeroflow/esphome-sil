//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const svName = "e2e-sv"

// svInstance is a running instance plus the pids the kill matrix needs.
type svInstance struct {
	env  *Env
	name string
	port int
	run  string // run directory
}

// svUp brings the instance up and returns it.
func svUp(t *testing.T, name string) *svInstance {
	t.Helper()
	env := newEnv(t)
	up := env.Up(t, name)
	api, _ := up["api"].(map[string]any)
	port, _ := api["port"].(float64)
	run, _ := up["run_dir"].(string)
	if port == 0 || run == "" {
		t.Fatalf("incomplete up result: %v", up)
	}
	return &svInstance{env: env, name: name, port: int(port), run: run}
}

// servePID is the pid in run.json.
func (in *svInstance) servePID(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(in.run, "run.json"))
	if err != nil {
		t.Fatalf("run.json: %v", err)
	}
	var rf struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil || rf.PID == 0 {
		t.Fatalf("run.json %q: %v", raw, err)
	}
	return rf.PID
}

// fwPID is the firmware pid of the current status.
func (in *svInstance) fwPID(t *testing.T) int {
	t.Helper()
	pid := int(svNum(in.env.Status(t, in.name), "firmware", "pid"))
	if pid == 0 {
		t.Fatal("status has no firmware pid")
	}
	return pid
}

func svNum(st map[string]any, path ...any) float64 {
	f, _ := jget(st, path...).(float64)
	return f
}

// svGone waits until pid no longer exists.
func svGone(t *testing.T, what string, pid int, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, fmt.Sprintf("%s (pid %d) to be gone", what, pid), func() bool { return pidGone(pid) })
}

// TestFirmwareCrashAutoRestart kills the firmware with SIGKILL: the supervisor
// boots it again like a device (D-03) and the loop resyncs.
func TestFirmwareCrashAutoRestart(t *testing.T) {
	in := svUp(t, svName)
	setOut, code := in.env.APIClient(t, in.port, mustKey(t, in), "set-fan", "--fan", "1", "--speed", "40")
	if code != 0 {
		t.Fatalf("set-fan: exit %d: %v", code, setOut)
	}
	old := in.fwPID(t)
	if err := syscall.Kill(old, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 %d: %v", old, err)
	}
	st := in.env.WaitStatus(t, in.name, 10*time.Second, func(s map[string]any) bool {
		pid := int(svNum(s, "firmware", "pid"))
		return pid != 0 && pid != old && s["phase"] == "ready" && jget(s, "firmware", "bus_connected") == true
	})
	if got := svNum(st, "firmware", "restarts"); got != 1 {
		t.Errorf("firmware.restarts = %v, want 1", got)
	}
	if k := jget(st, "firmware", "last_exit", "kind"); k != "crashed" {
		t.Errorf("last_exit.kind = %v, want crashed", k)
	}
	if sig := jget(st, "firmware", "last_exit", "signal"); sig != "SIGKILL" {
		t.Errorf("last_exit.signal = %v, want SIGKILL", sig)
	}
	if jget(st, "api", "ready") != true || jget(st, "firmware", "running") != true {
		t.Errorf("api.ready %v, running %v, want true", jget(st, "api", "ready"), jget(st, "firmware", "running"))
	}
	if !pidGone(old) {
		t.Errorf("old firmware pid %d is still alive", old)
	}
}

// mustKey is the API key of the instance, from its status.
func mustKey(t *testing.T, in *svInstance) string {
	t.Helper()
	key, _ := jget(in.env.Status(t, in.name), "api", "key").(string)
	if key == "" {
		t.Fatal("status carries no api key")
	}
	return key
}

// TestSupervisorKillNoOrphan kills the supervisor with SIGKILL: the firmware
// goes with it (Pdeathsig), status says not_running and the same name can be
// brought up again (BUILD-07, criterion 4).
func TestSupervisorKillNoOrphan(t *testing.T) {
	in := svUp(t, svName)
	serve, fw := in.servePID(t), in.fwPID(t)
	if err := syscall.Kill(serve, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 %d: %v", serve, err)
	}
	svGone(t, "sil serve", serve, 5*time.Second)
	svGone(t, "firmware", fw, 2*time.Second)

	out, exit, _ := in.env.Sil(t, "status", in.name)
	if exit != 1 {
		t.Errorf("sil status after the supervisor died: exit %d, want 1; %v", exit, out)
	}
	if code := jget(out, "error", "code"); code != "not_running" {
		t.Errorf("error.code = %v, want not_running; %v", code, out)
	}

	up, exit, stderr := in.env.Sil(t, "up", in.name, "--api-port", fmt.Sprint(in.port))
	if exit != 0 {
		t.Fatalf("sil up after the kill: exit %d: %v\nstderr: %s", exit, up, stderr)
	}
	in.env.WaitStatus(t, in.name, 20*time.Second, func(s map[string]any) bool { return s["phase"] == "ready" })
	down, exit, _ := in.env.Sil(t, "down", in.name)
	if exit != 0 || down["stopped"] != true {
		t.Fatalf("sil down: exit %d, %v", exit, down)
	}
}

// TestSupervisorSIGTERM stops the supervisor with SIGTERM: it stops the
// firmware group, removes run.json and exits.
func TestSupervisorSIGTERM(t *testing.T) {
	in := svUp(t, svName)
	serve, fw := in.servePID(t), in.fwPID(t)
	if err := syscall.Kill(serve, syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM %d: %v", serve, err)
	}
	svGone(t, "sil serve", serve, 5*time.Second)
	svGone(t, "firmware", fw, 5*time.Second)
	if _, err := os.Stat(filepath.Join(in.run, "run.json")); !os.IsNotExist(err) {
		t.Errorf("run.json still exists after SIGTERM (stat err %v)", err)
	}
	for _, sock := range []string{"api.sock", "bus.sock"} {
		if _, err := os.Stat(filepath.Join(in.run, sock)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after SIGTERM (stat err %v)", sock, err)
		}
	}
	if _, exit, _ := in.env.Sil(t, "status", in.name); exit != 1 {
		t.Errorf("sil status after SIGTERM: exit %d, want 1 (not_running)", exit)
	}
}

// TestDownFallback stops the supervisor with SIGSTOP so it cannot answer: sil
// down falls back to signalling the run.json pid and still removes everything.
func TestDownFallback(t *testing.T) {
	in := svUp(t, svName)
	serve, fw := in.servePID(t), in.fwPID(t)
	t.Cleanup(func() { _ = syscall.Kill(serve, syscall.SIGKILL) }) // never leave a stopped process behind
	if err := syscall.Kill(serve, syscall.SIGSTOP); err != nil {
		t.Fatalf("SIGSTOP %d: %v", serve, err)
	}
	start := sysClock.Now()
	out, exit, stderr := in.env.Sil(t, "down", in.name)
	took := sysClock.Now() - start
	if exit != 0 || out["stopped"] != true {
		t.Fatalf("sil down with a stopped supervisor: exit %d, %v\nstderr: %s", exit, out, stderr)
	}
	if took > 15*time.Second {
		t.Errorf("sil down took %s, want at most 15 s", took.Round(100*time.Millisecond))
	}
	t.Logf("sil down fallback took %s", took.Round(100*time.Millisecond))
	svGone(t, "sil serve", serve, 2*time.Second)
	svGone(t, "firmware", fw, 2*time.Second)
	for _, f := range []string{"run.json", "api.sock", "bus.sock"} {
		if _, err := os.Stat(filepath.Join(in.run, f)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after the fallback (stat err %v)", f, err)
		}
	}
}

// TestDownNotRunning: down of an instance that never ran is a success.
func TestDownNotRunning(t *testing.T) {
	env := newEnv(t)
	out, exit, stderr := env.Sil(t, "down", "e2e-never")
	if exit != 0 {
		t.Fatalf("sil down e2e-never: exit %d: %v\nstderr: %s", exit, out, stderr)
	}
	if out["instance"] != "e2e-never" || out["stopped"] != false {
		t.Fatalf("sil down e2e-never = %v, want instance e2e-never and stopped false", out)
	}
}

//go:build sil_e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// isoAutoPortLock serialises the tests that exercise sil's own port
// allocation (no injected --api-port): the allocation starts at 6054, so two
// worktrees running them at the same time would race for the same port. The
// lock is a per-user, host-wide file held until the test's cleanups are done;
// t.Cleanup runs last-in first-out, so it is released after the `sil down`
// cleanups that Env.UpAutoPort registers later.
func isoAutoPortLock(t *testing.T) {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("sil-e2e-autoport-%d.lock", os.Getuid()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open auto-port lock %s: %v", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		t.Fatalf("lock %s: %v", path, err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	})
}

// isoAPI returns port, key, mac and device name of an `up` result.
func isoAPI(t *testing.T, up map[string]any) (port int, key, mac, dev string) {
	t.Helper()
	api, _ := up["api"].(map[string]any)
	p, _ := api["port"].(float64)
	key, _ = api["key"].(string)
	mac, _ = api["mac"].(string)
	dev, _ = api["device_name"].(string)
	if p == 0 || key == "" || mac == "" || dev == "" {
		t.Fatalf("incomplete api object: %v", up)
	}
	return int(p), key, mac, dev
}

// isoServePID is the pid in run.json of a running instance.
func isoServePID(t *testing.T, env *Env, name string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(env.Runtime, "sil", name, "run.json"))
	if err != nil {
		t.Fatalf("run.json of %s: %v", name, err)
	}
	var rf struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil || rf.PID == 0 {
		t.Fatalf("run.json of %s has no pid: %s (%v)", name, raw, err)
	}
	return rf.PID
}

// isoFirmwarePID is firmware.pid of `sil status`.
func isoFirmwarePID(st map[string]any) int {
	f, _ := jget(st, "firmware", "pid").(float64)
	return int(f)
}

// isoDown runs `sil down name` and requires exit 0 with stopped true.
func isoDown(t *testing.T, env *Env, name string) {
	t.Helper()
	out, code, stderr := env.Sil(t, "down", name)
	if code != 0 || out["stopped"] != true {
		t.Fatalf("sil down %s: exit %d: %v\nstderr: %s", name, code, out, stderr)
	}
}

// isoGone waits until none of the pids exists any more.
func isoGone(t *testing.T, what string, pids ...int) {
	t.Helper()
	waitFor(t, 10*time.Second, what+" to be gone", func() bool {
		for _, pid := range pids {
			if !pidGone(pid) {
				return false
			}
		}
		return true
	})
}

// isoExpectError runs sil and requires one JSON error object with the given
// exit code and error code.
func isoExpectError(t *testing.T, env *Env, wantExit int, wantCode string, args ...string) map[string]any {
	t.Helper()
	out, exit, stderr := env.Sil(t, args...)
	if exit != wantExit || jget(out, "error", "code") != wantCode {
		t.Fatalf("sil %v: exit %d, error %v; want exit %d %s\nstderr: %s", args, exit, out["error"], wantExit, wantCode, stderr)
	}
	return out
}

// isoRun executes sil without touching *testing.T, so it is safe in goroutines.
func isoRun(env *Env, args ...string) (stdout string, exit int, err error) {
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 25*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, env.Bin, args...)
	cmd.Env = env.environ()
	var so bytes.Buffer
	cmd.Stdout = &so
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return "", 0, err
		}
		exit = ee.ExitCode()
	}
	return so.String(), exit, nil
}

// TestTwoInstances runs two auto-port instances side by side (BUILD-05,
// criterion 4): distinct identity, independent plants, and `sil down` of each
// removes all four processes.
func TestTwoInstances(t *testing.T) {
	isoAutoPortLock(t)
	env := newEnv(t)
	upA := env.UpAutoPort(t, "e2e-iso-a")
	upB := env.UpAutoPort(t, "e2e-iso-b")

	portA, keyA, macA, devA := isoAPI(t, upA)
	portB, keyB, macB, devB := isoAPI(t, upB)
	if portA < 6054 || portB < 6054 || portA == portB {
		t.Errorf("ports %d and %d: want distinct ports from 6054", portA, portB)
	}
	if macA == macB || !strings.HasPrefix(macA, "02:") || !strings.HasPrefix(macB, "02:") {
		t.Errorf("macs %q and %q: want distinct, 02: prefix", macA, macB)
	}
	if devA != "sil-e2e-iso-a" || devB != "sil-e2e-iso-b" {
		t.Errorf("device names %q and %q", devA, devB)
	}
	if keyA == keyB {
		t.Errorf("both instances share an API key")
	}
	if upA["run_dir"] == upB["run_dir"] {
		t.Errorf("both instances share run dir %v", upA["run_dir"])
	}

	if out, code := env.APIClient(t, portA, keyA, "set-fan", "--fan", "1", "--speed", "50"); code != 0 {
		t.Fatalf("set-fan on a: exit %d: %v", code, out)
	}
	env.WaitStatus(t, "e2e-iso-a", 10*time.Second, func(s map[string]any) bool { return tracerPortValue(s, 0, "duty") == 0.5 })
	if d := tracerPortValue(env.Status(t, "e2e-iso-b"), 0, "duty"); d != 0 {
		t.Errorf("b's duty changed to %v after a fan command on a", d)
	}

	serveA, fwA := isoServePID(t, env, "e2e-iso-a"), isoFirmwarePID(env.Status(t, "e2e-iso-a"))
	isoDown(t, env, "e2e-iso-a")
	isoGone(t, "a's serve and firmware processes", serveA, fwA)
	if st := env.Status(t, "e2e-iso-b"); st["phase"] != "ready" {
		t.Errorf("b after a went down: phase %v", st["phase"])
	}

	again := env.UpAutoPort(t, "e2e-iso-a")
	portA2, keyA2, macA2, _ := isoAPI(t, again)
	if portA2 != portA || keyA2 != keyA || macA2 != macA {
		t.Errorf("restarted a changed identity: port %d->%d mac %s->%s key same %v", portA, portA2, macA, macA2, keyA2 == keyA)
	}

	pids := []int{
		isoServePID(t, env, "e2e-iso-a"), isoFirmwarePID(env.Status(t, "e2e-iso-a")),
		isoServePID(t, env, "e2e-iso-b"), isoFirmwarePID(env.Status(t, "e2e-iso-b")),
	}
	for _, pid := range pids {
		if pid == 0 || pidGone(pid) {
			t.Fatalf("expected four live processes, got %v", pids)
		}
	}
	isoDown(t, env, "e2e-iso-a")
	isoDown(t, env, "e2e-iso-b")
	isoGone(t, "all four processes of both instances", pids...)
}

// TestPrefixNames: names sharing a prefix get disjoint state, run and cache
// directories.
func TestPrefixNames(t *testing.T) {
	env := newEnv(t)
	a := env.Up(t, "e2e-iso-a")
	b := env.Up(t, "e2e-iso-a-1")
	runA, _ := a["run_dir"].(string)
	runB, _ := b["run_dir"].(string)
	dirs := map[string][2]string{
		"run":   {filepath.Join(env.Runtime, "sil", "e2e-iso-a"), filepath.Join(env.Runtime, "sil", "e2e-iso-a-1")},
		"state": {filepath.Join(env.State, "sil", "e2e-iso-a"), filepath.Join(env.State, "sil", "e2e-iso-a-1")},
		"cache": {filepath.Join(env.Cache, "sil", "build", "e2e-iso-a"), filepath.Join(env.Cache, "sil", "build", "e2e-iso-a-1")},
	}
	if runA != dirs["run"][0] || runB != dirs["run"][1] {
		t.Errorf("run dirs %q and %q, want %v", runA, runB, dirs["run"])
	}
	for kind, d := range dirs {
		if _, err := os.Stat(d[0]); err != nil {
			t.Errorf("%s dir of a: %v", kind, err)
		}
		if _, err := os.Stat(d[1]); err != nil {
			t.Errorf("%s dir of a-1: %v", kind, err)
		}
		if strings.HasPrefix(d[1]+"/", d[0]+"/") || strings.HasPrefix(d[0]+"/", d[1]+"/") {
			t.Errorf("%s dirs nest: %s vs %s", kind, d[0], d[1])
		}
	}
	isoDown(t, env, "e2e-iso-a-1")
	if st := env.Status(t, "e2e-iso-a"); st["phase"] != "ready" {
		t.Errorf("a after a-1 went down: phase %v", st["phase"])
	}
}

// TestPortInUse: a taken port exits 2 port_in_use naming the port and the
// holder, never moves silently, and nothing is built or started.
func TestPortInUse(t *testing.T) {
	env := newEnv(t)
	port := freePort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Fatalf("hold port %d: %v", port, err)
	}
	defer ln.Close()

	out := isoExpectError(t, env, 2, "port_in_use", "up", "e2e-iso-c", "--api-port", fmt.Sprint(port))
	if got, _ := jget(out, "error", "detail", "port").(float64); int(got) != port {
		t.Errorf("detail.port = %v, want %d", jget(out, "error", "detail", "port"), port)
	}
	if pid, _ := jget(out, "error", "detail", "holder", "pid").(float64); int(pid) != os.Getpid() {
		t.Errorf("detail.holder.pid = %v, want %d (this test)", jget(out, "error", "detail", "holder"), os.Getpid())
	}
	if msg, _ := jget(out, "error", "message").(string); !strings.Contains(msg, fmt.Sprint(port)) {
		t.Errorf("message does not name the port: %q", msg)
	}
	// The stored (sticky) port is taken: still port_in_use, no silent move.
	out = isoExpectError(t, env, 2, "port_in_use", "up", "e2e-iso-c")
	if got, _ := jget(out, "error", "detail", "port").(float64); int(got) != port {
		t.Errorf("sticky port moved: detail.port = %v, want %d", jget(out, "error", "detail", "port"), port)
	}
	if _, err := os.Stat(filepath.Join(env.Runtime, "sil", "e2e-iso-c", "run.json")); err == nil {
		t.Errorf("a run.json exists although up failed")
	}
}

// TestPortClaimed: another instance's stored port cannot be taken; bad ports
// are invalid_argument.
func TestPortClaimed(t *testing.T) {
	env := newEnv(t)
	up := env.Up(t, "e2e-iso-a")
	portA, _, _, _ := isoAPI(t, up)
	isoDown(t, env, "e2e-iso-a")

	out := isoExpectError(t, env, 1, "port_claimed", "up", "e2e-iso-d", "--api-port", fmt.Sprint(portA))
	if jget(out, "error", "detail", "instance") != "e2e-iso-a" {
		t.Errorf("detail.instance = %v, want e2e-iso-a", jget(out, "error", "detail"))
	}
	if msg, _ := jget(out, "error", "message").(string); !strings.Contains(msg, "e2e-iso-a") {
		t.Errorf("message does not name the owner: %q", msg)
	}
	for _, bad := range []string{"80", "1023", "65536"} {
		isoExpectError(t, env, 1, "invalid_argument", "up", "e2e-iso-d", "--api-port", bad)
	}
}

// TestFreshPrefs: every `sil up` recreates the run dir, so preferences start
// fresh, and the firmware's environment points at <run>/prefs (BUILD-04).
func TestFreshPrefs(t *testing.T) {
	const name = "e2e-iso-f"
	env := newEnv(t)
	up := env.Up(t, name)
	run, _ := up["run_dir"].(string)
	prefs := filepath.Join(run, "prefs")
	marker := filepath.Join(prefs, "marker")
	if err := os.WriteFile(marker, []byte("stale"), 0o600); err != nil {
		t.Fatalf("marker: %v", err)
	}
	fwPID := isoFirmwarePID(env.Status(t, name))
	environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", fwPID))
	if err != nil {
		t.Fatalf("firmware environ: %v", err)
	}
	if !bytes.Contains(append([]byte{0}, environ...), []byte("\x00ESPHOME_PREFDIR="+prefs+"\x00")) {
		t.Errorf("firmware environment lacks ESPHOME_PREFDIR=%s", prefs)
	}

	isoDown(t, env, name)
	env.Up(t, name)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("marker survived down and up: %v", err)
	}
	if fi, err := os.Stat(prefs); err != nil || !fi.IsDir() {
		t.Errorf("prefs dir after up: %v", err)
	}
}

// TestReadyIsEncrypted: ready means a real encrypted handshake; the firmware
// keeps running with no API client; a wrong key cannot connect.
func TestReadyIsEncrypted(t *testing.T) {
	const name = "e2e-iso-e"
	env := newEnv(t)
	up := env.Up(t, name)
	port, key, _, _ := isoAPI(t, up)

	st := env.Status(t, name)
	if jget(st, "api", "ready") != true || st["phase"] != "ready" {
		t.Fatalf("status after up: api.ready %v, phase %v", jget(st, "api", "ready"), st["phase"])
	}

	t.Run("idle_hold", func(t *testing.T) {
		pid := isoFirmwarePID(st)
		if pid == 0 {
			t.Fatalf("no firmware pid in %v", st)
		}
		if err := sysClock.Sleep(t.Context(), 10*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("firmware pid %d is gone after 10 s idle: %v", pid, err)
		}
		after := env.Status(t, name)
		if isoFirmwarePID(after) != pid || jget(after, "firmware", "running") != true ||
			jget(after, "firmware", "restarts") != float64(0) || jget(after, "firmware", "last_exit") != nil ||
			after["phase"] != "ready" {
			t.Errorf("after 10 s idle: firmware %v, phase %v", after["firmware"], after["phase"])
		}
	})

	wrong := make([]byte, 32)
	if _, err := rand.Read(wrong); err != nil {
		t.Fatal(err)
	}
	if out, code := env.APIClient(t, port, base64.StdEncoding.EncodeToString(wrong), "info"); code != 3 {
		t.Errorf("wrong key: exit %d, want 3 (connection refused by the handshake): %v", code, out)
	}
	out, code := env.APIClient(t, port, key, "info")
	if code != 0 || out["device_name"] != "sil-"+name {
		t.Errorf("right key: exit %d: %v", code, out)
	}
}

// TestAlreadyRunningAndBusy: two concurrent ups of one instance (one builds,
// the other is instance_busy), then a third up of the running instance is
// already_running.
func TestAlreadyRunningAndBusy(t *testing.T) {
	const name = "e2e-iso-g"
	env := newEnv(t)
	t.Cleanup(func() { _, _, _ = env.run(t, "down", name) })
	port := freePort(t)

	type result struct {
		stdout string
		exit   int
		err    error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			so, exit, err := isoRun(env, "up", name, "--api-port", fmt.Sprint(port))
			results[i] = result{so, exit, err}
		}()
	}
	wg.Wait()
	var ok, busy int
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("running sil: %v", r.err)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(r.stdout), &out)
		switch {
		case r.exit == 0:
			ok++
		case r.exit == 1 && jget(out, "error", "code") == "instance_busy":
			busy++
		default:
			t.Errorf("unexpected result: exit %d stdout %s", r.exit, r.stdout)
		}
	}
	if ok != 1 || busy != 1 {
		t.Fatalf("concurrent ups: %d succeeded, %d instance_busy; want 1 and 1", ok, busy)
	}

	out := isoExpectError(t, env, 1, "already_running", "up", name)
	if pid, _ := jget(out, "error", "detail", "pid").(float64); int(pid) != isoServePID(t, env, name) {
		t.Errorf("already_running detail.pid = %v, want %d", jget(out, "error", "detail", "pid"), isoServePID(t, env, name))
	}
	if st := env.Status(t, name); st["phase"] != "ready" {
		t.Errorf("the running instance was disturbed: phase %v", st["phase"])
	}
}

// TestNamesAndPaths: strict names, socket path length, unsafe run-dir parent.
func TestNamesAndPaths(t *testing.T) {
	env := newEnv(t)
	for _, bad := range []string{"Bad_Name", "A", strings.Repeat("a", 28), "a-", "a_b"} {
		isoExpectError(t, env, 1, "invalid_name", "up", bad)
	}

	deep := "/tmp/" + strings.Repeat("d", 100)
	long := &Env{State: env.State, Runtime: deep, Cache: env.Cache, Repo: env.Repo, Bin: env.Bin}
	out := isoExpectError(t, long, 2, "socket_path_too_long", "up", "e2e-iso-n")
	wantPath := filepath.Join(deep, "sil", "e2e-iso-n", "bus.sock")
	if jget(out, "error", "detail", "path") != wantPath {
		t.Errorf("detail.path = %v, want %s", jget(out, "error", "detail", "path"), wantPath)
	}
	if msg, _ := jget(out, "error", "message").(string); !strings.Contains(msg, wantPath) {
		t.Errorf("message does not name the path: %q", msg)
	}
	if _, err := os.Stat(deep); err == nil {
		t.Errorf("%s was created although the socket path is too long", deep)
	}
	if entries, _ := os.ReadDir(filepath.Join(env.State, "sil")); len(entries) != 0 {
		t.Errorf("a refused up left state behind: %v", entries)
	}

	// A pre-existing, group-readable run-dir parent is refused before any build.
	parent := filepath.Join(env.Runtime, "sil")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	isoExpectError(t, env, 2, "unsafe_runtime_dir", "up", "e2e-iso-n")
}

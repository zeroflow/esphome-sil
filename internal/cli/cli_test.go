package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alecthomas/kong"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

const secretKey = "c2VjcmV0LWtleS1mb3ItdGhlLXRlc3Q="

var testClock = clock.NewReal()

// cliEnv points the XDG directories at fresh temp dirs (short, so the unix
// socket paths stay below sun_path) and unsets SIL_INSTANCE.
func cliEnv(t *testing.T) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "silcli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv(envInstance, "")
}

// result is one captured invocation.
type result struct {
	stdout, stderr string
	exit           int
}

// run drives Execute and fails the test unless stdout is exactly one JSON
// value (or empty for --help, which prints to stderr).
func run(t *testing.T, args ...string) result {
	t.Helper()
	var so, se bytes.Buffer
	exit := Execute(args, &so, &se)
	r := result{so.String(), se.String(), exit}
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("sil %v: stdout is not a JSON value (exit %d): %v\nstdout: %q\nstderr: %q", args, exit, err, r.stdout, r.stderr)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("sil %v: stdout holds more than one JSON value: %q", args, r.stdout)
	}
	return r
}

// object decodes stdout into a map.
func (r result) object(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatalf("stdout is not a JSON object: %v: %q", err, r.stdout)
	}
	return m
}

// errCode returns error.code, or "" when stdout is not an error object.
func (r result) errCode(t *testing.T) string {
	t.Helper()
	e, _ := r.object(t)["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// detail returns error.detail.
func (r result) detail(t *testing.T) map[string]any {
	t.Helper()
	e, _ := r.object(t)["error"].(map[string]any)
	d, _ := e["detail"].(map[string]any)
	return d
}

// fakeControl is the control.Control a supervisor would be: it records what
// the CLI asked and answers canned values.
type fakeControl struct {
	control.Control // later additions to the interface compile without touching this fake
	mu              sync.Mutex
	calls           []string
	sim             []control.SimRestartRequest
	press           []control.PressRequest
	sets            []control.SetRequest
	fans            control.FansResult
	edges           []bool
	status          control.Status
	err             error
	onDown          func() // runs when a down request arrives (e.g. to end the stand-in supervisor)
	// watch, when set, is the Watch handler; watchKinds records the kinds each watch asked for.
	watch      func(ctx context.Context, send func(any) error) error
	watchKinds [][]string
}

func (f *fakeControl) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

func (f *fakeControl) Status(context.Context) (control.Status, error) {
	return f.status, f.record("status")
}

func (f *fakeControl) Down(context.Context) (control.DownResult, error) {
	if f.onDown != nil {
		f.onDown()
	}
	return control.DownResult{}, f.record("down")
}

func (f *fakeControl) SimRestart(_ context.Context, r control.SimRestartRequest) (control.Status, error) {
	f.mu.Lock()
	f.sim = append(f.sim, r)
	f.mu.Unlock()
	return f.status, f.record("sim.restart")
}

func (f *fakeControl) FwRestart(context.Context) (control.Status, error) {
	return f.status, f.record("fw.restart")
}

func (f *fakeControl) Press(_ context.Context, id string, r control.PressRequest) (control.PressResult, error) {
	f.mu.Lock()
	f.press = append(f.press, r)
	f.mu.Unlock()
	return control.PressResult{Button: id, Action: "press", HoldMS: r.HoldMS, TDownUS: 5, TUpUS: 5 + r.HoldMS*1000}, f.record("press." + id)
}

func (f *fakeControl) SetButton(_ context.Context, id string, down bool) (control.PressResult, error) {
	f.mu.Lock()
	f.edges = append(f.edges, down)
	f.mu.Unlock()
	action := "up"
	if down {
		action = "down"
	}
	return control.PressResult{Button: id, Action: action, Level: down}, f.record(action + "." + id)
}

func (f *fakeControl) Set(_ context.Context, r control.SetRequest) (control.SetResult, error) {
	f.mu.Lock()
	f.sets = append(f.sets, r)
	f.mu.Unlock()
	applied := map[string]any{}
	for k, raw := range r.Values {
		var v float64
		_ = json.Unmarshal(bytes.Trim(raw, `"`), &v)
		applied[k] = v
	}
	return control.SetResult{Applied: applied}, f.record("set")
}

func (f *fakeControl) Fans(context.Context) (control.FansResult, error) {
	return f.fans, f.record("fans")
}

func (f *fakeControl) Watch(ctx context.Context, r control.WatchRequest, send func(any) error) error {
	f.mu.Lock()
	f.watchKinds = append(f.watchKinds, r.Kinds)
	watch := f.watch
	f.mu.Unlock()
	f.record("watch")
	if watch == nil {
		return nil
	}
	return watch(ctx, send)
}

func (f *fakeControl) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func readyStatus(name string) control.Status {
	return control.Status{
		Schema: control.SchemaVersion, Instance: name, Phase: control.PhaseReady,
		API:   control.APIStatus{DeviceName: "sil-" + name, MAC: "02:00:00:00:00:01", Host: "127.0.0.1", Port: 6054, Key: secretKey, Ready: true},
		Ports: []control.PortStatus{{Port: 1, Fan: "arctic-p12-pwm-pst", Duty: 0.4, RPM: 1200, State: "running"}},
	}
}

// supervisorProc starts a process that looks like `sil serve <name>` to
// instance.ProcessAlive (argv[0] is "sil", the arguments carry `serve <name>`)
// and registers its removal.
func supervisorProc(t *testing.T, name string) int {
	t.Helper()
	cmd := exec.Command("bash", "-c", `exec -a sil bash -c 'sleep 120 & wait' serve "$1"`, "bash", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the stand-in supervisor: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	// Wait for the exec: until then the cmdline is the launcher's, not sil's.
	ctx, cancel := clock.WithTimeout(context.Background(), testClock, 5*time.Second)
	defer cancel()
	for !instance.ProcessAlive(instance.RunFile{PID: cmd.Process.Pid}, name) {
		if err := testClock.Sleep(ctx, 5*time.Millisecond); err != nil {
			t.Fatalf("the stand-in supervisor for %s never became `sil serve %s`", name, name)
		}
	}
	return cmd.Process.Pid
}

// deadPID returns the PID of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeRun(t *testing.T, name string, pid int) instance.Paths {
	t.Helper()
	p, err := instance.PathsFor(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.WriteRunFile(p, instance.RunFile{
		Schema: 1, Instance: name, PID: pid, RunDir: p.RunDir, APISock: p.APISock(), BusSock: p.BusSock(),
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

// liveInstance registers a running instance: a live stand-in supervisor
// process and run.json. With serve it also serves fc on the control socket via
// control.Serve.
func liveInstance(t *testing.T, name string, fc *fakeControl) instance.Paths {
	t.Helper()
	p := writeRun(t, name, supervisorProc(t, name))
	if fc == nil {
		return p
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- control.Serve(ctx, p.APISock(), control.NewHandler(fc)) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitSocket(t, p.APISock())
	return p
}

func waitSocket(t *testing.T, path string) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), testClock, 5*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if err := testClock.Sleep(ctx, 10*time.Millisecond); err != nil {
			t.Fatalf("control socket %s never appeared", path)
		}
	}
}

func TestResolveInstance(t *testing.T) {
	t.Run("positional wins over the environment and live instances", func(t *testing.T) {
		cliEnv(t)
		t.Setenv(envInstance, "from-env")
		liveInstance(t, "live", nil)
		if got, err := resolveInstance("given"); err != nil || got != "given" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("SIL_INSTANCE wins over live instances", func(t *testing.T) {
		cliEnv(t)
		t.Setenv(envInstance, "from-env")
		liveInstance(t, "live", nil)
		if got, err := resolveInstance(""); err != nil || got != "from-env" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("the single live instance", func(t *testing.T) {
		cliEnv(t)
		liveInstance(t, "only", nil)
		writeRun(t, "stale", deadPID(t)) // a dead supervisor's leftover does not count
		if got, err := resolveInstance(""); err != nil || got != "only" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("none is ambiguous with an empty candidate list", func(t *testing.T) {
		cliEnv(t)
		writeRun(t, "stale", deadPID(t))
		_, err := resolveInstance("")
		assertAmbiguous(t, err, []string{})
	})
	t.Run("several are listed sorted", func(t *testing.T) {
		cliEnv(t)
		for _, n := range []string{"e2e-b", "zeta", "e2e-a", "alpha"} {
			liveInstance(t, n, nil)
		}
		_, err := resolveInstance("")
		assertAmbiguous(t, err, []string{"alpha", "e2e-a", "e2e-b", "zeta"})
	})
}

func assertAmbiguous(t *testing.T, err error, want []string) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "instance_ambiguous" || ae.Exit != apperr.ExitFailed {
		t.Fatalf("got %v, want Failed instance_ambiguous", err)
	}
	got, _ := ae.Detail["candidates"].([]string)
	if got == nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates %#v, want %#v", ae.Detail["candidates"], want)
	}
}

func TestNoInstanceIsAmbiguousJSON(t *testing.T) {
	cliEnv(t)
	for _, cmd := range [][]string{{"status"}, {"down"}, {"sim", "restart"}, {"fw", "restart"}} {
		r := run(t, cmd...)
		if r.exit != 1 || r.errCode(t) != "instance_ambiguous" {
			t.Fatalf("sil %v: exit %d, stdout %s", cmd, r.exit, r.stdout)
		}
		if !strings.Contains(r.stdout, `"detail":{"candidates":[]}`) {
			t.Fatalf("sil %v: want an empty candidate list in %s", cmd, r.stdout)
		}
	}
	liveInstance(t, "b", nil)
	liveInstance(t, "a", nil)
	r := run(t, "status")
	if r.exit != 1 || !strings.Contains(r.stdout, `"candidates":["a","b"]`) {
		t.Fatalf("exit %d, stdout %s", r.exit, r.stdout)
	}
}

// TestCommandsAreControlClients drives status, sim restart and fw restart
// against a fake control server only: no firmware, no supervisor.
func TestCommandsAreControlClients(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)

	r := run(t, "status", "fan")
	if r.exit != 0 || r.stderr != "" {
		t.Fatalf("status: exit %d, stderr %q, stdout %s", r.exit, r.stderr, r.stdout)
	}
	var st control.Status
	if err := json.Unmarshal([]byte(r.stdout), &st); err != nil || st.Instance != "fan" || st.API.Key != secretKey {
		t.Fatalf("status body %s (%v)", r.stdout, err)
	}
	// Field order follows the Go struct, not a map.
	if i, j := strings.Index(r.stdout, `"schema"`), strings.Index(r.stdout, `"instance"`); i != 0+len(`{`) || j < i {
		t.Fatalf("status fields are not in struct order: %s", r.stdout)
	}

	// The single live instance is found without a name; SIL_INSTANCE works too.
	if r := run(t, "status"); r.exit != 0 {
		t.Fatalf("status without a name: exit %d, %s", r.exit, r.stdout)
	}
	t.Setenv(envInstance, "fan")
	if r := run(t, "status"); r.exit != 0 {
		t.Fatalf("status via SIL_INSTANCE: exit %d, %s", r.exit, r.stdout)
	}

	if r := run(t, "sim", "restart", "--gap", "3s"); r.exit != 0 || r.object(t)["phase"] != "ready" {
		t.Fatalf("sim restart: exit %d, %s", r.exit, r.stdout)
	}
	if r := run(t, "sim", "restart"); r.exit != 0 {
		t.Fatalf("sim restart without gap: exit %d, %s", r.exit, r.stdout)
	}
	if r := run(t, "fw", "restart", "fan"); r.exit != 0 || r.object(t)["instance"] != "fan" {
		t.Fatalf("fw restart: exit %d, %s", r.exit, r.stdout)
	}

	want := []string{"status", "status", "status", "sim.restart", "sim.restart", "fw.restart"}
	if got := fc.seen(); !reflect.DeepEqual(got, want) {
		t.Fatalf("control calls %v, want %v", got, want)
	}
	if len(fc.sim) != 2 || fc.sim[0].GapS != 3 || fc.sim[1].GapS != 0 {
		t.Fatalf("sim restart requests %+v, want gap 3 then 0", fc.sim)
	}
}

func TestControlErrorsKeepTheirExitClass(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *apperr.Error
		exit int
	}{
		{"4xx is user-fixable", apperr.Failed("bad_state", "not now", map[string]any{"k": "v"}), 1},
		{"5xx is infrastructure", apperr.Infra("resync_failed", "engine is stuck", nil), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cliEnv(t)
			fc := &fakeControl{status: readyStatus("fan"), err: tc.err}
			liveInstance(t, "fan", fc)
			for _, cmd := range [][]string{{"status", "fan"}, {"sim", "restart", "fan"}, {"fw", "restart", "fan"}} {
				r := run(t, cmd...)
				if r.exit != tc.exit || r.errCode(t) != tc.err.Code {
					t.Fatalf("sil %v: exit %d, stdout %s; want exit %d code %s", cmd, r.exit, r.stdout, tc.exit, tc.err.Code)
				}
				if tc.err.Detail != nil && r.detail(t)["k"] != "v" {
					t.Fatalf("sil %v: detail lost: %s", cmd, r.stdout)
				}
				if strings.Contains(r.stdout+r.stderr, secretKey) {
					t.Fatalf("sil %v: the API key leaked into an error path", cmd)
				}
			}
		})
	}
}

func TestStatusFailureClasses(t *testing.T) {
	t.Run("run.json with a dead PID", func(t *testing.T) {
		cliEnv(t)
		p := writeRun(t, "gone", deadPID(t))
		r := run(t, "status", "gone")
		if r.exit != 1 || r.errCode(t) != "not_running" {
			t.Fatalf("exit %d, %s", r.exit, r.stdout)
		}
		d := r.detail(t)
		if d["phase"] != "dead" || d["run_dir"] != p.RunDir || d["instance"] != "gone" {
			t.Fatalf("detail %v, want phase dead and run_dir %s", d, p.RunDir)
		}
		for _, cmd := range [][]string{{"sim", "restart", "gone"}, {"fw", "restart", "gone"}} {
			if r := run(t, cmd...); r.exit != 1 || r.errCode(t) != "not_running" || r.detail(t)["phase"] != "dead" {
				t.Fatalf("sil %v: exit %d, %s", cmd, r.exit, r.stdout)
			}
		}
	})
	t.Run("an instance that never ran", func(t *testing.T) {
		cliEnv(t)
		if r := run(t, "status", "never"); r.exit != 1 || r.errCode(t) != "not_running" {
			t.Fatalf("exit %d, %s", r.exit, r.stdout)
		}
	})
	t.Run("a live PID with an unreachable control socket", func(t *testing.T) {
		cliEnv(t)
		liveInstance(t, "mute", nil)
		for _, cmd := range [][]string{{"status", "mute"}, {"sim", "restart", "mute"}, {"fw", "restart", "mute"}} {
			if r := run(t, cmd...); r.exit != 2 || r.errCode(t) != "control_unreachable" {
				t.Fatalf("sil %v: exit %d, %s", cmd, r.exit, r.stdout)
			}
		}
	})
	t.Run("phase failed is data, exit 0", func(t *testing.T) {
		cliEnv(t)
		st := readyStatus("sick")
		st.Phase = control.PhaseFailed
		st.Error = apperr.Failed("firmware_crash_loop", "the firmware keeps crashing", nil)
		liveInstance(t, "sick", &fakeControl{status: st})
		r := run(t, "status", "sick")
		if r.exit != 0 || r.object(t)["phase"] != "failed" {
			t.Fatalf("exit %d, %s", r.exit, r.stdout)
		}
	})
}

func TestParseErrorsAreJSON(t *testing.T) {
	cliEnv(t)
	for _, args := range [][]string{
		{"status", "--bogus"},
		{"frobnicate"},
		{},
		{"up"},
		{"sim", "restart", "--gap", "soon"},
		{"--nope", "status"},
	} {
		r := run(t, args...)
		if r.exit != 1 || r.errCode(t) != "invalid_argument" {
			t.Errorf("sil %v: exit %d, stdout %s; want exit 1 invalid_argument", args, r.exit, r.stdout)
		}
		if msg, _ := r.object(t)["error"].(map[string]any)["message"].(string); msg == "" {
			t.Errorf("sil %v: empty message", args)
		}
	}
}

func TestQuiet(t *testing.T) {
	cliEnv(t)
	liveInstance(t, "fan", &fakeControl{status: readyStatus("fan")})
	for _, args := range [][]string{{"--quiet", "status", "fan"}, {"status", "fan", "--quiet"}} {
		r := run(t, args...)
		if r.exit != 0 || r.stderr != "" {
			t.Fatalf("sil %v: exit %d, stderr %q", args, r.exit, r.stderr)
		}
	}
	// A failure still says what went wrong on stderr, and never the key.
	r := run(t, "--quiet", "status", "nobody")
	if r.exit != 1 || !strings.HasPrefix(r.stderr, "sil: not_running:") || strings.Contains(r.stderr, secretKey) {
		t.Fatalf("exit %d, stderr %q", r.exit, r.stderr)
	}
}

func TestHelpStaysOffStdout(t *testing.T) {
	cliEnv(t)
	var so, se bytes.Buffer
	if exit := Execute([]string{"--help"}, &so, &se); exit != 0 || so.Len() != 0 || !strings.Contains(se.String(), "Usage") {
		t.Fatalf("exit %d, stdout %q, stderr %q", exit, so.String(), se.String())
	}
}

// TestUnsafeRuntimeBaseIsRefused covers the world-reachable /tmp fallback: a
// runtime base that is not private is not trusted to name instances.
func TestUnsafeRuntimeBaseIsRefused(t *testing.T) {
	cliEnv(t)
	liveInstance(t, "fan", &fakeControl{status: readyStatus("fan")})
	base, err := runtimeBase()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{{"status"}, {"status", "fan"}, {"fw", "restart", "fan"}, {"down", "fan"}, {"down"}} {
		if r := run(t, cmd...); r.exit != 2 || r.errCode(t) != "unsafe_runtime_dir" {
			t.Fatalf("sil %v: exit %d, %s", cmd, r.exit, r.stdout)
		}
	}
}

// TestDownDoesNotDialTheRunFileSocket: like every instance command, down dials
// the socket in the instance's own run dir; the path run.json names is never
// connected to (T-01-17).
func TestDownDoesNotDialTheRunFileSocket(t *testing.T) {
	cliEnv(t)
	own := &fakeControl{status: readyStatus("fan")}
	p := liveInstance(t, "fan", own)
	foreign := &fakeControl{status: readyStatus("fan")}
	foreignSock := filepath.Join(t.TempDir(), "elsewhere.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- control.Serve(ctx, foreignSock, control.NewHandler(foreign)) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitSocket(t, foreignSock)
	pid := supervisorProc(t, "fan")
	own.onDown = func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }
	if err := instance.WriteRunFile(p, instance.RunFile{
		Schema: 1, Instance: "fan", PID: pid, RunDir: p.RunDir,
		APISock: foreignSock, BusSock: p.BusSock(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "down", "fan"); r.exit != 0 {
		t.Fatalf("exit %d, %s", r.exit, r.stdout)
	}
	own.mu.Lock()
	defer own.mu.Unlock()
	foreign.mu.Lock()
	defer foreign.mu.Unlock()
	if len(foreign.calls) != 0 || len(own.calls) == 0 || own.calls[0] != "down" {
		t.Fatalf("own socket calls %v, run.json socket calls %v; want down on the own socket only", own.calls, foreign.calls)
	}
}

// TestRunFileDoesNotChooseTheSocket: run.json says whether an instance is live,
// the client always dials the socket in the instance's own run dir.
func TestRunFileDoesNotChooseTheSocket(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	p := liveInstance(t, "fan", fc)
	if err := instance.WriteRunFile(p, instance.RunFile{
		Schema: 1, Instance: "fan", PID: supervisorProc(t, "fan"), RunDir: p.RunDir,
		APISock: filepath.Join(t.TempDir(), "elsewhere.sock"), BusSock: p.BusSock(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "status", "fan"); r.exit != 0 || r.object(t)["instance"] != "fan" {
		t.Fatalf("exit %d, %s", r.exit, r.stdout)
	}
}

// TestPressHolds: the presets, --hold, the instance selectors and the one JSON value.
func TestPressHolds(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want int64
	}{
		{[]string{"press", "usr1", "-i", "fan"}, 200},
		{[]string{"press", "usr2", "--long", "-i", "fan"}, 1500},
		{[]string{"press", "usr1", "--hold", "750ms", "--instance", "fan"}, 750},
		{[]string{"press", "usr3", "--hold", "2s", "-i", "fan"}, 2000},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cliEnv(t)
			fc := &fakeControl{status: readyStatus("fan")}
			liveInstance(t, "fan", fc)
			r := run(t, tc.args...)
			if r.exit != 0 {
				t.Fatalf("exit %d, stdout %s", r.exit, r.stdout)
			}
			fc.mu.Lock()
			got := append([]control.PressRequest(nil), fc.press...)
			fc.mu.Unlock()
			if len(got) != 1 || got[0].HoldMS != tc.want {
				t.Fatalf("hold requests %v, want one of %d ms", got, tc.want)
			}
			var res control.PressResult
			if err := json.Unmarshal([]byte(r.stdout), &res); err != nil || res.Button != tc.args[1] || res.Action != "press" || res.HoldMS != tc.want {
				t.Fatalf("result %s (%v)", r.stdout, err)
			}
		})
	}
}

// An explicit --hold 0s is not silently replaced by a preset: the server rejects it.
func TestPressHoldZeroIsRejected(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	r := run(t, "press", "usr1", "--hold", "0s", "-i", "fan")
	if r.exit != 1 || r.errCode(t) != "invalid_argument" || len(fc.press) != 0 {
		t.Fatalf("exit %d, stdout %s, presses %v, want exit 1 invalid_argument and no press", r.exit, r.stdout, fc.press)
	}
}

func TestPressInstanceFromEnvAndSingleLive(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	if r := run(t, "press", "usr1"); r.exit != 0 {
		t.Fatalf("single live instance: exit %d, %s", r.exit, r.stdout)
	}
	t.Setenv(envInstance, "fan")
	if r := run(t, "press", "usr1", "--long"); r.exit != 0 {
		t.Fatalf("SIL_INSTANCE: exit %d, %s", r.exit, r.stdout)
	}
	if len(fc.press) != 2 || fc.press[0].HoldMS != 200 || fc.press[1].HoldMS != 1500 {
		t.Fatalf("hold requests %v, want 200 then 1500", fc.press)
	}
}

func TestPressDownUp(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	r := run(t, "press", "usr1", "--down", "-i", "fan")
	if r.exit != 0 || r.object(t)["action"] != "down" || r.object(t)["level"] != true {
		t.Fatalf("--down: exit %d, %s", r.exit, r.stdout)
	}
	r = run(t, "press", "usr1", "--up", "-i", "fan")
	if r.exit != 0 || r.object(t)["action"] != "up" || r.object(t)["level"] != false {
		t.Fatalf("--up: exit %d, %s", r.exit, r.stdout)
	}
	if len(fc.press) != 0 || len(fc.edges) != 2 || !fc.edges[0] || fc.edges[1] {
		t.Fatalf("presses %v, edges %v, want no press and edges [true false]", fc.press, fc.edges)
	}
}

func TestPressModesAreExclusive(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	for _, args := range [][]string{
		{"press", "usr1", "--long", "--hold", "1s", "-i", "fan"},
		{"press", "usr1", "--down", "--up", "-i", "fan"},
		{"press", "usr1", "--long", "--down", "-i", "fan"},
		{"press", "usr1", "--hold", "1s", "--up", "-i", "fan"},
	} {
		r := run(t, args...)
		if r.exit != 1 || r.errCode(t) != "invalid_argument" {
			t.Fatalf("sil %v: exit %d, stdout %s, want exit 1 invalid_argument", args, r.exit, r.stdout)
		}
	}
	if len(fc.seen()) != 0 {
		t.Fatalf("a parse error reached the control API: %v", fc.seen())
	}
}

func TestPressErrorsKeepTheirCode(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan"), err: apperr.Failed("button_busy", "held", map[string]any{"button": "usr1"})}
	liveInstance(t, "fan", fc)
	r := run(t, "press", "usr1", "-i", "fan")
	if r.exit != 1 || r.errCode(t) != "button_busy" || r.detail(t)["button"] != "usr1" {
		t.Fatalf("exit %d, stdout %s, want exit 1 button_busy with detail.button usr1", r.exit, r.stdout)
	}
}

// Misuse of `sil set` fails before the control API is asked.
func TestSetMisuse(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	for _, args := range [][]string{
		{"set", "-i", "fan"},
		{"set", "temperature=", "-i", "fan"},
		{"set", "=5", "-i", "fan"},
		{"set", "temperature", "-i", "fan"},
		{"set", "temperature=1", "temperature=2", "-i", "fan"},
		{"set", "temperature=1", "humidity", "-i", "fan"},
	} {
		r := run(t, args...)
		if r.exit != 1 || r.errCode(t) != "invalid_argument" {
			t.Errorf("sil %v: exit %d, stdout %s, want exit 1 invalid_argument", args, r.exit, r.stdout)
		}
	}
	if len(fc.sets) != 0 {
		t.Fatalf("misuse reached the control API: %v", fc.sets)
	}
}

// The values travel as JSON strings exactly as typed, split at the first '='.
func TestSetSendsPairsAsTyped(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan")}
	liveInstance(t, "fan", fc)
	r := run(t, "set", "humidity=40", "temperature=24.50", "-i", "fan")
	if r.exit != 0 {
		t.Fatalf("exit %d, stdout %s", r.exit, r.stdout)
	}
	if len(fc.sets) != 1 {
		t.Fatalf("set requests %v, want one", fc.sets)
	}
	got := map[string]string{}
	for k, raw := range fc.sets[0].Values {
		got[k] = string(raw)
	}
	if fmt.Sprint(got) != `map[humidity:"40" temperature:"24.50"]` {
		t.Fatalf("values %v, want the strings \"40\" and \"24.50\"", got)
	}
	if res := r.object(t); res["applied"] == nil || len(res) != 1 {
		t.Fatalf("result %s, want exactly {applied: ...}", r.stdout)
	}
	// A value with its own '=' is split at the first one and left to the server to judge.
	if r := run(t, "set", "temperature=1=2", "-i", "fan"); r.exit != 0 || string(fc.sets[1].Values["temperature"]) != `"1=2"` {
		t.Fatalf("temperature=1=2: exit %d, values %v", r.exit, fc.sets[1].Values)
	}
}

func TestSetInstanceSelection(t *testing.T) {
	cliEnv(t)
	own := &fakeControl{status: readyStatus("fan")}
	other := &fakeControl{status: readyStatus("other")}
	liveInstance(t, "fan", own)
	liveInstance(t, "other", other)
	if r := run(t, "set", "temperature=20", "-i", "other"); r.exit != 0 {
		t.Fatalf("-i other: exit %d, %s", r.exit, r.stdout)
	}
	if len(other.sets) != 1 || len(own.sets) != 0 {
		t.Fatalf("sets: other %d, fan %d, want 1 and 0", len(other.sets), len(own.sets))
	}
	if r := run(t, "set", "temperature=20"); r.exit != 1 || r.errCode(t) != "instance_ambiguous" {
		t.Fatalf("two live instances without -i: exit %d, %s, want instance_ambiguous", r.exit, r.stdout)
	}
}

// A server-side error (unknown key, range) reaches stdout unchanged with exit 1.
func TestSetServerErrorPassesThrough(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan"), err: apperr.Failed("value_out_of_range", "humidity=101 is outside 0..100", map[string]any{"key": "humidity"})}
	liveInstance(t, "fan", fc)
	r := run(t, "set", "humidity=101", "-i", "fan")
	if r.exit != 1 || r.errCode(t) != "value_out_of_range" || r.detail(t)["key"] != "humidity" {
		t.Fatalf("exit %d, stdout %s, want exit 1 value_out_of_range naming humidity", r.exit, r.stdout)
	}
}

// `sil fans` prints the instance's fan library as one JSON value, in the order the server gave it.
func TestFansPrintsTheLibrary(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{status: readyStatus("fan"), fans: control.FansResult{Fans: []control.FanInfo{
		{ID: "arctic-p12-pwm-pst", Name: "Arctic P12 PWM PST", MinRPM: 200, MaxRPM: 1800, BelowMin: "stop"},
		{ID: "noctua-nf-a12x25-pwm", Name: "Noctua NF-A12x25 PWM", MinRPM: 450, MaxRPM: 2000, BelowMin: "hold"},
	}}}
	liveInstance(t, "fan", fc)
	r := run(t, "fans", "-i", "fan")
	if r.exit != 0 {
		t.Fatalf("exit %d, stdout %s", r.exit, r.stdout)
	}
	if got := fc.seen(); len(got) != 1 || got[0] != "fans" {
		t.Fatalf("control calls %v, want one fans call", got)
	}
	res := r.object(t)
	list, _ := res["fans"].([]any)
	if len(res) != 1 || len(list) != 2 {
		t.Fatalf("result %s, want exactly {fans: [two entries]}", r.stdout)
	}
	second, _ := list[1].(map[string]any)
	for k, want := range map[string]any{"id": "noctua-nf-a12x25-pwm", "name": "Noctua NF-A12x25 PWM", "min_rpm": 450.0, "max_rpm": 2000.0, "below_min": "hold"} {
		if second[k] != want {
			t.Errorf("fans[1].%s = %v, want %v", k, second[k], want)
		}
	}
	if len(second) != 5 {
		t.Errorf("fans[1] = %v, want exactly id, name, min_rpm, max_rpm, below_min", second)
	}
}

func TestFansInstanceSelection(t *testing.T) {
	cliEnv(t)
	own := &fakeControl{status: readyStatus("fan")}
	other := &fakeControl{status: readyStatus("other")}
	liveInstance(t, "fan", own)
	liveInstance(t, "other", other)
	if r := run(t, "fans", "-i", "other"); r.exit != 0 {
		t.Fatalf("-i other: exit %d, %s", r.exit, r.stdout)
	}
	if got := other.seen(); len(got) != 1 || len(own.seen()) != 0 {
		t.Fatalf("calls: other %v, fan %v, want one on other and none on fan", got, own.seen())
	}
	if r := run(t, "fans"); r.exit != 1 || r.errCode(t) != "instance_ambiguous" {
		t.Fatalf("two live instances without -i: exit %d, %s, want instance_ambiguous", r.exit, r.stdout)
	}
}

// `sil up --fan ID` reaches lifecycle.UpOptions.Fan; without it the field stays empty (the default fan).
func TestUpFanFlagReachesUpOptions(t *testing.T) {
	parse := func(args ...string) UpCmd {
		t.Helper()
		var c CLI
		parser, err := kong.New(&c, kong.Name("sil"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		return c.Up
	}
	env := &runEnv{quiet: true}
	with := parse("up", "x", "--fan", "noctua-nf-a12x25-pwm")
	if got := with.options(env); got.Fan != "noctua-nf-a12x25-pwm" || got.Name != "x" {
		t.Fatalf("options with --fan = %+v, want Fan noctua-nf-a12x25-pwm", got)
	}
	without := parse("up", "x")
	if got := without.options(env); got.Fan != "" {
		t.Fatalf("options without --fan = %+v, want an empty Fan (the default fan)", got)
	}
}

// The up help names --fan and the portN.fan way to change one port later.
func TestUpHelpMentionsFan(t *testing.T) {
	var so, se bytes.Buffer
	_ = Execute([]string{"up", "--help"}, &so, &se)
	if !strings.Contains(se.String(), "--fan") || !strings.Contains(se.String(), "port") {
		t.Fatalf("up --help = %q, want --fan documented", se.String())
	}
}

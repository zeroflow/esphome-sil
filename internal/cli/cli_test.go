package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	mu     sync.Mutex
	calls  []string
	sim    []control.SimRestartRequest
	status control.Status
	err    error
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

// supervisorProc starts a process whose cmdline carries `serve <name>`, which
// is what instance.ProcessAlive looks for, and registers its removal.
func supervisorProc(t *testing.T, name string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 120 & wait", "serve", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the stand-in supervisor: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
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

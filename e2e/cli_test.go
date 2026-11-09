//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cliCandidates is error.detail.candidates of an instance_ambiguous answer.
func cliCandidates(t *testing.T, out map[string]any) []string {
	t.Helper()
	raw, ok := jget(out, "error", "detail", "candidates").([]any)
	if !ok {
		t.Fatalf("no error.detail.candidates list in %v", out)
	}
	names := []string{}
	for _, v := range raw {
		s, _ := v.(string)
		names = append(names, s)
	}
	return names
}

// cliServePID is the supervisor PID from the run.json of an instance.
func cliServePID(t *testing.T, env *Env, name string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(env.Runtime, "sil", name, "run.json"))
	if err != nil {
		t.Fatalf("run.json of %s: %v", name, err)
	}
	var rf struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil || rf.PID <= 0 {
		t.Fatalf("run.json of %s carries no pid: %s (%v)", name, raw, err)
	}
	return rf.PID
}

// cliStatus runs `sil status args...` and fails the test unless the answer has
// the given exit code; every call is also checked by Env.Sil to print exactly
// one JSON object.
func cliStatus(t *testing.T, env *Env, wantExit int, args ...string) map[string]any {
	t.Helper()
	out, exit, stderr := env.Sil(t, append([]string{"status"}, args...)...)
	if exit != wantExit {
		t.Fatalf("sil status %v: exit %d, want %d: %v\nstderr: %s", args, exit, wantExit, out, stderr)
	}
	return out
}

// TestCLIContract proves the consumer-facing CLI contract on real instances:
// name resolution (positional, SIL_INSTANCE, single live instance), exactly one
// JSON value on stdout, --quiet and the exit class of a dead supervisor.
func TestCLIContract(t *testing.T) {
	env := newEnv(t)

	// No live instance: nothing to resolve.
	out := cliStatus(t, env, 1)
	if jget(out, "error", "code") != "instance_ambiguous" {
		t.Fatalf("status without any instance: %v", out)
	}
	if got := cliCandidates(t, out); len(got) != 0 {
		t.Fatalf("candidates %v, want none", got)
	}

	env.Up(t, "e2e-cli-a")
	env.Up(t, "e2e-cli-b")

	// Two live instances: listed sorted, and nothing is guessed.
	out = cliStatus(t, env, 1)
	if jget(out, "error", "code") != "instance_ambiguous" {
		t.Fatalf("status with two instances: %v", out)
	}
	if got, want := cliCandidates(t, out), []string{"e2e-cli-a", "e2e-cli-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates %v, want %v", got, want)
	}
	for _, cmd := range [][]string{{"fw", "restart"}, {"sim", "restart"}} {
		out, exit, _ := env.Sil(t, cmd...)
		if exit != 1 || jget(out, "error", "code") != "instance_ambiguous" {
			t.Fatalf("sil %v with two instances: exit %d: %v", cmd, exit, out)
		}
	}

	// SIL_INSTANCE picks one; the positional name still wins over it.
	env.Extra = []string{"SIL_INSTANCE=e2e-cli-a"}
	if got := jget(cliStatus(t, env, 0), "instance"); got != "e2e-cli-a" {
		t.Fatalf("SIL_INSTANCE=e2e-cli-a answered for %v", got)
	}
	if got := jget(cliStatus(t, env, 0, "e2e-cli-b"), "instance"); got != "e2e-cli-b" {
		t.Fatalf("positional e2e-cli-b under SIL_INSTANCE=e2e-cli-a answered for %v", got)
	}
	env.Extra = nil

	// With b down, the single live instance is used.
	if down, exit, stderr := env.Sil(t, "down", "e2e-cli-b"); exit != 0 || down["stopped"] != true {
		t.Fatalf("sil down e2e-cli-b: exit %d: %v\nstderr: %s", exit, down, stderr)
	}
	if got := jget(cliStatus(t, env, 0), "instance"); got != "e2e-cli-a" {
		t.Fatalf("plain status after downing b answered for %v, want e2e-cli-a", got)
	}

	// --quiet: nothing on stderr on success.
	t.Cleanup(func() { env.run(t, "down", "e2e-cli-c") })
	port := freePort(t)
	up, exit, stderr := env.Sil(t, "up", "e2e-cli-c", "--api-port", fmt.Sprint(port), "--quiet")
	if exit != 0 || stderr != "" {
		t.Fatalf("sil up --quiet: exit %d, stderr %q: %v", exit, stderr, up)
	}
	if jget(up, "instance") != "e2e-cli-c" {
		t.Fatalf("sil up --quiet answered %v", up)
	}
	if _, stderr, exit := env.run(t, "status", "e2e-cli-c", "--quiet"); exit != 0 || stderr != "" {
		t.Fatalf("sil status --quiet: exit %d, stderr %q", exit, stderr)
	}

	// An unknown flag is still one JSON value.
	if bad, exit, _ := env.Sil(t, "status", "--bogus"); exit != 1 || jget(bad, "error", "code") != "invalid_argument" {
		t.Fatalf("unknown flag: exit %d: %v", exit, bad)
	}

	// A supervisor that died hard leaves its run.json behind: not_running, phase dead.
	pid := cliServePID(t, env, "e2e-cli-a")
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 %d: %v", pid, err)
	}
	waitFor(t, 10*time.Second, "the killed supervisor to be gone", func() bool { return pidGone(pid) })
	out = cliStatus(t, env, 1, "e2e-cli-a")
	if jget(out, "error", "code") != "not_running" || jget(out, "error", "detail", "phase") != "dead" {
		t.Fatalf("status after kill -9: %v", out)
	}
	if runDir, _ := jget(out, "error", "detail", "run_dir").(string); !strings.HasSuffix(runDir, filepath.Join("sil", "e2e-cli-a")) {
		t.Fatalf("detail.run_dir %q does not name the instance run dir", runDir)
	}
	if _, err := os.Stat(filepath.Join(env.Runtime, "sil", "e2e-cli-a", "run.json")); err != nil {
		t.Fatalf("expected the dead supervisor's run.json to remain: %v", err)
	}
	// The dead instance no longer counts as live: c is the single live one.
	if got := jget(cliStatus(t, env, 0), "instance"); got != "e2e-cli-c" {
		t.Fatalf("plain status with a dead a answered for %v, want e2e-cli-c", got)
	}
}

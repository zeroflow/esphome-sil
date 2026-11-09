//go:build sil_e2e

// Package e2e holds docker-backed end-to-end tests. Helpers shared by all
// e2e files live here; helpers local to one test file are unexported and
// prefixed with that file's stem so files added in the same wave never
// declare the same symbol.
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/zeroflow/esphome-sil/internal/fwbuild"
)

// sysClock is the one clock of the e2e package (factor 1).
var sysClock = clock.NewReal()

// TestMain fails fast, with a clear message, when docker or the pinned image
// is missing. The tests never pull the image.
func TestMain(m *testing.M) {
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: docker daemon is not reachable by this user: %v\n%s\n", err, out)
		os.Exit(2)
	}
	if out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", fwbuild.Image).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: pinned image %s is not present locally (the tests never pull it): %v\n%s\n", fwbuild.Image, err, out)
		os.Exit(2)
	}
	binDir, err := os.MkdirTemp("", "sil-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: temp dir for the sil binary: %v\n", err)
		os.Exit(2)
	}
	if err := buildSil(ctx, binDir); err != nil {
		_ = os.RemoveAll(binDir)
		fmt.Fprintf(os.Stderr, "e2e: building cmd/sil failed: %v\n", err)
		os.Exit(2)
	}
	code := m.Run()
	_ = os.RemoveAll(binDir)
	os.Exit(code)
}

// silBin is the sil binary built by TestMain.
var silBin string

// buildSil compiles ./cmd/sil into dir (go from PATH, which the Makefile
// extends with ~/.local/bin).
func buildSil(ctx context.Context, dir string) error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go is not on PATH: %w", err)
	}
	silBin = filepath.Join(dir, "sil")
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", silBin, "./cmd/sil")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// findRepoRoot walks up from the test directory to the directory holding go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the test directory")
		}
		dir = parent
	}
}

// repoRoot is findRepoRoot for tests.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return dir
}

// cacheRoot is the e2e cache root: SIL_E2E_CACHE when set, else
// $HOME/.cache/sil-e2e/<first 12 hex chars of sha256 of the absolute repo
// root>. One root per worktree, so parallel executors never share a build
// dir or toolcache. Created 0700.
func cacheRoot(t testing.TB) string {
	t.Helper()
	root := os.Getenv("SIL_E2E_CACHE")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("home dir: %v", err)
		}
		sum := sha256.Sum256([]byte(repoRoot(t)))
		root = filepath.Join(home, ".cache", "sil-e2e", hex.EncodeToString(sum[:])[:12])
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create cache root: %v", err)
	}
	return root
}

// fwIdentity is the compiled-in identity of one test firmware.
type fwIdentity struct {
	Name, MAC string
	Port      int
	Key       string
}

// newFWIdentity derives the MAC like instance.MAC will (01-03): "02:" plus
// the first 5 bytes of sha256("sil-mac:"+name). The port is free right now,
// the key is 32 random bytes in base64.
func newFWIdentity(t testing.TB, name string) fwIdentity {
	t.Helper()
	sum := sha256.Sum256([]byte("sil-mac:" + name))
	parts := []string{"02"}
	for _, b := range sum[:5] {
		parts = append(parts, fmt.Sprintf("%02x", b))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random key: %v", err)
	}
	return fwIdentity{
		Name: name,
		MAC:  strings.Join(parts, ":"),
		Port: freePort(t),
		Key:  base64.StdEncoding.EncodeToString(key),
	}
}

// buildFirmware compiles examples/fan-skeleton.yaml for id in the pinned
// image and returns the program path.
func buildFirmware(t testing.TB, id fwIdentity) string {
	t.Helper()
	root := repoRoot(t)
	logPath := filepath.Join(t.TempDir(), "build.log")
	start := sysClock.Now()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 25*time.Minute)
	defer cancel()
	res, err := fwbuild.Build(ctx, fwbuild.Options{
		Instance:   id.Name,
		ConfigPath: filepath.Join(root, "examples", "fan-skeleton.yaml"),
		RepoRoot:   root,
		CacheDir:   filepath.Join(cacheRoot(t), "sil", "build", id.Name),
		Substitutions: map[string]string{
			"sil_name": id.Name,
			"sil_mac":  id.MAC,
			"sil_port": fmt.Sprint(id.Port),
		},
		APIKey:  id.Key,
		LogPath: logPath,
		Progress: func(msg string) {
			t.Log(msg)
		},
	})
	if err != nil {
		if raw, rerr := os.ReadFile(logPath); rerr == nil {
			tail := strings.ReplaceAll(string(raw), id.Key, "<key>")
			if len(tail) > 6000 {
				tail = tail[len(tail)-6000:]
			}
			t.Logf("build log tail:\n%s", tail)
		}
		t.Fatalf("fwbuild.Build: %v", err)
	}
	t.Logf("firmware build took %s (%s)", (sysClock.Now() - start).Round(time.Second), res)
	return res.Program
}

// fwProc is a running firmware process.
type fwProc struct {
	PID     int
	LogPath string
	done    chan struct{}
}

// alive reports whether the process has not exited yet.
func (p *fwProc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// startFirmware runs program with ESPHOME_PREFDIR and SIL_BUS set, in its
// own process group, output to a temp log; the group is killed on cleanup.
func startFirmware(t testing.TB, program, busPath, prefsDir string) *fwProc {
	t.Helper()
	if err := os.MkdirAll(prefsDir, 0o700); err != nil {
		t.Fatalf("prefs dir: %v", err)
	}
	logf, err := os.CreateTemp("", "sil-fw-*.log")
	if err != nil {
		t.Fatalf("firmware log: %v", err)
	}
	cmd := exec.Command(program)
	cmd.Dir = prefsDir
	cmd.Env = append(os.Environ(), "ESPHOME_PREFDIR="+prefsDir, "SIL_BUS="+busPath)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		t.Fatalf("start firmware: %v", err)
	}
	p := &fwProc{PID: cmd.Process.Pid, LogPath: logf.Name(), done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-p.PID, syscall.SIGTERM)
		timer := sysClock.NewTimer(3 * time.Second)
		select {
		case <-p.done:
			timer.Stop()
		case <-timer.C():
			_ = syscall.Kill(-p.PID, syscall.SIGKILL)
			<-p.done
		}
		if t.Failed() {
			if raw, err := os.ReadFile(p.LogPath); err == nil {
				if len(raw) > 4000 {
					raw = raw[len(raw)-4000:]
				}
				t.Logf("firmware log tail:\n%s", raw)
			}
		}
		logf.Close()
		_ = os.Remove(p.LogPath)
	})
	return p
}

// shortTempDir returns a fresh directory under /tmp so socket paths stay far
// below the 107-byte sun_path limit.
func shortTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// freePort returns a TCP port that can be bound on 0.0.0.0 right now.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitFor polls cond every 50 ms on the real clock until it holds or timeout
// passes.
func waitFor(t testing.TB, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, timeout)
	defer cancel()
	for {
		if cond() {
			return
		}
		if err := sysClock.Sleep(ctx, 50*time.Millisecond); err != nil {
			if cond() {
				return
			}
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
	}
}

// runAPIClient runs e2e/api_client.py inside the pinned image against the
// firmware API on 127.0.0.1:port and returns its one JSON object and exit
// code. The key reaches the container only through the process environment.
func runAPIClient(t testing.TB, port int, key string, args ...string) (map[string]any, int) {
	t.Helper()
	root := repoRoot(t)
	dockerArgs := []string{
		"run", "--rm", "--network", "host",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp",
		"-e", "PYTHONDONTWRITEBYTECODE=1",
		"-e", "SIL_API_KEY",
		"-v", filepath.Join(root, "e2e") + ":/s:ro",
		"--entrypoint", "python3",
		fwbuild.Image,
		"/s/api_client.py", "--port", fmt.Sprint(port),
	}
	dockerArgs = append(dockerArgs, args...)
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	cmd.Env = append(os.Environ(), "SIL_API_KEY="+key)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run api client: %v", err)
		}
		code = ee.ExitCode()
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("api client printed no JSON object (exit %d): %v\nstdout: %s\nstderr: %s", code, err, stdout.String(), stderr.String())
	}
	return out, code
}

// busPeer plays the simulator side of the sim-bus: it listens on a Unix
// socket, follows the newest connection and decodes every line.
type busPeer struct {
	ln     net.Listener
	frames chan map[string]any

	mu   sync.Mutex
	conn net.Conn
}

// listenBus starts a bus peer on path.
func listenBus(t testing.TB, path string) *busPeer {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %s: %v", path, err)
	}
	p := &busPeer{ln: ln, frames: make(chan map[string]any, 4096)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.conn = c
			p.mu.Unlock()
			go func() {
				dec := json.NewDecoder(c)
				for {
					var m map[string]any
					if err := dec.Decode(&m); err != nil {
						return
					}
					select {
					case p.frames <- m:
					default:
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		p.mu.Lock()
		if p.conn != nil {
			_ = p.conn.Close()
		}
		p.mu.Unlock()
	})
	return p
}

// Next returns the first frame that match accepts, skipping frames that do
// not match; it fails the test when none arrives within timeout.
func (p *busPeer) Next(t testing.TB, timeout time.Duration, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	timer := sysClock.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case m := <-p.frames:
			if match == nil || match(m) {
				return m
			}
		case <-timer.C():
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
			return nil
		}
	}
}

// Send writes one line (a newline is appended) to the newest connection.
func (p *busPeer) Send(t testing.TB, line string) {
	t.Helper()
	waitFor(t, 5*time.Second, "a firmware connection to send to", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.conn != nil
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.conn.Write(append([]byte(line), '\n')); err != nil {
		t.Fatalf("bus send: %v", err)
	}
}

// Env is an isolated sil environment: its own XDG state, runtime and (per
// worktree) cache directories, driving the real sil binary built by TestMain.
type Env struct {
	State, Runtime, Cache, Repo, Bin string
	// Extra is appended (KEY=VALUE) to the environment of every sil call.
	Extra []string

	ports map[string]int
}

// newEnv returns a fresh Env. The runtime dir is short so socket paths stay
// far below the 107-byte sun_path limit; the cache is shared with the other
// e2e tests of this worktree so firmware builds are reused.
func newEnv(t *testing.T) *Env {
	t.Helper()
	return &Env{
		State:   t.TempDir(),
		Runtime: shortTempDir(t),
		Cache:   cacheRoot(t),
		Repo:    repoRoot(t),
		Bin:     silBin,
		ports:   map[string]int{},
	}
}

// environ is the process environment with the Env overrides applied.
func (e *Env) environ() []string {
	drop := map[string]bool{
		"XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true, "XDG_CACHE_HOME": true,
		"SIL_REPO": true, "SIL_INSTANCE": true, "SIL_API_KEY": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && drop[k] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"XDG_STATE_HOME="+e.State,
		"XDG_RUNTIME_DIR="+e.Runtime,
		"XDG_CACHE_HOME="+e.Cache,
		"SIL_REPO="+e.Repo,
	)
	return append(env, e.Extra...)
}

// run executes sil and returns its raw stdout, stderr and exit code.
func (e *Env) run(t *testing.T, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 25*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Bin, args...)
	cmd.Env = e.environ()
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run sil %v: %v", args, err)
		}
		exit = ee.ExitCode()
	}
	return so.String(), se.String(), exit
}

// Sil runs sil and decodes stdout, which must be exactly one JSON object.
func (e *Env) Sil(t *testing.T, args ...string) (out map[string]any, exit int, stderr string) {
	t.Helper()
	so, se, exit := e.run(t, args...)
	dec := json.NewDecoder(strings.NewReader(so))
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("sil %v printed no JSON object on stdout (exit %d): %v\nstdout: %q\nstderr: %s", args, exit, err, so, se)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("sil %v printed more than one JSON value on stdout: %q", args, so)
	}
	return out, exit, se
}

// Up runs `sil up name` with a free API port picked once per name and reused
// on later Ups of that name (down/up flows keep their build cache and HA
// entry), unless extra carries its own --api-port. It fails the test on a
// non-zero exit and registers a cleanup `sil down`.
func (e *Env) Up(t *testing.T, name string, extra ...string) map[string]any {
	t.Helper()
	hasPort := false
	for _, a := range extra {
		if a == "--api-port" || strings.HasPrefix(a, "--api-port=") {
			hasPort = true
		}
	}
	if !hasPort {
		port, ok := e.ports[name]
		if !ok {
			port = freePort(t)
			e.ports[name] = port
		}
		extra = append([]string{"--api-port", fmt.Sprint(port)}, extra...)
	}
	return e.UpAutoPort(t, name, extra...)
}

// UpAutoPort runs `sil up name extra...` without injecting a port (tests of
// sil's own allocation). It fails the test on a non-zero exit and registers a
// cleanup `sil down`.
func (e *Env) UpAutoPort(t *testing.T, name string, extra ...string) map[string]any {
	t.Helper()
	t.Cleanup(func() {
		so, se, code := e.run(t, "down", name)
		if code != 0 {
			t.Logf("cleanup sil down %s: exit %d\nstdout: %s\nstderr: %s", name, code, so, se)
		}
	})
	out, code, stderr := e.Sil(t, append([]string{"up", name}, extra...)...)
	if code != 0 {
		t.Fatalf("sil up %s: exit %d: %v\nstderr: %s", name, code, out, stderr)
	}
	return out
}

// Status returns `sil status name`, failing the test on a non-zero exit.
func (e *Env) Status(t *testing.T, name string) map[string]any {
	t.Helper()
	out, code, stderr := e.Sil(t, "status", name)
	if code != 0 {
		t.Fatalf("sil status %s: exit %d: %v\nstderr: %s", name, code, out, stderr)
	}
	return out
}

// WaitStatus polls `sil status` every 200 ms until pred holds and returns that
// status; it fails the test after timeout.
func (e *Env) WaitStatus(t *testing.T, name string, timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, timeout)
	defer cancel()
	for {
		st := e.Status(t, name)
		if pred(st) {
			return st
		}
		if err := sysClock.Sleep(ctx, 200*time.Millisecond); err != nil {
			t.Fatalf("timed out after %s waiting for a status condition; last status: %v", timeout, st)
		}
	}
}

// APIClient runs e2e/api_client.py against the firmware API on port.
func (e *Env) APIClient(t *testing.T, port int, key string, args ...string) (map[string]any, int) {
	t.Helper()
	return runAPIClient(t, port, key, args...)
}

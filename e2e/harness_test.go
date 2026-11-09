//go:build sil_e2e

// Package e2e holds docker-backed end-to-end tests. Helpers shared by all
// e2e files live here; helpers local to one test file are unexported and
// prefixed with that file's stem so files added in the same wave never
// declare the same symbol.
package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	os.Exit(m.Run())
}

// repoRoot walks up from the test directory to the directory holding go.mod.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the test directory")
		}
		dir = parent
	}
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

package instance

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// code returns the apperr code of err ("" for nil, "?" for a foreign error).
func code(err error) string {
	if err == nil {
		return ""
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return "?"
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "dev", "e2e-tracer", "a1", "e2e-iso-a", "e2e-iso-a-1", strings.Repeat("a", 27)} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "A", "Bad_Name", "a_b", "-a", "a-", "a/b", "..", "a b", "ä", strings.Repeat("a", 28)} {
		err := ValidateName(bad)
		if code(err) != "invalid_name" {
			t.Errorf("ValidateName(%q) = %v, want invalid_name", bad, err)
		}
		if ae := apperr.AsError(err); ae != nil && ae.Exit != apperr.ExitFailed {
			t.Errorf("ValidateName(%q) exit class %d, want 1", bad, ae.Exit)
		}
	}
	// The message names the rule that was broken.
	for name, want := range map[string]string{"": "empty", strings.Repeat("a", 28): "28 characters", "Ab": "upper case", "a-": "start and end"} {
		if err := ValidateName(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateName(%q) = %v, want message containing %q", name, err, want)
		}
	}
}

func TestPathsForRuntimeFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/s")
	fallback := fmt.Sprintf("/tmp/sil-%d", os.Getuid())
	for _, rt := range []struct {
		set bool
		val string
	}{{false, ""}, {true, ""}, {true, "relative/dir"}} {
		if rt.set {
			t.Setenv("XDG_RUNTIME_DIR", rt.val)
		} else {
			os.Unsetenv("XDG_RUNTIME_DIR")
		}
		p, err := PathsFor("a")
		if err != nil {
			t.Fatal(err)
		}
		if p.RunDir != filepath.Join(fallback, "a") {
			t.Errorf("XDG_RUNTIME_DIR=%q (set=%v): run dir %q, want %q", rt.val, rt.set, p.RunDir, filepath.Join(fallback, "a"))
		}
		if p.StateDir != "/s/sil/a" {
			t.Errorf("identity must stay in the state dir, got %q", p.StateDir)
		}
	}

	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1")
	t.Setenv("XDG_CACHE_HOME", "/c")
	a, _ := PathsFor("a")
	b, _ := PathsFor("a-1")
	for _, pair := range [][2]string{{a.RunDir, b.RunDir}, {a.StateDir, b.StateDir}, {a.CacheDir, b.CacheDir}} {
		if pair[0] == pair[1] || strings.HasPrefix(pair[1]+"/", pair[0]+"/") || strings.HasPrefix(pair[0]+"/", pair[1]+"/") {
			t.Errorf("names a and a-1 share or nest a directory: %q vs %q", pair[0], pair[1])
		}
	}
	if a.RunDir != "/run/user/1/sil/a" || a.StateRoot() != "/s" {
		t.Errorf("paths a = %+v root %q", a, a.StateRoot())
	}
}

func TestCheckSocketPaths(t *testing.T) {
	// "/bus.sock" and "/api.sock" are 9 bytes each.
	ok := Paths{RunDir: "/" + strings.Repeat("a", 98-1)} // 98 bytes + 9 = 107
	if len(ok.BusSock()) != 107 {
		t.Fatalf("setup: bus socket is %d bytes", len(ok.BusSock()))
	}
	if err := ok.CheckSocketPaths(); err != nil {
		t.Errorf("107-byte socket path: %v", err)
	}
	long := Paths{RunDir: ok.RunDir + "a"}
	err := long.CheckSocketPaths()
	if code(err) != "socket_path_too_long" || apperr.ExitCode(err) != apperr.ExitInfra || !strings.Contains(err.Error(), long.BusSock()) {
		t.Errorf("108-byte socket path: %v", err)
	}
	// Bytes, not runes: 49 two-byte runes + "/" = 99 bytes (> 98) but only 50 runes.
	multi := Paths{RunDir: "/" + strings.Repeat("é", 49)}
	if len([]rune(multi.BusSock())) > 107 || code(multi.CheckSocketPaths()) != "socket_path_too_long" {
		t.Errorf("multi-byte path (%d bytes, %d runes): %v", len(multi.BusSock()), len([]rune(multi.BusSock())), multi.CheckSocketPaths())
	}
}

// writeIdentity plants another instance's identity.json under root.
func writeIdentity(t *testing.T, root, name string, port int) {
	t.Helper()
	dir := filepath.Join(root, "sil", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	body := fmt.Sprintf(`{"schema":1,"name":%q,"api_port":%d,"key":%q}`, name, port, key)
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAllocatePort(t *testing.T) {
	root := t.TempDir()
	writeIdentity(t, root, "x", 6054)
	writeIdentity(t, root, "y", 6056)
	// A malformed identity claims nothing.
	if err := os.MkdirAll(filepath.Join(root, "sil", "broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sil", "broken", "identity.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := AllocatePort(root, "me", 0, func(p int) bool { return p != 6055 })
	if err != nil || got != 6057 {
		t.Errorf("claimed {6054,6056}, 6055 unbindable: got %d, %v; want 6057", got, err)
	}
	again, _ := AllocatePort(root, "me", 0, func(p int) bool { return p != 6055 })
	if again != got {
		t.Errorf("allocation is not deterministic: %d then %d", got, again)
	}

	if _, err := AllocatePort(root, "me", 6056, nil); code(err) != "port_claimed" || !strings.Contains(err.Error(), "y") {
		t.Errorf("requested claimed port: %v, want port_claimed naming y", err)
	}
	if got, err := AllocatePort(root, "y", 6056, nil); err != nil || got != 6056 {
		t.Errorf("an instance may re-request its own port: %d, %v", got, err)
	}
	for _, bad := range []int{80, 1023, 65536, -5} {
		if _, err := AllocatePort(root, "me", bad, nil); code(err) != "invalid_argument" {
			t.Errorf("requested %d: %v, want invalid_argument", bad, err)
		}
	}
	if got, err := AllocatePort(root, "me", 6060, nil); err != nil || got != 6060 {
		t.Errorf("requested free port: %d, %v", got, err)
	}
	// A stored port is sticky even when it is not bindable (TestBind reports it).
	if got, err := AllocatePort(root, "x", 0, func(int) bool { return false }); err != nil || got != 6054 {
		t.Errorf("sticky port: %d, %v, want 6054", got, err)
	}
	if _, err := AllocatePort(root, "me", 0, func(int) bool { return false }); code(err) != "no_free_port" || apperr.ExitCode(err) != apperr.ExitInfra {
		t.Errorf("nothing bindable: %v, want no_free_port", err)
	}
}

func TestLoadOrCreate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := PathsFor("dev")
	if err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreate(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id.APIPort < DefaultAPIPort || id.APIPort >= DefaultAPIPort+PortScanSpan || id.Name != "dev" {
		t.Fatalf("first identity = %+v", id)
	}
	if key, err := base64.StdEncoding.DecodeString(id.Key); err != nil || len(key) != 32 {
		t.Fatalf("key = %q", id.Key)
	}
	for path, want := range map[string]os.FileMode{p.StateDir: 0o700, p.IdentityJSON(): 0o600} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %v", path, fi, err, want)
		}
	}
	again, err := LoadOrCreate(p, 0)
	if err != nil || again != id {
		t.Fatalf("second load = %+v %v, want %+v", again, err, id)
	}

	moved, err := LoadOrCreate(p, 7000)
	if err != nil || moved.APIPort != 7000 || moved.Key != id.Key {
		t.Fatalf("--api-port override = %+v %v", moved, err)
	}
	kept, _ := LoadOrCreate(p, 0)
	if kept.APIPort != 7000 {
		t.Fatalf("override is not sticky: %+v", kept)
	}
	if _, err := LoadOrCreate(p, 80); code(err) != "invalid_argument" {
		t.Errorf("port 80: %v", err)
	}

	// Another instance cannot take the stored port.
	other, _ := PathsFor("other")
	if _, err := LoadOrCreate(other, 7000); code(err) != "port_claimed" || !strings.Contains(err.Error(), "dev") {
		t.Errorf("claimed port: %v", err)
	}
	oid, err := LoadOrCreate(other, 0)
	if err != nil || oid.APIPort == 7000 {
		t.Errorf("other instance got %+v, %v", oid, err)
	}
}

func TestLoadOrCreateCorruptIsNeverRegenerated(t *testing.T) {
	for name, body := range map[string]string{
		"short key":    `{"schema":1,"name":"dev","api_port":6054,"key":"abc"}`,
		"missing key":  `{"schema":1,"name":"dev","api_port":6054}`,
		"missing port": `{"schema":1,"name":"dev","key":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `"}`,
		"low port":     `{"schema":1,"name":"dev","api_port":80,"key":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `"}`,
		"not json":     `{`,
		"wrong name":   `{"schema":1,"name":"zzz","api_port":6054,"key":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			p, _ := PathsFor("dev")
			if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p.IdentityJSON(), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadOrCreate(p, 0)
			if code(err) != "identity_corrupt" || apperr.ExitCode(err) != apperr.ExitInfra || !strings.Contains(err.Error(), p.StateDir) {
				t.Fatalf("err = %v, want identity_corrupt with the reset hint", err)
			}
			if raw, _ := os.ReadFile(p.IdentityJSON()); string(raw) != body {
				t.Errorf("identity.json was rewritten: %q", raw)
			}
		})
	}
}

func TestConcurrentCreatesGetDistinctPorts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const n = 8
	ports := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _ := PathsFor(fmt.Sprintf("c%d", i))
			id, err := LoadOrCreate(p, 0)
			if err != nil {
				t.Errorf("c%d: %v", i, err)
				return
			}
			ports[i] = id.APIPort
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for i, port := range ports {
		if port == 0 || seen[port] {
			t.Fatalf("ports %v: duplicate or missing at %d", ports, i)
		}
		seen[port] = true
	}
}

func TestMAC(t *testing.T) {
	m := MAC("dev")
	if !strings.HasPrefix(m, "02:") || len(m) != 17 || m != strings.ToLower(m) {
		t.Fatalf("MAC(dev) = %q", m)
	}
	if MAC("dev") != m || MAC("other") == m || MAC("a") == MAC("a-1") {
		t.Fatalf("MAC must be stable per name and differ between names")
	}
	if !EqualMAC("02:AA:BB:CC:DD:01", "02aabbccdd01") || !EqualMAC(m, strings.ToUpper(m)) {
		t.Errorf("EqualMAC must ignore case and colons")
	}
	if EqualMAC("02:aa:bb:cc:dd:01", "02:aa:bb:cc:dd:02") || EqualMAC("", "") {
		t.Errorf("EqualMAC accepts different or empty addresses")
	}
}

func TestLocks(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, _ := PathsFor("dev")
	rel, err := LockInstance(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockInstance(p); code(err) != "instance_busy" || apperr.ExitCode(err) != apperr.ExitFailed {
		t.Errorf("second lock: %v, want instance_busy (exit 1)", err)
	}
	other, _ := PathsFor("other")
	if rel2, err := LockInstance(other); err != nil {
		t.Errorf("a different instance must not be blocked: %v", err)
	} else {
		rel2()
	}
	rel()
	rel3, err := LockInstance(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	rel3()

	root := p.StateRoot()
	a, err := LockAlloc(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		b, err := LockAlloc(root)
		if err == nil {
			b()
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second LockAlloc did not wait for the first")
	default:
	}
	a()
	<-done
}

func TestTestBindAndPortHolder(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	err = TestBind(port)
	ae := apperr.AsError(err)
	if code(err) != "port_in_use" || ae.Exit != apperr.ExitInfra || ae.Detail["port"] != port {
		t.Fatalf("TestBind on a held port = %v (%+v)", err, ae)
	}
	pid, cmd, ok := PortHolder(port)
	if !ok || pid != os.Getpid() || cmd == "" {
		t.Errorf("PortHolder = %d %q %v, want pid %d", pid, cmd, ok, os.Getpid())
	}
	if !strings.Contains(ae.Message, fmt.Sprint(port)) || !strings.Contains(ae.Message, fmt.Sprint(os.Getpid())) {
		t.Errorf("message does not name port and holder: %q", ae.Message)
	}
	ln.Close()
	if err := TestBind(port); err != nil {
		t.Errorf("TestBind after close: %v", err)
	}
	if _, _, ok := PortHolder(port); ok {
		t.Errorf("PortHolder found a holder for a free port")
	}
}

func TestProcessAlive(t *testing.T) {
	self := RunFile{PID: os.Getpid()}
	if ProcessAlive(self, "dev") {
		t.Errorf("the test binary is not `sil serve dev`")
	}
	if ProcessAlive(RunFile{PID: 0}, "dev") || ProcessAlive(RunFile{PID: 1 << 22}, "dev") {
		t.Errorf("dead pids are not alive")
	}
}

func TestResetRunDirRefusesUnsafeParent(t *testing.T) {
	base := t.TempDir()
	runtimeDir := filepath.Join(base, "rt")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	p, err := PathsFor("dev")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh: created 0700, prefs inside.
	if err := ResetRunDir(p); err != nil {
		t.Fatalf("fresh ResetRunDir: %v", err)
	}
	if fi, err := os.Stat(p.PrefsDir()); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("prefs dir: %v %v", fi, err)
	}
	// A marker in prefs/ is gone after the next reset (fresh preferences).
	marker := filepath.Join(p.PrefsDir(), "marker")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ResetRunDir(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("marker survived ResetRunDir: %v", err)
	}

	// Group/other-accessible parent: refused.
	parent := filepath.Dir(p.RunDir)
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ResetRunDir(p); code(err) != "unsafe_runtime_dir" || apperr.ExitCode(err) != apperr.ExitInfra {
		t.Fatalf("loose parent: err = %v, want unsafe_runtime_dir", err)
	}

	// Symlink parent: refused.
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, parent); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRunParent(p); code(err) != "unsafe_runtime_dir" {
		t.Fatalf("symlink parent: err = %v, want unsafe_runtime_dir", err)
	}
}

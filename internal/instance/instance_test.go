package instance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "dev", "e2e-tracer", "a1", strings.Repeat("a", 27)} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "Bad_Name", "-a", "a-", "a/b", "..", "a b", strings.Repeat("a", 28)} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) = nil, want invalid_name", bad)
		}
	}
}

func TestMAC(t *testing.T) {
	m := MAC("dev")
	if !strings.HasPrefix(m, "02:") || len(m) != 17 || m != strings.ToLower(m) {
		t.Fatalf("MAC(dev) = %q", m)
	}
	if MAC("dev") != m || MAC("other") == m {
		t.Fatalf("MAC must be stable per name and differ between names")
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
	if id.APIPort != DefaultAPIPort || id.Key == "" {
		t.Fatalf("first identity = %+v", id)
	}
	for path, want := range map[string]os.FileMode{p.StateDir: 0o700, p.IdentityJSON(): 0o600} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v err %v, want %v", path, fi.Mode().Perm(), err, want)
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

	// Group/other-accessible parent: refused.
	parent := filepath.Dir(p.RunDir)
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ResetRunDir(p); err == nil || !strings.Contains(err.Error(), "run_dir_unsafe") {
		t.Fatalf("loose parent: err = %v, want run_dir_unsafe", err)
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
	if err := ResetRunDir(p); err == nil || !strings.Contains(err.Error(), "run_dir_unsafe") {
		t.Fatalf("symlink parent: err = %v, want run_dir_unsafe", err)
	}
}

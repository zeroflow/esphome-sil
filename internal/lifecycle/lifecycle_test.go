package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// TestResolveConfigRefusesBroadMount: outside any checkout the YAML's own
// directory is the build mount, and a YAML sitting directly in the home
// directory must not mount all of it (T-01-14).
func TestResolveConfigRefusesBroadMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIL_REPO", t.TempDir()) // not a checkout: RepoRoot fails
	write := func(dir string) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := filepath.Join(dir, "fw.yaml")
		if err := os.WriteFile(cfg, []byte("x: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	_, _, err := resolveConfig(write(home))
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "config_invalid" {
		t.Fatalf("yaml in $HOME: err %v, want config_invalid", err)
	}
	sub := filepath.Join(home, "proj")
	cfg, root, err := resolveConfig(write(sub))
	if err != nil || root != sub || cfg != filepath.Join(sub, "fw.yaml") {
		t.Fatalf("yaml in a subdirectory: %q, %q, %v", cfg, root, err)
	}
}

// resolveFan: "" is the default; an id outside the library is unknown_fan with the sorted ids and is
// never handed to a file read; a library file that does not load is fan_toml_invalid.
func TestResolveFan(t *testing.T) {
	t.Setenv("SIL_FAN_DIR", "")
	if got, err := resolveFan(""); err != nil || got != "arctic-p12-pwm-pst" {
		t.Fatalf("resolveFan(\"\") = %q, %v, want the default fan", got, err)
	}
	if got, err := resolveFan("noctua-nf-a12x25-pwm"); err != nil || got != "noctua-nf-a12x25-pwm" {
		t.Fatalf("resolveFan(noctua) = %q, %v", got, err)
	}
	for _, id := range []string{"nope", "../fans/arctic-p12-pwm-pst", "/etc/passwd", "arctic-p12-pwm-pst.toml", " arctic-p12-pwm-pst"} {
		_, err := resolveFan(id)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "unknown_fan" || ae.Exit != apperr.ExitFailed || ae.Detail["fan"] != id {
			t.Errorf("resolveFan(%q) = %v, want unknown_fan naming it", id, err)
			continue
		}
		if known, _ := ae.Detail["known"].([]string); len(known) < 2 || known[0] != "arctic-p12-pwm-pst" {
			t.Errorf("resolveFan(%q): detail.known = %v, want the sorted library ids", id, ae.Detail["known"])
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("name = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIL_FAN_DIR", dir)
	_, err := resolveFan("broken")
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "fan_toml_invalid" {
		t.Fatalf("resolveFan(broken) = %v, want fan_toml_invalid", err)
	}
}

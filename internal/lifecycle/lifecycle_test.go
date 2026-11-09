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

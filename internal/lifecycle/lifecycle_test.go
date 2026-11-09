package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// checkWebOptions runs before anything is built or locked: each rule of --listen, --allow-host and
// --ui-dir is invalid_argument naming the offending value, and valid input is normalized.
func TestCheckWebOptions(t *testing.T) {
	code := func(err error) string {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return ae.Code
		}
		return ""
	}
	dirWithIndex := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirWithIndex, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexIsDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(indexIsDir, "index.html"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("defaults", func(t *testing.T) {
		o := UpOptions{}
		if err := checkWebOptions(&o); err != nil || o.Listen != "0.0.0.0" || len(o.AllowHosts) != 0 || o.UIDir != "" {
			t.Fatalf("defaults: %+v %v, want listen 0.0.0.0 and nothing else", o, err)
		}
	})
	t.Run("listen", func(t *testing.T) {
		for _, ok := range []string{"0.0.0.0", "127.0.0.1", "192.168.4.20", "::1", "::"} {
			o := UpOptions{Listen: ok}
			if err := checkWebOptions(&o); err != nil || o.Listen != ok {
				t.Errorf("--listen %q: %+v %v, want accepted unchanged", ok, o, err)
			}
		}
		for _, bad := range []string{"localhost", "bench.lan", "127.0.0.1:8054", "300.1.1.1", "[::1]", " 127.0.0.1"} {
			o := UpOptions{Listen: bad}
			err := checkWebOptions(&o)
			if code(err) != "invalid_argument" || !strings.Contains(err.Error(), bad) {
				t.Errorf("--listen %q: %v, want invalid_argument naming it", bad, err)
			}
		}
	})
	t.Run("allow_host", func(t *testing.T) {
		o := UpOptions{AllowHosts: []string{"Bench.LAN.", "bench.lan", "pi-4", "a1.b2.example"}}
		if err := checkWebOptions(&o); err != nil || !slices.Equal(o.AllowHosts, []string{"bench.lan", "pi-4", "a1.b2.example"}) {
			t.Fatalf("normalized hosts = %v %v, want lower case, no trailing dot, no duplicates", o.AllowHosts, err)
		}
		for _, bad := range []string{"bad_host!", "", "a b", "-x.lan", "x-.lan", "a..b", ".lan", "host:8054", "http://x.lan", "x/y"} {
			o := UpOptions{AllowHosts: []string{bad}}
			if err := checkWebOptions(&o); code(err) != "invalid_argument" {
				t.Errorf("--allow-host %q: %v, want invalid_argument", bad, err)
			}
		}
	})
	t.Run("ui_dir", func(t *testing.T) {
		o := UpOptions{UIDir: dirWithIndex}
		if err := checkWebOptions(&o); err != nil || o.UIDir != dirWithIndex {
			t.Fatalf("ui dir: %+v %v, want accepted and absolute", o, err)
		}
		t.Chdir(filepath.Dir(dirWithIndex))
		rel := UpOptions{UIDir: filepath.Base(dirWithIndex)}
		if err := checkWebOptions(&rel); err != nil || rel.UIDir != dirWithIndex {
			t.Fatalf("relative ui dir: %+v %v, want %s", rel, err, dirWithIndex)
		}
		for name, bad := range map[string]string{
			"a file":              file,
			"a dir without index": t.TempDir(),
			"missing":             filepath.Join(t.TempDir(), "nope"),
			"index.html is a dir": indexIsDir,
		} {
			o := UpOptions{UIDir: bad}
			err := checkWebOptions(&o)
			if code(err) != "invalid_argument" || !strings.Contains(err.Error(), "--ui-dir needs a directory with index.html") {
				t.Errorf("--ui-dir %s: %v, want invalid_argument", name, err)
			}
		}
	})
}

//go:build sil_e2e

package e2e

import (
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// buildRecordPath is <env cache>/sil/build/<name>/build/sil-build.json.
func buildRecordPath(env *Env, name string) string {
	return filepath.Join(env.Cache, "sil", "build", name, "build", "sil-build.json")
}

// buildUpAgain runs `sil up name` for a name that was already brought up with
// env.Up (same sticky API port) and returns stdout JSON and stderr.
func buildUpAgain(t *testing.T, env *Env, name string, extra ...string) (map[string]any, string) {
	t.Helper()
	args := append([]string{"up", name, "--api-port", fmt.Sprint(env.ports[name])}, extra...)
	t.Cleanup(func() { env.run(t, "down", name) })
	out, code, stderr := env.Sil(t, args...)
	if code != 0 {
		t.Fatalf("sil %v: exit %d: %v\nstderr: %s", args, code, out, stderr)
	}
	return out, stderr
}

// TestBuildCache: an unchanged instance skips the compile, --rebuild never does.
func TestBuildCache(t *testing.T) {
	const name = "e2e-build"
	env := newEnv(t)

	start := sysClock.Now()
	first := env.Up(t, name, "--rebuild")
	t.Logf("forced build took %s", (sysClock.Now() - start).Round(1e8))
	if jget(first, "build", "cached") != false {
		t.Fatalf("a --rebuild must not be cached: %v", first["build"])
	}
	if _, err := os.Stat(buildRecordPath(env, name)); err != nil {
		t.Fatalf("no sil-build.json after a successful build: %v", err)
	}
	if _, code, _ := env.Sil(t, "down", name); code != 0 {
		t.Fatalf("sil down: exit %d", code)
	}

	start = sysClock.Now()
	second, stderr := buildUpAgain(t, env, name)
	t.Logf("cache-hit up took %s", (sysClock.Now() - start).Round(1e8))
	if jget(second, "build", "cached") != true {
		t.Fatalf("second up of an unchanged instance must be cached: %v", second["build"])
	}
	if !strings.Contains(stderr, "build cache hit") {
		t.Fatalf("stderr lacks `build cache hit`:\n%s", stderr)
	}
	if jget(second, "build", "program") != jget(first, "build", "program") {
		t.Fatalf("cached build changed the program path: %v vs %v", second["build"], first["build"])
	}
	if _, code, _ := env.Sil(t, "down", name); code != 0 {
		t.Fatalf("sil down: exit %d", code)
	}

	third, stderr := buildUpAgain(t, env, name, "--rebuild")
	if jget(third, "build", "cached") != false || strings.Contains(stderr, "build cache hit") {
		t.Fatalf("--rebuild must rebuild: %v\nstderr: %s", third["build"], stderr)
	}
}

// TestBannedComponents: host-incompatible configs stop with a named error, the
// same one every time, and leave no build record behind.
func TestBannedComponents(t *testing.T) {
	env := newEnv(t)
	root := repoRoot(t)

	cases := []struct {
		name, file string
		codes      []string
		mentions   []string
	}{
		{"e2e-ban-wifi", "with-wifi-ota.yaml", []string{"lint_rejected"}, []string{"wifi", "ota"}},
		{"e2e-ban-ledc", "with-ledc.yaml", []string{"config_invalid", "lint_rejected"}, []string{"ledc"}},
		{"e2e-ban-i2c", "with-i2c.yaml", []string{"config_invalid", "lint_rejected"}, []string{"i2c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := filepath.Join(root, "e2e", "testdata", tc.file)
			var runs []map[string]any
			for i := 0; i < 2; i++ {
				out, code, stderr := env.Sil(t, "up", tc.name, "--config", cfg)
				if code != 1 {
					t.Fatalf("run %d: exit %d, want 1: %v\nstderr: %s", i, code, out, stderr)
				}
				runs = append(runs, out)
			}
			if !reflect.DeepEqual(runs[0], runs[1]) {
				t.Fatalf("the same rejected config produced different errors:\n%v\n%v", runs[0], runs[1])
			}
			e, _ := runs[0]["error"].(map[string]any)
			code, _ := e["code"].(string)
			msg, _ := e["message"].(string)
			okCode := false
			for _, c := range tc.codes {
				okCode = okCode || code == c
			}
			if !okCode {
				t.Fatalf("error code %q, want one of %v: %v", code, tc.codes, e)
			}
			for _, m := range tc.mentions {
				if !strings.Contains(msg, m) {
					t.Errorf("message does not name %q: %s", m, msg)
				}
			}
			if tc.file == "with-wifi-ota.yaml" {
				var seen []string
				problems, _ := jget(e, "detail", "problems").([]any)
				for _, p := range problems {
					pm, _ := p.(map[string]any)
					seen = append(seen, fmt.Sprint(pm["component"]))
					if pm["file"] != cfg {
						t.Errorf("problem %v does not name the config file %s", pm, cfg)
					}
					if pm["hint"] == "" || pm["hint"] == nil {
						t.Errorf("problem %v has no hint", pm)
					}
				}
				for _, want := range tc.mentions {
					found := false
					for _, s := range seen {
						found = found || s == want
					}
					if !found {
						t.Errorf("problems %v lack %s", seen, want)
					}
				}
			}
			if _, err := os.Stat(buildRecordPath(env, tc.name)); !os.IsNotExist(err) {
				t.Errorf("a rejected config must not leave sil-build.json (stat err: %v)", err)
			}
		})
	}
}

// buildGitState is `git status --porcelain --ignored` of the repo.
func buildGitState(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--ignored").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

// TestStaticAndClean: the program is static, nothing is root-owned and the
// working tree is untouched by a build.
func TestStaticAndClean(t *testing.T) {
	const name = "e2e-clean"
	env := newEnv(t)
	before := buildGitState(t, env.Repo)

	up := env.Up(t, name)
	program, _ := jget(up, "build", "program").(string)
	f, err := elf.Open(program)
	if err != nil {
		t.Fatalf("open program %q: %v", program, err)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Errorf("program has a PT_INTERP header (dynamically linked)")
		}
	}
	if f.Section(".interp") != nil {
		t.Errorf("program has an .interp section")
	}
	if libs, _ := f.ImportedLibraries(); len(libs) != 0 {
		t.Errorf("program needs shared libraries: %v", libs)
	}
	f.Close()

	uid := uint32(os.Getuid())
	for _, root := range []string{env.State, env.Runtime, env.Cache} {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // a socket or a file removed while walking
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uid {
				t.Errorf("%s is owned by uid %d, want %d", p, st.Uid, uid)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if _, code, _ := env.Sil(t, "down", name); code != 0 {
		t.Fatalf("sil down: exit %d", code)
	}
	if after := buildGitState(t, env.Repo); after != before {
		t.Errorf("git status changed by the build:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestOfflineSeeded: once the library archives are in the toolcache, a new
// instance compiles with --network none.
func TestOfflineSeeded(t *testing.T) {
	env := newEnv(t)
	seed := filepath.Join(env.Cache, "sil", "toolcache", "pio_components")
	seedHTTP := filepath.Join(env.Cache, "sil", "toolcache", "pio_http")
	_, errComponents := os.Stat(seed)
	_, errHTTP := os.Stat(seedHTTP)
	if errComponents != nil || errHTTP != nil {
		const seedName = "e2e-offline-seed"
		env.Up(t, seedName)
		if _, code, _ := env.Sil(t, "down", seedName); code != 0 {
			t.Fatalf("sil down %s: exit %d", seedName, code)
		}
		for _, d := range []string{seed, seedHTTP} {
			if _, err := os.Stat(d); err != nil {
				t.Fatalf("the online build did not write the toolcache back: %v", err)
			}
		}
	}

	const name = "e2e-offline"
	start := sysClock.Now()
	up := env.Up(t, name)
	t.Logf("offline build of a new instance took %s", (sysClock.Now() - start).Round(1e8))
	runDir, _ := up["run_dir"].(string)
	raw, err := os.ReadFile(filepath.Join(runDir, "build.log"))
	if err != nil {
		t.Fatalf("read build.log: %v", err)
	}
	log := string(raw)
	if !strings.Contains(log, "network=none") {
		t.Fatalf("build.log does not record an offline compile:\n%s", log)
	}
	if strings.Contains(log, "sil: compile network=bridge") || strings.Contains(log, "Downloading") {
		t.Fatalf("a seeded build downloaded something:\n%s", log)
	}
	if jget(up, "build", "cached") != false {
		t.Fatalf("a new instance cannot be a cache hit: %v", up["build"])
	}
}

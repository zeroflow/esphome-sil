package fwbuild

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// pipeKey is a dummy API key; the stub docker echoes it so the tests can see
// whether it is redacted.
const pipeKey = "DUMMYKEYDUMMYKEYDUMMYKEYDUMMYKEYDUMMYKEY0="

// pipeStubDocker installs a fake `docker` first on PATH. It records every
// invocation (one line of arguments) into the returned log file. FAKE_MODE
// selects the behaviour: no_daemon, no_image, config_fail.
func pipeStubDocker(t *testing.T, mode string) (argsLog string) {
	t.Helper()
	bin := t.TempDir()
	argsLog = filepath.Join(t.TempDir(), "args.log")
	script := `#!/bin/sh
echo "$@" >> "$FAKE_ARGS_LOG"
case "$1 $2" in
  "version --format")
    [ "$FAKE_MODE" = no_daemon ] && { echo "Cannot connect to the Docker daemon" >&2; exit 1; }
    echo 27.0.0; exit 0 ;;
  "image inspect")
    [ "$FAKE_MODE" = no_image ] && { echo "No such image" >&2; exit 1; }
    echo sha256:fake; exit 0 ;;
  "run --rm")
    if [ "$FAKE_MODE" = config_fail ]; then
      echo "Creating cache directory /cache/platformio"
      echo "You can change this behavior by mounting a directory to the container's /cache directory."
      echo "ERROR Invalid key $SIL_API_KEY in config" >&2
      echo "Failed config" >&2
      exit 2
    fi
    exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_MODE", mode)
	t.Setenv("FAKE_ARGS_LOG", argsLog)
	return argsLog
}

func pipeOptions(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "examples", "x.yaml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("esphome:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	return Options{
		Instance:   "dev",
		ConfigPath: cfg,
		RepoRoot:   root,
		CacheDir:   filepath.Join(cache, "sil", "build", "dev"),
		Substitutions: map[string]string{
			"sil_name": "dev", "sil_mac": "02:aa:bb:cc:dd:01", "sil_port": "6054",
		},
		APIKey:  pipeKey,
		LogPath: filepath.Join(t.TempDir(), "run", "build.log"),
	}
}

func pipeCode(t *testing.T, err error) (*apperr.Error, bool) {
	t.Helper()
	var ae *apperr.Error
	return ae, errors.As(err, &ae)
}

func TestBuildDockerUnavailable(t *testing.T) {
	argsLog := pipeStubDocker(t, "no_daemon")
	_, err := Build(t.Context(), pipeOptions(t))
	ae, ok := pipeCode(t, err)
	if !ok || ae.Code != "docker_unavailable" || ae.Exit != apperr.ExitInfra {
		t.Fatalf("want Infra docker_unavailable, got %v", err)
	}
	raw, _ := os.ReadFile(argsLog)
	if strings.Contains(string(raw), "image") || strings.Contains(string(raw), "pull") {
		t.Fatalf("nothing but `docker version` may run: %q", raw)
	}
}

func TestBuildImageMissingNeverPulls(t *testing.T) {
	argsLog := pipeStubDocker(t, "no_image")
	o := pipeOptions(t)
	_, err := Build(t.Context(), o)
	ae, ok := pipeCode(t, err)
	if !ok || ae.Code != "image_missing" || ae.Exit != apperr.ExitInfra {
		t.Fatalf("want Infra image_missing, got %v", err)
	}
	if !strings.Contains(ae.Message, "docker pull ghcr.io/esphome/esphome:2026.10.1") {
		t.Fatalf("message lacks the exact pull hint: %s", ae.Message)
	}
	raw, _ := os.ReadFile(argsLog)
	if strings.Contains(string(raw), "pull") || strings.Contains(string(raw), "run ") {
		t.Fatalf("sil must neither pull nor run anything: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(o.CacheDir, "build", "sil-build.json")); !os.IsNotExist(err) {
		t.Fatal("a failed preflight must not write a build record")
	}
}

func TestBuildConfigFailureIsRedactedAndRepeatable(t *testing.T) {
	argsLog := pipeStubDocker(t, "config_fail")
	o := pipeOptions(t)
	var msgs []string
	for i := 0; i < 2; i++ {
		_, err := Build(t.Context(), o)
		ae, ok := pipeCode(t, err)
		if !ok || ae.Code != "config_invalid" || ae.Exit != apperr.ExitFailed {
			t.Fatalf("run %d: want Failed config_invalid, got %v", i, err)
		}
		if strings.Contains(ae.Message, pipeKey) {
			t.Fatalf("API key leaked into the message: %s", ae.Message)
		}
		for _, want := range []string{o.ConfigPath, "<redacted>", "Failed config"} {
			if !strings.Contains(ae.Message, want) {
				t.Fatalf("message lacks %q: %s", want, ae.Message)
			}
		}
		if strings.Contains(ae.Message, "Creating cache directory") {
			t.Fatalf("entrypoint notice must be filtered out: %s", ae.Message)
		}
		msgs = append(msgs, ae.Message)
	}
	if msgs[0] != msgs[1] {
		t.Fatalf("same failure rendered differently:\n%s\n---\n%s", msgs[0], msgs[1])
	}
	if _, err := os.Stat(filepath.Join(o.CacheDir, "build", "sil-build.json")); !os.IsNotExist(err) {
		t.Fatal("a config failure must not write a build record")
	}
	raw, _ := os.ReadFile(argsLog)
	if strings.Contains(string(raw), pipeKey) {
		t.Fatalf("the key must never be on the docker command line: %q", raw)
	}
	if !strings.Contains(string(raw), "--network none") {
		t.Fatalf("the config stage must run with --network none: %q", raw)
	}
}

func TestDockerArgsSeparateInstances(t *testing.T) {
	t.Parallel()
	a, b := pipeOptions(t), pipeOptions(t)
	argsA := dockerArgs(a, a.RepoRoot, filepath.Join(a.CacheDir, "build"), filepath.Join(a.CacheDir, "cache"), "none", "config", "x.yaml")
	argsB := dockerArgs(b, b.RepoRoot, filepath.Join(b.CacheDir, "build"), filepath.Join(b.CacheDir, "cache"), "none", "config", "x.yaml")
	mounts := func(args []string) (build string) {
		for i, s := range args {
			if s == "-v" && strings.HasSuffix(args[i+1], ":/build") {
				return args[i+1]
			}
		}
		return ""
	}
	if mounts(argsA) == "" || mounts(argsA) == mounts(argsB) {
		t.Fatalf("instances share a /build mount: %q vs %q", mounts(argsA), mounts(argsB))
	}
	joined := strings.Join(argsA, " ")
	for _, want := range []string{"--name " + containerName(a), "--network none", "ESPHOME_DATA_DIR=/build/data", "-e SIL_API_KEY ", ":ro"} {
		if !strings.Contains(joined+" ", want) {
			t.Fatalf("docker args lack %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, pipeKey) {
		t.Fatal("the key is on the command line")
	}
}

// TestContainerNameIsScopedToTheCacheDir: the Docker daemon is host-wide, so
// two instances with the same name under different cache roots must not share
// a container name (the pre-run `docker rm -f` would kill the other's build).
func TestContainerNameIsScopedToTheCacheDir(t *testing.T) {
	t.Parallel()
	a, b := pipeOptions(t), pipeOptions(t)
	if a.Substitutions["sil_name"] != b.Substitutions["sil_name"] || a.CacheDir == b.CacheDir {
		t.Fatalf("fixture must give the same name and different cache dirs: %q/%q %q/%q",
			a.Substitutions["sil_name"], b.Substitutions["sil_name"], a.CacheDir, b.CacheDir)
	}
	na, nb := containerName(a), containerName(b)
	if na == nb {
		t.Fatalf("two cache dirs share the container name %q", na)
	}
	if !strings.HasPrefix(na, "sil-build-"+a.Substitutions["sil_name"]+"-") || na != containerName(a) {
		t.Fatalf("container name %q is not stable or lacks the instance name", na)
	}
}

func TestMountRoot(t *testing.T) {
	t.Parallel()
	o := pipeOptions(t)
	if got, err := mountRoot(o); err != nil || got != o.RepoRoot {
		t.Fatalf("config inside the repo: mount %q, %v; want %q", got, err, o.RepoRoot)
	}
	other := t.TempDir() // not a git checkout, not inside RepoRoot
	cfg := filepath.Join(other, "y.yaml")
	if err := os.WriteFile(cfg, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.ConfigPath = cfg
	if got, err := mountRoot(o); err != nil || got != other {
		t.Fatalf("config outside any checkout: mount %q, %v; want its directory %q", got, err, other)
	}
}

// TestMountRootRefusesBroadTrees: a YAML directly in the home directory or in
// "/" must not make the build mount the whole home directory or the root.
func TestMountRootRefusesBroadTrees(t *testing.T) {
	home := t.TempDir() // a non-git "home"
	t.Setenv("HOME", home)
	o := pipeOptions(t)
	for name, cfg := range map[string]string{
		"yaml in $HOME": filepath.Join(home, "y.yaml"),
		"yaml in /":     "/y.yaml",
	} {
		o.ConfigPath = cfg
		_, err := mountRoot(o)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "config_invalid" {
			t.Errorf("%s: err %v, want config_invalid", name, err)
		}
	}
	sub := filepath.Join(home, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	o.ConfigPath = filepath.Join(sub, "y.yaml")
	if got, err := mountRoot(o); err != nil || got != sub {
		t.Errorf("yaml in a subdirectory of $HOME: mount %q, %v; want %q", got, err, sub)
	}
}

// TestClassifyCompileFailure: only a compiler diagnostic with exit 1 is the
// user's firmware (compile_failed); toolchain, network, docker and kill
// failures stay build_failed.
func TestClassifyCompileFailure(t *testing.T) {
	t.Parallel()
	const (
		gccLog      = "INFO Compiling app...\nsrc/main.cpp:12:5: error: 'foo' was not declared in this scope\nFAILED: src/main.cpp.o\nninja: build stopped: subcommand failed.\n"
		fatalLog    = "/config/x.h:3:10: fatal error: nope.h: No such file or directory\n"
		linkLog     = "ld: main.o: in function `setup': undefined reference to `bar'\ncollect2: error: ld returned 1 exit status\n"
		ninjaLog    = "[1/9] c++ x.o\nFAILED: x.o\nc++ ...\n"
		makeLog     = "*** [.pioenvs/dev/src/main.cpp.o] Error 1\n"
		downloadLog = "Failed to download https://example/noise-c.tar.gz\nsrc/main.cpp:1:1: error: x\n"
		httpLog     = "HTTPClientError: connection reset\n"
		unknownPkg  = "UnknownPackageError: Could not find the package with 'esphome/noise-c' requirements\n"
		dnsLog      = "ConnectionError: Temporary failure in name resolution\n"
		diskLog     = "OSError: [Errno 28] No space left on device\n"
	)
	for _, tc := range []struct {
		name string
		exit int
		log  string
		want string
	}{
		{"gcc error", 1, gccLog, "compile_failed"},
		{"gcc fatal error", 1, fatalLog, "compile_failed"},
		{"link error", 1, linkLog, "compile_failed"},
		{"ninja failure marker", 1, ninjaLog, "compile_failed"},
		{"make failure marker", 1, makeLog, "compile_failed"},
		{"download failure wins over a diagnostic", 1, downloadLog, "build_failed"},
		{"http client error", 1, httpLog, "build_failed"},
		{"unknown package offline", 1, unknownPkg, "build_failed"},
		{"dns error", 1, dnsLog, "build_failed"},
		{"disk full", 1, diskLog, "build_failed"},
		{"empty log", 1, "", "build_failed"},
		{"docker cannot run (125)", 125, gccLog, "build_failed"},
		{"killed (137)", 137, gccLog, "build_failed"},
		{"no exit status", -1, gccLog, "build_failed"},
	} {
		if got := classifyCompileFailure(tc.exit, []byte(tc.log)); got != tc.want {
			t.Errorf("%s: exit %d => %q, want %q", tc.name, tc.exit, got, tc.want)
		}
	}
}

// WR-03: in the config and generate stages only ESPHome's own failure exits (1 and 2) are a user-fixable
// config_invalid; docker's own codes, an OOM-killed container and a signalled client are bench
// infrastructure (build_failed), consistent with the compile stage.
func TestStageFailureClassification(t *testing.T) {
	t.Parallel()
	exitErr := func(script string) error {
		err := exec.Command("sh", "-c", script).Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("%q did not fail with an ExitError: %v", script, err)
		}
		return err
	}
	b := &builder{o: Options{ConfigPath: "/y/dev.yaml", LogPath: "/y/build.log"}}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"esphome rejected the yaml (2)", exitErr("exit 2"), "config_invalid"},
		{"esphome failed (1)", exitErr("exit 1"), "config_invalid"},
		{"docker cannot run (125)", exitErr("exit 125"), "build_failed"},
		{"docker cannot exec (126)", exitErr("exit 126"), "build_failed"},
		{"docker not found (127)", exitErr("exit 127"), "build_failed"},
		{"oom killed (137)", exitErr("exit 137"), "build_failed"},
		{"other exit code (3)", exitErr("exit 3"), "build_failed"},
		{"killed by a signal (-1)", exitErr("kill -9 $$"), "build_failed"},
		{"not an exit error", errors.New("docker: not found"), "build_failed"},
	} {
		err := b.stageFailure(context.Background(), tc.err, "esphome config failed", "some output\n")
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != tc.want {
			t.Errorf("%s: got %v, want code %s", tc.name, err, tc.want)
		}
	}
}

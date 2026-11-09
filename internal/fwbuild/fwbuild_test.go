package fwbuild

import (
	"errors"
	"os"
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
	for _, want := range []string{"--network none", "ESPHOME_DATA_DIR=/build/data", "-e SIL_API_KEY ", ":ro"} {
		if !strings.Contains(joined+" ", want) {
			t.Fatalf("docker args lack %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, pipeKey) {
		t.Fatal("the key is on the command line")
	}
}

func TestMountRoot(t *testing.T) {
	t.Parallel()
	o := pipeOptions(t)
	if got := mountRoot(o); got != o.RepoRoot {
		t.Fatalf("config inside the repo: mount %q, want %q", got, o.RepoRoot)
	}
	other := t.TempDir() // not a git checkout, not inside RepoRoot
	cfg := filepath.Join(other, "y.yaml")
	if err := os.WriteFile(cfg, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.ConfigPath = cfg
	if got := mountRoot(o); got != other {
		t.Fatalf("config outside any checkout: mount %q, want its directory %q", got, other)
	}
}

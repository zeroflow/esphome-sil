// Package fwbuild compiles a host-platform ESPHome YAML inside the pinned
// ESPHome image and returns the path of the resulting static program.
//
// The package is named fwbuild, not build, because the root .gitignore
// ignores every directory called build/.
package fwbuild

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// Image is the ESPHome toolchain image, pinned by tag and digest. It is
// never pulled by sil or the tests.
const Image = "ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64"

// Options describes one firmware build.
type Options struct {
	// Instance is the sil instance name (informational; the firmware name is
	// taken from Substitutions["sil_name"]).
	Instance string
	// ConfigPath is the absolute path of the firmware YAML.
	ConfigPath string
	// RepoRoot is mounted read-only at its own absolute path.
	RepoRoot string
	// CacheDir is <cache root>/sil/build/<name>; it holds build/ and cache/.
	CacheDir string
	// Substitutions are passed as -s key value (sil_name, sil_mac, sil_port).
	Substitutions map[string]string
	// APIKey is the api.encryption.key; it reaches docker only via the
	// process environment.
	APIKey string
	// LogPath receives the combined docker output.
	LogPath string
	// Rebuild discards the previous build output of this instance first.
	Rebuild bool
	// Progress, when set, receives human-readable progress messages.
	Progress func(msg string)
}

// Result is the outcome of a build.
type Result struct {
	Program string `json:"program"`
	Cached  bool   `json:"cached"`
}

// Build compiles o.ConfigPath in the pinned image.
func Build(ctx context.Context, o Options) (Result, error) {
	if err := validate(o); err != nil {
		return Result{}, err
	}
	name := o.Substitutions["sil_name"]
	buildDir := filepath.Join(o.CacheDir, "build")
	cacheDir := filepath.Join(o.CacheDir, "cache")
	homeDir := filepath.Join(cacheDir, "home")
	for _, d := range []string{o.CacheDir, buildDir, cacheDir, homeDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Result{}, apperr.Infra("build_failed", "cannot create build directory", map[string]any{"path": d, "error": err.Error()})
		}
	}
	if o.Rebuild {
		if err := os.RemoveAll(filepath.Join(buildDir, "sil-"+name)); err != nil {
			return Result{}, apperr.Infra("build_failed", "cannot clear previous build", map[string]any{"error": err.Error()})
		}
	}

	if err := os.MkdirAll(filepath.Dir(o.LogPath), 0o700); err != nil {
		return Result{}, apperr.Infra("build_failed", "cannot create log directory", map[string]any{"error": err.Error()})
	}
	logf, err := os.OpenFile(o.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Result{}, apperr.Infra("build_failed", "cannot open build log", map[string]any{"path": o.LogPath, "error": err.Error()})
	}
	defer logf.Close()

	args := dockerArgs(o, buildDir, cacheDir)
	if o.Progress != nil {
		o.Progress("building firmware in " + Image)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(envWithout(os.Environ(), "SIL_API_KEY"), "SIL_API_KEY="+o.APIKey)
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, apperr.Infra("build_failed", "firmware build failed; see the build log", map[string]any{"log": o.LogPath, "error": err.Error()})
	}

	program := filepath.Join(buildDir, "sil-"+name, ".pioenvs", "sil-"+name, "program")
	if st, err := os.Stat(program); err != nil || !st.Mode().IsRegular() {
		return Result{}, apperr.Infra("build_failed", "build finished without a program", map[string]any{"log": o.LogPath, "expected": program})
	}
	return Result{Program: program}, nil
}

func validate(o Options) error {
	for _, p := range []struct{ field, v string }{
		{"config_path", o.ConfigPath}, {"repo_root", o.RepoRoot}, {"cache_dir", o.CacheDir}, {"log_path", o.LogPath},
	} {
		if !filepath.IsAbs(p.v) {
			return apperr.Infra("build_failed", p.field+" must be an absolute path", map[string]any{"value": p.v})
		}
	}
	for _, k := range []string{"sil_name", "sil_mac", "sil_port"} {
		if o.Substitutions[k] == "" {
			return apperr.Infra("build_failed", "missing substitution "+k, nil)
		}
	}
	if o.APIKey == "" {
		return apperr.Infra("build_failed", "missing API key", nil)
	}
	return nil
}

// dockerArgs builds the argument slice; the API key is deliberately absent
// (`-e SIL_API_KEY` without a value takes it from the process environment).
func dockerArgs(o Options, buildDir, cacheDir string) []string {
	args := []string{
		"run", "--rm",
		"--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"-e", "HOME=/cache/home",
		"-e", "ESPHOME_DATA_DIR=/build/data",
		// The repo is mounted read-only; without this ESPHome tries to write
		// a .gitignore next to the YAML and the compile dies (verified on 2026.10.1).
		"-e", "ESPHOME_NOGITIGNORE=1",
		"-e", "SIL_API_KEY",
		"-v", o.RepoRoot + ":" + o.RepoRoot + ":ro",
		"-w", filepath.Dir(o.ConfigPath),
		"-v", buildDir + ":/build",
		"-v", cacheDir + ":/cache",
		Image,
	}
	keys := make([]string, 0, len(o.Substitutions))
	for k := range o.Substitutions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-s", k, o.Substitutions[k])
	}
	return append(args, "compile", filepath.Base(o.ConfigPath))
}

func envWithout(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}

// String makes Result printable in test logs.
func (r Result) String() string { return fmt.Sprintf("program=%s cached=%t", r.Program, r.Cached) }

// Package fwbuild compiles a host-platform ESPHome YAML inside the pinned
// ESPHome image and returns the path of the resulting static program.
//
// The pipeline is: preflight of docker and the pinned image (never pulled),
// `esphome config` offline, the stage-1 lint over the resolved config, an
// input hash that skips unchanged builds, `compile --only-generate` offline
// with the stage-2 lint over the auto-loaded integrations, the compile
// itself (offline once the shared library archives are cached), and a
// static-ELF assertion.
//
// The package is named fwbuild, not build, because the root .gitignore
// ignores every directory called build/.
package fwbuild

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// Image is the ESPHome toolchain image, pinned by tag and digest. It is
// never pulled by sil or the tests.
const Image = "ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64"

// tailLines is how many log lines a failure shows.
const tailLines = 40

// Options describes one firmware build.
type Options struct {
	// Instance is the sil instance name (informational; the firmware name is
	// taken from Substitutions["sil_name"]).
	Instance string
	// ConfigPath is the absolute path of the firmware YAML.
	ConfigPath string
	// RepoRoot is mounted read-only at its own absolute path when it contains
	// ConfigPath; otherwise the YAML's git top-level (else its directory) is.
	RepoRoot string
	// CacheDir is <cache root>/sil/build/<name>; it holds build/ and cache/.
	// The shared toolcache lives at <cache root>/sil/toolcache.
	CacheDir string
	// Substitutions are passed as -s key value (sil_name, sil_mac, sil_port).
	Substitutions map[string]string
	// APIKey is the api.encryption.key; it reaches docker only via the
	// process environment.
	APIKey string
	// LogPath receives the docker output of every stage.
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

// builder carries the state of one Build call.
type builder struct {
	o         Options
	name      string
	identity  Identity
	buildDir  string // <CacheDir>/build, mounted at /build
	cacheDir  string // <CacheDir>/cache, mounted at /cache
	dataDir   string // <CacheDir>/build/data, ESPHOME_DATA_DIR
	toolcache string
	mount     string
	imageID   string
	logf      *os.File
}

// Build runs the whole pipeline for o.ConfigPath.
func Build(ctx context.Context, o Options) (Result, error) {
	if err := validate(o); err != nil {
		return Result{}, err
	}
	port, _ := strconv.Atoi(o.Substitutions["sil_port"])
	b := &builder{
		o:    o,
		name: o.Substitutions["sil_name"],
		identity: Identity{
			DeviceName: "sil-" + o.Substitutions["sil_name"],
			MAC:        o.Substitutions["sil_mac"],
			Port:       port,
		},
		buildDir:  filepath.Join(o.CacheDir, "build"),
		cacheDir:  filepath.Join(o.CacheDir, "cache"),
		toolcache: toolcacheDir(o.CacheDir),
	}
	b.dataDir = filepath.Join(b.buildDir, "data")

	// platformio is pre-created so the image entrypoint does not print its
	// "Creating cache directory" notice into stdout the first time.
	for _, d := range []string{o.CacheDir, b.buildDir, b.cacheDir, filepath.Join(b.cacheDir, "home"), filepath.Join(b.cacheDir, "platformio")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Result{}, apperr.Infra("build_failed", "cannot create build directory", map[string]any{"path": d, "error": err.Error()})
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
	b.logf = logf

	return b.run(ctx)
}

func (b *builder) progress(msg string) {
	if b.o.Progress != nil {
		b.o.Progress(msg)
	}
}

func (b *builder) run(ctx context.Context) (Result, error) {
	if err := b.preflight(ctx); err != nil {
		return Result{}, err
	}
	b.mount = mountRoot(b.o)

	// Stage 1: esphome config, offline, then the lint over the resolved config.
	b.progress("checking the firmware config")
	resolved, err := b.stageConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := LintConfig(resolved, b.o.ConfigPath, b.identity); err != nil {
		return Result{}, err
	}

	// Skip the compile when nothing changed (D-15).
	componentDirs, includes := localInputs(resolved, filepath.Dir(b.o.ConfigPath))
	hash, err := InputHash(HashInputs{
		RecipeVersion: RecipeVersion, ImageID: b.imageID, Resolved: resolved,
		ComponentDirs: componentDirs, IncludeFiles: includes,
	})
	if err != nil {
		return Result{}, err
	}
	if !b.o.Rebuild {
		if rec, ok := ReadRecord(b.buildDir); ok && rec.Hit(hash) {
			b.progress("build cache hit")
			if err := AssertStatic(rec.Program); err != nil {
				return Result{}, err
			}
			return Result{Program: rec.Program, Cached: true}, nil
		}
	}
	removeRecord(b.buildDir)
	if b.o.Rebuild {
		if err := os.RemoveAll(filepath.Join(b.buildDir, "sil-"+b.name)); err != nil {
			return Result{}, apperr.Infra("build_failed", "cannot clear previous build", map[string]any{"error": err.Error()})
		}
	}

	// Stage 2: generate sources offline and lint what the components loaded.
	seeded, err := seedInstance(b.toolcache, b.dataDir, b.cacheDir)
	if err != nil {
		return Result{}, apperr.Infra("build_failed", "cannot seed the library archives from the toolcache",
			map[string]any{"toolcache": b.toolcache, "error": err.Error()})
	}
	if err := b.stageGenerate(ctx); err != nil {
		return Result{}, err
	}
	storagePath := filepath.Join(b.dataDir, "storage", filepath.Base(b.o.ConfigPath)+".json")
	storage, err := os.ReadFile(storagePath)
	if err != nil {
		return Result{}, apperr.Infra("build_failed", "ESPHome did not write its storage file", map[string]any{"path": storagePath, "error": err.Error()})
	}
	if err := LintStorage(storage, b.o.ConfigPath); err != nil {
		return Result{}, err
	}

	// Compile. Offline when the library archives are seeded.
	network := "none"
	if !seeded {
		network = "bridge"
		b.progress("fetching noise-c/libsodium archives (one-time, needs network)")
	}
	b.progress("building firmware in " + Image)
	if err := b.compile(ctx, network); err != nil {
		return Result{}, err
	}
	if err := writeBackToolcache(b.toolcache, b.dataDir, b.cacheDir); err != nil {
		// A missing write-back only costs the next new instance a download.
		fmt.Fprintf(b.logf, "sil: toolcache write-back failed: %v\n", err)
	}

	program := filepath.Join(b.buildDir, "sil-"+b.name, ".pioenvs", "sil-"+b.name, "program")
	if st, err := os.Stat(program); err != nil || !st.Mode().IsRegular() {
		return Result{}, apperr.Infra("build_failed", "build finished without a program", map[string]any{"log": b.o.LogPath, "expected": program})
	}
	if err := AssertStatic(program); err != nil {
		return Result{}, err
	}
	if err := WriteRecord(b.buildDir, BuildRecord{Hash: hash, Program: program}); err != nil {
		return Result{}, apperr.Infra("build_failed", "cannot write the build record", map[string]any{"error": err.Error()})
	}
	return Result{Program: program}, nil
}

// preflight checks docker (`docker version`) and the pinned image
// (`docker image inspect`). It never pulls.
func (b *builder) preflight(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Infra("docker_unavailable",
			"docker is not usable: start the docker daemon and make sure your user may talk to it (docker group)",
			map[string]any{"error": err.Error(), "output": strings.TrimSpace(string(out))})
	}
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", Image).Output()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Infra("image_missing",
			"the pinned ESPHome image is not present locally; sil never pulls it. Run: docker pull "+Image,
			map[string]any{"image": Image, "error": err.Error()})
	}
	b.imageID = strings.TrimSpace(string(out))
	return nil
}

// stageConfig runs `esphome config` offline and returns the resolved YAML.
// Its stdout embeds the API key, so it is kept in memory and never logged.
func (b *builder) stageConfig(ctx context.Context) ([]byte, error) {
	fmt.Fprintln(b.logf, "sil: config network=none")
	var stdout, stderr bytes.Buffer
	err := b.docker(ctx, "none", &stdout, io.MultiWriter(b.logf, &stderr), "config", filepath.Base(b.o.ConfigPath))
	if err != nil {
		return nil, b.stageFailure(ctx, err, "esphome config failed", stderr.String()+stdout.String())
	}
	return trimPreamble(stdout.Bytes()), nil
}

// stageGenerate runs `compile --only-generate` offline.
func (b *builder) stageGenerate(ctx context.Context) error {
	fmt.Fprintln(b.logf, "sil: generate network=none")
	var out bytes.Buffer
	w := io.MultiWriter(b.logf, &out)
	if err := b.docker(ctx, "none", w, w, "compile", "--only-generate", filepath.Base(b.o.ConfigPath)); err != nil {
		return b.stageFailure(ctx, err, "esphome code generation failed", out.String())
	}
	return nil
}

// compile runs the real compile; its whole output goes to the build log.
func (b *builder) compile(ctx context.Context, network string) error {
	fmt.Fprintf(b.logf, "sil: compile network=%s\n", network)
	if err := b.docker(ctx, network, b.logf, b.logf, "compile", filepath.Base(b.o.ConfigPath)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw, _ := os.ReadFile(b.o.LogPath)
		tail := lastLines(cleanOutput(string(raw), b.o.APIKey), tailLines)
		msg := "firmware build failed; see the build log " + b.o.LogPath
		if bytes.Contains(raw, []byte("Failed to download")) || bytes.Contains(raw, []byte("HTTPClientError")) {
			if network == "none" {
				msg += ": a library or its registry entry is missing from the toolcache and the compile runs offline; delete " + b.toolcache + " so the next build fetches them again (needs network)"
			} else {
				msg += ": the one-time fetch of the noise-c/libsodium archives failed; the first build on this host needs network access"
			}
		}
		return apperr.Infra("build_failed", msg, map[string]any{"log": b.o.LogPath, "output_tail": tail, "error": err.Error()})
	}
	return nil
}

// stageFailure maps a failed docker run of the config or generate stage: an
// ESPHome exit is a user-fixable config_invalid carrying ESPHome's message
// (key redacted) and the file; docker's own failures are infrastructure.
func (b *builder) stageFailure(ctx context.Context, err error, what, output string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || (ee.ExitCode() >= 125 && ee.ExitCode() <= 127) {
		return apperr.Infra("build_failed", what+": docker could not run the pinned image", map[string]any{"error": err.Error(), "log": b.o.LogPath})
	}
	tail := lastLines(cleanOutput(output, b.o.APIKey), tailLines)
	return apperr.Failed("config_invalid", fmt.Sprintf("%s: %s:\n%s", b.o.ConfigPath, what, tail),
		map[string]any{"file": b.o.ConfigPath})
}

// docker runs the pinned image with the given esphome arguments. The API key
// reaches the container only through the process environment.
func (b *builder) docker(ctx context.Context, network string, stdout, stderr io.Writer, esphomeArgs ...string) error {
	cmd := exec.CommandContext(ctx, "docker", dockerArgs(b.o, b.mount, b.buildDir, b.cacheDir, network, esphomeArgs...)...)
	cmd.Env = append(envWithout(os.Environ(), "SIL_API_KEY"), "SIL_API_KEY="+b.o.APIKey)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
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
	if _, err := strconv.Atoi(o.Substitutions["sil_port"]); err != nil {
		return apperr.Infra("build_failed", "sil_port must be a number", map[string]any{"value": o.Substitutions["sil_port"]})
	}
	if o.APIKey == "" {
		return apperr.Infra("build_failed", "missing API key", nil)
	}
	return nil
}

// mountRoot is the tree mounted read-only at its own absolute path: RepoRoot
// when it contains the config, else the YAML's git top-level, else the YAML's
// directory. Never "/" and never the home directory.
func mountRoot(o Options) string {
	dir := filepath.Dir(o.ConfigPath)
	if within(o.RepoRoot, o.ConfigPath) {
		return o.RepoRoot
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		top := strings.TrimSpace(string(out))
		home, _ := os.UserHomeDir()
		if filepath.IsAbs(top) && top != "/" && top != home && within(top, o.ConfigPath) {
			return top
		}
	}
	return dir
}

// within reports whether path is inside root (or equal to it).
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dockerArgs builds the argument slice; the API key is deliberately absent
// (`-e SIL_API_KEY` without a value takes it from the process environment).
// The repo mount is read-only so the working tree stays clean.
func dockerArgs(o Options, mount, buildDir, cacheDir, network string, esphomeArgs ...string) []string {
	args := []string{
		"run", "--rm", "--network", network,
		"--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"-e", "HOME=/cache/home",
		"-e", "ESPHOME_DATA_DIR=/build/data",
		// With the tree mounted read-only ESPHome would otherwise die writing a
		// .gitignore next to the YAML (verified on 2026.10.1).
		"-e", "ESPHOME_NOGITIGNORE=1",
		"-e", "SIL_API_KEY",
		"-v", mount + ":" + mount + ":ro",
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
	return append(args, esphomeArgs...)
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

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// cleanOutput removes ANSI sequences and the image entrypoint's cache notice,
// replaces every occurrence of the API key with <redacted> and trims blank
// lines, so the same failure always renders the same text.
func cleanOutput(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "<redacted>")
	}
	s = ansiRE.ReplaceAllString(s, "")
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(line, "Creating cache directory ") || strings.HasPrefix(line, "You can change this behavior by mounting") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// lastLines returns the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// String makes Result printable in test logs.
func (r Result) String() string { return fmt.Sprintf("program=%s cached=%t", r.Program, r.Cached) }

// Package lifecycle orchestrates `sil up` and `sil down`: identity, run dir,
// firmware build, the detached `sil serve` supervisor, and the wait until
// the firmware is on the bus.
package lifecycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
	"github.com/zeroflow/esphome-sil/internal/fwbuild"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

const (
	modulePath    = "github.com/zeroflow/esphome-sil"
	defaultConfig = "examples/rev33.yaml"
	readyBudget   = 60 * time.Second
	statusBudget  = 2 * time.Second
	pollEvery     = 250 * time.Millisecond
	downBudget    = 5 * time.Second // the control socket must answer a down request within this
	stopBudget    = 10 * time.Second
	termGrace     = 5 * time.Second // fallback: SIGTERM to SIGKILL
	killGrace     = 2 * time.Second
	tailLines     = 20
)

// UpOptions are the inputs of Up.
type UpOptions struct {
	Name, ConfigPath string
	APIPort          int
	// WebPort is --web-port (0 keeps the stored port), Listen the IP address of the web listener
	// ("" means 0.0.0.0), AllowHosts the extra Host names it accepts and UIDir a directory the page is
	// served from instead of the binary. Up normalizes them with checkWebOptions.
	WebPort        int
	Listen         string
	AllowHosts     []string
	UIDir          string
	Rebuild, Quiet bool
	// Fan is the library fan plugged into all four ports; "" means fanlib.DefaultFan.
	Fan string
	// Progress receives human progress lines; nil or Quiet discards them.
	Progress io.Writer
}

// APIInfo is what a client needs to reach the firmware's native API.
type APIInfo struct {
	Host string `json:"host"`
	// LanHost is the default-route IPv4 address for a Home Assistant on another machine (null when unknown).
	LanHost    *string `json:"lan_host"`
	Port       int     `json:"port"`
	Key        string  `json:"key"`
	DeviceName string  `json:"device_name"`
	MAC        string  `json:"mac"`
}

// WebInfo is where the page of the instance is reachable.
type WebInfo struct {
	URL    string `json:"url"`
	Port   int    `json:"port"`
	Listen string `json:"listen"`
}

// UpResult is the JSON printed by `sil up`.
type UpResult struct {
	Instance string         `json:"instance"`
	API      APIInfo        `json:"api"`
	Web      WebInfo        `json:"web"`
	RunDir   string         `json:"run_dir"`
	Build    fwbuild.Result `json:"build"`
}

// RepoRoot finds the esphome-sil checkout: SIL_REPO, else a walk up from the
// working directory, then from the executable, for a go.mod whose module line
// is github.com/zeroflow/esphome-sil.
func RepoRoot() (string, error) {
	if v := os.Getenv("SIL_REPO"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil && isRepoRoot(abs) {
			return abs, nil
		}
		return "", apperr.Failed("config_not_found", "SIL_REPO does not point at an esphome-sil checkout", map[string]any{"SIL_REPO": v})
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, dir := range starts {
		for {
			if isRepoRoot(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", apperr.Failed("config_not_found",
		"cannot locate the esphome-sil repository for the default example; pass --config or set SIL_REPO", nil)
}

func isRepoRoot(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) == modulePath
		}
	}
	return false
}

// Up builds and starts instance o.Name and waits until the firmware is ready
// (a real encrypted API handshake plus the bus connection).
//
// Order: name, paths and socket lengths (pure checks), config and run-dir
// parent, the per-instance lock (held until Up returns), liveness, identity
// and port, test-bind, fresh run dir, build, test-bind again, serve.
func Up(ctx context.Context, o UpOptions) (UpResult, error) {
	if err := instance.ValidateName(o.Name); err != nil {
		return UpResult{}, err
	}
	p, err := instance.PathsFor(o.Name)
	if err != nil {
		return UpResult{}, err
	}
	if err := p.CheckSocketPaths(); err != nil {
		return UpResult{}, err
	}
	progress := o.Progress
	if progress == nil || o.Quiet {
		progress = io.Discard
	}

	// The reach options are pure checks too: a bad address, host name or UI directory must not
	// build, lock or write anything.
	if err := checkWebOptions(&o); err != nil {
		return UpResult{}, err
	}
	configPath, repo, err := resolveConfig(o.ConfigPath)
	if err != nil {
		return UpResult{}, err
	}
	// An unknown or rejected fan must not leave an identity behind, so it is
	// checked before anything is written.
	fan, err := resolveFan(o.Fan)
	if err != nil {
		return UpResult{}, err
	}
	if err := instance.EnsureRunParent(p); err != nil {
		return UpResult{}, err
	}
	release, err := instance.LockInstance(p)
	if err != nil {
		return UpResult{}, err
	}
	defer release()

	// Liveness comes before LoadOrCreate: --api-port must not rewrite the
	// identity of an instance that is running.
	if err := checkNotRunning(ctx, p); err != nil {
		return UpResult{}, err
	}
	id, err := instance.LoadOrCreate(p, instance.PortRequest{API: o.APIPort, Web: o.WebPort, Listen: o.Listen})
	if err != nil {
		return UpResult{}, err
	}
	if err := instance.TestBind(id.APIPort); err != nil {
		return UpResult{}, err
	}
	// A taken sticky web port fails here, before the build, and is never moved (D-09).
	if err := instance.TestBindWeb(o.Listen, id.WebPort); err != nil {
		return UpResult{}, err
	}
	if err := instance.ResetRunDir(p); err != nil {
		return UpResult{}, err
	}

	res, err := fwbuild.Build(ctx, fwbuild.Options{
		Instance:   o.Name,
		ConfigPath: configPath,
		RepoRoot:   repo,
		CacheDir:   p.CacheDir,
		Substitutions: map[string]string{
			"sil_name": o.Name,
			"sil_mac":  instance.MAC(o.Name),
			"sil_port": strconv.Itoa(id.APIPort),
		},
		APIKey:   id.Key,
		LogPath:  p.BuildLog(),
		Rebuild:  o.Rebuild,
		Progress: func(msg string) { fmt.Fprintln(progress, msg) },
	})
	if err != nil {
		return UpResult{}, withLogTail(err, p.BuildLog(), id.Key)
	}

	if err := instance.WriteSpec(p, instance.Spec{
		Schema: 1, Instance: o.Name, Program: res.Program,
		Fans:   []string{fan, fan, fan, fan},
		Listen: o.Listen, AllowHosts: o.AllowHosts, UIDir: o.UIDir,
	}); err != nil {
		return UpResult{}, err
	}

	// The build takes long enough for another process to take the port.
	if err := instance.TestBind(id.APIPort); err != nil {
		return UpResult{}, err
	}
	if err := instance.TestBindWeb(o.Listen, id.WebPort); err != nil {
		return UpResult{}, err
	}
	srv, err := startServe(p)
	if err != nil {
		return UpResult{}, err
	}
	fmt.Fprintln(progress, "waiting for the firmware to connect to the simulator")
	if err := waitReady(ctx, p, srv, id.Key); err != nil {
		srv.abort()
		return UpResult{}, err
	}
	lan := instance.LANHost()
	hint := lan
	if hint == "" {
		hint = "127.0.0.1"
	}
	fmt.Fprintf(progress, "add to Home Assistant: host %s port %d, encryption key from this JSON (no mDNS on host; Settings > Devices & Services > Add Integration > ESPHome)\n", hint, id.APIPort)
	url := instance.WebURL(o.Listen, lan, id.WebPort)
	fmt.Fprintf(progress, "web UI: %s (unauthenticated: anyone who can reach this address can watch and drive this bench; sil up --listen 127.0.0.1 keeps it on this machine)\n", url)

	return UpResult{
		Instance: o.Name,
		API: APIInfo{
			Host: "127.0.0.1", LanHost: lanHostPtr(lan), Port: id.APIPort, Key: id.Key,
			DeviceName: id.DeviceName(), MAC: instance.MAC(o.Name),
		},
		Web:    WebInfo{URL: url, Port: id.WebPort, Listen: o.Listen},
		RunDir: p.RunDir,
		Build:  res,
	}, nil
}

// defaultListen is the address of the web listener when `sil up --listen` is not given (D-07).
const defaultListen = "0.0.0.0"

// checkWebOptions validates and normalizes the reach options of `sil up` (D-07, D-09) before
// anything is built or locked: Listen becomes a canonical IP literal (default 0.0.0.0), AllowHosts
// become lower-case DNS names without a trailing dot, UIDir becomes an absolute path to a
// directory that holds a regular index.html.
func checkWebOptions(o *UpOptions) error {
	if o.Listen == "" {
		o.Listen = defaultListen
	}
	ip := net.ParseIP(o.Listen)
	if ip == nil {
		return apperr.Failed("invalid_argument",
			fmt.Sprintf("--listen needs an IP address such as 0.0.0.0 or 127.0.0.1, got %q", o.Listen),
			map[string]any{"listen": o.Listen})
	}
	o.Listen = ip.String()

	var hosts []string
	for _, h := range o.AllowHosts {
		norm := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		if !validHostName(norm) {
			return apperr.Failed("invalid_argument",
				fmt.Sprintf("--allow-host needs a host name of letters, digits, '-' and '.', got %q", h),
				map[string]any{"allow_host": h})
		}
		if !slices.Contains(hosts, norm) {
			hosts = append(hosts, norm)
		}
	}
	o.AllowHosts = hosts

	if o.UIDir != "" {
		abs, err := filepath.Abs(o.UIDir)
		if err != nil {
			return apperr.Failed("invalid_argument", "--ui-dir needs a directory with index.html",
				map[string]any{"ui_dir": o.UIDir, "error": err.Error()})
		}
		fi, err := os.Stat(filepath.Join(abs, "index.html"))
		if err != nil || !fi.Mode().IsRegular() {
			return apperr.Failed("invalid_argument", "--ui-dir needs a directory with index.html",
				map[string]any{"ui_dir": o.UIDir})
		}
		o.UIDir = abs
	}
	return nil
}

// validHostName reports whether s is a plausible DNS name: dot-separated labels of 1 to 63 letters,
// digits and '-' that neither start nor end with '-', at most 253 bytes in all.
func validHostName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// resolveFan validates the fan of `sil up --fan`: "" is the default fan; any other id must be in
// the fan library (so it can never be turned into a path, T-02-12) and its file must load.
func resolveFan(fan string) (string, error) {
	if fan == "" {
		fan = fanlib.DefaultFan
	} else {
		ids, err := fanlib.IDs()
		if err != nil {
			return "", apperr.Infra("internal", "cannot list the fan library: "+err.Error(), nil)
		}
		if !slices.Contains(ids, fan) {
			return "", apperr.Failed("unknown_fan", fmt.Sprintf("unknown fan %q; known: %s", fan, strings.Join(ids, ", ")),
				map[string]any{"fan": fan, "known": ids})
		}
	}
	if _, err := fanlib.Load(fan); err != nil {
		return "", apperr.Failed("fan_toml_invalid", err.Error(), map[string]any{"fan": fan})
	}
	return fan, nil
}

// checkNotRunning is Failed already_running when run.json names a live
// `sil serve <name>` (PID alive and its command line matches). A missing
// run.json, or one naming a dead or foreign PID, means not running: the run
// dir is simply recreated.
func checkNotRunning(ctx context.Context, p instance.Paths) error {
	rf, err := instance.ReadRunFile(p)
	if err != nil || !instance.ProcessAlive(rf, p.Name) {
		return nil
	}
	detail := map[string]any{"instance": p.Name, "pid": rf.PID, "run_dir": rf.RunDir}
	clk := clock.NewReal()
	sctx, cancel := clock.WithTimeout(ctx, clk, time.Second)
	defer cancel()
	if st, err := control.Dial(p.APISock()).Status(sctx); err == nil {
		detail["phase"] = st.Phase
	} else {
		detail["control_responding"] = false
	}
	return apperr.Failed("already_running",
		fmt.Sprintf("instance %s is already running (pid %d); run `sil down %s` first", p.Name, rf.PID, p.Name), detail)
}

// resolveConfig returns the absolute firmware YAML and the directory mounted
// read-only into the build container.
func resolveConfig(configPath string) (config, repo string, err error) {
	root, rerr := RepoRoot()
	if configPath == "" {
		if rerr != nil {
			return "", "", rerr
		}
		config = filepath.Join(root, defaultConfig)
	} else {
		if config, err = filepath.Abs(configPath); err != nil {
			return "", "", apperr.Failed("config_not_found", "cannot resolve the config path", map[string]any{"path": configPath})
		}
	}
	st, serr := os.Stat(config)
	if serr != nil || !st.Mode().IsRegular() {
		return "", "", apperr.Failed("config_not_found", "firmware config file not found", map[string]any{"path": config})
	}
	if rerr != nil {
		// Outside any checkout: a self-contained config is mounted as its own directory.
		root = filepath.Dir(config)
	}
	if err := fwbuild.CheckMountRoot(root); err != nil {
		return "", "", err
	}
	return config, root, nil
}

// serveProc is the detached `sil serve` process as seen from Up.
type serveProc struct {
	cmd  *exec.Cmd
	done chan struct{}
	logf *os.File
}

func startServe(p instance.Paths) (*serveProc, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, apperr.Infra("serve_start_failed", "cannot locate the sil executable", map[string]any{"error": err.Error()})
	}
	logf, err := os.OpenFile(p.SilLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, apperr.Infra("serve_start_failed", "cannot open sil.log", map[string]any{"error": err.Error()})
	}
	cmd := exec.Command(exe, "serve", p.Name) // stdin stays /dev/null
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, apperr.Infra("serve_start_failed", "cannot start sil serve", map[string]any{"error": err.Error()})
	}
	sp := &serveProc{cmd: cmd, done: make(chan struct{}), logf: logf}
	// Reap the child if it dies while we are still waiting; the goroutine
	// ends with it, so a normal `sil up` (which exits right after) leaves
	// nothing behind and a long-lived caller leaves no zombie.
	go func() {
		_ = cmd.Wait()
		logf.Close()
		close(sp.done)
	}()
	return sp, nil
}

// abort stops a serve process that never became ready.
func (sp *serveProc) abort() {
	_ = sp.cmd.Process.Signal(syscall.SIGTERM)
	clk := clock.NewReal()
	timer := clk.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-sp.done:
	case <-timer.C():
		_ = sp.cmd.Process.Kill()
		<-sp.done
	}
}

// waitReady polls the control socket until the phase is ready.
func waitReady(ctx context.Context, p instance.Paths, srv *serveProc, key string) error {
	clk := clock.NewReal()
	budget, cancel := clock.WithTimeout(ctx, clk, readyBudget)
	defer cancel()
	client := control.Dial(p.APISock())
	for {
		sctx, scancel := clock.WithTimeout(budget, clk, statusBudget)
		st, err := client.Status(sctx)
		scancel()
		if err == nil {
			switch st.Phase {
			case control.PhaseReady:
				return nil
			case control.PhaseFailed:
				if st.Error != nil {
					return withLogTail(userClass(st.Error), p.FirmwareLog(), key)
				}
				return withLogTail(apperr.Infra("firmware_failed", "the firmware failed to start", nil), p.FirmwareLog(), key)
			}
		}
		select {
		case <-srv.done:
			return withLogTail(apperr.Infra("serve_exited", "sil serve exited before the firmware was ready; see sil.log", map[string]any{"log": p.SilLog()}), p.SilLog(), key)
		default:
		}
		if err := clk.Sleep(budget, pollEvery); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return withLogTail(apperr.Infra("ready_timeout",
				fmt.Sprintf("the firmware did not connect to the simulator within %s", readyBudget),
				map[string]any{"firmware_log": p.FirmwareLog()}), p.FirmwareLog(), key)
		}
	}
}

// userClass restores the exit class of errors the supervisor reports in a
// failed Status: the class does not travel over the control API, and a
// firmware that crash-loops is a problem with the firmware under test (exit
// 1), not with the bench.
func userClass(e *apperr.Error) *apperr.Error {
	if e.Code != "firmware_crash_loop" {
		return e
	}
	out := *e
	out.Exit = apperr.ExitFailed
	return &out
}

// withLogTail adds the last lines of a log file, with the API key redacted,
// to the detail of an apperr error.
func withLogTail(err error, logPath, key string) error {
	ae := apperr.AsError(err)
	tail := readTail(logPath, tailLines, key)
	if tail == "" {
		return ae
	}
	out := *ae
	out.Detail = make(map[string]any, len(ae.Detail)+1)
	for k, v := range ae.Detail {
		out.Detail[k] = v
	}
	out.Detail["log_tail"] = tail
	return &out
}

func readTail(path string, n int, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(raw)
	if key != "" {
		text = strings.ReplaceAll(text, key, "<key>")
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Down stops instance name. A missing run.json is not an error: it returns
// Stopped false, and so does a run.json that names a dead or foreign PID
// (it is cleaned up).
//
// The normal path asks the supervisor over its control socket. When that does
// not answer within downBudget (a stopped or wedged supervisor, a dead
// socket), Down falls back to SIGTERM and then SIGKILL of the run.json PID,
// but only while /proc/<pid>/cmdline still says `sil serve <name>` (T-01-24):
// the PID in run.json is never trusted alone. The firmware dies with its
// supervisor (Pdeathsig); the sockets and run.json are removed afterwards.
func Down(ctx context.Context, name string) (control.DownResult, error) {
	if err := instance.ValidateName(name); err != nil {
		return control.DownResult{}, err
	}
	p, err := instance.PathsFor(name)
	if err != nil {
		return control.DownResult{}, err
	}
	rf, err := instance.LoadRunFile(p)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Code == "not_running" {
			return control.DownResult{Instance: name, Stopped: false}, nil
		}
		return control.DownResult{}, err
	}
	if !instance.ProcessAlive(rf, name) {
		cleanupRun(p)
		return control.DownResult{Instance: name, Stopped: false}, nil
	}
	clk := clock.NewReal()
	dctx, cancel := clock.WithTimeout(ctx, clk, downBudget)
	// Dial the socket of this instance's own run dir, never the path run.json
	// names: run.json says whether the instance is live, not where to connect.
	res, err := control.Dial(p.APISock()).Down(dctx)
	timedOut := clock.TimedOut(dctx)
	cancel()
	if err != nil {
		var ae *apperr.Error
		unreachable := errors.As(err, &ae) && ae.Code == "control_unreachable"
		if !unreachable && !timedOut {
			if ctx.Err() != nil {
				return control.DownResult{}, ctx.Err()
			}
			return control.DownResult{}, err
		}
		return forceStop(ctx, clk, p, rf)
	}
	// The supervisor answered; give it stopBudget to exit, then force it.
	sctx, scancel := clock.WithTimeout(ctx, clk, stopBudget)
	defer scancel()
	for instance.ProcessAlive(rf, name) {
		if err := clk.Sleep(sctx, 50*time.Millisecond); err != nil {
			if ctx.Err() != nil {
				return control.DownResult{}, ctx.Err()
			}
			return forceStop(ctx, clk, p, rf)
		}
	}
	return res, nil
}

// forceStop is the Down fallback: SIGTERM the supervisor named by rf, wait
// termGrace, then SIGKILL it. Each signal is sent only after the PID has been
// confirmed to still be `sil serve <name>`.
func forceStop(ctx context.Context, clk clock.Clock, p instance.Paths, rf instance.RunFile) (control.DownResult, error) {
	if instance.ProcessAlive(rf, p.Name) {
		_ = syscall.Kill(rf.PID, syscall.SIGTERM)
		if !waitGone(ctx, clk, rf, p.Name, termGrace) {
			if ctx.Err() != nil {
				return control.DownResult{}, ctx.Err()
			}
			if instance.ProcessAlive(rf, p.Name) { // re-checked: the PID could have been reused meanwhile
				_ = syscall.Kill(rf.PID, syscall.SIGKILL)
			}
			if !waitGone(ctx, clk, rf, p.Name, killGrace) {
				if ctx.Err() != nil {
					return control.DownResult{}, ctx.Err()
				}
				return control.DownResult{}, apperr.Infra("stop_timeout",
					fmt.Sprintf("sil serve (pid %d) is still running after SIGTERM and SIGKILL", rf.PID),
					map[string]any{"pid": rf.PID})
			}
		}
	}
	cleanupRun(p)
	return control.DownResult{Instance: p.Name, Stopped: true}, nil
}

// waitGone polls until rf no longer names a live `sil serve <name>` or d has passed.
func waitGone(ctx context.Context, clk clock.Clock, rf instance.RunFile, name string, d time.Duration) bool {
	wctx, cancel := clock.WithTimeout(ctx, clk, d)
	defer cancel()
	for instance.ProcessAlive(rf, name) {
		if err := clk.Sleep(wctx, 50*time.Millisecond); err != nil {
			return !instance.ProcessAlive(rf, name)
		}
	}
	return true
}

// cleanupRun removes what a dead supervisor leaves behind: run.json and the
// two sockets (the run dir itself stays for the logs).
func cleanupRun(p instance.Paths) {
	for _, f := range []string{p.RunJSON(), p.APISock(), p.BusSock()} {
		_ = os.Remove(f)
	}
}

// lanHostPtr is the JSON form of a LAN address: nil (null) when it is unknown.
func lanHostPtr(h string) *string {
	if h == "" {
		return nil
	}
	return &h
}

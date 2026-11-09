// Package cli is the sil command line: a kong grammar over the lifecycle,
// control and supervisor packages.
//
// Contract (CLI-05): every command writes exactly one JSON value to stdout,
// built in memory first so a failure never leaves half a value behind.
// Failures print {"error":{"code","message","detail"}} and exit 1 for
// user-fixable and 2 for infrastructure errors; kong parse errors are
// invalid_argument (exit 1) in the same shape. Human text (progress lines,
// slog, the one-line error echo, --help) goes to stderr only; the global
// --quiet drops progress and slog. The hidden `serve` command is the detached
// supervisor; its stdout is sil.log, so it prints nothing.
//
// Instance commands (status, down, sim restart, fw restart) resolve the
// instance as positional name, else SIL_INSTANCE, else the single live
// instance (resolve.go). Only up and down orchestrate processes; status and the
// restart commands are pure clients of the control API.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/instance"
	"github.com/zeroflow/esphome-sil/internal/lifecycle"
	"github.com/zeroflow/esphome-sil/internal/supervisor"
)

const statusBudget = 5 * time.Second

// CLI is the command grammar.
type CLI struct {
	Quiet  bool      `help:"No progress lines and no log output on stderr."`
	Up     UpCmd     `cmd:"" help:"Build the firmware, start the instance and wait until it is on the sim-bus."`
	Status StatusCmd `cmd:"" help:"Print the instance status as JSON."`
	Down   DownCmd   `cmd:"" help:"Stop the instance."`
	Sim    SimCmd    `cmd:"" help:"Operate on the simulator of an instance."`
	Fw     FwCmd     `cmd:"" help:"Operate on the firmware of an instance."`
	Serve  ServeCmd  `cmd:"" hidden:"" help:"Run the detached supervisor (internal)."`
}

// UpCmd is `sil up`.
type UpCmd struct {
	Name    string `arg:"" help:"Instance name (a-z, 0-9, '-')."`
	Config  string `help:"Firmware YAML (default: examples/fan-skeleton.yaml of this checkout)."`
	APIPort int    `name:"api-port" help:"Firmware native API port (default: the stored port, first 6054)."`
	Rebuild bool   `help:"Discard the previous build output of this instance first."`
}

// StatusCmd is `sil status`.
type StatusCmd struct {
	Name string `arg:"" optional:"" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// DownCmd is `sil down`.
type DownCmd struct {
	Name string `arg:"" optional:"" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// SimCmd groups the simulator operations.
type SimCmd struct {
	Restart SimRestartCmd `cmd:"" help:"Replace the simulator with a fresh one; the firmware keeps running and resyncs."`
}

// SimRestartCmd is `sil sim restart`.
type SimRestartCmd struct {
	Name string        `arg:"" optional:"" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
	Gap  time.Duration `help:"Keep the simulator away for this long before the new one starts (0 to 30s), e.g. 3s."`
}

// FwCmd groups the firmware operations.
type FwCmd struct {
	Restart FwRestartCmd `cmd:"" help:"Restart the firmware process (SIGTERM, then SIGKILL after 3s) with the same program and prefs."`
}

// FwRestartCmd is `sil fw restart`.
type FwRestartCmd struct {
	Name string `arg:"" optional:"" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// ServeCmd is the hidden `sil serve`.
type ServeCmd struct {
	Name string `arg:"" help:"Instance name."`
}

// runEnv carries what commands hand back to Execute.
type runEnv struct {
	result  any
	silent  bool
	quiet   bool
	stderr  io.Writer
	context context.Context
}

// exitSignal unwinds out of kong when it wants to exit (help, version).
type exitSignal int

// Main parses args, runs the command and returns the process exit code.
func Main(args []string) int { return Execute(args, os.Stdout, os.Stderr) }

// Execute is Main with explicit output streams (the tests capture them).
func Execute(args []string, stdout, stderr io.Writer) int {
	env := &runEnv{stderr: stderr}

	var cli CLI
	// kong writes help to stderr: stdout carries JSON only.
	parser, err := kong.New(&cli,
		kong.Name("sil"),
		kong.Description("Software-in-the-loop bench for ESPHome firmware."),
		kong.Writers(stderr, stderr),
		kong.Exit(func(code int) { panic(exitSignal(code)) }),
		kong.Bind(env),
	)
	if err != nil {
		return fail(stdout, stderr, apperr.Infra("internal", err.Error(), nil))
	}

	var kctx *kong.Context
	if code, exited := catchExit(func() { kctx, err = parser.Parse(args) }); exited {
		return code
	}
	if err != nil {
		return fail(stdout, stderr, apperr.Failed("invalid_argument", err.Error(), nil))
	}

	env.quiet = cli.Quiet
	if env.quiet {
		env.stderr = io.Discard
	}
	setupLogging(stderr, env.quiet)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	env.context = ctx
	if err := kctx.Run(); err != nil {
		return fail(stdout, stderr, err)
	}
	if env.silent {
		return 0
	}
	if err := writeJSON(stdout, env.result); err != nil {
		return fail(stdout, stderr, apperr.Infra("internal", "cannot write the result: "+err.Error(), nil))
	}
	return 0
}

func catchExit(f func()) (code int, exited bool) {
	defer func() {
		if r := recover(); r != nil {
			es, ok := r.(exitSignal)
			if !ok {
				panic(r)
			}
			code, exited = int(es), true
		}
	}()
	f()
	return 0, false
}

// setupLogging sends slog to stderr: text on a terminal, JSON otherwise;
// --quiet discards it.
func setupLogging(stderr io.Writer, quiet bool) {
	var h slog.Handler
	switch f, _ := stderr.(*os.File); {
	case quiet:
		h = slog.NewTextHandler(io.Discard, nil)
	case f != nil && isTerminal(f):
		h = slog.NewTextHandler(stderr, nil)
	default:
		h = slog.NewJSONHandler(stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// writeJSON is the only way a JSON value reaches stdout: the value is
// marshalled completely first, so an unencodable value writes nothing.
func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// errorBody is the stdout shape of every failure.
type errorBody struct {
	Error *apperr.Error `json:"error"`
}

// fail prints the error object on stdout and a one-line human message on
// stderr, and returns the exit code. An error whose detail cannot be encoded
// is printed without it, so stdout always carries exactly one value.
func fail(stdout, stderr io.Writer, err error) int {
	ae := apperr.AsError(err)
	if werr := writeJSON(stdout, errorBody{ae}); werr != nil {
		bare := &apperr.Error{Code: ae.Code, Message: ae.Message, Exit: ae.Exit}
		_ = writeJSON(stdout, errorBody{bare})
	}
	fmt.Fprintf(stderr, "sil: %s: %s\n", ae.Code, ae.Message)
	return apperr.ExitCode(ae)
}

// Run implements `sil up`.
func (c *UpCmd) Run(env *runEnv) error {
	res, err := lifecycle.Up(env.context, lifecycle.UpOptions{
		Name: c.Name, ConfigPath: c.Config, APIPort: c.APIPort,
		Rebuild: c.Rebuild, Quiet: env.quiet, Progress: env.stderr,
	})
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil status`: the CLI only reads the control API. A control
// answer is exit 0 whatever the phase says (failed included): status reports
// the state, and the failure is data in the JSON (phase, error).
func (c *StatusCmd) Run(env *runEnv) error {
	_, client, err := connect(c.Name)
	if err != nil {
		return err
	}
	ctx, cancel := clock.WithTimeout(env.context, clock.NewReal(), statusBudget)
	defer cancel()
	st, err := client.Status(ctx)
	if err != nil {
		if clock.TimedOut(ctx) {
			return apperr.Infra("control_unreachable", fmt.Sprintf("no answer from the instance control socket within %s", statusBudget), nil)
		}
		return err
	}
	env.result = st
	return nil
}

// Run implements `sil down`.
func (c *DownCmd) Run(env *runEnv) error {
	p, err := resolvePaths(c.Name)
	if err != nil {
		return err
	}
	res, err := lifecycle.Down(env.context, p.Name)
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil sim restart`: the Status after the firmware resynced.
func (c *SimRestartCmd) Run(env *runEnv) error {
	_, client, err := connect(c.Name)
	if err != nil {
		return err
	}
	st, err := client.SimRestart(env.context, control.SimRestartRequest{GapS: c.Gap.Seconds()})
	if err != nil {
		return err
	}
	env.result = st
	return nil
}

// Run implements `sil fw restart`: the Status once the instance is ready again.
func (c *FwRestartCmd) Run(env *runEnv) error {
	_, client, err := connect(c.Name)
	if err != nil {
		return err
	}
	st, err := client.FwRestart(env.context)
	if err != nil {
		return err
	}
	env.result = st
	return nil
}

// Run implements the hidden `sil serve`.
func (c *ServeCmd) Run(env *runEnv) error {
	env.silent = true
	if err := instance.ValidateName(c.Name); err != nil {
		return err
	}
	err := supervisor.Serve(env.context, c.Name)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// connect resolves the instance and returns a control client for it. A
// run.json whose supervisor is gone (the PID is dead or reused) is Failed
// not_running with detail phase "dead"; a live supervisor whose socket does not
// answer surfaces later as control_unreachable (exit 2).
func connect(name string) (instance.Paths, *control.Client, error) {
	p, err := resolvePaths(name)
	if err != nil {
		return instance.Paths{}, nil, err
	}
	if err := checkPrivateDir(filepath.Dir(p.RunDir)); err != nil {
		return instance.Paths{}, nil, err
	}
	rf, err := instance.LoadRunFile(p)
	if err != nil {
		return instance.Paths{}, nil, err
	}
	if !instance.ProcessAlive(rf, p.Name) {
		return instance.Paths{}, nil, apperr.Failed("not_running", "instance "+p.Name+" is not running (its supervisor is gone)",
			map[string]any{"instance": p.Name, "phase": control.PhaseDead, "run_dir": p.RunDir})
	}
	// Dial the socket of this instance's own run dir, not the path run.json
	// names: run.json decides whether the instance is live, never where to connect.
	return p, control.Dial(p.APISock()), nil
}

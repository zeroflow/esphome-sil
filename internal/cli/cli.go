// Package cli is the sil command line: a kong grammar over the lifecycle,
// control and supervisor packages.
//
// Contract (CLI-05): every command writes exactly one JSON value to stdout.
// Failures print {"error":{"code","message","detail"}} and exit 1 for
// user-fixable and 2 for infrastructure errors. Progress lines and slog go to
// stderr only. The hidden `serve` command is the detached supervisor; its
// stdout is sil.log, so it prints nothing.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
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
	Quiet   bool   `help:"No progress lines on stderr."`
}

// StatusCmd is `sil status`.
type StatusCmd struct {
	Name string `arg:"" optional:"" env:"SIL_INSTANCE" help:"Instance name."`
}

// DownCmd is `sil down`.
type DownCmd struct {
	Name string `arg:"" optional:"" env:"SIL_INSTANCE" help:"Instance name."`
}

// SimCmd groups the simulator operations.
type SimCmd struct {
	Restart SimRestartCmd `cmd:"" help:"Replace the simulator with a fresh one; the firmware keeps running and resyncs."`
}

// SimRestartCmd is `sil sim restart`.
type SimRestartCmd struct {
	Name string        `arg:"" optional:"" env:"SIL_INSTANCE" help:"Instance name."`
	Gap  time.Duration `help:"Keep the simulator away for this long before the new one starts (0 to 30s), e.g. 3s."`
}

// FwCmd groups the firmware operations.
type FwCmd struct {
	Restart FwRestartCmd `cmd:"" help:"Restart the firmware process (SIGTERM, then SIGKILL after 3s) with the same program and prefs."`
}

// FwRestartCmd is `sil fw restart`.
type FwRestartCmd struct {
	Name string `arg:"" optional:"" env:"SIL_INSTANCE" help:"Instance name."`
}

// ServeCmd is the hidden `sil serve`.
type ServeCmd struct {
	Name string `arg:"" help:"Instance name."`
}

// runEnv carries what commands hand back to Main.
type runEnv struct {
	result  any
	silent  bool
	stderr  io.Writer
	context context.Context
}

// exitSignal unwinds out of kong when it wants to exit (help, version).
type exitSignal int

// Main parses args, runs the command and returns the process exit code.
func Main(args []string) int {
	setupLogging()
	env := &runEnv{stderr: os.Stderr}

	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("sil"),
		kong.Description("Software-in-the-loop bench for ESPHome firmware."),
		kong.Writers(os.Stdout, os.Stderr),
		kong.Exit(func(code int) { panic(exitSignal(code)) }),
		kong.Bind(env),
	)
	if err != nil {
		return fail(apperr.Infra("internal", err.Error(), nil))
	}

	var kctx *kong.Context
	if code, exited := catchExit(func() { kctx, err = parser.Parse(args) }); exited {
		return code
	}
	if err != nil {
		return fail(apperr.Failed("usage", err.Error(), nil))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	env.context = ctx
	if err := kctx.Run(); err != nil {
		return fail(err)
	}
	if env.silent {
		return 0
	}
	if err := writeJSON(os.Stdout, env.result); err != nil {
		return fail(apperr.Infra("internal", "cannot write the result: "+err.Error(), nil))
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

// setupLogging sends slog to stderr: text on a terminal, JSON otherwise.
func setupLogging() {
	var h slog.Handler
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		h = slog.NewTextHandler(os.Stderr, nil)
	} else {
		h = slog.NewJSONHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// fail prints the error object on stdout and a one-line human message on
// stderr, and returns the exit code.
func fail(err error) int {
	ae := apperr.AsError(err)
	_ = writeJSON(os.Stdout, struct {
		Error *apperr.Error `json:"error"`
	}{ae})
	fmt.Fprintf(os.Stderr, "sil: %s: %s\n", ae.Code, ae.Message)
	return apperr.ExitCode(ae)
}

// Run implements `sil up`.
func (c *UpCmd) Run(env *runEnv) error {
	res, err := lifecycle.Up(env.context, lifecycle.UpOptions{
		Name: c.Name, ConfigPath: c.Config, APIPort: c.APIPort,
		Rebuild: c.Rebuild, Quiet: c.Quiet, Progress: env.stderr,
	})
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil status`: the CLI only reads the control API.
func (c *StatusCmd) Run(env *runEnv) error {
	p, err := resolve(c.Name)
	if err != nil {
		return err
	}
	rf, err := instance.ReadRunFile(p)
	if err != nil {
		return err
	}
	ctx, cancel := clock.WithTimeout(env.context, clock.NewReal(), statusBudget)
	defer cancel()
	st, err := control.Dial(rf.APISock).Status(ctx)
	if err != nil {
		return err
	}
	env.result = st
	return nil
}

// Run implements `sil down`.
func (c *DownCmd) Run(env *runEnv) error {
	p, err := resolve(c.Name)
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
	p, err := resolve(c.Name)
	if err != nil {
		return err
	}
	rf, err := instance.ReadRunFile(p)
	if err != nil {
		return err
	}
	st, err := control.Dial(rf.APISock).SimRestart(env.context, control.SimRestartRequest{GapS: c.Gap.Seconds()})
	if err != nil {
		return err
	}
	env.result = st
	return nil
}

// Run implements `sil fw restart`: the Status once the instance is ready again.
func (c *FwRestartCmd) Run(env *runEnv) error {
	p, err := resolve(c.Name)
	if err != nil {
		return err
	}
	rf, err := instance.ReadRunFile(p)
	if err != nil {
		return err
	}
	st, err := control.Dial(rf.APISock).FwRestart(env.context)
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

// resolve turns the optional name argument into Paths. 01-12 replaces the
// instance_required error with full name resolution.
func resolve(name string) (instance.Paths, error) {
	if name == "" {
		return instance.Paths{}, apperr.Failed("instance_required",
			"no instance name given; pass <name> or set SIL_INSTANCE", nil)
	}
	return instance.PathsFor(name)
}

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
	"bufio"
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
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/instance"
	"github.com/zeroflow/esphome-sil/internal/lifecycle"
	"github.com/zeroflow/esphome-sil/internal/model"
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
	Press  PressCmd  `cmd:"" help:"Press a button (usr1..usr3): 200 ms by default, --long holds 1500 ms, --hold DUR sets the hold; --down/--up set the level and return."`
	Set    SetCmd    `cmd:"" help:"Set inputs: key=value pairs (temperature in degrees Celsius, humidity in %RH, portN.fan=ID to plug a library fan into port N), validated first and applied together."`
	Fans   FansCmd   `cmd:"" help:"List the fan library: JSON on stdout, a provenance summary per fan on stderr (--verbose: every value)."`
	// History is registered next to Fans: both are read-only JSON lookups.
	History HistoryCmd `cmd:"" help:"Print the plot history: one duty and RPM sample per port every 100 ms of sim time, the last 5 minutes."`
	Watch   WatchCmd   `cmd:"" help:"Follow the instance live as NDJSON: one snapshot line, then one line per change, until it stops (Ctrl-C ends it too)."`
	Logs    LogsCmd    `cmd:"" help:"Print the last lines of the firmware's and the simulator's logs (colour removed, the API key redacted); --follow keeps streaming."`
	Serve   ServeCmd   `cmd:"" hidden:"" help:"Run the detached supervisor (internal)."`
}

// UpCmd is `sil up`.
type UpCmd struct {
	Name    string `arg:"" help:"Instance name (a-z, 0-9, '-')."`
	Config  string `help:"Firmware YAML (default: examples/rev33.yaml of this checkout, the Rev 3.3 bench; examples/fan-skeleton.yaml is the minimal four-fan config)."`
	APIPort int    `name:"api-port" help:"Firmware native API port (default: the stored port, first 6054)."`
	WebPort int    `name:"web-port" help:"Web UI and HTTP API port (default: the stored port, first 8054)."`
	Listen  string `name:"listen" default:"0.0.0.0" help:"IP address the web UI and HTTP API listen on; 127.0.0.1 keeps them on this machine. They are unauthenticated."`
	// AllowHost is repeatable.
	AllowHost []string `name:"allow-host" help:"Also accept requests addressed to this host name (repeatable); IP addresses, localhost and this machine's name always work."`
	UIDir     string   `name:"ui-dir" help:"Serve the page from this directory instead of the binary (UI development)."`
	Rebuild   bool     `help:"Discard the previous build output of this instance first."`
	Fan       string   `help:"Library fan plugged into all four ports (default arctic-p12-pwm-pst); change one port later with: sil set portN.fan=ID."`
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

// PressCmd is `sil press`. --instance/-i is the instance selector of the verbs
// that take further arguments (the positional slot is taken by the button).
type PressCmd struct {
	Button   string         `arg:"" help:"Button: usr1, usr2 or usr3."`
	Long     bool           `xor:"mode" help:"Hold 1500 ms (past the product's 1000 ms long-press threshold)."`
	Hold     *time.Duration `xor:"mode" help:"Hold for this long (1ms to 60s), e.g. 750ms."`
	Down     bool           `xor:"mode" help:"Hold the button down and return at once."`
	Up       bool           `xor:"mode" help:"Release the button and return at once."`
	MaxHold  *time.Duration `name:"max-hold" help:"With --down: release by itself after this long of sim time (1ms to 60s), e.g. 10s. The web page always sets one."`
	Instance string         `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// SetCmd is `sil set`. Like press, the instance is selected with --instance/-i.
type SetCmd struct {
	Pairs    []string `arg:"" name:"key=value" help:"Input to set, e.g. temperature=24.5 humidity=40."`
	Instance string   `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// FansCmd is `sil fans`. Like set, the instance is selected with --instance/-i.
type FansCmd struct {
	Verbose  bool   `help:"Also list every value with its provenance and source (stderr)."`
	Instance string `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// HistoryCmd is `sil history`. Like set, the instance is selected with --instance/-i.
type HistoryCmd struct {
	Window   time.Duration `help:"Only the last D of sim time, up to 5m, e.g. 60s; default the whole 5 minutes."`
	Instance string        `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// WatchCmd is `sil watch`. Like press, the instance is selected with --instance/-i. It is a
// streaming command: it writes NDJSON lines to stdout as they arrive instead of one JSON value.
type WatchCmd struct {
	Kind     []string `sep:"," help:"Only these event kinds, comma separated (duty, rpm, state, fan, button, env, leds, firmware, sim; sample is opt-in: 10 events a second, only when named); the snapshot, lagged and stopped lines always come."`
	Count    int      `help:"Exit 0 after this many event lines following the snapshot (0 = until the instance stops)."`
	Instance string   `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// LogsCmd is `sil logs`. Like press, the instance is selected with --instance/-i.
type LogsCmd struct {
	Source   string `enum:"firmware,sim,all" default:"firmware" help:"Which log: firmware (firmware.log), sim (sil.log) or all (firmware lines first)."`
	Tail     int    `default:"100" help:"Lines per source (1 to 10000)."`
	Follow   bool   `help:"After the tail, stream every new line as NDJSON {\"src\",\"line\"} until the instance stops (Ctrl-C ends it too)."`
	Raw      bool   `help:"Print the bare lines as plain text with the colour sequences kept, instead of JSON."`
	Instance string `name:"instance" short:"i" help:"Instance name (default: $SIL_INSTANCE, else the single live instance)."`
}

// ServeCmd is the hidden `sil serve`.
type ServeCmd struct {
	Name string `arg:"" help:"Instance name."`
}

// runEnv carries what commands hand back to Execute.
type runEnv struct {
	result any
	// silent: the command wrote its own output (streaming commands, serve); Execute prints no
	// result value after a successful run.
	silent  bool
	quiet   bool
	stdout  io.Writer
	stderr  io.Writer
	context context.Context
}

// exitSignal unwinds out of kong when it wants to exit (help, version).
type exitSignal int

// Main parses args, runs the command and returns the process exit code.
func Main(args []string) int { return Execute(args, os.Stdout, os.Stderr) }

// Execute is Main with explicit output streams (the tests capture them).
func Execute(args []string, stdout, stderr io.Writer) int {
	env := &runEnv{stdout: stdout, stderr: stderr}

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
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
	res, err := lifecycle.Up(env.context, c.options(env))
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// options maps the parsed flags to the lifecycle inputs.
func (c *UpCmd) options(env *runEnv) lifecycle.UpOptions {
	return lifecycle.UpOptions{
		Name: c.Name, ConfigPath: c.Config, APIPort: c.APIPort,
		WebPort: c.WebPort, Listen: c.Listen, AllowHosts: c.AllowHost, UIDir: c.UIDir,
		Rebuild: c.Rebuild, Fan: c.Fan, Quiet: env.quiet, Progress: env.stderr,
	}
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
	// Same gate as connect: an unsafe runtime base must not steer the
	// signalling fallback or the socket dial (T-01-17).
	if err := checkPrivateDir(filepath.Dir(p.RunDir)); err != nil {
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

// Run implements `sil press`: the PressResult once the release edge was sent
// (--down and --up answer at once).
func (c *PressCmd) Run(env *runEnv) error {
	if c.MaxHold != nil && !c.Down {
		return apperr.Failed("invalid_argument", "--max-hold needs --down", nil)
	}
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	var res control.PressResult
	switch {
	case c.Down, c.Up:
		var req control.DownRequest
		if c.MaxHold != nil {
			ms := c.MaxHold.Milliseconds() // an explicit 0s reaches the server as 0 and fails validation there
			req.MaxHoldMS = &ms
		}
		res, err = client.SetButton(env.context, c.Button, c.Down, req)
	default:
		hold := control.ShortPress
		if c.Long {
			hold = control.LongPress
		}
		if c.Hold != nil {
			hold = *c.Hold // an explicit --hold 0s reaches the server as hold_ms 0 and fails validation
		}
		res, err = client.Press(env.context, c.Button, control.PressRequest{HoldMS: hold.Milliseconds()})
	}
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil set`: the pairs are split at the first '=' and sent as JSON strings exactly
// as typed; the server validates all of them and applies them together.
func (c *SetCmd) Run(env *runEnv) error {
	if len(c.Pairs) == 0 {
		return apperr.Failed("invalid_argument", "set needs at least one key=value pair, e.g. temperature=24.5", nil)
	}
	values := make(map[string]json.RawMessage, len(c.Pairs))
	for _, pair := range c.Pairs {
		key, val, ok := strings.Cut(pair, "=")
		if !ok || key == "" || val == "" {
			return apperr.Failed("invalid_argument", fmt.Sprintf("%q is not a key=value pair with a key and a value", pair), map[string]any{"pair": pair})
		}
		if _, dup := values[key]; dup {
			return apperr.Failed("invalid_argument", fmt.Sprintf("key %q is given twice", key), map[string]any{"key": key})
		}
		raw, err := json.Marshal(val)
		if err != nil {
			return apperr.Failed("invalid_argument", fmt.Sprintf("%q cannot be sent: %v", pair, err), map[string]any{"pair": pair})
		}
		values[key] = raw
	}
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	res, err := client.Set(env.context, control.SetRequest{Values: values})
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil fans`: the library of the instance's own supervisor, so SIL_FAN_DIR of that
// instance decides, not of this process.
func (c *FansCmd) Run(env *runEnv) error {
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	res, err := client.Fans(env.context)
	if err != nil {
		return err
	}
	env.result = res
	writeFansSummary(env.stderr, res, c.Verbose)
	return nil
}

// writeFansSummary prints the human view of `sil fans` to w (stderr, discarded by --quiet): one line
// per fan and, with verbose, one line per value in model.ValuePaths order. It is text only: a source
// is printed, never opened or fetched.
func writeFansSummary(w io.Writer, res control.FansResult, verbose bool) {
	for _, f := range res.Fans {
		if f.Provenance == nil {
			fmt.Fprintf(w, "%s  %s  schema %d  no per-value provenance (flat v0.1.0 file)\n", f.ID, f.Name, f.Schema)
			continue
		}
		fmt.Fprintf(w, "%s  %s  schema %d  measured %d · datasheet %d · assumed %d · unobservable %d\n",
			f.ID, f.Name, f.Schema, f.Provenance.Measured, f.Provenance.Datasheet, f.Provenance.Assumed, f.Provenance.Unobservable)
		if !verbose {
			continue
		}
		for _, path := range model.ValuePaths {
			v, ok := f.Values[path]
			if !ok {
				continue
			}
			line := fmt.Sprintf("    %-22s%s", path, v.Kind)
			if v.Source != nil {
				line += "  " + *v.Source
				if text, ok := f.Sources[*v.Source]; ok {
					line += " " + text
				}
			}
			fmt.Fprintln(w, line)
		}
	}
}

// Run implements `sil history`: the supervisor's sample ring as one JSON value. An out-of-range
// window is rejected by the server (invalid_argument), like --gap and --hold.
func (c *HistoryCmd) Run(env *runEnv) error {
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	res, err := client.History(env.context, control.HistoryRequest{WindowS: c.Window.Seconds()})
	if err != nil {
		return err
	}
	env.result = res
	return nil
}

// Run implements `sil watch`: the lines of the stream are copied to stdout as they arrive.
func (c *WatchCmd) Run(env *runEnv) error {
	env.silent = true
	if c.Count < 0 {
		return apperr.Failed("invalid_argument", "--count must not be negative", map[string]any{"count": c.Count})
	}
	req := control.WatchRequest{Kinds: c.Kind}
	if err := req.Validate(); err != nil {
		return err
	}
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	return relayStream(env.context, client, req.WatchPath(), env.stdout, 1, c.Count, nil)
}

// Run implements `sil logs`: one JSON value, or with --raw bare lines; with --follow an NDJSON stream.
func (c *LogsCmd) Run(env *runEnv) error {
	req := control.LogsRequest{Source: c.Source, Tail: c.Tail, Raw: c.Raw}
	if err := req.Validate(); err != nil {
		return err
	}
	_, client, err := connect(c.Instance)
	if err != nil {
		return err
	}
	if c.Follow {
		env.silent = true
		var show func([]byte) []byte
		if c.Raw {
			show = rawLogLine
		}
		return relayStream(env.context, client, req.LogsPath(true), env.stdout, 0, 0, show)
	}
	res, err := client.Logs(env.context, req)
	if err != nil {
		return err
	}
	if !c.Raw {
		env.result = res
		return nil
	}
	env.silent = true
	var out bytes.Buffer
	for _, l := range res.Lines {
		out.WriteString(l.Line)
		out.WriteByte('\n')
	}
	if _, err := env.stdout.Write(out.Bytes()); err != nil {
		return apperr.Infra("internal", "cannot write the logs: "+err.Error(), nil)
	}
	return nil
}

// rawLogLine is the bare text of one {"src","line"} stream line; the stopped line prints nothing.
func rawLogLine(line []byte) []byte {
	var l control.LogLine
	if control.IsStoppedLine(line) || json.Unmarshal(line, &l) != nil {
		return nil
	}
	return append([]byte{}, l.Line...)
}

// streamer opens an NDJSON stream; *control.Client is the real one.
type streamer interface {
	Stream(ctx context.Context, path string) (io.ReadCloser, error)
}

// relayStream copies the NDJSON stream at path to out, one line at a time, as the lines arrive.
// show, when set, renders a line before it is written (nil writes it as it is; a nil result
// writes nothing for that line). The first skip
// lines (the snapshot) do not count towards count; with count > 0 it returns nil after that many
// lines following them. It returns nil after the stopped line and when ctx ends (Ctrl-C), and
// control_unreachable when the stream ends without a stopped line.
func relayStream(ctx context.Context, st streamer, path string, out io.Writer, skip, count int, show func(line []byte) []byte) error {
	rc, err := st.Stream(ctx, path)
	if err != nil {
		return err
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	sc.Split(control.ScanCompleteLines)
	seen := 0
	for sc.Scan() {
		line := sc.Bytes()
		stopped := control.IsStoppedLine(line)
		text := line
		if show != nil {
			text = show(line)
		}
		if text != nil {
			if _, err := out.Write(append(append([]byte(nil), text...), '\n')); err != nil {
				return apperr.Infra("internal", "cannot write the stream: "+err.Error(), nil)
			}
		}
		if stopped {
			return nil
		}
		seen++
		if count > 0 && seen-skip >= count {
			return nil
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return apperr.Infra("control_unreachable", "the stream ended without a stopped line: the connection to the instance was lost", nil)
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

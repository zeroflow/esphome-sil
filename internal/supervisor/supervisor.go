// Package supervisor is the body of the hidden `sil serve <name>` process
// (D-02): a thin supervisor that owns the firmware process group, the
// control API and the engine slot.
//
// The engine is a goroutine with fresh state per engine.New, not a child
// process. engine.Run owns bus.Listen and the unlink of the bus socket; the
// supervisor owns the lifecycle of both through the engine slot (start,
// cancel, wait for Run to return) together with the firmware process group.
//
// SimRestart replaces the engine, FwRestart the firmware process; both, and
// shutdown, are serialized by opMu. Status never takes opMu: it reads the
// published engine snapshot and a few fields under mu, so it keeps answering
// while a restart is in flight.
package supervisor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apiclient"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

// stopGrace is how long the firmware gets after SIGTERM before SIGKILL.
const stopGrace = 3 * time.Second

// Readiness probe policy (RESEARCH Pattern 4): after every firmware start the
// native API is probed every probeEvery for up to probeBudget; each attempt
// has its own deadline inside apiclient.Probe.
const (
	probeEvery  = 250 * time.Millisecond
	probeBudget = 20 * time.Second
)

// Restart policy: how long the firmware gets to resync after a simulator
// restart and how long it gets to become ready after its own restart.
const (
	resyncBudget  = 15 * time.Second
	readyBudget   = 30 * time.Second
	waitEvery     = 50 * time.Millisecond
	maxRestartGap = 30 * time.Second
)

// engineSlot is one engine instance: its cancel function and a channel that
// closes when Run has returned (listener closed, socket unlinked).
type engineSlot struct {
	eng    *engine.Engine
	cancel context.CancelFunc
	done   chan struct{}
	err    error // Run's result, valid once done is closed
}

// fwProc is one firmware process.
type fwProc struct {
	pid       int
	gone      chan struct{} // closed once the process has been reaped
	recorded  chan struct{} // closed once its exit has been recorded
	requested bool          // a stop was asked for (guarded by supervisor.mu)
}

type fwExit struct {
	proc  *fwProc
	state *os.ProcessState
	err   error
}

type supervisor struct {
	paths  instance.Paths
	id     instance.Identity
	spec   instance.Spec
	clk    clock.Clock
	log    *slog.Logger
	engCfg engine.Config

	fwExits chan fwExit // Wait results, consumed by the supervisor loop

	stopOnce sync.Once
	stopped  chan struct{} // closed when stopAll has finished

	// opMu serializes SimRestart, FwRestart and shutdown. downReq is closed
	// when a shutdown starts so that an operation in flight gives up at once.
	opMu     sync.Mutex
	downReq  chan struct{}
	downOnce sync.Once

	mu          sync.Mutex
	slot        *engineSlot
	engRestarts int
	fw          *fwProc
	fwPID       int
	fwRunning   bool
	restarts    int
	restarting  bool
	stopping    bool
	lastExit    *control.Exit
	failure     *apperr.Error

	// Native-API readiness (guarded by mu): apiReady is set by a probe that
	// completed a Noise handshake with this instance's key and matched its
	// name and MAC; apiFailure is the probe's verdict when it never will.
	apiReady    bool
	apiFailure  *apperr.Error
	probeGen    int
	probeCancel context.CancelFunc
	probeWG     sync.WaitGroup
}

// Serve runs the supervisor for instance name until `sil down` or a
// SIGTERM, SIGINT or SIGHUP arrives. All three take the same shutdown path.
func Serve(ctx context.Context, name string) error {
	p, err := instance.PathsFor(name)
	if err != nil {
		return err
	}
	id, err := instance.LoadOrCreate(p, 0)
	if err != nil {
		return err
	}
	spec, err := instance.ReadSpec(p)
	if err != nil {
		return err
	}
	var ports []engine.PortConfig
	for i, fanID := range spec.Fans {
		fan, err := fanlib.Load(fanID)
		if err != nil {
			return apperr.Failed("fan_toml_invalid", err.Error(), map[string]any{"fan": fanID})
		}
		ports = append(ports, engine.PortConfig{Port: i + 1, FanID: fanID, Fan: fan})
	}

	log := slog.Default().With("instance", name)
	clk := clock.NewReal()
	s := &supervisor{
		paths: p, id: id, spec: spec, clk: clk, log: log,
		engCfg: engine.Config{Clock: clk, BusPath: p.BusSock(), Ports: ports, Log: log},

		fwExits: make(chan fwExit, 8),
		stopped: make(chan struct{}),
		downReq: make(chan struct{}),
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stopSignals()
	return s.run(sigCtx)
}

func (s *supervisor) run(sigCtx context.Context) error {
	// Whatever happens below, the firmware group must not outlive us.
	defer s.shutdown()

	// Engine slot. The engine runs on its own context: shutdown stops the
	// firmware first and cancels the engine afterwards.
	sl, err := s.startEngine()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.slot = sl
	s.mu.Unlock()

	if err := s.startFirmware(); err != nil {
		return err
	}
	s.startProbe()

	ln, err := control.Listen(s.paths.APISock())
	if err != nil {
		return err
	}
	srvCtx, srvCancel := context.WithCancel(context.Background())
	srvDone := make(chan error, 1)
	go func() { srvDone <- control.ServeListener(srvCtx, ln, control.NewHandler(s)) }()

	// run.json only after the control socket accepts connections.
	if err := instance.WriteRunFile(s.paths, instance.RunFile{
		Schema: 1, Instance: s.id.Name, PID: os.Getpid(),
		RunDir: s.paths.RunDir, APISock: s.paths.APISock(), BusSock: s.paths.BusSock(),
	}); err != nil {
		srvCancel()
		<-srvDone
		return apperr.Infra("run_file_failed", "cannot write run.json", map[string]any{"error": err.Error()})
	}
	s.log.Info("instance serving", "pid", os.Getpid(), "api_sock", s.paths.APISock())

loop:
	for {
		select {
		case <-sigCtx.Done():
			s.log.Info("shutdown requested by signal")
			s.shutdown()
			break loop
		case <-s.stopped:
			break loop
		case ex := <-s.fwExits:
			s.recordExit(ex)
		}
	}

	_ = os.Remove(s.paths.RunJSON())
	srvCancel()
	<-srvDone
	s.log.Info("instance stopped")
	return nil
}

// startEngine builds a fresh engine (a new World, D-02), runs it and waits
// until it listens on the bus socket.
func (s *supervisor) startEngine() (*engineSlot, error) {
	eng := engine.New(s.engCfg)
	ctx, cancel := context.WithCancel(context.Background())
	sl := &engineSlot{eng: eng, cancel: cancel, done: make(chan struct{})}
	go func() {
		sl.err = eng.Run(ctx)
		close(sl.done)
	}()
	select {
	case <-eng.Ready():
		return sl, nil
	case <-sl.done:
		cancel()
		return nil, apperr.Infra("engine_failed", "the simulator engine did not start", map[string]any{"error": fmt.Sprint(sl.err)})
	}
}

// stopEngine cancels sl and waits until Run has returned: the listener is
// closed and the bus socket unlinked.
func (s *supervisor) stopEngine(sl *engineSlot) {
	if sl == nil {
		return
	}
	sl.cancel()
	<-sl.done
}

// startFirmware starts the firmware from a dedicated goroutine that locks
// its OS thread and never unlocks: Pdeathsig is tied to the spawning thread,
// and Go may retire unlocked threads. It makes the new process the current one.
func (s *supervisor) startFirmware() error {
	logf, err := os.OpenFile(s.paths.FirmwareLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return apperr.Infra("firmware_start_failed", "cannot open firmware.log", map[string]any{"error": err.Error()})
	}
	p := &fwProc{gone: make(chan struct{}), recorded: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread lives as long as the child
		defer logf.Close()
		cmd := exec.Command(s.spec.Program)
		cmd.Dir = s.paths.RunDir
		cmd.Env = append(os.Environ(),
			"ESPHOME_PREFDIR="+s.paths.PrefsDir(),
			"SIL_BUS="+s.paths.BusSock(),
		)
		cmd.Stdout, cmd.Stderr = logf, logf
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
		if err := cmd.Start(); err != nil {
			close(p.gone)
			started <- err
			return
		}
		p.pid = cmd.Process.Pid
		s.mu.Lock()
		s.fw, s.fwPID, s.fwRunning = p, p.pid, true
		s.mu.Unlock()
		started <- nil
		err := cmd.Wait()
		s.fwExits <- fwExit{proc: p, state: cmd.ProcessState, err: err}
		close(p.gone)
	}()
	if err := <-started; err != nil {
		return apperr.Infra("firmware_start_failed", "cannot start the firmware program", map[string]any{"program": s.spec.Program, "error": err.Error()})
	}
	return nil
}

// stopProc sends SIGTERM to the firmware process group (it flushes its
// preferences on SIGTERM), waits stopGrace on a clock timer, then SIGKILLs
// the group.
func (s *supervisor) stopProc(p *fwProc) {
	if p == nil || p.pid == 0 {
		return
	}
	select {
	case <-p.gone:
		return
	default:
	}
	_ = syscall.Kill(-p.pid, syscall.SIGTERM)
	timer := s.clk.NewTimer(stopGrace)
	select {
	case <-p.gone:
		timer.Stop()
	case <-timer.C():
		s.log.Warn("firmware ignored SIGTERM, killing its process group", "pid", p.pid)
		_ = syscall.Kill(-p.pid, syscall.SIGKILL)
		<-p.gone
	}
}

// shutdown is the one way to stop the instance (down request, signal, startup
// failure): it makes an operation in flight give up, waits for it, then stops
// the firmware group and the engine. Idempotent.
func (s *supervisor) shutdown() {
	s.downOnce.Do(func() { close(s.downReq) })
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.stopAll()
}

// stopAll stops the firmware group first, then the engine. Callers hold opMu
// (or run before any operation can); it is idempotent and concurrent callers
// wait for the first one to finish.
func (s *supervisor) stopAll() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		fw, sl := s.fw, s.slot
		s.mu.Unlock()
		s.stopProc(fw)
		s.stopEngine(sl)
		close(s.stopped)
	})
}

// startProbe (re)starts the readiness probe for the firmware that was just
// started: the previous probe, if any, is cancelled and api.ready is reset.
func (s *supervisor) startProbe() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	if s.probeCancel != nil {
		s.probeCancel()
	}
	s.probeGen++
	gen := s.probeGen
	s.apiReady, s.apiFailure = false, nil
	ctx, cancel := context.WithCancel(context.Background())
	s.probeCancel = cancel
	s.probeWG.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.probeWG.Done()
		s.probeLoop(ctx, gen)
	}()
}

// setAPI records the verdict of probe generation gen unless a newer probe
// has started since.
func (s *supervisor) setAPI(gen int, ready bool, failure *apperr.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.probeGen {
		return
	}
	s.apiReady, s.apiFailure = ready, failure
	if failure != nil {
		s.log.Error("native API readiness failed", "code", failure.Code, "message", failure.Message)
	} else if ready {
		s.log.Info("native API ready", "port", s.id.APIPort)
	}
}

// probeLoop probes 127.0.0.1:<api port> until the instance's own firmware
// answers (ready), something else answers (failed), or probeBudget elapses
// (ready_timeout). A TCP connect alone never counts.
func (s *supervisor) probeLoop(ctx context.Context, gen int) {
	key, err := base64.StdEncoding.DecodeString(s.id.Key)
	if err != nil {
		s.setAPI(gen, false, apperr.Infra("identity_corrupt", "the instance key is not valid base64", nil))
		return
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.id.APIPort))
	budget, cancel := clock.WithTimeout(ctx, s.clk, probeBudget)
	defer cancel()
	var last error
	for {
		h, err := apiclient.Probe(budget, addr, key, s.clk)
		if err == nil {
			if bad := mismatch(h, s.id); bad != nil {
				s.setAPI(gen, false, bad)
			} else {
				s.setAPI(gen, true, nil)
			}
			return
		}
		if ctx.Err() != nil {
			return // shutting down or superseded by a newer probe
		}
		var pe *apiclient.ProbeError
		if errors.As(err, &pe) {
			if pe.Kind == apiclient.KindPlaintextAPI || pe.Kind == apiclient.KindHandshakeRejected {
				s.setAPI(gen, false, apperr.Infra(pe.Kind, pe.Msg, map[string]any{"port": s.id.APIPort}))
				return
			}
			last = err
		}
		s.mu.Lock()
		running := s.fwRunning
		s.mu.Unlock()
		if !running {
			return // the exit is reported through the firmware status
		}
		if err := s.clk.Sleep(budget, probeEvery); err != nil {
			if ctx.Err() != nil {
				return
			}
			detail := map[string]any{"port": s.id.APIPort}
			if last != nil {
				detail["last_error"] = last.Error()
			}
			s.setAPI(gen, false, apperr.Infra("ready_timeout",
				fmt.Sprintf("the firmware's native API did not complete an encrypted handshake within %s", probeBudget), detail))
			return
		}
	}
}

// mismatch is nil when the hello names this instance's own device and MAC.
func mismatch(h apiclient.Hello, id instance.Identity) *apperr.Error {
	wantName, wantMAC := id.DeviceName(), instance.MAC(id.Name)
	if h.ServerName == wantName && h.Name == wantName && instance.EqualMAC(h.MAC, wantMAC) {
		return nil
	}
	return apperr.Infra("api_identity_mismatch",
		fmt.Sprintf("the process answering on API port %d is not this instance's firmware (expected %s %s, got %s %s)",
			id.APIPort, wantName, wantMAC, h.ServerName, h.MAC),
		map[string]any{
			"port":     id.APIPort,
			"expected": map[string]any{"name": wantName, "mac": wantMAC},
			"got":      map[string]any{"server_name": h.ServerName, "name": h.Name, "mac": h.MAC},
		})
}

// recordExit classifies a firmware exit. An exit nobody asked for is
// unexpected: phase failed (01-11 adds auto-restart here).
func (s *supervisor) recordExit(ex fwExit) {
	defer close(ex.proc.recorded)
	exit := &control.Exit{AtS: s.clk.Now().Seconds()}
	if ex.state != nil {
		if ws, ok := ex.state.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				exit.Signal = ws.Signal().String()
			} else {
				code := ws.ExitStatus()
				exit.Code = &code
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fw == ex.proc {
		s.fwRunning = false
	}
	requested := s.stopping || ex.proc.requested
	switch {
	case requested:
		exit.Kind = "requested"
	case exit.Code != nil && *exit.Code == 0:
		exit.Kind = "rebooted"
	default:
		exit.Kind = "crashed"
	}
	s.lastExit = exit
	if !requested {
		s.failure = apperr.Infra("firmware_exited", "the firmware process exited unexpectedly",
			map[string]any{"kind": exit.Kind, "code": exit.Code, "signal": exit.Signal})
		s.log.Error("firmware exited unexpectedly", "kind", exit.Kind, "code", exit.Code, "signal", exit.Signal)
	}
}

// Status implements control.Control.
func (s *supervisor) Status(_ context.Context) (control.Status, error) {
	s.mu.Lock()
	snap := s.slot.eng.Snapshot()
	pid, running, stopping, restarting := s.fwPID, s.fwRunning, s.stopping, s.restarting
	lastExit, failure := s.lastExit, s.failure
	apiReady, apiFailure := s.apiReady, s.apiFailure
	restarts, engRestarts := s.restarts, s.engRestarts
	s.mu.Unlock()
	if failure == nil && apiFailure != nil {
		failure = apiFailure
	}

	phase := control.PhaseStarting
	switch {
	case stopping:
		phase = control.PhaseStopping
	case failure != nil || (!running && !restarting):
		phase = control.PhaseFailed
	case apiReady && snap.BusConnected && !restarting:
		phase = control.PhaseReady
	}
	st := control.Status{
		Schema:   control.SchemaVersion,
		Instance: s.id.Name,
		Phase:    phase,
		API: control.APIStatus{
			DeviceName: s.id.DeviceName(), MAC: instance.MAC(s.id.Name),
			Host: "127.0.0.1", Port: s.id.APIPort, Key: s.id.Key,
			Ready: apiReady, // Noise handshake with the instance key and name/MAC match
		},
		Firmware: control.FirmwareStatus{PID: pid, Running: running, BusConnected: snap.BusConnected, Restarts: restarts, LastExit: lastExit},
		Sim:      control.SimStatus{TimeS: snap.SimTime.Seconds(), TimeFactor: 1, EngineRestarts: engRestarts, BusProto: snap.BusProto},
		Ports:    make([]control.PortStatus, 0, len(snap.Ports)),
		Warnings: append([]string{}, snap.Warnings...),
		Error:    failure,
	}
	for _, p := range snap.Ports {
		st.Ports = append(st.Ports, control.PortStatus{Port: p.Port, Fan: p.Fan, Duty: p.Duty, RPM: p.RPM, State: p.State})
	}
	return st, nil
}

// Down implements control.Control: stop everything, then let the main loop
// answer and exit.
func (s *supervisor) Down(_ context.Context) (control.DownResult, error) {
	s.shutdown()
	return control.DownResult{Instance: s.id.Name, Stopped: true}, nil
}

// withDown derives a context that is also cancelled when a shutdown starts.
func (s *supervisor) withDown(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-s.downReq:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// errStopping is what an operation answers when the instance is going down.
func errStopping() error {
	return apperr.Failed("stopping", "the instance is shutting down", nil)
}

// waitUntil polls cond every waitEvery on the supervisor clock until it holds
// (nil), ctx ends (its error) or budget elapses (errBudget).
var errBudget = errors.New("budget elapsed")

func (s *supervisor) waitUntil(ctx context.Context, budget time.Duration, cond func() (bool, error)) error {
	wctx, cancel := clock.WithTimeout(ctx, s.clk, budget)
	defer cancel()
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if err := s.clk.Sleep(wctx, waitEvery); err != nil {
			if clock.TimedOut(wctx) {
				return errBudget
			}
			return ctx.Err()
		}
	}
}

// SimRestart implements control.Control (D-02): the engine is cancelled and
// replaced by a fresh one on the same bus path after gap; the firmware keeps
// running and resyncs on its own.
func (s *supervisor) SimRestart(ctx context.Context, r control.SimRestartRequest) (control.Status, error) {
	if err := r.Validate(); err != nil {
		return control.Status{}, err
	}
	gap := time.Duration(r.GapS * float64(time.Second))
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.isStopping() {
		return control.Status{}, errStopping()
	}
	dctx, dcancel := s.withDown(context.Background())
	defer dcancel()
	rctx, rcancel := s.withDown(ctx)
	defer rcancel()

	s.mu.Lock()
	old := s.slot
	s.engRestarts++
	s.mu.Unlock()
	s.stopEngine(old)
	s.log.Info("simulator engine stopped for a restart", "gap", gap)
	if gap > 0 {
		if err := s.clk.Sleep(dctx, gap); err != nil {
			return control.Status{}, errStopping()
		}
	}
	sl, err := s.startEngine()
	if err != nil {
		return control.Status{}, err
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		s.stopEngine(sl)
		return control.Status{}, errStopping()
	}
	s.slot = sl
	s.mu.Unlock()

	err = s.waitUntil(rctx, resyncBudget, func() (bool, error) {
		snap := sl.eng.Snapshot()
		if snap.BusConnected && snap.Synced {
			return true, nil
		}
		s.mu.Lock()
		running, restarting := s.fwRunning, s.restarting
		s.mu.Unlock()
		if !running && !restarting {
			return false, apperr.Failed("firmware_not_running", "the firmware is not running, so it cannot resync", nil)
		}
		return false, nil
	})
	switch {
	case errors.Is(err, errBudget):
		snap := sl.eng.Snapshot()
		return control.Status{}, apperr.Infra("resync_timeout",
			fmt.Sprintf("the firmware did not resync with the new simulator within %s", resyncBudget),
			map[string]any{"bus_connected": snap.BusConnected, "synced": snap.Synced, "warnings": snap.Warnings})
	case err != nil:
		if s.isStopping() || isDown(s.downReq) {
			return control.Status{}, errStopping()
		}
		return control.Status{}, err
	}
	return s.Status(ctx)
}

func isDown(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (s *supervisor) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

// FwRestart implements control.Control (D-05): SIGTERM (SIGKILL after the
// grace period), then the same program with the same environment and prefs.
func (s *supervisor) FwRestart(ctx context.Context) (control.Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.isStopping() {
		return control.Status{}, errStopping()
	}
	rctx, rcancel := s.withDown(ctx)
	defer rcancel()

	s.mu.Lock()
	old := s.fw
	s.restarting = true
	s.apiReady, s.apiFailure, s.failure = false, nil, nil
	if old != nil {
		old.requested = true
	}
	s.mu.Unlock()
	// restarting hides the gap between the old exit and the new start from the
	// phase; it is cleared as soon as the new process exists (or the restart fails).
	clearRestarting := func() {
		s.mu.Lock()
		s.restarting = false
		s.mu.Unlock()
	}
	defer clearRestarting()

	s.stopProc(old)
	if old != nil {
		// Wait for the main loop to record the exit (last_exit.kind requested).
		tm := s.clk.NewTimer(5 * time.Second)
		select {
		case <-old.recorded:
			tm.Stop()
		case <-tm.C():
			s.log.Warn("the exit of the old firmware was not recorded in time")
		}
	}
	if s.isStopping() || isDown(s.downReq) {
		return control.Status{}, errStopping()
	}
	s.mu.Lock()
	s.restarts++
	s.mu.Unlock()
	if err := s.startFirmware(); err != nil {
		return control.Status{}, err
	}
	s.startProbe()
	clearRestarting()
	s.log.Info("firmware restarted", "restarts", s.restartCount())

	var last control.Status
	err := s.waitUntil(rctx, readyBudget, func() (bool, error) {
		st, _ := s.Status(rctx)
		last = st
		if st.Phase == control.PhaseReady {
			return true, nil
		}
		if st.Error != nil {
			return false, st.Error
		}
		return false, nil
	})
	switch {
	case errors.Is(err, errBudget):
		return control.Status{}, apperr.Infra("ready_timeout",
			fmt.Sprintf("the restarted firmware was not ready within %s", readyBudget),
			map[string]any{"phase": last.Phase, "api_ready": last.API.Ready, "bus_connected": last.Firmware.BusConnected})
	case err != nil:
		if s.isStopping() || isDown(s.downReq) {
			return control.Status{}, errStopping()
		}
		return control.Status{}, err
	}
	return last, nil
}

func (s *supervisor) restartCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

var _ control.Control = (*supervisor)(nil)

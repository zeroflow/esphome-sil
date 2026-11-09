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
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	proc      Process
	pid       int
	gone      chan struct{} // closed once the process has been reaped
	recorded  chan struct{} // closed once its exit has been recorded
	requested bool          // a stop was asked for (guarded by supervisor.mu)
}

type fwExit struct {
	proc *fwProc
	info ExitInfo
}

type supervisor struct {
	paths  instance.Paths
	id     instance.Identity
	spec   instance.Spec
	clk    clock.Clock
	log    *slog.Logger
	engCfg engine.Config

	starter Starter     // launches firmware processes (ExecStarter, a fake in tests)
	guard   *CrashGuard // counts unrequested firmware exits (D-03)
	// probe runs the readiness probe of one generation; Serve sets probeLoop.
	probe func(ctx context.Context, gen int)

	fwExits  chan fwExit // Wait results, consumed by the supervisor loop
	loopDone chan struct{}
	engDied  chan error // an engine that was up has returned on its own
	starts   int        // firmware launches so far (guarded by mu)

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

		starter:  ExecStarter{},
		guard:    NewCrashGuard(clk),
		fwExits:  make(chan fwExit, 8),
		loopDone: make(chan struct{}),
		engDied:  make(chan error, 1),
		stopped:  make(chan struct{}),
		downReq:  make(chan struct{}),
	}
	s.probe = s.probeLoop

	sigCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stopSignals()
	return s.run(sigCtx)
}

func (s *supervisor) run(sigCtx context.Context) error {
	// run.json is written before anything is started, so a `sil up` that dies
	// meanwhile (SIGKILL) leaves a supervisor that `sil down` and the already
	// running check can still find. The control socket does not exist yet;
	// `sil down` then falls back to signalling the PID.
	rf := instance.RunFile{
		Schema: 1, Instance: s.id.Name, PID: os.Getpid(),
		RunDir: s.paths.RunDir, APISock: s.paths.APISock(), BusSock: s.paths.BusSock(),
	}
	if err := instance.WriteRunFile(s.paths, rf); err != nil {
		return apperr.Infra("run_file_failed", "cannot write run.json", map[string]any{"error": err.Error()})
	}
	// Registered before the shutdown defer so it runs after it: run.json goes
	// away only once the firmware group is stopped, on every way out.
	defer func() { _ = os.Remove(s.paths.RunJSON()) }()

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

	s.log.Info("instance serving", "pid", os.Getpid(), "api_sock", s.paths.APISock())

	loopErr := s.mainLoop(sigCtx)

	// Every way out of the loop (down, signal, engine failure, panic) stops
	// the firmware group before run.json and the sockets go away (Pitfall 7).
	s.shutdown()
	_ = os.Remove(s.paths.RunJSON())
	srvCancel()
	<-srvDone
	s.log.Info("instance stopped")
	return loopErr
}

// mainLoop reacts to signals, firmware exits and an engine that died on its
// own until the instance is stopped. A panic is logged and ends the loop like
// any other failure, so the caller still runs the orderly shutdown.
func (s *supervisor) mainLoop(sigCtx context.Context) (err error) {
	defer close(s.loopDone)
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("supervisor loop panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = apperr.Infra("supervisor_panic", "the supervisor loop panicked", map[string]any{"panic": fmt.Sprint(r)})
		}
	}()
	for {
		select {
		case <-sigCtx.Done():
			s.log.Info("shutdown requested by signal")
			return nil
		case <-s.stopped:
			return nil
		case ee := <-s.engDied:
			s.log.Error("the simulator engine stopped on its own", "error", ee)
			return apperr.Infra("engine_failed", "the simulator engine stopped unexpectedly", map[string]any{"error": fmt.Sprint(ee)})
		case ex := <-s.fwExits:
			if s.recordExit(ex) {
				go s.autoRestart(ex.proc)
			}
		}
	}
}

// startEngine builds a fresh engine (a new World, D-02), runs it and waits
// until it listens on the bus socket.
func (s *supervisor) startEngine() (*engineSlot, error) {
	eng := engine.New(s.engCfg)
	ctx, cancel := context.WithCancel(context.Background())
	sl := &engineSlot{eng: eng, cancel: cancel, done: make(chan struct{})}
	var up atomic.Bool // set once the engine listens
	go func() {
		sl.err = eng.Run(ctx)
		// An engine that was up and returns without being cancelled is a failure.
		if up.Load() && ctx.Err() == nil {
			select {
			case s.engDied <- sl.err:
			default:
			}
		}
		close(sl.done)
	}()
	select {
	case <-eng.Ready():
		up.Store(true)
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

// startSpec is the launch description of the firmware: always the same
// program, preferences and environment, so a restart is a reboot of the same
// device.
func (s *supervisor) startSpec(n int) StartSpec {
	return StartSpec{
		Program: s.spec.Program,
		Env: append(os.Environ(),
			"ESPHOME_PREFDIR="+s.paths.PrefsDir(),
			"SIL_BUS="+s.paths.BusSock(),
		),
		LogPath: s.paths.FirmwareLog(),
		Dir:     s.paths.RunDir,
		Start:   n,
	}
}

// startFirmware launches the firmware through the Starter, makes the new
// process the current one and watches it until it is reaped.
func (s *supervisor) startFirmware() error {
	s.mu.Lock()
	s.starts++
	n := s.starts
	s.mu.Unlock()
	proc, err := s.starter.Start(s.startSpec(n))
	if err != nil {
		return apperr.Infra("firmware_start_failed", "cannot start the firmware program", map[string]any{"program": s.spec.Program, "error": err.Error()})
	}
	p := &fwProc{proc: proc, pid: proc.PID(), gone: make(chan struct{}), recorded: make(chan struct{})}
	s.mu.Lock()
	s.fw, s.fwPID, s.fwRunning = p, p.pid, true
	s.mu.Unlock()
	go func() {
		info := proc.Wait()
		close(p.gone)
		select {
		case s.fwExits <- fwExit{proc: p, info: info}:
		case <-s.loopDone:
		}
	}()
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
	_ = p.proc.SignalGroup(syscall.SIGTERM)
	timer := s.clk.NewTimer(stopGrace)
	select {
	case <-p.gone:
		timer.Stop()
	case <-timer.C():
		s.log.Warn("firmware ignored SIGTERM, killing its process group", "pid", p.pid)
		_ = p.proc.SignalGroup(syscall.SIGKILL)
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
		cancelProbe := s.probeCancel
		s.mu.Unlock()
		// The probe would otherwise keep dialling the dead API port and report
		// a ready_timeout for an instance that is already stopping.
		if cancelProbe != nil {
			cancelProbe()
		}
		s.stopProc(fw)
		s.stopEngine(sl)
		s.probeWG.Wait()
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
		s.probe(ctx, gen)
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
			// Literal codes at the call sites, so the registry scan (apperr/codes_test.go) sees them.
			switch pe.Kind {
			case apiclient.KindPlaintextAPI:
				s.setAPI(gen, false, apperr.Infra("plaintext_api", pe.Msg, map[string]any{"port": s.id.APIPort}))
				return
			case apiclient.KindHandshakeRejected:
				s.setAPI(gen, false, apperr.Infra("handshake_rejected", pe.Msg, map[string]any{"port": s.id.APIPort}))
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

// recordExit records a firmware exit: last_exit, and for an unrequested exit
// the crash guard. It reports whether the firmware should be started again
// (D-03); the caller then runs autoRestart. An exit nobody asked for that
// trips the guard marks the instance failed with firmware_crash_loop and does
// not restart.
func (s *supervisor) recordExit(ex fwExit) (restart bool) {
	defer close(ex.proc.recorded)
	s.mu.Lock()
	defer s.mu.Unlock()
	info := ex.info
	info.Requested = s.stopping || ex.proc.requested
	exit := &control.Exit{Kind: Classify(info), Code: info.Code, Signal: info.Signal, AtS: s.clk.Now().Seconds()}
	if s.fw == ex.proc {
		s.fwRunning = false
	}
	s.lastExit = exit
	if info.Requested {
		return false
	}
	s.log.Error("firmware exited unexpectedly", "kind", exit.Kind, "code", exit.Code, "signal", exit.Signal)
	if s.guard.Record() {
		s.failure = apperr.Failed("firmware_crash_loop",
			fmt.Sprintf("the firmware exited %d times within %s; not restarting it", crashLimit, crashWindow),
			map[string]any{"last_exit": exit, "window_s": int(crashWindow / time.Second), "limit": crashLimit})
		s.log.Error("firmware crash loop, giving up", "limit", crashLimit, "window", crashWindow)
		return false
	}
	s.restarting = true // hides the gap until the new process exists
	return true
}

// autoRestart starts the firmware again after an unrequested exit and
// re-runs the readiness probe. It serializes with FwRestart and shutdown on
// opMu and gives up when the exited process is no longer the current one (a
// restart got there first) or the instance is going down.
func (s *supervisor) autoRestart(old *fwProc) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	clearRestarting := func() {
		s.mu.Lock()
		s.restarting = false
		s.mu.Unlock()
	}
	s.mu.Lock()
	skip := s.stopping || s.fw != old
	s.mu.Unlock()
	if skip || isDown(s.downReq) {
		clearRestarting()
		return
	}
	s.mu.Lock()
	s.restarts++
	s.apiReady, s.apiFailure = false, nil
	s.mu.Unlock()
	if err := s.startFirmware(); err != nil {
		s.mu.Lock()
		s.failure = apperr.AsError(err)
		s.restarting = false
		s.mu.Unlock()
		s.log.Error("cannot restart the firmware", "error", err)
		return
	}
	s.startProbe()
	clearRestarting()
	s.log.Info("firmware restarted after an unrequested exit", "restarts", s.restartCount())
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
		Buttons:  make(map[string]bool, len(engine.ButtonIDs)),
		Warnings: append([]string{}, snap.Warnings...),
		Error:    failure,
	}
	for _, p := range snap.Ports {
		st.Ports = append(st.Ports, control.PortStatus{Port: p.Port, Fan: p.Fan, Duty: p.Duty, RPM: p.RPM, State: p.State})
	}
	for _, id := range engine.ButtonIDs {
		st.Buttons[id] = snap.Buttons[id]
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
	// Counted when the old engine goes away: status shows it during the gap.
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
		// The slot still holds the engine that was just stopped; record the
		// failure so Status reports it instead of "starting" forever. An
		// existing failure (a firmware crash loop) is the root diagnosis and
		// is kept: the single slot must not lose it to engine_failed.
		s.mu.Lock()
		if s.failure == nil {
			s.failure = apperr.AsError(err)
		}
		s.mu.Unlock()
		return control.Status{}, err
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		s.stopEngine(sl)
		return control.Status{}, errStopping()
	}
	s.slot = sl
	if s.failure != nil && s.failure.Code == "engine_failed" {
		s.failure = nil
	}
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

// currentEngine is the engine in the slot right now.
func (s *supervisor) currentEngine() *engine.Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slot.eng
}

// checkButton answers invalid_argument unless id is one of the buttons.
func checkButton(id string) error {
	for _, k := range engine.ButtonIDs {
		if id == k {
			return nil
		}
	}
	return apperr.Failed("invalid_argument", fmt.Sprintf("unknown button %q; known: %s", id, strings.Join(engine.ButtonIDs, ", ")),
		map[string]any{"button": id, "known": engine.ButtonIDs})
}

// buttonErr maps an engine sentinel to its CLI error. Literal codes at the
// call sites, so the registry scan sees them. A context error and anything
// foreign pass through unchanged.
func buttonErr(id string, err error) error {
	detail := map[string]any{"button": id}
	switch {
	case errors.Is(err, engine.ErrNotConnected), errors.Is(err, engine.ErrStopped):
		return apperr.Failed("firmware_not_connected", "the firmware is not connected to the simulator, so the button cannot be pressed now", detail)
	case errors.Is(err, engine.ErrInputNotAvailable):
		return apperr.Failed("input_not_available", "the firmware has no input "+id+" on the sim-bus", detail)
	case errors.Is(err, engine.ErrButtonBusy):
		return apperr.Failed("button_busy", "button "+id+" is already held", detail)
	case errors.Is(err, engine.ErrButtonNotHeld):
		return apperr.Failed("button_not_held", "button "+id+" is not held", detail)
	}
	return err
}

// Press implements control.Control (D-01, D-02, D-04): the button is held for
// r.HoldMS of sim clock time, then released; it returns once the release edge
// was handed to the bus. The release always happens, also when the caller went
// away during the hold, so a press never leaves a button stuck.
func (s *supervisor) Press(ctx context.Context, id string, r control.PressRequest) (control.PressResult, error) {
	if err := checkButton(id); err != nil {
		return control.PressResult{}, err
	}
	if err := r.Validate(); err != nil {
		return control.PressResult{}, err
	}
	if s.isStopping() {
		return control.PressResult{}, errStopping()
	}
	eng := s.currentEngine()
	down, err := eng.SetButton(ctx, id, true, true)
	if err != nil {
		return control.PressResult{}, buttonErr(id, err)
	}
	pctx, cancel := s.withDown(ctx)
	defer cancel()
	sleepErr := s.clk.Sleep(pctx, time.Duration(r.HoldMS)*time.Millisecond)
	// The release goes to whichever engine is in the slot now. A replaced
	// engine (sim restart) left a fresh World with the button released, and a
	// stopped one answers ErrStopped at once: both count as released.
	up, err := s.currentEngine().SetButton(context.Background(), id, false, false)
	if err != nil {
		if !errors.Is(err, engine.ErrStopped) {
			return control.PressResult{}, buttonErr(id, err)
		}
		up = s.clk.Now()
	}
	if sleepErr != nil {
		if s.isStopping() || isDown(s.downReq) {
			return control.PressResult{}, errStopping()
		}
		return control.PressResult{}, sleepErr
	}
	return control.PressResult{
		Button: id, Action: "press", HoldMS: r.HoldMS, Level: false,
		TDownUS: down.Microseconds(), TUpUS: up.Microseconds(),
	}, nil
}

// SetButton implements control.Control (D-03): one strict edge, no hold. A
// hold on a held button is button_busy, a release of a released one
// button_not_held; nothing is queued.
func (s *supervisor) SetButton(ctx context.Context, id string, down bool) (control.PressResult, error) {
	if err := checkButton(id); err != nil {
		return control.PressResult{}, err
	}
	if s.isStopping() {
		return control.PressResult{}, errStopping()
	}
	t, err := s.currentEngine().SetButton(ctx, id, down, true)
	if err != nil {
		return control.PressResult{}, buttonErr(id, err)
	}
	res := control.PressResult{Button: id, Action: "up", Level: down}
	if down {
		res.Action = "down"
		res.TDownUS = t.Microseconds()
	} else {
		res.TUpUS = t.Microseconds()
	}
	return res, nil
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
// The stop is requested, so it never counts towards the crash guard.
func (s *supervisor) FwRestart(ctx context.Context) (control.Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.isStopping() {
		return control.Status{}, errStopping()
	}
	rctx, rcancel := s.withDown(ctx)
	defer rcancel()

	if err := s.cycleFirmware(); err != nil {
		return control.Status{}, err
	}

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

// cycleFirmware stops the current firmware on request and starts the next
// one with a fresh readiness probe. The caller holds opMu.
func (s *supervisor) cycleFirmware() error {
	s.mu.Lock()
	old := s.fw
	s.restarting = true
	s.apiReady, s.apiFailure = false, nil
	// Only firmware-scoped failures (a crash loop, a failed auto-restart) are
	// dropped: an engine_failed failure stays until the engine is restarted,
	// since restarting the firmware does not bring the engine back.
	if s.failure != nil && s.failure.Code != "engine_failed" {
		s.failure = nil
	}
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
		case <-s.downReq:
			tm.Stop()
		case <-tm.C():
			s.log.Warn("the exit of the old firmware was not recorded in time")
		}
	}
	if s.isStopping() || isDown(s.downReq) {
		return errStopping()
	}
	s.mu.Lock()
	s.restarts++
	s.mu.Unlock()
	if err := s.startFirmware(); err != nil {
		return err
	}
	s.startProbe()
	clearRestarting()
	s.log.Info("firmware restarted", "restarts", s.restartCount())
	return nil
}

func (s *supervisor) restartCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

var _ control.Control = (*supervisor)(nil)

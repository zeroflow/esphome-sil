// Package supervisor is the body of the hidden `sil serve <name>` process
// (D-02): a thin supervisor that owns the firmware process group, the
// control API and the engine slot.
//
// The engine is a goroutine with fresh state per engine.New, not a child
// process. engine.Run owns bus.Listen and the unlink of the bus socket; the
// supervisor owns the lifecycle of both through the engine slot (start,
// cancel, wait for Run to return) together with the firmware process group.
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

type fwExit struct {
	state *os.ProcessState
	err   error
}

type supervisor struct {
	paths instance.Paths
	id    instance.Identity
	spec  instance.Spec
	clk   clock.Clock
	log   *slog.Logger
	eng   *engine.Engine

	engCancel context.CancelFunc
	engDone   chan error

	fwExits chan fwExit   // Wait results, consumed by the supervisor loop
	fwGone  chan struct{} // closed once the firmware process has been reaped

	stopOnce sync.Once
	stopped  chan struct{} // closed when stopAll has finished

	mu        sync.Mutex
	fwPID     int
	fwRunning bool
	stopping  bool
	lastExit  *control.Exit
	failure   *apperr.Error

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
		eng: engine.New(engine.Config{Clock: clk, BusPath: p.BusSock(), Ports: ports, Log: log}),

		fwExits: make(chan fwExit, 1),
		fwGone:  make(chan struct{}),
		stopped: make(chan struct{}),
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stopSignals()
	return s.run(sigCtx)
}

func (s *supervisor) run(sigCtx context.Context) error {
	// Whatever happens below, the firmware group must not outlive us.
	defer s.stopAll()

	// Engine slot. The engine runs on its own context: shutdown stops the
	// firmware first and cancels the engine afterwards.
	engCtx, engCancel := context.WithCancel(context.Background())
	s.engCancel = engCancel
	s.engDone = make(chan error, 1)
	go func() { s.engDone <- s.eng.Run(engCtx) }()
	select {
	case <-s.eng.Ready():
	case err := <-s.engDone:
		engCancel()
		return apperr.Infra("engine_failed", "the simulator engine did not start", map[string]any{"error": fmt.Sprint(err)})
	}

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
			s.stopAll()
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

// startFirmware starts the firmware from a dedicated goroutine that locks
// its OS thread and never unlocks: Pdeathsig is tied to the spawning thread,
// and Go may retire unlocked threads.
func (s *supervisor) startFirmware() error {
	logf, err := os.OpenFile(s.paths.FirmwareLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return apperr.Infra("firmware_start_failed", "cannot open firmware.log", map[string]any{"error": err.Error()})
	}
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
			close(s.fwGone)
			started <- err
			return
		}
		s.mu.Lock()
		s.fwPID, s.fwRunning = cmd.Process.Pid, true
		s.mu.Unlock()
		started <- nil
		err := cmd.Wait()
		s.fwExits <- fwExit{state: cmd.ProcessState, err: err}
		close(s.fwGone)
	}()
	if err := <-started; err != nil {
		return apperr.Infra("firmware_start_failed", "cannot start the firmware program", map[string]any{"program": s.spec.Program, "error": err.Error()})
	}
	return nil
}

// stopFirmware sends SIGTERM to the firmware process group (it flushes its
// preferences on SIGTERM), waits stopGrace on a clock timer, then SIGKILLs
// the group.
func (s *supervisor) stopFirmware() {
	s.mu.Lock()
	pid, running := s.fwPID, s.fwRunning
	s.mu.Unlock()
	if pid == 0 {
		return
	}
	select {
	case <-s.fwGone:
		return
	default:
	}
	if running {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	timer := s.clk.NewTimer(stopGrace)
	select {
	case <-s.fwGone:
		timer.Stop()
	case <-timer.C():
		s.log.Warn("firmware ignored SIGTERM, killing its process group", "pid", pid)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-s.fwGone
	}
}

// stopAll is the one shutdown path (down request, signal, startup failure):
// firmware group first, then the engine. Idempotent; concurrent callers wait
// for the first one to finish.
func (s *supervisor) stopAll() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		s.mu.Unlock()
		s.stopFirmware()
		if s.engCancel != nil {
			s.engCancel()
			<-s.engDone
		}
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

// recordExit classifies a firmware exit. An exit while not stopping is
// unexpected: phase failed (01-11 adds auto-restart here).
func (s *supervisor) recordExit(ex fwExit) {
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
	s.fwRunning = false
	switch {
	case s.stopping:
		exit.Kind = "requested"
	case exit.Code != nil && *exit.Code == 0:
		exit.Kind = "rebooted"
	default:
		exit.Kind = "crashed"
	}
	s.lastExit = exit
	if !s.stopping {
		s.failure = apperr.Infra("firmware_exited", "the firmware process exited unexpectedly",
			map[string]any{"kind": exit.Kind, "code": exit.Code, "signal": exit.Signal})
		s.log.Error("firmware exited unexpectedly", "kind", exit.Kind, "code", exit.Code, "signal", exit.Signal)
	}
}

// Status implements control.Control.
func (s *supervisor) Status(_ context.Context) (control.Status, error) {
	snap := s.eng.Snapshot()
	s.mu.Lock()
	pid, running, stopping, lastExit, failure := s.fwPID, s.fwRunning, s.stopping, s.lastExit, s.failure
	apiReady, apiFailure := s.apiReady, s.apiFailure
	s.mu.Unlock()
	if failure == nil && apiFailure != nil {
		failure = apiFailure
	}

	phase := control.PhaseStarting
	switch {
	case stopping:
		phase = control.PhaseStopping
	case failure != nil || !running:
		phase = control.PhaseFailed
	case apiReady && snap.BusConnected:
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
		Firmware: control.FirmwareStatus{PID: pid, Running: running, BusConnected: snap.BusConnected, LastExit: lastExit},
		Sim:      control.SimStatus{TimeS: snap.SimTime.Seconds(), TimeFactor: 1, BusProto: snap.BusProto},
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
	s.stopAll()
	return control.DownResult{Instance: s.id.Name, Stopped: true}, nil
}

var _ control.Control = (*supervisor)(nil)

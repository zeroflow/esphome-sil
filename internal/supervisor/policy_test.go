package supervisor

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

func intp(n int) *int { return &n }

func TestClassify(t *testing.T) {
	for name, tc := range map[string]struct {
		in   ExitInfo
		want string
	}{
		"requested wins over a signal": {ExitInfo{Signal: "SIGTERM", Requested: true}, "requested"},
		"requested with code":          {ExitInfo{Code: intp(0), Requested: true}, "requested"},
		"code 0":                       {ExitInfo{Code: intp(0)}, "rebooted"},
		"code 1":                       {ExitInfo{Code: intp(1)}, "crashed"},
		"code 139":                     {ExitInfo{Code: intp(139)}, "crashed"},
		"signal":                       {ExitInfo{Signal: "SIGKILL"}, "crashed"},
		"nothing known":                {ExitInfo{}, "crashed"},
	} {
		if got := Classify(tc.in); got != tc.want {
			t.Errorf("%s: Classify = %q, want %q", name, got, tc.want)
		}
	}
}

func TestSignalName(t *testing.T) {
	for sig, want := range map[syscall.Signal]string{syscall.SIGKILL: "SIGKILL", syscall.SIGSEGV: "SIGSEGV", syscall.SIGABRT: "SIGABRT", syscall.Signal(60): "SIG60"} {
		if got := SignalName(sig); got != want {
			t.Errorf("SignalName(%d) = %q, want %q", int(sig), got, want)
		}
	}
}

func TestCrashGuard(t *testing.T) {
	t.Run("five within 30 s trip on the fifth", func(t *testing.T) {
		clk := clock.NewManual()
		g := NewCrashGuard(clk)
		for i := 1; i <= 5; i++ {
			got := g.Record()
			if want := i == 5; got != want {
				t.Fatalf("Record #%d = %v, want %v", i, got, want)
			}
			clk.Advance(5 * time.Second)
		}
	})
	t.Run("five 15 s apart never trip", func(t *testing.T) {
		clk := clock.NewManual()
		g := NewCrashGuard(clk)
		for i := 1; i <= 12; i++ {
			if g.Record() {
				t.Fatalf("Record #%d tripped with exits 15 s apart", i)
			}
			clk.Advance(15 * time.Second)
		}
	})
	t.Run("old exits leave the window", func(t *testing.T) {
		clk := clock.NewManual()
		g := NewCrashGuard(clk)
		for i := 0; i < 4; i++ {
			g.Record()
		}
		clk.Advance(30 * time.Second) // exactly the window: the four no longer count
		if g.Record() {
			t.Fatal("tripped although four of the five exits are a full window old")
		}
		for i := 0; i < 3; i++ {
			if g.Record() {
				t.Fatalf("tripped early on exit %d of the new window", i+2)
			}
		}
		if !g.Record() {
			t.Fatal("the fifth exit of the new window did not trip")
		}
	})
}

// fakeProc is a firmware process the test controls.
type fakeProc struct {
	pid  int
	done chan struct{}

	mu       sync.Mutex
	info     ExitInfo
	sigs     []syscall.Signal
	exitOn   map[syscall.Signal]bool // signals that end the process
	finished bool
}

func (p *fakeProc) PID() int { return p.pid }

func (p *fakeProc) Wait() ExitInfo {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

func (p *fakeProc) SignalGroup(sig syscall.Signal) error {
	p.mu.Lock()
	p.sigs = append(p.sigs, sig)
	end := p.exitOn[sig]
	p.mu.Unlock()
	if end {
		p.finish(ExitInfo{Signal: SignalName(sig)})
	}
	return nil
}

// finish ends the process once with info.
func (p *fakeProc) finish(info ExitInfo) {
	p.mu.Lock()
	if p.finished {
		p.mu.Unlock()
		return
	}
	p.finished, p.info = true, info
	p.mu.Unlock()
	close(p.done)
}

func (p *fakeProc) signals() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.sigs...)
}

// fakeStarter hands out fakeProcs; behave decides what the n-th one does.
type fakeStarter struct {
	behave func(n int, p *fakeProc)

	mu    sync.Mutex
	procs []*fakeProc
	specs []StartSpec
}

func (f *fakeStarter) Start(s StartSpec) (Process, error) {
	f.mu.Lock()
	n := len(f.procs) + 1
	p := &fakeProc{pid: 1000 + n, done: make(chan struct{}), exitOn: map[syscall.Signal]bool{}}
	f.procs = append(f.procs, p)
	f.specs = append(f.specs, s)
	f.mu.Unlock()
	if f.behave != nil {
		f.behave(n, p)
	}
	return p, nil
}

func (f *fakeStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.procs)
}

func (f *fakeStarter) proc(i int) *fakeProc {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.procs[i]
}

// polite exits on SIGTERM; stubborn only on SIGKILL.
func polite(p *fakeProc)   { p.exitOn[syscall.SIGTERM] = true }
func stubborn(p *fakeProc) { p.exitOn[syscall.SIGKILL] = true }

type probeCounter struct {
	mu sync.Mutex
	n  int
}

func (c *probeCounter) run(context.Context, int) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *probeCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// newFake builds a supervisor around a fake starter and a Manual clock, with
// the main loop running; no engine, no control socket, no real probe.
func newFake(t *testing.T, st *fakeStarter) (*supervisor, *clock.Manual, *probeCounter) {
	t.Helper()
	clk := clock.NewManual()
	probes := &probeCounter{}
	s := &supervisor{
		paths:    instance.Paths{Name: "t", RunDir: t.TempDir()},
		spec:     instance.Spec{Program: "/fake/firmware"},
		clk:      clk,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		starter:  st,
		guard:    NewCrashGuard(clk),
		probe:    probes.run,
		fwExits:  make(chan fwExit, 8),
		loopDone: make(chan struct{}),
		engDied:  make(chan error, 1),
		stopped:  make(chan struct{}),
		downReq:  make(chan struct{}),
	}
	if err := s.startFirmware(); err != nil {
		t.Fatal(err)
	}
	s.startProbe()
	ctx, cancel := context.WithCancel(context.Background())
	loopErr := make(chan error, 1)
	go func() { loopErr <- s.mainLoop(ctx) }()
	t.Cleanup(func() {
		s.shutdown() // fake processes that ignore SIGTERM need the manual clock
		cancel()
		<-loopErr
	})
	return s, clk, probes
}

var realClock = clock.NewReal()

// eventually polls cond every 2 ms for up to 5 s of real time.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), realClock, 5*time.Second)
	defer cancel()
	for !cond() {
		if err := realClock.Sleep(ctx, 2*time.Millisecond); err != nil {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (s *supervisor) snapshotState() (restarts int, last, failureCode string, running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastExit != nil {
		last = s.lastExit.Kind
	}
	if s.failure != nil {
		failureCode = s.failure.Code
	}
	return s.restarts, last, failureCode, s.fwRunning
}

// A firmware that dies at once with code 1 is restarted four times, then the
// fifth unrequested exit stops the restarts and fails the instance.
func TestSupervisorCrashLoop(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { p.finish(ExitInfo{Code: intp(1)}) }}
	s, _, probes := newFake(t, st)

	eventually(t, "firmware_crash_loop", func() bool { _, _, code, _ := s.snapshotState(); return code != "" })
	restarts, last, code, running := s.snapshotState()
	if code != "firmware_crash_loop" {
		t.Fatalf("failure = %q, want firmware_crash_loop", code)
	}
	if st.count() != 5 || restarts != 4 {
		t.Fatalf("%d launches and %d restarts, want 5 and 4 (no fifth restart)", st.count(), restarts)
	}
	if last != "crashed" || running {
		t.Fatalf("last_exit %q, running %v, want crashed and not running", last, running)
	}
	s.mu.Lock()
	failure, exit := s.failure, s.lastExit
	s.mu.Unlock()
	if failure.Exit != apperr.ExitFailed {
		t.Errorf("firmware_crash_loop exit class %d, want %d", failure.Exit, apperr.ExitFailed)
	}
	if exit.Code == nil || *exit.Code != 1 {
		t.Errorf("last_exit.code = %v, want 1", exit.Code)
	}
	if failure.Detail["window_s"] != 30 || failure.Detail["limit"] != 5 {
		t.Errorf("detail = %v, want window_s 30 and limit 5", failure.Detail)
	}
	// Nothing starts afterwards.
	if err := realClock.Sleep(context.Background(), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if st.count() != 5 {
		t.Fatalf("%d launches after the guard tripped, want 5", st.count())
	}
	// Every launch was announced to the readiness probe: the first plus four restarts.
	if probes.count() != 5 {
		t.Errorf("%d readiness probes, want 5", probes.count())
	}
	// Same program, prefs and environment on every launch, numbered 1..5.
	for i, sp := range st.specs {
		if sp.Start != i+1 || sp.Program != "/fake/firmware" || sp.LogPath != s.paths.FirmwareLog() {
			t.Errorf("launch %d spec = %+v", i+1, sp)
		}
		if !hasEnv(sp.Env, "ESPHOME_PREFDIR="+s.paths.PrefsDir()) || !hasEnv(sp.Env, "SIL_BUS="+s.paths.BusSock()) {
			t.Errorf("launch %d lacks ESPHOME_PREFDIR/SIL_BUS", i+1)
		}
	}
}

func hasEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// Exit code 0 is a reboot: restarted, last_exit.kind rebooted, no failure.
func TestSupervisorRebootExit(t *testing.T) {
	st := &fakeStarter{behave: func(n int, p *fakeProc) {
		if n == 1 {
			p.finish(ExitInfo{Code: intp(0)})
			return
		}
		polite(p)
	}}
	s, _, probes := newFake(t, st)
	// autoRestart sets fwRunning before it starts the probe, and the probe counts from its own
	// goroutine, so the probe count is part of the condition being waited for.
	eventually(t, "the restart and its probe", func() bool {
		r, _, _, running := s.snapshotState()
		return r == 1 && running && probes.count() == 2
	})
	_, last, code, _ := s.snapshotState()
	if last != "rebooted" || code != "" {
		t.Fatalf("last_exit %q, failure %q, want rebooted and none", last, code)
	}
	if st.count() != 2 || probes.count() != 2 {
		t.Fatalf("%d launches and %d probes, want 2 and 2", st.count(), probes.count())
	}
	s.mu.Lock()
	exit := s.lastExit
	s.mu.Unlock()
	if exit.Code == nil || *exit.Code != 0 {
		t.Errorf("last_exit.code = %v, want 0", exit.Code)
	}
}

// Requested stops (the FwRestart path) never count: eight cycles in a row do
// not trip the guard, and the crash loop still needs five unrequested exits.
func TestSupervisorRequestedExitsAreNotCounted(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { polite(p) }}
	s, _, _ := newFake(t, st)

	for i := 1; i <= 8; i++ {
		s.opMu.Lock()
		err := s.cycleFirmware()
		s.opMu.Unlock()
		if err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		_, last, code, _ := s.snapshotState()
		if last != "requested" || code != "" {
			t.Fatalf("after cycle %d: last_exit %q, failure %q, want requested and none", i, last, code)
		}
	}
	if r, _, _, _ := s.snapshotState(); r != 8 || st.count() != 9 {
		t.Fatalf("%d restarts and %d launches, want 8 and 9", r, st.count())
	}

	// Four crashes in between are still below the limit; the fifth trips.
	for i := 1; i <= 4; i++ {
		before := st.count()
		st.proc(before - 1).finish(ExitInfo{Signal: "SIGKILL"})
		eventually(t, "the automatic restart", func() bool { return st.count() == before+1 })
		eventually(t, "the restart to settle", func() bool { _, _, _, running := s.snapshotState(); return running })
		if _, _, code, _ := s.snapshotState(); code != "" {
			t.Fatalf("crash %d: failure %q, want none", i, code)
		}
	}
	_, last, _, _ := s.snapshotState()
	if last != "crashed" {
		t.Fatalf("last_exit %q, want crashed", last)
	}
	s.mu.Lock()
	sig := s.lastExit.Signal
	s.mu.Unlock()
	if sig != "SIGKILL" {
		t.Errorf("last_exit.signal = %q, want SIGKILL", sig)
	}
	st.proc(st.count() - 1).finish(ExitInfo{Signal: "SIGKILL"})
	eventually(t, "firmware_crash_loop", func() bool { _, _, code, _ := s.snapshotState(); return code == "firmware_crash_loop" })
}

// Shutdown stops the process group first: SIGTERM, and SIGKILL once the 3 s
// timer has fired on the Manual clock.
func TestSupervisorShutdownEscalates(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { stubborn(p) }}
	s, clk, _ := newFake(t, st)
	p := st.proc(0)

	done := make(chan struct{})
	go func() { s.shutdown(); close(done) }()
	clk.BlockUntil(1) // the SIGKILL timer
	if got := p.signals(); len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("signals before the timer = %v, want [SIGTERM]", got)
	}
	select {
	case <-done:
		t.Fatal("shutdown returned while the firmware was still running")
	default:
	}
	clk.Advance(stopGrace)
	<-done
	got := p.signals()
	if len(got) != 2 || got[0] != syscall.SIGTERM || got[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v, want [SIGTERM SIGKILL]", got)
	}
	select {
	case <-s.stopped:
	default:
		t.Fatal("stopped is not closed after shutdown")
	}
	// The exit is the supervisor's own doing, never a crash, and nothing restarts.
	eventually(t, "the loop to stop", func() bool {
		select {
		case <-s.loopDone:
			return true
		default:
			return false
		}
	})
	if st.count() != 1 {
		t.Fatalf("%d launches, want 1", st.count())
	}
}

// A well-behaved firmware needs only SIGTERM.
func TestSupervisorShutdownPolite(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { polite(p) }}
	s, _, _ := newFake(t, st)
	s.shutdown()
	if got := st.proc(0).signals(); len(got) != 1 || got[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [SIGTERM]", got)
	}
}

// An unrequested exit that races with shutdown is not restarted.
func TestSupervisorNoRestartWhileStopping(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { polite(p) }}
	s, _, _ := newFake(t, st)
	s.mu.Lock()
	old := s.fw
	s.mu.Unlock()
	s.shutdown()
	s.autoRestart(old) // as if the loop had decided to restart just before
	if st.count() != 1 {
		t.Fatalf("%d launches after shutdown, want 1", st.count())
	}
	if s.restartCount() != 0 {
		t.Fatalf("restarts = %d, want 0", s.restartCount())
	}
}

func TestExecStarterGroupAndPrefs(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/firmware.log"
	proc, err := ExecStarter{}.Start(StartSpec{Program: "/bin/sh", Env: []string{"X=1"}, LogPath: logPath, Dir: dir, Start: 7})
	if err != nil {
		t.Fatal(err)
	}
	// /bin/sh without arguments reads stdin (/dev/null) and exits 0.
	info := proc.Wait()
	if info.Code == nil || *info.Code != 0 {
		t.Fatalf("exit = %+v, want code 0", info)
	}
	if err := proc.SignalGroup(syscall.SIGTERM); err != nil {
		t.Fatalf("SignalGroup after exit: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "=== firmware start 7 ===") {
		t.Fatalf("firmware.log = %q, want the start separator", string(raw))
	}
	if _, err := (ExecStarter{}).Start(StartSpec{Program: dir + "/missing", LogPath: logPath, Dir: dir}); err == nil {
		t.Fatal("starting a missing program succeeded")
	}
}

// A failed SimRestart must not replace an existing failure: after a crash
// loop, a restart whose engine does not start and a later successful one
// leave the instance reporting firmware_crash_loop, not an empty error.
func TestSimRestartKeepsAnExistingFailure(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { p.finish(ExitInfo{Code: intp(1)}) }}
	s, clk, _ := newFake(t, st)
	eventually(t, "firmware_crash_loop", func() bool { _, _, code, _ := s.snapshotState(); return code == "firmware_crash_loop" })

	good := filepath.Join(t.TempDir(), "bus.sock")
	bad := filepath.Join(t.TempDir(), "missing", "bus.sock")
	s.engCfg = engine.Config{Clock: clk, BusPath: good, Log: s.log}
	sl, err := s.startEngine()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.slot = sl
	s.mu.Unlock()

	failureCode := func() string {
		t.Helper()
		st, err := s.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if st.Error == nil {
			return ""
		}
		return st.Error.Code
	}

	// The engine cannot start: the original diagnosis stays.
	s.engCfg.BusPath = bad
	if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); apperr.AsError(err).Code != "engine_failed" {
		t.Fatalf("failing SimRestart = %v, want engine_failed", err)
	}
	if got := failureCode(); got != "firmware_crash_loop" {
		t.Fatalf("after a failed SimRestart status error = %q, want firmware_crash_loop", got)
	}

	// The engine starts again (the firmware is gone, so the resync step
	// reports firmware_not_running): the crash-loop diagnosis is still there.
	s.engCfg.BusPath = good
	if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); apperr.AsError(err).Code != "firmware_not_running" {
		t.Fatalf("recovering SimRestart = %v, want firmware_not_running", err)
	}
	if got := failureCode(); got != "firmware_crash_loop" {
		t.Fatalf("after a successful SimRestart status error = %q, want firmware_crash_loop", got)
	}
}

// A firmware restart drops firmware-scoped failures only: after a SimRestart
// whose engine cannot start, FwRestart must not erase engine_failed while the
// engine is still down.
func TestFwRestartKeepsEngineFailed(t *testing.T) {
	st := &fakeStarter{behave: func(_ int, p *fakeProc) { polite(p) }}
	s, clk, _ := newFake(t, st)

	good := filepath.Join(t.TempDir(), "bus.sock")
	bad := filepath.Join(t.TempDir(), "missing", "bus.sock")
	s.engCfg = engine.Config{Clock: clk, BusPath: good, Log: s.log}
	sl, err := s.startEngine()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.slot = sl
	s.mu.Unlock()

	s.engCfg.BusPath = bad
	if _, err := s.SimRestart(context.Background(), control.SimRestartRequest{}); apperr.AsError(err).Code != "engine_failed" {
		t.Fatalf("failing SimRestart = %v, want engine_failed", err)
	}

	if _, err := s.FwRestart(context.Background()); apperr.AsError(err).Code != "engine_failed" {
		t.Fatalf("FwRestart after a failed SimRestart = %v, want engine_failed", err)
	}
	status, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Error == nil || status.Error.Code != "engine_failed" {
		t.Fatalf("status error after FwRestart = %+v, want engine_failed", status.Error)
	}
	if status.Phase != control.PhaseFailed {
		t.Fatalf("phase = %q, want failed", status.Phase)
	}
}

package engine

import (
	"bufio"
	"context"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/bus"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
	"github.com/zeroflow/esphome-sil/internal/model"
)

// guard is the real-time budget for every wait. The engine itself runs only on
// the Manual clock, so this is purely a "do not hang the suite" limit.
const guard = 5 * time.Second

// fanParams is the Arctic P12 model every test port uses.
func fanParams(t testing.TB) model.Params {
	t.Helper()
	p, err := fanlib.Load(fanlib.DefaultFan)
	if err != nil {
		t.Fatalf("load default fan: %v", err)
	}
	return p
}

// firmware is a fake firmware peer: a socket client that records every frame
// the engine sends.
type firmware struct {
	t    *testing.T
	conn net.Conn

	mu     sync.Mutex
	frames []bus.Frame
}

func (fw *firmware) send(f bus.Frame) {
	fw.t.Helper()
	if _, err := fw.conn.Write(bus.Encode(f)); err != nil {
		fw.t.Fatalf("firmware write %s: %v", f, err)
	}
}

func (fw *firmware) readLoop() {
	sc := bufio.NewScanner(fw.conn)
	sc.Buffer(make([]byte, 0, bus.MaxLine), bus.MaxLine)
	for sc.Scan() {
		f, err := bus.Decode(sc.Bytes())
		if err != nil {
			fw.t.Errorf("engine sent an undecodable line %q: %v", sc.Text(), err)
			return
		}
		fw.mu.Lock()
		fw.frames = append(fw.frames, f)
		fw.mu.Unlock()
	}
}

// received returns a copy of the frames recorded so far.
func (fw *firmware) received() []bus.Frame {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return append([]bus.Frame(nil), fw.frames...)
}

// rpmFrames returns the recorded in frames for channel ch.
func (fw *firmware) rpmFrames(ch string) []bus.Frame {
	var out []bus.Frame
	for _, f := range fw.received() {
		if c, _ := f.GetStr("ch"); f.Kind() == "in" && c == ch {
			out = append(out, f)
		}
	}
	return out
}

// harness is an engine on a Manual clock with a connected fake firmware.
type harness struct {
	t   *testing.T
	eng *Engine
	clk *clock.Manual
	fw  *firmware
}

// waitUntil polls cond on the real clock; it never touches the engine clock.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	rc := clock.NewReal()
	deadline := rc.Now() + guard
	for !cond() {
		if rc.Now() > deadline {
			t.Fatalf("timed out waiting for %s", what)
		}
		_ = rc.Sleep(context.Background(), 5*time.Millisecond)
	}
}

// newHarness starts the engine on a short socket path (never t.TempDir, which
// can exceed the 107-byte sun_path limit), connects a fake firmware, sends
// hello and waits for the welcome.
func newHarness(t *testing.T) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	busPath := filepath.Join(dir, "bus.sock")

	fan := fanParams(t)
	var ports []PortConfig
	for n := 1; n <= 4; n++ {
		ports = append(ports, PortConfig{Port: n, FanID: fanlib.DefaultFan, Fan: fan})
	}
	clk := clock.NewManual()
	eng := New(Config{Clock: clk, BusPath: busPath, Ports: ports})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- eng.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		tm := clock.NewReal().NewTimer(guard)
		defer tm.Stop()
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		case <-tm.C():
			t.Error("Run did not return after cancel")
		}
	})
	tm := clock.NewReal().NewTimer(guard)
	select {
	case <-eng.Ready():
	case err := <-runErr:
		t.Fatalf("engine stopped before it was ready: %v", err)
	case <-tm.C():
		t.Fatal("engine never became ready")
	}
	tm.Stop()
	clk.BlockUntil(1) // the engine's tick ticker

	conn, err := net.Dial("unix", busPath)
	if err != nil {
		t.Fatalf("dial %s: %v", busPath, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	fw := &firmware{t: t, conn: conn}
	go fw.readLoop()
	fw.send(bus.NewFrame("hello").Int("proto", bus.Proto).Int("t", 0))
	h := &harness{t: t, eng: eng, clk: clk, fw: fw}
	waitUntil(t, "the welcome frame", func() bool {
		for _, f := range fw.received() {
			if f.Kind() == "welcome" {
				return true
			}
		}
		return false
	})
	return h
}

// out sends one firmware output frame.
func (h *harness) out(ch string, v float64) {
	h.t.Helper()
	h.fw.send(bus.NewFrame("out").Str("ch", ch).Num("v", v).Int("t", 0))
}

// settle returns once every frame sent so far has been applied. The inbox is
// FIFO, so a marker frame on an otherwise unused channel works as a barrier.
func (h *harness) settle() {
	h.t.Helper()
	h.out("port4.duty", 0.01)
	waitUntil(h.t, "the settle marker", func() bool { return h.eng.Snapshot().Ports[3].Duty == 0.01 })
}

func TestEngineWelcomesHelloWithProto1(t *testing.T) {
	h := newHarness(t)
	var welcome bus.Frame
	for _, f := range h.fw.received() {
		if f.Kind() == "welcome" {
			welcome = f
		}
	}
	if proto, ok := welcome.GetInt("proto"); !ok || proto != 1 {
		t.Fatalf("welcome proto = %d (present %v), want 1; frame %s", proto, ok, welcome)
	}
	if got, ok := welcome.GetInt("t"); !ok || got != 0 {
		t.Fatalf("welcome t = %d (present %v), want 0 on an unadvanced clock; frame %s", got, ok, welcome)
	}
	waitUntil(t, "the engine to record the connection", func() bool {
		s := h.eng.Snapshot()
		return s.BusConnected && s.BusProto == bus.Proto
	})
}

func TestEngineDutyLastValueWins(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first float64
		last  float64
	}{
		{"up", 0.2, 0.4},
		{"down", 0.4, 0.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.out("port1.duty", tc.first)
			h.out("port1.duty", tc.last)
			h.settle()
			s := h.eng.Snapshot()
			if got := s.Ports[0].Duty; got != tc.last {
				t.Fatalf("port1 duty = %v, want exactly %v (last value wins)", got, tc.last)
			}
			for i := 1; i < 3; i++ {
				if got := s.Ports[i].Duty; got != 0 {
					t.Fatalf("port%d duty = %v, want 0 (untouched)", i+1, got)
				}
			}
		})
	}
}

// relDiff is |a-b| relative to b (absolute when b is 0).
func relDiff(a, b float64) float64 {
	if b == 0 {
		return math.Abs(a)
	}
	return math.Abs(a-b) / math.Abs(b)
}

func TestEngineAdvancesModelOnlyOnClockTicks(t *testing.T) {
	h := newHarness(t)
	h.out("port1.duty", 0.2)
	h.out("port1.duty", 0.4)
	waitUntil(t, "port1 duty 0.4", func() bool { return h.eng.Snapshot().Ports[0].Duty == 0.4 })

	// Real time passes, sim time must not.
	_ = clock.NewReal().Sleep(context.Background(), 50*time.Millisecond)
	if s := h.eng.Snapshot(); s.SimTime != 0 || s.Ports[0].RPM != 0 {
		t.Fatalf("model moved without a clock tick: sim %v rpm %v", s.SimTime, s.Ports[0].RPM)
	}

	// Reference: 100 compositions of model.Step at dt = 10 ms and duty 0.4.
	p := fanParams(t)
	ref := make([]float64, 101)
	st := model.Initial(p)
	for k := 1; k <= 100; k++ {
		st = model.Step(p, st, 0.4, 10*time.Millisecond)
		ref[k] = st.RPM
	}
	if ref[100] <= 0 {
		t.Fatalf("reference rpm after 1 s is %v; the test would prove nothing", ref[100])
	}

	// Concurrent Snapshot readers while the clock advances (go test -race).
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 2; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := h.eng.Snapshot()
				sum := s.SimTime.Seconds()
				for _, ps := range s.Ports {
					sum += ps.RPM + ps.Duty
				}
				_ = sum
			}
		}()
	}

	h.clk.Advance(time.Second)
	waitUntil(t, "SimTime == 1 s", func() bool { return h.eng.Snapshot().SimTime == time.Second })
	close(stop)
	readers.Wait()

	s := h.eng.Snapshot()
	if got := s.Ports[0].RPM; relDiff(got, ref[100]) > 1e-9 {
		t.Fatalf("port1 rpm after 1 s = %.12g, want %.12g (100 x model.Step)", got, ref[100])
	}
	if got := s.Ports[0].Duty; got != 0.4 {
		t.Fatalf("port1 duty = %v, want 0.4", got)
	}
	for i := 1; i < 4; i++ {
		if s.Ports[i].RPM != 0 {
			t.Fatalf("port%d rpm = %v with duty 0, want 0", i+1, s.Ports[i].RPM)
		}
	}

	// Every port1.rpm in frame carries an integer t equal to the sim time in
	// microseconds, and the rpm it carries is the reference value for that time.
	last := int64(960000) // ticks at 10 ms + k*50 ms; 960 ms is the last send in the first second
	waitUntil(t, "the rpm frame stamped 960000", func() bool {
		fs := h.fw.rpmFrames("port1.rpm")
		if len(fs) == 0 {
			return false
		}
		tt, _ := fs[len(fs)-1].GetInt("t")
		return tt == last
	})
	frames := h.fw.rpmFrames("port1.rpm")
	if len(frames) < 2 {
		t.Fatalf("got %d port1.rpm frames, want several", len(frames))
	}
	prev := int64(-1)
	for _, f := range frames {
		tt, ok := f.GetInt("t")
		if !ok {
			t.Fatalf("in frame without an integer t: %s", f)
		}
		if tt <= prev {
			t.Fatalf("frame t %d not after previous %d: %s", tt, prev, f)
		}
		prev = tt
		if tt%10000 != 0 || tt > 1000000 {
			t.Fatalf("frame t = %d is not a tick time within the first second: %s", tt, f)
		}
		v, ok := f.GetF32("v")
		if !ok {
			t.Fatalf("in frame without v: %s", f)
		}
		want := float32(ref[tt/10000])
		if v != want {
			t.Fatalf("frame at t=%d carries rpm %v, want %v (the model's rpm at that sim time)", tt, v, want)
		}
	}
	if first, _ := frames[0].GetInt("t"); first != 0 {
		t.Fatalf("first port1.rpm frame t = %d, want 0 (the hello snapshot)", first)
	}
}

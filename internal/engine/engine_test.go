package engine

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	t       *testing.T
	eng     *Engine
	clk     *clock.Manual
	fw      *firmware
	busPath string
	cancel  context.CancelFunc // stops the engine's Run
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

// fullManifest is what the skeleton firmware announces: the four duty outputs
// then the four rpm inputs, in registration order.
func fullManifest() []bus.Channel {
	var chs []bus.Channel
	for n := 1; n <= 4; n++ {
		chs = append(chs, bus.Channel{Ch: fmt.Sprintf("port%d.duty", n), Dir: "out", Type: "f32"})
	}
	for n := 1; n <= 4; n++ {
		chs = append(chs, bus.Channel{Ch: fmt.Sprintf("port%d.rpm", n), Dir: "in", Type: "f32"})
	}
	return chs
}

// startEngine runs an engine on a Manual clock on a short socket path (never
// t.TempDir, which can exceed the 107-byte sun_path limit) and waits until it
// listens. No firmware is connected yet.
func startEngine(t *testing.T) *harness {
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
	return &harness{t: t, eng: eng, clk: clk, busPath: busPath, cancel: cancel}
}

// dial connects a fake firmware without saying anything yet.
func (h *harness) dial() *firmware {
	h.t.Helper()
	conn, err := net.Dial("unix", h.busPath)
	if err != nil {
		h.t.Fatalf("dial %s: %v", h.busPath, err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	fw := &firmware{t: h.t, conn: conn}
	go fw.readLoop()
	return fw
}

// session sends hello (announcing len(chans) channels), the chan frames and
// the closing sync, like the firmware hub does on every connect.
func (fw *firmware) session(chans []bus.Channel) {
	fw.t.Helper()
	fw.send(bus.NewFrame("hello").Int("proto", bus.Proto).Str("name", "test-fw").Int("chans", int64(len(chans))).Int("t", 0))
	for _, c := range chans {
		fw.send(bus.NewFrame("chan").Str("ch", c.Ch).Str("dir", c.Dir).Str("type", c.Type))
	}
	fw.send(bus.NewFrame("sync").Int("t", 0))
}

// kinds returns the kind of every frame received so far.
func (fw *firmware) kinds() []string {
	var ks []string
	for _, f := range fw.received() {
		ks = append(ks, f.Kind())
	}
	return ks
}

// waitKinds waits until at least n frames arrived and returns their kinds.
func (fw *firmware) waitKinds(n int) []string {
	fw.t.Helper()
	waitUntil(fw.t, fmt.Sprintf("%d frames from the engine", n), func() bool { return len(fw.received()) >= n })
	return fw.kinds()
}

// newHarness starts the engine, connects a fake firmware with the full
// manifest and waits until the session is synced on both sides.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := startEngine(t)
	h.fw = h.dial()
	h.fw.session(fullManifest())
	waitUntil(t, "the engine's sync", func() bool {
		for _, f := range h.fw.received() {
			if f.Kind() == "sync" {
				return true
			}
		}
		return false
	})
	waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
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

func TestEngineSnapshotOrderAfterHello(t *testing.T) {
	h := newHarness(t)
	want := []string{"welcome", "in", "in", "in", "in", "sync"}
	got := h.fw.waitKinds(len(want))[:len(want)]
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame kinds after hello = %v, want %v", h.fw.kinds(), want)
		}
	}
	for i, f := range h.fw.received()[1:5] {
		if ch, _ := f.GetStr("ch"); ch != fmt.Sprintf("port%d.rpm", i+1) {
			t.Fatalf("snapshot frame %d is for %q, want port%d.rpm (port order)", i, ch, i+1)
		}
		if v, ok := f.GetF32("v"); !ok || v != 0 {
			t.Fatalf("snapshot frame %s: rpm %v (present %v), want 0 for a fresh engine", f, v, ok)
		}
	}
	if w := h.eng.Snapshot().Warnings; len(w) != 0 {
		t.Fatalf("full manifest produced warnings %v", w)
	}
}

func TestEngineManifestWarnings(t *testing.T) {
	without := func(ch string) []bus.Channel {
		var out []bus.Channel
		for _, c := range fullManifest() {
			if c.Ch != ch {
				out = append(out, c)
			}
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		chans []bus.Channel
		want  []string
	}{
		{"missing input", without("port3.rpm"), []string{"missing channel port3.rpm (in)"}},
		{"missing output", without("port2.duty"), []string{"missing channel port2.duty (out)"}},
		{"unknown channel", append(fullManifest(), bus.Channel{Ch: "foo.bar", Dir: "out", Type: "f32"}),
			[]string{"unknown channel foo.bar (ignored)"}},
		{"wrong direction", func() []bus.Channel {
			chs := fullManifest()
			chs[0].Dir = "in"
			return chs
		}(), []string{`channel port1.duty has direction "in", expected "out"`}},
		{"wrong type", func() []bus.Channel {
			chs := fullManifest()
			chs[7].Type = "i32"
			return chs
		}(), []string{`channel port4.rpm has type "i32", expected "f32"`}},
		{"zero channels", nil, []string{
			"missing channel port1.duty (out)", "missing channel port1.rpm (in)",
			"missing channel port2.duty (out)", "missing channel port2.rpm (in)",
			"missing channel port3.duty (out)", "missing channel port3.rpm (in)",
			"missing channel port4.duty (out)", "missing channel port4.rpm (in)",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := startEngine(t)
			fw := h.dial()
			fw.session(tc.chans)
			// The session completes whatever the manifest says.
			got := fw.waitKinds(6)
			if got[0] != "welcome" || got[5] != "sync" {
				t.Fatalf("frame kinds = %v, want welcome, four in frames, sync", got)
			}
			waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
			s := h.eng.Snapshot()
			if !s.BusConnected {
				t.Fatalf("a manifest mismatch must not drop the connection")
			}
			if fmt.Sprint(s.Warnings) != fmt.Sprint(tc.want) {
				t.Fatalf("warnings = %q, want %q", s.Warnings, tc.want)
			}
		})
	}
}

func TestEngineUnknownOutChannelWarnsOnce(t *testing.T) {
	h := newHarness(t)
	h.out("foo.bar", 1)
	h.out("foo.bar", 2)
	h.settle()
	w := h.eng.Snapshot().Warnings
	if len(w) != 1 || w[0] != "unknown channel foo.bar (ignored)" {
		t.Fatalf("warnings = %q, want one unknown-channel warning", w)
	}
}

func TestEngineProtoMismatch(t *testing.T) {
	h := startEngine(t)
	fw := h.dial()
	fw.send(bus.NewFrame("hello").Int("proto", 2).Str("name", "old-fw").Int("chans", 0).Int("t", 0))
	waitUntil(t, "the engine to record the mismatch", func() bool {
		return len(h.eng.Snapshot().Warnings) == 1
	})
	s := h.eng.Snapshot()
	if s.BusConnected || s.BusProto != 0 {
		t.Fatalf("bus_connected %v proto %d after a mismatching hello, want false and 0", s.BusConnected, s.BusProto)
	}
	if !strings.Contains(s.Warnings[0], "protocol mismatch") {
		t.Fatalf("warning %q does not name the protocol mismatch", s.Warnings[0])
	}
	waitUntil(t, "the err frame", func() bool { return len(fw.received()) == 1 })
	f := fw.received()[0]
	if msg, _ := f.GetStr("msg"); f.Kind() != "err" || !strings.Contains(msg, "proto mismatch") {
		t.Fatalf("first reply = %s, want err with a proto mismatch message", f)
	}
	// The engine closed the connection after the err.
	_ = fw.conn.SetReadDeadline(clock.NewReal().Deadline(guard))
	if _, err := fw.conn.Read(make([]byte, 1)); err == nil {
		t.Fatalf("read after the err frame returned data, want EOF")
	}
}

func TestEngineSnapshotCarriesCurrentRPM(t *testing.T) {
	h := newHarness(t)
	h.out("port1.duty", 0.4)
	h.settle()
	h.clk.Advance(time.Second)
	waitUntil(t, "SimTime == 1 s", func() bool { return h.eng.Snapshot().SimTime == time.Second })
	rpm := h.eng.Snapshot().Ports[0].RPM
	if rpm <= 0 {
		t.Fatalf("port1 rpm %v after 1 s at duty 0.4; the test would prove nothing", rpm)
	}

	// A restarted firmware connects: the newer connection replaces the older and
	// its hello is answered with the rpm the fan has right now.
	fw2 := h.dial()
	fw2.session(fullManifest())
	got := fw2.waitKinds(6)
	if got[0] != "welcome" || got[5] != "sync" {
		t.Fatalf("frame kinds on the new connection = %v", got)
	}
	f := fw2.received()[1]
	if ch, _ := f.GetStr("ch"); ch != "port1.rpm" {
		t.Fatalf("first snapshot frame is %s, want port1.rpm", f)
	}
	if v, _ := f.GetF32("v"); v != float32(rpm) {
		t.Fatalf("snapshot port1.rpm = %v, want the current %v", v, float32(rpm))
	}
	waitUntil(t, "the old connection to receive bye", func() bool {
		ks := h.fw.kinds()
		return len(ks) > 0 && ks[len(ks)-1] == "bye"
	})
	if s := h.eng.Snapshot(); !s.BusConnected || !s.Synced {
		t.Fatalf("after the replacement bus_connected %v synced %v, want true and true", s.BusConnected, s.Synced)
	}
	// Late frames of the replaced connection no longer reach the model.
	_, _ = h.fw.conn.Write(bus.Encode(bus.NewFrame("out").Str("ch", "port2.duty").Num("v", 0.9).Int("t", 0))) // the write may fail: the peer is closed
	_ = clock.NewReal().Sleep(context.Background(), 100*time.Millisecond)
	if d := h.eng.Snapshot().Ports[1].Duty; d != 0 {
		t.Fatalf("port2 duty = %v from the replaced connection, want 0", d)
	}
}

func TestEngineDisconnectClearsSession(t *testing.T) {
	h := newHarness(t)
	_ = h.fw.conn.Close()
	waitUntil(t, "bus_connected to drop", func() bool { return !h.eng.Snapshot().BusConnected })
	if s := h.eng.Snapshot(); s.Synced || s.BusProto != 0 || len(s.Warnings) != 0 {
		t.Fatalf("after a plain close: synced %v proto %d warnings %q, want false 0 none", s.Synced, s.BusProto, s.Warnings)
	}
}

// A stopped engine (a simulator restart in progress) must not claim a bus connection.
func TestEngineStoppedSnapshotIsDisconnected(t *testing.T) {
	h := newHarness(t)
	if s := h.eng.Snapshot(); !s.BusConnected || !s.Synced {
		t.Fatalf("before the stop: bus_connected %v synced %v", s.BusConnected, s.Synced)
	}
	h.cancel()
	waitUntil(t, "the final snapshot", func() bool { return !h.eng.Snapshot().BusConnected })
	if s := h.eng.Snapshot(); s.Synced || s.BusProto != 0 {
		t.Fatalf("after the stop: synced %v proto %d, want false and 0", s.Synced, s.BusProto)
	}
}

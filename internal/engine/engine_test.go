package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zeroflow/esphome-sil/internal/bus"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/events"
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
	rec     *recorder          // every event the engine emitted
}

// recorder is an Emit that keeps what it was given and numbers it like a broker.
type recorder struct {
	mu  sync.Mutex
	evs []events.Event
}

func (r *recorder) emit(ev events.Event) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev.Seq = uint64(len(r.evs)) + 1
	r.evs = append(r.evs, ev)
	return ev.Seq
}

// all returns a copy of the events so far.
func (r *recorder) all() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.evs...)
}

// of returns the events of kind k.
func (r *recorder) of(k string) []events.Event {
	var out []events.Event
	for _, ev := range r.all() {
		if ev.K == k {
			out = append(out, ev)
		}
	}
	return out
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
	rec := &recorder{}
	eng := New(Config{Clock: clk, BusPath: busPath, Ports: ports, Emit: rec.emit})

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
	return &harness{t: t, eng: eng, clk: clk, busPath: busPath, cancel: cancel, rec: rec}
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
	want := []string{"welcome", "in", "in", "in", "in", "in", "in", "in", "in", "in", "sync"}
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
	// Temperature and humidity (the defaults of a zero Config.Env) sit between the rpm and the button frames.
	for i, f := range h.fw.received()[5:7] {
		if ch, _ := f.GetStr("ch"); ch != envChannels[i] {
			t.Fatalf("snapshot frame %d is for %q, want %s (env order)", 5+i, ch, envChannels[i])
		}
	}
	// The released level of every button follows (Research Pitfall 1).
	for i, f := range h.fw.received()[7:10] {
		if ch, _ := f.GetStr("ch"); ch != ButtonIDs[i] {
			t.Fatalf("snapshot frame %d is for %q, want %s (button order)", 7+i, ch, ButtonIDs[i])
		}
		if v, ok := f.GetF32("v"); !ok || v != 0 {
			t.Fatalf("snapshot frame %s: level %v (present %v), want 0 (released)", f, v, ok)
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
			got := fw.waitKinds(11)
			if got[0] != "welcome" || got[10] != "sync" {
				t.Fatalf("frame kinds = %v, want welcome, four rpm, temperature, humidity and three button in frames, sync", got)
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

// WR-04: a peer that sends out frames on ever-new channel names cannot grow the warning list (or
// one entry) without bound; the list stops at maxWarnings with a closing note.
func TestEngineWarningsAreBounded(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3*maxWarnings; i++ {
		h.out(fmt.Sprintf("junk.%d", i), 1)
	}
	h.out("long."+strings.Repeat("\u00e4", 1000), 1) // multi-byte runes: the cut must stay valid UTF-8
	h.settle()
	w := h.eng.Snapshot().Warnings
	if len(w) != maxWarnings {
		t.Fatalf("%d warnings, want the cap of %d", len(w), maxWarnings)
	}
	if w[0] != "unknown channel junk.0 (ignored)" {
		t.Fatalf("first warning %q, want the first one kept", w[0])
	}
	if w[len(w)-1] != warningsSuppressed {
		t.Fatalf("last warning %q, want the suppression note", w[len(w)-1])
	}
	// Re-sending an already known channel stays a no-op at the cap.
	h.out("junk.0", 2)
	h.settle()
	if got := len(h.eng.Snapshot().Warnings); got != maxWarnings {
		t.Fatalf("%d warnings after a duplicate, want %d", got, maxWarnings)
	}
}

func TestEngineWarnTruncatesLongEntries(t *testing.T) {
	e := &Engine{}
	e.warn(strings.Repeat("\u00e4", 1000)) // 2000 bytes
	if len(e.warnings) != 1 {
		t.Fatalf("%d warnings, want 1", len(e.warnings))
	}
	got := e.warnings[0]
	if len(got) > maxWarningLen+len("...") || !strings.HasSuffix(got, "...") || !utf8.ValidString(got) {
		t.Fatalf("warning of %d bytes %q: want at most %d bytes plus an ellipsis, valid UTF-8", len(got), got[:20], maxWarningLen)
	}
	e.warn(strings.Repeat("\u00e4", 1000))
	if len(e.warnings) != 1 {
		t.Fatalf("the same long warning was recorded twice")
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
	got := fw2.waitKinds(11)
	if got[0] != "welcome" || got[10] != "sync" {
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

// buttonManifest is the Rev 3.3 manifest: the ports plus the buttons except those in skip.
func buttonManifest(skip ...string) []bus.Channel {
	chs := fullManifest()
	for _, id := range ButtonIDs {
		omit := false
		for _, s := range skip {
			omit = omit || s == id
		}
		if !omit {
			chs = append(chs, bus.Channel{Ch: id, Dir: "in", Type: "f32"})
		}
	}
	return chs
}

// newButtonHarness is newHarness with a manifest that announces the buttons.
func newButtonHarness(t *testing.T, skip ...string) *harness {
	t.Helper()
	h := startEngine(t)
	h.fw = h.dial()
	h.fw.session(buttonManifest(skip...))
	waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
	return h
}

// buttonFrames returns the recorded in frames for button id.
func (fw *firmware) buttonFrames(id string) []bus.Frame { return fw.rpmFrames(id) }

func setButton(t *testing.T, e *Engine, id string, level, strict bool) (time.Duration, error) {
	t.Helper()
	return setButtonHold(t, e, id, level, strict, 0)
}

// setButtonHold is setButton with a max hold: the engine releases the button by itself after it.
func setButtonHold(t *testing.T, e *Engine, id string, level, strict bool, maxHold time.Duration) (time.Duration, error) {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), clock.NewReal(), guard)
	defer cancel()
	return e.SetButton(ctx, id, level, strict, maxHold)
}

func TestEngineButtonsStartReleased(t *testing.T) {
	h := newButtonHarness(t)
	b := h.eng.Snapshot().Buttons
	if len(b) != len(ButtonIDs) {
		t.Fatalf("Buttons = %v, want all of %v", b, ButtonIDs)
	}
	for _, id := range ButtonIDs {
		if v, ok := b[id]; !ok || v {
			t.Fatalf("Buttons[%s] = %v (present %v), want false", id, v, ok)
		}
	}
}

func TestEngineSetButtonErrors(t *testing.T) {
	t.Run("before the hello", func(t *testing.T) {
		h := startEngine(t) // listening, no firmware
		if _, err := setButton(t, h.eng, "usr1", true, true); !errors.Is(err, ErrNotConnected) {
			t.Fatalf("strict down without a firmware: %v, want ErrNotConnected", err)
		}
	})
	t.Run("hello without a manifest", func(t *testing.T) {
		h := startEngine(t)
		fw := h.dial()
		fw.send(bus.NewFrame("hello").Int("proto", bus.Proto).Str("name", "test-fw").Int("chans", 8).Int("t", 0))
		waitUntil(t, "the engine to record the connection", func() bool { return h.eng.Snapshot().BusConnected })
		if _, err := setButton(t, h.eng, "usr1", true, true); !errors.Is(err, ErrNotConnected) {
			t.Fatalf("strict down before the manifest: %v, want ErrNotConnected", err)
		}
	})
	t.Run("input not in the manifest", func(t *testing.T) {
		h := newButtonHarness(t, "usr2")
		if _, err := setButton(t, h.eng, "usr2", true, true); !errors.Is(err, ErrInputNotAvailable) {
			t.Fatalf("down on usr2 without that channel: %v, want ErrInputNotAvailable", err)
		}
		if w := h.eng.Snapshot().Warnings; len(w) != 0 {
			t.Fatalf("a missing optional button channel warned: %q", w)
		}
		if _, err := setButton(t, h.eng, "usr1", true, true); err != nil {
			t.Fatalf("down on usr1: %v", err)
		}
	})
	t.Run("busy and not held", func(t *testing.T) {
		h := newButtonHarness(t)
		if _, err := setButton(t, h.eng, "usr1", false, true); !errors.Is(err, ErrButtonNotHeld) {
			t.Fatalf("strict up on a released button: %v, want ErrButtonNotHeld", err)
		}
		if _, err := setButton(t, h.eng, "usr1", false, false); err != nil {
			t.Fatalf("non-strict up on a released button: %v, want nil", err)
		}
		if _, err := setButton(t, h.eng, "usr1", true, true); err != nil {
			t.Fatalf("first down: %v", err)
		}
		if _, err := setButton(t, h.eng, "usr1", true, true); !errors.Is(err, ErrButtonBusy) {
			t.Fatalf("second down: %v, want ErrButtonBusy", err)
		}
		if _, err := setButton(t, h.eng, "usr3", true, true); err != nil {
			t.Fatalf("down on another button while usr1 is held: %v", err)
		}
		if _, err := setButton(t, h.eng, "usr1", false, true); err != nil {
			t.Fatalf("up on a held button: %v", err)
		}
	})
	t.Run("unknown id", func(t *testing.T) {
		h := newButtonHarness(t)
		if _, err := setButton(t, h.eng, "usr9", true, false); err == nil {
			t.Fatal("an id outside ButtonIDs was accepted")
		}
	})
}

func TestEngineSetButtonSendsOneFrameAtTheEdge(t *testing.T) {
	h := newButtonHarness(t)
	h.clk.Advance(750 * time.Millisecond)
	waitUntil(t, "SimTime == 750 ms", func() bool { return h.eng.Snapshot().SimTime == 750*time.Millisecond })
	before := len(h.fw.buttonFrames("usr1"))

	edge, err := setButton(t, h.eng, "usr1", true, true)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	waitUntil(t, "the usr1 frame", func() bool { return len(h.fw.buttonFrames("usr1")) == before+1 })
	fr := h.fw.buttonFrames("usr1")
	last := fr[len(fr)-1]
	if v, ok := last.GetF32("v"); !ok || v != 1 {
		t.Fatalf("frame %s: v %v (present %v), want 1", last, v, ok)
	}
	if tm, ok := last.GetInt("t"); !ok || tm != edge.Microseconds() {
		t.Fatalf("frame t = %d (present %v), want the edge time %d", tm, ok, edge.Microseconds())
	}
	if edge < 750*time.Millisecond {
		t.Fatalf("edge time %v is before the sim time 750 ms", edge)
	}
	if !h.eng.Snapshot().Buttons["usr1"] {
		t.Fatal("Snapshot.Buttons[usr1] is false after the down")
	}

	h.clk.Advance(200 * time.Millisecond)
	edge2, err := setButton(t, h.eng, "usr1", false, true)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if edge2-edge < 200*time.Millisecond {
		t.Fatalf("up edge %v is less than 200 ms after the down edge %v", edge2, edge)
	}
	waitUntil(t, "the release frame", func() bool { return len(h.fw.buttonFrames("usr1")) == before+2 })
	fr = h.fw.buttonFrames("usr1")
	if v, _ := fr[len(fr)-1].GetF32("v"); v != 0 {
		t.Fatalf("release frame %s, want v 0", fr[len(fr)-1])
	}
	if h.eng.Snapshot().Buttons["usr1"] {
		t.Fatal("Snapshot.Buttons[usr1] is true after the up")
	}
}

// A button that is held when the firmware reconnects is announced as held in the new in-snapshot.
func TestEngineHeldButtonSurvivesAFirmwareReconnect(t *testing.T) {
	h := newButtonHarness(t)
	if _, err := setButton(t, h.eng, "usr2", true, true); err != nil {
		t.Fatal(err)
	}
	fw2 := h.dial()
	fw2.session(buttonManifest())
	fw2.waitKinds(11)
	fr := fw2.buttonFrames("usr2")
	if len(fr) == 0 {
		t.Fatal("no usr2 frame on the new connection")
	}
	if v, _ := fr[0].GetF32("v"); v != 1 {
		t.Fatalf("usr2 in the new snapshot = %v, want 1 (still held)", v)
	}
}

func TestEngineSetButtonAfterStopAnswersAtOnce(t *testing.T) {
	h := newButtonHarness(t)
	h.cancel()
	waitUntil(t, "the engine to stop", func() bool { return !h.eng.Snapshot().BusConnected })
	done := make(chan error, 1)
	go func() {
		_, err := h.eng.SetButton(context.Background(), "usr1", false, false, 0)
		done <- err
	}()
	tm := clock.NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("SetButton after Run returned: %v, want ErrStopped", err)
		}
	case <-tm.C():
		t.Fatal("SetButton blocked on a stopped engine")
	}
}

// ledChan is the leds channel entry of the Rev 3.3 firmware's manifest.
func ledChan(n int64, seg string) bus.Channel {
	return bus.Channel{Ch: LEDChannel, Dir: "out", Type: "rgb", N: n, Seg: seg}
}

// ledPx is an out frame on the leds channel carrying px.
func ledPx(px string) bus.Frame {
	return bus.NewFrame("out").Str("ch", LEDChannel).Str("px", px).Int("t", 0)
}

// ledHello sends hello, the port chan frames, the given extra chan entries (with n and seg when set),
// then early (frames sent before the sync, like the hub's px replay), then the sync.
func ledHello(fw *firmware, extra []bus.Channel, early ...bus.Frame) {
	fw.t.Helper()
	fw.send(bus.NewFrame("hello").Int("proto", bus.Proto).Str("name", "test-fw").Int("chans", int64(len(fullManifest())+len(extra))).Int("t", 0))
	for _, c := range append(fullManifest(), extra...) {
		f := bus.NewFrame("chan").Str("ch", c.Ch).Str("dir", c.Dir).Str("type", c.Type)
		if c.N > 0 {
			f = f.Int("n", c.N).Str("seg", c.Seg)
		}
		fw.send(f)
	}
	for _, f := range early {
		fw.send(f)
	}
	fw.send(bus.NewFrame("sync").Int("t", 0))
}

// newLEDHarness starts an engine whose firmware announces the given leds channel and is synced.
func newLEDHarness(t *testing.T, c bus.Channel, early ...bus.Frame) *harness {
	t.Helper()
	h := startEngine(t)
	h.fw = h.dial()
	ledHello(h.fw, []bus.Channel{c}, early...)
	waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
	return h
}

// ledBarrier returns once every frame sent so far has been applied. Unlike settle it can be used
// repeatedly: each call waits for a duty value no earlier call used.
func (h *harness) ledBarrier(v float64) {
	h.t.Helper()
	h.out("port3.duty", v)
	waitUntil(h.t, "the barrier frame", func() bool { return h.eng.Snapshot().Ports[2].Duty == v })
}

func ledBlack(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "#000000"
	}
	return out
}

func TestEngineLEDManifest(t *testing.T) {
	t.Run("segments and a black strip", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0,fan1:1:1"))
		s := h.eng.Snapshot()
		if fmt.Sprint(s.LEDs.Segments) != "map[board:[0] fan1:[1]]" {
			t.Fatalf("segments = %v, want board [0] and fan1 [1]", s.LEDs.Segments)
		}
		if fmt.Sprint(s.LEDs.Strip) != fmt.Sprint(ledBlack(5)) {
			t.Fatalf("strip = %v, want 5 x #000000", s.LEDs.Strip)
		}
		if len(s.Warnings) != 0 {
			t.Fatalf("warnings = %v, want none", s.Warnings)
		}
	})
	t.Run("a segment spans several LEDs", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "status:0:1"))
		if got := fmt.Sprint(h.eng.Snapshot().LEDs.Segments["status"]); got != "[0 1]" {
			t.Fatalf("status = %s, want [0 1]", got)
		}
	})
	t.Run("an empty seg is no segments and no warning", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, ""))
		s := h.eng.Snapshot()
		if len(s.LEDs.Segments) != 0 || len(s.LEDs.Strip) != 5 || len(s.Warnings) != 0 {
			t.Fatalf("segments %v strip %v warnings %v, want none, 5 entries, none", s.LEDs.Segments, s.LEDs.Strip, s.Warnings)
		}
	})
	t.Run("no leds channel", func(t *testing.T) {
		h := newHarness(t)
		s := h.eng.Snapshot()
		if len(s.LEDs.Strip) != 0 || len(s.LEDs.Segments) != 0 || len(s.Warnings) != 0 {
			t.Fatalf("strip %v segments %v warnings %v, want all empty", s.LEDs.Strip, s.LEDs.Segments, s.Warnings)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		c := ledChan(5, "board:0:0")
		c.Type = "f32"
		h := newLEDHarness(t, c)
		s := h.eng.Snapshot()
		if fmt.Sprint(s.Warnings) != `[channel leds has type "f32", expected "rgb"]` {
			t.Fatalf("warnings = %q, want the wrong-type warning", s.Warnings)
		}
		if len(s.LEDs.Strip) != 0 || len(s.LEDs.Segments) != 0 {
			t.Fatalf("strip %v segments %v, want both empty", s.LEDs.Strip, s.LEDs.Segments)
		}
	})
	t.Run("wrong direction", func(t *testing.T) {
		c := ledChan(5, "board:0:0")
		c.Dir = "in"
		h := newLEDHarness(t, c)
		if fmt.Sprint(h.eng.Snapshot().Warnings) != `[channel leds has direction "in", expected "out"]` {
			t.Fatalf("warnings = %q", h.eng.Snapshot().Warnings)
		}
	})
}

func TestEngineLEDManifestWarnings(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int64
		seg  string
		strp int // expected strip length: 0 for a bad n, n for a bad seg
	}{
		{"n 0", 0, "board:0:0", 0},
		{"n 256", 256, "board:0:0", 0},
		{"seg without to", 5, "board:0", 5},
		{"uppercase label", 5, "Board:0:0", 5},
		{"from after to", 5, "a:2:1", 5},
		{"to beyond n", 5, "a:0:5", 5},
		{"duplicate label", 5, "a:0:0,a:1:1", 5},
		{"signed index", 5, "a:+0:1", 5},
		{"negative index", 5, "a:-1:1", 5},
		{"empty part", 5, "a:0:0,", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := startEngine(t)
			h.fw = h.dial()
			// n 0 is not sent by the hub (it only writes n for pixels() > 0), so build the frame by hand.
			h.fw.send(bus.NewFrame("hello").Int("proto", bus.Proto).Str("name", "test-fw").Int("chans", 9).Int("t", 0))
			for _, c := range fullManifest() {
				h.fw.send(bus.NewFrame("chan").Str("ch", c.Ch).Str("dir", c.Dir).Str("type", c.Type))
			}
			h.fw.send(bus.NewFrame("chan").Str("ch", LEDChannel).Str("dir", "out").Str("type", "rgb").Int("n", tc.n).Str("seg", tc.seg))
			h.fw.send(bus.NewFrame("sync").Int("t", 0))
			waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
			s := h.eng.Snapshot()
			if len(s.Warnings) != 1 || !strings.HasPrefix(s.Warnings[0], "channel leds") {
				t.Fatalf("warnings = %q, want exactly one naming channel leds", s.Warnings)
			}
			if len(s.LEDs.Segments) != 0 {
				t.Fatalf("segments = %v, want none", s.LEDs.Segments)
			}
			if len(s.LEDs.Strip) != tc.strp {
				t.Fatalf("strip has %d entries, want %d", len(s.LEDs.Strip), tc.strp)
			}
		})
	}
}

func TestEngineLEDPixels(t *testing.T) {
	blue := "0000FF" + strings.Repeat("0", 24)
	t.Run("px is stored lowercase", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"))
		h.fw.send(ledPx(blue))
		h.settle()
		s := h.eng.Snapshot()
		want := ledBlack(5)
		want[0] = "#0000ff"
		if fmt.Sprint(s.LEDs.Strip) != fmt.Sprint(want) {
			t.Fatalf("strip = %v, want %v", s.LEDs.Strip, want)
		}
		if len(s.Warnings) != 0 {
			t.Fatalf("warnings = %v, want none", s.Warnings)
		}
	})
	t.Run("exact pixel bytes", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"))
		h.fw.send(ledPx("002500" + "010203" + "fffffe" + "a0b1c2" + "7f8081"))
		h.settle()
		want := []string{"#002500", "#010203", "#fffffe", "#a0b1c2", "#7f8081"}
		if got := h.eng.Snapshot().LEDs.Strip; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("strip = %v, want %v (no conversion or rounding)", got, want)
		}
	})
	t.Run("px before the manifest is kept", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"), ledPx(blue))
		s := h.eng.Snapshot()
		if len(s.LEDs.Strip) != 5 || s.LEDs.Strip[0] != "#0000ff" || len(s.Warnings) != 0 {
			t.Fatalf("strip %v warnings %v, want the early frame kept without a warning", s.LEDs.Strip, s.Warnings)
		}
	})
	t.Run("early px of the wrong size is dropped with one warning", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"), ledPx("0000ff0000ff"))
		s := h.eng.Snapshot()
		if len(s.Warnings) != 1 || fmt.Sprint(s.LEDs.Strip) != fmt.Sprint(ledBlack(5)) {
			t.Fatalf("strip %v warnings %q, want 5 x black and one warning", s.LEDs.Strip, s.Warnings)
		}
	})
	for _, bad := range []struct{ name, px string }{
		{"length 29", strings.Repeat("0", 29)},
		{"not hex", "zz0000" + strings.Repeat("0", 24)},
		{"too short for n", strings.Repeat("0", 24)},
		{"too long for n", strings.Repeat("0", 36)},
		{"empty", ""},
	} {
		t.Run("bad px "+bad.name, func(t *testing.T) {
			h := newLEDHarness(t, ledChan(5, "board:0:0"))
			h.fw.send(ledPx(blue))
			h.ledBarrier(0.11)
			before := h.eng.Snapshot().LEDs.Strip
			h.fw.send(ledPx(bad.px))
			h.fw.send(ledPx(bad.px)) // the same warning twice is recorded once
			h.ledBarrier(0.12)
			s := h.eng.Snapshot()
			if len(s.Warnings) != 1 || !strings.HasPrefix(s.Warnings[0], "channel leds") {
				t.Fatalf("warnings = %q, want exactly one naming channel leds", s.Warnings)
			}
			if fmt.Sprint(s.LEDs.Strip) != fmt.Sprint(before) {
				t.Fatalf("strip = %v, want the previous strip %v kept", s.LEDs.Strip, before)
			}
		})
	}
	t.Run("a value instead of px", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"))
		h.fw.send(bus.NewFrame("out").Str("ch", LEDChannel).Num("v", 1).Int("t", 0))
		h.settle()
		s := h.eng.Snapshot()
		if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "wrong type") {
			t.Fatalf("warnings = %q, want one wrong-type warning", s.Warnings)
		}
	})
	t.Run("the strip survives a disconnect and a new session starts empty", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"))
		h.fw.send(ledPx(blue))
		h.settle()
		_ = h.fw.conn.Close()
		waitUntil(t, "the disconnect", func() bool { return !h.eng.Snapshot().BusConnected })
		s := h.eng.Snapshot()
		if len(s.LEDs.Strip) != 5 || s.LEDs.Strip[0] != "#0000ff" || len(s.LEDs.Segments) != 1 {
			t.Fatalf("after the disconnect: strip %v segments %v, want the last frame kept", s.LEDs.Strip, s.LEDs.Segments)
		}
		fw2 := h.dial()
		fw2.send(bus.NewFrame("hello").Int("proto", bus.Proto).Str("name", "test-fw").Int("chans", 0).Int("t", 0))
		waitUntil(t, "the new session", func() bool { return h.eng.Snapshot().BusConnected })
		if s := h.eng.Snapshot(); len(s.LEDs.Strip) != 0 || len(s.LEDs.Segments) != 0 {
			t.Fatalf("new session: strip %v segments %v, want both empty until the manifest", s.LEDs.Strip, s.LEDs.Segments)
		}
	})
	t.Run("a published snapshot is not changed by later frames", func(t *testing.T) {
		h := newLEDHarness(t, ledChan(5, "board:0:0"))
		before := h.eng.Snapshot()
		h.fw.send(ledPx(blue))
		h.ledBarrier(0.13)
		if before.LEDs.Strip[0] != "#000000" {
			t.Fatalf("an earlier snapshot changed to %v", before.LEDs.Strip)
		}
		if h.eng.Snapshot().LEDs.Strip[0] != "#0000ff" {
			t.Fatalf("the new frame is missing: %v", h.eng.Snapshot().LEDs.Strip)
		}
	})
}

// envValues returns the values of the recorded in frames for channel ch, in order.
func (fw *firmware) envValues(ch string) []float32 {
	var out []float32
	for _, f := range fw.rpmFrames(ch) {
		v, _ := f.GetF32("v")
		out = append(out, v)
	}
	return out
}

func applyChange(t *testing.T, e *Engine, c Change) error {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), clock.NewReal(), guard)
	defer cancel()
	return e.Apply(ctx, c)
}

func fp(v float64) *float64 { return &v }

// An unchanged environment is sent exactly once per EnvResend of sim time, per value (D-11).
func TestEngineEnvResendEvery10s(t *testing.T) {
	h := newHarness(t)
	for _, ch := range []string{ChanTemperature, ChanHumidity} {
		if n := len(h.fw.envValues(ch)); n != 1 {
			t.Fatalf("%s frames after hello = %d, want the one of the in-snapshot", ch, n)
		}
	}
	for s := 1; s <= 29; s++ {
		h.clk.Advance(time.Second)
	}
	waitUntil(t, "SimTime == 29 s", func() bool { return h.eng.Snapshot().SimTime == 29*time.Second })
	for _, ch := range []string{ChanTemperature, ChanHumidity} {
		// hello at 0, then 10 s and 20 s.
		waitUntil(t, ch+" re-sends", func() bool { return len(h.fw.envValues(ch)) == 3 })
	}
	h.clk.Advance(time.Second) // 30 s
	waitUntil(t, "the third re-send", func() bool { return len(h.fw.envValues(ChanTemperature)) == 4 })
	if n := len(h.fw.envValues(ChanHumidity)); n != 4 {
		t.Fatalf("humidity frames = %d, want 4 (0, 10, 20 and 30 s)", n)
	}
}

// Apply sends exactly the changed value at once, applies all fields together and re-arms that
// value's 10 s timer.
func TestEngineApplySendsChangedValueAtOnce(t *testing.T) {
	h := newHarness(t)
	h.clk.Advance(4 * time.Second)
	waitUntil(t, "SimTime == 4 s", func() bool { return h.eng.Snapshot().SimTime == 4*time.Second })
	if err := applyChange(t, h.eng, Change{Temperature: fp(30)}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the temperature frame", func() bool { return len(h.fw.envValues(ChanTemperature)) == 2 })
	if got := h.fw.envValues(ChanTemperature); got[1] != 30 {
		t.Fatalf("temperature frames %v, want the second to be 30", got)
	}
	if n := len(h.fw.envValues(ChanHumidity)); n != 1 {
		t.Fatalf("humidity frames = %d after a temperature-only Apply, want 1", n)
	}
	if e := h.eng.Snapshot().Env; e.Temperature != 30 || e.Humidity != 0 {
		t.Fatalf("Snapshot.Env = %+v, want temperature 30 and the unchanged humidity", e)
	}

	// Both fields at once: one event, both frames, and the snapshot never shows half of it.
	if err := applyChange(t, h.eng, Change{Temperature: fp(31), Humidity: fp(50)}); err != nil {
		t.Fatal(err)
	}
	if e := h.eng.Snapshot().Env; e.Temperature != 31 || e.Humidity != 50 {
		t.Fatalf("Snapshot.Env = %+v right after Apply returned, want 31 and 50", e)
	}
	waitUntil(t, "both frames", func() bool {
		return len(h.fw.envValues(ChanTemperature)) == 3 && len(h.fw.envValues(ChanHumidity)) == 2
	})

	// The timer of a changed value restarts: temperature was sent at 4 s, so its next re-send is
	// at 14 s, the humidity at 14 s as well (both set at 4 s); nothing at 10 s.
	h.clk.Advance(9 * time.Second) // 13 s
	waitUntil(t, "SimTime == 13 s", func() bool { return h.eng.Snapshot().SimTime == 13*time.Second })
	if n := len(h.fw.envValues(ChanTemperature)); n != 3 {
		t.Fatalf("temperature frames = %d at 13 s, want 3 (no re-send before 14 s)", n)
	}
	h.clk.Advance(time.Second) // 14 s
	waitUntil(t, "the re-send at 14 s", func() bool { return len(h.fw.envValues(ChanTemperature)) == 4 })
}

// A fresh engine announces Config.Env in its in-snapshot, and Apply before any firmware connects
// is what the firmware then sees.
func TestEngineConfigEnvInSnapshot(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	clk := clock.NewManual()
	eng := New(Config{Clock: clk, BusPath: filepath.Join(dir, "bus.sock"), Env: Env{Temperature: 22, Humidity: 45}})
	if e := eng.Snapshot().Env; e.Temperature != 22 || e.Humidity != 45 {
		t.Fatalf("Snapshot.Env before Run = %+v, want the Config.Env", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	<-eng.Ready()
	clk.BlockUntil(1)
	if err := applyChange(t, eng, Change{Humidity: fp(40)}); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, eng: eng, clk: clk, busPath: filepath.Join(dir, "bus.sock")}
	fw := h.dial()
	fw.session(fullManifest())
	// No ports configured: welcome, temperature, humidity, three buttons, sync.
	fw.waitKinds(7)
	if got := fw.envValues(ChanTemperature); len(got) != 1 || got[0] != 22 {
		t.Fatalf("temperature in the snapshot = %v, want [22]", got)
	}
	if got := fw.envValues(ChanHumidity); len(got) != 1 || got[0] != 40 {
		t.Fatalf("humidity in the snapshot = %v, want [40] (the value set before the connect)", got)
	}
}

func TestEngineApplyAfterStopAnswersAtOnce(t *testing.T) {
	h := newHarness(t)
	h.cancel()
	waitUntil(t, "the engine to stop", func() bool { return !h.eng.Snapshot().BusConnected })
	done := make(chan error, 1)
	go func() { done <- h.eng.Apply(context.Background(), Change{Temperature: fp(20)}) }()
	tm := clock.NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("Apply after Run returned: %v, want ErrStopped", err)
		}
	case <-tm.C():
		t.Fatal("Apply blocked on a stopped engine")
	}
}

// The env channels are optional: a wrong direction or type warns, a missing one does not.
func TestEngineEnvManifestWarnings(t *testing.T) {
	h := startEngine(t)
	fw := h.dial()
	fw.session(append(fullManifest(),
		bus.Channel{Ch: ChanTemperature, Dir: "out", Type: "f32"},
		bus.Channel{Ch: ChanHumidity, Dir: "in", Type: "rgb"}))
	waitUntil(t, "the manifest to be checked", func() bool { return h.eng.Snapshot().Synced })
	want := []string{`channel temperature has direction "out", expected "in"`, `channel humidity has type "rgb", expected "f32"`}
	if got := h.eng.Snapshot().Warnings; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
	h2 := startEngine(t)
	fw2 := h2.dial()
	fw2.session(append(fullManifest(),
		bus.Channel{Ch: ChanTemperature, Dir: "in", Type: "f32"}, bus.Channel{Ch: ChanHumidity, Dir: "in", Type: "f32"}))
	waitUntil(t, "the manifest to be checked", func() bool { return h2.eng.Snapshot().Synced })
	if w := h2.eng.Snapshot().Warnings; len(w) != 0 {
		t.Fatalf("a correct env manifest produced warnings %q", w)
	}
}

const noctuaID = "noctua-nf-a12x25-pwm"

// fanConfig loads library fan id as the replacement for a port.
func fanConfig(t *testing.T, id string) PortConfig {
	t.Helper()
	p, err := fanlib.Load(id)
	if err != nil {
		t.Fatalf("load fan %s: %v", id, err)
	}
	return PortConfig{FanID: id, Fan: p}
}

// advanceTo moves the Manual clock by d and waits until the engine has processed the last tick.
func (h *harness) advanceTo(d time.Duration) {
	h.t.Helper()
	target := h.eng.Snapshot().SimTime + d
	h.clk.Advance(d)
	waitUntil(h.t, fmt.Sprintf("SimTime == %v", target), func() bool { return h.eng.Snapshot().SimTime == target })
}

// setDuty drives port n's duty from the firmware and waits until the engine has it.
func (h *harness) setDuty(n int, duty float64) {
	h.t.Helper()
	h.out(fmt.Sprintf("port%d.duty", n), duty)
	waitUntil(h.t, fmt.Sprintf("port%d duty %v", n, duty), func() bool { return h.eng.Snapshot().Ports[n-1].Duty == duty })
}

func replug(t *testing.T, h *harness, port int, id string) {
	t.Helper()
	if err := applyChange(t, h.eng, Change{Fans: map[int]PortConfig{port: fanConfig(t, id)}}); err != nil {
		t.Fatalf("replug port %d to %s: %v", port, id, err)
	}
}

// A swap is an instant unplug and replug: rpm 0 and the new fan id in the very next snapshot, then
// the new fan follows its own file from rest (MODEL-03, D-10).
func TestEngineReplugSwapsFanAndResetsToRest(t *testing.T) {
	h := newHarness(t)
	h.setDuty(1, 1.0)
	h.setDuty(2, 1.0)
	h.advanceTo(8 * time.Second)
	if s := h.eng.Snapshot(); s.Ports[0].RPM < 1700 || s.Ports[0].Fan != fanlib.DefaultFan {
		t.Fatalf("before the swap port1 = %+v, want a running Arctic", s.Ports[0])
	}

	replug(t, h, 1, noctuaID)
	s := h.eng.Snapshot()
	if s.Ports[0].Fan != noctuaID || s.Ports[0].RPM != 0 {
		t.Fatalf("right after the swap port1 = %+v, want the Noctua at rpm 0", s.Ports[0])
	}
	if s.Ports[0].Duty != 1.0 || s.Ports[0].Port != 1 {
		t.Fatalf("port1 = %+v: the duty and the port number must survive a replug", s.Ports[0])
	}
	if s.Ports[1].Fan != fanlib.DefaultFan || s.Ports[1].RPM < 1700 {
		t.Fatalf("port2 = %+v changed by a replug of port1", s.Ports[1])
	}
	h.advanceTo(8 * time.Second)
	if got := h.eng.Snapshot().Ports[0].RPM; got < 1900 || got > 2000 {
		t.Fatalf("port1 rpm 8 s after the swap = %v, want toward the Noctua's 2000", got)
	}
	// The firmware is told about the drop to 0 by the normal change rule: a frame below 300 after
	// the frames that showed the old fan running.
	var ran, dropped bool
	for _, f := range h.fw.rpmFrames("port1.rpm") {
		v, _ := f.GetF32("v")
		if v > 1500 {
			ran = true
		} else if ran && v < 300 {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("no port1.rpm frame below 300 followed the running old fan: %v", h.fw.envValues("port1.rpm"))
	}
}

// A hold fan never stops: replugged at duty 0.10 it restarts from 0 and climbs back to its min_rpm,
// also when the same fan is plugged again.
func TestEngineReplugHoldFanClimbsToMinRPM(t *testing.T) {
	h := newHarness(t)
	h.setDuty(1, 0.10)
	replug(t, h, 1, noctuaID)
	h.advanceTo(15 * time.Second)
	if got := h.eng.Snapshot().Ports[0].RPM; got < 430 || got > 470 {
		t.Fatalf("Noctua at duty 0.10 holds %v rpm, want 430..470", got)
	}
	replug(t, h, 1, noctuaID) // same-fan replug
	if s := h.eng.Snapshot().Ports[0]; s.RPM != 0 || s.State != "running" || s.Fan != noctuaID {
		t.Fatalf("same-fan replug: port1 = %+v, want rpm 0 and still a running hold fan", s)
	}
	h.advanceTo(10 * time.Second)
	if got := h.eng.Snapshot().Ports[0].RPM; got < 430 || got > 470 {
		t.Fatalf("Noctua 10 s after a same-fan replug = %v rpm, want 430..470 (it never stays at 0)", got)
	}
}

// A stop fan plugged in below its start_duty stays at rest, over a running Arctic in its dead band
// (same-fan replug) and over a Noctua alike.
func TestEngineReplugStopFanBelowStartDutyStaysStopped(t *testing.T) {
	h := newHarness(t)

	// Port 1: a running Arctic in its dead band (duty 0.08 is between min_duty 0.05 and start_duty 0.10).
	h.setDuty(1, 1.0)
	h.advanceTo(5 * time.Second)
	h.setDuty(1, 0.08)
	h.advanceTo(10 * time.Second)
	if s := h.eng.Snapshot().Ports[0]; s.State != "running" || s.RPM < 150 {
		t.Fatalf("port1 = %+v, want an Arctic still running in its dead band", s)
	}
	replug(t, h, 1, fanlib.DefaultFan)
	if s := h.eng.Snapshot().Ports[0]; s.RPM != 0 || s.State != "stopped" {
		t.Fatalf("same-fan replug: port1 = %+v, want rpm 0 and stopped", s)
	}

	// Port 2: a Noctua holding about 450 rpm at duty 0.08, then an Arctic plugged in.
	replug(t, h, 2, noctuaID)
	h.setDuty(2, 0.08)
	h.advanceTo(15 * time.Second)
	if got := h.eng.Snapshot().Ports[1].RPM; got < 430 || got > 470 {
		t.Fatalf("Noctua at duty 0.08 holds %v rpm, want 430..470", got)
	}
	replug(t, h, 2, fanlib.DefaultFan)
	if s := h.eng.Snapshot().Ports[1]; s.Fan != fanlib.DefaultFan || s.RPM != 0 || s.State != "stopped" {
		t.Fatalf("Arctic over the Noctua: port2 = %+v, want rpm 0 and stopped", s)
	}

	h.advanceTo(5 * time.Second)
	for i := 0; i < 2; i++ {
		if s := h.eng.Snapshot().Ports[i]; s.RPM != 0 || s.State != "stopped" {
			t.Fatalf("5 s later port%d = %+v, want exactly 0 and stopped (duty 0.08 is below start_duty 0.10)", i+1, s)
		}
	}
	// And it starts once the duty reaches start_duty.
	h.setDuty(1, 0.5)
	h.advanceTo(5 * time.Second)
	if s := h.eng.Snapshot().Ports[0]; s.RPM < 500 || s.State != "running" {
		t.Fatalf("port1 = %+v at duty 0.5, want a running fan", s)
	}
}

// Env and fan fields of one Change land in one event: no snapshot shows one without the other.
func TestEngineApplyEnvAndFansInOneEvent(t *testing.T) {
	h := newHarness(t)
	noctua := fanConfig(t, noctuaID)
	stop := make(chan struct{})
	var bad atomic.Value
	var readers sync.WaitGroup
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
			if (s.Env.Temperature == 31) != (s.Ports[0].Fan == noctuaID) {
				bad.Store(fmt.Sprintf("half a change: temperature %v with port1 fan %q", s.Env.Temperature, s.Ports[0].Fan))
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		if err := applyChange(t, h.eng, Change{Temperature: fp(31), Fans: map[int]PortConfig{1: noctua}}); err != nil {
			t.Fatal(err)
		}
		if err := applyChange(t, h.eng, Change{Temperature: fp(5), Fans: map[int]PortConfig{1: fanConfig(t, fanlib.DefaultFan)}}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()
	if v := bad.Load(); v != nil {
		t.Fatal(v)
	}
	if s := h.eng.Snapshot(); s.Env.Temperature != 5 || s.Ports[0].Fan != fanlib.DefaultFan {
		t.Fatalf("final snapshot %+v / %+v, want temperature 5 and the Arctic", s.Env, s.Ports[0])
	}
}

// A port the engine does not have is ignored; the other fields of the Change still apply.
func TestEngineReplugUnknownPortIsIgnored(t *testing.T) {
	h := newHarness(t)
	if err := applyChange(t, h.eng, Change{Temperature: fp(26), Fans: map[int]PortConfig{9: fanConfig(t, noctuaID)}}); err != nil {
		t.Fatal(err)
	}
	s := h.eng.Snapshot()
	if s.Env.Temperature != 26 || len(s.Ports) != 4 {
		t.Fatalf("snapshot %+v, want temperature 26 and four ports", s)
	}
	for _, p := range s.Ports {
		if p.Fan != fanlib.DefaultFan {
			t.Fatalf("port%d fan %q after a replug of port 9", p.Port, p.Fan)
		}
	}
}

// duty events carry the change from the firmware; a repeated value is not a change.
func TestEngineEmitsDutyOnChangeOnly(t *testing.T) {
	h := newHarness(t)
	h.out("port2.duty", 0.4)
	h.out("port2.duty", 0.4) // identical
	h.out("port2.duty", 0)   // back to the initial value is a change from 0.4
	h.out("port1.duty", 0)   // identical to the initial value
	h.settle()
	var got []string
	for _, ev := range h.rec.of("duty") {
		if ev.Port == 4 {
			continue // the settle marker
		}
		got = append(got, fmt.Sprintf("%d=%v", ev.Port, ev.V))
	}
	if want := "[2=0.4 2=0]"; fmt.Sprint(got) != want {
		t.Fatalf("duty events %v, want %s", got, want)
	}
}

// rpm is sampled once per second of sim time and only when the float32 value changed.
func TestEngineEmitsRPMOncePerSecondOnChange(t *testing.T) {
	h := newHarness(t)
	h.advanceTo(2 * time.Second)
	if n := len(h.rec.of("rpm")); n != 0 {
		t.Fatalf("%d rpm events at rest, want none (nothing changed)", n)
	}
	h.setDuty(1, 1.0)
	h.advanceTo(10 * time.Second)
	evs := h.rec.of("rpm")
	if len(evs) < 4 || len(evs) > 11 {
		t.Fatalf("%d rpm events over 10 s of spin-up, want about one per second: %+v", len(evs), evs)
	}
	for i, ev := range evs {
		if ev.Port != 1 {
			t.Fatalf("rpm event %d on port %d, want only port 1 (the others did not change)", i, ev.Port)
		}
		if i > 0 && ev.T-evs[i-1].T < RPMEventStep.Microseconds() {
			t.Fatalf("rpm events %d and %d are %d us apart, want at least %d", i-1, i, ev.T-evs[i-1].T, RPMEventStep.Microseconds())
		}
		if i > 0 && ev.V.(float64) <= evs[i-1].V.(float64) {
			t.Fatalf("rpm did not rise between events %d and %d: %v then %v", i-1, i, evs[i-1].V, ev.V)
		}
	}
	// At the steady state nothing changes any more, so nothing is emitted.
	h.advanceTo(30 * time.Second)
	before := len(h.rec.of("rpm"))
	h.advanceTo(10 * time.Second)
	if after := len(h.rec.of("rpm")); after != before {
		t.Fatalf("%d rpm events at the steady state, want none", after-before)
	}
}

// A port starts or stops: state events, and one per port when the engine starts.
func TestEngineEmitsStateAndStartStates(t *testing.T) {
	h := newHarness(t)
	var start []string
	for _, ev := range h.rec.of("state") {
		start = append(start, fmt.Sprintf("%d=%v", ev.Port, ev.V))
	}
	if want := "[1=stopped 2=stopped 3=stopped 4=stopped]"; fmt.Sprint(start) != want {
		t.Fatalf("start states %v, want %s (the Arctic stops below its start duty)", start, want)
	}
	h.setDuty(3, 1.0)
	h.advanceTo(time.Second)
	last := h.rec.of("state")
	if ev := last[len(last)-1]; ev.Port != 3 || ev.V != "running" {
		t.Fatalf("last state event %+v, want port 3 running", ev)
	}
	// A hold fan is running from the start.
	replug(t, h, 1, noctuaID)
	fan := h.rec.of("fan")
	if len(fan) != 1 || fan[0].Port != 1 || fan[0].V != noctuaID {
		t.Fatalf("fan events %+v, want one for port 1 = %s", fan, noctuaID)
	}
	last = h.rec.of("state")
	if ev := last[len(last)-1]; ev.Port != 1 || ev.V != "running" {
		t.Fatalf("last state event %+v, want port 1 running after plugging a hold fan", ev)
	}
}

func TestEngineEmitsButtonAndEnv(t *testing.T) {
	h := newHarness(t)
	if _, err := h.eng.SetButton(context.Background(), "usr2", true, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.eng.SetButton(context.Background(), "usr2", true, false, 0); err != nil { // not a change
		t.Fatal(err)
	}
	if _, err := h.eng.SetButton(context.Background(), "usr2", false, false, 0); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range h.rec.of("button") {
		got = append(got, fmt.Sprintf("%s=%v", ev.ID, ev.V))
	}
	if want := "[usr2=true usr2=false]"; fmt.Sprint(got) != want {
		t.Fatalf("button events %v, want %s", got, want)
	}
	if err := applyChange(t, h.eng, Change{Temperature: fp(26), Humidity: fp(40)}); err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, ev := range h.rec.of("env") {
		got = append(got, fmt.Sprintf("%s=%v", ev.ID, ev.V))
	}
	if want := "[temperature=26 humidity=40]"; fmt.Sprint(got) != want {
		t.Fatalf("env events %v, want %s", got, want)
	}
}

// Every pixel frame is an event, an identical replay included (D-08).
func TestEngineEmitsEveryLEDFrame(t *testing.T) {
	h := newLEDHarness(t, ledChan(5, "board:0:0"))
	blue := "0000ff" + strings.Repeat("0", 24)
	h.fw.send(ledPx(blue))
	h.fw.send(ledPx(blue))
	h.fw.send(ledPx("zz0000" + strings.Repeat("0", 24))) // invalid: no event
	h.fw.send(ledPx(blue))
	h.settle()
	evs := h.rec.of("leds")
	if len(evs) != 3 {
		t.Fatalf("%d leds events, want 3 (two identical frames and one more, the invalid one is not an event): %+v", len(evs), evs)
	}
	for _, ev := range evs {
		if len(ev.Strip) != 5 || ev.Strip[0] != "#0000ff" {
			t.Fatalf("leds event strip %v, want 5 LEDs with the first blue", ev.Strip)
		}
	}
}

// firmware true on hello, false on a disconnect and when the engine stops; Snapshot.Seq follows
// the events.
func TestEngineEmitsFirmwareAndSnapshotSeq(t *testing.T) {
	h := newHarness(t)
	fwEvs := func() string {
		var got []string
		for _, ev := range h.rec.of("firmware") {
			got = append(got, fmt.Sprint(ev.V))
		}
		return fmt.Sprint(got)
	}
	if got := fwEvs(); got != "[true]" {
		t.Fatalf("firmware events %s after the hello, want [true]", got)
	}
	h.setDuty(1, 0.5)
	last := h.rec.all()[len(h.rec.all())-1]
	if s := h.eng.Snapshot(); s.Seq != last.Seq {
		t.Fatalf("Snapshot.Seq = %d, want the seq %d of the last event (%+v)", s.Seq, last.Seq, last)
	}
	_ = h.fw.conn.Close()
	waitUntil(t, "the disconnect event", func() bool { return fwEvs() == "[true false]" })
	if s := h.eng.Snapshot(); s.Seq != h.rec.all()[len(h.rec.all())-1].Seq {
		t.Fatalf("Snapshot.Seq = %d after the disconnect, want the last event's seq", s.Seq)
	}
}

func TestEngineStopEmitsFirmwareFalse(t *testing.T) {
	h := newHarness(t)
	h.cancel()
	waitUntil(t, "the engine to stop", func() bool { return !h.eng.Snapshot().BusConnected })
	waitUntil(t, "the firmware false event", func() bool {
		evs := h.rec.of("firmware")
		return len(evs) == 2 && evs[1].V == false
	})
}

// SnapshotSync is taken on the engine goroutine and carries Config.Seq when set.
func TestEngineSnapshotSyncStampsTheBrokerSeq(t *testing.T) {
	h := newHarness(t)
	h.eng.cfg.Seq = func() uint64 { return 4242 }
	h.setDuty(2, 0.3)
	s, err := h.eng.SnapshotSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Seq != 4242 || s.Ports[1].Duty != 0.3 {
		t.Fatalf("SnapshotSync = seq %d duty %v, want seq 4242 and the duty just set", s.Seq, s.Ports[1].Duty)
	}
	h.eng.cfg.Seq = nil
	s, err = h.eng.SnapshotSync(context.Background())
	if err != nil || s.Seq != h.rec.all()[len(h.rec.all())-1].Seq {
		t.Fatalf("SnapshotSync without Config.Seq: seq %d err %v, want the last emitted seq", s.Seq, err)
	}
	h.cancel()
	waitUntil(t, "the engine to stop", func() bool { return !h.eng.Snapshot().BusConnected })
	if _, err := h.eng.SnapshotSync(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("SnapshotSync after stop: %v, want ErrStopped", err)
	}
}

// samplesOf decodes the V of every sample event.
func samplesOf(t *testing.T, evs []events.Event) [][]events.Sample {
	t.Helper()
	var out [][]events.Sample
	for _, ev := range evs {
		v, ok := ev.V.([]events.Sample)
		if !ok {
			t.Fatalf("sample event V is %T, want []events.Sample", ev.V)
		}
		out = append(out, v)
	}
	return out
}

// Every 100 ms of sim time one sample event carries duty and rpm of every port; without the
// firmware on the bus the values are null (a gap), never zero.
func TestSampleCadence(t *testing.T) {
	h := newHarness(t)
	h.setDuty(1, 0.4)
	h.advanceTo(time.Second)
	evs := h.rec.of(events.SampleKind)
	if len(evs) != 10 {
		t.Fatalf("%d sample events after 1 s of ticks, want 10", len(evs))
	}
	for i, ev := range evs {
		if want := int64(i+1) * 100_000; ev.T != want {
			t.Fatalf("sample %d at t=%d, want %d (100 ms apart)", i, ev.T, want)
		}
	}
	snap := h.eng.Snapshot()
	vs := samplesOf(t, evs)
	last := vs[len(vs)-1]
	if len(last) != 4 {
		t.Fatalf("a sample has %d entries, want one per port (4)", len(last))
	}
	for i, s := range last {
		if s.Port != i+1 || s.Duty == nil || s.RPM == nil {
			t.Fatalf("entry %d = %+v, want port %d with duty and rpm", i, s, i+1)
		}
		if *s.Duty != snap.Ports[i].Duty || *s.RPM != snap.Ports[i].RPM {
			t.Fatalf("entry %d = duty %v rpm %v, want the snapshot's %v / %v", i, *s.Duty, *s.RPM, snap.Ports[i].Duty, snap.Ports[i].RPM)
		}
	}
	if *last[0].Duty != 0.4 || *last[1].Duty != 0 {
		t.Fatalf("duties %v %v, want 0.4 and 0", *last[0].Duty, *last[1].Duty)
	}
	if *last[0].RPM <= 0 {
		t.Fatalf("port 1 rpm %v after 1 s at 40%%, want it spinning up", *last[0].RPM)
	}

	d := startEngine(t) // no firmware on the bus
	d.advanceTo(time.Second)
	dv := samplesOf(t, d.rec.of(events.SampleKind))
	if len(dv) != 10 {
		t.Fatalf("%d samples without a firmware, want 10 (gaps are still samples)", len(dv))
	}
	for _, sample := range dv {
		for i, s := range sample {
			if s.Port != i+1 || s.Duty != nil || s.RPM != nil {
				t.Fatalf("disconnected entry %+v, want port %d with null duty and rpm", s, i+1)
			}
		}
	}
	raw, err := json.Marshal(dv[0][0])
	if err != nil || string(raw) != `{"port":1,"duty":null,"rpm":null}` {
		t.Fatalf("a null entry marshals as %s (%v), want explicit nulls", raw, err)
	}
}

// A slot the engine missed is skipped, never back-filled.
func TestSampleSkipsMissedSlots(t *testing.T) {
	e := New(Config{Clock: clock.NewManual()})
	e.nextSample = sampleStep
	if e.sampleDue(99 * time.Millisecond) {
		t.Fatal("a sample was due before the first slot")
	}
	if !e.sampleDue(350 * time.Millisecond) {
		t.Fatal("no sample due at 350 ms with the slot at 100 ms open")
	}
	if e.sampleDue(350 * time.Millisecond) {
		t.Fatal("the same instant produced a second sample: the missed slots were back-filled")
	}
	if e.sampleDue(399 * time.Millisecond) {
		t.Fatal("a sample was due at 399 ms, want the next slot at 400 ms")
	}
	if !e.sampleDue(400 * time.Millisecond) {
		t.Fatal("no sample due at the 400 ms slot")
	}
	if e.nextSample != 500*time.Millisecond {
		t.Fatalf("nextSample = %v after the 400 ms slot, want 500ms", e.nextSample)
	}
}

// startOn runs a new engine on clk that publishes into emit, like the supervisor does after a sim
// restart, and returns it with a stop function that waits for Run.
func startOn(t *testing.T, clk *clock.Manual, emit func(events.Event) uint64) (*Engine, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	fan := fanParams(t)
	ports := []PortConfig{{Port: 1, FanID: fanlib.DefaultFan, Fan: fan}, {Port: 2, FanID: fanlib.DefaultFan, Fan: fan}}
	eng := New(Config{Clock: clk, BusPath: filepath.Join(dir, "bus.sock"), Ports: ports, Emit: emit})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()
	tm := clock.NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case <-eng.Ready():
	case err := <-done:
		t.Fatalf("engine stopped before it was ready: %v", err)
	case <-tm.C():
		t.Fatal("engine never became ready")
	}
	clk.BlockUntil(1)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			wt := clock.NewReal().NewTimer(guard)
			defer wt.Stop()
			select {
			case <-done:
			case <-wt.C():
				t.Error("Run did not return after cancel")
			}
		})
	}
	t.Cleanup(stop)
	return eng, stop
}

// The first sample of every engine carries gap:true, so a sim restart is marked in the stream.
func TestSampleGapMarker(t *testing.T) {
	broker := events.NewBroker()
	clk := clock.NewManual()
	run := func(d time.Duration) (stop func()) {
		eng, stop := startOn(t, clk, broker.Publish)
		target := eng.Snapshot().SimTime + d
		clk.Advance(d)
		waitUntil(t, fmt.Sprintf("SimTime == %v", target), func() bool { return eng.Snapshot().SimTime == target })
		return stop
	}
	stopA := run(time.Second)
	stopA() // the sim restart: engine A is gone
	clk.Advance(3 * time.Second)
	run(time.Second) // engine B starts on the same broker and the same clock

	_, hist := broker.History(0)
	if len(hist) != 20 {
		t.Fatalf("History holds %d samples, want 10 from each engine", len(hist))
	}
	var gaps []int
	for i, ev := range hist {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		hasGap := strings.Contains(string(raw), `"gap"`)
		if hasGap != ev.Gap || (hasGap && !strings.Contains(string(raw), `"gap":true`)) {
			t.Fatalf("sample %d marshals as %s with Gap=%v", i, raw, ev.Gap)
		}
		if ev.Gap {
			gaps = append(gaps, i)
		}
		if i > 0 && ev.Seq <= hist[i-1].Seq {
			t.Fatalf("history is not in seq order at %d", i)
		}
	}
	if fmt.Sprint(gaps) != "[0 10]" {
		t.Fatalf("gap samples at %v, want exactly the first sample of each engine [0 10]", gaps)
	}
	if d := hist[10].T - hist[9].T; d <= 3_000_000 {
		t.Fatalf("B's first sample is %d us after A's last, want more than the 3 s restart gap", d)
	}
}

// behindEngine is an engine that is never run: noteTick is driven by hand, the way the tick branch
// of Run calls it.
func behindEngine() (*Engine, *recorder) {
	rec := &recorder{}
	return New(Config{Clock: clock.NewManual(), Emit: rec.emit}), rec
}

// note feeds one tick: due at tickAt, handled `late` after it, dt after the previous tick.
func (e *Engine) note(tickAt, late, dt time.Duration) {
	e.simNow = tickAt
	e.inTick = true
	e.noteTick(tickAt, tickAt+late, dt)
	e.inTick = false
	e.publish()
}

func simVals(rec *recorder) string {
	var vs []string
	for _, ev := range rec.of("sim") {
		vs = append(vs, fmt.Sprint(ev.V))
	}
	return fmt.Sprint(vs)
}

// The behind-real-time flag is computed from tick lateness with a 2 s hold-off (UIC-30 / A-03-05).
func TestBehindRealTime(t *testing.T) {
	t.Run("on time stays in sync", func(t *testing.T) {
		e, rec := behindEngine()
		for i := 1; i <= 500; i++ {
			e.note(time.Duration(i)*tick, 0, tick)
		}
		if e.Snapshot().BehindRealTime || len(rec.of("sim")) != 0 {
			t.Fatalf("behind %v, sim events %s after 500 on-time ticks, want false and none", e.Snapshot().BehindRealTime, simVals(rec))
		}
	})

	t.Run("a tick after a long gap sets it once", func(t *testing.T) {
		e, rec := behindEngine()
		e.note(10*tick, 0, tick)
		e.note(10*tick+200*time.Millisecond, 0, 200*time.Millisecond) // dt 200 ms: the loop stalled
		if !e.Snapshot().BehindRealTime {
			t.Fatal("a 200 ms gap between ticks did not set behind")
		}
		e.note(10*tick+210*time.Millisecond, 0, tick)
		if got := simVals(rec); got != "[behind]" {
			t.Fatalf("sim events %s, want exactly one behind", got)
		}
	})

	t.Run("a tick handled 60 ms late counts", func(t *testing.T) {
		e, rec := behindEngine()
		e.note(tick, 60*time.Millisecond, tick)
		if !e.Snapshot().BehindRealTime || simVals(rec) != "[behind]" {
			t.Fatalf("behind %v, events %s, want a tick handled 60 ms late to set it", e.Snapshot().BehindRealTime, simVals(rec))
		}
	})

	t.Run("exactly the threshold is still on time", func(t *testing.T) {
		e, _ := behindEngine()
		e.note(tick, behindLate, tick)
		e.note(2*tick, 0, behindLate)
		if e.Snapshot().BehindRealTime {
			t.Fatal("50 ms is not more than 50 ms")
		}
	})

	t.Run("hysteresis", func(t *testing.T) {
		e, rec := behindEngine()
		at := time.Second
		e.note(at, 200*time.Millisecond, tick) // the late tick
		late := at
		for at += tick; at-late < behindClear; at += tick {
			e.note(at, 0, tick) // up to 1.99 s after the late one
		}
		if !e.Snapshot().BehindRealTime {
			t.Fatalf("cleared %v after the late tick, want it held for %v", at-tick-late, behindClear)
		}
		if at-late != behindClear {
			t.Fatalf("test arithmetic: at-late = %v", at-late)
		}
		e.note(at, 0, tick) // exactly 2 s
		if e.Snapshot().BehindRealTime {
			t.Fatal("still behind 2 s after the last late tick")
		}
		if got := simVals(rec); got != "[behind in_sync]" {
			t.Fatalf("sim events %s, want behind then in_sync, once each", got)
		}
	})

	t.Run("a second late tick restarts the hold-off", func(t *testing.T) {
		e, rec := behindEngine()
		at := time.Second
		e.note(at, 200*time.Millisecond, tick)
		first := at
		for at += tick; at < first+time.Second; at += tick {
			e.note(at, 0, tick)
		}
		e.note(at, 100*time.Millisecond, tick) // late again after 1 s
		second := at
		for at += tick; at-second < behindClear; at += tick {
			e.note(at, 0, tick)
			if at-first >= behindClear && !e.Snapshot().BehindRealTime {
				t.Fatalf("cleared at %v after the first late tick although a second came at %v", at-first, second-first)
			}
		}
		if !e.Snapshot().BehindRealTime {
			t.Fatal("cleared before 2 s after the second late tick")
		}
		e.note(at, 0, tick)
		if e.Snapshot().BehindRealTime {
			t.Fatal("still behind 2 s after the second late tick")
		}
		if got := simVals(rec); got != "[behind in_sync]" {
			t.Fatalf("sim events %s, want one behind and one in_sync (no flicker)", got)
		}
	})
}

// On the Manual clock every tick is delivered at its due time, so the flag never rises.
func TestBehindRealTimeManualClockStaysInSync(t *testing.T) {
	h := newHarness(t)
	h.advanceTo(5 * time.Second)
	if h.eng.Snapshot().BehindRealTime {
		t.Fatal("behind_real_time rose on a Manual clock")
	}
	if got := h.rec.of("sim"); len(got) != 0 {
		t.Fatalf("sim events %+v on a Manual clock, want none", got)
	}
}

// buttonEvents renders the button events as "id=level[:reason]".
func buttonEvents(rec *recorder) string {
	var out []string
	for _, ev := range rec.of("button") {
		x := fmt.Sprintf("%s=%v", ev.ID, ev.V)
		if ev.Reason != "" {
			x += ":" + ev.Reason
		}
		out = append(out, x)
	}
	return fmt.Sprint(out)
}

// D-15: a button held with a max hold is released by the engine on the first tick at or after the
// deadline, in sim time, even when nobody asks for it.
func TestMaxHold(t *testing.T) {
	t.Run("auto release", func(t *testing.T) {
		h := newButtonHarness(t)
		before := len(h.fw.buttonFrames("usr1"))
		if _, err := setButtonHold(t, h.eng, "usr1", true, true, 500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "the down frame", func() bool { return len(h.fw.buttonFrames("usr1")) == before+1 })
		h.advanceTo(490 * time.Millisecond)
		if !h.eng.Snapshot().Buttons["usr1"] {
			t.Fatal("released at 490 ms, before the 500 ms max hold")
		}
		h.advanceTo(10 * time.Millisecond)
		if h.eng.Snapshot().Buttons["usr1"] {
			t.Fatal("still held at 500 ms: the engine did not release it")
		}
		if got := buttonEvents(h.rec); got != "[usr1=true usr1=false:max_hold]" {
			t.Fatalf("button events %s, want the down and one max_hold release", got)
		}
		if rel := h.rec.of("button")[1]; rel.T != 500_000 {
			t.Fatalf("release stamped t=%d, want the tick time 500000", rel.T)
		}
		waitUntil(t, "the release frame", func() bool { return len(h.fw.buttonFrames("usr1")) == before+2 })
		fr := h.fw.buttonFrames("usr1")
		if v, _ := fr[len(fr)-1].GetF32("v"); v != 0 {
			t.Fatalf("release frame %s, want v 0", fr[len(fr)-1])
		}
		if _, err := setButton(t, h.eng, "usr1", false, true); !errors.Is(err, ErrButtonNotHeld) {
			t.Fatalf("release after the auto-release: %v, want ErrButtonNotHeld", err)
		}
		h.advanceTo(2 * time.Second)
		if n := len(h.rec.of("button")); n != 2 {
			t.Fatalf("%d button events after more ticks, want the release to happen once", n)
		}
	})

	t.Run("an explicit release cancels the deadline", func(t *testing.T) {
		h := newButtonHarness(t)
		if _, err := setButtonHold(t, h.eng, "usr1", true, true, 500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		h.advanceTo(200 * time.Millisecond)
		if _, err := setButton(t, h.eng, "usr1", false, true); err != nil {
			t.Fatal(err)
		}
		// A new hold without a max hold must not inherit the old deadline either.
		if _, err := setButton(t, h.eng, "usr1", true, true); err != nil {
			t.Fatal(err)
		}
		h.advanceTo(2 * time.Second)
		if !h.eng.Snapshot().Buttons["usr1"] {
			t.Fatal("the cancelled deadline released the second hold")
		}
		if got := buttonEvents(h.rec); got != "[usr1=true usr1=false usr1=true]" {
			t.Fatalf("button events %s, want no max_hold release", got)
		}
	})

	t.Run("zero never releases", func(t *testing.T) {
		h := newButtonHarness(t)
		if _, err := setButtonHold(t, h.eng, "usr3", true, true, 0); err != nil {
			t.Fatal(err)
		}
		h.advanceTo(30 * time.Second)
		if !h.eng.Snapshot().Buttons["usr3"] {
			t.Fatal("a hold without a max hold was released")
		}
	})

	t.Run("deadlines are per button and a new engine has none", func(t *testing.T) {
		h := newButtonHarness(t)
		if _, err := setButtonHold(t, h.eng, "usr1", true, true, 300*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if _, err := setButtonHold(t, h.eng, "usr2", true, true, 800*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		h.advanceTo(300 * time.Millisecond)
		snap := h.eng.Snapshot().Buttons
		if snap["usr1"] || !snap["usr2"] {
			t.Fatalf("buttons %v at 300 ms, want usr1 released and usr2 held", snap)
		}
		h.advanceTo(500 * time.Millisecond)
		if h.eng.Snapshot().Buttons["usr2"] {
			t.Fatal("usr2 still held at 800 ms")
		}
		fresh := newButtonHarness(t)
		if len(fresh.eng.holdUntil) != 0 {
			t.Fatalf("a new engine starts with deadlines %v", fresh.eng.holdUntil)
		}
	})
}

package bus

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

const guard = 5 * time.Second

// recorder is a Handler that logs every event as one string.
type recorder struct {
	mu       sync.Mutex
	events   []string
	snapshot []Frame
	manifest [][]Channel
	// helloGate, when set, blocks Hello until it is closed.
	helloGate chan struct{}
	onHello   func()
}

func (r *recorder) log(format string, args ...any) {
	r.mu.Lock()
	r.events = append(r.events, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

func (r *recorder) Hello(h Hello) []Frame {
	r.log("hello %s %d %d", h.Name, h.Proto, h.Chans)
	if r.onHello != nil {
		r.onHello()
	}
	if r.helloGate != nil {
		<-r.helloGate
	}
	return r.snapshot
}
func (r *recorder) Manifest(chs []Channel) {
	r.mu.Lock()
	r.manifest = append(r.manifest, chs)
	r.mu.Unlock()
	r.log("manifest %d", len(chs))
}
func (r *recorder) Frame(f Frame)           { r.log("frame %s", f) }
func (r *recorder) Disconnected(why string) { r.log("disconnected %q", why) }
func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}
func (r *recorder) count(prefix string) (n int) {
	for _, e := range r.all() {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

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

// startServer serves rec on a short socket path with the real clock.
func startServer(t *testing.T, rec *recorder, configure ...func(*Server)) (*Server, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "bus.sock")
	srv, err := Listen(path, clock.NewReal())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range configure {
		f(srv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, rec) }()
	t.Cleanup(func() {
		cancel()
		tm := clock.NewReal().NewTimer(guard)
		defer tm.Stop()
		select {
		case <-done:
		case <-tm.C():
			t.Error("Serve did not return")
		}
	})
	return srv, path
}

// client is a raw socket client that records the lines it receives.
type client struct {
	t    *testing.T
	conn net.Conn

	mu     sync.Mutex
	lines  []string
	closed chan struct{}
}

func dial(t *testing.T, path string) *client {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cl := &client{t: t, conn: c, closed: make(chan struct{})}
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		defer close(cl.closed)
		sc := bufio.NewScanner(c)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<16)
		for sc.Scan() {
			cl.mu.Lock()
			cl.lines = append(cl.lines, sc.Text())
			cl.mu.Unlock()
		}
	}()
	return cl
}

func (c *client) write(s string) { _, _ = c.conn.Write([]byte(s)) }
func (c *client) sendFrame(f Frame) {
	c.t.Helper()
	if _, err := c.conn.Write(Encode(f)); err != nil {
		c.t.Fatalf("write %s: %v", f, err)
	}
}
func (c *client) hello(name string, chans int64) {
	c.t.Helper()
	c.sendFrame(NewFrame("hello").Int("proto", Proto).Str("name", name).Int("chans", chans).Int("t", 0))
}
func (c *client) got() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// kinds returns the "k" of every received line.
func (c *client) kinds() []string {
	var ks []string
	for _, l := range c.got() {
		f, err := Decode([]byte(l))
		if err != nil {
			ks = append(ks, "undecodable:"+l)
			continue
		}
		ks = append(ks, f.Kind())
	}
	return ks
}

func (c *client) waitKinds(want ...string) {
	c.t.Helper()
	waitUntil(c.t, fmt.Sprintf("kinds %v", want), func() bool { return len(c.got()) >= len(want) })
	if got := c.kinds()[:len(want)]; !reflect.DeepEqual(got, want) {
		c.t.Fatalf("received kinds %v, want prefix %v", c.kinds(), want)
	}
}

func (c *client) waitClosed() {
	c.t.Helper()
	tm := clock.NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case <-c.closed:
	case <-tm.C():
		c.t.Fatalf("connection not closed by the server; received %v", c.got())
	}
}

func TestHelloTimeoutIsTwoSeconds(t *testing.T) {
	if HelloTimeout != 2*time.Second {
		t.Fatalf("HelloTimeout = %v, want 2s", HelloTimeout)
	}
	srv, err := Listen(filepath.Join(t.TempDir(), "b.sock"), clock.NewReal())
	if err != nil {
		t.Skipf("tempdir path unusable for a socket: %v", err)
	}
	defer srv.Close()
	if srv.helloTimeout != 2*time.Second {
		t.Fatalf("server hello timeout = %v, want 2s", srv.helloTimeout)
	}
}

func TestSilentClientIsClosed(t *testing.T) {
	rec := &recorder{}
	_, path := startServer(t, rec, func(s *Server) { s.helloTimeout = 150 * time.Millisecond })
	c := dial(t, path)
	c.waitClosed()
	// No session was active, so the stray client is reported.
	waitUntil(t, "the timeout to be reported", func() bool { return rec.count(`disconnected "hello_timeout"`) == 1 })
	if rec.count("hello") != 0 {
		t.Fatalf("handler saw a hello from a silent client: %v", rec.all())
	}
}

func TestFirstFrameMustBeHello(t *testing.T) {
	rec := &recorder{}
	_, path := startServer(t, rec)
	c := dial(t, path)
	c.sendFrame(NewFrame("out").Str("ch", "port1.duty").Num("v", 0.4).Int("t", 0))
	c.waitClosed()
	ks := c.kinds()
	if len(ks) != 1 || ks[0] != "err" {
		t.Fatalf("received %v, want one err frame", c.got())
	}
	waitUntil(t, "no_hello to be reported", func() bool { return rec.count(`disconnected "no_hello"`) == 1 })
}

func TestProtoMismatch(t *testing.T) {
	rec := &recorder{}
	_, path := startServer(t, rec)
	c := dial(t, path)
	c.sendFrame(NewFrame("hello").Int("proto", 2).Str("name", "old").Int("chans", 0).Int("t", 0))
	c.waitClosed()
	got := c.got()
	if len(got) != 1 || !strings.Contains(got[0], `"k":"err"`) || !strings.Contains(got[0], "proto mismatch: sim 1, firmware 2") {
		t.Fatalf("received %v, want one err frame naming the proto mismatch", got)
	}
	waitUntil(t, "proto_mismatch to be reported", func() bool { return rec.count(`disconnected "proto_mismatch"`) == 1 })
	if rec.count("hello") != 0 {
		t.Fatalf("Handler.Hello called for a mismatching peer: %v", rec.all())
	}
}

func TestBadFrameClosesTheSession(t *testing.T) {
	for _, tc := range []struct {
		name, line, reason string
	}{
		{"line over the cap", strings.Repeat("x", 4097) + "\n", "bad_frame: too_long"},
		{"line over the cap without newline", strings.Repeat("x", 5000), "bad_frame: too_long"},
		{"trailing garbage", `{"k":"out","ch":"a","v":1} x` + "\n", "bad_frame: trailing"},
		{"not an object", "hello\n", "bad_frame: not_object"},
		{"nested value", `{"k":"out","v":{}}` + "\n", "bad_frame: nested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			_, path := startServer(t, rec)
			c := dial(t, path)
			c.hello("fw", 0)
			c.waitKinds("welcome", "sync")
			c.write(tc.line)
			c.waitClosed()
			waitUntil(t, "the reason to be reported", func() bool { return rec.count(fmt.Sprintf("disconnected %q", tc.reason)) == 1 })
			ks := c.kinds()
			if ks[len(ks)-1] != "err" {
				t.Fatalf("last frame before the close = %v, want err", ks)
			}
		})
	}
}

func TestSessionOrderAndHeldSends(t *testing.T) {
	rec := &recorder{
		snapshot:  []Frame{NewFrame("in").Str("ch", "port1.rpm").Num("v", 100).Int("t", 7), NewFrame("in").Str("ch", "port2.rpm").Num("v", 200).Int("t", 7)},
		helloGate: make(chan struct{}),
	}
	srv, path := startServer(t, rec)
	rec.onHello = func() {
		// A steady-state frame that the engine sends while it computes the snapshot.
		if err := srv.Send(NewFrame("in").Str("ch", "port1.rpm").Num("v", 150).Int("t", 8)); err != nil {
			t.Errorf("Send during hello: %v", err)
		}
		close(rec.helloGate)
	}
	c := dial(t, path)
	c.hello("fw", 0)
	c.waitKinds("welcome", "in", "in", "sync", "in")
	got := c.got()
	if !strings.Contains(got[4], `"v":150`) {
		t.Fatalf("held frame = %s, want the v 150 frame after sync", got[4])
	}
	if !strings.Contains(got[0], `"proto":1`) {
		t.Fatalf("welcome = %s, want proto 1", got[0])
	}
	// Later sends go straight out.
	if err := srv.Send(NewFrame("in").Str("ch", "port2.rpm").Num("v", 250).Int("t", 9)); err != nil {
		t.Fatal(err)
	}
	c.waitKinds("welcome", "in", "in", "sync", "in", "in")
}

func TestSendWithoutSession(t *testing.T) {
	srv, _ := startServer(t, &recorder{})
	if err := srv.Send(NewFrame("in")); err != nil {
		t.Fatalf("Send with no session = %v, want nil", err)
	}
}

func TestManifestAtSyncAndOutFramesInOrder(t *testing.T) {
	rec := &recorder{}
	_, path := startServer(t, rec)
	c := dial(t, path)
	c.hello("fw", 2)
	c.sendFrame(NewFrame("chan").Str("ch", "port1.duty").Str("dir", "out").Str("type", "f32"))
	c.sendFrame(NewFrame("chan").Str("ch", "port1.rpm").Str("dir", "in").Str("type", "f32"))
	c.sendFrame(NewFrame("out").Str("ch", "port1.duty").Num("v", 0.5).Int("t", 1))
	c.sendFrame(NewFrame("sync").Int("t", 1))
	c.sendFrame(NewFrame("sync").Int("t", 2)) // a second sync is ignored
	c.sendFrame(NewFrame("out").Str("ch", "port1.duty").Num("v", 0.6).Int("t", 3))
	waitUntil(t, "both out frames", func() bool { return rec.count("frame") == 2 })
	ev := rec.all()
	if !strings.HasPrefix(ev[0], "hello fw 1 2") {
		t.Fatalf("events %v: want the hello first", ev)
	}
	if ev[1] != "frame "+NewFrame("out").Str("ch", "port1.duty").Num("v", 0.5).Int("t", 1).String() {
		t.Fatalf("events %v: want the snapshot out frame second", ev)
	}
	if ev[2] != "manifest 2" {
		t.Fatalf("events %v: want the manifest at the sync, after the snapshot's out frames", ev)
	}
	if rec.count("manifest") != 1 {
		t.Fatalf("manifest delivered %d times, want once", rec.count("manifest"))
	}
	want := []Channel{{"port1.duty", "out", "f32"}, {"port1.rpm", "in", "f32"}}
	if !reflect.DeepEqual(rec.manifest[0], want) {
		t.Fatalf("manifest = %v, want %v", rec.manifest[0], want)
	}
}

func TestNewerConnectionReplacesOlder(t *testing.T) {
	rec := &recorder{}
	_, path := startServer(t, rec)
	a := dial(t, path)
	a.hello("a", 0)
	a.waitKinds("welcome", "sync")
	b := dial(t, path)
	b.hello("b", 0)
	b.waitKinds("welcome", "sync")

	// The older one got bye, then EOF.
	a.waitClosed()
	if ks := a.kinds(); ks[len(ks)-1] != "bye" {
		t.Fatalf("older connection received %v, want bye last", ks)
	}
	ev := rec.all()
	want := []string{"hello a 1 0", `disconnected "replaced"`, "hello b 1 0"}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("events %v, want %v", ev, want)
	}
	// Frames of the replaced connection are discarded, frames of the new one arrive.
	a.write(string(Encode(NewFrame("out").Str("ch", "late").Num("v", 1).Int("t", 0))))
	b.sendFrame(NewFrame("out").Str("ch", "fresh").Num("v", 2).Int("t", 0))
	waitUntil(t, "the fresh frame", func() bool { return rec.count("frame") == 1 })
	if ev := rec.all(); strings.Contains(strings.Join(ev, "\n"), "late") {
		t.Fatalf("a frame of the replaced connection was delivered: %v", ev)
	}
	// Closing the replaced connection afterwards reports nothing new.
	_ = a.conn.Close()
	b.sendFrame(NewFrame("out").Str("ch", "fresh").Num("v", 3).Int("t", 0))
	waitUntil(t, "the second fresh frame", func() bool { return rec.count("frame") == 2 })
	if rec.count("disconnected") != 1 {
		t.Fatalf("events %v: want only the replaced notification", rec.all())
	}
}

func TestStrayClientDoesNotDisturbTheSession(t *testing.T) {
	rec := &recorder{}
	srv, path := startServer(t, rec, func(s *Server) { s.helloTimeout = 200 * time.Millisecond })
	a := dial(t, path)
	a.hello("a", 0)
	a.waitKinds("welcome", "sync")
	stray := dial(t, path) // silent, then garbage
	stray.write("garbage\n")
	stray.waitClosed()
	silent := dial(t, path)
	silent.waitClosed()
	if rec.count("disconnected") != 0 {
		t.Fatalf("a stray client disturbed the active session: %v", rec.all())
	}
	if err := srv.Send(NewFrame("in").Str("ch", "port1.rpm").Num("v", 1).Int("t", 0)); err != nil {
		t.Fatal(err)
	}
	a.waitKinds("welcome", "sync", "in")
}

func TestPeerByeAndErrEndTheSession(t *testing.T) {
	for _, tc := range []struct{ name, send, reason string }{
		{"bye", `{"k":"bye"}`, `disconnected ""`},
		{"err", `{"k":"err","msg":"boom"}`, `disconnected "peer_error: boom"`},
		{"second hello", `{"k":"hello","proto":1}`, `disconnected "bad_frame: duplicate hello"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			_, path := startServer(t, rec)
			c := dial(t, path)
			c.hello("fw", 0)
			c.waitKinds("welcome", "sync")
			c.write(tc.send + "\n")
			c.waitClosed()
			waitUntil(t, "the end to be reported", func() bool { return rec.count(tc.reason) == 1 })
		})
	}
}

func TestCloseUnlinksTheSocket(t *testing.T) {
	rec := &recorder{}
	srv, path := startServer(t, rec)
	c := dial(t, path)
	c.hello("fw", 0)
	c.waitKinds("welcome", "sync")
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	c.waitClosed()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still present after Close: %v", err)
	}
	if n := rec.count("disconnected"); n != 0 {
		t.Fatalf("Close reported %d disconnects, want none", n)
	}
}

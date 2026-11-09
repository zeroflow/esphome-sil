package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// watchControl is a Control whose Watch runs a script and counts how many streams are open.
type watchControl struct {
	keyControl
	script func(ctx context.Context, send func(any) error) error
	calls  atomic.Int32
	active atomic.Int32
}

func (w *watchControl) Watch(ctx context.Context, _ control.WatchRequest, send func(any) error) error {
	w.calls.Add(1)
	w.active.Add(1)
	defer w.active.Add(-1)
	return w.script(ctx, send)
}

// snapshotThenBlock sends the snapshot and then holds the stream open until it ends.
func snapshotThenBlock(ctx context.Context, send func(any) error) error {
	if err := send(control.SnapshotLine{K: "snapshot", Seq: 1}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws" }

// dialRead opens /ws and reads the first frame.
func dialRead(t *testing.T, url string) (*websocket.Conn, string, error) {
	t.Helper()
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	_, data, err := c.Read(ctx)
	return c, string(data), err
}

// waitFor polls cond on the real clock for up to 3 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := within(3 * time.Second)
	defer cancel()
	for !cond() {
		if rt.Sleep(ctx, 10*time.Millisecond) != nil {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// Each /ws text frame plus a newline is the matching line of GET /api/v1/watch (D-12).
func TestWSFramesEqualWatchLines(t *testing.T) {
	ctl := &watchControl{script: func(_ context.Context, send func(any) error) error {
		lines := []any{
			control.SnapshotLine{T: 1500, K: "snapshot", Seq: 3, Status: keyControl{}.status()},
			events.Event{T: 2000, K: "duty", Seq: 4, Port: 1, V: 0.425},
			events.Event{T: 2500, K: "env", Seq: 5, ID: "temperature", V: 23.5},
			events.Event{T: 3000, K: "leds", Seq: 6, Port: 2, Strip: []string{"ff0000", "00ff00"}},
			events.Event{T: 3500, K: "firmware", Seq: 7, V: "a<b> & \"c\" é ☃"},
			events.Event{T: 4000, K: "lagged", Dropped: 12},
			control.NewStoppedLine(5 * time.Second),
		}
		for _, l := range lines {
			if err := send(l); err != nil {
				return err
			}
		}
		return nil
	}}
	srv := httptest.NewServer(NewHandler(ctl, Options{}))
	defer srv.Close()

	var frames []string
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("stream ended with %v, want a normal closure", err)
			}
			break
		}
		if typ != websocket.MessageText {
			t.Fatalf("frame type %v, want text", typ)
		}
		frames = append(frames, string(data)+"\n")
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/watch", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var lines []string
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			lines = append(lines, line)
		}
		if err != nil {
			break
		}
	}
	if len(frames) != 7 || len(lines) != 7 {
		t.Fatalf("%d frames and %d NDJSON lines, want 7 of each", len(frames), len(lines))
	}
	for i := range frames {
		if frames[i] != lines[i] {
			t.Errorf("frame %d = %q, NDJSON line = %q", i, frames[i], lines[i])
		}
		if !json.Valid([]byte(frames[i])) {
			t.Errorf("frame %d is not JSON: %q", i, frames[i])
		}
	}
	if strings.Contains(frames[0], secretKey) {
		t.Errorf("snapshot frame leaks the key: %s", frames[0])
	}
}

// pingTap is a listener that closes pinged when the server writes its first ping frame (opcode 0x89
// at the start of a write): from then on the library holds the connection's write lock for that frame.
type pingTap struct {
	net.Listener
	pinged chan struct{}
	once   sync.Once
}

func (l *pingTap) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return tapConn{Conn: c, l: l}, nil
}

type tapConn struct {
	net.Conn
	l *pingTap
}

func (c tapConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 && p[0] == 0x89 {
		c.l.once.Do(func() { close(c.l.pinged) })
	}
	return n, err
}

// A browser that stops answering pings is closed 15 s + 10 s after its last sign of life, and both
// the Watch slot and the browser slot come back.
//
// The manual clock jumps the library's own timeouts too: coder/websocket closes the connection hard
// when the context of a write expires while the write is still in flight (its write timeout), and
// then no going-away close frame is ever sent. The test therefore never advances time while a write
// is in flight: the snapshot write is over once the script's send returned (sent), and the ping
// write is over once a later frame, which needs the same write lock, has been sent (barrier).
func TestWSPingReaps(t *testing.T) {
	mc := clock.NewManual()
	sent := make(chan struct{})
	var sentOnce sync.Once // a second Watch call (e.g. a retried stream) must not close sent twice
	barrier := make(chan chan struct{})
	ctl := &watchControl{script: func(ctx context.Context, send func(any) error) error {
		if err := send(control.SnapshotLine{K: "snapshot", Seq: 1}); err != nil {
			return err
		}
		sentOnce.Do(func() { close(sent) })
		for {
			select {
			case done := <-barrier:
				err := send(events.Event{K: "env", Seq: 2, ID: "temperature", V: 21.0})
				close(done)
				if err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}}
	srv := httptest.NewUnstartedServer(NewHandler(ctl, Options{Clock: mc}))
	tap := &pingTap{Listener: srv.Listener, pinged: make(chan struct{})}
	srv.Listener = tap
	srv.Start()
	defer srv.Close()

	// This client reads the snapshot and then never reads again, so it never answers a ping.
	dead, first, err := dialRead(t, wsURL(srv))
	if err != nil || !strings.Contains(first, `"snapshot"`) {
		t.Fatalf("first frame = %q (%v)", first, err)
	}
	if got := ctl.active.Load(); got != 1 {
		t.Fatalf("%d streams open, want 1", got)
	}
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	await := func(what string, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", what)
		}
	}
	await("the snapshot write to finish", sent)

	// The ping ticker is armed before the first frame is written. The ping arms its pong timer before
	// it writes, so once the ping is on the wire the pong budget is running.
	mc.Advance(wsPingEvery)
	await("the ping frame", tap.pinged)
	done := make(chan struct{})
	select {
	case barrier <- done:
	case <-ctx.Done():
		t.Fatal("the script never took the barrier")
	}
	await("a frame behind the ping", done)
	if got := ctl.active.Load(); got != 1 {
		t.Fatalf("%d streams open right after the ping, want the pong budget to still run", got)
	}
	mc.Advance(wsPongBudget)

	waitFor(t, "the stalled stream to be reaped", func() bool { return ctl.active.Load() == 0 })
	// The client now reads what the server queued: the ping (answered by Read), the env frame, then the
	// going-away close.
	for {
		_, _, err := dead.Read(ctx)
		if err == nil {
			continue
		}
		if websocket.CloseStatus(err) != websocket.StatusGoingAway {
			t.Fatalf("stalled client read = %v, want a going-away close", err)
		}
		break
	}

	// Both slots are free: a full set of maxBrowserStreams streams fits again.
	for i := 0; i < maxBrowserStreams; i++ {
		if _, f, err := dialRead(t, wsURL(srv)); err != nil || !strings.Contains(f, `"snapshot"`) {
			t.Fatalf("stream %d after the reap: %q (%v)", i+1, f, err)
		}
	}
}

// A browser that answers its pings is never reaped, however many ping periods pass.
func TestWSPingKeepsAnsweringClients(t *testing.T) {
	mc := clock.NewManual()
	fire := make(chan struct{})
	sent := make(chan struct{})
	var sentOnce sync.Once // a second Watch call must not close sent twice
	ctl := &watchControl{script: func(ctx context.Context, send func(any) error) error {
		if err := send(control.SnapshotLine{K: "snapshot", Seq: 1}); err != nil {
			return err
		}
		sentOnce.Do(func() { close(sent) })
		for {
			select {
			case <-fire:
				if err := send(events.Event{K: "env", Seq: 2, ID: "temperature", V: 21.0}); err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}}
	srv := httptest.NewServer(NewHandler(ctl, Options{Clock: mc}))
	defer srv.Close()

	ctx, cancel := within(10 * time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	frames := make(chan string, 4)
	go func() { // Read answers pings while it waits.
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				close(frames)
				return
			}
			frames <- string(data)
		}
	}()
	if f := <-frames; !strings.Contains(f, `"snapshot"`) {
		t.Fatalf("first frame = %q", f)
	}
	// Time jumps only once the snapshot write is over (see TestWSPingReaps).
	select {
	case <-sent:
	case <-ctx.Done():
		t.Fatal("the snapshot write never finished")
	}
	for i := 0; i < 3; i++ {
		mc.Advance(wsPingEvery)
		// The pong comes back in microseconds; the pong timer is stopped when it does.
		_ = rt.Sleep(ctx, 200*time.Millisecond)
		mc.Advance(wsPongBudget)
	}
	fire <- struct{}{}
	select {
	case f, ok := <-frames:
		if !ok || !strings.Contains(f, `"temperature"`) {
			t.Fatalf("after three ping periods got %q (open %v), want the env frame", f, ok)
		}
	case <-ctx.Done():
		t.Fatal("no frame after three ping periods")
	}
	if got := ctl.active.Load(); got != 1 {
		t.Fatalf("%d streams open, want 1", got)
	}
}

// At most maxBrowserStreams browser streams run at once; the next one gets one error frame and a
// close and never reaches Watch.
func TestBrowserStreamCap(t *testing.T) {
	ctl := &watchControl{script: snapshotThenBlock}
	srv := httptest.NewServer(NewHandler(ctl, Options{}))
	defer srv.Close()

	var open []*websocket.Conn
	for i := 0; i < maxBrowserStreams; i++ {
		c, f, err := dialRead(t, wsURL(srv))
		if err != nil || !strings.Contains(f, `"snapshot"`) {
			t.Fatalf("stream %d: %q (%v)", i+1, f, err)
		}
		open = append(open, c)
	}

	c9, f, err := dialRead(t, wsURL(srv))
	if err != nil {
		t.Fatalf("9th stream: %v", err)
	}
	var fr struct {
		Error struct {
			Code   string         `json:"code"`
			Detail map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(f), &fr); err != nil || fr.Error.Code != "too_many_streams" || fr.Error.Detail["limit"] != float64(8) {
		t.Fatalf("9th stream first frame = %q, want a too_many_streams error with limit 8", f)
	}
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	if _, _, err := c9.Read(ctx); websocket.CloseStatus(err) != websocket.StatusTryAgainLater {
		t.Fatalf("9th stream after the error frame: %v, want a try-again-later close", err)
	}
	if got := ctl.calls.Load(); got != int32(maxBrowserStreams) {
		t.Fatalf("Watch called %d times, want %d: the refused stream must not reach it", got, maxBrowserStreams)
	}

	// One of the eight goes away; a new page streams.
	_ = open[0].CloseNow()
	waitFor(t, "a browser slot to free up", func() bool { return ctl.active.Load() == int32(maxBrowserStreams-1) })
	if _, f, err := dialRead(t, wsURL(srv)); err != nil || !strings.Contains(f, `"snapshot"`) {
		t.Fatalf("stream after one closed: %q (%v)", f, err)
	}
}

// The snapshot of a failed instance carries the status error as a top-level "error" key next to
// "k": "snapshot" (CR-01). The page tells it from the final error frame of a failed stream, which has
// "error" and no "k", so this shape is pinned on both sides.
func TestWSSnapshotOfFailedInstanceKeepsK(t *testing.T) {
	st := keyControl{}.status()
	st.Phase = control.PhaseFailed
	st.Error = apperr.Infra("firmware_crash_loop", "the firmware crashed", nil)
	ctl := &watchControl{script: func(_ context.Context, send func(any) error) error {
		return send(control.SnapshotLine{K: "snapshot", Seq: 1, Status: st})
	}}
	srv := httptest.NewServer(NewHandler(ctl, Options{}))
	defer srv.Close()
	_, f, err := dialRead(t, wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	var fr struct {
		K     *string `json:"k"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(f), &fr); err != nil {
		t.Fatal(err)
	}
	if fr.K == nil || *fr.K != "snapshot" || fr.Error == nil || fr.Error.Code != "firmware_crash_loop" {
		t.Fatalf("snapshot of a failed instance = %s, want k snapshot together with the status error", f)
	}
}

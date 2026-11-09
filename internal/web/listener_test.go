package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// rt is the real clock of these tests: they exercise real TCP connections, and every wait goes
// through it (never context.WithTimeout).
var rt = clock.NewReal()

// tickControl is a Control whose Watch sends the snapshot and then one tick event every 100 ms until
// the stream ends.
type tickControl struct{ keyControl }

func (tickControl) Watch(ctx context.Context, _ control.WatchRequest, send func(any) error) error {
	if err := send(control.SnapshotLine{K: "snapshot", Seq: 1}); err != nil {
		return err
	}
	for n := 1; ; n++ {
		if err := rt.Sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
		if err := send(map[string]any{"k": "tick", "n": n}); err != nil {
			return err
		}
	}
}

// serveLimited serves h behind Limit(o) on a loopback port and returns its address. The server is
// shut down and joined when the test ends.
func serveLimited(t *testing.T, h http.Handler, o ListenerOptions) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, Limit(ln, rt, o), h) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve = %v", err)
		}
	})
	return ln.Addr().String()
}

// within runs f with a context that ends after d on the real clock.
func within(d time.Duration) (context.Context, context.CancelFunc) {
	return clock.WithTimeout(context.Background(), rt, d)
}

// expectClosed fails unless the server closes conn within d. It may send a final response first
// (net/http answers a stalled partial request with 408 before it closes).
func expectClosed(t *testing.T, conn net.Conn, d time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(rt.Deadline(d))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatalf("connection still open after %v", d)
		}
	}
}

func TestSlowHeader(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{HeaderBudget: 200 * time.Millisecond})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HT")); err != nil {
		t.Fatal(err)
	}
	expectClosed(t, conn, 2*time.Second)
}

// A client that trickles its headers one byte at a time is closed when the absolute budget ends,
// however steadily it sends.
func TestTrickledHeaderIsClosed(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{HeaderBudget: 300 * time.Millisecond})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	go func() {
		for _, b := range []byte("GET / HTTP/1.1\r\nHost: x\r\nX-Slow: " + strings.Repeat("a", 200)) {
			if _, err := conn.Write([]byte{b}); err != nil {
				return
			}
			if rt.Sleep(ctx, 40*time.Millisecond) != nil {
				return
			}
		}
	}()
	start := rt.Now()
	expectClosed(t, conn, 2*time.Second)
	if took := rt.Now() - start; took > 1500*time.Millisecond {
		t.Fatalf("closed after %v, want about the 300 ms budget", took)
	}
}

// A body trickled after the headers cannot hold a slot: LimitBody re-arms a deadline from the
// injected clock, because net/http clears the header deadline once the headers are read.
func TestStalledBodyIsClosed(t *testing.T) {
	read := make(chan error, 1)
	h := LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
		read <- err
	}), rt, 300*time.Millisecond)
	addr := serveLimited(t, h, ListenerOptions{})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "POST /api/v1/set HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nabcde"); err != nil {
		t.Fatal(err)
	}
	wait, cancel := within(3 * time.Second)
	defer cancel()
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("the stalled body was read to the end")
		}
	case <-wait.Done():
		t.Fatal("the handler is still waiting for a stalled body after 3 s")
	}
}

// Requests whose body arrives in time, and requests without one (GET, /ws), pass LimitBody.
func TestLimitBodyLeavesGoodRequestsAlone(t *testing.T) {
	addr := serveLimited(t, LimitBody(NewHandler(tickControl{}, Options{}), rt, 200*time.Millisecond), ListenerOptions{})
	ctx, cancel := within(5 * time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST / = %d, want 404 (no page, but served)", resp.StatusCode)
	}
	c, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("stream ended before tick 6 (600 ms, three body budgets): %v", err)
		}
		if strings.Contains(string(data), `"n":6`) {
			return
		}
	}
}

func TestIdleConnectionIsClosed(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{HeaderBudget: 200 * time.Millisecond})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expectClosed(t, conn, 2*time.Second)
}

func TestStreamOutlivesHeaderBudget(t *testing.T) {
	addr := serveLimited(t, NewHandler(tickControl{}, Options{}), ListenerOptions{HeaderBudget: 200 * time.Millisecond})

	t.Run("ws", func(t *testing.T) {
		ctx, cancel := within(5 * time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("stream ended before tick 11 (1.1 s, five budgets): %v", err)
			}
			if strings.Contains(string(data), `"n":11`) {
				return
			}
		}
	})

	t.Run("ndjson", func(t *testing.T) {
		ctx, cancel := within(5 * time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/watch", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.Contains(sc.Text(), `"n":11`) {
				return
			}
		}
		t.Fatalf("stream ended before tick 11 (1.1 s, five budgets): %v", sc.Err())
	})
}

func TestConnCap(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{MaxConns: 2})
	a, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	// The kernel completes the handshake; the listener then closes the connection at once.
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	expectClosed(t, c, 2*time.Second)

	// The two admitted connections are still open (they simply have not sent anything).
	_ = a.SetReadDeadline(rt.Deadline(100 * time.Millisecond))
	if _, err := a.Read(make([]byte, 1)); err == nil {
		t.Fatal("first connection was closed")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("first connection: %v, want it open", err)
	}

	// One slot comes back when one of the two closes.
	_ = a.Close()
	ctx, cancel := within(3 * time.Second)
	defer cancel()
	for {
		resp, err := httpGet(ctx, "http://"+addr+"/")
		if err == nil {
			if resp != 200 {
				t.Fatalf("GET / = %d", resp)
			}
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("no connection was admitted after one closed: %v", err)
		}
		_ = rt.Sleep(ctx, 20*time.Millisecond)
	}
}

func httpGet(ctx context.Context, url string) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func TestKeepAlivesOff(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(rt.Deadline(3 * time.Second))
	br := bufio.NewReader(conn)
	req := "GET / HTTP/1.1\r\nHost: x\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !resp.Close {
		t.Errorf("response does not announce Connection: close: %v", resp.Header)
	}
	// A second request on the same connection is not served.
	_, _ = io.WriteString(conn, req)
	if _, err := http.ReadResponse(br, nil); err == nil {
		t.Fatal("a second request on the same connection was served")
	}
}

func TestLimitReleasesOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	l := Limit(ln, rt, ListenerOptions{MaxConns: 1})
	dial := func() net.Conn {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c1 := dial()
	defer c1.Close()
	a1, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_ = a1.Close()
	_ = a1.Close() // a second Close must not free a second slot
	c2, c3 := dial(), dial()
	defer c2.Close()
	defer c3.Close()
	a2, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	// c3 is over the cap: Accept would close it and wait for the next connection, so no further
	// connection is returned while a2 is open.
	ctx, cancel := within(200 * time.Millisecond)
	defer cancel()
	got := make(chan net.Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			got <- c
		}
	}()
	select {
	case c := <-got:
		_ = c.Close()
		t.Fatal("a third connection was admitted with MaxConns 1")
	case <-ctx.Done():
	}
	expectClosed(t, c3, time.Second)
}

// The handler of the cases above is reachable and the key is redacted through the limited
// listener, i.e. Limit and Serve change nothing about the routes.
func TestServeLimitedRoutes(t *testing.T) {
	addr := serveLimited(t, testHandler(), ListenerOptions{})
	ctx, cancel := within(3 * time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st control.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil || st.API.Key != RedactedKey {
		t.Fatalf("status = %+v (%v)", st, err)
	}
}

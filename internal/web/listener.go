package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// Defaults of ListenerOptions: enough for a handful of browsers and the CLI, small enough that a
// LAN machine cannot hold the listener's file descriptors.
const (
	defaultMaxConns     = 64
	defaultHeaderBudget = 10 * time.Second
)

// ListenerOptions bound what the LAN-facing listener will hold. Zero values take the defaults.
type ListenerOptions struct {
	// MaxConns is the number of connections held at once; the next one is closed at Accept.
	MaxConns int
	// HeaderBudget is how long a new connection has to deliver its request headers.
	HeaderBudget time.Duration
}

// Limit wraps ln so that at most o.MaxConns connections are open at once and every connection
// must deliver its request headers within o.HeaderBudget.
//
// The header budget is a read deadline set at Accept from the injected clock (no http.Server
// timeout field: those are wall-clock timers and the repository forbids them). It is an absolute
// time, so a client that trickles its headers byte by byte is cut off like one that stalls.
//
// This relies on how net/http (Go 1.27.3, net/http/server.go) treats the connection's read
// deadline, and holds only while keep-alives are off (Serve turns them off):
//   - conn.serve sets a deadline only when ReadHeaderTimeout is configured (line 2058), so the
//     Accept-time deadline is still armed when the first request is read;
//   - readRequest replaces it with wholeReqDeadline, zero without a ReadTimeout, only after the
//     headers are parsed (line 1103), so a /ws stream, a hijacked connection or a long NDJSON
//     response outlives the budget;
//   - the loop that clears the deadline before the next request (lines 2176 to 2200) is reached
//     only on a kept-alive connection, so with keep-alives off no request can have its header
//     read run under a cleared deadline.
//
// TestSlowHeader, TestTrickledHeaderIsClosed and TestStreamOutlivesHeaderBudget exercise this
// through the production Serve and Limit, so a Go release that changes it fails the tests.
// What the deadline does not bound is a request body trickled after the headers; LimitBody does.
func Limit(ln net.Listener, clk clock.Clock, o ListenerOptions) net.Listener {
	if o.MaxConns <= 0 {
		o.MaxConns = defaultMaxConns
	}
	if o.HeaderBudget <= 0 {
		o.HeaderBudget = defaultHeaderBudget
	}
	if clk == nil {
		clk = clock.NewReal()
	}
	return &limited{Listener: ln, clk: clk, budget: o.HeaderBudget, slots: make(chan struct{}, o.MaxConns)}
}

type limited struct {
	net.Listener
	clk    clock.Clock
	budget time.Duration
	slots  chan struct{}
}

// Accept returns the next connection that fits under the cap; connections over it are closed at
// once, which the peer sees as an immediate EOF.
func (l *limited) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
		default:
			_ = c.Close()
			continue
		}
		_ = c.SetReadDeadline(l.clk.Deadline(l.budget))
		return &limitedConn{Conn: c, release: func() { <-l.slots }}, nil
	}
}

// limitedConn gives its slot back exactly once, on the first Close.
type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

// Serve serves h on ln until ctx is done, one request per connection, then shuts the server
// down, giving in-flight requests 2 s before it closes them. It is control.ServeListener with
// keep-alives off, so an idle connection never outlives its response.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h}
	// Keep-alives off is load-bearing: Limit's header deadline is only sound while every
	// connection serves one request (see Limit).
	srv.SetKeepAlivesEnabled(false)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	sctx, cancel := clock.WithTimeout(context.Background(), clock.NewReal(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	<-done
	return nil
}

// LimitBody bounds how long a client may take to send a request body: for a request that has one,
// the connection's read deadline is set to budget from now on the injected clock (10 s when
// budget is not positive), because net/http clears the header deadline once the headers are in.
// Requests without a body (GET, HEAD, the /ws upgrade) are left alone, so streams are unaffected.
// The deadline is never cleared again: the listener serves one request per connection.
func LimitBody(h http.Handler, clk clock.Clock, budget time.Duration) http.Handler {
	if budget <= 0 {
		budget = defaultHeaderBudget
	}
	if clk == nil {
		clk = clock.NewReal()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			_ = http.NewResponseController(w).SetReadDeadline(clk.Deadline(budget))
		}
		h.ServeHTTP(w, r)
	})
}

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
// timeout field: those are wall-clock timers and the repository forbids them). net/http, with no
// ReadTimeout configured, replaces that deadline with none once it has read the headers, so a
// /ws stream, a hijacked connection or a long NDJSON response outlives the budget; the listener
// tests prove this behaviour of the standard library.
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

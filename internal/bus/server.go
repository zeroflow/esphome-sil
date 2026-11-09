package bus

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
)

// Handler receives events from the active bus connection. Callbacks run on
// the connection's goroutines and must not block for long.
type Handler interface {
	Connected()
	Frame(f Frame)
	Disconnected()
}

const sendQueue = 1024

// conn is one accepted connection with its own writer goroutine.
type conn struct {
	c    net.Conn
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.c.Close()
	})
}

// Server is the simulator side of the bus: it listens on a Unix socket and
// follows the newest connection (a firmware restart replaces the old one).
type Server struct {
	ln   *net.UnixListener
	path string

	mu     sync.Mutex
	active *conn
	closed bool
	wg     sync.WaitGroup
}

// Listen removes a stale socket file, listens on path and restricts the
// socket to its owner (mode 0600).
func Listen(path string) (*Server, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("bus: remove stale socket %s: %w", path, err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("bus: listen %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("bus: chmod %s: %w", path, err)
	}
	return &Server{ln: ln, path: path}, nil
}

// Serve accepts connections until ctx is done or the server is closed, and
// returns nil in both cases. A new connection replaces the older one.
func (s *Server) Serve(ctx context.Context, h Handler) error {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	defer s.wg.Wait()
	for {
		nc, err := s.ln.AcceptUnix()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("bus: accept: %w", err)
		}
		c := &conn{c: nc, out: make(chan []byte, sendQueue), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.close()
			return nil
		}
		old := s.active
		s.active = c
		s.wg.Add(2)
		s.mu.Unlock()
		if old != nil {
			old.close()
		}
		go s.writer(c)
		go s.reader(c, h)
	}
}

func (s *Server) isActive(c *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active == c
}

func (s *Server) writer(c *conn) {
	defer s.wg.Done()
	for {
		select {
		case b := <-c.out:
			if _, err := c.c.Write(b); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (s *Server) reader(c *conn, h Handler) {
	defer s.wg.Done()
	defer c.close()
	if s.isActive(c) {
		h.Connected()
	}
	sc := bufio.NewScanner(c.c)
	sc.Buffer(make([]byte, 0, MaxLine), MaxLine)
	for sc.Scan() {
		if !s.isActive(c) {
			return // replaced: the new connection owns the handler now
		}
		f, err := Decode(sc.Bytes())
		if err != nil {
			continue // lenient: a bad line is skipped; 01-06 turns it into an error frame
		}
		h.Frame(f)
	}
	// EOF, error, or a line over MaxLine (scanner stops): the connection ends.
	s.mu.Lock()
	wasActive := s.active == c
	if wasActive {
		s.active = nil
	}
	s.mu.Unlock()
	if wasActive {
		h.Disconnected()
	}
}

// Send queues f for the active connection. It returns nil when no connection
// is active and an error when the connection's queue is full (the peer has
// stopped reading); it never blocks.
func (s *Server) Send(f Frame) error {
	s.mu.Lock()
	c := s.active
	s.mu.Unlock()
	if c == nil {
		return nil
	}
	select {
	case c.out <- Encode(f):
		return nil
	default:
		return errors.New("bus: send queue full")
	}
}

// Close closes the listener and the active connection and unlinks the socket.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	c := s.active
	s.active = nil
	s.mu.Unlock()
	err := s.ln.Close()
	if c != nil {
		c.close()
	}
	if rerr := os.Remove(s.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

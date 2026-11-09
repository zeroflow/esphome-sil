package bus

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

const (
	// HelloTimeout is how long a new connection may stay silent before it is
	// closed: its first frame must be a hello.
	HelloTimeout = 2 * time.Second

	sendQueue    = 1024
	maxChans     = 1024 // chan frames accepted in one session manifest
	writeTimeout = 5 * time.Second
)

// Hello is the firmware's opening message.
type Hello struct {
	Proto int64
	Name  string
	Chans int64
	T     int64
}

// Channel is one entry of the firmware's channel manifest. N and Seg are only
// set for the pixel channel (type "rgb"): its LED count and its segment list
// ("label:from:to,..."); every other channel leaves them zero.
type Channel struct {
	Ch, Dir, Type string
	N             int64
	Seg           string
}

// Handler receives the events of the active session. Calls are serialized and
// arrive in the order the events happened on the wire; they must not block for
// long. Events of a connection that was replaced are never delivered after
// the newer connection's Hello.
type Handler interface {
	// Hello is called for a valid hello (any protocol version that matches
	// Proto). It returns the in-snapshot: the server answers with welcome,
	// these frames, then sync, and only afterwards forwards any frame sent
	// with Server.Send, in order.
	Hello(h Hello) []Frame
	// Manifest is called once per session when the firmware's snapshot ends
	// (its sync), with the chan frames it sent.
	Manifest(chs []Channel)
	// Frame is called for every out frame in arrival order, snapshot included.
	Frame(f Frame)
	// Disconnected is called once when the active session ends. reason is ""
	// for a plain close (EOF, bye), "replaced" when a newer connection took
	// over, "proto_mismatch", "hello_timeout", "no_hello", "bad_hello",
	// "bad_frame: <class>", "peer_error: <msg>" or "send_queue_full".
	// A connection that never completed a hello is reported (with its reason)
	// only when no session is active, so a stray client cannot hide a good one.
	Disconnected(reason string)
}

// Session states of a connection that is the active one (guarded by Server.mu).
const (
	stHolding = iota // hello accepted, snapshot not yet queued: Send is held back
	stReady          // welcome, snapshot and sync are queued: Send goes straight out
)

// conn is one accepted connection with its own writer goroutine.
type conn struct {
	c    net.Conn
	out  chan []byte
	done chan struct{} // closed on a hard close
	quit chan struct{} // closed to flush the queue and then close

	doneOnce, quitOnce sync.Once

	whyMu sync.Mutex
	why   string // reason recorded by whoever closed the connection

	// guarded by Server.mu
	state int
	held  [][]byte
}

func newConn(nc net.Conn) *conn {
	return &conn{c: nc, out: make(chan []byte, sendQueue), done: make(chan struct{}), quit: make(chan struct{})}
}

func (c *conn) close() {
	c.doneOnce.Do(func() {
		close(c.done)
		_ = c.c.Close()
	})
}

// finish flushes what is queued and then closes.
func (c *conn) finish() { c.quitOnce.Do(func() { close(c.quit) }) }

func (c *conn) enqueue(b []byte) bool {
	select {
	case c.out <- b:
		return true
	default:
		return false
	}
}

func (c *conn) setWhy(w string) {
	c.whyMu.Lock()
	if c.why == "" {
		c.why = w
	}
	c.whyMu.Unlock()
}

func (c *conn) getWhy() string {
	c.whyMu.Lock()
	defer c.whyMu.Unlock()
	return c.why
}

// Server is the simulator side of the bus: it listens on a Unix socket and
// follows the newest firmware session (a firmware restart replaces the old one).
type Server struct {
	ln           *net.UnixListener
	path         string
	clk          clock.Clock
	helloTimeout time.Duration

	mu     sync.Mutex // active, closed, conns and the per-connection state
	dmu    sync.Mutex // serializes every Handler call
	active *conn
	conns  map[*conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// Listen removes a stale socket file, listens on path and restricts the
// socket to its owner (mode 0600). clk supplies the hello deadline, the write
// deadline and the time stamps of welcome and sync.
func Listen(path string, clk clock.Clock) (*Server, error) {
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
	return &Server{ln: ln, path: path, clk: clk, helloTimeout: HelloTimeout, conns: map[*conn]struct{}{}}, nil
}

// Serve accepts connections until ctx is done or the server is closed, and
// returns nil in both cases.
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
		c := newConn(nc)
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.close()
			return nil
		}
		s.conns[c] = struct{}{}
		s.wg.Add(2)
		s.mu.Unlock()
		go s.writer(c)
		go s.session(c, h)
	}
}

func (s *Server) writer(c *conn) {
	defer s.wg.Done()
	write := func(b []byte) bool {
		_ = c.c.SetWriteDeadline(s.clk.Deadline(writeTimeout))
		if _, err := c.c.Write(b); err != nil {
			c.close()
			return false
		}
		return true
	}
	for {
		select {
		case b := <-c.out:
			if !write(b) {
				return
			}
		case <-c.quit:
			for {
				select {
				case b := <-c.out:
					if !write(b) {
						return
					}
				default:
					c.close()
					return
				}
			}
		case <-c.done:
			return
		}
	}
}

// session runs one connection from accept to close.
func (s *Server) session(c *conn, h Handler) {
	defer s.wg.Done()
	reason, activated := s.read(c, h)
	if reason == "" {
		reason = c.getWhy()
	}
	s.end(c, h, reason, activated)
	c.finish()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) sendErr(c *conn, msg string) {
	c.enqueue(Encode(NewFrame("err").Str("msg", msg)))
}

// splitLF frames on "\n" only. bufio.ScanLines would also drop a trailing
// "\r", hiding from Decode a byte that the protocol (and the firmware's parser)
// rejects.
func splitLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// read frames lines and drives the session until it ends; it returns the
// reason (see Handler.Disconnected) and whether the connection became active.
func (s *Server) read(c *conn, h Handler) (reason string, activated bool) {
	_ = c.c.SetReadDeadline(s.clk.Deadline(s.helloTimeout))
	sc := bufio.NewScanner(c.c)
	sc.Buffer(make([]byte, 0, MaxLine), MaxLine)
	sc.Split(splitLF)
	var chans []Channel
	manifest := false
	for sc.Scan() {
		f, err := Decode(sc.Bytes())
		if err != nil {
			class := "invalid"
			var de *DecodeError
			if errors.As(err, &de) {
				class = de.Class
			}
			s.sendErr(c, "bad frame: "+class)
			return "bad_frame: " + class, activated
		}
		if !activated {
			if f.Kind() != "hello" {
				s.sendErr(c, "expected hello as the first frame")
				return "no_hello", false
			}
			proto, ok := f.GetInt("proto")
			if !ok {
				s.sendErr(c, "hello needs an integer proto")
				return "bad_hello", false
			}
			hello := Hello{Proto: proto}
			hello.Name, _ = f.GetStr("name")
			hello.Chans, _ = f.GetInt("chans")
			hello.T, _ = f.GetInt("t")
			if proto != Proto {
				// The newest peer is the one status describes, so a mismatching
				// peer takes the slot too and is then refused.
				if !s.takeover(c, h) {
					return "", false
				}
				s.sendErr(c, fmt.Sprintf("proto mismatch: sim %d, firmware %d", Proto, proto))
				return "proto_mismatch", true
			}
			if why := s.open(c, h, hello); why != "" {
				return why, why != "replaced" && why != "closed"
			}
			activated = true
			_ = c.c.SetReadDeadline(time.Time{})
			continue
		}
		switch f.Kind() {
		case "hello":
			s.sendErr(c, "unexpected second hello")
			return "bad_frame: duplicate hello", true
		case "chan":
			if len(chans) >= maxChans {
				s.sendErr(c, "too many channels")
				return "bad_frame: too many chan frames", true
			}
			ch, ok := f.GetStr("ch")
			if !ok {
				s.sendErr(c, "chan needs a ch")
				return "bad_frame: chan without ch", true
			}
			dir, _ := f.GetStr("dir")
			typ, _ := f.GetStr("type")
			entry := Channel{Ch: ch, Dir: dir, Type: typ}
			entry.N, _ = f.GetInt("n")
			entry.Seg, _ = f.GetStr("seg")
			chans = append(chans, entry)
		case "sync":
			if manifest {
				continue
			}
			manifest = true
			if !s.deliver(c, func() { h.Manifest(chans) }) {
				return "replaced", true
			}
		case "out":
			if !s.deliver(c, func() { h.Frame(f) }) {
				return "replaced", true
			}
		case "bye":
			return "", true
		case "err":
			msg, _ := f.GetStr("msg")
			return "peer_error: " + msg, true
		}
		// Anything else (an in frame from the firmware, a future kind) is ignored.
	}
	err := sc.Err()
	switch {
	case err == nil, errors.Is(err, net.ErrClosed):
		return "", activated
	case errors.Is(err, bufio.ErrTooLong):
		s.sendErr(c, "line too long")
		return "bad_frame: too_long", activated
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "hello_timeout", false
	default:
		return "", activated
	}
}

// takeover makes c the active connection, closing the previous one after a
// bye, and reports false when the server is closed.
func (s *Server) takeover(c *conn, h Handler) bool {
	s.dmu.Lock()
	defer s.dmu.Unlock()
	return s.activateLocked(c, h)
}

func (s *Server) activateLocked(c *conn, h Handler) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	old := s.active
	s.active = c
	c.state = stHolding
	s.mu.Unlock()
	if old != nil && old != c {
		old.enqueue(Encode(NewFrame("bye")))
		old.finish()
		h.Disconnected("replaced")
	}
	return true
}

// open activates c, asks the handler for the in-snapshot and queues welcome,
// snapshot and sync followed by everything Send held back meanwhile. It
// returns "" on success or the reason the session cannot go on.
func (s *Server) open(c *conn, h Handler, hello Hello) string {
	s.dmu.Lock()
	defer s.dmu.Unlock()
	if !s.activateLocked(c, h) {
		return "closed"
	}
	frames := h.Hello(hello)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != c {
		return "replaced"
	}
	now := s.clk.Now().Microseconds()
	ok := c.enqueue(Encode(NewFrame("welcome").Int("proto", Proto).Int("t", now)))
	for _, f := range frames {
		ok = ok && c.enqueue(Encode(f))
	}
	ok = ok && c.enqueue(Encode(NewFrame("sync").Int("t", now)))
	for _, b := range c.held {
		ok = ok && c.enqueue(b)
	}
	c.held = nil
	c.state = stReady
	if !ok {
		c.close()
		return "send_queue_full"
	}
	return ""
}

// deliver runs fn under the handler lock when c is still the active session.
func (s *Server) deliver(c *conn, fn func()) bool {
	s.dmu.Lock()
	defer s.dmu.Unlock()
	s.mu.Lock()
	ok := s.active == c
	s.mu.Unlock()
	if ok {
		fn()
	}
	return ok
}

// end reports the close of c to the handler when it matters.
func (s *Server) end(c *conn, h Handler, reason string, activated bool) {
	s.dmu.Lock()
	defer s.dmu.Unlock()
	s.mu.Lock()
	closed := s.closed
	wasActive := s.active == c
	if wasActive {
		s.active = nil
	}
	none := s.active == nil
	s.mu.Unlock()
	if closed {
		return
	}
	switch {
	case wasActive:
		h.Disconnected(reason)
	case !activated && none && reason != "":
		h.Disconnected(reason)
	}
}

// Send queues f for the active session. Frames sent between a hello and the
// moment its snapshot is queued are kept and follow the snapshot. It returns
// nil when no session is active and an error when the peer has stopped
// reading (the connection is closed, the firmware reconnects and resyncs); it
// never blocks.
func (s *Server) Send(f Frame) error {
	b := Encode(f)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.active
	if c == nil {
		return nil
	}
	if c.state == stHolding {
		if len(c.held) >= sendQueue {
			c.setWhy("send_queue_full")
			c.close()
			return errors.New("bus: send queue full")
		}
		c.held = append(c.held, b)
		return nil
	}
	if !c.enqueue(b) {
		c.setWhy("send_queue_full")
		c.close()
		return errors.New("bus: send queue full")
	}
	return nil
}

// Close closes the listener and every connection and unlinks the socket.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.active = nil
	all := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		all = append(all, c)
	}
	s.mu.Unlock()
	err := s.ln.Close()
	for _, c := range all {
		c.close()
	}
	if rerr := os.Remove(s.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

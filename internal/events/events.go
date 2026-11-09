// Package events is the supervisor-owned broker behind `sil watch`: the engine publishes typed
// change events, every watcher gets its own bounded queue, and nobody ever blocks the engine.
//
// The broker lives as long as the instance, not as long as one simulator engine, so a stream
// survives `sil sim restart` (the new engine publishes into the same broker). Event names and
// fields are internal while the project is 0.x (D-12).
package events

import (
	"errors"
	"sync"
)

const (
	// MaxSubscribers is how many streams one instance serves at once.
	MaxSubscribers = 16
	// SubscriberBuffer is the size of each subscriber's queue.
	SubscriberBuffer = 1024
)

// Kinds are the change events the engine and the supervisor publish. The stream also carries
// snapshot, lagged and stopped lines, which are not kinds a watcher can select.
var Kinds = []string{"duty", "rpm", "state", "fan", "button", "env", "leds", "firmware", "sim"}

// Event is one change. T is simulator time in microseconds, Seq a strictly increasing number per
// broker. V holds the new value; its meaning depends on K (see the Kinds list in the SUMMARY of
// plan 02-06): duty and rpm a number, state running|stopped, fan an id, button a bool, env a
// number, firmware a bool, sim "restarted".
type Event struct {
	T       int64    `json:"t"`
	K       string   `json:"k"`
	Seq     uint64   `json:"seq"`
	Port    int      `json:"port,omitempty"`
	ID      string   `json:"id,omitempty"`
	V       any      `json:"v,omitempty"`
	Strip   []string `json:"strip,omitempty"`
	Dropped uint64   `json:"dropped,omitempty"`
}

// Errors of Subscribe.
var (
	ErrTooMany = errors.New("too many event streams")
	ErrClosed  = errors.New("the event broker is closed")
)

// UnknownKind returns the first of kinds that is not in Kinds, or "".
func UnknownKind(kinds []string) string {
	for _, k := range kinds {
		known := false
		for _, want := range Kinds {
			if k == want {
				known = true
				break
			}
		}
		if !known {
			return k
		}
	}
	return ""
}

// Broker fans events out to subscribers. All methods are safe for concurrent use.
type Broker struct {
	buf int

	mu     sync.Mutex
	seq    uint64
	subs   map[*Sub]struct{}
	closed bool
}

// NewBroker returns an empty broker whose subscribers buffer SubscriberBuffer events.
func NewBroker() *Broker { return newBroker(SubscriberBuffer) }

func newBroker(buf int) *Broker {
	return &Broker{buf: buf, subs: map[*Sub]struct{}{}}
}

// Sub is one subscription. Read events from C; call Close when done.
type Sub struct {
	b     *Broker
	ch    chan Event
	kinds map[string]bool // nil = every kind
	start uint64

	// Guarded by b.mu.
	dropped     uint64 // events lost since the last delivered one
	lastDropSeq uint64
	lastDropT   int64
	closed      bool
}

// Publish stamps ev with the next seq and offers it to every subscriber whose filter accepts it.
// It never blocks: a subscriber whose queue is full loses the event and is told with a lagged
// event before its next delivered one. After Close it does nothing.
func (b *Broker) Publish(ev Event) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return b.seq
	}
	b.seq++
	ev.Seq = b.seq
	for s := range b.subs {
		s.offer(ev)
	}
	return ev.Seq
}

// offer queues ev for s. The broker's lock is held.
func (s *Sub) offer(ev Event) {
	if s.kinds != nil && !s.kinds[ev.K] {
		return
	}
	need := 1
	if s.dropped > 0 {
		need = 2 // the lagged marker and the event
	}
	if cap(s.ch)-len(s.ch) < need {
		s.dropped++
		s.lastDropSeq, s.lastDropT = ev.Seq, ev.T
		return
	}
	if s.dropped > 0 {
		s.ch <- Event{T: s.lastDropT, K: "lagged", Seq: s.lastDropSeq, Dropped: s.dropped}
		s.dropped = 0
	}
	s.ch <- ev
}

// Seq is the seq of the last published event (0 before the first).
func (b *Broker) Seq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Subscribe registers a subscriber for kinds (empty = all). It fails with ErrTooMany at
// MaxSubscribers, ErrClosed after Close and with an error for a kind that is not in Kinds.
func (b *Broker) Subscribe(kinds []string) (*Sub, error) {
	if k := UnknownKind(kinds); k != "" {
		return nil, errors.New("unknown event kind " + k)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	if len(b.subs) >= MaxSubscribers {
		return nil, ErrTooMany
	}
	s := &Sub{b: b, ch: make(chan Event, b.buf), start: b.seq}
	if len(kinds) > 0 {
		s.kinds = make(map[string]bool, len(kinds))
		for _, k := range kinds {
			s.kinds[k] = true
		}
	}
	b.subs[s] = struct{}{}
	return s, nil
}

// Close ends every subscription (their channels close after the buffered events) and makes later
// Subscribe calls fail. Idempotent.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for s := range b.subs {
		s.closed = true
		close(s.ch)
	}
	b.subs = map[*Sub]struct{}{}
}

// C delivers the subscriber's events; it is closed by Broker.Close or Sub.Close.
func (s *Sub) C() <-chan Event { return s.ch }

// Start is the broker seq at the moment of subscription: every event with a higher seq that
// passes the filter reaches C (or is counted by a lagged event).
func (s *Sub) Start() uint64 { return s.start }

// Close ends this subscription and frees its slot. Idempotent.
func (s *Sub) Close() {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	delete(s.b.subs, s)
	close(s.ch)
}

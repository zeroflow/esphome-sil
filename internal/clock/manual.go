package clock

import (
	"context"
	"sync"
	"time"
)

// Manual is a deterministic Clock for tests. Its time starts at zero and moves
// only when Advance is called; it never reads the wall clock (MODEL-04).
// Production code uses NewReal; Manual is constructed only in _test.go files.
//
// Tickers, timers and sleepers are waiters kept in due order. Advance walks
// them one at a time, setting Now to each due time before delivering, so a
// receiver that reads Now on a tick sees exactly the tick's due time.
// Deliveries to tickers and timers are blocking sends on unbuffered channels:
// Advance returns only after every receiver has taken its value.
type Manual struct {
	advMu sync.Mutex // serialises Advance calls

	mu      sync.Mutex
	cond    *sync.Cond // signalled when a waiter is registered
	now     time.Duration
	seq     uint64
	waiters []*waiter // sorted by (due, seq)
}

type waiter struct {
	due    time.Duration
	seq    uint64
	period time.Duration // > 0 for tickers

	c       chan time.Duration // tickers and timers
	wake    chan struct{}      // sleepers: closed when due
	stopped chan struct{}      // tickers: closed by Stop to abort a delivery
	once    sync.Once
}

// NewManual returns a Manual clock at time zero.
func NewManual() *Manual {
	m := &Manual{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// insert adds w in (due, seq) order. m.mu must be held.
func (m *Manual) insert(w *waiter) {
	m.seq++
	w.seq = m.seq
	i := len(m.waiters)
	for i > 0 && m.waiters[i-1].due > w.due {
		i--
	}
	m.waiters = append(m.waiters, nil)
	copy(m.waiters[i+1:], m.waiters[i:])
	m.waiters[i] = w
	m.cond.Broadcast()
}

// remove deletes w and reports whether it was registered.
func (m *Manual) remove(w *waiter) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.waiters {
		if x == w {
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			return true
		}
	}
	return false
}

// Now is the time advanced so far.
func (m *Manual) Now() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Deadline is the zero time.Time: a Manual clock has no wall time, and a zero
// deadline means "no deadline" to net.Conn.
func (m *Manual) Deadline(time.Duration) time.Time { return time.Time{} }

// BlockUntil returns once at least n tickers, timers or sleepers are
// registered. It lets a test wait for the code under test to arm its waiters
// before it calls Advance.
func (m *Manual) BlockUntil(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.waiters) < n {
		m.cond.Wait()
	}
}

// Advance moves time forward by d, delivering every ticker tick, timer and
// sleeper that falls due, in due order, with Now set to each due time.
func (m *Manual) Advance(d time.Duration) {
	if d < 0 {
		d = 0
	}
	m.advMu.Lock()
	defer m.advMu.Unlock()
	m.mu.Lock()
	target := m.now + d
	m.mu.Unlock()
	for {
		m.mu.Lock()
		if len(m.waiters) == 0 || m.waiters[0].due > target {
			m.now = target
			m.mu.Unlock()
			return
		}
		w := m.waiters[0]
		m.waiters = m.waiters[1:]
		due := w.due
		if due > m.now {
			m.now = due
		}
		if w.period > 0 {
			w.due += w.period
			m.insert(w) // re-arm before delivering so Stop during delivery removes it
		}
		m.mu.Unlock()

		switch {
		case w.wake != nil:
			close(w.wake)
		case w.period > 0:
			select {
			case <-w.stopped: // stopped while queued: deliver nothing
				continue
			default:
			}
			select {
			case w.c <- due:
			case <-w.stopped:
			}
		default:
			w.c <- due
		}
	}
}

// Sleep blocks until Advance passes now+d, or returns ctx's error when ctx
// ends first.
func (m *Manual) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	w := &waiter{wake: make(chan struct{})}
	m.mu.Lock()
	w.due = m.now + d
	m.insert(w)
	m.mu.Unlock()
	select {
	case <-w.wake:
		return nil
	case <-ctx.Done():
		if !m.remove(w) {
			return nil // Advance already took it: the sleep completed
		}
		return ctx.Err()
	}
}

type manualTimer struct {
	m *Manual
	w *waiter
}

// NewTimer delivers the due time once, when Advance passes now+d.
func (m *Manual) NewTimer(d time.Duration) Timer {
	w := &waiter{c: make(chan time.Duration)}
	m.mu.Lock()
	w.due = m.now + d
	m.insert(w)
	m.mu.Unlock()
	return &manualTimer{m: m, w: w}
}

func (t *manualTimer) C() <-chan time.Duration { return t.w.c }

// Stop reports true when it prevented the timer from firing, false when
// Advance already took it (its value is then delivered or being delivered, and
// the owner must receive it so Advance can return).
func (t *manualTimer) Stop() bool { return t.m.remove(t.w) }

type manualTicker struct {
	m *Manual
	w *waiter
}

// NewTicker delivers the due time every d of advanced time. It panics for
// d <= 0, like time.NewTicker.
func (m *Manual) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: non-positive interval for NewTicker")
	}
	w := &waiter{period: d, c: make(chan time.Duration), stopped: make(chan struct{})}
	m.mu.Lock()
	w.due = m.now + d
	m.insert(w)
	m.mu.Unlock()
	return &manualTicker{m: m, w: w}
}

func (t *manualTicker) C() <-chan time.Duration { return t.w.c }

// Stop unregisters the ticker and releases an Advance blocked delivering to it.
func (t *manualTicker) Stop() {
	t.w.once.Do(func() {
		close(t.w.stopped)
		t.m.remove(t.w)
	})
}

var _ Clock = (*Manual)(nil)

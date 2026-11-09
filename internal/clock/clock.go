// Package clock is the only place in the repository that reads wall time.
//
// Everything else (engine, supervisor, build, tests) takes a Clock so that
// time can later be scaled or driven manually. Infrastructure timeouts go
// through the clock as well: WithTimeout for contexts, Deadline for socket
// deadlines, Sleep for polling.
package clock

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Clock is the time source used by all other packages.
type Clock interface {
	// Now is the time elapsed on this clock since its start.
	Now() time.Duration
	// NewTicker delivers the clock's Now() every d of clock time.
	NewTicker(d time.Duration) Ticker
	// NewTimer delivers the clock's Now() once after d of clock time.
	NewTimer(d time.Duration) Timer
	// Sleep blocks for d of clock time or until ctx ends (returning ctx's error).
	Sleep(ctx context.Context, d time.Duration) error
	// Deadline is the wall-clock time d of clock time from now, for socket deadlines.
	Deadline(d time.Duration) time.Time
}

// Ticker repeatedly delivers clock time on C until stopped.
type Ticker interface {
	C() <-chan time.Duration
	Stop()
}

// Timer delivers clock time once on C.
type Timer interface {
	C() <-chan time.Duration
	// Stop reports false only when the value has been or will be delivered on C.
	Stop() bool
}

// Real is the production clock: monotonic, factor fixed 1.
type Real struct {
	start time.Time
}

// NewReal returns a clock whose Now starts at zero.
func NewReal() *Real { return &Real{start: time.Now()} }

// Now returns the monotonic time since NewReal.
func (r *Real) Now() time.Duration { return time.Since(r.start) }

// Deadline returns the wall time d from now.
func (r *Real) Deadline(d time.Duration) time.Time { return time.Now().Add(d) }

// Sleep waits for d or until ctx ends.
func (r *Real) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type realTimer struct {
	c chan time.Duration
	t *time.Timer
}

// NewTimer starts a one-shot timer with a buffered channel.
func (r *Real) NewTimer(d time.Duration) Timer {
	rt := &realTimer{c: make(chan time.Duration, 1)}
	rt.t = time.AfterFunc(d, func() { rt.c <- r.Now() })
	return rt
}

func (t *realTimer) C() <-chan time.Duration { return t.c }

// Stop reports false when the callback has started or finished, i.e. a value
// is buffered on C or about to be.
func (t *realTimer) Stop() bool { return t.t.Stop() }

type realTicker struct {
	c    chan time.Duration
	stop chan struct{}
	once sync.Once
}

// NewTicker starts a ticker; slow receivers miss ticks instead of blocking it.
func (r *Real) NewTicker(d time.Duration) Ticker {
	rt := &realTicker{c: make(chan time.Duration, 1), stop: make(chan struct{})}
	t := time.NewTicker(d)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-t.C:
				select {
				case rt.c <- r.Now():
				default:
				}
			case <-rt.stop:
				return
			}
		}
	}()
	return rt
}

func (t *realTicker) C() <-chan time.Duration { return t.c }
func (t *realTicker) Stop()                   { t.once.Do(func() { close(t.stop) }) }

// WithTimeout returns a context cancelled with cause context.DeadlineExceeded
// when c's timer for d fires. It is the only sanctioned context timeout
// outside this package. The returned CancelFunc cancels with context.Canceled.
func WithTimeout(parent context.Context, c Clock, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	t := c.NewTimer(d)
	go func() {
		select {
		case <-t.C():
			cancel(context.DeadlineExceeded)
		case <-ctx.Done():
			if !t.Stop() {
				// The value has been or will be delivered; consume it so no
				// clock delivery is left blocked.
				<-t.C()
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled) }
}

// TimedOut reports whether ctx ended through a clock timeout (as opposed to
// a cancellation).
func TimedOut(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), context.DeadlineExceeded)
}

var _ Clock = (*Real)(nil)

package clock

import (
	"context"
	"testing"
	"time"
)

// guard is the real-time budget for anything a test waits on. Tests that drive
// Manual must never hang the suite, so every wait goes through it.
const guard = 5 * time.Second

// waitFor polls cond on the real clock until it holds or the guard expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	rc := NewReal()
	deadline := rc.Now() + guard
	for !cond() {
		if rc.Now() > deadline {
			t.Fatalf("timed out waiting for %s", what)
		}
		_ = rc.Sleep(context.Background(), time.Millisecond)
	}
}

// recvDur receives one value from c or fails after the guard.
func recvDur(t *testing.T, c <-chan time.Duration, what string) time.Duration {
	t.Helper()
	tm := NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case v := <-c:
		return v
	case <-tm.C():
		t.Fatalf("timed out waiting for %s", what)
		return 0
	}
}

// waitClosed waits for ch to be closed or fails after the guard.
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	tm := NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case <-ch:
	case <-tm.C():
		t.Fatalf("timed out waiting for %s", what)
	}
}

// advanceAsync runs m.Advance(d) in a goroutine and returns a channel closed
// when it returns.
func advanceAsync(m *Manual, d time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Advance(d)
	}()
	return done
}

func TestManualNowStartsAtZeroAndMovesOnlyOnAdvance(t *testing.T) {
	m := NewManual()
	if got := m.Now(); got != 0 {
		t.Fatalf("Now() = %v, want 0", got)
	}
	_ = NewReal().Sleep(context.Background(), 20*time.Millisecond)
	if got := m.Now(); got != 0 {
		t.Fatalf("Now() moved without Advance: %v", got)
	}
	m.Advance(1500 * time.Millisecond)
	if got := m.Now(); got != 1500*time.Millisecond {
		t.Fatalf("Now() = %v, want 1.5s", got)
	}
}

func TestManualTickerFiresExactly100TimesInOrder(t *testing.T) {
	m := NewManual()
	tk := m.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	done := advanceAsync(m, time.Second)
	for i := 1; i <= 100; i++ {
		got := recvDur(t, tk.C(), "tick")
		want := time.Duration(i) * 10 * time.Millisecond
		if got != want {
			t.Fatalf("tick %d = %v, want %v", i, got, want)
		}
	}
	waitClosed(t, done, "Advance(1s) to return")
	select {
	case v := <-tk.C():
		t.Fatalf("101st tick delivered: %v", v)
	default:
	}
	if got := m.Now(); got != time.Second {
		t.Fatalf("Now() = %v, want 1s", got)
	}
}

func TestManualTickerStopDuringDeliveryDoesNotBlockAdvance(t *testing.T) {
	m := NewManual()
	tk := m.NewTicker(10 * time.Millisecond)
	done := advanceAsync(m, time.Second)
	// Nobody receives: Advance is blocked delivering the first tick.
	waitFor(t, "ticker re-armed by the in-flight delivery", func() bool { return m.due(0) == 20*time.Millisecond })
	tk.Stop()
	waitClosed(t, done, "Advance to return after Stop")
	if got := m.Now(); got != time.Second {
		t.Fatalf("Now() = %v, want 1s", got)
	}
}

func TestManualTimerFiresOnceAtDueTime(t *testing.T) {
	m := NewManual()
	tm := m.NewTimer(250 * time.Millisecond)
	m.Advance(249 * time.Millisecond) // not due: would block if it fired
	done := advanceAsync(m, time.Millisecond)
	if got := recvDur(t, tm.C(), "timer"); got != 250*time.Millisecond {
		t.Fatalf("timer value = %v, want 250ms", got)
	}
	waitClosed(t, done, "Advance to return")
	m.Advance(time.Second)
	select {
	case v := <-tm.C():
		t.Fatalf("timer fired twice: %v", v)
	default:
	}
}

func TestManualTimerStopBeforeDueReturnsTrueAndPreventsFiring(t *testing.T) {
	m := NewManual()
	tm := m.NewTimer(time.Second)
	if !tm.Stop() {
		t.Fatal("Stop() before the due time = false, want true")
	}
	m.Advance(2 * time.Second) // would block forever if the timer still fired
	select {
	case v := <-tm.C():
		t.Fatalf("stopped timer fired: %v", v)
	default:
	}
	if tm.Stop() {
		t.Fatal("second Stop() = true, want false")
	}
}

func TestManualTimerStopAfterAdvanceTookItReturnsFalse(t *testing.T) {
	m := NewManual()
	tm := m.NewTimer(10 * time.Millisecond)
	done := advanceAsync(m, 20*time.Millisecond)
	waitFor(t, "Advance to take the timer", func() bool { return m.count() == 0 })
	if tm.Stop() {
		t.Fatal("Stop() while delivery is in flight = true, want false")
	}
	if got := recvDur(t, tm.C(), "in-flight timer delivery"); got != 10*time.Millisecond {
		t.Fatalf("timer value = %v, want 10ms", got)
	}
	waitClosed(t, done, "Advance to return")
}

func TestManualDeliveryOrderFollowsDueTime(t *testing.T) {
	m := NewManual()
	tk := m.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	late := m.NewTimer(25 * time.Millisecond)
	early := m.NewTimer(5 * time.Millisecond)
	done := advanceAsync(m, 30*time.Millisecond)
	type ev struct {
		who string
		at  time.Duration
	}
	var got []ev
	for len(got) < 5 {
		tm := NewReal().NewTimer(guard)
		select {
		case v := <-tk.C():
			got = append(got, ev{"tick", v})
		case v := <-early.C():
			got = append(got, ev{"early", v})
		case v := <-late.C():
			got = append(got, ev{"late", v})
		case <-tm.C():
			t.Fatalf("timed out; got %v", got)
		}
		tm.Stop()
	}
	waitClosed(t, done, "Advance to return")
	want := []ev{{"early", 5 * time.Millisecond}, {"tick", 10 * time.Millisecond}, {"tick", 20 * time.Millisecond}, {"late", 25 * time.Millisecond}, {"tick", 30 * time.Millisecond}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestManualSleepReturnsAfterAdvancePassesDeadline(t *testing.T) {
	m := NewManual()
	res := make(chan error, 1)
	go func() { res <- m.Sleep(context.Background(), 100*time.Millisecond) }()
	m.BlockUntil(1)
	m.Advance(99 * time.Millisecond)
	select {
	case err := <-res:
		t.Fatalf("Sleep returned early: %v", err)
	default:
	}
	m.Advance(time.Millisecond)
	tm := NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Sleep = %v, want nil", err)
		}
	case <-tm.C():
		t.Fatal("Sleep did not return after the deadline")
	}
}

func TestManualSleepReturnsCtxErrWhenCancelledFirst(t *testing.T) {
	m := NewManual()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- m.Sleep(ctx, time.Hour) }()
	m.BlockUntil(1)
	cancel()
	tm := NewReal().NewTimer(guard)
	defer tm.Stop()
	select {
	case err := <-res:
		if err != context.Canceled {
			t.Fatalf("Sleep = %v, want context.Canceled", err)
		}
	case <-tm.C():
		t.Fatal("Sleep did not return after cancel")
	}
	if n := m.count(); n != 0 {
		t.Fatalf("cancelled sleeper left %d waiters registered", n)
	}
	// A pre-cancelled context returns without registering.
	if err := m.Sleep(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("Sleep on a cancelled ctx = %v, want context.Canceled", err)
	}
	if n := m.count(); n != 0 {
		t.Fatalf("pre-cancelled Sleep registered %d waiters", n)
	}
}

func TestManualBlockUntilCountsTickersTimersAndSleepers(t *testing.T) {
	m := NewManual()
	reached := make(chan struct{})
	go func() {
		m.BlockUntil(3)
		close(reached)
	}()
	tk := m.NewTicker(time.Second)
	defer tk.Stop()
	tm := m.NewTimer(time.Second)
	defer tm.Stop()
	select {
	case <-reached:
		t.Fatal("BlockUntil(3) returned with 2 waiters")
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = m.Sleep(ctx, time.Second) }()
	waitClosed(t, reached, "BlockUntil(3)")
	m.BlockUntil(0) // returns immediately
}

func TestManualDeadlineIsTheZeroTime(t *testing.T) {
	m := NewManual()
	m.Advance(time.Hour)
	if d := m.Deadline(time.Second); !d.IsZero() {
		t.Fatalf("Deadline = %v, want the zero time.Time", d)
	}
}

func TestManualWithTimeoutIsDrivenByTheInjectedClock(t *testing.T) {
	m := NewManual()
	ctx, cancel := WithTimeout(context.Background(), m, 2*time.Second)
	defer cancel()
	m.Advance(1999 * time.Millisecond)
	select {
	case <-ctx.Done():
		t.Fatal("WithTimeout context done after 1999ms of a 2s timeout")
	default:
	}
	if TimedOut(ctx) {
		t.Fatal("TimedOut true before the deadline")
	}
	m.Advance(time.Millisecond)
	waitClosed(t, ctx.Done(), "WithTimeout context to end")
	if !TimedOut(ctx) {
		t.Fatalf("TimedOut false after the deadline; cause = %v", context.Cause(ctx))
	}
}

func TestManualWithTimeoutCancelBeforeDueLeavesTimedOutFalse(t *testing.T) {
	m := NewManual()
	ctx, cancel := WithTimeout(context.Background(), m, 2*time.Second)
	m.Advance(time.Second)
	cancel()
	waitClosed(t, ctx.Done(), "cancelled context to end")
	if TimedOut(ctx) {
		t.Fatal("TimedOut true after CancelFunc")
	}
	done := advanceAsync(m, 5*time.Second)
	waitClosed(t, done, "Advance(5s) after cancel (a clock delivery is left blocked)")
	if TimedOut(ctx) {
		t.Fatal("TimedOut turned true after the later Advance")
	}
}

func TestManualWithTimeoutParentCancelledBeforeDueLeavesTimedOutFalse(t *testing.T) {
	m := NewManual()
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := WithTimeout(parent, m, 2*time.Second)
	defer cancel()
	m.Advance(time.Second)
	cancelParent()
	waitClosed(t, ctx.Done(), "child context to end")
	if TimedOut(ctx) {
		t.Fatal("TimedOut true after parent cancel")
	}
	done := advanceAsync(m, 5*time.Second)
	waitClosed(t, done, "Advance(5s) after parent cancel (a clock delivery is left blocked)")
	if TimedOut(ctx) {
		t.Fatal("TimedOut turned true after the later Advance")
	}
}

// count is the number of registered waiters.
func (m *Manual) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.waiters)
}

// due is the due time of the i-th registered waiter in due order, or -1.
func (m *Manual) due(i int) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= len(m.waiters) {
		return -1
	}
	return m.waiters[i].due
}

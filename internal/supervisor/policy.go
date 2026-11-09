package supervisor

import (
	"sync"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
)

// Crash-loop policy (D-03, T-01-23): the crashLimit-th unrequested firmware
// exit within crashWindow stops the automatic restarts.
const (
	crashWindow = 30 * time.Second
	crashLimit  = 5
)

// Exit kinds of control.Exit.Kind.
const (
	KindRequested = "requested"
	KindRebooted  = "rebooted"
	KindCrashed   = "crashed"
)

// Classify names how a firmware exit ended: requested (the supervisor asked
// for it), rebooted (exit code 0, as ESP.restart() does on host) or crashed
// (a non-zero code or a signal).
func Classify(e ExitInfo) string {
	switch {
	case e.Requested:
		return KindRequested
	case e.Code != nil && *e.Code == 0 && e.Signal == "":
		return KindRebooted
	default:
		return KindCrashed
	}
}

// CrashGuard counts unrequested firmware exits on an injected clock.
type CrashGuard struct {
	clk clock.Clock

	mu    sync.Mutex
	times []time.Duration
}

// NewCrashGuard returns a guard with a 30 s window and a limit of 5.
func NewCrashGuard(clk clock.Clock) *CrashGuard {
	return &CrashGuard{clk: clk}
}

// Record notes one unrequested exit now and reports whether it was the
// crashLimit-th within the last crashWindow (an exit exactly crashWindow ago
// no longer counts). Callers never record requested exits.
func (g *CrashGuard) Record() (tripped bool) {
	now := g.clk.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	kept := g.times[:0]
	for _, t := range g.times {
		if now-t < crashWindow {
			kept = append(kept, t)
		}
	}
	g.times = append(kept, now)
	return len(g.times) >= crashLimit
}

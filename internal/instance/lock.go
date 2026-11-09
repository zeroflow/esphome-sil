package instance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
)

// allocLockBudget is how long LockAlloc waits for another allocation.
const allocLockBudget = 10 * time.Second

// LockInstance takes the per-instance lock (flock on <StateDir>/.lock) that
// `sil up` holds from its first step to its return, so two concurrent ups of
// the same instance cannot race on its run dir, identity or ports. A held lock
// is Failed "instance_busy". The kernel drops the lock when the process
// exits.
func LockInstance(p Paths) (release func(), err error) {
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return nil, apperr.Infra("lock_failed", "cannot create the instance state directory", map[string]any{"path": p.StateDir, "error": err.Error()})
	}
	f, err := openLock(filepath.Join(p.StateDir, ".lock"))
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, apperr.Failed("instance_busy",
				"another `sil up` of instance "+p.Name+" is in progress; wait for it to finish",
				map[string]any{"instance": p.Name})
		}
		return nil, apperr.Infra("lock_failed", "cannot lock the instance", map[string]any{"error": err.Error()})
	}
	return unlocker(f), nil
}

// LockAlloc takes the global allocation lock (flock on
// <stateRoot>/sil/.alloc.lock) that serialises identity creation and port
// allocation across instances. It waits up to 10 s (clock-based) for a
// concurrent allocation.
func LockAlloc(stateRoot string) (release func(), err error) {
	dir := filepath.Join(stateRoot, "sil")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, apperr.Infra("lock_failed", "cannot create the state directory", map[string]any{"path": dir, "error": err.Error()})
	}
	f, err := openLock(filepath.Join(dir, ".alloc.lock"))
	if err != nil {
		return nil, err
	}
	clk := clock.NewReal()
	ctx, cancel := clock.WithTimeout(context.Background(), clk, allocLockBudget)
	defer cancel()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return unlocker(f), nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, apperr.Infra("lock_failed", "cannot take the allocation lock", map[string]any{"error": err.Error()})
		}
		if serr := clk.Sleep(ctx, 20*time.Millisecond); serr != nil {
			_ = f.Close()
			return nil, apperr.Infra("lock_timeout",
				fmt.Sprintf("another sil process held the allocation lock for %s", allocLockBudget),
				map[string]any{"path": f.Name()})
		}
	}
}

func openLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, apperr.Infra("lock_failed", "cannot open a lock file", map[string]any{"path": path, "error": err.Error()})
	}
	return f, nil
}

func unlocker(f *os.File) func() {
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

package supervisor

import (
	"context"
	"fmt"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// errTooManyStreams is what the 17th stream (watch and logs --follow together) answers.
func errTooManyStreams() error {
	return apperr.Infra("too_many_streams",
		fmt.Sprintf("this instance already serves %d streams (watch and logs --follow together); close one first", events.MaxSubscribers),
		map[string]any{"limit": events.MaxSubscribers})
}

// acquireStream takes one of the instance's stream slots; the returned func gives it back.
func (s *supervisor) acquireStream() (release func(), err error) {
	if s.isStopping() {
		return nil, errStopping()
	}
	for {
		n := s.streams.Load()
		if n >= events.MaxSubscribers {
			return nil, errTooManyStreams()
		}
		if s.streams.CompareAndSwap(n, n+1) {
			return func() { s.streams.Add(-1) }, nil
		}
	}
}

// joinSnapshot takes the state a watch starts from, after sub exists, together with the seq it is
// consistent with. The snapshot is taken on the engine goroutine between two events and stamped
// with the broker's seq, so every event with a lower or equal seq is already in it and every
// event with a higher seq reaches sub: nothing is lost or repeated at the join. An engine that
// has stopped (the gap of a simulator restart) emits nothing any more, so its last snapshot goes
// with the seq at the moment of subscription.
func (s *supervisor) joinSnapshot(ctx context.Context, sub *events.Sub) (engine.Snapshot, uint64) {
	eng := s.currentEngine()
	snap, err := eng.SnapshotSync(ctx)
	if err != nil {
		return eng.Snapshot(), sub.Start()
	}
	return snap, snap.Seq
}

// Watch implements control.Control (CLI-03, D-12): the snapshot line, then every change event of
// the selected kinds in seq order, then the stopped line when the instance stops. A subscriber
// that cannot keep up loses events (and is told with a lagged line); it never slows the engine.
func (s *supervisor) Watch(ctx context.Context, req control.WatchRequest, send func(v any) error) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s.broker == nil {
		return apperr.Infra("internal", "this instance has no event broker", nil)
	}
	release, err := s.acquireStream()
	if err != nil {
		return err
	}
	defer release()
	sub, err := s.broker.Subscribe(req.Kinds)
	switch {
	case err == events.ErrTooMany:
		return errTooManyStreams()
	case err == events.ErrClosed:
		return errStopping()
	case err != nil:
		return apperr.Failed("invalid_argument", err.Error(), nil)
	}
	defer sub.Close()

	snap, seq := s.joinSnapshot(ctx, sub)
	st := s.statusFrom(&snap)
	if err := send(control.SnapshotLine{T: snap.SimTime.Microseconds(), K: "snapshot", Seq: seq, Status: st}); err != nil {
		return err
	}
	for {
		select {
		case ev, ok := <-sub.C():
			if !ok {
				return send(control.NewStoppedLine(s.clk.Now()))
			}
			if ev.K != "lagged" && ev.Seq <= seq {
				continue // already in the snapshot
			}
			if err := send(ev); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

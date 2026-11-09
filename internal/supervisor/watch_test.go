package supervisor

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/engine"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// watchSup is a supervisor with a running engine publishing into a broker, and no firmware.
func watchSup(t *testing.T) *supervisor {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sil-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	fan, err := fanlib.Load(fanlib.DefaultFan)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual()
	b := events.NewBroker()
	s := &supervisor{
		clk:     clk,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		broker:  b,
		engCfg:  engine.Config{Clock: clk, BusPath: filepath.Join(dir, "bus.sock"), Emit: b.Publish, Seq: b.Seq},
		engDied: make(chan error, 1),
		downReq: make(chan struct{}),
		stopped: make(chan struct{}),
		fans:    []engine.PortConfig{{Port: 1, FanID: fanlib.DefaultFan, Fan: fan}},
	}
	sl, err := s.startEngine()
	if err != nil {
		t.Fatal(err)
	}
	s.slot = sl
	t.Cleanup(func() { s.stopEngine(sl) })
	return s
}

// watching runs Watch in the background and hands its lines over, decoded.
type watching struct {
	lines chan map[string]any
	done  chan error
	stop  context.CancelFunc
}

func startWatch(t *testing.T, s *supervisor, kinds ...string) *watching {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &watching{lines: make(chan map[string]any, 4096), done: make(chan error, 1), stop: cancel}
	go func() {
		w.done <- s.Watch(ctx, control.WatchRequest{Kinds: kinds}, func(v any) error {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			w.lines <- m
			return nil
		})
	}()
	t.Cleanup(cancel)
	return w
}

func (w *watching) next(t *testing.T) map[string]any {
	t.Helper()
	tm := realClock.NewTimer(5 * time.Second)
	defer tm.Stop()
	select {
	case m := <-w.lines:
		return m
	case err := <-w.done:
		t.Fatalf("Watch returned %v while a line was expected", err)
	case <-tm.C():
		t.Fatal("no line within 5 s")
	}
	return nil
}

func setTemp(t *testing.T, s *supervisor, v float64) {
	t.Helper()
	raw, _ := json.Marshal(v)
	if _, err := s.Set(context.Background(), control.SetRequest{Values: map[string]json.RawMessage{"temperature": raw}}); err != nil {
		t.Fatalf("set temperature=%v: %v", v, err)
	}
}

func TestWatchSnapshotThenEventsThenStopped(t *testing.T) {
	s := watchSup(t)
	setTemp(t, s, 30)
	w := startWatch(t, s)
	snap := w.next(t)
	if snap["k"] != "snapshot" || snap["instance"] != nil && snap["schema"] != float64(1) {
		t.Fatalf("first line %v, want the snapshot", snap)
	}
	env, _ := snap["env"].(map[string]any)
	if env["temperature"] != float64(30) {
		t.Fatalf("snapshot env %v, want temperature 30 (set before the watch)", env)
	}
	if _, ok := snap["seq"].(float64); !ok {
		t.Fatalf("snapshot has no integer seq: %v", snap)
	}
	setTemp(t, s, 31)
	ev := w.next(t)
	if ev["k"] != "env" || ev["id"] != "temperature" || ev["v"] != float64(31) || ev["seq"].(float64) <= snap["seq"].(float64) {
		t.Fatalf("event %v, want env temperature 31 with a seq above the snapshot's", ev)
	}
	s.shutdown()
	stopped := w.next(t)
	if stopped["k"] != "stopped" {
		t.Fatalf("last line %v, want stopped", stopped)
	}
	if err := <-w.done; err != nil {
		t.Fatalf("Watch returned %v after the stop, want nil", err)
	}
}

func TestWatchKindFilterAndValidation(t *testing.T) {
	s := watchSup(t)
	w := startWatch(t, s, "env")
	w.next(t) // snapshot
	if err := s.Watch(context.Background(), control.WatchRequest{Kinds: []string{"nope"}}, func(any) error { return nil }); errCode(err) != "invalid_argument" {
		t.Fatalf("unknown kind: %v, want invalid_argument", err)
	}
	if _, err := s.Set(context.Background(), control.SetRequest{Values: map[string]json.RawMessage{
		"port1.fan": json.RawMessage(`"noctua-nf-a12x25-pwm"`), "temperature": json.RawMessage(`27`),
	}}); err != nil {
		t.Fatal(err)
	}
	ev := w.next(t)
	if ev["k"] != "env" || ev["v"] != float64(27) {
		t.Fatalf("event %v, want the env event only (fan and state events are filtered)", ev)
	}
	select {
	case extra := <-w.lines:
		t.Fatalf("extra line %v with --kind env", extra)
	default:
	}
}

func TestWatchTooManyStreams(t *testing.T) {
	s := watchSup(t)
	var ws []*watching
	for i := 0; i < events.MaxSubscribers; i++ {
		w := startWatch(t, s)
		w.next(t)
		ws = append(ws, w)
	}
	err := s.Watch(context.Background(), control.WatchRequest{}, func(any) error { return nil })
	if errCode(err) != "too_many_streams" {
		t.Fatalf("the 17th stream: %v, want too_many_streams", err)
	}
	ws[0].stop()
	<-ws[0].done
	// The slot is free again.
	w := startWatch(t, s)
	if got := w.next(t); got["k"] != "snapshot" {
		t.Fatalf("stream after a slot was freed: %v", got)
	}
}

// Watches that join while the temperature changes continuously never lose or repeat a change at
// the join: the snapshot's temperature is followed by exactly the next values, one by one.
func TestWatchJoinIsLossless(t *testing.T) {
	s := watchSup(t)
	tempOf := func(n int) float64 { return float64(n) / 1000 }
	setTemp(t, s, tempOf(0))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 1; n < 120000; n++ {
			select {
			case <-stop:
				return
			default:
			}
			raw, _ := json.Marshal(tempOf(n))
			if _, err := s.Set(context.Background(), control.SetRequest{Values: map[string]json.RawMessage{"temperature": raw}}); err != nil {
				t.Errorf("set temperature=%v: %v", tempOf(n), err)
				return
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
	}()
	for round := 0; round < 25; round++ {
		w := startWatch(t, s, "env")
		snap := w.next(t)
		n0 := int(math.Round(snap["env"].(map[string]any)["temperature"].(float64) * 1000))
		for i := 1; i <= 5; i++ {
			ev := w.next(t)
			if ev["k"] != "env" || ev["v"] != tempOf(n0+i) {
				t.Fatalf("round %d: after the snapshot's temperature %v event %d is %v, want %v (a change was lost or repeated)",
					round, tempOf(n0), i, ev["v"], tempOf(n0+i))
			}
		}
		w.stop()
		_ = realClock.Sleep(context.Background(), time.Millisecond)
	}
}

// During the gap of a simulator restart the old engine is stopped: a watch still starts, from its
// final snapshot, and sees the new engine's events.
func TestWatchJoinsDuringTheEngineGap(t *testing.T) {
	s := watchSup(t)
	old := s.slot
	s.stopEngine(old) // the gap
	w := startWatch(t, s)
	snap := w.next(t)
	if snap["k"] != "snapshot" {
		t.Fatalf("first line %v", snap)
	}
	s.inMu.Lock()
	sl, err := s.startEngine()
	s.inMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.slot = sl
	s.mu.Unlock()
	t.Cleanup(func() { s.stopEngine(sl) })
	setTemp(t, s, 33)
	for {
		ev := w.next(t)
		if ev["k"] == "env" {
			if ev["v"] != float64(33) {
				t.Fatalf("env event %v, want 33", ev)
			}
			return
		}
		if ev["k"] == "stopped" {
			t.Fatalf("stream ended: %v", ev)
		}
		// the new engine's start states pass by
	}
}

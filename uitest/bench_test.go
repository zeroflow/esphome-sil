//go:build sil_ui

package uitest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// call is one recorded call on the bench: the operation, its arguments as text and when it came
// (sysClock time).
type call struct {
	Op   string
	Args string
	At   time.Duration
}

// bench is a fake instance behind the real web handler. It embeds control.Control, so a method
// the tests do not need yet panics if the page calls it; later plans add the methods they use.
// Status, Watch and the helpers below are all the page needs to show readouts.
type bench struct {
	control.Control

	mu     sync.Mutex
	st     control.Status
	broker *events.Broker
	calls  []call
	fail   map[string]error
	drop   chan struct{} // closed by DropStreams; replaced by a fresh one
	hold   chan struct{} // non-nil while the snapshot is held

	inject map[chan events.Event]struct{} // the running Watch calls, for Inject

	btnHold chan struct{} // non-nil while SetButton calls wait before they apply

	histHold chan struct{}                                      // non-nil while History answers are held
	histFn   func(control.HistoryRequest) control.HistoryResult // replaces the broker's history when set
}

// benchKey is a fixed 44-character base64 string standing in for the API key.
const benchKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func newBench(t *testing.T) *bench {
	t.Helper()
	b := &bench{
		broker: events.NewBroker(),
		fail:   map[string]error{},
		drop:   make(chan struct{}),
	}
	t.Cleanup(b.broker.Close)
	b.st = control.Status{
		Schema:   control.SchemaVersion,
		Instance: "ui-bench",
		Phase:    control.PhaseReady,
		API: control.APIStatus{
			DeviceName: "ui-bench", MAC: "98:35:69:ab:f6:79", Host: "127.0.0.1", Port: 6054, Key: benchKey, Ready: true,
		},
		Firmware: control.FirmwareStatus{PID: 4242, Running: true, BusConnected: true},
		Sim:      control.SimStatus{TimeS: 12.3, TimeFactor: 1, BusProto: 1},
		Web:      control.WebStatus{Listen: "0.0.0.0", Port: 8054, URL: "http://127.0.0.1:8054/"},
		Env:      control.EnvStatus{Temperature: 22, Humidity: 45},
		Buttons:  map[string]bool{"usr1": false, "usr2": false, "usr3": false},
		LEDs: control.LEDStatus{
			Strip: []string{"#000000", "#000000", "#000000", "#000000", "#000000"},
			Segments: map[string][]int{
				"board": {0}, "fan1": {1}, "fan2": {2}, "fan3": {3}, "fan4": {4},
			},
		},
		Warnings: []string{},
	}
	for i := 1; i <= 4; i++ {
		b.st.Ports = append(b.st.Ports, control.PortStatus{
			Port: i, Fan: "arctic-p12-pwm-pst", Duty: 0.4, RPM: float64(1000 + 100*i), State: "running",
		})
	}
	return b
}

func (b *bench) record(op string, args ...any) {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = fmt.Sprint(a)
	}
	b.calls = append(b.calls, call{Op: op, Args: strings.Join(parts, " "), At: sysClock.Now()})
}

// enter records a call and returns the error armed for op by FailNext (once).
func (b *bench) enter(op string, args ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record(op, args...)
	if err := b.fail[strings.ToLower(op)]; err != nil {
		delete(b.fail, strings.ToLower(op))
		return err
	}
	return nil
}

// clone copies a Status so a caller never shares maps and slices with the bench.
func clone(st control.Status) control.Status {
	raw, err := json.Marshal(st)
	if err != nil {
		panic(err)
	}
	var out control.Status
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

// Status implements control.Control.
func (b *bench) Status(ctx context.Context) (control.Status, error) {
	if err := b.enter("Status"); err != nil {
		return control.Status{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return clone(b.st), nil
}

// Watch mirrors supervisor.Watch: subscribe first, then take the status and the broker's seq under
// one lock, send the snapshot, forward the events above that seq, send the stopped line when the
// subscription closes. After DropStreams it returns an error without a stopped line.
func (b *bench) Watch(ctx context.Context, req control.WatchRequest, send func(v any) error) error {
	if err := b.enter("Watch", strings.Join(req.Kinds, ",")); err != nil {
		return err
	}
	b.mu.Lock()
	drop, hold := b.drop, b.hold
	b.mu.Unlock()
	sub, err := b.broker.Subscribe(req.Kinds)
	switch {
	case err == events.ErrClosed:
		return apperr.Failed("stopping", "the instance is shutting down", nil)
	case err != nil:
		return apperr.Failed("invalid_argument", err.Error(), nil)
	}
	defer sub.Close()
	injected := make(chan events.Event, 16)
	b.mu.Lock()
	if b.inject == nil {
		b.inject = map[chan events.Event]struct{}{}
	}
	b.inject[injected] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.inject, injected)
		b.mu.Unlock()
	}()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	st := clone(b.st)
	seq := b.broker.Seq()
	b.mu.Unlock()
	if err := send(control.SnapshotLine{T: int64(st.Sim.TimeS * 1e6), K: "snapshot", Seq: seq, Status: st}); err != nil {
		return err
	}
	for {
		select {
		case ev, ok := <-sub.C():
			if !ok {
				return send(control.NewStoppedLine(sysClock.Now()))
			}
			if ev.K != "lagged" && ev.Seq <= seq {
				continue
			}
			if err := send(ev); err != nil {
				return err
			}
		case ev := <-injected:
			if err := send(ev); err != nil {
				return err
			}
		case <-drop:
			return apperr.Infra("control_unreachable", "the stream was dropped", nil)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Emit applies mutate to the status and publishes ev under one lock, so a snapshot taken by Watch
// contains exactly the events with a lower or equal seq. A zero ev.T becomes the sim time.
func (b *bench) Emit(ev events.Event, mutate func(*control.Status)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if mutate != nil {
		mutate(&b.st)
	}
	if ev.T == 0 {
		ev.T = int64(b.st.Sim.TimeS * 1e6)
	}
	b.broker.Publish(ev)
}

// Inject sends ev to every running Watch as it is, past the kind filter of its subscription: the
// lagged marker of a real instance is made by the subscription itself, so it reaches a stream
// whatever kinds it asked for, which Publish cannot imitate.
func (b *bench) Inject(ev events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.inject {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Mutate changes the status without publishing an event: what a page sees only in its next snapshot.
func (b *bench) Mutate(mutate func(*control.Status)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mutate(&b.st)
}

// DropStreams makes every running Watch return an error without a stopped line (a lost
// connection); streams opened later work normally.
func (b *bench) DropStreams() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record("DropStreams")
	close(b.drop)
	b.drop = make(chan struct{})
}

// Stop ends the instance: every stream gets the stopped line and later Watch calls fail.
func (b *bench) Stop() {
	b.mu.Lock()
	b.record("Stop")
	b.mu.Unlock()
	b.broker.Close()
}

// HoldSnapshot makes Watch calls wait before they send the snapshot, until ReleaseSnapshot.
func (b *bench) HoldSnapshot() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hold == nil {
		b.hold = make(chan struct{})
	}
}

// ReleaseSnapshot lets the held Watch calls (and later ones) send their snapshot.
func (b *bench) ReleaseSnapshot() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hold != nil {
		close(b.hold)
		b.hold = nil
	}
}

// History implements control.Control: the sample events of the broker within the window, as the
// instance serves them. The broker's answer is computed when the request arrives and sent when
// ReleaseHistory lets it go (at once when it is not held), so a held answer is stale by the samples
// that arrive meanwhile, like a slow response on a real instance. When SetHistory gave a function,
// the answer is that function's result, computed when the answer is sent.
func (b *bench) History(ctx context.Context, r control.HistoryRequest) (control.HistoryResult, error) {
	if err := b.enter("History", r.WindowS); err != nil {
		return control.HistoryResult{}, err
	}
	b.mu.Lock()
	hold, fn := b.histHold, b.histFn
	b.mu.Unlock()
	seq, samples := b.broker.History(time.Duration(r.WindowS * float64(time.Second)))
	res := control.HistoryResult{Seq: seq, WindowS: r.WindowS, Samples: samples}
	if n := len(samples); n > 0 {
		res.T = samples[n-1].T
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return control.HistoryResult{}, ctx.Err()
		}
	}
	if fn != nil {
		return fn(r), nil
	}
	return res, nil
}

// HoldHistory makes History answers wait until ReleaseHistory.
func (b *bench) HoldHistory() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.histHold == nil {
		b.histHold = make(chan struct{})
	}
}

// ReleaseHistory lets the held History answers (and later ones) go.
func (b *bench) ReleaseHistory() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.histHold != nil {
		close(b.histHold)
		b.histHold = nil
	}
}

// SetHistory makes History answer with fn(request), computed when the answer is sent, instead of the
// broker's samples.
func (b *bench) SetHistory(fn func(control.HistoryRequest) control.HistoryResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.histFn = fn
}

// Calls returns the recorded calls in order.
func (b *bench) Calls() []call {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]call(nil), b.calls...)
}

// callsOf returns the recorded calls of one operation.
func (b *bench) callsOf(op string) []call {
	var out []call
	for _, c := range b.Calls() {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// FailNext makes the next call of op (any letter case: "set", "Set") return err.
func (b *bench) FailNext(op string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fail[strings.ToLower(op)] = err
}

// waitCalls waits until op has been called n times (at least) and returns those calls.
func (b *bench) waitCalls(t *testing.T, op string, n int, d time.Duration) []call {
	t.Helper()
	ctx := within(t, d)
	for {
		if got := b.callsOf(op); len(got) >= n {
			return got
		}
		if err := sysClock.Sleep(ctx, 20*time.Millisecond); err != nil {
			t.Fatalf("waiting for %d %s calls, got %d: %v", n, op, len(b.callsOf(op)), err)
		}
	}
}

// Fans implements control.Control: every fan of the shipped library, sorted by id.
func (b *bench) Fans(ctx context.Context) (control.FansResult, error) {
	if err := b.enter("Fans"); err != nil {
		return control.FansResult{}, err
	}
	ids, err := fanlib.IDs()
	if err != nil {
		return control.FansResult{}, apperr.Infra("internal", err.Error(), nil)
	}
	res := control.FansResult{Fans: make([]control.FanInfo, 0, len(ids))}
	for _, id := range ids {
		p, err := fanlib.Load(id)
		if err != nil {
			return control.FansResult{}, apperr.Infra("internal", err.Error(), nil)
		}
		res.Fans = append(res.Fans, control.FanInfo{ID: id, Name: p.Name, MinRPM: p.MinRPM, MaxRPM: p.MaxRPM, BelowMin: p.BelowMin})
	}
	return res, nil
}

// Set implements control.Control the way the page needs it: the call is recorded with its values as
// compact JSON (keys sorted), FailNext("set") answers with the armed error, and otherwise every pair
// is validated first (a known fan id for portN.fan, a number or numeric string for temperature and
// humidity; no ranges, so the tests choose the refusals) and then applied as the events `fan` and
// `env`, which also change the status.
func (b *bench) Set(ctx context.Context, r control.SetRequest) (control.SetResult, error) {
	raw, _ := json.Marshal(r.Values)
	if err := b.enter("Set", string(raw)); err != nil {
		return control.SetResult{}, err
	}
	ids, err := fanlib.IDs()
	if err != nil {
		return control.SetResult{}, apperr.Infra("internal", err.Error(), nil)
	}
	keys := make([]string, 0, len(r.Values))
	for k := range r.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type change struct {
		ev     events.Event
		mutate func(*control.Status)
	}
	var changes []change
	applied := map[string]any{}
	for _, k := range keys {
		text := strings.TrimSpace(string(r.Values[k]))
		var port int
		if _, err := fmt.Sscanf(k, "port%d.fan", &port); err == nil && port >= 1 && port <= len(b.st.Ports) {
			var id string
			if json.Unmarshal([]byte(text), &id) != nil || !contains(ids, id) {
				return control.SetResult{}, apperr.Failed("unknown_fan", fmt.Sprintf("unknown fan %s for %s", text, k),
					map[string]any{"key": k, "fan": text})
			}
			changes = append(changes, change{events.Event{K: "fan", Port: port, V: id}, func(st *control.Status) { st.Ports[port-1].Fan = id }})
			applied[k] = id
			continue
		}
		if k != "temperature" && k != "humidity" {
			return control.SetResult{}, apperr.Failed("unknown_key", fmt.Sprintf("unknown key %q", k), map[string]any{"key": k})
		}
		var v float64
		var s string
		if json.Unmarshal([]byte(text), &s) == nil {
			text = s
		}
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return control.SetResult{}, apperr.Failed("invalid_argument", fmt.Sprintf("%s needs a finite number, got %s", k, text),
				map[string]any{"key": k, "value": text})
		}
		key := k
		changes = append(changes, change{events.Event{K: "env", ID: key, V: v}, func(st *control.Status) {
			if key == "temperature" {
				st.Env.Temperature = v
			} else {
				st.Env.Humidity = v
			}
		}})
		applied[k] = v
	}
	for _, c := range changes {
		b.Emit(c.ev, c.mutate)
	}
	return control.SetResult{Applied: applied}, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// SetButton implements control.Control the way the page needs it: the call is recorded as
// button.down (args: id and the max hold in ms, 0 when none) or button.up (args: id), FailNext of
// those names answers with the armed error, and otherwise the engine's strict rules apply (down on a
// held button is button_busy, up on a released one button_not_held) before the button event is
// published and the status changed.
func (b *bench) SetButton(ctx context.Context, id string, down bool, r control.DownRequest) (control.PressResult, error) {
	op := "button.up"
	var maxHold int64
	args := []any{id}
	if down {
		op = "button.down"
		if r.MaxHoldMS != nil {
			maxHold = *r.MaxHoldMS
		}
		args = append(args, maxHold)
	}
	if err := b.enter(op, args...); err != nil {
		return control.PressResult{}, err
	}
	b.mu.Lock()
	hold := b.btnHold
	b.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return control.PressResult{}, ctx.Err()
		}
	}
	b.mu.Lock()
	held, known := b.st.Buttons[id]
	b.mu.Unlock()
	switch {
	case !known:
		return control.PressResult{}, apperr.Failed("input_not_available", fmt.Sprintf("this bench has no button %s", id), map[string]any{"button": id})
	case down && held:
		return control.PressResult{}, apperr.Failed("button_busy", fmt.Sprintf("%s is already held", id), map[string]any{"button": id})
	case !down && !held:
		return control.PressResult{}, apperr.Failed("button_not_held", fmt.Sprintf("%s is not held", id), map[string]any{"button": id})
	}
	b.Emit(events.Event{K: "button", ID: id, V: down}, func(st *control.Status) { st.Buttons[id] = down })
	action := "up"
	if down {
		action = "down"
	}
	return control.PressResult{Button: id, Action: action, Level: down, MaxHoldMS: maxHold}, nil
}

// AutoRelease is the engine's max-hold release of button id: a button event v false with reason
// max_hold, and the status changed.
func (b *bench) AutoRelease(id string) {
	b.Emit(events.Event{K: "button", ID: id, V: false, Reason: "max_hold"}, func(st *control.Status) { st.Buttons[id] = false })
}

// HoldButtonEvents makes SetButton calls wait (after they are recorded) until ReleaseButtonEvents, so
// a test can see what the page shows between its request and the server's report.
func (b *bench) HoldButtonEvents() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.btnHold == nil {
		b.btnHold = make(chan struct{})
	}
}

// ReleaseButtonEvents lets the waiting SetButton calls (and later ones) go.
func (b *bench) ReleaseButtonEvents() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.btnHold != nil {
		close(b.btnHold)
		b.btnHold = nil
	}
}

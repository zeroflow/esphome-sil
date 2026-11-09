package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// syncBuffer is a stdout that the test can read while Execute is still writing to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) lines() []string {
	s := strings.TrimSpace(b.String())
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// watchSnapshot and watchEvent are the lines a stand-in supervisor streams.
func watchSnapshot(send func(any) error) error {
	return send(control.SnapshotLine{T: 1, K: "snapshot", Seq: 4, Status: readyStatus("w")})
}

func watchEvent(seq int) map[string]any {
	return map[string]any{"t": 100 * seq, "k": "duty", "seq": seq, "port": 1, "v": 0.5}
}

// streamed runs `sil watch args...` against fc and returns the captured output.
func streamed(t *testing.T, fc *fakeControl, args ...string) (stdout []string, stderr string, exit int) {
	t.Helper()
	cliEnv(t)
	liveInstance(t, "w", fc)
	var so syncBuffer
	var se bytes.Buffer
	exit = Execute(append([]string{"watch", "-i", "w"}, args...), &so, &se)
	return so.lines(), se.String(), exit
}

func TestWatchCopiesTheStreamUntilStopped(t *testing.T) {
	fc := &fakeControl{watch: func(_ context.Context, send func(any) error) error {
		if err := watchSnapshot(send); err != nil {
			return err
		}
		for seq := 5; seq <= 6; seq++ {
			if err := send(watchEvent(seq)); err != nil {
				return err
			}
		}
		return send(control.NewStoppedLine(time.Second))
	}}
	lines, stderr, exit := streamed(t, fc)
	if exit != 0 || len(lines) != 4 {
		t.Fatalf("exit %d with %d lines, want exit 0 and snapshot, 2 events, stopped\n%q\nstderr: %s", exit, len(lines), lines, stderr)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &snap); err != nil || snap["k"] != "snapshot" || snap["seq"] != float64(4) || snap["instance"] != "w" {
		t.Fatalf("first line %q is not the snapshot with the status fields", lines[0])
	}
	if !control.IsStoppedLine([]byte(lines[3])) {
		t.Fatalf("last line %q, want the stopped line", lines[3])
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Fatalf("line %q is not JSON", l)
		}
	}
}

func TestWatchKindFlagReachesTheRequest(t *testing.T) {
	fc := &fakeControl{watch: func(_ context.Context, send func(any) error) error { return send(control.NewStoppedLine(0)) }}
	if _, _, exit := streamed(t, fc, "--kind", "duty,button"); exit != 0 {
		t.Fatalf("exit %d", exit)
	}
	if want := [][]string{{"duty", "button"}}; !reflect.DeepEqual(fc.watchKinds, want) {
		t.Fatalf("watch asked for %v, want %v", fc.watchKinds, want)
	}
}

// --count N: exit 0 after N event lines following the snapshot, while the stream goes on.
func TestWatchCountEndsTheStream(t *testing.T) {
	fc := &fakeControl{watch: func(ctx context.Context, send func(any) error) error {
		_ = watchSnapshot(send)
		for seq := 5; seq <= 20; seq++ {
			if err := send(watchEvent(seq)); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	lines, _, exit := streamed(t, fc, "--count", "3")
	if exit != 0 || len(lines) != 4 {
		t.Fatalf("exit %d with %d lines, want exit 0 and the snapshot plus 3 events: %q", exit, len(lines), lines)
	}
	var last map[string]any
	_ = json.Unmarshal([]byte(lines[3]), &last)
	if last["seq"] != float64(7) {
		t.Fatalf("last line %q, want the third event (seq 7)", lines[3])
	}
}

// A connection that drops without the stopped line is a lost instance: exit 2.
func TestWatchDroppedStreamIsControlUnreachable(t *testing.T) {
	fc := &fakeControl{watch: func(_ context.Context, send func(any) error) error {
		_ = watchSnapshot(send)
		return send(watchEvent(5)) // the handler returns here: no stopped line
	}}
	lines, stderr, exit := streamed(t, fc)
	if exit != 2 || len(lines) != 3 {
		t.Fatalf("exit %d with %d lines, want exit 2 with snapshot, event and the error: %q", exit, len(lines), lines)
	}
	var eb struct {
		Error *apperr.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &eb); err != nil || eb.Error == nil || eb.Error.Code != "control_unreachable" {
		t.Fatalf("last line %q, want {error: control_unreachable}", lines[2])
	}
	if !strings.Contains(stderr, "control_unreachable") {
		t.Fatalf("stderr %q does not name control_unreachable", stderr)
	}
}

// The 17th stream is refused before anything is printed but the error.
func TestWatchTooManyStreams(t *testing.T) {
	fc := &fakeControl{watch: func(context.Context, func(any) error) error {
		return apperr.Infra("too_many_streams", "full", map[string]any{"limit": 16})
	}}
	lines, _, exit := streamed(t, fc)
	if exit != 2 || len(lines) != 1 {
		t.Fatalf("exit %d with lines %q, want exit 2 and exactly the error object", exit, lines)
	}
	var eb struct {
		Error *apperr.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &eb); err != nil || eb.Error == nil || eb.Error.Code != "too_many_streams" {
		t.Fatalf("stdout %q, want {error: too_many_streams}", lines[0])
	}
}

func TestWatchArgumentErrors(t *testing.T) {
	fc := &fakeControl{watch: func(context.Context, func(any) error) error { return nil }}
	for name, args := range map[string][]string{
		"unknown kind":   {"--kind", "duty,nope"},
		"negative count": {"--count", "-1"},
	} {
		t.Run(name, func(t *testing.T) {
			lines, _, exit := streamed(t, fc, args...)
			var eb struct {
				Error *apperr.Error `json:"error"`
			}
			if exit != 1 || len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &eb) != nil || eb.Error == nil || eb.Error.Code != "invalid_argument" {
				t.Fatalf("exit %d, stdout %q, want exit 1 invalid_argument", exit, lines)
			}
		})
	}
	if len(fc.watchKinds) != 0 {
		t.Fatalf("the instance was asked to watch for invalid arguments: %v", fc.watchKinds)
	}
}

// Ctrl-C ends the stream with exit 0.
func TestWatchSIGINTEndsWithExit0(t *testing.T) {
	fc := &fakeControl{watch: func(ctx context.Context, send func(any) error) error {
		_ = watchSnapshot(send)
		<-ctx.Done()
		return ctx.Err()
	}}
	cliEnv(t)
	liveInstance(t, "w", fc)
	var so syncBuffer
	var se bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Execute([]string{"watch", "-i", "w"}, &so, &se) }()

	ctx, cancel := clock.WithTimeout(context.Background(), testClock, 5*time.Second)
	defer cancel()
	for len(so.lines()) == 0 {
		if err := testClock.Sleep(ctx, 5*time.Millisecond); err != nil {
			t.Fatal("no snapshot line before the SIGINT")
		}
	}
	// Execute has installed its signal handler by now (it printed a line from the stream).
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-done:
		if exit != 0 {
			t.Fatalf("exit %d after SIGINT, want 0\nstdout: %s\nstderr: %s", exit, so.String(), se.String())
		}
	case <-ctx.Done():
		t.Fatal("sil watch did not end after SIGINT")
	}
	if lines := so.lines(); len(lines) != 1 {
		t.Fatalf("stdout %q after SIGINT, want just the snapshot line", lines)
	}
}

// relayStream against a stream that is cut in the middle of the run.
func TestRelayStreamLostMidway(t *testing.T) {
	var out bytes.Buffer
	err := relayStream(context.Background(), cutStream{"{\"k\":\"snapshot\"}\n{\"k\":\"duty\""}, "/x", &out, 1, 0, nil)
	var ae *apperr.Error
	if !asAppErr(err, &ae) || ae.Code != "control_unreachable" || ae.Exit != apperr.ExitInfra {
		t.Fatalf("got %v, want control_unreachable", err)
	}
	if out.String() != "{\"k\":\"snapshot\"}\n" {
		t.Fatalf("output %q: a half line that the connection cut off must not be printed", out.String())
	}
}

type cutStream struct{ data string }

func (c cutStream) Stream(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(c.data)), nil
}

func asAppErr(err error, target **apperr.Error) bool { return errors.As(err, target) }

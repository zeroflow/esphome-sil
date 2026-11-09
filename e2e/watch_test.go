//go:build sil_e2e

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// watchProc is a running `sil watch` (or `sil logs --follow`): its stdout lines arrive on lines as
// the process prints them; exitc carries the exit code once the process has ended.
type watchProc struct {
	cmd    *exec.Cmd
	lines  chan string
	exitc  chan int
	stderr *bytes.Buffer
	seen   []string // every line handed out by next, for failure messages
	args   []string
}

// watchStart runs `sil args...` with the Env's isolated environment and reads its stdout
// line by line on a goroutine. The process is killed at test cleanup if it is still alive.
func watchStart(t *testing.T, env *Env, args ...string) *watchProc {
	t.Helper()
	cmd := exec.Command(env.Bin, args...)
	cmd.Env = env.environ()
	stderr := &bytes.Buffer{}
	var emu sync.Mutex
	cmd.Stderr = watchLockedWriter{&emu, stderr}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("sil %v: %v", args, err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sil %v: %v", args, err)
	}
	w := &watchProc{cmd: cmd, lines: make(chan string, 1<<16), exitc: make(chan int, 1), stderr: stderr, args: args}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			w.lines <- sc.Text()
		}
		close(w.lines)
		code := 0
		if err := cmd.Wait(); err != nil {
			code = -1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		w.exitc <- code
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return w
}

type watchLockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l watchLockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// next returns the next stdout line, decoded; it fails the test when none arrives within d or
// the process ended.
func (w *watchProc) next(t *testing.T, d time.Duration, what string) map[string]any {
	t.Helper()
	m, ok := w.nextOrTimeout(t, d)
	if !ok {
		t.Fatalf("%s: no line within %s from sil %v (lines so far: %d)\nstderr: %s", what, d, w.args, len(w.seen), w.stderr.String())
	}
	return m
}

// nextOrTimeout is next without the failure: ok is false when d ran out.
func (w *watchProc) nextOrTimeout(t *testing.T, d time.Duration) (map[string]any, bool) {
	t.Helper()
	tm := sysClock.NewTimer(d)
	defer tm.Stop()
	select {
	case line, open := <-w.lines:
		if !open {
			t.Fatalf("sil %v ended while a line was expected (lines so far %d)\nstderr: %s", w.args, len(w.seen), w.stderr.String())
		}
		w.seen = append(w.seen, line)
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("sil %v printed a line that is not a JSON object: %q", w.args, line)
		}
		return m, true
	case <-tm.C():
		return nil, false
	}
}

// until reads lines until match holds for one and returns it with every line read on the way,
// the match last. It fails the test after d.
func (w *watchProc) until(t *testing.T, d time.Duration, what string, match func(map[string]any) bool) (map[string]any, []map[string]any) {
	t.Helper()
	deadline := sysClock.Now() + d
	var read []map[string]any
	for {
		left := deadline - sysClock.Now()
		if left <= 0 {
			t.Fatalf("%s: not seen within %s; read %d lines, the last ones: %v", what, d, len(read), watchTail(read, 5))
		}
		m, ok := w.nextOrTimeout(t, left)
		if !ok {
			continue
		}
		read = append(read, m)
		if match(m) {
			return m, read
		}
	}
}

func watchTail(lines []map[string]any, n int) []map[string]any {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

// exit waits for the process to end and returns its exit code.
func (w *watchProc) exit(t *testing.T, d time.Duration) int {
	t.Helper()
	tm := sysClock.NewTimer(d)
	defer tm.Stop()
	select {
	case code := <-w.exitc:
		return code
	case <-tm.C():
		t.Fatalf("sil %v did not exit within %s\nstderr: %s", w.args, d, w.stderr.String())
		return -1
	}
}

// drain returns the remaining stdout lines once the process has ended.
func (w *watchProc) drain() []string {
	var rest []string
	for l := range w.lines {
		rest = append(rest, l)
	}
	return rest
}

func (w *watchProc) interrupt(t *testing.T) {
	t.Helper()
	if err := w.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("SIGINT to sil %v: %v", w.args, err)
	}
}

// watchInt reports whether v is a JSON number with an integral value.
func watchInt(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f)
}

// watchCheckLine requires integer t, a kind and an integer seq.
func watchCheckLine(t *testing.T, m map[string]any) {
	t.Helper()
	if !watchInt(m["t"]) || !watchInt(m["seq"]) {
		t.Fatalf("line %v: want an integer t and an integer seq", m)
	}
	if k, _ := m["k"].(string); k == "" {
		t.Fatalf("line %v: no k", m)
	}
}

// TestWatchStream is CLI-03 end to end: snapshot, then typed change events, across a simulator
// restart, ended by `sil down`.
func TestWatchStream(t *testing.T) {
	in := r33Up(t, "e2e-watch")
	watch := func(t *testing.T, extra ...string) *watchProc {
		t.Helper()
		return watchStart(t, in.env, append([]string{"watch", "-i", in.name}, extra...)...)
	}

	t.Run("snapshot_first", func(t *testing.T) {
		w := watch(t)
		snap := w.next(t, 10*time.Second, "the snapshot")
		if snap["k"] != "snapshot" || snap["schema"] != float64(1) || snap["instance"] != in.name || !watchInt(snap["t"]) || !watchInt(snap["seq"]) {
			t.Fatalf("first line %v, want k snapshot, schema 1, instance %s and an integer t and seq", snap, in.name)
		}
		for _, field := range []string{"ports", "env", "buttons", "leds", "api", "firmware", "sim"} {
			if snap[field] == nil {
				t.Fatalf("snapshot has no %s: %v", field, snap)
			}
		}
		// The same fields as `sil status`: every key of the status object is on the snapshot line.
		st := in.env.Status(t, in.name)
		for key := range st {
			if _, ok := snap[key]; !ok {
				t.Fatalf("status has key %q, the snapshot line does not: %v", key, snap)
			}
		}
		// The boot pulse is still running (it lasts until a real API client connects), so events
		// keep coming and continue at snapshot seq + 1 without a gap.
		base := snap["seq"].(float64)
		for i := 1; i <= 10; i++ {
			ev := w.next(t, 10*time.Second, fmt.Sprintf("event %d after the snapshot", i))
			watchCheckLine(t, ev)
			if ev["k"] == "lagged" {
				t.Fatalf("lagged line during the first 10 events: %v", ev)
			}
			if ev["seq"] != base+float64(i) {
				t.Fatalf("event %d is %v, want seq %v (snapshot seq %v + %d)", i, ev, base+float64(i), base, i)
			}
		}
		w.interrupt(t)
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d after SIGINT, want 0\nstderr: %s", code, w.stderr.String())
		}
	})

	t.Run("status_while_streaming", func(t *testing.T) {
		w1, w2 := watch(t), watch(t, "--kind", "sim")
		w1.next(t, 10*time.Second, "snapshot of stream 1")
		w2.next(t, 10*time.Second, "snapshot of stream 2")
		start := sysClock.Now()
		st := in.env.Status(t, in.name)
		if st["phase"] != "ready" || sysClock.Now()-start > 5*time.Second {
			t.Fatalf("status with two open streams: phase %v after %s", st["phase"], sysClock.Now()-start)
		}
		w1.interrupt(t)
		w2.interrupt(t)
		w1.exit(t, 5*time.Second)
		w2.exit(t, 5*time.Second)
	})

	t.Run("press_events", func(t *testing.T) {
		// Fan 1 at 40 %, then one USR1 press: Fan 1 50 %, Fans 2-4 10 % (see TestPressVocabulary).
		in.setFan(t, 1, 40)
		in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
		w := watch(t)
		w.next(t, 10*time.Second, "the snapshot")
		in.mustPress(t, "usr1")
		var buttons []string
		duties := map[float64]float64{}
		var lastSeq float64
		w.until(t, 10*time.Second, "button true, button false and a duty event on every port", func(m map[string]any) bool {
			watchCheckLine(t, m)
			if seq := m["seq"].(float64); seq <= lastSeq {
				t.Fatalf("seq %v is not above the previous %v: %v", seq, lastSeq, m)
			} else {
				lastSeq = seq
			}
			switch m["k"] {
			case "button":
				if m["id"] == "usr1" {
					buttons = append(buttons, fmt.Sprint(m["v"]))
				}
			case "duty":
				if len(buttons) != 2 {
					t.Fatalf("duty event %v before the press was complete (button events %v)", m, buttons)
				}
				duties[m["port"].(float64)] = m["v"].(float64)
			}
			return len(duties) == 4
		})
		if strings.Join(buttons, ",") != "true,false" {
			t.Fatalf("usr1 button events %v, want true then false", buttons)
		}
		for port, want := range map[float64]float64{1: 0.5, 2: 0.1, 3: 0.1, 4: 0.1} {
			if math.Abs(duties[port]-want) > 1e-3 {
				t.Fatalf("duty of port %v is %v, want %v (all: %v)", port, duties[port], want, duties)
			}
		}
		w.interrupt(t)
		w.exit(t, 5*time.Second)
	})

	t.Run("leds_events", func(t *testing.T) {
		w := watch(t, "--kind", "leds")
		w.next(t, 10*time.Second, "the snapshot")
		var times []float64
		first, _ := w.until(t, 3*time.Second, "a leds event within 3 s", func(m map[string]any) bool { return m["k"] == "leds" })
		if strip, _ := first["strip"].([]any); len(strip) != 5 {
			t.Fatalf("leds event %v, want a strip of 5 entries", first)
		}
		times = append(times, first["t"].(float64))
		deadline := sysClock.Now() + 7*time.Second
		for sysClock.Now() < deadline {
			m, ok := w.nextOrTimeout(t, time.Second)
			if ok && m["k"] == "leds" {
				times = append(times, m["t"].(float64))
			}
		}
		for i, ti := range times {
			n := 0
			for _, tj := range times[i:] {
				if tj < ti+3e6 {
					n++
				}
			}
			if n > 66 {
				t.Fatalf("%d leds events within 3 s of sim time starting at event %d, want at most 66 (20 Hz)", n, i)
			}
		}
		if len(times) < 10 {
			t.Fatalf("only %d leds events in 10 s of the boot pulse", len(times))
		}
		w.interrupt(t)
		w.exit(t, 5*time.Second)
	})

	t.Run("env_event", func(t *testing.T) {
		w := watch(t, "--kind", "env")
		w.next(t, 10*time.Second, "the snapshot")
		if out, code := in.set(t, "temperature=26"); code != 0 {
			t.Fatalf("set temperature=26: exit %d: %v", code, out)
		}
		ev := w.next(t, 10*time.Second, "the env event")
		watchCheckLine(t, ev)
		if ev["k"] != "env" || ev["id"] != "temperature" || ev["v"] != float64(26) {
			t.Fatalf("event %v, want env temperature 26", ev)
		}
		w.interrupt(t)
		w.exit(t, 5*time.Second)
	})

	t.Run("kind_filter", func(t *testing.T) {
		w := watch(t, "--kind", "duty")
		snap := w.next(t, 10*time.Second, "the snapshot")
		if snap["k"] != "snapshot" {
			t.Fatalf("first line %v", snap)
		}
		in.mustPress(t, "usr3")
		duties := 0
		deadline := sysClock.Now() + 4*time.Second
		for sysClock.Now() < deadline {
			m, ok := w.nextOrTimeout(t, time.Second)
			if !ok {
				continue
			}
			watchCheckLine(t, m)
			switch m["k"] {
			case "duty":
				duties++
			case "lagged":
			default:
				t.Fatalf("line %v with --kind duty, want only duty (and lagged)", m)
			}
		}
		if duties == 0 {
			t.Fatal("no duty event after pressing usr3")
		}
		w.interrupt(t)
		w.exit(t, 5*time.Second)

		// An unknown kind never reaches the instance.
		out, code, _ := in.env.Sil(t, "watch", "-i", in.name, "--kind", "duty,nope")
		r33ExpectErr(t, "watch --kind nope", out, code, "invalid_argument")
	})

	t.Run("count", func(t *testing.T) {
		w := watch(t, "--count", "3")
		code := w.exit(t, 15*time.Second)
		lines := w.drain()
		if code != 0 || len(lines) != 4 {
			t.Fatalf("exit %d with %d lines, want exit 0 and the snapshot plus 3 event lines: %q", code, len(lines), lines)
		}
		var snap map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &snap); err != nil || snap["k"] != "snapshot" {
			t.Fatalf("first line %q is not the snapshot", lines[0])
		}
	})

	t.Run("too_many_streams", func(t *testing.T) {
		var ws []*watchProc
		for i := 0; i < 16; i++ {
			w := watch(t, "--kind", "sim")
			w.next(t, 10*time.Second, fmt.Sprintf("snapshot of stream %d", i+1))
			ws = append(ws, w)
		}
		out, code, _ := in.env.Sil(t, "watch", "-i", in.name)
		if code != 2 || r33ErrCode(out) != "too_many_streams" {
			t.Fatalf("the 17th stream: exit %d %v, want exit 2 too_many_streams", code, out)
		}
		st := in.env.Status(t, in.name)
		if st["phase"] != "ready" {
			t.Fatalf("status with 16 streams open: phase %v", st["phase"])
		}
		for _, w := range ws {
			w.interrupt(t)
		}
		for _, w := range ws {
			w.exit(t, 5*time.Second)
		}
	})

	t.Run("across_sim_restart", func(t *testing.T) {
		w := watch(t, "--kind", "sim,firmware,button")
		w.next(t, 10*time.Second, "the snapshot")
		if out, code, _ := in.env.Sil(t, "sim", "restart", in.name); code != 0 {
			t.Fatalf("sim restart: exit %d: %v", code, out)
		}
		var fw []string
		restarted := false
		w.until(t, 20*time.Second, "sim restarted and firmware false then true", func(m map[string]any) bool {
			watchCheckLine(t, m)
			switch m["k"] {
			case "sim":
				restarted = m["v"] == "restarted"
			case "firmware":
				fw = append(fw, fmt.Sprint(m["v"]))
			}
			return restarted && strings.Join(fw, ",") == "false,true"
		})
		in.mustPress(t, "usr2")
		_, got := w.until(t, 10*time.Second, "usr2 button events on the same stream", func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr2" && m["v"] == false
		})
		if len(got) == 0 {
			t.Fatal("no events")
		}
		w.interrupt(t)
		w.exit(t, 5*time.Second)
	})

	t.Run("stopped_on_down", func(t *testing.T) {
		w := watch(t, "--kind", "sim")
		w.next(t, 10*time.Second, "the snapshot")
		start := sysClock.Now()
		out, code, _ := in.env.Sil(t, "down", in.name)
		if code != 0 {
			t.Fatalf("down: exit %d: %v", code, out)
		}
		stopped, _ := w.until(t, 5*time.Second, "the stopped line", func(m map[string]any) bool { return m["k"] == "stopped" })
		if !watchInt(stopped["t"]) {
			t.Fatalf("stopped line %v, want an integer t", stopped)
		}
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d after the instance stopped, want 0\nstderr: %s", code, w.stderr.String())
		}
		t.Logf("down plus stopped line took %s with an open stream", sysClock.Now()-start)
	})
}

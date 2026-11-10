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
		// keep coming. Their seq is strictly increasing and starts above the snapshot's; it is not
		// consecutive any more because the opt-in 10 Hz sample events (03-07) take broker seq
		// numbers that a default stream never receives.
		prev := snap["seq"].(float64)
		for i := 1; i <= 10; i++ {
			ev := w.next(t, 10*time.Second, fmt.Sprintf("event %d after the snapshot", i))
			watchCheckLine(t, ev)
			if ev["k"] == "lagged" {
				t.Fatalf("lagged line during the first 10 events: %v", ev)
			}
			if ev["k"] == "sample" {
				t.Fatalf("a default stream carried a sample line: %v", ev)
			}
			if seq := ev["seq"].(float64); seq <= prev {
				t.Fatalf("event %d is %v, want a seq above %v (snapshot seq %v)", i, ev, prev, snap["seq"])
			} else {
				prev = seq
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

	// This subtest closes audit tech debt #1 (cross-phase 01 resync x 02 buttons x
	// 03 page): a USR button held while the simulator restarts is released by the stopping engine.
	// The stream says so (reason engine_stopped) before `sim restarted`, `sil status` in the gap
	// shows it released, and the firmware sees the release at the resync. USR3 is a 50..1000 ms
	// click in examples/rev33.yaml, so a hold across a 2 s gap triggers nothing in the firmware
	// (USR2 is a long press and would).
	t.Run("held_button_across_sim_restart", func(t *testing.T) {
		w := watch(t, "--kind", "sim,firmware,button")
		w.next(t, 10*time.Second, "the snapshot")
		in.mustPress(t, "usr3", "--down")
		w.until(t, 10*time.Second, "usr3 held", func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr3" && m["v"] == true
		})

		res := in.async("sim", "restart", in.name, "--gap", "2s")

		// The gap status comes from the stopped engine's final snapshot: usr3 is released.
		deadline := sysClock.Now() + 10*time.Second
		for {
			st := in.env.Status(t, in.name)
			if jget(st, "firmware", "bus_connected") == false {
				if level, present := r33Buttons(st, "usr3"); !present || level {
					t.Fatalf("status in the restart gap: usr3 present %v, level %v, want present and released", present, level)
				}
				break
			}
			if sysClock.Now() > deadline {
				t.Fatalf("no status with bus_connected false within 10s of the restart")
			}
			if err := sysClock.Sleep(t.Context(), 100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		if r := r33Wait(t, "sim restart", res); r.exit != 0 {
			t.Fatalf("sim restart: exit %d: %v", r.exit, r.out)
		}

		restarted := false
		_, got := w.until(t, 20*time.Second, "firmware true after sim restarted", func(m map[string]any) bool {
			watchCheckLine(t, m)
			switch m["k"] {
			case "sim":
				if m["v"] == "restarted" {
					restarted = true
				}
			case "firmware":
				return restarted && m["v"] == true
			}
			return false
		})
		idx := func(match func(map[string]any) bool) int {
			for i, m := range got {
				if match(m) {
					return i
				}
			}
			return -1
		}
		isRelease := func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr3" && m["v"] == false
		}
		fwFalse := idx(func(m map[string]any) bool { return m["k"] == "firmware" && m["v"] == false })
		rel := idx(isRelease)
		rst := idx(func(m map[string]any) bool { return m["k"] == "sim" && m["v"] == "restarted" })
		fwTrue := len(got) - 1
		if !(fwFalse >= 0 && fwFalse < rel && rel < rst && rst < fwTrue) {
			t.Fatalf("order: firmware false %d, usr3 release %d, sim restarted %d, firmware true %d, want strictly increasing; lines %v", fwFalse, rel, rst, fwTrue, got)
		}
		if got[rel]["reason"] != "engine_stopped" {
			t.Fatalf("usr3 release %v, want reason engine_stopped", got[rel])
		}
		for i, m := range got {
			if i != rel && i < rst && isRelease(m) {
				t.Fatalf("second usr3 release before sim restarted: %v", m)
			}
		}

		out, code := in.press(t, "usr3", "--up")
		r33ExpectErr(t, "up after the restart", out, code, "button_not_held")

		in.mustPress(t, "usr3")
		w.until(t, 10*time.Second, "usr3 pressed again", func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr3" && m["v"] == true && m["reason"] == nil
		})
		w.until(t, 10*time.Second, "usr3 released again", func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr3" && m["v"] == false && m["reason"] == nil
		})
		w.interrupt(t)
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d, want 0\nstderr: %s", code, w.stderr.String())
		}
	})

	// stopped_on_down also holds usr1 over `sil down`: the stopping engine puts its engine_stopped
	// release on the stream before the stopped line (tech debt #1).
	t.Run("stopped_on_down", func(t *testing.T) {
		w := watch(t, "--kind", "sim,button")
		w.next(t, 10*time.Second, "the snapshot")
		in.mustPress(t, "usr1", "--down")
		w.until(t, 10*time.Second, "usr1 held", func(m map[string]any) bool {
			return m["k"] == "button" && m["id"] == "usr1" && m["v"] == true
		})
		start := sysClock.Now()
		out, code, _ := in.env.Sil(t, "down", in.name)
		if code != 0 {
			t.Fatalf("down: exit %d: %v", code, out)
		}
		stopped, read := w.until(t, 5*time.Second, "the stopped line", func(m map[string]any) bool { return m["k"] == "stopped" })
		released := false
		for _, m := range read {
			if m["k"] == "button" && m["id"] == "usr1" && m["v"] == false && m["reason"] == "engine_stopped" {
				released = true
			}
		}
		if !released {
			t.Fatalf("no usr1 release with reason engine_stopped before the stopped line; read %v", read)
		}
		if !watchInt(stopped["t"]) {
			t.Fatalf("stopped line %v, want an integer t", stopped)
		}
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d after the instance stopped, want 0\nstderr: %s", code, w.stderr.String())
		}
		t.Logf("down plus stopped line took %s with an open stream", sysClock.Now()-start)
	})
}

// watchLogLines decodes the lines of a `sil logs` result.
func watchLogLines(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	list, _ := out["lines"].([]any)
	var lines []map[string]any
	for _, v := range list {
		m, _ := v.(map[string]any)
		if m == nil {
			t.Fatalf("lines entry %v is not an object", v)
		}
		lines = append(lines, m)
	}
	return lines
}

// watchCleanLogs requires that no line carries an ESC byte or the instance's API key.
func watchCleanLogs(t *testing.T, in *r33Inst, lines []map[string]any) {
	t.Helper()
	for _, l := range lines {
		text, _ := l["line"].(string)
		if strings.ContainsRune(text, 0x1b) {
			t.Fatalf("line %q holds an ANSI escape byte", text)
		}
		if strings.Contains(text, in.key) {
			t.Fatalf("line %q holds the API key", text)
		}
	}
}

// TestLogs is CLI-04 end to end: tail, sources, bounds, raw, follow across a firmware restart,
// and the stopped line when the instance goes down.
func TestLogs(t *testing.T) {
	in := r33Up(t, "e2e-logs")
	logs := func(t *testing.T, extra ...string) (map[string]any, int) {
		t.Helper()
		out, code, _ := in.env.Sil(t, append([]string{"logs", "-i", in.name}, extra...)...)
		return out, code
	}

	t.Run("firmware_tail", func(t *testing.T) {
		out, code := logs(t, "--tail", "10000")
		if code != 0 || out["source"] != "firmware" {
			t.Fatalf("logs: exit %d: %v, want exit 0 and source firmware", code, out)
		}
		lines := watchLogLines(t, out)
		found := false
		for _, l := range lines {
			if l["src"] != "firmware" {
				t.Fatalf("line %v, want only firmware lines", l)
			}
			if l["line"] == "=== firmware start 1 ===" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no `=== firmware start 1 ===` line among %d lines", len(lines))
		}
		watchCleanLogs(t, in, lines)
		// The default tail is 100 lines.
		def, code := logs(t)
		if code != 0 || len(watchLogLines(t, def)) > 100 {
			t.Fatalf("default logs: exit %d with %d lines, want at most 100", code, len(watchLogLines(t, def)))
		}
	})

	t.Run("sim_and_all", func(t *testing.T) {
		sim, code := logs(t, "--source", "sim")
		simLines := watchLogLines(t, sim)
		if code != 0 || sim["source"] != "sim" || len(simLines) == 0 {
			t.Fatalf("logs --source sim: exit %d: %v, want a non-empty list", code, sim)
		}
		for _, l := range simLines {
			if l["src"] != "sim" {
				t.Fatalf("line %v, want only sim lines", l)
			}
		}
		watchCleanLogs(t, in, simLines)
		all, code := logs(t, "--source", "all", "--tail", "50")
		if code != 0 || all["source"] != "all" {
			t.Fatalf("logs --source all: exit %d: %v", code, all)
		}
		lines := watchLogLines(t, all)
		firstSim, lastFw := -1, -1
		for i, l := range lines {
			switch l["src"] {
			case "sim":
				if firstSim < 0 {
					firstSim = i
				}
			case "firmware":
				lastFw = i
			}
		}
		if firstSim < 0 || lastFw < 0 || lastFw > firstSim {
			t.Fatalf("--source all: firmware lines up to %d, sim lines from %d, want both present and firmware first", lastFw, firstSim)
		}
		watchCleanLogs(t, in, lines)
	})

	t.Run("tail_bounds", func(t *testing.T) {
		out, code := logs(t, "--source", "all", "--tail", "1")
		perSrc := map[any]int{}
		for _, l := range watchLogLines(t, out) {
			perSrc[l["src"]]++
		}
		if code != 0 || perSrc["firmware"] != 1 || perSrc["sim"] != 1 || len(perSrc) != 2 {
			t.Fatalf("logs --tail 1 --source all: exit %d, lines per source %v, want one each", code, perSrc)
		}
		for _, bad := range [][]string{{"--tail", "0"}, {"--tail", "10001"}, {"--source", "nope"}, {"--source", "build"}} {
			out, code := logs(t, bad...)
			r33ExpectErr(t, fmt.Sprint("logs ", bad), out, code, "invalid_argument")
		}
	})

	t.Run("raw", func(t *testing.T) {
		so, _, code := in.env.run(t, "logs", "-i", in.name, "--raw", "--tail", "10000")
		if code != 0 {
			t.Fatalf("logs --raw: exit %d", code)
		}
		var obj map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(so)), &obj) == nil {
			t.Fatalf("logs --raw printed a JSON object: %.200s", so)
		}
		if !strings.Contains(so, "=== firmware start 1 ===\n") {
			t.Fatalf("logs --raw output has no separator line: %.300s", so)
		}
		if strings.Contains(so, in.key) {
			t.Fatal("logs --raw leaked the API key")
		}
	})

	t.Run("follow_across_fw_restart", func(t *testing.T) {
		w := watchStart(t, in.env, "logs", "-i", in.name, "--follow", "--tail", "1")
		w.next(t, 10*time.Second, "the first tail line")
		if out, code, _ := in.env.Sil(t, "fw", "restart", in.name); code != 0 {
			t.Fatalf("fw restart: exit %d: %v", code, out)
		}
		line, _ := w.until(t, 10*time.Second, "the second firmware start separator", func(m map[string]any) bool {
			return m["line"] == "=== firmware start 2 ==="
		})
		if line["src"] != "firmware" {
			t.Fatalf("separator line %v, want src firmware", line)
		}
		w.interrupt(t)
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d after SIGINT, want 0", code)
		}
	})

	t.Run("follow_stopped_on_down", func(t *testing.T) {
		w := watchStart(t, in.env, "logs", "-i", in.name, "--follow", "--tail", "1")
		w.next(t, 10*time.Second, "the first tail line")
		if out, code, _ := in.env.Sil(t, "down", in.name); code != 0 {
			t.Fatalf("down: exit %d: %v", code, out)
		}
		w.until(t, 5*time.Second, "the stopped line", func(m map[string]any) bool { return m["k"] == "stopped" })
		if code := w.exit(t, 5*time.Second); code != 0 {
			t.Fatalf("exit %d after the instance stopped, want 0\nstderr: %s", code, w.stderr.String())
		}
	})
}

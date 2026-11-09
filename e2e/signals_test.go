//go:build sil_e2e

package e2e

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// signalsInstance is the instance name the tests of this file share (one at a time).
const signalsInstance = "e2e-sig"

// sampleRows returns the samples of a `sil history` result.
func sampleRows(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["samples"].([]any)
	if !ok {
		t.Fatalf("history result has no samples array: %v", out)
	}
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("samples[%d] = %v, want an object", i, r)
		}
		rows[i] = m
	}
	return rows
}

// checkSampleRow requires k sample, an integer t and seq, and four entries for ports 1..4.
func checkSampleRow(t *testing.T, m map[string]any) {
	t.Helper()
	watchCheckLine(t, m)
	if m["k"] != "sample" {
		t.Fatalf("line %v: want k sample", m)
	}
	v, _ := m["v"].([]any)
	if len(v) != 4 {
		t.Fatalf("sample %v: want 4 entries, one per port", m)
	}
	for i, e := range v {
		if got := jget(e, "port"); got != float64(i+1) {
			t.Fatalf("sample %v: entry %d is port %v, want %d", m, i, got, i+1)
		}
	}
}

// gapRows returns the indexes of the rows that carry a gap member; every one must be true.
func gapRows(t *testing.T, rows []map[string]any) []int {
	t.Helper()
	var at []int
	for i, m := range rows {
		if g, has := m["gap"]; has {
			if g != true {
				t.Fatalf("sample %d has gap %v, want only true", i, g)
			}
			at = append(at, i)
		}
	}
	return at
}

// TestHistory is D-12 end to end: the engine's 10 Hz sample events reach `sil history`, the
// opt-in `sample` watch kind and GET /api/v1/history on the web listener, and a simulator
// restart is marked in the stream itself.
func TestHistory(t *testing.T) {
	in := r33Up(t, signalsInstance)
	in.setFan(t, 1, 40)
	in.waitDuties(t, [4]float64{0.4, 0, 0, 0})
	sysClock.Sleep(t.Context(), 3*time.Second)

	t.Run("history", func(t *testing.T) {
		out, code, stderr := in.env.Sil(t, "history", "-i", in.name, "--window", "2s")
		if code != 0 {
			t.Fatalf("sil history: exit %d: %v\n%s", code, out, stderr)
		}
		rows := sampleRows(t, out)
		if len(rows) < 15 || len(rows) > 25 {
			t.Fatalf("%d samples in a 2 s window, want 15..25 (10 Hz)", len(rows))
		}
		var gaps []float64
		prevSeq := -1.0
		for i, m := range rows {
			checkSampleRow(t, m)
			if seq := m["seq"].(float64); seq <= prevSeq {
				t.Fatalf("sample %d seq %v does not increase after %v", i, seq, prevSeq)
			} else {
				prevSeq = seq
			}
			if i > 0 {
				gaps = append(gaps, m["t"].(float64)-rows[i-1]["t"].(float64))
			}
		}
		slices.Sort(gaps)
		if med := gaps[len(gaps)/2]; med < 90000 || med > 110000 {
			t.Fatalf("median gap between samples is %v us, want 90000..110000 (100 ms of sim time)", med)
		}
		t.Logf("measured sample spacing: median %v us over %d samples", gaps[len(gaps)/2], len(rows))
		newest := rows[len(rows)-1]
		if d := jget(newest, "v", 0, "duty"); d != 0.4 {
			t.Fatalf("newest sample port 1 duty = %v, want 0.4", d)
		}
		if rseq, _ := out["seq"].(float64); rseq < prevSeq {
			t.Fatalf("result seq %v is below the last sample's seq %v", rseq, prevSeq)
		}
		if w, _ := out["window_s"].(float64); w != 2 {
			t.Fatalf("result window_s = %v, want 2", out["window_s"])
		}
	})

	t.Run("sample is opt-in", func(t *testing.T) {
		w := watchStart(t, in.env, "watch", "-i", in.name)
		w.next(t, 10*time.Second, "the snapshot")
		for {
			m, ok := w.nextOrTimeout(t, 2*time.Second)
			if !ok {
				break
			}
			if m["k"] == "sample" {
				t.Fatalf("default sil watch carried a sample line: %v", m)
			}
		}
		w.interrupt(t)
		w.exit(t, 10*time.Second)

		so, se, code := in.env.run(t, "watch", "-i", in.name, "--kind", "sample", "--count", "3")
		if code != 0 {
			t.Fatalf("sil watch --kind sample --count 3: exit %d\n%s", code, se)
		}
		n := 0
		for _, line := range strings.Split(strings.TrimSpace(so), "\n") {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("not JSON: %q", line)
			}
			if m["k"] == "sample" {
				checkSampleRow(t, m)
				n++
			}
		}
		if n != 3 {
			t.Fatalf("%d sample lines, want exactly 3:\n%s", n, so)
		}
	})

	t.Run("window is bounded", func(t *testing.T) {
		out, code, _ := in.env.Sil(t, "history", "-i", in.name, "--window", "301s")
		if code != 1 || r33ErrCode(out) != "invalid_argument" {
			t.Fatalf("history --window 301s: exit %d, %v, want exit 1 invalid_argument", code, out)
		}
	})

	t.Run("web", func(t *testing.T) {
		status, _, body := webDo(t, in.env.webPorts[in.name], "GET", "/api/v1/history?window_s=1")
		if status != 200 {
			t.Fatalf("GET /api/v1/history?window_s=1 = %d: %s", status, body)
		}
		if rows := sampleRows(t, webJSON(t, body)); len(rows) == 0 {
			t.Fatalf("the web history has no samples: %s", body)
		}
	})

	t.Run("restart gap marker", func(t *testing.T) {
		if out, code, stderr := in.env.Sil(t, "sim", "restart", in.name, "--gap", "1s"); code != 0 {
			t.Fatalf("sil sim restart: exit %d: %v\n%s", code, out, stderr)
		}
		sysClock.Sleep(t.Context(), time.Second)
		out, code, stderr := in.env.Sil(t, "history", "-i", in.name)
		if code != 0 {
			t.Fatalf("sil history: exit %d: %v\n%s", code, out, stderr)
		}
		rows := sampleRows(t, out)
		at := gapRows(t, rows)
		if len(at) != 2 || at[0] != 0 {
			t.Fatalf("gap markers at %v of %d samples, want exactly two: the ring's first sample and the first after the restart", at, len(rows))
		}
		after, before := rows[at[1]], rows[at[1]-1]
		if d := after["t"].(float64) - before["t"].(float64); d < 1_000_000 {
			t.Fatalf("the first sample after the restart is %v us after the previous one, want at least 1000000", d)
		}
	})
}

// TestBehindField proves the wiring of sim.behind_real_time from the engine to status and watch.
// No value is asserted on the real clock: a loaded host may legitimately fall behind. The flip
// thresholds and the 2 s hold-off are proven on the Manual clock by TestBehindRealTime.
func TestBehindField(t *testing.T) {
	in := r33Up(t, signalsInstance)

	st := in.env.Status(t, in.name)
	if _, isBool := jget(st, "sim", "behind_real_time").(bool); !isBool {
		t.Fatalf("status sim.behind_real_time = %v, want a JSON boolean", jget(st, "sim", "behind_real_time"))
	}

	w := watchStart(t, in.env, "watch", "-i", in.name, "--kind", "sim", "--count", "0")
	snap := w.next(t, 10*time.Second, "the snapshot")
	cur, isBool := jget(snap, "sim", "behind_real_time").(bool)
	if !isBool {
		t.Fatalf("snapshot sim.behind_real_time = %v, want a JSON boolean", jget(snap, "sim", "behind_real_time"))
	}
	flips := 0
	deadline := sysClock.Now() + 3*time.Second
	for {
		left := deadline - sysClock.Now()
		if left <= 0 {
			break
		}
		m, ok := w.nextOrTimeout(t, left)
		if !ok {
			break
		}
		if m["k"] != "sim" {
			t.Fatalf("a --kind sim stream carried %v", m)
		}
		switch m["v"] {
		case "behind":
			if cur {
				t.Fatalf("behind event while already behind: %v", m)
			}
			cur = true
			flips++
		case "in_sync":
			if !cur {
				t.Fatalf("in_sync event while already in sync: %v", m)
			}
			cur = false
			flips++
		case "restarted":
		default:
			t.Fatalf("unexpected sim value in %v, want behind, in_sync or restarted", m)
		}
	}
	w.interrupt(t)
	w.exit(t, 10*time.Second)
	t.Logf("behind_real_time flips during the idle watch: %d (snapshot value at the end: %v)", flips, cur)
}

// TestMaxHold is D-15 end to end: a button held with a max hold is released by the engine in sim
// time, the firmware reacts as to a long press, and the guard is reachable from the CLI.
func TestMaxHold(t *testing.T) {
	in := r33Up(t, signalsInstance)
	w := watchStart(t, in.env, "watch", "-i", in.name, "--kind", "button")
	w.next(t, 10*time.Second, "the snapshot")

	start := sysClock.Now()
	out, code := in.press(t, "usr2", "--down", "--max-hold", "1500ms")
	if code != 0 || out["action"] != "down" || out["max_hold_ms"] != float64(1500) || out["level"] != true {
		t.Fatalf("press usr2 --down --max-hold 1500ms: exit %d, %v, want action down and max_hold_ms 1500", code, out)
	}
	if took := sysClock.Now() - start; took > 1200*time.Millisecond {
		t.Errorf("--down --max-hold took %s, want it to return at once", took)
	}

	in.env.WaitStatus(t, in.name, 5*time.Second, func(st map[string]any) bool {
		level, present := r33Buttons(st, "usr2")
		return present && !level
	})
	in.waitDuties(t, [4]float64{1, 1, 1, 1}) // the firmware saw a long press
	rel, _ := w.until(t, 5*time.Second, "the max_hold release", func(m map[string]any) bool {
		return m["k"] == "button" && m["id"] == "usr2" && m["v"] == false && m["reason"] == "max_hold"
	})
	t.Logf("auto-release event: %v", rel)

	out, code = in.press(t, "usr2", "--up")
	if code != 1 || r33ErrCode(out) != "button_not_held" {
		t.Fatalf("press usr2 --up after the auto-release: exit %d, %v, want exit 1 button_not_held", code, out)
	}

	// An explicit up before the deadline cancels it.
	if out, code = in.press(t, "usr2", "--down", "--max-hold", "2s"); code != 0 {
		t.Fatalf("press usr2 --down --max-hold 2s: exit %d, %v", code, out)
	}
	sysClock.Sleep(t.Context(), 500*time.Millisecond)
	if out, code = in.press(t, "usr2", "--up"); code != 0 {
		t.Fatalf("press usr2 --up: exit %d, %v", code, out)
	}
	deadline := sysClock.Now() + 2500*time.Millisecond
	for left := deadline - sysClock.Now(); left > 0; left = deadline - sysClock.Now() {
		m, ok := w.nextOrTimeout(t, left)
		if ok && m["reason"] == "max_hold" {
			t.Fatalf("a cancelled max hold released the button anyway: %v", m)
		}
	}

	for _, args := range [][]string{{"usr1", "--max-hold", "1s"}, {"usr1", "--down", "--max-hold", "0s"}} {
		if out, code = in.press(t, args...); code != 1 || r33ErrCode(out) != "invalid_argument" {
			t.Fatalf("press %v: exit %d, %v, want exit 1 invalid_argument", args, code, out)
		}
	}
}

package supervisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	long := strings.Repeat("a", 70<<10) // longer than a block
	for name, tc := range map[string]struct {
		content string
		n       int
		want    []string
	}{
		"trailing newline":    {"a\nb\nc\n", 2, []string{"b", "c"}},
		"no trailing newline": {"a\nb\nc", 2, []string{"b", "c"}},
		"n exceeds":           {"a\nb\n", 10, []string{"a", "b"}},
		"exactly n":           {"a\nb\n", 2, []string{"a", "b"}},
		"empty file":          {"", 5, nil},
		"one line":            {"only", 3, []string{"only"}},
		"blank lines kept":    {"a\n\nb\n", 3, []string{"a", "", "b"}},
		"crlf":                {"a\r\nb\r\n", 2, []string{"a", "b"}},
		"line cut at 64 KiB":  {"x\n" + long + "\ny\n", 3, []string{"x", long[:64<<10], "y"}},
	} {
		t.Run(name, func(t *testing.T) {
			writeFile(t, path, tc.content)
			got, err := tailLines(path, tc.n)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tailLines = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		got, err := tailLines(filepath.Join(dir, "nope.log"), 5)
		if err != nil || len(got) != 0 {
			t.Fatalf("missing file: %q, %v, want an empty list", got, err)
		}
	})
	t.Run("across the 64 KiB block boundary", func(t *testing.T) {
		var b strings.Builder
		for i := 0; i < 5000; i++ {
			fmt.Fprintf(&b, "line %05d %s\n", i, strings.Repeat("x", 30))
		}
		writeFile(t, path, b.String())
		got, err := tailLines(path, 3000) // about 130 KB: three blocks
		if err != nil || len(got) != 3000 || got[0] != "line 02000 "+strings.Repeat("x", 30) || got[2999] != "line 04999 "+strings.Repeat("x", 30) {
			t.Fatalf("got %d lines (%v), first %q last %q", len(got), err, got[0], got[len(got)-1])
		}
		for i, l := range got {
			if want := fmt.Sprintf("line %05d ", 2000+i); !strings.HasPrefix(l, want) {
				t.Fatalf("line %d is %q, want it to start with %q (a block seam broke a line)", i, l, want)
			}
		}
	})
	t.Run("a directory is an error", func(t *testing.T) {
		if _, err := tailLines(dir, 3); err == nil {
			t.Fatal("tailLines of a directory succeeded")
		}
	})
}

func TestStripANSI(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[0;32mINFO\x1b[0m hello":     "INFO hello",
		"\x1b[1;31m\x1b[0m":               "",
		"plain":                           "plain",
		"keep [brackets] and \x1b other":  "keep [brackets] and \x1b other",
		"\x1b[0;36m[D][x:1]\x1b[0m: text": "[D][x:1]: text",
	} {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

const testKey = "c2VjcmV0LWtleS1mb3ItdGhlLXRlc3Q="

// logSup is a supervisor with a run dir holding firmware.log and sil.log and a Manual clock.
func logSup(t *testing.T) (*supervisor, *clock.Manual) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewManual()
	s := &supervisor{
		paths:   instance.Paths{Name: "t", RunDir: dir},
		id:      instance.Identity{Name: "t", Key: testKey},
		clk:     clk,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		stopped: make(chan struct{}),
		downReq: make(chan struct{}),
	}
	return s, clk
}

func TestLogsServesTailsCleanly(t *testing.T) {
	s, _ := logSup(t)
	writeFile(t, s.paths.FirmwareLog(), "=== firmware start 1 ===\n\x1b[0;32m[I][app:029]: key "+testKey+"\x1b[0m\nlast\n")
	writeFile(t, s.paths.SilLog(), `{"msg":"instance serving"}`+"\n")
	writeFile(t, filepath.Join(s.paths.RunDir, "build.log"), "BUILD SECRET\n")

	res, err := s.Logs(context.Background(), control.LogsRequest{Source: "firmware", Tail: 100})
	if err != nil || res.Source != "firmware" {
		t.Fatalf("Logs: %+v, %v", res, err)
	}
	want := []control.LogLine{
		{Src: "firmware", Line: "=== firmware start 1 ==="},
		{Src: "firmware", Line: "[I][app:029]: key <redacted>"},
		{Src: "firmware", Line: "last"},
	}
	if !reflect.DeepEqual(res.Lines, want) {
		t.Fatalf("lines %+v, want %+v", res.Lines, want)
	}
	raw, _ := s.Logs(context.Background(), control.LogsRequest{Source: "firmware", Tail: 2, Raw: true})
	if len(raw.Lines) != 2 || !strings.Contains(raw.Lines[0].Line, "\x1b[0;32m") || strings.Contains(raw.Lines[0].Line, testKey) {
		t.Fatalf("raw lines %+v, want ANSI kept and the key still redacted", raw.Lines)
	}
	all, _ := s.Logs(context.Background(), control.LogsRequest{Source: "all", Tail: 1})
	if len(all.Lines) != 2 || all.Lines[0].Src != "firmware" || all.Lines[1].Src != "sim" || all.Lines[0].Line != "last" {
		t.Fatalf("all/tail 1 = %+v, want the last line of firmware, then of sim", all.Lines)
	}
	sim, _ := s.Logs(context.Background(), control.LogsRequest{Source: "sim", Tail: 5})
	if len(sim.Lines) != 1 || sim.Lines[0].Src != "sim" {
		t.Fatalf("sim = %+v", sim.Lines)
	}
	for _, l := range append(append(res.Lines, all.Lines...), sim.Lines...) {
		if strings.Contains(l.Line, "BUILD SECRET") {
			t.Fatalf("build.log leaked: %+v", l)
		}
	}
}

func TestLogsMissingFileAndBadRequests(t *testing.T) {
	s, _ := logSup(t)
	res, err := s.Logs(context.Background(), control.LogsRequest{Source: "all", Tail: 10})
	if err != nil || res.Lines == nil || len(res.Lines) != 0 {
		t.Fatalf("missing files: %+v, %v, want an empty (non-nil) list", res, err)
	}
	for name, r := range map[string]control.LogsRequest{
		"source": {Source: "build", Tail: 10},
		"empty":  {Tail: 10},
		"zero":   {Source: "sim", Tail: 0},
		"big":    {Source: "sim", Tail: 10001},
	} {
		if _, err := s.Logs(context.Background(), r); errCode(err) != "invalid_argument" {
			t.Errorf("%s: %v, want invalid_argument", name, err)
		}
	}
	// A directory where the log file should be: logs_unreadable (exit 2).
	if err := os.Mkdir(s.paths.FirmwareLog(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Logs(context.Background(), control.LogsRequest{Source: "firmware", Tail: 5}); errCode(err) != "logs_unreadable" {
		t.Fatalf("unreadable log: %v, want logs_unreadable", err)
	}
}

// followed runs FollowLogs in the background and collects what it sends.
type followed struct {
	mu    sync.Mutex
	lines []control.LogLine
	stop  bool
	done  chan error
}

func startFollow(t *testing.T, s *supervisor, r control.LogsRequest) (*followed, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f := &followed{done: make(chan error, 1)}
	go func() {
		f.done <- s.FollowLogs(ctx, r, func(v any) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			switch x := v.(type) {
			case control.LogLine:
				f.lines = append(f.lines, x)
			case control.StoppedLine:
				f.stop = true
			}
			return nil
		})
	}()
	t.Cleanup(cancel)
	return f, cancel
}

// result waits up to 5 s for FollowLogs to return.
func (f *followed) result() (err error, ok bool) {
	tm := realClock.NewTimer(5 * time.Second)
	defer tm.Stop()
	select {
	case err := <-f.done:
		return err, true
	case <-tm.C():
		return nil, false
	}
}

func (f *followed) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, l := range f.lines {
		out = append(out, l.Src+":"+l.Line)
	}
	return out
}

func (f *followed) waitTexts(t *testing.T, want ...string) {
	t.Helper()
	eventually(t, fmt.Sprintf("log lines %q", want), func() bool { return reflect.DeepEqual(f.texts(), want) })
}

// tick lets the follower's 200 ms poll fire once and waits until it sleeps again.
func tick(clk *clock.Manual) {
	clk.BlockUntil(1)
	clk.Advance(logPollEvery)
	clk.BlockUntil(1)
}

func TestFollowLogsDeliversGrowthPartialLinesAndTruncation(t *testing.T) {
	s, clk := logSup(t)
	fw := s.paths.FirmwareLog()
	writeFile(t, fw, "one\ntwo\npart")
	f, _ := startFollow(t, s, control.LogsRequest{Source: "firmware", Tail: 100})
	// The tail does not include the unfinished line.
	f.waitTexts(t, "firmware:one", "firmware:two")

	appendFile(t, fw, "ial\nthree\n"+"\x1b[0;31mred "+testKey+"\x1b[0m\nfour")
	tick(clk)
	f.waitTexts(t, "firmware:one", "firmware:two", "firmware:partial", "firmware:three", "firmware:red <redacted>")

	// Nothing new: nothing sent, and the unfinished "four" is held back.
	tick(clk)
	if got := len(f.texts()); got != 5 {
		t.Fatalf("%d lines after an idle poll, want 5", got)
	}
	appendFile(t, fw, "!\n")
	tick(clk)
	f.waitTexts(t, "firmware:one", "firmware:two", "firmware:partial", "firmware:three", "firmware:red <redacted>", "firmware:four!")

	// A shorter file was truncated: reading restarts at its beginning.
	writeFile(t, fw, "new\n")
	tick(clk)
	eventually(t, "the line after the truncation", func() bool {
		got := f.texts()
		return len(got) == 7 && got[6] == "firmware:new"
	})

	// The instance stops: one last poll, the stopped line, a nil return.
	appendFile(t, fw, "bye\n")
	close(s.stopped)
	if err, ok := f.result(); !ok {
		t.Fatal("FollowLogs did not return after the instance stopped")
	} else if err != nil {
		t.Fatalf("FollowLogs returned %v after the stop, want nil", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stop || f.lines[len(f.lines)-1].Line != "bye" {
		t.Fatalf("stopped %v, last line %+v, want the final poll's line and then stopped", f.stop, f.lines[len(f.lines)-1])
	}
}

func TestFollowLogsFileAppearsLaterAndCtxEnds(t *testing.T) {
	s, clk := logSup(t)
	f, cancel := startFollow(t, s, control.LogsRequest{Source: "all", Tail: 10, Raw: true})
	tick(clk)
	if len(f.texts()) != 0 {
		t.Fatalf("lines from files that do not exist: %q", f.texts())
	}
	writeFile(t, s.paths.SilLog(), "{\"msg\":\"x\"}\n")
	writeFile(t, s.paths.FirmwareLog(), "\x1b[0mraw\n")
	tick(clk)
	f.waitTexts(t, "firmware:\x1b[0mraw", "sim:{\"msg\":\"x\"}")
	cancel()
	if err, ok := f.result(); !ok {
		t.Fatal("FollowLogs did not return after its context ended")
	} else if err == nil {
		t.Fatal("FollowLogs returned nil after the request ended, want the context error")
	}
}

func TestFollowLogsCountsAsAStream(t *testing.T) {
	s, _ := logSup(t)
	for i := 0; i < 16; i++ {
		f, _ := startFollow(t, s, control.LogsRequest{Source: "firmware", Tail: 1})
		_ = f
	}
	eventually(t, "16 follow streams", func() bool { return s.streams.Load() == 16 })
	err := s.FollowLogs(context.Background(), control.LogsRequest{Source: "firmware", Tail: 1}, func(any) error { return nil })
	if errCode(err) != "too_many_streams" {
		t.Fatalf("the 17th stream: %v, want too_many_streams", err)
	}
}

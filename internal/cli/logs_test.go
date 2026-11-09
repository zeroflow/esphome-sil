package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/control"
)

func logsResult() control.LogsResult {
	return control.LogsResult{Source: "firmware", Lines: []control.LogLine{
		{Src: "firmware", Line: "=== firmware start 1 ==="},
		{Src: "firmware", Line: "\x1b[0;32mgreen\x1b[0m"},
	}}
}

// `sil logs` prints the LogsResult as one JSON value and asks for the defaults.
func TestLogsPrintsOneJSONValue(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{logs: logsResult()}
	liveInstance(t, "l", fc)
	r := run(t, "logs", "-i", "l")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	var got control.LogsResult
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || !reflect.DeepEqual(got, logsResult()) {
		t.Fatalf("stdout %q (%v), want the LogsResult", r.stdout, err)
	}
	if want := []control.LogsRequest{{Source: "firmware", Tail: 100}}; !reflect.DeepEqual(fc.logsReqs, want) {
		t.Fatalf("request %+v, want the defaults %+v", fc.logsReqs, want)
	}
}

func TestLogsFlagsReachTheRequest(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{logs: logsResult()}
	liveInstance(t, "l", fc)
	if r := run(t, "logs", "-i", "l", "--source", "all", "--tail", "7"); r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	if want := (control.LogsRequest{Source: "all", Tail: 7}); fc.logsReqs[0] != want {
		t.Fatalf("request %+v, want %+v", fc.logsReqs[0], want)
	}
}

func TestLogsArgumentErrors(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{logs: logsResult()}
	liveInstance(t, "l", fc)
	for name, args := range map[string][]string{
		"unknown source": {"--source", "nope"},
		"build source":   {"--source", "build"},
		"tail zero":      {"--tail", "0"},
		"tail too big":   {"--tail", "10001"},
		"tail negative":  {"--tail", "-3"},
	} {
		t.Run(name, func(t *testing.T) {
			r := run(t, append([]string{"logs", "-i", "l"}, args...)...)
			if r.exit != 1 || r.errCode(t) != "invalid_argument" {
				t.Fatalf("exit %d code %q (%s), want exit 1 invalid_argument", r.exit, r.errCode(t), r.stdout)
			}
		})
	}
	if len(fc.logsReqs) != 0 {
		t.Fatalf("invalid arguments reached the instance: %+v", fc.logsReqs)
	}
}

// --raw prints the bare lines, ANSI kept, and no JSON.
func TestLogsRaw(t *testing.T) {
	cliEnv(t)
	liveInstance(t, "l", &fakeControl{logs: logsResult()})
	var so, se bytes.Buffer
	if exit := Execute([]string{"logs", "-i", "l", "--raw"}, &so, &se); exit != 0 {
		t.Fatalf("exit %d: %s", exit, se.String())
	}
	if want := "=== firmware start 1 ===\n\x1b[0;32mgreen\x1b[0m\n"; so.String() != want {
		t.Fatalf("stdout %q, want %q", so.String(), want)
	}
}

func TestLogsFollowStreamsUntilStopped(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{followLogs: func(_ context.Context, send func(any) error) error {
		for _, l := range []control.LogLine{{Src: "firmware", Line: "a"}, {Src: "sim", Line: "\x1b[0mb"}} {
			if err := send(l); err != nil {
				return err
			}
		}
		return send(control.NewStoppedLine(time.Second))
	}}
	liveInstance(t, "l", fc)

	var so, se bytes.Buffer
	if exit := Execute([]string{"logs", "-i", "l", "--follow", "--source", "all"}, &so, &se); exit != 0 {
		t.Fatalf("exit %d: %s", exit, se.String())
	}
	lines := strings.Split(strings.TrimSpace(so.String()), "\n")
	if len(lines) != 3 || lines[0] != `{"src":"firmware","line":"a"}` || !control.IsStoppedLine([]byte(lines[2])) {
		t.Fatalf("stdout %q, want two {src,line} lines and the stopped line", so.String())
	}
	if fc.logsReqs[0].Source != "all" || fc.logsReqs[0].Tail != 100 {
		t.Fatalf("request %+v", fc.logsReqs[0])
	}

	// With --raw the same stream prints bare lines (colour kept) and nothing for the stopped line.
	so.Reset()
	if exit := Execute([]string{"logs", "-i", "l", "--follow", "--raw"}, &so, &se); exit != 0 {
		t.Fatalf("raw follow exit %d: %s", exit, se.String())
	}
	if want := "a\n\x1b[0mb\n"; so.String() != want {
		t.Fatalf("raw follow stdout %q, want %q", so.String(), want)
	}
}

func TestLogsFollowDroppedStreamIsControlUnreachable(t *testing.T) {
	cliEnv(t)
	fc := &fakeControl{followLogs: func(_ context.Context, send func(any) error) error {
		return send(control.LogLine{Src: "firmware", Line: "a"})
	}}
	liveInstance(t, "l", fc)
	var so, se bytes.Buffer
	if exit := Execute([]string{"logs", "-i", "l", "--follow"}, &so, &se); exit != 2 || !strings.Contains(so.String(), `"control_unreachable"`) {
		t.Fatalf("exit %d stdout %q, want exit 2 control_unreachable", exit, so.String())
	}
}

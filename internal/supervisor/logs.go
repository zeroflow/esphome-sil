package supervisor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/control"
)

const (
	// logPollEvery is how often a followed log file is looked at (no inotify: no new dependency).
	logPollEvery = 200 * time.Millisecond
	// logBlock is the block size of the backwards tail read and the longest line that is served
	// whole; a longer line is cut.
	logBlock = 64 << 10
	// logScanMax bounds how much of a file one tail request reads.
	logScanMax = 8 << 20
	// logChunk is how much new data one follow poll reads at most.
	logChunk = 1 << 20
	// redacted replaces the instance's API key in every served line.
	redacted = "<redacted>"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes ANSI colour sequences (ESC [ digits and semicolons m) and nothing else.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// tailResult is the end of a log file.
type tailResult struct {
	lines []string
	// partial: the last of lines has no newline yet (the writer is in the middle of it).
	partial bool
	// next is the offset a follower continues from: the file size, or the start of the partial
	// last line.
	next int64
}

// tailLines returns the last n lines of the file at path, with and without a trailing newline.
// A file that does not exist or is empty has no lines.
func tailLines(path string, n int) ([]string, error) {
	r, err := readTail(path, n)
	return r.lines, err
}

// readTail reads backwards in 64 KiB blocks until it holds n complete lines (or 8 MiB, or the
// start of the file). A CR before the LF is dropped and a line longer than 64 KiB is cut.
func readTail(path string, n int) (tailResult, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return tailResult{}, nil
	}
	if err != nil {
		return tailResult{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return tailResult{}, err
	}
	size := fi.Size()
	if size == 0 || n < 1 {
		return tailResult{next: size}, nil
	}
	var buf []byte
	pos, newlines := size, 0
	for pos > 0 && newlines <= n && int64(len(buf)) < logScanMax {
		step := int64(logBlock)
		if step > pos {
			step = pos
		}
		pos -= step
		blk := make([]byte, step)
		if _, err := f.ReadAt(blk, pos); err != nil && !errors.Is(err, io.EOF) {
			return tailResult{}, err
		}
		newlines += bytes.Count(blk, []byte{'\n'})
		buf = append(blk, buf...)
	}
	res := tailResult{next: size}
	text := buf
	if bytes.HasSuffix(text, []byte{'\n'}) {
		text = text[:len(text)-1]
	} else {
		res.partial = true
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			res.next = pos + int64(i) + 1
		} else {
			res.next = pos
		}
	}
	parts := bytes.Split(text, []byte{'\n'})
	if pos > 0 && len(parts) > 1 {
		parts = parts[1:] // the first one starts before the block
	}
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	for _, p := range parts {
		p = bytes.TrimSuffix(p, []byte{'\r'})
		if len(p) > logBlock {
			p = p[:logBlock]
		}
		res.lines = append(res.lines, string(p))
	}
	return res, nil
}

// logSource is one served file: the source name and its path. The paths come from instance.Paths
// only; nothing from a request is ever part of a path, and build.log is not in the list.
type logSource struct {
	src, path string
}

func (s *supervisor) logSources(source string) []logSource {
	fw := logSource{control.LogSourceFirmware, s.paths.FirmwareLog()}
	sim := logSource{control.LogSourceSim, s.paths.SilLog()}
	switch source {
	case control.LogSourceFirmware:
		return []logSource{fw}
	case control.LogSourceSim:
		return []logSource{sim}
	}
	return []logSource{fw, sim}
}

// clean makes one file line fit to be served: ANSI colour removed unless raw, the instance's API
// key replaced by <redacted> always.
func (s *supervisor) clean(line string, raw bool) string {
	if !raw {
		line = stripANSI(line)
	}
	if s.id.Key != "" {
		line = strings.ReplaceAll(line, s.id.Key, redacted)
	}
	return line
}

func logsUnreadable(path string, err error) error {
	return apperr.Infra("logs_unreadable", "cannot read the log file", map[string]any{"path": path, "error": err.Error()})
}

// Logs implements control.Control (CLI-04): the last r.Tail lines of each selected log, firmware
// first.
func (s *supervisor) Logs(_ context.Context, r control.LogsRequest) (control.LogsResult, error) {
	if err := r.Validate(); err != nil {
		return control.LogsResult{}, err
	}
	res := control.LogsResult{Source: r.Source, Lines: []control.LogLine{}}
	for _, src := range s.logSources(r.Source) {
		t, err := readTail(src.path, r.Tail)
		if err != nil {
			return control.LogsResult{}, logsUnreadable(src.path, err)
		}
		for _, l := range t.lines {
			res.Lines = append(res.Lines, control.LogLine{Src: src.src, Line: s.clean(l, r.Raw)})
		}
	}
	return res, nil
}

// follower tails one file from a byte offset, line by line.
type follower struct {
	logSource
	off   int64
	carry []byte // an unterminated line read so far
}

// poll reads what was appended since the last call and returns its complete lines. A file that
// got shorter was truncated or replaced: it is read again from its beginning.
func (f *follower) poll() ([]string, error) {
	fi, err := os.Stat(f.path)
	if errors.Is(err, os.ErrNotExist) {
		f.off, f.carry = 0, nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if fi.Size() < f.off {
		f.off, f.carry = 0, nil
	}
	if fi.Size() == f.off {
		return nil, nil
	}
	file, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	for f.off < fi.Size() {
		chunk := fi.Size() - f.off
		if chunk > logChunk {
			chunk = logChunk
		}
		data := make([]byte, chunk)
		n, err := file.ReadAt(data, f.off)
		if err != nil && !errors.Is(err, io.EOF) {
			return lines, err
		}
		data = append(f.carry, data[:n]...)
		f.off += int64(n)
		f.carry = nil
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			lines = append(lines, string(cutLine(bytes.TrimSuffix(data[:i], []byte{'\r'}))))
			data = data[i+1:]
		}
		if len(data) > logBlock { // a line that long without a newline is served cut
			lines = append(lines, string(cutLine(data)))
			data = nil
		}
		f.carry = append([]byte(nil), data...)
		if n == 0 {
			break
		}
	}
	return lines, nil
}

func cutLine(b []byte) []byte {
	if len(b) > logBlock {
		return b[:logBlock]
	}
	return b
}

// FollowLogs implements control.Control (CLI-04): the tail, then every new line as the files
// grow (polled every 200 ms on the supervisor clock), then the stopped line when the instance
// stops. It counts as one of the instance's streams.
func (s *supervisor) FollowLogs(ctx context.Context, r control.LogsRequest, send func(v any) error) error {
	if err := r.Validate(); err != nil {
		return err
	}
	release, err := s.acquireStream()
	if err != nil {
		return err
	}
	defer release()

	var fols []*follower
	for _, src := range s.logSources(r.Source) {
		t, err := readTail(src.path, r.Tail)
		if err != nil {
			return logsUnreadable(src.path, err)
		}
		lines := t.lines
		if t.partial && len(lines) > 0 {
			lines = lines[:len(lines)-1] // its end arrives with the next poll
		}
		for _, l := range lines {
			if err := send(control.LogLine{Src: src.src, Line: s.clean(l, r.Raw)}); err != nil {
				return err
			}
		}
		fols = append(fols, &follower{logSource: src, off: t.next})
	}

	// The wait ends when the instance stops: the final poll then catches what the firmware wrote
	// while it was being stopped.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.stopped:
			cancel()
		case <-wctx.Done():
		}
	}()
	pollAll := func() error {
		for _, f := range fols {
			lines, err := f.poll()
			for _, l := range lines {
				if serr := send(control.LogLine{Src: f.src, Line: s.clean(l, r.Raw)}); serr != nil {
					return serr
				}
			}
			if err != nil {
				return logsUnreadable(f.path, err)
			}
		}
		return nil
	}
	for {
		if err := pollAll(); err != nil {
			return err
		}
		if err := s.clk.Sleep(wctx, logPollEvery); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := pollAll(); err != nil {
				return err
			}
			return send(control.NewStoppedLine(s.clk.Now()))
		}
	}
}

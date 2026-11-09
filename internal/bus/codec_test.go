package bus

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// vectorRow is one non-comment row of protocol/vectors/frames.tsv.
type vectorRow struct {
	name, kind, line, expect string
}

// repoRoot walks up from the test's working directory to the directory with go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// unescapePct turns %HH into the byte; a % without two hex digits is a bug in the vector file.
func unescapePct(t *testing.T, s string) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			sb.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			t.Fatalf("bad %%-escape at %d in vector line %q", i, s)
		}
		b, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			t.Fatalf("bad %%-escape at %d in vector line %q", i, s)
		}
		sb.WriteByte(byte(b))
		i += 2
	}
	return sb.String()
}

func loadVectors(t *testing.T) []vectorRow {
	t.Helper()
	path := filepath.Join(repoRoot(t), "protocol", "vectors", "frames.tsv")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []vectorRow
	seen := map[string]bool{}
	for n, ln := range strings.Split(string(raw), "\n") {
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		cols := strings.Split(ln, "\t")
		if len(cols) != 4 {
			t.Fatalf("%s:%d: want 4 tab-separated columns, got %d", path, n+1, len(cols))
		}
		r := vectorRow{cols[0], cols[1], unescapePct(t, cols[2]), cols[3]}
		if seen[r.name] {
			t.Fatalf("%s:%d: duplicate row name %q", path, n+1, r.name)
		}
		seen[r.name] = true
		switch r.kind {
		case "valid", "invalid", "encode":
		default:
			t.Fatalf("%s:%d: unknown row kind %q", path, n+1, r.kind)
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("no vector rows")
	}
	return rows
}

type tupleField struct {
	key, typ, val string
}

func parseTuple(t *testing.T, tuple string) []tupleField {
	t.Helper()
	var out []tupleField
	for _, part := range strings.Split(tuple, ";") {
		eq := strings.IndexByte(part, '=')
		if eq < 1 || len(part) < eq+3 || part[eq+2] != ':' {
			t.Fatalf("bad tuple field %q in %q", part, tuple)
		}
		out = append(out, tupleField{part[:eq], string(part[eq+1]), part[eq+3:]})
	}
	return out
}

// frameFromTuple builds a frame in tuple order through the public builders.
func frameFromTuple(t *testing.T, tuple string) Frame {
	t.Helper()
	var f Frame
	for _, tf := range parseTuple(t, tuple) {
		switch tf.typ {
		case "s":
			f = f.Str(tf.key, tf.val)
		case "n":
			v, err := strconv.ParseFloat(tf.val, 32)
			if err != nil {
				t.Fatalf("tuple number %q: %v", tf.val, err)
			}
			f = f.Num(tf.key, v)
		case "i":
			v, err := strconv.ParseInt(tf.val, 10, 64)
			if err != nil {
				t.Fatalf("tuple int %q: %v", tf.val, err)
			}
			f = f.Int(tf.key, v)
		default:
			t.Fatalf("bad tuple type %q", tf.typ)
		}
	}
	return f
}

// checkTuple compares a decoded frame with a tuple, by value and in order.
func checkTuple(t *testing.T, f Frame, tuple string) {
	t.Helper()
	want := parseTuple(t, tuple)
	keys := f.Keys()
	if len(keys) != len(want) {
		t.Fatalf("got %d fields %v, want %d (%s)", len(keys), keys, len(want), tuple)
	}
	for i, tf := range want {
		if keys[i] != tf.key {
			t.Fatalf("field %d: key %q, want %q", i, keys[i], tf.key)
		}
		switch tf.typ {
		case "s":
			got, ok := f.GetStr(tf.key)
			if !ok || got != tf.val {
				t.Fatalf("%s: GetStr = %q,%v, want %q", tf.key, got, ok, tf.val)
			}
		case "n":
			w, err := strconv.ParseFloat(tf.val, 32)
			if err != nil {
				t.Fatalf("tuple number %q: %v", tf.val, err)
			}
			got, ok := f.GetF32(tf.key)
			if !ok || got != float32(w) {
				t.Fatalf("%s: GetF32 = %v,%v, want %v", tf.key, got, ok, float32(w))
			}
		case "i":
			w, err := strconv.ParseInt(tf.val, 10, 64)
			if err != nil {
				t.Fatalf("tuple int %q: %v", tf.val, err)
			}
			got, ok := f.GetInt(tf.key)
			if !ok || got != w {
				t.Fatalf("%s: GetInt = %d,%v, want %d", tf.key, got, ok, w)
			}
		}
	}
}

func TestVectors(t *testing.T) {
	for _, r := range loadVectors(t) {
		t.Run(r.name, func(t *testing.T) {
			switch r.kind {
			case "valid":
				f, err := Decode([]byte(r.line))
				if err != nil {
					t.Fatalf("Decode(%q): %v", r.line, err)
				}
				checkTuple(t, f, r.expect)
			case "invalid":
				_, err := Decode([]byte(r.line))
				var de *DecodeError
				if !errors.As(err, &de) {
					t.Fatalf("Decode(%q) error = %v, want *DecodeError class %s", r.line, err, r.expect)
				}
				if de.Class != r.expect {
					t.Fatalf("Decode(%q) class = %s (%v), want %s", r.line, de.Class, de, r.expect)
				}
			case "encode":
				got := string(Encode(frameFromTuple(t, r.expect)))
				if got != r.line+"\n" {
					t.Fatalf("Encode = %q, want %q", got, r.line+"\n")
				}
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	for _, r := range loadVectors(t) {
		if r.kind != "valid" {
			continue
		}
		t.Run(r.name, func(t *testing.T) {
			enc := Encode(frameFromTuple(t, r.expect))
			if len(enc) > MaxLine {
				t.Fatalf("encoded line is %d bytes, over MaxLine", len(enc))
			}
			if enc[len(enc)-1] != '\n' {
				t.Fatal("Encode must end with a newline")
			}
			f, err := Decode(enc[:len(enc)-1])
			if err != nil {
				t.Fatalf("Decode(Encode(f)): %v", err)
			}
			checkTuple(t, f, r.expect)
		})
	}
}

// GetNum keeps the 01-02 decimal normalization: a float32 on the wire reads back
// as the shortest decimal of that float32, so exact duty assertions hold.
func TestGetNumNormalization(t *testing.T) {
	for _, lit := range []string{"0.4", "0.08", "0.2", "0.6", "0.5", "1"} {
		f, err := Decode([]byte(`{"k":"out","ch":"port1.duty","v":` + lit + `}`))
		if err != nil {
			t.Fatalf("%s: %v", lit, err)
		}
		want, _ := strconv.ParseFloat(lit, 64)
		got, ok := f.GetNum("v")
		if !ok || got != want {
			t.Errorf("GetNum(v=%s) = %v,%v, want exactly %v", lit, got, ok, want)
		}
	}
	// And through the encoder: Num(0.4) crosses the wire as "0.4", not 0.4000000059604645.
	enc := string(Encode(NewFrame("out").Num("v", 0.4)))
	if enc != "{\"k\":\"out\",\"v\":0.4}\n" {
		t.Errorf("Encode(Num(0.4)) = %q", enc)
	}
}

func TestNonFiniteIsRefused(t *testing.T) {
	for name, v := range map[string]float64{
		"nan":       math.NaN(),
		"+inf":      math.Inf(1),
		"-inf":      math.Inf(-1),
		"too_big":   1e300,
		"too_small": -1e300,
	} {
		f := NewFrame("out").Num("v", v)
		if got := f.Keys(); len(got) != 1 || got[0] != "k" {
			t.Errorf("%s: Num left keys %v, want only k", name, got)
		}
		line := Encode(f)
		if strings.ContainsAny(string(line), "NI") {
			t.Errorf("%s: encoded line %q mentions NaN/Inf", name, line)
		}
	}
}

func TestEncodeSanitizesStrings(t *testing.T) {
	line := Encode(NewFrame("err").Str("msg", "a\tb\xc3\xa9\x7f\n"))
	want := "{\"k\":\"err\",\"msg\":\"a?b????\"}\n"
	if string(line) != want {
		t.Fatalf("Encode = %q, want %q", line, want)
	}
	if _, err := Decode(line[:len(line)-1]); err != nil {
		t.Fatalf("a sanitized line must decode: %v", err)
	}
}

func TestGetIntIsStrict(t *testing.T) {
	f, err := Decode([]byte(`{"k":"x","a":1.5,"b":1e3,"c":9223372036854775808,"d":-7,"e":"5"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b", "c", "e"} {
		if v, ok := f.GetInt(key); ok {
			t.Errorf("GetInt(%s) = %d, want not ok", key, v)
		}
	}
	if v, ok := f.GetInt("d"); !ok || v != -7 {
		t.Errorf("GetInt(d) = %d,%v, want -7", v, ok)
	}
}

func TestDecodeErrorShape(t *testing.T) {
	line := `{"k":"x","v":01}`
	_, err := Decode([]byte(line))
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("error %v is not a *DecodeError", err)
	}
	if de.Class != "bad_number" || de.Offset < 0 || de.Offset > len(line) {
		t.Errorf("DecodeError = %+v, want class bad_number and an offset inside the line", de)
	}
	if !strings.Contains(de.Error(), "bad_number") {
		t.Errorf("Error() = %q does not name the class", de.Error())
	}
}

// Every prefix and every single-byte deletion of every vector line must decode or
// return a *DecodeError, never panic.
func TestDecodeNeverPanics(t *testing.T) {
	check := func(b []byte) {
		if _, err := Decode(b); err != nil {
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Fatalf("Decode(%q) returned %T, want *DecodeError", b, err)
			}
		}
	}
	for _, r := range loadVectors(t) {
		if r.kind == "encode" {
			continue
		}
		line := []byte(r.line)
		for i := 0; i <= len(line); i++ {
			check(line[:i])
			if i < len(line) {
				check(append(append([]byte{}, line[:i]...), line[i+1:]...))
			}
		}
	}
}

// Package bus implements the sim-bus: flat NDJSON frames over a Unix socket.
package bus

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Proto is the sim-bus protocol version.
	Proto = 1
	// MaxLine is the longest line, including the newline.
	MaxLine = 4096
)

const (
	kindStr = 's'
	kindNum = 'n'
	kindInt = 'i'
)

type field struct {
	key  string
	kind byte
	raw  string // string value, or the number literal as written
}

// Frame is one flat bus message: ordered keys, string and number values.
// Numbers are kept as the literal text so nothing is rounded on the way.
type Frame struct {
	fields []field
}

// NewFrame starts a frame with its kind in the first key "k".
func NewFrame(kind string) Frame {
	return Frame{}.Str("k", kind)
}

func (f Frame) with(fd field) Frame {
	nf := make([]field, len(f.fields)+1)
	copy(nf, f.fields)
	nf[len(f.fields)] = fd
	return Frame{fields: nf}
}

// Str appends a string field.
func (f Frame) Str(key, v string) Frame { return f.with(field{key, kindStr, v}) }

// Num appends a float32 field written as its shortest decimal. NaN and the
// infinities have no wire form, so the field is omitted for them.
func (f Frame) Num(key string, v float64) Frame {
	f32 := float32(v)
	if f32 != f32 || f32 > 3.4028235e38 || f32 < -3.4028235e38 {
		return f
	}
	return f.with(field{key, kindNum, strconv.FormatFloat(float64(f32), 'g', -1, 32)})
}

// Int appends an integer field.
func (f Frame) Int(key string, v int64) Frame {
	return f.with(field{key, kindInt, strconv.FormatInt(v, 10)})
}

func (f Frame) lookup(key string) (field, bool) {
	for _, fd := range f.fields {
		if fd.key == key {
			return fd, true
		}
	}
	return field{}, false
}

// Kind is the value of "k", or "" when absent.
func (f Frame) Kind() string {
	s, _ := f.GetStr("k")
	return s
}

// GetStr returns a string field.
func (f Frame) GetStr(key string) (string, bool) {
	fd, ok := f.lookup(key)
	if !ok || fd.kind != kindStr {
		return "", false
	}
	return fd.raw, true
}

// GetInt returns an integer field (a number written without fraction or exponent).
func (f Frame) GetInt(key string) (int64, bool) {
	fd, ok := f.lookup(key)
	if !ok || fd.kind == kindStr {
		return 0, false
	}
	n, err := strconv.ParseInt(fd.raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// GetF32 returns a number field as the float32 that crossed the wire. Use it
// for value-level comparisons with the firmware's float.
func (f Frame) GetF32(key string) (float32, bool) {
	fd, ok := f.lookup(key)
	if !ok || fd.kind == kindStr {
		return 0, false
	}
	v, err := strconv.ParseFloat(fd.raw, 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}

// GetNum returns a number field as the shortest decimal of its wire float32,
// parsed at float64. A firmware duty of 0.4f therefore reads as exactly 0.4
// (not 0.4000000059604645), and exact comparisons against the commanded
// decimal hold in the engine and everywhere downstream.
func (f Frame) GetNum(key string) (float64, bool) {
	v, ok := f.GetF32(key)
	if !ok {
		return 0, false
	}
	d, err := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'g', -1, 32), 64)
	if err != nil {
		return 0, false
	}
	return d, true
}

// Keys lists the keys in wire order.
func (f Frame) Keys() []string {
	keys := make([]string, len(f.fields))
	for i, fd := range f.fields {
		keys[i] = fd.key
	}
	return keys
}

// String is the canonical tuple form, e.g. k=s:out;ch=s:port1.duty;v=n:0.4;t=i:1000.
func (f Frame) String() string {
	parts := make([]string, len(f.fields))
	for i, fd := range f.fields {
		parts[i] = fmt.Sprintf("%s=%c:%s", fd.key, fd.kind, fd.raw)
	}
	return strings.Join(parts, ";")
}

func writeQuoted(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteByte(s[i])
	}
	sb.WriteByte('"')
}

// Encode renders the frame as one line including the trailing newline.
func Encode(f Frame) []byte {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, fd := range f.fields {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeQuoted(&sb, fd.key)
		sb.WriteByte(':')
		if fd.kind == kindStr {
			writeQuoted(&sb, fd.raw)
		} else {
			sb.WriteString(fd.raw)
		}
	}
	sb.WriteString("}\n")
	return []byte(sb.String())
}

// Decode parses one line (without its newline). It is lenient: any flat
// object of string and number values is accepted; nesting, arrays and
// literals are errors. Plan 01-06 makes the grammar strict.
func Decode(line []byte) (Frame, error) {
	p := parser{s: string(line)}
	return p.object()
}

type parser struct {
	s string
	i int
}

func (p *parser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *parser) fail(msg string) error {
	return fmt.Errorf("bus: bad frame at byte %d: %s", p.i, msg)
}

func (p *parser) object() (Frame, error) {
	var f Frame
	p.skip()
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return f, p.fail("expected '{'")
	}
	p.i++
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return f, p.end()
	}
	for {
		p.skip()
		key, err := p.str()
		if err != nil {
			return Frame{}, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return Frame{}, p.fail("expected ':'")
		}
		p.i++
		p.skip()
		if p.i >= len(p.s) {
			return Frame{}, p.fail("missing value")
		}
		switch c := p.s[p.i]; {
		case c == '"':
			v, err := p.str()
			if err != nil {
				return Frame{}, err
			}
			f = f.with(field{key, kindStr, v})
		case c == '-' || (c >= '0' && c <= '9'):
			fd, err := p.num(key)
			if err != nil {
				return Frame{}, err
			}
			f = f.with(fd)
		case c == '{' || c == '[':
			return Frame{}, p.fail("nesting is not allowed")
		default:
			return Frame{}, p.fail("value must be a string or a number")
		}
		p.skip()
		if p.i >= len(p.s) {
			return Frame{}, p.fail("unterminated object")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return f, p.end()
		default:
			return Frame{}, p.fail("expected ',' or '}'")
		}
	}
}

func (p *parser) end() error {
	p.skip()
	if p.i != len(p.s) {
		return p.fail("trailing characters")
	}
	return nil
}

func (p *parser) str() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", p.fail("expected string")
	}
	p.i++
	var sb strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return sb.String(), nil
		case c == '\\':
			if p.i+1 >= len(p.s) || (p.s[p.i+1] != '"' && p.s[p.i+1] != '\\') {
				return "", p.fail("unsupported escape")
			}
			sb.WriteByte(p.s[p.i+1])
			p.i += 2
		default:
			sb.WriteByte(c)
			p.i++
		}
	}
	return "", p.fail("unterminated string")
}

func (p *parser) num(key string) (field, error) {
	start := p.i
	isInt := true
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '.' || c == 'e' || c == 'E' {
			isInt = false
		} else if !(c == '-' || c == '+' || (c >= '0' && c <= '9')) {
			break
		}
		p.i++
	}
	raw := p.s[start:p.i]
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || ne.Err != strconv.ErrRange {
			return field{}, p.fail("bad number " + strconv.Quote(raw))
		}
	}
	kind := byte(kindNum)
	if isInt {
		kind = kindInt
	}
	return field{key, kind, raw}, nil
}

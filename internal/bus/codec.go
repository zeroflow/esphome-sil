// Package bus implements the sim-bus: flat NDJSON frames over a Unix socket.
package bus

import (
	"fmt"
	"math"
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
// infinities have no wire form, and neither has a float64 beyond the float32
// range (it would become an infinity), so the field is left out for all of
// them: a frame never carries NaN or Inf text. See protocol/PROTOCOL.md.
func (f Frame) Num(key string, v float64) Frame {
	f32 := float32(v)
	if math.IsNaN(float64(f32)) || math.IsInf(float64(f32), 0) {
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

// GetInt returns an integer field: a number whose text is -?[0-9]+ and fits in
// int64 (nothing with a fraction, an exponent or a plus sign).
func (f Frame) GetInt(key string) (int64, bool) {
	fd, ok := f.lookup(key)
	if !ok || fd.kind == kindStr || !isIntLiteral(fd.raw) {
		return 0, false
	}
	n, err := strconv.ParseInt(fd.raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func isIntLiteral(s string) bool {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
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

// The error classes of a rejected line. They are the vocabulary shared with the
// C++ codec and the "invalid" rows of protocol/vectors/frames.tsv.
const (
	ClassEmpty     = "empty"
	ClassTooLong   = "too_long"
	ClassNotObject = "not_object"
	ClassNested    = "nested"
	ClassBadKey    = "bad_key"
	ClassBadValue  = "bad_value"
	ClassBadEscape = "bad_escape"
	ClassBadChar   = "bad_char"
	ClassDupKey    = "dup_key"
	ClassMissingK  = "missing_k"
	ClassBadK      = "bad_k"
	ClassBadNumber = "bad_number"
	ClassTrailing  = "trailing"
)

// maxNumberLen is the longest number literal, in characters.
const maxNumberLen = 32

// DecodeError is returned by Decode for a line that breaks the grammar. Class is
// one of the Class constants; Offset is the byte where the problem was found.
type DecodeError struct {
	Class  string
	Offset int
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("bus: bad frame (%s) at byte %d", e.Class, e.Offset)
}

func writeQuoted(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 0x20 || c > 0x7e:
			sb.WriteByte('?') // outside printable ASCII: never emit a line the peer would reject
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
}

// Encode renders the frame as one line including the trailing newline. Bytes in
// strings outside printable ASCII are written as '?' so the line always decodes.
// Encode does not truncate: callers keep frames under MaxLine. A frame cannot
// hold NaN or Inf (Num leaves such a field out), so Encode never emits them.
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

// Decode parses one line (without its newline) under the strict grammar of
// protocol/PROTOCOL.md: one flat object, unique keys, string and number values
// only. It returns a *DecodeError whose Class names the first problem found.
// The length cap is checked before anything else and the scan is a single pass
// without recursion.
func Decode(line []byte) (Frame, error) {
	if len(line) == 0 {
		return Frame{}, &DecodeError{ClassEmpty, 0}
	}
	if len(line) > MaxLine-1 { // the newline counts toward MaxLine
		return Frame{}, &DecodeError{ClassTooLong, MaxLine - 1}
	}
	d := decoder{s: line}
	fields, err := d.object()
	if err != nil {
		return Frame{}, err
	}
	f := Frame{fields: fields}
	k, ok := f.lookup("k")
	switch {
	case !ok:
		return Frame{}, &DecodeError{ClassMissingK, 0}
	case k.kind != kindStr || k.raw == "":
		return Frame{}, &DecodeError{ClassBadK, 0}
	}
	return f, nil
}

type decoder struct {
	s []byte
	i int
}

func (d *decoder) fail(class string) error { return &DecodeError{class, d.i} }

func (d *decoder) skip() {
	for d.i < len(d.s) && d.s[d.i] == ' ' {
		d.i++
	}
}

func (d *decoder) object() ([]field, error) {
	var fields []field
	d.skip()
	if d.i >= len(d.s) || d.s[d.i] != '{' {
		return nil, d.fail(ClassNotObject)
	}
	d.i++
	d.skip()
	if d.i < len(d.s) && d.s[d.i] == '}' {
		d.i++
		return fields, d.end()
	}
	for {
		d.skip()
		key, err := d.key()
		if err != nil {
			return nil, err
		}
		for _, fd := range fields {
			if fd.key == key {
				return nil, d.fail(ClassDupKey)
			}
		}
		d.skip()
		if d.i >= len(d.s) || d.s[d.i] != ':' {
			return nil, d.fail(ClassNotObject)
		}
		d.i++
		d.skip()
		fd, err := d.value(key)
		if err != nil {
			return nil, err
		}
		fields = append(fields, fd)
		d.skip()
		if d.i >= len(d.s) {
			return nil, d.fail(ClassNotObject)
		}
		switch d.s[d.i] {
		case ',':
			d.i++
		case '}':
			d.i++
			return fields, d.end()
		default:
			return nil, d.fail(ClassNotObject)
		}
	}
}

func (d *decoder) end() error {
	d.skip()
	if d.i != len(d.s) {
		return d.fail(ClassTrailing)
	}
	return nil
}

// key reads a quoted key: [a-z][a-z0-9_]*.
func (d *decoder) key() (string, error) {
	if d.i >= len(d.s) {
		return "", d.fail(ClassNotObject)
	}
	if d.s[d.i] != '"' {
		return "", d.fail(ClassBadKey)
	}
	d.i++
	start := d.i
	for d.i < len(d.s) && d.s[d.i] != '"' {
		c := d.s[d.i]
		first := d.i == start
		if !(c >= 'a' && c <= 'z') && (first || !((c >= '0' && c <= '9') || c == '_')) {
			return "", d.fail(ClassBadKey)
		}
		d.i++
	}
	if d.i >= len(d.s) {
		return "", d.fail(ClassNotObject)
	}
	if d.i == start {
		return "", d.fail(ClassBadKey)
	}
	key := string(d.s[start:d.i])
	d.i++
	return key, nil
}

func (d *decoder) value(key string) (field, error) {
	if d.i >= len(d.s) {
		return field{}, d.fail(ClassNotObject)
	}
	switch c := d.s[d.i]; {
	case c == '"':
		v, err := d.str()
		if err != nil {
			return field{}, err
		}
		return field{key, kindStr, v}, nil
	case c == '{' || c == '[':
		return field{}, d.fail(ClassNested)
	case c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9'):
		return d.num(key)
	default:
		return field{}, d.fail(ClassBadValue)
	}
}

// str reads a quoted string: printable ASCII with only \" and \\ as escapes.
func (d *decoder) str() (string, error) {
	d.i++ // opening quote
	var sb strings.Builder
	for d.i < len(d.s) {
		c := d.s[d.i]
		switch {
		case c == '"':
			d.i++
			return sb.String(), nil
		case c == '\\':
			if d.i+1 >= len(d.s) || (d.s[d.i+1] != '"' && d.s[d.i+1] != '\\') {
				return "", d.fail(ClassBadEscape)
			}
			sb.WriteByte(d.s[d.i+1])
			d.i += 2
		case c < 0x20 || c > 0x7e:
			return "", d.fail(ClassBadChar)
		default:
			sb.WriteByte(c)
			d.i++
		}
	}
	return "", d.fail(ClassNotObject)
}

func isNumberByte(c byte) bool {
	return (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
}

// num reads a JSON number: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?, at
// most 32 characters, and within float32 range.
func (d *decoder) num(key string) (field, error) {
	start := d.i
	for d.i < len(d.s) && isNumberByte(d.s[d.i]) {
		d.i++
	}
	tok := string(d.s[start:d.i])
	if len(tok) > maxNumberLen || !validNumber(tok) {
		return field{}, &DecodeError{ClassBadNumber, start}
	}
	if _, err := strconv.ParseFloat(tok, 32); err != nil {
		return field{}, &DecodeError{ClassBadNumber, start} // out of float32 range
	}
	kind := byte(kindInt)
	if strings.ContainsAny(tok, ".eE") {
		kind = kindNum
	}
	return field{key, kind, tok}, nil
}

func validNumber(t string) bool {
	i, n := 0, len(t)
	digits := func() int {
		start := i
		for i < n && t[i] >= '0' && t[i] <= '9' {
			i++
		}
		return i - start
	}
	if i < n && t[i] == '-' {
		i++
	}
	switch {
	case i < n && t[i] == '0':
		i++
	case i < n && t[i] >= '1' && t[i] <= '9':
		digits()
	default:
		return false
	}
	if i < n && t[i] == '.' {
		i++
		if digits() == 0 {
			return false
		}
	}
	if i < n && (t[i] == 'e' || t[i] == 'E') {
		i++
		if i < n && (t[i] == '+' || t[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == n
}

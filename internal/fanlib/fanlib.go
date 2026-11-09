// Package fanlib loads fan parameter files from the fan library.
//
// A fan file is a flat TOML document with nine required keys. Loading is
// strict (D-14): an unknown key, a missing key or an out-of-range value stops
// the load with an Error naming the key and its location; nothing is ever
// defaulted or corrected silently.
package fanlib

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/zeroflow/esphome-sil/fans"
	"github.com/zeroflow/esphome-sil/internal/model"
)

// DefaultFan is the fan every port uses unless configured otherwise.
const DefaultFan = "arctic-p12-pwm-pst"

// EnvFanDir names the environment variable that replaces the embedded fan
// library with a directory, so fan files can be edited without rebuilding sil.
const EnvFanDir = "SIL_FAN_DIR"

// maxFileBytes caps how much of one fan file is read (T-01-08).
const maxFileBytes = 64 << 10

// Source is the fan library: the directory named by SIL_FAN_DIR when that is
// set, else the library embedded in the binary.
func Source() fs.FS {
	if dir := os.Getenv(EnvFanDir); dir != "" {
		return os.DirFS(dir)
	}
	return fans.FS
}

// Error is a located fan file problem. Line is 1-based and 0 when the problem
// has no single line (a missing key); Col is 1-based and 0 when unknown.
type Error struct {
	File      string
	Line, Col int
	Key, Msg  string
}

// Error renders "<file>:<line>:<col>: <msg>", omitting ":<col>" when Col is 0
// and ":<line>" when Line is 0.
func (e *Error) Error() string {
	loc := e.File
	if e.Line > 0 {
		loc += ":" + strconv.Itoa(e.Line)
		if e.Col > 0 {
			loc += ":" + strconv.Itoa(e.Col)
		}
	}
	return loc + ": " + e.Msg
}

// rawFan mirrors the file with pointer fields so a missing key is detectable.
type rawFan struct {
	Name      *string  `toml:"name"`
	Source    *string  `toml:"source"`
	MinDuty   *float64 `toml:"min_duty"`
	StartDuty *float64 `toml:"start_duty"`
	BelowMin  *string  `toml:"below_min"`
	MinRPM    *float64 `toml:"min_rpm"`
	MaxRPM    *float64 `toml:"max_rpm"`
	TauUp     *float64 `toml:"tau_up"`
	TauDown   *float64 `toml:"tau_down"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Load reads <id>.toml from the fan library and validates it.
func Load(id string) (model.Params, error) {
	if !idPattern.MatchString(id) || strings.Contains(id, "..") {
		return model.Params{}, fmt.Errorf("fanlib: invalid fan id %q", id)
	}
	return loadFrom(Source(), id, fileLabel(id))
}

// fileLabel is how an error names the file of id: fans/<id>.toml for the
// embedded library, the absolute path under SIL_FAN_DIR otherwise.
func fileLabel(id string) string {
	dir := os.Getenv(EnvFanDir)
	if dir == "" {
		return "fans/" + id + ".toml"
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(dir, id+".toml")
}

// IDs returns the sorted file stems of the *.toml files in Source().
func IDs() ([]string, error) {
	entries, err := fs.ReadDir(Source(), ".")
	if err != nil {
		return nil, fmt.Errorf("fanlib: list fan library: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".toml"))
	}
	sort.Strings(ids)
	return ids, nil
}

// loadFrom reads id+".toml" from fsys; file is the name used in errors.
func loadFrom(fsys fs.FS, id, file string) (model.Params, error) {
	f, err := fsys.Open(id + ".toml")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return model.Params{}, fmt.Errorf("fanlib: fan %q not found (looked for %s)", id, file)
		}
		return model.Params{}, fmt.Errorf("fanlib: fan %q: %w", id, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return model.Params{}, fmt.Errorf("fanlib: read %s: %w", file, err)
	}
	if len(data) > maxFileBytes {
		return model.Params{}, &Error{File: file, Msg: "file is larger than 64 KiB"}
	}

	var raw rawFan
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return model.Params{}, decodeError(file, err)
	}
	return validate(file, data, raw)
}

// decodeError turns a go-toml error into a located *Error.
func decodeError(file string, err error) error {
	var sm *toml.StrictMissingError
	if errors.As(err, &sm) && len(sm.Errors) > 0 {
		first := sm.Errors[0]
		line, col := first.Position()
		key := strings.Join(first.Key(), ".")
		return &Error{File: file, Line: line, Col: col, Key: key, Msg: "unknown field " + key}
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		line, col := de.Position()
		return &Error{File: file, Line: line, Col: col, Key: strings.Join(de.Key(), "."),
			Msg: strings.TrimPrefix(de.Error(), "toml: ")}
	}
	return &Error{File: file, Msg: err.Error()}
}

// validate applies the semantic rules in a fixed order; the first violation
// is reported at the line of the offending key.
func validate(file string, data []byte, r rawFan) (model.Params, error) {
	fail := func(key, format string, args ...any) error {
		return &Error{File: file, Line: keyLine(data, key), Key: key, Msg: fmt.Sprintf(format, args...)}
	}

	// All nine keys present.
	type present struct {
		key string
		ok  bool
	}
	for _, p := range []present{
		{"name", r.Name != nil}, {"source", r.Source != nil},
		{"min_duty", r.MinDuty != nil}, {"start_duty", r.StartDuty != nil},
		{"below_min", r.BelowMin != nil},
		{"min_rpm", r.MinRPM != nil}, {"max_rpm", r.MaxRPM != nil},
		{"tau_up", r.TauUp != nil}, {"tau_down", r.TauDown != nil},
	} {
		if !p.ok {
			return model.Params{}, &Error{File: file, Key: p.key, Msg: "missing required key " + p.key}
		}
	}
	if strings.TrimSpace(*r.Name) == "" {
		return model.Params{}, fail("name", "name must not be empty")
	}
	if strings.TrimSpace(*r.Source) == "" {
		return model.Params{}, fail("source", "source must not be empty")
	}
	if *r.BelowMin != "stop" && *r.BelowMin != "hold" {
		return model.Params{}, fail("below_min", "below_min must be \"stop\" or \"hold\" (got %q)", *r.BelowMin)
	}

	minDuty, startDuty := *r.MinDuty, *r.StartDuty
	minRPM, maxRPM := *r.MinRPM, *r.MaxRPM
	tauUp, tauDown := *r.TauUp, *r.TauDown

	for _, n := range []struct {
		key string
		v   float64
	}{
		{"min_duty", minDuty}, {"start_duty", startDuty},
		{"min_rpm", minRPM}, {"max_rpm", maxRPM},
		{"tau_up", tauUp}, {"tau_down", tauDown},
	} {
		if math.IsNaN(n.v) || math.IsInf(n.v, 0) {
			return model.Params{}, fail(n.key, "%s (%g) must be finite", n.key, n.v)
		}
	}
	switch {
	case !(minDuty >= 0 && minDuty < 1):
		return model.Params{}, fail("min_duty", "min_duty (%g) must be in [0, 1)", minDuty)
	case !(startDuty >= 0 && startDuty <= 1):
		return model.Params{}, fail("start_duty", "start_duty (%g) must be in [0, 1]", startDuty)
	case startDuty < minDuty:
		return model.Params{}, fail("start_duty", "start_duty (%g) must be >= min_duty (%g)", startDuty, minDuty)
	case minRPM < 0:
		return model.Params{}, fail("min_rpm", "min_rpm (%g) must be >= 0", minRPM)
	case minRPM > maxRPM:
		return model.Params{}, fail("min_rpm", "min_rpm (%g) must be <= max_rpm (%g)", minRPM, maxRPM)
	case !(maxRPM > 0):
		return model.Params{}, fail("max_rpm", "max_rpm (%g) must be > 0", maxRPM)
	case !(tauUp > 0):
		return model.Params{}, fail("tau_up", "tau_up must be > 0 (got %g)", tauUp)
	case !(tauDown > 0):
		return model.Params{}, fail("tau_down", "tau_down must be > 0 (got %g)", tauDown)
	}

	return model.Params{
		Name: *r.Name, Source: *r.Source,
		MinDuty: minDuty, StartDuty: startDuty, BelowMin: *r.BelowMin,
		MinRPM: minRPM, MaxRPM: maxRPM, TauUp: tauUp, TauDown: tauDown,
	}, nil
}

// keyLine returns the 1-based line of the first `key =` assignment in data,
// or 0 when there is none.
func keyLine(data []byte, key string) int {
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			return i + 1
		}
	}
	return 0
}

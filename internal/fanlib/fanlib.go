// Package fanlib loads fan parameter files from the fan library.
//
// There are two file formats, both strict. Schema 1 (no schema key, or
// schema = 1; see v1.go) is a flat TOML document with nine required keys, the
// v0.1.0 format. Schema 2 (schema = 2, see v2.go, which lists its keys and its
// rule order) has tables for the stop and start thresholds, the dynamics, the
// duty to rpm curves, the optional tach, datasheet and measured tables, and a
// per-value provenance table. A schema above 2 is refused as needing a newer
// sil. Loading is strict (D-14): an unknown key, a missing key or an
// out-of-range value stops the load with an Error naming the key and its
// location; nothing is ever defaulted or corrected silently. A file is read
// once per load and the result depends only on its bytes.
package fanlib

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	// The schema probe is lenient on purpose: it tolerates every other key, so
	// the format decides which decoder (and which strictness) applies.
	var probe struct {
		Schema *int `toml:"schema"`
	}
	if err := toml.Unmarshal(data, &probe); err != nil {
		return model.Params{}, decodeError(file, err)
	}
	if probe.Schema == nil || *probe.Schema == 1 {
		return loadV1(file, data)
	}
	switch n := *probe.Schema; {
	case n == 2:
		return loadV2(file, data)
	case n > 2:
		return model.Params{}, &Error{File: file, Line: locate(data, "", "schema"), Key: "schema",
			Msg: fmt.Sprintf("schema %d needs a newer sil (this sil reads schema 1 and 2)", n)}
	default:
		return model.Params{}, &Error{File: file, Line: locate(data, "", "schema"), Key: "schema",
			Msg: "schema must be 1 or 2"}
	}
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

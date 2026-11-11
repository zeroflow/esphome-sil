package fanlib

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// Schema 1 fan file: the flat v0.1.0 format with nine required keys, optionally
// preceded by `schema = 1`. This code moved here unchanged from fanlib.go; only
// Schema was added to rawFan and Params.Schema is set to 1.

// rawFan mirrors a schema 1 file with pointer fields so a missing key is detectable. Schema is
// accepted so that an explicit `schema = 1` decodes; the probe in loadFrom has checked its value.
type rawFan struct {
	Schema    *int     `toml:"schema"`
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

// loadV1 decodes a schema 1 file strictly and validates it.
func loadV1(file string, data []byte) (model.Params, error) {
	var raw rawFan
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return model.Params{}, decodeError(file, err)
	}
	p, err := validate(file, data, raw)
	if err != nil {
		return model.Params{}, err
	}
	p.Schema = 1
	return p, nil
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

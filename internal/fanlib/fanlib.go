// Package fanlib loads fan parameter files from the fan library.
package fanlib

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/zeroflow/esphome-sil/fans"
	"github.com/zeroflow/esphome-sil/internal/model"
)

// DefaultFan is the fan every port uses unless configured otherwise.
const DefaultFan = "arctic-p12-pwm-pst"

// Source is the fan library. Plan 01-05 adds the SIL_FAN_DIR override.
func Source() fs.FS { return fans.FS }

type fanFile struct {
	Name      string  `toml:"name"`
	Source    string  `toml:"source"`
	MinDuty   float64 `toml:"min_duty"`
	StartDuty float64 `toml:"start_duty"`
	BelowMin  string  `toml:"below_min"`
	MinRPM    float64 `toml:"min_rpm"`
	MaxRPM    float64 `toml:"max_rpm"`
	TauUp     float64 `toml:"tau_up"`
	TauDown   float64 `toml:"tau_down"`
}

// Load reads <id>.toml from the fan library. Unknown keys are rejected so a
// typo such as max_rmp is an error, not a silent default.
func Load(id string) (model.Params, error) {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return model.Params{}, fmt.Errorf("fanlib: invalid fan id %q", id)
	}
	raw, err := fs.ReadFile(Source(), id+".toml")
	if err != nil {
		return model.Params{}, fmt.Errorf("fanlib: fan %q: %w", id, err)
	}
	var f fanFile
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return model.Params{}, fmt.Errorf("fanlib: fan %q: %w", id, err)
	}
	if f.BelowMin != "stop" && f.BelowMin != "hold" {
		return model.Params{}, fmt.Errorf("fanlib: fan %q: below_min must be \"stop\" or \"hold\", got %q", id, f.BelowMin)
	}
	if f.MaxRPM <= f.MinRPM || f.MinDuty < 0 || f.MinDuty >= 1 || f.TauUp <= 0 || f.TauDown <= 0 {
		return model.Params{}, fmt.Errorf("fanlib: fan %q: parameters out of range", id)
	}
	return model.Params{
		Name: f.Name, Source: f.Source,
		MinDuty: f.MinDuty, StartDuty: f.StartDuty, BelowMin: f.BelowMin,
		MinRPM: f.MinRPM, MaxRPM: f.MaxRPM, TauUp: f.TauUp, TauDown: f.TauDown,
	}, nil
}

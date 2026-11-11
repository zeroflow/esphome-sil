package fanlib

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// Schema 2 fan file (schema = 2). Every table is declared here with pointer
// fields, so a missing key is detectable and any key not declared is rejected
// with its position. That covers a schema 1 "source" string (D-07: sources are
// named ids in [sources]) and a start-kick key (D-12: there is no kick).
//
// Keys of a schema 2 file:
//
//	schema = 2                  name = "..."  (at most 128 bytes)
//	[sources]                   id = "text"   (at most 32; id ^[a-z][a-z0-9_]*$; text 1..512 bytes)
//	[stop]                      duty                     in [0, 1)
//	[start]                     duty >= stop.duty        in [0, 1];  delay_s in [0, 60]
//	[dynamics]                  tau_up_s, tau_down_s, coast_tau_s     in (0, 600]
//	[curve.up] [curve.down]     duty = [...], rpm = [...]  2..64 points, duty strictly increasing in
//	                            [0, 1], rpm in [0, 100000] non-decreasing; curve.down is optional
//	[tach]        (optional)    ppr integer 1..16, noise_rpm 0..1000
//	[datasheet]   (optional)    airflow_m3h <= 100000, power_w <= 10000, ref_rpm <= 100000, each > 0
//	[measured]    (optional)    date, board, esphome, recording_sha256, tolerance: non-empty strings
//	                            of at most 512 bytes
//	[provenance]                "<value path>" = "<kind>" or "<kind>:<source id>" (at most 64 bytes)
//
// name, every [sources] text and every [measured] string must be free of control characters (they
// are printed to a terminal by `sil fans`).
//
// A value path is a member of model.ValuePaths; every value present in the file needs exactly one
// provenance entry (MODEL-12). measured.tolerance is a value; the other four [measured] keys
// identify the measurement record and take no entry. Provenance keys match value paths exactly: no
// case folding, no trimming.
//
// validateV2 applies its rules in a fixed order, so a file with several violations always reports
// the same first one: (a) required keys, (b) finite numbers, (c) scalar ranges and caps, (d) curve
// shape, (e) the D-10 floor rule, (f) provenance. Every line number comes from locate, which knows
// which [table] a key is in: duty and rpm appear in several tables.

// Caps (T-04-06): a fan file is hand edited and later machine written, so every size is bounded.
const (
	maxNameBytes   = 128
	maxTextBytes   = 512 // a source text, a [measured] string
	maxProvBytes   = 64  // a provenance entry value
	maxCurvePoints = 64
	maxSources     = 32
	maxDelayS      = 60
	maxTauS        = 600
	maxRPM         = 100000
	maxAirflowM3h  = 100000
	maxPowerW      = 10000
	maxNoiseRPM    = 1000
	minPPR, maxPPR = 1, 16
)

var sourceID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type rawV2Stop struct {
	Duty *float64 `toml:"duty"`
}

type rawV2Start struct {
	Duty   *float64 `toml:"duty"`
	DelayS *float64 `toml:"delay_s"`
}

type rawV2Dynamics struct {
	TauUpS    *float64 `toml:"tau_up_s"`
	TauDownS  *float64 `toml:"tau_down_s"`
	CoastTauS *float64 `toml:"coast_tau_s"`
}

type rawV2Curve struct {
	Duty []float64 `toml:"duty"`
	RPM  []float64 `toml:"rpm"`
}

type rawV2Curves struct {
	Up   *rawV2Curve `toml:"up"`
	Down *rawV2Curve `toml:"down"`
}

type rawV2Tach struct {
	PPR      *int     `toml:"ppr"`
	NoiseRPM *float64 `toml:"noise_rpm"`
}

type rawV2Datasheet struct {
	AirflowM3h *float64 `toml:"airflow_m3h"`
	PowerW     *float64 `toml:"power_w"`
	RefRPM     *float64 `toml:"ref_rpm"`
}

// rawV2Measured: date, board, esphome and recording_sha256 identify the
// measurement record and are not fan values. tolerance is a value (MODEL-12).
type rawV2Measured struct {
	Date            *string `toml:"date"`
	Board           *string `toml:"board"`
	ESPHome         *string `toml:"esphome"`
	RecordingSHA256 *string `toml:"recording_sha256"`
	Tolerance       *string `toml:"tolerance"`
}

type rawV2 struct {
	Schema     *int              `toml:"schema"`
	Name       *string           `toml:"name"`
	Sources    map[string]string `toml:"sources"`
	Stop       *rawV2Stop        `toml:"stop"`
	Start      *rawV2Start       `toml:"start"`
	Dynamics   *rawV2Dynamics    `toml:"dynamics"`
	Curve      *rawV2Curves      `toml:"curve"`
	Tach       *rawV2Tach        `toml:"tach"`
	Datasheet  *rawV2Datasheet   `toml:"datasheet"`
	Measured   *rawV2Measured    `toml:"measured"`
	Provenance map[string]string `toml:"provenance"`
}

// loadV2 decodes a schema 2 file strictly and validates it.
func loadV2(file string, data []byte) (model.Params, error) {
	var raw rawV2
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return model.Params{}, decodeError(file, err)
	}
	return validateV2(file, data, raw)
}

// validateV2 applies the schema 2 rules in the order documented at the top of this file.
func validateV2(file string, data []byte, r rawV2) (model.Params, error) {
	fail := func(key, format string, args ...any) error {
		table, k := splitKey(key)
		return &Error{File: file, Line: locate(data, table, k), Key: key, Msg: fmt.Sprintf(format, args...)}
	}

	var stopDuty, startDuty, startDelay, tauUp, tauDown, coastTau *float64
	if r.Stop != nil {
		stopDuty = r.Stop.Duty
	}
	if r.Start != nil {
		startDuty, startDelay = r.Start.Duty, r.Start.DelayS
	}
	if r.Dynamics != nil {
		tauUp, tauDown, coastTau = r.Dynamics.TauUpS, r.Dynamics.TauDownS, r.Dynamics.CoastTauS
	}
	var up, down *rawV2Curve
	if r.Curve != nil {
		up, down = r.Curve.Up, r.Curve.Down
	}

	// (a) Required keys. coast_tau_s has no fallback to tau_down_s (D-09). A table that is present
	// needs all its keys.
	missing := func(key string) error {
		return &Error{File: file, Key: key, Msg: "missing required key " + key}
	}
	for _, p := range []struct {
		key string
		ok  bool
	}{
		{"name", r.Name != nil},
		{"stop.duty", stopDuty != nil},
		{"start.duty", startDuty != nil},
		{"start.delay_s", startDelay != nil},
		{"dynamics.tau_up_s", tauUp != nil},
		{"dynamics.tau_down_s", tauDown != nil},
		{"dynamics.coast_tau_s", coastTau != nil},
		{"curve.up.duty", up != nil && up.Duty != nil},
		{"curve.up.rpm", up != nil && up.RPM != nil},
		{"curve.down.duty", down == nil || down.Duty != nil},
		{"curve.down.rpm", down == nil || down.RPM != nil},
		{"tach.ppr", r.Tach == nil || r.Tach.PPR != nil},
		{"tach.noise_rpm", r.Tach == nil || r.Tach.NoiseRPM != nil},
		{"datasheet.airflow_m3h", r.Datasheet == nil || r.Datasheet.AirflowM3h != nil},
		{"datasheet.power_w", r.Datasheet == nil || r.Datasheet.PowerW != nil},
		{"datasheet.ref_rpm", r.Datasheet == nil || r.Datasheet.RefRPM != nil},
		{"measured.date", r.Measured == nil || r.Measured.Date != nil},
		{"measured.board", r.Measured == nil || r.Measured.Board != nil},
		{"measured.esphome", r.Measured == nil || r.Measured.ESPHome != nil},
		{"measured.recording_sha256", r.Measured == nil || r.Measured.RecordingSHA256 != nil},
		{"measured.tolerance", r.Measured == nil || r.Measured.Tolerance != nil},
		{"provenance", r.Provenance != nil},
	} {
		if !p.ok {
			return model.Params{}, missing(p.key)
		}
	}

	// (b) Every number is finite; go-toml parses nan and inf.
	type numKey struct {
		key string
		v   *float64
	}
	nums := []numKey{
		{"stop.duty", stopDuty}, {"start.duty", startDuty}, {"start.delay_s", startDelay},
		{"dynamics.tau_up_s", tauUp}, {"dynamics.tau_down_s", tauDown}, {"dynamics.coast_tau_s", coastTau},
	}
	if r.Tach != nil {
		nums = append(nums, numKey{"tach.noise_rpm", r.Tach.NoiseRPM})
	}
	if r.Datasheet != nil {
		nums = append(nums,
			numKey{"datasheet.airflow_m3h", r.Datasheet.AirflowM3h},
			numKey{"datasheet.power_w", r.Datasheet.PowerW},
			numKey{"datasheet.ref_rpm", r.Datasheet.RefRPM})
	}
	for _, n := range nums {
		if n.v != nil && (math.IsNaN(*n.v) || math.IsInf(*n.v, 0)) {
			return model.Params{}, fail(n.key, "%s (%g) must be finite", n.key, *n.v)
		}
	}
	for _, c := range []struct {
		name string
		c    *rawV2Curve
	}{{"curve.up", up}, {"curve.down", down}} {
		if c.c == nil {
			continue
		}
		for _, arr := range []struct {
			key string
			v   []float64
		}{{c.name + ".duty", c.c.Duty}, {c.name + ".rpm", c.c.RPM}} {
			for i, v := range arr.v {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return model.Params{}, fail(arr.key, "%s[%d] (%g) must be finite", arr.key, i, v)
				}
			}
		}
	}

	// (c) Scalar ranges and caps.
	switch {
	case strings.TrimSpace(*r.Name) == "":
		return model.Params{}, fail("name", "name must not be empty")
	case len(*r.Name) > maxNameBytes:
		return model.Params{}, fail("name", "name is %d bytes; at most %d are allowed", len(*r.Name), maxNameBytes)
	case hasControl(*r.Name):
		return model.Params{}, fail("name", "name must not contain control characters")
	case !(*stopDuty >= 0 && *stopDuty < 1):
		return model.Params{}, fail("stop.duty", "stop.duty (%g) must be in [0, 1)", *stopDuty)
	case !(*startDuty >= 0 && *startDuty <= 1):
		return model.Params{}, fail("start.duty", "start.duty (%g) must be in [0, 1]", *startDuty)
	case *startDuty < *stopDuty:
		return model.Params{}, fail("start.duty", "start.duty (%g) must be >= stop.duty (%g)", *startDuty, *stopDuty)
	case *startDelay < 0:
		return model.Params{}, fail("start.delay_s", "start.delay_s (%g) must be >= 0", *startDelay)
	case *startDelay > maxDelayS:
		return model.Params{}, fail("start.delay_s", "start.delay_s (%g) must be at most %d", *startDelay, maxDelayS)
	case !(*tauUp > 0):
		return model.Params{}, fail("dynamics.tau_up_s", "dynamics.tau_up_s must be > 0 (got %g)", *tauUp)
	case *tauUp > maxTauS:
		return model.Params{}, fail("dynamics.tau_up_s", "dynamics.tau_up_s (%g) must be at most %d", *tauUp, maxTauS)
	case !(*tauDown > 0):
		return model.Params{}, fail("dynamics.tau_down_s", "dynamics.tau_down_s must be > 0 (got %g)", *tauDown)
	case *tauDown > maxTauS:
		return model.Params{}, fail("dynamics.tau_down_s", "dynamics.tau_down_s (%g) must be at most %d", *tauDown, maxTauS)
	case !(*coastTau > 0):
		return model.Params{}, fail("dynamics.coast_tau_s", "dynamics.coast_tau_s must be > 0 (got %g)", *coastTau)
	case *coastTau > maxTauS:
		return model.Params{}, fail("dynamics.coast_tau_s", "dynamics.coast_tau_s (%g) must be at most %d", *coastTau, maxTauS)
	}
	if err := checkOptionalTables(r, fail); err != nil {
		return model.Params{}, err
	}

	// (d) Curve shape.
	upCurve, err := checkCurve("curve.up", up, fail)
	if err != nil {
		return model.Params{}, err
	}
	var downCurve model.Curve
	if down != nil {
		if downCurve, err = checkCurve("curve.down", down, fail); err != nil {
			return model.Params{}, err
		}
	}

	// (e) D-10: a fan that never stops carries its floor rpm as a curve point at duty 0. The rule
	// holds for curve.down too when there is one: the plant follows it while the duty falls, so a
	// down curve that reaches 0 rpm would stop a fan that is reported as running.
	if *stopDuty == 0 {
		if !(upCurve[0].Duty == 0 && upCurve[0].RPM > 0) {
			return model.Params{}, fail("stop.duty",
				"stop.duty = 0 (the fan never stops) needs the first curve.up point at duty 0 with rpm > 0 (the floor rpm); got duty %g rpm %g",
				upCurve[0].Duty, upCurve[0].RPM)
		}
		if len(downCurve) > 0 && !(downCurve[0].Duty == 0 && downCurve[0].RPM > 0) {
			return model.Params{}, fail("curve.down.duty",
				"stop.duty = 0 (the fan never stops) needs the first curve.down point at duty 0 with rpm > 0 (the floor rpm); got duty %g rpm %g",
				downCurve[0].Duty, downCurve[0].RPM)
		}
	}

	// (f) Provenance.
	prov, err := checkProvenance(file, data, r)
	if err != nil {
		return model.Params{}, err
	}

	p := model.Params{
		Name:       *r.Name,
		MinDuty:    *stopDuty,
		StartDuty:  *startDuty,
		BelowMin:   "stop",
		MinRPM:     math.Min(upCurve.Min(), minOrInf(downCurve)),
		MaxRPM:     math.Max(upCurve.Max(), downCurve.Max()),
		TauUp:      *tauUp,
		TauDown:    *tauDown,
		CoastTau:   *coastTau,
		StartDelay: time.Duration(math.Round(*startDelay * 1e9)),
		Curve:      model.Curves{Up: upCurve, Down: downCurve},
		Schema:     2,
		Provenance: prov,
		Sources:    maps.Clone(r.Sources),
	}
	if r.Tach != nil {
		p.Tach = model.TachParams{PPR: *r.Tach.PPR, NoiseRPM: *r.Tach.NoiseRPM}
	}
	if r.Datasheet != nil {
		p.Datasheet = &model.Datasheet{AirflowM3h: *r.Datasheet.AirflowM3h, PowerW: *r.Datasheet.PowerW, RefRPM: *r.Datasheet.RefRPM}
	}
	if *stopDuty == 0 {
		p.BelowMin = "hold"
	}
	return p, nil
}

// checkOptionalTables is the rest of rule (c): the ranges of [tach], [datasheet], [measured] and
// [sources], in that order. Required keys and finiteness were checked by the earlier rules.
func checkOptionalTables(r rawV2, fail func(key, format string, args ...any) error) error {
	if t := r.Tach; t != nil {
		if *t.PPR < minPPR || *t.PPR > maxPPR {
			return fail("tach.ppr", "tach.ppr (%d) must be an integer from %d to %d", *t.PPR, minPPR, maxPPR)
		}
		if *t.NoiseRPM < 0 || *t.NoiseRPM > maxNoiseRPM {
			return fail("tach.noise_rpm", "tach.noise_rpm (%g) must be from 0 to %d", *t.NoiseRPM, maxNoiseRPM)
		}
	}
	if d := r.Datasheet; d != nil {
		for _, v := range []struct {
			key string
			v   float64
			max float64
		}{
			{"datasheet.airflow_m3h", *d.AirflowM3h, maxAirflowM3h},
			{"datasheet.power_w", *d.PowerW, maxPowerW},
			{"datasheet.ref_rpm", *d.RefRPM, maxRPM},
		} {
			switch {
			case !(v.v > 0):
				return fail(v.key, "%s (%g) must be > 0", v.key, v.v)
			case v.v > v.max:
				return fail(v.key, "%s (%g) must be at most %g", v.key, v.v, v.max)
			}
		}
	}
	if m := r.Measured; m != nil {
		for _, v := range []struct {
			key string
			v   string
		}{
			{"measured.date", *m.Date}, {"measured.board", *m.Board}, {"measured.esphome", *m.ESPHome},
			{"measured.recording_sha256", *m.RecordingSHA256}, {"measured.tolerance", *m.Tolerance},
		} {
			switch {
			case strings.TrimSpace(v.v) == "":
				return fail(v.key, "%s must not be empty", v.key)
			case len(v.v) > maxTextBytes:
				return fail(v.key, "%s is %d bytes; at most %d are allowed", v.key, len(v.v), maxTextBytes)
			case hasControl(v.v):
				return fail(v.key, "%s must not contain control characters", v.key)
			}
		}
	}
	if len(r.Sources) > maxSources {
		return fail("sources", "[sources] has %d entries; at most %d are allowed", len(r.Sources), maxSources)
	}
	ids := make([]string, 0, len(r.Sources))
	for id := range r.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		key, text := "sources."+id, r.Sources[id]
		switch {
		case !sourceID.MatchString(id):
			return fail(key, "source id %q must match %s", id, sourceID)
		case strings.TrimSpace(text) == "":
			return fail(key, "%s must not be empty", key)
		case len(text) > maxTextBytes:
			return fail(key, "%s is %d bytes; at most %d are allowed", key, len(text), maxTextBytes)
		case hasControl(text):
			return fail(key, "%s must not contain control characters", key)
		}
	}
	return nil
}

// hasControl reports whether s holds a control character (ESC, BEL, NUL, a newline, ...). Fan text
// is printed to a terminal by `sil fans`, so a control character in it would be an escape sequence
// injection point (T-04-06).
func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}

// minOrInf is the lowest rpm of c, or +Inf for an empty curve so that it does
// not take part in a minimum.
func minOrInf(c model.Curve) float64 {
	if len(c) == 0 {
		return math.Inf(1)
	}
	return c.Min()
}

// checkCurve validates one [curve.up] / [curve.down] table and returns it as a
// model.Curve. Equal-length arrays, 2 to 64 points, duty in [0, 1] and
// strictly increasing (so no zero-width segment ever reaches the division in
// Curve.At), rpm in [0, 100000] and non-decreasing.
func checkCurve(name string, c *rawV2Curve, fail func(key, format string, args ...any) error) (model.Curve, error) {
	dk, rk := name+".duty", name+".rpm"
	if len(c.Duty) != len(c.RPM) {
		return nil, fail(rk, "%s has %d values but %s has %d (the arrays are parallel)", rk, len(c.RPM), dk, len(c.Duty))
	}
	if len(c.Duty) < 2 {
		return nil, fail(dk, "%s needs at least 2 points (got %d)", name, len(c.Duty))
	}
	if len(c.Duty) > maxCurvePoints {
		return nil, fail(dk, "%s has %d points; at most %d are allowed", name, len(c.Duty), maxCurvePoints)
	}
	out := make(model.Curve, len(c.Duty))
	for i := range c.Duty {
		d, v := c.Duty[i], c.RPM[i]
		switch {
		case !(d >= 0 && d <= 1):
			return nil, fail(dk, "%s[%d] (%g) must be in [0, 1]", dk, i, d)
		case i > 0 && !(d > c.Duty[i-1]):
			return nil, fail(dk, "%s[%d] (%g) must be greater than %s[%d] (%g): duty must be strictly increasing", dk, i, d, dk, i-1, c.Duty[i-1])
		case v < 0:
			return nil, fail(rk, "%s[%d] (%g) must be >= 0", rk, i, v)
		case v > maxRPM:
			return nil, fail(rk, "%s[%d] (%g) must be at most %d", rk, i, v, maxRPM)
		case i > 0 && v < c.RPM[i-1]:
			return nil, fail(rk, "%s[%d] (%g) must not be below %s[%d] (%g): rpm must be non-decreasing", rk, i, v, rk, i-1, c.RPM[i-1])
		}
		out[i] = model.Point{Duty: d, RPM: v}
	}
	return out, nil
}

// presentValue reports whether the file has the value at dotted path. A curve
// table counts as one value.
func presentValue(r rawV2, path string) bool {
	switch path {
	case "stop.duty":
		return r.Stop != nil && r.Stop.Duty != nil
	case "start.duty":
		return r.Start != nil && r.Start.Duty != nil
	case "start.delay_s":
		return r.Start != nil && r.Start.DelayS != nil
	case "dynamics.tau_up_s":
		return r.Dynamics != nil && r.Dynamics.TauUpS != nil
	case "dynamics.tau_down_s":
		return r.Dynamics != nil && r.Dynamics.TauDownS != nil
	case "dynamics.coast_tau_s":
		return r.Dynamics != nil && r.Dynamics.CoastTauS != nil
	case "curve.up":
		return r.Curve != nil && r.Curve.Up != nil
	case "curve.down":
		return r.Curve != nil && r.Curve.Down != nil
	case "tach.ppr":
		return r.Tach != nil && r.Tach.PPR != nil
	case "tach.noise_rpm":
		return r.Tach != nil && r.Tach.NoiseRPM != nil
	case "datasheet.airflow_m3h":
		return r.Datasheet != nil && r.Datasheet.AirflowM3h != nil
	case "datasheet.power_w":
		return r.Datasheet != nil && r.Datasheet.PowerW != nil
	case "datasheet.ref_rpm":
		return r.Datasheet != nil && r.Datasheet.RefRPM != nil
	case "measured.tolerance":
		return r.Measured != nil && r.Measured.Tolerance != nil
	}
	return false
}

// checkProvenance enforces D-05..D-07: exactly one entry per present value, an
// entry names a present value, its kind is known, and a source reference
// ("kind:source_id") names an id of [sources]. Provenance never defaults. The
// result lists the entries in model.ValuePaths order.
func checkProvenance(file string, data []byte, r rawV2) ([]model.ProvEntry, error) {
	entryErr := func(path, format string, args ...any) error {
		return &Error{File: file, Line: locate(data, "provenance", path), Key: path,
			Msg: fmt.Sprintf("provenance entry %q: ", path) + fmt.Sprintf(format, args...)}
	}

	// Entries first (sorted, so the report is stable): a typo in an entry key is
	// the root cause and must not hide behind the value that now lacks an entry.
	paths := make([]string, 0, len(r.Provenance))
	for path := range r.Provenance {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	parsed := make(map[string]model.ProvEntry, len(paths))
	for _, path := range paths {
		if !presentValue(r, path) {
			return nil, &Error{File: file, Line: locate(data, "provenance", path), Key: path,
				Msg: fmt.Sprintf("provenance entry %q names no value of this file", path)}
		}
		if len(r.Provenance[path]) > maxProvBytes {
			return nil, entryErr(path, "the entry is %d bytes; at most %d are allowed", len(r.Provenance[path]), maxProvBytes)
		}
		kind, source, hasSource := strings.Cut(r.Provenance[path], ":")
		if kind == "" {
			return nil, entryErr(path, "empty kind (want one of %s)", strings.Join(model.Kinds, ", "))
		}
		if !isKind(kind) {
			return nil, entryErr(path, "unknown kind %q (want one of %s)", kind, strings.Join(model.Kinds, ", "))
		}
		if hasSource {
			if source == "" {
				return nil, entryErr(path, "empty source id after %q:", kind)
			}
			if _, ok := r.Sources[source]; !ok {
				return nil, entryErr(path, "source id %q is not defined in [sources]", source)
			}
		}
		parsed[path] = model.ProvEntry{Path: path, Kind: kind, Source: source}
	}

	var out []model.ProvEntry
	for _, path := range model.ValuePaths {
		if !presentValue(r, path) {
			continue
		}
		e, ok := parsed[path]
		if !ok {
			table, k := splitKey(path)
			return nil, &Error{File: file, Line: locate(data, table, k), Key: path,
				Msg: fmt.Sprintf("%s has no provenance entry (add %q = \"assumed\" to [provenance])", path, path)}
		}
		out = append(out, e)
	}
	return out, nil
}

func isKind(k string) bool {
	for _, v := range model.Kinds {
		if v == k {
			return true
		}
	}
	return false
}

// A TOML key: bare, "quoted" or 'quoted', possibly dotted. Quoted parts keep their content as
// written (so "start.duty" and " stop.duty" stay distinct), which is what the exact-match rule for
// provenance keys needs.
const tomlKey = `(?:"[^"]*"|'[^']*'|[A-Za-z0-9_-]+)(?:\s*\.\s*(?:"[^"]*"|'[^']*'|[A-Za-z0-9_-]+))*`

var (
	headerLine = regexp.MustCompile(`^\s*\[\s*(` + tomlKey + `)\s*\]`)
	assignLine = regexp.MustCompile(`^\s*(` + tomlKey + `)\s*=`)
	keyPart    = regexp.MustCompile(`"([^"]*)"|'([^']*)'|([A-Za-z0-9_-]+)`)
)

// dottedKey joins the parts of a TOML key with dots, without quotes or spaces around the parts.
func dottedKey(raw string) string {
	var parts []string
	for _, m := range keyPart.FindAllStringSubmatch(raw, -1) {
		parts = append(parts, m[1]+m[2]+m[3])
	}
	return strings.Join(parts, ".")
}

// splitKey splits a dotted key path into its table and its last part.
func splitKey(dotted string) (table, key string) {
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		return dotted[:i], dotted[i+1:]
	}
	return "", dotted
}

// valueScan follows a TOML value from line to line, so that locate knows when a line continues a
// value and is not a [header] or an assignment: the lines of a multi-line array (depth > 0) and of
// a multi-line string (ml holds its delimiter).
type valueScan struct {
	depth int    // open '[' of a multi-line array
	ml    string // `"""` or `'''` while inside a multi-line string, else ""
}

// open reports whether the next line continues the value.
func (v *valueScan) open() bool { return v.depth > 0 || v.ml != "" }

// scan reads one line (or the value part of an assignment line). Brackets inside strings and
// comments do not count. A backslash escapes the next character inside a basic string (one or
// three double quotes), so an escaped quote does not end it; a literal string (single quotes) has
// no escapes.
func (v *valueScan) scan(s string) {
	var quote byte // the delimiter of an open single-line string
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case v.ml != "":
			if v.ml[0] == '"' && c == '\\' {
				i++ // the escaped character
			} else if strings.HasPrefix(s[i:], v.ml) {
				i += 2
				// Up to two more quotes next to the closing delimiter belong to the string.
				for n := 0; n < 2 && i+1 < len(s) && s[i+1] == v.ml[0]; n++ {
					i++
				}
				v.ml = ""
			}
		case quote != 0:
			if quote == '"' && c == '\\' {
				i++ // the escaped character
			} else if c == quote {
				quote = 0
			}
		case strings.HasPrefix(s[i:], `"""`) || strings.HasPrefix(s[i:], `'''`):
			v.ml = s[i : i+3]
			i += 2
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return
		case c == '[':
			v.depth++
		case c == ']' && v.depth > 0:
			v.depth--
		}
	}
}

// locate returns the 1-based line of key inside [table] (table "" is the top level), or 0 when the
// file has no such key. It follows the [headers] as it reads, so a key name that repeats in several
// tables (duty, rpm) is found in the one asked for, never in an earlier one. Dotted keys
// (`down.duty = ...` under [curve]) and quoted keys (`"start.duty" = ...` under [provenance]) are
// matched by their full dotted path. A key that is itself a table (table "curve", key "up", or
// table "", key "provenance") is located at its [header] line when there is no assignment for it.
func locate(data []byte, table, key string) int {
	want := key
	if table != "" {
		want = table + "." + key
	}
	cur, header := "", 0
	var vs valueScan
	for i, line := range strings.Split(string(data), "\n") {
		if vs.open() { // inside a multi-line array or string
			vs.scan(line)
			continue
		}
		if m := headerLine.FindStringSubmatch(line); m != nil {
			cur = dottedKey(m[1])
			if cur == want && header == 0 {
				header = i + 1
			}
			continue
		}
		if m := assignLine.FindStringSubmatch(line); m != nil {
			full := dottedKey(m[1])
			if cur != "" {
				full = cur + "." + full
			}
			if full == want {
				return i + 1
			}
			vs = valueScan{}
			vs.scan(line[len(m[0]):])
		}
	}
	return header
}

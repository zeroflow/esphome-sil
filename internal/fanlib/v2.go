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

	"github.com/pelletier/go-toml/v2"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// Schema 2 fan file (schema = 2). Every table is declared here with pointer
// fields, so a missing key is detectable and any key not declared is rejected
// with its position. That covers a schema 1 "source" string (D-07: sources are
// named ids in [sources]) and a start-kick key (D-12: there is no kick).

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

// validateV2 applies the schema 2 rules in a fixed order, so the first
// violation is the one reported: (a) required keys, (b) finite numbers, (c)
// scalar ranges, (d) curve shape, (e) the D-10 floor rule, (f) provenance
// completeness (D-05, D-06, D-07).
func validateV2(file string, data []byte, r rawV2) (model.Params, error) {
	fail := func(key, format string, args ...any) error {
		return &Error{File: file, Line: lineOf(data, key), Key: key, Msg: fmt.Sprintf(format, args...)}
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

	// (a) Required keys. coast_tau_s has no fallback to tau_down_s (D-09).
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

	// (c) Scalar ranges.
	switch {
	case strings.TrimSpace(*r.Name) == "":
		return model.Params{}, fail("name", "name must not be empty")
	case !(*stopDuty >= 0 && *stopDuty < 1):
		return model.Params{}, fail("stop.duty", "stop.duty (%g) must be in [0, 1)", *stopDuty)
	case !(*startDuty >= 0 && *startDuty <= 1):
		return model.Params{}, fail("start.duty", "start.duty (%g) must be in [0, 1]", *startDuty)
	case *startDuty < *stopDuty:
		return model.Params{}, fail("start.duty", "start.duty (%g) must be >= stop.duty (%g)", *startDuty, *stopDuty)
	case *startDelay < 0:
		return model.Params{}, fail("start.delay_s", "start.delay_s (%g) must be >= 0", *startDelay)
	case !(*tauUp > 0):
		return model.Params{}, fail("dynamics.tau_up_s", "dynamics.tau_up_s must be > 0 (got %g)", *tauUp)
	case !(*tauDown > 0):
		return model.Params{}, fail("dynamics.tau_down_s", "dynamics.tau_down_s must be > 0 (got %g)", *tauDown)
	case !(*coastTau > 0):
		return model.Params{}, fail("dynamics.coast_tau_s", "dynamics.coast_tau_s must be > 0 (got %g)", *coastTau)
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

	// (e) D-10: a fan that never stops carries its floor rpm as a curve point at duty 0.
	if *stopDuty == 0 && !(upCurve[0].Duty == 0 && upCurve[0].RPM > 0) {
		return model.Params{}, fail("stop.duty",
			"stop.duty = 0 (the fan never stops) needs the first curve.up point at duty 0 with rpm > 0 (the floor rpm); got duty %g rpm %g",
			upCurve[0].Duty, upCurve[0].RPM)
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
	if *stopDuty == 0 {
		p.BelowMin = "hold"
	}
	return p, nil
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
// model.Curve. Equal-length arrays, at least two points, duty in [0, 1] and
// strictly increasing (so no zero-width segment ever reaches the division in
// Curve.At), rpm >= 0 and non-decreasing.
func checkCurve(name string, c *rawV2Curve, fail func(key, format string, args ...any) error) (model.Curve, error) {
	dk, rk := name+".duty", name+".rpm"
	if len(c.Duty) != len(c.RPM) {
		return nil, fail(rk, "%s has %d values but %s has %d (the arrays are parallel)", rk, len(c.RPM), dk, len(c.Duty))
	}
	if len(c.Duty) < 2 {
		return nil, fail(dk, "%s needs at least 2 points (got %d)", name, len(c.Duty))
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
		return &Error{File: file, Line: entryLine(data, path), Key: path,
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
			return nil, &Error{File: file, Line: entryLine(data, path), Key: path,
				Msg: fmt.Sprintf("provenance entry %q names no value of this file", path)}
		}
		kind, source, hasSource := strings.Cut(r.Provenance[path], ":")
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
			return nil, &Error{File: file, Line: lineOf(data, path), Key: path,
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

var tableHeader = regexp.MustCompile(`^\s*\[\s*([^\[\]]+?)\s*\]`)

// lineOf returns the 1-based line of the dotted key ("stop.duty" is key duty in
// table [stop]) in data. For a key that is itself a table ("curve.up") it is
// the header line. 0 when the key is not found.
func lineOf(data []byte, dotted string) int {
	table, key := "", dotted
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		table, key = dotted[:i], dotted[i+1:]
	}
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	cur, header := "", 0
	for i, line := range strings.Split(string(data), "\n") {
		if m := tableHeader.FindStringSubmatch(line); m != nil {
			cur = strings.ReplaceAll(m[1], " ", "")
			if cur == dotted {
				header = i + 1
			}
			continue
		}
		if cur == table && keyRe.MatchString(line) {
			return i + 1
		}
	}
	if header > 0 {
		return header
	}
	return keyLine(data, dotted)
}

// entryLine returns the line of a [provenance] entry whose key is path, quoted
// or bare, or 0.
func entryLine(data []byte, path string) int {
	re := regexp.MustCompile(`^\s*["']?` + regexp.QuoteMeta(path) + `["']?\s*=`)
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			return i + 1
		}
	}
	return 0
}

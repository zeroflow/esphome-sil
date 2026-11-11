package fanlib

import (
	"errors"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// base is a valid fan file; every fixture below is derived from it. The key
// lines are fixed: name 1, source 2, min_duty 3, start_duty 4, below_min 5,
// min_rpm 6, max_rpm 7, tau_up 8, tau_down 9.
const base = `name = "Test fan"
source = "unit test fixture"
min_duty = 0.05
start_duty = 0.10
below_min = "stop"
min_rpm = 200.0
max_rpm = 1800.0
tau_up = 1.0
tau_down = 2.5
`

// with returns base with the line of key replaced by line.
func with(t *testing.T, key, line string) string {
	t.Helper()
	lines := strings.Split(base, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, key+" ") {
			lines[i] = line
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("base has no key %s", key)
	return ""
}

// load runs the shared loader over one inline file served from a MapFS.
func load(body string) (model.Params, error) {
	fsys := fstest.MapFS{"test.toml": {Data: []byte(body)}}
	return loadFrom(fsys, "test", "fans/test.toml")
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("error %v (%T) is not a *fanlib.Error", err, err)
	}
	return fe
}

func TestLoadValidFile(t *testing.T) {
	p, err := load(base)
	if err != nil {
		t.Fatalf("valid file: %v", err)
	}
	want := model.Params{
		Name: "Test fan", Source: "unit test fixture",
		MinDuty: 0.05, StartDuty: 0.10, BelowMin: "stop",
		MinRPM: 200, MaxRPM: 1800, TauUp: 1.0, TauDown: 2.5, Schema: 1,
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("params = %+v, want %+v", p, want)
	}
	// Integers where floats are expected are fine.
	if _, err := load(with(t, "min_rpm", "min_rpm = 200")); err != nil {
		t.Errorf("integer min_rpm: %v", err)
	}
}

func TestUnknownKeyIsLocated(t *testing.T) {
	body := strings.Replace(base, "max_rpm", "max_rmp", 1)
	_, err := load(body)
	fe := asError(t, err)
	if fe.Key != "max_rmp" || fe.Msg != "unknown field max_rmp" {
		t.Errorf("key %q msg %q, want max_rmp / unknown field max_rmp", fe.Key, fe.Msg)
	}
	if fe.File != "fans/test.toml" || fe.Line != 7 || fe.Col != 1 {
		t.Errorf("position %s:%d:%d, want fans/test.toml:7:1", fe.File, fe.Line, fe.Col)
	}
	if got, want := err.Error(), "fans/test.toml:7:1: unknown field max_rmp"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUnknownKeyDoesNotHideBehindMissingKey(t *testing.T) {
	// max_rpm is also missing here; the typo is the root cause and wins.
	_, err := load(strings.Replace(base, "max_rpm", "max_rmp", 1))
	if fe := asError(t, err); !strings.Contains(fe.Msg, "unknown field max_rmp") {
		t.Errorf("msg %q, want the unknown field first", fe.Msg)
	}
}

func TestMissingRequiredKey(t *testing.T) {
	for _, key := range []string{"name", "source", "min_duty", "start_duty", "below_min", "min_rpm", "max_rpm", "tau_up", "tau_down"} {
		var lines []string
		for _, l := range strings.Split(base, "\n") {
			if !strings.HasPrefix(l, key+" ") {
				lines = append(lines, l)
			}
		}
		_, err := load(strings.Join(lines, "\n"))
		if err == nil {
			t.Errorf("file without %s loaded", key)
			continue
		}
		fe := asError(t, err)
		if fe.Key != key || fe.Msg != "missing required key "+key {
			t.Errorf("without %s: key %q msg %q", key, fe.Key, fe.Msg)
		}
		if !strings.Contains(err.Error(), "missing required key "+key) || !strings.HasPrefix(err.Error(), "fans/test.toml") {
			t.Errorf("without %s: Error() = %q", key, err.Error())
		}
	}
}

func TestSemanticErrorsCarryTheLineOfTheKey(t *testing.T) {
	cases := []struct {
		name string
		body string
		line int
		key  string
		msg  string
	}{
		{"start below min", with(t, "start_duty", "start_duty = 0.03"), 4, "start_duty", "start_duty (0.03) must be >= min_duty (0.05)"},
		{"rpm order", with(t, "min_rpm", "min_rpm = 2000"), 6, "min_rpm", "min_rpm (2000) must be <= max_rpm (1800)"},
		{"bad below_min", with(t, "below_min", `below_min = "coast"`), 5, "below_min", `below_min must be "stop" or "hold"`},
		{"tau_up zero", with(t, "tau_up", "tau_up = 0"), 8, "tau_up", "tau_up must be > 0"},
		{"tau_down negative", with(t, "tau_down", "tau_down = -1.5"), 9, "tau_down", "tau_down must be > 0"},
		{"min_duty one", with(t, "min_duty", "min_duty = 1.0"), 3, "min_duty", "min_duty (1) must be in [0, 1)"},
		{"min_duty negative", with(t, "min_duty", "min_duty = -0.1"), 3, "min_duty", "min_duty (-0.1) must be in [0, 1)"},
		{"start_duty above one", with(t, "start_duty", "start_duty = 1.5"), 4, "start_duty", "start_duty (1.5) must be in [0, 1]"},
		{"min_rpm negative", with(t, "min_rpm", "min_rpm = -5"), 6, "min_rpm", "min_rpm (-5) must be >= 0"},
		{"max_rpm zero", strings.Replace(with(t, "min_rpm", "min_rpm = 0"), "max_rpm = 1800.0", "max_rpm = 0", 1), 7, "max_rpm", "max_rpm (0) must be > 0"},
		{"empty name", with(t, "name", `name = ""`), 1, "name", "name must not be empty"},
		{"empty source", with(t, "source", `source = ""`), 2, "source", "source must not be empty"},
		{"nan tau", with(t, "tau_up", "tau_up = nan"), 8, "tau_up", "tau_up (NaN) must be finite"},
		{"inf rpm", with(t, "max_rpm", "max_rpm = inf"), 7, "max_rpm", "max_rpm (+Inf) must be finite"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(c.body)
			if err == nil {
				t.Fatalf("file loaded, want an error containing %q", c.msg)
			}
			fe := asError(t, err)
			if fe.Key != c.key || fe.Line != c.line || fe.Col != 0 {
				t.Errorf("key %q line %d col %d, want %q line %d col 0", fe.Key, fe.Line, fe.Col, c.key, c.line)
			}
			if !strings.Contains(fe.Msg, c.msg) {
				t.Errorf("msg %q, want it to contain %q", fe.Msg, c.msg)
			}
			if got, want := err.Error(), "fans/test.toml:"+strconv.Itoa(c.line)+": "+fe.Msg; got != want {
				t.Errorf("Error() = %q, want %q (no :col when col is 0)", got, want)
			}
		})
	}
}

func TestStartEqualsMinIsAccepted(t *testing.T) {
	p, err := load(with(t, "start_duty", "start_duty = 0.05"))
	if err != nil {
		t.Fatalf("zero-width hysteresis band rejected: %v", err)
	}
	if p.StartDuty != p.MinDuty {
		t.Errorf("start %v min %v", p.StartDuty, p.MinDuty)
	}
}

func TestHoldFanIsAccepted(t *testing.T) {
	p, err := load(with(t, "below_min", `below_min = "hold"`))
	if err != nil || p.BelowMin != "hold" {
		t.Errorf("hold fan: %+v, %v", p, err)
	}
}

func TestSyntaxAndTypeErrorsAreLocated(t *testing.T) {
	_, err := load(with(t, "tau_up", "tau_up = = 1"))
	fe := asError(t, err)
	if fe.Line != 8 || fe.Col == 0 {
		t.Errorf("syntax error at %d:%d, want line 8 and a column", fe.Line, fe.Col)
	}
	_, err = load(with(t, "min_duty", `min_duty = "low"`))
	fe = asError(t, err)
	if fe.Line != 3 {
		t.Errorf("type error at line %d, want 3 (%v)", fe.Line, err)
	}
}

func TestOversizedFileIsRejected(t *testing.T) {
	big := base + "# " + strings.Repeat("x", maxFileBytes) + "\n"
	_, err := load(big)
	if err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Errorf("oversized file: %v, want a 64 KiB error", err)
	}
	// Exactly at the cap is fine.
	pad := maxFileBytes - len(base) - 3
	ok := base + "# " + strings.Repeat("x", pad) + "\n"
	if len(ok) != maxFileBytes {
		t.Fatalf("fixture is %d bytes, want %d", len(ok), maxFileBytes)
	}
	if _, err := load(ok); err != nil {
		t.Errorf("file of exactly %d bytes: %v", maxFileBytes, err)
	}
}

func TestEmbeddedLibraryLoadsCleanly(t *testing.T) {
	t.Setenv("SIL_FAN_DIR", "")
	ids, err := IDs()
	if err != nil {
		t.Fatalf("IDs: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("embedded fan library is empty")
	}
	found := false
	for i, id := range ids {
		if id == DefaultFan {
			found = true
		}
		if i > 0 && ids[i-1] >= id {
			t.Errorf("IDs not sorted: %v", ids)
		}
		p, err := Load(id)
		if err != nil {
			t.Errorf("Load(%q): %v", id, err)
			continue
		}
		if p.Schema != 2 {
			t.Errorf("%s Schema = %d, want 2 (D-01: the shipped library is schema 2)", id, p.Schema)
		}
	}
	if !found {
		t.Errorf("DefaultFan %q is not in %v", DefaultFan, ids)
	}
}

// shipped loads one embedded fan.
func shipped(t *testing.T, id string) model.Params {
	t.Helper()
	t.Setenv("SIL_FAN_DIR", "")
	p, err := Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShippedArcticFan(t *testing.T) {
	p := shipped(t, DefaultFan)
	if p.Schema != 2 || p.BelowMin != "stop" || p.MinDuty != 0.05 || p.StartDuty != 0.10 ||
		p.MinRPM != 200 || p.MaxRPM != 1800 {
		t.Errorf("arctic params changed: %+v", p)
	}
	up, down := p.Curve.Up, p.Curve.Down
	if len(up) < 2 || up[0] != (model.Point{Duty: 0.05, RPM: 200}) || up[len(up)-1] != (model.Point{Duty: 1.0, RPM: 1800}) {
		t.Errorf("curve.up = %v, want it to run from (0.05, 200) to (1.0, 1800) (the page-confirmed end points)", up)
	}
	if len(down) != len(up) {
		t.Fatalf("curve.down has %d points, curve.up %d", len(down), len(up))
	}
	for i := 1; i < len(up)-1; i++ {
		if down[i].Duty != up[i].Duty || !(down[i].RPM < up[i].RPM) {
			t.Errorf("interior point %d: down %v is not strictly below up %v at the same duty", i, down[i], up[i])
		}
	}
	if p.StartDelay != 400*time.Millisecond || p.CoastTau != 6 {
		t.Errorf("start delay %v coast tau %v, want 400ms and 6", p.StartDelay, p.CoastTau)
	}
	if p.Tach != (model.TachParams{PPR: 2, NoiseRPM: 20}) {
		t.Errorf("tach = %+v, want {2, 20}", p.Tach)
	}
	if p.Datasheet == nil || *p.Datasheet != (model.Datasheet{AirflowM3h: 95.7, PowerW: 1.2, RefRPM: 1800}) {
		t.Errorf("datasheet = %+v, want {95.7, 1.2, 1800}", p.Datasheet)
	}
}

func TestShippedNoctuaFan(t *testing.T) {
	p := shipped(t, "noctua-nf-a12x25-pwm")
	if p.Schema != 2 || p.BelowMin != "hold" || p.MinDuty != 0 || p.MaxRPM != 2000 || p.MinRPM != 450 {
		t.Errorf("noctua params changed: %+v", p)
	}
	if len(p.Curve.Up) < 2 || p.Curve.Up[0] != (model.Point{Duty: 0, RPM: 450}) {
		t.Errorf("curve.up = %v, want the first point (0, 450) (D-10 never-stops form)", p.Curve.Up)
	}
	if p.Tach != (model.TachParams{PPR: 2, NoiseRPM: 20}) {
		t.Errorf("tach = %+v, want {2, 20}", p.Tach)
	}
	if p.Datasheet == nil || *p.Datasheet != (model.Datasheet{AirflowM3h: 102.1, PowerW: 1.68, RefRPM: 2000}) {
		t.Errorf("datasheet = %+v, want {102.1, 1.68, 2000}", p.Datasheet)
	}
}

// TestShippedProvenanceMatchesEvidence pins the exact path to kind[:source] map of both shipped files, so a
// provenance upgrade without evidence fails here (T-04-17, MODEL-12). A value is `datasheet` only when a
// fetched vendor page states it, when it is derived arithmetically from such page figures with the derivation
// recorded in the file, or when an official vendor datasheet PDF states it with its URL in [sources] (D-04 as
// amended). Nothing in the shipped library is `measured` before Phase 8.
func TestShippedProvenanceMatchesEvidence(t *testing.T) {
	assumed := map[string]string{
		"start.duty": "assumed", "start.delay_s": "assumed",
		"dynamics.tau_up_s": "assumed", "dynamics.tau_down_s": "assumed", "dynamics.coast_tau_s": "assumed",
		"curve.down": "assumed", "tach.ppr": "assumed", "tach.noise_rpm": "assumed",
	}
	want := map[string]map[string]string{
		"arctic-p12-pwm-pst": {
			"stop.duty":             "datasheet:arctic_page",
			"curve.up":              "assumed:arctic_page",
			"datasheet.airflow_m3h": "datasheet:arctic_page",
			"datasheet.power_w":     "datasheet:arctic_page",
			"datasheet.ref_rpm":     "datasheet:arctic_page",
		},
		"noctua-nf-a12x25-pwm": {
			"stop.duty":             "assumed",
			"curve.up":              "assumed:noctua_datasheet",
			"datasheet.airflow_m3h": "datasheet:noctua_datasheet",
			"datasheet.power_w":     "datasheet:noctua_datasheet",
			"datasheet.ref_rpm":     "datasheet:noctua_datasheet",
		},
	}
	for id, w := range want {
		maps.Copy(w, assumed)
		p := shipped(t, id)
		got := map[string]string{}
		for _, e := range p.Provenance {
			v := e.Kind
			if e.Source != "" {
				v += ":" + e.Source
			}
			got[e.Path] = v
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s provenance = %v, want %v", id, got, w)
		}
		if len(p.Sources) == 0 {
			t.Errorf("%s has no [sources]", id)
		}
		for sid, text := range p.Sources {
			if !strings.HasPrefix(text, "https://") {
				t.Errorf("%s source %s = %q, want it to start with https://", id, sid, text)
			}
		}
		for _, e := range p.Provenance {
			if e.Source != "" {
				if _, ok := p.Sources[e.Source]; !ok {
					t.Errorf("%s: %s cites %q, which [sources] does not define", id, e.Path, e.Source)
				}
			}
		}
	}
}

// TestShippedFansShowNoise is the deterministic check of D-03's intent: the default sim visibly jitters. Each
// shipped fan is held at duty 0.6 under the default seed; 60 consecutive 1 s gate readings must take at least 3
// distinct values and spread by more than 15 rpm (population standard deviation), the most a noise-free tach can
// reach, toggling between two levels 30 rpm apart. With noise_rpm 0 the same fan gives at most 2 values.
func TestShippedFansShowNoise(t *testing.T) {
	// 0x5EED is lifecycle.DefaultSeed (fanlib cannot import lifecycle).
	const seed = 0x5EED
	readings := func(p model.Params) []float64 {
		pl := model.NewPlant(p, 1, seed)
		var out []float64
		for i := 1; i <= 7000; i++ { // 70 s in 10 ms steps
			pl.Step(0.6, 10*time.Millisecond)
			if i%100 == 0 && i > 1000 {
				r, ok := pl.Reading()
				if !ok {
					t.Fatalf("no gate reading at %d s", i/100)
				}
				out = append(out, r)
			}
		}
		return out
	}
	distinct := func(v []float64) int {
		m := map[float64]bool{}
		for _, x := range v {
			m[x] = true
		}
		return len(m)
	}
	for _, id := range []string{"arctic-p12-pwm-pst", "noctua-nf-a12x25-pwm"} {
		p := shipped(t, id)
		v := readings(p)
		if len(v) != 60 {
			t.Fatalf("%s: %d readings, want 60", id, len(v))
		}
		mean := 0.0
		for _, x := range v {
			mean += x
		}
		mean /= float64(len(v))
		ss := 0.0
		for _, x := range v {
			ss += (x - mean) * (x - mean)
		}
		sd := math.Sqrt(ss / float64(len(v)))
		if n := distinct(v); n < 3 || sd <= 15 {
			t.Errorf("%s: %d distinct readings, sd %.1f rpm, want at least 3 and above 15 (noise_rpm %g)", id, n, sd, p.Tach.NoiseRPM)
		}
		quiet := p
		quiet.Tach.NoiseRPM = 0
		if n := distinct(readings(quiet)); n > 2 {
			t.Errorf("%s with noise_rpm 0: %d distinct readings, want at most 2", id, n)
		}
	}
}

func TestLoadUnknownIDNamesIDAndLocation(t *testing.T) {
	t.Setenv("SIL_FAN_DIR", "")
	_, err := Load("no-such-fan")
	if err == nil || !strings.Contains(err.Error(), "no-such-fan") || !strings.Contains(err.Error(), "fans/") {
		t.Errorf("embedded unknown id: %v, want the id and the library location", err)
	}
	dir := t.TempDir()
	t.Setenv("SIL_FAN_DIR", dir)
	_, err = Load("no-such-fan")
	if err == nil || !strings.Contains(err.Error(), "no-such-fan") || !strings.Contains(err.Error(), dir) {
		t.Errorf("SIL_FAN_DIR unknown id: %v, want the id and %s", err, dir)
	}
}

func TestLoadRejectsUnsafeIDs(t *testing.T) {
	t.Setenv("SIL_FAN_DIR", "")
	for _, id := range []string{"", "../etc/passwd", "a/b", `a\b`, "..", ".hidden", "x.toml/..", "a b"} {
		if _, err := Load(id); err == nil {
			t.Errorf("Load(%q) succeeded", id)
		}
	}
}

func TestSilFanDirOverridesTheEmbeddedLibrary(t *testing.T) {
	dir := t.TempDir()
	custom := strings.Replace(base, "max_rpm = 1800.0", "max_rpm = 2400.0", 1)
	if err := os.WriteFile(filepath.Join(dir, "custom.toml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a fan"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIL_FAN_DIR", dir)

	if _, err := fs.Stat(Source(), "custom.toml"); err != nil {
		t.Fatalf("Source() does not read SIL_FAN_DIR: %v", err)
	}
	p, err := Load("custom")
	if err != nil || p.MaxRPM != 2400 {
		t.Fatalf("Load(custom) = %+v, %v", p, err)
	}
	ids, err := IDs()
	if err != nil || len(ids) != 1 || ids[0] != "custom" {
		t.Errorf("IDs() = %v, %v, want [custom]", ids, err)
	}
	// The embedded default is not reachable while the override is set.
	if _, err := Load(DefaultFan); err == nil {
		t.Errorf("Load(%s) succeeded although SIL_FAN_DIR holds no such file", DefaultFan)
	}

	// An error from an override file names the absolute path.
	bad := strings.Replace(base, "max_rpm", "max_rmp", 1)
	if err := os.WriteFile(filepath.Join(dir, "bad.toml"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load("bad")
	fe := asError(t, err)
	if fe.File != filepath.Join(dir, "bad.toml") || fe.Line != 7 {
		t.Errorf("override error at %s:%d, want %s:7", fe.File, fe.Line, filepath.Join(dir, "bad.toml"))
	}
}

func TestErrorFormatting(t *testing.T) {
	e := &Error{File: "f.toml", Line: 3, Col: 5, Key: "k", Msg: "m"}
	if got := e.Error(); got != "f.toml:3:5: m" {
		t.Errorf("with column: %q", got)
	}
	e = &Error{File: "f.toml", Line: 3, Msg: "m"}
	if got := e.Error(); got != "f.toml:3: m" {
		t.Errorf("without column: %q", got)
	}
	e = &Error{File: "f.toml", Msg: "m"}
	if got := e.Error(); got != "f.toml: m" {
		t.Errorf("without line: %q", got)
	}
}

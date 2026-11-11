package fanlib

import (
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// v2base is a valid schema 2 file, equal to testdata/fans-v2/curve-test.toml without the comments.
// Every case below derives from it by replacing or removing one line.
const v2base = `schema = 2
name = "Curve test fan"

[sources]
test = "test fixture, not a real fan"

[stop]
duty = 0.05

[start]
duty = 0.10
delay_s = 0.0

[dynamics]
tau_up_s = 0.5
tau_down_s = 0.8
coast_tau_s = 1.0

[curve.up]
duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]
rpm = [300, 600, 1000, 1300, 1550, 1700]

[curve.down]
duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]
rpm = [300, 520, 900, 1200, 1480, 1700]

[provenance]
"stop.duty" = "assumed:test"
"start.duty" = "assumed:test"
"start.delay_s" = "assumed:test"
"dynamics.tau_up_s" = "assumed:test"
"dynamics.tau_down_s" = "assumed:test"
"dynamics.coast_tau_s" = "assumed:test"
"curve.up" = "assumed:test"
"curve.down" = "assumed:test"
`

// v2with replaces the first occurrence of old in v2base by repl (old must exist).
func v2with(t *testing.T, old, repl string) string {
	t.Helper()
	if !strings.Contains(v2base, old) {
		t.Fatalf("v2base has no %q", old)
	}
	return strings.Replace(v2base, old, repl, 1)
}

// lineWith is the 1-based line of the first line of body that contains s.
func lineWith(t *testing.T, body, s string) int {
	t.Helper()
	for i, l := range strings.Split(body, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	t.Fatalf("no line with %q", s)
	return 0
}

// curveOf builds a curve from parallel duty and rpm arrays.
func curveOf(duty, rpm []float64) model.Curve {
	c := make(model.Curve, len(duty))
	for i := range duty {
		c[i] = model.Point{Duty: duty[i], RPM: rpm[i]}
	}
	return c
}

func TestV2LoadsIntoParams(t *testing.T) {
	p, err := load(v2base)
	if err != nil {
		t.Fatalf("valid schema 2 file: %v", err)
	}
	if p.Name != "Curve test fan" || p.Schema != 2 {
		t.Errorf("name %q schema %d, want Curve test fan / 2", p.Name, p.Schema)
	}
	if p.MinDuty != 0.05 || p.StartDuty != 0.10 || p.StartDelay != 0 {
		t.Errorf("thresholds %v %v %v, want 0.05 0.10 0", p.MinDuty, p.StartDuty, p.StartDelay)
	}
	if p.TauUp != 0.5 || p.TauDown != 0.8 || p.CoastTau != 1.0 {
		t.Errorf("taus %v %v %v, want 0.5 0.8 1.0", p.TauUp, p.TauDown, p.CoastTau)
	}
	if p.MaxRPM != 1700 || p.MinRPM != 300 || p.BelowMin != "stop" {
		t.Errorf("max %v min %v below_min %q, want 1700 300 stop", p.MaxRPM, p.MinRPM, p.BelowMin)
	}
	wantUp := curveOf([]float64{0.05, 0.20, 0.40, 0.60, 0.80, 1}, []float64{300, 600, 1000, 1300, 1550, 1700})
	wantDown := curveOf([]float64{0.05, 0.20, 0.40, 0.60, 0.80, 1}, []float64{300, 520, 900, 1200, 1480, 1700})
	if len(p.Curve.Up) != 6 || len(p.Curve.Down) != 6 {
		t.Fatalf("curves have %d / %d points, want 6 / 6", len(p.Curve.Up), len(p.Curve.Down))
	}
	for i := range wantUp {
		if p.Curve.Up[i] != wantUp[i] || p.Curve.Down[i] != wantDown[i] {
			t.Errorf("point %d: up %+v down %+v, want %+v %+v", i, p.Curve.Up[i], p.Curve.Down[i], wantUp[i], wantDown[i])
		}
	}
	if len(p.Provenance) != 8 {
		t.Fatalf("%d provenance entries, want 8: %+v", len(p.Provenance), p.Provenance)
	}
	for i, want := range []string{"stop.duty", "start.duty", "start.delay_s", "dynamics.tau_up_s", "dynamics.tau_down_s", "dynamics.coast_tau_s", "curve.up", "curve.down"} {
		if e := p.Provenance[i]; e.Path != want || e.Kind != model.KindAssumed || e.Source != "test" {
			t.Errorf("provenance[%d] = %+v, want %s assumed:test", i, e, want)
		}
	}
	if len(p.Sources) != 1 || p.Sources["test"] == "" {
		t.Errorf("sources = %v, want just test", p.Sources)
	}
	// The loaded params drive the model: rest, then a start.
	if s := model.Step(p, model.Initial(p), 0.4, 10*time.Millisecond); !s.Running || s.RPM <= 0 {
		t.Errorf("a step at duty 0.4 from rest: %+v, want running", s)
	}
}

func TestV2WithoutDownCurveAndSource(t *testing.T) {
	body := v2with(t, "[curve.down]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [300, 520, 900, 1200, 1480, 1700]\n", "")
	body = strings.Replace(body, "\"curve.down\" = \"assumed:test\"\n", "", 1)
	body = strings.Replace(body, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"assumed\"", 1)
	p, err := load(body)
	if err != nil {
		t.Fatalf("file without a down curve: %v", err)
	}
	if p.Curve.Down != nil || len(p.Provenance) != 7 {
		t.Errorf("down %v, %d provenance entries, want no down curve and 7 entries", p.Curve.Down, len(p.Provenance))
	}
	if e := p.Provenance[0]; e.Path != "stop.duty" || e.Kind != "assumed" || e.Source != "" {
		t.Errorf("provenance[0] = %+v, want stop.duty assumed with no source", e)
	}
}

func TestV2NeverStopsFormDerivesHold(t *testing.T) {
	hold := v2with(t, "[stop]\nduty = 0.05", "[stop]\nduty = 0.0")
	hold = strings.Replace(hold, "[curve.up]\nduty = [0.05,", "[curve.up]\nduty = [0.0,", 1)
	hold = strings.Replace(hold, "rpm = [300, 600,", "rpm = [450, 600,", 1)
	p, err := load(hold)
	if err != nil {
		t.Fatalf("never-stops fan: %v", err)
	}
	if p.BelowMin != "hold" || p.MinDuty != 0 || p.Curve.Up[0] != (model.Point{Duty: 0, RPM: 450}) {
		t.Errorf("BelowMin %q MinDuty %v first point %+v, want hold / 0 / (0, 450)", p.BelowMin, p.MinDuty, p.Curve.Up[0])
	}
	if !model.Initial(p).Running {
		t.Error("a never-stops fan must start out running")
	}

	for name, body := range map[string]string{
		"first point at 0.1": strings.Replace(hold, "[curve.up]\nduty = [0.0,", "[curve.up]\nduty = [0.1,", 1),
		"floor rpm 0":        strings.Replace(hold, "rpm = [450, 600,", "rpm = [0, 600,", 1),
	} {
		_, err := load(body)
		if err == nil {
			t.Errorf("%s: loaded, want an error", name)
			continue
		}
		fe := asError(t, err)
		if fe.Key != "stop.duty" || fe.Line != lineWith(t, body, "duty = 0.0") {
			t.Errorf("%s: key %q line %d, want stop.duty at its line", name, fe.Key, fe.Line)
		}
	}
}

func TestV2UnknownKeysAreLocated(t *testing.T) {
	cases := []struct{ name, old, repl, key, line string }{
		{"start kick key", "delay_s = 0.0\n", "delay_s = 0.0\nkick_duty = 0.3\n", "start.kick_duty", "kick_duty"},
		{"misspelt curve key", "rpm = [300, 600,", "rmp = [300, 600,", "curve.up.rmp", "rmp"},
		{"v1 source string", "name = \"Curve test fan\"\n", "name = \"Curve test fan\"\nsource = \"x\"\n", "source", "source ="},
		{"top level typo", "name = \"Curve test fan\"\n", "name = \"Curve test fan\"\nfoo = 1\n", "foo", "foo ="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := v2with(t, c.old, c.repl)
			_, err := load(body)
			if err == nil {
				t.Fatal("file loaded, want an unknown-key error")
			}
			fe := asError(t, err)
			if fe.Key != c.key || !strings.Contains(fe.Msg, "unknown field") {
				t.Errorf("key %q msg %q, want %q / unknown field", fe.Key, fe.Msg, c.key)
			}
			if want := lineWith(t, body, c.line); fe.Line != want || fe.Col == 0 {
				t.Errorf("position %d:%d, want line %d and a column", fe.Line, fe.Col, want)
			}
			if !strings.HasPrefix(err.Error(), "fans/test.toml:") {
				t.Errorf("Error() = %q, want file:line:col", err.Error())
			}
		})
	}
}

func TestV2MissingKeysAreNamed(t *testing.T) {
	cases := []struct{ name, body, key string }{
		{"coast tau", v2with(t, "coast_tau_s = 1.0\n", ""), "dynamics.coast_tau_s"},
		{"name", v2with(t, "name = \"Curve test fan\"\n", ""), "name"},
		{"stop table", v2with(t, "[stop]\nduty = 0.05\n", ""), "stop.duty"},
		{"delay", v2with(t, "delay_s = 0.0\n", ""), "start.delay_s"},
		{"provenance table", v2base[:strings.Index(v2base, "[provenance]")], "provenance"},
		{"down curve rpm", v2with(t, "rpm = [300, 520, 900, 1200, 1480, 1700]\n", ""), "curve.down.rpm"},
		{"up curve", v2with(t, "[curve.up]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [300, 600, 1000, 1300, 1550, 1700]\n", ""), "curve.up.duty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(c.body)
			if err == nil {
				t.Fatal("file loaded, want a missing-key error")
			}
			fe := asError(t, err)
			if fe.Key != c.key || fe.Msg != "missing required key "+c.key || fe.Line != 0 {
				t.Errorf("key %q msg %q line %d, want %q / missing required key / line 0", fe.Key, fe.Msg, fe.Line, c.key)
			}
		})
	}
}

func TestV2ProvenanceRules(t *testing.T) {
	cases := []struct {
		name string
		body string
		key  string // the Error.Key
		msg  string
	}{
		{"value without entry", v2with(t, "\"start.delay_s\" = \"assumed:test\"\n", ""), "start.delay_s", "no provenance entry"},
		{"entry for an absent down curve", v2with(t, "[curve.down]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [300, 520, 900, 1200, 1480, 1700]\n", ""), "curve.down", "names no value"},
		{"misspelt entry", v2base + "\"start.dutty\" = \"assumed\"\n", "start.dutty", "names no value"},
		{"unknown kind", v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"guessed\""), "stop.duty", "unknown kind"},
		{"undefined source id", v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"datasheet:nope\""), "stop.duty", "not defined in [sources]"},
		{"empty source id", v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"datasheet:\""), "stop.duty", "empty source id"},
		{"entry for a record key", v2base + "\"measured.date\" = \"measured\"\n", "measured.date", "names no value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(c.body)
			if err == nil {
				t.Fatal("file loaded, want a provenance error")
			}
			fe := asError(t, err)
			if fe.Key != c.key || !strings.Contains(fe.Msg, c.msg) || !strings.Contains(fe.Msg, c.key) {
				t.Errorf("key %q msg %q, want key %q and a message naming it with %q", fe.Key, fe.Msg, c.key, c.msg)
			}
			if fe.Line == 0 {
				t.Errorf("no line for %q (%v)", c.key, err)
			}
		})
	}

	// A kind alone (no source id) is fine, and a measured.tolerance value needs an entry too.
	withTol := v2base + "\n[measured]\ntolerance = \"0.05\"\n"
	if _, err := load(withTol); err == nil || !strings.Contains(err.Error(), "measured.tolerance") {
		t.Errorf("measured.tolerance without an entry: %v, want an error naming it", err)
	}
	withTol = strings.Replace(withTol, "[provenance]\n", "[provenance]\n\"measured.tolerance\" = \"measured\"\n", 1)
	if _, err := load(withTol); err != nil {
		t.Errorf("measured.tolerance with an entry: %v", err)
	}
}

func TestV2CurveShapeRules(t *testing.T) {
	const upRPM = "rpm = [300, 600, 1000, 1300, 1550, 1700]"
	const upDuty = "[curve.up]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]"
	cases := []struct {
		name string
		body string
		key  string
	}{
		{"one point", v2with(t, upDuty+"\n"+upRPM, "[curve.up]\nduty = [0.5]\nrpm = [900]"), "curve.up.duty"},
		{"unequal lengths", v2with(t, upRPM, "rpm = [300, 600, 1000]"), "curve.up.rpm"},
		{"equal consecutive duties", v2with(t, upDuty, "[curve.up]\nduty = [0.05, 0.20, 0.20, 0.60, 0.80, 1.00]"), "curve.up.duty"},
		{"decreasing rpm", v2with(t, upRPM, "rpm = [300, 600, 500, 1300, 1550, 1700]"), "curve.up.rpm"},
		{"duty above one", v2with(t, upDuty, "[curve.up]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.2]"), "curve.up.duty"},
		{"nan rpm", v2with(t, upRPM, "rpm = [300, 600, nan, 1300, 1550, 1700]"), "curve.up.rpm"},
		{"negative rpm", v2with(t, upRPM, "rpm = [-1, 600, 1000, 1300, 1550, 1700]"), "curve.up.rpm"},
		{"down curve decreasing", v2with(t, "rpm = [300, 520, 900, 1200, 1480, 1700]", "rpm = [300, 520, 900, 800, 1480, 1700]"), "curve.down.rpm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(c.body)
			if err == nil {
				t.Fatal("file loaded, want a curve error")
			}
			fe := asError(t, err)
			curve := c.key[:strings.LastIndex(c.key, ".")]
			if fe.Key != c.key || !strings.Contains(fe.Msg, curve) || fe.Line == 0 {
				t.Errorf("key %q line %d msg %q, want %q with the curve %q named in the message and a line", fe.Key, fe.Line, fe.Msg, c.key, curve)
			}
		})
	}
	// Equal consecutive rpm (a flat segment) is fine.
	if _, err := load(v2with(t, upRPM, "rpm = [300, 600, 600, 1300, 1550, 1700]")); err != nil {
		t.Errorf("flat segment: %v", err)
	}
}

func TestV2ScalarRanges(t *testing.T) {
	cases := []struct{ name, old, repl, key string }{
		{"stop duty one", "[stop]\nduty = 0.05", "[stop]\nduty = 1.0", "stop.duty"},
		{"start below stop", "[start]\nduty = 0.10", "[start]\nduty = 0.02", "start.duty"},
		{"negative delay", "delay_s = 0.0", "delay_s = -1", "start.delay_s"},
		{"zero tau up", "tau_up_s = 0.5", "tau_up_s = 0", "dynamics.tau_up_s"},
		{"nan coast tau", "coast_tau_s = 1.0", "coast_tau_s = nan", "dynamics.coast_tau_s"},
		{"empty name", "name = \"Curve test fan\"", "name = \"\"", "name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(v2with(t, c.old, c.repl))
			if err == nil {
				t.Fatal("file loaded")
			}
			if fe := asError(t, err); fe.Key != c.key || fe.Line == 0 {
				t.Errorf("key %q line %d, want %q with a line (%v)", fe.Key, fe.Line, c.key, err)
			}
		})
	}
}

func TestV2SchemaProbe(t *testing.T) {
	_, err := load(strings.Replace(v2base, "schema = 2", "schema = 3", 1))
	if err == nil {
		t.Fatal("schema 3 loaded")
	}
	fe := asError(t, err)
	if fe.Key != "schema" || fe.Line != 1 || !strings.Contains(fe.Msg, "schema must be 2") {
		t.Errorf("key %q line %d msg %q, want schema / 1 / schema must be 2", fe.Key, fe.Line, fe.Msg)
	}
	// A file without the key is still the flat v0.1.0 format.
	p, err := load(base)
	if err != nil || p.Schema != 0 || p.Curve.Up != nil || p.Provenance != nil {
		t.Errorf("schema 1 file: %+v, %v, want the flat v0.1.0 params", p, err)
	}
	// A schema 1 file that names schema = 1 is not accepted either.
	if _, err := load("schema = 1\n" + base); err == nil {
		t.Error("schema = 1 loaded")
	}
}

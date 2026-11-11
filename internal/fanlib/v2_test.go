package fanlib

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	// The down curve carries the floor rpm at duty 0 as well (WR-01).
	hold = strings.Replace(hold, "[curve.down]\nduty = [0.05,", "[curve.down]\nduty = [0.0,", 1)
	hold = strings.Replace(hold, "rpm = [300, 520,", "rpm = [450, 520,", 1)
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
	if p.MinRPM != 450 || p.Curve.Down[0] != (model.Point{Duty: 0, RPM: 450}) {
		t.Errorf("MinRPM %v first down point %+v, want 450 / (0, 450)", p.MinRPM, p.Curve.Down[0])
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

	// WR-01: the rule holds for curve.down too, or the fan would decay to 0 rpm while "running".
	for name, body := range map[string]string{
		"down first point at 0.05": strings.Replace(hold, "[curve.down]\nduty = [0.0,", "[curve.down]\nduty = [0.05,", 1),
		"down floor rpm 0":         strings.Replace(hold, "rpm = [450, 520,", "rpm = [0, 520,", 1),
		"down starts at half duty": strings.Replace(hold, "[curve.down]\nduty = [0.0, 0.20, 0.40, 0.60, 0.80, 1.00]", "[curve.down]\nduty = [0.5, 0.60, 0.70, 0.80, 0.90, 1.00]", 1),
	} {
		_, err := load(body)
		if err == nil {
			t.Errorf("%s: loaded, want an error", name)
			continue
		}
		fe := asError(t, err)
		if fe.Key != "curve.down.duty" || !strings.Contains(fe.Msg, "curve.down") || fe.Line != lineIn(t, body, "curve.down", "duty") {
			t.Errorf("%s: key %q line %d msg %q, want curve.down.duty at the down duty line", name, fe.Key, fe.Line, fe.Msg)
		}
	}

	// Without a down curve the up curve is used, so the up rule is all that applies.
	noDown := strings.Replace(hold, "[curve.down]\nduty = [0.0, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [450, 520, 900, 1200, 1480, 1700]\n", "", 1)
	noDown = strings.Replace(noDown, "\"curve.down\" = \"assumed:test\"\n", "", 1)
	if strings.Contains(noDown, "curve.down") {
		t.Fatal("test setup: the down curve is still in the file")
	}
	if p, err := load(noDown); err != nil || p.BelowMin != "hold" || p.Curve.Down != nil {
		t.Errorf("never-stops fan without a down curve: %+v, %v", p, err)
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
	withTol := v2base + "\n[measured]\ndate = \"2026-11-10\"\nboard = \"rev33\"\nesphome = \"2026.10.1\"\nrecording_sha256 = \"abc123\"\ntolerance = \"0.05\"\n"
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

// inTable replaces the first old after the [header] line of v2base (old must follow it).
func inTable(t *testing.T, header, old, repl string) string {
	t.Helper()
	i := strings.Index(v2base, "["+header+"]")
	if i < 0 {
		t.Fatalf("v2base has no [%s]", header)
	}
	rest := v2base[i:]
	if !strings.Contains(rest, old) {
		t.Fatalf("v2base has no %q after [%s]", old, header)
	}
	return v2base[:i] + strings.Replace(rest, old, repl, 1)
}

// lineIn is the 1-based line of the first line starting with key after the [header] line of body.
func lineIn(t *testing.T, body, header, key string) int {
	t.Helper()
	in := false
	for i, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			in = strings.Contains(strings.ReplaceAll(l, " ", ""), "["+header+"]")
			continue
		}
		if in && strings.HasPrefix(strings.TrimSpace(l), key) {
			return i + 1
		}
	}
	t.Fatalf("no %q line in [%s]", key, header)
	return 0
}

// withTable inserts a table before [provenance] and appends provenance entries after the others.
func withTable(table string, entries ...string) string {
	body := strings.Replace(v2base, "[provenance]\n", table+"\n[provenance]\n", 1)
	for _, e := range entries {
		body += "\"" + e + "\" = \"assumed:test\"\n"
	}
	return body
}

// wantError loads body and requires an *Error with the key, the line and a message containing msg.
func wantError(t *testing.T, body, key string, line int, msg string) {
	t.Helper()
	_, err := load(body)
	if err == nil {
		t.Fatalf("file loaded, want an error at %s (line %d)", key, line)
	}
	fe := asError(t, err)
	if fe.Key != key || fe.Line != line || !strings.Contains(fe.Msg, msg) {
		t.Errorf("error %q (key %q line %d), want key %q line %d containing %q", err, fe.Key, fe.Line, key, line, msg)
	}
}

func TestV2ErrorsAreLocatedInTheirTable(t *testing.T) {
	const downDuty = "duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]"
	cases := []struct {
		name, body, key, header, line, msg string
	}{
		{"non-increasing duty under curve.down",
			inTable(t, "curve.down", downDuty, "duty = [0.05, 0.20, 0.20, 0.60, 0.80, 1.00]"),
			"curve.down.duty", "curve.down", "duty", "strictly increasing"},
		{"tau_down_s zero in dynamics",
			v2with(t, "tau_down_s = 0.8", "tau_down_s = 0"),
			"dynamics.tau_down_s", "dynamics", "tau_down_s", "must be > 0"},
		{"stop.duty out of range, not the duty of [start] or a curve",
			inTable(t, "stop", "duty = 0.05", "duty = 1.5"),
			"stop.duty", "stop", "duty", "must be in [0, 1)"},
		{"start.duty below stop.duty",
			inTable(t, "start", "duty = 0.10", "duty = 0.01"),
			"start.duty", "start", "duty", "must be >= stop.duty"},
		{"decreasing rpm under curve.down",
			inTable(t, "curve.down", "rpm = [300, 520, 900, 1200, 1480, 1700]", "rpm = [300, 520, 900, 800, 1480, 1700]"),
			"curve.down.rpm", "curve.down", "rpm", "non-decreasing"},
		{"curve table header with spaces",
			strings.Replace(inTable(t, "curve.down", downDuty, "duty = [0.05, 0.20, 0.20, 0.60, 0.80, 1.00]"), "[curve.down]", "[ curve.down ]", 1),
			"curve.down.duty", " curve.down ", "duty", "strictly increasing"},
		{"dotted keys inside [curve]",
			strings.Replace(strings.Replace(v2base, "[curve.down]\n", "[curve]\n", 1),
				"duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [300, 520", "down.duty = [0.05, 0.20, 0.20, 0.60, 0.80, 1.00]\ndown.rpm = [300, 520", 1),
			"curve.down.duty", "curve", "down.duty", "strictly increasing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := strings.TrimSpace(c.header)
			line := lineIn(t, c.body, h, c.line)
			wantError(t, c.body, c.key, line, c.msg)
		})
	}

	t.Run("a multi-line array does not confuse the table tracking", func(t *testing.T) {
		body := inTable(t, "curve.up", "rpm = [300, 600, 1000, 1300, 1550, 1700]",
			"rpm = [\n  300, 600,\n  500, # decreasing\n  1300, 1550, 1700,\n]")
		wantError(t, body, "curve.up.rpm", lineIn(t, body, "curve.up", "rpm"), "non-decreasing")
		// The header-like line [0.4] inside the array must not move the scanner into another table.
		tricky := strings.Replace(body, "  500, # decreasing\n", "  500,\n  [1],\n", 1)
		if got := locate([]byte(tricky), "curve.down", "rpm"); got != lineIn(t, tricky, "curve.down", "rpm") {
			t.Errorf("locate curve.down.rpm = %d, want %d", got, lineIn(t, tricky, "curve.down", "rpm"))
		}
	})
	t.Run("bad kind is located at the quoted key in [provenance]", func(t *testing.T) {
		body := v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"guessed\"")
		wantError(t, body, "stop.duty", lineWith(t, body, "\"stop.duty\" ="), "unknown kind")
	})
	t.Run("the duty of an equal curve is the line under its own header", func(t *testing.T) {
		body := inTable(t, "curve.up", "duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]", "duty = [0.05, 0.20, 0.20, 0.60, 0.80, 1.00]")
		wantError(t, body, "curve.up.duty", lineIn(t, body, "curve.up", "duty"), "strictly increasing")
		if lineIn(t, body, "curve.up", "duty") == lineIn(t, body, "curve.down", "duty") {
			t.Fatal("test setup: the two duty lines coincide")
		}
	})
}

// curveLines builds the duty and rpm lines of an n-point curve.
func curveLines(n int, rpmLast float64) (duty, rpm string) {
	d := make([]string, n)
	r := make([]string, n)
	for i := range d {
		d[i] = fmt.Sprintf("%g", float64(i)/float64(n-1))
		r[i] = fmt.Sprintf("%d", 100+i)
	}
	r[n-1] = fmt.Sprintf("%g", rpmLast)
	return "duty = [" + strings.Join(d, ", ") + "]", "rpm = [" + strings.Join(r, ", ") + "]"
}

// WR-02: free text is printed to a terminal by `sil fans`, so a control character in it is refused.
func TestV2RejectsControlCharacters(t *testing.T) {
	record := "date = \"2026-11-10\"\nboard = \"rev33\"\nesphome = \"2026.10.1\"\nrecording_sha256 = \"abc123\"\ntolerance = \"0.05\"\n"
	measured := withTable("[measured]\n"+record, "measured.tolerance")
	cases := []struct {
		name, body, key, line string
	}{
		{"name with ESC", v2with(t, "name = \"Curve test fan\"", "name = \"Curve\\u001b[2J\""), "name", "name ="},
		{"name with a tab", v2with(t, "name = \"Curve test fan\"", "name = \"Curve\\tfan\""), "name", "name ="},
		{"source text with BEL", v2with(t, "test fixture, not a real fan", "a\\u001b]0;t\\u0007b"), "sources.test", "test ="},
		{"measured board with NUL", strings.Replace(measured, "board = \"rev33\"", "board = \"rev\\u000033\"", 1), "measured.board", "board ="},
		{"measured tolerance with ESC", strings.Replace(measured, "tolerance = \"0.05\"", "tolerance = \"\\u001b0.05\"", 1), "measured.tolerance", "tolerance ="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantError(t, c.body, c.key, lineWith(t, c.body, c.line), "control characters")
		})
	}
	// Printable non-ASCII text is fine.
	if _, err := load(v2with(t, "name = \"Curve test fan\"", "name = \"Lüfter 120 mm · PWM\"")); err != nil {
		t.Errorf("a name with non-ASCII printable text: %v", err)
	}
}

func TestV2Caps(t *testing.T) {
	upDuty := "duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]"
	upRPM := "rpm = [300, 600, 1000, 1300, 1550, 1700]"
	dutyN, rpmN := curveLines(65, 1700)
	_, rpmBig := curveLines(6, 100001)
	manySources := "[sources]\ntest = \"test fixture, not a real fan\"\n"
	for i := 0; i < 32; i++ {
		manySources += fmt.Sprintf("s%d = \"x\"\n", i)
	}
	cases := []struct {
		name, body, key string
		line            func(body string) int
		msg             string
	}{
		{"65 curve points", strings.Replace(strings.Replace(v2base, upDuty, dutyN, 1), upRPM, rpmN, 1), "curve.up.duty",
			func(b string) int { return lineIn(t, b, "curve.up", "duty") }, "at most 64"},
		{"delay_s 61", v2with(t, "delay_s = 0.0", "delay_s = 61"), "start.delay_s",
			func(b string) int { return lineWith(t, b, "delay_s") }, "60"},
		{"tau_up_s 601", v2with(t, "tau_up_s = 0.5", "tau_up_s = 601"), "dynamics.tau_up_s",
			func(b string) int { return lineWith(t, b, "tau_up_s") }, "600"},
		{"tau_down_s 601", v2with(t, "tau_down_s = 0.8", "tau_down_s = 601"), "dynamics.tau_down_s",
			func(b string) int { return lineWith(t, b, "tau_down_s") }, "600"},
		{"coast_tau_s 601", v2with(t, "coast_tau_s = 1.0", "coast_tau_s = 601"), "dynamics.coast_tau_s",
			func(b string) int { return lineWith(t, b, "coast_tau_s") }, "600"},
		{"rpm 100001", v2with(t, upRPM, strings.Replace(rpmBig, "100, 101, 102, 103, 104, ", "300, 600, 1000, 1300, 1550, ", 1)), "curve.up.rpm",
			func(b string) int { return lineIn(t, b, "curve.up", "rpm") }, "100000"},
		{"33 sources", strings.Replace(v2base, "[sources]\ntest = \"test fixture, not a real fan\"\n", manySources, 1), "sources",
			func(b string) int { return lineWith(t, b, "[sources]") }, "at most 32"},
		{"source id Bad-Id", v2with(t, "test = \"test fixture, not a real fan\"\n", "test = \"test fixture, not a real fan\"\nBad-Id = \"x\"\n"), "sources.Bad-Id",
			func(b string) int { return lineWith(t, b, "Bad-Id") }, "^[a-z][a-z0-9_]*$"},
		{"513-byte source text", v2with(t, "test fixture, not a real fan", strings.Repeat("x", 513)), "sources.test",
			func(b string) int { return lineIn(t, b, "sources", "test") }, "512"},
		{"129-byte name", v2with(t, "name = \"Curve test fan\"", "name = \""+strings.Repeat("n", 129)+"\""), "name",
			func(b string) int { return lineWith(t, b, "name =") }, "128"},
		{"65-byte provenance value", v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"assumed:"+strings.Repeat("x", 57)+"\""), "stop.duty",
			func(b string) int { return lineWith(t, b, "\"stop.duty\" =") }, "64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantError(t, c.body, c.key, c.line(c.body), c.msg)
		})
	}

	// The caps themselves are allowed: 64 points, 60 s, 600 s, 100000 rpm, 32 sources, 512 and 128 bytes.
	dutyN, rpmN = curveLines(64, 100000)
	atCap := strings.Replace(strings.Replace(v2base, upDuty, dutyN, 1), upRPM, rpmN, 1)
	atCap = strings.Replace(atCap, "delay_s = 0.0", "delay_s = 60", 1)
	atCap = strings.Replace(atCap, "tau_up_s = 0.5", "tau_up_s = 600", 1)
	atCap = strings.Replace(atCap, "name = \"Curve test fan\"", "name = \""+strings.Repeat("n", 128)+"\"", 1)
	atCap = strings.Replace(atCap, "test fixture, not a real fan", strings.Repeat("x", 512), 1)
	if _, err := load(atCap); err != nil {
		t.Errorf("file at every cap: %v", err)
	}
	if _, err := load(strings.Replace(v2base, "[sources]\ntest = \"test fixture, not a real fan\"\n", strings.Replace(manySources, "s31 = \"x\"\n", "", 1), 1)); err != nil {
		t.Errorf("32 sources: %v", err)
	}
}

func TestV2TachTable(t *testing.T) {
	tach := func(ppr, noise string) string {
		return withTable("[tach]\nppr = "+ppr+"\nnoise_rpm = "+noise+"\n", "tach.ppr", "tach.noise_rpm")
	}
	p, err := load(tach("2", "5"))
	if err != nil {
		t.Fatalf("ppr 2 noise 5: %v", err)
	}
	if p.Tach != (model.TachParams{PPR: 2, NoiseRPM: 5}) {
		t.Errorf("Tach = %+v, want {2 5}", p.Tach)
	}
	for _, e := range p.Provenance {
		if e.Path == "tach.ppr" && e.Kind == "assumed" {
			goto counted
		}
	}
	t.Errorf("Provenance %v has no tach.ppr entry", p.Provenance)
counted:
	if p, err := load(tach("1", "0")); err != nil || p.Tach.PPR != 1 {
		t.Errorf("ppr 1 noise 0 (both lower limits): %+v, %v", p.Tach, err)
	}
	if p, err := load(tach("16", "1000")); err != nil || p.Tach.PPR != 16 {
		t.Errorf("ppr 16 noise 1000 (both upper limits): %+v, %v", p.Tach, err)
	}
	if p, err := load(v2base); err != nil || p.Tach != (model.TachParams{}) {
		t.Errorf("no [tach]: %+v, %v, want the ideal (zero) tach", p.Tach, err)
	}

	for _, c := range []struct{ name, ppr, noise, key, msg string }{
		{"ppr 0", "0", "5", "tach.ppr", "1 to 16"},
		{"ppr 17", "17", "5", "tach.ppr", "1 to 16"},
		{"ppr float", "2.5", "5", "tach.ppr", ""},
		{"noise -1", "2", "-1", "tach.noise_rpm", "0 to 1000"},
		{"noise 1001", "2", "1001", "tach.noise_rpm", "0 to 1000"},
		{"noise nan", "2", "nan", "tach.noise_rpm", "finite"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := tach(c.ppr, c.noise)
			wantError(t, body, c.key, lineIn(t, body, "tach", strings.TrimPrefix(c.key, "tach.")), c.msg)
		})
	}

	t.Run("only ppr names tach.noise_rpm", func(t *testing.T) {
		_, err := load(withTable("[tach]\nppr = 2\n", "tach.ppr"))
		if fe := asError(t, err); fe.Key != "tach.noise_rpm" || fe.Msg != "missing required key tach.noise_rpm" {
			t.Errorf("error %v, want missing required key tach.noise_rpm", err)
		}
	})
	t.Run("a tach value needs its provenance entry", func(t *testing.T) {
		_, err := load(withTable("[tach]\nppr = 2\nnoise_rpm = 5\n", "tach.ppr"))
		if fe := asError(t, err); fe.Key != "tach.noise_rpm" || !strings.Contains(fe.Msg, "no provenance entry") {
			t.Errorf("error %v, want tach.noise_rpm without provenance", err)
		}
	})
}

func TestV2DatasheetTable(t *testing.T) {
	ds := func(air, pw, ref string) string {
		return withTable("[datasheet]\nairflow_m3h = "+air+"\npower_w = "+pw+"\nref_rpm = "+ref+"\n",
			"datasheet.airflow_m3h", "datasheet.power_w", "datasheet.ref_rpm")
	}
	p, err := load(ds("95.7", "1.2", "1800"))
	if err != nil {
		t.Fatalf("datasheet: %v", err)
	}
	if p.Datasheet == nil || *p.Datasheet != (model.Datasheet{AirflowM3h: 95.7, PowerW: 1.2, RefRPM: 1800}) {
		t.Errorf("Datasheet = %+v, want {95.7 1.2 1800}", p.Datasheet)
	}
	if p, err := load(v2base); err != nil || p.Datasheet != nil {
		t.Errorf("no [datasheet]: %+v, %v, want nil", p.Datasheet, err)
	}
	if _, err := load(ds("100000", "10000", "100000")); err != nil {
		t.Errorf("datasheet at its caps: %v", err)
	}
	for _, c := range []struct{ name, air, pw, ref, key, msg string }{
		{"airflow 0", "0", "1.2", "1800", "airflow_m3h", "must be > 0"},
		{"power negative", "95.7", "-1", "1800", "power_w", "must be > 0"},
		{"ref_rpm 0", "95.7", "1.2", "0", "ref_rpm", "must be > 0"},
		{"airflow 100001", "100001", "1.2", "1800", "airflow_m3h", "100000"},
		{"power 10001", "95.7", "10001", "1800", "power_w", "10000"},
		{"ref_rpm 100001", "95.7", "1.2", "100001", "ref_rpm", "100000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := ds(c.air, c.pw, c.ref)
			wantError(t, body, "datasheet."+c.key, lineIn(t, body, "datasheet", c.key), c.msg)
		})
	}
	for _, missing := range []string{"airflow_m3h", "power_w", "ref_rpm"} {
		t.Run("missing "+missing, func(t *testing.T) {
			body := ds("95.7", "1.2", "1800")
			var kept []string
			for _, l := range strings.Split(body, "\n") {
				if !strings.HasPrefix(l, missing+" =") {
					kept = append(kept, l)
				}
			}
			_, err := load(strings.Join(kept, "\n"))
			if fe := asError(t, err); fe.Key != "datasheet."+missing || fe.Msg != "missing required key datasheet."+missing {
				t.Errorf("error %v, want missing required key datasheet.%s", err, missing)
			}
		})
	}
}

func TestV2MeasuredTable(t *testing.T) {
	const record = "date = \"2026-11-10\"\nboard = \"rev33\"\nesphome = \"2026.10.1\"\nrecording_sha256 = \"abc123\"\ntolerance = \"0.05\"\n"
	body := withTable("[measured]\n"+record, "measured.tolerance")
	body = strings.Replace(body, "\"measured.tolerance\" = \"assumed:test\"", "\"measured.tolerance\" = \"measured\"", 1)
	p, err := load(body)
	if err != nil {
		t.Fatalf("all five keys with a tolerance entry: %v", err)
	}
	var tol *model.ProvEntry
	for i := range p.Provenance {
		if p.Provenance[i].Path == "measured.tolerance" {
			tol = &p.Provenance[i]
		}
	}
	if tol == nil || tol.Kind != "measured" {
		t.Errorf("Provenance %v, want measured.tolerance counted as kind measured", p.Provenance)
	}
	if len(p.Provenance) != 9 {
		t.Errorf("%d provenance entries, want the 8 of the base plus measured.tolerance", len(p.Provenance))
	}

	t.Run("without the tolerance entry", func(t *testing.T) {
		b := withTable("[measured]\n" + record)
		wantError(t, b, "measured.tolerance", lineIn(t, b, "measured", "tolerance"), "no provenance entry")
	})
	for _, k := range []string{"date", "board", "esphome", "recording_sha256"} {
		t.Run("entry for "+k+" names no value", func(t *testing.T) {
			b := withTable("[measured]\n"+record, "measured.tolerance", "measured."+k)
			wantError(t, b, "measured."+k, lineWith(t, b, "\"measured."+k+"\" ="), "names no value")
		})
	}
	for _, k := range []string{"date", "board", "esphome", "recording_sha256", "tolerance"} {
		t.Run("missing "+k, func(t *testing.T) {
			var kept []string
			for _, l := range strings.Split(body, "\n") {
				if !strings.HasPrefix(l, k+" =") {
					kept = append(kept, l)
				}
			}
			_, err := load(strings.Join(kept, "\n"))
			if fe := asError(t, err); fe.Key != "measured."+k || fe.Msg != "missing required key measured."+k {
				t.Errorf("error %v, want missing required key measured.%s", err, k)
			}
		})
		t.Run("empty "+k, func(t *testing.T) {
			b := strings.Replace(body, "\n"+k+" = \"", "\n"+k+" = \"\"\n#", 1)
			wantError(t, b, "measured."+k, lineIn(t, b, "measured", k), "must not be empty")
		})
	}
	t.Run("513 bytes", func(t *testing.T) {
		b := strings.Replace(body, "\"abc123\"", "\""+strings.Repeat("x", 513)+"\"", 1)
		wantError(t, b, "measured.recording_sha256", lineIn(t, b, "measured", "recording_sha256"), "512")
	})
	if _, err := load(v2base); err != nil {
		t.Errorf("no [measured] table: %v", err)
	}
}

func TestV2Adjacency(t *testing.T) {
	// A zero-width hysteresis band is fine.
	if _, err := load(v2with(t, "[start]\nduty = 0.10", "[start]\nduty = 0.05")); err != nil {
		t.Errorf("start.duty == stop.duty: %v", err)
	}
	// Equal consecutive duties are not (division by zero in Curve.At), at the line of that curve's duty.
	for _, c := range []string{"curve.up", "curve.down"} {
		body := inTable(t, c, "duty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]", "duty = [0.05, 0.20, 0.40, 0.40, 0.80, 1.00]")
		wantError(t, body, c+".duty", lineIn(t, body, c, "duty"), "strictly increasing")
	}
	// Equal consecutive rpm are.
	if _, err := load(inTable(t, "curve.down", "rpm = [300, 520, 900, 1200, 1480, 1700]", "rpm = [300, 520, 900, 900, 1480, 1700]")); err != nil {
		t.Errorf("flat rpm segment: %v", err)
	}

	// A repeated provenance key is a located TOML duplicate-key error.
	dup := v2base + "\"stop.duty\" = \"assumed\"\n"
	_, err := load(dup)
	if err == nil {
		t.Fatal("duplicate provenance key loaded")
	}
	if want := len(strings.Split(dup, "\n")) - 1; asError(t, err).Line != want || asError(t, err).Col == 0 {
		t.Errorf("duplicate key error %q at %d:%d, want line %d (the second entry) and a column", err, asError(t, err).Line, asError(t, err).Col, want)
	}

	// A provenance key equals a value path exactly: no case folding, no trimming.
	for _, key := range []string{"Stop.Duty", " stop.duty", "stop.duty ", "STOP.DUTY"} {
		body := v2base + "\"" + key + "\" = \"assumed\"\n"
		wantError(t, body, key, lineWith(t, body, "\""+key+"\" = \"assumed\""), "names no value")
	}
}

func TestV2Empty(t *testing.T) {
	upDuty := "[curve.up]\nduty = [0.05, 0.20, 0.40, 0.60, 0.80, 1.00]\nrpm = [300, 600, 1000, 1300, 1550, 1700]"
	for _, c := range []struct{ name, repl string }{
		{"zero points", "[curve.up]\nduty = []\nrpm = []"},
		{"one point", "[curve.up]\nduty = [0.5]\nrpm = [900]"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := v2with(t, upDuty, c.repl)
			wantError(t, body, "curve.up.duty", lineIn(t, body, "curve.up", "duty"), "curve.up needs at least 2 points")
		})
	}
	t.Run("no provenance table", func(t *testing.T) {
		_, err := load(v2base[:strings.Index(v2base, "[provenance]")])
		if fe := asError(t, err); fe.Key != "provenance" || fe.Msg != "missing required key provenance" {
			t.Errorf("error %v, want missing required key provenance", err)
		}
	})
	t.Run("empty kind", func(t *testing.T) {
		body := v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"\"")
		wantError(t, body, "stop.duty", lineWith(t, body, "\"stop.duty\" ="), "empty kind")
	})
	t.Run("kind with an empty id", func(t *testing.T) {
		body := v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"datasheet:\"")
		wantError(t, body, "stop.duty", lineWith(t, body, "\"stop.duty\" ="), "empty source id")
	})
	t.Run("an id with no kind", func(t *testing.T) {
		body := v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \":test\"")
		wantError(t, body, "stop.duty", lineWith(t, body, "\"stop.duty\" ="), "empty kind")
	})
	t.Run("no sources table and no source ids", func(t *testing.T) {
		body := strings.Replace(v2base, "[sources]\ntest = \"test fixture, not a real fan\"\n\n", "", 1)
		body = strings.ReplaceAll(body, "assumed:test", "assumed")
		p, err := load(body)
		if err != nil {
			t.Fatalf("a file without sources and without source ids: %v", err)
		}
		if len(p.Sources) != 0 {
			t.Errorf("Sources = %v, want empty", p.Sources)
		}
	})
}

func TestV2FirstErrorIsStable(t *testing.T) {
	// An unknown kind (rule f) and a NaN tau (rule b): the rule order decides, every time.
	body := v2with(t, "\"stop.duty\" = \"assumed:test\"", "\"stop.duty\" = \"guessed\"")
	body = strings.Replace(body, "tau_up_s = 0.5", "tau_up_s = nan", 1)
	// Two bad kinds: the sorted first path is reported.
	both := strings.Replace(v2base, "\"start.duty\" = \"assumed:test\"", "\"start.duty\" = \"nope\"", 1)
	both = strings.Replace(both, "\"curve.up\" = \"assumed:test\"", "\"curve.up\" = \"nope\"", 1)
	for i := 0; i < 25; i++ {
		_, err := load(body)
		if fe := asError(t, err); fe.Key != "dynamics.tau_up_s" || !strings.Contains(fe.Msg, "finite") {
			t.Fatalf("run %d: %v, want the NaN tau (rule b) first", i, err)
		}
		_, err = load(both)
		if fe := asError(t, err); fe.Key != "curve.up" {
			t.Fatalf("run %d: %v, want curve.up (sorted first of two bad kinds)", i, err)
		}
	}
}

func TestSchemaProbeVariants(t *testing.T) {
	// schema = 1 is the flat format, loaded by the same v1 path as a file without the key.
	p, err := load("schema = 1\n" + base)
	if err != nil || p.Schema != 1 || p.Name != "Test fan" || p.Source != "unit test fixture" {
		t.Errorf("schema = 1: %+v, %v, want the flat fan with Schema 1", p, err)
	}
	if p, err := load(base); err != nil || p.Schema != 1 {
		t.Errorf("no schema key: Schema %d, %v, want 1", p.Schema, err)
	}
	// An explicit schema = 1 is as strict as v1 always was.
	if _, err := load("schema = 1\n" + strings.Replace(base, "tau_up", "tau_upp", 1)); err == nil {
		t.Error("schema = 1 with a typo loaded")
	}
	// A schema 2 body under schema = 1 is not v2.
	if _, err := load(strings.Replace(v2base, "schema = 2", "schema = 1", 1)); err == nil {
		t.Error("schema 2 tables under schema = 1 loaded")
	}

	for _, c := range []struct{ schema, msg string }{
		{"3", "schema 3 needs a newer sil (this sil reads schema 1 and 2)"},
		{"100", "needs a newer sil"},
		{"0", "schema must be 1 or 2"},
		{"-1", "schema must be 1 or 2"},
	} {
		t.Run("schema "+c.schema, func(t *testing.T) {
			body := strings.Replace(v2base, "schema = 2", "schema = "+c.schema, 1)
			wantError(t, body, "schema", 1, c.msg)
		})
	}
}

// Prohibition (MODEL-12): loading never dereferences a [sources] entry, not even one that is a URL.
func TestV2LoadNeverFetchesASource(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	body := strings.Replace(v2base, "test fixture, not a real fan", srv.URL+"/datasheet.pdf", 1)
	p, err := load(body)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sources["test"] != srv.URL+"/datasheet.pdf" {
		t.Errorf("Sources = %v, want the URL kept as plain text", p.Sources)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the source URL was requested %d times while loading, want 0", n)
	}
}

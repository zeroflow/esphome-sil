package fwbuild

// Fixtures were captured once from the pinned image (ESPHome 2026.10.1), with
// the key replaced by the literal text DUMMYKEY (the real command prints the
// key wrapped in the literal characters \033[8m ... \033[28m):
//
//	export SIL_API_KEY=<a dummy key>
//	docker run --rm --network none --user $(id -u):$(id -g) \
//	  -e HOME=/cache/home -e ESPHOME_DATA_DIR=/build/data -e ESPHOME_NOGITIGNORE=1 -e SIL_API_KEY \
//	  -v $PWD:$PWD:ro -w $PWD/examples -v $T/build:/build -v $T/cache:/cache <Image> \
//	  -s sil_name e2e-lint -s sil_mac 02:aa:bb:cc:dd:01 -s sil_port 6054 config fan-skeleton.yaml
//	    -> testdata/resolved-example.yaml   (stdout from the first "substitutions:" line on)
//	... same flags with `compile --only-generate fan-skeleton.yaml`
//	    -> testdata/storage-example.json    (= $T/build/data/storage/fan-skeleton.yaml.json)
//
// The path of the local external component was rewritten to /repo/... so the
// fixture does not carry a machine-specific path.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

const lintConfigPath = "/repo/examples/fan-skeleton.yaml"

var lintWant = Identity{DeviceName: "sil-e2e-lint", MAC: "02:aa:bb:cc:dd:01", Port: 6054}

func lintFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

// lintProblems unwraps a lint_rejected error into its problems.
func lintProblems(t *testing.T, err error) []Problem {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("want *apperr.Error, got %T: %v", err, err)
	}
	if ae.Code != "lint_rejected" || ae.Exit != apperr.ExitFailed {
		t.Fatalf("want lint_rejected exit 1, got %s exit %d: %s", ae.Code, ae.Exit, ae.Message)
	}
	ps, ok := ae.Detail["problems"].([]Problem)
	if !ok || len(ps) == 0 {
		t.Fatalf("detail.problems missing: %#v", ae.Detail)
	}
	return ps
}

func lintComponents(ps []Problem) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.Component] = true
	}
	return m
}

func TestLintConfigAcceptsResolvedExample(t *testing.T) {
	t.Parallel()
	if err := LintConfig([]byte(lintFixture(t, "resolved-example.yaml")), lintConfigPath, lintWant); err != nil {
		t.Fatalf("LintConfig(resolved-example) = %v, want nil", err)
	}
}

func TestLintConfigToleratesEntrypointPreamble(t *testing.T) {
	t.Parallel()
	pre := "Creating cache directory /cache/platformio\nYou can change this behavior by mounting a directory to the container's /cache directory.\n"
	if err := LintConfig([]byte(pre+lintFixture(t, "resolved-example.yaml")), lintConfigPath, lintWant); err != nil {
		t.Fatalf("LintConfig with preamble = %v, want nil", err)
	}
}

func TestLintConfigBanned(t *testing.T) {
	t.Parallel()
	base := lintFixture(t, "resolved-example.yaml")
	cases := []struct {
		name, add, component string
	}{
		{"wifi", "wifi:\n  ssid: x\n", "wifi"},
		{"i2c", "i2c:\n  sda: 1\n", "i2c"},
		{"ota", "ota:\n  - platform: esphome\n", "ota"},
		{"http_request", "http_request:\n  timeout: 5s\n", "http_request"},
		{"update", "update:\n  - platform: http_request\n", "update"},
		{"captive_portal", "captive_portal:\n", "captive_portal"},
		{"web_server", "web_server:\n  port: 80\n", "web_server"},
		{"esp32", "esp32:\n  board: x\n", "esp32"},
		{"json", "json:\n", "json"},
		{"ledc", "output:\n  - platform: ledc\n    pin: 1\n", "ledc"},
		{"pulse_counter", "sensor:\n  - platform: pulse_counter\n    pin: 1\n", "pulse_counter"},
		{"esp32_rmt_led_strip", "light:\n  - platform: esp32_rmt_led_strip\n    pin: 1\n", "esp32_rmt_led_strip"},
		{"sntp", "time:\n  - platform: sntp\n", "sntp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ps := lintProblems(t, LintConfig([]byte(base+tc.add), lintConfigPath, lintWant))
			if !lintComponents(ps)[tc.component] {
				t.Fatalf("component %q not reported: %+v", tc.component, ps)
			}
			for _, p := range ps {
				if p.Component == tc.component && (p.File != lintConfigPath || p.Hint == "" || p.Message == "") {
					t.Fatalf("incomplete problem: %+v", p)
				}
			}
		})
	}
}

func TestLintConfigReportsAllAtOnce(t *testing.T) {
	t.Parallel()
	in := lintFixture(t, "resolved-example.yaml") + "wifi:\n  ssid: x\nota:\n  - platform: esphome\ni2c:\n  sda: 1\n"
	err := LintConfig([]byte(in), lintConfigPath, lintWant)
	ps := lintProblems(t, err)
	got := lintComponents(ps)
	for _, want := range []string{"wifi", "ota", "i2c"} {
		if !got[want] {
			t.Fatalf("%s missing from %+v", want, ps)
		}
	}
	msg := err.Error()
	if !strings.Contains(msg, lintConfigPath) || !strings.Contains(msg, "3 problem(s)") {
		t.Fatalf("message %q lacks the file or the count", msg)
	}
}

func TestLintConfigExactNamesOnly(t *testing.T) {
	t.Parallel()
	base := lintFixture(t, "resolved-example.yaml")
	for name, add := range map[string]string{
		"upper-case platform": "sensor:\n  - platform: WIFI\n",
		"longer key":          "wifi_extra:\n  a: 1\n",
		"longer platform":     "text_sensor:\n  - platform: wifi_info\n",
		"homeassistant time":  "time:\n  - platform: homeassistant\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := LintConfig([]byte(base+add), lintConfigPath, lintWant); err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func TestLintConfigIdentityAndApi(t *testing.T) {
	t.Parallel()
	base := lintFixture(t, "resolved-example.yaml")
	edit := func(old, repl string) string {
		if !strings.Contains(base, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.Replace(base, old, repl, 1)
	}
	cases := []struct {
		name, in, component string
		ok                  bool
	}{
		{"reboot_timeout 15min", edit("reboot_timeout: 0s", "reboot_timeout: 15min"), "api.reboot_timeout", false},
		{"reboot_timeout 0ms", edit("reboot_timeout: 0s", "reboot_timeout: 0ms"), "", true},
		{"reboot_timeout 0min", edit("reboot_timeout: 0s", "reboot_timeout: 0min"), "", true},
		{"reboot_timeout garbage", edit("reboot_timeout: 0s", "reboot_timeout: soon"), "api.reboot_timeout", false},
		{"missing key", edit("    key: \\033[8mDUMMYKEY\\033[28m\n", "    other: 1\n"), "api.encryption", false},
		{"no -static", edit("    - -static\n", "    - -O2\n"), "esphome.build_flags", false},
		{"other name", edit("name: sil-e2e-lint", "name: sil-other"), "esphome.name", false},
		{"mac case-insensitive", edit("02:AA:BB:CC:DD:01", "02:AA:bb:CC:dd:01"), "", true},
		{"other mac", edit("02:AA:BB:CC:DD:01", "02:AA:BB:CC:DD:02"), "host.mac_address", false},
		{"other port", edit("  port: 6054\n", "  port: 6099\n"), "api.port", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := LintConfig([]byte(tc.in), lintConfigPath, lintWant)
			if tc.ok {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			ps := lintProblems(t, err)
			if !lintComponents(ps)[tc.component] {
				t.Fatalf("%s not reported: %+v", tc.component, ps)
			}
		})
	}
}

func TestLintConfigNoApiBlock(t *testing.T) {
	t.Parallel()
	in := "esphome:\n  name: sil-e2e-lint\n  build_flags:\n    - -static\nhost:\n  mac_address: 02:AA:BB:CC:DD:01\n"
	got := lintComponents(lintProblems(t, LintConfig([]byte(in), lintConfigPath, lintWant)))
	for _, want := range []string{"api.encryption", "api.reboot_timeout"} {
		if !got[want] {
			t.Fatalf("%s not reported: %v", want, got)
		}
	}
}

func TestLintConfigCustomTags(t *testing.T) {
	t.Parallel()
	in := lintFixture(t, "resolved-example.yaml") + "interval:\n  - interval: 1s\n    then:\n      - lambda: !lambda |-\n          id(x).publish_state(1);\n    extra: !secret thing\n"
	if err := LintConfig([]byte(in), lintConfigPath, lintWant); err != nil {
		t.Fatalf("custom tags must decode: %v", err)
	}
}

func TestLintConfigGarbage(t *testing.T) {
	t.Parallel()
	err := LintConfig([]byte("a: [unclosed\n"), lintConfigPath, lintWant)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "config_invalid" {
		t.Fatalf("want config_invalid, got %v", err)
	}
}

func TestLintConfigIdempotent(t *testing.T) {
	t.Parallel()
	in := []byte(lintFixture(t, "resolved-example.yaml") + "wifi:\n  ssid: x\nota:\n  - platform: esphome\n")
	a := LintConfig(in, lintConfigPath, lintWant)
	b := LintConfig(in, lintConfigPath, lintWant)
	if a == nil || b == nil || a.Error() != b.Error() {
		t.Fatalf("not idempotent: %v / %v", a, b)
	}
	var ea, eb *apperr.Error
	errors.As(a, &ea)
	errors.As(b, &eb)
	if len(ea.Detail["problems"].([]Problem)) != len(eb.Detail["problems"].([]Problem)) {
		t.Fatalf("problem lists differ")
	}
}

func TestLintStorage(t *testing.T) {
	t.Parallel()
	raw := lintFixture(t, "storage-example.json")
	if err := LintStorage([]byte(raw), lintConfigPath); err != nil {
		t.Fatalf("LintStorage(example) = %v, want nil", err)
	}
	t.Run("json auto-loaded", func(t *testing.T) {
		t.Parallel()
		in := strings.Replace(raw, `"logger",`, `"json",
    "logger",`, 1)
		ps := lintProblems(t, LintStorage([]byte(in), lintConfigPath))
		if !lintComponents(ps)["json"] {
			t.Fatalf("json not reported: %+v", ps)
		}
	})
	t.Run("mqtt and json", func(t *testing.T) {
		t.Parallel()
		in := strings.Replace(raw, `"logger",`, `"json",
    "logger",
    "mqtt",`, 1)
		ps := lintProblems(t, LintStorage([]byte(in), lintConfigPath))
		if !lintComponents(ps)["json"] {
			t.Fatalf("json not reported: %+v", ps)
		}
	})
	t.Run("banned platform", func(t *testing.T) {
		t.Parallel()
		in := strings.Replace(raw, `"fan/speed",`, `"fan/speed",
    "output/ledc",`, 1)
		ps := lintProblems(t, LintStorage([]byte(in), lintConfigPath))
		if !lintComponents(ps)["ledc"] {
			t.Fatalf("ledc not reported: %+v", ps)
		}
	})
	t.Run("wifi auto-loaded", func(t *testing.T) {
		t.Parallel()
		in := strings.Replace(raw, `"logger",`, `"logger",
    "wifi",`, 1)
		if !lintComponents(lintProblems(t, LintStorage([]byte(in), lintConfigPath)))["wifi"] {
			t.Fatal("wifi not reported")
		}
	})
	t.Run("not json", func(t *testing.T) {
		t.Parallel()
		if err := LintStorage([]byte("{"), lintConfigPath); err == nil {
			t.Fatal("want an error for broken storage JSON")
		}
	})
}

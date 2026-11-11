package fanlib

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/model"
)

// v010Fixtures are the v0.1.0 shipped fan files, kept byte for byte (D-01), with the sha256 of
// `git show v0.1.0:fans/<file>`. They are the reference of every exact-rpm test once the shipped
// files change, so a silent edit must fail here.
var v010Fixtures = map[string]string{
	"arctic-p12-pwm-pst.toml":   "d9fb55c79cec165d3208dfadaa9bcdece060572523d9efd002ed9312ef101215",
	"noctua-nf-a12x25-pwm.toml": "d7dacb32bebc2a3dab2021d01eeb83e4351d84d10d66e53ad2ca0a97b8f5013f",
}

func v010Dir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fans-v0.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestV010FixturesArePinned(t *testing.T) {
	dir := v010Dir(t)
	for name, want := range v010Fixtures {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s sha256 = %s, want %s (the v0.1.0 bytes)", name, got, want)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != len(v010Fixtures) {
		t.Errorf("%s holds %d entries, want exactly the %d pinned files", dir, len(ents), len(v010Fixtures))
	}
}

func TestV010FixturesLoadAsV010(t *testing.T) {
	t.Setenv(EnvFanDir, v010Dir(t))
	ids, err := IDs()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"arctic-p12-pwm-pst", "noctua-nf-a12x25-pwm"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("IDs() = %v, want %v", ids, want)
	}
	cases := []struct {
		id                 string
		minDuty, startDuty float64
		belowMin           string
		minRPM, maxRPM     float64
		tauUp, tauDown     float64
	}{
		{"arctic-p12-pwm-pst", 0.05, 0.10, "stop", 200, 1800, 1.0, 2.5},
		{"noctua-nf-a12x25-pwm", 0.20, 0.20, "hold", 450, 2000, 1.0, 2.5},
	}
	for _, c := range cases {
		p, err := Load(c.id)
		if err != nil {
			t.Fatalf("Load(%s): %v", c.id, err)
		}
		if p.MinDuty != c.minDuty || p.StartDuty != c.startDuty || p.BelowMin != c.belowMin ||
			p.MinRPM != c.minRPM || p.MaxRPM != c.maxRPM || p.TauUp != c.tauUp || p.TauDown != c.tauDown {
			t.Errorf("%s = %+v, want the v0.1.0 parameters %+v", c.id, p, c)
		}
		if p.Schema > 1 {
			t.Errorf("%s Schema = %d, want <= 1 (flat v0.1.0 format)", c.id, p.Schema)
		}
		if len(p.Curve.Up) != 0 || len(p.Curve.Down) != 0 || p.Tach != (model.TachParams{}) ||
			p.Datasheet != nil || p.Provenance != nil || p.StartDelay != 0 || p.CoastTau != 0 {
			t.Errorf("%s carries schema 2 data: %+v", c.id, p)
		}
	}
}

//go:build sil_e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/fwbuild"
)

var ledsHexColour = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// ledsRGB splits a "#rrggbb" strip entry.
func ledsRGB(t testing.TB, c string) (r, g, b int) {
	t.Helper()
	if !ledsHexColour.MatchString(c) {
		t.Fatalf("strip entry %q is not #rrggbb in lowercase hex", c)
	}
	v, _ := strconv.ParseUint(c[1:], 16, 32)
	return int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff)
}

// ledsDominant classifies a colour by its dominant channel and a threshold, never by an exact
// value: ESPHome applies brightness, gamma correction and effect curves (Research Pitfall 5).
// want is 'r', 'g' or 'b'; the other two channels must be 0 and the wanted one at least 0x20.
func ledsDominant(t testing.TB, c string, want byte) bool {
	t.Helper()
	r, g, b := ledsRGB(t, c)
	switch want {
	case 'r':
		return r >= 0x20 && g == 0 && b == 0
	case 'g':
		return g >= 0x20 && r == 0 && b == 0
	default:
		return b >= 0x20 && r == 0 && g == 0
	}
}

// ledsStrip returns leds.strip of a status object.
func ledsStrip(st map[string]any) []string {
	list, _ := jget(st, "leds", "strip").([]any)
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

// ledsOff reports whether strip[from..to] are all #000000 in st.
func ledsOff(st map[string]any, from, to int) bool {
	strip := ledsStrip(st)
	if len(strip) <= to {
		return false
	}
	for _, c := range strip[from : to+1] {
		if c != "#000000" {
			return false
		}
	}
	return true
}

// ledsBuild compiles yamlPath for id in the pinned image like buildFirmware does for the skeleton.
func ledsBuild(t testing.TB, id fwIdentity, yamlPath string) string {
	t.Helper()
	root := repoRoot(t)
	logPath := filepath.Join(t.TempDir(), "build.log")
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 25*time.Minute)
	defer cancel()
	res, err := fwbuild.Build(ctx, fwbuild.Options{
		Instance:   id.Name,
		ConfigPath: yamlPath,
		RepoRoot:   root,
		CacheDir:   filepath.Join(cacheRoot(t), "sil", "build", id.Name),
		Substitutions: map[string]string{
			"sil_name": id.Name,
			"sil_mac":  id.MAC,
			"sil_port": fmt.Sprint(id.Port),
		},
		APIKey:   id.Key,
		LogPath:  logPath,
		Progress: func(msg string) { t.Log(msg) },
	})
	if err != nil {
		t.Fatalf("fwbuild.Build %s: %v", filepath.Base(yamlPath), err)
	}
	return res.Program
}

// TestLEDStatus runs the real Rev 3.3 firmware under sil and reads its LED strip from `sil status`:
// the segment names come from the firmware's partition lights, the board LED pulses blue until a
// real API client connects (sil's own readiness probe does not count), and each fan LED follows
// that fan's RPM.
func TestLEDStatus(t *testing.T) {
	in := r33Up(t, "e2e-leds")

	st := in.env.Status(t, in.name)
	wantSegs := map[string][]int{"board": {0}, "fan1": {1}, "fan2": {2}, "fan3": {3}, "fan4": {4}}
	got := map[string][]int{}
	raw, _ := json.Marshal(jget(st, "leds", "segments"))
	if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, wantSegs) {
		t.Fatalf("leds.segments = %s, want exactly %v", raw, wantSegs)
	}
	if strip := ledsStrip(st); len(strip) != 5 {
		t.Fatalf("leds.strip = %v, want 5 entries", strip)
	} else {
		for _, c := range strip {
			ledsRGB(t, c)
		}
	}
	if w, _ := st["warnings"].([]any); len(w) != 0 {
		t.Fatalf("warnings = %v, want none", w)
	}

	// Boot: the board LED pulses blue because sil's readiness probe (client_info "sil") does not
	// turn it green; the fan LEDs are off.
	blue := false
	var last map[string]any
	waitFor(t, 3*time.Second, "the board LED to show the blue boot pulse", func() bool {
		last = in.env.Status(t, in.name)
		strip := ledsStrip(last)
		if len(strip) == 5 && ledsDominant(t, strip[0], 'b') {
			blue = true
			return true
		}
		return false
	})
	if !blue || !ledsOff(last, 1, 4) {
		t.Fatalf("boot strip = %v, want a blue board LED and fan LEDs off", ledsStrip(last))
	}

	// A real API client connects: the board LED turns static green.
	in.setFan(t, 1, 40)
	waitFor(t, 3*time.Second, "the board LED to turn green", func() bool {
		strip := ledsStrip(in.env.Status(t, in.name))
		return len(strip) == 5 && ledsDominant(t, strip[0], 'g')
	})
	a := ledsStrip(in.env.Status(t, in.name))
	if err := sysClock.Sleep(t.Context(), 500*time.Millisecond); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	b := ledsStrip(in.env.Status(t, in.name))
	if !reflect.DeepEqual(a[:1], b[:1]) {
		t.Errorf("board LED changed between two reads 500 ms apart: %v then %v (want static green)", a[0], b[0])
	}

	// Fan 1 spins up: only its LED turns green.
	st = in.env.WaitStatus(t, in.name, 8*time.Second, func(st map[string]any) bool {
		strip := ledsStrip(st)
		return len(strip) == 5 && ledsDominant(t, strip[1], 'g')
	})
	if !ledsOff(st, 2, 4) {
		t.Errorf("strip = %v, want fans 2-4 off while only fan 1 runs", ledsStrip(st))
	}
}

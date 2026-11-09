//go:build sil_e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
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
	// The colour is "dominant green" while the default 1 s fade-in is still running (gamma makes its
	// start slow), so give the fade time to finish; then the LED must stand still.
	var a, b []string
	for try := 0; try < 6; try++ {
		a = ledsStrip(in.env.Status(t, in.name))
		if err := sysClock.Sleep(t.Context(), 500*time.Millisecond); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		b = ledsStrip(in.env.Status(t, in.name))
		if reflect.DeepEqual(a[:1], b[:1]) {
			break
		}
	}
	if !reflect.DeepEqual(a[:1], b[:1]) {
		t.Errorf("board LED still changing 3 s after it turned green: %v then %v (want static green)", a[0], b[0])
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

// ledsOut returns the leds out frames received on connection conn, in arrival order.
func ledsOut(p *ioPeer, conn int) []ioFrame {
	var out []ioFrame
	for _, f := range ioConnFrames(p, conn) {
		if f.M["k"] == "out" && f.M["ch"] == "leds" {
			out = append(out, f)
		}
	}
	return out
}

// ledsPx is the px of a leds frame and its firmware timestamp t in microseconds.
func ledsPx(f ioFrame) (px string, tUS float64) {
	px, _ = f.M["px"].(string)
	tUS, _ = f.M["t"].(float64)
	return px, tUS
}

// ledsFirstPixel is "#rrggbb" of the first LED of a px string.
func ledsFirstPixel(px string) string {
	if len(px) < 6 {
		return px
	}
	return "#" + px[:6]
}

// ledsClient connects a real API client once the firmware's API is up, so the board LED turns
// green (the client's client_info is not "sil").
func ledsClient(t testing.TB, id fwIdentity) {
	t.Helper()
	waitFor(t, 30*time.Second, "an API client to connect", func() bool {
		_, code := runAPIClient(t, id.Port, id.Key, "info")
		return code == 0
	})
}

// ledsStart runs the firmware program against a fresh fake peer.
func ledsStart(t testing.TB, program string) *ioPeer {
	t.Helper()
	dir := shortTempDir(t)
	busPath := filepath.Join(dir, "bus.sock")
	peer := ioListen(t, busPath)
	startFirmware(t, program, busPath, filepath.Join(dir, "prefs"))
	peer.Wait(15*time.Second, "sync on connection 1", func(f ioFrame) bool { return f.Conn == 1 && f.M["k"] == "sync" })
	return peer
}

// ledsCheckRate asserts the D-07 rules on the frames of a window: enough frames, every px 30
// lowercase hex characters, every frame different from the previous one, consecutive firmware
// timestamps at least 50000 us apart. It returns the measured rate in frames per second.
func ledsCheckRate(t testing.TB, frames []ioFrame, minFrames int) float64 {
	t.Helper()
	if len(frames) < minFrames {
		t.Fatalf("%d leds frames, want at least %d", len(frames), minFrames)
	}
	hex30 := regexp.MustCompile(`^[0-9a-f]{30}$`)
	var prevPx string
	var prevT float64
	for i, f := range frames {
		px, ts := ledsPx(f)
		if !hex30.MatchString(px) {
			t.Fatalf("frame %d px = %q, want 30 lowercase hex characters", i, px)
		}
		if i > 0 {
			if px == prevPx {
				t.Errorf("frame %d repeats the px of frame %d (%s): not change-only", i, i-1, px)
			}
			if gap := ts - prevT; gap < 50000 {
				t.Errorf("frame %d is %.0f us after frame %d, want >= 50000 (20 Hz cap)", i, gap, i-1)
			}
		}
		prevPx, prevT = px, ts
	}
	_, first := ledsPx(frames[0])
	_, last := ledsPx(frames[len(frames)-1])
	if last <= first {
		return 0
	}
	return float64(len(frames)-1) / ((last - first) / 1e6)
}

// ledsRev33With writes a copy of examples/rev33.yaml with old replaced by replacement and returns
// its path. The copy sits next to the original, so its relative component path and the build's
// read-only mount of the repo keep working; the name matches the *_dev.yaml pattern of .gitignore
// and the file is removed when the test ends.
func ledsRev33With(t testing.TB, old, replacement string) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "examples")
	raw, err := os.ReadFile(filepath.Join(dir, "rev33.yaml"))
	if err != nil {
		t.Fatalf("read rev33.yaml: %v", err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("rev33.yaml no longer contains %q", old)
	}
	f, err := os.CreateTemp(dir, "rev33-*_dev.yaml")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	if _, err := f.WriteString(strings.Replace(string(raw), old, replacement, 1)); err != nil {
		t.Fatalf("write variant: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close variant: %v", err)
	}
	return f.Name()
}

// TestLEDFrames drives the real Rev 3.3 firmware from a fake simulator (no sil involved) and
// checks the LED frames on the wire: the manifest, the 20 Hz cap with change-only frames during a
// pulse and during another effect, silence when the strip is static, and the replay on reconnect.
func TestLEDFrames(t *testing.T) {
	id := newFWIdentity(t, "e2e-ledsfw")
	program := ledsBuild(t, id, filepath.Join(repoRoot(t), "examples", "rev33.yaml"))

	t.Run("manifest", func(t *testing.T) {
		peer := ledsStart(t, program)
		var leds ioFrame
		for _, f := range ioConnFrames(peer, 1) {
			if f.M["k"] != "chan" {
				continue
			}
			if f.M["ch"] == "leds" {
				leds = f
				continue
			}
			if _, ok := f.M["n"]; ok {
				t.Errorf("chan %v carries n; only the rgb channel may", f.M["ch"])
			}
			if _, ok := f.M["seg"]; ok {
				t.Errorf("chan %v carries seg; only the rgb channel may", f.M["ch"])
			}
		}
		want := map[string]any{
			"k": "chan", "ch": "leds", "dir": "out", "type": "rgb", "n": float64(5),
			"seg": "board:0:0,fan1:1:1,fan2:2:2,fan3:3:3,fan4:4:4",
		}
		if !reflect.DeepEqual(leds.M, want) {
			t.Fatalf("leds chan frame = %v, want %v", leds.M, want)
		}
	})

	t.Run("pulse_rate", func(t *testing.T) {
		peer := ledsStart(t, program)
		if err := sysClock.Sleep(t.Context(), 4*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		frames := ledsOut(peer, 1)
		rate := ledsCheckRate(t, frames, 10)
		t.Logf("boot pulse: %d leds frames in the first 4 s of the firmware, %.1f frames/s", len(frames), rate)
		if rate > 20.01 {
			t.Errorf("measured %.1f frames/s, want at most 20", rate)
		}
	})

	t.Run("idle_silence", func(t *testing.T) {
		peer := ledsStart(t, program)
		ledsClient(t, id)
		if err := sysClock.Sleep(t.Context(), 3*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		before := ledsOut(peer, 1)
		if err := sysClock.Sleep(t.Context(), 5*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		after := ledsOut(peer, 1)
		t.Logf("idle window of 5 s: %d leds frames (%d before it)", len(after)-len(before), len(before))
		if len(after) != len(before) {
			t.Fatalf("%d leds frames arrived in 5 s of an idle firmware, want 0", len(after)-len(before))
		}
		px, _ := ledsPx(after[len(after)-1])
		if !ledsDominant(t, ledsFirstPixel(px), 'g') {
			t.Errorf("last leds frame before the silence has first pixel %s, want green-dominant (the final colour was flushed)", ledsFirstPixel(px))
		}
	})

	t.Run("replay", func(t *testing.T) {
		peer := ledsStart(t, program)
		ledsClient(t, id)
		var last string
		waitFor(t, 15*time.Second, "a green strip with no leds frame for 1 s", func() bool {
			frames := ledsOut(peer, 1)
			if len(frames) == 0 {
				return false
			}
			px, _ := ledsPx(frames[len(frames)-1])
			if !ledsDominant(t, ledsFirstPixel(px), 'g') || sysClock.Now()-frames[len(frames)-1].At < time.Second {
				return false
			}
			last = px
			return true
		})
		peer.Down()
		if err := sysClock.Sleep(t.Context(), 300*time.Millisecond); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		peer.Up()
		peer.Wait(10*time.Second, "sync on connection 2", func(f ioFrame) bool { return f.Conn == 2 && f.M["k"] == "sync" })

		var between []ioFrame
		seenHello := false
		for _, f := range ioConnFrames(peer, 2) {
			if f.M["k"] == "hello" {
				seenHello = true
			}
			if f.M["k"] == "sync" {
				break
			}
			if seenHello && f.M["k"] == "out" && f.M["ch"] == "leds" {
				between = append(between, f)
			}
		}
		if len(between) != 1 {
			t.Fatalf("%d leds frames between hello and sync on connection 2, want exactly 1", len(between))
		}
		if px, _ := ledsPx(between[0]); px != last {
			t.Errorf("replayed px = %s, want the last px of connection 1 (%s)", px, last)
		}
		// The strip did not change, so nothing follows the replay.
		if err := sysClock.Sleep(t.Context(), 2*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		if n := len(ledsOut(peer, 2)); n != 1 {
			t.Errorf("%d leds frames on connection 2, want only the replay", n)
		}
	})

	// Effects other than pulse obey the same rules: the board partition runs addressable_rainbow,
	// (its effect name is "Rainbow") which rewrites the strip about every 32 ms.
	t.Run("rainbow_rate", func(t *testing.T) {
		rid := newFWIdentity(t, "e2e-ledsfw-rb")
		variant := ledsRev33With(t, `call.set_effect("pulse");`, `call.set_effect("rainbow");`)
		peer := ledsStart(t, ledsBuild(t, rid, variant))
		if err := sysClock.Sleep(t.Context(), 4*time.Second); err != nil {
			t.Fatalf("sleep: %v", err)
		}
		frames := ledsOut(peer, 1)
		rate := ledsCheckRate(t, frames, 20)
		t.Logf("addressable_rainbow: %d leds frames in the first 4 s of the firmware, %.1f frames/s", len(frames), rate)
		if rate > 20.01 {
			t.Errorf("measured %.1f frames/s, want at most 20", rate)
		}
		// Only the board LED (pixel 0) is lit; the other four stay off in every frame.
		for i, f := range frames {
			if px, _ := ledsPx(f); px[6:] != strings.Repeat("0", 24) {
				t.Fatalf("frame %d = %s: LEDs 1-4 should stay off while only the board partition runs", i, px)
			}
		}
	})
}

//go:build sil_ui

package uitest

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// gammaRGB is the UI-SPEC display gamma of a "#rrggbb" string, written independently of view.js:
// per channel round(255 * (c/255)^(1/2.2)), as the CSS rgb() text a computed style reports.
func gammaRGB(hex string) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		panic(err)
	}
	f := func(c int) int { return int(math.Round(255 * math.Pow(float64(c)/255, 1/2.2))) }
	return fmt.Sprintf("rgb(%d, %d, %d)", f(r), f(g), f(b))
}

// lensInfo is what one LED column shows.
type lensInfo struct {
	Background string `json:"background"`
	Shadow     string `json:"shadow"`
	Rim        string `json:"rim"`
	Caption    string `json:"caption"`
	Title      string `json:"title"`
	Hex        string `json:"hex"`
	HexSrc     string `json:"hexSrc"`
	HexFmt     string `json:"hexFmt"`
	Aria       string `json:"aria"`
	Role       string `json:"role"`
}

const readLensesJS = `[...document.querySelectorAll('#leds .led')].map((c) => {
  const lens = c.querySelector('.lens'), cap = c.querySelector('.led-caption'), hex = c.querySelector('.led-hex');
  const cs = getComputedStyle(lens);
  return {background: cs.backgroundColor, shadow: cs.boxShadow, rim: cs.borderTopColor,
    caption: cap.textContent, title: cap.title, hex: hex.textContent,
    hexSrc: hex.dataset.src || '', hexFmt: hex.dataset.fmt || '',
    aria: lens.getAttribute('aria-label') || '', role: lens.getAttribute('role') || ''};
})`

func readLenses(t *testing.T, p *cdptest.Page) []lensInfo {
	t.Helper()
	var out []lensInfo
	if err := p.Eval(within(t, 10*time.Second), readLensesJS, &out); err != nil {
		t.Fatalf("reading the lenses: %v", err)
	}
	return out
}

// ledBench is a bench whose strip and segments are the plan's example.
func ledBench(t *testing.T, strip []string) *bench {
	b := newBench(t)
	b.Mutate(func(st *control.Status) {
		st.LEDs.Strip = strip
		st.LEDs.Segments = map[string][]int{"board": {0}, "status": {0, 1}, "fan2": {2, 3}}
	})
	return b
}

// waitLens waits until lens i shows background want.
func waitLens(t *testing.T, p *cdptest.Page, i int, want string) {
	t.Helper()
	pred := fmt.Sprintf("(() => { const l = document.querySelectorAll('#leds .lens')[%d]; return !!l && getComputedStyle(l).backgroundColor === %q; })()", i, want)
	if err := p.WaitFor(within(t, 5*time.Second), pred); err != nil {
		t.Fatalf("lens %d never showed %s: %v (lenses %+v)", i, want, err, readLenses(t, p))
	}
}

// TestLEDs: one round lens per strip entry, lit with the display-gamma colour and a glow, dark for
// #000000, with the segment names as caption and the raw hex string under it (UI-03, UIC-14/15/17).
func TestLEDs(t *testing.T) {
	strip := []string{"#ff0000", "#000000", "#100800", "#00ff00", "#0000ff"}
	b := ledBench(t, strip)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)
	waitLens(t, p, 2, gammaRGB("#100800"))

	lenses := readLenses(t, p)
	if len(lenses) != len(strip) {
		t.Fatalf("%d lenses, want %d: %+v", len(lenses), len(strip), lenses)
	}
	wantCaption := []string{"board, status", "status", "fan2", "fan2", "—"}
	for i, l := range lenses {
		if l.Hex != strip[i] {
			t.Errorf("lens %d hex = %q, want the bench string %q", i, l.Hex, strip[i])
		}
		if l.HexSrc != fmt.Sprintf("leds.strip.%d", i) || l.HexFmt != "hex" {
			t.Errorf("lens %d hex span has data-src %q data-fmt %q, want leds.strip.%d and hex", i, l.HexSrc, l.HexFmt, i)
		}
		if l.Caption != wantCaption[i] {
			t.Errorf("lens %d caption = %q, want %q", i, l.Caption, wantCaption[i])
		}
		if l.Title != wantCaption[i] {
			t.Errorf("lens %d caption title = %q, want the full name %q", i, l.Title, wantCaption[i])
		}
		if want := wantCaption[i] + " " + strip[i]; l.Aria != want {
			t.Errorf("lens %d aria-label = %q, want %q", i, l.Aria, want)
		}
		if l.Role != "img" {
			t.Errorf("lens %d role = %q, want img", i, l.Role)
		}
		if l.Rim != "rgb(42, 46, 51)" {
			t.Errorf("lens %d rim = %s, want #2a2e33", i, l.Rim)
		}
		if i == 1 {
			continue
		}
		if want := gammaRGB(strip[i]); l.Background != want {
			t.Errorf("lens %d background = %s, want the gamma colour %s", i, l.Background, want)
		}
		if l.Shadow == "none" || l.Shadow == "" {
			t.Errorf("lens %d is lit but has no glow (%q)", i, l.Shadow)
		}
	}
	if lenses[1].Background != "rgb(18, 20, 22)" || lenses[1].Shadow != "none" {
		t.Errorf("the #000000 lens = %s / %s, want the dark lens rgb(18, 20, 22) without glow", lenses[1].Background, lenses[1].Shadow)
	}
	// A dim value is visibly dim, not black: #100800 has gamma red 66, well above the dark lens.
	if lenses[2].Background == "rgb(18, 20, 22)" || lenses[2].Background == "rgb(0, 0, 0)" {
		t.Errorf("the dim lens %s is not visibly lit", lenses[2].Background)
	}

	// The strip changes: the lenses repaint, and a lens switched off loses its glow.
	next := []string{"#00ff00", "#ff00ff", "#000000", "#ffffff", "#000000"}
	b.Emit(events.Event{K: "leds", Strip: next}, func(st *control.Status) { st.LEDs.Strip = next })
	waitLens(t, p, 0, gammaRGB("#00ff00"))
	waitLens(t, p, 1, gammaRGB("#ff00ff"))
	waitLens(t, p, 2, "rgb(18, 20, 22)")
	waitLens(t, p, 3, "rgb(255, 255, 255)")
	lenses = readLenses(t, p)
	for i, l := range lenses {
		if l.Hex != next[i] {
			t.Errorf("after the event lens %d hex = %q, want %q", i, l.Hex, next[i])
		}
		if want := wantCaption[i] + " " + next[i]; l.Aria != want {
			t.Errorf("after the event lens %d aria-label = %q, want %q", i, l.Aria, want)
		}
	}
	if lenses[2].Shadow != "none" || lenses[1].Shadow == "none" {
		t.Errorf("glow after the event: lens 1 %q lens 2 %q", lenses[1].Shadow, lenses[2].Shadow)
	}
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		t.Errorf("console errors: %v", errs)
	}
}

// TestLEDCount: the lens count follows leds.strip, never a fixed number (UIC-17): none, one, many.
func TestLEDCount(t *testing.T) {
	for _, n := range []int{0, 1, 8} {
		strip := make([]string, n)
		for i := range strip {
			strip[i] = "#102030"
		}
		b := ledBench(t, strip)
		p := openPage(t, newSite(t, b))
		waitText(t, p, "#link", "LIVE", 15*time.Second)
		if err := p.WaitFor(within(t, 5*time.Second), fmt.Sprintf("document.querySelectorAll('#leds .led [data-src]').length === %d", n)); err != nil {
			t.Errorf("strip of %d: %v", n, err)
		}
		var lenses int
		if err := p.Eval(within(t, 5*time.Second), `document.querySelectorAll('#leds .lens').length`, &lenses); err != nil || lenses != n {
			t.Errorf("strip of %d: %d lenses (%v)", n, lenses, err)
		}
	}
}

// TestLEDsConnecting: before the first snapshot the well shows one dark lens with an em dash and
// #------, none of it a value element; the snapshot replaces it by one lens per strip entry.
func TestLEDsConnecting(t *testing.T) {
	b := ledBench(t, []string{"#ff0000", "#000000", "#100800", "#00ff00", "#0000ff"})
	b.HoldSnapshot()
	base := newSite(t, b)
	p := openPage(t, base)
	b.waitCalls(t, "Watch", 1, 10*time.Second)
	if err := sysClock.Sleep(within(t, 5*time.Second), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	lenses := readLenses(t, p)
	if len(lenses) != 1 {
		t.Fatalf("%d lenses before the first snapshot, want 1: %+v", len(lenses), lenses)
	}
	l := lenses[0]
	if l.Caption != "—" || l.Hex != "#------" {
		t.Errorf("placeholder shows %q / %q, want — / #------", l.Caption, l.Hex)
	}
	if l.Background != "rgb(18, 20, 22)" || l.Shadow != "none" {
		t.Errorf("placeholder lens = %s / %s, want the dark lens without glow", l.Background, l.Shadow)
	}
	var valued int
	if err := p.Eval(within(t, 5*time.Second), `document.querySelectorAll('#leds [data-src], #leds [data-fmt]').length`, &valued); err != nil || valued != 0 {
		t.Fatalf("%d value attributes inside #leds before the first snapshot (%v), want 0", valued, err)
	}

	b.ReleaseSnapshot()
	waitText(t, p, "#link", "LIVE", 10*time.Second)
	waitLens(t, p, 4, gammaRGB("#0000ff"))
	lenses = readLenses(t, p)
	if len(lenses) != 5 {
		t.Fatalf("%d lenses after the snapshot, want 5", len(lenses))
	}
	for i, l := range lenses {
		if want := fmt.Sprintf("leds.strip.%d", i); l.HexSrc != want {
			t.Errorf("lens %d hex data-src = %q, want %q", i, l.HexSrc, want)
		}
	}
}

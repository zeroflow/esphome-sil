//go:build sil_ui

// Package uitest holds the browser tests of the web page: headless Chrome with the network blocked
// loads the page from the real web handler over a fake bench, and the tests read what the page
// shows. No docker, no firmware; run them with `make test-ui`.
package uitest

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/cdptest"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/web"
	"github.com/zeroflow/esphome-sil/ui"
)

// sysClock is the one clock of the uitest package (factor 1).
var sysClock = clock.NewReal()

// chrome is the one browser of the run: offline, 1280 x 720.
var chrome *cdptest.Browser

// TestMain launches Chrome once. It fails fast, with a clear message, when Chrome is missing, so
// the suite never passes without a browser.
func TestMain(m *testing.M) {
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 60*time.Second)
	b, err := cdptest.Launch(ctx, sysClock, cdptest.LaunchOptions{Offline: true, Width: 1280, Height: 720})
	cancel()
	if errors.Is(err, cdptest.ErrNoChrome) {
		fmt.Fprintln(os.Stderr, "uitest: Chrome not found; install google-chrome or set SIL_CHROME")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "uitest: launching Chrome failed: %v\n", err)
		os.Exit(1)
	}
	chrome = b
	code := m.Run()
	chrome.Close()
	os.Exit(code)
}

// newSite serves the real web handler (the embedded page, /ws and the API) over c on 127.0.0.1,
// behind the production guard (Host rules, CSRF and origin checks, and the Content-Security-Policy
// every browser test therefore runs under), and returns its base URL.
func newSite(t *testing.T, c control.Control) string {
	t.Helper()
	srv := httptest.NewServer(web.Guard(web.NewHandler(c, web.Options{Static: ui.FS(), Clock: sysClock}), web.GuardOptions{}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// openPage opens a tab on base and waits for its load event.
func openPage(t *testing.T, base string) *cdptest.Page {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 20*time.Second)
	defer cancel()
	p, err := chrome.NewPage(ctx)
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Navigate(ctx, base+"/"); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	return p
}

// within returns a context that ends after d on sysClock.
func within(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, d)
	t.Cleanup(cancel)
	return ctx
}

// textOf returns the text of the first element matching selector ("" when there is none).
func textOf(t *testing.T, p *cdptest.Page, selector string) string {
	t.Helper()
	var s string
	js := fmt.Sprintf("(() => { const e = document.querySelector(%q); return e ? e.textContent : ''; })()", selector)
	if err := p.Eval(within(t, 10*time.Second), js, &s); err != nil {
		t.Fatalf("text of %s: %v", selector, err)
	}
	return s
}

// waitText waits until the element matching selector shows want.
func waitText(t *testing.T, p *cdptest.Page, selector, want string, d time.Duration) {
	t.Helper()
	pred := fmt.Sprintf("(document.querySelector(%q) || {}).textContent === %q", selector, want)
	if err := p.WaitFor(within(t, d), pred); err != nil {
		t.Fatalf("%s never showed %q (shows %q): %v", selector, want, textOf(t, p, selector), err)
	}
}

// TestCSPIsEnforced: the guard's Content-Security-Policy is delivered to the browser and enforced,
// so TestOfflineLoadAndLive's empty console means the page runs under it. An inline script is
// blocked and shows up as a console error, which is how a violation by the page itself would.
func TestCSPIsEnforced(t *testing.T) {
	b := newBench(t)
	base := newSite(t, b)
	p := openPage(t, base)
	waitText(t, p, "#link", "LIVE", 15*time.Second)

	var policy string
	if err := p.Eval(within(t, 10*time.Second), `fetch('/').then(r => r.headers.get('content-security-policy'))`, &policy); err != nil {
		t.Fatalf("read the policy: %v", err)
	}
	host := strings.TrimPrefix(base, "http://")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self' ws://" + host, "frame-ancestors 'none'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("Content-Security-Policy %q lacks %q", policy, want)
		}
	}
	if errs := p.ConsoleErrors(); len(errs) != 0 {
		t.Fatalf("console errors before the probe: %v", errs)
	}

	var ran bool
	probe := `(() => { window.__inline = false; const s = document.createElement('script'); s.textContent = 'window.__inline = true'; document.head.appendChild(s); return window.__inline; })()`
	if err := p.Eval(within(t, 10*time.Second), probe, &ran); err != nil || ran {
		t.Fatalf("an inline script ran (%v, %v): the policy is not enforced", ran, err)
	}
	ctx := within(t, 5*time.Second)
	for {
		for _, e := range p.ConsoleErrors() {
			if strings.Contains(e, "Content Security Policy") {
				return
			}
		}
		if err := sysClock.Sleep(ctx, 50*time.Millisecond); err != nil {
			t.Fatalf("no CSP violation reached the console (errors %v): %v", p.ConsoleErrors(), err)
		}
	}
}

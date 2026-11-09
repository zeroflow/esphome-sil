// Package cdptest is a small headless-Chrome launcher and Chrome DevTools Protocol client for the
// browser tests (uitest) and the end-to-end tests (e2e). It is test support: cmd/sil never
// imports it, and it has no tests of its own; uitest and e2e exercise it.
//
// Chrome is a host tool, found through $SIL_CHROME or on PATH; it is never downloaded. Every wait
// goes through the clock.Clock passed to Launch.
package cdptest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/clock"
)

// ErrNoChrome is what Launch returns when no Chrome binary is found.
var ErrNoChrome = errors.New("cdptest: Chrome not found; install google-chrome or set SIL_CHROME")

const (
	launchBudget = 20 * time.Second
	pollEvery    = 50 * time.Millisecond
	readLimit    = 64 << 20
)

// LaunchOptions configure Launch.
type LaunchOptions struct {
	// Offline maps every host except 127.0.0.1 to NOTFOUND, so nothing off the machine is reachable.
	Offline bool
	// Width and Height are the initial window size; 1280 x 720 when zero.
	Width, Height int
}

// Request is one request the page made: an HTTP request (Kind "http", from
// Network.requestWillBeSent) or a WebSocket connection (Kind "ws", Method "GET", from
// Network.webSocketCreated; Chrome reports a WebSocket only there).
type Request struct {
	Method, URL, Kind string
}

// SameOrigin reports whether u is on the origin of base. http pairs with ws and https with wss; the
// host and the port (explicit or the scheme's default) must be equal.
func SameOrigin(base, u string) bool {
	b, err := url.Parse(base)
	if err != nil {
		return false
	}
	r, err := url.Parse(u)
	if err != nil {
		return false
	}
	if secure(b.Scheme) != secure(r.Scheme) || !web(b.Scheme) || !web(r.Scheme) {
		return false
	}
	return strings.EqualFold(b.Hostname(), r.Hostname()) && port(b) == port(r)
}

func web(scheme string) bool {
	switch scheme {
	case "http", "ws", "https", "wss":
		return true
	}
	return false
}

func secure(scheme string) bool { return scheme == "https" || scheme == "wss" }

func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if secure(u.Scheme) {
		return "443"
	}
	return "80"
}

// findChrome returns the Chrome binary: $SIL_CHROME, else the first of google-chrome, chromium and
// chromium-browser on PATH.
func findChrome() (string, error) {
	if p := os.Getenv("SIL_CHROME"); p != "" {
		return p, nil
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNoChrome
}

// cappedBuffer keeps the first limit bytes written to it (Chrome's stderr, for launch failures).
type cappedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := 8192 - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Browser is one running Chrome.
type Browser struct {
	clk  clock.Clock
	cmd  *exec.Cmd
	dir  string
	conn *conn
	done chan struct{} // closed when the process has been reaped

	closeOnce sync.Once
}

// Launch starts headless Chrome and connects to its DevTools endpoint.
func Launch(ctx context.Context, clk clock.Clock, o LaunchOptions) (*Browser, error) {
	bin, err := findChrome()
	if err != nil {
		return nil, err
	}
	if o.Width == 0 {
		o.Width = 1280
	}
	if o.Height == 0 {
		o.Height = 720
	}
	dir, err := os.MkdirTemp("", "sil-chrome-")
	if err != nil {
		return nil, fmt.Errorf("cdptest: temp profile dir: %w", err)
	}
	args := []string{
		"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir=" + dir, "--remote-debugging-port=0",
		fmt.Sprintf("--window-size=%d,%d", o.Width, o.Height),
	}
	if os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	if o.Offline {
		args = append(args, "--host-resolver-rules=MAP * ~NOTFOUND , EXCLUDE 127.0.0.1")
	}
	args = append(args, "about:blank")
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr := &cappedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("cdptest: start %s: %w", bin, err)
	}
	b := &Browser{clk: clk, cmd: cmd, dir: dir, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(b.done)
	}()

	lctx, cancel := clock.WithTimeout(ctx, clk, launchBudget)
	defer cancel()
	wsURL, err := b.waitEndpoint(lctx)
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("%w\nchrome stderr:\n%s", err, stderr.String())
	}
	ws, _, err := websocket.Dial(lctx, wsURL, nil)
	if err != nil {
		b.Close()
		return nil, fmt.Errorf("cdptest: dial %s: %w", wsURL, err)
	}
	ws.SetReadLimit(readLimit)
	b.conn = newConn(ws)
	return b, nil
}

// waitEndpoint polls <user-data-dir>/DevToolsActivePort: its first line is the port, its second
// the browser endpoint path.
func (b *Browser) waitEndpoint(ctx context.Context) (string, error) {
	file := filepath.Join(b.dir, "DevToolsActivePort")
	for {
		if raw, err := os.ReadFile(file); err == nil {
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) >= 2 {
				if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil {
					return "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1]), nil
				}
			}
		}
		select {
		case <-b.done:
			return "", errors.New("cdptest: Chrome exited before it opened a DevTools endpoint")
		default:
		}
		if err := b.clk.Sleep(ctx, pollEvery); err != nil {
			return "", fmt.Errorf("cdptest: Chrome did not open a DevTools endpoint: %w", err)
		}
	}
}

// Close ends Chrome and its helper processes and removes the temporary profile.
func (b *Browser) Close() {
	b.closeOnce.Do(func() {
		if b.conn != nil {
			b.conn.close()
		}
		if b.cmd.Process != nil {
			_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-b.done
		_ = os.RemoveAll(b.dir)
	})
}

// NewPage opens a blank tab and attaches to it.
func (b *Browser) NewPage(ctx context.Context) (*Page, error) {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := b.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := b.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true}, &attached); err != nil {
		return nil, err
	}
	p := &Page{b: b, target: created.TargetID, session: attached.SessionID}
	b.conn.register(attached.SessionID, p)
	for _, m := range []string{"Page.enable", "Runtime.enable", "Network.enable", "Log.enable"} {
		if err := p.send(ctx, m, nil, nil); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Page is one tab.
type Page struct {
	b       *Browser
	target  string
	session string

	mu       sync.Mutex
	requests []Request
	console  []string
	loads    int
	touch    bool
}

func (p *Page) send(ctx context.Context, method string, params, out any) error {
	return p.b.conn.call(ctx, p.session, method, params, out)
}

// Close closes the tab.
func (p *Page) Close() {
	ctx, cancel := clock.WithTimeout(context.Background(), p.b.clk, 5*time.Second)
	defer cancel()
	_ = p.b.conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": p.target}, nil)
	p.b.conn.unregister(p.session)
}

// Navigate loads u and returns once the page's load event has fired.
func (p *Page) Navigate(ctx context.Context, u string) error {
	p.mu.Lock()
	before := p.loads
	p.mu.Unlock()
	var res struct {
		ErrorText string `json:"errorText"`
	}
	if err := p.send(ctx, "Page.navigate", map[string]any{"url": u}, &res); err != nil {
		return err
	}
	if res.ErrorText != "" {
		return fmt.Errorf("cdptest: navigate to %s: %s", u, res.ErrorText)
	}
	for {
		p.mu.Lock()
		loaded := p.loads > before
		p.mu.Unlock()
		if loaded {
			return nil
		}
		if err := p.b.clk.Sleep(ctx, 20*time.Millisecond); err != nil {
			return fmt.Errorf("cdptest: waiting for %s to load: %w", u, err)
		}
	}
}

// Eval evaluates the JavaScript expression js in the page (await works inside it; a promise is
// awaited) and decodes the JSON value into out when out is not nil. A thrown exception becomes an
// error.
func (p *Page) Eval(ctx context.Context, js string, out any) error {
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
	}
	err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "(async () => (" + js + "\n))()",
		"awaitPromise":  true,
		"returnByValue": true,
	}, &res)
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return fmt.Errorf("cdptest: eval %q: %s", js, res.ExceptionDetails.message())
	}
	if out == nil || len(res.Result.Value) == 0 {
		return nil
	}
	if err := json.Unmarshal(res.Result.Value, out); err != nil {
		return fmt.Errorf("cdptest: eval %q: decode %s: %w", js, res.Result.Value, err)
	}
	return nil
}

// WaitFor evaluates the JavaScript expression pred in the page every 50 ms until it is truthy or
// ctx ends.
func (p *Page) WaitFor(ctx context.Context, pred string) error {
	script := "new Promise((resolve) => { const poll = () => { let ok = false; try { ok = !!(" + pred +
		"); } catch (e) { ok = false; } if (ok) { resolve(true); } else { setTimeout(poll, 50); } }; poll(); })"
	if err := p.Eval(ctx, script, nil); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cdptest: waiting for %s: %w", pred, ctx.Err())
		}
		return err
	}
	return nil
}

// Requests returns every request the page made so far, in order.
func (p *Page) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.requests...)
}

// ConsoleErrors returns the console errors, uncaught exceptions and error log entries (CSP
// violations arrive there) the page produced so far.
func (p *Page) ConsoleErrors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.console...)
}

// Mouse dispatches a mouse event: typ is mousePressed, mouseReleased or mouseMoved; the button is
// the left one.
func (p *Page) Mouse(ctx context.Context, typ string, x, y float64) error {
	params := map[string]any{"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1}
	switch typ {
	case "mousePressed":
		params["buttons"] = 1
	case "mouseMoved":
		params["button"] = "none"
		params["clickCount"] = 0
	}
	return p.send(ctx, "Input.dispatchMouseEvent", params, nil)
}

// Touch dispatches a touch event with one touch point: typ is touchStart, touchMove, touchEnd or
// touchCancel. Touch emulation is switched on the first time.
func (p *Page) Touch(ctx context.Context, typ string, x, y float64) error {
	p.mu.Lock()
	on := p.touch
	p.touch = true
	p.mu.Unlock()
	if !on {
		if err := p.send(ctx, "Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true}, nil); err != nil {
			return err
		}
	}
	points := []map[string]any{}
	if typ == "touchStart" || typ == "touchMove" {
		points = append(points, map[string]any{"x": x, "y": y})
	}
	return p.send(ctx, "Input.dispatchTouchEvent", map[string]any{"type": typ, "touchPoints": points}, nil)
}

var keyCodes = map[string]int{
	"Enter": 13, "Escape": 27, "Tab": 9, "Backspace": 8, " ": 32,
	"ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40,
}

// Key dispatches a key event: typ is keyDown or keyUp, key a DOM key value ("a", "Enter", "Escape",
// "ArrowUp"). A printable key sends its text with keyDown.
func (p *Page) Key(ctx context.Context, typ, key string) error {
	params := map[string]any{"type": typ, "key": key, "code": key}
	if n := len([]rune(key)); n == 1 {
		r := []rune(key)[0]
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			params["code"] = "Key" + strings.ToUpper(key)
			params["windowsVirtualKeyCode"] = int(strings.ToUpper(key)[0])
		case r >= '0' && r <= '9':
			params["code"] = "Digit" + key
			params["windowsVirtualKeyCode"] = int(r)
		case r == ' ':
			params["code"] = "Space"
		}
		if typ == "keyDown" {
			params["text"] = key
		}
	}
	if vk, ok := keyCodes[key]; ok {
		params["windowsVirtualKeyCode"] = vk
		if key == "Enter" && typ == "keyDown" {
			params["text"] = "\r"
		}
	}
	return p.send(ctx, "Input.dispatchKeyEvent", params, nil)
}

// SetViewport sets the page size in CSS pixels (device scale factor 1).
func (p *Page) SetViewport(ctx context.Context, w, h int, mobile bool) error {
	return p.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": 1, "mobile": mobile,
	}, nil)
}

// Center returns the viewport coordinates of the center of the first element matching selector.
func (p *Page) Center(ctx context.Context, selector string) (x, y float64, err error) {
	sel, _ := json.Marshal(selector)
	var c []float64
	js := "(() => { const e = document.querySelector(" + string(sel) + "); if (!e) { return null; } " +
		"const r = e.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]; })()"
	if err := p.Eval(ctx, js, &c); err != nil {
		return 0, 0, err
	}
	if len(c) != 2 {
		return 0, 0, fmt.Errorf("cdptest: no element matches %q", selector)
	}
	return c[0], c[1], nil
}

type exceptionDetails struct {
	Text      string `json:"text"`
	Exception *struct {
		Description string `json:"description"`
		Value       any    `json:"value"`
	} `json:"exception"`
}

func (e *exceptionDetails) message() string {
	if e.Exception != nil {
		if e.Exception.Description != "" {
			return e.Exception.Description
		}
		if e.Exception.Value != nil {
			return fmt.Sprint(e.Exception.Value)
		}
	}
	return e.Text
}

// remoteObjects renders console call arguments as text.
func remoteObjects(args []struct {
	Type        string `json:"type"`
	Value       any    `json:"value"`
	Description string `json:"description"`
}) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case a.Value != nil:
			parts = append(parts, fmt.Sprint(a.Value))
		case a.Description != "":
			parts = append(parts, a.Description)
		default:
			parts = append(parts, a.Type)
		}
	}
	return strings.Join(parts, " ")
}

// event folds one DevTools event of this page into the recorders.
func (p *Page) event(method string, params json.RawMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch method {
	case "Network.requestWillBeSent":
		var e struct {
			Request struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(params, &e) == nil {
			p.requests = append(p.requests, Request{Method: e.Request.Method, URL: e.Request.URL, Kind: "http"})
		}
	case "Network.webSocketCreated":
		var e struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(params, &e) == nil {
			p.requests = append(p.requests, Request{Method: "GET", URL: e.URL, Kind: "ws"})
		}
	case "Page.loadEventFired":
		p.loads++
	case "Runtime.consoleAPICalled":
		var e struct {
			Type string `json:"type"`
			Args []struct {
				Type        string `json:"type"`
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(params, &e) == nil && e.Type == "error" {
			p.console = append(p.console, "console.error: "+remoteObjects(e.Args))
		}
	case "Runtime.exceptionThrown":
		var e struct {
			ExceptionDetails exceptionDetails `json:"exceptionDetails"`
		}
		if json.Unmarshal(params, &e) == nil {
			p.console = append(p.console, "exception: "+e.ExceptionDetails.message())
		}
	case "Log.entryAdded":
		var e struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
				URL   string `json:"url"`
			} `json:"entry"`
		}
		if json.Unmarshal(params, &e) == nil && e.Entry.Level == "error" {
			msg := "log: " + e.Entry.Text
			if e.Entry.URL != "" {
				msg += " (" + e.Entry.URL + ")"
			}
			p.console = append(p.console, msg)
		}
	}
}

// conn is the DevTools connection: an id counter, the replies still awaited and a read loop that
// hands replies to their callers and events to their page.
type conn struct {
	ws *websocket.Conn

	mu      sync.Mutex
	next    int64
	pending map[int64]chan reply
	pages   map[string]*Page
	dead    error
	closed  chan struct{}
}

type reply struct {
	result json.RawMessage
	err    error
}

type message struct {
	ID        int64           `json:"id"`
	Result    json.RawMessage `json:"result"`
	Error     *protocolError  `json:"error"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId"`
}

type protocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func newConn(ws *websocket.Conn) *conn {
	c := &conn{ws: ws, pending: map[int64]chan reply{}, pages: map[string]*Page{}, closed: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *conn) register(session string, p *Page) {
	c.mu.Lock()
	c.pages[session] = p
	c.mu.Unlock()
}

func (c *conn) unregister(session string) {
	c.mu.Lock()
	delete(c.pages, session)
	c.mu.Unlock()
}

func (c *conn) readLoop() {
	defer close(c.closed)
	for {
		_, raw, err := c.ws.Read(context.Background())
		if err != nil {
			c.fail(fmt.Errorf("cdptest: DevTools connection lost: %w", err))
			return
		}
		var m message
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m.Method != "" {
			c.mu.Lock()
			p := c.pages[m.SessionID]
			c.mu.Unlock()
			if p != nil {
				p.event(m.Method, m.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if m.Error != nil {
			detail := m.Error.Message
			if m.Error.Data != "" {
				detail += ": " + m.Error.Data
			}
			ch <- reply{err: fmt.Errorf("cdptest: protocol error %d: %s", m.Error.Code, detail)}
			continue
		}
		ch <- reply{result: m.Result}
	}
}

// fail ends every call still waiting.
func (c *conn) fail(err error) {
	c.mu.Lock()
	c.dead = err
	pending := c.pending
	c.pending = map[int64]chan reply{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- reply{err: err}
	}
}

func (c *conn) close() {
	_ = c.ws.CloseNow()
	<-c.closed
}

// call sends one command (on session, or on the browser when session is empty) and waits for its
// reply, decoding the result into out when out is not nil.
func (c *conn) call(ctx context.Context, session, method string, params, out any) error {
	c.mu.Lock()
	if c.dead != nil {
		err := c.dead
		c.mu.Unlock()
		return err
	}
	c.next++
	id := c.next
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	if session != "" {
		payload["sessionId"] = session
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cdptest: %s: %w", method, err)
	}
	if err := c.ws.Write(ctx, websocket.MessageText, raw); err != nil {
		c.forget(id)
		return fmt.Errorf("cdptest: %s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("%s: %w", method, r.err)
		}
		if out != nil && len(r.result) > 0 {
			if err := json.Unmarshal(r.result, out); err != nil {
				return fmt.Errorf("cdptest: %s: decode result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		return fmt.Errorf("cdptest: %s: %w", method, ctx.Err())
	}
}

func (c *conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

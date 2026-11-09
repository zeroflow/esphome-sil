//go:build sil_e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/fwbuild"
)

// ioFrame is one decoded line from the firmware and the number (1-based) of
// the connection it arrived on.
type ioFrame struct {
	Conn int
	M    map[string]any
	At   time.Duration // sysClock time of arrival
}

// ioPeer plays the simulator side of the sim-bus and, unlike the shared
// busPeer, keeps every frame in arrival order with its connection number so
// a test can count frames without consuming them. Down and Up take the
// listener away and bring it back on the same path.
type ioPeer struct {
	t    testing.TB
	path string
	// manual switches off the automatic welcome and sync answer to a hello.
	manual bool

	mu     sync.Mutex
	ln     net.Listener
	conn   net.Conn
	nconn  int
	frames []ioFrame
}

func ioListen(t testing.TB, path string) *ioPeer { return ioListenMode(t, path, false) }

// ioListenMode starts a peer; with manual true it never answers a hello by itself.
func ioListenMode(t testing.TB, path string, manual bool) *ioPeer {
	t.Helper()
	p := &ioPeer{t: t, path: path, manual: manual}
	p.Up()
	t.Cleanup(p.Down)
	return p
}

// Up starts listening on the peer's path.
func (p *ioPeer) Up() {
	p.t.Helper()
	ln, err := net.Listen("unix", p.path)
	if err != nil {
		p.t.Fatalf("listen %s: %v", p.path, err)
	}
	p.mu.Lock()
	p.ln = ln
	p.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.nconn++
			n := p.nconn
			p.conn = c
			p.mu.Unlock()
			go p.read(c, n)
		}
	}()
}

// Down closes the listener (which removes the socket file) and the current
// connection, so the firmware sees a closed bus and then a missing socket.
func (p *ioPeer) Down() {
	p.mu.Lock()
	ln, c := p.ln, p.conn
	p.ln, p.conn = nil, nil
	p.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if c != nil {
		_ = c.Close()
	}
}

func (p *ioPeer) read(c net.Conn, n int) {
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(bytes.TrimSpace(line), &m) != nil {
			continue
		}
		p.mu.Lock()
		p.frames = append(p.frames, ioFrame{Conn: n, M: m, At: sysClock.Now()})
		p.mu.Unlock()
		if m["k"] == "hello" && !p.manual {
			// Speak the session (BUS-02): welcome and an empty in snapshot. The hub's
			// reconnect backoff only resets on a welcome.
			_, _ = c.Write([]byte(`{"k":"welcome","proto":1,"t":0}` + "\n" + `{"k":"sync","t":0}` + "\n"))
		}
	}
}

// Send writes one line to the current connection.
func (p *ioPeer) Send(line string) error { return p.SendRaw(line + "\n") }

// SendRaw writes s to the current connection as is.
func (p *ioPeer) SendRaw(s string) error {
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c == nil {
		return errors.New("no connection")
	}
	_, err := c.Write([]byte(s))
	return err
}

// Frames returns a copy of everything received so far.
func (p *ioPeer) Frames() []ioFrame {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ioFrame(nil), p.frames...)
}

// Wait polls until a frame matching pred has arrived and returns it.
func (p *ioPeer) Wait(timeout time.Duration, what string, pred func(ioFrame) bool) ioFrame {
	p.t.Helper()
	var found ioFrame
	waitFor(p.t, timeout, what, func() bool {
		for _, f := range p.Frames() {
			if pred(f) {
				found = f
				return true
			}
		}
		return false
	})
	return found
}

// Duties counts frames `out` on ch with value v on connection conn.
func (p *ioPeer) Duties(conn int, ch string, v float64) int {
	n := 0
	for _, f := range p.Frames() {
		if f.Conn == conn && f.M["k"] == "out" && f.M["ch"] == ch && f.M["v"] == v {
			n++
		}
	}
	return n
}

func ioKind(kind string, conn int) func(ioFrame) bool {
	return func(f ioFrame) bool { return f.Conn == conn && f.M["k"] == kind }
}

// ioFixture writes a minimal host firmware that uses the sim_bus component
// into a fresh short temp dir and returns the file path and the bus socket
// path baked into it as the hub `path:`.
func ioFixture(t testing.TB, name, body string) (yamlPath, busPath string) {
	t.Helper()
	dir := shortTempDir(t)
	busPath = filepath.Join(dir, "bus.sock")
	yaml := fmt.Sprintf(`esphome:
  name: fx-%s
  build_flags:
    - -static

host:

logger:

external_components:
  - source:
      type: local
      path: %s
    components: [sim_bus]

sim_bus:
  id: bus
  path: %s

%s`, name, filepath.Join(repoRoot(t), "components"), busPath, body)
	yamlPath = filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
	return yamlPath, busPath
}

// ioCompile compiles yamlPath in the pinned image with no network and
// returns the program path. Build output and HOME live in per-call temp dirs.
func ioCompile(t testing.TB, yamlPath string) string {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Dir(yamlPath)
	buildDir, cacheDir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "home"), 0o700); err != nil {
		t.Fatalf("home dir: %v", err)
	}
	args := []string{
		"run", "--rm", "--pull=never", "--network", "none",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "HOME=/cache/home",
		"-e", "ESPHOME_DATA_DIR=/build/data",
		"-e", "ESPHOME_NOGITIGNORE=1",
		"-v", root + ":" + root + ":ro",
		"-v", dir + ":" + dir + ":ro",
		"-w", dir,
		"-v", buildDir + ":/build",
		"-v", cacheDir + ":/cache",
		fwbuild.Image, "compile", filepath.Base(yamlPath),
	}
	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		tail := out.String()
		if len(tail) > 6000 {
			tail = tail[len(tail)-6000:]
		}
		t.Fatalf("compile %s: %v\n%s", filepath.Base(yamlPath), err, tail)
	}
	matches, _ := filepath.Glob(filepath.Join(buildDir, "*", ".pioenvs", "*", "program"))
	if len(matches) != 1 {
		t.Fatalf("compile %s: want exactly one program under %s, found %v", filepath.Base(yamlPath), buildDir, matches)
	}
	return matches[0]
}

// ioSamples returns the (t, value) pairs of one entity from a watch result,
// without the null states.
func ioSamples(t testing.TB, out map[string]any, entity string) [][2]float64 {
	t.Helper()
	series, _ := out["series"].(map[string]any)
	list, _ := series[entity].([]any)
	var res [][2]float64
	for _, s := range list {
		pair, ok := s.([]any)
		if !ok || len(pair) != 2 {
			continue
		}
		ts, ok1 := pair[0].(float64)
		v, ok2 := pair[1].(float64)
		if ok1 && ok2 {
			res = append(res, [2]float64{ts, v})
		}
	}
	return res
}

func ioWatch(t testing.TB, port int, key string, seconds int) map[string]any {
	t.Helper()
	out, code := runAPIClient(t, port, key, "watch", "--seconds", fmt.Sprint(seconds))
	if code != 0 {
		t.Fatalf("watch exit %d: %v", code, out)
	}
	return out
}

// TestGuardsCompile proves that a config with only sim_bus outputs and a
// config with only sim_bus sensors both compile in the pinned image, and that
// the programs run: an empty SIL_BUS is ignored and the YAML path is used.
func TestGuardsCompile(t *testing.T) {
	cases := []struct {
		name, body string
		want       func(*ioPeer) // extra assertions once hello arrived
	}{
		{
			name: "output-only",
			body: `output:
  - platform: sim_bus
    id: pwm1
    channel: port1.duty

fan:
  - platform: speed
    output: pwm1
    name: "Fan 1"
    speed_count: 100
    restore_mode: ALWAYS_OFF
`,
			want: func(p *ioPeer) {
				m := p.Wait(5*time.Second, "port1.duty boot frame", func(f ioFrame) bool {
					return f.Conn == 1 && f.M["k"] == "out" && f.M["ch"] == "port1.duty"
				})
				if m.M["v"] != float64(0) {
					t.Errorf("boot duty = %v, want 0", m.M["v"])
				}
			},
		},
		{
			name: "sensor-only",
			body: `sensor:
  - platform: sim_bus
    name: "Fan 1 Speed"
    channel: port1.rpm
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yamlPath, busPath := ioFixture(t, c.name, c.body)
			program := ioCompile(t, yamlPath)
			peer := ioListen(t, busPath)
			// busPath "" makes startFirmware set SIL_BUS to the empty string.
			startFirmware(t, program, "", filepath.Join(shortTempDir(t), "prefs"))
			peer.Wait(5*time.Second, "hello on the YAML path (empty SIL_BUS must be ignored)", ioKind("hello", 1))
			if c.want != nil {
				c.want(peer)
			}
		})
	}
}

// TestRPMMean runs the real simulator and checks that the firmware reports
// the RPM of a spinning-up fan as a steady 1 s mean, not once per bus frame.
func TestRPMMean(t *testing.T) {
	const name = "e2e-io"
	env := newEnv(t)
	up := env.Up(t, name)
	api, _ := up["api"].(map[string]any)
	port := int(api["port"].(float64))
	key, _ := api["key"].(string)

	out, code := env.APIClient(t, port, key, "set-fan", "--fan", "1", "--speed", "40")
	if code != 0 {
		t.Fatalf("set-fan exit %d: %v", code, out)
	}
	samples := ioSamples(t, ioWatch(t, port, key, 6), "Fan 1 Speed")
	t.Logf("Fan 1 Speed during spin-up: %v", samples)
	if len(samples) < 4 || len(samples) > 8 {
		t.Fatalf("%d Fan 1 Speed updates in 6 s, want 4 to 8 (one per second, not one per bus frame)", len(samples))
	}
	for i := 1; i < len(samples); i++ {
		if samples[i][1] < samples[i-1][1] {
			t.Errorf("Fan 1 Speed fell during spin-up: %v then %v", samples[i-1], samples[i])
		}
	}
	if last := samples[len(samples)-1][1]; last <= 0 {
		t.Errorf("Fan 1 Speed never rose above 0")
	}
}

// ioSender sends port1.rpm = base + slope*elapsed seconds to the peer every
// 50 ms until stopped. A ramp (slope != 0) makes the 1 s mean visible: the
// published value sits in the middle of the window, not at its last frame.
type ioSender struct {
	stop chan struct{}
	done chan struct{}
}

func ioSend(p *ioPeer, base, slope float64) *ioSender {
	s := &ioSender{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		tick := sysClock.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		start := sysClock.Now()
		for {
			select {
			case <-s.stop:
				return
			case <-tick.C():
				v := base + slope*(sysClock.Now()-start).Seconds()
				_ = p.Send(fmt.Sprintf(`{"k":"in","ch":"port1.rpm","v":%.2f}`, v))
			}
		}
	}()
	return s
}

func (s *ioSender) Stop() {
	close(s.stop)
	<-s.done
}

// TestFakePeer drives the real firmware from a fake simulator: output dedupe,
// the 1 s mean, and the disconnect and reconnect semantics.
func TestFakePeer(t *testing.T) {
	id := newFWIdentity(t, "e2e-peer")
	program := buildFirmware(t, id)

	dir := shortTempDir(t)
	busPath := filepath.Join(dir, "bus.sock")
	peer := ioListen(t, busPath)
	startFirmware(t, program, busPath, filepath.Join(dir, "prefs"))

	peer.Wait(10*time.Second, "hello", ioKind("hello", 1))
	for n := 1; n <= 4; n++ {
		ch := fmt.Sprintf("port%d.duty", n)
		peer.Wait(5*time.Second, "boot snapshot of "+ch, func(f ioFrame) bool {
			return f.Conn == 1 && f.M["k"] == "out" && f.M["ch"] == ch && f.M["v"] == float64(0)
		})
	}

	// IO-01: the same command twice reaches the bus once.
	for i := 0; i < 2; i++ {
		out, code := runAPIClient(t, id.Port, id.Key, "set-fan", "--fan", "1", "--speed", "40")
		if code != 0 {
			t.Fatalf("set-fan exit %d: %v", code, out)
		}
	}
	peer.Wait(5*time.Second, "port1.duty 0.4", func(f ioFrame) bool {
		return f.M["k"] == "out" && f.M["ch"] == "port1.duty" && f.M["v"] == 0.4
	})
	if err := sysClock.Sleep(t.Context(), time.Second); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if n := peer.Duties(1, "port1.duty", 0.4); n != 1 {
		t.Fatalf("port1.duty 0.4 frames after two identical commands = %d, want exactly 1", n)
	}

	// IO-02: a ramp from the peer arrives at the API as about one update per second.
	sender := ioSend(peer, 1000, 10)
	samples := ioSamples(t, ioWatch(t, id.Port, id.Key, 6), "Fan 1 Speed")
	sender.Stop()
	t.Logf("Fan 1 Speed with the peer ramping from 1000: %v", samples)
	if len(samples) < 4 || len(samples) > 8 {
		t.Fatalf("%d Fan 1 Speed updates in 6 s, want 4 to 8 (the peer sent ~120 frames)", len(samples))
	}
	for _, s := range samples {
		if s[1] < 990 || s[1] > 1100 {
			t.Errorf("Fan 1 Speed %v is not around 1000", s)
		}
	}

	// A4: the peer goes away. The state stays at the last mean and does not change.
	peer.Down()
	held := ioSamples(t, ioWatch(t, id.Port, id.Key, 3), "Fan 1 Speed")
	if len(held) > 1 {
		t.Fatalf("Fan 1 Speed published %d times in 3 s without a peer, want only the held state: %v", len(held), held)
	}

	// Reconnect: within the 2 s backoff cap, one snapshot, and fresh means only from new values.
	peer.Up()
	// The hub backs off from 100 ms to 2 s while the socket is missing, so the first
	// attempt after the listener is back comes within 2 s plus a loop pass.
	peer.Wait(4*time.Second, "reconnect hello", ioKind("hello", 2))
	if err := sysClock.Sleep(t.Context(), time.Second); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if n := peer.Duties(2, "port1.duty", 0.4); n != 1 {
		t.Errorf("port1.duty 0.4 in the reconnect snapshot = %d, want exactly 1", n)
	}
	for n := 2; n <= 4; n++ {
		if c := peer.Duties(2, fmt.Sprintf("port%d.duty", n), 0); c != 1 {
			t.Errorf("port%d.duty 0 in the reconnect snapshot = %d, want 1", n, c)
		}
	}
	sender = ioSend(peer, 500, 0)
	after := ioSamples(t, ioWatch(t, id.Port, id.Key, 3), "Fan 1 Speed")
	sender.Stop()
	t.Logf("Fan 1 Speed after the reconnect, peer sending 500: %v", after)
	got500 := false
	for i, s := range after {
		if i == 0 && s[1] != 500 {
			continue // the state held across the outage, sent on subscribe
		}
		if s[1] != 500 {
			t.Errorf("Fan 1 Speed %v after the reconnect mixes in the pre-outage value", s)
		} else {
			got500 = true
		}
	}
	if !got500 {
		t.Errorf("Fan 1 Speed never reached 500 after the reconnect: %v", after)
	}

	// T-01-11: more than 4096 bytes without a newline drop the connection, and so
	// does a complete line that long; the hub reconnects on its own each time.
	if err := peer.SendRaw(strings.Repeat("x", 5000)); err != nil {
		t.Fatalf("send oversized unfinished line: %v", err)
	}
	peer.Wait(3*time.Second, "reconnect after 5000 bytes without a newline (hello on connection 3)", ioKind("hello", 3))
	if err := peer.Send(strings.Repeat("x", 5000)); err != nil {
		t.Fatalf("send oversized line: %v", err)
	}
	peer.Wait(3*time.Second, "reconnect after a 5000-byte line (hello on connection 4)", ioKind("hello", 4))
}

// ioConnFrames returns the frames received on connection conn, in order.
func ioConnFrames(p *ioPeer, conn int) []ioFrame {
	var out []ioFrame
	for _, f := range p.Frames() {
		if f.Conn == conn {
			out = append(out, f)
		}
	}
	return out
}

// TestHubSession checks the firmware half of the session (BUS-02, BUS-03):
// the hello snapshot, the answers to welcome, err and bad lines, and the
// reconnect backoff with its reset.
func TestHubSession(t *testing.T) {
	id := newFWIdentity(t, "e2e-hub")
	program := buildFirmware(t, id)
	dir := shortTempDir(t)
	busPath := filepath.Join(dir, "bus.sock")
	peer := ioListenMode(t, busPath, true)
	proc := startFirmware(t, program, busPath, filepath.Join(dir, "prefs"))

	// Connection 1 is made in setup(), before any output was written: hello, the 8 chan
	// frames and sync, then the four boot duties as ordinary out frames.
	peer.Wait(10*time.Second, "the boot duties on connection 1", func(f ioFrame) bool {
		return f.Conn == 1 && f.M["k"] == "out" && f.M["ch"] == "port4.duty"
	})
	fs := ioConnFrames(peer, 1)
	kindsOf := func(fs []ioFrame) string {
		var ks []string
		for _, f := range fs {
			ks = append(ks, fmt.Sprint(f.M["k"]))
		}
		return strings.Join(ks, " ")
	}
	chanKinds := "hello" + strings.Repeat(" chan", 8)
	if got, want := kindsOf(fs), chanKinds+" sync out out out out"; got != want {
		t.Fatalf("connection 1 kinds = %s\nwant                 %s", got, want)
	}
	hello := fs[0].M
	if hello["proto"] != float64(1) || hello["chans"] != float64(8) || hello["name"] != "sil-"+id.Name {
		t.Errorf("hello = %v, want proto 1, chans 8, name sil-%s", hello, id.Name)
	}
	if _, ok := hello["t"].(float64); !ok {
		t.Errorf("hello has no integer t: %v", hello)
	}
	var chans []string
	for _, f := range fs[1:9] {
		chans = append(chans, fmt.Sprintf("%v/%v/%v", f.M["ch"], f.M["dir"], f.M["type"]))
	}
	wantChans := []string{
		"port1.duty/out/f32", "port2.duty/out/f32", "port3.duty/out/f32", "port4.duty/out/f32",
		"port1.rpm/in/f32", "port2.rpm/in/f32", "port3.rpm/in/f32", "port4.rpm/in/f32",
	}
	if strings.Join(chans, " ") != strings.Join(wantChans, " ") {
		t.Errorf("manifest = %v\nwant       %v", chans, wantChans)
	}
	for i, f := range fs[10:14] {
		if f.M["ch"] != fmt.Sprintf("port%d.duty", i+1) || f.M["v"] != float64(0) {
			t.Errorf("boot out frame %d = %v, want port%d.duty 0", i, f.M, i+1)
		}
	}

	// A welcome of another protocol version drops the connection; the hub then
	// reconnects with a growing delay (100, 200, 400, 800 ms, ...).
	send := func(conn int, line string) {
		t.Helper()
		if err := peer.Send(line); err != nil {
			t.Fatalf("send on connection %d: %v", conn, err)
		}
	}
	var hellos []time.Duration
	note := func(conn int) {
		f := peer.Wait(5*time.Second, fmt.Sprintf("hello on connection %d", conn), ioKind("hello", conn))
		hellos = append(hellos, f.At)
	}
	hellos = append(hellos, fs[0].At)
	for conn := 1; conn <= 5; conn++ {
		send(conn, `{"k":"welcome","proto":2,"t":0}`)
		note(conn + 1)
	}
	for i := 1; i < len(hellos); i++ {
		gap := hellos[i] - hellos[i-1]
		want := 100 * time.Millisecond << (i - 1) // 100, 200, 400, 800, 1600
		t.Logf("reconnect gap %d: %s (backoff %s)", i, gap.Round(time.Millisecond), want)
		if gap < want*8/10 || gap > want+500*time.Millisecond {
			t.Errorf("reconnect gap %d = %s, want about %s", i, gap, want)
		}
	}

	// A bad line makes the hub answer err and drop the connection.
	send(6, `not json`)
	peer.Wait(3*time.Second, "err frame on connection 6", func(f ioFrame) bool { return f.Conn == 6 && f.M["k"] == "err" })
	note(7)
	// An err frame from the simulator drops it too.
	send(7, `{"k":"err","msg":"test"}`)
	note(8)
	// A good welcome resets the backoff: after a bye the reconnect takes about 100 ms.
	send(8, `{"k":"welcome","proto":1,"t":0}`)
	send(8, `{"k":"sync","t":0}`)
	send(8, `{"k":"bye"}`)
	start := sysClock.Now()
	f := peer.Wait(3*time.Second, "hello on connection 9", ioKind("hello", 9))
	if gap := f.At - start; gap > 700*time.Millisecond {
		t.Errorf("reconnect after a welcome took %s, want about 100 ms (backoff reset)", gap)
	}
	// A reconnect replays the last value of every output before sync.
	peer.Wait(3*time.Second, "sync on connection 9", func(f ioFrame) bool { return f.Conn == 9 && f.M["k"] == "sync" })
	if got, want := kindsOf(ioConnFrames(peer, 9)[:14]), chanKinds+" out out out out sync"; got != want {
		t.Errorf("connection 9 kinds = %s\nwant                 %s", got, want)
	}
	if !proc.alive() {
		t.Fatalf("firmware exited during the session tests")
	}
	// The firmware API stayed responsive through all of it.
	if out, code := runAPIClient(t, id.Port, id.Key, "info"); code != 0 {
		t.Fatalf("info exit %d: %v", code, out)
	}
}

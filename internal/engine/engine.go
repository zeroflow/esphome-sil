// Package engine is the single-writer actor that owns the simulated plant.
//
// All model state is mutated by the engine goroutine only. Bus callbacks and
// clock ticks arrive through one inbox/select loop, and Snapshot() hands out
// immutable values published through an atomic pointer. The engine is a value
// built from scratch by New, so dropping it and creating another is how the
// simulator restarts (D-02).
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zeroflow/esphome-sil/internal/bus"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/model"
)

const (
	tick         = 10 * time.Millisecond
	rpmFrameStep = 50 * time.Millisecond
)

// ButtonIDs are the user buttons of the Rev 3.3 board, in snapshot order. They are also the names
// of the sim-bus `in` channels that carry the button levels.
var ButtonIDs = []string{"usr1", "usr2", "usr3"}

// LEDChannel is the sim-bus channel of the status LED strip (direction out, type rgb).
const LEDChannel = "leds"

const (
	ledMaxN     = 255         // a 4096-byte bus line holds at most 6 x 255 hex characters of pixels
	ledMaxPxLen = 6 * ledMaxN // 1530
)

var ledLabelRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// LEDState is the status LED strip as the firmware wrote it: the final pixel bytes (brightness,
// colour correction and effects already applied by ESPHome), and the segment names the firmware
// declares through its partition lights. Both are empty for a firmware without a leds channel.
type LEDState struct {
	// Strip holds one "#rrggbb" (lowercase) per LED; the Go side never converts or rounds a pixel.
	Strip []string
	// Segments maps a segment label to the LED indexes it covers, from..to inclusive.
	Segments map[string][]int
}

// Errors of the engine's operations. They are sentinels: compare with errors.Is.
var (
	// ErrNotConnected: the firmware is not on the bus or has not sent its manifest yet.
	ErrNotConnected = errors.New("the firmware is not connected to the sim-bus")
	// ErrInputNotAvailable: the firmware's manifest has no channel for the input.
	ErrInputNotAvailable = errors.New("the firmware has no such input")
	// ErrButtonBusy: the button is already held.
	ErrButtonBusy = errors.New("the button is already held")
	// ErrButtonNotHeld: the button is not held.
	ErrButtonNotHeld = errors.New("the button is not held")
	// ErrStopped: the engine's Run has returned; the engine answers at once and never blocks.
	ErrStopped = errors.New("the simulator engine has stopped")
)

// PortConfig binds a fan model to a port.
type PortConfig struct {
	Port  int
	FanID string
	Fan   model.Params
}

// Config configures an engine.
type Config struct {
	Clock   clock.Clock
	BusPath string
	Ports   []PortConfig
	Log     *slog.Logger
}

// PortState is the observable state of one port.
type PortState struct {
	Port  int
	Fan   string
	Duty  float64
	RPM   float64
	State string // running|stopped
}

// Snapshot is an immutable view of the engine at one instant.
type Snapshot struct {
	SimTime      time.Duration
	BusConnected bool
	BusProto     int
	// Synced is true once the firmware's manifest of the current session has been checked.
	Synced bool
	Ports  []PortState
	// Buttons is the level of every button in ButtonIDs (true = held); always all of them.
	Buttons map[string]bool
	// LEDs is the status LED strip; it keeps the last frame while the bus is down.
	LEDs     LEDState
	Warnings []string
}

type eventKind int

const (
	evHello eventKind = iota
	evManifest
	evFrame
	evDisconnected
	evButton
)

type event struct {
	kind   eventKind
	frame  bus.Frame
	hello  bus.Hello
	chans  []bus.Channel
	reason string
	reply  chan []bus.Frame

	// evButton
	button buttonOp
}

// buttonOp is one SetButton request; the reply channel has capacity 1.
type buttonOp struct {
	id            string
	level, strict bool
	reply         chan buttonReply
}

type buttonReply struct {
	t   time.Duration
	err error
}

// Engine is the simulator core.
type Engine struct {
	cfg   Config
	ready chan struct{}
	snap  atomic.Pointer[Snapshot]
	inbox chan event

	// Owned by the Run goroutine from here on.
	ports     []portRuntime
	connected bool
	synced    bool
	proto     int
	warnings  []string
	simNow    time.Duration
	srv       *bus.Server
	nextRPM   time.Duration

	// done is closed when Run has returned, so operations never block on a dead engine.
	done chan struct{}
	// buttons is the held level per button id; an id is always present.
	buttons map[string]bool
	// btnAvail records the optional button channels of the current session's manifest.
	btnAvail map[string]bool
	// led* is the status LED strip. ledN is the manifest's LED count (0 until a valid manifest);
	// ledStrip is empty until the first pixel frame or a valid manifest of the session. A new
	// session (hello) starts them empty; a disconnect leaves them in place.
	ledN     int
	ledStrip []string
	ledSegs  map[string][]int
}

type portRuntime struct {
	cfg      PortConfig
	state    model.State
	duty     float64
	dutyCh   string
	rpmCh    string
	sent     float32
	sentOnce bool
}

// New builds an engine; nothing runs until Run.
func New(cfg Config) *Engine {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	e := &Engine{
		cfg: cfg, ready: make(chan struct{}), inbox: make(chan event, 1024), done: make(chan struct{}),
		buttons: map[string]bool{}, btnAvail: map[string]bool{},
	}
	for _, id := range ButtonIDs {
		e.buttons[id] = false
	}
	for _, pc := range cfg.Ports {
		e.ports = append(e.ports, portRuntime{
			cfg:    pc,
			state:  model.Initial(pc.Fan),
			dutyCh: fmt.Sprintf("port%d.duty", pc.Port),
			rpmCh:  fmt.Sprintf("port%d.rpm", pc.Port),
		})
	}
	e.publish()
	return e
}

// Ready is closed once the engine listens on its bus socket.
func (e *Engine) Ready() <-chan struct{} { return e.ready }

// Snapshot returns the latest published state; safe from any goroutine.
func (e *Engine) Snapshot() Snapshot { return *e.snap.Load() }

func (e *Engine) publish() {
	s := &Snapshot{
		SimTime:      e.simNow,
		BusConnected: e.connected,
		BusProto:     e.proto,
		Synced:       e.synced,
		Ports:        make([]PortState, len(e.ports)),
		Buttons:      make(map[string]bool, len(e.buttons)),
		LEDs:         LEDState{Strip: append([]string{}, e.ledStrip...), Segments: make(map[string][]int, len(e.ledSegs))},
		Warnings:     append([]string{}, e.warnings...),
	}
	for label, idx := range e.ledSegs {
		s.LEDs.Segments[label] = append([]int{}, idx...)
	}
	for i := range e.ports {
		p := &e.ports[i]
		state := "stopped"
		if p.state.Running {
			state = "running"
		}
		s.Ports[i] = PortState{Port: p.cfg.Port, Fan: p.cfg.FanID, Duty: p.duty, RPM: p.state.RPM, State: state}
	}
	for id, v := range e.buttons {
		s.Buttons[id] = v
	}
	e.snap.Store(s)
}

// handler forwards bus callbacks into the inbox; it never touches model state.
type handler struct {
	e   *Engine
	ctx context.Context
}

func (h handler) put(ev event) {
	select {
	case h.e.inbox <- ev:
	case <-h.ctx.Done():
	}
}

// Hello hands the hello to the engine goroutine and waits for the in-snapshot
// it computes, so the snapshot is taken at one well-defined point of the
// engine's event order.
func (h handler) Hello(hl bus.Hello) []bus.Frame {
	reply := make(chan []bus.Frame, 1)
	h.put(event{kind: evHello, hello: hl, reply: reply})
	select {
	case frames := <-reply:
		return frames
	case <-h.ctx.Done():
		return nil
	}
}
func (h handler) Manifest(chs []bus.Channel) { h.put(event{kind: evManifest, chans: chs}) }
func (h handler) Frame(f bus.Frame)          { h.put(event{kind: evFrame, frame: f}) }
func (h handler) Disconnected(reason string) { h.put(event{kind: evDisconnected, reason: reason}) }
func (e *Engine) micros() int64              { return e.simNow.Microseconds() }
func (e *Engine) send(f bus.Frame)           { e.sendLog(e.srv.Send(f)) }
func (e *Engine) sendLog(err error) {
	if err != nil {
		e.cfg.Log.Warn("bus send failed", "err", err)
	}
}

// Run listens on the bus socket and drives the model until ctx is done. It
// returns nil after the listener is closed and the socket unlinked.
func (e *Engine) Run(ctx context.Context) error {
	srv, err := bus.Listen(e.cfg.BusPath, e.cfg.Clock)
	if err != nil {
		return err
	}
	e.srv = srv
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx, handler{e: e, ctx: ctx}) }()

	clk := e.cfg.Clock
	e.simNow = clk.Now()
	e.nextRPM = e.simNow
	last := e.simNow
	e.publish()
	ticker := clk.NewTicker(tick)
	defer ticker.Stop()
	close(e.ready)

	for {
		select {
		case <-ctx.Done():
			cancel()
			_ = srv.Close()
			<-served
			e.stopped()
			return nil
		case err := <-served:
			_ = srv.Close()
			cancel()
			e.stopped()
			return err
		case now := <-ticker.C():
			dt := now - last
			last = now
			e.simNow = now
			for i := range e.ports {
				p := &e.ports[i]
				p.state = model.Step(p.cfg.Fan, p.state, p.duty, dt)
			}
			if e.connected && e.simNow >= e.nextRPM {
				e.nextRPM = e.simNow + rpmFrameStep
				e.sendRPM(false)
			}
			e.publish()
		case ev := <-e.inbox:
			e.handle(ev)
			e.publish()
		}
	}
}

// stopped publishes the final state of an engine that is shutting down: the
// bus is gone, so a status read during a simulator restart says so.
func (e *Engine) stopped() {
	e.connected, e.synced, e.proto = false, false, 0
	e.btnAvail = map[string]bool{}
	e.publish()
	close(e.done)
}

func (e *Engine) handle(ev event) {
	switch ev.kind {
	case evHello:
		e.connected, e.synced = true, false
		e.btnAvail = map[string]bool{}
		e.proto = int(ev.hello.Proto)
		e.warnings = nil
		e.ledN, e.ledStrip, e.ledSegs = 0, nil, nil
		ev.reply <- e.inSnapshot()
	case evManifest:
		e.synced = true
		e.checkManifest(ev.chans)
	case evDisconnected:
		e.connected, e.synced = false, false
		e.btnAvail = map[string]bool{}
		e.proto = 0
		e.warnings = nil
		switch ev.reason {
		case "", "replaced":
		case "proto_mismatch":
			e.warn(fmt.Sprintf("bus protocol mismatch: the firmware's hello was refused (the simulator speaks protocol %d)", bus.Proto))
		default:
			e.warn("bus connection ended: " + ev.reason)
		}
	case evFrame:
		e.handleFrame(ev.frame)
	case evButton:
		e.handleButton(ev.button)
	}
}

// warn records a warning once.
func (e *Engine) warn(w string) {
	for _, have := range e.warnings {
		if have == w {
			return
		}
	}
	e.warnings = append(e.warnings, w)
}

// inSnapshot is the in frame of every port with its current rpm, in port
// order, then the level of every button in ButtonIDs order. The released
// levels matter: ESPHome runs no state callbacks for an entity's first state,
// so a button whose first level were 1 would lose its first press. It also
// marks the rpm values as sent, so the steady-state rate rule only sends
// changes from here on.
func (e *Engine) inSnapshot() []bus.Frame {
	frames := make([]bus.Frame, 0, len(e.ports)+len(ButtonIDs))
	for i := range e.ports {
		p := &e.ports[i]
		p.sent, p.sentOnce = float32(p.state.RPM), true
		frames = append(frames, bus.NewFrame("in").Str("ch", p.rpmCh).Num("v", float64(p.sent)).Int("t", e.micros()))
	}
	for _, id := range ButtonIDs {
		frames = append(frames, e.buttonFrame(id))
	}
	return frames
}

// buttonFrame is the in frame carrying the current level of button id.
func (e *Engine) buttonFrame(id string) bus.Frame {
	v := 0.0
	if e.buttons[id] {
		v = 1
	}
	return bus.NewFrame("in").Str("ch", id).Num("v", v).Int("t", e.micros())
}

// SetButton sets the level of button id (true = held) and returns the sim
// time of the edge. strict applies the press rules in this order: the bus must
// be connected and the manifest received (ErrNotConnected), the manifest must
// have the channel (ErrInputNotAvailable), a hold needs a released button
// (ErrButtonBusy) and a release a held one (ErrButtonNotHeld). Non-strict only
// sets the level. An engine whose Run has returned answers ErrStopped at once.
func (e *Engine) SetButton(ctx context.Context, id string, level, strict bool) (time.Duration, error) {
	select {
	case <-e.done:
		return 0, ErrStopped
	default:
	}
	op := buttonOp{id: id, level: level, strict: strict, reply: make(chan buttonReply, 1)}
	select {
	case e.inbox <- event{kind: evButton, button: op}:
	case <-e.done:
		return 0, ErrStopped
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	select {
	case r := <-op.reply:
		return r.t, r.err
	case <-e.done:
		// A reply that was sent before the engine stopped still counts.
		select {
		case r := <-op.reply:
			return r.t, r.err
		default:
			return 0, ErrStopped
		}
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// handleButton applies one SetButton on the engine goroutine. The edge is
// stamped with the clock's current time (not the last tick), so two edges
// separated by a clock sleep are at least that far apart in sim time.
func (e *Engine) handleButton(op buttonOp) {
	if _, ok := e.buttons[op.id]; !ok {
		op.reply <- buttonReply{err: fmt.Errorf("unknown button %q", op.id)}
		return
	}
	if op.strict {
		switch {
		case !e.connected || !e.synced:
			op.reply <- buttonReply{err: ErrNotConnected}
			return
		case !e.btnAvail[op.id]:
			op.reply <- buttonReply{err: ErrInputNotAvailable}
			return
		case op.level && e.buttons[op.id]:
			op.reply <- buttonReply{err: ErrButtonBusy}
			return
		case !op.level && !e.buttons[op.id]:
			op.reply <- buttonReply{err: ErrButtonNotHeld}
			return
		}
	}
	if now := e.cfg.Clock.Now(); now > e.simNow {
		e.simNow = now
	}
	e.buttons[op.id] = op.level
	if e.connected {
		e.send(e.buttonFrame(op.id))
	}
	op.reply <- buttonReply{t: e.simNow}
}

// expectedChannel returns the manifest entry a configured port requires.
type expectedChannel struct{ ch, dir string }

// checkManifest turns every difference between the firmware's channels and
// the configured ports into a warning; none of them is fatal.
func (e *Engine) checkManifest(chs []bus.Channel) {
	var want []expectedChannel
	known := map[string]bool{}
	for i := range e.ports {
		want = append(want, expectedChannel{e.ports[i].dutyCh, "out"}, expectedChannel{e.ports[i].rpmCh, "in"})
		known[e.ports[i].dutyCh], known[e.ports[i].rpmCh] = true, true
	}
	have := map[string]bus.Channel{}
	for _, c := range chs {
		if _, dup := have[c.Ch]; !dup {
			have[c.Ch] = c
		}
	}
	// Optional channels: their presence enables the matching operation; a missing one never warns.
	known[LEDChannel] = true
	e.checkLEDs(have[LEDChannel], have)
	for _, id := range ButtonIDs {
		known[id] = true
		c, ok := have[id]
		switch {
		case !ok:
		case c.Dir != "in":
			e.warn(fmt.Sprintf("channel %s has direction %q, expected \"in\"", id, c.Dir))
		case c.Type != "f32":
			e.warn(fmt.Sprintf("channel %s has type %q, expected \"f32\"", id, c.Type))
		default:
			e.btnAvail[id] = true
		}
	}
	for _, w := range want {
		c, ok := have[w.ch]
		switch {
		case !ok:
			e.warn(fmt.Sprintf("missing channel %s (%s)", w.ch, w.dir))
		case c.Dir != w.dir:
			e.warn(fmt.Sprintf("channel %s has direction %q, expected %q", w.ch, c.Dir, w.dir))
		case c.Type != "f32":
			e.warn(fmt.Sprintf("channel %s has type %q, expected \"f32\"", w.ch, c.Type))
		}
	}
	for _, c := range chs {
		if !known[c.Ch] {
			e.warn(fmt.Sprintf("unknown channel %s (ignored)", c.Ch))
		}
	}
}

func (e *Engine) handleFrame(f bus.Frame) {
	if f.Kind() != "out" {
		return
	}
	ch, _ := f.GetStr("ch")
	if ch == LEDChannel {
		e.handleLEDFrame(f)
		return
	}
	matched := false
	for i := range e.ports {
		if e.ports[i].dutyCh != ch {
			continue
		}
		matched = true
		if v, ok := f.GetNum("v"); ok {
			e.ports[i].duty = math.Min(1, math.Max(0, v))
		}
	}
	if !matched {
		e.warn(fmt.Sprintf("unknown channel %s (ignored)", ch))
	}
}

// sendRPM sends an in frame per port whose float32 rpm changed since the last
// send, or for every port when force is set.
func (e *Engine) sendRPM(force bool) {
	for i := range e.ports {
		p := &e.ports[i]
		v := float32(p.state.RPM)
		if !force && p.sentOnce && v == p.sent {
			continue
		}
		p.sent, p.sentOnce = v, true
		e.send(bus.NewFrame("in").Str("ch", p.rpmCh).Num("v", float64(v)).Int("t", e.micros()))
	}
}

// checkLEDs validates the optional leds channel of the manifest. A violation is one warning and
// leaves the strip and the segments as they are; a valid LED count (re)sizes an empty strip to
// black. have holds the first manifest entry per channel.
func (e *Engine) checkLEDs(c bus.Channel, have map[string]bus.Channel) {
	if _, ok := have[LEDChannel]; !ok {
		e.ledN, e.ledStrip, e.ledSegs = 0, nil, nil
		return
	}
	switch {
	case c.Dir != "out":
		e.warn(fmt.Sprintf("channel %s has direction %q, expected \"out\"", LEDChannel, c.Dir))
		return
	case c.Type != "rgb":
		e.warn(fmt.Sprintf("channel %s has type %q, expected \"rgb\"", LEDChannel, c.Type))
		return
	case c.N < 1 || c.N > ledMaxN:
		e.warn(fmt.Sprintf("channel %s declares n=%d LEDs, expected 1..%d", LEDChannel, c.N, ledMaxN))
		return
	}
	e.ledN = int(c.N)
	// A pixel frame of this session that arrived before the manifest (the firmware's replay
	// precedes its sync) must fit the declared count.
	if len(e.ledStrip) != 0 && len(e.ledStrip) != e.ledN {
		e.warn(fmt.Sprintf("channel %s: pixel frame of %d LEDs does not match n=%d", LEDChannel, len(e.ledStrip), e.ledN))
		e.ledStrip = nil
	}
	if len(e.ledStrip) == 0 {
		e.ledStrip = make([]string, e.ledN)
		for i := range e.ledStrip {
			e.ledStrip[i] = "#000000"
		}
	}
	segs, err := parseSegments(c.Seg, e.ledN)
	if err != nil {
		e.warn(fmt.Sprintf("channel %s has a bad seg %q: %v", LEDChannel, c.Seg, err))
		return
	}
	e.ledSegs = segs
}

// parseSegments parses "label:from:to,..." against n LEDs. The empty string means no segments.
func parseSegments(seg string, n int) (map[string][]int, error) {
	out := map[string][]int{}
	if seg == "" {
		return out, nil
	}
	for _, part := range strings.Split(seg, ",") {
		f := strings.Split(part, ":")
		if len(f) != 3 {
			return nil, fmt.Errorf("segment %q is not label:from:to", part)
		}
		if !ledLabelRE.MatchString(f[0]) {
			return nil, fmt.Errorf("segment label %q does not match ^[a-z][a-z0-9_]*$", f[0])
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("duplicate segment label %q", f[0])
		}
		from, err1 := strconv.Atoi(f[1])
		to, err2 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || f[1] == "" || f[2] == "" || f[1][0] == '+' || f[2][0] == '+' || f[1][0] == '-' || f[2][0] == '-' {
			return nil, fmt.Errorf("segment %q needs non-negative integer from and to", part)
		}
		if from > to || to >= n {
			return nil, fmt.Errorf("segment %q must satisfy 0 <= from <= to < %d", part, n)
		}
		idx := make([]int, 0, to-from+1)
		for i := from; i <= to; i++ {
			idx = append(idx, i)
		}
		out[f[0]] = idx
	}
	return out, nil
}

// handleLEDFrame stores the pixels of an out frame on the leds channel. A bad frame is one warning
// and keeps the previous strip. It is accepted before the manifest too.
func (e *Engine) handleLEDFrame(f bus.Frame) {
	px, ok := f.GetStr("px")
	if !ok {
		e.warn(fmt.Sprintf("channel %s carries pixels (px), not a number: wrong type, frame ignored", LEDChannel))
		return
	}
	limit := ledMaxPxLen
	if e.ledN > 0 {
		limit = 6 * e.ledN
	}
	switch {
	case len(px) == 0 || len(px)%6 != 0:
		e.warn(fmt.Sprintf("channel %s: px of %d characters is not a multiple of 6, frame ignored", LEDChannel, len(px)))
		return
	case len(px) > limit || (e.ledN > 0 && len(px) != limit):
		e.warn(fmt.Sprintf("channel %s: px of %d characters does not fit the strip (expected at most %d), frame ignored", LEDChannel, len(px), limit))
		return
	}
	strip := make([]string, 0, len(px)/6)
	for i := 0; i < len(px); i += 6 {
		chunk := px[i : i+6]
		for j := 0; j < 6; j++ {
			c := chunk[j]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				e.warn(fmt.Sprintf("channel %s: px has a non-hex character, frame ignored", LEDChannel))
				return
			}
		}
		strip = append(strip, "#"+strings.ToLower(chunk))
	}
	e.ledStrip = strip
}

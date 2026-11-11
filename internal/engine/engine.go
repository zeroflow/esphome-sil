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
	"unicode/utf8"

	"github.com/zeroflow/esphome-sil/internal/bus"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/events"
	"github.com/zeroflow/esphome-sil/internal/model"
)

const (
	tick         = 10 * time.Millisecond
	rpmFrameStep = 50 * time.Millisecond
	// sampleStep is the spacing of the plot samples (D-12): one sample event per 100 ms of sim
	// time, stamped with the tick that produced it.
	sampleStep = 100 * time.Millisecond
	// behindLate is how late a tick may be handled, or how long after the previous tick, before the
	// simulator counts as behind real time; behindClear is how long without such a tick it takes
	// to count as in sync again (UIC-30, planner assumption A-03-05).
	behindLate  = 50 * time.Millisecond
	behindClear = 2 * time.Second
)

// RPMEventStep is how often a port's rpm is sampled for an rpm event, in sim time. The simulator
// cannot see the instant the firmware publishes the value (Research Pattern 5), so rpm events are
// a once-per-second sample, and only when the float32 value changed since the last rpm event.
const RPMEventStep = time.Second

// ButtonIDs are the user buttons of the Rev 3.3 board, in snapshot order. They are also the names
// of the sim-bus `in` channels that carry the button levels.
var ButtonIDs = []string{"usr1", "usr2", "usr3"}

// LEDChannel is the sim-bus channel of the status LED strip (direction out, type rgb).
const LEDChannel = "leds"

// The environment channels (direction in, type f32): constant values set by the user.
const (
	ChanTemperature = "temperature"
	ChanHumidity    = "humidity"
)

// EnvResend is how often an unchanged environment value is sent again, mirroring the HDC1080's
// 10 s update_interval (D-11). It is measured in sim time.
const EnvResend = 10 * time.Second

// Env is the room the firmware measures: temperature in degrees Celsius, humidity in %RH.
type Env struct {
	Temperature float64
	Humidity    float64
}

// Change is a set of input changes applied together by Apply; a nil field is left alone.
type Change struct {
	Temperature, Humidity *float64
	// Fans replugs ports: the key is a port number (PortConfig.Port), the value the fan to plug in.
	// A replug is an instant unplug and plug: the port's fan state starts over from rest at the
	// current duty, whether or not the fan model changes. A port number the engine does not have
	// is ignored (the supervisor validates before it applies).
	Fans map[int]PortConfig
}

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
	// Env is the initial temperature and humidity of the room.
	Env Env
	Log *slog.Logger
	// Emit receives every change event, on the engine goroutine, and returns the seq it assigned
	// (the supervisor's events.Broker.Publish). Nil means no events. It must never block.
	Emit func(events.Event) uint64
	// Seq returns the seq of the last event published by anyone; SnapshotSync stamps it on its
	// snapshot. Nil means the last seq this engine emitted.
	Seq func() uint64
	// Seed is the noise seed every port's plant is built with. It has no effect until the tach
	// noise of plan 04-04; the supervisor sets it from spec.json (plan 04-05).
	Seed uint64
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
	// Env is the current temperature and humidity.
	Env Env
	// Buttons is the level of every button in ButtonIDs (true = held); always all of them.
	Buttons map[string]bool
	// LEDs is the status LED strip; it keeps the last frame while the bus is down.
	LEDs     LEDState
	Warnings []string
	// BehindRealTime is true while the engine handles ticks late (see noteTick).
	BehindRealTime bool
	// Seq is the seq of the last event the engine emitted before this snapshot was taken (0 when
	// there is no Emit): the state here includes every event up to it. SnapshotSync stamps the
	// broker's seq instead (Config.Seq).
	Seq uint64
}

type eventKind int

const (
	evHello eventKind = iota
	evManifest
	evFrame
	evDisconnected
	evButton
	evApply
	evSync
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
	// evApply
	apply applyOp
	// evSync
	sync chan Snapshot
}

// buttonOp is one SetButton request; the reply channel has capacity 1.
type buttonOp struct {
	id            string
	level, strict bool
	// maxHold, when > 0 on a hold, makes the engine release the button by itself after that much
	// sim time (D-15).
	maxHold time.Duration
	reply   chan buttonReply
}

type buttonReply struct {
	t   time.Duration
	err error
}

// applyOp is one Apply request; the reply channel has capacity 1.
type applyOp struct {
	change Change
	reply  chan struct{}
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
	// nextSample is the sim time of the next sample event. sampled is false in a new engine and set
	// by its first sample, which carries Gap.
	nextSample time.Duration
	sampled    bool
	// behind is the behind-real-time flag; lastLate is the due time of the last late tick.
	behind   bool
	lastLate time.Duration
	// env is the room; nextEnv[i] is when env value i (temperature, humidity) is sent next.
	env     Env
	nextEnv [2]time.Duration

	// done is closed when Run has returned, so operations never block on a dead engine.
	done chan struct{}
	// buttons is the held level per button id; an id is always present.
	buttons map[string]bool
	// holdUntil is the sim time at which the engine releases a held button by itself; a button
	// without a max hold has no entry. A new engine (sim restart) starts with none, and a
	// stopped engine clears it.
	holdUntil map[string]time.Duration
	// btnAvail records the optional button channels of the current session's manifest.
	btnAvail map[string]bool
	// led* is the status LED strip. ledN is the manifest's LED count (0 until a valid manifest);
	// ledStrip is empty until the first pixel frame or a valid manifest of the session. A new
	// session (hello) starts them empty; a disconnect leaves them in place.
	ledN     int
	ledStrip []string
	ledSegs  map[string][]int
	// lastSeq is the seq Emit returned last. inTick is set while a clock tick is handled: its
	// events are stamped with the tick's own time, not with a clock that may have moved on.
	lastSeq uint64
	inTick  bool
}

type portRuntime struct {
	cfg      PortConfig
	plant    *model.Plant
	duty     float64
	dutyCh   string
	rpmCh    string
	sent     float32
	sentOnce bool
	// Event bookkeeping: the rpm of the last rpm event, when the rpm is sampled next, and the
	// state of the last state event.
	evRPM   float32
	nextRPM time.Duration
	evState string
}

// New builds an engine; nothing runs until Run.
func New(cfg Config) *Engine {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	e := &Engine{
		cfg: cfg, ready: make(chan struct{}), inbox: make(chan event, 1024), done: make(chan struct{}),
		buttons: map[string]bool{}, holdUntil: map[string]time.Duration{}, btnAvail: map[string]bool{}, env: cfg.Env,
	}
	for _, id := range ButtonIDs {
		e.buttons[id] = false
	}
	for _, pc := range cfg.Ports {
		e.ports = append(e.ports, portRuntime{
			cfg:    pc,
			plant:  model.NewPlant(pc.Fan, pc.Port, cfg.Seed),
			dutyCh: fmt.Sprintf("port%d.duty", pc.Port),
			rpmCh:  fmt.Sprintf("port%d.rpm", pc.Port),
		})
		p := &e.ports[len(e.ports)-1]
		p.evRPM, p.evState = float32(p.plant.Shaft()), stateName(p.plant.Running())
	}
	e.publish()
	return e
}

// Ready is closed once the engine listens on its bus socket.
func (e *Engine) Ready() <-chan struct{} { return e.ready }

// Snapshot returns the latest published state; safe from any goroutine.
func (e *Engine) Snapshot() Snapshot { return *e.snap.Load() }

func (e *Engine) publish() { e.snap.Store(e.build()) }

// build is the current state as a new Snapshot.
func (e *Engine) build() *Snapshot {
	s := &Snapshot{
		SimTime:        e.simNow,
		BusConnected:   e.connected,
		BusProto:       e.proto,
		Synced:         e.synced,
		BehindRealTime: e.behind,
		Ports:          make([]PortState, len(e.ports)),
		Env:            e.env,
		Buttons:        make(map[string]bool, len(e.buttons)),
		LEDs:           LEDState{Strip: append([]string{}, e.ledStrip...), Segments: make(map[string][]int, len(e.ledSegs))},
		Warnings:       append([]string{}, e.warnings...),
		Seq:            e.lastSeq,
	}
	for label, idx := range e.ledSegs {
		s.LEDs.Segments[label] = append([]int{}, idx...)
	}
	for i := range e.ports {
		p := &e.ports[i]
		state := "stopped"
		if p.plant.Running() {
			state = "running"
		}
		s.Ports[i] = PortState{Port: p.cfg.Port, Fan: p.cfg.FanID, Duty: p.duty, RPM: p.plant.Shaft(), State: state}
	}
	for id, v := range e.buttons {
		s.Buttons[id] = v
	}
	return s
}

func stateName(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

// emit hands one change event to Config.Emit, stamped with the current sim time. It reads the
// clock without moving simNow, so an event from a bus frame is exact to the microsecond while
// the model keeps stepping on ticks only.
func (e *Engine) emit(ev events.Event) {
	if e.cfg.Emit == nil {
		return
	}
	now := e.simNow
	if c := e.cfg.Clock.Now(); c > now && !e.inTick {
		now = c
	}
	ev.T = now.Microseconds()
	e.lastSeq = e.cfg.Emit(ev)
}

// emitState emits a state event when port p's running state differs from the last state event.
func (e *Engine) emitState(p *portRuntime) {
	if st := stateName(p.plant.Running()); st != p.evState {
		p.evState = st
		e.emit(events.Event{K: "state", Port: p.cfg.Port, V: st})
	}
}

// emitRPM samples every port's rpm for an rpm event (once per RPMEventStep, only on a change).
func (e *Engine) emitRPM() {
	for i := range e.ports {
		p := &e.ports[i]
		if e.simNow < p.nextRPM {
			continue
		}
		p.nextRPM = e.simNow + RPMEventStep
		if v := float32(p.plant.Shaft()); v != p.evRPM {
			p.evRPM = v
			e.emit(events.Event{K: "rpm", Port: p.cfg.Port, V: float64(v)})
		}
	}
}

// noteTick keeps the behind-real-time flag (UI-05). A tick is late when it was handled more than
// behindLate after its due time tickAt (handledAt is the clock when the engine got to it) or more
// than behindLate after the previous tick (dt). A late tick sets the flag, publishing sim "behind"
// on the edge; once behindClear of sim time has passed since the last late tick the flag clears
// with sim "in_sync". A late tick inside the hold-off restarts it, so the flag does not flicker. On
// a Manual clock every tick is delivered at its due time, so the flag never rises there.
func (e *Engine) noteTick(tickAt, handledAt, dt time.Duration) {
	if handledAt-tickAt > behindLate || dt > behindLate {
		e.lastLate = tickAt
		if !e.behind {
			e.behind = true
			e.emit(events.Event{K: "sim", V: "behind"})
		}
		return
	}
	if e.behind && tickAt-e.lastLate >= behindClear {
		e.behind = false
		e.emit(events.Event{K: "sim", V: "in_sync"})
	}
}

// releaseExpired releases every held button whose max hold has run out (D-15): the level goes
// false, the button event says why and the release reaches the firmware like an up from a client.
// It runs on ticks, so a release happens at most one tick after its deadline.
func (e *Engine) releaseExpired() {
	for _, id := range ButtonIDs {
		until, ok := e.holdUntil[id]
		if !ok || until > e.simNow {
			continue
		}
		delete(e.holdUntil, id)
		if !e.buttons[id] {
			continue
		}
		e.buttons[id] = false
		e.emit(events.Event{K: "button", ID: id, V: false, Reason: "max_hold"})
		if e.connected {
			e.send(e.buttonFrame(id))
		}
	}
}

// sampleDue reports whether a sample event is due at sim time now and, when it is, moves
// nextSample to the first slot beyond now: a slot the engine missed (a late tick) is skipped, never
// back-filled.
func (e *Engine) sampleDue(now time.Duration) bool {
	if now < e.nextSample {
		return false
	}
	for e.nextSample <= now {
		e.nextSample += sampleStep
	}
	return true
}

// emitSample emits the plot sample of this tick when one is due: every port's duty and rpm, or
// null for both while the firmware is not on the bus. The first sample of an engine carries Gap,
// so the stream itself marks the break a simulator restart leaves in sim time.
func (e *Engine) emitSample() {
	if !e.sampleDue(e.simNow) {
		return
	}
	v := make([]events.Sample, len(e.ports))
	for i := range e.ports {
		p := &e.ports[i]
		v[i].Port = p.cfg.Port
		if e.connected {
			duty, rpm := p.duty, p.plant.Shaft()
			v[i].Duty, v[i].RPM = &duty, &rpm
		}
	}
	e.emit(events.Event{K: events.SampleKind, V: v, Gap: !e.sampled})
	e.sampled = true
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
	e.nextSample = e.simNow + sampleStep
	e.nextEnv = [2]time.Duration{e.simNow + EnvResend, e.simNow + EnvResend}
	last := e.simNow
	// A new engine starts every port from rest; saying so lets a stream that outlives a simulator
	// restart see the states of the new engine.
	for i := range e.ports {
		p := &e.ports[i]
		p.evState = ""
		e.emitState(p)
	}
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
			e.inTick = true
			e.noteTick(now, clk.Now(), dt)
			for i := range e.ports {
				p := &e.ports[i]
				p.plant.Step(p.duty, dt)
				e.emitState(p)
			}
			e.emitRPM()
			e.emitSample()
			e.releaseExpired()
			e.inTick = false
			if e.connected && e.simNow >= e.nextRPM {
				e.nextRPM = e.simNow + rpmFrameStep
				e.sendRPM(false)
			}
			if e.connected {
				e.resendEnv()
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
//
// A stopped engine holds nothing: its World goes away and the next engine starts with every
// button released, which the firmware sees in the hello in-snapshot. So a button still held
// is released here and the stream says so (reason engine_stopped), and the final snapshot
// shows every button released. `sil watch`, `sil status` in a restart gap and the page then
// agree with the firmware (CLI-03, UI-04), whichever way the engine stopped (restart,
// shutdown or failure). No bus frame is sent: the bus is closed by now.
func (e *Engine) stopped() {
	if e.connected {
		e.emit(events.Event{K: "firmware", V: false})
	}
	for _, id := range ButtonIDs {
		if e.buttons[id] {
			e.buttons[id] = false
			e.emit(events.Event{K: "button", ID: id, V: false, Reason: "engine_stopped"})
		}
	}
	e.holdUntil = map[string]time.Duration{}
	e.connected, e.synced, e.proto = false, false, 0
	e.btnAvail = map[string]bool{}
	e.publish()
	close(e.done)
}

func (e *Engine) handle(ev event) {
	switch ev.kind {
	case evHello:
		e.connected, e.synced = true, false
		e.emit(events.Event{K: "firmware", V: true})
		e.btnAvail = map[string]bool{}
		e.proto = int(ev.hello.Proto)
		e.warnings = nil
		e.ledN, e.ledStrip, e.ledSegs = 0, nil, nil
		ev.reply <- e.inSnapshot()
	case evManifest:
		e.synced = true
		e.checkManifest(ev.chans)
	case evDisconnected:
		if e.connected {
			e.emit(events.Event{K: "firmware", V: false})
		}
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
	case evApply:
		e.handleApply(ev.apply)
	case evSync:
		snap := e.build()
		if e.cfg.Seq != nil {
			snap.Seq = e.cfg.Seq()
		}
		ev.sync <- *snap
	}
}

const (
	// maxWarnings bounds the warning list and maxWarningLen each entry. Warnings can carry peer
	// supplied text (channel names, a peer error) and the list is copied on every publish and
	// serialized in every status, so an unbounded list is a peer-driven growth path.
	maxWarnings   = 64
	maxWarningLen = 256

	warningsSuppressed = "further warnings suppressed"
)

// warn records a warning once. Entries are cut to maxWarningLen bytes (on a rune boundary) and the
// list stops at maxWarnings, its last entry saying that more were dropped.
func (e *Engine) warn(w string) {
	if len(w) > maxWarningLen {
		cut := maxWarningLen
		for cut > 0 && !utf8.RuneStart(w[cut]) {
			cut--
		}
		w = w[:cut] + "..."
	}
	for _, have := range e.warnings {
		if have == w {
			return
		}
	}
	switch {
	case len(e.warnings) >= maxWarnings:
		return
	case len(e.warnings) == maxWarnings-1:
		w = warningsSuppressed
	}
	e.warnings = append(e.warnings, w)
}

// inSnapshot is the in frame of every port with its current rpm, in port
// order, then temperature and humidity, then the level of every button in ButtonIDs order. The released
// levels matter: ESPHome runs no state callbacks for an entity's first state,
// so a button whose first level were 1 would lose its first press. It also
// marks the rpm values as sent, so the steady-state rate rule only sends
// changes from here on.
func (e *Engine) inSnapshot() []bus.Frame {
	frames := make([]bus.Frame, 0, len(e.ports)+2+len(ButtonIDs))
	for i := range e.ports {
		p := &e.ports[i]
		p.sent, p.sentOnce = float32(p.plant.Current()), true
		frames = append(frames, bus.NewFrame("in").Str("ch", p.rpmCh).Num("v", float64(p.sent)).Int("t", e.micros()))
	}
	frames = append(frames, e.envFrame(0), e.envFrame(1))
	e.nextEnv = [2]time.Duration{e.simNow + EnvResend, e.simNow + EnvResend}
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

// envChannels names the environment channels in snapshot order.
var envChannels = [2]string{ChanTemperature, ChanHumidity}

// envFrame is the in frame carrying environment value i (0 temperature, 1 humidity).
func (e *Engine) envFrame(i int) bus.Frame {
	v := e.env.Temperature
	if i == 1 {
		v = e.env.Humidity
	}
	return bus.NewFrame("in").Str("ch", envChannels[i]).Num("v", v).Int("t", e.micros())
}

// resendEnv sends every environment value whose deadline has passed and schedules the next one.
func (e *Engine) resendEnv() {
	for i := range e.nextEnv {
		if e.simNow >= e.nextEnv[i] {
			e.nextEnv[i] = e.simNow + EnvResend
			e.send(e.envFrame(i))
		}
	}
}

// Apply changes the inputs in c together: one inbox event, so no frame or status ever shows half
// of a change. Changed values reach the bus at once when it is connected. An engine whose Run has
// returned answers ErrStopped at once.
func (e *Engine) Apply(ctx context.Context, c Change) error {
	select {
	case <-e.done:
		return ErrStopped
	default:
	}
	op := applyOp{change: c, reply: make(chan struct{}, 1)}
	select {
	case e.inbox <- event{kind: evApply, apply: op}:
	case <-e.done:
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-op.reply:
		return nil
	case <-e.done:
		// A reply that was sent before the engine stopped still counts.
		select {
		case <-op.reply:
			return nil
		default:
			return ErrStopped
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handleApply applies one Change on the engine goroutine.
func (e *Engine) handleApply(op applyOp) {
	if now := e.cfg.Clock.Now(); now > e.simNow {
		e.simNow = now
	}
	if v := op.change.Temperature; v != nil {
		e.env.Temperature = *v
		e.emit(events.Event{K: "env", ID: ChanTemperature, V: *v})
		e.nextEnv[0] = e.simNow + EnvResend
		if e.connected {
			e.send(e.envFrame(0))
		}
	}
	if v := op.change.Humidity; v != nil {
		e.env.Humidity = *v
		e.emit(events.Event{K: "env", ID: ChanHumidity, V: *v})
		e.nextEnv[1] = e.simNow + EnvResend
		if e.connected {
			e.send(e.envFrame(1))
		}
	}
	// A replug resets the fan to rest (rpm 0) in this same event, so the snapshot published right
	// after it shows the new fan at 0 rpm; the normal rpm change rule sends the 0 and the next tick
	// steps the new model from rest at the current duty, so the new fan's own start rule decides.
	for i := range e.ports {
		p := &e.ports[i]
		if pc, ok := op.change.Fans[p.cfg.Port]; ok {
			pc.Port = p.cfg.Port
			p.cfg = pc
			p.plant = model.NewPlant(pc.Fan, pc.Port, e.cfg.Seed)
			e.emit(events.Event{K: "fan", Port: pc.Port, V: pc.FanID})
			e.emitState(p)
		}
	}
	e.publish() // before the reply, so a status read after Apply returned already shows the change
	op.reply <- struct{}{}
}

// SnapshotSync returns a snapshot taken on the engine goroutine, between two events: its state
// includes every event the engine emitted up to Seq and nothing after it. A watcher that
// subscribes to the broker first and then calls this skips the events with a seq up to
// Snapshot.Seq and forwards the rest, so no change is lost or repeated at the join. An engine
// whose Run has returned answers ErrStopped at once.
func (e *Engine) SnapshotSync(ctx context.Context) (Snapshot, error) {
	select {
	case <-e.done:
		return Snapshot{}, ErrStopped
	default:
	}
	reply := make(chan Snapshot, 1)
	select {
	case e.inbox <- event{kind: evSync, sync: reply}:
	case <-e.done:
		return Snapshot{}, ErrStopped
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case s := <-reply:
		return s, nil
	case <-e.done:
		select {
		case s := <-reply:
			return s, nil
		default:
			return Snapshot{}, ErrStopped
		}
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// SetButton sets the level of button id (true = held) and returns the sim
// time of the edge. strict applies the press rules in this order: the bus must
// be connected and the manifest received (ErrNotConnected), the manifest must
// have the channel (ErrInputNotAvailable), a hold needs a released button
// (ErrButtonBusy) and a release a held one (ErrButtonNotHeld). Non-strict only
// sets the level. A hold with maxHold > 0 is released by the engine after maxHold of sim time
// unless a client releases it first; any other edge cancels the deadline. An engine whose Run has
// returned answers ErrStopped at once.
func (e *Engine) SetButton(ctx context.Context, id string, level, strict bool, maxHold time.Duration) (time.Duration, error) {
	select {
	case <-e.done:
		return 0, ErrStopped
	default:
	}
	op := buttonOp{id: id, level: level, strict: strict, maxHold: maxHold, reply: make(chan buttonReply, 1)}
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
	if e.buttons[op.id] != op.level {
		e.emit(events.Event{K: "button", ID: op.id, V: op.level})
	}
	e.buttons[op.id] = op.level
	if op.level && op.maxHold > 0 {
		e.holdUntil[op.id] = e.simNow + op.maxHold
	} else {
		delete(e.holdUntil, op.id)
	}
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
	for _, id := range envChannels {
		known[id] = true
		c, ok := have[id]
		switch {
		case !ok:
		case c.Dir != "in":
			e.warn(fmt.Sprintf("channel %s has direction %q, expected \"in\"", id, c.Dir))
		case c.Type != "f32":
			e.warn(fmt.Sprintf("channel %s has type %q, expected \"f32\"", id, c.Type))
		}
	}
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
			duty := math.Min(1, math.Max(0, v))
			if duty != e.ports[i].duty {
				e.emit(events.Event{K: "duty", Port: e.ports[i].cfg.Port, V: duty})
			}
			e.ports[i].duty = duty
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
		v := float32(p.plant.Frame())
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
	e.emit(events.Event{K: "leds", Strip: append([]string{}, strip...)})
}

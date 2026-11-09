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
	"fmt"
	"log/slog"
	"math"
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
	Synced   bool
	Ports    []PortState
	Warnings []string
}

type eventKind int

const (
	evHello eventKind = iota
	evManifest
	evFrame
	evDisconnected
)

type event struct {
	kind   eventKind
	frame  bus.Frame
	hello  bus.Hello
	chans  []bus.Channel
	reason string
	reply  chan []bus.Frame
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
	e := &Engine{cfg: cfg, ready: make(chan struct{}), inbox: make(chan event, 1024)}
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
		Warnings:     append([]string{}, e.warnings...),
	}
	for i := range e.ports {
		p := &e.ports[i]
		state := "stopped"
		if p.state.Running {
			state = "running"
		}
		s.Ports[i] = PortState{Port: p.cfg.Port, Fan: p.cfg.FanID, Duty: p.duty, RPM: p.state.RPM, State: state}
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
			return nil
		case err := <-served:
			_ = srv.Close()
			cancel()
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
	e.publish()
}

func (e *Engine) handle(ev event) {
	switch ev.kind {
	case evHello:
		e.connected, e.synced = true, false
		e.proto = int(ev.hello.Proto)
		e.warnings = nil
		ev.reply <- e.inSnapshot()
	case evManifest:
		e.synced = true
		e.checkManifest(ev.chans)
	case evDisconnected:
		e.connected, e.synced = false, false
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
// order. It also marks the values as sent, so the steady-state rate rule only
// sends changes from here on.
func (e *Engine) inSnapshot() []bus.Frame {
	frames := make([]bus.Frame, 0, len(e.ports))
	for i := range e.ports {
		p := &e.ports[i]
		p.sent, p.sentOnce = float32(p.state.RPM), true
		frames = append(frames, bus.NewFrame("in").Str("ch", p.rpmCh).Num("v", float64(p.sent)).Int("t", e.micros()))
	}
	return frames
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

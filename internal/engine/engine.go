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
	Ports        []PortState
	Warnings     []string
}

type eventKind int

const (
	evConnected eventKind = iota
	evFrame
	evDisconnected
)

type event struct {
	kind  eventKind
	frame bus.Frame
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
	proto     int
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
		Ports:        make([]PortState, len(e.ports)),
		Warnings:     []string{},
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
func (h handler) Connected()        { h.put(event{kind: evConnected}) }
func (h handler) Frame(f bus.Frame) { h.put(event{kind: evFrame, frame: f}) }
func (h handler) Disconnected()     { h.put(event{kind: evDisconnected}) }
func (e *Engine) micros() int64     { return e.simNow.Microseconds() }
func (e *Engine) send(f bus.Frame)  { e.sendLog(e.srv.Send(f)) }
func (e *Engine) sendLog(err error) {
	if err != nil {
		e.cfg.Log.Warn("bus send failed", "err", err)
	}
}

// Run listens on the bus socket and drives the model until ctx is done. It
// returns nil after the listener is closed and the socket unlinked.
func (e *Engine) Run(ctx context.Context) error {
	srv, err := bus.Listen(e.cfg.BusPath)
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

func (e *Engine) handle(ev event) {
	switch ev.kind {
	case evConnected:
		e.connected = true
		e.proto = 0
		for i := range e.ports {
			e.ports[i].sentOnce = false
		}
	case evDisconnected:
		e.connected = false
		e.proto = 0
	case evFrame:
		e.handleFrame(ev.frame)
	}
}

func (e *Engine) handleFrame(f bus.Frame) {
	switch f.Kind() {
	case "hello":
		proto, _ := f.GetInt("proto")
		e.proto = int(proto)
		e.send(bus.NewFrame("welcome").Int("proto", bus.Proto).Int("t", e.micros()))
		e.sendRPM(true)
	case "out":
		ch, _ := f.GetStr("ch")
		for i := range e.ports {
			if e.ports[i].dutyCh != ch {
				continue
			}
			if v, ok := f.GetNum("v"); ok {
				e.ports[i].duty = math.Min(1, math.Max(0, v))
			}
		}
	}
	// Unknown kinds and channels are ignored (01-10 turns them into warnings).
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

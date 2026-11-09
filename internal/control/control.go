// Package control is the control API of one sil instance: the Status schema,
// the Control interface the supervisor implements, the HTTP handler that
// exposes it and the Unix-socket client the CLI uses.
//
// Phase 1 serves the API only on <run>/api.sock (mode 0600 inside the 0700
// run dir) because Status carries the API encryption key. A 127.0.0.1 TCP
// listener can be added later behind the same handler.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
)

// SchemaVersion is the Status.Schema value written by this version.
const SchemaVersion = 1

// Phase values of Status.Phase.
const (
	PhaseStarting = "starting"
	PhaseReady    = "ready"
	PhaseFailed   = "failed"
	PhaseStopping = "stopping"
	PhaseDead     = "dead"
)

// Status is the schema 1 view of a running instance. Fields are extended by
// later phases, never renamed.
type Status struct {
	Schema   int            `json:"schema"`
	Instance string         `json:"instance"`
	Phase    string         `json:"phase"` // starting|ready|failed|stopping|dead
	API      APIStatus      `json:"api"`
	Firmware FirmwareStatus `json:"firmware"`
	Sim      SimStatus      `json:"sim"`
	Ports    []PortStatus   `json:"ports"`
	// Buttons is the current level of every button (true = held): usr1, usr2, usr3.
	Buttons map[string]bool `json:"buttons"`
	// LEDs is the status LED strip the firmware wrote, with the firmware's own segment names.
	LEDs     LEDStatus     `json:"leds"`
	Warnings []string      `json:"warnings"`
	Error    *apperr.Error `json:"error,omitempty"`
}

// LEDStatus is the status LED strip: one "#rrggbb" per LED (the final pixel bytes, exactly as the
// firmware wrote them) and the named LED index ranges the firmware declares. Both are empty (never
// null) for a firmware without a leds channel.
type LEDStatus struct {
	Strip    []string         `json:"strip"`
	Segments map[string][]int `json:"segments"`
}

// APIStatus describes the firmware's native API endpoint.
type APIStatus struct {
	DeviceName string `json:"device_name"`
	MAC        string `json:"mac"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Key        string `json:"key"`
	Ready      bool   `json:"ready"`
}

// FirmwareStatus describes the firmware process.
type FirmwareStatus struct {
	PID          int   `json:"pid"`
	Running      bool  `json:"running"`
	BusConnected bool  `json:"bus_connected"`
	Restarts     int   `json:"restarts"`
	LastExit     *Exit `json:"last_exit"`
}

// Exit records how the firmware process ended.
type Exit struct {
	Kind   string  `json:"kind"` // requested|rebooted|crashed
	Code   *int    `json:"code"`
	Signal string  `json:"signal,omitempty"`
	AtS    float64 `json:"at_s"`
}

// SimStatus describes the simulator.
type SimStatus struct {
	TimeS          float64 `json:"time_s"`
	TimeFactor     int     `json:"time_factor"`
	EngineRestarts int     `json:"engine_restarts"`
	BusProto       int     `json:"bus_proto"`
}

// PortStatus is one fan port.
type PortStatus struct {
	Port  int     `json:"port"`
	Fan   string  `json:"fan"`
	Duty  float64 `json:"duty"`
	RPM   float64 `json:"rpm"`
	State string  `json:"state"`
}

// DownResult is the outcome of stopping an instance.
type DownResult struct {
	Instance string `json:"instance"`
	Stopped  bool   `json:"stopped"`
}

// MaxRestartGap is the longest `sil sim restart --gap` the control API accepts.
const MaxRestartGap = 30 * time.Second

// SimRestartRequest is the body of POST /api/v1/sim/restart.
type SimRestartRequest struct {
	// GapS is how long the simulator stays away before the new engine starts.
	GapS float64 `json:"gap_s"`
}

// Validate answers invalid_argument unless 0 <= gap_s <= 30.
func (r SimRestartRequest) Validate() error {
	if math.IsNaN(r.GapS) || r.GapS < 0 || r.GapS > MaxRestartGap.Seconds() {
		return apperr.Failed("invalid_argument", fmt.Sprintf("gap_s must be between 0 and %g seconds", MaxRestartGap.Seconds()),
			map[string]any{"gap_s": r.GapS})
	}
	return nil
}

// Button presses. The presets are shared by the CLI and later clients; the HTTP API itself needs an
// explicit hold_ms.
const (
	ShortPress = 200 * time.Millisecond
	LongPress  = 1500 * time.Millisecond
	MaxHold    = 60 * time.Second
)

// PressRequest is the body of POST /api/v1/buttons/{id}/press.
type PressRequest struct {
	// HoldMS is how long the button is held, in milliseconds of sim time.
	HoldMS int64 `json:"hold_ms"`
}

// Validate answers invalid_argument unless 1 <= hold_ms <= 60000.
func (r PressRequest) Validate() error {
	if r.HoldMS < 1 || r.HoldMS > MaxHold.Milliseconds() {
		return apperr.Failed("invalid_argument", fmt.Sprintf("hold_ms must be between 1 and %d", MaxHold.Milliseconds()),
			map[string]any{"hold_ms": r.HoldMS})
	}
	return nil
}

// PressResult is the outcome of a press, a down or an up.
type PressResult struct {
	Button string `json:"button"`
	Action string `json:"action"` // press|down|up
	HoldMS int64  `json:"hold_ms,omitempty"`
	Level  bool   `json:"level"`
	// TDownUS and TUpUS are the sim times (microseconds) of the edges.
	TDownUS int64 `json:"t_down_us,omitempty"`
	TUpUS   int64 `json:"t_up_us,omitempty"`
}

// Restart budgets of the client: the longest a restart request may take
// (30 s gap plus resync, or stop grace plus readiness) is well inside it.
const restartBudget = 60 * time.Second

// Control is what the supervisor offers and the client consumes.
type Control interface {
	Status(ctx context.Context) (Status, error)
	Down(ctx context.Context) (DownResult, error)
	// SimRestart replaces the simulator engine and returns the Status once the
	// firmware has resynced with the new one.
	SimRestart(ctx context.Context, r SimRestartRequest) (Status, error)
	// FwRestart restarts the firmware process and returns the Status once the
	// instance is ready again.
	FwRestart(ctx context.Context) (Status, error)
	// Press holds button id for r.HoldMS of sim time and returns once the
	// release edge was handed to the bus.
	Press(ctx context.Context, id string, r PressRequest) (PressResult, error)
	// SetButton sets the level of button id (down = held) and returns at once.
	SetButton(ctx context.Context, id string, down bool) (PressResult, error)
}

type errorBody struct {
	Error *apperr.Error `json:"error"`
}

// NewHandler exposes c as GET /api/v1/status, POST /api/v1/down,
// POST /api/v1/sim/restart, POST /api/v1/fw/restart and
// POST /api/v1/buttons/{id}/press|down|up. An
// *apperr.Error becomes {"error":{...}} with HTTP 4xx (exit class 1) or 5xx
// (exit class 2).
func NewHandler(c Control) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		st, err := c.Status(r.Context())
		reply(w, st, err)
	})
	mux.HandleFunc("POST /api/v1/down", func(w http.ResponseWriter, r *http.Request) {
		res, err := c.Down(r.Context())
		reply(w, res, err)
	})
	mux.HandleFunc("POST /api/v1/sim/restart", func(w http.ResponseWriter, r *http.Request) {
		var req SimRestartRequest
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			reply(w, nil, apperr.Failed("invalid_argument", "cannot read the request body: "+err.Error(), nil))
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				reply(w, nil, apperr.Failed("invalid_argument", "bad request body: "+err.Error(), nil))
				return
			}
		}
		if err := req.Validate(); err != nil {
			reply(w, nil, err)
			return
		}
		st, err := c.SimRestart(r.Context(), req)
		reply(w, st, err)
	})
	mux.HandleFunc("POST /api/v1/fw/restart", func(w http.ResponseWriter, r *http.Request) {
		st, err := c.FwRestart(r.Context())
		reply(w, st, err)
	})
	mux.HandleFunc("POST /api/v1/buttons/{id}/press", func(w http.ResponseWriter, r *http.Request) {
		var req PressRequest
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			reply(w, nil, apperr.Failed("invalid_argument", "cannot read the request body: "+err.Error(), nil))
			return
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			reply(w, nil, apperr.Failed("invalid_argument", "bad request body, want {\"hold_ms\": N}: "+err.Error(), nil))
			return
		}
		if err := req.Validate(); err != nil {
			reply(w, nil, err)
			return
		}
		res, err := c.Press(r.Context(), r.PathValue("id"), req)
		reply(w, res, err)
	})
	mux.HandleFunc("POST /api/v1/buttons/{id}/down", func(w http.ResponseWriter, r *http.Request) {
		res, err := c.SetButton(r.Context(), r.PathValue("id"), true)
		reply(w, res, err)
	})
	mux.HandleFunc("POST /api/v1/buttons/{id}/up", func(w http.ResponseWriter, r *http.Request) {
		res, err := c.SetButton(r.Context(), r.PathValue("id"), false)
		reply(w, res, err)
	})
	return mux
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		ae := apperr.AsError(err)
		code := http.StatusInternalServerError
		if ae.Exit == apperr.ExitFailed {
			code = http.StatusBadRequest
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(errorBody{Error: ae})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// Listen creates the Unix socket at sockPath (replacing a stale file) with
// mode 0600.
func Listen(sockPath string) (net.Listener, error) {
	if err := os.Remove(sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, apperr.Infra("control_listen_failed", "cannot remove stale control socket", map[string]any{"path": sockPath, "error": err.Error()})
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		return nil, apperr.Infra("control_listen_failed", "cannot listen on the control socket", map[string]any{"path": sockPath, "error": err.Error()})
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, apperr.Infra("control_listen_failed", "cannot restrict the control socket to its owner", map[string]any{"path": sockPath, "error": err.Error()})
	}
	return ln, nil
}

// ServeListener serves h on ln until ctx is done, then shuts the server down
// gracefully (in-flight requests such as the response to Down are finished)
// and returns nil. The http.Server has a Handler only: the wall-clock lint
// bans the stdlib timeout fields.
func ServeListener(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	sctx, cancel := clock.WithTimeout(context.Background(), clock.NewReal(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	<-done
	return nil
}

// Serve listens on sockPath and serves h until ctx is done.
func Serve(ctx context.Context, sockPath string, h http.Handler) error {
	ln, err := Listen(sockPath)
	if err != nil {
		return err
	}
	return ServeListener(ctx, ln, h)
}

// Client implements Control over a Unix socket.
type Client struct {
	sock string
	http *http.Client
}

// Dial returns a client for the control socket at sockPath. Nothing is
// connected until a call is made; request deadlines come from the caller's
// context (clock.WithTimeout).
func Dial(sockPath string) *Client {
	dialer := &net.Dialer{}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", sockPath)
		},
		DisableKeepAlives: true,
	}
	return &Client{sock: sockPath, http: &http.Client{Transport: tr}}
}

// Status implements Control.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	if err := c.do(ctx, http.MethodGet, "/api/v1/status", nil, &st); err != nil {
		return Status{}, err
	}
	if st.Error != nil && st.Error.Exit == 0 {
		st.Error.Exit = apperr.ExitInfra
	}
	return st, nil
}

// Down implements Control.
func (c *Client) Down(ctx context.Context) (DownResult, error) {
	var res DownResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/down", nil, &res); err != nil {
		return DownResult{}, err
	}
	return res, nil
}

// SimRestart implements Control. The request is bounded by a clock timeout,
// which answers resync_timeout.
func (c *Client) SimRestart(ctx context.Context, r SimRestartRequest) (Status, error) {
	rctx, cancel := clock.WithTimeout(ctx, clock.NewReal(), restartBudget)
	defer cancel()
	body, err := json.Marshal(r)
	if err != nil {
		return Status{}, apperr.Infra("internal", err.Error(), nil)
	}
	var st Status
	if err := c.do(rctx, http.MethodPost, "/api/v1/sim/restart", body, &st); err != nil {
		if clock.TimedOut(rctx) {
			return Status{}, apperr.Infra("resync_timeout", fmt.Sprintf("no answer to the simulator restart within %s", restartBudget), nil)
		}
		return Status{}, err
	}
	return st, nil
}

// FwRestart implements Control. The request is bounded by a clock timeout,
// which answers ready_timeout.
func (c *Client) FwRestart(ctx context.Context) (Status, error) {
	rctx, cancel := clock.WithTimeout(ctx, clock.NewReal(), restartBudget)
	defer cancel()
	var st Status
	if err := c.do(rctx, http.MethodPost, "/api/v1/fw/restart", nil, &st); err != nil {
		if clock.TimedOut(rctx) {
			return Status{}, apperr.Infra("ready_timeout", fmt.Sprintf("no answer to the firmware restart within %s", restartBudget), nil)
		}
		return Status{}, err
	}
	return st, nil
}

// Press implements Control. The request is bounded by MaxHold plus slack on
// the real clock; a fired budget is control_unreachable.
func (c *Client) Press(ctx context.Context, id string, r PressRequest) (PressResult, error) {
	rctx, cancel := clock.WithTimeout(ctx, clock.NewReal(), MaxHold+10*time.Second)
	defer cancel()
	body, err := json.Marshal(r)
	if err != nil {
		return PressResult{}, apperr.Infra("internal", err.Error(), nil)
	}
	var res PressResult
	if err := c.do(rctx, http.MethodPost, "/api/v1/buttons/"+url.PathEscape(id)+"/press", body, &res); err != nil {
		if clock.TimedOut(rctx) {
			return PressResult{}, apperr.Infra("control_unreachable", "no answer to the button press in time", nil)
		}
		return PressResult{}, err
	}
	return res, nil
}

// SetButton implements Control.
func (c *Client) SetButton(ctx context.Context, id string, down bool) (PressResult, error) {
	action := "up"
	if down {
		action = "down"
	}
	var res PressResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/buttons/"+url.PathEscape(id)+"/"+action, nil, &res); err != nil {
		return PressResult{}, err
	}
	return res, nil
}

func (c *Client) do(ctx context.Context, method, path string, in []byte, out any) error {
	var body io.Reader
	if in != nil {
		body = bytes.NewReader(in)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://sil"+path, body)
	if in != nil && err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return apperr.Infra("control_unreachable", "cannot build the control request", map[string]any{"error": err.Error()})
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Infra("control_unreachable", "cannot reach the instance control socket", map[string]any{"socket": c.sock, "error": err.Error()})
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Infra("control_unreachable", "control response was cut off", map[string]any{"socket": c.sock, "error": err.Error()})
	}
	if resp.StatusCode >= 400 {
		var eb errorBody
		if json.Unmarshal(raw, &eb) != nil || eb.Error == nil {
			return apperr.Infra("control_error", fmt.Sprintf("unexpected control response %s", resp.Status), nil)
		}
		eb.Error.Exit = apperr.ExitInfra
		if resp.StatusCode < 500 {
			eb.Error.Exit = apperr.ExitFailed
		}
		return eb.Error
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return apperr.Infra("control_error", "control response is not valid JSON", map[string]any{"error": err.Error()})
	}
	return nil
}

var _ Control = (*Client)(nil)

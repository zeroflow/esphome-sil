// Package control is the control API of one sil instance: the Status schema,
// the Control interface the supervisor implements, the HTTP handler that
// exposes it and the Unix-socket client the CLI uses.
//
// Phase 1 serves the API only on <run>/api.sock (mode 0600 inside the 0700
// run dir) because Status carries the API encryption key. A 127.0.0.1 TCP
// listener can be added later behind the same handler.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	Warnings []string       `json:"warnings"`
	Error    *apperr.Error  `json:"error,omitempty"`
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

// Control is what the supervisor offers and the client consumes.
type Control interface {
	Status(ctx context.Context) (Status, error)
	Down(ctx context.Context) (DownResult, error)
}

type errorBody struct {
	Error *apperr.Error `json:"error"`
}

// NewHandler exposes c as GET /api/v1/status and POST /api/v1/down. An
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
	if err := c.do(ctx, http.MethodGet, "/api/v1/status", &st); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/api/v1/down", &res); err != nil {
		return DownResult{}, err
	}
	return res, nil
}

func (c *Client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://sil"+path, nil)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Infra("control_unreachable", "control response was cut off", map[string]any{"socket": c.sock, "error": err.Error()})
	}
	if resp.StatusCode >= 400 {
		var eb errorBody
		if json.Unmarshal(body, &eb) != nil || eb.Error == nil {
			return apperr.Infra("control_error", fmt.Sprintf("unexpected control response %s", resp.Status), nil)
		}
		eb.Error.Exit = apperr.ExitInfra
		if resp.StatusCode < 500 {
			eb.Error.Exit = apperr.ExitFailed
		}
		return eb.Error
	}
	if err := json.Unmarshal(body, out); err != nil {
		return apperr.Infra("control_error", "control response is not valid JSON", map[string]any{"error": err.Error()})
	}
	return nil
}

var _ Control = (*Client)(nil)

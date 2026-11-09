package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/events"
)

// Operation is one row of the control API. The table Operations is the identity of the API: the
// socket handler (NewHandler), the LAN handler (NewLANHandler) and the page's request check all
// read it, and so does the CLI test that proves every row has a `sil` command. A new operation is
// a new row, with its LAN flag decided there; no route exists outside the table.
type Operation struct {
	// Name is the operation's identifier, unique in the table (status, sim.restart, watch.ws).
	Name string
	// Method and Path are the net/http ServeMux pattern parts, e.g. POST /api/v1/buttons/{id}/press.
	Method, Path string
	// Transport is how the reply travels: "json" (one value), "ndjson" (a line stream) or "ws"
	// (WebSocket frames; served by the web package, not by this one).
	Transport string
	// CLI is a working `sil` invocation of the same operation (without the leading "sil").
	CLI string
	// LAN says whether the web listener serves the row. A row with LAN false answers 403
	// operation_not_allowed there and never reaches the Control; api.sock serves every row.
	LAN bool

	// handler is nil only for the ws row.
	handler func(Control) http.HandlerFunc
}

// Operations is the control API, one row per operation.
//
// LAN false: down, sim.restart and fw.restart change the instance's life (D-19); logs, tail and
// follow, carries the instance's own log files (D-20). All of them stay on the control socket.
var Operations = []Operation{
	{Name: "status", Method: "GET", Path: "/api/v1/status", Transport: "json", CLI: "status", LAN: true, handler: opStatus},
	{Name: "down", Method: "POST", Path: "/api/v1/down", Transport: "json", CLI: "down", LAN: false, handler: opDown},
	{Name: "sim.restart", Method: "POST", Path: "/api/v1/sim/restart", Transport: "json", CLI: "sim restart", LAN: false, handler: opSimRestart},
	{Name: "fw.restart", Method: "POST", Path: "/api/v1/fw/restart", Transport: "json", CLI: "fw restart", LAN: false, handler: opFwRestart},
	{Name: "press", Method: "POST", Path: "/api/v1/buttons/{id}/press", Transport: "json", CLI: "press usr1", LAN: true, handler: opPress},
	{Name: "button.down", Method: "POST", Path: "/api/v1/buttons/{id}/down", Transport: "json", CLI: "press usr1 --down", LAN: true, handler: opButtonDown},
	{Name: "button.up", Method: "POST", Path: "/api/v1/buttons/{id}/up", Transport: "json", CLI: "press usr1 --up", LAN: true, handler: opButtonUp},
	{Name: "set", Method: "POST", Path: "/api/v1/set", Transport: "json", CLI: "set temperature=22", LAN: true, handler: opSet},
	{Name: "fans", Method: "GET", Path: "/api/v1/fans", Transport: "json", CLI: "fans", LAN: true, handler: opFans},
	{Name: "history", Method: "GET", Path: "/api/v1/history", Transport: "json", CLI: "history", LAN: true, handler: opHistory},
	{Name: "watch", Method: "GET", Path: "/api/v1/watch", Transport: "ndjson", CLI: "watch", LAN: true, handler: opWatch},
	{Name: "logs", Method: "GET", Path: "/api/v1/logs", Transport: "json", CLI: "logs", LAN: false, handler: opLogs},
	{Name: "watch.ws", Method: "GET", Path: "/ws", Transport: "ws", CLI: "watch", LAN: true},
}

func init() {
	names := map[string]bool{}
	patterns := map[string]bool{}
	for _, op := range Operations {
		pat := op.Method + " " + op.Path
		if names[op.Name] || patterns[pat] {
			panic("control.Operations: duplicate row " + op.Name + " (" + pat + ")")
		}
		names[op.Name], patterns[pat] = true, true
		if (op.handler == nil) != (op.Transport == "ws") {
			panic("control.Operations: row " + op.Name + " must have a handler exactly when its transport is not ws")
		}
	}
}

// OperationByName returns the row called name.
func OperationByName(name string) (Operation, bool) {
	for _, op := range Operations {
		if op.Name == name {
			return op, true
		}
	}
	return Operation{}, false
}

var (
	matchOnce sync.Once
	matchMux  *http.ServeMux
	matchRows map[string]Operation
)

// MatchOperation returns the row that serves method and path (a path without query), asking a
// ServeMux built from the table so wildcards and method qualifiers match exactly as they do when
// serving.
func MatchOperation(method, path string) (Operation, bool) {
	matchOnce.Do(func() {
		matchMux = http.NewServeMux()
		matchRows = make(map[string]Operation, len(Operations))
		for _, op := range Operations {
			pat := op.Method + " " + op.Path
			matchRows[pat] = op
			matchMux.HandleFunc(pat, func(http.ResponseWriter, *http.Request) {})
		}
	})
	if !strings.HasPrefix(path, "/") {
		return Operation{}, false
	}
	req, err := http.NewRequest(method, "http://sil.invalid"+path, nil)
	if err != nil {
		return Operation{}, false
	}
	_, pat := matchMux.Handler(req)
	op, ok := matchRows[pat]
	return op, ok
}

// NewLANHandler exposes the rows of Operations with LAN true over c and refuses every other row
// with 403 operation_not_allowed naming its `sil` command (D-19, D-20). A refused row never calls
// c. The ws row is not served here.
func NewLANHandler(c Control) http.Handler {
	mux := http.NewServeMux()
	for _, op := range Operations {
		if op.handler == nil {
			continue
		}
		if op.LAN {
			mux.HandleFunc(op.Method+" "+op.Path, op.handler(c))
			continue
		}
		mux.HandleFunc(op.Method+" "+op.Path, refuseOperation(op))
	}
	return mux
}

// refuseOperation answers 403 operation_not_allowed: the operation exists only on the instance's
// control socket, where the caller is on the machine.
func refuseOperation(op Operation) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteErrorStatus(w, http.StatusForbidden, apperr.Failed("operation_not_allowed",
			"`sil "+op.CLI+"` is only available on the instance's control socket: run `sil "+op.CLI+"` on this machine",
			map[string]any{"operation": op.Name, "cli": op.CLI}))
	}
}

func opStatus(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := c.Status(r.Context())
		reply(w, st, err)
	}
}

func opDown(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := c.Down(r.Context())
		reply(w, res, err)
	}
}

func opSimRestart(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func opFwRestart(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := c.FwRestart(r.Context())
		reply(w, st, err)
	}
}

func opPress(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func opButtonDown(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := c.SetButton(r.Context(), r.PathValue("id"), true)
		reply(w, res, err)
	}
}

func opButtonUp(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := c.SetButton(r.Context(), r.PathValue("id"), false)
		reply(w, res, err)
	}
}

func opSet(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SetRequest
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			reply(w, nil, apperr.Failed("invalid_argument", "cannot read the request body: "+err.Error(), nil))
			return
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			reply(w, nil, apperr.Failed("invalid_argument", "bad request body, want {\"values\": {\"key\": value}}: "+err.Error(), nil))
			return
		}
		if req.Values == nil {
			reply(w, nil, apperr.Failed("invalid_argument", "bad request body: values is missing or null", nil))
			return
		}
		res, err := c.Set(r.Context(), req)
		reply(w, res, err)
	}
}

func opFans(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := c.Fans(r.Context())
		reply(w, res, err)
	}
}

func opHistory(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := HistoryRequest{WindowS: MaxHistoryWindow.Seconds()}
		if q := r.URL.Query(); q.Has("window_s") {
			v := q.Get("window_s")
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				reply(w, nil, apperr.Failed("invalid_argument", fmt.Sprintf("window_s needs a number of seconds, got %q", v), map[string]any{"window_s": v}))
				return
			}
			req.WindowS = f
		}
		if err := req.Validate(); err != nil {
			reply(w, nil, err)
			return
		}
		res, err := c.History(r.Context(), req)
		if err == nil && res.Samples == nil {
			res.Samples = []events.Event{}
		}
		reply(w, res, err)
	}
}

func opWatch(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req WatchRequest
		if k := r.URL.Query().Get("kind"); k != "" {
			req.Kinds = strings.Split(k, ",")
		}
		if err := req.Validate(); err != nil {
			reply(w, nil, err)
			return
		}
		streamNDJSON(w, func(send func(any) error) error { return c.Watch(r.Context(), req, send) })
	}
}

func opLogs(c Control) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		req := LogsRequest{Source: LogSourceFirmware, Tail: DefaultLogTail}
		if v := q.Get("source"); v != "" {
			req.Source = v
		}
		if v := q.Get("tail"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				reply(w, nil, apperr.Failed("invalid_argument", fmt.Sprintf("tail needs an integer, got %q", v), map[string]any{"tail": v}))
				return
			}
			req.Tail = n
		}
		flags := map[string]bool{}
		for _, name := range []string{"raw", "follow"} {
			switch v := q.Get(name); v {
			case "", "0":
			case "1":
				flags[name] = true
			default:
				reply(w, nil, apperr.Failed("invalid_argument", fmt.Sprintf("%s must be 0 or 1, got %q", name, v), map[string]any{name: v}))
				return
			}
		}
		req.Raw = flags["raw"]
		if err := req.Validate(); err != nil {
			reply(w, nil, err)
			return
		}
		if flags["follow"] {
			streamNDJSON(w, func(send func(any) error) error { return c.FollowLogs(r.Context(), req, send) })
			return
		}
		res, err := c.Logs(r.Context(), req)
		reply(w, res, err)
	}
}

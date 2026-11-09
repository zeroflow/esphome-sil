// Package web is the instance's web front door: the embedded page, the control API behind a
// key-redacting wrapper, and the /ws stream whose frames are `sil watch` lines.
//
// The listener is reachable from the LAN without authentication (D-08). What keeps the instance
// safe on it is what the handler leaves out: the firmware's API key never leaves the process
// over this handler (D-18).
package web

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// RedactedKey replaces api.key in every Status the web handler returns (D-18).
const RedactedKey = "<redacted>"

// Options configure NewHandler.
type Options struct {
	// Static is the page file system (ui.FS(), or a directory during development).
	Static fs.FS
	// Clock times the websocket writes; the real clock when nil.
	Clock clock.Clock
	// Log receives connection-level notes; the default logger when nil.
	Log *slog.Logger
}

// NewHandler serves the page, /ws and the control API of c over the web listener. The signature
// is fixed: later plans extend behind it.
func NewHandler(c control.Control, o Options) http.Handler {
	if o.Clock == nil {
		o.Clock = clock.NewReal()
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	rc := redacting{c}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", control.NewHandler(rc))
	mux.Handle("GET /ws", newWS(rc, o))
	mux.Handle("/", staticHandler(o.Static))
	return mux
}

// staticHandler serves the page files for GET and HEAD and answers 405 for every other method.
func staticHandler(static fs.FS) http.Handler {
	if static == nil {
		return http.NotFoundHandler()
	}
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			control.WriteErrorStatus(w, http.StatusMethodNotAllowed,
				apperr.Failed("invalid_argument", "the page is read-only: only GET and HEAD are served here",
					map[string]any{"method": r.Method}))
			return
		}
		files.ServeHTTP(w, r)
	})
}

// redacting is the Control the web handler talks to: the supervisor's, except that api.key is
// replaced by RedactedKey in Status, in the Status that SimRestart and FwRestart return and in
// the snapshot line of Watch (D-18). Everything else passes through.
type redacting struct{ control.Control }

func redact(st control.Status) control.Status {
	st.API.Key = RedactedKey
	return st
}

func (r redacting) Status(ctx context.Context) (control.Status, error) {
	st, err := r.Control.Status(ctx)
	return redact(st), err
}

func (r redacting) SimRestart(ctx context.Context, req control.SimRestartRequest) (control.Status, error) {
	st, err := r.Control.SimRestart(ctx, req)
	return redact(st), err
}

func (r redacting) FwRestart(ctx context.Context) (control.Status, error) {
	st, err := r.Control.FwRestart(ctx)
	return redact(st), err
}

func (r redacting) Watch(ctx context.Context, req control.WatchRequest, send func(v any) error) error {
	return r.Control.Watch(ctx, req, func(v any) error {
		switch line := v.(type) {
		case control.SnapshotLine:
			line.Status = redact(line.Status)
			return send(line)
		case *control.SnapshotLine:
			cp := *line
			cp.Status = redact(cp.Status)
			return send(cp)
		}
		return send(v)
	})
}

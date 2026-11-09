package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
)

// wsWriteBudget is how long one frame write may take before the connection is given up.
const wsWriteBudget = 10 * time.Second

// Liveness and sharing of the stream slots (T-03-15). A browser that does not answer a ping within
// wsPongBudget is closed; browsers together may hold maxBrowserStreams of the instance's 16 Watch
// slots, so `sil watch` and `sil logs --follow` always keep the rest.
const (
	wsPingEvery       = 15 * time.Second
	wsPongBudget      = 10 * time.Second
	maxBrowserStreams = 8
)

// errorFrame is the last frame of a stream that failed after the upgrade.
type errorFrame struct {
	Error *apperr.Error `json:"error"`
}

// newWS serves GET /ws: Control.Watch over a websocket. Frame 1 is the `sil watch` snapshot line,
// every later frame is one event line or the stopped line, each encoded exactly like a `sil watch`
// line without its newline. `kind=` selects event kinds like `sil watch --kind`.
func newWS(c control.Control, o Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req control.WatchRequest
		if k := r.URL.Query().Get("kind"); k != "" {
			req.Kinds = strings.Split(k, ",")
		}
		if err := req.Validate(); err != nil {
			control.WriteError(w, err)
			return
		}
		// The default origin check authorizes the request host; Accept answers a rejection itself.
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		// Write-only use: CloseRead keeps ping, pong and close frames working and cancels ctx when the
		// peer goes away.
		ctx := conn.CloseRead(r.Context())

		write := func(payload []byte) error {
			wctx, cancel := clock.WithTimeout(ctx, o.Clock, wsWriteBudget)
			defer cancel()
			return conn.Write(wctx, websocket.MessageText, payload)
		}
		send := func(v any) error {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err != nil {
				return err
			}
			return write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
		}

		err = c.Watch(ctx, req, send)
		if err != nil && ctx.Err() == nil {
			ae := apperr.AsError(err)
			o.Log.Debug("websocket stream failed", "code", ae.Code, "error", ae.Message)
			if raw, merr := json.Marshal(errorFrame{Error: ae}); merr == nil {
				_ = write(raw)
			}
			_ = conn.Close(websocket.StatusTryAgainLater, ae.Code)
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	})
}

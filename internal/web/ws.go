package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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

// errPongTimeout is the cause of a stream context that ended because the browser stopped
// answering pings.
var errPongTimeout = errors.New("the browser did not answer a ping")

// errTooManyBrowsers is the final frame of the browser stream that does not fit under
// maxBrowserStreams. The code is the one the instance's 17th stream gets (exit class 2).
func errTooManyBrowsers() *apperr.Error {
	return apperr.Infra("too_many_streams",
		fmt.Sprintf("this bench already serves %d browser pages; close one", maxBrowserStreams),
		map[string]any{"limit": maxBrowserStreams})
}

// newWS serves GET /ws: Control.Watch over a websocket. Frame 1 is the `sil watch` snapshot line,
// every later frame is one event line or the stopped line, each encoded exactly like a `sil watch`
// line without its newline. `kind=` selects event kinds like `sil watch --kind`.
//
// A browser holds one of maxBrowserStreams slots (counted here, like the instance's CAS loop in
// supervisor.acquireStream) on top of the Watch slot, and is pinged every wsPingEvery: one that
// does not answer within wsPongBudget is closed, which gives both slots back.
func newWS(c control.Control, o Options) http.Handler {
	var browsers atomic.Int32
	acquire := func() bool {
		for {
			n := browsers.Load()
			if n >= maxBrowserStreams {
				return false
			}
			if browsers.CompareAndSwap(n, n+1) {
				return true
			}
		}
	}
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
		fail := func(ae *apperr.Error) {
			if raw, merr := json.Marshal(errorFrame{Error: ae}); merr == nil {
				_ = write(raw)
			}
			_ = conn.Close(websocket.StatusTryAgainLater, ae.Code)
		}

		// The 9th page is told why and never reaches Watch.
		if !acquire() {
			fail(errTooManyBrowsers())
			return
		}
		var once sync.Once
		release := func() { once.Do(func() { browsers.Add(-1) }) }
		defer release()

		// The ticker is armed before the first frame so that the first ping is always due wsPingEvery
		// after the page connected.
		wctx, stop := context.WithCancelCause(ctx)
		defer stop(nil)
		tick := o.Clock.NewTicker(wsPingEvery)
		defer tick.Stop()
		go func() {
			for {
				select {
				case <-tick.C():
					pctx, cancel := clock.WithTimeout(wctx, o.Clock, wsPongBudget)
					err := conn.Ping(pctx)
					cancel()
					if err != nil {
						if wctx.Err() == nil {
							stop(errPongTimeout)
						}
						return
					}
				case <-wctx.Done():
					return
				}
			}
		}()

		err = c.Watch(wctx, req, send)
		release() // both slots are free before any closing handshake starts
		switch {
		case errors.Is(context.Cause(wctx), errPongTimeout):
			o.Log.Debug("websocket stream reaped", "reason", errPongTimeout.Error())
			_ = conn.Close(websocket.StatusGoingAway, "no pong")
		case err != nil && ctx.Err() == nil:
			ae := apperr.AsError(err)
			o.Log.Debug("websocket stream failed", "code", ae.Code, "error", ae.Message)
			if raw, merr := json.Marshal(errorFrame{Error: ae}); merr == nil {
				_ = write(raw)
			}
			_ = conn.Close(websocket.StatusTryAgainLater, ae.Code)
		default:
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}
	})
}

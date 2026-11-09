package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/control"
	"github.com/zeroflow/esphome-sil/internal/web"
)

// getJSON decodes the JSON object GET url answers.
func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	ctx, cancel := clock.WithTimeout(context.Background(), realClock, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	if res.StatusCode != 200 || json.Unmarshal(raw, &m) != nil {
		t.Fatalf("GET %s = %d %s", url, res.StatusCode, raw)
	}
	return m
}

// diffPaths lists the JSON paths where a and b differ.
func diffPaths(path string, a, b any) []string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{path}
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		var out []string
		for k := range keys {
			out = append(out, diffPaths(path+"."+k, av[k], bv[k])...)
		}
		sort.Strings(out)
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return []string{path}
		}
		var out []string
		for i := range av {
			out = append(out, diffPaths(fmt.Sprintf("%s.%d", path, i), av[i], bv[i])...)
		}
		return out
	}
	if !reflect.DeepEqual(a, b) {
		return []string{path}
	}
	return nil
}

// TestSnapshotEqualsStatus (UI-06 check b, D-18): on a real engine, the first /ws frame minus t, k
// and seq equals GET /api/v1/status of the same web listener, and the status of the control socket
// handler differs from the TCP one in api.key alone.
func TestSnapshotEqualsStatus(t *testing.T) {
	s := watchSup(t)
	s.id.Name = "ws-parity"
	s.id.APIPort = 6099
	s.id.WebPort = 8099
	s.id.Key = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	setTemp(t, s, 26.5)

	webSrv := httptest.NewServer(web.NewHandler(s, web.Options{
		Static: fstest.MapFS{"index.html": {Data: []byte("x")}},
		Clock:  s.clk,
	}))
	t.Cleanup(webSrv.Close)
	sockSrv := httptest.NewServer(control.NewHandler(s))
	t.Cleanup(sockSrv.Close)

	ctx, cancel := clock.WithTimeout(context.Background(), realClock, 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(webSrv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("first frame %s: %v", data, err)
	}
	if frame["k"] != "snapshot" {
		t.Fatalf("first frame is %v, want the snapshot", frame["k"])
	}
	if _, ok := frame["seq"].(float64); !ok {
		t.Fatalf("snapshot has no seq: %s", data)
	}
	if _, ok := frame["t"].(float64); !ok {
		t.Fatalf("snapshot has no t: %s", data)
	}
	for _, k := range []string{"t", "k", "seq"} {
		delete(frame, k)
	}

	tcp := getJSON(t, webSrv.URL+"/api/v1/status")
	if d := diffPaths("", frame, tcp); len(d) != 0 {
		t.Errorf("the first /ws frame and GET status differ at %v\nframe: %v\nstatus: %v", d, frame, tcp)
	}
	if got := frame["api"].(map[string]any)["key"]; got != web.RedactedKey {
		t.Errorf("the snapshot's api.key = %v, want %s", got, web.RedactedKey)
	}

	sock := getJSON(t, sockSrv.URL+"/api/v1/status")
	if got := sock["api"].(map[string]any)["key"]; got != s.id.Key {
		t.Errorf("the socket status api.key = %v, want the real key", got)
	}
	if got := tcp["api"].(map[string]any)["key"]; got != web.RedactedKey {
		t.Errorf("the TCP status api.key = %v, want %s", got, web.RedactedKey)
	}
	sock["api"].(map[string]any)["key"] = web.RedactedKey
	if d := diffPaths("", sock, tcp); len(d) != 0 {
		t.Errorf("socket and TCP status differ at %v besides api.key", d)
	}

	// With the Manual clock still, nothing happens: no second frame.
	rctx, rcancel := clock.WithTimeout(ctx, realClock, 300*time.Millisecond)
	defer rcancel()
	if _, extra, err := c.Read(rctx); err == nil {
		t.Errorf("a second frame arrived without any change: %s", extra)
	} else if !clock.TimedOut(rctx) {
		// The library reports its own cancellation on a read timeout; only the context tells.
		t.Errorf("waiting for a second frame: %v, want the wait to time out", err)
	}
}

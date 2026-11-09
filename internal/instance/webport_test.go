package instance

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeIdentity stores a hand-written identity.json for instance name under root.
func writeIdentityFields(t *testing.T, root, name string, fields map[string]any) {
	t.Helper()
	dir := filepath.Join(root, "sil", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateWebPort(t *testing.T) {
	root := t.TempDir()
	writeIdentityFields(t, root, "a", map[string]any{"api_port": 8054, "web_port": 8055})
	writeIdentityFields(t, root, "self", map[string]any{"api_port": 8056})

	t.Run("skips_claimed_api_and_web_ports_and_own_api_port", func(t *testing.T) {
		got, err := AllocateWebPort(root, "self", 0, nil)
		if err != nil || got != 8057 {
			t.Fatalf("AllocateWebPort = %d %v, want 8057 (8054 and 8055 are claimed by a, 8056 is self's api_port)", got, err)
		}
	})
	t.Run("skips_unbindable_ports", func(t *testing.T) {
		got, err := AllocateWebPort(root, "self", 0, func(p int) bool { return p != 8057 })
		if err != nil || got != 8058 {
			t.Fatalf("AllocateWebPort = %d %v, want 8058", got, err)
		}
	})
	t.Run("explicit_port_checks", func(t *testing.T) {
		if _, err := AllocateWebPort(root, "self", 80, nil); code(err) != "invalid_argument" {
			t.Fatalf("port 80: %v, want invalid_argument", err)
		}
		if _, err := AllocateWebPort(root, "self", 8055, nil); code(err) != "port_claimed" {
			t.Fatalf("claimed port: %v, want port_claimed", err)
		}
		if got, err := AllocateWebPort(root, "self", 9000, nil); err != nil || got != 9000 {
			t.Fatalf("free explicit port = %d %v, want 9000", got, err)
		}
	})
	t.Run("stored_port_is_sticky", func(t *testing.T) {
		writeIdentityFields(t, root, "sticky", map[string]any{"api_port": 6054, "web_port": 8999})
		got, err := AllocateWebPort(root, "sticky", 0, func(int) bool { return false })
		if err != nil || got != 8999 {
			t.Fatalf("AllocateWebPort = %d %v, want the stored 8999", got, err)
		}
	})
	t.Run("none_free", func(t *testing.T) {
		_, err := AllocateWebPort(root, "self", 0, func(int) bool { return false })
		if code(err) != "no_free_port" {
			t.Fatalf("err = %v, want no_free_port", err)
		}
	})
}

func TestLoadOrCreateWebPort(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := PathsFor("dev")
	if err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreate(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id.WebPort < DefaultWebPort || id.WebPort >= DefaultWebPort+PortScanSpan {
		t.Fatalf("web port = %d, want within 8054..9053", id.WebPort)
	}
	if id.WebPort == id.APIPort {
		t.Fatalf("web port %d equals the API port", id.WebPort)
	}
	again, err := LoadOrCreate(p, 0)
	if err != nil || again != id {
		t.Fatalf("second load = %+v %v, want %+v (sticky)", again, err, id)
	}
}

// An identity written by Phase 1 or 2 has no web_port: the next LoadOrCreate gives it one and
// leaves the key and the API port alone (D-09).
func TestLoadOrCreateMigratesLegacyIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := PathsFor("old")
	if err != nil {
		t.Fatal(err)
	}
	first, err := LoadOrCreate(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeIdentityFields(t, p.StateRoot(), "old", map[string]any{"schema": 1, "name": "old", "api_port": first.APIPort, "key": first.Key})
	got, err := LoadOrCreate(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != first.Key || got.APIPort != first.APIPort || got.WebPort == 0 {
		t.Fatalf("migrated identity = %+v, want key %q and api_port %d kept and a web port", got, first.Key, first.APIPort)
	}
	raw, err := os.ReadFile(p.IdentityJSON())
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Identity
	if err := json.Unmarshal(raw, &onDisk); err != nil || onDisk != got {
		t.Fatalf("identity.json = %+v %v, want %+v", onDisk, err, got)
	}
}

func TestLoadOrCreateWebPortOutOfRangeIsCorrupt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := PathsFor("bad")
	if err != nil {
		t.Fatal(err)
	}
	first, err := LoadOrCreate(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeIdentityFields(t, p.StateRoot(), "bad", map[string]any{"schema": 1, "name": "bad", "api_port": first.APIPort, "web_port": 80, "key": first.Key})
	if _, err := LoadOrCreate(p, 0); code(err) != "identity_corrupt" {
		t.Fatalf("err = %v, want identity_corrupt", err)
	}
}

func TestListenWebAndURL(t *testing.T) {
	ln, err := ListenWeb("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if err := TestBindWeb("127.0.0.1", port); code(err) != "port_in_use" {
		t.Fatalf("TestBindWeb on a taken port: %v, want port_in_use", err)
	}
	if _, err := ListenWeb("203.0.113.1", port); code(err) != "port_bind_failed" {
		t.Fatalf("ListenWeb on a foreign address: %v, want port_bind_failed", err)
	}
	for _, tc := range []struct {
		listen, lan string
		want        string
	}{
		{"0.0.0.0", "192.168.4.7", "192.168.4.7"},
		{"", "192.168.4.7", "192.168.4.7"},
		{"0.0.0.0", "", "127.0.0.1"},
		{"127.0.0.1", "192.168.4.7", "127.0.0.1"},
		{"::1", "192.168.4.7", "[::1]"},
	} {
		want := "http" + "://" + tc.want + ":" + strconv.Itoa(8054) + "/"
		if got := WebURL(tc.listen, tc.lan, 8054); got != want {
			t.Errorf("WebURL(%q, %q) = %q, want %q", tc.listen, tc.lan, got, want)
		}
	}
}

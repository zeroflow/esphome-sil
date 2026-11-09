//go:build sil_e2e

package e2e

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fwGitStatus returns `git status --porcelain --ignored` of the repo.
func fwGitStatus(t testing.TB, root string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := exec.Command("git", "-C", root, "status", "--porcelain", "--ignored")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git status: %v", err)
	}
	return out.String()
}

// fwAssertStatic fails when program has a PT_INTERP header or .interp section.
func fwAssertStatic(t testing.TB, program string) {
	t.Helper()
	f, err := elf.Open(program)
	if err != nil {
		t.Fatalf("open ELF %s: %v", program, err)
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatalf("%s is dynamically linked (PT_INTERP present)", program)
		}
	}
	if f.Section(".interp") != nil {
		t.Fatalf("%s is dynamically linked (.interp section present)", program)
	}
}

// fwAssertNoRootFiles fails when any file under dir is owned by uid 0.
func fwAssertNoRootFiles(t testing.TB, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == 0 {
			t.Errorf("root-owned file in the build cache: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

func fwOutFor(ch string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["k"] == "out" && m["ch"] == ch }
}

// TestFirmwareBus runs the real firmware, built in the pinned image, against
// a test bus peer.
func TestFirmwareBus(t *testing.T) {
	root := repoRoot(t)
	before := fwGitStatus(t, root)

	id := newFWIdentity(t, "e2e-fw")
	program := buildFirmware(t, id)
	fwAssertStatic(t, program)
	fwAssertNoRootFiles(t, filepath.Join(cacheRoot(t), "sil", "build", id.Name))

	dir := shortTempDir(t)
	busPath := filepath.Join(dir, "bus.sock")
	proc := startFirmware(t, program, busPath, filepath.Join(dir, "prefs"))

	// Nothing listens yet: the firmware must keep retrying (500 ms) until the
	// peer appears, and must not die meanwhile.
	if err := sysClock.Sleep(t.Context(), time.Second); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if !proc.alive() {
		t.Fatalf("firmware exited while nothing listened on the bus")
	}
	peer := listenBus(t, busPath)

	hello := peer.Next(t, 3*time.Second, "hello frame", func(m map[string]any) bool { return m["k"] == "hello" })
	if hello["proto"] != float64(1) {
		t.Fatalf("hello proto = %v, want 1", hello["proto"])
	}
	for n := 1; n <= 4; n++ {
		ch := "port" + string(rune('0'+n)) + ".duty"
		m := peer.Next(t, 5*time.Second, "out frame for "+ch, fwOutFor(ch))
		if m["v"] != float64(0) {
			t.Errorf("%s boot value = %v, want 0 (ALWAYS_OFF)", ch, m["v"])
		}
	}
	if !proc.alive() {
		t.Fatalf("firmware exited")
	}
	t.Run("api_info", func(t *testing.T) {
		out, code := runAPIClient(t, id.Port, id.Key, "info")
		if code != 0 {
			t.Fatalf("info exit %d: %v", code, out)
		}
		if out["device_name"] != "sil-"+id.Name {
			t.Errorf("device_name = %v, want sil-%s", out["device_name"], id.Name)
		}
		norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, ":", "")) }
		if mac, _ := out["mac_address"].(string); norm(mac) != norm(id.MAC) {
			t.Errorf("mac_address = %v, want %s", out["mac_address"], id.MAC)
		}
		have := map[string]bool{}
		if list, ok := out["entities"].([]any); ok {
			for _, e := range list {
				if s, ok := e.(string); ok {
					have[s] = true
				}
			}
		}
		for n := 1; n <= 4; n++ {
			for _, name := range []string{fmt.Sprintf("Fan %d", n), fmt.Sprintf("Fan %d Speed", n)} {
				if !have[name] {
					t.Errorf("entity %q missing from %v", name, out["entities"])
				}
			}
		}
	})

	// setFan drives one fan through the encrypted API and expects the duty on the bus.
	setFan := func(t *testing.T, fan, speed int, want float64) {
		t.Helper()
		out, code := runAPIClient(t, id.Port, id.Key, "set-fan", "--fan", fmt.Sprint(fan), "--speed", fmt.Sprint(speed))
		if code != 0 {
			t.Fatalf("set-fan exit %d: %v", code, out)
		}
		ch := fmt.Sprintf("port%d.duty", fan)
		m := peer.Next(t, 5*time.Second, fmt.Sprintf("%s = %v", ch, want), func(m map[string]any) bool {
			return fwOutFor(ch)(m) && m["v"] == want
		})
		t.Logf("fan %d speed %d -> %v", fan, speed, m)
	}

	t.Run("fan1_duty", func(t *testing.T) { setFan(t, 1, 40, 0.4) })

	t.Run("four_ports", func(t *testing.T) {
		setFan(t, 2, 20, 0.2)
		setFan(t, 3, 60, 0.6)
		setFan(t, 4, 100, 1.0)
	})

	t.Run("rpm_in", func(t *testing.T) {
		peer.Send(t, `{"k":"in","ch":"port1.rpm","v":1234}`)
		out, code := runAPIClient(t, id.Port, id.Key, "watch", "--seconds", "3")
		if code != 0 {
			t.Fatalf("watch exit %d: %v", code, out)
		}
		series, _ := out["series"].(map[string]any)
		samples, _ := series["Fan 1 Speed"].([]any)
		for _, s := range samples {
			if pair, ok := s.([]any); ok && len(pair) == 2 && pair[1] == float64(1234) {
				return
			}
		}
		t.Fatalf("Fan 1 Speed never reached 1234: %v", series)
	})

	if after := fwGitStatus(t, root); after != before {
		t.Fatalf("working tree changed by the build:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

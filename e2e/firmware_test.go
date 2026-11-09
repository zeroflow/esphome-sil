//go:build sil_e2e

package e2e

import (
	"bytes"
	"debug/elf"
	"io/fs"
	"os/exec"
	"path/filepath"
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
	if after := fwGitStatus(t, root); after != before {
		t.Fatalf("working tree changed by the build:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

//go:build sil_e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeroflow/esphome-sil/internal/clock"
	"github.com/zeroflow/esphome-sil/internal/fwbuild"
)

// codegenHeader is the part every inline fixture shares: a minimal host
// firmware that loads the repo's sim_bus component.
func codegenHeader(t testing.TB, name string) string {
	t.Helper()
	return fmt.Sprintf(`esphome:
  name: fx-%s

host:

logger:

external_components:
  - source:
      type: local
      path: %s
    components: [sim_bus]

`, name, filepath.Join(repoRoot(t), "components"))
}

// codegenFixture writes header + body as <case>.yaml into a fresh short temp
// dir and returns the file path.
func codegenFixture(t testing.TB, name, body string) string {
	t.Helper()
	dir := shortTempDir(t)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(codegenHeader(t, name)+body), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
	return path
}

// codegenConfig runs `esphome config` on yamlPath in the pinned image with
// no network and the repo and the YAML's directory mounted read-only. extra
// are esphome global options (such as -s key value) placed before the
// subcommand. It returns the combined output and the exit code.
func codegenConfig(t testing.TB, yamlPath string, env []string, extra ...string) (string, int) {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Dir(yamlPath)
	args := []string{
		"run", "--rm", "--pull=never", "--network", "none",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp",
		"-e", "ESPHOME_DATA_DIR=/tmp/data",
		"-e", "ESPHOME_NOGITIGNORE=1",
		"-e", "SIL_API_KEY",
		"-v", root + ":" + root + ":ro",
	}
	if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		args = append(args, "-v", dir+":"+dir+":ro")
	}
	args = append(args, "-w", dir, fwbuild.Image)
	args = append(args, extra...)
	args = append(args, "config", filepath.Base(yamlPath))

	ctx, cancel := clock.WithTimeout(context.Background(), sysClock, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run esphome config: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// TestCodegenValidation proves the config-time errors of the sim_bus
// component (IO-06, BUS-03) in the pinned image, and that the example still
// validates.
func TestCodegenValidation(t *testing.T) {
	root := repoRoot(t)
	before := fwGitStatus(t, root)

	longPath := "/tmp/" + strings.Repeat("é", 60) // 65 characters, 125 bytes
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "missing-channel",
			body: "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n",
			want: []string{"'channel' is a required option"},
		},
		{
			name: "bad-channel",
			body: "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: Port1.Duty\n",
			want: []string{"invalid sim_bus channel 'Port1.Duty'"},
		},
		{
			name: "dup-channel",
			body: "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: port1.duty\n  - platform: sim_bus\n    id: o2\n    channel: port1.duty\n",
			want: []string{"duplicate sim_bus channel 'port1.duty'", "o1", "o2"},
		},
		{
			name: "long-path",
			body: "sim_bus:\n  id: bus\n  path: \"" + longPath + "\"\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: port1.duty\n",
			want: []string{"sun_path limit is 107"},
		},
		{
			name: "empty-path",
			body: "sim_bus:\n  id: bus\n  path: \"\"\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: port1.duty\n",
			want: []string{"must not be empty"},
		},
	}
	byteCount := regexp.MustCompile(`sim_bus path is (\d+) bytes`)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := codegenConfig(t, codegenFixture(t, c.name, c.body), nil)
			if code == 0 {
				t.Fatalf("esphome config accepted the %s fixture:\n%s", c.name, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			if c.name == "long-path" {
				m := byteCount.FindStringSubmatch(out)
				if m == nil {
					t.Fatalf("no byte count in output:\n%s", out)
				}
				if n, _ := strconv.Atoi(m[1]); n < 120 {
					t.Errorf("byte count %d, want at least 120 (UTF-8 bytes, not characters)", n)
				}
			}
		})
	}

	// Boundary: a one-character channel is legal.
	t.Run("one-char-channel", func(t *testing.T) {
		body := "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: a\n"
		if out, code := codegenConfig(t, codegenFixture(t, "one-char-channel", body), nil); code != 0 {
			t.Fatalf("one-character channel rejected (exit %d):\n%s", code, out)
		}
	})

	// Rejected shapes beyond Port1.Duty: each must be named in the message.
	for _, ch := range []string{"port1..duty", ".port1", "port1."} {
		t.Run("bad-channel "+ch, func(t *testing.T) {
			body := "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: \"" + ch + "\"\n"
			out, code := codegenConfig(t, codegenFixture(t, "bad-shape", body), nil)
			if code == 0 || !strings.Contains(out, "invalid sim_bus channel '"+ch+"'") {
				t.Fatalf("channel %q: exit %d, output:\n%s", ch, code, out)
			}
		})
	}
	t.Run("empty-channel", func(t *testing.T) {
		body := "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: \"\"\n"
		out, code := codegenConfig(t, codegenFixture(t, "empty-channel", body), nil)
		if code == 0 || !strings.Contains(out, "invalid sim_bus channel ''") {
			t.Fatalf("empty channel: exit %d, output:\n%s", code, out)
		}
	})

	// Duplicates across platforms count too.
	t.Run("dup-channel-across-platforms", func(t *testing.T) {
		body := "sim_bus:\n  id: bus\n\noutput:\n  - platform: sim_bus\n    id: o1\n    channel: port1.x\n\nsensor:\n  - platform: sim_bus\n    id: s1\n    name: S1\n    channel: port1.x\n"
		out, code := codegenConfig(t, codegenFixture(t, "dup-across", body), nil)
		if code == 0 || !strings.Contains(out, "duplicate sim_bus channel 'port1.x'") {
			t.Fatalf("exit %d, output:\n%s", code, out)
		}
	})

	t.Run("fan-skeleton", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
		out, code := codegenConfig(t, filepath.Join(root, "examples", "fan-skeleton.yaml"),
			[]string{"SIL_API_KEY=" + key},
			"-s", "sil_name", "codegen", "-s", "sil_mac", "02:00:00:00:00:02", "-s", "sil_port", "6099")
		if code != 0 {
			t.Fatalf("examples/fan-skeleton.yaml rejected (exit %d):\n%s", code, out)
		}
	})

	if after := fwGitStatus(t, root); after != before {
		t.Fatalf("esphome config wrote to the repo tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

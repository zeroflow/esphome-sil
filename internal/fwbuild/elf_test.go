package fwbuild

import (
	"debug/elf"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

func elfGoTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(p); err != nil {
		t.Skip("no go tool available to build a static test binary")
	}
	return p
}

func elfNotStatic(t *testing.T, err error) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "not_static" || ae.Exit != apperr.ExitInfra {
		t.Fatalf("want Infra not_static, got %v", err)
	}
}

func TestAssertStatic(t *testing.T) {
	t.Parallel()
	t.Run("static go binary", func(t *testing.T) {
		t.Parallel()
		work, out := t.TempDir(), t.TempDir()
		src := filepath.Join(work, "main.go")
		if err := os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(out, "static")
		cmd := exec.Command(elfGoTool(t), "build", "-o", bin, src)
		cmd.Dir = out
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, raw)
		}
		if err := AssertStatic(bin); err != nil {
			t.Fatalf("AssertStatic(static binary) = %v", err)
		}
	})
	t.Run("dynamic system binary", func(t *testing.T) {
		t.Parallel()
		path, err := exec.LookPath("ls")
		if err != nil {
			t.Skip("no ls")
		}
		f, err := elf.Open(path)
		if err != nil {
			t.Skipf("ls is not an ELF file: %v", err)
		}
		libs, _ := f.ImportedLibraries()
		f.Close()
		if len(libs) == 0 {
			t.Skip("ls is statically linked on this system")
		}
		elfNotStatic(t, AssertStatic(path))
	})
	t.Run("not an elf", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "script")
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		elfNotStatic(t, AssertStatic(p))
	})
	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		elfNotStatic(t, AssertStatic(filepath.Join(t.TempDir(), "nope")))
	})
}

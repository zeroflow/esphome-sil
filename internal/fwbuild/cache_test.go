package fwbuild

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func cacheWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cacheHash(t *testing.T, in HashInputs) string {
	t.Helper()
	h, err := InputHash(in)
	if err != nil {
		t.Fatalf("InputHash: %v", err)
	}
	return h
}

func TestInputHash(t *testing.T) {
	t.Parallel()
	comp := t.TempDir()
	cacheWrite(t, filepath.Join(comp, "sim_bus", "__init__.py"), "x = 1\n")
	cacheWrite(t, filepath.Join(comp, "sim_bus", "sim_bus.cpp"), "int a;\n")
	inc := filepath.Join(t.TempDir(), "extra.h")
	cacheWrite(t, inc, "#define A 1\n")

	base := HashInputs{RecipeVersion: "1", ImageID: "sha256:aaa", Resolved: []byte("esphome:\n"), ComponentDirs: []string{comp}, IncludeFiles: []string{inc}}
	h := cacheHash(t, base)
	if h2 := cacheHash(t, base); h2 != h {
		t.Fatalf("identical inputs hash differently: %s vs %s", h, h2)
	}

	t.Run("recipe", func(t *testing.T) {
		in := base
		in.RecipeVersion = "2"
		if cacheHash(t, in) == h {
			t.Fatal("recipe version change not reflected")
		}
	})
	t.Run("image", func(t *testing.T) {
		in := base
		in.ImageID = "sha256:bbb"
		if cacheHash(t, in) == h {
			t.Fatal("image id change not reflected")
		}
	})
	t.Run("resolved", func(t *testing.T) {
		in := base
		in.Resolved = []byte("esphome:!")
		if cacheHash(t, in) == h {
			t.Fatal("resolved config change not reflected")
		}
	})
	t.Run("component byte", func(t *testing.T) {
		comp2 := t.TempDir()
		cacheWrite(t, filepath.Join(comp2, "sim_bus", "__init__.py"), "x = 1\n")
		cacheWrite(t, filepath.Join(comp2, "sim_bus", "sim_bus.cpp"), "int b;\n") // one byte differs
		in := base
		in.ComponentDirs = []string{comp2}
		if cacheHash(t, in) == h {
			t.Fatal("component source change not reflected")
		}
	})
	t.Run("include byte", func(t *testing.T) {
		inc2 := filepath.Join(t.TempDir(), "extra.h")
		cacheWrite(t, inc2, "#define A 2\n")
		in := base
		in.IncludeFiles = []string{inc2}
		if cacheHash(t, in) == h {
			t.Fatal("include change not reflected")
		}
	})
	t.Run("walk order and location do not matter", func(t *testing.T) {
		comp2 := t.TempDir() // same layout, files created in the opposite order, other location
		cacheWrite(t, filepath.Join(comp2, "sim_bus", "sim_bus.cpp"), "int a;\n")
		cacheWrite(t, filepath.Join(comp2, "sim_bus", "__init__.py"), "x = 1\n")
		inc2 := filepath.Join(t.TempDir(), "extra.h")
		cacheWrite(t, inc2, "#define A 1\n")
		in := base
		in.ComponentDirs = []string{comp2}
		in.IncludeFiles = []string{inc2}
		if cacheHash(t, in) != h {
			t.Fatal("hash depends on creation order or location")
		}
	})
	t.Run("bytecode caches are ignored", func(t *testing.T) {
		cacheWrite(t, filepath.Join(comp, "sim_bus", "__pycache__", "x.cpython-312.pyc"), "junk")
		cacheWrite(t, filepath.Join(comp, "sim_bus", "stale.pyc"), "junk")
		if cacheHash(t, base) != h {
			t.Fatal("pyc files changed the hash")
		}
	})
	t.Run("missing tree is an error", func(t *testing.T) {
		in := base
		in.ComponentDirs = []string{filepath.Join(comp, "nope")}
		if _, err := InputHash(in); err == nil {
			t.Fatal("want an error for a missing component dir")
		}
	})
}

func TestBuildRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	prog := filepath.Join(dir, "program")
	cacheWrite(t, prog, "elf")

	if _, ok := ReadRecord(dir); ok {
		t.Fatal("ReadRecord on an empty dir must report not ok")
	}
	if err := WriteRecord(dir, BuildRecord{Hash: "abc", Program: prog}); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	rec, ok := ReadRecord(dir)
	if !ok || !reflect.DeepEqual(rec, BuildRecord{Schema: 1, Hash: "abc", Program: prog}) {
		t.Fatalf("round trip: %+v ok=%v", rec, ok)
	}
	st, err := os.Stat(filepath.Join(dir, "sil-build.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("record file: %v %v", st, err)
	}
	if !rec.Hit("abc") {
		t.Fatal("matching hash with an existing program must hit")
	}
	if rec.Hit("abd") {
		t.Fatal("other hash must miss")
	}
	if err := os.Remove(prog); err != nil {
		t.Fatal(err)
	}
	if rec.Hit("abc") {
		t.Fatal("a missing program must miss")
	}
	cacheWrite(t, filepath.Join(dir, "sil-build.json"), "{not json")
	if _, ok := ReadRecord(dir); ok {
		t.Fatal("a corrupt record must report not ok")
	}
	removeRecord(dir)
	removeRecord(dir) // idempotent
}

func TestLocalInputs(t *testing.T) {
	t.Parallel()
	resolved := []byte(`esphome:
  name: x
  includes:
    - inc/a.h
    - /abs/b.h
  includes_c: [c.c]
external_components:
  - source:
      type: local
      path: ../components
    components: [sim_bus]
  - source:
      type: git
      url: https://example.invalid/x
  - source:
      type: local
      path: /abs/comp
`)
	comps, incs := localInputs(resolved, "/y")
	if want := []string{"/components", "/abs/comp"}; !reflect.DeepEqual(comps, want) {
		t.Fatalf("components = %v, want %v", comps, want)
	}
	if want := []string{"/y/inc/a.h", "/abs/b.h", "/y/c.c"}; !reflect.DeepEqual(incs, want) {
		t.Fatalf("includes = %v, want %v", incs, want)
	}
	// The captured fixture has exactly the one local component source.
	raw, err := os.ReadFile("testdata/resolved-example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	comps, incs = localInputs(raw, "/repo/examples")
	if want := []string{"/repo/examples/../components"}; !reflect.DeepEqual(comps, want) || len(incs) != 0 {
		t.Fatalf("fixture: components=%v includes=%v", comps, incs)
	}
}

func TestToolcache(t *testing.T) {
	t.Parallel()
	if got, want := toolcacheDir("/c/sil/build/dev"), "/c/sil/toolcache"; got != want {
		t.Fatalf("toolcacheDir = %q, want %q", got, want)
	}

	root := t.TempDir()
	tool := filepath.Join(root, "toolcache")
	dataA, cacheA := filepath.Join(root, "a", "data"), filepath.Join(root, "a", "cache")
	dataB, cacheB := filepath.Join(root, "b", "data"), filepath.Join(root, "b", "cache")
	httpA := filepath.Join(cacheA, httpCacheRel)

	// No seed yet: nothing to copy, nothing to write back.
	if seeded, err := seedInstance(tool, dataA, cacheA); err != nil || seeded {
		t.Fatalf("seedInstance without seed = %v, %v", seeded, err)
	}
	if err := writeBackToolcache(tool, dataA, cacheA); err != nil {
		t.Fatalf("writeBack without source: %v", err)
	}
	if _, err := os.Stat(tool); !os.IsNotExist(err) {
		t.Fatalf("toolcache must not be created without a source: %v", err)
	}

	// An online build leaves archives and registry responses in A; they are
	// written back once, the responses with a frozen expiry.
	cacheWrite(t, filepath.Join(dataA, "pio_components", "host", "h1", "lib.c"), "A")
	if err := os.Symlink("lib.c", filepath.Join(dataA, "pio_components", "host", "h1", "link")); err != nil {
		t.Fatal(err)
	}
	cacheWrite(t, filepath.Join(httpA, "aaaa1111"), `{"registry":"a"}`)
	cacheWrite(t, filepath.Join(httpA, "db.data"), "1794192645=aaaa1111\n1794192645=gone0000\n")
	if err := writeBackToolcache(tool, dataA, cacheA); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	db, _ := os.ReadFile(filepath.Join(tool, "pio_http", "db.data"))
	if string(db) != "4102444800=aaaa1111\n" {
		t.Fatalf("frozen db.data = %q", db)
	}
	cacheWrite(t, filepath.Join(dataA, "pio_components", "host", "h1", "lib.c"), "A2")
	if err := writeBackToolcache(tool, dataA, cacheA); err != nil { // second call is a no-op
		t.Fatalf("second writeBack: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(tool, "pio_components", "host", "h1", "lib.c")); string(raw) != "A" {
		t.Fatalf("toolcache was overwritten: %q", raw)
	}
	entries, _ := os.ReadDir(tool)
	if len(entries) != 2 {
		t.Fatalf("want pio_components and pio_http only, got %v", entries)
	}

	// A new instance is seeded; an instance that has its own archives keeps
	// them, but always gets the frozen registry responses.
	if seeded, err := seedInstance(tool, dataB, cacheB); err != nil || !seeded {
		t.Fatalf("seedInstance = %v, %v", seeded, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dataB, "pio_components", "host", "h1", "lib.c")); string(raw) != "A" {
		t.Fatalf("seed content = %q", raw)
	}
	if link, err := os.Readlink(filepath.Join(dataB, "pio_components", "host", "h1", "link")); err != nil || link != "lib.c" {
		t.Fatalf("symlink not copied: %q %v", link, err)
	}
	cacheWrite(t, filepath.Join(dataB, "pio_components", "host", "h1", "lib.c"), "B")
	cacheWrite(t, filepath.Join(cacheB, httpCacheRel, "db.data"), "1=stale\n")
	if seeded, err := seedInstance(tool, dataB, cacheB); err != nil || !seeded {
		t.Fatalf("second seedInstance = %v, %v", seeded, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dataB, "pio_components", "host", "h1", "lib.c")); string(raw) != "B" {
		t.Fatalf("existing instance copy was replaced: %q", raw)
	}
	if db, _ := os.ReadFile(filepath.Join(cacheB, httpCacheRel, "db.data")); string(db) != "4102444800=aaaa1111\n" {
		t.Fatalf("instance registry cache not replaced by the frozen copy: %q", db)
	}
	for _, d := range []string{dataB, filepath.Join(cacheB, "platformio", "cache")} {
		leftovers, _ := filepath.Glob(filepath.Join(d, ".seed-*"))
		if len(leftovers) != 0 {
			t.Fatalf("temp dirs left behind: %v", leftovers)
		}
	}

	// A toolcache with only the archives (an older layout) is not a seed.
	half := filepath.Join(root, "half")
	cacheWrite(t, filepath.Join(half, "pio_components", "x"), "1")
	if seeded, _ := seedInstance(half, filepath.Join(root, "c", "data"), filepath.Join(root, "c", "cache")); seeded {
		t.Fatal("a toolcache without registry responses must not count as seeded")
	}
}

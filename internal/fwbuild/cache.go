package fwbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// RecipeVersion changes whenever the build recipe (docker flags, stages,
// lint rules that influence the result) changes, so old build records are
// never reused across recipes.
const RecipeVersion = "1"

const (
	recordName       = "sil-build.json"
	recordSchema     = 1
	pioComponentsDir = "pio_components"
)

// HashInputs are everything a build result depends on (D-15).
type HashInputs struct {
	RecipeVersion string
	ImageID       string
	// Resolved is the stage-1 `esphome config` stdout: substitutions, the
	// injected identity, resolved packages and local component paths.
	Resolved []byte
	// ComponentDirs are the local external_components source directories and
	// IncludeFiles the esphome.includes / includes_c entries (files or
	// directories). Their contents are hashed.
	ComponentDirs, IncludeFiles []string
}

// InputHash returns the hex sha256 over the recipe version, the image ID, the
// resolved config and the contents of the component and include trees. The
// result does not depend on directory walk order or on where the trees live
// on disk (only on their relative layout and bytes).
func InputHash(in HashInputs) (string, error) {
	h := sha256.New()
	field := func(label string, data []byte) {
		fmt.Fprintf(h, "%s %d\n", label, len(data))
		h.Write(data)
		h.Write([]byte{'\n'})
	}
	field("recipe", []byte(in.RecipeVersion))
	field("image", []byte(in.ImageID))
	field("resolved", in.Resolved)
	for i, d := range in.ComponentDirs {
		if err := hashTree(h, "component"+strconv.Itoa(i), d); err != nil {
			return "", err
		}
	}
	for i, f := range in.IncludeFiles {
		if err := hashTree(h, "include"+strconv.Itoa(i), f); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashTree feeds the sorted relative paths and contents of every regular file
// under root (root itself may be a file) into w. Python bytecode caches are
// skipped: they appear whenever someone runs esphome locally and say nothing
// about the sources.
func hashTree(w io.Writer, label, root string) error {
	st, err := os.Stat(root)
	if err != nil {
		return apperr.Infra("build_failed", "cannot read a build input for the cache hash", map[string]any{"path": root, "error": err.Error()})
	}
	var rels []string
	if st.IsDir() {
		if err = collectRels(root, "", 0, &rels); err != nil {
			return apperr.Infra("build_failed", "cannot walk a build input for the cache hash", map[string]any{"path": root, "error": err.Error()})
		}
		sort.Strings(rels)
	} else {
		rels = []string{""}
	}
	fmt.Fprintf(w, "tree %s %d\n", label, len(rels))
	for _, rel := range rels {
		p := root
		if rel != "" {
			p = filepath.Join(root, filepath.FromSlash(rel))
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return apperr.Infra("build_failed", "cannot read a build input for the cache hash", map[string]any{"path": p, "error": err.Error()})
		}
		fmt.Fprintf(w, "file %s %d\n", rel, len(raw))
		if _, err := w.Write(raw); err != nil {
			return err
		}
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	return nil
}

// maxTreeDepth bounds the directory nesting collectRels follows. Symlinks to
// directories are followed (like the compiler does), so a link cycle would
// otherwise recurse forever.
const maxTreeDepth = 64

// collectRels appends to rels the slash-separated path (relative to the hashed
// root, with prefix) of every regular file under dir. Unlike filepath.WalkDir,
// which reports a symlinked directory as a non-directory and never descends
// into it, this follows symlinks for the root and for every sub-directory, so
// a component tree reached through a link is hashed by content.
func collectRels(dir, prefix string, depth int, rels *[]string) error {
	if depth > maxTreeDepth {
		return fmt.Errorf("directory nesting or symlink loop deeper than %d levels at %s", maxTreeDepth, dir)
	}
	entries, err := os.ReadDir(dir) // follows a symlinked dir itself
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		if !e.IsDir() && strings.HasSuffix(name, ".pyc") {
			continue
		}
		info, err := os.Stat(p) // follows symlinks, like the compiler
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if name == "__pycache__" {
				continue
			}
			if err := collectRels(p, prefix+name+"/", depth+1, rels); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			*rels = append(*rels, prefix+name)
		}
	}
	return nil
}

// localInputs extracts, from the resolved config, the local external
// component source directories and the esphome.includes / includes_c paths,
// made absolute against yamlDir when relative. Remote sources cannot be
// hashed by content; their url and ref are part of the resolved text.
func localInputs(resolved []byte, yamlDir string) (componentDirs, includeFiles []string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(trimPreamble(resolved), &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, nil
	}
	root := doc.Content[0]
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(yamlDir, p)
	}
	if ec := mapGet(root, "external_components"); ec != nil && ec.Kind == yaml.SequenceNode {
		for _, item := range ec.Content {
			src := mapGet(item, "source")
			if scalarOf(mapGet(src, "type")) == "local" {
				if p := scalarOf(mapGet(src, "path")); p != "" {
					componentDirs = append(componentDirs, abs(p))
				}
			}
		}
	}
	esphome := mapGet(root, "esphome")
	for _, key := range []string{"includes", "includes_c"} {
		if list := mapGet(esphome, key); list != nil && list.Kind == yaml.SequenceNode {
			for _, item := range list.Content {
				if p := scalarOf(item); p != "" {
					includeFiles = append(includeFiles, abs(p))
				}
			}
		}
	}
	return componentDirs, includeFiles
}

// BuildRecord is <CacheDir>/build/sil-build.json: the hash of the inputs of
// the last successful build and the program it produced.
type BuildRecord struct {
	Schema  int    `json:"schema"`
	Hash    string `json:"hash"`
	Program string `json:"program"`
}

// Hit reports whether the record describes a finished build for hash whose
// program still exists.
func (r BuildRecord) Hit(hash string) bool {
	if r.Schema != recordSchema || r.Hash == "" || r.Hash != hash || r.Program == "" {
		return false
	}
	st, err := os.Stat(r.Program)
	return err == nil && st.Mode().IsRegular()
}

// ReadRecord reads sil-build.json from buildDir; ok is false when it is
// missing or unreadable.
func ReadRecord(buildDir string) (BuildRecord, bool) {
	raw, err := os.ReadFile(filepath.Join(buildDir, recordName))
	if err != nil {
		return BuildRecord{}, false
	}
	var r BuildRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return BuildRecord{}, false
	}
	return r, true
}

// WriteRecord writes sil-build.json into buildDir atomically.
func WriteRecord(buildDir string, r BuildRecord) error {
	r.Schema = recordSchema
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(buildDir, ".sil-build-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(raw, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write build record: %v %v", werr, cerr)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(buildDir, recordName)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// removeRecord deletes sil-build.json (a missing file is fine).
func removeRecord(buildDir string) {
	_ = os.Remove(filepath.Join(buildDir, recordName))
}

// toolcacheDir is the shared library archive cache, derived from the
// instance cache dir (<XDG_CACHE_HOME>/sil/build/<name>), never from the
// process environment, so callers that point CacheDir at a per-worktree cache
// root never share a toolcache with another worktree.
func toolcacheDir(cacheDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(cacheDir)), "toolcache")
}

// httpCacheRel is PlatformIO's HTTP cache below the container's /cache
// (PLATFORMIO_CACHE_DIR=/cache/platformio/cache, namespace http). Every
// compile that needs a registry library resolves it through this cache, and
// PlatformIO only keeps an entry valid for one hour; without a frozen copy a
// "seeded" compile still needs the network, and fails offline after an hour.
const (
	httpCacheRel  = "platformio/cache/http"
	httpCacheTool = "pio_http"
	// frozenExpiry (2100-01-01 UTC) replaces the expiry of every cached
	// registry response in the toolcache copy. The cache is keyed by request
	// and its db.data lines are "<expiry unix seconds>=<file>" in the pinned
	// image's PlatformIO; the image is pinned by digest, so the format cannot
	// drift under us. Freezing also pins the resolved library versions.
	frozenExpiry = "4102444800"
)

// toolcacheReady reports whether the toolcache holds both parts of the seed.
func toolcacheReady(toolcache string) bool {
	for _, d := range []string{pioComponentsDir, httpCacheTool} {
		if st, err := os.Stat(filepath.Join(toolcache, d)); err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// seedInstance installs the toolcache into an instance before a build: the
// library archives (pio_components, only when the instance has none) and the
// frozen registry responses (always replaced, they are tiny). It reports
// whether the toolcache holds a complete seed; when it does not, nothing is
// copied and the compile needs network.
func seedInstance(toolcache, dataDir, cacheDir string) (seeded bool, err error) {
	if !toolcacheReady(toolcache) {
		return false, nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return true, err
	}
	if _, err := os.Stat(filepath.Join(dataDir, pioComponentsDir)); err != nil {
		if err := installTree(filepath.Join(toolcache, pioComponentsDir), filepath.Join(dataDir, pioComponentsDir)); err != nil {
			return true, err
		}
	}
	dst := filepath.Join(cacheDir, httpCacheRel)
	if err := os.RemoveAll(dst); err != nil {
		return true, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return true, err
	}
	return true, installTree(filepath.Join(toolcache, httpCacheTool), dst)
}

// installTree copies src to dst through a temp sibling and a rename.
func installTree(src, dst string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".seed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	staged := filepath.Join(tmp, "tree")
	if err := copyTree(src, staged); err != nil {
		return err
	}
	return os.Rename(staged, dst)
}

// writeBackToolcache copies what a successful online build fetched into the
// toolcache, each part only when it is still missing, through a temp dir and
// a rename (so concurrent builds are safe: the loser's rename fails and its
// temp dir is removed).
func writeBackToolcache(toolcache, dataDir, cacheDir string) error {
	parts := []struct {
		src, name string
		freeze    bool
	}{
		{filepath.Join(dataDir, pioComponentsDir), pioComponentsDir, false},
		{filepath.Join(cacheDir, httpCacheRel), httpCacheTool, true},
	}
	for _, p := range parts {
		if st, err := os.Stat(p.src); err != nil || !st.IsDir() {
			continue
		}
		dst := filepath.Join(toolcache, p.name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(toolcache, 0o700); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(toolcache, ".write-")
		if err != nil {
			return err
		}
		staged := filepath.Join(tmp, p.name)
		err = copyTree(p.src, staged)
		if err == nil && p.freeze {
			err = freezeHTTPCache(staged)
		}
		if err == nil {
			if err = os.Rename(staged, dst); err != nil {
				if _, serr := os.Stat(dst); serr == nil {
					err = nil // another build won the race
				}
			}
		}
		os.RemoveAll(tmp)
		if err != nil {
			return err
		}
	}
	return nil
}

// freezeHTTPCache rewrites db.data of a PlatformIO HTTP cache so that every
// entry whose response file exists expires in 2100.
func freezeHTTPCache(dir string) error {
	db := filepath.Join(dir, "db.data")
	raw, err := os.ReadFile(db)
	if err != nil {
		return err
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		_, file, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || file == "" || strings.ContainsAny(file, "/\\") {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, file)); err == nil && st.Mode().IsRegular() {
			out = append(out, frozenExpiry+"="+file)
		}
	}
	return os.WriteFile(db, []byte(strings.Join(out, "\n")+"\n"), 0o600)
}

// copyTree copies directories, regular files and symlinks from src to dst,
// keeping the permission bits. dst must not exist.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700|info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600|info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

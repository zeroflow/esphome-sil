package ui

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zeroflow/esphome-sil/internal/control"
)

// TestEmbedContents: the page files every test and the server rely on are in the embedded tree.
func TestEmbedContents(t *testing.T) {
	for _, name := range []string{"index.html", "app.css", "js/app.js", "js/store.js", "js/view.js", "js/api.js", "js/inputs.js", "js/message.js"} {
		b, err := fs.ReadFile(FS(), name)
		if err != nil {
			t.Errorf("%s is not embedded: %v", name, err)
		} else if len(b) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

// firstPartyFiles walks fsys without descending into vendor/ (vendored libraries are third-party
// code: they are pinned by sha256 and covered behaviourally by the offline browser tests).
func firstPartyFiles(fsys fs.FS, visit func(name string, lines []string)) error {
	return fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		visit(name, strings.Split(string(b), "\n"))
		return nil
	})
}

// externalRefs lists file:line of every http:// or https:// in the first-party files.
func externalRefs(fsys fs.FS) ([]string, error) {
	var out []string
	err := firstPartyFiles(fsys, func(name string, lines []string) {
		for i, l := range lines {
			if strings.Contains(l, "http://") || strings.Contains(l, "https://") {
				out = append(out, fmt.Sprintf("%s:%d: %s", name, i+1, strings.TrimSpace(l)))
			}
		}
	})
	return out, err
}

// bannedDOM are the HTML-string DOM APIs and the code-from-string calls the page never uses: the
// page builds its DOM from the template and sets text through textContent (Research Pitfall 5).
var bannedDOM = []string{
	"innerHTML", "outerHTML", "insertAdjacentHTML", "createContextualFragment", "document.write",
	"eval(", "new Function", "setTimeout(\"", "setTimeout('", "setInterval(\"", "setInterval('",
}

// unsafeDOM lists file:line of every banned token in the page's JavaScript.
func unsafeDOM(fsys fs.FS) ([]string, error) {
	var out []string
	err := firstPartyFiles(fsys, func(name string, lines []string) {
		if path.Ext(name) != ".js" {
			return
		}
		for i, l := range lines {
			for _, tok := range bannedDOM {
				if strings.Contains(l, tok) {
					out = append(out, fmt.Sprintf("%s:%d: %q in %s", name, i+1, tok, strings.TrimSpace(l)))
				}
			}
		}
	})
	return out, err
}

// TestNoExternalRefs (UI-01): no first-party file names an external URL. The scan itself is checked
// against a tree that has one, so it cannot pass by finding nothing.
func TestNoExternalRefs(t *testing.T) {
	got, err := externalRefs(FS())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got {
		t.Errorf("external reference: %s", l)
	}

	bad := fstest.MapFS{
		"index.html":      {Data: []byte("<p>\n<script src=\"https://cdn.example/x.js\"></script>")},
		"js/a.js":         {Data: []byte("fetch('http://example.com')")},
		"vendor/lib.js":   {Data: []byte("// https://github.com/vendored/lib")},
		"js/vendor/in.js": {Data: []byte("// http://vendored.example")},
		"css/ok.css":      {Data: []byte("a { color: red }")},
	}
	found, err := externalRefs(bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || !strings.HasPrefix(found[0], "index.html:2:") || !strings.HasPrefix(found[1], "js/a.js:1:") {
		t.Errorf("self-check: scan found %v, want index.html:2 and js/a.js:1 and nothing under vendor/", found)
	}
}

// TestNoUnsafeDOM: the page's JavaScript uses no HTML-string DOM API and no code-from-string call.
func TestNoUnsafeDOM(t *testing.T) {
	got, err := unsafeDOM(FS())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got {
		t.Errorf("unsafe DOM use: %s", l)
	}

	bad := fstest.MapFS{
		"js/a.js":        {Data: []byte("el.innerHTML = x;\nok();\neval('1')")},
		"js/b.js":        {Data: []byte("el.insertAdjacentHTML('beforeend', x); new Function('return 1')")},
		"js/vendor/v.js": {Data: []byte("el.innerHTML = vendored")},
		"index.html":     {Data: []byte("innerHTML in prose")},
	}
	found, err := unsafeDOM(bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 4 {
		t.Errorf("self-check: scan found %v, want 4 hits in js/a.js and js/b.js only", found)
	}
}

// opPairRE matches a ["METHOD", "/path"] pair of the page's operation table.
var opPairRE = regexp.MustCompile(`\[\s*"(GET|POST|PUT|PATCH|DELETE)"\s*,\s*"(/[^"]*)"\s*\]`)

// paramRE matches a path parameter such as {id}.
var paramRE = regexp.MustCompile(`\{[^}]*\}`)

// unmatchedOps lists the pairs of src that are not a LAN row of control.Operations, and how many
// pairs src holds.
func unmatchedOps(src string) (bad []string, n int) {
	for _, m := range opPairRE.FindAllStringSubmatch(src, -1) {
		n++
		op, ok := control.MatchOperation(m[1], paramRE.ReplaceAllString(m[2], "sample"))
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("%s %s is not a row of control.Operations", m[1], m[2]))
		case !op.LAN:
			bad = append(bad, fmt.Sprintf("%s %s is row %s, which the LAN does not serve", m[1], m[2], op.Name))
		}
	}
	return bad, n
}

// TestJSOpsInOperations (UI-06): every [method, path] pair of the OPS table in js/api.js is a
// control.Operations row with LAN true, so the page cannot call an operation the CLI does not have.
// The scan is checked against a table with a stray pair, so it cannot pass by finding nothing.
func TestJSOpsInOperations(t *testing.T) {
	src, err := fs.ReadFile(FS(), "js/api.js")
	if err != nil {
		t.Fatal(err)
	}
	bad, n := unmatchedOps(string(src))
	if n == 0 {
		t.Fatal("no [method, path] pair found in js/api.js")
	}
	for _, b := range bad {
		t.Error(b)
	}

	bad, n = unmatchedOps(`const OPS = { a: ["GET", "/api/v1/fans"], b: ["POST", "/api/v1/down"], c: ["GET", "/api/v1/nowhere"], d: ["POST", "/api/v1/buttons/{id}/press"] };`)
	if n != 4 || len(bad) != 2 {
		t.Errorf("self-check: %d pairs, problems %v, want 4 pairs and the two stray ones (down is not LAN, nowhere is no row)", n, bad)
	}
}

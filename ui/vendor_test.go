package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// vendorRow is one row of the VENDOR.md file table.
type vendorRow struct {
	file   string
	bytes  int
	sha256 string
}

// vendorRowRE matches a table row `| file | bytes | sha256 |` whose second cell is a number and
// whose third is 64 hex digits (header and separator rows do not match).
var vendorRowRE = regexp.MustCompile(`^\|\s*([^|\s]+)\s*\|\s*(\d+)\s*\|\s*([0-9a-f]{64})\s*\|\s*$`)

// parseVendorTable returns the file rows of a VENDOR.md text.
func parseVendorTable(md string) []vendorRow {
	var rows []vendorRow
	for _, l := range strings.Split(md, "\n") {
		m := vendorRowRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		rows = append(rows, vendorRow{file: m[1], bytes: n, sha256: m[3]})
	}
	return rows
}

// vendorProblems re-hashes every table row from fsys/vendor and lists every difference: a missing
// file, a wrong size or hash, a file in vendor/ the table does not list, an empty table.
func vendorProblems(fsys fs.FS) ([]string, error) {
	md, err := fs.ReadFile(fsys, "vendor/VENDOR.md")
	if err != nil {
		return nil, err
	}
	rows := parseVendorTable(string(md))
	var out []string
	if len(rows) == 0 {
		out = append(out, "VENDOR.md lists no file")
	}
	listed := map[string]bool{"VENDOR.md": true}
	for _, r := range rows {
		listed[r.file] = true
		b, err := fs.ReadFile(fsys, "vendor/"+r.file)
		if err != nil {
			out = append(out, r.file+" is listed but not embedded: "+err.Error())
			continue
		}
		sum := sha256.Sum256(b)
		if len(b) != r.bytes {
			out = append(out, r.file+": "+strconv.Itoa(len(b))+" bytes, VENDOR.md says "+strconv.Itoa(r.bytes))
		}
		if got := hex.EncodeToString(sum[:]); got != r.sha256 {
			out = append(out, r.file+": sha256 "+got+", VENDOR.md says "+r.sha256)
		}
	}
	entries, err := fs.ReadDir(fsys, "vendor")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			out = append(out, e.Name()+" is in vendor/ but not in the VENDOR.md table")
		}
	}
	return out, nil
}

// TestVendorHashes (UI-01): the vendored files are the files that were checked at vendoring time.
// The check is run against altered trees as well, so it cannot pass by finding nothing.
func TestVendorHashes(t *testing.T) {
	got, err := vendorProblems(FS())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		t.Error(p)
	}
	if rows, _ := fs.ReadFile(FS(), "vendor/VENDOR.md"); len(parseVendorTable(string(rows))) < 3 {
		t.Errorf("VENDOR.md table has fewer than the 3 vendored files")
	}

	hash := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	table := func(rows ...string) string {
		return "| file | bytes | sha256 |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n"
	}
	row := func(name, content string) string {
		return "| " + name + " | " + strconv.Itoa(len(content)) + " | " + hash(content) + " |"
	}
	mk := func(files map[string]string) fs.FS {
		m := fstest.MapFS{}
		for n, c := range files {
			m[n] = &fstest.MapFile{Data: []byte(c)}
		}
		return m
	}

	cases := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"good", map[string]string{"vendor/VENDOR.md": table(row("a.js", "abc")), "vendor/a.js": "abc"}, 0},
		{"changed content", map[string]string{"vendor/VENDOR.md": table(row("a.js", "abc")), "vendor/a.js": "abd"}, 1},
		{"changed size", map[string]string{"vendor/VENDOR.md": table(row("a.js", "abc")), "vendor/a.js": "abcd"}, 2},
		{"unlisted file", map[string]string{"vendor/VENDOR.md": table(row("a.js", "abc")), "vendor/a.js": "abc", "vendor/b.js": "x"}, 1},
		{"missing file", map[string]string{"vendor/VENDOR.md": table(row("a.js", "abc"))}, 1},
		{"empty table", map[string]string{"vendor/VENDOR.md": "no table here\n", "vendor/a.js": "abc"}, 2},
	}
	for _, c := range cases {
		p, err := vendorProblems(mk(c.files))
		if err != nil {
			t.Fatalf("self-check %s: %v", c.name, err)
		}
		if len(p) != c.want {
			t.Errorf("self-check %s: %d problems %v, want %d", c.name, len(p), p, c.want)
		}
	}
}

// TestVendorServed (UI-01): the vendored files are served from the embedded tree, byte-identical,
// with a JavaScript and a CSS content type, so the page never needs a CDN.
func TestVendorServed(t *testing.T) {
	srv := httptest.NewServer(http.FileServerFS(FS()))
	defer srv.Close()

	for _, c := range []struct{ name, typeWant string }{
		{"uPlot.iife.min.js", "javascript"},
		{"uPlot.min.css", "text/css"},
	} {
		want, err := fs.ReadFile(FS(), "vendor/"+c.name)
		if err != nil {
			t.Fatalf("%s is not embedded: %v", c.name, err)
		}
		resp, err := http.Get(srv.URL + "/vendor/" + c.name)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /vendor/%s: status %d", c.name, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, c.typeWant) {
			t.Errorf("GET /vendor/%s: Content-Type %q, want it to contain %q", c.name, ct, c.typeWant)
		}
		if string(body) != string(want) {
			t.Errorf("GET /vendor/%s: %d bytes served, %d vendored, not identical", c.name, len(body), len(want))
		}
	}
}

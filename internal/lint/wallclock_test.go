// Package lint holds repository-wide source checks that run under plain
// `go test ./...`.
//
// The wall-clock lint enforces MODEL-04: only internal/clock may read wall
// time or arm a wall-clock timer. Everything else takes a clock.Clock, so time
// can later be scaled or driven manually. The check is pure AST (no type
// information), so it works on every file regardless of build tags.
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	useClock = "use the injected clock.Clock (Now, NewTicker, NewTimer, Sleep, Deadline)"
	useWith  = "use clock.WithTimeout(ctx, clk, d) or clk.Deadline(d)"
)

// bannedTime are the time package functions that read or wait on wall time.
var bannedTime = setOf("Now", "Since", "Until", "Sleep", "After", "AfterFunc", "Tick", "NewTimer", "NewTicker")

// bannedContext are the context constructors that arm a wall-clock timer.
var bannedContext = setOf("WithTimeout", "WithDeadline", "WithTimeoutCause", "WithDeadlineCause")

// bannedFuncs are package-level functions that arm a wall-clock timer,
// keyed by import path.
var bannedFuncs = map[string]map[string]bool{
	"net":      setOf("DialTimeout"),
	"net/http": setOf("TimeoutHandler"),
}

// bannedFields are the standard-library struct fields that arm a wall-clock
// timer, keyed by import path and then type name.
var bannedFields = map[string]map[string]map[string]bool{
	"net/http": {
		"Client":    setOf("Timeout"),
		"Server":    setOf("ReadTimeout", "ReadHeaderTimeout", "WriteTimeout", "IdleTimeout"),
		"Transport": setOf("TLSHandshakeTimeout", "ResponseHeaderTimeout", "ExpectContinueTimeout", "IdleConnTimeout"),
	},
	"net":     {"Dialer": setOf("Timeout", "Deadline")},
	"os/exec": {"Cmd": setOf("WaitDelay")},
}

// fieldImports are the packages whose presence turns on the assignment check.
var fieldImports = setOf("net", "net/http", "os/exec")

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// allFieldNames is every banned field name, for the assignment check.
func allFieldNames() map[string]bool {
	m := map[string]bool{}
	for _, types := range bannedFields {
		for _, fields := range types {
			for f := range fields {
				m[f] = true
			}
		}
	}
	return m
}

// checkSource parses one file and returns its findings as "file:line: message",
// sorted by line.
func checkSource(filename string, src any) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	type finding struct {
		line int
		msg  string
	}
	var found []finding
	add := func(pos token.Pos, format string, args ...any) {
		found = append(found, finding{fset.Position(pos).Line, fmt.Sprintf(format, args...)})
	}

	// Local name -> import path for the packages of interest.
	names := map[string]string{}
	importsFieldPkg := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		watched := path == "time" || path == "context" || bannedFuncs[path] != nil || bannedFields[path] != nil
		if !watched {
			continue
		}
		if fieldImports[path] {
			importsFieldPkg = true
		}
		local := filepath.Base(path)
		if spec.Name != nil {
			switch spec.Name.Name {
			case "_":
				continue
			case ".":
				add(spec.Pos(), "dot import of %q hides wall-clock use from this lint; import it by name", path)
				continue
			default:
				local = spec.Name.Name
			}
		}
		names[local] = path
	}
	fieldNames := allFieldNames()

	// pkgOf reports the import path an identifier refers to; an identifier the
	// parser resolved to a local declaration (Obj != nil) shadows the import.
	pkgOf := func(e ast.Expr) string {
		id, ok := e.(*ast.Ident)
		if !ok || id.Obj != nil {
			return ""
		}
		return names[id.Name]
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			pkg := pkgOf(n.X)
			sel := n.Sel.Name
			switch {
			case pkg == "time" && bannedTime[sel]:
				add(n.Pos(), "time.%s reads or waits on the wall clock; %s", sel, useClock)
			case pkg == "context" && bannedContext[sel]:
				add(n.Pos(), "context.%s arms a wall-clock timer; %s", sel, useWith)
			case pkg != "" && bannedFuncs[pkg][sel]:
				add(n.Pos(), "%s.%s arms a wall-clock timer; %s", filepath.Base(pkg), sel, useWith)
			}
		case *ast.CompositeLit:
			sel, ok := n.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg := pkgOf(sel.X)
			banned := bannedFields[pkg][sel.Sel.Name]
			if banned == nil {
				return true
			}
			for _, el := range n.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && banned[key.Name] {
					add(kv.Pos(), "%s.%s field %s arms a wall-clock timer; %s", filepath.Base(pkg), sel.Sel.Name, key.Name, useWith)
				}
			}
		case *ast.AssignStmt:
			if !importsFieldPkg {
				return true
			}
			for _, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && fieldNames[sel.Sel.Name] {
					add(sel.Pos(), "assignment to .%s arms a wall-clock timer (no type information: this file imports net, net/http or os/exec and the repo's own structs must not use this field name); %s", sel.Sel.Name, useWith)
				}
			}
		}
		return true
	})

	sort.SliceStable(found, func(i, j int) bool { return found[i].line < found[j].line })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = fmt.Sprintf("%s:%d: %s", filename, f.line, f.msg)
	}
	return out, nil
}

// repoRoot walks up from the test directory to the directory holding go.mod.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the test directory")
		}
		dir = parent
	}
}

// TestNoWallClock scans every .go file in the repository, whatever its build
// tags, and fails on any wall-clock use outside internal/clock.
func TestNoWallClock(t *testing.T) {
	root := repoRoot(t)
	scanned := 0
	sawE2E := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "prototype":
				return fs.SkipDir
			}
			if rel == "internal/clock" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		if strings.HasPrefix(rel, "e2e/") {
			sawE2E = true
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		findings, err := checkSource(rel, src)
		if err != nil {
			t.Errorf("%s: cannot parse: %v", rel, err)
			return nil
		}
		for _, f := range findings {
			t.Error(f)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if scanned == 0 || !sawE2E {
		t.Fatalf("scan looked at %d files (e2e seen: %v); the walk is broken", scanned, sawE2E)
	}
}

type fixture struct {
	name string
	src  string // every line that must be reported ends in "// want"
}

var fixtures = []fixture{
	{"aliased time import and method values", `package x

import tm "time"

func f() {
	_ = tm.Now() // want
	g := tm.Sleep // want
	_ = g
	<-tm.After(tm.Second) // want
	_ = tm.NewTicker // want
	var d tm.Duration = tm.Second
	_ = d
}
`},
	{"every banned time function by default name", `package x

import "time"

func f() {
	_ = time.Now() // want
	_ = time.Since(time.Time{}) // want
	_ = time.Until(time.Time{}) // want
	time.Sleep(1) // want
	<-time.After(1) // want
	time.AfterFunc(1, nil) // want
	_ = time.Tick(1) // want
	_ = time.NewTimer(1) // want
	_ = time.NewTicker(1) // want
}
`},
	{"dot import of time", `package x

import . "time" // want

func f() { _ = Now() }
`},
	{"context timers: alias, method value, all four", `package x

import cx "context"

func f(ctx cx.Context, d int) {
	cx.WithTimeout(ctx, d) // want
	_ = cx.WithDeadline // want
	cx.WithTimeoutCause(ctx, d, nil) // want
	cx.WithDeadlineCause(ctx, d, nil) // want
	cx.WithCancel(ctx)
}
`},
	{"dot import of context", `package x

import . "context" // want

func f(ctx Context) { WithTimeout(ctx, 1) }
`},
	{"standard-library timeout fields in composite literals", `package x

import (
	"net"
	"net/http"
	"os/exec"
)

func f(d int) {
	_ = &http.Client{Timeout: d} // want
	_ = http.Server{ReadHeaderTimeout: d} // want
	_ = http.Server{Addr: "x", ReadTimeout: d, WriteTimeout: d, IdleTimeout: d} // want
	_ = &http.Transport{TLSHandshakeTimeout: d} // want
	_ = &http.Transport{ResponseHeaderTimeout: d, ExpectContinueTimeout: d, IdleConnTimeout: d} // want
	_ = net.Dialer{Timeout: d} // want
	_ = net.Dialer{Deadline: d} // want
	_ = exec.Cmd{WaitDelay: d} // want
}
`},
	{"timeout fields set by assignment", `package x

import (
	"net/http"
	"os/exec"
)

func f(srv *http.Server, cmd *exec.Cmd, d int) {
	srv.IdleTimeout = d // want
	cmd.WaitDelay = d // want
}
`},
	{"net.DialTimeout and http.TimeoutHandler", `package x

import (
	n "net"
	"net/http"
)

func f(h http.Handler, d int) {
	_, _ = n.DialTimeout("tcp", "x", d) // want
	_ = http.TimeoutHandler(h, d, "") // want
	dial := n.DialTimeout // want
	_ = dial
}
`},
	{"clean code is not reported", `package x

import (
	"context"
	"net"
	"net/http"
	"time"

	"example.com/clock"
)

type T struct{ Timeout int }

func f(ctx context.Context, clk clock.Clock, conn net.Conn, h http.Handler, d time.Duration) {
	ctx, cancel := clock.WithTimeout(ctx, clk, d)
	defer cancel()
	_ = conn.SetDeadline(clk.Deadline(d))
	_ = &http.Server{Handler: h}
	_ = &http.Client{}
	_ = net.Dialer{}
	_, _ = context.WithCancel(ctx)
	var _ time.Duration = 3 * time.Second
	_ = time.Millisecond
}

func shadow(time T) {
	_ = time.Now
}
`},
	{"assignment check is off without net, net/http or os/exec", `package x

type T struct{ Timeout, WaitDelay int }

func f(t *T) {
	t.Timeout = 1
	t.WaitDelay = 2
}
`},
}

// TestLintSelfTest proves the checker fires with file:line on inline fixture
// sources and stays quiet on clean code.
func TestLintSelfTest(t *testing.T) {
	const fname = "fixture.go"
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			want := map[int]bool{}
			for i, line := range strings.Split(fx.src, "\n") {
				if strings.Contains(line, "// want") {
					want[i+1] = true
				}
			}
			findings, err := checkSource(fname, fx.src)
			if err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}
			got := map[int]bool{}
			for _, f := range findings {
				var line int
				rest, ok := strings.CutPrefix(f, fname+":")
				if !ok {
					t.Fatalf("finding %q does not start with %s:", f, fname)
				}
				numText, _, _ := strings.Cut(rest, ":")
				if line, err = strconv.Atoi(numText); err != nil {
					t.Fatalf("finding %q has no line number", f)
				}
				got[line] = true
			}
			for line := range want {
				if !got[line] {
					t.Errorf("line %d not reported", line)
				}
			}
			for line := range got {
				if !want[line] {
					t.Errorf("line %d reported but is clean: %v", line, findings)
				}
			}
			if len(want) > 0 && len(findings) < len(want) {
				t.Errorf("%d findings for %d expected lines: %v", len(findings), len(want), findings)
			}
		})
	}
}

// checkGlobalRand reports (stub) the use of the global source of math/rand or math/rand/v2.
func checkGlobalRand(filename string, src any) ([]string, error) { return nil, nil }

var randFixtures = []fixture{
	{"global source functions, aliased and as values", `package x

import (
	"math/rand"
	r2 "math/rand/v2"
)

func f() {
	_ = rand.Intn(5) // want
	_ = r2.Float64() // want
	rand.Seed(1) // want
	g := rand.Int63 // want
	_ = g
	_ = r2.N(10) // want
	rand.Shuffle(3, func(i, j int) {}) // want
}
`},
	{"local generators and types are allowed", `package x

import (
	"math/rand"
	r2 "math/rand/v2"
)

func f(src rand.Source, z *rand.Zipf, g *r2.Rand) {
	r := rand.New(rand.NewSource(1))
	_ = r.Intn(5)
	p := r2.New(r2.NewPCG(1, 2))
	_ = p.Float64()
	_ = r2.New(r2.NewChaCha8([32]byte{}))
	_ = rand.NewZipf(r, 1.5, 2, 10)
	var _ r2.PCG
	_ = src
	_ = z
	_ = g
}
`},
	{"a local identifier named rand is not the package", `package x

type gen struct{}

func (gen) Intn(int) int { return 0 }

func f() {
	rand := gen{}
	_ = rand.Intn(5)
}
`},
}

// TestGlobalRandSelfTest proves checkGlobalRand fires on the global source with file:line, accepts
// local generators and types, and is not fooled by a shadowing identifier.
func TestGlobalRandSelfTest(t *testing.T) {
	const fname = "fixture.go"
	for _, fx := range randFixtures {
		t.Run(fx.name, func(t *testing.T) {
			want := map[int]bool{}
			for i, line := range strings.Split(fx.src, "\n") {
				if strings.Contains(line, "// want") {
					want[i+1] = true
				}
			}
			findings, err := checkGlobalRand(fname, fx.src)
			if err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}
			got := map[int]bool{}
			for _, f := range findings {
				rest, ok := strings.CutPrefix(f, fname+":")
				if !ok {
					t.Fatalf("finding %q does not start with %s:", f, fname)
				}
				numText, msg, _ := strings.Cut(rest, ":")
				line, err := strconv.Atoi(numText)
				if err != nil {
					t.Fatalf("finding %q has no line number", f)
				}
				if !strings.Contains(msg, "MODEL-10") {
					t.Errorf("finding %q does not name MODEL-10", f)
				}
				got[line] = true
			}
			for line := range want {
				if !got[line] {
					t.Errorf("line %d not reported", line)
				}
			}
			for line := range got {
				if !want[line] {
					t.Errorf("line %d reported but is clean: %v", line, findings)
				}
			}
		})
	}
}

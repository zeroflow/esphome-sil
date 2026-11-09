package apperr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const apperrImportPath = "github.com/zeroflow/esphome-sil/internal/apperr"

// site is one literal-code call of Failed or Infra.
type site struct {
	code string
	pos  token.Position
}

// scanSites parses the production Go files under internal/ and cmd/ (testdata and _test.go files
// skipped) and returns every Failed/Infra call with a string-literal code, per constructor. A call
// whose code is not a string literal is returned in dynamic, with its call site.
func scanSites(t *testing.T) (failed, infra []site, dynamic []string) {
	t.Helper()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			local := ""
			if f.Name.Name == "apperr" {
				local = "." // inside the package the constructors are bare identifiers
			}
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p != apperrImportPath {
					continue
				}
				local = "apperr"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
			if local == "" {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					if x, ok := fn.X.(*ast.Ident); ok && x.Name == local {
						name = fn.Sel.Name
					}
				case *ast.Ident:
					if local == "." {
						name = fn.Name
					}
				}
				if name != "Failed" && name != "Infra" {
					return true
				}
				pos := fset.Position(call.Pos())
				if len(call.Args) == 0 {
					dynamic = append(dynamic, pos.String()+": no arguments")
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					dynamic = append(dynamic, pos.String()+": code is not a string literal")
					return true
				}
				code, _ := strconv.Unquote(lit.Value)
				s := site{code: code, pos: pos}
				if name == "Failed" {
					failed = append(failed, s)
				} else {
					infra = append(infra, s)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", top, err)
		}
	}
	return failed, infra, dynamic
}

func TestRegistryMatchesCallSites(t *testing.T) {
	failed, infra, dynamic := scanSites(t)
	if len(failed) == 0 || len(infra) == 0 {
		t.Fatalf("scan found %d Failed and %d Infra call sites; the scan is broken", len(failed), len(infra))
	}
	for _, d := range dynamic {
		t.Errorf("%s: every apperr.Failed/Infra code must be a string literal so the registry can be checked", d)
	}

	used := map[string]int{}
	check := func(sites []site, want int, ctor string) {
		for _, s := range sites {
			got, ok := Registry[s.code]
			switch {
			case !ok:
				t.Errorf("%s: apperr.%s(%q) is not in apperr.Registry", s.pos, ctor, s.code)
			case got != want:
				t.Errorf("%s: apperr.%s(%q) is exit %d but the registry says %d", s.pos, ctor, s.code, want, got)
			}
			if prev, seen := used[s.code]; seen && prev != want {
				t.Errorf("%s: code %q is used with both Failed and Infra", s.pos, s.code)
			}
			used[s.code] = want
		}
	}
	check(failed, ExitFailed, "Failed")
	check(infra, ExitInfra, "Infra")

	var stale []string
	for code := range Registry {
		if _, ok := used[code]; !ok {
			stale = append(stale, code)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("registry codes never used in internal/ or cmd/: %v", stale)
	}
}

func TestRegistryClasses(t *testing.T) {
	if ExitFailed != 1 || ExitInfra != 2 {
		t.Fatalf("exit classes are 1 and 2, got %d and %d", ExitFailed, ExitInfra)
	}
	for code, class := range Registry {
		if class != ExitFailed && class != ExitInfra {
			t.Errorf("registry code %q has class %d; only 1 and 2 are exit classes (0 is success)", code, class)
		}
		if code == "" || strings.ToLower(code) != code || strings.ContainsAny(code, " -") {
			t.Errorf("registry code %q is not snake_case", code)
		}
	}
}

// Every constructor yields the class the registry records, and ExitCode maps nil to 0 and foreign errors to 2.
func TestExitCodeFollowsRegistry(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
	for code, class := range Registry {
		var e *Error
		if class == ExitFailed {
			e = Failed(code, "m", nil)
		} else {
			e = Infra(code, "m", nil)
		}
		if got := ExitCode(e); got != class {
			t.Errorf("ExitCode(%s) = %d, want %d", code, got, class)
		}
	}
	if got := ExitCode(AsError(errString("boom"))); got != ExitInfra {
		t.Errorf("a foreign error exits %d, want %d", got, ExitInfra)
	}
	if got := AsError(errString("boom")); Registry[got.Code] != ExitInfra {
		t.Errorf("foreign errors become %q which the registry must list as exit 2", got.Code)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

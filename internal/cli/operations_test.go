package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/zeroflow/esphome-sil/internal/control"
)

// TestOperationsCLI proves the CLI field of every row of control.Operations is a working `sil`
// invocation: it parses with the grammar Execute uses and selects the command its leading words
// name. Nothing runs and no instance is resolved.
func TestOperationsCLI(t *testing.T) {
	for _, op := range control.Operations {
		t.Run(op.Name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli,
				kong.Name("sil"),
				kong.Writers(io.Discard, io.Discard),
				kong.Exit(func(code int) { panic(exitSignal(code)) }),
			)
			if err != nil {
				t.Fatal(err)
			}
			var kctx *kong.Context
			if code, exited := catchExit(func() { kctx, err = parser.Parse(strings.Fields(op.CLI)) }); exited {
				t.Fatalf("`sil %s` made the parser exit with %d", op.CLI, code)
			}
			if err != nil {
				t.Fatalf("`sil %s` does not parse: %v", op.CLI, err)
			}
			var path []string
			for _, w := range strings.Fields(kctx.Command()) {
				if !strings.HasPrefix(w, "<") {
					path = append(path, w)
				}
			}
			words := strings.Fields(op.CLI)
			if len(path) == 0 || len(path) > len(words) || strings.Join(words[:len(path)], " ") != strings.Join(path, " ") {
				t.Fatalf("`sil %s` selects command %q, which its leading words do not name", op.CLI, strings.Join(path, " "))
			}
		})
	}
}

package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeroflow/esphome-sil/internal/fanlib"
)

// TestMain points the fan library at the byte-for-byte v0.1.0 fixtures (D-02). The exact-rpm tests of this
// package compare against v0.1.0 numbers; the shipped schema 2 library is covered by internal/fanlib and
// e2e/library_v2_test.go. Tests that load testdata/fans-v2 fixtures keep overriding it with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fans-v0.1.0"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervisor tests: %v\n", err)
		os.Exit(2)
	}
	if err := os.Setenv(fanlib.EnvFanDir, dir); err != nil {
		fmt.Fprintf(os.Stderr, "supervisor tests: %v\n", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

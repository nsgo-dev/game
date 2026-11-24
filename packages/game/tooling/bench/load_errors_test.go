package bench_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/tooling/bench"
)

func TestLoadFileErrorNamesFileLineAndFix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.yaml")
	text := "version: 1\nmax_regression_pct: {ns_per_op: 25, bytes_per_op: 25, allocs_per_op: 25}\nextra: 1\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := bench.LoadFile(path)
	loaderrtest.Expect(t, err, path, 3)

	missing := filepath.Join(t.TempDir(), "none.yaml")
	_, err = bench.LoadFile(missing)
	loaderrtest.Expect(t, err, missing, 0)
}

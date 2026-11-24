package bench_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/tooling/bench"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*bench.Baseline]{
		Decode: bench.Decode,
		Encode: (*bench.Baseline).Encode,
		Files:  []string{"testdata/baseline.yaml"},
	})
}

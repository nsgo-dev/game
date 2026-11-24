package stats_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/stats"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*stats.File]{
		Decode: stats.Parse,
		Encode: stats.Encode,
		Files:  []string{"testdata/*.yaml"},
	})
}

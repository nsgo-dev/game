package quality_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/quality"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[quality.Config]{
		Decode: quality.ParseConfig,
		Encode: quality.EncodeConfig,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "quality",
	})
}

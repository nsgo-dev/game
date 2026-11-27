package quality_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/quality"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[quality.Config]{
		Decode: quality.ParseConfig,
		Encode: quality.EncodeConfig,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "quality",
	})
}

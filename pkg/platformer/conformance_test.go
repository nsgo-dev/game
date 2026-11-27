package platformer_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/platformer"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[platformer.Config]{
		Decode: platformer.ParseConfig,
		Encode: platformer.EncodeConfig,
		Files:  []string{"testdata/hero.yaml"},
		Doc:    "platformer",
	})
}

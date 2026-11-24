package bullets_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*bullets.File]{
		Decode: bullets.Parse,
		Encode: bullets.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "bullets",
	})
}

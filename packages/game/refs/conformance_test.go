package refs_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/refs"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*refs.Index]{
		Decode: refs.ParseIndex,
		Encode: refs.EncodeIndex,
		Files:  []string{"testdata/game.index.yaml"},
	})
}

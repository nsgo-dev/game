package refs_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*refs.Index]{
		Decode: refs.ParseIndex,
		Encode: refs.EncodeIndex,
		Files:  []string{"testdata/game.index.yaml"},
		Doc:    "tooling/refs",
	})
}

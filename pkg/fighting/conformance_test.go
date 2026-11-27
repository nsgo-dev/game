package fighting_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/fighting"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*fighting.MoveSet]{
		Decode: fighting.Parse,
		Encode: fighting.Encode,
		Files:  []string{"testdata/moves.yaml"},
		Doc:    "fighting",
	})
}

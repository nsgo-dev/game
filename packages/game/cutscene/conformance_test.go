package cutscene_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[cutscene.Cutscene]{
		Decode: cutscene.Parse,
		Encode: cutscene.Encode,
		Files:  []string{"testdata/intro.yaml"},
		Doc:    "cutscene",
	})
}

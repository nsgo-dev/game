package cutscene_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/cutscene"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[cutscene.Cutscene]{
		Decode:     cutscene.Parse,
		Encode:     cutscene.Encode,
		Files:      []string{"testdata/intro.yaml", "testdata/floor1_intro.cutscene.yaml"},
		Doc:        "cutscene",
		MaxVersion: cutscene.CurrentVersion,
	})
}

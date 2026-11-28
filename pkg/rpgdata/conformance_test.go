package rpgdata_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/rpgdata"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*rpgdata.Database]{
		Decode: rpgdata.Parse,
		Encode: rpgdata.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "rpgdata",
	})
}

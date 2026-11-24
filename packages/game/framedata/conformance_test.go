package framedata_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/framedata"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*framedata.Doc]{
		Decode: framedata.Parse,
		Encode: framedata.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "framedata",
	})
}

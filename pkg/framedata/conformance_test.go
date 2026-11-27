package framedata_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/framedata"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*framedata.Doc]{
		Decode: framedata.Parse,
		Encode: framedata.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "framedata",
	})
}

package input_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/input"
)

func TestTouchLayoutFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[input.TouchLayout]{
		Decode: input.ParseTouchLayout,
		Encode: input.EncodeTouchLayout,
		Files:  []string{"testdata/*.touch.yaml"},
		Doc:    "input#touch",
	})
}

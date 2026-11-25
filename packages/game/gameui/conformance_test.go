package gameui_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*gameui.ScreenDoc]{
		Decode: gameui.ParseScreen,
		Encode: gameui.EncodeScreen,
		Files:  []string{"testdata/*.screen.yaml"},
		Doc:    "gameui",
	})
}

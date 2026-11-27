package gameui_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/gameui"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*gameui.ScreenDoc]{
		Decode: gameui.ParseScreen,
		Encode: gameui.EncodeScreen,
		Files:  []string{"testdata/*.screen.yaml"},
		Doc:    "gameui",
	})
}

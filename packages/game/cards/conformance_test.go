package cards_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*cards.BoardDoc]{
		Decode: cards.ParseBoardDoc,
		Encode: cards.EncodeBoardDoc,
		Files:  []string{"testdata/board.yaml"},
	})
}

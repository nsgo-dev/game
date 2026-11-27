package cards_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/cards"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*cards.BoardDoc]{
		Decode: cards.ParseBoardDoc,
		Encode: cards.EncodeBoardDoc,
		Files:  []string{"testdata/board.yaml"},
		Doc:    "cards",
	})
}

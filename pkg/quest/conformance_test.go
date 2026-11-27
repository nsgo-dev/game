package quest_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/quest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*quest.Book]{
		Decode: quest.Parse,
		Encode: quest.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "quest",
	})
}

package leaderboard_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/leaderboard"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*leaderboard.Catalog]{
		Decode: leaderboard.Parse,
		Encode: (*leaderboard.Catalog).Encode,
		Files:  []string{"testdata/leaderboards.yaml"},
		Doc:    "online/leaderboard",
	})
}

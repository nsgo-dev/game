package selfhost_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[selfhost.Config]{
		Decode: selfhost.ParseConfig,
		Encode: selfhost.Config.Marshal,
		Files:  []string{"testdata/selfhost.yaml"},
		Doc:    "online/leaderboard/selfhost",
	})
}

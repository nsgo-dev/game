package cloudsave_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/cloudsave"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[cloudsave.Config]{
		Decode: cloudsave.ParseConfig,
		Encode: cloudsave.Config.Marshal,
		Files:  []string{"testdata/cloudsave.yaml"},
	})
}

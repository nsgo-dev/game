package cloudsave_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/cloudsave"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[cloudsave.Config]{
		Decode: cloudsave.ParseConfig,
		Encode: cloudsave.Config.Marshal,
		Files:  []string{"testdata/cloudsave.yaml"},
		Doc:    "cloudsave",
	})
}

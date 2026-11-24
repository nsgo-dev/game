package skeletal_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/skeletal"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*skeletal.SkeletonData]{
		Decode: skeletal.Load,
		Encode: skeletal.Encode,
		Files:  []string{"testdata/*.yaml"},
	})
}

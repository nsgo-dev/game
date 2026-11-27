package skeletal_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/skeletal"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*skeletal.SkeletonData]{
		Decode: skeletal.Load,
		Encode: skeletal.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "skeletal",
	})
}

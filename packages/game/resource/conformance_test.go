package resource_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/resource"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[resource.Resource]{
		Decode: resource.Parse,
		Encode: resource.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "resource",
	})
}

package inventory_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/inventory"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*inventory.Catalog]{
		Decode: inventory.Parse,
		Encode: (*inventory.Catalog).Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "inventory",
	})
}

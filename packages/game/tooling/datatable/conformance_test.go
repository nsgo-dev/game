package datatable_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[[]weapon]{
		Decode: func(data []byte) ([]weapon, error) { return datatable.LoadYAML[weapon]("weapons.yaml", data) },
		Encode: datatable.MarshalYAML[weapon],
		Files:  []string{"testdata/weapons.yaml"},
	})
}

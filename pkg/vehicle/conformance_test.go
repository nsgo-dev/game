package vehicle_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/vehicle"
)

func TestFormatConformance(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		formattest.Run(t, formattest.Format[vehicle.Config]{
			Decode: vehicle.ParseConfig,
			Encode: vehicle.EncodeConfig,
			Files:  []string{"testdata/reference_car.yaml"},
			Doc:    "vehicle",
		})
	})
	t.Run("ghost", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*vehicle.Ghost]{
			Decode: vehicle.ParseGhost,
			Encode: vehicle.EncodeGhost,
			Files:  []string{"testdata/*.ghost.yaml"},
			Doc:    "vehicle#ghost",
		})
	})
}

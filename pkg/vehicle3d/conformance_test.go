package vehicle3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

func TestFormatConformance(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		formattest.Run(t, formattest.Format[vehicle3d.Config]{
			Decode: vehicle3d.ParseConfig,
			Encode: vehicle3d.EncodeConfig,
			Files:  []string{"testdata/reference_car.yaml"},
			Doc:    "vehicle3d",
		})
	})
	t.Run("ghost", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*vehicle3d.Ghost]{
			Decode: vehicle3d.ParseGhost,
			Encode: vehicle3d.EncodeGhost,
			Files:  []string{"testdata/*.ghost.yaml"},
			Doc:    "vehicle3d#ghost",
		})
	})
}

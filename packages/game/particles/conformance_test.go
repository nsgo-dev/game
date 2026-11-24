package particles_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/particles"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[particles.Config]{
		Decode: particles.UnmarshalConfigYAML,
		Encode: particles.MarshalConfigYAML,
		Files:  []string{"testdata/sparks.yaml"},
		Doc:    "particles",
	})
}

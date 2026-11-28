package particles_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/particles"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[particles.Config]{
		Decode: particles.UnmarshalConfigYAML,
		Encode: particles.MarshalConfigYAML,
		Files:  []string{"testdata/sparks.yaml"},
		Doc:    "particles",
	})
}

func TestEffectFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[particles.Effect]{
		Decode: particles.UnmarshalEffectYAML,
		Encode: particles.MarshalEffectYAML,
		Files:  []string{"testdata/*.effect.yaml"},
		Doc:    "particles#effect",
	})
}

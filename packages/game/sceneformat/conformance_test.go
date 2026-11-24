package sceneformat_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*sceneformat.SceneDoc]{
		Decode:     sceneformat.Parse,
		Encode:     sceneformat.Encode,
		Files:      []string{"testdata/*.scene.yaml", "testdata/project/*.scene.yaml", "testdata/project/scenes/*.scene.yaml"},
		MaxVersion: 2,
	})
}

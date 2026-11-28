package sceneformat_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// TestFormatConformance checks the scene format on the fixtures that are already in the bytes Encode writes. testdata/cave_room.scene.yaml keeps its hand-written quoting, zero values and key order for the parser tests and fuzz seeds, so its canonical form is checked from testdata/canonical instead.
func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*sceneformat.SceneDoc]{
		Decode:     sceneformat.Parse,
		Encode:     sceneformat.Encode,
		Files:      []string{"testdata/fleet.scene.yaml", "testdata/hall.scene.yaml", "testdata/parallax.scene.yaml", "testdata/lighting3d.scene.yaml", "testdata/timer.scene.yaml", "testdata/behaviors.scene.yaml", "testdata/canonical/*.scene.yaml", "testdata/project/*.scene.yaml", "testdata/project/scenes/*.scene.yaml"},
		Doc:        "sceneformat",
		MaxVersion: 4,
	})
}

// TestConflictFormatConformance checks the scene merge conflict YAML format against the shared format conformance rules and docs/<lang>/formats.md examples.
func TestConflictFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*sceneformat.ConflictDoc]{
		Decode:     sceneformat.ParseConflict,
		Encode:     sceneformat.EncodeConflict,
		Files:      []string{"testdata/canonical/*.conflict.yaml"},
		Doc:        "sceneformat#conflict",
		MaxVersion: sceneformat.ConflictVersion,
	})
}

package worldmap_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/worldmap"
)

// TestFormatConformance checks the map definition format. A map's State is stored as JSON in a save.Store slot, not as a YAML file, so it is not checked here.
func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*worldmap.Doc]{
		Decode: worldmap.Parse,
		Encode: worldmap.Encode,
		Files:  []string{"testdata/castle.yaml"},
		Doc:    "worldmap",
	})
}

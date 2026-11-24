package worldmap_test

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/worldmap"
)

func TestFormatConformance(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*worldmap.Doc]{
			Decode: worldmap.Parse,
			Encode: worldmap.Encode,
			Files:  []string{"testdata/castle.yaml"},
		})
	})
	t.Run("state", func(t *testing.T) {
		def, err := os.ReadFile("testdata/castle.yaml")
		if err != nil {
			t.Fatal(err)
		}
		// A state is loaded by decoding it and restoring it into the map it belongs to, which checks its version and rooms, and saved by encoding what the map reports.
		formattest.Run(t, formattest.Format[worldmap.State]{
			Decode: func(data []byte) (worldmap.State, error) {
				var s worldmap.State
				if err := yaml.Unmarshal(data, &s); err != nil {
					return worldmap.State{}, err
				}
				m, err := worldmap.Load(def)
				if err != nil {
					return worldmap.State{}, err
				}
				if err := m.Restore(s); err != nil {
					return worldmap.State{}, err
				}
				return m.State(), nil
			},
			Encode: func(s worldmap.State) ([]byte, error) { return yaml.Marshal(s) },
			Files:  []string{"testdata/castle.state.yaml"},
		})
	})
}

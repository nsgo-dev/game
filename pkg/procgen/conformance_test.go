package procgen_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/procgen"
)

// TestFormatConformance checks the rules, room templates and placement formats on testdata/canonical, which holds fixtures in the bytes EncodeRules and EncodeRoomTemplates write; testdata/ruins.yaml keeps its hand-written comment and flow style for the parser tests and fuzz seeds.
func TestFormatConformance(t *testing.T) {
	t.Run("rules", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*procgen.Rules]{
			Decode: procgen.ParseRules,
			Encode: procgen.EncodeRules,
			Files:  []string{"testdata/canonical/ruins.yaml"},
			Doc:    "procgen",
		})
	})
	t.Run("rooms", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*procgen.RoomTemplates]{
			Decode: procgen.ParseRoomTemplates,
			Encode: procgen.EncodeRoomTemplates,
			Files:  []string{"testdata/canonical/rooms.yaml"},
			Doc:    "procgen#rooms",
		})
	})
	t.Run("placement", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*procgen.Placement]{
			Decode: procgen.ParsePlacement,
			Encode: procgen.EncodePlacement,
			Files:  []string{"testdata/canonical/forest.placement.yaml"},
			Doc:    "procgen#placement",
		})
	})
}

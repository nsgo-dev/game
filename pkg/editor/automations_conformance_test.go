package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestAutomationsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.AutomationsFile]{
		Doc:    "editor#automations",
		Decode: editor.ParseAutomations,
		Encode: editor.EncodeAutomations,
		Files:  []string{"testdata/automations.yaml"},
	})
}

package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestEditorSettingsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.EditorSettings]{
		Doc:    "editor#settings",
		Decode: editor.ParseEditorSettings,
		Encode: editor.EncodeEditorSettings,
		Files:  []string{"testdata/settings.yaml"},
	})
}

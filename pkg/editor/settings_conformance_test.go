package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestEditorSettingsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.EditorSettings]{
		Doc:    "editor#settings",
		Decode: editor.ParseEditorSettings,
		Encode: editor.EncodeEditorSettings,
		Files:  []string{"testdata/settings.yaml"},
	})
}

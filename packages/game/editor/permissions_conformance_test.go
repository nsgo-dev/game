package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestPermissionsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.PermissionsFile]{
		Doc:    "editor#permissions",
		Decode: editor.ParsePermissions,
		Encode: editor.EncodePermissions,
		Files:  []string{"testdata/permissions.yaml"},
	})
}

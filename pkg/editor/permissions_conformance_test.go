package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestPermissionsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.PermissionsFile]{
		Doc:    "editor#permissions",
		Decode: editor.ParsePermissions,
		Encode: editor.EncodePermissions,
		Files:  []string{"testdata/permissions.yaml"},
	})
}

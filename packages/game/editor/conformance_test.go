package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestRecentProjectsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.RecentProjectsFile]{
		Decode: editor.ParseRecentProjects,
		Encode: editor.EncodeRecentProjects,
		Files:  []string{"testdata/recent_projects.yaml"},
		Doc:    "editor#recent",
	})
}

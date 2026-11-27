package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/editor"
)

func TestRecentProjectsFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.RecentProjectsFile]{
		Decode: editor.ParseRecentProjects,
		Encode: editor.EncodeRecentProjects,
		Files:  []string{"testdata/recent_projects.yaml"},
		Doc:    "editor#recent",
	})
}

func TestCheckpointManifestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.CheckpointManifest]{
		Decode: editor.ParseCheckpointManifest,
		Encode: editor.EncodeCheckpointManifest,
		Files:  []string{"testdata/checkpoint_manifest.yaml"},
		Doc:    "editor#checkpoint",
	})
}

func TestLayoutFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.LayoutFile]{
		Decode: editor.ParseLayout,
		Encode: editor.EncodeLayout,
		Files:  []string{"testdata/layout.yaml"},
		Doc:    "editor#layout",
	})
}

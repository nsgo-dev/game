package main

import (
	"embed"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

//go:embed scenes/town.scene.yaml
var townScene []byte

func TestTownSceneIsEditorReadable(t *testing.T) {
	doc, err := sceneformat.Parse(townScene)
	if err != nil {
		t.Fatalf("Parse town scene: %v", err)
	}
	if doc.Name != "Hollowbrook" || len(doc.Layers) != 1 {
		t.Fatalf("scene = %+v", doc)
	}
}

var _ embed.FS

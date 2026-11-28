package main

import (
	"embed"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

//go:embed scenes/level1.scene.yaml
var levelScene []byte

func TestLevelSceneIsEditorReadable(t *testing.T) {
	doc, err := sceneformat.Parse(levelScene)
	if err != nil {
		t.Fatalf("Parse level scene: %v", err)
	}
	if doc.Name != "Lantern Hills" || len(doc.Layers) != 2 {
		t.Fatalf("scene = %+v", doc)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("Encode: %v", err)
	}
}

var _ embed.FS

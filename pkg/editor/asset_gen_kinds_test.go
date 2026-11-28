package editor

import (
	"github.com/nsgo-dev/game/pkg/importsettings"
	"testing"
)

func TestAssetGeneratorKindsAndSourceMetadataRoundTrip(t *testing.T) {
	for _, kind := range []AssetGeneratorKind{AssetGenImages, AssetGenSound, AssetGenTextures, AssetGenModels, AssetGenAnimation, AssetGenTiles} {
		if !kind.Valid() {
			t.Fatalf("kind %q rejected", kind)
		}
	}
	b, err := importsettings.EncodeAssetSource(&importsettings.AssetSource{Version: importsettings.AssetSourceVersion, Kind: "models", Files: []string{"art/hero.glb", "art/hero.material.yaml"}, Generator: "gen", Prompt: "hero", License: "CC0-1.0"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := importsettings.ParseAssetSource(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "models" || len(got.Files) != 2 {
		t.Fatalf("metadata = %+v", got)
	}
}

package editor

import "testing"

func TestAssetGeneratorKindsIncludeMediaAndStructuredAssets(t *testing.T) {
	for _, kind := range []AssetGeneratorKind{AssetGenImages, AssetGenSound, AssetGenTexture, AssetGenModel, AssetGenAnimation, AssetGenTileset} {
		if !kind.Valid() {
			t.Fatalf("%q is not valid", kind)
		}
	}
	for path, want := range map[string]AssetGeneratorKind{"art/hero.png": AssetGenTexture, "models/hero.gltf": AssetGenModel, "anim/run.animation.yaml": AssetGenAnimation} {
		if got := inferAssetGenKind(path, ""); got != want {
			t.Fatalf("inferAssetGenKind(%q) = %q, want %q", path, got, want)
		}
	}
}

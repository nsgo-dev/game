package editor

import "testing"

func TestComposeAssetPromptDeterministicAndFramed(t *testing.T) {
	brief := AssetStyleBrief{Summary: "watercolor, warm light, bold silhouettes"}
	want := "slime\n\nwatercolor, warm light, bold silhouettes\n\ntransparent background, full subject with comfortable margin"
	if got := ComposeAssetPrompt("slime", brief, AssetGenImages); got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
	if got := ComposeAssetPrompt("slime", brief, AssetGenImages); got != want {
		t.Fatalf("second prompt = %q", got)
	}
}

func TestAssetReferenceHash(t *testing.T) {
	if got := AssetReferenceHash([]byte("reference")); got != "52367a6622b19f08825e915fad80c542ad4f4c34dbcebad9f5007994b3e39208" {
		t.Fatalf("hash = %s", got)
	}
}

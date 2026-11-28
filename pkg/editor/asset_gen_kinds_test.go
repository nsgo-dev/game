package editor_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/importsettings"
)

func TestAssetGeneratorKindsValidAndGeneratable(t *testing.T) {
	for _, tc := range []struct {
		kind        editor.AssetGeneratorKind
		generatable bool
	}{
		{editor.AssetGenImages, true},
		{editor.AssetGenSound, true},
		{editor.AssetGenTextures, false},
		{editor.AssetGenModels, false},
		{editor.AssetGenAnimation, false},
		{editor.AssetGenTiles, false},
	} {
		if !tc.kind.Valid() {
			t.Errorf("kind %q rejected by Valid", tc.kind)
		}
		if got := tc.kind.Generatable(); got != tc.generatable {
			t.Errorf("kind %q Generatable = %v, want %v", tc.kind, got, tc.generatable)
		}
	}
	if editor.AssetGeneratorKind("meshes").Valid() {
		t.Fatal(`unknown kind "meshes" accepted by Valid`)
	}
}

// A kind without an in-memory validator must be refused before the untrusted generator is called, so no unvalidated payload ever reaches a candidate.
func TestAssetGeneratorRefusesKindWithoutValidator(t *testing.T) {
	ed, dir := newAssetGenTestProject(t)
	defer ed.DisconnectAllAssetGenerators()
	beforeFiles := listAllFiles(t, dir)

	transport, calls, mu := startFixtureImageGenerator(t, "CC0-1.0", true, nil)
	if err := ed.ConnectAssetGenerator(context.Background(), editor.GeneratorServerConfig{Name: "mesh-gen", Kind: editor.AssetGenModels}, transport); err != nil {
		t.Fatalf("ConnectAssetGenerator(models): %v", err)
	}
	_, err := ed.GenerateAsset(context.Background(), editor.GenerateAssetParams{
		TargetPath: "art/props/crate.glb",
		Generator:  "mesh-gen",
		Prompt:     "wooden crate",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("GenerateAsset(models) error = %v, want a not-supported refusal", err)
	}
	mu.Lock()
	n := len(*calls)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("generator was called %d times for a kind without a validator", n)
	}
	if after := listAllFiles(t, dir); !slices.Equal(beforeFiles, after) {
		t.Fatalf("refused generation wrote files: %v", after)
	}
}

func TestAssetSourceKindAndFilesRoundTrip(t *testing.T) {
	in := &importsettings.AssetSource{
		Version:   importsettings.AssetSourceVersion,
		Kind:      "models",
		Files:     []string{"art/hero.glb", "art/hero.material.yaml"},
		Generator: "gen",
		Prompt:    "hero",
		License:   "CC0-1.0",
	}
	b, err := importsettings.EncodeAssetSource(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importsettings.ParseAssetSource(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "models" || !slices.Equal(got.Files, in.Files) {
		t.Fatalf("metadata = %+v", got)
	}
	again, err := importsettings.EncodeAssetSource(got)
	if err != nil || !bytes.Equal(b, again) {
		t.Fatalf("source YAML not canonical:\ngot:\n%s\nwant:\n%s", again, b)
	}

	for _, bad := range []*importsettings.AssetSource{
		{Version: 1, Kind: "models", Files: []string{""}, Generator: "gen", Prompt: "hero"},
		{Version: 1, Kind: "models", Files: []string{" art/hero.glb"}, Generator: "gen", Prompt: "hero"},
		{Version: 1, Kind: "two\nlines", Generator: "gen", Prompt: "hero"},
	} {
		if err := importsettings.ValidateAssetSource(bad); err == nil {
			t.Errorf("ValidateAssetSource(%+v) accepted invalid kind or files", bad)
		}
	}
}

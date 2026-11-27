package assetbuild_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
)

func TestBuildRemoteHashesAndManifest(t *testing.T) {
	root := t.TempDir()
	musicDir := filepath.Join(root, "assets", "music")
	spritesDir := filepath.Join(root, "assets", "sprites")
	if err := os.MkdirAll(musicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(spritesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	themeV1 := []byte("theme-audio-v1-bytes")
	heroBytes := []byte("hero-sprite-bytes")
	if err := os.WriteFile(filepath.Join(musicDir, "theme.m4a"), themeV1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spritesDir, "hero.png"), heroBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// Dotfiles, .go files and import sidecars must be skipped.
	if err := os.WriteFile(filepath.Join(root, "assets", ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "manifest_gen.go"), []byte("package assets"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(musicDir, "theme.m4a.import.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &project.Assets{
		Manifest: &project.ManifestTarget{
			In:  "assets",
			Out: "assets/manifest_gen.go",
		},
		Remote: &project.RemoteTarget{
			Out:     "dist/cdn",
			Base:    "https://cdn.example.com/game/",
			Include: []string{"music/"},
		},
	}

	b := &assetbuild.Builder{Root: root, Version: "test"}
	rep, err := b.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	if rep.Count(assetbuild.Built) != 2 {
		t.Fatalf("expected manifest + remote built, got %+v", rep.Targets)
	}

	manifestPath := filepath.Join(root, "dist", "cdn", assets.RemoteManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read remote.yaml: %v", err)
	}
	rm, err := assets.ParseRemoteManifest(data)
	if err != nil {
		t.Fatalf("ParseRemoteManifest: %v", err)
	}
	if rm.Base != "https://cdn.example.com/game/" {
		t.Fatalf("Base = %q", rm.Base)
	}
	if len(rm.Files) != 1 {
		t.Fatalf("Include filter should emit 1 file, got %+v", rm.Files)
	}
	e := rm.Files[0]
	if e.URI != "asset:music/theme.m4a" || e.Size != int64(len(themeV1)) {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if wantPath := assets.HashedPath("music/theme.m4a", e.SHA256); e.Path != wantPath {
		t.Fatalf("entry.Path = %q, want %q", e.Path, wantPath)
	}
	hashedV1 := filepath.Join(root, "dist", "cdn", filepath.FromSlash(e.Path))
	if got, err := os.ReadFile(hashedV1); err != nil || string(got) != string(themeV1) {
		t.Fatalf("hashed file read mismatch: err=%v got=%q", err, got)
	}

	// Second build with nothing changed must be up to date.
	rep2, err := b.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if rep2.Count(assetbuild.Built) != 0 || rep2.Count(assetbuild.UpToDate) != 2 {
		t.Fatalf("expected all up to date, got %+v", rep2.Targets)
	}

	// Check mode passes when up to date.
	bCheck := &assetbuild.Builder{Root: root, Version: "test", Check: true}
	repCheck, err := bCheck.Build(context.Background(), cfg)
	if err != nil || repCheck.Count(assetbuild.Stale) != 0 {
		t.Fatalf("check up-to-date failed: err=%v targets=%+v", err, repCheck.Targets)
	}

	// Edit theme.m4a -> check reports stale, rebuild writes new hash and deletes old hashed file!
	themeV2 := []byte("theme-audio-v2-updated-bytes")
	if err := os.WriteFile(filepath.Join(musicDir, "theme.m4a"), themeV2, 0o644); err != nil {
		t.Fatal(err)
	}
	repCheck2, err := bCheck.Build(context.Background(), cfg)
	if err != nil || repCheck2.Count(assetbuild.Stale) != 1 {
		t.Fatalf("check after edit should mark remote stale: err=%v targets=%+v", err, repCheck2.Targets)
	}

	rep3, err := b.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("rebuild after edit: %v", err)
	}
	if rep3.Count(assetbuild.Built) != 1 {
		t.Fatalf("expected only remote rebuilt, got %+v", rep3.Targets)
	}
	if _, err := os.Stat(hashedV1); !os.IsNotExist(err) {
		t.Fatalf("stale hashed file %s should have been removed, err=%v", hashedV1, err)
	}
	data2, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	rm2, err := assets.ParseRemoteManifest(data2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rm2.Files) != 1 || rm2.Files[0].SHA256 == e.SHA256 || rm2.Files[0].Size != int64(len(themeV2)) {
		t.Fatalf("manifest not updated for v2: %+v", rm2.Files)
	}
}

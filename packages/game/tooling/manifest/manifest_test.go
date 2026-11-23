package manifest_test

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/manifest"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanDeterminismClassificationAndCollisions(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"hero.png",
		"hero.jpg",
		"jump.wav",
		"music/theme.m4a",
		"maps/level_1.tmj",
		"data/sprites.json",
		"ui/2x/play-btn.png",
		"a-b.txt",
		"a_b.txt",
		".DS_Store",
		".hidden/secret.png",
	}
	for _, f := range files {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x")
	}

	entries, err := manifest.Scan(dir, manifest.DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// 9 visible files; dotfiles and dotdirs are skipped.
	if len(entries) != 9 {
		t.Fatalf("got %d entries, want 9: %+v", len(entries), entries)
	}

	byPath := make(map[string]manifest.Entry, len(entries))
	for i, e := range entries {
		if i > 0 && entries[i-1].Path >= e.Path {
			t.Errorf("entries not sorted: %q >= %q", entries[i-1].Path, e.Path)
		}
		if !token.IsIdentifier(e.Ident) || token.IsKeyword(e.Ident) {
			t.Errorf("invalid Go identifier %q for %q", e.Ident, e.Path)
		}
		byPath[e.Path] = e
	}

	wants := map[string]struct {
		ident string
		kind  manifest.Kind
		uri   string
	}{
		"hero.jpg":           {"ImageHeroJpg", manifest.KindImage, "asset:hero.jpg"},
		"hero.png":           {"ImageHeroPng", manifest.KindImage, "asset:hero.png"},
		"jump.wav":           {"SoundJump", manifest.KindAudio, "asset:jump.wav"},
		"music/theme.m4a":    {"SoundMusicTheme", manifest.KindAudio, "asset:music/theme.m4a"},
		"maps/level_1.tmj":   {"MapMapsLevel1", manifest.KindMap, "asset:maps/level_1.tmj"},
		"data/sprites.json":  {"AssetDataSprites", manifest.KindOther, "asset:data/sprites.json"},
		"ui/2x/play-btn.png": {"ImageUi2xPlayBtn", manifest.KindImage, "asset:ui/2x/play-btn.png"},
		"a-b.txt":            {"AssetABTxt", manifest.KindOther, "asset:a-b.txt"},
		"a_b.txt":            {"AssetABTxt2", manifest.KindOther, "asset:a_b.txt"},
	}
	for p, want := range wants {
		got, ok := byPath[p]
		if !ok {
			t.Fatalf("missing entry %q", p)
		}
		if got.Ident != want.ident || got.Kind != want.kind || got.URI != want.uri {
			t.Errorf("entry %q = %+v, want ident=%q kind=%v uri=%q", p, got, want.ident, want.kind, want.uri)
		}
	}
}

func TestGenerateParsesAndIsFormatted(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		opts  manifest.Options
	}{
		{
			name:  "empty",
			files: nil,
			opts:  manifest.DefaultOptions(),
		},
		{
			name:  "image only",
			files: []string{"hero.png"},
			opts:  manifest.DefaultOptions(),
		},
		{
			name:  "audio only",
			files: []string{"jump.wav"},
			opts:  manifest.DefaultOptions(),
		},
		{
			name:  "data only",
			files: []string{"level1.tmj", "atlas.json"},
			opts:  manifest.DefaultOptions(),
		},
		{
			name:  "mixed custom pkg",
			files: []string{"hero.png", "jump.wav", "level1.tmj", "atlas.json"},
			opts:  manifest.Options{Package: "gameassets", Prefix: "asset:"},
		},
		{
			name:  "embed mode",
			files: []string{"hero.png", "jump.wav", "level1.tmj", "atlas.json"},
			opts:  manifest.Options{Package: "assets", Embed: true, EmbedDir: "files"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				writeTestFile(t, filepath.Join(dir, filepath.FromSlash(f)), "data")
			}
			src, err := manifest.Generate(dir, tc.opts)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "manifest_gen.go", src, parser.ParseComments)
			if err != nil {
				t.Fatalf("parser.ParseFile failed: %v\n%s", err, src)
			}
			wantPkg := tc.opts.Package
			if wantPkg == "" {
				wantPkg = "assets"
			}
			if file.Name.Name != wantPkg {
				t.Errorf("package = %q, want %q", file.Name.Name, wantPkg)
			}
			reformatted, err := format.Source(src)
			if err != nil {
				t.Fatalf("format.Source: %v", err)
			}
			if !bytes.Equal(src, reformatted) {
				t.Errorf("generated output is not gofmt-formatted:\ngot:\n%s\nwant:\n%s", src, reformatted)
			}
		})
	}
}

func TestGeneratedManifestCompilesAndFailsWhenAssetRemoved(t *testing.T) {
	goMod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read packages/game/go.mod: %v", err)
	}
	goSum, err := os.ReadFile(filepath.Join("..", "..", "go.sum"))
	if err != nil {
		t.Fatalf("read packages/game/go.sum: %v", err)
	}

	prepareModule := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		var lines []string
		inToolBlock := false
		for _, line := range strings.Split(string(goMod), "\n") {
			trimmed := strings.TrimSpace(line)
			// The tool directives, single lines or a block, can name packages of the module being renamed, so they are left out.
			switch {
			case inToolBlock:
				inToolBlock = trimmed != ")"
				continue
			case trimmed == "tool (":
				inToolBlock = true
				continue
			case strings.HasPrefix(trimmed, "tool "):
				continue
			case strings.HasPrefix(trimmed, "module "):
				lines = append(lines, "module example.com/manifestcheck")
				continue
			}
			lines = append(lines, line)
		}
		writeTestFile(t, filepath.Join(root, "go.mod"), strings.Join(lines, "\n"))
		writeTestFile(t, filepath.Join(root, "go.sum"), string(goSum))
		return root
	}

	goBuild := func(dir string) ([]byte, error) {
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = dir
		cmd.Env = append(slices.Clone(os.Environ()), "GOWORK=off", "GOPROXY=off")
		return cmd.CombinedOutput()
	}

	t.Run("uri mode compile check and removal failure", func(t *testing.T) {
		root := prepareModule(t)
		assetsDir := filepath.Join(root, "assets")
		writeTestFile(t, filepath.Join(assetsDir, "hero.png"), "png-bytes")
		writeTestFile(t, filepath.Join(assetsDir, "jump.wav"), "wav-bytes")
		writeTestFile(t, filepath.Join(assetsDir, "level1.tmj"), `{"type":"map"}`)
		writeTestFile(t, filepath.Join(assetsDir, "sprites.json"), `{"frames":{}}`)

		outFile := filepath.Join(root, "genassets", "manifest_gen.go")
		opts := manifest.DefaultOptions()
		opts.Package = "genassets"
		if err := manifest.WriteFile(assetsDir, outFile, opts); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		consumer := `package main

import (
	"example.com/manifestcheck/genassets"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	_ core.ImageSource = genassets.ImageHero
	_ audio.Source     = genassets.SoundJump
	_ string           = genassets.MapLevel1
	_ string           = genassets.AssetSprites
	_ []string         = genassets.All
)

func main() {}
`
		writeTestFile(t, filepath.Join(root, "main.go"), consumer)

		if out, err := goBuild(root); err != nil {
			t.Fatalf("go build failed with all assets present: %v\n%s", err, out)
		}

		// Remove jump.wav and regenerate the manifest: consumer must fail to compile.
		if err := os.Remove(filepath.Join(assetsDir, "jump.wav")); err != nil {
			t.Fatal(err)
		}
		if err := manifest.WriteFile(assetsDir, outFile, opts); err != nil {
			t.Fatalf("regenerate WriteFile: %v", err)
		}
		out, err := goBuild(root)
		if err == nil {
			t.Fatalf("expected go build to fail after removing jump.wav, but it succeeded")
		}
		if !strings.Contains(string(out), "SoundJump") {
			t.Fatalf("expected compile error mentioning SoundJump, got:\n%s", out)
		}
	})

	t.Run("embed mode compile check and removal failure", func(t *testing.T) {
		root := prepareModule(t)
		assetsDir := filepath.Join(root, "assets")
		writeTestFile(t, filepath.Join(assetsDir, "hero.png"), "png-bytes")
		writeTestFile(t, filepath.Join(assetsDir, "jump.wav"), "wav-bytes")
		writeTestFile(t, filepath.Join(assetsDir, "level1.tmj"), `{"type":"map"}`)

		outFile := filepath.Join(assetsDir, "manifest_gen.go")
		opts := manifest.Options{Package: "assets", Embed: true}
		if err := manifest.WriteFile(assetsDir, outFile, opts); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		// Regenerating inside the same directory must ignore manifest_gen.go and remain byte-identical.
		first, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatal(err)
		}
		second, err := manifest.Generate(assetsDir, opts)
		if err != nil {
			t.Fatalf("second Generate: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("regenerating inside assets dir was not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
		}

		consumer := `package main

import (
	"example.com/manifestcheck/assets"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	_ core.ImageSource = assets.ImageHero
	_ audio.Source     = assets.SoundJump
	_ []byte           = assets.MapLevel1
	_ []string         = assets.All
)

func main() {}
`
		writeTestFile(t, filepath.Join(root, "main.go"), consumer)

		if out, err := goBuild(root); err != nil {
			t.Fatalf("go build failed in embed mode: %v\n%s", err, out)
		}

		// Removing an embedded asset fails the build immediately.
		if err := os.Remove(filepath.Join(assetsDir, "hero.png")); err != nil {
			t.Fatal(err)
		}
		if _, err := goBuild(root); err == nil {
			t.Fatalf("expected go build to fail after deleting embedded hero.png")
		}
	})
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "hero.png"), "png")
	writeTestFile(t, filepath.Join(dir, "sfx", "jump.wav"), "wav")

	if err := manifest.Verify(dir, []string{"asset:hero.png", "sfx/jump.wav"}); err != nil {
		t.Fatalf("Verify valid refs: %v", err)
	}
	if err := manifest.Verify(dir, nil); err != nil {
		t.Fatalf("Verify empty refs: %v", err)
	}

	err := manifest.Verify(dir, []string{"asset:hero.png", "asset:missing.png", "sfx/gone.wav"})
	if err == nil || !strings.Contains(err.Error(), "missing.png") || !strings.Contains(err.Error(), "gone.wav") {
		t.Fatalf("Verify missing refs err = %v, want both missing files reported", err)
	}

	for _, bad := range []string{"", "asset:", "../escape.png", "asset:../escape.png", "/etc/passwd", "sfx"} {
		if err := manifest.Verify(dir, []string{bad}); err == nil {
			t.Errorf("Verify(%q) succeeded, want error", bad)
		}
	}
}

func TestManifestErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := manifest.Generate(dir, manifest.Options{Package: "123bad"}); err == nil {
		t.Error("expected error for invalid package name")
	}
	if _, err := manifest.Generate(dir, manifest.Options{Package: "func"}); err == nil {
		t.Error("expected error for Go keyword package name")
	}
	if _, err := manifest.Generate(dir, manifest.Options{Embed: true, EmbedDir: "../outside"}); err == nil {
		t.Error("expected error for escaping EmbedDir")
	}
	if _, err := manifest.Scan(filepath.Join(dir, "nonexistent"), manifest.DefaultOptions()); err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

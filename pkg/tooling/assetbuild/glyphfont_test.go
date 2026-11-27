package assetbuild_test

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"github.com/nsgo-dev/nsgo/core"
)

// glyphFontProject declares a font target that bakes a folder of glyph images.
const glyphFontProject = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
assets:
  fonts:
    - in: art/fonts/score
      out: assets/fonts/score.fnt
      glyphs:
        plus.png: "+"
        times.png: "×"
      spacing: 2
`

const glyphFontID = "font assets/fonts/score.fnt"

// newGlyphFontProject writes glyphFontProject and its glyph images into a new directory and returns it. The digits are 8 pixels wide, "1" and "2" are 10 and 6 pixels tall, so the line height is 10 and "2" sits 4 pixels above the bottom.
func newGlyphFontProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "art/fonts/score/0.png"), 8, 8, color.NRGBA{R: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/1.png"), 8, 10, color.NRGBA{G: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/2.png"), 8, 6, color.NRGBA{B: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/3.png"), 8, 8, color.NRGBA{R: 255, G: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/4.png"), 8, 8, color.NRGBA{R: 255, B: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/plus.png"), 6, 8, color.NRGBA{G: 255, B: 255, A: 255})
	writePNG(t, filepath.Join(root, "art/fonts/score/times.png"), 6, 6, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(glyphFontProject))
	return root
}

func TestGlyphFolderBakesABitmapFont(t *testing.T) {
	root := newGlyphFontProject(t)
	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{glyphFontID: assetbuild.Built})
	if got := r.Targets[0].Outputs; strings.Join(got, " ") != "assets/fonts/score.fnt assets/fonts/score.png" {
		t.Fatalf("font outputs = %v", got)
	}
	f := loadFont(t, root, "assets/fonts/score.fnt")
	if f.LineHeight != 10 || f.Base != 10 {
		t.Fatalf("line height %v and base %v, want 10, the tallest glyph's height", f.LineHeight, f.Base)
	}
	if len(f.Glyphs) != 7 || len(f.Pages) != 1 {
		t.Fatalf("font has %d glyphs on %d pages, want 7 and 1", len(f.Glyphs), len(f.Pages))
	}
	// Each glyph's advance is its image's width plus spacing.
	for chars, want := range map[string]float32{"0": 10, "1": 10, "2": 10, "3": 10, "4": 10, "+": 8, "×": 8} {
		r := []rune(chars)[0]
		g, ok := f.Glyphs[r]
		if !ok {
			t.Fatalf("no glyph for %q", chars)
		}
		if g.XAdvance != want {
			t.Errorf("glyph %q advance = %v, want %v (width + spacing 2)", chars, g.XAdvance, want)
		}
	}
	// Bottom is the default align: the 6-pixel glyphs sit 4 pixels above the line's bottom, the 10-pixel one at the top.
	if g := f.Glyphs['2']; g.YOffset != 4 || g.XOffset != 0 {
		t.Errorf("glyph 2 offsets = (%v, %v), want (0, 4) with align bottom", g.XOffset, g.YOffset)
	}
	if g := f.Glyphs['×']; g.YOffset != 4 {
		t.Errorf("glyph × yoffset = %v, want 4", g.YOffset)
	}
	if got, want := f.Measure("1234", text.Options{}).Width, core.Points(40); got != want {
		t.Errorf("Measure(1234) width = %v, want %v", got, want)
	}
	placements := f.Layout("1234", 0, 0, text.Options{})
	if len(placements) != 4 {
		t.Fatalf("Layout(1234) = %d placements, want 4", len(placements))
	}
	if got := placements[1].X; got != 10 {
		t.Errorf("second glyph x = %v, want 10", got)
	}
	if got := placements[0].Y; got != 0 {
		t.Errorf("glyph 1 y = %v, want 0 (it is 10 pixels tall)", got)
	}
	if got := placements[1].Y; got != 4 {
		t.Errorf("glyph 2 y = %v, want 4 (it is 6 pixels tall)", got)
	}
	if got := placements[2].Y; got != 2 {
		t.Errorf("glyph 3 y = %v, want 2 (it is 8 pixels tall)", got)
	}

	// Top and center place the glyphs at the top and the middle of the line instead.
	editProject(t, root, "      spacing: 2\n", "      spacing: 2\n      align: top\n")
	build(t, root, assetbuild.Builder{})
	if g := loadFont(t, root, "assets/fonts/score.fnt").Glyphs['2']; g.YOffset != 0 {
		t.Errorf("align top: glyph 2 yoffset = %v, want 0", g.YOffset)
	}
	editProject(t, root, "      align: top\n", "      align: center\n")
	build(t, root, assetbuild.Builder{})
	if g := loadFont(t, root, "assets/fonts/score.fnt").Glyphs['2']; g.YOffset != 2 {
		t.Errorf("align center: glyph 2 yoffset = %v, want 2", g.YOffset)
	}
}

func TestGlyphFolderIsDeterministic(t *testing.T) {
	a, b := newGlyphFontProject(t), newGlyphFontProject(t)
	build(t, a, assetbuild.Builder{})
	build(t, b, assetbuild.Builder{})
	for _, rel := range []string{"assets/fonts/score.fnt", "assets/fonts/score.png"} {
		x, err := os.ReadFile(filepath.Join(a, rel))
		if err != nil {
			t.Fatal(err)
		}
		y, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(x, y) {
			t.Fatalf("%s differs between two builds of the same inputs", rel)
		}
	}
	// A check of a fresh checkout without the cache compares the outputs on disk with a build in a temporary folder.
	if err := os.RemoveAll(filepath.Join(a, ".game")); err != nil {
		t.Fatal(err)
	}
	wantStatuses(t, build(t, a, assetbuild.Builder{Check: true}), map[string]assetbuild.Status{glyphFontID: assetbuild.UpToDate})
}

func TestEditingOneGlyphRebuildsTheFont(t *testing.T) {
	root := newGlyphFontProject(t)
	build(t, root, assetbuild.Builder{})
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{glyphFontID: assetbuild.UpToDate})

	// Editing one glyph image rebakes the font.
	writePNG(t, filepath.Join(root, "art/fonts/score/2.png"), 12, 6, color.NRGBA{B: 255, A: 255})
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{glyphFontID: assetbuild.Built})
	if g := loadFont(t, root, "assets/fonts/score.fnt").Glyphs['2']; g.XAdvance != 14 {
		t.Fatalf("after widening 2: advance = %v, want 14 (12 + spacing 2)", g.XAdvance)
	}

	// The folder's sidecar may set padding, and removing it bakes the default again.
	writeFile(t, filepath.Join(root, "art/fonts/score.import.yaml"), []byte("version: 1\nfont: {padding: 5}\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{glyphFontID: assetbuild.Built})
	if err := os.Remove(filepath.Join(root, "art/fonts/score.import.yaml")); err != nil {
		t.Fatal(err)
	}
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{glyphFontID: assetbuild.Built})
}

func TestGlyphFolderRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, file, data, want string
	}{
		{"sidecar beside a glyph image", "art/fonts/score/plus.png.import.yaml", "version: 1\nfont: {padding: 2}\n", "apply to nothing"},
		{"sidecar size", "art/fonts/score.import.yaml", "version: 1\nfont: {size: 8}\n", "only padding applies"},
		{"sidecar chars", "art/fonts/score.import.yaml", "version: 1\nfont: {chars: '0123'}\n", "only padding applies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newGlyphFontProject(t)
			writeFile(t, filepath.Join(root, tc.file), []byte(tc.data))
			if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Build error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	// Refusals that change the folder or game.yaml.
	root := newGlyphFontProject(t)

	// A name that is not one character and has no glyphs entry.
	if err := os.Remove(filepath.Join(root, "art/fonts/score/plus.png")); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(root, "art/fonts/score/dot.png"), 4, 4, color.NRGBA{A: 255})
	editProject(t, root, "        plus.png: \"+\"\n        times.png: \"×\"\n", "        dot.png: \".\"\n        times.png: \"×\"\n")
	build(t, root, assetbuild.Builder{})
	writePNG(t, filepath.Join(root, "art/fonts/score/question.png"), 4, 4, color.NRGBA{A: 255})
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "no glyphs entry maps it") {
		t.Fatalf("unmapped name: Build error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "art/fonts/score/question.png")); err != nil {
		t.Fatal(err)
	}

	// Two images for one character.
	writePNG(t, filepath.Join(root, "art/fonts/score/zero.png"), 4, 4, color.NRGBA{A: 255})
	editProject(t, root, "        times.png: \"×\"\n", "        times.png: \"×\"\n        zero.png: \"0\"\n")
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "both glyphs of \"0\"") {
		t.Fatalf("duplicate character: Build error = %v", err)
	}

	// A glyphs entry for a file the folder does not hold.
	if err := os.Remove(filepath.Join(root, "art/fonts/score/zero.png")); err != nil {
		t.Fatal(err)
	}
	editProject(t, root, "        zero.png: \"0\"\n", "        minus.png: \"-\"\n")
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "which is not in the folder") {
		t.Fatalf("missing mapped file: Build error = %v", err)
	}
	editProject(t, root, "        minus.png: \"-\"\n", "")

	// A file that is not a PNG image.
	writeFile(t, filepath.Join(root, "art/fonts/score/readme.txt"), []byte("digits"))
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "is not a PNG image") {
		t.Fatalf("non-PNG file: Build error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "art/fonts/score/readme.txt")); err != nil {
		t.Fatal(err)
	}

	// A damaged PNG.
	writeFile(t, filepath.Join(root, "art/fonts/score/0.png"), []byte("not a png"))
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "decode 0.png") {
		t.Fatalf("damaged PNG: Build error = %v", err)
	}
}

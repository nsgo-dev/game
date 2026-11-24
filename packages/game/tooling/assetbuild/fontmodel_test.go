package assetbuild_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// fontAndModelProject extends sampleProject with a font target and a model target.
const fontAndModelProject = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/hud.fnt
      size: 16
      chars: "AVa "
  models:
    - in: models/tri.gltf
      out: assets/tri.glb
`

// triangleGLTF is a one-triangle model whose buffer and image are files beside it, with an animation that moves its node.
const triangleGLTF = `{
  "asset": {"version": "2.0"},
  "scene": 0,
  "scenes": [{"nodes": [0]}],
  "nodes": [{"name": "tri", "mesh": 0}],
  "meshes": [{"primitives": [{"attributes": {"POSITION": 0}}]}],
  "textures": [{"source": 0}],
  "images": [{"uri": "tri%20skin.png"}],
  "buffers": [{"uri": "tri.bin", "byteLength": 68}],
  "bufferViews": [
    {"buffer": 0, "byteOffset": 0, "byteLength": 36},
    {"buffer": 0, "byteOffset": 36, "byteLength": 8},
    {"buffer": 0, "byteOffset": 44, "byteLength": 24}
  ],
  "accessors": [
    {"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": [0, 0, 0], "max": [1, 1, 0]},
    {"bufferView": 1, "componentType": 5126, "count": 2, "type": "SCALAR", "min": [0], "max": [1]},
    {"bufferView": 2, "componentType": 5126, "count": 2, "type": "VEC3"}
  ],
  "animations": [{"channels": [{"sampler": 0, "target": {"node": 0, "path": "translation"}}], "samplers": [{"input": 1, "output": 2}]}],
  "extras": {"note": "<kept>"}
}`

// triangleBin is the buffer of triangleGLTF: the triangle's three positions, the animation's two times and its two translations.
func triangleBin() []byte {
	var buf bytes.Buffer
	for _, f := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 2, 0, 0} {
		_ = binary.Write(&buf, binary.LittleEndian, f)
	}
	return buf.Bytes()
}

func newFontAndModelProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	writeFile(t, filepath.Join(root, "models/tri.gltf"), []byte(triangleGLTF))
	writeFile(t, filepath.Join(root, "models/tri.bin"), triangleBin())
	writePNG(t, filepath.Join(root, "models/tri skin.png"), 4, 4, color.NRGBA{R: 255, A: 255})
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontAndModelProject))
	return root
}

const (
	fontID  = "font assets/fonts/hud.fnt"
	modelID = "model assets/tri.glb"
)

// loadFont reads the BMFont at rel under root as a game loads it.
func loadFont(t *testing.T, root, rel string) *text.Font {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	f, err := text.LoadBMFont(data, func(int, string) core.ImageSource { return core.ImageSource{} })
	if err != nil {
		t.Fatalf("LoadBMFont: %v\n%s", err, data)
	}
	return f
}

// loadGLB reads the GLB at rel under root on its own, as a game loads it.
func loadGLB(t *testing.T, root, rel string) *gltf.Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gltf.Load(data, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return doc
}

func TestFontTargetBakesABitmapFont(t *testing.T) {
	root := newFontAndModelProject(t)
	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{fontID: assetbuild.Built, modelID: assetbuild.Built})
	if got := r.Targets[0].Outputs; strings.Join(got, " ") != "assets/fonts/hud.fnt assets/fonts/hud.png" {
		t.Fatalf("font outputs = %v", got)
	}
	f := loadFont(t, root, "assets/fonts/hud.fnt")
	if f.Size != 16 || f.Face != "Go" || len(f.Glyphs) != 4 || len(f.Pages) != 1 {
		t.Fatalf("font = size %v, face %q, %d glyphs, %d pages", f.Size, f.Face, len(f.Glyphs), len(f.Pages))
	}
	if g := f.Glyphs[' ']; g.Frame.W != 0 || g.XAdvance <= 0 {
		t.Fatalf("space = %+v, want no ink and an advance", g)
	}
	if g := f.Glyphs['A']; g.Frame.W <= 0 || g.Frame.H <= 0 || g.YOffset < 0 || g.YOffset+g.Frame.H > f.Base+1 {
		t.Fatalf("A = %+v, base %v", g, f.Base)
	}
	// The kerning between the baked characters is the font's, at the baked size in whole pixels.
	otf, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	kern := func(a, b rune, size int) float32 {
		t.Helper()
		ga, err := otf.GlyphIndex(&buf, a)
		if err != nil {
			t.Fatal(err)
		}
		gb, err := otf.GlyphIndex(&buf, b)
		if err != nil {
			t.Fatal(err)
		}
		k, err := otf.Kern(&buf, ga, gb, fixed.I(size), font.HintingFull)
		if err != nil && !errors.Is(err, sfnt.ErrNotFound) {
			t.Fatal(err)
		}
		return float32(k.Round())
	}
	for _, a := range "AVa " {
		for _, b := range "AVa " {
			if got, want := f.Kerning(a, b), kern(a, b, 16); got != want {
				t.Errorf("kerning %q %q = %v, want %v", a, b, got, want)
			}
		}
	}
	if f.LineHeight < 16 || f.Base <= 0 || f.Base > f.LineHeight {
		t.Fatalf("line height %v, base %v", f.LineHeight, f.Base)
	}
}

// withKern returns the TrueType font ttf with a kern table of pairs added, as assetbuild.AddKernTable adds it.
func withKern(t *testing.T, ttf []byte, pairs [][3]int) []byte {
	t.Helper()
	out, err := assetbuild.AddKernTable(ttf, pairs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestFontKerningScalesWithSize checks that the baked kerning is the font's at the baked size, which opentype's Face.Kern, giving the same amount at every size, does not report.
func TestFontKerningScalesWithSize(t *testing.T) {
	root := newFontAndModelProject(t)
	// Go Regular has 2048 units per em, so -256 units is -2 pixels at 16 pixels and -8 at 64.
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), withKern(t, goregular.TTF, [][3]int{{'A', 'V', -256}, {'V', 'a', -128}}))
	build(t, root, assetbuild.Builder{})
	f := loadFont(t, root, "assets/fonts/hud.fnt")
	if got := [3]float32{f.Kerning('A', 'V'), f.Kerning('V', 'a'), f.Kerning('V', 'A')}; got != [3]float32{-2, -1, 0} {
		t.Fatalf("kerning at 16 pixels: A V, V a, V A = %v, want [-2 -1 0]", got)
	}
	editProject(t, root, "      size: 16\n", "      size: 64\n")
	build(t, root, assetbuild.Builder{})
	f = loadFont(t, root, "assets/fonts/hud.fnt")
	if got := [3]float32{f.Kerning('A', 'V'), f.Kerning('V', 'a'), f.Kerning('V', 'A')}; got != [3]float32{-8, -4, 0} {
		t.Fatalf("kerning at 64 pixels: A V, V a, V A = %v, want [-8 -4 0]", got)
	}
}

// TestEditingAFontSidecarRebakesIt holds issue #487's acceptance for fonts: a sidecar's size, characters and padding change the baked font on the next run, rebuilding only the font.
func TestEditingAFontSidecarRebakesIt(t *testing.T) {
	root := newFontAndModelProject(t)
	build(t, root, assetbuild.Builder{})
	before := loadFont(t, root, "assets/fonts/hud.fnt")

	sidecar := filepath.Join(root, "fonts/go.ttf.import.yaml")
	writeFile(t, sidecar, []byte("version: 1\nfont:\n  size: 32\n  chars: '0123456789'\n  padding: 3\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{fontID: assetbuild.Built, modelID: assetbuild.UpToDate})
	after := loadFont(t, root, "assets/fonts/hud.fnt")
	if after.Size != 32 || len(after.Glyphs) != 10 || after.LineHeight <= before.LineHeight {
		t.Fatalf("with the sidecar: size %v, %d glyphs, line height %v (was %v)", after.Size, len(after.Glyphs), after.LineHeight, before.LineHeight)
	}
	if _, ok := after.Glyphs['A']; ok {
		t.Fatal("the sidecar's chars did not replace game.yaml's")
	}
	fnt, err := os.ReadFile(filepath.Join(root, "assets/fonts/hud.fnt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(fnt, []byte("spacing=3,3")) {
		t.Fatalf("padding 3 is not recorded:\n%s", fnt)
	}

	// Listing the same characters in another order bakes the same font.
	writeFile(t, sidecar, []byte("version: 1\nfont:\n  size: 32\n  chars: '9876543210'\n  padding: 3\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{fontID: assetbuild.UpToDate, modelID: assetbuild.UpToDate})

	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	wantStatuses(t, build(t, root, assetbuild.Builder{Check: true}), map[string]assetbuild.Status{fontID: assetbuild.Stale, modelID: assetbuild.UpToDate})
	build(t, root, assetbuild.Builder{})
	if again := loadFont(t, root, "assets/fonts/hud.fnt"); again.Size != 16 || len(again.Glyphs) != 4 {
		t.Fatalf("after removing the sidecar: size %v, %d glyphs", again.Size, len(again.Glyphs))
	}
}

func TestFontIsDeterministic(t *testing.T) {
	a, b := newFontAndModelProject(t), newFontAndModelProject(t)
	build(t, a, assetbuild.Builder{})
	build(t, b, assetbuild.Builder{})
	for _, rel := range []string{"assets/fonts/hud.fnt", "assets/fonts/hud.png", "assets/tri.glb"} {
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
	wantStatuses(t, build(t, a, assetbuild.Builder{Check: true}), map[string]assetbuild.Status{fontID: assetbuild.UpToDate, modelID: assetbuild.UpToDate})
}

func TestFontWithSeveralPages(t *testing.T) {
	root := newFontAndModelProject(t)
	editProject(t, root, "      size: 16\n      chars: \"AVa \"\n", "      size: 1500\n      chars: \"MW\"\n")
	r := build(t, root, assetbuild.Builder{})
	if got := r.Targets[0].Outputs; strings.Join(got, " ") != "assets/fonts/hud-0.png assets/fonts/hud-1.png assets/fonts/hud.fnt" {
		t.Fatalf("font outputs = %v", got)
	}
	if f := loadFont(t, root, "assets/fonts/hud.fnt"); len(f.Pages) != 2 || f.Glyphs['M'].Page == f.Glyphs['W'].Page {
		t.Fatalf("font has %d pages, M on %d and W on %d", len(f.Pages), f.Glyphs['M'].Page, f.Glyphs['W'].Page)
	}

	// Fewer pages next time delete the pages no longer written.
	editProject(t, root, "      size: 1500\n", "      size: 20\n")
	build(t, root, assetbuild.Builder{})
	wantGone(t, root, "assets/fonts/hud-0.png", "assets/fonts/hud-1.png")
}

// TestFontPagesLeftWithoutTheCache checks that pages of an earlier build are removed, and reported as stale by Check, when the cache that lists them was not kept, as in a checkout that commits the outputs but not the cache.
func TestFontPagesLeftWithoutTheCache(t *testing.T) {
	root := newFontAndModelProject(t)
	editProject(t, root, "      size: 16\n      chars: \"AVa \"\n", "      size: 1500\n      chars: \"MW\"\n")
	build(t, root, assetbuild.Builder{})
	removeCache := func() {
		t.Helper()
		if err := os.Remove(filepath.Join(root, project.DefaultAssetsCache)); err != nil {
			t.Fatal(err)
		}
	}
	removeCache()
	editProject(t, root, "      size: 1500\n", "      size: 20\n")
	r := build(t, root, assetbuild.Builder{})
	if got := r.Targets[0].Outputs; strings.Join(got, " ") != "assets/fonts/hud.fnt assets/fonts/hud.png" {
		t.Fatalf("font outputs = %v", got)
	}
	wantGone(t, root, "assets/fonts/hud-0.png", "assets/fonts/hud-1.png")

	// A leftover page next to current outputs makes the font stale for Check, which removes nothing.
	page, err := os.ReadFile(filepath.Join(root, "assets/fonts/hud.png"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "assets/fonts/hud-1.png"), page)
	writeFile(t, filepath.Join(root, "assets/fonts/hud-1.json"), []byte("{}"))
	removeCache()
	wantStatuses(t, build(t, root, assetbuild.Builder{Check: true}), map[string]assetbuild.Status{fontID: assetbuild.Stale, modelID: assetbuild.UpToDate})
	if _, err := os.Stat(filepath.Join(root, "assets/fonts/hud-1.png")); err != nil {
		t.Fatalf("Check removed the leftover page: %v", err)
	}
	build(t, root, assetbuild.Builder{})
	wantGone(t, root, "assets/fonts/hud-1.png")
	// A JSON file is never a font's page, so it is left alone.
	if _, err := os.Stat(filepath.Join(root, "assets/fonts/hud-1.json")); err != nil {
		t.Fatalf("hud-1.json was removed: %v", err)
	}
}

func TestModelTargetPacksOneGLB(t *testing.T) {
	root := newFontAndModelProject(t)
	build(t, root, assetbuild.Builder{})
	doc := loadGLB(t, root, "assets/tri.glb")
	if len(doc.Animations) != 1 || len(doc.Images) != 1 || doc.Images[0].URI != "" || doc.Images[0].MimeType != "image/png" {
		t.Fatalf("GLB has %d animations and images %+v", len(doc.Animations), doc.Images)
	}
	img, err := doc.DecodeImage(0)
	if err != nil || img.Bounds().Dx() != 4 {
		t.Fatalf("embedded image: %v, %v", img, err)
	}
	pos, err := doc.ReadVec3(0)
	if err != nil || len(pos) != 3 || pos[1] != (math3d.Vec3{X: 1}) {
		t.Fatalf("positions = %v, %v", pos, err)
	}
	if len(doc.Scenes[0].Roots) != 1 || doc.Nodes[doc.Scenes[0].Roots[0]].Name != "tri" {
		t.Fatalf("scale 1 changed the scene: %+v", doc.Scenes[0])
	}
	data, err := os.ReadFile(filepath.Join(root, "assets/tri.glb"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"note":"<kept>"`)) {
		t.Fatal("the model's extras are not kept as written")
	}

	// Editing a file the model names rebuilds it.
	writePNG(t, filepath.Join(root, "models/tri skin.png"), 8, 8, color.NRGBA{G: 255, A: 255})
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{fontID: assetbuild.UpToDate, modelID: assetbuild.Built})
	if img, err := loadGLB(t, root, "assets/tri.glb").DecodeImage(0); err != nil || img.Bounds().Dx() != 8 {
		t.Fatalf("after editing the image: %v, %v", img, err)
	}
}

// TestEditingAModelSidecarRepacksIt holds issue #487's acceptance for models: a sidecar's scale and animations change the packed GLB on the next run, rebuilding only the model.
func TestEditingAModelSidecarRepacksIt(t *testing.T) {
	root := newFontAndModelProject(t)
	build(t, root, assetbuild.Builder{})

	writeFile(t, filepath.Join(root, "models/tri.gltf.import.yaml"), []byte("version: 1\nmodel:\n  scale: 0.5\n  animations: false\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), map[string]assetbuild.Status{fontID: assetbuild.UpToDate, modelID: assetbuild.Built})
	doc := loadGLB(t, root, "assets/tri.glb")
	if len(doc.Animations) != 0 {
		t.Fatalf("animations: false kept %d animations", len(doc.Animations))
	}
	roots := doc.Scenes[0].Roots
	if len(roots) != 1 {
		t.Fatalf("scene roots = %v", roots)
	}
	top := doc.Nodes[roots[0]]
	if top.Scale != (math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}) || len(top.Children) != 1 || doc.Nodes[top.Children[0]].Name != "tri" {
		t.Fatalf("scene root = %+v", top)
	}
	if w := doc.Nodes[top.Children[0]].WorldMatrix; math.Abs(float64(w[0])-0.5) > 1e-6 {
		t.Fatalf("the triangle's world matrix is not scaled by 0.5: %v", w)
	}

	// A credit-only edit changes no output.
	writeFile(t, filepath.Join(root, "models/tri.gltf.import.yaml"), []byte("version: 1\nmodel: {scale: 0.5, animations: false}\ncredit: {author: Kenney, license: CC0-1.0}\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), all2(assetbuild.UpToDate))
}

// TestModelScaleWithScenesSharingARoot checks that scenes listing the same root node share the scaled node put above it, since glTF allows a node only one parent.
func TestModelScaleWithScenesSharingARoot(t *testing.T) {
	root := newFontAndModelProject(t)
	src := strings.Replace(triangleGLTF, `"scenes": [{"nodes": [0]}],`, `"scenes": [{"nodes": [0]}, {"nodes": [0, 1]}],`, 1)
	src = strings.Replace(src, `"nodes": [{"name": "tri", "mesh": 0}],`, `"nodes": [{"name": "tri", "mesh": 0}, {"name": "marker"}],`, 1)
	writeFile(t, filepath.Join(root, "models/tri.gltf"), []byte(src))
	writeFile(t, filepath.Join(root, "models/tri.gltf.import.yaml"), []byte("version: 1\nmodel: {scale: 2}\n"))
	build(t, root, assetbuild.Builder{})
	doc := loadGLB(t, root, "assets/tri.glb")
	if len(doc.Scenes) != 2 || len(doc.Scenes[0].Roots) != 1 || len(doc.Scenes[1].Roots) != 2 {
		t.Fatalf("scenes = %+v", doc.Scenes)
	}
	if doc.Scenes[0].Roots[0] != doc.Scenes[1].Roots[0] {
		t.Fatalf("the scenes put tri under different nodes: %+v", doc.Scenes)
	}
	for _, r := range doc.Scenes[1].Roots {
		n := doc.Nodes[r]
		if n.Scale != (math3d.Vec3{X: 2, Y: 2, Z: 2}) || len(n.Children) != 1 {
			t.Fatalf("root %d = %+v, want a node scaled by 2 above one of the model's roots", r, n)
		}
	}
	if len(doc.Nodes) != 4 {
		t.Fatalf("%d nodes, want the model's 2 and one scaled node above each", len(doc.Nodes))
	}
}

func all2(s assetbuild.Status) map[string]assetbuild.Status {
	return map[string]assetbuild.Status{fontID: s, modelID: s}
}

func TestModelFromAGLB(t *testing.T) {
	root := newFontAndModelProject(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "placeholder", "pack", "models", "crate.glb"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "models/crate.glb"), data)
	editProject(t, root, "      out: assets/tri.glb\n", "      out: assets/tri.glb\n    - in: models/crate.glb\n      out: assets/crate.glb\n      scale: 2\n")
	build(t, root, assetbuild.Builder{})
	in, err := gltf.Load(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := loadGLB(t, root, "assets/crate.glb")
	if len(out.Meshes) != len(in.Meshes) || len(out.Nodes) != len(in.Nodes)+len(in.Scenes) {
		t.Fatalf("packed crate has %d meshes and %d nodes; the source has %d and %d", len(out.Meshes), len(out.Nodes), len(in.Meshes), len(in.Nodes))
	}
	a, err := in.ReadVec3(0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := out.ReadVec3(0)
	if err != nil || len(a) != len(b) || a[0] != b[0] {
		t.Fatalf("accessor 0 changed in packing: %v vs %v (%v)", a[:1], b, err)
	}
}

func TestFontAndModelRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, file, data, want string
	}{
		{"image settings on a font", "fonts/go.ttf.import.yaml", "version: 1\nimage: {filter: nearest}\n", "holds image settings, but a font target bakes it as a font"},
		{"font settings on a model", "models/tri.gltf.import.yaml", "version: 1\nfont: {size: 8}\n", "holds font settings, but a model target packs it as a model"},
		{"image settings on an image a model embeds", "models/tri skin.png.import.yaml", "version: 1\nimage: {filter: nearest}\n", "no target in game.yaml reads models/tri skin.png"},
		{"not a font", "fonts/go.ttf", "not a font", "parse font"},
		{"no glyph", "fonts/go.ttf.import.yaml", "version: 1\nfont: {chars: \"\U0001F600\"}\n", "a glyph for none"},
		{"missing buffer", "models/tri.bin", "", "byteLength 68"},
		{"model leaves the project", "models/tri.gltf", strings.Replace(triangleGLTF, `"tri.bin"`, `"../../outside.bin"`, 1), "leaves the project"},
		{"absolute URI", "models/tri.gltf", strings.Replace(triangleGLTF, `"tri.bin"`, `"file:///etc/passwd"`, 1), "relative path"},
		{"not glTF", "models/tri.gltf", "[1, 2]", "decode glTF JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFontAndModelProject(t)
			writeFile(t, filepath.Join(root, tc.file), []byte(tc.data))
			if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Build error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestModelCreditOnlySidecarBesideAnEmbeddedImage(t *testing.T) {
	root := newFontAndModelProject(t)
	writeFile(t, filepath.Join(root, "models/tri skin.png.import.yaml"), []byte("version: 1\ncredit: {author: Ana, license: CC-BY-4.0}\n"))
	build(t, root, assetbuild.Builder{})
}

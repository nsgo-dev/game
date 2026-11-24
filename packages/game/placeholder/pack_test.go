package placeholder_test

import (
	"bytes"
	"context"
	"flag"
	"image"
	"image/draw"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/placeholder"
	"github.com/nsgo-dev/game/packages/game/placeholder/internal/gen"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

var update = flag.Bool("update", false, "rewrite the committed pack from the generator")

// packDir is the pack's folder in the source tree.
const packDir = "pack"

// credits is the pack's credits file, which a person writes, so the generator neither writes nor checks it.
const credits = "CREDITS.md"

// packFiles returns the paths of every file in the pack but its credits.
func packFiles(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := fs.WalkDir(placeholder.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && p != credits {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// TestPackMatchesGenerator checks that the committed pack is exactly what the generator draws, comparing images by their pixels so that a change to the PNG encoder's compression does not fail it. With -update it rewrites the pack first; the embedded FS still holds the old files until the next build, so the comparison reads the files on disk.
func TestPackMatchesGenerator(t *testing.T) {
	files, err := gen.Files()
	if err != nil {
		t.Fatal(err)
	}
	again, err := gen.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range gen.Paths(files) {
		if !bytes.Equal(files[p], again[p]) {
			t.Errorf("%s: the generator wrote different bytes on a second run", p)
		}
	}
	if *update {
		for _, p := range gen.Paths(files) {
			dst := filepath.Join(packDir, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, files[p], 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	disk := os.DirFS(packDir)
	var committed []string
	err = fs.WalkDir(disk, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && p != credits {
			committed = append(committed, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := gen.Paths(files); !slices.Equal(committed, want) {
		t.Fatalf("the pack holds %v, the generator writes %v; run go tool nsgo test ./placeholder -run TestPackMatchesGenerator -update from packages/game and delete files it no longer writes", committed, want)
	}
	for _, p := range committed {
		got, err := fs.ReadFile(disk, p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(p, ".png") {
			if !samePixels(t, got, files[p]) {
				t.Errorf("%s: the committed pixels differ from the generator's; run go tool nsgo test ./placeholder -run TestPackMatchesGenerator -update from packages/game", p)
			}
			continue
		}
		if !bytes.Equal(got, files[p]) {
			t.Errorf("%s: the committed file differs from the generator's (%d and %d bytes); run go tool nsgo test ./placeholder -run TestPackMatchesGenerator -update from packages/game", p, len(got), len(files[p]))
		}
	}
}

func samePixels(t *testing.T, a, b []byte) bool {
	t.Helper()
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(nrgba(ia).Pix, nrgba(ib).Pix) && ia.Bounds() == ib.Bounds()
}

func nrgba(img image.Image) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Rect, img, img.Bounds().Min, draw.Src)
	return out
}

// TestSidecars checks that every image and sound has a sidecar in its canonical form, which parses and encodes back to the same bytes, and that nothing else has one.
func TestSidecars(t *testing.T) {
	paths := packFiles(t)
	for _, p := range paths {
		if importsettings.IsSidecar(p) {
			asset := strings.TrimSuffix(p, importsettings.Suffix)
			if !slices.Contains(paths, asset) {
				t.Errorf("%s: no asset %s beside it", p, asset)
			}
			data, err := fs.ReadFile(placeholder.FS(), p)
			if err != nil {
				t.Fatal(err)
			}
			f, err := importsettings.Parse(data)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			again, err := importsettings.Encode(f)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, data) {
				t.Errorf("%s is not in canonical form:\n%s\nwant\n%s", p, data, again)
			}
			if strings.HasSuffix(asset, ".png") != (f.Image != nil) {
				t.Errorf("%s: an image's sidecar needs an image section and a sound's an audio one", p)
			}
			continue
		}
		if ext := filepath.Ext(p); (ext == ".png" || ext == ".wav") && !slices.Contains(paths, importsettings.Path(p)) {
			t.Errorf("%s has no import settings sidecar", p)
		}
	}
}

// assetsYAML is the assets section the placeholder guide (docs/en/placeholder.md) gives for a project that copies the pack's folder into art/placeholder: an atlas of the sprite sheets, the tileset and the font page as textures, and every sound converted.
const assetsYAML = `version: 1
name: Pack
id: dev.example.pack
appVersion: '1'
window: {width: 320, height: 180}
assets:
  atlases:
    - name: sprites
      in: art/placeholder/sprites
      out: assets
  textures:
    - in: art/placeholder/tiles/tileset.png
      out: assets/tileset.ktx2
    - in: art/placeholder/fonts/pixel.png
      out: assets/pixel.ktx2
  audio:
    - in: art/placeholder/sounds/click.wav
      out: assets/click.wav
    - in: art/placeholder/sounds/coin.wav
      out: assets/coin.wav
    - in: art/placeholder/sounds/explosion.wav
      out: assets/explosion.wav
    - in: art/placeholder/sounds/hit.wav
      out: assets/hit.wav
    - in: art/placeholder/sounds/jump.wav
      out: assets/jump.wav
    - in: art/placeholder/sounds/music_loop.wav
      out: assets/music_loop.wav
    - in: art/placeholder/sounds/powerup.wav
      out: assets/powerup.wav
    - in: art/placeholder/sounds/select.wav
      out: assets/select.wav
    - in: art/placeholder/sounds/shoot.wav
      out: assets/shoot.wav
`

// TestPackBuildsWithGameAssets copies the pack into a project and builds it with the assets section the guide gives, so every sidecar is one game assets accepts and applies.
func TestPackBuildsWithGameAssets(t *testing.T) {
	root := t.TempDir()
	for _, p := range packFiles(t) {
		data, err := fs.ReadFile(placeholder.FS(), p)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, "art", "placeholder", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := project.Parse([]byte(assetsYAML))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range placeholderSounds(t) {
		if !strings.Contains(assetsYAML, "sounds/"+s+".wav") {
			t.Errorf("the assets section converts no sounds/%s.wav", s)
		}
	}
	b := &assetbuild.Builder{Root: root}
	r, err := b.Build(context.Background(), f.Assets)
	if err != nil {
		t.Fatal(err)
	}
	if want := 1 + 2 + len(placeholderSounds(t)); len(r.Targets) != want {
		t.Fatalf("built %d targets, want %d", len(r.Targets), want)
	}
	atlasJSON, err := os.ReadFile(filepath.Join(root, "assets", "sprites.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range []string{`"player_7.png"`, `"items_0.png"`, `"ui_3.png"`} {
		if !bytes.Contains(atlasJSON, []byte(frame)) {
			t.Errorf("the atlas has no frame %s, which the sheets' sidecars slice", frame)
		}
	}
	music, err := os.ReadFile(filepath.Join(root, "assets", "music_loop.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(music, []byte("smpl")) {
		t.Error("the converted music has no smpl chunk, so its sidecar's loop was lost")
	}
}

// placeholderSounds returns the names of the pack's sounds.
func placeholderSounds(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(placeholder.FS(), "sounds")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".wav"); ok {
			names = append(names, n)
		}
	}
	return names
}

func TestSheets(t *testing.T) {
	want := map[string][]string{
		"player":  {"idle", "run", "jump", "hurt"},
		"slime":   {"idle"},
		"bat":     {"fly"},
		"coin":    {"spin"},
		"items":   {"gem", "heart", "key", "star", "potion", "bomb", "shield", "flag"},
		"effects": {"explosion", "sparkle", "bullet"},
		"ui":      {"panel", "button", "button_pressed", "cursor"},
	}
	entries, err := fs.Glob(placeholder.FS(), "sprites/*.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("sprites/ holds %v, want a sheet for each of %d names", entries, len(want))
	}
	for name, anims := range want {
		a, err := placeholder.Sheet(name)
		if err != nil {
			t.Fatalf("Sheet(%q): %v", name, err)
		}
		frames := int(a.Image.Size.Width) / gen.Cell
		if len(a.Frames) != frames || a.Image.Size.Height != gen.Cell {
			t.Errorf("%s: %d frames on a %vx%v sheet, want one per %d-pixel cell", name, len(a.Frames), a.Image.Size.Width, a.Image.Size.Height, gen.Cell)
		}
		if _, ok := a.Frames[name+"_0"]; !ok {
			t.Errorf("%s: no frame %s_0", name, name)
		}
		for _, anim := range anims {
			if len(a.Animations[anim].Frames) == 0 {
				t.Errorf("%s: no animation %q", name, anim)
			}
		}
		if len(a.Animations) != len(anims) {
			t.Errorf("%s: %d animations, want %v", name, len(a.Animations), anims)
		}
	}
	ui, err := placeholder.Sheet("ui")
	if err != nil {
		t.Fatal(err)
	}
	if in := ui.Slices["panel"]; in.Left != 4 || in.Top != 4 || in.Right != 4 || in.Bottom != 4 {
		t.Errorf("panel's nine-slice insets = %+v, want 4 on every side", in)
	}
	fx, err := placeholder.Sheet("effects")
	if err != nil {
		t.Fatal(err)
	}
	if fx.Animations["explosion"].Loop || !fx.Animations["sparkle"].Loop {
		t.Error("the explosion plays once and the sparkle loops")
	}
	if _, err := placeholder.Sheet("missing"); err == nil {
		t.Error("Sheet of a missing sheet returned no error")
	}
}

func TestFont(t *testing.T) {
	f, err := placeholder.Font()
	if err != nil {
		t.Fatal(err)
	}
	if f.LineHeight != 10 || f.Base != 7 {
		t.Errorf("line height %v and base %v, want 10 and 7", f.LineHeight, f.Base)
	}
	for r := rune(' '); r <= '~'; r++ {
		g, ok := f.Glyphs[r]
		if !ok {
			t.Errorf("no glyph for %q", r)
			continue
		}
		if g.XAdvance < 2 || g.XAdvance > 6 {
			t.Errorf("%q advances %v, want 2 to 6", r, g.XAdvance)
		}
	}
	if f.Glyphs['i'].XAdvance >= f.Glyphs['m'].XAdvance {
		t.Error("the font is not proportional: i is as wide as m")
	}
}

func TestLevel(t *testing.T) {
	m, err := placeholder.Level()
	if err != nil {
		t.Fatal(err)
	}
	if m.Width != gen.LevelWidth || m.Height != gen.LevelHeight || m.TileWidth != gen.Cell {
		t.Fatalf("level is %dx%d tiles of %v, want %dx%d of %d", m.Width, m.Height, m.TileWidth, gen.LevelWidth, gen.LevelHeight, gen.Cell)
	}
	if m.Layer("decor") == nil || m.Layer("ground") == nil {
		t.Fatal("level lacks its decor or ground layer")
	}
	objs := m.ObjectLayer("objects")
	if objs == nil {
		t.Fatal("level has no objects layer")
	}
	types := map[string]int{}
	for _, o := range objs.Objects {
		types[o.Type]++
	}
	for _, typ := range []string{"spawn", "coin", "gem", "slime", "bat", "exit"} {
		if types[typ] == 0 {
			t.Errorf("level has no %s object", typ)
		}
	}
	if m.FlagsAt(0, 13)&tilemap.TileSolid == 0 {
		t.Error("the grass under the spawn point is not solid")
	}
	if m.FlagsAt(14, 11)&tilemap.TileOneWay == 0 {
		t.Error("the platform over the pool is not one-way")
	}
	if m.FlagsAt(14, 13) != 0 {
		t.Error("the air over the pool is not empty")
	}
	ts := m.Tilesets[0]
	if len(ts.Animations) != 3 {
		t.Errorf("tileset has %d animated tiles, want water, lava and the torch", len(ts.Animations))
	}
}

func TestModels(t *testing.T) {
	for _, name := range []string{"crate.glb", "robot_arm.gltf", "gem.glb"} {
		doc, err := placeholder.Model(name)
		if err != nil {
			t.Fatalf("Model(%q): %v", name, err)
		}
		if len(doc.Meshes) == 0 {
			t.Errorf("%s has no mesh", name)
		}
	}
	arm, err := placeholder.Model("robot_arm.gltf")
	if err != nil {
		t.Fatal(err)
	}
	if len(arm.Skins) != 1 || len(arm.Animations) == 0 {
		t.Error("the robot arm is not skinned and animated")
	}
	if _, err := placeholder.Model("missing.glb"); err == nil {
		t.Error("Model of a missing model returned no error")
	} else if msg := err.Error(); !strings.HasPrefix(msg, `placeholder: no model "missing.glb"`) || !strings.Contains(msg, "crate.glb, robot_arm.gltf, gem.glb") {
		t.Errorf("Model of a missing model: %v, want it named with the models there are", err)
	}
}

func TestSounds(t *testing.T) {
	for _, name := range placeholderSounds(t) {
		src, err := placeholder.Sound(name)
		if err != nil {
			t.Fatal(err)
		}
		pcm, rate, channels, err := sound.DecodeWAV(src.Data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rate != gen.SampleRate || channels != 1 {
			t.Errorf("%s: %d Hz, %d channels, want %d Hz mono", name, rate, channels, gen.SampleRate)
		}
		peak := 0.0
		for _, s := range pcm {
			peak = max(peak, s, -s)
		}
		if peak < 0.75 || peak > 0.85 {
			t.Errorf("%s: peak %.3f, want 0.8", name, peak)
		}
		if name == "music_loop" {
			if want := gen.MusicBeats * gen.SampleRate * 60 / gen.MusicBPM; len(pcm) != want {
				t.Errorf("music loop is %d samples, want %d beats, %d", len(pcm), gen.MusicBeats, want)
			}
			// Every note fades to silence, so the loop's end meets its start without a click.
			if first, last := pcm[0], pcm[len(pcm)-1]; first*first > 1e-4 || last*last > 1e-4 {
				t.Errorf("music loop starts at %v and ends at %v, want both silent", first, last)
			}
		}
	}
	if _, err := placeholder.Sound("missing"); err == nil {
		t.Error("Sound of a missing sound returned no error")
	}
}

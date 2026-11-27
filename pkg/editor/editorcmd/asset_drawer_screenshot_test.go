package editorcmd_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func renderPixelArtPNG(t *testing.T, rows []string, pal map[byte]color.RGBA) []byte {
	t.Helper()
	h := len(rows)
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	for y, r := range rows {
		for x := range len(r) {
			if c, ok := pal[r[x]]; ok {
				img.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func renderTilesetPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			c := color.RGBA{R: 0x3B, G: 0x42, B: 0x50, A: 0xFF}
			if x%16 < 2 || y%16 < 2 {
				c = color.RGBA{R: 0x1F, G: 0x23, B: 0x2A, A: 0xFF}
			} else if (x%16)+(y%16) < 8 {
				c = color.RGBA{R: 0x4A, G: 0x52, B: 0x62, A: 0xFF}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func renderSheetPNG(t *testing.T, sprites [][2]any, pal map[byte]color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 128, 32))
	for i, sp := range sprites {
		ox := (i % 8) * 16
		oy := (i / 8) * 16
		rows := sp[1].([]string)
		for y, r := range rows {
			for x := range len(r) {
				if c, ok := pal[r[x]]; ok && ox+x < 128 && oy+y < 32 {
					img.SetRGBA(ox+x, oy+y, c)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func renderDemoWAV(t *testing.T, sampleRate int, durSec float64, freq float64) []byte {
	t.Helper()
	n := int(float64(sampleRate) * durSec)
	dataSize := n * 2
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	for i := range n {
		p := float64(i) / float64(max(1, n))
		env := min(1.0, p*9.0) * math.Pow(1.0-p, 1.3)
		s := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * env * 22000)
		_ = binary.Write(&buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

// TestAssetDrawerMacOSScreenshots renders the Asset Drawer screen on the real native macOS host
// beside design/editor/Assets.dc.html (screen 1) for visual inspection of tokens, layout,
// drag preview pill, missing-file inspector, and empty-folder state.
func TestAssetDrawerMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pal := map[byte]color.RGBA{
		'o': {0x19, 0x1B, 0x22, 0xFF},
		'L': {0xD7, 0xDE, 0xEA, 0xFF},
		'S': {0x9A, 0xA5, 0xBA, 0xFF},
		'D': {0x5E, 0x67, 0x7C, 0xFF},
		'V': {0x0F, 0x11, 0x16, 0xFF},
		'P': {0xE9, 0xA8, 0x4A, 0xFF},
		'Q': {0xB8, 0x74, 0x2A, 0xFF},
		'B': {0x6B, 0x4A, 0x33, 0xFF},
		'W': {0xE6, 0xEB, 0xF2, 0xFF},
		'G': {0x5F, 0xBF, 0x7A, 0xFF},
		'H': {0xB4, 0xEF, 0xC2, 0xFF},
		'E': {0x3B, 0x8A, 0x55, 0xFF},
		'Y': {0xF2, 0xC1, 0x4E, 0xFF},
		'y': {0xFF, 0xF3, 0xB8, 0xFF},
		'd': {0xB8, 0x86, 0x2A, 0xFF},
		'R': {0xFF, 0x6B, 0x6B, 0xFF},
		'r': {0xFF, 0xB3, 0xB3, 0xFF},
		'M': {0x6D, 0x5A, 0x8C, 0xFF},
		'T': {0x8A, 0x5A, 0x3A, 0xFF},
	}

	knightStand := []string{
		"....PP......", "...PQP......", "..oooooo....", ".oLLLSSo....",
		".oLSVVVo....", ".oLSSSSo....", "..oooooo..W.", ".oLLSSSDo.W.",
		"oLLSSSSDDoW.", "oSSBBBBDDooo", ".oDSSSSDo.o.", "..oDo.oDo...",
		"..oBo.oBo...", "..ooo.ooo...",
	}
	knightStride := []string{
		"....PP......", "...PQP......", "..oooooo....", ".oLLLSSo....",
		".oLSVVVo....", ".oLSSSSo....", "..oooooo..W.", ".oLLSSSDo.W.",
		"oLLSSSSDDoW.", "oSSBBBBDDooo", ".oDSSSSDo.o.", ".oDo...oDo..",
		".oBo...oBo..", ".ooo...ooo..",
	}
	slimeRows := []string{
		"....ooo....", "..ooGGGoo..", ".oGHHGGGGo.", ".oGHGGVGVo.",
		"oGGGGGGGGGo", "oGGGGGGGGGo", "oEEEEEEEEEo", ".ooooooooo.",
	}
	coinRows := []string{
		".oooo.", "oYyYYo", "oYyYdo", "oYYYdo", "oYYddo", ".oooo.",
	}
	heartRows := []string{
		".oo.oo.", "oRroRRo", "oRRRRRo", ".oRRRo.", "..oRo..", "...o...",
	}
	torchRows := []string{
		".oPo.", "oPyPo", "oPPPo", ".oQo.", ".oTo.", ".oTo.", ".oTo.", ".ooo.",
	}
	batRows := []string{
		"o........o", "oo.oooo.oo", "oMoMMMMoMo", ".oMRMMRMo.", "..oMMMMo..", "...o..o...",
	}

	write("game.yaml", []byte(`version: 1
name: dungeon-run
id: dev.nsgo.dungeonrun
appVersion: 0.1.0
window:
  width: 640
  height: 360
assets:
  textures:
    - in: art/props/coin.png
      out: built/coin.ktx2
    - in: art/characters/slime.png
      out: built/slime.ktx2
  audio:
    - in: audio/sfx/jump.wav
      out: built/jump.ogg
    - in: audio/sfx/coin_pickup.wav
      out: built/coin_pickup.ogg
  fonts:
    - in: fonts/PixelOperator.ttf
      out: built/pixel.fnt
    - in: fonts/zpix.ttf
      out: built/zpix.fnt
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`))

	write("scenes/level_01.scene.yaml", []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#181C24"
layers:
  - name: Actors
    space: world
    order: 0
    objects:
      - id: knight
        name: Knight
        x: 96
        y: 210
        sprite:
          atlas: characters
          frame: knight_run_0
      - id: slime_01
        name: Slime 1
        x: 236
        y: 220
        sprite:
          atlas: characters
          frame: slime
      - id: slime_02
        name: Slime 2
        x: 330
        y: 220
        sprite:
          atlas: characters
          frame: slime
      - id: coin_01
        name: Coin 1
        x: 470
        y: 160
        sprite:
          atlas: characters
          frame: coin
      - id: coin_02
        name: Coin 2
        x: 500
        y: 160
        sprite:
          atlas: characters
          frame: coin
      - id: door
        name: Door
        x: 576
        y: 190
        material: materials/door.material.yaml
`))

	write("materials/door.material.yaml", []byte("type: material\nversion: 1\nname: Door\ntexture: art/props/door_open.png\n"))
	write("materials/knight.material.yaml", []byte("type: material\nversion: 1\nname: Knight\ntexture: art/characters/knight.png\n"))
	write("screens/hud.screen.yaml", []byte("version: 1\nname: hud\ndesignWidth: 640\ndesignHeight: 360\nnodes:\n  - id: hearts\n    type: sprite_row\n    sprite: art/ui/heart.png\n    spriteCount: 3\n"))
	write("tilesets/stone.tileset.yaml", []byte("version: 1\ntileWidth: 16\ntileHeight: 16\ncolumns: 4\nimage: art/tiles/stone_tiles.png\n"))
	write("cutscenes/intro.cutscene.yaml", []byte("version: 1\nname: intro\nscene: scenes/level_01.scene.yaml\nduration: 2s\ntracks:\n  - id: sfx\n    kind: sound\n    clips:\n      - id: squish\n        start: 0.2s\n        duration: 0.3s\n        source: audio/sfx/slime_squish.wav\n"))

	sheetPNG := renderSheetPNG(t, [][2]any{
		{"knight_run_0", knightStride},
		{"knight_run_1", knightStand},
		{"knight_run_2", knightStride},
		{"knight_idle_0", knightStand},
		{"knight_idle_1", knightStand},
		{"slime", slimeRows},
		{"coin", coinRows},
		{"bat", batRows},
	}, pal)
	write("art/characters.png", sheetPNG)
	write("art/characters.json", []byte(`{
  "frames": {
    "knight_run_0": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}},
    "knight_run_1": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}},
    "knight_run_2": {"frame": {"x": 32, "y": 0, "w": 16, "h": 16}},
    "knight_idle_0": {"frame": {"x": 48, "y": 0, "w": 16, "h": 16}},
    "knight_idle_1": {"frame": {"x": 64, "y": 0, "w": 16, "h": 16}},
    "slime": {"frame": {"x": 80, "y": 0, "w": 16, "h": 16}},
    "coin": {"frame": {"x": 96, "y": 0, "w": 16, "h": 16}},
    "bat": {"frame": {"x": 112, "y": 0, "w": 16, "h": 16}}
  },
  "meta": {"image": "characters.png", "size": {"w": 128, "h": 32}}
}`))

	write("art/characters/knight.png", renderPixelArtPNG(t, knightStand, pal))
	write("art/characters/slime.png", renderPixelArtPNG(t, slimeRows, pal))
	write("art/characters/bat.png", renderPixelArtPNG(t, batRows, pal))
	write("art/props/coin.png", renderPixelArtPNG(t, coinRows, pal))
	write("art/props/torch_old.png", renderPixelArtPNG(t, torchRows, pal))
	write("art/tiles/stone_tiles.png", renderTilesetPNG(t))
	write("art/ui/heart.png", renderPixelArtPNG(t, heartRows, pal))
	write("audio/sfx/jump.wav", renderDemoWAV(t, 44100, 0.31, 440))
	write("audio/sfx/coin_pickup.wav", renderDemoWAV(t, 44100, 0.18, 880))
	write("audio/sfx/slime_squish.wav", renderDemoWAV(t, 44100, 0.27, 220))
	write("audio/sfx/slime_squish.wav.source.yaml", []byte("version: 1\ngenerator: sfx-gen\n"))
	write("fonts/PixelOperator.ttf", []byte("font"))
	write("fonts/zpix.ttf", []byte("font"))
	if err := os.MkdirAll(filepath.Join(dir, "audio", "music"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Order modification times so sprites, tiles, audio, and fonts appear in the artboard's visual mix.
	baseTime := time.Now()
	ordered := []string{
		"art/characters/bat.png",
		"art/characters/knight.png",
		"art/props/coin.png",
		"art/characters/slime.png",
		"art/tiles/stone_tiles.png",
		"art/ui/heart.png",
		"art/props/torch_old.png",
		"audio/sfx/jump.wav",
		"audio/sfx/coin_pickup.wav",
		"audio/sfx/slime_squish.wav",
		"fonts/PixelOperator.ttf",
		"fonts/zpix.ttf",
		"tilesets/stone.tileset.yaml",
		"art/characters.png",
	}
	for i, rel := range ordered {
		mt := baseTime.Add(-time.Duration(i+1) * time.Minute)
		_ = os.Chtimes(filepath.Join(dir, filepath.FromSlash(rel)), mt, mt)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	ed.SetLeftRailMode(editor.LeftRailAssets)
	ed.ProposeAssetImports([]string{"art/characters/bat.png"})
	ed.SelectAssetPath("art/characters/knight.png")
	ed.SetAssetPreviewClip("run")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Screen 1: browse, knight clip preview, and coin.png drag ghost over viewport.
	app.Post(func() {
		if err := ed.BeginAssetDrag("art/props/coin.png"); err == nil {
			cx, cy := ed.WorldToCanvas(528, 160)
			ed.UpdateAssetDrag(cx, cy)
		}
	})
	app.Settle()
	app.Screenshot("asset-drawer-browse")

	// Cancel drag and inspect missing door_open.png.
	app.Post(func() {
		ed.CancelAssetDrag()
		ed.SelectAssetPath("art/props/door_open.png")
	})
	app.Settle()
	app.Screenshot("asset-drawer-missing")

	// Empty folder state: audio/music.
	app.Press("id:asset-folder-audio-music")
	app.Settle()
	app.Screenshot("asset-drawer-empty-folder")
}

// Command genassets deterministically generates all original art, tilemap,
// bitmap font, and audio assets for the Cave Run sample game: the embedded
// title font under examples/caverun/assets/ and the streamed level group under
// examples/caverun/resources/.
//
// All generated assets are original works dedicated to the public domain under
// CC0-1.0 (see examples/caverun/CREDITS.md). This generator uses only the Go
// standard library and produces byte-for-byte identical output on every run.
//
// Usage:
//
//	go run ./examples/caverun/tools/genassets/main.go
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	outDir := flag.String("out", defaultDir("resources"), "output directory for streamed level resources")
	titleDir := flag.String("title-out", defaultDir("assets"), "output directory for embedded title assets")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}
	if err := os.MkdirAll(*titleDir, 0o755); err != nil {
		fatalf("create title output dir: %v", err)
	}

	if err := generateSprites(*outDir); err != nil {
		fatalf("generate sprites: %v", err)
	}
	if err := generateTileset(*outDir); err != nil {
		fatalf("generate tileset: %v", err)
	}
	if err := generateLevel(*outDir); err != nil {
		fatalf("generate level: %v", err)
	}
	if err := generateFont(*titleDir); err != nil {
		fatalf("generate font: %v", err)
	}
	if err := generateAudio(*outDir); err != nil {
		fatalf("generate audio: %v", err)
	}

	fmt.Printf("generated Cave Run assets in %s (title) and %s (resources)\n", *titleDir, *outDir)
}

func defaultDir(sub string) string {
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", sub))
	}
	return filepath.Join("examples", "caverun", sub)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genassets: "+format+"\n", args...)
	os.Exit(1)
}

// -----------------------------------------------------------------------------
// Palette & Pixel Grid Helpers
// -----------------------------------------------------------------------------

var palette = map[rune]color.NRGBA{
	'.': {0x00, 0x00, 0x00, 0x00}, // transparent
	'k': {0x14, 0x10, 0x20, 0xff}, // deep cavern outline
	'D': {0x1d, 0x18, 0x2e, 0xff}, // cave slate deepest
	'd': {0x27, 0x22, 0x3b, 0xff}, // cave slate dark
	'm': {0x3f, 0x39, 0x5b, 0xff}, // cave slate mid
	'l': {0x61, 0x5a, 0x82, 0xff}, // cave slate light
	'h': {0x8e, 0x88, 0xae, 0xff}, // stone highlight
	'G': {0x1f, 0x6b, 0x48, 0xff}, // moss dark
	'g': {0x48, 0xb8, 0x6a, 0xff}, // moss bright
	'B': {0x5c, 0x34, 0x22, 0xff}, // wood / leather dark
	'b': {0x8c, 0x53, 0x32, 0xff}, // wood / leather mid
	't': {0xc2, 0x83, 0x4e, 0xff}, // wood / tan light
	'R': {0xa6, 0x2c, 0x3b, 0xff}, // crimson dark
	'r': {0xe3, 0x4f, 0x5b, 0xff}, // crimson bright
	'p': {0xff, 0x8a, 0x80, 0xff}, // crimson highlight
	'S': {0xcc, 0x95, 0x6d, 0xff}, // skin shadow
	's': {0xf5, 0xc6, 0x9c, 0xff}, // skin light
	'Y': {0xe8, 0xa8, 0x38, 0xff}, // gold / amber
	'y': {0xff, 0xf0, 0x8a, 0xff}, // bright yellow
	'C': {0x1b, 0x7d, 0x96, 0xff}, // cyan dark
	'c': {0x38, 0xd9, 0xd9, 0xff}, // cyan bright
	'i': {0xb8, 0xff, 0xf9, 0xff}, // ice cyan highlight
	'w': {0xff, 0xff, 0xff, 0xff}, // pure white
	'P': {0x43, 0x1e, 0x6b, 0xff}, // amethyst dark
	'v': {0x7f, 0x3a, 0xc9, 0xff}, // amethyst mid
	'V': {0xc4, 0x7d, 0xff, 0xff}, // amethyst bright
	'O': {0x8f, 0x1d, 0x24, 0xff}, // lava dark
	'o': {0xf2, 0x64, 0x22, 0xff}, // lava orange
}

func blitGrid(dst *image.NRGBA, ox, oy int, name string, rows []string) {
	if len(rows) != 16 {
		panic(fmt.Sprintf("sprite/tile %q has %d rows, want 16", name, len(rows)))
	}
	for y, row := range rows {
		if len(row) != 16 {
			panic(fmt.Sprintf("sprite/tile %q row %d has %d cols, want 16", name, y, len(row)))
		}
		for x, ch := range row {
			col, ok := palette[ch]
			if !ok {
				panic(fmt.Sprintf("sprite/tile %q has unknown palette rune %q at (%d,%d)", name, ch, x, y))
			}
			dst.SetNRGBA(ox+x, oy+y, col)
		}
	}
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// -----------------------------------------------------------------------------
// 1. Sprite Atlas (sprites.png + sprites.json)
// -----------------------------------------------------------------------------

type spriteSpec struct {
	name     string
	duration int
	grid     []string
}

type frameTagSpec struct {
	Name      string `json:"name"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Direction string `json:"direction"`
	Repeat    string `json:"repeat,omitempty"`
	Color     string `json:"color"`
}

func generateSprites(outDir string) error {
	specs := allSpriteSpecs()
	const (
		cols     = 8
		rows     = 4
		cellSize = 16
		sheetW   = cols * cellSize
		sheetH   = rows * cellSize
	)
	if len(specs) > cols*rows {
		return fmt.Errorf("too many sprites (%d) for %dx%d sheet", len(specs), cols, rows)
	}

	sheet := image.NewNRGBA(image.Rect(0, 0, sheetW, sheetH))

	// Build Aseprite JSON-hash preserving exact insertion order so frameTags
	// indices match sprite.LoadAtlas's sequential decoder.
	var framesBuf bytes.Buffer
	framesBuf.WriteString("{\n")
	for i, sp := range specs {
		x := (i % cols) * cellSize
		y := (i / cols) * cellSize
		blitGrid(sheet, x, y, sp.name, sp.grid)

		entry := fmt.Sprintf(
			"    %q: {\n"+
				"      \"frame\": { \"x\": %d, \"y\": %d, \"w\": %d, \"h\": %d },\n"+
				"      \"rotated\": false,\n"+
				"      \"trimmed\": false,\n"+
				"      \"spriteSourceSize\": { \"x\": 0, \"y\": 0, \"w\": %d, \"h\": %d },\n"+
				"      \"sourceSize\": { \"w\": %d, \"h\": %d },\n"+
				"      \"duration\": %d\n"+
				"    }",
			sp.name, x, y, cellSize, cellSize, cellSize, cellSize, cellSize, cellSize, sp.duration,
		)
		framesBuf.WriteString(entry)
		if i < len(specs)-1 {
			framesBuf.WriteString(",")
		}
		framesBuf.WriteString("\n")
	}
	framesBuf.WriteString("  }")

	tags := []frameTagSpec{
		{Name: "idle", From: 0, To: 3, Direction: "forward", Color: "#48b86aff"},
		{Name: "run", From: 4, To: 7, Direction: "forward", Color: "#e34f5bff"},
		{Name: "jump", From: 8, To: 9, Direction: "forward", Color: "#e8a838ff"},
		{Name: "gem", From: 10, To: 13, Direction: "forward", Color: "#38d9d9ff"},
		{Name: "slime", From: 14, To: 17, Direction: "forward", Color: "#7f3ac9ff"},
		{Name: "portal", From: 18, To: 21, Direction: "forward", Color: "#c47dffff"},
		{Name: "spark", From: 22, To: 24, Direction: "forward", Repeat: "1", Color: "#fff08aff"},
	}

	tagsJSON, err := json.MarshalIndent(tags, "    ", "  ")
	if err != nil {
		return err
	}

	atlasJSON := fmt.Sprintf("{\n"+
		"  \"frames\": %s,\n"+
		"  \"meta\": {\n"+
		"    \"app\": \"https://github.com/nsgo-dev/game/tree/main/examples/caverun/tools/genassets\",\n"+
		"    \"version\": \"1.0\",\n"+
		"    \"image\": \"sprites.png\",\n"+
		"    \"format\": \"RGBA8888\",\n"+
		"    \"size\": { \"w\": %d, \"h\": %d },\n"+
		"    \"scale\": \"1\",\n"+
		"    \"frameTags\": %s\n"+
		"  }\n"+
		"}\n",
		framesBuf.String(), sheetW, sheetH, string(tagsJSON),
	)

	if err := writePNG(filepath.Join(outDir, "sprites.png"), sheet); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "sprites.json"), []byte(atlasJSON), 0o644)
}

func allSpriteSpecs() []spriteSpec {
	return []spriteSpec{
		// 0..3: Hero idle (explorer with leather hat, brass headlamp, crimson scarf)
		{
			name: "hero_idle_0", duration: 220,
			grid: []string{
				"....kkkkkk......",
				"...kbbttbbk.....",
				"..kBbbbbbbBk....",
				".kBBBBYyyBBBk...",
				"..kkksssskkk....",
				"....ksSksSk.....",
				"....ksssssk.....",
				"...kRrrrrrpkk...",
				"..kRrrRRRRrpk...",
				"..kBkmllmmkBk...",
				"...kkmllmmkk....",
				"....kBYYBBk.....",
				"....kmmkmmk.....",
				"....kddkddk.....",
				"...kBBkkBBk.....",
				"...kkkkkkkk.....",
			},
		},
		{
			name: "hero_idle_1", duration: 220,
			grid: []string{
				"....kkkkkk......",
				"...kbbttbbk.....",
				"..kBbbbbbbBk....",
				".kBBBBYwyBBBk...",
				"..kkksssskkk....",
				"....ksSksSk.....",
				"....ksssssk.....",
				"...kRrrrrrpkk...",
				"..kRrRRRRrrpk...",
				"..kBkmllmmkBk...",
				"...kkmllmmkk....",
				"....kBYYBBk.....",
				"....kmmkmmk.....",
				"....kddkddk.....",
				"...kBBkkBBk.....",
				"...kkkkkkkk.....",
			},
		},
		{
			name: "hero_idle_2", duration: 220,
			grid: []string{
				"................",
				"....kkkkkk......",
				"...kbbttbbk.....",
				"..kBbbbbbbBk....",
				".kBBBBYyyBBBk...",
				"..kkksssskkk....",
				"....ksSksSk.....",
				"...kRrrrrrpkk...",
				"..kRrrRRRRrpk...",
				"..kBkmllmmkBk...",
				"...kkmllmmkk....",
				"....kBYYBBk.....",
				"....kmmkmmk.....",
				"....kddkddk.....",
				"...kBBkkBBk.....",
				"...kkkkkkkk.....",
			},
		},
		{
			name: "hero_idle_3", duration: 220,
			grid: []string{
				"....kkkkkk......",
				"...kbbttbbk.....",
				"..kBbbbbbbBk....",
				".kBBBBYyyBBBk...",
				"..kkksssskkk....",
				"....ksSksSk.....",
				"....ksssssk.....",
				"...kRrrrrrpk....",
				"..kRrrRRRRrpk...",
				"..kBkmllmmkBk...",
				"...kkmllmmkk....",
				"....kBYYBBk.....",
				"....kmmkmmk.....",
				"....kddkddk.....",
				"...kBBkkBBk.....",
				"...kkkkkkkk.....",
			},
		},
		// 4..7: Hero run cycle
		{
			name: "hero_run_0", duration: 90,
			grid: []string{
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYyyBBBk..",
				"...kkksssskkk...",
				".....ksSksSk....",
				".kk..ksssssk....",
				"kRrkRrrrrrpkk...",
				".kkRrrRRRRrpk...",
				"...kBkmllmmkBk..",
				"....kkmllmmkk...",
				".....kBYYBBk....",
				"...kkmmk.kmmk...",
				"..kBBdk...kddk..",
				"..kkkk.....kBBk.",
				"...........kkkk.",
			},
		},
		{
			name: "hero_run_1", duration: 90,
			grid: []string{
				"................",
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYwyBBBk..",
				"...kkksssskkk...",
				".....ksSksSk....",
				"kRrkRrrrrrpk....",
				".kkRrrRRRRrpk...",
				"....kkmllmmkk...",
				"....kBkmllmmk...",
				".....kBYYBBk....",
				".....kmmkmmk....",
				"....kddkkddk....",
				"....kBBkkBBk....",
				"....kkkkkkkk....",
			},
		},
		{
			name: "hero_run_2", duration: 90,
			grid: []string{
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYyyBBBk..",
				"...kkksssskkk...",
				".....ksSksSk....",
				".....ksssssk....",
				"kRrrkRrrrrrpk...",
				".kkkRrrRRRRpk...",
				"...kBkmllmmkBk..",
				"....kkmllmmkk...",
				".....kBYYBBk....",
				"....kmmkk.kmmk..",
				"...kddk...kBBdk.",
				"..kBBk.....kkkk.",
				"..kkkk..........",
			},
		},
		{
			name: "hero_run_3", duration: 90,
			grid: []string{
				"................",
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYyyBBBk..",
				"...kkksssskkk...",
				".....ksSksSk....",
				".kRkRrrrrrpk....",
				"..kRrrRRRRrpk...",
				"....kkmllmmkk...",
				"....kBkmllmmk...",
				".....kBYYBBk....",
				".....kmmkmmk....",
				"....kddkkddk....",
				"....kBBkkBBk....",
				"....kkkkkkkk....",
			},
		},
		// 8..9: Hero jump / airborne
		{
			name: "hero_jump_0", duration: 140,
			grid: []string{
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYwyBBBk..",
				"...kkksssskkk...",
				".....ksSksSk..kk",
				".kk..ksssssk.kBk",
				"kRrkRrrrrrpkkkBk",
				".kkRrrRRRRrppkk.",
				"....kkmllmmkk...",
				"....kBkmllmmk...",
				".....kBYYBBk....",
				"...kkmmk.kmmkk..",
				"..kBBdk...kddBk.",
				"..kkkk.....kkkk.",
				"................",
			},
		},
		{
			name: "hero_jump_1", duration: 140,
			grid: []string{
				".....kkkkkk.....",
				"....kbbttbbk....",
				"...kBbbbbbbBk...",
				"..kBBBBYyyBBBk..",
				".kk.kksssskkk...",
				"kRrk.ksSksSk....",
				".kRrksssssk.....",
				"..kkRrrrrrpkk...",
				"...kBrrRRRRpk...",
				"...kBkmllmmkBk..",
				"....kkmllmmkk...",
				".....kBYYBBk....",
				"....kmmk.kmmk...",
				"...kddk...kddk..",
				"...kBBk...kBBk..",
				"...kkkk...kkkk..",
			},
		},
		// 10..13: Faceted cave gem spin
		{
			name: "gem_0", duration: 120,
			grid: []string{
				"................",
				"......kkkk......",
				".....kiiick.....",
				"....kiwiiick....",
				"...kiwicccCck...",
				"..kiwicccccCck..",
				"..kicccccccCCk..",
				"..kiccccccCCCk..",
				"...kcccccCCCk...",
				"....kcccCCCk....",
				".....kcCCCk.....",
				"......kCCk......",
				".......kk.......",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "gem_1", duration: 120,
			grid: []string{
				"................",
				"................",
				"......kkkk......",
				".....kiiick.....",
				"....kiwiiCck....",
				"...kiicccCCck...",
				"...kiccccCCCk...",
				"...kcccccCCCk...",
				"....kcccCCCk....",
				".....kcCCCk.....",
				"......kCCk......",
				".......kk.......",
				"................",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "gem_2", duration: 120,
			grid: []string{
				".......w........",
				"......wkw.......",
				"......kiik......",
				".....kiwik......",
				".....kicCk......",
				".....kicCk......",
				".....kccCk......",
				".....kccCk......",
				".....kcCCk......",
				"......kCk.......",
				"......kCk.......",
				".......k........",
				"................",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "gem_3", duration: 120,
			grid: []string{
				"................",
				"................",
				"......kkkk......",
				".....kciick.....",
				"....kcciwiCk....",
				"...kccciicCCk...",
				"...kcccccCCCk...",
				"...kcccccCCCk...",
				"....kcccCCCk....",
				".....kcCCCk.....",
				"......kCCk......",
				".......kk.......",
				"................",
				"................",
				"................",
				"................",
			},
		},
		// 14..17: Patrolling cave slime
		{
			name: "slime_0", duration: 150,
			grid: []string{
				"................",
				"................",
				"................",
				"................",
				"................",
				".....kkkkkk.....",
				"...kkVVVVvvkk...",
				"..kVVwwVVvvvvk..",
				".kVVVvvvVVvvvvk.",
				".kVvkykvvvkykvk.",
				".kVvkYkvvvkYkvk.",
				".kvvvvvvvvvvvPk.",
				".kvvvvPPPPvvPPk.",
				".kPPPPPvvPPPPPk.",
				"..kkkkkkkkkkkk..",
				"................",
			},
		},
		{
			name: "slime_1", duration: 150,
			grid: []string{
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
				"....kkkkkkkk....",
				"..kkVVVVvvvvkk..",
				".kVVwwVVvvvvvvk.",
				"kVVvkykvvvkykvvk",
				"kVvvkykvvvkykvPk",
				"kvvvvvvvvvvvvPPk",
				"kPPPPPPPPPPPPPPk",
				".kkkkkkkkkkkkkk.",
				"................",
			},
		},
		{
			name: "slime_2", duration: 150,
			grid: []string{
				"................",
				"................",
				"................",
				".....kkkkkk.....",
				"....kVVVVvvk....",
				"...kVVwwVvvvk...",
				"..kVVvvvvvvvvk..",
				"..kVvkykvkykvk..",
				"..kVvkYkvkYkvk..",
				"..kvvvvvvvvvPk..",
				"..kvvvvvvvvPPk..",
				"..kvvPPPPvvPPk..",
				"...kPPPPPPPPk...",
				"....kkkkkkkk....",
				"................",
				"................",
			},
		},
		{
			name: "slime_3", duration: 150,
			grid: []string{
				"................",
				"................",
				"................",
				"................",
				"................",
				".....kkkkkk.....",
				"...kkVVVVvvkk...",
				"..kVVwwVVvvvvk..",
				".kVVVvvvVVvvvvk.",
				".kVvkykvvvkykvk.",
				".kVvkYkvvvkYkvk.",
				".kvvvvvvvvvvvPk.",
				".kPPPvvvvvvPPPk.",
				".kPPPPPPPPPPPPk.",
				"..kkkkkkkkkkkk..",
				"................",
			},
		},
		// 18..21: Swirling exit portal
		{
			name: "portal_0", duration: 120,
			grid: []string{
				"....kkkkkkkk....",
				"..kkPPvvvvPPkk..",
				".kPPvvVVVVvvPPk.",
				".kPvVVywwyVVvPk.",
				"kPvVVywyywyVVvPk",
				"kPvVywyVVywyVvPk",
				"kPvVywyvvywyVvPk",
				"kPvVVywyywyVVvPk",
				"kPvVVywwwwyVVvPk",
				"kPvVywyvvywyVvPk",
				"kPvVywyVVywyVvPk",
				"kPvVVywyywyVVvPk",
				".kPvVVywwyVVvPk.",
				".kPPvvVVVVvvPPk.",
				"..kkPPvvvvPPkk..",
				"....kkkkkkkk....",
			},
		},
		{
			name: "portal_1", duration: 120,
			grid: []string{
				"....kkkkkkkk....",
				"..kkvvvvVVPPkk..",
				".kPPVVywwyVVPPk.",
				".kPvVywVVwyVvPk.",
				"kPvVywyvvywyVvPk",
				"kPvVwyVwwVywyvPk",
				"kPvVwywyywywVvPk",
				"kPvVwywyywywVvPk",
				"kPvVwyVwwVywyvPk",
				"kPvVywyvvywyVvPk",
				"kPvVVywVVwyVVvPk",
				"kPvVVVywwyVVvvPk",
				".kPvVVVVVVvvPPk.",
				".kPPvvvvvvPPPPk.",
				"..kkPPvvvvPPkk..",
				"....kkkkkkkk....",
			},
		},
		{
			name: "portal_2", duration: 120,
			grid: []string{
				"....kkkkkkkk....",
				"..kkPPVVVVPPkk..",
				".kPPvvVVVVvvPPk.",
				".kPvVVyYYyVVvPk.",
				"kPvVVyYwwYyVVvPk",
				"kPvVyYwyywYyVvPk",
				"kPvVyYwyYwYyVvPk",
				"kPvVyYwYywYyVvPk",
				"kPvVyYwyywYyVvPk",
				"kPvVVyYwwYyVVvPk",
				"kPvVVyYVVYyVVvPk",
				"kPvVVvyYYyvVVvPk",
				".kPvVVVVVVVVvPk.",
				".kPPvvVVVVvvPPk.",
				"..kkPPvvvvPPkk..",
				"....kkkkkkkk....",
			},
		},
		{
			name: "portal_3", duration: 120,
			grid: []string{
				"....kkkkkkkk....",
				"..kkPPVVvvvvkk..",
				".kPPVVywwyVVPPk.",
				".kPvVywVVwyVvPk.",
				"kPvVywyvvywyVvPk",
				"kPvVwyVwwVywyvPk",
				"kPvVwywyywywVvPk",
				"kPvVwywyywywVvPk",
				"kPvVwyVwwVywyvPk",
				"kPvVywyvvywyVvPk",
				"kPvVVywVVwyVVvPk",
				"kPvvvVVywwyVVvPk",
				".kPPvvVVVVVVvPk.",
				".kPPPPvvvvvvPPk.",
				"..kkPPvvvvPPkk..",
				"....kkkkkkkk....",
			},
		},
		// 22..24: Particle spark burst
		{
			name: "spark_0", duration: 70,
			grid: []string{
				"................",
				"................",
				".......y........",
				".......y........",
				"......ywy.......",
				".....ywwwy......",
				"....ywwwwwy.....",
				"..yyywwwwwyyy...",
				"....ywwwwwy.....",
				".....ywwwy......",
				"......ywy.......",
				".......y........",
				".......y........",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "spark_1", duration: 70,
			grid: []string{
				"................",
				".......Y........",
				"...y...y...y....",
				"....y..w..y.....",
				".....Y...Y......",
				"................",
				"..Y.........Y...",
				".Yyw.......wyY..",
				"..Y.........Y...",
				"................",
				".....Y...Y......",
				"....y..w..y.....",
				"...y...y...y....",
				".......Y........",
				"................",
				"................",
			},
		},
		{
			name: "spark_2", duration: 70,
			grid: []string{
				"................",
				"..Y..........Y..",
				"................",
				"................",
				"................",
				"................",
				"................",
				".Y............Y.",
				"................",
				"................",
				"................",
				"................",
				"................",
				"..Y..........Y..",
				"................",
				"................",
			},
		},
		// 25..27: HUD icons
		{
			name: "heart_full", duration: 100,
			grid: []string{
				"................",
				"................",
				"..kkkk....kkkk..",
				".kppppk..krrrrk.",
				"kppwwrpkkrRRRRrk",
				"kpwrrrrrkrrrrRRk",
				"kprrrrrrrrrrrRRk",
				"krrrrrrrrrrrrRRk",
				".krrrrrrrrrrRRk.",
				"..krrrrrrrrRRk..",
				"...krrrrrrRRk...",
				"....krrrrRRk....",
				".....krrRRk.....",
				"......kRRk......",
				".......kk.......",
				"................",
			},
		},
		{
			name: "heart_empty", duration: 100,
			grid: []string{
				"................",
				"................",
				"..kkkk....kkkk..",
				".kddddk..kddddk.",
				"kddmmddkkddDDddk",
				"kdmdddddkddddDDk",
				"kddddddddddddDDk",
				"kddddddddddddDDk",
				".kddddddddddDDk.",
				"..kddddddddDDk..",
				"...kddddddDDk...",
				"....kddddDDk....",
				".....kddDDk.....",
				"......kDDk......",
				".......kk.......",
				"................",
			},
		},
		{
			name: "gem_icon", duration: 100,
			grid: []string{
				"................",
				"................",
				".....kkkkkk.....",
				"....kiiiiiCk....",
				"...kiwiiicCCk...",
				"..kiwiccccCCCk..",
				"..kiccccccCCCk..",
				"...kcccccCCCk...",
				"....kcccCCCk....",
				".....kcCCCk.....",
				"......kCCk......",
				".......kk.......",
				"................",
				"................",
				"................",
				"................",
			},
		},
	}
}

// -----------------------------------------------------------------------------
// 2. Cave Tileset (tileset.png — 64x32, 4x2 grid of 16x16 tiles)
// -----------------------------------------------------------------------------

func generateTileset(outDir string) error {
	const (
		cols     = 4
		rows     = 2
		tileSize = 16
	)
	img := image.NewNRGBA(image.Rect(0, 0, cols*tileSize, rows*tileSize))

	tiles := []struct {
		name string
		grid []string
	}{
		// Tile 0 (GID 1): solid cut cave stone
		{
			name: "stone_solid",
			grid: []string{
				"kkkkkkkkkkkkkkkk",
				"khhhhhhhkhhhhhhk",
				"khllllllkhlllllk",
				"khlmmmmlkhlmmmlk",
				"khlmmmmmkhlmmmlk",
				"khlmmddmkhlmddmk",
				"khddddddkhdddddk",
				"kkkkkkkkkkkkkkkk",
				"khhhkhhhhhhhkhhk",
				"khllkhllllllkhlk",
				"khlmkhlmmmmlkhlk",
				"khlmkhlmmmmmkhlk",
				"khlmkhlmmddmkhlk",
				"khddkhddddddkhdk",
				"kdddkddddddddkdk",
				"kkkkkkkkkkkkkkkk",
			},
		},
		// Tile 1 (GID 2): mossy top stone block
		{
			name: "stone_moss",
			grid: []string{
				"kggggggggggggggk",
				"kggGgggggGgggggk",
				"kgGGGgggGGGggGgk",
				"kGGkGGgGGkGGgGGk",
				"kGkhhkGGkhhhkGGk",
				"kkllllkklllllkGk",
				"khlmmmmlkhlmmmlk",
				"khlmmddmkhlmddmk",
				"khddddddkhdddddk",
				"kkkkkkkkkkkkkkkk",
				"khhhkhhhhhhhkhhk",
				"khllkhllllllkhlk",
				"khlmkhlmmmmlkhlk",
				"khlmkhlmmddmkhlk",
				"khddkhddddddkhdk",
				"kkkkkkkkkkkkkkkk",
			},
		},
		// Tile 2 (GID 3): one-way wooden bridge platform
		{
			name: "bridge",
			grid: []string{
				"kkkkkkkkkkkkkkkk",
				"ktttttktttttkttk",
				"kttbbbkttbbbkbtk",
				"kbbbbbkbBbbbkbbk",
				"kBBBBBkkBBBBkkBk",
				"kkkkkkkkkkkkkkkk",
				".kBBk......kBBk.",
				".kBbk......kbBk.",
				"..kk........kk..",
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
			},
		},
		// Tile 3 (GID 4): dark cave background wall
		{
			name: "wall_bg",
			grid: []string{
				"DDDDDDDDDDDDDDDD",
				"DddddddDDddddddD",
				"DddddddDDddddddD",
				"DddmdddDDddddddD",
				"DddddddDDddmdddD",
				"DddddddDDddddddD",
				"DDDDDDDDDDDDDDDD",
				"DdddDDddddddDDdD",
				"DdddDDddddddDDdD",
				"DdddDDddmdddDDdD",
				"DdddDDddddddDDdD",
				"DdddDDddddddDDdD",
				"DdddDDddddddDDdD",
				"DDDDDDDDDDDDDDDD",
				"DddddddDDddddddD",
				"DddddddDDddddddD",
			},
		},
		// Tile 4 (GID 5): dark cave background wall with carved stone arch/pillar
		{
			name: "wall_arch",
			grid: []string{
				"DDDDDDDDDDDDDDDD",
				"DDdmmmmmmmmmmddD",
				"DdmmDDDDDDDDmmdD",
				"DdmDDdmmmmDDdmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DdmDDmDDDDmDDmdD",
				"DDDDDDDDDDDDDDDD",
			},
		},
		// Tile 5 (GID 6): glowing cave crystal cluster on dark wall
		{
			name: "crystal_decor",
			grid: []string{
				"DDDDDDDDDDDDDDDD",
				"DddddddDDddddddD",
				"DddddddDDddddddD",
				"DddddddDDddddddD",
				"DddddddDDddddddD",
				"DddddddkdddddddD",
				"DdddddkikddkdddD",
				"DddkddkiCkkiCddD",
				"DdkikdkicCkiCkdD",
				"DdkiCkkicCkcCkdD",
				"DdkicCkccCkcCkdD",
				"DdkccCkccCkCCkdD",
				"DdkCCCkcCCkCCkdD",
				"DDkCCCkCCCkCCkDD",
				"DDkmmmmmmmmmmkDD",
				"DDDDDDDDDDDDDDDD",
			},
		},
		// Tile 6 (GID 7): animated lava frame 0
		{
			name: "lava_0",
			grid: []string{
				"................",
				"................",
				"..yy......yy....",
				".yyyy....yyyy...",
				"yoooyyyyooooyyyy",
				"oooooyyoooooooyy",
				"ooOooooooooOoooo",
				"oOOOoooyyooOOooo",
				"OOoOOooooooOOOoo",
				"OOOOOoooOoooOOOO",
				"OOoOOOOOOOooOOOO",
				"OOOOOOoOOOOOOOOO",
				"OOOoOOOOOOOoOOOO",
				"OOOOOOOOOOOOOOOO",
				"OOOOOOOOOOOOOOOO",
				"OOOOOOOOOOOOOOOO",
			},
		},
		// Tile 7 (GID 8): animated lava frame 1
		{
			name: "lava_1",
			grid: []string{
				"................",
				"................",
				"......yy......yy",
				".....yyyy....yyy",
				"yyyooooyyyyooooy",
				"oyoooooooyyooooo",
				"oooOooooooooOooo",
				"ooOOoooOooooOOoo",
				"oOOOOooyyooOOOOo",
				"OOOOOooooooOOOOO",
				"OOOOooOooooOOoOO",
				"OOOOOOoOOOOOOOOO",
				"OOOOOOOoOOOOoOOO",
				"OOOOOOOOOOOOOOOO",
				"OOOOOOOOOOOOOOOO",
				"OOOOOOOOOOOOOOOO",
			},
		},
	}

	for i, tile := range tiles {
		x := (i % cols) * tileSize
		y := (i / cols) * tileSize
		blitGrid(img, x, y, tile.name, tile.grid)
	}

	return writePNG(filepath.Join(outDir, "tileset.png"), img)
}

// -----------------------------------------------------------------------------
// 3. Tiled Map (level1.tmj — 40x14 orthogonal 16x16 map)
// -----------------------------------------------------------------------------

type tmjMap struct {
	CompressionLevel int          `json:"compressionlevel"`
	Height           int          `json:"height"`
	Infinite         bool         `json:"infinite"`
	Layers           []tmjLayer   `json:"layers"`
	NextLayerID      int          `json:"nextlayerid"`
	NextObjectID     int          `json:"nextobjectid"`
	Orientation      string       `json:"orientation"`
	RenderOrder      string       `json:"renderorder"`
	TiledVersion     string       `json:"tiledversion"`
	TileHeight       int          `json:"tileheight"`
	Tilesets         []tmjTileset `json:"tilesets"`
	TileWidth        int          `json:"tilewidth"`
	Type             string       `json:"type"`
	Version          string       `json:"version"`
	Width            int          `json:"width"`
}

type tmjLayer struct {
	Data      []int       `json:"data,omitempty"`
	DrawOrder string      `json:"draworder,omitempty"`
	Height    int         `json:"height,omitempty"`
	ID        int         `json:"id"`
	Name      string      `json:"name"`
	Objects   []tmjObject `json:"objects,omitempty"`
	Opacity   float64     `json:"opacity"`
	Type      string      `json:"type"`
	Visible   bool        `json:"visible"`
	Width     int         `json:"width,omitempty"`
	X         int         `json:"x"`
	Y         int         `json:"y"`
}

type tmjObject struct {
	Height     float64       `json:"height"`
	ID         int           `json:"id"`
	Name       string        `json:"name"`
	Properties []tmjProperty `json:"properties,omitempty"`
	Rotation   float64       `json:"rotation"`
	Type       string        `json:"type"`
	Visible    bool          `json:"visible"`
	Width      float64       `json:"width"`
	X          float64       `json:"x"`
	Y          float64       `json:"y"`
}

type tmjTileset struct {
	Columns     int       `json:"columns"`
	FirstGID    int       `json:"firstgid"`
	Image       string    `json:"image"`
	ImageHeight int       `json:"imageheight"`
	ImageWidth  int       `json:"imagewidth"`
	Margin      int       `json:"margin"`
	Name        string    `json:"name"`
	Spacing     int       `json:"spacing"`
	TileCount   int       `json:"tilecount"`
	TileHeight  int       `json:"tileheight"`
	Tiles       []tmjTile `json:"tiles"`
	TileWidth   int       `json:"tilewidth"`
}

type tmjTile struct {
	Animation  []tmjAnimFrame `json:"animation,omitempty"`
	ID         int            `json:"id"`
	Properties []tmjProperty  `json:"properties,omitempty"`
	Type       string         `json:"type,omitempty"`
}

type tmjAnimFrame struct {
	Duration int `json:"duration"`
	TileID   int `json:"tileid"`
}

type tmjProperty struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func generateLevel(outDir string) error {
	const (
		mapW     = 40
		mapH     = 14
		tileSize = 16
	)

	// Terrain layout: '#' solid stone (GID 1), '=' mossy stone (GID 2),
	// '-' one-way wooden bridge (GID 3), '~' animated lava (GID 7), '.' empty (0).
	terrainRows := []string{
		"########################################", // r00
		"####.......###..........###.........####", // r01
		"##....................................##", // r02
		"#......................................#", // r03
		"#......................................#", // r04
		"#......................................#", // r05
		"#......................................#", // r06
		"#..................................====#", // r07
		"#................-----.................#", // r08
		"#...............................---....#", // r09
		"#....----...--..........--.............#", // r10
		"#.........==....=======....=======.....#", // r11
		"#=========##....#######....#######=====#", // r12
		"############~~~~#######~~~~#############", // r13
	}

	if len(terrainRows) != mapH {
		return fmt.Errorf("terrain height %d, want %d", len(terrainRows), mapH)
	}

	terrain := make([]int, mapW*mapH)
	for y, row := range terrainRows {
		if len(row) != mapW {
			return fmt.Errorf("terrain row %d width %d, want %d", y, len(row), mapW)
		}
		for x, ch := range row {
			var gid int
			switch ch {
			case '#':
				gid = 1
			case '=':
				gid = 2
			case '-':
				gid = 3
			case '~':
				gid = 7
			case '.':
				gid = 0
			default:
				return fmt.Errorf("unknown terrain char %q at (%d,%d)", ch, x, y)
			}
			terrain[y*mapW+x] = gid
		}
	}

	// Background cave wall with carved arches (GID 5) and glowing crystals (GID 6).
	bg := make([]int, mapW*mapH)
	for i := range bg {
		bg[i] = 4 // wall_bg
	}
	archCols := []int{4, 8, 18, 20, 29, 36, 37}
	for _, col := range archCols {
		for y := 3; y <= 10; y++ {
			bg[y*mapW+col] = 5 // wall_arch
		}
	}
	crystals := [][2]int{
		{3, 11},
		{17, 10},
		{21, 10},
		{35, 11},
		{38, 11},
		{36, 6},
	}
	for _, c := range crystals {
		bg[c[1]*mapW+c[0]] = 6 // crystal_decor
	}

	objAt := func(id int, name, typ string, col, row int, props ...tmjProperty) tmjObject {
		// Self-check: objects must never spawn inside solid terrain.
		if g := terrain[row*mapW+col]; g == 1 || g == 2 || g == 7 {
			panic(fmt.Sprintf("object %q at (%d,%d) overlaps terrain GID %d", name, col, row, g))
		}
		return tmjObject{
			ID:         id,
			Name:       name,
			Type:       typ,
			X:          float64(col * tileSize),
			Y:          float64(row * tileSize),
			Width:      tileSize,
			Height:     tileSize,
			Visible:    true,
			Properties: props,
		}
	}

	objects := []tmjObject{
		objAt(1, "spawn", "spawn", 2, 11),
		objAt(2, "gem_1", "gem", 6, 9, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(3, "gem_2", "gem", 13, 9, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(4, "gem_3", "gem", 18, 7, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(5, "gem_4", "gem", 20, 7, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(6, "gem_5", "gem", 24, 9, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(7, "gem_6", "gem", 30, 9, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(8, "gem_7", "gem", 36, 11, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(9, "gem_8", "gem", 33, 8, tmjProperty{Name: "value", Type: "int", Value: 100}),
		objAt(10, "slime_1", "slime", 19, 10,
			tmjProperty{Name: "patrolMinX", Type: "float", Value: float64(16 * tileSize)},
			tmjProperty{Name: "patrolMaxX", Type: "float", Value: float64(22 * tileSize)},
			tmjProperty{Name: "speed", Type: "float", Value: 36.0},
		),
		objAt(11, "slime_2", "slime", 29, 10,
			tmjProperty{Name: "patrolMinX", Type: "float", Value: float64(27 * tileSize)},
			tmjProperty{Name: "patrolMaxX", Type: "float", Value: float64(32 * tileSize)},
			tmjProperty{Name: "speed", Type: "float", Value: 40.0},
		),
		objAt(12, "goal", "goal", 37, 6),
	}

	level := tmjMap{
		CompressionLevel: -1,
		Height:           mapH,
		Infinite:         false,
		NextLayerID:      4,
		NextObjectID:     len(objects) + 1,
		Orientation:      "orthogonal",
		RenderOrder:      "right-down",
		TiledVersion:     "1.10.2",
		TileHeight:       tileSize,
		TileWidth:        tileSize,
		Type:             "map",
		Version:          "1.10",
		Width:            mapW,
		Tilesets: []tmjTileset{
			{
				Columns:     4,
				FirstGID:    1,
				Image:       "tileset.png",
				ImageHeight: 32,
				ImageWidth:  64,
				Margin:      0,
				Name:        "cave_tiles",
				Spacing:     0,
				TileCount:   8,
				TileHeight:  tileSize,
				TileWidth:   tileSize,
				Tiles: []tmjTile{
					{
						ID:   0,
						Type: "solid",
						Properties: []tmjProperty{
							{Name: "solid", Type: "bool", Value: true},
						},
					},
					{
						ID:   1,
						Type: "solid",
						Properties: []tmjProperty{
							{Name: "solid", Type: "bool", Value: true},
						},
					},
					{
						ID:   2,
						Type: "oneWay",
						Properties: []tmjProperty{
							{Name: "oneWay", Type: "bool", Value: true},
						},
					},
					{
						ID:   6,
						Type: "hazard",
						Animation: []tmjAnimFrame{
							{TileID: 6, Duration: 250},
							{TileID: 7, Duration: 250},
						},
						Properties: []tmjProperty{
							{Name: "hazard", Type: "bool", Value: true},
						},
					},
					{
						ID:   7,
						Type: "hazard",
						Properties: []tmjProperty{
							{Name: "hazard", Type: "bool", Value: true},
						},
					},
				},
			},
		},
		Layers: []tmjLayer{
			{
				ID:      1,
				Name:    "background",
				Type:    "tilelayer",
				Width:   mapW,
				Height:  mapH,
				Opacity: 1,
				Visible: true,
				Data:    bg,
			},
			{
				ID:      2,
				Name:    "terrain",
				Type:    "tilelayer",
				Width:   mapW,
				Height:  mapH,
				Opacity: 1,
				Visible: true,
				Data:    terrain,
			},
			{
				ID:        3,
				Name:      "objects",
				Type:      "objectgroup",
				DrawOrder: "topdown",
				Opacity:   1,
				Visible:   true,
				Objects:   objects,
			},
		},
	}

	data, err := json.MarshalIndent(level, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(outDir, "level1.tmj"), data, 0o644)
}

// -----------------------------------------------------------------------------
// 4. 8x8 Pixel Bitmap Font (font.png + font.fnt in AngelCode BMFont text format)
// -----------------------------------------------------------------------------

func generateFont(outDir string) error {
	const (
		cols   = 16
		rows   = 6
		cellW  = 8
		cellH  = 8
		atlasW = cols * cellW // 128
		atlasH = rows * cellH // 48
	)

	img := image.NewNRGBA(image.Rect(0, 0, atlasW, atlasH))
	white := color.NRGBA{0xff, 0xff, 0xff, 0xff}

	var fnt strings.Builder
	fnt.WriteString("info face=\"CaveRunPixel\" size=8 bold=0 italic=0 charset=\"\" unicode=1 stretchH=100 smooth=0 aa=1 padding=0,0,0,0 spacing=1,1 outline=0\n")
	fmt.Fprintf(&fnt, "common lineHeight=%d base=7 scaleW=%d scaleH=%d pages=1 packed=0 alphaChnl=0 redChnl=4 greenChnl=4 blueChnl=4\n", cellH, atlasW, atlasH)
	fnt.WriteString("page id=0 file=\"font.png\"\n")
	fnt.WriteString("chars count=95\n")

	for code := 32; code <= 126; code++ {
		idx := code - 32
		cx := (idx % cols) * cellW
		cy := (idx / cols) * cellH

		glyph, ok := pixelFontGlyphs[rune(code)]
		if !ok {
			return fmt.Errorf("missing font glyph for ASCII %d (%q)", code, rune(code))
		}

		maxCol := -1
		for y, bits := range glyph {
			for x := 0; x < 8; x++ {
				if bits&(1<<uint(7-x)) != 0 {
					img.SetNRGBA(cx+x, cy+y, white)
					if x > maxCol {
						maxCol = x
					}
				}
			}
		}

		advance := 7
		switch {
		case code == ' ':
			advance = 5
		case code >= '0' && code <= '9':
			// Uniform digit width keeps score/timer readouts steady.
			advance = 7
		case maxCol >= 0:
			advance = maxCol + 2
			if advance < 4 {
				advance = 4
			}
			if advance > 8 {
				advance = 8
			}
		}

		fmt.Fprintf(&fnt,
			"char id=%d x=%d y=%d width=%d height=%d xoffset=0 yoffset=0 xadvance=%d page=0 chnl=15\n",
			code, cx, cy, cellW, cellH, advance,
		)
	}

	if err := writePNG(filepath.Join(outDir, "font.png"), img); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "font.fnt"), []byte(fnt.String()), 0o644)
}

// pixelFontGlyphs defines an original 8x8 pixel bitmap font for printable ASCII
// 32..126. Each uint8 is one row, bit 7 = leftmost pixel, bit 0 = rightmost.
var pixelFontGlyphs = map[rune][8]uint8{
	' ':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	'!':  {0x30, 0x30, 0x30, 0x30, 0x30, 0x00, 0x30, 0x00},
	'"':  {0x6c, 0x6c, 0x24, 0x00, 0x00, 0x00, 0x00, 0x00},
	'#':  {0x48, 0x48, 0xfc, 0x48, 0xfc, 0x48, 0x48, 0x00},
	'$':  {0x30, 0x7c, 0xc0, 0x78, 0x0c, 0xf8, 0x30, 0x00},
	'%':  {0xc4, 0xc8, 0x10, 0x20, 0x40, 0x98, 0x18, 0x00},
	'&':  {0x70, 0x88, 0x88, 0x70, 0xa8, 0x90, 0x68, 0x00},
	'\'': {0x30, 0x30, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00},
	'(':  {0x18, 0x30, 0x60, 0x60, 0x60, 0x30, 0x18, 0x00},
	')':  {0x60, 0x30, 0x18, 0x18, 0x18, 0x30, 0x60, 0x00},
	'*':  {0x00, 0x48, 0x30, 0xfc, 0x30, 0x48, 0x00, 0x00},
	'+':  {0x00, 0x30, 0x30, 0xfc, 0x30, 0x30, 0x00, 0x00},
	',':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x30, 0x60},
	'-':  {0x00, 0x00, 0x00, 0xfc, 0x00, 0x00, 0x00, 0x00},
	'.':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x60, 0x60, 0x00},
	'/':  {0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x00, 0x00},
	'0':  {0x78, 0xcc, 0xdc, 0xec, 0xcc, 0xcc, 0x78, 0x00},
	'1':  {0x30, 0x70, 0x30, 0x30, 0x30, 0x30, 0xfc, 0x00},
	'2':  {0x78, 0xcc, 0x0c, 0x38, 0x60, 0xc0, 0xfc, 0x00},
	'3':  {0x78, 0xcc, 0x0c, 0x38, 0x0c, 0xcc, 0x78, 0x00},
	'4':  {0x1c, 0x3c, 0x6c, 0xcc, 0xfe, 0x0c, 0x0c, 0x00},
	'5':  {0xfc, 0xc0, 0xf8, 0x0c, 0x0c, 0xcc, 0x78, 0x00},
	'6':  {0x38, 0x60, 0xc0, 0xf8, 0xcc, 0xcc, 0x78, 0x00},
	'7':  {0xfc, 0xcc, 0x0c, 0x18, 0x30, 0x30, 0x30, 0x00},
	'8':  {0x78, 0xcc, 0xcc, 0x78, 0xcc, 0xcc, 0x78, 0x00},
	'9':  {0x78, 0xcc, 0xcc, 0x7c, 0x0c, 0x18, 0x70, 0x00},
	':':  {0x00, 0x60, 0x60, 0x00, 0x60, 0x60, 0x00, 0x00},
	';':  {0x00, 0x30, 0x30, 0x00, 0x30, 0x30, 0x60, 0x00},
	'<':  {0x0c, 0x18, 0x30, 0x60, 0x30, 0x18, 0x0c, 0x00},
	'=':  {0x00, 0x00, 0xfc, 0x00, 0xfc, 0x00, 0x00, 0x00},
	'>':  {0x60, 0x30, 0x18, 0x0c, 0x18, 0x30, 0x60, 0x00},
	'?':  {0x78, 0xcc, 0x0c, 0x18, 0x30, 0x00, 0x30, 0x00},
	'@':  {0x7c, 0xc6, 0xde, 0xde, 0xdc, 0xc0, 0x7c, 0x00},
	'A':  {0x30, 0x78, 0xcc, 0xcc, 0xfc, 0xcc, 0xcc, 0x00},
	'B':  {0xf8, 0xcc, 0xcc, 0xf8, 0xcc, 0xcc, 0xf8, 0x00},
	'C':  {0x78, 0xcc, 0xc0, 0xc0, 0xc0, 0xcc, 0x78, 0x00},
	'D':  {0xf0, 0xd8, 0xcc, 0xcc, 0xcc, 0xd8, 0xf0, 0x00},
	'E':  {0xfc, 0xc0, 0xc0, 0xf8, 0xc0, 0xc0, 0xfc, 0x00},
	'F':  {0xfc, 0xc0, 0xc0, 0xf8, 0xc0, 0xc0, 0xc0, 0x00},
	'G':  {0x78, 0xcc, 0xc0, 0xdc, 0xcc, 0xcc, 0x7c, 0x00},
	'H':  {0xcc, 0xcc, 0xcc, 0xfc, 0xcc, 0xcc, 0xcc, 0x00},
	'I':  {0x78, 0x30, 0x30, 0x30, 0x30, 0x30, 0x78, 0x00},
	'J':  {0x3c, 0x18, 0x18, 0x18, 0xd8, 0xd8, 0x70, 0x00},
	'K':  {0xcc, 0xd8, 0xf0, 0xe0, 0xf0, 0xd8, 0xcc, 0x00},
	'L':  {0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xfc, 0x00},
	'M':  {0xc6, 0xee, 0xfe, 0xd6, 0xc6, 0xc6, 0xc6, 0x00},
	'N':  {0xcc, 0xec, 0xfc, 0xdc, 0xcc, 0xcc, 0xcc, 0x00},
	'O':  {0x78, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0x78, 0x00},
	'P':  {0xf8, 0xcc, 0xcc, 0xf8, 0xc0, 0xc0, 0xc0, 0x00},
	'Q':  {0x78, 0xcc, 0xcc, 0xcc, 0xdc, 0xd8, 0x74, 0x00},
	'R':  {0xf8, 0xcc, 0xcc, 0xf8, 0xd8, 0xcc, 0xcc, 0x00},
	'S':  {0x78, 0xcc, 0xc0, 0x78, 0x0c, 0xcc, 0x78, 0x00},
	'T':  {0xfc, 0x30, 0x30, 0x30, 0x30, 0x30, 0x30, 0x00},
	'U':  {0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0x78, 0x00},
	'V':  {0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0x78, 0x30, 0x00},
	'W':  {0xc6, 0xc6, 0xc6, 0xd6, 0xfe, 0xee, 0xc6, 0x00},
	'X':  {0xcc, 0xcc, 0x78, 0x30, 0x78, 0xcc, 0xcc, 0x00},
	'Y':  {0xcc, 0xcc, 0xcc, 0x78, 0x30, 0x30, 0x30, 0x00},
	'Z':  {0xfc, 0x0c, 0x18, 0x30, 0x60, 0xc0, 0xfc, 0x00},
	'[':  {0x78, 0x60, 0x60, 0x60, 0x60, 0x60, 0x78, 0x00},
	'\\': {0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x00, 0x00},
	']':  {0x78, 0x18, 0x18, 0x18, 0x18, 0x18, 0x78, 0x00},
	'^':  {0x30, 0x78, 0xcc, 0x00, 0x00, 0x00, 0x00, 0x00},
	'_':  {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xfc, 0x00},
	'`':  {0x60, 0x30, 0x18, 0x00, 0x00, 0x00, 0x00, 0x00},
	'a':  {0x00, 0x00, 0x78, 0x0c, 0x7c, 0xcc, 0x7c, 0x00},
	'b':  {0xc0, 0xc0, 0xf8, 0xcc, 0xcc, 0xcc, 0xf8, 0x00},
	'c':  {0x00, 0x00, 0x78, 0xcc, 0xc0, 0xcc, 0x78, 0x00},
	'd':  {0x0c, 0x0c, 0x7c, 0xcc, 0xcc, 0xcc, 0x7c, 0x00},
	'e':  {0x00, 0x00, 0x78, 0xcc, 0xfc, 0xc0, 0x78, 0x00},
	'f':  {0x38, 0x60, 0xf8, 0x60, 0x60, 0x60, 0x60, 0x00},
	'g':  {0x00, 0x00, 0x7c, 0xcc, 0xcc, 0x7c, 0x0c, 0x78},
	'h':  {0xc0, 0xc0, 0xf8, 0xcc, 0xcc, 0xcc, 0xcc, 0x00},
	'i':  {0x30, 0x00, 0x70, 0x30, 0x30, 0x30, 0x78, 0x00},
	'j':  {0x18, 0x00, 0x38, 0x18, 0x18, 0x98, 0x98, 0x70},
	'k':  {0xc0, 0xc0, 0xcc, 0xd8, 0xf0, 0xd8, 0xcc, 0x00},
	'l':  {0x70, 0x30, 0x30, 0x30, 0x30, 0x30, 0x78, 0x00},
	'm':  {0x00, 0x00, 0xec, 0xfe, 0xd6, 0xc6, 0xc6, 0x00},
	'n':  {0x00, 0x00, 0xf8, 0xcc, 0xcc, 0xcc, 0xcc, 0x00},
	'o':  {0x00, 0x00, 0x78, 0xcc, 0xcc, 0xcc, 0x78, 0x00},
	'p':  {0x00, 0x00, 0xf8, 0xcc, 0xcc, 0xf8, 0xc0, 0xc0},
	'q':  {0x00, 0x00, 0x7c, 0xcc, 0xcc, 0x7c, 0x0c, 0x0c},
	'r':  {0x00, 0x00, 0xdc, 0xe0, 0xc0, 0xc0, 0xc0, 0x00},
	's':  {0x00, 0x00, 0x7c, 0xc0, 0x78, 0x0c, 0xf8, 0x00},
	't':  {0x30, 0x30, 0xfc, 0x30, 0x30, 0x34, 0x18, 0x00},
	'u':  {0x00, 0x00, 0xcc, 0xcc, 0xcc, 0xcc, 0x7c, 0x00},
	'v':  {0x00, 0x00, 0xcc, 0xcc, 0xcc, 0x78, 0x30, 0x00},
	'w':  {0x00, 0x00, 0xc6, 0xc6, 0xd6, 0xfe, 0x6c, 0x00},
	'x':  {0x00, 0x00, 0xcc, 0x78, 0x30, 0x78, 0xcc, 0x00},
	'y':  {0x00, 0x00, 0xcc, 0xcc, 0xcc, 0x7c, 0x0c, 0x78},
	'z':  {0x00, 0x00, 0xfc, 0x18, 0x30, 0x60, 0xfc, 0x00},
	'{':  {0x1c, 0x30, 0x30, 0xe0, 0x30, 0x30, 0x1c, 0x00},
	'|':  {0x30, 0x30, 0x30, 0x30, 0x30, 0x30, 0x30, 0x00},
	'}':  {0xe0, 0x30, 0x30, 0x1c, 0x30, 0x30, 0xe0, 0x00},
	'~':  {0x00, 0x00, 0x74, 0xd8, 0x00, 0x00, 0x00, 0x00},
}

// -----------------------------------------------------------------------------
// 5. Synthesized Retro PCM WAV Audio (16-bit mono, 22050 Hz)
// -----------------------------------------------------------------------------

const sampleRate = 22050

func generateAudio(outDir string) error {
	files := []struct {
		name    string
		samples []float64
	}{
		{"sfx_jump.wav", synthJump()},
		{"sfx_gem.wav", synthGem()},
		{"sfx_hurt.wav", synthHurt()},
		{"sfx_win.wav", synthWin()},
		{"music_cave.wav", synthCaveMusic()},
	}
	for _, f := range files {
		if err := writeWAV(filepath.Join(outDir, f.name), sampleRate, f.samples); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	return nil
}

func pulseWave(phase, duty float64) float64 {
	frac := phase - math.Floor(phase)
	if frac < duty {
		return 1.0
	}
	return -1.0
}

func triangleWave(phase float64) float64 {
	frac := phase - math.Floor(phase)
	if frac < 0.5 {
		return 4.0*frac - 1.0
	}
	return 3.0 - 4.0*frac
}

func midiFreq(note int) float64 {
	return 440.0 * math.Pow(2.0, float64(note-69)/12.0)
}

func synthJump() []float64 {
	n := int(math.Round(0.18 * sampleRate))
	out := make([]float64, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		freq := 210.0 + 470.0*math.Pow(t, 0.72)
		phase += freq / sampleRate
		env := math.Pow(1.0-t, 1.4)
		if i < 60 {
			env *= float64(i) / 60.0
		}
		out[i] = 0.45 * env * pulseWave(phase, 0.25)
	}
	return out
}

func synthGem() []float64 {
	// Crisp ascending E-major sparkle arpeggio: E6 -> G#6 -> B6 -> E7
	notes := []int{88, 92, 95, 100}
	stepSamples := int(math.Round(0.055 * sampleRate))
	tailSamples := int(math.Round(0.11 * sampleRate))
	total := len(notes)*stepSamples + tailSamples
	out := make([]float64, total)

	for idx, note := range notes {
		start := idx * stepSamples
		dur := stepSamples + tailSamples
		if start+dur > total {
			dur = total - start
		}
		freq := midiFreq(note)
		phase := 0.0
		for i := 0; i < dur; i++ {
			t := float64(i) / float64(dur)
			phase += freq / sampleRate
			env := math.Exp(-5.5 * t)
			if i < 30 {
				env *= float64(i) / 30.0
			}
			sig := 0.65*triangleWave(phase) + 0.35*pulseWave(phase, 0.25)
			out[start+i] += 0.32 * env * sig
		}
	}
	return out
}

func synthHurt() []float64 {
	n := int(math.Round(0.26 * sampleRate))
	out := make([]float64, n)
	phase := 0.0
	var lfsr uint16 = 0xace1
	noise := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		freq := 230.0*math.Pow(1.0-t, 1.3) + 58.0
		phase += freq / sampleRate
		if i%6 == 0 {
			bit := ((lfsr >> 0) ^ (lfsr >> 2) ^ (lfsr >> 3) ^ (lfsr >> 5)) & 1
			lfsr = (lfsr >> 1) | (bit << 15)
			if lfsr&1 == 1 {
				noise = 1.0
			} else {
				noise = -1.0
			}
		}
		env := math.Pow(1.0-t, 1.2)
		if i < 40 {
			env *= float64(i) / 40.0
		}
		buzz := pulseWave(phase, 0.5)
		out[i] = 0.45 * env * (0.6*buzz + 0.4*noise)
	}
	return out
}

func synthWin() []float64 {
	// Upbeat 6-note victory fanfare (C5 E5 G5 C6 G5 C6) with bass harmony.
	type step struct {
		lead int
		bass int
		secs float64
	}
	phrase := []step{
		{72, 48, 0.11},
		{76, 52, 0.11},
		{79, 55, 0.11},
		{84, 60, 0.16},
		{79, 55, 0.11},
		{84, 48, 0.34},
	}

	var total int
	for _, s := range phrase {
		total += int(s.secs * sampleRate)
	}
	out := make([]float64, total)

	pos := 0
	for idx, s := range phrase {
		n := int(s.secs * sampleRate)
		leadFreq := midiFreq(s.lead)
		bassFreq := midiFreq(s.bass)
		leadPhase, bassPhase := 0.0, 0.0
		for i := 0; i < n && pos+i < total; i++ {
			t := float64(i) / float64(n)
			vib := 1.0
			if idx == len(phrase)-1 && t > 0.25 {
				vib = 1.0 + 0.008*math.Sin(2*math.Pi*6.5*float64(i)/sampleRate)
			}
			leadPhase += (leadFreq * vib) / sampleRate
			bassPhase += bassFreq / sampleRate

			env := 1.0 - 0.55*t
			if idx == len(phrase)-1 {
				env = math.Pow(1.0-t, 1.35)
			}
			if i < 50 {
				env *= float64(i) / 50.0
			}
			if n-i < 80 {
				env *= float64(n-i) / 80.0
			}

			lead := pulseWave(leadPhase, 0.25)
			bass := triangleWave(bassPhase)
			out[pos+i] = 0.38*env*lead + 0.28*env*bass
		}
		pos += n
	}
	return out
}

func synthCaveMusic() []float64 {
	// Seamless 4-bar D-Dorian cavern loop at 150 BPM (32 eighth-note steps = 6.4s).
	const (
		steps      = 32
		stepLength = 0.20 // seconds per eighth note
	)
	stepSamples := int(math.Round(stepLength * sampleRate))
	total := steps * stepSamples
	out := make([]float64, total)

	// 0 = rest.
	leadNotes := [steps]int{
		74, 0, 77, 79, 81, 0, 79, 77, // Bar 1: D5 . F5 G5 A5 . G5 F5
		76, 0, 74, 72, 74, 0, 69, 0, //  Bar 2: E5 . D5 C5 D5 . A4 .
		70, 0, 74, 77, 76, 0, 74, 72, // Bar 3: Bb4 . D5 F5 E5 . D5 C5
		74, 76, 77, 76, 74, 0, 69, 0, // Bar 4: D5 E5 F5 E5 D5 . A4 .
	}
	arpNotes := [steps]int{
		62, 65, 69, 65, 62, 65, 69, 65, // Dm
		57, 60, 64, 60, 57, 60, 64, 60, // Am
		58, 62, 65, 62, 58, 62, 65, 62, // Bb
		60, 64, 67, 64, 57, 61, 64, 69, // C -> A7 turnaround
	}
	bassNotes := [steps]int{
		50, 0, 45, 0, 50, 0, 53, 0, // D3 A2 D3 F3
		45, 0, 48, 0, 45, 0, 43, 0, // A2 C3 A2 G2
		46, 0, 50, 0, 46, 0, 48, 0, // Bb2 D3 Bb2 C3
		48, 0, 52, 0, 45, 0, 45, 0, // C3 E3 A2 A2
	}

	leadPhase, arpPhase, bassPhase := 0.0, 0.0, 0.0
	for s := 0; s < steps; s++ {
		base := s * stepSamples
		lNote := leadNotes[s]
		aNote := arpNotes[s]
		bNote := bassNotes[s]

		for i := 0; i < stepSamples; i++ {
			t := float64(i) / float64(stepSamples)
			mix := 0.0

			if lNote > 0 {
				leadPhase += midiFreq(lNote) / sampleRate
				env := math.Pow(1.0-t, 0.75)
				if i < 60 {
					env *= float64(i) / 60.0
				}
				if stepSamples-i < 80 {
					env *= float64(stepSamples-i) / 80.0
				}
				mix += 0.24 * env * pulseWave(leadPhase, 0.25)
			}

			if aNote > 0 {
				arpPhase += midiFreq(aNote) / sampleRate
				env := math.Exp(-4.0 * t)
				if i < 30 {
					env *= float64(i) / 30.0
				}
				mix += 0.12 * env * pulseWave(arpPhase, 0.125)
			}

			if bNote > 0 {
				bassPhase += midiFreq(bNote) / sampleRate
				env := math.Pow(1.0-t, 0.55)
				if i < 50 {
					env *= float64(i) / 50.0
				}
				if stepSamples-i < 100 {
					env *= float64(stepSamples-i) / 100.0
				}
				mix += 0.30 * env * triangleWave(bassPhase)
			}

			out[base+i] = mix
		}
	}

	// Micro-fade loop boundaries (2 ms) so looping never clicks.
	fade := int(math.Round(0.002 * sampleRate))
	for i := 0; i < fade && i < total/2; i++ {
		g := float64(i) / float64(fade)
		out[i] *= g
		out[total-1-i] *= g
	}
	return out
}

func writeWAV(path string, rate uint32, samples []float64) error {
	const (
		channels      uint16 = 1
		bitsPerSample uint16 = 16
	)
	dataSize := uint32(len(samples) * 2)
	byteRate := rate * uint32(channels) * uint32(bitsPerSample/8)
	blockAlign := channels * (bitsPerSample / 8)

	var buf bytes.Buffer
	buf.Grow(44 + int(dataSize))

	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, channels)
	_ = binary.Write(&buf, binary.LittleEndian, rate)
	_ = binary.Write(&buf, binary.LittleEndian, byteRate)
	_ = binary.Write(&buf, binary.LittleEndian, blockAlign)
	_ = binary.Write(&buf, binary.LittleEndian, bitsPerSample)

	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)

	for _, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		v := int16(math.Round(s * 32767.0))
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

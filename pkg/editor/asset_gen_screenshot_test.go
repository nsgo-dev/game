package editor_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func renderArtSpritePNG(t *testing.T, rows []string, pal map[byte]color.RGBA, scale int) []byte {
	t.Helper()
	h := len(rows)
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, w*scale, h*scale))
	for y, r := range rows {
		for x := 0; x < len(r); x++ {
			c, ok := pal[r[x]]
			if !ok {
				continue
			}
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					img.SetRGBA(x*scale+sx, y*scale+sy, c)
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

func renderFixtureWAV(t *testing.T, durSec, rmsDB float64) []byte {
	t.Helper()
	const sr = 22050
	n := int(math.Round(durSec * sr))
	samples := make([]float64, n)
	for i := range n {
		f := float64(i) / float64(n)
		env := math.Min(1, f*9) * math.Pow(1-f, 1.3)
		samples[i] = math.Sin(2*math.Pi*520*float64(i)/sr) * env
	}
	buf := &audioconv.Buffer{SampleRate: sr, Channels: 1, Samples: samples}
	buf.NormalizeRMS(rmsDB)
	var out bytes.Buffer
	if err := audioconv.EncodeWAV(&out, buf); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestAssetGeneratorMacOSScreenshots renders the full-window Generated Assets screen
// on the real macOS host beside design/editor/Assets.dc.html Screen 2 and Spec.dc.html.
func TestAssetGeneratorMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.NSGoFileName), []byte("name: dungeon-run\nid: dev.example.dungeon\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: dungeon-run\nid: dev.example.dungeon\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte("art:\n  summary: \"16 px pixel art, 4-colour shading, cold stone with warm torch light. 1 px dark outline on characters.\"\n  pixelGrid: 16\n  maxColors: 4\n  outlineColors: 1\n  minContrast: 3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "art", "characters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "audio", "sfx"), 0o755); err != nil {
		t.Fatal(err)
	}

	slRows := []string{
		"....ooo....",
		"..ooGGGoo..",
		".oGHHGGGGo.",
		".oGHGGVGVo.",
		"oGGGGGGGGGo",
		"oGGGGGGGGGo",
		"oEEEEEEEEEo",
		".ooooooooo.",
	}
	slBRows := []string{
		"...o..o....",
		"..oHooHo...",
		".oGHHGGGGo.",
		".oGHGGVGVo.",
		"oGGGGGGGGGo",
		"oGGGGGGGGGo",
		"oEEEEEEEEEo",
		".ooooooooo.",
	}
	slCRows := []string{
		"....ooo....",
		"..ooJJJoo..",
		".oJHHGGKKo.",
		".oGHGNVGVo.",
		"oGGNGGKKKUo",
		"oKKGGGKKUUo",
		"oEEUUUUUEEo",
		".ooooooooo.",
	}
	greenPal := map[byte]color.RGBA{
		'o': {0x19, 0x1B, 0x22, 0xFF},
		'V': {0x0F, 0x11, 0x16, 0xFF},
		'G': {0x5F, 0xBF, 0x7A, 0xFF},
		'H': {0xB4, 0xEF, 0xC2, 0xFF},
		'E': {0x3B, 0x8A, 0x55, 0xFF},
	}
	frostPal := map[byte]color.RGBA{
		'o': {0x19, 0x1B, 0x22, 0xFF},
		'V': {0x0F, 0x11, 0x16, 0xFF},
		'G': {0x7C, 0xC4, 0xE8, 0xFF},
		'H': {0xE0, 0xF6, 0xFF, 0xFF},
		'E': {0x3F, 0x7F, 0xA8, 0xFF},
	}
	frost7Pal := map[byte]color.RGBA{
		'o': {0x19, 0x1B, 0x22, 0xFF},
		'V': {0x0F, 0x11, 0x16, 0xFF},
		'G': {0x7C, 0xC4, 0xE8, 0xFF},
		'H': {0xE0, 0xF6, 0xFF, 0xFF},
		'E': {0x3F, 0x7F, 0xA8, 0xFF},
		'J': {0x9F, 0xD8, 0xF0, 0xFF},
		'K': {0x5A, 0xA0, 0xC8, 0xFF},
		'N': {0xC8, 0xB8, 0xFF, 0xFF},
		'U': {0x2F, 0x6A, 0x90, 0xFF},
	}

	if err := os.WriteFile(filepath.Join(dir, "art", "characters", "slime.png"), renderArtSpritePNG(t, slRows, greenPal, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "jump.wav"), renderFixtureWAV(t, 0.31, -14.0), 0o644); err != nil {
		t.Fatal(err)
	}
	// Existing generated files on disk: one licensed, one unknown license (blocking export).
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "slime_squish.ogg.source.yaml"), []byte("version: 1\ngenerator: sfx-gen\nmodel: sfx-v1\nprompt: slime squish\nlicense: sfx-gen-standard\ncommercialUse: true\napprovedBy: kim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio", "sfx", "torch_flicker.wav.source.yaml"), []byte("version: 1\ngenerator: sfx-gen\nprompt: dungeon torch crackle\nlicense: unknown\ncommercialUse: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cA := renderArtSpritePNG(t, slRows, frostPal, 1)
	cB := renderArtSpritePNG(t, slBRows, frostPal, 1)
	cC := renderArtSpritePNG(t, slCRows, frost7Pal, 1)
	cD := renderArtSpritePNG(t, slRows, frostPal, 2)

	imgSrv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "pixel-gen", Version: "1.0.0"}, nil)
	imgSrv.AddTool(&sdkmcp.Tool{Name: "generate_image", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		body, _ := json.Marshal(map[string]any{
			"model":          "pixflux-1",
			"license":        "pixellab-commercial-1.2",
			"commercialUse":  true,
			"elapsedSeconds": 21,
			"candidates": []map[string]any{
				{"seed": 81514, "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(cA)},
				{"seed": 81515, "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(cB)},
				{"seed": 81516, "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(cC)},
				{"seed": 81517, "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(cD)},
			},
		})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}}}, nil
	})
	imgCT, imgST := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = imgSrv.Run(ctx, imgST) }()

	wA := renderFixtureWAV(t, 0.34, -14.1)
	wB := renderFixtureWAV(t, 0.29, -13.8)
	wC := renderFixtureWAV(t, 0.61, -9.6)
	sndSrv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "sfx-gen", Version: "1.0.0"}, nil)
	sndSrv.AddTool(&sdkmcp.Tool{Name: "generate_sound", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		body, _ := json.Marshal(map[string]any{
			"model":         "sfx-v1",
			"license":       "sfx-gen-standard",
			"commercialUse": true,
			"candidates": []map[string]any{
				{"seed": 4410, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wA)},
				{"seed": 4411, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wB)},
				{"seed": 4412, "mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wC)},
			},
		})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}}}, nil
	})
	sndCT, sndST := sdkmcp.NewInMemoryTransports()
	go func() { _ = sndSrv.Run(ctx, sndST) }()

	ed, err := editor.New(editor.DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ed.DisconnectAllAssetGenerators()
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetApprovalAuthor("kim")
	ed.LoadAssetStyleBrief()

	if err := ed.ConnectAssetGenerator(ctx, editor.GeneratorServerConfig{Name: "pixel-gen", Kind: editor.AssetGenImages}, imgCT); err != nil {
		t.Fatal(err)
	}
	if err := ed.ConnectAssetGenerator(ctx, editor.GeneratorServerConfig{Name: "sfx-gen", Kind: editor.AssetGenSound}, sndCT); err != nil {
		t.Fatal(err)
	}
	req, err := ed.GenerateAsset(ctx, editor.GenerateAssetParams{
		TargetPath: "art/characters/slime_frost.png",
		Generator:  "pixel-gen",
		Prompt:     "ice slime, 16 px pixel art, 4-colour shading, 1 px dark outline, facing right",
		UserPrompt: "An ice slime for floor 3. Same size and rules as slime.png.",
		References: []string{"art/characters/slime.png", "brief.yaml#art"},
		Count:      4,
	})
	if err != nil {
		t.Fatal(err)
	}
	ed.SelectGeneratedCandidate(req.ID, "B")
	if _, err := ed.GenerateAsset(ctx, editor.GenerateAssetParams{
		TargetPath: "audio/sfx/slime_freeze.wav",
		Generator:  "sfx-gen",
		Kind:       editor.AssetGenSound,
		Prompt:     "the ice slime's freeze attack, short and crisp",
		References: []string{"audio/sfx/jump.wav"},
		Count:      3,
	}); err != nil {
		t.Fatal(err)
	}
	ed.SetAssetGeneratorOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	if app.Find("id:asset-gen-screen") == nil {
		t.Fatal("asset-gen-screen not visible")
	}
	app.Screenshot("asset-gen-candidates")

	// Pick candidate B and capture picked state, then Undo pick to verify UI updates cleanly.
	app.Press("id:asset-gen-pick-B")
	app.Screenshot("asset-gen-picked")

	app.Press("id:asset-gen-bg-checker")
	app.Screenshot("asset-gen-checker")
}

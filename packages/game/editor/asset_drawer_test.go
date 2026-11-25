package editor_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func makeTestPNG(t *testing.T, w, h int, col color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := col
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				c = color.RGBA{R: 0x19, G: 0x1B, B: 0x22, A: 0xFF}
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

func makeTestWAV(t *testing.T, sampleRate int, durSec float64, freq float64) []byte {
	t.Helper()
	numSamples := int(float64(sampleRate) * durSec)
	dataSize := numSamples * 2
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	for i := range numSamples {
		env := 1.0 - float64(i)/float64(max(1, numSamples))
		s := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * env * 18000)
		_ = binary.Write(&buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

func writeAssetDrawerFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile := func(rel string, data []byte) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeFile("game.yaml", []byte(`version: 1
name: DungeonRun
id: dev.nsgo.dungeon
appVersion: 0.1.0
window:
  width: 640
  height: 360
assets:
  textures:
    - in: art/props/coin.png
      out: built/coin.ktx2
  audio:
    - in: audio/sfx/jump.wav
      out: built/jump.ogg
  fonts:
    - in: fonts/PixelOperator.ttf
      out: built/pixel.fnt
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`))

	writeFile("scenes/level_01.scene.yaml", []byte(`version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: Actors
    space: world
    order: 0
    objects:
      - id: hero
        name: Knight
        x: 160
        y: 160
        sprite:
          atlas: sheet
          frame: slime
      - id: door
        name: Door
        x: 420
        y: 160
        material: materials/door.material.yaml
`))

	writeFile("materials/door.material.yaml", []byte(`type: material
version: 1
name: Door
texture: art/props/door_open.png
`))

	writeFile("screens/hud.screen.yaml", []byte(`version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: hearts
    type: sprite_row
    sprite: art/ui/heart.png
    spriteCount: 3
`))

	writeFile("tilesets/stone.tileset.yaml", []byte(`version: 1
tileWidth: 16
tileHeight: 16
image: art/tiles/stone_tiles.png
`))

	// Atlas JSON referencing sheet.png with knight (multi-frame clips), slime, and coin frames.
	writeFile("art/sheet.png", makeTestPNG(t, 64, 32, color.RGBA{R: 0x5F, G: 0xBF, B: 0x7A, A: 0xFF}))
	writeFile("art/sheet.json", []byte(`{
  "frames": {
    "knight_run_0": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}},
    "knight_run_1": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}},
    "knight_idle_0": {"frame": {"x": 32, "y": 0, "w": 16, "h": 16}},
    "slime": {"frame": {"x": 0, "y": 16, "w": 16, "h": 16}},
    "coin": {"frame": {"x": 16, "y": 16, "w": 16, "h": 16}}
  },
  "meta": {"image": "sheet.png", "size": {"w": 64, "h": 32}}
}`))

	writeFile("art/characters/knight.png", makeTestPNG(t, 16, 16, color.RGBA{R: 0xD7, G: 0xDE, B: 0xEA, A: 0xFF}))
	writeFile("art/characters/slime.png", makeTestPNG(t, 16, 12, color.RGBA{R: 0x5F, G: 0xBF, B: 0x7A, A: 0xFF}))
	writeFile("art/props/coin.png", makeTestPNG(t, 16, 16, color.RGBA{R: 0xF2, G: 0xC1, B: 0x4E, A: 0xFF}))
	writeFile("art/props/torch_old.png", makeTestPNG(t, 16, 16, color.RGBA{R: 0xE9, G: 0xA8, B: 0x4A, A: 0xFF}))
	writeFile("art/tiles/stone_tiles.png", makeTestPNG(t, 32, 32, color.RGBA{R: 0x3B, G: 0x42, B: 0x50, A: 0xFF}))
	writeFile("art/ui/heart.png", makeTestPNG(t, 16, 16, color.RGBA{R: 0xFF, G: 0x6B, B: 0x6B, A: 0xFF}))

	writeFile("audio/sfx/jump.wav", makeTestWAV(t, 22050, 0.31, 440))
	writeFile("audio/sfx/slime_squish.wav", makeTestWAV(t, 22050, 0.27, 220))
	writeFile("audio/sfx/slime_squish.wav.source.yaml", []byte("version: 1\ngenerator: sfx-gen\n"))
	writeFile("audio/sfx/unused_chime.wav", makeTestWAV(t, 22050, 0.18, 880))

	// Empty folder audio/music for empty-folder state.
	if err := os.MkdirAll(filepath.Join(dir, "audio", "music"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile("fonts/PixelOperator.ttf", []byte("font-placeholder"))

	// Make cutscenes/intro.cutscene.yaml reference slime_squish.wav and a missing sound missing_hit.wav.
	writeFile("cutscenes/intro.cutscene.yaml", []byte(`version: 1
name: intro
scene: scenes/level_01.scene.yaml
duration: 2.0
tracks:
  - id: sfx
    kind: sound
    clips:
      - id: squish
        start: 0.2
        duration: 0.3
        source: audio/sfx/slime_squish.wav
      - id: hit
        start: 0.8
        duration: 0.2
        source: audio/sfx/missing_hit.wav
`))

	return dir
}

// TestAssetDrawerDragDropPlaceSwapUndoAndEscape verifies Acceptance Criterion #1:
// dragging a sprite onto empty space places it snapped on the active layer in one undo step,
// dropping it onto an existing object swaps that object's sprite and ONE Undo restores the old sprite,
// and Escape cancels an active drag cleanly.
func TestAssetDrawerDragDropPlaceSwapUndoAndEscape(t *testing.T) {
	dir := writeAssetDrawerFixture(t)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	ed.SetCanvasSize(640, 360)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	// Switch left rail from Scene to Assets.
	app.Press("id:rail-mode-assets")
	app.Settle()
	if ed.LeftRailMode() != editor.LeftRailAssets {
		t.Fatalf("LeftRailMode = %v, want LeftRailAssets", ed.LeftRailMode())
	}

	// 1) Drag coin.png onto empty viewport space -> placed on Actors, snapped to 16 px, in 1 undo step.
	emptyX, emptyY := ed.WorldToCanvas(247, 119)
	app.Post(func() {
		if err := ed.BeginAssetDrag("art/props/coin.png"); err != nil {
			t.Errorf("BeginAssetDrag: %v", err)
			return
		}
		ed.UpdateAssetDrag(emptyX, emptyY)
	})
	app.Settle()
	dragSt := ed.AssetDragState()
	if !dragSt.Active || !dragSt.HoveringViewport || dragSt.PillName != "coin.png" ||
		!strings.Contains(dragSt.PillDetail, "new Sprite on Actors · snapped to 16 px") {
		t.Fatalf("unexpected drag preview state: %+v", dragSt)
	}
	if !app.Exists("id:asset-drag-pill") {
		t.Fatal("expected asset-drag-pill overlay while dragging over viewport")
	}

	var placedID string
	var swapped bool
	app.Post(func() {
		var derr error
		placedID, swapped, derr = ed.DropAssetAt("art/props/coin.png", emptyX, emptyY)
		if derr != nil {
			t.Errorf("DropAssetAt empty: %v", derr)
		}
	})
	app.Settle()
	if swapped || placedID == "" {
		t.Fatalf("DropAssetAt empty: id=%q swapped=%v, want new object", placedID, swapped)
	}
	placed, _, _ := ed.FindObject(placedID)
	if placed == nil || placed.Sprite == nil || placed.Sprite.Frame != "coin" {
		t.Fatalf("placed object = %+v, want coin sprite", placed)
	}
	if int(placed.X)%16 != 0 || int(placed.Y)%16 != 0 {
		t.Fatalf("placed position = (%.1f, %.1f), want snapped to 16 px", placed.X, placed.Y)
	}

	// One Undo removes the placed object.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if obj, _, _ := ed.FindObject(placedID); obj != nil {
		t.Fatalf("expected 1 Undo to remove placed object %q", placedID)
	}

	// 2) Drag coin.png onto existing object "hero" (currently wearing "slime") -> swaps sprite in ONE undo step.
	heroBefore, _, _ := ed.FindObject("hero")
	if heroBefore == nil || heroBefore.Sprite == nil || heroBefore.Sprite.Frame != "slime" {
		t.Fatalf("hero before swap = %+v, want slime sprite", heroBefore)
	}
	heroCX, heroCY := ed.WorldToCanvas(heroBefore.X+4, heroBefore.Y+4)
	app.Post(func() {
		var derr error
		placedID, swapped, derr = ed.DropAssetAt("art/props/coin.png", heroCX, heroCY)
		if derr != nil {
			t.Errorf("DropAssetAt hero: %v", derr)
		}
	})
	app.Settle()
	if !swapped || placedID != "hero" {
		t.Fatalf("DropAssetAt hero: id=%q swapped=%v, want hero swapped=true", placedID, swapped)
	}
	heroAfter, _, _ := ed.FindObject("hero")
	if heroAfter == nil || heroAfter.Sprite == nil || heroAfter.Sprite.Frame != "coin" {
		t.Fatalf("hero after swap = %+v, want coin sprite", heroAfter)
	}

	// ONE Undo restores the exact old sprite ("slime").
	app.Post(func() { ed.Undo() })
	app.Settle()
	heroUndone, _, _ := ed.FindObject("hero")
	if heroUndone == nil || heroUndone.Sprite == nil || heroUndone.Sprite.Frame != "slime" {
		t.Fatalf("hero after 1 Undo = %+v, want restored slime sprite", heroUndone)
	}

	// 3) Escape cancels an active drag cleanly without mutating the scene.
	app.Post(func() {
		if err := ed.BeginAssetDrag("art/props/coin.png"); err != nil {
			t.Errorf("BeginAssetDrag for Esc: %v", err)
		}
		ed.UpdateAssetDrag(emptyX, emptyY)
	})
	app.Settle()
	if !ed.AssetDragActive() {
		t.Fatal("expected AssetDragActive before Escape")
	}
	app.Key("escape")
	app.Settle()
	if ed.AssetDragActive() {
		t.Fatal("expected Escape to cancel active asset drag")
	}
}

// TestAssetDrawerUnusedAndMissingMatchRefsExactly verifies Acceptance Criterion #2:
// Unused and Missing files list the exact assets refs finds unreferenced or missing in a fixture project,
// USED BY lists referring files from refs, unused shows the deletion-ask message,
// and missing offers Replace with a file + Ask the agent.
func TestAssetDrawerUnusedAndMissingMatchRefsExactly(t *testing.T) {
	dir := writeAssetDrawerFixture(t)
	scan, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatalf("refs.Scan: %v", err)
	}
	wantUnused := scan.UnreferencedAssets()
	wantMissing := scan.MissingAssets()
	if len(wantUnused) == 0 || len(wantMissing) == 0 {
		t.Fatalf("fixture sanity check failed: unused=%v missing=%v", wantUnused, wantMissing)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	var askedPath, askedPrompt string
	ed.OnAssetAskAgent = func(assetPath, prompt string) {
		askedPath, askedPrompt = assetPath, prompt
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Press("id:rail-mode-assets")
	app.Settle()

	// Verify SHOW ONLY -> Unused matches refs.Project.UnreferencedAssets() exactly.
	app.Press("id:asset-filter-unused")
	app.Settle()
	unusedSnap := ed.AssetDrawerSnapshot()
	gotUnused := make([]string, 0, len(unusedSnap.VisibleItems))
	for _, it := range unusedSnap.VisibleItems {
		gotUnused = append(gotUnused, it.Path)
	}
	slices.Sort(gotUnused)
	if !slices.Equal(gotUnused, wantUnused) {
		t.Fatalf("Unused filter = %v, want exact refs.UnreferencedAssets() %v", gotUnused, wantUnused)
	}

	// Select an unused asset and verify the exact unused message.
	app.Post(func() { ed.SelectAssetPath("art/props/torch_old.png") })
	app.Settle()
	emptyRefsNode := app.Find("id:asset-used-by-empty")
	const wantUnusedMsg = "Nothing references it. The agent can propose deleting it; deleting files always asks you."
	if emptyRefsNode.Text != wantUnusedMsg {
		t.Fatalf("unused USED BY text = %q, want %q", emptyRefsNode.Text, wantUnusedMsg)
	}

	// Verify SHOW ONLY -> Missing files matches refs.Project.MissingAssets() exactly.
	app.Press("id:asset-filter-missing")
	app.Settle()
	missingSnap := ed.AssetDrawerSnapshot()
	gotMissing := make([]string, 0, len(missingSnap.VisibleItems))
	for _, it := range missingSnap.VisibleItems {
		gotMissing = append(gotMissing, it.Path)
	}
	slices.Sort(gotMissing)
	if !slices.Equal(gotMissing, wantMissing) {
		t.Fatalf("Missing files filter = %v, want exact refs.MissingAssets() %v", gotMissing, wantMissing)
	}

	// Select missing door_open.png -> verify USED BY matches refs.UsedBy, and Replace + Ask agent actions exist.
	app.Post(func() { ed.SelectAssetPath("art/props/door_open.png") })
	app.Settle()
	if !app.Exists("id:asset-missing-replace") || !app.Exists("id:asset-missing-ask-agent") {
		t.Fatal("missing asset inspector must offer Replace with a file and Ask the agent")
	}
	usedSection := allText(app.Find("id:asset-used-by-section"))
	for _, r := range scan.UsedBy("art/props/door_open.png") {
		if !strings.Contains(usedSection, r.File) {
			t.Fatalf("USED BY missing referring file %q in %q", r.File, usedSection)
		}
	}

	// Press Ask the agent on the missing asset.
	app.Press("id:asset-missing-ask-agent")
	app.Settle()
	if askedPath != "art/props/door_open.png" || askedPrompt == "" {
		t.Fatalf("Ask the agent callback = (%q, %q)", askedPath, askedPrompt)
	}

	// Replace the missing asset with a file and verify Missing count drops by 1.
	replFile := filepath.Join(t.TempDir(), "door_replacement.png")
	if err := os.WriteFile(replFile, makeTestPNG(t, 16, 16, color.RGBA{R: 0x6B, G: 0x5A, B: 0x45, A: 0xFF}), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		if err := ed.ReplaceMissingAsset("art/props/door_open.png", replFile); err != nil {
			t.Errorf("ReplaceMissingAsset: %v", err)
		}
	})
	app.Settle()
	afterSnap := ed.AssetDrawerSnapshot()
	if afterSnap.Counts.Missing != len(wantMissing)-1 {
		t.Fatalf("Missing count after replace = %d, want %d", afterSnap.Counts.Missing, len(wantMissing)-1)
	}
}

func TestAssetDrawerFoldersKindsFiltersEmptyStatesAndPreviews(t *testing.T) {
	dir := writeAssetDrawerFixture(t)
	// Give coin.png a newer mtime than knight.png so recently-changed sort is deterministic.
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, "art", "characters", "knight.png"), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(filepath.Join(dir, "art", "props", "coin.png"), now.Add(-1*time.Minute), now.Add(-1*time.Minute))

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Press("id:rail-mode-assets")
	app.Settle()

	// Footer reports "N assets · M shown" and missing file problem.
	footerCount := app.Find("id:asset-drawer-footer-count").Text
	if !strings.Contains(footerCount, "assets · ") || !strings.Contains(footerCount, " shown") {
		t.Fatalf("unexpected footer count: %q", footerCount)
	}
	footerProb := allText(app.Find("id:asset-drawer-footer-problem"))
	if !strings.Contains(footerProb, "missing") {
		t.Fatalf("expected missing problem in footer, got %q", footerProb)
	}

	// Generated filter finds slime_squish.wav (backed by .source.yaml).
	app.Press("id:asset-filter-generated")
	app.Settle()
	genSnap := ed.AssetDrawerSnapshot()
	if len(genSnap.VisibleItems) != 1 || genSnap.VisibleItems[0].Path != "audio/sfx/slime_squish.wav" {
		t.Fatalf("Generated filter = %+v, want only slime_squish.wav", genSnap.VisibleItems)
	}
	app.Press("id:asset-filter-generated") // toggle off
	app.Settle()

	// Empty folder state: clicking audio/music shows exact empty folder prompt.
	app.Press("id:asset-folder-audio-music")
	app.Settle()
	if !app.Exists("id:asset-drawer-empty-folder") {
		t.Fatal("expected empty-folder state for audio/music")
	}
	emptyHint := app.Find("id:asset-drawer-empty-hint").Text
	wantEmptyFolderHint := "audio/music is empty · Drop .ogg or .wav files here, or ask the agent."
	if emptyHint != wantEmptyFolderHint {
		t.Fatalf("empty folder hint = %q, want %q", emptyHint, wantEmptyFolderHint)
	}

	// Return to All assets and test Kind tabs + search no-match state.
	app.Press("id:asset-folder-all")
	app.Settle()
	app.Press("id:asset-kind-fonts")
	app.Settle()
	if snap := ed.AssetDrawerSnapshot(); len(snap.VisibleItems) != 1 || snap.VisibleItems[0].Name != "PixelOperator.ttf" {
		t.Fatalf("Fonts tab = %+v, want PixelOperator.ttf", snap.VisibleItems)
	}
	app.Type("id:asset-drawer-search", "zzzz-no-match")
	app.Settle()
	if !app.Exists("id:asset-drawer-empty-match") {
		t.Fatal("expected no-match empty state")
	}
	if got := app.Find("id:asset-drawer-empty-hint").Text; got != "Clear a filter to see more assets." {
		t.Fatalf("no-match hint = %q", got)
	}
	app.Press("id:asset-drawer-clear-filters")
	app.Settle()
	if snap := ed.AssetDrawerSnapshot(); len(snap.VisibleItems) != len(snap.AllItems) {
		t.Fatalf("Clear filters left %d/%d shown", len(snap.VisibleItems), len(snap.AllItems))
	}

	// Right panel: clip frame-by-frame preview on knight.png (matched to knight_run_0/1 & knight_idle_0).
	app.Post(func() { ed.SelectAssetPath("art/characters/knight.png") })
	app.Settle()
	if !app.Exists("id:asset-preview-clips") || !app.Exists("id:asset-preview-play") {
		t.Fatal("expected clip chips, frame strip, and play button for multi-frame knight.png")
	}
	app.Press("id:asset-clip-run")
	app.Settle()
	if ed.AssetPreviewClip() != "run" {
		t.Fatalf("AssetPreviewClip = %q, want run", ed.AssetPreviewClip())
	}
	app.Press("id:asset-frame-next")
	app.Settle()
	if ed.AssetPreviewFrame() != 1 {
		t.Fatalf("AssetPreviewFrame after next = %d, want 1", ed.AssetPreviewFrame())
	}

	// Right panel: audio play/stop toggle on jump.wav.
	app.Post(func() { ed.SelectAssetPath("audio/sfx/jump.wav") })
	app.Settle()
	app.Press("id:asset-preview-play")
	app.Settle()
	if ed.AssetAudioPlayingPath() != "audio/sfx/jump.wav" {
		t.Fatalf("AssetAudioPlayingPath = %q, want audio/sfx/jump.wav", ed.AssetAudioPlayingPath())
	}
	app.Press("id:asset-preview-play")
	app.Settle()
	if ed.AssetAudioPlayingPath() != "" {
		t.Fatalf("expected second Play press to stop sound preview, got %q", ed.AssetAudioPlayingPath())
	}

	// Proposed filter + Keep banner.
	app.Post(func() { ed.ProposeAssetImports([]string{"art/props/coin.png"}) })
	app.Settle()
	if !app.Exists("id:asset-proposed-banner") {
		t.Fatal("expected proposed import banner")
	}
	app.Press("id:asset-filter-proposed")
	app.Settle()
	if snap := ed.AssetDrawerSnapshot(); len(snap.VisibleItems) != 1 || snap.VisibleItems[0].Path != "art/props/coin.png" {
		t.Fatalf("Proposed filter = %+v", snap.VisibleItems)
	}
	app.Press("id:asset-proposed-keep")
	app.Settle()
	if app.Exists("id:asset-proposed-banner") {
		t.Fatal("expected Keep to dismiss proposed banner")
	}
}

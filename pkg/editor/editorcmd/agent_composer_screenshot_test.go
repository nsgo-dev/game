package editorcmd_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestAgentComposerMacOSScreenshots renders the @-mentions picker, context chips,
// chip hover YAML preview, and context meter ring + breakdown popover on the real
// macOS host (Dark and Light appearances) for visual verification beside
// docs/design/editor/Threads.dc.html.
func TestAgentComposerMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"scenes", "prefabs", "art/enemies", "audio/sfx", "rules"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: dungeon-run
id: com.example.dungeonrun
appVersion: 0.1.0
window:
  title: Dungeon Run
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(`title: Dungeon Run
goals:
  - Door opens after two lever pulls
  - Slimes guard the exit
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(`# Dungeon Run
Every file is YAML. Keep edits reviewable.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules/enemies.md"), []byte(`No enemy within 64 px of the start.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prefabs/slime.prefab.yaml"), []byte(`version: 1
name: slime
hp: 2
`), 0o644); err != nil {
		t.Fatal(err)
	}
	slimeImg := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			slimeImg.SetRGBA(x, y, color.RGBA{R: 0x57, G: 0xa8, B: 0x7a, A: 0xff})
		}
	}
	if f, err := os.Create(filepath.Join(dir, "art/enemies/slime_green.png")); err == nil {
		_ = png.Encode(f, slimeImg)
		_ = f.Close()
	}
	_ = os.WriteFile(filepath.Join(dir, "audio/sfx/slime_squish.wav"), []byte("RIFF\x00\x00\x00\x00WAVE"), 0o644)

	sceneYAML := []byte(`version: 4
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    objects:
      - id: knight
        name: Knight
        x: 132
        y: 260
        sprite:
          atlas: actors
          frame: hero
      - id: door
        name: Door
        x: 540
        y: 260
        properties:
          locked: true
          opens_after_pulls: 2
      - id: lever_01
        name: lever_01
        x: 490
        y: 270
      - id: slime_01
        name: slime_01
        x: 228
        y: 276
        sprite:
          atlas: actors
          frame: slime
      - id: slime_02
        name: slime_02
        x: 292
        y: 276
        sprite:
          atlas: actors
          frame: slime
      - id: slime_03
        name: slime_03
        x: 420
        y: 230
        sprite:
          atlas: actors
          frame: slime
`)
	if err := os.WriteFile(filepath.Join(dir, "scenes/level_01.scene.yaml"), sceneYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	sheet := image.NewRGBA(image.Rect(0, 0, 48, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0xe9, G: 0xa8, B: 0x4a, A: 0xff})
		}
		for x := 24; x < 48; x++ {
			sheet.SetRGBA(x, y, color.RGBA{R: 0x57, G: 0xa8, B: 0x7a, A: 0xff})
		}
	}
	actors := &sprite.Atlas{
		Image: core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{
			"hero":  {Name: "hero", X: 0, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
			"slime": {Name: "slime", X: 24, Y: 0, W: 24, H: 24, Width: 24, Height: 24},
		},
	}

	ed, err := editor.New(nil, sceneformat.AtlasMap{"actors": actors})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.MoveObject("slime_03", 430, 230)
	ed.AddComposerPlaytestRun(editor.ComposerPlaytestRun{
		ID:      "run-1405",
		Name:    "Run 14:05 · 10 s",
		Summary: "slime_02 fell off the plank at 6.2 s",
		Detail:  "profile: cautious\nruns: 10\nslime_02 fell off the plank at 6.2 s",
	})
	for _, prompt := range []string{
		"Door opens after two lever pulls",
		"Click sound on the lever",
		"Lever glows while in reach",
		"Door opens over 0.6 s",
		"Open the door with a short rumble and a small camera shake.",
	} {
		ed.AddAgentThreadTurn(editor.AgentThreadTurn{
			Prompt: prompt,
			Reply:  "Done: " + prompt,
			Tokens: 16_400,
		})
	}
	ed.SetAgentTokenUsage(editor.AgentTokenUsage{
		Limit:         200_000,
		Conversation:  82_000,
		OpenScene:     24_000,
		Attached:      14_000,
		Always:        12_000,
		ToolsAndInstr: 10_000,
		ViewportImage: 6_000,
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 1. Dark: @sli picker open with attached chips (Door, lever_01), suggested chip (Knight · selected), Always chips, 74% ring.
	app.Post(func() {
		ed.SetRightRailMode(editor.RightRailAgent)
		ed.SelectObject("knight")
		_, _ = ed.AttachObjectChip("door")
		_, _ = ed.AttachObjectChip("lever_01")
		ed.SetComposerDraft("Put one slime in front of the door: @sli")
	})
	app.Settle()
	app.Screenshot("agent-composer-picker-dark")

	// 2. Dark: Chip hover preview showing exact "# level_01.scene.yaml · lines ..." and Door YAML.
	app.Post(func() {
		ed.CloseMentionPicker()
		ed.SetComposerDraft("Open the door after two lever pulls")
		chips := ed.ComposerChips()
		if len(chips) > 0 {
			ed.SetHoveredChip(chips[0].ID)
		}
	})
	app.Settle()
	app.Screenshot("agent-composer-hover-dark")

	// 3. Dark: Context ring breakdown popover (74% warning + Compact thread + Start fresh with summary).
	app.Post(func() {
		ed.SetHoveredChip("")
		ed.SetContextBreakdownOpen(true)
	})
	app.Settle()
	app.Screenshot("agent-composer-breakdown-dark")

	// 4. Light: Context ring critical 93% "compact now" + breakdown popover in Spec Light theme.
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
		ed.SetAgentTokenUsage(editor.AgentTokenUsage{
			Limit:         200_000,
			Conversation:  120_000,
			OpenScene:     24_000,
			Attached:      14_000,
			Always:        12_000,
			ToolsAndInstr: 10_000,
			ViewportImage: 6_000,
		})
		ed.SetContextBreakdownOpen(true)
	})
	app.Settle()
	app.Screenshot("agent-composer-critical-light")
}

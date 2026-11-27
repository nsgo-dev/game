package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestAgentThreadsMacOSScreenshots renders the Agent Threads UI on the real native macOS host
// across folded/expanded thread view, grouped thread switcher popover, empty-thread view,
// Inspector footer attribution, and Spec Light theme for visual verification beside
// docs/design/editor/Threads.dc.html and Spec.dc.html.
func TestAgentThreadsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	scenesDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(scenesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	level1Doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#1a1f2a",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{ID: "Player", Name: "Player", X: 132, Y: 350},
					{ID: "Knight", Name: "Knight", X: 444, Y: 286},
					{
						ID:   "Door",
						Name: "Door",
						X:    632,
						Y:    356,
						Properties: map[string]any{
							"locked":            true,
							"opens_after_pulls": 2,
						},
					},
					{ID: "lever_01", Name: "lever_01", X: 598, Y: 372},
					{ID: "slime_01", Name: "slime_01", X: 228, Y: 376},
					{ID: "slime_02", Name: "slime_02", X: 292, Y: 376},
					{ID: "slime_03", Name: "slime_03", X: 512, Y: 300},
				},
			},
		},
	}
	bossDoc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "boss_room",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#1a1f2a",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{ID: "boss_spawn", Name: "boss_spawn", X: 320, Y: 220},
				},
			},
		},
	}
	l1Bytes, err := sceneformat.Encode(level1Doc)
	if err != nil {
		t.Fatal(err)
	}
	bossBytes, err := sceneformat.Encode(bossDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenesDir, "level_01.scene.yaml"), l1Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenesDir, "boss_room.scene.yaml"), bossBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	proj := &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.1.0",
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			MainScene: "level_01",
			Scenes: []project.Entry{
				{Name: "level_01", Path: "scenes/level_01.scene.yaml"},
				{Name: "boss_room", Path: "scenes/boss_room.scene.yaml"},
			},
		},
	}
	projBytes, err := project.Encode(proj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), projBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	briefYAML := `suggestions:
  - "Block out level_01 with three platforms and an exit door"
  - "Add a slime that patrols the floor and costs the Knight one heart"
  - "Set up the HUD: three hearts and a coin counter"
`
	if err := os.WriteFile(filepath.Join(dir, "brief.yaml"), []byte(briefYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.Now = func() time.Time { return now }
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SetAgentMCPConnected(true, "MCP")

	// Seed threads matching docs/design/editor/Threads.dc.html.
	now = time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Spikes under the plank",
		Prompt:       "Place spikes under the wooden plank.",
		Outcome:      editor.AgentThreadOutcomeReverted,
		ChangesTotal: 2,
	})
	ed.NewAgentThread()
	now = time.Date(2026, 11, 26, 13, 20, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Slimes and a coin trail",
		Prompt:       "Put three slimes and a coin trail toward the door.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 6,
		ChangesKept:  5,
	})
	ed.NewAgentThread()
	now = time.Date(2026, 11, 26, 13, 40, 0, 0, time.UTC)
	r1, _ := ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Door opens after two lever pulls",
		Prompt:       "Keep the door shut until the lever is pulled twice, then let it open.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 3,
		ChangesKept:  3,
		Objects:      []string{"Door", "lever_01"},
		Detail:       "Door.locked, Door.opens_after_pulls, lever_01.pulls",
		Steps: []editor.AgentThreadStep{
			{ID: "s1", Label: "Set Door.opens_after_pulls = 2 and wired lever_01", Status: editor.AgentThreadStepDone, Objects: []string{"Door"}},
		},
	})
	now = time.Date(2026, 11, 26, 13, 52, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Click sound on the lever",
		Prompt:       "Add a click sound when the lever moves.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 2,
		ChangesKept:  2,
		Objects:      []string{"lever_01"},
		Detail:       "lever_01.sound, audio/sfx/lever_click.wav",
	})
	now = time.Date(2026, 11, 26, 14, 5, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Lever glows while in reach",
		Prompt:       "Make the lever glow while the Knight is within reach.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 2,
		ChangesKept:  1,
		Detail:       "lever_01.glow kept · reach_radius 48 reverted",
	})
	now = time.Date(2026, 11, 26, 14, 11, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Door opens over 0.6 s",
		Prompt:       "Slow the door opening down to 0.6 s.",
		Outcome:      editor.AgentThreadOutcomeReverted,
		ChangesTotal: 1,
		Detail:       "Door.open_time 0.25 → 0.6",
	})
	now = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Open the door with a short rumble and a small camera shake",
		Prompt:       "Open the door with a short rumble and a small camera shake.",
		Outcome:      editor.AgentThreadOutcomePending,
		ChangesTotal: 3,
		Files:        []string{"prefabs/door.prefab.yaml", "scenes/level_01.scene.yaml"},
		Objects:      []string{"Door"},
		Steps: []editor.AgentThreadStep{
			{ID: "st1", Label: "Read door.prefab.yaml and camera.yaml", Status: editor.AgentThreadStepDone},
			{ID: "st2", Label: "Added door_rumble.wav and a 0.3 s shake on open", Status: editor.AgentThreadStepDone},
			{ID: "st3", Label: "Played 6 s headless · door opens on the second pull", Status: editor.AgentThreadStepDone},
		},
	})
	doorThreadID := ed.ActiveAgentThread().ID

	// Project-wide thread + boss_room thread.
	ed.NewProjectAgentThread()
	now = time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:   "Double jump for the Knight",
		Prompt:  "Add a double jump ability for the Knight.",
		Outcome: editor.AgentThreadOutcomeKept,
		Files:   []string{"game.yaml", "prefabs/knight.prefab.yaml"},
	})
	_ = ed.OpenScene("scenes/boss_room.scene.yaml")
	now = time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	_, _ = ed.SendAgentRequestWith(editor.AgentSendRequest{
		Title:        "Torches around the boss arena",
		Prompt:       "Place flickering torches around the boss arena.",
		Outcome:      editor.AgentThreadOutcomeKept,
		ChangesTotal: 4,
		ChangesKept:  4,
	})
	now = time.Date(2026, 11, 26, 14, 20, 0, 0, time.UTC)
	_ = ed.OpenScene("scenes/level_01.scene.yaml")
	_ = ed.SelectAgentThread(doorThreadID)
	ed.SelectObject("Door")
	ed.SetRightRailTab(editor.RightRailAgent)
	ed.SetExpandedAgentRequestID(r1.ID)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"}))

	// 1. Folded earlier requests + expanded request + "Open this step in History" + "Jump to latest" + latest pending card.
	app.Screenshot("agent-threads-folded-expanded")

	// 2. Thread switcher popover open grouped by open scene, other scenes, and Project · no scene.
	app.Post(func() {
		ed.SetAgentSwitcherOpen(true)
	})
	app.Settle()
	app.Screenshot("agent-threads-switcher")

	// 3. Empty thread view with always-read context chips and FROM YOUR BRIEF suggestions.
	app.Post(func() {
		ed.NewAgentThread()
	})
	app.Settle()
	app.Screenshot("agent-threads-empty")

	// 4. Inspector footer linking Door to the request that last changed it.
	app.Post(func() {
		_ = ed.SelectAgentThread(doorThreadID)
		ed.SetRightRailTab(editor.RightRailInspector)
		ed.SelectObject("Door")
	})
	app.Settle()
	app.Screenshot("agent-threads-inspector-footer")

	// 5. Spec Light theme parity check on Agent threads + switcher.
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
		ed.SetRightRailTab(editor.RightRailAgent)
		ed.SetAgentSwitcherOpen(true)
	})
	app.Settle()
	app.Screenshot("agent-threads-light")
}

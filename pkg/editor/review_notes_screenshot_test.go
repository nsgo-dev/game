package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestReviewNotesAndRoundsMacOSScreenshots renders the Review screen's note composer,
// viewport pins, Round 1 / Round 2 switcher, Round 2 banner, paused Agent panel card,
// and Spec Dark/Light appearances on the real native macOS host for visual inspection
// beside design/editor/Review.dc.html and design/editor/Spec.dc.html.
func TestReviewNotesAndRoundsMacOSScreenshots(t *testing.T) {
	beforeDoc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "knight", Name: "Knight", X: 352, Y: 192, Z: 10},
					{ID: "door_1", Name: "door_1", X: 574, Y: 248, Z: 5},
				},
			},
		},
	}
	beforeScene, err := sceneformat.Encode(beforeDoc)
	if err != nil {
		t.Fatal(err)
	}
	afterDoc, err := sceneformat.Parse(beforeScene)
	if err != nil {
		t.Fatal(err)
	}
	afterDoc.Layers[0].Objects[0].Z = 12
	afterDoc.Layers[0].Objects = append(afterDoc.Layers[0].Objects,
		sceneformat.ObjectDoc{ID: "slime_01", Name: "slime_01", Prefab: "prefabs/slime.prefab.yaml", X: 160, Y: 278, Z: 11},
		sceneformat.ObjectDoc{ID: "slime_02", Name: "slime_02", Prefab: "prefabs/slime.prefab.yaml", X: 232, Y: 278, Z: 11},
		sceneformat.ObjectDoc{ID: "slime_03", Name: "slime_03", Prefab: "prefabs/slime.prefab.yaml", X: 452, Y: 208, Z: 11},
		sceneformat.ObjectDoc{ID: "coin_01", Name: "coin_01", X: 486, Y: 280, Z: 10},
	)
	afterScene, err := sceneformat.Encode(afterDoc)
	if err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(beforeDoc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.FilePath = "scenes/level_01.scene.yaml"
	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{ID: "c1", Name: "claude-code", DisplayName: "Claude Code", Transport: "stdio"})
	ed.SetRightRailTab(editor.RightTabAgent)

	if err := ed.StartTurnReview(editor.ReviewTurnInput{
		Request: "Put slime patrols on the lower floors of level_01 and level_02, with a coin trail toward each door.",
		Files: []editor.ReviewFileInput{
			{
				Path:       "scenes/level_01.scene.yaml",
				BeforeYAML: beforeScene,
				AfterYAML:  afterScene,
				Reasons: map[string]string{
					"slime_01": "Two slimes share the 120 px floor span, half a cycle apart.",
					"slime_02": "Two slimes share the 120 px floor span, half a cycle apart.",
					"slime_03": "The plank is the only other floor wide enough for a patrol; this slime guards the coin trail from above.",
					"coin_01":  "Three coins lead from under the plank to door_1.",
					"knight":   "Slimes are drawn at z 11; without this the Knight disappears behind slime_03.",
				},
				Details: map[string]string{
					"slime_01": "patrols x 150–270",
					"slime_02": "patrols x 150–270, half a cycle behind",
					"slime_03": "patrols the plank x 440–490",
					"coin_01":  "value 1 · start of the trail",
					"knight":   "z 10 → 12 · drawn above slimes",
				},
			},
			{
				Path:       "prefabs/slime.prefab.yaml",
				BeforeYAML: []byte("version: 1\ncomponents:\n  patrol:\n    speed: 32\n"),
				AfterYAML:  []byte("version: 1\ncomponents:\n  patrol:\n    speed: 24\n"),
				Reasons: map[string]string{
					"patrol.speed": "Slower patrols leave time to collect the trail between passes.",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	snap := ed.ReviewStatus()
	var s3ID, c1ID string
	for _, ch := range snap.Changes {
		switch ch.ObjectID {
		case "slime_03":
			s3ID = ch.ID
		case "coin_01":
			c1ID = ch.ID
		}
	}
	if s3ID == "" || c1ID == "" {
		t.Fatalf("missing expected changes s3=%q c1=%q", s3ID, c1ID)
	}
	ed.FocusReviewChange(s3ID)
	if err := ed.SetReviewNote(s3ID, "This one sits where you land coming off the upper ledge. Move it about 40 px left."); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetReviewNote(c1ID, "Lift the trail 8 px so it reads as a jump arc."); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Round 1 with saved notes, Send 2 notes CTA, and viewport pins.
	app.Screenshot("review-notes-round1-dark")

	// 2. Inline note composer open on slime_03.
	app.Post(func() {
		_ = ed.OpenReviewNoteComposer(s3ID)
	})
	app.Settle()
	app.Screenshot("review-notes-composer-dark")

	// 3. Paused review card in Agent panel showing unsent notes.
	app.Post(func() {
		ed.CancelReviewNoteComposer()
		ed.PauseReview()
	})
	app.Settle()
	app.Screenshot("review-notes-paused-agent-dark")

	// 4. Send notes & apply Round 2 redo (dark + light).
	app.Post(func() {
		_ = ed.ResumeReview()
		_, _ = ed.SendReviewNotes()
		x2 := float32(412)
		y2 := float32(272)
		_ = ed.ApplyReviewRedo([]editor.ReviewRedoChange{
			{
				ChangeID: s3ID,
				Detail:   "patrols the plank x 400–450",
				X:        &x2,
			},
			{
				ChangeID: c1ID,
				Detail:   "value 1 · raised 8 px",
				Y:        &y2,
			},
		})
	})
	app.Settle()
	app.Screenshot("review-notes-round2-dark")

	// 5. Spec Light appearance of Round 2.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("review-notes-round2-light")
}

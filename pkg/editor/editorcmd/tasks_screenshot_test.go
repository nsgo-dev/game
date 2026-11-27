package editorcmd_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestBackgroundTasksMacOSScreenshots(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 38, 0, time.UTC)
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Camera: &sceneformat.CameraDoc{
			X: 320,
			Y: 180,
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "player", Name: "Player", X: 112, Y: 240},
					{ID: "knight", Name: "Knight", X: 160, Y: 240},
					{ID: "door", Name: "Door", X: 540, Y: 236},
					{ID: "slime_01", Name: "slime_01", X: 210, Y: 266},
					{ID: "slime_02", Name: "slime_02", X: 310, Y: 266},
					{ID: "slime_03", Name: "slime_03", X: 460, Y: 210},
				},
			},
		},
	}

	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.Now = func() time.Time { return now }
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)

	// Populate the exact five groups shown in docs/design/editor/Tasks.dc.html.
	for _, req := range []editor.TaskRequest{
		{
			ID:               "t3",
			Title:            "Torches around the boss arena",
			Source:           editor.TaskSourcePlan,
			PlanStep:         2,
			PlanTotal:        5,
			StartMode:        editor.TaskStartQueue,
			ScenePath:        "boss_room",
			PermissionPrompt: "wants to run the game",
			PermissionDetail: "Torches around the boss arena wants to play boss_room headless for 10 s.",
			InitialState:     editor.TaskNeedsYou,
			TimeLabel:        "1:12",
		},
		{
			ID:             "t1",
			Number:         1,
			Title:          "slime_02 jumps the gap at x 360",
			Source:         editor.TaskSourceUser,
			StartMode:      editor.TaskStartQueue,
			ScenePath:      "level_01",
			StepIndex:      2,
			StepTotal:      4,
			StepSummary:    "placing the jump arc",
			EditingObjects: []string{"slime_02"},
			Steps: []editor.TaskStep{
				{Summary: "Measured the gap: 64 px from x 360 to 424", Done: true},
				{Summary: "Adding a jump to slime_02's patrol · step 2 of 4", Active: true},
			},
			InitialState:   editor.TaskRunning,
			ElapsedSeconds: 38,
		},
		{
			ID:              "t2",
			Number:          2,
			Title:           "All slimes 20% faster",
			Source:          editor.TaskSourceUser,
			StartMode:       editor.TaskStartParallel,
			ScenePath:       "level_01",
			HoldingObjects:  []string{"slime_01", "slime_03"},
			BlockedOn:       "slime_02",
			BlockedByTaskID: "t1",
			Steps: []editor.TaskStep{
				{Summary: "waiting on slime_02 (task 1)", Blocked: true},
			},
			InitialState:   editor.TaskRunning,
			ElapsedSeconds: 12,
		},
		{
			ID:           "t4",
			Title:        "Nightly: fix game check warnings",
			Source:       editor.TaskSourceAutomation,
			StartMode:    editor.TaskStartQueue,
			ScenePath:    "level_02",
			InitialState: editor.TaskReadyToReview,
			TimeLabel:    "02:00",
			Review:       &editor.TaskReview{ID: "rev-t4", TaskID: "t4", ChangeCount: 3},
		},
		{
			ID:           "t5",
			Title:        "Translate HUD strings to Japanese",
			Source:       editor.TaskSourceAutomation,
			StartMode:    editor.TaskStartQueue,
			ScenePath:    "locale/ja.yaml",
			InitialState: editor.TaskReadyToReview,
			TimeLabel:    "02:10",
			Review:       &editor.TaskReview{ID: "rev-t5", TaskID: "t5", ChangeCount: 12},
		},
		{
			ID:                "t6",
			Title:             "Coin pickup sound a little higher",
			Source:            editor.TaskSourceUser,
			StartMode:         editor.TaskStartQueue,
			StartsAfterTaskID: "t1",
			InitialState:      editor.TaskQueued,
		},
		{
			ID:           "t7",
			Title:        "Bake normals for the cave tiles",
			Source:       editor.TaskSourceAutomation,
			StartMode:    editor.TaskStartQueue,
			Error:        "asset tool exited with code 1",
			InitialState: editor.TaskFailed,
			TimeLabel:    "01:30",
		},
	} {
		if _, err := ed.SubmitTask(req); err != nil {
			t.Fatalf("SubmitTask %s: %v", req.ID, err)
		}
	}
	ed.FocusTask("t1")
	ed.SetTaskDismissSelected("t4", true)
	ed.SetTaskComposerDraft("Add a lever beside the door")
	ed.SetTasksPanelOpen(true)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if h := app.Find("id:editor-toolbar").Bounds().Size.Height; h > 104 {
		t.Fatalf("2D editor-toolbar height at 1440x900 = %v pt, want <= 104 pt (<= 2 rows)", h)
	}

	app.Screenshot("background-tasks-dark")

	// Also verify Spec Light appearance.
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
	})
	app.Settle()
	app.Screenshot("background-tasks-light")
}

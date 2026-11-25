package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func inspectorSpecRegistry(t *testing.T) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	for _, ct := range []sceneformat.ComponentType{
		{
			Name: "patrol",
			Doc:  "Walks between two points.",
			Fields: []sceneformat.Field{
				{Name: "speed", Kind: sceneformat.FieldFloat, Default: 24.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(400), Step: 1},
				{Name: "wait", Kind: sceneformat.FieldFloat, Default: 0.5, Min: sceneformat.Float64(0), Step: 0.1},
			},
		},
		{
			Name: "health",
			Doc:  "Hit points and invulnerability window.",
			Fields: []sceneformat.Field{
				{Name: "max", Kind: sceneformat.FieldInt, Default: 3, Min: sceneformat.Float64(1), Max: sceneformat.Float64(100)},
				{Name: "iframes", Kind: sceneformat.FieldFloat, Default: 0.6, Min: sceneformat.Float64(0), Step: 0.1},
			},
		},
		{
			Name: "pickup",
			Doc:  "Collectible item value.",
			Fields: []sceneformat.Field{
				{Name: "value", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0)},
			},
		},
		{
			Name: "physics",
			Doc:  "Rigid body settings.",
			Fields: []sceneformat.Field{
				{Name: "solid", Kind: sceneformat.FieldBool, Default: true},
				{Name: "gravity", Kind: sceneformat.FieldFloat, Default: 9.8, Min: sceneformat.Float64(0), Max: sceneformat.Float64(100)},
				{Name: "friction", Kind: sceneformat.FieldFloat, Default: 0.2, Min: sceneformat.Float64(0), Max: sceneformat.Float64(1)},
				{Name: "layer", Kind: sceneformat.FieldInt, Default: 0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31)},
				{Name: "kind", Kind: sceneformat.FieldString, Default: "static"},
			},
		},
		{
			Name: "area",
			Doc:  "Trigger overlap volume.",
			Fields: []sceneformat.Field{
				{Name: "radius", Kind: sceneformat.FieldFloat, Default: 16.0, Min: sceneformat.Float64(1)},
			},
		},
		{
			Name: "sound_emitter",
			Doc:  "Plays spatial sound effects.",
			Fields: []sceneformat.Field{
				{Name: "volume", Kind: sceneformat.FieldFloat, Default: 0.8, Min: sceneformat.Float64(0), Max: sceneformat.Float64(1), Step: 0.05},
			},
		},
		{
			Name: "light2d",
			Doc:  "2D point light.",
			Fields: []sceneformat.Field{
				{Name: "radius", Kind: sceneformat.FieldFloat, Default: 48.0, Min: sceneformat.Float64(1)},
			},
		},
	} {
		if err := reg.Register(ct); err != nil {
			t.Fatalf("Register %s: %v", ct.Name, err)
		}
	}
	return reg
}

func newInspectorSpecEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc := &sceneformat.SceneDoc{
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
					{
						ID:     "knight",
						Name:   "Knight",
						X:      120,
						Y:      340,
						Z:      12,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
					},
					{
						ID:     "slime_02",
						Name:   "slime_02",
						X:      284,
						Y:      340,
						Z:      10,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
						Components: map[string]any{
							"patrol": map[string]any{
								"speed": 30.0,
								"wait":  0.5,
							},
							"health": map[string]any{
								"max":     3,
								"iframes": 0.6,
							},
						},
						Properties: map[string]any{
							"title":       "Fast Slime",
							"speed_bonus": 4.0,
						},
					},
				},
			},
		},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(inspectorSpecRegistry(t)); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SetActiveLayer("actors")
	ed.SelectObject("slime_02")
	return ed
}

func TestInspectorHeaderAndOpenYAML(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	dir := t.TempDir()
	sceneFile := filepath.Join(dir, "level_01.scene.yaml")
	if err := ed.SaveFile(sceneFile); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	raw, err := os.ReadFile(sceneFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	wantLine := 0
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "- id: slime_02") {
			wantLine = i + 1
			break
		}
	}
	if wantLine == 0 {
		t.Fatalf("could not find slime_02 in saved YAML:\n%s", raw)
	}

	var openedPath string
	var openedLine int
	ed.OnOpenFileAtLine = func(path string, line int) {
		openedPath = path
		openedLine = line
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	if got := app.Find("id:inspector-object-id").Text; got != "slime_02" {
		t.Fatalf("inspector-object-id = %q, want slime_02", got)
	}
	if got := app.Find("id:inspector-object-subtitle").Text; got != "Sprite · layer actors" {
		t.Fatalf("inspector-object-subtitle = %q, want %q", got, "Sprite · layer actors")
	}

	app.Press("id:inspector-open-yaml")
	if openedPath != sceneFile || openedLine != wantLine {
		t.Fatalf("OnOpenFileAtLine got (%q, %d), want (%q, %d)", openedPath, openedLine, sceneFile, wantLine)
	}
	if !strings.Contains(ed.Status, sceneFile) || !strings.Contains(ed.Status, ":2") && openedLine >= 10 && !strings.Contains(ed.Status, ":") {
		t.Fatalf("unexpected Open YAML status: %q", ed.Status)
	}
}

func TestInspectorFieldFilterNarrowsSectionsAndFields(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1400))

	if !app.Exists("id:inspector-x") || !app.Exists("id:inspector-comp-patrol-speed") || !app.Exists("id:inspector-comp-patrol-wait") || !app.Exists("id:inspector-comp-health-max") {
		t.Fatal("expected unfiltered inspector to show transform, patrol, and health fields")
	}

	// Filtering "speed" leaves only fields matching "speed".
	app.Type("id:inspector-field-filter", "speed")
	if !app.Exists("id:inspector-comp-patrol-speed") {
		t.Fatal("expected patrol.speed to remain visible when filtering 'speed'")
	}
	if !app.Exists("id:inspector-prop-speed_bonus") {
		t.Fatal("expected property speed_bonus to remain visible when filtering 'speed'")
	}
	for _, hidden := range []string{
		"id:inspector-x",
		"id:inspector-y",
		"id:inspector-name",
		"id:inspector-sprite",
		"id:inspector-comp-patrol-wait",
		"id:inspector-comp-health-max",
		"id:inspector-comp-health-iframes",
		"id:inspector-prop-title",
	} {
		if app.Exists(hidden) {
			t.Fatalf("expected %s to be hidden when filtering 'speed'", hidden)
		}
	}

	// Filtering by component name "health" shows all HEALTH fields and hides PATROL.
	app.Press("id:inspector-field-filter-clear")
	app.Type("id:inspector-field-filter", "health")
	if !app.Exists("id:inspector-comp-health-max") || !app.Exists("id:inspector-comp-health-iframes") {
		t.Fatal("expected all health fields to show when filtering 'health'")
	}
	if app.Exists("id:inspector-comp-patrol-speed") || app.Exists("id:inspector-x") {
		t.Fatal("expected patrol and transform fields to hide when filtering 'health'")
	}

	// Clearing restores every section.
	app.Press("id:inspector-field-filter-clear")
	if !app.Exists("id:inspector-x") || !app.Exists("id:inspector-comp-patrol-speed") || !app.Exists("id:inspector-comp-health-max") {
		t.Fatal("expected clearing field filter to restore all sections")
	}
}

func TestInspectorScrubXLabelOneUndoStepAndEscapeRestores(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1200))

	beforeIdx := ed.History.Index()
	origX := float32(284)

	// Scrubbing the X label by drag moves the object live and records ONE undo step.
	app.Pan("id:inspector-x-scrub", core.Point{}, core.Point{X: 20, Y: 0}, core.Point{X: 40, Y: 0})
	obj, _, _ := ed.FindObject("slime_02")
	if obj.X != origX+10 {
		t.Fatalf("slime_02.X after scrubbing +40pt = %v, want %v", obj.X, origX+10)
	}
	if got := ed.History.Index() - beforeIdx; got != 1 {
		t.Fatalf("scrub recorded %d undo steps, want 1", got)
	}
	// One Undo restores the original X.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if obj.X != origX {
		t.Fatalf("slime_02.X after one Undo = %v, want %v", obj.X, origX)
	}

	// Escape during an active scrub cancels and restores the original value with zero undo steps.
	idxBeforeEsc := ed.History.Index()
	app.Post(func() {
		if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "slime_02", Field: "x", Step: 1}); err != nil {
			t.Errorf("BeginInspectorScrub: %v", err)
		}
		if _, err := ed.UpdateInspectorScrub(48, false); err != nil {
			t.Errorf("UpdateInspectorScrub: %v", err)
		}
	})
	app.Settle()
	if obj.X == origX {
		t.Fatal("expected live scrub preview to move slime_02.X before Escape")
	}
	app.Key("escape")
	if ed.InspectorScrubbing() {
		t.Fatal("expected Escape to cancel active inspector scrub")
	}
	if obj.X != origX {
		t.Fatalf("slime_02.X after Escape during scrub = %v, want %v", obj.X, origX)
	}
	if ed.History.Index() != idxBeforeEsc {
		t.Fatalf("cancelled scrub recorded %d undo steps, want 0", ed.History.Index()-idxBeforeEsc)
	}

	// In 3D mode, Shift scrubs in 0.01 fine steps and records one undo step.
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
		if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "slime_02", Field: "posZ", Step: 1}); err != nil {
			t.Errorf("BeginInspectorScrub 3D: %v", err)
		}
		if _, err := ed.UpdateInspectorScrub(12, true); err != nil {
			t.Errorf("UpdateInspectorScrub 3D shift: %v", err)
		}
		if !ed.EndInspectorScrub() {
			t.Errorf("EndInspectorScrub 3D returned false")
		}
	})
	app.Settle()
	if math.Abs(float64(obj.PosZ)-0.03) > 1e-4 {
		t.Fatalf("slime_02.PosZ after 3D Shift scrub (+12pt = 3 steps × 0.01) = %v, want 0.03", obj.PosZ)
	}
	if got := ed.History.Index() - idxBeforeEsc; got != 1 {
		t.Fatalf("3D Shift scrub recorded %d undo steps, want 1", got)
	}

	// Scrubbing an unset registered component field starts from its registered default (friction 0.2), not 0.
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode2D)
		if err := ed.AddComponent("knight", "physics"); err != nil {
			t.Fatalf("AddComponent physics: %v", err)
		}
		// Remove friction from map so it is unset and falls back to default 0.2.
		k, _, _ := ed.FindObject("knight")
		delete(k.Components["physics"].(map[string]any), "friction")
		if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "knight", Field: "comp:physics.friction", Step: 0.1}); err != nil {
			t.Fatalf("BeginInspectorScrub friction: %v", err)
		}
		val, err := ed.UpdateInspectorScrub(8, false)
		if err != nil {
			t.Fatalf("UpdateInspectorScrub friction: %v", err)
		}
		if math.Abs(val-0.4) > 1e-6 {
			t.Fatalf("scrubbed unset friction = %v, want 0.4 (default 0.2 + 2×0.1)", val)
		}
		ed.CancelInspectorScrub()
	})
	app.Settle()
}

func TestInspectorGroupedAddComponentAndAskToAdd(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	var receivedPrompt string
	ed.OnComposerPrompt = func(prompt string) {
		receivedPrompt = prompt
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1400))

	// Add component groups registered types slime_02 lacks under GAMEPLAY, PHYSICS, AUDIO AND VISUAL.
	for _, groupID := range []string{
		"id:inspector-add-group-gameplay",
		"id:inspector-add-group-physics",
		"id:inspector-add-group-audio-visual",
	} {
		if !app.Exists(groupID) {
			t.Fatalf("expected grouped Add component section %s", groupID)
		}
	}
	if !app.Exists("id:inspector-add-component-pickup") || !app.Exists("id:inspector-add-component-area") || !app.Exists("id:inspector-add-component-sound_emitter") {
		t.Fatal("expected pickup, area, and sound_emitter buttons inside grouped Add component")
	}

	// Ask to add puts a request about the selected object into the composer.
	app.Press("id:inspector-ask-to-add")
	wantPrompt := "Add a component to slime_02 that "
	if ed.ComposerDraft() != wantPrompt {
		t.Fatalf("ComposerDraft() = %q, want %q", ed.ComposerDraft(), wantPrompt)
	}
	if receivedPrompt != wantPrompt {
		t.Fatalf("OnComposerPrompt got %q, want %q", receivedPrompt, wantPrompt)
	}
}

func TestInspectorReviewIndicatorsProblemPillsCollapseAndFooter(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	ed.SetInspectorReviewChanges([]editor.InspectorReviewChange{
		{ObjectID: "slime_02", Component: "patrol", Field: "speed", OldValue: "24", NewValue: "30"},
	})
	ed.SetInspectorFieldProblems([]editor.InspectorFieldProblem{
		{ObjectID: "slime_02", Component: "health", Field: "max", Message: "Must be 1 or more"},
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 1400))

	if got := allText(app.Find("id:inspector-comp-patrol-speed-review")); !strings.Contains(got, "was 24 · in review") {
		t.Fatalf("review sub-row = %q, want %q", got, "was 24 · in review")
	}
	if got := app.Find("id:inspector-section-comp-patrol-meta").Text; got != "1 in review" {
		t.Fatalf("patrol section meta = %q, want %q", got, "1 in review")
	}
	if got := allText(app.Find("id:inspector-comp-health-max-problem")); !strings.Contains(got, "Must be 1 or more") {
		t.Fatalf("problem pill = %q, want %q", got, "Must be 1 or more")
	}
	if got := allText(app.Find("id:inspector-section-comp-health-problem")); !strings.Contains(got, "1 problem") {
		t.Fatalf("health section problem pill = %q, want %q", got, "1 problem")
	}
	if got := app.Find("id:inspector-section-transform-meta").Text; got != "5 fields" {
		t.Fatalf("transform section field count = %q, want %q", got, "5 fields")
	}
	if got := app.Find("id:inspector-footer").Text; got != "Everything else about this object is in its YAML. Ask the agent, or open the file." {
		t.Fatalf("inspector-footer = %q", got)
	}

	// Collapsing TRANSFORM hides X/Y while keeping the section header visible.
	app.Press("id:inspector-section-transform")
	if app.Exists("id:inspector-x") {
		t.Fatal("expected inspector-x to hide after collapsing TRANSFORM")
	}
	app.Press("id:inspector-section-transform")
	if !app.Exists("id:inspector-x") {
		t.Fatal("expected inspector-x to reappear after expanding TRANSFORM")
	}
}

func TestInspectorSpecUIAndScreenshots(t *testing.T) {
	ed := newInspectorSpecEditor(t)
	ed.SetInspectorReviewChanges([]editor.InspectorReviewChange{
		{ObjectID: "slime_02", Component: "patrol", Field: "speed", OldValue: "24", NewValue: "30"},
	})
	ed.SetInspectorFieldProblems([]editor.InspectorFieldProblem{
		{ObjectID: "slime_02", Component: "health", Field: "max", Message: "Must be 1 or more"},
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Screenshot("inspector-sections-2d")

	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
	})
	app.Settle()
	if !app.Exists("id:inspector-scrub-hint-3d") {
		t.Fatal("expected 3D scrub hint in 3D mode")
	}
	app.Screenshot("inspector-sections-3d")
}

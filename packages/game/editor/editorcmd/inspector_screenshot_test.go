package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestInspectorMacOSScreenshots renders the redesigned Inspector on the real native macOS host
// across 2D, filtered ("speed"), and 3D modes for visual verification beside
// docs/design/editor/Inspector.dc.html, Main.dc.html, Viewport3D.dc.html, and Spec.dc.html.
func TestInspectorMacOSScreenshots(t *testing.T) {
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
	} {
		if err := reg.Register(ct); err != nil {
			t.Fatalf("Register %s: %v", ct.Name, err)
		}
	}

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
						ID:     "slime_02",
						Name:   "slime_02",
						X:      284,
						Y:      340,
						Z:      10,
						Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
						Components: map[string]any{
							"patrol": map[string]any{"speed": 30.0, "wait": 0.5},
							"health": map[string]any{"max": 3, "iframes": 0.6},
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

	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	ed.SetActiveLayer("actors")
	ed.SelectObject("slime_02")
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
		ed.SetInspectorFilter("speed")
	})
	app.Settle()
	app.Screenshot("inspector-sections-filtered")

	app.Post(func() {
		ed.SetInspectorFilter("")
		ed.SetViewMode(editor.ViewMode3D)
	})
	app.Settle()
	app.Screenshot("inspector-sections-3d")
}

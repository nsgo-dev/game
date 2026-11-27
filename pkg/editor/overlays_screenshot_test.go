package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestViewportOverlaysMacOSScreenshots(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterNavRegion2DComponent(reg); err != nil {
		t.Fatalf("register navregion2d: %v", err)
	}
	for _, ct := range []sceneformat.ComponentType{
		{
			Name: "body",
			Fields: []sceneformat.Field{
				{Name: "shape", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -12, Y: -12, Width: 24, Height: 24}},
			},
		},
		{
			Name: "trigger",
			Fields: []sceneformat.Field{
				{Name: "zone", Kind: sceneformat.FieldRect, Default: sceneformat.Rect{X: -20, Y: -20, Width: 40, Height: 40}},
			},
		},
		{
			Name:   "lever",
			Events: []sceneformat.Event{{Name: "pulled"}},
		},
		{
			Name:     "door",
			Handlers: []sceneformat.Handler{{Name: "open"}},
		},
		{
			Name: "patrol",
			Fields: []sceneformat.Field{
				{Name: "from", Kind: sceneformat.FieldFloat, Default: 0.0},
				{Name: "to", Kind: sceneformat.FieldFloat, Default: 0.0},
			},
		},
	} {
		if err := reg.Register(ct); err != nil {
			t.Fatalf("register %s: %v", ct.Name, err)
		}
	}

	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Camera: &sceneformat.CameraDoc{
			X: 320,
			Y: 180,
			Bounds: &sceneformat.BoundsDoc{
				X:      0,
				Y:      0,
				Width:  1280,
				Height: 720,
			},
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "knight",
						Name: "Knight",
						X:    110,
						Y:    240,
						Components: map[string]any{
							"body": map[string]any{
								"shape": sceneformat.Rect{X: -10, Y: -16, Width: 20, Height: 32},
							},
						},
					},
					{
						ID:   "lever_01",
						Name: "lever_01",
						X:    180,
						Y:    240,
						Components: map[string]any{
							"lever": map[string]any{},
							"trigger": map[string]any{
								"zone": sceneformat.Rect{X: -18, Y: -18, Width: 36, Height: 36},
							},
						},
					},
					{
						ID:   "door_01",
						Name: "door_01",
						X:    490,
						Y:    232,
						Components: map[string]any{
							"door": map[string]any{},
							"trigger": map[string]any{
								"zone": sceneformat.Rect{X: -24, Y: -24, Width: 48, Height: 48},
							},
						},
					},
					{
						ID:   "slime_01",
						Name: "slime_01",
						X:    270,
						Y:    248,
						Components: map[string]any{
							"body": map[string]any{
								"shape": sceneformat.Rect{X: -13, Y: -8, Width: 26, Height: 16},
							},
							"patrol": map[string]any{
								"from": 220.0,
								"to":   350.0,
							},
						},
					},
					{
						ID:   "nav_floor",
						Name: "nav_floor",
						X:    320,
						Y:    264,
						Components: map[string]any{
							sceneformat.NavRegion2DComponentName: map[string]any{
								"version": sceneformat.NavRegion2DComponentVersion,
								"mode":    "walkable",
								"points":  [][2]float64{{-250, -14}, {250, -14}, {250, 14}, {-250, 14}},
							},
						},
					},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "lever_01", Event: "pulled", To: "door_01", Handler: "open"},
		},
	}

	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)
	ed.SelectObject("slime_01")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Screenshot("viewport-overlays-before")

	app.Press("id:viewport-overlays-button")
	app.Settle()
	if !ed.OverlaysPopoverOpen() {
		t.Fatalf("expected overlays popover to open after pressing viewport-overlays-button")
	}
	app.Press("id:viewport-overlay-switch-connections")
	app.Settle()
	app.Press("id:viewport-overlay-switch-patrol_paths")
	app.Settle()
	app.Press("id:viewport-overlay-switch-labels")
	app.Settle()

	if !ed.ViewportOverlayEnabled(editor.OverlayConnections) ||
		!ed.ViewportOverlayEnabled(editor.OverlayPatrolPaths) ||
		!ed.ViewportOverlayEnabled(editor.OverlayLabels) {
		t.Fatalf("expected toggled overlays to be enabled: %v", ed.ActiveViewportOverlays())
	}

	app.Screenshot("viewport-overlays-popover")
}

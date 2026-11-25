package editormcp_test

import (
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func countInteriorColorPixels(img image.Image, inset int, match func(c color.NRGBA) bool) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y + inset; y < b.Max.Y-inset; y++ {
		for x := b.Min.X + inset; x < b.Max.X-inset; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if match(c) {
				n++
			}
		}
	}
	return n
}

func isAccentBlue(c color.NRGBA) bool {
	// Spec accent #3B7BFF (R=59, G=123, B=255) — allow anti-aliased edge tolerance.
	return c.A > 180 && c.B > 200 && c.R < 110 && c.G > 80 && c.G < 170
}

func isPillTextLight(c color.NRGBA) bool {
	// Spec text #D9DDE5 (R=217, G=221, B=229) drawn inside connection/object pills.
	return c.A > 200 && c.R > 190 && c.G > 195 && c.B > 205
}

func TestViewportImageOverlaysConnectionsArrowAndSharedSet(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#15181D",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "button_01", Name: "button_01", X: 70, Y: 130},
					{ID: "door_01", Name: "door_01", X: 250, Y: 130},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{From: "button_01", Event: "pulled", To: "door_01", Handler: "open"},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(320, 180)

	// Also register a custom overlay to prove viewport_overlays supports plugin/custom IDs.
	customDraws := 0
	if err := ed.RegisterViewportOverlay(editor.ViewportOverlay{
		ID:   "spawn_ring",
		Name: "Spawn ring",
		Draw: func(_ *editor.Editor, d *core.Drawing, _ *sceneformat.BuiltScene) {
			customDraws++
			d.SetStrokeColor(core.RGB(0x4FD38A)).SetLineWidth(2)
			d.BeginPath().Arc(160, 90, 24, 0, 6.2831853, false).Stroke()
		},
	}); err != nil {
		t.Fatalf("RegisterViewportOverlay: %v", err)
	}

	s := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// 1. With viewport_overlays: [], no connection arrow or text pixels are drawn.
	resEmpty := call(t, cs, editormcp.ToolViewportImage, map[string]any{
		"viewport_overlays": []string{},
	})
	mustOK(t, resEmpty)
	imgEmpty := pngOf(t, resEmpty)
	accentEmpty := countInteriorColorPixels(imgEmpty, 8, isAccentBlue)
	if textEmpty := countInteriorColorPixels(imgEmpty, 8, isPillTextLight); textEmpty != 0 {
		t.Fatalf("expected 0 interior label pixels with empty viewport_overlays, got %d", textEmpty)
	}

	// 2. Requesting viewport_overlays: ["connections"] draws the curved arrow and "pulled → open" label
	// without mutating the editor's active overlay set.
	resConn := call(t, cs, editormcp.ToolViewportImage, map[string]any{
		"viewport_overlays": []string{editor.OverlayConnections},
	})
	mustOK(t, resConn)
	var infoConn editormcp.ViewportImage
	structured(t, resConn, &infoConn)
	if !slices.Equal(infoConn.ViewportOverlays, []string{editor.OverlayConnections}) {
		t.Fatalf("ViewportOverlays = %v, want [connections]", infoConn.ViewportOverlays)
	}
	if infoConn.SkippedOps["DrawFillText"] != 0 {
		t.Fatalf("expected DrawFillText to be rendered into PNG, got SkippedOps=%v", infoConn.SkippedOps)
	}
	imgConn := pngOf(t, resConn)
	accentConn := countInteriorColorPixels(imgConn, 8, isAccentBlue)
	if added := accentConn - accentEmpty; added < 100 {
		t.Fatalf("expected >= 100 added accent blue connection arrow pixels, got %d (base=%d, with=%d)", added, accentEmpty, accentConn)
	}
	labelPixels := countInteriorColorPixels(imgConn, 8, isPillTextLight)
	if labelPixels < 30 {
		t.Fatalf("expected legible \"pulled → open\" label pixels inside pill, got %d", labelPixels)
	}
	if ed.ViewportOverlayEnabled(editor.OverlayConnections) {
		t.Fatal("viewport_image with viewport_overlays override mutated editor's active overlay set")
	}

	// 3. Turning on Connections in the editor makes default viewport_image (omitting viewport_overlays)
	// draw the exact same arrow automatically ("The agent's screenshots use the same set").
	if err := ed.SetViewportOverlay(editor.OverlayConnections, true); err != nil {
		t.Fatalf("SetViewportOverlay(connections, true): %v", err)
	}
	resShared := call(t, cs, editormcp.ToolViewportImage, map[string]any{})
	mustOK(t, resShared)
	var infoShared editormcp.ViewportImage
	structured(t, resShared, &infoShared)
	if !slices.Contains(infoShared.ViewportOverlays, editor.OverlayConnections) {
		t.Fatalf("default viewport_image missing active connections overlay: %v", infoShared.ViewportOverlays)
	}
	imgShared := pngOf(t, resShared)
	if got := countInteriorColorPixels(imgShared, 8, isAccentBlue); got != accentConn {
		t.Fatalf("shared default viewport_image drew %d arrow pixels, explicit connections drew %d", got, accentConn)
	}

	// 4. Custom/plugin overlay renders when named in viewport_overlays.
	resCustom := call(t, cs, editormcp.ToolViewportImage, map[string]any{
		"viewport_overlays": []string{"spawn_ring"},
	})
	mustOK(t, resCustom)
	if customDraws != 1 {
		t.Fatalf("custom overlay Draw called %d times via viewport_image, want 1", customDraws)
	}

	// 5. Unknown overlay name is refused as a tool error.
	resBad := call(t, cs, editormcp.ToolViewportImage, map[string]any{
		"viewport_overlays": []string{"nonexistent_overlay"},
	})
	if !resBad.IsError {
		t.Fatal("expected unknown viewport_overlays name to return a tool error")
	}

	// 6. In 3D view mode, omitting viewport_overlays reports no 2D overlays, and explicit
	// viewport_overlays returns a clear tool error instead of silently ignoring them.
	ed.SetViewMode(editor.ViewMode3D)
	res3D := call(t, cs, editormcp.ToolViewportImage, map[string]any{})
	mustOK(t, res3D)
	var info3D editormcp.ViewportImage
	structured(t, res3D, &info3D)
	if info3D.ViewMode != "3D" {
		t.Fatalf("3D viewport_image ViewMode = %q, want 3D", info3D.ViewMode)
	}
	if len(info3D.ViewportOverlays) != 0 {
		t.Fatalf("3D viewport_image reported 2D ViewportOverlays = %v, want empty", info3D.ViewportOverlays)
	}
	res3DOverride := call(t, cs, editormcp.ToolViewportImage, map[string]any{
		"viewport_overlays": []string{editor.OverlayConnections},
	})
	if !res3DOverride.IsError {
		t.Fatal("expected explicit viewport_overlays in 3D view mode to return a tool error")
	}
}

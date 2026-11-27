package editor_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestViewport3DScreenshots renders a 3D courtyard scene on the real native macOS host
// matching design/editor/Viewport3D.dc.html and Spec.dc.html across Perspective,
// Top · ortho, and User · ortho views.
func TestViewport3DScreenshots(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "courtyard",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#14171D",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "fountain", Name: "Fountain", X: 0, Y: 1.2, PosZ: 0, ScaleX: sceneformat.Float32(4), ScaleY: sceneformat.Float32(2.4), ScaleZ: sceneformat.Float32(4), Mesh: "box"},
					{ID: "gate", Name: "Gate", X: 0, Y: 1.5, PosZ: 8, ScaleX: sceneformat.Float32(4.6), ScaleY: sceneformat.Float32(3), ScaleZ: sceneformat.Float32(0.8), Mesh: "box"},
					{ID: "wall_n", Name: "Wall North", X: 0, Y: 0.6, PosZ: -8, ScaleX: sceneformat.Float32(16.6), ScaleY: sceneformat.Float32(1.2), ScaleZ: sceneformat.Float32(0.6), Mesh: "box"},
					{ID: "wall_w", Name: "Wall West", X: -8, Y: 0.6, PosZ: 0, ScaleX: sceneformat.Float32(0.6), ScaleY: sceneformat.Float32(1.2), ScaleZ: sceneformat.Float32(15.4), Mesh: "box"},
					{ID: "wall_e", Name: "Wall East", X: 8, Y: 0.6, PosZ: 0, ScaleX: sceneformat.Float32(0.6), ScaleY: sceneformat.Float32(1.2), ScaleZ: sceneformat.Float32(15.4), Mesh: "box"},
					{ID: "lantern_ne", Name: "Lantern NE", X: 7, Y: 1.3, PosZ: -7, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(2.6), ScaleZ: sceneformat.Float32(0.5), Mesh: "cylinder"},
					{ID: "lantern_nw", Name: "Lantern NW", X: -7, Y: 1.3, PosZ: -7, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(2.6), ScaleZ: sceneformat.Float32(0.5), Mesh: "cylinder"},
					{ID: "lantern_se", Name: "Lantern SE", X: 7, Y: 1.3, PosZ: 7, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(2.6), ScaleZ: sceneformat.Float32(0.5), Mesh: "cylinder"},
					{ID: "lantern_sw", Name: "Lantern SW", X: -7, Y: 1.3, PosZ: 7, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(2.6), ScaleZ: sceneformat.Float32(0.5), Mesh: "cylinder"},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0.8, 0},
		Yaw:      float32(35 * math.Pi / 180),
		Pitch:    float32(28 * math.Pi / 180),
		Distance: 24,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      1000,
	}
	ed.SelectObject("fountain")
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Perspective · yaw 35° · pitch 28°") || !strings.Contains(got, " · Local") {
		t.Fatalf("status = %q, want Perspective · yaw 35° · pitch 28° and Local", got)
	}
	app.Screenshot("viewport3d-perspective")

	app.Press("id:view3d-axis-top")
	app.Press("id:view3d-ortho")
	app.Settle()
	if got := app.Find("id:view3d-name").Text; got != "Top · ortho" {
		t.Fatalf("view3d-name = %q, want Top · ortho", got)
	}
	app.Screenshot("viewport3d-top-ortho")

	app.Press("id:view3d-orbit-right")
	app.Settle()
	if got := app.Find("id:view3d-name").Text; got != "User · ortho" {
		t.Fatalf("view3d-name = %q, want User · ortho", got)
	}
	app.Screenshot("viewport3d-user-ortho")
}

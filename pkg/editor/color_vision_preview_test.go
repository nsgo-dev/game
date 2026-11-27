package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestEditorColorVisionPreviewViewportAndHUD(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "cvd_scene",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{Name: "world", Space: sceneformat.SpaceWorld},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := ed.ColorVisionPreview(); got != a11y.VisionNormal {
		t.Fatalf("default ColorVisionPreview = %v, want VisionNormal", got)
	}
	if _, ok := ed.ColorVisionPreviewEffect(); ok {
		t.Fatal("expected ColorVisionPreviewEffect to return false when Normal")
	}

	// Cycling traverses Normal -> Protanopia -> Deuteranopia -> Tritanopia -> Normal.
	wantCycle := []a11y.ColorVision{
		a11y.VisionProtanopia,
		a11y.VisionDeuteranopia,
		a11y.VisionTritanopia,
		a11y.VisionNormal,
	}
	for _, want := range wantCycle {
		if got := ed.CycleColorVisionPreview(); got != want {
			t.Fatalf("CycleColorVisionPreview = %v, want %v", got, want)
		}
	}

	// Open the full EditorView and verify viewport color-vision overlay + badge + reset.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	if app.Exists("id:viewport-color-vision-layer") {
		t.Fatal("viewport-color-vision-layer should not mount in Normal mode")
	}

	// Switch via the overlays popover button.
	app.Post(func() { ed.SetOverlaysPopoverOpen(true) })
	app.Settle()
	app.Press("id:viewport-color-vision-deuteranopia")
	app.Settle()

	if got := ed.ColorVisionPreview(); got != a11y.VisionDeuteranopia {
		t.Fatalf("ColorVisionPreview after press = %v, want VisionDeuteranopia", got)
	}
	if _, ok := ed.ColorVisionPreviewEffect(); !ok {
		t.Fatal("expected ColorVisionPreviewEffect to return true for Deuteranopia")
	}
	if !app.Exists("id:viewport-color-vision-layer") || !app.Exists("id:viewport-color-vision-shader") {
		t.Fatal("expected viewport color-vision shader overlay to mount when Deuteranopia is active")
	}
	if !app.Exists("id:viewport-color-vision-badge") {
		t.Fatal("expected viewport-color-vision-badge when Deuteranopia is active")
	}

	// Clear button resets back to Normal and unmounts the overlay.
	app.Press("id:viewport-color-vision-clear")
	app.Settle()
	if got := ed.ColorVisionPreview(); got != a11y.VisionNormal {
		t.Fatalf("ColorVisionPreview after clear = %v, want VisionNormal", got)
	}
	if app.Exists("id:viewport-color-vision-layer") {
		t.Fatal("expected viewport-color-vision-layer to unmount after clearing")
	}
}

func TestHUDLayoutColorVisionToggleAndSimulatedContrast(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("game.yaml", "version: 1\nid: dev.example.cvdhud\nname: cvd-hud\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\ndisplay:\n  width: 640\n  height: 360\n")
	mustWrite("locales/en.yaml", "version: 1\nlocale: en\nstrings:\n  hud.hp: HP 100\n")
	mustWrite("screens/hud.screen.yaml", `version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: hp_label
    type: label
    anchor: top_left
    width: 120
    height: 28
    textKey: hud.hp
    textColor: "#cc2222"
    background: "#22aa22"
`)

	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}

	rowsNormal := ed.HUDContrastRows()
	if len(rowsNormal) != 1 || rowsNormal[0].NodeID != "hp_label" {
		t.Fatalf("HUDContrastRows = %+v, want 1 row for hp_label", rowsNormal)
	}
	if math.Abs(rowsNormal[0].NormalRatio-rowsNormal[0].SimulatedRatio) > 1e-6 {
		t.Fatalf("Normal mode should have equal normal and simulated contrast: %+v", rowsNormal[0])
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	if !app.Exists("id:hud-color-vision-panel") || !app.Exists("id:hud-contrast-row-hp_label") {
		t.Fatal("expected hud-color-vision-panel and contrast row to render")
	}

	// Capture canvas ops before and after selecting Protanopia in HUD Layout.
	opsBefore := app.Find("id:hud-multisize-canvas").Drawing.Ops
	app.Press("id:hud-color-vision-protanopia")
	app.Settle()

	if got := ed.ColorVisionPreview(); got != a11y.VisionProtanopia {
		t.Fatalf("ColorVisionPreview = %v, want VisionProtanopia", got)
	}
	rowsProt := ed.HUDContrastRows()
	if len(rowsProt) != 1 {
		t.Fatalf("HUDContrastRows(protanopia) len = %d, want 1", len(rowsProt))
	}
	wantSimFG := a11y.Simulate(core.RGB(0xcc2222), a11y.VisionProtanopia)
	wantSimBG := a11y.Simulate(core.RGB(0x22aa22), a11y.VisionProtanopia)
	if rowsProt[0].SimForeground != wantSimFG || rowsProt[0].SimBackground != wantSimBG {
		t.Fatalf("simulated colors = (%#08x, %#08x), want (%#08x, %#08x)",
			uint32(rowsProt[0].SimForeground), uint32(rowsProt[0].SimBackground),
			uint32(wantSimFG), uint32(wantSimBG))
	}

	// Verify the HUD multi-size canvas actually emitted the simulated fill color for hp_label.
	opsAfter := app.Find("id:hud-multisize-canvas").Drawing.Ops
	hasColor := func(ops []core.DrawOp, want core.Color) bool {
		for _, op := range ops {
			if op.Kind == core.DrawFillColor && op.Color == want {
				return true
			}
		}
		return false
	}
	if !hasColor(opsBefore, core.RGB(0x22aa22)) {
		t.Fatal("expected normal HUD canvas to draw original green background #22aa22")
	}
	if !hasColor(opsAfter, wantSimBG) {
		t.Fatalf("expected protanopia HUD canvas to draw simulated background %#08x", uint32(wantSimBG))
	}
}

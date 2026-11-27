package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCommandPaletteNodesScopesSettingsRecentAndAskAgent(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)

	// 1. PaletteItems includes scene nodes and settings pages.
	items := ed.PaletteItems()
	hasKnightNode := false
	hasViewportSetting := false
	for _, it := range items {
		if it.ID == "node:knight" {
			hasKnightNode = true
		}
		if it.ID == "setting:viewport" {
			hasViewportSetting = true
		}
	}
	if !hasKnightNode {
		t.Fatal("PaletteItems missing scene node item node:knight")
	}
	if !hasViewportSetting {
		t.Fatal("PaletteItems missing settings page item setting:viewport")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// 2. '@' scope jumps directly to a scene node, selects it, switches to Inspector, and records recent.
	app.Key("meta+k")
	app.Settle()
	app.Type("id:editor-command-palette >> role:textbox", "@door")
	app.Settle()
	app.Key("enter")
	app.Settle()
	if ed.CommandPaletteOpen() {
		t.Fatal("selecting @door did not close Command Palette")
	}
	if ed.SelectedID != "door_trigger" {
		t.Fatalf("SelectedID after @door = %q, want door_trigger", ed.SelectedID)
	}
	if ed.RightRailTab() != editor.RightTabInspector {
		t.Fatalf("RightRailTab after @door = %v, want RightTabInspector", ed.RightRailTab())
	}
	recent := ed.PaletteRecent()
	if len(recent) == 0 || recent[0] != "node:door_trigger" {
		t.Fatalf("PaletteRecent() = %v, want node:door_trigger first", recent)
	}

	// 3. Execute setting:viewport via ExecutePaletteItem opens Settings on Viewport page.
	app.Post(func() {
		if err := ed.ExecutePaletteItem("setting:viewport"); err != nil {
			t.Errorf("ExecutePaletteItem(setting:viewport): %v", err)
		}
	})
	app.Settle()
	if !ed.SettingsOpen() || ed.SettingsPage() != editor.SettingsPageViewport {
		t.Fatalf("Settings after setting:viewport: open=%v page=%q, want true/viewport", ed.SettingsOpen(), ed.SettingsPage())
	}
	app.Post(func() { ed.CloseSettings() })
	app.Settle()

	// 4. Natural-language query in Command Palette surfaces "Ask the agent" and stages prompt in Agent composer.
	app.Key("meta+k")
	app.Settle()
	app.Type("id:editor-command-palette >> role:textbox", "add two more patrolling slimes")
	app.Settle()
	app.Key("enter")
	app.Settle()
	if ed.RightRailTab() != editor.RightTabAgent {
		t.Fatalf("RightRailTab after ask query = %v, want RightTabAgent", ed.RightRailTab())
	}
	if got := ed.ComposerDraft(); got != "add two more patrolling slimes" {
		t.Fatalf("ComposerDraft() = %q, want %q", got, "add two more patrolling slimes")
	}
}

func TestSceneTreeQuickFilterAndQuickAdd(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// 1. Toggle filter bar and filter for "slime".
	app.Press("id:scene-rail-filter-toggle")
	app.Settle()
	if !ed.SceneTreeFilterOpen() || !app.Exists("id:scene-tree-filter-input") {
		t.Fatal("clicking scene-rail-filter-toggle did not open scene tree filter input")
	}
	app.Post(func() { ed.SetSceneTreeFilter("slime") })
	app.Settle()
	if !app.Exists("id:scene-tree-label-slime_01") {
		t.Fatal("filtered tree missing matching node slime_01")
	}
	if app.Exists("id:scene-tree-label-knight") {
		t.Fatal("filtered tree should hide non-matching node knight")
	}

	// Non-matching query displays empty message.
	app.Post(func() { ed.SetSceneTreeFilter("no-such-node-xyz") })
	app.Settle()
	if !app.Exists("id:scene-tree-filter-empty") {
		t.Fatal("missing scene-tree-filter-empty for unmatched filter query")
	}

	// Closing filter bar clears active filter and restores all nodes.
	app.Press("id:scene-rail-filter-toggle")
	app.Settle()
	if ed.SceneTreeFilter() != "" || !app.Exists("id:scene-tree-label-knight") {
		t.Fatalf("closing filter bar did not clear filter (%q) and restore knight", ed.SceneTreeFilter())
	}

	// 2. Quick-add empty object, trigger, light, and layer (each undoable through History).
	beforeCount := ed.SceneNodeCount()
	var addedID string
	app.Post(func() {
		var err error
		addedID, err = ed.AddSceneRailObject("trigger")
		if err != nil {
			t.Errorf("AddSceneRailObject(trigger): %v", err)
		}
	})
	app.Settle()
	if addedID == "" || ed.SelectedID != addedID {
		t.Fatalf("AddSceneRailObject(trigger) did not select new object: id=%q selected=%q", addedID, ed.SelectedID)
	}
	if got := ed.SceneRailObjectKind(addedID); got != "trigger" {
		t.Fatalf("added trigger kind = %q, want trigger", got)
	}
	if ed.SceneNodeCount() != beforeCount+1 {
		t.Fatalf("SceneNodeCount = %d, want %d", ed.SceneNodeCount(), beforeCount+1)
	}

	// Undo removes the newly added object.
	app.Post(func() { ed.Undo() })
	app.Settle()
	if ed.SceneNodeCount() != beforeCount {
		t.Fatalf("after Undo: SceneNodeCount = %d, want %d", ed.SceneNodeCount(), beforeCount)
	}

	// Quick-add layer creates a unique layer and activates it.
	var newLayer string
	app.Post(func() {
		var err error
		newLayer, err = ed.AddSceneRailLayer()
		if err != nil {
			t.Errorf("AddSceneRailLayer: %v", err)
		}
	})
	app.Settle()
	if newLayer == "" || ed.ActiveLayer != newLayer {
		t.Fatalf("AddSceneRailLayer: layer=%q active=%q", newLayer, ed.ActiveLayer)
	}
}

func TestInspectorCheckboxVisibleLabelsAndSceneTreeNoClipping(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)
	ed.SetProposedNewObjectIDs([]string{"slime_01"})
	ed.SelectObject("knight")
	ed.SetRightRailTab(editor.RightTabInspector)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// 1. Inspector Flip X and Flip Y checkboxes must display visible "X" and "Y" text.
	if got := strings.TrimSpace(allText(app.Find("id:inspector-flip-x"))); got != "X" {
		t.Fatalf("inspector-flip-x visible text = %q, want %q", got, "X")
	}
	if got := strings.TrimSpace(allText(app.Find("id:inspector-flip-y"))); got != "Y" {
		t.Fatalf("inspector-flip-y visible text = %q, want %q", got, "Y")
	}

	// 2. Scene tree row with NEW + prefab + eye + lock must NOT clip lock button outside the 232px left rail.
	railBounds := app.Find("id:editor-scene-column").Bounds()
	lockBounds := app.Find("id:scene-tree-lock-slime_01").Bounds()
	railRight := railBounds.Origin.X + railBounds.Size.Width
	lockRight := lockBounds.Origin.X + lockBounds.Size.Width
	if lockRight > railRight {
		t.Fatalf("scene-tree-lock-slime_01 right edge %.1f overflows left rail right edge %.1f", lockRight, railRight)
	}
}

func TestViewportCameraCaptionReactivePositionAndDotGrid(t *testing.T) {
	ed, _ := setupMainLayoutProject(t)
	ed.SetOverlayLegendOpen(true)

	// Verify SetCanvasSize fires SubscribeCanvasSize without bumping Editor.Revision.
	revBefore := ed.Revision()
	sizeHits := 0
	unsub := ed.SubscribeCanvasSize(func() { sizeHits++ })
	defer unsub()
	ed.SetCanvasSize(700, 500)
	if sizeHits != 1 {
		t.Fatalf("SubscribeCanvasSize calls = %d, want 1", sizeHits)
	}
	if ed.Revision() != revBefore {
		t.Fatalf("SetCanvasSize unexpectedly bumped Revision from %d to %d", revBefore, ed.Revision())
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	// Camera caption must NOT sit on top of the top-left overlay legend (Y >= 40 or above camera rect).
	captionBounds := app.Find("id:viewport-camera-caption").Bounds()
	legendBounds := app.Find("id:viewport-overlay-legend").Bounds()
	overlapX := captionBounds.Origin.X < legendBounds.Origin.X+legendBounds.Size.Width &&
		captionBounds.Origin.X+captionBounds.Size.Width > legendBounds.Origin.X
	overlapY := captionBounds.Origin.Y < legendBounds.Origin.Y+legendBounds.Size.Height &&
		captionBounds.Origin.Y+captionBounds.Size.Height > legendBounds.Origin.Y
	if overlapX && overlapY {
		t.Fatalf("viewport-camera-caption %+v overlaps viewport-overlay-legend %+v", captionBounds, legendBounds)
	}

	// Verify 2D edit DrawViewport renders ViewportDot backdrop and neutral non-sprite markers.
	var editDraw core.Drawing
	ed.DrawViewport(&editDraw, core.Size{Width: 800, Height: 600})
	spec := editor.DarkSpecPalette()
	foundDot := false
	foundMarkerFill := false
	for _, op := range editDraw.Ops {
		if (op.Kind == core.DrawStrokeColor || op.Kind == core.DrawFillColor) && op.Color == spec.ViewportDot {
			foundDot = true
		}
		if op.Kind == core.DrawFillColor && op.Color == spec.Raised {
			foundMarkerFill = true
		}
	}
	if !foundDot {
		t.Fatal("2D edit DrawViewport did not draw Spec ViewportDot grid")
	}
	if !foundMarkerFill {
		t.Fatal("2D edit DrawViewport did not draw neutral markers for non-sprite objects")
	}
}

func TestFirstRunConnectStepWrapsInsideCard(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetStartScreenOpen(true)
	ed.SetStartProjectIdea("A short dungeon platformer")
	ed.SetStartProjectName("dungeon-run")
	// A long, writable parent directory makes the connect step's path text long enough to need wrapping on every host.
	parent := filepath.Join(t.TempDir(), "VeryLongTestDirectoryNameForWrappingCheck", "AnotherLongSegmentSoThePathWrapsInsideTheCard")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	ed.SetStartProjectParentDir(parent)
	if err := ed.StartWithAI(); err != nil {
		t.Fatalf("StartWithAI: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), focusDesktopEnv)
	app.Settle()

	cardBounds := app.Find("id:first-run-connect-card").Bounds()
	stepBounds := app.Find("id:first-run-connect-step-0").Bounds()
	cardRight := cardBounds.Origin.X + cardBounds.Size.Width
	stepRight := stepBounds.Origin.X + stepBounds.Size.Width
	if stepRight > cardRight {
		t.Fatalf("first-run-connect-step-0 right edge %.1f overflows card right edge %.1f", stepRight, cardRight)
	}
}

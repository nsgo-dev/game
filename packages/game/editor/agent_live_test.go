package editor_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const agentLiveTestScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#15181D"
layers:
  - name: Actors
    space: world
    order: 0
    objects:
      - id: knight
        name: Knight
        x: 240
        y: 230
        z: 10
      - id: door
        name: Door
        x: 520
        y: 248
        z: 5
      - id: coin_04
        name: coin_04
        x: 360
        y: 260
        z: 2
`

func newAgentLiveTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	doc, err := sceneformat.Parse([]byte(agentLiveTestScene))
	if err != nil {
		t.Fatalf("parse test scene: %v", err)
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)
	return ed
}

func findOverlay(list []editor.ViewportLiveOverlay, kind editor.OverlayLegendKind, objectID string) (editor.ViewportLiveOverlay, bool) {
	for _, ov := range list {
		if ov.Kind == kind && ov.ObjectID == objectID {
			return ov, true
		}
	}
	return editor.ViewportLiveOverlay{}, false
}

func TestAgentLiveTurnOverlaysTreeBadgesAndLegendPersistence(t *testing.T) {
	ed := newAgentLiveTestEditor(t)
	settingsPath := filepath.Join(t.TempDir(), "editor.yaml")
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	// Start a pending turn and apply add + change + remove as the agent.
	ed.BeginAgentTurn("Place slime and adjust door")
	if err := ed.RunAsAgent(func() error {
		if _, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{
			ID:   "slime_01",
			Name: "slime_01",
			X:    300,
			Y:    260,
		}); err != nil {
			return err
		}
		if err := ed.SetObjectZ("door", 12); err != nil {
			return err
		}
		return ed.DeleteObject("coin_04")
	}); err != nil {
		t.Fatalf("agent edits: %v", err)
	}
	app.Settle()

	// Inspectable overlay list reports Added, Changed, and Removed with Spec properties.
	all := ed.ViewportLiveOverlays()
	addedOv, ok := findOverlay(all, editor.OverlayLegendAdded, "slime_01")
	if !ok || !addedOv.Dashed || !addedOv.Visible || addedOv.Label != "+ slime_01" {
		t.Fatalf("added overlay = %+v (ok=%v), want dashed visible '+ slime_01'", addedOv, ok)
	}
	changedOv, ok := findOverlay(all, editor.OverlayLegendChanged, "door")
	if !ok || !changedOv.Dashed || !changedOv.Visible || changedOv.Label != "z 5 → 12" {
		t.Fatalf("changed overlay = %+v (ok=%v), want dashed visible 'z 5 → 12'", changedOv, ok)
	}
	removedOv, ok := findOverlay(all, editor.OverlayLegendRemoved, "coin_04")
	if !ok || removedOv.Dashed || !removedOv.Ghost || !removedOv.Visible {
		t.Fatalf("removed overlay = %+v (ok=%v), want solid ghost visible", removedOv, ok)
	}

	// Scene tree shows NEW, MOD, and DEL markers.
	if got := app.Find("id:scene-tree-agent-badge-slime_01").Text; got != "NEW" {
		t.Fatalf("slime_01 tree badge = %q, want NEW", got)
	}
	if got := app.Find("id:scene-tree-agent-badge-door").Text; got != "MOD" {
		t.Fatalf("door tree badge = %q, want MOD", got)
	}
	if got := app.Find("id:scene-tree-agent-badge-coin_04").Text; got != "DEL" {
		t.Fatalf("coin_04 tree badge = %q, want DEL", got)
	}

	// Overlay legend is shown automatically in top-left and turning off Added hides only Added.
	if app.Find("id:viewport-overlay-legend") == nil {
		t.Fatal("expected viewport-overlay-legend to show automatically while agent overlays are active")
	}
	app.Press("id:legend-added")
	app.Settle()

	if ed.OverlayLegendKindEnabled(editor.OverlayLegendAdded) {
		t.Fatal("expected OverlayLegendAdded to be disabled after pressing legend-added")
	}
	vis := ed.VisibleViewportLiveOverlays()
	if _, stillVisible := findOverlay(vis, editor.OverlayLegendAdded, "slime_01"); stillVisible {
		t.Fatal("expected Added overlay to be hidden from VisibleViewportLiveOverlays after toggling legend off")
	}
	if _, stillVisible := findOverlay(vis, editor.OverlayLegendChanged, "door"); !stillVisible {
		t.Fatal("expected Changed overlay to remain visible when only Added was toggled off")
	}

	// Verify legend choice persisted in editor.yaml and round-trips cleanly.
	loaded, err := editor.LoadEditorSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadEditorSettings: %v", err)
	}
	if len(loaded.HiddenOverlayKinds) != 1 || loaded.HiddenOverlayKinds[0] != string(editor.OverlayLegendAdded) {
		t.Fatalf("persisted HiddenOverlayKinds = %v, want [added]", loaded.HiddenOverlayKinds)
	}
	encoded, err := editor.EncodeEditorSettings(loaded)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	reparsed, err := editor.ParseEditorSettings(encoded)
	if err != nil {
		t.Fatalf("ParseEditorSettings round-trip: %v", err)
	}
	if len(reparsed.HiddenOverlayKinds) != 1 || reparsed.HiddenOverlayKinds[0] != "added" {
		t.Fatalf("round-tripped HiddenOverlayKinds = %v, want [added]", reparsed.HiddenOverlayKinds)
	}
}

func TestAgentSelectionCoexistenceAndReadOnlyGuard(t *testing.T) {
	ed := newAgentLiveTestEditor(t)
	ed.SetCanvasSize(640, 360)
	ed.SelectObject("knight")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	// Mark knight as currently touched by the agent.
	ed.BeginAgentStep([]string{"knight"}, "tuning Knight")
	app.Settle()

	// Selection keeps its handles AND agent outline sits 3 px further out (6 px vs 3 px).
	if !ed.Gizmo2DShown() {
		t.Fatal("expected selection to keep its 2D gizmo handles while agent is working on the same object")
	}
	all := ed.ViewportLiveOverlays()
	selOv, okSel := findOverlay(all, editor.OverlayLegendSelection, "knight")
	agentOv, okAgent := findOverlay(all, editor.OverlayLegendAgent, "knight")
	if !okSel || !okAgent {
		t.Fatalf("expected both Selection and Agent overlays on knight, got %+v", all)
	}
	if agentOv.OutsetPx != 6 || selOv.OutsetPx != 0 || !selOv.ReadOnly || !agentOv.ReadOnly {
		t.Fatalf("coexistence outsets/readOnly mismatch: sel=%+v agent=%+v", selOv, agentOv)
	}
	if agentOv.Label != "tuning Knight" {
		t.Fatalf("agent pill label = %q, want %q", agentOv.Label, "tuning Knight")
	}

	// Inspector shows the read-only banner and keeps Delete disabled (pressing does not remove knight).
	if app.Find("id:inspector-agent-working-banner") == nil {
		t.Fatal("expected inspector-agent-working-banner when inspecting agent-touched object")
	}
	if app.Find("id:inspector-delete") == nil {
		t.Fatal("expected inspector-delete button to be rendered")
	}

	// Manual edits on knight are refused across viewport drag, move, property, scrub, rename, delete.
	if err := ed.MoveObject("knight", 100, 100); !errors.Is(err, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual MoveObject err = %v, want ErrAgentTouchingObject", err)
	}
	if err := ed.SetObjectZ("knight", 99); !errors.Is(err, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual SetObjectZ err = %v, want ErrAgentTouchingObject", err)
	}
	if err := ed.SetObjectName("knight", "Other"); !errors.Is(err, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual SetObjectName err = %v, want ErrAgentTouchingObject", err)
	}
	if err := ed.DeleteObject("knight"); !errors.Is(err, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual DeleteObject err = %v, want ErrAgentTouchingObject", err)
	}
	if err := ed.BeginInspectorScrub(editor.InspectorScrubField{ObjectID: "knight", Field: "x", Step: 1}); !errors.Is(err, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual BeginInspectorScrub err = %v, want ErrAgentTouchingObject", err)
	}
	kx, ky := ed.WorldToCanvas(240, 230)
	if h := ed.BeginDrag2D(kx, ky); h != editor.Handle2DNone {
		t.Fatalf("manual BeginDrag2D on agent-touched object returned %v, want Handle2DNone", h)
	}

	// Manual edits on an UNTOUCHED object stay available!
	if err := ed.MoveObject("door", 500, 240); err != nil {
		t.Fatalf("manual MoveObject on untouched door failed: %v", err)
	}

	// Agent edits through RunAsAgent are allowed even while knight is marked.
	if err := ed.RunAsAgent(func() error {
		return ed.MoveObject("knight", 260, 230)
	}); err != nil {
		t.Fatalf("RunAsAgent MoveObject on knight failed: %v", err)
	}

	// Once the agent's step moves on, manual edits to knight are allowed again.
	ed.EndAgentStep()
	app.Settle()

	if err := ed.MoveObject("knight", 280, 235); err != nil {
		t.Fatalf("manual MoveObject after EndAgentStep failed: %v", err)
	}
	obj, _, _ := ed.FindObject("knight")
	if obj == nil || obj.X != 280 || obj.Y != 235 {
		t.Fatalf("knight position after manual move = %+v, want (280, 235)", obj)
	}
}

func TestAgentTurnKeepAndRevertClearOverlaysAndTree(t *testing.T) {
	t.Run("keep collapses to one undo step and clears overlays", func(t *testing.T) {
		ed := newAgentLiveTestEditor(t)
		beforeIdx := ed.History.Index()

		ed.BeginAgentTurn("Add slime and update door")
		if err := ed.RunAsAgent(func() error {
			if _, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "slime_01", Name: "slime_01", X: 300, Y: 260}); err != nil {
				return err
			}
			return ed.SetObjectZ("door", 12)
		}); err != nil {
			t.Fatal(err)
		}
		if !ed.HasAgentLiveOverlays() {
			t.Fatal("expected live overlays before Keep")
		}

		if err := ed.KeepAgentTurn(); err != nil {
			t.Fatalf("KeepAgentTurn: %v", err)
		}
		if ed.AgentTurnPending() || ed.HasAgentLiveOverlays() || len(ed.AgentTurnChanges()) != 0 {
			t.Fatal("expected KeepAgentTurn to clear pending turn and live overlays")
		}
		if got := ed.History.Index(); got != beforeIdx+1 {
			t.Fatalf("history index after Keep = %d, want %d (single undo step)", got, beforeIdx+1)
		}

		// One Undo reverts both edits.
		if !ed.Undo() {
			t.Fatal("expected Undo to succeed")
		}
		if obj, _, _ := ed.FindObject("slime_01"); obj != nil {
			t.Fatal("expected single Undo after Keep to remove slime_01")
		}
		if door, _, _ := ed.FindObject("door"); door == nil || door.Z != 5 {
			t.Fatalf("expected single Undo after Keep to restore door.Z=5, got %+v", door)
		}
	})

	t.Run("revert restores scene and clears overlays", func(t *testing.T) {
		ed := newAgentLiveTestEditor(t)
		ed.BeginAgentTurn("Add, change, remove")
		if err := ed.RunAsAgent(func() error {
			if _, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "slime_02", Name: "slime_02", X: 310, Y: 260}); err != nil {
				return err
			}
			if err := ed.SetObjectZ("door", 20); err != nil {
				return err
			}
			return ed.DeleteObject("coin_04")
		}); err != nil {
			t.Fatal(err)
		}

		if err := ed.RevertAgentTurn(); err != nil {
			t.Fatalf("RevertAgentTurn: %v", err)
		}
		if ed.AgentTurnPending() || ed.HasAgentLiveOverlays() || len(ed.AgentTurnChanges()) != 0 {
			t.Fatal("expected RevertAgentTurn to clear pending turn and live overlays")
		}
		if obj, _, _ := ed.FindObject("slime_02"); obj != nil {
			t.Fatal("expected RevertAgentTurn to remove slime_02")
		}
		if door, _, _ := ed.FindObject("door"); door == nil || door.Z != 5 {
			t.Fatalf("expected RevertAgentTurn to restore door.Z=5, got %+v", door)
		}
		if coin, _, _ := ed.FindObject("coin_04"); coin == nil {
			t.Fatal("expected RevertAgentTurn to restore removed coin_04")
		}
	})
}

func TestAgentHeaderPillAndReducedMotionAndCameraInvariant(t *testing.T) {
	ed := newAgentLiveTestEditor(t)
	ed.SetCanvasSize(640, 360)
	viewBefore := ed.View2D()
	cam3DBefore := ed.Camera3D

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	// Idle: header pill hidden.
	if app.Exists("id:header-agent-pill") {
		t.Fatal("expected header-agent-pill to be hidden when agent is idle")
	}

	// Working state: matches Working.dc.html phrasing.
	ed.SetAgentState(editor.AgentStateWorking, "playing level_01 headless")
	app.Settle()
	pillText := app.Find("id:header-agent-pill-text").Text
	if !strings.Contains(pillText, "Agent is playing level_01 headless") || !strings.Contains(pillText, "manual edits stay available") {
		t.Fatalf("header pill text = %q, want Working.dc.html copy", pillText)
	}

	// Waiting state.
	ed.SetAgentState(editor.AgentStateWaiting, "run the game?")
	app.Settle()
	if got := app.Find("id:header-agent-pill-text").Text; !strings.Contains(got, "Waiting for you") || !strings.Contains(got, "run the game?") {
		t.Fatalf("waiting pill text = %q", got)
	}

	// Reduced motion keeps agent outline solid at 100%.
	rm := true
	ed.SetReducedMotionOverride(&rm)
	ed.BeginAgentStep([]string{"knight"}, "placing slime_02")
	if op := ed.AgentPulseOpacity(); op != 1.0 {
		t.Fatalf("AgentPulseOpacity under reduced motion = %v, want 1.0", op)
	}
	ed.EndAgentStep()

	// Camera never moves on the agent's behalf.
	if ed.View2D() != viewBefore {
		t.Fatalf("2D camera moved during agent activity: got %+v, want %+v", ed.View2D(), viewBefore)
	}
	if ed.Camera3D != cam3DBefore {
		t.Fatalf("3D camera moved during agent activity: got %+v, want %+v", ed.Camera3D, cam3DBefore)
	}
}

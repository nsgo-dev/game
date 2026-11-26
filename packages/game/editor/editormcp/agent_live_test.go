package editormcp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func findLiveOverlay(list []editor.ViewportLiveOverlay, kind editor.OverlayLegendKind, objectID string) (editor.ViewportLiveOverlay, bool) {
	for _, ov := range list {
		if ov.Kind == kind && ov.ObjectID == objectID {
			return ov, true
		}
	}
	return editor.ViewportLiveOverlay{}, false
}

// Acceptance #1013:
//   - Drives a turn through MCP that places one object and changes another, checks through the
//     inspectable overlay list that the viewport draws an added and a changed overlay and that
//     the tree shows NEW; turning off the Added row of the legend hides that overlay.
//   - A manual move of an object the agent is touching is refused, and allowed once the agent's
//     step moves on.
//   - Keep and Revert clear the overlays and tree markers.
//   - The editor never moves the viewport camera on the agent's behalf.
func TestMCPLiveTurnOverlaysLegendReadOnlyAndKeepRevert(t *testing.T) {
	f := openFixture(t)
	f.ed.SetCanvasSize(640, 360)
	_ = f.ed.SetAppearance(editor.AppearanceDark)
	_ = f.ed.SetLanguage(editor.LanguageEnglish)

	viewBefore := f.ed.View2D()
	cam3DBefore := f.ed.Camera3D

	var s *editormcp.Server
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = editormcp.New(f.ed, editormcp.Options{Run: cx.Post})
		cx.Cleanup(func() { _ = s.Close() })
		return editor.EditorView(cx, f.ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	cs := connect(t, s, nil)

	// Pump app.Settle while an MCP call posts work onto the editor's UI goroutine via cx.Post.
	callUI := func(tool string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		var res *mcp.CallToolResult
		done := make(chan struct{})
		go func() {
			defer close(done)
			res = call(t, cs, tool, args)
		}()
		for {
			app.Settle()
			select {
			case <-done:
				app.Settle()
				return res
			case <-time.After(2 * time.Millisecond):
			}
		}
	}
	runUI := func(fn func() error) error {
		var err error
		done := make(chan struct{})
		go func() {
			defer close(done)
			err = fn()
		}()
		for {
			app.Settle()
			select {
			case <-done:
				app.Settle()
				return err
			case <-time.After(2 * time.Millisecond):
			}
		}
	}

	// 1. Drive a turn through MCP that places one object (slime_01) and changes another (hero).
	mustOK(t, callUI(editormcp.ToolSceneEdit, map[string]any{
		"label": "Place slime_01 and raise hero z",
		"edits": []any{
			map[string]any{
				"op":    "add",
				"layer": "actors",
				"object": map[string]any{
					"id":   "slime_01",
					"name": "slime_01",
					"x":    220,
					"y":    120,
				},
			},
			map[string]any{
				"op": "set",
				"id": "hero",
				"fields": map[string]any{
					"z": 12,
				},
			},
		},
	}))
	app.Settle()

	// Camera never moved on the agent's behalf.
	if f.ed.View2D() != viewBefore {
		t.Fatalf("2D camera moved during MCP turn: got %+v, want %+v", f.ed.View2D(), viewBefore)
	}
	if f.ed.Camera3D != cam3DBefore {
		t.Fatalf("3D camera moved during MCP turn: got %+v, want %+v", f.ed.Camera3D, cam3DBefore)
	}

	// Inspectable overlay list contains Added (+ slime_01) and Changed (z 0 → 12).
	vis := f.ed.VisibleViewportLiveOverlays()
	addedOv, okAdded := findLiveOverlay(vis, editor.OverlayLegendAdded, "slime_01")
	if !okAdded || addedOv.Label != "+ slime_01" || !addedOv.Dashed {
		t.Fatalf("added overlay = %+v (ok=%v), want dashed '+ slime_01'", addedOv, okAdded)
	}
	changedOv, okChanged := findLiveOverlay(vis, editor.OverlayLegendChanged, "hero")
	if !okChanged || changedOv.Label != "z 0 → 12" || !changedOv.Dashed {
		t.Fatalf("changed overlay = %+v (ok=%v), want dashed 'z 0 → 12'", changedOv, okChanged)
	}

	// Scene tree shows NEW on slime_01 and MOD on hero.
	if got := app.Find("id:scene-tree-agent-badge-slime_01").Text; got != "NEW" {
		t.Fatalf("scene tree badge for slime_01 = %q, want NEW", got)
	}
	if got := app.Find("id:scene-tree-agent-badge-hero").Text; got != "MOD" {
		t.Fatalf("scene tree badge for hero = %q, want MOD", got)
	}

	// Turning off the Added row of the legend hides the Added overlay while keeping Changed visible.
	app.Press("id:legend-added")
	app.Settle()
	visAfterToggle := f.ed.VisibleViewportLiveOverlays()
	if _, stillVisible := findLiveOverlay(visAfterToggle, editor.OverlayLegendAdded, "slime_01"); stillVisible {
		t.Fatal("expected Added overlay to be hidden after turning off Added row in legend")
	}
	if _, stillVisible := findLiveOverlay(visAfterToggle, editor.OverlayLegendChanged, "hero"); !stillVisible {
		t.Fatal("expected Changed overlay to stay visible after turning off Added row")
	}
	// Restore Added row for subsequent checks.
	app.Press("id:legend-added")
	app.Settle()

	// 2. While the MCP server marks hero as touched by the active step, a manual move is refused,
	//    MCP edits still succeed, and once the step moves on, manual moves are allowed again.
	ctx := context.Background()
	if err := runUI(func() error { return s.BeginStep(ctx, []string{"hero"}, "adjusting hero") }); err != nil {
		t.Fatalf("BeginStep: %v", err)
	}

	var moveErr error
	app.Post(func() { moveErr = f.ed.MoveObject("hero", 400, 300) })
	app.Settle()
	if !errors.Is(moveErr, editor.ErrAgentTouchingObject) {
		t.Fatalf("manual MoveObject while agent touches hero: err=%v, want ErrAgentTouchingObject", moveErr)
	}
	mustOK(t, callUI(editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{"op": "move", "id": "hero", "x": 120, "y": 80},
		},
	}))

	// Advance the step off hero -> manual move on hero is allowed again.
	if err := runUI(func() error { return s.EndStep(ctx) }); err != nil {
		t.Fatalf("EndStep: %v", err)
	}
	app.Post(func() { moveErr = f.ed.MoveObject("hero", 130, 85) })
	app.Settle()
	if moveErr != nil {
		t.Fatalf("manual MoveObject after EndStep failed: %v", moveErr)
	}

	// 3. Keep clears the overlays and tree markers.
	var keepErr error
	app.Post(func() { keepErr = f.ed.KeepAgentTurn() })
	app.Settle()
	if keepErr != nil {
		t.Fatalf("KeepAgentTurn: %v", keepErr)
	}
	if f.ed.HasAgentLiveOverlays() {
		t.Fatal("expected KeepAgentTurn to clear live overlays")
	}
	if app.Exists("id:scene-tree-agent-badge-slime_01") {
		t.Fatal("expected KeepAgentTurn to clear NEW badge in scene tree")
	}

	// 4. Drive another turn and verify Revert restores scene and clears overlays and tree markers.
	mustOK(t, callUI(editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{
				"op":    "add",
				"layer": "actors",
				"object": map[string]any{
					"id":   "slime_02",
					"name": "slime_02",
					"x":    260,
					"y":    140,
				},
			},
			map[string]any{"op": "remove", "id": "slime_01"},
		},
	}))
	if !f.ed.HasAgentLiveOverlays() {
		t.Fatal("expected live overlays before Revert")
	}
	if !app.Exists("id:scene-tree-agent-badge-slime_02") || !app.Exists("id:scene-tree-agent-badge-slime_01") {
		t.Fatal("expected NEW and DEL badges in scene tree before Revert")
	}

	var revertErr error
	app.Post(func() { revertErr = f.ed.RevertAgentTurn() })
	app.Settle()
	if revertErr != nil {
		t.Fatalf("RevertAgentTurn: %v", revertErr)
	}
	if f.ed.HasAgentLiveOverlays() {
		t.Fatal("expected RevertAgentTurn to clear live overlays")
	}
	if app.Exists("id:scene-tree-agent-badge-slime_02") || app.Exists("id:scene-tree-agent-badge-slime_01") {
		t.Fatal("expected RevertAgentTurn to clear scene tree badges")
	}
	if obj, _, _ := f.ed.FindObject("slime_02"); obj != nil {
		t.Fatal("expected RevertAgentTurn to remove slime_02")
	}
	if obj, _, _ := f.ed.FindObject("slime_01"); obj == nil {
		t.Fatal("expected RevertAgentTurn to restore slime_01")
	}
}

func TestMCPNoOpEditDiscardsEmptyTurnAndPreservesExplicitStep(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// No-op edit (hero.z is already 0) must not leave an empty pending turn blocking autosave.
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, map[string]any{
		"edits": []any{
			map[string]any{
				"op":     "set",
				"id":     "hero",
				"fields": map[string]any{"z": 0},
			},
		},
	}))
	if f.ed.AgentTurnPending() || f.ed.HasOpenReview() {
		t.Fatalf("no-op scene_edit left empty turn open: pending=%v openReview=%v",
			f.ed.AgentTurnPending(), f.ed.HasOpenReview())
	}

	// Running game_check while an explicit step is active must not clear the explicit step lock.
	ctx := context.Background()
	if err := s.BeginStep(ctx, []string{"hero"}, "inspecting hero"); err != nil {
		t.Fatalf("BeginStep: %v", err)
	}
	mustOK(t, call(t, cs, editormcp.ToolGameCheck, nil))
	if !f.ed.AgentTouchingObject("hero") {
		t.Fatal("game_check cleared active explicit step lock on hero")
	}
	if err := s.EndStep(ctx); err != nil {
		t.Fatalf("EndStep: %v", err)
	}
	if f.ed.AgentTouchingObject("hero") {
		t.Fatal("EndStep did not release hero")
	}
}

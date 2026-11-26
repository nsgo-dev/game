package editor_test

import (
	"bytes"
	"errors"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func newSketchTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
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
				Name:  "World",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "floor", Name: "Floor", X: 320, Y: 320},
					{ID: "ledge_02", Name: "ledge_02", X: 540, Y: 190},
				},
			},
			{
				Name:  "Actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "player", Name: "Player", X: 110, Y: 280},
					{ID: "knight", Name: "Knight", X: 400, Y: 224},
					{ID: "door", Name: "Door", X: 530, Y: 280},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(640, 360)
	return ed
}

func assertSketchNear(t *testing.T, label string, got, want, tol float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(tol) {
		t.Fatalf("%s = %v, want %v (±%v)", label, got, want, tol)
	}
}

// #1025 Acceptance 1: A test draws a box, an arrow and a text mark, sends them, and the scripted
// agent receives three marks with scene coordinates that match the viewport at two zoom levels
// (plus a rendered PNG with the marks overlaid), while scene YAML never contains sketch marks.
func TestSketchDrawBoxArrowTextAndSendAtTwoZoomLevels(t *testing.T) {
	ed := newSketchTestEditor(t)

	initialYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	cleanPNG, _, _, err := ed.RenderSketchViewportPNG(1)
	if err != nil {
		t.Fatalf("RenderSketchViewportPNG(clean): %v", err)
	}

	var received []editor.SketchAgentRequest
	ed.OnSendSketch = func(req editor.SketchAgentRequest) {
		received = append(received, req)
	}

	// Zoom level 1: 1.0x centered on (320, 180).
	ed.SetView2D(editor.View2D{X: 320, Y: 180, Zoom: 1.0})
	ed.StartSketch()

	// Draw Box via canvas drag at zoom 1.0.
	ed.SetSketchTool(editor.SketchToolBox)
	if !ed.BeginSketchDrag(171, 269) || !ed.UpdateSketchDrag(280, 302) {
		t.Fatal("Begin/UpdateSketchDrag for box failed")
	}
	m1, ok := ed.EndSketchDrag()
	if !ok || m1.Number != 1 || m1.Kind != editor.SketchToolBox || m1.Rect == nil {
		t.Fatalf("box mark = (%+v, %v)", m1, ok)
	}
	ed.SetSketchMarkLabel(1, "3 spikes")

	// Draw Arrow via canvas drag at zoom 1.0.
	ed.SetSketchTool(editor.SketchToolArrow)
	if !ed.BeginSketchDrag(465, 111) || !ed.UpdateSketchDrag(533, 242) {
		t.Fatal("Begin/UpdateSketchDrag for arrow failed")
	}
	m2, ok := ed.EndSketchDrag()
	if !ok || m2.Number != 2 || m2.Kind != editor.SketchToolArrow || m2.Start == nil || m2.End == nil {
		t.Fatalf("arrow mark = (%+v, %v)", m2, ok)
	}
	ed.SetSketchMarkLabel(2, "locked until the lever")

	// Place Text mark via canvas click at zoom 1.0.
	ed.SetSketchTool(editor.SketchToolText)
	if !ed.BeginSketchDrag(490, 170) {
		t.Fatal("BeginSketchDrag for text failed")
	}
	m3, ok := ed.EndSketchDrag()
	if !ok || m3.Number != 3 || m3.Kind != editor.SketchToolText || m3.At == nil {
		t.Fatalf("text mark = (%+v, %v)", m3, ok)
	}
	ed.SetSketchMarkLabel(3, "platform, back and forth")

	if ed.SketchBarCountLabel() != "3 marks not saved" {
		t.Fatalf("SketchBarCountLabel = %q, want %q", ed.SketchBarCountLabel(), "3 marks not saved")
	}
	if ed.SketchChipLabel() != "Sketch · 3 marks" {
		t.Fatalf("SketchChipLabel = %q, want %q", ed.SketchChipLabel(), "Sketch · 3 marks")
	}

	req1, err := ed.SendSketch("Build what I sketched.")
	if err != nil {
		t.Fatalf("SendSketch zoom 1: %v", err)
	}
	if len(received) != 1 || len(req1.Marks) != 3 {
		t.Fatalf("received=%d marks=%d, want 1 / 3", len(received), len(req1.Marks))
	}
	if req1.ChipLabel != "Sketch · 3 marks" {
		t.Fatalf("ChipLabel = %q, want Sketch · 3 marks", req1.ChipLabel)
	}
	// Verify exact scene coordinates match CanvasToWorld at zoom 1.0.
	bx0, by0 := ed.CanvasToWorld(171, 269)
	bx1, by1 := ed.CanvasToWorld(280, 302)
	assertSketchNear(t, "zoom1 box.X", req1.Marks[0].Rect.X, bx0, 0.01)
	assertSketchNear(t, "zoom1 box.Y", req1.Marks[0].Rect.Y, by0, 0.01)
	assertSketchNear(t, "zoom1 box.W", req1.Marks[0].Rect.Width, bx1-bx0, 0.01)
	assertSketchNear(t, "zoom1 box.H", req1.Marks[0].Rect.Height, by1-by0, 0.01)

	ax0, ay0 := ed.CanvasToWorld(465, 111)
	ax1, ay1 := ed.CanvasToWorld(533, 242)
	assertSketchNear(t, "zoom1 arrow.Start.X", req1.Marks[1].Start.X, ax0, 0.01)
	assertSketchNear(t, "zoom1 arrow.Start.Y", req1.Marks[1].Start.Y, ay0, 0.01)
	assertSketchNear(t, "zoom1 arrow.End.X", req1.Marks[1].End.X, ax1, 0.01)
	assertSketchNear(t, "zoom1 arrow.End.Y", req1.Marks[1].End.Y, ay1, 0.01)

	tx, ty := ed.CanvasToWorld(490, 170)
	assertSketchNear(t, "zoom1 text.At.X", req1.Marks[2].At.X, tx, 0.01)
	assertSketchNear(t, "zoom1 text.At.Y", req1.Marks[2].At.Y, ty, 0.01)

	// Verify PNG is a valid image that visibly differs from the unmarked viewport PNG.
	img1, err := png.Decode(bytes.NewReader(req1.PNG))
	if err != nil {
		t.Fatalf("decode sketch PNG zoom 1: %v", err)
	}
	if img1.Bounds().Dx() != 640 || img1.Bounds().Dy() != 360 {
		t.Fatalf("sketch PNG bounds = %v, want 640x360", img1.Bounds())
	}
	if bytes.Equal(req1.PNG, cleanPNG) {
		t.Fatal("rendered sketch PNG is identical to unmarked viewport PNG")
	}

	// Zoom level 2: 2.0x panned to (400, 210).
	// First verify the existing marks project accurately via WorldToCanvas at zoom 2.0 without shifting scene coords.
	ed.SetView2D(editor.View2D{X: 400, Y: 210, Zoom: 2.0})
	cx2, cy2 := ed.WorldToCanvas(req1.Marks[0].Rect.X, req1.Marks[0].Rect.Y)
	backX, backY := ed.CanvasToWorld(cx2, cy2)
	assertSketchNear(t, "zoom2 roundtrip X", backX, req1.Marks[0].Rect.X, 0.01)
	assertSketchNear(t, "zoom2 roundtrip Y", backY, req1.Marks[0].Rect.Y, 0.01)

	// Clear and draw a fresh Box, Arrow, and Text mark at zoom 2.0, then send and verify exact scene coordinates.
	ed.ClearSketch()
	ed.StartSketch()

	ed.SetSketchTool(editor.SketchToolBox)
	ed.BeginSketchDrag(100, 120)
	ed.UpdateSketchDrag(260, 200)
	ed.EndSketchDrag()
	ed.SetSketchMarkLabel(1, "spikes zoomed")

	ed.SetSketchTool(editor.SketchToolArrow)
	ed.BeginSketchDrag(300, 80)
	ed.UpdateSketchDrag(460, 240)
	ed.EndSketchDrag()
	ed.SetSketchMarkLabel(2, "door zoomed")

	ed.SetSketchTool(editor.SketchToolText)
	ed.BeginSketchDrag(420, 150)
	ed.EndSketchDrag()
	ed.SetSketchMarkLabel(3, "note zoomed")

	req2, err := ed.SendSketch("Zoomed sketch")
	if err != nil {
		t.Fatalf("SendSketch zoom 2: %v", err)
	}
	if len(received) != 2 || len(req2.Marks) != 3 {
		t.Fatalf("zoom 2 received=%d marks=%d, want 2 / 3", len(received), len(req2.Marks))
	}
	z2bx0, z2by0 := ed.CanvasToWorld(100, 120)
	z2bx1, z2by1 := ed.CanvasToWorld(260, 200)
	assertSketchNear(t, "zoom2 box.X", req2.Marks[0].Rect.X, z2bx0, 0.01)
	assertSketchNear(t, "zoom2 box.Y", req2.Marks[0].Rect.Y, z2by0, 0.01)
	assertSketchNear(t, "zoom2 box.W", req2.Marks[0].Rect.Width, z2bx1-z2bx0, 0.01)
	assertSketchNear(t, "zoom2 box.H", req2.Marks[0].Rect.Height, z2by1-z2by0, 0.01)

	z2ax0, z2ay0 := ed.CanvasToWorld(300, 80)
	z2ax1, z2ay1 := ed.CanvasToWorld(460, 240)
	assertSketchNear(t, "zoom2 arrow.Start.X", req2.Marks[1].Start.X, z2ax0, 0.01)
	assertSketchNear(t, "zoom2 arrow.Start.Y", req2.Marks[1].Start.Y, z2ay0, 0.01)
	assertSketchNear(t, "zoom2 arrow.End.X", req2.Marks[1].End.X, z2ax1, 0.01)
	assertSketchNear(t, "zoom2 arrow.End.Y", req2.Marks[1].End.Y, z2ay1, 0.01)

	z2tx, z2ty := ed.CanvasToWorld(420, 150)
	assertSketchNear(t, "zoom2 text.At.X", req2.Marks[2].At.X, z2tx, 0.01)
	assertSketchNear(t, "zoom2 text.At.Y", req2.Marks[2].At.Y, z2ty, 0.01)

	// Scene YAML and History must be completely untouched by sketch marks.
	afterYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after sketch: %v", err)
	}
	if !bytes.Equal(initialYAML, afterYAML) {
		t.Fatalf("scene YAML mutated by sketch layer:\n%s", string(afterYAML))
	}
	if ed.History.Len() != 0 {
		t.Fatalf("History.Len() = %d, want 0 (sketch marks must not pollute scene undo history)", ed.History.Len())
	}
	if strings.Contains(string(afterYAML), "spikes") || strings.Contains(string(afterYAML), "sketch") {
		t.Fatalf("scene YAML leaked sketch content:\n%s", string(afterYAML))
	}
}

// #1025 Acceptance 2 & 3 + Keyboard paths & UI workflow:
// - Esc finishes sketch mode while keeping marks
// - Keyboard shortcuts switch tools (D, P, A, B, T, I, H) and register in ScopeSketch
// - Reading card displays Go ahead and Correct a mark (reopening sketch with that exact mark selected)
// - Review Sketch overlay toggles, Revert keeps sketch marks, Keep clears sketch layer
// - Compact 2D editor-toolbar stays untouched
func TestSketchUIKeyboardReadingCorrectMarkAndReviewKeepRevert(t *testing.T) {
	ed := newSketchTestEditor(t)
	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), macEnv)

	// Confirm the 2D editor-toolbar stays compact (no extra sketch button added).
	if app.Exists("id:editor-toolbar >> id:tool-sketch") {
		t.Fatal("editor-toolbar must stay compact without adding extra sketch buttons")
	}

	// Verify all sketch commands are registered with keyboard paths and shortcut rows.
	wantCmds := []string{
		editor.CmdSketchToggle,
		editor.CmdSketchToolPen,
		editor.CmdSketchToolArrow,
		editor.CmdSketchToolBox,
		editor.CmdSketchToolText,
		editor.CmdSketchAddMarkAtCursor,
		editor.CmdSketchUndoMark,
		editor.CmdSketchToggleMarks,
		editor.CmdSketchClear,
		editor.CmdSketchDone,
		editor.CmdSketchSend,
		editor.CmdSketchApproveReading,
		editor.CmdSketchCorrectMark,
		editor.CmdSketchToggleReviewOverlay,
		editor.CmdSketchKeepReview,
		editor.CmdSketchRevertReview,
	}
	for _, id := range wantCmds {
		cmd, ok := ed.Command(id)
		if !ok {
			t.Fatalf("command %q missing from registry", id)
		}
		if cmd.Shortcut == "" {
			t.Fatalf("command %q has no shortcut row", id)
		}
	}

	// Press 'd' to start Sketch mode.
	app.Key("d")
	if !ed.SketchActive() {
		t.Fatal("pressing 'd' did not activate sketch mode")
	}
	if !app.Exists("id:viewport-sketch-toolbar") {
		t.Fatal("viewport-sketch-toolbar did not appear when sketch mode activated")
	}
	if !app.Exists("id:scene-tree-sketch-layer") {
		t.Fatal("SKETCH LAYER row missing in scene tree")
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Sketch layer") || !strings.Contains(got, "scene") {
		t.Fatalf("status bar mode while sketching = %q, want Sketch layer + scene coords", got)
	}

	// Keyboard tool switching in ScopeSketch: B (Box), I (add mark at cursor), A (Arrow), I, T (Text), I, P (Pen).
	app.Key("b")
	if ed.SketchTool() != editor.SketchToolBox {
		t.Fatalf("SketchTool after 'b' = %v, want box", ed.SketchTool())
	}
	app.Key("i")
	app.Key("a")
	if ed.SketchTool() != editor.SketchToolArrow {
		t.Fatalf("SketchTool after 'a' = %v, want arrow", ed.SketchTool())
	}
	app.Key("i")
	app.Key("t")
	if ed.SketchTool() != editor.SketchToolText {
		t.Fatalf("SketchTool after 't' = %v, want text", ed.SketchTool())
	}
	app.Key("i")
	app.Key("p")
	if ed.SketchTool() != editor.SketchToolPen {
		t.Fatalf("SketchTool after 'p' = %v, want pen", ed.SketchTool())
	}
	if ed.SketchMarkCount() != 3 {
		t.Fatalf("SketchMarkCount after keyboard insert = %d, want 3", ed.SketchMarkCount())
	}
	if got := app.Find("id:sketch-bar-count").Text; got != "3 marks not saved" {
		t.Fatalf("sketch-bar-count = %q, want %q", got, "3 marks not saved")
	}

	// Press Esc to finish sketch mode while keeping the 3 marks.
	app.Key("escape")
	if ed.SketchActive() {
		t.Fatal("Escape did not finish sketch mode")
	}
	if ed.SketchMarkCount() != 3 {
		t.Fatalf("Escape cleared sketch marks: got %d, want 3", ed.SketchMarkCount())
	}
	if !app.Exists("id:sketch-context-chip") {
		t.Fatal("Sketch · 3 marks chip missing in composer after Esc")
	}
	// Click chip to open "Sent with this request" preview.
	app.Press("id:sketch-context-chip")
	if !app.Exists("id:sketch-sent-preview") {
		t.Fatal("clicking sketch-context-chip did not open Sent with this request preview")
	}

	// Send the sketch.
	app.Post(func() {
		ed.SetSketchMarkLabel(1, "3 spikes")
		ed.SetSketchMarkLabel(2, "locked until the lever")
		ed.SetSketchMarkLabel(3, "platform, back and forth")
		ed.SetSketchPromptDraft("Build what I sketched.")
	})
	app.Settle()
	app.Press("id:sketch-composer-send")
	if !ed.SketchHoldActive() {
		t.Fatal("sending sketch did not arm the hold gate")
	}

	// Agent posts reading card.
	app.Post(func() {
		if err := ed.PostSketchReading(editor.SketchReading{
			Items: []editor.SketchReadingItem{
				{Number: 1, Text: "Three spike tiles on the floor."},
				{Number: 2, Text: "Door stays locked until lever_01 is pulled."},
				{Number: 3, Text: "Moving platform between x 487 and 604."},
			},
			Assumptions: []string{
				"There is no lever in level_01 yet. I will add lever_01 left of the plank at x 295 unless you point somewhere else.",
			},
		}); err != nil {
			t.Errorf("PostSketchReading: %v", err)
		}
	})
	app.Settle()
	if !app.Exists("id:sketch-reading-card") || !app.Exists("id:sketch-reading-go-ahead") || !app.Exists("id:sketch-reading-correct") {
		t.Fatal("sketch reading card controls missing")
	}

	// Clicking reading item #2 (Correct a mark) reopens sketch mode with mark 2 selected and keeps hold active.
	app.Press("id:sketch-reading-item-2")
	if !ed.SketchActive() {
		t.Fatal("Correct a mark did not reopen sketch mode")
	}
	if ed.SelectedSketchMark() != 2 {
		t.Fatalf("SelectedSketchMark = %d, want 2", ed.SelectedSketchMark())
	}
	if !ed.SketchHoldActive() {
		t.Fatal("Correct a mark must keep confirmation hold active")
	}

	// Re-send, post reading, and press Go ahead.
	app.Press("id:sketch-composer-send")
	app.Post(func() {
		_ = ed.PostSketchReading(editor.SketchReading{
			Items: []editor.SketchReadingItem{
				{Number: 1, Text: "Three spike tiles on the floor."},
				{Number: 2, Text: "Door stays locked until lever_01 is pulled."},
				{Number: 3, Text: "Moving platform."},
			},
		})
	})
	app.Settle()
	app.Press("id:sketch-reading-go-ahead")
	if ed.SketchHoldActive() || !ed.SketchApproved() {
		t.Fatalf("after Go ahead: holdActive=%v approved=%v, want false/true", ed.SketchHoldActive(), ed.SketchApproved())
	}

	// Simulate agent applying edits and opening Sketch Review.
	app.Post(func() {
		id, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "lever_01", Name: "lever_01", X: 295, Y: 280})
		if err != nil {
			t.Errorf("PlaceObject: %v", err)
			return
		}
		ed.RecordSketchReviewEdits("Agent: add lever_01", []string{id}, nil, nil)
	})
	app.Settle()
	if !app.Exists("id:sketch-review-card") || !app.Exists("id:sketch-review-overlay-toggle") {
		t.Fatal("sketch review card or Sketch overlay toggle missing")
	}
	if !ed.SketchReviewOverlayVisible() {
		t.Fatal("Sketch overlay should default to ON in review")
	}
	app.Press("id:sketch-review-overlay-toggle")
	if ed.SketchReviewOverlayVisible() {
		t.Fatal("clicking Sketch overlay toggle did not turn overlay OFF")
	}
	app.Press("id:sketch-review-overlay-toggle")
	if !ed.SketchReviewOverlayVisible() {
		t.Fatal("clicking Sketch overlay toggle again did not turn overlay ON")
	}

	// Revert restores scene AND keeps the 3 sketch marks!
	app.Press("id:sketch-review-revert")
	if ed.SketchReviewOpen() {
		t.Fatal("Revert did not close sketch review")
	}
	if obj, _, _ := ed.FindObject("lever_01"); obj != nil {
		t.Fatal("Revert did not remove lever_01 added during review")
	}
	if ed.SketchMarkCount() != 3 {
		t.Fatalf("Revert cleared sketch marks: got %d, want 3", ed.SketchMarkCount())
	}

	// Approve again, apply edit, and Keep -> keeps lever_01 AND clears sketch layer!
	app.Post(func() {
		_ = ed.ApproveSketchReading()
		id, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "lever_01", Name: "lever_01", X: 295, Y: 280})
		if err != nil {
			t.Errorf("PlaceObject 2: %v", err)
			return
		}
		ed.RecordSketchReviewEdits("Agent: add lever_01", []string{id}, nil, nil)
	})
	app.Settle()
	app.Press("id:sketch-review-keep")
	if ed.SketchReviewOpen() {
		t.Fatal("Keep did not close sketch review")
	}
	if obj, _, _ := ed.FindObject("lever_01"); obj == nil {
		t.Fatal("Keep did not retain lever_01")
	}
	if ed.SketchMarkCount() != 0 {
		t.Fatalf("Keep did not clear sketch marks: got %d, want 0", ed.SketchMarkCount())
	}
}

// #1025 Acceptance 4: Real nsgotest screenshots on macOS beside docs/design/editor/Sketch.dc.html.
func TestSketchUIAndScreenshots(t *testing.T) {
	ed := newSketchTestEditor(t)
	_ = ed.SetAppearance(editor.AppearanceDark)
	ed.SetView2D(editor.View2D{X: 320, Y: 180, Zoom: 1.0})
	ed.StartSketch()
	ed.AddSketchBox(171, 240, 109, 36, "3 spikes")
	ed.AddSketchPen([]editor.SketchPoint{
		{X: 390, Y: 165},
		{X: 430, Y: 152},
		{X: 470, Y: 174},
		{X: 510, Y: 158},
		{X: 550, Y: 168},
	}, "platform, back and forth")
	ed.AddSketchArrow(450, 108, 522, 245, "locked until the lever")
	ed.SelectSketchMark(0)
	ed.SetSketchTool(editor.SketchToolArrow)
	ed.SetSketchPromptDraft("Build what I sketched. Spikes should look like the reference image.")
	ed.SetSketchPreviewOpen(true)

	macEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), macEnv)

	app.Screenshot("sketch-active")

	// State 2: Sent sketch + agent reading card with Go ahead / Correct a mark.
	app.Post(func() {
		_, _ = ed.SendSketch("Build what I sketched. Spikes should look like the reference image.")
		_ = ed.PostSketchReading(editor.SketchReading{
			Summary: "I read your sketch as:",
			Items: []editor.SketchReadingItem{
				{Number: 1, Mark: "Box · “3 spikes”", Text: "Three spike tiles on the floor from x 171 to 280, using prefabs/spikes.prefab.yaml, 1 damage each."},
				{Number: 2, Mark: "Line · “platform, back and forth”", Text: "A new moving_platform, 64 px wide, going back and forth between x 390 and 550 at 40 px/s."},
				{Number: 3, Mark: "Arrow at Door · “locked until the lever”", Text: "Door stays locked until lever_01 is pulled once."},
			},
			Assumptions: []string{
				"There is no lever in level_01 yet. I will add lever_01 left of the plank at x 295 unless you point somewhere else.",
			},
		})
	})
	app.Settle()

	app.Screenshot("sketch-reading")

	// State 3: Review with Sketch overlay toggle + Keep / Revert.
	app.Post(func() {
		_ = ed.ApproveSketchReading()
		s1, _ := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "spikes_01", Name: "spikes_01", X: 225, Y: 258})
		p1, _ := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "moving_platform", Name: "moving_platform", X: 465, Y: 165})
		l1, _ := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "lever_01", Name: "lever_01", X: 295, Y: 280})
		_ = ed.SetProperty("door", "locked_until", "lever_01")
		ed.RecordSketchReviewEdits("Agent: build sketch", []string{s1, p1, l1}, []string{"door"}, nil)
	})
	app.Settle()

	app.Screenshot("sketch-review")
}

func TestSketchLifecycleResetPlayGuardAndDetachedChip(t *testing.T) {
	ed := newSketchTestEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760))
	_ = app

	// Detaching the sketch chip prevents SendSketch from attaching removed marks.
	ed.AddSketchBox(100, 100, 60, 40, "spikes")
	ed.SetSketchAttached(false)
	if _, err := ed.SendSketch("build it"); !errors.Is(err, editor.ErrSketchEmpty) {
		t.Fatalf("SendSketch with detached chip = %v, want ErrSketchEmpty", err)
	}
	ed.SetSketchAttached(true)

	// Send, post reading, approve, and record review edits, then Play guards all mutators.
	if _, err := ed.SendSketch("build spikes"); err != nil {
		t.Fatalf("SendSketch: %v", err)
	}
	if err := ed.PostSketchReading(editor.SketchReading{
		Items: []editor.SketchReadingItem{{Number: 1, Text: "Add spikes"}},
	}); err != nil {
		t.Fatalf("PostSketchReading: %v", err)
	}
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if m := ed.AddSketchBox(10, 10, 20, 20, "bad"); m.Number != 0 {
		t.Fatalf("AddSketchBox during Play succeeded: %+v", m)
	}
	if m := ed.AddSketchArrow(10, 10, 20, 20, "bad"); m.Number != 0 {
		t.Fatalf("AddSketchArrow during Play succeeded: %+v", m)
	}
	if m := ed.AddSketchText(10, 10, "bad"); m.Number != 0 {
		t.Fatalf("AddSketchText during Play succeeded: %+v", m)
	}
	if _, err := ed.SendSketch("bad"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SendSketch during Play = %v, want ErrPlaying", err)
	}
	if err := ed.ApproveSketchReading(); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("ApproveSketchReading during Play = %v, want ErrPlaying", err)
	}
	if err := ed.CorrectSketchMark(1); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("CorrectSketchMark during Play = %v, want ErrPlaying", err)
	}
	if err := ed.ReplyToSketchReading("ok"); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("ReplyToSketchReading during Play = %v, want ErrPlaying", err)
	}
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// OpenDemo resets all sketch marks, reading, hold gate, and review baseYAML so nothing leaks across projects.
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.SketchMarkCount() != 0 || ed.SketchHoldActive() || ed.SketchReading() != nil || ed.SketchReviewOpen() {
		t.Fatalf("OpenDemo leaked sketch state: marks=%d hold=%v reading=%v review=%v",
			ed.SketchMarkCount(), ed.SketchHoldActive(), ed.SketchReading() != nil, ed.SketchReviewOpen())
	}
}

func TestSketchNonFiniteCoordsAndPayloadBounds(t *testing.T) {
	ed := newSketchTestEditor(t)
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	if m := ed.AddSketchBox(nan, 0, 10, 10, "nan"); m.Number != 0 {
		t.Fatalf("AddSketchBox accepted NaN: %+v", m)
	}
	if m := ed.AddSketchBox(0, 0, inf, 10, "inf"); m.Number != 0 {
		t.Fatalf("AddSketchBox accepted Inf: %+v", m)
	}
	if m := ed.AddSketchArrow(0, 0, nan, 10, "nan"); m.Number != 0 {
		t.Fatalf("AddSketchArrow accepted NaN: %+v", m)
	}
	if m := ed.AddSketchText(inf, 0, "inf"); m.Number != 0 {
		t.Fatalf("AddSketchText accepted Inf: %+v", m)
	}
	if ed.SketchMarkCount() != 0 {
		t.Fatalf("non-finite marks were stored: count=%d", ed.SketchMarkCount())
	}

	// Huge label is clamped cleanly.
	longLabel := strings.Repeat("x", 1000)
	mk := ed.AddSketchBox(20, 20, 40, 30, longLabel)
	if len([]rune(mk.Label)) > 256 {
		t.Fatalf("label was not clamped: len=%d", len([]rune(mk.Label)))
	}

	// PostSketchReading refuses > 200 items and > 32 assumptions.
	tooManyItems := make([]editor.SketchReadingItem, 201)
	for i := range tooManyItems {
		tooManyItems[i] = editor.SketchReadingItem{Number: 1, Text: "x"}
	}
	if err := ed.PostSketchReading(editor.SketchReading{Items: tooManyItems}); err == nil {
		t.Fatal("PostSketchReading accepted 201 items, want error")
	}
	tooManyAssump := make([]string, 33)
	for i := range tooManyAssump {
		tooManyAssump[i] = "a"
	}
	if err := ed.PostSketchReading(editor.SketchReading{
		Items:       []editor.SketchReadingItem{{Number: 1, Text: "ok"}},
		Assumptions: tooManyAssump,
	}); err == nil {
		t.Fatal("PostSketchReading accepted 33 assumptions, want error")
	}

	// RenderSketchViewportPNG refuses 3D view mode and caps huge viewports cleanly.
	ed.SetViewMode(editor.ViewMode3D)
	if _, _, _, err := ed.RenderSketchViewportPNG(1); err == nil {
		t.Fatal("RenderSketchViewportPNG succeeded in 3D mode, want error")
	}
	ed.SetViewMode(editor.ViewMode2D)
	ed.SetCanvasSize(3000, 3000)
	_, pw, ph, err := ed.RenderSketchViewportPNG(4)
	if err != nil {
		t.Fatalf("RenderSketchViewportPNG on oversized viewport: %v", err)
	}
	if pw > 2048 || ph > 2048 || int64(pw)*int64(ph) > 4_000_000 {
		t.Fatalf("RenderSketchViewportPNG did not cap pixels: %dx%d", pw, ph)
	}
}

func TestSketchPanelCloseSpecLightTypingAndRevertSelection(t *testing.T) {
	ed := newSketchTestEditor(t)
	ed.AddSketchBox(100, 100, 80, 40, "spikes")
	ed.StartSketch()
	if _, err := ed.SendSketch("build spikes"); err != nil {
		t.Fatalf("SendSketch: %v", err)
	}
	if err := ed.PostSketchReading(editor.SketchReading{
		Items: []editor.SketchReadingItem{{Number: 1, Text: "Add spikes"}},
	}); err != nil {
		t.Fatalf("PostSketchReading: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1360, 820), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	light := editor.LightSpecPalette()
	dark := editor.DarkSpecPalette()

	if got := app.Find("id:sketch-agent-panel").Style.Background; got != light.Panel {
		t.Fatalf("light mode sketch-agent-panel bg = %#x, want %#x", uint32(got), uint32(light.Panel))
	}

	// Clicking sketch-panel-close must dismiss the panel cleanly even while a reading card is open,
	// revealing RightRailView underneath.
	app.Press("id:sketch-panel-close")
	app.Settle()
	if ed.SketchPanelOpen() {
		t.Fatal("SketchPanelOpen remained true after pressing sketch-panel-close with reading open")
	}
	if !app.Exists("id:right-rail-inspector-wrap") {
		t.Fatal("expected RightRailView (right-rail-inspector-wrap) visible after closing SketchPanelView")
	}

	// Reopen panel, focus composer input -> IsTyping() == true, then flip Dark mode live.
	app.Post(func() { ed.SetSketchPanelOpen(true) })
	app.Settle()
	app.Focus("id:sketch-composer-input")
	app.Settle()
	if !ed.IsTyping() {
		t.Fatal("expected IsTyping() true while sketch-composer-input is focused")
	}
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	if got := app.Find("id:sketch-agent-panel").Style.Background; got != dark.Panel {
		t.Fatalf("dark mode sketch-agent-panel bg = %#x, want %#x", uint32(got), uint32(dark.Panel))
	}

	// Full-window screens block CmdSketchToggle and SetSketchActive.
	app.Post(func() {
		ed.OpenSettingsPage(editor.SettingsPageGeneral)
		ed.SetSketchActive(true)
	})
	app.Settle()
	if ed.SketchActive() {
		t.Fatal("SetSketchActive(true) succeeded while Settings full-window screen was open")
	}
	app.Post(func() { ed.CloseSettings() })
	app.Settle()

	// RevertSketchReview must clear SelectedID when it pointed at a reverted agent-added object.
	app.Post(func() {
		if err := ed.ApproveSketchReading(); err != nil {
			t.Errorf("ApproveSketchReading: %v", err)
			return
		}
		addedID, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "temp_spikes", Name: "temp_spikes", X: 120, Y: 120})
		if err != nil {
			t.Errorf("PlaceObject: %v", err)
			return
		}
		ed.SelectObject(addedID)
		ed.RecordSketchReviewEdits("Agent: add temp_spikes", []string{addedID}, nil, nil)
		if err := ed.RevertSketchReview(); err != nil {
			t.Errorf("RevertSketchReview: %v", err)
		}
	})
	app.Settle()
	if ed.SelectedID != "" {
		t.Fatalf("SelectedID = %q after RevertSketchReview removed object, want empty", ed.SelectedID)
	}
}

func TestSketchAutoSavePauseHistorySyncPreviewGuardsAndLabelSpaces(t *testing.T) {
	ed := newSketchTestEditor(t)
	ed.SetCanvasSize(640, 360)

	// 1. Typing spaces into SetSketchMarkLabel must preserve spaces interactively; SendSketch trims cleanly.
	mk := ed.AddSketchBox(100, 120, 80, 32, "")
	if !ed.SetSketchMarkLabel(mk.Number, "3 ") {
		t.Fatal("SetSketchMarkLabel(\"3 \") failed")
	}
	got, ok := ed.SketchMarkByNumber(mk.Number)
	if !ok || got.Label != "3 " {
		t.Fatalf("label after typing trailing space = %q, want %q", got.Label, "3 ")
	}
	if !ed.SetSketchMarkLabel(mk.Number, "3 spikes ") {
		t.Fatal("SetSketchMarkLabel(\"3 spikes \") failed")
	}
	req, err := ed.SendSketch("build spikes")
	if err != nil {
		t.Fatalf("SendSketch: %v", err)
	}
	if len(req.Marks) != 1 || req.Marks[0].Label != "3 spikes" {
		t.Fatalf("sent mark label = %q, want trimmed %q", req.Marks[0].Label, "3 spikes")
	}

	// 2. EndSketchDrag must not panic if marks are deleted mid-drag, and SetViewMode(3D) cancels drag.
	ed.StartSketch()
	ed.SetSketchTool(editor.SketchToolArrow)
	bx, by := ed.WorldToCanvas(100, 120)
	if !ed.BeginSketchDrag(bx-10, by-12) {
		t.Fatal("BeginSketchDrag on badge returned false")
	}
	if !ed.SketchDragging() {
		t.Fatal("expected SketchDragging() == true")
	}
	ed.DeleteSketchMark(1)
	ed.EndSketchDrag() // must not panic even though hitMark was 1
	ed.AddSketchBox(100, 120, 80, 32, "spikes")
	ed.StartSketch()
	ed.BeginSketchDrag(200, 200)
	ed.SetViewMode(editor.ViewMode3D)
	if ed.SketchDragging() {
		t.Fatal("SetViewMode(ViewMode3D) did not cancel active sketch drag")
	}
	ed.SetViewMode(editor.ViewMode2D)

	// 3. HasOpenReview pauses autosave during SketchReviewOpen, and RevertSketchReview trims History entries.
	if err := ed.MoveObject("floor", 325, 320); err != nil {
		t.Fatalf("MoveObject baseline: %v", err)
	}
	baseEntries := len(ed.HistoryEntries())
	if baseEntries == 0 {
		t.Fatal("expected at least 1 baseline history entry")
	}
	if _, err := ed.SendSketch("add spikes"); err != nil {
		t.Fatalf("SendSketch 2: %v", err)
	}
	if err := ed.ApproveSketchReading(); err != nil {
		t.Fatalf("ApproveSketchReading: %v", err)
	}
	addedID, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{ID: "ghost_spikes", Name: "ghost_spikes", X: 140, Y: 136})
	if err != nil {
		t.Fatalf("PlaceObject: %v", err)
	}
	ed.RecordSketchReviewEdits("Agent: add ghost_spikes", []string{addedID}, nil, nil)
	if !ed.HasOpenReview() {
		t.Fatal("HasOpenReview() = false while SketchReviewOpen() is true; autosave would persist unkept agent edits")
	}
	if len(ed.HistoryEntries()) <= baseEntries {
		t.Fatalf("expected agent edit to append history entry: got %d, base %d", len(ed.HistoryEntries()), baseEntries)
	}
	if err := ed.RevertSketchReview(); err != nil {
		t.Fatalf("RevertSketchReview: %v", err)
	}
	if ed.HasOpenReview() {
		t.Fatal("HasOpenReview() remained true after RevertSketchReview")
	}
	if gotEntries := len(ed.HistoryEntries()); gotEntries != baseEntries {
		t.Fatalf("RevertSketchReview left ghost history entries: got %d, want %d", gotEntries, baseEntries)
	}

	// 4. Read-only History preview guards SketchActive and all sketch mutators.
	firstEntryID := ed.HistoryEntries()[0].ID
	if err := ed.PreviewHistoryEntry(firstEntryID); err != nil {
		t.Fatalf("PreviewHistoryEntry: %v", err)
	}
	if !ed.HistoryPreviewActive() {
		t.Fatal("expected HistoryPreviewActive() == true")
	}
	ed.SetSketchActive(true)
	if ed.SketchActive() {
		t.Fatal("SketchActive() must be false while HistoryPreviewActive() is true")
	}
	if m := ed.AddSketchBox(10, 10, 20, 20, "blocked"); m.Number != 0 {
		t.Fatalf("AddSketchBox succeeded during HistoryPreviewActive: %+v", m)
	}
	if _, err := ed.SendSketch("blocked"); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("SendSketch during HistoryPreview = %v, want ErrHistoryPreview", err)
	}
	if err := ed.ApproveSketchReading(); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("ApproveSketchReading during HistoryPreview = %v, want ErrHistoryPreview", err)
	}
	if err := ed.CorrectSketchMark(1); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("CorrectSketchMark during HistoryPreview = %v, want ErrHistoryPreview", err)
	}
	if err := ed.ReplyToSketchReading("ok"); !errors.Is(err, editor.ErrHistoryPreview) {
		t.Fatalf("ReplyToSketchReading during HistoryPreview = %v, want ErrHistoryPreview", err)
	}
	ed.ExitHistoryPreview()

	// 5. You-only PermissionsDraftOpen blocks sketch shortcut commands (including mod+enter).
	if _, err := ed.SendSketch("build spikes"); err != nil {
		t.Fatalf("SendSketch 3: %v", err)
	}
	if err := ed.PostSketchReading(editor.SketchReading{
		Items: []editor.SketchReadingItem{{Number: 1, Text: "Add spikes"}},
	}); err != nil {
		t.Fatalf("PostSketchReading: %v", err)
	}
	if _, err := ed.SubmitPermissionsDraft(editor.PermissionsDraftRequest{
		Request: "allow scenes",
		Rules: []editor.PermissionsDraftRuleSpec{
			{Action: editor.PermissionActionEdit, Patterns: []string{"scenes/**"}, Decision: editor.PermissionAllow},
		},
	}); err != nil {
		t.Fatalf("SubmitPermissionsDraft: %v", err)
	}
	if !ed.PermissionsDraftOpen() {
		t.Fatal("expected PermissionsDraftOpen() == true")
	}
	for _, cmdID := range []string{editor.CmdSketchToggle, editor.CmdSketchApproveReading, editor.CmdSketchSend} {
		cmd, ok := ed.Command(cmdID)
		if !ok {
			t.Fatalf("command %s not found", cmdID)
		}
		if cmd.Enabled == nil || cmd.Enabled() {
			t.Fatalf("command %s enabled while PermissionsDraftOpen() == true, want disabled", cmdID)
		}
	}
	ed.DiscardPermissionsDraft()
}

package editormcp_test

import (
	"bytes"
	"image/png"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

// #1025 Acceptance: End-to-end scripted MCP agent flow over sketch_read, sketch_reading,
// scene_edit hold gate until Go ahead, Correct a mark, two zoom levels, Keep vs Revert,
// and verifying scene YAML never persists sketch marks.
func TestSketchMCPReadReadingHoldGateAndKeepRevert(t *testing.T) {
	f := openFixture(t)
	f.ed.SetCanvasSize(640, 360)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	initialYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	// Draw Box, Arrow, and Text marks at Zoom 1.0, then send.
	f.ed.SetView2D(editor.View2D{X: 320, Y: 180, Zoom: 1.0})
	f.ed.StartSketch()
	f.ed.SetSketchTool(editor.SketchToolBox)
	f.ed.BeginSketchDrag(180, 220)
	f.ed.UpdateSketchDrag(290, 260)
	f.ed.EndSketchDrag()
	f.ed.SetSketchMarkLabel(1, "3 spikes")

	f.ed.SetSketchTool(editor.SketchToolArrow)
	f.ed.BeginSketchDrag(410, 120)
	f.ed.UpdateSketchDrag(490, 240)
	f.ed.EndSketchDrag()
	f.ed.SetSketchMarkLabel(2, "locked until the lever")

	f.ed.SetSketchTool(editor.SketchToolText)
	f.ed.BeginSketchDrag(340, 150)
	f.ed.EndSketchDrag()
	f.ed.SetSketchMarkLabel(3, "platform, back and forth")

	if _, err := f.ed.SendSketch("Build what I sketched."); err != nil {
		t.Fatalf("SendSketch: %v", err)
	}

	// 1. Agent reads the sketch via sketch_read and gets exact scene coordinates + PNG.
	readRes := call(t, cs, editormcp.ToolSketchRead, map[string]any{})
	readText := mustOK(t, readRes)
	var sr editormcp.SketchReadResult
	structured(t, readRes, &sr)
	if len(sr.Marks) != 3 || sr.HoldState != "pending" || !sr.HoldActive {
		t.Fatalf("sketch_read = %+v\n%s", sr, readText)
	}
	if sr.ChipLabel != "Sketch · 3 marks" || sr.Prompt != "Build what I sketched." {
		t.Fatalf("sketch_read chip/prompt = %q / %q", sr.ChipLabel, sr.Prompt)
	}
	bx0, by0 := f.ed.CanvasToWorld(180, 220)
	if math.Abs(float64(sr.Marks[0].Rect.X-bx0)) > 0.01 || math.Abs(float64(sr.Marks[0].Rect.Y-by0)) > 0.01 {
		t.Fatalf("zoom 1 box scene coords = %+v, want (%v, %v)", sr.Marks[0].Rect, bx0, by0)
	}
	var gotPNG []byte
	for _, content := range readRes.Content {
		if ic, ok := content.(*mcp.ImageContent); ok && ic.MIMEType == "image/png" {
			gotPNG = ic.Data
		}
	}
	if len(gotPNG) == 0 {
		t.Fatal("sketch_read did not return a PNG image")
	}
	if _, err := png.Decode(bytes.NewReader(gotPNG)); err != nil {
		t.Fatalf("sketch_read returned invalid PNG: %v", err)
	}

	// Verify coordinates stay identical in scene space after zooming viewport to 2.0x.
	f.ed.SetView2D(editor.View2D{X: 380, Y: 200, Zoom: 2.0})
	readRes2 := call(t, cs, editormcp.ToolSketchRead, map[string]any{})
	mustOK(t, readRes2)
	var sr2 editormcp.SketchReadResult
	structured(t, readRes2, &sr2)
	if math.Abs(float64(sr2.Marks[0].Rect.X-bx0)) > 0.01 || math.Abs(float64(sr2.Marks[0].Rect.Y-by0)) > 0.01 {
		t.Fatalf("zoom 2 box scene coords drifted: %+v vs (%v, %v)", sr2.Marks[0].Rect, bx0, by0)
	}

	// 2. scene_edit MUST hold/refuse while the sent sketch is awaiting confirmation.
	editArgs := map[string]any{
		"edits": []any{
			map[string]any{"op": "add", "layer": "actors", "object": map[string]any{"id": "spikes_1", "x": 235, "y": 240}},
		},
	}
	if res := call(t, cs, editormcp.ToolSceneEdit, editArgs); !res.IsError || !strings.Contains(text(res), "sketch") {
		t.Fatalf("scene_edit while sketch pending = (%v, %q), want hold error", res.IsError, text(res))
	}
	if obj, _, _ := f.ed.FindObject("spikes_1"); obj != nil {
		t.Fatal("held scene_edit mutated the scene")
	}
	// Other mutating scene MCP tools must also refuse while held.
	if res := call(t, cs, editormcp.ToolObjectDuplicate, map[string]any{"id": "hero"}); !res.IsError || !strings.Contains(text(res), "sketch") {
		t.Fatalf("object_duplicate while sketch pending = (%v, %q), want hold error", res.IsError, text(res))
	}

	// 3. Agent posts its reading card via sketch_reading.
	readingRes := call(t, cs, editormcp.ToolSketchReading, map[string]any{
		"items": []any{
			map[string]any{"number": 1, "text": "Place three spike tiles at x 180..290."},
			map[string]any{"number": 2, "text": "Lock the door until lever_01 is pulled."},
			map[string]any{"number": 3, "text": "Add a moving platform around (340, 150)."},
		},
		"assumptions": []string{
			"There is no lever in the scene yet; I will add lever_01 at x 295 unless you point elsewhere.",
		},
	})
	mustOK(t, readingRes)
	var rOut editormcp.SketchReadingResult
	structured(t, readingRes, &rOut)
	if rOut.HoldState != "reading" || !rOut.HoldActive || rOut.Reading == nil || len(rOut.Reading.Items) != 3 {
		t.Fatalf("sketch_reading result = %+v", rOut)
	}

	// scene_edit MUST STILL hold after reading is posted, until user clicks Go ahead!
	if res := call(t, cs, editormcp.ToolSceneEdit, editArgs); !res.IsError || !strings.Contains(text(res), "sketch") {
		t.Fatalf("scene_edit before Go ahead = (%v, %q), want hold error", res.IsError, text(res))
	}

	// 4. Correct a mark reopens sketch mode with mark 2 selected and keeps hold active.
	if err := f.ed.CorrectSketchMark(2); err != nil {
		t.Fatalf("CorrectSketchMark(2): %v", err)
	}
	if !f.ed.SketchActive() || f.ed.SelectedSketchMark() != 2 || !f.ed.SketchHoldActive() {
		t.Fatalf("after CorrectSketchMark(2): active=%v sel=%d hold=%v", f.ed.SketchActive(), f.ed.SelectedSketchMark(), f.ed.SketchHoldActive())
	}
	if res := call(t, cs, editormcp.ToolSceneEdit, editArgs); !res.IsError {
		t.Fatalf("scene_edit while correcting mark succeeded, want hold error")
	}

	// 5. User clicks Go ahead -> scene_edit succeeds and opens Sketch Review.
	if err := f.ed.ApproveSketchReading(); err != nil {
		t.Fatalf("ApproveSketchReading: %v", err)
	}
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, editArgs))
	if obj, _, _ := f.ed.FindObject("spikes_1"); obj == nil {
		t.Fatal("scene_edit after Go ahead did not add spikes_1")
	}
	if !f.ed.SketchReviewOpen() || !f.ed.SketchReviewOverlayVisible() {
		t.Fatalf("after approved scene_edit: reviewOpen=%v overlay=%v, want true/true", f.ed.SketchReviewOpen(), f.ed.SketchReviewOverlayVisible())
	}

	// 6. Revert undoes spikes_1 AND keeps all 3 sketch marks!
	if err := f.ed.RevertSketchReview(); err != nil {
		t.Fatalf("RevertSketchReview: %v", err)
	}
	if obj, _, _ := f.ed.FindObject("spikes_1"); obj != nil {
		t.Fatal("RevertSketchReview did not remove spikes_1")
	}
	if f.ed.SketchMarkCount() != 3 {
		t.Fatalf("RevertSketchReview cleared marks: got %d, want 3", f.ed.SketchMarkCount())
	}
	// Scene YAML matches initial cleanly after Revert.
	revertedYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after revert: %v", err)
	}
	if !bytes.Equal(initialYAML, revertedYAML) {
		t.Fatalf("scene YAML after revert differs from initial:\n%s", string(revertedYAML))
	}

	// 7. Approve again, apply edit, Keep -> retains spikes_1 AND clears sketch layer; scene YAML never contains sketch data.
	if err := f.ed.ApproveSketchReading(); err != nil {
		t.Fatalf("ApproveSketchReading 2: %v", err)
	}
	mustOK(t, call(t, cs, editormcp.ToolSceneEdit, editArgs))
	if err := f.ed.KeepSketchReview(); err != nil {
		t.Fatalf("KeepSketchReview: %v", err)
	}
	if obj, _, _ := f.ed.FindObject("spikes_1"); obj == nil {
		t.Fatal("KeepSketchReview lost spikes_1")
	}
	if f.ed.SketchMarkCount() != 0 {
		t.Fatalf("KeepSketchReview did not clear sketch marks: got %d", f.ed.SketchMarkCount())
	}
	keptYAML, err := f.ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML after keep: %v", err)
	}
	if strings.Contains(string(keptYAML), "locked until the lever") || strings.Contains(string(keptYAML), "sketch") {
		t.Fatalf("kept scene YAML contains sketch data:\n%s", string(keptYAML))
	}
}

func TestSketchHoldGateBlocksAllMutatingToolsRecordsObjectReviewAndIsRaceFree(t *testing.T) {
	f := openFixture(t)
	f.ed.SetCanvasSize(640, 360)
	s := editormcp.New(f.ed, editormcp.Options{OnPermissionAsk: allowAsk})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	f.ed.AddSketchBox(80, 120, 60, 40, "spikes")
	if _, err := f.ed.SendSketch("build spikes"); err != nil {
		t.Fatalf("SendSketch: %v", err)
	}

	// Verify ALL mutating MCP tools respect the active sketch hold gate.
	blockedCalls := []struct {
		name string
		args map[string]any
	}{
		{editormcp.ToolRefactorRename, map[string]any{"from": "scenes/main.scene.yaml", "to": "scenes/renamed.scene.yaml"}},
		{editormcp.ToolTilesetEdit, map[string]any{"ops": []map[string]any{{"op": "set_tile", "id": 0, "solid": true}}}},
		{editormcp.ToolStatesWrite, map[string]any{"file": "hero.states.yaml", "yaml": "version: 1\nentry: idle\nstates:\n  - name: idle\n    clip: anim/idle.animation.yaml\n"}},
		{editormcp.ToolCutsceneWrite, map[string]any{"file": "intro.cutscene.yaml", "yaml": "version: 2\nkind: timeline\nscene: scenes/main.scene.yaml\nlength: 2s\ntracks:\n  - id: cam\n    kind: camera\n    clips:\n      - id: c1\n        start: 0s\n        duration: 2s\n        action: hold\n        target: hero\n"}},
		{editormcp.ToolMergeResolve, map[string]any{"allAgent": true}},
		{editormcp.ToolTileReviewKeep, map[string]any{}},
		{editormcp.ToolTileReviewRevertAll, map[string]any{}},
		{editormcp.ToolObjectSetField, map[string]any{"id": "hero", "field": "x", "value": 99}},
		{editormcp.ToolObjectDuplicate, map[string]any{"id": "hero"}},
	}
	for _, tc := range blockedCalls {
		res := call(t, cs, tc.name, tc.args)
		if !res.IsError {
			t.Errorf("%s succeeded while sketch hold was active, want hold error", tc.name)
			continue
		}
		if got := text(res); !strings.Contains(got, "waiting for the person to confirm") {
			t.Errorf("%s error = %q, want sketch hold refusal", tc.name, got)
		}
	}

	// Exercise concurrent sketch_read and sketch_reading calls under -race.
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			mustOK(t, call(t, cs, editormcp.ToolSketchRead, map[string]any{"includeImage": false}))
		}()
		go func() {
			defer wg.Done()
			mustOK(t, call(t, cs, editormcp.ToolSketchReading, map[string]any{
				"summary": "I read your sketch as:",
				"items":   []map[string]any{{"number": 1, "text": "Place spikes"}},
			}))
		}()
	}
	wg.Wait()

	// Approving allows object_set_field and records it into the post-sketch review card.
	if err := f.ed.ApproveSketchReading(); err != nil {
		t.Fatalf("ApproveSketchReading: %v", err)
	}
	mustOK(t, call(t, cs, editormcp.ToolObjectSetField, map[string]any{"id": "hero", "field": "x", "value": 123}))
	if !f.ed.SketchReviewOpen() {
		t.Fatal("expected SketchReviewOpen() == true after approved object_set_field")
	}
	changes := f.ed.SketchReviewChanges()
	if len(changes) == 0 || changes[0].ID != "hero" {
		t.Fatalf("SketchReviewChanges = %+v, want hero recorded", changes)
	}
}

func TestSketchToolsRespectPrivacyAndToolToggles(t *testing.T) {
	f := openFixture(t)
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	f.ed.AddSketchBox(10, 20, 40, 50, "spikes")
	if _, err := f.ed.SendSketch("build spikes"); err != nil {
		t.Fatalf("SendSketch: %v", err)
	}

	// Turning off Viewport images refuses sketch_read when an image is requested or implied by marks,
	// while still allowing includeImage: false so coordinates can be read without sending pixels.
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, false); err != nil {
		t.Fatalf("SetAgentPrivacy(ViewportImages, false): %v", err)
	}
	if res := call(t, cs, editormcp.ToolSketchRead, map[string]any{"includeImage": true}); !res.IsError {
		t.Fatal("sketch_read(includeImage:true) succeeded with ViewportImages disabled")
	}
	if res := call(t, cs, editormcp.ToolSketchRead, map[string]any{}); !res.IsError {
		t.Fatal("sketch_read({}) with marks succeeded with ViewportImages disabled")
	}
	mustOK(t, call(t, cs, editormcp.ToolSketchRead, map[string]any{"includeImage": false}))

	// Turning off Scene and data files refuses both sketch_read and sketch_reading.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, false); err != nil {
		t.Fatalf("SetAgentPrivacy(SceneAndData, false): %v", err)
	}
	if res := call(t, cs, editormcp.ToolSketchRead, map[string]any{"includeImage": false}); !res.IsError {
		t.Fatal("sketch_read succeeded with SceneAndData disabled")
	}
	if res := call(t, cs, editormcp.ToolSketchReading, map[string]any{
		"items": []map[string]any{{"number": 1, "text": "Place spikes"}},
	}); !res.IsError {
		t.Fatal("sketch_reading succeeded with SceneAndData disabled")
	}

	// Restore privacy and verify Settings › Tools toggle disables sketch_reading cleanly.
	if err := f.ed.SetAgentPrivacy(editor.PrivacySceneAndData, true); err != nil {
		t.Fatalf("SetAgentPrivacy(SceneAndData, true): %v", err)
	}
	if err := f.ed.SetAgentPrivacy(editor.PrivacyViewportImages, true); err != nil {
		t.Fatalf("SetAgentPrivacy(ViewportImages, true): %v", err)
	}
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolSketchReading, false); err != nil {
		t.Fatalf("SetMCPToolEnabled(sketch_reading, false): %v", err)
	}
	if res := call(t, cs, editormcp.ToolSketchReading, map[string]any{
		"items": []map[string]any{{"number": 1, "text": "Place spikes"}},
	}); !res.IsError || !strings.Contains(text(res), "turned off") {
		t.Fatalf("disabled sketch_reading result = (err=%v, %q), want turned-off refusal", res.IsError, text(res))
	}
}

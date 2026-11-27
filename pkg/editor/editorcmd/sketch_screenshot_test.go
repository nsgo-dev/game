package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosSketchPreviewScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#15181D"
layers:
  - name: Actors
    space: world
    order: 0
    objects:
      - id: floor
        name: Floor
        x: 320
        y: 300
      - id: knight
        name: Knight
        x: 112
        y: 238
        z: 10
      - id: plank
        name: Plank
        x: 342
        y: 215
      - id: door
        name: Door
        x: 522
        y: 248
`

// TestSketchMacOSScreenshots renders real native macOS screenshots of the viewport sketch layer
// (active numbered marks + floating bar + Scene Tree SKETCH LAYER + Composer preview),
// the agent reading card (with Go ahead / Correct a mark + warning assumptions),
// and the post-sketch review card (with Sketch overlay toggle + Keep / Revert)
// for visual verification beside design/editor/Sketch.dc.html and Spec.dc.html (#1025).
func TestSketchMacOSScreenshots(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(macosSketchPreviewScene))
	if err != nil {
		t.Fatalf("sceneformat.Parse: %v", err)
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
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

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

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

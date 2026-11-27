package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosAgentLiveScene = `version: 3
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
        y: 318
      - id: knight
        name: Knight
        x: 210
        y: 225
        z: 10
      - id: door
        name: Door
        x: 510
        y: 230
        z: 5
      - id: coin_04
        name: coin_04
        x: 410
        y: 252
        z: 2
`

// TestAgentLiveEditsMacOSScreenshots renders the live agent edit indicators in the viewport,
// scene tree, inspector, overlay legend, and header pill on native macOS in both Dark and Light
// Spec themes for visual inspection beside design/editor/Working.dc.html, Main.dc.html, and Spec.dc.html.
func TestAgentLiveEditsMacOSScreenshots(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(macosAgentLiveScene))
	if err != nil {
		t.Fatalf("parse scene: %v", err)
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	rm := true
	ed.SetReducedMotionOverride(&rm)
	ed.SelectObject("knight")

	// Open a pending turn with Added (slime_01, slime_02), Changed (door z 5 → 12), Removed (coin_04),
	// and Agent working coexisting with Selection on knight (3 px further out, read-only).
	ed.BeginAgentTurn("Place slimes and update door")
	if err := ed.RunAsAgent(func() error {
		if _, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{
			ID:   "slime_01",
			Name: "slime_01",
			X:    300,
			Y:    258,
		}); err != nil {
			return err
		}
		if _, err := ed.PlaceObject("Actors", sceneformat.ObjectDoc{
			ID:   "slime_02",
			Name: "slime_02",
			X:    355,
			Y:    258,
		}); err != nil {
			return err
		}
		if err := ed.SetObjectZ("door", 12); err != nil {
			return err
		}
		return ed.DeleteObject("coin_04")
	}); err != nil {
		t.Fatalf("agent turn edits: %v", err)
	}
	// Select knight so Selection + Agent working coexistence and Inspector read-only banner show together.
	ed.SelectObject("knight")
	ed.BeginAgentStep([]string{"knight"}, "playing level_01 headless")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()
	app.Screenshot("agent-live-working-dark")

	// Switch to Light theme to verify Spec Light contrast parity.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("agent-live-working-light")

	// End active step so header pill transitions to Pending Review (Keep / Revert) in Dark theme.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Post(func() { ed.EndAgentStep() })
	app.Settle()
	app.Screenshot("agent-live-review-dark")
}

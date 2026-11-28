package editor

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestPlayInEditorStepsMotionBehaviorsAndInspectorListsSchemas(t *testing.T) {
	// Load the showcase fixture containing all ten motion behaviors and play it in-editor with no Go callbacks.
	raw, err := os.ReadFile(filepath.Join("..", "sceneformat", "testdata", "behaviors.scene.yaml"))
	if err != nil {
		t.Fatalf("ReadFile behaviors.scene.yaml: %v", err)
	}
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ed, err := New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, name := range sceneformat.BehaviorComponentNames() {
		ct, ok := ed.lookupComponentType(name)
		if !ok || ct.Name != name {
			t.Fatalf("lookupComponentType(%q) = %v, want registered schema", name, ok)
		}
	}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if err := ed.PlayIn(cx); err != nil {
			t.Fatalf("PlayIn: %v", err)
		}
		return core.View{}
	})

	dt := time.Second / 60
	for range 120 {
		ed.runPlayStep(dt, nil)
	}

	if ed.playScene.Object("saw").Doc.Rotation == 0 {
		t.Error("saw did not rotate during Play-in-Editor")
	}
	if math.Abs(float64(ed.playScene.Object("coin").Doc.Y-60)) < 0.1 {
		t.Error("coin did not oscillate with sine during Play-in-Editor")
	}
	if ed.playScene.Object("toast") != nil {
		t.Error("toast should have been freed by fade(destroy: true) during Play-in-Editor")
	}

	// Stop restores the exact pre-play scene snapshot.
	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ed.Built.Object("toast") == nil {
		t.Error("Stop did not restore freed toast object")
	}
	if ed.Built.Object("saw").Doc.Rotation != 0 {
		t.Errorf("Stop did not restore saw rotation: got %v", ed.Built.Object("saw").Doc.Rotation)
	}
}

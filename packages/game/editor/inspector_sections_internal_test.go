package editor

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

func TestInspectorProblemMatchingUsesWordBoundaries(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "level_01",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{ID: "slime_02", Name: "slime_02", X: 284, Y: 340, Z: 10},
					{ID: "box", Name: "Box", X: 10, Y: 20},
				},
			},
		},
	}
	ed, err := New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.ScenePath = "scenes/level_01.scene.yaml"
	ed.problems.problems = []assetcheck.Problem{
		// Contains letters 'x' ("texture"), 'y' ("layer"), 'z' ("size") — must NOT trigger x/y/z pills.
		{File: ed.ScenePath, Message: `object "slime_02": missing sprite texture on layer actors (size invalid)`},
		// Mentions "skybox" — must NOT match object "box".
		{File: ed.ScenePath, Message: `object "skybox": invalid alpha`},
	}

	for _, field := range []string{"x", "y", "z"} {
		if msg, ok := ed.inspectorFieldProblemFor("slime_02", "", field); ok {
			t.Fatalf("false-positive problem on slime_02.%s: %q", field, msg)
		}
	}
	if msg, ok := ed.inspectorFieldProblemFor("box", "", "alpha"); ok {
		t.Fatalf("false-positive problem on box.alpha from skybox message: %q", msg)
	}
	// Sprite field on slime_02 SHOULD still match word-bounded "sprite".
	if _, ok := ed.inspectorFieldProblemFor("slime_02", "", "sprite"); !ok {
		t.Fatal("expected word-bounded sprite problem on slime_02")
	}

	// Explicit field reference ("field y") on slime_02 MUST match y (and NOT x or z).
	ed.problems.problems = []assetcheck.Problem{
		{File: ed.ScenePath, Message: `object "slime_02": field y must be >= 0`},
	}
	if _, ok := ed.inspectorFieldProblemFor("slime_02", "", "y"); !ok {
		t.Fatal("expected problem match for explicit field y")
	}
	if msg, ok := ed.inspectorFieldProblemFor("slime_02", "", "x"); ok {
		t.Fatalf("unexpected problem match for x: %q", msg)
	}
	if msg, ok := ed.inspectorFieldProblemFor("slime_02", "", "z"); ok {
		t.Fatalf("unexpected problem match for z: %q", msg)
	}
}

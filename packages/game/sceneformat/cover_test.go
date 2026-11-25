package sceneformat_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const coverSceneYAML = `version: 2
name: CoverArena
layers:
  - name: tactical
    objects:
      - id: crate_cover
        x: 64
        "y": 80
        components:
          cover_point:
            facingX: 1
            facingY: 0
            height: half
            tags: flank, crate
      - id: bunker_group
        x: 120
        "y": 40
        rotation: 1.5707964
        children:
          - id: bunker_cover
            x: 10
            components:
              cover_point:
                facingX: 1
                facingY: 0
                height: full
      - id: default_cover
        x: 200
        "y": 120
        components:
          cover_point: {}
`

func TestCoverPointComponentRoundTripAndExtract(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterCoverPoint(reg); err != nil {
		t.Fatalf("RegisterCoverPoint: %v", err)
	}

	doc, err := sceneformat.ParseWith([]byte(coverSceneYAML), sceneformat.ParseOptions{
		File:       "cover.scene.yaml",
		Components: reg,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	wantYAML := sceneformat.SchemaCommentV2 + "\n" + coverSceneYAML
	if string(encoded) != wantYAML {
		t.Fatalf("YAML round-trip changed the file:\n--- got ---\n%s\n--- want ---\n%s", encoded, wantYAML)
	}

	pts, err := sceneformat.ExtractCoverPoints(doc)
	if err != nil {
		t.Fatalf("ExtractCoverPoints: %v", err)
	}
	if len(pts) != 3 {
		t.Fatalf("extracted %d cover points, want 3", len(pts))
	}

	// 1. crate_cover: half cover at (64, 80) facing (+1, 0) with tags ["flank", "crate"].
	if pts[0].ID != 1 || !pts[0].Pos.ApproxEqual(math2d.V2(64, 80), 1e-4) ||
		!pts[0].Facing.ApproxEqual(math2d.V2(1, 0), 1e-4) ||
		pts[0].Height != ai.CoverHalf || !slices.Equal(pts[0].Tags, []string{"flank", "crate"}) {
		t.Fatalf("pts[0] = %+v, unexpected", pts[0])
	}

	// 2. bunker_cover: child of bunker_group rotated +pi/2 at (120, 40) -> local (10, 0) becomes (120, 50),
	// local facing (1, 0) rotates to (0, 1).
	if pts[1].ID != 2 || !pts[1].Pos.ApproxEqual(math2d.V2(120, 50), 1e-3) ||
		!pts[1].Facing.ApproxEqual(math2d.V2(0, 1), 1e-3) ||
		pts[1].Height != ai.CoverFull {
		t.Fatalf("pts[1] = %+v, want pos=(120, 50) facing=(0, 1) full", pts[1])
	}

	// 3. default_cover: defaults height="full", facing=(0, -1).
	if pts[2].ID != 3 || !pts[2].Pos.ApproxEqual(math2d.V2(200, 120), 1e-4) ||
		!pts[2].Facing.ApproxEqual(math2d.V2(0, -1), 1e-4) ||
		pts[2].Height != ai.CoverFull {
		t.Fatalf("pts[2] = %+v, want default full cover facing (0, -1)", pts[2])
	}

	// Verify BuiltScene extraction produces identical cover points and feeds directly into ai.TacticalQuery.
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	builtPts, err := sceneformat.ExtractBuiltCoverPoints(built)
	if err != nil {
		t.Fatalf("ExtractBuiltCoverPoints: %v", err)
	}
	if len(builtPts) != len(pts) {
		t.Fatalf("ExtractBuiltCoverPoints count = %d, want %d", len(builtPts), len(pts))
	}

	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{&ai.CoverPointGenerator{Points: builtPts}},
		&ai.AngleTest{Mode: ai.AngleCoverToTarget, Filter: ai.AtMost(math.Pi / 4), Weight: 1.0, Invert: true},
	)
	best, ok := q.Best(ai.TacticalContext{
		QuerierPos: math2d.V2(60, 80),
		TargetPos:  math2d.V2(200, 80), // east of crate_cover -> crate_cover faces (+1, 0) directly at target
	})
	if !ok || best.CoverID != 1 {
		t.Fatalf("query over scene cover points got (%+v, %v), want CoverID 1", best, ok)
	}
}

func TestCoverPointValidationErrors(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterCoverPoint(reg); err != nil {
		t.Fatalf("RegisterCoverPoint: %v", err)
	}

	t.Run("unknown field rejected on ParseWith", func(t *testing.T) {
		bad := strings.Replace(coverSceneYAML, "height: half", "hight: half", 1)
		_, err := sceneformat.ParseWith([]byte(bad), sceneformat.ParseOptions{File: "bad.yaml", Components: reg})
		var ce *sceneformat.ComponentError
		if !errors.As(err, &ce) || ce.Field != "hight" {
			t.Fatalf("err = %v, want ComponentError for unknown field hight", err)
		}
	})

	t.Run("invalid height rejected on ExtractCoverPoints", func(t *testing.T) {
		bad := strings.Replace(coverSceneYAML, "height: half", "height: tall", 1)
		doc, err := sceneformat.ParseWith([]byte(bad), sceneformat.ParseOptions{File: "bad.yaml", Components: reg})
		if err != nil {
			t.Fatalf("ParseWith: %v", err)
		}
		_, err = sceneformat.ExtractCoverPoints(doc)
		var ce *sceneformat.ComponentError
		if !errors.As(err, &ce) || ce.Field != "height" || ce.Object != "crate_cover" {
			t.Fatalf("ExtractCoverPoints err = %v, want ComponentError on crate_cover.height", err)
		}
	})

	t.Run("zero facing rejected on ExtractCoverPoints", func(t *testing.T) {
		bad := strings.Replace(coverSceneYAML, "facingX: 1\n            facingY: 0", "facingX: 0\n            facingY: 0", 1)
		doc, err := sceneformat.ParseWith([]byte(bad), sceneformat.ParseOptions{File: "bad.yaml", Components: reg})
		if err != nil {
			t.Fatalf("ParseWith: %v", err)
		}
		_, err = sceneformat.ExtractCoverPoints(doc)
		var ce *sceneformat.ComponentError
		if !errors.As(err, &ce) || ce.Object != "crate_cover" {
			t.Fatalf("ExtractCoverPoints err = %v, want ComponentError on crate_cover", err)
		}
	})
}

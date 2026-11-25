package physics3d_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func TestGenerateCoverPoints3D(t *testing.T) {
	w := physics3d.NewWorld()

	// Flat floor slab (height = 0.2 < HalfCoverMinHeight) must NOT produce cover points.
	floor := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.1, Z: 20}}),
		physics3d.Vec3{X: 0, Y: -0.1, Z: 0},
	)
	floor.Tag = "floor"
	w.AddBody(floor)

	// Waist-high crate (height = 1.0 -> CoverHalf).
	crate := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1.0, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 0, Y: 0.5, Z: 0},
	)
	crate.Tag = "crate"
	w.AddBody(crate)

	// Full-height rotated barricade (45 degrees around Y, height = 2.0 -> CoverFull).
	wall := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1.5, Y: 1.0, Z: 0.3}}),
		physics3d.Vec3{X: 6, Y: 1.0, Z: 0},
	)
	wall.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(math.Pi/4))
	wall.Tag = "wall"
	w.AddBody(wall)

	// Circular full-height pillar (capsule, height = 2.4 -> CoverFull).
	pillar := physics3d.NewStaticBody(
		physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.6, 2.4)),
		physics3d.Vec3{X: -5, Y: 1.2, Z: 4},
	)
	pillar.Tag = "pillar"
	w.AddBody(pillar)

	// Standing cylinder pillar (height = 2.0 -> CoverFull) and a barrel lying on its side, which is skipped.
	column := physics3d.NewStaticBody(
		physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.5, 2.0)),
		physics3d.Vec3{X: 5, Y: 1.0, Z: 6},
	)
	column.Tag = "column"
	w.AddBody(column)
	lying := physics3d.NewStaticBody(
		physics3d.CylinderShape(physics3d.Cylinder{A: physics3d.Vec3{X: -1}, B: physics3d.Vec3{X: 1}, Radius: 0.5}),
		physics3d.Vec3{X: -6, Y: 0.5, Z: -6},
	)
	lying.Tag = "lying"
	w.AddBody(lying)

	// Dynamic crate skipped by default when IncludeDynamic is false.
	dyn := physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.6, Y: 0.5, Z: 0.6}}),
		physics3d.Vec3{X: 0, Y: 0.5, Z: -6},
		10,
	)
	dyn.Tag = "dynamic"
	w.AddBody(dyn)

	opts := physics3d.CoverOptions{
		Spacing:  1.0,
		Standoff: 0.6,
	}
	pts := w.GenerateCoverPoints(opts)
	if len(pts) == 0 {
		t.Fatal("expected generated 3D cover points")
	}

	var sawCrateHalf, sawWallFull, sawPillarFull, sawColumnFull bool
	for _, cp := range pts {
		if cp.HasTag("floor") {
			t.Fatalf("flat floor slab should not generate cover: %+v", cp)
		}
		if cp.HasTag("dynamic") {
			t.Fatalf("dynamic body should be skipped when IncludeDynamic=false: %+v", cp)
		}
		if math.Abs(float64(cp.Facing.Len()-1)) > 1e-3 {
			t.Fatalf("non-unit facing %v on cover %+v", cp.Facing, cp)
		}
		if math.Abs(float64(cp.Elevation)) > 1e-3 {
			t.Fatalf("expected floor elevation ~0, got %v on %+v", cp.Elevation, cp)
		}
		if cp.HasTag("crate") && cp.Height == ai.CoverHalf {
			sawCrateHalf = true
		}
		if cp.HasTag("wall") && cp.Height == ai.CoverFull {
			sawWallFull = true
		}
		if cp.HasTag("pillar") && cp.Height == ai.CoverFull {
			sawPillarFull = true
		}
		if cp.HasTag("column") && cp.Height == ai.CoverFull {
			sawColumnFull = true
			if d := cp.Pos.Sub(math2d.V2(5, 6)).Len(); math.Abs(float64(d-1.1)) > 1e-3 {
				t.Fatalf("column cover %+v is %v from the column's axis, want radius + standoff 1.1", cp, d)
			}
		}
		if cp.HasTag("lying") {
			t.Fatalf("a cylinder lying on its side should not generate cover: %+v", cp)
		}
	}
	if !sawCrateHalf || !sawWallFull || !sawPillarFull || !sawColumnFull {
		t.Fatalf("missing cover types: crate=%v wall=%v pillar=%v column=%v", sawCrateHalf, sawWallFull, sawPillarFull, sawColumnFull)
	}

	// Determinism across repeated runs.
	pts2 := w.GenerateCoverPoints(opts)
	if !slices.EqualFunc(pts, pts2, func(a, b ai.CoverPoint) bool {
		return a.ID == b.ID && a.Pos == b.Pos && a.Facing == b.Facing && a.Height == b.Height && a.Elevation == b.Elevation && slices.Equal(a.Tags, b.Tags)
	}) {
		t.Fatal("GenerateCoverPoints output was not deterministic across repeated calls")
	}

	// Verify cover points around the crate integrate directly with ai.TacticalQuery on the XZ plane.
	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{&ai.CoverPointGenerator{Points: pts, RequiredTag: "crate"}},
		&ai.AngleTest{Mode: ai.AngleCoverToTarget, Filter: ai.AtMost(math.Pi / 6), Weight: 2.0, Invert: true},
		&ai.DistanceTest{To: ai.OriginQuerier, Weight: 1.0, Invert: true},
	)
	best, ok := q.Best(ai.TacticalContext{
		QuerierPos: math2d.V2(0, -3),
		TargetPos:  math2d.V2(0, 10), // threat is in +Z, so agent should shelter on the -Z side facing +Z (0, 1)
	})
	if !ok {
		t.Fatal("expected valid 3D crate cover point")
	}
	if best.Pos.Y >= 0 || best.Facing.Y < 0.9 {
		t.Fatalf("expected cover on -Z side facing +Z, got pos=%v facing=%v", best.Pos, best.Facing)
	}
}

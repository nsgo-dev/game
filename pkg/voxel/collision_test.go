package voxel_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/voxel"
)

func buildTestTerrain(t *testing.T, chunkSize int, voxelSize float32) (*voxel.World, voxel.BlockID, voxel.BlockID) {
	t.Helper()
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
	water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, MaterialID: 1})

	w, err := voxel.NewWorld(voxel.WorldConfig{
		ChunkSize: chunkSize,
		VoxelSize: voxelSize,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Multi-chunk terrain spanning negative and positive coordinates:
	// - Flat ground at y = 0 for x in [-10..20], z in [-6..6]
	// - Single-block step at x in [6..10], y = 1
	// - Second single-block step at x in [11..14], y = 2
	// - Tall 3-block cliff wall at x = 15, y in [1..4]
	// - Water pool (non-solid) at x in [2..3], y = 1
	w.FillBox(voxel.BlockCoord{X: -10, Y: 0, Z: -6}, voxel.BlockCoord{X: 20, Y: 0, Z: 6}, stone)
	w.FillBox(voxel.BlockCoord{X: 6, Y: 1, Z: -4}, voxel.BlockCoord{X: 10, Y: 1, Z: 4}, stone)
	w.FillBox(voxel.BlockCoord{X: 11, Y: 1, Z: -4}, voxel.BlockCoord{X: 14, Y: 2, Z: 4}, stone)
	w.FillBox(voxel.BlockCoord{X: 15, Y: 1, Z: -4}, voxel.BlockCoord{X: 15, Y: 4, Z: 4}, stone)
	w.FillBox(voxel.BlockCoord{X: 2, Y: 1, Z: -2}, voxel.BlockCoord{X: 3, Y: 1, Z: 2}, water)
	return w, stone, water
}

func TestVoxelWorldColliderImmediateSetBlockAndNonSolid(t *testing.T) {
	w, stone, water := buildTestTerrain(t, 8, 1.0)
	vc := w.Collider()

	// Water is non-solid and must not block IsSolid, overlap, raycast, or sweep.
	if w.IsSolid(voxel.BlockCoord{X: 2, Y: 1, Z: 0}) {
		t.Fatal("non-solid water reported as solid")
	}
	if !w.IsSolid(voxel.BlockCoord{X: 6, Y: 1, Z: 0}) {
		t.Fatal("solid stone reported as non-solid")
	}

	waterProbe := physics3d.BoxShape(physics3d.Box{
		Center:      physics3d.Vec3{X: 2.5, Y: 1.5, Z: 0.5},
		HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3},
	})
	if c := vc.TestShape(waterProbe); c.Hit {
		t.Fatalf("collider unexpectedly hit non-solid water: %+v", c)
	}

	// Raycast passes straight through water and hits stone at x = 6.
	rh, ok := vc.Raycast(physics3d.Ray{
		Origin:  physics3d.Vec3{X: 0.5, Y: 1.5, Z: 0.5},
		Dir:     physics3d.Vec3{X: 1},
		MaxDist: 20,
	})
	if !ok || math.Abs(float64(rh.Point.X-6.0)) > 1e-4 || rh.Normal != (physics3d.Vec3{X: -1}) {
		t.Fatalf("unexpected raycast through water: ok=%v hit=%+v", ok, rh)
	}

	// Place a solid block at (3, 1, 0) via SetBlock — zero meshing / zero rebuild!
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 1, Z: 0}, stone)

	rh2, ok := vc.Raycast(physics3d.Ray{
		Origin:  physics3d.Vec3{X: 0.5, Y: 1.5, Z: 0.5},
		Dir:     physics3d.Vec3{X: 1},
		MaxDist: 20,
	})
	if !ok || math.Abs(float64(rh2.Point.X-3.0)) > 1e-4 {
		t.Fatalf("SetBlock edit not reflected immediately in Raycast: ok=%v hit=%+v", ok, rh2)
	}
	sh, ok := vc.Sweep(physics3d.BoxShape(physics3d.Box{
		Center:      physics3d.Vec3{X: 0.5, Y: 1.5, Z: 0.5},
		HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3},
	}), physics3d.Vec3{X: 10})
	if !ok || sh.BlockX != 3 || sh.BlockY != 1 || sh.BlockZ != 0 || math.Abs(float64(sh.Distance-2.2)) > 1e-3 {
		t.Fatalf("SetBlock edit not reflected immediately in Sweep: ok=%v hit=%+v", ok, sh)
	}

	// Replace with water again: sweep passes right through to x=6.
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 1, Z: 0}, water)
	sh2, ok := vc.Sweep(physics3d.BoxShape(physics3d.Box{
		Center:      physics3d.Vec3{X: 0.5, Y: 1.5, Z: 0.5},
		HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3},
	}), physics3d.Vec3{X: 10})
	if !ok || sh2.BlockX != 6 || sh2.BlockY != 1 || sh2.BlockZ != 0 {
		t.Fatalf("replacing solid with non-solid not reflected immediately: ok=%v hit=%+v", ok, sh2)
	}
}

func TestVoxelCrateRestAndFallOnSetBlock(t *testing.T) {
	w, stone, _ := buildTestTerrain(t, 8, 1.0)
	// Also test negative chunk coordinates by dropping a crate at (-5, 0, -3).
	w.SetBlock(voxel.BlockCoord{X: -5, Y: 0, Z: -3}, stone)

	phys := physics3d.NewWorld()
	vc := voxel.NewCollider(w)
	phys.AddVoxelCollider(vc)

	// Crate 1 sits on (0, 0, 0), Crate 2 sits stacked on top of Crate 1.
	crate1 := phys.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		physics3d.Vec3{X: 0.5, Y: 3.0, Z: 0.5},
		1.0,
	))
	crate2 := phys.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		physics3d.Vec3{X: 0.5, Y: 4.0, Z: 0.5},
		1.0,
	))
	// Distant crate on (-5, 0, -3) should stay asleep when (0, 0, 0) is removed.
	farCrate := phys.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		physics3d.Vec3{X: -4.5, Y: 3.0, Z: -2.5},
		1.0,
	))

	dt := time.Second / 60
	for range 240 {
		phys.Step(dt)
	}
	if !crate1.Sleeping || !crate2.Sleeping || !farCrate.Sleeping {
		t.Fatalf("crates did not settle to sleep: c1=%v c2=%v far=%v", crate1.Sleeping, crate2.Sleeping, farCrate.Sleeping)
	}
	if math.Abs(float64(crate1.Pos.Y-1.4)) > 0.02 {
		t.Fatalf("crate1 resting Y=%.4f, want ~1.4", crate1.Pos.Y)
	}
	if math.Abs(float64(crate2.Pos.Y-2.2)) > 0.03 {
		t.Fatalf("crate2 resting Y=%.4f, want ~2.2", crate2.Pos.Y)
	}

	// Remove the block underneath crate1 via SetBlock — zero rebuild!
	w.SetBlock(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.Air)

	// Both stacked crates should wake immediately, while farCrate stays asleep.
	if crate1.Sleeping || crate2.Sleeping {
		t.Fatalf("SetBlock did not wake supported crates: c1=%v c2=%v", crate1.Sleeping, crate2.Sleeping)
	}
	if !farCrate.Sleeping {
		t.Fatal("SetBlock needlessly woke distant crate")
	}

	for range 90 {
		phys.Step(dt)
	}
	if crate1.Pos.Y >= 0.0 {
		t.Fatalf("crate1 did not fall after block removal: Y=%.3f", crate1.Pos.Y)
	}
	if crate2.Pos.Y >= 0.5 {
		t.Fatalf("crate2 did not fall after block removal: Y=%.3f", crate2.Pos.Y)
	}
	if !farCrate.Sleeping || math.Abs(float64(farCrate.Pos.Y-1.4)) > 0.02 {
		t.Fatalf("farCrate disturbed: sleeping=%v Y=%.4f", farCrate.Sleeping, farCrate.Pos.Y)
	}
}

func TestVoxelCharacterControllerWalkStepUpJumpOnGeneratedTerrain(t *testing.T) {
	w, _, _ := buildTestTerrain(t, 8, 1.0)
	phys := physics3d.NewWorld()
	phys.AddVoxelCollider(w.Collider())

	// Start in negative X, walk through water (non-solid), across chunk border x=0 and x=8,
	// step up two 1-block ledges (y=1 -> y=2 -> y=3), stop at the 3-block cliff at x=15.
	cc := physics3d.NewCharacterController(physics3d.Vec3{X: -3.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
	cc.StepHeight = 1.05
	dt := time.Second / 60

	for range 5 {
		cc.Move(phys, physics3d.Vec3{}, dt)
	}
	if !cc.Grounded {
		t.Fatalf("character failed to ground on generated terrain: %+v", cc.Pos)
	}

	stepUps := 0
	for range 360 {
		res := cc.Move(phys, physics3d.Vec3{X: 5.0}, dt)
		if res.SteppedUp {
			stepUps++
		}
	}
	if stepUps < 2 {
		t.Fatalf("expected >= 2 single-block step-ups, got %d (pos=%+v)", stepUps, cc.Pos)
	}
	if math.Abs(float64(cc.Pos.Y-3.0)) > 0.03 {
		t.Fatalf("expected character on top of 2nd ledge (Y=3.0), got Y=%.4f (X=%.3f)", cc.Pos.Y, cc.Pos.X)
	}
	// Stopped by wall at x = 15 (feet X <= 15 - 0.3 = 14.7).
	if cc.Pos.X > 14.75 || !cc.OnWall {
		t.Fatalf("expected character blocked by 3-block wall at x=15: pos=%+v onWall=%v", cc.Pos, cc.OnWall)
	}

	// Jump in place and land cleanly on Y=3.0.
	jumpRes := cc.Move(phys, physics3d.Vec3{Y: 6.5}, dt)
	if jumpRes.Grounded {
		t.Fatal("expected jump to leave ground")
	}
	peakY := cc.Pos.Y
	for range 120 {
		cc.Move(phys, physics3d.Vec3{}, dt)
		if cc.Pos.Y > peakY {
			peakY = cc.Pos.Y
		}
	}
	if peakY < 4.5 {
		t.Fatalf("jump apex too low: peakY=%.3f (want >= 4.5)", peakY)
	}
	if !cc.Grounded || math.Abs(float64(cc.Pos.Y-3.0)) > 0.03 {
		t.Fatalf("character did not land cleanly after jump: grounded=%v pos=%+v", cc.Grounded, cc.Pos)
	}
}

func TestVoxelCharacterMoverWalkStepUpJumpAndCeiling(t *testing.T) {
	w, stone, _ := buildTestTerrain(t, 8, 1.0)
	// Add a low ceiling overhang at x in [-6..-5], y = 3 so jumping hits the ceiling.
	w.FillBox(voxel.BlockCoord{X: -6, Y: 3, Z: -1}, voxel.BlockCoord{X: -5, Y: 3, Z: 1}, stone)

	cm := voxel.NewCharacterMover(math3d.Vec3{X: -5.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
	dt := time.Second / 60

	// 1. Jump under ceiling at y = 3 (ceiling bottom at y=3.0, feet max Y = 3.0 - 1.8 = 1.2).
	hitCeiling := false
	for tick := range 60 {
		wish := math3d.Vec3{}
		if tick == 0 {
			wish.Y = 7.0
		}
		res := cm.Move(w, wish, dt)
		if res.OnCeiling {
			hitCeiling = true
		}
		if cm.Pos.Y+cm.Height > 3.001 {
			t.Fatalf("character penetrated ceiling: top=%.4f > 3.0", cm.Pos.Y+cm.Height)
		}
	}
	if !hitCeiling {
		t.Fatal("expected ceiling hit under overhang")
	}
	if !cm.Grounded || math.Abs(float64(cm.Pos.Y-1.0)) > 1e-4 {
		t.Fatalf("character did not return to floor y=1.0: grounded=%v pos=%+v", cm.Grounded, cm.Pos)
	}

	// 2. Walk from x=-5.5 across negative/positive chunks, through non-solid water,
	//    step up single-block ledges (y=1 -> y=2 -> y=3), and stop at 3-block wall x=15.
	stepUps := 0
	for range 360 {
		res := cm.Move(w, math3d.Vec3{X: 5.0}, dt)
		if res.SteppedUp {
			stepUps++
		}
	}
	if stepUps != 2 {
		t.Fatalf("expected exactly 2 single-block step-ups, got %d (pos=%+v)", stepUps, cm.Pos)
	}
	if math.Abs(float64(cm.Pos.Y-3.0)) > 1e-4 {
		t.Fatalf("expected Y=3.0 on second ledge, got %.5f", cm.Pos.Y)
	}
	if math.Abs(float64(cm.Pos.X-14.7)) > 1e-3 || !cm.OnWall || cm.WallNormal != (math3d.Vec3{X: -1}) {
		t.Fatalf("expected wall block at x=14.7 with normal -X: pos=%+v onWall=%v wallN=%+v", cm.Pos, cm.OnWall, cm.WallNormal)
	}

	// 3. Jump on top of ledge and land cleanly.
	res := cm.Move(w, math3d.Vec3{Y: 6.0}, dt)
	if res.Grounded {
		t.Fatal("expected jump to lift off ground")
	}
	peakY := cm.Pos.Y
	for range 120 {
		cm.Move(w, math3d.Vec3{}, dt)
		if cm.Pos.Y > peakY {
			peakY = cm.Pos.Y
		}
	}
	if peakY < 4.5 {
		t.Fatalf("CharacterMover jump apex too low: %.3f", peakY)
	}
	if !cm.Grounded || math.Abs(float64(cm.Pos.Y-3.0)) > 1e-4 {
		t.Fatalf("CharacterMover failed to land cleanly: grounded=%v pos=%+v", cm.Grounded, cm.Pos)
	}

	// 4. Remove the ledge block under the character -> falls immediately.
	w.SetBlock(voxel.BlockCoord{X: 14, Y: 2, Z: 0}, voxel.Air)
	w.SetBlock(voxel.BlockCoord{X: 14, Y: 2, Z: 1}, voxel.Air)
	for range 45 {
		cm.Move(w, math3d.Vec3{}, dt)
	}
	if cm.Pos.Y >= 2.5 {
		t.Fatalf("CharacterMover did not fall after supporting block removed: Y=%.3f", cm.Pos.Y)
	}
}

func TestVoxelStepUpBlockedByLowCeiling(t *testing.T) {
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, VoxelSize: 1.0, Registry: reg})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Floor at y=0, step block at (3, 1, 0), low ceiling block at (3, 3, 0) leaving only 1 unit height above step.
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: -1}, voxel.BlockCoord{X: 6, Y: 0, Z: 1}, stone)
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 1, Z: 0}, stone)
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 3, Z: 0}, stone)

	cm := voxel.NewCharacterMover(math3d.Vec3{X: 1.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
	dt := time.Second / 60
	for range 60 {
		res := cm.Move(w, math3d.Vec3{X: 4.0}, dt)
		if res.SteppedUp {
			t.Fatalf("stepped up despite insufficient headroom: pos=%+v", cm.Pos)
		}
	}
	if cm.Pos.X > 2.75 || cm.Pos.Y > 1.01 {
		t.Fatalf("character squeezed under low ceiling: pos=%+v", cm.Pos)
	}
}

func TestVoxelCollisionDeterminism(t *testing.T) {
	type snapshot struct {
		cratePos physics3d.Vec3
		crateVel physics3d.Vec3
		crateRot physics3d.Quat
		ccPos    physics3d.Vec3
		moverPos math3d.Vec3
		moverVel math3d.Vec3
	}

	simulate := func() snapshot {
		w, _, _ := buildTestTerrain(t, 8, 1.0)
		phys := physics3d.NewWorld()
		phys.AddVoxelCollider(w.Collider())

		crate := phys.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
			physics3d.Vec3{X: 0.5, Y: 3.5, Z: 0.5},
			1.0,
		))
		cc := physics3d.NewCharacterController(physics3d.Vec3{X: -2.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)
		cc.StepHeight = 1.05
		mover := voxel.NewCharacterMover(math3d.Vec3{X: -2.5, Y: 1.0, Z: 0.5}, 0.3, 1.8)

		dt := time.Second / 60
		for tick := range 220 {
			if tick == 160 {
				w.SetBlock(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.Air)
			}
			phys.Step(dt)
			jumpCC := float32(0)
			jumpM := float32(0)
			if tick == 100 {
				jumpCC = 6.0
				jumpM = 6.0
			}
			cc.Move(phys, physics3d.Vec3{X: 4.0, Y: jumpCC}, dt)
			mover.Move(w, math3d.Vec3{X: 4.0, Y: jumpM}, dt)
		}
		return snapshot{
			cratePos: crate.Pos,
			crateVel: crate.Vel,
			crateRot: crate.Rot,
			ccPos:    cc.Pos,
			moverPos: mover.Pos,
			moverVel: mover.Vel,
		}
	}

	a := simulate()
	b := simulate()

	eqP := func(x, y physics3d.Vec3) bool {
		return math.Float32bits(x.X) == math.Float32bits(y.X) &&
			math.Float32bits(x.Y) == math.Float32bits(y.Y) &&
			math.Float32bits(x.Z) == math.Float32bits(y.Z)
	}
	eqM := func(x, y math3d.Vec3) bool {
		return math.Float32bits(x.X) == math.Float32bits(y.X) &&
			math.Float32bits(x.Y) == math.Float32bits(y.Y) &&
			math.Float32bits(x.Z) == math.Float32bits(y.Z)
	}
	eqQ := func(x, y physics3d.Quat) bool {
		return math.Float32bits(x.X) == math.Float32bits(y.X) &&
			math.Float32bits(x.Y) == math.Float32bits(y.Y) &&
			math.Float32bits(x.Z) == math.Float32bits(y.Z) &&
			math.Float32bits(x.W) == math.Float32bits(y.W)
	}

	if !eqP(a.cratePos, b.cratePos) ||
		!eqP(a.crateVel, b.crateVel) ||
		!eqQ(a.crateRot, b.crateRot) ||
		!eqP(a.ccPos, b.ccPos) ||
		!eqM(a.moverPos, b.moverPos) ||
		!eqM(a.moverVel, b.moverVel) {
		t.Fatalf("non-deterministic results across runs:\nA=%+v\nB=%+v", a, b)
	}
}

func TestVoxelMoveAndSlideDiagonalWallNonUnitScaleAndClearWake(t *testing.T) {
	// Non-unit voxel size (0.5): floor at y=0 (top at 0.5), step at x=4..6, y=1 (top at 1.0), wall at x=7.
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, VoxelSize: 0.5, Registry: reg})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: -2}, voxel.BlockCoord{X: 8, Y: 0, Z: 10}, stone)
	w.FillBox(voxel.BlockCoord{X: 4, Y: 1, Z: -2}, voxel.BlockCoord{X: 6, Y: 1, Z: 10}, stone)
	w.FillBox(voxel.BlockCoord{X: 7, Y: 1, Z: -2}, voxel.BlockCoord{X: 7, Y: 4, Z: 10}, stone)

	cm := voxel.NewCharacterMover(math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.25}, 0.2, 0.9)
	cm.StepHeight = w.VoxelSize() // 0.5 (one block)
	dt := time.Second / 60

	// Walk diagonally (+X and +Z): climbs the 0.5-unit ledge, then slides along the X wall in +Z.
	stepped := false
	for range 150 {
		res := cm.Move(w, math3d.Vec3{X: 2.5, Z: 1.0}, dt)
		if res.SteppedUp {
			stepped = true
		}
	}
	if !stepped {
		t.Fatalf("expected 0.5-voxel step-up, got pos=%+v", cm.Pos)
	}
	if math.Abs(float64(cm.Pos.Y-1.0)) > 1e-3 {
		t.Fatalf("expected Y=1.0 on 0.5-unit ledge, got %.4f", cm.Pos.Y)
	}
	// Wall starts at x = 7*0.5 = 3.5 -> max center X = 3.5 - 0.2 = 3.3, while Z kept sliding cleanly.
	if math.Abs(float64(cm.Pos.X-3.3)) > 1e-3 || cm.Pos.Z < 1.5 {
		t.Fatalf("diagonal wall slide failed at half-voxel scale: pos=%+v", cm.Pos)
	}

	// Sleeping crate on 0.5-unit terrain wakes and falls when w.Clear() is called.
	phys := physics3d.NewWorld()
	phys.AddVoxelCollider(w.Collider())
	crate := phys.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
		physics3d.Vec3{X: 1.0, Y: 1.5, Z: 0.5},
		1.0,
	))
	for range 180 {
		phys.Step(dt)
	}
	if !crate.Sleeping || math.Abs(float64(crate.Pos.Y-0.7)) > 0.02 {
		t.Fatalf("crate did not settle on 0.5-voxel floor: sleeping=%v pos=%+v", crate.Sleeping, crate.Pos)
	}
	w.Clear()
	if crate.Sleeping {
		t.Fatal("World.Clear did not wake sleeping crate")
	}
	for range 60 {
		phys.Step(dt)
	}
	if crate.Pos.Y >= 0.0 {
		t.Fatalf("crate did not fall after World.Clear: Y=%.3f", crate.Pos.Y)
	}
}

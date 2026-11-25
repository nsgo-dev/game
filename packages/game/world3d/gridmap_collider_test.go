package world3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func buildTestDungeonGridMap() *world3d.GridMap {
	lib := world3d.NewMeshLibrary()
	fullCube := math3d.AABB{
		Min: math3d.Vec3{X: -1, Y: -1, Z: -1},
		Max: math3d.Vec3{X: 1, Y: 1, Z: 1},
	}
	floorTile := lib.Add(world3d.MeshTileDef{
		Name:          "floor",
		MeshID:        1,
		LocalBounds:   fullCube,
		ColliderBoxes: []math3d.AABB{fullCube},
	})
	wallTile := lib.Add(world3d.MeshTileDef{
		Name:          "wall",
		MeshID:        2,
		LocalBounds:   fullCube,
		ColliderBoxes: []math3d.AABB{fullCube},
	})
	// Non-colliding decorative grass tile (no ColliderBoxes).
	decorTile := lib.Add(world3d.MeshTileDef{
		Name:        "grass_decor",
		MeshID:      3,
		LocalBounds: fullCube,
	})

	g := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
	g.Origin = math3d.Vec3{X: -4, Y: 0, Z: -4}

	// 6x1x6 solid floor at Y=0 (36 cells -> merges into 1 static body!).
	g.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 5, Y: 0, Z: 5}, floorTile, 0)
	// North wall (6x2x1 at Z=0, Y=1..2 -> 12 cells).
	g.FillBox(world3d.GridCoord{X: 0, Y: 1, Z: 0}, world3d.GridCoord{X: 5, Y: 2, Z: 0}, wallTile, 0)
	// East wall with a 1-cell doorway at Z=3, Y=1.
	g.FillBox(world3d.GridCoord{X: 5, Y: 1, Z: 1}, world3d.GridCoord{X: 5, Y: 2, Z: 5}, wallTile, 0)
	g.ClearCell(world3d.GridCoord{X: 5, Y: 1, Z: 3})
	// Decorative non-colliding tile on top of the floor at (2,1,2).
	g.SetCell(world3d.GridCoord{X: 2, Y: 1, Z: 2}, decorTile, 0)
	return g
}

func TestGridMapMergedCollidersMatchUnmergedPhysics(t *testing.T) {
	g := buildTestDungeonGridMap()

	rawBoxes := g.ColliderBoxes()
	mergedBoxes := g.MergedColliderBoxes()
	if len(rawBoxes) != 57 { // 36 floor + 12 north wall + 9 east wall (10 - 1 doorway)
		t.Fatalf("raw ColliderBoxes = %d, want 57", len(rawBoxes))
	}
	if len(mergedBoxes) >= len(rawBoxes)/4 {
		t.Fatalf("expected strong collider reduction: merged=%d raw=%d", len(mergedBoxes), len(rawBoxes))
	}

	worldMerged := physics3d.NewWorld()
	worldPerCell := physics3d.NewWorld()

	mergedBodies := g.AddCollidersToWorld(worldMerged, world3d.GridColliderOptions{
		Tag:      "dungeon",
		Friction: 0.6,
	})
	perCellBodies := g.AddCollidersToWorld(worldPerCell, world3d.GridColliderOptions{
		DisableMerge: true,
		Tag:          "dungeon",
		Friction:     0.6,
	})

	if len(mergedBodies) != len(mergedBoxes) {
		t.Fatalf("added merged bodies = %d, want %d", len(mergedBodies), len(mergedBoxes))
	}
	if len(perCellBodies) != len(rawBoxes) {
		t.Fatalf("added per-cell bodies = %d, want %d", len(perCellBodies), len(rawBoxes))
	}

	// 1. Drop identical dynamic boxes and spheres into both worlds (including across cell seams!).
	type dropSpec struct {
		shape physics3d.Shape
		pos   physics3d.Vec3
	}
	drops := []dropSpec{
		// Directly over a cell center (floor top is Y=2.0).
		{physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}), physics3d.Vec3{X: -1, Y: 6, Z: 1}},
		// Directly straddling a cell seam between X=0 and X=2, Z=0 and Z=2!
		{physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: 0, Y: 7, Z: 4}},
		// Over the non-colliding decorative grass tile at cell (2,1,2) -> world center (1, 3, 1): must fall through decor onto floor Y=2.0!
		{physics3d.SphereShape(physics3d.Sphere{Radius: 0.35}), physics3d.Vec3{X: 1, Y: 5, Z: 1}},
		// Over the north wall top (Y=6.0).
		{physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), physics3d.Vec3{X: 0, Y: 9, Z: -3}},
	}

	mergedDyn := make([]*physics3d.Body, len(drops))
	perCellDyn := make([]*physics3d.Body, len(drops))
	for i, d := range drops {
		mergedDyn[i] = worldMerged.AddBody(physics3d.NewDynamicBody(d.shape, d.pos, 1.0))
		perCellDyn[i] = worldPerCell.AddBody(physics3d.NewDynamicBody(d.shape, d.pos, 1.0))
	}

	dt := time.Second / 60
	for range 240 {
		worldMerged.Step(dt)
		worldPerCell.Step(dt)
	}

	wantY := []float32{2.4, 2.5, 2.35, 6.5}
	for i := range drops {
		pM := mergedDyn[i].Pos
		pC := perCellDyn[i].Pos
		if math.Abs(float64(pM.Y-wantY[i])) > 0.05 {
			t.Fatalf("drop[%d] merged Y = %v, want ~%v", i, pM.Y, wantY[i])
		}
		if math.Abs(float64(pM.X-pC.X)) > 0.05 || math.Abs(float64(pM.Y-pC.Y)) > 0.05 || math.Abs(float64(pM.Z-pC.Z)) > 0.05 {
			t.Fatalf("drop[%d] merged pos %v diverged from per-cell pos %v", i, pM, pC)
		}
	}

	// 2. Raycast equivalence across solid floor, walls, and open doorway (cell (5,1,3) -> world X=[6,8], Y=[2,4], Z=[2,4]).
	rays := [][2]physics3d.Vec3{
		{{X: 0, Y: 10, Z: 2}, {X: 0, Y: -5, Z: 2}}, // hit floor at Y=2
		{{X: 0, Y: 3, Z: 3}, {X: 12, Y: 3, Z: 3}},  // pass cleanly through east doorway!
		{{X: 0, Y: 3, Z: 0}, {X: 12, Y: 3, Z: 0}},  // hit east wall at X=6
		{{X: 2, Y: 5, Z: 3}, {X: 12, Y: 5, Z: 3}},  // hit lintel above doorway at X=6
		{{X: 0, Y: 3, Z: 2}, {X: 0, Y: 3, Z: -10}}, // hit north wall at Z=-2
	}
	for i, r := range rays {
		hM, okM := worldMerged.RaycastFirst(r[0], r[1])
		hC, okC := worldPerCell.RaycastFirst(r[0], r[1])
		if okM != okC {
			t.Fatalf("ray[%d] hit mismatch: merged=%v perCell=%v", i, okM, okC)
		}
		if okM {
			if math.Abs(float64(hM.Distance-hC.Distance)) > 1e-3 {
				t.Fatalf("ray[%d] distance mismatch: merged=%v perCell=%v", i, hM.Distance, hC.Distance)
			}
			if math.Abs(float64(hM.Normal.X-hC.Normal.X)) > 1e-3 ||
				math.Abs(float64(hM.Normal.Y-hC.Normal.Y)) > 1e-3 ||
				math.Abs(float64(hM.Normal.Z-hC.Normal.Z)) > 1e-3 {
				t.Fatalf("ray[%d] normal mismatch: merged=%v perCell=%v", i, hM.Normal, hC.Normal)
			}
		}
	}

	// 3. CharacterController walks smoothly across merged floor and stops at east wall (X=6).
	ccM := physics3d.NewCharacterController(physics3d.Vec3{X: -1, Y: 2.2, Z: 0}, 0.35, 1.8)
	ccC := physics3d.NewCharacterController(physics3d.Vec3{X: -1, Y: 2.2, Z: 0}, 0.35, 1.8)
	for range 150 {
		ccM.Move(worldMerged, physics3d.Vec3{X: 4}, dt)
		ccC.Move(worldPerCell, physics3d.Vec3{X: 4}, dt)
	}
	if !ccM.Grounded || !ccM.OnWall {
		t.Fatalf("merged character expected Grounded=true OnWall=true, got (%v, %v) pos=%v", ccM.Grounded, ccM.OnWall, ccM.Pos)
	}
	if math.Abs(float64(ccM.Pos.X-ccC.Pos.X)) > 0.05 || math.Abs(float64(ccM.Pos.Y-ccC.Pos.Y)) > 0.05 {
		t.Fatalf("character merged pos %v diverged from per-cell pos %v", ccM.Pos, ccC.Pos)
	}
}

func TestHeightmapColliderBridge(t *testing.T) {
	hm := &world3d.Heightmap{
		Cols:        4,
		Rows:        4,
		CellSize:    2.0,
		HeightScale: 5.0,
		Origin:      math3d.Vec3{X: -3, Y: 1, Z: -3},
		Heights: []float32{
			0.4, 0.4, 0.4, 0.4,
			0.4, 0.4, 0.4, 0.4,
			0.4, 0.4, 0.4, 0.4,
			0.4, 0.4, 0.4, 0.4,
		},
	}
	hf := hm.Collider()
	w := physics3d.NewWorld()
	w.AddHeightfield(hf)

	// Height is Origin.Y (1) + 0.4*5 (2) = 3.0.
	hit, ok := w.RaycastFirst(physics3d.Vec3{X: 0, Y: 10, Z: 0}, physics3d.Vec3{X: 0, Y: -5, Z: 0})
	if !ok || math.Abs(float64(hit.Point.Y-3.0)) > 1e-3 {
		t.Fatalf("Heightmap.Collider raycast = (%+v, %v), want Y=3.0", hit, ok)
	}
}

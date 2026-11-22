package world3d_test

import (
	"context"
	"image"
	"image/color"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func TestGridOrientations(t *testing.T) {
	seen := make(map[math3d.Mat4]uint8, world3d.GridOrientationCount)
	for i := range uint8(world3d.GridOrientationCount) {
		m := world3d.GridOrientationMat4(i)
		if prev, dup := seen[m]; dup {
			t.Fatalf("orientation %d duplicates orientation %d", i, prev)
		}
		seen[m] = i

		// Proper right-handed rotation: det == +1 and M^T * M == I.
		det := m.Mat3().Det()
		if !math3d.ApproxEqual(det, 1, eps) {
			t.Fatalf("orientation %d det = %v, want +1", i, det)
		}
		prod := m.Transpose().Mul(m)
		if !prod.ApproxEqual(math3d.Mat4Identity(), eps) {
			t.Fatalf("orientation %d is not orthogonal: M^T*M = %v", i, prod)
		}
	}

	// Orientations 0..3 must match 0°, 90°, 180°, 270° rotations around +Y.
	for k := range 4 {
		got := world3d.GridOrientationMat4(uint8(k))
		want := math3d.RotateY(float32(k) * (math.Pi / 2))
		if !got.ApproxEqual(want, eps) {
			t.Fatalf("orientation %d = %v, want RotateY(%d*90deg) = %v", k, got, k, want)
		}
	}

	// Modulo wraparound check.
	if world3d.GridOrientationMat4(25) != world3d.GridOrientationMat4(1) {
		t.Fatalf("expected orientation 25 to wrap to 1")
	}
}

func TestLoadMeshLibraryFromGLTF(t *testing.T) {
	sampleGLTF := []byte(`{
		"asset": { "version": "2.0" },
		"nodes": [
			{ "name": "RootEmpty" },
			{ "name": "FloorTile", "mesh": 0 },
			{ "name": "WallTile", "mesh": 1 },
			{ "mesh": 2 }
		],
		"meshes": [
			{
				"name": "FloorMesh",
				"primitives": [
					{ "attributes": { "POSITION": 0 }, "material": 2 }
				]
			},
			{
				"name": "WallMesh",
				"primitives": [
					{ "attributes": { "POSITION": 1 }, "material": 5 },
					{ "attributes": { "POSITION": 2 }, "material": 6 }
				]
			},
			{
				"name": "PillarMesh",
				"primitives": [
					{ "attributes": { "POSITION": 3 } }
				]
			}
		],
		"accessors": [
			{ "min": [-1, -0.1, -1], "max": [1, 0.1, 1] },
			{ "min": [-1, -1, -0.2], "max": [1, 0, 0.2] },
			{ "min": [-1, 0, -0.2], "max": [1, 1, 0.2] },
			{ "min": [-0.3, -1, -0.3], "max": [0.3, 1, 0.3] }
		]
	}`)

	lib, err := world3d.LoadMeshLibraryFromGLTF(sampleGLTF)
	if err != nil {
		t.Fatalf("LoadMeshLibraryFromGLTF failed: %v", err)
	}
	if lib.Len() != 3 {
		t.Fatalf("lib.Len() = %d, want 3 (RootEmpty skipped)", lib.Len())
	}

	floor, ok := lib.ByName("FloorTile")
	if !ok {
		t.Fatalf("FloorTile not found")
	}
	if floor.MeshID != 0 || floor.MaterialID != 2 {
		t.Fatalf("FloorTile = (mesh %d, mat %d), want (0, 2)", floor.MeshID, floor.MaterialID)
	}
	wantFloorBounds := math3d.AABB{Min: math3d.Vec3{X: -1, Y: -0.1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 0.1, Z: 1}}
	if !floor.LocalBounds.Min.ApproxEqual(wantFloorBounds.Min, eps) || !floor.LocalBounds.Max.ApproxEqual(wantFloorBounds.Max, eps) {
		t.Fatalf("FloorTile bounds = %v, want %v", floor.LocalBounds, wantFloorBounds)
	}
	if len(floor.ColliderBoxes) != 1 {
		t.Fatalf("FloorTile ColliderBoxes len = %d, want 1", len(floor.ColliderBoxes))
	}

	// WallTile unions two primitive POSITION accessors: Y in [-1, 0] U [0, 1] => [-1, 1].
	wall, ok := lib.ByName("WallTile")
	if !ok {
		t.Fatalf("WallTile not found")
	}
	if wall.MeshID != 1 || wall.MaterialID != 5 {
		t.Fatalf("WallTile = (mesh %d, mat %d), want (1, 5)", wall.MeshID, wall.MaterialID)
	}
	if !wall.LocalBounds.Min.ApproxEqual(math3d.Vec3{X: -1, Y: -1, Z: -0.2}, eps) ||
		!wall.LocalBounds.Max.ApproxEqual(math3d.Vec3{X: 1, Y: 1, Z: 0.2}, eps) {
		t.Fatalf("WallTile union bounds = %v, want [-1,-1,-0.2]..[1,1,0.2]", wall.LocalBounds)
	}

	// Unnamed node falls back to mesh name "PillarMesh".
	pillar, ok := lib.ByName("PillarMesh")
	if !ok {
		t.Fatalf("PillarMesh fallback name not found")
	}
	gotByID, ok := lib.Get(pillar.ID)
	if !ok || gotByID.Name != "PillarMesh" || gotByID.MeshID != 2 || gotByID.MaterialID != 0 {
		t.Fatalf("Get(pillar.ID) = (%v, %v), want PillarMesh mesh=2 mat=0", gotByID, ok)
	}

	// Error paths.
	if _, err := world3d.LoadMeshLibraryFromGLTF(nil); err == nil {
		t.Fatalf("expected error on empty glTF")
	}
	if _, err := world3d.LoadMeshLibraryFromGLTF([]byte(`{"asset":{"version":"1.0"}}`)); err == nil {
		t.Fatalf("expected error on glTF 1.0")
	}
	if _, err := world3d.LoadMeshLibraryFromGLTF([]byte(`{"asset":{"version":"2.0"},"nodes":[{"mesh":99}],"meshes":[]}`)); err == nil {
		t.Fatalf("expected error on out-of-range mesh index")
	}
}

func TestGridMapEditsBatchingAndFrustum(t *testing.T) {
	lib := world3d.NewMeshLibrary()
	tileA := lib.Add(world3d.MeshTileDef{
		Name:        "stone",
		MeshID:      10,
		MaterialID:  1,
		LocalBounds: math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}},
	})
	tileB := lib.Add(world3d.MeshTileDef{
		Name:        "wood",
		MeshID:      20,
		MaterialID:  2,
		LocalBounds: math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}},
	})

	gm := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
	gm.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: -3}, world3d.GridCoord{X: 1, Y: 0, Z: -2}, tileA, 0) // 4 stone cells in front (-Z)
	gm.SetCell(world3d.GridCoord{X: 0, Y: 0, Z: -4}, tileB, 1)                                       // 1 wood cell in front (-Z)
	gm.SetCell(world3d.GridCoord{X: 0, Y: 0, Z: 10}, tileA, 2)                                       // 1 stone cell behind camera (+Z)
	gm.SetCell(world3d.GridCoord{X: -2, Y: 1, Z: -2}, tileB, 3)                                      // 1 wood cell to be cleared

	if gm.Count() != 7 {
		t.Fatalf("Count() = %d, want 7", gm.Count())
	}
	gm.ClearCell(world3d.GridCoord{X: -2, Y: 1, Z: -2})
	if gm.Count() != 6 {
		t.Fatalf("Count() after ClearCell = %d, want 6", gm.Count())
	}
	if _, ok := gm.Cell(world3d.GridCoord{X: -2, Y: 1, Z: -2}); ok {
		t.Fatalf("cleared cell still present")
	}
	cell, ok := gm.Cell(world3d.GridCoord{X: 0, Y: 0, Z: -4})
	if !ok || cell.TileID != tileB || cell.Orientation != 1 {
		t.Fatalf("Cell(0,0,-4) = (%v, %v), want tileB orient=1", cell, ok)
	}

	// Unculled batches: 5 stone (Mesh 10, Mat 1) and 1 wood (Mesh 20, Mat 2).
	batches := gm.BuildInstances(nil, nil)
	if len(batches) != 2 {
		t.Fatalf("unculled batch count = %d, want 2", len(batches))
	}
	if batches[0].MeshID != 10 || batches[0].MaterialID != 1 || len(batches[0].Transforms) != 5 {
		t.Fatalf("batch[0] = (mesh %d mat %d len %d), want (10, 1, 5)",
			batches[0].MeshID, batches[0].MaterialID, len(batches[0].Transforms))
	}
	if batches[1].MeshID != 20 || batches[1].MaterialID != 2 || len(batches[1].Transforms) != 1 {
		t.Fatalf("batch[1] = (mesh %d mat %d len %d), want (20, 2, 1)",
			batches[1].MeshID, batches[1].MaterialID, len(batches[1].Transforms))
	}

	// Camera at (1, 1, 2) looking down -Z: cell at Z=10 (world Z=20..22) is behind the camera and must be culled.
	view := math3d.LookAt(math3d.Vec3{X: 1, Y: 1, Z: 2}, math3d.Vec3{X: 1, Y: 1, Z: -10}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(70), 16.0/9.0, 0.5, 50)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))

	batches = gm.BuildInstances(&frustum, batches)
	if len(batches) != 2 {
		t.Fatalf("culled batch count = %d, want 2", len(batches))
	}
	if len(batches[0].Transforms) != 4 || len(batches[1].Transforms) != 1 {
		t.Fatalf("culled counts = (%d, %d), want (4, 1)", len(batches[0].Transforms), len(batches[1].Transforms))
	}

	// Steady-state buffer reuse must incur zero heap allocations.
	allocs := testing.AllocsPerRun(50, func() {
		batches = gm.BuildInstances(&frustum, batches)
	})
	if allocs != 0 {
		t.Fatalf("BuildInstances steady-state allocs = %v, want 0", allocs)
	}
}

func TestGridMapCollisionAndRaycast(t *testing.T) {
	lib := world3d.NewMeshLibrary()
	// Half-width wall along local +X side: X in [0.5, 1.0], Y in [-1, 1], Z in [-1, 1].
	wallTile := lib.Add(world3d.MeshTileDef{
		Name:        "half_wall",
		MeshID:      1,
		MaterialID:  1,
		LocalBounds: math3d.AABB{Min: math3d.Vec3{X: 0.5, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}},
		ColliderBoxes: []math3d.AABB{
			{Min: math3d.Vec3{X: 0.5, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}},
		},
	})
	// Decorative tile with no colliders.
	decorTile := lib.Add(world3d.MeshTileDef{
		Name:        "fern",
		MeshID:      2,
		MaterialID:  2,
		LocalBounds: math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -1, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0, Z: 0.5}},
	})

	gm := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
	// Cell (0, 0, 0) spans [0, 2]^3 with center (1, 1, 1).
	// Orient 0 keeps collider at X in [1.5, 2.0], Y in [0, 2], Z in [0, 2].
	gm.SetCell(world3d.GridCoord{X: 0, Y: 0, Z: 0}, wallTile, 0)
	// Cell (1, 0, 0) spans [2, 4] x [0, 2] x [0, 2] with center (3, 1, 1).
	// Orient 1 (+90° around +Y) rotates local +X to -Z -> collider spans X in [2, 4], Y in [0, 2], Z in [0, 0.5].
	gm.SetCell(world3d.GridCoord{X: 1, Y: 0, Z: 0}, wallTile, 1)
	// Decorative tile at (0, 0, 1) emits no collision boxes.
	gm.SetCell(world3d.GridCoord{X: 0, Y: 0, Z: 1}, decorTile, 0)

	// Query region covering (0,0,0) and (0,0,1) should return only wallTile's collider box.
	boxes := gm.CollisionBoxesIn(math3d.AABB{
		Min: math3d.Vec3{X: 0, Y: 0, Z: 0},
		Max: math3d.Vec3{X: 1.9, Y: 2, Z: 4},
	})
	if len(boxes) != 1 {
		t.Fatalf("CollisionBoxesIn returned %d boxes, want 1", len(boxes))
	}
	wantBox0 := math3d.AABB{Min: math3d.Vec3{X: 1.5, Y: 0, Z: 0}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2}}
	if !boxes[0].Min.ApproxEqual(wantBox0.Min, eps) || !boxes[0].Max.ApproxEqual(wantBox0.Max, eps) {
		t.Fatalf("box[0] = %v, want %v", boxes[0], wantBox0)
	}

	// Query rotated wall in cell (1, 0, 0): must lie at Z in [0, 0.5], not Z in [1.0, 2.0].
	rotBoxes := gm.CollisionBoxesIn(math3d.AABB{
		Min: math3d.Vec3{X: 2.1, Y: 0, Z: 0},
		Max: math3d.Vec3{X: 3.9, Y: 2, Z: 0.4},
	})
	if len(rotBoxes) != 1 {
		t.Fatalf("rotated CollisionBoxesIn returned %d boxes, want 1", len(rotBoxes))
	}
	wantRot := math3d.AABB{Min: math3d.Vec3{X: 2, Y: 0, Z: 0}, Max: math3d.Vec3{X: 4, Y: 2, Z: 0.5}}
	if !rotBoxes[0].Min.ApproxEqual(wantRot.Min, eps) || !rotBoxes[0].Max.ApproxEqual(wantRot.Max, eps) {
		t.Fatalf("rotated box = %v, want %v", rotBoxes[0], wantRot)
	}

	// Raycast along +X from (-1, 1, 1) with non-unit Dir (3, 0, 0) must hit cell (0,0,0) at X=1.5, dist=2.5, normal=(-1,0,0).
	hit, ok := gm.Raycast(math3d.Ray{
		Origin: math3d.Vec3{X: -1, Y: 1, Z: 1},
		Dir:    math3d.Vec3{X: 3, Y: 0, Z: 0},
	}, 20)
	if !ok {
		t.Fatalf("Raycast missed wallTile")
	}
	if hit.Coord != (world3d.GridCoord{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("hit.Coord = %v, want (0,0,0)", hit.Coord)
	}
	if !math3d.ApproxEqual(hit.Distance, 2.5, eps) {
		t.Fatalf("hit.Distance = %v, want 2.5", hit.Distance)
	}
	if !hit.Point.ApproxEqual(math3d.Vec3{X: 1.5, Y: 1, Z: 1}, eps) {
		t.Fatalf("hit.Point = %v, want (1.5,1,1)", hit.Point)
	}
	if !hit.Normal.ApproxEqual(math3d.Vec3{X: -1, Y: 0, Z: 0}, eps) {
		t.Fatalf("hit.Normal = %v, want (-1,0,0)", hit.Normal)
	}

	// Short maxDist must miss.
	if _, ok := gm.Raycast(math3d.Ray{
		Origin: math3d.Vec3{X: -1, Y: 1, Z: 1},
		Dir:    math3d.Vec3{X: 1, Y: 0, Z: 0},
	}, 2.0); ok {
		t.Fatalf("expected Raycast with maxDist=2.0 to miss wall at dist=2.5")
	}
}

func makeSlopedTestHeightmap(cols, rows int, cellSize float32) *world3d.Heightmap {
	heights := make([]float32, cols*rows)
	for r := range rows {
		for c := range cols {
			// Flat low meadow on Z < half, rising steeply past Z >= half.
			if r < rows/2 {
				heights[r*cols+c] = 1.0 + 0.05*float32(math.Sin(float64(c)*0.3))
			} else {
				// Slope ~63.4 deg (rise 2 per 1 cell).
				heights[r*cols+c] = 1.0 + float32(r-rows/2+1)*2.0*cellSize
			}
		}
	}
	return &world3d.Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: 1.0,
		Heights:     heights,
	}
}

func TestFoliageScatterDeterminismSlopeHeightAndDensityMap(t *testing.T) {
	hm := makeSlopedTestHeightmap(33, 33, 1.0) // [0..32] x [0..32]

	// Density map: left half (X < 16) = 1.0, right half (X >= 16) = 0.0.
	img := image.NewGray(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 16 {
			img.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	dm := world3d.LoadDensityMapGray(img)

	layer := world3d.FoliageLayer{
		Name:            "meadow_grass",
		MeshID:          7,
		MaterialID:      3,
		Density:         2.0,
		MinScale:        0.8,
		MaxScale:        1.4,
		AlignToNormal:   1.0,
		MinSlopeDeg:     0,
		MaxSlopeDeg:     30,
		MinHeight:       0,
		MaxHeight:       5,
		DensityMap:      dm,
		BaseMaxDistance: 100,
		Seed:            42,
	}

	fieldA := world3d.Scatter(hm, layer, math3d.AABB{})
	fieldB := world3d.Scatter(hm, layer, math3d.AABB{})

	if fieldA.TotalCount() == 0 {
		t.Fatalf("expected non-zero scattered instances")
	}
	if fieldA.TotalCount() != fieldB.TotalCount() {
		t.Fatalf("non-deterministic scatter counts: %d vs %d", fieldA.TotalCount(), fieldB.TotalCount())
	}

	// Collect all instances using an enclosing orthographic frustum.
	view := math3d.LookAt(math3d.Vec3{X: 16, Y: 50, Z: 16}, math3d.Vec3{X: 16, Y: 0, Z: 16}, math3d.Vec3{X: 0, Y: 0, Z: -1})
	proj := math3d.Ortho(-40, 40, -40, 40, 1, 100)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))
	allProf := world3d.QualityProfile{DensityScale: 1, MaxDistance: 0, FadeStartRatio: 1}

	visA := fieldA.CollectVisible(math3d.Vec3{X: 16, Y: 1, Z: 16}, frustum, allProf, nil)
	visB := fieldB.CollectVisible(math3d.Vec3{X: 16, Y: 1, Z: 16}, frustum, allProf, nil)
	if len(visA) != fieldA.TotalCount() || len(visA) != len(visB) {
		t.Fatalf("collected counts = (%d, %d), want %d", len(visA), len(visB), fieldA.TotalCount())
	}

	minNormalY := float32(math.Cos(float64(math3d.DegToRad(30)))) - 1e-3
	for i, inst := range visA {
		if !inst.Transform.ApproxEqual(visB[i].Transform, eps) {
			t.Fatalf("instance %d transform differs between deterministic runs", i)
		}
		pos, _, scale, ok := inst.Transform.Decompose()
		if !ok {
			t.Fatalf("instance %d matrix failed Decompose", i)
		}
		// Must lie on left half where DensityMap > 0 (allowing 1 texel bilinear transition around X=16).
		if pos.X > 17.1 {
			t.Fatalf("instance %d placed in zero-density region at X=%v", i, pos.X)
		}
		// Must match exact terrain height and height bounds.
		wantY, inBounds := hm.HeightAt(pos.X, pos.Z)
		if !inBounds || !math3d.ApproxEqual(pos.Y, wantY, 1e-3) {
			t.Fatalf("instance %d Y=%v disagrees with HeightAt=%v", i, pos.Y, wantY)
		}
		if pos.Y < layer.MinHeight || pos.Y > layer.MaxHeight {
			t.Fatalf("instance %d height %v outside [%v, %v]", i, pos.Y, layer.MinHeight, layer.MaxHeight)
		}
		// Must satisfy slope limit and normal alignment.
		n := hm.NormalAt(pos.X, pos.Z)
		if n.Y < minNormalY {
			t.Fatalf("instance %d placed on steep slope (normal.Y=%v < %v)", i, n.Y, minNormalY)
		}
		upAxis := inst.Transform.Col(1).XYZ().Norm()
		if !upAxis.ApproxEqual(n, 1e-3) {
			t.Fatalf("instance %d local +Y %v not aligned with terrain normal %v", i, upAxis, n)
		}
		// Scale within [MinScale, MaxScale].
		if scale.X < layer.MinScale-eps || scale.X > layer.MaxScale+eps {
			t.Fatalf("instance %d scale %v outside [%v, %v]", i, scale.X, layer.MinScale, layer.MaxScale)
		}
	}
}

func TestFoliageQualityLevelsFadeAndFrustum(t *testing.T) {
	// Flat 100m x 100m heightmap centered around (0, 0).
	hm := &world3d.Heightmap{
		Cols:        11,
		Rows:        11,
		CellSize:    10,
		HeightScale: 1,
		Origin:      math3d.Vec3{X: -50, Y: 0, Z: -50},
		Heights:     make([]float32, 121),
	}
	field := world3d.Scatter(hm, world3d.FoliageLayer{
		Name:            "grass",
		MeshID:          1,
		MaterialID:      1,
		Density:         0.5, // ~5,000 instances
		MinScale:        0.5,
		MaxScale:        1.0,
		BaseMaxDistance: 200,
		Seed:            99,
	}, math3d.AABB{})

	camPos := math3d.Vec3{X: 0, Y: 2, Z: 45}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 0, Y: 0, Z: -50}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 250)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))

	// Quality levels should monotonically increase visible count.
	levels := []world3d.QualityLevel{
		world3d.QualityLow,
		world3d.QualityMedium,
		world3d.QualityHigh,
		world3d.QualityUltra,
	}
	prevCount := 0
	var out []world3d.FoliageInstance
	for _, lvl := range levels {
		prof := world3d.DefaultQualityProfile(lvl)
		out = field.CollectVisible(camPos, frustum, prof, out)
		if len(out) <= prevCount {
			t.Fatalf("quality level %d produced %d instances, expected > %d", lvl, len(out), prevCount)
		}
		prevCount = len(out)
	}

	// Verify smooth fade curve and strict MaxDistance cutoff.
	fadeProf := world3d.QualityProfile{
		DensityScale:   1.0,
		MaxDistance:    60,
		FadeStartRatio: 0.5, // fade band: [30, 60]
	}
	out = field.CollectVisible(camPos, frustum, fadeProf, out)
	if len(out) == 0 {
		t.Fatalf("expected visible instances with fade profile")
	}

	sawFullOpaque := false
	sawFading := false
	for _, inst := range out {
		pos := math3d.Vec3{X: inst.Transform[12], Y: inst.Transform[13], Z: inst.Transform[14]}
		d := pos.Dist(camPos)
		if d >= fadeProf.MaxDistance {
			t.Fatalf("instance at dist %v exceeded MaxDistance %v", d, fadeProf.MaxDistance)
		}
		if d <= 29.9 {
			sawFullOpaque = true
			if inst.Fade != 1 {
				t.Fatalf("instance at dist %v inside fade start has Fade=%v, want 1", d, inst.Fade)
			}
		} else if d >= 30.1 && d <= 59.9 {
			sawFading = true
			if inst.Fade <= 0 || inst.Fade >= 1 {
				t.Fatalf("instance at dist %v in fade band has Fade=%v, want in (0,1)", d, inst.Fade)
			}
		}
		// Must not be behind the camera (looking toward -Z from Z=45).
		if pos.Z > camPos.Z+1.1 {
			t.Fatalf("instance behind camera at Z=%v was not frustum-culled", pos.Z)
		}
	}
	if !sawFullOpaque || !sawFading {
		t.Fatalf("fade test coverage incomplete: sawFullOpaque=%v sawFading=%v", sawFullOpaque, sawFading)
	}

	// Steady-state CollectVisible must allocate zero heap memory when reusing out.
	allocs := testing.AllocsPerRun(50, func() {
		out = field.CollectVisible(camPos, frustum, fadeProf, out)
	})
	if allocs != 0 {
		t.Fatalf("CollectVisible steady-state allocs = %v, want 0", allocs)
	}
}

func BenchmarkFoliage100kCull(b *testing.B) {
	// 200m x 200m terrain with Density 2.5/m^2 => 100,000 grass instances.
	hm := &world3d.Heightmap{
		Cols:        21,
		Rows:        21,
		CellSize:    10,
		HeightScale: 1,
		Origin:      math3d.Vec3{X: -100, Y: 0, Z: -100},
		Heights:     make([]float32, 21*21),
	}
	field := world3d.Scatter(hm, world3d.FoliageLayer{
		Name:            "grass_100k",
		MeshID:          1,
		MaterialID:      1,
		Density:         2.5,
		MinScale:        0.7,
		MaxScale:        1.3,
		BaseMaxDistance: 80,
		Seed:            12345,
	}, math3d.AABB{})
	if field.TotalCount() < 95_000 {
		b.Fatalf("expected ~100,000 instances, got %d", field.TotalCount())
	}

	camPos := math3d.Vec3{X: 0, Y: 2, Z: 0}
	view := math3d.LookAt(camPos, math3d.Vec3{X: 0, Y: 1, Z: -50}, math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 150)
	frustum := math3d.FrustumFromMat4(proj.Mul(view))
	profile := world3d.QualityProfile{
		DensityScale:   1.0,
		MaxDistance:    80,
		FadeStartRatio: 0.75,
	}

	out := make([]world3d.FoliageInstance, 0, field.TotalCount())
	// Warm up once to verify non-empty visible set.
	out = field.CollectVisible(camPos, frustum, profile, out)
	if len(out) == 0 {
		b.Fatalf("expected visible grass instances in frustum")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out = field.CollectVisible(camPos, frustum, profile, out)
	}
	b.ReportMetric(float64(field.TotalCount()), "total_inst")
	b.ReportMetric(float64(len(out)), "visible_inst")
}

func TestFloatingOriginWalk20Km(t *testing.T) {
	const (
		threshold = 512.0
		stepDist  = 0.1
		totalDist = 20_000.0
	)
	steps := int(totalDist / stepDist) // 200,000 steps across 20 km

	fo := world3d.NewFloatingOrigin(threshold)
	playerLocal := math3d.Vec3Zero()

	// Walk diagonally in XZ: unit vector (0.8, 0, -0.6).
	dir := math3d.Vec3{X: 0.8, Y: 0.0, Z: -0.6}
	stepVec := dir.Scale(stepDist)

	// Static world landmark near the 20 km destination, plus a nearby active prop
	// tracked across rebases via RebaseMat4.
	destLandmarkWorld := world3d.WorldPos64{X: 16_003.5, Y: 1.25, Z: -12_004.0}
	activePropLocal := math3d.Vec3{X: 3.5, Y: 1.25, Z: -4.0}
	activePropWorld := fo.LocalToWorld(activePropLocal)
	activePropMat := math3d.Translate(activePropLocal)

	rebaseCount := 0
	maxStepJitter := 0.0

	for i := range steps {
		prevWorld := fo.LocalToWorld(playerLocal)
		playerLocal = playerLocal.Add(stepVec)

		if rebased, shift := fo.Update(playerLocal); rebased {
			rebaseCount++
			playerLocal = playerLocal.Add(shift)
			activePropLocal = activePropLocal.Add(shift)
			activePropMat = world3d.RebaseMat4(activePropMat, shift)
			if !playerLocal.ApproxEqual(math3d.Vec3Zero(), 1e-6) {
				t.Fatalf("step %d: after rebase playerLocal = %v, want (0,0,0)", i, playerLocal)
			}
		}

		if localDist := float64(playerLocal.Len()); localDist > threshold+1e-3 {
			t.Fatalf("step %d: local distance %v exceeded threshold %v", i, localDist, threshold)
		}

		curWorld := fo.LocalToWorld(playerLocal)
		stepDelta := curWorld.Dist(prevWorld)
		jitter := math.Abs(stepDelta - stepDist)
		if jitter > maxStepJitter {
			maxStepJitter = jitter
		}
		if jitter >= 1e-4 {
			t.Fatalf("step %d (dist=%.1fm): step jitter = %g >= 1e-4m", i, float64(i)*stepDist, jitter)
		}
	}

	if rebaseCount < 35 {
		t.Fatalf("expected >= 35 rebases over 20km with 512m threshold, got %d", rebaseCount)
	}

	finalWorld := fo.LocalToWorld(playerLocal)
	totalWalked := finalWorld.Dist(world3d.WorldPos64{})
	if math.Abs(totalWalked-totalDist) > 1.0 {
		t.Fatalf("total walked = %.3fm, want ~%.1fm", totalWalked, totalDist)
	}

	// Verify WorldToLocal / LocalToWorld round-trip at 20 km preserves sub-millimeter accuracy.
	localLandmark := fo.WorldToLocal(destLandmarkWorld)
	roundTrip := fo.LocalToWorld(localLandmark)
	if errDist := roundTrip.Dist(destLandmarkWorld); errDist > 1e-4 {
		t.Fatalf("20km WorldToLocal/LocalToWorld round-trip error = %g m, want <= 1e-4 m", errDist)
	}

	// Verify RebaseMat4 matches shifted local transform and preserves world position.
	matPos := math3d.Vec3{X: activePropMat[12], Y: activePropMat[13], Z: activePropMat[14]}
	if !matPos.ApproxEqual(activePropLocal, 1e-3) {
		t.Fatalf("rebased activePropMat pos = %v, want %v", matPos, activePropLocal)
	}
	if errDist := fo.LocalToWorld(activePropLocal).Dist(activePropWorld); errDist > 1e-2 {
		t.Fatalf("active prop world drift after %d rebases = %g m", rebaseCount, errDist)
	}
}

func TestChunkStreamerSyncBudgetAndUnload(t *testing.T) {
	var unloaded []world3d.ChunkCoord
	s := world3d.NewChunkStreamer(world3d.ChunkStreamerConfig[string]{
		ChunkSize:               64,
		LoadRadius:              1, // 3x3 = 9 chunks
		UnloadRadius:            2, // hysteresis band at radius 2
		MaxIntegrationsPerFrame: 3,
		Workers:                 0, // synchronous deterministic mode
		Loader: func(_ context.Context, c world3d.ChunkCoord) (string, error) {
			return "loaded", nil
		},
		OnUnload: func(c world3d.ChunkCoord, _ string) {
			unloaded = append(unloaded, c)
		},
	})
	defer s.Close()

	origin := world3d.WorldPos64{X: 10, Y: 0, Z: 10} // chunk (0, 0)
	s.Update(origin)
	if got := len(s.LoadedChunks()); got != 3 {
		t.Fatalf("frame 1 loaded = %d, want 3 (MaxIntegrationsPerFrame=3)", got)
	}
	// Center chunk (0, 0) must load on frame 1 (closest first).
	if v, ok := s.Get(world3d.ChunkCoord{X: 0, Z: 0}); !ok || v != "loaded" {
		t.Fatalf("center chunk (0,0) not loaded on frame 1")
	}

	s.Update(origin)
	if got := len(s.LoadedChunks()); got != 6 {
		t.Fatalf("frame 2 loaded = %d, want 6", got)
	}
	s.Update(origin)
	if got := len(s.LoadedChunks()); got != 9 {
		t.Fatalf("frame 3 loaded = %d, want 9", got)
	}

	// Move 1 chunk east to (1, 0): old chunks at X=-1 are at distance 2 == UnloadRadius, so none unload yet!
	s.Update(world3d.WorldPos64{X: 70, Y: 0, Z: 10})
	if len(unloaded) != 0 {
		t.Fatalf("hysteresis violated: unloaded %d chunks at distance == UnloadRadius", len(unloaded))
	}

	// Move far east to chunk (10, 0): all previous chunks (> UnloadRadius) must unload.
	s.Update(world3d.WorldPos64{X: 650, Y: 0, Z: 10})
	if len(unloaded) < 9 {
		t.Fatalf("expected >= 9 unloaded chunks after teleport, got %d", len(unloaded))
	}
	if _, ok := s.Get(world3d.ChunkCoord{X: 0, Z: 0}); ok {
		t.Fatalf("old chunk (0,0) still loaded after moving to (10,0)")
	}
}

func TestChunkStreamerAsyncRaceBudgetAndClose(t *testing.T) {
	var (
		mu             sync.Mutex
		loadedTotal    int
		unloadedTotal  int
		canceledLoads  atomic.Int32
		maxInOneUpdate int
		thisUpdate     int
	)

	s := world3d.NewChunkStreamer(world3d.ChunkStreamerConfig[int]{
		ChunkSize:               32,
		LoadRadius:              2, // 5x5 = 25 chunks
		UnloadRadius:            3,
		MaxIntegrationsPerFrame: 4,
		Workers:                 4,
		Loader: func(ctx context.Context, c world3d.ChunkCoord) (int, error) {
			timer := time.NewTimer(2 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				canceledLoads.Add(1)
				return 0, ctx.Err()
			case <-timer.C:
				return int(c.X*1000 + c.Z), nil
			}
		},
		OnLoad: func(_ world3d.ChunkCoord, _ int) {
			mu.Lock()
			loadedTotal++
			thisUpdate++
			mu.Unlock()
		},
		OnUnload: func(_ world3d.ChunkCoord, _ int) {
			mu.Lock()
			unloadedTotal++
			mu.Unlock()
		},
	})

	pos := world3d.WorldPos64{X: 16, Y: 0, Z: 16}
	deadline := time.Now().Add(3 * time.Second)
	for len(s.LoadedChunks()) < 25 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for 25 async chunks to load (got %d, pending=%d, ready=%d)",
				len(s.LoadedChunks()), s.PendingCount(), s.ReadyCount())
		}
		mu.Lock()
		thisUpdate = 0
		mu.Unlock()

		s.Update(pos)

		mu.Lock()
		if thisUpdate > maxInOneUpdate {
			maxInOneUpdate = thisUpdate
		}
		mu.Unlock()
		time.Sleep(3 * time.Millisecond)
	}

	if maxInOneUpdate > 4 {
		t.Fatalf("integrated %d chunks in a single Update, exceeding MaxIntegrationsPerFrame=4", maxInOneUpdate)
	}

	// Teleport far away while rapidly advancing to trigger unloads and stale load cancellations.
	s.Update(world3d.WorldPos64{X: 1000, Y: 0, Z: 1000})
	s.Update(world3d.WorldPos64{X: 2000, Y: 0, Z: 2000})
	time.Sleep(5 * time.Millisecond)

	// Clean Close() cancels remaining workers and unloads any remaining loaded chunks; idempotent.
	s.Close()
	s.Close()

	if len(s.LoadedChunks()) != 0 {
		t.Fatalf("expected 0 loaded chunks after Close, got %d", len(s.LoadedChunks()))
	}
	mu.Lock()
	gotLoaded, gotUnloaded := loadedTotal, unloadedTotal
	mu.Unlock()
	if gotLoaded != gotUnloaded {
		t.Fatalf("load/unload mismatch after Close: loaded=%d unloaded=%d", gotLoaded, gotUnloaded)
	}
}

func TestGridMapBoundsFollowLibraryRevision(t *testing.T) {
	// #546: replacing a tile definition must invalidate cached chunk bounds.
	lib := world3d.NewMeshLibrary()
	id := lib.Add(world3d.MeshTileDef{LocalBounds: math3d.AABB{
		Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
	}})
	g := world3d.NewGridMap(math3d.Vec3{X: 1, Y: 1, Z: 1}, lib)
	g.SetCell(world3d.GridCoord{}, id, 0)
	if got := g.Bounds().Max.X; got > 1 {
		t.Fatalf("bounds before replacement = %v, want <= 1", got)
	}
	lib.Add(world3d.MeshTileDef{ID: id, LocalBounds: math3d.AABB{
		Min: math3d.Vec3{X: -2, Y: -2, Z: -2}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2},
	}})
	if got := g.Bounds().Max.X; got < 2 {
		t.Fatalf("bounds after replacement = %v, want the new ±2 bounds", got)
	}
}

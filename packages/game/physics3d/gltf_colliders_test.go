package physics3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func buildBoxPrimitive(minV, maxV math3d.Vec3, indexed bool) gltf.Primitive {
	// 8 corners of an AABB, 12 CCW outward triangles.
	c := [8]math3d.Vec3{
		{X: minV.X, Y: minV.Y, Z: minV.Z}, // 0
		{X: maxV.X, Y: minV.Y, Z: minV.Z}, // 1
		{X: maxV.X, Y: maxV.Y, Z: minV.Z}, // 2
		{X: minV.X, Y: maxV.Y, Z: minV.Z}, // 3
		{X: minV.X, Y: minV.Y, Z: maxV.Z}, // 4
		{X: maxV.X, Y: minV.Y, Z: maxV.Z}, // 5
		{X: maxV.X, Y: maxV.Y, Z: maxV.Z}, // 6
		{X: minV.X, Y: maxV.Y, Z: maxV.Z}, // 7
	}
	idx := []uint32{
		// +Y top (CCW from +Y)
		3, 7, 6, 3, 6, 2,
		// -Y bottom
		0, 1, 5, 0, 5, 4,
		// +Z front
		4, 5, 6, 4, 6, 7,
		// -Z back
		1, 0, 3, 1, 3, 2,
		// +X right
		5, 1, 2, 5, 2, 6,
		// -X left
		0, 4, 7, 0, 7, 3,
	}
	bounds := math3d.AABB{Min: minV, Max: maxV}
	if indexed {
		return gltf.Primitive{
			Mode:      gltf.ModeTriangles,
			Material:  -1,
			Positions: c[:],
			Indices:   idx,
			Bounds:    bounds,
		}
	}
	verts := make([]math3d.Vec3, len(idx))
	for i, k := range idx {
		verts[i] = c[k]
	}
	return gltf.Primitive{
		Mode:      gltf.ModeTriangles,
		Material:  -1,
		Positions: verts,
		Bounds:    bounds,
	}
}

// buildGLTFJSONWithBoxMesh builds a valid embedded-base64 glTF 2.0 JSON document
// containing a single unit box mesh ([-0.5, 0.5]^3) and caller-supplied nodes.
func buildGLTFJSONWithBoxMesh(t *testing.T, nodes []map[string]any, roots []int) []byte {
	t.Helper()
	prim := buildBoxPrimitive(math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, true)

	var bin bytes.Buffer
	posOff := bin.Len()
	for _, p := range prim.Positions {
		_ = binary.Write(&bin, binary.LittleEndian, p.X)
		_ = binary.Write(&bin, binary.LittleEndian, p.Y)
		_ = binary.Write(&bin, binary.LittleEndian, p.Z)
	}
	posLen := bin.Len() - posOff

	idxOff := bin.Len()
	for _, k := range prim.Indices {
		_ = binary.Write(&bin, binary.LittleEndian, uint16(k))
	}
	idxLen := bin.Len() - idxOff

	docMap := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"scene": 0,
		"scenes": []map[string]any{
			{"name": "Level", "nodes": roots},
		},
		"nodes": nodes,
		"meshes": []map[string]any{
			{
				"name": "UnitCube",
				"primitives": []map[string]any{
					{
						"attributes": map[string]any{"POSITION": 0},
						"indices":    1,
						"mode":       4,
					},
				},
			},
		},
		"accessors": []map[string]any{
			{
				"bufferView":    0,
				"componentType": 5126,
				"count":         len(prim.Positions),
				"type":          "VEC3",
				"min":           []float32{-0.5, -0.5, -0.5},
				"max":           []float32{0.5, 0.5, 0.5},
			},
			{
				"bufferView":    1,
				"componentType": 5123,
				"count":         len(prim.Indices),
				"type":          "SCALAR",
			},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			{"buffer": 0, "byteOffset": idxOff, "byteLength": idxLen},
		},
		"buffers": []map[string]any{
			{
				"byteLength": bin.Len(),
				"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes()),
			},
		},
	}
	data, err := json.Marshal(docMap)
	if err != nil {
		t.Fatalf("marshal glTF: %v", err)
	}
	return data
}

func TestNewTriMeshFromGLTFPrimitiveModesAndTransforms(t *testing.T) {
	// 1. Indexed vs non-indexed box primitives under parent*child transform.
	indexedPrim := buildBoxPrimitive(math3d.Vec3{X: -1, Y: -1, Z: -1}, math3d.Vec3{X: 1, Y: 1, Z: 1}, true)
	nonIndexedPrim := buildBoxPrimitive(math3d.Vec3{X: -1, Y: -1, Z: -1}, math3d.Vec3{X: 1, Y: 1, Z: 1}, false)

	world := math3d.TRS(
		math3d.Vec3{X: 10, Y: 2, Z: -4},
		math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)),
		math3d.Vec3{X: 2, Y: 0.5, Z: 3},
	)

	tmIdx := physics3d.NewTriMeshFromGLTFPrimitive(indexedPrim, world)
	tmNonIdx := physics3d.NewTriMeshFromGLTFPrimitive(nonIndexedPrim, world)
	if len(tmIdx.Triangles) != 12 || len(tmNonIdx.Triangles) != 12 {
		t.Fatalf("tri counts = (%d, %d), want (12, 12)", len(tmIdx.Triangles), len(tmNonIdx.Triangles))
	}

	// After 90 deg Y rot, local X(+-2) -> Z(+-2) and local Z(+-3) -> X(+-3), Y(+-0.5).
	ray := physics3d.Ray{Origin: physics3d.Vec3{X: 10, Y: 5, Z: -4}, Dir: physics3d.Vec3{Y: -1}, MaxDist: 10}
	h1, ok1 := tmIdx.Raycast(ray)
	h2, ok2 := tmNonIdx.Raycast(ray)
	if !ok1 || !ok2 {
		t.Fatalf("raycast failed: ok1=%v ok2=%v", ok1, ok2)
	}
	if !approx(h1.Point.Y, 2.5, 1e-3) || !approx(h2.Point.Y, 2.5, 1e-3) {
		t.Fatalf("hit Y = (%v, %v), want 2.5", h1.Point.Y, h2.Point.Y)
	}

	// 2. Triangle strip & triangle fan.
	strip := gltf.Primitive{
		Mode: gltf.ModeTriangleStrip,
		Positions: []math3d.Vec3{
			{X: 0, Y: 0, Z: 0},
			{X: 0, Y: 0, Z: 2},
			{X: 2, Y: 0, Z: 0},
			{X: 2, Y: 0, Z: 2},
		},
	}
	tmStrip := physics3d.NewTriMeshFromGLTFPrimitive(strip, math3d.Mat4Identity())
	if len(tmStrip.Triangles) != 2 {
		t.Fatalf("strip triangles = %d, want 2", len(tmStrip.Triangles))
	}
	// Both strip triangles should face +Y.
	for i, tri := range tmStrip.Triangles {
		if tri.Normal().Y < 0.99 {
			t.Fatalf("strip tri[%d] normal = %v, want +Y", i, tri.Normal())
		}
	}

	fan := gltf.Primitive{
		Mode: gltf.ModeTriangleFan,
		Positions: []math3d.Vec3{
			{X: 0, Y: 0, Z: 0},
			{X: 0, Y: 0, Z: 2},
			{X: 2, Y: 0, Z: 2},
			{X: 2, Y: 0, Z: 0},
		},
	}
	tmFan := physics3d.NewTriMeshFromGLTFPrimitive(fan, math3d.Mat4Identity())
	if len(tmFan.Triangles) != 2 {
		t.Fatalf("fan triangles = %d, want 2", len(tmFan.Triangles))
	}

	// 3. Mirrored (negative determinant) transform flips winding so outward normals stay +Y.
	mirror := math3d.Scale(math3d.Vec3{X: -1, Y: 1, Z: 1})
	tmMirror := physics3d.NewTriMeshFromGLTFPrimitive(strip, mirror)
	for i, tri := range tmMirror.Triangles {
		if tri.Normal().Y < 0.99 {
			t.Fatalf("mirrored tri[%d] normal = %v, want +Y", i, tri.Normal())
		}
	}

	// 4. Non-triangle primitives are ignored.
	lines := gltf.Primitive{
		Mode:      gltf.ModeLines,
		Positions: []math3d.Vec3{{X: 0}, {X: 1}, {X: 2}, {X: 3}},
	}
	if tmLines := physics3d.NewTriMeshFromGLTFPrimitive(lines, math3d.Mat4Identity()); len(tmLines.Triangles) != 0 {
		t.Fatalf("expected 0 triangles for ModeLines, got %d", len(tmLines.Triangles))
	}
}

func TestGLTFLevelColOnlyWalkableAndExcludedFromDrawables(t *testing.T) {
	// Real glTF 2.0 level loaded via gltf.Load:
	// - Node 0: LevelRoot translated by (0, 1, 0)
	//   - Node 1: FloorVisual (visual-only mesh, drawn, NOT a collider)
	//   - Node 2: Floor-colonly (20x0.5x20 slab at local Y=-0.25 -> world top Y=1.0, NOT drawn, walkable TriMesh)
	//   - Node 3: Wall-boxcolonly (1x4x10 wall at local X=5, Y=2 -> world X=[4.5, 5.5], Y=[1, 5], NOT drawn, Box body)
	//   - Node 4: DecorativeCrate (visual-only, drawn)
	//   - Node 5: Ramp-col (drawn AND TriMesh collider)
	nodes := []map[string]any{
		{
			"name":        "LevelRoot",
			"translation": []float32{0, 1, 0},
			"children":    []int{1, 2, 3, 4, 5},
		},
		{
			"name":        "FloorVisual",
			"mesh":        0,
			"translation": []float32{0, -0.25, 0},
			"scale":       []float32{20, 0.5, 20},
		},
		{
			"name":        "Floor-colonly",
			"mesh":        0,
			"translation": []float32{0, -0.25, 0},
			"scale":       []float32{20, 0.5, 20},
		},
		{
			"name":        "Wall-boxcolonly",
			"mesh":        0,
			"translation": []float32{5, 2, 0},
			"scale":       []float32{1, 4, 10},
		},
		{
			"name":        "DecorativeCrate",
			"mesh":        0,
			"translation": []float32{-4, 0.5, 0},
		},
		{
			"name":        "Ramp-col",
			"mesh":        0,
			"translation": []float32{-6, 0.5, 0},
			"scale":       []float32{2, 1, 4},
		},
	}

	raw := buildGLTFJSONWithBoxMesh(t, nodes, []int{0})
	doc, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load failed: %v", err)
	}

	// Verify -colonly nodes are excluded from drawn renderables.
	drawable := doc.DrawableNodes()
	drawableNames := make([]string, len(drawable))
	for i, ni := range drawable {
		drawableNames[i] = doc.Nodes[ni].Name
		if doc.Nodes[ni].Name == "Floor-colonly" || doc.Nodes[ni].Name == "Wall-boxcolonly" {
			t.Fatalf("collider-only node %q appeared in DrawableNodes", doc.Nodes[ni].Name)
		}
	}
	if len(drawable) != 3 {
		t.Fatalf("DrawableNodes = %v (len %d), want 3 (FloorVisual, DecorativeCrate, Ramp-col)", drawableNames, len(drawable))
	}

	world := physics3d.NewWorld()
	col, err := world.AddGLTFColliders(doc, physics3d.GLTFColliderOptions{})
	if err != nil {
		t.Fatalf("AddGLTFColliders failed: %v", err)
	}
	if len(col.TriMeshes) != 2 {
		t.Fatalf("TriMeshes = %d, want 2 (Floor-colonly + Ramp-col)", len(col.TriMeshes))
	}
	if len(col.Bodies) != 1 {
		t.Fatalf("Bodies = %d, want 1 (Wall-boxcolonly)", len(col.Bodies))
	}

	// CharacterController drops onto Floor-colonly (world Y=1.0), walks +X toward Wall-boxcolonly (X=4.5),
	// stays grounded at Y=1.0, and stops cleanly against the wall without passing through!
	cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 1.5, Z: 0}, 0.35, 1.8)
	dt := time.Second / 60
	for range 120 {
		cc.Move(world, physics3d.Vec3{X: 4.0}, dt)
	}

	if !cc.Grounded {
		t.Fatalf("expected character to be grounded on Floor-colonly, pos=%v", cc.Pos)
	}
	if !approx(cc.Pos.Y, 1.0, 0.03) {
		t.Fatalf("character Y = %v, want ~1.0 (Floor-colonly top)", cc.Pos.Y)
	}
	// Wall-boxcolonly left face is at X=4.5, character radius is 0.35 -> stops around X <= 4.16.
	if cc.Pos.X < 3.8 || cc.Pos.X > 4.2 {
		t.Fatalf("character X = %v, want stopped against Wall-boxcolonly near 4.15", cc.Pos.X)
	}
	if !cc.OnWall {
		t.Fatalf("expected character OnWall=true against Wall-boxcolonly")
	}

	// RemoveFromWorld cleans up every body and trimesh.
	col.RemoveFromWorld(world)
	if len(world.Bodies()) != 0 || len(world.TriMeshes()) != 0 {
		t.Fatalf("after RemoveFromWorld: bodies=%d trimeshes=%d", len(world.Bodies()), len(world.TriMeshes()))
	}
}

func TestGLTFAllColliderShapesWithTransformsAndExtras(t *testing.T) {
	q45Y := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(45))
	q90Z := math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, math3d.DegToRad(90))

	nodes := []map[string]any{
		{
			// Parent translates everything by (2, 0, 0).
			"name":        "Parent",
			"translation": []float32{2, 0, 0},
			"children":    []int{1, 2, 3, 4, 5, 6},
		},
		{
			// 1: Rotated box collider (-boxcolonly) -> exact OBB at (2, 0, 0) rotated 45 deg around Y, half-extents (2, 0.5, 0.5).
			"name":     "Beam-boxcolonly",
			"mesh":     0,
			"rotation": []float32{q45Y.X, q45Y.Y, q45Y.Z, q45Y.W},
			"scale":    []float32{4, 1, 1},
		},
		{
			// 2: Scaled sphere collider (-spherecolonly) -> center (2, 5, 0), local radius 0.5 * maxScale(3) = 1.5.
			"name":        "Orb-spherecolonly",
			"mesh":        0,
			"translation": []float32{0, 5, 0},
			"scale":       []float32{2, 3, 1},
		},
		{
			// 3: Rotated capsule collider (-capsulecolonly) -> tall box scaled (1, 4, 1) rotated 90 deg Z -> horizontal along X!
			"name":        "Log-capsulecolonly",
			"mesh":        0,
			"translation": []float32{0, 0, 6},
			"rotation":    []float32{q90Z.X, q90Z.Y, q90Z.Z, q90Z.W},
			"scale":       []float32{1, 4, 1},
		},
		{
			// 4: Convex collider (-convcolonly) -> TriMesh fallback at (10, 0, 0).
			"name":        "Hull-convcolonly",
			"mesh":        0,
			"translation": []float32{8, 0, 0},
			"scale":       []float32{2, 2, 2},
		},
		{
			// 5: Extras-defined meshless box trigger with explicit center, halfExtents, tag, layer.
			"name":        "GoalEmpty",
			"translation": []float32{0, 0, -5},
			"extras": map[string]any{
				"collider": map[string]any{
					"shape":       "box",
					"trigger":     true,
					"tag":         "goal",
					"center":      []float32{0, 1, 0},
					"halfExtents": []float32{1.5, 1.0, 1.5},
					"layer":       2,
				},
			},
		},
		{
			// 6: Extras-defined mesh node with draw:false and capsule dimensions.
			"name":        "HiddenCapsule",
			"mesh":        0,
			"translation": []float32{-4, 2, 0},
			"extras": map[string]any{
				"collider": map[string]any{
					"shape":  "capsule",
					"draw":   false,
					"radius": 0.6,
					"height": 2.4,
				},
			},
		},
	}

	raw := buildGLTFJSONWithBoxMesh(t, nodes, []int{0})
	doc, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	// None of these 6 nodes are drawable.
	if got := doc.DrawableNodes(); len(got) != 0 {
		t.Fatalf("expected 0 drawable nodes, got %v", got)
	}

	world := physics3d.NewWorld()
	col, err := world.AddGLTFColliders(doc, physics3d.GLTFColliderOptions{TagPrefix: "lvl:"})
	if err != nil {
		t.Fatalf("AddGLTFColliders: %v", err)
	}
	if len(col.Bodies) != 5 || len(col.TriMeshes) != 1 {
		t.Fatalf("got %d bodies, %d trimeshes; want 5 bodies, 1 trimesh", len(col.Bodies), len(col.TriMeshes))
	}

	// Check 1: OBB rotated 45 deg around Y at (2, 0, 0) with half-extents (2, 0.5, 0.5).
	// A point along the rotated beam axis at distance 1.8 from (2,0,0) MUST overlap, while
	// a corner of its AABB that is NOT inside the rotated beam must NOT overlap!
	d45 := float32(math.Sqrt2 * 0.5)
	beamInside := physics3d.SphereShape(physics3d.Sphere{
		Center: physics3d.Vec3{X: 2 + 1.5*d45, Y: 0, Z: -1.5 * d45},
		Radius: 0.1,
	})
	beamMiss := physics3d.SphereShape(physics3d.Sphere{
		Center: physics3d.Vec3{X: 2 + 1.5*d45, Y: 0, Z: 1.5 * d45},
		Radius: 0.1,
	})
	if len(world.QueryOverlap(beamInside)) == 0 {
		t.Fatalf("expected rotated Beam-boxcolonly to overlap point along its rotated axis")
	}
	if hits := world.QueryOverlap(beamMiss); len(hits) != 0 {
		t.Fatalf("expected rotated Beam-boxcolonly to miss empty AABB corner, got %+v", hits)
	}

	// Check 2: Sphere at (2, 5, 0) with radius 1.5.
	var orb *physics3d.Body
	var logBody *physics3d.Body
	var goalBody *physics3d.Body
	var hiddenCap *physics3d.Body
	for _, b := range col.Bodies {
		switch b.Tag {
		case "lvl:Orb":
			orb = b
		case "lvl:Log":
			logBody = b
		case "lvl:goal":
			goalBody = b
		case "lvl:HiddenCapsule":
			hiddenCap = b
		}
	}
	if orb == nil || orb.Shape.Kind != physics3d.ShapeSphere || !approx(orb.Shape.Sphere.Radius, 1.5, 1e-4) || !approxVec(orb.Pos, physics3d.Vec3{X: 2, Y: 5, Z: 0}, 1e-4) {
		t.Fatalf("unexpected Orb body: %+v", orb)
	}

	// Check 3: Log capsule rotated 90 deg around Z -> world endpoints at (2-1.5, 0, 6) and (2+1.5, 0, 6), radius 0.5.
	if logBody == nil {
		t.Fatalf("missing Log capsule body")
	}
	ws := logBody.WorldShape()
	if ws.Kind != physics3d.ShapeCapsule || !approx(ws.Capsule.Radius, 0.5, 1e-4) {
		t.Fatalf("unexpected Log world shape: %+v", ws)
	}
	minX := min(ws.Capsule.A.X, ws.Capsule.B.X)
	maxX := max(ws.Capsule.A.X, ws.Capsule.B.X)
	if !approx(minX, 0.5, 1e-3) || !approx(maxX, 3.5, 1e-3) {
		t.Fatalf("Log capsule X endpoints = [%v, %v], want [0.5, 3.5]", minX, maxX)
	}

	// Check 4: Convex fallback trimesh at (10, 0, 0) with bounds [9..11, -1..1, -1..1].
	if col.TriMeshes[0].Tag != "lvl:Hull" {
		t.Fatalf("Hull tag = %q, want %q", col.TriMeshes[0].Tag, "lvl:Hull")
	}
	if h, ok := world.RaycastFirst(physics3d.Vec3{X: 10, Y: 5, Z: 0}, physics3d.Vec3{X: 10, Y: -5, Z: 0}); !ok || h.TriMesh == nil || !approx(h.Point.Y, 1.0, 1e-3) {
		t.Fatalf("expected raycast hit on Hull-convcolonly at Y=1, got (%+v, %v)", h, ok)
	}

	// Check 5: Meshless trigger box at (2, 1, -5) with halfExtents (1.5, 1, 1.5), layer 2.
	if goalBody == nil || !goalBody.IsTrigger || goalBody.Layer != 2 || !approxVec(goalBody.Pos, physics3d.Vec3{X: 2, Y: 1, Z: -5}, 1e-4) {
		t.Fatalf("unexpected GoalEmpty body: %+v", goalBody)
	}

	// Check 6: HiddenCapsule at (-2, 2, 0) with radius 0.6, total height 2.4 -> halfSeg = 0.6.
	if hiddenCap == nil {
		t.Fatalf("missing HiddenCapsule body")
	}
	hcws := hiddenCap.WorldShape()
	if hcws.Kind != physics3d.ShapeCapsule || !approx(hcws.Capsule.Radius, 0.6, 1e-4) || !approx(hcws.Capsule.B.Y-hcws.Capsule.A.Y, 1.2, 1e-4) {
		t.Fatalf("unexpected HiddenCapsule world shape: %+v", hcws)
	}
}

func TestMergeBoxesGreedy3D(t *testing.T) {
	// 1. Solid 4x2x3 block of unit cubes -> merges along X, then Z, then Y into 1 single box.
	var solid []physics3d.Box
	for y := range 2 {
		for z := range 3 {
			for x := range 4 {
				solid = append(solid, physics3d.BoxFromMinMax(
					physics3d.Vec3{X: float32(x), Y: float32(y), Z: float32(z)},
					physics3d.Vec3{X: float32(x + 1), Y: float32(y + 1), Z: float32(z + 1)},
				))
			}
		}
	}
	merged := physics3d.MergeBoxes(solid, 0)
	if len(merged) != 1 {
		t.Fatalf("4x2x3 solid merged into %d boxes, want 1: %+v", len(merged), merged)
	}
	if !approxVec(merged[0].Min(), physics3d.Vec3{X: 0, Y: 0, Z: 0}, 1e-5) ||
		!approxVec(merged[0].Max(), physics3d.Vec3{X: 4, Y: 2, Z: 3}, 1e-5) {
		t.Fatalf("merged solid bounds = [%v, %v], want [(0,0,0), (4,2,3)]", merged[0].Min(), merged[0].Max())
	}

	// 2. Wall with doorway: 3x3 wall missing (1,0,0) and (1,1,0) -> 7 cells merge to 3 boxes.
	var wall []physics3d.Box
	for y := range 3 {
		for x := range 3 {
			if x == 1 && y < 2 {
				continue
			}
			wall = append(wall, physics3d.BoxFromMinMax(
				physics3d.Vec3{X: float32(x), Y: float32(y), Z: 0},
				physics3d.Vec3{X: float32(x + 1), Y: float32(y + 1), Z: 1},
			))
		}
	}
	// Reverse input order to verify determinism.
	rev := make([]physics3d.Box, len(wall))
	for i := range wall {
		rev[i] = wall[len(wall)-1-i]
	}
	m1 := physics3d.MergeBoxes(wall, 0)
	m2 := physics3d.MergeBoxes(rev, 0)
	if len(m1) != 3 {
		t.Fatalf("doorway wall merged into %d boxes, want 3: %+v", len(m1), m1)
	}
	if len(m2) != len(m1) {
		t.Fatalf("order-dependent merge count: %d vs %d", len(m1), len(m2))
	}
	for i := range m1 {
		if m1[i] != m2[i] {
			t.Fatalf("order-dependent merge box[%d]: %+v vs %+v", i, m1[i], m2[i])
		}
	}

	// 3. Floating-point jitter within eps still merges cleanly.
	jitter := []physics3d.Box{
		physics3d.BoxFromMinMax(physics3d.Vec3{X: 0, Y: 0, Z: 0}, physics3d.Vec3{X: 1.0000001, Y: 1, Z: 1}),
		physics3d.BoxFromMinMax(physics3d.Vec3{X: 0.9999999, Y: 0.00001, Z: -0.00001}, physics3d.Vec3{X: 2, Y: 1.00001, Z: 0.99999}),
	}
	mj := physics3d.MergeBoxes(jitter, 1e-4)
	if len(mj) != 1 {
		t.Fatalf("jittered adjacent boxes merged into %d boxes, want 1", len(mj))
	}
}

package navbake3d_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/ai/navbake3d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/world3d"
)

// smallAgent bakes the small test levels at a fine resolution.
var (
	smallAgent    = navbake3d.AgentType{Name: "a", Radius: 0.3, Height: 1.5, MaxClimb: 0.3}
	smallSettings = navbake3d.Settings{CellSize: 0.1, CellHeight: 0.05, MaxSimplificationError: 0.1}
)

// floorBox adds a 10 by 10 floor whose top is at y 0, as a physics3d static box.
func floorBox(w *physics3d.World) {
	box(w, 0, -0.2, 0, 10, 0, 10, 0)
}

func bakeSmall(t *testing.T, g *navbake3d.Geometry) *world3d.NavMesh {
	t.Helper()
	m, err := navbake3d.BakeMesh(g, smallSettings, smallAgent)
	if err != nil {
		t.Fatal(err)
	}
	return m.NavMesh()
}

// onFloor reports whether nm has a polygon at floor height under (x, z).
func onFloor(nm *world3d.NavMesh, x, z float32) bool {
	i, ok := nm.FindPolygon(v3(x, 0, z))
	return ok && nm.Polys[i].HeightAt(x, z) < 0.2
}

func TestPhysicsShapesAreSolid(t *testing.T) {
	w := physics3d.NewWorld()
	floorBox(w)
	sphere := physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 1}), physics3d.Vec3{X: 3, Y: 0.5, Z: 3})
	w.AddBody(sphere)
	capsule := physics3d.NewStaticBody(physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.5, 3)), physics3d.Vec3{X: 7, Y: 1.5, Z: 7})
	w.AddBody(capsule)
	tilted := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 0.2}}), physics3d.Vec3{X: 7, Y: 1, Z: 3})
	tilted.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, 0.7)
	w.AddBody(tilted)
	// Dynamic bodies and triggers are not level geometry.
	w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), physics3d.Vec3{X: 3, Y: 1, Z: 7}, 1))
	trigger := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), physics3d.Vec3{X: 5, Y: 1, Z: 5})
	trigger.IsTrigger = true
	w.AddBody(trigger)

	g := &navbake3d.Geometry{}
	g.AddPhysicsWorld(w, navbake3d.LayerAll)
	nm := bakeSmall(t, g)
	for _, p := range [][2]float32{{3, 3}, {7, 7}, {7, 3}} {
		if onFloor(nm, p[0], p[1]) {
			t.Errorf("the floor inside the shape at %v is walkable", p)
		}
	}
	for _, p := range [][2]float32{{3, 7}, {5, 5}, {1, 1}} {
		if !onFloor(nm, p[0], p[1]) {
			t.Errorf("the open floor at %v, under a dynamic body or a trigger, is not walkable", p)
		}
	}
	if _, ok := nm.FindPath(v3(1, 0, 1), v3(9, 0, 9)); !ok {
		t.Fatal("no path across the floor")
	}
}

func TestPhysicsTriMeshAndHeightfield(t *testing.T) {
	w := physics3d.NewWorld()
	// A heightfield on layer 1 and a ramp on layer 2 up to y 1 at x 10.
	heights := make([]float32, 11*11)
	hf := physics3d.NewHeightfieldCollider(11, 11, 1, physics3d.Vec3{}, heights)
	hf.Layer = 1
	w.AddHeightfield(hf)
	tm := physics3d.NewTriMeshCollider([]physics3d.Vec3{{X: 10, Z: 0}, {X: 10, Z: 10}, {X: 14, Y: 1, Z: 10}, {X: 14, Y: 1, Z: 0}}, []uint32{0, 1, 2, 0, 2, 3})
	tm.Layer = 2
	w.AddTriMesh(tm)

	g := &navbake3d.Geometry{}
	g.AddPhysicsWorld(w, 1)
	nm := bakeSmall(t, g)
	if !onFloor(nm, 5, 5) {
		t.Fatal("the heightfield is not walkable")
	}
	if _, ok := nm.FindPolygon(v3(12, 0.5, 5)); ok {
		t.Fatal("a layer 1 selection baked the layer 2 ramp")
	}

	g = &navbake3d.Geometry{}
	g.AddPhysicsWorld(w, navbake3d.LayerAll)
	nm = bakeSmall(t, g)
	path, ok := nm.FindPath(v3(5, 0, 5), v3(13, 0.75, 5))
	if !ok {
		t.Fatal("no path from the heightfield up the ramp")
	}
	if end := path[len(path)-1]; end.Y < 0.6 || end.Y > 0.95 {
		t.Fatalf("path ends at height %v on the ramp, want about 0.75", end.Y)
	}
}

// voxelSlab is a VoxelBoundsGrid of 1 m voxels: a 10 by 10 floor one voxel thick whose top is at y 0, with a pillar three voxels tall on the cell at x 5, z 5.
type voxelSlab struct{}

func (voxelSlab) VoxelSize() float32 { return 1 }

func (voxelSlab) IsSolid(x, y, z int) bool {
	if x < 0 || x >= 10 || z < 0 || z >= 10 {
		return false
	}
	return y == -1 || (x == 5 && z == 5 && y >= 0 && y < 3)
}

func (voxelSlab) VoxelBounds() (lo, hi physics3d.Vec3, ok bool) {
	return physics3d.Vec3{Y: -1}, physics3d.Vec3{X: 10, Y: 3, Z: 10}, true
}

// voxelOpen is a VoxelGrid that does not report its bounds.
type voxelOpen struct{}

func (voxelOpen) VoxelSize() float32 { return 1 }

func (voxelOpen) IsSolid(_, y, _ int) bool { return y == -1 }

func TestPhysicsVoxelColliders(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddVoxelCollider(physics3d.NewVoxelCollider(voxelSlab{}))
	g := &navbake3d.Geometry{}
	g.AddPhysicsWorld(w, navbake3d.LayerAll)
	nm := bakeSmall(t, g)
	if !onFloor(nm, 2, 2) {
		t.Fatal("the voxel floor is not walkable")
	}
	if onFloor(nm, 5.5, 5.5) {
		t.Fatal("the floor inside the voxel pillar is walkable")
	}

	open := physics3d.NewWorld()
	open.AddVoxelCollider(physics3d.NewVoxelCollider(voxelOpen{}))
	g = &navbake3d.Geometry{}
	g.AddPhysicsWorld(open, navbake3d.LayerAll)
	if g.TriangleCount() != 0 {
		t.Fatalf("a voxel grid without bounds added %d triangles", g.TriangleCount())
	}
}

func TestGridMapColliderBoxes(t *testing.T) {
	lib := world3d.NewMeshLibrary()
	floor := lib.Add(world3d.MeshTileDef{Name: "floor", ColliderBoxes: []math3d.AABB{{Min: v3(-1, -1, -1), Max: v3(1, -0.8, 1)}}})
	block := lib.Add(world3d.MeshTileDef{Name: "block", ColliderBoxes: []math3d.AABB{{Min: v3(-1, -1, -1), Max: v3(1, 1, 1)}}})
	gm := world3d.NewGridMap(v3(2, 2, 2), lib)
	gm.FillBox(world3d.GridCoord{}, world3d.GridCoord{X: 4, Z: 4}, floor, 0)
	gm.SetCell(world3d.GridCoord{X: 2, Z: 2}, block, 0)

	g := &navbake3d.Geometry{}
	g.AddGridMap(gm, 1)
	m, err := navbake3d.BakeMesh(g, smallSettings, smallAgent)
	if err != nil {
		t.Fatal(err)
	}
	nm := m.NavMesh()
	// The floor tiles' tops are at y 0.2; the block replacing the middle tile fills its cell from y 0 to 2.
	if i, ok := nm.FindPolygon(v3(1, 0.2, 1)); !ok || nm.Polys[i].HeightAt(1, 1) < 0.1 {
		t.Fatal("the floor tiles are not walkable")
	}
	if i, ok := nm.FindPolygon(v3(5, 0.2, 5)); ok && nm.Polys[i].HeightAt(5, 5) < 1 {
		t.Fatal("the floor under the block is walkable")
	}
}

func TestSceneMeshColliders(t *testing.T) {
	s := scene3d.NewScene()
	quad := []math3d.Vec3{v3(-5, 0, -5), v3(-5, 0, 5), v3(5, 0, 5), v3(-5, 0, -5), v3(5, 0, 5), v3(5, 0, -5)}
	ground := scene3d.NewNode("ground")
	ground.SetCollider(&scene3d.MeshCollider{Triangles: quad})
	ground.SetPosition(v3(5, 0, 5))
	if err := s.Add(ground); err != nil {
		t.Fatal(err)
	}
	// Disabled nodes and nodes off the mask add nothing.
	hidden := scene3d.NewNode("off")
	hidden.SetCollider(&scene3d.MeshCollider{Triangles: quad})
	hidden.SetPosition(v3(5, 3, 5))
	hidden.Enabled = false
	if err := s.Add(hidden); err != nil {
		t.Fatal(err)
	}
	// A disabled node's children add nothing either.
	child := scene3d.NewNode("off child")
	child.SetCollider(&scene3d.MeshCollider{Triangles: quad})
	if err := hidden.AddChild(child); err != nil {
		t.Fatal(err)
	}
	// An enabled node after a disabled one is still baked.
	roof := scene3d.NewNode("roof")
	roof.SetCollider(&scene3d.MeshCollider{Triangles: quad})
	roof.SetPosition(v3(5, 9, 5))
	if err := s.Add(roof); err != nil {
		t.Fatal(err)
	}
	other := scene3d.NewNode("other")
	other.SetCollider(&scene3d.MeshCollider{Triangles: quad})
	other.SetPosition(v3(5, 6, 5))
	other.Layer = 2
	if err := s.Add(other); err != nil {
		t.Fatal(err)
	}
	g := &navbake3d.Geometry{}
	if err := g.AddScene(s, scene3d.LayerDefault, nil); err != nil {
		t.Fatal(err)
	}
	if g.TriangleCount() != 4 {
		t.Fatalf("AddScene added %d triangles, want the ground's and the roof's 4", g.TriangleCount())
	}
	nm := bakeSmall(t, g)
	if !onFloor(nm, 5, 5) {
		t.Fatal("the scene's ground is not walkable")
	}
}

func TestSceneFileWithGLTFMeshes(t *testing.T) {
	fsys := os.DirFS("testdata/level")
	data, err := os.ReadFile("testdata/level/level.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.NavMesh != "level.navmesh.yaml" {
		t.Fatalf("scene names navMesh %q", doc.NavMesh)
	}
	g := &navbake3d.Geometry{}
	if err := g.AddSceneFile(doc, fsys); err != nil {
		t.Fatal(err)
	}
	// Two boxes of 12 triangles each.
	if g.TriangleCount() != 24 {
		t.Fatalf("the scene added %d triangles, want 24", g.TriangleCount())
	}
	nm := bakeSmall(t, g)
	if !onFloor(nm, 2, 2) {
		t.Fatal("the scene's floor is not walkable")
	}
	if onFloor(nm, 5, 5) {
		t.Fatal("the floor under the pillar is walkable")
	}
	// A model that does not load is reported with the node.
	bad := &sceneformat.SceneDoc{Version: 2, Layers: []sceneformat.LayerDoc{{Name: "l", Objects: []sceneformat.ObjectDoc{{ID: "x", Model: "missing.glb"}}}}}
	if err := (&navbake3d.Geometry{}).AddSceneFile(bad, fsys); err == nil || !strings.Contains(err.Error(), "missing.glb") {
		t.Fatalf("AddSceneFile with a missing model: %v", err)
	}
}

func TestSceneFileBakesOffMeshLinksIntoNavMeshYAML(t *testing.T) {
	fsys := os.DirFS("testdata/level")
	// Two separate elevated platforms (y=3 and y=0) built from box.gltf + a one-way drop nav_link placed in the scene.
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "level",
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "ledge",
						X:      2,
						Y:      2.9,
						PosZ:   2,
						ScaleX: sceneformat.Float32(4),
						ScaleY: sceneformat.Float32(0.2),
						ScaleZ: sceneformat.Float32(4),
						Model:  "box.gltf",
					},
					{
						ID:     "floor",
						X:      10,
						Y:      -0.1,
						PosZ:   2,
						ScaleX: sceneformat.Float32(4),
						ScaleY: sceneformat.Float32(0.2),
						ScaleZ: sceneformat.Float32(4),
						Model:  "box.gltf",
					},
					{
						ID:   "ledge_drop",
						X:    3.2,
						Y:    3.0,
						PosZ: 2.0,
						Components: map[string]any{
							sceneformat.NavLinkComponentName: map[string]any{
								"endX":          5.6,
								"endY":          -3.0,
								"endZ":          0.0,
								"radius":        0.5,
								"bidirectional": false,
								"area":          int(world3d.NavAreaJump),
								"flags":         int(world3d.NavFlagJump),
								"linkID":        44,
								"userData":      "drop",
							},
						},
					},
				},
			},
		},
	}

	g := &navbake3d.Geometry{}
	if err := g.AddSceneFile(doc, fsys); err != nil {
		t.Fatalf("AddSceneFile: %v", err)
	}
	if len(g.OffMeshLinks()) != 1 {
		t.Fatalf("got %d links on geometry, want 1", len(g.OffMeshLinks()))
	}

	file, err := navbake3d.Bake(g, smallSettings, []navbake3d.AgentType{smallAgent})
	if err != nil {
		t.Fatalf("Bake: %v", err)
	}
	raw, err := navbake3d.Encode(file)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	parsed, err := navbake3d.Parse(raw)
	if err != nil {
		t.Fatalf("Parse:\n%s\nerr: %v", raw, err)
	}

	nm := parsed.Meshes[0].NavMesh()
	down := nm.FindPathWithOptions(v3(1.5, 3, 2), v3(10.5, 0, 2), ai.PathOptions{})
	if !down.Found || len(down.Links) != 1 || down.Links[0].LinkID != 44 || down.Links[0].UserData != "drop" {
		t.Fatalf("expected path down scene-baked drop link, got %+v", down)
	}
	if up := nm.FindPathWithOptions(v3(10.5, 0, 2), v3(1.5, 3, 2), ai.PathOptions{}); up.Found {
		t.Fatalf("one-way drop link baked from scene should not work in reverse: %+v", up)
	}
}

func TestGLTFTriangles(t *testing.T) {
	tris := navbake3d.GLTFTriangles(loadBox(t))
	if len(tris) != 36 {
		t.Fatalf("the box has %d vertices of triangles, want 36", len(tris))
	}
	// A mirroring transform keeps the box's top a floor.
	g := &navbake3d.Geometry{}
	g.AddGLTF(loadBox(t), math3d.TRS(v3(5, -0.5, 5), math3d.QuatIdentity(), v3(-10, 1, 10)), 1)
	nm := bakeSmall(t, g)
	if !onFloor(nm, 5, 5) {
		t.Fatal("the mirrored box's top is not walkable")
	}
	if !navbake3d.IsModelPath("Models/Level.GLB") || navbake3d.IsModelPath("level.obj") {
		t.Fatal("IsModelPath")
	}
}

func TestAddMeshSkipsBadTriangles(t *testing.T) {
	g := &navbake3d.Geometry{}
	nan := float32(0)
	nan /= nan
	g.AddMesh([]math3d.Vec3{v3(0, 0, 0), v3(1, 0, 0), v3(2, 0, 0), v3(nan, 0, 0)}, []uint32{0, 1, 2, 0, 1, 3, 0, 1, 9}, math3d.Mat4Identity(), 1)
	if n := g.TriangleCount(); n != 0 {
		t.Fatalf("added %d degenerate, non-finite or out-of-range triangles", n)
	}
	g.AddMesh([]math3d.Vec3{v3(0, 0, 0), v3(0, 0, 1), v3(1, 0, 0)}, nil, math3d.Mat4Identity(), 0)
	if n := g.TriangleCount(); n != 1 {
		t.Fatalf("added %d triangles from an unindexed list, want 1", n)
	}
}

package scene3d_test

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// wallQuadTriangles returns two CCW triangles in the XY plane (z = 0, outward
// normal +Z) spanning [-halfW, halfW] x [-halfH, halfH].
func wallQuadTriangles(halfW, halfH float32) []math3d.Vec3 {
	bl := math3d.Vec3{X: -halfW, Y: -halfH, Z: 0}
	br := math3d.Vec3{X: halfW, Y: -halfH, Z: 0}
	tr := math3d.Vec3{X: halfW, Y: halfH, Z: 0}
	tl := math3d.Vec3{X: -halfW, Y: halfH, Z: 0}
	return []math3d.Vec3{bl, br, tr, bl, tr, tl}
}

// floorQuadTriangles returns two CCW triangles in the XZ plane (y = 0, outward
// normal +Y) spanning [-half, half] x [-half, half].
func floorQuadTriangles(half float32) []math3d.Vec3 {
	v0 := math3d.Vec3{X: -half, Y: 0, Z: half}
	v1 := math3d.Vec3{X: half, Y: 0, Z: half}
	v2 := math3d.Vec3{X: half, Y: 0, Z: -half}
	v3 := math3d.Vec3{X: -half, Y: 0, Z: -half}
	return []math3d.Vec3{v0, v1, v2, v0, v2, v3}
}

func geomArea(g *scene3d.DecalGeometry) float32 {
	var total float32
	for i := 0; i+2 < len(g.Indices); i += 3 {
		a := g.Vertices[g.Indices[i]].Pos
		b := g.Vertices[g.Indices[i+1]].Pos
		c := g.Vertices[g.Indices[i+2]].Pos
		total += 0.5 * b.Sub(a).Cross(c.Sub(a)).Len()
	}
	return total
}

func TestDecalBoxClippingAndUVs(t *testing.T) {
	sc := scene3d.NewScene()
	wall := scene3d.NewNode("wall")
	wall.SetCollider(&scene3d.MeshCollider{Triangles: wallQuadTriangles(5, 5)})
	if err := sc.Add(wall); err != nil {
		t.Fatalf("Add wall: %v", err)
	}
	sc.UpdateTransforms()

	d := scene3d.NewDecalOnSurface(
		math3d.Vec3{X: 0.5, Y: -0.25, Z: 0},
		math3d.Vec3{Z: 1},
		math3d.Vec3Up(),
		math3d.Vec3{X: 1, Y: 0.5, Z: 0.5}, // 2x1x1 box
	)
	d.Bias = 0.01
	d.DepthFade = 0

	var geom scene3d.DecalGeometry
	tris := scene3d.ProjectDecal(sc, d, &geom)
	if tris == 0 {
		t.Fatal("expected projected triangles on wall, got 0")
	}
	if len(geom.Batches) != 1 || geom.Batches[0].IndexCount != uint32(tris*3) {
		t.Fatalf("unexpected batches: %+v", geom.Batches)
	}

	// Area must equal the exact 2 x 1 = 2.0 box footprint.
	area := geomArea(&geom)
	if !math3d.ApproxEqual(area, 2.0, 1e-3) {
		t.Errorf("clipped decal area = %f, want 2.0", area)
	}

	// Every clipped vertex must stay inside the box X/Y bounds, be biased to
	// z == 0.01, and have UVs in [0, 1] matching top-left origin.
	var minU, maxU, minV, maxV float32 = 2, -1, 2, -1
	for i, v := range geom.Vertices {
		if v.Pos.X < -0.5-1e-4 || v.Pos.X > 1.5+1e-4 {
			t.Errorf("v[%d].Pos.X = %f outside [-0.5, 1.5]", i, v.Pos.X)
		}
		if v.Pos.Y < -0.75-1e-4 || v.Pos.Y > 0.25+1e-4 {
			t.Errorf("v[%d].Pos.Y = %f outside [-0.75, 0.25]", i, v.Pos.Y)
		}
		if !math3d.ApproxEqual(v.Pos.Z, d.Bias, 1e-4) {
			t.Errorf("v[%d].Pos.Z = %f, want bias %f", i, v.Pos.Z, d.Bias)
		}
		if v.UV.X < -1e-5 || v.UV.X > 1+1e-5 || v.UV.Y < -1e-5 || v.UV.Y > 1+1e-5 {
			t.Errorf("v[%d].UV = %+v outside [0, 1]", i, v.UV)
		}
		wantU := (v.Pos.X-d.Transform.Pos.X)/(2*d.HalfExtents.X) + 0.5
		wantV := 0.5 - (v.Pos.Y-d.Transform.Pos.Y)/(2*d.HalfExtents.Y)
		if !math3d.ApproxEqual(v.UV.X, wantU, 1e-3) || !math3d.ApproxEqual(v.UV.Y, wantV, 1e-3) {
			t.Errorf("v[%d].UV = (%f, %f), want (%f, %f)", i, v.UV.X, v.UV.Y, wantU, wantV)
		}
		if !math3d.ApproxEqual(v.Color.W, 1.0, 1e-4) {
			t.Errorf("v[%d].Color.W = %f, want 1.0", i, v.Color.W)
		}
		minU = min(minU, v.UV.X)
		maxU = max(maxU, v.UV.X)
		minV = min(minV, v.UV.Y)
		maxV = max(maxV, v.UV.Y)
	}
	if !math3d.ApproxEqual(minU, 0, 1e-3) || !math3d.ApproxEqual(maxU, 1, 1e-3) ||
		!math3d.ApproxEqual(minV, 0, 1e-3) || !math3d.ApproxEqual(maxV, 1, 1e-3) {
		t.Errorf("UV span = [%f..%f] x [%f..%f], want [0..1] x [0..1]", minU, maxU, minV, maxV)
	}
}

func TestDecalNormalsAndAngleFade(t *testing.T) {
	makeRotatedWallScene := func(deg float32) *scene3d.Scene {
		sc := scene3d.NewScene()
		wall := scene3d.NewNode("wall")
		wall.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(deg)))
		wall.SetCollider(&scene3d.MeshCollider{Triangles: wallQuadTriangles(3, 3)})
		_ = sc.Add(wall)
		sc.UpdateTransforms()
		return sc
	}

	makeDecal := func() scene3d.Decal {
		d := scene3d.NewDecal()
		d.Transform.Pos = math3d.Vec3Zero()
		d.HalfExtents = math3d.Vec3{X: 1, Y: 1, Z: 1}
		d.AngleFadeStart = math3d.DegToRad(30)
		d.AngleFadeEnd = math3d.DegToRad(75)
		d.DepthFade = 0
		return d
	}

	// 0° head-on: full opacity.
	var g0 scene3d.DecalGeometry
	if tris := scene3d.ProjectDecal(makeRotatedWallScene(0), makeDecal(), &g0); tris == 0 {
		t.Fatal("0° surface should receive decal")
	}
	a0 := g0.Vertices[0].Color.W
	if !math3d.ApproxEqual(a0, 1.0, 1e-4) {
		t.Errorf("0° alpha = %f, want 1.0", a0)
	}

	// 20° (below AngleFadeStart 30°): still full opacity.
	var g20 scene3d.DecalGeometry
	if tris := scene3d.ProjectDecal(makeRotatedWallScene(20), makeDecal(), &g20); tris == 0 {
		t.Fatal("20° surface should receive decal")
	}
	if !math3d.ApproxEqual(g20.Vertices[0].Color.W, 1.0, 1e-4) {
		t.Errorf("20° alpha = %f, want 1.0", g20.Vertices[0].Color.W)
	}

	// 45° and 60° (inside fade ramp): strictly monotonic fade between 0 and 1.
	var g45, g60 scene3d.DecalGeometry
	if scene3d.ProjectDecal(makeRotatedWallScene(45), makeDecal(), &g45) == 0 {
		t.Fatal("45° surface should receive faded decal")
	}
	if scene3d.ProjectDecal(makeRotatedWallScene(60), makeDecal(), &g60) == 0 {
		t.Fatal("60° surface should receive faded decal")
	}
	a45 := g45.Vertices[0].Color.W
	a60 := g60.Vertices[0].Color.W
	if !(a45 < 1.0 && a45 > a60 && a60 > 0) {
		t.Errorf("expected 1.0 > alpha(45°)=%f > alpha(60°)=%f > 0", a45, a60)
	}

	// 80° (past AngleFadeEnd 75°), 90° (perpendicular), and 180° (back-facing):
	// all must be clipped away completely.
	for _, deg := range []float32{75, 80, 90, 135, 180} {
		var g scene3d.DecalGeometry
		if tris := scene3d.ProjectDecal(makeRotatedWallScene(deg), makeDecal(), &g); tris != 0 {
			t.Errorf("%v° surface received %d triangles, want 0", deg, tris)
		}
	}

	// Direct AngleFade helper matches projection behavior.
	d := makeDecal()
	if got := d.AngleFade(math3d.Vec3{Z: 1}); !math3d.ApproxEqual(got, 1, 1e-4) {
		t.Errorf("AngleFade(+Z) = %f, want 1", got)
	}
	if got := d.AngleFade(math3d.Vec3{X: 1}); got != 0 {
		t.Errorf("AngleFade(+X) = %f, want 0", got)
	}
	if got := d.AngleFade(math3d.Vec3{Z: -1}); got != 0 {
		t.Errorf("AngleFade(-Z) = %f, want 0", got)
	}
}

func TestDecalSmoothNormalsAndDepthFade(t *testing.T) {
	// Single triangle inside the projection box with one head-on vertex normal
	// and two tilted vertex normals.
	nHead := math3d.Vec3{Z: 1}
	nTilt := math3d.Vec3{X: float32(math.Sin(float64(math3d.DegToRad(60)))), Z: float32(math.Cos(float64(math3d.DegToRad(60))))}
	recv := &scene3d.DecalReceiverMesh{
		Positions: []math3d.Vec3{
			{X: -0.4, Y: -0.4, Z: 0},
			{X: 0.4, Y: -0.4, Z: 0},
			{X: 0, Y: 0.4, Z: 0},
		},
		Normals: []math3d.Vec3{nHead, nTilt, nTilt},
		Indices: []uint32{0, 1, 2},
	}

	d := scene3d.NewDecal()
	d.HalfExtents = math3d.Vec3{X: 1, Y: 1, Z: 1}
	d.AngleFadeStart = math3d.DegToRad(30)
	d.AngleFadeEnd = math3d.DegToRad(80)
	d.DepthFade = 0

	var geom scene3d.DecalGeometry
	if tris := scene3d.ProjectDecalOntoReceiver(d, recv, math3d.Mat4Identity(), &geom); tris != 1 {
		t.Fatalf("got %d triangles, want 1", tris)
	}
	if !math3d.ApproxEqual(geom.Vertices[0].Color.W, 1.0, 1e-4) {
		t.Errorf("head-on vertex alpha = %f, want 1.0", geom.Vertices[0].Color.W)
	}
	if !(geom.Vertices[1].Color.W > 0 && geom.Vertices[1].Color.W < 0.8) {
		t.Errorf("tilted vertex alpha = %f, want in (0, 0.8)", geom.Vertices[1].Color.W)
	}

	// DepthFade fades near |local.z| -> 0.5 (here HalfExtents.Z == 1, so world
	// z in [-1, 1] maps to local.z in [-0.5, 0.5]).
	dDepth := scene3d.NewDecal()
	dDepth.HalfExtents = math3d.Vec3{X: 1, Y: 1, Z: 1}
	dDepth.DepthFade = 0.4 // outer 40% of half-depth (|z| in [0.6, 1.0])

	sampleAtZ := func(z float32) float32 {
		var g scene3d.DecalGeometry
		scene3d.ProjectDecalOntoReceiver(dDepth, recv, math3d.Translate(math3d.Vec3{Z: z}), &g)
		if len(g.Vertices) == 0 {
			return 0
		}
		return g.Vertices[0].Color.W
	}
	aCenter := sampleAtZ(0)
	aNearEdge := sampleAtZ(0.8)
	aAtEdge := sampleAtZ(0.999)
	if !math3d.ApproxEqual(aCenter, 1.0, 1e-4) {
		t.Errorf("center depth alpha = %f, want 1.0", aCenter)
	}
	if !(aNearEdge > 0 && aNearEdge < 1.0) {
		t.Errorf("near-edge depth alpha = %f, want in (0, 1)", aNearEdge)
	}
	if aAtEdge >= aNearEdge {
		t.Errorf("edge alpha %f should be less than near-edge alpha %f", aAtEdge, aNearEdge)
	}
}

func TestDecalLayerMaskExcludesObjects(t *testing.T) {
	const (
		layerWorld     uint32 = 1 << 0
		layerCharacter uint32 = 1 << 2
		layerFX        uint32 = 1 << 5
	)

	sc := scene3d.NewScene()

	worldWall := scene3d.NewNode("world_wall")
	worldWall.Layer = layerWorld
	worldWall.SetCollider(&scene3d.MeshCollider{Triangles: wallQuadTriangles(2, 2)})

	character := scene3d.NewNode("character")
	character.Layer = layerCharacter
	character.SetPosition(math3d.Vec3{Z: 0.1})
	character.SetDecalReceiver(&scene3d.DecalReceiverMesh{
		Positions: wallQuadTriangles(2, 2),
	})

	hiddenWall := scene3d.NewNode("hidden_wall")
	hiddenWall.Layer = layerWorld
	hiddenWall.Visible = false
	hiddenWall.SetPosition(math3d.Vec3{Z: 0.2})
	hiddenWall.SetCollider(&scene3d.MeshCollider{Triangles: wallQuadTriangles(2, 2)})

	_ = sc.Add(worldWall)
	_ = sc.Add(character)
	_ = sc.Add(hiddenWall)
	sc.UpdateTransforms()

	d := scene3d.NewDecal()
	d.HalfExtents = math3d.Vec3{X: 1, Y: 1, Z: 1}
	d.Bias = 0
	d.DepthFade = 0

	// LayerWorld only: hits worldWall (z == 0), excludes character (z == 0.1) and hiddenWall.
	d.LayerMask = layerWorld
	var gWorld scene3d.DecalGeometry
	if tris := scene3d.ProjectDecal(sc, d, &gWorld); tris == 0 {
		t.Fatal("expected decal on worldWall")
	}
	for i, v := range gWorld.Vertices {
		if !math3d.ApproxEqual(v.Pos.Z, 0, 1e-4) {
			t.Errorf("vertex %d landed at z=%f, expected only worldWall (z=0)", i, v.Pos.Z)
		}
	}

	// LayerCharacter only: hits character (z == 0.1) and excludes worldWall.
	d.LayerMask = layerCharacter
	var gChar scene3d.DecalGeometry
	if tris := scene3d.ProjectDecal(sc, d, &gChar); tris == 0 {
		t.Fatal("expected decal on character")
	}
	for i, v := range gChar.Vertices {
		if !math3d.ApproxEqual(v.Pos.Z, 0.1, 1e-4) {
			t.Errorf("vertex %d landed at z=%f, expected only character (z=0.1)", i, v.Pos.Z)
		}
	}

	// Unmatched layer or zero mask: zero geometry.
	for _, mask := range []uint32{layerFX, 0} {
		d.LayerMask = mask
		var gNone scene3d.DecalGeometry
		if tris := scene3d.ProjectDecal(sc, d, &gNone); tris != 0 {
			t.Errorf("mask %#x produced %d triangles, want 0", mask, tris)
		}
	}
}

func TestDecalPoolCapacityEvictionAndLifetime(t *testing.T) {
	pool := scene3d.NewDecalPool(3)
	if pool.Cap() != 3 || pool.Len() != 0 {
		t.Fatalf("Cap=%d Len=%d, want 3, 0", pool.Cap(), pool.Len())
	}

	// Spawn 5 decals with equal SortOrder: oldest 2 must be evicted (FIFO).
	handles := make([]scene3d.DecalHandle, 5)
	for i := range 5 {
		d := scene3d.NewDecal()
		d.TextureID = uint32(i + 1)
		handles[i] = pool.Spawn(d)
		if !handles[i].Valid() {
			t.Fatalf("spawn %d returned invalid handle", i)
		}
		if pool.Len() > pool.Cap() {
			t.Fatalf("Len %d exceeded Cap %d", pool.Len(), pool.Cap())
		}
	}
	if pool.Len() != 3 {
		t.Fatalf("Len = %d, want 3", pool.Len())
	}
	for i := range 2 {
		if pool.Contains(handles[i]) {
			t.Errorf("oldest handle %d should have been evicted", i)
		}
		if _, ok := pool.Get(handles[i]); ok {
			t.Errorf("Get on evicted handle %d should fail", i)
		}
	}
	active := pool.Active()
	if len(active) != 3 || active[0].TextureID != 3 || active[1].TextureID != 4 || active[2].TextureID != 5 {
		t.Fatalf("active TextureIDs = %v, want [3 4 5]", []uint32{active[0].TextureID, active[1].TextureID, active[2].TextureID})
	}

	// Priority eviction: high SortOrder survives while lowest SortOrder is evicted.
	pool.Clear()
	if pool.Len() != 0 {
		t.Fatalf("Len after Clear = %d, want 0", pool.Len())
	}
	if pool.Contains(handles[4]) {
		t.Error("handle remained valid after Clear")
	}

	high := scene3d.NewDecal()
	high.SortOrder = 10
	high.TextureID = 100
	hHigh := pool.Spawn(high)

	low1 := scene3d.NewDecal()
	low1.SortOrder = 0
	low1.TextureID = 1
	hLow1 := pool.Spawn(low1)

	low2 := scene3d.NewDecal()
	low2.SortOrder = 0
	low2.TextureID = 2
	hLow2 := pool.Spawn(low2)

	// Pool is full (high, low1, low2); spawning another decal evicts low1 (lowest SortOrder, oldest).
	mid := scene3d.NewDecal()
	mid.SortOrder = 5
	mid.TextureID = 50
	hMid := pool.Spawn(mid)

	if !pool.Contains(hHigh) || pool.Contains(hLow1) || !pool.Contains(hLow2) || !pool.Contains(hMid) {
		t.Fatalf("unexpected priority eviction state: high=%v low1=%v low2=%v mid=%v",
			pool.Contains(hHigh), pool.Contains(hLow1), pool.Contains(hLow2), pool.Contains(hMid))
	}

	// Active returns ascending SortOrder: low2 (0), mid (5), high (10).
	ordered := pool.Active()
	if ordered[0].TextureID != 2 || ordered[1].TextureID != 50 || ordered[2].TextureID != 100 {
		t.Errorf("ordered TextureIDs = [%d %d %d], want [2 50 100]",
			ordered[0].TextureID, ordered[1].TextureID, ordered[2].TextureID)
	}

	// Lifetime, FadeIn, FadeOut, and Step expiration.
	pool.Clear()
	fading := scene3d.NewDecal()
	fading.Lifetime = 2 * time.Second
	fading.FadeIn = 500 * time.Millisecond
	fading.FadeOut = 500 * time.Millisecond
	hFade := pool.Spawn(fading)

	dPtr, ok := pool.Get(hFade)
	if !ok || dPtr.EffectiveOpacity() != 0 {
		t.Fatalf("at Age=0 with FadeIn, EffectiveOpacity = %v, want 0", dPtr.EffectiveOpacity())
	}
	pool.Step(250 * time.Millisecond)
	if !math3d.ApproxEqual(dPtr.EffectiveOpacity(), 0.5, 1e-3) {
		t.Errorf("mid-FadeIn opacity = %f, want 0.5", dPtr.EffectiveOpacity())
	}
	pool.Step(750 * time.Millisecond) // Age = 1.0s (plateau)
	if !math3d.ApproxEqual(dPtr.EffectiveOpacity(), 1.0, 1e-3) {
		t.Errorf("plateau opacity = %f, want 1.0", dPtr.EffectiveOpacity())
	}
	pool.Step(750 * time.Millisecond) // Age = 1.75s (halfway through 500ms FadeOut)
	if !math3d.ApproxEqual(dPtr.EffectiveOpacity(), 0.5, 1e-3) {
		t.Errorf("mid-FadeOut opacity = %f, want 0.5", dPtr.EffectiveOpacity())
	}
	if expired := pool.Step(500 * time.Millisecond); expired != 1 {
		t.Errorf("Step past Lifetime expired = %d, want 1", expired)
	}
	if pool.Len() != 0 || pool.Contains(hFade) {
		t.Errorf("expired decal still alive: len=%d contains=%v", pool.Len(), pool.Contains(hFade))
	}

	// Zero-capacity pool is a safe no-op.
	zeroPool := scene3d.NewDecalPool(0)
	if h := zeroPool.Spawn(scene3d.NewDecal()); h.Valid() || zeroPool.Len() != 0 {
		t.Fatalf("zero pool Spawn returned %+v len=%d", h, zeroPool.Len())
	}
}

func TestDecalPoolZeroAllocHotPath(t *testing.T) {
	sc := scene3d.NewScene()
	floor := scene3d.NewNode("floor")
	floor.SetCollider(&scene3d.MeshCollider{Triangles: floorQuadTriangles(10)})
	_ = sc.Add(floor)
	sc.UpdateTransforms()
	sc.SyncBVH()

	pool := scene3d.NewDecalPool(16)
	d := scene3d.NewDecalOnSurface(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3Up(),
		math3d.Vec3{Z: -1},
		math3d.Vec3{X: 1, Y: 1, Z: 0.5},
	)
	d.Lifetime = 10 * time.Second

	var geom scene3d.DecalGeometry
	// Warm up pool and geometry slice capacities.
	for range 16 {
		pool.Spawn(d)
	}
	pool.BuildGeometry(sc, &geom)

	allocs := testing.AllocsPerRun(100, func() {
		pool.Spawn(d)
		pool.Step(16 * time.Millisecond)
		pool.BuildGeometry(sc, &geom)
	})
	if allocs != 0 {
		t.Errorf("steady-state Spawn+Step+BuildGeometry allocated %.1f times/op, want 0", allocs)
	}
}

// solidWallWGSL draws opaque receiver geometry using the same @group(0) camera
// uniform layout as DecalWGSL and render3d.CameraWGSL.
const solidWallWGSL = render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

func packSolidQuad(tris []math3d.Vec3, normal math3d.Vec3, col [4]float32) []byte {
	out := make([]byte, len(tris)*10*4)
	off := 0
	put := func(v float32) {
		binary.LittleEndian.PutUint32(out[off:], math.Float32bits(v))
		off += 4
	}
	for _, p := range tris {
		put(p.X)
		put(p.Y)
		put(p.Z)
		put(normal.X)
		put(normal.Y)
		put(normal.Z)
		put(col[0])
		put(col[1])
		put(col[2])
		put(col[3])
	}
	return out
}

func newSolidPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout, depth gpu.TextureFormat) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(solidWallWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL solid: %v", err)
	}
	desc := gpu.PipelineDesc{
		Label:    "solid_wall",
		Vertex:   gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment: gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:  []*gpu.BindGroupLayout{camLayout},
		VertexBuffers: []gpu.VertexBufferLayout{{
			Stride:   10 * 4,
			StepMode: gpu.StepVertex,
			Attributes: []gpu.VertexAttribute{
				{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
				{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
				{Format: gpu.VertexFloat32x4, Offset: 24, Location: 2},
			},
		}},
		Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:   []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
	}
	if depth != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{Format: depth, DepthWrite: true, DepthCompare: gpu.CompareLess}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline solid: %v", err)
	}
	return pipe
}

func expectColorNear(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol uint8) {
	t.Helper()
	got := img.RGBAAt(x, y)
	diff := func(a, b uint8) uint8 {
		if a > b {
			return a - b
		}
		return b - a
	}
	if diff(got.R, want.R) > tol || diff(got.G, want.G) > tol || diff(got.B, want.B) > tol || diff(got.A, want.A) > tol {
		t.Errorf("pixel (%d, %d) = %v, want %v (±%d)", x, y, got, want, tol)
	}
}

func TestGPUDecalForwardPathSoftwareRender(t *testing.T) {
	const size = 96
	const (
		layerWall     uint32 = 1 << 0
		layerExcluded uint32 = 1 << 1
	)

	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	// Share render3d.CameraBuffer across opaque receiver geometry and GPUDecalRenderer.
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	decalRen, err := scene3d.NewGPUDecalRenderer(nil, dev, scene3d.GPUDecalOptions{
		Label:        "test_decals",
		DepthFormat:  gpu.FormatDepth24Plus,
		CameraLayout: camBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewGPUDecalRenderer: %v", err)
	}
	t.Cleanup(func() { _ = decalRen.Close() })

	solidPipe := newSolidPipeline(t, nil, dev, camBuf.Layout(), gpu.FormatDepth24Plus)
	t.Cleanup(func() { _ = solidPipe.Close() })

	// Scene with a gray wall (layerWall) on the left/center and a blue excluded
	// panel (layerExcluded) on the right side, both facing +Z toward the camera.
	sc := scene3d.NewScene()
	wallTris := wallQuadTriangles(3, 3)
	wallNode := scene3d.NewNode("wall")
	wallNode.Layer = layerWall
	wallNode.SetCollider(&scene3d.MeshCollider{Triangles: wallTris})

	exclNode := scene3d.NewNode("excluded_panel")
	exclNode.Layer = layerExcluded
	exclNode.SetPosition(math3d.Vec3{X: 1.4, Y: 0, Z: 0.05})
	exclTris := wallQuadTriangles(0.8, 2.0)
	exclNode.SetCollider(&scene3d.MeshCollider{Triangles: exclTris})

	_ = sc.Add(wallNode)
	_ = sc.Add(exclNode)
	sc.UpdateTransforms()

	// Upload solid wall and excluded panel geometry.
	worldWallTris := make([]math3d.Vec3, len(wallTris))
	copy(worldWallTris, wallTris)
	wallVB, err := dev.CreateBuffer(nil, gpu.BufferDesc{
		Label: "wall_vb",
		Usage: gpu.UsageVertex,
		Data:  packSolidQuad(worldWallTris, math3d.Vec3{Z: 1}, [4]float32{0.5, 0.5, 0.5, 1}),
	})
	if err != nil {
		t.Fatalf("CreateBuffer wall: %v", err)
	}
	t.Cleanup(func() { _ = wallVB.Close() })

	worldExclTris := make([]math3d.Vec3, len(exclTris))
	exclMat := exclNode.WorldMatrix()
	for i, p := range exclTris {
		worldExclTris[i] = exclMat.TransformPoint(p)
	}
	exclVB, err := dev.CreateBuffer(nil, gpu.BufferDesc{
		Label: "excl_vb",
		Usage: gpu.UsageVertex,
		Data:  packSolidQuad(worldExclTris, math3d.Vec3{Z: 1}, [4]float32{0.2, 0.35, 0.85, 1}),
	})
	if err != nil {
		t.Fatalf("CreateBuffer excl: %v", err)
	}
	t.Cleanup(func() { _ = exclVB.Close() })

	// Create a circular opaque-center / transparent-corner albedo texture for
	// texture ID 1 (blob shadow / splat style).
	const texDim = 16
	circleTex := make([]byte, texDim*texDim*4)
	for y := range texDim {
		for x := range texDim {
			dx := (float32(x)+0.5)/texDim - 0.5
			dy := (float32(y)+0.5)/texDim - 0.5
			off := (y*texDim + x) * 4
			if dx*dx+dy*dy <= 0.35*0.35 {
				circleTex[off+0] = 255
				circleTex[off+1] = 255
				circleTex[off+2] = 255
				circleTex[off+3] = 255
			}
		}
	}
	if err := decalRen.UploadTextureRGBA(1, texDim, texDim, circleTex); err != nil {
		t.Fatalf("UploadTextureRGBA: %v", err)
	}

	// Spawn two decals via DecalPool:
	// 1. Solid red mark centered at (-0.6, 0, 0) covering wall + overlapping excluded panel's X range,
	//    but LayerMask = layerWall so the excluded panel stays blue!
	// 2. Dark circular blob splat (TextureID 1) at (0, 0, 0).
	pool := scene3d.NewDecalPool(8)

	redMark := scene3d.NewDecalOnSurface(
		math3d.Vec3{X: 0.4, Y: 0, Z: 0},
		math3d.Vec3{Z: 1},
		math3d.Vec3Up(),
		math3d.Vec3{X: 1.5, Y: 0.9, Z: 0.5},
	)
	redMark.Albedo = core.RGB(0xee2222)
	redMark.LayerMask = layerWall
	redMark.DepthFade = 0
	redMark.SortOrder = 0
	pool.Spawn(redMark)

	blob := scene3d.NewDecalOnSurface(
		math3d.Vec3{X: -1.4, Y: 0, Z: 0},
		math3d.Vec3{Z: 1},
		math3d.Vec3Up(),
		math3d.Vec3{X: 0.8, Y: 0.8, Z: 0.5},
	)
	blob.Albedo = core.RGB(0x101010)
	blob.TextureID = 1
	blob.LayerMask = layerWall
	blob.DepthFade = 0
	blob.SortOrder = 1
	pool.Spawn(blob)

	var geom scene3d.DecalGeometry
	if tris := pool.BuildGeometry(sc, &geom); tris == 0 {
		t.Fatal("BuildGeometry produced 0 triangles")
	}
	if err := decalRen.Upload(&geom); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 6, 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x10141c),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1,
		},
	})
	// 1. Opaque forward geometry pass.
	pass.SetPipeline(solidPipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetVertexBuffer(0, wallVB, 0)
	pass.Draw(uint32(len(worldWallTris)), 0)
	pass.SetVertexBuffer(0, exclVB, 0)
	pass.Draw(uint32(len(worldExclTris)), 0)
	// 2. Forward decal pass over the same depth buffer without z-fighting.
	decalRen.DrawWithCamera(pass, camBuf.BindGroup())
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	img := sr.Image()
	// World-to-pixel helper for orthographic camera of height 6 centered at (0,0):
	// x in [-3, 3] -> px in [0, 96], y in [-3, 3] -> py in [96, 0].
	toPx := func(wx, wy float32) (int, int) {
		px := int((wx + 3) / 6 * size)
		py := int((3 - wy) / 6 * size)
		return px, py
	}

	// Untouched wall above decals is gray (~128, 128, 128).
	gx, gy := toPx(0, 2.2)
	expectColorNear(t, img, gx, gy, color.RGBA{R: 128, G: 128, B: 128, A: 255}, 5)

	// Center of red decal on wall is red (no z-fighting against wall depth!).
	rx, ry := toPx(0.0, 0.0)
	expectColorNear(t, img, rx, ry, color.RGBA{R: 238, G: 34, B: 34, A: 255}, 6)

	// Excluded panel at x=1.4 is overlapped by redMark's box (x in [-1.1, 1.9]),
	// yet must remain blue because its layer was excluded by LayerMask!
	ex, ey := toPx(1.4, 0.0)
	expectColorNear(t, img, ex, ey, color.RGBA{R: 51, G: 89, B: 217, A: 255}, 6)

	// Center of circular blob splat at (-1.4, 0) is dark.
	bx, by := toPx(-1.4, 0.0)
	expectColorNear(t, img, bx, by, color.RGBA{R: 16, G: 16, B: 16, A: 255}, 6)

	// Corner of circular blob box at (-2.1, 0.7) has transparent texels -> stays wall gray.
	cx, cy := toPx(-2.1, 0.7)
	expectColorNear(t, img, cx, cy, color.RGBA{R: 128, G: 128, B: 128, A: 255}, 6)
}

func TestGPUDecalDrawsInMobileForwardGPUView(t *testing.T) {
	// Test both full GPU profile and WebGL 2 / OpenGL ES 3.0 mobile forward
	// profile (nsgotest.NoGPUCompute) inside gpu.View with pixel readback.
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"mobile-forward-webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			const viewSize = 64

			sc := scene3d.NewScene()
			wallTris := wallQuadTriangles(3, 3)
			wall := scene3d.NewNode("wall")
			wall.SetCollider(&scene3d.MeshCollider{Triangles: wallTris})
			_ = sc.Add(wall)
			sc.UpdateTransforms()

			pool := scene3d.NewDecalPool(4)
			d := scene3d.NewDecalOnSurface(
				math3d.Vec3Zero(),
				math3d.Vec3{Z: 1},
				math3d.Vec3Up(),
				math3d.Vec3{X: 1, Y: 1, Z: 0.5},
			)
			d.Albedo = core.RGB(0x22cc44) // green decal on gray wall
			d.DepthFade = 0
			pool.Spawn(d)

			var geom scene3d.DecalGeometry
			if pool.BuildGeometry(sc, &geom) == 0 {
				t.Fatal("expected projected decal geometry")
			}

			cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 6, 1, 0.1, 20)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

			var (
				decalRen  *scene3d.GPUDecalRenderer
				solidPipe *gpu.Pipeline
				wallVB    *gpu.Buffer
				camBuf    *render3d.CameraBuffer
			)

			opts := append([]nsgotest.Option{nsgotest.Size(viewSize, viewSize), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "decal-view",
					Style:       core.Style{Width: core.Pt(viewSize), Height: core.Pt(viewSize)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						// Use standalone internal camera buffer on GPUDecalRenderer too.
						decalRen, err = scene3d.NewGPUDecalRenderer(cx, dev, scene3d.GPUDecalOptions{
							Label:       "mobile_decals",
							DepthFormat: gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewGPUDecalRenderer: %v", err)
						}
						decalRen.WriteCamera(cam)
						if err := decalRen.Upload(&geom); err != nil {
							t.Fatalf("Upload: %v", err)
						}

						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						camBuf.WriteCamera(cam)
						solidPipe = newSolidPipeline(t, cx, dev, camBuf.Layout(), gpu.FormatDepth24Plus)
						wallVB, err = dev.CreateBuffer(cx, gpu.BufferDesc{
							Label: "wall",
							Usage: gpu.UsageVertex,
							Data:  packSolidQuad(wallTris, math3d.Vec3{Z: 1}, [4]float32{0.5, 0.5, 0.5, 1}),
						})
						if err != nil {
							t.Fatalf("CreateBuffer wall: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(solidPipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						pass.SetVertexBuffer(0, wallVB, 0)
						pass.Draw(uint32(len(wallTris)), 0)
						decalRen.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)

			img, err := app.GPUReadPixels("id:decal-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			// Corner of wall (outside decal box) is wall gray.
			expectColorNear(t, img, 6, 6, color.RGBA{R: 128, G: 128, B: 128, A: 255}, 6)
			// Center of view (inside decal box) is green decal on top of wall!
			expectColorNear(t, img, viewSize/2, viewSize/2, color.RGBA{R: 34, G: 204, B: 68, A: 255}, 6)
		})
	}
}

func TestDecalCornerAndAngleFadePixelReadback(t *testing.T) {
	// L-shaped corner: front wall facing +Z (0°), 55° bevel wall (partial fade),
	// and perpendicular side wall facing +X (90°, clipped away).
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	decalRen, err := scene3d.NewGPUDecalRenderer(nil, dev, scene3d.GPUDecalOptions{
		DepthFormat:  gpu.FormatDepth24Plus,
		CameraLayout: camBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewGPUDecalRenderer: %v", err)
	}
	t.Cleanup(func() { _ = decalRen.Close() })
	solidPipe := newSolidPipeline(t, nil, dev, camBuf.Layout(), gpu.FormatDepth24Plus)
	t.Cleanup(func() { _ = solidPipe.Close() })

	sc := scene3d.NewScene()
	// Flat wall on left half: x in [-2, 0], z = 0, normal = +Z.
	flatNode := scene3d.NewNode("flat")
	flatNode.SetPosition(math3d.Vec3{X: -1, Y: 0, Z: 0})
	flatTris := wallQuadTriangles(1, 2)
	flatNode.SetCollider(&scene3d.MeshCollider{Triangles: flatTris})

	// Tilted wall on right half: rotated 55° around Y around x = +1.
	tiltNode := scene3d.NewNode("tilt")
	tiltNode.SetPosition(math3d.Vec3{X: 1, Y: 0, Z: 0})
	tiltNode.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(55)))
	tiltNode.SetCollider(&scene3d.MeshCollider{Triangles: flatTris})

	// Perpendicular side wall (90°) right inside the decal box.
	perpNode := scene3d.NewNode("perp")
	perpNode.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)))
	perpNode.SetCollider(&scene3d.MeshCollider{Triangles: flatTris})

	_ = sc.Add(flatNode)
	_ = sc.Add(tiltNode)
	_ = sc.Add(perpNode)
	sc.UpdateTransforms()

	d := scene3d.NewDecal()
	d.HalfExtents = math3d.Vec3{X: 2.5, Y: 1.5, Z: 2.0}
	d.Albedo = core.RGB(0xff0000)
	d.AngleFadeStart = math3d.DegToRad(30)
	d.AngleFadeEnd = math3d.DegToRad(75)
	d.DepthFade = 0

	var geom scene3d.DecalGeometry
	scene3d.ProjectDecal(sc, d, &geom)
	if geom.TriangleCount() == 0 {
		t.Fatal("expected projected triangles")
	}
	// Verify perpendicular wall received no vertices (every vertex normal must have Z > 0.1).
	for i, v := range geom.Vertices {
		if v.Normal.Z < 0.1 {
			t.Errorf("vertex %d has perpendicular/back normal %+v", i, v.Normal)
		}
	}

	// Build solid vertex buffer for flat + tilt walls (gray 0.5, 0.5, 0.5).
	var solidData []byte
	for _, n := range []*scene3d.Node{flatNode, tiltNode} {
		m := n.WorldMatrix()
		worldN := m.TransformDir(math3d.Vec3{Z: 1}).Norm()
		worldTris := make([]math3d.Vec3, len(flatTris))
		for i, p := range flatTris {
			worldTris[i] = m.TransformPoint(p)
		}
		solidData = append(solidData, packSolidQuad(worldTris, worldN, [4]float32{0.5, 0.5, 0.5, 1})...)
	}
	solidVB, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "walls", Usage: gpu.UsageVertex, Data: solidData})
	if err != nil {
		t.Fatalf("CreateBuffer walls: %v", err)
	}
	t.Cleanup(func() { _ = solidVB.Close() })

	if err := decalRen.Upload(&geom); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x10141c),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreStore,
			ClearDepth:   1,
		},
	})
	pass.SetPipeline(solidPipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetVertexBuffer(0, solidVB, 0)
	pass.Draw(uint32(len(flatTris)*2), 0)
	decalRen.DrawWithCamera(pass, camBuf.BindGroup())
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	// Flat wall at x = -1 -> px = 16, py = 32: full red decal (255, 0, 0).
	flatPx := img.RGBAAt(16, 32)
	// Tilted 55° wall at x = +1 -> px = 48, py = 32: blended between gray (128,128,128) and red (255,0,0).
	tiltPx := img.RGBAAt(48, 32)

	if flatPx.R < 245 || flatPx.G > 10 {
		t.Errorf("flat wall pixel = %v, want full red", flatPx)
	}
	if !(tiltPx.R > 140 && tiltPx.R < 245 && tiltPx.G > 15 && tiltPx.G < 115) {
		t.Errorf("55° tilted wall pixel = %v, want smooth alpha blend between gray(128,128,128) and red(255,0,0)", tiltPx)
	}
}

type customTriReceiver struct {
	tris [][3]math3d.Vec3
}

func (c customTriReceiver) DecalTriangles(emit func(p0, p1, p2, n0, n1, n2 math3d.Vec3) bool) {
	var zero math3d.Vec3
	for _, tri := range c.tris {
		if !emit(tri[0], tri[1], tri[2], zero, zero, zero) {
			return
		}
	}
}

func TestDecalEdgeCasesAndHelpers(t *testing.T) {
	// Mirrored scale keeps outward normals valid.
	sc := scene3d.NewScene()
	mirrored := scene3d.NewNode("mirrored")
	mirrored.SetScale(math3d.Vec3{X: -1, Y: 1, Z: 1})
	mirrored.SetCollider(&scene3d.MeshCollider{Triangles: wallQuadTriangles(2, 2)})
	_ = sc.Add(mirrored)
	sc.UpdateTransforms()

	d := scene3d.NewDecal()
	d.HalfExtents = math3d.Vec3{X: 1, Y: 1, Z: 1}
	var geom scene3d.DecalGeometry
	if tris := scene3d.ProjectDecal(sc, d, &geom); tris == 0 {
		t.Fatal("mirrored node should still receive front-facing decal")
	}
	if geom.Bounds().Empty() {
		t.Error("expected non-empty geometry bounds")
	}

	// Custom DecalReceiver via BuildReceiverGeometry & pool helpers.
	pool := scene3d.NewDecalPool(4)
	d2 := scene3d.NewDecal()
	d2.LayerMask = 1 << 3
	h := pool.Spawn(d2)
	var visited int
	pool.ForEach(func(got scene3d.DecalHandle, _ *scene3d.Decal) bool {
		if got == h {
			visited++
		}
		return true
	})
	if visited != 1 {
		t.Errorf("ForEach visited = %d, want 1", visited)
	}

	recv := customTriReceiver{tris: [][3]math3d.Vec3{
		{{X: -0.4, Y: -0.4, Z: 0}, {X: 0.4, Y: -0.4, Z: 0}, {X: 0, Y: 0.4, Z: 0}},
	}}
	if tris := pool.BuildReceiverGeometry(recv, math3d.Mat4Identity(), 1<<3, &geom); tris != 1 {
		t.Errorf("BuildReceiverGeometry matching layer = %d tris, want 1", tris)
	}
	if tris := pool.BuildReceiverGeometry(recv, math3d.Mat4Identity(), 1<<0, &geom); tris != 0 {
		t.Errorf("BuildReceiverGeometry excluded layer = %d tris, want 0", tris)
	}
	if !pool.Remove(h) || pool.Remove(h) {
		t.Error("Remove should succeed once then return false")
	}

	// Vertex/Index binary serialization size invariants.
	d3 := scene3d.NewDecal()
	geom.Reset()
	scene3d.ProjectDecalOntoReceiver(d3, recv, math3d.Mat4Identity(), &geom)
	vBytes := geom.AppendVertexData(nil)
	iBytes := geom.AppendIndexData(nil)
	if len(vBytes) != geom.VertexDataSize() || len(vBytes)%scene3d.DecalVertexStride != 0 {
		t.Errorf("vertex bytes len = %d, want %d", len(vBytes), geom.VertexDataSize())
	}
	if len(iBytes) != geom.IndexDataSize() || len(iBytes)%4 != 0 {
		t.Errorf("index bytes len = %d, want %d (4-byte aligned)", len(iBytes), geom.IndexDataSize())
	}

	// Non-finite decal fields are rejected cleanly without NaN output.
	bad := scene3d.NewDecal()
	bad.Transform.Pos.X = float32(math.NaN())
	geom.Reset()
	if tris := scene3d.ProjectDecalOntoReceiver(bad, recv, math3d.Mat4Identity(), &geom); tris != 0 {
		t.Errorf("NaN decal produced %d triangles, want 0", tris)
	}
	if !bad.WorldBounds().Empty() {
		t.Error("NaN decal WorldBounds should be empty")
	}
}

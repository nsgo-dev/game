package render3d_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func testCamera() (viewProj math3d.Mat4, camPos math3d.Vec3) {
	camPos = math3d.Vec3{X: 0, Y: 0, Z: 10}
	view := math3d.LookAt(camPos, math3d.Vec3Zero(), math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 100)
	return proj.Mul(view), camPos
}

func unitBoxAt(pos math3d.Vec3, half float32) math3d.AABB {
	h := math3d.Vec3{X: half, Y: half, Z: half}
	return math3d.AABB{Min: pos.Sub(h), Max: pos.Add(h)}
}

func TestFrustumCulling(t *testing.T) {
	vp, camPos := testCamera()
	items := []render3d.DrawItem{
		{ID: 1, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Translate(math3d.Vec3{Z: 0}), WorldBounds: unitBoxAt(math3d.Vec3{Z: 0}, 1), Triangles: 100, Vertices: 60},
		{ID: 2, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Translate(math3d.Vec3{Z: -20}), WorldBounds: unitBoxAt(math3d.Vec3{Z: -20}, 1), Triangles: 100, Vertices: 60},
		// Behind camera (camera is at z=10 looking down -Z).
		{ID: 3, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Translate(math3d.Vec3{Z: 25}), WorldBounds: unitBoxAt(math3d.Vec3{Z: 25}, 1), Triangles: 100, Vertices: 60},
		// Far to the right outside frustum.
		{ID: 4, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Translate(math3d.Vec3{X: 500}), WorldBounds: unitBoxAt(math3d.Vec3{X: 500}, 1), Triangles: 100, Vertices: 60},
		// Beyond far plane (z < 10 - 100 = -90).
		{ID: 5, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Translate(math3d.Vec3{Z: -150}), WorldBounds: unitBoxAt(math3d.Vec3{Z: -150}, 1), Triangles: 100, Vertices: 60},
		// Empty AABB should be culled.
		{ID: 6, MeshID: 1, MaterialID: 1, PipelineID: 1, Transform: math3d.Mat4Identity(), WorldBounds: math3d.AABBEmpty(), Triangles: 100, Vertices: 60},
	}

	var q render3d.Queue
	plan := q.Build(vp, camPos, items)

	if plan.Stats.TotalItems != 6 {
		t.Fatalf("TotalItems = %d, want 6", plan.Stats.TotalItems)
	}
	if plan.Stats.VisibleItems != 2 {
		t.Fatalf("VisibleItems = %d, want 2", plan.Stats.VisibleItems)
	}
	if plan.Stats.CulledItems != 4 {
		t.Fatalf("CulledItems = %d, want 4", plan.Stats.CulledItems)
	}
	if len(plan.Opaque) != 2 || plan.Opaque[0].ID != 1 || plan.Opaque[1].ID != 2 {
		t.Fatalf("visible IDs = %+v, want [1, 2]", plan.Opaque)
	}

	// Verify OpenGL [-1, 1] frustum mode (FrustumNO) works identically with PerspectiveNO.
	view := math3d.LookAt(camPos, math3d.Vec3Zero(), math3d.Vec3Up())
	vpNO := math3d.PerspectiveNO(math3d.DegToRad(60), 16.0/9.0, 0.1, 100).Mul(view)
	qNO := render3d.Queue{FrustumNO: true}
	planNO := qNO.Build(vpNO, camPos, items)
	if planNO.Stats.VisibleItems != 2 || planNO.Stats.CulledItems != 4 {
		t.Fatalf("FrustumNO visible/culled = %d/%d, want 2/4", planNO.Stats.VisibleItems, planNO.Stats.CulledItems)
	}
}

func TestFrustumKeepsNearGeometryWithNOProjections(t *testing.T) {
	// #546: a [0, 1] frustum extraction of an OpenGL [-1, 1] projection puts
	// the near plane halfway into the view and culls visible geometry. The
	// conservative [-1, 1] extraction must keep all three boxes.
	items := []render3d.DrawItem{
		{ID: 1, MeshID: 1, MaterialID: 1, PipelineID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -1}, 0.5), Triangles: 12, Vertices: 8},
		{ID: 2, MeshID: 1, MaterialID: 1, PipelineID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -50}, 0.5), Triangles: 12, Vertices: 8},
		{ID: 3, MeshID: 1, MaterialID: 1, PipelineID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -90}, 0.5), Triangles: 12, Vertices: 8},
	}
	ortho := math3d.OrthoNO(-2, 2, -2, 2, 0.1, 100)
	var q render3d.Queue
	if plan := q.Build(ortho, math3d.Vec3Zero(), items); plan.Stats.VisibleItems != 3 {
		t.Fatalf("OrthoNO with default Queue: visible = %d, want 3", plan.Stats.VisibleItems)
	}
	perspective := math3d.PerspectiveNO(math3d.DegToRad(60), 1, 0.1, 100)
	if plan := q.Build(perspective, math3d.Vec3Zero(), items); plan.Stats.VisibleItems != 3 {
		t.Fatalf("PerspectiveNO with default Queue: visible = %d, want 3", plan.Stats.VisibleItems)
	}
}

func TestLODOverrideKeepsTheOtherCount(t *testing.T) {
	// #546: a level that sets only Triangles must not zero the base Vertices.
	vp, camPos := testCamera()
	item := render3d.DrawItem{
		ID: 1, MeshID: 1, MaterialID: 1, PipelineID: 1,
		WorldBounds: unitBoxAt(math3d.Vec3{Z: -10}, 1),
		Triangles:   100, Vertices: 200,
		LODs: []render3d.LODLevel{{MeshID: 2, Triangles: 50}},
	}
	var q render3d.Queue
	plan := q.Build(vp, camPos, []render3d.DrawItem{item})
	if len(plan.Opaque) != 1 {
		t.Fatalf("visible = %d, want 1", len(plan.Opaque))
	}
	if got := plan.Opaque[0].Triangles; got != 50 {
		t.Errorf("Triangles = %d, want 50", got)
	}
	if got := plan.Opaque[0].Vertices; got != 200 {
		t.Errorf("Vertices = %d, want the base 200", got)
	}
	if plan.Stats.Vertices != 200 {
		t.Errorf("stats Vertices = %d, want 200", plan.Stats.Vertices)
	}
}

func TestRenderStatsAddKeepsResidentMemory(t *testing.T) {
	// Resident memory is per target, not per pass; summing it double-counts.
	a := render3d.RenderStats{TextureBytes: 1 << 20, BufferBytes: 1 << 19}
	b := render3d.RenderStats{TextureBytes: 1 << 20, BufferBytes: 1 << 19}
	sum := a.Add(b)
	if sum.TextureBytes != 1<<20 || sum.BufferBytes != 1<<19 {
		t.Errorf("resident memory = tex:%d buf:%d, want 1MB/512KB", sum.TextureBytes, sum.BufferBytes)
	}
	if sum.VisibleItems != 0 {
		t.Errorf("VisibleItems = %d, want 0", sum.VisibleItems)
	}
}

func TestProjectScreenRatioBehindCenterIsFinestLOD(t *testing.T) {
	// A giant object whose center is at or behind the camera must not read as
	// zero and pick the coarsest LOD; the frustum culls wholly behind geometry.
	vp, _ := testCamera()
	bounds := unitBoxAt(math3d.Vec3{Z: 5}, 50)
	if got := render3d.ProjectScreenRatio(bounds, vp); got != 1 {
		t.Errorf("ProjectScreenRatio = %v, want 1", got)
	}
}

func TestTransparentSortUsesViewDepthOffAxis(t *testing.T) {
	// #546: Euclidean distance orders these wrongly; clip-space depth does not.
	vp, camPos := testCamera()
	items := []render3d.DrawItem{
		// Viewed 10 units deep but far off axis.
		{ID: 1, MeshID: 1, MaterialID: 1, PipelineID: 1, Blend: render3d.BlendAlphaBlend,
			WorldBounds: unitBoxAt(math3d.Vec3{X: 10, Z: -5}, 0.2), Triangles: 12, Vertices: 8},
		// On axis but 2 units deeper; Euclidean distance is smaller.
		{ID: 2, MeshID: 1, MaterialID: 1, PipelineID: 1, Blend: render3d.BlendAlphaBlend,
			WorldBounds: unitBoxAt(math3d.Vec3{Z: -7}, 0.2), Triangles: 12, Vertices: 8},
	}
	var q render3d.Queue
	plan := q.Build(vp, camPos, items)
	if len(plan.Transparent) != 2 {
		t.Fatalf("transparent = %d, want 2", len(plan.Transparent))
	}
	if plan.Transparent[0].ID != 2 || plan.Transparent[1].ID != 1 {
		t.Fatalf("transparent order = [%d %d], want [2 1]", plan.Transparent[0].ID, plan.Transparent[1].ID)
	}
}

func TestOpaqueSortingAndStateSwitchMinimization(t *testing.T) {
	vp, camPos := testCamera()
	// Deliberately interleaved pipelines, materials, meshes, and depths.
	items := []render3d.DrawItem{
		{ID: 1, PipelineID: 2, MaterialID: 20, MeshID: 2, WorldBounds: unitBoxAt(math3d.Vec3{Z: -5}, 0.5), Triangles: 10, Vertices: 12},
		{ID: 2, PipelineID: 1, MaterialID: 10, MeshID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -30}, 0.5), Triangles: 10, Vertices: 12},
		{ID: 3, PipelineID: 2, MaterialID: 10, MeshID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -10}, 0.5), Triangles: 10, Vertices: 12},
		{ID: 4, PipelineID: 1, MaterialID: 10, MeshID: 1, WorldBounds: unitBoxAt(math3d.Vec3{Z: -2}, 0.5), Triangles: 10, Vertices: 12},
		{ID: 5, PipelineID: 1, MaterialID: 20, MeshID: 3, WorldBounds: unitBoxAt(math3d.Vec3{Z: -15}, 0.5), Triangles: 10, Vertices: 12},
		{ID: 6, PipelineID: 1, MaterialID: 10, MeshID: 1, Blend: render3d.BlendAlphaMask, WorldBounds: unitBoxAt(math3d.Vec3{Z: -8}, 0.5), Triangles: 10, Vertices: 12},
	}

	var q render3d.Queue
	plan := q.Build(vp, camPos, items)

	if len(plan.Opaque) != 6 {
		t.Fatalf("len(Opaque) = %d, want 6", len(plan.Opaque))
	}

	// Expected order:
	// (P=1, M=10, Mesh=1, Opaque, near ID=4)
	// (P=1, M=10, Mesh=1, Opaque, far  ID=2)
	// (P=1, M=10, Mesh=1, AlphaMask,   ID=6)
	// (P=1, M=20, Mesh=3, Opaque,      ID=5)
	// (P=2, M=10, Mesh=1, Opaque,      ID=3)
	// (P=2, M=20, Mesh=2, Opaque,      ID=1)
	wantIDs := []uint64{4, 2, 6, 5, 3, 1}
	for i, want := range wantIDs {
		if plan.Opaque[i].ID != want {
			t.Fatalf("Opaque[%d].ID = %d, want %d (full order: %v)", i, plan.Opaque[i].ID, want, plan.Opaque)
		}
	}

	// Front-to-back check within identical state group (ID=4 at z=-2 before ID=2 at z=-30).
	if !(plan.Opaque[0].DistanceSq < plan.Opaque[1].DistanceSq) {
		t.Fatalf("expected front-to-back order: %v vs %v", plan.Opaque[0].DistanceSq, plan.Opaque[1].DistanceSq)
	}

	// Minimal pipeline switches: P=1 -> P=2 is exactly 1 switch.
	if plan.Stats.PipelineSwitches != 1 {
		t.Fatalf("PipelineSwitches = %d, want 1", plan.Stats.PipelineSwitches)
	}
	// Batches are: (1,10,1,Opaque)[2], (1,10,1,Mask)[1], (1,20,3)[1], (2,10,1)[1], (2,20,2)[1].
	// Material transitions: 10 -> 10 (0), 10 -> 20 (+1), 20 -> 10 (+1), 10 -> 20 (+1) = 3.
	if plan.Stats.MaterialSwitches != 3 {
		t.Fatalf("MaterialSwitches = %d, want 3", plan.Stats.MaterialSwitches)
	}
	if plan.Stats.InstancedBatches != 5 {
		t.Fatalf("InstancedBatches = %d, want 5", plan.Stats.InstancedBatches)
	}
}

func TestTransparentBackToFrontSorting(t *testing.T) {
	vp, camPos := testCamera()
	items := []render3d.DrawItem{
		{ID: 1, PipelineID: 1, MaterialID: 1, MeshID: 1, Blend: render3d.BlendAlphaBlend, WorldBounds: unitBoxAt(math3d.Vec3{Z: 5}, 0.5), Triangles: 2, Vertices: 4},   // dist = 5
		{ID: 2, PipelineID: 2, MaterialID: 2, MeshID: 2, Blend: render3d.BlendAlphaBlend, WorldBounds: unitBoxAt(math3d.Vec3{Z: -40}, 0.5), Triangles: 2, Vertices: 4}, // dist = 50
		{ID: 3, PipelineID: 1, MaterialID: 1, MeshID: 1, Blend: render3d.BlendAlphaBlend, WorldBounds: unitBoxAt(math3d.Vec3{Z: -10}, 0.5), Triangles: 2, Vertices: 4}, // dist = 20
		{ID: 4, PipelineID: 3, MaterialID: 3, MeshID: 3, Blend: render3d.BlendAlphaBlend, WorldBounds: unitBoxAt(math3d.Vec3{Z: 0}, 0.5), Triangles: 2, Vertices: 4},   // dist = 10
	}

	var q render3d.Queue
	plan := q.Build(vp, camPos, items)

	if len(plan.Opaque) != 0 || len(plan.Batches) != 0 {
		t.Fatalf("expected no opaque draws, got %d/%d", len(plan.Opaque), len(plan.Batches))
	}
	if len(plan.Transparent) != 4 {
		t.Fatalf("len(Transparent) = %d, want 4", len(plan.Transparent))
	}

	// Strictly back-to-front regardless of PipelineID/MaterialID: 2 (50) -> 3 (20) -> 4 (10) -> 1 (5).
	wantIDs := []uint64{2, 3, 4, 1}
	for i, want := range wantIDs {
		if plan.Transparent[i].ID != want {
			t.Fatalf("Transparent[%d].ID = %d, want %d", i, plan.Transparent[i].ID, want)
		}
		if i > 0 && plan.Transparent[i-1].DistanceSq < plan.Transparent[i].DistanceSq {
			t.Fatalf("Transparent not strictly back-to-front at %d", i)
		}
	}
	if plan.Stats.TotalDrawCalls != 4 {
		t.Fatalf("TotalDrawCalls = %d, want 4", plan.Stats.TotalDrawCalls)
	}
}

func TestInstancingAndSteadyStateZeroAllocs(t *testing.T) {
	vp, camPos := testCamera()
	const count = 100
	items := make([]render3d.DrawItem, count)
	for i := range count {
		pos := math3d.Vec3{X: float32(i%10) - 4.5, Y: 0, Z: -float32(i/10)*2 - 2}
		items[i] = render3d.DrawItem{
			ID:          uint64(i + 1),
			PipelineID:  7,
			MaterialID:  3,
			MeshID:      42,
			Blend:       render3d.BlendOpaque,
			Transform:   math3d.Translate(pos),
			WorldBounds: unitBoxAt(pos, 0.4),
			Triangles:   120,
			Vertices:    80,
		}
	}

	var q render3d.Queue
	plan := q.Build(vp, camPos, items)

	if plan.Stats.VisibleItems != count || plan.Stats.OpaqueDraws != count {
		t.Fatalf("visible/opaque = %d/%d, want %d", plan.Stats.VisibleItems, plan.Stats.OpaqueDraws, count)
	}
	if plan.Stats.InstancedBatches != 1 || plan.Stats.TotalDrawCalls != 1 {
		t.Fatalf("batches/drawCalls = %d/%d, want 1/1", plan.Stats.InstancedBatches, plan.Stats.TotalDrawCalls)
	}
	if len(plan.Batches) != 1 || len(plan.Batches[0].Transforms) != count {
		t.Fatalf("batch transforms = %d, want %d", len(plan.Batches[0].Transforms), count)
	}
	if plan.Stats.Triangles != count*120 || plan.Stats.Vertices != count*80 {
		t.Fatalf("tris/verts = %d/%d, want %d/%d", plan.Stats.Triangles, plan.Stats.Vertices, count*120, count*80)
	}
	if plan.Stats.PipelineSwitches != 0 || plan.Stats.MaterialSwitches != 0 {
		t.Fatalf("switches = pipe:%d mat:%d, want 0/0", plan.Stats.PipelineSwitches, plan.Stats.MaterialSwitches)
	}

	// Verify DisableInstancing produces 100 individual batches.
	qNoInst := render3d.Queue{DisableInstancing: true}
	planNoInst := qNoInst.Build(vp, camPos, items)
	if planNoInst.Stats.InstancedBatches != count || planNoInst.Stats.TotalDrawCalls != count {
		t.Fatalf("DisableInstancing batches/draws = %d/%d, want %d", planNoInst.Stats.InstancedBatches, planNoInst.Stats.TotalDrawCalls, count)
	}

	// Verify 0 allocs/op in steady state after warmup.
	_ = q.Build(vp, camPos, items)
	allocs := testing.AllocsPerRun(50, func() {
		_ = q.Build(vp, camPos, items)
	})
	if allocs != 0 {
		t.Fatalf("Queue.Build steady-state allocs = %v, want 0", allocs)
	}

	// Verify 0 allocs/op in steady state with LOD hysteresis enabled too.
	levels := []render3d.LODLevel{
		{MeshID: 42, MaxDistance: 25, Triangles: 120, Vertices: 80},
		{MeshID: 43, MaxDistance: 0, Triangles: 30, Vertices: 20},
	}
	for i := range items {
		items[i].LODs = levels
	}
	q.LODHysteresis = 0.1
	_ = q.Build(vp, camPos, items) // warmup map + buffers
	lodAllocs := testing.AllocsPerRun(50, func() {
		_ = q.Build(vp, camPos, items)
	})
	if lodAllocs != 0 {
		t.Fatalf("Queue.Build steady-state LOD+hysteresis allocs = %v, want 0", lodAllocs)
	}
}

func TestLODSelectionAndHysteresis(t *testing.T) {
	levels := []render3d.LODLevel{
		{MeshID: 100, MaxDistance: 20, Triangles: 2000, Vertices: 1200},
		{MeshID: 101, MaxDistance: 50, Triangles: 600, Vertices: 400},
		{MeshID: 102, MaxDistance: 0, Triangles: 120, Vertices: 90}, // coarsest fallback
	}

	if got := render3d.SelectLOD(nil, 10, 0.5, 0, -1); got != -1 {
		t.Fatalf("empty levels = %d, want -1", got)
	}
	if got := render3d.SelectLOD(levels, 10, -1, 0, -1); got != 0 {
		t.Fatalf("dist 10 = %d, want 0", got)
	}
	if got := render3d.SelectLOD(levels, 35, -1, 0, -1); got != 1 {
		t.Fatalf("dist 35 = %d, want 1", got)
	}
	if got := render3d.SelectLOD(levels, 80, -1, 0, -1); got != 2 {
		t.Fatalf("dist 80 = %d, want 2", got)
	}

	// Hysteresis 10% around LOD0 MaxDistance=20 (outward band 22, inward band 18).
	const h = 0.10
	// Just past 20 (21 <= 22): should stay at LOD0 (no popping).
	if got := render3d.SelectLOD(levels, 21, -1, h, 0); got != 0 {
		t.Fatalf("hysteresis outward 21 from 0 = %d, want 0", got)
	}
	// Past 22 (22.5 > 22): should degrade to LOD1.
	if got := render3d.SelectLOD(levels, 22.5, -1, h, 0); got != 1 {
		t.Fatalf("hysteresis outward 22.5 from 0 = %d, want 1", got)
	}
	// Jittering back inside 20 (19 > 18): should stay at LOD1 (no popping).
	if got := render3d.SelectLOD(levels, 19, -1, h, 1); got != 1 {
		t.Fatalf("hysteresis inward 19 from 1 = %d, want 1", got)
	}
	// Clearly inside 18 (17 <= 18): should upgrade back to LOD0.
	if got := render3d.SelectLOD(levels, 17, -1, h, 1); got != 0 {
		t.Fatalf("hysteresis inward 17 from 1 = %d, want 0", got)
	}

	// Screen-ratio-driven LOD selection with hysteresis.
	ratioLevels := []render3d.LODLevel{
		{MeshID: 200, MinScreenRatio: 0.25, Triangles: 1000, Vertices: 600},
		{MeshID: 201, MinScreenRatio: 0.08, Triangles: 300, Vertices: 200},
		{MeshID: 202, MinScreenRatio: 0, Triangles: 50, Vertices: 40},
	}
	if got := render3d.SelectLOD(ratioLevels, -1, 0.30, 0, -1); got != 0 {
		t.Fatalf("ratio 0.30 = %d, want 0", got)
	}
	// 0.23 >= 0.25*(1-0.1) = 0.225 -> stays at LOD0.
	if got := render3d.SelectLOD(ratioLevels, -1, 0.23, h, 0); got != 0 {
		t.Fatalf("ratio hysteresis 0.23 from 0 = %d, want 0", got)
	}
	// 0.20 < 0.225 -> drops to LOD1.
	if got := render3d.SelectLOD(ratioLevels, -1, 0.20, h, 0); got != 1 {
		t.Fatalf("ratio hysteresis 0.20 from 0 = %d, want 1", got)
	}
	// 0.26 < 0.25*(1+0.1) = 0.275 -> stays at LOD1.
	if got := render3d.SelectLOD(ratioLevels, -1, 0.26, h, 1); got != 1 {
		t.Fatalf("ratio hysteresis 0.26 from 1 = %d, want 1", got)
	}
	// 0.28 >= 0.275 -> upgrades to LOD0.
	if got := render3d.SelectLOD(ratioLevels, -1, 0.28, h, 1); got != 0 {
		t.Fatalf("ratio hysteresis 0.28 from 1 = %d, want 0", got)
	}

	// Verify ProjectScreenRatio mathematically: 90 deg FOV, sphere radius r at dist d => ratio = r / d.
	// A box of half-extents (h, h, h) has bounding sphere radius sqrt(3)*h.
	proj90 := math3d.Perspective(math3d.DegToRad(90), 1.0, 0.1, 100)
	view := math3d.LookAt(math3d.Vec3Zero(), math3d.Vec3Forward(), math3d.Vec3Up())
	vp90 := proj90.Mul(view)
	half := float32(1.0 / math.Sqrt(3)) // bounding sphere radius == 1.0
	gotRatio := render3d.ProjectScreenRatio(unitBoxAt(math3d.Vec3{Z: -10}, half), vp90)
	if !math3d.ApproxEqual(gotRatio, 0.1, 1e-3) {
		t.Fatalf("ProjectScreenRatio = %v, want ~0.1", gotRatio)
	}

	// Verify LOD integration inside Queue.Build (including hysteresis & per-LOD batching).
	vp, camPos := testCamera()
	q := render3d.Queue{LODHysteresis: 0.1}
	nearItem := render3d.DrawItem{ID: 1, PipelineID: 1, MaterialID: 1, MeshID: 999, WorldBounds: unitBoxAt(math3d.Vec3{Z: 0}, 1), LODs: levels}  // dist = 10 -> LOD0
	midItem := render3d.DrawItem{ID: 2, PipelineID: 1, MaterialID: 1, MeshID: 999, WorldBounds: unitBoxAt(math3d.Vec3{Z: -25}, 1), LODs: levels} // dist = 35 -> LOD1
	farItem := render3d.DrawItem{ID: 3, PipelineID: 1, MaterialID: 1, MeshID: 999, WorldBounds: unitBoxAt(math3d.Vec3{Z: -60}, 1), LODs: levels} // dist = 70 -> LOD2

	plan := q.Build(vp, camPos, []render3d.DrawItem{nearItem, midItem, farItem})
	if len(plan.Opaque) != 3 {
		t.Fatalf("len(Opaque) = %d, want 3", len(plan.Opaque))
	}
	if plan.Opaque[0].MeshID != 100 || plan.Opaque[1].MeshID != 101 || plan.Opaque[2].MeshID != 102 {
		t.Fatalf("selected LOD meshes = [%d, %d, %d], want [100, 101, 102]",
			plan.Opaque[0].MeshID, plan.Opaque[1].MeshID, plan.Opaque[2].MeshID)
	}
	wantTris := uint64(2000 + 600 + 120)
	if plan.Stats.Triangles != wantTris {
		t.Fatalf("LOD Triangles = %d, want %d", plan.Stats.Triangles, wantTris)
	}

	// Move nearItem just across the 20m boundary (dist = 21 -> z = -11): hysteresis keeps LOD0!
	nearItem.WorldBounds = unitBoxAt(math3d.Vec3{Z: -11}, 1)
	plan2 := q.Build(vp, camPos, []render3d.DrawItem{nearItem})
	if plan2.Opaque[0].LODIndex != 0 || plan2.Opaque[0].MeshID != 100 {
		t.Fatalf("expected hysteresis to retain LOD0 at dist=21, got LOD%d (mesh %d)",
			plan2.Opaque[0].LODIndex, plan2.Opaque[0].MeshID)
	}

	// ResetLODHistory drops memory so dist=21 immediately picks LOD1.
	q.ResetLODHistory()
	plan3 := q.Build(vp, camPos, []render3d.DrawItem{nearItem})
	if plan3.Opaque[0].LODIndex != 1 || plan3.Opaque[0].MeshID != 101 {
		t.Fatalf("after ResetLODHistory at dist=21, got LOD%d, want LOD1", plan3.Opaque[0].LODIndex)
	}
}

func TestRenderStatsAndBudgets(t *testing.T) {
	mobile := render3d.MobileBudget()
	desktop := render3d.DesktopBudget()
	if !(mobile.MaxDrawCalls < desktop.MaxDrawCalls && mobile.MaxTriangles < desktop.MaxTriangles && mobile.MaxTextureBytes < desktop.MaxTextureBytes) {
		t.Fatalf("expected MobileBudget stricter than DesktopBudget: %+v vs %+v", mobile, desktop)
	}

	okStats := render3d.RenderStats{
		TotalItems:       200,
		CulledItems:      80,
		VisibleItems:     120,
		OpaqueDraws:      110,
		TransparentDraws: 10,
		InstancedBatches: 30,
		TotalDrawCalls:   40,
		Triangles:        50_000,
		Vertices:         80_000,
		MaterialSwitches: 20,
		PipelineSwitches: 5,
		TextureBytes:     64 * 1024 * 1024,
		BufferBytes:      16 * 1024 * 1024,
	}
	if violations := render3d.CheckBudget(okStats, mobile); len(violations) != 0 {
		t.Fatalf("expected no violations for okStats, got %v", violations)
	}
	if !strings.Contains(okStats.Summary(), "draws=40") {
		t.Fatalf("unexpected Summary: %s", okStats.Summary())
	}

	// Exceed draw calls, triangles, and texture memory on MobileBudget.
	heavy := okStats
	heavy.TotalDrawCalls = mobile.MaxDrawCalls + 25
	heavy.Triangles = mobile.MaxTriangles + 10_000
	heavy.TextureBytes = mobile.MaxTextureBytes + 32*1024*1024

	violations := render3d.CheckBudget(heavy, mobile)
	if len(violations) != 3 {
		t.Fatalf("got %d violations (%v), want 3", len(violations), violations)
	}
	wantMetrics := []string{"draw_calls", "triangles", "texture_bytes"}
	for i, m := range wantMetrics {
		if violations[i].Metric != m {
			t.Fatalf("violation[%d].Metric = %q, want %q", i, violations[i].Metric, m)
		}
		if !strings.Contains(violations[i].String(), m) {
			t.Fatalf("violation String() missing metric name: %q", violations[i].String())
		}
	}

	// Same heavy stats should still pass DesktopBudget.
	if v := render3d.CheckBudget(heavy, desktop); len(v) != 0 {
		t.Fatalf("expected heavy stats to pass DesktopBudget, got %v", v)
	}

	// Verify Add aggregates two passes cleanly.
	sum := okStats.Add(okStats)
	if sum.TotalDrawCalls != okStats.TotalDrawCalls*2 || sum.Triangles != okStats.Triangles*2 {
		t.Fatalf("RenderStats.Add mismatch: %+v", sum)
	}
}

func TestGizmoSegmentsPickingAndDragging(t *testing.T) {
	var g render3d.Gizmo
	origin := math3d.Vec3{X: 1, Y: 2, Z: 3}
	const scale = float32(2.0)

	// Segments generation for all 3 modes.
	if segs := g.Segments(origin, scale, render3d.GizmoTranslate); len(segs) == 0 {
		t.Fatal("GizmoTranslate emitted 0 segments")
	}
	if segs := g.Segments(origin, scale, render3d.GizmoRotate); len(segs) != 32*3 {
		t.Fatalf("GizmoRotate emitted %d segments, want %d", len(segs), 32*3)
	}
	if segs := g.Segments(origin, scale, render3d.GizmoScale); len(segs) == 0 {
		t.Fatal("GizmoScale emitted 0 segments")
	}

	// Ray picking in Translate mode: aim directly at midpoints of X, Y, Z shafts and XY plane.
	pickCases := []struct {
		name string
		mode render3d.GizmoMode
		ray  math3d.Ray
		want render3d.GizmoAxis
	}{
		{
			name: "translate X",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 1.0, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisX,
		},
		{
			name: "translate Y",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{Y: 1.0, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisY,
		},
		{
			name: "translate Z",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 5, Z: 1.0}), Dir: math3d.Vec3{X: -1}},
			want: render3d.AxisZ,
		},
		{
			name: "translate XY plane",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 0.45, Y: 0.45, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisXY,
		},
		{
			name: "translate center AxisAll",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisAll,
		},
		{
			name: "translate miss",
			mode: render3d.GizmoTranslate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 10, Y: 10, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisNone,
		},
		{
			name: "rotate X ring (YZ plane)",
			mode: render3d.GizmoRotate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 5, Y: scale * 0.7071, Z: scale * 0.7071}), Dir: math3d.Vec3{X: -1}},
			want: render3d.AxisX,
		},
		{
			name: "rotate Y ring (XZ plane)",
			mode: render3d.GizmoRotate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: scale * 0.7071, Y: 5, Z: scale * 0.7071}), Dir: math3d.Vec3{Y: -1}},
			want: render3d.AxisY,
		},
		{
			name: "rotate Z ring (XY plane)",
			mode: render3d.GizmoRotate,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: scale * 0.7071, Y: scale * 0.7071, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisZ,
		},
		{
			name: "scale center AxisAll",
			mode: render3d.GizmoScale,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisAll,
		},
		{
			name: "scale X handle",
			mode: render3d.GizmoScale,
			ray:  math3d.Ray{Origin: origin.Add(math3d.Vec3{X: 1.5, Z: 5}), Dir: math3d.Vec3{Z: -1}},
			want: render3d.AxisX,
		},
	}

	for _, tc := range pickCases {
		t.Run(tc.name, func(t *testing.T) {
			axis, hitT := g.PickAxis(tc.ray, origin, scale, tc.mode)
			if axis != tc.want {
				t.Fatalf("PickAxis = %v (t=%v), want %v", axis, hitT, tc.want)
			}
			if tc.want != render3d.AxisNone && hitT <= 0 {
				t.Fatalf("expected positive hitT, got %v", hitT)
			}
		})
	}

	// DragTranslate along X without and with grid snap.
	rStart := math3d.Ray{Origin: math3d.Vec3{X: 1, Y: 2, Z: 10}, Dir: math3d.Vec3{Z: -1}}
	rEnd := math3d.Ray{Origin: math3d.Vec3{X: 3.3, Y: 4.7, Z: 10}, Dir: math3d.Vec3{Z: -1}}
	dx := g.DragTranslate(origin, render3d.AxisX, rStart, rEnd, 0)
	if !dx.ApproxEqual(math3d.Vec3{X: 2.3, Y: 0, Z: 0}, eps) {
		t.Fatalf("DragTranslate AxisX = %v, want (2.3, 0, 0)", dx)
	}
	dxSnap := g.DragTranslate(origin, render3d.AxisX, rStart, rEnd, 0.5)
	if !dxSnap.ApproxEqual(math3d.Vec3{X: 2.5, Y: 0, Z: 0}, eps) {
		t.Fatalf("DragTranslate AxisX snap=0.5 = %v, want (2.5, 0, 0)", dxSnap)
	}

	// DragTranslate planar XY constrains Z to 0.
	dxy := g.DragTranslate(origin, render3d.AxisXY, rStart, rEnd, 0.5)
	if !dxy.ApproxEqual(math3d.Vec3{X: 2.5, Y: 2.5, Z: 0}, eps) {
		t.Fatalf("DragTranslate AxisXY snap=0.5 = %v, want (2.5, 2.5, 0)", dxy)
	}

	// DragRotate around +Z from +X to +Y should yield +pi/2 radians.
	rotOrigin := math3d.Vec3Zero()
	rotStart := math3d.Ray{Origin: math3d.Vec3{X: 2, Y: 0, Z: 5}, Dir: math3d.Vec3{Z: -1}}
	rotQuarter := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 2, Z: 5}, Dir: math3d.Vec3{Z: -1}}
	ang := g.DragRotate(rotOrigin, render3d.AxisZ, rotStart, rotQuarter, 0)
	if !math3d.ApproxEqual(ang, math.Pi/2, eps) {
		t.Fatalf("DragRotate AxisZ = %v, want pi/2", ang)
	}

	// Angle snapping to 15 degrees: 22 deg -> 15 deg.
	rad22 := math3d.DegToRad(22)
	rot22 := math3d.Ray{
		Origin: math3d.Vec3{X: 2 * float32(math.Cos(float64(rad22))), Y: 2 * float32(math.Sin(float64(rad22))), Z: 5},
		Dir:    math3d.Vec3{Z: -1},
	}
	snap15 := math3d.DegToRad(15)
	angSnap := g.DragRotate(rotOrigin, render3d.AxisZ, rotStart, rot22, snap15)
	if !math3d.ApproxEqual(angSnap, snap15, eps) {
		t.Fatalf("DragRotate snap 15deg = %v deg, want 15 deg", math3d.RadToDeg(angSnap))
	}
	qRot := g.DragRotateQuat(rotOrigin, render3d.AxisZ, rotStart, rotQuarter, 0)
	if !qRot.RotateVec3(math3d.Vec3UnitX()).ApproxEqual(math3d.Vec3UnitY(), eps) {
		t.Fatalf("DragRotateQuat did not rotate +X to +Y: %v", qRot.RotateVec3(math3d.Vec3UnitX()))
	}

	// DragScale along Y and uniform AxisAll with snapping.
	sStart := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 1, Z: 5}, Dir: math3d.Vec3{Z: -1}}
	sEnd := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 2.35, Z: 5}, Dir: math3d.Vec3{Z: -1}}
	sy := g.DragScale(rotOrigin, render3d.AxisY, sStart, sEnd, 0.25)
	if !sy.ApproxEqual(math3d.Vec3{X: 0, Y: 1.25, Z: 0}, eps) {
		t.Fatalf("DragScale AxisY snap=0.25 = %v, want (0, 1.25, 0)", sy)
	}
	sAll := g.DragScale(rotOrigin, render3d.AxisAll, sStart, sEnd, 0.25)
	if !sAll.ApproxEqual(math3d.Vec3{X: 1.25, Y: 1.25, Z: 1.25}, eps) {
		t.Fatalf("DragScale AxisAll snap=0.25 = %v, want (1.25, 1.25, 1.25)", sAll)
	}
}

func TestDrawGizmoAndDebugDrawEmitCanvasOps(t *testing.T) {
	vp, _ := testCamera()
	viewport := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 300, Height: 200},
	}

	var dd render3d.DebugDraw
	dd.AABB(unitBoxAt(math3d.Vec3Zero(), 1), core.RGB(0x00ff00))
	dd.Sphere(math3d.Vec3{X: 2}, 0.5, core.RGB(0x00ffff), 12)
	dd.Ray(math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3UnitY()}, 2, core.RGB(0xffff00))
	dd.Axes(math3d.Vec3Zero(), 1)
	dd.Grid(math3d.Vec3Zero(), 4, 4, core.RGB(0x666666))
	if invVP, ok := vp.Inverse(); ok {
		dd.Frustum(invVP, core.RGB(0xff00ff))
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "gizmo",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw: func(d *core.Drawing, _ core.Size) {
					render3d.DrawGizmo(d, vp, viewport, math3d.Vec3Zero(), 1.5, render3d.GizmoTranslate, render3d.AxisX)
				},
			},
			core.Canvas{
				TestID: "gizmo-behind",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// Entire gizmo behind camera at z=30 (camera is at z=10 looking down -Z).
					render3d.DrawGizmo(d, vp, viewport, math3d.Vec3{Z: 30}, 1.0, render3d.GizmoTranslate, render3d.AxisNone)
				},
			},
			core.Canvas{
				TestID: "debug",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw: func(d *core.Drawing, _ core.Size) {
					dd.Flush(d, vp, viewport)
				},
			},
		}}
	})

	gizmoOps := app.Find("gizmo").Ops
	var lineTos, strokes int
	var lastStrokeWidth float32
	for _, op := range gizmoOps {
		switch op.Kind {
		case core.DrawLineTo:
			lineTos++
		case core.DrawStroke:
			strokes++
		case core.DrawLineWidth:
			lastStrokeWidth = op.W
		}
	}
	// 3 shafts x 5 (shaft + 4 head lines) + 3 planar squares x 4 edges + center cube x 12 edges.
	if lineTos != 39 {
		t.Fatalf("gizmo DrawLineTo count = %d, want 39", lineTos)
	}
	if strokes == 0 {
		t.Fatal("gizmo emitted no DrawStroke ops")
	}
	// Active axis is drawn last with thicker ActiveLineWidth (3.5).
	if !math3d.ApproxEqual(lastStrokeWidth, 3.5, eps) {
		t.Fatalf("last stroke width = %v, want 3.5 for active axis drawn on top", lastStrokeWidth)
	}

	// Behind-camera gizmo should emit 0 LineTo / Stroke ops.
	behindOps := app.Find("gizmo-behind").Ops
	for _, op := range behindOps {
		if op.Kind == core.DrawLineTo || op.Kind == core.DrawStroke {
			t.Fatalf("behind-camera gizmo unexpectedly drew op %+v", op)
		}
	}

	// DebugDraw should emit DrawStroke and DrawLineTo ops.
	debugOps := app.Find("debug").Ops
	var debugLines int
	for _, op := range debugOps {
		if op.Kind == core.DrawLineTo {
			debugLines++
		}
	}
	if debugLines == 0 {
		t.Fatal("DebugDraw.Flush emitted no lines")
	}

	// Near-plane clipping: a segment from behind camera (z=20) to in front (z=0)
	// must stay visible and produce finite screen coordinates.
	ax, ay, bx, by, ok := render3d.ProjectSegment(math3d.Vec3{Z: 20}, math3d.Vec3{Z: 0}, vp, viewport)
	if !ok {
		t.Fatal("expected segment crossing near plane to be visible")
	}
	for _, v := range []float32{ax, ay, bx, by} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("non-finite clipped screen coord: %v", v)
		}
	}

	dd.Clear()
	if len(dd.Lines()) != 0 {
		t.Fatalf("expected Clear to empty lines, got %d", len(dd.Lines()))
	}
}

func TestFrameProfiler(t *testing.T) {
	p := render3d.NewFrameProfiler(5)
	if p.Count() != 0 || p.AverageFrameTime() != 0 || p.FPS() != 0 {
		t.Fatal("new profiler should be empty")
	}

	// Record 6 samples into capacity-5 ring buffer (oldest 100ms drops out).
	durations := []time.Duration{
		100 * time.Millisecond,
		10 * time.Millisecond,
		15 * time.Millisecond,
		20 * time.Millisecond,
		25 * time.Millisecond,
		30 * time.Millisecond,
	}
	for i, d := range durations {
		p.Record(d, render3d.RenderStats{
			TotalDrawCalls: 10 + i*5,
			Triangles:      uint64(1000 * (i + 1)),
		})
	}
	if p.Count() != 5 {
		t.Fatalf("Count = %d, want 5", p.Count())
	}
	if avg := p.AverageFrameTime(); avg != 20*time.Millisecond {
		t.Fatalf("AverageFrameTime = %v, want 20ms", avg)
	}
	if !math3d.ApproxEqual(float32(p.FPS()), 50, 1e-2) {
		t.Fatalf("FPS = %v, want 50", p.FPS())
	}
	if p95 := p.P95FrameTime(); p95 != 30*time.Millisecond {
		t.Fatalf("P95FrameTime = %v, want 30ms", p95)
	}
	if p.PeakDrawCalls() != 35 {
		t.Fatalf("PeakDrawCalls = %d, want 35", p.PeakDrawCalls())
	}
	if p.PeakTriangles() != 6000 {
		t.Fatalf("PeakTriangles = %d, want 6000", p.PeakTriangles())
	}

	p.Reset()
	if p.Count() != 0 || p.PeakDrawCalls() != 0 || p.PeakTriangles() != 0 {
		t.Fatal("Reset did not clear profiler")
	}
}

func TestGizmoDragTranslateGuardsGrazingPlaneRays(t *testing.T) {
	var g render3d.Gizmo
	origin := math3d.Vec3Zero()

	// Ray grazing the XZ plane (normal = +Y, |dir.Y| = 0.005 < 0.08) must return zero rather than exploding.
	r0 := math3d.Ray{
		Origin: math3d.Vec3{X: 0, Y: 1, Z: 200},
		Dir:    math3d.Vec3{X: 0, Y: -0.005, Z: -1}.Norm(),
	}
	r1 := math3d.Ray{
		Origin: math3d.Vec3{X: 0, Y: 1, Z: 200},
		Dir:    math3d.Vec3{X: 0.05, Y: -0.001, Z: -1}.Norm(),
	}
	delta := g.DragTranslate(origin, render3d.AxisXZ, r0, r1, 0)
	if delta != math3d.Vec3Zero() {
		t.Fatalf("grazing XZ plane DragTranslate = %v, want zero", delta)
	}

	// AxisAll with currentRay nearly perpendicular to view plane normal must also return zero.
	v0 := math3d.Ray{Origin: math3d.Vec3{Z: 10}, Dir: math3d.Vec3{Z: -1}}
	v1 := math3d.Ray{Origin: math3d.Vec3{Z: 10}, Dir: math3d.Vec3{X: 1, Z: -0.01}.Norm()}
	deltaAll := g.DragTranslate(origin, render3d.AxisAll, v0, v1, 0)
	if deltaAll != math3d.Vec3Zero() {
		t.Fatalf("grazing AxisAll DragTranslate = %v, want zero", deltaAll)
	}
}

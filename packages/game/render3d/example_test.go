package render3d_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
)

// tree returns a draw item for a 2x2x2 tree mesh centered at x, z.
func tree(id uint64, x, z float32) render3d.DrawItem {
	center := math3d.Vec3{X: x, Y: 1, Z: z}
	return render3d.DrawItem{
		ID: id, MeshID: 1, MaterialID: 1, PipelineID: 1,
		Transform:   math3d.Translate(center),
		WorldBounds: math3d.AABB{Min: center.Sub(math3d.Vec3One()), Max: center.Add(math3d.Vec3One())},
		Triangles:   500, Vertices: 300,
	}
}

// A Queue culls draw items outside the camera's frustum, groups identical meshes into instanced batches and sorts the rest, giving a plan and its stats for the frame.
func ExampleQueue() {
	eye := math3d.Vec3{Y: 2, Z: 10}
	view := math3d.LookAt(eye, math3d.Vec3Zero(), math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9, 0.1, 100)
	viewProj := proj.Mul(view)

	items := []render3d.DrawItem{tree(1, -2, 0), tree(2, 0, 0), tree(3, 2, 0), tree(4, 0, 50)} // the last is behind the camera
	var q render3d.Queue
	plan := q.Build(viewProj, eye, items)
	fmt.Println(plan.Stats.TotalItems, plan.Stats.CulledItems, plan.Stats.InstancedBatches, plan.Stats.TotalDrawCalls, plan.Stats.Triangles)
	// Output:
	// 4 1 1 1 1500
}

// SelectLOD picks a coarser mesh as an object gets farther away.
func ExampleSelectLOD() {
	levels := []render3d.LODLevel{
		{MeshID: 10, MaxDistance: 20, Triangles: 5000},
		{MeshID: 11, MaxDistance: 60, Triangles: 1200},
		{MeshID: 12, Triangles: 200},
	}
	for _, d := range []float32{5, 40, 90} {
		fmt.Println(d, levels[render3d.SelectLOD(levels, d, -1, 0, -1)].MeshID)
	}
	// Output:
	// 5 10
	// 40 11
	// 90 12
}

// CheckBudget compares a frame's stats with a device budget and lists what went over.
func ExampleCheckBudget() {
	stats := render3d.RenderStats{TotalDrawCalls: 400, Triangles: 250_000}
	for _, v := range render3d.CheckBudget(stats, render3d.Budget{MaxDrawCalls: 200, MaxTriangles: 500_000}) {
		fmt.Println(v)
	}
	// Output:
	// draw_calls: 400 exceeds budget 200
}

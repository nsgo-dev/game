package world3d_test

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// show prints vectors to one decimal, writing a rounded negative zero as 0.
func show(vs ...math3d.Vec3) string {
	num := func(f float32) string {
		s := strconv.FormatFloat(float64(f), 'f', 1, 32)
		if s == "-0.0" {
			s = "0.0"
		}
		return s
	}
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = "(" + num(v.X) + ", " + num(v.Y) + ", " + num(v.Z) + ")"
	}
	return strings.Join(out, " ")
}

// A Heightmap is terrain from a grid of heights: HeightAt reads the ground under a point, interpolating between samples, and Raycast finds where a ray meets it.
func ExampleHeightmap() {
	h := &world3d.Heightmap{
		Cols: 3, Rows: 3, CellSize: 10, HeightScale: 2,
		Heights: []float32{
			0, 0, 0,
			0, 1, 0,
			0, 0, 0,
		},
	}
	y, ok := h.HeightAt(10, 10)
	fmt.Println(y, ok)
	y, _ = h.HeightAt(5, 10)
	fmt.Println(y)
	hit, ok := h.Raycast(math3d.Ray{Origin: math3d.Vec3{X: 10, Y: 50, Z: 10}, Dir: math3d.Vec3{Y: -1}}, 100)
	fmt.Println(ok, show(hit.Point))
	// Output:
	// 2 true
	// 1
	// true (10.0, 2.0, 10.0)
}

// A NavMesh joins walkable polygons that share edges and finds a path across them.
func ExampleNavMesh() {
	floor := [][]math3d.Vec3{
		{{X: 0, Z: 0}, {X: 0, Z: 10}, {X: 10, Z: 10}, {X: 10, Z: 0}},
		{{X: 10, Z: 0}, {X: 10, Z: 10}, {X: 20, Z: 10}, {X: 20, Z: 0}},
	}
	nm := world3d.BuildNavMesh(floor, math3d.DegToRad(45))
	path, ok := nm.FindPath(math3d.Vec3{X: 2, Z: 5}, math3d.Vec3{X: 18, Z: 5})
	fmt.Println(ok, show(path...))
	_, ok = nm.FindPolygon(math3d.Vec3{X: 30, Z: 5})
	fmt.Println(ok)
	// Output:
	// true (2.0, 0.0, 5.0) (18.0, 0.0, 5.0)
	// false
}

// A FloatingOrigin keeps positions near zero in float32 by moving the origin when the player wanders far, while WorldPos64 keeps the true position in float64.
func ExampleFloatingOrigin() {
	fo := world3d.NewFloatingOrigin(1000)
	rebased, shift := fo.Update(math3d.Vec3{X: 1500, Z: 20})
	fmt.Println(rebased, show(shift))
	fmt.Println(fo.Origin, show(fo.WorldToLocal(world3d.WorldPos64{X: 1600, Z: 20})))
	// Output:
	// true (-1500.0, 0.0, -20.0)
	// {1500 0 20} (100.0, 0.0, 0.0)
}

// WorldPos64 finds the chunk a position falls in, as a ChunkStreamer does to decide which chunks to load.
func ExampleWorldPos64_ChunkCoord() {
	for _, p := range []world3d.WorldPos64{{X: 10, Z: 10}, {X: 130, Z: -1}, {X: 1e6, Z: 0}} {
		fmt.Println(p.ChunkCoord(128))
	}
	// Output:
	// {0 0}
	// {1 -1}
	// {7812 0}
}

// A GridMap places tiles from a mesh library on a 3D grid, as a block or dungeon editor does, and raycasts against them.
func ExampleGridMap() {
	g := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, world3d.NewMeshLibrary())
	g.FillBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 4, Y: 0, Z: 4}, 1, 0)
	fmt.Println(g.Count(), g.Bounds())
	hit, ok := g.Raycast(math3d.Ray{Origin: math3d.Vec3{X: 3, Y: 10, Z: 3}, Dir: math3d.Vec3{Y: -1}}, 50)
	fmt.Println(ok, hit.Coord, hit.Point.Y)
	// Output:
	// 25 {{0 0 0} {10 2 10}}
	// true {1 0 1} 2
}

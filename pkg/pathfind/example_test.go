package pathfind_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// AStar finds the cheapest path around the walls of a BoolGrid.
func ExampleAStar() {
	grid := pathfind.NewBoolGrid(5, 3)
	for row := range 2 {
		grid.SetWalkable(2, row, false)
	}
	path, cost, ok := pathfind.NewAStar().Find(grid, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 4, Row: 0})
	fmt.Println(ok, cost)
	fmt.Println(path)
	// Output:
	// true 8
	// [{0 0} {1 0} {1 1} {1 2} {2 2} {3 2} {3 1} {3 0} {4 0}]
}

// SmoothGridPath turns a grid A* path into world-space waypoints while keeping a body radius clear of blocked cells.
func ExampleSmoothGridPath() {
	grid := pathfind.NewBoolGrid(6, 4)
	path, _, _ := pathfind.NewAStar().Find(grid, pathfind.Cell{Col: 0, Row: 0}, pathfind.Cell{Col: 5, Row: 3})
	waypoints := pathfind.SmoothGridPath(grid, path, pathfind.GridPathOptions{CellSize: 16, Radius: 5})
	fmt.Println(len(path), "->", len(waypoints), waypoints)
	// Output:
	// 9 -> 2 [{8 8} {88 56}]
}

// A CostMap makes some cells dearer to cross, so AStar goes around them when that is cheaper.
func ExampleCostMap() {
	m := pathfind.NewCostMap(3, 3)
	m.SetCost(1, 1, 10)
	path, cost, _ := pathfind.NewAStar().Find(m, pathfind.Cell{Col: 1, Row: 0}, pathfind.Cell{Col: 1, Row: 2})
	fmt.Println(len(path), cost)
	// Output:
	// 5 4
}

// GridFunc adapts a game's own map to Grid without copying it.
func ExampleGridFunc() {
	level := []string{
		"..#",
		".##",
		"...",
	}
	g := pathfind.GridFunc{
		Cols: 3, Rows: 3,
		Walk: func(col, row int) bool { return level[row][col] == '.' },
	}
	path, _, ok := pathfind.NewAStar().Find(g, pathfind.Cell{Col: 1, Row: 0}, pathfind.Cell{Col: 2, Row: 2})
	fmt.Println(ok, path)
	// Output:
	// true [{1 0} {0 0} {0 1} {0 2} {1 2} {2 2}]
}

// HexGrid adapts a hexagonal tilemap.Tilemap to GraphSearch for both A* paths and budgeted movement ranges with reconstructed paths.
func ExampleHexGrid() {
	m := tilemap.New(5, 5, 32, 36)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	m.SetCellFlags(2, 2, tilemap.TileSolid)

	hex := pathfind.NewHexGrid(m)
	search := pathfind.NewGraphSearch[pathfind.Cell]()
	rng := search.Range(hex, pathfind.Cell{Col: 1, Row: 2}, 3)
	path, ok := rng.Path(pathfind.Cell{Col: 3, Row: 2})
	cost, _ := rng.Cost(pathfind.Cell{Col: 3, Row: 2})
	fmt.Println(ok, cost, len(path))
	// Output:
	// true 3 4
}

// WaypointGraph stores hand-placed 2D or 3D waypoints with weighted edges searched by GraphSearch.
func ExampleWaypointGraph() {
	g := pathfind.NewWaypointGraph()
	a := g.Add(core.Point{X: 0, Y: 0})
	b := g.Add(core.Point{X: 30, Y: 0})
	c := g.Add(core.Point{X: 30, Y: 40})
	g.ConnectUndirected(a, b)
	g.ConnectUndirected(b, c)
	g.ConnectUndirected(a, c)

	path, cost, ok := pathfind.NewGraphSearch[int]().Find(g, a, c)
	fmt.Println(ok, cost, path)
	// Output:
	// true 50 [0 2]
}

// A NavMesh joins convex polygons that share edges and finds paths across them.
func ExampleNavMesh() {
	mesh := pathfind.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		{{X: 10, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 10}, {X: 10, Y: 10}},
	})
	fmt.Println(mesh.PolygonCount(), mesh.Neighbors(0))
	path, ok := mesh.FindPath(core.Point{X: 2, Y: 5}, core.Point{X: 18, Y: 5})
	fmt.Println(ok, path)
	// Output:
	// 2 [1]
	// true [{2 5} {18 5}]
}

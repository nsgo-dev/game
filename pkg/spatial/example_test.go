package spatial_test

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/pkg/spatial"
	"github.com/nsgo-dev/nsgo/core"
)

func box(x, y, w, h core.Points) core.Bounds {
	return core.Bounds{Origin: core.Point{X: x, Y: y}, Size: core.Size{Width: w, Height: h}}
}

// A Hash finds the objects near an area without testing every one, here for collision pairs between enemies.
func ExampleHash() {
	grid := spatial.NewHash[string](32)
	grid.Insert("slime", box(10, 10, 8, 8))
	grid.Insert("bat", box(14, 12, 8, 8))
	grid.Insert("golem", box(200, 200, 16, 16))

	near := grid.Query(box(0, 0, 40, 40), nil)
	slices.Sort(near)
	fmt.Println(near)
	var pairs []string
	grid.QueryPairs(func(a, b string) {
		if a > b {
			a, b = b, a
		}
		pairs = append(pairs, a+"+"+b)
	})
	fmt.Println(pairs)
	// Output:
	// [bat slime]
	// [bat+slime]
}

// A Quadtree indexes objects across a large world and answers point and area queries.
func ExampleQuadtree() {
	qt := spatial.NewQuadtree[int](box(0, 0, 1024, 1024), 4, 8)
	for i := range 10 {
		qt.Insert(i, box(core.Points(i)*100, 0, 10, 10))
	}
	hits := qt.Query(box(150, 0, 300, 50), nil)
	slices.Sort(hits)
	fmt.Println(hits, qt.QueryPoint(core.Point{X: 905, Y: 5}, nil))
	// Output:
	// [2 3 4] [9]
}

// Cull keeps only the items whose bounds touch the view, so offscreen objects are not drawn.
func ExampleCull() {
	trees := []core.Bounds{box(0, 0, 10, 10), box(500, 0, 10, 10), box(95, 95, 10, 10)}
	view := box(0, 0, 100, 100)
	visible := spatial.Cull(view, trees, func(b core.Bounds) core.Bounds { return b }, nil)
	fmt.Println(len(visible), spatial.Intersects(view, trees[1]))
	// Output:
	// 2 false
}

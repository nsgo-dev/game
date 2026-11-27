package pathfind3d_test

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
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

// A NavMesh joins walkable polygons that share edges and finds a path across them.
func ExampleNavMesh() {
	floor := [][]math3d.Vec3{
		{{X: 0, Z: 0}, {X: 0, Z: 10}, {X: 10, Z: 10}, {X: 10, Z: 0}},
		{{X: 10, Z: 0}, {X: 10, Z: 10}, {X: 20, Z: 10}, {X: 20, Z: 0}},
	}
	nm := pathfind3d.BuildNavMesh(floor, math3d.DegToRad(45))
	path, ok := nm.FindPath(math3d.Vec3{X: 2, Z: 5}, math3d.Vec3{X: 18, Z: 5})
	fmt.Println(ok, show(path...))
	_, ok = nm.FindPolygon(math3d.Vec3{X: 30, Z: 5})
	fmt.Println(ok)
	// Output:
	// true (2.0, 0.0, 5.0) (18.0, 0.0, 5.0)
	// false
}

package ai_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
)

// AStar finds the cheapest path around the walls of a BoolGrid.
func ExampleAStar() {
	grid := ai.NewBoolGrid(5, 3)
	for row := range 2 {
		grid.SetWalkable(2, row, false)
	}
	path, cost, ok := ai.NewAStar().Find(grid, ai.Cell{Col: 0, Row: 0}, ai.Cell{Col: 4, Row: 0})
	fmt.Println(ok, cost)
	fmt.Println(path)
	// Output:
	// true 8
	// [{0 0} {1 0} {1 1} {1 2} {2 2} {3 2} {3 1} {3 0} {4 0}]
}

// A CostMap makes some cells dearer to cross, so AStar goes around them when that is cheaper.
func ExampleCostMap() {
	m := ai.NewCostMap(3, 3)
	m.SetCost(1, 1, 10)
	path, cost, _ := ai.NewAStar().Find(m, ai.Cell{Col: 1, Row: 0}, ai.Cell{Col: 1, Row: 2})
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
	g := ai.GridFunc{
		Cols: 3, Rows: 3,
		Walk: func(col, row int) bool { return level[row][col] == '.' },
	}
	path, _, ok := ai.NewAStar().Find(g, ai.Cell{Col: 1, Row: 0}, ai.Cell{Col: 2, Row: 2})
	fmt.Println(ok, path)
	// Output:
	// true [{1 0} {0 0} {0 1} {0 2} {1 2} {2 2}]
}

// A NavMesh joins convex polygons that share edges and finds paths across them.
func ExampleNavMesh() {
	mesh := ai.BuildNavMesh([][]core.Point{
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

// Steering forces move an Agent; Combine weighs several of them and caps the total at the agent's MaxForce.
func ExampleAgent() {
	a := ai.Agent{MaxSpeed: 100, MaxForce: 400}
	target := core.Point{X: 100, Y: 0}
	for range 30 {
		force := ai.Combine(a.MaxForce,
			ai.WeightedForce{Force: ai.Arrive(a, target, 40), Weight: 1},
			ai.WeightedForce{Force: ai.Flee(a, core.Point{X: 0, Y: 50}, 20), Weight: 2},
		)
		a.Step(force, time.Second/30)
	}
	fmt.Printf("x %.1f y %.1f\n", a.Pos.X, a.Pos.Y)
	// Output:
	// x 38.3 y 0.0
}

// FollowPath steers along waypoints and records progress in a PathFollowState.
func ExampleFollowPath() {
	a := ai.Agent{MaxSpeed: 50, MaxForce: 500}
	path := []core.Point{{X: 10, Y: 0}, {X: 10, Y: 10}}
	var state ai.PathFollowState
	steps := 0
	for !state.Done && steps < 1000 {
		a.Step(ai.FollowPath(a, path, &state, 1, false), time.Second/60)
		steps++
	}
	fmt.Println(state.Done, state.Index)
	// Output:
	// true 1
}

// Wander picks a smoothly changing heading from a caller-supplied random source, so the same source gives the same walk.
func ExampleWander() {
	seed := uint32(1)
	next := func() float32 {
		seed = seed*1664525 + 1013904223
		return float32(seed>>8) / (1 << 24)
	}
	a := ai.Agent{MaxSpeed: 20, MaxForce: 40}
	var state ai.WanderState
	for range 10 {
		a.Step(ai.Wander(a, &state, 5, 10, 0.5, next), time.Second/10)
	}
	fmt.Printf("%.2f %.2f\n", a.Pos.X, a.Pos.Y)
	// Output:
	// 7.96 -2.05
}

// A behavior tree ticks from the root each update: here a guard picks between attacking a target in range and patrolling.
func ExampleSelector() {
	inRange := false
	log := func(name string) ai.Node {
		return ai.Action(func(time.Duration) ai.Status {
			fmt.Println(name)
			return ai.StatusSuccess
		})
	}
	tree := ai.Selector(
		ai.Sequence(ai.Condition(func() bool { return inRange }), log("attack")),
		ai.Sequence(log("patrol"), ai.Wait(time.Second)),
	)
	fmt.Println(tree.Tick(time.Second / 2))
	inRange = true
	fmt.Println(tree.Tick(time.Second / 2))
	// Output:
	// patrol
	// Running
	// attack
	// Success
}

// Parallel runs its children together; RequireAll succeeds once every child has.
func ExampleParallel() {
	tree := ai.Parallel(ai.RequireAll,
		ai.Wait(time.Second),
		ai.Repeater(2, ai.Succeeder(ai.Inverter(ai.Condition(func() bool { return true })))),
	)
	fmt.Println(tree.Tick(time.Second / 2))
	fmt.Println(tree.Tick(time.Second / 2))
	// Output:
	// Running
	// Success
}

// Cooldown lets its child run again only after the given time has passed.
func ExampleCooldown() {
	shots := 0
	fire := ai.Cooldown(time.Second, ai.Action(func(time.Duration) ai.Status { shots++; return ai.StatusSuccess }))
	for range 6 {
		fire.Tick(time.Second / 4)
	}
	fmt.Println("shots:", shots)
	// Output:
	// shots: 2
}

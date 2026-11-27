package ai_test

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
)

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

// A PerceptionSystem steps each guard's senses: a target in its sight cone with a clear line of sight is seen, a noise within hearing range is remembered where it was made, and memories are forgotten once nothing has sensed them for ForgetAfter.
func ExamplePerceptionSystem() {
	guard := &ai.Perception{
		Self:        1,
		Team:        1,
		Facing:      core.Point{X: 1},
		Sight:       ai.SightConfig{Range: 100, HalfAngle: math.Pi / 4},
		Hearing:     ai.HearingConfig{Range: 60},
		ForgetAfter: time.Second,
	}
	sys := ai.PerceptionSystem{Budget: 32}
	sys.Add(guard)

	const dt = time.Second / 4
	sys.Step(dt, []ai.Target{{ID: 7, Team: 2, Pos: core.Point{X: 50, Y: 10}}})
	sys.PostNoise(ai.Noise{Source: 8, Team: 2, Pos: core.Point{X: -30}, Loudness: 1})
	sys.Step(dt, nil)
	for _, ev := range guard.Events() {
		fmt.Println(ev.Kind, ev.Memory.ID, ev.Memory.Sense, ev.Memory.Pos)
	}
	for range 4 {
		sys.Step(dt, nil)
		for _, ev := range guard.Events() {
			fmt.Println(ev.Kind, ev.Memory.ID, ev.Memory.Age)
		}
	}
	// Output:
	// lost 7 sight {50 10}
	// gained 8 hearing {-30 0}
	// forgotten 7 1s
	// forgotten 8 1s
}

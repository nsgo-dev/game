package tween_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/tween"
	"github.com/nsgo-dev/nsgo/core"
)

// A Tween animates a value on the game's fixed step; Repeat with yoyo plays it back and forth.
func ExampleFloat32() {
	alpha := float32(0)
	tw := tween.Float32(0, 1, time.Second, core.Linear, func(v float32) { alpha = v }).Repeat(1, true)
	for range 6 {
		tw.Update(400 * time.Millisecond)
		fmt.Printf("%.1f ", alpha)
	}
	fmt.Println(tw.Done())
	// Output:
	// 0.4 0.8 0.8 0.4 0.0 0.0 true
}

// A Timeline sequences tweens and calls: Append runs a tween after the previous one ends, and Add starts one at a set time.
func ExampleTimeline() {
	var x, y float32
	tl := tween.NewTimeline().
		Append(tween.Float32(0, 100, time.Second, core.Linear, func(v float32) { x = v })).
		Append(tween.Float32(0, 50, 500*time.Millisecond, core.Linear, func(v float32) { y = v })).
		Call(time.Second, func() { fmt.Println("halfway cue") })
	fmt.Println(tl.Duration())
	tl.Play()
	tl.Update(1250 * time.Millisecond)
	fmt.Println(x, y, tl.Done())
	tl.Update(time.Second)
	fmt.Println(x, y, tl.Done())
	// Output:
	// 1.5s
	// halfway cue
	// 100 25 false
	// 100 50 true
}

// A PathFollower moves along a curve at a constant speed, for platforms or enemies on a route.
func ExamplePathFollower() {
	route := tween.Polyline([]core.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 50}}, false)
	f := tween.NewPathFollower(route, tween.FollowerOptions{Speed: 50})
	for range 3 {
		f.Update(time.Second)
		fmt.Println(f.Position(), f.Distance())
	}
	fmt.Println(f.Done())
	// Output:
	// {50 0} 50
	// {100 0} 100
	// {100 50} 150
	// true
}

// A Group updates many independent tweens together and drops them as they finish.
func ExampleGroup() {
	var g tween.Group
	g.Add(
		tween.Float64(0, 1, 100*time.Millisecond, core.Linear, func(float64) {}),
		tween.Float64(0, 1, 300*time.Millisecond, core.Linear, func(float64) {}),
	)
	g.Update(200 * time.Millisecond)
	fmt.Println(g.Len())
	// Output:
	// 1
}

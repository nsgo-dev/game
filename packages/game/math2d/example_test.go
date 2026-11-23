package math2d_test

import (
	"fmt"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/math2d"
)

// show prints v to two decimals, writing a rounded negative zero as 0.
func show(v math2d.Vec2) string {
	num := func(f float32) string {
		s := strconv.FormatFloat(float64(f), 'f', 2, 32)
		if s == "-0.00" {
			s = "0.00"
		}
		return s
	}
	return "(" + num(v.X) + ", " + num(v.Y) + ")"
}

// Vec2 is a 2D vector for positions, velocities and directions.
func ExampleVec2() {
	pos := math2d.V2(0, 0)
	target := math2d.V2(3, 4)
	fmt.Println(pos.Dist(target), show(target.Sub(pos).Normalize()))
	fmt.Println(show(pos.MoveTowards(target, 2)))
	fmt.Println(show(math2d.V2(1, -1).Reflect(math2d.UnitY())))
	fmt.Println(show(math2d.UnitX().Rotate(math.Pi / 2)))
	// Output:
	// 5 (0.60, 0.80)
	// (1.20, 1.60)
	// (1.00, 1.00)
	// (0.00, 1.00)
}

// An Affine composes translation, rotation and scale; Invert maps a world point back into the local space, as picking under a camera does.
func ExampleAffine() {
	m := math2d.TRS(math2d.V2(100, 50), math.Pi/2, math2d.V2(2, 2))
	world := m.TransformPoint(math2d.V2(10, 0))
	fmt.Println(show(world))
	inv, ok := m.Invert()
	fmt.Println(show(inv.TransformPoint(world)), ok)
	// Output:
	// (100.00, 70.00)
	// (10.00, 0.00) true
}

// Rect tests overlaps between axis-aligned boxes and clips one by another.
func ExampleRect() {
	player := math2d.Rect{X: 0, Y: 0, W: 10, H: 20}
	spike := math2d.FromCenter(math2d.V2(12, 10), math2d.V2(8, 8))
	fmt.Println(player.Overlaps(spike))
	hit, ok := player.Intersection(spike)
	fmt.Println(hit, ok)
	fmt.Println(show(player.ClosestPoint(math2d.V2(20, 25))))
	// Output:
	// true
	// {8 6 2 8} true
	// (10.00, 20.00)
}

// Circle tests overlaps with other circles and with rectangles.
func ExampleCircle() {
	bullet := math2d.Circle{Center: math2d.V2(5, 5), Radius: 1}
	enemy := math2d.Circle{Center: math2d.V2(6.5, 5), Radius: 1}
	wall := math2d.Rect{X: 10, Y: 0, W: 2, H: 10}
	fmt.Println(bullet.Overlaps(enemy), bullet.OverlapsRect(wall), bullet.Bounds())
	// Output:
	// true false {4 4 2 2}
}

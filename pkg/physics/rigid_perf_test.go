package physics_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// perfScene is the 500-body performance scenario, which BenchmarkRigidWorld500Bodies and BenchmarkRigidWorld500BodiesPacked time against the 16.7 ms of a 60 fps frame: a bin 640 px wide with a floor, two walls and a chain ramp, and 500 boxes, circles, rounded hexagons and capsules dropped into it in a grid.
func perfScene() *physics.RigidWorld {
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(
		physics.NewStaticRigidBox(320, 610, 680, 20),
		physics.NewStaticRigidBox(-10, 300, 20, 620),
		physics.NewStaticRigidBox(650, 300, 20, 620),
		physics.NewStaticRigidBody(physics.ChainShape([]core.Point{{X: 0, Y: 520}, {X: 160, Y: 580}, {X: 320, Y: 600}}, false)),
	)
	hexagon := func(x, y float32) physics.Shape {
		pts := make([]core.Point, 6)
		for i, d := range [][2]float32{{6, 0}, {3, 5.2}, {-3, 5.2}, {-6, 0}, {-3, -5.2}, {3, -5.2}} {
			pts[i] = core.Point{X: core.Points(x + d[0]), Y: core.Points(y + d[1])}
		}
		p, _ := physics.NewPolygon(pts, 0.5)
		return physics.PolygonShape(p)
	}
	for i := range 500 {
		x := float32(20 + 24*(i%25))
		y := float32(560 - 24*(i/25))
		var b *physics.RigidBody
		switch i % 4 {
		case 0:
			b = physics.NewRigidBox(x, y, 14, 14, 1)
		case 1:
			b = physics.NewRigidCircle(x, y, 7, 1)
		case 2:
			b = physics.NewRigidBody(hexagon(x, y), 1)
		default:
			b = physics.NewRigidBody(physics.CapsuleShape(physics.Capsule{X1: x - 5, Y1: y, X2: x + 5, Y2: y, R: 4}), 1)
		}
		b.Angle = float32(i%7) * 0.1
		w.AddBody(b)
	}
	return w
}

func BenchmarkRigidWorld500Bodies(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		w := perfScene()
		b.StartTimer()
		for range 60 {
			w.Step(step)
		}
	}
}

func BenchmarkRigidWorld500BodiesPacked(b *testing.B) {
	w := perfScene()
	for range 600 {
		w.Step(step)
	}
	b.ReportAllocs()
	for b.Loop() {
		w.Step(step)
	}
}

// boxPile2000Scene builds 2000 boxes of size 20 px arranged in 40 columns of 50 on a static floor, with sleeping disabled so every box stays active.
func boxPile2000Scene() *physics.RigidWorld {
	w := &physics.RigidWorld{GravityY: 600, DisableSleep: true}
	w.AddBody(physics.NewStaticRigidBox(500, 1010, 2000, 20))
	const (
		cols = 40
		rows = 50
		size = 20
	)
	for r := range rows {
		y := float32(1000 - size*0.5 - float32(r)*size)
		for c := range cols {
			x := float32(100 + float32(c)*(size+2))
			w.AddBody(physics.NewRigidBox(x, y, size, size, 1))
		}
	}
	return w
}

func BenchmarkRigidWorld2000Boxes(b *testing.B) {
	w := boxPile2000Scene()
	for range 60 {
		w.Step(step)
	}
	b.ReportAllocs()
	for b.Loop() {
		w.Step(step)
	}
}

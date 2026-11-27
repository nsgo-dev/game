package physics3d_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

// terrain returns a 12 m square heightfield: rolling bumps for x < 0 and flat ground for x >= 0.
func terrain() *physics3d.HeightfieldCollider {
	const n, cell = 25, 0.5
	origin := v3{X: -6, Z: -6}
	h := make([]float32, n*n)
	for z := range n {
		for x := range n {
			wx, wz := origin.X+float32(x)*cell, origin.Z+float32(z)*cell
			if wx < 0 {
				h[z*n+x] = 0.15 * float32(math.Sin(float64(0.8*wx))*math.Cos(float64(0.8*wz)))
			}
		}
	}
	return physics3d.NewHeightfieldCollider(n, n, cell, origin, h)
}

// pileScene drops eight rocks in a pile on the bumpy half of the terrain and a stack of three barrels on the flat half.
func pileScene(t testing.TB) (*physics3d.World, []*physics3d.Body, []*physics3d.Body) {
	w := physics3d.NewWorld()
	w.AddHeightfield(terrain())
	var rocks []*physics3d.Body
	for i := range 8 {
		h, err := physics3d.NewConvexHull(rockPoints(uint64(10+i), 60, 0.6), 24)
		if err != nil {
			t.Fatal(err)
		}
		h = h.Recentered()
		pos := h.Center.Add(v3{X: -2.5 + float32(i%2)*0.45, Y: 0.6 + float32(i/2)*0.55, Z: float32((i/2)%2) * 0.3})
		h.Center = v3{}
		b := physics3d.NewDynamicBody(physics3d.ConvexHullShape(h), pos, 2)
		b.Rot = physics3d.QuatFromAxisAngle(v3{X: 1, Y: 0.3, Z: 0.2}, float32(i)*0.7)
		rocks = append(rocks, w.AddBody(b))
	}
	var barrels []*physics3d.Body
	for i := range 3 {
		cyl := physics3d.VerticalCylinder(v3{}, 0.3, 0.6)
		barrels = append(barrels, w.AddBody(physics3d.NewDynamicBody(physics3d.CylinderShape(cyl), v3{X: 2, Y: 0.31 + float32(i)*0.62}, 4)))
	}
	return w, rocks, barrels
}

func axisUp(b *physics3d.Body) float32 { return b.Rot.RotateVec(v3{Y: 1}).Y }

func TestConvexRocksAndBarrelsSettleAndSleepOnHeightfield(t *testing.T) {
	w, rocks, barrels := pileScene(t)
	hf := w.Heightfields()[0]
	steps := 0
	allAsleep := func() bool {
		for _, b := range w.Bodies() {
			if !b.Sleeping {
				return false
			}
		}
		return true
	}
	for steps = 0; steps < 60*20 && !allAsleep(); steps++ {
		w.Step(step60)
	}
	for i, b := range rocks {
		ground, _ := hf.HeightAt(b.Pos.X, b.Pos.Z)
		if !b.Sleeping || b.Pos.Y < ground-0.05 || b.Pos.Y > 2 {
			t.Errorf("rock %d at %v (ground %v) sleeping=%v after %d steps", i, b.Pos, ground, b.Sleeping, steps)
		}
	}
	for i, b := range barrels {
		want := 0.3 + float32(i)*0.6
		if !b.Sleeping || axisUp(b) < 0.995 || !near(b.Pos.Y, want, 0.02) || !near(b.Pos.X, 2, 0.05) || !near(b.Pos.Z, 0, 0.05) {
			t.Errorf("barrel %d at %v up %v sleeping=%v after %d steps, want standing at y %v", i, b.Pos, axisUp(b), b.Sleeping, steps, want)
		}
	}
	// Asleep, the pile stays put.
	before := make([]v3, len(w.Bodies()))
	for i, b := range w.Bodies() {
		before[i] = b.Pos
	}
	for range 120 {
		w.Step(step60)
	}
	for i, b := range w.Bodies() {
		if b.Pos != before[i] {
			t.Fatalf("body %d drifted from %v to %v while asleep", i, before[i], b.Pos)
		}
	}
}

func TestBarrelRollsOnItsSide(t *testing.T) {
	for _, floor := range []string{"box", "heightfield"} {
		t.Run(floor, func(t *testing.T) {
			w := physics3d.NewWorld()
			if floor == "box" {
				w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 20, Y: 0.5, Z: 20}}), v3{Y: -0.5}))
			} else {
				w.AddHeightfield(physics3d.NewHeightfieldCollider(41, 41, 0.5, v3{X: -10, Z: -10}, make([]float32, 41*41)))
			}
			const r = 0.4
			b := physics3d.NewDynamicBody(physics3d.CylinderShape(physics3d.Cylinder{A: v3{Z: -0.6}, B: v3{Z: 0.6}, Radius: r}), v3{X: -4, Y: r}, 10)
			b.Vel = v3{X: 3}
			barrel := w.AddBody(b)
			for range 90 {
				w.Step(step60)
			}
			axis := barrel.Rot.RotateVec(v3{Z: 1})
			if math.Abs(float64(axis.Y)) > 0.02 || !near(barrel.Pos.Y, r, 0.01) || math.Abs(float64(barrel.Pos.Z)) > 0.05 {
				t.Fatalf("barrel left its side: pos %v axis %v", barrel.Pos, axis)
			}
			if barrel.Pos.X < -1.5 {
				t.Fatalf("barrel only reached x %v", barrel.Pos.X)
			}
			// Rolling without slipping: the bottom of the barrel is at rest, so ω_z·r = -v_x.
			if spin := -barrel.AngVel.Z * r; barrel.Vel.X < 0.5 || math.Abs(float64(spin-barrel.Vel.X)) > 0.05*float64(barrel.Vel.X) {
				t.Fatalf("barrel slides instead of rolling: vel %v, angvel %v", barrel.Vel, barrel.AngVel)
			}
		})
	}
}

func TestBarrelStandsOnItsEnd(t *testing.T) {
	for _, tilt := range []float32{0, 0.08} {
		t.Run(fmt.Sprintf("tilt %v", tilt), func(t *testing.T) {
			w := physics3d.NewWorld()
			w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 20, Y: 0.5, Z: 20}}), v3{Y: -0.5}))
			b := physics3d.NewDynamicBody(physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 0.3, 0.9)), v3{Y: 0.8}, 10)
			b.Rot = physics3d.QuatFromAxisAngle(v3{X: 1, Z: 0.5}, tilt)
			barrel := w.AddBody(b)
			for i := 0; i < 600 && !barrel.Sleeping; i++ {
				w.Step(step60)
			}
			if !barrel.Sleeping || axisUp(barrel) < 0.999 || !near(barrel.Pos.Y, 0.45, 0.01) {
				t.Fatalf("barrel at %v up %v sleeping=%v, want standing upright asleep at y 0.45", barrel.Pos, axisUp(barrel), barrel.Sleeping)
			}
		})
	}
}

func TestConvexSimulationIsDeterministic(t *testing.T) {
	run := func() []physics3d.Body {
		w, _, _ := pileScene(t)
		for range 240 {
			w.Step(step60)
		}
		out := make([]physics3d.Body, 0, len(w.Bodies()))
		for _, b := range w.Bodies() {
			out = append(out, *b)
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i].Pos != b[i].Pos || a[i].Rot != b[i].Rot || a[i].Vel != b[i].Vel || a[i].AngVel != b[i].AngVel || a[i].Sleeping != b[i].Sleeping {
			t.Fatalf("body %d differs between runs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func BenchmarkConvexPileStep(b *testing.B) {
	w, _, _ := pileScene(b)
	for range 30 {
		w.Step(step60)
	}
	for b.Loop() {
		w.Step(step60)
	}
}

func TestConvexBodiesRestOnTriangleMesh(t *testing.T) {
	// Two large triangles make the floor, so the single triangle under each body has to hold it flat on its own.
	floor := physics3d.NewTriMeshCollider([]v3{{X: -10, Z: -10}, {X: 10, Z: -10}, {X: 10, Z: 10}, {X: -10, Z: 10}}, []uint32{0, 2, 1, 0, 3, 2})
	w := physics3d.NewWorld()
	w.AddTriMesh(floor)
	wedge := w.AddBody(physics3d.NewDynamicBody(physics3d.ConvexHullShape(wedgeHull(t)), v3{X: -2, Y: 0.6}, 3))
	barrel := w.AddBody(physics3d.NewDynamicBody(physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 0.3, 0.9)), v3{X: 2, Y: 0.6}, 10))
	for i := 0; i < 600 && !(wedge.Sleeping && barrel.Sleeping); i++ {
		w.Step(step60)
	}
	if !wedge.Sleeping || !near(wedge.Pos.Y, 0.5, 0.01) || axisUp(wedge) < 0.999 {
		t.Fatalf("wedge at %v up %v sleeping=%v", wedge.Pos, axisUp(wedge), wedge.Sleeping)
	}
	if !barrel.Sleeping || !near(barrel.Pos.Y, 0.45, 0.01) || axisUp(barrel) < 0.999 {
		t.Fatalf("barrel at %v up %v sleeping=%v", barrel.Pos, axisUp(barrel), barrel.Sleeping)
	}
}

func TestFastConvexBodiesDoNotTunnel(t *testing.T) {
	small, err := physics3d.NewConvexHull(rockPoints(5, 30, 0.3), 16)
	if err != nil {
		t.Fatal(err)
	}
	small = small.Recentered()
	small.Center = v3{}
	shapes := map[string]physics3d.Shape{
		"hull":     physics3d.ConvexHullShape(small),
		"cylinder": physics3d.CylinderShape(physics3d.Cylinder{A: v3{X: -0.15}, B: v3{X: 0.15}, Radius: 0.12}),
	}
	for _, name := range []string{"hull", "cylinder"} {
		t.Run(name+" through thin box", func(t *testing.T) {
			w := physics3d.NewWorld()
			w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 5, Y: 0.05, Z: 5}}), v3{}))
			b := w.AddBody(physics3d.NewDynamicBody(shapes[name], v3{Y: 1}, 1))
			b.Vel = v3{Y: -50}
			for range 60 {
				w.Step(step60)
			}
			if b.Pos.Y < 0 {
				t.Fatalf("%s at -50 m/s ended at y = %v, under the 0.1 m slab", name, b.Pos.Y)
			}
		})
		t.Run(name+" through triangle mesh", func(t *testing.T) {
			w := physics3d.NewWorld()
			w.AddTriMesh(physics3d.NewTriMeshCollider([]v3{{X: -5, Z: -5}, {X: -5, Z: 5}, {X: 5, Z: 5}, {X: 5, Z: -5}}, []uint32{0, 1, 2, 0, 2, 3}))
			b := w.AddBody(physics3d.NewDynamicBody(shapes[name], v3{Y: 1}, 1))
			b.Vel = v3{Y: -60}
			for range 60 {
				w.Step(step60)
			}
			if b.Pos.Y < 0 {
				t.Fatalf("%s at -60 m/s ended at y = %v, under the mesh", name, b.Pos.Y)
			}
		})
	}
}

// TestConvexShapesAsAreas uses a hull and a cylinder as gravity areas, checking that GravityAt follows their exact shape rather than their bounds, and floats a barrel in water.
func TestConvexShapesAsAreas(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = v3{Y: -10}
	wedge := wedgeHull(t)
	hullArea := physics3d.NewStaticBody(physics3d.ConvexHullShape(wedge), v3{X: -5})
	hullArea.IsTrigger = true
	hullArea.Area = physics3d.AreaSettings{GravityMode: physics3d.AreaOverrideReplace, Gravity: v3{X: 1}}
	w.AddBody(hullArea)
	cylArea := physics3d.NewStaticBody(physics3d.CylinderShape(physics3d.Cylinder{A: v3{Z: -1}, B: v3{Z: 1}, Radius: 1}), v3{X: 5})
	cylArea.IsTrigger = true
	cylArea.Area = physics3d.AreaSettings{GravityMode: physics3d.AreaOverrideReplace, Gravity: v3{Z: 1}}
	w.AddBody(cylArea)

	b := wedge.Translated(v3{X: -5}).Centroid()
	for _, tc := range []struct {
		name string
		p    v3
		want v3
	}{
		{"inside the wedge", b, v3{X: 1}},
		{"in the wedge's bounds but above its slope", v3{X: -4.6, Y: 0.4, Z: 0.4}, v3{Y: -10}},
		{"inside the cylinder", v3{X: 5.5, Y: 0.5, Z: 0.9}, v3{Z: 1}},
		{"in the cylinder's bounds but past its round side", v3{X: 5.9, Y: 0.9}, v3{Y: -10}},
		{"past the cylinder's cap", v3{X: 5, Z: 1.1}, v3{Y: -10}},
	} {
		if g := w.GravityAt(tc.p); !nearVec(g, tc.want, 1e-5) {
			t.Errorf("%s: gravity at %v = %v, want %v", tc.name, tc.p, g, tc.want)
		}
	}

	// A barrel half as dense as water floats with half of it under the surface at y = 0.
	pool := physics3d.NewWorld()
	water := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: v3{X: 10, Y: 2, Z: 10}}), v3{Y: -2})
	water.IsTrigger = true
	water.Area = physics3d.AreaSettings{FluidDensity: 1000, FluidLinearDrag: 6, FluidAngularDrag: 4}
	pool.AddBody(water)
	cyl := physics3d.VerticalCylinder(v3{}, 0.3, 1)
	volume := float32(math.Pi) * 0.09
	barrel := pool.AddBody(physics3d.NewDynamicBody(physics3d.CylinderShape(cyl), v3{Y: 0.5}, 500*volume))
	for range 360 {
		pool.Step(step60)
	}
	if !near(barrel.Pos.Y, 0, 0.03) {
		t.Fatalf("barrel floats at y = %v, want about 0", barrel.Pos.Y)
	}
}

// TestCharacterControllerAgainstConvexBodies checks that the character controller stops at a cylinder pillar it walks into and stands on and walks across a convex hull.
func TestCharacterControllerAgainstConvexBodies(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.BoxFromMinMax(v3{X: -10, Y: -1, Z: -10}, v3{X: 10, Y: 0, Z: 10})), v3{}))
	w.AddBody(physics3d.NewStaticBody(physics3d.CylinderShape(physics3d.VerticalCylinder(v3{}, 0.5, 2)), v3{X: 3, Y: 1}))
	cc := physics3d.NewCharacterController(v3{}, 0.3, 1.8)
	runLoop(t, 60, 3*time.Second, func(dt time.Duration) {
		cc.Move(w, v3{X: 2}, dt)
	})
	if cc.Pos.X < 2.1 || cc.Pos.X > 2.21 || !near(cc.Pos.Y, 0, 0.02) || !cc.Grounded {
		t.Fatalf("walking into a pillar: pos %v grounded %v, want stopped at x 2.2 on the floor", cc.Pos, cc.Grounded)
	}

	// A low, wide hull block whose top is at y 1.
	block := physics3d.NewWorld()
	block.AddBody(physics3d.NewStaticBody(physics3d.ConvexHullShape(cubeHull(t, 1)).Transformed(v3{}, physics3d.QuatFromAxisAngle(v3{Y: 1}, 0.3)), v3{}))
	cc = physics3d.NewCharacterController(v3{Y: 1.5}, 0.3, 1.8)
	runLoop(t, 60, time.Second, func(dt time.Duration) {
		cc.Move(block, v3{}, dt)
	})
	if !near(cc.Pos.Y, 1, 0.02) || !cc.Grounded || cc.GroundNormal.Y < 0.99 {
		t.Fatalf("standing on a hull: pos %v grounded %v normal %v, want on its top at y 1", cc.Pos, cc.Grounded, cc.GroundNormal)
	}
	runLoop(t, 60, time.Second/2, func(dt time.Duration) {
		cc.Move(block, v3{X: 1}, dt)
	})
	if !near(cc.Pos.Y, 1, 0.02) || !cc.Grounded || cc.Pos.X < 0.4 {
		t.Fatalf("walking across a hull: pos %v grounded %v, want on its top at y 1", cc.Pos, cc.Grounded)
	}
}

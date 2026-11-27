package physics3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

// stackPenetration builds n unit boxes resting on a floor, the top one topMass heavy and the rest 1 kg, steps it for ten seconds and returns the deepest contact between neighbors, whether every box sleeps, and the top box.
func stackPenetration(t *testing.T, n int, topMass float32) (float32, bool, *physics3d.Body) {
	t.Helper()
	w := physics3d.NewWorld()
	floor := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}), physics3d.Vec3{Y: -1}))
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	boxes := make([]*physics3d.Body, n)
	for i := range n {
		mass := float32(1)
		if i == n-1 {
			mass = topMass
		}
		boxes[i] = w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: float32(i) + 0.5}, mass))
	}
	for range 600 {
		w.Step(step60)
	}
	var deepest float32
	asleep := true
	for i, b := range boxes {
		below := floor
		if i > 0 {
			below = boxes[i-1]
		}
		if c := physics3d.TestShapes(b.WorldShape(), below.WorldShape()); c.Hit {
			deepest = max(deepest, c.Depth)
		}
		asleep = asleep && b.Sleeping
	}
	return deepest, asleep, boxes[n-1]
}

func TestStacksSleepWithoutResidualPenetration(t *testing.T) {
	// Before #546's shock propagation a 100 kg box on a 1 kg box fell asleep 4 cm deep, and a 10-box stack under a 100 kg box never settled, sinking 8 cm.
	for _, tc := range []struct {
		name    string
		n       int
		topMass float32
	}{
		{"heavy on light", 2, 100},
		{"heavy on tall stack", 10, 100},
		{"tall uniform stack", 20, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deepest, asleep, top := stackPenetration(t, tc.n, tc.topMass)
			if deepest > 0.005 {
				t.Errorf("deepest contact = %v m, want at most 5 mm", deepest)
			}
			if !asleep {
				t.Error("stack never fell asleep")
			}
			// Each resting contact keeps about a millimeter of overlap, so the stack may sink at most 2 mm per box.
			if want := float32(tc.n) - 0.5; top.Pos.Y < want-0.002*float32(tc.n) || top.Pos.Y > want+0.001 {
				t.Errorf("top box rests at y = %v, want within %v below %v", top.Pos.Y, 0.002*float32(tc.n), want)
			}
		})
	}
}

func TestStackSettlesDeterministically(t *testing.T) {
	d1, s1, top1 := stackPenetration(t, 10, 100)
	d2, s2, top2 := stackPenetration(t, 10, 100)
	if d1 != d2 || s1 != s2 || top1.Pos != top2.Pos {
		t.Fatalf("two identical runs differ: depth %v/%v, asleep %v/%v, top %v/%v", d1, d2, s1, s2, top1.Pos, top2.Pos)
	}
}

func TestBodyDoesNotSleepWhilePenetrating(t *testing.T) {
	// A unit box pinned between two static walls that each overlap it by 5 cm cannot be pushed out of either, so it must stay awake rather than freeze the overlap.
	wall := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 2}})
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	t.Run("zero gravity", func(t *testing.T) {
		// Without gravity there is no up, so the shock pass is off and the sleep gate alone keeps the box awake.
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}
		w.AddBody(physics3d.NewStaticBody(wall, physics3d.Vec3{X: -0.95}))
		w.AddBody(physics3d.NewStaticBody(wall, physics3d.Vec3{X: 0.95}))
		box := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{}, 1))
		for range 120 {
			w.Step(step60)
		}
		if box.Sleeping {
			t.Fatal("box fell asleep while overlapping both walls by 5 cm")
		}
	})
	t.Run("gravity with sideways contacts", func(t *testing.T) {
		// Under gravity the box rests on a floor; the wall contacts are horizontal, which the shock pass leaves to the solver, so the sleep gate must still see their depth.
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 1, Z: 5}}), physics3d.Vec3{Y: -1}))
		w.AddBody(physics3d.NewStaticBody(wall, physics3d.Vec3{X: -0.95, Y: 2}))
		w.AddBody(physics3d.NewStaticBody(wall, physics3d.Vec3{X: 0.95, Y: 2}))
		box := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 0.5}, 1))
		for range 120 {
			w.Step(step60)
		}
		if box.Sleeping {
			t.Fatal("box fell asleep while overlapping both walls by 5 cm")
		}
		if box.Pos.Y < 0.49 {
			t.Fatalf("box sank to y = %v, want it resting on the floor at 0.5", box.Pos.Y)
		}
	})
}

func TestStackSleepsUnderSidewaysGravity(t *testing.T) {
	// Shock propagation derives up from -Gravity, so a stack against a wall under gravity along -X settles like one on a floor.
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{X: -9.81}
	wall := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 20, Z: 20}}), physics3d.Vec3{X: -1}))
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	light := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 0.5}, 1))
	heavy := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 1.5}, 100))
	for range 600 {
		w.Step(step60)
	}
	for _, pair := range [][2]*physics3d.Body{{light, wall}, {heavy, light}} {
		if c := physics3d.TestShapes(pair[0].WorldShape(), pair[1].WorldShape()); c.Hit && c.Depth > 0.005 {
			t.Errorf("contact depth = %v m, want at most 5 mm", c.Depth)
		}
	}
	if !light.Sleeping || !heavy.Sleeping {
		t.Errorf("sleeping = %v, %v; want both asleep", light.Sleeping, heavy.Sleeping)
	}
	if heavy.Pos.X < 1.5-0.004 {
		t.Errorf("heavy box rests at x = %v, want within 4 mm of 1.5", heavy.Pos.X)
	}
}

func TestStackSleepsUnderCeiling(t *testing.T) {
	// A ceiling contact points down, so the shock pass treats the static ceiling as the upper body and moves nothing into it; a stack with 1 cm of headroom settles and sleeps.
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 1, Z: 5}}), physics3d.Vec3{Y: -1}))
	ceiling := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 1, Z: 5}}), physics3d.Vec3{Y: 3.01}))
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	lower := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 0.5}, 1))
	upper := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 1.5}, 100))
	for range 600 {
		w.Step(step60)
	}
	if !lower.Sleeping || !upper.Sleeping {
		t.Errorf("sleeping = %v, %v; want both asleep", lower.Sleeping, upper.Sleeping)
	}
	if c := physics3d.TestShapes(upper.WorldShape(), ceiling.WorldShape()); c.Hit {
		t.Errorf("top box touches the ceiling %v deep", c.Depth)
	}
}

func TestHeightfieldDepthMeasuredAlongFlippedNormal(t *testing.T) {
	// A flat 2x2-cell heightfield at y = 0.
	hf := physics3d.NewHeightfieldCollider(3, 3, 1, physics3d.Vec3{X: -1, Z: -1}, make([]float32, 9))
	for _, tc := range []struct {
		name  string
		shape physics3d.Shape
		want  float32
	}{
		// Center 0.3 below the surface: the nearest-point test says depth 0.2 pushing down; out the top it is 0.5 + 0.3.
		{"sphere", physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 0.2, Y: -0.3, Z: 0.3}, Radius: 0.5}), 0.8},
		// SAT picks -Y at depth 0.3; out the top the box needs 0.5 + 0.2.
		{"box", physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{X: 0.1, Y: -0.2, Z: 0.2}, HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), 0.7},
		{"capsule", physics3d.CapsuleShape(physics3d.Capsule{A: physics3d.Vec3{X: -0.3, Y: -0.4, Z: 0.1}, B: physics3d.Vec3{X: 0.3, Y: -0.4, Z: 0.1}, Radius: 0.5}), 0.9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := hf.TestShape(tc.shape)
			if !c.Hit {
				t.Fatal("no contact")
			}
			if !approxVec(c.Normal, physics3d.Vec3{Y: 1}, 1e-5) {
				t.Fatalf("normal = %v, want +Y", c.Normal)
			}
			if !approx(c.Depth, tc.want, 1e-5) {
				t.Fatalf("depth = %v, want %v", c.Depth, tc.want)
			}
			// Moving the shape by Normal*Depth leaves it resting on the surface.
			if after := hf.TestShape(tc.shape.Translated(c.Normal.Scale(c.Depth + 1e-4))); after.Hit {
				t.Fatalf("still overlapping by %v after moving out along the contact", after.Depth)
			}
		})
	}
}

func TestSphereBelowHeightfieldSurfacesInOneStep(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	w.AddHeightfield(physics3d.NewHeightfieldCollider(5, 5, 1, physics3d.Vec3{X: -2, Z: -2}, make([]float32, 25)))
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: 0.2, Y: -0.3, Z: 0.3}, 1))
	w.Step(step60)
	if ball.Pos.Y < 0.45 {
		t.Fatalf("sphere sunk 0.3 below the terrain is at y = %v after one step, want it resting on top near 0.5", ball.Pos.Y)
	}
}

func TestHeightfieldLedgeKeepsMeasuredDepth(t *testing.T) {
	// A 0.5 high step: columns 5 and 6 form a plateau, the cell before them a steep slope. A sphere at the foot of the slope touches the plateau's edge from the side; that contact's normal points down and is flipped up, but the sphere is not under the plateau, so it must keep its small measured depth instead of the whole step height (#546).
	heights := make([]float32, 7*3)
	for z := range 3 {
		heights[z*7+5], heights[z*7+6] = 0.5, 0.5
	}
	hf := physics3d.NewHeightfieldCollider(7, 3, 0.2, physics3d.Vec3{X: -0.6, Z: -0.2}, heights)
	sphere := physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 0.02, Y: 0.4}, Radius: 0.4})
	c := hf.TestShape(sphere)
	if !c.Hit {
		t.Fatal("no contact with the slope")
	}
	if c.Depth > 0.2 {
		t.Fatalf("deepest contact = %v along %v, want the slope contact (about 0.08), not the 0.5 step height", c.Depth, c.Normal)
	}
	for _, cc := range hf.CollectContacts(sphere, nil) {
		if cc.Depth > 0.2 {
			t.Fatalf("CollectContacts reported %v along %v, want every contact below 0.2", cc.Depth, cc.Normal)
		}
	}
}

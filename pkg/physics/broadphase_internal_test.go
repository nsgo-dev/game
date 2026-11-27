package physics

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// bruteForceContacts is the contact detection RigidWorld had before the broadphase: every pair i < j, culled by bounding boxes.
func bruteForceContacts(w *RigidWorld, restitutionThresh, h float64) []rigidManifold {
	var manifolds []rigidManifold
	n := len(w.Bodies)
	for i := range n {
		a := w.Bodies[i]
		if a == nil || a.Shape.Empty() {
			continue
		}
		aMinX, aMinY, aMaxX, aMaxY := a.worldAABB()
		for j := i + 1; j < n; j++ {
			b := w.Bodies[j]
			if b == nil || b.Shape.Empty() || (a.Static && b.Static) || !CanCollideRigid(a, b) {
				continue
			}
			bMinX, bMinY, bMaxX, bMaxY := b.worldAABB()
			if aMaxX <= bMinX || bMaxX <= aMinX || aMaxY <= bMinY || bMaxY <= aMinY {
				continue
			}
			first := len(manifolds)
			manifolds = collideRigidPair(a, b, manifolds)
			for k := first; k < len(manifolds); k++ {
				prepareManifold(&manifolds[k], restitutionThresh, h)
			}
		}
	}
	return manifolds
}

// randomRigidWorld builds a crowded scene of n bodies of every shape from seed, with static terrain, filtered layers, nil entries, empty shapes, touching and coincident boxes, and a body whose bounds are NaN.
func randomRigidWorld(seed uint64, n int) *RigidWorld {
	r := rand.New(rand.NewPCG(seed, seed^0x5bd1e995))
	w := &RigidWorld{GravityY: 600, DisableSleep: true}
	coord := func(span float64) float32 { return float32(math.Round(r.Float64()*span*4) / 4) }
	for i := range n {
		x, y := coord(400), coord(300)
		var b *RigidBody
		switch r.IntN(12) {
		case 0:
			b = NewRigidCircle(x, y, 2+coord(10), 1)
		case 1:
			b = NewStaticRigidBox(x, y, 4+coord(80), 4+coord(10))
		case 2:
			b = NewRigidBody(PolygonShape(NewBoxPolygon(x, y, 4+coord(12), 4+coord(12), float32(r.Float64()*3))), 1)
		case 3:
			b = NewRigidBody(CapsuleShape(Capsule{X1: x, Y1: y, X2: x + coord(20), Y2: y + coord(10), R: 1 + coord(4)}), 1)
		case 4:
			b = NewStaticRigidBody(SegmentShape(Segment{X1: x, Y1: y, X2: x + coord(60) - 30, Y2: y + coord(60) - 30}))
		case 5:
			b = NewStaticRigidBody(ChainShape([]core.Point{{X: core.Points(x), Y: core.Points(y)}, {X: core.Points(x + 30), Y: core.Points(y + coord(10))}, {X: core.Points(x + 60), Y: core.Points(y)}}, false))
		case 6:
			// Exactly touching neighbours share a box edge, which both passes cull alike.
			b = NewRigidBox(float32(math.Round(float64(x)/10)*10), float32(math.Round(float64(y)/10)*10), 10, 10, 1)
		case 7:
			b = NewRigidBox(x, y, 0, 10, 1) // empty
		case 8:
			if i%5 == 0 {
				w.Bodies = append(w.Bodies, nil)
				continue
			}
			b = NewRigidBox(x, y, 8, 8, 1)
		case 9:
			b = NewRigidBox(x, y, 6+coord(10), 6+coord(10), 1)
			b.Layer, b.Mask = LayerBit(uint(r.IntN(3))), LayerBit(uint(r.IntN(3)))
		default:
			b = NewRigidBox(x, y, 6+coord(14), 6+coord(14), 1)
			b.Angle = float32(r.Float64() * 3)
		}
		w.AddBody(b)
	}
	if n > 8 {
		// The NaN body and its one partner sit on a layer of their own, so the NaN they exchange does not spread to the rest of the scene.
		nan := NewRigidCircle(float32(math.NaN()), 10, 4, 1)
		partner := NewRigidBox(50, 12, 10, 10, 1)
		nan.Layer, nan.Mask = LayerBit(30), LayerBit(30)
		partner.Layer, partner.Mask = LayerBit(30), LayerBit(30)
		w.Bodies[n/2], w.Bodies[n/2+1] = nan, partner
	}
	return w
}

// prepareForDetection runs the part of Step before contact detection, so the internal passes can be called directly.
func prepareForDetection(w *RigidWorld) {
	w.stamp++
	for i, b := range w.Bodies {
		if b == nil {
			continue
		}
		b.stamp = w.stamp
		b.index = i
		b.ensureLocal()
		b.prepareMass()
	}
}

func sameManifolds(t *testing.T, got, want []rigidManifold) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("broadphase gave %d manifolds, brute force %d", len(got), len(want))
	}
	bits := func(f float64) uint64 { return math.Float64bits(f) }
	for i := range got {
		g, w := got[i], want[i]
		if g.a != w.a || g.b != w.b || g.sub != w.sub || g.count != w.count || bits(g.nx) != bits(w.nx) || bits(g.ny) != bits(w.ny) || bits(g.friction) != bits(w.friction) {
			t.Fatalf("manifold %d differs:\ngot  %+v\nwant %+v", i, g, w)
		}
		for k := range g.count {
			gp, wp := g.points[k], w.points[k]
			if gp.id != wp.id || bits(gp.px) != bits(wp.px) || bits(gp.py) != bits(wp.py) || bits(gp.depth) != bits(wp.depth) || bits(gp.massNormal) != bits(wp.massNormal) {
				t.Fatalf("manifold %d point %d differs:\ngot  %+v\nwant %+v", i, k, gp, wp)
			}
		}
	}
}

func checkBroadphaseMatchesBruteForce(t *testing.T, seed uint64, n int, steps int) {
	t.Helper()
	w := randomRigidWorld(seed, n)
	const h = 1.0 / 60
	for s := 0; ; s++ {
		prepareForDetection(w)
		want := bruteForceContacts(w, 10, h)
		got := w.detectContacts(w.broadphase(), 10, h, nil)
		sameManifolds(t, got, want)
		if s == steps {
			return
		}
		w.Step(time.Second / 60)
	}
}

func TestBroadphaseMatchesBruteForce(t *testing.T) {
	for seed := range uint64(40) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			checkBroadphaseMatchesBruteForce(t, seed, 10+int(seed)*3, 20)
		})
	}
}

func FuzzBroadphaseMatchesBruteForce(f *testing.F) {
	f.Add(uint64(1), uint8(30))
	f.Add(uint64(7), uint8(200))
	f.Add(uint64(99), uint8(3))
	f.Fuzz(func(t *testing.T, seed uint64, n uint8) {
		checkBroadphaseMatchesBruteForce(t, seed, int(n), 3)
	})
}

func TestBroadphaseSkipsSleepingPairs(t *testing.T) {
	w := &RigidWorld{GravityY: 600}
	floor := NewStaticRigidBox(100, 110, 400, 20)
	a := NewRigidBox(80, 90, 20, 20, 1)
	b := NewRigidBox(120, 90, 20, 20, 1)
	w.AddBody(floor, a, b)
	for range 120 {
		w.Step(time.Second / 60)
	}
	if !a.Sleeping() || !b.Sleeping() {
		t.Fatal("boxes did not sleep")
	}
	prepareForDetection(w)
	if got := w.detectContacts(w.broadphase(), 10, 1.0/60, nil); len(got) != 0 {
		t.Fatalf("sleeping bodies produced %d manifolds", len(got))
	}
	// Their contacts stay cached while asleep, ready to warm-start them when they wake.
	if len(w.contacts) != 2 {
		t.Fatalf("cache holds %d manifolds while asleep, want the 2 floor contacts", len(w.contacts))
	}
}

func TestWarmStartCarriesRestingImpulse(t *testing.T) {
	w := &RigidWorld{GravityY: 600, DisableSleep: true}
	floor := NewStaticRigidBox(100, 110, 400, 20)
	crate := NewRigidBox(100, 90, 20, 20, 2)
	w.AddBody(floor, crate)
	for range 60 {
		w.Step(time.Second / 60)
	}
	if len(w.contacts) != 1 || w.contacts[0].count != 2 {
		t.Fatalf("cache = %+v, want one two-point manifold", w.contacts)
	}
	// The floor carries the crate's weight: the two points' normal impulses add up to m*g*h.
	c := w.contacts[0]
	sum := c.points[0].pn + c.points[1].pn
	if want := 2 * 600.0 / 60; math.Abs(sum-want) > 0.05*want {
		t.Fatalf("cached normal impulse %v, want %v", sum, want)
	}
	if c.points[0].id == c.points[1].id {
		t.Fatalf("both points share feature id %#x", c.points[0].id)
	}
	// A step twice as long starts from twice the impulse.
	prepareForDetection(w)
	ms := w.detectContacts(w.broadphase(), 10, 2.0/60, nil)
	crate.VX, crate.VY, crate.AngularVel = 0, 0, 0
	w.warmStart(ms, 2.0/60)
	if got := ms[0].points[0].pn + ms[0].points[1].pn; math.Abs(got-2*sum) > 1e-4 {
		t.Fatalf("warm start for a 2x step = %v, want %v", got, 2*sum)
	}
}

func TestWarmStartHoldsTallTower(t *testing.T) {
	// Twelve boxes stacked with the default iterations: impulses carried across steps hold the tower up and let it sleep, while a solver that restarts every contact from zero lets it buckle.
	build := func() (*RigidWorld, *RigidBody) {
		w := &RigidWorld{GravityY: 600}
		w.AddBody(NewStaticRigidBox(100, 410, 400, 20))
		var top *RigidBody
		for i := range 12 {
			top = NewRigidBox(100, float32(390-20*i), 20, 20, 1)
			w.AddBody(top)
		}
		return w, top
	}
	w, top := build()
	for range 180 {
		w.Step(time.Second / 60)
	}
	if math.Abs(float64(top.X)-100) > 1 || math.Abs(float64(top.Y)-170) > 2 || !top.Sleeping() {
		t.Fatalf("warm-started tower top at (%v, %v) sleeping=%v, want asleep near (100, 170)", top.X, top.Y, top.Sleeping())
	}
	cold, coldTop := build()
	for range 180 {
		cold.contacts = nil
		cold.Step(time.Second / 60)
	}
	if math.Abs(float64(coldTop.Y)-170) < 2 {
		t.Fatalf("cold tower top at (%v, %v) also stands; the test no longer shows warm starting", coldTop.X, coldTop.Y)
	}
}

func TestBroadphaseScalesWithContactsNotBodyPairs(t *testing.T) {
	// 500 boxes packed in a grid with 1 px gaps, the bottom row sunk half a pixel into the floor: the brute-force pass would run its box test on all 124,750 pairs, the broadphase reports only the floor contacts of the bottom row and nothing else.
	w := &RigidWorld{GravityY: 600}
	w.AddBody(NewStaticRigidBox(300, 510, 700, 20))
	for i := range 500 {
		w.AddBody(NewRigidBox(float32(11*(i%25)), 495.5-11*float32(i/25), 10, 10, 1))
	}
	prepareForDetection(w)
	if got := len(w.broadphase()); got != 25 {
		t.Fatalf("broadphase reported %d pairs, want the 25 floor contacts", got)
	}
}

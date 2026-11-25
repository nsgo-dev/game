package physics_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

const step60 = time.Second / 60

// tileFloor returns a 40 by 12 grid of 16 px tiles whose row 10 (y 160 to 176) is solid.
func tileFloor() *physics.Grid {
	g := physics.NewGrid(40, 12, 16, 16)
	g.FillRect(0, 10, 40, 1, physics.TileSolid)
	return g
}

func stepFor(w *physics.RigidWorld, d time.Duration) {
	for range int(d / step60) {
		w.Step(step60)
	}
}

func TestRigidCrateRestsOnTileFloor(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor()}
	crate := physics.NewRigidBox(100, 60, 20, 20, 1)
	ball := physics.NewRigidCircle(200, 40, 8, 1)
	w.AddBody(crate, ball)
	stepFor(w, 3*time.Second)

	if bottom := crate.Y + 10; math.Abs(float64(bottom-160)) > 0.5 {
		t.Fatalf("crate bottom = %v, want it resting on the floor at 160", bottom)
	}
	if bottom := ball.Y + 8; math.Abs(float64(bottom-160)) > 0.5 {
		t.Fatalf("ball bottom = %v, want it resting on the floor at 160", bottom)
	}
	if !crate.Sleeping() || !ball.Sleeping() {
		t.Fatalf("sleeping = %v, %v after 3 s at rest, want both asleep", crate.Sleeping(), ball.Sleeping())
	}
}

func TestRigidTileWallsAndCeilingsHoldBodies(t *testing.T) {
	g := tileFloor()
	g.FillRect(20, 0, 1, 10, physics.TileSolid) // a wall at x 320 to 336
	w := &physics.RigidWorld{GravityY: 600, Tiles: g}
	crate := physics.NewRigidBox(290, 150, 20, 20, 1)
	crate.VX = 400
	w.AddBody(crate)
	stepFor(w, 2*time.Second)
	if right := crate.X + 10; right > 320.5 {
		t.Fatalf("crate right = %v, want it stopped by the wall at 320", right)
	}

	// A crate thrown up under a solid ceiling stops below it.
	g2 := physics.NewGrid(10, 10, 16, 16)
	g2.FillRect(0, 2, 10, 1, physics.TileSolid) // y 32 to 48
	w2 := &physics.RigidWorld{Tiles: g2}
	up := physics.NewRigidBox(80, 100, 16, 16, 1)
	up.VY = -300
	w2.AddBody(up)
	stepFor(w2, time.Second)
	if top := up.Y - 8; top < 47.5 {
		t.Fatalf("crate top = %v, want it stopped below the ceiling at 48", top)
	}
}

func TestRigidBodiesSlideAndRollAcrossTileSeams(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func() *physics.RigidBody
		half float32
	}{
		{"box", func() *physics.RigidBody { return physics.NewRigidBox(40, 150, 20, 20, 1) }, 10},
		{"circle", func() *physics.RigidBody { return physics.NewRigidCircle(40, 152, 8, 1) }, 8},
		{"capsule", func() *physics.RigidBody {
			return physics.NewRigidBody(physics.CapsuleShape(physics.Capsule{X1: 30, Y1: 152, X2: 50, Y2: 152, R: 8}), 1)
		}, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Frictionless, so a box or capsule that catches on a seam shows it as lost speed; the circle rolls.
			w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor(), TileFriction: -1}
			b := tc.body()
			if tc.name == "circle" {
				w.TileFriction = 0
			}
			w.AddBody(b)
			stepFor(w, time.Second/2)
			b.VX = 150
			if tc.name == "circle" {
				b.AngularVel = 150 / 8 // already rolling, so friction does not slow it
			}
			startX := b.X
			for range 120 {
				w.Step(step60)
				// A seam would kick the body up or tilt a box off the floor.
				if bottom := b.Y + tc.half; bottom < 159.9 || bottom > 160.5 {
					t.Fatalf("bottom = %v at x %v, want the body on the floor surface at 160", bottom, b.X)
				}
				if tc.name == "box" && math.Abs(float64(b.Angle)) > 1e-3 {
					t.Fatalf("box angle = %v at x %v, want it flat on the floor", b.Angle, b.X)
				}
			}
			if moved := b.X - startX; moved < 280 {
				t.Fatalf("moved %v px across %d seams in 2 s, want the body to keep its speed of 150 px/s", moved, int(moved/16))
			}
		})
	}
}

func TestRigidOneWayTileHoldsFromAboveOnly(t *testing.T) {
	g := physics.NewGrid(20, 20, 16, 16)
	g.FillRect(0, 10, 20, 1, physics.TileOneWay) // y 160 to 176
	g.FillRect(0, 19, 20, 1, physics.TileSolid)  // y 304 to 320
	w := &physics.RigidWorld{GravityY: 600, Tiles: g}
	dropped := physics.NewRigidBox(60, 100, 16, 16, 1)
	jumper := physics.NewRigidBox(200, 290, 16, 16, 1)
	jumper.VY = -600
	w.AddBody(dropped, jumper)
	stepFor(w, 3*time.Second)
	if bottom := dropped.Y + 8; math.Abs(float64(bottom-160)) > 0.5 {
		t.Fatalf("dropped crate bottom = %v, want it standing on the one-way tiles at 160", bottom)
	}
	if bottom := jumper.Y + 8; math.Abs(float64(bottom-160)) > 0.5 {
		t.Fatalf("jumping crate bottom = %v, want it through the one-way tiles from below and landed on top at 160", bottom)
	}
}

func TestRigidFastBodyStopsOnOneTileFloor(t *testing.T) {
	for _, speed := range []float32{3000, 12000} {
		w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor()}
		shot := physics.NewRigidCircle(100, 20, 3, 0.1)
		shot.VY = speed
		w.AddBody(shot)
		stepFor(w, time.Second/2)
		if shot.Y > 160 {
			t.Fatalf("at %v px/s the shot ended at y %v, want it stopped above the floor at 160", speed, shot.Y)
		}
	}
}

func TestRigidSleepingBodyWakesWhenItsTileIsRemoved(t *testing.T) {
	g := tileFloor()
	w := &physics.RigidWorld{GravityY: 600, Tiles: g}
	crate := physics.NewRigidBox(104, 150, 16, 16, 1) // over tile column 6 only
	w.AddBody(crate)
	stepFor(w, 2*time.Second)
	if !crate.Sleeping() {
		t.Fatal("crate did not fall asleep on the floor")
	}
	g.Set(6, 10, 0)
	w.Step(step60)
	if crate.Sleeping() {
		t.Fatal("crate kept sleeping after the tile under it was removed")
	}
	stepFor(w, time.Second/2)
	if crate.Y < 170 {
		t.Fatalf("crate y = %v, want it falling through the hole", crate.Y)
	}
}

func TestRigidTileLayersFilterBodies(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor(), TileLayer: physics.LayerBit(1), TileMask: physics.LayerBit(1)}
	solid := physics.NewRigidBox(60, 100, 16, 16, 1)
	solid.Layer, solid.Mask = physics.LayerBit(1), physics.LayerBit(1)
	ghost := physics.NewRigidBox(120, 100, 16, 16, 1)
	ghost.Layer, ghost.Mask = physics.LayerBit(2), physics.LayerBit(2)
	w.AddBody(solid, ghost)
	stepFor(w, time.Second)
	if solid.Y > 153 {
		t.Fatalf("solid crate y = %v, want it on the floor", solid.Y)
	}
	if ghost.Y < 200 {
		t.Fatalf("ghost crate y = %v, want it through the floor it does not collide with", ghost.Y)
	}
}

func TestRigidKinematicPlatformCarriesCrate(t *testing.T) {
	for _, mode := range []string{"velocity", "target"} {
		t.Run(mode, func(t *testing.T) {
			w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor()}
			platform := physics.NewRigidBox(100, 120, 96, 12, 5)
			platform.Kinematic = true
			crate := physics.NewRigidBox(100, 90, 20, 20, 1)
			w.AddBody(platform, crate)
			stepFor(w, time.Second)
			if bottom := crate.Y + 10; math.Abs(float64(bottom-114)) > 0.5 {
				t.Fatalf("crate bottom = %v, want it resting on the platform top at 114", bottom)
			}
			if platform.Y != 120 || platform.VY != 0 || platform.Angle != 0 {
				t.Fatalf("platform at y %v, vy %v, angle %v, want the crate to leave it unmoved", platform.Y, platform.VY, platform.Angle)
			}

			platformStart := platform.X
			var crateMid, platformMid float32
			for i := range 90 {
				if i == 30 {
					// The crate slips while friction brings it up to the platform's speed; from then on it rides.
					crateMid, platformMid = crate.X, platform.X
				}
				switch mode {
				case "velocity":
					platform.VX = 60
				case "target":
					platform.SetTargetPose(platformStart+float32(i+1), 120, 0)
				}
				w.Step(step60)
			}
			moved := platform.X - platformStart
			if want := float32(90); mode == "target" && moved != want {
				t.Fatalf("platform moved %v px by its targets, want exactly %v", moved, want)
			}
			if carried, rode := crate.X-crateMid, platform.X-platformMid; math.Abs(float64(carried-rode)) > 0.5 || math.Abs(float64(crate.VX-60)) > 0.5 {
				t.Fatalf("crate moved %v px at %v px/s while the platform moved %v px at 60 px/s, want it carried along", carried, crate.VX, rode)
			}
			if bottom := crate.Y + 10; math.Abs(float64(bottom-114)) > 0.5 {
				t.Fatalf("crate bottom = %v, want it still riding the platform top at 114", bottom)
			}
			if mode == "target" {
				// No new target: the platform stops where the last one put it.
				w.Step(step60)
				if platform.VX != 0 || platform.X != platformStart+90 {
					t.Fatalf("platform vx %v at x %v after its last target, want it stopped at %v", platform.VX, platform.X, platformStart+90)
				}
			}
		})
	}
}

func TestRigidKinematicBodyPushesAndIsNotPushed(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor()}
	pusher := physics.NewRigidBox(40, 140, 16, 40, 1)
	pusher.Kinematic = true
	crate := physics.NewRigidBox(100, 150, 20, 20, 1)
	w.AddBody(pusher, crate)
	stepFor(w, time.Second/2)
	for range 120 {
		pusher.VX = 80
		w.Step(step60)
	}
	if pusher.VX != 80 || pusher.Y != 140 {
		t.Fatalf("pusher vx %v at y %v, want the crate to leave it unaffected", pusher.VX, pusher.Y)
	}
	if gap := (crate.X - 10) - (pusher.X + 8); gap < -0.5 || gap > 1 {
		t.Fatalf("gap between pusher and crate = %v, want the crate pushed ahead of it", gap)
	}
	// Gravity, forces and impulses do not move a kinematic body.
	pusher.VX = 0
	pusher.ApplyForce(0, 1000)
	pusher.ApplyImpulse(0, 1000)
	x, y := pusher.X, pusher.Y
	w.Step(step60)
	if pusher.X != x || pusher.Y != y {
		t.Fatalf("pusher moved from (%v, %v) to (%v, %v) with no velocity", x, y, pusher.X, pusher.Y)
	}
}

func TestRigidKinematicPlatformWakesSleepingCrate(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600}
	platform := physics.NewRigidBox(100, 120, 96, 12, 0)
	platform.Static = false
	platform.Kinematic = true
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	w.AddBody(platform, crate)
	stepFor(w, 2*time.Second)
	if !crate.Sleeping() {
		t.Fatal("crate did not fall asleep on the still platform")
	}
	platform.VY = -30
	w.Step(step60)
	if crate.Sleeping() {
		t.Fatal("crate kept sleeping on a platform that started moving")
	}
	stepFor(w, time.Second)
	if bottom := crate.Y + 10; math.Abs(float64(bottom-(platform.Y-6))) > 0.5 {
		t.Fatalf("crate bottom = %v, platform top = %v, want it lifted with the platform", bottom, platform.Y-6)
	}
}

func TestRigidRaycastFirstReturnsNearestBody(t *testing.T) {
	w := &physics.RigidWorld{Tiles: tileFloor()}
	far := physics.NewStaticRigidBox(300, 100, 20, 20)
	near := physics.NewRigidBox(200, 100, 20, 20, 1)
	near.Angle = math.Pi / 4
	ghost := physics.NewStaticRigidBox(150, 100, 20, 20)
	ghost.Layer = physics.LayerBit(3)
	sensor := physics.NewStaticRigidBox(120, 100, 20, 20)
	sensor.Sensor = true
	w.AddBody(far, near, ghost, sensor)

	hit, ok := w.RaycastFirst(core.Point{X: 0, Y: 100}, core.Point{X: 400, Y: 100}, physics.QueryAll, physics.LayerBit(0))
	if !ok || hit.RigidBody != near {
		t.Fatalf("hit = %+v, want the turned box, the nearest body on layer 0", hit)
	}
	// The turned box's corner points at the ray: 200 - 10*sqrt(2).
	if wantX := 200 - 10*math.Sqrt2; math.Abs(float64(hit.Point.X)-wantX) > 1e-3 || math.Abs(float64(hit.Point.Y)-100) > 1e-3 {
		t.Fatalf("point = %v, want (%.3f, 100)", hit.Point, wantX)
	}
	if hit.NormalX >= 0 || math.Abs(float64(hit.NormalX*hit.NormalX+hit.NormalY*hit.NormalY)-1) > 1e-5 {
		t.Fatalf("normal = (%v, %v), want a unit normal facing the ray", hit.NormalX, hit.NormalY)
	}

	// Every layer: the ghost is nearer; with sensors asked for, the sensor is nearer still.
	if hit, _ := w.RaycastFirst(core.Point{X: 0, Y: 100}, core.Point{X: 400, Y: 100}, 0, 0); hit.RigidBody != ghost {
		t.Fatalf("hit = %+v, want the ghost box on any layer", hit)
	}
	if hit, _ := w.RaycastFirst(core.Point{X: 0, Y: 100}, core.Point{X: 400, Y: 100}, physics.QueryAll|physics.QuerySensors, 0); hit.RigidBody != sensor || hit.Point.X != 110 || hit.NormalX != -1 {
		t.Fatalf("hit = %+v, want the sensor's left face at x 110", hit)
	}

	all := w.Raycast(core.Point{X: 0, Y: 100}, core.Point{X: 400, Y: 100}, 0, 0)
	if len(all) != 3 || all[0].RigidBody != ghost || all[1].RigidBody != near || all[2].RigidBody != far {
		t.Fatalf("Raycast = %+v, want ghost, near and far in distance order", all)
	}

	// Straight down onto the floor tiles.
	down, ok := w.RaycastFirst(core.Point{X: 40, Y: 0}, core.Point{X: 40, Y: 300}, physics.QueryTiles, 0)
	if !ok || !down.Tile || down.Col != 2 || down.Row != 10 || down.Point.Y != 160 || down.NormalY != -1 {
		t.Fatalf("hit = %+v, want tile (2, 10) at y 160", down)
	}
}

func TestRigidPointAndOverlapQueries(t *testing.T) {
	w := &physics.RigidWorld{Tiles: tileFloor()}
	turned := physics.NewRigidBox(100, 100, 40, 10, 1)
	turned.Angle = math.Pi / 2 // now 10 wide and 40 tall
	circle := physics.NewRigidCircle(104, 100, 6, 1)
	sensor := physics.NewStaticRigidBox(100, 100, 50, 50)
	sensor.Sensor = true
	w.AddBody(turned, circle, sensor)

	if got := w.QueryPoint(100, 118, 0, 0); len(got) != 1 || got[0] != turned {
		t.Fatalf("QueryPoint(100, 118) = %v, want the turned box only", got)
	}
	if got := w.QueryPoint(118, 100, 0, 0); len(got) != 0 {
		t.Fatalf("QueryPoint(118, 100) = %v, want nothing outside the turned box", got)
	}
	if got := w.QueryPoint(104, 100, physics.QueryBodies|physics.QuerySensors, 0); len(got) != 3 || got[0] != turned || got[1] != circle || got[2] != sensor {
		t.Fatalf("QueryPoint(104, 100) = %v, want all three in body order", got)
	}

	hits := w.QueryOverlap(physics.BoxShape(physics.Box{X: 90, Y: 110, W: 20, H: 60}), 0, 0)
	if len(hits) < 3 || hits[0].RigidBody != turned {
		t.Fatalf("QueryOverlap = %+v, want the turned box then floor tiles", hits)
	}
	for i, h := range hits[1:] {
		if !h.Tile || h.Row != 10 || h.Col != 5+i {
			t.Fatalf("hit %d = %+v, want floor tiles 5 and 6 in row-major order", i+1, h)
		}
	}
	if hits := w.QueryOverlap(physics.CircleShape(physics.Circle{X: 100, Y: 100, R: 2}), physics.QueryBodies, 0); len(hits) != 2 || hits[0].RigidBody != turned || hits[1].RigidBody != circle {
		t.Fatalf("QueryOverlap(circle) = %+v, want the turned box and the circle", hits)
	}

	// Shape casts skip sensors unless asked for them.
	probe := physics.CircleShape(physics.Circle{X: 40, Y: 100, R: 2})
	if hit, ok := w.ShapeCast(probe, 100, 0, 0); !ok || hit.RigidBody != turned {
		t.Fatalf("ShapeCast = %+v, %v, want the turned box past the sensor", hit, ok)
	}
	if hit, ok := w.ShapeCastWith(probe, 100, 0, physics.ShapeCastOptions{QueryMask: physics.QueryBodies | physics.QuerySensors}); !ok || hit.RigidBody != sensor {
		t.Fatalf("ShapeCastWith(QuerySensors) = %+v, %v, want the sensor", hit, ok)
	}
}

// contactLog records every contact and sensor event of a world as text, naming bodies by Tag.
type contactLog []string

func (l *contactLog) attach(w *physics.RigidWorld, step *int) {
	w.OnContact = func(ev physics.ContactEvent) {
		b := "tiles"
		if ev.BodyB != nil {
			b = ev.BodyB.Tag
		}
		*l = append(*l, fmt.Sprintf("%d %s %s-%s (%d,%d) p=(%.4f,%.4f) n=(%.4f,%.4f) v=%.4f", *step, ev.Kind, ev.BodyA.Tag, b, ev.Col, ev.Row, ev.Point.X, ev.Point.Y, ev.NormalX, ev.NormalY, ev.ApproachSpeed))
	}
	w.OnSensor = func(ev physics.SensorEvent) {
		*l = append(*l, fmt.Sprintf("%d sensor %s %s-%s", *step, ev.Kind, ev.Sensor.Tag, ev.Visitor.Tag))
	}
}

func TestRigidLandingBoxEmitsOneBeginWithImpactSpeedAndEndWhenLifted(t *testing.T) {
	for _, floorKind := range []string{"body", "tiles"} {
		t.Run(floorKind, func(t *testing.T) {
			w := &physics.RigidWorld{GravityY: 600}
			if floorKind == "tiles" {
				w.Tiles = tileFloor()
			} else {
				floor := physics.NewStaticRigidBox(320, 168, 640, 16)
				floor.Tag = "floor"
				w.AddBody(floor)
			}
			box := physics.NewRigidBox(100, 50, 20, 20, 1)
			box.Tag = "box"
			w.AddBody(box)

			var begins, ends []physics.ContactEvent
			w.OnContact = func(ev physics.ContactEvent) {
				if ev.Kind == physics.ContactBegin {
					begins = append(begins, ev)
				} else {
					ends = append(ends, ev)
				}
			}
			// Falling 100 px from rest under 600 px/s² meets the floor at about sqrt(2*600*100) ≈ 346 px/s.
			stepFor(w, 3*time.Second)
			if len(begins) != 1 || len(ends) != 0 {
				t.Fatalf("begins %d, ends %d after landing and resting, want 1 and 0", len(begins), len(ends))
			}
			ev := begins[0]
			down := float32(1)
			if floorKind == "tiles" {
				if ev.BodyA != box || !ev.Tile || ev.BodyB != nil || ev.Row != 10 || ev.TileFlags != physics.TileSolid {
					t.Fatalf("begin = %+v, want the box against floor tile row 10", ev)
				}
			} else {
				// The floor was added first, so it is BodyA and the normal points from it up to the box.
				if ev.BodyA != w.Bodies[0] || ev.BodyB != box || ev.Tile {
					t.Fatalf("begin = %+v, want the floor and the box", ev)
				}
				down = -1
			}
			if math.Abs(float64(ev.ApproachSpeed)-346) > 15 {
				t.Fatalf("approach speed = %v, want the impact speed of about 346 px/s", ev.ApproachSpeed)
			}
			if ev.NormalY*down < 0.99 || math.Abs(float64(ev.Point.Y-160)) > 6 {
				t.Fatalf("normal (%v, %v) at %v, want it vertical, from BodyA toward BodyB, at the floor's top y 160", ev.NormalX, ev.NormalY, ev.Point)
			}
			if !box.Sleeping() {
				t.Fatal("box did not fall asleep, so the sleeping case is not covered")
			}

			box.Y -= 40 // lifted off the floor
			w.Step(step60)
			if len(ends) != 1 || (ends[0].BodyA != box && ends[0].BodyB != box) || len(begins) != 1 {
				t.Fatalf("begins %d, ends %+v after lifting, want one end event for the box", len(begins), ends)
			}
			if got := w.ContactEvents(); len(got) != 1 || got[0].Kind != physics.ContactEnd {
				t.Fatalf("ContactEvents = %+v, want the end event", got)
			}
		})
	}
}

func TestRigidContactEndsWhenBodyLeavesWorld(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600, Tiles: tileFloor()}
	box := physics.NewRigidBox(100, 140, 20, 20, 1)
	w.AddBody(box)
	stepFor(w, time.Second)
	w.RemoveBody(box)
	w.Step(step60)
	if ev := w.ContactEvents(); len(ev) != 1 || ev[0].Kind != physics.ContactEnd || ev[0].BodyA != box || !ev[0].Tile {
		t.Fatalf("ContactEvents = %+v, want the box's contact with the tiles to end", ev)
	}
}

func TestRigidSensorReportsBodiesPassingThrough(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600}
	zone := physics.NewStaticRigidBox(100, 150, 200, 40)
	zone.Sensor = true
	zone.Tag = "zone"
	other := physics.NewStaticRigidBox(100, 150, 10, 10)
	other.Sensor = true
	other.Tag = "other"
	ball := physics.NewRigidCircle(100, 50, 5, 1)
	ball.Tag = "ball"
	w.AddBody(zone, other, ball)
	var got []string
	w.OnSensor = func(ev physics.SensorEvent) {
		got = append(got, fmt.Sprintf("%s %s-%s", ev.Kind, ev.Sensor.Tag, ev.Visitor.Tag))
	}
	w.OnContact = func(ev physics.ContactEvent) {
		t.Errorf("contact event %+v, want sensors to collide with nothing", ev)
	}
	stepFor(w, time.Second)
	if ball.Y < 250 {
		t.Fatalf("ball y = %v, want it through the sensors, which do not collide", ball.Y)
	}
	want := []string{"begin zone-ball", "begin other-ball", "end other-ball", "end zone-ball"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sensor events = %q, want %q (and no sensor seeing a sensor)", got, want)
	}
}

// eventScene builds a deterministic scene of boxes and circles raining onto tiles, a static ramp and a kinematic platform, with a sensor.
func eventScene() *physics.RigidWorld {
	g := tileFloor()
	g.FillRect(30, 4, 1, 6, physics.TileSolid)
	g.FillRect(8, 6, 6, 1, physics.TileOneWay)
	w := &physics.RigidWorld{GravityY: 600, Tiles: g}
	ramp := physics.NewStaticRigidBody(physics.SegmentShape(physics.Segment{X1: 300, Y1: 100, X2: 460, Y2: 150}))
	ramp.Tag = "ramp"
	platform := physics.NewRigidBox(200, 120, 64, 8, 1)
	platform.Kinematic = true
	platform.VX = 20
	platform.Tag = "platform"
	zone := physics.NewStaticRigidBox(380, 60, 120, 40)
	zone.Sensor = true
	zone.Tag = "zone"
	w.AddBody(ramp, platform, zone)
	for i := range 24 {
		x := 120 + float32(i%8)*36 + float32(i/8)*5
		y := 10 - float32(i/8)*30
		var b *physics.RigidBody
		if i%3 == 0 {
			b = physics.NewRigidCircle(x, y, 6, 1)
		} else {
			b = physics.NewRigidBox(x, y, 12, 12, 1)
			b.Angle = float32(i) * 0.1
		}
		b.Restitution = 0.2
		b.Tag = fmt.Sprint("b", i)
		w.AddBody(b)
	}
	return w
}

func TestRigidContactEventsAreDeterministic(t *testing.T) {
	run := func(restoreAt int) (contactLog, uint64) {
		w := eventScene()
		var log contactLog
		step := 0
		log.attach(w, &step)
		for step = 0; step < 240; step++ {
			if step == restoreAt {
				// Round-trip the world through JSON mid-run; the events that follow must not change.
				data, err := json.Marshal(w.Save())
				if err != nil {
					t.Fatal(err)
				}
				var snap physics.RigidWorldSnapshot
				if err := json.Unmarshal(data, &snap); err != nil {
					t.Fatal(err)
				}
				fresh := eventScene()
				if err := fresh.Restore(snap); err != nil {
					t.Fatal(err)
				}
				if fresh.Checksum() != w.Checksum() {
					t.Fatalf("restored checksum %x, want %x", fresh.Checksum(), w.Checksum())
				}
				w = fresh
				log.attach(w, &step)
			}
			w.Step(step60)
		}
		return log, w.Checksum()
	}
	first, sum := run(-1)
	if len(first) < 20 {
		t.Fatalf("only %d events, want a scene that exercises begin and end events", len(first))
	}
	var hasTile, hasSensor, hasEnd bool
	for _, l := range first {
		hasTile = hasTile || strings.Contains(l, "tiles")
		hasSensor = hasSensor || strings.Contains(l, "sensor")
		hasEnd = hasEnd || strings.Contains(l, " end ")
	}
	if !hasTile || !hasSensor || !hasEnd {
		t.Fatalf("tile %v, sensor %v, end %v events, want all three kinds in the scene", hasTile, hasSensor, hasEnd)
	}
	for _, restoreAt := range []int{-1, 37, 150} {
		again, againSum := run(restoreAt)
		if fmt.Sprint(again) != fmt.Sprint(first) || againSum != sum {
			for i := range min(len(again), len(first)) {
				if again[i] != first[i] {
					t.Fatalf("restore at %d: event %d = %q, want %q", restoreAt, i, again[i], first[i])
				}
			}
			t.Fatalf("restore at %d: %d events and checksum %x, want %d and %x", restoreAt, len(again), againSum, len(first), sum)
		}
	}
}

func TestRigidKinematicAndSensorSnapshotRoundTrip(t *testing.T) {
	w := eventScene()
	stepFor(w, time.Second/2)
	w.Bodies[1].SetTargetPose(250, 110, 0.1)
	snap := w.Save()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back physics.RigidWorldSnapshot
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	var restored physics.RigidWorld
	if err := restored.Restore(back); err != nil {
		t.Fatal(err)
	}
	if restored.Checksum() != w.Checksum() {
		t.Fatalf("restored checksum %x, want %x", restored.Checksum(), w.Checksum())
	}
	if again, _ := json.Marshal(restored.Save()); string(again) != string(data) {
		t.Fatal("saving the restored world gives a different snapshot")
	}
	p := restored.Bodies[1]
	if !p.Kinematic || !restored.Bodies[2].Sensor {
		t.Fatal("restored bodies lost their kinematic or sensor flag")
	}
	w.Step(step60)
	restored.Step(step60)
	if p.X != 250 || p.Y != 110 || restored.Checksum() != w.Checksum() {
		t.Fatalf("restored platform at (%v, %v), checksum %x; want (250, 110) and %x", p.X, p.Y, restored.Checksum(), w.Checksum())
	}
}

func BenchmarkRigidWorldTileLevel(b *testing.B) {
	g := physics.NewGrid(80, 40, 16, 16)
	g.FillRect(0, 38, 80, 2, physics.TileSolid)
	for c := 0; c < 80; c += 10 {
		g.FillRect(c, 30, 4, 1, physics.TileSolid)
	}
	w := &physics.RigidWorld{GravityY: 600, Tiles: g, DisableSleep: true}
	for i := range 200 {
		w.AddBody(physics.NewRigidBox(20+float32(i%60)*20, 20+float32(i/60)*30, 12, 12, 1))
	}
	for range 120 {
		w.Step(step60)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Step(step60)
	}
}

// fastLandingWorld returns a world whose floor top is at y 2000, a static body or a row of 16 px tiles, with body dropped onto it.
func fastLandingWorld(floorKind string, body *physics.RigidBody) *physics.RigidWorld {
	w := &physics.RigidWorld{GravityY: 600}
	if floorKind == "tiles" {
		g := physics.NewGrid(40, 130, 16, 16)
		g.FillRect(0, 125, 40, 1, physics.TileSolid) // y 2000 to 2016
		w.Tiles = g
	} else {
		w.AddBody(physics.NewStaticRigidBox(320, 2008, 640, 16))
	}
	w.AddBody(body)
	return w
}

func TestRigidFastLandingReportsItsImpactSpeedAndRests(t *testing.T) {
	for _, floorKind := range []string{"body", "tiles"} {
		for _, tc := range []struct {
			name string
			body func() *physics.RigidBody
			half float32
		}{
			// Dropped 900 px, a box meets the floor at about sqrt(2*600*900) ≈ 1039 px/s and moves more than half its size in a step, so continuous collision stops it.
			{"box", func() *physics.RigidBody { return physics.NewRigidBox(104, 2000-10-900, 20, 20, 1) }, 10},
			{"small box", func() *physics.RigidBody { return physics.NewRigidBox(104, 2000-4-900, 8, 8, 1) }, 4},
			{"bullet circle", func() *physics.RigidBody {
				c := physics.NewRigidCircle(104, 2000-3-100, 3, 0.1)
				c.VY = 1500
				c.Bullet = true
				return c
			}, 3},
		} {
			t.Run(floorKind+"/"+tc.name, func(t *testing.T) {
				body := tc.body()
				w := fastLandingWorld(floorKind, body)
				var begins, ends []physics.ContactEvent
				var speed float32
				w.OnContact = func(ev physics.ContactEvent) {
					if ev.Kind == physics.ContactBegin {
						begins = append(begins, ev)
					} else {
						ends = append(ends, ev)
					}
				}
				for range 600 {
					if body.VY > speed {
						speed = body.VY
					}
					w.Step(step60)
				}
				if len(begins) != 1 || len(ends) != 0 {
					t.Fatalf("begins %+v, ends %+v, want one begin and no end", begins, ends)
				}
				if speed < 1000 {
					t.Fatalf("fastest fall %v px/s, want at least 1000 px/s so continuous collision runs", speed)
				}
				if got := begins[0].ApproachSpeed; math.Abs(float64(got-speed)) > 0.05*float64(speed) {
					t.Fatalf("approach speed %v, want the impact speed of about %v px/s", got, speed)
				}
				if bottom := body.Y + tc.half; math.Abs(float64(bottom-2000)) > 0.5 {
					t.Fatalf("bottom %v, want it resting on the floor at 2000", bottom)
				}
				if !body.Sleeping() || math.Abs(float64(body.Angle)) > 1e-3 {
					t.Fatalf("sleeping %v, angle %v after 10 s, want it asleep and level", body.Sleeping(), body.Angle)
				}
			})
		}
	}
}

func TestRigidFastBoxLandingAcrossTileSeamStaysLevel(t *testing.T) {
	for _, tc := range []struct {
		x, size float32
	}{{96, 20}, {100, 20}, {104, 30}, {120, 40}} {
		body := physics.NewRigidBox(tc.x, 2000-tc.size/2-800, tc.size, tc.size, 1)
		w := fastLandingWorld("tiles", body)
		var events []physics.ContactEvent
		w.OnContact = func(ev physics.ContactEvent) { events = append(events, ev) }
		maxAngle := 0.0
		for range 600 {
			w.Step(step60)
			maxAngle = max(maxAngle, math.Abs(float64(body.Angle)))
		}
		if maxAngle > 1e-3 {
			t.Fatalf("%v px box at x %v turned by up to %v rad, want it to land flat across the seam", tc.size, tc.x, maxAngle)
		}
		if len(events) != 1 || events[0].Kind != physics.ContactBegin || events[0].ApproachSpeed < 900 {
			t.Fatalf("%v px box at x %v: events %+v, want one begin at the impact speed", tc.size, tc.x, events)
		}
		if !body.Sleeping() || math.Abs(float64(body.X-tc.x)) > 0.01 {
			t.Fatalf("%v px box: sleeping %v at x %v, want it asleep where it landed at x %v", tc.size, body.Sleeping(), body.X, tc.x)
		}
	}
}

func TestRigidContactEndsWhenRestingBodyBecomesKinematicOrStatic(t *testing.T) {
	for _, floorKind := range []string{"body", "tiles"} {
		for _, kind := range []string{"kinematic", "static"} {
			t.Run(floorKind+"/"+kind, func(t *testing.T) {
				w := &physics.RigidWorld{GravityY: 600}
				if floorKind == "tiles" {
					w.Tiles = tileFloor()
				} else {
					w.AddBody(physics.NewStaticRigidBox(320, 168, 640, 16))
				}
				crate := physics.NewRigidBox(100, 140, 20, 20, 1)
				w.AddBody(crate)
				var log []physics.ContactEventKind
				w.OnContact = func(ev physics.ContactEvent) { log = append(log, ev.Kind) }
				stepFor(w, 2*time.Second)
				if len(log) != 1 || log[0] != physics.ContactBegin || !crate.Sleeping() {
					t.Fatalf("events %v, sleeping %v, want the crate resting with one begin", log, crate.Sleeping())
				}
				if kind == "kinematic" {
					crate.Kinematic = true // picked up
					crate.VY = -300
				} else {
					crate.Static = true
				}
				stepFor(w, time.Second/2)
				if len(log) != 2 || log[1] != physics.ContactEnd {
					t.Fatalf("events %v after the crate became %s, want its contact to end", log, kind)
				}
			})
		}
	}
}

func TestRigidShapeCastsSeeTilesAndSensorsOnly(t *testing.T) {
	w := &physics.RigidWorld{Tiles: tileFloor()}
	sensor := physics.NewStaticRigidBox(100, 100, 20, 20)
	sensor.Sensor = true
	crate := physics.NewRigidBox(100, 130, 20, 20, 1)
	w.AddBody(sensor, crate)
	probe := physics.CircleShape(physics.Circle{X: 200, Y: 40, R: 4})

	if hit, ok := w.ShapeCast(probe, 0, 200, 0); !ok || !hit.Tile || hit.Row != 10 || math.Abs(float64(hit.Point.Y-160)) > 0.01 {
		t.Fatalf("ShapeCast down = %+v, %v, want the floor tile at y 160", hit, ok)
	}
	hits := w.ShapeCastAll(physics.CircleShape(physics.Circle{X: 100, Y: 40, R: 4}), 0, 200, 0)
	if len(hits) < 2 || hits[0].RigidBody != crate {
		t.Fatalf("ShapeCastAll down = %+v, want the crate, then floor tiles", hits)
	}
	for _, h := range hits[1:] {
		if !h.Tile || h.Row != 10 {
			t.Fatalf("ShapeCastAll down = %+v, want floor tiles after the crate", hits)
		}
	}
	sensorsOnly := physics.ShapeCastOptions{QueryMask: physics.QuerySensors}
	if hit, ok := w.ShapeCastWith(physics.CircleShape(physics.Circle{X: 100, Y: 40, R: 4}), 0, 200, sensorsOnly); !ok || hit.RigidBody != sensor {
		t.Fatalf("ShapeCastWith(QuerySensors) = %+v, %v, want the sensor", hit, ok)
	}
	if hits := w.ShapeCastAllWith(physics.CircleShape(physics.Circle{X: 100, Y: 40, R: 4}), 0, 200, sensorsOnly); len(hits) != 1 || hits[0].RigidBody != sensor {
		t.Fatalf("ShapeCastAllWith(QuerySensors) = %+v, want only the sensor", hits)
	}
	if _, ok := w.ShapeCastWith(probe, 0, 200, physics.ShapeCastOptions{QueryMask: physics.QueryBodies}); ok {
		t.Fatal("ShapeCastWith(QueryBodies) hit something below an empty column, want the tiles left out")
	}
}

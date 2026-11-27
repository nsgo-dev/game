package physics_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// TestWorldMoveAndSlideClosedDoorAndRemoved verifies Acceptance #1:
// a player is stopped by a closed door body and walks through after the door body is removed.
func TestWorldMoveAndSlideClosedDoorAndRemoved(t *testing.T) {
	const dt = time.Second / 60

	grid := physics.NewGrid(20, 10, 16, 16)
	grid.FillRect(0, 8, 20, 2, physics.TileSolid) // floor top at Y=128

	w := &physics.World{Tiles: grid}
	player := physics.NewBoxBody(32, 112, 12, 16) // bottom at 128
	player.Tag = "player"
	door := physics.NewBoxBody(80, 96, 8, 32) // closed door blocking X=80..88, Y=96..128
	door.Tag = "door"
	w.Add(player, door)

	var hitDoor bool
	var lastDoorCol physics.SlideCollision
	for range 60 {
		player.VX = 120
		player.VY += 600 * float32(dt.Seconds())
		res := w.MoveAndSlide(player, dt)
		for _, c := range res.Collisions {
			if c.Body == door {
				hitDoor = true
				lastDoorCol = c
			}
		}
		if player.Shape.Box.Right() > 80.01 {
			t.Fatalf("player penetrated closed door: right=%v", player.Shape.Box.Right())
		}
	}
	if !hitDoor {
		t.Fatal("expected player to collide with closed door body")
	}
	if !near(player.Shape.Box.Right(), 80, 1e-3) {
		t.Fatalf("player right = %v, want 80 (flush against door)", player.Shape.Box.Right())
	}
	if !player.OnRightWall || !player.Grounded {
		t.Fatalf("expected OnRightWall && Grounded, got OnRightWall=%v Grounded=%v", player.OnRightWall, player.Grounded)
	}
	if !near(lastDoorCol.NormalX, -1, 1e-4) || !near(lastDoorCol.NormalY, 0, 1e-4) {
		t.Fatalf("door normal = (%v, %v), want (-1, 0)", lastDoorCol.NormalX, lastDoorCol.NormalY)
	}
	if !near(float32(lastDoorCol.Position.X), 80, 1e-2) {
		t.Fatalf("door contact X = %v, want 80", lastDoorCol.Position.X)
	}

	// Open the door by removing it from the world; player walks straight through.
	if !w.Remove(door) {
		t.Fatal("failed to remove door body")
	}
	for range 30 {
		player.VX = 120
		player.VY += 600 * float32(dt.Seconds())
		res := w.MoveAndSlide(player, dt)
		if res.OnRightWall {
			t.Fatalf("unexpected wall hit after removing door: %+v", res.Collisions)
		}
	}
	if player.Shape.Box.X <= 110 {
		t.Fatalf("player did not walk through opened door: X=%v, want > 110", player.Shape.Box.X)
	}
}

// TestWorldMoveAndSlideTopDown30DegreeWall verifies Acceptance #2:
// a top-down character slides along a 30-degree wall without sticking or jitter,
// across Box, Circle and Capsule movers and Polygon, Segment, Chain and static RigidBody walls.
func TestWorldMoveAndSlideTopDown30DegreeWall(t *testing.T) {
	const dt = time.Second / 60
	rad30 := math.Pi / 6
	// Wall running at 30 degrees from horizontal: from (40, 140) up-right to (240, 140 - 200*tan(30°)).
	// Wait: let's build a wall oriented at 30 degrees clockwise so moving right (+X) hits its face and slides along it.
	// Segment from (100, 200) to (100 + 200*cos(30°), 200 - 200*sin(30°)):
	// tangent unit vector is (cos30, -sin30), outward normal facing the mover approaching from left/above is (-sin30, -cos30).
	cos30 := math.Cos(rad30)
	sin30 := math.Sin(rad30)
	// Build a thick 30-degree wall polygon (240 long, 20 thick) centered at (160, 120), rotated -30 deg (-pi/6).
	wallPoly := physics.NewBoxPolygon(160, 120, 240, 20, float32(-rad30))
	wallSeg := physics.Segment{
		X1: float32(160 - 120*cos30),
		Y1: float32(120 + 120*sin30),
		X2: float32(160 + 120*cos30),
		Y2: float32(120 - 120*sin30),
	}
	wallChain := physics.ChainShape([]core.Point{
		{X: core.Points(160 - 120*cos30), Y: core.Points(120 + 120*sin30)},
		{X: 160, Y: 120},
		{X: core.Points(160 + 120*cos30), Y: core.Points(120 - 120*sin30)},
	}, false)

	type wallSpec struct {
		name  string
		setup func(w *physics.World)
	}
	walls := []wallSpec{
		{
			name: "PolygonBody",
			setup: func(w *physics.World) {
				w.Add(&physics.Body{Tag: "wall", Shape: physics.PolygonShape(wallPoly)})
			},
		},
		{
			name: "SegmentBody",
			setup: func(w *physics.World) {
				w.Add(&physics.Body{Tag: "wall", Shape: physics.SegmentShape(wallSeg)})
			},
		},
		{
			name: "ChainBody",
			setup: func(w *physics.World) {
				w.Add(&physics.Body{Tag: "wall", Shape: wallChain})
			},
		},
		{
			name: "StaticRigidBox",
			setup: func(w *physics.World) {
				rb := physics.NewStaticRigidBox(160, 120, 240, 20)
				rb.Angle = float32(-rad30)
				w.RigidWorld = &physics.RigidWorld{Bodies: []*physics.RigidBody{rb}}
			},
		},
	}

	type moverSpec struct {
		name string
		make func() *physics.Body
	}
	movers := []moverSpec{
		{
			name: "Box",
			make: func() *physics.Body {
				return physics.NewBoxBody(80, 110, 14, 14)
			},
		},
		{
			name: "Circle",
			make: func() *physics.Body {
				return physics.NewCircleBody(87, 117, 7)
			},
		},
		{
			name: "Capsule",
			make: func() *physics.Body {
				return &physics.Body{
					Shape: physics.CapsuleShape(physics.Capsule{X1: 87, Y1: 112, X2: 87, Y2: 122, R: 6}),
				}
			},
		},
	}

	// Expected tangential advance per step when pushing purely right (VX = 120, VY = 0)
	// against a 30-degree wall:
	// tangent t = (cos30, -sin30), speed along wall = 120 * cos30, so per step:
	// dx = 120 * dt * cos30^2 = 2 * 0.75 = 1.5
	// dy = -120 * dt * cos30 * sin30 = -2 * 0.4330127 = -0.8660254
	wantStepDX := 120.0 * dt.Seconds() * cos30 * cos30
	wantStepDY := -120.0 * dt.Seconds() * cos30 * sin30

	opts := physics.MoveOptions{Mode: physics.MotionFloating}
	for _, ws := range walls {
		for _, ms := range movers {
			t.Run(ws.name+"/"+ms.name, func(t *testing.T) {
				w := &physics.World{}
				ws.setup(w)
				mover := ms.make()
				w.Add(mover)

				slidingSteps := 0
				prevBounds := mover.Bounds()
				for step := range 80 {
					// Top-down player holds right key every frame.
					mover.VX = 120
					mover.VY = 0
					res := w.MoveAndSlideWith(mover, dt, opts)
					curBounds := mover.Bounds()
					stepDX := float64(curBounds.X - prevBounds.X)
					stepDY := float64(curBounds.Y - prevBounds.Y)
					prevBounds = curBounds

					if res.OnFloor || res.OnCeiling {
						t.Fatalf("step %d: MotionFloating set OnFloor=%v OnCeiling=%v", step, res.OnFloor, res.OnCeiling)
					}
					if res.OnWall && len(res.Collisions) > 0 {
						// Verify normal matches 30-degree wall normal (-sin30, -cos30) = (-0.5, -0.8660254).
						n := res.WallNormal
						if !near(float32(n.X), float32(-sin30), 2e-2) || !near(float32(n.Y), float32(-cos30), 2e-2) {
							t.Fatalf("step %d: wall normal = (%v, %v), want (~%v, ~%v)", step, n.X, n.Y, -sin30, -cos30)
						}
						// After the initial impact step, every subsequent sliding step must make
						// smooth, uniform progress along the 30° wall without sticking or jitter.
						if slidingSteps >= 2 {
							if math.Abs(stepDX-wantStepDX) > 0.05 || math.Abs(stepDY-wantStepDY) > 0.05 {
								t.Fatalf("step %d: jitter/sticking along 30° wall: step=(%.4f, %.4f), want (%.4f, %.4f)",
									step, stepDX, stepDY, wantStepDX, wantStepDY)
							}
						}
						slidingSteps++
					}
				}
				if slidingSteps < 30 {
					t.Fatalf("expected at least 30 continuous sliding steps along 30° wall, got %d", slidingSteps)
				}
			})
		}
	}
}

// TestWorldMoveAndSlideOneWayBodyDropThrough verifies Acceptance #3:
// a body standing on a one-way body stays supported, passes up from below, ignores side approach,
// and drops cleanly through it when OneWayDrop is set.
func TestWorldMoveAndSlideOneWayBodyDropThrough(t *testing.T) {
	const dt = time.Second / 60

	w := &physics.World{}
	plat := physics.NewBoxBody(40, 100, 120, 12)
	plat.Tag = "one-way-platform"
	plat.OneWay = true
	floor := physics.NewBoxBody(0, 180, 200, 20)
	floor.Tag = "floor"
	w.Add(plat, floor)

	// 1. Approach from the side: passes right through without stopping.
	sideMover := physics.NewBoxBody(10, 96, 12, 16)
	w.Add(sideMover)
	for range 30 {
		sideMover.VX = 120
		sideMover.VY = 0
		res := w.MoveAndSlide(sideMover, dt)
		if len(res.Collisions) > 0 {
			t.Fatalf("one-way body blocked horizontal approach: %+v", res.Collisions)
		}
	}
	if sideMover.Shape.Box.X < 60 {
		t.Fatalf("side mover did not pass through one-way body: X=%v", sideMover.Shape.Box.X)
	}
	w.Remove(sideMover)

	// 2. Jump up from underneath: passes through, then falls onto top once above it.
	player := physics.NewBoxBody(80, 130, 12, 16)
	player.VY = -260
	w.Add(player)

	passedThroughUpward := false
	for range 60 {
		player.VY += 600 * float32(dt.Seconds())
		w.MoveAndSlide(player, dt)
		if player.Shape.Box.Bottom() < 100 {
			passedThroughUpward = true
		}
	}
	if !passedThroughUpward {
		t.Fatalf("player failed to jump up through one-way body; Y=%v", player.Shape.Box.Y)
	}
	if !player.Grounded || !near(player.Shape.Box.Bottom(), 100, 1e-3) {
		t.Fatalf("player failed to land on one-way body: grounded=%v bottom=%v", player.Grounded, player.Shape.Box.Bottom())
	}

	// 3. Drop through by setting OneWayDrop = true for a few frames, then clearing it while mid-pass.
	player.OneWayDrop = true
	for i := range 40 {
		if i == 3 {
			// Clearing OneWayDrop while overlapping the platform must NOT snap the player back up.
			player.OneWayDrop = false
		}
		player.VY += 600 * float32(dt.Seconds())
		w.MoveAndSlide(player, dt)
	}
	if !player.Grounded || !near(player.Shape.Box.Bottom(), 180, 1e-3) {
		t.Fatalf("player failed to drop through one-way body onto floor: grounded=%v bottom=%v", player.Grounded, player.Shape.Box.Bottom())
	}
}

// TestWorldMoveAndSlideDeterminism verifies Acceptance #4:
// identical inputs across repeated runs produce bit-identical shapes, velocities, collisions, and checksums.
func TestWorldMoveAndSlideDeterminism(t *testing.T) {
	const dt = time.Second / 60

	run := func() (physics.Shape, float32, float32, uint64, int) {
		grid := physics.NewGrid(20, 15, 16, 16)
		grid.FillRect(0, 13, 20, 2, physics.TileSolid)
		grid.FillRect(4, 9, 5, 1, physics.TileOneWay)

		w := &physics.World{Tiles: grid}
		ramp, ok := physics.NewPolygon([]core.Point{{X: 140, Y: 208}, {X: 260, Y: 208}, {X: 260, Y: 140}}, 0)
		if !ok {
			t.Fatal("invalid ramp polygon")
		}
		w.Add(&physics.Body{ID: 1, Tag: "ramp", Shape: physics.PolygonShape(ramp)})
		ow := physics.NewBoxBody(100, 150, 48, 8)
		ow.ID = 2
		ow.OneWay = true
		w.Add(ow)
		rb := physics.NewStaticRigidCircle(220, 100, 18)
		w.RigidWorld = &physics.RigidWorld{Bodies: []*physics.RigidBody{rb}}

		mover := &physics.Body{
			ID:    3,
			Shape: physics.CapsuleShape(physics.Capsule{X1: 30, Y1: 170, X2: 30, Y2: 184, R: 6}),
		}
		w.Add(mover)

		colCount := 0
		for step := range 180 {
			if step < 90 {
				mover.VX = 95
			} else {
				mover.VX = -80
			}
			if step == 40 && mover.Grounded {
				mover.VY = -260
			}
			mover.OneWayDrop = step >= 120 && step < 126
			mover.VY += 620 * float32(dt.Seconds())
			res := w.MoveAndSlide(mover, dt)
			colCount += len(res.Collisions)
		}
		return mover.Shape, mover.VX, mover.VY, w.Checksum(), colCount
	}

	s1, vx1, vy1, h1, c1 := run()
	s2, vx2, vy2, h2, c2 := run()
	if s1 != s2 ||
		math.Float32bits(vx1) != math.Float32bits(vx2) ||
		math.Float32bits(vy1) != math.Float32bits(vy2) ||
		h1 != h2 || c1 != c2 {
		t.Fatalf("non-deterministic MoveAndSlide:\nrun1: shape=%+v v=(%v,%v) hash=%#x cols=%d\nrun2: shape=%+v v=(%v,%v) hash=%#x cols=%d",
			s1, vx1, vy1, h1, c1, s2, vx2, vy2, h2, c2)
	}
	if c1 == 0 {
		t.Fatal("expected collisions during determinism trajectory")
	}
}

func TestWorldMoveAndSlideUpDirectionAndFloorAngle(t *testing.T) {
	const dt = time.Second / 60

	// 1. Inverted gravity: Up = (0, 1), so a surface above (normal (0, 1)) is the Floor.
	w := &physics.World{}
	upperSlab := physics.NewBoxBody(0, 0, 200, 16)
	w.Add(upperSlab)
	mover := physics.NewBoxBody(50, 40, 14, 14)
	w.Add(mover)

	opts := physics.MoveOptions{UpX: 0, UpY: 1}
	var last physics.SlideResult
	for range 20 {
		mover.VY = -200
		last = w.MoveAndSlideWith(mover, dt, opts)
	}
	if !last.OnFloor || last.OnCeiling || !near(mover.Shape.Box.Top(), 16, 1e-3) {
		t.Fatalf("inverted up: OnFloor=%v OnCeiling=%v top=%v", last.OnFloor, last.OnCeiling, mover.Shape.Box.Top())
	}
	if !near(float32(last.FloorNormal.Y), 1, 1e-4) {
		t.Fatalf("inverted up FloorNormal=%+v, want (0, 1)", last.FloorNormal)
	}

	// 2. FloorMaxAngle: 55-degree ramp is a wall at default 45°, and a floor at 60°.
	rad55 := 55 * math.Pi / 180
	ramp55, ok := physics.NewPolygon([]core.Point{
		{X: 0, Y: 200},
		{X: 100, Y: 200},
		{X: 100, Y: core.Points(200 - 100*math.Tan(rad55))},
	}, 0)
	if !ok {
		t.Fatal("invalid 55 deg ramp")
	}
	w2 := &physics.World{}
	w2.Add(&physics.Body{Shape: physics.PolygonShape(ramp55)})

	bDefault := physics.NewBoxBody(30, 100, 12, 12)
	w2.Add(bDefault)
	var hitDefault physics.SlideResult
	for range 30 {
		bDefault.VY = 180
		res := w2.MoveAndSlide(bDefault, dt)
		if len(res.Collisions) > 0 {
			hitDefault = res
			break
		}
	}
	if !hitDefault.OnWall || hitDefault.OnFloor {
		t.Fatalf("55 deg ramp with default 45° max angle: OnFloor=%v OnWall=%v", hitDefault.OnFloor, hitDefault.OnWall)
	}
	w2.Remove(bDefault)

	bSteep := physics.NewBoxBody(30, 100, 12, 12)
	w2.Add(bSteep)
	var hitSteep physics.SlideResult
	for range 30 {
		bSteep.VY = 180
		res := w2.MoveAndSlideWith(bSteep, dt, physics.MoveOptions{FloorMaxAngle: float32(60 * math.Pi / 180)})
		if len(res.Collisions) > 0 {
			hitSteep = res
			break
		}
	}
	if !hitSteep.OnFloor || hitSteep.OnWall {
		t.Fatalf("55 deg ramp with 60° max angle: OnFloor=%v OnWall=%v", hitSteep.OnFloor, hitSteep.OnWall)
	}
}

func TestWorldMoveAndSlideLayersAndStaticRigidBodies(t *testing.T) {
	const dt = time.Second / 60

	layerPlayer := physics.LayerBit(0)
	layerDoor := physics.LayerBit(1)
	layerGhost := physics.LayerBit(2)

	w := &physics.World{}
	door := physics.NewBoxBody(60, 0, 10, 60)
	door.Layer = layerDoor
	ghostWall := physics.NewBoxBody(40, 0, 10, 60)
	ghostWall.Layer = layerGhost
	w.Add(door, ghostWall)

	// Add one dynamic and one static RigidBody to RigidWorld:
	// MoveAndSlide must collide only with the static RigidBody (when layers match).
	rw := &physics.RigidWorld{}
	dynCrate := physics.NewRigidBox(30, 20, 12, 12, 5)
	dynCrate.Layer = layerDoor
	statPillar := physics.NewStaticRigidBox(100, 20, 12, 60)
	statPillar.Layer = layerDoor
	rw.AddBody(dynCrate, statPillar)
	w.RigidWorld = rw

	player := physics.NewBoxBody(0, 10, 12, 12)
	player.Layer = layerPlayer
	player.Mask = layerDoor // ignores layerGhost
	w.Add(player)

	for range 60 {
		player.VX = 120
		w.MoveAndSlide(player, dt)
	}
	// Passed through dynCrate (at 24..36) and ghostWall (at 40..50), stopped at door (X=60).
	if !near(player.Shape.Box.Right(), 60, 1e-3) {
		t.Fatalf("player right = %v, want 60 (blocked by layerDoor, ignoring dynamic rigid and layerGhost)", player.Shape.Box.Right())
	}

	// Remove door: player continues and stops at static RigidBody pillar (left edge at 100 - 6 = 94).
	w.Remove(door)
	var hitRigid *physics.RigidBody
	for range 60 {
		player.VX = 120
		res := w.MoveAndSlide(player, dt)
		for _, c := range res.Collisions {
			if c.RigidBody != nil {
				hitRigid = c.RigidBody
			}
		}
	}
	if hitRigid != statPillar {
		t.Fatalf("expected collision with static RigidBody pillar, got %v", hitRigid)
	}
	if !near(player.Shape.Box.Right(), 94, 1e-3) {
		t.Fatalf("player right = %v, want 94 (flush against static RigidBody)", player.Shape.Box.Right())
	}
}

func TestWorldMoveAndSlideTileSeamsAndAntiTunneling(t *testing.T) {
	const dt = time.Second / 60

	// 1. Tile seam smoothness: Circle and Box glide across 10 adjacent solid floor tiles without snagging.
	grid := physics.NewGrid(12, 6, 16, 16)
	grid.FillRect(0, 4, 12, 2, physics.TileSolid) // floor top at Y=64
	w := &physics.World{Tiles: grid}

	ball := physics.NewCircleBody(16, 56, 8) // bottom at 64
	w.Add(ball)
	for step := range 80 {
		ball.VX = 120
		ball.VY = 60
		res := w.MoveAndSlide(ball, dt)
		if res.OnRightWall || res.OnLeftWall {
			t.Fatalf("step %d: circle snagged on flat tile seam at X=%v: %+v", step, ball.Shape.Circle.X, res.Collisions)
		}
	}
	if ball.Shape.Circle.X < 150 {
		t.Fatalf("circle did not glide across tile floor: X=%v", ball.Shape.Circle.X)
	}

	// 2. High-speed anti-tunneling across a zero-thickness Segment wall.
	w2 := &physics.World{}
	thinWall := &physics.Body{Shape: physics.SegmentShape(physics.Segment{X1: 100, Y1: 0, X2: 100, Y2: 100})}
	fast := physics.NewBoxBody(10, 40, 12, 12)
	fast.VX = 20000 // > 333 px per frame
	w2.Add(thinWall, fast)
	res := w2.MoveAndSlide(fast, dt)
	if !res.OnRightWall || !near(fast.Shape.Box.Right(), 100, 1e-3) {
		t.Fatalf("fast body tunneled through segment: right=%v onRightWall=%v", fast.Shape.Box.Right(), res.OnRightWall)
	}
}

func TestWorldMoveAndSlideCornerCrease(t *testing.T) {
	const dt = time.Second / 60
	// Acute corner formed by vertical right wall at X=100 and 30-degree slanted ceiling.
	w := &physics.World{}
	w.Add(physics.NewBoxBody(100, 0, 16, 200))
	w.Add(&physics.Body{Shape: physics.SegmentShape(physics.Segment{X1: 0, Y1: 80, X2: 120, Y2: 20})})

	mover := physics.NewCircleBody(70, 70, 8)
	w.Add(mover)
	opts := physics.MoveOptions{Mode: physics.MotionFloating}
	for range 60 {
		mover.VX = 150
		mover.VY = -150
		w.MoveAndSlideWith(mover, dt, opts)
	}
	settledX, settledY := mover.Shape.Circle.X, mover.Shape.Circle.Y
	for range 20 {
		mover.VX = 150
		mover.VY = -150
		w.MoveAndSlideWith(mover, dt, opts)
		if math.Abs(float64(mover.Shape.Circle.X-settledX)) > 1e-3 || math.Abs(float64(mover.Shape.Circle.Y-settledY)) > 1e-3 {
			t.Fatalf("corner jitter: moved from (%v,%v) to (%v,%v)", settledX, settledY, mover.Shape.Circle.X, mover.Shape.Circle.Y)
		}
	}
}

func TestWorldSnapshotBodyOneWayRoundTrip(t *testing.T) {
	w := &physics.World{}
	solid := physics.NewBoxBody(0, 100, 100, 16)
	solid.ID = 1
	plat := physics.NewBoxBody(20, 60, 60, 8)
	plat.ID = 2
	plat.OneWay = true
	w.Add(solid, plat)

	hOneWay := w.Checksum()
	plat.OneWay = false
	hSolid := w.Checksum()
	if hOneWay == hSolid {
		t.Fatal("expected Body.OneWay to affect World.Checksum")
	}
	plat.OneWay = true

	raw, err := json.Marshal(w.Save())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var snap physics.WorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	restored := &physics.World{}
	if err := restored.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !restored.Bodies[1].OneWay || restored.Bodies[0].OneWay {
		t.Fatalf("OneWay flags after restore = [%v, %v], want [false, true]", restored.Bodies[0].OneWay, restored.Bodies[1].OneWay)
	}
	if restored.Checksum() != hOneWay {
		t.Fatalf("restored checksum = %#x, want %#x", restored.Checksum(), hOneWay)
	}
}

func TestCharacterBodyWithWorldDoorOneWaySlopeAndPlatform(t *testing.T) {
	const dt = time.Second / 60

	grid := physics.NewGrid(20, 10, 16, 16)
	grid.FillRect(0, 8, 20, 2, physics.TileSolid) // floor top at Y=128

	w := &physics.World{Tiles: grid}
	door := physics.NewBoxBody(112, 96, 8, 32)
	door.Tag = "door"
	owBody := physics.NewBoxBody(20, 100, 48, 8)
	owBody.Tag = "oneway-ledge"
	owBody.OneWay = true
	w.Add(door, owBody)

	// Also have a walkable slope and a moving platform at the same time.
	slopes := []physics.Slope{
		{A: core.Point{X: 160, Y: 128}, B: core.Point{X: 224, Y: 96}},
	}
	plat := &physics.MovingPlatform{
		Body: physics.NewBoxBody(240, 96, 48, 8),
	}
	plat.Body.OneWay = true // verify Body.OneWay works on MovingPlatform too
	w.Add(plat.Body)

	ch := physics.NewCharacterBody(physics.NewBoxBody(32, 76, 12, 16))
	w.Add(ch.Body)

	// 1. Land on one-way body.
	for range 25 {
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.MoveWorld(w, slopes, []*physics.MovingPlatform{plat}, dt)
	}
	if !ch.Grounded || !near(ch.Body.Shape.Box.Bottom(), 100, 1e-2) {
		t.Fatalf("failed to land on one-way body: grounded=%v bottom=%v", ch.Grounded, ch.Body.Shape.Box.Bottom())
	}

	// 2. Drop through one-way body onto tile floor.
	ch.Body.OneWayDrop = true
	for i := range 25 {
		if i == 4 {
			ch.Body.OneWayDrop = false
		}
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.MoveWorld(w, slopes, []*physics.MovingPlatform{plat}, dt)
	}
	if !ch.Grounded || !near(ch.Body.Shape.Box.Bottom(), 128, 1e-2) {
		t.Fatalf("failed to drop through one-way body: grounded=%v bottom=%v", ch.Grounded, ch.Body.Shape.Box.Bottom())
	}

	// 3. Walk right into closed door body: stopped flush at X=112.
	for range 60 {
		ch.Body.VX = 120
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.MoveWorld(w, slopes, []*physics.MovingPlatform{plat}, dt)
	}
	if !near(ch.Body.Shape.Box.Right(), 112, 1e-2) || !ch.Body.OnRightWall {
		t.Fatalf("not stopped by closed door: right=%v onRightWall=%v", ch.Body.Shape.Box.Right(), ch.Body.OnRightWall)
	}

	// 4. Remove door, walk through and climb the slope up toward Y=96.
	w.Remove(door)
	for range 50 {
		ch.Body.VX = 120
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.MoveWorld(w, slopes, []*physics.MovingPlatform{plat}, dt)
	}
	if !ch.Grounded || ch.Body.Shape.Box.X < 180 || ch.Body.Shape.Box.Bottom() >= 120 {
		t.Fatalf("failed to pass removed door and climb slope: grounded=%v box=%+v", ch.Grounded, ch.Body.Shape.Box)
	}
}

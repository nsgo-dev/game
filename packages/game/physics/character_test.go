package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCharacterBodyMovingPlatformHorizontalAndVertical(t *testing.T) {
	dt := time.Second / 60

	// Platform starts at x=40..104, y=100..112.
	plat := &physics.MovingPlatform{
		Body:   physics.NewBoxBody(40, 100, 64, 12),
		OneWay: true,
	}
	// Character starts slightly above the platform and lands on it.
	ch := physics.NewCharacterBody(physics.NewBoxBody(60, 80, 12, 16))

	// Land onto platform with gravity.
	for range 20 {
		plat.DeltaX, plat.DeltaY = 0, 0
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.Move(nil, nil, []*physics.MovingPlatform{plat}, dt)
	}
	if !ch.Grounded || ch.Riding != plat || !near(ch.Body.Shape.Box.Bottom(), 100, 1e-3) {
		t.Fatalf("failed to land on platform: grounded=%v riding=%v bottom=%v", ch.Grounded, ch.Riding, ch.Body.Shape.Box.Bottom())
	}

	startRelX := ch.Body.Shape.Box.X - plat.Body.Shape.Box.X

	// 1. Horizontal platform movement (+2 px/step for 30 steps = +60 px).
	for range 30 {
		plat.Move(2, 0)
		ch.Body.VY += 600 * float32(dt.Seconds())
		ch.Move(nil, nil, []*physics.MovingPlatform{plat}, dt)
		if !ch.Grounded || ch.Riding != plat {
			t.Fatalf("lost platform during horizontal ride: grounded=%v riding=%v", ch.Grounded, ch.Riding)
		}
	}
	if !near(ch.Body.Shape.Box.X-plat.Body.Shape.Box.X, startRelX, 1e-2) {
		t.Fatalf("horizontal drift on moving platform: relX=%v, want %v", ch.Body.Shape.Box.X-plat.Body.Shape.Box.X, startRelX)
	}
	if !near(ch.Body.Shape.Box.X, 60+60, 1e-2) {
		t.Fatalf("expected character X=120 after ride, got %v", ch.Body.Shape.Box.X)
	}

	// 2. Vertical upward movement (-1.5 px/step for 20 steps = -30 px) with zero gravity too.
	ch.Body.VY = 0
	for range 20 {
		plat.Move(0, -1.5)
		ch.Move(nil, nil, []*physics.MovingPlatform{plat}, dt)
		if !ch.Grounded || ch.Riding != plat {
			t.Fatalf("lost platform during upward ride: grounded=%v riding=%v", ch.Grounded, ch.Riding)
		}
	}
	if !near(ch.Body.Shape.Box.Bottom(), plat.Body.Shape.Box.Top(), 1e-2) || !near(plat.Body.Shape.Box.Top(), 70, 1e-2) {
		t.Fatalf("upward ride mismatch: charBottom=%v platTop=%v", ch.Body.Shape.Box.Bottom(), plat.Body.Shape.Box.Top())
	}

	// 3. Vertical downward movement (+2 px/step for 25 steps = +50 px).
	for range 25 {
		plat.Move(0, 2)
		ch.Move(nil, nil, []*physics.MovingPlatform{plat}, dt)
		if !ch.Grounded || ch.Riding != plat {
			t.Fatalf("lost platform during downward ride: grounded=%v riding=%v", ch.Grounded, ch.Riding)
		}
	}
	if !near(ch.Body.Shape.Box.Bottom(), plat.Body.Shape.Box.Top(), 1e-2) || !near(plat.Body.Shape.Box.Top(), 120, 1e-2) {
		t.Fatalf("downward ride mismatch: charBottom=%v platTop=%v", ch.Body.Shape.Box.Bottom(), plat.Body.Shape.Box.Top())
	}
}

func TestCharacterBodyWalkUpAndDown30DegreeSlope(t *testing.T) {
	dt := time.Second / 60

	// 30-degree slope rising to the right from (50, 200) to (250, 200 - 200*tan(30°)).
	rad30 := math.Pi / 6
	rise := float32(200 * math.Tan(rad30))
	upSlope := physics.Slope{
		A: core.Point{X: 50, Y: 200},
		B: core.Point{X: 250, Y: core.Points(200 - rise)},
	}
	// Flat floor up to the slope base (y = 200..216) using a simple tile grid? Or flat slope + ramp.
	flatFloor := physics.Slope{
		A: core.Point{X: 0, Y: 200},
		B: core.Point{X: 50, Y: 200},
	}
	slopes := []physics.Slope{flatFloor, upSlope}

	// Start on flat floor at x=20, bottom=200.
	ch := physics.NewCharacterBody(physics.NewBoxBody(20, 184, 16, 16))
	ch.SnapDist = 8

	// Walk UP the 30° slope using game.Loop + nsgotest for determinism.
	loop := &game.Loop{
		Rate: 60,
		Update: func(step time.Duration) {
			ch.Body.VX = 120
			ch.Body.VY += 600 * float32(step.Seconds())
			ch.Move(nil, slopes, nil, step)
		},
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	app.Advance(time.Second) // moves ~120 px right -> x ~ 140, right edge ~ 156 (well up the ramp)

	if !ch.Grounded {
		t.Fatalf("expected character to stay grounded climbing 30 deg slope")
	}
	rightEdge := ch.Body.Shape.Box.Right()
	expectedY := 200 - (rightEdge-50)*float32(math.Tan(rad30))
	if !near(ch.Body.Shape.Box.Bottom(), expectedY, 0.5) {
		t.Fatalf("uphill slope bottom=%v, want ~%v (x=%v)", ch.Body.Shape.Box.Bottom(), expectedY, ch.Body.Shape.Box.X)
	}
	wantNX := -float32(math.Sin(rad30))
	wantNY := -float32(math.Cos(rad30))
	if !near(float32(ch.GroundNormal.X), wantNX, 1e-3) || !near(float32(ch.GroundNormal.Y), wantNY, 1e-3) {
		t.Fatalf("uphill ground normal = (%v, %v), want (%v, %v)", ch.GroundNormal.X, ch.GroundNormal.Y, wantNX, wantNY)
	}

	// Now walk DOWN the 30° slope (leftward, VX = -120) with VY = 0 to verify SnapDist downhill snapping!
	ch.Body.VY = 0
	for range 45 {
		ch.Body.VX = -120
		ch.Body.VY = 0
		ch.Move(nil, slopes, nil, dt)
		if !ch.Grounded {
			t.Fatalf("character lost ground while walking downhill on 30 deg slope at x=%v y=%v", ch.Body.Shape.Box.X, ch.Body.Shape.Box.Y)
		}
	}
	// Character should have descended smoothly along the ramp back toward y=200.
	if ch.Body.Shape.Box.Bottom() <= expectedY+20 {
		t.Fatalf("character did not descend downhill slope: bottom=%v (started at %v)", ch.Body.Shape.Box.Bottom(), expectedY)
	}
	rightEdgeAfter := ch.Body.Shape.Box.Right()
	if rightEdgeAfter > 50 {
		wantDownY := 200 - (rightEdgeAfter-50)*float32(math.Tan(rad30))
		if !near(ch.Body.Shape.Box.Bottom(), wantDownY, 0.5) {
			t.Fatalf("downhill snapped bottom=%v, want ~%v", ch.Body.Shape.Box.Bottom(), wantDownY)
		}
	}
}

// TestCharacterBodyClimbsOntoSolidTileAtSlopeTop walks up a 45 degree slope whose top meets the top of a solid tile: the character must step onto the tile instead of stopping at its side, which it overlaps for one step before the climb lifts it.
func TestCharacterBodyClimbsOntoSolidTileAtSlopeTop(t *testing.T) {
	dt := time.Second / 60
	grid := physics.NewGrid(8, 4, 16, 16)
	grid.FillRect(0, 3, 8, 1, physics.TileSolid)
	grid.FillRect(3, 2, 5, 1, physics.TileSolid)
	slopes := []physics.Slope{{A: core.Point{X: 32, Y: 48}, B: core.Point{X: 48, Y: 32}}}

	ch := physics.NewCharacterBody(physics.NewBoxBody(4, 32, 12, 16))
	for i := range 60 {
		ch.Body.VX = 98
		ch.Body.VY += 720 * float32(dt.Seconds())
		res := ch.Move(grid, slopes, nil, dt)
		if res.OnRightWall {
			t.Fatalf("step %d: stopped by the tile's side at x=%v, bottom %v", i, ch.Body.Shape.Box.X, ch.Body.Shape.Box.Bottom())
		}
		if !ch.Grounded {
			t.Fatalf("step %d: left the ground at x=%v, bottom %v", i, ch.Body.Shape.Box.X, ch.Body.Shape.Box.Bottom())
		}
	}
	if b := ch.Body.Shape.Box; b.X < 80 || b.Bottom() != 32 {
		t.Fatalf("ended at x=%v, bottom %v; want on the tile top at 32 past x=80", b.X, b.Bottom())
	}

	// Walking into a wall from a slope still stops at the wall, at the slope's height.
	grid.Set(7, 1, physics.TileSolid)
	for range 30 {
		ch.Body.VX = 98
		ch.Body.VY += 720 * float32(dt.Seconds())
		ch.Move(grid, slopes, nil, dt)
	}
	if b := ch.Body.Shape.Box; b.Right() != 112 || b.Bottom() != 32 || !ch.Body.OnRightWall {
		t.Fatalf("at the wall: right %v, bottom %v, on wall %v", b.Right(), b.Bottom(), ch.Body.OnRightWall)
	}

	// Walking back down, the character stays on the ground past the slope's lower end, where it snaps onto the floor tiles instead of falling for a step.
	for i := 0; ch.Body.Shape.Box.X > 8; i++ {
		if i > 80 {
			t.Fatalf("stuck walking downhill at x=%v", ch.Body.Shape.Box.X)
		}
		ch.Body.VX = -98
		ch.Body.VY += 720 * float32(dt.Seconds())
		res := ch.Move(grid, slopes, nil, dt)
		if !ch.Grounded || res.OnLeftWall {
			t.Fatalf("downhill step %d: grounded %v, on wall %v at x=%v, bottom %v", i, ch.Grounded, res.OnLeftWall, ch.Body.Shape.Box.X, ch.Body.Shape.Box.Bottom())
		}
	}
	if b := ch.Body.Shape.Box; b.Bottom() != 48 {
		t.Fatalf("ended at x=%v, bottom %v; want on the floor at 48", b.X, b.Bottom())
	}
}

func TestCharacterBodyBlockedBy60DegreeSlope(t *testing.T) {
	dt := time.Second / 60

	// 60-degree slope rising right from (60, 200) to (110, 200 - 50*tan(60°)).
	rad60 := math.Pi / 3
	steep := physics.Slope{
		A: core.Point{X: 60, Y: 200},
		B: core.Point{X: 110, Y: core.Points(200 - 50*math.Tan(rad60))},
	}
	flat := physics.Slope{
		A: core.Point{X: 0, Y: 200},
		B: core.Point{X: 60, Y: 200},
	}
	slopes := []physics.Slope{flat, steep}

	ch := physics.NewCharacterBody(physics.NewBoxBody(20, 184, 16, 16))
	// Default MaxSlopeAngle is 45°; 60° must block horizontal advance.
	hitWall := false
	for range 60 {
		ch.Body.VX = 120
		ch.Body.VY += 600 * float32(dt.Seconds())
		res := ch.Move(nil, slopes, nil, dt)
		if res.OnRightWall {
			hitWall = true
		}
	}
	if !hitWall {
		t.Fatalf("expected 60 deg slope to block character with OnRightWall")
	}
	// Right edge must remain at or left of slope base x=60 (i.e. X <= 44).
	if ch.Body.Shape.Box.Right() > 60.1 {
		t.Fatalf("character penetrated 60 deg slope: right=%v, want <= 60", ch.Body.Shape.Box.Right())
	}
	if !near(ch.Body.Shape.Box.Bottom(), 200, 0.5) {
		t.Fatalf("character climbed steep 60 deg slope: bottom=%v, want 200", ch.Body.Shape.Box.Bottom())
	}

	// Jumping off a walkable slope detaches cleanly without snapping back down.
	rad30 := math.Pi / 6
	walkSlope := physics.Slope{
		A: core.Point{X: 0, Y: 200},
		B: core.Point{X: 200, Y: core.Points(200 - 200*math.Tan(rad30))},
	}
	jumper := physics.NewCharacterBody(physics.NewBoxBody(20, 170, 16, 16))
	for range 20 {
		jumper.Body.VY += 600 * float32(dt.Seconds())
		jumper.Move(nil, []physics.Slope{walkSlope}, nil, dt)
	}
	if !jumper.Grounded {
		t.Fatalf("jumper should land on 30 deg slope")
	}
	jumper.Body.VX = 60
	jumper.Body.VY = -240
	jumper.Move(nil, []physics.Slope{walkSlope}, nil, dt)
	if jumper.Grounded {
		t.Fatalf("jumper should detach from slope when jumping upward")
	}
}

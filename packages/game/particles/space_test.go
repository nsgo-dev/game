package particles_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSimulationSpaceWorldVsLocal(t *testing.T) {
	world := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           10,
		Y:           20,
		Space:       particles.SpaceWorld,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    0,
		AngleMax:    0,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
	}
	local := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           10,
		Y:           20,
		Space:       particles.SpaceLocal,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    0,
		AngleMax:    0,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
	}

	world.Burst(1)
	local.Burst(1)

	world.Update(100 * time.Millisecond)
	local.Update(100 * time.Millisecond)

	// Move both emitters to (200, 100) and rotate local emitter by +90 degrees (pi/2).
	world.X, world.Y = 200, 100
	local.X, local.Y = 200, 100
	local.Rotation = math.Pi / 2

	// World particle stays at (10 + 10, 20) = (20, 20) despite emitter moving.
	wp := world.Particles()[0]
	if !near(wp.X, 20) || !near(wp.Y, 20) {
		t.Fatalf("world particle = (%v, %v), want (20, 20)", wp.X, wp.Y)
	}

	// Local particle stays at local (10, 0), which transforms to (200, 100 + 10) = (200, 110) with rotation pi/2.
	lp := local.Particles()[0]
	if !near(lp.X, 10) || !near(lp.Y, 0) {
		t.Fatalf("local particle coords = (%v, %v), want (10, 0)", lp.X, lp.Y)
	}
	wx, wy := local.ParticleWorldPos(lp)
	if !near(wx, 200) || !near(wy, 110) {
		t.Fatalf("ParticleWorldPos = (%v, %v), want (200, 110)", wx, wy)
	}

	// Verify local emitter draws identically to a world sprite at (200, 110) rotated pi/2.
	expected := sprite.NewSprite(sheet.Frames["spark"])
	expected.X, expected.Y = 200, 110
	expected.OriginX, expected.OriginY = 4, 4
	expected.Rotation = math.Pi / 2
	batch := &sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{&expected}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "local", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				local.Draw(d)
			}},
			core.Canvas{TestID: "ref", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				batch.Draw(d)
			}},
		}}
	})

	got := app.Find("local").Ops
	want := app.Find("ref").Ops
	if len(got) == 0 {
		t.Fatal("local emitter emitted no draw ops")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("local emitter draw ops != transformed reference sprite ops:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestAttachedEmitterAndFollow(t *testing.T) {
	var targetX, targetY float32 = 0, 50
	e := &particles.Emitter{
		Rate:         40, // 4 particles per 100ms step
		MaxParticles: 64,
		LifetimeMin:  time.Second,
		LifetimeMax:  time.Second,
	}
	e.Attach(func() (float32, float32) {
		return targetX, targetY
	})
	if e.X != 0 || e.Y != 50 {
		t.Fatalf("Attach did not sync initial position: got (%v, %v)", e.X, e.Y)
	}

	// Move target rapidly from x=0 to x=40 in one 100ms step (emits 4 particles).
	targetX = 40
	e.Update(100 * time.Millisecond)

	if e.Count() != 4 {
		t.Fatalf("Count = %d, want 4", e.Count())
	}
	// Particles should be smoothly distributed along [10, 20, 30, 40] instead of clumped at 40.
	wantXs := []float32{10, 20, 30, 40}
	for i, p := range e.Particles() {
		if !near(p.X, wantXs[i]) || !near(p.Y, 50) {
			t.Fatalf("particle %d at (%v, %v), want (%v, 50)", i, p.X, p.Y, wantXs[i])
		}
	}

	// Detach stops tracking targetX/targetY.
	e.Detach()
	targetX = 500
	e.Update(100 * time.Millisecond)
	if e.X != 40 {
		t.Fatalf("X after Detach = %v, want 40", e.X)
	}

	// Follow smoothly interpolates too.
	e.Clear()
	e.Follow(0, 10)
	e.Update(10 * time.Millisecond) // establish prevPos at (0, 10)
	e.Clear()
	e.Follow(0, 10)
	e.Follow(80, 10)
	e.Update(100 * time.Millisecond)
	if e.Count() != 4 {
		t.Fatalf("Count after Follow = %d, want 4", e.Count())
	}
	if e.Particles()[0].X >= e.Particles()[3].X {
		t.Fatalf("expected Follow particles spread along X, got first=%v last=%v", e.Particles()[0].X, e.Particles()[3].X)
	}

	// AttachTransform updates X, Y, Rotation.
	var rot float32 = 1.25
	e.AttachTransform(func() (float32, float32, float32) {
		return 15, 25, rot
	})
	if !near(e.X, 15) || !near(e.Y, 25) || !near(e.Rotation, 1.25) {
		t.Fatalf("AttachTransform initial = (%v, %v, %v), want (15, 25, 1.25)", e.X, e.Y, e.Rotation)
	}
	rot = 2.5
	e.Update(16 * time.Millisecond)
	if !near(e.Rotation, 2.5) {
		t.Fatalf("Rotation after Update = %v, want 2.5", e.Rotation)
	}
}

func TestSubEmittersBirthDeathCollision(t *testing.T) {
	childBirth := particles.Config{
		LifetimeMin: 500 * time.Millisecond,
		LifetimeMax: 500 * time.Millisecond,
		SpeedMin:    0,
		SpeedMax:    0,
		FrameNames:  []string{"spark"},
	}
	childCollision := particles.Config{
		LifetimeMin: 500 * time.Millisecond,
		LifetimeMax: 500 * time.Millisecond,
		SpeedMin:    10,
		SpeedMax:    10,
		AngleMin:    -math.Pi / 2,
		AngleMax:    -math.Pi / 2,
		FrameNames:  []string{"ember"},
	}
	childDeath := particles.Config{
		LifetimeMin: 500 * time.Millisecond,
		LifetimeMax: 500 * time.Millisecond,
		SpeedMin:    20,
		SpeedMax:    20,
		AngleMin:    0,
		AngleMax:    0,
		FrameNames:  []string{"smoke"},
	}

	floor := &particles.RectCollider{X: 0, Y: 50, W: 200, H: 20}

	e := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           50,
		Y:           30,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    math.Pi / 2, // straight down toward floor at Y=50
		AngleMax:    math.Pi / 2,
		LifetimeMin: 300 * time.Millisecond,
		LifetimeMax: 300 * time.Millisecond,
		Seed:        77,
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   0.5,
			Collider: floor,
		},
		SubEmitters: []particles.SubEmitterRule{
			{Trigger: particles.TriggerOnBirth, Config: childBirth, BurstCount: 2, InheritVelocity: 0.5},
			{Trigger: particles.TriggerOnCollision, Config: childCollision, BurstCount: 3, InheritVelocity: 0},
			{Trigger: particles.TriggerOnDeath, Config: childDeath, BurstCount: 4, InheritVelocity: 0},
		},
	}

	// Burst 1 parent particle -> immediately fires TriggerOnBirth (2 child particles inheriting 0.5*VY = 50).
	e.Burst(1)
	if e.Count() != 1 {
		t.Fatalf("parent Count = %d, want 1", e.Count())
	}
	if len(e.Children()) < 1 || e.Children()[0].Count() != 2 {
		t.Fatalf("birth sub-emitter count = %d, want 2", e.TotalCount()-e.Count())
	}
	for _, cp := range e.Children()[0].Particles() {
		if !near(cp.X, 50) || !near(cp.Y, 30) || !near(cp.VY, 50) {
			t.Fatalf("birth child particle = pos(%v,%v) vel(%v,%v), want (50,30) (0,50)", cp.X, cp.Y, cp.VX, cp.VY)
		}
	}

	// Step 250ms: parent travels from Y=30 down by 25 to Y=55 -> hits floor at Y=50 -> fires TriggerOnCollision (3 children).
	e.Update(250 * time.Millisecond)
	if len(e.Children()) < 2 || e.Children()[1] == nil || e.Children()[1].Count() != 3 {
		t.Fatalf("collision sub-emitter did not fire 3 particles (children=%v)", e.Children())
	}
	for _, cp := range e.Children()[1].Particles() {
		if !near(cp.X, 50) || !near(cp.Y, 50) {
			t.Fatalf("collision child spawned at (%v, %v), want (50, 50)", cp.X, cp.Y)
		}
	}

	// Step another 100ms (total 350ms > 300ms lifetime): parent expires -> fires TriggerOnDeath (4 children).
	e.Update(100 * time.Millisecond)
	if e.Count() != 0 {
		t.Fatalf("parent Count after expiry = %d, want 0", e.Count())
	}
	if len(e.Children()) < 3 || e.Children()[2] == nil || e.Children()[2].Count() != 4 {
		t.Fatalf("death sub-emitter did not fire 4 particles")
	}
	if e.TotalCount() != 2+3+4 {
		t.Fatalf("TotalCount = %d, want 9", e.TotalCount())
	}

	// Drawing parent draws all live child sub-emitters too.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "sub", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
			e.Draw(d)
		}}
	})
	if len(app.Find("sub").Ops) == 0 {
		t.Fatal("parent Draw with live sub-emitters emitted 0 ops")
	}

	// Clear and Reset clear children too.
	e.Reset()
	if e.TotalCount() != 0 {
		t.Fatalf("TotalCount after Reset = %d, want 0", e.TotalCount())
	}
}

func TestSystemManagesMultipleEmitters(t *testing.T) {
	e1 := &particles.Emitter{Atlas: sheet, Frames: []sprite.Frame{sheet.Frames["spark"]}, LifetimeMin: time.Second}
	e2 := &particles.Emitter{Atlas: sheet, Frames: []sprite.Frame{sheet.Frames["smoke"]}, LifetimeMin: time.Second}
	sys := particles.NewSystem(e1, e2)

	e1.Burst(3)
	e2.Burst(5)
	if sys.TotalCount() != 8 {
		t.Fatalf("System TotalCount = %d, want 8", sys.TotalCount())
	}
	sys.Update(100 * time.Millisecond)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "sys", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
			sys.Draw(d)
		}}
	})
	if len(app.Find("sys").Ops) == 0 {
		t.Fatal("System.Draw emitted 0 ops")
	}

	sys.Remove(e1)
	if sys.TotalCount() != 5 {
		t.Fatalf("System TotalCount after Remove = %d, want 5", sys.TotalCount())
	}
	sys.Clear()
	if sys.TotalCount() != 0 {
		t.Fatalf("System TotalCount after Clear = %d, want 0", sys.TotalCount())
	}
}

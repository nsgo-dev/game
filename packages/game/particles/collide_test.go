package particles_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
)

func TestTileGridColliderModes(t *testing.T) {
	// Solid floor at row >= 2 (Y >= 32) and right wall at col >= 4 (X >= 64), 16x16 tiles.
	grid := &particles.TileGridCollider{
		TileSize: 16,
		SolidAt: func(col, row int) bool {
			return row >= 2 || col >= 4
		},
	}

	t.Run("BounceWithRestitutionAndFriction", func(t *testing.T) {
		e := &particles.Emitter{
			X:           16,
			Y:           28,
			SpeedMin:    float32(math.Hypot(100, 100)),
			SpeedMax:    float32(math.Hypot(100, 100)),
			AngleMin:    math.Pi / 4, // VX=100, VY=100
			AngleMax:    math.Pi / 4,
			LifetimeMin: time.Second,
			LifetimeMax: time.Second,
			Collision: particles.CollisionConfig{
				Enabled:  true,
				Mode:     particles.CollideBounce,
				Bounce:   0.8,
				Friction: 0.25,
				Radius:   0,
				Collider: grid,
			},
		}
		e.Burst(1)
		// 100ms step moves to (26, 38) -> hits floor at Y=32 with normal (0, -1).
		e.Update(100 * time.Millisecond)
		if e.Count() != 1 {
			t.Fatalf("Count = %d, want 1", e.Count())
		}
		p := e.Particles()[0]
		if !near(p.Y, 32) {
			t.Fatalf("Y after floor hit = %v, want 32", p.Y)
		}
		// Normal velocity reversed & scaled by Bounce (0.8): VY = -80.
		// Tangential velocity damped by (1 - Friction = 0.75): VX = 75.
		if !near(p.VX, 75) || !near(p.VY, -80) {
			t.Fatalf("vel after bounce = (%v, %v), want (75, -80)", p.VX, p.VY)
		}
	})

	t.Run("BounceWithRadiusAndSeamSliding", func(t *testing.T) {
		// Slide horizontally across tile seam at X=16 while slightly pressing into floor Y=32 with radius=4.
		hit, hx, hy, nx, ny := grid.ResolveParticle(15, 28, 17, 29, 4)
		if !hit {
			t.Fatal("expected hit with floor tile")
		}
		if !near(hy, 28) || !near(nx, 0) || !near(ny, -1) {
			t.Fatalf("seam hit = pos(%v,%v) norm(%v,%v), want y=28 norm=(0,-1)", hx, hy, nx, ny)
		}

		// Inside corner between floor (Y=32) and right wall (X=64) with radius=4 -> pushed to (60, 28).
		hit, hx, hy, nx, ny = grid.ResolveParticle(58, 26, 62, 30, 4)
		if !hit {
			t.Fatal("expected corner hit")
		}
		if !near(hx, 60) || !near(hy, 28) {
			t.Fatalf("corner resolved pos = (%v, %v), want (60, 28)", hx, hy)
		}
		if !(nx < -0.5 && ny < -0.5) {
			t.Fatalf("corner normal = (%v, %v), expected diagonal (-0.707, -0.707)", nx, ny)
		}
	})

	t.Run("Stop", func(t *testing.T) {
		e := &particles.Emitter{
			X:           10,
			Y:           24,
			SpeedMin:    100,
			SpeedMax:    100,
			AngleMin:    math.Pi / 2,
			AngleMax:    math.Pi / 2,
			LifetimeMin: time.Second,
			LifetimeMax: time.Second,
			Collision: particles.CollisionConfig{
				Enabled:  true,
				Mode:     particles.CollideStop,
				Radius:   2,
				Collider: grid,
			},
		}
		e.Burst(1)
		e.Update(100 * time.Millisecond)
		if e.Count() != 1 {
			t.Fatalf("Count = %d, want 1", e.Count())
		}
		p := e.Particles()[0]
		if !near(p.Y, 30) || p.VX != 0 || p.VY != 0 {
			t.Fatalf("stopped particle = pos(%v,%v) vel(%v,%v), want y=30 vel=(0,0)", p.X, p.Y, p.VX, p.VY)
		}
	})

	t.Run("Kill", func(t *testing.T) {
		e := &particles.Emitter{
			X:           10,
			Y:           24,
			SpeedMin:    100,
			SpeedMax:    100,
			AngleMin:    math.Pi / 2,
			AngleMax:    math.Pi / 2,
			LifetimeMin: time.Second,
			LifetimeMax: time.Second,
			Collision: particles.CollisionConfig{
				Enabled:  true,
				Mode:     particles.CollideKill,
				Collider: grid,
			},
		}
		e.Burst(3)
		e.Update(100 * time.Millisecond)
		if e.Count() != 0 {
			t.Fatalf("Count after CollideKill = %d, want 0", e.Count())
		}
	})

	t.Run("NegativeOrigin", func(t *testing.T) {
		negGrid := &particles.TileGridCollider{
			TileSize: 10,
			OriginX:  -20,
			OriginY:  -20,
			SolidAt: func(col, row int) bool {
				return col == 0 && row == 0 // [-20, -10] x [-20, -10]
			},
		}
		hit, hx, hy, nx, ny := negGrid.ResolveParticle(-25, -15, -18, -15, 0)
		if !hit || !near(hx, -20) || !near(hy, -15) || !near(nx, -1) || !near(ny, 0) {
			t.Fatalf("negOrigin hit=(%v) pos=(%v,%v) norm=(%v,%v), want true (-20,-15) (-1,0)", hit, hx, hy, nx, ny)
		}
	})
}

func TestRectAndCircleAndMultiColliders(t *testing.T) {
	// Thin vertical wall at X in [50, 52], fast particle moving from X=10 to X=100 (swept anti-tunneling).
	wall := &particles.RectCollider{X: 50, Y: 0, W: 2, H: 100}
	hit, hx, hy, nx, ny := wall.ResolveParticle(10, 50, 100, 50, 2)
	if !hit || !near(hx, 48) || !near(hy, 50) || !near(nx, -1) || !near(ny, 0) {
		t.Fatalf("swept rect hit=%v pos=(%v,%v) norm=(%v,%v), want true (48,50) (-1,0)", hit, hx, hy, nx, ny)
	}

	// Circle collider at (100, 50) radius 10; particle hits top-left at radius 2.
	circle := &particles.CircleCollider{X: 100, Y: 50, Radius: 10}
	hit, hx, hy, nx, ny = circle.ResolveParticle(100, 20, 100, 42, 2)
	if !hit || !near(hx, 100) || !near(hy, 38) || !near(nx, 0) || !near(ny, -1) {
		t.Fatalf("circle hit=%v pos=(%v,%v) norm=(%v,%v), want true (100,38) (0,-1)", hit, hx, hy, nx, ny)
	}

	// Swept anti-tunneling through circle.
	hit, hx, hy, nx, ny = circle.ResolveParticle(70, 50, 130, 50, 0)
	if !hit || !near(hx, 90) || !near(hy, 50) || !near(nx, -1) || !near(ny, 0) {
		t.Fatalf("swept circle hit=%v pos=(%v,%v) norm=(%v,%v), want true (90,50) (-1,0)", hit, hx, hy, nx, ny)
	}

	// MultiCollider picks the earliest collider along the path (wall at X=50 before circle at X=100).
	multi := particles.MultiCollider{circle, wall}
	hit, hx, hy, nx, ny = multi.ResolveParticle(10, 50, 130, 50, 0)
	if !hit || !near(hx, 50) || !near(nx, -1) {
		t.Fatalf("multi collider hit=%v pos=(%v,%v) norm=(%v,%v), want wall at x=50", hit, hx, hy, nx, ny)
	}

	// Bounce off CircleCollider via Emitter.
	e := &particles.Emitter{
		X:           100,
		Y:           25,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    math.Pi / 2,
		AngleMax:    math.Pi / 2,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   1.0,
			Radius:   2,
			Collider: circle,
		},
	}
	e.Burst(1)
	e.Update(200 * time.Millisecond)
	p := e.Particles()[0]
	if !near(p.Y, 38) || !near(p.VY, -100) {
		t.Fatalf("circle bounce particle = pos(%v,%v) vel(%v,%v), want y=38 vy=-100", p.X, p.Y, p.VX, p.VY)
	}
}

func TestCollisionInLocalSpace(t *testing.T) {
	floor := &particles.RectCollider{X: 0, Y: 100, W: 200, H: 20}
	e := &particles.Emitter{
		X:           50,
		Y:           80,
		Space:       particles.SpaceLocal,
		SpeedMin:    100,
		SpeedMax:    100,
		AngleMin:    math.Pi / 2, // down (+Y)
		AngleMax:    math.Pi / 2,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   1.0,
			Collider: floor,
		},
	}
	e.Burst(1)
	// World start (50, 80), moves 30px down -> hits floor at world Y=100 (local Y=20) and bounces to VY=-100.
	e.Update(300 * time.Millisecond)
	p := e.Particles()[0]
	if !near(p.Y, 20) || !near(p.VY, -100) {
		t.Fatalf("local-space bounce = local(%v,%v) vel(%v,%v), want y=20 vy=-100", p.X, p.Y, p.VX, p.VY)
	}
}

func TestCollisionZeroAllocs(t *testing.T) {
	grid := &particles.TileGridCollider{
		TileSize: 16,
		SolidAt: func(col, row int) bool {
			return row >= 10 || col <= 0 || col >= 20
		},
	}
	multi := particles.MultiCollider{
		grid,
		&particles.RectCollider{X: 0, Y: 160, W: 320, H: 16},
		&particles.CircleCollider{X: 160, Y: 100, Radius: 20},
	}

	e := &particles.Emitter{
		X:            160,
		Y:            60,
		Shape:        particles.Circle(30),
		MaxParticles: 256,
		LifetimeMin:  10 * time.Second,
		LifetimeMax:  10 * time.Second,
		SpeedMin:     40,
		SpeedMax:     120,
		AngleMin:     0,
		AngleMax:     2 * math.Pi,
		GravityY:     200,
		Seed:         42,
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   0.8,
			Friction: 0.1,
			Radius:   2,
			Collider: multi,
		},
	}
	e.Burst(256)
	step := time.Second / 60
	for range 10 {
		e.Update(step)
	}

	allocs := testing.AllocsPerRun(100, func() {
		e.Update(step)
	})
	if allocs != 0 {
		t.Fatalf("colliding Update allocated %v times per step, want 0", allocs)
	}
}

func BenchmarkParticles5000Collide(b *testing.B) {
	const count = 5000
	grid := &particles.TileGridCollider{
		TileSize: 16,
		SolidAt: func(col, row int) bool {
			return row >= 20 || row < 0 || col < 0 || col >= 40
		},
	}

	e := &particles.Emitter{
		X:            320,
		Y:            160,
		Shape:        particles.Rect(500, 200),
		MaxParticles: count,
		LifetimeMin:  time.Hour,
		LifetimeMax:  time.Hour,
		SpeedMin:     60,
		SpeedMax:     240,
		AngleMin:     0,
		AngleMax:     2 * math.Pi,
		GravityY:     300,
		Seed:         2026,
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   0.85,
			Friction: 0.05,
			Radius:   2,
			Collider: grid,
		},
	}
	e.Burst(count)
	step := time.Second / 60
	// Warm up into steady bouncing state.
	for range 30 {
		e.Update(step)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Update(step)
	}
}

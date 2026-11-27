package particles_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sprite"
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

func TestSubEmitterLocalSpacePreservesTriggerAnchors(t *testing.T) {
	parent := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           0,
		Y:           0,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
		Seed:        11,
		SubEmitters: []particles.SubEmitterRule{
			{
				Trigger:    particles.TriggerOnBirth,
				BurstCount: 1,
				Config: particles.Config{
					Space:       particles.SpaceLocal,
					LifetimeMin: time.Second,
					LifetimeMax: time.Second,
					FrameNames:  []string{"ember"},
				},
			},
		},
	}

	// Burst at three distinct positions; earlier local-space children must stay at their own trigger points.
	triggers := [][2]float32{{0, 0}, {500, 0}, {200, 300}}
	for _, pos := range triggers {
		parent.X, parent.Y = pos[0], pos[1]
		parent.Burst(1)
	}

	if len(parent.Children()) != 1 || parent.Children()[0] == nil {
		t.Fatalf("expected 1 child emitter, got %v", parent.Children())
	}
	ch := parent.Children()[0]
	if ch.Space != particles.SpaceLocal {
		t.Fatalf("child Space = %v, want SpaceLocal", ch.Space)
	}
	if ch.Count() != len(triggers) {
		t.Fatalf("child Count = %d, want %d", ch.Count(), len(triggers))
	}

	assertChildWorldPositions := func(phase string) {
		t.Helper()
		for i, want := range triggers {
			wx, wy := ch.ParticleWorldPos(ch.Particles()[i])
			if !near(wx, want[0]) || !near(wy, want[1]) {
				t.Fatalf("%s: child[%d] world pos = (%v, %v), want (%v, %v)", phase, i, wx, wy, want[0], want[1])
			}
		}
	}
	assertChildWorldPositions("after bursts")

	// Verify Draw places each child sprite at its own trigger anchor, not at the newest trigger point.
	refSprites := make([]sprite.Sprite, len(triggers))
	refPtrs := make([]*sprite.Sprite, len(triggers))
	for i, pos := range triggers {
		refSprites[i] = sprite.NewSprite(sheet.Frames["ember"])
		refSprites[i].X, refSprites[i].Y = pos[0], pos[1]
		refSprites[i].OriginX, refSprites[i].OriginY = 4, 4
		refPtrs[i] = &refSprites[i]
	}
	refBatch := &sprite.Batch{Atlas: sheet, Sprites: refPtrs}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "child", Style: core.Style{Width: core.Pt(600), Height: core.Pt(400)}, Draw: func(d *core.Drawing, _ core.Size) {
				ch.Draw(d)
			}},
			core.Canvas{TestID: "ref", Style: core.Style{Width: core.Pt(600), Height: core.Pt(400)}, Draw: func(d *core.Drawing, _ core.Size) {
				refBatch.Draw(d)
			}},
		}}
	})
	gotOps := app.Find("child").Ops
	wantOps := app.Find("ref").Ops
	if len(gotOps) == 0 {
		t.Fatal("child Draw emitted 0 ops")
	}
	if !reflect.DeepEqual(gotOps, wantOps) {
		t.Fatalf("local-space child draw ops != anchored reference sprites:\ngot:  %+v\nwant: %+v", gotOps, wantOps)
	}

	// Hot-reloading config resets child.X/Y to config defaults (0, 0) and must not move live anchored children.
	parent.ReloadConfig(parent.Config())
	assertChildWorldPositions("after ReloadConfig")
}

func TestSubEmitterSequentialTriggersWorldAndLocal(t *testing.T) {
	makeParent := func(childSpace particles.SimulationSpace, childRot float32) *particles.Emitter {
		return &particles.Emitter{
			Atlas:       sheet,
			Frames:      []sprite.Frame{sheet.Frames["spark"]},
			Seed:        23,
			LifetimeMin: 125 * time.Millisecond,
			LifetimeMax: 125 * time.Millisecond,
			SubEmitters: []particles.SubEmitterRule{
				{
					Trigger:    particles.TriggerOnDeath,
					BurstCount: 1,
					Config: particles.Config{
						Space:       childSpace,
						Rotation:    childRot,
						SpeedMin:    100,
						SpeedMax:    100,
						AngleMin:    0,
						AngleMax:    0,
						LifetimeMin: time.Second,
						LifetimeMax: time.Second,
						FrameNames:  []string{"smoke"},
					},
				},
			},
		}
	}

	for _, tc := range []struct {
		name     string
		childRot float32
		wantDX1  float32 // child 0 displacement after 250ms
		wantDY1  float32
		wantDX2  float32 // child 1 displacement after 125ms
		wantDY2  float32
	}{
		{name: "unrotated", childRot: 0, wantDX1: 25, wantDY1: 0, wantDX2: 12.5, wantDY2: 0},
		{name: "rotated_90deg", childRot: math.Pi / 2, wantDX1: 0, wantDY1: 25, wantDX2: 0, wantDY2: 12.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worldParent := makeParent(particles.SpaceWorld, tc.childRot)
			localParent := makeParent(particles.SpaceLocal, tc.childRot)

			// Trigger 1: parent particle at (10, 20) dies at t = 125ms.
			worldParent.X, worldParent.Y = 10, 20
			localParent.X, localParent.Y = 10, 20
			worldParent.Burst(1)
			localParent.Burst(1)

			// Step 125ms: parent 0 expires -> spawns child 0 at (10, 20).
			worldParent.Update(125 * time.Millisecond)
			localParent.Update(125 * time.Millisecond)

			// Spawn parent 1 at (400, 150).
			worldParent.X, worldParent.Y = 400, 150
			localParent.X, localParent.Y = 400, 150
			worldParent.Burst(1)
			localParent.Burst(1)

			// Advance 125ms: child 0 moves 125ms; parent 1 dies and spawns child 1 at (400, 150).
			worldParent.Update(125 * time.Millisecond)
			localParent.Update(125 * time.Millisecond)

			// Advance 125ms: child 0 has 250ms total motion from (10, 20); child 1 has 125ms total motion from (400, 150).
			worldParent.Update(125 * time.Millisecond)
			localParent.Update(125 * time.Millisecond)

			wch := worldParent.Children()[0]
			lch := localParent.Children()[0]
			if wch.Count() != 2 || lch.Count() != 2 {
				t.Fatalf("counts world=%d local=%d, want 2 each", wch.Count(), lch.Count())
			}

			wantPos := [2][2]float32{
				{10 + tc.wantDX1, 20 + tc.wantDY1},
				{400 + tc.wantDX2, 150 + tc.wantDY2},
			}
			for i, want := range wantPos {
				wx, wy := wch.ParticleWorldPos(wch.Particles()[i])
				lx, ly := lch.ParticleWorldPos(lch.Particles()[i])
				if !near(wx, want[0]) || !near(wy, want[1]) {
					t.Fatalf("world child[%d] = (%v, %v), want (%v, %v)", i, wx, wy, want[0], want[1])
				}
				if !near(lx, want[0]) || !near(ly, want[1]) {
					t.Fatalf("local child[%d] = (%v, %v), want (%v, %v)", i, lx, ly, want[0], want[1])
				}
			}

			if tc.childRot == 0 {
				// Without rotation, SpaceWorld and SpaceLocal child draw ops must be identical.
				app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
					return core.View{Children: []core.Node{
						core.Canvas{TestID: "world", Style: core.Style{Width: core.Pt(600), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
							wch.Draw(d)
						}},
						core.Canvas{TestID: "local", Style: core.Style{Width: core.Pt(600), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
							lch.Draw(d)
						}},
					}}
				})
				if !reflect.DeepEqual(app.Find("local").Ops, app.Find("world").Ops) {
					t.Fatalf("unrotated local and world child draw ops differ:\nlocal: %+v\nworld: %+v", app.Find("local").Ops, app.Find("world").Ops)
				}
			}
		})
	}
}

func TestSubEmitterInheritedVelocityConsistentAcrossSpaces(t *testing.T) {
	for _, parentSpace := range []particles.SimulationSpace{particles.SpaceWorld, particles.SpaceLocal} {
		for _, childRot := range []float32{0, math.Pi / 2, -math.Pi / 4} {
			makeParent := func(childSpace particles.SimulationSpace) *particles.Emitter {
				return &particles.Emitter{
					Atlas:       sheet,
					Frames:      []sprite.Frame{sheet.Frames["spark"]},
					X:           30,
					Y:           40,
					Rotation:    math.Pi / 3,
					Space:       parentSpace,
					SpeedMin:    200,
					SpeedMax:    200,
					AngleMin:    0.2,
					AngleMax:    0.2,
					LifetimeMin: time.Second,
					LifetimeMax: time.Second,
					Seed:        99,
					SubEmitters: []particles.SubEmitterRule{
						{
							Trigger:         particles.TriggerOnBirth,
							BurstCount:      1,
							InheritVelocity: 0.5,
							Config: particles.Config{
								Space:       childSpace,
								Rotation:    childRot,
								SpeedMin:    40,
								SpeedMax:    40,
								AngleMin:    0,
								AngleMax:    0,
								LifetimeMin: time.Second,
								LifetimeMax: time.Second,
								FrameNames:  []string{"ember"},
							},
						},
					},
				}
			}

			wParent := makeParent(particles.SpaceWorld)
			lParent := makeParent(particles.SpaceLocal)

			// First burst at (30, 40), second burst at (300, 200).
			wParent.Burst(1)
			lParent.Burst(1)
			wParent.X, wParent.Y = 300, 200
			lParent.X, lParent.Y = 300, 200
			wParent.Burst(1)
			lParent.Burst(1)

			wParent.Update(250 * time.Millisecond)
			lParent.Update(250 * time.Millisecond)

			wch := wParent.Children()[0]
			lch := lParent.Children()[0]
			if wch.Count() != 2 || lch.Count() != 2 {
				t.Fatalf("parentSpace=%v childRot=%v: counts world=%d local=%d, want 2", parentSpace, childRot, wch.Count(), lch.Count())
			}
			for i := range 2 {
				wx, wy := wch.ParticleWorldPos(wch.Particles()[i])
				lx, ly := lch.ParticleWorldPos(lch.Particles()[i])
				if math.Abs(float64(wx-lx)) > 1e-2 || math.Abs(float64(wy-ly)) > 1e-2 {
					t.Fatalf("parentSpace=%v childRot=%v child[%d]: local worldPos=(%v,%v) != world worldPos=(%v,%v)",
						parentSpace, childRot, i, lx, ly, wx, wy)
				}
			}
		}
	}
}

func TestSubEmitterLocalSpaceTrailsCollisionAndGrandchildren(t *testing.T) {
	t.Run("trails_keep_trigger_anchor_live_and_fading", func(t *testing.T) {
		trailCfg := &particles.TrailConfig{
			MaxPoints: 8,
			Lifetime:  1.0,
			Width:     4,
			Color:     core.RGBA(0xffaa00, 1),
		}
		makeParent := func(childSpace particles.SimulationSpace) *particles.Emitter {
			return &particles.Emitter{
				Atlas:       sheet,
				Frames:      []sprite.Frame{sheet.Frames["spark"]},
				LifetimeMin: time.Second,
				LifetimeMax: time.Second,
				SubEmitters: []particles.SubEmitterRule{
					{
						Trigger:    particles.TriggerOnBirth,
						BurstCount: 1,
						Config: particles.Config{
							Space:       childSpace,
							SpeedMin:    100,
							SpeedMax:    100,
							AngleMin:    0,
							AngleMax:    0,
							LifetimeMin: 200 * time.Millisecond,
							LifetimeMax: 200 * time.Millisecond,
							Trail:       trailCfg,
							FrameNames:  []string{"ember"},
						},
					},
				},
			}
		}

		worldParent := makeParent(particles.SpaceWorld)
		localParent := makeParent(particles.SpaceLocal)

		// Burst 1 at (10, 20), advance 100ms so child 0 has a visible trail segment.
		worldParent.X, worldParent.Y = 10, 20
		localParent.X, localParent.Y = 10, 20
		worldParent.Burst(1)
		localParent.Burst(1)
		worldParent.Update(100 * time.Millisecond)
		localParent.Update(100 * time.Millisecond)

		// Burst 2 at (500, 300), advance 150ms so child 0 expires (its trail becomes fading) while child 1 is live.
		worldParent.X, worldParent.Y = 500, 300
		localParent.X, localParent.Y = 500, 300
		worldParent.Burst(1)
		localParent.Burst(1)
		worldParent.Update(150 * time.Millisecond)
		localParent.Update(150 * time.Millisecond)

		lch := localParent.Children()[0]
		if lch.Count() != 1 || lch.TrailCount() != 2 {
			t.Fatalf("local child Count=%d TrailCount=%d, want 1 live particle and 2 trails (1 fading + 1 live)", lch.Count(), lch.TrailCount())
		}

		// Compare drawn trail coordinates: every point of the fading trail must stay near x=10..20, not x=500.
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.View{Children: []core.Node{
				core.Canvas{TestID: "local", Style: core.Style{Width: core.Pt(600), Height: core.Pt(400)}, Draw: func(d *core.Drawing, _ core.Size) {
					lch.Draw(d)
				}},
			}}
		})
		ops := app.Find("local").Ops
		var sawFirstAnchor, sawSecondAnchor bool
		for _, op := range ops {
			if op.Kind == core.DrawTranslate {
				if near(op.X, 10) && near(op.Y, 20) {
					sawFirstAnchor = true
				}
				if near(op.X, 500) && near(op.Y, 300) {
					sawSecondAnchor = true
				}
			}
		}
		if !sawFirstAnchor || !sawSecondAnchor {
			t.Fatalf("expected trail DrawTranslate ops at both (10,20) and (500,300), sawFirst=%v sawSecond=%v ops=%+v", sawFirstAnchor, sawSecondAnchor, ops)
		}
	})

	t.Run("collision_and_grandchildren_use_particle_anchor", func(t *testing.T) {
		wall := &particles.RectCollider{X: 15, Y: 0, W: 10, H: 100}
		parent := &particles.Emitter{
			Atlas:       sheet,
			Frames:      []sprite.Frame{sheet.Frames["spark"]},
			LifetimeMin: time.Second,
			LifetimeMax: time.Second,
			SubEmitters: []particles.SubEmitterRule{
				{
					Trigger:    particles.TriggerOnBirth,
					BurstCount: 1,
					Config: particles.Config{
						Space:       particles.SpaceLocal,
						SpeedMin:    100,
						SpeedMax:    100,
						AngleMin:    0,
						AngleMax:    0,
						LifetimeMin: 250 * time.Millisecond,
						LifetimeMax: 250 * time.Millisecond,
						Collision: particles.CollisionConfig{
							Enabled:  true,
							Mode:     particles.CollideStop,
							Collider: wall,
						},
						SubEmitters: []particles.SubEmitterRule{
							{
								Trigger:    particles.TriggerOnDeath,
								BurstCount: 1,
								Config: particles.Config{
									Space:       particles.SpaceLocal,
									LifetimeMin: time.Second,
									LifetimeMax: time.Second,
								},
							},
						},
					},
				},
			},
		}

		// Burst 0 at (0, 50) heading right toward wall at x=15.
		parent.X, parent.Y = 0, 50
		parent.Burst(1)
		// Burst 1 far away at (500, 50) before stepping.
		parent.X, parent.Y = 500, 50
		parent.Burst(1)

		// Step 200ms: child 0 must collide with wall at x=15 and stop; child 1 moves unimpeded to x=520.
		parent.Update(200 * time.Millisecond)
		ch := parent.Children()[0]
		wx0, wy0 := ch.ParticleWorldPos(ch.Particles()[0])
		wx1, wy1 := ch.ParticleWorldPos(ch.Particles()[1])
		if !near(wx0, 15) || !near(wy0, 50) {
			t.Fatalf("child[0] after collision = (%v, %v), want (15, 50)", wx0, wy0)
		}
		if !near(wx1, 520) || !near(wy1, 50) {
			t.Fatalf("child[1] world pos = (%v, %v), want (520, 50)", wx1, wy1)
		}

		// Step 100ms more: both children die and spawn grandchildren at their respective world positions (15, 50) and (520, 50).
		parent.Update(100 * time.Millisecond)
		if len(ch.Children()) != 1 || ch.Children()[0] == nil || ch.Children()[0].Count() != 2 {
			t.Fatalf("expected 2 grandchildren, got %v", ch.Children())
		}
		gch := ch.Children()[0]
		gx0, gy0 := gch.ParticleWorldPos(gch.Particles()[0])
		gx1, gy1 := gch.ParticleWorldPos(gch.Particles()[1])
		if !near(gx0, 15) || !near(gy0, 50) {
			t.Fatalf("grandchild[0] = (%v, %v), want (15, 50)", gx0, gy0)
		}
		if !near(gx1, 520) || !near(gy1, 50) {
			t.Fatalf("grandchild[1] = (%v, %v), want (520, 50)", gx1, gy1)
		}
	})
}

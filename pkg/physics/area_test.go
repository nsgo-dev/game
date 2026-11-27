package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
)

func TestRigidBodyGravityScale(t *testing.T) {
	dt := time.Second / 60
	w := &physics.RigidWorld{GravityY: 600}

	normal := physics.NewRigidBox(0, 0, 10, 10, 1)
	circle := physics.NewRigidCircle(20, 0, 5, 1)
	weightless := physics.NewRigidBox(40, 0, 10, 10, 1)
	weightless.GravityScale = 0
	doubleG := physics.NewRigidBox(60, 0, 10, 10, 1)
	doubleG.GravityScale = 2
	inverted := physics.NewRigidBox(80, 0, 10, 10, 1)
	inverted.GravityScale = -0.5

	// Zero-initialized literal defaults to effective scale 1, and SetGravityScale(0) works on it.
	literalDefault := &physics.RigidBody{
		Shape: physics.BoxShape(physics.Box{X: 95, Y: -5, W: 10, H: 10}),
		X:     100,
		Y:     0,
		Mass:  1,
	}
	literalZero := &physics.RigidBody{
		Shape: physics.BoxShape(physics.Box{X: 115, Y: -5, W: 10, H: 10}),
		X:     120,
		Y:     0,
		Mass:  1,
	}
	literalZero.SetGravityScale(0)

	w.AddBody(normal, circle, weightless, doubleG, inverted, literalDefault, literalZero)
	for range 30 {
		w.Step(dt)
	}

	if !near(normal.Y, circle.Y, 1e-3) {
		t.Fatalf("box and circle default gravity mismatch: box.Y=%v circle.Y=%v", normal.Y, circle.Y)
	}
	if !near(literalDefault.Y, normal.Y, 1e-3) {
		t.Fatalf("zero-literal default gravity mismatch: literal.Y=%v normal.Y=%v", literalDefault.Y, normal.Y)
	}
	if weightless.Y != 0 || weightless.VY != 0 {
		t.Fatalf("expected weightless body unchanged, got Y=%v VY=%v", weightless.Y, weightless.VY)
	}
	if literalZero.Y != 0 || literalZero.VY != 0 {
		t.Fatalf("expected literalZero body unchanged, got Y=%v VY=%v", literalZero.Y, literalZero.VY)
	}
	if !near(doubleG.Y, normal.Y*2, 1e-2) {
		t.Fatalf("expected doubleG.Y ~= 2*normal.Y (%v), got %v", normal.Y*2, doubleG.Y)
	}
	if !near(inverted.Y, -normal.Y*0.5, 1e-2) {
		t.Fatalf("expected inverted.Y ~= -0.5*normal.Y (%v), got %v", -normal.Y*0.5, inverted.Y)
	}
}

func TestAreaGravityModesAndPriority(t *testing.T) {
	w := &physics.RigidWorld{GravityX: 0, GravityY: 100}
	box := physics.BoxShape(physics.Box{X: 0, Y: 0, W: 100, H: 100})

	// Base gravity when no triggers match.
	gx, gy := w.GravityAt(50, 50)
	if !near(gx, 0, 1e-5) || !near(gy, 100, 1e-5) {
		t.Fatalf("base gravity = (%v, %v), want (0, 100)", gx, gy)
	}

	// Add mode adds on top of base.
	addLow := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    1,
			GravityMode: physics.AreaOverrideAdd,
			GravityX:    20,
			GravityY:    30,
		},
	}
	w.AddTrigger(addLow)
	gx, gy = w.GravityAt(50, 50)
	if !near(gx, 20, 1e-5) || !near(gy, 130, 1e-5) {
		t.Fatalf("Add gravity = (%v, %v), want (20, 130)", gx, gy)
	}

	// Combine at higher priority replaces lower priority + base, and sums equal-priority Combine/Add.
	combA := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    5,
			GravityMode: physics.AreaOverrideCombine,
			GravityX:    10,
			GravityY:    40,
		},
	}
	combB := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    5,
			GravityMode: physics.AreaOverrideCombine,
			GravityX:    -4,
			GravityY:    15,
		},
	}
	w.AddTrigger(combA, combB)
	gx, gy = w.GravityAt(50, 50)
	if !near(gx, 6, 1e-5) || !near(gy, 55, 1e-5) {
		t.Fatalf("Combine gravity = (%v, %v), want (6, 55)", gx, gy)
	}

	// Higher-priority Add still adds on top of lower-priority Combine/Replace.
	addHigh := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    10,
			GravityMode: physics.AreaOverrideAdd,
			GravityX:    5,
			GravityY:    -5,
		},
	}
	w.AddTrigger(addHigh)
	gx, gy = w.GravityAt(50, 50)
	if !near(gx, 11, 1e-5) || !near(gy, 50, 1e-5) {
		t.Fatalf("High Add + Combine gravity = (%v, %v), want (11, 50)", gx, gy)
	}

	// Replace at priority 8 supersedes priority 5, while priority 10 Add still adds on top.
	repFirst := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    8,
			GravityMode: physics.AreaOverrideReplace,
			GravityX:    -50,
			GravityY:    0,
		},
	}
	repTie := &physics.Trigger{
		Shape: box,
		Area: physics.AreaSettings{
			Priority:    8,
			GravityMode: physics.AreaOverrideReplace,
			GravityX:    999,
			GravityY:    999,
		},
	}
	w.AddTrigger(repFirst, repTie)
	gx, gy = w.GravityAt(50, 50)
	if !near(gx, -45, 1e-5) || !near(gy, -5, 1e-5) {
		t.Fatalf("High Add + Replace gravity = (%v, %v), want (-45, -5)", gx, gy)
	}

	// Layer/mask filtering excludes triggers that don't interact.
	w2 := &physics.RigidWorld{GravityY: 100}
	w2.AddTrigger(&physics.Trigger{
		Shape: box,
		Layer: physics.LayerBit(1),
		Mask:  physics.LayerBit(2),
		Area: physics.AreaSettings{
			GravityMode: physics.AreaOverrideReplace,
			GravityY:    -200,
		},
	})
	effMatch := w2.AreaEffectAtShape(physics.BoxShape(physics.Box{X: 10, Y: 10, W: 10, H: 10}), physics.LayerBit(2), physics.LayerBit(1))
	effMiss := w2.AreaEffectAtShape(physics.BoxShape(physics.Box{X: 10, Y: 10, W: 10, H: 10}), physics.LayerBit(3), physics.LayerBit(1))
	if !near(effMatch.GravityY, -200, 1e-5) {
		t.Fatalf("matching layer gravity = %v, want -200", effMatch.GravityY)
	}
	if !near(effMiss.GravityY, 100, 1e-5) {
		t.Fatalf("non-matching layer gravity = %v, want 100", effMiss.GravityY)
	}
}

func TestAreaPointGravityFallAndOrbit(t *testing.T) {
	dt := time.Second / 120

	// 1. Verify falloff formulas via GravityAt.
	const cx, cy float32 = 0, 0
	for _, tc := range []struct {
		name    string
		falloff physics.GravityFalloff
		unit    float32
		dist    float32
		wantMag float32
	}{
		{"None", physics.GravityFalloffNone, 10, 40, 100},
		{"LinearHalf", physics.GravityFalloffLinear, 80, 40, 50},
		{"LinearBeyondCutoff", physics.GravityFalloffLinear, 30, 40, 0},
		{"Inverse", physics.GravityFalloffInverse, 20, 40, 50},
		{"InverseSquare", physics.GravityFalloffInverseSquare, 20, 40, 25},
	} {
		w := &physics.RigidWorld{}
		w.AddTrigger(&physics.Trigger{
			Shape: physics.CircleShape(physics.Circle{X: cx, Y: cy, R: 200}),
			Area: physics.AreaSettings{
				GravityMode:         physics.AreaOverrideReplace,
				PointGravity:        true,
				GravityPointX:       cx,
				GravityPointY:       cy,
				GravityStrength:     100,
				GravityFalloff:      tc.falloff,
				GravityUnitDistance: tc.unit,
			},
		})
		gx, gy := w.GravityAt(tc.dist, 0)
		if !near(gx, -tc.wantMag, 1e-3) || !near(gy, 0, 1e-3) {
			t.Fatalf("falloff %s at (%v,0): got (%v,%v), want (%v,0)", tc.name, tc.dist, gx, gy, -tc.wantMag)
		}
	}

	// 2. Body falls directly toward point center from rest.
	fallWorld := &physics.RigidWorld{GravityY: 500}
	fallWorld.AddTrigger(&physics.Trigger{
		Shape: physics.CircleShape(physics.Circle{X: 0, Y: 0, R: 300}),
		Area: physics.AreaSettings{
			GravityMode:     physics.AreaOverrideReplace,
			PointGravity:    true,
			GravityPointX:   0,
			GravityPointY:   0,
			GravityStrength: 400,
		},
	})
	dropper := physics.NewRigidCircle(120, 90, 6, 1) // dist = 150
	fallWorld.AddBody(dropper)
	for range 60 {
		fallWorld.Step(dt)
	}
	distAfter := float32(math.Hypot(float64(dropper.X), float64(dropper.Y)))
	if distAfter >= 110 {
		t.Fatalf("expected body to fall toward center, dist=%v (started at 150)", distAfter)
	}
	// Motion should stay radial along the (120, 90) ray.
	cross := dropper.X*90 - dropper.Y*120
	if math.Abs(float64(cross)) > 1e-1 {
		t.Fatalf("expected radial fall toward center, cross product=%v", cross)
	}

	// 3. Body orbits a point-gravity planet under inverse-square gravity.
	const (
		r0       = 100.0
		unitDist = 100.0
		gStr     = 400.0
	)
	// At r = r0 = unitDist, a = gStr, so circular orbit speed v = sqrt(a * r0) = 200.
	orbitWorld := &physics.RigidWorld{}
	orbitWorld.AddTrigger(&physics.Trigger{
		Shape: physics.CircleShape(physics.Circle{X: 0, Y: 0, R: 400}),
		Area: physics.AreaSettings{
			GravityMode:         physics.AreaOverrideReplace,
			PointGravity:        true,
			GravityPointX:       0,
			GravityPointY:       0,
			GravityStrength:     gStr,
			GravityFalloff:      physics.GravityFalloffInverseSquare,
			GravityUnitDistance: unitDist,
		},
	})
	sat := physics.NewRigidCircle(r0, 0, 4, 1)
	sat.VY = float32(math.Sqrt(gStr * r0))
	orbitWorld.AddBody(sat)

	var seenQ [4]bool
	minR, maxR := math.MaxFloat64, 0.0
	// Period T = 2*pi*r0 / v = pi seconds (~377 steps at 120 Hz); run 2 full orbits.
	for range 800 {
		orbitWorld.Step(dt)
		r := math.Hypot(float64(sat.X), float64(sat.Y))
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		switch {
		case sat.X >= 0 && sat.Y >= 0:
			seenQ[0] = true
		case sat.X < 0 && sat.Y >= 0:
			seenQ[1] = true
		case sat.X < 0 && sat.Y < 0:
			seenQ[2] = true
		case sat.X >= 0 && sat.Y < 0:
			seenQ[3] = true
		}
	}
	for q, ok := range seenQ {
		if !ok {
			t.Fatalf("orbiting body never visited quadrant %d (minR=%v maxR=%v)", q, minR, maxR)
		}
	}
	if minR < 90 || maxR > 110 {
		t.Fatalf("orbit drifted out of stable band [90, 110]: minR=%v maxR=%v", minR, maxR)
	}
}

func TestAreaDampingOverrides(t *testing.T) {
	dt := time.Second / 60

	w := &physics.RigidWorld{}
	// Thick molasses zone replaces damping with high linear & angular damping.
	w.AddTrigger(&physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: 0, Y: 0, W: 200, H: 200}),
		Area: physics.AreaSettings{
			LinearDampingMode:  physics.AreaOverrideReplace,
			LinearDamping:      8,
			AngularDampingMode: physics.AreaOverrideReplace,
			AngularDamping:     10,
		},
	})
	// Vacuum zone replaces damping with 0 so a damped body keeps full velocity inside it.
	w.AddTrigger(&physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: 300, Y: 0, W: 200, H: 200}),
		Area: physics.AreaSettings{
			LinearDampingMode:  physics.AreaOverrideReplace,
			LinearDamping:      0,
			AngularDampingMode: physics.AreaOverrideReplace,
			AngularDamping:     0,
		},
	})

	inMud := physics.NewRigidBox(50, 100, 10, 10, 1)
	inMud.VX = 100
	inMud.AngularVel = 6
	inMud.LinearDamping = 0.1
	inMud.AngularDamping = 0.1

	inVacuum := physics.NewRigidBox(350, 100, 10, 10, 1)
	inVacuum.VX = 100
	inVacuum.AngularVel = 6
	inVacuum.LinearDamping = 5
	inVacuum.AngularDamping = 5

	free := physics.NewRigidBox(600, 100, 10, 10, 1)
	free.VX = 100
	free.AngularVel = 6
	free.LinearDamping = 0.1
	free.AngularDamping = 0.1

	w.AddBody(inMud, inVacuum, free)
	for range 30 {
		w.Step(dt)
	}

	if inMud.VX >= free.VX*0.2 || inMud.AngularVel >= free.AngularVel*0.2 {
		t.Fatalf("expected high damping area to strongly slow body: mud=(vx=%v av=%v) free=(vx=%v av=%v)", inMud.VX, inMud.AngularVel, free.VX, free.AngularVel)
	}
	if !near(inVacuum.VX, 100, 1e-3) || !near(inVacuum.AngularVel, 6, 1e-3) {
		t.Fatalf("expected zero-damping Replace area to preserve velocity, got vx=%v av=%v", inVacuum.VX, inVacuum.AngularVel)
	}
}

func TestAreaBuoyancyFloatingCrateAndSinking(t *testing.T) {
	dt := time.Second / 60
	const (
		waterTop    = 100.0
		waterBottom = 180.0
		crateSize   = 20.0
		crateArea   = crateSize * crateSize // 400
		waterDens   = 1.0
	)

	w := &physics.RigidWorld{GravityY: 400}
	// Static pool floor at Y = waterBottom.
	floor := physics.NewStaticRigidBox(200, waterBottom+20, 600, 40)
	w.AddBody(floor)

	water := &physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: -100, Y: waterTop, W: 600, H: waterBottom - waterTop}),
		Area: physics.AreaSettings{
			FluidDensity:     waterDens,
			FluidLinearDrag:  6.0,
			FluidAngularDrag: 4.0,
		},
	}
	w.AddTrigger(water)

	// Crates with densities 0.25, 0.50, 0.75 (should float) and 1.50 (should sink).
	light := physics.NewRigidBox(0, waterTop-10, crateSize, crateSize, 0.25*crateArea)
	half := physics.NewRigidBox(60, waterTop-10, crateSize, crateSize, 0.50*crateArea)
	heavy := physics.NewRigidBox(120, waterTop-10, crateSize, crateSize, 0.75*crateArea)
	sinker := physics.NewRigidBox(180, waterTop-10, crateSize, crateSize, 1.50*crateArea)
	half.AngularVel = 5

	w.AddBody(light, half, heavy, sinker)
	for range 360 {
		w.Step(dt)
	}

	// Submerged fraction f = density / waterDens -> center Y = waterTop + (f - 0.5)*crateSize.
	checkFloat := func(name string, b *physics.RigidBody, wantFrac float32) {
		t.Helper()
		wantY := float32(waterTop) + (wantFrac-0.5)*float32(crateSize)
		if !near(b.Y, wantY, 0.5) {
			t.Fatalf("%s crate Y=%v, want ~%v (submerged frac %v)", name, b.Y, wantY, wantFrac)
		}
		if math.Abs(float64(b.VY)) > 1.0 {
			t.Fatalf("%s crate not settled: VY=%v", name, b.VY)
		}
	}
	checkFloat("light(0.25)", light, 0.25)
	checkFloat("half(0.50)", half, 0.50)
	checkFloat("heavy(0.75)", heavy, 0.75)

	// Spinning floating crate's angular velocity is damped by FluidAngularDrag.
	if math.Abs(float64(half.AngularVel)) > 0.05 {
		t.Fatalf("expected fluid angular drag to damp spin, got AngularVel=%v", half.AngularVel)
	}

	// Dense crate sinks to rest on the pool floor (center Y ~ waterBottom - crateSize/2).
	wantFloorY := float32(waterBottom - crateSize*0.5)
	if !near(sinker.Y, wantFloorY, 1.0) {
		t.Fatalf("expected dense crate to sink to floor Y~%v, got Y=%v", wantFloorY, sinker.Y)
	}

	// Verify fluid flow current carries a floating crate horizontally.
	water.Area.FluidVelX = 80
	startX := half.X
	for range 120 {
		w.Step(dt)
	}
	if half.X <= startX+60 {
		t.Fatalf("expected fluid current to carry crate right: startX=%v endX=%v", startX, half.X)
	}
}

func TestCharacterBodyRespectsAreaGravity(t *testing.T) {
	dt := time.Second / 60

	grid := physics.NewGrid(20, 20, 16, 16)
	grid.FillRect(0, 18, 20, 2, physics.TileSolid) // floor at Y = 288
	grid.FillRect(0, 0, 20, 1, physics.TileSolid)  // ceiling at Y = 16

	world := &physics.World{
		Tiles:    grid,
		GravityY: 800,
	}
	// Low-gravity zone on the left (X: 0..100).
	world.AddTrigger(&physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: 0, Y: 16, W: 100, H: 272}),
		Area: physics.AreaSettings{
			GravityMode: physics.AreaOverrideReplace,
			GravityY:    200,
		},
	})
	// Upward anti-gravity elevator on the right (X: 200..300).
	world.AddTrigger(&physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: 200, Y: 16, W: 100, H: 272}),
		Area: physics.AreaSettings{
			GravityMode: physics.AreaOverrideReplace,
			GravityY:    -600,
		},
	})

	lowG := physics.NewCharacterBody(physics.NewBoxBody(40, 100, 12, 16))
	lowG.World = world
	normalG := physics.NewCharacterBody(physics.NewBoxBody(140, 100, 12, 16))
	normalG.World = world
	antiG := physics.NewCharacterBody(physics.NewBoxBody(240, 272, 12, 16))
	antiG.World = world
	zeroScale := physics.NewCharacterBody(physics.NewBoxBody(160, 100, 12, 16))
	zeroScale.World = world
	zeroScale.SetGravityScale(0)

	if _, gy := lowG.EffectiveGravity(); !near(gy, 200, 1e-4) {
		t.Fatalf("lowG.EffectiveGravity() Y = %v, want 200", gy)
	}

	for range 25 {
		lowG.Move(grid, nil, nil, dt)
		normalG.Move(grid, nil, nil, dt)
		antiG.Move(grid, nil, nil, dt)
		zeroScale.Move(grid, nil, nil, dt)
	}

	// Normal-gravity character falls significantly further down than low-gravity character.
	if normalG.Body.Shape.Box.Y <= lowG.Body.Shape.Box.Y+20 {
		t.Fatalf("expected normalG (%v) to fall further than lowG (%v)", normalG.Body.Shape.Box.Y, lowG.Body.Shape.Box.Y)
	}
	// Anti-gravity character rises upward off the floor.
	if antiG.Body.Shape.Box.Y >= 240 {
		t.Fatalf("expected antiG character to rise upward, Y=%v", antiG.Body.Shape.Box.Y)
	}
	// Weightless character stays put.
	if zeroScale.Body.Shape.Box.Y != 100 {
		t.Fatalf("expected zeroScale character to remain at Y=100, got %v", zeroScale.Body.Shape.Box.Y)
	}
}

func TestAreaSimulationDeterminism2D(t *testing.T) {
	dt := time.Second / 60

	run := func() ([]float32, float32, float32) {
		w := &physics.RigidWorld{GravityY: 450}
		w.AddTrigger(&physics.Trigger{
			Shape: physics.BoxShape(physics.Box{X: -100, Y: 80, W: 300, H: 200}),
			Area: physics.AreaSettings{
				Priority:         1,
				FluidDensity:     1.0,
				FluidLinearDrag:  3.5,
				FluidAngularDrag: 2.0,
				FluidVelX:        25,
			},
		})
		w.AddTrigger(&physics.Trigger{
			Shape: physics.CircleShape(physics.Circle{X: 180, Y: 40, R: 120}),
			Area: physics.AreaSettings{
				Priority:            2,
				GravityMode:         physics.AreaOverrideAdd,
				PointGravity:        true,
				GravityPointX:       180,
				GravityPointY:       40,
				GravityStrength:     180,
				GravityFalloff:      physics.GravityFalloffInverse,
				GravityUnitDistance: 50,
				LinearDampingMode:   physics.AreaOverrideAdd,
				LinearDamping:       0.5,
			},
		})

		b1 := physics.NewRigidBox(40, 60, 20, 20, 200)
		b1.VX = 30
		b1.AngularVel = 2.5
		b2 := physics.NewRigidCircle(130, 50, 10, 120)
		b2.SetGravityScale(0.75)
		b2.VY = -40
		w.AddBody(b1, b2)

		charWorld := &physics.World{GravityY: 450}
		charWorld.AddTrigger(w.Triggers...)
		ch := physics.NewCharacterBody(physics.NewBoxBody(120, 40, 12, 16))
		ch.World = charWorld

		for range 240 {
			w.Step(dt)
			ch.Move(nil, nil, nil, dt)
		}
		return []float32{
			b1.X, b1.Y, b1.VX, b1.VY, b1.Angle, b1.AngularVel,
			b2.X, b2.Y, b2.VX, b2.VY, b2.Angle, b2.AngularVel,
		}, ch.Body.Shape.Box.X, ch.Body.Shape.Box.Y
	}

	s1, cx1, cy1 := run()
	s2, cx2, cy2 := run()
	for i := range s1 {
		if math.Float32bits(s1[i]) != math.Float32bits(s2[i]) {
			t.Fatalf("non-deterministic rigid body state[%d]: run1=%v run2=%v", i, s1[i], s2[i])
		}
	}
	if math.Float32bits(cx1) != math.Float32bits(cx2) || math.Float32bits(cy1) != math.Float32bits(cy2) {
		t.Fatalf("non-deterministic character state: run1=(%v,%v) run2=(%v,%v)", cx1, cy1, cx2, cy2)
	}
}

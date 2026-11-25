package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func near32(a, b, tol float32) bool {
	return math.Abs(float64(a-b)) <= float64(tol)
}

func TestBodyGravityScale3D(t *testing.T) {
	dt := time.Second / 60
	w := physics3d.NewWorld()

	box := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	normal := w.AddBody(physics3d.NewDynamicBody(box, physics3d.Vec3{X: 0, Y: 10}, 1))
	normal.LinearDamping = 0

	weightlessDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 3, Y: 10}, 1)
	weightlessDef.LinearDamping = 0
	weightlessDef.GravityScale = 0
	weightless := w.AddBody(weightlessDef)

	doubleDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 6, Y: 10}, 1)
	doubleDef.LinearDamping = 0
	doubleDef.GravityScale = 2
	doubleG := w.AddBody(doubleDef)

	invertedDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 9, Y: 10}, 1)
	invertedDef.LinearDamping = 0
	invertedDef.GravityScale = -0.5
	inverted := w.AddBody(invertedDef)

	// Zero-initialized literal defaults to effective scale 1, and SetGravityScale(0) works on it.
	litDefault := w.AddBody(physics3d.Body{Shape: box, Pos: physics3d.Vec3{X: 12, Y: 10}, Mass: 1})
	litZeroDef := physics3d.Body{Shape: box, Pos: physics3d.Vec3{X: 15, Y: 10}, Mass: 1}
	litZeroDef.SetGravityScale(0)
	litZero := w.AddBody(litZeroDef)

	for range 30 {
		w.Step(dt)
	}

	drop := 10 - normal.Pos.Y
	if drop <= 0.5 {
		t.Fatalf("expected normal body to fall, drop=%v", drop)
	}
	if !near32(litDefault.Pos.Y, normal.Pos.Y, 1e-4) {
		t.Fatalf("zero-literal default gravity mismatch: lit=%v normal=%v", litDefault.Pos.Y, normal.Pos.Y)
	}
	if weightless.Pos.Y != 10 || weightless.Vel.Y != 0 {
		t.Fatalf("expected weightless body unchanged, got Pos.Y=%v Vel.Y=%v", weightless.Pos.Y, weightless.Vel.Y)
	}
	if litZero.Pos.Y != 10 || litZero.Vel.Y != 0 {
		t.Fatalf("expected litZero body unchanged, got Pos.Y=%v Vel.Y=%v", litZero.Pos.Y, litZero.Vel.Y)
	}
	if !near32(10-doubleG.Pos.Y, drop*2, 1e-3) {
		t.Fatalf("expected doubleG drop ~= 2*normal (%v), got %v", drop*2, 10-doubleG.Pos.Y)
	}
	if !near32(inverted.Pos.Y-10, drop*0.5, 1e-3) {
		t.Fatalf("expected inverted rise ~= 0.5*normal (%v), got %v", drop*0.5, inverted.Pos.Y-10)
	}
}

func TestAreaGravityModesAndPriority3D(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{Y: -10}

	vol := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 10, Z: 10}})
	p := physics3d.Vec3{X: 0, Y: 0, Z: 0}

	// Base gravity when no trigger areas exist.
	if g := w.GravityAt(p); !near32(g.Y, -10, 1e-5) {
		t.Fatalf("base gravity = %v, want (0,-10,0)", g)
	}

	// Add mode adds on top of base gravity.
	addLow := physics3d.NewStaticBody(vol, p)
	addLow.IsTrigger = true
	addLow.Area = physics3d.AreaSettings{
		Priority:    1,
		GravityMode: physics3d.AreaOverrideAdd,
		Gravity:     physics3d.Vec3{X: 2, Y: -3, Z: 1},
	}
	w.AddBody(addLow)
	if g := w.GravityAt(p); !near32(g.X, 2, 1e-5) || !near32(g.Y, -13, 1e-5) || !near32(g.Z, 1, 1e-5) {
		t.Fatalf("Add gravity = %v, want (2,-13,1)", g)
	}

	// Combine at priority 5 sums equal-priority Combine areas and replaces lower priority + base.
	combA := physics3d.NewStaticBody(vol, p)
	combA.IsTrigger = true
	combA.Area = physics3d.AreaSettings{
		Priority:    5,
		GravityMode: physics3d.AreaOverrideCombine,
		Gravity:     physics3d.Vec3{X: 4, Y: -2, Z: 0},
	}
	combB := physics3d.NewStaticBody(vol, p)
	combB.IsTrigger = true
	combB.Area = physics3d.AreaSettings{
		Priority:    5,
		GravityMode: physics3d.AreaOverrideCombine,
		Gravity:     physics3d.Vec3{X: -1, Y: -5, Z: 3},
	}
	w.AddBody(combA)
	w.AddBody(combB)
	if g := w.GravityAt(p); !near32(g.X, 3, 1e-5) || !near32(g.Y, -7, 1e-5) || !near32(g.Z, 3, 1e-5) {
		t.Fatalf("Combine gravity = %v, want (3,-7,3)", g)
	}

	// Higher-priority Add still adds on top of lower-priority Combine/Replace.
	addHigh := physics3d.NewStaticBody(vol, p)
	addHigh.IsTrigger = true
	addHigh.Area = physics3d.AreaSettings{
		Priority:    10,
		GravityMode: physics3d.AreaOverrideAdd,
		Gravity:     physics3d.Vec3{X: 0, Y: 5, Z: -1},
	}
	w.AddBody(addHigh)
	if g := w.GravityAt(p); !near32(g.X, 3, 1e-5) || !near32(g.Y, -2, 1e-5) || !near32(g.Z, 2, 1e-5) {
		t.Fatalf("High Add + Combine gravity = %v, want (3,-2,2)", g)
	}

	// Replace at priority 8 supersedes priority 5, ties break by insertion order, priority 10 Add still adds.
	repFirst := physics3d.NewStaticBody(vol, p)
	repFirst.IsTrigger = true
	repFirst.Area = physics3d.AreaSettings{
		Priority:    8,
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{X: -20, Y: 0, Z: 0},
	}
	repSecond := physics3d.NewStaticBody(vol, p)
	repSecond.IsTrigger = true
	repSecond.Area = physics3d.AreaSettings{
		Priority:    8,
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{X: 999, Y: 999, Z: 999},
	}
	w.AddBody(repFirst)
	w.AddBody(repSecond)
	if g := w.GravityAt(p); !near32(g.X, -20, 1e-5) || !near32(g.Y, 5, 1e-5) || !near32(g.Z, -1, 1e-5) {
		t.Fatalf("High Add + Replace gravity = %v, want (-20,5,-1)", g)
	}

	// Layer/mask filtering excludes non-matching triggers.
	w2 := physics3d.NewWorld()
	w2.Gravity = physics3d.Vec3{Y: -10}
	filtered := physics3d.NewStaticBody(vol, p)
	filtered.IsTrigger = true
	filtered.Layer = physics3d.LayerBit(1)
	filtered.Mask = physics3d.LayerBit(2)
	filtered.Area = physics3d.AreaSettings{
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{Y: 20},
	}
	w2.AddBody(filtered)
	probe := physics3d.SphereShape(physics3d.Sphere{Center: p, Radius: 0.5})
	effMatch := w2.AreaEffectAtShape(probe, physics3d.LayerBit(2), physics3d.LayerBit(1))
	effMiss := w2.AreaEffectAtShape(probe, physics3d.LayerBit(3), physics3d.LayerBit(1))
	if !near32(effMatch.Gravity.Y, 20, 1e-5) {
		t.Fatalf("matching layer gravity = %v, want 20", effMatch.Gravity.Y)
	}
	if !near32(effMiss.Gravity.Y, -10, 1e-5) {
		t.Fatalf("non-matching layer gravity = %v, want -10", effMiss.Gravity.Y)
	}
}

func TestAreaPointGravityFallAndOrbit3D(t *testing.T) {
	dt := time.Second / 120

	// 1. Verify falloff modes via GravityAt.
	for _, tc := range []struct {
		name    string
		falloff physics3d.GravityFalloff
		unit    float32
		dist    float32
		wantMag float32
	}{
		{"None", physics3d.GravityFalloffNone, 5, 20, 40},
		{"LinearHalf", physics3d.GravityFalloffLinear, 40, 20, 20},
		{"LinearCutoff", physics3d.GravityFalloffLinear, 15, 20, 0},
		{"Inverse", physics3d.GravityFalloffInverse, 10, 20, 20},
		{"InverseSquare", physics3d.GravityFalloffInverseSquare, 10, 20, 10},
	} {
		w := physics3d.NewWorld()
		planet := physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 100}), physics3d.Vec3{})
		planet.IsTrigger = true
		planet.Area = physics3d.AreaSettings{
			GravityMode:         physics3d.AreaOverrideReplace,
			PointGravity:        true,
			GravityPoint:        physics3d.Vec3{},
			GravityStrength:     40,
			GravityFalloff:      tc.falloff,
			GravityUnitDistance: tc.unit,
		}
		w.AddBody(planet)
		g := w.GravityAt(physics3d.Vec3{X: tc.dist})
		if !near32(g.X, -tc.wantMag, 1e-3) || !near32(g.Y, 0, 1e-3) || !near32(g.Z, 0, 1e-3) {
			t.Fatalf("falloff %s at (%v,0,0): got %v, want (%v,0,0)", tc.name, tc.dist, g, -tc.wantMag)
		}
	}

	// 2. Body falls directly toward point center in 3D.
	fallWorld := physics3d.NewWorld()
	planet := physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 100}), physics3d.Vec3{})
	planet.IsTrigger = true
	planet.Area = physics3d.AreaSettings{
		GravityMode:     physics3d.AreaOverrideReplace,
		PointGravity:    true,
		GravityPoint:    physics3d.Vec3{},
		GravityStrength: 20,
	}
	fallWorld.AddBody(planet)
	probeDef := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: 6, Y: 8, Z: 0}, 1)
	probeDef.LinearDamping = 0
	probe := fallWorld.AddBody(probeDef)
	for range 90 {
		fallWorld.Step(dt)
	}
	if probe.Pos.Len() >= 5.0 {
		t.Fatalf("expected body to fall toward center, dist=%v (started at 10)", probe.Pos.Len())
	}
	if !near32(probe.Pos.Z, 0, 1e-4) {
		t.Fatalf("expected radial fall in XY plane, Z=%v", probe.Pos.Z)
	}

	// 3. Satellite orbits planet in 3D under inverse-square point gravity.
	const (
		r0       = 10.0
		unitDist = 10.0
		gStr     = 40.0
	)
	orbitWorld := physics3d.NewWorld()
	orbitWorld.Gravity = physics3d.Vec3{}
	field := physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 50}), physics3d.Vec3{})
	field.IsTrigger = true
	field.Area = physics3d.AreaSettings{
		GravityMode:         physics3d.AreaOverrideReplace,
		PointGravity:        true,
		GravityPoint:        physics3d.Vec3{},
		GravityStrength:     gStr,
		GravityFalloff:      physics3d.GravityFalloffInverseSquare,
		GravityUnitDistance: unitDist,
	}
	orbitWorld.AddBody(field)

	satDef := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}), physics3d.Vec3{X: r0}, 1)
	satDef.LinearDamping = 0
	satDef.Vel = physics3d.Vec3{Z: float32(math.Sqrt(gStr * r0))} // v = sqrt(400) = 20
	sat := orbitWorld.AddBody(satDef)

	var seenQ [4]bool
	minR, maxR := float32(1e9), float32(0)
	// Period T = 2*pi*10 / 20 = pi seconds (~377 steps at 120 Hz); run ~2 orbits.
	for range 800 {
		orbitWorld.Step(dt)
		r := sat.Pos.Len()
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		switch {
		case sat.Pos.X >= 0 && sat.Pos.Z >= 0:
			seenQ[0] = true
		case sat.Pos.X < 0 && sat.Pos.Z >= 0:
			seenQ[1] = true
		case sat.Pos.X < 0 && sat.Pos.Z < 0:
			seenQ[2] = true
		case sat.Pos.X >= 0 && sat.Pos.Z < 0:
			seenQ[3] = true
		}
	}
	for q, ok := range seenQ {
		if !ok {
			t.Fatalf("orbiting body never visited XZ quadrant %d (minR=%v maxR=%v)", q, minR, maxR)
		}
	}
	if minR < 9.0 || maxR > 11.0 {
		t.Fatalf("3D orbit drifted outside [9, 11]: minR=%v maxR=%v", minR, maxR)
	}
}

func TestAreaDampingOverrides3D(t *testing.T) {
	dt := time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}

	mud := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 10, Z: 10}}), physics3d.Vec3{X: 0})
	mud.IsTrigger = true
	mud.Area = physics3d.AreaSettings{
		LinearDampingMode:  physics3d.AreaOverrideReplace,
		LinearDamping:      8,
		AngularDampingMode: physics3d.AreaOverrideReplace,
		AngularDamping:     10,
	}
	w.AddBody(mud)

	vacuum := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 10, Z: 10}}), physics3d.Vec3{X: 40})
	vacuum.IsTrigger = true
	vacuum.Area = physics3d.AreaSettings{
		LinearDampingMode:  physics3d.AreaOverrideReplace,
		LinearDamping:      0,
		AngularDampingMode: physics3d.AreaOverrideReplace,
		AngularDamping:     0,
	}
	w.AddBody(vacuum)

	box := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	bMudDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 0}, 1)
	bMudDef.Vel = physics3d.Vec3{X: 10}
	bMudDef.AngVel = physics3d.Vec3{Y: 6}
	bMud := w.AddBody(bMudDef)

	bVacDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 40}, 1)
	bVacDef.Vel = physics3d.Vec3{X: 10}
	bVacDef.AngVel = physics3d.Vec3{Y: 6}
	bVacDef.LinearDamping = 5
	bVacDef.AngularDamping = 5
	bVac := w.AddBody(bVacDef)

	bFreeDef := physics3d.NewDynamicBody(box, physics3d.Vec3{X: 80}, 1)
	bFreeDef.Vel = physics3d.Vec3{X: 10}
	bFreeDef.AngVel = physics3d.Vec3{Y: 6}
	bFree := w.AddBody(bFreeDef)

	for range 30 {
		w.Step(dt)
	}

	if bMud.Vel.X >= bFree.Vel.X*0.2 || bMud.AngVel.Y >= bFree.AngVel.Y*0.2 {
		t.Fatalf("expected mud area to strongly damp body: mud=(vx=%v wy=%v) free=(vx=%v wy=%v)", bMud.Vel.X, bMud.AngVel.Y, bFree.Vel.X, bFree.AngVel.Y)
	}
	if !near32(bVac.Vel.X, 10, 1e-3) || !near32(bVac.AngVel.Y, 6, 1e-3) {
		t.Fatalf("expected zero-damping Replace area to preserve velocity, got vx=%v wy=%v", bVac.Vel.X, bVac.AngVel.Y)
	}
}

func TestAreaBuoyancyFloatingCrateAndSinking3D(t *testing.T) {
	dt := time.Second / 60
	const (
		waterSurface = 0.0
		poolFloorY   = -3.0
		waterDensity = 1000.0
	)

	w := physics3d.NewWorld()
	// Pool floor top at Y = -3.
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 30, Y: 1, Z: 30}}),
		physics3d.Vec3{Y: poolFloorY - 1},
	))

	// Water volume spanning Y in [-3, 0].
	waterDef := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 30, Y: 1.5, Z: 30}}),
		physics3d.Vec3{Y: -1.5},
	)
	waterDef.IsTrigger = true
	waterDef.Area = physics3d.AreaSettings{
		FluidDensity:     waterDensity,
		FluidLinearDrag:  6.0,
		FluidAngularDrag: 4.0,
	}
	water := w.AddBody(waterDef)

	// 1x1x1 m crates (volume = 1 m^3), so Mass equals implicit density in kg/m^3.
	crateShape := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	mkCrate := func(x, mass float32) *physics3d.Body {
		b := physics3d.NewDynamicBody(crateShape, physics3d.Vec3{X: x, Y: 0.5}, mass)
		b.LinearDamping = 0
		b.AngularDamping = 0
		return w.AddBody(b)
	}

	light := mkCrate(-6, 250)  // 25% submerged -> center Y = +0.25
	half := mkCrate(-2, 500)   // 50% submerged -> center Y =  0.00
	heavy := mkCrate(2, 750)   // 75% submerged -> center Y = -0.25
	sinker := mkCrate(6, 1500) // denser than water -> sinks to floor at Y = -9.5
	half.AngVel = physics3d.Vec3{Y: 5}

	for range 360 {
		w.Step(dt)
	}

	checkFloat := func(name string, b *physics3d.Body, wantFrac float32) {
		t.Helper()
		// Submerged depth = wantFrac * 1.0, bottom = waterSurface - wantFrac, center = bottom + 0.5.
		wantY := waterSurface + (0.5 - wantFrac)
		if !near32(b.Pos.Y, wantY, 0.03) {
			t.Fatalf("%s crate Pos.Y=%v, want ~%v (submerged frac %v)", name, b.Pos.Y, wantY, wantFrac)
		}
		if math.Abs(float64(b.Vel.Y)) > 0.05 {
			t.Fatalf("%s crate not settled: Vel.Y=%v", name, b.Vel.Y)
		}
	}
	checkFloat("light(0.25)", light, 0.25)
	checkFloat("half(0.50)", half, 0.50)
	checkFloat("heavy(0.75)", heavy, 0.75)

	if half.AngVel.Len() > 0.05 {
		t.Fatalf("expected fluid angular drag to damp spin, got AngVel=%v", half.AngVel)
	}
	if !near32(sinker.Pos.Y, poolFloorY+0.5, 0.05) {
		t.Fatalf("expected dense crate to sink to floor Y~%v, got %v", poolFloorY+0.5, sinker.Pos.Y)
	}

	// Verify fluid current wakes/carries floating crate horizontally.
	water.Area.FluidVelocity = physics3d.Vec3{X: 3}
	half.Wake()
	startX := half.Pos.X
	for range 120 {
		w.Step(dt)
	}
	if half.Pos.X <= startX+2.0 {
		t.Fatalf("expected fluid current to carry crate in +X: startX=%v endX=%v", startX, half.Pos.X)
	}
}

func TestAreaWakesSleepingBody3D(t *testing.T) {
	dt := time.Second / 60
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 1, Z: 10}}),
		physics3d.Vec3{Y: -1},
	))
	crate := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{Y: 0.5},
		1,
	))

	for range 120 {
		w.Step(dt)
	}
	if !crate.Sleeping {
		t.Fatalf("expected crate to fall asleep on floor before area is added")
	}

	// Adding an upward anti-gravity trigger area must wake the sleeping crate and lift it.
	antiG := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 5, Z: 5}}),
		physics3d.Vec3{Y: 2},
	)
	antiG.IsTrigger = true
	antiG.Area = physics3d.AreaSettings{
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{Y: 12},
	}
	w.AddBody(antiG)

	for range 40 {
		w.Step(dt)
	}
	if crate.Sleeping || crate.Pos.Y <= 1.5 {
		t.Fatalf("expected anti-gravity area to wake and lift crate, sleeping=%v Pos.Y=%v", crate.Sleeping, crate.Pos.Y)
	}
}

func TestCharacterControllerRespectsAreaGravity3D(t *testing.T) {
	dt := time.Second / 60
	w := physics3d.NewWorld()
	// Floor top at Y = 0.
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 40, Y: 1, Z: 40}}),
		physics3d.Vec3{Y: -1},
	))

	// Low-gravity zone around X = -10.
	lowZone := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 4, Y: 10, Z: 4}}),
		physics3d.Vec3{X: -10, Y: 5},
	)
	lowZone.IsTrigger = true
	lowZone.Area = physics3d.AreaSettings{
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{Y: -2.0},
	}
	w.AddBody(lowZone)

	// Anti-gravity elevator around X = 10.
	antiZone := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 4, Y: 10, Z: 4}}),
		physics3d.Vec3{X: 10, Y: 5},
	)
	antiZone.IsTrigger = true
	antiZone.Area = physics3d.AreaSettings{
		GravityMode: physics3d.AreaOverrideReplace,
		Gravity:     physics3d.Vec3{Y: 6.0},
	}
	w.AddBody(antiZone)

	// Point-gravity zone around X = 25 pulling toward (28, 5, 0).
	pointZone := physics3d.NewStaticBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 6}),
		physics3d.Vec3{X: 25, Y: 5},
	)
	pointZone.IsTrigger = true
	pointZone.Area = physics3d.AreaSettings{
		GravityMode:     physics3d.AreaOverrideReplace,
		PointGravity:    true,
		GravityPoint:    physics3d.Vec3{X: 28, Y: 5},
		GravityStrength: 10,
	}
	w.AddBody(pointZone)

	ccLow := physics3d.NewCharacterController(physics3d.Vec3{X: -10, Y: 8}, 0.3, 1.8)
	ccNormal := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 8}, 0.3, 1.8)
	ccAnti := physics3d.NewCharacterController(physics3d.Vec3{X: 10, Y: 0}, 0.3, 1.8)
	ccZero := physics3d.NewCharacterController(physics3d.Vec3{X: 3, Y: 8}, 0.3, 1.8)
	ccZero.SetGravityScale(0)
	ccPoint := physics3d.NewCharacterController(physics3d.Vec3{X: 22, Y: 4.1}, 0.3, 1.8)

	if g := ccLow.EffectiveGravity(w); !near32(g.Y, -2.0, 1e-4) {
		t.Fatalf("ccLow.EffectiveGravity = %v, want (0,-2,0)", g)
	}

	for range 30 {
		ccLow.Move(w, physics3d.Vec3{}, dt)
		ccNormal.Move(w, physics3d.Vec3{}, dt)
		ccAnti.Move(w, physics3d.Vec3{}, dt)
		ccZero.Move(w, physics3d.Vec3{}, dt)
		ccPoint.Move(w, physics3d.Vec3{}, dt)
	}

	if ccNormal.Pos.Y >= ccLow.Pos.Y-0.5 {
		t.Fatalf("expected ccNormal (%v) to fall further than ccLow (%v)", ccNormal.Pos.Y, ccLow.Pos.Y)
	}
	if ccAnti.Pos.Y <= 0.5 || ccAnti.Grounded {
		t.Fatalf("expected ccAnti to lift off floor, Pos.Y=%v Grounded=%v", ccAnti.Pos.Y, ccAnti.Grounded)
	}
	if !near32(ccZero.Pos.Y, 8, 1e-4) {
		t.Fatalf("expected weightless character to remain at Y=8, got %v", ccZero.Pos.Y)
	}
	if ccPoint.Pos.X <= 22.5 {
		t.Fatalf("expected point-gravity area to pull character toward +X center, Pos.X=%v", ccPoint.Pos.X)
	}
}

func TestAreaSimulationDeterminism3D(t *testing.T) {
	dt := time.Second / 60

	run := func() ([]float32, physics3d.Vec3) {
		w := physics3d.NewWorld()

		water := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 15, Y: 5, Z: 15}}),
			physics3d.Vec3{Y: -5},
		)
		water.IsTrigger = true
		water.Area = physics3d.AreaSettings{
			Priority:         1,
			FluidDensity:     1000,
			FluidLinearDrag:  4.0,
			FluidAngularDrag: 2.0,
			FluidVelocity:    physics3d.Vec3{X: 1.5, Z: -0.5},
		}
		w.AddBody(water)

		planet := physics3d.NewStaticBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 12}),
			physics3d.Vec3{X: 6, Y: 2, Z: 0},
		)
		planet.IsTrigger = true
		planet.Area = physics3d.AreaSettings{
			Priority:            2,
			GravityMode:         physics3d.AreaOverrideAdd,
			PointGravity:        true,
			GravityPoint:        physics3d.Vec3{X: 6, Y: 2, Z: 0},
			GravityStrength:     4.0,
			GravityFalloff:      physics3d.GravityFalloffInverse,
			GravityUnitDistance: 3.0,
			LinearDampingMode:   physics3d.AreaOverrideAdd,
			LinearDamping:       0.25,
		}
		w.AddBody(planet)

		b1Def := physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
			physics3d.Vec3{X: -1, Y: 1, Z: 0.5},
			500,
		)
		b1Def.Vel = physics3d.Vec3{X: 1, Y: -0.5, Z: 0.25}
		b1Def.AngVel = physics3d.Vec3{X: 0.5, Y: 1.5, Z: -0.3}
		b1 := w.AddBody(b1Def)

		b2Def := physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			physics3d.Vec3{X: 3, Y: 3, Z: 1},
			2,
		)
		b2Def.SetGravityScale(0.6)
		b2Def.Vel = physics3d.Vec3{Z: 2.0}
		b2 := w.AddBody(b2Def)

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 2, Y: 4, Z: 0}, 0.3, 1.8)

		for range 240 {
			w.Step(dt)
			cc.Move(w, physics3d.Vec3{X: 0.5}, dt)
		}

		return []float32{
			b1.Pos.X, b1.Pos.Y, b1.Pos.Z, b1.Vel.X, b1.Vel.Y, b1.Vel.Z,
			b1.Rot.X, b1.Rot.Y, b1.Rot.Z, b1.Rot.W, b1.AngVel.X, b1.AngVel.Y, b1.AngVel.Z,
			b2.Pos.X, b2.Pos.Y, b2.Pos.Z, b2.Vel.X, b2.Vel.Y, b2.Vel.Z,
		}, cc.Pos
	}

	s1, c1 := run()
	s2, c2 := run()
	for i := range s1 {
		if math.Float32bits(s1[i]) != math.Float32bits(s2[i]) {
			t.Fatalf("non-deterministic 3D body state[%d]: run1=%v run2=%v", i, s1[i], s2[i])
		}
	}
	if math.Float32bits(c1.X) != math.Float32bits(c2.X) ||
		math.Float32bits(c1.Y) != math.Float32bits(c2.Y) ||
		math.Float32bits(c1.Z) != math.Float32bits(c2.Z) {
		t.Fatalf("non-deterministic 3D character pos: run1=%v run2=%v", c1, c2)
	}
}

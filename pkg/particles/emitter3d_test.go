package particles_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/nsgo/core"
)

type decodedInstance struct {
	center math3d.Vec3
	right  math3d.Vec3
	up     math3d.Vec3
	r, g   uint8
	b, a   uint8
}

func decodeInstances(t *testing.T, raw []byte) []decodedInstance {
	t.Helper()
	if len(raw)%particles.ParticleInstanceStride != 0 {
		t.Fatalf("raw length %d not a multiple of %d", len(raw), particles.ParticleInstanceStride)
	}
	n := len(raw) / particles.ParticleInstanceStride
	out := make([]decodedInstance, n)
	readF32 := func(b []byte) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(b))
	}
	for i := range n {
		s := raw[i*particles.ParticleInstanceStride:]
		out[i] = decodedInstance{
			center: math3d.Vec3{X: readF32(s[0:]), Y: readF32(s[4:]), Z: readF32(s[8:])},
			right:  math3d.Vec3{X: readF32(s[12:]), Y: readF32(s[16:]), Z: readF32(s[20:])},
			up:     math3d.Vec3{X: readF32(s[24:]), Y: readF32(s[28:]), Z: readF32(s[32:])},
			r:      s[36],
			g:      s[37],
			b:      s[38],
			a:      s[39],
		}
	}
	return out
}

func newTestEmitter3D(seed uint64, maxParticles int) *particles.Emitter3D {
	return &particles.Emitter3D{
		Position:           math3d.Vec3{X: 1, Y: 2, Z: 3},
		Direction:          math3d.Vec3Up(),
		Shape:              particles.Cone(0.5, 0.35),
		Rate:               float32(maxParticles) * 2,
		MaxParticles:       maxParticles,
		LifetimeMin:        500 * time.Millisecond,
		LifetimeMax:        1500 * time.Millisecond,
		SpeedMin:           2,
		SpeedMax:           8,
		Spread:             0.2,
		Drag:               0.5,
		Gravity:            math3d.Vec3{Y: -9.8},
		SizeMin:            0.1,
		SizeMax:            0.4,
		InitialRotationMin: 0,
		InitialRotationMax: 2 * math.Pi,
		RotationSpeedMin:   -2,
		RotationSpeedMax:   2,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.5},
			{T: 0.5, Value: 1.5},
			{T: 1, Value: 0},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		RotationCurve: particles.FloatCurve{
			{T: 0, Value: 0},
			{T: 1, Value: 1},
		},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.RGBA(0xffaa33, 1)},
			{T: 1, Color: core.RGBA(0x3366ff, 0.5)},
		},
		Seed: seed,
	}
}

func TestEmitter3DSeedDeterminism10k(t *testing.T) {
	const count = 10000
	e1 := newTestEmitter3D(280, count)
	e2 := newTestEmitter3D(280, count)
	eDiff := newTestEmitter3D(281, count)

	e1.Burst(count)
	e2.Burst(count)
	eDiff.Burst(count)

	step := time.Second / 60
	for range 10 {
		e1.Step(step)
		e2.Step(step)
		eDiff.Step(step)
	}

	if e1.Count() != count || e2.Count() != count {
		t.Fatalf("counts = (%d, %d), want %d", e1.Count(), e2.Count(), count)
	}

	camPos := math3d.Vec3{Z: 15}
	camFwd := math3d.Vec3Forward()
	camRight := math3d.Vec3Right()
	camUp := math3d.Vec3Up()

	e1.SortByDepth(camPos, camFwd)
	e2.SortByDepth(camPos, camFwd)
	eDiff.SortByDepth(camPos, camFwd)

	b1 := e1.AppendInstances(nil, camRight, camUp)
	b2 := e2.AppendInstances(nil, camRight, camUp)
	bDiff := eDiff.AppendInstances(nil, camRight, camUp)

	if !bytes.Equal(b1, b2) {
		t.Fatal("same-seed 10k emitters produced different instance bytes")
	}
	if bytes.Equal(b1, bDiff) {
		t.Fatal("different-seed 10k emitters unexpectedly produced identical instance bytes")
	}

	// Reset replays identically from Seed.
	e1.Reset()
	if e1.Count() != 0 || e1.Active() {
		t.Fatalf("after Reset: count=%d active=%v, want 0/false", e1.Count(), e1.Active())
	}
	e1.Burst(count)
	for range 10 {
		e1.Step(step)
	}
	e1.SortByDepth(camPos, camFwd)
	bReplay := e1.AppendInstances(nil, camRight, camUp)
	if !bytes.Equal(b1, bReplay) {
		t.Fatal("Reset did not replay identical 10k particle bytes")
	}
}

func TestEmitter3DSpawnShapes(t *testing.T) {
	const n = 500

	t.Run("point", func(t *testing.T) {
		e := &particles.Emitter3D{
			Position:     math3d.Vec3{X: 3, Y: -2, Z: 5},
			Shape:        particles.Point3D(),
			MaxParticles: n,
			Seed:         1,
		}
		e.Burst(n)
		for i := range e.Count() {
			p := e.Particle(i).Position
			if p != e.Position {
				t.Fatalf("point particle %d at %v, want %v", i, p, e.Position)
			}
		}
	})

	t.Run("sphere", func(t *testing.T) {
		const r = float32(4)
		e := &particles.Emitter3D{
			Shape:        particles.Sphere(r),
			MaxParticles: n,
			Seed:         2,
		}
		e.Burst(n)
		var maxDist float32
		hasNegY, hasPosY := false, false
		for i := range e.Count() {
			p := e.Particle(i).Position
			d := p.Len()
			if d > r+1e-4 {
				t.Fatalf("sphere particle %d dist=%v > %v", i, d, r)
			}
			if d > maxDist {
				maxDist = d
			}
			if p.Y < 0 {
				hasNegY = true
			}
			if p.Y > 0 {
				hasPosY = true
			}
		}
		if !hasNegY || !hasPosY || maxDist < r*0.7 {
			t.Fatalf("sphere distribution looks degenerate: negY=%v posY=%v maxDist=%v", hasNegY, hasPosY, maxDist)
		}
	})

	t.Run("hemisphere", func(t *testing.T) {
		const r = float32(3)
		dir := math3d.Vec3{X: 1, Y: 1, Z: 0}.Norm()
		e := &particles.Emitter3D{
			Direction:    dir,
			Shape:        particles.Hemisphere(r),
			MaxParticles: n,
			Seed:         3,
		}
		e.Burst(n)
		for i := range e.Count() {
			p := e.Particle(i).Position
			if p.Len() > r+1e-4 {
				t.Fatalf("hemisphere particle %d dist=%v > %v", i, p.Len(), r)
			}
			if p.Dot(dir) < -1e-4 {
				t.Fatalf("hemisphere particle %d below equator: dot=%v", i, p.Dot(dir))
			}
		}
	})

	t.Run("box", func(t *testing.T) {
		e := &particles.Emitter3D{
			Direction:    math3d.Vec3Up(),
			Shape:        particles.Box(2, 4, 6),
			MaxParticles: n,
			Seed:         4,
		}
		e.Burst(n)
		for i := range e.Count() {
			p := e.Particle(i).Position
			if math.Abs(float64(p.X)) > 1+1e-4 || math.Abs(float64(p.Y)) > 2+1e-4 || math.Abs(float64(p.Z)) > 3+1e-4 {
				t.Fatalf("box particle %d out of bounds: %v", i, p)
			}
		}
	})

	t.Run("cone", func(t *testing.T) {
		const r = float32(2)
		const angle = float32(0.5)
		cosLimit := float32(math.Cos(float64(angle))) - 1e-4
		e := &particles.Emitter3D{
			Direction:    math3d.Vec3Up(),
			Shape:        particles.Cone(r, angle),
			SpeedMin:     10,
			SpeedMax:     10,
			MaxParticles: n,
			Seed:         5,
		}
		e.Burst(n)
		for i := range e.Count() {
			pt := e.Particle(i)
			if math.Abs(float64(pt.Position.Y)) > 1e-4 {
				t.Fatalf("cone base y=%v, want 0", pt.Position.Y)
			}
			if pt.Position.Len() > r+1e-4 {
				t.Fatalf("cone base dist=%v > %v", pt.Position.Len(), r)
			}
			vDir := pt.Velocity.Norm()
			if vDir.Dot(math3d.Vec3Up()) < cosLimit {
				t.Fatalf("cone vel dir %v outside angle %v", vDir, angle)
			}
		}
	})

	t.Run("cone_zero_radius", func(t *testing.T) {
		const angle = float32(0.4)
		cosLimit := float32(math.Cos(float64(angle))) - 1e-4
		e := &particles.Emitter3D{
			Direction:    math3d.Vec3UnitX(),
			Shape:        particles.Cone(0, angle),
			SpeedMin:     5,
			SpeedMax:     5,
			MaxParticles: 100,
			Seed:         6,
		}
		e.Burst(100)
		for i := range e.Count() {
			pt := e.Particle(i)
			if pt.Position != (math3d.Vec3{}) {
				t.Fatalf("zero-radius cone spawned at %v", pt.Position)
			}
			if pt.Velocity.Norm().Dot(math3d.Vec3UnitX()) < cosLimit {
				t.Fatalf("zero-radius cone vel %v outside cone", pt.Velocity)
			}
		}
	})

	t.Run("ring", func(t *testing.T) {
		const r = float32(5)
		e := &particles.Emitter3D{
			Direction:    math3d.Vec3Up(),
			Shape:        particles.Ring3D(r),
			MaxParticles: n,
			Seed:         7,
		}
		e.Burst(n)
		for i := range e.Count() {
			p := e.Particle(i).Position
			if math.Abs(float64(p.Y)) > 1e-4 {
				t.Fatalf("ring particle y=%v, want 0", p.Y)
			}
			if math.Abs(float64(p.Len()-r)) > 1e-3 {
				t.Fatalf("ring particle radius=%v, want %v", p.Len(), r)
			}
		}
	})
}

func TestEmitter3DKinematicsCurvesAndExpiration(t *testing.T) {
	e := &particles.Emitter3D{
		Position:           math3d.Vec3{X: 1, Y: 2, Z: 3},
		Direction:          math3d.Vec3UnitX(),
		SpeedMin:           10,
		SpeedMax:           10,
		Drag:               2,
		Gravity:            math3d.Vec3{Y: -10},
		LifetimeMin:        time.Second,
		LifetimeMax:        time.Second,
		SizeMin:            2,
		SizeMax:            2,
		InitialRotationMin: 0.5,
		InitialRotationMax: 0.5,
		RotationSpeedMin:   1,
		RotationSpeedMax:   1,
		SizeCurve:          particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 3}},
		AlphaCurve:         particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 0}},
		RotationCurve:      particles.FloatCurve{{T: 0, Value: 0}, {T: 1, Value: 0.5}},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.RGBA(0xff0000, 1)},
			{T: 1, Color: core.RGBA(0x0000ff, 1)},
		},
	}

	e.Burst(1)
	if !e.Active() || e.Count() != 1 {
		t.Fatalf("after burst: active=%v count=%d", e.Active(), e.Count())
	}

	// Non-positive step is a no-op.
	e.Step(0)
	e.Step(-10 * time.Millisecond)
	if e.Particle(0).Age != 0 {
		t.Fatalf("non-positive Step mutated age to %v", e.Particle(0).Age)
	}

	// Advance 0.5s:
	// v after gravity = (10, -5, 0); after drag exp(-2*0.5) = e^-1:
	// vx = 10 * e^-1, vy = -5 * e^-1
	// pos = (1,2,3) + v*0.5
	// t = 0.5: size = 2 * 2 = 4, alpha = 0.5, rot = 0.5 + 1*0.5 + 0.25 = 1.25
	e.Update(500 * time.Millisecond)
	p := e.Particle(0)
	damp := float32(math.Exp(-1))
	wantVX := 10 * damp
	wantVY := -5 * damp
	if !near(p.Velocity.X, wantVX) || !near(p.Velocity.Y, wantVY) || !near(p.Velocity.Z, 0) {
		t.Fatalf("vel = %v, want (%v, %v, 0)", p.Velocity, wantVX, wantVY)
	}
	if !near(p.Position.X, 1+wantVX*0.5) || !near(p.Position.Y, 2+wantVY*0.5) || !near(p.Position.Z, 3) {
		t.Fatalf("pos = %v, want (%v, %v, 3)", p.Position, 1+wantVX*0.5, 2+wantVY*0.5)
	}
	if !near(p.Size, 4) {
		t.Fatalf("size = %v, want 4", p.Size)
	}
	if !near(p.Alpha, 0.5) {
		t.Fatalf("alpha = %v, want 0.5", p.Alpha)
	}
	if !near(p.Rotation, 1.25) {
		t.Fatalf("rot = %v, want 1.25", p.Rotation)
	}

	// Expire after another 0.5s.
	e.Step(500 * time.Millisecond)
	if e.Count() != 0 || e.Active() {
		t.Fatalf("expected expiration, got count=%d active=%v", e.Count(), e.Active())
	}

	// Default MaxParticles when <= 0 is 256.
	def := &particles.Emitter3D{}
	def.Burst(500)
	if def.Count() != 256 {
		t.Fatalf("default MaxParticles = %d, want 256", def.Count())
	}
}

func TestEmitter3DLocalVsWorldSpace(t *testing.T) {
	rot90Y := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)

	local := &particles.Emitter3D{
		Position:    math3d.Vec3{X: 10, Y: 0, Z: 0},
		Direction:   math3d.Vec3UnitX(),
		Space:       particles.SpaceLocal,
		SpeedMin:    2,
		SpeedMax:    2,
		LifetimeMin: 5 * time.Second,
	}
	world := &particles.Emitter3D{
		Position:    math3d.Vec3{X: 10, Y: 0, Z: 0},
		Direction:   math3d.Vec3UnitX(),
		Space:       particles.SpaceWorld,
		SpeedMin:    2,
		SpeedMax:    2,
		LifetimeMin: 5 * time.Second,
	}

	local.Burst(1)
	world.Burst(1)
	local.Step(time.Second)
	world.Step(time.Second)

	// Move and rotate emitters after emission.
	local.Position = math3d.Vec3{X: 20, Y: 5, Z: 0}
	local.Rotation = rot90Y
	world.Position = math3d.Vec3{X: 20, Y: 5, Z: 0}
	world.Rotation = rot90Y

	// World particle stays at (12, 0, 0).
	if wp := world.ParticleWorldPos(0); !wp.ApproxEqual(math3d.Vec3{X: 12, Y: 0, Z: 0}, 1e-3) {
		t.Fatalf("world particle moved to %v, want (12,0,0)", wp)
	}
	// Local particle stored at local (2, 0, 0); rotated +90 deg around +Y gives (0, 0, -2) + (20, 5, 0) = (20, 5, -2).
	wantLocal := math3d.Vec3{X: 20, Y: 5, Z: -2}
	if lp := local.ParticleWorldPos(0); !lp.ApproxEqual(wantLocal, 1e-3) {
		t.Fatalf("local particle world pos = %v, want %v", lp, wantLocal)
	}

	// AppendInstances packs the transformed world position.
	inst := decodeInstances(t, local.AppendInstances(nil, math3d.Vec3Right(), math3d.Vec3Up()))
	if len(inst) != 1 || !inst[0].center.ApproxEqual(wantLocal, 1e-3) {
		t.Fatalf("local packed center = %v, want %v", inst[0].center, wantLocal)
	}
}

func TestEmitter3DBillboardModes(t *testing.T) {
	camRight := math3d.Vec3Right()
	camUp := math3d.Vec3Up()

	t.Run("spherical_rotated", func(t *testing.T) {
		e := &particles.Emitter3D{
			Billboard:          particles.BillboardSpherical,
			SizeMin:            2,
			SizeMax:            2,
			InitialRotationMin: math.Pi / 2,
			InitialRotationMax: math.Pi / 2,
			LifetimeMin:        time.Second,
		}
		e.Burst(1)
		inst := decodeInstances(t, e.AppendInstances(nil, camRight, camUp))
		// Rotated +90 deg CCW: right -> +Y*2, up -> -X*2.
		if !inst[0].right.ApproxEqual(math3d.Vec3{Y: 2}, 1e-4) {
			t.Fatalf("spherical rotated right = %v, want (0,2,0)", inst[0].right)
		}
		if !inst[0].up.ApproxEqual(math3d.Vec3{X: -2}, 1e-4) {
			t.Fatalf("spherical rotated up = %v, want (-2,0,0)", inst[0].up)
		}
	})

	t.Run("cylindrical", func(t *testing.T) {
		// Camera tilted upward, looking down-forward; cylindrical particle stays locked to +Y.
		tiltedUp := math3d.Vec3{Y: 1, Z: 1}.Norm()
		e := &particles.Emitter3D{
			Billboard:   particles.BillboardCylindrical,
			Axis:        math3d.Vec3Up(),
			SizeMin:     3,
			SizeMax:     3,
			LifetimeMin: time.Second,
		}
		e.Burst(1)
		inst := decodeInstances(t, e.AppendInstances(nil, camRight, tiltedUp))
		if !inst[0].up.ApproxEqual(math3d.Vec3{Y: 3}, 1e-4) {
			t.Fatalf("cylindrical up = %v, want (0,3,0)", inst[0].up)
		}
		if !inst[0].right.ApproxEqual(math3d.Vec3{X: 3}, 1e-4) {
			t.Fatalf("cylindrical right = %v, want (3,0,0)", inst[0].right)
		}
	})

	t.Run("stretched", func(t *testing.T) {
		e := &particles.Emitter3D{
			Billboard:     particles.BillboardStretched,
			Direction:     math3d.Vec3UnitX(),
			SpeedMin:      20,
			SpeedMax:      20,
			SizeMin:       1,
			SizeMax:       1,
			StretchFactor: 0.2,
			LifetimeMin:   time.Second,
		}
		e.Burst(1)
		inst := decodeInstances(t, e.AppendInstances(nil, camRight, camUp))
		// Length = size (1) + speed (20) * stretch (0.2) = 5 along +X.
		if !inst[0].up.ApproxEqual(math3d.Vec3{X: 5}, 1e-4) {
			t.Fatalf("stretched up = %v, want (5,0,0)", inst[0].up)
		}
		// side = velDir (+X) × camBack (+Z) = (0, -1, 0), scaled by size 1.
		if !inst[0].right.ApproxEqual(math3d.Vec3{Y: -1}, 1e-4) {
			t.Fatalf("stretched right = %v, want (0,-1,0)", inst[0].right)
		}
	})
}

func TestEmitter3DSortByDepthBackToFront(t *testing.T) {
	e := &particles.Emitter3D{
		MaxParticles: 10,
		LifetimeMin:  10 * time.Second,
	}
	zs := []float32{-2, -10, 3, -5, 0, -20, 1}
	for _, z := range zs {
		e.Position = math3d.Vec3{Z: z}
		e.Burst(1)
	}

	// Camera at z = +10 looking along -Z (Forward = (0,0,-1)).
	// Depth = (p.Z - 10) * (-1) = 10 - p.Z.
	// Farthest particle has smallest p.Z (-20 -> depth 30), nearest has largest p.Z (+3 -> depth 7).
	camPos := math3d.Vec3{Z: 10}
	camFwd := math3d.Vec3Forward()
	e.SortByDepth(camPos, camFwd)

	inst := decodeInstances(t, e.AppendInstances(nil, math3d.Vec3Right(), math3d.Vec3Up()))
	if len(inst) != len(zs) {
		t.Fatalf("packed %d instances, want %d", len(inst), len(zs))
	}
	for i := 1; i < len(inst); i++ {
		prevDepth := inst[i-1].center.Sub(camPos).Dot(camFwd)
		curDepth := inst[i].center.Sub(camPos).Dot(camFwd)
		if prevDepth < curDepth {
			t.Fatalf("not sorted back-to-front at %d: depth[%d]=%v < depth[%d]=%v", i, i-1, prevDepth, i, curDepth)
		}
	}
	if inst[0].center.Z != -20 || inst[len(inst)-1].center.Z != 3 {
		t.Fatalf("first.Z=%v last.Z=%v, want -20 and 3", inst[0].center.Z, inst[len(inst)-1].center.Z)
	}
}

func TestEmitter3DSteadyStateZeroAllocs10k(t *testing.T) {
	const count = 10000
	e := newTestEmitter3D(77, count)
	e.Rate = 0
	e.LifetimeMin = time.Hour
	e.LifetimeMax = time.Hour
	e.Burst(count)

	step := time.Second / 60
	camPos := math3d.Vec3{Z: 15}
	camFwd := math3d.Vec3Forward()
	camRight := math3d.Vec3Right()
	camUp := math3d.Vec3Up()

	// Warm up scratch buffers.
	e.Step(step)
	e.SortByDepth(camPos, camFwd)
	buf := e.AppendInstances(nil, camRight, camUp)

	if allocs := testing.AllocsPerRun(50, func() {
		e.Step(step)
		buf = e.AppendInstances(buf[:0], camRight, camUp)
	}); allocs != 0 {
		t.Fatalf("Step+AppendInstances allocated %v times/op, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		e.Step(step)
		e.SortByDepth(camPos, camFwd)
		buf = e.AppendInstances(buf[:0], camRight, camUp)
	}); allocs != 0 {
		t.Fatalf("Step+SortByDepth+AppendInstances allocated %v times/op, want 0", allocs)
	}
}

// BenchmarkEmitter3D10kStepAndPack measures stepping and packing 10,000 active 3D particles with curves, drag, gravity, and spin into GPU instance bytes (additive / unsorted path).
func BenchmarkEmitter3D10kStepAndPack(b *testing.B) {
	const count = 10000
	e := newTestEmitter3D(99, count)
	e.Rate = 0
	e.LifetimeMin = time.Hour
	e.LifetimeMax = time.Hour
	e.Burst(count)

	step := time.Second / 60
	camRight := math3d.Vec3Right()
	camUp := math3d.Vec3Up()
	buf := make([]byte, 0, count*particles.ParticleInstanceStride)
	e.Step(step)
	buf = e.AppendInstances(buf[:0], camRight, camUp)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Step(step)
		if n := e.Count(); n < count {
			e.Burst(count - n)
		}
		buf = e.AppendInstances(buf[:0], camRight, camUp)
	}
	if len(buf) != count*particles.ParticleInstanceStride {
		b.Fatalf("packed %d bytes, want %d", len(buf), count*particles.ParticleInstanceStride)
	}
}

// BenchmarkEmitter3D10kStepSortAndPack measures stepping, back-to-front radix sorting, and packing 10,000 active 3D particles for alpha-blended rendering.
func BenchmarkEmitter3D10kStepSortAndPack(b *testing.B) {
	const count = 10000
	e := newTestEmitter3D(101, count)
	e.Rate = 0
	e.LifetimeMin = time.Hour
	e.LifetimeMax = time.Hour
	e.Burst(count)

	step := time.Second / 60
	camPos := math3d.Vec3{Z: 15}
	camFwd := math3d.Vec3Forward()
	camRight := math3d.Vec3Right()
	camUp := math3d.Vec3Up()
	buf := make([]byte, 0, count*particles.ParticleInstanceStride)
	e.Step(step)
	e.SortByDepth(camPos, camFwd)
	buf = e.AppendInstances(buf[:0], camRight, camUp)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Step(step)
		if n := e.Count(); n < count {
			e.Burst(count - n)
		}
		e.SortByDepth(camPos, camFwd)
		buf = e.AppendInstances(buf[:0], camRight, camUp)
	}
	if len(buf) != count*particles.ParticleInstanceStride {
		b.Fatalf("packed %d bytes, want %d", len(buf), count*particles.ParticleInstanceStride)
	}
}

// Emitter3D steps 3D particles deterministically and packs them into 40-byte GPU instances ready for instanced rendering.
func ExampleEmitter3D() {
	e := particles.NewEmitter3D(particles.Emitter3DConfig{
		Position:     math3d.Vec3{Y: 1},
		Direction:    math3d.Vec3Up(),
		Shape:        particles.Cone(0.25, 0.3),
		MaxParticles: 1000,
		LifetimeMin:  500 * time.Millisecond,
		LifetimeMax:  time.Second,
		SpeedMin:     2,
		SpeedMax:     5,
		Gravity:      math3d.Vec3{Y: -9.8},
		Blend:        particles.BlendAdditive,
		Seed:         42,
	})
	e.Burst(50)
	e.Step(time.Second / 60)
	raw := e.AppendInstances(nil, math3d.Vec3Right(), math3d.Vec3Up())
	fmt.Println(e.Count(), len(raw)/particles.ParticleInstanceStride)
	// Output:
	// 50 50
}

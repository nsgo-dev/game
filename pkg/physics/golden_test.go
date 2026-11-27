package physics_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/detmath"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// The trajectories below cover the code TestGoldenTrajectory2D leaves out: XPBD constraints, the character controller, and tile collision against a grid whose origin and tile size are not whole numbers, so a product rounded once by a fused multiply-add lands on a different float than the same product rounded twice. Each hash must be the same on amd64 and arm64; the macOS job runs them on arm64 and the Linux job on amd64.

// goldenGrid returns a walled tile grid with a fractional origin and tile size, a one-way ledge, a low ceiling block and a pillar.
func goldenGrid() *physics.Grid {
	grid := physics.NewGrid(26, 16, 13.7, 11.3)
	grid.OriginX, grid.OriginY = -7.3, 3.9
	grid.FillRect(0, 14, 26, 2, physics.TileSolid)
	grid.FillRect(0, 0, 1, 16, physics.TileSolid)
	grid.FillRect(25, 0, 1, 16, physics.TileSolid)
	grid.FillRect(0, 0, 26, 1, physics.TileSolid)
	grid.FillRect(7, 9, 6, 1, physics.TileOneWay)
	grid.FillRect(15, 10, 3, 1, physics.TileSolid)
	grid.FillRect(19, 11, 1, 3, physics.TileSolid)
	return grid
}

func hashBody(h *detmath.Hasher, b *physics.Body) {
	bounds := b.Bounds()
	h.WriteFloat32(bounds.X)
	h.WriteFloat32(bounds.Y)
	h.WriteFloat32(bounds.W)
	h.WriteFloat32(bounds.H)
	h.WriteFloat32(b.VX)
	h.WriteFloat32(b.VY)
	h.WriteBool(b.Grounded)
	h.WriteBool(b.OnCeiling)
	h.WriteBool(b.OnLeftWall)
	h.WriteBool(b.OnRightWall)
}

func hashMoveResult(h *detmath.Hasher, r physics.MoveResult) {
	h.WriteInt(len(r.Contacts))
	for _, c := range r.Contacts {
		h.WriteInt(c.Col)
		h.WriteInt(c.Row)
		h.WriteUint32(uint32(c.Flags))
		h.WriteFloat32(c.NormalX)
		h.WriteFloat32(c.NormalY)
	}
	h.WriteInt(len(r.Collisions))
	h.WriteBool(r.Grounded)
	h.WriteBool(r.OnWall)
	h.WriteBool(r.OnCeiling)
}

// goldenSoftWorld2D builds a SoftWorld with a pinned bending rope holding a tilted crate by a rigid pin, a compliant rope holding a wheel, a pressurized shape-matched soft body, a free particle cloud, a World ledge, a tilted static rigid ramp, and the golden tile grid.
func goldenSoftWorld2D() (*physics.SoftWorld, *physics.RigidWorld) {
	world := &physics.World{Tiles: goldenGrid()}
	world.Add(physics.NewBoxBody(150.3, 60.7, 30.1, 8.9))

	rw := &physics.RigidWorld{GravityY: 600}
	ramp := physics.NewStaticRigidBox(230.9, 120.3, 90.7, 10.1)
	ramp.Angle = -0.31
	crate := physics.NewRigidBox(142.3, 52.9, 20.3, 14.1, 4)
	crate.Angle = 0.3
	crate.AngularVel = 1.1
	wheel := physics.NewRigidCircle(60.7, 90.1, 7.3, 2)
	wheel.VX = 35.5
	rw.AddBody(ramp, crate, wheel)

	sw := physics.NewSoftWorld()
	sw.World = world
	sw.RigidWorld = rw
	sw.GravityX = 13.3
	sw.GravityY = 600
	sw.Damping = 0.013

	rope := sw.AddRope(physics.RopeConfig{
		StartX: 20.3, StartY: 15.7,
		EndX: 140.9, EndY: 45.1,
		Segments:          14,
		TotalMass:         0.7,
		ParticleRadius:    1.9,
		EnableBending:     true,
		BendingCompliance: 0.0003,
		PinStart:          true,
		Friction:          0.35,
	})
	sw.AttachRigidBody(rope[len(rope)-1], crate, core.Point{X: -5.3, Y: -6.1}, 0)

	hanger := sw.AddRope(physics.RopeConfig{
		StartX: 60.7, StartY: 20.3,
		EndX: 60.7, EndY: 80.9,
		Segments:       6,
		TotalMass:      0.3,
		ParticleRadius: 1.7,
		Compliance:     0.0007,
		PinStart:       true,
	})
	sw.AttachRigidBody(hanger[len(hanger)-1], wheel, core.Point{X: 0.7, Y: -6.3}, 0.0009)

	sw.AddSoftBody(physics.SoftBodyConfig{
		CenterX: 200.7, CenterY: 40.3,
		RadiusX: 18.3, RadiusY: 14.1,
		Segments:            14,
		TotalMass:           1.3,
		ParticleRadius:      2.3,
		EdgeCompliance:      0.0002,
		EnableBending:       true,
		BendingCompliance:   0.001,
		AreaCompliance:      0.0001,
		Pressure:            1.2,
		UseShapeMatching:    true,
		ShapeMatchStiffness: 0.4,
		Friction:            0.6,
		Restitution:         0.3,
	})

	for i := range 9 {
		sw.AddParticle(physics.Particle{
			X:           100.3 + float32(float32(i)*7.9),
			Y:           70.1 - float32(float32(i%3)*5.3),
			VX:          float32(i-4) * 11.7,
			VY:          -23.9,
			InvMass:     1.7,
			Radius:      2.1,
			Friction:    0.45,
			Restitution: 0.55,
		})
	}
	return sw, rw
}

func softWorldChecksum(sw *physics.SoftWorld, rw *physics.RigidWorld) uint64 {
	h := detmath.NewHasher()
	h.WriteInt(len(sw.Particles))
	for _, p := range sw.Particles {
		h.WriteFloat32(p.X)
		h.WriteFloat32(p.Y)
		h.WriteFloat32(p.PrevX)
		h.WriteFloat32(p.PrevY)
		h.WriteFloat32(p.VX)
		h.WriteFloat32(p.VY)
	}
	h.WriteUint64(rw.Checksum())
	return h.Sum64()
}

func TestGoldenTrajectorySoftWorld2D(t *testing.T) {
	const want uint64 = 0x2ec32113b43c7f69

	const dt = time.Second / 60
	sw, rw := goldenSoftWorld2D()
	for step := range 240 {
		if step%40 == 7 {
			sw.Particles[len(sw.Particles)-1-step%9].ApplyForce(-310.7, -950.3)
		}
		if step == 120 {
			rw.Bodies[1].ApplyImpulse(-90.3, -40.7)
		}
		rw.Step(dt)
		sw.Step(dt)
	}
	if got := softWorldChecksum(sw, rw); got != want {
		t.Fatalf("SoftWorld golden checksum = %#016x, want %#016x", got, want)
	}
}

// goldenCharacters returns a character that walks against the bare grid, slopes and a one-way moving platform, and one that moves through a World with a body, a ledge and a low-gravity area.
func goldenCharacters() (grid *physics.Grid, slopes []physics.Slope, platform *physics.MovingPlatform, walker, swimmer *physics.CharacterBody, world *physics.World) {
	grid = goldenGrid()
	slopes = []physics.Slope{
		{A: core.Point{X: 60.3, Y: 161.9}, B: core.Point{X: 101.7, Y: 139.3}},
		{A: core.Point{X: 101.7, Y: 139.3}, B: core.Point{X: 115.1, Y: 139.3}},
		{A: core.Point{X: 230.3, Y: 161.9}, B: core.Point{X: 262.9, Y: 120.7}},
	}
	platform = &physics.MovingPlatform{Body: physics.NewBoxBody(120.7, 95.3, 40.9, 6.1), OneWay: true}
	walker = physics.NewCharacterBody(physics.NewBoxBody(30.9, 120.3, 10.3, 15.7))
	walker.SnapDist = 6.3

	world = &physics.World{Tiles: goldenGrid(), GravityY: 700}
	world.Add(physics.NewBoxBody(180.3, 150.1, 20.7, 11.9))
	world.AddTrigger(&physics.Trigger{
		Shape: physics.BoxShape(physics.Box{X: 140.1, Y: 40.3, W: 70.9, H: 80.7}),
		Area: physics.AreaSettings{
			GravityMode:     physics.AreaOverrideAdd,
			GravityY:        -350.3,
			FluidLinearDrag: 0.9,
		},
	})
	swimmer = physics.NewCharacterBody(physics.NewCircleBody(90.1, 60.7, 6.7))
	swimmer.World = world
	swimmer.SetGravityScale(1.3)
	return grid, slopes, platform, walker, swimmer, world
}

func hashCharacter(h *detmath.Hasher, c *physics.CharacterBody, r physics.MoveResult) {
	hashBody(h, c.Body)
	h.WriteBool(c.Grounded)
	h.WriteFloat32(float32(c.GroundNormal.X))
	h.WriteFloat32(float32(c.GroundNormal.Y))
	h.WriteBool(c.Riding != nil)
	hashMoveResult(h, r)
}

func TestGoldenTrajectoryCharacter(t *testing.T) {
	const want uint64 = 0x52cdaff60882a819

	const dt = time.Second / 60
	dtSec := float32(dt.Seconds())
	grid, slopes, platform, walker, swimmer, world := goldenCharacters()
	platforms := []*physics.MovingPlatform{platform}
	walker.Body.VX = 97.3
	swimmer.Body.VX = 61.9
	h := detmath.NewHasher()
	for step := range 480 {
		if (step/90)%2 == 0 {
			platform.Move(0.73, 0.21)
		} else {
			platform.Move(-0.73, -0.21)
		}
		if walker.Body.OnRightWall {
			walker.Body.VX = -97.3
		} else if walker.Body.OnLeftWall {
			walker.Body.VX = 97.3
		}
		if walker.Grounded && step%37 == 0 {
			walker.Body.VY = -310.7
		}
		walker.Body.VY += float32(700 * dtSec)
		hashCharacter(&h, walker, walker.Move(grid, slopes, platforms, dt))

		if swimmer.Body.OnRightWall {
			swimmer.Body.VX = -61.9
		} else if swimmer.Body.OnLeftWall {
			swimmer.Body.VX = 61.9
		}
		if swimmer.Grounded && step%53 == 0 {
			swimmer.Body.VY = -420.1
		}
		hashCharacter(&h, swimmer, swimmer.MoveWorld(world, slopes, nil, dt))
	}
	if got := h.Sum64(); got != want {
		t.Fatalf("character golden checksum = %#016x, want %#016x", got, want)
	}
}

func TestGoldenTrajectoryTiles(t *testing.T) {
	const want uint64 = 0x875eaa18086183db

	const dt = time.Second / 60
	dtSec := float32(dt.Seconds())
	world := &physics.World{Tiles: goldenGrid(), GravityY: 640}
	bodies := []*physics.Body{
		physics.NewBoxBody(20.3, 30.7, 11.9, 13.3),
		physics.NewCircleBody(120.9, 40.1, 5.7),
		physics.NewBoxBody(250.7, 100.3, 9.1, 9.7),
		physics.NewBoxBody(180.1, 150.9, 7.7, 10.3),
	}
	speeds := []float32{143.7, -211.3, -97.1, 305.9}
	for i, b := range bodies {
		b.VX = speeds[i]
		b.VY = -150.3 * float32(i+1)
	}
	world.Add(bodies...)
	probe := physics.BoxShape(physics.Box{X: 80.3, Y: 90.7, W: 60.9, H: 30.1})

	h := detmath.NewHasher()
	for step := range 360 {
		for i, b := range bodies {
			if b.OnRightWall {
				b.VX = -speeds[i]
			} else if b.OnLeftWall {
				b.VX = speeds[i]
			}
			if b.Grounded && (step+i*11)%29 == 0 {
				b.VY = -380.9
			}
			b.VY += float32(world.GravityY * dtSec)
			hashMoveResult(&h, physics.MoveAndSlide(b, world.Tiles, dt))
			hashBody(&h, b)
		}
		probe.Box.X = 80.3 + float32(float32(step%60)*1.7)
		hits := world.QueryOverlapLayer(probe, physics.QueryAll, 0)
		h.WriteInt(len(hits))
		for _, hit := range hits {
			h.WriteBool(hit.Tile)
			h.WriteInt(hit.Col)
			h.WriteInt(hit.Row)
			h.WriteFloat32(hit.Contact.NormalX)
			h.WriteFloat32(hit.Contact.NormalY)
			h.WriteFloat32(hit.Contact.Depth)
		}
	}
	h.WriteUint64(world.Checksum())
	if got := h.Sum64(); got != want {
		t.Fatalf("tile golden checksum = %#016x, want %#016x", got, want)
	}
}

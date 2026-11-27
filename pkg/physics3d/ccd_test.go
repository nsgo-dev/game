package physics3d_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

func TestCCD3DSphereStopsAtThinStaticWall(t *testing.T) {
	const dt = time.Second / 60
	walls := []struct {
		name  string
		angle float32
		setup func(w *physics3d.World)
	}{
		{
			name:  "box",
			angle: 0,
			setup: func(w *physics3d.World) {
				// 2 cm thick wall centered at X=0 (near face at X=-0.01).
				w.AddBody(physics3d.NewStaticBody(
					physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
					physics3d.Vec3{},
				))
			},
		},
		{
			name:  "rotated_box",
			angle: 0.2,
			setup: func(w *physics3d.World) {
				wall := physics3d.NewStaticBody(
					physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
					physics3d.Vec3{},
				)
				wall.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, 0.2)
				w.AddBody(wall)
			},
		},
		{
			name:  "trimesh",
			angle: 0,
			setup: func(w *physics3d.World) {
				verts := []physics3d.Vec3{
					{X: 0, Y: -5, Z: -5},
					{X: 0, Y: 5, Z: -5},
					{X: 0, Y: 5, Z: 5},
					{X: 0, Y: -5, Z: 5},
				}
				w.AddTriMesh(physics3d.NewTriMeshCollider(verts, []uint32{0, 1, 2, 0, 2, 3}))
			},
		},
	}

	for _, wall := range walls {
		for _, speed := range []float32{400, 800, 2000} {
			t.Run(wall.name, func(t *testing.T) {
				w := physics3d.NewWorld()
				w.Gravity = physics3d.Vec3{}
				wall.setup(w)

				ball := w.AddBody(physics3d.NewDynamicBody(
					physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
					physics3d.Vec3{X: -1},
					1,
				))
				ball.LinearDamping = 0
				ball.Restitution = 0
				ball.Vel = physics3d.Vec3{X: speed}

				wallRot := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, wall.angle)
				invWallRot := wallRot.Conjugate()
				wallNormal := wallRot.RotateVec(physics3d.Vec3{X: 1})
				for i := range 30 {
					w.Step(dt)
					localPos := invWallRot.RotateVec(ball.Pos)
					if math.Abs(float64(localPos.Y)) <= 5 && math.Abs(float64(localPos.Z)) <= 5 && localPos.X > 0 {
						t.Fatalf("speed %v m/s: sphere tunneled past 2 cm static wall at step %d: pos=%+v local=%+v", speed, i, ball.Pos, localPos)
					}
				}
				if vn := ball.Vel.Dot(wallNormal); vn > 1 {
					t.Fatalf("speed %v m/s: sphere kept approach velocity %v into wall", speed, vn)
				}
			})
		}
	}
}

func TestCCD3DSphereBouncesOffThinStaticWall(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
		physics3d.Vec3{},
	))
	ball := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
		physics3d.Vec3{X: -1},
		1,
	))
	ball.LinearDamping = 0
	ball.Restitution = 1
	ball.Vel = physics3d.Vec3{X: 800}

	for range 3 {
		w.Step(dt)
	}
	if ball.Pos.X > -0.05 {
		t.Fatalf("elastic sphere at X=%v, expected rebound to -X side of wall", ball.Pos.X)
	}
	if ball.Vel.X > -790 {
		t.Fatalf("elastic sphere rebounded with Vel.X=%v, want near -800", ball.Vel.X)
	}
}

func TestCCD3DSpinningPlankDoesNotPassPillar(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}

	// Vertical static pillar placed in the sweep arc of a 2 m plank spinning around +Y.
	// With +Y angular velocity, the +X end of the plank sweeps toward -Z.
	pillar := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.04, Y: 1.0, Z: 0.04}}),
		physics3d.Vec3{X: 0.85, Y: 0, Z: -0.25},
	))
	_ = pillar

	plank := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1.0, Y: 0.05, Z: 0.05}}),
		physics3d.Vec3{},
		2.0,
	))
	plank.LinearDamping = 0
	plank.AngularDamping = 0
	plank.Restitution = 0.2
	plank.AngVel = physics3d.Vec3{Y: 30}

	// Track the +X end of the plank: it starts at (1, 0, 0) and rotates toward -Z.
	// If it tunneled past the pillar at (0.85, 0, -0.25), the +X end would cross Z < -0.35.
	for i := range 15 {
		w.Step(dt)
		tip := plank.Pos.Add(plank.Rot.RotateVec(physics3d.Vec3{X: 1.0}))
		if tip.Z < -0.35 {
			t.Fatalf("spinning 2 m plank at 30 rad/s passed through pillar at step %d: tip=%+v angVel=%+v", i, tip, plank.AngVel)
		}
	}
	if plank.AngVel.Y > 15 {
		t.Fatalf("expected pillar impact to arrest plank spin: AngVel=%+v", plank.AngVel)
	}
}

func TestCCD3DBulletStopsAtThinDynamicWall(t *testing.T) {
	const dt = time.Second / 60
	cases := []struct {
		name      string
		wallThick float32
		wallAngle float32
		wallVelX  float32
		bulletVY  float32
		speed     float32
		rest      float32
	}{
		{"10cm_400", 0.10, 0, 0, 0, 400, 0},
		{"10cm_800", 0.10, 0, 0, 0, 800, 0},
		{"2cm_2000", 0.02, 0, 0, 0, 2000, 0},
		{"elastic_800", 0.02, 0, 0, 0, 800, 1},
		{"rotated_800", 0.02, 0.3, 0, 0, 800, 0},
		{"oblique_800", 0.02, 0, 0, 120, 800, 0},
		{"moving_head_on_800", 0.04, 0, -50, 0, 800, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := physics3d.NewWorld()
			w.Gravity = physics3d.Vec3{}

			wall := w.AddBody(physics3d.NewDynamicBody(
				physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: tc.wallThick * 0.5, Y: 5, Z: 5}}),
				physics3d.Vec3{},
				10,
			))
			wall.LinearDamping = 0
			wall.AngularDamping = 0
			wall.Restitution = tc.rest
			wall.Vel = physics3d.Vec3{X: tc.wallVelX}
			if tc.wallAngle != 0 {
				wall.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, tc.wallAngle)
			}

			bullet := w.AddBody(physics3d.NewDynamicBody(
				physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
				physics3d.Vec3{X: -1},
				1,
			))
			bullet.Bullet = true
			bullet.LinearDamping = 0
			bullet.AngularDamping = 0
			bullet.Restitution = tc.rest
			bullet.Vel = physics3d.Vec3{X: tc.speed, Y: tc.bulletVY}

			gotEnter := false
			w.OnContact = func(ev physics3d.ContactEvent) {
				if ev.Kind == physics3d.ContactEnter && ((ev.BodyA == wall && ev.BodyB == bullet) || (ev.BodyA == bullet && ev.BodyB == wall)) {
					gotEnter = true
				}
			}

			for i := range 20 {
				w.Step(dt)
				// Check bullet center in wall's local frame (+X is through the wall, Y/Z span [-5, 5]).
				invRot := wall.Rot.Conjugate()
				localPos := invRot.RotateVec(bullet.Pos.Sub(wall.Pos))
				if math.Abs(float64(localPos.Y)) <= 5 && math.Abs(float64(localPos.Z)) <= 5 && localPos.X > 0 {
					t.Fatalf("bullet tunneled past dynamic wall at step %d: bullet=%+v wall=%+v local=%+v",
						i, bullet.Pos, wall.Pos, localPos)
				}
			}
			if !gotEnter {
				t.Fatal("expected ContactEnter event for 3D bullet vs dynamic wall")
			}
			if wall.Vel.X <= tc.wallVelX {
				t.Fatalf("expected bullet impact to transfer +X momentum to dynamic wall: wall.Vel.X=%v initial=%v", wall.Vel.X, tc.wallVelX)
			}
			if tc.rest == 1 && bullet.Vel.X >= 0 {
				t.Fatalf("expected elastic bullet to rebound: bullet.Vel.X=%v", bullet.Vel.X)
			}
		})
	}
}

func TestCCD3DNonBulletStillPassesThinDynamicWall(t *testing.T) {
	const dt = time.Second / 60
	for _, speed := range []float32{400, 800} {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		wall := w.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.05, Y: 5, Z: 5}}),
			physics3d.Vec3{},
			10,
		))
		wall.LinearDamping = 0

		sphere := w.AddBody(physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
			physics3d.Vec3{X: -1},
			1,
		))
		sphere.Bullet = false
		sphere.LinearDamping = 0
		sphere.Vel = physics3d.Vec3{X: speed}

		for range 5 {
			w.Step(dt)
		}
		if sphere.Pos.X <= wall.Pos.X+1 {
			t.Fatalf("speed %v m/s: non-bullet stopped at X=%v (wall X=%v); expected non-bullets to keep passing dynamic bodies",
				speed, sphere.Pos.X, wall.Pos.X)
		}
	}
}

func TestCCD3DSlidingOverTiledFloorSeams(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	for i := range 25 {
		tile := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 0.5, Z: 2}}),
			physics3d.Vec3{X: float32(i*2) - 2, Y: -0.5},
		))
		tile.Friction = 0
	}
	wall := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 2, Z: 2}}),
		physics3d.Vec3{X: 30, Y: 2},
	))
	_ = wall

	crate := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.25, Y: 0.25, Z: 0.25}}),
		physics3d.Vec3{X: 0, Y: 0.25},
		1,
	))
	crate.Friction = 0
	crate.LinearDamping = 0
	crate.Bullet = true

	for range 15 {
		w.Step(dt)
	}
	crate.Vel = physics3d.Vec3{X: 40}
	for i := range 60 {
		w.Step(dt)
		if crate.Pos.X > 30 {
			t.Fatalf("sliding crate tunneled through wall at step %d: X=%v", i, crate.Pos.X)
		}
	}
	// Must have slid across all tiles up to the wall at X=30 without catching on tile seams.
	if crate.Pos.X < 29 {
		t.Fatalf("sliding crate snagged on tiled floor seams at X=%v, want near wall X=29.74", crate.Pos.X)
	}
}

func TestCCD3DBulletDeterminismAndSnapshot(t *testing.T) {
	const dt = time.Second / 60
	build := func(bulletFlag bool) *physics3d.World {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}
		wall := w.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.02, Y: 4, Z: 4}}),
			physics3d.Vec3{X: 4},
			10,
		))
		wall.LinearDamping = 0
		wall.Vel = physics3d.Vec3{X: -10}
		shot := w.AddBody(physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
			physics3d.Vec3{X: -4},
			1,
		))
		shot.Bullet = bulletFlag
		shot.LinearDamping = 0
		shot.Restitution = 0.5
		shot.Vel = physics3d.Vec3{X: 250}
		return w
	}

	// Checksum must distinguish Bullet=true from Bullet=false.
	if build(true).Checksum() == build(false).Checksum() {
		t.Fatal("World.Checksum did not distinguish Body.Bullet")
	}

	w1 := build(true)
	w2 := build(true)
	// Step once (bullet is still in flight), save/JSON-roundtrip/restore w2, then run through impact.
	w1.Step(dt)
	w2.Step(dt)
	raw, err := json.Marshal(w2.Save())
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded physics3d.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Version != physics3d.SnapshotVersion {
		t.Fatalf("snapshot version = %d, want %d", decoded.Version, physics3d.SnapshotVersion)
	}
	wRestored := physics3d.NewWorld()
	if err := wRestored.Restore(decoded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !wRestored.Bodies()[1].Bullet {
		t.Fatal("Restore did not preserve Body.Bullet=true")
	}
	for range 15 {
		w1.Step(dt)
		wRestored.Step(dt)
	}
	if w1.Checksum() != wRestored.Checksum() {
		t.Fatalf("post-impact checksum mismatch after Restore: got %#x, want %#x", wRestored.Checksum(), w1.Checksum())
	}

	// Legacy v3 snapshot (without bullet field) still restores cleanly with Bullet=false.
	v3Snap := build(false).Save()
	v3Snap.Version = 3
	wLegacy := physics3d.NewWorld()
	if err := wLegacy.Restore(v3Snap); err != nil {
		t.Fatalf("Restore v3 snapshot: %v", err)
	}
	if wLegacy.Bodies()[1].Bullet {
		t.Fatal("v3 snapshot should restore with Bullet=false")
	}
}

func TestCCD3DHighSpeedNoNaNOrJitter(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
		physics3d.Vec3{X: 10},
	))
	ball := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
		physics3d.Vec3{X: -50},
		1,
	))
	ball.LinearDamping = 0
	ball.Bullet = true
	ball.Vel = physics3d.Vec3{X: 5000} // 83.3 m/step (> 800x thickness)

	for range 10 {
		w.Step(dt)
		if math.IsNaN(float64(ball.Pos.X)) || math.IsInf(float64(ball.Pos.X), 0) {
			t.Fatalf("non-finite position after high-speed CCD: %+v", ball.Pos)
		}
		if ball.Pos.X > 10 {
			t.Fatalf("5000 m/s bullet tunneled past thin wall: X=%v", ball.Pos.X)
		}
	}
}

func BenchmarkCCD3DFastBodyStaticWall(b *testing.B) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
		physics3d.Vec3{X: 5},
	))
	ball := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
		physics3d.Vec3{X: -1},
		1,
	))
	ball.LinearDamping = 0
	ball.Vel = physics3d.Vec3{X: 800}
	// Warm caches and scratch buffers.
	w.Step(dt)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ball.Pos = physics3d.Vec3{X: -1}
		ball.Vel = physics3d.Vec3{X: 800}
		ball.AngVel = physics3d.Vec3{}
		ball.Rot = physics3d.QuatIdentity()
		w.Step(dt)
	}
}

func BenchmarkCCD3DBulletDynamicWall(b *testing.B) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	wall := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.01, Y: 5, Z: 5}}),
		physics3d.Vec3{X: 5},
		10,
	))
	wall.LinearDamping = 0
	ball := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.05}),
		physics3d.Vec3{X: -1},
		1,
	))
	ball.Bullet = true
	ball.LinearDamping = 0
	ball.Vel = physics3d.Vec3{X: 800}
	// Warm caches and scratch buffers.
	w.Step(dt)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		wall.Pos = physics3d.Vec3{X: 5}
		wall.Vel = physics3d.Vec3{}
		wall.AngVel = physics3d.Vec3{}
		wall.Rot = physics3d.QuatIdentity()
		ball.Pos = physics3d.Vec3{X: -1}
		ball.Vel = physics3d.Vec3{X: 800}
		ball.AngVel = physics3d.Vec3{}
		ball.Rot = physics3d.QuatIdentity()
		w.Step(dt)
	}
}

package physics3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

func TestCharacterControllerMovingRotatingRisingPlatform10Seconds(t *testing.T) {
	run := func(t *testing.T, applyYaw bool) {
		t.Helper()
		w := physics3d.NewWorld()
		const (
			platHalfY = float32(0.5)
			linVX     = float32(1.4)
			linVY     = float32(0.8)
			linVZ     = float32(-0.6)
			omegaY    = float32(1.25) // rad/s (~2 full turns in 10 s)
		)
		plat := w.AddBody(physics3d.Body{
			Tag:       "platform",
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3.0, Y: platHalfY, Z: 3.0}}),
			Pos:       physics3d.Vec3{Y: -platHalfY},
			Kinematic: true,
		})

		// Stand off-center on the platform so rotation moves the character along
		// a circular arc around the platform pivot.
		initLocal := physics3d.Vec3{X: 1.5, Y: platHalfY, Z: 0.75}
		cc := physics3d.NewCharacterController(
			plat.Pos.Add(initLocal),
			0.3,
			1.8,
		)
		cc.ApplyPlatformYaw = applyYaw

		// Initial landing tick.
		res := cc.Move(w, physics3d.Vec3{}, step60)
		if !res.Grounded || res.GroundBody != plat {
			t.Fatalf("initial landing failed: grounded=%v body=%v", res.Grounded, res.GroundBody)
		}

		dtSec := float32(step60.Seconds())
		var totalYawDelta float32
		const totalSteps = 600 // 10 full seconds at 60 Hz
		for step := 1; step <= totalSteps; step++ {
			elapsed := float32(step) * dtSec
			plat.Pos = physics3d.Vec3{
				X: linVX * elapsed,
				Y: -platHalfY + linVY*elapsed,
				Z: linVZ * elapsed,
			}
			plat.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, omegaY*elapsed)
			w.Step(step60)

			res = cc.Move(w, physics3d.Vec3{}, step60)
			if !res.Grounded {
				t.Fatalf("step %d: detached from moving platform (pos=%v plat=%v)", step, res.Pos, plat.Pos)
			}
			if res.GroundBody != plat {
				t.Fatalf("step %d: GroundBody = %v, want platform", step, res.GroundBody)
			}
			if res.Crushed {
				t.Fatalf("step %d: unexpectedly reported Crushed on open platform", step)
			}

			// Verify character stayed locked to its initial local offset on the platform.
			curLocal := plat.Rot.Conjugate().RotateVec(res.Pos.Sub(plat.Pos))
			if !approxVec(curLocal, initLocal, 0.015) {
				t.Fatalf("step %d: drifted on platform: local=%v, want %v (world=%v plat=%v)",
					step, curLocal, initLocal, res.Pos, plat.Pos)
			}

			// Verify GroundVelocity matches linear + angular velocity at contact point.
			r := res.Pos.Sub(plat.Pos)
			wantVel := physics3d.Vec3{X: linVX, Y: linVY, Z: linVZ}.Add(
				physics3d.Vec3{Y: omegaY}.Cross(r),
			)
			if !approxVec(res.GroundVelocity, wantVel, 0.05) {
				t.Fatalf("step %d: GroundVelocity = %v, want ~%v", step, res.GroundVelocity, wantVel)
			}

			wantDYaw := omegaY * dtSec
			if !approx(res.GroundYawDelta, wantDYaw, 1e-4) {
				t.Fatalf("step %d: GroundYawDelta = %v, want %v", step, res.GroundYawDelta, wantDYaw)
			}
			totalYawDelta += res.GroundYawDelta
		}

		wantTotalYaw := omegaY * 10.0
		if !approx(totalYawDelta, wantTotalYaw, 0.02) {
			t.Fatalf("accumulated GroundYawDelta = %v, want %v", totalYawDelta, wantTotalYaw)
		}
		if applyYaw {
			if !approx(cc.Yaw, wantTotalYaw, 0.02) {
				t.Fatalf("cc.Yaw = %v, want %v", cc.Yaw, wantTotalYaw)
			}
		} else {
			if cc.Yaw != 0 {
				t.Fatalf("ApplyPlatformYaw=false should leave cc.Yaw at 0, got %v", cc.Yaw)
			}
		}
	}

	t.Run("ApplyPlatformYawTrue", func(t *testing.T) { run(t, true) })
	t.Run("ApplyPlatformYawFalse", func(t *testing.T) { run(t, false) })

	t.Run("FastDescendingElevatorStaysGrounded", func(t *testing.T) {
		w := physics3d.NewWorld()
		plat := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.5, Z: 2}}),
			Pos:       physics3d.Vec3{Y: 20},
			Kinematic: true,
		})
		cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 20.5}, 0.3, 1.8)
		if res := cc.Move(w, physics3d.Vec3{}, step60); !res.Grounded || res.GroundBody != plat {
			t.Fatalf("initial landing failed: %+v", res)
		}

		// Descend at -12 m/s (faster than 1-tick gravity acceleration).
		dtSec := float32(step60.Seconds())
		for step := 1; step <= 120; step++ {
			plat.Pos.Y = 20 - 12*float32(step)*dtSec
			res := cc.Move(w, physics3d.Vec3{}, step60)
			if !res.Grounded || res.GroundBody != plat {
				t.Fatalf("step %d: detached from fast-descending elevator (pos=%v platY=%v)", step, res.Pos, plat.Pos.Y)
			}
			if !approx(res.Pos.Y, plat.Pos.Y+0.5, 0.01) {
				t.Fatalf("step %d: feet Y = %v, want %v", step, res.Pos.Y, plat.Pos.Y+0.5)
			}
		}
	})
}

func TestCharacterControllerDynamicMovingPlatform(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}

	// Dynamic barge gliding and spinning freely in zero gravity.
	bargeBody := physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3, Y: 0.5, Z: 3}}),
		physics3d.Vec3{Y: -0.5},
		50.0,
	)
	bargeBody.LinearDamping = 0
	bargeBody.AngularDamping = 0
	bargeBody.Vel = physics3d.Vec3{X: 2.0, Z: 1.0}
	bargeBody.AngVel = physics3d.Vec3{Y: 0.8}
	barge := w.AddBody(bargeBody)

	// Re-enable gravity for the character via a custom gravity scale or world gravity?
	// Keep downward gravity in the world and set barge.GravityScale = 0!
	w.Gravity = physics3d.Vec3{Y: -9.81}
	barge.SetGravityScale(0)

	initLocal := physics3d.Vec3{X: 1.2, Y: 0.5, Z: -0.6}
	cc := physics3d.NewCharacterController(barge.Pos.Add(initLocal), 0.3, 1.8)
	if res := cc.Move(w, physics3d.Vec3{}, step60); !res.Grounded || res.GroundBody != barge {
		t.Fatalf("character did not land on dynamic barge: %+v", res)
	}

	for step := 1; step <= 180; step++ {
		w.Step(step60)
		res := cc.Move(w, physics3d.Vec3{}, step60)
		if !res.Grounded || res.GroundBody != barge {
			t.Fatalf("step %d: lost dynamic barge ground: grounded=%v body=%v", step, res.Grounded, res.GroundBody)
		}
		curLocal := barge.Rot.Conjugate().RotateVec(res.Pos.Sub(barge.Pos))
		if !approxVec(curLocal, initLocal, 0.02) {
			t.Fatalf("step %d: drifted on dynamic barge: local=%v, want %v", step, curLocal, initLocal)
		}
		r := res.Pos.Sub(barge.Pos)
		wantVel := barge.Vel.Add(barge.AngVel.Cross(r))
		if !approxVec(res.GroundVelocity, wantVel, 0.05) {
			t.Fatalf("step %d: GroundVelocity = %v, want %v", step, res.GroundVelocity, wantVel)
		}
	}
	if !approx(cc.Yaw, 0.8*3.0, 0.03) {
		t.Fatalf("cc.Yaw after 3s on dynamic rotating barge = %v, want ~2.4", cc.Yaw)
	}
}

func TestCharacterControllerPlatformOnLeaveModes(t *testing.T) {
	runJump := func(mode physics3d.PlatformOnLeave, platVel physics3d.Vec3) (jumpPosAfterHalfSec physics3d.Vec3, peakY float32, velAfterJump physics3d.Vec3) {
		w := physics3d.NewWorld()
		plat := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3, Y: 0.5, Z: 3}}),
			Pos:       physics3d.Vec3{Y: -0.5},
			Kinematic: true,
		})
		cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
		cc.PlatformOnLeave = mode

		dtSec := float32(step60.Seconds())
		// Ride for 10 steps to establish platform velocity.
		for i := 0; i < 10; i++ {
			if i > 0 {
				plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
			}
			cc.Move(w, physics3d.Vec3{}, step60)
		}

		// Jump upward on step 10.
		plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
		startX := cc.Pos.X
		res := cc.Move(w, physics3d.Vec3{Y: 6.0}, step60)
		velAfterJump = cc.Vel
		peakY = res.Pos.Y

		// Coast airborne for 29 more steps (0.5 s total) with zero wishMove.
		for i := 0; i < 29; i++ {
			plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
			res = cc.Move(w, physics3d.Vec3{}, step60)
			if res.Pos.Y > peakY {
				peakY = res.Pos.Y
			}
		}
		return physics3d.Vec3{X: cc.Pos.X - startX, Y: cc.Pos.Y, Z: cc.Pos.Z}, peakY, velAfterJump
	}

	t.Run("JumpHorizontalAndRisingPlatform", func(t *testing.T) {
		platVel := physics3d.Vec3{X: 4.0, Y: 2.0}
		dispAdd, peakAdd, velAdd := runJump(physics3d.PlatformLeaveAddVelocity, platVel)
		dispUp, peakUp, _ := runJump(physics3d.PlatformLeaveAddUpwardVelocity, platVel)
		dispNone, peakNone, velNone := runJump(physics3d.PlatformLeaveDoNothing, platVel)

		// AddVelocity keeps both horizontal (+4 m/s * 0.5 s = +2.0 m) and upward (+2 m/s) boost.
		if !approx(dispAdd.X, 2.0, 0.08) {
			t.Fatalf("AddVelocity horizontal travel = %v, want ~2.0 (vel=%v)", dispAdd.X, velAdd)
		}
		// AddUpwardVelocity matches AddVelocity when platform moves upward.
		if !approx(dispUp.X, dispAdd.X, 1e-4) || !approx(peakUp, peakAdd, 1e-4) {
			t.Fatalf("AddUpwardVelocity (%v, peak %v) != AddVelocity (%v, peak %v) on rising platform",
				dispUp, peakUp, dispAdd, peakAdd)
		}
		// DoNothing drops horizontal and vertical platform velocity.
		if !approx(dispNone.X, 0, 0.05) {
			t.Fatalf("DoNothing horizontal travel = %v, want ~0 (vel=%v)", dispNone.X, velNone)
		}
		if peakAdd <= peakNone+0.5 {
			t.Fatalf("AddVelocity peakY (%v) should be significantly higher than DoNothing (%v)", peakAdd, peakNone)
		}
	})

	t.Run("JumpDescendingPlatformClampsDownwardOnlyInAddUpwardMode", func(t *testing.T) {
		platVel := physics3d.Vec3{X: 4.0, Y: -3.0}
		dispAdd, peakAdd, _ := runJump(physics3d.PlatformLeaveAddVelocity, platVel)
		dispUp, peakUp, _ := runJump(physics3d.PlatformLeaveAddUpwardVelocity, platVel)
		dispNone, peakNone, _ := runJump(physics3d.PlatformLeaveDoNothing, platVel)

		// AddVelocity keeps horizontal (+2.0 m) AND downward (-3 m/s), reducing jump height.
		if !approx(dispAdd.X, 2.0, 0.08) {
			t.Fatalf("AddVelocity horizontal travel = %v, want ~2.0", dispAdd.X)
		}
		// AddUpwardVelocity keeps horizontal (+2.0 m) but ignores downward (-3 m/s), matching DoNothing's peakY!
		if !approx(dispUp.X, 2.0, 0.08) {
			t.Fatalf("AddUpwardVelocity horizontal travel = %v, want ~2.0", dispUp.X)
		}
		if peakUp <= peakAdd+0.5 {
			t.Fatalf("AddUpwardVelocity peakY (%v) should exceed AddVelocity (%v) on descending platform", peakUp, peakAdd)
		}
		if !approx(peakUp, peakNone, 0.05) {
			t.Fatalf("AddUpwardVelocity peakY (%v) should match DoNothing (%v) on descending platform", peakUp, peakNone)
		}
		if !approx(dispNone.X, 0, 0.05) {
			t.Fatalf("DoNothing horizontal travel = %v, want ~0", dispNone.X)
		}
	})

	t.Run("WalkOffMovingPlatform", func(t *testing.T) {
		runWalkOff := func(mode physics3d.PlatformOnLeave) (float32, float32) {
			w := physics3d.NewWorld()
			plat := w.AddBody(physics3d.Body{
				Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1.0, Y: 0.5, Z: 1.0}}),
				Pos:       physics3d.Vec3{Y: -0.5},
				Kinematic: true,
			})
			cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0.7, Y: 0}, 0.3, 1.8)
			cc.PlatformOnLeave = mode
			dtSec := float32(step60.Seconds())
			platVel := physics3d.Vec3{Z: 5.0}

			// Establish ground on moving platform.
			for i := 0; i < 4; i++ {
				if i > 0 {
					plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
				}
				cc.Move(w, physics3d.Vec3{}, step60)
			}

			// Walk off the +X edge while platform moves along +Z.
			leftZ := float32(0)
			left := false
			for i := 0; i < 40; i++ {
				plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
				wish := physics3d.Vec3{}
				if !left {
					wish.X = 3.0
				}
				res := cc.Move(w, wish, step60)
				if !left && !res.Grounded {
					left = true
					leftZ = res.Pos.Z
				}
			}
			if !left {
				t.Fatalf("mode %v: character never walked off platform", mode)
			}
			return cc.Pos.Z - leftZ, cc.Vel.Z
		}

		dzAdd, vzAdd := runWalkOff(physics3d.PlatformLeaveAddVelocity)
		dzNone, vzNone := runWalkOff(physics3d.PlatformLeaveDoNothing)

		if dzAdd < 1.5 || !approx(vzAdd, 5.0, 0.2) {
			t.Fatalf("AddVelocity walk-off failed: dz=%v vz=%v, want dz>1.5 vz~5.0", dzAdd, vzAdd)
		}
		if math.Abs(float64(dzNone)) > 0.1 || math.Abs(float64(vzNone)) > 0.1 {
			t.Fatalf("DoNothing walk-off kept velocity: dz=%v vz=%v, want ~0", dzNone, vzNone)
		}
	})

	t.Run("JumpViaDirectVelYMatchesWishMoveY", func(t *testing.T) {
		platVel := physics3d.Vec3{X: 4.0, Y: 2.0}
		for _, mode := range []physics3d.PlatformOnLeave{
			physics3d.PlatformLeaveAddVelocity,
			physics3d.PlatformLeaveAddUpwardVelocity,
			physics3d.PlatformLeaveDoNothing,
		} {
			dispWish, peakWish, velWish := runJump(mode, platVel)

			w := physics3d.NewWorld()
			plat := w.AddBody(physics3d.Body{
				Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3, Y: 0.5, Z: 3}}),
				Pos:       physics3d.Vec3{Y: -0.5},
				Kinematic: true,
			})
			cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
			cc.PlatformOnLeave = mode

			dtSec := float32(step60.Seconds())
			for i := 0; i < 10; i++ {
				if i > 0 {
					plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
				}
				cc.Move(w, physics3d.Vec3{}, step60)
			}

			// Jump by setting cc.Vel.Y directly with wishMove.Y == 0.
			plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
			startX := cc.Pos.X
			cc.Vel.Y = 6.0
			res := cc.Move(w, physics3d.Vec3{}, step60)
			velDirect := cc.Vel
			peakDirect := res.Pos.Y
			for i := 0; i < 29; i++ {
				plat.Pos = plat.Pos.Add(platVel.Scale(dtSec))
				res = cc.Move(w, physics3d.Vec3{}, step60)
				if res.Pos.Y > peakDirect {
					peakDirect = res.Pos.Y
				}
			}
			dispDirect := physics3d.Vec3{X: cc.Pos.X - startX, Y: cc.Pos.Y, Z: cc.Pos.Z}

			if dispDirect != dispWish || peakDirect != peakWish || velDirect != velWish {
				t.Fatalf("mode %v: direct Vel.Y jump diverged from wishMove.Y:\ndirect: disp=%v peak=%v vel=%v\nwish:   disp=%v peak=%v vel=%v",
					mode, dispDirect, peakDirect, velDirect, dispWish, peakWish, velWish)
			}
		}
	})
}

func TestCharacterControllerKinematicWallPush(t *testing.T) {
	t.Run("SlidingWallPushesStationaryAndResistingCharacter", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -10, Y: -1, Z: -5}, physics3d.Vec3{X: 20, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		// Thin kinematic sliding door (thickness 0.16 m) moving at 6 m/s (0.1 m/step).
		doorHalfX := float32(0.08)
		door := w.AddBody(physics3d.Body{
			Tag:       "door",
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: doorHalfX, Y: 1.5, Z: 3.0}}),
			Pos:       physics3d.Vec3{X: -1.5, Y: 1.5, Z: 0},
			Kinematic: true,
		})

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		// Initial tick records kinematic pose and grounds character.
		cc.Move(w, physics3d.Vec3{}, step60)

		dtSec := float32(step60.Seconds())
		sawWall := false
		for step := 1; step <= 60; step++ {
			door.Pos.X += 6.0 * dtSec
			// For the second half, character resists by walking back toward the door.
			wish := physics3d.Vec3{}
			if step > 30 {
				wish.X = -2.0
			}
			res := cc.Move(w, wish, step60)
			if res.OnWall {
				sawWall = true
			}
			if res.Crushed {
				t.Fatalf("step %d: open-floor sliding door should not set Crushed", step)
			}
			// Character must stay ahead of the door's front (+X) face without tunneling through.
			doorFront := door.Pos.X + doorHalfX
			if cc.Pos.X < doorFront+cc.Radius-0.01 {
				t.Fatalf("step %d: door passed through character: charX=%v doorFront=%v (want >= %v)",
					step, cc.Pos.X, doorFront, doorFront+cc.Radius)
			}
			if ct := physics3d.TestShapes(cc.Shape(), door.WorldShape()); ct.Hit && ct.Depth > 2e-3 {
				t.Fatalf("step %d: capsule penetrated sliding door by %v", step, ct.Depth)
			}
		}
		if !sawWall {
			t.Fatal("expected OnWall while pushed by sliding kinematic door")
		}
		if !cc.Grounded {
			t.Fatalf("character should stay grounded on floor while pushed: pos=%v", cc.Pos)
		}
	})

	t.Run("SlidingWallDeflectsAlongAngledStaticWallWithoutCrush", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -10, Y: -1, Z: -10}, physics3d.Vec3{X: 10, Y: 0, Z: 10})),
			physics3d.Vec3{},
		))
		// 45-degree static guide wall deflecting +X motion toward +Z.
		guide := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 2, Z: 5}}),
			physics3d.Vec3{X: 1.5, Y: 2, Z: 0},
		)
		guide.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, math.Pi/4)
		w.AddBody(guide)

		pusher := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 4}}),
			Pos:       physics3d.Vec3{X: -1.5, Y: 2, Z: 0},
			Kinematic: true,
		})
		cc := physics3d.NewCharacterController(physics3d.Vec3{}, 0.3, 1.8)
		cc.Move(w, physics3d.Vec3{}, step60)

		dtSec := float32(step60.Seconds())
		for step := 1; step <= 60; step++ {
			pusher.Pos.X += 2.0 * dtSec
			res := cc.Move(w, physics3d.Vec3{}, step60)
			if res.Crushed {
				t.Fatalf("step %d: deflecting 45-degree wall falsely triggered Crushed (pos=%v)", step, res.Pos)
			}
		}
		if cc.Pos.Z < 0.3 {
			t.Fatalf("expected angled guide wall to deflect character in +Z, got pos=%v", cc.Pos)
		}
	})
}

func TestCharacterControllerCrusher(t *testing.T) {
	t.Run("HorizontalOpposingKinematicWalls", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -10, Y: -1, Z: -5}, physics3d.Vec3{X: 10, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		leftWall := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 3}}),
			Pos:       physics3d.Vec3{X: -1.5, Y: 2, Z: 0},
			Kinematic: true,
		})
		rightWall := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 2, Z: 3}}),
			Pos:       physics3d.Vec3{X: 1.5, Y: 2, Z: 0},
			Kinematic: true,
		})

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		if res := cc.Move(w, physics3d.Vec3{}, step60); res.Crushed {
			t.Fatal("should not be crushed with wide gap")
		}

		dtSec := float32(step60.Seconds())
		crushedAtStep := -1
		for step := 1; step <= 60; step++ {
			leftWall.Pos.X += 1.5 * dtSec
			rightWall.Pos.X -= 1.5 * dtSec
			res := cc.Move(w, physics3d.Vec3{}, step60)
			gap := (rightWall.Pos.X - 0.5) - (leftWall.Pos.X + 0.5)
			if gap > 2*cc.Radius+0.02 && res.Crushed {
				t.Fatalf("step %d: crushed prematurely at gap=%v (diameter=%v)", step, gap, 2*cc.Radius)
			}
			if res.Crushed && crushedAtStep < 0 {
				crushedAtStep = step
			}
		}
		if crushedAtStep < 0 {
			t.Fatal("expected Crushed=true when two opposing kinematic walls close past capsule diameter")
		}

		// Re-open the crusher: Crushed must clear once gap exceeds capsule diameter.
		leftWall.Pos.X = -1.5
		rightWall.Pos.X = 1.5
		res := cc.Move(w, physics3d.Vec3{}, step60)
		if res.Crushed || cc.Crushed {
			t.Fatal("expected Crushed=false after crusher walls retract")
		}
	})

	t.Run("VerticalRisingElevatorIntoStaticCeiling", func(t *testing.T) {
		w := physics3d.NewWorld()
		// Static ceiling with bottom face at Y = 2.4.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: 2.4, Z: -5}, physics3d.Vec3{X: 5, Y: 3.4, Z: 5})),
			physics3d.Vec3{},
		))
		lift := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.5, Z: 2}}),
			Pos:       physics3d.Vec3{Y: -0.5},
			Kinematic: true,
		})

		cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
		cc.Move(w, physics3d.Vec3{}, step60)

		dtSec := float32(step60.Seconds())
		crushed := false
		for step := 1; step <= 60; step++ {
			lift.Pos.Y += 1.5 * dtSec
			res := cc.Move(w, physics3d.Vec3{}, step60)
			if res.Crushed {
				crushed = true
				break
			}
		}
		if !crushed {
			t.Fatalf("expected Crushed=true when rising elevator squeezes character into ceiling (pos=%v liftTop=%v)", cc.Pos, lift.Pos.Y+0.5)
		}
	})

	t.Run("VerticalDescendingCeilingOntoStaticFloor", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 5, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		press := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.5, Z: 2}}),
			Pos:       physics3d.Vec3{Y: 2.7}, // bottom at 2.2
			Kinematic: true,
		})

		cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
		cc.Move(w, physics3d.Vec3{}, step60)

		dtSec := float32(step60.Seconds())
		crushed := false
		for step := 1; step <= 60; step++ {
			press.Pos.Y -= 1.5 * dtSec
			res := cc.Move(w, physics3d.Vec3{}, step60)
			if res.Crushed {
				crushed = true
				break
			}
		}
		if !crushed {
			t.Fatalf("expected Crushed=true when descending ceiling squeezes character onto floor (pos=%v pressBottom=%v)", cc.Pos, press.Pos.Y-0.5)
		}
	})
}

func TestCharacterControllerMovingPlatformDeterminism(t *testing.T) {
	type stepRecord struct {
		res          physics3d.MoveResult
		vel          physics3d.Vec3
		yaw          float32
		groundBodyID int
	}

	simulate := func() []stepRecord {
		w := physics3d.NewWorld()
		plat := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 3, Y: 0.5, Z: 3}}),
			Pos:       physics3d.Vec3{Y: -0.5},
			Kinematic: true,
		})
		wall := w.AddBody(physics3d.Body{
			Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 2.0, Z: 3.0}}),
			Pos:       physics3d.Vec3{X: -3.0, Y: 2.0, Z: 0},
			Kinematic: true,
		})
		stopWall := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 2.0, Z: 3.0}}),
			physics3d.Vec3{X: 5.5, Y: 2.0, Z: 0},
		))
		_ = stopWall

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 1.0, Y: 0, Z: 0.5}, 0.3, 1.8)
		dtSec := float32(step60.Seconds())

		out := make([]stepRecord, 0, 180)
		for step := 0; step < 180; step++ {
			elapsed := float32(step) * dtSec
			plat.Pos = physics3d.Vec3{X: 0.8 * elapsed, Y: -0.5 + 0.3*elapsed, Z: 0.2 * elapsed}
			plat.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, 0.9*elapsed)
			if step >= 90 {
				wall.Pos.X += 3.0 * dtSec
			}
			w.Step(step60)

			wish := physics3d.Vec3{}
			if step == 60 {
				wish.Y = 5.0
			} else if step >= 20 && step < 40 {
				wish.X = 1.0
			}
			res := cc.Move(w, wish, step60)
			gid := 0
			if res.GroundBody != nil {
				gid = res.GroundBody.ID
			}
			resCopy := res
			resCopy.GroundBody = nil // compare via stable ID
			out = append(out, stepRecord{
				res:          resCopy,
				vel:          cc.Vel,
				yaw:          cc.Yaw,
				groundBodyID: gid,
			})
		}
		return out
	}

	run1 := simulate()
	run2 := simulate()
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("step %d diverged:\nrun1=%+v\nrun2=%+v", i, run1[i], run2[i])
		}
	}
}

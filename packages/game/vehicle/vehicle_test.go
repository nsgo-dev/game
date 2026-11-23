package vehicle_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/vehicle"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func approx(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

// flatWorld returns a world with a large flat ground whose top is at y = 0, and the reference car resting on it at the origin facing -Z.
func flatWorld(t testing.TB) (*physics3d.World, *vehicle.Vehicle) {
	t.Helper()
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(box(2000, 1, 2000), physics3d.Vec3{Y: -1}))
	car, err := vehicle.New(w, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.65}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatal(err)
	}
	return w, car
}

func drive(w *physics3d.World, car *vehicle.Vehicle, in vehicle.Input, steps int) {
	for range steps {
		car.SetInput(in)
		car.Step(w, dt)
		w.Step(dt)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	cfg := vehicle.ReferenceConfig()
	cfg.Mass = 0
	if _, err := vehicle.New(physics3d.NewWorld(), cfg, physics3d.Vec3{}, physics3d.QuatIdentity()); err == nil {
		t.Fatal("New accepted a zero mass")
	}
}

func TestNewCopiesWheels(t *testing.T) {
	cfg := vehicle.ReferenceConfig()
	car, err := vehicle.New(physics3d.NewWorld(), cfg, physics3d.Vec3{}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Wheels[0].Radius = 99
	if car.Config.Wheels[0].Radius == 99 {
		t.Fatal("Vehicle.Config shares the caller's Wheels slice")
	}
}

func TestSuspensionSettlesAtStaticLoadAndSleeps(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{}, 4*rate)

	cfg := car.Config
	// Each of the four springs carries a quarter of the weight: compression = m g / (4 k).
	wantComp := cfg.Mass * 9.81 / 4 / cfg.Wheels[0].Stiffness
	for _, wh := range car.Wheels {
		if !wh.Contact {
			t.Fatalf("wheel %s has no contact at rest", wh.Config.Name)
		}
		if comp := wh.Config.RestLength - wh.SuspensionLength; !approx(comp, wantComp, 0.01) {
			t.Fatalf("wheel %s compression = %v, want about %v", wh.Config.Name, comp, wantComp)
		}
		if wh.Sliding {
			t.Fatalf("wheel %s slides at rest", wh.Config.Name)
		}
	}
	// Center of mass height: ground + radius + suspension length - anchor offset.
	wantY := cfg.Wheels[0].Radius + cfg.Wheels[0].RestLength - wantComp - cfg.Wheels[0].Anchor[1]
	if !approx(car.Body.Pos.Y, wantY, 0.01) {
		t.Fatalf("ride height = %v, want about %v", car.Body.Pos.Y, wantY)
	}
	if !car.Body.Sleeping {
		t.Fatalf("settled car is awake: vel %+v angvel %+v", car.Body.Vel, car.Body.AngVel)
	}
	if car.Grounded() != 4 {
		t.Fatalf("Grounded = %d, want 4", car.Grounded())
	}

	car.SetInput(vehicle.Input{Throttle: 1})
	if car.Body.Sleeping {
		t.Fatal("SetInput with throttle did not wake the car")
	}
	drive(w, car, vehicle.Input{Throttle: 1}, rate)
	if car.Speed() <= 1 {
		t.Fatalf("speed after 1 s of throttle = %v", car.Speed())
	}
}

func TestAccelerateBrakeAndReverse(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{}, rate/2)

	drive(w, car, vehicle.Input{Throttle: 1}, 3*rate)
	fast := car.Speed()
	// About 0.5 g at standstill, fading with speed: 3 s should give 9-15 m/s.
	if fast < 9 || fast > 15 {
		t.Fatalf("speed after 3 s full throttle = %v, want 9-15 m/s", fast)
	}
	if car.Body.Pos.Z >= 0 {
		t.Fatalf("car drove toward +Z (z = %v); forward is -Z", car.Body.Pos.Z)
	}
	if x := car.Body.Pos.X; !approx(x, 0, 1e-3) {
		t.Fatalf("car drifted sideways to x = %v driving straight", x)
	}
	for _, wh := range car.Wheels {
		if wh.SpinSpeed <= 0 {
			t.Fatalf("wheel %s spin speed = %v while rolling forward", wh.Config.Name, wh.SpinSpeed)
		}
	}

	steps := 0
	for car.Speed() > 0.05 && steps < 5*rate {
		drive(w, car, vehicle.Input{Brake: 1}, 1)
		steps++
	}
	// 1 g of braking stops 9-15 m/s in about 1-1.5 s.
	if steps > 2*rate {
		t.Fatalf("braking from %v m/s took %d steps", fast, steps)
	}
	if s := car.Speed(); s < -0.05 {
		t.Fatalf("brakes reversed the car: speed %v", s)
	}

	drive(w, car, vehicle.Input{Throttle: -1}, 6*rate)
	if s := car.Speed(); s >= 0 || s < -car.Config.MaxReverseSpeed-0.01 {
		t.Fatalf("reverse speed = %v, want in [-%v, 0)", s, car.Config.MaxReverseSpeed)
	}
}

func TestTopSpeedIsCapped(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Throttle: 1}, 60*rate)
	if s := car.Speed(); s <= 25 || s > car.Config.MaxSpeed {
		t.Fatalf("speed after a minute = %v, want in (25, %v]", s, car.Config.MaxSpeed)
	}
}

func TestSteerRightTurnsRight(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Throttle: 0.5}, rate)
	drive(w, car, vehicle.Input{Throttle: 0.3, Steer: 1}, rate)

	if a := car.Wheels[0].SteerAngle; !approx(a, -float32(30*math.Pi/180), 1e-4) {
		t.Fatalf("front steer angle = %v, want -30 degrees (right)", a)
	}
	if a := car.Wheels[2].SteerAngle; a != 0 {
		t.Fatalf("rear wheel steered to %v", a)
	}
	if f := car.Forward(); f.X <= 0.2 {
		t.Fatalf("forward after steering right = %+v, want a +X component", f)
	}
	if car.Body.AngVel.Y >= 0 {
		t.Fatalf("yaw rate = %v, want negative (clockwise from above) turning right", car.Body.AngVel.Y)
	}
	if up := car.Up(); up.Y < 0.95 {
		t.Fatalf("car rolled over in a gentle turn: up %+v", up)
	}
}

func TestSteeringEasesAtSteerRate(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Steer: -1}, 1)
	want := float32(car.Config.SteerRate*math.Pi/180) / rate
	if a := car.Wheels[0].SteerAngle; !approx(a, want, 1e-6) {
		t.Fatalf("steer angle after one step = %v, want %v", a, want)
	}
}

func TestHandbrakeLocksRearWheels(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Throttle: 1}, 2*rate)
	drive(w, car, vehicle.Input{Handbrake: true}, 1)
	for _, wh := range car.Wheels {
		locked := wh.Config.Handbrake
		if locked && wh.SpinSpeed != 0 {
			t.Fatalf("handbrake wheel %s spins at %v", wh.Config.Name, wh.SpinSpeed)
		}
		if !locked && wh.SpinSpeed <= 0 {
			t.Fatalf("free wheel %s stopped spinning", wh.Config.Name)
		}
	}
	if !car.Wheels[2].Sliding {
		t.Fatal("a locked rear wheel at speed is not sliding")
	}
	drive(w, car, vehicle.Input{Handbrake: true}, 4*rate)
	if s := car.Speed(); math.Abs(float64(s)) > 0.1 {
		t.Fatalf("speed after 4 s of handbrake = %v", s)
	}
}

func TestAirborneCarFallsWithoutContact(t *testing.T) {
	w := physics3d.NewWorld()
	car, err := vehicle.New(w, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 10}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatal(err)
	}
	drive(w, car, vehicle.Input{Throttle: 1}, rate/2)
	if car.Grounded() != 0 {
		t.Fatalf("Grounded = %d in mid-air", car.Grounded())
	}
	for _, wh := range car.Wheels {
		if wh.SuspensionLength != wh.Config.RestLength || wh.SuspensionForce != 0 {
			t.Fatalf("airborne wheel %s: length %v force %v", wh.Config.Name, wh.SuspensionLength, wh.SuspensionForce)
		}
	}
	if car.Body.Pos.Y >= 10 || car.Body.Vel.Y >= 0 {
		t.Fatalf("car did not fall: %+v", car.Body.Pos)
	}
	if v := car.Body.Vel; v.X != 0 || v.Z != 0 {
		t.Fatalf("throttle pushed an airborne car: vel %+v", v)
	}
}

func TestWheelRaysSkipTriggersAndChassis(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{}, 2*rate)
	want := car.Body.Pos.Y

	w2, car2 := flatWorld(t)
	vehicle.AddCheckpoint(w2, box(5, 5, 5), physics3d.Vec3{})
	drive(w2, car2, vehicle.Input{}, 2*rate)
	if car2.Body.Pos.Y != want {
		t.Fatalf("ride height inside a trigger = %v, want %v", car2.Body.Pos.Y, want)
	}
	if car2.Wheels[0].Ground == nil || car2.Wheels[0].Ground.IsTrigger {
		t.Fatalf("wheel ground = %+v, want the static ground", car2.Wheels[0].Ground)
	}
}

func TestRayMaskFiltersGround(t *testing.T) {
	w := physics3d.NewWorld()
	ground := physics3d.NewStaticBody(box(50, 1, 50), physics3d.Vec3{Y: -1})
	ground.Layer = physics3d.LayerBit(3)
	w.AddBody(ground)
	car, err := vehicle.New(w, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.65}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatal(err)
	}
	car.RayMask = physics3d.LayerBit(1)
	drive(w, car, vehicle.Input{}, 1)
	if car.Grounded() != 0 {
		t.Fatal("wheel rays hit a ground outside RayMask")
	}
	car.RayMask = physics3d.LayerBit(3)
	drive(w, car, vehicle.Input{}, 1)
	if car.Grounded() != 4 {
		t.Fatalf("Grounded = %d with the ground's layer in RayMask", car.Grounded())
	}
}

func TestSuspensionPushesDynamicGround(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(box(50, 1, 50), physics3d.Vec3{Y: -1}))
	plate := w.AddBody(physics3d.NewDynamicBody(box(3, 0.1, 3), physics3d.Vec3{Y: 0.1}, 50))
	plate.LockRotation = true
	plate.InvInertia = 0
	car, err := vehicle.New(w, vehicle.ReferenceConfig(), physics3d.Vec3{Y: 0.85}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatal(err)
	}
	car.Step(w, dt)
	if car.Wheels[0].Ground != plate {
		t.Fatalf("wheel ground = %v, want the plate", car.Wheels[0].Ground)
	}
	if plate.Vel.Y >= 0 {
		t.Fatalf("plate velocity after the car's suspension pushed it = %+v, want downward", plate.Vel)
	}
}

func TestInputIsClamped(t *testing.T) {
	_, car := flatWorld(t)
	nan := float32(math.NaN())
	car.SetInput(vehicle.Input{Throttle: 3, Brake: -2, Steer: nan})
	if got := car.Input(); got != (vehicle.Input{Throttle: 1}) {
		t.Fatalf("Input = %+v, want throttle 1 and the rest 0", got)
	}
	car.SetInput(vehicle.Input{Throttle: -3, Brake: 4, Steer: -9})
	if got := car.Input(); got != (vehicle.Input{Throttle: -1, Brake: 1, Steer: -1}) {
		t.Fatalf("Input = %+v", got)
	}
}

func TestStepIgnoresNonPositiveDt(t *testing.T) {
	w, car := flatWorld(t)
	before := *car.Body
	car.SetInput(vehicle.Input{Throttle: 1})
	car.Step(w, 0)
	car.Step(w, -dt)
	if car.Body.Vel != before.Vel || car.Body.AngVel != before.AngVel {
		t.Fatal("Step with dt <= 0 changed the chassis")
	}
}

func TestResetStopsAndMovesCar(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Throttle: 1, Steer: 1}, 2*rate)
	rot := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, math.Pi/2)
	car.Reset(physics3d.Vec3{X: 5, Y: 0.65, Z: 5}, rot)
	if car.Body.Vel != (physics3d.Vec3{}) || car.Body.AngVel != (physics3d.Vec3{}) {
		t.Fatal("Reset left the chassis moving")
	}
	if car.Input() != (vehicle.Input{}) {
		t.Fatal("Reset kept the input")
	}
	for _, wh := range car.Wheels {
		if wh.Contact || wh.SteerAngle != 0 || wh.SpinSpeed != 0 || wh.SuspensionLength != wh.Config.RestLength {
			t.Fatalf("Reset left wheel state %+v", wh)
		}
	}
	// Facing -X after a quarter turn left.
	if f := car.Forward(); !approx(f.X, -1, 1e-5) {
		t.Fatalf("forward after Reset = %+v", f)
	}
	drive(w, car, vehicle.Input{}, 1)
	if car.Wheels[0].SteerAngle != 0 {
		t.Fatal("steering was not centered by Reset")
	}
}

func TestWheelTransform(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{}, rate)
	wh := car.Wheels[0]
	pos, rot := car.WheelTransform(0)
	anchor := car.Body.Pos.Add(car.Body.Rot.RotateVec(physics3d.Vec3{X: wh.Config.Anchor[0], Y: wh.Config.Anchor[1], Z: wh.Config.Anchor[2]}))
	if !approx(pos.X, anchor.X, 1e-5) || !approx(pos.Z, anchor.Z, 1e-5) || !approx(pos.Y, anchor.Y-wh.SuspensionLength, 1e-3) {
		t.Fatalf("wheel center = %+v, want %v below anchor %+v", pos, wh.SuspensionLength, anchor)
	}
	// The wheel's bottom touches the ground.
	if !approx(pos.Y-wh.Config.Radius, 0, 2e-3) {
		t.Fatalf("wheel bottom at y = %v, want 0", pos.Y-wh.Config.Radius)
	}
	if rot.AngleTo(car.Body.Rot) > 1e-3 {
		t.Fatalf("resting wheel rotation %+v differs from chassis %+v", rot, car.Body.Rot)
	}

	drive(w, car, vehicle.Input{Steer: -1}, rate)
	_, rot = car.WheelTransform(0)
	axle := rot.RotateVec(physics3d.Vec3{X: 1})
	// Steered left, the axle turns counter-clockwise from above: its -Z component grows.
	if axle.Z >= -0.3 {
		t.Fatalf("steered wheel axle = %+v", axle)
	}
}

func TestSpinAngleWraps(t *testing.T) {
	w, car := flatWorld(t)
	drive(w, car, vehicle.Input{Throttle: 1}, 5*rate)
	for _, wh := range car.Wheels {
		if wh.SpinAngle < -math.Pi || wh.SpinAngle > math.Pi {
			t.Fatalf("spin angle %v outside [-pi, pi]", wh.SpinAngle)
		}
	}
}

// TestVehicleInGameLoop drives the car from a real game.Loop for one second of display time.
func TestVehicleInGameLoop(t *testing.T) {
	w, car := flatWorld(t)
	steps := 0
	l := &game.Loop{Rate: rate, Update: func(d time.Duration) {
		car.SetInput(vehicle.Input{Throttle: 1})
		car.Step(w, d)
		w.Step(d)
		steps++
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	app.Advance(time.Second)
	if steps < rate-1 {
		t.Fatalf("loop ran %d steps in a second", steps)
	}
	if car.Speed() <= 1 {
		t.Fatalf("speed after a second in the loop = %v", car.Speed())
	}
}

func BenchmarkVehicleStep(b *testing.B) {
	w, car := flatWorld(b)
	car.SetInput(vehicle.Input{Throttle: 0.5, Steer: 0.3})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		car.Step(w, dt)
		w.Step(dt)
	}
}

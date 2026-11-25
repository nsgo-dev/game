package vehicle_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/vehicle"
)

func buildDeterministicVehicleScene(t *testing.T) (*physics3d.World, *vehicle.Vehicle) {
	t.Helper()
	world := physics3d.NewWorld()
	world.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 200, Y: 1, Z: 200}}),
		physics3d.Vec3{Y: -1},
	))
	// Small ramp ahead of the car
	rampVerts := []physics3d.Vec3{
		{X: -6, Y: 0, Z: -18},
		{X: 6, Y: 0, Z: -18},
		{X: 6, Y: 0.7, Z: -26},
		{X: -6, Y: 0.7, Z: -26},
	}
	world.AddTriMesh(physics3d.NewTriMeshCollider(rampVerts, []uint32{0, 1, 2, 0, 2, 3}))

	cfg := vehicle.ReferenceConfig()
	car, err := vehicle.New(world, cfg, physics3d.Vec3{Y: 1.0}, physics3d.QuatIdentity())
	if err != nil {
		t.Fatalf("vehicle.New: %v", err)
	}
	return world, car
}

func driveVehicleStep(world *physics3d.World, car *vehicle.Vehicle, step int) {
	const dt = time.Second / 60
	var in vehicle.Input
	switch {
	case step < 90:
		in = vehicle.Input{Throttle: 1.0}
	case step < 150:
		in = vehicle.Input{Throttle: 0.85, Steer: -0.65}
	case step < 185:
		in = vehicle.Input{Throttle: 0.4, Steer: 0.8, Handbrake: true}
	case step < 220:
		in = vehicle.Input{Brake: 0.7, Steer: 0.2}
	default:
		in = vehicle.Input{}
	}
	car.SetInput(in)
	car.Step(world, dt)
	world.Step(dt)
}

func TestVehicleSaveRestoreBitIdentical(t *testing.T) {
	world, car := buildDeterministicVehicleScene(t)
	for step := 0; step < 100; step++ {
		driveVehicleStep(world, car, step)
	}

	worldSnap := world.Save()
	carSnap := car.Save()
	snapWorldHash := world.Checksum()
	snapCarHash := car.Checksum()

	rawW, err := json.Marshal(worldSnap)
	if err != nil {
		t.Fatalf("marshal world: %v", err)
	}
	rawC, err := json.Marshal(carSnap)
	if err != nil {
		t.Fatalf("marshal vehicle: %v", err)
	}
	var decW physics3d.WorldSnapshot
	var decC vehicle.Snapshot
	if err := json.Unmarshal(rawW, &decW); err != nil {
		t.Fatalf("unmarshal world: %v", err)
	}
	if err := json.Unmarshal(rawC, &decC); err != nil {
		t.Fatalf("unmarshal vehicle: %v", err)
	}

	for step := 100; step < 240; step++ {
		driveVehicleStep(world, car, step)
	}
	wantWorldHash := world.Checksum()
	wantCarHash := car.Checksum()

	if err := world.Restore(decW); err != nil {
		t.Fatalf("world.Restore: %v", err)
	}
	if err := car.Restore(decC, world); err != nil {
		t.Fatalf("car.Restore: %v", err)
	}
	if world.Checksum() != snapWorldHash || car.Checksum() != snapCarHash {
		t.Fatalf("restored checksums mismatch: got (%#x, %#x), want (%#x, %#x)",
			world.Checksum(), car.Checksum(), snapWorldHash, snapCarHash)
	}

	for step := 100; step < 240; step++ {
		driveVehicleStep(world, car, step)
	}
	if world.Checksum() != wantWorldHash || car.Checksum() != wantCarHash {
		t.Fatalf("step 240 checksums after restore mismatch: got (%#x, %#x), want (%#x, %#x)",
			world.Checksum(), car.Checksum(), wantWorldHash, wantCarHash)
	}
}

func TestVehicleGoldenTrajectory(t *testing.T) {
	// Cross-architecture golden trajectory hashes (must match identically on amd64 and arm64).
	const (
		wantCarGolden   uint64 = 0xe1c416f566fad20c
		wantWorldGolden uint64 = 0x3db756e0501f3076
	)

	world, car := buildDeterministicVehicleScene(t)
	for step := 0; step < 240; step++ {
		driveVehicleStep(world, car, step)
	}
	if got := car.Checksum(); got != wantCarGolden {
		t.Fatalf("vehicle.Checksum() = %#016x, want %#016x", got, wantCarGolden)
	}
	if got := world.Checksum(); got != wantWorldGolden {
		t.Fatalf("world.Checksum() = %#016x, want %#016x", got, wantWorldGolden)
	}
}

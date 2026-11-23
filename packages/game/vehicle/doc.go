// Package vehicle simulates raycast cars on a physics3d.World: wheel colliders cast from the chassis, spring-damper suspension, longitudinal and lateral tire friction inside a friction circle, and engine, brake, steering and handbrake input. It also tracks race checkpoints and laps on trigger bodies and records ghost replays of a lap as YAML.
//
// # Model
//
// A Vehicle is one dynamic physics3d.Body (the chassis, whose Pos is the center of mass) plus a list of wheels. Every fixed step, Vehicle.Step casts one ray per wheel from its anchor along the chassis' down axis, turns the compression into a suspension force, and applies tire impulses at the contact point before World.Step integrates the chassis. Wheels have no bodies of their own, so they cannot snag on geometry and cost one raycast each.
//
// Vehicles face -Z in local space, with +X to the right and +Y up, matching the conventions in the 3D models guide.
//
// # Files
//
// A car's tuning is a Config, written by people as versioned YAML (ParseConfig and EncodeConfig). A lap's ghost is a Ghost, written by the game as versioned YAML (ParseGhost and EncodeGhost). Both refuse unknown fields, encode deterministically and round-trip without change.
//
// # Determinism
//
// Vehicle.Step uses no maps, wall clock or randomness and visits wheels in Config order, so the same Config, start pose and per-step Input on the same World produce bit-identical poses, lap times and ghosts across runs. Feeding Input from a replay.Player (through Controls.Read) reproduces a recorded drive.
package vehicle

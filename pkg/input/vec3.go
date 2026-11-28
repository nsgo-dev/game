package input

// Vec3 is a 3-component float32 vector in right-handed Y-up space (+X right, +Y up, -Z forward), the positions and directions of AimAssist's 3D evaluation. Its field layout matches math3d.Vec3 and pathfind.Vec3, so callers convert with a zero-cost input.Vec3(v) cast; input keeps a type of its own so it imports neither the experimental math3d nor pathfind, which imports tilemap and sprite and would make a cycle for any of their tests that uses gametest.
type Vec3 struct {
	X, Y, Z float32
}

package pathfind

// Vec3 is a 3-component float32 vector in right-handed Y-up space (+X right, +Y up, -Z forward). Its field layout matches math3d.Vec3 so callers can convert values with a zero-cost pathfind.Vec3(v) cast without importing the experimental game/math3d package from stable game/pathfind.
type Vec3 struct {
	X, Y, Z float32
}

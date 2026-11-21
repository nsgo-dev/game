// Package physics3d provides compact, deterministic 3D collision detection,
// raycasting, rigid-body dynamics, and a kinematic character controller in pure Go.
//
// # Architecture
//
// physics3d is implemented purely with the Go standard library and no cgo or
// third-party physics engine bindings (Epic #180, option a). Avoiding cgo keeps
// builds reproducible and portable across every NSGo target — macOS, Windows,
// Linux, iOS, Android, and WebAssembly — without C/C++ toolchains, per-platform
// native libraries, or FFI marshaling overhead.
//
// The simulation trades full articulated meshes and rotational inertia tensors
// for a focused set of primitives (axis-aligned boxes, spheres, and capsules),
// exact overlap and raycast queries, translational rigid bodies with iterative
// impulse and positional resolution, and a capsule character controller that
// handles slopes, stairs, and wall sliding.
//
// # Coordinate Convention
//
// physics3d uses a right-handed, Y-up coordinate system: +X is right, +Y is up,
// and +Z points toward the viewer. Default gravity in NewWorld is (0, -9.81, 0).
//
// # Determinism
//
// Stepping a World with fixed durations (such as from game.Loop.Update) runs
// collision detection, impulse solving, and positional correction in a fixed
// body and contact order with no map-order iteration or wall-clock input, so
// identical initial states produce bit-identical trajectories across runs.
package physics3d

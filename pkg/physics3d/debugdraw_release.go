//go:build release

package physics3d

import "github.com/nsgo-dev/nsgo/core"

// DrawLine3D is a zero-cost no-op in release builds.
func DrawLine3D(*core.Drawing, Mat4, core.Bounds, Vec3, Vec3, core.Color, float32) {}

// DrawLine3DZO is a zero-cost no-op in release builds.
func DrawLine3DZO(*core.Drawing, Mat4, core.Bounds, Vec3, Vec3, core.Color, float32) {}

// DrawShape is a zero-cost no-op in release builds.
func DrawShape(*core.Drawing, Mat4, core.Bounds, Shape, DebugStyle) {}

// DrawBody is a zero-cost no-op in release builds.
func DrawBody(*core.Drawing, Mat4, core.Bounds, *Body, DebugStyle) {}

// DrawCharacter is a zero-cost no-op in release builds.
func DrawCharacter(*core.Drawing, Mat4, core.Bounds, *CharacterController, DebugStyle) {}

// DrawContact is a zero-cost no-op in release builds.
func DrawContact(*core.Drawing, Mat4, core.Bounds, Contact, DebugStyle) {}

// DrawWorld is a zero-cost no-op in release builds.
func DrawWorld(*core.Drawing, Mat4, core.Bounds, *World, DebugStyle) {}

// DrawRaycast is a zero-cost no-op in release builds.
func DrawRaycast(*core.Drawing, Mat4, core.Bounds, Ray, RaycastHit, bool, DebugStyle) {}

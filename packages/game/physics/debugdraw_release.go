//go:build release

package physics

import "github.com/nsgo-dev/nsgo/core"

// DebugStyle configures colors and screen-space annotation sizes for physics debug drawing.
// In release builds, debug drawing functions are no-ops and link no drawing code.
type DebugStyle struct {
	BoxColor        core.Color
	CircleColor     core.Color
	SolidTileColor  core.Color
	OneWayTileColor core.Color
	ContactColor    core.Color
	NormalColor     core.Color
	RayColor        core.Color
	VelocityColor   core.Color
	PolygonColor    core.Color
	CapsuleColor    core.Color
	SegmentColor    core.Color

	LineWidth     float32
	Zoom          float32
	NormalLength  float32
	ContactRadius float32
	VelocityScale float32
}

// DefaultDebugStyle returns a zero DebugStyle in release builds.
func DefaultDebugStyle() DebugStyle { return DebugStyle{} }

// DrawShape is a no-op in release builds.
func DrawShape(*core.Drawing, Shape, DebugStyle) {}

// DrawBody is a no-op in release builds.
func DrawBody(*core.Drawing, *Body, DebugStyle) {}

// DrawContact is a no-op in release builds.
func DrawContact(*core.Drawing, Contact, DebugStyle) {}

// DrawTileContacts is a no-op in release builds.
func DrawTileContacts(*core.Drawing, TileGrid, []TileContact, DebugStyle) {}

// DrawTileGrid is a no-op in release builds.
func DrawTileGrid(*core.Drawing, TileGrid, core.Bounds, DebugStyle) {}

// DrawWorld is a no-op in release builds.
func DrawWorld(*core.Drawing, *World, core.Bounds, []Contact, DebugStyle) {}

// DrawRaycast is a no-op in release builds.
func DrawRaycast(*core.Drawing, core.Point, core.Point, []RaycastHit, DebugStyle) {}

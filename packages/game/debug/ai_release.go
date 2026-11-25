//go:build release

package debug

import (
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/nsgo/core"
)

// BehaviorTreeSnapshot is a no-op returning the zero AISnapshot in release builds.
func BehaviorTreeSnapshot(string, *ai.BTInspector) AISnapshot { return AISnapshot{} }

// StateMachineSnapshot is a no-op returning the zero AISnapshot in release builds.
func StateMachineSnapshot(string, flow.MachineInspector) AISnapshot { return AISnapshot{} }

// SetAI is a no-op in release builds.
func (*Overlay) SetAI(AISnapshot) {}

// ClearAI is a no-op in release builds.
func (*Overlay) ClearAI() {}

// RefreshAI is a no-op in release builds.
func (*Overlay) RefreshAI() {}

// AISnapshot always returns the zero AISnapshot in release builds.
func (*Overlay) AISnapshot() AISnapshot { return AISnapshot{} }

// DrawVector is a no-op in release builds.
func (*Overlay) DrawVector(*core.Drawing, core.Point, core.Point, core.Color) {}

// DrawPath is a no-op in release builds.
func (*Overlay) DrawPath(*core.Drawing, []core.Point, core.Color) {}

// DrawTarget is a no-op in release builds.
func (*Overlay) DrawTarget(*core.Drawing, core.Point, core.Color) {}

// DrawSteering is a no-op in release builds.
func (*Overlay) DrawSteering(*core.Drawing, SteeringDebug) {}

// DrawVector is a no-op in release builds.
func (*ShapeOverlay) DrawVector(*core.Drawing, core.Point, core.Point, core.Color) {}

// DrawPath is a no-op in release builds.
func (*ShapeOverlay) DrawPath(*core.Drawing, []core.Point, core.Color) {}

// DrawTarget is a no-op in release builds.
func (*ShapeOverlay) DrawTarget(*core.Drawing, core.Point, core.Color) {}

// DrawSteering is a no-op in release builds.
func (*ShapeOverlay) DrawSteering(*core.Drawing, SteeringDebug) {}

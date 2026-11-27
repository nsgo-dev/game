package scene

import (
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// ScreenNotifier watches a world rectangle against camera views and fires OnEnter and OnExit when it starts and stops being visible. It is the code counterpart of the scene format's built-in "on_screen" component, for games that do not use scene files.
//
// Rect is the watched rectangle in world coordinates; move it as the object moves. Margin is a band of extra points around a view's bounds: a visible notifier fires OnExit only after leaving the view by more than the margin, while an off-screen notifier fires OnEnter as soon as it touches the view itself, so a view hovering near an edge does not flap between the two states.
//
// A notifier starts off-screen, so the first Step that finds Rect inside a view fires OnEnter, as Godot's VisibleOnScreenNotifier2D does for a node that begins on-screen. Step with no views treats the notifier as on-screen. It is not safe for concurrent use: step it from the goroutine that runs the game's update.
type ScreenNotifier struct {
	// Rect is the watched rectangle in world coordinates. An empty rectangle (zero or negative size) never enters any view.
	Rect core.Bounds
	// Margin is the extra points around a view's bounds an on-screen notifier may reach before OnExit fires; 0 exits exactly at the view's edge. A negative margin behaves as 0.
	Margin float32
	// OnEnter runs when Rect starts overlapping a view (or the first Step sees it inside one). Nil fires nothing.
	OnEnter func()
	// OnExit runs when Rect stops overlapping every view, margin included. Nil fires nothing.
	OnExit func()

	onScreen bool
}

// OnScreen reports whether the notifier is currently inside a view, as of the last Step.
func (n *ScreenNotifier) OnScreen() bool {
	if n == nil {
		return false
	}
	return n.onScreen
}

// Step tests Rect against every view's visible bounds and fires OnEnter or OnExit when the result changes, then returns whether the notifier is on screen. An object is on screen when any view sees it, so pass every camera that draws the world, such as every SplitScreen viewport's camera. Nil cameras are skipped, and Step with no views reports the notifier on screen. Visible bounds are the axis-aligned rectangle covering a view's rotated corners, as tilemap.Camera.VisibleBounds computes them.
func (n *ScreenNotifier) Step(views ...*tilemap.Camera) bool {
	if n == nil {
		return false
	}
	rect := math2d.FromBounds(n.Rect)
	margin := n.Margin
	if margin < 0 {
		margin = 0
	}
	vis := false
	cameras := 0
	for _, cam := range views {
		if cam == nil {
			continue
		}
		cameras++
		bounds := math2d.FromBounds(cam.VisibleBounds())
		if n.onScreen {
			bounds = bounds.Expand(margin)
		}
		if rect.Overlaps(bounds) {
			vis = true
			break
		}
	}
	if cameras == 0 {
		// No view to test against, so everything is on screen (except an empty rectangle).
		vis = !rect.Empty()
	}
	if vis && !n.onScreen {
		n.onScreen = true
		if n.OnEnter != nil {
			n.OnEnter()
		}
	}
	if !vis && n.onScreen {
		n.onScreen = false
		if n.OnExit != nil {
			n.OnExit()
		}
	}
	return n.onScreen
}

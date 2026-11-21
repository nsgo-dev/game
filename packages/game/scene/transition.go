package scene

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Direction is the travel direction of a Wipe transition's dividing edge.
type Direction uint8

const (
	// DirectionLeft sweeps the dividing edge from right to left, revealing the new scene on the right.
	DirectionLeft Direction = iota
	// DirectionRight sweeps the dividing edge from left to right, revealing the new scene on the left.
	DirectionRight
	// DirectionUp sweeps the dividing edge from bottom to top, revealing the new scene on the bottom.
	DirectionUp
	// DirectionDown sweeps the dividing edge from top to bottom, revealing the new scene on the top.
	DirectionDown
)

// Transition animates the visual handoff between an outgoing scene and an incoming scene over Duration.
type Transition struct {
	// Duration is how long the transition runs; a non-positive Duration switches immediately.
	Duration time.Duration
	// Curve eases the linear progress in [0, 1]; its zero value is core.Linear.
	Curve core.Curve
	// Draw draws one transition frame at eased progress in [0, 1] into d for a canvas of size,
	// calling drawOld and drawNew as needed to render the outgoing and incoming scenes.
	Draw func(d *core.Drawing, size core.Size, progress float64, drawOld, drawNew func(*core.Drawing))
}

// Fade returns a Transition lasting duration.
//
// When color is 0 or core.Transparent, it crossfades directly from the old scene (alpha 1 to 0) to the new scene (alpha 0 to 1).
// When color is non-transparent, it fades the old scene to color over the first half of duration and reveals the new scene from color over the second half.
func Fade(duration time.Duration, color core.Color) *Transition {
	if color == 0 || color == core.Transparent {
		return &Transition{
			Duration: duration,
			Draw: func(d *core.Drawing, _ core.Size, progress float64, drawOld, drawNew func(*core.Drawing)) {
				p := clamp01(progress)
				if oldAlpha := float32(1 - p); oldAlpha > 0 && drawOld != nil {
					d.Save()
					d.SetAlpha(oldAlpha)
					drawOld(d)
					d.Restore()
				}
				if newAlpha := float32(p); newAlpha > 0 && drawNew != nil {
					d.Save()
					d.SetAlpha(newAlpha)
					drawNew(d)
					d.Restore()
				}
			},
		}
	}
	return &Transition{
		Duration: duration,
		Draw: func(d *core.Drawing, size core.Size, progress float64, drawOld, drawNew func(*core.Drawing)) {
			p := clamp01(progress)
			var overlayAlpha float32
			if p < 0.5 {
				if drawOld != nil {
					d.Save()
					drawOld(d)
					d.Restore()
				}
				overlayAlpha = float32(p * 2)
			} else {
				if drawNew != nil {
					d.Save()
					drawNew(d)
					d.Restore()
				}
				overlayAlpha = float32((1 - p) * 2)
			}
			if overlayAlpha > 0 && size.Width > 0 && size.Height > 0 {
				d.Save()
				d.SetAlpha(overlayAlpha)
				d.SetFillColor(color)
				d.FillRect(0, 0, float32(size.Width), float32(size.Height))
				d.Restore()
			}
		},
	}
}

// Crossfade returns a Transition that crossfades directly from the old scene to the new scene over duration.
func Crossfade(duration time.Duration) *Transition {
	return Fade(duration, 0)
}

// Wipe returns a Transition that clips the old and new scenes along an axis-aligned dividing edge moving in dir over duration.
func Wipe(duration time.Duration, dir Direction) *Transition {
	return &Transition{
		Duration: duration,
		Draw: func(d *core.Drawing, size core.Size, progress float64, drawOld, drawNew func(*core.Drawing)) {
			p := clamp01(progress)
			if p <= 0 {
				if drawOld != nil {
					d.Save()
					drawOld(d)
					d.Restore()
				}
				return
			}
			if p >= 1 {
				if drawNew != nil {
					d.Save()
					drawNew(d)
					d.Restore()
				}
				return
			}
			w, h := float32(size.Width), float32(size.Height)
			var oldX, oldY, oldW, oldH float32
			var newX, newY, newW, newH float32
			switch dir {
			case DirectionRight:
				split := w * float32(p)
				oldX, oldY, oldW, oldH = split, 0, w-split, h
				newX, newY, newW, newH = 0, 0, split, h
			case DirectionDown:
				split := h * float32(p)
				oldX, oldY, oldW, oldH = 0, split, w, h-split
				newX, newY, newW, newH = 0, 0, w, split
			case DirectionUp:
				split := h * float32(1-p)
				oldX, oldY, oldW, oldH = 0, 0, w, split
				newX, newY, newW, newH = 0, split, w, h-split
			default: // DirectionLeft
				split := w * float32(1-p)
				oldX, oldY, oldW, oldH = 0, 0, split, h
				newX, newY, newW, newH = split, 0, w-split, h
			}
			if drawOld != nil && oldW > 0 && oldH > 0 {
				d.Save()
				d.BeginPath().Rect(oldX, oldY, oldW, oldH).Clip().BeginPath()
				drawOld(d)
				d.Restore()
			}
			if drawNew != nil && newW > 0 && newH > 0 {
				d.Save()
				d.BeginPath().Rect(newX, newY, newW, newH).Clip().BeginPath()
				drawNew(d)
				d.Restore()
			}
		},
	}
}

// CustomTransition returns a Transition driven over duration with a caller-supplied draw function.
func CustomTransition(duration time.Duration, draw func(d *core.Drawing, size core.Size, progress float64, drawOld, drawNew func(*core.Drawing))) *Transition {
	return &Transition{
		Duration: duration,
		Draw:     draw,
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

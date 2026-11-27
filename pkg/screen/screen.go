// Package screen maps a game's virtual canvas onto any window size, converts pointer input between view and virtual coordinates, keeps HUD layers inside the platform's safe area, and applies a player's video settings (full screen, display and frame cap) with Video.
package screen

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Mode chooses how a Viewport fits its virtual canvas into a window of another aspect ratio.
type Mode uint8

const (
	// ModeFit scales uniformly so the virtual canvas fits inside the window, centered, with bars filling any leftover margin.
	ModeFit Mode = iota
	// ModeFill scales uniformly so the virtual canvas covers the window, centered, clipping what overflows.
	ModeFill
	// ModeStretch scales X and Y independently so the virtual canvas fills the window.
	ModeStretch
	// ModeExpand scales uniformly to match the virtual canvas on the tighter axis and expands the visible virtual world along the other so the window fills with no bars or cropping.
	ModeExpand
)

// String returns the mode's name.
func (m Mode) String() string {
	switch m {
	case ModeFit:
		return "fit"
	case ModeFill:
		return "fill"
	case ModeStretch:
		return "stretch"
	case ModeExpand:
		return "expand"
	default:
		return "unknown"
	}
}

// Viewport maps a virtual resolution onto any window: computing its placement, recording its bars, clip and transform into a core.Drawing, converting pointer input, and deriving safe-area bounds for HUD layers.
type Viewport struct {
	// Width and Height are the virtual canvas size in game points (for example 320 by 180).
	Width, Height float32
	// Mode chooses how the virtual canvas adapts to the window's aspect ratio. Its zero value is ModeFit.
	Mode Mode
	// IntegerScale scales by the largest whole integer >= 1 that fits the virtual canvas inside the window, falling back to fractional ModeFit when the window is smaller than 1x, centered with bars.
	IntegerScale bool
	// PixelSnap aligns viewport offsets to whole points and causes the Viewport's Snap and SnapPoint methods to round coordinates to whole virtual pixels.
	PixelSnap bool
	// BarColor is the color filled into letterbox and pillarbox bars. Zero defaults to opaque black; core.Transparent draws no bars.
	BarColor core.Color
}

// Placement is where a Viewport places its virtual canvas in a window.
type Placement struct {
	// Rect is the virtual canvas's rectangle in view coordinates. In ModeFill it may extend past the window's edges.
	Rect core.Bounds
	// VirtualSize is the visible virtual canvas size passed to Draw's callback: (Width, Height) in ModeFit, ModeFill and ModeStretch, and expanded along the longer axis in ModeExpand.
	VirtualSize core.Size
	// ScaleX and ScaleY convert virtual coordinates to view coordinates.
	ScaleX, ScaleY float32
	// Bars are the non-overlapping letterbox or pillarbox rectangles in view coordinates, empty when the virtual canvas covers the window.
	Bars []core.Bounds
}

// Layout returns where v places its virtual canvas in a window of the given size.
//
// It returns a zero Placement if the window or virtual resolution has a non-positive dimension.
func (v Viewport) Layout(window core.Size) Placement {
	winW, winH := float64(window.Width), float64(window.Height)
	virW, virH := float64(v.Width), float64(v.Height)
	if winW <= 0 || winH <= 0 || virW <= 0 || virH <= 0 {
		return Placement{}
	}

	sx := winW / virW
	sy := winH / virH
	cross := winW*virH - winH*virW

	if v.IntegerScale {
		scale := min(sx, sy)
		if scale >= 1 {
			scale = math.Floor(scale)
		}
		var rectW, rectH float64
		if scale < 1 {
			if cross < 0 {
				rectW, rectH = winW, virH*scale
			} else if cross > 0 {
				rectW, rectH = virW*scale, winH
			} else {
				rectW, rectH = winW, winH
			}
		} else {
			rectW, rectH = virW*scale, virH*scale
		}
		rectW = min(rectW, winW)
		rectH = min(rectH, winH)
		ox := (winW - rectW) / 2
		oy := (winH - rectH) / 2
		if v.PixelSnap {
			ox = round(ox)
			oy = round(oy)
		}
		return Placement{
			Rect: core.Bounds{
				Origin: core.Point{X: core.Points(ox), Y: core.Points(oy)},
				Size:   core.Size{Width: core.Points(rectW), Height: core.Points(rectH)},
			},
			VirtualSize: core.Size{Width: core.Points(v.Width), Height: core.Points(v.Height)},
			ScaleX:      float32(scale),
			ScaleY:      float32(scale),
			Bars:        computeBars(winW, winH, ox, oy, rectW, rectH),
		}
	}

	switch v.Mode {
	case ModeFill:
		var scale, rectW, rectH float64
		if cross > 0 {
			scale = sx
			rectW, rectH = winW, virH*sx
		} else if cross < 0 {
			scale = sy
			rectW, rectH = virW*sy, winH
		} else {
			scale = sx
			rectW, rectH = winW, winH
		}
		rectW = max(rectW, winW)
		rectH = max(rectH, winH)
		ox := (winW - rectW) / 2
		oy := (winH - rectH) / 2
		if v.PixelSnap {
			ox = round(ox)
			oy = round(oy)
		}
		return Placement{
			Rect: core.Bounds{
				Origin: core.Point{X: core.Points(ox), Y: core.Points(oy)},
				Size:   core.Size{Width: core.Points(rectW), Height: core.Points(rectH)},
			},
			VirtualSize: core.Size{Width: core.Points(v.Width), Height: core.Points(v.Height)},
			ScaleX:      float32(scale),
			ScaleY:      float32(scale),
		}

	case ModeStretch:
		return Placement{
			Rect:        core.Bounds{Size: window},
			VirtualSize: core.Size{Width: core.Points(v.Width), Height: core.Points(v.Height)},
			ScaleX:      float32(sx),
			ScaleY:      float32(sy),
		}

	case ModeExpand:
		var scale, outW, outH float64
		if cross > 0 {
			scale = sy
			outW, outH = winW/sy, virH
		} else if cross < 0 {
			scale = sx
			outW, outH = virW, winH/sx
		} else {
			scale = sx
			outW, outH = virW, virH
		}
		return Placement{
			Rect:        core.Bounds{Size: window},
			VirtualSize: core.Size{Width: core.Points(outW), Height: core.Points(outH)},
			ScaleX:      float32(scale),
			ScaleY:      float32(scale),
		}

	default: // ModeFit
		var scale, rectW, rectH float64
		if cross < 0 {
			scale = sx
			rectW, rectH = winW, min(virH*sx, winH)
		} else if cross > 0 {
			scale = sy
			rectW, rectH = min(virW*sy, winW), winH
		} else {
			scale = sx
			rectW, rectH = winW, winH
		}
		ox := (winW - rectW) / 2
		oy := (winH - rectH) / 2
		if v.PixelSnap {
			ox = round(ox)
			oy = round(oy)
		}
		return Placement{
			Rect: core.Bounds{
				Origin: core.Point{X: core.Points(ox), Y: core.Points(oy)},
				Size:   core.Size{Width: core.Points(rectW), Height: core.Points(rectH)},
			},
			VirtualSize: core.Size{Width: core.Points(v.Width), Height: core.Points(v.Height)},
			ScaleX:      float32(scale),
			ScaleY:      float32(scale),
			Bars:        computeBars(winW, winH, ox, oy, rectW, rectH),
		}
	}
}

// Draw records the viewport's bars, clip (when bars or overflow are present), translation and scale into d, calls fn with the visible virtual size, and restores d's state to what it was before.
func (v Viewport) Draw(d *core.Drawing, window core.Size, fn func(d *core.Drawing, size core.Size)) {
	p := v.Layout(window)
	if p.ScaleX <= 0 || p.ScaleY <= 0 {
		return
	}
	if len(p.Bars) > 0 {
		if c, ok := v.barColor(); ok {
			d.Save()
			d.SetFillColor(c)
			for _, b := range p.Bars {
				d.FillRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height))
			}
			d.Restore()
		}
	}
	if fn == nil {
		return
	}
	d.Save()
	if clip, ok := p.clipRect(window); ok {
		d.BeginPath().
			Rect(float32(clip.Origin.X), float32(clip.Origin.Y), float32(clip.Size.Width), float32(clip.Size.Height)).
			Clip().
			BeginPath()
	}
	if p.Rect.Origin.X != 0 || p.Rect.Origin.Y != 0 {
		d.Translate(float32(p.Rect.Origin.X), float32(p.Rect.Origin.Y))
	}
	if p.ScaleX != 1 || p.ScaleY != 1 {
		d.Scale(p.ScaleX, p.ScaleY)
	}
	fn(d, p.VirtualSize)
	d.Restore()
}

// ViewToGame converts a point in view coordinates (such as core.PressEvent.Position) to virtual game coordinates.
//
// It returns ok == false when p falls on a letterbox or pillarbox bar, outside the window, or when the viewport has a non-positive layout.
func (v Viewport) ViewToGame(window core.Size, p core.Point) (core.Point, bool) {
	pl := v.Layout(window)
	if pl.ScaleX <= 0 || pl.ScaleY <= 0 {
		return core.Point{}, false
	}
	visible := intersectBounds(pl.Rect, core.Bounds{Size: window})
	if visible.Size.Width <= 0 || visible.Size.Height <= 0 || !visible.Contains(p) {
		return core.Point{}, false
	}
	gx := (float64(p.X) - float64(pl.Rect.Origin.X)) / float64(pl.ScaleX)
	gy := (float64(p.Y) - float64(pl.Rect.Origin.Y)) / float64(pl.ScaleY)
	return core.Point{X: core.Points(gx), Y: core.Points(gy)}, true
}

// GameToView converts a point in virtual game coordinates to view coordinates.
func (v Viewport) GameToView(window core.Size, p core.Point) core.Point {
	pl := v.Layout(window)
	if pl.ScaleX <= 0 || pl.ScaleY <= 0 {
		return core.Point{}
	}
	vx := float64(pl.Rect.Origin.X) + float64(p.X)*float64(pl.ScaleX)
	vy := float64(pl.Rect.Origin.Y) + float64(p.Y)*float64(pl.ScaleY)
	return core.Point{X: core.Points(vx), Y: core.Points(vy)}
}

// Snap rounds x and y to whole virtual pixels when v.PixelSnap is true, and returns them unchanged otherwise.
func (v Viewport) Snap(x, y float32) (float32, float32) {
	if !v.PixelSnap {
		return x, y
	}
	return Snap(x, y)
}

// SnapPoint rounds p to whole virtual pixels when v.PixelSnap is true, and returns it unchanged otherwise.
func (v Viewport) SnapPoint(p core.Point) core.Point {
	if !v.PixelSnap {
		return p
	}
	return SnapPoint(p)
}

// Snap rounds x and y to whole virtual pixels.
func Snap(x, y float32) (float32, float32) {
	return float32(round(float64(x))), float32(round(float64(y)))
}

// SnapPoint rounds p's coordinates to whole virtual pixels.
func SnapPoint(p core.Point) core.Point {
	return core.Point{
		X: core.Points(round(float64(p.X))),
		Y: core.Points(round(float64(p.Y))),
	}
}

// SafeRect returns the window rectangle inset by insets in view coordinates, clamped so its origin stays in the window and its size is never negative.
func SafeRect(window core.Size, insets core.Edges) core.Bounds {
	if window.Width <= 0 || window.Height <= 0 {
		return core.Bounds{}
	}
	left := min(max(core.Points(0), insets.Left), window.Width)
	top := min(max(core.Points(0), insets.Top), window.Height)
	right := max(left, window.Width-max(core.Points(0), insets.Right))
	bottom := max(top, window.Height-max(core.Points(0), insets.Bottom))
	return core.Bounds{
		Origin: core.Point{X: left, Y: top},
		Size:   core.Size{Width: right - left, Height: bottom - top},
	}
}

// SafeRectForEnv returns SafeRect for env's current window size and safe-area insets. Called inside a computation or Canvas.Draw, it subscribes to window and safe-area changes.
func SafeRectForEnv(env *core.Env) core.Bounds {
	if env == nil {
		return core.Bounds{}
	}
	return SafeRect(env.Window(), env.SafeArea())
}

// SafeGameRect returns the safe area clamped to the viewport's visible game rectangle and converted into virtual game coordinates, so screen-space HUD layers stay clear of notches, status bars and home indicators.
func (v Viewport) SafeGameRect(window core.Size, insets core.Edges) core.Bounds {
	pl := v.Layout(window)
	if pl.ScaleX <= 0 || pl.ScaleY <= 0 {
		return core.Bounds{}
	}
	clamped := intersectBounds(SafeRect(window, insets), pl.Rect)
	if clamped.Size.Width <= 0 || clamped.Size.Height <= 0 {
		return core.Bounds{}
	}
	x := (float64(clamped.Origin.X) - float64(pl.Rect.Origin.X)) / float64(pl.ScaleX)
	y := (float64(clamped.Origin.Y) - float64(pl.Rect.Origin.Y)) / float64(pl.ScaleY)
	w := float64(clamped.Size.Width) / float64(pl.ScaleX)
	h := float64(clamped.Size.Height) / float64(pl.ScaleY)
	return core.Bounds{
		Origin: core.Point{X: core.Points(x), Y: core.Points(y)},
		Size:   core.Size{Width: core.Points(w), Height: core.Points(h)},
	}
}

// SafeGameRectForEnv returns SafeGameRect for env's current window size and safe-area insets. Called inside a computation or Canvas.Draw, it subscribes to window and safe-area changes.
func (v Viewport) SafeGameRectForEnv(env *core.Env) core.Bounds {
	if env == nil {
		return core.Bounds{}
	}
	return v.SafeGameRect(env.Window(), env.SafeArea())
}

func (v Viewport) barColor() (core.Color, bool) {
	if v.BarColor == 0 {
		return core.RGB(0x000000), true
	}
	if v.BarColor == core.Transparent || v.BarColor&0xFF == 0 {
		return 0, false
	}
	return v.BarColor, true
}

func (p Placement) clipRect(window core.Size) (core.Bounds, bool) {
	if len(p.Bars) > 0 {
		return p.Rect, true
	}
	if p.Rect.Origin.X < 0 || p.Rect.Origin.Y < 0 ||
		p.Rect.Origin.X+p.Rect.Size.Width > window.Width ||
		p.Rect.Origin.Y+p.Rect.Size.Height > window.Height {
		return core.Bounds{Size: window}, true
	}
	return core.Bounds{}, false
}

func computeBars(winW, winH, x, y, w, h float64) []core.Bounds {
	const eps = 1e-4
	left := max(0, x)
	top := max(0, y)
	right := min(winW, x+w)
	bottom := min(winH, y+h)

	var bars []core.Bounds
	if top > eps {
		bars = append(bars, core.Bounds{
			Origin: core.Point{X: 0, Y: 0},
			Size:   core.Size{Width: core.Points(winW), Height: core.Points(top)},
		})
	}
	if winH-bottom > eps {
		bars = append(bars, core.Bounds{
			Origin: core.Point{X: 0, Y: core.Points(bottom)},
			Size:   core.Size{Width: core.Points(winW), Height: core.Points(winH - bottom)},
		})
	}
	midH := bottom - top
	if midH > eps {
		if left > eps {
			bars = append(bars, core.Bounds{
				Origin: core.Point{X: 0, Y: core.Points(top)},
				Size:   core.Size{Width: core.Points(left), Height: core.Points(midH)},
			})
		}
		if winW-right > eps {
			bars = append(bars, core.Bounds{
				Origin: core.Point{X: core.Points(right), Y: core.Points(top)},
				Size:   core.Size{Width: core.Points(winW - right), Height: core.Points(midH)},
			})
		}
	}
	return bars
}

func intersectBounds(a, b core.Bounds) core.Bounds {
	x0 := max(a.Origin.X, b.Origin.X)
	y0 := max(a.Origin.Y, b.Origin.Y)
	x1 := min(a.Origin.X+a.Size.Width, b.Origin.X+b.Size.Width)
	y1 := min(a.Origin.Y+a.Size.Height, b.Origin.Y+b.Size.Height)
	if x1 <= x0 || y1 <= y0 {
		return core.Bounds{}
	}
	return core.Bounds{
		Origin: core.Point{X: x0, Y: y0},
		Size:   core.Size{Width: x1 - x0, Height: y1 - y0},
	}
}

func round(v float64) float64 {
	r := math.Round(v)
	if r == 0 {
		return 0
	}
	return r
}

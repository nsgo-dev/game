package gameui

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// ScaleMode controls how a Viewport maps a virtual resolution into a physical window size.
type ScaleMode uint8

const (
	// ScaleFit uniformly scales the virtual canvas to fit inside the window, adding letterbox or pillarbox bars as needed.
	ScaleFit ScaleMode = iota
	// ScaleFill uniformly scales the virtual canvas to cover the entire window, cropping any overflow symmetrically.
	ScaleFill
	// ScaleStretch scales X and Y independently to fill the window without preserving aspect ratio.
	ScaleStretch
	// ScaleInteger uniformly scales by the largest integer factor >= 1 that fits inside the window,
	// falling back to ScaleFit when the window is smaller than the virtual resolution.
	ScaleInteger

	// ScaleLetterbox is an alias for ScaleFit.
	ScaleLetterbox = ScaleFit
	// IntegerScale is an alias for ScaleInteger.
	IntegerScale = ScaleInteger
)

// Viewport maps a fixed virtual UI resolution onto a physical window size with scaling, letterboxing,
// drawing transform setup, and window-to-virtual coordinate conversion.
type Viewport struct {
	VirtualWidth  float32
	VirtualHeight float32
	WindowWidth   float32
	WindowHeight  float32
	ScaleMode     ScaleMode
	// LetterboxColor fills letterbox/pillarbox bars in Draw and DrawLetterbox.
	// Zero defaults to opaque black; set core.Transparent to skip drawing bars.
	LetterboxColor core.Color
}

// VirtualViewport is an alias for Viewport.
type VirtualViewport = Viewport

// NewViewport returns a Viewport configured for virtualW x virtualH using mode.
func NewViewport(virtualW, virtualH float32, mode ScaleMode) Viewport {
	return Viewport{
		VirtualWidth:  virtualW,
		VirtualHeight: virtualH,
		ScaleMode:     mode,
	}
}

// SetWindowSize updates the viewport's physical window dimensions.
func (v *Viewport) SetWindowSize(size core.Size) {
	v.WindowWidth = float32(size.Width)
	v.WindowHeight = float32(size.Height)
}

// WithWindowSize returns a copy of v with WindowWidth and WindowHeight set to w and h.
func (v Viewport) WithWindowSize(w, h float32) Viewport {
	v.WindowWidth = w
	v.WindowHeight = h
	return v
}

func (v Viewport) effectiveVirtualSize() (vw, vh float32) {
	vw, vh = v.VirtualWidth, v.VirtualHeight
	if vw <= 0 {
		vw = v.WindowWidth
	}
	if vh <= 0 {
		vh = v.WindowHeight
	}
	return vw, vh
}

// VirtualBounds returns the layout rectangle in virtual coordinates: (0, 0, VirtualWidth, VirtualHeight).
func (v Viewport) VirtualBounds() core.Bounds {
	vw, vh := v.effectiveVirtualSize()
	if vw <= 0 || vh <= 0 {
		return core.Bounds{}
	}
	return core.Bounds{
		Size: core.Size{Width: core.Points(vw), Height: core.Points(vh)},
	}
}

// ScaleXY returns the horizontal and vertical scale factors from virtual coordinates to window coordinates.
func (v Viewport) ScaleXY() (sx, sy float32) {
	vw, vh := v.effectiveVirtualSize()
	ww, wh := v.WindowWidth, v.WindowHeight
	if vw <= 0 || vh <= 0 || ww <= 0 || wh <= 0 {
		return 0, 0
	}
	rx := ww / vw
	ry := wh / vh
	switch v.ScaleMode {
	case ScaleFill:
		s := max(rx, ry)
		return s, s
	case ScaleStretch:
		return rx, ry
	case ScaleInteger:
		s := min(rx, ry)
		if s >= 1 {
			s = float32(math.Floor(float64(s)))
		}
		return s, s
	default: // ScaleFit / ScaleLetterbox
		s := min(rx, ry)
		return s, s
	}
}

// Scale returns the uniform scale factor (or min(sx, sy) for ScaleStretch).
func (v Viewport) Scale() float32 {
	sx, sy := v.ScaleXY()
	return min(sx, sy)
}

// Rect returns the projected virtual canvas rectangle in physical window coordinates.
// For ScaleFill, Rect may extend outside [0, WindowWidth] x [0, WindowHeight].
func (v Viewport) Rect() core.Bounds {
	vw, vh := v.effectiveVirtualSize()
	sx, sy := v.ScaleXY()
	if sx <= 0 || sy <= 0 {
		return core.Bounds{}
	}
	rw := vw * sx
	rh := vh * sy
	ox := (v.WindowWidth - rw) / 2
	oy := (v.WindowHeight - rh) / 2
	if v.ScaleMode == ScaleInteger {
		ox = float32(math.Floor(float64(ox)))
		oy = float32(math.Floor(float64(oy)))
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(ox), Y: core.Points(oy)},
		Size:   core.Size{Width: core.Points(rw), Height: core.Points(rh)},
	}
}

// VisibleBounds returns the intersection of Rect() with the window bounds [0, WindowWidth] x [0, WindowHeight].
// For ScaleFit and ScaleInteger this equals Rect(); for ScaleFill it equals the full window bounds.
func (v Viewport) VisibleBounds() core.Bounds {
	if v.WindowWidth <= 0 || v.WindowHeight <= 0 {
		return core.Bounds{}
	}
	r := v.Rect()
	x0 := max(float32(0), float32(r.Origin.X))
	y0 := max(float32(0), float32(r.Origin.Y))
	x1 := min(v.WindowWidth, float32(r.Origin.X)+float32(r.Size.Width))
	y1 := min(v.WindowHeight, float32(r.Origin.Y)+float32(r.Size.Height))
	if x1 <= x0 || y1 <= y0 {
		return core.Bounds{}
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(x0), Y: core.Points(y0)},
		Size:   core.Size{Width: core.Points(x1 - x0), Height: core.Points(y1 - y0)},
	}
}

// LetterboxBars returns the non-empty, non-overlapping bar rectangles inside the window outside Rect().
func (v Viewport) LetterboxBars() []core.Bounds {
	ww, wh := v.WindowWidth, v.WindowHeight
	if ww <= 0 || wh <= 0 {
		return nil
	}
	r := v.Rect()
	rx := float32(r.Origin.X)
	ry := float32(r.Origin.Y)
	rw := float32(r.Size.Width)
	rh := float32(r.Size.Height)
	if rw <= 0 || rh <= 0 {
		return []core.Bounds{{Size: core.Size{Width: core.Points(ww), Height: core.Points(wh)}}}
	}

	const eps = 1e-3
	var bars []core.Bounds
	topH := clampf(ry, 0, wh)
	if topH > eps {
		bars = append(bars, core.Bounds{
			Origin: core.Point{X: 0, Y: 0},
			Size:   core.Size{Width: core.Points(ww), Height: core.Points(topH)},
		})
	} else {
		topH = 0
	}
	botY := clampf(ry+rh, 0, wh)
	if wh-botY > eps {
		bars = append(bars, core.Bounds{
			Origin: core.Point{X: 0, Y: core.Points(botY)},
			Size:   core.Size{Width: core.Points(ww), Height: core.Points(wh - botY)},
		})
	} else {
		botY = wh
	}
	midY := topH
	midH := max(0, botY-topH)
	if midH > eps {
		leftW := clampf(rx, 0, ww)
		if leftW > eps {
			bars = append(bars, core.Bounds{
				Origin: core.Point{X: 0, Y: core.Points(midY)},
				Size:   core.Size{Width: core.Points(leftW), Height: core.Points(midH)},
			})
		}
		rightX := clampf(rx+rw, 0, ww)
		if ww-rightX > eps {
			bars = append(bars, core.Bounds{
				Origin: core.Point{X: core.Points(rightX), Y: core.Points(midY)},
				Size:   core.Size{Width: core.Points(ww - rightX), Height: core.Points(midH)},
			})
		}
	}
	return bars
}

// WindowToVirtual maps a physical window point into virtual coordinates.
// ok is false when p falls outside the active viewport (for example, inside letterbox/pillarbox bars or outside the window).
func (v Viewport) WindowToVirtual(p core.Point) (core.Point, bool) {
	sx, sy := v.ScaleXY()
	if sx <= 0 || sy <= 0 {
		return core.Point{}, false
	}
	vw, vh := v.effectiveVirtualSize()
	r := v.Rect()
	px := float32(p.X)
	py := float32(p.Y)
	vx := (px - float32(r.Origin.X)) / sx
	vy := (py - float32(r.Origin.Y)) / sy
	out := core.Point{X: core.Points(vx), Y: core.Points(vy)}

	const eps = 1e-4
	inWindow := px >= -eps && px <= v.WindowWidth+eps && py >= -eps && py <= v.WindowHeight+eps
	inVirtual := vx >= -eps && vx <= vw+eps && vy >= -eps && vy <= vh+eps
	return out, inWindow && inVirtual
}

// VirtualToWindow maps a point in virtual coordinates into physical window coordinates.
func (v Viewport) VirtualToWindow(p core.Point) core.Point {
	sx, sy := v.ScaleXY()
	if sx <= 0 || sy <= 0 {
		return core.Point{}
	}
	r := v.Rect()
	return core.Point{
		X: core.Points(float32(r.Origin.X) + float32(p.X)*sx),
		Y: core.Points(float32(r.Origin.Y) + float32(p.Y)*sy),
	}
}

// DrawLetterbox fills any active letterbox/pillarbox bars into d unless LetterboxColor is core.Transparent.
func (v Viewport) DrawLetterbox(d *core.Drawing) {
	if d == nil || v.LetterboxColor == core.Transparent {
		return
	}
	bars := v.LetterboxBars()
	if len(bars) == 0 {
		return
	}
	col := v.LetterboxColor
	if col == 0 {
		col = core.RGB(0x000000)
	}
	d.Save()
	d.SetFillColor(col)
	for _, bar := range bars {
		d.FillRect(float32(bar.Origin.X), float32(bar.Origin.Y), float32(bar.Size.Width), float32(bar.Size.Height))
	}
	d.Restore()
}

// Apply clips d to VisibleBounds and applies the viewport's translation and scale transform.
// Callers should wrap Apply in d.Save() / d.Restore().
func (v Viewport) Apply(d *core.Drawing) {
	if d == nil {
		return
	}
	sx, sy := v.ScaleXY()
	if sx <= 0 || sy <= 0 {
		return
	}
	vis := v.VisibleBounds()
	if vis.Size.Width > 0 && vis.Size.Height > 0 {
		d.ClipPath(rectPath(float32(vis.Origin.X), float32(vis.Origin.Y), float32(vis.Size.Width), float32(vis.Size.Height), 0), core.NonZero)
	}
	r := v.Rect()
	if r.Origin.X != 0 || r.Origin.Y != 0 {
		d.Translate(float32(r.Origin.X), float32(r.Origin.Y))
	}
	if sx != 1 || sy != 1 {
		d.Scale(sx, sy)
	}
}

// Draw renders letterbox bars, sets up the viewport clip and transform inside Save/Restore,
// and invokes drawFn with the virtual canvas bounds.
func (v Viewport) Draw(d *core.Drawing, drawFn func(d *core.Drawing, virtualBounds core.Bounds)) {
	if d == nil || drawFn == nil {
		return
	}
	vb := v.VirtualBounds()
	if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
		return
	}
	d.Save()
	v.DrawLetterbox(d)
	v.Apply(d)
	drawFn(d, vb)
	d.Restore()
}

// FrameDrawer draws a stylized background frame (such as a nine-slice sprite or custom skin)
// for a widget in its current interaction state.
type FrameDrawer func(d *core.Drawing, bounds core.Bounds, state WidgetState)

// NineSlice describes a 9-patch sprite frame with fixed corner margins (Left, Top, Right, Bottom)
// inside Src of Image. Corners stay unstretched while edges and center scale to fill destination bounds.
type NineSlice struct {
	Image  core.ImageSource
	Src    core.Bounds
	Left   float32
	Top    float32
	Right  float32
	Bottom float32
}

// prefSize returns the nine-slice's natural size in points: its source rectangle when set, else the image's declared size.
func (ns NineSlice) prefSize() (w, h float32) {
	w, h = float32(ns.Src.Size.Width), float32(ns.Src.Size.Height)
	if w <= 0 {
		w = float32(ns.Image.Size.Width)
	}
	if h <= 0 {
		h = float32(ns.Image.Size.Height)
	}
	return w, h
}

// Draw renders the 9-patch image stretched into dst.
// Corner insets clamp proportionally if dst is smaller than Left+Right or Top+Bottom.
func (ns NineSlice) Draw(d *core.Drawing, dst core.Bounds) {
	if d == nil || dst.Size.Width <= 0 || dst.Size.Height <= 0 {
		return
	}
	sw := float32(ns.Src.Size.Width)
	sh := float32(ns.Src.Size.Height)
	if sw <= 0 || sh <= 0 {
		return
	}
	sx0 := float32(ns.Src.Origin.X)
	sy0 := float32(ns.Src.Origin.Y)

	sLeft := clampf(ns.Left, 0, sw)
	sRight := clampf(ns.Right, 0, sw-sLeft)
	sMidW := max(0, sw-sLeft-sRight)

	sTop := clampf(ns.Top, 0, sh)
	sBot := clampf(ns.Bottom, 0, sh-sTop)
	sMidH := max(0, sh-sTop-sBot)

	dx0 := float32(dst.Origin.X)
	dy0 := float32(dst.Origin.Y)
	dw := float32(dst.Size.Width)
	dh := float32(dst.Size.Height)

	dLeft, dRight := sLeft, sRight
	if dLeft+dRight > dw && dLeft+dRight > 0 {
		scale := dw / (dLeft + dRight)
		dLeft *= scale
		dRight *= scale
	}
	dMidW := max(0, dw-dLeft-dRight)

	dTop, dBot := sTop, sBot
	if dTop+dBot > dh && dTop+dBot > 0 {
		scale := dh / (dTop + dBot)
		dTop *= scale
		dBot *= scale
	}
	dMidH := max(0, dh-dTop-dBot)

	srcX := [3]float32{sx0, sx0 + sLeft, sx0 + sLeft + sMidW}
	srcW := [3]float32{sLeft, sMidW, sRight}
	dstX := [3]float32{dx0, dx0 + dLeft, dx0 + dLeft + dMidW}
	dstW := [3]float32{dLeft, dMidW, dRight}

	srcY := [3]float32{sy0, sy0 + sTop, sy0 + sTop + sMidH}
	srcH := [3]float32{sTop, sMidH, sBot}
	dstY := [3]float32{dy0, dy0 + dTop, dy0 + dTop + dMidH}
	dstH := [3]float32{dTop, dMidH, dBot}

	for row := 0; row < 3; row++ {
		if srcH[row] <= 0 || dstH[row] <= 0 {
			continue
		}
		for col := 0; col < 3; col++ {
			if srcW[col] <= 0 || dstW[col] <= 0 {
				continue
			}
			d.DrawImageFrom(
				ns.Image,
				srcX[col], srcY[row], srcW[col], srcH[row],
				dstX[col], dstY[row], dstW[col], dstH[row],
			)
		}
	}
}

// FrameDrawer adapts a single NineSlice into a FrameDrawer callback.
func (ns NineSlice) FrameDrawer() FrameDrawer {
	return func(d *core.Drawing, bounds core.Bounds, _ WidgetState) {
		ns.Draw(d, bounds)
	}
}

// NineSliceStateFrame returns a state-aware FrameDrawer that selects the matching NineSlice
// for Disabled, Pressed, Hovered, or Normal state, falling back to normal when a state slice is nil.
func NineSliceStateFrame(normal, hover, pressed, disabled *NineSlice) FrameDrawer {
	return func(d *core.Drawing, bounds core.Bounds, state WidgetState) {
		pick := normal
		switch {
		case state.Disabled && disabled != nil:
			pick = disabled
		case state.Pressed && pressed != nil:
			pick = pressed
		case state.Hovered && hover != nil:
			pick = hover
		}
		if pick != nil {
			pick.Draw(d, bounds)
		}
	}
}

// TextDrawParams carries the text string, anchor position, font, color, alignment, baseline, and widget bounds
// passed to a custom TextDrawer hook.
type TextDrawParams struct {
	Text     string
	X, Y     float32
	Font     core.Font
	Color    core.Color
	Align    core.TextAlign
	Baseline core.TextBaseline
	Bounds   core.Bounds
}

// TextDrawer renders widget text into d, allowing custom bitmap or stylized font renderers without circular imports.
type TextDrawer func(d *core.Drawing, params TextDrawParams)

// TextMeasurer measures text width and height for layout when using a custom TextDrawer.
type TextMeasurer func(text string, font core.Font) (width, height float32)

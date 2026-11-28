package gameui

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

func expandedHitBounds(visual core.Bounds, hitSize float32) core.Bounds {
	if hitSize <= 0 {
		return visual
	}
	w := float32(visual.Size.Width)
	h := float32(visual.Size.Height)
	if w <= 0 && h <= 0 {
		return visual
	}
	hw := max(w, hitSize)
	hh := max(h, hitSize)
	cx := float32(visual.Origin.X) + w/2
	cy := float32(visual.Origin.Y) + h/2
	return core.Bounds{
		Origin: core.Point{X: core.Points(cx - hw/2), Y: core.Points(cy - hh/2)},
		Size:   core.Size{Width: core.Points(hw), Height: core.Points(hh)},
	}
}

func pointInCircleBounds(visual core.Bounds, hitSize float32, p core.Point) bool {
	w := float32(visual.Size.Width)
	h := float32(visual.Size.Height)
	if w <= 0 || h <= 0 {
		return false
	}
	diam := max(max(w, h), hitSize)
	r := diam / 2
	cx := float32(visual.Origin.X) + w/2
	cy := float32(visual.Origin.Y) + h/2
	dx := float32(p.X) - cx
	dy := float32(p.Y) - cy
	return dx*dx+dy*dy <= r*r
}

// ColorRect draws a solid or translucent filled rectangle (such as a modal backdrop or HUD plate).
type ColorRect struct {
	Color        core.Color
	CornerRadius float32
	Width        float32
	Height       float32
	MinWidth     float32
	MinHeight    float32
	Flex         float32
	Align        Align
	Hidden       bool

	bounds core.Bounds
}

// Bounds returns the rectangle's laid-out bounds.
func (c *ColorRect) Bounds() core.Bounds { return c.bounds }

func (c *ColorRect) spec() layoutSpec {
	return layoutSpec{
		Width:     c.Width,
		Height:    c.Height,
		MinWidth:  c.MinWidth,
		MinHeight: c.MinHeight,
		Flex:      c.Flex,
		Align:     c.Align,
		Hidden:    c.Hidden,
	}
}

func (c *ColorRect) measure(_ *Theme) core.Size {
	return applySizeConstraints(0, 0, c.spec())
}

func (c *ColorRect) layout(bounds core.Bounds, _ *Theme) { c.bounds = bounds }
func (c *ColorRect) focusable() bool                     { return false }
func (c *ColorRect) childWidgets() []Widget              { return nil }
func (c *ColorRect) setBounds(r core.Bounds)             { c.bounds = r }

func (c *ColorRect) draw(d *core.Drawing, _ *Theme, _ WidgetState) {
	b := c.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 || c.Color == 0 || c.Color == core.Transparent {
		return
	}
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	d.SetFillColor(c.Color).FillPath(rectPath(x, y, w, h, max(0, c.CornerRadius)), core.NonZero)
}

// SpriteRow displays a horizontal row of repeated sprite icons (such as health hearts or ammo pips).
type SpriteRow struct {
	SpritePath  string
	SpriteFrame string
	Image       core.ImageSource
	Src         core.Bounds
	HasImage    bool
	Count       int
	Spacing     float32
	Color       core.Color
	Width       float32
	Height      float32
	MinWidth    float32
	MinHeight   float32
	Flex        float32
	Align       Align
	Hidden      bool

	bounds core.Bounds
}

// Bounds returns the sprite row's laid-out bounds.
func (s *SpriteRow) Bounds() core.Bounds { return s.bounds }

func (s *SpriteRow) effectiveCount() int {
	if s.Count > 0 {
		return s.Count
	}
	return 1
}

func (s *SpriteRow) spec() layoutSpec {
	return layoutSpec{
		Width:     s.Width,
		Height:    s.Height,
		MinWidth:  s.MinWidth,
		MinHeight: s.MinHeight,
		Flex:      s.Flex,
		Align:     s.Align,
		Hidden:    s.Hidden,
	}
}

func (s *SpriteRow) measure(_ *Theme) core.Size {
	count := s.effectiveCount()
	iconH := float32(16)
	if s.Height > 0 {
		iconH = s.Height
	} else if s.HasImage && s.Src.Size.Height > 0 {
		iconH = float32(s.Src.Size.Height)
	}
	iconW := iconH
	if s.HasImage && s.Src.Size.Width > 0 {
		iconW = float32(s.Src.Size.Width)
	}
	spacing := max(0, s.Spacing)
	prefW := float32(count)*iconW + float32(max(0, count-1))*spacing
	return applySizeConstraints(prefW, iconH, s.spec())
}

func (s *SpriteRow) layout(bounds core.Bounds, _ *Theme) { s.bounds = bounds }
func (s *SpriteRow) focusable() bool                     { return false }
func (s *SpriteRow) childWidgets() []Widget              { return nil }
func (s *SpriteRow) setBounds(r core.Bounds)             { s.bounds = r }

func (s *SpriteRow) draw(d *core.Drawing, theme *Theme, _ WidgetState) {
	b := s.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	count := s.effectiveCount()
	spacing := max(0, s.Spacing)
	availW := max(0, w-spacing*float32(max(0, count-1)))
	iconW := min(h, availW/float32(count))
	iconH := min(h, iconW)
	if iconW <= 0 || iconH <= 0 {
		return
	}
	x := float32(b.Origin.X)
	y := float32(b.Origin.Y) + (h-iconH)/2
	col := s.Color
	if col == 0 {
		col = theme.Accent
	}
	for i := range count {
		ix := x + float32(i)*(iconW+spacing)
		if s.HasImage {
			if s.Src.Size.Width > 0 && s.Src.Size.Height > 0 {
				d.DrawImageFrom(
					s.Image,
					float32(s.Src.Origin.X), float32(s.Src.Origin.Y), float32(s.Src.Size.Width), float32(s.Src.Size.Height),
					ix, y, iconW, iconH,
				)
			} else {
				d.DrawImageIn(s.Image, ix, y, iconW, iconH)
			}
		} else {
			d.SetFillColor(col).FillPath(rectPath(ix, y, iconW, iconH, iconW*0.25), core.NonZero)
		}
	}
}

// TouchStick is an on-screen virtual thumbstick with an optional expanded circular touch hit area.
type TouchStick struct {
	HitSize   float32
	BaseColor core.Color
	KnobColor core.Color
	ValueX    float32
	ValueY    float32
	OnMove    func(x, y float32)
	Disabled  bool
	Width     float32
	Height    float32
	MinWidth  float32
	MinHeight float32
	Flex      float32
	Align     Align
	Hidden    bool

	bounds core.Bounds
}

// Bounds returns the stick's visual bounds.
func (t *TouchStick) Bounds() core.Bounds { return t.bounds }

// HitBounds returns the stick's interactive touch target bounds, expanded to HitSize when larger than Bounds.
func (t *TouchStick) HitBounds() core.Bounds {
	return expandedHitBounds(t.bounds, t.HitSize)
}

func (t *TouchStick) spec() layoutSpec {
	return layoutSpec{
		Width:     t.Width,
		Height:    t.Height,
		MinWidth:  t.MinWidth,
		MinHeight: t.MinHeight,
		Flex:      t.Flex,
		Align:     t.Align,
		Hidden:    t.Hidden,
	}
}

func (t *TouchStick) measure(_ *Theme) core.Size {
	return applySizeConstraints(72, 72, t.spec())
}

func (t *TouchStick) layout(bounds core.Bounds, _ *Theme) { t.bounds = bounds }
func (t *TouchStick) focusable() bool                     { return false }
func (t *TouchStick) hitTestPoint(p core.Point) bool {
	return pointInCircleBounds(t.bounds, t.HitSize, p)
}
func (t *TouchStick) childWidgets() []Widget  { return nil }
func (t *TouchStick) setBounds(r core.Bounds) { t.bounds = r }

func (t *TouchStick) draw(d *core.Drawing, theme *Theme, _ WidgetState) {
	b := t.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	cx := float32(b.Origin.X) + w/2
	cy := float32(b.Origin.Y) + h/2
	r := min(w, h) / 2
	baseCol := t.BaseColor
	if baseCol == 0 {
		baseCol = core.RGBA(0xffffff, 0.08)
	}
	knobCol := t.KnobColor
	if knobCol == 0 {
		knobCol = core.RGBA(0xffffff, 0.28)
	}
	ring := new(core.Path).Arc(cx, cy, r, 0, 2*math.Pi, false)
	d.SetFillColor(baseCol).FillPath(ring, core.NonZero)
	borderCol := theme.Border
	if borderCol != 0 && borderCol != core.Transparent {
		d.SetStrokeColor(borderCol).SetLineWidth(max(1, theme.BorderWidth)).StrokePath(ring)
	}
	knobR := r * 0.35
	kx := cx + clampf(t.ValueX, -1, 1)*(r-knobR)
	ky := cy + clampf(t.ValueY, -1, 1)*(r-knobR)
	knob := new(core.Path).Arc(kx, ky, knobR, 0, 2*math.Pi, false)
	d.SetFillColor(knobCol).FillPath(knob, core.NonZero)
}

// TouchButton is an on-screen circular touch action button with an expanded touch hit target.
type TouchButton struct {
	Text       string
	HitSize    float32
	OnClick    func()
	Disabled   bool
	Font       core.Font
	TextColor  core.Color
	Background core.Color
	FocusOrder int
	Nav        NavOverrides
	Width      float32
	Height     float32
	MinWidth   float32
	MinHeight  float32
	Flex       float32
	Align      Align
	Hidden     bool
	TextDrawer TextDrawer

	bounds core.Bounds
}

// Bounds returns the touch button's visual bounds.
func (b *TouchButton) Bounds() core.Bounds { return b.bounds }

// HitBounds returns the touch button's interactive hit area, expanded to HitSize when larger than Bounds.
func (b *TouchButton) HitBounds() core.Bounds {
	return expandedHitBounds(b.bounds, b.HitSize)
}

func (b *TouchButton) spec() layoutSpec {
	return layoutSpec{
		Width:     b.Width,
		Height:    b.Height,
		MinWidth:  b.MinWidth,
		MinHeight: b.MinHeight,
		Flex:      b.Flex,
		Align:     b.Align,
		Hidden:    b.Hidden,
	}
}

func (b *TouchButton) measure(_ *Theme) core.Size {
	return applySizeConstraints(52, 52, b.spec())
}

func (b *TouchButton) layout(bounds core.Bounds, _ *Theme) { b.bounds = bounds }
func (b *TouchButton) focusable() bool                     { return !b.Disabled && !b.Hidden }
func (b *TouchButton) focusOrder() int                     { return b.FocusOrder }
func (b *TouchButton) hitTestPoint(p core.Point) bool {
	return pointInCircleBounds(b.bounds, b.HitSize, p)
}
func (b *TouchButton) childWidgets() []Widget  { return nil }
func (b *TouchButton) setBounds(r core.Bounds) { b.bounds = r }

func (b *TouchButton) draw(d *core.Drawing, theme *Theme, state WidgetState) {
	rect := b.bounds
	w, h := float32(rect.Size.Width), float32(rect.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	cx := float32(rect.Origin.X) + w/2
	cy := float32(rect.Origin.Y) + h/2
	r := min(w, h) / 2

	fill := b.Background
	if fill == 0 {
		fill = core.RGBA(0xffffff, 0.14)
	}
	border := theme.Border
	textColor := b.TextColor
	if textColor == 0 {
		textColor = theme.Text
	}
	switch {
	case state.Disabled:
		fill = theme.Disabled
		textColor = theme.DisabledText
	case state.Pressed:
		fill = theme.Active
		border = theme.Accent
	case state.Hovered:
		fill = theme.Hover
	}

	circle := new(core.Path).Arc(cx, cy, r, 0, 2*math.Pi, false)
	d.SetFillColor(fill).FillPath(circle, core.NonZero)
	if border != 0 && border != core.Transparent {
		d.SetStrokeColor(border).SetLineWidth(max(1, theme.BorderWidth)).StrokePath(circle)
	}
	if state.Focused && !state.Disabled {
		drawFocusRing(d, rect, r, theme)
	}
	if b.Text != "" {
		font := resolveFont(b.Font, theme.Font, theme.TextScale)
		drawText(d, b.TextDrawer, theme, TextDrawParams{
			Text:     b.Text,
			X:        cx,
			Y:        cy,
			Font:     font,
			Color:    textColor,
			Align:    core.TextAlignCenter,
			Baseline: core.BaselineMiddle,
			Bounds:   rect,
		})
	}
}

// AnchoredItem pairs a child Widget with its anchor, offset, safe-area inset preference, and sizing mode inside a ScreenCanvas.
type AnchoredItem struct {
	ID          string
	Widget      Widget
	Anchor      ScreenAnchor
	Offset      ScreenOffsetDoc
	SafeArea    bool
	SizeMode    SizeMode
	FixedWidth  float32
	FixedHeight float32
	// PassThrough marks an unsized container whose children anchor directly against the parent's full bounds and safe area.
	PassThrough bool
}

// ScreenCanvas lays out children using nine-point anchors or fill, optional safe-area insets, and offsets.
// It implements Container and Widget.
type ScreenCanvas struct {
	SafeArea     core.Edges
	Items        []AnchoredItem
	Background   core.Color
	CornerRadius float32
	Width        float32
	Height       float32
	MinWidth     float32
	MinHeight    float32
	Flex         float32
	Align        Align
	Hidden       bool

	bounds core.Bounds
}

// Bounds returns the canvas's laid-out rectangle.
func (c *ScreenCanvas) Bounds() core.Bounds { return c.bounds }

// ChildWidgets returns the direct child widgets in render order.
func (c *ScreenCanvas) ChildWidgets() []Widget {
	if len(c.Items) == 0 {
		return nil
	}
	out := make([]Widget, 0, len(c.Items))
	for _, it := range c.Items {
		if it.Widget != nil {
			out = append(out, it.Widget)
		}
	}
	return out
}

func (c *ScreenCanvas) spec() layoutSpec {
	return layoutSpec{
		Width:     c.Width,
		Height:    c.Height,
		MinWidth:  c.MinWidth,
		MinHeight: c.MinHeight,
		Flex:      c.Flex,
		Align:     c.Align,
		Hidden:    c.Hidden,
	}
}

func (c *ScreenCanvas) measure(_ *Theme) core.Size {
	return applySizeConstraints(0, 0, c.spec())
}

func (c *ScreenCanvas) focusable() bool        { return false }
func (c *ScreenCanvas) childWidgets() []Widget { return c.ChildWidgets() }
func (c *ScreenCanvas) setBounds(r core.Bounds) {
	c.bounds = r
}

func (c *ScreenCanvas) draw(d *core.Drawing, _ *Theme, _ WidgetState) {
	b := c.bounds
	w, h := float32(b.Size.Width), float32(b.Size.Height)
	if w <= 0 || h <= 0 || c.Background == 0 || c.Background == core.Transparent {
		return
	}
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	d.SetFillColor(c.Background).FillPath(rectPath(x, y, w, h, max(0, c.CornerRadius)), core.NonZero)
}

func insetSafeBounds(bounds core.Bounds, safe core.Edges) core.Bounds {
	l := max(float32(0), float32(safe.Left))
	r := max(float32(0), float32(safe.Right))
	t := max(float32(0), float32(safe.Top))
	b := max(float32(0), float32(safe.Bottom))
	w := max(float32(0), float32(bounds.Size.Width)-l-r)
	h := max(float32(0), float32(bounds.Size.Height)-t-b)
	return core.Bounds{
		Origin: core.Point{
			X: core.Points(float32(bounds.Origin.X) + l),
			Y: core.Points(float32(bounds.Origin.Y) + t),
		},
		Size: core.Size{
			Width:  core.Points(w),
			Height: core.Points(h),
		},
	}
}

func anchorRect(bounds, safeBounds core.Bounds, useSafe bool, anchor ScreenAnchor, offset ScreenOffsetDoc, w, h float32) core.Bounds {
	ref := bounds
	if useSafe {
		ref = safeBounds
	}
	if anchor == AnchorFill {
		return ref
	}
	rx := float32(ref.Origin.X)
	ry := float32(ref.Origin.Y)
	rw := float32(ref.Size.Width)
	rh := float32(ref.Size.Height)

	if anchor == "" {
		anchor = AnchorTopLeft
	}

	var x, y float32
	switch anchor {
	case AnchorTopLeft, AnchorCenterLeft, AnchorBottomLeft:
		x = rx + offset.X
	case AnchorTopCenter, AnchorCenter, AnchorBottomCenter:
		x = rx + (rw-w)/2 + offset.X
	case AnchorTopRight, AnchorCenterRight, AnchorBottomRight:
		x = rx + rw - offset.X - w
	}

	switch anchor {
	case AnchorTopLeft, AnchorTopCenter, AnchorTopRight:
		y = ry + offset.Y
	case AnchorCenterLeft, AnchorCenter, AnchorCenterRight:
		y = ry + (rh-h)/2 + offset.Y
	case AnchorBottomLeft, AnchorBottomCenter, AnchorBottomRight:
		y = ry + rh - offset.Y - h
	}

	return core.Bounds{
		Origin: core.Point{X: core.Points(x), Y: core.Points(y)},
		Size:   core.Size{Width: core.Points(max(0, w)), Height: core.Points(max(0, h))},
	}
}

func (c *ScreenCanvas) layout(bounds core.Bounds, theme *Theme) {
	c.bounds = bounds
	safeBounds := insetSafeBounds(bounds, c.SafeArea)

	for _, it := range c.Items {
		if it.Widget == nil {
			continue
		}
		if it.Widget.spec().Hidden {
			zeroTreeBounds(it.Widget)
			continue
		}
		if it.PassThrough {
			if sub, ok := it.Widget.(*ScreenCanvas); ok {
				sub.SafeArea = c.SafeArea
			}
			it.Widget.layout(bounds, theme)
			continue
		}
		if it.Anchor == AnchorFill {
			target := bounds
			if it.SafeArea {
				target = safeBounds
			}
			it.Widget.layout(target, theme)
			continue
		}
		pref := it.Widget.measure(theme)
		w := float32(pref.Width)
		h := float32(pref.Height)
		if it.SizeMode != SizeFitText && it.FixedWidth > 0 {
			w = it.FixedWidth
		}
		if it.FixedHeight > 0 {
			h = it.FixedHeight
		}
		cb := anchorRect(bounds, safeBounds, it.SafeArea, it.Anchor, it.Offset, w, h)
		it.Widget.layout(cb, theme)
	}
}

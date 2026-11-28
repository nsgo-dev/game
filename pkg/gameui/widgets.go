package gameui

import "github.com/nsgo-dev/nsgo/core"

// ToggleStyle picks the visual presentation of a Toggle widget.
type ToggleStyle uint8

const (
	// ToggleCheckbox draws a square checkbox with a checkmark when checked.
	ToggleCheckbox ToggleStyle = iota
	// ToggleSwitch draws a sliding pill switch.
	ToggleSwitch
)

// Panel frames a single optional Child with padding, background fill, and border.
type Panel struct {
	Child          Widget
	Padding        core.Edges
	Align          Align
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Hidden         bool
	Background     core.Color
	Border         core.Color
	CornerRadius   float32 // 0 uses Theme.CornerRadius; < 0 uses square corners.
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice

	bounds core.Bounds
}

// Bounds returns the panel's laid-out rectangle.
func (p *Panel) Bounds() core.Bounds { return p.bounds }

// ChildWidgets returns the panel's child if non-nil.
func (p *Panel) ChildWidgets() []Widget {
	if p.Child == nil {
		return nil
	}
	return []Widget{p.Child}
}

func (p *Panel) spec() layoutSpec {
	return layoutSpec{
		Width:     p.Width,
		Height:    p.Height,
		MinWidth:  p.MinWidth,
		MinHeight: p.MinHeight,
		Flex:      p.Flex,
		Hidden:    p.Hidden,
	}
}
func (p *Panel) focusable() bool        { return false }
func (p *Panel) childWidgets() []Widget { return p.ChildWidgets() }
func (p *Panel) setBounds(b core.Bounds) {
	p.bounds = b
}

// VBox arranges Children vertically from top to bottom with Spacing and Padding.
type VBox struct {
	Children       []Widget
	Spacing        float32
	Padding        core.Edges
	Align          Align
	Justify        Justify
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Hidden         bool
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice

	bounds core.Bounds
}

// Bounds returns the VBox's laid-out rectangle.
func (b *VBox) Bounds() core.Bounds { return b.bounds }

// ChildWidgets returns the VBox's direct children.
func (b *VBox) ChildWidgets() []Widget { return b.Children }

func (b *VBox) spec() layoutSpec {
	return layoutSpec{
		Width:     b.Width,
		Height:    b.Height,
		MinWidth:  b.MinWidth,
		MinHeight: b.MinHeight,
		Flex:      b.Flex,
		Hidden:    b.Hidden,
	}
}
func (b *VBox) focusable() bool         { return false }
func (b *VBox) childWidgets() []Widget  { return b.Children }
func (b *VBox) setBounds(r core.Bounds) { b.bounds = r }

// HBox arranges Children horizontally from left to right with Spacing and Padding.
type HBox struct {
	Children       []Widget
	Spacing        float32
	Padding        core.Edges
	Align          Align
	Justify        Justify
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Hidden         bool
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice

	bounds core.Bounds
}

// Bounds returns the HBox's laid-out rectangle.
func (b *HBox) Bounds() core.Bounds { return b.bounds }

// ChildWidgets returns the HBox's direct children.
func (b *HBox) ChildWidgets() []Widget { return b.Children }

func (b *HBox) spec() layoutSpec {
	return layoutSpec{
		Width:     b.Width,
		Height:    b.Height,
		MinWidth:  b.MinWidth,
		MinHeight: b.MinHeight,
		Flex:      b.Flex,
		Hidden:    b.Hidden,
	}
}
func (b *HBox) focusable() bool         { return false }
func (b *HBox) childWidgets() []Widget  { return b.Children }
func (b *HBox) setBounds(r core.Bounds) { b.bounds = r }

// Label displays non-interactive text. Focus navigation skips labels automatically.
type Label struct {
	Text           string
	Color          core.Color
	Background     core.Color
	CornerRadius   float32
	Padding        core.Edges
	Font           core.Font
	TextAlign      core.TextAlign
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds core.Bounds
}

// Bounds returns the label's laid-out rectangle.
func (l *Label) Bounds() core.Bounds { return l.bounds }

func (l *Label) spec() layoutSpec {
	return layoutSpec{
		Width:     l.Width,
		Height:    l.Height,
		MinWidth:  l.MinWidth,
		MinHeight: l.MinHeight,
		Flex:      l.Flex,
		Align:     l.Align,
		Hidden:    l.Hidden,
	}
}
func (l *Label) focusable() bool         { return false }
func (l *Label) childWidgets() []Widget  { return nil }
func (l *Label) setBounds(r core.Bounds) { l.bounds = r }

// Button is a clickable and focusable action trigger.
type Button struct {
	Text           string
	OnClick        func()
	Disabled       bool
	Font           core.Font
	TextColor      core.Color
	Background     core.Color
	CornerRadius   float32
	Padding        core.Edges
	FocusOrder     int
	TouchHitSize   float32
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	Nav            NavOverrides
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds core.Bounds
}

// Bounds returns the button's laid-out rectangle.
func (b *Button) Bounds() core.Bounds { return b.bounds }

// HitBounds returns the button's interactive hit area, expanded to TouchHitSize when larger than Bounds.
func (b *Button) HitBounds() core.Bounds {
	return expandedHitBounds(b.bounds, b.TouchHitSize)
}

func (b *Button) spec() layoutSpec {
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
func (b *Button) focusable() bool                { return !b.Disabled && !b.Hidden }
func (b *Button) focusOrder() int                { return b.FocusOrder }
func (b *Button) hitTestPoint(p core.Point) bool { return b.HitBounds().Contains(p) }
func (b *Button) childWidgets() []Widget         { return nil }
func (b *Button) setBounds(r core.Bounds)        { b.bounds = r }

// Toggle is a focusable boolean checkbox or switch control.
type Toggle struct {
	Label          string
	Checked        bool
	Style          ToggleStyle
	OnChange       func(checked bool)
	Disabled       bool
	Font           core.Font
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	Nav            NavOverrides
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds core.Bounds
}

// Bounds returns the toggle's laid-out rectangle.
func (t *Toggle) Bounds() core.Bounds { return t.bounds }

func (t *Toggle) spec() layoutSpec {
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
func (t *Toggle) focusable() bool         { return !t.Disabled && !t.Hidden }
func (t *Toggle) childWidgets() []Widget  { return nil }
func (t *Toggle) setBounds(r core.Bounds) { t.bounds = r }

// Slider is a horizontal draggable and left/right steppable value control in [Min, Max].
// When Min and Max are both 0, the range defaults to [0, 1].
// When Step <= 0, keyboard and Adjust steps default to (Max - Min) / 10.
type Slider struct {
	Label          string
	Min            float32
	Max            float32
	Value          float32
	Step           float32
	OnChange       func(value float32)
	Disabled       bool
	Font           core.Font
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	Nav            NavOverrides
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds    core.Bounds
	trackRect core.Bounds
}

// Bounds returns the slider's laid-out rectangle.
func (s *Slider) Bounds() core.Bounds { return s.bounds }

// TrackBounds returns the slider's interactive track rectangle from the last Layout pass.
func (s *Slider) TrackBounds() core.Bounds { return s.trackRect }

// boundsRange returns the effective [min, max] range for the slider.
func (s *Slider) boundsRange() (float32, float32) {
	if s.Min == 0 && s.Max == 0 {
		return 0, 1
	}
	if s.Max < s.Min {
		return s.Max, s.Min
	}
	return s.Min, s.Max
}

// effectiveStep returns the step increment used by Adjust and keyboard left/right.
func (s *Slider) effectiveStep() float32 {
	if s.Step > 0 {
		return s.Step
	}
	minV, maxV := s.boundsRange()
	span := maxV - minV
	if span <= 0 {
		return 0
	}
	return span / 10
}

// Fraction returns the slider's current Value normalized into [0, 1].
func (s *Slider) Fraction() float32 {
	minV, maxV := s.boundsRange()
	span := maxV - minV
	if span <= 0 {
		return 0
	}
	return clampf((s.Value-minV)/span, 0, 1)
}

// SetValue clamps v into [Min, Max], updates Value, and invokes OnChange if Value changed.
func (s *Slider) SetValue(v float32) bool {
	minV, maxV := s.boundsRange()
	next := clampf(v, minV, maxV)
	if next == s.Value {
		return false
	}
	s.Value = next
	if s.OnChange != nil {
		s.OnChange(next)
	}
	return true
}

func (s *Slider) setFromX(px float32) bool {
	minV, maxV := s.boundsRange()
	tr := s.trackRect
	if tr.Size.Width <= 0 {
		tr = s.bounds
	}
	if tr.Size.Width <= 0 {
		return false
	}
	t := clampf((px-float32(tr.Origin.X))/float32(tr.Size.Width), 0, 1)
	return s.SetValue(minV + t*(maxV-minV))
}

func (s *Slider) spec() layoutSpec {
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
func (s *Slider) focusable() bool         { return !s.Disabled && !s.Hidden }
func (s *Slider) childWidgets() []Widget  { return nil }
func (s *Slider) setBounds(r core.Bounds) { s.bounds = r }

// ProgressBar displays a read-only progress meter in [Min, Max] (or [0, 1] when both Min and Max are 0).
type ProgressBar struct {
	Value          float32
	Min            float32
	Max            float32
	Label          string
	FillColor      core.Color
	TrackColor     core.Color
	Font           core.Font
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds core.Bounds
}

// Bounds returns the progress bar's laid-out rectangle.
func (p *ProgressBar) Bounds() core.Bounds { return p.bounds }

// Fraction returns Value normalized into [0, 1].
func (p *ProgressBar) Fraction() float32 {
	return progressFraction(p.Value, p.Min, p.Max)
}

func (p *ProgressBar) spec() layoutSpec {
	return layoutSpec{
		Width:     p.Width,
		Height:    p.Height,
		MinWidth:  p.MinWidth,
		MinHeight: p.MinHeight,
		Flex:      p.Flex,
		Align:     p.Align,
		Hidden:    p.Hidden,
	}
}
func (p *ProgressBar) focusable() bool         { return false }
func (p *ProgressBar) childWidgets() []Widget  { return nil }
func (p *ProgressBar) setBounds(r core.Bounds) { p.bounds = r }

// FillMode picks how an ImageProgress reveals its progress image as the value grows.
type FillMode uint8

const (
	// FillLeftToRight reveals the progress image from the left edge to the right (the default).
	FillLeftToRight FillMode = iota
	// FillRightToLeft reveals the progress image from the right edge to the left.
	FillRightToLeft
	// FillTopToBottom reveals the progress image from the top edge to the bottom.
	FillTopToBottom
	// FillBottomToTop reveals the progress image from the bottom edge to the top.
	FillBottomToTop
	// FillCenterHorizontal reveals the progress image from the horizontal center outward to both sides.
	FillCenterHorizontal
	// FillCenterVertical reveals the progress image from the vertical center outward to both edges.
	FillCenterVertical
	// FillClockwise reveals the progress image along a clockwise pie sweep from StartAngle.
	FillClockwise
	// FillCounterClockwise reveals the progress image along a counter-clockwise pie sweep from StartAngle.
	FillCounterClockwise
)

// String returns the screen-file name of m (such as "clockwise"), or "left_to_right" for an unknown mode.
func (m FillMode) String() string {
	switch m {
	case FillRightToLeft:
		return "right_to_left"
	case FillTopToBottom:
		return "top_to_bottom"
	case FillBottomToTop:
		return "bottom_to_top"
	case FillCenterHorizontal:
		return "center_h"
	case FillCenterVertical:
		return "center_v"
	case FillClockwise:
		return "clockwise"
	case FillCounterClockwise:
		return "counter_clockwise"
	default:
		return "left_to_right"
	}
}

// ParseFillMode maps a screen-file fill name (such as "clockwise") to its FillMode, reporting whether s was recognized.
func ParseFillMode(s string) (FillMode, bool) {
	switch s {
	case "left_to_right":
		return FillLeftToRight, true
	case "right_to_left":
		return FillRightToLeft, true
	case "top_to_bottom":
		return FillTopToBottom, true
	case "bottom_to_top":
		return FillBottomToTop, true
	case "center_h":
		return FillCenterHorizontal, true
	case "center_v":
		return FillCenterVertical, true
	case "clockwise":
		return FillClockwise, true
	case "counter_clockwise":
		return FillCounterClockwise, true
	default:
		return 0, false
	}
}

// radial reports whether m is one of the pie-sweep fill modes.
func (m FillMode) radial() bool {
	return m == FillClockwise || m == FillCounterClockwise
}

// ImageProgress displays a read-only progress meter in [Min, Max] (or [0, 1] when both Min and Max are 0)
// built from up to three layered images: Under drawn in full first, Progress revealed by the value along Fill,
// and Over drawn in full last (such as an ornate frame over a boss bar).
//
// Each layer is a NineSlice: a plain image when its insets are zero, or a nine-patch that keeps its cap sizes
// when the widget is wider or taller than the source. Linear fills clip the progress layer to the revealed
// rectangle, so a nine-slice progress image keeps its caps at any value; radial fills (FillClockwise and
// FillCounterClockwise) clip it to a pie from StartAngle degrees (0 points right, positive is clockwise)
// sweeping Span degrees (0 defaults to 360). When a layer is nil the widget falls back to a flat fill:
// TrackColor (or Theme.Track) for Under and FillColor (or Theme.Accent) for Progress.
type ImageProgress struct {
	Value      float32
	Min        float32
	Max        float32
	Under      *NineSlice
	Progress   *NineSlice
	Over       *NineSlice
	Fill       FillMode
	StartAngle float32 // degrees, 0 = right, positive clockwise; radial fills only
	Span       float32 // degrees, 0 defaults to 360; radial fills only
	FillColor  core.Color
	TrackColor core.Color
	Width      float32
	Height     float32
	MinWidth   float32
	MinHeight  float32
	Flex       float32
	Align      Align
	Hidden     bool

	bounds core.Bounds
}

// Bounds returns the image progress's laid-out rectangle.
func (p *ImageProgress) Bounds() core.Bounds { return p.bounds }

// Fraction returns Value normalized into [0, 1].
func (p *ImageProgress) Fraction() float32 {
	return progressFraction(p.Value, p.Min, p.Max)
}

func (p *ImageProgress) spec() layoutSpec {
	return layoutSpec{
		Width:     p.Width,
		Height:    p.Height,
		MinWidth:  p.MinWidth,
		MinHeight: p.MinHeight,
		Flex:      p.Flex,
		Align:     p.Align,
		Hidden:    p.Hidden,
	}
}
func (p *ImageProgress) focusable() bool         { return false }
func (p *ImageProgress) childWidgets() []Widget  { return nil }
func (p *ImageProgress) setBounds(r core.Bounds) { p.bounds = r }

// progressFraction normalizes value into [0, 1] over [min, max], defaulting a zero range to [0, 1] and swapping an inverted range.
func progressFraction(value, min, max float32) float32 {
	if min == 0 && max == 0 {
		min, max = 0, 1
	} else if max < min {
		min, max = max, min
	}
	span := max - min
	if span <= 0 {
		return 0
	}
	return clampf((value-min)/span, 0, 1)
}

// Spacer reserves empty space inside a VBox or HBox.
// A zero-value Spacer defaults to Flex: 1 so it expands to push siblings apart; setting Width or Height without Flex creates a fixed gap.
type Spacer struct {
	Width     float32
	Height    float32
	MinWidth  float32
	MinHeight float32
	Flex      float32
	Hidden    bool

	bounds core.Bounds
}

// Bounds returns the spacer's laid-out rectangle.
func (s *Spacer) Bounds() core.Bounds { return s.bounds }

func (s *Spacer) spec() layoutSpec {
	flex := s.Flex
	if flex == 0 && s.Width <= 0 && s.Height <= 0 {
		flex = 1
	}
	return layoutSpec{
		Width:     s.Width,
		Height:    s.Height,
		MinWidth:  s.MinWidth,
		MinHeight: s.MinHeight,
		Flex:      flex,
		Hidden:    s.Hidden,
	}
}
func (s *Spacer) focusable() bool         { return false }
func (s *Spacer) childWidgets() []Widget  { return nil }
func (s *Spacer) setBounds(r core.Bounds) { s.bounds = r }

// List is a focusable vertical list of selectable items supporting pointer and keyboard/gamepad selection.
type List struct {
	Items          []string
	Selected       int
	ItemHeight     float32
	Spacing        float32
	OnSelect       func(index int, item string)
	Disabled       bool
	Font           core.Font
	Width          float32
	Height         float32
	MinWidth       float32
	MinHeight      float32
	Flex           float32
	Align          Align
	Hidden         bool
	Nav            NavOverrides
	DrawBackground func(*core.Drawing, core.Bounds)
	FrameDrawer    FrameDrawer
	NineSlice      *NineSlice
	TextDrawer     TextDrawer

	bounds     core.Bounds
	itemRects  []core.Bounds
	hoveredOne int // 1-based item index; 0 means none
	pressedOne int // 1-based item index; 0 means none
}

// Bounds returns the list's laid-out rectangle.
func (l *List) Bounds() core.Bounds { return l.bounds }

// ItemBounds returns the laid-out rectangle for item at index, or zero Bounds if out of range.
func (l *List) ItemBounds(index int) core.Bounds {
	if index < 0 || index >= len(l.itemRects) {
		return core.Bounds{}
	}
	return l.itemRects[index]
}

// ItemAt returns the 0-based index of the item containing p, or -1 if none.
func (l *List) ItemAt(p core.Point) int {
	for i, r := range l.itemRects {
		if r.Contains(p) {
			return i
		}
	}
	return -1
}

// Select updates Selected to index (clamped into [0, len(Items)-1]) and invokes OnSelect.
// Returns false when the list is disabled or empty.
func (l *List) Select(index int) bool {
	if l.Disabled || len(l.Items) == 0 {
		return false
	}
	if index < 0 {
		index = 0
	}
	if index >= len(l.Items) {
		index = len(l.Items) - 1
	}
	l.Selected = index
	if l.OnSelect != nil {
		l.OnSelect(index, l.Items[index])
	}
	return true
}

func (l *List) effectiveItemHeight(theme *Theme) float32 {
	if l.ItemHeight > 0 {
		return l.ItemHeight
	}
	font := resolveFont(l.Font, theme.Font, theme.TextScale)
	return max(float32(font.Size)+theme.Padding, 28)
}

func (l *List) spec() layoutSpec {
	return layoutSpec{
		Width:     l.Width,
		Height:    l.Height,
		MinWidth:  l.MinWidth,
		MinHeight: l.MinHeight,
		Flex:      l.Flex,
		Align:     l.Align,
		Hidden:    l.Hidden,
	}
}
func (l *List) focusable() bool         { return !l.Disabled && !l.Hidden }
func (l *List) childWidgets() []Widget  { return nil }
func (l *List) setBounds(r core.Bounds) { l.bounds = r }

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

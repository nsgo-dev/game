package input

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const twoPi = float32(2 * math.Pi)

// VirtualStyle controls how VirtualStick and VirtualButton draw themselves into a core.Drawing.
//
// Zero fields fall back to DefaultVirtualStyle so VirtualStyle{} draws a visible default overlay.
type VirtualStyle struct {
	BaseFill           core.Color
	BaseStroke         core.Color
	BaseStrokeWidth    float32
	KnobFill           core.Color
	KnobActiveFill     core.Color
	KnobRadiusRatio    float32
	ButtonFill         core.Color
	ButtonPressedFill  core.Color
	ButtonStroke       core.Color
	ButtonStrokeWidth  float32
	ButtonCornerRadius float32
	LabelColor         core.Color
	LabelFont          core.Font
}

// DefaultVirtualStyle returns a translucent white-on-dark overlay style for virtual sticks and buttons.
func DefaultVirtualStyle() VirtualStyle {
	return VirtualStyle{
		BaseFill:           core.RGBA(0xffffff, 0.2),
		BaseStroke:         core.RGBA(0xffffff, 0.45),
		BaseStrokeWidth:    2,
		KnobFill:           core.RGBA(0xffffff, 0.55),
		KnobActiveFill:     core.RGBA(0xffffff, 0.85),
		KnobRadiusRatio:    0.4,
		ButtonFill:         core.RGBA(0xffffff, 0.22),
		ButtonPressedFill:  core.RGBA(0xffffff, 0.65),
		ButtonStroke:       core.RGBA(0xffffff, 0.5),
		ButtonStrokeWidth:  2,
		ButtonCornerRadius: 8,
		LabelColor:         core.RGB(0xffffff),
		LabelFont:          core.Font{Size: 14, Weight: core.FontWeightSemibold},
	}
}

func (s VirtualStyle) resolved() VirtualStyle {
	def := DefaultVirtualStyle()
	if s.BaseFill == 0 {
		s.BaseFill = def.BaseFill
	}
	if s.BaseStroke == 0 {
		s.BaseStroke = def.BaseStroke
	}
	if s.BaseStrokeWidth == 0 {
		s.BaseStrokeWidth = def.BaseStrokeWidth
	}
	if s.KnobFill == 0 {
		s.KnobFill = def.KnobFill
	}
	if s.KnobActiveFill == 0 {
		s.KnobActiveFill = def.KnobActiveFill
	}
	if s.KnobRadiusRatio <= 0 {
		s.KnobRadiusRatio = def.KnobRadiusRatio
	}
	if s.ButtonFill == 0 {
		s.ButtonFill = def.ButtonFill
	}
	if s.ButtonPressedFill == 0 {
		s.ButtonPressedFill = def.ButtonPressedFill
	}
	if s.ButtonStroke == 0 {
		s.ButtonStroke = def.ButtonStroke
	}
	if s.ButtonStrokeWidth == 0 {
		s.ButtonStrokeWidth = def.ButtonStrokeWidth
	}
	if s.LabelColor == 0 {
		s.LabelColor = def.LabelColor
	}
	if s.LabelFont == (core.Font{}) {
		s.LabelFont = def.LabelFont
	}
	return s
}

// VirtualStick is a fixed or floating on-screen thumbstick that captures a single touch finger and outputs normalized AxisX and AxisY values in [-1, 1] with radial dead-zone rescaling.
type VirtualStick struct {
	// Name optionally identifies the stick. When AxisXName or AxisYName is empty and Name is set, VirtualPad exposes them as Name+"_x" and Name+"_y".
	Name string
	// AxisXName and AxisYName name the horizontal and vertical axes exposed through VirtualPad.Axis and Actions.Axis.
	AxisXName string
	AxisYName string
	// Zone is the touch-down activation rectangle. On a fixed stick with an empty Zone, touches inside the circle (Center, Radius) activate it.
	Zone core.Bounds
	// Center is the resting base center of the stick.
	Center core.Point
	// Radius is the maximum travel radius of the knob in points.
	Radius float32
	// DeadZone is the radial dead-zone fraction in [0, 1). Displacements inside DeadZone*Radius yield 0; displacements beyond it rescale smoothly to [0, 1].
	DeadZone float32
	// Floating, when true, re-centers the stick's base on the touch-down point inside Zone and returns it to Center on release.
	Floating bool

	// AxisX and AxisY hold the stick's current normalized outputs in [-1, 1].
	AxisX float64
	AxisY float64

	active   bool
	fingerID int
	basePos  core.Point
	knobPos  core.Point
}

// Active reports whether a touch finger is currently captured by the stick.
func (s *VirtualStick) Active() bool {
	return s.active
}

// BasePosition returns the current center of the stick's base ring.
func (s *VirtualStick) BasePosition() core.Point {
	if s.active {
		return s.basePos
	}
	return s.Center
}

// KnobPosition returns the current center of the stick's thumb knob.
func (s *VirtualStick) KnobPosition() core.Point {
	if s.active {
		return s.knobPos
	}
	return s.Center
}

// Reset releases any captured finger and returns the stick to its neutral center.
func (s *VirtualStick) Reset() {
	s.active = false
	s.fingerID = 0
	s.AxisX = 0
	s.AxisY = 0
	s.basePos = s.Center
	s.knobPos = s.Center
}

// OnTouch feeds a touch event directly to the stick when used standalone without a VirtualPad.
func (s *VirtualStick) OnTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		if s.active {
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if hasChanged && !t.Changed {
				continue
			}
			if s.canStart(t.Position) {
				s.capture(t.ID, t.Position)
				return
			}
		}
	case core.TouchMove:
		if !s.active {
			return
		}
		for _, t := range ev.Touches {
			if t.ID == s.fingerID {
				s.update(t.Position)
				return
			}
		}
	case core.TouchEnd, core.TouchCancel:
		if !s.active {
			return
		}
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			s.Reset()
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if t.ID == s.fingerID && (t.Changed || !hasChanged) {
				s.Reset()
				return
			}
		}
	}
}

// Draw draws the stick's base ring and knob into d, leaving d's state unchanged.
func (s *VirtualStick) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil || s.Radius <= 0 {
		return
	}
	st := style.resolved()
	base := s.BasePosition()
	knob := s.KnobPosition()
	knobR := s.Radius * st.KnobRadiusRatio

	d.Save()
	d.BeginPath().
		Arc(float32(base.X), float32(base.Y), s.Radius, 0, twoPi, false).
		SetFillColor(st.BaseFill).
		Fill()
	if st.BaseStrokeWidth > 0 {
		d.SetStrokeColor(st.BaseStroke).
			SetLineWidth(st.BaseStrokeWidth).
			Stroke()
	}

	knobColor := st.KnobFill
	if s.active {
		knobColor = st.KnobActiveFill
	}
	d.BeginPath().
		Arc(float32(knob.X), float32(knob.Y), knobR, 0, twoPi, false).
		SetFillColor(knobColor).
		Fill()
	d.Restore()
}

func (s *VirtualStick) axisXKey() string {
	if s.AxisXName != "" {
		return s.AxisXName
	}
	if s.Name != "" {
		return s.Name + "_x"
	}
	return ""
}

func (s *VirtualStick) axisYKey() string {
	if s.AxisYName != "" {
		return s.AxisYName
	}
	if s.Name != "" {
		return s.Name + "_y"
	}
	return ""
}

func (s *VirtualStick) canStart(pos core.Point) bool {
	if s.Zone.Size.Width > 0 && s.Zone.Size.Height > 0 {
		return s.Zone.Contains(pos)
	}
	if !s.Floating && s.Radius > 0 {
		return pointDist(pos, s.Center) <= s.Radius
	}
	return false
}

func (s *VirtualStick) capture(id int, pos core.Point) {
	s.active = true
	s.fingerID = id
	if s.Floating {
		s.basePos = pos
	} else {
		s.basePos = s.Center
	}
	s.update(pos)
}

func (s *VirtualStick) update(pos core.Point) {
	if s.Radius <= 0 {
		s.knobPos = s.basePos
		s.AxisX = 0
		s.AxisY = 0
		return
	}
	dx := float64(pos.X - s.basePos.X)
	dy := float64(pos.Y - s.basePos.Y)
	dist := math.Hypot(dx, dy)
	r := float64(s.Radius)

	if dist > r {
		scale := r / dist
		s.knobPos = core.Point{
			X: s.basePos.X + core.Points(dx*scale),
			Y: s.basePos.Y + core.Points(dy*scale),
		}
	} else {
		s.knobPos = pos
	}

	norm := dist / r
	if norm > 1 {
		norm = 1
	}
	dz := float64(s.DeadZone)
	if dz < 0 {
		dz = 0
	}
	if dz >= 1 || norm <= dz || dist == 0 {
		s.AxisX = 0
		s.AxisY = 0
		return
	}
	scaled := (norm - dz) / (1 - dz)
	s.AxisX = (dx / dist) * scaled
	s.AxisY = (dy / dist) * scaled
}

// VirtualButton is a circular or rectangular on-screen button with multi-touch finger tracking.
type VirtualButton struct {
	// Center and Radius define a circular button when Radius > 0.
	Center core.Point
	Radius float32
	// Bounds defines a rectangular button when Radius <= 0.
	Bounds core.Bounds
	// Label is optional text drawn centered inside the button.
	Label string
	// Action is the action name driven by this button in VirtualPad and Actions.
	Action string

	fingers map[int]struct{}
}

// Pressed reports whether at least one finger is currently pressing the button.
func (b *VirtualButton) Pressed() bool {
	return len(b.fingers) > 0
}

// Contains reports whether p lies inside the button's circular or rectangular hit area.
func (b *VirtualButton) Contains(p core.Point) bool {
	if b.Radius > 0 {
		return pointDist(p, b.Center) <= b.Radius
	}
	if b.Bounds.Size.Width > 0 && b.Bounds.Size.Height > 0 {
		return b.Bounds.Contains(p)
	}
	return false
}

// Reset releases all fingers tracked by the button.
func (b *VirtualButton) Reset() {
	clear(b.fingers)
}

// OnTouch feeds a touch event directly to the button when used standalone without a VirtualPad.
func (b *VirtualButton) OnTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if hasChanged && !t.Changed {
				continue
			}
			if b.Contains(t.Position) {
				b.pressFinger(t.ID)
			}
		}
	case core.TouchMove:
		for _, t := range ev.Touches {
			if b.Contains(t.Position) {
				b.pressFinger(t.ID)
			} else {
				delete(b.fingers, t.ID)
			}
		}
	case core.TouchEnd, core.TouchCancel:
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			b.Reset()
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if t.Changed || !hasChanged {
				delete(b.fingers, t.ID)
			}
		}
	}
}

func (b *VirtualButton) pressFinger(id int) {
	if b.fingers == nil {
		b.fingers = make(map[int]struct{})
	}
	b.fingers[id] = struct{}{}
}

func (b *VirtualButton) updateOwnedFinger(id int, pos core.Point) {
	if b.Contains(pos) {
		b.pressFinger(id)
	} else {
		delete(b.fingers, id)
	}
}

func (b *VirtualButton) releaseFinger(id int) {
	delete(b.fingers, id)
}

// Draw draws the button and its optional label into d, leaving d's state unchanged.
func (b *VirtualButton) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil {
		return
	}
	st := style.resolved()
	fill := st.ButtonFill
	if b.Pressed() {
		fill = st.ButtonPressedFill
	}

	var lx, ly float32
	switch {
	case b.Radius > 0:
		cx, cy := float32(b.Center.X), float32(b.Center.Y)
		lx, ly = cx, cy
		d.Save()
		d.BeginPath().
			Arc(cx, cy, b.Radius, 0, twoPi, false).
			SetFillColor(fill).
			Fill()
	case b.Bounds.Size.Width > 0 && b.Bounds.Size.Height > 0:
		x := float32(b.Bounds.Origin.X)
		y := float32(b.Bounds.Origin.Y)
		w := float32(b.Bounds.Size.Width)
		h := float32(b.Bounds.Size.Height)
		lx, ly = x+w/2, y+h/2
		d.Save()
		d.BeginPath()
		if st.ButtonCornerRadius > 0 {
			d.RoundRect(x, y, w, h, st.ButtonCornerRadius)
		} else {
			d.Rect(x, y, w, h)
		}
		d.SetFillColor(fill).Fill()
	default:
		return
	}

	if st.ButtonStrokeWidth > 0 {
		d.SetStrokeColor(st.ButtonStroke).
			SetLineWidth(st.ButtonStrokeWidth).
			Stroke()
	}
	if b.Label != "" {
		d.SetFillColor(st.LabelColor).
			SetTextAlign(core.TextAlignCenter).
			SetTextBaseline(core.BaselineMiddle).
			FillText(b.Label, lx, ly, st.LabelFont)
	}
	d.Restore()
}

// VirtualPad owns a collection of VirtualSticks and VirtualButtons, routes multi-touch fingers without controls stealing each other's touches, optionally wires into Actions, and draws all controls into a core.Drawing.
type VirtualPad struct {
	// MapPoint, when set, transforms touch positions from view coordinates before hit-testing and updating sticks and buttons. Returning false ignores the touch point.
	MapPoint func(core.Point) (core.Point, bool)
	Sticks   []*VirtualStick
	Buttons  []*VirtualButton

	owners  map[int]any
	actions *Actions
}

// NewVirtualPad returns an empty VirtualPad.
func NewVirtualPad() *VirtualPad {
	return &VirtualPad{}
}

// AddStick adds s to p and returns s.
func (p *VirtualPad) AddStick(s *VirtualStick) *VirtualStick {
	if s != nil {
		p.Sticks = append(p.Sticks, s)
	}
	return s
}

// AddButton adds b to p and returns b.
func (p *VirtualPad) AddButton(b *VirtualButton) *VirtualButton {
	if b != nil {
		p.Buttons = append(p.Buttons, b)
	}
	return b
}

// BindActions connects p to a (or disconnects from any previous Actions when a is nil).
func (p *VirtualPad) BindActions(a *Actions) {
	if p.actions != nil && p.actions != a {
		p.actions.UnbindVirtualPad(p)
	}
	if a != nil {
		a.BindVirtualPad(p)
	}
}

// Pressed reports whether any button in p bound to action is currently pressed.
func (p *VirtualPad) Pressed(action string) bool {
	if action == "" {
		return false
	}
	for _, b := range p.Buttons {
		if b != nil && b.Action == action && b.Pressed() {
			return true
		}
	}
	return false
}

// Axis returns the current axis value in [-1, 1] for name from p's sticks (or 1 if a button with Action == name is pressed).
func (p *VirtualPad) Axis(name string) float64 {
	if name == "" {
		return 0
	}
	var sum float64
	var matched bool
	for _, s := range p.Sticks {
		if s == nil {
			continue
		}
		if s.axisXKey() == name {
			sum += s.AxisX
			matched = true
		}
		if s.axisYKey() == name {
			sum += s.AxisY
			matched = true
		}
	}
	if matched {
		return clampAxis(sum)
	}
	if p.Pressed(name) {
		return 1
	}
	return 0
}

// Reset releases all active sticks, buttons, and tracked finger ownership.
func (p *VirtualPad) Reset() {
	clear(p.owners)
	for _, s := range p.Sticks {
		if s != nil {
			s.Reset()
		}
	}
	for _, b := range p.Buttons {
		if b != nil {
			b.Reset()
		}
	}
}

// OnTouch routes ev to p's buttons and sticks so each touch finger stays owned by the control that captured it.
func (p *VirtualPad) OnTouch(ev core.TouchEvent) {
	p.handleTouch(ev, nil)
	if p.actions != nil {
		p.actions.syncRaw()
	}
}

// Attach wires p.OnTouch onto v, chaining any existing v.OnTouch callback.
func (p *VirtualPad) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { p.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = p.OnTouch
	}
}

// Draw draws all sticks and buttons in p into d.
func (p *VirtualPad) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil {
		return
	}
	for _, s := range p.Sticks {
		if s != nil {
			s.Draw(d, style)
		}
	}
	for _, b := range p.Buttons {
		if b != nil {
			b.Draw(d, style)
		}
	}
}

func (p *VirtualPad) handleTouch(ev core.TouchEvent, fallbackMap func(core.Point) (core.Point, bool)) {
	mapFn := p.MapPoint
	if mapFn == nil {
		mapFn = fallbackMap
	}
	mapPoint := func(pt core.Point) (core.Point, bool) {
		if mapFn == nil {
			return pt, true
		}
		return mapFn(pt)
	}

	switch ev.Phase {
	case core.TouchStart:
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if hasChanged && !t.Changed {
				continue
			}
			if _, owned := p.owners[t.ID]; owned {
				continue
			}
			pos, ok := mapPoint(t.Position)
			if !ok {
				continue
			}
			if owner := p.claimFinger(t.ID, pos); owner != nil {
				if p.owners == nil {
					p.owners = make(map[int]any)
				}
				p.owners[t.ID] = owner
			}
		}
	case core.TouchMove:
		for _, t := range ev.Touches {
			owner, ok := p.owners[t.ID]
			if !ok {
				continue
			}
			pos, mapped := mapPoint(t.Position)
			if !mapped {
				continue
			}
			switch c := owner.(type) {
			case *VirtualStick:
				c.update(pos)
			case *VirtualButton:
				c.updateOwnedFinger(t.ID, pos)
			}
		}
	case core.TouchEnd, core.TouchCancel:
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			p.Reset()
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if !t.Changed && hasChanged {
				continue
			}
			owner, ok := p.owners[t.ID]
			if !ok {
				continue
			}
			switch c := owner.(type) {
			case *VirtualStick:
				c.Reset()
			case *VirtualButton:
				c.releaseFinger(t.ID)
			}
			delete(p.owners, t.ID)
		}
	}
}

func (p *VirtualPad) claimFinger(id int, pos core.Point) any {
	// Buttons take priority over broader stick activation zones.
	for _, b := range p.Buttons {
		if b != nil && b.Contains(pos) {
			b.pressFinger(id)
			return b
		}
	}
	for _, s := range p.Sticks {
		if s != nil && !s.active && s.canStart(pos) {
			s.capture(id, pos)
			return s
		}
	}
	return nil
}

func touchesHaveChanged(touches []core.Touch) bool {
	for _, t := range touches {
		if t.Changed {
			return true
		}
	}
	return false
}

func pointDist(a, b core.Point) float32 {
	return float32(math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)))
}

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
	// UpPositive, when true, makes AxisY positive while the knob is pushed up, as a game controller's stick reads with GamepadAxisOptions.Invert. By default AxisY follows screen coordinates and is positive while the knob is pushed down.
	UpPositive bool

	// AxisX and AxisY hold the stick's current normalized outputs in [-1, 1]. AxisX is positive to the right; AxisY is positive downward, or upward with UpPositive.
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
	if s.UpPositive {
		s.AxisY = -s.AxisY
	}
}

// VirtualButtonMode selects how a VirtualButton turns the fingers on it into its pressed state.
type VirtualButtonMode uint8

const (
	// ButtonHold presses the button while at least one finger is on it. It is the zero value.
	ButtonHold VirtualButtonMode = iota
	// ButtonToggle flips the button between pressed and released on each touch-down, and keeps that state after the finger lifts until the next touch-down or Reset.
	ButtonToggle
	// ButtonTap presses the button once per touch-down, however long the finger stays: through Actions its action is pressed for exactly one Step. A finger sliding onto it from another button in a VirtualPad with SlideBetweenButtons counts as a touch-down.
	ButtonTap
)

// VirtualIcon draws a glyph into frame, the area inside a control it decorates, in color. It runs inside the control's Draw, so the drawing state it changes is restored afterwards.
type VirtualIcon func(d *core.Drawing, frame core.Bounds, color core.Color)

// VirtualButton is a circular or rectangular on-screen button with multi-touch finger tracking.
type VirtualButton struct {
	// Center and Radius define a circular button when Radius > 0.
	Center core.Point
	Radius float32
	// Bounds defines a rectangular button when Radius <= 0.
	Bounds core.Bounds
	// Label is optional text drawn centered inside the button.
	Label string
	// Icon, when set, is drawn centered inside the button in place of Label.
	Icon VirtualIcon
	// Action is the action name driven by this button in VirtualPad and Actions.
	Action string
	// Mode selects hold (the default), toggle or tap behavior.
	Mode VirtualButtonMode

	fingers map[int]struct{}
	toggled bool
	tapped  bool
}

// Pressed reports whether the button is pressed: while a finger is on it in ButtonHold mode, while toggled on in ButtonToggle mode, and from a touch-down until the next Actions.Step in ButtonTap mode.
func (b *VirtualButton) Pressed() bool {
	switch b.Mode {
	case ButtonToggle:
		return b.toggled
	case ButtonTap:
		return b.tapped
	default:
		return len(b.fingers) > 0
	}
}

// Held reports whether at least one finger is on the button, whatever its Mode.
func (b *VirtualButton) Held() bool {
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

// Reset releases all fingers tracked by the button, turns a toggled button off and drops a pending tap.
func (b *VirtualButton) Reset() {
	clear(b.fingers)
	b.toggled = false
	b.tapped = false
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
				b.touchDown(t.ID)
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
			b.releaseFingers()
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

// touchDown records finger id going down on the button: it holds the button, flips a toggle and starts a tap.
func (b *VirtualButton) touchDown(id int) {
	if _, ok := b.fingers[id]; ok {
		return
	}
	b.pressFinger(id)
	switch b.Mode {
	case ButtonToggle:
		b.toggled = !b.toggled
	case ButtonTap:
		b.tapped = true
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

// releaseFingers lifts every finger, keeping a toggle's state and a pending tap.
func (b *VirtualButton) releaseFingers() {
	clear(b.fingers)
}

// frame returns the square or rectangle the button's hit area fills.
func (b *VirtualButton) frame() core.Bounds {
	if b.Radius > 0 {
		r := core.Points(b.Radius)
		return core.Bounds{Origin: core.Point{X: b.Center.X - r, Y: b.Center.Y - r}, Size: core.Size{Width: 2 * r, Height: 2 * r}}
	}
	return b.Bounds
}

// Draw draws the button and its optional icon or label into d, leaving d's state unchanged.
func (b *VirtualButton) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil {
		return
	}
	st := style.resolved()
	fill := st.ButtonFill
	if b.Held() || (b.Mode == ButtonToggle && b.toggled) {
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
	switch {
	case b.Icon != nil:
		d.Save()
		b.Icon(d, insetFrame(b.frame(), 0.25), st.LabelColor)
		d.Restore()
	case b.Label != "":
		d.SetFillColor(st.LabelColor).
			SetTextAlign(core.TextAlignCenter).
			SetTextBaseline(core.BaselineMiddle).
			FillText(b.Label, lx, ly, st.LabelFont)
	}
	d.Restore()
}

// insetFrame shrinks f by fraction of its width and height on each side.
func insetFrame(f core.Bounds, fraction float32) core.Bounds {
	dx := f.Size.Width * core.Points(fraction)
	dy := f.Size.Height * core.Points(fraction)
	return core.Bounds{
		Origin: core.Point{X: f.Origin.X + dx, Y: f.Origin.Y + dy},
		Size:   core.Size{Width: f.Size.Width - 2*dx, Height: f.Size.Height - 2*dy},
	}
}

// VirtualControl is an on-screen control a VirtualPad routes touches to beside its sticks and buttons, such as a VirtualDPad or a control kind a plugin registers with RegisterTouchControlKind.
//
// The pad hands each new finger to the first control whose Claim takes it and keeps sending that finger to the same control, wherever it moves, until it lifts.
type VirtualControl interface {
	// Claim reports whether finger id, touching down at p, starts the control; a control that returns true owns the finger until Release.
	Claim(id int, p core.Point) bool
	// Move reports that owned finger id moved to p.
	Move(id int, p core.Point)
	// Release reports that owned finger id lifted or was cancelled.
	Release(id int)
	// Reset releases every finger and returns the control to rest.
	Reset()
	// ActionNames returns the names of the actions the control drives.
	ActionNames() []string
	// ActionPressed reports whether the control currently presses action name.
	ActionPressed(name string) bool
	// AxisValues calls yield with the name and current value in [-1, 1] of every axis the control drives, zero values included.
	AxisValues(yield func(name string, value float64))
	// Draw draws the control into d, leaving d's state unchanged.
	Draw(d *core.Drawing, style VirtualStyle)
}

// ButtonControl is a VirtualControl that is a VirtualButton underneath, such as a plugin's custom-drawn or cooldown button that wraps one. A VirtualPad treats it as one of its buttons: a finger touching down on it is claimed before sticks and other controls, follows it and, with SlideBetweenButtons, slides onto and off it; its ButtonTap press ends after one Actions.Step; and TouchControls names it as a button for assistive technology. The pad drives the VirtualButton itself, so the control's Claim, Move and Release are not called for its fingers.
type ButtonControl interface {
	VirtualControl
	// Button returns the VirtualButton the control presses, the same one on every call.
	Button() *VirtualButton
}

// VirtualPad owns a collection of VirtualSticks, VirtualButtons and other VirtualControls, routes multi-touch fingers without controls stealing each other's touches, optionally wires into Actions, and draws all controls into a core.Drawing.
type VirtualPad struct {
	// MapPoint, when set, transforms touch positions from view coordinates before hit-testing and updating sticks and buttons. Returning false ignores the touch point.
	MapPoint func(core.Point) (core.Point, bool)
	Sticks   []*VirtualStick
	Buttons  []*VirtualButton
	// Controls holds the pad's other controls, such as VirtualDPads.
	Controls []VirtualControl
	// SlideBetweenButtons, when true, hands a finger that slides off one button onto another to that button, so a thumb can rock between neighbouring buttons without lifting. A ButtonToggle button is never entered this way. Fingers owned by sticks and other controls never move to a button.
	SlideBetweenButtons bool
	// OnPress, when set, is called each time a finger presses something: touching down on a button, sliding onto another button, or a control whose Claim or Move newly presses one of its actions. It suits a haptic tick.
	OnPress func()

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

// AddControl adds c to p and returns c.
func (p *VirtualPad) AddControl(c VirtualControl) VirtualControl {
	if c != nil {
		p.Controls = append(p.Controls, c)
	}
	return c
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

// Pressed reports whether any button or control in p bound to action is currently pressing it.
func (p *VirtualPad) Pressed(action string) bool {
	if action == "" {
		return false
	}
	for _, b := range p.Buttons {
		if b != nil && b.Action == action && b.Pressed() {
			return true
		}
	}
	for _, c := range p.Controls {
		if c != nil && c.ActionPressed(action) {
			return true
		}
	}
	return false
}

// Axis returns the current axis value in [-1, 1] for name from p's sticks and controls (or 1 if a button with Action == name is pressed).
func (p *VirtualPad) Axis(name string) float64 {
	if name == "" {
		return 0
	}
	var sum float64
	var matched bool
	p.eachAxis(func(axis string, v float64) {
		if axis == name {
			sum += v
			matched = true
		}
	})
	if matched {
		return clampAxis(sum)
	}
	if p.Pressed(name) {
		return 1
	}
	return 0
}

// Reset releases all active sticks, buttons and controls, turns toggled buttons off and clears tracked finger ownership.
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
	for _, c := range p.Controls {
		if c != nil {
			c.Reset()
		}
	}
}

// OnTouch routes ev to p's buttons, controls and sticks so each touch finger stays owned by the control that captured it.
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

// Draw draws all sticks, controls and buttons in p into d.
func (p *VirtualPad) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil {
		return
	}
	for _, s := range p.Sticks {
		if s != nil {
			s.Draw(d, style)
		}
	}
	for _, c := range p.Controls {
		if c != nil {
			c.Draw(d, style)
		}
	}
	for _, b := range p.Buttons {
		if b != nil {
			b.Draw(d, style)
		}
	}
}

// eachAction calls fn with the action name of every button and control in p; a name may repeat and may be empty.
func (p *VirtualPad) eachAction(fn func(string)) {
	for _, b := range p.Buttons {
		if b != nil {
			fn(b.Action)
		}
	}
	for _, c := range p.Controls {
		if c != nil {
			for _, name := range c.ActionNames() {
				fn(name)
			}
		}
	}
}

// eachAxis calls fn with the name and value of every axis p's sticks and controls drive; a name may repeat and may be empty.
func (p *VirtualPad) eachAxis(fn func(string, float64)) {
	for _, s := range p.Sticks {
		if s != nil {
			fn(s.axisXKey(), s.AxisX)
			fn(s.axisYKey(), s.AxisY)
		}
	}
	for _, c := range p.Controls {
		if c != nil {
			c.AxisValues(fn)
		}
	}
}

// endStep ends the taps the step just sampled, so a ButtonTap button is pressed for one Actions.Step per touch-down.
func (p *VirtualPad) endStep() {
	for _, b := range p.Buttons {
		if b != nil {
			b.tapped = false
		}
	}
	for _, c := range p.Controls {
		if bc, ok := c.(ButtonControl); ok {
			bc.Button().tapped = false
		}
	}
}

// active reports whether any finger is on one of p's controls.
func (p *VirtualPad) active() bool {
	return len(p.owners) > 0
}

func (p *VirtualPad) pressed() {
	if p.OnPress != nil {
		p.OnPress()
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
				p.moveButtonFinger(t.ID, c, pos)
			case ButtonControl:
				p.moveButtonFinger(t.ID, c.Button(), pos)
			case VirtualControl:
				if p.OnPress == nil {
					c.Move(t.ID, pos)
					continue
				}
				before := pressedMask(c)
				c.Move(t.ID, pos)
				if pressedMask(c)&^before != 0 {
					p.pressed()
				}
			}
		}
	case core.TouchEnd, core.TouchCancel:
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			p.releaseAll()
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
			releaseOwner(owner, t.ID)
			delete(p.owners, t.ID)
		}
	}
}

// releaseAll lifts every finger, as a cancel of all touches does, keeping toggled buttons and pending taps.
func (p *VirtualPad) releaseAll() {
	for id, owner := range p.owners {
		releaseOwner(owner, id)
	}
	clear(p.owners)
	for _, s := range p.Sticks {
		if s != nil {
			s.Reset()
		}
	}
	for _, b := range p.Buttons {
		if b != nil {
			b.releaseFingers()
		}
	}
}

func releaseOwner(owner any, id int) {
	switch c := owner.(type) {
	case *VirtualStick:
		c.Reset()
	case *VirtualButton:
		c.releaseFinger(id)
	case ButtonControl:
		c.Button().releaseFinger(id)
	case VirtualControl:
		c.Release(id)
	}
}

// moveButtonFinger follows finger id, owned by button b, to pos: it holds b while the finger is inside it and, with SlideBetweenButtons, hands the finger to another button it slides onto.
func (p *VirtualPad) moveButtonFinger(id int, b *VirtualButton, pos core.Point) {
	if b.Contains(pos) {
		b.pressFinger(id)
		return
	}
	b.releaseFinger(id)
	if !p.SlideBetweenButtons {
		return
	}
	next, owner := p.buttonAt(pos, b)
	if next == nil {
		return
	}
	next.touchDown(id)
	p.owners[id] = owner
	p.pressed()
}

// buttonAt returns the first button other than except that contains pos and accepts a sliding finger, and the value p.owners records for it.
func (p *VirtualPad) buttonAt(pos core.Point, except *VirtualButton) (*VirtualButton, any) {
	for _, b := range p.Buttons {
		if b != nil && b != except && b.Mode != ButtonToggle && b.Contains(pos) {
			return b, b
		}
	}
	for _, c := range p.Controls {
		if bc, ok := c.(ButtonControl); ok {
			if b := bc.Button(); b != except && b.Mode != ButtonToggle && b.Contains(pos) {
				return b, bc
			}
		}
	}
	return nil, nil
}

func (p *VirtualPad) claimFinger(id int, pos core.Point) any {
	// Buttons take priority over broader stick activation zones and other controls.
	for _, b := range p.Buttons {
		if b != nil && b.Contains(pos) {
			b.touchDown(id)
			p.pressed()
			return b
		}
	}
	for _, c := range p.Controls {
		if bc, ok := c.(ButtonControl); ok && bc.Button().Contains(pos) {
			bc.Button().touchDown(id)
			p.pressed()
			return bc
		}
	}
	for _, c := range p.Controls {
		if c == nil {
			continue
		}
		if _, ok := c.(ButtonControl); ok {
			continue
		}
		var before uint64
		if p.OnPress != nil {
			before = pressedMask(c)
		}
		if c.Claim(id, pos) {
			if p.OnPress != nil && pressedMask(c)&^before != 0 {
				p.pressed()
			}
			return c
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

// pressedMask returns a bit for each of the first 64 of c's actions that c currently presses.
func pressedMask(c VirtualControl) uint64 {
	var m uint64
	for i, name := range c.ActionNames() {
		if i >= 64 {
			break
		}
		if name != "" && c.ActionPressed(name) {
			m |= 1 << i
		}
	}
	return m
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

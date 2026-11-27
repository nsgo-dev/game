package input

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

// touchFadeDuration is how long TouchControls takes to fade between fully opaque and IdleAlpha.
const touchFadeDuration = 250 * time.Millisecond

// TouchControl is one control a TouchControls layer builds from a TouchControlSpec: a VirtualControl that the layer also places on screen. The kinds this package registers build a VirtualButton, VirtualDPad or VirtualStick; a plugin implements TouchControl for a kind of its own and registers it with RegisterTouchControlKind.
type TouchControl interface {
	VirtualControl
	// Place moves and sizes the control to frame, in the view's points, as TouchControls computes it from the spec's anchor, offset and size; scale is the view points per reference point applied, for other lengths the spec gives such as a stick's radius. TouchControls calls it on every Resize, SetLayout and SetMirrored.
	Place(frame core.Bounds, scale float32)
}

// AccessibleControl is an optional interface a TouchControl implements to describe itself to assistive technology. TouchControls.Draw names a control that implements it with the role and selected state it returns, a ButtonControl as a button selected while pressed, and any other control as a group.
type AccessibleControl interface {
	// Accessibility returns the control's role, such as core.RoleSwitch for a switch, and whether it is selected, such as that switch being on.
	Accessibility() (role core.Role, selected bool)
}

// TouchControlFactory builds a control from its spec. It returns an error for a spec it cannot build, such as an option it does not know, which NewTouchControls reports against the control.
type TouchControlFactory func(spec TouchControlSpec) (TouchControl, error)

var touchRegistry = struct {
	sync.RWMutex
	kinds map[string]TouchControlFactory
	icons map[string]VirtualIcon
}{kinds: map[string]TouchControlFactory{}, icons: map[string]VirtualIcon{}}

// RegisterTouchControlKind makes kind a control kind TouchLayout documents can name, built by factory. Call it from an init function, as database/sql drivers register; it panics when kind is empty, factory is nil or kind is already registered, button, dpad and stick included.
func RegisterTouchControlKind(kind string, factory TouchControlFactory) {
	if kind == "" || factory == nil {
		panic("input: RegisterTouchControlKind needs a kind and a factory")
	}
	touchRegistry.Lock()
	defer touchRegistry.Unlock()
	if _, dup := touchRegistry.kinds[kind]; dup {
		panic(fmt.Sprintf("input: touch control kind %q registered twice", kind))
	}
	touchRegistry.kinds[kind] = factory
}

// TouchControlKinds returns the sorted names of every registered control kind.
func TouchControlKinds() []string {
	touchRegistry.RLock()
	defer touchRegistry.RUnlock()
	return slices.Sorted(maps.Keys(touchRegistry.kinds))
}

// RegisterTouchIcon makes name an icon a button's spec can name in its icon field. This package registers arrowUp, arrowDown, arrowLeft and arrowRight. It panics when name is empty, icon is nil or name is already registered.
func RegisterTouchIcon(name string, icon VirtualIcon) {
	if name == "" || icon == nil {
		panic("input: RegisterTouchIcon needs a name and an icon")
	}
	touchRegistry.Lock()
	defer touchRegistry.Unlock()
	if _, dup := touchRegistry.icons[name]; dup {
		panic(fmt.Sprintf("input: touch icon %q registered twice", name))
	}
	touchRegistry.icons[name] = icon
}

// TouchIcon returns the icon registered as name.
func TouchIcon(name string) (VirtualIcon, bool) {
	touchRegistry.RLock()
	defer touchRegistry.RUnlock()
	icon, ok := touchRegistry.icons[name]
	return icon, ok
}

func init() {
	RegisterTouchControlKind(TouchKindButton, newTouchButton)
	RegisterTouchControlKind(TouchKindDPad, newTouchDPad)
	RegisterTouchControlKind(TouchKindStick, newTouchStick)
	for _, a := range dpadArrows {
		name := map[DPadDirection]string{DPadUp: "arrowUp", DPadDown: "arrowDown", DPadLeft: "arrowLeft", DPadRight: "arrowRight"}[a.dir]
		dx, dy := a.dx, a.dy
		RegisterTouchIcon(name, func(d *core.Drawing, frame core.Bounds, color core.Color) {
			size := float32(min(frame.Size.Width, frame.Size.Height)) / 2
			drawArrow(d, float32(frame.Origin.X+frame.Size.Width/2), float32(frame.Origin.Y+frame.Size.Height/2), dx, dy, size, color)
		})
	}
}

// touchButton is a button TouchControls builds: a VirtualButton, round or rectangular.
type touchButton struct {
	*VirtualButton
	rect bool
}

func newTouchButton(spec TouchControlSpec) (TouchControl, error) {
	b := &VirtualButton{Action: spec.Action, Label: spec.Text}
	switch spec.Mode {
	case TouchModeToggle:
		b.Mode = ButtonToggle
	case TouchModeTap:
		b.Mode = ButtonTap
	}
	if spec.Icon != "" {
		icon, ok := TouchIcon(spec.Icon)
		if !ok {
			return nil, loaderr.Errorf("icon", "name an icon registered with RegisterTouchIcon, such as arrowLeft", "unknown icon %q", spec.Icon)
		}
		b.Icon = icon
	}
	return &touchButton{VirtualButton: b, rect: spec.Shape == TouchShapeRect}, nil
}

func (b *touchButton) Button() *VirtualButton { return b.VirtualButton }

func (b *touchButton) Claim(id int, p core.Point) bool {
	if !b.Contains(p) {
		return false
	}
	b.touchDown(id)
	return true
}

func (b *touchButton) Move(id int, p core.Point) { b.updateOwnedFinger(id, p) }

func (b *touchButton) Release(id int) { b.releaseFinger(id) }

func (b *touchButton) ActionNames() []string { return []string{b.Action} }

func (b *touchButton) ActionPressed(name string) bool {
	return name != "" && name == b.Action && b.Pressed()
}

func (b *touchButton) AxisValues(func(string, float64)) {}

func (b *touchButton) Place(frame core.Bounds, _ float32) {
	if b.rect {
		b.Radius = 0
		b.Bounds = frame
		return
	}
	b.Center = frameCenter(frame)
	b.Radius = float32(min(frame.Size.Width, frame.Size.Height)) / 2
}

// touchDPad is a d-pad TouchControls builds.
type touchDPad struct {
	*VirtualDPad
}

func newTouchDPad(spec TouchControlSpec) (TouchControl, error) {
	return &touchDPad{&VirtualDPad{
		Ways:      spec.Ways,
		DeadZone:  float32(spec.DeadZone),
		Up:        spec.Up,
		Down:      spec.Down,
		Left:      spec.Left,
		Right:     spec.Right,
		AxisXName: spec.AxisX,
		AxisYName: spec.AxisY,
	}}, nil
}

func (p *touchDPad) Place(frame core.Bounds, _ float32) {
	p.Center = frameCenter(frame)
	p.Radius = float32(min(frame.Size.Width, frame.Size.Height)) / 2
}

// touchStick is a stick TouchControls builds, with an up-positive Y axis; radius is its knob travel in reference points, 0 for half its frame's smaller side.
type touchStick struct {
	*VirtualStick
	radius float32
}

func newTouchStick(spec TouchControlSpec) (TouchControl, error) {
	return &touchStick{
		VirtualStick: &VirtualStick{
			AxisXName:  spec.AxisX,
			AxisYName:  spec.AxisY,
			DeadZone:   float32(spec.DeadZone),
			Floating:   spec.Floating,
			UpPositive: true,
		},
		radius: float32(spec.Radius),
	}, nil
}

func (s *touchStick) Claim(id int, p core.Point) bool {
	if s.active || !s.canStart(p) {
		return false
	}
	s.capture(id, p)
	return true
}

func (s *touchStick) Move(id int, p core.Point) {
	if s.active && id == s.fingerID {
		s.update(p)
	}
}

func (s *touchStick) Release(id int) {
	if s.active && id == s.fingerID {
		s.Reset()
	}
}

func (s *touchStick) ActionNames() []string { return nil }

func (s *touchStick) ActionPressed(string) bool { return false }

func (s *touchStick) AxisValues(yield func(string, float64)) {
	if name := s.axisXKey(); name != "" {
		yield(name, s.AxisX)
	}
	if name := s.axisYKey(); name != "" {
		yield(name, s.AxisY)
	}
}

func (s *touchStick) Place(frame core.Bounds, scale float32) {
	s.Center = frameCenter(frame)
	s.Radius = float32(min(frame.Size.Width, frame.Size.Height)) / 2
	if s.radius > 0 {
		s.Radius = s.radius * scale
	}
	// A floating stick starts anywhere in its frame; a fixed one inside its circle.
	s.Zone = core.Bounds{}
	if s.Floating {
		s.Zone = frame
	}
}

func frameCenter(f core.Bounds) core.Point {
	return core.Point{X: f.Origin.X + f.Size.Width/2, Y: f.Origin.Y + f.Size.Height/2}
}

// TouchControlInfo describes one placed control of a TouchControls layer.
type TouchControlInfo struct {
	// ID, Kind and Label are the control's spec fields.
	ID, Kind, Label string
	// Frame is where the control is on screen, in the view's points.
	Frame core.Bounds
}

type touchEntry struct {
	spec    TouchControlSpec
	control TouchControl
	frame   core.Bounds
}

// TouchControls is a layer of on-screen controls built from a TouchLayout: it places them in the safe area at a size that follows the screen, drives the same named actions and axes as the keyboard and game controllers through Actions, shows itself when the player touches the screen and hides when they type or use a controller, fades when idle, mirrors for left-handed play, and saves a player's edited layout.
//
// Call BindActions with the game's Actions, which routes the view's touches to the controls, Attach on the game's view after Actions.Attach, Resize whenever the view's size or safe area changes, Update once per game.Loop step after Actions.Step, and Draw from the game's Canvas. Controls work in the view's points, so they ignore Actions.MapPoint.
//
// Its methods are called on the engine's goroutine, as core.View's callbacks and game.Loop.Update are.
type TouchControls struct {
	// Style is how the controls draw; its zero value draws DefaultVirtualStyle.
	Style VirtualStyle
	// Haptics, when set, plays a selection tick each time a finger presses a control.
	Haptics *Haptics

	layout   TouchLayout
	entries  []touchEntry
	pad      *VirtualPad
	tracker  *DeviceTracker
	actions  *Actions
	size     core.Size
	safe     core.Edges
	visible  bool
	alpha    float32
	idle     time.Duration
	placed   bool
	keysHeld bool
	padsUsed bool
}

// NewTouchControls builds the controls layout declares. It returns an error, a *loaderr.Error naming the control's field, when layout is invalid, names a kind no one registered, or a kind's factory refuses a control.
//
// The controls start visible when cx's primary pointer is coarse (Env.Pointer is core.PointerCoarse, as on phones and tablets) and hidden otherwise, until the player touches the screen; a nil cx starts them hidden. They are placed on a screen of layout's reference size until the first Resize.
func NewTouchControls(cx *core.Scope, layout TouchLayout) (*TouchControls, error) {
	t := &TouchControls{alpha: 1}
	t.pad = &VirtualPad{
		// The controls are placed in the view's points, so a mapping Actions applies for its regions must not move them.
		MapPoint:            func(p core.Point) (core.Point, bool) { return p, true },
		SlideBetweenButtons: true,
		OnPress:             t.onPress,
	}
	if err := t.build(layout); err != nil {
		return nil, err
	}
	t.tracker = NewDeviceTracker(nil, DeviceUnknown)
	t.tracker.OnChange(t.onDevice)
	if cx != nil {
		t.visible = core.Untracked(cx, cx.Env().Pointer) == core.PointerCoarse
	}
	t.Resize(core.Size{Width: core.Points(layout.Reference.Width), Height: core.Points(layout.Reference.Height)}, core.Edges{})
	return t, nil
}

// build validates l and replaces t's controls with those it declares.
func (t *TouchControls) build(l TouchLayout) error {
	if err := ValidateTouchLayout(l); err != nil {
		return err
	}
	entries := make([]touchEntry, 0, len(l.Controls))
	for i, spec := range l.Controls {
		touchRegistry.RLock()
		factory := touchRegistry.kinds[spec.Kind]
		touchRegistry.RUnlock()
		if factory == nil {
			return &loaderr.Error{Field: loaderr.Path("controls", i, "kind"), Problem: fmt.Sprintf("unknown kind %q", spec.Kind), Expected: "button, dpad, stick or a kind registered with RegisterTouchControlKind", Fix: "correct the kind, or import the package that registers it"}
		}
		c, err := factory(cloneTouchSpec(spec))
		if err != nil {
			if e, ok := loaderr.As(err); ok && e.Field != "" {
				located := *e
				// The factory's field is a path within the control, as loaderr.Path writes it.
				sep := "."
				if strings.HasPrefix(located.Field, "[") {
					sep = ""
				}
				located.Field = loaderr.Path("controls", i) + sep + located.Field
				return &located
			}
			return loaderr.Errorf(loaderr.Path("controls", i), "correct the control as its kind documents", "%s %q: %w", spec.Kind, spec.ID, err)
		}
		if c == nil {
			return loaderr.Errorf(loaderr.Path("controls", i, "kind"), "fix the factory registered for the kind", "kind %q built no control", spec.Kind)
		}
		entries = append(entries, touchEntry{spec: spec, control: c})
	}
	t.pad.Reset()
	t.pad.Controls = t.pad.Controls[:0]
	for _, e := range entries {
		t.pad.AddControl(e.control)
	}
	t.layout = cloneTouchLayout(l)
	t.entries = entries
	t.syncActions()
	return nil
}

// Layout returns a copy of the layout t shows, mirrored or not as SetMirrored last set it.
func (t *TouchControls) Layout() TouchLayout {
	return cloneTouchLayout(t.layout)
}

// SetLayout replaces t's controls with those l declares, such as a layout the player edited, and places them on the current screen. Fingers on the old controls are released. On error t keeps its controls.
func (t *TouchControls) SetLayout(l TouchLayout) error {
	if err := t.build(l); err != nil {
		return err
	}
	t.place()
	return nil
}

// SetMirrored swaps the controls between the left and right of the screen when mirrored is true, for left-handed play, and back when it is false.
func (t *TouchControls) SetMirrored(mirrored bool) {
	t.layout.Mirrored = mirrored
	t.place()
}

// Pad returns the VirtualPad t routes touches through, for a game that reads its controls directly.
func (t *TouchControls) Pad() *VirtualPad {
	return t.pad
}

// BindActions connects t's controls to a, so their actions and axes read through a's Pressed and Axis beside the keyboard's and controllers', a routes the touches it hears to them, and t watches a for keyboard and controller use. A nil a disconnects t.
func (t *TouchControls) BindActions(a *Actions) {
	t.pad.BindActions(a)
	t.actions = a
}

// Tracker returns the DeviceTracker that shows t on touch and hides it on keyboard and controller use. Feed it more events, such as a game's own key handler, to switch t the same way.
func (t *TouchControls) Tracker() *DeviceTracker {
	return t.tracker
}

// Attach wires t's device tracking onto v, chaining v's existing callbacks, so a touch anywhere on v shows the controls and a key v hears hides them. Call it after Actions.Attach. It hears touches and keys alone: a press synthesized from a touch can report Keyboard when a key was the last input, which must not hide the controls the finger is using.
func (t *TouchControls) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { t.tracker.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = t.tracker.OnTouch
	}
	if prev := v.OnKeyDown; prev != nil {
		v.OnKeyDown = func(ev core.KeyEvent) bool { t.tracker.OnKey(ev); return prev(ev) }
	} else {
		v.OnKeyDown = func(ev core.KeyEvent) bool { t.tracker.OnKey(ev); return false }
	}
}

// Resize places the controls on a view of size whose edges system UI covers by safe, such as Env.SafeArea or core.Insets of the view's scope. Calling it again with the same values does nothing.
func (t *TouchControls) Resize(size core.Size, safe core.Edges) {
	if t.placed && size == t.size && safe == t.safe {
		return
	}
	t.size, t.safe = size, safe
	t.placed = true
	t.place()
}

// place computes every control's frame from its spec and hands it to the control.
func (t *TouchControls) place() {
	x0, y0 := t.safe.Left, t.safe.Top
	sw := max(t.size.Width-t.safe.Left-t.safe.Right, 0)
	sh := max(t.size.Height-t.safe.Top-t.safe.Bottom, 0)
	ref := t.layout.Reference
	scale := min(float64(sw)/ref.Width, float64(sh)/ref.Height)
	if t.layout.MaxScale > 0 {
		scale = min(scale, t.layout.MaxScale)
	}
	for i := range t.entries {
		e := &t.entries[i]
		spec := e.spec
		w := core.Points(max(spec.Size.Width*scale, MinTouchTarget))
		h := core.Points(max(spec.Size.Height*scale, MinTouchTarget))
		ox := core.Points(spec.Offset.X * scale)
		oy := core.Points(spec.Offset.Y * scale)
		horizontal, vertical := anchorSides(spec.Anchor)
		if t.layout.Mirrored {
			horizontal = -horizontal
			if horizontal == 0 {
				ox = -ox
			}
		}
		var x, y core.Points
		switch horizontal {
		case -1:
			x = x0 + ox
		case 1:
			x = x0 + sw - w - ox
		default:
			x = x0 + (sw-w)/2 + ox
		}
		switch vertical {
		case -1:
			y = y0 + oy
		case 1:
			y = y0 + sh - h - oy
		default:
			y = y0 + (sh-h)/2 + oy
		}
		// Keep the control inside the safe area, pinned to its top left when it is larger than the area.
		x = max(min(x, x0+sw-w), x0)
		y = max(min(y, y0+sh-h), y0)
		e.frame = core.Bounds{Origin: core.Point{X: x, Y: y}, Size: core.Size{Width: w, Height: h}}
		e.control.Place(e.frame, float32(scale))
	}
}

// anchorSides returns which side of the safe area anchor sits against: -1 for left or top, 1 for right or bottom, 0 for the middle.
func anchorSides(anchor string) (horizontal, vertical int) {
	switch anchor {
	case AnchorTopLeft:
		return -1, -1
	case AnchorTop:
		return 0, -1
	case AnchorTopRight:
		return 1, -1
	case AnchorLeft:
		return -1, 0
	case AnchorRight:
		return 1, 0
	case AnchorBottomLeft:
		return -1, 1
	case AnchorBottom:
		return 0, 1
	case AnchorBottomRight:
		return 1, 1
	default:
		return 0, 0
	}
}

// Visible reports whether t draws its controls.
func (t *TouchControls) Visible() bool {
	return t.visible
}

// SetVisible shows or hides the controls. Showing makes them fully opaque; hiding lifts every finger on them, keeping toggled buttons on. Touches still reach hidden controls, and a press on one shows them again. Either way the next switch of device counts: a key or a controller hides controls shown this way, and a touch shows controls hidden this way.
func (t *TouchControls) SetVisible(visible bool) {
	if visible == t.visible {
		return
	}
	t.visible = visible
	if visible {
		t.alpha, t.idle = 1, 0
		// The tracker hides the controls only when the device changes, so it must not still hold the keyboard or a controller.
		t.tracker.SetDevice(DeviceTouch)
		return
	}
	if t.tracker.LastDeviceUntracked() == DeviceTouch {
		t.tracker.SetDevice(DeviceUnknown)
	}
	t.pad.releaseAll()
	t.syncActions()
}

// Wake makes the controls fully opaque and restarts the idle fade, as a press does. Call it when the controls come back into view after the game stopped drawing them, such as when play starts or resumes from a pause, so they do not first appear already faded by the time spent elsewhere.
func (t *TouchControls) Wake() {
	t.alpha, t.idle = 1, 0
}

// Alpha returns the opacity t draws its controls with: 0 while hidden, and between IdleAlpha and 1 as they fade.
func (t *TouchControls) Alpha() float32 {
	if !t.visible {
		return 0
	}
	return t.alpha
}

// Update advances the idle fade by dt and hides the controls when a key or a game controller the bound Actions reads starts being used. Call it once per game.Loop step, after Actions.Step.
func (t *TouchControls) Update(dt time.Duration) {
	if a := t.actions; a != nil {
		keys := len(a.keysDown) > 0 || len(a.watchedKeys) > 0 || len(a.watchedCodes) > 0
		if keys && !t.keysHeld {
			t.tracker.OnKey(core.KeyEvent{})
		}
		t.keysHeld = keys
		name, used := gamepadInUse(a)
		if used && !t.padsUsed {
			t.tracker.OnGamepadActivity(name)
		}
		t.padsUsed = used
	}
	if t.pad.active() {
		t.idle = 0
	} else {
		t.idle += dt
	}
	target := float32(1)
	if after := t.layout.IdleAfter; after > 0 && t.idle >= after {
		target = float32(t.layout.IdleAlpha)
	}
	step := float32(dt) / float32(touchFadeDuration)
	switch {
	case t.alpha < target:
		t.alpha = min(t.alpha+step, target)
	case t.alpha > target:
		t.alpha = max(t.alpha-step, target)
	}
}

// gamepadInUse reports whether a button is pressed or a stick pushed past halfway on a controller a last read, and that controller's name.
func gamepadInUse(a *Actions) (string, bool) {
	for _, p := range a.gpPads {
		for _, v := range p.Buttons {
			if v >= 0.5 {
				return p.Name, true
			}
		}
		for _, v := range p.Axes {
			if v >= 0.5 || v <= -0.5 {
				return p.Name, true
			}
		}
	}
	return "", false
}

// Draw draws the visible controls into d at their current opacity, leaving d's state unchanged, and names each one as a canvas node for assistive technology and tests, with its label and the test ID "touch-" followed by its id: a ButtonControl as a button selected while pressed, an AccessibleControl with the role it reports, and any other control as a group. The nodes are named whatever the opacity, so controls an idleAlpha of 0 fades out of sight still reach screen readers and tests while they take touches. d is the drawing of a Canvas that covers the view t is placed on.
func (t *TouchControls) Draw(d *core.Drawing) {
	if d == nil || !t.visible {
		return
	}
	if t.alpha > 0 {
		d.Save()
		d.SetAlpha(t.alpha)
		t.pad.Draw(d, t.Style)
		d.Restore()
	}
	for _, e := range t.entries {
		node := core.CanvasNode{Bounds: e.frame, Role: core.RoleGroup, Label: e.spec.Label, TestID: "touch-" + e.spec.ID}
		switch c := e.control.(type) {
		case AccessibleControl:
			node.Role, node.Selected = c.Accessibility()
		case ButtonControl:
			node.Role = core.RoleButton
			node.Selected = c.Button().Pressed()
		}
		d.Node(node)
	}
}

// Controls describes every control in layout order, with where it is on screen.
func (t *TouchControls) Controls() []TouchControlInfo {
	out := make([]TouchControlInfo, len(t.entries))
	for i, e := range t.entries {
		out[i] = TouchControlInfo{ID: e.spec.ID, Kind: e.spec.Kind, Label: e.spec.Label, Frame: e.frame}
	}
	return out
}

// Control returns the control whose spec has id, or nil.
func (t *TouchControls) Control(id string) TouchControl {
	for _, e := range t.entries {
		if e.spec.ID == id {
			return e.control
		}
	}
	return nil
}

// Button returns the VirtualButton of the control id when it is a ButtonControl, a button kind or a plugin's, or nil.
func (t *TouchControls) Button(id string) *VirtualButton {
	if b, ok := t.Control(id).(ButtonControl); ok {
		return b.Button()
	}
	return nil
}

// DPad returns the VirtualDPad of the d-pad control id, or nil when id is not a d-pad.
func (t *TouchControls) DPad(id string) *VirtualDPad {
	if p, ok := t.Control(id).(*touchDPad); ok {
		return p.VirtualDPad
	}
	return nil
}

// Stick returns the VirtualStick of the stick control id, or nil when id is not a stick.
func (t *TouchControls) Stick(id string) *VirtualStick {
	if s, ok := t.Control(id).(*touchStick); ok {
		return s.VirtualStick
	}
	return nil
}

// Reset releases every finger on the controls and turns toggled buttons off. Call it with Actions.Reset, which also resets the controls while they are bound.
func (t *TouchControls) Reset() {
	t.pad.Reset()
	t.syncActions()
}

// Save writes t's layout into store under key as the YAML EncodeTouchLayout writes, so a player's edited layout survives a restart. It fails with *core.StorageFullError when storage is full.
func (t *TouchControls) Save(store core.Storage, key string) error {
	data, err := EncodeTouchLayout(t.layout)
	if err != nil {
		return err
	}
	return store.SetItem(key, string(data))
}

// Load replaces t's layout with the one saved in store under key, and reports whether key was present. When key is absent, Load keeps t's layout and returns false, nil. When the saved layout does not parse or build, Load keeps t's layout and returns false and a *loaderr.Error that names key as its file.
func (t *TouchControls) Load(store core.Storage, key string) (bool, error) {
	raw, ok := store.GetItem(key)
	if !ok {
		return false, nil
	}
	const fix = "save the layout again with TouchControls.Save, or remove the key so the default layout applies"
	l, err := parseTouchLayout([]byte(raw))
	if err == nil {
		err = t.SetLayout(l)
	}
	if err != nil {
		return false, loaderr.Locate(err, key, []byte(raw), fix)
	}
	return true, nil
}

func (t *TouchControls) onPress() {
	t.Wake()
	// Through the tracker, so the next key or controller use counts as a change of device and hides the controls again.
	t.tracker.SetDevice(DeviceTouch)
	t.SetVisible(true)
	if t.Haptics != nil {
		t.Haptics.Selection()
	}
}

func (t *TouchControls) onDevice(d DeviceKind) {
	switch {
	case d == DeviceTouch:
		t.SetVisible(true)
	case d == DeviceKeyboard || d.IsGamepad():
		t.SetVisible(false)
	}
}

func (t *TouchControls) syncActions() {
	if t.actions != nil {
		t.actions.syncRaw()
	}
}

func cloneTouchSpec(s TouchControlSpec) TouchControlSpec {
	s.Options = maps.Clone(s.Options)
	return s
}

func cloneTouchLayout(l TouchLayout) TouchLayout {
	l.Controls = slices.Clone(l.Controls)
	for i := range l.Controls {
		l.Controls[i] = cloneTouchSpec(l.Controls[i])
	}
	return l
}

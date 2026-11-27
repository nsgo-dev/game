package input

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultDPadDeadZone is the dead zone a VirtualDPad uses when its DeadZone is zero or negative: the inner fifth of its radius selects no direction.
const DefaultDPadDeadZone = 0.2

// DPadDirection is a set of d-pad directions, one bit each; a diagonal on an 8-way VirtualDPad holds two.
type DPadDirection uint8

// The four directions of a VirtualDPad.
const (
	DPadUp DPadDirection = 1 << iota
	DPadDown
	DPadLeft
	DPadRight
)

// Has reports whether d includes every direction in dir.
func (d DPadDirection) Has(dir DPadDirection) bool {
	return dir != 0 && d&dir == dir
}

// dpadArrows lists the four arrows a VirtualDPad draws, with the screen direction each points in.
var dpadArrows = [4]struct {
	dir    DPadDirection
	dx, dy float32
}{
	{DPadUp, 0, -1},
	{DPadDown, 0, 1},
	{DPadLeft, -1, 0},
	{DPadRight, 1, 0},
}

// VirtualDPad is a round on-screen directional pad that one finger owns as a whole: the finger can rock or slide between directions without lifting, and the pad quantizes its offset from Center into 4 or 8 directions.
//
// Each direction drives the action named in Up, Down, Left or Right, and AxisXName and AxisYName, when set, name axes the pad drives with -1, 0 or 1. AxisY is positive up, as a game controller's d-pad and inverted stick read. Add it to a VirtualPad with AddControl, or feed it touches with OnTouch.
type VirtualDPad struct {
	// Center and Radius define the pad's circular hit area. A finger that touched down inside it keeps steering the pad when it slides past the edge.
	Center core.Point
	Radius float32
	// Ways is 4 for up, down, left and right only, or 8 to add the diagonals; any other value means 4.
	Ways int
	// DeadZone is the fraction of Radius in [0, 1) around Center that selects no direction; zero or negative uses DefaultDPadDeadZone.
	DeadZone float32
	// Up, Down, Left and Right name the actions each direction presses; an empty name presses nothing.
	Up, Down, Left, Right string
	// AxisXName and AxisYName, when set, name the axes the pad drives: -1 left and +1 right on AxisXName, +1 up and -1 down on AxisYName.
	AxisXName string
	AxisYName string

	active   bool
	fingerID int
	dir      DPadDirection
}

// Active reports whether a finger currently owns the pad.
func (p *VirtualDPad) Active() bool {
	return p.active
}

// Direction returns the directions the owning finger currently selects, or 0 at rest or inside the dead zone.
func (p *VirtualDPad) Direction() DPadDirection {
	return p.dir
}

// Contains reports whether pt lies inside the pad's circle.
func (p *VirtualDPad) Contains(pt core.Point) bool {
	return p.Radius > 0 && pointDist(pt, p.Center) <= p.Radius
}

// Reset releases the owning finger and clears the direction.
func (p *VirtualDPad) Reset() {
	p.active = false
	p.fingerID = 0
	p.dir = 0
}

// Claim takes finger id when it touches down inside the pad while no other finger owns it.
func (p *VirtualDPad) Claim(id int, pt core.Point) bool {
	if p.active || !p.Contains(pt) {
		return false
	}
	p.active = true
	p.fingerID = id
	p.dir = p.quantize(pt)
	return true
}

// Move steers the pad with its owning finger.
func (p *VirtualDPad) Move(id int, pt core.Point) {
	if p.active && id == p.fingerID {
		p.dir = p.quantize(pt)
	}
}

// Release lets go of the owning finger.
func (p *VirtualDPad) Release(id int) {
	if p.active && id == p.fingerID {
		p.Reset()
	}
}

// ActionNames returns the pad's non-empty direction actions.
func (p *VirtualDPad) ActionNames() []string {
	names := make([]string, 0, 4)
	for _, name := range [4]string{p.Up, p.Down, p.Left, p.Right} {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ActionPressed reports whether a selected direction drives action name.
func (p *VirtualDPad) ActionPressed(name string) bool {
	if name == "" || p.dir == 0 {
		return false
	}
	return (p.dir.Has(DPadUp) && p.Up == name) ||
		(p.dir.Has(DPadDown) && p.Down == name) ||
		(p.dir.Has(DPadLeft) && p.Left == name) ||
		(p.dir.Has(DPadRight) && p.Right == name)
}

// AxisValues yields AxisXName and AxisYName, when set, with the selected direction's -1, 0 or 1.
func (p *VirtualDPad) AxisValues(yield func(name string, value float64)) {
	if p.AxisXName != "" {
		var x float64
		if p.dir.Has(DPadRight) {
			x++
		}
		if p.dir.Has(DPadLeft) {
			x--
		}
		yield(p.AxisXName, x)
	}
	if p.AxisYName != "" {
		var y float64
		if p.dir.Has(DPadUp) {
			y++
		}
		if p.dir.Has(DPadDown) {
			y--
		}
		yield(p.AxisYName, y)
	}
}

// OnTouch feeds a touch event directly to the pad when used standalone without a VirtualPad.
func (p *VirtualDPad) OnTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		if p.active {
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if hasChanged && !t.Changed {
				continue
			}
			if p.Claim(t.ID, t.Position) {
				return
			}
		}
	case core.TouchMove:
		for _, t := range ev.Touches {
			p.Move(t.ID, t.Position)
		}
	case core.TouchEnd, core.TouchCancel:
		if !p.active {
			return
		}
		if ev.Phase == core.TouchCancel && len(ev.Touches) == 0 {
			p.Reset()
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if t.ID == p.fingerID && (t.Changed || !hasChanged) {
				p.Reset()
				return
			}
		}
	}
}

// Draw draws the pad's base, its four arrows and a highlight behind each selected direction into d, leaving d's state unchanged.
func (p *VirtualDPad) Draw(d *core.Drawing, style VirtualStyle) {
	if d == nil || p.Radius <= 0 {
		return
	}
	st := style.resolved()
	cx, cy, r := float32(p.Center.X), float32(p.Center.Y), p.Radius

	d.Save()
	d.BeginPath().
		Arc(cx, cy, r, 0, twoPi, false).
		SetFillColor(st.BaseFill).
		Fill()
	if st.BaseStrokeWidth > 0 {
		d.SetStrokeColor(st.BaseStroke).
			SetLineWidth(st.BaseStrokeWidth).
			Stroke()
	}
	for _, a := range dpadArrows {
		ax, ay := cx+a.dx*r*0.6, cy+a.dy*r*0.6
		color := st.KnobFill
		if p.dir.Has(a.dir) {
			d.BeginPath().
				Arc(ax, ay, r*0.32, 0, twoPi, false).
				SetFillColor(st.ButtonPressedFill).
				Fill()
			color = st.LabelColor
		}
		drawArrow(d, ax, ay, a.dx, a.dy, r*0.18, color)
	}
	d.Restore()
}

// quantize returns the directions pt selects, measured from Center.
func (p *VirtualDPad) quantize(pt core.Point) DPadDirection {
	dx := float64(pt.X - p.Center.X)
	dy := float64(pt.Y - p.Center.Y)
	dz := float64(p.DeadZone)
	if dz <= 0 {
		dz = DefaultDPadDeadZone
	}
	dist := math.Hypot(dx, dy)
	if p.Radius <= 0 || dist == 0 || dist <= dz*float64(p.Radius) {
		return 0
	}
	// Up is positive, so sectors run counter-clockwise from the right.
	angle := math.Atan2(-dy, dx)
	if p.Ways == 8 {
		sectors := [8]DPadDirection{DPadRight, DPadUp | DPadRight, DPadUp, DPadUp | DPadLeft, DPadLeft, DPadDown | DPadLeft, DPadDown, DPadDown | DPadRight}
		i := int(math.Round(angle/(math.Pi/4))) % 8
		return sectors[(i+8)%8]
	}
	sectors := [4]DPadDirection{DPadRight, DPadUp, DPadLeft, DPadDown}
	i := int(math.Round(angle/(math.Pi/2))) % 4
	return sectors[(i+4)%4]
}

// drawArrow fills a triangle centered on (cx, cy) pointing along the unit vector (ux, uy), size from its center to its tip.
func drawArrow(d *core.Drawing, cx, cy, ux, uy, size float32, color core.Color) {
	// The perpendicular spreads the base on either side of the arrow's axis.
	px, py := -uy, ux
	d.BeginPath().
		MoveTo(cx+ux*size, cy+uy*size).
		LineTo(cx-ux*size*0.6+px*size, cy-uy*size*0.6+py*size).
		LineTo(cx-ux*size*0.6-px*size, cy-uy*size*0.6-py*size).
		ClosePath().
		SetFillColor(color).
		Fill()
}

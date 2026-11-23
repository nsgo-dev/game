package worldmap

import (
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// Style sets the colors and sizes DrawMinimap and DrawWorldMap use. A zero (fully transparent) color leaves that part out, except Current (see below), and a zero size uses DefaultStyle's.
type Style struct {
	// Background fills the whole destination rectangle first.
	Background core.Color
	// Visited, Revealed and Current fill visited rooms, revealed rooms and the current room. A transparent Current fills the current room with Visited, so it is drawn like any other visited room.
	Visited, Revealed, Current core.Color
	// Border outlines every drawn room with BorderWidth.
	Border      core.Color
	BorderWidth float32
	// Link draws a line of LinkWidth between the centers of two linked rooms when both are discovered and in the drawn area.
	Link      core.Color
	LinkWidth float32
	// Marker is the color of markers whose kind is not in MarkerKinds; each marker is a square MarkerSize points wide.
	Marker      core.Color
	MarkerKinds map[string]core.Color
	MarkerSize  float32
	// Player is the color of the square PlayerSize points wide drawn at the player's position.
	Player     core.Color
	PlayerSize float32
	// Padding insets the world map from the destination rectangle, in points.
	Padding float32
}

// DefaultStyle returns a dark style with lighter visited rooms, dim revealed rooms and a highlighted current room.
func DefaultStyle() Style {
	return Style{
		Background:  core.RGBA(0x0b0f1a, 0.85),
		Visited:     core.RGB(0x3b5b8c),
		Revealed:    core.RGB(0x1f2c44),
		Current:     core.RGB(0x5d8fd6),
		Border:      core.RGB(0xc8d6ec),
		BorderWidth: 1,
		Link:        core.RGB(0xc8d6ec),
		LinkWidth:   2,
		Marker:      core.RGB(0xf2c14e),
		MarkerSize:  6,
		Player:      core.RGB(0xff5a5f),
		PlayerSize:  6,
		Padding:     8,
	}
}

func (s *Style) resolved() Style {
	def := DefaultStyle()
	if s == nil {
		return def
	}
	out := *s
	if out.BorderWidth <= 0 {
		out.BorderWidth = def.BorderWidth
	}
	if out.LinkWidth <= 0 {
		out.LinkWidth = def.LinkWidth
	}
	if out.MarkerSize <= 0 {
		out.MarkerSize = def.MarkerSize
	}
	if out.PlayerSize <= 0 {
		out.PlayerSize = def.PlayerSize
	}
	if out.Padding < 0 {
		out.Padding = 0
	}
	return out
}

// Projection maps world coordinates onto a drawn map: a map point is world*Scale + Offset.
type Projection struct {
	// Scale is map points per world unit; always positive for projections this package returns.
	Scale float32
	// Offset is where the world origin lands on the map.
	Offset math2d.Vec2
}

// ToMap converts a world point to a point on the drawn map.
func (p Projection) ToMap(v math2d.Vec2) math2d.Vec2 {
	return math2d.Vec2{X: v.X*p.Scale + p.Offset.X, Y: v.Y*p.Scale + p.Offset.Y}
}

// ToWorld converts a point on the drawn map, such as a tap, back to world coordinates. It returns the world origin when Scale is 0.
func (p Projection) ToWorld(v math2d.Vec2) math2d.Vec2 {
	if p.Scale == 0 {
		return math2d.Vec2{}
	}
	return math2d.Vec2{X: (v.X - p.Offset.X) / p.Scale, Y: (v.Y - p.Offset.Y) / p.Scale}
}

// RectToMap converts a world rectangle to the drawn map.
func (p Projection) RectToMap(r math2d.Rect) math2d.Rect {
	o := p.ToMap(math2d.Vec2{X: r.X, Y: r.Y})
	return math2d.Rect{X: o.X, Y: o.Y, W: r.W * p.Scale, H: r.H * p.Scale}
}

// MinimapProjection centers the world point center in dst at scale map points per world unit; a scale that is not positive and finite behaves as 1. The Offset is not finite when center is not, or when center*scale overflows float32.
func MinimapProjection(dst core.Bounds, center math2d.Vec2, scale float32) Projection {
	if !(scale > 0) || !finite(scale) {
		scale = 1
	}
	c := math2d.FromBounds(dst).Center()
	return Projection{Scale: scale, Offset: math2d.Vec2{X: c.X - center.X*scale, Y: c.Y - center.Y*scale}}
}

// WorldMapProjection fits every room of area, discovered or not, into dst inset by padding, keeping the aspect ratio and centering it, so the layout does not shift as rooms are discovered. It returns false when the map has no such area or the inset dst is empty.
func (m *Map) WorldMapProjection(dst core.Bounds, area string, padding float32) (Projection, bool) {
	ab, ok := m.areas[area]
	if !ok {
		return Projection{}, false
	}
	if padding < 0 {
		padding = 0
	}
	box := math2d.FromBounds(dst).Expand(-padding)
	if box.Empty() {
		return Projection{}, false
	}
	scale := min(box.W/ab.W, box.H/ab.H)
	if !(scale > 0) || !finite(scale) {
		return Projection{}, false
	}
	bc, ac := box.Center(), ab.Center()
	return Projection{Scale: scale, Offset: math2d.Vec2{X: bc.X - ac.X*scale, Y: bc.Y - ac.Y*scale}}, true
}

// DrawMinimap draws the discovered rooms of the current room's area (the default area when there is no current room) around the player's world position, at scale map points per world unit, clipped to dst. The player's square is drawn at the center of dst. It draws nothing when the projection is not finite (see MinimapProjection), such as for a NaN or infinite player position.
func (m *Map) DrawMinimap(d *core.Drawing, dst core.Bounds, player math2d.Vec2, scale float32, style *Style) {
	if d == nil {
		return
	}
	proj := MinimapProjection(dst, player, scale)
	if !finite(proj.Offset.X) || !finite(proj.Offset.Y) {
		return
	}
	m.draw(d, dst, proj, m.currentArea(), player, true, style)
}

// DrawWorldMap draws the discovered rooms of area fitted into dst (see WorldMapProjection). When player is not nil and finite and the current room is in area, the player's square is drawn at that world position. It draws nothing when the map has no such area.
func (m *Map) DrawWorldMap(d *core.Drawing, dst core.Bounds, area string, player *math2d.Vec2, style *Style) {
	if d == nil {
		return
	}
	st := style.resolved()
	proj, ok := m.WorldMapProjection(dst, area, st.Padding)
	if !ok {
		return
	}
	var at math2d.Vec2
	show := player != nil && finite(player.X) && finite(player.Y) && m.current >= 0 && m.currentArea() == area
	if show {
		at = *player
	}
	m.draw(d, dst, proj, area, at, show, &st)
}

func (m *Map) draw(d *core.Drawing, dst core.Bounds, proj Projection, area string, player math2d.Vec2, showPlayer bool, style *Style) {
	st := style.resolved()
	box := math2d.FromBounds(dst)
	if box.Empty() {
		return
	}
	view := math2d.Rect{
		X: (box.X - proj.Offset.X) / proj.Scale,
		Y: (box.Y - proj.Offset.Y) / proj.Scale,
		W: box.W / proj.Scale,
		H: box.H / proj.Scale,
	}
	shown := func(i int) bool {
		r := &m.rooms[i]
		return r.Area == area && m.status[i] != Unknown && r.Bounds.Overlaps(view)
	}

	d.Save()
	// Each path is cleared after use so later FillRect calls do not make the recorder replay it.
	d.BeginPath().Rect(box.X, box.Y, box.W, box.H).Clip().BeginPath()
	if visible(st.Background) {
		d.SetFillColor(st.Background).FillRect(box.X, box.Y, box.W, box.H)
	}

	if visible(st.Link) {
		started := false
		for _, l := range m.links {
			a, b := &m.rooms[l[0]], &m.rooms[l[1]]
			if a.Area != area || b.Area != area || m.status[l[0]] == Unknown || m.status[l[1]] == Unknown {
				continue
			}
			pa, pb := proj.ToMap(a.Bounds.Center()), proj.ToMap(b.Bounds.Center())
			if !math2d.FromMinMax(pa.Min(pb), pa.Max(pb)).Expand(st.LinkWidth).Overlaps(box) {
				continue
			}
			if !started {
				d.BeginPath()
				started = true
			}
			d.MoveTo(pa.X, pa.Y).LineTo(pb.X, pb.Y)
		}
		if started {
			d.SetStrokeColor(st.Link).SetLineWidth(st.LinkWidth).Stroke().BeginPath()
		}
	}

	m.fillRooms(d, proj, shown, Revealed, st.Revealed)
	m.fillRooms(d, proj, func(i int) bool { return shown(i) && i != m.current }, Visited, st.Visited)
	if m.current >= 0 && shown(m.current) {
		c := st.Current
		if !visible(c) {
			c = st.Visited
		}
		if visible(c) {
			r := proj.RectToMap(m.rooms[m.current].Bounds)
			d.SetFillColor(c).FillRect(r.X, r.Y, r.W, r.H)
		}
	}
	if visible(st.Border) {
		started := false
		for i := range m.rooms {
			if !shown(i) {
				continue
			}
			if !started {
				d.BeginPath()
				started = true
			}
			r := proj.RectToMap(m.rooms[i].Bounds)
			d.Rect(r.X, r.Y, r.W, r.H)
		}
		if started {
			d.SetStrokeColor(st.Border).SetLineWidth(st.BorderWidth).Stroke().BeginPath()
		}
	}

	m.drawMarkers(d, proj, box, area, m.fixed, &st)
	m.drawMarkers(d, proj, box, area, m.pins, &st)

	if showPlayer && visible(st.Player) {
		p := proj.ToMap(player)
		h := st.PlayerSize / 2
		d.SetFillColor(st.Player).FillRect(p.X-h, p.Y-h, st.PlayerSize, st.PlayerSize)
	}
	d.Restore()
}

func (m *Map) fillRooms(d *core.Drawing, proj Projection, shown func(int) bool, s Discovery, c core.Color) {
	if !visible(c) {
		return
	}
	started := false
	for i := range m.rooms {
		if m.status[i] != s || !shown(i) {
			continue
		}
		if !started {
			d.BeginPath()
			started = true
		}
		r := proj.RectToMap(m.rooms[i].Bounds)
		d.Rect(r.X, r.Y, r.W, r.H)
	}
	if started {
		d.SetFillColor(c).Fill().BeginPath()
	}
}

func (m *Map) drawMarkers(d *core.Drawing, proj Projection, box math2d.Rect, area string, markers []Marker, st *Style) {
	h := st.MarkerSize / 2
	for i := range markers {
		mk := &markers[i]
		ri := m.index[mk.Room]
		if m.rooms[ri].Area != area || m.status[ri] == Unknown {
			continue
		}
		c, ok := st.MarkerKinds[mk.Kind]
		if !ok {
			c = st.Marker
		}
		if !visible(c) {
			continue
		}
		p := proj.ToMap(math2d.Vec2{X: mk.X, Y: mk.Y})
		r := math2d.Rect{X: p.X - h, Y: p.Y - h, W: st.MarkerSize, H: st.MarkerSize}
		if !r.Overlaps(box) {
			continue
		}
		d.SetFillColor(c).FillRect(r.X, r.Y, r.W, r.H)
	}
}

// visible reports whether c has any opacity; core.Color packs RGBA with alpha in the low byte.
func visible(c core.Color) bool { return c&0xff != 0 }

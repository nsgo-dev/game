//go:build !release

package physics

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const twoPi = float32(2 * math.Pi)

// DebugStyle configures colors and screen-space annotation sizes for physics debug drawing.
// Zero fields fall back to DefaultDebugStyle values.
//
// LineWidth, NormalLength and ContactRadius are given in screen points and divided by the
// active camera zoom (Zoom when positive, otherwise inferred from the drawing's current
// transform) so outlines, contact markers and normal arrows stay crisp at every zoom.
type DebugStyle struct {
	BoxColor        core.Color
	CircleColor     core.Color
	SolidTileColor  core.Color
	OneWayTileColor core.Color
	ContactColor    core.Color
	NormalColor     core.Color
	RayColor        core.Color
	VelocityColor   core.Color

	LineWidth     float32
	Zoom          float32
	NormalLength  float32
	ContactRadius float32
	// VelocityScale draws each body's velocity vector scaled by this duration in seconds
	// when positive. Leave 0 to omit velocity vectors.
	VelocityScale float32
}

// DefaultDebugStyle returns a high-contrast DebugStyle suitable for drawing over game scenes.
func DefaultDebugStyle() DebugStyle {
	return DebugStyle{
		BoxColor:        core.RGB(0x22c55e),
		CircleColor:     core.RGB(0x38bdf8),
		SolidTileColor:  core.RGB(0xf59e0b),
		OneWayTileColor: core.RGB(0xc084fc),
		ContactColor:    core.RGB(0xef4444),
		NormalColor:     core.RGB(0xfacc15),
		RayColor:        core.RGB(0xf472b6),
		VelocityColor:   core.RGB(0x60a5fa),
		LineWidth:       1,
		NormalLength:    12,
		ContactRadius:   2.5,
	}
}

type resolvedStyle struct {
	boxColor        core.Color
	circleColor     core.Color
	solidTileColor  core.Color
	oneWayTileColor core.Color
	contactColor    core.Color
	normalColor     core.Color
	rayColor        core.Color
	velocityColor   core.Color

	zoom          float32
	lineWidth     float32
	normalLen     float32
	contactRadius float32
	arrowHead     float32
	velocityScale float32
}

func resolveDebugStyle(d *core.Drawing, s DebugStyle) resolvedStyle {
	def := DefaultDebugStyle()
	if s.BoxColor == 0 {
		s.BoxColor = def.BoxColor
	}
	if s.CircleColor == 0 {
		s.CircleColor = def.CircleColor
	}
	if s.SolidTileColor == 0 {
		s.SolidTileColor = def.SolidTileColor
	}
	if s.OneWayTileColor == 0 {
		s.OneWayTileColor = def.OneWayTileColor
	}
	if s.ContactColor == 0 {
		s.ContactColor = def.ContactColor
	}
	if s.NormalColor == 0 {
		s.NormalColor = def.NormalColor
	}
	if s.RayColor == 0 {
		s.RayColor = def.RayColor
	}
	if s.VelocityColor == 0 {
		s.VelocityColor = def.VelocityColor
	}
	if !isPositiveFinite(s.LineWidth) {
		s.LineWidth = def.LineWidth
	}
	if !isPositiveFinite(s.NormalLength) {
		s.NormalLength = def.NormalLength
	}
	if !isPositiveFinite(s.ContactRadius) {
		s.ContactRadius = def.ContactRadius
	}
	if !isPositiveFinite(s.VelocityScale) {
		s.VelocityScale = 0
	}

	zoom := s.Zoom
	if !isPositiveFinite(zoom) {
		zoom = drawingZoom(d)
	}

	normalLen := s.NormalLength / zoom
	arrowHead := min(normalLen*0.35, max(3.5, s.LineWidth*3)/zoom)

	return resolvedStyle{
		boxColor:        s.BoxColor,
		circleColor:     s.CircleColor,
		solidTileColor:  s.SolidTileColor,
		oneWayTileColor: s.OneWayTileColor,
		contactColor:    s.ContactColor,
		normalColor:     s.NormalColor,
		rayColor:        s.RayColor,
		velocityColor:   s.VelocityColor,
		zoom:            zoom,
		lineWidth:       s.LineWidth / zoom,
		normalLen:       normalLen,
		contactRadius:   s.ContactRadius / zoom,
		arrowHead:       arrowHead,
		velocityScale:   s.VelocityScale,
	}
}

func isPositiveFinite(v float32) bool {
	return v > 0 && !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

type linear2D struct{ a, b, c, d float64 }

// drawingZoom replays the transform stack recorded in d.Ops and returns the active uniform
// scale magnitude (or 1 when degenerate or identity).
func drawingZoom(d *core.Drawing) float32 {
	if d == nil || len(d.Ops) == 0 {
		return 1
	}
	cur := linear2D{a: 1, d: 1}
	var stack []linear2D
	for i := range d.Ops {
		op := &d.Ops[i]
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				cur = stack[n-1]
				stack = stack[:n-1]
			}
		case core.DrawScale:
			sx, sy := float64(op.X), float64(op.Y)
			cur = linear2D{
				a: cur.a * sx,
				b: cur.b * sx,
				c: cur.c * sy,
				d: cur.d * sy,
			}
		case core.DrawRotate:
			sin, cos := math.Sincos(float64(op.A))
			cur = linear2D{
				a: cur.a*cos + cur.c*sin,
				b: cur.b*cos + cur.d*sin,
				c: -cur.a*sin + cur.c*cos,
				d: -cur.b*sin + cur.d*cos,
			}
		case core.DrawTransform:
			na, nb, nc, nd := float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2)
			cur = linear2D{
				a: cur.a*na + cur.c*nb,
				b: cur.b*na + cur.d*nb,
				c: cur.a*nc + cur.c*nd,
				d: cur.b*nc + cur.d*nd,
			}
		case core.DrawSetTransform:
			cur = linear2D{
				a: float64(op.X1),
				b: float64(op.Y1),
				c: float64(op.X2),
				d: float64(op.Y2),
			}
		}
	}
	sx := math.Hypot(cur.a, cur.b)
	sy := math.Hypot(cur.c, cur.d)
	scale := math.Sqrt(sx * sy)
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 1
	}
	return float32(scale)
}

// DrawShape draws s as a wireframe outline in d using style: a rectangle for ShapeBox, or a
// circle with a +X orientation radius tick for ShapeCircle. It preserves d's drawing state.
func DrawShape(d *core.Drawing, s Shape, style DebugStyle) {
	if d == nil || s.Empty() {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	drawShape(d, s, st)
	d.Restore()
}

func drawShape(d *core.Drawing, s Shape, st resolvedStyle) {
	switch s.Kind {
	case ShapeBox:
		b := s.Box
		if b.Empty() {
			return
		}
		d.SetStrokeColor(st.boxColor)
		d.SetLineWidth(st.lineWidth)
		d.BeginPath()
		d.Rect(b.X, b.Y, b.W, b.H)
		d.Stroke()
	case ShapeCircle:
		c := s.Circle
		if c.Empty() {
			return
		}
		d.SetStrokeColor(st.circleColor)
		d.SetLineWidth(st.lineWidth)
		d.BeginPath()
		d.Arc(c.X, c.Y, c.R, 0, twoPi, false)
		d.MoveTo(c.X, c.Y)
		d.LineTo(c.X+c.R, c.Y)
		d.Stroke()
	}
}

// DrawBody draws b's collision shape and, when style.VelocityScale > 0, its velocity vector.
// It preserves d's drawing state.
func DrawBody(d *core.Drawing, b *Body, style DebugStyle) {
	if d == nil || b == nil || b.Shape.Empty() {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	drawBody(d, b, st)
	d.Restore()
}

func drawBody(d *core.Drawing, b *Body, st resolvedStyle) {
	if b == nil || b.Shape.Empty() {
		return
	}
	drawShape(d, b.Shape, st)
	if st.velocityScale > 0 && (b.VX != 0 || b.VY != 0) {
		var cx, cy float32
		switch b.Shape.Kind {
		case ShapeBox:
			cx, cy = b.Shape.Box.Center()
		case ShapeCircle:
			cx, cy = b.Shape.Circle.X, b.Shape.Circle.Y
		}
		ex := cx + b.VX*st.velocityScale
		ey := cy + b.VY*st.velocityScale
		drawArrow(d, cx, cy, ex, ey, st.velocityColor, st.lineWidth, st.arrowHead)
	}
}

// DrawContact draws a contact point marker and a normal arrow from (c.PointX, c.PointY) along
// (c.NormalX, c.NormalY) when c.Hit is true. It preserves d's drawing state.
func DrawContact(d *core.Drawing, c Contact, style DebugStyle) {
	if d == nil || !c.Hit {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	drawContactPoint(d, c.PointX, c.PointY, c.NormalX, c.NormalY, st.contactColor, st)
	d.Restore()
}

func drawContactPoint(d *core.Drawing, px, py, nx, ny float32, dotColor core.Color, st resolvedStyle) {
	d.SetFillColor(dotColor)
	d.BeginPath()
	d.Arc(px, py, st.contactRadius, 0, twoPi, false)
	d.Fill()

	if nlen := math.Hypot(float64(nx), float64(ny)); nlen > 0 {
		ux := float32(float64(nx) / nlen)
		uy := float32(float64(ny) / nlen)
		drawArrow(d, px, py, px+ux*st.normalLen, py+uy*st.normalLen, st.normalColor, st.lineWidth, st.arrowHead)
	}
}

func drawArrow(d *core.Drawing, x0, y0, x1, y1 float32, color core.Color, lineWidth, headLen float32) {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	length := math.Hypot(dx, dy)
	if length <= 0 {
		return
	}
	ux, uy := dx/length, dy/length
	px, py := -uy, ux

	hl := min(float64(headLen), length*0.45)
	hw := hl * 0.5
	leftX := float32(float64(x1) - ux*hl + px*hw)
	leftY := float32(float64(y1) - uy*hl + py*hw)
	rightX := float32(float64(x1) - ux*hl - px*hw)
	rightY := float32(float64(y1) - uy*hl - py*hw)

	d.SetStrokeColor(color)
	d.SetLineWidth(lineWidth)
	d.BeginPath()
	d.MoveTo(x0, y0)
	d.LineTo(x1, y1)
	d.MoveTo(leftX, leftY)
	d.LineTo(x1, y1)
	d.LineTo(rightX, rightY)
	d.Stroke()
}

// DrawTileContacts draws contact points and surface normal arrows for each TileContact returned
// by MoveAndSlide, using grid's tile dimensions to place each contact on the contacted tile face.
// It preserves d's drawing state.
func DrawTileContacts(d *core.Drawing, grid TileGrid, contacts []TileContact, style DebugStyle) {
	if d == nil || grid == nil || len(contacts) == 0 {
		return
	}
	tileW, tileH := grid.TileSize()
	if tileW <= 0 || tileH <= 0 {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	for _, tc := range contacts {
		cx := (float32(tc.Col) + 0.5) * tileW
		cy := (float32(tc.Row) + 0.5) * tileH
		px := cx + tc.NormalX*(tileW*0.5)
		py := cy + tc.NormalY*(tileH*0.5)
		drawContactPoint(d, px, py, tc.NormalX, tc.NormalY, st.contactColor, st)
	}
	d.Restore()
}

// DrawTileGrid draws solid and one-way collision tiles from grid that intersect visible.
// Solid tiles draw as full cell rectangles in SolidTileColor; one-way tiles draw as top-edge
// platforms with downward end ticks in OneWayTileColor. An empty visible bounds draws nothing.
// It preserves d's drawing state.
func DrawTileGrid(d *core.Drawing, grid TileGrid, visible core.Bounds, style DebugStyle) {
	if d == nil || grid == nil {
		return
	}
	minCol, maxCol, minRow, maxRow, tileW, tileH, ok := visibleTileRange(grid, visible)
	if !ok {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	drawTileGridRange(d, grid, minCol, maxCol, minRow, maxRow, tileW, tileH, st)
	d.Restore()
}

func visibleTileRange(grid TileGrid, visible core.Bounds) (minCol, maxCol, minRow, maxRow int, tileW, tileH float32, ok bool) {
	if grid == nil || visible.Size.Width <= 0 || visible.Size.Height <= 0 {
		return 0, 0, 0, 0, 0, 0, false
	}
	tileW, tileH = grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		return 0, 0, 0, 0, 0, 0, false
	}
	tw, th := float64(tileW), float64(tileH)
	visMinX := float64(visible.Origin.X)
	visMinY := float64(visible.Origin.Y)
	visMaxX := visMinX + float64(visible.Size.Width)
	visMaxY := visMinY + float64(visible.Size.Height)

	minCol = max(0, int(math.Floor(visMinX/tw)))
	maxCol = min(cols-1, int(math.Ceil(visMaxX/tw))-1)
	minRow = max(0, int(math.Floor(visMinY/th)))
	maxRow = min(rows-1, int(math.Ceil(visMaxY/th))-1)
	if minCol > maxCol || minRow > maxRow {
		return 0, 0, 0, 0, 0, 0, false
	}
	return minCol, maxCol, minRow, maxRow, tileW, tileH, true
}

func drawTileGridRange(d *core.Drawing, grid TileGrid, minCol, maxCol, minRow, maxRow int, tileW, tileH float32, st resolvedStyle) {
	// Pass 1: solid tile boxes.
	solidStarted := false
	for row := minRow; row <= maxRow; row++ {
		y := float32(row) * tileH
		for col := minCol; col <= maxCol; col++ {
			if grid.TileFlagsAt(col, row)&TileSolid == 0 {
				continue
			}
			if !solidStarted {
				d.SetStrokeColor(st.solidTileColor)
				d.SetLineWidth(st.lineWidth)
				d.BeginPath()
				solidStarted = true
			}
			d.Rect(float32(col)*tileW, y, tileW, tileH)
		}
	}
	if solidStarted {
		d.Stroke()
	}

	// Pass 2: one-way platform top edges with short downward end ticks.
	oneWayStarted := false
	tick := min(tileH*0.25, 4/st.zoom)
	if tick <= 0 {
		tick = tileH * 0.25
	}
	for row := minRow; row <= maxRow; row++ {
		y := float32(row) * tileH
		for col := minCol; col <= maxCol; col++ {
			flags := grid.TileFlagsAt(col, row)
			if flags&TileSolid != 0 || flags&TileOneWay == 0 {
				continue
			}
			if !oneWayStarted {
				d.SetStrokeColor(st.oneWayTileColor)
				d.SetLineWidth(st.lineWidth)
				d.BeginPath()
				oneWayStarted = true
			}
			x := float32(col) * tileW
			d.MoveTo(x, y+tick)
			d.LineTo(x, y)
			d.LineTo(x+tileW, y)
			d.LineTo(x+tileW, y+tick)
		}
	}
	if oneWayStarted {
		d.Stroke()
	}
}

// DrawWorld draws visible tiles from w.Tiles, every body in w.Bodies and every active contact
// with its normal arrow. It works directly inside a core.Canvas or debug overlay under any
// camera transform and preserves d's drawing state.
func DrawWorld(d *core.Drawing, w *World, visible core.Bounds, contacts []Contact, style DebugStyle) {
	if d == nil {
		return
	}
	hasTiles := false
	var minCol, maxCol, minRow, maxRow int
	var tileW, tileH float32
	if w != nil && w.Tiles != nil {
		minCol, maxCol, minRow, maxRow, tileW, tileH, hasTiles = visibleTileRange(w.Tiles, visible)
	}
	hasBodies := false
	if w != nil {
		for _, b := range w.Bodies {
			if b != nil && !b.Shape.Empty() {
				hasBodies = true
				break
			}
		}
	}
	hasContacts := false
	for _, c := range contacts {
		if c.Hit {
			hasContacts = true
			break
		}
	}
	if !hasTiles && !hasBodies && !hasContacts {
		return
	}

	st := resolveDebugStyle(d, style)
	d.Save()
	if hasTiles {
		drawTileGridRange(d, w.Tiles, minCol, maxCol, minRow, maxRow, tileW, tileH, st)
	}
	if hasBodies {
		for _, b := range w.Bodies {
			drawBody(d, b, st)
		}
	}
	if hasContacts {
		for _, c := range contacts {
			if c.Hit {
				drawContactPoint(d, c.PointX, c.PointY, c.NormalX, c.NormalY, st.contactColor, st)
			}
		}
	}
	d.Restore()
}

// DrawRaycast draws a ray segment from from to to in RayColor along with hit markers and
// surface normal arrows for each hit in hits. It preserves d's drawing state.
func DrawRaycast(d *core.Drawing, from, to core.Point, hits []RaycastHit, style DebugStyle) {
	if d == nil {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()
	d.SetStrokeColor(st.rayColor)
	d.SetLineWidth(st.lineWidth)
	d.BeginPath()
	d.MoveTo(float32(from.X), float32(from.Y))
	d.LineTo(float32(to.X), float32(to.Y))
	d.Stroke()

	for _, h := range hits {
		drawContactPoint(d, float32(h.Point.X), float32(h.Point.Y), h.NormalX, h.NormalY, st.rayColor, st)
	}
	d.Restore()
}

package main

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// hudReserve and barReserve are the heights the native controls cover at the top and bottom of the board view, which the board stays clear of.
	hudReserve = 104
	barReserve = 96
	// boardMargin is the least space beside the board.
	boardMargin = 16
	// maxCell is the largest a cell is drawn, in points.
	maxCell = 64
)

// The board's colors.
var (
	colorBackground = core.RGB(0x0b1020)
	colorFloor      = core.RGB(0x1c2541)
	colorWall       = core.RGB(0x3a506b)
	colorWallEdge   = core.RGB(0x5bc0be)
	colorVoid       = core.RGB(0x02040a)
	colorVoidRing   = core.RGBA(0x7c3aed, 0.55)
	colorLampOff    = core.RGB(0x475569)
	colorLampOn     = core.RGB(0xfde047)
	colorSpark      = core.RGB(0x67e8f9)
	colorSparkCore  = core.RGB(0xf0fdff)
)

// boardLayout places a board's cells in its view: the top left corner of cell (0, 0) and the size of a cell.
type boardLayout struct {
	x, y, cell float32
}

// layoutFor centers b in a view of size, in the space between the native controls, with square cells as large as fit up to maxCell.
func layoutFor(size core.Size, b *Board) boardLayout {
	w, h := float32(size.Width), float32(size.Height)
	cell := min((w-2*boardMargin)/float32(b.Width), (h-hudReserve-barReserve)/float32(b.Height), maxCell)
	cell = max(cell, 1)
	return boardLayout{
		x:    (w - cell*float32(b.Width)) / 2,
		y:    hudReserve + (h-hudReserve-barReserve-cell*float32(b.Height))/2,
		cell: cell,
	}
}

// cellAt returns the position p of the view in cells, and whether it is on the board's view at all.
func (l boardLayout) cellAt(p core.Point) (x, y float32, ok bool) {
	if l.cell <= 0 {
		return 0, 0, false
	}
	return (float32(p.X) - l.x) / l.cell, (float32(p.Y) - l.y) / l.cell, true
}

// center returns the view position of the middle of cell c.
func (l boardLayout) center(c Cell) core.Point {
	return core.Point{X: core.Points(l.x + (float32(c.X)+0.5)*l.cell), Y: core.Points(l.y + (float32(c.Y)+0.5)*l.cell)}
}

// draw paints the board into a view of size: its tiles, the lamps with their glow, and the spark.
func (g *sparkGame) draw(d *core.Drawing, size core.Size) {
	frame := g.frame.Get() // redraws on every frame the loop draws
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(colorBackground).FillRect(0, 0, w, h)
	if g.history == nil {
		return
	}
	b := g.board()
	l := layoutFor(size, b)
	gap := l.cell * 0.06
	for y := range b.Height {
		for x := range b.Width {
			c := Cell{x, y}
			px, py := l.x+float32(x)*l.cell, l.y+float32(y)*l.cell
			switch b.At(c) {
			case TileWall:
				d.SetFillColor(colorWall).BeginPath().RoundRect(px+gap/2, py+gap/2, l.cell-gap, l.cell-gap, l.cell*0.14).Fill()
				d.SetStrokeColor(colorWallEdge).SetLineWidth(max(l.cell*0.04, 1)).BeginPath().
					MoveTo(px+l.cell*0.2, py+l.cell*0.2).LineTo(px+l.cell*0.8, py+l.cell*0.2).Stroke()
			case TileVoid:
				d.SetFillColor(colorVoid).BeginPath().RoundRect(px+gap/2, py+gap/2, l.cell-gap, l.cell-gap, l.cell*0.14).Fill()
				d.SetStrokeColor(colorVoidRing).SetLineWidth(max(l.cell*0.05, 1)).BeginPath().
					Ellipse(px+l.cell/2, py+l.cell/2, l.cell*0.3, l.cell*0.3).Stroke()
			default:
				d.SetFillColor(colorFloor).BeginPath().RoundRect(px+gap/2, py+gap/2, l.cell-gap, l.cell-gap, l.cell*0.14).Fill()
			}
			if b.At(c) == TileLamp {
				g.drawLamp(d, px+l.cell/2, py+l.cell/2, l.cell, g.glow[y*b.Width+x])
			}
		}
	}
	g.drawSpark(d, l, frame)
}

// drawLamp paints a lamp centered on cx, cy, lit by glow from 0 (dark) to 1.
func (g *sparkGame) drawLamp(d *core.Drawing, cx, cy, cell, glow float32) {
	if glow > 0 {
		d.SetAlpha(0.35*glow).SetFillColor(colorLampOn).BeginPath().Ellipse(cx, cy, cell*0.42, cell*0.42).Fill().SetAlpha(1)
	}
	d.SetFillColor(colorLampOff).BeginPath().Ellipse(cx, cy, cell*0.2, cell*0.2).Fill()
	if glow > 0 {
		d.SetAlpha(glow).SetFillColor(colorLampOn).BeginPath().Ellipse(cx, cy, cell*0.2, cell*0.2).Fill().SetAlpha(1)
	}
}

// drawSpark paints the spark where its animation has it, pulsing with the frame count.
func (g *sparkGame) drawSpark(d *core.Drawing, l boardLayout, frame uint64) {
	if g.sparkScale <= 0 {
		return
	}
	pulse := 1 + 0.06*float32(math.Sin(float64(frame)*0.12))
	r := l.cell * 0.3 * g.sparkScale * pulse
	cx := l.x + (g.sparkX+g.bumpX+0.5)*l.cell
	cy := l.y + (g.sparkY+g.bumpY+0.5)*l.cell
	d.SetAlpha(0.3).SetFillColor(colorSpark).BeginPath().Ellipse(cx, cy, r*1.6, r*1.6).Fill().SetAlpha(1)
	d.SetFillColor(colorSpark).BeginPath().Ellipse(cx, cy, r, r).Fill()
	d.SetFillColor(colorSparkCore).BeginPath().Ellipse(cx, cy, r*0.45, r*0.45).Fill()
}

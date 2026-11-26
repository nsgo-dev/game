package main

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/nsgo/core"
)

// The card art is drawn by code, so the template needs no image files: a cream face with the rank in its corners and the suit's shape in the middle, and a patterned back for the cards of the draw pile.
var (
	tableColor    = core.RGB(0x14532d)
	slotColor     = core.RGBA(0xf8fafc, 0.18)
	faceColor     = core.RGB(0xfbf5e9)
	backColor     = core.RGB(0x1e3a5f)
	backLineColor = core.RGBA(0xf8fafc, 0.35)
	edgeColor     = core.RGBA(0x0f172a, 0.55)
	selectColor   = core.RGB(0xfacc15)
	acceptColor   = core.RGB(0x4ade80)
	refuseColor   = core.RGB(0xf87171)
)

const cardRadius = 7

// draw is the Canvas's Draw: the table scaled into the window by the viewport game.yaml's display sets.
func (g *cardGame) draw(d *core.Drawing, size core.Size) {
	_ = g.frame.Get()
	g.size = size
	g.viewport.Draw(d, size, g.drawTable)
}

// drawTable draws the table in virtual coordinates: the felt, an outline for every pile, the cards bottom first, the slot a held card would drop onto, and the selected card of the hand.
func (g *cardGame) drawTable(d *core.Drawing, size core.Size) {
	t := g.table
	b := t.board
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(tableColor).BeginPath().Rect(0, 0, w, h).Fill()

	card := b.Size()
	for i, l := range t.ladders {
		g.drawSlot(d, l, slotColor)
		r := l.Rect
		d.SetAlpha(0.35)
		drawSuit(d, i, g.colors[i], r.X+r.W/2, r.Y+r.H/2, card.X*0.22)
		d.SetAlpha(1)
	}
	g.drawSlot(d, t.deck, slotColor)
	if t.deck.Len() == 0 && t.redeals > 0 {
		// An empty draw pile that can be dealt again shows a ring around the redeals left.
		r := t.deck.Rect
		d.SetStrokeColor(slotColor).SetLineWidth(3).BeginPath().Ellipse(r.X+r.W/2, r.Y+r.H/2, card.X*0.28, card.X*0.28).Stroke()
		drawLabel(d, strconv.Itoa(t.redeals), r.X+r.W/2, r.Y+r.H/2, 18, slotColor)
	}
	g.drawSlot(d, t.discard, slotColor)

	g.order = b.DrawOrder(g.order[:0])
	for _, c := range g.order {
		g.drawCard(d, c)
		if c.Held() {
			if s, ok := b.DropTarget(c); s != nil && s != c.Slot() {
				color := refuseColor
				if ok {
					color = acceptColor
				}
				g.drawSlot(d, s, color)
			}
		}
	}
	if c := g.selectedCard(); c != nil && t.result == playing {
		p := c.Pose()
		d.Save().Translate(p.Pos.X, p.Pos.Y).Rotate(p.Rot)
		d.SetStrokeColor(selectColor).SetLineWidth(3).BeginPath().RoundRect(-card.X/2-3, -card.Y/2-3, card.X+6, card.Y+6, cardRadius+3).Stroke()
		d.Restore()
	}
}

// drawSlot outlines the area of s in color.
func (g *cardGame) drawSlot(d *core.Drawing, s *cards.Slot, color core.Color) {
	r := s.Rect
	if s == g.table.hand {
		d.SetStrokeColor(color).SetLineWidth(2).BeginPath().RoundRect(r.X, r.Y, r.W, r.H, 14).Stroke()
		return
	}
	d.SetStrokeColor(color).SetLineWidth(2).BeginPath().RoundRect(r.X, r.Y, r.W, r.H, cardRadius).Stroke()
}

// drawCard draws c where the board has it: face down on the draw pile and face up everywhere else.
func (g *cardGame) drawCard(d *core.Drawing, c *cards.Card) {
	size := g.table.board.Size()
	p := c.Pose()
	w, h := size.X, size.Y
	d.Save().Translate(p.Pos.X, p.Pos.Y).Rotate(p.Rot)
	if c.Held() {
		d.SetShadow(core.RGBA(0x000000, 0.45), 0, 6, 12)
	}
	if c.Slot() == g.table.deck {
		d.SetFillColor(backColor).BeginPath().RoundRect(-w/2, -h/2, w, h, cardRadius).Fill()
		d.SetShadow(0, 0, 0, 0)
		d.SetStrokeColor(backLineColor).SetLineWidth(1.5).BeginPath().RoundRect(-w/2+5, -h/2+5, w-10, h-10, cardRadius-2)
		for y := -h/2 + 14; y < h/2-8; y += 12 {
			d.MoveTo(-w/2+9, y).LineTo(w/2-9, y)
		}
		d.Stroke()
	} else {
		f := faceOf(c)
		color := g.colors[f.suit]
		d.SetFillColor(faceColor).BeginPath().RoundRect(-w/2, -h/2, w, h, cardRadius).Fill()
		d.SetShadow(0, 0, 0, 0)
		rank := strconv.Itoa(f.rank)
		drawLabel(d, rank, -w/2+11, -h/2+14, 16, color)
		drawLabel(d, rank, w/2-11, h/2-14, 16, color)
		drawSuit(d, f.suit, color, 0, 2, w*0.2)
	}
	d.SetStrokeColor(edgeColor).SetLineWidth(1).BeginPath().RoundRect(-w/2, -h/2, w, h, cardRadius).Stroke()
	d.Restore()
}

// drawLabel draws text centered on x, y.
func drawLabel(d *core.Drawing, text string, x, y float32, size core.Points, color core.Color) {
	d.SetFillColor(color).SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle)
	d.FillText(text, x, y, core.Font{Size: size, Weight: core.FontWeightBold})
}

// drawSuit draws the shape of suit number i, r points across from its middle at x, y: a flame's drop, a tide's circle, a leaf's diamond and a stone's square, in the order of rules.yaml.
func drawSuit(d *core.Drawing, i int, color core.Color, x, y, r float32) {
	d.SetFillColor(color).BeginPath()
	switch i % maxSuits {
	case 0:
		d.MoveTo(x, y-r*1.2)
		d.QuadTo(x+r*1.1, y, x, y+r)
		d.QuadTo(x-r*1.1, y, x, y-r*1.2)
	case 1:
		d.Arc(x, y, r, 0, 2*math.Pi, false)
	case 2:
		d.MoveTo(x, y-r*1.15).LineTo(x+r, y).LineTo(x, y+r*1.15).LineTo(x-r, y).ClosePath()
	default:
		d.RoundRect(x-r*0.85, y-r*0.85, r*1.7, r*1.7, r*0.25)
	}
	d.Fill()
}

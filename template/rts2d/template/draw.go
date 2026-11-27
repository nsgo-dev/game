package main

import (
	"math"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/rts"
	"github.com/nsgo-dev/nsgo/core"
)

// Everything is drawn by code in world points, under the viewport's transform: no image files, so the template runs as it is and a game swaps in sprites (game/sprite) one kind of thing at a time.

var (
	groundColor   = core.RGB(0x1c2a1e)
	rockColor     = core.RGB(0x4b5563)
	crystalColor  = core.RGB(0x67e8f9)
	padColor      = core.RGBA(0x94a3b8, 0.35)
	selectColor   = core.RGB(0xfef08a)
	cursorColor   = core.RGB(0xffffff)
	healthColor   = core.RGB(0x4ade80)
	damageColor   = core.RGB(0x7f1d1d)
	fogStyle      = fog.Style{Unseen: core.RGBA(0x020617, 0.92), Explored: core.RGBA(0x020617, 0.55)}
	boxFillColor  = core.RGBA(0xfef08a, 0.12)
	boxEdgeColor  = core.RGBA(0xfef08a, 0.8)
	flashColor    = core.RGB(0xffffff)
	shotColor     = core.RGBA(0xfde68a, 0.9)
	constructTint = core.RGBA(0x0b1120, 0.55)
)

// draw paints the battlefield as the player sees it.
func (g *skirmish) draw(d *core.Drawing) {
	w := g.w
	size := w.size()
	d.SetFillColor(groundColor).FillRect(0, 0, size.X, size.Y)
	g.drawTerrain(d)
	for _, e := range w.ents {
		if e.building != nil && w.visibleTo(player, e) {
			g.drawBuilding(d, e)
		}
	}
	for _, e := range w.ents {
		if e.unit != nil && w.visibleTo(player, e) {
			g.drawUnit(d, e)
		}
	}
	w.fog.Draw(d, fog.Team(player), nil, fogStyle)
	if r, ok := g.box.Box(); ok {
		rts.DrawSelectionBox(d, r, boxFillColor, boxEdgeColor, 1)
	}
	if g.cursorShown && g.playing() {
		c := g.cursor
		d.SetStrokeColor(cursorColor).SetLineWidth(1.2).BeginPath().
			MoveTo(c.X-6, c.Y).LineTo(c.X-2, c.Y).MoveTo(c.X+2, c.Y).LineTo(c.X+6, c.Y).
			MoveTo(c.X, c.Y-6).LineTo(c.X, c.Y-2).MoveTo(c.X, c.Y+2).LineTo(c.X, c.Y+6).Stroke()
	}
}

// drawTerrain paints rocks, crystal cells and empty build pads.
func (g *skirmish) drawTerrain(d *core.Drawing) {
	w := g.w
	m := g.data.battleMap
	d.SetFillColor(rockColor).BeginPath()
	for row := range m.rows {
		for col := range m.cols {
			if t, _ := m.tile(col, row); t == tileRock {
				d.Rect(float32(col)*w.cell, float32(row)*w.cell, w.cell, w.cell)
			}
		}
	}
	d.Fill()
	d.SetFillColor(crystalColor).BeginPath()
	for _, n := range w.nodes {
		if n.left == 0 {
			continue
		}
		c := w.cellCenter(n.cell)
		// A cell shrinks as it is mined out.
		r := w.cell * (0.2 + 0.25*float32(n.left)/float32(w.r.CrystalsPerNode))
		d.MoveTo(c.X, c.Y-r).LineTo(c.X+r, c.Y).LineTo(c.X, c.Y+r).LineTo(c.X-r, c.Y).ClosePath()
	}
	d.Fill()
	size := float32(w.r.building(barracksName).Size) * w.cell
	d.SetStrokeColor(padColor).SetLineWidth(1).SetLineDash(3, 2).BeginPath()
	for _, t := range []team{player, opponent} {
		s := w.sides[t]
		for i, c := range s.pads {
			if !s.padUsed[i] {
				d.Rect(float32(c.Col)*w.cell+1, float32(c.Row)*w.cell+1, size-2, size-2)
			}
		}
	}
	d.Stroke().SetLineDash()
}

// drawBuilding paints a headquarters or a barracks, with its construction or production progress and its health, as the player knows them: an enemy building out of sight shows as the player last saw it.
func (g *skirmish) drawBuilding(d *core.Drawing, b *entity) {
	w := g.w
	r := w.buildingRect(b)
	hp, buildLeft := w.shownTo(player, b)
	col := b.team.color()
	if b.flash > 0 && (b.team == player || w.inSight(player, b)) {
		col = flashColor
	}
	d.SetFillColor(col.Faded(0.35)).FillRect(r.X+1, r.Y+1, r.W-2, r.H-2)
	d.SetStrokeColor(col).SetLineWidth(1.5).StrokeRect(r.X+1, r.Y+1, r.W-2, r.H-2)
	c := r.Center()
	if b.building.Name == hqBuildingName {
		// A headquarters wears a crystal on its roof.
		s := r.W * 0.18
		d.SetFillColor(col).BeginPath().MoveTo(c.X, c.Y-s).LineTo(c.X+s, c.Y).LineTo(c.X, c.Y+s).LineTo(c.X-s, c.Y).ClosePath().Fill()
	} else {
		d.SetStrokeColor(col).SetLineWidth(1).BeginPath().MoveTo(r.X+4, r.Y+4).LineTo(r.X+r.W-4, r.Y+r.H-4).MoveTo(r.X+r.W-4, r.Y+4).LineTo(r.X+4, r.Y+r.H-4).Stroke()
	}
	if buildLeft > 0 {
		d.SetFillColor(constructTint).FillRect(r.X+1, r.Y+1, r.W-2, r.H-2)
		total := float32(secondsToSteps(b.building.BuildSeconds))
		bar(d, r.X+2, r.Y+r.H-5, r.W-4, 1-float32(buildLeft)/total, selectColor)
	} else if len(b.queue) > 0 && b.team == player {
		total := float32(secondsToSteps(b.queue[0].TrainSeconds))
		bar(d, r.X+2, r.Y+r.H-5, r.W-4, 1-float32(b.trainLeft)/total, selectColor)
	}
	if hp < b.maxHP {
		bar(d, r.X+2, r.Y-4, r.W-4, float32(hp)/float32(b.maxHP), healthColor)
	}
}

// drawUnit paints a unit by its type: a worker is a circle, a soldier a square and a ranger a triangle.
func (g *skirmish) drawUnit(d *core.Drawing, e *entity) {
	w := g.w
	p := w.pos(e)
	rad := e.unit.Radius
	col := e.team.color()
	if e.flash > 0 {
		col = flashColor
	}
	if e.shot > 0 && e.unit.Range > 2*w.cell/3 {
		d.SetStrokeColor(shotColor).SetLineWidth(0.8).BeginPath().MoveTo(p.X, p.Y).LineTo(e.shotAt.X, e.shotAt.Y).Stroke()
	}
	d.SetFillColor(col).BeginPath()
	switch e.unit.Name {
	case soldierName:
		d.Rect(p.X-rad, p.Y-rad, 2*rad, 2*rad)
	case rangerName:
		d.MoveTo(p.X, p.Y-rad*1.2).LineTo(p.X+rad, p.Y+rad*0.8).LineTo(p.X-rad, p.Y+rad*0.8).ClosePath()
	default:
		d.Ellipse(p.X, p.Y, rad, rad)
	}
	d.Fill()
	if e.carry > 0 {
		d.SetFillColor(crystalColor).FillRect(p.X-1.5, p.Y-rad-3, 3, 3)
	}
	selected := e.team == player && g.sel.Contains(e.body)
	focused := g.focus > 0 && e.team == player && containsID(g.groups.Group(g.focusGroup), e.body)
	if selected || focused {
		ring := selectColor
		if focused && g.focus%10 < 5 {
			ring = flashColor
		}
		d.SetStrokeColor(ring).SetLineWidth(1).BeginPath().Ellipse(p.X, p.Y, rad+2, rad+2).Stroke()
	}
	if e.hp < e.maxHP {
		bar(d, p.X-rad-1, p.Y+rad+2, 2*rad+2, float32(e.hp)/float32(e.maxHP), healthColor)
	}
}

// bar paints a one-and-a-half point high bar filled to fraction f.
func bar(d *core.Drawing, x, y, width, f float32, fill core.Color) {
	f = float32(math.Max(0, math.Min(1, float64(f))))
	d.SetFillColor(damageColor).FillRect(x, y, width, 1.5)
	d.SetFillColor(fill).FillRect(x, y, width*f, 1.5)
}

func containsID(ids []rts.UnitID, id rts.UnitID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

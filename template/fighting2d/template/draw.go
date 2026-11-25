package main

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/framedata"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// The arena is drawn by code from the frame data itself: a fighter's first hurtbox is its head and the others its body, its active hitboxes are the strikes, and a projectile is the glow of its hitbox. Changing a move's boxes changes how it looks, so what the player sees is what can hit and be hit.

var (
	skyTop     = core.RGB(0x1e1b4b)
	skyLow     = core.RGB(0x7c3aed)
	moonColor  = core.RGB(0xfde68a)
	ridgeColor = core.RGB(0x312e81)
	floorColor = core.RGB(0x1f2937)
	lineColor  = core.RGBA(0x94a3b8, 0.25)
	hurtLine   = core.RGBA(0x22c55e, 0.9)
	hitLine    = core.RGBA(0xef4444, 0.95)
)

// shapes draws the hitbox overlay F1 toggles; release builds compile it out.
var shapes debug.ShapeOverlay

// draw paints the arena into the Canvas, scaled into the virtual resolution game.yaml's display sets.
func (g *fightingGame) draw(d *core.Drawing, size core.Size) {
	g.viewSize = size
	g.viewport.Draw(d, size, func(d *core.Drawing, vs core.Size) {
		w, h := float32(vs.Width), float32(vs.Height)
		floor := g.roster.file.Rules.Floor
		drawStage(d, w, h, floor)
		if m := g.m; m != nil {
			for _, p := range m.players {
				drawShadow(d, p, floor)
			}
			for _, p := range m.players {
				drawFighter(d, p, m.steps)
			}
			for _, s := range m.shots {
				drawShot(d, s)
			}
			for _, s := range m.sparks {
				drawSpark(d, s)
			}
			if g.hitboxes {
				for _, p := range m.players {
					shapes.DrawFighterBoxes(d, &p.f, hurtLine, hitLine)
				}
				for _, s := range m.shots {
					shapes.DrawFighterBoxes(d, &s.f, hurtLine, hitLine)
				}
			}
		}
		if g.scr == screenFight && g.showTouch() {
			g.drawTouchControls(d)
		}
	})
}

// showTouch reports whether to draw the touch controls: on a device whose pointer is a finger, or once the screen has been touched.
func (g *fightingGame) showTouch() bool {
	return g.controls.touched || core.Untracked(g.cx, func() bool { return g.cx.Env().Pointer() == core.PointerCoarse })
}

// drawStage paints a dusk sky, a moon, a ridge line and the floor.
func drawStage(d *core.Drawing, w, h, floor float32) {
	bands := 6
	for i := range bands {
		t := float32(i) / float32(bands-1)
		d.SetFillColor(mix(skyTop, skyLow, t)).FillRect(0, floor*float32(i)/float32(bands), w, floor/float32(bands)+1)
	}
	d.SetFillColor(moonColor.Faded(0.9)).BeginPath().Arc(w*0.78, 52, 22, 0, 2*math.Pi, false).Fill()
	d.SetFillColor(ridgeColor).BeginPath().MoveTo(0, floor)
	for x := float32(0); x <= w; x += 40 {
		y := floor - 40 - 26*float32(math.Sin(float64(x)/57)) - 14*float32(math.Sin(float64(x)/23))
		d.LineTo(x, y)
	}
	d.LineTo(w, floor).ClosePath().Fill()
	d.SetFillColor(floorColor).FillRect(0, floor, w, h-floor)
	d.SetStrokeColor(lineColor).SetLineWidth(1).BeginPath()
	for x := float32(-200); x < w+200; x += 48 {
		d.MoveTo(w/2+(x-w/2)*0.6, floor).LineTo(x, h)
	}
	d.Stroke()
}

// mix blends a toward b by t.
func mix(a, b core.Color, t float32) core.Color {
	ar, ag, ab, _ := a.Components()
	br, bg, bb, _ := b.Components()
	l := func(x, y float64) uint32 { return uint32(math.Round((x + (y-x)*float64(t)) * 255)) }
	return core.RGB(l(ar, br)<<16 | l(ag, bg)<<8 | l(ab, bb))
}

func drawShadow(d *core.Drawing, p *player, floor float32) {
	lift := (floor - p.f.Y) / 120
	d.SetFillColor(core.RGBA(0x000000, 0.35*(1-min(lift, 0.8)))).BeginPath().Ellipse(p.f.X, floor+3, 22*(1-lift*0.4), 4).Fill()
}

// drawFighter draws a fighter from the boxes of its current frame. Frames without hurtboxes, where the fighter cannot be hit, show its stance's outline faded instead, and a knocked-out fighter lies on the floor.
func drawFighter(d *core.Drawing, p *player, steps int) {
	ft := p.fighter
	if p.moveName() == "ko" && !p.airborne {
		drawKnockedOut(d, p)
		return
	}
	var buf [12]framedata.WorldBox
	boxes := p.f.Boxes(buf[:0])
	hurts := 0
	for _, b := range boxes {
		if b.Kind == framedata.KindHurt {
			hurts++
		}
	}
	body := ft.color
	if hurts == 0 {
		stance := "idle"
		if p.airborne {
			stance = "jump"
		}
		boxes = stanceBoxes(p, stance, boxes)
		body = body.Faded(0.55)
	}
	if p.moveName() == "hurt" && steps/3%2 == 0 {
		body = core.RGB(0xffffff)
	}
	var torso core.Point
	head := true
	for _, b := range boxes {
		if b.Kind != framedata.KindHurt {
			continue
		}
		r := b.Rect
		if head {
			head = false
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			d.SetFillColor(body).BeginPath().Arc(cx, cy, r.W/2+1, 0, 2*math.Pi, false).Fill()
			d.SetFillColor(core.RGB(0x0f172a)).BeginPath().Arc(cx+p.forward()*r.W*0.22, cy-1, 2, 0, 2*math.Pi, false).Fill()
			continue
		}
		if torso == (core.Point{}) {
			torso = core.Point{X: core.Points(r.X + r.W/2), Y: core.Points(r.Y + r.H/3)}
		}
		d.SetFillColor(body).BeginPath().RoundRect(r.X, r.Y, r.W, r.H, 5).Fill()
	}
	for _, b := range boxes {
		if b.Kind != framedata.KindHit {
			continue
		}
		r := b.Rect
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		d.SetStrokeColor(ft.accent).SetLineWidth(5).SetLineCap(core.CapRound).BeginPath().MoveTo(float32(torso.X), float32(torso.Y)).LineTo(cx, cy).Stroke()
		d.SetFillColor(ft.accent).BeginPath().RoundRect(r.X, r.Y, r.W, r.H, min(r.W, r.H)/2).Fill()
	}
	if p.blocking() {
		x := p.f.X + p.forward()*20
		y := p.f.Y - 50
		if p.moveName() == "crouch_guard" {
			y = p.f.Y - 28
		}
		a0 := float32(-math.Pi / 2.6)
		if p.f.FacingLeft {
			a0 += math.Pi
		}
		d.SetStrokeColor(ft.accent.Faded(0.85)).SetLineWidth(3).BeginPath().Arc(x-p.forward()*8, y, 24, a0, a0+2*math.Pi/2.6, false).Stroke()
	}
}

// stanceBoxes places the hurtboxes of the stance move's first frame for p, to draw it on frames it has none.
func stanceBoxes(p *player, stance string, dst []framedata.WorldBox) []framedata.WorldBox {
	mv, _ := p.fighter.frames.Move(stance)
	for i, b := range mv.Boxes {
		if b.Kind != framedata.KindHurt || !b.Active(1) {
			continue
		}
		x := p.f.X + b.X
		if p.f.FacingLeft {
			x = p.f.X - b.X - b.Width
		}
		dst = append(dst, framedata.WorldBox{Kind: b.Kind, Index: i, Rect: math2d.Rect{X: x, Y: p.f.Y + b.Y, W: b.Width, H: b.Height}})
	}
	return dst
}

func drawKnockedOut(d *core.Drawing, p *player) {
	x, y := p.f.X, p.f.Y
	d.SetFillColor(p.fighter.color.Faded(0.8)).BeginPath().RoundRect(x-30, y-14, 60, 14, 6).Fill()
	d.BeginPath().Arc(x-p.forward()*38, y-9, 9, 0, 2*math.Pi, false).Fill()
}

func drawShot(d *core.Drawing, s *projectile) {
	var buf [4]framedata.WorldBox
	for _, b := range s.f.Boxes(buf[:0]) {
		r := b.Rect
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		d.SetFillColor(s.owner.fighter.accent.Faded(0.35)).BeginPath().Ellipse(cx, cy, r.W*0.9, r.H*0.8).Fill()
		d.SetFillColor(s.owner.fighter.accent).BeginPath().Ellipse(cx, cy, r.W/2, r.H/2).Fill()
		d.SetFillColor(core.RGB(0xffffff)).BeginPath().Ellipse(cx, cy, r.W/5, r.H/5).Fill()
	}
}

func drawSpark(d *core.Drawing, s spark) {
	t := 1 - float32(s.life)/sparkLife
	c := core.RGB(0xfde047)
	if s.blocked {
		c = core.RGB(0x7dd3fc)
	}
	d.SetStrokeColor(c.Faded(1-t)).SetLineWidth(3).BeginPath().Arc(s.x, s.y, 6+18*t, 0, 2*math.Pi, false).Stroke()
	for i := range 6 {
		a := float64(i) * math.Pi / 3
		r0, r1 := 4+14*t, 10+22*t
		cos, sin := float32(math.Cos(a)), float32(math.Sin(a))
		d.BeginPath().MoveTo(s.x+cos*r0, s.y+sin*r0).LineTo(s.x+cos*r1, s.y+sin*r1).Stroke()
	}
}

// drawTouchControls draws the on-screen stick and the two attack buttons at the regions game.yaml binds them to.
func (g *fightingGame) drawTouchControls(d *core.Drawing) {
	g.controls.stick.Draw(d, input.DefaultVirtualStyle())
	font := core.Font{Size: 16, Weight: core.FontWeightBold}
	d.SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle)
	for _, b := range []struct{ action, label string }{{"p1_light", "L"}, {"p1_heavy", "H"}} {
		r, ok := Project.Region(b.action)
		if !ok {
			continue
		}
		cx, cy := float32(r.Origin.X+r.Size.Width/2), float32(r.Origin.Y+r.Size.Height/2)
		fill := core.RGBA(0xffffff, 0.18)
		if g.controls.actions[0].Pressed(b.action) {
			fill = core.RGBA(0xffffff, 0.4)
		}
		d.SetFillColor(fill).BeginPath().Arc(cx, cy, float32(r.Size.Width)/2, 0, 2*math.Pi, false).Fill()
		d.SetFillColor(core.RGBA(0xffffff, 0.85)).FillText(b.label, cx, cy, font)
	}
}

package main

import (
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/core"
)

// stageSize is the game's virtual canvas, game.yaml's display: everything is drawn at this size and scaled to the window.
var stageSize = core.Size{Width: core.Points(Project.Display.Width), Height: core.Points(Project.Display.Height)}

// talkStyle is how conversations look: the text box along the bottom with the speaker's portrait standing on it, and the options above.
func talkStyle(c *content) vn.Style {
	return vn.Style{
		Atlas:          c.atlas,
		PortraitHeight: 72,
		PortraitBottom: 84,
		InactiveAlpha:  0.6,
		BoxColor:       core.RGBA(0x10131f, 0.94),
		BoxHeight:      76,
		Margin:         8,
		Padding:        12,
		TextFont:       core.Font{Size: 12},
		NameFont:       core.Font{Size: 11, Weight: core.FontWeightBold},
		NamePlateColor: core.RGB(0x3b4b6b),
		ChoiceWidth:    300,
		ChoiceHeight:   24,
		ChoiceGap:      6,
		ChoiceColor:    core.RGBA(0x2a3150, 0.96),
	}
}

// drawFrame draws the atlas frame name with its top-left corner at (x, y), size points wide and high, mirrored when flip is set.
func (g *Game) drawFrame(d *core.Drawing, name string, x, y, size float32, flip bool) {
	f, ok := g.c.atlas.Frames[name]
	if !ok {
		return
	}
	if flip {
		d.Save().Translate(x+size, y).Scale(-1, 1)
		d.DrawImageFrom(g.c.atlas.Image, f.X, f.Y, f.W, f.H, 0, 0, size, size)
		d.Restore()
		return
	}
	d.DrawImageFrom(g.c.atlas.Image, f.X, f.Y, f.W, f.H, x, y, size, size)
}

// cellPoint returns the top-left corner of a figure walking from one cell to another with left of stepTime still to go.
func (g *Game) cellPoint(from, to Cell, left float32) (float32, float32) {
	size := float32(g.c.world.TileSize)
	t := 1 - left/float32(stepTime)
	x := float32(from.X()) + (float32(to.X())-float32(from.X()))*t
	y := float32(from.Y()) + (float32(to.Y())-float32(from.Y()))*t
	return x * size, y * size
}

// drawMap draws the current map with its items, people, foes and the player, the camera following the player.
func (g *Game) drawMap(d *core.Drawing) {
	size := float32(g.c.world.TileSize)
	tm := g.tiles[g.mapID]
	px, py := g.cellPoint(g.from, g.pos, float32(g.walk))
	bounds := tm.WorldBounds()
	cam := tilemap.Camera{X: px + size/2, Y: py + size/2, Width: float32(stageSize.Width), Height: float32(stageSize.Height), Bounds: &bounds}
	cam.Clamp()

	d.Save()
	d.SetImageSmoothing(false)
	cam.Apply(d)
	tm.Draw(d, &cam)
	for _, it := range g.level.Items {
		if !g.taken[it.ID] {
			g.drawFrame(d, g.c.itemArt(it.Item), float32(it.At.X())*size, float32(it.At.Y())*size, size, false)
		}
	}
	for _, p := range g.level.People {
		g.drawFrame(d, p.Art, float32(p.At.X())*size, float32(p.At.Y())*size, size, false)
	}
	for _, f := range g.foes {
		x, y := g.cellPoint(f.from, f.at, float32(f.walk))
		g.drawFrame(d, f.kind.Art, x, y, size, f.at.X() < f.from.X())
	}
	g.drawFrame(d, "hero", px, py, size, g.facing == dirLeft)
	d.Restore()
}

// drawField draws the map, the on-screen pad when touch is set and, during a conversation, its text box, portraits and options.
func (g *Game) drawField(d *core.Drawing, touch bool) {
	g.drawMap(d)
	if g.mode == modeTalk {
		st := talkStyle(g.c)
		g.talk.Draw(d, stageSize, st)
		if choices := g.talk.Choices(); len(choices) > 0 && g.choice < len(choices) {
			// vn draws the options but not which one keyboard and gamepad players have selected, so the game rings it.
			b := st.Layout(stageSize, len(choices)).Choices[g.choice]
			d.SetStrokeColor(core.RGB(0xf2d14b)).SetLineWidth(2).BeginPath().
				RoundRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height), 10).Stroke()
		}
		return
	}
	if touch && g.mode == modeField {
		drawPads(d)
	}
}

// drawPads draws the on-screen pad where game.yaml's touch regions are: arrows for the four directions, A to confirm and B to cancel.
func drawPads(d *core.Drawing) {
	labels := map[string]string{actionUp: "▲", actionDown: "▼", actionLeft: "◀", actionRight: "▶", actionConfirm: "A", actionCancel: "B"}
	for _, name := range padActions {
		r, ok := Project.Region(name)
		if !ok {
			continue
		}
		x, y, w, h := float32(r.Origin.X), float32(r.Origin.Y), float32(r.Size.Width), float32(r.Size.Height)
		d.SetFillColor(core.RGBA(0x0f172a, 0.45)).BeginPath().RoundRect(x+2, y+2, w-4, h-4, 8).Fill()
		text.DrawPlatform(d, labels[name], x+w/2, y+h/2, text.PlatformStyle{
			Font:     core.Font{Size: 14, Weight: core.FontWeightBold},
			Color:    core.RGBA(0xffffff, 0.85),
			Align:    text.AlignCenter,
			Baseline: core.BaselineMiddle,
		})
	}
}

// drawBattle draws the fight: the foe, large, over a meadow, and both sides' HP.
func (g *Game) drawBattle(d *core.Drawing) {
	w, h := float32(stageSize.Width), float32(stageSize.Height)
	d.SetFillColor(core.RGB(0x1d2a44)).FillRect(0, 0, w, h)
	d.SetFillColor(core.RGB(0x2f5a34)).FillRect(0, h*0.42, w, h*0.58)
	b := g.battle
	if b == nil {
		return
	}
	size := float32(96)
	d.Save()
	d.SetImageSmoothing(false)
	if b.hit > 0 {
		d.SetAlpha(0.4)
	}
	g.drawFrame(d, b.foe.kind.Art, (w-size)/2, 24, size, false)
	d.Restore()
	drawBar(d, (w-160)/2, 128, 160, b.hp, b.foe.kind.HP, core.RGB(0xe85d5d))
	if b.hurt > 0 {
		d.SetFillColor(core.RGBA(0xe85d5d, 0.25)).FillRect(0, 0, w, h)
	}
}

// drawBar draws a bar 8 points high filled to n of most.
func drawBar(d *core.Drawing, x, y, w float32, n, most int, fill core.Color) {
	d.SetFillColor(core.RGBA(0x000000, 0.5)).BeginPath().RoundRect(x, y, w, 8, 4).Fill()
	if most > 0 && n > 0 {
		d.SetFillColor(fill).BeginPath().RoundRect(x, y, w*float32(n)/float32(most), 8, 4).Fill()
	}
}

// drawShade draws the map under a tint, behind the title, defeat and victory menus.
func (g *Game) drawShade(d *core.Drawing, tint core.Color) {
	g.drawMap(d)
	d.SetFillColor(tint).FillRect(0, 0, float32(stageSize.Width), float32(stageSize.Height))
}

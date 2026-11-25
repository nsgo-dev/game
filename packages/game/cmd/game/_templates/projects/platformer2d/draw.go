package main

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// painter draws a run: the sky, the tiles through the camera, the sparks, lamps and beacon, the player's sprite, the room fade and the on-screen controls.
type painter struct {
	tiles *tilemap.Tilemap
	atlas *sprite.Atlas
	run   sprite.Animation
	hero  sprite.Sprite
	batch *sprite.Batch
}

// newPainter returns a painter for w's level.
func newPainter(w *world) *painter {
	atlas := newPlayerAtlas()
	p := &painter{
		tiles: newTilemap(w.level),
		atlas: atlas,
		run:   atlas.Animations["run"],
		hero:  sprite.NewSprite(atlas.Frames["stand"]),
	}
	p.batch = &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&p.hero}}
	return p
}

// update picks the player's frame for the step w just played: in the air, running or standing.
func (p *painter) update(w *world, dt time.Duration) {
	b := w.body
	switch {
	case !b.Grounded:
		p.hero.Frame = p.atlas.Frames["jump"]
	case b.VX > 5 || b.VX < -5:
		p.run.Update(dt)
		p.hero.Frame = p.run.Frame()
	default:
		p.run.Reset()
		p.hero.Frame = p.atlas.Frames["stand"]
	}
	p.hero.FlipX = w.facing < 0
}

// draw draws w on the virtual screen of size.
func (p *painter) draw(d *core.Drawing, w *world, size core.Size) {
	vw, vh := float32(size.Width), float32(size.Height)
	cam := &w.camera

	// The sky, and two rows of hills that scroll slower than the level, nearer ones faster.
	d.SetFillColor(core.RGB(0x1b2a4e)).FillRect(0, 0, vw, vh)
	d.SetFillColor(core.RGB(0x2c3f6b)).FillRect(0, vh*0.55, vw, vh*0.45)
	drawHills(d, vw, cam.X*0.2, vh*0.62, 70, core.RGB(0x263a63))
	drawHills(d, vw, cam.X*0.45, vh*0.78, 52, core.RGB(0x22345a))

	d.Save()
	cam.Apply(d)
	p.tiles.Draw(d, cam)
	t := float32(w.steps) / 60
	tile := w.tile
	for _, s := range w.sparks {
		if s.taken {
			continue
		}
		x := (float32(s.col) + 0.5) * tile
		y := (float32(s.row)+0.5)*tile + float32(math.Sin(float64(t*3+float32(s.col))))*1.5
		d.SetFillColor(core.RGBA(0xffd14d, 0.25)).BeginPath().Ellipse(x, y, 6, 6).Fill()
		d.SetFillColor(core.RGB(0xffe58a)).BeginPath().
			MoveTo(x, y-4).LineTo(x+3, y).LineTo(x, y+4).LineTo(x-3, y).ClosePath().Fill()
	}
	for _, cp := range w.checkpoints {
		drawLamp(d, float32(cp.col)*tile, float32(cp.row+1)*tile, tile, cp.lit)
	}
	drawBeacon(d, float32(w.goal[0])*tile, float32(w.goal[1]+1)*tile, tile, t, w.state == won)

	b := w.body.Bounds()
	p.hero.X = b.X + b.W/2
	p.hero.Y = b.Bottom()
	p.batch.Draw(d)
	d.Restore()

	w.director.DrawFade(d)
}

// drawHills draws a row of round hills across the screen, offset by scroll.
func drawHills(d *core.Drawing, width, scroll, top, radius float32, c core.Color) {
	d.SetFillColor(c).BeginPath()
	span := radius * 2.6
	start := -float32(math.Mod(float64(scroll), float64(span))) - span
	for x := start; x < width+span; x += span {
		d.Ellipse(x, top+radius, radius*1.3, radius)
	}
	d.Fill()
}

// drawLamp draws a checkpoint lamp post standing on bottom, lit or dark.
func drawLamp(d *core.Drawing, x, bottom, tile float32, lit bool) {
	cx := x + tile/2
	d.SetFillColor(core.RGB(0x4b5563)).FillRect(cx-1, bottom-tile*1.6, 2, tile*1.6)
	glass := core.RGB(0x64748b)
	if lit {
		d.SetFillColor(core.RGBA(0xffd14d, 0.3)).BeginPath().Ellipse(cx, bottom-tile*1.7, 9, 9).Fill()
		glass = core.RGB(0xffd14d)
	}
	d.SetFillColor(glass).BeginPath().RoundRect(cx-4, bottom-tile*1.95, 8, 8, 2).Fill()
}

// drawBeacon draws the goal: a stone plinth with a flame that grows once the run is won.
func drawBeacon(d *core.Drawing, x, bottom, tile, t float32, lit bool) {
	cx := x + tile/2
	d.SetFillColor(core.RGB(0x94a3b8)).FillRect(cx-6, bottom-10, 12, 10)
	d.SetFillColor(core.RGB(0x64748b)).FillRect(cx-8, bottom-13, 16, 3)
	flame := float32(6 + math.Sin(float64(t*8)))
	if lit {
		flame = 11
	}
	d.SetFillColor(core.RGBA(0xff9f43, 0.35)).BeginPath().Ellipse(cx, bottom-13-flame/2, flame, flame).Fill()
	d.SetFillColor(core.RGB(0xffd14d)).BeginPath().
		MoveTo(cx, bottom-13-flame*1.4).QuadTo(cx+5, bottom-15, cx, bottom-13).QuadTo(cx-5, bottom-15, cx, bottom-13-flame*1.4).Fill()
}

// drawTouchControls draws the regions game.yaml binds left, right and jump to, brighter while held.
func (p *painter) drawTouchControls(d *core.Drawing, a *input.Actions) {
	for _, name := range []string{"left", "right", actionJump} {
		r, ok := Project.Region(name)
		if !ok {
			continue
		}
		x, y := float32(r.Origin.X)+4, float32(r.Origin.Y)+4
		w, h := float32(r.Size.Width)-8, float32(r.Size.Height)-8
		alpha := float32(0.22)
		if a.Pressed(name) {
			alpha = 0.45
		}
		d.SetFillColor(core.RGBA(0xf8fafc, alpha)).BeginPath().RoundRect(x, y, w, h, 8).Fill()
		cx, cy := x+w/2, y+h/2
		d.SetFillColor(core.RGBA(0xf8fafc, 0.85)).BeginPath()
		switch name {
		case "left":
			d.MoveTo(cx+5, cy-8).LineTo(cx-6, cy).LineTo(cx+5, cy+8)
		case "right":
			d.MoveTo(cx-5, cy-8).LineTo(cx+6, cy).LineTo(cx-5, cy+8)
		default:
			d.MoveTo(cx-9, cy+5).LineTo(cx, cy-7).LineTo(cx+9, cy+5)
		}
		d.ClosePath().Fill()
	}
}

package main

import (
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/procgen"
	"github.com/nsgo-dev/nsgo/core"
)

// tileSize is a cell's side in virtual screen points.
const tileSize = 10

// The palette, as 0xrrggbb.
const (
	colorVoid      = 0x05070d
	colorWall      = 0x3b4256
	colorWallSeen  = 0x1d2130
	colorFloor     = 0x161b29
	colorFloorSeen = 0x0d111b
	colorFloorDot  = 0x2a3247
	colorPlayer    = 0xf8fafc
	colorStairs    = 0xe2e8f0
	colorRelic     = 0xfb923c
	colorHealth    = 0xef4444
)

// glyphFont draws the letters of the player, the monsters and the items.
var glyphFont = core.Font{Family: core.FontMonospace, Size: 9, Weight: core.FontWeightBold}

// itemGlyphs are the letters each kind of item is drawn as, as roguelikes have long drawn them.
var itemGlyphs = map[string]string{
	kindWeapon:  "/",
	kindArmor:   "[",
	kindDraught: "!",
	kindTonic:   "?",
	kindShards:  "*",
}

// drawRun draws r's floor into a virtual screen of size, with the camera centred on the player and held inside the floor: the cells in sight bright, those seen before dim, and monsters and items only where the player sees them.
func drawRun(d *core.Drawing, r *run, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(core.RGB(colorVoid)).FillRect(0, 0, w, h)
	f := r.floor
	ox := camera(float32(r.pos.X*tileSize+tileSize/2), w, float32(f.grid.Width*tileSize))
	oy := camera(float32(r.pos.Y*tileSize+tileSize/2), h, float32(f.grid.Height*tileSize))
	d.Save().Translate(-ox, -oy)
	defer d.Restore()

	x0, y0 := max(0, int(ox)/tileSize), max(0, int(oy)/tileSize)
	x1, y1 := min(f.grid.Width, int(ox+w)/tileSize+1), min(f.grid.Height, int(oy+h)/tileSize+1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := y*f.grid.Width + x
			if !f.seen[i] {
				continue
			}
			lit := r.visible[i]
			px, py := float32(x*tileSize), float32(y*tileSize)
			if procgen.IsFloor(f.grid.Cells[i]) {
				d.SetFillColor(core.RGB(pick(lit, colorFloor, colorFloorSeen))).FillRect(px, py, tileSize, tileSize)
				if lit {
					d.SetFillColor(core.RGB(colorFloorDot)).FillRect(px+4, py+4, 2, 2)
				}
				continue
			}
			d.SetFillColor(core.RGB(pick(lit, colorWall, colorWallSeen))).FillRect(px, py, tileSize, tileSize)
		}
	}

	d.SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle)
	if i := f.exit.Y*f.grid.Width + f.exit.X; f.seen[i] {
		cx, cy := center(f.exit)
		if f.relic {
			d.SetFillColor(core.RGB(colorRelic)).BeginPath().
				MoveTo(cx, cy-4.5).LineTo(cx+4, cy).LineTo(cx, cy+4.5).LineTo(cx-4, cy).ClosePath().Fill()
		} else {
			d.SetFillColor(core.RGB(colorStairs)).FillText(">", cx, cy, glyphFont)
		}
	}
	for _, e := range f.loot() {
		at := f.at(e)
		if !r.visible[at.Y*f.grid.Width+at.X] {
			continue
		}
		cx, cy := center(at)
		def, _ := r.data.dungeon.item(f.lootOf(e).Item)
		c, _ := parseColor(def.Color)
		d.SetFillColor(core.RGB(c)).FillText(itemGlyphs[def.Kind], cx, cy, glyphFont)
	}
	for _, e := range f.foes() {
		at := f.at(e)
		if !r.visible[at.Y*f.grid.Width+at.X] {
			continue
		}
		foe := f.foe(e)
		def := r.foeDef(foe)
		cx, cy := center(at)
		c, _ := parseColor(def.Color)
		d.SetFillColor(core.RGB(c)).FillText(def.Glyph, cx, cy, glyphFont)
		if foe.Health < def.Health {
			px, py := float32(at.X*tileSize+1), float32(at.Y*tileSize)
			d.SetFillColor(core.RGB(colorFloorSeen)).FillRect(px, py, tileSize-2, 1.5)
			d.SetFillColor(core.RGB(colorHealth)).FillRect(px, py, (tileSize-2)*float32(foe.Health)/float32(def.Health), 1.5)
		}
	}
	cx, cy := center(r.pos)
	d.SetFillColor(core.RGB(colorPlayer)).FillText("@", cx, cy, glyphFont)
}

// camera returns the left (or top) edge of a view length long that centres on at, held inside a floor extent long, or centring the floor when it is smaller than the view.
func camera(at, length, extent float32) float32 {
	if extent <= length {
		return (extent - length) / 2
	}
	return min(max(at-length/2, 0), extent-length)
}

// center returns the middle of c in floor points.
func center(c cell) (float32, float32) {
	return float32(c.X*tileSize) + tileSize/2, float32(c.Y*tileSize) + tileSize/2
}

func pick(cond bool, a, b uint32) uint32 {
	if cond {
		return a
	}
	return b
}

// touchControls are the actions game.yaml gives an on-screen region, each with the symbol drawn on it.
var touchControls = []struct{ action, symbol string }{
	{"up", "▲"}, {"left", "◀"}, {actionWait, "·"}, {"right", "▶"}, {"down", "▼"},
	{actionCleave, "C"}, {actionQuaff, "!"},
}

// drawTouchControls draws the regions game.yaml binds to the directions, waiting and the abilities, brighter while held.
func drawTouchControls(d *core.Drawing, a *input.Actions) {
	font := core.Font{Size: 11, Weight: core.FontWeightBold}
	d.SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle)
	for _, c := range touchControls {
		r, ok := Project.Region(c.action)
		if !ok {
			continue
		}
		x, y := float32(r.Origin.X)+1.5, float32(r.Origin.Y)+1.5
		w, h := float32(r.Size.Width)-3, float32(r.Size.Height)-3
		alpha := float32(0.18)
		if a.Pressed(c.action) {
			alpha = 0.42
		}
		d.SetFillColor(core.RGBA(0xf8fafc, alpha)).BeginPath().RoundRect(x, y, w, h, 5).Fill()
		d.SetFillColor(core.RGBA(0xf8fafc, 0.85)).FillText(c.symbol, x+w/2, y+h/2, font)
	}
}

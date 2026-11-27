package main

import (
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// artFrame is one picture of the atlas: its name, its size in pixels and the painter that colors it.
type artFrame struct {
	name string
	w, h int
	// paint returns the color at (u, v), both from -1 at the frame's left or top edge to 1 at its right or bottom edge, and false where the frame is transparent.
	paint func(u, v float64) (color.NRGBA, bool)
}

// Palette of the art, which reads well on the dark starfield.
var (
	colShip     = color.NRGBA{0x7d, 0xd3, 0xfc, 0xff}
	colShipDark = color.NRGBA{0x02, 0x84, 0xc7, 0xff}
	colCockpit  = color.NRGBA{0xf0, 0xf9, 0xff, 0xff}
	colFlame    = color.NRGBA{0xfb, 0xbf, 0x24, 0xff}
	colEnemy    = color.NRGBA{0xf4, 0x72, 0xb6, 0xff}
	colEnemyDk  = color.NRGBA{0x9d, 0x17, 0x4d, 0xff}
	colWeaver   = color.NRGBA{0xa7, 0x8b, 0xfa, 0xff}
	colSkimmer  = color.NRGBA{0x4a, 0xde, 0x80, 0xff}
	colGunship  = color.NRGBA{0xfb, 0x92, 0x3c, 0xff}
	colBoss     = color.NRGBA{0xe1, 0x1d, 0x48, 0xff}
	colBossCore = color.NRGBA{0xfd, 0xe0, 0x47, 0xff}
	colMetal    = color.NRGBA{0x47, 0x55, 0x69, 0xff}
	colShot     = color.NRGBA{0xbb, 0xf7, 0xd0, 0xff}
	colPellet   = color.NRGBA{0xfe, 0xf0, 0x8a, 0xff}
	colOrb      = color.NRGBA{0xf0, 0xab, 0xfc, 0xff}
	colWhite    = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// artFrames are the pictures of the game, drawn in code so the template ships no image files. Replace makeAtlas with sprite.LoadAtlas, or with a sheet from the placeholder package, when you add art.
var artFrames = []artFrame{
	{"ship", 22, 24, paintShip},
	{"dart", 16, 18, paintDart},
	{"weaver", 20, 20, paintWeaver},
	{"skimmer", 20, 14, paintSkimmer},
	{"gunship", 34, 30, paintGunship},
	{"boss", 72, 52, paintBoss},
	{"shot", 4, 12, glow(colShot, 1.0)},
	{"pellet", 7, 7, glow(colPellet, 0.9)},
	{"orb", 9, 9, ring(colOrb)},
	{"spark", 5, 5, glow(colWhite, 1.0)},
	{"item_power", 13, 13, paintItem(color.NRGBA{0x22, 0xc5, 0x5e, 0xff}, letterP)},
	{"item_bomb", 13, 13, paintItem(color.NRGBA{0xf9, 0x73, 0x16, 0xff}, letterB)},
	{"item_life", 13, 13, paintItem(color.NRGBA{0xec, 0x48, 0x99, 0xff}, letterL)},
}

// makeAtlas paints every frame of artFrames side by side into one sheet, each frame's pivot at its middle, so every sprite's position is where its center is.
func makeAtlas() *sprite.Atlas {
	width, height := 0, 0
	for _, f := range artFrames {
		width += f.w + 1
		height = max(height, f.h)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, width, height))
	frames := make(map[string]sprite.Frame, len(artFrames))
	x := 0
	for _, f := range artFrames {
		for py := range f.h {
			for px := range f.w {
				u := (float64(px)+0.5)/float64(f.w)*2 - 1
				v := (float64(py)+0.5)/float64(f.h)*2 - 1
				if c, ok := f.paint(u, v); ok {
					sheet.Set(x+px, py, c)
				}
			}
		}
		w, h := float32(f.w), float32(f.h)
		frames[f.name] = sprite.Frame{Name: f.name, X: float32(x), W: w, H: h, Width: w, Height: h, PivotX: w / 2, PivotY: h / 2}
		x += f.w + 1
	}
	return &sprite.Atlas{Image: core.ImageSource{Pixels: sheet}, Frames: frames}
}

// paintShip is the player's ship: a swept arrow with a cockpit and an engine flame.
func paintShip(u, v float64) (color.NRGBA, bool) {
	ax := math.Abs(u)
	switch {
	case v > 0.7 && ax < 0.18:
		return colFlame, true
	case math.Hypot(u, (v+0.15)*1.4) < 0.22:
		return colCockpit, true
	case v > 0.45 && v < 0.75 && ax > 0.55 && ax < 0.95:
		return colShipDark, true
	case v > -0.95 && ax < (v+0.95)*0.55 && v < 0.7:
		if ax > (v+0.95)*0.4 {
			return colShipDark, true
		}
		return colShip, true
	}
	return color.NRGBA{}, false
}

// paintDart is a small arrow pointing down, the fastest enemy.
func paintDart(u, v float64) (color.NRGBA, bool) {
	ax := math.Abs(u)
	if v < 0.95 && ax < (0.95-v)*0.5 {
		if math.Hypot(u, v+0.2) < 0.25 {
			return colWhite, true
		}
		if ax > (0.95-v)*0.35 {
			return colEnemyDk, true
		}
		return colEnemy, true
	}
	return color.NRGBA{}, false
}

// paintWeaver is a ringed disc that sways as it comes down.
func paintWeaver(u, v float64) (color.NRGBA, bool) {
	r := math.Hypot(u, v)
	switch {
	case r < 0.3:
		return colWhite, true
	case r < 0.6:
		return colWeaver, true
	case r < 0.95 && math.Abs(v) < 0.3:
		return colEnemyDk, true
	}
	return color.NRGBA{}, false
}

// paintSkimmer is a flat wing that crosses the screen.
func paintSkimmer(u, v float64) (color.NRGBA, bool) {
	ax := math.Abs(u)
	switch {
	case math.Hypot(u, v) < 0.3:
		return colWhite, true
	case math.Abs(v) < 0.95-ax*0.8:
		if v > 0.2 {
			return colMetal, true
		}
		return colSkimmer, true
	}
	return color.NRGBA{}, false
}

// paintGunship is a heavy hull with two cannons that stops to fire rings.
func paintGunship(u, v float64) (color.NRGBA, bool) {
	ax := math.Abs(u)
	switch {
	case math.Hypot(u, v) < 0.28:
		return colBossCore, true
	case ax > 0.55 && ax < 0.8 && v > 0 && v < 0.95:
		return colMetal, true
	case ax < 0.9 && math.Abs(v) < 0.55-ax*0.2:
		return colGunship, true
	case ax < 0.35 && v < 0 && v > -0.95:
		return colEnemyDk, true
	}
	return color.NRGBA{}, false
}

// paintBoss is the boss: a wide fortress with a glowing core and wing batteries.
func paintBoss(u, v float64) (color.NRGBA, bool) {
	ax := math.Abs(u)
	switch {
	case math.Hypot(u*1.4, v) < 0.3:
		return colBossCore, true
	case math.Hypot(u*1.4, v) < 0.42:
		return colWhite, true
	case ax > 0.7 && ax < 0.95 && v > -0.2 && v < 0.95:
		return colMetal, true
	case ax < 0.95 && v > -0.55+ax*0.3 && v < 0.55-ax*0.2:
		if math.Mod(ax*10, 2) < 0.25 {
			return colEnemyDk, true
		}
		return colBoss, true
	case ax < 0.3 && v >= -0.95 && v < -0.3:
		return colMetal, true
	}
	return color.NRGBA{}, false
}

// glow returns a painter of a soft-edged ellipse in c, white in the middle out to 0.45 times solid of its radius.
func glow(c color.NRGBA, solid float64) func(u, v float64) (color.NRGBA, bool) {
	return func(u, v float64) (color.NRGBA, bool) {
		r := math.Hypot(u, v)
		if r > 1 {
			return color.NRGBA{}, false
		}
		if r < 0.45*solid {
			return colWhite, true
		}
		out := c
		out.A = uint8(255 * min(1, (1-r)*2.5))
		return out, true
	}
}

// ring returns a painter of a round bullet in c with a white middle, which shows on any background.
func ring(c color.NRGBA) func(u, v float64) (color.NRGBA, bool) {
	return func(u, v float64) (color.NRGBA, bool) {
		r := math.Hypot(u, v)
		switch {
		case r > 1:
			return color.NRGBA{}, false
		case r < 0.45:
			return colWhite, true
		}
		return c, true
	}
}

// Letters drawn on the items, five rows of three cells, a set cell a 1.
var (
	letterP = [5]uint8{0b111, 0b101, 0b111, 0b100, 0b100}
	letterB = [5]uint8{0b110, 0b101, 0b110, 0b101, 0b110}
	letterL = [5]uint8{0b100, 0b100, 0b100, 0b100, 0b111}
)

// paintItem returns a painter of a rounded square in c with a white letter on it.
func paintItem(c color.NRGBA, letter [5]uint8) func(u, v float64) (color.NRGBA, bool) {
	return func(u, v float64) (color.NRGBA, bool) {
		if math.Abs(u) > 0.92 && math.Abs(v) > 0.92 {
			return color.NRGBA{}, false
		}
		col := int((u + 0.6) / 0.4)
		row := int((v + 0.75) / 0.3)
		if u >= -0.6 && u < 0.6 && v >= -0.75 && v < 0.75 && letter[row]&(0b100>>col) != 0 {
			return colWhite, true
		}
		return c, true
	}
}

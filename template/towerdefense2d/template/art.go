package main

import (
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// The game draws all of its art in code, so it runs with no image files. Replace makeAtlas with sprite.LoadAtlas when you add art.

// groundFrames are the atlas frames of the ground tiles, in tileset order: a tile's global ID is its index here plus 1.
var groundFrames = []string{"grass", "grass-dark", "road", "spawn", "gate", "rock", "water"}

// projectileSize is the edge of a projectile frame in pixels.
const projectileSize = 12

// makeAtlas draws the ground tiles at the level's tile size and one projectile frame for each tower's pattern, in the tower's color, into one sprite sheet.
func makeAtlas(d *Data) *sprite.Atlas {
	tile := max(1, int(d.Level.TileSize))
	var shots []string
	shotColor := map[string]color.NRGBA{}
	for _, t := range d.Towers.Towers {
		// LoadData checked that every tower's pattern exists and names a frame.
		p, _ := d.Projectiles.Pattern(t.Pattern)
		if _, ok := shotColor[p.Frame]; !ok {
			shots = append(shots, p.Frame)
			shotColor[p.Frame] = nrgba(t.Color)
		}
	}
	sheet := image.NewRGBA(image.Rect(0, 0, tile*len(groundFrames)+projectileSize*len(shots), max(tile, projectileSize)))
	atlas := &sprite.Atlas{Frames: map[string]sprite.Frame{}}
	for i, name := range groundFrames {
		x := i * tile
		drawGround(sheet, x, tile, name)
		atlas.Frames[name] = sprite.Frame{Name: name, X: float32(x), W: float32(tile), H: float32(tile), Width: float32(tile), Height: float32(tile)}
	}
	for i, name := range shots {
		x := tile*len(groundFrames) + i*projectileSize
		drawProjectile(sheet, x, shotColor[name])
		atlas.Frames[name] = sprite.Frame{Name: name, X: float32(x), W: projectileSize, H: projectileSize, Width: projectileSize, Height: projectileSize}
	}
	atlas.Image = core.ImageSource{Pixels: sheet}
	return atlas
}

// groundTileset returns the tileset of the ground frames of atlas.
func groundTileset(atlas *sprite.Atlas) *tilemap.Tileset {
	ts := tilemap.NewAtlasTileset(1, atlas, groundFrames...)
	ts.Name = "ground"
	return ts
}

// groundGID returns the global tile ID that draws the legend tile t at (col, row), checkering the grass.
func groundGID(t byte, col, row int) uint32 {
	name := "grass"
	switch t {
	case TileGrass:
		if (col+row)%2 == 1 {
			name = "grass-dark"
		}
	case TileRoad:
		name = "road"
	case TileSpawn:
		name = "spawn"
	case TileGate:
		name = "gate"
	case TileRock:
		name = "rock"
	case TileWater:
		name = "water"
	}
	for i, f := range groundFrames {
		if f == name {
			return uint32(i + 1)
		}
	}
	return 1
}

// drawGround paints the ground tile name into dst at column x, size pixels square.
func drawGround(dst *image.RGBA, x, size int, name string) {
	s := float64(size)
	for py := range size {
		for px := range size {
			u, v := (float64(px)+0.5)/s, (float64(py)+0.5)/s
			var c color.NRGBA
			switch name {
			case "grass", "grass-dark":
				c = color.NRGBA{0x3f, 0x7d, 0x3a, 0xff}
				if name == "grass-dark" {
					c = color.NRGBA{0x38, 0x72, 0x34, 0xff}
				}
				// A few blades of lighter grass.
				if (px*7+py*13)%23 == 0 {
					c = color.NRGBA{0x5c, 0x9e, 0x4f, 0xff}
				}
			case "road", "spawn", "gate":
				c = color.NRGBA{0xc2, 0xa0, 0x6b, 0xff}
				if (px*5+py*11)%17 == 0 {
					c = color.NRGBA{0xa8, 0x86, 0x55, 0xff}
				}
				if name == "spawn" && math.Hypot(u-0.5, v-0.5) < 0.32 {
					c = color.NRGBA{0x6b, 0x21, 0xa8, 0xff}
				}
				if name == "gate" && u > 0.2 && u < 0.8 && v > 0.15 && v < 0.85 {
					c = color.NRGBA{0x7c, 0x4a, 0x1e, 0xff}
					if u > 0.45 && u < 0.55 || v > 0.48 && v < 0.56 {
						c = color.NRGBA{0xe5, 0xc0, 0x7b, 0xff}
					}
				}
			case "rock":
				c = color.NRGBA{0x3f, 0x7d, 0x3a, 0xff}
				if d := math.Hypot(u-0.5, (v-0.55)*1.3); d < 0.4 {
					c = color.NRGBA{0x78, 0x71, 0x6c, 0xff}
					if u+v < 0.85 {
						c = color.NRGBA{0xa8, 0xa2, 0x9e, 0xff}
					}
				}
			case "water":
				c = color.NRGBA{0x1d, 0x4e, 0x89, 0xff}
				if int(v*8+math.Sin(u*6.28)*1.2)%3 == 0 {
					c = color.NRGBA{0x2b, 0x6c, 0xb0, 0xff}
				}
			}
			dst.Set(x+px, py, c)
		}
	}
}

// drawProjectile paints a glowing orb in c into dst at column x.
func drawProjectile(dst *image.RGBA, x int, c color.NRGBA) {
	center := float64(projectileSize-1) / 2
	radius := float64(projectileSize) / 2
	for py := range projectileSize {
		for px := range projectileSize {
			d := math.Hypot(float64(px)-center, float64(py)-center) / radius
			if d > 1 {
				continue
			}
			p := c
			if d < 0.4 {
				p = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			} else {
				p.A = uint8(255 * min(1, (1-d)*2.2))
			}
			dst.Set(x+px, py, p)
		}
	}
}

// nrgba returns the CSS color s, which Validate accepted, as a color.NRGBA.
func nrgba(s string) color.NRGBA {
	c, _ := core.ParseColor(s)
	// A core.Color holds red, green, blue and alpha from the high byte down.
	return color.NRGBA{R: uint8(c >> 24), G: uint8(c >> 16), B: uint8(c >> 8), A: uint8(c)}
}

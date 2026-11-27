package main

import (
	"image"
	"image/color"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// The art is drawn by code, so the template has no image files to license or load. Replace a function here with sprite.LoadAtlas or a tileset image from a Tiled map when you add art.

// artTile is the size in pixels of a tile and of a player frame.
const artTile = 16

// The tileset's global tile IDs, in the order tilesetImage draws them.
const (
	tileGrass  uint32 = 1 // ground with open sky above it
	tileDirt   uint32 = 2 // ground under ground
	tileLedge  uint32 = 3
	tileSpikes uint32 = 4
	tileCount         = 4
)

var (
	colGrass     = color.RGBA{0x5f, 0xc2, 0x5a, 0xff}
	colGrassDark = color.RGBA{0x3c, 0x8d, 0x4a, 0xff}
	colDirt      = color.RGBA{0x7a, 0x4e, 0x36, 0xff}
	colDirtDark  = color.RGBA{0x5c, 0x37, 0x27, 0xff}
	colWood      = color.RGBA{0xc8, 0x8f, 0x52, 0xff}
	colWoodDark  = color.RGBA{0x8a, 0x5a, 0x32, 0xff}
	colSteel     = color.RGBA{0xd9, 0xe2, 0xec, 0xff}
	colSteelDark = color.RGBA{0x8c, 0x9b, 0xab, 0xff}
	colCloak     = color.RGBA{0x3b, 0x6e, 0xd8, 0xff}
	colCloakDark = color.RGBA{0x27, 0x4a, 0x9c, 0xff}
	colFace      = color.RGBA{0xff, 0xe0, 0xbd, 0xff}
	colEye       = color.RGBA{0x1d, 0x1b, 0x2e, 0xff}
	colGlow      = color.RGBA{0xff, 0xd1, 0x4d, 0xff}
)

// newTilemap returns the level's tiles as a tilemap with a code-drawn tileset, for drawing: a ground tile with open sky above it has grass on top.
func newTilemap(l *Level) *tilemap.Tilemap {
	rows := l.rows()
	cols := len(rows[0])
	ts := float32(l.TileSize)
	set := tilemap.NewGridTileset(1, core.ImageSource{Pixels: tilesetImage()}, artTile, artTile, tileCount, tileCount)
	data := make([]uint32, cols*len(rows))
	for r, row := range rows {
		for c := 0; c < len(row); c++ {
			var gid uint32
			switch row[c] {
			case glyphGround:
				gid = tileGrass
				if r > 0 && rows[r-1][c] == glyphGround {
					gid = tileDirt
				}
			case glyphLedge:
				gid = tileLedge
			case glyphSpikes:
				gid = tileSpikes
			}
			data[r*cols+c] = gid
		}
	}
	m := tilemap.New(cols, len(rows), ts, ts, set)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", cols, len(rows), data))
	return m
}

// tilesetImage draws the four tiles side by side.
func tilesetImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, artTile*tileCount, artTile))
	for y := range artTile {
		for x := range artTile {
			// Grass: a green band with a ragged edge over dirt with a few darker stones.
			switch {
			case y < 4 || (y == 4 && x%3 != 0):
				img.SetRGBA(x, y, colGrass)
			case y == 5 && x%3 == 1:
				img.SetRGBA(x, y, colGrassDark)
			case (x*7+y*3)%11 == 0:
				img.SetRGBA(x, y, colDirtDark)
			default:
				img.SetRGBA(x, y, colDirt)
			}
			// Dirt.
			if (x*7+y*3)%11 == 0 {
				img.SetRGBA(artTile+x, y, colDirtDark)
			} else {
				img.SetRGBA(artTile+x, y, colDirt)
			}
			// Ledge: a plank in the top quarter.
			switch {
			case y == 0 || y == 4:
				img.SetRGBA(2*artTile+x, y, colWoodDark)
			case y < 4:
				img.SetRGBA(2*artTile+x, y, colWood)
			}
			// Spikes: two steel triangles in the lower half.
			if y >= 8 {
				d := x % 8
				half := (y - 8) / 2
				if d >= 3-half && d <= 4+half {
					c := colSteel
					if d > 3 {
						c = colSteelDark
					}
					img.SetRGBA(3*artTile+x, y, c)
				}
			}
		}
	}
	return img
}

// newPlayerAtlas draws the player's frames, a small lantern bearer in a blue cloak, and the run cycle that plays while it moves on the ground.
func newPlayerAtlas() *sprite.Atlas {
	names := []string{"stand", "run1", "run2", "jump"}
	img := image.NewRGBA(image.Rect(0, 0, artTile*len(names), artTile))
	frames := make(map[string]sprite.Frame, len(names))
	for i, name := range names {
		drawPlayerFrame(img, i*artTile, i)
		frames[name] = sprite.Frame{Name: name, X: float32(i * artTile), W: artTile, H: artTile, Width: artTile, Height: artTile, PivotX: artTile / 2, PivotY: artTile}
	}
	step := 120 * time.Millisecond
	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: img},
		Frames: frames,
		Animations: map[string]sprite.Animation{
			"run": {Loop: true, Frames: []sprite.AnimationFrame{
				{Frame: frames["run1"], Duration: step},
				{Frame: frames["stand"], Duration: step},
				{Frame: frames["run2"], Duration: step},
				{Frame: frames["stand"], Duration: step},
			}},
		},
	}
}

// drawPlayerFrame draws pose 0 (standing), 1 and 2 (striding) or 3 (in the air) at x offset ox, facing right.
func drawPlayerFrame(img *image.RGBA, ox, pose int) {
	set := func(x, y int, c color.RGBA) { img.SetRGBA(ox+x, y, c) }
	// Hood and face.
	for y := 3; y < 9; y++ {
		for x := 4; x < 12; x++ {
			set(x, y, colCloak)
		}
	}
	for y := 5; y < 9; y++ {
		for x := 6; x < 11; x++ {
			set(x, y, colFace)
		}
	}
	set(9, 6, colEye)
	// Cloak.
	for y := 9; y < 14; y++ {
		for x := 4 - (y-9)/3; x < 12+(y-9)/3; x++ {
			c := colCloak
			if x < 6 {
				c = colCloakDark
			}
			set(x, y, c)
		}
	}
	// Feet.
	feet := [4][2]int{{5, 9}, {4, 10}, {6, 8}, {5, 10}}[pose]
	footY := 15
	if pose == 3 {
		footY = 14
	}
	for _, x := range feet {
		set(x, footY, colEye)
		set(x+1, footY, colEye)
		set(x, footY-1, colCloakDark)
	}
	// The lantern, held out in front.
	ly := 9
	if pose == 3 {
		ly = 7
	}
	for y := ly; y < ly+3; y++ {
		for x := 12; x < 15; x++ {
			set(x, y, colGlow)
		}
	}
	set(13, ly-1, colWoodDark)
}

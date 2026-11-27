package gen

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// TileColumns and TileRows are the tileset's size in tiles.
const (
	TileColumns = 8
	TileRows    = 4
)

// tileInfo is one tile of the tileset: its name (its type in Tiled), its custom properties and, for the first frame of an animated tile, its frames.
type tileInfo struct {
	name  string
	flags []string // boolean properties set to true: solid, oneWay, ladder, hazard, water
	anim  []int    // the tile ids of its frames
	ms    int      // how long each frame of anim shows, in milliseconds
}

// Tiles are the tileset's tiles by id.
var Tiles = [TileColumns * TileRows]tileInfo{
	{name: "grass", flags: []string{"solid"}},
	{name: "dirt", flags: []string{"solid"}},
	{name: "stone", flags: []string{"solid"}},
	{name: "brick", flags: []string{"solid"}},
	{name: "wood", flags: []string{"solid"}},
	{name: "platform", flags: []string{"oneWay"}},
	{name: "ladder", flags: []string{"ladder"}},
	{name: "spikes", flags: []string{"hazard"}},
	{name: "water", flags: []string{"water"}, anim: []int{8, 9, 10, 11}, ms: 150},
	{name: "water", flags: []string{"water"}},
	{name: "water", flags: []string{"water"}},
	{name: "water", flags: []string{"water"}},
	{name: "lava", flags: []string{"hazard"}, anim: []int{12, 13, 14, 15}, ms: 200},
	{name: "lava", flags: []string{"hazard"}},
	{name: "lava", flags: []string{"hazard"}},
	{name: "lava", flags: []string{"hazard"}},
	{name: "ice", flags: []string{"solid"}},
	{name: "crate", flags: []string{"solid"}},
	{name: "bush"},
	{name: "flower"},
	{name: "sign"},
	{name: "door_top"},
	{name: "door_bottom"},
	{name: "wall"},
	{name: "metal", flags: []string{"solid"}},
	{name: "bonus", flags: []string{"solid"}},
	{name: "sand", flags: []string{"solid"}},
	{name: "snow", flags: []string{"solid"}},
	{name: "torch", anim: []int{28, 29}, ms: 250},
	{name: "torch"},
	{name: "chain"},
	{name: "window"},
}

// addTiles draws the tileset and writes it with its Tiled tileset, a sample level and the image's sidecar.
func addTiles(files map[string][]byte) error {
	c := newCanvas(TileColumns*Cell, TileRows*Cell)
	for id := range Tiles {
		x, y := id%TileColumns*Cell, id/TileColumns*Cell
		drawTile(c, image.Rect(x, y, x+Cell, y+Cell), id)
	}
	img, err := encodePNG(c.img)
	if err != nil {
		return err
	}
	files["tiles/tileset.png"] = img
	if err := addSidecar(files, "tiles/tileset.png", &importsettings.File{Image: &importsettings.Image{Filter: "nearest"}}); err != nil {
		return err
	}
	ts, err := tilesetJSON()
	if err != nil {
		return err
	}
	files["tiles/tileset.tsj"] = ts
	level, err := levelJSON()
	if err != nil {
		return err
	}
	files["tiles/level.tmj"] = level
	return nil
}

// speckle fills r with base and scatters dark and light pixels over it.
func speckle(c *canvas, r image.Rectangle, base, dark, light color.NRGBA, seed int) {
	c.rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y, base)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			switch hash(x, y, seed) % 11 {
			case 0:
				c.set(x, y, dark)
			case 1:
				c.set(x, y, light)
			}
		}
	}
}

// blocks draws mortar lines of col on r in a running bond of courses rows tall.
func blocks(c *canvas, r image.Rectangle, rows int, col color.NRGBA) {
	for y := 0; y < Cell; y++ {
		if y%rows == rows-1 {
			c.rect(r.Min.X, r.Min.Y+y, r.Max.X, r.Min.Y+y+1, col)
			continue
		}
		course := y / rows
		x := 7
		if course%2 == 1 {
			x = 3
		}
		c.set(r.Min.X+x, r.Min.Y+y, col)
		c.set(r.Min.X+x+8, r.Min.Y+y, col)
	}
}

// drawTile draws tile id in r.
func drawTile(c *canvas, r image.Rectangle, id int) {
	x0, y0 := r.Min.X, r.Min.Y
	fx, fy := float64(x0), float64(y0)
	set := func(x, y int, col color.NRGBA) { c.set(x0+x, y0+y, col) }
	fill := func(x0r, y0r, x1r, y1r int, col color.NRGBA) { c.rect(x0+x0r, y0+y0r, x0+x1r, y0+y1r, col) }
	switch id {
	case 0, 27: // grass, snow
		top, light := green2, green3
		if id == 27 {
			top, light = white, grey3
		}
		speckle(c, r, brown2, brown1, brown3, id)
		fill(0, 0, Cell, 4, top)
		for x := range Cell {
			if hash(x, 4, id)%3 == 0 {
				set(x, 4, top)
			}
			if hash(x, 0, id)%2 == 0 {
				set(x, 0, light)
			}
		}
	case 1: // dirt
		speckle(c, r, brown2, brown1, brown3, id)
	case 2: // stone
		c.rect(x0, y0, r.Max.X, r.Max.Y, grey2)
		blocks(c, r, 8, grey1)
		for x := range Cell {
			if hash(x, 0, id)%2 == 0 {
				set(x, 0, grey3)
				set(x, 8, grey3)
			}
		}
	case 3: // brick
		c.rect(x0, y0, r.Max.X, r.Max.Y, red)
		blocks(c, r, 4, brown1)
	case 4: // wood
		speckle(c, r, brown3, brown2, brown3, id)
		for _, y := range []int{4, 9, 14} {
			fill(0, y, Cell, y+1, brown1)
		}
		for _, p := range [][2]int{{2, 2}, {13, 2}, {2, 7}, {13, 7}, {2, 12}, {13, 12}} {
			set(p[0], p[1], brown1)
		}
	case 5: // platform
		fill(0, 0, Cell, 1, brown3)
		fill(0, 1, Cell, 4, brown2)
		fill(0, 4, Cell, 5, brown1)
		set(3, 2, brown1)
		set(12, 2, brown1)
	case 6: // ladder
		fill(2, 0, 4, Cell, brown2)
		fill(12, 0, 14, Cell, brown2)
		for _, y := range []int{2, 6, 10, 14} {
			fill(4, y, 12, y+1, brown3)
		}
	case 7: // spikes
		for i := range 2 {
			x := fx + float64(8*i)
			c.poly(r, [][2]float64{{x, fy + 16}, {x + 4, fy + 4}, {x + 8, fy + 16}}, grey3)
			c.poly(r, [][2]float64{{x + 4, fy + 4}, {x + 8, fy + 16}, {x + 4, fy + 16}}, grey2)
			set(8*i+3, 5, white)
		}
		fill(0, 15, Cell, Cell, grey1)
	case 8, 9, 10, 11: // water
		k := id - 8
		speckle(c, r, blue2, blue1, blue2, id)
		fill(0, 8, Cell, Cell, blue1)
		for x := range Cell {
			switch (x + 4*k) % 8 {
			case 0, 2:
				set(x, 2, blue3)
			case 1:
				set(x, 1, cyan)
				set(x, 2, blue3)
			}
		}
	case 12, 13, 14, 15: // lava
		k := id - 12
		c.rect(x0, y0, r.Max.X, r.Max.Y, orange)
		fill(0, 8, Cell, Cell, red)
		for x := range Cell {
			if (x+3*k)%6 == 0 {
				set(x, 1, yellow)
			}
		}
		c.ellipse(r, fx+float64((4+5*k)%16), fy+11-float64(2*k), 1.5, 1.5, yellow)
	case 16: // ice
		c.rect(x0, y0, r.Max.X, r.Max.Y, cyan)
		for y := range Cell {
			for x := range Cell {
				if (x+y)%7 == 0 {
					set(x, y, white)
				}
			}
		}
		fill(0, 15, Cell, Cell, blue3)
	case 17: // crate
		c.rect(x0, y0, r.Max.X, r.Max.Y, brown3)
		fill(0, 0, Cell, 1, brown1)
		fill(0, 15, Cell, Cell, brown1)
		fill(0, 0, 1, Cell, brown1)
		fill(15, 0, Cell, Cell, brown1)
		fill(1, 1, 15, 3, brown2)
		fill(1, 13, 15, 15, brown2)
		c.line(x0+2, y0+3, x0+13, y0+12, brown1)
	case 18: // bush
		c.ellipse(r, fx+8, fy+11, 7, 5, green1)
		c.ellipse(r, fx+7.5, fy+10.5, 6, 4.5, green2)
		for _, p := range [][2]int{{5, 9}, {9, 8}, {11, 11}, {4, 12}} {
			set(p[0], p[1], green3)
		}
	case 19: // flower
		fill(7, 8, 9, Cell, green1)
		set(6, 12, green2)
		set(9, 11, green2)
		for _, p := range [][2]float64{{6, 5}, {10, 5}, {8, 3}, {8, 7}} {
			c.ellipse(r, fx+p[0], fy+p[1], 2, 2, pink)
		}
		c.ellipse(r, fx+8, fy+5, 1.5, 1.5, yellow)
	case 20: // sign
		fill(7, 9, 9, Cell, brown2)
		fill(2, 3, 14, 10, brown1)
		fill(3, 4, 13, 9, brown3)
		fill(4, 5, 12, 6, brown1)
		fill(4, 7, 10, 8, brown1)
	case 21, 22: // door
		fill(2, 0, 14, Cell, brown1)
		top := 0
		if id == 21 {
			top = 2
			fill(2, 0, 14, 1, transparent)
			fill(3, 1, 13, 2, brown1)
		}
		fill(3, top, 13, Cell, brown2)
		for _, x := range []int{5, 8, 11} {
			fill(x, top, x+1, Cell, brown1)
		}
		if id == 22 {
			set(10, 4, yellow)
			fill(2, 15, 14, Cell, grey1)
		}
	case 23, 31: // wall, window
		c.rect(x0, y0, r.Max.X, r.Max.Y, grey1)
		blocks(c, r, 8, ink)
		if id == 31 {
			fill(3, 3, 13, 13, ink)
			fill(4, 4, 12, 12, blue1)
			fill(7, 4, 9, 12, grey2)
			fill(4, 7, 12, 9, grey2)
			set(5, 5, white)
		}
	case 24: // metal
		c.rect(x0, y0, r.Max.X, r.Max.Y, grey3)
		fill(0, 0, Cell, 1, grey2)
		fill(0, 15, Cell, Cell, grey2)
		fill(0, 0, 1, Cell, grey2)
		fill(15, 0, Cell, Cell, grey2)
		for _, p := range [][2]int{{2, 2}, {13, 2}, {2, 13}, {13, 13}} {
			set(p[0], p[1], white)
		}
	case 25: // bonus
		c.rect(x0, y0, r.Max.X, r.Max.Y, goldDark)
		fill(1, 1, 15, 15, gold)
		fill(7, 4, 9, 12, white)
		fill(4, 7, 12, 9, white)
	case 26: // sand
		speckle(c, r, yellow, orange, white, id)
	case 28, 29: // torch
		fill(7, 8, 9, 15, brown2)
		k := float64(id - 28)
		c.ellipse(r, fx+8, fy+5-k/2, 2.5, 3.5+k/2, orange)
		c.ellipse(r, fx+8, fy+6, 1.5, 2, yellow)
	case 30: // chain
		for y := 0; y < Cell; y += 4 {
			if y%8 == 0 {
				fill(7, y, 9, y+1, grey3)
				fill(7, y+3, 9, y+4, grey3)
				fill(6, y+1, 7, y+3, grey3)
				fill(9, y+1, 10, y+3, grey3)
			} else {
				fill(7, y, 9, y+4, grey2)
			}
		}
	default:
		panic(fmt.Sprintf("gen: no drawing for tile %d", id))
	}
}

// Tiled's JSON, in the key order Tiled writes it.
type (
	tiledProperty struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value any    `json:"value"`
	}
	tiledFrame struct {
		Duration int `json:"duration"`
		TileID   int `json:"tileid"`
	}
	tiledTile struct {
		Animation  []tiledFrame    `json:"animation,omitempty"`
		ID         int             `json:"id"`
		Properties []tiledProperty `json:"properties,omitempty"`
		Type       string          `json:"type"`
	}
	tiledTileset struct {
		Columns      int         `json:"columns"`
		Image        string      `json:"image"`
		ImageHeight  int         `json:"imageheight"`
		ImageWidth   int         `json:"imagewidth"`
		Margin       int         `json:"margin"`
		Name         string      `json:"name"`
		Spacing      int         `json:"spacing"`
		TileCount    int         `json:"tilecount"`
		TiledVersion string      `json:"tiledversion"`
		TileHeight   int         `json:"tileheight"`
		Tiles        []tiledTile `json:"tiles"`
		TileWidth    int         `json:"tilewidth"`
		Type         string      `json:"type"`
		Version      string      `json:"version"`
	}
	tiledObject struct {
		Height   int    `json:"height"`
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Point    bool   `json:"point,omitempty"`
		Rotation int    `json:"rotation"`
		Type     string `json:"type"`
		Visible  bool   `json:"visible"`
		Width    int    `json:"width"`
		X        int    `json:"x"`
		Y        int    `json:"y"`
	}
	tiledLayer struct {
		Data      []int         `json:"data,omitempty"`
		DrawOrder string        `json:"draworder,omitempty"`
		Height    int           `json:"height,omitempty"`
		ID        int           `json:"id"`
		Name      string        `json:"name"`
		Objects   []tiledObject `json:"objects,omitempty"`
		Opacity   int           `json:"opacity"`
		Type      string        `json:"type"`
		Visible   bool          `json:"visible"`
		Width     int           `json:"width,omitempty"`
		X         int           `json:"x"`
		Y         int           `json:"y"`
	}
	tiledTilesetRef struct {
		FirstGID int    `json:"firstgid"`
		Source   string `json:"source"`
	}
	tiledMap struct {
		CompressionLevel int               `json:"compressionlevel"`
		Height           int               `json:"height"`
		Infinite         bool              `json:"infinite"`
		Layers           []tiledLayer      `json:"layers"`
		NextLayerID      int               `json:"nextlayerid"`
		NextObjectID     int               `json:"nextobjectid"`
		Orientation      string            `json:"orientation"`
		RenderOrder      string            `json:"renderorder"`
		TiledVersion     string            `json:"tiledversion"`
		TileHeight       int               `json:"tileheight"`
		Tilesets         []tiledTilesetRef `json:"tilesets"`
		TileWidth        int               `json:"tilewidth"`
		Type             string            `json:"type"`
		Version          string            `json:"version"`
		Width            int               `json:"width"`
	}
)

const (
	tiledVersion = "1.11.0"
	tiledFormat  = "1.10"
)

// tilesetJSON returns the tileset as an external Tiled tileset (.tsj): every tile's name as its type, its flags as boolean properties and the animations of water, lava and the torch.
func tilesetJSON() ([]byte, error) {
	ts := tiledTileset{
		Columns: TileColumns, Image: "tileset.png", ImageHeight: TileRows * Cell, ImageWidth: TileColumns * Cell,
		Name: "placeholder", TileCount: len(Tiles), TiledVersion: tiledVersion, TileHeight: Cell, TileWidth: Cell,
		Type: "tileset", Version: tiledFormat,
	}
	for id, t := range Tiles {
		tile := tiledTile{ID: id, Type: t.name}
		for _, f := range t.flags {
			tile.Properties = append(tile.Properties, tiledProperty{Name: f, Type: "bool", Value: true})
		}
		for _, frame := range t.anim {
			tile.Animation = append(tile.Animation, tiledFrame{Duration: t.ms, TileID: frame})
		}
		ts.Tiles = append(ts.Tiles, tile)
	}
	return marshalTiled(ts)
}

// Level is the sample level's size in tiles.
const (
	LevelWidth  = 40
	LevelHeight = 15
)

// tileID returns the id of the tile named name, the first one for an animated tile.
func tileID(name string) int {
	for id, t := range Tiles {
		if t.name == name {
			return id
		}
	}
	panic("gen: no tile named " + name)
}

// levelJSON returns a small platformer level on the tileset: decorations behind, the ground and its obstacles, and the points where the player, pickups and enemies start, with a rectangle for the exit.
func levelJSON() ([]byte, error) {
	ground := make([]int, LevelWidth*LevelHeight)
	decor := make([]int, LevelWidth*LevelHeight)
	put := func(layer []int, col, row int, name string) { layer[row*LevelWidth+col] = tileID(name) + 1 }
	for col := range LevelWidth {
		if col >= 14 && col <= 17 {
			put(ground, col, 14, "water")
			continue
		}
		top := "grass"
		if col >= 33 && col <= 35 {
			top = "ice"
		}
		put(ground, col, 13, top)
		put(ground, col, 14, "dirt")
	}
	for col := 14; col <= 17; col++ {
		put(ground, col, 11, "platform")
	}
	put(ground, 5, 9, "bonus")
	put(ground, 6, 9, "bonus")
	for col := 9; col <= 11; col++ {
		put(ground, col, 9, "brick")
	}
	for row := 11; row <= 12; row++ {
		put(ground, 20, row, "stone")
		put(ground, 21, row, "stone")
	}
	put(ground, 24, 12, "spikes")
	put(ground, 25, 12, "spikes")
	put(ground, 28, 12, "crate")
	for row := 7; row <= 12; row++ {
		put(ground, 31, row, "ladder")
	}
	for col := 32; col <= 36; col++ {
		put(ground, col, 7, "wood")
	}
	put(ground, 38, 11, "door_top")
	put(ground, 38, 12, "door_bottom")

	put(decor, 1, 12, "sign")
	put(decor, 3, 12, "bush")
	put(decor, 8, 12, "flower")
	put(decor, 26, 12, "bush")
	put(decor, 37, 12, "torch")
	put(decor, 39, 12, "torch")
	for row := 0; row <= 2; row++ {
		put(decor, 12, row, "chain")
	}

	px := func(tiles float64) int { return int(tiles * Cell) }
	point := func(id int, name, typ string, x, y float64) tiledObject {
		return tiledObject{ID: id, Name: name, Point: true, Type: typ, Visible: true, X: px(x), Y: px(y)}
	}
	objects := []tiledObject{
		point(1, "player", "spawn", 2.5, 13),
		point(2, "", "coin", 5.5, 8.5),
		point(3, "", "coin", 6.5, 8.5),
		point(4, "", "coin", 15.5, 10.5),
		point(5, "", "coin", 16.5, 10.5),
		point(6, "", "coin", 33.5, 6.5),
		point(7, "", "coin", 34.5, 6.5),
		point(8, "", "gem", 35.5, 6.5),
		point(9, "", "slime", 23, 13),
		point(10, "", "bat", 27.5, 8),
		{ID: 11, Name: "exit", Type: "exit", Visible: true, X: px(38), Y: px(11), Width: Cell, Height: 2 * Cell},
	}
	m := tiledMap{
		CompressionLevel: -1, Height: LevelHeight,
		Layers: []tiledLayer{
			{Data: decor, Height: LevelHeight, ID: 1, Name: "decor", Opacity: 1, Type: "tilelayer", Visible: true, Width: LevelWidth},
			{Data: ground, Height: LevelHeight, ID: 2, Name: "ground", Opacity: 1, Type: "tilelayer", Visible: true, Width: LevelWidth},
			{DrawOrder: "topdown", ID: 3, Name: "objects", Objects: objects, Opacity: 1, Type: "objectgroup", Visible: true},
		},
		NextLayerID: 4, NextObjectID: len(objects) + 1, Orientation: "orthogonal", RenderOrder: "right-down",
		TiledVersion: tiledVersion, TileHeight: Cell, Tilesets: []tiledTilesetRef{{FirstGID: 1, Source: "tileset.tsj"}},
		TileWidth: Cell, Type: "map", Version: tiledFormat, Width: LevelWidth,
	}
	return marshalTiled(m)
}

// marshalTiled writes v as JSON indented by one space, as Tiled does, with a final newline.
func marshalTiled(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

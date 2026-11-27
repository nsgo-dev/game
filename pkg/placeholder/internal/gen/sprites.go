package gen

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"

	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

// Cell is the size in pixels of a sprite frame and of a tile.
const Cell = 16

// strip is one sprite sheet of the pack: a row of Cell by Cell frames, written as sprites/<name>.png with an Aseprite-style JSON array export and an import settings sidecar that slices it on the same grid.
type strip struct {
	name string
	// durations is each frame's duration in milliseconds; its length is the frame count.
	durations []int
	draw      func(c *canvas, cell image.Rectangle, frame int)
	tags      []tag
	slices    []string // frames, by tag name, whose middle Cell/2 square stretches as a nine-slice
}

// tag is an Aseprite frame tag: an animation from frame from to frame to, looping unless repeat is set.
type tag struct {
	name     string
	from, to int
	repeat   string
}

// strips are the pack's sprite sheets.
var strips = []strip{
	{
		name:      "player",
		durations: []int{400, 400, 100, 100, 100, 100, 100, 200},
		draw:      drawPlayer,
		tags:      []tag{{"idle", 0, 1, ""}, {"run", 2, 5, ""}, {"jump", 6, 6, ""}, {"hurt", 7, 7, ""}},
	},
	{
		name:      "slime",
		durations: []int{150, 150, 150, 150},
		draw:      drawSlime,
		tags:      []tag{{"idle", 0, 3, ""}},
	},
	{
		name:      "bat",
		durations: []int{100, 100, 100, 100},
		draw:      drawBat,
		tags:      []tag{{"fly", 0, 3, ""}},
	},
	{
		name:      "coin",
		durations: []int{120, 120, 120, 120},
		draw:      drawCoin,
		tags:      []tag{{"spin", 0, 3, ""}},
	},
	{
		name:      "items",
		durations: []int{100, 100, 100, 100, 100, 100, 100, 100},
		draw:      drawItem,
		tags:      []tag{{"gem", 0, 0, ""}, {"heart", 1, 1, ""}, {"key", 2, 2, ""}, {"star", 3, 3, ""}, {"potion", 4, 4, ""}, {"bomb", 5, 5, ""}, {"shield", 6, 6, ""}, {"flag", 7, 7, ""}},
	},
	{
		name:      "effects",
		durations: []int{60, 60, 80, 80, 100, 150, 150, 100},
		draw:      drawEffect,
		tags:      []tag{{"explosion", 0, 4, "1"}, {"sparkle", 5, 6, ""}, {"bullet", 7, 7, ""}},
	},
	{
		name:      "ui",
		durations: []int{100, 100, 100, 100},
		draw:      drawUI,
		tags:      []tag{{"panel", 0, 0, ""}, {"button", 1, 1, ""}, {"button_pressed", 2, 2, ""}, {"cursor", 3, 3, ""}},
		slices:    []string{"panel", "button", "button_pressed"},
	},
}

// addSprites draws each strip and writes its image, its JSON and its sidecar.
func addSprites(files map[string][]byte) error {
	for _, s := range strips {
		n := len(s.durations)
		c := newCanvas(n*Cell, Cell)
		for f := range n {
			s.draw(c, image.Rect(f*Cell, 0, (f+1)*Cell, Cell), f)
		}
		img, err := encodePNG(c.img)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		base := "sprites/" + s.name
		files[base+".png"] = img
		data, err := s.export()
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		files[base+".json"] = data
		if err := addSidecar(files, base+".png", &importsettings.File{Image: &importsettings.Image{
			Slice: &importsettings.Slice{Width: Cell, Height: Cell, Count: n},
		}}); err != nil {
			return err
		}
	}
	return nil
}

// The JSON of an Aseprite json-array export, with TexturePacker's pivot, which sprite.LoadAtlas also reads, in the order Aseprite writes its keys.
type (
	asepriteRect struct {
		X int `json:"x"`
		Y int `json:"y"`
		W int `json:"w"`
		H int `json:"h"`
	}
	asepriteSize struct {
		W int `json:"w"`
		H int `json:"h"`
	}
	asepritePoint struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	asepriteFrame struct {
		Filename         string        `json:"filename"`
		Frame            asepriteRect  `json:"frame"`
		Rotated          bool          `json:"rotated"`
		Trimmed          bool          `json:"trimmed"`
		SpriteSourceSize asepriteRect  `json:"spriteSourceSize"`
		SourceSize       asepriteSize  `json:"sourceSize"`
		Pivot            asepritePoint `json:"pivot"`
		Duration         int           `json:"duration"`
	}
	asepriteTag struct {
		Name      string `json:"name"`
		From      int    `json:"from"`
		To        int    `json:"to"`
		Direction string `json:"direction"`
		Repeat    string `json:"repeat,omitempty"`
	}
	asepriteSliceKey struct {
		Frame  int          `json:"frame"`
		Bounds asepriteRect `json:"bounds"`
		Center asepriteRect `json:"center"`
	}
	asepriteSlice struct {
		Name  string             `json:"name"`
		Color string             `json:"color"`
		Keys  []asepriteSliceKey `json:"keys"`
	}
	asepriteMeta struct {
		App       string          `json:"app"`
		Version   string          `json:"version"`
		Image     string          `json:"image"`
		Format    string          `json:"format"`
		Size      asepriteSize    `json:"size"`
		Scale     string          `json:"scale"`
		FrameTags []asepriteTag   `json:"frameTags"`
		Slices    []asepriteSlice `json:"slices"`
	}
	asepriteExport struct {
		Frames []asepriteFrame `json:"frames"`
		Meta   asepriteMeta    `json:"meta"`
	}
)

// export returns the strip's JSON. Frame i is named <strip>_<i>, as game assets names the same frame <strip>_<i>.png when the sidecar slices the sheet into an atlas.
func (s strip) export() ([]byte, error) {
	n := len(s.durations)
	e := asepriteExport{
		Frames: make([]asepriteFrame, n),
		Meta: asepriteMeta{
			App:       "github.com/nsgo-dev/game placeholder pack",
			Version:   "1",
			Image:     s.name + ".png",
			Format:    "RGBA8888",
			Size:      asepriteSize{W: n * Cell, H: Cell},
			Scale:     "1",
			FrameTags: make([]asepriteTag, len(s.tags)),
			Slices:    []asepriteSlice{},
		},
	}
	for i, d := range s.durations {
		e.Frames[i] = asepriteFrame{
			Filename:         fmt.Sprintf("%s_%d", s.name, i),
			Frame:            asepriteRect{X: i * Cell, W: Cell, H: Cell},
			SpriteSourceSize: asepriteRect{W: Cell, H: Cell},
			SourceSize:       asepriteSize{W: Cell, H: Cell},
			Pivot:            asepritePoint{X: 0.5, Y: 0.5},
			Duration:         d,
		}
	}
	for i, t := range s.tags {
		e.Meta.FrameTags[i] = asepriteTag{Name: t.name, From: t.from, To: t.to, Direction: "forward", Repeat: t.repeat}
	}
	for _, name := range s.slices {
		frame := -1
		for _, t := range s.tags {
			if t.name == name {
				frame = t.from
			}
		}
		if frame < 0 {
			return nil, fmt.Errorf("slice %q names no tag", name)
		}
		e.Meta.Slices = append(e.Meta.Slices, asepriteSlice{
			Name:  name,
			Color: "#0000ffff",
			Keys: []asepriteSliceKey{{
				Frame:  frame,
				Bounds: asepriteRect{W: Cell, H: Cell},
				Center: asepriteRect{X: Cell / 4, Y: Cell / 4, W: Cell / 2, H: Cell / 2},
			}},
		})
	}
	data, err := json.MarshalIndent(e, "", " ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// drawPlayer draws a round little explorer in a visor: idle (0-1), run (2-5), jump (6) and hurt (7).
func drawPlayer(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	bob := []int{0, 1, 0, 1, 0, 1, -1, 0}[f]
	// Each leg's column and how far it is lifted.
	legs := [][4]int{{5, 0, 9, 0}, {5, 0, 9, 0}, {4, 0, 10, 1}, {6, 0, 8, 0}, {4, 1, 10, 0}, {6, 0, 8, 0}, {5, 1, 9, 1}, {5, 0, 9, 0}}[f]
	for i := 0; i < 4; i += 2 {
		x := r.Min.X + legs[i]
		c.rect(x, 11, x+2, 14-legs[i+1], blue1)
	}
	cy := 7 + float64(bob)
	c.ellipse(r, ox+8, cy, 4.5, 4.5, blue1)
	c.ellipse(r, ox+7.5, cy-0.5, 4, 4, blue2)
	armY := 8 + bob
	if f == 6 {
		armY = 4 + bob
	}
	c.set(r.Min.X+3, armY, blue2)
	c.set(r.Min.X+12, armY, blue2)
	c.rect(r.Min.X+6, 5+bob, r.Min.X+11, 8+bob, cyan)
	if f == 7 {
		// Crossed-out eyes.
		for _, x := range []int{6, 9} {
			c.set(r.Min.X+x, 5+bob, ink)
			c.set(r.Min.X+x+1, 6+bob, ink)
			c.set(r.Min.X+x, 7+bob, ink)
		}
		c.recolor(r, blue2, red)
		c.recolor(r, blue1, purple)
	} else {
		c.set(r.Min.X+7, 6+bob, ink)
		c.set(r.Min.X+9, 6+bob, ink)
		c.set(r.Min.X+10, 5+bob, white)
	}
	c.outline(r, ink)
}

// drawSlime draws a slime squashing and stretching as it breathes.
func drawSlime(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	sx := []float64{1, 1.1, 0.95, 1.05}[f]
	sy := []float64{1, 0.85, 1.1, 0.95}[f]
	rx, ry := 6*sx, 6*sy
	const floor = 14
	dome := image.Rect(r.Min.X, r.Min.Y, r.Max.X, floor)
	c.ellipse(dome, ox+8, floor, rx, ry, green1)
	c.ellipse(dome, ox+7.5, floor-0.5, rx-1, ry-1, green2)
	top := floor - ry
	c.ellipse(dome, ox+8-rx/2, top+2.5, 1, 1, green3)
	eyeY := int(floor - ry/2)
	c.set(r.Min.X+6, eyeY, ink)
	c.set(r.Min.X+10, eyeY, ink)
	c.outline(r, ink)
}

// drawBat draws a bat beating its wings.
func drawBat(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	wy := []float64{-4, -1, 3, -1}[f]
	left := [][2]float64{{5.5, 7}, {0.5, 6 + wy}, {1.5, 10 + wy/2}, {5.5, 10}}
	right := make([][2]float64, len(left))
	for i, p := range left {
		left[i][0] = ox + p[0]
		right[i] = [2]float64{ox + 16 - p[0], p[1]}
	}
	c.poly(r, left, purple)
	c.poly(r, right, purple)
	c.ellipse(r, ox+8, 8.5, 3, 3, violet)
	c.set(r.Min.X+6, 5, violet)
	c.set(r.Min.X+10, 5, violet)
	c.set(r.Min.X+7, 8, yellow)
	c.set(r.Min.X+9, 8, yellow)
	c.outline(r, ink)
}

// drawCoin draws a gold coin turning on its axis.
func drawCoin(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	rx := []float64{5, 3.5, 1, 3.5}[f]
	c.ellipse(r, ox+8, 8, rx, 6, gold)
	if rx > 2 {
		c.ellipse(r, ox+8, 8, rx-1.5, 4.5, goldDark)
		c.ellipse(r, ox+8, 8, rx-2.5, 3.5, gold)
		c.set(r.Min.X+int(8-rx/2), 5, white)
	}
	c.outline(r, ink)
}

// drawItem draws one of the pickups: gem, heart, key, star, potion, bomb, shield and flag.
func drawItem(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	at := func(pts ...[2]float64) [][2]float64 {
		for i := range pts {
			pts[i][0] += ox
		}
		return pts
	}
	x := r.Min.X
	switch f {
	case 0: // gem
		c.poly(r, at([2]float64{8, 2}, [2]float64{13, 7}, [2]float64{8, 14}, [2]float64{3, 7}), cyan)
		c.poly(r, at([2]float64{3, 7}, [2]float64{13, 7}, [2]float64{8, 14}), blue3)
		c.set(x+6, 5, white)
	case 1: // heart
		c.ellipse(r, ox+5.5, 6, 2.8, 2.8, red)
		c.ellipse(r, ox+10.5, 6, 2.8, 2.8, red)
		c.poly(r, at([2]float64{2.8, 7}, [2]float64{13.2, 7}, [2]float64{8, 13.5}), red)
		c.set(x+5, 5, pink)
	case 2: // key
		c.ellipse(r, ox+5, 8, 3.5, 3.5, gold)
		c.ellipse(r, ox+5, 8, 1.5, 1.5, transparent)
		c.rect(x+8, 7, x+14, 9, gold)
		c.rect(x+11, 9, x+12, 11, gold)
		c.rect(x+13, 9, x+14, 11, gold)
	case 3: // star
		c.poly(r, at([2]float64{8, 2}, [2]float64{9.59, 6.32}, [2]float64{14.18, 6.49}, [2]float64{10.57, 9.33}, [2]float64{11.82, 13.76}, [2]float64{8, 11.2}, [2]float64{4.18, 13.76}, [2]float64{5.43, 9.33}, [2]float64{1.82, 6.49}, [2]float64{6.41, 6.32}), yellow)
		c.set(x+7, 6, white)
	case 4: // potion
		c.ellipse(r, ox+8, 10.5, 4.5, 4, grey3)
		c.ellipse(image.Rect(r.Min.X, 9, r.Max.X, r.Max.Y), ox+8, 10.5, 4.5, 4, pink)
		c.rect(x+7, 3, x+9, 7, grey3)
		c.rect(x+6, 2, x+10, 4, brown3)
		c.set(x+6, 9, white)
	case 5: // bomb
		c.ellipse(r, ox+7.5, 9.5, 5, 5, grey1)
		c.ellipse(r, ox+5.5, 7.5, 1.5, 1.5, grey2)
		c.line(x+10, 5, x+12, 2, brown3)
		c.set(x+12, 1, yellow)
		c.set(x+13, 2, orange)
	case 6: // shield
		c.poly(r, at([2]float64{3, 3}, [2]float64{13, 3}, [2]float64{13, 8}, [2]float64{8, 14}, [2]float64{3, 8}), blue2)
		c.rect(x+7, 4, x+9, 12, yellow)
		c.rect(x+4, 4, x+12, 5, blue3)
	case 7: // flag
		c.rect(x+4, 2, x+5, 14, grey3)
		c.poly(r, at([2]float64{5, 2}, [2]float64{13, 5}, [2]float64{5, 8}), red)
		c.rect(x+3, 14, x+7, 15, grey2)
	}
	c.outline(r, ink)
}

// drawEffect draws an explosion (0-4), a sparkle (5-6) and a bullet (7).
func drawEffect(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	x := r.Min.X
	switch {
	case f <= 4:
		outer := []float64{3, 5, 6.5, 7, 7.5}[f]
		hole := []float64{0, 0, 3, 5, 6.5}[f]
		col := []color.NRGBA{white, yellow, orange, red, grey2}[f]
		c.ellipse(r, ox+8, 8, outer, outer, col)
		if f == 1 {
			c.ellipse(r, ox+8, 8, 2.5, 2.5, white)
		}
		if hole > 0 {
			c.ellipse(r, ox+8, 8, hole, hole, transparent)
		}
	case f <= 6:
		arm := 2 + 2*(f-5)
		c.rect(x+8-arm, 8, x+8+arm+1, 9, yellow)
		c.rect(x+8, 8-arm, x+9, 8+arm+1, yellow)
		if f == 6 {
			for _, d := range [][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
				c.set(x+8+d[0], 8+d[1], yellow)
			}
		}
		c.set(x+8, 8, white)
	default:
		c.ellipse(r, ox+8, 8, 3, 2, yellow)
		c.ellipse(r, ox+8, 8, 1.5, 1, white)
	}
	c.outline(r, ink)
}

// drawUI draws a panel and a button, both nine-slices, a pressed button and a pointer.
func drawUI(c *canvas, r image.Rectangle, f int) {
	ox := float64(r.Min.X)
	x := r.Min.X
	switch f {
	case 0: // panel
		c.rect(x, 0, x+16, 16, ink)
		c.rect(x+1, 1, x+15, 15, grey2)
		c.rect(x+1, 1, x+15, 2, grey3)
		c.rect(x+3, 3, x+13, 13, grey1)
	case 1: // button
		c.rect(x, 0, x+16, 16, ink)
		c.rect(x+1, 1, x+15, 15, blue1)
		c.rect(x+1, 1, x+15, 13, blue2)
		c.rect(x+1, 1, x+15, 2, blue3)
	case 2: // pressed button
		c.rect(x, 0, x+16, 16, ink)
		c.rect(x+1, 1, x+15, 15, blue1)
		c.rect(x+1, 3, x+15, 15, blue2)
	case 3: // pointer
		pts := [][2]float64{{2, 1}, {2, 13}, {5, 10}, {8, 15}, {10, 14}, {7, 9}, {11, 9}}
		for i := range pts {
			pts[i][0] += ox
		}
		c.poly(r, pts, white)
		c.outline(r, ink)
	}
}

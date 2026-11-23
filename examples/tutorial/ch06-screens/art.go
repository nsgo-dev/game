package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// The sprite sheet is one row: the basket, the two frames of the twinkling star, then the rock.
const (
	sheetWidth  = basketWidth + 3*itemSize
	sheetHeight = itemSize
)

// twinkleFrame is how long each frame of the star's twinkle shows.
const twinkleFrame = 200 * time.Millisecond

// makeAtlas paints the game's sprite sheet in code and describes its frames, so the game runs without image files. A game with art loads its sheet with sprite.LoadAtlas instead.
func makeAtlas() *sprite.Atlas {
	sheet := image.NewRGBA(image.Rect(0, 0, sheetWidth, sheetHeight))
	paintBasket(sheet, 0)
	paintStar(sheet, basketWidth, 1, color.RGBA{R: 0xfd, G: 0xe0, B: 0x47, A: 0xff})
	paintStar(sheet, basketWidth+itemSize, 0.8, color.RGBA{R: 0xfa, G: 0xcc, B: 0x15, A: 0xff})
	paintRock(sheet, basketWidth+2*itemSize)

	basket := frame("basket", 0, basketWidth, basketHeight)
	star0 := frame("star_0", basketWidth, itemSize, itemSize)
	star1 := frame("star_1", basketWidth+itemSize, itemSize, itemSize)
	rock := frame("rock", basketWidth+2*itemSize, itemSize, itemSize)
	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{basket.Name: basket, star0.Name: star0, star1.Name: star1, rock.Name: rock},
		Animations: map[string]sprite.Animation{
			"twinkle": {
				Loop: true,
				Frames: []sprite.AnimationFrame{
					{Frame: star0, Duration: twinkleFrame},
					{Frame: star1, Duration: twinkleFrame},
				},
			},
		},
	}
}

// frame is the untrimmed frame name, w by h points at x on the sheet's row, with its pivot in its middle.
func frame(name string, x, w, h float32) sprite.Frame {
	return sprite.Frame{Name: name, X: x, W: w, H: h, Width: w, Height: h, PivotX: w / 2, PivotY: h / 2}
}

// paintBasket paints a woven basket, wider at its rim than at its base, at x on the sheet.
func paintBasket(dst *image.RGBA, x0 int) {
	body := color.RGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff}
	weave := color.RGBA{R: 0xb4, G: 0x53, B: 0x09, A: 0xff}
	rim := color.RGBA{R: 0xfc, G: 0xd3, B: 0x4d, A: 0xff}
	for y := range basketHeight {
		// The half width narrows from the rim to the base.
		half := float64(basketWidth)/2 - float64(y)*5/float64(basketHeight)
		for x := range basketWidth {
			dx := math.Abs(float64(x) + 0.5 - float64(basketWidth)/2)
			if dx > half {
				continue
			}
			c := body
			switch {
			case y < 3:
				c = rim
			case (x+y)%6 < 2:
				c = weave
			}
			dst.SetRGBA(x0+x, y, c)
		}
	}
}

// paintStar paints a five-pointed star, scale times the size of an item, centered in the item-sized cell at x on the sheet.
func paintStar(dst *image.RGBA, x0 int, scale float64, fill color.RGBA) {
	center := float64(itemSize) / 2
	outer := center * scale
	inner := outer * 0.45
	var points [10][2]float64
	for i := range points {
		r := outer
		if i%2 == 1 {
			r = inner
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		points[i] = [2]float64{center + r*math.Cos(a), center + r*math.Sin(a)}
	}
	glow := color.RGBA{R: 0xfe, G: 0xf9, B: 0xc3, A: 0xff}
	for y := range itemSize {
		for x := range itemSize {
			px, py := float64(x)+0.5, float64(y)+0.5
			if !inPolygon(points[:], px, py) {
				continue
			}
			c := fill
			if math.Hypot(px-center, py-center) < inner*0.8 {
				c = glow
			}
			dst.SetRGBA(x0+x, y, c)
		}
	}
}

// inPolygon reports whether x, y is inside the polygon of points, by the even-odd rule.
func inPolygon(points [][2]float64, x, y float64) bool {
	in := false
	for i, j := 0, len(points)-1; i < len(points); j, i = i, i+1 {
		a, b := points[i], points[j]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

// paintRock paints a lumpy grey rock with two craters, in the item-sized cell at x on the sheet.
func paintRock(dst *image.RGBA, x0 int) {
	center := float64(itemSize) / 2
	stone := color.RGBA{R: 0x78, G: 0x71, B: 0x6c, A: 0xff}
	shade := color.RGBA{R: 0x57, G: 0x53, B: 0x4e, A: 0xff}
	for y := range itemSize {
		for x := range itemSize {
			dx, dy := float64(x)+0.5-center, float64(y)+0.5-center
			// The radius wobbles with the angle, so the outline is lumpy rather than round.
			r := center * (0.82 + 0.1*math.Sin(3*math.Atan2(dy, dx)))
			if math.Hypot(dx, dy) > r {
				continue
			}
			c := stone
			if math.Hypot(dx+3, dy+2) < 2.5 || math.Hypot(dx-3, dy-3) < 2 {
				c = shade
			}
			dst.SetRGBA(x0+x, y, c)
		}
	}
}

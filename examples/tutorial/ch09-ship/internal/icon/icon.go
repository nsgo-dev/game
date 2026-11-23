// Package icon draws Star Catcher's app icon: a golden star falling into a basket on a night sky.
package icon

import (
	"image"
	"image/color"
	"math"
)

var (
	sky    = color.RGBA{R: 0x0b, G: 0x10, B: 0x26, A: 0xff}
	star   = color.RGBA{R: 0xfd, G: 0xe0, B: 0x47, A: 0xff}
	basket = color.RGBA{R: 0xf5, G: 0x9e, B: 0x0b, A: 0xff}
	rim    = color.RGBA{R: 0xfc, G: 0xd3, B: 0x4d, A: 0xff}
)

// Render draws the icon size pixels square. It draws the same pixels every time, so the icon checked into assets/ can be compared with it.
func Render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	points := starPoints(s/2, s*0.38, s*0.3, s*0.13)
	for y := range size {
		for x := range size {
			px, py := float64(x)+0.5, float64(y)+0.5
			c := sky
			switch {
			case inPolygon(points, px, py):
				c = star
			case py > s*0.66 && py < s*0.7 && math.Abs(px-s/2) < s*0.3:
				c = rim
			case py >= s*0.7 && py < s*0.82 && math.Abs(px-s/2) < s*0.3-(py-s*0.7)*0.6:
				c = basket
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// starPoints are the ten corners of a five-pointed star centered on cx, cy.
func starPoints(cx, cy, outer, inner float64) [][2]float64 {
	points := make([][2]float64, 10)
	for i := range points {
		r := outer
		if i%2 == 1 {
			r = inner
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		points[i] = [2]float64{cx + r*math.Cos(a), cy + r*math.Sin(a)}
	}
	return points
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

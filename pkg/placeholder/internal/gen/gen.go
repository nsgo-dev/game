// Package gen draws every file of the placeholder asset pack in code: sprite strips, a tileset with a Tiled tileset and map, a bitmap font, sounds and models, with the import settings sidecar of each image and sound.
//
// Files returns the same bytes on every run and every architecture: the drawing and synthesis use integer arithmetic, comparisons, square roots and products converted to float64 before they are summed, which the compiler may not fuse into a multiply-add, and no function such as math.Sin or math.Exp whose last bit differs between architectures. Package placeholder's tests compare the committed pack with Files, and rewrite it with -update.
package gen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sort"

	"github.com/nsgo-dev/game/pkg/importsettings"
)

// Files returns every file of the pack by its slash-separated path in the pack, except CREDITS.md, which a person writes.
func Files() (map[string][]byte, error) {
	files := map[string][]byte{}
	steps := []struct {
		name string
		run  func(map[string][]byte) error
	}{
		{"sprites", addSprites},
		{"tiles", addTiles},
		{"font", addFont},
		{"sounds", addSounds},
		{"models", addModels},
	}
	for _, s := range steps {
		if err := s.run(files); err != nil {
			return nil, fmt.Errorf("gen: %s: %w", s.name, err)
		}
	}
	return files, nil
}

// Paths returns the paths of files in order.
func Paths(files map[string][]byte) []string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// addSidecar encodes f as the import settings sidecar of the asset at path.
func addSidecar(files map[string][]byte, path string, f *importsettings.File) error {
	f.Version = importsettings.CurrentVersion
	data, err := importsettings.Encode(f)
	if err != nil {
		return fmt.Errorf("%s: %w", importsettings.Path(path), err)
	}
	files[importsettings.Path(path)] = data
	return nil
}

// encodePNG encodes img with the standard library's default compression.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rgb returns the opaque color 0xRRGGBB.
func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}

// The pack's palette: a few ramps shared by the sprites, tiles and font, so the pieces look like one set.
var (
	transparent = color.NRGBA{}
	ink         = rgb(0x1a1c2c) // outlines
	white       = rgb(0xf4f4f4)
	grey1       = rgb(0x333c57)
	grey2       = rgb(0x566c86)
	grey3       = rgb(0x94b0c2)
	blue1       = rgb(0x29366f)
	blue2       = rgb(0x3b5dc9)
	blue3       = rgb(0x41a6f6)
	cyan        = rgb(0x73eff7)
	green1      = rgb(0x257179)
	green2      = rgb(0x38b764)
	green3      = rgb(0xa7f070)
	yellow      = rgb(0xffcd75)
	orange      = rgb(0xef7d57)
	red         = rgb(0xb13e53)
	purple      = rgb(0x5d275d)
	brown1      = rgb(0x4a2f24)
	brown2      = rgb(0x7a4b32)
	brown3      = rgb(0xa8714a)
	gold        = rgb(0xf7b733)
	goldDark    = rgb(0xc27c0e)
	pink        = rgb(0xf27ab8)
	violet      = rgb(0x8a4fd6)
)

// canvas is an image drawn on with pixel-center tests: a shape covers a pixel when it covers the pixel's center.
type canvas struct {
	img *image.NRGBA
}

func newCanvas(w, h int) *canvas {
	return &canvas{img: image.NewNRGBA(image.Rect(0, 0, w, h))}
}

// set colors the pixel at x, y, ignoring pixels outside the image.
func (c *canvas) set(x, y int, col color.NRGBA) {
	if image.Pt(x, y).In(c.img.Rect) {
		c.img.SetNRGBA(x, y, col)
	}
}

// at returns the color of the pixel at x, y.
func (c *canvas) at(x, y int) color.NRGBA {
	return c.img.NRGBAAt(x, y)
}

// rect fills the pixels from x0, y0 up to, but not including, x1, y1.
func (c *canvas) rect(x0, y0, x1, y1 int, col color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c.set(x, y, col)
		}
	}
}

// ellipse fills the ellipse centered on cx, cy with radii rx and ry, inside clip.
func (c *canvas) ellipse(clip image.Rectangle, cx, cy, rx, ry float64, col color.NRGBA) {
	if rx <= 0 || ry <= 0 {
		return
	}
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			dx := (float64(x) + 0.5 - cx) / rx
			dy := (float64(y) + 0.5 - cy) / ry
			if float64(dx*dx)+float64(dy*dy) <= 1 {
				c.set(x, y, col)
			}
		}
	}
}

// poly fills the polygon pts, by the even-odd rule, inside clip.
func (c *canvas) poly(clip image.Rectangle, pts [][2]float64, col color.NRGBA) {
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			if inPolygon(float64(x)+0.5, float64(y)+0.5, pts) {
				c.set(x, y, col)
			}
		}
	}
}

func inPolygon(px, py float64, pts [][2]float64) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi := pts[i][0], pts[i][1]
		xj, yj := pts[j][0], pts[j][1]
		if (yi > py) != (yj > py) {
			// The edge's x at py, with the product rounded before the sum.
			xCross := xi + float64((py-yi)*(xj-xi))/(yj-yi)
			if px < xCross {
				in = !in
			}
		}
	}
	return in
}

// line draws a one-pixel line from x0, y0 to x1, y1 with Bresenham's algorithm.
func (c *canvas) line(x0, y0, x1, y1 int, col color.NRGBA) {
	dx, sx := abs(x1-x0), sign(x1-x0)
	dy, sy := -abs(y1-y0), sign(y1-y0)
	e := dx + dy
	for {
		c.set(x0, y0, col)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

// outline draws col on each clear pixel of r next to (left, right, above or below) a pixel that is not clear, giving the shapes in r a one-pixel border.
func (c *canvas) outline(r image.Rectangle, col color.NRGBA) {
	var edge []image.Point
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c.at(x, y).A != 0 {
				continue
			}
			for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				n := image.Pt(x, y).Add(d)
				if n.In(r) && c.at(n.X, n.Y).A != 0 {
					edge = append(edge, image.Pt(x, y))
					break
				}
			}
		}
	}
	for _, p := range edge {
		c.set(p.X, p.Y, col)
	}
}

// recolor replaces each pixel of from in r with to.
func (c *canvas) recolor(r image.Rectangle, from, to color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if c.at(x, y) == from {
				c.set(x, y, to)
			}
		}
	}
}

// hash returns a well mixed number for x, y and seed, for speckles that look random and never change.
func hash(x, y, seed int) uint32 {
	h := uint32(x)*0x9e3779b1 ^ uint32(y)*0x85ebca77 ^ uint32(seed)*0xc2b2ae3d
	h ^= h >> 15
	h *= 0x2c1b3c6d
	h ^= h >> 12
	h *= 0x297a2d39
	h ^= h >> 15
	return h
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

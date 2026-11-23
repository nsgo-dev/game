package effects

import (
	"fmt"
	"image"
	"math"
)

// Input is what an effect's Reference reads: the pass before it, or the frame for the first, as Input and InputSize read them in its source.
type Input struct {
	at   func(x, y float64) [4]float64
	w, h float64
}

// At returns the premultiplied color of the input at (x, y), in the frame's pixels: for the frame, the pixel under the point, transparent outside it.
func (in Input) At(x, y float64) [4]float64 { return in.at(x, y) }

// Size returns the frame's size in pixels.
func (in Input) Size() (w, h float64) { return in.w, in.h }

// Reference draws frame through the chain on the CPU into a picture of width by height pixels, with each effect's Reference, as View draws it on the GPU into a box of that many pixels: each pixel's center is mapped onto the frame and drawn by the last effect. It is the picture a test compares a chain's output with, and a screenshot of it where no GPU draws. Colors are clamped to 0 to 1 and rounded to 8 bits.
//
// It returns an error when an effect has no Reference.
func (c *Chain) Reference(frame *image.RGBA, width, height int) (*image.RGBA, error) {
	for i, e := range c.effects {
		if e.Reference == nil {
			return nil, fmt.Errorf("effects: %s has no Reference", e.label(i))
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, max(width, 0), max(height, 0)))
	if width <= 0 || height <= 0 {
		return out, nil
	}
	var fw, fh float64
	if frame != nil {
		fw, fh = float64(frame.Rect.Dx()), float64(frame.Rect.Dy())
	}
	in := Input{at: framePixel(frame), w: fw, h: fh}
	for _, e := range c.effects {
		prev, ref := in, e.Reference
		in = Input{at: func(x, y float64) [4]float64 { return ref(prev, x, y) }, w: fw, h: fh}
	}
	sx, sy := fw/float64(width), fh/float64(height)
	for py := range height {
		row := out.Pix[py*out.Stride:]
		for px := range width {
			c := in.at((float64(px)+0.5)*sx, (float64(py)+0.5)*sy)
			for k, v := range c {
				row[px*4+k] = to8(v)
			}
		}
	}
	return out, nil
}

// framePixel reads frame's pixel under a point, premultiplied from 0 to 1, transparent outside it, as image0At reads the frame's texture.
func framePixel(frame *image.RGBA) func(x, y float64) [4]float64 {
	return func(x, y float64) [4]float64 {
		if frame == nil || math.IsNaN(x) || math.IsNaN(y) {
			return [4]float64{}
		}
		fx, fy := math.Floor(x), math.Floor(y)
		if fx < 0 || fy < 0 || fx >= float64(frame.Rect.Dx()) || fy >= float64(frame.Rect.Dy()) {
			return [4]float64{}
		}
		i := frame.PixOffset(frame.Rect.Min.X+int(fx), frame.Rect.Min.Y+int(fy))
		p := frame.Pix[i : i+4 : i+4]
		return [4]float64{float64(p[0]) / 255, float64(p[1]) / 255, float64(p[2]) / 255, float64(p[3]) / 255}
	}
}

// to8 clamps v to 0 to 1 and rounds it to 8 bits.
func to8(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(v * 255))
}

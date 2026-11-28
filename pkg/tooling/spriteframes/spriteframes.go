// Package spriteframes turns a sequence of raster frames into deterministic,
// atlas-ready images.
package spriteframes

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// KeyOptions controls chroma-key removal. RGB values are compared in 8-bit
// Euclidean distance. Softness adds a linear alpha ramp after Tolerance.
type KeyOptions struct {
	Color     color.NRGBA
	Tolerance uint8
	Softness  uint8
	Despill   float64
	FillHoles bool
}

// ChromaKey returns copies of frames with the key colour made transparent.
func ChromaKey(frames []*image.NRGBA, opts KeyOptions) ([]*image.NRGBA, error) {
	if len(frames) == 0 {
		return nil, errors.New("spriteframes: no frames")
	}
	if opts.Despill < 0 || opts.Despill > 1 || math.IsNaN(opts.Despill) {
		return nil, fmt.Errorf("spriteframes: despill must be between 0 and 1")
	}
	out := make([]*image.NRGBA, len(frames))
	for fi, src := range frames {
		if src == nil {
			return nil, fmt.Errorf("spriteframes: frame %d is nil", fi)
		}
		dst := image.NewNRGBA(src.Bounds())
		for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
			for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
				p := src.NRGBAAt(x, y)
				d := colorDistance(p, opts.Color)
				cut := float64(opts.Tolerance)
				a := 255.0
				if d <= cut {
					a = 0
				} else if opts.Softness > 0 && d < cut+float64(opts.Softness) {
					a = 255 * (d - cut) / float64(opts.Softness)
				}
				if opts.Despill > 0 && a < 255 {
					spill := float64(opts.Color.G) - float64(p.G)
					if spill > 0 {
						p.G = uint8(float64(p.G) + spill*opts.Despill*(1-a/255))
					}
				}
				p.A = uint8(math.Round(float64(p.A) * a / 255))
				dst.SetNRGBA(x, y, p)
			}
		}
		if opts.FillHoles {
			fillHoles(dst)
		}
		out[fi] = dst
	}
	return out, nil
}

func colorDistance(a, b color.NRGBA) float64 {
	dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func fillHoles(img *image.NRGBA) {
	for y := img.Bounds().Min.Y + 1; y < img.Bounds().Max.Y-1; y++ {
		for x := img.Bounds().Min.X + 1; x < img.Bounds().Max.X-1; x++ {
			if img.NRGBAAt(x, y).A != 0 {
				continue
			}
			var sumR, sumG, sumB, n int
			for _, q := range []image.Point{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				p := img.NRGBAAt(q.X, q.Y)
				if p.A > 0 {
					sumR += int(p.R)
					sumG += int(p.G)
					sumB += int(p.B)
					n++
				}
			}
			if n == 4 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(sumR / n), uint8(sumG / n), uint8(sumB / n), 255})
			}
		}
	}
}

// LoopSeam rotates frames so the least-visible adjacent transition is the
// loop seam. A duplicate final frame is removed before selecting the seam.
func LoopSeam(frames []*image.NRGBA) ([]*image.NRGBA, error) {
	if len(frames) == 0 {
		return nil, errors.New("spriteframes: no frames")
	}
	for _, f := range frames {
		if f == nil {
			return nil, errors.New("spriteframes: nil frame")
		}
	}
	n := len(frames)
	if n > 1 && frameDistance(frames[0], frames[n-1]) == 0 {
		n--
		frames = frames[:n]
	}
	if n < 2 {
		return append([]*image.NRGBA(nil), frames...), nil
	}
	best, score := 0, math.Inf(1)
	for i := 0; i < n-1; i++ {
		if d := frameDistance(frames[i], frames[i+1]); d < score {
			score, best = d, i
		}
	}
	result := make([]*image.NRGBA, 0, n)
	result = append(result, frames[best+1:]...)
	result = append(result, frames[:best+1]...)
	return result, nil
}

func frameDistance(a, b *image.NRGBA) float64 {
	ba, bb := a.Bounds(), b.Bounds()
	w, h := ba.Dx(), ba.Dy()
	if bb.Dx() != w || bb.Dy() != h {
		return math.Inf(1)
	}
	var total float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pa, pb := a.NRGBAAt(ba.Min.X+x, ba.Min.Y+y), b.NRGBAAt(bb.Min.X+x, bb.Min.Y+y)
			total += colorDistance(pa, pb)
		}
	}
	return total
}

// AnchorMode controls shared-frame placement.
type AnchorMode string

const AnchorBottomCenter AnchorMode = "bottom-center"

// Align returns equally sized frames with their non-transparent content
// aligned to a shared bottom-center anchor. Empty frames are rejected.
func Align(frames []*image.NRGBA, mode AnchorMode) ([]*image.NRGBA, image.Rectangle, error) {
	if mode != AnchorBottomCenter {
		return nil, image.Rectangle{}, fmt.Errorf("spriteframes: unsupported anchor %q", mode)
	}
	if len(frames) == 0 {
		return nil, image.Rectangle{}, errors.New("spriteframes: no frames")
	}
	type bounds struct{ r image.Rectangle }
	bs := make([]bounds, len(frames))
	maxW, maxH := 0, 0
	for i, f := range frames {
		if f == nil {
			return nil, image.Rectangle{}, fmt.Errorf("spriteframes: frame %d is nil", i)
		}
		r := alphaBounds(f)
		if r.Empty() {
			return nil, image.Rectangle{}, fmt.Errorf("spriteframes: frame %d is fully transparent", i)
		}
		bs[i].r = r
		if r.Dx() > maxW {
			maxW = r.Dx()
		}
		if r.Dy() > maxH {
			maxH = r.Dy()
		}
	}
	canvas := image.Rect(0, 0, maxW, maxH)
	out := make([]*image.NRGBA, len(frames))
	for i, f := range frames {
		dst := image.NewNRGBA(canvas)
		r := bs[i].r
		dx := (maxW-r.Dx())/2 - r.Min.X
		dy := maxH - r.Max.Y
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				dst.SetNRGBA(x+dx, y+dy, f.NRGBAAt(x, y))
			}
		}
		out[i] = dst
	}
	return out, canvas, nil
}

func alphaBounds(img *image.NRGBA) image.Rectangle {
	r := image.Rectangle{}
	first := true
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				continue
			}
			p := image.Rect(x, y, x+1, y+1)
			if first {
				r = p
				first = false
			} else {
				r = r.Union(p)
			}
		}
	}
	return r
}

// ResizeNearest scales each frame to width and height using nearest-neighbor
// sampling. It is useful for reducing source renders to a pixel grid.
func ResizeNearest(frames []*image.NRGBA, width, height int) ([]*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, errors.New("spriteframes: output size must be positive")
	}
	out := make([]*image.NRGBA, len(frames))
	for i, src := range frames {
		if src == nil {
			return nil, fmt.Errorf("spriteframes: frame %d is nil", i)
		}
		dst := image.NewNRGBA(image.Rect(0, 0, width, height))
		b := src.Bounds()
		for y := 0; y < height; y++ {
			sy := b.Min.Y + y*b.Dy()/height
			for x := 0; x < width; x++ {
				sx := b.Min.X + x*b.Dx()/width
				dst.SetNRGBA(x, y, src.NRGBAAt(sx, sy))
			}
		}
		out[i] = dst
	}
	return out, nil
}

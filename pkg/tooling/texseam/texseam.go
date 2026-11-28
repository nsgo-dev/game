// Package texseam checks and repairs image seams and derives tangent-space maps
// for textures used by the game renderer.
package texseam

import (
	"image"
	"image/color"
	"math"
)

const DefaultThreshold = 1.3

// SeamReport contains the wrap-edge and interior differences for both axes.
// Ratios at or below Threshold pass the tiling check.
type SeamReport struct {
	WrapXRatio float64
	WrapYRatio float64
	InteriorX  float64
	InteriorY  float64
	Threshold  float64
	Pass       bool
}

// Check measures the left/right and top/bottom wrap seams using the default
// threshold. Differences are normalized to the mean difference of interior
// neighbors on the same axis.
func Check(img image.Image) SeamReport {
	return CheckWithThreshold(img, DefaultThreshold)
}

// CheckWithThreshold is Check with an explicit ratio threshold.
func CheckWithThreshold(img image.Image, threshold float64) SeamReport {
	if threshold <= 0 || math.IsNaN(threshold) {
		threshold = DefaultThreshold
	}
	b := bounds(img)
	if b.Dx() < 2 || b.Dy() < 2 {
		return SeamReport{Threshold: threshold, Pass: true}
	}
	wrapX, interiorX := edgeDifference(img, b, true)
	wrapY, interiorY := edgeDifference(img, b, false)
	rx := ratio(wrapX, interiorX)
	ry := ratio(wrapY, interiorY)
	return SeamReport{WrapXRatio: rx, WrapYRatio: ry, InteriorX: interiorX, InteriorY: interiorY, Threshold: threshold, Pass: rx <= threshold && ry <= threshold}
}

func edgeDifference(img image.Image, b image.Rectangle, xAxis bool) (wrap, interior float64) {
	var wrapCount, interiorCount int
	if xAxis {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			wrap += pixelDifference(img.At(b.Min.X, y), img.At(b.Max.X-1, y))
			wrapCount++
			for x := b.Min.X; x+1 < b.Max.X-1; x++ {
				interior += pixelDifference(img.At(x, y), img.At(x+1, y))
				interiorCount++
			}
		}
	} else {
		for x := b.Min.X; x < b.Max.X; x++ {
			wrap += pixelDifference(img.At(x, b.Min.Y), img.At(x, b.Max.Y-1))
			wrapCount++
			for y := b.Min.Y; y+1 < b.Max.Y-1; y++ {
				interior += pixelDifference(img.At(x, y), img.At(x, y+1))
				interiorCount++
			}
		}
	}
	if wrapCount > 0 {
		wrap /= float64(wrapCount)
	}
	if interiorCount > 0 {
		interior /= float64(interiorCount)
	}
	return wrap, interior
}

func ratio(edge, interior float64) float64 {
	if interior <= 1e-12 {
		if edge <= 1e-12 {
			return 0
		}
		return math.Inf(1)
	}
	return edge / interior
}

func pixelDifference(a, b color.Color) float64 {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return (math.Abs(float64(ar)-float64(br)) + math.Abs(float64(ag)-float64(bg)) + math.Abs(float64(ab)-float64(bb)) + math.Abs(float64(aa)-float64(ba))) / (4 * 65535)
}

// FixOptions controls seam repair. Band is the number of pixels over which
// each edge is blended toward the opposite edge. Zero uses roughly one eighth
// of the smaller dimension, capped at 16 pixels.
type FixOptions struct {
	Band int
}

// Fix repairs wrap seams by matching opposite edges and feathering the change
// into the image. It preserves the input dimensions and color-independent
// deterministic output.
func Fix(img image.Image, opts FixOptions) *image.RGBA {
	b := bounds(img)
	out := cloneRGBA(img, b)
	if b.Dx() < 2 || b.Dy() < 2 {
		return out
	}
	band := opts.Band
	if band <= 0 {
		band = b.Dx()
		if b.Dy() < band {
			band = b.Dy()
		}
		band /= 8
		if band < 1 {
			band = 1
		}
		if band > 16 {
			band = 16
		}
	}
	if band*2 >= b.Dx() {
		band = max(1, b.Dx()/2-1)
	}
	if band*2 >= b.Dy() {
		band = max(1, b.Dy()/2-1)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		left := out.RGBAAt(b.Min.X, y)
		right := out.RGBAAt(b.Max.X-1, y)
		match := average(left, right)
		for i := 0; i < band; i++ {
			alpha := cosineFade(i, band)
			out.SetRGBA(b.Min.X+i, y, blend(out.RGBAAt(b.Min.X+i, y), match, alpha))
			out.SetRGBA(b.Max.X-1-i, y, blend(out.RGBAAt(b.Max.X-1-i, y), match, alpha))
		}
	}
	for x := b.Min.X; x < b.Max.X; x++ {
		top := out.RGBAAt(x, b.Min.Y)
		bottom := out.RGBAAt(x, b.Max.Y-1)
		match := average(top, bottom)
		for i := 0; i < band; i++ {
			alpha := cosineFade(i, band)
			out.SetRGBA(x, b.Min.Y+i, blend(out.RGBAAt(x, b.Min.Y+i), match, alpha))
			out.SetRGBA(x, b.Max.Y-1-i, blend(out.RGBAAt(x, b.Max.Y-1-i), match, alpha))
		}
	}
	return out
}

// Merge keeps edited pixels only in a feathered vertical and horizontal band
// around the tile center. Pixels outside those bands remain byte-identical to
// original.
func Merge(original, edited image.Image, band int) *image.RGBA {
	b := bounds(original)
	out := cloneRGBA(original, b)
	if edited == nil || band <= 0 || b.Empty() {
		return out
	}
	editedBounds := edited.Bounds()
	centerX, centerY := b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := abs(x-centerX), abs(y-centerY)
			alpha := math.Max(featherAlpha(dx, band), featherAlpha(dy, band))
			if alpha == 0 {
				continue
			}
			if !image.Pt(x, y).In(editedBounds) {
				continue
			}
			out.SetRGBA(x, y, blend(out.RGBAAt(x, y), color.RGBAModel.Convert(edited.At(x, y)).(color.RGBA), alpha))
		}
	}
	return out
}

// Height converts an image to luminance height, applying a deterministic box
// blur with the requested radius before returning an 8-bit grayscale image.
func Height(img image.Image, blur int) *image.Gray {
	b := bounds(img)
	out := image.NewGray(b)
	if img == nil || b.Empty() {
		return out
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			v := (299*int(r) + 587*int(g) + 114*int(bl)) / 1000
			out.SetGray(x, y, color.Gray{Y: uint8(v >> 8)})
		}
	}
	if blur <= 0 {
		return out
	}
	return boxBlur(out, blur)
}

// SampleMode selects edge behavior for Normal sampling.
type SampleMode uint8

const (
	Wrap SampleMode = iota
	Clamp
)

// Normal derives an RGBA8 tangent-space normal map from a height image. The
// channels use the renderer convention (128,128,255) for a flat normal.
func Normal(height image.Image, strength float64, mode SampleMode) *image.NRGBA {
	b := bounds(height)
	out := image.NewNRGBA(b)
	if height == nil || b.Empty() {
		return out
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			left := heightSample(height, x-1, y, mode)
			right := heightSample(height, x+1, y, mode)
			up := heightSample(height, x, y-1, mode)
			down := heightSample(height, x, y+1, mode)
			nx := -(right - left) * strength
			ny := -(down - up) * strength
			nz := 1.0
			length := math.Sqrt(nx*nx + ny*ny + nz*nz)
			nx, ny, nz = nx/length, ny/length, nz/length
			out.SetNRGBA(x, y, color.NRGBA{R: toByte(nx), G: toByte(ny), B: toByte(nz), A: 255})
		}
	}
	return out
}

// Roughness derives an inverted, contrast-stretched grayscale roughness map.
func Roughness(height image.Image, contrast float64) *image.Gray {
	b := bounds(height)
	out := image.NewGray(b)
	if height == nil || b.Empty() {
		return out
	}
	if contrast <= 0 || math.IsNaN(contrast) {
		contrast = 1
	}
	minV, maxV := 1.0, 0.0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := grayValue(height.At(x, y))
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
	}
	span := maxV - minV
	if span == 0 {
		span = 1
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := (grayValue(height.At(x, y)) - minV) / span
			v = (v-0.5)*contrast + 0.5
			v = 1 - clamp(v)
			out.SetGray(x, y, color.Gray{Y: uint8(math.Round(v * 255))})
		}
	}
	return out
}

func bounds(img image.Image) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	return img.Bounds()
}
func cloneRGBA(img image.Image, b image.Rectangle) *image.RGBA {
	out := image.NewRGBA(b)
	if img != nil {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				out.SetRGBA(x, y, color.RGBAModel.Convert(img.At(x, y)).(color.RGBA))
			}
		}
	}
	return out
}
func average(a, b color.RGBA) color.RGBA {
	return color.RGBA{uint8((uint16(a.R) + uint16(b.R)) / 2), uint8((uint16(a.G) + uint16(b.G)) / 2), uint8((uint16(a.B) + uint16(b.B)) / 2), uint8((uint16(a.A) + uint16(b.A)) / 2)}
}
func blend(a, b color.RGBA, t float64) color.RGBA {
	t = clamp(t)
	return color.RGBA{uint8(math.Round(float64(a.R)*(1-t) + float64(b.R)*t)), uint8(math.Round(float64(a.G)*(1-t) + float64(b.G)*t)), uint8(math.Round(float64(a.B)*(1-t) + float64(b.B)*t)), uint8(math.Round(float64(a.A)*(1-t) + float64(b.A)*t))}
}
func cosineFade(i, n int) float64 {
	if n <= 1 {
		return 1
	}
	return 0.5 + 0.5*math.Cos(math.Pi*float64(i)/float64(n-1))
}
func featherAlpha(distance, band int) float64 {
	if distance > band {
		return 0
	}
	return 0.5 + 0.5*math.Cos(math.Pi*float64(distance)/float64(band))
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func grayValue(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 65535
}
func toByte(v float64) uint8 { return uint8(math.Round(clamp(v*0.5+0.5) * 255)) }
func heightSample(img image.Image, x, y int, mode SampleMode) float64 {
	b := img.Bounds()
	if mode == Wrap {
		x = b.Min.X + ((x-b.Min.X)%b.Dx()+b.Dx())%b.Dx()
		y = b.Min.Y + ((y-b.Min.Y)%b.Dy()+b.Dy())%b.Dy()
	} else {
		if x < b.Min.X {
			x = b.Min.X
		}
		if x >= b.Max.X {
			x = b.Max.X - 1
		}
		if y < b.Min.Y {
			y = b.Min.Y
		}
		if y >= b.Max.Y {
			y = b.Max.Y - 1
		}
	}
	return grayValue(img.At(x, y))
}
func boxBlur(src *image.Gray, radius int) *image.Gray {
	b := src.Bounds()
	out := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var sum, n int
			for yy := y - radius; yy <= y+radius; yy++ {
				cy := yy
				if cy < b.Min.Y {
					cy = b.Min.Y
				}
				if cy >= b.Max.Y {
					cy = b.Max.Y - 1
				}
				for xx := x - radius; xx <= x+radius; xx++ {
					cx := xx
					if cx < b.Min.X {
						cx = b.Min.X
					}
					if cx >= b.Max.X {
						cx = b.Max.X - 1
					}
					sum += int(src.GrayAt(cx, cy).Y)
					n++
				}
			}
			out.SetGray(x, y, color.Gray{Y: uint8(sum / n)})
		}
	}
	return out
}

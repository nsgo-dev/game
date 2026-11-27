package postfx

import (
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// maxPaletteColors caps how many columns PaletteSwap scans in row 0 so a large texture cannot stall a fragment shader.
	maxPaletteColors = 256
	// defaultPaletteTolerance is the minimum per-channel tolerance for PaletteSwap: about half an 8-bit step, so identical 8-bit colors always match while distinct 8-bit colors (>= 1/255) never collide.
	defaultPaletteTolerance = 0.002
)

// paletteSwapSource is PaletteSwap's Kage program. Its matching, row blending and premultiplication match paletteSwapReference on the CPU.
const paletteSwapSource = `package main

// Amount is how far matched colors turn to the target row, 0 to 1: 0 or less, or NaN, returns the input unchanged.
var Amount float

// Row is the palette row to swap to, from 0 (the source keys) to the texture's height minus 1; fractional values blend adjacent rows.
var Row float

// Tolerance is the largest difference in any straight RGB channel, 0 to 1, that matches a key color in row 0; values at or below 0.002 match exact 8-bit colors, and 1 matches the nearest key color.
var Tolerance float

func Effect(pos vec2) vec4 {
	c := Input(pos)
	if !(Amount > 0) {
		return c
	}
	size := TextureSize()
	if !(size.x >= 1 && size.y >= 1) || !(c.a > 0) {
		return c
	}
	ca := min(c.a, 1.0)
	rgb := clamp(c.rgb/c.a, 0.0, 1.0)
	minTol := 0.002 / ca
	tol := Tolerance
	if !(tol > minTol) {
		tol = minTol
	}
	n := int(size.x)
	if n > 256 {
		n = 256
	}
	bestIdx := -1
	bestDist := 4.0
	for i := 0; i < n; i++ {
		key := Texture(vec2(float(i)+0.5, 0.5))
		if !(key.a > 0) {
			continue
		}
		krgb := clamp(key.rgb/key.a, 0.0, 1.0)
		diff := abs(rgb - krgb)
		if max(diff.r, max(diff.g, diff.b)) <= tol {
			d2 := dot(diff, diff)
			if d2 < bestDist {
				bestDist = d2
				bestIdx = i
				if d2 < 1e-9 {
					break
				}
			}
		}
	}
	if bestIdx < 0 {
		return c
	}
	maxRow := size.y - 1.0
	row := Row
	if !(row > 0) {
		row = 0
	}
	if row > maxRow {
		row = maxRow
	}
	r0 := floor(row)
	r1 := min(r0+1.0, maxRow)
	frac := row - r0
	dst0 := Texture(vec2(float(bestIdx)+0.5, r0+0.5))
	dst1 := Texture(vec2(float(bestIdx)+0.5, r1+0.5))
	swapped := (dst0 + (dst1-dst0)*frac) * ca
	return c + (swapped-c)*min(Amount, 1.0)
}
`

// paletteRampSource is PaletteRamp's Kage program. Its luminance and row blending match paletteRampReference on the CPU.
const paletteRampSource = `package main

// Amount is how far the frame turns to the palette ramp, 0 to 1: 0 or less, or NaN, returns the input unchanged.
var Amount float

// Row is the palette row to map into, from 0 to the texture's height minus 1; fractional values blend adjacent rows.
var Row float

func Effect(pos vec2) vec4 {
	c := Input(pos)
	if !(Amount > 0) {
		return c
	}
	size := TextureSize()
	if !(size.x >= 1 && size.y >= 1) || !(c.a > 0) {
		return c
	}
	ca := min(c.a, 1.0)
	rgb := clamp(c.rgb/c.a, 0.0, 1.0)
	l := clamp(dot(rgb, vec3(0.2126, 0.7152, 0.0722)), 0.0, 1.0)
	col := floor(l*(size.x-1.0) + 0.5)
	maxRow := size.y - 1.0
	row := Row
	if !(row > 0) {
		row = 0
	}
	if row > maxRow {
		row = maxRow
	}
	r0 := floor(row)
	r1 := min(r0+1.0, maxRow)
	frac := row - r0
	dst0 := Texture(vec2(col+0.5, r0+0.5))
	dst1 := Texture(vec2(col+0.5, r1+0.5))
	swapped := (dst0 + (dst1-dst0)*frac) * ca
	return c + (swapped-c)*min(Amount, 1.0)
}
`

// PaletteSwap returns an effect that swaps colors through a palette texture: row 0 holds up to 256 source (key) colors (transparent pixels are skipped), and each row from 0 to height-1 holds a target palette. A pixel whose straight RGB matches a key in row 0 within Tolerance is replaced by the color at that column in Row, premultiplied by the pixel's alpha; unmatched colors and transparent pixels pass through unchanged.
//
// Its uniforms, by name:
//
//   - Amount, from 0 to 1: how far matched colors turn to the target row; 0 or less, the default, or NaN returns the input unchanged.
//   - Row, from 0 to height-1: which palette row to swap to; clamped to the texture's height, with NaN as 0. Fractional values blend linearly between adjacent rows, so animating Row transitions smoothly between palettes on the frame clock.
//   - Tolerance, from 0 to 1: the largest difference in any straight RGB channel that matches a key in row 0. Values at or below 0.002/alpha (including 0, the default, and NaN) match exact 8-bit colors, accounting for premultiplied alpha rounding; larger values pick the nearest matching key in Euclidean RGB distance, and 1 maps every non-transparent color to its nearest key.
//
// palette is bound as the effect's Texture and may be left zero to supply or replace it per view via View.Textures or View.TexturesFunc. The effect has a Reference, so a chain of it draws a reference picture when the palette has Pixels or Data.
func PaletteSwap(palette core.ImageSource) Effect {
	return Effect{
		Name:      "palette-swap",
		Source:    paletteSwapSource,
		Reference: paletteSwapReference,
		Texture:   palette,
	}
}

// PaletteRamp returns an effect that maps each pixel's straight Rec. 709 luminance through a palette texture: each row is a dark-to-light palette of any width, with column 0 for black and column width-1 for white, preserving the pixel's alpha (multiplied by the palette entry's alpha).
//
// Its uniforms, by name:
//
//   - Amount, from 0 to 1: how far the frame turns to the palette; 0 or less, the default, or NaN returns the input unchanged.
//   - Row, from 0 to height-1: which palette row to map into; clamped to the texture's height, with NaN as 0. Fractional values blend linearly between adjacent rows.
//
// palette is bound as the effect's Texture and may be left zero to supply or replace it per view via View.Textures or View.TexturesFunc. The effect has a Reference, so a chain of it draws a reference picture when the palette has Pixels or Data.
func PaletteRamp(palette core.ImageSource) Effect {
	return Effect{
		Name:      "palette-ramp",
		Source:    paletteRampSource,
		Reference: paletteRampReference,
		Texture:   palette,
	}
}

// PaletteImage builds a premultiplied RGBA palette texture from rows of colors, one row per palette. Shorter rows are padded on the right with transparent pixels. Returns an empty image when rows has no colors.
func PaletteImage(rows ...[]core.Color) *image.RGBA {
	w := 0
	for _, row := range rows {
		if len(row) > w {
			w = len(row)
		}
	}
	if w == 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	img := image.NewRGBA(image.Rect(0, 0, w, len(rows)))
	for y, row := range rows {
		for x, c := range row {
			r, g, b, a := c.Components()
			img.SetRGBA(x, y, color.RGBA{
				R: to8(r * a),
				G: to8(g * a),
				B: to8(b * a),
				A: to8(a),
			})
		}
	}
	return img
}

// paletteSwapReference draws PaletteSwap on the CPU, matching paletteSwapSource on the GPU.
func paletteSwapReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	c := in.At(x, y)
	amount := Float(u, "Amount")
	if !(amount > 0) {
		return c
	}
	tw, th := in.TextureSize()
	if !(tw >= 1 && th >= 1) || !(c[3] > 0) {
		return c
	}
	ca := math.Min(c[3], 1)
	r, g, b := clamp01(c[0]/c[3]), clamp01(c[1]/c[3]), clamp01(c[2]/c[3])
	minTol := defaultPaletteTolerance / ca
	tol := Float(u, "Tolerance")
	if !(tol > minTol) {
		tol = minTol
	}
	n := min(int(tw), maxPaletteColors)
	bestIdx := -1
	bestDist := 4.0
	for i := range n {
		key := in.TextureAt(float64(i)+0.5, 0.5)
		if !(key[3] > 0) {
			continue
		}
		kr, kg, kb := clamp01(key[0]/key[3]), clamp01(key[1]/key[3]), clamp01(key[2]/key[3])
		dr, dg, db := math.Abs(r-kr), math.Abs(g-kg), math.Abs(b-kb)
		if math.Max(dr, math.Max(dg, db)) <= tol {
			d2 := dr*dr + dg*dg + db*db
			if d2 < bestDist {
				bestDist = d2
				bestIdx = i
				if d2 < 1e-9 {
					break
				}
			}
		}
	}
	if bestIdx < 0 {
		return c
	}
	maxRow := th - 1
	row := Float(u, "Row")
	if !(row > 0) {
		row = 0
	}
	if row > maxRow {
		row = maxRow
	}
	r0 := math.Floor(row)
	r1 := math.Min(r0+1, maxRow)
	frac := row - r0
	col := float64(bestIdx) + 0.5
	dst0 := in.TextureAt(col, r0+0.5)
	dst1 := in.TextureAt(col, r1+0.5)
	k := math.Min(amount, 1)
	for i := range c {
		swapped := (dst0[i] + (dst1[i]-dst0[i])*frac) * ca
		c[i] += (swapped - c[i]) * k
	}
	return c
}

// paletteRampReference draws PaletteRamp on the CPU, matching paletteRampSource on the GPU.
func paletteRampReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	c := in.At(x, y)
	amount := Float(u, "Amount")
	if !(amount > 0) {
		return c
	}
	tw, th := in.TextureSize()
	if !(tw >= 1 && th >= 1) || !(c[3] > 0) {
		return c
	}
	ca := math.Min(c[3], 1)
	r, g, b := saturate(c[0]/c[3]), saturate(c[1]/c[3]), saturate(c[2]/c[3])
	l := saturate(0.2126*r + 0.7152*g + 0.0722*b)
	col := math.Floor(l*(tw-1)+0.5) + 0.5
	maxRow := th - 1
	row := Float(u, "Row")
	if !(row > 0) {
		row = 0
	}
	if row > maxRow {
		row = maxRow
	}
	r0 := math.Floor(row)
	r1 := math.Min(r0+1, maxRow)
	frac := row - r0
	dst0 := in.TextureAt(col, r0+0.5)
	dst1 := in.TextureAt(col, r1+0.5)
	k := math.Min(amount, 1)
	for i := range c {
		swapped := (dst0[i] + (dst1[i]-dst0[i])*frac) * ca
		c[i] += (swapped - c[i]) * k
	}
	return c
}

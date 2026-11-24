package sprite

import (
	"bytes"
	"image"
	"math"
	"slices"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// maxOutlineThickness bounds the per-sprite outline radius in source pixels
	// so extreme values cannot allocate unbounded pixel buffers.
	maxOutlineThickness = 64
	// effectPoolKeepFloor is the minimum number of scratch slots retained on a
	// Batch before idle tail slots are released.
	effectPoolKeepFloor = 4
)

// nextEffectVersion issues process-unique ImageSource.Version values whenever
// a sprite effect buffer is written, preventing pixel-cache collisions across
// frames or temporary Batch literals.
var nextEffectVersion atomic.Uint64

// Flash blends a sprite's non-transparent RGB toward Color by Strength in
// [0, 1] while preserving each pixel's alpha. Strength <= 0 (or NaN) disables
// the flash; Strength >= 1 draws a solid-color silhouette.
type Flash struct {
	// Color is the target flash or silhouette color. Its RGB channels set the
	// color; the zero Color is black.
	Color core.Color
	// Strength is the blend amount in [0, 1].
	Strength float32
}

// Silhouette returns a full-strength Flash that draws the sprite's alpha shape
// in solid color c.
func Silhouette(c core.Color) Flash {
	return Flash{Color: c, Strength: 1}
}

func (f Flash) active() bool {
	return f.Strength > 0 && !math.IsNaN(float64(f.Strength))
}

func (f Flash) strength() float64 {
	if !f.active() {
		return 0
	}
	if f.Strength >= 1 || math.IsInf(float64(f.Strength), 1) {
		return 1
	}
	return float64(f.Strength)
}

// Outline draws a solid-color border around a sprite's opaque pixels without
// clipping at the frame edge or sampling adjacent frames in the atlas.
type Outline struct {
	// Color is the outline color. Its alpha sets the outline opacity; a Color
	// with alpha 0 draws no outline.
	Color core.Color
	// Thickness is the border width in source pixels, clamped to [0, 64].
	// Values <= 0 or NaN disable the outline; values in (0, 1) draw a 1-pixel
	// border with opacity scaled by Thickness so outlines can animate smoothly.
	Thickness float32
	// AlphaThreshold is the minimum pixel alpha in (0, 1] treated as inside
	// the sprite silhouette. Zero (or NaN) treats any non-transparent pixel
	// (alpha >= 1/255) as inside.
	AlphaThreshold float32
}

func (o Outline) active() bool {
	if !(o.Thickness > 0) || math.IsNaN(float64(o.Thickness)) {
		return false
	}
	_, _, _, a := o.Color.Components()
	return a > 0
}

func (o Outline) thickness() float64 {
	if !o.active() {
		return 0
	}
	t := float64(o.Thickness)
	if math.IsInf(t, 1) || t > maxOutlineThickness {
		return maxOutlineThickness
	}
	return t
}

func (o Outline) padding() int {
	t := o.thickness()
	if t <= 0 {
		return 0
	}
	return max(1, int(math.Ceil(t)))
}

func (o Outline) thresholdByte() uint8 {
	if !(o.AlphaThreshold > 0) || math.IsNaN(float64(o.AlphaThreshold)) {
		return 1
	}
	v := float64(o.AlphaThreshold)
	if v >= 1 || math.IsInf(v, 1) {
		return 255
	}
	b := int(math.Ceil(v * 255))
	return uint8(min(max(b, 1), 255))
}

// Dissolve progressively erodes a sprite's pixels as Threshold advances from 0
// (intact) to 1 (fully dissolved), optionally coloring a glowing band along
// the dissolve front.
type Dissolve struct {
	// Threshold is the dissolve progress in [0, 1]. Values <= 0 or NaN leave
	// the sprite intact; values >= 1 dissolve every pixel to transparent.
	Threshold float32
	// EdgeWidth is the width of the glowing edge above Threshold, in [0, 1].
	// Surviving pixels whose noise lies in [Threshold, Threshold+EdgeWidth)
	// are blended toward EdgeColor.
	EdgeWidth float32
	// EdgeColor is the dissolve front color when EdgeWidth > 0. Its alpha sets
	// the blend strength over surviving edge pixels.
	EdgeColor core.Color
	// Seed selects the deterministic built-in noise pattern when Noise is nil.
	Seed uint32
	// Scale controls the spatial cell size of the built-in noise in source
	// pixels. Values <= 1 (or NaN) sample per-pixel hash noise; larger values
	// interpolate smooth value-noise cells.
	Scale float32
	// Noise is an optional grayscale noise image sampled in [0, 1] across the
	// frame. When nil, deterministic value noise seeded by Seed is used.
	Noise image.Image
}

func (d Dissolve) active() bool {
	return d.Threshold > 0 && !math.IsNaN(float64(d.Threshold))
}

func (d Dissolve) fullyDissolved() bool {
	return d.Threshold >= 1 && !math.IsNaN(float64(d.Threshold))
}

func (d Dissolve) threshold() float64 {
	if !d.active() {
		return 0
	}
	if d.Threshold >= 1 || math.IsInf(float64(d.Threshold), 1) {
		return 1
	}
	return float64(d.Threshold)
}

func (d Dissolve) edgeWidth() float64 {
	if !(d.EdgeWidth > 0) || math.IsNaN(float64(d.EdgeWidth)) {
		return 0
	}
	if d.EdgeWidth >= 1 || math.IsInf(float64(d.EdgeWidth), 1) {
		return 1
	}
	return float64(d.EdgeWidth)
}

func (d Dissolve) scale() float64 {
	if !(d.Scale > 1) || math.IsNaN(float64(d.Scale)) || math.IsInf(float64(d.Scale), 0) {
		return 1
	}
	return float64(d.Scale)
}

// PaletteEntry maps one straight sRGB source color to a replacement color in a
// PaletteSwap.
type PaletteEntry struct {
	// From is the straight sRGB source color to match.
	From core.Color
	// To is the replacement color. Its alpha scales the matched pixel's alpha,
	// so an opaque core.RGB preserves the original pixel's opacity.
	To core.Color
}

// PaletteSwap remaps sprite colors using an ordered table of PaletteEntry
// replacements.
type PaletteSwap struct {
	// Entries are checked in order; the first match wins.
	Entries []PaletteEntry
	// Tolerance is the maximum per-channel 8-bit RGB difference allowed for a
	// match. Zero requires an exact 8-bit match.
	Tolerance uint8
}

func (p PaletteSwap) active() bool { return len(p.Entries) > 0 }

// Effect describes per-sprite visual effects applied to one Sprite's pixels
// before compositing onto a drawing, leaving neighboring sprites and anything
// drawn under the sprite unchanged.
//
// Its zero value applies no effect, so a Sprite with a nil or inactive Effect
// remains on Batch's DrawAtlas fast path. Multiple stages may be combined on
// one Effect; they run in deterministic order:
//
//  1. PaletteSwap (matches and remaps source atlas colors)
//  2. Sprite.Tint (multiplies base sprite color when Tint is also set)
//  3. Dissolve    (discards pixels below threshold and tints the dissolve edge)
//  4. Flash       (blends RGB toward a solid color or silhouette, preserving alpha)
//  5. Outline     (draws a solid border around opaque pixels with padding)
//  6. Custom      (optional per-pixel hook over premultiplied RGBA)
//
// Parameters may change every frame. Batch reuses per-slot RGBA pixel buffers
// across frames so changing parameters does not leak memory.
type Effect struct {
	// Flash blends sprite RGB toward a solid color (or silhouette at Strength 1).
	Flash Flash
	// Outline draws a solid-color alpha border around opaque sprite pixels.
	Outline Outline
	// Dissolve erodes sprite pixels against deterministic or custom noise.
	Dissolve Dissolve
	// PaletteSwap remaps source colors per sprite.
	PaletteSwap PaletteSwap
	// Custom, if set, runs last on each pixel of the sprite's frame buffer.
	// x and y are source-pixel coordinates relative to the unpadded frame's
	// top-left (outside [0, w) x [0, h) inside Outline padding); w and h are
	// the unpadded frame dimensions in source pixels. r, g, b, a are
	// premultiplied channels in [0, 1]; the returned premultiplied channels
	// are clamped to valid premultiplied [0, 1] values.
	Custom func(x, y, w, h int, r, g, b, a float32) (nr, ng, nb, na float32)
}

// Active reports whether e applies any visible effect. A nil or zero Effect
// returns false.
func (e *Effect) Active() bool {
	if e == nil {
		return false
	}
	return e.Flash.active() || e.Outline.active() || e.Dissolve.active() || e.PaletteSwap.active() || e.Custom != nil
}

// OutlinePadding returns the border padding in source pixels added on each side
// of the sprite when Outline is active, or 0 otherwise.
func (e *Effect) OutlinePadding() int {
	if e == nil {
		return 0
	}
	return e.Outline.padding()
}

// Apply renders frame of src through e into a new premultiplied *image.RGBA,
// returning the image and the outline padding in source pixels added on each
// side. It returns (nil, 0) if src is nil or frame has non-positive dimensions.
func (e *Effect) Apply(src image.Image, frame Frame) (out *image.RGBA, pad int) {
	if src == nil || frame.W <= 0 || frame.H <= 0 {
		return nil, 0
	}
	rgba := imageToPremulRGBA(src)
	if rgba == nil {
		return nil, 0
	}
	sx0 := int(math.Round(float64(frame.X)))
	sy0 := int(math.Round(float64(frame.Y)))
	srcW := int(math.Round(float64(frame.W)))
	srcH := int(math.Round(float64(frame.H)))
	if srcW <= 0 || srcH <= 0 {
		return nil, 0
	}
	fx := e
	if fx == nil {
		fx = &Effect{}
	}
	pad = fx.Outline.padding()
	dst := image.NewRGBA(image.Rect(0, 0, srcW+2*pad, srcH+2*pad))
	var alphaScratch []uint8
	applyEffectInto(dst, &alphaScratch, rgba, sx0, sy0, srcW, srcH, pad, 0, fx)
	return dst, pad
}

// effectSlot holds reusable per-sprite pixel scratch storage inside a Batch so
// changing effect parameters every frame performs zero steady-state allocations.
type effectSlot struct {
	pixels  *image.RGBA
	alpha   []uint8
	entries []PaletteEntry
	version uint64
	key     effectCacheKey
	valid   bool
}

type effectCacheKey struct {
	src            *image.RGBA
	srcVersion     uint64
	sx0, sy0       int
	srcW, srcH     int
	tint           core.Color
	flashColor     core.Color
	flashStrength  uint32
	outlineColor   core.Color
	outlineThick   uint32
	outlineThresh  uint8
	dissolveThresh uint32
	dissolveEdgeW  uint32
	dissolveEdgeC  core.Color
	dissolveSeed   uint32
	dissolveScale  uint32
	noiseKind      uint8
	noisePtr       any
	noiseBounds    image.Rectangle
	swapTolerance  uint8
	swapCount      int
}

func normF32Bits(v float64) uint32 {
	f := float32(v)
	if f == 0 {
		f = 0
	}
	return math.Float32bits(f)
}

func makeEffectCacheKey(src *image.RGBA, srcVersion uint64, sx0, sy0, srcW, srcH int, tint core.Color, fx *Effect) (effectCacheKey, bool) {
	if fx.Custom != nil {
		return effectCacheKey{}, false
	}
	var noiseKind uint8
	var noisePtr any
	var noiseBounds image.Rectangle
	if fx.Dissolve.Noise != nil {
		noiseBounds = fx.Dissolve.Noise.Bounds()
		switch n := fx.Dissolve.Noise.(type) {
		case *image.RGBA:
			noiseKind, noisePtr = 1, n
		case *image.NRGBA:
			noiseKind, noisePtr = 2, n
		case *image.Gray:
			noiseKind, noisePtr = 3, n
		case *image.Alpha:
			noiseKind, noisePtr = 4, n
		case *image.Paletted:
			noiseKind, noisePtr = 5, n
		default:
			// Custom image.Image implementations may not be comparable.
			return effectCacheKey{}, false
		}
	}
	if tint == untinted {
		tint = 0
	}
	return effectCacheKey{
		src:            src,
		srcVersion:     srcVersion,
		sx0:            sx0,
		sy0:            sy0,
		srcW:           srcW,
		srcH:           srcH,
		tint:           tint,
		flashColor:     fx.Flash.Color,
		flashStrength:  normF32Bits(fx.Flash.strength()),
		outlineColor:   fx.Outline.Color,
		outlineThick:   normF32Bits(fx.Outline.thickness()),
		outlineThresh:  fx.Outline.thresholdByte(),
		dissolveThresh: normF32Bits(fx.Dissolve.threshold()),
		dissolveEdgeW:  normF32Bits(fx.Dissolve.edgeWidth()),
		dissolveEdgeC:  fx.Dissolve.EdgeColor,
		dissolveSeed:   fx.Dissolve.Seed,
		dissolveScale:  normF32Bits(fx.Dissolve.scale()),
		noiseKind:      noiseKind,
		noisePtr:       noisePtr,
		noiseBounds:    noiseBounds,
		swapTolerance:  fx.PaletteSwap.Tolerance,
		swapCount:      len(fx.PaletteSwap.Entries),
	}, true
}

func (b *Batch) nextEffectSlot(idx *int) *effectSlot {
	i := *idx
	*idx = i + 1
	if i < len(b.fxPool) {
		return &b.fxPool[i]
	}
	b.fxPool = append(b.fxPool, effectSlot{})
	return &b.fxPool[i]
}

func (b *Batch) trimEffectPool(used int) {
	keep := max(effectPoolKeepFloor, used*2)
	if len(b.fxPool) > keep {
		clear(b.fxPool[keep:])
		b.fxPool = b.fxPool[:keep]
	}
}

// resolveAtlasPixels returns premultiplied RGBA pixels for atlas when available
// in memory (Image.Pixels or Image.Data decoded lazily via image.Decode). For
// URI-only atlases with neither Pixels nor Data it returns ok == false.
func resolveAtlasPixels(atlas *Atlas) (src *image.RGBA, version uint64, ok bool) {
	if atlas == nil {
		return nil, 0, false
	}
	img := atlas.Image
	if img.Pixels != nil {
		if img.Pixels.Rect.Empty() {
			return nil, 0, false
		}
		return img.Pixels, img.Version, true
	}
	if len(img.Data) == 0 {
		return nil, 0, false
	}
	if atlas.decoded != nil &&
		len(atlas.decodedSrc) == len(img.Data) &&
		&atlas.decodedSrc[0] == &img.Data[0] &&
		atlas.decodedVer == img.Version {
		return atlas.decoded, img.Version, true
	}
	decoded, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return nil, 0, false
	}
	rgba := imageToPremulRGBA(decoded)
	if rgba == nil {
		return nil, 0, false
	}
	atlas.decoded = rgba
	atlas.decodedSrc = img.Data
	atlas.decodedVer = img.Version
	return rgba, img.Version, true
}

// imageToPremulRGBA returns img as a premultiplied *image.RGBA, avoiding a
// copy when img is already one.
func imageToPremulRGBA(img image.Image) *image.RGBA {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	if b.Empty() {
		return nil
	}
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		dRow := (y - b.Min.Y) * dst.Stride
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			off := dRow + (x-b.Min.X)*4
			dst.Pix[off+0] = uint8(r16 >> 8)
			dst.Pix[off+1] = uint8(g16 >> 8)
			dst.Pix[off+2] = uint8(b16 >> 8)
			dst.Pix[off+3] = uint8(a16 >> 8)
		}
	}
	return dst
}

func atlasPixelScale(img core.ImageSource, src *image.RGBA) (pxPerPtX, pxPerPtY float64) {
	if img.Size.Width > 0 && img.Size.Height > 0 && src != nil && !src.Rect.Empty() {
		return float64(src.Rect.Dx()) / float64(img.Size.Width), float64(src.Rect.Dy()) / float64(img.Size.Height)
	}
	return 1, 1
}

// drawEffect renders s with its active Effect into d using a reused per-slot
// pixel buffer when atlas pixels are readable, or falls back to per-draw
// tinting on URI-only atlases.
func (s *Sprite) drawEffect(d *core.Drawing, atlas *Atlas, slot *effectSlot) {
	if s.hidden() {
		return
	}
	fx := s.Effect
	src, srcVer, ok := resolveAtlasPixels(atlas)
	if !ok {
		s.drawEffectFallback(d, atlas.Image)
		return
	}

	f := s.Frame
	pxPerPtX, pxPerPtY := atlasPixelScale(atlas.Image, src)
	sx0 := int(math.Round(float64(f.X) * pxPerPtX))
	sy0 := int(math.Round(float64(f.Y) * pxPerPtY))
	srcW := int(math.Round(float64(f.W) * pxPerPtX))
	srcH := int(math.Round(float64(f.H) * pxPerPtY))
	if srcW <= 0 || srcH <= 0 {
		return
	}

	pad := fx.Outline.padding()
	outW, outH := srcW+2*pad, srcH+2*pad

	var tint core.Color
	if s.tinted() {
		tint = s.Tint
	}
	key, cacheable := makeEffectCacheKey(src, srcVer, sx0, sy0, srcW, srcH, tint, fx)
	hit := cacheable && slot.valid && slot.key == key && slices.Equal(slot.entries, fx.PaletteSwap.Entries) &&
		slot.pixels != nil && slot.pixels.Rect.Dx() == outW && slot.pixels.Rect.Dy() == outH
	if !hit {
		wantRect := image.Rect(0, 0, outW, outH)
		needBytes := outW * outH * 4
		if slot.pixels == nil || cap(slot.pixels.Pix) < needBytes {
			slot.pixels = image.NewRGBA(wantRect)
		} else if slot.pixels.Rect != wantRect {
			slot.pixels.Pix = slot.pixels.Pix[:needBytes]
			slot.pixels.Stride = outW * 4
			slot.pixels.Rect = wantRect
		}
		applyEffectInto(slot.pixels, &slot.alpha, src, sx0, sy0, srcW, srcH, pad, tint, fx)
		slot.version = nextEffectVersion.Add(1)
		slot.key = key
		slot.entries = append(slot.entries[:0], fx.PaletteSwap.Entries...)
		slot.valid = cacheable
	}

	padPtX := float32(float64(pad) / pxPerPtX)
	padPtY := float32(float64(pad) / pxPerPtY)
	outPtW := f.W + 2*padPtX
	outPtH := f.H + 2*padPtY
	outSrc := core.ImageSource{
		Pixels:  slot.pixels,
		Version: slot.version,
		Size:    core.Size{Width: core.Points(outPtW), Height: core.Points(outPtH)},
	}

	masked := s.Mask != nil && s.Mask.state() == maskActive
	m := s.Transform()
	plain := m.B == 0 && m.C == 0 && m.A > 0 && m.D > 0
	blended := s.Blend != core.CompositeSourceOver
	saved := !plain || s.Alpha < 1 || blended || masked

	if saved {
		d.Save()
	}
	if masked {
		s.Mask.clip(d)
	}
	if blended {
		d.SetComposite(s.Blend)
	}
	if s.Alpha < 1 {
		d.SetAlpha(s.Alpha)
	}
	lx, ly, lw, lh := f.OffsetX-padPtX, f.OffsetY-padPtY, outPtW, outPtH
	if plain {
		d.DrawImageIn(outSrc, m.Tx+m.A*lx, m.Ty+m.D*ly, m.A*lw, m.D*lh)
	} else {
		d.Transform(m.A, m.B, m.C, m.D, m.Tx, m.Ty)
		d.DrawImageIn(outSrc, lx, ly, lw, lh)
	}
	if saved {
		d.Restore()
	}
}

// drawEffectFallback draws a sprite whose atlas has no CPU-readable pixels
// (URI-only). Full dissolve skips drawing; Flash degrades to a per-draw
// multiply tint (DrawTintedImageFrom) so dark silhouettes remain exact and
// neighbors are never affected.
func (s *Sprite) drawEffectFallback(d *core.Drawing, img core.ImageSource) {
	fx := s.Effect
	if fx.Dissolve.fullyDissolved() && fx.Custom == nil {
		return
	}
	if !fx.Flash.active() {
		s.draw(d, img)
		return
	}
	k := fx.Flash.strength()
	fr, fg, fb, _ := fx.Flash.Color.Components()
	// Lerp opaque white (identity tint) toward Flash.Color by Strength.
	tr := 1 + (fr-1)*k
	tg := 1 + (fg-1)*k
	tb := 1 + (fb-1)*k
	ta := 1.0
	if s.tinted() {
		sr, sg, sb, sa := s.Tint.Components()
		tr *= sr
		tg *= sg
		tb *= sb
		ta *= sa
	}
	r8 := uint32(toByte8(tr))
	g8 := uint32(toByte8(tg))
	b8 := uint32(toByte8(tb))
	a8 := uint32(toByte8(ta))
	effTint := core.Color(r8<<24 | g8<<16 | b8<<8 | a8)

	cp := *s
	cp.Effect = nil
	cp.Tint = effTint
	cp.draw(d, img)
}

func applyEffectInto(dst *image.RGBA, alphaScratch *[]uint8, src *image.RGBA, sx0, sy0, srcW, srcH, pad int, tint core.Color, fx *Effect) {
	outW, outH := srcW+2*pad, srcH+2*pad
	clear(dst.Pix)

	srcBounds := src.Bounds()
	hasSwap := fx.PaletteSwap.active()
	swapTol := int(fx.PaletteSwap.Tolerance)
	swapEntries := fx.PaletteSwap.Entries

	hasTint := tint != 0 && tint != untinted
	var tintR, tintG, tintB, tintA float64
	if hasTint {
		tintR, tintG, tintB, tintA = tint.Components()
	}

	hasDissolve := fx.Dissolve.active()
	dissolveFull := fx.Dissolve.fullyDissolved()
	dissolveThresh := fx.Dissolve.threshold()
	dissolveEdgeW := fx.Dissolve.edgeWidth()
	dissolveEdgeR, dissolveEdgeG, dissolveEdgeB, dissolveEdgeA := fx.Dissolve.EdgeColor.Components()
	dissolveScale := fx.Dissolve.scale()
	dissolveSeed := fx.Dissolve.Seed
	dissolveNoise := fx.Dissolve.Noise
	var noiseBounds image.Rectangle
	if dissolveNoise != nil {
		noiseBounds = dissolveNoise.Bounds()
	}

	hasFlash := fx.Flash.active()
	flashK := fx.Flash.strength()
	flashR, flashG, flashB, _ := fx.Flash.Color.Components()

	for y := range srcH {
		sy := srcBounds.Min.Y + sy0 + y
		inY := sy >= srcBounds.Min.Y && sy < srcBounds.Max.Y
		dRow := (y + pad) * dst.Stride
		for x := range srcW {
			sx := srcBounds.Min.X + sx0 + x
			if !inY || sx < srcBounds.Min.X || sx >= srcBounds.Max.X {
				continue
			}
			si := src.PixOffset(sx, sy)
			pa := src.Pix[si+3]
			if pa == 0 {
				continue
			}
			pr := src.Pix[si+0]
			pg := src.Pix[si+1]
			pb := src.Pix[si+2]

			// Stage 1: PaletteSwap (in straight 8-bit sRGB).
			if hasSwap {
				sr, sg, sb := unpremul8(pr, pg, pb, pa)
				for _, entry := range swapEntries {
					er := int(entry.From >> 24 & 0xff)
					eg := int(entry.From >> 16 & 0xff)
					eb := int(entry.From >> 8 & 0xff)
					if absInt(int(sr)-er) <= swapTol && absInt(int(sg)-eg) <= swapTol && absInt(int(sb)-eb) <= swapTol {
						tr := uint8(entry.To >> 24 & 0xff)
						tg := uint8(entry.To >> 16 & 0xff)
						tb := uint8(entry.To >> 8 & 0xff)
						ta := uint8(entry.To & 0xff)
						pa = uint8((uint16(pa)*uint16(ta) + 127) / 255)
						pr, pg, pb = premul8(tr, tg, tb, pa)
						break
					}
				}
				if pa == 0 {
					continue
				}
			}

			r := float64(pr) / 255
			g := float64(pg) / 255
			b := float64(pb) / 255
			a := float64(pa) / 255

			// Stage 2: Sprite.Tint (multiply premultiplied channels).
			if hasTint {
				r *= tintR * tintA
				g *= tintG * tintA
				b *= tintB * tintA
				a *= tintA
				if a <= 0 {
					continue
				}
			}

			// Stage 3: Dissolve.
			if hasDissolve {
				if dissolveFull {
					continue
				}
				n := sampleDissolveNoise(x, y, srcW, srcH, dissolveSeed, dissolveScale, dissolveNoise, noiseBounds)
				if n < dissolveThresh {
					continue
				}
				if dissolveEdgeW > 0 && n < dissolveThresh+dissolveEdgeW && dissolveEdgeA > 0 {
					r += (dissolveEdgeR*a - r) * dissolveEdgeA
					g += (dissolveEdgeG*a - g) * dissolveEdgeA
					b += (dissolveEdgeB*a - b) * dissolveEdgeA
				}
			}

			// Stage 4: Solid-color Flash / Silhouette.
			if hasFlash {
				r += (flashR*a - r) * flashK
				g += (flashG*a - g) * flashK
				b += (flashB*a - b) * flashK
			}

			di := dRow + (x+pad)*4
			dst.Pix[di+0] = toByte8(r)
			dst.Pix[di+1] = toByte8(g)
			dst.Pix[di+2] = toByte8(b)
			dst.Pix[di+3] = toByte8(a)
		}
	}

	// Stage 5: Alpha Outline.
	if fx.Outline.active() {
		applyOutline(dst, alphaScratch, outW, outH, fx.Outline)
	}

	// Stage 6: Custom per-pixel hook.
	if fx.Custom != nil {
		for oy := range outH {
			row := oy * dst.Stride
			for ox := range outW {
				i := row + ox*4
				r := float32(dst.Pix[i+0]) / 255
				g := float32(dst.Pix[i+1]) / 255
				b := float32(dst.Pix[i+2]) / 255
				a := float32(dst.Pix[i+3]) / 255
				nr, ng, nb, na := fx.Custom(ox-pad, oy-pad, srcW, srcH, r, g, b, a)
				ca := clampUnit(float64(na))
				dst.Pix[i+0] = toByte8(min(clampUnit(float64(nr)), ca))
				dst.Pix[i+1] = toByte8(min(clampUnit(float64(ng)), ca))
				dst.Pix[i+2] = toByte8(min(clampUnit(float64(nb)), ca))
				dst.Pix[i+3] = toByte8(ca)
			}
		}
	}
}

type outlineOffset struct {
	dx, dy int
}

func applyOutline(dst *image.RGBA, alphaScratch *[]uint8, w, h int, o Outline) {
	n := w * h
	if cap(*alphaScratch) < n {
		*alphaScratch = make([]uint8, n)
	}
	mask := (*alphaScratch)[:n]
	for y := range h {
		row := y * dst.Stride
		mRow := y * w
		for x := range w {
			mask[mRow+x] = dst.Pix[row+x*4+3]
		}
	}

	thick := o.thickness()
	radius := max(thick, 1.0)
	rCeil := int(math.Ceil(radius))
	rSq := radius*radius + 1e-6
	var offsetsBuf [64]outlineOffset
	offsets := offsetsBuf[:0]
	for dy := -rCeil; dy <= rCeil; dy++ {
		for dx := -rCeil; dx <= rCeil; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			if float64(dx*dx+dy*dy) <= rSq {
				offsets = append(offsets, outlineOffset{dx, dy})
			}
		}
	}

	thresh := o.thresholdByte()
	or, og, ob, oa := o.Color.Components()
	if thick < 1 {
		oa *= thick
	}
	opR, opG, opB, opA := or*oa, og*oa, ob*oa, oa
	opR8, opG8, opB8, opA8 := toByte8(opR), toByte8(opG), toByte8(opB), toByte8(opA)

	for y := range h {
		mRow := y * w
		dRow := y * dst.Stride
		for x := range w {
			sa := mask[mRow+x]
			if sa == 255 {
				continue
			}
			hit := false
			for _, off := range offsets {
				nx, ny := x+off.dx, y+off.dy
				if nx >= 0 && nx < w && ny >= 0 && ny < h && mask[ny*w+nx] >= thresh {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			di := dRow + x*4
			if sa == 0 {
				dst.Pix[di+0] = opR8
				dst.Pix[di+1] = opG8
				dst.Pix[di+2] = opB8
				dst.Pix[di+3] = opA8
				continue
			}
			// Composite translucent sprite pixel over the outline color.
			invA := 1 - float64(sa)/255
			sr := float64(dst.Pix[di+0]) / 255
			sg := float64(dst.Pix[di+1]) / 255
			sb := float64(dst.Pix[di+2]) / 255
			saF := float64(sa) / 255
			dst.Pix[di+0] = toByte8(sr + opR*invA)
			dst.Pix[di+1] = toByte8(sg + opG*invA)
			dst.Pix[di+2] = toByte8(sb + opB*invA)
			dst.Pix[di+3] = toByte8(saF + opA*invA)
		}
	}
}

func sampleDissolveNoise(x, y, w, h int, seed uint32, scale float64, noise image.Image, nb image.Rectangle) float64 {
	if noise != nil && !nb.Empty() {
		u := (float64(x) + 0.5) / float64(w)
		v := (float64(y) + 0.5) / float64(h)
		nx := nb.Min.X + int(math.Floor(u*float64(nb.Dx())))
		ny := nb.Min.Y + int(math.Floor(v*float64(nb.Dy())))
		nx = min(max(nx, nb.Min.X), nb.Max.X-1)
		ny = min(max(ny, nb.Min.Y), nb.Max.Y-1)
		r16, g16, b16, _ := noise.At(nx, ny).RGBA()
		return (0.2126*float64(r16) + 0.7152*float64(g16) + 0.0722*float64(b16)) / 65535.0
	}
	if scale <= 1 {
		return dissolveHash(x, y, seed)
	}
	gx := float64(x) / scale
	gy := float64(y) / scale
	x0 := int(math.Floor(gx))
	y0 := int(math.Floor(gy))
	fx := gx - float64(x0)
	fy := gy - float64(y0)
	ux := fx * fx * (3 - 2*fx)
	uy := fy * fy * (3 - 2*fy)
	n00 := dissolveHash(x0, y0, seed)
	n10 := dissolveHash(x0+1, y0, seed)
	n01 := dissolveHash(x0, y0+1, seed)
	n11 := dissolveHash(x0+1, y0+1, seed)
	top := n00 + (n10-n00)*ux
	bot := n01 + (n11-n01)*ux
	return top + (bot-top)*uy
}

// dissolveHash returns a deterministic value in [0, 1) for cell (x, y) and seed.
func dissolveHash(x, y int, seed uint32) float64 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + seed*2246822519 + 3266489917
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h&0x00ffffff) / float64(0x01000000)
}

func unpremul8(pr, pg, pb, pa uint8) (sr, sg, sb uint8) {
	switch pa {
	case 0:
		return 0, 0, 0
	case 255:
		return pr, pg, pb
	default:
		a := uint32(pa)
		half := a / 2
		return uint8(min(255, (uint32(pr)*255+half)/a)),
			uint8(min(255, (uint32(pg)*255+half)/a)),
			uint8(min(255, (uint32(pb)*255+half)/a))
	}
}

func premul8(sr, sg, sb, pa uint8) (pr, pg, pb uint8) {
	switch pa {
	case 0:
		return 0, 0, 0
	case 255:
		return sr, sg, sb
	default:
		a := uint16(pa)
		return uint8((uint16(sr)*a + 127) / 255),
			uint8((uint16(sg)*a + 127) / 255),
			uint8((uint16(sb)*a + 127) / 255)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampUnit(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

func toByte8(v float64) uint8 {
	return uint8(math.Round(clampUnit(v) * 255))
}

package text

import (
	"container/list"
	"image"
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// NSGo's core.Drawing has no per-draw fragment shader (core.Shader is a view of its own, not an op inside a canvas), so the distance field is resolved on the CPU: each glyph is thresholded once at the scale it is drawn at, cached, and drawn as an image.

const (
	// scaleSteps is how many resolved scales a doubling of the size spans: a glyph drawn between two of them is resolved at the nearer one and stretched by at most 2^(1/32), about 2%, which widens its one-pixel edge by as much.
	scaleSteps = 16
	// maxGlyphPixels caps a resolved glyph image's side, so text close to the camera stays bounded in memory; past it the glyph is stretched.
	maxGlyphPixels = 1024
	// defaultCacheBytes is the cache bound when SDFFont.CacheBytes is 0.
	defaultCacheBytes = 16 << 20
)

// glyphKey names one resolved glyph image: the glyph and its rectangle, the quantized scale, the colors it was resolved with and the font settings resolving reads, so changing any of them resolves the glyph afresh.
type glyphKey struct {
	r            rune
	page         int
	frame        [4]float32 // the glyph's rectangle in its page: x, y, w, h
	step         int32
	color        core.Color
	outlineColor core.Color
	outline      uint32 // math.Float32bits of the outline width in em
	field        FieldType
	size         uint32 // math.Float32bits of the font's Size
	rng, middle  uint32 // math.Float32bits of the font's DistanceRange and DistanceRangeMiddle
}

type glyphEntry struct {
	key   glyphKey
	img   core.ImageSource
	scale float32 // output pixels per atlas pixel the image was resolved at
	bytes int
}

// glyphCache keeps resolved glyph images, dropping the least recently used past its byte bound.
type glyphCache struct {
	entries map[glyphKey]*list.Element
	lru     list.List
	bytes   int
}

func (c *glyphCache) get(k glyphKey) (*glyphEntry, bool) {
	if c.entries == nil {
		return nil, false
	}
	el, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*glyphEntry), true
}

func (c *glyphCache) put(e *glyphEntry, limit int) {
	if c.entries == nil {
		c.entries = make(map[glyphKey]*list.Element)
		c.lru.Init()
	}
	c.entries[e.key] = c.lru.PushFront(e)
	c.bytes += e.bytes
	// The entry just added stays even when it alone is over the bound, so the glyph being drawn always has its image.
	for c.bytes > limit && c.lru.Len() > 1 {
		last := c.lru.Back()
		old := last.Value.(*glyphEntry)
		c.lru.Remove(last)
		delete(c.entries, old.key)
		c.bytes -= old.bytes
	}
}

// CachedGlyphs returns how many resolved glyph images the font keeps and their size in bytes.
func (f *SDFFont) CachedGlyphs() (count, bytes int) {
	if f == nil {
		return 0, 0
	}
	return len(f.cache.entries), f.cache.bytes
}

// ClearCache drops every resolved glyph image the font keeps.
func (f *SDFFont) ClearCache() {
	if f == nil {
		return
	}
	f.cache = glyphCache{}
}

func (f *SDFFont) cacheLimit() int {
	if f.CacheBytes > 0 {
		return f.CacheBytes
	}
	return defaultCacheBytes
}

// quantizeScale returns the step nearest scale (output pixels per atlas pixel) and the scale of that step, keeping the glyph's image within maxGlyphPixels.
func quantizeScale(scale float32, g Glyph) (int32, float32) {
	side := max(g.Frame.W, g.Frame.H)
	if side > 0 && scale*side > maxGlyphPixels {
		scale = maxGlyphPixels / side
	}
	step := int32(math.Round(math.Log2(float64(scale)) * scaleSteps))
	q := float32(math.Exp2(float64(step) / scaleSteps))
	for side > 0 && q*side > maxGlyphPixels {
		step--
		q = float32(math.Exp2(float64(step) / scaleSteps))
	}
	return step, q
}

// glyphImage returns g's image resolved near scale output pixels per atlas pixel, from the cache when it is there, and the scale it was resolved at.
func (f *SDFFont) glyphImage(g Glyph, scale float32, st sdfStyle) (*glyphEntry, bool) {
	if g.Frame.W <= 0 || g.Frame.H <= 0 || !(scale > 0) || math.IsInf(float64(scale), 0) {
		return nil, false
	}
	fp := f.pages[g.Page]
	if fp == nil {
		return nil, false
	}
	step, q := quantizeScale(scale, g)
	key := glyphKey{
		r: g.ID, page: g.Page, frame: [4]float32{g.Frame.X, g.Frame.Y, g.Frame.W, g.Frame.H},
		step: step, color: st.color, outlineColor: st.outlineColor, outline: math.Float32bits(st.outline),
		field: f.Field, size: math.Float32bits(f.Size), rng: math.Float32bits(f.DistanceRange), middle: math.Float32bits(f.DistanceRangeMiddle),
	}
	if e, ok := f.cache.get(key); ok {
		return e, true
	}
	img := f.resolve(fp, g, q, st)
	e := &glyphEntry{key: key, img: core.ImageSource{Pixels: img}, scale: q, bytes: len(img.Pix)}
	f.cache.put(e, f.cacheLimit())
	return e, true
}

// sdfStyle is what a glyph's image is resolved with.
type sdfStyle struct {
	color, outlineColor core.Color
	outline             float32 // em
}

// resolve thresholds g's distance field at scale output pixels per atlas pixel into a premultiplied image whose pixel (i, j) covers the glyph's atlas rectangle from (i/scale, j/scale) to ((i+1)/scale, (j+1)/scale).
//
// The field is sampled bilinearly (per channel, before the median, so a multi-channel field keeps its corners) at each output pixel's center, turned into a distance in output pixels and covered linearly across one pixel: the edge is one output pixel wide at every scale.
func (f *SDFFont) resolve(fp *fieldPage, g Glyph, scale float32, st sdfStyle) *image.RGBA {
	w := max(1, int(math.Ceil(float64(g.Frame.W*scale))))
	h := max(1, int(math.Ceil(float64(g.Frame.H*scale))))
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// toPixels turns a field value's offset from the outline's value, on, into a distance in output pixels.
	toPixels := f.DistanceRange * scale
	on := 0.5 - float64(f.DistanceRangeMiddle/f.DistanceRange)
	// The field only covers on*DistanceRange atlas pixels outside the outline, so an outline cannot be wider.
	outlinePx := min(st.outline*f.Size*scale, float32(on)*f.DistanceRange*scale-0.5)
	hasOutline := outlinePx > 0 && st.outlineColor&0xff != 0

	fr, fg, fb, fa := st.color.Components()
	or, og, ob, oa := st.outlineColor.Components()

	x0, y0 := g.Frame.X, g.Frame.Y
	minX, maxX := float64(x0), float64(x0+g.Frame.W-1)
	minY, maxY := float64(y0), float64(y0+g.Frame.H-1)
	if maxX < minX {
		maxX = minX
	}
	if maxY < minY {
		maxY = minY
	}
	inv := 1 / float64(scale)
	for j := range h {
		// Texel centers sit at half pixels: atlas point y samples texel row y - 0.5.
		ty := min(max(float64(y0)+(float64(j)+0.5)*inv-0.5, minY), maxY)
		for i := range w {
			tx := min(max(float64(x0)+(float64(i)+0.5)*inv-0.5, minX), maxX)
			edge, outer := fp.sample(tx, ty, f.Field)
			fill := clamp01((edge-on)*float64(toPixels) + 0.5)
			var cr, cg, cb, ca float64
			if hasOutline {
				ol := clamp01((outer-on)*float64(toPixels) + float64(outlinePx) + 0.5)
				// The fill draws over its outline, both premultiplied.
				oA := oa * ol * (1 - fill*fa)
				cr = fr*fa*fill + or*oA
				cg = fg*fa*fill + og*oA
				cb = fb*fa*fill + ob*oA
				ca = fa*fill + oA
			} else {
				cr, cg, cb, ca = fr*fa*fill, fg*fa*fill, fb*fa*fill, fa*fill
			}
			o := j*img.Stride + i*4
			img.Pix[o] = to8(cr)
			img.Pix[o+1] = to8(cg)
			img.Pix[o+2] = to8(cb)
			img.Pix[o+3] = to8(ca)
		}
	}
	return img
}

// sample returns the field at texel position (x, y), bilinearly interpolated: the value whose 0.5 is the outline, and the one outlines read (a multi-and-true field's true distance, else the same).
func (fp *fieldPage) sample(x, y float64, field FieldType) (edge, outer float64) {
	ix, iy := int(x), int(y)
	fx, fy := x-float64(ix), y-float64(iy)
	ix1, iy1 := min(ix+1, fp.w-1), min(iy+1, fp.h-1)
	ch := fp.channels
	i00, i10 := (iy*fp.w+ix)*ch, (iy*fp.w+ix1)*ch
	i01, i11 := (iy1*fp.w+ix)*ch, (iy1*fp.w+ix1)*ch
	w00, w10 := (1-fx)*(1-fy), fx*(1-fy)
	w01, w11 := (1-fx)*fy, fx*fy
	var v [4]float64
	for c := range ch {
		v[c] = (w00*float64(fp.pix[i00+c]) + w10*float64(fp.pix[i10+c]) + w01*float64(fp.pix[i01+c]) + w11*float64(fp.pix[i11+c])) / 255
	}
	switch field {
	case FieldMSDF:
		m := median(v[0], v[1], v[2])
		return m, m
	case FieldMTSDF:
		return median(v[0], v[1], v[2]), v[3]
	}
	return v[0], v[0]
}

func median(a, b, c float64) float64 {
	return max(min(a, b), min(max(a, b), c))
}

func clamp01(v float64) float64 {
	return min(max(v, 0), 1)
}

func to8(v float64) uint8 {
	return uint8(clamp01(v)*255 + 0.5)
}

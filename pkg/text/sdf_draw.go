package text

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// SDFOptions configures how an SDFFont lays out and draws text.
//
// Its zero value draws opaque white, left-aligned, at the font's own size, with no outline or wrapping.
type SDFOptions struct {
	// Size is the em size the text is drawn at, in points; 0 means the font's Size. DrawWorld ignores it and takes the size from WorldText.
	Size float32
	// MaxWidth wraps lines to fit within MaxWidth when > 0 and defines the alignment box [x, x+MaxWidth] for AlignCenter and AlignRight, as Options.MaxWidth does. It is in points for Draw and in world units for DrawWorld.
	MaxWidth float32
	// Align is the horizontal alignment of each line.
	Align Align
	// LineSpacing is extra vertical space between lines, in ems.
	LineSpacing float32
	// Color is the text's fill color; 0 means opaque white.
	Color core.Color
	// Alpha is the text's opacity from 0 to 1; 0 means 1, and a negative alpha draws nothing. Changing it, as a fading damage number does, reuses the glyphs already resolved.
	Alpha float32
	// Outline is the width of an outline around each glyph, in ems; 0 draws none. It cannot reach past the font's distance field, DistanceRange/2 - DistanceRangeMiddle atlas pixels outside the outline, and stops there.
	Outline float32
	// OutlineColor is the outline's color; 0 draws no outline.
	OutlineColor core.Color
	// PixelScale is the device pixels a point covers, so glyphs are resolved at the screen's density; 0 means 1.
	PixelScale float32
}

func (o SDFOptions) style() sdfStyle {
	st := sdfStyle{color: o.Color, outlineColor: o.OutlineColor}
	if st.color == 0 {
		st.color = 0xffffffff
	}
	if o.Outline > 0 && o.OutlineColor != 0 && finite32(o.Outline) {
		st.outline = o.Outline
	} else {
		st.outlineColor = 0
	}
	return st
}

func (o SDFOptions) pixelScale() float32 {
	if o.PixelScale > 0 && finite32(o.PixelScale) {
		return o.PixelScale
	}
	return 1
}

// emScale returns the points (or world units) per atlas pixel for an em of size, the font's Size when size is 0.
func (f *SDFFont) emScale(size float32) float32 {
	if size == 0 {
		return 1
	}
	return size / f.Size
}

// layoutOptions returns the Options that lay out text at scale points per atlas pixel: MaxWidth stays in the caller's units, and LineSpacing turns from ems into them.
func (f *SDFFont) layoutOptions(o SDFOptions, scale float32) Options {
	return Options{MaxWidth: o.MaxWidth, Align: o.Align, Scale: scale, LineSpacing: o.LineSpacing * f.Size * scale}
}

// Measure returns the bounding width and height, in points, of s laid out with opts.
func (f *SDFFont) Measure(s string, opts SDFOptions) core.Size {
	if f == nil || opts.Size < 0 {
		return core.Size{}
	}
	return f.layoutFont().Measure(s, f.layoutOptions(opts, f.emScale(opts.Size)))
}

// Layout computes the placements, in points, of all visible glyphs in s starting at (x, y), the top left of its first line.
func (f *SDFFont) Layout(s string, x, y float32, opts SDFOptions) []GlyphPlacement {
	if f == nil || opts.Size < 0 {
		return nil
	}
	return f.layoutFont().Layout(s, x, y, f.layoutOptions(opts, f.emScale(opts.Size)))
}

// Draw draws s at (x, y), the top left of its first line, into d, each glyph resolved from the distance field at the size it is drawn at, so it stays sharp at any Size and PixelScale.
//
// The current transform of d is not known to Draw: under a scaling transform, set PixelScale to the device pixels a point then covers.
func (f *SDFFont) Draw(d *core.Drawing, s string, x, y float32, opts SDFOptions) {
	if f == nil || d == nil || s == "" || opts.Size < 0 || opts.Alpha < 0 {
		return
	}
	scale := f.emScale(opts.Size)
	placements := f.layoutFont().Layout(s, x, y, f.layoutOptions(opts, scale))
	if len(placements) == 0 {
		return
	}
	st, ps := opts.style(), opts.pixelScale()
	f.beginAlpha(d, opts.Alpha)
	for _, p := range placements {
		f.drawGlyph(d, p.Glyph, math2d.Affine{A: scale, D: scale, Tx: p.X, Ty: p.Y}, ps, st)
	}
	f.endAlpha(d, opts.Alpha)
}

func (f *SDFFont) beginAlpha(d *core.Drawing, alpha float32) {
	if alpha > 0 && alpha < 1 {
		d.Save()
		d.SetAlpha(alpha)
	}
}

func (f *SDFFont) endAlpha(d *core.Drawing, alpha float32) {
	if alpha > 0 && alpha < 1 {
		d.Restore()
	}
}

// drawGlyph draws g with m mapping the glyph's atlas rectangle, from (0, 0) at its top left in atlas pixels, into d. The glyph is resolved at the scale m has on average (the square root of its determinant), times pixelScale.
func (f *SDFFont) drawGlyph(d *core.Drawing, g Glyph, m math2d.Affine, pixelScale float32, st sdfStyle) {
	det := m.A*m.D - m.B*m.C
	scale := float32(math.Sqrt(math.Abs(float64(det)))) * pixelScale
	e, ok := f.glyphImage(g, scale, st)
	if !ok {
		return
	}
	iw := float32(e.img.Pixels.Rect.Dx())
	ih := float32(e.img.Pixels.Rect.Dy())
	// The image's pixel (i, j) is the glyph's atlas point (i/q, j/q).
	inv := 1 / e.scale
	if m.B == 0 && m.C == 0 && m.A > 0 && m.D > 0 {
		d.DrawImageFrom(e.img, 0, 0, iw, ih, m.Tx, m.Ty, iw*inv*m.A, ih*inv*m.D)
		return
	}
	d.Save()
	d.Transform(m.A*inv, m.B*inv, m.C*inv, m.D*inv, m.Tx, m.Ty)
	d.DrawImageFrom(e.img, 0, 0, iw, ih, 0, 0, iw, ih)
	d.Restore()
}

package gametest

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// ImageDraw is one rectangle of an image that recorded operations put on the canvas: a DrawImage, DrawImageIn or DrawImageFrom, or one sprite of a DrawAtlas.
type ImageDraw struct {
	// Image is the image drawn from.
	Image core.ImageSource
	// SX, SY, SW and SH are the rectangle of the image drawn, in the image's points; SW and SH are 0 when the operation draws the whole image.
	SX, SY, SW, SH float32
	// Corners are where the drawn rectangle's top left, top right, bottom right and bottom left land on the canvas, after the drawing's transform. An image drawn at its natural size (DrawImage, DrawTintedImage) takes the size of its source rectangle, or of the whole image when the source sets Size or Pixels; with neither, its size is unknown and all four corners are its top left.
	Corners [4]core.Point
	// Alpha is the opacity it draws with: the drawing's alpha, times the sprite's own for a DrawAtlas sprite.
	Alpha float32
	// Composite is how it mixes with what is under it.
	Composite core.CompositeOp
	// Tinted reports whether the image's channels are multiplied by Tint, as DrawTintedImage records; a DrawAtlas sprite is never tinted.
	Tinted bool
	// Tint is the color a tinted image is multiplied by.
	Tint core.Color
	// Sampling is how the image's texels are sampled.
	Sampling core.ImageSampling
	// Clip is the clipping in effect, outermost first: the operations of each clipping path, each followed by the Clip that used it. It is empty when nothing clips the image.
	Clip []ClipStep
}

// ClipStep is one operation of a clipping path, or the Clip that ends one, with the drawing's transform when it was recorded.
type ClipStep struct {
	// Op is the path operation (MoveTo, LineTo, QuadTo, CubicTo, Arc, Rect, RoundRect, Ellipse or ClosePath) or the Clip.
	Op core.DrawOp
	// Transform is the drawing's transform then, as the arguments a, b, c, d, e, f of Drawing.Transform.
	Transform [6]float32
}

// ImageDraws replays the state that ops set (Save, Restore, Translate, Scale, Rotate, Transform, SetTransform, SetAlpha, SetComposite and clipping paths) and returns every image rectangle they draw, in order. It reads only images: fills, strokes, texts and meshes are left out.
//
// It lets a test check where sprites land and what they show however they were recorded: one DrawImageFrom per sprite, or many sprites in one DrawAtlas, as sprite.Batch draws them.
func ImageDraws(ops []core.DrawOp) []ImageDraw {
	type state struct {
		m         affine
		alpha     float64
		composite core.CompositeOp
		clip      []ClipStep
	}
	cur := state{m: identityAffine(), alpha: 1}
	var stack []state
	var path []ClipStep
	var out []ImageDraw
	add := func(op core.DrawOp, m affine, sx, sy, sw, sh, w, h, alpha float64) {
		corner := func(x, y float64) core.Point {
			px, py := m.apply(x, y)
			return core.Point{X: core.Points(px), Y: core.Points(py)}
		}
		out = append(out, ImageDraw{
			Image: *op.Image,
			SX:    float32(sx), SY: float32(sy), SW: float32(sw), SH: float32(sh),
			Corners:   [4]core.Point{corner(0, 0), corner(w, 0), corner(w, h), corner(0, h)},
			Alpha:     float32(alpha),
			Composite: cur.composite,
			Tinted:    op.Tinted,
			Tint:      tint(op),
			Sampling:  op.Sampling,
			Clip:      cur.clip,
		})
	}
	step := func(op core.DrawOp) ClipStep {
		m := cur.m
		return ClipStep{Op: op, Transform: [6]float32{float32(m.a), float32(m.b), float32(m.c), float32(m.d), float32(m.e), float32(m.f)}}
	}
	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				cur, stack = stack[n-1], stack[:n-1]
			}
		case core.DrawTranslate:
			cur.m = cur.m.mul(affine{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)})
		case core.DrawScale:
			cur.m = cur.m.mul(affine{a: float64(op.X), d: float64(op.Y)})
		case core.DrawRotate:
			sin, cos := math.Sincos(float64(op.A))
			cur.m = cur.m.mul(affine{a: cos, b: sin, c: -sin, d: cos})
		case core.DrawTransform:
			cur.m = cur.m.mul(affine{a: float64(op.X1), b: float64(op.Y1), c: float64(op.X2), d: float64(op.Y2), e: float64(op.X), f: float64(op.Y)})
		case core.DrawSetTransform:
			cur.m = affine{a: float64(op.X1), b: float64(op.Y1), c: float64(op.X2), d: float64(op.Y2), e: float64(op.X), f: float64(op.Y)}
		case core.DrawAlpha:
			cur.alpha = clamp01(float64(op.A))
		case core.DrawComposite:
			cur.composite = op.Composite
		case core.DrawBeginPath:
			path = path[:0]
		case core.DrawMoveTo, core.DrawLineTo, core.DrawQuadTo, core.DrawCubicTo, core.DrawArc, core.DrawRect, core.DrawRoundRect, core.DrawEllipse, core.DrawClosePath:
			path = append(path, step(op))
		case core.DrawClip:
			// A new slice, so that the clip saved on the stack and the clips of earlier draws keep theirs.
			cur.clip = append(append(slices.Clip(cur.clip), path...), step(op))
		case core.DrawImage:
			if op.Image == nil {
				continue
			}
			w, h := float64(op.W), float64(op.H)
			if w == 0 && h == 0 {
				// Drawn at its natural size, as RenderOps draws it.
				w, h = float64(op.X2), float64(op.Y2)
				if w == 0 && h == 0 {
					w, h = naturalSize(*op.Image)
				}
			}
			// The destination rectangle, in the drawing's coordinates, is the drawn rectangle's local space.
			add(op, cur.m.mul(affine{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)}),
				float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2), w, h, cur.alpha)
		case core.DrawAtlas:
			if op.Image == nil || op.Atlas == nil {
				continue
			}
			for _, s := range *op.Atlas {
				t := s.Transform
				m := cur.m.mul(affine{a: float64(t.SCos), b: float64(t.SSin), c: -float64(t.SSin), d: float64(t.SCos), e: float64(t.TX), f: float64(t.TY)})
				r := s.Rect
				add(op, m, float64(r.Origin.X), float64(r.Origin.Y), float64(r.Size.Width), float64(r.Size.Height),
					float64(r.Size.Width), float64(r.Size.Height), cur.alpha*float64(s.Alpha))
			}
		}
	}
	return out
}

// tint returns the color a DrawImage op multiplies its image by, or the zero color when it is untinted.
func tint(op core.DrawOp) core.Color {
	if !op.Tinted {
		return 0
	}
	return op.Color
}

// naturalSize returns the size an image drawn at its natural size takes: its Size when set, else its pixels' bounds, else 0 by 0, as it is not known before the image loads.
func naturalSize(img core.ImageSource) (w, h float64) {
	switch {
	case img.Size.Width != 0 || img.Size.Height != 0:
		return float64(img.Size.Width), float64(img.Size.Height)
	case img.Pixels != nil:
		b := img.Pixels.Bounds()
		return float64(b.Dx()), float64(b.Dy())
	}
	return 0, 0
}

// AssertSameImages fails t unless got and want draw the same image rectangles, in the same order, to the same places with the same alpha, composite, tint, sampling and clip, their numbers within 1e-3.
//
// Unlike comparing the operations themselves, it holds however the images were recorded, such as one DrawImageFrom per sprite against one DrawAtlas. It compares only images, as ImageDraws reads them: a test whose recordings also fill, stroke or write text checks those on its own.
func AssertSameImages(t testing.TB, got, want []core.DrawOp) {
	t.Helper()
	g, w := ImageDraws(got), ImageDraws(want)
	if len(g) != len(w) {
		t.Fatalf("%d image draws, want %d:\n%+v\nwant:\n%+v", len(g), len(w), g, w)
	}
	for i := range w {
		if a, b := g[i], w[i]; !sameImageDraw(a, b) {
			t.Fatalf("image draw %d is %+v, want %+v", i, a, b)
		}
	}
}

// near reports whether a and b are equal within 1e-3 of the larger of 1 and their size.
func near(a, b float32) bool {
	return math.Abs(float64(a-b)) <= 1e-3*max(1, math.Abs(float64(a)), math.Abs(float64(b)))
}

func sameImageDraw(a, b ImageDraw) bool {
	same := reflect.DeepEqual(a.Image, b.Image) && a.Composite == b.Composite && a.Tinted == b.Tinted && a.Tint == b.Tint && a.Sampling == b.Sampling &&
		near(a.SX, b.SX) && near(a.SY, b.SY) && near(a.SW, b.SW) && near(a.SH, b.SH) && near(a.Alpha, b.Alpha) && len(a.Clip) == len(b.Clip)
	for j := range a.Corners {
		same = same && near(float32(a.Corners[j].X), float32(b.Corners[j].X)) && near(float32(a.Corners[j].Y), float32(b.Corners[j].Y))
	}
	for j := 0; same && j < len(a.Clip); j++ {
		same = sameClipStep(a.Clip[j], b.Clip[j])
	}
	return same
}

func sameClipStep(a, b ClipStep) bool {
	x, y := a.Op, b.Op
	same := x.Kind == y.Kind && x.CCW == y.CCW && x.EvenOdd == y.EvenOdd
	for _, p := range [][2]float32{{x.X, y.X}, {x.Y, y.Y}, {x.X1, y.X1}, {x.Y1, y.Y1}, {x.X2, y.X2}, {x.Y2, y.Y2}, {x.W, y.W}, {x.H, y.H}, {x.R, y.R}, {x.A0, y.A0}, {x.A1, y.A1}, {x.RY, y.RY}} {
		same = same && near(p[0], p[1])
	}
	for j := range a.Transform {
		same = same && near(a.Transform[j], b.Transform[j])
	}
	return same
}

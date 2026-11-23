package gametest

import (
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// ImageDraw is one rectangle of an image that recorded operations put on the canvas: a DrawImage, DrawImageIn or DrawImageFrom, or one sprite of a DrawAtlas.
type ImageDraw struct {
	// Image is the image drawn from.
	Image core.ImageSource
	// SX, SY, SW and SH are the rectangle of the image drawn, in the image's points; SW and SH are 0 when the operation draws the whole image.
	SX, SY, SW, SH float32
	// Corners are where the drawn rectangle's top left, top right, bottom right and bottom left land on the canvas, after the drawing's transform.
	Corners [4]core.Point
	// Alpha is the opacity it draws with: the drawing's alpha, times the sprite's own for a DrawAtlas sprite.
	Alpha float32
	// Composite is how it mixes with what is under it.
	Composite core.CompositeOp
}

// ImageDraws replays the state that ops set (Save, Restore, Translate, Scale, Rotate, Transform, SetTransform, SetAlpha and SetComposite) and returns every image rectangle they draw, in order.
//
// It lets a test check where sprites land and what they show however they were recorded: one DrawImageFrom per sprite, or many sprites in one DrawAtlas, as sprite.Batch draws them.
func ImageDraws(ops []core.DrawOp) []ImageDraw {
	type state struct {
		m         affine
		alpha     float64
		composite core.CompositeOp
	}
	cur := state{m: identityAffine(), alpha: 1}
	var stack []state
	var out []ImageDraw
	add := func(img *core.ImageSource, m affine, sx, sy, sw, sh, w, h, alpha float64) {
		corner := func(x, y float64) core.Point {
			px, py := m.apply(x, y)
			return core.Point{X: core.Points(px), Y: core.Points(py)}
		}
		out = append(out, ImageDraw{
			Image: *img,
			SX:    float32(sx), SY: float32(sy), SW: float32(sw), SH: float32(sh),
			Corners:   [4]core.Point{corner(0, 0), corner(w, 0), corner(w, h), corner(0, h)},
			Alpha:     float32(alpha),
			Composite: cur.composite,
		})
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
		case core.DrawImage:
			if op.Image == nil {
				continue
			}
			// The destination rectangle, in the drawing's coordinates, is the drawn rectangle's local space.
			add(op.Image, cur.m.mul(affine{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)}),
				float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2), float64(op.W), float64(op.H), cur.alpha)
		case core.DrawAtlas:
			if op.Image == nil || op.Atlas == nil {
				continue
			}
			for _, s := range *op.Atlas {
				t := s.Transform
				m := cur.m.mul(affine{a: float64(t.SCos), b: float64(t.SSin), c: -float64(t.SSin), d: float64(t.SCos), e: float64(t.TX), f: float64(t.TY)})
				r := s.Rect
				add(op.Image, m, float64(r.Origin.X), float64(r.Origin.Y), float64(r.Size.Width), float64(r.Size.Height),
					float64(r.Size.Width), float64(r.Size.Height), cur.alpha*float64(s.Alpha))
			}
		}
	}
	return out
}

// AssertSameImages fails t unless got and want draw the same image rectangles, in the same order, to the same places with the same alpha and composite, their numbers within 1e-3.
//
// Unlike comparing the operations themselves, it holds however the images were recorded, such as one DrawImageFrom per sprite against one DrawAtlas.
func AssertSameImages(t testing.TB, got, want []core.DrawOp) {
	t.Helper()
	g, w := ImageDraws(got), ImageDraws(want)
	if len(g) != len(w) {
		t.Fatalf("%d image draws, want %d:\n%+v\nwant:\n%+v", len(g), len(w), g, w)
	}
	near := func(a, b float32) bool {
		return math.Abs(float64(a-b)) <= 1e-3*max(1, math.Abs(float64(a)), math.Abs(float64(b)))
	}
	for i := range w {
		a, b := g[i], w[i]
		same := reflect.DeepEqual(a.Image, b.Image) && a.Composite == b.Composite &&
			near(a.SX, b.SX) && near(a.SY, b.SY) && near(a.SW, b.SW) && near(a.SH, b.SH) && near(a.Alpha, b.Alpha)
		for j := range a.Corners {
			same = same && near(float32(a.Corners[j].X), float32(b.Corners[j].X)) && near(float32(a.Corners[j].Y), float32(b.Corners[j].Y))
		}
		if !same {
			t.Fatalf("image draw %d is %+v, want %+v", i, a, b)
		}
	}
}

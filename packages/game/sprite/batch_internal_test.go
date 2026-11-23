package sprite

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/nsgo/core"
)

// drawEach draws b as Batch.Draw did before it drew with DrawAtlas: one DrawImageFrom per sprite, in Z order, under the batch's mask.
func drawEach(b *Batch, d *core.Drawing) {
	draw := func(d *core.Drawing) {
		b.order = append(b.order[:0], b.Sprites...)
		slices.SortStableFunc(b.order, func(x, y *Sprite) int { return cmp.Compare(x.Z, y.Z) })
		for _, s := range b.order {
			s.draw(d, b.Atlas.Image)
		}
		clear(b.order)
	}
	if b.Mask != nil {
		DrawMasked(d, *b.Mask, draw)
		return
	}
	draw(d)
}

// stamp is one picture a recording puts on the canvas: a rectangle of an image, the transform taking the rectangle's points (from 0, 0 at its top left) to the canvas, and the alpha, blend, clip and sampling it draws with.
type stamp struct {
	image     core.ImageSource
	src       [4]float64
	m         [6]float64
	alpha     float64
	composite core.CompositeOp
	clip      string
	sampling  core.ImageSampling
}

// stamps plays ops as a canvas would and returns what they put on it, in order. It fails t on an operation a sprite batch does not record.
func stamps(t testing.TB, ops []core.DrawOp) []stamp {
	t.Helper()
	type state struct {
		m         [6]float64
		alpha     float64
		composite core.CompositeOp
		clip      string
	}
	mul := func(m, n [6]float64) [6]float64 {
		return [6]float64{
			m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
			m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
			m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
		}
	}
	f := func(v float32) float64 { return float64(v) }
	cur := state{m: [6]float64{1, 0, 0, 1, 0, 0}, alpha: 1}
	var stack []state
	var path string
	var out []stamp
	for i, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if len(stack) == 0 {
				t.Fatalf("op %d restores with nothing saved", i)
			}
			cur, stack = stack[len(stack)-1], stack[:len(stack)-1]
		case core.DrawTranslate:
			cur.m = mul(cur.m, [6]float64{1, 0, 0, 1, f(op.X), f(op.Y)})
		case core.DrawTransform:
			cur.m = mul(cur.m, [6]float64{f(op.X1), f(op.Y1), f(op.X2), f(op.Y2), f(op.X), f(op.Y)})
		case core.DrawAlpha:
			cur.alpha = f(op.A)
		case core.DrawComposite:
			cur.composite = op.Composite
		case core.DrawBeginPath:
			path = ""
		case core.DrawRect, core.DrawArc, core.DrawEllipse, core.DrawMoveTo, core.DrawLineTo, core.DrawClosePath:
			path += fmt.Sprintf("%+v@%v;", op, cur.m)
		case core.DrawClip:
			cur.clip += "[" + path + "]"
		case core.DrawImage:
			sw, sh := f(op.X2), f(op.Y2)
			out = append(out, stamp{
				image:     *op.Image,
				src:       [4]float64{f(op.X1), f(op.Y1), sw, sh},
				m:         mul(cur.m, [6]float64{f(op.W) / sw, 0, 0, f(op.H) / sh, f(op.X), f(op.Y)}),
				alpha:     cur.alpha,
				composite: cur.composite,
				clip:      cur.clip,
				sampling:  op.Sampling,
			})
		case core.DrawAtlas:
			for _, s := range *op.Atlas {
				x := s.Transform
				out = append(out, stamp{
					image:     *op.Image,
					src:       [4]float64{f(float32(s.Rect.Origin.X)), f(float32(s.Rect.Origin.Y)), f(float32(s.Rect.Size.Width)), f(float32(s.Rect.Size.Height))},
					m:         mul(cur.m, [6]float64{f(x.SCos), f(x.SSin), -f(x.SSin), f(x.SCos), f(x.TX), f(x.TY)}),
					alpha:     cur.alpha * f(s.Alpha),
					composite: cur.composite,
					clip:      cur.clip,
					sampling:  op.Sampling,
				})
			}
		default:
			t.Fatalf("op %d is %v, which a sprite batch does not record", i, op.Kind)
		}
	}
	if len(stack) != 0 {
		t.Fatalf("%d saves left unrestored", len(stack))
	}
	return out
}

// samePicture fails t unless got and want put the same stamps on the canvas, their numbers within float rounding.
func samePicture(t testing.TB, got, want []core.DrawOp) {
	t.Helper()
	g, w := stamps(t, got), stamps(t, want)
	if len(g) != len(w) {
		t.Fatalf("%d stamps, want %d:\n%+v\nwant:\n%+v", len(g), len(w), g, w)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-3*max(1, math.Abs(a), math.Abs(b)) }
	for i := range w {
		a, b := g[i], w[i]
		same := reflect.DeepEqual(a.image, b.image) && a.composite == b.composite && a.clip == b.clip && a.sampling == b.sampling && near(a.alpha, b.alpha)
		for j := range a.src {
			same = same && near(a.src[j], b.src[j])
		}
		for j := range a.m {
			same = same && near(a.m[j], b.m[j])
		}
		if !same {
			t.Fatalf("stamp %d is %+v, want %+v", i, a, b)
		}
	}
}

var testSheet = &Atlas{
	Image: core.ImageSource{URI: "asset:sheet.png"},
	Frames: map[string]Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
	},
}

// fuzzSprites builds a batch of testSheet from data, nine bytes a sprite, with every way a sprite can be placed, faded, blended and masked.
func fuzzSprites(data []byte) *Batch {
	frames := []Frame{testSheet.Frames["hero"], testSheet.Frames["coin"], {}}
	turns := []float32{0, math.Pi / 2, 0.3, math.Pi, -2.1}
	scales := [][2]float32{{1, 1}, {2, 2}, {0.5, 0.5}, {1.5, 0.5}, {-1, -1}, {-1, 1}, {0, 1}}
	alphas := []float32{1, 0.5, 0.25, 0, 1.5}
	blends := []core.CompositeOp{core.CompositeSourceOver, core.CompositeLighter}
	rect := RectMask(core.Bounds{Origin: core.Point{X: 2, Y: 3}, Size: core.Size{Width: 40, Height: 30}})
	empty := CircleMask(1, 1, 0)
	masks := []*Mask{nil, nil, &rect, &empty, {}}
	b := &Batch{Atlas: testSheet}
	for len(data) >= 9 {
		p := data[:9]
		data = data[9:]
		s := NewSprite(frames[int(p[0])%len(frames)])
		s.X = float32(int8(p[1])) / 2
		s.Y = float32(int8(p[2])) / 2
		s.OriginX, s.OriginY = float32(p[3]%17), float32(p[3]/16)
		s.Rotation = turns[int(p[4])%len(turns)]
		sc := scales[int(p[5])%len(scales)]
		s.ScaleX, s.ScaleY = sc[0], sc[1]
		s.FlipX, s.FlipY = p[6]&1 != 0, p[6]&2 != 0
		s.Alpha = alphas[int(p[6]>>2)%len(alphas)]
		s.Blend = blends[int(p[7]&1)]
		s.Mask = masks[int(p[7]>>1)%len(masks)]
		s.Z = int(p[8] % 4)
		b.Sprites = append(b.Sprites, &s)
	}
	if len(data) > 0 && data[0]&1 != 0 {
		b.Mask = &rect
	}
	return b
}

// record draws with draw into a drawing whose alpha the caller has set to alpha, as a batch inside a faded layer is drawn.
func record(alpha float32, draw func(d *core.Drawing)) []core.DrawOp {
	var d core.Drawing
	if alpha != 1 {
		d.SetAlpha(alpha)
	}
	draw(&d)
	return d.Ops
}

func TestABatchDrawsTheSamePictureAsOneDrawPerSprite(t *testing.T) {
	cases := map[string][]byte{
		"plain and moved":            {0, 10, 20, 0, 0, 0, 0, 0, 0, 1, 40, 20, 0, 0, 0, 0, 0, 0, 0, 80, 20, 0, 0, 0, 0, 0, 0},
		"turned and evenly scaled":   {0, 10, 20, 8, 1, 1, 0, 0, 0, 1, 40, 20, 8, 2, 2, 0, 0, 0, 0, 80, 20, 8, 3, 0, 0, 0, 0, 1, 90, 20, 8, 4, 4, 3, 0, 0},
		"mirrored and uneven breaks": {0, 10, 20, 0, 0, 0, 0, 0, 0, 1, 40, 20, 0, 0, 0, 1, 0, 0, 0, 80, 20, 0, 0, 3, 0, 0, 0, 1, 90, 20, 0, 0, 0, 0, 0, 0, 0, 9, 9, 0, 0, 0, 0, 0, 0},
		"fades split runs":           {0, 10, 20, 0, 0, 0, 4, 0, 0, 1, 40, 20, 0, 0, 0, 4, 0, 0, 0, 80, 20, 0, 0, 0, 0, 0, 0, 1, 90, 20, 0, 0, 0, 0, 0, 0, 0, 9, 9, 0, 0, 0, 8, 0, 0},
		"blends, masks and hidden":   {0, 10, 20, 0, 0, 0, 0, 1, 0, 1, 40, 20, 0, 0, 0, 0, 4, 0, 0, 80, 20, 0, 0, 0, 0, 6, 0, 1, 90, 20, 0, 0, 0, 0, 8, 0, 2, 9, 9, 0, 0, 0, 0, 0, 0, 0, 9, 9, 0, 0, 6, 0, 0, 0},
		"Z order and a batch mask":   {0, 10, 20, 0, 0, 0, 0, 0, 3, 1, 40, 20, 0, 0, 0, 0, 0, 1, 0, 80, 20, 0, 1, 1, 0, 0, 1, 1, 90, 20, 0, 0, 0, 0, 0, 0, 1},
	}
	for name, data := range cases {
		for _, alpha := range []float32{1, 0.5} {
			t.Run(fmt.Sprintf("%s/alpha=%v", name, alpha), func(t *testing.T) {
				b := fuzzSprites(data)
				got := record(alpha, b.Draw)
				samePicture(t, got, record(alpha, func(d *core.Drawing) { drawEach(b, d) }))
				// The batch's state is cleared for the next frame.
				if len(b.rects) != 0 || b.lone != nil || slices.ContainsFunc(b.order, func(s *Sprite) bool { return s != nil }) {
					t.Fatal("Draw kept sprites between frames")
				}
			})
		}
	}
}

func FuzzBatchDrawsTheSamePictureAsOneDrawPerSprite(f *testing.F) {
	f.Add([]byte{0, 10, 20, 0, 0, 0, 0, 0, 0, 1, 40, 20, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{0, 10, 20, 8, 2, 1, 4, 0, 1, 1, 40, 20, 8, 2, 2, 4, 0, 1, 0, 80, 20, 8, 3, 4, 0, 0, 0, 1})
	f.Add([]byte{1, 200, 20, 3, 1, 5, 3, 2, 2, 0, 4, 5, 9, 4, 6, 1, 7, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		b := fuzzSprites(data)
		alpha := float32(1)
		if len(data) > 0 && data[len(data)-1]&2 != 0 {
			alpha = 0.5
		}
		samePicture(t, record(alpha, b.Draw), record(alpha, func(d *core.Drawing) { drawEach(b, d) }))
	})
}

func TestABatchOfPlainSpritesIsOneDrawAtlas(t *testing.T) {
	b := &Batch{Atlas: testSheet}
	for i := range 1000 {
		s := NewSprite(testSheet.Frames[[]string{"hero", "coin"}[i%2]])
		s.X, s.Y = float32(i%40)*16, float32(i/40)*16
		if i%3 == 0 {
			s.OriginX, s.OriginY, s.Rotation, s.ScaleX, s.ScaleY = 8, 8, float32(i), 1.5, 1.5
		}
		b.Sprites = append(b.Sprites, &s)
	}
	var d core.Drawing
	b.Draw(&d)
	if len(d.Ops) != 1 || d.Ops[0].Kind != core.DrawAtlas || len(*d.Ops[0].Atlas) != 1000 {
		t.Fatalf("recorded %d ops, want one DrawAtlas of 1000 sprites: %+v", len(d.Ops), d.Ops)
	}
	samePicture(t, d.Ops, record(1, func(d *core.Drawing) { drawEach(b, d) }))
}

func TestAFadedRunDrawsItsAlphasInPlaceOfTheDrawings(t *testing.T) {
	coin := testSheet.Frames["coin"]
	b := &Batch{Atlas: testSheet}
	for i, a := range []float32{0.5, 0.25, 1, 1} {
		s := NewSprite(coin)
		s.X, s.Alpha = float32(i)*20, a
		b.Sprites = append(b.Sprites, &s)
	}
	var d core.Drawing
	b.Draw(&d)
	kinds := make([]core.DrawKind, len(d.Ops))
	for i, op := range d.Ops {
		kinds[i] = op.Kind
	}
	want := []core.DrawKind{core.DrawSave, core.DrawAlpha, core.DrawAtlas, core.DrawRestore, core.DrawAtlas}
	if !slices.Equal(kinds, want) || d.Ops[1].A != 1 {
		t.Fatalf("recorded %v, want %v with the alpha set to 1", kinds, want)
	}
	if got := *d.Ops[2].Atlas; got[0].Alpha != 0.5 || got[1].Alpha != 0.25 {
		t.Fatalf("faded run's alphas are %v, %v, want 0.5, 0.25", got[0].Alpha, got[1].Alpha)
	}
	if got := *d.Ops[2].Atlas; got[0].Rect != (core.Bounds{Origin: core.Point{X: 16}, Size: core.Size{Width: 8, Height: 10}}) || got[0].Transform != (core.RSXform{SCos: 1, TX: 4, TY: 3}) {
		t.Fatalf("first coin is %+v, want its trimmed rectangle at 4, 3", got[0])
	}
}

// pixelSheet is a 32 by 16 image of two 16 by 16 tiles, each with a gradient in every channel so that a misplaced or mirrored texel shows.
func pixelSheet() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := range 16 {
		for x := range 32 {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 16), B: uint8(255 - x*4), A: 255})
		}
	}
	return img
}

func TestABatchRendersTheSamePixelsAsOneDrawPerSprite(t *testing.T) {
	src := core.ImageSource{Pixels: pixelSheet(), Version: 1}
	atlas := &Atlas{Image: src, Frames: map[string]Frame{
		"a": {Name: "a", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"b": {Name: "b", X: 18, Y: 2, W: 12, H: 12, OffsetX: 2, OffsetY: 2, Width: 16, Height: 16},
	}}
	b := &Batch{Atlas: atlas}
	for i := range 24 {
		s := NewSprite(atlas.Frames[[]string{"a", "b"}[i%2]])
		s.X, s.Y = float32(8+(i%6)*20), float32(8+(i/6)*20)
		s.OriginX, s.OriginY = 8, 8
		s.Rotation = float32(i%4) * (math.Pi / 2)
		if i%5 == 0 {
			s.ScaleX, s.ScaleY = 0.5, 0.5
		}
		if i%7 == 0 {
			s.Alpha = 0.5
		}
		if i%9 == 0 {
			s.FlipX = true
		}
		b.Sprites = append(b.Sprites, &s)
	}
	got := gametest.RenderOps(128, 96, record(1, b.Draw))
	want := gametest.RenderOps(128, 96, record(1, func(d *core.Drawing) { drawEach(b, d) }))
	if _, n := gametest.DiffImage(got, want); n != 0 {
		t.Fatalf("%d pixels differ from one DrawImageFrom per sprite", n)
	}
	if blank := gametest.RenderOps(128, 96, nil); func() bool { _, n := gametest.DiffImage(got, blank); return n == 0 }() {
		t.Fatal("the batch drew no pixels")
	}
}

// benchSprites returns n sprites of testSheet spread over a 640-point square, a fifth of them turned and scaled about their centers, as moving game sprites are.
func benchSprites(n int) []*Sprite {
	sprites := make([]*Sprite, n)
	var seed [8]byte
	for i := range sprites {
		binary.LittleEndian.PutUint64(seed[:], uint64(i)*0x9e3779b97f4a7c15)
		s := NewSprite(testSheet.Frames[[]string{"hero", "coin"}[i%2]])
		s.X, s.Y = float32(seed[0])*2.5, float32(seed[1])*2.5
		if i%5 == 0 {
			s.OriginX, s.OriginY = 8, 8
			s.Rotation = float32(seed[2]) / 40
			s.ScaleX, s.ScaleY = 1.25, 1.25
		}
		sprites[i] = &s
	}
	return sprites
}

// BenchmarkBatchRecord compares recording a frame of n sprites with one DrawImageFrom each (before) and with DrawAtlas (after). sprites/60Hz is how many sprites the recording alone fits into one 16.7 ms frame at that rate.
func BenchmarkBatchRecord(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		for _, way := range []struct {
			name string
			draw func(*Batch, *core.Drawing)
		}{
			{"per-sprite", drawEach},
			{"atlas", (*Batch).Draw},
		} {
			b.Run(fmt.Sprintf("%s/n=%d", way.name, n), func(b *testing.B) {
				batch := &Batch{Atlas: testSheet, Sprites: benchSprites(n)}
				d := core.Drawing{Ops: make([]core.DrawOp, 0, 4*n)}
				way.draw(batch, &d)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					d.Ops = d.Ops[:0]
					way.draw(batch, &d)
				}
				b.StopTimer()
				perSprite := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(n)
				b.ReportMetric(1e9/60/perSprite, "sprites/60Hz")
			})
		}
	}
}

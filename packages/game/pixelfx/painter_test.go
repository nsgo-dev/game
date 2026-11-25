package pixelfx_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
)

func count(f *pixelfx.Frame) (color, light int) {
	for i := range f.Color {
		if f.Color[i] != 0 {
			color++
		}
		if f.Light[i] != 0 {
			light++
		}
	}
	return color, light
}

func TestDotDithersByAlpha(t *testing.T) {
	f := pixelfx.NewFrame(8, 8, 0, 0)
	p := f.Painter()
	for y := range 8 {
		for x := range 8 {
			p.Dot(float64(x), float64(y), pixelfx.White, 0.5)
		}
	}
	if c, _ := count(f); c != 32 {
		t.Fatalf("half alpha draws %d of 64 pixels, want 32 (the Bayer cells below one half)", c)
	}
	p.Dot(0, 0, pixelfx.Black, 0)
	if f.Color[0] == pixelfx.Black {
		t.Fatal("a dot at alpha 0 drew")
	}
}

func TestFlipMirrorsAboutTheOrigin(t *testing.T) {
	a, b := pixelfx.NewFrame(32, 16, 0, 0), pixelfx.NewFrame(32, 16, 0, 0)
	draw := func(p *pixelfx.Painter) {
		p.Circle(3, -2, 4, pixelfx.Patch, 1)
		p.Line(-10, 4, 9, -5, pixelfx.White, 1)
		p.Rect(5, 2, 4, 3, pixelfx.Ink, 1)
	}
	draw(a.Painter().Place(16, 8, false, 0))
	draw(b.Painter().Place(16, 8, true, 0))
	for y := range 16 {
		for x := range 32 {
			if a.Color[y*32+x] != b.Color[y*32+31-x] {
				t.Fatalf("pixel (%d, %d) is %#x, mirrored pixel is %#x", x, y, a.Color[y*32+x], b.Color[y*32+31-x])
			}
		}
	}
}

func TestLightAddsAndSaturates(t *testing.T) {
	f := pixelfx.NewFrame(4, 4, 0, 0)
	p := f.Painter()
	for range 3 {
		p.Light(1, 1, pixelfx.RGB(200, 100, 10), 1)
	}
	if got := f.Light[5]; got != pixelfx.RGB(255, 255, 30) {
		t.Fatalf("three lights add to %#x, want %#x", got, pixelfx.RGB(255, 255, 30))
	}
	p.LightK = 0.001
	p.Light(2, 2, pixelfx.White, 1)
	if f.Light[10] != 0 {
		t.Fatal("light below the 0.004 threshold was added")
	}
}

func TestCompositeOverTransparencyAndBackgrounds(t *testing.T) {
	f := pixelfx.NewFrame(3, 1, 0, 0)
	p := f.Painter()
	p.Px(0, 0, pixelfx.RGB(10, 20, 30))
	p.Blend(1, 0, pixelfx.RGB(200, 0, 0), 0.5)
	p.Light(2, 0, pixelfx.RGB(0, 100, 0), 1)
	got := pixelfx.Composite(f, nil, nil, 1)
	want := []byte{10, 20, 30, 255, 200, 0, 0, 127, 0, 255, 0, 100}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("over transparency: %v, want %v", got, want)
		}
	}
	got = pixelfx.Composite(f, nil, []uint32{pixelfx.RGB(0, 0, 100)}, 1)
	if got[4] != 100 || got[6] != 50 || got[7] != 255 || got[9] != 100 || got[10] != 100 {
		t.Fatalf("over a flat color: %v", got)
	}
}

func TestEdgeFadeEasesLightOut(t *testing.T) {
	f := pixelfx.NewFrame(20, 20, 0, 0)
	for i := range f.Light {
		f.Light[i] = pixelfx.RGB(200, 200, 200)
	}
	pixelfx.EdgeFade(f, 8)
	if f.Light[0]&255 >= f.Light[3*20+3]&255 || f.Light[10*20+10] != pixelfx.RGB(200, 200, 200) {
		t.Fatalf("edge %#x, near edge %#x, middle %#x", f.Light[0], f.Light[63], f.Light[210])
	}
}

func TestTextAndSprite(t *testing.T) {
	f := pixelfx.NewFrame(40, 12, 0, 0)
	p := f.Painter()
	p.Text("HI", 20, 2, pixelfx.White, 1, pixelfx.TextOpts{Align: pixelfx.AlignCenter, Outline: pixelfx.Ink})
	if w, h := p.Measure("HI", pixelfx.TextOpts{}); w != 7 || h != 5 {
		t.Fatalf("HI measures %vx%v, want 7x5", w, h)
	}
	c, _ := count(f)
	if c == 0 {
		t.Fatal("text drew nothing")
	}
	g := pixelfx.NewFrame(12, 19, 0, 0)
	pixelfx.DrawActor(g.Painter().Place(6, 19, false, 0), pixelfx.StockActor(1), 1)
	if c, _ := count(g); c == 0 || g.Color[0] != 0 {
		t.Fatalf("the stock actor drew %d pixels and %#x in the corner", c, g.Color[0])
	}
}

func TestPainterClipped(t *testing.T) {
	f := pixelfx.NewFrame(16, 16, 0, 0)
	p := f.Painter().Place(4, 4, false, 0).Clipped(0, 0, 8, 8)
	p.Rect(-4, -4, 16, 16, pixelfx.White, 1)
	p.Glow(0, 0, 6, pixelfx.Patch, 1)
	for y := range 16 {
		for x := range 16 {
			inside := x >= 4 && x < 12 && y >= 4 && y < 12
			if (f.Color[y*16+x] != 0) != inside {
				t.Fatalf("color at (%d, %d): %#x, want inside=%v", x, y, f.Color[y*16+x], inside)
			}
			if !inside && f.Light[y*16+x] != 0 {
				t.Fatalf("light leaked outside clip at (%d, %d): %#x", x, y, f.Light[y*16+x])
			}
		}
	}
}

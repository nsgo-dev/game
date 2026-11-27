package pixelfx_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/nsgo/core"
)

func TestImagePremultipliesAndVersionsFrames(t *testing.T) {
	f := pixelfx.NewFrame(2, 1, 0, 0)
	p := f.Painter()
	p.Px(0, 0, pixelfx.RGB(200, 100, 50))
	p.Blend(1, 0, pixelfx.RGB(200, 100, 50), 0.5)
	var im pixelfx.Image
	if im.Source().Pixels != nil {
		t.Fatal("an empty Image has pixels")
	}
	src := im.Set(f)
	if src.Version != 1 || src.Size.Width != 2 || src.Size.Height != 1 {
		t.Fatalf("source %+v", src)
	}
	if got := src.Pixels.Pix; got[0] != 200 || got[3] != 255 || got[4] != 100 || got[5] != 50 || got[7] != 127 {
		t.Fatalf("pixels %v, want the half-alpha pixel premultiplied", got)
	}
	if im.Set(f).Version != 2 {
		t.Fatal("a second Set keeps the version")
	}
}

func TestViewScalesWithoutSmoothing(t *testing.T) {
	f := pixelfx.NewFrame(2, 2, 0, 0)
	p := f.Painter()
	p.Px(0, 0, pixelfx.White)
	p.Px(1, 1, pixelfx.RGB(255, 0, 0))
	var im pixelfx.Image
	src := im.Set(f)
	v := pixelfx.View{Source: im.Source, Scale: 4}
	canvas := v.Render(nil).(core.Canvas)
	var d core.Drawing
	canvas.Draw(&d, core.Size{Width: 8, Height: 8})
	draws := gametest.ImageDraws(d.Ops)
	if len(draws) != 1 || draws[0].Sampling != core.ImageSamplingNearest || draws[0].Image.Pixels != src.Pixels {
		t.Fatalf("draws %+v", draws)
	}
	img := gametest.RenderOps(8, 8, d.Ops)
	if c := img.RGBAAt(3, 3); c.R != 255 || c.G != 255 || c.A != 255 {
		t.Fatalf("pixel (3, 3) is %v, want the white pixel scaled up to fill it", c)
	}
	if c := img.RGBAAt(4, 4); c.R != 255 || c.G != 0 {
		t.Fatalf("pixel (4, 4) is %v, want red with no blend into white", c)
	}
	if c := img.RGBAAt(5, 2); c.A != 0 {
		t.Fatalf("pixel (5, 2) is %v, want empty", c)
	}
}

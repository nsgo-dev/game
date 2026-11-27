package tilemap_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// sameOps fails t unless got and want are the same drawing operations within float32 rounding.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j]) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func makeUniformMap(w, h int, ts *tilemap.Tileset) *tilemap.Tilemap {
	m := tilemap.New(w, h, 16, 16, ts)
	data := make([]uint32, w*h)
	for row := range h {
		for col := range w {
			// Deterministic tile pattern depending only on (col, row), identical in the shared 20x20 region.
			data[row*w+col] = uint32((col+row)%4) + 1
		}
	}
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", w, h, data)}
	return m
}

func TestLargeTilemapDrawsOnlyVisibleTiles(t *testing.T) {
	img := core.ImageSource{URI: "asset:tiles.png"}
	ts := tilemap.NewGridTileset(1, img, 16, 16, 4, 16)

	huge := makeUniformMap(1000, 1000, ts)
	small := makeUniformMap(20, 20, ts)

	// Exact grid-aligned camera: [32, 160) x [16, 112) = 8 cols x 6 rows = 48 visible tiles.
	camAligned := &tilemap.Camera{X: 96, Y: 64, Zoom: 1, Width: 128, Height: 96}
	// Fractional & rotated camera well inside [0, 320) x [0, 320).
	camRotated := &tilemap.Camera{X: 160, Y: 160, Zoom: 1.5, Rotation: 0.4, Width: 120, Height: 90}
	// Camera completely outside the map.
	camOutside := &tilemap.Camera{X: -500, Y: -500, Zoom: 1, Width: 128, Height: 96}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "huge-aligned", Style: core.Style{Width: core.Pt(128), Height: core.Pt(96)}, Draw: func(d *core.Drawing, _ core.Size) {
				camAligned.Apply(d)
				huge.Draw(d, camAligned)
			}},
			core.Canvas{TestID: "small-aligned", Style: core.Style{Width: core.Pt(128), Height: core.Pt(96)}, Draw: func(d *core.Drawing, _ core.Size) {
				camAligned.Apply(d)
				small.Draw(d, camAligned)
			}},
			core.Canvas{TestID: "huge-rotated", Style: core.Style{Width: core.Pt(120), Height: core.Pt(90)}, Draw: func(d *core.Drawing, _ core.Size) {
				camRotated.Apply(d)
				huge.Draw(d, camRotated)
			}},
			core.Canvas{TestID: "small-rotated", Style: core.Style{Width: core.Pt(120), Height: core.Pt(90)}, Draw: func(d *core.Drawing, _ core.Size) {
				camRotated.Apply(d)
				small.Draw(d, camRotated)
			}},
			core.Canvas{TestID: "huge-outside", Style: core.Style{Width: core.Pt(128), Height: core.Pt(96)}, Draw: func(d *core.Drawing, _ core.Size) {
				huge.Draw(d, camOutside)
			}},
		}}
	})

	hugeAligned := app.Find("huge-aligned").Ops
	smallAligned := app.Find("small-aligned").Ops
	if len(hugeAligned) != len(smallAligned) {
		t.Fatalf("1000x1000 drew %d ops, 20x20 drew %d ops; want identical", len(hugeAligned), len(smallAligned))
	}
	sameOps(t, hugeAligned, smallAligned)
	// 2 Translate ops from camAligned.Apply + 48 visible tile DrawImageFrom ops = 50 ops.
	if len(hugeAligned) != 2+48 {
		t.Fatalf("got %d ops for 8x6 visible tiles + camera, want 50", len(hugeAligned))
	}

	hugeRot := app.Find("huge-rotated").Ops
	smallRot := app.Find("small-rotated").Ops
	if len(hugeRot) != len(smallRot) {
		t.Fatalf("rotated 1000x1000 drew %d ops, 20x20 drew %d ops; want identical", len(hugeRot), len(smallRot))
	}
	sameOps(t, hugeRot, smallRot)

	if outside := app.Find("huge-outside").Ops; len(outside) != 0 {
		t.Fatalf("outside camera drew %d ops, want 0", len(outside))
	}
}

func TestAtlasTilesetLayersAndFlips(t *testing.T) {
	img := core.ImageSource{URI: "asset:atlas.png"}
	atlas := &sprite.Atlas{
		Image: img,
		Frames: map[string]sprite.Frame{
			"brick": {Name: "brick", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"gem":   {Name: "gem", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
		},
	}
	ts := tilemap.NewAtlasTileset(1, atlas, "brick", "gem")
	m := tilemap.New(3, 1, 16, 16, ts)
	// Row 0: untrimmed brick, trimmed gem, horizontally-flipped trimmed gem.
	m.Layers = []tilemap.TileLayer{
		tilemap.NewTileLayer("main", 3, 1, []uint32{1, 2, tilemap.FlipHorizontal | 2}),
		{Name: "hidden", Width: 3, Height: 1, Data: []uint32{1, 1, 1}, Opacity: 1, Visible: false},
		{Name: "faint", Width: 3, Height: 1, Data: []uint32{0, 1, 0}, OffsetX: 2, OffsetY: 3, Opacity: 0.5, Visible: true},
	}

	if wb := m.WorldBounds(); wb != (core.Bounds{Size: core.Size{Width: 48, Height: 16}}) {
		t.Fatalf("WorldBounds = %+v, want 48x16", wb)
	}
	if m.Layer("main") == nil || m.Layer("missing") != nil {
		t.Fatal("Layer lookup mismatch")
	}
	if m.TileAt(0, 1, 0) != 2 || m.TileAt(0, -1, 0) != 0 || m.TileAt(9, 0, 0) != 0 {
		t.Fatal("TileAt mismatch")
	}
	m.SetTile(0, 99, 99, 1) // out-of-bounds no-op

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "got", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				m.Draw(d, nil)
			}},
			core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				// main layer
				d.DrawImageFrom(img, 0, 0, 16, 16, 0, 0, 16, 16)
				d.DrawImageFrom(img, 16, 0, 8, 10, 20, 3, 8, 10)
				// FlipHorizontal at col=2 (dstX=32, dstY=0, tw=16, th=16): a=-1, d=1, e=48, f=0
				d.Save().Transform(-1, 0, 0, 1, 48, 0).DrawImageFrom(img, 16, 0, 8, 10, 4, 3, 8, 10).Restore()
				// faint layer (opacity 0.5, offset 2, 3, col 1 -> 18, 3)
				d.Save().SetAlpha(0.5).DrawImageFrom(img, 0, 0, 16, 16, 18, 3, 16, 16).Restore()
			}},
		}}
	})
	sameOps(t, app.Find("got").Ops, app.Find("want").Ops)
}

func TestAnimatedTilesAdvanceOnGameLoopAndDrawActiveFrame(t *testing.T) {
	img := core.ImageSource{URI: "asset:water.png"}
	ts := tilemap.NewGridTileset(1, img, 16, 16, 4, 8)
	ts.Animations = map[uint32]*tilemap.TileAnimation{
		0: {Frames: []tilemap.TileAnimFrame{
			{TileID: 0, Duration: 100 * time.Millisecond},
			{TileID: 1, Duration: 200 * time.Millisecond},
			{TileID: 2, Duration: 150 * time.Millisecond},
		}},
	}
	m := tilemap.New(1, 1, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("water", 1, 1, []uint32{1})}

	var tick *core.Signal[int]
	var wantFrame *core.Signal[int]
	l := &game.Loop{
		Rate: 100, // 10ms fixed steps
		Update: func(dt time.Duration) {
			m.Update(dt)
		},
		Draw: func(float64) {
			tick.Set(tick.Get() + 1)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		wantFrame = core.NewSignal(cx, 0)
		l.Run(cx)
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "map", Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				m.Draw(d, nil)
			}},
			core.Canvas{TestID: "expected", Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				f := wantFrame.Get()
				d.DrawImageFrom(img, float32(f*16), 0, 16, 16, 0, 0, 16, 16)
			}},
		}}
	})

	checkFrame := func(label string, wantIdx int, wantTile uint32) {
		t.Helper()
		anim := ts.Animations[0]
		if anim.Index() != wantIdx || anim.CurrentTileID() != wantTile {
			t.Fatalf("%s: animation index=%d tile=%d, want index=%d tile=%d", label, anim.Index(), anim.CurrentTileID(), wantIdx, wantTile)
		}
		app.Post(func() { wantFrame.Set(int(wantTile)) })
		app.Settle()
		sameOps(t, app.Find("map").Ops, app.Find("expected").Ops)
	}

	// t = 0ms -> frame 0 (tile 0)
	checkFrame("t=0ms", 0, 0)

	// t = 50ms -> still frame 0 (tile 0)
	app.Advance(50 * time.Millisecond)
	checkFrame("t=50ms", 0, 0)

	// t = 100ms -> frame 1 (tile 1, lasts 200ms until 300ms)
	app.Advance(50 * time.Millisecond)
	checkFrame("t=100ms", 1, 1)

	// t = 250ms -> still frame 1
	app.Advance(150 * time.Millisecond)
	checkFrame("t=250ms", 1, 1)

	// t = 300ms -> frame 2 (tile 2, lasts 150ms until 450ms)
	app.Advance(50 * time.Millisecond)
	checkFrame("t=300ms", 2, 2)

	// t = 450ms -> wraps to frame 0 (tile 0)
	app.Advance(150 * time.Millisecond)
	checkFrame("t=450ms", 0, 0)

	// t = 1000ms (total 1s = 2 full 450ms cycles + 100ms) -> frame 1 (tile 1)
	app.Advance(550 * time.Millisecond)
	checkFrame("t=1000ms", 1, 1)
}

func TestTileAnimationEdgeCases(t *testing.T) {
	// Empty animation stays at 0.
	var empty tilemap.TileAnimation
	empty.Update(100 * time.Millisecond)
	if empty.CurrentTileID() != 0 || empty.Index() != 0 {
		t.Fatalf("empty animation returned tile=%d idx=%d", empty.CurrentTileID(), empty.Index())
	}

	// All zero-duration frames stay put without looping forever.
	zero := tilemap.TileAnimation{Frames: []tilemap.TileAnimFrame{{TileID: 5, Duration: 0}, {TileID: 6, Duration: 0}}}
	zero.Update(100 * time.Millisecond)
	if zero.Index() != 0 || zero.CurrentTileID() != 5 {
		t.Fatalf("zero-duration animation moved to idx=%d tile=%d", zero.Index(), zero.CurrentTileID())
	}

	// Long step skips whole rounds cleanly and Reset restores frame 0.
	a := tilemap.TileAnimation{Frames: []tilemap.TileAnimFrame{
		{TileID: 10, Duration: 30 * time.Millisecond},
		{TileID: 20, Duration: 20 * time.Millisecond},
		{TileID: 30, Duration: 50 * time.Millisecond},
	}}
	a.Update(-10 * time.Millisecond)
	if a.Index() != 0 {
		t.Fatalf("negative dt moved index to %d", a.Index())
	}
	// 10 rounds (1000ms) + 40ms -> frame 1 (30ms..50ms).
	a.Update(1040 * time.Millisecond)
	if a.Index() != 1 || a.CurrentTileID() != 20 {
		t.Fatalf("after 1040ms: idx=%d tile=%d, want 1 and 20", a.Index(), a.CurrentTileID())
	}
	a.Reset()
	if a.Index() != 0 || a.CurrentTileID() != 10 {
		t.Fatalf("after Reset: idx=%d tile=%d, want 0 and 10", a.Index(), a.CurrentTileID())
	}
}

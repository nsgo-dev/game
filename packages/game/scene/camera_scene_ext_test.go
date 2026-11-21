package scene_test

import (
	"cmp"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestParallaxLayersFactorsOffsetsAndDefaults(t *testing.T) {
	cam := &tilemap.Camera{
		X:         100,
		Y:         80,
		Zoom:      2,
		Width:     400,
		Height:    200,
		ShakeSeed: 11,
	}
	cam.Shake(10, 200*time.Millisecond)
	shake := cam.ShakeOffset()
	cx := cam.X + float32(shake.X)
	cy := cam.Y + float32(shake.Y)

	mkSprite := func(x, y float32) *sprite.Batch {
		sp := sprite.NewSprite(testAtlas.Frames["hero"])
		sp.X, sp.Y = x, y
		return &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sp}}
	}

	b0 := mkSprite(10, 10)
	bHalf := mkSprite(20, 20)
	bOne := mkSprite(30, 30)
	bDefault := mkSprite(35, 35)
	bFore := mkSprite(40, 40)

	// 0.0 factor via NewParallaxLayer (fixed against camera translation).
	l0 := scene.NewParallaxLayer("sky", 0, 0, 0)
	l0.AddBatch(b0)

	// 0.5 X, 0.25 Y factor with static OffsetX/OffsetY.
	lHalf := scene.NewLayer("hills", scene.SpaceWorld, 1)
	lHalf.ParallaxX = 0.5
	lHalf.ParallaxY = 0.25
	lHalf.OffsetX = 12
	lHalf.OffsetY = -8
	lHalf.AddBatch(bHalf)

	// 1.0 factor via NewLayer default.
	lOne := scene.NewLayer("world", scene.SpaceWorld, 2)
	lOne.AddBatch(bOne)

	// Struct literal with zero ParallaxX/ParallaxY defaults to 1.0 (backwards-compatible).
	lDefault := &scene.Layer{
		Name:    "literal-world",
		Space:   scene.SpaceWorld,
		Visible: true,
		Order:   3,
	}
	lDefault.AddBatch(bDefault)

	// 1.5 foreground factor via SetParallax on struct literal.
	lFore := &scene.Layer{
		Name:    "foreground",
		Space:   scene.SpaceWorld,
		Visible: true,
		Order:   4,
	}
	lFore.SetParallax(1.5, 1.5)
	lFore.AddBatch(bFore)

	sc := scene.NewScene(cam)
	sc.AddLayer(l0)
	sc.AddLayer(lHalf)
	sc.AddLayer(lOne)
	sc.AddLayer(lDefault)
	sc.AddLayer(lFore)

	app := nsgotest.Start(t, func(cxScope *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "scene",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(200)},
				Draw:   func(d *core.Drawing, _ core.Size) { sc.Draw(d) },
			},
			core.Canvas{
				TestID: "hand",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(200)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// 0.0 parallax: center + scale, no camera position translate.
					d.Save()
					d.Translate(200, 100)
					d.Scale(2, 2)
					b0.Draw(d)
					d.Restore()

					// 0.5 X, 0.25 Y + Offset(12, -8): translate -(cx*0.5 - 12), -(cy*0.25 - (-8)).
					d.Save()
					d.Translate(200, 100)
					d.Scale(2, 2)
					d.Translate(-(cx*0.5 - 12), -(cy*0.25 + 8))
					bHalf.Draw(d)
					d.Restore()

					// 1.0 parallax (NewLayer default).
					d.Save()
					d.Translate(200, 100)
					d.Scale(2, 2)
					d.Translate(-cx, -cy)
					bOne.Draw(d)
					d.Restore()

					// 1.0 parallax (struct literal default).
					d.Save()
					d.Translate(200, 100)
					d.Scale(2, 2)
					d.Translate(-cx, -cy)
					bDefault.Draw(d)
					d.Restore()

					// 1.5 parallax (foreground).
					d.Save()
					d.Translate(200, 100)
					d.Scale(2, 2)
					d.Translate(-(cx * 1.5), -(cy * 1.5))
					bFore.Draw(d)
					d.Restore()
				},
			},
		}}
	})

	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)

	// Setting ParallaxX/ParallaxY to 0 on a NewLayer pins it to 0.0 (does not snap back to 1.0).
	pinned := scene.NewLayer("pinned", scene.SpaceWorld, 0)
	pinned.ParallaxX = 0
	pinned.ParallaxY = 0
	pinned.AddBatch(b0)
	scPinned := scene.NewScene(&tilemap.Camera{X: 250, Y: -120, Zoom: 1, Width: 200, Height: 100})
	scPinned.AddLayer(pinned)

	app2 := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "got",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { scPinned.Draw(d) },
			},
			core.Canvas{
				TestID: "want",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save()
					d.Translate(100, 50)
					b0.Draw(d)
					d.Restore()
				},
			},
		}}
	})
	sameOps(t, app2.Find("got").Ops, app2.Find("want").Ops)
}

func TestParallaxHitTestMatchesTransform(t *testing.T) {
	cam := &tilemap.Camera{
		X:      200,
		Y:      100,
		Zoom:   2,
		Width:  400,
		Height: 200,
	}

	// Sprite sits at layer (100, 25), size 16x16 centered at origin (8, 8).
	// On a layer with ParallaxX=0.5, ParallaxY=0.25, OffsetX=10, OffsetY=5:
	// effective cam = (200*0.5 - 10, 100*0.25 - 5) = (90, 20).
	// Sprite center (100, 25) is at (+10, +5) relative to effective cam,
	// so at zoom=2 in 400x200 view it appears at view (200 + 20, 100 + 10) = (220, 110).
	cloud := sprite.NewSprite(testAtlas.Frames["hero"])
	cloud.X, cloud.Y = 100, 25
	cloud.OriginX, cloud.OriginY = 8, 8
	cloudBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&cloud}}

	layer := scene.NewParallaxLayer("clouds", 0, 0.5, 0.25)
	layer.OffsetX = 10
	layer.OffsetY = 5
	layer.AddBatch(cloudBatch)

	sc := scene.NewScene(cam)
	sc.AddLayer(layer)

	// Hit at exact parallax-projected view point (220, 110).
	hit, ok := sc.HitTest(core.Point{X: 220, Y: 110})
	if !ok || hit.Sprite != &cloud || hit.Layer != layer {
		t.Fatalf("HitTest(220, 110) = %+v, %v; want cloud", hit, ok)
	}
	if math.Abs(float64(hit.Point.X-100)) > 1e-3 || math.Abs(float64(hit.Point.Y-25)) > 1e-3 {
		t.Fatalf("hit.Point = %+v, want (100, 25)", hit.Point)
	}

	// Un-parallaxed world position (100, 25) -> cam.WorldToView = (0, 50) must NOT hit!
	if miss, ok := sc.HitTest(cam.WorldToView(core.Point{X: 100, Y: 25})); ok {
		t.Fatalf("un-parallaxed point unexpectedly hit %+v", miss)
	}
}

func TestRepeatingParallaxTilesCoverViewportAndHitTest(t *testing.T) {
	// Camera at X=260, Y=0, Width=200, Height=100, Zoom=1.
	// With ParallaxX=0.5, effective cam X = 130 -> visible X = [30, 230].
	// With RepeatX=true, RepeatWidth=100 -> tiles col 0 ([0..100)), col 1 ([100..200)), col 2 ([200..300)) cover [30, 230].
	cam := &tilemap.Camera{
		X:      260,
		Y:      50,
		Zoom:   1,
		Width:  200,
		Height: 100,
	}

	cloud := sprite.NewSprite(testAtlas.Frames["hero"])
	cloud.X, cloud.Y = 20, 40 // spans [20..36, 40..56] within each 100-wide period
	batch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&cloud}}

	layer := scene.NewParallaxLayer("sky", 0, 0.5, 1.0)
	layer.RepeatX = true
	layer.RepeatWidth = 100
	layer.AddBatch(batch)

	sc := scene.NewScene(cam)
	sc.AddLayer(layer)

	app := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "scene",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { sc.Draw(d) },
			},
			core.Canvas{
				TestID: "hand",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save()
					d.Translate(100, 50)
					d.Translate(-130, -50)
					// col 0 (tx = 0): no extra Save/Translate
					batch.Draw(d)
					// col 1 (tx = 100)
					d.Save()
					d.Translate(100, 0)
					batch.Draw(d)
					d.Restore()
					// col 2 (tx = 200)
					d.Save()
					d.Translate(200, 0)
					batch.Draw(d)
					d.Restore()
					d.Restore()
				},
			},
		}}
	})

	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)

	// HitTest against the repeated copy in col 1 (layer X = 120..136, Y = 40..56):
	// Under pcam (X=130, Y=50, W=200, H=100), layer (128, 48) maps to view (98, 48).
	hit, ok := sc.HitTest(core.Point{X: 98, Y: 48})
	if !ok || hit.Sprite != &cloud {
		t.Fatalf("HitTest on col 1 repeated cloud = %+v, %v; want cloud", hit, ok)
	}
	if math.Abs(float64(hit.Point.X-28)) > 1e-3 || math.Abs(float64(hit.Point.Y-48)) > 1e-3 {
		t.Fatalf("repeated hit local point = %+v, want (28, 48)", hit.Point)
	}

	// HitTest against col 2 (layer X = 220..236, Y = 40..56): layer (224, 48) maps to view (194, 48).
	if hit2, ok := sc.HitTest(core.Point{X: 194, Y: 48}); !ok || hit2.Sprite != &cloud {
		t.Fatalf("HitTest on col 2 repeated cloud = %+v, %v; want cloud", hit2, ok)
	}

	// Empty gap between repeated clouds (layer X = 160 -> view X = 130) misses!
	if miss, ok := sc.HitTest(core.Point{X: 130, Y: 48}); ok {
		t.Fatalf("gap between repeated clouds unexpectedly hit %+v", miss)
	}

	// Verify 2D repeating (both RepeatX and RepeatY) covers negative and positive indices cleanly.
	cam2 := &tilemap.Camera{X: 0, Y: 0, Zoom: 1, Width: 100, Height: 100} // visible [-50..50] x [-50..50]
	l2d := scene.NewParallaxLayer("stars", 0, 1, 1)
	l2d.RepeatX, l2d.RepeatWidth = true, 60  // cols -1, 0
	l2d.RepeatY, l2d.RepeatHeight = true, 60 // rows -1, 0 -> 2x2 = 4 draws
	l2d.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
		d.FillRect(5, 5, 4, 4)
	}))
	sc2 := scene.NewScene(cam2)
	sc2.AddLayer(l2d)

	app2 := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "grid-got",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { sc2.Draw(d) },
			},
			core.Canvas{
				TestID: "grid-want",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save()
					d.Translate(50, 50)
					// row -1, col -1 (-60, -60)
					d.Save().Translate(-60, -60).Save().FillRect(5, 5, 4, 4).Restore().Restore()
					// row -1, col 0 (0, -60)
					d.Save().Translate(0, -60).Save().FillRect(5, 5, 4, 4).Restore().Restore()
					// row 0, col -1 (-60, 0)
					d.Save().Translate(-60, 0).Save().FillRect(5, 5, 4, 4).Restore().Restore()
					// row 0, col 0 (0, 0)
					d.Save().FillRect(5, 5, 4, 4).Restore()
					d.Restore()
				},
			},
		}}
	})
	sameOps(t, app2.Find("grid-got").Ops, app2.Find("grid-want").Ops)
}

func TestSplitScreenVerticalHorizontalQuadDrawAndDividers(t *testing.T) {
	origCam := &tilemap.Camera{X: 999, Y: 999, Zoom: 1, Width: 200, Height: 100}
	hero := sprite.NewSprite(testAtlas.Frames["hero"])
	hero.X, hero.Y = 50, 50
	batch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&hero}}

	worldLayer := scene.NewLayer("world", scene.SpaceWorld, 0)
	worldLayer.AddBatch(batch)
	sc := scene.NewScene(origCam)
	sc.AddLayer(worldLayer)

	dividerColor := core.RGB(0xFFFFFF)
	hudColor := core.RGB(0x00FF00)

	// 1. VerticalSplit with independent cameras, per-player HUD, and divider line.
	leftCam := &tilemap.Camera{X: 50, Y: 50, Zoom: 1}
	rightCam := &tilemap.Camera{X: 120, Y: 30, Zoom: 2}
	vSplit := scene.VerticalSplit(core.Size{Width: 200, Height: 100}, leftCam, rightCam)
	vSplit.DividerWidth = 4
	vSplit.DividerColor = dividerColor
	vSplit.Viewports[0].DrawHUD = func(d *core.Drawing, sz core.Size) {
		d.SetFillColor(hudColor).FillRect(2, 2, float32(sz.Width)-4, 8)
	}

	// 2. HorizontalSplit with divider line.
	topCam := &tilemap.Camera{X: 10, Y: 20, Zoom: 1}
	bottomCam := &tilemap.Camera{X: 80, Y: 90, Zoom: 1.5}
	hSplit := scene.HorizontalSplit(core.Size{Width: 200, Height: 100}, topCam, bottomCam)
	hSplit.DividerWidth = 2
	hSplit.DividerColor = dividerColor

	// 3. QuadSplit with 4 cameras and merged vertical + horizontal divider lines.
	quadCams := [4]*tilemap.Camera{
		{X: 0, Y: 0, Zoom: 1},
		{X: 50, Y: 0, Zoom: 1},
		{X: 0, Y: 50, Zoom: 1},
		{X: 50, Y: 50, Zoom: 2},
	}
	qSplit := scene.QuadSplit(core.Size{Width: 200, Height: 100}, quadCams)
	qSplit.DividerWidth = 2
	qSplit.DividerColor = dividerColor

	app := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "v-got",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { vSplit.Draw(d, sc) },
			},
			core.Canvas{
				TestID: "v-want",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// Left viewport: clip (0,0,100,100), cam centered at (50,50), zoom 1, pos (50,50)
					d.Save()
					d.BeginPath().Rect(0, 0, 100, 100).Clip().BeginPath()
					d.Save()
					d.Translate(50, 50)
					d.Translate(-50, -50)
					batch.Draw(d)
					d.Restore()
					d.Save()
					d.SetFillColor(hudColor).FillRect(2, 2, 96, 8)
					d.Restore()
					d.Restore()

					// Right viewport: clip (100,0,100,100), translate (100,0), cam centered at (50,50), zoom 2, pos (120,30)
					d.Save()
					d.BeginPath().Rect(100, 0, 100, 100).Clip().BeginPath()
					d.Translate(100, 0)
					d.Save()
					d.Translate(50, 50)
					d.Scale(2, 2)
					d.Translate(-120, -30)
					batch.Draw(d)
					d.Restore()
					d.Restore()

					// Vertical divider centered on x=100, width 4.
					d.Save()
					d.SetFillColor(dividerColor)
					d.FillRect(98, 0, 4, 100)
					d.Restore()
				},
			},
			core.Canvas{
				TestID: "h-got",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { hSplit.Draw(d, sc) },
			},
			core.Canvas{
				TestID: "h-want",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// Top viewport: (0,0,200,50)
					d.Save()
					d.BeginPath().Rect(0, 0, 200, 50).Clip().BeginPath()
					d.Save()
					d.Translate(100, 25)
					d.Translate(-10, -20)
					batch.Draw(d)
					d.Restore()
					d.Restore()

					// Bottom viewport: (0,50,200,50)
					d.Save()
					d.BeginPath().Rect(0, 50, 200, 50).Clip().BeginPath()
					d.Translate(0, 50)
					d.Save()
					d.Translate(100, 25)
					d.Scale(1.5, 1.5)
					d.Translate(-80, -90)
					batch.Draw(d)
					d.Restore()
					d.Restore()

					// Horizontal divider centered on y=50, width 2.
					d.Save()
					d.SetFillColor(dividerColor)
					d.FillRect(0, 49, 200, 2)
					d.Restore()
				},
			},
			core.Canvas{
				TestID: "q-got",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { qSplit.Draw(d, sc) },
			},
			core.Canvas{
				TestID: "q-want",
				Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					origins := [4][2]float32{{0, 0}, {100, 0}, {0, 50}, {100, 50}}
					for i, o := range origins {
						d.Save()
						d.BeginPath().Rect(o[0], o[1], 100, 50).Clip().BeginPath()
						if o[0] != 0 || o[1] != 0 {
							d.Translate(o[0], o[1])
						}
						d.Save()
						d.Translate(50, 25)
						if quadCams[i].Zoom != 1 {
							d.Scale(quadCams[i].Zoom, quadCams[i].Zoom)
						}
						if quadCams[i].X != 0 || quadCams[i].Y != 0 {
							d.Translate(-quadCams[i].X, -quadCams[i].Y)
						}
						batch.Draw(d)
						d.Restore()
						d.Restore()
					}
					d.Save()
					d.SetFillColor(dividerColor)
					d.FillRect(99, 0, 2, 100)
					d.FillRect(0, 49, 200, 2)
					d.Restore()
				},
			},
		}}
	})

	sameOps(t, app.Find("v-got").Ops, app.Find("v-want").Ops)
	sameOps(t, app.Find("h-got").Ops, app.Find("h-want").Ops)
	sameOps(t, app.Find("q-got").Ops, app.Find("q-want").Ops)

	if sc.Camera != origCam {
		t.Fatalf("SplitScreen.Draw did not restore sc.Camera: got %p, want %p", sc.Camera, origCam)
	}
}

func TestSplitScreenHitTestAndInputRouting(t *testing.T) {
	origCam := &tilemap.Camera{X: -500, Y: -500}

	// Place 4 distinct targets far apart in world space, one centered in each QuadSplit camera!
	var targets [4]sprite.Sprite
	centers := [4]core.Point{
		{X: 0, Y: 0},
		{X: 500, Y: 0},
		{X: 0, Y: 500},
		{X: 500, Y: 500},
	}
	sprites := make([]*sprite.Sprite, 4)
	for i := range targets {
		targets[i] = sprite.NewSprite(testAtlas.Frames["hero"])
		targets[i].X = float32(centers[i].X)
		targets[i].Y = float32(centers[i].Y)
		targets[i].OriginX, targets[i].OriginY = 8, 8
		sprites[i] = &targets[i]
	}

	batch := &sprite.Batch{Atlas: testAtlas, Sprites: sprites}
	layer := scene.NewLayer("actors", scene.SpaceWorld, 0)
	layer.AddBatch(batch)
	sc := scene.NewScene(origCam)
	sc.AddLayer(layer)

	var cams [4]*tilemap.Camera
	for i := range cams {
		cams[i] = &tilemap.Camera{
			X:    float32(centers[i].X),
			Y:    float32(centers[i].Y),
			Zoom: 1,
		}
	}
	// Make bottom-right zoomed 2x to verify per-viewport zoom during HitTest!
	cams[3].Zoom = 2

	quad := scene.QuadSplit(core.Size{Width: 200, Height: 100}, cams)

	// Center of each quadrant in parent 200x100 view:
	quadViewCenters := [4]core.Point{
		{X: 50, Y: 25},
		{X: 150, Y: 25},
		{X: 50, Y: 75},
		{X: 150, Y: 75},
	}

	for i, vp := range quadViewCenters {
		vi, hit, ok := quad.HitTest(sc, vp)
		if !ok || vi != i || hit.Sprite != &targets[i] {
			t.Fatalf("quad %d HitTest(%+v) = (vi=%d, hit=%+v, ok=%v), want sprite %d", i, vp, vi, hit, ok, i)
		}
		if hit.ViewPoint != vp {
			t.Fatalf("quad %d hit.ViewPoint = %+v, want %+v", i, hit.ViewPoint, vp)
		}
		if hit.Point != centers[i] {
			t.Fatalf("quad %d hit.Point = %+v, want %+v", i, hit.Point, centers[i])
		}
	}

	// Zoom 2x check on quadrant 3: sprite half-width is 8 world pts = 16 view pts -> offset +12 view pts hits!
	// On quadrant 0 (zoom 1x), offset +12 view pts is outside 8-pt half-width -> misses!
	if vi, hit, ok := quad.HitTest(sc, core.Point{X: 162, Y: 75}); !ok || vi != 3 || hit.Sprite != &targets[3] {
		t.Fatalf("quad 3 zoomed edge hit = (vi=%d, hit=%+v, ok=%v), want sprite 3", vi, hit, ok)
	}
	if vi, hit, ok := quad.HitTest(sc, core.Point{X: 62, Y: 25}); ok || vi != 0 || hit != nil {
		t.Fatalf("quad 0 outside sprite = (vi=%d, hit=%+v, ok=%v), want (0, nil, false)", vi, hit, ok)
	}

	// Point outside all viewports returns (-1, nil, false).
	if vi, hit, ok := quad.HitTest(sc, core.Point{X: 250, Y: 25}); ok || vi != -1 || hit != nil {
		t.Fatalf("outside SplitScreen = (vi=%d, hit=%+v, ok=%v), want (-1, nil, false)", vi, hit, ok)
	}

	if sc.Camera != origCam {
		t.Fatalf("SplitScreen.HitTest did not restore sc.Camera")
	}

	// DispatchPress & DispatchTouch via nsgotest on VerticalSplit!
	vSplit := scene.VerticalSplit(core.Size{Width: 200, Height: 100}, cams[0], cams[1])
	var pressed []int
	sc.SetSpriteHit(&targets[0], scene.HitOptions{
		OnPress: func(_ scene.HitResult, _ core.PressEvent) { pressed = append(pressed, 0) },
	})
	sc.SetSpriteHit(&targets[1], scene.HitOptions{
		OnPress: func(_ scene.HitResult, _ core.PressEvent) { pressed = append(pressed, 1) },
	})

	app := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID:  "split",
				Style:   core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				OnPress: func(ev core.PressEvent) { vSplit.DispatchPress(sc, ev) },
				Draw:    func(d *core.Drawing, _ core.Size) { vSplit.Draw(d, sc) },
			},
			core.View{
				TestID:  "touch",
				Style:   core.Style{Width: core.Pt(200), Height: core.Pt(100)},
				OnTouch: func(ev core.TouchEvent) { vSplit.DispatchTouch(sc, ev) },
			},
		}}
	})

	// Left half center (50, 50) hits target 0; right half center (150, 50) hits target 1.
	app.PressAt("split", core.Point{X: 50, Y: 50})
	app.PressAt("split", core.Point{X: 150, Y: 50})
	f := app.Touch("touch", core.Point{X: 150, Y: 50})
	f.Lift()

	wantPressed := []int{0, 1, 1}
	if !reflect.DeepEqual(pressed, wantPressed) {
		t.Fatalf("split dispatch pressed = %v, want %v", pressed, wantPressed)
	}
}

func TestDepthSortYOrderInterleavingAndHitTest(t *testing.T) {
	// Overlapping sprites at X=10, Y=100 (front), Y=92 (back), Y=96 (mid) — all Z=0,
	// plus a high-Z sprite at Y=20 (Z=1, must still draw AFTER Z=0 sprites!).
	back := sprite.NewSprite(testAtlas.Frames["hero"])
	back.X, back.Y, back.Z = 10, 92, 0 // covers [10..26, 92..108]

	front := sprite.NewSprite(testAtlas.Frames["hero"])
	front.X, front.Y, front.Z = 10, 100, 0 // covers [10..26, 100..116]

	mid := sprite.NewSprite(testAtlas.Frames["hero"])
	mid.X, mid.Y, mid.Z = 10, 96, 0 // covers [10..26, 96..112]

	overlay := sprite.NewSprite(testAtlas.Frames["coin"])
	overlay.X, overlay.Y, overlay.Z = 10, 20, 1 // Z=1 draws after all Z=0 even though Y=20 is smallest

	// Deliberately unsorted slice order: front, overlay, back, mid.
	batch := &sprite.Batch{
		Atlas:   testAtlas,
		Sprites: []*sprite.Sprite{&front, &overlay, &back, &mid},
	}
	origOrder := append([]*sprite.Sprite(nil), batch.Sprites...)

	layer := scene.NewLayer("actors", scene.SpaceScreen, 0)
	layer.DepthSort = scene.SortY
	layer.AddBatch(batch)

	sc := scene.NewScene(nil)
	sc.AddLayer(layer)

	// Expected draw order: back (Z=0, Y=92), mid (Z=0, Y=96), front (Z=0, Y=100), overlay (Z=1, Y=20).
	expectedBatch := &sprite.Batch{
		Atlas:   testAtlas,
		Sprites: []*sprite.Sprite{&back, &mid, &front, &overlay},
	}

	app := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "got",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(150)},
				Draw:   func(d *core.Drawing, _ core.Size) { sc.Draw(d) },
			},
			core.Canvas{
				TestID: "want",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(150)},
				Draw:   func(d *core.Drawing, _ core.Size) { expectedBatch.Draw(d) },
			},
		}}
	})

	sameOps(t, app.Find("got").Ops, app.Find("want").Ops)

	// Verify batch.Sprites slice was NOT mutated.
	if !reflect.DeepEqual(batch.Sprites, origOrder) {
		t.Fatal("SortY Draw mutated Batch.Sprites slice order")
	}

	// Overlap point (15, 104) lies inside back ([92..108]), mid ([96..112]), and front ([100..116]).
	// Front (Y=100, drawn last) MUST win HitTest!
	top, ok := sc.HitTest(core.Point{X: 15, Y: 104})
	if !ok || top.Sprite != &front {
		t.Fatalf("HitTest on Y-sorted overlap = %+v, %v; want front (Y=100)", top, ok)
	}
	all := sc.HitTestAll(core.Point{X: 15, Y: 104})
	if len(all) != 3 || all[0].Sprite != &front || all[1].Sprite != &mid || all[2].Sprite != &back {
		t.Fatalf("HitTestAll on Y-sorted overlap = [%v, %v, %v], want [front, mid, back]",
			all[0].Sprite, all[1].Sprite, all[2].Sprite)
	}

	// Flip to SortNone: slice order [front, back, mid] is preserved (Z=0 ties keep slice order -> mid is drawn last!).
	layer.DepthSort = scene.SortNone
	topNone, ok := sc.HitTest(core.Point{X: 15, Y: 104})
	if !ok || topNone.Sprite != &mid {
		t.Fatalf("HitTest under SortNone = %+v, %v; want mid (last in slice)", topNone, ok)
	}
}

func TestDepthSortInterleavesBatchesAndDrawables(t *testing.T) {
	red := core.RGB(0xFF0000)
	blue := core.RGB(0x0000FF)

	sTop := sprite.NewSprite(testAtlas.Frames["hero"])
	sTop.X, sTop.Y = 20, 20
	sBot := sprite.NewSprite(testAtlas.Frames["hero"])
	sBot.X, sBot.Y = 20, 60
	batchA := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sBot, &sTop}}

	sMid := sprite.NewSprite(testAtlas.Frames["coin"])
	sMid.X, sMid.Y = 20, 40
	batchB := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sMid}}

	d1 := scene.NewDrawable(func(d *core.Drawing) {
		d.SetFillColor(red).FillRect(20, 30, 16, 16)
	})
	d1.Y = 30
	d1.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 20, Y: 30}, Size: core.Size{Width: 16, Height: 16}})

	d2 := scene.NewDrawable(func(d *core.Drawing) {
		d.SetFillColor(blue).FillRect(20, 50, 16, 16)
	})
	d2.Y = 50
	d2.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 20, Y: 50}, Size: core.Size{Width: 16, Height: 16}})

	layer := scene.NewLayer("mixed", scene.SpaceScreen, 0)
	layer.DepthSort = scene.SortY
	layer.AddBatch(batchA)
	layer.AddDrawable(d2)
	layer.AddBatch(batchB)
	layer.AddDrawable(d1)

	sc := scene.NewScene(nil)
	sc.AddLayer(layer)

	// Expected painter's order: sTop(20) -> d1(30) -> sMid(40) -> d2(50) -> sBot(60).
	app := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "got",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, _ core.Size) { sc.Draw(d) },
			},
			core.Canvas{
				TestID: "want",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sTop}}).Draw(d)
					d.Save().SetFillColor(red).FillRect(20, 30, 16, 16).Restore()
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sMid}}).Draw(d)
					d.Save().SetFillColor(blue).FillRect(20, 50, 16, 16).Restore()
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sBot}}).Draw(d)
				},
			},
		}}
	})
	sameOps(t, app.Find("got").Ops, app.Find("want").Ops)

	// At (24, 42): overlaps d1 ([30..46]) and sMid ([40..56]). sMid (Y=40) draws AFTER d1 (Y=30), so sMid wins!
	if hit, ok := sc.HitTest(core.Point{X: 24, Y: 42}); !ok || hit.Sprite != &sMid {
		t.Fatalf("HitTest(24, 42) = %+v, %v; want sMid over d1", hit, ok)
	}
	// At (24, 52): overlaps sMid ([40..56]) and d2 ([50..66]). d2 (Y=50) draws AFTER sMid (Y=40), so d2 wins!
	if hit, ok := sc.HitTest(core.Point{X: 24, Y: 52}); !ok || hit.Drawable != d2 {
		t.Fatalf("HitTest(24, 52) = %+v, %v; want d2 over sMid", hit, ok)
	}
}

func TestDepthSortCustomKeysAndSortFunc(t *testing.T) {
	// 1. SortY with isometric SpriteSortKey = sp.X + sp.Y:
	// Sprite A at (50, 10) has iso key 60; Sprite B at (10, 40) has iso key 50.
	// Even though A has smaller Y (10 < 40), its iso key (60 > 50) puts A in FRONT of B!
	a := sprite.NewSprite(testAtlas.Frames["hero"])
	a.X, a.Y = 50, 10
	a.ScaleX, a.ScaleY = 4, 4 // large overlapping boxes [50..114, 10..74]

	b := sprite.NewSprite(testAtlas.Frames["hero"])
	b.X, b.Y = 10, 40
	b.ScaleX, b.ScaleY = 4, 4 // [10..74, 40..104] -> overlaps A on [50..74, 40..74]

	isoBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&a, &b}}
	isoLayer := scene.NewLayer("iso", scene.SpaceScreen, 0)
	isoLayer.DepthSort = scene.SortY
	isoLayer.SpriteSortKey = func(sp *sprite.Sprite) float32 {
		return sp.X + sp.Y
	}
	isoLayer.AddBatch(isoBatch)

	scIso := scene.NewScene(nil)
	scIso.AddLayer(isoLayer)

	appIso := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "got",
				Style:  core.Style{Width: core.Pt(120), Height: core.Pt(120)},
				Draw:   func(d *core.Drawing, _ core.Size) { scIso.Draw(d) },
			},
			core.Canvas{
				TestID: "want",
				Style:  core.Style{Width: core.Pt(120), Height: core.Pt(120)},
				Draw: func(d *core.Drawing, _ core.Size) {
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&b, &a}}).Draw(d)
				},
			},
		}}
	})
	sameOps(t, appIso.Find("got").Ops, appIso.Find("want").Ops)

	// Overlap (60, 50): A (key 60) is drawn after B (key 50), so A wins HitTest!
	if hit, ok := scIso.HitTest(core.Point{X: 60, Y: 50}); !ok || hit.Sprite != &a {
		t.Fatalf("isometric HitTest(60, 50) = %+v, %v; want A", hit, ok)
	}

	// 2. SortCustom with SortFunc on drawables + SpriteSortKey overriding Sprite.Z:
	var drawLog []string
	dLow := scene.NewDrawable(func(d *core.Drawing) { drawLog = append(drawLog, "low") })
	dLow.Z = 1
	dLow.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 40, Height: 40}})

	dHigh := scene.NewDrawable(func(d *core.Drawing) { drawLog = append(drawLog, "high") })
	dHigh.Z = 10
	dHigh.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 40, Height: 40}})

	customLayer := scene.NewLayer("custom", scene.SpaceScreen, 0)
	customLayer.DepthSort = scene.SortCustom
	// Reverse-Z custom comparator: higher Z draws first, lower Z draws on top!
	customLayer.SortFunc = func(x, y scene.Item) int {
		var xz, yz int
		if x.Drawable != nil {
			xz = x.Drawable.Z
		}
		if y.Drawable != nil {
			yz = y.Drawable.Z
		}
		return cmp.Compare(yz, xz)
	}
	customLayer.AddDrawable(dLow)
	customLayer.AddDrawable(dHigh)

	scCustom := scene.NewScene(nil)
	scCustom.AddLayer(customLayer)

	nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.Canvas{
			TestID: "custom",
			Style:  core.Style{Width: core.Pt(50), Height: core.Pt(50)},
			Draw:   func(d *core.Drawing, _ core.Size) { scCustom.Draw(d) },
		}
	})
	if !reflect.DeepEqual(drawLog, []string{"high", "low"}) {
		t.Fatalf("SortCustom SortFunc draw order = %v, want [high low]", drawLog)
	}
	// HitTest must return dLow (drawn last under custom comparator)!
	if hit, ok := scCustom.HitTest(core.Point{X: 10, Y: 10}); !ok || hit.Drawable != dLow {
		t.Fatalf("SortCustom HitTest = %+v, %v; want dLow", hit, ok)
	}

	// 3. SortCustom with SpriteSortKey overriding Sprite.Z:
	sp1 := sprite.NewSprite(testAtlas.Frames["hero"])
	sp1.X, sp1.Y, sp1.Z = 10, 10, 100 // higher Z, but smaller custom key (X=10)
	sp2 := sprite.NewSprite(testAtlas.Frames["hero"])
	sp2.X, sp2.Y, sp2.Z = 14, 10, 0 // lower Z, but larger custom key (X=14)
	spLayer := scene.NewLayer("custom-sp", scene.SpaceScreen, 0)
	spLayer.DepthSort = scene.SortCustom
	spLayer.SpriteSortKey = func(sp *sprite.Sprite) float32 { return sp.X }
	spLayer.AddBatch(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sp2, &sp1}})

	scSp := scene.NewScene(nil)
	scSp.AddLayer(spLayer)

	appSp := nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "got",
				Style:  core.Style{Width: core.Pt(50), Height: core.Pt(50)},
				Draw:   func(d *core.Drawing, _ core.Size) { scSp.Draw(d) },
			},
			core.Canvas{
				TestID: "want",
				Style:  core.Style{Width: core.Pt(50), Height: core.Pt(50)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// sp1 (X=10) draws first, then sp2 (X=14) draws on top despite lower Z.
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sp1}}).Draw(d)
					(&sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&sp2}}).Draw(d)
				},
			},
		}}
	})
	sameOps(t, appSp.Find("got").Ops, appSp.Find("want").Ops)

	// Overlap (16, 16): sp2 (drawn last) wins HitTest!
	if hit, ok := scSp.HitTest(core.Point{X: 16, Y: 16}); !ok || hit.Sprite != &sp2 {
		t.Fatalf("SortCustom SpriteSortKey HitTest = %+v, %v; want sp2", hit, ok)
	}

	// 4. SortY with item-level SortKey on Drawables (e.g., custom foot baseline).
	var footLog []string
	tall := scene.NewDrawable(func(_ *core.Drawing) { footLog = append(footLog, "tall") })
	tall.Y = 10 // top is 10, height is 80 -> foot baseline 90
	tall.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 50, Height: 100}})

	short := scene.NewDrawable(func(_ *core.Drawing) { footLog = append(footLog, "short") })
	short.Y = 40 // top is 40, height is 20 -> foot baseline 60
	short.Hit = scene.RectHit(core.Bounds{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 50, Height: 100}})

	heights := map[*scene.Drawable]float32{tall: 80, short: 20}
	footLayer := scene.NewLayer("foot", scene.SpaceScreen, 0)
	footLayer.DepthSort = scene.SortY
	footLayer.SortKey = func(it scene.Item) float32 {
		if it.Drawable != nil {
			return it.Drawable.Y + heights[it.Drawable]
		}
		return 0
	}
	footLayer.AddDrawable(tall)
	footLayer.AddDrawable(short)

	scFoot := scene.NewScene(nil)
	scFoot.AddLayer(footLayer)
	nsgotest.Start(t, func(_ *core.Scope) core.Node {
		return core.Canvas{
			TestID: "foot",
			Style:  core.Style{Width: core.Pt(50), Height: core.Pt(100)},
			Draw:   func(d *core.Drawing, _ core.Size) { scFoot.Draw(d) },
		}
	})
	if !reflect.DeepEqual(footLog, []string{"short", "tall"}) {
		t.Fatalf("SortY SortKey foot-baseline order = %v, want [short tall]", footLog)
	}
	if hit, ok := scFoot.HitTest(core.Point{X: 10, Y: 50}); !ok || hit.Drawable != tall {
		t.Fatalf("SortY SortKey HitTest = %+v, %v; want tall", hit, ok)
	}
}

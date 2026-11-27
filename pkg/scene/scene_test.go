package scene_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var testAtlas = &sprite.Atlas{
	Image: core.ImageSource{URI: "asset:test.png"},
	Frames: map[string]sprite.Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
		"btn":  {Name: "btn", X: 32, Y: 0, W: 40, H: 20, Width: 40, Height: 20},
	},
}

func testTilemap() *tilemap.Tilemap {
	ts := &tilemap.Tileset{
		Name:       "tiles",
		Image:      core.ImageSource{URI: "asset:tiles.png"},
		FirstGID:   1,
		TileWidth:  16,
		TileHeight: 16,
		Columns:    2,
		TileCount:  4,
	}
	layer := tilemap.NewTileLayer("ground", 2, 1, []uint32{1, 2})
	return &tilemap.Tilemap{
		Width:      2,
		Height:     1,
		TileWidth:  16,
		TileHeight: 16,
		Tilesets:   []*tilemap.Tileset{ts},
		Layers:     []tilemap.TileLayer{layer},
	}
}

// sameOps fails t unless got and want match operation-for-operation within float rounding.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
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

func TestSceneDrawsOrderedWorldAndScreenLayers(t *testing.T) {
	cam := &tilemap.Camera{
		X:      30,
		Y:      20,
		Zoom:   2,
		Width:  200,
		Height: 100,
	}
	tm := testTilemap()
	hero := sprite.NewSprite(testAtlas.Frames["hero"])
	hero.X, hero.Y = 24, 12
	worldBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&hero}}

	hudBtn := sprite.NewSprite(testAtlas.Frames["btn"])
	hudBtn.X, hudBtn.Y = 8, 6
	hudBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&hudBtn}}

	// Add layers out of order, including an Order tie (stable) and a hidden layer.
	hudLayer := scene.NewLayer("hud", scene.SpaceScreen, 10)
	hudLayer.AddBatch(hudBatch)

	hiddenLayer := scene.NewLayer("hidden", scene.SpaceWorld, 5)
	hiddenLayer.Visible = false
	hiddenLayer.AddBatch(worldBatch)

	groundLayer := scene.NewLayer("ground", scene.SpaceWorld, 1)
	groundLayer.AddTilemap(tm)

	actorsLayer := scene.NewLayer("actors", scene.SpaceWorld, 1) // same Order as ground: draws after ground
	actorsLayer.AddBatch(worldBatch)

	sc := scene.NewScene(cam)
	sc.AddLayer(hudLayer)
	sc.AddLayer(hiddenLayer)
	sc.AddLayer(groundLayer)
	sc.AddLayer(actorsLayer)

	if sc.Layer("actors") != actorsLayer || sc.Layer("missing") != nil {
		t.Fatal("Layer lookup mismatch")
	}

	var tick *core.Signal[int]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				sc.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				// 1. groundLayer (SpaceWorld, Order 1)
				d.Save()
				cam.Apply(d)
				tm.Draw(d, cam)
				d.Restore()
				// 2. actorsLayer (SpaceWorld, Order 1, tie kept)
				d.Save()
				cam.Apply(d)
				worldBatch.Draw(d)
				d.Restore()
				// 3. hudLayer (SpaceScreen, Order 10 — unaffected by camera!)
				hudBatch.Draw(d)
			}},
		}}
	})

	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)
	if sc.Layers[0] != hudLayer {
		t.Fatal("Scene.Draw mutated Scene.Layers slice order")
	}

	// Move the camera: world ops shift while screen HUD op stays at exact (8, 6).
	before := app.Find("scene").Ops
	lastBefore := before[len(before)-1]
	app.Post(func() {
		cam.X = 75
		cam.Y = -40
		tick.Set(1)
	})
	app.Settle()

	after := app.Find("scene").Ops
	sameOps(t, after, app.Find("hand").Ops)
	lastAfter := after[len(after)-1]
	sameOps(t, []core.DrawOp{lastAfter}, []core.DrawOp{lastBefore})
	if lastAfter.Kind != core.DrawImage || lastAfter.X != 8 || lastAfter.Y != 6 {
		t.Fatalf("screen HUD op changed with camera: after=%+v", lastAfter)
	}
}

func TestCustomDrawableDrawsInExactOrderAndUpdates(t *testing.T) {
	hero := sprite.NewSprite(testAtlas.Frames["hero"])
	hero.X, hero.Y = 20, 20
	batch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&hero}}

	var updateLog []string
	var totalDT time.Duration

	back := scene.NewDrawable(func(d *core.Drawing) {
		d.SetFillColor(core.RGB(0xFF0000))
		d.FillRect(0, 0, 10, 10)
	})
	back.Z = -1
	back.Update = func(dt time.Duration) {
		updateLog = append(updateLog, "back")
		totalDT += dt
	}

	front := scene.NewDrawable(func(d *core.Drawing) {
		// Deliberately mutates transform without restoring to verify Scene isolates drawables.
		d.Translate(5, 5)
		d.SetFillColor(core.RGB(0x00FF00))
		d.FillRect(1, 2, 3, 4)
	})
	front.Z = 2
	front.Update = func(dt time.Duration) {
		updateLog = append(updateLog, "front")
	}

	hidden := scene.NewDrawable(func(d *core.Drawing) {
		d.FillRect(99, 99, 99, 99)
	})
	hidden.Visible = false
	hidden.Z = 1

	layer := scene.NewLayer("mixed", scene.SpaceScreen, 0)
	layer.AddDrawable(front)
	layer.AddItem(scene.Item{Batch: batch, Z: 0})
	layer.AddDrawable(hidden)
	layer.AddDrawable(back)

	cam := &tilemap.Camera{ShakeSeed: 3}
	cam.Shake(8, 100*time.Millisecond)
	sc := scene.NewScene(cam)
	sc.AddLayer(layer)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				sc.Draw(d)
				// Draw one final rect after Scene.Draw to verify d is clean.
				d.FillRect(50, 50, 5, 5)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Save()
				d.SetFillColor(core.RGB(0xFF0000))
				d.FillRect(0, 0, 10, 10)
				d.Restore()
				batch.Draw(d)
				d.Save()
				d.Translate(5, 5)
				d.SetFillColor(core.RGB(0x00FF00))
				d.FillRect(1, 2, 3, 4)
				d.Restore()
				d.FillRect(50, 50, 5, 5)
			}},
		}}
	})
	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)

	sc.Update(50 * time.Millisecond)
	sc.Update(50 * time.Millisecond)
	wantLog := []string{"back", "front", "back", "front"}
	if !reflect.DeepEqual(updateLog, wantLog) {
		t.Fatalf("update order = %v, want %v", updateLog, wantLog)
	}
	if totalDT != 100*time.Millisecond {
		t.Fatalf("totalDT = %v, want 100ms", totalDT)
	}
	if off := cam.ShakeOffset(); off != (core.Point{}) {
		t.Fatalf("camera shake did not finish during Scene.Update: offset = %+v", off)
	}
}

func TestHitTestOverlappingSpritesLayersTransformsAndShapes(t *testing.T) {
	cam := &tilemap.Camera{
		X:      100,
		Y:      100,
		Zoom:   2,
		Width:  400,
		Height: 200,
	}
	// World (100, 100) maps to View (200, 100). Zoom 2: 10 world units = 20 view units.

	low := sprite.NewSprite(testAtlas.Frames["hero"])
	low.X, low.Y, low.OriginX, low.OriginY, low.Z = 100, 100, 8, 8, 1

	high := sprite.NewSprite(testAtlas.Frames["hero"])
	high.X, high.Y, high.OriginX, high.OriginY, high.Z = 100, 100, 8, 8, 5

	ignored := sprite.NewSprite(testAtlas.Frames["hero"])
	ignored.X, ignored.Y, ignored.OriginX, ignored.OriginY, ignored.Z = 100, 100, 8, 8, 99

	invisible := sprite.NewSprite(testAtlas.Frames["hero"])
	invisible.X, invisible.Y, invisible.OriginX, invisible.OriginY, invisible.Z, invisible.Alpha = 100, 100, 8, 8, 100, 0

	// Rotated + scaled + flipped trimmed coin sprite at world (150, 100):
	// Untrimmed frame is 16x16, origin (8, 8), ScaleX=2, ScaleY=1, Rotation=pi/2 (clockwise):
	// Local X (width 16 * scale 2 = 32) rotates onto world Y [84..116].
	// Local Y (height 16 * scale 1 = 16) rotates onto world X [142..158].
	rotCoin := sprite.NewSprite(testAtlas.Frames["coin"])
	rotCoin.X, rotCoin.Y = 150, 100
	rotCoin.OriginX, rotCoin.OriginY = 8, 8
	rotCoin.ScaleX, rotCoin.ScaleY = 2, 1
	rotCoin.Rotation = math.Pi / 2
	rotCoin.FlipX = true

	// Circle-shaped sprite at world (50, 100) with radius 6 inside its 16x16 box.
	orb := sprite.NewSprite(testAtlas.Frames["hero"])
	orb.X, orb.Y, orb.OriginX, orb.OriginY = 50, 100, 8, 8

	worldBatch := &sprite.Batch{
		Atlas:   testAtlas,
		Sprites: []*sprite.Sprite{&high, &low, &ignored, &invisible, &rotCoin, &orb},
	}

	// Screen HUD sprite positioned at view (190..230, 90..110), overlapping view (200, 100).
	hud := sprite.NewSprite(testAtlas.Frames["btn"])
	hud.X, hud.Y = 190, 90
	hudBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&hud}}

	// Polygon drawable on a middle world layer (triangle around world (100, 140)).
	poly := scene.NewDrawable(nil)
	poly.Hit = scene.PolygonHit(
		core.Point{X: 90, Y: 145},
		core.Point{X: 110, Y: 145},
		core.Point{X: 100, Y: 125},
	)

	worldLayer := scene.NewLayer("world", scene.SpaceWorld, 1)
	worldLayer.AddBatch(worldBatch)
	worldLayer.AddDrawable(poly)

	hudLayer := scene.NewLayer("hud", scene.SpaceScreen, 10)
	hudLayer.AddBatch(hudBatch)

	sc := scene.NewScene(cam)
	sc.AddLayer(worldLayer)
	sc.AddLayer(hudLayer)
	sc.SetSpriteHit(&ignored, scene.HitOptions{Ignore: true})
	sc.SetSpriteHit(&orb, scene.HitOptions{
		Hit: scene.SpriteLocalHit(&orb, scene.CircleHit(core.Point{X: 8, Y: 8}, 6)),
	})

	// 1. Overlapping screen HUD over world high & low at view (200, 100):
	top, ok := sc.HitTest(core.Point{X: 200, Y: 100})
	if !ok || top.Sprite != &hud || top.Space != scene.SpaceScreen || top.Layer != hudLayer {
		t.Fatalf("HitTest(200, 100) = %+v, %v; want screen hud sprite", top, ok)
	}
	if top.Point != (core.Point{X: 200, Y: 100}) || top.ViewPoint != (core.Point{X: 200, Y: 100}) {
		t.Fatalf("screen hit point = %+v (view %+v), want (200, 100)", top.Point, top.ViewPoint)
	}

	all := sc.HitTestAll(core.Point{X: 200, Y: 100})
	if len(all) != 3 || all[0].Sprite != &hud || all[1].Sprite != &high || all[2].Sprite != &low {
		t.Fatalf("HitTestAll(200, 100) = %+v, want [hud, high, low]", all)
	}
	if all[1].Space != scene.SpaceWorld || all[1].Point != (core.Point{X: 100, Y: 100}) {
		t.Fatalf("world hit point = %+v (space %v), want world (100, 100)", all[1].Point, all[1].Space)
	}

	// Hide HUD layer: now high world sprite is topmost!
	hudLayer.Visible = false
	top, ok = sc.HitTest(core.Point{X: 200, Y: 100})
	if !ok || top.Sprite != &high {
		t.Fatalf("with hud hidden, HitTest(200, 100) = %+v, %v; want high", top, ok)
	}

	// 2. Rotated + scaled + flipped trimmed sprite at world (150, 100):
	// World (150, 114) is inside rotated height [84..116], maps to view (300, 128).
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 150, Y: 114})); !ok || hit.Sprite != &rotCoin {
		t.Fatalf("rotated sprite interior hit = %+v, %v; want rotCoin", hit, ok)
	}
	// World (162, 100) would be inside unrotated width 32, but after pi/2 rotation X spans only [142..158].
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 162, Y: 100})); ok {
		t.Fatalf("rotated sprite outside point hit %+v, want miss", hit)
	}

	// 3. Circle custom hit shape on orb (center world 50,100, radius 6):
	// Inside circle at (54, 100) hits; corner of 16x16 box at (57, 107) misses!
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 54, Y: 100})); !ok || hit.Sprite != &orb {
		t.Fatalf("circle hit interior = %+v, %v; want orb", hit, ok)
	}
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 57, Y: 107})); ok {
		t.Fatalf("circle hit corner = %+v, want miss", hit)
	}

	// 4. Polygon drawable hit test:
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 100, Y: 140})); !ok || hit.Drawable != poly {
		t.Fatalf("polygon interior hit = %+v, %v; want poly", hit, ok)
	}
	if hit, ok := sc.HitTest(cam.WorldToView(core.Point{X: 91, Y: 127})); ok {
		t.Fatalf("polygon outside hit = %+v, want miss", hit)
	}
}

func TestDispatchPressAndTouchUnderNSGoTest(t *testing.T) {
	cam := &tilemap.Camera{X: 0, Y: 0, Zoom: 1, Width: 200, Height: 200}

	back := sprite.NewSprite(testAtlas.Frames["hero"])
	back.X, back.Y, back.Z = 0, 0, 1 // world (0..16, 0..16) -> view (100..116, 100..116)
	front := sprite.NewSprite(testAtlas.Frames["hero"])
	front.X, front.Y, front.Z = 4, 4, 5 // world (4..20, 4..20) -> view (104..120, 104..120)

	worldBatch := &sprite.Batch{Atlas: testAtlas, Sprites: []*sprite.Sprite{&back, &front}}
	worldLayer := scene.NewLayer("world", scene.SpaceWorld, 1)
	worldLayer.AddBatch(worldBatch)

	hudDrawable := scene.NewDrawable(func(d *core.Drawing) {
		d.FillRect(10, 10, 30, 30)
	})
	hudDrawable.Hit = scene.RectHit(core.Bounds{
		Origin: core.Point{X: 10, Y: 10},
		Size:   core.Size{Width: 30, Height: 30},
	})
	screenLayer := scene.NewLayer("hud", scene.SpaceScreen, 5)
	screenLayer.AddDrawable(hudDrawable)

	sc := scene.NewScene(cam)
	sc.AddLayer(worldLayer)
	sc.AddLayer(screenLayer)

	var hits []string
	sc.SetSpriteHit(&back, scene.HitOptions{
		OnPress: func(h scene.HitResult, _ core.PressEvent) { hits = append(hits, "back-press") },
	})
	sc.SetSpriteHit(&front, scene.HitOptions{
		OnPress: func(h scene.HitResult, _ core.PressEvent) { hits = append(hits, "front-press") },
		OnTouch: func(h scene.HitResult, ev core.TouchEvent) {
			if ev.Phase == core.TouchStart {
				hits = append(hits, "front-touch")
			}
		},
	})
	hudDrawable.OnPress = func(h scene.HitResult, _ core.PressEvent) {
		hits = append(hits, "hud-press")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID:  "stage",
				Style:   core.Style{Width: core.Pt(200), Height: core.Pt(200)},
				OnPress: func(ev core.PressEvent) { sc.DispatchPress(ev) },
				Draw:    func(d *core.Drawing, _ core.Size) { sc.Draw(d) },
			},
			core.View{
				TestID:  "touch-host",
				Style:   core.Style{Width: core.Pt(200), Height: core.Pt(200)},
				OnTouch: func(ev core.TouchEvent) { sc.DispatchTouch(ev) },
			},
		}}
	})

	// Press overlap of back and front -> only front receives OnPress.
	app.PressAt("stage", core.Point{X: 108, Y: 108})
	// Press back-only region -> back receives OnPress.
	app.PressAt("stage", core.Point{X: 101, Y: 101})
	// Press screen HUD drawable -> hud receives OnPress.
	app.PressAt("stage", core.Point{X: 20, Y: 20})
	// Touch front -> OnTouch fires on TouchStart (not OnPress).
	f := app.Touch("touch-host", core.Point{X: 108, Y: 108})
	f.Lift()
	// Touch hud (has OnPress only, no OnTouch) -> falls back to OnPress on TouchStart.
	f2 := app.Touch("touch-host", core.Point{X: 20, Y: 20})
	f2.Lift()

	want := []string{"front-press", "back-press", "hud-press", "front-touch", "hud-press"}
	if !reflect.DeepEqual(hits, want) {
		t.Fatalf("dispatch hits = %v, want %v", hits, want)
	}
}

func TestManagerSwitchPushPopLifecycleAndDisposal(t *testing.T) {
	var events []string
	var aTicks, bTicks, overlayTicks int
	var aScope, bScope, overlayScope *core.Scope
	var mgr *scene.Manager

	var sceneA, sceneB, overlay *scene.Controller

	sceneA = &scene.Controller{
		Enter: func(cx *core.Scope) {
			aScope = cx
			events = append(events, "A:enter")
			cx.Cleanup(func() { events = append(events, "A:cleanup") })
			l := &game.Loop{Rate: 60, Update: func(time.Duration) { aTicks++ }}
			l.Run(cx)
		},
		Pause:  func() { events = append(events, "A:pause") },
		Resume: func() { events = append(events, "A:resume") },
		Exit:   func() { events = append(events, "A:exit") },
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(core.RGB(0x111111))
			d.FillRect(0, 0, 10, 10)
		},
	}

	overlay = &scene.Controller{
		Transparent: true,
		Enter: func(cx *core.Scope) {
			overlayScope = cx
			events = append(events, "O:enter")
			cx.Cleanup(func() { events = append(events, "O:cleanup") })
			l := &game.Loop{Rate: 60, Update: func(time.Duration) { overlayTicks++ }}
			l.Run(cx)
		},
		Exit: func() { events = append(events, "O:exit") },
		Update: func(dt time.Duration) {
			events = append(events, "O:update")
		},
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(core.RGB(0x222222))
			d.FillRect(5, 5, 5, 5)
		},
	}

	sceneB = &scene.Controller{
		Enter: func(cx *core.Scope) {
			bScope = cx
			events = append(events, "B:enter")
			cx.Cleanup(func() { events = append(events, "B:cleanup") })
			l := &game.Loop{Rate: 60, Update: func(time.Duration) { bTicks++ }}
			l.Run(cx)
		},
		Exit: func() { events = append(events, "B:exit") },
		Update: func(dt time.Duration) {
			// Verify switching from inside Update does not parent next under B's scope!
			if dt == time.Millisecond {
				mgr.Switch(sceneA, nil)
			}
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(sceneA, nil)
		return core.Canvas{
			TestID: "stage",
			Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
			Draw:   func(d *core.Drawing, size core.Size) { mgr.Draw(d, size, 0) },
		}
	})

	// Run sceneA's loop for 100ms (6 steps at 60Hz).
	app.Advance(100 * time.Millisecond)
	if aTicks != 6 {
		t.Fatalf("sceneA ticks = %d, want 6", aTicks)
	}

	// Push transparent overlay on top of sceneA: pauses A, enters O, draws BOTH A and O!
	app.Post(func() {
		mgr.Push(overlay)
		mgr.Update(16 * time.Millisecond)
	})
	app.Settle()
	if mgr.Depth() != 2 || mgr.Current() != overlay {
		t.Fatalf("after Push: depth=%d current=%v", mgr.Depth(), mgr.Current())
	}
	ops := app.Find("stage").Ops
	var fillColors []core.Color
	for _, op := range ops {
		if op.Kind == core.DrawFillColor {
			fillColors = append(fillColors, op.Color)
		}
	}
	if !reflect.DeepEqual(fillColors, []core.Color{core.RGB(0x111111), core.RGB(0x222222)}) {
		t.Fatalf("transparent overlay fill colors = %v, want [A, O]", fillColors)
	}

	app.Advance(100 * time.Millisecond)
	if overlayTicks != 6 {
		t.Fatalf("overlay ticks = %d, want 6", overlayTicks)
	}

	// Pop overlay: exits & disposes O (stopping O's loop), resumes A!
	app.Post(func() {
		popped := mgr.Pop()
		if popped != overlay {
			t.Errorf("Pop returned %v, want overlay", popped)
		}
	})
	app.Settle()
	if !overlayScope.Disposed() {
		t.Fatal("overlay child scope was not disposed on Pop")
	}
	savedOverlayTicks := overlayTicks
	app.Advance(100 * time.Millisecond)
	if overlayTicks != savedOverlayTicks {
		t.Fatalf("overlay loop kept running after Pop: %d -> %d", savedOverlayTicks, overlayTicks)
	}

	// Switch A -> B: exits & disposes A (stopping A's loop), enters B (starts B's loop).
	app.Post(func() { mgr.Switch(sceneB, nil) })
	app.Settle()
	if !aScope.Disposed() {
		t.Fatal("sceneA child scope was not disposed on Switch")
	}
	savedATicks := aTicks
	app.Advance(100 * time.Millisecond)
	if aTicks != savedATicks {
		t.Fatalf("sceneA loop kept running after Switch: %d -> %d", savedATicks, aTicks)
	}
	if bTicks != 6 {
		t.Fatalf("sceneB ticks = %d, want 6", bTicks)
	}

	// Switch from INSIDE sceneB's Update callback back to sceneA:
	// sceneB disposes, and the newly entered sceneA's scope must remain alive!
	app.Post(func() { mgr.Update(time.Millisecond) })
	app.Settle()
	if !bScope.Disposed() {
		t.Fatal("sceneB scope was not disposed when switching from Update")
	}
	if aScope.Disposed() {
		t.Fatal("new sceneA scope was erroneously disposed when switching from Update")
	}

	wantPrefix := []string{
		"A:enter",
		"A:pause", "O:enter", "O:update",
		"O:exit", "O:cleanup", "A:resume",
		"A:exit", "A:cleanup", "B:enter",
		"B:exit", "B:cleanup", "A:enter",
	}
	if !reflect.DeepEqual(events, wantPrefix) {
		t.Fatalf("lifecycle events = %v, want %v", events, wantPrefix)
	}
}

func TestFadeCrossfadeDrawsExpectedAlphaAndDisposesOldScene(t *testing.T) {
	red := core.RGB(0xFF0000)
	blue := core.RGB(0x0000FF)

	var oldExited, oldCleaned bool
	var oldTicks int
	var oldScope *core.Scope

	sceneA := &scene.Controller{
		Enter: func(cx *core.Scope) {
			oldScope = cx
			cx.Cleanup(func() { oldCleaned = true })
			l := &game.Loop{Rate: 60, Update: func(time.Duration) { oldTicks++ }}
			l.Run(cx)
		},
		Exit: func() { oldExited = true },
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(red)
			d.FillRect(0, 0, 50, 50)
		},
	}
	sceneB := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) {
			d.SetFillColor(blue)
			d.FillRect(10, 10, 50, 50)
		},
	}

	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(sceneA, nil)
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "stage",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw:   func(d *core.Drawing, size core.Size) { mgr.Draw(d, size, 0) },
			},
			core.Canvas{
				TestID: "want-0",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetAlpha(1).SetFillColor(red).FillRect(0, 0, 50, 50).Restore()
				},
			},
			core.Canvas{
				TestID: "want-half",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetAlpha(0.5).SetFillColor(red).FillRect(0, 0, 50, 50).Restore()
					d.Save().SetAlpha(0.5).SetFillColor(blue).FillRect(10, 10, 50, 50).Restore()
				},
			},
			core.Canvas{
				TestID: "want-1",
				Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.SetFillColor(blue).FillRect(10, 10, 50, 50)
				},
			},
		}}
	})

	// Start a 400ms crossfade (color 0): progress is 0.0 immediately.
	app.Post(func() { mgr.Switch(sceneB, scene.Fade(400*time.Millisecond, 0)) })
	app.Settle()

	if !mgr.Transitioning() || mgr.TransitionProgress() != 0 {
		t.Fatalf("at t=0: transitioning=%v progress=%v", mgr.Transitioning(), mgr.TransitionProgress())
	}
	if oldExited || oldCleaned || oldScope.Disposed() {
		t.Fatal("old scene disposed prematurely at progress 0.0")
	}
	sameOps(t, app.Find("stage").Ops, app.Find("want-0").Ops)

	// Halfway (200ms): progress = 0.5, both scenes drawn at alpha 0.5, old scene still alive.
	app.Advance(200 * time.Millisecond)
	if !mgr.Transitioning() || math.Abs(mgr.TransitionProgress()-0.5) > 1e-4 {
		t.Fatalf("at t=200ms: transitioning=%v progress=%v", mgr.Transitioning(), mgr.TransitionProgress())
	}
	if oldExited || oldCleaned || oldScope.Disposed() {
		t.Fatal("old scene disposed prematurely at progress 0.5")
	}
	sameOps(t, app.Find("stage").Ops, app.Find("want-half").Ops)

	// Complete (400ms): progress = 1.0, old scene exited & disposed, only new scene draws.
	app.Advance(200 * time.Millisecond)
	if mgr.Transitioning() || mgr.TransitionProgress() != 1 {
		t.Fatalf("at t=400ms: transitioning=%v progress=%v", mgr.Transitioning(), mgr.TransitionProgress())
	}
	if !oldExited || !oldCleaned || !oldScope.Disposed() {
		t.Fatalf("old scene not disposed on completion: exited=%v cleaned=%v disposed=%v", oldExited, oldCleaned, oldScope.Disposed())
	}
	sameOps(t, app.Find("stage").Ops, app.Find("want-1").Ops)

	// Old scene's loop must no longer tick after disposal.
	savedTicks := oldTicks
	app.Advance(100 * time.Millisecond)
	if oldTicks != savedTicks {
		t.Fatalf("old scene loop kept ticking after fade completed: %d -> %d", savedTicks, oldTicks)
	}
}

func TestFadeThroughColorDrawsOverlayAlpha(t *testing.T) {
	red := core.RGB(0xFF0000)
	blue := core.RGB(0x0000FF)
	black := core.RGB(0x000000)

	sceneA := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) { d.SetFillColor(red).FillRect(0, 0, 20, 20) },
	}
	sceneB := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) { d.SetFillColor(blue).FillRect(0, 0, 20, 20) },
	}

	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(sceneA, nil)
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "stage",
				Style:  core.Style{Width: core.Pt(80), Height: core.Pt(60)},
				Draw:   func(d *core.Drawing, size core.Size) { mgr.Draw(d, size, 0) },
			},
			core.Canvas{
				TestID: "want-quarter",
				Style:  core.Style{Width: core.Pt(80), Height: core.Pt(60)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetFillColor(red).FillRect(0, 0, 20, 20).Restore()
					d.Save().SetAlpha(0.5).SetFillColor(black).FillRect(0, 0, 80, 60).Restore()
				},
			},
			core.Canvas{
				TestID: "want-half",
				Style:  core.Style{Width: core.Pt(80), Height: core.Pt(60)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetFillColor(blue).FillRect(0, 0, 20, 20).Restore()
					d.Save().SetAlpha(1.0).SetFillColor(black).FillRect(0, 0, 80, 60).Restore()
				},
			},
			core.Canvas{
				TestID: "want-three-quarter",
				Style:  core.Style{Width: core.Pt(80), Height: core.Pt(60)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetFillColor(blue).FillRect(0, 0, 20, 20).Restore()
					d.Save().SetAlpha(0.5).SetFillColor(black).FillRect(0, 0, 80, 60).Restore()
				},
			},
		}}
	})

	app.Post(func() { mgr.Switch(sceneB, scene.Fade(400*time.Millisecond, black)) })
	app.Settle()

	app.Advance(100 * time.Millisecond) // p = 0.25
	sameOps(t, app.Find("stage").Ops, app.Find("want-quarter").Ops)

	app.Advance(100 * time.Millisecond) // p = 0.50
	sameOps(t, app.Find("stage").Ops, app.Find("want-half").Ops)

	app.Advance(100 * time.Millisecond) // p = 0.75
	sameOps(t, app.Find("stage").Ops, app.Find("want-three-quarter").Ops)

	app.Advance(100 * time.Millisecond) // p = 1.00
	if mgr.Transitioning() {
		t.Fatal("fade-through-color did not finish at 400ms")
	}
}

func TestWipeAllDirectionsDrawsExpectedClipRects(t *testing.T) {
	red := core.RGB(0xFF0000)
	blue := core.RGB(0x0000FF)

	cases := []struct {
		name                   string
		dir                    scene.Direction
		oldX, oldY, oldW, oldH float32
		newX, newY, newW, newH float32
	}{
		// At p = 0.25 on 200x100:
		{"left", scene.DirectionLeft, 0, 0, 150, 100, 150, 0, 50, 100},
		{"right", scene.DirectionRight, 50, 0, 150, 100, 0, 0, 50, 100},
		{"up", scene.DirectionUp, 0, 0, 200, 75, 0, 75, 200, 25},
		{"down", scene.DirectionDown, 0, 25, 200, 75, 0, 0, 200, 25},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var oldDisposed bool
			sceneA := &scene.Controller{
				Enter: func(cx *core.Scope) { cx.Cleanup(func() { oldDisposed = true }) },
				Draw:  func(d *core.Drawing, _ float64) { d.SetFillColor(red).FillRect(0, 0, 200, 100) },
			}
			sceneB := &scene.Controller{
				Draw: func(d *core.Drawing, _ float64) { d.SetFillColor(blue).FillRect(0, 0, 200, 100) },
			}

			var mgr *scene.Manager
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				mgr = scene.NewManager(cx)
				mgr.Switch(sceneA, nil)
				return core.View{Children: []core.Node{
					core.Canvas{
						TestID: "stage",
						Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
						Draw:   func(d *core.Drawing, size core.Size) { mgr.Draw(d, size, 0) },
					},
					core.Canvas{
						TestID: "want",
						Style:  core.Style{Width: core.Pt(200), Height: core.Pt(100)},
						Draw: func(d *core.Drawing, _ core.Size) {
							d.Save().BeginPath().Rect(tc.oldX, tc.oldY, tc.oldW, tc.oldH).Clip().BeginPath()
							d.SetFillColor(red).FillRect(0, 0, 200, 100)
							d.Restore()
							d.Save().BeginPath().Rect(tc.newX, tc.newY, tc.newW, tc.newH).Clip().BeginPath()
							d.SetFillColor(blue).FillRect(0, 0, 200, 100)
							d.Restore()
						},
					},
				}}
			})

			app.Post(func() { mgr.Switch(sceneB, scene.Wipe(400*time.Millisecond, tc.dir)) })
			app.Settle()

			app.Advance(100 * time.Millisecond) // p = 0.25
			sameOps(t, app.Find("stage").Ops, app.Find("want").Ops)
			if oldDisposed {
				t.Fatal("old scene disposed before wipe completed")
			}

			app.Advance(300 * time.Millisecond) // p = 1.0
			if mgr.Transitioning() || !oldDisposed {
				t.Fatalf("after wipe: transitioning=%v oldDisposed=%v", mgr.Transitioning(), oldDisposed)
			}
		})
	}
}

func TestCustomTransitionAndInterruptDisposesPreviousScenes(t *testing.T) {
	var seenProgress []float64
	var seenSize core.Size
	var aDisposed, bDisposed bool

	sceneA := &scene.Controller{
		Enter: func(cx *core.Scope) { cx.Cleanup(func() { aDisposed = true }) },
		Draw:  func(d *core.Drawing, _ float64) { d.FillRect(0, 0, 10, 10) },
	}
	sceneB := &scene.Controller{
		Enter: func(cx *core.Scope) { cx.Cleanup(func() { bDisposed = true }) },
		Draw:  func(d *core.Drawing, _ float64) { d.FillRect(20, 20, 10, 10) },
	}
	sceneC := &scene.Controller{
		Draw: func(d *core.Drawing, _ float64) { d.FillRect(40, 40, 10, 10) },
	}

	custom := scene.CustomTransition(200*time.Millisecond, func(d *core.Drawing, size core.Size, progress float64, drawOld, drawNew func(*core.Drawing)) {
		seenProgress = append(seenProgress, progress)
		seenSize = size
		d.Save()
		d.Translate(float32(-progress*float64(size.Width)), 0)
		drawOld(d)
		d.Restore()
		d.Save()
		d.Translate(float32((1-progress)*float64(size.Width)), 0)
		drawNew(d)
		d.Restore()
	})

	var mgr *scene.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		mgr.Switch(sceneA, nil)
		return core.Canvas{
			TestID: "stage",
			Style:  core.Style{Width: core.Pt(100), Height: core.Pt(80)},
			Draw:   func(d *core.Drawing, size core.Size) { mgr.Draw(d, size, 0) },
		}
	})

	app.Post(func() { mgr.Switch(sceneB, custom) })
	app.Settle()

	app.Advance(100 * time.Millisecond) // halfway through A -> B
	if len(seenProgress) < 2 || seenProgress[0] != 0 || math.Abs(seenProgress[len(seenProgress)-1]-0.5) > 1e-4 {
		t.Fatalf("unexpected custom transition progress samples: %v", seenProgress)
	}
	if seenSize != (core.Size{Width: 100, Height: 80}) {
		t.Fatalf("custom transition size = %+v, want 100x80", seenSize)
	}
	if aDisposed || bDisposed {
		t.Fatalf("unexpected early disposal: a=%v b=%v", aDisposed, bDisposed)
	}

	// Interrupt in-flight A -> B transition by switching immediately to C:
	// both A (outgoing) and B (current) must be disposed!
	app.Post(func() { mgr.Switch(sceneC, nil) })
	app.Settle()
	if !aDisposed || !bDisposed || mgr.Transitioning() || mgr.Current() != sceneC {
		t.Fatalf("after interrupt: aDisposed=%v bDisposed=%v transitioning=%v current=%v",
			aDisposed, bDisposed, mgr.Transitioning(), mgr.Current())
	}
}

func TestDisposingManagerParentScopeExitsStackedScenes(t *testing.T) {
	var exited []string
	var aTicks, bTicks int
	var shown *core.Signal[bool]

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(cx *core.Scope) core.Node {
				mgr := scene.NewManager(cx)
				mgr.Switch(&scene.Controller{
					Enter: func(cx *core.Scope) {
						l := &game.Loop{Rate: 60, Update: func(time.Duration) { aTicks++ }}
						l.Run(cx)
					},
					Exit: func() { exited = append(exited, "A") },
				}, nil)
				mgr.Push(&scene.Controller{
					Enter: func(cx *core.Scope) {
						l := &game.Loop{Rate: 60, Update: func(time.Duration) { bTicks++ }}
						l.Run(cx)
					},
					Exit: func() { exited = append(exited, "B") },
				})
				return core.View{}
			},
		}
	})

	app.Advance(100 * time.Millisecond)
	if aTicks == 0 || bTicks == 0 {
		t.Fatalf("expected active loops before disposal: a=%d b=%d", aTicks, bTicks)
	}

	app.Post(func() { shown.Set(false) })
	app.Settle()

	if !reflect.DeepEqual(exited, []string{"B", "A"}) {
		t.Fatalf("parent scope disposal exit order = %v, want [B A]", exited)
	}
	savedA, savedB := aTicks, bTicks
	app.Advance(100 * time.Millisecond)
	if aTicks != savedA || bTicks != savedB {
		t.Fatalf("loops kept running after parent scope disposal: A %d->%d, B %d->%d", savedA, aTicks, savedB, bTicks)
	}
}

func TestManagerStackAndLayerParallax(t *testing.T) {
	var mgr *scene.Manager
	a := &scene.Controller{Scene: scene.NewScene(nil)}
	b := &scene.Controller{Scene: scene.NewScene(nil), Transparent: true}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = scene.NewManager(cx)
		return core.View{}
	})
	if stack := mgr.Stack(); stack != nil {
		t.Fatalf("empty Stack = %v, want nil", stack)
	}
	app.Post(func() {
		mgr.Switch(a, nil)
		mgr.Push(b)
	})
	app.Settle()

	stack := mgr.Stack()
	if len(stack) != 2 || stack[0] != a || stack[1] != b {
		t.Fatalf("Stack = %v, want [a b]", stack)
	}
	// Mutating the returned slice must not affect the manager's stack.
	stack[0] = nil
	if mgr.Stack()[0] != a {
		t.Fatal("mutating Stack() slice affected Manager")
	}

	var nilMgr *scene.Manager
	if nilMgr.Stack() != nil {
		t.Fatal("nil Manager.Stack() should be nil")
	}

	lit := &scene.Layer{Name: "literal"}
	if px, py := lit.Parallax(); px != 1 || py != 1 {
		t.Fatalf("literal Layer.Parallax() = (%v, %v), want (1, 1)", px, py)
	}
	locked := scene.NewLayer("locked", scene.SpaceWorld, 0).SetParallax(0, 0.5)
	if px, py := locked.Parallax(); px != 0 || py != 0.5 {
		t.Fatalf("SetParallax(0, 0.5).Parallax() = (%v, %v), want (0, 0.5)", px, py)
	}
	var nilLayer *scene.Layer
	if px, py := nilLayer.Parallax(); px != 1 || py != 1 {
		t.Fatalf("nil Layer.Parallax() = (%v, %v), want (1, 1)", px, py)
	}
}

package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func solidSpriteImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func coreColorToRGBA(c core.Color) color.RGBA {
	if c == core.Transparent {
		return color.RGBA{}
	}
	return color.RGBA{
		R: uint8(c >> 24 & 0xff),
		G: uint8(c >> 16 & 0xff),
		B: uint8(c >> 8 & 0xff),
		A: uint8(c & 0xff),
	}
}

func decodeBase64PNG(t *testing.T, b64 string) *image.RGBA {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return decodePNGBytes(t, raw)
}

func decodePNGBytes(t *testing.T, raw []byte) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			rgba.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return rgba
}

func assertPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, label string) {
	t.Helper()
	got := img.RGBAAt(x, y)
	if got != want {
		t.Errorf("%s pixel (%d,%d) = %+v, want %+v", label, x, y, got, want)
	}
}

func TestScreenshotBaseAndAlignedOverlays(t *testing.T) {
	bgColor := core.RGB(0x101820)
	bgRGBA := coreColorToRGBA(bgColor)
	heroRGBA := color.RGBA{R: 220, G: 40, B: 50, A: 255}
	ballRGBA := color.RGBA{R: 40, G: 90, B: 230, A: 255}
	hiddenRGBA := color.RGBA{R: 250, G: 250, B: 10, A: 255}

	heroAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(16, 16, heroRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
	ballAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(20, 20, ballRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"ball": {Name: "ball", X: 0, Y: 0, W: 20, H: 20, Width: 20, Height: 20},
		},
	}
	hiddenAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(8, 8, hiddenRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"secret": {Name: "secret", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		},
	}

	hero := sprite.NewSprite(heroAtlas.Frames["hero"])
	hero.X, hero.Y = 10, 20 // occupies pixels x=[10..25], y=[20..35]

	ball := sprite.NewSprite(ballAtlas.Frames["ball"])
	ball.X, ball.Y = 60, 30 // occupies pixels x=[60..79], y=[30..49]

	secret := sprite.NewSprite(hiddenAtlas.Frames["secret"])
	secret.X, secret.Y = 110, 4

	sc := scene.NewScene(nil)
	actors := scene.NewLayer("actors", scene.SpaceWorld, 0)
	actors.AddBatch(&sprite.Batch{Atlas: heroAtlas, Sprites: []*sprite.Sprite{&hero}})
	actors.AddBatch(&sprite.Batch{Atlas: ballAtlas, Sprites: []*sprite.Sprite{&ball}})
	sc.AddLayer(actors)

	hiddenLayer := scene.NewLayer("hidden", scene.SpaceWorld, 10)
	hiddenLayer.Visible = false
	hiddenLayer.AddBatch(&sprite.Batch{Atlas: hiddenAtlas, Sprites: []*sprite.Sprite{&secret}})
	sc.AddLayer(hiddenLayer)

	// Physics world with inset hero box, ball circle, trigger, and tile grid.
	grid := physics.NewGrid(8, 4, 16, 16)
	grid.Set(0, 3, physics.TileSolid)  // pixels x=[0..15], y=[48..63]
	grid.Set(1, 3, physics.TileOneWay) // top edge at y=48, x=[16..31]
	pw := &physics.World{Tiles: grid}
	pw.Add(
		&physics.Body{
			Tag:   "hero_box",
			Shape: physics.BoxShape(physics.Box{X: 12, Y: 22, W: 12, H: 12}), // pixels x=[12..23], y=[22..33]
		},
		&physics.Body{
			Tag:   "ball_col",
			Shape: physics.CircleShape(physics.Circle{X: 70, Y: 40, R: 10}), // bounds x=[60..79], y=[30..49]
		},
	)
	pw.AddTrigger(&physics.Trigger{
		Tag:   "goal",
		Shape: physics.BoxShape(physics.Box{X: 90, Y: 10, W: 12, H: 10}), // pixels x=[90..101], y=[10..19]
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Scene:                sc,
			Physics:              pw,
			ScreenshotBackground: bgColor,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// 1. Base screenshot without overlays.
	baseRes := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"width":  128,
		"height": 64,
	})
	if baseRes.Width != 128 || baseRes.Height != 64 || baseRes.Format != "png" || baseRes.Bytes <= 0 || baseRes.PNGBase64 == "" {
		t.Fatalf("unexpected base screenshot metadata: %+v", baseRes)
	}
	if baseRes.OverlaysDrawn.Bounds || baseRes.OverlaysDrawn.CollisionShapes {
		t.Fatalf("expected no overlays drawn, got %+v", baseRes.OverlaysDrawn)
	}
	baseImg := decodeBase64PNG(t, baseRes.PNGBase64)
	if baseImg.Bounds() != image.Rect(0, 0, 128, 64) {
		t.Fatalf("decoded bounds = %v, want 128x64", baseImg.Bounds())
	}
	assertPixel(t, baseImg, 10, 20, heroRGBA, "base hero top-left")
	assertPixel(t, baseImg, 25, 35, heroRGBA, "base hero bottom-right")
	assertPixel(t, baseImg, 18, 28, heroRGBA, "base hero interior")
	assertPixel(t, baseImg, 9, 20, bgRGBA, "base outside hero left")
	assertPixel(t, baseImg, 26, 20, bgRGBA, "base outside hero right")
	assertPixel(t, baseImg, 60, 30, ballRGBA, "base ball top-left")
	assertPixel(t, baseImg, 79, 49, ballRGBA, "base ball bottom-right")
	assertPixel(t, baseImg, 112, 6, bgRGBA, "hidden layer sprite must not draw")

	// 2. Screenshot with sprite bounds + collision shapes enabled.
	ovRes := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"width":            128,
		"height":           64,
		"bounds":           true,
		"collision_shapes": true,
	})
	if !ovRes.OverlaysDrawn.Bounds || !ovRes.OverlaysDrawn.CollisionShapes {
		t.Fatalf("expected bounds and collision_shapes overlays, got %+v", ovRes.OverlaysDrawn)
	}
	if ovRes.OverlaysDrawn.SpriteBounds != 2 || ovRes.OverlaysDrawn.PhysicsBoxes != 1 || ovRes.OverlaysDrawn.PhysicsCircles != 1 || ovRes.OverlaysDrawn.PhysicsTriggers != 1 || ovRes.OverlaysDrawn.PhysicsTiles != 2 {
		t.Fatalf("unexpected overlay counts: %+v", ovRes.OverlaysDrawn)
	}

	style := mcp.DefaultOverlayStyle()
	boundsCol := coreColorToRGBA(style.BoundsColor)
	boxCol := coreColorToRGBA(style.BoxColor)
	circleCol := coreColorToRGBA(style.CircleColor)
	triggerCol := coreColorToRGBA(style.TriggerColor)
	solidTileCol := coreColorToRGBA(style.SolidTileColor)
	oneWayCol := coreColorToRGBA(style.OneWayTileColor)

	ovImg := decodeBase64PNG(t, ovRes.PNGBase64)

	// Hero sprite bounds border aligned exactly with x=[10..25], y=[20..35].
	for _, pt := range [][2]int{{10, 20}, {25, 20}, {10, 35}, {25, 35}, {17, 20}, {17, 35}, {10, 28}, {25, 28}} {
		assertPixel(t, ovImg, pt[0], pt[1], boundsCol, "hero bounds border")
	}
	// Immediately outside hero bounds remains untouched background.
	for _, pt := range [][2]int{{9, 20}, {26, 20}, {10, 19}, {10, 36}} {
		assertPixel(t, ovImg, pt[0], pt[1], bgRGBA, "outside hero bounds")
	}
	// Band between hero sprite bounds and inset physics box remains hero red.
	assertPixel(t, ovImg, 11, 21, heroRGBA, "hero sprite band inside bounds")
	assertPixel(t, ovImg, 24, 34, heroRGBA, "hero sprite band bottom-right")

	// Hero physics box border aligned exactly with x=[12..23], y=[22..33].
	for _, pt := range [][2]int{{12, 22}, {23, 22}, {12, 33}, {23, 33}, {17, 22}, {12, 28}} {
		assertPixel(t, ovImg, pt[0], pt[1], boxCol, "hero physics box border")
	}
	// Interior of hero physics box remains hero red.
	assertPixel(t, ovImg, 18, 28, heroRGBA, "hero physics box interior")

	// Ball sprite bounds corners aligned at x=[60..79], y=[30..49].
	for _, pt := range [][2]int{{60, 30}, {79, 30}, {60, 49}, {79, 49}} {
		assertPixel(t, ovImg, pt[0], pt[1], boundsCol, "ball bounds corner")
	}
	// Ball circle collider cardinal points and +X orientation tick aligned with ball bounds.
	for _, pt := range [][2]int{{79, 40}, {60, 40}, {70, 30}, {70, 49}, {74, 40}} {
		assertPixel(t, ovImg, pt[0], pt[1], circleCol, "ball circle collider")
	}
	// Uncovered interior of ball stays blue.
	assertPixel(t, ovImg, 65, 35, ballRGBA, "ball interior")

	// Trigger box at x=[90..101], y=[10..19].
	for _, pt := range [][2]int{{90, 10}, {101, 10}, {90, 19}, {101, 19}} {
		assertPixel(t, ovImg, pt[0], pt[1], triggerCol, "trigger border")
	}
	assertPixel(t, ovImg, 95, 15, bgRGBA, "trigger interior")

	// Solid tile at x=[0..15], y=[48..63] and one-way tile top edge at y=48, x=[16..31].
	assertPixel(t, ovImg, 0, 48, solidTileCol, "solid tile top-left")
	assertPixel(t, ovImg, 15, 63, solidTileCol, "solid tile bottom-right")
	assertPixel(t, ovImg, 7, 55, bgRGBA, "solid tile interior")
	assertPixel(t, ovImg, 16, 48, oneWayCol, "one-way tile top-left")
	assertPixel(t, ovImg, 24, 48, oneWayCol, "one-way tile top-mid")
	assertPixel(t, ovImg, 31, 48, oneWayCol, "one-way tile top-right")
	assertPixel(t, ovImg, 32, 48, bgRGBA, "outside one-way tile right")
}

func TestScreenshotCameraPanZoomAndViewportAlignment(t *testing.T) {
	bgColor := core.RGB(0x182028)
	bgRGBA := coreColorToRGBA(bgColor)
	barColor := core.RGB(0x111111)
	barRGBA := coreColorToRGBA(barColor)
	actorRGBA := color.RGBA{R: 200, G: 70, B: 40, A: 255}
	hudRGBA := color.RGBA{R: 180, G: 40, B: 200, A: 255}

	actorAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(16, 12, actorRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"player": {Name: "player", X: 0, Y: 0, W: 16, H: 12, Width: 16, Height: 12},
		},
	}
	hudAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(8, 8, hudRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"heart": {Name: "heart", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		},
	}

	// Virtual canvas 160x90 rendered into 320x240 via ModeFit:
	// Scale = 2, letterbox bars top y=[0..29] and bottom y=[210..239], virtual rect origin = (0, 30).
	vp := &screen.Viewport{
		Width:    160,
		Height:   90,
		Mode:     screen.ModeFit,
		BarColor: barColor,
	}

	// Camera centered at world (50, 40) with Zoom=2 on virtual 160x90:
	// WorldToView(wx, wy) = (80 + (wx - 50)*2, 45 + (wy - 40)*2)
	// ViewToScreen(vx, vy) = (vx*2, 30 + vy*2)
	cam := &tilemap.Camera{X: 50, Y: 40, Zoom: 2, Width: 160, Height: 90}
	sc := scene.NewScene(cam)

	// World sprite at (42, 34) size 16x12:
	// Virtual: [64, 96) x [33, 57) -> Screen: [128, 192) x [96, 144) -> pixels x=[128..191], y=[96..143].
	sp := sprite.NewSprite(actorAtlas.Frames["player"])
	sp.X, sp.Y = 42, 34
	worldLayer := scene.NewLayer("world", scene.SpaceWorld, 0)
	worldLayer.AddBatch(&sprite.Batch{Atlas: actorAtlas, Sprites: []*sprite.Sprite{&sp}})
	sc.AddLayer(worldLayer)

	// HUD sprite in screen space at (4, 4) size 8x8:
	// Unaffected by camera; under viewport -> Screen: [8, 24) x [38, 54) -> pixels x=[8..23], y=[38..53].
	hudSp := sprite.NewSprite(hudAtlas.Frames["heart"])
	hudSp.X, hudSp.Y = 4, 4
	hudLayer := scene.NewLayer("hud", scene.SpaceScreen, 10)
	hudLayer.AddBatch(&sprite.Batch{Atlas: hudAtlas, Sprites: []*sprite.Sprite{&hudSp}})
	sc.AddLayer(hudLayer)

	// Inset physics box at (44, 36) size 12x8:
	// Virtual: [68, 92) x [37, 53) -> Screen: [136, 184) x [104, 136) -> pixels x=[136..183], y=[104..135].
	pw := &physics.World{}
	pw.Add(&physics.Body{
		Tag:   "player",
		Shape: physics.BoxShape(physics.Box{X: 44, Y: 36, W: 12, H: 8}),
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Scene:                sc,
			Physics:              pw,
			Viewport:             vp,
			ScreenshotBackground: bgColor,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	res := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"width":  320,
		"height": 240,
		"overlays": map[string]any{
			"bounds":  true,
			"physics": true,
		},
	})
	if res.Width != 320 || res.Height != 240 {
		t.Fatalf("size = %dx%d, want 320x240", res.Width, res.Height)
	}
	if res.OverlaysDrawn.SpriteBounds != 2 || res.OverlaysDrawn.PhysicsBoxes != 1 {
		t.Fatalf("overlays_drawn = %+v", res.OverlaysDrawn)
	}

	style := mcp.DefaultOverlayStyle()
	boundsCol := coreColorToRGBA(style.BoundsColor)
	boxCol := coreColorToRGBA(style.BoxColor)
	img := decodeBase64PNG(t, res.PNGBase64)

	// Letterbox bars remain intact.
	assertPixel(t, img, 10, 10, barRGBA, "top letterbox bar")
	assertPixel(t, img, 300, 225, barRGBA, "bottom letterbox bar")

	// World sprite + bounds outline aligned at exact zoomed/panned/letterboxed screen coords.
	for _, pt := range [][2]int{{128, 96}, {191, 96}, {128, 143}, {191, 143}} {
		assertPixel(t, img, pt[0], pt[1], boundsCol, "transformed world sprite bounds")
	}
	assertPixel(t, img, 127, 96, bgRGBA, "outside transformed sprite left")
	assertPixel(t, img, 192, 96, bgRGBA, "outside transformed sprite right")
	assertPixel(t, img, 130, 98, actorRGBA, "transformed sprite pixel inside bounds")

	// Physics box aligned inside transformed world sprite at x=[136..183], y=[104..135].
	for _, pt := range [][2]int{{136, 104}, {183, 104}, {136, 135}, {183, 135}} {
		assertPixel(t, img, pt[0], pt[1], boxCol, "transformed physics box")
	}
	assertPixel(t, img, 160, 120, actorRGBA, "transformed physics box interior")

	// SpaceScreen HUD sprite + bounds aligned at x=[8..23], y=[38..53].
	for _, pt := range [][2]int{{8, 38}, {23, 38}, {8, 53}, {23, 53}} {
		assertPixel(t, img, pt[0], pt[1], boundsCol, "screen-space HUD sprite bounds")
	}
	assertPixel(t, img, 15, 45, hudRGBA, "screen-space HUD sprite interior")

	// Also verify grid, camera and labels overlays execute cleanly and draw within the viewport clip.
	fullRes := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"width":  320,
		"height": 240,
		"overlays": map[string]any{
			"bounds":    true,
			"physics":   true,
			"grid":      true,
			"grid_step": 16,
			"camera":    true,
			"labels":    true,
		},
	})
	if !fullRes.OverlaysDrawn.Grid || !fullRes.OverlaysDrawn.Camera || !fullRes.OverlaysDrawn.Labels {
		t.Fatalf("expected grid/camera/labels flags set: %+v", fullRes.OverlaysDrawn)
	}
	fullImg := decodeBase64PNG(t, fullRes.PNGBase64)
	// Letterbox bar must still be completely untouched (clipped by viewport).
	assertPixel(t, fullImg, 160, 5, barRGBA, "top letterbox bar clipped against grid/camera/labels")
	// Camera crosshair at center of virtual rect (160, 120).
	assertPixel(t, fullImg, 160, 120, coreColorToRGBA(style.CameraColor), "camera center crosshair")
}

func TestScreenshotParallaxScaledOriginAndSceneManager(t *testing.T) {
	bgColor := core.RGB(0x0c1018)
	cloudRGBA := color.RGBA{R: 190, G: 210, B: 240, A: 255}
	scaledRGBA := color.RGBA{R: 230, G: 140, B: 40, A: 255}
	titleRGBA := color.RGBA{R: 90, G: 200, B: 110, A: 255}

	cloudAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(10, 10, cloudRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"cloud": {Name: "cloud", X: 0, Y: 0, W: 10, H: 10, Width: 10, Height: 10},
		},
	}
	scaledAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(8, 8, scaledRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"crate": {Name: "crate", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8, PivotX: 4, PivotY: 4},
		},
	}
	titleAtlas := &sprite.Atlas{
		Image: core.ImageSource{Pixels: solidSpriteImage(12, 6, titleRGBA), Version: 1},
		Frames: map[string]sprite.Frame{
			"logo": {Name: "logo", X: 0, Y: 0, W: 12, H: 6, Width: 12, Height: 6},
		},
	}

	// Camera at (40, 20) in a 100x80 view: center=(50, 40).
	cam := &tilemap.Camera{X: 40, Y: 20, Zoom: 1, Width: 100, Height: 80}
	gameScene := scene.NewScene(cam)

	// Parallax layer (0.5, 0.5) with Offset (6, 4):
	// pcam.X = 40*0.5 - 6 = 14, pcam.Y = 20*0.5 - 4 = 6.
	// Screen offset = (50 - 14, 40 - 6) = (36, 34).
	// Cloud at (0, 0) size 10x10 -> screen [36..45] x [34..43].
	parallaxLayer := scene.NewParallaxLayer("clouds", -5, 0.5, 0.5)
	parallaxLayer.OffsetX, parallaxLayer.OffsetY = 6, 4
	cloud := sprite.NewSprite(cloudAtlas.Frames["cloud"])
	cloud.X, cloud.Y = 0, 0
	parallaxLayer.AddBatch(&sprite.Batch{Atlas: cloudAtlas, Sprites: []*sprite.Sprite{&cloud}})
	gameScene.AddLayer(parallaxLayer)

	// World layer with centered-origin 2x-scaled sprite at (40, 20):
	// Bounds in world = [40 - 8, 40 + 8) x [20 - 8, 20 + 8) = [32, 48) x [12, 28).
	// Under cam (40, 20) -> screen [42..57] x [32..47].
	worldLayer := scene.NewLayer("actors", scene.SpaceWorld, 0)
	crate := sprite.NewSprite(scaledAtlas.Frames["crate"])
	crate.X, crate.Y = 40, 20
	crate.ScaleX, crate.ScaleY = 2, 2
	worldLayer.AddBatch(&sprite.Batch{Atlas: scaledAtlas, Sprites: []*sprite.Sprite{&crate}})
	gameScene.AddLayer(worldLayer)

	// Transparent pause overlay scene on top of gameScene.
	pauseScene := scene.NewScene(nil)
	pauseLayer := scene.NewLayer("pause_hud", scene.SpaceScreen, 0)
	pauseScene.AddLayer(pauseLayer)

	// Inactive named scene "title".
	titleScene := scene.NewScene(nil)
	titleLayer := scene.NewLayer("title_ui", scene.SpaceScreen, 0)
	logo := sprite.NewSprite(titleAtlas.Frames["logo"])
	logo.X, logo.Y = 10, 10 // screen [10..21] x [10..15]
	titleLayer.AddBatch(&sprite.Batch{Atlas: titleAtlas, Sprites: []*sprite.Sprite{&logo}})
	titleScene.AddLayer(titleLayer)

	gameCtrl := &scene.Controller{Scene: gameScene}
	pauseCtrl := &scene.Controller{Scene: pauseScene, Transparent: true}
	names := map[*scene.Controller]string{gameCtrl: "gameplay", pauseCtrl: "pause"}

	pw := &physics.World{}
	pw.Add(&physics.Body{Shape: physics.BoxShape(physics.Box{X: 34, Y: 14, W: 12, H: 12})}) // screen [44..55] x [34..45]

	var (
		cx  *core.Scope
		mgr *scene.Manager
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		mgr = scene.NewManager(cx)
		mgr.Switch(gameCtrl, nil)
		mgr.Push(pauseCtrl)
		if _, err := mcp.Attach(cx, mcp.Config{
			Scenes:               mgr,
			SceneName:            func(c *scene.Controller) string { return names[c] },
			NamedScenes:          map[string]*scene.Scene{"title": titleScene},
			PhysicsFunc:          func() *physics.World { return pw },
			ScreenshotBackground: bgColor,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Capturing active transparent stack draws underlying gameplay scene + parallax + scaled sprite + physics box.
	res := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"bounds":           true,
		"collision_shapes": true,
	})
	if res.Width != 100 || res.Height != 80 || res.Scene != "pause" {
		t.Fatalf("transparent stack result = %+v", res)
	}
	if res.OverlaysDrawn.SpriteBounds != 2 || res.OverlaysDrawn.PhysicsBoxes != 1 {
		t.Fatalf("overlays_drawn = %+v", res.OverlaysDrawn)
	}
	style := mcp.DefaultOverlayStyle()
	boundsCol := coreColorToRGBA(style.BoundsColor)
	boxCol := coreColorToRGBA(style.BoxColor)
	img := decodeBase64PNG(t, res.PNGBase64)

	// Parallax cloud bounds at [36..45] x [34..43].
	assertPixel(t, img, 36, 34, boundsCol, "parallax cloud top-left")
	assertPixel(t, img, 36, 43, boundsCol, "parallax cloud bottom-left")
	assertPixel(t, img, 39, 39, cloudRGBA, "parallax cloud interior")

	// Centered-origin 2x-scaled crate bounds at [42..57] x [32..47], with inset physics box at [44..55] x [34..45].
	for _, pt := range [][2]int{{42, 32}, {57, 32}, {42, 47}, {57, 47}} {
		assertPixel(t, img, pt[0], pt[1], boundsCol, "scaled crate bounds")
	}
	for _, pt := range [][2]int{{44, 34}, {55, 34}, {44, 45}, {55, 45}} {
		assertPixel(t, img, pt[0], pt[1], boxCol, "crate physics box")
	}
	assertPixel(t, img, 50, 40, scaledRGBA, "crate interior")

	// Capturing an inactive named scene directly via "scene": "title".
	titleRes := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"scene":  "title",
		"width":  40,
		"height": 30,
		"bounds": true,
	})
	if titleRes.Scene != "title" || titleRes.OverlaysDrawn.SpriteBounds != 1 {
		t.Fatalf("named scene capture = %+v", titleRes)
	}
	titleImg := decodeBase64PNG(t, titleRes.PNGBase64)
	assertPixel(t, titleImg, 10, 10, boundsCol, "title logo top-left bounds")
	assertPixel(t, titleImg, 21, 15, boundsCol, "title logo bottom-right bounds")
	assertPixel(t, titleImg, 15, 12, titleRGBA, "title logo interior")
}

func TestScreenshotCaptureFrameDrawFrameAndDiskOutput(t *testing.T) {
	// 1. CaptureFrame: overlays composite onto cloned frame without mutating caller's image.
	srcImg := solidSpriteImage(80, 60, color.RGBA{R: 30, G: 40, B: 50, A: 255})
	pw := &physics.World{}
	pw.Add(&physics.Body{Shape: physics.BoxShape(physics.Box{X: 10, Y: 10, W: 20, H: 15})})

	shotsDir := t.TempDir()
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Physics: pw,
			CaptureFrame: func(context.Context) (*image.RGBA, error) {
				return srcImg, nil
			},
			ScreenshotDir: shotsDir,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Dimensions are inferred automatically from CaptureFrame; path writes PNG inside ScreenshotDir.
	diskRes := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"collision_shapes": true,
		"path":             "nested/frame1.png",
	})
	if diskRes.Width != 80 || diskRes.Height != 60 || diskRes.Path != "nested/frame1.png" || diskRes.PNGBase64 != "" {
		t.Fatalf("disk screenshot result = %+v", diskRes)
	}
	onDisk, err := os.ReadFile(filepath.Join(shotsDir, "nested", "frame1.png"))
	if err != nil {
		t.Fatalf("read written screenshot: %v", err)
	}
	if len(onDisk) != diskRes.Bytes {
		t.Fatalf("file bytes = %d, reported = %d", len(onDisk), diskRes.Bytes)
	}
	diskImg := decodePNGBytes(t, onDisk)
	boxCol := coreColorToRGBA(mcp.DefaultOverlayStyle().BoxColor)
	assertPixel(t, diskImg, 10, 10, boxCol, "CaptureFrame overlay box top-left")
	assertPixel(t, diskImg, 29, 24, boxCol, "CaptureFrame overlay box bottom-right")
	// Source image returned by CaptureFrame must NOT be mutated.
	assertPixel(t, srcImg, 10, 10, color.RGBA{R: 30, G: 40, B: 50, A: 255}, "CaptureFrame source image unmodified")

	// 2. DrawFrame + RigidPhysics (rotated box and circle).
	rw := &physics.RigidWorld{}
	rb := physics.NewRigidBox(32, 24, 16, 10, 1)
	rb.Angle = math.Pi / 4
	rc := physics.NewRigidCircle(60, 24, 8, 1)
	rw.AddBody(rb, rc)

	var cx2 *core.Scope
	app2 := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx2 = scope
		if _, err := mcp.Attach(cx2, mcp.Config{
			RigidPhysics: rw,
			DrawFrame: func(d *core.Drawing, w, h int) {
				d.SetFillColor(core.RGB(0x202020)).FillRect(0, 0, float32(w), float32(h))
			},
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	rigidRes := callTool[mcp.ScreenshotResult](t, app2, cx2, mcp.ToolScreenshot, map[string]any{
		"width":            96,
		"height":           48,
		"collision_shapes": true,
	})
	if rigidRes.OverlaysDrawn.PhysicsBoxes != 1 || rigidRes.OverlaysDrawn.PhysicsCircles != 1 {
		t.Fatalf("rigid overlays = %+v", rigidRes.OverlaysDrawn)
	}
	rigidImg := decodeBase64PNG(t, rigidRes.PNGBase64)
	circleCol := coreColorToRGBA(mcp.DefaultOverlayStyle().CircleColor)
	assertPixel(t, rigidImg, 67, 24, circleCol, "rigid circle +X edge")
	assertPixel(t, rigidImg, 52, 24, circleCol, "rigid circle -X edge")
}

func TestScreenshotValidationAndGracefulNilHandling(t *testing.T) {
	sc := scene.NewScene(&tilemap.Camera{X: 32, Y: 24, Width: 64, Height: 48})
	shotsDir := t.TempDir()

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			Scene:         sc,
			ScreenshotDir: shotsDir,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Graceful nil Physics: collision_shapes: true succeeds with 0 shapes drawn, dimensions inferred from Camera.
	graceful := callTool[mcp.ScreenshotResult](t, app, cx, mcp.ToolScreenshot, map[string]any{
		"bounds":           true,
		"collision_shapes": true,
	})
	if graceful.Width != 64 || graceful.Height != 48 || graceful.OverlaysDrawn.PhysicsBoxes != 0 || graceful.OverlaysDrawn.SpriteBounds != 0 {
		t.Fatalf("graceful nil physics/empty scene = %+v", graceful)
	}

	// Unknown top-level and nested fields refused.
	if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, `{"bogus":true}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown top-level field = %v, want unknown field error", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, `{"overlays":{"bogus":true}}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown nested field = %v, want unknown field error", err)
	}

	// Unsupported format refused.
	if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, map[string]any{"format": "jpeg"}); err == nil || !strings.Contains(err.Error(), "unsupported screenshot format") {
		t.Fatalf("bad format = %v", err)
	}

	// Invalid dimensions refused.
	for _, bad := range []map[string]any{
		{"width": -10, "height": 10},
		{"width": 10, "height": -10},
		{"width": 64},
		{"height": 48},
		{"width": 9000, "height": 9000},
	} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, bad); err == nil {
			t.Errorf("expected error for bad dimensions %v", bad)
		}
	}

	// Negative grid_step refused.
	if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, map[string]any{
		"overlays": map[string]any{"grid": true, "grid_step": -8},
	}); err == nil {
		t.Fatal("expected error for negative grid_step")
	}

	// Unknown scene name refused.
	if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, map[string]any{"scene": "does_not_exist"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown scene = %v", err)
	}

	// Path traversal and unsafe paths refused.
	for _, badPath := range []string{"../escape.png", "a/../../escape.png", "/tmp/abs.png", ".", "..", `sub\win.png`, "C:shot.png"} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolScreenshot, map[string]any{"path": badPath}); err == nil {
			t.Errorf("expected error for unsafe path %q", badPath)
		}
	}

	// Unconfigured bridge (no ScreenshotDir, no Scene/Physics/DrawFrame/CaptureFrame).
	var cxEmpty *core.Scope
	appEmpty := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cxEmpty = scope
		if _, err := mcp.Attach(cxEmpty, mcp.Config{}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	if _, err := callToolRaw(t, appEmpty, cxEmpty, mcp.ToolScreenshot, map[string]any{"width": 32, "height": 32, "path": "a.png"}); err == nil || !strings.Contains(err.Error(), "ScreenshotDir") {
		t.Fatalf("path without ScreenshotDir = %v", err)
	}
	if _, err := callToolRaw(t, appEmpty, cxEmpty, mcp.ToolScreenshot, nil); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("no dimensions = %v", err)
	}
	if _, err := callToolRaw(t, appEmpty, cxEmpty, mcp.ToolScreenshot, map[string]any{"width": 32, "height": 32}); err == nil || !strings.Contains(err.Error(), "no screenshot source") {
		t.Fatalf("no source = %v", err)
	}
}

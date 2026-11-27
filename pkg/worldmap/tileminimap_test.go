package worldmap_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/game/pkg/worldmap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func solidTileSheet(tileW, tileH int, tiles ...color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, tileW*len(tiles), tileH))
	for i, c := range tiles {
		for y := range tileH {
			for x := range tileW {
				img.SetRGBA(i*tileW+x, y, c)
			}
		}
	}
	return img
}

func makeOrthoTestMap() *tilemap.Tilemap {
	// 3 tiles:
	// GID 1 (local 0): green grass from tileset image (no property)
	// GID 2 (local 1): blue water in tileset image, overridden by minimap_color property to bright cyan
	// GID 3 (local 2): half red (200,0,0) / half blue (0,0,100) tileset image -> average (100,0,50)
	sheet := solidTileSheet(8, 8,
		color.RGBA{R: 0x20, G: 0xa0, B: 0x30, A: 0xff},
		color.RGBA{R: 0x00, G: 0x00, B: 0x40, A: 0xff},
		color.RGBA{R: 200, G: 0, B: 0, A: 0xff},
	)
	// Make right half of tile 2 blue (0, 0, 100) so average is (100, 0, 50).
	for y := range 8 {
		for x := 4; x < 8; x++ {
			sheet.SetRGBA(16+x, y, color.RGBA{R: 0, G: 0, B: 100, A: 0xff})
		}
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet}, 8, 8, 3, 3)
	ts.Name = "terrain"
	ts.TileProperties = map[uint32]tilemap.Properties{
		1: {
			worldmap.DefaultTileColorProperty: {Name: worldmap.DefaultTileColorProperty, Type: "color", Value: "#00ccff"},
		},
	}

	m := tilemap.New(4, 3, 16, 16, ts)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 4, 3, []uint32{
		1, 2, 3, 0,
		1, 1, 2, 3 | tilemap.FlipHorizontal,
		0, 2, 1, 1,
	}))
	return m
}

func TestTileMinimapColorResolutionAndLayers(t *testing.T) {
	m := makeOrthoTestMap()
	// Add a semi-transparent overlay layer on top of (0,0).
	overlaySheet := solidTileSheet(8, 8, color.RGBA{R: 200, G: 0, B: 0, A: 255})
	ts2 := tilemap.NewGridTileset(10, core.ImageSource{Pixels: overlaySheet}, 8, 8, 1, 1)
	m.Tilesets = append(m.Tilesets, ts2)
	top := tilemap.NewTileLayer("decor", 4, 3, nil)
	top.Opacity = 0.5
	m.Layers = append(m.Layers, top)
	m.SetTile(1, 0, 0, 10)

	// Add a hidden layer that must be ignored.
	hidden := tilemap.NewTileLayer("hidden", 4, 3, nil)
	hidden.Visible = false
	m.Layers = append(m.Layers, hidden)
	m.SetTile(2, 1, 0, 10)

	tm := worldmap.NewTileMinimap(m, nil)
	img := tm.TerrainImage()
	if img == nil || img.Bounds() != image.Rect(0, 0, 4, 3) {
		t.Fatalf("TerrainImage bounds = %v, want (0,0)-(4,3)", img)
	}

	// (1,0) uses minimap_color property #00ccff (overrides dark blue image; hidden layer ignored).
	if got, want := img.RGBAAt(1, 0), (color.RGBA{R: 0x00, G: 0xcc, B: 0xff, A: 0xff}); got != want {
		t.Errorf("property tile (1,0) = %v, want %v", got, want)
	}
	// (2,0) averages half (200,0,0) and half (0,0,100) -> (100,0,50).
	if got, want := img.RGBAAt(2, 0), (color.RGBA{R: 100, G: 0, B: 50, A: 0xff}); got != want {
		t.Errorf("averaged tile (2,0) = %v, want %v", got, want)
	}
	// (3,1) is GID 3 with FlipHorizontal bit set -> same average color.
	if got, want := img.RGBAAt(3, 1), (color.RGBA{R: 100, G: 0, B: 50, A: 0xff}); got != want {
		t.Errorf("flipped tile (3,1) = %v, want %v", got, want)
	}
	// (3,0) is empty -> transparent.
	if got := img.RGBAAt(3, 0); got.A != 0 {
		t.Errorf("empty tile (3,0) = %v, want transparent", got)
	}
	// (0,0) blends 50% red (200,0,0) over green (0x20,0xa0,0x30).
	got00 := img.RGBAAt(0, 0)
	if got00.R < 105 || got00.R > 125 || got00.G < 70 || got00.G > 90 || got00.A != 255 {
		t.Errorf("composited layer cell (0,0) = %v, want ~50%% red over green", got00)
	}

	// Custom ColorProperty and Images map (URI + PNG Data + Atlas frames).
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, solidTileSheet(4, 4, color.RGBA{R: 0xaa, G: 0x55, B: 0x11, A: 0xff})); err != nil {
		t.Fatal(err)
	}
	atlasImg := solidTileSheet(4, 4, color.RGBA{R: 0x11, G: 0xbb, B: 0x22, A: 0xff})
	atlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:atlas.png"},
		Frames: map[string]sprite.Frame{
			"stone": {X: 0, Y: 0, W: 4, H: 4, Width: 4, Height: 4},
		},
	}
	tsPNG := tilemap.NewGridTileset(1, core.ImageSource{Data: pngBuf.Bytes()}, 4, 4, 1, 1)
	tsAtlas := tilemap.NewAtlasTileset(2, atlas, "stone")
	m2 := tilemap.New(2, 1, 16, 16, tsPNG, tsAtlas)
	m2.Layers = append(m2.Layers, tilemap.NewTileLayer("base", 2, 1, []uint32{1, 2}))
	tm2 := worldmap.NewTileMinimap(m2, &worldmap.TileMinimapOptions{
		Images: map[string]image.Image{"atlas.png": atlasImg},
	})
	if got, want := tm2.TerrainImage().RGBAAt(0, 0), (color.RGBA{R: 0xaa, G: 0x55, B: 0x11, A: 0xff}); got != want {
		t.Errorf("PNG Data tileset pixel = %v, want %v", got, want)
	}
	if got, want := tm2.TerrainImage().RGBAAt(1, 0), (color.RGBA{R: 0x11, G: 0xbb, B: 0x22, A: 0xff}); got != want {
		t.Errorf("Atlas URI tileset pixel = %v, want %v", got, want)
	}
}

func TestTileMinimapIncrementalSetTileUpdate(t *testing.T) {
	m := makeOrthoTestMap()
	tm := worldmap.NewTileMinimap(m, nil)

	src0 := tm.Sync(nil, 0, nil)
	if got := tm.TerrainImage().RGBAAt(0, 0); got != (color.RGBA{R: 0x20, G: 0xa0, B: 0x30, A: 0xff}) {
		t.Fatalf("initial (0,0) = %v, want green", got)
	}

	// Unchanged sync keeps Version identical.
	src1 := tm.Sync(nil, 0, nil)
	if src1.Version != src0.Version {
		t.Fatalf("unchanged Sync changed Version from %d to %d", src0.Version, src1.Version)
	}

	// Single SetTile updates only that cell and increments Version.
	m.SetTile(0, 0, 0, 2) // cyan property color #00ccff
	src2 := tm.Sync(nil, 0, nil)
	if src2.Version <= src1.Version {
		t.Fatalf("SetTile did not advance Version: %d -> %d", src1.Version, src2.Version)
	}
	if got, want := tm.TerrainImage().RGBAAt(0, 0), (color.RGBA{R: 0x00, G: 0xcc, B: 0xff, A: 0xff}); got != want {
		t.Fatalf("updated (0,0) = %v, want %v", got, want)
	}
	// Neighboring cell untouched.
	if got, want := tm.TerrainImage().RGBAAt(0, 1), (color.RGBA{R: 0x20, G: 0xa0, B: 0x30, A: 0xff}); got != want {
		t.Fatalf("neighbor (0,1) = %v, want %v", got, want)
	}

	// Invalidate forces refresh after direct layer.Data mutation.
	m.Layers[0].Data[0] = 3
	tm.Invalidate()
	src3 := tm.Sync(nil, 0, nil)
	if src3.Version <= src2.Version {
		t.Fatalf("Invalidate did not advance Version")
	}
	if got, want := tm.TerrainImage().RGBAAt(0, 0), (color.RGBA{R: 100, G: 0, B: 50, A: 0xff}); got != want {
		t.Fatalf("after Invalidate (0,0) = %v, want %v", got, want)
	}
}

func TestTileMinimapFogOverlayAndRevisionSkipping(t *testing.T) {
	m := makeOrthoTestMap()
	tm := worldmap.NewTileMinimap(m, nil)

	f := fog.New(m.Width, m.Height, m.TileWidth, m.TileHeight)
	f.AddSource(fog.Source{Team: 1, Col: 0, Row: 0, Radius: 0}) // only (0,0) visible
	f.Explore(1, 1, 0)                                          // (1,0) explored
	f.Update()

	style := worldmap.DefaultTileMinimapStyle()
	style.Fog = fog.Style{
		Unseen:   core.RGB(0x000000),
		Explored: core.RGBA(0x000000, 0.5),
	}

	src1 := tm.Sync(f, 1, &style)
	img := tm.Image()
	// (0,0) is Visible: full green terrain.
	if got, want := img.RGBAAt(0, 0), (color.RGBA{R: 0x20, G: 0xa0, B: 0x30, A: 0xff}); got != want {
		t.Errorf("visible cell (0,0) = %v, want %v", got, want)
	}
	// (1,0) is Explored: cyan (0, 0xcc, 0xff) dimmed by ~50% black.
	exp := img.RGBAAt(1, 0)
	if exp.R != 0 || exp.G < 95 || exp.G > 108 || exp.B < 120 || exp.B > 135 || exp.A != 255 {
		t.Errorf("explored cell (1,0) = %v, want ~50%% dimmed cyan", exp)
	}
	// (2,0) is Unseen: solid black.
	if got, want := img.RGBAAt(2, 0), (color.RGBA{R: 0, G: 0, B: 0, A: 255}); got != want {
		t.Errorf("unseen cell (2,0) = %v, want black", got)
	}

	// Second Sync with unchanged fog.Revision keeps Version unchanged.
	src2 := tm.Sync(f, 1, &style)
	if src2.Version != src1.Version {
		t.Fatalf("unchanged fog advanced Version: %d -> %d", src1.Version, src2.Version)
	}

	// Changing a tile in a visible cell updates composited incrementally even while fog revision is unchanged.
	m.SetTile(0, 0, 0, 2) // cyan
	src3 := tm.Sync(f, 1, &style)
	if src3.Version <= src2.Version {
		t.Fatalf("SetTile with unchanged fog did not advance composited Version")
	}
	if got, want := tm.Image().RGBAAt(0, 0), (color.RGBA{R: 0x00, G: 0xcc, B: 0xff, A: 0xff}); got != want {
		t.Errorf("visible cell after SetTile = %v, want %v", got, want)
	}
	// Unseen cell stays black even when its tile changes.
	m.SetTile(0, 2, 0, 2)
	tm.Sync(f, 1, &style)
	if got, want := tm.Image().RGBAAt(2, 0), (color.RGBA{R: 0, G: 0, B: 0, A: 255}); got != want {
		t.Errorf("unseen cell after SetTile = %v, want black", got)
	}

	// Revealing (2,0) advances fog.Revision and updates the composited image.
	f.Explore(1, 2, 0)
	src4 := tm.Sync(f, 1, &style)
	if src4.Version <= src3.Version {
		t.Fatalf("fog change did not advance Version")
	}
	if got := tm.Image().RGBAAt(2, 0); got.B < 100 {
		t.Errorf("newly explored cell (2,0) = %v, want dimmed cyan", got)
	}

	// Switching to another team recomposites for that team.
	tm.Sync(f, 2, &style)
	if got, want := tm.Image().RGBAAt(0, 0), (color.RGBA{R: 0, G: 0, B: 0, A: 255}); got != want {
		t.Errorf("team 2 unseen cell (0,0) = %v, want black", got)
	}
}

func TestTileMinimapIsometricDiamondAndClickToPan(t *testing.T) {
	// Isometric tile image with transparent corners and solid green diamond inside.
	isoTile := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			dx := math.Abs(float64(x)+0.5-8) / 8
			dy := math.Abs(float64(y)+0.5-4) / 4
			if dx+dy <= 1.0 {
				isoTile.SetRGBA(x, y, color.RGBA{R: 40, G: 180, B: 60, A: 255})
			}
		}
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: isoTile}, 16, 8, 1, 1)
	ts.TileProperties = map[uint32]tilemap.Properties{
		1: {worldmap.DefaultTileColorProperty: {Name: worldmap.DefaultTileColorProperty, Type: "color", Value: "#ff3300"}},
	}
	ts.TileCount = 2

	m := tilemap.New(6, 4, 64, 32, ts)
	m.Orientation = tilemap.OrientationIsometric
	data := make([]uint32, 6*4)
	for i := range data {
		data[i] = 1
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("iso", 6, 4, data))

	tm := worldmap.NewTileMinimap(m, nil)
	img := tm.TerrainImage()
	// Diamond image size is (W+H) x (W+H-1) = 10 x 9.
	if got, want := img.Bounds(), image.Rect(0, 0, 10, 9); got != want {
		t.Fatalf("isometric TerrainImage bounds = %v, want %v", got, want)
	}

	// Transparent corners outside the diamond must remain alpha=0.
	for _, pt := range [][2]int{{0, 0}, {9, 0}, {0, 8}, {9, 8}} {
		if c := img.RGBAAt(pt[0], pt[1]); c.A != 0 {
			t.Errorf("corner (%d,%d) outside isometric diamond = %v, want transparent", pt[0], pt[1], c)
		}
	}
	// Tile (0,0) sits at top of diamond: pixels (H-1, 0) = (3,0) and (4,0), opaque green despite transparent sprite corners!
	wantGreen := color.RGBA{R: 40, G: 180, B: 60, A: 255}
	if c := img.RGBAAt(3, 0); c != wantGreen {
		t.Errorf("iso tile (0,0) left pixel = %v, want %v", c, wantGreen)
	}
	if c := img.RGBAAt(4, 0); c != wantGreen {
		t.Errorf("iso tile (0,0) right pixel = %v, want %v", c, wantGreen)
	}

	// Updating one isometric tile updates its exact 2 pixels.
	m.SetTile(0, 2, 1, 2) // #ff3300 -> x0 = 2-1+4-1 = 4, y = 2+1 = 3
	tm.Sync(nil, 0, nil)
	wantRed := color.RGBA{R: 0xff, G: 0x33, B: 0x00, A: 0xff}
	if c := img.RGBAAt(4, 3); c != wantRed {
		t.Errorf("updated iso tile (2,1) pixel (4,3) = %v, want %v", c, wantRed)
	}
	if c := img.RGBAAt(5, 3); c != wantRed {
		t.Errorf("updated iso tile (2,1) pixel (5,3) = %v, want %v", c, wantRed)
	}

	// Click-to-pan on isometric map centers camera on exact world point of the clicked diamond tile.
	dst := core.Bounds{Origin: core.Point{X: 20, Y: 10}, Size: core.Size{Width: 200, Height: 120}}
	style := &worldmap.TileMinimapStyle{Padding: 4}
	targetPt := m.TileToWorld(4, 2)
	targetWorld := math2d.Vec2{X: float32(targetPt.X), Y: float32(targetPt.Y)}

	click, ok := tm.WorldToMinimap(dst, targetWorld, style)
	if !ok {
		t.Fatal("WorldToMinimap failed on isometric map")
	}
	cam := &tilemap.Camera{X: 0, Y: 0, Width: 120, Height: 80, Zoom: 1}
	if !tm.PanCamera(cam, dst, click, style) {
		t.Fatal("PanCamera returned false for click inside dst")
	}
	if !(math2d.Vec2{X: cam.X, Y: cam.Y}).ApproxEqual(targetWorld, 1e-3) {
		t.Fatalf("PanCamera centered camera at (%v, %v), want %v", cam.X, cam.Y, targetWorld)
	}
	if col, row := m.WorldToTile(core.Point{X: core.Points(cam.X), Y: core.Points(cam.Y)}); col != 4 || row != 2 {
		t.Fatalf("camera center maps to tile (%d, %d), want (4, 2)", col, row)
	}
}

func TestTileMinimapClickToPanOrthogonalAndClamping(t *testing.T) {
	m := makeOrthoTestMap() // 4x3 tiles of 16x16 -> world 64x48
	tm := worldmap.NewTileMinimap(m, nil)

	dst := core.Bounds{Origin: core.Point{X: 10, Y: 20}, Size: core.Size{Width: 128, Height: 96}}
	style := &worldmap.TileMinimapStyle{Padding: 0} // exact 2x scale: offset (10, 20)

	// Clicking the center of dst (74, 68) centers camera on world center (32, 24).
	cam := &tilemap.Camera{Width: 32, Height: 24, Zoom: 1}
	if !tm.PanCamera(cam, dst, math2d.Vec2{X: 74, Y: 68}, style) {
		t.Fatal("PanCamera returned false")
	}
	if !(math2d.Vec2{X: cam.X, Y: cam.Y}).ApproxEqual(math2d.Vec2{X: 32, Y: 24}, 1e-4) {
		t.Fatalf("camera center = (%v, %v), want (32, 24)", cam.X, cam.Y)
	}

	// Clicking top-left corner of tile (1, 1) -> world (16, 16) -> minimap (42, 52).
	if !tm.PanCamera(cam, dst, math2d.Vec2{X: 42, Y: 52}, style) {
		t.Fatal("PanCamera returned false")
	}
	if !(math2d.Vec2{X: cam.X, Y: cam.Y}).ApproxEqual(math2d.Vec2{X: 16, Y: 16}, 1e-4) {
		t.Fatalf("camera center = (%v, %v), want (16, 16)", cam.X, cam.Y)
	}

	// With cam.Bounds set, clicking the far corner clamps the camera's visible view inside bounds.
	wb := m.WorldBounds()
	cam.Bounds = &wb
	if !tm.PanCamera(cam, dst, math2d.Vec2{X: 11, Y: 21}, style) {
		t.Fatal("PanCamera near corner returned false")
	}
	// Camera view is 32x24, so min clamped center is (16, 12).
	if !(math2d.Vec2{X: cam.X, Y: cam.Y}).ApproxEqual(math2d.Vec2{X: 16, Y: 12}, 1e-4) {
		t.Fatalf("clamped camera center = (%v, %v), want (16, 12)", cam.X, cam.Y)
	}

	// Click outside dst or NaN does not move the camera.
	prevX, prevY := cam.X, cam.Y
	for _, bad := range []math2d.Vec2{{X: 5, Y: 50}, {X: 200, Y: 50}, {X: float32(math.NaN()), Y: 50}} {
		if tm.PanCamera(cam, dst, bad, style) {
			t.Errorf("PanCamera(%v) returned true, want false", bad)
		}
		if cam.X != prevX || cam.Y != prevY {
			t.Fatalf("PanCamera(%v) mutated camera to (%v,%v)", bad, cam.X, cam.Y)
		}
	}
	if tm.PanCamera(nil, dst, math2d.Vec2{X: 50, Y: 50}, style) {
		t.Fatal("PanCamera(nil) returned true")
	}
}

func TestTileMinimapDrawMarkersCameraAndFogHiding(t *testing.T) {
	m := makeOrthoTestMap() // 64x48 world
	tm := worldmap.NewTileMinimap(m, nil)

	f := fog.New(m.Width, m.Height, m.TileWidth, m.TileHeight)
	f.AddSource(fog.Source{Team: 1, Col: 0, Row: 0, Radius: 0}) // only (0,0) visible
	f.Update()

	cam := &tilemap.Camera{X: 32, Y: 24, Width: 32, Height: 24, Zoom: 1}
	dst := core.Bounds{Size: core.Size{Width: 128, Height: 96}}
	style := &worldmap.TileMinimapStyle{
		Background:  core.RGB(0x101010),
		Camera:      core.RGB(0xffffff),
		CameraWidth: 2,
		Marker:      core.RGB(0xffff00),
		MarkerKinds: map[string]core.Color{
			"ally":  core.RGB(0x00ff00),
			"enemy": core.RGB(0xff0000),
		},
		MarkerSize: 6,
		Padding:    0,
		Fog:        fog.DefaultStyle(),
	}

	markers := []worldmap.TileMarker{
		// Visible ally at tile (0,0) center -> world (8, 8) -> minimap (16, 16)
		{Position: math2d.Vec2{X: 8, Y: 8}, Kind: "ally", RequireVisible: true},
		// Hidden enemy at unseen tile (3,2) center -> world (56, 40) -> minimap (112, 80)
		{Position: math2d.Vec2{X: 56, Y: 40}, Kind: "enemy", RequireVisible: true},
		// Always-visible POI with explicit color at tile (2,1) center -> world (40, 24) -> minimap (80, 48)
		{Position: math2d.Vec2{X: 40, Y: 24}, Color: core.RGB(0xff00ff), Size: 6},
	}

	var d core.Drawing
	tm.Draw(&d, dst, &worldmap.TileMinimapOverlay{
		Fog:     f,
		Team:    1,
		Camera:  cam,
		Markers: markers,
	}, style)

	img := gametest.RenderOps(128, 96, d.Ops)
	// Ally marker at (16, 16) is green.
	if got := img.RGBAAt(16, 16); got != rgba(0x00ff00) {
		t.Errorf("ally marker pixel (16,16) = %v, want green", got)
	}
	// Enemy marker at (112, 80) must be hidden by fog -> black.
	if got := img.RGBAAt(112, 80); got != rgba(0x000000) {
		t.Errorf("fog-hidden enemy marker pixel (112,80) = %v, want black", got)
	}
	// POI marker at (80, 48) is magenta.
	if got := img.RGBAAt(80, 48); got != rgba(0xff00ff) {
		t.Errorf("POI marker pixel (80,48) = %v, want magenta", got)
	}
	// Camera rect covers world [16,48]x[12,36] -> minimap [32,96]x[24,72]; top edge at (64, 24) is white.
	if got := img.RGBAAt(64, 24); got != rgba(0xffffff) {
		t.Errorf("camera rect top edge pixel (64,24) = %v, want white", got)
	}

	// Rotated camera emits MoveTo/LineTo/ClosePath/Stroke.
	cam.Rotation = 0.4
	var dRot core.Drawing
	tm.Draw(&dRot, dst, &worldmap.TileMinimapOverlay{Camera: cam}, style)
	var hasMove, hasClose, hasStroke bool
	for _, op := range dRot.Ops {
		switch op.Kind {
		case core.DrawMoveTo:
			hasMove = true
		case core.DrawClosePath:
			hasClose = true
		case core.DrawStroke:
			hasStroke = true
		}
	}
	if !hasMove || !hasClose || !hasStroke {
		t.Fatalf("rotated camera did not emit closed stroked quad: move=%v close=%v stroke=%v", hasMove, hasClose, hasStroke)
	}
}

func TestTileMinimapInCanvas(t *testing.T) {
	m := makeOrthoTestMap()
	tm := worldmap.NewTileMinimap(m, nil)
	cam := &tilemap.Camera{X: 32, Y: 24, Width: 32, Height: 24, Zoom: 1}
	style := worldmap.DefaultTileMinimapStyle()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "tile-minimap",
			Style:  core.Style{Width: core.Pt(128), Height: core.Pt(96)},
			Draw: func(d *core.Drawing, size core.Size) {
				tm.Draw(d, core.Bounds{Size: size}, &worldmap.TileMinimapOverlay{
					Camera: cam,
					Markers: []worldmap.TileMarker{
						{Position: math2d.Vec2{X: 8, Y: 8}, Kind: "unit"},
					},
				}, &style)
			},
		}
	})

	ops := app.Find("tile-minimap").Ops
	var hasImage, hasStroke, hasFill bool
	for _, op := range ops {
		switch op.Kind {
		case core.DrawImage:
			hasImage = true
		case core.DrawStroke:
			hasStroke = true
		case core.DrawFill:
			hasFill = true
		}
	}
	if !hasImage || !hasStroke || !hasFill {
		t.Fatalf("canvas ops missing expected draws: image=%v stroke=%v fill=%v", hasImage, hasStroke, hasFill)
	}
}

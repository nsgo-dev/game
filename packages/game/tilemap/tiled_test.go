package tilemap_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func loadSampleTiled(t *testing.T) (*tilemap.Tilemap, []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "sample.tmj"))
	if err != nil {
		t.Fatal(err)
	}
	var loadedImages []string
	m, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{
		LoadJSON: func(source string) ([]byte, error) {
			return os.ReadFile(filepath.Join("testdata", filepath.FromSlash(source)))
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			loadedImages = append(loadedImages, imagePath)
			return core.ImageSource{URI: "asset:" + imagePath}, nil
		},
	})
	if err != nil {
		t.Fatalf("LoadTiled(sample.tmj): %v", err)
	}
	return m, loadedImages
}

func TestLoadTiledTwoTilesetsFlipsAndDrawOps(t *testing.T) {
	m, images := loadSampleTiled(t)

	if m.Width != 4 || m.Height != 3 || m.TileWidth != 16 || m.TileHeight != 16 {
		t.Fatalf("map dimensions = %dx%d (%gx%g), want 4x3 (16x16)", m.Width, m.Height, m.TileWidth, m.TileHeight)
	}
	if len(m.Tilesets) != 2 {
		t.Fatalf("loaded %d tilesets, want 2 (embedded + external)", len(m.Tilesets))
	}
	if m.Tilesets[0].Name != "terrain" || m.Tilesets[0].FirstGID != 1 {
		t.Fatalf("tileset[0] = %q firstgid=%d, want terrain firstgid=1", m.Tilesets[0].Name, m.Tilesets[0].FirstGID)
	}
	if m.Tilesets[1].Name != "props" || m.Tilesets[1].FirstGID != 9 {
		t.Fatalf("tileset[1] = %q firstgid=%d, want props firstgid=9", m.Tilesets[1].Name, m.Tilesets[1].FirstGID)
	}
	// External tileset image path is resolved relative to tilesets/props.tsj -> tilesets/props.png.
	if len(images) != 2 || images[0] != "terrain.png" || images[1] != "tilesets/props.png" {
		t.Fatalf("resolved images = %v, want [terrain.png tilesets/props.png]", images)
	}
	if len(m.Layers) != 3 {
		t.Fatalf("loaded %d tile layers, want 3", len(m.Layers))
	}
	if m.Layer("hidden").Visible {
		t.Fatal("hidden layer should have Visible=false")
	}

	terrainImg := core.ImageSource{URI: "asset:terrain.png"}
	propsImg := core.ImageSource{URI: "asset:tilesets/props.png"}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "tiled", Style: core.Style{Width: core.Pt(80), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				m.Draw(d, nil)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(80), Height: core.Pt(64)}, Draw: func(d *core.Drawing, _ core.Size) {
				// ground layer:
				// terrain tileset has margin=1, spacing=2, 16x16 tiles, columns=4:
				//   local 0 -> (1, 1), local 1 -> (19, 1), local 2 -> (37, 1)
				// props tileset has margin=0, spacing=0, 16x16 tiles, columns=2:
				//   local 0 (GID 9) -> (0, 0), local 1 (GID 10) -> (16, 0)

				// Row 0: tile 1 with flags 0, H, V, H|V
				d.DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16)
				// col=1 (16,0), H: a=-1, b=0, c=0, d=1, e=32, f=0
				d.Save().Transform(-1, 0, 0, 1, 32, 0).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()
				// col=2 (32,0), V: a=1, b=0, c=0, d=-1, e=32, f=16
				d.Save().Transform(1, 0, 0, -1, 32, 16).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()
				// col=3 (48,0), H|V: a=-1, b=0, c=0, d=-1, e=64, f=16
				d.Save().Transform(-1, 0, 0, -1, 64, 16).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()

				// Row 1: tile 1 with flags H|D, V|D, D, H|V|D
				// col=0 (0,16), H|D (90° CW): a=0, b=1, c=-1, d=0, e=16, f=16
				d.Save().Transform(0, 1, -1, 0, 16, 16).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()
				// col=1 (16,16), V|D (90° CCW): a=0, b=-1, c=1, d=0, e=16, f=32
				d.Save().Transform(0, -1, 1, 0, 16, 32).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()
				// col=2 (32,16), D: a=0, b=1, c=1, d=0, e=32, f=16
				d.Save().Transform(0, 1, 1, 0, 32, 16).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()
				// col=3 (48,16), H|V|D: a=0, b=-1, c=-1, d=0, e=64, f=32
				d.Save().Transform(0, -1, -1, 0, 64, 32).DrawImageFrom(terrainImg, 1, 1, 16, 16, 0, 0, 16, 16).Restore()

				// Row 2: GID 2 (terrain local 1), GID 3 (animated terrain local 2), GID 9 (props local 0), GID 10 (props local 1)
				d.DrawImageFrom(terrainImg, 19, 1, 16, 16, 0, 32, 16, 16)
				d.DrawImageFrom(terrainImg, 37, 1, 16, 16, 16, 32, 16, 16)
				d.DrawImageFrom(propsImg, 0, 0, 16, 16, 32, 32, 16, 16)
				d.DrawImageFrom(propsImg, 16, 0, 16, 16, 48, 32, 16, 16)

				// overlay layer: opacity=0.5, offset=(4, -2), GID 9 at col=1, row=1 -> dst=(20, 14)
				d.Save().SetAlpha(0.5).DrawImageFrom(propsImg, 0, 0, 16, 16, 20, 14, 16, 16).Restore()
				// hidden layer is not drawn.
			}},
		}}
	})
	sameOps(t, app.Find("tiled").Ops, app.Find("hand").Ops)
}

func TestLoadTiledObjectsAndCustomProperties(t *testing.T) {
	m, _ := loadSampleTiled(t)

	// Map custom properties (string, int, float, bool, color #AARRGGBB, file, object).
	if v, ok := m.Properties.String("title"); !ok || v != "Cavern" {
		t.Fatalf("map.title = (%q, %v), want (Cavern, true)", v, ok)
	}
	if v, ok := m.Properties.Int("level"); !ok || v != 3 {
		t.Fatalf("map.level = (%d, %v), want (3, true)", v, ok)
	}
	if v, ok := m.Properties.Float("gravity"); !ok || v != 9.81 {
		t.Fatalf("map.gravity = (%g, %v), want (9.81, true)", v, ok)
	}
	if v, ok := m.Properties.Bool("dark"); !ok || !v {
		t.Fatalf("map.dark = (%v, %v), want (true, true)", v, ok)
	}
	if v, ok := m.Properties.Color("ambient"); !ok || v != core.Color(0x20406080) {
		t.Fatalf("map.ambient = (%#08x, %v), want (0x20406080, true)", uint32(v), ok)
	}
	if v, ok := m.Properties.File("music"); !ok || v != "audio/cavern.ogg" {
		t.Fatalf("map.music = (%q, %v), want (audio/cavern.ogg, true)", v, ok)
	}
	if v, ok := m.Properties.ObjectID("entry"); !ok || v != 2 {
		t.Fatalf("map.entry = (%d, %v), want (2, true)", v, ok)
	}

	// Tileset & tile properties (embedded and external).
	if v, ok := m.Tilesets[0].Properties.String("biome"); !ok || v != "stone" {
		t.Fatalf("terrain.biome = (%q, %v), want (stone, true)", v, ok)
	}
	if v, ok := m.Tilesets[1].Properties.String("theme"); !ok || v != "dungeon" {
		t.Fatalf("props.theme = (%q, %v), want (dungeon, true)", v, ok)
	}
	if v, ok := m.TileProperties(1).Float("friction"); !ok || v != 0.75 {
		t.Fatalf("tile 1 friction = (%g, %v), want (0.75, true)", v, ok)
	}
	if v, ok := m.TileProperties(9).String("loot"); !ok || v != "gold" {
		t.Fatalf("tile 9 loot = (%q, %v), want (gold, true)", v, ok)
	}
	if v, ok := m.TileProperties(9).Int("value"); !ok || v != 100 {
		t.Fatalf("tile 9 value = (%d, %v), want (100, true)", v, ok)
	}
	if v, ok := m.TileProperties(9).Color("tint"); !ok || v != core.RGB(0x00ccff) {
		t.Fatalf("tile 9 tint = (%#08x, %v), want (%#08x, true)", uint32(v), ok, uint32(core.RGB(0x00ccff)))
	}
	if v, ok := m.TileProperties(10).Bool("breakable"); !ok || !v {
		t.Fatalf("tile 10 breakable = (%v, %v), want (true, true)", v, ok)
	}

	// Collision flags populated automatically from tile properties (embedded + external).
	if f := m.FlagsAt(0, 0); f != tilemap.TileSolid {
		t.Fatalf("FlagsAt(0,0) = %v, want TileSolid", f)
	}
	if f := m.FlagsAt(0, 2); f != tilemap.TileOneWay {
		t.Fatalf("FlagsAt(0,2) = %v, want TileOneWay", f)
	}
	if f := m.FlagsAt(3, 2); f != tilemap.TileFlags(4) {
		t.Fatalf("FlagsAt(3,2) = %v, want custom flag 4", f)
	}

	// Layer properties.
	if v, ok := m.Layer("ground").Properties.String("kind"); !ok || v != "collision" {
		t.Fatalf("ground.kind = (%q, %v), want (collision, true)", v, ok)
	}
	if v, ok := m.Layer("overlay").Properties.Float("parallax"); !ok || v != 1.25 {
		t.Fatalf("overlay.parallax = (%g, %v), want (1.25, true)", v, ok)
	}

	// Object layer & all 6 object shapes.
	entities := m.ObjectLayer("entities")
	if entities == nil {
		t.Fatal("ObjectLayer(entities) is nil")
	}
	if entities.OffsetX != 2 || entities.OffsetY != 3 || !entities.Visible || entities.Opacity != 1 {
		t.Fatalf("entities metadata = %+v", entities)
	}
	if v, ok := entities.Properties.Bool("active"); !ok || !v {
		t.Fatalf("entities.active = (%v, %v), want (true, true)", v, ok)
	}
	if len(entities.Objects) != 6 {
		t.Fatalf("entities has %d objects, want 6", len(entities.Objects))
	}

	// 1. Rectangle + all property types.
	exit := entities.Objects[0]
	if exit.ID != 1 || exit.Name != "exit" || exit.Type != "trigger" || exit.Shape != tilemap.ObjectRect ||
		exit.X != 48 || exit.Y != 16 || exit.Width != 16 || exit.Height != 32 || exit.Rotation != 15 || !exit.Visible {
		t.Fatalf("exit object = %+v", exit)
	}
	if v, ok := exit.Properties.String("next"); !ok || v != "level2" {
		t.Fatalf("exit.next = (%q, %v)", v, ok)
	}
	if v, ok := exit.Properties.Int("delay"); !ok || v != 500 {
		t.Fatalf("exit.delay = (%d, %v)", v, ok)
	}
	if v, ok := exit.Properties.Float("fade"); !ok || v != 0.5 {
		t.Fatalf("exit.fade = (%g, %v)", v, ok)
	}
	if v, ok := exit.Properties.Bool("locked"); !ok || v {
		t.Fatalf("exit.locked = (%v, %v)", v, ok)
	}
	if v, ok := exit.Properties.Color("glow"); !ok || v != core.RGB(0xff8800) {
		t.Fatalf("exit.glow = (%#08x, %v), want %#08x", uint32(v), ok, uint32(core.RGB(0xff8800)))
	}
	if v, ok := exit.Properties.File("script"); !ok || v != "scripts/exit.lua" {
		t.Fatalf("exit.script = (%q, %v)", v, ok)
	}
	if v, ok := exit.Properties.ObjectID("linked"); !ok || v != 2 {
		t.Fatalf("exit.linked = (%d, %v)", v, ok)
	}

	// 2. Point (using Tiled 1.9+ class fallback).
	spawn := entities.Objects[1]
	if spawn.Shape != tilemap.ObjectPoint || spawn.Name != "spawn" || spawn.Type != "marker" || spawn.X != 8 || spawn.Y != 24 {
		t.Fatalf("spawn object = %+v", spawn)
	}

	// 3. Ellipse.
	lamp := entities.Objects[2]
	if lamp.Shape != tilemap.ObjectEllipse || lamp.Name != "lamp" || lamp.Width != 12 || lamp.Height != 8 {
		t.Fatalf("lamp object = %+v", lamp)
	}

	// 4. Polygon + WorldPoints.
	ramp := entities.Objects[3]
	if ramp.Shape != tilemap.ObjectPolygon || len(ramp.Points) != 3 {
		t.Fatalf("ramp object = %+v", ramp)
	}
	wantRampPts := []core.Point{{X: 16, Y: 32}, {X: 32, Y: 32}, {X: 32, Y: 24}}
	gotRampPts := ramp.WorldPoints()
	for i, want := range wantRampPts {
		if gotRampPts[i] != want {
			t.Fatalf("ramp WorldPoints[%d] = %+v, want %+v", i, gotRampPts[i], want)
		}
	}

	// 5. Polyline + WorldPoints.
	patrol := entities.Objects[4]
	if patrol.Shape != tilemap.ObjectPolyline || len(patrol.Points) != 3 {
		t.Fatalf("patrol object = %+v", patrol)
	}
	wantPatrolPts := []core.Point{{X: 4, Y: 8}, {X: 24, Y: 8}, {X: 24, Y: 20}}
	gotPatrolPts := patrol.WorldPoints()
	for i, want := range wantPatrolPts {
		if gotPatrolPts[i] != want {
			t.Fatalf("patrol WorldPoints[%d] = %+v, want %+v", i, gotPatrolPts[i], want)
		}
	}

	// 6. Tile object (flipped GID, Visible=false).
	chest := entities.Objects[5]
	if chest.Shape != tilemap.ObjectTile || chest.TileID() != 9 || chest.FlipFlags() != tilemap.FlipHorizontal || chest.Visible {
		t.Fatalf("chest object = %+v (TileID=%d FlipFlags=%#x)", chest, chest.TileID(), chest.FlipFlags())
	}
}

func TestLoadTiledAnimatedTileAdvancesOnLoop(t *testing.T) {
	m, _ := loadSampleTiled(t)
	// Isolate only the animated tile at (1, 2) (GID 3 -> terrain local 2: frames 2[100ms], 3[200ms], 4[150ms]).
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("anim", 1, 1, []uint32{3})}
	m.Width, m.Height = 1, 1

	terrainImg := core.ImageSource{URI: "asset:terrain.png"}
	// Source rects in terrain (margin=1, spacing=2, 16x16, columns=4):
	// local 2 -> (37, 1), local 3 -> (55, 1), local 4 -> (1, 19)
	frameSrc := []core.Point{{X: 37, Y: 1}, {X: 55, Y: 1}, {X: 1, Y: 19}}

	var tick *core.Signal[int]
	var wantIdx *core.Signal[int]
	l := &game.Loop{
		Rate: 100,
		Update: func(dt time.Duration) {
			m.Update(dt)
		},
		Draw: func(float64) {
			tick.Set(tick.Get() + 1)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		wantIdx = core.NewSignal(cx, 0)
		l.Run(cx)
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "tiled", Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				m.Draw(d, nil)
			}},
			core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(32), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				src := frameSrc[wantIdx.Get()]
				d.DrawImageFrom(terrainImg, float32(src.X), float32(src.Y), 16, 16, 0, 0, 16, 16)
			}},
		}}
	})

	assertFrame := func(idx int) {
		t.Helper()
		app.Post(func() { wantIdx.Set(idx) })
		app.Settle()
		sameOps(t, app.Find("tiled").Ops, app.Find("want").Ops)
	}

	assertFrame(0)
	app.Advance(100 * time.Millisecond)
	assertFrame(1)
	app.Advance(200 * time.Millisecond)
	assertFrame(2)
	app.Advance(150 * time.Millisecond)
	assertFrame(0)
}

func TestLoadTiledGroupLayerAndErrors(t *testing.T) {
	// Nested group layer accumulates offsets, opacity, and visibility.
	groupJSON := []byte(`{
		"width": 1, "height": 1, "tilewidth": 16, "tileheight": 16,
		"orientation": "orthogonal",
		"tilesets": [{"firstgid": 1, "name": "t", "image": "t.png", "columns": 1, "tilecount": 1}],
		"layers": [{
			"name": "g", "type": "group", "offsetx": 10, "offsety": 20, "opacity": 0.5, "visible": true,
			"layers": [{
				"name": "child", "type": "tilelayer", "width": 1, "height": 1,
				"offsetx": 5, "offsety": -4, "opacity": 0.5, "visible": true,
				"data": [1]
			}]
		}]
	}`)
	m, err := tilemap.LoadTiled(groupJSON, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled(group): %v", err)
	}
	ch := m.Layer("child")
	if ch == nil || ch.OffsetX != 15 || ch.OffsetY != 16 || ch.Opacity != 0.25 || !ch.Visible {
		t.Fatalf("grouped child layer = %+v, want offset (15,16) opacity 0.25 visible true", ch)
	}

	// Error cases.
	for _, tc := range []struct {
		name string
		json string
	}{
		{"unknown orientation", `{"width":1,"height":1,"tilewidth":16,"tileheight":16,"orientation":"diagonal"}`},
		{"bad chunk tile count", `{"width":0,"height":0,"tilewidth":16,"tileheight":16,"infinite":true,"layers":[{"name":"a","type":"tilelayer","chunks":[{"x":0,"y":0,"width":2,"height":2,"data":[1,2]}]}]}`},
		{"zero tile size", `{"width":1,"height":1,"tilewidth":0,"tileheight":16}`},
		{"missing LoadJSON", `{"width":1,"height":1,"tilewidth":16,"tileheight":16,"tilesets":[{"firstgid":1,"source":"ext.tsj"}]}`},
		{"encoded layer", `{"width":1,"height":1,"tilewidth":16,"tileheight":16,"layers":[{"name":"a","type":"tilelayer","encoding":"base64","data":"AAAA"}]}`},
		{"wrong tile count", `{"width":2,"height":2,"tilewidth":16,"tileheight":16,"layers":[{"name":"a","type":"tilelayer","data":[1,2]}]}`},
		{"negative anim duration", `{"width":1,"height":1,"tilewidth":16,"tileheight":16,"tilesets":[{"firstgid":1,"name":"t","columns":1,"tiles":[{"id":0,"animation":[{"tileid":0,"duration":-10}]}]}]}`},
	} {
		if _, err := tilemap.LoadTiled([]byte(tc.json), tilemap.TilesetLoader{}); err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
	}
}

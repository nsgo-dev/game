package tilemap_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const sampleLDtkJSON = `{
	"defaultGridSize": 16,
	"defs": {
		"tilesets": [
			{
				"uid": 10,
				"identifier": "Dungeon",
				"relPath": "tiles/dungeon.png",
				"pxWid": 64,
				"pxHei": 32,
				"tileGridSize": 16,
				"spacing": 0,
				"padding": 0,
				"__cWid": 4,
				"__cHei": 2
			}
		],
		"layers": [
			{
				"uid": 20,
				"identifier": "Collisions",
				"type": "IntGrid",
				"gridSize": 16,
				"intGridValues": [
					{"value": 1, "identifier": "solid", "color": "#FFFFFF"},
					{"value": 2, "identifier": "oneWay", "color": "#00FF00"},
					{"value": 3, "identifier": "spikes", "color": "#FF0000"}
				]
			}
		]
	},
	"levels": [
		{
			"identifier": "Level_0",
			"uid": 0,
			"worldX": 0,
			"worldY": 0,
			"pxWid": 64,
			"pxHei": 48,
			"fieldInstances": [
				{"__identifier": "theme", "__type": "String", "__value": "crypt"}
			],
			"layerInstances": [
				{
					"__identifier": "Entities",
					"__type": "Entities",
					"__cWid": 4,
					"__cHei": 3,
					"__gridSize": 16,
					"__opacity": 1.0,
					"__pxTotalOffsetX": 0,
					"__pxTotalOffsetY": 0,
					"visible": true,
					"entityInstances": [
						{
							"__identifier": "Hero",
							"px": [16, 32],
							"width": 16,
							"height": 24,
							"iid": "hero-1",
							"fieldInstances": [
								{"__identifier": "hp", "__type": "Int", "__value": 100},
								{"__identifier": "speed", "__type": "Float", "__value": 4.25},
								{"__identifier": "invincible", "__type": "Bool", "__value": false},
								{"__identifier": "title", "__type": "String", "__value": "Knight"},
								{"__identifier": "aura", "__type": "Color", "__value": "#00CCFF"}
							]
						},
						{
							"__identifier": "Waypoint",
							"px": [48, 16],
							"width": 0,
							"height": 0,
							"iid": "wp-1",
							"fieldInstances": []
						}
					]
				},
				{
					"__identifier": "Decor",
					"__type": "Tiles",
					"__cWid": 4,
					"__cHei": 3,
					"__gridSize": 16,
					"__opacity": 0.5,
					"__pxTotalOffsetX": 2,
					"__pxTotalOffsetY": -1,
					"__tilesetDefUid": 10,
					"visible": true,
					"gridTiles": [
						{"px": [16, 0], "src": [16, 0], "f": 3, "t": 1}
					]
				},
				{
					"__identifier": "AutoWall",
					"__type": "AutoLayer",
					"__cWid": 4,
					"__cHei": 3,
					"__gridSize": 16,
					"__opacity": 1.0,
					"__pxTotalOffsetX": 0,
					"__pxTotalOffsetY": 0,
					"__tilesetDefUid": 10,
					"visible": true,
					"autoLayerTiles": [
						{"px": [0, 0], "src": [0, 0], "f": 1, "t": 0},
						{"px": [32, 16], "src": [32, 0], "f": 2, "t": 2}
					]
				},
				{
					"__identifier": "Collisions",
					"__type": "IntGrid",
					"__cWid": 4,
					"__cHei": 3,
					"__gridSize": 16,
					"__opacity": 1.0,
					"layerDefUid": 20,
					"visible": true,
					"intGridCsv": [
						1, 0, 0, 0,
						0, 2, 0, 3,
						1, 1, 1, 1
					]
				}
			]
		},
		{
			"identifier": "Level_1",
			"uid": 1,
			"worldX": 64,
			"worldY": 0,
			"pxWid": 32,
			"pxHei": 32,
			"layerInstances": [
				{
					"__identifier": "Floor",
					"__type": "Tiles",
					"__cWid": 2,
					"__cHei": 2,
					"__gridSize": 16,
					"__opacity": 1.0,
					"__tilesetDefUid": 10,
					"visible": true,
					"gridTiles": [
						{"px": [0, 0], "src": [0, 0], "f": 0, "t": 0}
					]
				}
			]
		}
	]
}`

func TestLoadLDtkTilesAutoLayerIntGridAndEntities(t *testing.T) {
	var resolved []string
	resolve := func(relPath string) core.ImageSource {
		resolved = append(resolved, relPath)
		return core.ImageSource{URI: "asset:" + relPath}
	}

	m, err := tilemap.LoadLDtk([]byte(sampleLDtkJSON), "Level_0", resolve)
	if err != nil {
		t.Fatalf("LoadLDtk(Level_0): %v", err)
	}
	if m.Width != 4 || m.Height != 3 || m.TileWidth != 16 || m.TileHeight != 16 {
		t.Fatalf("map size = %dx%d (%gx%g), want 4x3 (16x16)", m.Width, m.Height, m.TileWidth, m.TileHeight)
	}
	if len(resolved) != 1 || resolved[0] != "tiles/dungeon.png" {
		t.Fatalf("resolved images = %v, want [tiles/dungeon.png]", resolved)
	}
	if theme, ok := m.Properties.String("theme"); !ok || theme != "crypt" {
		t.Fatalf("level theme = (%q, %v), want (crypt, true)", theme, ok)
	}

	// Bottom-to-top layer order: AutoWall (index 0) before Decor (index 1).
	if len(m.Layers) != 2 || m.Layers[0].Name != "AutoWall" || m.Layers[1].Name != "Decor" {
		t.Fatalf("layers = %+v, want [AutoWall, Decor]", m.Layers)
	}

	// Verify flip bits mapped from LDtk f (bit 0 = FlipHorizontal, bit 1 = FlipVertical).
	if got := m.TileAt(0, 0, 0); got != (tilemap.FlipHorizontal | 1) {
		t.Fatalf("AutoWall(0,0) = %#x, want FlipHorizontal|1", got)
	}
	if got := m.TileAt(0, 2, 1); got != (tilemap.FlipVertical | 3) {
		t.Fatalf("AutoWall(2,1) = %#x, want FlipVertical|3", got)
	}
	if got := m.TileAt(1, 1, 0); got != (tilemap.FlipHorizontal | tilemap.FlipVertical | 2) {
		t.Fatalf("Decor(1,0) = %#x, want FlipHorizontal|FlipVertical|2", got)
	}

	// IntGrid collision flags + Raycast.
	if f := m.FlagsAt(0, 0); f != tilemap.TileSolid {
		t.Fatalf("FlagsAt(0,0) = %v, want TileSolid", f)
	}
	if f := m.FlagsAt(1, 1); f != tilemap.TileOneWay {
		t.Fatalf("FlagsAt(1,1) = %v, want TileOneWay", f)
	}
	if f := m.FlagsAt(3, 1); f != tilemap.TileFlags(1<<2) {
		t.Fatalf("FlagsAt(3,1) = %v, want custom flag 4 (1<<2)", f)
	}
	hit, ok := m.Raycast(core.Point{X: 24, Y: 8}, core.Point{X: 24, Y: 44}, tilemap.TileSolid)
	if !ok || hit.Col != 1 || hit.Row != 2 {
		t.Fatalf("LDtk IntGrid raycast = (%+v, %v), want solid floor hit at (1,2)", hit, ok)
	}

	// Entity layer and custom fields.
	entities := m.ObjectLayer("Entities")
	if entities == nil || len(entities.Objects) != 2 {
		t.Fatalf("Entities layer = %+v", entities)
	}
	hero := entities.Objects[0]
	if hero.Name != "Hero" || hero.Shape != tilemap.ObjectRect || hero.X != 16 || hero.Y != 32 || hero.Width != 16 || hero.Height != 24 {
		t.Fatalf("Hero object = %+v", hero)
	}
	if hp, ok := hero.Properties.Int("hp"); !ok || hp != 100 {
		t.Fatalf("hero.hp = (%d, %v), want (100, true)", hp, ok)
	}
	if spd, ok := hero.Properties.Float("speed"); !ok || spd != 4.25 {
		t.Fatalf("hero.speed = (%g, %v), want (4.25, true)", spd, ok)
	}
	if inv, ok := hero.Properties.Bool("invincible"); !ok || inv {
		t.Fatalf("hero.invincible = (%v, %v), want (false, true)", inv, ok)
	}
	if title, ok := hero.Properties.String("title"); !ok || title != "Knight" {
		t.Fatalf("hero.title = (%q, %v), want (Knight, true)", title, ok)
	}
	if aura, ok := hero.Properties.Color("aura"); !ok || aura != core.RGB(0x00ccff) {
		t.Fatalf("hero.aura = (%#08x, %v), want %#08x", uint32(aura), ok, uint32(core.RGB(0x00ccff)))
	}
	wp := entities.Objects[1]
	if wp.Name != "Waypoint" || wp.Shape != tilemap.ObjectPoint || wp.X != 48 || wp.Y != 16 {
		t.Fatalf("Waypoint object = %+v", wp)
	}

	// Verify exact drawing ops (AutoWall flipped tiles, then Decor with opacity=0.5 & offset (2,-1)).
	img := core.ImageSource{URI: "asset:tiles/dungeon.png"}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "ldtk", Style: core.Style{Width: core.Pt(64), Height: core.Pt(48)}, Draw: func(d *core.Drawing, _ core.Size) {
				m.Draw(d, nil)
			}},
			core.Canvas{TestID: "want", Style: core.Style{Width: core.Pt(64), Height: core.Pt(48)}, Draw: func(d *core.Drawing, _ core.Size) {
				// AutoWall (0,0) local 0, FlipH: dst=(0,0), a=-1, d=1, e=16, f=0
				d.Save().Transform(-1, 0, 0, 1, 16, 0).DrawImageFrom(img, 0, 0, 16, 16, 0, 0, 16, 16).Restore()
				// AutoWall (2,1) local 2, FlipV: dst=(32,16), a=1, d=-1, e=32, f=32
				d.Save().Transform(1, 0, 0, -1, 32, 32).DrawImageFrom(img, 32, 0, 16, 16, 0, 0, 16, 16).Restore()
				// Decor (1,0) local 1, FlipH|FlipV, offset=(2,-1), opacity=0.5 -> dst=(18,-1), a=-1, d=-1, e=34, f=15
				d.Save().SetAlpha(0.5).Save().Transform(-1, 0, 0, -1, 34, 15).DrawImageFrom(img, 16, 0, 16, 16, 0, 0, 16, 16).Restore().Restore()
			}},
		}}
	})
	sameOps(t, app.Find("ldtk").Ops, app.Find("want").Ops)

	// LoadLDtkAll loads every level keyed by identifier.
	all, err := tilemap.LoadLDtkAll([]byte(sampleLDtkJSON), resolve)
	if err != nil {
		t.Fatalf("LoadLDtkAll: %v", err)
	}
	if len(all) != 2 || all["Level_0"] == nil || all["Level_1"] == nil {
		t.Fatalf("LoadLDtkAll returned keys %v, want Level_0 and Level_1", all)
	}
	if all["Level_1"].TileAt(0, 0, 0) != 1 {
		t.Fatalf("Level_1 tile(0,0) = %d, want 1", all["Level_1"].TileAt(0, 0, 0))
	}

	// Empty identifier loads the first level.
	first, err := tilemap.LoadLDtk([]byte(sampleLDtkJSON), "", nil)
	if err != nil || first.Width != 4 {
		t.Fatalf("LoadLDtk default level = (%+v, %v)", first, err)
	}

	// Error cases.
	if _, err := tilemap.LoadLDtk([]byte(sampleLDtkJSON), "Missing", nil); err == nil {
		t.Fatal("expected error for missing LDtk level")
	}
	if _, err := tilemap.LoadLDtk([]byte(`{"levels":[]}`), "", nil); err == nil {
		t.Fatal("expected error for empty LDtk levels")
	}
}

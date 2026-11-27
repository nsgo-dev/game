package tilemap_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/tilemap"
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

const groupedLDtkJSON = `{
	"defaultGridSize": 16,
	"defs": {
		"layers": [
			{
				"uid": 10,
				"identifier": "Terrain",
				"type": "IntGrid",
				"gridSize": 16,
				"intGridValuesGroups": [
					{"uid": 100, "identifier": "Solid", "color": "#FFFFFF"},
					{"uid": 200, "identifier": "Hazards", "color": "#FF0000"}
				],
				"intGridValues": [
					{"value": 1, "identifier": "Stone", "color": "#888888", "groupUid": 100},
					{"value": 2, "identifier": "Bridge", "color": "#AA8844", "groupUid": 0},
					{"value": 3, "identifier": "Lava", "color": "#FF3300", "groupUid": 200},
					{"value": 4, "identifier": "Decor", "color": "#00AA00", "groupUid": 0}
				]
			}
		]
	},
	"levels": [
		{
			"identifier": "L0",
			"uid": 0,
			"pxWid": 64,
			"pxHei": 16,
			"layerInstances": [
				{
					"__identifier": "Terrain",
					"__type": "IntGrid",
					"__cWid": 4,
					"__cHei": 1,
					"__gridSize": 16,
					"layerDefUid": 10,
					"intGridCsv": [1, 2, 3, 4]
				}
			]
		}
	]
}`

func TestLoadLDtkWithCustomIntGridMappingAndGroups(t *testing.T) {
	// Default heuristic honors intGridValuesGroups ("Stone" in group "Solid" -> TileSolid).
	defMap, err := tilemap.LoadLDtk([]byte(groupedLDtkJSON), "L0", nil)
	if err != nil {
		t.Fatalf("LoadLDtk: %v", err)
	}
	if got := defMap.FlagsAt(0, 0); got != tilemap.TileSolid {
		t.Fatalf("default Stone (group Solid) = %v, want TileSolid", got)
	}

	// LoadLDtkWith applies project-configured identifier, group, and numeric value rules (including explicit 0).
	const flagHazard tilemap.TileFlags = 1 << 4
	const flagBridgeExtra tilemap.TileFlags = 1 << 5
	opts := tilemap.LDtkOptions{
		IntGridFlags: map[string]tilemap.TileFlags{
			"bridge":  tilemap.TileOneWay | flagBridgeExtra,
			"hazards": flagHazard,
			"decor":   0, // explicitly non-colliding
		},
		IntGridValueFlags: map[int]tilemap.TileFlags{
			1: tilemap.TileSolid | tilemap.TileFlags(1<<3), // numeric rule overrides group
		},
	}
	m, err := tilemap.LoadLDtkWith([]byte(groupedLDtkJSON), "L0", opts)
	if err != nil {
		t.Fatalf("LoadLDtkWith: %v", err)
	}
	if got := m.FlagsAt(0, 0); got != (tilemap.TileSolid | tilemap.TileFlags(1<<3)) {
		t.Fatalf("value 1 (Stone) = %v, want Solid|(1<<3)", got)
	}
	if got := m.FlagsAt(1, 0); got != (tilemap.TileOneWay | flagBridgeExtra) {
		t.Fatalf("Bridge = %v, want OneWay|flagBridgeExtra", got)
	}
	if got := m.FlagsAt(2, 0); got != flagHazard {
		t.Fatalf("Lava (group Hazards) = %v, want flagHazard", got)
	}
	if got := m.FlagsAt(3, 0); got != 0 {
		t.Fatalf("Decor = %v, want 0 (explicitly cleared)", got)
	}

	// LoadLDtkAllWith and LoadLDtkWorldWith apply the same options.
	all, err := tilemap.LoadLDtkAllWith([]byte(groupedLDtkJSON), opts)
	if err != nil || all["L0"].FlagsAt(2, 0) != flagHazard || all["L0"].FlagsAt(3, 0) != 0 {
		t.Fatalf("LoadLDtkAllWith = (%v, %v)", all, err)
	}
	w, err := tilemap.LoadLDtkWorldWith([]byte(groupedLDtkJSON), "", opts)
	if err != nil {
		t.Fatalf("LoadLDtkWorldWith: %v", err)
	}
	lm, err := w.Levels[0].Load()
	if err != nil || lm.FlagsAt(1, 0) != (tilemap.TileOneWay|flagBridgeExtra) || lm.FlagsAt(3, 0) != 0 {
		t.Fatalf("LoadLDtkWorldWith level = (%v, %v)", lm, err)
	}
}

func TestLoadLDtkMalformedLayerBoundsAndOverflow(t *testing.T) {
	cases := []struct {
		name      string
		json      string
		wantField string
		wantSub   string
	}{
		{
			name:      "intgrid zero width divide by zero (#1551)",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":0,"pxHei":0,"layerInstances":[{"__identifier":"G","__type":"IntGrid","__cWid":0,"__cHei":0,"__gridSize":16,"intGridCsv":[1]}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "positive",
		},
		{
			name:      "intgrid mixed-case missing width (#1551 fuzz)",
			json:      `{"defAultGridSiZe":1,"levels":[{"lAYerInstAnCes":[{"__tYpe":"IntGrid","intGridCsv":[1]}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "positive",
		},
		{
			name:      "tiles zero height after fallback",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":8,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":0,"__cHei":0,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cHei",
			wantSub:   "positive",
		},
		{
			name:      "autolayer zero width after fallback",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":0,"pxHei":64,"layerInstances":[{"__identifier":"A","__type":"AutoLayer","__cWid":0,"__cHei":0,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "positive",
		},
		{
			name:      "negative layer width",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":48,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":-1,"__cHei":3,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "non-negative",
		},
		{
			name:      "negative layer height",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":48,"layerInstances":[{"__identifier":"A","__type":"AutoLayer","__cWid":4,"__cHei":-2,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cHei",
			wantSub:   "non-negative",
		},
		{
			name:      "unbounded allocation 20000x20000 (#1551)",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":20000,"__cHei":20000,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "exceeds maximum",
		},
		{
			name:      "autolayer exceeds cell limit via level fallback",
			json:      `{"defaultGridSize":1,"levels":[{"identifier":"L","pxWid":5000,"pxHei":5000,"layerInstances":[{"__identifier":"A","__type":"AutoLayer","__gridSize":1}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "exceeds maximum",
		},
		{
			name:      "int64 overflow 4294967296x4294967297 (#1551)",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":4294967296,"__cHei":4294967297,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "overflow",
		},
		{
			name:      "32-bit overflow 65536x65537",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":65536,"__cHei":65537,"__gridSize":16}]}]}`,
			wantField: "levels[0].layerInstances[0].__cWid",
			wantSub:   "exceeds maximum",
		},
		{
			name:      "intGridCsv longer than layer dimensions",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":32,"pxHei":16,"layerInstances":[{"__identifier":"G","__type":"IntGrid","__cWid":2,"__cHei":1,"__gridSize":16,"intGridCsv":[1,0,1]}]}]}`,
			wantField: "levels[0].layerInstances[0].intGridCsv",
			wantSub:   "intGridCsv",
		},
		{
			name:      "cumulative multi-layer tile budget exceeded",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"L0","__type":"Tiles","__cWid":4096,"__cHei":4096},{"__identifier":"L1","__type":"Tiles","__cWid":4096,"__cHei":4096},{"__identifier":"L2","__type":"Tiles","__cWid":4096,"__cHei":4096},{"__identifier":"L3","__type":"Tiles","__cWid":4096,"__cHei":4096},{"__identifier":"L4","__type":"Tiles","__cWid":4096,"__cHei":4096}]}]}`,
			wantField: "levels[0].layerInstances[4].__cWid",
			wantSub:   "total tile allocation",
		},
		{
			name:      "negative level pxWid",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":-16,"pxHei":32,"layerInstances":[]}]}`,
			wantField: "levels[0].pxWid",
			wantSub:   "non-negative",
		},
		{
			name:      "negative level pxHei",
			json:      `{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":32,"pxHei":-16,"layerInstances":[]}]}`,
			wantField: "levels[0].pxHei",
			wantSub:   "non-negative",
		},
		{
			name:      "tileset grid exceeds tile ID range",
			json:      `{"defaultGridSize":16,"defs":{"tilesets":[{"uid":1,"identifier":"Huge","__cWid":100000,"__cHei":100000}]},"levels":[{"identifier":"L","pxWid":16,"pxHei":16,"layerInstances":[]}]}`,
			wantField: "defs.tilesets[0].__cWid",
			wantSub:   "tile ID range",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.json)

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, errAll := tilemap.LoadLDtkAllWith(raw, tilemap.LDtkOptions{})
			runtime.ReadMemStats(&after)
			if errAll == nil {
				t.Fatalf("LoadLDtkAllWith(%s) succeeded, want error on %s", tc.name, tc.wantField)
			}
			// Guard against accidental giant allocations before failing.
			if after.TotalAlloc-before.TotalAlloc > 4<<20 {
				t.Fatalf("LoadLDtkAllWith(%s) allocated %d bytes before failing", tc.name, after.TotalAlloc-before.TotalAlloc)
			}
			le, ok := loaderr.As(errAll)
			if !ok {
				t.Fatalf("LoadLDtkAllWith error = %T (%v), want *loaderr.Error", errAll, errAll)
			}
			if le.Field != tc.wantField {
				t.Errorf("LoadLDtkAllWith Field = %q, want %q (err: %v)", le.Field, tc.wantField, errAll)
			}
			if le.Fix == "" {
				t.Errorf("LoadLDtkAllWith Fix is empty (err: %v)", errAll)
			}
			if tc.wantSub != "" && !strings.Contains(strings.ToLower(errAll.Error()), strings.ToLower(tc.wantSub)) {
				t.Errorf("LoadLDtkAllWith error %q does not mention %q", errAll.Error(), tc.wantSub)
			}

			_, errOne := tilemap.LoadLDtkWith(raw, "", tilemap.LDtkOptions{})
			if errOne == nil {
				t.Fatalf("LoadLDtkWith(%s) succeeded, want error on %s", tc.name, tc.wantField)
			}
			if leOne, ok := loaderr.As(errOne); !ok || leOne.Field != tc.wantField {
				t.Errorf("LoadLDtkWith error = %v (field %v), want field %q", errOne, leOne, tc.wantField)
			}

			w, errWorld := tilemap.LoadLDtkWorldWith(raw, "", tilemap.LDtkOptions{})
			if errWorld == nil {
				if len(w.Levels) == 0 {
					t.Fatalf("LoadLDtkWorldWith(%s) returned 0 levels", tc.name)
				}
				_, errLazy := w.Levels[0].Load()
				if errLazy == nil {
					t.Fatalf("LoadLDtkWorldWith(%s).Levels[0].Load() succeeded, want error on %s", tc.name, tc.wantField)
				}
				if leLazy, ok := loaderr.As(errLazy); !ok || leLazy.Field != tc.wantField {
					t.Errorf("LoadLDtkWorldWith lazy Load error = %v, want field %q", errLazy, tc.wantField)
				}
			} else if leW, ok := loaderr.As(errWorld); !ok || leW.Field != tc.wantField {
				t.Errorf("LoadLDtkWorldWith error = %v, want field %q", errWorld, tc.wantField)
			}
		})
	}

	// Valid fallback: layer with __cWid=0, __cHei=0 inherits positive level dimensions and sparse IntGrid loads cleanly.
	t.Run("valid level dimension fallback and entities-only zero-size level", func(t *testing.T) {
		fallback := []byte(`{"defaultGridSize":16,"levels":[{"identifier":"L","pxWid":32,"pxHei":16,"layerInstances":[{"__identifier":"G","__type":"IntGrid","__cWid":0,"__cHei":0,"intGridCsv":[1]},{"__identifier":"T","__type":"Tiles","__cWid":0,"__cHei":0}]}]}`)
		m, err := tilemap.LoadLDtk(fallback, "L", nil)
		if err != nil {
			t.Fatalf("LoadLDtk(fallback): %v", err)
		}
		if m.Width != 2 || m.Height != 1 || m.FlagsAt(0, 0) != tilemap.TileSolid || len(m.Layers) != 1 || len(m.Layers[0].Data) != 2 {
			t.Fatalf("fallback map = %+v", m)
		}

		entitiesOnly := []byte(`{"defaultGridSize":16,"levels":[{"identifier":"E","pxWid":0,"pxHei":0,"layerInstances":[{"__identifier":"Objects","__type":"Entities","entityInstances":[{"__identifier":"Spawn","px":[8,8],"width":0,"height":0}]}]}]}`)
		me, err := tilemap.LoadLDtk(entitiesOnly, "E", nil)
		if err != nil {
			t.Fatalf("LoadLDtk(entitiesOnly): %v", err)
		}
		if obj := me.ObjectLayer("Objects"); obj == nil || len(obj.Objects) != 1 {
			t.Fatalf("entitiesOnly Objects = %+v", obj)
		}
	})

	// Multi-level LoadLDtkAll cumulative budget refuses loading 5 levels of 4096x4096 layers without allocating > 256 MiB.
	t.Run("LoadLDtkAll multi-level cumulative budget", func(t *testing.T) {
		var levels []string
		for i := range 5 {
			levels = append(levels, fmt.Sprintf(`{"identifier":"L%d","pxWid":64,"pxHei":64,"layerInstances":[{"__identifier":"T","__type":"Tiles","__cWid":4096,"__cHei":4096}]}`, i))
		}
		raw := []byte(`{"defaultGridSize":16,"levels":[` + strings.Join(levels, ",") + `]}`)
		_, err := tilemap.LoadLDtkAll(raw, nil)
		if err == nil {
			t.Fatal("LoadLDtkAll succeeded for 5x 4096x4096 levels, want budget error")
		}
		if le, ok := loaderr.As(err); !ok || le.Field != "levels[4].layerInstances[0].__cWid" {
			t.Fatalf("LoadLDtkAll budget error = %v, want field levels[4].layerInstances[0].__cWid", err)
		}
	})
}

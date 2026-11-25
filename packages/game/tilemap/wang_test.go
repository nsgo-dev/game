package tilemap_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// makeCorner3Tileset builds a complete 3-terrain (1=Grass, 2=Dirt, 3=Water) corner WangSet
// containing all 3^4 = 81 corner combinations.
// Local TileID = (TL-1)*27 + (TR-1)*9 + (BR-1)*3 + (BL-1).
func makeCorner3Tileset(firstGID uint32) (*tilemap.Tileset, *tilemap.WangSet) {
	ws := tilemap.WangSet{
		Name:     "Terrains",
		Type:     tilemap.WangCorner,
		Tile:     0,
		FirstGID: firstGID,
		Colors: []tilemap.WangColor{
			{Name: "Grass", Color: core.RGB(0x00ff00), Tile: 0, Probability: 1},
			{Name: "Dirt", Color: core.RGB(0x8b4513), Tile: 40, Probability: 1},
			{Name: "Water", Color: core.RGB(0x0080ff), Tile: 80, Probability: 1},
		},
		WangTiles: make([]tilemap.WangTile, 0, 81),
	}
	for tl := uint8(1); tl <= 3; tl++ {
		for tr := uint8(1); tr <= 3; tr++ {
			for br := uint8(1); br <= 3; br++ {
				for bl := uint8(1); bl <= 3; bl++ {
					id := uint32((tl-1)*27 + (tr-1)*9 + (br-1)*3 + (bl - 1))
					ws.WangTiles = append(ws.WangTiles, tilemap.WangTile{
						TileID:      id,
						WangID:      tilemap.WangID{0, tr, 0, br, 0, bl, 0, tl},
						Probability: 1,
					})
				}
			}
		}
	}
	ts := tilemap.NewGridTileset(firstGID, core.Src("asset:terrain.png"), 16, 16, 9, 81)
	ts.Name = "terrain"
	ts.WangSets = []tilemap.WangSet{ws}
	return ts, &ts.WangSets[0]
}

func cornerTileGID(firstGID uint32, tl, tr, br, bl uint8) uint32 {
	return firstGID + uint32((tl-1)*27+(tr-1)*9+(br-1)*3+(bl-1))
}

// makeEdge3Tileset builds a complete 3-terrain (1=Grass, 2=Dirt, 3=Water) edge WangSet
// containing all 3^4 = 81 edge combinations.
// Local TileID = (T-1)*27 + (R-1)*9 + (B-1)*3 + (L-1).
func makeEdge3Tileset(firstGID uint32) (*tilemap.Tileset, *tilemap.WangSet) {
	ws := tilemap.WangSet{
		Name:     "Edges",
		Type:     tilemap.WangEdge,
		Tile:     0,
		FirstGID: firstGID,
		Colors: []tilemap.WangColor{
			{Name: "Grass", Tile: 0, Probability: 1},
			{Name: "Dirt", Tile: 40, Probability: 1},
			{Name: "Water", Tile: 80, Probability: 1},
		},
		WangTiles: make([]tilemap.WangTile, 0, 81),
	}
	for top := uint8(1); top <= 3; top++ {
		for right := uint8(1); right <= 3; right++ {
			for bottom := uint8(1); bottom <= 3; bottom++ {
				for left := uint8(1); left <= 3; left++ {
					id := uint32((top-1)*27 + (right-1)*9 + (bottom-1)*3 + (left - 1))
					ws.WangTiles = append(ws.WangTiles, tilemap.WangTile{
						TileID:      id,
						WangID:      tilemap.WangID{top, 0, right, 0, bottom, 0, left, 0},
						Probability: 1,
					})
				}
			}
		}
	}
	ts := tilemap.NewGridTileset(firstGID, core.Src("asset:edges.png"), 16, 16, 9, 81)
	ts.Name = "edges"
	ts.WangSets = []tilemap.WangSet{ws}
	return ts, &ts.WangSets[0]
}

func edgeTileGID(firstGID uint32, top, right, bottom, left uint8) uint32 {
	return firstGID + uint32((top-1)*27+(right-1)*9+(bottom-1)*3+(left-1))
}

// makeMixed2Tileset builds a complete 2-terrain (1=Grass, 2=Dirt) mixed WangSet
// with all 2^8 = 256 combinations of the 8 slots.
func makeMixed2Tileset(firstGID uint32) (*tilemap.Tileset, *tilemap.WangSet) {
	ws := tilemap.WangSet{
		Name:     "Mixed",
		Type:     tilemap.WangMixed,
		Tile:     0,
		FirstGID: firstGID,
		Colors: []tilemap.WangColor{
			{Name: "Grass", Tile: 0, Probability: 1},
			{Name: "Dirt", Tile: 255, Probability: 1},
		},
		WangTiles: make([]tilemap.WangTile, 0, 256),
	}
	for mask := range 256 {
		var wid tilemap.WangID
		for s := range 8 {
			if (mask>>s)&1 != 0 {
				wid[s] = 2
			} else {
				wid[s] = 1
			}
		}
		ws.WangTiles = append(ws.WangTiles, tilemap.WangTile{
			TileID:      uint32(mask),
			WangID:      wid,
			Probability: 1,
		})
	}
	ts := tilemap.NewGridTileset(firstGID, core.Src("asset:mixed.png"), 16, 16, 16, 256)
	ts.Name = "mixed"
	ts.WangSets = []tilemap.WangSet{ws}
	return ts, &ts.WangSets[0]
}

// assertSeamlessWangGrid checks that every pair of adjacent WangSet tiles in [x0, x1) x [y0, y1)
// agrees on all shared edges and corners.
func assertSeamlessWangGrid(t *testing.T, m *tilemap.Tilemap, layer int, ws *tilemap.WangSet, firstGID uint32, x0, y0, x1, y1 int) {
	t.Helper()
	widAt := func(c, r int) (tilemap.WangID, bool) {
		gid := m.TileAt(layer, c, r) & tilemap.TileIDMask
		if gid < firstGID {
			return tilemap.WangID{}, false
		}
		return ws.WangIDForTile(gid - firstGID)
	}
	for r := y0; r < y1; r++ {
		for c := x0; c < x1; c++ {
			a, okA := widAt(c, r)
			if !okA {
				continue
			}
			// Check Right neighbor (c+1, r)
			if b, okB := widAt(c+1, r); okB {
				if ws.Type == tilemap.WangEdge || ws.Type == tilemap.WangMixed {
					if a[tilemap.WangRight] != b[tilemap.WangLeft] {
						t.Errorf("edge mismatch (%d,%d).Right=%d vs (%d,%d).Left=%d", c, r, a[tilemap.WangRight], c+1, r, b[tilemap.WangLeft])
					}
				}
				if ws.Type == tilemap.WangCorner || ws.Type == tilemap.WangMixed {
					if a[tilemap.WangTopRight] != b[tilemap.WangTopLeft] {
						t.Errorf("corner mismatch (%d,%d).TR=%d vs (%d,%d).TL=%d", c, r, a[tilemap.WangTopRight], c+1, r, b[tilemap.WangTopLeft])
					}
					if a[tilemap.WangBottomRight] != b[tilemap.WangBottomLeft] {
						t.Errorf("corner mismatch (%d,%d).BR=%d vs (%d,%d).BL=%d", c, r, a[tilemap.WangBottomRight], c+1, r, b[tilemap.WangBottomLeft])
					}
				}
			}
			// Check Bottom neighbor (c, r+1)
			if b, okB := widAt(c, r+1); okB {
				if ws.Type == tilemap.WangEdge || ws.Type == tilemap.WangMixed {
					if a[tilemap.WangBottom] != b[tilemap.WangTop] {
						t.Errorf("edge mismatch (%d,%d).Bottom=%d vs (%d,%d).Top=%d", c, r, a[tilemap.WangBottom], c, r+1, b[tilemap.WangTop])
					}
				}
				if ws.Type == tilemap.WangCorner || ws.Type == tilemap.WangMixed {
					if a[tilemap.WangBottomLeft] != b[tilemap.WangTopLeft] {
						t.Errorf("corner mismatch (%d,%d).BL=%d vs (%d,%d).TL=%d", c, r, a[tilemap.WangBottomLeft], c, r+1, b[tilemap.WangTopLeft])
					}
					if a[tilemap.WangBottomRight] != b[tilemap.WangTopRight] {
						t.Errorf("corner mismatch (%d,%d).BR=%d vs (%d,%d).TR=%d", c, r, a[tilemap.WangBottomRight], c, r+1, b[tilemap.WangTopRight])
					}
				}
			}
			// Check BottomRight diagonal neighbor (c+1, r+1)
			if ws.Type == tilemap.WangCorner || ws.Type == tilemap.WangMixed {
				if b, okB := widAt(c+1, r+1); okB {
					if a[tilemap.WangBottomRight] != b[tilemap.WangTopLeft] {
						t.Errorf("diag corner mismatch (%d,%d).BR=%d vs (%d,%d).TL=%d", c, r, a[tilemap.WangBottomRight], c+1, r+1, b[tilemap.WangTopLeft])
					}
				}
				if b, okB := widAt(c-1, r+1); okB {
					if a[tilemap.WangBottomLeft] != b[tilemap.WangTopRight] {
						t.Errorf("diag corner mismatch (%d,%d).BL=%d vs (%d,%d).TR=%d", c, r, a[tilemap.WangBottomLeft], c-1, r+1, b[tilemap.WangTopRight])
					}
				}
			}
		}
	}
}

func TestWangLoadTiledEmbeddedAndExternal(t *testing.T) {
	extTSJ := `{
		"name": "roads",
		"image": "roads.png",
		"imagewidth": 64,
		"tilewidth": 16,
		"tileheight": 16,
		"columns": 4,
		"tilecount": 16,
		"tiles": [
			{"id": 0, "probability": 0.75},
			{"id": 1, "probability": 0.25},
			{"id": 2, "probability": 0}
		],
		"wangsets": [
			{
				"name": "RoadEdges",
				"type": "edge",
				"class": "road-set",
				"tile": 0,
				"properties": [{"name": "layer", "type": "string", "value": "ground"}],
				"colors": [
					{"name": "Grass", "color": "#00ff00", "tile": 0, "probability": 1.0, "class": "biomes", "properties": [{"name": "walkable", "type": "bool", "value": true}]},
					{"name": "Road", "color": "#80ff8800", "tile": 1, "probability": 0.5}
				],
				"wangtiles": [
					{"tileid": 0, "wangid": [1, 0, 1, 0, 1, 0, 1, 0]},
					{"tileid": 1, "wangid": [2, 0, 2, 0, 2, 0, 2, 0]},
					{"tileid": 2, "wangid": [1, 0, 2, 0, 1, 0, 2, 0]}
				]
			}
		]
	}`

	tmj := `{
		"width": 4,
		"height": 4,
		"tilewidth": 16,
		"tileheight": 16,
		"orientation": "orthogonal",
		"tilesets": [
			{
				"firstgid": 1,
				"name": "ground",
				"image": "ground.png",
				"tilewidth": 16,
				"tileheight": 16,
				"columns": 4,
				"tilecount": 16,
				"tiles": [
					{"id": 0, "probability": 2.5}
				],
				"wangsets": [
					{
						"name": "GroundCorners",
						"type": "corner",
						"tile": -1,
						"colors": [
							{"name": "Grass", "color": "#22aa22", "tile": 0},
							{"name": "Dirt", "color": "#996633"}
						],
						"wangtiles": [
							{"tileid": 0, "wangid": [0, 1, 0, 1, 0, 1, 0, 1]},
							{"tileid": 1, "wangid": [0, 2, 0, 2, 0, 2, 0, 2]}
						]
					},
					{
						"name": "GroundMixed",
						"type": "mixed",
						"colors": [
							{"name": "Grass", "color": "#22aa22"},
							{"name": "Dirt", "color": "#996633"}
						],
						"wangtiles": [
							{"tileid": 0, "wangid": [1, 1, 1, 1, 1, 1, 1, 1]}
						]
					}
				]
			},
			{
				"firstgid": 17,
				"source": "sets/roads.tsj"
			}
		],
		"layers": [
			{"name": "ground", "type": "tilelayer", "width": 4, "height": 4, "data": [0,0,0,0, 0,0,0,0, 0,0,0,0, 0,0,0,0]}
		]
	}`

	m, err := tilemap.LoadTiled([]byte(tmj), tilemap.TilesetLoader{
		LoadJSON: func(source string) ([]byte, error) {
			if source != "sets/roads.tsj" {
				return nil, fmt.Errorf("unexpected source %q", source)
			}
			return []byte(extTSJ), nil
		},
	})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}

	ts0 := m.Tilesets[0]
	if ts0.TileProbability(0) != 2.5 || ts0.TileProbability(1) != 1.0 {
		t.Fatalf("ts0 tile probabilities = (%v, %v), want (2.5, 1.0)", ts0.TileProbability(0), ts0.TileProbability(1))
	}
	if len(ts0.WangSets) != 2 {
		t.Fatalf("ts0 WangSets len = %d, want 2", len(ts0.WangSets))
	}
	cws := ts0.WangSet("GroundCorners")
	if cws == nil || cws != ts0.WangSetAt(0) || cws != m.WangSet("GroundCorners") {
		t.Fatalf("WangSet lookup mismatch for GroundCorners")
	}
	if cws.Type != tilemap.WangCorner || cws.Type.String() != "corner" || cws.Tile != -1 || cws.FirstGID != 1 {
		t.Fatalf("GroundCorners header = %+v", cws)
	}
	if cws.Colors[1].Tile != -1 || cws.Colors[1].Probability != 1.0 {
		t.Fatalf("omitted color tile/probability = (%d, %v), want (-1, 1.0)", cws.Colors[1].Tile, cws.Colors[1].Probability)
	}
	if cws.WangTiles[0].Probability != 2.5 || cws.WangTiles[1].Probability != 1.0 {
		t.Fatalf("GroundCorners wangtile probs = (%v, %v), want (2.5, 1.0)", cws.WangTiles[0].Probability, cws.WangTiles[1].Probability)
	}
	if mws := ts0.WangSetAt(1); mws == nil || mws.Type != tilemap.WangMixed || mws.Type.String() != "mixed" || mws.Tile != -1 {
		t.Fatalf("GroundMixed = %+v", mws)
	}
	if ts0.WangSetAt(-1) != nil || ts0.WangSetAt(2) != nil || ts0.WangSet("missing") != nil {
		t.Fatalf("expected nil for out-of-range/missing WangSet lookups")
	}

	// External .tsj WangSet.
	ews := m.WangSet("RoadEdges")
	if ews == nil {
		t.Fatal("expected RoadEdges from external .tsj")
	}
	if ews.Type != tilemap.WangEdge || ews.Type.String() != "edge" || ews.FirstGID != 17 || ews.Class != "road-set" || ews.Tile != 0 {
		t.Fatalf("RoadEdges = %+v", ews)
	}
	if layer, ok := ews.Properties.String("layer"); !ok || layer != "ground" {
		t.Fatalf("RoadEdges property layer = (%q, %v), want (ground, true)", layer, ok)
	}
	if idx, ok := ews.ColorIndex("Road"); !ok || idx != 2 {
		t.Fatalf("ColorIndex(Road) = (%d, %v), want (2, true)", idx, ok)
	}
	if ews.Colors[1].Color != core.Color(0xff880080) || ews.Colors[1].Probability != 0.5 {
		t.Fatalf("Road color = (%#x, %v), want (0xff880080, 0.5)", uint32(ews.Colors[1].Color), ews.Colors[1].Probability)
	}
	if walk, ok := ews.Colors[0].Properties.Bool("walkable"); !ok || !walk {
		t.Fatalf("Grass walkable prop = (%v, %v), want (true, true)", walk, ok)
	}
	if wid, ok := ews.WangIDForTile(2); !ok || wid != (tilemap.WangID{1, 0, 2, 0, 1, 0, 2, 0}) {
		t.Fatalf("WangIDForTile(2) = (%v, %v)", wid, ok)
	}
	if ews.WangTiles[0].Probability != 0.75 || ews.WangTiles[1].Probability != 0.25 || ews.WangTiles[2].Probability != 0 {
		t.Fatalf("external wangtile probs = (%v, %v, %v)", ews.WangTiles[0].Probability, ews.WangTiles[1].Probability, ews.WangTiles[2].Probability)
	}
}

func TestWangLoadTiledErrors(t *testing.T) {
	base := `"tilewidth": 16, "tileheight": 16, "width": 2, "height": 2`
	cases := []struct {
		name, json, field string
	}{
		{
			name:  "invalid wangset type",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "hex"}]}]}`,
			field: "tilesets[0].wangsets[0].type",
		},
		{
			name:  "missing wangset type",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w"}]}]}`,
			field: "tilesets[0].wangsets[0].type",
		},
		{
			name:  "invalid color hex",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "corner", "colors": [{"name": "g", "color": "not-hex"}]}]}]}`,
			field: "tilesets[0].wangsets[0].colors[0].color",
		},
		{
			name:  "negative color probability",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "corner", "colors": [{"name": "g", "probability": -0.5}]}]}]}`,
			field: "tilesets[0].wangsets[0].colors[0].probability",
		},
		{
			name:  "negative tile probability",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "tiles": [{"id": 0, "probability": -1}]}]}`,
			field: "tilesets[0].tiles[0].probability",
		},
		{
			name:  "wrong wangid length",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "corner", "colors": [{"name": "g"}], "wangtiles": [{"tileid": 0, "wangid": [0, 1, 0, 1]}]}]}]}`,
			field: "tilesets[0].wangsets[0].wangtiles[0].wangid",
		},
		{
			name:  "wangid color out of bounds",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "corner", "colors": [{"name": "g"}], "wangtiles": [{"tileid": 0, "wangid": [0, 2, 0, 1, 0, 1, 0, 1]}]}]}]}`,
			field: "tilesets[0].wangsets[0].wangtiles[0].wangid",
		},
		{
			name:  "wangid negative index",
			json:  `{` + base + `, "tilesets": [{"firstgid": 1, "wangsets": [{"name": "w", "type": "edge", "wangtiles": [{"tileid": 0, "wangid": [-1, 0, 1, 0, 1, 0, 1, 0]}]}]}]}`,
			field: "tilesets[0].wangsets[0].wangtiles[0].wangid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "terrain.tmj"), "terrain.tmj", data, tc.field)
		})
	}

	t.Run("external tsj wangset error names tsj file", func(t *testing.T) {
		tsj := loaderrtest.Indent(`{"name": "t", "wangsets": [{"name": "w", "type": "corner", "colors": [{"name": "g"}], "wangtiles": [{"tileid": 0, "wangid": [0, 9, 0, 1, 0, 1, 0, 1]}]}]}`)
		tmj := loaderrtest.Indent(`{` + base + `, "tilesets": [{"firstgid": 1, "source": "sets/terrain.tsj"}]}`)
		_, err := tilemap.LoadTiled(tmj, tilemap.TilesetLoader{
			LoadJSON: func(string) ([]byte, error) { return tsj, nil },
		})
		loaderrtest.ExpectField(t, loaderr.WithFile(err, "map.tmj"), "sets/terrain.tsj", tsj, "wangsets[0].wangtiles[0].wangid")
	})
}

func TestWangGoldenThreeTerrainCorner(t *testing.T) {
	const (
		firstGID = uint32(1)
		G        = uint8(1) // Grass
		D        = uint8(2) // Dirt
		W        = uint8(3) // Water
	)
	ts, ws := makeCorner3Tileset(firstGID)
	m := tilemap.New(5, 5, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}

	// Start with a 5x5 Grass field, then paint Dirt at (1, 2) and Water at (3, 2).
	m.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)
	m.SetTerrain(0, 1, 2, ws, D, nil)
	m.SetTerrain(0, 3, 2, ws, W, nil)

	// Golden expected grid matching Tiled's Terrain Brush (TL, TR, BR, BL corners):
	// Row 0: untouched Grass
	// Row 1: (0,1)=G,G,D,G  (1,1)=G,G,D,D  (2,1)=G,G,W,D [3-terrain!]  (3,1)=G,G,W,W  (4,1)=G,G,G,W
	// Row 2: (0,2)=G,D,D,G  (1,2)=D,D,D,D  (2,2)=D,W,W,D [Dirt-Water]  (3,2)=W,W,W,W  (4,2)=W,G,G,W
	// Row 3: (0,3)=G,D,G,G  (1,3)=D,D,G,G  (2,3)=D,W,G,G [3-terrain!]  (3,3)=W,W,G,G  (4,3)=W,G,G,G
	// Row 4: untouched Grass
	c := func(tl, tr, br, bl uint8) uint32 { return cornerTileGID(firstGID, tl, tr, br, bl) }
	want := [5][5]uint32{
		{c(G, G, G, G), c(G, G, G, G), c(G, G, G, G), c(G, G, G, G), c(G, G, G, G)},
		{c(G, G, D, G), c(G, G, D, D), c(G, G, W, D), c(G, G, W, W), c(G, G, G, W)},
		{c(G, D, D, G), c(D, D, D, D), c(D, W, W, D), c(W, W, W, W), c(W, G, G, W)},
		{c(G, D, G, G), c(D, D, G, G), c(D, W, G, G), c(W, W, G, G), c(W, G, G, G)},
		{c(G, G, G, G), c(G, G, G, G), c(G, G, G, G), c(G, G, G, G), c(G, G, G, G)},
	}
	for r := range 5 {
		for col := range 5 {
			got := m.TileAt(0, col, r)
			if got != want[r][col] {
				gotW, _ := ws.WangIDForTile(got - firstGID)
				wantW, _ := ws.WangIDForTile(want[r][col] - firstGID)
				t.Errorf("cell (%d,%d): got GID %d %v, want GID %d %v", col, r, got, gotW, want[r][col], wantW)
			}
		}
	}
	assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 5, 5)

	// Verify SetTerrainRect produces the exact same result as painting each cell individually.
	mRect := tilemap.New(5, 5, 16, 16, ts)
	mRect.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}
	mRect.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)
	mRect.SetTerrainRect(0, 1, 1, 2, 2, ws, W, nil)

	mSingle := tilemap.New(5, 5, 16, 16, ts)
	mSingle.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}
	mSingle.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)
	for r := 1; r <= 2; r++ {
		for col := 1; col <= 2; col++ {
			mSingle.SetTerrain(0, col, r, ws, W, nil)
		}
	}
	for r := range 5 {
		for col := range 5 {
			if mRect.TileAt(0, col, r) != mSingle.TileAt(0, col, r) {
				t.Errorf("rect vs single mismatch at (%d,%d): rect=%d single=%d", col, r, mRect.TileAt(0, col, r), mSingle.TileAt(0, col, r))
			}
		}
	}
	assertSeamlessWangGrid(t, mRect, 0, ws, firstGID, 0, 0, 5, 5)

	// Repainting the 2x2 Water patch back to Grass must restore the entire 5x5 grid to solid Grass.
	mRect.SetTerrainRect(0, 1, 1, 2, 2, ws, G, nil)
	grassGID := c(G, G, G, G)
	for r := range 5 {
		for col := range 5 {
			if got := mRect.TileAt(0, col, r); got != grassGID {
				t.Errorf("after repainting Grass at (%d,%d): got %d, want %d", col, r, got, grassGID)
			}
		}
	}
}

func TestWangTwoHopShorelinePropagation(t *testing.T) {
	// 3-terrain corner tileset with ONLY Grass<->Dirt and Dirt<->Water transitions (NO direct Grass<->Water tiles).
	const (
		firstGID = uint32(1)
		G        = uint8(1)
		D        = uint8(2)
		W        = uint8(3)
	)
	fullTS, fullWS := makeCorner3Tileset(firstGID)
	filtered := *fullWS
	filtered.WangTiles = nil
	for _, wt := range fullWS.WangTiles {
		hasG, hasW := false, false
		for _, slot := range []int{tilemap.WangTopRight, tilemap.WangBottomRight, tilemap.WangBottomLeft, tilemap.WangTopLeft} {
			if wt.WangID[slot] == G {
				hasG = true
			}
			if wt.WangID[slot] == W {
				hasW = true
			}
		}
		if hasG && hasW {
			continue
		}
		filtered.WangTiles = append(filtered.WangTiles, wt)
	}
	ts := *fullTS
	ts.WangSets = []tilemap.WangSet{filtered}
	ws := &ts.WangSets[0]

	m := tilemap.New(5, 5, 16, 16, &ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}
	m.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)
	m.SetTerrain(0, 2, 2, ws, W, nil)

	// Center is Water, Wave 1 becomes Dirt<->Water transitions, Wave 2 becomes Grass<->Dirt transitions!
	c := func(tl, tr, br, bl uint8) uint32 { return cornerTileGID(firstGID, tl, tr, br, bl) }
	want := [5][5]uint32{
		{c(G, G, D, G), c(G, G, D, D), c(G, G, D, D), c(G, G, D, D), c(G, G, G, D)},
		{c(G, D, D, G), c(D, D, W, D), c(D, D, W, W), c(D, D, D, W), c(D, G, G, D)},
		{c(G, D, D, G), c(D, W, W, D), c(W, W, W, W), c(W, D, D, W), c(D, G, G, D)},
		{c(G, D, D, G), c(D, W, D, D), c(W, W, D, D), c(W, D, D, D), c(D, G, G, D)},
		{c(G, D, G, G), c(D, D, G, G), c(D, D, G, G), c(D, D, G, G), c(D, G, G, G)},
	}
	for r := range 5 {
		for col := range 5 {
			got := m.TileAt(0, col, r)
			if got != want[r][col] {
				gotW, _ := ws.WangIDForTile(got - firstGID)
				wantW, _ := ws.WangIDForTile(want[r][col] - firstGID)
				t.Errorf("2-hop (%d,%d): got %d %v, want %d %v", col, r, got, gotW, want[r][col], wantW)
			}
		}
	}
	assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 5, 5)
}

func TestWangEdgeSetThreeTerrains(t *testing.T) {
	const (
		firstGID = uint32(10)
		G        = uint8(1)
		D        = uint8(2)
		W        = uint8(3)
	)
	ts, ws := makeEdge3Tileset(firstGID)
	m := tilemap.New(5, 5, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}
	m.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)

	// Paint an L-shape of Dirt: (1,1), (2,1), (1,2), then Water at (3,2).
	m.SetTerrain(0, 1, 1, ws, D, nil)
	m.SetTerrain(0, 2, 1, ws, D, nil)
	m.SetTerrain(0, 1, 2, ws, D, nil)
	m.SetTerrain(0, 3, 2, ws, W, nil)

	e := func(top, right, bottom, left uint8) uint32 { return edgeTileGID(firstGID, top, right, bottom, left) }
	// Cell (2, 2) touches Dirt on Top and Left, Water on Right, Grass on Bottom -> 3-terrain edge tile!
	if got, want := m.TileAt(0, 2, 2), e(D, W, G, D); got != want {
		t.Fatalf("(2,2) = %d, want %d (T=D, R=W, B=G, L=D)", got, want)
	}
	// Diagonal cell (0, 0) shares no edge with Dirt (1,1), so it stays solid Grass.
	if got, want := m.TileAt(0, 0, 0), e(G, G, G, G); got != want {
		t.Fatalf("(0,0) = %d, want solid Grass %d", got, want)
	}
	assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 5, 5)
}

func TestWangMixedSetTransitions(t *testing.T) {
	const (
		firstGID = uint32(1)
		G        = uint8(1)
		D        = uint8(2)
	)
	ts, ws := makeMixed2Tileset(firstGID)
	m := tilemap.New(5, 5, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, nil)}
	m.SetTerrainRect(0, 0, 0, 5, 5, ws, G, nil)

	// Paint a single Dirt cell at (2, 2).
	m.SetTerrain(0, 2, 2, ws, D, nil)
	// Top neighbor (2, 1) must have BR, Bottom, BL = Dirt, rest = Grass.
	gotTop, ok := ws.WangIDForTile(m.TileAt(0, 2, 1) - firstGID)
	wantTop := tilemap.WangID{G, G, G, D, D, D, G, G}
	if !ok || gotTop != wantTop {
		t.Fatalf("(2,1) mixed WangID = %v, want %v", gotTop, wantTop)
	}
	// TopRight neighbor (3, 1) must have only BL = Dirt, rest = Grass.
	gotTR, ok := ws.WangIDForTile(m.TileAt(0, 3, 1) - firstGID)
	wantTR := tilemap.WangID{G, G, G, G, G, D, G, G}
	if !ok || gotTR != wantTR {
		t.Fatalf("(3,1) mixed WangID = %v, want %v", gotTR, wantTR)
	}
	assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 5, 5)

	// Paint diagonal neighbor (3, 3) as well and check the shared hinge cell (3, 2).
	m.SetTerrain(0, 3, 3, ws, D, nil)
	gotHinge, ok := ws.WangIDForTile(m.TileAt(0, 3, 2) - firstGID)
	wantHinge := tilemap.WangID{G, G, G, D, D, D, D, D}
	if !ok || gotHinge != wantHinge {
		t.Fatalf("(3,2) hinge mixed WangID = %v, want %v", gotHinge, wantHinge)
	}
	assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 5, 5)
}

func TestWangIsometricMap(t *testing.T) {
	const (
		firstGID = uint32(1)
		G        = uint8(1)
		D        = uint8(2)
		W        = uint8(3)
	)
	for _, tc := range []struct {
		name string
		make func(uint32) (*tilemap.Tileset, *tilemap.WangSet)
	}{
		{"corner", makeCorner3Tileset},
		{"edge", makeEdge3Tileset},
		{"mixed", makeMixed2Tileset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, ws := tc.make(firstGID)
			m := tilemap.New(6, 6, 32, 16, ts)
			m.Orientation = tilemap.OrientationIsometric
			m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("iso", 6, 6, nil)}

			rng := random.New(99)
			m.SetTerrainRect(0, 0, 0, 6, 6, ws, G, rng)
			m.SetTerrainRect(0, 2, 2, 2, 2, ws, D, rng)
			if len(ws.Colors) >= 3 {
				m.SetTerrain(0, 4, 2, ws, W, rng)
			}
			assertSeamlessWangGrid(t, m, 0, ws, firstGID, 0, 0, 6, 6)

			// Verify isometric diamond world geometry agrees with grid-space Wang slots:
			// Cell (2, 2)'s Right diamond vertex (ox+tw, oy+th/2) is identical to
			// cell (3, 1)'s Left diamond vertex (ox, oy+th/2), matching WangTopRight(1) <-> WangBottomLeft(5).
			oA := m.TileOrigin(2, 2)
			oB := m.TileOrigin(3, 1)
			if oA.X+core.Points(m.TileWidth) != oB.X || oA.Y != oB.Y {
				t.Fatalf("unexpected iso vertex geometry: (2,2)=%v (3,1)=%v", oA, oB)
			}
			if ws.Type != tilemap.WangEdge {
				wA, _ := ws.WangIDForTile(m.TileAt(0, 2, 2) - firstGID)
				wB, _ := ws.WangIDForTile(m.TileAt(0, 3, 1) - firstGID)
				if wA[tilemap.WangTopRight] != wB[tilemap.WangBottomLeft] {
					t.Fatalf("iso shared Right/Left vertex mismatch: %d vs %d", wA[tilemap.WangTopRight], wB[tilemap.WangBottomLeft])
				}
			}
		})
	}
}

func TestWangChunkedLayerNegativeCoords(t *testing.T) {
	const (
		firstGID = uint32(1)
		G        = uint8(1)
		D        = uint8(2)
	)
	ts, ws := makeCorner3Tileset(firstGID)
	m := tilemap.NewInfinite(16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", tilemap.DefaultChunkSize)}

	// Paint across the (-1,-1) / (0,0) chunk boundary.
	m.SetTerrainRect(0, -3, -3, 6, 6, ws, G, nil)
	m.SetTerrain(0, -1, -1, ws, D, nil)
	m.SetTerrain(0, 0, 0, ws, D, nil)

	assertSeamlessWangGrid(t, m, 0, ws, firstGID, -3, -3, 3, 3)

	// All four quadrants around (0, 0) have modified chunks.
	if dirty := m.Layers[0].DirtyChunks(); len(dirty) != 4 {
		t.Fatalf("dirty chunks across origin = %v (len %d), want 4", dirty, len(dirty))
	}
	// Empty cell outside painted rect must stay 0 because tileset has no 0-color transitions.
	if got := m.TileAt(0, -4, -4); got != 0 {
		t.Fatalf("outside empty cell (-4,-4) = %d, want 0", got)
	}
}

func TestWangProbabilitySelectionAndDeterminism(t *testing.T) {
	// 4 variants of solid Grass with probabilities 10, 3, 1, and explicit 0.
	tmj := `{
		"width": 20,
		"height": 20,
		"tilewidth": 16,
		"tileheight": 16,
		"tilesets": [{
			"firstgid": 1,
			"name": "variants",
			"tilewidth": 16,
			"tileheight": 16,
			"columns": 4,
			"tilecount": 4,
			"tiles": [
				{"id": 0, "probability": 0},
				{"id": 1, "probability": 10},
				{"id": 2, "probability": 3},
				{"id": 3, "probability": 1}
			],
			"wangsets": [{
				"name": "GrassVariants",
				"type": "corner",
				"colors": [{"name": "Grass", "color": "#00ff00"}],
				"wangtiles": [
					{"tileid": 0, "wangid": [0, 1, 0, 1, 0, 1, 0, 1]},
					{"tileid": 1, "wangid": [0, 1, 0, 1, 0, 1, 0, 1]},
					{"tileid": 2, "wangid": [0, 1, 0, 1, 0, 1, 0, 1]},
					{"tileid": 3, "wangid": [0, 1, 0, 1, 0, 1, 0, 1]}
				]
			}]
		}],
		"layers": [{"name": "g", "type": "tilelayer", "width": 20, "height": 20, "data": [` + repeatZero(400) + `]}]
	}`

	load := func() (*tilemap.Tilemap, *tilemap.WangSet) {
		m, err := tilemap.LoadTiled([]byte(tmj), tilemap.TilesetLoader{})
		if err != nil {
			t.Fatalf("LoadTiled: %v", err)
		}
		return m, m.WangSet("GrassVariants")
	}

	// Same seed produces bit-identical grids.
	m1, ws1 := load()
	m2, ws2 := load()
	m1.SetTerrainRect(0, 0, 0, 20, 20, ws1, 1, random.New(12345))
	m2.SetTerrainRect(0, 0, 0, 20, 20, ws2, 1, random.New(12345))
	counts := map[uint32]int{}
	for r := range 20 {
		for c := range 20 {
			g1, g2 := m1.TileAt(0, c, r), m2.TileAt(0, c, r)
			if g1 != g2 {
				t.Fatalf("same seed mismatch at (%d,%d): %d != %d", c, r, g1, g2)
			}
			counts[g1]++
		}
	}
	// TileID 0 (GID 1) has explicit probability 0, so it must never be picked.
	if counts[1] != 0 {
		t.Fatalf("probability-0 tile picked %d times, want 0", counts[1])
	}
	// Expected roughly 286 : 86 : 29 across 400 cells for weights 10 : 3 : 1.
	if !(counts[2] > counts[3] && counts[3] > counts[4] && counts[4] > 0) {
		t.Fatalf("unexpected weighted counts (want GID2 > GID3 > GID4 > 0): %v", counts)
	}

	// nil rng deterministically selects lowest positive-weight TileID (TileID 1 -> GID 2).
	mNil, wsNil := load()
	mNil.SetTerrainRect(0, 0, 0, 4, 4, wsNil, 1, nil)
	for r := range 4 {
		for c := range 4 {
			if got := mNil.TileAt(0, c, r); got != 2 {
				t.Fatalf("nil rng at (%d,%d) = %d, want lowest positive-weight GID 2", c, r, got)
			}
		}
	}
}

func repeatZero(n int) string {
	b := make([]byte, 0, n*2)
	for i := range n {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '0')
	}
	return string(b)
}

func TestWangZeroColorAndErase(t *testing.T) {
	// 15-tile corner set with 1=Grass and 0=Empty (no all-zero tile).
	ws := tilemap.WangSet{
		Name:     "GrassOnEmpty",
		Type:     tilemap.WangCorner,
		FirstGID: 1,
		Colors:   []tilemap.WangColor{{Name: "Grass", Probability: 1}},
	}
	for mask := 1; mask < 16; mask++ {
		var wid tilemap.WangID
		if mask&1 != 0 {
			wid[tilemap.WangTopRight] = 1
		}
		if mask&2 != 0 {
			wid[tilemap.WangBottomRight] = 1
		}
		if mask&4 != 0 {
			wid[tilemap.WangBottomLeft] = 1
		}
		if mask&8 != 0 {
			wid[tilemap.WangTopLeft] = 1
		}
		ws.WangTiles = append(ws.WangTiles, tilemap.WangTile{TileID: uint32(mask), WangID: wid})
	}
	ts := tilemap.NewGridTileset(1, core.Src("asset:island.png"), 16, 16, 4, 16)
	ts.WangSets = []tilemap.WangSet{ws}
	m := tilemap.New(5, 5, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 5, 5, nil)}

	// Painting (2, 2) on an empty map fills (2,2) and its 8 Grass-to-Empty neighbors.
	m.SetTerrain(0, 2, 2, &ts.WangSets[0], 1, nil)
	if got := m.TileAt(0, 2, 2); got != 1+15 {
		t.Fatalf("(2,2) = %d, want %d", got, 1+15)
	}
	if got := m.TileAt(0, 3, 3); got != 1+8 { // TL=1
		t.Fatalf("(3,3) = %d, want %d (TL=1)", got, 1+8)
	}
	if got := m.TileAt(0, 0, 0); got != 0 {
		t.Fatalf("(0,0) = %d, want 0", got)
	}
	assertSeamlessWangGrid(t, m, 0, &ts.WangSets[0], 1, 0, 0, 5, 5)

	// Erasing (2, 2) with terrain=0 clears (2, 2) AND all 8 orphaned transition neighbors back to 0.
	m.SetTerrain(0, 2, 2, &ts.WangSets[0], 0, nil)
	for r := range 5 {
		for c := range 5 {
			if got := m.TileAt(0, c, r); got != 0 {
				t.Errorf("after erase, (%d,%d) = %d, want 0", c, r, got)
			}
		}
	}
}

func TestWangFlippedExistingTilesAndNonWangProtection(t *testing.T) {
	const (
		firstGID = uint32(1)
		G        = uint8(1)
		D        = uint8(2)
		propGID  = uint32(200)
	)
	ts, ws := makeCorner3Tileset(firstGID)
	m := tilemap.New(4, 3, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 4, 3, nil)}
	m.SetTerrainRect(0, 0, 0, 4, 3, ws, G, nil)

	// Place a non-Wang prop tile at (0, 1) and a horizontally flipped Grass-Dirt tile at (2, 1):
	// Unflipped (TL=G, TR=D, BR=D, BL=G) flipped horizontally becomes (TL=D, TR=G, BR=G, BL=D).
	m.SetTile(0, 0, 1, propGID)
	m.SetTile(0, 2, 1, cornerTileGID(firstGID, G, D, D, G)|tilemap.FlipHorizontal)

	// Painting Dirt at (1, 1) must:
	// 1. Not overwrite non-Wang propGID at (0, 1).
	// 2. See that flipped (2, 1) already has TL=D, BL=D on its left edge, so leave it intact!
	m.SetTerrain(0, 1, 1, ws, D, nil)
	if got := m.TileAt(0, 0, 1); got != propGID {
		t.Fatalf("non-Wang prop tile overwritten: got %d, want %d", got, propGID)
	}
	wantFlipped := cornerTileGID(firstGID, G, D, D, G) | tilemap.FlipHorizontal
	if got := m.TileAt(0, 2, 1); got != wantFlipped {
		t.Fatalf("matching flipped neighbor modified: got %#x, want %#x", got, wantFlipped)
	}
}

func TestWangNonFiniteAndNegativeProbabilitiesIgnored(t *testing.T) {
	ws := tilemap.WangSet{
		Name:     "BadWeights",
		Type:     tilemap.WangCorner,
		FirstGID: 1,
		Colors: []tilemap.WangColor{
			{Name: "Grass", Probability: math.NaN()},
		},
		WangTiles: []tilemap.WangTile{
			{TileID: 0, WangID: tilemap.WangID{0, 1, 0, 1, 0, 1, 0, 1}, Probability: math.Inf(1)},
			{TileID: 1, WangID: tilemap.WangID{0, 1, 0, 1, 0, 1, 0, 1}, Probability: math.NaN()},
			{TileID: 2, WangID: tilemap.WangID{0, 1, 0, 1, 0, 1, 0, 1}, Probability: -3},
			{TileID: 3, WangID: tilemap.WangID{0, 1, 0, 1, 0, 1, 0, 1}, Probability: 2},
		},
	}
	ts := tilemap.NewGridTileset(1, core.Src("asset:badweights.png"), 16, 16, 4, 4)
	ts.WangSets = []tilemap.WangSet{ws}
	m := tilemap.New(4, 4, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("g", 4, 4, nil)}

	// With rng, only TileID 3 (GID 4) has a valid positive finite weight.
	m.SetTerrainRect(0, 0, 0, 4, 4, &ts.WangSets[0], 1, random.New(7))
	for r := range 4 {
		for c := range 4 {
			if got := m.TileAt(0, c, r); got != 4 {
				t.Fatalf("rng pick at (%d,%d) = %d, want GID 4", c, r, got)
			}
		}
	}

	// With nil rng, lowest positive finite weight TileID is also TileID 3 (GID 4).
	m.SetTerrainRect(0, 0, 0, 4, 4, &ts.WangSets[0], 1, nil)
	for r := range 4 {
		for c := range 4 {
			if got := m.TileAt(0, c, r); got != 4 {
				t.Fatalf("nil rng pick at (%d,%d) = %d, want GID 4", c, r, got)
			}
		}
	}
}

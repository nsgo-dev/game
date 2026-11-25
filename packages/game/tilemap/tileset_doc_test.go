package tilemap_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTilesetFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*tilemap.TilesetDoc]{
		Decode: tilemap.ParseTileset,
		Encode: tilemap.EncodeTileset,
		Files:  []string{"testdata/*.tileset.yaml"},
		Doc:    "tilemap#tileset",
	})
}

func loadTilesetDoc(t testing.TB, name string) *tilemap.TilesetDoc {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := tilemap.ParseTileset(data)
	if err != nil {
		t.Fatalf("ParseTileset(%s): %v", name, err)
	}
	return doc
}

func TestAddTilesetDocGridTileset(t *testing.T) {
	doc := loadTilesetDoc(t, "cave.tileset.yaml")
	m := tilemap.New(4, 2, 16, 16)
	var asked []string
	ts, err := m.AddTilesetDoc(doc, 0, tilemap.TilesetAssets{Image: func(p string) (core.ImageSource, error) {
		asked = append(asked, p)
		return core.ImageSource{URI: "asset:" + p}, nil
	}})
	if err != nil {
		t.Fatalf("AddTilesetDoc: %v", err)
	}
	if !reflect.DeepEqual(asked, []string{"tiles/cave.png"}) {
		t.Fatalf("images asked for = %v", asked)
	}
	if ts.FirstGID != 1 || ts.Name != "cave" || ts.Image.URI != "asset:tiles/cave.png" || ts.Columns != 4 || ts.TileCount != 8 || ts.Spacing != 2 || ts.Margin != 1 || ts.TileWidth != 16 || ts.TileHeight != 16 {
		t.Fatalf("tileset = %+v", ts)
	}
	if len(m.Tilesets) != 1 || m.Tilesets[0] != ts {
		t.Fatalf("map tilesets = %v", m.Tilesets)
	}
	if got := ts.Properties["scale"]; got.Type != "float" || got.Value != 2.0 {
		t.Fatalf("scale property = %+v", got)
	}
	if got := ts.Properties["depth"]; got.Type != "int" || got.Value != 3 {
		t.Fatalf("depth property = %+v", got)
	}
	if got, _ := ts.Properties.Bool("lit"); got {
		t.Fatal("lit property is true")
	}
	if f, ok := m.TileProperties(2).Float("friction"); !ok || f != 0.5 {
		t.Fatalf("tile 1 friction = %v, %v", f, ok)
	}

	// Flags: tiles 0 and 1 are solid, 3 one-way, 6 carries a custom bit.
	for gid, want := range map[uint32]tilemap.TileFlags{1: tilemap.TileSolid, 2: tilemap.TileSolid, 3: 0, 4: tilemap.TileOneWay, 7: 4} {
		if got := m.Flags(gid); got != want {
			t.Errorf("Flags(%d) = %v, want %v", gid, got, want)
		}
	}

	slope := m.TileShapes(3)
	if len(slope) != 1 || slope[0].Shape != tilemap.ObjectPolygon || slope[0].Name != "slope" || !reflect.DeepEqual(slope[0].Points, []core.Point{{X: 0, Y: 16}, {X: 16, Y: 0}, {X: 16, Y: 16}}) {
		t.Fatalf("slope shapes = %+v", slope)
	}
	water := m.TileShapes(7)
	if len(water) != 3 {
		t.Fatalf("tile 6 has %d shapes", len(water))
	}
	if water[0].Shape != tilemap.ObjectRect || water[0].Type != "water" || !reflect.DeepEqual(water[0].Points, []core.Point{{X: 0, Y: 8}, {X: 16, Y: 8}, {X: 16, Y: 16}, {X: 0, Y: 16}}) {
		t.Fatalf("rect shape = %+v", water[0])
	}
	if d, _ := water[0].Properties.Float("depth"); d != 1 || water[0].Properties["depth"].Type != "float" {
		t.Fatalf("rect shape depth = %+v", water[0].Properties["depth"])
	}
	if water[1].Shape != tilemap.ObjectEllipse || len(water[1].Points) != tilemap.EllipseShapeSegments || water[1].Points[0] != (core.Point{X: 12, Y: 8}) {
		t.Fatalf("ellipse shape = %+v", water[1])
	}
	if water[2].Shape != tilemap.ObjectPolyline || water[2].Closed() {
		t.Fatalf("polyline shape = %+v", water[2])
	}
	// A flipped slope is mirrored like its image.
	if got := m.TileShapes(3 | tilemap.FlipHorizontal); !reflect.DeepEqual(got[0].Points, []core.Point{{X: 16, Y: 16}, {X: 0, Y: 0}, {X: 0, Y: 16}}) {
		t.Fatalf("flipped slope = %+v", got[0].Points)
	}

	anim := ts.Animations[4]
	if anim == nil || len(anim.Frames) != 2 || anim.Frames[1] != (tilemap.TileAnimFrame{TileID: 5, Duration: 100 * time.Millisecond}) {
		t.Fatalf("animation = %+v", anim)
	}
	m.Update(250 * time.Millisecond)
	if got := anim.CurrentTileID(); got != 5 {
		t.Fatalf("after 250ms the animation shows tile %d, want 5", got)
	}

	rock := m.Terrain("rock")
	if rock == nil || rock.Autotile.Mode != tilemap.Autotile4 || rock.Autotile.Default != 1 || rock.Autotile.Tile(tilemap.MaskEast) != 2 || rock.Autotile.Tile(tilemap.MaskWest) != 1 {
		t.Fatalf("rock terrain = %+v", rock)
	}
	if !rock.Contains(2|tilemap.FlipVertical) || rock.Contains(3) || rock.Contains(0) {
		t.Fatal("rock terrain membership is wrong")
	}
	if m.Terrain("sand") != nil {
		t.Fatal("unknown terrain found")
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 4, 2, []uint32{1, 1, 0, 1, 0, 0, 0, 0}))
	m.ApplyAutotile(0, rock.Autotile, rock.Contains)
	if got := m.Layers[0].Data[:4]; !reflect.DeepEqual(got, []uint32{2, 1, 0, 1}) {
		t.Fatalf("autotiled row = %v, want [2 1 0 1]", got)
	}
}

func TestAddTilesetDocAtlasTileset(t *testing.T) {
	doc := loadTilesetDoc(t, "props.tileset.yaml")
	atlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:props.png"},
		Frames: map[string]sprite.Frame{
			"barrel":  {Name: "barrel", X: 0, W: 16, H: 32, Width: 16, Height: 32},
			"torch_1": {Name: "torch_1", X: 16, W: 16, H: 32, Width: 16, Height: 32},
			"torch_2": {Name: "torch_2", X: 32, W: 16, H: 32, Width: 16, Height: 32},
		},
	}
	m := tilemap.New(2, 1, 16, 16)
	if _, err := m.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{}); err == nil || !strings.Contains(err.Error(), "TilesetAssets.Atlas") {
		t.Fatalf("AddTilesetDoc without an atlas resolver: %v", err)
	}
	ts, err := m.AddTilesetDoc(doc, 10, tilemap.TilesetAssets{Atlas: func(name string) (*sprite.Atlas, error) {
		if name != "props" {
			return nil, errors.New("no such atlas")
		}
		return atlas, nil
	}})
	if err != nil {
		t.Fatalf("AddTilesetDoc: %v", err)
	}
	if ts.FirstGID != 10 || len(ts.Frames) != 3 || ts.Frames[2].X != 32 || ts.TileHeight != 32 || ts.Atlas != atlas {
		t.Fatalf("tileset = %+v", ts)
	}
	if got, local := m.TilesetForGID(11); got != ts || local != 1 {
		t.Fatalf("TilesetForGID(11) = %v, %d", got, local)
	}
	// A tileset whose IDs overlap one the map has is refused.
	if _, err := m.AddTilesetDoc(doc, 12, tilemap.TilesetAssets{Atlas: func(string) (*sprite.Atlas, error) { return atlas, nil }}); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping tileset: %v", err)
	}
	missing := *doc
	missing.Frames = []string{"barrel", "lamp"}
	missing.Tiles = nil
	if _, err := tilemap.New(1, 1, 16, 16).AddTilesetDoc(&missing, 1, tilemap.TilesetAssets{Atlas: func(string) (*sprite.Atlas, error) { return atlas, nil }}); err == nil || !strings.Contains(err.Error(), `no frame "lamp"`) {
		t.Fatalf("missing frame: %v", err)
	}
}

func TestTilesetFrame(t *testing.T) {
	doc := loadTilesetDoc(t, "cave.tileset.yaml")
	f, ok := tilemap.TilesetFrame(doc, nil, 5)
	// Tile 5 is in column 1 of row 1: margin 1, then one tile of 16 and a spacing of 2.
	if !ok || f.X != 19 || f.Y != 19 || f.W != 16 || f.H != 16 || f.Width != 16 {
		t.Fatalf("TilesetFrame(5) = %+v, %v", f, ok)
	}
	if _, ok := tilemap.TilesetFrame(doc, nil, 8); ok {
		t.Fatal("tile 8 is outside the tileset")
	}
	props := loadTilesetDoc(t, "props.tileset.yaml")
	if _, ok := tilemap.TilesetFrame(props, nil, 0); ok {
		t.Fatal("an atlas tileset with no atlas has no frames")
	}
}

func TestEncodeTilesetKeepsFloatKinds(t *testing.T) {
	doc := &tilemap.TilesetDoc{
		Version: 1, TileWidth: 8, TileHeight: 8, Image: "t.png", Columns: 1, TileCount: 1,
		Properties: map[string]any{"whole": 2.0, "count": 2},
	}
	data, err := tilemap.EncodeTileset(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("whole: 2.0\n")) || !bytes.Contains(data, []byte("count: 2\n")) {
		t.Fatalf("encoded:\n%s", data)
	}
	back, err := tilemap.ParseTileset(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, doc) {
		t.Fatalf("round trip = %+v, want %+v", back, doc)
	}
	if doc.Properties["whole"] != 2.0 {
		t.Fatal("EncodeTileset changed the document")
	}
}

func TestParseTilesetErrors(t *testing.T) {
	const head = "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: t.png\ncolumns: 2\ntileCount: 4\n"
	cases := []struct {
		name, yaml, field, want string
	}{
		{"empty", "", "", "empty document"},
		{"unknown_field", head + "bogus: 1\n", "bogus", "bogus"},
		{"zero_tile_width", "version: 1\ntileWidth: 0\ntileHeight: 16\nimage: t.png\ncolumns: 1\ntileCount: 1\n", "tileWidth", "tileWidth 0"},
		{"no_source", "version: 1\ntileWidth: 16\ntileHeight: 16\n", "image", "needs an image or an atlas"},
		{"both_sources", head + "atlas: a\n", "atlas", "cannot both"},
		{"bad_image_path", "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: ../t.png\ncolumns: 1\ntileCount: 1\n", "image", "slash-separated"},
		{"no_columns", "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: t.png\ntileCount: 1\n", "columns", "columns 0"},
		{"no_tile_count", "version: 1\ntileWidth: 16\ntileHeight: 16\nimage: t.png\ncolumns: 1\n", "tileCount", "tileCount 0"},
		{"negative_spacing", head + "spacing: -1\n", "spacing", "spacing -1"},
		{"image_frames", head + "frames: [a]\n", "frames", "frames need atlas"},
		{"atlas_columns", "version: 1\ntileWidth: 16\ntileHeight: 16\natlas: a\ncolumns: 2\nframes: [a]\n", "columns", "columns needs image"},
		{"atlas_no_frames", "version: 1\ntileWidth: 16\ntileHeight: 16\natlas: a\n", "frames", "needs from 1"},
		{"duplicate_frame", "version: 1\ntileWidth: 16\ntileHeight: 16\natlas: a\nframes: [a, b, a]\n", "frames[2]", "is tile 0 and tile 2"},
		{"empty_frame", "version: 1\ntileWidth: 16\ntileHeight: 16\natlas: a\nframes: [a, '']\n", "frames[1]", "empty name"},
		{"list_property", head + "properties:\n  tags: [a]\n", "properties.tags", "[]interface {}"},
		{"tile_out_of_range", head + "tiles:\n  - id: 4\n", "tiles[0].id", "outside the tileset's 4 tiles"},
		{"tiles_out_of_order", head + "tiles:\n  - id: 2\n  - id: 1\n", "tiles[1].id", "comes after tile 2"},
		{"duplicate_tile", head + "tiles:\n  - id: 1\n  - id: 1\n", "tiles[1].id", "comes after tile 1"},
		{"bad_shape_kind", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: circle\n", "tiles[0].shapes[0].kind", "unknown shape kind"},
		{"rect_no_size", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: rect\n        width: 4\n", "tiles[0].shapes[0].height", "height 0"},
		{"rect_points", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: rect\n        width: 4\n        height: 4\n        points: [[0, 0]]\n", "tiles[0].shapes[0].points", "no points"},
		{"polygon_two_points", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: polygon\n        points: [[0, 0], [1, 1]]\n", "tiles[0].shapes[0].points", "has 2 points"},
		{"polygon_box", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: polyline\n        width: 3\n        points: [[0, 0], [1, 1]]\n", "tiles[0].shapes[0].width", "no width"},
		{"bad_point", head + "tiles:\n  - id: 0\n    shapes:\n      - kind: polyline\n        points: [[0, 0], [1]]\n", "tiles[0].shapes[0].points[1]", "[x, y] pair"},
		{"frame_out_of_range", head + "tiles:\n  - id: 0\n    animation:\n      - tile: 9\n        duration: 1s\n", "tiles[0].animation[0].tile", "shows tile 9"},
		{"frame_without_tile", head + "tiles:\n  - id: 0\n    animation:\n      - duration: 100ms\n", "tiles[0].animation[0].tile", "has no tile"},
		{"frame_null_tile", head + "tiles:\n  - id: 0\n    animation:\n      - tile: null\n        duration: 100ms\n", "tiles[0].animation[0].tile", "has no tile"},
		{"zero_duration", head + "tiles:\n  - id: 0\n    animation:\n      - tile: 1\n        duration: 0s\n", "tiles[0].animation[0].duration", "duration 0s"},
		{"neighbors_without_terrain", head + "tiles:\n  - id: 0\n    neighbors: [north]\n", "tiles[0].neighbors", "without a terrain"},
		{"unknown_terrain", head + "tiles:\n  - id: 0\n    terrain: sand\n", "tiles[0].terrain", `unknown terrain "sand"`},
		{"bad_terrain_mode", head + "terrains:\n  - name: rock\n    mode: wang\n", "terrains[0].mode", `mode "wang"`},
		{"terrain_default_out_of_range", head + "terrains:\n  - name: rock\n    mode: cardinal\n    default: 4\n", "terrains[0].default", "outside the tileset's 4 tiles"},
		{"terrain_default_negative", head + "terrains:\n  - name: rock\n    mode: cardinal\n    default: -1\n", "terrains[0].default", "outside the tileset's 4 tiles"},
		{"duplicate_terrain", head + "terrains:\n  - name: rock\n    mode: blob\n  - name: rock\n    mode: blob\n", "terrains[1].name", "duplicate terrain"},
		{"unknown_neighbor", head + "terrains:\n  - name: rock\n    mode: blob\ntiles:\n  - id: 0\n    terrain: rock\n    neighbors: [up]\n", "tiles[0].neighbors[0]", `unknown neighbor "up"`},
		{"corner_in_cardinal", head + "terrains:\n  - name: rock\n    mode: cardinal\ntiles:\n  - id: 0\n    terrain: rock\n    neighbors: [northeast]\n", "tiles[0].neighbors[0]", "corner"},
		{"blob_corner_without_edges", head + "terrains:\n  - name: rock\n    mode: blob\ntiles:\n  - id: 0\n    terrain: rock\n    neighbors: [north, northeast]\n", "tiles[0].neighbors[1]", "without both edges"},
		{"neighbor_twice", head + "terrains:\n  - name: rock\n    mode: cardinal\ntiles:\n  - id: 0\n    terrain: rock\n    neighbors: [north, north]\n", "tiles[0].neighbors[1]", "twice"},
		{"same_neighbors", head + "terrains:\n  - name: rock\n    mode: cardinal\ntiles:\n  - id: 0\n    terrain: rock\n    neighbors: [north, east]\n  - id: 1\n    terrain: rock\n    neighbors: [east, north]\n", "tiles[1].neighbors", "tiles 0 and 1"},
		{"trailing_document", head + "---\nversion: 1\n", "", "trailing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tilemap.ParseTileset([]byte(tc.yaml))
			if err == nil {
				t.Fatal("ParseTileset succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %T is not a *loaderr.Error", err)
			}
			if tc.field != "" && le.Field != tc.field {
				t.Fatalf("field = %q, want %q (%v)", le.Field, tc.field, err)
			}
			if tc.yaml != "" && le.Line == 0 {
				t.Fatalf("error has no line: %v", err)
			}
		})
	}
}

func TestLoadTilesetFSNamesTheFile(t *testing.T) {
	_, err := tilemap.LoadTilesetFS(os.DirFS("testdata"), "cave.tileset.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tilemap.LoadTilesetFS(os.DirFS("testdata"), "sample.tmj"); err == nil || !strings.HasPrefix(err.Error(), "sample.tmj:") {
		t.Fatalf("error does not name the file: %v", err)
	}
}

func TestTerrainDocDefaultAndRuntimeHelpers(t *testing.T) {
	doc := loadTilesetDoc(t, "terrain_blob.tileset.yaml")
	// Explicit default overrides mask-0 fallback when set to another tile.
	def := 5
	custom := doc.Clone()
	custom.Terrains[0].Default = &def
	if *doc.Terrains[0].Default != 0 {
		t.Fatal("Clone shared TerrainDoc.Default pointer")
	}

	tset := custom.Terrain("grass", 1)
	if tset == nil || tset.Autotile.Mode != tilemap.Autotile8 || tset.Autotile.Default != 6 {
		t.Fatalf("custom.Terrain = %+v, want Autotile8 with Default 6", tset)
	}
	if !tset.Contains(6) || !tset.Contains(1) || tset.Contains(15) {
		t.Fatalf("Contains check failed for custom terrain")
	}

	m := tilemap.New(3, 3, 16, 16)
	if _, err := m.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{}); err != nil {
		t.Fatalf("AddTilesetDoc: %v", err)
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 3, 3, make([]uint32, 9)))

	// Place a 3x3 blob via SetTerrainTile and compare against ApplyTerrain on a fresh map.
	for r := range 3 {
		for c := range 3 {
			if !m.SetTerrainTile(0, c, r, "grass", true) {
				t.Fatalf("SetTerrainTile(%d,%d) returned false", c, r)
			}
		}
	}
	ref := tilemap.New(3, 3, 16, 16)
	if _, err := ref.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{}); err != nil {
		t.Fatalf("AddTilesetDoc ref: %v", err)
	}
	ref.Layers = append(ref.Layers, tilemap.NewTileLayer("ground", 3, 3, []uint32{1, 1, 1, 1, 1, 1, 1, 1, 1}))
	if !ref.ApplyTerrain(0, "grass") {
		t.Fatal("ApplyTerrain returned false")
	}
	if !reflect.DeepEqual(m.Layers[0].Data, ref.Layers[0].Data) {
		t.Fatalf("SetTerrainTile 3x3 = %v, want ApplyTerrain %v", m.Layers[0].Data, ref.Layers[0].Data)
	}
	// Center cell (1,1) should be local tile 5 -> GID 6.
	if got := m.TileAt(0, 1, 1); got != 6 {
		t.Fatalf("center GID = %d, want 6", got)
	}
	if m.ApplyTerrain(0, "unknown") || m.SetTerrainTile(0, 0, 0, "unknown", true) {
		t.Fatal("unknown terrain should return false")
	}
}

func TestTerrainMaskHelpers(t *testing.T) {
	// Autotile4 allows 16 masks; Autotile8 allows 47 corner-gated masks.
	c4, c8 := 0, 0
	for m := range 256 {
		if tilemap.ValidTerrainMask(tilemap.Autotile4, uint8(m)) {
			c4++
		}
		if tilemap.ValidTerrainMask(tilemap.Autotile8, uint8(m)) {
			c8++
		}
	}
	if c4 != 16 || c8 != 47 {
		t.Fatalf("valid mask counts = (%d, %d), want (16, 47)", c4, c8)
	}

	// Enabling a corner in blob mode auto-enables its adjacent edges; disabling an edge clears dependent corners.
	mask := tilemap.ToggleTerrainMask(tilemap.Autotile8, 0, tilemap.MaskNorthEast)
	want := tilemap.MaskNorth | tilemap.MaskEast | tilemap.MaskNorthEast
	if mask != want {
		t.Fatalf("Toggle(0, NE) = %#x, want %#x", mask, want)
	}
	names := tilemap.TerrainMaskNeighbors(mask)
	if !reflect.DeepEqual(names, []string{"north", "east", "northeast"}) {
		t.Fatalf("TerrainMaskNeighbors = %v", names)
	}
	parsed, err := tilemap.TerrainNeighborsMask(names)
	if err != nil || parsed != mask {
		t.Fatalf("TerrainNeighborsMask = %#x, %v", parsed, err)
	}
	// Disabling north must clear northeast as well, leaving only east.
	mask = tilemap.ToggleTerrainMask(tilemap.Autotile8, mask, tilemap.MaskNorth)
	if mask != tilemap.MaskEast {
		t.Fatalf("Toggle(N|E|NE, N) = %#x, want %#x", mask, tilemap.MaskEast)
	}
	// In cardinal mode, diagonal bits are ignored.
	if got := tilemap.ToggleTerrainMask(tilemap.Autotile4, tilemap.MaskNorth, tilemap.MaskNorthEast); got != tilemap.MaskNorth {
		t.Fatalf("cardinal Toggle(N, NE) = %#x, want %#x", got, tilemap.MaskNorth)
	}
}

func FuzzParseTileset(f *testing.F) {
	for _, name := range []string{"cave.tileset.yaml", "props.tileset.yaml"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("version: 1\ntileWidth: 1\ntileHeight: 1\natlas: a\nframes: [a]\nproperties: {x: 1.0}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := tilemap.ParseTileset(data)
		if err != nil {
			return
		}
		out, err := tilemap.EncodeTileset(doc)
		if err != nil {
			t.Fatalf("EncodeTileset of a parsed tileset: %v", err)
		}
		back, err := tilemap.ParseTileset(out)
		if err != nil {
			t.Fatalf("ParseTileset of encoded tileset: %v\n%s", err, out)
		}
		again, err := tilemap.EncodeTileset(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("second encode differs (%v):\n%s\nwant:\n%s", err, again, out)
		}
		if doc.Count() <= 64 {
			m := tilemap.New(2, 2, float32(doc.TileWidth), float32(doc.TileHeight))
			atlas := &sprite.Atlas{Frames: map[string]sprite.Frame{}}
			for _, name := range doc.Frames {
				atlas.Frames[name] = sprite.Frame{W: 1, H: 1}
			}
			if _, err := m.AddTilesetDoc(doc, 1, tilemap.TilesetAssets{Atlas: func(string) (*sprite.Atlas, error) { return atlas, nil }}); err != nil {
				t.Fatalf("AddTilesetDoc of a valid tileset: %v", err)
			}
			m.Update(time.Second)
		}
	})
}

package sceneformat_test

import (
	"bytes"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// caveTileset is a YAML tileset of 8 tiles in a 4-column grid image: tile 0 is solid, tile 1 one-way, and tile 2 animates through tiles 2 and 3.
const caveTileset = `version: 1
name: cave
tileWidth: 16
tileHeight: 16
image: tiles/cave.png
columns: 4
tileCount: 8
tiles:
  - id: 0
    solid: true
  - id: 1
    oneWay: true
    properties:
      kind: ledge
  - id: 2
    animation:
      - tile: 2
        duration: 100ms
      - tile: 3
        duration: 100ms
`

// layeredScene places tiles of caveTileset in two layers of a 4 by 2 grid, seen through a 32 by 16 virtual viewport.
const layeredScene = `version: 2
name: Layered
virtualWidth: 32
virtualHeight: 16
camera:
  x: 16
  "y": 8
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 4
      height: 2
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 0 0 0
            - .  .  1h  .
        - name: deco
          rows:
            - 2 . . .
            - . . . .
`

// imageAssets is FSAssets that also names images with an "asset:" URI, to show Build resolves tileset images through an ImageResolver.
type imageAssets struct {
	sceneformat.FSAssets
	asked []string
}

func (a *imageAssets) Image(p string) (core.ImageSource, error) {
	a.asked = append(a.asked, p)
	return core.ImageSource{URI: "asset:" + p}, nil
}

func layeredProject() fstest.MapFS {
	return fstest.MapFS{
		"tilesets/cave.tileset.yaml": {Data: []byte(caveTileset)},
		"level.scene.yaml":           {Data: []byte(layeredScene)},
	}
}

func buildLayered(t *testing.T, fsys fs.FS) (*sceneformat.BuiltScene, *imageAssets) {
	t.Helper()
	data, err := fs.ReadFile(fsys, "level.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	assets := &imageAssets{FSAssets: sceneformat.FSAssets{FS: fsys}}
	built, err := sceneformat.Build(doc, assets)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return built, assets
}

func TestLayeredTilemapBuildsATilemap(t *testing.T) {
	built, assets := buildLayered(t, layeredProject())
	if !slices.Equal(assets.asked, []string{"tiles/cave.png"}) {
		t.Fatalf("images asked for = %v", assets.asked)
	}
	ground := built.Layer("ground")
	m := ground.Map
	if m == nil || ground.Tilemap != nil {
		t.Fatalf("ground has Map %v and Tilemap %v, want a map only", m, ground.Tilemap)
	}
	if len(m.Layers) != 2 || m.Layers[0].Name != "floor" || m.Layers[1].Name != "deco" || m.Width != 4 || m.Height != 2 || m.TileWidth != 16 {
		t.Fatalf("map = %+v", m)
	}
	if got := m.TileAt(0, 2, 1); got != 2|tilemap.FlipHorizontal {
		t.Fatalf("floor (2, 1) = %#x, want tile 1 flipped horizontally", got)
	}
	if got := m.FlagsAt(1, 0); got != tilemap.TileSolid {
		t.Fatalf("FlagsAt(1, 0) = %v, want solid", got)
	}
	if got := m.FlagsAt(2, 1); got != tilemap.TileOneWay {
		t.Fatalf("FlagsAt(2, 1) = %v, want one-way", got)
	}
	if got, _ := m.TileProperties(2).String("kind"); got != "ledge" {
		t.Fatalf("tile 1 kind = %q", got)
	}
	hit, ok := m.Raycast(core.Point{X: 8, Y: 28}, core.Point{X: 8, Y: -8}, tilemap.TileSolid)
	if !ok || hit.Col != 0 || hit.Row != 0 || hit.Point != (core.Point{X: 8, Y: 16}) || hit.Normal != (core.Point{X: 0, Y: 1}) {
		t.Fatalf("Raycast = %+v, %v", hit, ok)
	}
	if hits := m.TilesInRect(core.Bounds{Size: core.Size{Width: 64, Height: 32}}, tilemap.TileOneWay); len(hits) != 1 || hits[0].Col != 2 || hits[0].Row != 1 {
		t.Fatalf("one-way tiles = %+v", hits)
	}
}

func TestLayeredTilemapDrawsVisibleTilesAndAnimates(t *testing.T) {
	built, _ := buildLayered(t, layeredProject())
	img := core.ImageSource{URI: "asset:tiles/cave.png"}
	all, _ := buildLayered(t, layeredProject())
	all.DrawAllTiles = true
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				// The 32 by 16 view holds the first two cells of the top row: the floor's two solid tiles, then the deco layer's animated tile 2.
				d.DrawImageFrom(img, 0, 0, 16, 16, 0, 0, 16, 16)
				d.DrawImageFrom(img, 0, 0, 16, 16, 16, 0, 16, 16)
				d.DrawImageFrom(img, 32, 0, 16, 16, 0, 0, 16, 16)
			}},
			core.Canvas{TestID: "all", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				all.Draw(d)
			}},
		}}
	})
	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)
	// Drawing every tile adds the rest of the floor: two more solid tiles and the flipped ledge, drawn under a flip transform.
	if got := countImageDraws(app.Find("all").Ops); got != 6 {
		t.Fatalf("DrawAllTiles drew %d tiles, want 6", got)
	}

	built.Update(100 * time.Millisecond)
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.DrawImageFrom(img, 0, 0, 16, 16, 0, 0, 16, 16)
				d.DrawImageFrom(img, 0, 0, 16, 16, 16, 0, 16, 16)
				// After 100ms the animated tile shows tile 3, the next cell of the image.
				d.DrawImageFrom(img, 48, 0, 16, 16, 0, 0, 16, 16)
			}},
		}}
	})
	sameOps(t, app2.Find("scene").Ops, app2.Find("hand").Ops)
}

// countImageDraws counts the image draws among ops.
func countImageDraws(ops []core.DrawOp) int {
	var d core.Drawing
	d.DrawImageFrom(core.ImageSource{URI: "x"}, 0, 0, 1, 1, 0, 0, 1, 1)
	kind := d.Ops[0].Kind
	n := 0
	for _, op := range ops {
		if op.Kind == kind {
			n++
		}
	}
	return n
}

func TestLayeredTilemapRoundTripsCanonically(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(layeredScene))
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	// The aligned row is written with single spaces; the document itself keeps what it read.
	if !bytes.Contains(out, []byte("            - . . 1h .\n")) {
		t.Fatalf("encoded:\n%s", out)
	}
	if doc.Layers[0].Tilemap.TileLayers[0].Rows[1] != ".  .  1h  ." {
		t.Fatal("Encode changed the document")
	}
	back, err := sceneformat.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	again, err := sceneformat.Encode(back)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("second save differs (%v):\n%s", err, again)
	}
	clone := back.Clone()
	clone.Layers[0].Tilemap.TileLayers[0].Rows[0] = ". . . ."
	if back.Layers[0].Tilemap.TileLayers[0].Rows[0] != "0 0 0 0" {
		t.Fatal("Clone shares tile rows")
	}
}

func TestTileRows(t *testing.T) {
	cells, err := sceneformat.ParseTileRow("  .\t0  12vh 3dhv 007 ")
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{0, 1, 13 | tilemap.FlipHorizontal | tilemap.FlipVertical, 4 | tilemap.FlipMask, 8}
	if !slices.Equal(cells, want) {
		t.Fatalf("cells = %#x, want %#x", cells, want)
	}
	if got := sceneformat.FormatTileRow(cells); got != ". 0 12hv 3hvd 7" {
		t.Fatalf("FormatTileRow = %q", got)
	}
	for _, bad := range []string{"x", "1x", "h", "1hh", "-1", "268435455", "1.5"} {
		if _, err := sceneformat.ParseTileRow(bad); err == nil {
			t.Errorf("ParseTileRow(%q) succeeded", bad)
		}
	}
	l := &sceneformat.TileLayerDoc{Name: "floor", Rows: []string{"0 0", ".  1"}}
	if c, err := l.Cell(1, 1); err != nil || c != 2 {
		t.Fatalf("Cell(1, 1) = %d, %v", c, err)
	}
	if err := l.SetCell(0, 1, 5|tilemap.FlipVertical); err != nil {
		t.Fatal(err)
	}
	if l.Rows[1] != "4v 1" {
		t.Fatalf("row after SetCell = %q", l.Rows[1])
	}
	if err := l.SetCell(0, 1, tilemap.FlipVertical); err != nil || l.Rows[1] != ". 1" {
		t.Fatalf("a flip without a tile empties the cell: %q, %v", l.Rows[1], err)
	}
	if _, err := l.Cell(2, 0); err == nil {
		t.Fatal("Cell outside the row succeeded")
	}
	if err := l.SetCell(0, 2, 1); err == nil {
		t.Fatal("SetCell outside the layer succeeded")
	}
}

func TestLayeredTilemapValidation(t *testing.T) {
	const head = "version: 2\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 1\n"
	cases := []struct {
		name, yaml, want string
		line             int
	}{
		{"version_1", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 1\n      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows: [\". .\"]\n", "need version 2", 9},
		{"no_tileset_file", head + "      tileLayers:\n        - name: f\n          rows: [\". .\"]\n", "requires a tilesetFile", 4},
		{"bad_tileset_file", head + "      tilesetFile: ../t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows: [\". .\"]\n", "slash-separated", 9},
		{"no_layers", head + "      tilesetFile: t.tileset.yaml\n", "at least one tile layer", 4},
		{"with_data", head + "      tilesetFile: t.tileset.yaml\n      data: [1, 2]\n      tileLayers:\n        - name: f\n          rows: [\". .\"]\n", "cannot combine", 10},
		{"empty_layer_name", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - rows: [\". .\"]\n", "empty name", 11},
		{"duplicate_layer", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows: [\". .\"]\n        - name: f\n          rows: [\". .\"]\n", "two tile layers named", 13},
		{"bad_opacity", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          opacity: 2\n          rows: [\". .\"]\n", "opacity 2", 12},
		{"row_count", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows: [\". .\", \". .\"]\n", "has 2 rows, want 1", 12},
		{"cell_count", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows:\n            - \". . .\"\n", "has 3 cells, want 2", 13},
		{"bad_cell", head + "      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows:\n            - \". 2x\"\n", "cell 1", 13},
		{"level_without_ldtk", "version: 2\nlayers:\n  - name: a\n    tilemap:\n      source: m.tmj\n      level: L\n", "requires an LDtk", 6},
		{"level_alone", "version: 2\nlayers:\n  - name: a\n    tilemap:\n      level: L\n", "requires an LDtk source", 5},
		{"level_version_1", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      source: m.ldtk\n      level: L\n", "need version 2", 6},
		{"dimensions_overflow", "version: 2\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2147483647\n      height: 2147483647\n      tilesetFile: t.tileset.yaml\n      tileLayers:\n        - name: f\n          rows: [\".\"]\n        - name: g\n          rows: [\".\"]\n", "more than", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatal("Parse succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			if le, ok := loaderr.As(err); !ok || le.Line != tc.line {
				t.Fatalf("error %q is not at line %d", err, tc.line)
			}
		})
	}
}

func TestLayeredTilemapBuildErrors(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(layeredScene))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Build(doc, sceneformat.AtlasMap{}); err == nil || !strings.Contains(err.Error(), "requires a FileResolver") {
		t.Fatalf("Build without a FileResolver: %v", err)
	}
	fsys := layeredProject()
	fsys["tilesets/cave.tileset.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(caveTileset, "tileCount: 8", "tileCount: 2", 1))}
	if _, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys}); err == nil || !strings.Contains(err.Error(), "tileset.yaml") {
		t.Fatalf("Build with an invalid tileset: %v", err)
	}
	fsys["tilesets/cave.tileset.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(caveTileset, "tileCount: 8", "tileCount: 2", 1)[:strings.Index(caveTileset, "tiles:")] + "tiles:\n  - id: 0\n    solid: true\n")}
	if _, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys}); err == nil || !strings.Contains(err.Error(), "places tile 2, but tileset") {
		t.Fatalf("Build with a tile outside the tileset: %v", err)
	}
	delete(fsys, "tilesets/cave.tileset.yaml")
	if _, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys}); err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Build with a missing tileset: %v", err)
	}
}

// propsTSJ is an external Tiled tileset whose tile 0 is solid, beside its image.
const propsTSJ = `{"name": "props", "image": "props.png", "imagewidth": 32, "imageheight": 16, "tilewidth": 16, "tileheight": 16,
	"columns": 2, "tilecount": 2, "tiles": [{"id": 0, "properties": [{"name": "solid", "type": "bool", "value": true}]}]}`

// levelTMJ is a Tiled map that uses propsTSJ from another directory.
const levelTMJ = `{"width": 2, "height": 1, "tilewidth": 16, "tileheight": 16, "orientation": "orthogonal",
	"tilesets": [{"firstgid": 1, "source": "../tilesets/props.tsj"}],
	"layers": [{"type": "tilelayer", "name": "ground", "width": 2, "height": 1, "opacity": 1, "visible": true, "data": [1, 2]}]}`

func TestTiledSourceBuildsWithoutAResolver(t *testing.T) {
	fsys := fstest.MapFS{
		"maps/level.tmj":        {Data: []byte(levelTMJ)},
		"tilesets/props.tsj":    {Data: []byte(propsTSJ)},
		"scenes/tmj.scene.yaml": {Data: []byte("version: 1\nlayers:\n  - name: ground\n    tilemap:\n      source: maps/level.tmj\n")},
	}
	doc, err := sceneformat.Parse(fsys["scenes/tmj.scene.yaml"].Data)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := built.Layer("ground").Map
	if m == nil || len(m.Layers) != 1 || m.Layers[0].Name != "ground" {
		t.Fatalf("map = %+v", m)
	}
	if got := m.Tilesets[0].Image.URI; got != "tilesets/props.png" {
		t.Fatalf("tileset image = %q, want the path from the project root", got)
	}
	if m.FlagsAt(0, 0) != tilemap.TileSolid || m.FlagsAt(1, 0) != 0 {
		t.Fatal("the tileset's solid property did not reach the map")
	}

	// A map that does not load names its file.
	fsys["maps/level.tmj"] = &fstest.MapFile{Data: []byte(`{"width": 2, "height": 1, "tilewidth": 0, "tileheight": 16}`)}
	if _, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys}); err == nil || !strings.Contains(err.Error(), "maps/level.tmj:1:") {
		t.Fatalf("Build of a broken map: %v", err)
	}
	unknown, err := sceneformat.Parse([]byte("version: 1\nlayers:\n  - name: ground\n    tilemap:\n      source: maps/level.json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Build(unknown, sceneformat.FSAssets{FS: fsys}); err == nil || !strings.Contains(err.Error(), "is not a Tiled map") {
		t.Fatalf("Build of an unknown source: %v", err)
	}
}

// caveLDtk is an LDtk project of two levels, each with an IntGrid layer whose value 1 is solid.
const caveLDtk = `{
	"defaultGridSize": 16,
	"defs": {
		"tilesets": [{"uid": 10, "identifier": "Cave", "relPath": "cave.png", "pxWid": 32, "pxHei": 16, "tileGridSize": 16, "spacing": 0, "padding": 0, "__cWid": 2, "__cHei": 1}],
		"layers": [{"uid": 20, "identifier": "Walls", "type": "IntGrid", "gridSize": 16, "intGridValues": [{"value": 1, "identifier": "solid", "color": "#FFFFFF"}]}]
	},
	"levels": [
		{"identifier": "Level_0", "uid": 0, "worldX": 0, "worldY": 0, "pxWid": 32, "pxHei": 16, "layerInstances": [
			{"__identifier": "Walls", "__type": "IntGrid", "__cWid": 2, "__cHei": 1, "__gridSize": 16, "__opacity": 1, "layerDefUid": 20, "visible": true, "intGridCsv": [1, 0]}
		]},
		{"identifier": "Level_1", "uid": 1, "worldX": 0, "worldY": 0, "pxWid": 32, "pxHei": 16, "layerInstances": [
			{"__identifier": "Walls", "__type": "IntGrid", "__cWid": 2, "__cHei": 1, "__gridSize": 16, "__opacity": 1, "layerDefUid": 20, "visible": true, "intGridCsv": [0, 1]}
		]}
	]
}`

func TestLDtkSourceBuildsTheNamedLevel(t *testing.T) {
	fsys := fstest.MapFS{"maps/caves.ldtk": {Data: []byte(caveLDtk)}}
	for _, tc := range []struct {
		level     string
		solidCol  int
		emptyCol  int
		wantError string
	}{
		{level: "", solidCol: 0, emptyCol: 1},
		{level: "Level_1", solidCol: 1, emptyCol: 0},
		{level: "Level_9", wantError: `level "Level_9" not found`},
	} {
		src := "version: 2\nlayers:\n  - name: world\n    tilemap:\n      source: maps/caves.ldtk\n"
		if tc.level != "" {
			src += "      level: " + tc.level + "\n"
		}
		doc, err := sceneformat.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		built, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys})
		if tc.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("level %q: Build error %v, want %q", tc.level, err, tc.wantError)
			}
			continue
		}
		if err != nil {
			t.Fatalf("level %q: Build: %v", tc.level, err)
		}
		m := built.Layer("world").Map
		if m.FlagsAt(tc.solidCol, 0) != tilemap.TileSolid || m.FlagsAt(tc.emptyCol, 0) != 0 {
			t.Fatalf("level %q: flags %v %v", tc.level, m.FlagsAt(0, 0), m.FlagsAt(1, 0))
		}
	}
}

func TestTilemapResolverStillWins(t *testing.T) {
	// A resolver that implements TilemapResolver loads sources itself, even when it also reads files.
	res := resolverOf{
		ext: externalResolver{AtlasMap: caveResolver, maps: map[string]*sceneformat.TilemapDoc{
			"maps/level.tmj": {TileWidth: 16, TileHeight: 16, Width: 1, Height: 1, Tileset: "cave_tiles", Data: []int{1}},
		}},
		fs: sceneformat.FSAssets{FS: fstest.MapFS{}},
	}
	doc, err := sceneformat.Parse([]byte("version: 1\nlayers:\n  - name: ground\n    tilemap:\n      source: maps/level.tmj\n"))
	if err != nil {
		t.Fatal(err)
	}
	built, err := sceneformat.Build(doc, res)
	if err != nil {
		t.Fatal(err)
	}
	if l := built.Layer("ground"); l.Tilemap == nil || l.Map != nil {
		t.Fatalf("ground = %+v, want the resolver's sprite batch", l)
	}
}

func TestTilemapResolverLeavesLDtkLevelsToBuild(t *testing.T) {
	// Tilemap(source) has no level parameter, so a source naming an LDtk level is read through the FileResolver.
	res := resolverOf{
		ext: externalResolver{AtlasMap: caveResolver, maps: map[string]*sceneformat.TilemapDoc{
			"maps/caves.ldtk": {TileWidth: 16, TileHeight: 16, Width: 1, Height: 1, Tileset: "cave_tiles", Data: []int{1}},
		}},
		fs: sceneformat.FSAssets{FS: fstest.MapFS{"maps/caves.ldtk": {Data: []byte(caveLDtk)}}},
	}
	doc, err := sceneformat.Parse([]byte("version: 2\nlayers:\n  - name: world\n    tilemap:\n      source: maps/caves.ldtk\n      level: Level_1\n"))
	if err != nil {
		t.Fatal(err)
	}
	built, err := sceneformat.Build(doc, res)
	if err != nil {
		t.Fatal(err)
	}
	l := built.Layer("world")
	if l.Map == nil || l.Tilemap != nil {
		t.Fatalf("world = %+v, want the LDtk level built from the file", l)
	}
	if l.Map.FlagsAt(1, 0) != tilemap.TileSolid || l.Map.FlagsAt(0, 0) != 0 {
		t.Fatalf("flags %v %v, want Level_1", l.Map.FlagsAt(0, 0), l.Map.FlagsAt(1, 0))
	}
}

// resolverOf is an AssetResolver that resolves atlases and tilemaps through externalResolver and reads files through FSAssets.
type resolverOf struct {
	ext externalResolver
	fs  sceneformat.FSAssets
}

func (r resolverOf) Atlas(name string) (*sprite.Atlas, error) { return r.ext.Atlas(name) }
func (r resolverOf) Tilemap(source string) (*sceneformat.TilemapDoc, error) {
	return r.ext.Tilemap(source)
}
func (r resolverOf) ReadFile(p string) ([]byte, error) { return r.fs.ReadFile(p) }

func TestFSAssets(t *testing.T) {
	var a sceneformat.FSAssets
	if _, err := a.ReadFile("x"); err == nil {
		t.Fatal("ReadFile with no file system succeeded")
	}
	if _, err := a.Scene("x"); err == nil {
		t.Fatal("Scene with no file system succeeded")
	}
	if _, err := a.Atlas("x"); err == nil {
		t.Fatal("Atlas with no atlases succeeded")
	}
	a = sceneformat.FSAssets{FS: layeredProject(), Atlases: caveResolver}
	if got, err := a.Atlas("cave_tiles"); err != nil || got != caveTilesAtlas {
		t.Fatalf("Atlas = %v, %v", got, err)
	}
	if doc, err := a.Scene("level.scene.yaml"); err != nil || doc.Name != "Layered" {
		t.Fatalf("Scene = %v, %v", doc, err)
	}
	if !reflect.DeepEqual(mustRead(t, a, "tilesets/cave.tileset.yaml"), []byte(caveTileset)) {
		t.Fatal("ReadFile returned other bytes")
	}
}

func TestTilemapOffsetsParseBuildValidateAndMerge(t *testing.T) {
	const v3Single = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v3.json
version: 3
name: Offset Map
layers:
  - name: floor
    tilemap:
      offsetX: -32
      offsetY: -16
      tileWidth: 16
      tileHeight: 16
      width: 3
      height: 2
      tileset: cave_tiles
      data: [0, 0, 0, 0, 0, 1]
`
	doc, err := sceneformat.Parse([]byte(v3Single))
	if err != nil {
		t.Fatalf("Parse v3 single with offset: %v", err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != v3Single {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, v3Single)
	}
	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sprites := built.Layer("floor").Tilemap.Sprites
	if len(sprites) != 1 || sprites[0].X != 0 || sprites[0].Y != 0 {
		t.Fatalf("offset sprite = %+v, want single tile at (0, 0) from col=2, row=1 with offset (-32, -16)", sprites)
	}

	// Layered tilemap sets Tilemap.Origin from offsetX/offsetY.
	v3Layered := strings.Replace(layeredScene, "version: 2", "version: 3", 1)
	v3Layered = strings.Replace(v3Layered, "      tileWidth: 16", "      offsetX: -16\n      offsetY: -32\n      tileWidth: 16", 1)
	fsys := layeredProject()
	fsys["level.scene.yaml"] = &fstest.MapFile{Data: []byte(v3Layered)}
	builtLayered, _ := buildLayered(t, fsys)
	if got := builtLayered.Layer("ground").Map.Origin; got != (core.Point{X: -16, Y: -32}) {
		t.Fatalf("layered Map.Origin = %+v, want (-16, -32)", got)
	}

	// Older versions and ref tilemaps refuse offsetX/offsetY.
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"v1_offset", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      offsetX: -16\n      tileWidth: 16\n      tileHeight: 16\n      width: 1\n      height: 1\n      tileset: cave_tiles\n      data: [0]\n", "need version 3"},
		{"v2_offset", "version: 2\nlayers:\n  - name: a\n    tilemap:\n      offsetY: -16\n      tileWidth: 16\n      tileHeight: 16\n      width: 1\n      height: 1\n      tileset: cave_tiles\n      data: [0]\n", "need version 3"},
		{"ref_offset", "version: 3\ntilemaps:\n  g:\n    tileWidth: 16\n    tileHeight: 16\n    width: 1\n    height: 1\n    tileset: cave_tiles\n    data: [0]\nlayers:\n  - name: a\n    tilemap:\n      ref: g\n      offsetX: -16\n", "cannot set offsetX or offsetY on a ref tilemap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := sceneformat.Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse = %v, want %q", err, tc.want)
			}
		})
	}

	// Three-way merge merges offsetX and offsetY independently.
	base := doc.Clone()
	ours := doc.Clone()
	theirs := doc.Clone()
	ours.Layers[0].Tilemap.OffsetX = -48
	theirs.Layers[0].Tilemap.OffsetY = -32
	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("unexpected merge conflicts: %+v", res.Conflicts)
	}
	if got := res.Doc.Layers[0].Tilemap; got.OffsetX != -48 || got.OffsetY != -32 {
		t.Fatalf("merged offset = (%v, %v), want (-48, -32)", got.OffsetX, got.OffsetY)
	}
}

func mustRead(t *testing.T, a sceneformat.FSAssets, p string) []byte {
	t.Helper()
	data, err := a.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInlineTilemapWithTilesetFileTakesFlagsAndAnimations(t *testing.T) {
	const inlineScene = `version: 2
name: Inline Tileset
virtualWidth: 64
virtualHeight: 32
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 4
      height: 2
      tilesetFile: tilesets/cave.tileset.yaml
      data: [0, 3, 0, 0, 1, 1, 2, 1]
`
	doc, err := sceneformat.Parse([]byte(inlineScene))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tm := doc.Layers[0].Tilemap
	if !tm.HasTilesetFile() || tm.IsLayered() {
		t.Fatalf("HasTilesetFile=%v IsLayered=%v, want true/false", tm.HasTilesetFile(), tm.IsLayered())
	}
	// Round-trips unchanged through Encode and Parse.
	enc, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := sceneformat.Parse(enc)
	if err != nil {
		t.Fatalf("Parse(enc): %v", err)
	}
	again, err := sceneformat.Encode(back)
	if err != nil || !bytes.Equal(again, enc) {
		t.Fatalf("round-trip mismatch (%v):\n%s", err, again)
	}

	built, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: layeredProject()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ground := built.Layer("ground")
	if ground.Map == nil || ground.Tilemap != nil {
		t.Fatalf("ground Map=%v Tilemap=%v, want Map only", ground.Map, ground.Tilemap)
	}
	if ground.Map.FlagsAt(0, 1) != tilemap.TileSolid || ground.Map.FlagsAt(2, 1) != tilemap.TileOneWay {
		t.Fatalf("flags = (%v, %v), want (TileSolid, TileOneWay)", ground.Map.FlagsAt(0, 1), ground.Map.FlagsAt(2, 1))
	}
	// Collide directly via physics.MoveAndSlide with no Go code naming any tile.
	body := &physics.Body{
		Shape: physics.BoxShape(physics.Box{X: 2, Y: 0, W: 12, H: 12}),
	}
	for range 20 {
		body.VY += 400 / 60.0
		physics.MoveAndSlide(body, ground.Map, time.Second/60)
	}
	if !body.Grounded || body.Bounds().Bottom() != 16 {
		t.Fatalf("MoveAndSlide: grounded=%v bottom=%v, want grounded at 16", body.Grounded, body.Bounds().Bottom())
	}
	// Animated tile 2 (GID 3 at col 1, row 0) advances on built.Update.
	anim := ground.Map.Tilesets[0].Animations[2]
	if anim == nil || anim.CurrentTileID() != 2 {
		t.Fatalf("initial animation = %+v, want tile 2", anim)
	}
	built.Update(100 * time.Millisecond)
	if anim.CurrentTileID() != 3 {
		t.Fatalf("after 100ms animation shows tile %d, want 3", anim.CurrentTileID())
	}
	// Combining tileset and tilesetFile is refused.
	if _, err := sceneformat.Parse([]byte(strings.Replace(inlineScene, "tilesetFile: tilesets/cave.tileset.yaml", "tileset: cave\n      tilesetFile: tilesets/cave.tileset.yaml", 1))); err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("combining tileset and tilesetFile error = %v", err)
	}
}

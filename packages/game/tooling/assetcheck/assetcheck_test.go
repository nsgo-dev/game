package assetcheck_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// pngBytes returns a blank PNG image of the given size.
func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const (
	roomScene = `version: 1
name: Room
tilemaps:
  grid:
    tileWidth: 16
    tileHeight: 16
    width: 2
    height: 1
    tileset: tiles
    data: [1, 2]
layers:
  - name: floor
    tilemap:
      ref: grid
  - name: level
    tilemap:
      source: maps/level.tmj
  - name: actors
    objects:
      - id: hero
        sprite:
          atlas: actors
          frame: hero
        material: materials/stone.material.yaml
`
	actorsAtlas = `{
  "frames": {
    "hero": {
      "frame": {"x": 0, "y": 0, "w": 16, "h": 16},
      "sourceSize": {"w": 16, "h": 16}
    }
  },
  "meta": {
    "image": "actors.png",
    "scale": "1"
  }
}
`
	tilesAtlas = `{
  "frames": [
    {"filename": "1", "frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
    {"filename": "2", "frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}
  ],
  "meta": {
    "image": "tiles.png"
  }
}
`
	levelMap = `{
  "width": 2,
  "height": 1,
  "tilewidth": 16,
  "tileheight": 16,
  "orientation": "orthogonal",
  "tilesets": [
    {
      "firstgid": 1,
      "source": "tiles.tsj"
    }
  ],
  "layers": [
    {"type": "tilelayer", "name": "ground", "width": 2, "height": 1, "data": [1, 2]},
    {
      "type": "group",
      "name": "bg",
      "layers": [
        {
          "type": "imagelayer",
          "name": "sky",
          "image": "../art/sky.png"
        }
      ]
    }
  ]
}
`
	tilesTileset = `{
  "name": "tiles",
  "tilewidth": 16,
  "tileheight": 16,
  "tilecount": 2,
  "columns": 2,
  "imagewidth": 32,
  "imageheight": 16,
  "image": "../atlases/tiles.png"
}
`
	worldLDtk = `{
  "defaultGridSize": 16,
  "defs": {
    "tilesets": [
      {
        "uid": 1,
        "identifier": "Tiles",
        "relPath": "atlases/tiles.png",
        "pxWid": 32,
        "pxHei": 16,
        "tileGridSize": 16
      }
    ],
    "layers": []
  },
  "levels": [
    {
      "identifier": "Level_0",
      "uid": 2,
      "pxWid": 32,
      "pxHei": 16,
      "layerInstances": []
    }
  ]
}
`
	stoneMaterial = `type: material
version: 1
name: Stone
texture: art/stone.png
opacity: 0.75
`
)

// project returns a small game project in which every reference resolves.
func project(t testing.TB) fstest.MapFS {
	return fstest.MapFS{
		"nsgo.yaml":                     {Data: []byte("name: Test\n")},
		"scenes/room.scene.yaml":        {Data: []byte(roomScene)},
		"atlases/actors.json":           {Data: []byte(actorsAtlas)},
		"atlases/actors.png":            {Data: pngBytes(t, 32, 16)},
		"atlases/tiles.json":            {Data: []byte(tilesAtlas)},
		"atlases/tiles.png":             {Data: pngBytes(t, 32, 16)},
		"maps/level.tmj":                {Data: []byte(levelMap)},
		"maps/tiles.tsj":                {Data: []byte(tilesTileset)},
		"art/sky.png":                   {Data: pngBytes(t, 64, 32)},
		"art/stone.png":                 {Data: pngBytes(t, 8, 8)},
		"world.ldtk":                    {Data: []byte(worldLDtk)},
		"materials/stone.material.yaml": {Data: []byte(stoneMaterial)},
		"config.json":                   {Data: []byte(`{"volume": 1}`)},
		"broken.json":                   {Data: []byte(`{"not": json`)},
	}
}

func mustCheck(t *testing.T, fsys fstest.MapFS) *assetcheck.Report {
	t.Helper()
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCleanProject(t *testing.T) {
	r := mustCheck(t, project(t))
	if !r.OK() {
		t.Fatalf("problems in a clean project:\n%s", problems(r))
	}
	if r.Scenes != 1 || r.Maps != 2 || r.Tilesets != 1 || r.Atlases != 2 {
		t.Fatalf("counted %d scenes, %d maps, %d tilesets, %d atlases; want 1, 2, 1, 2", r.Scenes, r.Maps, r.Tilesets, r.Atlases)
	}
	if got, want := r.Summary(), "1 scene, 2 maps, 1 tileset, 2 atlases: no problems"; got != want {
		t.Fatalf("Summary() = %q, want %q", got, want)
	}
}

func problems(r *assetcheck.Report) string {
	var b strings.Builder
	for _, p := range r.Problems {
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// lineOf returns the 1-based line of the first line of text containing marker.
func lineOf(t *testing.T, text, marker string) int {
	t.Helper()
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, marker) {
			return i + 1
		}
	}
	t.Fatalf("marker %q not in text", marker)
	return 0
}

func TestBrokenReferencesNameFileAndLine(t *testing.T) {
	cases := []struct {
		name string
		// file is changed by replacing old with new, or removed when new is "-" and old is empty.
		file, old, new string
		// wantFile is where the problem is reported, at the line holding marker in its final text (0 when marker is empty), with a message containing msg.
		wantFile, marker, msg string
	}{
		{"unknown atlas", "scenes/room.scene.yaml", "atlas: actors", "atlas: heroes", "scenes/room.scene.yaml", "atlas: heroes", `atlas "heroes" not found: no atlas JSON named heroes.json in the checked directory`},
		{"missing frame", "scenes/room.scene.yaml", "frame: hero", "frame: villain", "scenes/room.scene.yaml", "frame: villain", `frame "villain" is not in atlas "actors" (atlases/actors.json)`},
		{"child missing frame", "scenes/room.scene.yaml", "        material: materials/stone.material.yaml\n", "        material: materials/stone.material.yaml\n        children:\n          - id: hat\n            sprite:\n              atlas: actors\n              frame: hat\n", "scenes/room.scene.yaml", "frame: hat", `frame "hat" is not in atlas "actors" (atlases/actors.json)`},
		{"tile without frame", "scenes/room.scene.yaml", "data: [1, 2]", "data: [1, 7]", "scenes/room.scene.yaml", "data: [1, 7]", `tile 7 has no frame "7" in tileset atlas "tiles" (atlases/tiles.json)`},
		{"unknown tileset atlas", "scenes/room.scene.yaml", "tileset: tiles", "tileset: cave", "scenes/room.scene.yaml", "tileset: cave", `tileset atlas "cave" not found`},
		{"missing material", "materials/stone.material.yaml", "", "-", "scenes/room.scene.yaml", "material:", `material "materials/stone.material.yaml": not found`},
		{"invalid material", "materials/stone.material.yaml", "opacity: 0.75", "opacity: 3", "materials/stone.material.yaml", "", "opacity"},
		{"material unknown field", "materials/stone.material.yaml", "name: Stone", "name: Stone\nshine: 1", "materials/stone.material.yaml", "shine: 1", "field shine not found"},
		{"missing material texture", "art/stone.png", "", "-", "materials/stone.material.yaml", "texture:", `texture "art/stone.png": not found`},
		{"missing tilemap source", "maps/level.tmj", "", "-", "scenes/room.scene.yaml", "source: maps/level.tmj", `tilemap source "maps/level.tmj": not found`},
		{"tilemap source not tmj", "scenes/room.scene.yaml", "source: maps/level.tmj", "source: maps/level.tmx", "scenes/room.scene.yaml", "source: maps/level.tmx", "is not a Tiled JSON map (.tmj)"},
		{"tilemap source outside", "scenes/room.scene.yaml", "source: maps/level.tmj", "source: ../level.tmj", "scenes/room.scene.yaml", "source: ../level.tmj", "leads outside the checked directory"},
		{"scene validation", "scenes/room.scene.yaml", "  - name: actors", "  - name: actors\n    space: sky", "scenes/room.scene.yaml", "space: sky", `invalid space "sky"`},
		{"scene unknown field", "scenes/room.scene.yaml", "        sprite:", "        sprit: 1\n        sprite:", "scenes/room.scene.yaml", "sprit: 1", "field sprit not found"},
		{"scene syntax", "scenes/room.scene.yaml", "name: Room", "name: [Room", "scenes/room.scene.yaml", "", "did not find expected"},
		{"missing external tileset", "maps/tiles.tsj", "", "-", "maps/level.tmj", `"source": "tiles.tsj"`, `external tileset "tiles.tsj": not found`},
		{"tsx tileset", "maps/level.tmj", `"source": "tiles.tsj"`, `"source": "tiles.tsx"`, "maps/level.tmj", `"source": "tiles.tsx"`, "not a JSON tileset"},
		{"missing tileset image", "maps/tiles.tsj", `"image": "../atlases/tiles.png"`, `"image": "../atlases/tile.png"`, "maps/tiles.tsj", `"image"`, `tileset image "../atlases/tile.png": not found`},
		{"missing image layer", "art/sky.png", "", "-", "maps/level.tmj", `"image": "../art/sky.png"`, `image layer "../art/sky.png": not found`},
		{"map syntax", "maps/level.tmj", `"height": 1,`, `"height": 1,,`, "maps/level.tmj", `"height": 1,,`, "invalid JSON"},
		{"map does not load", "maps/level.tmj", `"orientation": "orthogonal"`, `"orientation": "round"`, "maps/level.tmj", "", "orientation"},
		{"ldtk tileset image", "world.ldtk", `"relPath": "atlases/tiles.png"`, `"relPath": "atlases/tile.png"`, "world.ldtk", `"relPath"`, `tileset image "atlases/tile.png": not found`},
		{"ldtk external level", "world.ldtk", `"identifier": "Level_0",`, `"identifier": "Level_0",` + "\n" + `"externalRelPath": "world/Level_0.ldtkl",`, "world.ldtk", `"externalRelPath"`, `level "Level_0" is saved in a separate file ("world/Level_0.ldtkl"), which tilemap.LoadLDtkAll does not read`},
		{"ldtk background", "world.ldtk", `"identifier": "Level_0",`, `"identifier": "Level_0",` + "\n" + `"bgRelPath": "C:/art/bg.png",`, "world.ldtk", `"bgRelPath"`, "is an absolute path"},
		{"atlas image", "atlases/actors.png", "", "-", "atlases/actors.json", `"image": "actors.png"`, `meta.image "actors.png": not found`},
		{"atlas image URI", "atlases/actors.json", `"image": "actors.png"`, `"image": "asset:actors.png"`, "atlases/actors.json", `"image": "asset:actors.png"`, "is a URI"},
		{"frame outside image", "atlases/actors.json", `"x": 0, "y": 0, "w": 16`, `"x": 24, "y": 0, "w": 16`, "atlases/actors.json", `"x": 24`, `frame "hero" at (24, 0) size 16x16 lies outside its 32x16 image`},
		{"array frame outside image", "atlases/tiles.json", `"x": 16, "y": 0`, `"x": 16, "y": 8`, "atlases/tiles.json", `"x": 16, "y": 8`, `frame "2" at (16, 8)`},
		{"image does not decode", "atlases/actors.png", "", "not a png", "atlases/actors.json", `"image": "actors.png"`, "does not decode"},
		{"atlas does not load", "atlases/actors.json", `"scale": "1"`, `"scale": "0"`, "atlases/actors.json", "", "meta.scale"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := project(t)
			switch {
			case tc.new == "-":
				delete(fsys, tc.file)
			case tc.old == "":
				fsys[tc.file] = &fstest.MapFile{Data: []byte(tc.new)}
			default:
				text := string(fsys[tc.file].Data)
				if !strings.Contains(text, tc.old) {
					t.Fatalf("%s does not contain %q", tc.file, tc.old)
				}
				fsys[tc.file] = &fstest.MapFile{Data: []byte(strings.Replace(text, tc.old, tc.new, 1))}
			}
			r := mustCheck(t, fsys)
			if len(r.Problems) != 1 {
				t.Fatalf("%d problems, want 1:\n%s", len(r.Problems), problems(r))
			}
			p := r.Problems[0]
			wantLine := 0
			if tc.marker != "" {
				wantLine = lineOf(t, string(fsys[tc.wantFile].Data), tc.marker)
			}
			if p.File != tc.wantFile || (tc.marker != "" && p.Line != wantLine) || !strings.Contains(p.Message, tc.msg) {
				t.Fatalf("problem %q, want %s:%d with %q", p, tc.wantFile, wantLine, tc.msg)
			}
			if tc.marker == "" && p.File == "scenes/room.scene.yaml" && p.Line == 0 {
				t.Fatalf("scene problem %q has no line", p)
			}
		})
	}
}

func TestProblemString(t *testing.T) {
	if got := (assetcheck.Problem{File: "a/b.tmj", Line: 3, Message: "bad"}).String(); got != "a/b.tmj:3: bad" {
		t.Fatalf("String() = %q", got)
	}
	if got := (assetcheck.Problem{File: "a/b.tmj", Message: "bad"}).String(); got != "a/b.tmj: bad" {
		t.Fatalf("String() without a line = %q", got)
	}
}

func TestProblemsAreSortedAndCounted(t *testing.T) {
	fsys := project(t)
	delete(fsys, "art/sky.png")
	delete(fsys, "atlases/actors.png")
	fsys["scenes/room.scene.yaml"].Data = []byte(strings.Replace(roomScene, "frame: hero", "frame: villain", 1))
	r := mustCheck(t, fsys)
	var files []string
	for _, p := range r.Problems {
		files = append(files, p.File)
	}
	// The atlas whose image is missing still loads, so the scene's frame is still checked.
	want := []string{"atlases/actors.json", "maps/level.tmj", "scenes/room.scene.yaml"}
	if !slices.Equal(files, want) {
		t.Fatalf("problems in %q, want %q:\n%s", files, want, problems(r))
	}
	if got := r.Summary(); got != "1 scene, 2 maps, 1 tileset, 2 atlases: 3 problems" {
		t.Fatalf("Summary() = %q", got)
	}
}

func TestBrokenAtlasIsReportedOnce(t *testing.T) {
	fsys := project(t)
	fsys["atlases/actors.json"].Data = []byte(strings.Replace(actorsAtlas, `"scale": "1"`, `"scale": "-1"`, 1))
	fsys["scenes/room.scene.yaml"].Data = []byte(strings.Replace(roomScene, "frame: hero", "frame: villain", 1))
	r := mustCheck(t, fsys)
	if len(r.Problems) != 1 || r.Problems[0].File != "atlases/actors.json" {
		t.Fatalf("want only the atlas's own problem:\n%s", problems(r))
	}
}

func TestBrokenTilesetSkipsMapLoader(t *testing.T) {
	fsys := project(t)
	fsys["maps/tiles.tsj"].Data = []byte(strings.Replace(tilesTileset, `"name": "tiles",`, `"name": "tiles",,`, 1))
	r := mustCheck(t, fsys)
	if len(r.Problems) != 1 || r.Problems[0].File != "maps/tiles.tsj" || r.Problems[0].Line != 2 {
		t.Fatalf("want only the tileset's syntax error on line 2:\n%s", problems(r))
	}
}

func TestNearestAtlasWins(t *testing.T) {
	fsys := project(t)
	// A second project in a subdirectory has its own actors atlas, without the hero frame.
	fsys["other/nsgo.yaml"] = &fstest.MapFile{Data: []byte("name: Other\n")}
	fsys["other/actors.json"] = &fstest.MapFile{Data: []byte(strings.Replace(actorsAtlas, `"hero"`, `"villain"`, 1))}
	fsys["other/actors.png"] = &fstest.MapFile{Data: pngBytes(t, 32, 16)}
	fsys["other/room.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: v\n        sprite:\n          atlas: actors\n          frame: villain\n")}
	if r := mustCheck(t, fsys); !r.OK() {
		t.Fatalf("each scene should find its own project's atlas:\n%s", problems(r))
	}

	// Within one project, two atlases named alike at the same distance are ambiguous until named by path.
	fsys = project(t)
	fsys["sprites/actors.json"] = &fstest.MapFile{Data: []byte(actorsAtlas)}
	fsys["sprites/actors.png"] = &fstest.MapFile{Data: pngBytes(t, 32, 16)}
	r := mustCheck(t, fsys)
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0].Message, "is ambiguous: atlases/actors.json, sprites/actors.json") {
		t.Fatalf("want an ambiguous atlas:\n%s", problems(r))
	}
	fsys["scenes/room.scene.yaml"].Data = []byte(strings.Replace(roomScene, "atlas: actors", "atlas: sprites/actors", 1))
	if r := mustCheck(t, fsys); !r.OK() {
		t.Fatalf("an atlas named by its path should resolve:\n%s", problems(r))
	}

	// An atlas beside the scene beats one elsewhere in the project.
	fsys = project(t)
	fsys["scenes/actors.json"] = &fstest.MapFile{Data: []byte(strings.Replace(actorsAtlas, `"hero"`, `"villain"`, 1))}
	fsys["scenes/actors.png"] = &fstest.MapFile{Data: pngBytes(t, 32, 16)}
	r = mustCheck(t, fsys)
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0].Message, `(scenes/actors.json)`) {
		t.Fatalf("want the scene's sibling atlas to be used:\n%s", problems(r))
	}
}

func TestNearestAtlasCountsDirectories(t *testing.T) {
	villain := strings.Replace(actorsAtlas, `"hero"`, `"villain"`, 1)
	scene := "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: v\n        sprite:\n          atlas: actors\n          frame: villain\n"
	cases := []struct {
		name string
		// scene is where the scene is; near holds the atlas it must find, which has the villain frame, and far one at a greater distance, which does not.
		scene, near, far string
	}{
		{"sibling beats subdirectory", "lv/lv.scene.yaml", "lv/actors.json", "lv/sub/actors.json"},
		{"root sibling beats subdirectory", "root.scene.yaml", "actors.json", "lv/actors.json"},
		{"parent beats cousin", "lv/a/a.scene.yaml", "lv/actors.json", "other/b/actors.json"},
		{"subdirectory beats cousin", "lv/lv.scene.yaml", "lv/sub/actors.json", "other/actors.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"nsgo.yaml": {Data: []byte("name: Test\n")},
				tc.scene:    {Data: []byte(scene)},
				tc.near:     {Data: []byte(villain)},
				tc.far:      {Data: []byte(actorsAtlas)},
				path.Join(path.Dir(tc.near), "actors.png"): {Data: pngBytes(t, 32, 16)},
				path.Join(path.Dir(tc.far), "actors.png"):  {Data: pngBytes(t, 32, 16)},
			}
			if r := mustCheck(t, fsys); !r.OK() || r.Atlases != 2 {
				t.Fatalf("want %s used for %s (%d atlases):\n%s", tc.near, tc.scene, r.Atlases, problems(r))
			}
		})
	}
}

func TestUnusableAtlasIsReported(t *testing.T) {
	sceneLine := lineOf(t, roomScene, "atlas: actors")
	cases := []struct {
		name, old, new string
		// line and msg are the problem expected in atlases/actors.json besides the scene's.
		line int
		msg  string
	}{
		{"syntax error", `"frames": {`, `"frames": {,`, 2, "invalid JSON"},
		{"not an atlas", `"meta": {`, `"info": {`, 0, "not an atlas: want top-level frames and meta objects"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := project(t)
			fsys["atlases/actors.json"].Data = []byte(strings.Replace(actorsAtlas, tc.old, tc.new, 1))
			r := mustCheck(t, fsys)
			if len(r.Problems) != 2 {
				t.Fatalf("%d problems, want 2:\n%s", len(r.Problems), problems(r))
			}
			a, s := r.Problems[0], r.Problems[1]
			if a.File != "atlases/actors.json" || a.Line != tc.line || !strings.Contains(a.Message, tc.msg) {
				t.Fatalf("atlas problem %q, want atlases/actors.json:%d with %q", a, tc.line, tc.msg)
			}
			if s.File != "scenes/room.scene.yaml" || s.Line != sceneLine || s.Message != `atlas "actors": atlases/actors.json is not a usable atlas` {
				t.Fatalf("scene problem %q", s)
			}
		})
	}

	// A JSON file no scene names as an atlas is none of the check's business.
	fsys := project(t)
	fsys["data/broken.json"] = &fstest.MapFile{Data: []byte("{,")}
	fsys["data/config.json"] = &fstest.MapFile{Data: []byte(`{"debug": true}`)}
	if r := mustCheck(t, fsys); !r.OK() {
		t.Fatalf("reported JSON files no scene uses:\n%s", problems(r))
	}
}

func TestLDtkExternalLevelIsReported(t *testing.T) {
	fsys := project(t)
	fsys["world.ldtk"].Data = []byte(strings.Replace(worldLDtk, `"identifier": "Level_0",`, `"identifier": "Level_0",`+"\n"+`"externalRelPath": "world/Level_0.ldtkl",`, 1))
	fsys["world/Level_0.ldtkl"] = &fstest.MapFile{Data: []byte(`{"identifier": "Level_0", "layerInstances": []}`)}
	r := mustCheck(t, fsys)
	want := lineOf(t, string(fsys["world.ldtk"].Data), "externalRelPath")
	if len(r.Problems) != 1 || r.Problems[0].File != "world.ldtk" || r.Problems[0].Line != want || !strings.Contains(r.Problems[0].Message, "does not read") {
		t.Fatalf("want an external level reported at world.ldtk:%d:\n%s", want, problems(r))
	}
}

func TestSkipsDirectoriesTheGoCommandSkips(t *testing.T) {
	fsys := project(t)
	for _, dir := range []string{"testdata", ".cache", "_old", "maps/testdata"} {
		fsys[dir+"/bad.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 9\n")}
		fsys[dir+"/bad.tmj"] = &fstest.MapFile{Data: []byte("{")}
	}
	if r := mustCheck(t, fsys); !r.OK() || r.Scenes != 1 {
		t.Fatalf("checked skipped directories (%d scenes):\n%s", r.Scenes, problems(r))
	}
}

func TestCheckFailsWithoutRoot(t *testing.T) {
	if _, err := assetcheck.Check(os.DirFS("does-not-exist")); err == nil {
		t.Fatal("Check of a missing directory succeeded")
	}
}

// TestExamples checks the repository's sample games, as the Examples workflow does with game check.
func TestExamples(t *testing.T) {
	r, err := assetcheck.Check(os.DirFS("../../../../examples"))
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK() {
		t.Fatalf("problems in the examples:\n%s", problems(r))
	}
	if r.Maps == 0 || r.Atlases == 0 {
		t.Fatalf("found no maps or atlases in the examples: %s", r.Summary())
	}
}

func TestCheckSceneConnections(t *testing.T) {
	sub := `version: 2
layers:
  - name: actors
    objects:
      - id: button
      - id: door
connections:
  - from: button
    event: pressed
    to: door
    handler: open
`
	good := `version: 2
layers:
  - name: actors
    objects:
      - id: west
        scene: scenes/pair.scene.yaml
      - id: lever
connections:
  - from: lever
    event: pressed
    to: west/door
    handler: open
`
	fsys := fstest.MapFS{
		"nsgo.yaml":               &fstest.MapFile{Data: []byte("name: test\n")},
		"scenes/pair.scene.yaml":  &fstest.MapFile{Data: []byte(sub)},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(good)},
	}
	r, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK() {
		t.Fatalf("valid connected scenes reported problems:\n%s", problems(r))
	}

	// Self-contained scene with unknown target object reports the exact line of "to".
	badLocal := `version: 2
layers:
  - name: actors
    objects:
      - id: button
connections:
  - from: button
    event: pressed
    to: missing_door
    handler: open
`
	fsys["scenes/bad_local.scene.yaml"] = &fstest.MapFile{Data: []byte(badLocal)}
	// Instancing scene with unknown instanced target reports the exact line of "to".
	badInst := strings.Replace(good, "to: west/door", "to: west/ghost", 1)
	fsys["scenes/bad_inst.scene.yaml"] = &fstest.MapFile{Data: []byte(badInst)}

	r, err = assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	got := problems(r)
	for _, want := range []string{
		`scenes/bad_local.scene.yaml:9: sceneformat: connection 0 references unknown target object "missing_door"`,
		`scenes/bad_inst.scene.yaml:11: sceneformat: connection 0 references unknown target object "west/ghost"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems do not include %q:\n%s", want, got)
		}
	}
}

func FuzzCheckScene(f *testing.F) {
	f.Add([]byte(roomScene))
	f.Add([]byte("version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 1\n      tileset: tiles\n      data: [1, 9]\n"))
	f.Add([]byte("version: 1\nname: [x\n"))
	base := project(f)
	f.Fuzz(func(t *testing.T, scene []byte) {
		fsys := fstest.MapFS{}
		for k, v := range base {
			fsys[k] = v
		}
		fsys["scenes/room.scene.yaml"] = &fstest.MapFile{Data: scene}
		r, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatal(err)
		}
		lines := yamlLines(scene)
		for _, p := range r.Problems {
			if p.Line < 0 || (p.File == "scenes/room.scene.yaml" && p.Line > lines) {
				t.Fatalf("problem %q has line %d outside the %d-line file %q", p, p.Line, lines, scene)
			}
		}
	})
}

func FuzzCheckTiledMap(f *testing.F) {
	f.Add([]byte(levelMap))
	f.Add([]byte(`{"tilesets": [{"source": "../x.tsj"}], "layers": [{"type": "group", "layers": [{"type": "imagelayer", "image": "C:/x.png"}]}]}`))
	f.Add([]byte("[[[[1]]]]"))
	base := project(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fsys := fstest.MapFS{}
		for k, v := range base {
			fsys[k] = v
		}
		fsys["maps/level.tmj"] = &fstest.MapFile{Data: data}
		r, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Count(data, []byte("\n")) + 1
		for _, p := range r.Problems {
			if p.Line < 0 || (p.File == "maps/level.tmj" && p.Line > lines) {
				t.Fatalf("problem %q has line %d outside the %d-line file %q", p, p.Line, lines, data)
			}
		}
	})
}

func BenchmarkCheck(b *testing.B) {
	fsys := project(b)
	for b.Loop() {
		if _, err := assetcheck.Check(fsys); err != nil {
			b.Fatal(err)
		}
	}
}

// yamlLines returns how many lines YAML counts in data: it breaks lines at CR, LF, CRLF, NEL and the Unicode line and paragraph separators, not only at LF.
func yamlLines(data []byte) int {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	return 1 + strings.Count(s, "\n") + strings.Count(s, "\r") + strings.Count(s, "\u0085") + strings.Count(s, "\u2028") + strings.Count(s, "\u2029")
}

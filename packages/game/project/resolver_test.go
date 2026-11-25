package project_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// atlasJSON returns a TexturePacker atlas of one 16x16 frame called frame, packed into image.
func atlasJSON(frame, image string) string {
	return `{"frames": {"` + frame + `": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}}}, "meta": {"image": "` + image + `", "size": {"w": 16, "h": 16}}}`
}

// file returns a MapFS file holding s.
func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// Resolver serves every interface the editor and sceneformat.Build look for.
var (
	_ sceneformat.AssetResolver           = (*project.Resolver)(nil)
	_ sceneformat.SceneResolver           = (*project.Resolver)(nil)
	_ sceneformat.MaterialResolver        = (*project.Resolver)(nil)
	_ sceneformat.PhysicsMaterialResolver = (*project.Resolver)(nil)
	_ sceneformat.FileResolver            = (*project.Resolver)(nil)
	_ editor.AtlasLister                  = (*project.Resolver)(nil)
)

func TestResolverFindsAtlasesNearestTheScene(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml":                   file("version: 1\n"),
		"sprites/actors.json":         file(atlasJSON("hero", "actors.png")),
		"sprites/actors.png":          file("root actors"),
		"levels/cave/actors.json":     file(atlasJSON("bat", "../../art/bat.png")),
		"art/bat.png":                 file("bat image"),
		"ui/icons.json":               file(atlasJSON("coin", "icons.png")),
		"ui/icons.png":                file("icons image"),
		"data/table.json":             file(`{"rows": []}`),
		"broken.json":                 file(`{"frames": `),
		"_drafts/actors.json":         file(atlasJSON("draft", "x.png")),
		".cache/actors.json":          file(atlasJSON("cached", "x.png")),
		"testdata/actors.json":        file(atlasJSON("fixture", "x.png")),
		"vendor/other/game.yaml":      file("version: 1\n"),
		"vendor/other/actors.json":    file(atlasJSON("other", "x.png")),
		"levels/cave/cave.scene.yaml": file("version: 2\nlayers: []\n"),
	}
	r := project.NewResolver(fsys)

	// From the root, actors is the root's own atlas: sprites/ is nearer than levels/cave/.
	hero, err := r.Atlas("actors")
	if err != nil {
		t.Fatalf("Atlas(actors): %v", err)
	}
	if _, ok := hero.Frames["hero"]; !ok || string(hero.Image.Data) != "root actors" {
		t.Fatalf("Atlas(actors) = frames %v image %q, want sprites/actors.json with its image", hero.Frames, hero.Image.Data)
	}
	// A scene in levels/cave finds the atlas beside it, whose image lies elsewhere in the project.
	cave := r.ForScene("levels/cave/cave.scene.yaml")
	bat, err := cave.Atlas("actors")
	if err != nil {
		t.Fatalf("ForScene(cave).Atlas(actors): %v", err)
	}
	if _, ok := bat.Frames["bat"]; !ok || string(bat.Image.Data) != "bat image" {
		t.Fatalf("cave's actors = frames %v image %q, want levels/cave/actors.json", bat.Frames, bat.Image.Data)
	}
	// A path from the root names an atlas from anywhere.
	if a, err := cave.Atlas("sprites/actors"); err != nil || a != hero {
		t.Fatalf("Atlas(sprites/actors) = %v, %v; want the cached root atlas", a, err)
	}

	if got, want := r.AtlasNames(), []string{"actors", "icons", "levels/cave/actors"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AtlasNames from the root = %q, want %q", got, want)
	}
	if got, want := cave.AtlasNames(), []string{"actors", "icons", "sprites/actors"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AtlasNames from the cave = %q, want %q", got, want)
	}

	for name, want := range map[string]string{
		"missing": `atlas "missing" not found`,
		"table":   `atlas "table" not found`,
		"draft":   `atlas "draft" not found`,
		"other":   `not found`,
	} {
		if _, err := r.Atlas(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Atlas(%s) = %v, want an error containing %q", name, err, want)
		}
	}
}

func TestResolverRefusesAmbiguousAndBrokenAtlases(t *testing.T) {
	fsys := fstest.MapFS{
		"a/tiles.json":        file(atlasJSON("1", "tiles.png")),
		"a/tiles.png":         file("a"),
		"b/tiles.json":        file(atlasJSON("1", "tiles.png")),
		"b/tiles.png":         file("b"),
		"noimage.json":        file(atlasJSON("1", "missing.png")),
		"escape.json":         file(atlasJSON("1", "../outside.png")),
		"badframes.json":      file(`{"frames": {"x": "not a frame"}, "meta": {}}`),
		"scenes/a.scene.yaml": file("version: 2\nlayers: []\n"),
	}
	r := project.NewResolver(fsys)
	if _, err := r.Atlas("tiles"); err == nil || !strings.Contains(err.Error(), "ambiguous: a/tiles.json, b/tiles.json") {
		t.Fatalf("Atlas(tiles) = %v, want it ambiguous", err)
	}
	if a, err := r.Atlas("b/tiles"); err != nil || string(a.Image.Data) != "b" {
		t.Fatalf("Atlas(b/tiles) = %v, %v", a, err)
	}
	if got, want := r.AtlasNames(), []string{"a/tiles", "b/tiles", "badframes", "escape", "noimage"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AtlasNames = %q, want %q", got, want)
	}
	for name, want := range map[string]string{
		"noimage":   "noimage.json: meta.image",
		"escape":    "leaves the project",
		"badframes": "badframes.json",
	} {
		if _, err := r.Atlas(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Atlas(%s) = %v, want an error containing %q", name, err, want)
		}
	}
}

func TestResolverRefreshReadsTheFilesAgain(t *testing.T) {
	fsys := fstest.MapFS{"hero.json": file(atlasJSON("hero", "hero.png")), "hero.png": file("v1")}
	r := project.NewResolver(fsys)
	scene := r.ForScene("scenes/a.scene.yaml")
	a, err := r.Atlas("hero")
	if err != nil {
		t.Fatal(err)
	}
	fsys["hero.png"] = file("v2")
	fsys["coin.json"] = file(atlasJSON("coin", "hero.png"))
	if b, _ := scene.Atlas("hero"); b != a || len(scene.AtlasNames()) != 1 {
		t.Fatalf("before Refresh the scene's resolver read the files again")
	}
	scene.Refresh()
	b, err := r.Atlas("hero")
	if err != nil || b == a || string(b.Image.Data) != "v2" {
		t.Fatalf("after Refresh Atlas(hero) = %v, %v; want it loaded again", b, err)
	}
	if got := r.AtlasNames(); !reflect.DeepEqual(got, []string{"coin", "hero"}) {
		t.Fatalf("after Refresh AtlasNames = %q", got)
	}
}

func TestResolverLoadsScenesAndMaterials(t *testing.T) {
	fsys := fstest.MapFS{
		"scenes/crate.scene.yaml":       file("version: 2\nname: Crate\nlayers:\n  - name: main\n    objects:\n      - id: box\n"),
		"materials/stone.material.yaml": file("type: material\nversion: 1\nname: Stone\nbaseColor: '#8a8f99'\n"),
	}
	r := project.NewResolver(fsys)
	doc, err := r.Scene("scenes/crate.scene.yaml")
	if err != nil || doc.Name != "Crate" {
		t.Fatalf("Scene = %+v, %v", doc, err)
	}
	// Scene files are read on every request.
	fsys["scenes/crate.scene.yaml"] = file("version: 2\nname: Box\nlayers: []\n")
	if doc, err := r.Scene("scenes/crate.scene.yaml"); err != nil || doc.Name != "Box" {
		t.Fatalf("Scene after a save = %+v, %v", doc, err)
	}
	if _, err := r.Scene("scenes/missing.scene.yaml"); err == nil {
		t.Fatal("Scene read a missing file")
	}
	m, err := r.Material("materials/stone.material.yaml")
	if err != nil || m.Name != "Stone" {
		t.Fatalf("Material = %+v, %v", m, err)
	}
	if again, _ := r.ForScene("scenes/crate.scene.yaml").Material("materials/stone.material.yaml"); again != m {
		t.Fatal("Material returned another value for the same file")
	}
}

const caveMap = `{"width": 3, "height": 2, "tilewidth": 16, "tileheight": 16, "orientation": "orthogonal", "infinite": false,
 "tilesets": [{"firstgid": 1, "name": "rock", "image": "../art/rock.png", "imagewidth": 48, "imageheight": 16, "tilewidth": 16, "tileheight": 16, "columns": 3, "tilecount": 3, "margin": 0, "spacing": 0}, {"firstgid": 4, "source": "props.tsj"}],
 "layers": [{"type": "tilelayer", "name": "ground", "width": 3, "height": 2, "data": [1, 2, 0, 3, 3, 1], "visible": true, "opacity": 1}]}`

const propsTileset = `{"name": "props", "image": "props.png", "imagewidth": 16, "imageheight": 16, "tilewidth": 16, "tileheight": 16, "columns": 1, "tilecount": 1}`

func TestResolverReadsTheProjectsTilesetsAndMaps(t *testing.T) {
	fsys := fstest.MapFS{
		"maps/cave.tmj":              file(strings.Replace(caveMap, "[1, 2, 0, 3, 3, 1]", "[1, 4, 0, 3, 3, 1]", 1)),
		"maps/props.tsj":             file(propsTileset),
		"tilesets/cave.tileset.yaml": file("version: 1\ntileWidth: 16\ntileHeight: 16\nimage: tiles/cave.png\ncolumns: 2\ntileCount: 2\n"),
	}
	r := project.NewResolver(fsys)
	if data, err := r.ReadFile("maps/props.tsj"); err != nil || string(data) != propsTileset {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	if _, err := r.ReadFile("maps/missing.tmj"); err == nil {
		t.Fatal("ReadFile of a missing file succeeded")
	}

	// A Tiled map drawing from two tilesets, one of them external, and a layered grid with a YAML tileset build from the project's files.
	doc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Layers: []sceneformat.LayerDoc{
		{Name: "cave", Tilemap: &sceneformat.TilemapDoc{Source: "maps/cave.tmj"}},
		{Name: "ground", Tilemap: &sceneformat.TilemapDoc{TileWidth: 16, TileHeight: 16, Width: 2, Height: 1, TilesetFile: "tilesets/cave.tileset.yaml", TileLayers: []sceneformat.TileLayerDoc{{Name: "floor", Rows: []string{"0 1"}}}}},
	}}
	built, err := sceneformat.Build(doc, r)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if m := built.Layer("cave").Map; m == nil {
		t.Fatal("the Tiled map did not build")
	}
	if m := built.Layer("ground").Map; m == nil || m.TileAt(0, 1, 0) != 2 {
		t.Fatalf("the layered grid = %+v", m)
	}
}

func TestNearestAtlases(t *testing.T) {
	atlases := []string{"sprites/hero.json", "levels/one/hero.json", "levels/two/hero.json", "ui/hero.json", "hero/sprites.json"}
	for _, tc := range []struct {
		from, name string
		want       []string
	}{
		{".", "hero", []string{"sprites/hero.json", "ui/hero.json"}},
		{"levels/one", "hero", []string{"levels/one/hero.json"}},
		{"levels", "hero", []string{"levels/one/hero.json", "levels/two/hero.json"}},
		{"levels/one", "ui/hero", []string{"ui/hero.json"}},
		{".", "hero/sprites", []string{"hero/sprites.json"}},
		{".", "sprites", []string{"hero/sprites.json"}},
		{".", "./ui/hero", []string{"ui/hero.json"}},
		{".", "villain", nil},
		{".", "", nil},
	} {
		if got := project.NearestAtlases(atlases, tc.from, tc.name); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("NearestAtlases(%s, %q) = %q, want %q", tc.from, tc.name, got, tc.want)
		}
	}
}

func TestResolverServesThePhysicsMaterialsBuildPhysicsLoads(t *testing.T) {
	r := project.NewResolver(fstest.MapFS{
		"materials/bouncy.physics.yaml": file("type: physicsMaterial\nversion: 1\nfriction: 0.2\nrestitution: 0.85\n"),
	})
	doc := &sceneformat.SceneDoc{Version: 2, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{{
		ID: "ball",
		Components: map[string]any{
			"body3d":     map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0, "material": "materials/bouncy.physics.yaml"},
			"collider3d": map[string]any{"version": 1, "shape": "sphere", "radius": 0.5},
		},
	}}}}}
	built, err := sceneformat.Build(doc, r)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, &sceneformat.PhysicsSettings{}, r)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}
	if b := ps.Bodies3D["ball"]; b == nil || b.Restitution != 0.85 || b.Friction != 0.2 {
		t.Fatalf("the ball's body = %+v, want the project's bouncy material", b)
	}
	if _, err := r.PhysicsMaterial("materials/missing.physics.yaml"); err == nil {
		t.Fatal("PhysicsMaterial of a missing file succeeded")
	}
}

func TestTheAtlasRulesGameCheckShares(t *testing.T) {
	for _, tc := range []struct {
		p, name string
		want    bool
	}{
		{"sprites/actors.json", "actors", true},
		{"sprites/actors.json", "sprites/actors", true},
		{"sprites/actors.json", "/sprites/actors", true},
		{"sprites/actors.json", "a/../sprites/actors", true},
		{"sprites/actors.json", "other/actors", false},
		{"sprites/actors.json", "", false},
	} {
		if got := project.AtlasNamed(tc.p, tc.name); got != tc.want {
			t.Errorf("AtlasNamed(%q, %q) = %t, want %t", tc.p, tc.name, got, tc.want)
		}
	}
	for name, want := range map[string]bool{".git": true, "_drafts": true, "testdata": true, "sprites": false} {
		if got := project.SkipDir(name); got != want {
			t.Errorf("SkipDir(%q) = %t, want %t", name, got, want)
		}
	}
	for data, want := range map[string]bool{
		atlasJSON("hero", "a.png"):     true,
		`{"frames": [], "meta": {}}`:   true,
		`{"frames": {}, "meta": []}`:   false,
		`{"rows": []}`:                 false,
		`{"frames": `:                  false,
		`[{"frames": {}, "meta": {}}]`: false,
	} {
		if got := project.IsAtlasJSON([]byte(data)); got != want {
			t.Errorf("IsAtlasJSON(%s) = %t, want %t", data, got, want)
		}
	}
}

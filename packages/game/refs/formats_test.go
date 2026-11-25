package refs_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// refList formats p's references as "file:line field value -> target".
func refList(p *refs.Project) []string {
	var out []string
	for _, r := range p.Refs {
		out = append(out, fmt.Sprintf("%s:%d %s %q -> %s", r.File, r.Line, r.Field, r.Value, r.Target))
	}
	return out
}

// checkList formats p.Check's problems.
func checkList(p *refs.Project) []string {
	var out []string
	for _, prob := range p.Check() {
		out = append(out, prob.String())
	}
	return out
}

func TestJSONTilesetsAndAtlases(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml":       {Data: []byte(gameHead)},
		"maps/level.tmj":  {Data: []byte(`{"tilesets": [{"firstgid": 1, "source": "tiles.json"}]}` + "\n")},
		"maps/tiles.json": {Data: []byte(`{"image": "../art/tiles.png", "tiles": [{"id": 0, "image": "../art/one.png"}]}` + "\n")},
		"art/tiles.png":   {Data: []byte("tiles")},
		"art/one.png":     {Data: []byte("one")},
		"atlases/ui.json": {Data: []byte(`{"frames": {"a": {}}, "meta": {"image": "ui.png"}}` + "\n")},
		"atlases/ui.png":  {Data: []byte("ui")},
		// Neither a tileset a map names nor an atlas: not read.
		"config.json": {Data: []byte(`{"meta": {"image": "x.png"}}`)},
	}
	p := scan(t, fsys)
	want := []string{
		`atlases/ui.json:1 atlas image "ui.png" -> atlases/ui.png`,
		`maps/level.tmj:1 external tileset "tiles.json" -> maps/tiles.json`,
		`maps/tiles.json:1 tileset image "../art/tiles.png" -> art/tiles.png`,
		`maps/tiles.json:1 tile image "../art/one.png" -> art/one.png`,
	}
	if got := refList(p); !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := checkList(p); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}

	dir := dirFS(t, fsys)
	// Moving the tileset rewrites the map's reference to it and its own references relative to itself.
	move(t, dir, "maps/tiles.json", "maps/sub/tiles.json")
	if got := read(t, dir, "maps/level.tmj"); !strings.Contains(got, `"source": "sub/tiles.json"`) {
		t.Fatalf("map:\n%s", got)
	}
	if got, want := read(t, dir, "maps/sub/tiles.json"), `{"image": "../../art/tiles.png", "tiles": [{"id": 0, "image": "../../art/one.png"}]}`+"\n"; got != want {
		t.Fatalf("tileset:\n%s\nwant:\n%s", got, want)
	}
	// Moving an image the tileset names rewrites the tileset.
	move(t, dir, "art/tiles.png", "gfx/tiles.png")
	if got := read(t, dir, "maps/sub/tiles.json"); !strings.Contains(got, `"image": "../../gfx/tiles.png"`) {
		t.Fatalf("tileset:\n%s", got)
	}
	// Moving an atlas page rewrites the atlas's meta.image.
	move(t, dir, "atlases/ui.png", "atlases/pages/ui.png")
	if got, want := read(t, dir, "atlases/ui.json"), `{"frames": {"a": {}}, "meta": {"image": "pages/ui.png"}}`+"\n"; got != want {
		t.Fatalf("atlas:\n%s\nwant:\n%s", got, want)
	}
	clean(t, dir)
}

func TestGameYAMLAssets(t *testing.T) {
	const assets = `assets:
  atlases:
    - name: ui
      in: art/ui
      out: gen/atlases
  textures:
    - in: art/albedo.png
      out: gen/albedo.ktx2
  manifest:
    in: gen
    out: gen/manifest_gen.go
`
	fsys := fstest.MapFS{
		"game.yaml":       {Data: []byte(gameHead + assets)},
		"art/ui/a.png":    {Data: []byte("a")},
		"art/albedo.png":  {Data: []byte("albedo")},
		"materials/m.yml": {Data: []byte("type: material\nversion: 1\nuid: mat0000000001\n")},
	}
	p := scan(t, fsys)
	want := []string{
		`game.yaml:9 assets atlas in "art/ui" -> art/ui`,
		`game.yaml:10 assets atlas out "gen/atlases" -> gen/atlases`,
		`game.yaml:12 assets texture in "art/albedo.png" -> art/albedo.png`,
		`game.yaml:13 assets texture out "gen/albedo.ktx2" -> gen/albedo.ktx2`,
		`game.yaml:15 assets manifest in "gen" -> gen`,
		`game.yaml:16 assets manifest out "gen/manifest_gen.go" -> gen/manifest_gen.go`,
	}
	if got := refList(p); !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// What game assets writes need not exist yet; its inputs must.
	if got := checkList(p); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
	missing := fstest.MapFS{"game.yaml": fsys["game.yaml"], "art/albedo.png": fsys["art/albedo.png"], "art/ui.png": {Data: []byte("not a folder")}}
	if got, want := checkList(scan(t, missing)), []string{`game.yaml:9: assets atlas in "art/ui": not found`}; !slices.Equal(got, want) {
		t.Fatalf("problems: %v, want %v", got, want)
	}

	dir := dirFS(t, fsys)
	pl := move(t, dir, "art", "gfx")
	if got, want := read(t, dir, "game.yaml"), gameHead+strings.NewReplacer("in: art/ui", "in: gfx/ui", "in: art/albedo.png", "in: gfx/albedo.png").Replace(assets); got != want {
		t.Fatalf("game.yaml:\n%s\nwant:\n%s", got, want)
	}
	// The index lists the texture, a file the project reads, and neither folders nor what game assets writes.
	for _, w := range pl.Writes {
		if w.Path == refs.IndexFileName && (!strings.Contains(string(w.Data), "path: gfx/albedo.png\n") || strings.Contains(string(w.Data), "gen") || strings.Contains(string(w.Data), "gfx/ui\n")) {
			t.Fatalf("index:\n%s", w.Data)
		}
	}
	move(t, dir, "gfx/ui", "gfx/hud")
	if got := read(t, dir, "game.yaml"); !strings.Contains(got, "in: gfx/hud\n") {
		t.Fatalf("game.yaml:\n%s", got)
	}
	clean(t, dir)
}

func TestGameYAMLThatDoesNotParse(t *testing.T) {
	fsys := project()
	fsys["game.yaml"] = &fstest.MapFile{Data: []byte("version: 1\nautoload:\n  scenes:\n    - name: room\n      path: scenes/room.scene.yaml\n")}
	p := scan(t, fsys)
	if got := checkList(p); len(got) != 1 || !strings.HasPrefix(got[0], "game.yaml: project: ") {
		t.Fatalf("problems: %v", got)
	}
	for _, r := range p.Refs {
		if r.File == "game.yaml" {
			t.Fatalf("read a reference from a game.yaml that does not parse: %+v", r)
		}
	}
	if _, err := refs.PlanMove(fsys, "scenes/room.scene.yaml", "scenes/hall.scene.yaml"); err == nil || !strings.Contains(err.Error(), "fix these first") {
		t.Fatalf("PlanMove = %v", err)
	}
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{}); err == nil || !strings.Contains(err.Error(), "game.yaml: project: ") {
		t.Fatalf("PlanIndex = %v", err)
	}
}

func TestUIDsBelongToOneProject(t *testing.T) {
	const scene = "version: 2\nuid: shared0000001\n"
	fsys := fstest.MapFS{
		"a/go.mod":       {Data: []byte("module a\n")},
		"a/x.scene.yaml": {Data: []byte(scene)},
		"b/go.mod":       {Data: []byte("module b\n")},
		"b/x.scene.yaml": {Data: []byte(scene)},
	}
	// Two projects made from one template share uids, which each project's own index keeps apart.
	if got := checkList(scan(t, fsys)); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{Rand: &counter{}})
	if err != nil || len(pl.Assigned) != 0 {
		t.Fatalf("PlanIndex assigned %v, %v", pl, err)
	}
	// Within one project, a shared uid is a copy.
	fsys["a/y.scene.yaml"] = &fstest.MapFile{Data: []byte(scene)}
	if got, want := checkList(scan(t, fsys)), []string{`a/y.scene.yaml:2: uid "shared0000001" is also the uid of a/x.scene.yaml: one of them is a copy, which game index gives a new uid`}; !slices.Equal(got, want) {
		t.Fatalf("problems: %v, want %v", got, want)
	}
	pl, err = refs.PlanIndex(fsys, refs.IndexOptions{Rand: &counter{}})
	if err != nil || !slices.Equal(pl.Assigned, []string{"a/y.scene.yaml"}) {
		t.Fatalf("PlanIndex assigned %v, %v", pl, err)
	}
}

func TestPlanIndexFillsEmptyUIDs(t *testing.T) {
	files := map[string]string{
		"scenes/a.scene.yaml": "version: 2\nuid:\nname: A\n",
		"scenes/b.scene.yaml": "version: 2\nuid: \"\"\n",
		"scenes/c.scene.yaml": "version: 2\nuid: ~ # none yet\n",
		"materials/m.yaml":    "type: material\nversion: 1\nuid: null\r\n",
	}
	fsys := fstest.MapFS{"game.yaml": {Data: []byte(gameHead)}}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}
	// The loaders read an empty or null uid as none, and so does game index, which fills it in where it is.
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{Rand: &counter{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"materials/m.yaml", "scenes/a.scene.yaml", "scenes/b.scene.yaml", "scenes/c.scene.yaml"}; !slices.Equal(pl.Assigned, want) {
		t.Fatalf("assigned %q, want %q", pl.Assigned, want)
	}
	for _, w := range pl.Writes {
		fsys[w.Path] = &fstest.MapFile{Data: w.Data}
	}
	p := scan(t, fsys)
	for _, f := range p.Files {
		if f.Kind == refs.KindProject {
			continue
		}
		var tmpl string
		switch f.Path {
		case "scenes/a.scene.yaml":
			tmpl = "version: 2\nuid: %s\nname: A\n"
		case "scenes/b.scene.yaml":
			tmpl = "version: 2\nuid: \"%s\"\n"
		case "scenes/c.scene.yaml":
			tmpl = "version: 2\nuid: %s # none yet\n"
		case "materials/m.yaml":
			tmpl = "type: material\nversion: 1\nuid: %s\r\n"
		}
		data := fsys[f.Path].Data
		if !resource.ValidUID(f.UID) || string(data) != fmt.Sprintf(tmpl, f.UID) {
			t.Fatalf("%s:\n%q", f.Path, data)
		}
		if f.Kind == refs.KindScene {
			if doc, err := sceneformat.Parse(data); err != nil || doc.UID != f.UID {
				t.Fatalf("%s: sceneformat: %v", f.Path, err)
			}
		} else if r, err := resource.Parse(data); err != nil || r.(*resource.Material).UID != f.UID {
			t.Fatalf("%s: resource: %v", f.Path, err)
		}
	}
}

func TestPlanMoveChangesOnlyTheCase(t *testing.T) {
	dir := dirFS(t, project())
	// On a file system that ignores case, art/Sky.png already names art/sky.png; elsewhere a hard link stands in for that.
	if _, err := os.Lstat(filepath.Join(dir, "art", "Sky.png")); errors.Is(err, fs.ErrNotExist) {
		if err := os.Link(filepath.Join(dir, "art", "sky.png"), filepath.Join(dir, "art", "Sky.png")); err != nil {
			t.Fatal(err)
		}
	}
	move(t, dir, "art/sky.png", "art/Sky.png")
	entries, err := os.ReadDir(filepath.Join(dir, "art"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"Sky.png", "stone.png", "tiles.png"}; !slices.Equal(names, want) {
		t.Fatalf("art holds %q, want %q", names, want)
	}
	if got := read(t, dir, "maps/level.tmj"); !strings.Contains(got, `"../art/Sky.png"`) {
		t.Fatalf("map:\n%s", got)
	}
	clean(t, dir)
	// A different file with a name that differs only in case is still refused.
	if _, err := refs.PlanMove(os.DirFS(dir), "art/stone.png", "art/Sky.png"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("PlanMove onto another file = %v", err)
	}
}

func TestYAMLTilesetsAndLayeredTilemaps(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml":                  {Data: []byte(gameHead)},
		"tilesets/cave.tileset.yaml": {Data: []byte("version: 1\ntileWidth: 16\ntileHeight: 16\nimage: art/cave.png\ncolumns: 1\ntileCount: 1\n")},
		"art/cave.png":               {Data: []byte("cave")},
		"level.scene.yaml": {Data: []byte("version: 2\ntilemaps:\n  shared:\n    tileWidth: 16\n    tileHeight: 16\n    width: 1\n    height: 1\n    tilesetFile: tilesets/cave.tileset.yaml\n    tileLayers:\n      - name: a\n        rows: [\"0\"]\n" +
			"layers:\n  - name: ground\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 1\n      height: 1\n      tilesetFile: tilesets/rock.tileset.yaml\n      tileLayers:\n        - name: a\n          rows: [\"0\"]\n")},
	}
	p := scan(t, fsys)
	want := []string{
		`level.scene.yaml:8 tileset file "tilesets/cave.tileset.yaml" -> tilesets/cave.tileset.yaml`,
		`level.scene.yaml:19 tileset file "tilesets/rock.tileset.yaml" -> tilesets/rock.tileset.yaml`,
		`tilesets/cave.tileset.yaml:4 tileset image "art/cave.png" -> art/cave.png`,
	}
	if got := refList(p); !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := checkList(p); len(got) != 1 || !strings.Contains(got[0], "level.scene.yaml:19") {
		t.Fatalf("problems = %v, want the missing tileset file", got)
	}
	for _, f := range p.Files {
		if f.Path == "tilesets/cave.tileset.yaml" && f.Kind != refs.KindTileset {
			t.Fatalf("tileset read as %q", f.Kind)
		}
	}
}

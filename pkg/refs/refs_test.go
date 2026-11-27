package refs_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/refs"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// gameHead is the smallest valid game.yaml, which tests extend.
const gameHead = "version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\n"

const (
	gameYAML = `version: 1
name: Room
id: dev.example.room
appVersion: 0.1.0
window:
  width: 640
  height: 360
autoload:
  mainScene: room
  scenes:
    - name: room
      path: scenes/room.scene.yaml
  resources:
    - name: stone
      path: "materials/stone.material.yaml"
`
	roomScene = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
uid: scene00000001
inherits: scenes/base.yaml
tilemaps:
  grid:
    source: maps/level.tmj
overrides:
  hero:
    material: 'materials/stone.material.yaml'
layers:
  - name: actors
    objects:
      # The enemy is a scene of its own.
      - id: enemy
        scene: scenes/enemy.scene.yaml
        overrides:
          body:
            material: materials/stone.material.yaml
        children:
          - id: hat
            material: materials/stone.material.yaml
`
	baseScene = `version: 2
uid: scene00000002
layers:
  - name: actors
    objects:
      - id: hero
        material: materials/stone.material.yaml
`
	enemyScene = `version: 2
uid: scene00000003
layers:
  - name: actors
    objects:
      - id: body
`
	stoneMaterial = `type: material
version: 1
uid: mat0000000004
name: Stone
texture: art/stone.png
`
	levelMap = `{
  "tilesets": [{"firstgid": 1, "source": "tiles.tsj"}],
  "layers": [
    {"type": "group", "layers": [{"type": "imagelayer", "image": "../art/sky.png"}]}
  ]
}
`
	tilesTileset = `{"image": "../art/tiles.png"}
`
	worldLDtk = `{"defs": {"tilesets": [{"relPath": "art/tiles.png"}]}, "levels": [{"bgRelPath": null}]}
`
)

// project returns a project in which every reference resolves.
func project() fstest.MapFS {
	return fstest.MapFS{
		"game.yaml":                     {Data: []byte(gameYAML)},
		"scenes/room.scene.yaml":        {Data: []byte(roomScene)},
		"scenes/base.yaml":              {Data: []byte(baseScene)},
		"scenes/enemy.scene.yaml":       {Data: []byte(enemyScene)},
		"materials/stone.material.yaml": {Data: []byte(stoneMaterial)},
		"art/stone.png":                 {Data: []byte("stone")},
		"art/sky.png":                   {Data: []byte("sky")},
		"art/tiles.png":                 {Data: []byte("tiles")},
		"maps/level.tmj":                {Data: []byte(levelMap)},
		"maps/tiles.tsj":                {Data: []byte(tilesTileset)},
		"world.ldtk":                    {Data: []byte(worldLDtk)},
	}
}

func scan(t testing.TB, fsys fstest.MapFS) *refs.Project {
	t.Helper()
	p, err := refs.Scan(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanFindsEveryReference(t *testing.T) {
	p := scan(t, project())
	var got []string
	for _, r := range p.Refs {
		got = append(got, fmt.Sprintf("%s:%d:%d %s %q -> %s", r.File, r.Line, r.Column, r.Field, r.Value, r.Target))
	}
	want := []string{
		`game.yaml:12:13 autoload scene "scenes/room.scene.yaml" -> scenes/room.scene.yaml`,
		`game.yaml:15:13 autoload resource "materials/stone.material.yaml" -> materials/stone.material.yaml`,
		`maps/level.tmj:2:42 external tileset "tiles.tsj" -> maps/tiles.tsj`,
		`maps/level.tmj:4:66 image layer "../art/sky.png" -> art/sky.png`,
		`maps/tiles.tsj:1:11 tileset image "../art/tiles.png" -> art/tiles.png`,
		`materials/stone.material.yaml:5:10 texture "art/stone.png" -> art/stone.png`,
		`scenes/base.yaml:7:19 material "materials/stone.material.yaml" -> materials/stone.material.yaml`,
		`scenes/room.scene.yaml:4:11 inherits "scenes/base.yaml" -> scenes/base.yaml`,
		`scenes/room.scene.yaml:7:13 tilemap source "maps/level.tmj" -> maps/level.tmj`,
		`scenes/room.scene.yaml:10:15 material "materials/stone.material.yaml" -> materials/stone.material.yaml`,
		`scenes/room.scene.yaml:16:16 scene "scenes/enemy.scene.yaml" -> scenes/enemy.scene.yaml`,
		`scenes/room.scene.yaml:19:23 material "materials/stone.material.yaml" -> materials/stone.material.yaml`,
		`scenes/room.scene.yaml:22:23 material "materials/stone.material.yaml" -> materials/stone.material.yaml`,
		`world.ldtk:1:36 tileset image "art/tiles.png" -> art/tiles.png`,
	}
	if !slices.Equal(got, want) {
		for i := range max(len(got), len(want)) {
			if i >= len(got) || i >= len(want) || got[i] != want[i] {
				t.Errorf("ref %d differs", i)
			}
		}
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var files []string
	for _, f := range p.Files {
		files = append(files, fmt.Sprintf("%s %s %s", f.Path, f.Kind, f.UID))
	}
	wantFiles := []string{
		"game.yaml project ",
		"maps/level.tmj tiled ",
		"maps/tiles.tsj tiled ",
		"materials/stone.material.yaml resource mat0000000004",
		"scenes/base.yaml scene scene00000002",
		"scenes/enemy.scene.yaml scene scene00000003",
		"scenes/room.scene.yaml scene scene00000001",
		"world.ldtk ldtk ",
	}
	if !slices.Equal(files, wantFiles) {
		t.Fatalf("files:\n%s\nwant:\n%s", strings.Join(files, "\n"), strings.Join(wantFiles, "\n"))
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems in a clean project: %v", probs)
	}
}

// TestScanAgreesWithLoaders checks that every scene Scan reads is one sceneformat parses, and every resource one resource parses, so the uids it finds are the ones the loaders see.
func TestScanAgreesWithLoaders(t *testing.T) {
	fsys := project()
	for _, f := range scan(t, fsys).Files {
		data := fsys[f.Path].Data
		switch f.Kind {
		case refs.KindScene:
			doc, err := sceneformat.Parse(data)
			if err != nil || doc.UID != f.UID {
				t.Errorf("%s: sceneformat: %v, uid %q, want %q", f.Path, err, doc.UID, f.UID)
			}
		case refs.KindResource:
			r, err := resource.Parse(data)
			if err != nil {
				t.Errorf("%s: %v", f.Path, err)
			} else if m := r.(*resource.Material); m.UID != f.UID {
				t.Errorf("%s: uid %q, want %q", f.Path, m.UID, f.UID)
			}
		}
	}
}

func TestScanRootsAndOutsidePaths(t *testing.T) {
	fsys := fstest.MapFS{
		"a/nsgo.yaml":              {Data: []byte("name: A\n")},
		"a/scenes/x.scene.yaml":    {Data: []byte("version: 1\nlayers:\n  - name: l\n    objects:\n      - id: o\n        material: m.material.yaml\n")},
		"b/game.yaml":              {Data: []byte(gameHead)},
		"b/scenes/y.scene.yaml":    {Data: []byte("version: 1\nlayers:\n  - name: l\n    objects:\n      - id: o\n        material: ../m.material.yaml\n")},
		"b/maps/m.tmj":             {Data: []byte(`{"layers": [{"image": "C:/x.png"}, {"image": "asset:x.png"}, {"image": "../../../x.png"}, {"image": ""}]}`)},
		"testdata/z.scene.yaml":    {Data: []byte("version: 1\ninherits: q.yaml\n")},
		"_old/z.scene.yaml":        {Data: []byte("version: 1\ninherits: q.yaml\n")},
		"broken.scene.yaml":        {Data: []byte("version: [1\n")},
		"notes.yaml":               {Data: []byte("type: memo\n")},
		"seq.scene.yaml":           {Data: []byte("- 1\n")},
		"maps/broken.tmj":          {Data: []byte(`{"layers": [`)},
		"materials/raw.yaml":       {Data: []byte("type: material\nversion: 1\nuid: bad\n")},
		"materials/num.yaml":       {Data: []byte("type: material\nversion: 1\ntexture: 3\n")},
		"materials/tagged.yaml":    {Data: []byte("type: material\nversion: 1\ntexture: !!binary aGk=\n")},
		"materials/anchored.yaml":  {Data: []byte("type: material\nversion: 1\nname: &n art/x.png\ntexture: *n\n")},
		"scenes/flow.scene.yaml":   {Data: []byte("{version: 1, inherits: base.yaml}\n")},
		"scenes/deep/d.scene.yaml": {Data: []byte("version: 1\ninherits: scenes/flow.scene.yaml\n")},
	}
	p := scan(t, fsys)
	var got []string
	for _, r := range p.Refs {
		got = append(got, fmt.Sprintf("%s %s %q -> %q root %s", r.File, r.Field, r.Value, r.Target, p.Root(r.File)))
	}
	want := []string{
		`a/scenes/x.scene.yaml material "m.material.yaml" -> "a/m.material.yaml" root a`,
		`b/maps/m.tmj image layer "C:/x.png" -> "" root b`,
		`b/maps/m.tmj image layer "asset:x.png" -> "" root b`,
		`b/maps/m.tmj image layer "../../../x.png" -> "" root b`,
		`b/scenes/y.scene.yaml material "../m.material.yaml" -> "" root b`,
		`materials/anchored.yaml texture "art/x.png" -> "art/x.png" root .`,
		`scenes/deep/d.scene.yaml inherits "scenes/flow.scene.yaml" -> "scenes/flow.scene.yaml" root .`,
		`scenes/flow.scene.yaml inherits "base.yaml" -> "base.yaml" root .`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// A reference that names no file in the directory is not checked; the others are.
	var probs []string
	for _, prob := range p.Check() {
		probs = append(probs, prob.String())
	}
	wantProbs := []string{
		`a/scenes/x.scene.yaml:6: material "m.material.yaml": not found`,
		`materials/anchored.yaml:3: texture "art/x.png": not found`,
		`scenes/flow.scene.yaml:1: inherits "base.yaml": not found`,
	}
	if !slices.Equal(probs, wantProbs) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(probs, "\n"), strings.Join(wantProbs, "\n"))
	}
}

func TestRelPath(t *testing.T) {
	for _, tc := range []struct{ dir, target, want string }{
		{".", "a/b.png", "a/b.png"},
		{"maps", "maps/t.tsj", "t.tsj"},
		{"maps", "art/sky.png", "../art/sky.png"},
		{"maps/deep", "maps/x.png", "../x.png"},
		{"a/b", "c/d/e.png", "../../c/d/e.png"},
		{"maps", "maps", "../maps"},
	} {
		if got := refs.RelPath(tc.dir, tc.target); got != tc.want {
			t.Errorf("RelPath(%q, %q) = %q, want %q", tc.dir, tc.target, got, tc.want)
		}
	}
}

// indexed returns project() with the index game index would write for it.
func indexed(t testing.TB) fstest.MapFS {
	t.Helper()
	fsys := project()
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Writes) != 1 || pl.Writes[0].Path != refs.IndexFileName || len(pl.Assigned) != 0 || len(pl.Updated) != 0 {
		t.Fatalf("plan for a project whose files all have uids: %+v", pl)
	}
	fsys[refs.IndexFileName] = &fstest.MapFile{Data: pl.Writes[0].Data}
	return fsys
}

func TestIndexOfProject(t *testing.T) {
	fsys := indexed(t)
	const header = "# The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.\nversion: 1\nfiles:\n"
	got := string(fsys[refs.IndexFileName].Data)
	// The hashes depend on the test files' contents; check the shape and the uid entries exactly.
	ix, err := refs.ParseIndex([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range ix.Files {
		kind := "sha256"
		if e.UID != "" {
			kind = "uid " + e.UID
		}
		paths = append(paths, e.Path+" "+kind)
	}
	wantPaths := []string{
		"art/sky.png sha256",
		"art/stone.png sha256",
		"art/tiles.png sha256",
		"maps/level.tmj sha256",
		"maps/tiles.tsj sha256",
		"materials/stone.material.yaml uid mat0000000004",
		"scenes/base.yaml uid scene00000002",
		"scenes/enemy.scene.yaml uid scene00000003",
		"scenes/room.scene.yaml uid scene00000001",
	}
	if !slices.Equal(paths, wantPaths) {
		t.Fatalf("index:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(wantPaths, "\n"))
	}
	if !strings.HasPrefix(got, header) {
		t.Fatalf("index starts %q", got)
	}
	again, err := refs.EncodeIndex(ix)
	if err != nil || string(again) != got {
		t.Fatalf("index does not round-trip: %v\n%s", err, again)
	}
	// An index that is up to date is not written again.
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{})
	if err != nil || !pl.Empty() {
		t.Fatalf("second game index plans %+v, %v", pl, err)
	}
}

func TestIndexNormalizesCRLFForTextFiles(t *testing.T) {
	actorsLF := "version: 1\nactors:\n  - id: hero\n    hp: 100\n"
	actorsCRLF := strings.ReplaceAll(actorsLF, "\n", "\r\n")
	gameLF := gameHead + "autoload:\n  resources:\n    - name: actors\n      path: data/actors.yaml\n"
	gameCRLF := strings.ReplaceAll(gameLF, "\n", "\r\n")

	lfFS := fstest.MapFS{
		"game.yaml":        {Data: []byte(gameLF)},
		"data/actors.yaml": {Data: []byte(actorsLF)},
	}
	crlfFS := fstest.MapFS{
		"game.yaml":        {Data: []byte(gameCRLF)},
		"data/actors.yaml": {Data: []byte(actorsCRLF)},
	}

	plLF, err := refs.PlanIndex(lfFS, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plCRLF, err := refs.PlanIndex(crlfFS, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plLF.Writes) != 1 || len(plCRLF.Writes) != 1 {
		t.Fatalf("writes: LF=%d CRLF=%d, want 1 each", len(plLF.Writes), len(plCRLF.Writes))
	}
	if !bytes.Equal(plLF.Writes[0].Data, plCRLF.Writes[0].Data) {
		t.Fatalf("LF and CRLF YAML produced different indexes:\nLF:\n%s\nCRLF:\n%s", plLF.Writes[0].Data, plCRLF.Writes[0].Data)
	}

	// Committing the LF index and running PlanIndex on a CRLF checkout plans nothing.
	crlfFS[refs.IndexFileName] = &fstest.MapFile{Data: plLF.Writes[0].Data}
	plNoop, err := refs.PlanIndex(crlfFS, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !plNoop.Empty() {
		t.Fatalf("PlanIndex on CRLF checkout with LF index planned %s", describe(plNoop))
	}

	// Moved finds a YAML file moved on a CRLF checkout against an LF index.
	rename(crlfFS, "data/actors.yaml", "data/npcs.yaml")
	p := scan(t, crlfFS)
	if to, ok := p.Moved("data/actors.yaml"); !ok || to != "data/npcs.yaml" {
		t.Fatalf("Moved(data/actors.yaml) = %q, %v; want data/npcs.yaml, true", to, ok)
	}

	// PlanMove rewriting an edited CRLF text file (Tiled .tmj) still records the LF-normalized SHA-256.
	mapLF := "{\n  \"layers\": [\n    {\"type\": \"imagelayer\", \"image\": \"../art/sky.png\"}\n  ]\n}\n"
	mapCRLF := strings.ReplaceAll(mapLF, "\n", "\r\n")
	moveLF := fstest.MapFS{
		"game.yaml":      {Data: []byte(gameHead)},
		"maps/level.tmj": {Data: []byte(mapLF)},
		"art/sky.png":    {Data: []byte("\x89PNG\r\n\x1a\nsky")},
	}
	moveCRLF := fstest.MapFS{
		"game.yaml":      {Data: []byte(gameHead)},
		"maps/level.tmj": {Data: []byte(mapCRLF)},
		"art/sky.png":    {Data: []byte("\x89PNG\r\n\x1a\nsky")},
	}
	for _, fsys := range []fstest.MapFS{moveLF, moveCRLF} {
		fsys["scenes/room.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 2\nuid: scene00000001\ntilemaps:\n  grid:\n    source: maps/level.tmj\n")}
	}
	pmLF, err := refs.PlanMove(moveLF, "art/sky.png", "art/bg/sky.png")
	if err != nil {
		t.Fatal(err)
	}
	pmCRLF, err := refs.PlanMove(moveCRLF, "art/sky.png", "art/bg/sky.png")
	if err != nil {
		t.Fatal(err)
	}
	findIndex := func(pl *refs.Plan) []byte {
		for _, w := range pl.Writes {
			if w.Path == refs.IndexFileName {
				return w.Data
			}
		}
		return nil
	}
	if ixLF, ixCRLF := findIndex(pmLF), findIndex(pmCRLF); !bytes.Equal(ixLF, ixCRLF) {
		t.Fatalf("PlanMove index differs between LF and CRLF .tmj:\nLF:\n%s\nCRLF:\n%s", ixLF, ixCRLF)
	}
}

func TestIndexPreservesBinaryBytes(t *testing.T) {
	// Even a binary-extension file whose bytes happen to be ASCII, or an unknown-extension binary file containing NUL/non-UTF-8 bytes, must hash its exact bytes rather than normalizing CRLF.
	for _, tc := range []struct {
		name string
		path string
		a, b []byte
	}{
		{"png_header", "art/pic.png", []byte("\x89PNG\r\n\x1a\nrest"), []byte("\x89PNG\n\x1a\nrest")},
		{"png_ascii", "art/stub.png", []byte("line1\r\nline2\r\n"), []byte("line1\nline2\n")},
		{"bin_ascii", "data/buf.bin", []byte("ab\r\ncd"), []byte("ab\ncd")},
		{"unknown_ext_binary", "data/raw.dat", []byte("\x00\xff\r\nbin"), []byte("\x00\xff\nbin")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mk := func(data []byte) []byte {
				fsys := fstest.MapFS{
					"game.yaml": {Data: []byte(gameHead + "autoload:\n  resources:\n    - name: res\n      path: " + tc.path + "\n")},
					tc.path:     {Data: data},
				}
				pl, err := refs.PlanIndex(fsys, refs.IndexOptions{})
				if err != nil {
					t.Fatal(err)
				}
				return pl.Writes[0].Data
			}
			if bytes.Equal(mk(tc.a), mk(tc.b)) {
				t.Errorf("%s: binary bytes with CRLF and LF hashed identically", tc.path)
			}
		})
	}
}

// describe returns what pl moves and writes, for failure messages.
func describe(pl *refs.Plan) string {
	if pl == nil {
		return "nil"
	}
	var b strings.Builder
	for _, m := range pl.Moves {
		fmt.Fprintf(&b, "move %s to %s\n", m.From, m.To)
	}
	for _, w := range pl.Writes {
		fmt.Fprintf(&b, "write %s:\n%s\n", w.Path, w.Data)
	}
	return b.String()
}

// rename returns fsys with the file at from moved to to.
func rename(fsys fstest.MapFS, from, to string) {
	fsys[to] = fsys[from]
	delete(fsys, from)
}

func TestMovedFindsFilesMovedWithoutTheTools(t *testing.T) {
	fsys := indexed(t)
	rename(fsys, "materials/stone.material.yaml", "materials/rock/stone.material.yaml")
	rename(fsys, "art/sky.png", "art/bg/sky.png")
	rename(fsys, "scenes/enemy.scene.yaml", "actors/enemy.scene.yaml")
	p := scan(t, fsys)
	var got []string
	for _, prob := range p.Check() {
		got = append(got, prob.String())
	}
	want := []string{
		`game.yaml:15: autoload resource "materials/stone.material.yaml": not found; it moved to "materials/rock/stone.material.yaml"; run game index to update the reference`,
		`maps/level.tmj:4: image layer "../art/sky.png": not found; it moved to "../art/bg/sky.png"; run game index to update the reference`,
		`scenes/base.yaml:7: material "materials/stone.material.yaml": not found; it moved to "materials/rock/stone.material.yaml"; run game index to update the reference`,
		`scenes/room.scene.yaml:10: material "materials/stone.material.yaml": not found; it moved to "materials/rock/stone.material.yaml"; run game index to update the reference`,
		`scenes/room.scene.yaml:16: scene "scenes/enemy.scene.yaml": not found; it moved to "actors/enemy.scene.yaml"; run game index to update the reference`,
		`scenes/room.scene.yaml:19: material "materials/stone.material.yaml": not found; it moved to "materials/rock/stone.material.yaml"; run game index to update the reference`,
		`scenes/room.scene.yaml:22: material "materials/stone.material.yaml": not found; it moved to "materials/rock/stone.material.yaml"; run game index to update the reference`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A copy with the same contents makes an image ambiguous, and an image the index never listed is not found again.
	fsys["art/copy/sky.png"] = &fstest.MapFile{Data: []byte("sky")}
	fsys["maps/tiles.tsj"] = &fstest.MapFile{Data: []byte(`{"image": "../art/new.png"}`)}
	p = scan(t, fsys)
	for _, target := range []string{"art/sky.png", "art/new.png", "art/stone.png"} {
		if to, ok := p.Moved(target); ok {
			t.Errorf("Moved(%q) = %q", target, to)
		}
	}
	if to, ok := p.Moved("scenes/enemy.scene.yaml"); !ok || to != "actors/enemy.scene.yaml" {
		t.Errorf("Moved(enemy) = %q, %v", to, ok)
	}
}

// dirFS writes fsys to a new temporary directory and returns it.
func dirFS(t *testing.T, fsys fstest.MapFS) string {
	t.Helper()
	dir := t.TempDir()
	for name, f := range fsys {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// clean checks that the project in dir has no problems.
func clean(t *testing.T, dir string) {
	t.Helper()
	p, err := refs.Scan(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems after the plan: %v", probs)
	}
}

func move(t *testing.T, dir, from, to string) *refs.Plan {
	t.Helper()
	pl, err := refs.PlanMove(os.DirFS(dir), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := pl.Apply(dir); err != nil {
		t.Fatal(err)
	}
	return pl
}

func TestPlanMoveFile(t *testing.T) {
	dir := dirFS(t, indexed(t))
	pl := move(t, dir, "materials/stone.material.yaml", "materials/rock.material.yaml")
	if len(pl.Updated) != 5 {
		t.Fatalf("updated %d references, want 5: %+v", len(pl.Updated), pl.Updated)
	}
	var written []string
	for _, w := range pl.Writes {
		written = append(written, w.Path)
	}
	if want := []string{"game.index.yaml", "game.yaml", "scenes/base.yaml", "scenes/room.scene.yaml"}; !slices.Equal(written, want) {
		t.Fatalf("wrote %q, want %q", written, want)
	}
	// Each reference keeps its quoting, and everything else in the file, comments included, is kept byte for byte.
	want := strings.NewReplacer(
		"'materials/stone.material.yaml'", "'materials/rock.material.yaml'",
		"material: materials/stone.material.yaml", "material: materials/rock.material.yaml",
	).Replace(roomScene)
	if got := read(t, dir, "scenes/room.scene.yaml"); got != want {
		t.Fatalf("room scene:\n%s\nwant:\n%s", got, want)
	}
	if got, want := read(t, dir, "game.yaml"), strings.Replace(gameYAML, `"materials/stone.material.yaml"`, `"materials/rock.material.yaml"`, 1); got != want {
		t.Fatalf("game.yaml:\n%s", got)
	}
	if got := read(t, dir, "materials/rock.material.yaml"); got != stoneMaterial {
		t.Fatalf("moved file changed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "materials", "stone.material.yaml")); !os.IsNotExist(err) {
		t.Fatalf("old file still there: %v", err)
	}
	if ix := read(t, dir, refs.IndexFileName); !strings.Contains(ix, "  - path: materials/rock.material.yaml\n    uid: mat0000000004\n") || strings.Contains(ix, "stone.material") {
		t.Fatalf("index:\n%s", ix)
	}
	clean(t, dir)
}

func TestPlanMoveDirectory(t *testing.T) {
	dir := dirFS(t, indexed(t))
	// The maps move into a deeper directory: references to them change, and so do their own references relative to themselves, except between two files that move together.
	move(t, dir, "maps", "levels/maps")
	if got := read(t, dir, "levels/maps/level.tmj"); got != strings.Replace(levelMap, `"../art/sky.png"`, `"../../art/sky.png"`, 1) {
		t.Fatalf("level map:\n%s", got)
	}
	if got := read(t, dir, "levels/maps/tiles.tsj"); got != `{"image": "../../art/tiles.png"}`+"\n" {
		t.Fatalf("tileset:\n%s", got)
	}
	if got := read(t, dir, "scenes/room.scene.yaml"); !strings.Contains(got, "    source: levels/maps/level.tmj\n") {
		t.Fatalf("room scene:\n%s", got)
	}
	clean(t, dir)

	// Moving the images they name rewrites the JSON in place.
	move(t, dir, "art", "gfx")
	if got := read(t, dir, "world.ldtk"); got != strings.Replace(worldLDtk, `"art/tiles.png"`, `"gfx/tiles.png"`, 1) {
		t.Fatalf("world:\n%s", got)
	}
	if got := read(t, dir, "materials/stone.material.yaml"); !strings.Contains(got, "texture: gfx/stone.png\n") {
		t.Fatalf("material:\n%s", got)
	}
	clean(t, dir)
}

func TestPlanMoveRefuses(t *testing.T) {
	fsys := indexed(t)
	fsys["sub/go.mod"] = &fstest.MapFile{Data: []byte("module sub\n")}
	fsys["sub/x.png"] = &fstest.MapFile{Data: []byte("x")}
	fsys["scenes/long.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 1\nlayers:\n  - name: l\n    objects:\n      - id: o\n        material: >-\n          art/sky.png\n")}
	for _, tc := range []struct{ name, from, to, msg string }{
		{"missing", "art/none.png", "art/x.png", "file does not exist"},
		{"exists", "art/sky.png", "art/stone.png", "art/stone.png already exists"},
		{"same", "art/sky.png", "art/sky.png", "already at"},
		{"into itself", "art", "art/deeper", "into itself"},
		{"invalid", "art/sky.png", "../sky.png", "must be a slash-separated path"},
		{"project file", "game.yaml", "config/game.yaml", "stay at the project root"},
		{"index", refs.IndexFileName, "x.yaml", "stay at the project root"},
		{"to index", "art/sky.png", "art/" + refs.IndexFileName, "stay at the project root"},
		{"other project", "art/sky.png", "sub/sky.png", "out of its project root"},
		{"holds a project", "sub", "lib/sub", "a project of its own"},
		{"block scalar", "art/sky.png", "art/bg/sky.png", "cannot be rewritten in place"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := refs.PlanMove(fsys, tc.from, tc.to)
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("PlanMove(%q, %q) = %v, want an error containing %q", tc.from, tc.to, err, tc.msg)
			}
		})
	}

	// A scene that does not parse but names the file stops the move; one that does not name it does not.
	fsys = indexed(t)
	fsys["scenes/bad.scene.yaml"] = &fstest.MapFile{Data: []byte("version: [1\nmaterial: materials/stone.material.yaml\n")}
	if _, err := refs.PlanMove(fsys, "materials/stone.material.yaml", "m.material.yaml"); err == nil || !strings.Contains(err.Error(), "scenes/bad.scene.yaml does not parse") {
		t.Fatalf("move with an unparsed scene naming it: %v", err)
	}
	if _, err := refs.PlanMove(fsys, "art/sky.png", "art/bg/sky.png"); err != nil {
		t.Fatalf("move with an unparsed scene not naming it: %v", err)
	}

	// A broken index or a uid two files share must be fixed first.
	fsys = indexed(t)
	fsys["scenes/copy.scene.yaml"] = fsys["scenes/enemy.scene.yaml"]
	if _, err := refs.PlanMove(fsys, "art/sky.png", "art/bg/sky.png"); err == nil || !strings.Contains(err.Error(), "is also the uid of") {
		t.Fatalf("move with a shared uid: %v", err)
	}
	fsys = indexed(t)
	fsys[refs.IndexFileName] = &fstest.MapFile{Data: []byte("version: 1\nfiles:\n  - path: a\n    extra: 1\n")}
	if _, err := refs.PlanMove(fsys, "art/sky.png", "art/bg/sky.png"); err == nil || !strings.Contains(err.Error(), "game.index.yaml:4: field extra not found") {
		t.Fatalf("move with a broken index: %v", err)
	}
}

// counter is a deterministic source of uids: each read returns the next byte value.
type counter struct{ n byte }

func (c *counter) Read(p []byte) (int, error) {
	for i := range p {
		c.n++
		p[i] = c.n
	}
	return len(p), nil
}

func TestPlanIndexAssignsUIDsAndFollowsMoves(t *testing.T) {
	fsys := indexed(t)
	// A scene and a material without a uid, one with CRLF line breaks; a copied scene; and files moved without the tools.
	fsys["scenes/new.scene.yaml"] = &fstest.MapFile{Data: []byte("# New.\nversion: 2\nname: New\n")}
	fsys["materials/glass.yaml"] = &fstest.MapFile{Data: []byte("type: material\r\nversion: 1\r\nopacity: 0.5\r\n")}
	fsys["materials/last.yaml"] = &fstest.MapFile{Data: []byte("type: material\nversion: 1")}
	fsys["scenes/a-copy.scene.yaml"] = fsys["scenes/enemy.scene.yaml"]
	rename(fsys, "materials/stone.material.yaml", "materials/rock.material.yaml")
	rename(fsys, "art/tiles.png", "art/tileset.png")

	dir := dirFS(t, fsys)
	pl, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{Rand: &counter{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"materials/glass.yaml", "materials/last.yaml", "scenes/a-copy.scene.yaml", "scenes/new.scene.yaml"}; !slices.Equal(pl.Assigned, want) {
		t.Fatalf("assigned %q, want %q", pl.Assigned, want)
	}
	if len(pl.Updated) != 7 {
		t.Fatalf("updated %d references, want 7: %+v", len(pl.Updated), pl.Updated)
	}
	if err := pl.Apply(dir); err != nil {
		t.Fatal(err)
	}
	uids := map[string]string{}
	for _, name := range pl.Assigned {
		p := scan(t, fstest.MapFS{name: {Data: []byte(read(t, dir, name))}})
		if len(p.Files) != 1 || !resource.ValidUID(p.Files[0].UID) {
			t.Fatalf("%s has no uid:\n%s", name, read(t, dir, name))
		}
		uids[name] = p.Files[0].UID
	}
	if got, want := read(t, dir, "scenes/new.scene.yaml"), "# New.\nversion: 2\nuid: "+uids["scenes/new.scene.yaml"]+"\nname: New\n"; got != want {
		t.Fatalf("new scene:\n%q\nwant\n%q", got, want)
	}
	if got, want := read(t, dir, "materials/glass.yaml"), "type: material\r\nversion: 1\r\nuid: "+uids["materials/glass.yaml"]+"\r\nopacity: 0.5\r\n"; got != want {
		t.Fatalf("glass:\n%q\nwant\n%q", got, want)
	}
	if got, want := read(t, dir, "materials/last.yaml"), "type: material\nversion: 1\nuid: "+uids["materials/last.yaml"]+"\n"; got != want {
		t.Fatalf("last:\n%q\nwant\n%q", got, want)
	}
	// The copy gets a new uid; the original, which the index lists, keeps its own.
	if got := read(t, dir, "scenes/a-copy.scene.yaml"); got != strings.Replace(enemyScene, "scene00000003", uids["scenes/a-copy.scene.yaml"], 1) {
		t.Fatalf("copy:\n%s", got)
	}
	if got := read(t, dir, "scenes/enemy.scene.yaml"); got != enemyScene {
		t.Fatalf("original:\n%s", got)
	}
	if got := read(t, dir, "maps/tiles.tsj"); got != `{"image": "../art/tileset.png"}`+"\n" {
		t.Fatalf("tileset:\n%s", got)
	}
	if got := read(t, dir, "scenes/base.yaml"); !strings.Contains(got, "material: materials/rock.material.yaml\n") {
		t.Fatalf("base:\n%s", got)
	}
	// Each new file parses with its loader, uid included.
	for name, id := range uids {
		data := []byte(read(t, dir, name))
		if strings.Contains(name, ".scene.") {
			if doc, err := sceneformat.Parse(data); err != nil || doc.UID != id {
				t.Errorf("%s: %v", name, err)
			}
		} else if r, err := resource.Parse(data); err != nil || r.(*resource.Material).UID != id {
			t.Errorf("%s: %v", name, err)
		}
	}
	clean(t, dir)
	if pl, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{}); err != nil || !pl.Empty() {
		t.Fatalf("second game index plans %v, %v", describe(pl), err)
	}
}

func TestPlanIndexFindsSceneByUIDAlone(t *testing.T) {
	// scenes/base.yaml is a scene only because room inherits it; once moved, only its uid finds it.
	fsys := indexed(t)
	rename(fsys, "scenes/base.yaml", "scenes/common/base.yaml")
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Updated) != 1 || pl.Updated[0].Field != refs.FieldInherits {
		t.Fatalf("updated %+v", pl.Updated)
	}
	for _, w := range pl.Writes {
		fsys[w.Path] = &fstest.MapFile{Data: w.Data}
	}
	if got := string(fsys["scenes/room.scene.yaml"].Data); !strings.Contains(got, "inherits: scenes/common/base.yaml\n") {
		t.Fatalf("room:\n%s", got)
	}
	if got := string(fsys[refs.IndexFileName].Data); !strings.Contains(got, "  - path: scenes/common/base.yaml\n    uid: scene00000002\n") {
		t.Fatalf("index:\n%s", got)
	}
	if pl, err := refs.PlanIndex(fsys, refs.IndexOptions{}); err != nil || !pl.Empty() {
		t.Fatalf("second game index plans %v, %v", describe(pl), err)
	}
}

func TestPlanIndexKeepsWhatItCannotFind(t *testing.T) {
	fsys := indexed(t)
	delete(fsys, "art/sky.png")
	pl, err := refs.PlanIndex(fsys, refs.IndexOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing changes: the index keeps the entry of the missing image, so a later scan can still find it once it is back under a new path.
	if !pl.Empty() {
		t.Fatalf("plan: %+v", pl)
	}
	fsys["art/bg/sky.png"] = &fstest.MapFile{Data: []byte("sky")}
	if to, ok := scan(t, fsys).Moved("art/sky.png"); !ok || to != "art/bg/sky.png" {
		t.Fatalf("Moved = %q, %v", to, ok)
	}
}

func TestPlanIndexRefuses(t *testing.T) {
	fsys := project()
	fsys["materials/bad.yaml"] = &fstest.MapFile{Data: []byte("type: material\nversion: 1\nuid: nope\n")}
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{}); err == nil || !strings.Contains(err.Error(), `materials/bad.yaml:3: uid "nope" is not a uid`) {
		t.Fatalf("PlanIndex with a bad uid: %v", err)
	}
	fsys = project()
	fsys["scenes/flow.scene.yaml"] = &fstest.MapFile{Data: []byte("{version: 1}\n")}
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{}); err == nil || !strings.Contains(err.Error(), "flow mapping") {
		t.Fatalf("PlanIndex with a flow scene: %v", err)
	}
	fsys = project()
	fsys["scenes/noversion.scene.yaml"] = &fstest.MapFile{Data: []byte("name: x\n")}
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{}); err == nil || !strings.Contains(err.Error(), "no version") {
		t.Fatalf("PlanIndex without a version: %v", err)
	}
	fsys = project()
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{Rand: bytes.NewReader(nil)}); err != nil {
		t.Fatalf("PlanIndex needing no uid read from its source: %v", err)
	}
	fsys["scenes/new.scene.yaml"] = &fstest.MapFile{Data: []byte("version: 1\n")}
	if _, err := refs.PlanIndex(fsys, refs.IndexOptions{Rand: bytes.NewReader(nil)}); err == nil {
		t.Fatal("PlanIndex with an empty source of uids succeeded")
	}
}

func TestRewriteKeepsQuoting(t *testing.T) {
	for _, tc := range []struct{ name, value, written string }{
		{"plain", `materials/stone.material.yaml`, `materials/rock.material.yaml`},
		{"double", `"materials/stone.material.yaml"`, `"materials/rock.material.yaml"`},
		{"single", `'materials/stone.material.yaml'`, `'materials/rock.material.yaml'`},
		{"flow", `{x: 1}`, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.written == "" {
				return
			}
			fsys := fstest.MapFS{
				"game.yaml":                     {Data: []byte(gameHead + "autoload:\n  resources:\n    - name: s\n      path: " + tc.value + " # the stone\n")},
				"materials/stone.material.yaml": {Data: []byte(stoneMaterial)},
			}
			pl, err := refs.PlanMove(fsys, "materials/stone.material.yaml", "materials/rock.material.yaml")
			if err != nil {
				t.Fatal(err)
			}
			want := gameHead + "autoload:\n  resources:\n    - name: s\n      path: " + tc.written + " # the stone\n"
			if got := string(pl.Writes[1].Data); pl.Writes[1].Path != "game.yaml" || got != want {
				t.Fatalf("%s:\n%s\nwant:\n%s", pl.Writes[1].Path, got, want)
			}
		})
	}
	// A new path that plain YAML would read differently is quoted.
	fsys := fstest.MapFS{
		"game.yaml":   {Data: []byte(gameHead + "autoload:\n  resources:\n    - name: s\n      path: a.png\n    - name: t\n      path: 'b.png'\n")},
		"a.png":       {Data: []byte("a")},
		"b.png":       {Data: []byte("b")},
		"sub/ok.yaml": {Data: []byte("x: 1\n")},
	}
	pl, err := refs.PlanMove(fsys, "a.png", "art/#1, a: b.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(pl.Writes[1].Data); !strings.Contains(got, `path: "art/#1, a: b.png"`) {
		t.Fatalf("game.yaml:\n%s", got)
	}
	pl, err = refs.PlanMove(fsys, "b.png", "it's.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(pl.Writes[1].Data); !strings.Contains(got, `path: 'it''s.png'`) {
		t.Fatalf("game.yaml:\n%s", got)
	}
}

func TestIndexValidate(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, tc := range []struct{ name, data, msg string }{
		{"empty", "", "empty index"},
		{"version", "version: 2\n", "unsupported index version 2"},
		{"unknown field", "version: 1\nextra: 1\n", `unknown field "extra"`},
		{"trailing", "version: 1\n---\nversion: 1\n", "trailing YAML document"},
		{"bad path", "version: 1\nfiles:\n  - path: ../x\n    sha256: " + sha + "\n", "must be a slash-separated path"},
		{"order", "version: 1\nfiles:\n  - path: b\n    sha256: " + sha + "\n  - path: a\n    sha256: " + sha + "\n", "must come after"},
		{"twice", "version: 1\nfiles:\n  - path: a\n    sha256: " + sha + "\n  - path: a\n    sha256: " + sha + "\n", "must come after"},
		{"both", "version: 1\nfiles:\n  - path: a\n    sha256: " + sha + "\n    uid: 0000000000000\n", "both a uid and a sha256"},
		{"neither", "version: 1\nfiles:\n  - path: a\n", "needs a uid or a sha256"},
		{"bad uid", "version: 1\nfiles:\n  - path: a\n    uid: 000000000000u\n", "is not a uid"},
		{"bad sha", "version: 1\nfiles:\n  - path: a\n    sha256: ABC\n", "64 lower-case hexadecimal digits"},
		{"same uid", "version: 1\nfiles:\n  - path: a\n    uid: 0000000000000\n  - path: b\n    uid: 0000000000000\n", "have the same uid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := refs.ParseIndex([]byte(tc.data)); err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("ParseIndex = %v, want an error containing %q", err, tc.msg)
			}
		})
	}
	if _, err := refs.EncodeIndex(nil); err == nil {
		t.Fatal("EncodeIndex(nil) succeeded")
	}
	ix := &refs.Index{Version: 1, Files: []refs.IndexEntry{{Path: "a", UID: "0000000000000"}, {Path: "b/c.png", SHA256: sha}}}
	if e, ok := ix.Lookup("b/c.png"); !ok || e.SHA256 != sha {
		t.Fatalf("Lookup = %+v, %v", e, ok)
	}
	if _, ok := ix.Lookup("b"); ok {
		t.Fatal("Lookup of an unlisted path succeeded")
	}
	if _, ok := (*refs.Index)(nil).Lookup("a"); ok {
		t.Fatal("Lookup in a nil index succeeded")
	}
}

func TestApplyStopsAtAnExistingDestination(t *testing.T) {
	dir := dirFS(t, project())
	pl := &refs.Plan{Moves: []refs.Move{{From: "art/sky.png", To: "art/stone.png"}}}
	if err := pl.Apply(dir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Apply = %v", err)
	}
	if err := (&refs.Plan{}).Apply(filepath.Join(dir, "none")); err == nil {
		t.Fatal("Apply in a missing directory succeeded")
	}
	// A write keeps the file's permissions and leaves no temporary file behind.
	if err := os.Chmod(filepath.Join(dir, "game.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	pl = &refs.Plan{Writes: []refs.Write{{Path: "game.yaml", Data: []byte("x\n")}, {Path: "new/dir/f.yaml", Data: []byte("y\n")}}}
	if err := pl.Apply(dir); err == nil {
		t.Fatal("Apply wrote into a directory that does not exist")
	}
	info, err := os.Stat(filepath.Join(dir, "game.yaml"))
	if err != nil || info.Mode().Perm() != 0o600 || read(t, dir, "game.yaml") != "x\n" {
		t.Fatalf("game.yaml: %v %v %q", info.Mode(), err, read(t, dir, "game.yaml"))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("left %s behind", e.Name())
		}
	}
}

func FuzzScanScene(f *testing.F) {
	f.Add([]byte(roomScene))
	f.Add([]byte("version: 1\ninherits: &a x.yaml\nlayers: [{name: l, objects: [{id: o, scene: *a}]}]\n"))
	f.Add([]byte("version: 1\nlayers:\n- name: l\n  objects:\n  - id: o\n    material: \"m\\u00e9.yaml\"\n"))
	f.Fuzz(func(t *testing.T, scene []byte) {
		fsys := project()
		fsys["scenes/room.scene.yaml"] = &fstest.MapFile{Data: scene}
		p, err := refs.Scan(fsys)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range p.Refs {
			if r.Line < 1 || r.Column < 1 {
				t.Fatalf("reference %+v has no place", r)
			}
		}
		// Moving the material either fails or yields files whose references all resolve to the moved file.
		pl, err := refs.PlanMove(fsys, "materials/stone.material.yaml", "materials/rock.material.yaml")
		if err != nil {
			return
		}
		moved := fstest.MapFS{}
		for k, v := range fsys {
			moved[k] = v
		}
		rename(moved, "materials/stone.material.yaml", "materials/rock.material.yaml")
		for _, w := range pl.Writes {
			moved[w.Path] = &fstest.MapFile{Data: w.Data}
		}
		after := scan(t, moved)
		for _, r := range after.Refs {
			if r.Target == "materials/stone.material.yaml" {
				t.Fatalf("reference %+v still names the old path:\n%s", r, moved[r.File].Data)
			}
		}
	})
}

func FuzzParseIndex(f *testing.F) {
	f.Add([]byte("version: 1\nfiles:\n  - path: a\n    uid: 0000000000000\n"))
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		ix, err := refs.ParseIndex(data)
		if err != nil {
			return
		}
		out, err := refs.EncodeIndex(ix)
		if err != nil {
			t.Fatalf("EncodeIndex of a parsed index: %v", err)
		}
		again, err := refs.ParseIndex(out)
		if err != nil {
			t.Fatal(err)
		}
		out2, err := refs.EncodeIndex(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding does not round-trip:\n%s\n%s", out, out2)
		}
	})
}

func BenchmarkScan(b *testing.B) {
	fsys := indexed(b)
	for b.Loop() {
		if _, err := refs.Scan(fsys); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlanMove(b *testing.B) {
	fsys := indexed(b)
	for b.Loop() {
		if _, err := refs.PlanMove(fsys, "materials/stone.material.yaml", "materials/rock.material.yaml"); err != nil {
			b.Fatal(err)
		}
	}
}

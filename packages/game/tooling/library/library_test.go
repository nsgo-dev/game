package library_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// repoLibrary is the shared library in this repository's library directory, which DefaultIndex serves.
var repoLibrary = filepath.Join("..", "..", "..", "..", "library")

// minimalProject is a game.yaml with nothing but what Parse requires.
const minimalProject = "version: 1\nname: Cave Run\nid: dev.example.caverun\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 180\n"

// newProject writes a project directory holding game.yaml with content and returns its path.
func newProject(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// readProjectFile reads and parses dir's game.yaml.
func readProjectFile(t *testing.T, dir string) *project.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	f, err := project.Parse(data)
	if err != nil {
		t.Fatalf("game.yaml after the change does not parse: %v\n%s", err, data)
	}
	return f
}

// TestInstallSampleAssetPack installs the shared library's sample asset pack into a project: its files land under packs/dungeon-tiles, game.yaml autoloads its atlas and records the pack, and the project and its assets check clean. Installing it again changes nothing, and removing it restores game.yaml byte for byte.
func TestInstallSampleAssetPack(t *testing.T) {
	ctx := context.Background()
	lib, err := library.Open(ctx, repoLibrary)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := lib.Fetch(ctx, "dungeon-tiles")
	if err != nil {
		t.Fatal(err)
	}
	dir := newProject(t, minimalProject)
	c, err := library.Install(dir, pack, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.NoOp() || len(c.Remove) != 0 || len(c.Write) != 3 {
		t.Fatalf("the change writes %d and removes %v", len(c.Write), c.Remove)
	}

	for _, name := range []string{"pack.yaml", "tiles.json", "tiles.png"} {
		got, err := os.ReadFile(filepath.Join(dir, "packs", "dungeon-tiles", name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(repoLibrary, "packs", "dungeon-tiles", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs from the pack's", name)
		}
	}

	f := readProjectFile(t, dir)
	if f.Autoload == nil || !reflect.DeepEqual(f.Autoload.Resources, []project.Entry{{Name: "dungeon-tiles", Path: "packs/dungeon-tiles/tiles.json"}}) {
		t.Fatalf("autoload = %+v, want the atlas as resource dungeon-tiles", f.Autoload)
	}
	entry, _ := lib.Index.Find("dungeon-tiles")
	want := []project.Pack{{
		Name:        "dungeon-tiles",
		Kind:        project.PackAsset,
		PackVersion: "1.0.0",
		SHA256:      entry.SHA256,
		Dir:         "packs/dungeon-tiles",
		Files:       []string{"pack.yaml", "tiles.json", "tiles.png"},
		Resources:   []string{"dungeon-tiles"},
	}}
	if !reflect.DeepEqual(f.Packs, want) {
		t.Fatalf("packs = %+v, want %+v", f.Packs, want)
	}

	if _, err := project.Check(os.DirFS(dir)); err != nil {
		t.Fatalf("project.Check after the install: %v", err)
	}
	report, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || report.Atlases != 1 {
		t.Fatalf("assetcheck found %d atlases and %v", report.Atlases, report.Problems)
	}

	installed, _ := os.ReadFile(filepath.Join(dir, project.FileName))
	again, err := library.Install(dir, pack, "")
	if err != nil || !again.NoOp() {
		t.Fatalf("installing the same pack again = %+v, %v; want no change", again, err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, project.FileName)); !bytes.Equal(after, installed) {
		t.Fatal("installing the same pack again rewrote game.yaml")
	}

	if _, err := library.Remove(dir, "dungeon-tiles"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, project.FileName)); string(got) != minimalProject {
		t.Fatalf("game.yaml after the removal = %q, want %q", got, minimalProject)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs")); !os.IsNotExist(err) {
		t.Fatalf("the removal left the packs directory: %v", err)
	}
}

// TestRepoLibraryVerifies fetches every pack of the shared library, which checks each against its digest, so a pull request adding a pack with a wrong digest or a manifest that disagrees with its entry fails.
func TestRepoLibraryVerifies(t *testing.T) {
	ctx := context.Background()
	lib, err := library.Open(ctx, repoLibrary)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Remote() {
		t.Fatal("the repository's library has a pack outside it")
	}
	if len(lib.Index.Packs) == 0 {
		t.Fatal("the repository's library has no packs")
	}
	data, err := os.ReadFile(filepath.Join(repoLibrary, library.IndexFileName))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := library.EncodeIndex(lib.Index); err != nil || !bytes.Equal(out, data) {
		t.Fatalf("the index is not in canonical form (%v):\n%s", err, out)
	}
	for _, e := range lib.Index.Packs {
		p, err := lib.Fetch(ctx, e.Name)
		if err != nil {
			t.Error(err)
			continue
		}
		if p.Manifest.Description != e.Description || p.Manifest.License != e.License {
			t.Errorf("%s: the index's description and license differ from the manifest's", e.Name)
		}
		if out, err := library.EncodeManifest(p.Manifest); err != nil || !bytes.Equal(out, p.Files[library.ManifestFileName]) {
			t.Errorf("%s: pack.yaml is not in canonical form (%v):\n%s", e.Name, err, out)
		}
	}
}

// TestDigestIsSha256sumOfTheListing checks Digest against the listing sha256sum prints, written out by hand.
func TestDigestIsSha256sumOfTheListing(t *testing.T) {
	files := map[string][]byte{"b/c.txt": []byte("c\n"), "a.txt": []byte("a\n"), library.ManifestFileName: []byte("version: 1\n")}
	sum := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	listing := sum("a\n") + "  a.txt\n" + sum("c\n") + "  b/c.txt\n" + sum("version: 1\n") + "  pack.yaml\n"
	if got, want := library.Digest(files), sum(listing); got != want {
		t.Fatalf("Digest = %s, want %s", got, want)
	}
	files["a.txt"] = []byte("A\n")
	if library.Digest(files) == sum(listing) {
		t.Fatal("Digest did not change with a file's content")
	}
}

// testPack is a pack for the tests below: its manifest and its files other than pack.yaml.
type testPack struct {
	manifest string
	files    map[string]string
}

// newLibrary returns a library in memory holding packs, each under packs/<name> with an index entry whose digest is the pack's.
func newLibrary(t *testing.T, packs ...testPack) (*library.Library, fstest.MapFS) {
	t.Helper()
	fsys := fstest.MapFS{}
	idx := &library.Index{Version: library.IndexVersion}
	for _, tp := range packs {
		m, err := library.ParseManifest([]byte(tp.manifest))
		if err != nil {
			t.Fatal(err)
		}
		dir := "packs/" + m.Name
		fsys[dir+"/"+library.ManifestFileName] = &fstest.MapFile{Data: []byte(tp.manifest)}
		for name, content := range tp.files {
			fsys[dir+"/"+name] = &fstest.MapFile{Data: []byte(content)}
		}
		p, err := library.ReadPack(fsys, dir)
		if err != nil {
			t.Fatal(err)
		}
		idx.Packs = append(idx.Packs, library.Entry{Name: m.Name, Kind: m.Kind, PackVersion: m.PackVersion, Description: m.Description, License: m.License, Source: dir, SHA256: p.Digest()})
	}
	data, err := library.EncodeIndex(idx)
	if err != nil {
		t.Fatal(err)
	}
	fsys[library.IndexFileName] = &fstest.MapFile{Data: data}
	lib, err := library.OpenFS(fsys, library.IndexFileName)
	if err != nil {
		t.Fatal(err)
	}
	return lib, fsys
}

// fetch fetches the pack called name from lib, failing the test on an error.
func fetch(t *testing.T, lib *library.Library, name string) *library.Pack {
	t.Helper()
	p, err := lib.Fetch(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var (
	roomsV1 = testPack{
		manifest: `version: 1
name: rooms
kind: template
packVersion: 1.0.0
description: Starter rooms.
license: MIT
into: scenes/rooms
files: [hall.scene.yaml, notes/old.txt, shared.txt]
autoload:
  scenes:
    - {name: hall, path: hall.scene.yaml}
  resources:
    - {name: room-notes, path: notes/old.txt}
`,
		files: map[string]string{"hall.scene.yaml": "version: 1\nname: Hall\n", "notes/old.txt": "old\n", "shared.txt": "shared\n"},
	}
	roomsV2 = testPack{
		manifest: `version: 1
name: rooms
kind: template
packVersion: 2.0.0
description: Starter rooms.
license: MIT
into: scenes/rooms
files: [cellar.scene.yaml, hall.scene.yaml, shared.txt]
autoload:
  scenes:
    - {name: cellar, path: cellar.scene.yaml}
    - {name: hall, path: hall.scene.yaml}
`,
		files: map[string]string{"cellar.scene.yaml": "version: 1\nname: Cellar\n", "hall.scene.yaml": "version: 1\nname: Great Hall\n", "shared.txt": "shared\n"},
	}
)

// TestInstallUpdatesAnInstalledPack installs a newer release over an older one: files the new release lacks go, changed files are replaced, unchanged ones are left alone, and its autoload entries keep their places in game.yaml.
func TestInstallUpdatesAnInstalledPack(t *testing.T) {
	lib1, _ := newLibrary(t, roomsV1)
	lib2, _ := newLibrary(t, roomsV2)
	dir := newProject(t, minimalProject+"autoload:\n  mainScene: title\n  scenes:\n    - name: title\n      path: title.yaml\n")
	if err := os.WriteFile(filepath.Join(dir, "title.yaml"), []byte("version: 1\nname: Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Install(dir, fetch(t, lib1, "rooms"), ""); err != nil {
		t.Fatal(err)
	}
	f := readProjectFile(t, dir)
	if got := f.Autoload.Scenes; !reflect.DeepEqual(got, []project.Entry{{Name: "title", Path: "title.yaml"}, {Name: "hall", Path: "scenes/rooms/hall.scene.yaml"}}) {
		t.Fatalf("scenes after v1 = %+v", got)
	}
	// The player makes hall the main scene, so it must survive the update.
	f.Autoload.MainScene = "hall"
	data, err := project.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := library.Install(dir, fetch(t, lib2, "rooms"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Remove, []string{"scenes/rooms/notes/old.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Remove = %v, want %v", got, want)
	}
	var written []string
	for name := range c.Write {
		written = append(written, name)
	}
	if len(written) != 3 || c.Write["scenes/rooms/shared.txt"] != nil {
		t.Fatalf("Write = %v, want the manifest, cellar and hall but not the unchanged shared.txt", written)
	}
	if _, err := os.Stat(filepath.Join(dir, "scenes", "rooms", "notes")); !os.IsNotExist(err) {
		t.Fatalf("the emptied notes directory is still there: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "scenes", "rooms", "hall.scene.yaml")); string(got) != "version: 1\nname: Great Hall\n" {
		t.Fatalf("hall.scene.yaml = %q, want the new release's", got)
	}
	f = readProjectFile(t, dir)
	wantScenes := []project.Entry{{Name: "title", Path: "title.yaml"}, {Name: "hall", Path: "scenes/rooms/hall.scene.yaml"}, {Name: "cellar", Path: "scenes/rooms/cellar.scene.yaml"}}
	if !reflect.DeepEqual(f.Autoload.Scenes, wantScenes) || f.Autoload.MainScene != "hall" || len(f.Autoload.Resources) != 0 {
		t.Fatalf("autoload after v2 = %+v", f.Autoload)
	}
	rec := f.FindPack("rooms")
	if rec == nil || rec.PackVersion != "2.0.0" || !reflect.DeepEqual(rec.Scenes, []string{"cellar", "hall"}) || rec.Resources != nil {
		t.Fatalf("record after v2 = %+v", rec)
	}
	if _, err := project.Check(os.DirFS(dir)); err != nil {
		t.Fatalf("project.Check after the update: %v", err)
	}

	// Removing it unsets the main scene it provided and keeps the project's own.
	if _, err := library.Remove(dir, "rooms"); err != nil {
		t.Fatal(err)
	}
	f = readProjectFile(t, dir)
	if f.Packs != nil || f.Autoload.MainScene != "" || !reflect.DeepEqual(f.Autoload.Scenes, []project.Entry{{Name: "title", Path: "title.yaml"}}) {
		t.Fatalf("after the removal: autoload %+v, packs %+v", f.Autoload, f.Packs)
	}
	if _, err := os.Stat(filepath.Join(dir, "scenes", "rooms")); !os.IsNotExist(err) {
		t.Fatalf("the removal left the pack's emptied directory: %v", err)
	}
	// scenes holds the pack's directory and may be the project's own, so it stays.
	if info, err := os.Stat(filepath.Join(dir, "scenes")); err != nil || !info.IsDir() {
		t.Fatalf("the removal deleted the directory above the pack's: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "title.yaml")); err != nil {
		t.Fatalf("the removal deleted the project's own scene: %v", err)
	}
}

const toolsPack = `version: 1
name: level-tools
kind: plugin
packVersion: 0.1.0
description: Health and waypoint editors.
license: Apache-2.0
files: [waypoints.yaml]
plugins: [health, waypoints]
`

// TestPluginPackTurnsPluginsOn installs a plugin pack into a project that already turns one of its plugins on: only the other is recorded as the pack's, and removing the pack keeps the project's own.
func TestPluginPackTurnsPluginsOn(t *testing.T) {
	lib, _ := newLibrary(t, testPack{manifest: toolsPack, files: map[string]string{"waypoints.yaml": "version: 1\n"}})
	dir := newProject(t, minimalProject+"editor:\n  plugins: [health]\n")
	if _, err := library.Install(dir, fetch(t, lib, "level-tools"), "tools/level"); err != nil {
		t.Fatal(err)
	}
	f := readProjectFile(t, dir)
	if !reflect.DeepEqual(f.Editor.Plugins, []string{"health", "waypoints"}) {
		t.Fatalf("plugins = %v", f.Editor.Plugins)
	}
	if rec := f.FindPack("level-tools"); rec == nil || rec.Dir != "tools/level" || !reflect.DeepEqual(rec.Plugins, []string{"waypoints"}) {
		t.Fatalf("record = %+v", rec)
	}
	if _, err := os.Stat(filepath.Join(dir, "tools", "level", "waypoints.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Remove(dir, "level-tools"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, project.FileName)); string(got) != minimalProject+"editor:\n  plugins: [health]\n" {
		t.Fatalf("game.yaml after the removal = %q", got)
	}
}

// TestPlanInstallRefusesConflicts checks the conflicts PlanInstall refuses before anything is written.
func TestPlanInstallRefusesConflicts(t *testing.T) {
	lib, _ := newLibrary(t, roomsV1, testPack{manifest: toolsPack, files: map[string]string{"waypoints.yaml": "version: 1\n"}})
	rooms := fetch(t, lib, "rooms")
	base := project.MustParse([]byte(minimalProject))

	// A file the project has with other content is refused, and one with the same content is taken as the pack's.
	if _, err := library.PlanInstall(fstest.MapFS{"scenes/rooms/shared.txt": {Data: []byte("mine\n")}}, base, rooms, ""); err == nil || !strings.Contains(err.Error(), "scenes/rooms/shared.txt") {
		t.Fatalf("an existing file: %v", err)
	}
	c, err := library.PlanInstall(fstest.MapFS{"scenes/rooms/shared.txt": {Data: []byte("shared\n")}}, base, rooms, "")
	if err != nil || c.Write["scenes/rooms/shared.txt"] != nil || c.Project.FindPack("rooms") == nil {
		t.Fatalf("an identical file: %+v, %v", c, err)
	}

	// An autoload entry of the same name the project has is refused.
	clash := project.MustParse([]byte(minimalProject + "autoload:\n  scenes:\n    - name: hall\n      path: hall.yaml\n"))
	if _, err := library.PlanInstall(fstest.MapFS{}, clash, rooms, ""); err == nil || !strings.Contains(err.Error(), `"hall"`) {
		t.Fatalf("an autoload clash: %v", err)
	}

	// A file another pack wrote is refused.
	installed, err := library.PlanInstall(fstest.MapFS{}, base, rooms, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.PlanInstall(fstest.MapFS{}, installed.Project, fetch(t, lib, "level-tools"), "scenes/rooms"); err == nil || !strings.Contains(err.Error(), "pack rooms") {
		t.Fatalf("another pack's file: %v", err)
	}

	// A directory outside the project, or a hidden one, is refused.
	for _, dir := range []string{"../elsewhere", ".github/workflows", "assets/.hidden", "aux"} {
		if _, err := library.PlanInstall(fstest.MapFS{}, base, rooms, dir); err == nil {
			t.Fatalf("install directory %q was accepted", dir)
		}
	}

	// A file another pack wrote is refused when the names differ only in case, as they name one file on macOS and Windows.
	upper := testPack{manifest: strings.Replace(toolsPack, "files: [waypoints.yaml]", "files: [SHARED.TXT]", 1), files: map[string]string{"SHARED.TXT": "x\n"}}
	libUpper, _ := newLibrary(t, upper)
	if _, err := library.PlanInstall(fstest.MapFS{}, installed.Project, fetch(t, libUpper, "level-tools"), "Scenes/Rooms"); err == nil || !strings.Contains(err.Error(), "pack rooms") {
		t.Fatalf("another pack's file in another case: %v", err)
	}
	if _, err := library.PlanRemove(fstest.MapFS{}, base, "rooms"); err == nil {
		t.Fatal("removing a pack that is not installed succeeded")
	}
	if base.Packs != nil {
		t.Fatal("planning changed the project file it was given")
	}
}

// TestApplyFilesRestoresOnFailure makes a write fail partway and checks that the files written before it are restored.
func TestApplyFilesRestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// blocker is a file, so writing blocker/c.txt fails after a.txt and new/b.txt were written.
	if err := os.WriteFile(filepath.Join(dir, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &library.Change{
		Project: project.MustParse([]byte(minimalProject)),
		Write: map[string][]byte{
			"a.txt":         []byte("after\n"),
			"blocker/c.txt": []byte("c\n"),
			"a/new/b.txt":   []byte("b\n"),
		},
	}
	if err := c.Apply(dir); err == nil {
		t.Fatal("Apply succeeded through a file in the way")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "before\n" {
		t.Fatalf("a.txt = %q, want it restored", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Fatalf("the directories made for a/new/b.txt are still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, project.FileName)); !os.IsNotExist(err) {
		t.Fatalf("Apply wrote game.yaml after a failed file: %v", err)
	}
}

// TestFetchRefusesAPackThatDiffersFromItsEntry checks that a pack whose files or manifest no longer match its index entry is refused.
func TestFetchRefusesAPackThatDiffersFromItsEntry(t *testing.T) {
	lib, fsys := newLibrary(t, roomsV1)
	fsys["packs/rooms/shared.txt"] = &fstest.MapFile{Data: []byte("tampered\n")}
	if _, err := lib.Fetch(context.Background(), "rooms"); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("a tampered file: %v", err)
	}
	fsys["packs/rooms/shared.txt"] = &fstest.MapFile{Data: []byte("shared\n")}
	lib.Index.Packs[0].PackVersion = "9.0.0"
	if _, err := lib.Fetch(context.Background(), "rooms"); err == nil || !strings.Contains(err.Error(), "version 1.0.0") {
		t.Fatalf("a version that differs: %v", err)
	}
	if _, err := lib.Fetch(context.Background(), "missing"); err == nil {
		t.Fatal("fetching a pack the index has not succeeded")
	}
}

// TestOpenOverHTTP serves a library from a test server and fetches a pack from it, file by file.
func TestOpenOverHTTP(t *testing.T) {
	_, fsys := newLibrary(t, roomsV1)
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		http.FileServerFS(fsys).ServeHTTP(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	lib, err := library.Open(ctx, srv.URL+"/"+library.IndexFileName)
	if err != nil {
		t.Fatal(err)
	}
	if !lib.Remote() {
		t.Fatal("a library read over http is not remote")
	}
	p, err := lib.Fetch(ctx, "rooms")
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Files["notes/old.txt"]) != "old\n" {
		t.Fatalf("notes/old.txt = %q", p.Files["notes/old.txt"])
	}
	want := []string{"/index.yaml", "/packs/rooms/pack.yaml", "/packs/rooms/hall.scene.yaml", "/packs/rooms/notes/old.txt", "/packs/rooms/shared.txt"}
	mu.Lock()
	got := slices.Clone(requests)
	mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if _, err := library.Open(ctx, srv.URL+"/missing.yaml"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a missing index: %v", err)
	}
}

// TestOpenRefusesPlainHTTPToAnotherHost checks that a library must be read over https unless it is on this machine.
func TestOpenRefusesPlainHTTPToAnotherHost(t *testing.T) {
	for _, loc := range []string{"http://example.com/index.yaml", "https:///index.yaml"} {
		if _, err := library.Open(context.Background(), loc); err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("Open(%q) = %v, want an error asking for https", loc, err)
		}
	}
}

func TestSearch(t *testing.T) {
	idx, err := library.ParseIndex([]byte(`version: 1
packs:
  - {name: ui-kit, kind: asset, packVersion: '1', description: Buttons and panels., tags: [ui], license: MIT, source: a, sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08}
  - {name: dungeon-tiles, kind: asset, packVersion: '1', description: Stone tiles., tags: [2d, tiles], license: CC0-1.0, source: b, sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08}
  - {name: path-tools, kind: plugin, packVersion: '1', description: Draw paths for tiles and actors., license: MIT, source: c, sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08}
`))
	if err != nil {
		t.Fatal(err)
	}
	names := func(es []library.Entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Name)
		}
		return out
	}
	for _, c := range []struct {
		kind, query string
		want        []string
	}{
		{"", "", []string{"dungeon-tiles", "path-tools", "ui-kit"}},
		{"", "TILES", []string{"dungeon-tiles", "path-tools"}},
		{project.PackAsset, "tiles", []string{"dungeon-tiles"}},
		{"", "tiles actors", []string{"path-tools"}},
		{"", "ui", []string{"ui-kit"}},
		{project.PackTemplate, "", nil},
	} {
		if got := names(idx.Search(c.kind, c.query)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Search(%q, %q) = %v, want %v", c.kind, c.query, got, c.want)
		}
	}
}

func TestParseIndexRefusesInvalidIndexes(t *testing.T) {
	const sum = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	entry := func(fields string) string {
		return "version: 1\npacks:\n  - {name: tiles, kind: asset, packVersion: '1', description: Tiles., license: MIT, source: packs/tiles, sha256: " + sum + fields + "}\n"
	}
	cases := map[string]string{
		"empty":            "",
		"version":          "version: 2\n",
		"unknown field":    "version: 1\nowner: me\n",
		"unknown entry":    entry(", url: x"),
		"trailing":         entry("") + "---\nversion: 1\n",
		"name":             strings.Replace(entry(""), "name: tiles", "name: Tiles", 1),
		"kind":             strings.Replace(entry(""), "kind: asset", "kind: music", 1),
		"version spaces":   strings.Replace(entry(""), "packVersion: '1'", "packVersion: '1 0'", 1),
		"no description":   strings.Replace(entry(""), "description: Tiles.", "description: ''", 1),
		"no license":       strings.Replace(entry(""), "license: MIT", "license: ''", 1),
		"license":          strings.Replace(entry(""), "license: MIT", "license: 'MIT;rm'", 1),
		"tag":              entry(", tags: [Tiles]"),
		"duplicate tag":    entry(", tags: [a, a]"),
		"escaping source":  strings.Replace(entry(""), "source: packs/tiles", "source: ../tiles", 1),
		"http source":      strings.Replace(entry(""), "source: packs/tiles", "source: 'http://example.com/tiles'", 1),
		"query source":     strings.Replace(entry(""), "source: packs/tiles", "source: 'https://example.com/tiles?x=1'", 1),
		"sha256":           strings.Replace(entry(""), sum, strings.ToUpper(sum), 1),
		"duplicate pack":   entry("") + "  - {name: tiles, kind: asset, packVersion: '1', description: Tiles., license: MIT, source: packs/other, sha256: " + sum + "}\n",
		"no packs version": "packs: []\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if idx, err := library.ParseIndex([]byte(src)); err == nil {
				t.Fatalf("ParseIndex accepted %q as %+v", src, idx)
			}
		})
	}
	if _, err := library.ParseIndex([]byte(entry(", tags: [2d]"))); err != nil {
		t.Fatalf("a valid index: %v", err)
	}
	for _, source := range []string{"https://example.com/packs/tiles", "http://127.0.0.1:8080/packs/tiles", "http://localhost/packs/tiles"} {
		if _, err := library.ParseIndex([]byte(strings.Replace(entry(""), "source: packs/tiles", "source: '"+source+"'", 1))); err != nil {
			t.Fatalf("source %s: %v", source, err)
		}
	}
}

func TestParseManifestRefusesInvalidManifests(t *testing.T) {
	const head = "version: 1\nname: tiles\nkind: asset\npackVersion: 1.0.0\ndescription: Tiles.\nlicense: CC0-1.0\n"
	cases := map[string]string{
		"version":           strings.Replace(head, "version: 1", "version: 3", 1),
		"unknown field":     head + "homepage: x\n",
		"into":              head + "into: ../x\n",
		"file":              head + "files: [/etc/passwd]\n",
		"backslash":         head + "files: ['a\\b']\n",
		"manifest listed":   head + "files: [pack.yaml]\n",
		"duplicate file":    head + "files: [a, a]\n",
		"file in a file":    head + "files: [a, a/b]\n",
		"hidden into":       head + "into: .github/workflows\n",
		"hidden into elem":  head + "into: assets/.cache\n",
		"hidden file":       head + "files: [.github/workflows/ci.yml]\n",
		"go source":         head + "files: [init.go]\n",
		"go source case":    head + "files: [sub/INIT.GO]\n",
		"assembly":          head + "files: [a.s]\n",
		"cgo":               head + "files: [a.c]\n",
		"go.mod":            head + "files: [sub/go.mod]\n",
		"go.sum":            head + "files: [go.sum]\n",
		"case clash":        head + "files: [Tiles.png, tiles.png]\n",
		"case dir clash":    head + "files: [A, a/b]\n",
		"manifest case":     head + "files: [Pack.yaml]\n",
		"colon":             head + "files: ['a:b.png']\n",
		"reserved":          head + "files: [con.txt]\n",
		"reserved com":      head + "files: [sub/COM1]\n",
		"trailing dot":      head + "files: ['a.']\n",
		"trailing space":    head + "files: ['a ']\n",
		"newline":           head + "files: [\"a\\nb\"]\n",
		"autoload missing":  head + "files: [a.json]\nautoload: {resources: [{name: r, path: b.json}]}\n",
		"autoload name":     head + "files: [a.json]\nautoload: {resources: [{name: 'r r', path: a.json}]}\n",
		"autoload twice":    head + "files: [a.json]\nautoload: {resources: [{name: r, path: a.json}, {name: r, path: a.json}]}\n",
		"scene extension":   head + "files: [a.json]\nautoload: {scenes: [{name: s, path: a.json}]}\n",
		"asset plugins":     head + "plugins: [health]\n",
		"plugin no plugins": strings.Replace(head, "kind: asset", "kind: plugin", 1),
		"plugin twice":      strings.Replace(head, "kind: asset", "kind: plugin", 1) + "plugins: [a, a]\n",
		"plugin name":       strings.Replace(head, "kind: asset", "kind: plugin", 1) + "plugins: ['a b']\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if m, err := library.ParseManifest([]byte(src)); err == nil {
				t.Fatalf("ParseManifest accepted %q as %+v", src, m)
			}
		})
	}
	m, err := library.ParseManifest([]byte(head + "files: [a.json, sub/b.png]\nautoload:\n  resources:\n    - name: a\n      path: a.json\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := library.EncodeManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := head + "files:\n  - a.json\n  - sub/b.png\nautoload:\n  resources:\n    - name: a\n      path: a.json\n"; string(out) != want {
		t.Fatalf("EncodeManifest = %q, want %q", out, want)
	}
}

func TestReadPackRefusesAMissingFile(t *testing.T) {
	fsys := fstest.MapFS{"p/pack.yaml": {Data: []byte("version: 1\nname: p\nkind: asset\npackVersion: '1'\ndescription: P.\nlicense: MIT\nfiles: [gone.png]\n")}}
	if _, err := library.ReadPack(fsys, "p"); err == nil || !strings.Contains(err.Error(), "gone.png") {
		t.Fatalf("ReadPack = %v, want an error naming gone.png", err)
	}
	if _, err := library.ReadPack(fstest.MapFS{}, "p"); err == nil {
		t.Fatal("ReadPack of a directory without pack.yaml succeeded")
	}
}

func TestKindsMatchTheProjectFile(t *testing.T) {
	for _, kind := range project.PackKinds {
		m := fmt.Sprintf("version: 1\nname: p\nkind: %s\npackVersion: '1'\ndescription: P.\nlicense: MIT\n", kind)
		if kind == project.PackPlugin {
			m += "plugins: [x]\n"
		}
		if _, err := library.ParseManifest([]byte(m)); err != nil {
			t.Errorf("kind %s: %v", kind, err)
		}
	}
}

// TestUpdateKeepsTheInstallDirectory installs a pack into a directory of the installer's choosing and updates it without naming one: the new release goes where the old one is, not where its manifest says.
func TestUpdateKeepsTheInstallDirectory(t *testing.T) {
	lib1, _ := newLibrary(t, roomsV1)
	lib2, _ := newLibrary(t, roomsV2)
	dir := newProject(t, minimalProject)
	if _, err := library.Install(dir, fetch(t, lib1, "rooms"), "levels/rooms"); err != nil {
		t.Fatal(err)
	}
	c, err := library.Install(dir, fetch(t, lib2, "rooms"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Pack.Dir != "levels/rooms" {
		t.Fatalf("the update went to %s, want levels/rooms", c.Pack.Dir)
	}
	for _, name := range []string{"cellar.scene.yaml", "hall.scene.yaml", "shared.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "levels", "rooms", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "scenes")); !os.IsNotExist(err) {
		t.Fatalf("the update wrote into the manifest's directory: %v", err)
	}
	if f := readProjectFile(t, dir); f.FindPack("rooms").Dir != "levels/rooms" || f.Autoload.Scenes[0].Path != "levels/rooms/hall.scene.yaml" {
		t.Fatalf("after the update: record %+v, scenes %+v", f.FindPack("rooms"), f.Autoload.Scenes)
	}
}

// layoutPack is a release of a pack called layout holding files, each with content of its own name.
func layoutPack(version string, files ...string) testPack {
	content := map[string]string{}
	for _, f := range files {
		content[f] = f + "\n"
	}
	return testPack{
		manifest: "version: 1\nname: layout\nkind: asset\npackVersion: " + version + "\ndescription: Layout.\nlicense: MIT\nfiles: [" + strings.Join(files, ", ") + "]\n",
		files:    content,
	}
}

// TestUpdateSwapsAFileAndADirectory updates a pack whose new release has a directory where the old one had a file, and back again.
func TestUpdateSwapsAFileAndADirectory(t *testing.T) {
	v1, _ := newLibrary(t, layoutPack("1", "notes", "keep.txt"))
	v2, _ := newLibrary(t, layoutPack("2", "notes/a.txt", "notes/b.txt", "keep.txt"))
	dir := newProject(t, minimalProject)
	notes := filepath.Join(dir, "packs", "layout", "notes")
	if _, err := library.Install(dir, fetch(t, v1, "layout"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Install(dir, fetch(t, v2, "layout"), ""); err != nil {
		t.Fatalf("a file turned into a directory: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(notes, "a.txt")); err != nil || string(got) != "notes/a.txt\n" {
		t.Fatalf("notes/a.txt = %q, %v", got, err)
	}
	if _, err := library.Install(dir, fetch(t, v1, "layout"), ""); err != nil {
		t.Fatalf("a directory turned into a file: %v", err)
	}
	if got, err := os.ReadFile(notes); err != nil || string(got) != "notes\n" {
		t.Fatalf("notes = %q, %v", got, err)
	}
	if _, err := project.Check(os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}

	// A directory that also holds a file of the project's own is not the pack's to replace.
	if _, err := library.Install(dir, fetch(t, v2, "layout"), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, "mine.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Install(dir, fetch(t, v1, "layout"), ""); err == nil {
		t.Fatal("an update replaced a directory holding a file of the project's own")
	}
	if got, _ := os.ReadFile(filepath.Join(notes, "mine.txt")); string(got) != "mine\n" {
		t.Fatalf("the refused update changed mine.txt to %q", got)
	}
}

// TestRemoveKeepsDirectoriesAboveThePack installs a pack into a directory inside one the project already had empty, and checks that removing it deletes the pack's directory but not that one.
func TestRemoveKeepsDirectoriesAboveThePack(t *testing.T) {
	lib, _ := newLibrary(t, roomsV1)
	dir := newProject(t, minimalProject)
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Install(dir, fetch(t, lib, "rooms"), "assets/rooms"); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Remove(dir, "rooms"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "rooms")); !os.IsNotExist(err) {
		t.Fatalf("the pack's directory is still there: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "assets")); err != nil || !info.IsDir() {
		t.Fatalf("the removal deleted the project's own empty assets directory: %v", err)
	}
}

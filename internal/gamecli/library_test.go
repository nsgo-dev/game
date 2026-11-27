package gamecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/library"
)

// repoLibrary is the shared library in this repository's library directory.
var repoLibrary = filepath.Join("..", "..", "library")

const libraryProject = "version: 1\nname: Cave Run\nid: dev.example.caverun\nappVersion: 0.1.0\nwindow:\n  width: 320\n  height: 180\n"

func TestLibraryInstallsListsAndRemovesAPack(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{project.FileName: libraryProject})
	game := func(args ...string) string {
		t.Helper()
		a, stdout, stderr := newTestApp(t)
		args = append([]string{"library", "-index", repoLibrary, "-project", dir}, args...)
		if code := run(args, a); code != 0 {
			t.Fatalf("%q exit %d (stderr: %s)", args, code, stderr)
		}
		return stdout.String()
	}

	if got, want := game("list", "dungeon"), "PACK           KIND   VERSION  INSTALLED  DESCRIPTION\ndungeon-tiles  asset  1.0.0    -          A 16x16 stone floor tile and brick wall tile with a TexturePacker atlas.\n"; got != want {
		t.Fatalf("list = %q, want %q", got, want)
	}
	if got, want := game("install", "dungeon-tiles"), "installed dungeon-tiles 1.0.0 into packs/dungeon-tiles (3 files)\n"; got != want {
		t.Fatalf("install = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs", "dungeon-tiles", "tiles.json")); err != nil {
		t.Fatal(err)
	}
	if got := game("-kind", "asset", "list"); !strings.Contains(got, "1.0.0    1.0.0") {
		t.Fatalf("list after the install does not show it installed: %q", got)
	}
	if got, want := game("install", "dungeon-tiles"), "dungeon-tiles 1.0.0 is already installed in packs/dungeon-tiles\n"; got != want {
		t.Fatalf("installing again = %q, want %q", got, want)
	}
	if got, want := game("remove", "dungeon-tiles"), "removed dungeon-tiles 1.0.0 from packs/dungeon-tiles (3 files)\n"; got != want {
		t.Fatalf("remove = %q, want %q", got, want)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, project.FileName)); string(data) != libraryProject {
		t.Fatalf("game.yaml after the removal = %q", data)
	}
}

func TestLibraryInstallIntoAnotherDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{project.FileName: libraryProject})
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"library", "-index", repoLibrary, "-project", dir, "-dir", "assets/tiles", "install", "dungeon-tiles"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "into assets/tiles") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "tiles", "tiles.png")); err != nil {
		t.Fatal(err)
	}

	// Installing it again without -dir keeps it where it is rather than moving it to the manifest's directory.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"library", "-index", repoLibrary, "-project", dir, "install", "dungeon-tiles"}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if got, want := stdout.String(), "dungeon-tiles 1.0.0 is already installed in assets/tiles\n"; got != want {
		t.Fatalf("installing again = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs")); !os.IsNotExist(err) {
		t.Fatalf("installing again wrote the manifest's directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "tiles", "tiles.png")); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryDigestMatchesTheIndex(t *testing.T) {
	lib, err := library.OpenFS(os.DirFS(repoLibrary), library.IndexFileName)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := lib.Index.Find("dungeon-tiles")
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"library", "digest", filepath.Join(repoLibrary, "packs", "dungeon-tiles")}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if got := stdout.String(); got != entry.SHA256+"\n" {
		t.Fatalf("digest = %q, want %q", got, entry.SHA256)
	}
}

func TestLibraryRefusesWrongCalls(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"library"}, 2, "expected one of list, install, remove, digest"},
		{[]string{"library", "fetch"}, 2, `unknown verb "fetch"`},
		{[]string{"library", "install"}, 2, "needs the name of a pack"},
		{[]string{"library", "remove"}, 2, "needs the name of an installed pack"},
		{[]string{"library", "digest"}, 2, "one pack directory"},
		{[]string{"library", "-kind", "asset", "install", "x"}, 2, "-kind is for list"},
		{[]string{"library", "-kind", "music", "list"}, 2, `-kind "music"`},
		{[]string{"library", "-dir", "x", "list"}, 2, "-dir is for install"},
		{[]string{"library", "-dir", "x", "install", "a", "b"}, 2, "one pack, not 2"},
		{[]string{"library", "-index", repoLibrary, "-project", dir, "install", "dungeon-tiles"}, 1, "no game.yaml"},
		{[]string{"library", "-index", repoLibrary, "-project", dir, "list", "nothing-like-this"}, 0, "no packs match"},
		{[]string{"library", "-project", dir, "remove", "dungeon-tiles"}, 1, "game.yaml"},
	} {
		a, _, stderr := newTestApp(t)
		if code := run(c.args, a); code != c.code || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%q exit %d, stderr %q; want exit %d and %q", c.args, code, stderr, c.code, c.want)
		}
	}
}

func TestEditorShowsTheLibrary(t *testing.T) {
	a, _, stderr := newTestApp(t)
	var opened *editor.Editor
	a.launch = func(ed *editor.Editor) { opened = ed }
	if code := run([]string{"editor", "-library", repoLibrary}, a); code != 0 {
		t.Fatalf("exit %d (stderr: %s)", code, stderr)
	}
	if opened == nil || opened.Library == nil {
		t.Fatalf("launched %+v, want an editor with the library", opened)
	}
	if _, ok := opened.Library.Index.Find("dungeon-tiles"); !ok {
		t.Fatal("the editor's library lacks the sample pack")
	}
	a, _, stderr = newTestApp(t)
	if code := run([]string{"editor", "-library", filepath.Join(t.TempDir(), "none")}, a); code != 1 {
		t.Fatalf("a missing library: exit %d (stderr: %s)", code, stderr)
	}
}

package editor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/library"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// repoLibrary is the shared library in this repository's library directory.
var repoLibrary = filepath.Join("..", "..", "..", "library")

// openLibrary opens the library at location, failing the test on an error.
func openLibrary(t *testing.T, location string) *library.Library {
	t.Helper()
	lib, err := library.Open(context.Background(), location)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// TestLibraryPanelInstallsAndRemovesAPack installs the shared library's sample asset pack from the library panel: its files land in the project, game.yaml records it and autoloads its atlas, and the panel shows it installed until Remove takes it out again.
func TestLibraryPanelInstallsAndRemovesAPack(t *testing.T) {
	dir := writeBareProject(t, "Bare")
	ed := newTestEditor(t)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.Library = openLibrary(t, repoLibrary)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if got := strings.TrimSpace(allText(app.Find("id:library-install-dungeon-tiles"))); got != "Install" {
		t.Fatalf("the install button reads %q", got)
	}
	if app.Enabled("id:library-remove-dungeon-tiles") {
		t.Fatal("Remove is enabled for a pack the project has not installed")
	}
	app.Press("id:library-install-dungeon-tiles")
	app.Settle()

	if !strings.Contains(ed.Status, "Installed pack dungeon-tiles 1.0.0 into packs/dungeon-tiles") {
		t.Fatalf("status = %q", ed.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs", "dungeon-tiles", "tiles.png")); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	f := project.MustParse(saved)
	if f.FindPack("dungeon-tiles") == nil || !reflect.DeepEqual(f.Autoload.Resources, []project.Entry{{Name: "dungeon-tiles", Path: "packs/dungeon-tiles/tiles.json"}}) {
		t.Fatalf("saved game.yaml:\n%s", saved)
	}
	if ed.ProjectDirty() || ed.History.CanUndo() {
		t.Fatalf("after the install the project is dirty (%v) or has undo steps (%v)", ed.ProjectDirty(), ed.History.CanUndo())
	}
	if got := strings.TrimSpace(allText(app.Find("id:library-install-dungeon-tiles"))); got != "Installed" || app.Enabled("id:library-install-dungeon-tiles") {
		t.Fatalf("after the install the button reads %q, enabled %v", got, app.Enabled("id:library-install-dungeon-tiles"))
	}
	if got := allText(app.Find("id:library-version-dungeon-tiles")); !strings.Contains(got, "installed 1.0.0") {
		t.Fatalf("the pack's line reads %q", got)
	}

	app.Press("id:library-remove-dungeon-tiles")
	app.Settle()
	if ed.Project.Packs != nil || !strings.Contains(ed.Status, "Removed pack dungeon-tiles") {
		t.Fatalf("after Remove: packs %+v, status %q", ed.Project.Packs, ed.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs")); !os.IsNotExist(err) {
		t.Fatalf("Remove left the pack's directory: %v", err)
	}
	if got := strings.TrimSpace(allText(app.Find("id:library-install-dungeon-tiles"))); got != "Install" {
		t.Fatalf("after Remove the button reads %q", got)
	}
}

// writePluginLibrary writes a library holding a plugin pack that turns on the sample health plugin and an editor plugin this editor lacks, and returns its directory.
func writePluginLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packDir := filepath.Join(dir, "packs", "health-tools")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "version: 1\nname: health-tools\nkind: plugin\npackVersion: 0.2.0\ndescription: Health editing.\nlicense: MIT\nplugins: [health, paths]\n"
	if err := os.WriteFile(filepath.Join(packDir, library.ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := library.ReadPack(os.DirFS(packDir), ".")
	if err != nil {
		t.Fatal(err)
	}
	idx := &library.Index{Version: library.IndexVersion, Packs: []library.Entry{{
		Name: "health-tools", Kind: project.PackPlugin, PackVersion: "0.2.0", Description: "Health editing.", License: "MIT", Source: "packs/health-tools", SHA256: p.Digest(),
	}}}
	data, err := library.EncodeIndex(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, library.IndexFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestInstallPackTurnsItsPluginsOn installs a plugin pack: the registered plugin it names turns on, and the status bar names the one the editor lacks.
func TestInstallPackTurnsItsPluginsOn(t *testing.T) {
	ed := newPluginEditor(t, writePluginProject(t))
	lib := openLibrary(t, writePluginLibrary(t))
	p, err := lib.Fetch(context.Background(), "health-tools")
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.InstallPack(p); err != nil {
		t.Fatal(err)
	}
	if got := ed.ActivePlugins(); !slices.Equal(got, []string{"health"}) {
		t.Fatalf("ActivePlugins = %v, want health", got)
	}
	if !strings.Contains(ed.Status, "plugins not built into this editor: paths") {
		t.Fatalf("status = %q", ed.Status)
	}
	if err := ed.InstallPack(p); err != nil || !strings.Contains(ed.Status, "already installed") {
		t.Fatalf("installing it again = %v, status %q", err, ed.Status)
	}
	if err := ed.RemovePack("health-tools"); err != nil {
		t.Fatal(err)
	}
	if got := ed.ActivePlugins(); got != nil {
		t.Fatalf("after the removal ActivePlugins = %v", got)
	}
}

// TestInstallPackRefusesWhatItCannotSave checks that a pack is not installed without a project or over unsaved project edits, and that a refused install changes nothing.
func TestInstallPackRefusesWhatItCannotSave(t *testing.T) {
	p, err := openLibrary(t, repoLibrary).Fetch(context.Background(), "dungeon-tiles")
	if err != nil {
		t.Fatal(err)
	}
	ed := newTestEditor(t)
	if err := ed.InstallPack(p); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("without a project: %v", err)
	}
	dir := writeBareProject(t, "Bare")
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.EditProject("rename", func(f *project.File) { f.Name = "Renamed" }); err != nil {
		t.Fatal(err)
	}
	if err := ed.InstallPack(p); !errors.Is(err, editor.ErrProjectDirty) {
		t.Fatalf("over unsaved edits: %v", err)
	}
	if err := ed.RemovePack("dungeon-tiles"); !errors.Is(err, editor.ErrProjectDirty) {
		t.Fatalf("removing over unsaved edits: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packs")); !os.IsNotExist(err) {
		t.Fatalf("a refused install wrote files: %v", err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}
	if err := ed.RemovePack("dungeon-tiles"); err == nil {
		t.Fatal("removing a pack that is not installed succeeded")
	}
}

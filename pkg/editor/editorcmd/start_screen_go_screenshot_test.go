package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestStartScreenMissingGoScreenshots captures real native macOS screenshots of the Start Screen
// missing-Go notice (with Create disabled) and after Retry finds a compatible Go toolchain.
func TestStartScreenMissingGoScreenshots(t *testing.T) {
	parentDir := t.TempDir()

	ed, err := editor.NewDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ed.Close() }()

	if err := ed.SetLanguage(editor.LanguageEnglish); err != nil {
		t.Fatal(err)
	}
	ed.SetMinGoVersion("1.27.3")
	ed.SetProjectTemplates([]editor.ProjectTemplate{
		{Name: "starter", Title: "Starter", Summary: "An empty scene and a loop", Dimension: "2d", Default: true},
		{Name: "platformer2d", Title: "2D platformer", Summary: "Rooms, checkpoints, coyote time", Dimension: "2d"},
		{Name: "rpg2d", Title: "2D RPG", Summary: "Town, dialogue, quests, saves", Dimension: "2d"},
		{Name: "racing3d", Title: "3D racer", Summary: "Laps, ghosts, AI drivers", Dimension: "3d"},
	})
	ed.SetStartProjectParentDir(parentDir)
	ed.SetStartProjectName("dungeon-run")
	ed.SetStartScreenOpen(true)

	status := editor.GoToolchainStatus{Found: false, MinVersion: "1.27.3"}
	ed.GoToolchainProbe = func(minVersion string) editor.GoToolchainStatus {
		st := status
		st.MinVersion = minVersion
		return st
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800), nsgotest.Environment(core.Environment{
		OS:    core.OSMacOS,
		Idiom: core.IdiomDesktop,
		Dark:  true,
	}))
	app.Settle()

	app.Screenshot("start-screen-missing-go-dark")

	status = editor.GoToolchainStatus{Found: true, Binary: "/usr/local/go/bin/go", Version: "1.27.4"}
	app.Press("id:start-go-retry")
	app.Settle()

	app.Screenshot("start-screen-go-ready-dark")
}

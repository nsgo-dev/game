package editor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestChineseUIMacOSScreenshots renders the Start Screen and Settings › Project page
// with the editor language set to Simplified Chinese on the native macOS host (#1503).
func TestChineseUIMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "title.scene.yaml"), []byte("version: 1\nname: Title\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pf := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Dungeon Run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.4.2",
		Window: project.Window{
			Width:  640,
			Height: 360,
		},
		Autoload: &project.Autoload{
			Scenes: []project.Entry{
				{Name: "title", Path: "scenes/title.scene.yaml"},
			},
			MainScene: "title",
		},
	}
	data, err := project.Encode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Start screen in Simplified Chinese.
	startEd, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	startEd.SetLanguage(editor.LanguageChinese)
	startEd.SetAppearance(editor.AppearanceDark)
	startEd.SetStartScreenOpen(true)

	startApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, startEd)
	}, nsgotest.Size(1440, 900))
	startApp.Settle()
	startApp.Screenshot("chinese-start-screen")

	// 2. Settings › Project page in Simplified Chinese.
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetLanguage(editor.LanguageChinese)
	ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Post(func() { ed.OpenSettingsPage(editor.SettingsPageProject) })
	app.Settle()
	app.Screenshot("chinese-settings-project")
}

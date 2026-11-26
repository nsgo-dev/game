package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestSettingsProjectPagesMacOSScreenshots renders the four PROJECT · game.yaml
// Settings pages (Project, Input map, Audio buses, Export) on the real native macOS
// host (`env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestSettingsProjectPagesMacOSScreenshots ./editor/editorcmd`)
// for visual verification beside docs/design/editor/Settings.dc.html and Spec.dc.html.
func TestSettingsProjectPagesMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "title.scene.yaml"), []byte("version: 1\nname: Title\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "level_01.scene.yaml"), []byte("version: 1\nname: Level 01\nlayers:\n  - name: world\n    space: world\n    order: 0\n"), 0o644); err != nil {
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
				{Name: "level_01", Path: "scenes/level_01.scene.yaml"},
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

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetLanguage(editor.LanguageEnglish)
	ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	for _, act := range []struct {
		name string
		key  string
		btn  string
	}{
		{"move", "a", "dpadLeft"},
		{"jump", "space", "south"},
		{"attack", "j", "west"},
		{"pause", "escape", "start"},
	} {
		if err := ed.AddInputAction(act.name); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.AddInputBinding(act.name, project.Binding{Kind: project.KindKey, Key: act.key}); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.AddInputBinding(act.name, project.Binding{Kind: project.KindGamepadButton, Button: act.btn}); err != nil {
			t.Fatal(err)
		}
	}
	for _, bus := range []struct {
		name string
		vol  float32
	}{
		{"Music", 0.50},
		{"Effects", 0.71},
		{"Voice", 1.00},
	} {
		if err := ed.AddAudioBus(bus.name); err != nil {
			t.Fatal(err)
		}
		if err := ed.SetAudioBusVolume(bus.name, bus.vol); err != nil {
			t.Fatal(err)
		}
	}
	if err := ed.AddExportPreset("macos", "macos_arm64", "macos_x64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("windows", "windows_x64"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("web"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddExportPreset("ios"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveProject(); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	for _, shot := range []struct {
		page editor.SettingsPage
		name string
	}{
		{editor.SettingsPageProject, "settings-project"},
		{editor.SettingsPageInput, "settings-input-map"},
		{editor.SettingsPageAudio, "settings-audio-buses"},
		{editor.SettingsPageExport, "settings-export"},
	} {
		app.Post(func() { ed.OpenSettingsPage(shot.page) })
		app.Settle()
		app.Screenshot(shot.name)
	}
}

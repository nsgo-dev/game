package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
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

// TestSettingsAgentPagesMacOSScreenshots renders the three AGENT Settings pages
// (Models and connections, Tools, Usage and privacy) on native macOS in both Dark and Light
// appearances (`env -u GOROOT TMPDIR=/tmp go tool nsgo test -target macos -run TestSettingsAgentPagesMacOSScreenshots ./editor/editorcmd`)
// for visual review beside docs/design/editor/Settings.dc.html and Spec.dc.html.
func TestSettingsAgentPagesMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
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
		Window:     project.Window{Width: 640, Height: 360},
		Autoload: &project.Autoload{
			Scenes:    []project.Entry{{Name: "level_01", Path: "scenes/level_01.scene.yaml"}},
			MainScene: "level_01",
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
	ed.Keychain = editor.NewMemoryKeychain()
	ed.Now = func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }
	_ = ed.SetLanguage(editor.LanguageEnglish)
	_ = ed.SetAppearance(editor.AppearanceDark)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	srv := editormcp.New(ed, editormcp.Options{})
	t.Cleanup(func() { _ = srv.Close() })

	ed.NotifyAgentMCPConnected(editor.AgentClientInfo{
		ID:          "c1",
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Transport:   "stdio",
	})
	if err := ed.StoreAgentAPIKey(editor.DefaultAgentAPIKeyAccount, "sk-ant-screenshot-placeholder"); err != nil {
		t.Fatal(err)
	}
	ed.RecordAgentRequest(142)
	ed.RecordAgentReviewOutcome(true, 43)
	ed.RecordAgentReviewOutcome(false, 7)
	ed.RecordPlaytestRun(380)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	for _, shot := range []struct {
		page editor.SettingsPage
		name string
	}{
		{editor.SettingsPageModels, "settings-agent-models-dark"},
		{editor.SettingsPageTools, "settings-agent-tools-dark"},
		{editor.SettingsPageUsage, "settings-agent-usage-dark"},
	} {
		app.Post(func() { ed.OpenSettingsPage(shot.page) })
		app.Settle()
		app.Screenshot(shot.name)
	}
	app.Post(func() {
		_ = ed.SetAppearance(editor.AppearanceLight)
		ed.OpenSettingsPage(editor.SettingsPageModels)
	})
	app.Settle()
	app.Screenshot("settings-agent-models-light")
}

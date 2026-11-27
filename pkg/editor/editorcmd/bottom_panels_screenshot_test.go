package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const bottomPanelsFixtureProject = `version: 1
name: DungeonRun
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
`

const bottomPanelsFixtureNSGo = `name: DungeonRun
id: dev.example.dungeonrun
version: 1.0.0
`

const bottomPanelsFixtureLevelScene = `version: 2
uid: scn0000000101
name: Dungeon Level
layers:
  - name: actors
    objects:
      - id: hero
        name: Knight
        x: 64
        y: 80
        sprite:
          atlas: actors
          frame: knight_idle
      - id: slime
        name: Slime
        x: 160
        y: 80
        material: materials/stone.material.yaml
        sprite:
          atlas: actors
          frame: missing_frame
`

const bottomPanelsFixtureOtherScene = `version: 2
uid: scn0000000102
name: Crypt
layers:
  - name: actors
    objects:
      - id: pillar
        name: Stone Pillar
        material: materials/stone.material.yaml
`

const bottomPanelsFixtureAtlas = `{
  "frames": {
    "knight_idle": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}},
    "knight_run": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}}
  },
  "meta": {
    "image": "actors.png",
    "size": {"w": 32, "h": 16}
  }
}
`

const bottomPanelsFixtureTable = `version: 1
rows:
  - id: goblin
    name: Cave Goblin
    hp: 45
    speed: 3.5
    boss: false
    cooldown: 500ms
  - id: slime
    name: Cave Slime
    hp: 20
    speed: 1.5
    boss: false
    cooldown: 1s
  - id: wyrm
    name: Ancient Wyrm
    hp: 1200
    speed: 6.25
    boss: true
    cooldown: 2s
`

// TestBottomPanelsMacOSScreenshots renders each of the five bottom panels
// (Animation, Data Table, Localization, Search & References, and Problems) on
// the real native macOS host for visual review against Spec.dc.html.
func TestBottomPanelsMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(project.FileName, []byte(bottomPanelsFixtureProject))
	write(project.NSGoFileName, []byte(bottomPanelsFixtureNSGo))
	write("art/actors.json", []byte(bottomPanelsFixtureAtlas))
	write("art/actors.png", renderTilesetPNG(t))
	write("materials/stone.material.yaml", []byte("type: material\nversion: 1\n"))
	write("scenes/level.scene.yaml", []byte(bottomPanelsFixtureLevelScene))
	write("scenes/crypt.scene.yaml", []byte(bottomPanelsFixtureOtherScene))
	write("data/enemies.yaml", []byte(bottomPanelsFixtureTable))

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	_ = ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)

	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "physics",
		Fields: []sceneformat.Field{
			{Name: "gravity", Kind: sceneformat.FieldFloat, Default: 9.8},
		},
	})
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	app.Settle()

	// 1. Problems panel (with real missing-frame problem from assetcheck).
	app.Screenshot("bottom-panel-problems")

	// 2. Search & References panel: Search results and References tab.
	app.Post(func() {
		ed.SetSearchOpen(true)
		_ = ed.SetSearchQuery("stone")
	})
	app.Settle()
	app.Screenshot("bottom-panel-search")

	app.Post(func() {
		_, _ = ed.ShowReferences("materials/stone.material.yaml")
	})
	app.Settle()
	app.Screenshot("bottom-panel-references")

	app.Post(func() {
		ed.SetSearchOpen(false)
	})
	app.Settle()

	// 3. Data Table panel.
	app.Post(func() {
		if err := ed.OpenDataTableProjectFile("data/enemies.yaml"); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Screenshot("bottom-panel-datatable")

	app.Post(func() {
		_ = ed.CloseDataTable()
	})
	app.Settle()

	// 4. Localization panel (with a translated key, a missing translation, and a plural key).
	app.Post(func() {
		if err := ed.NewLocalizations(filepath.Join(dir, "locales"), "en", "zh-Hans"); err != nil {
			t.Error(err)
			return
		}
		_ = ed.AddLocalizationKey("menu.play", false)
		_ = ed.SetLocalizationString("en", "menu.play", "Play")
		_ = ed.AddLocalizationKey("hud.coins", true)
		_ = ed.SetLocalizationPlural("en", "hud.coins", l10n.PluralForms{One: "{count} coin", Other: "{count} coins"})
	})
	app.Settle()
	app.Screenshot("bottom-panel-localization")

	app.Post(func() {
		_ = ed.CloseLocalizations()
	})
	app.Settle()

	// 5. Animation panel (with keyed numeric tracks, selected key, and an event).
	app.Post(func() {
		ed.SelectObject("hero")
		if err := ed.NewClip("hero", "Knight Hop", 2*time.Second); err != nil {
			t.Error(err)
			return
		}
		if tr, k, err := ed.KeyProperty("", "x", 0); err == nil {
			_ = ed.SetKeyValue(tr, k, 64)
			_ = ed.SetKeyInterp(tr, k, resource.InterpSmooth)
		}
		if tr, k, err := ed.KeyProperty("", "x", time.Second); err == nil {
			_ = ed.SetKeyValue(tr, k, 128)
			ed.SelectClipKey(tr, k)
		}
		if tr, k, err := ed.KeyProperty("", "alpha", 500*time.Millisecond); err == nil {
			_ = ed.SetKeyValue(tr, k, 0.5)
		}
		_ = ed.AddClipEvent(time.Second, "land")
		ed.SeekClip(time.Second)
	})
	app.Settle()
	app.Screenshot("bottom-panel-animation")

	// 6. Focus-visible ring on a real timeline keyframe button via keyboard navigation.
	app.Focus("id:anim-key-0-0")
	app.Key("Tab")
	app.Settle()
	app.Screenshot("bottom-panel-animation-focus")
}

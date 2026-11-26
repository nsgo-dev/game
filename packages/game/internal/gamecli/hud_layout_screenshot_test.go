package gamecli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestEditorHUDLayoutScreenshots mounts the real NSGo Game Editor on macOS with the dungeon-run
// HUD and pause screens matching docs/design/editor/HUDLayout.dc.html and captures screenshots
// for side-by-side visual review against the artboard.
func TestEditorHUDLayoutScreenshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nid: dev.example.dungeonrun\nname: dungeon-run\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\ndisplay:\n  width: 640\n  height: 360\n"), 0o644); err != nil {
		t.Fatalf("write game.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		t.Fatalf("mkdir locales: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "screens"), 0o755); err != nil {
		t.Fatalf("mkdir screens: %v", err)
	}

	writeLocale := func(file, loc string, stringsMap map[string]string) {
		data, err := l10n.EncodeYAML(&l10n.StringTable{
			Version: l10n.StringTableVersion,
			Locale:  loc,
			Strings: stringsMap,
		})
		if err != nil {
			t.Fatalf("encode %s: %v", file, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "locales", file), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	writeLocale("en.yaml", "en", map[string]string{
		"hud.coins":      "× 12",
		"hud.floor.1":    "Floor 1 · The Cellar",
		"hud.hint.lever": "Jump at levers to pull",
		"pause.title":    "Paused",
		"pause.resume":   "Resume",
		"pause.restart":  "Restart checkpoint",
		"pause.settings": "Settings",
		"pause.quit":     "Quit to title",
		"title.heading":  "Dungeon Run",
		"gameover.title": "Game Over",
	})
	writeLocale("zh-Hans.yaml", "zh-Hans", map[string]string{
		"hud.coins":      "× 12",
		"hud.floor.1":    "第一层 · 地窖",
		"hud.hint.lever": "靠近拉杆时按下跳跃键即可拉动",
		"pause.title":    "已暂停",
		"pause.resume":   "继续",
		"pause.restart":  "从上一个检查点重新开始",
		"pause.settings": "设置",
		"pause.quit":     "返回标题画面",
		"title.heading":  "地牢奔跑",
		"gameover.title": "游戏结束",
	})

	safeFalse := false
	writeScreen := func(file string, doc *gameui.ScreenDoc) {
		data, err := gameui.EncodeScreen(doc)
		if err != nil {
			t.Fatalf("encode %s: %v", file, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "screens", file), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	writeScreen("hud.screen.yaml", &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "hud",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{
				ID:      "status",
				Type:    gameui.NodeVBox,
				Anchor:  gameui.AnchorTopLeft,
				Offset:  &gameui.ScreenOffsetDoc{X: 12, Y: 12},
				Spacing: 4,
				Children: []gameui.ScreenNodeDoc{
					{ID: "hearts", Type: gameui.NodeSpriteRow, Width: 52, Height: 14, SpriteCount: 3, Spacing: 2},
					{ID: "coins", Type: gameui.NodeLabel, Width: 52, Height: 16, TextKey: "hud.coins"},
				},
			},
			{
				ID:         "floor_label",
				Type:       gameui.NodeLabel,
				Anchor:     gameui.AnchorTopCenter,
				Offset:     &gameui.ScreenOffsetDoc{X: 0, Y: 12},
				SizeMode:   gameui.SizeFitText,
				Height:     18,
				TextKey:    "hud.floor.1",
				TextColor:  "#6b624f",
				Background: "#1c2230",
			},
			{
				ID:           "pause_button",
				Type:         gameui.NodeButton,
				Anchor:       gameui.AnchorTopRight,
				Offset:       &gameui.ScreenOffsetDoc{X: 12, Y: 12},
				Width:        24,
				Height:       24,
				Background:   "rgba(16,18,22,0.6)",
				CornerRadius: 4,
			},
			{
				ID:           "hint",
				Type:         gameui.NodeLabel,
				Anchor:       gameui.AnchorBottomCenter,
				Offset:       &gameui.ScreenOffsetDoc{X: 0, Y: 70},
				SizeMode:     gameui.SizeFixed,
				Width:        160,
				Height:       22,
				Padding:      &gameui.ScreenPaddingDoc{Left: 6, Right: 6},
				TextKey:      "hud.hint.lever",
				TextColor:    "#ffffff",
				Background:   "rgba(16,18,22,0.78)",
				CornerRadius: 4,
			},
			{
				ID:            "touch",
				Type:          gameui.NodeContainer,
				DeviceClasses: []gameui.DeviceClass{gameui.DevicePhone, gameui.DeviceTablet},
				Children: []gameui.ScreenNodeDoc{
					{ID: "stick", Type: gameui.NodeTouchStick, Anchor: gameui.AnchorBottomLeft, Offset: &gameui.ScreenOffsetDoc{X: 24, Y: 20}, Width: 72, Height: 72, TouchHitSize: 96},
					{ID: "jump", Type: gameui.NodeTouchButton, Anchor: gameui.AnchorBottomRight, Offset: &gameui.ScreenOffsetDoc{X: 24, Y: 28}, Width: 52, Height: 52, TouchHitSize: 64},
					{ID: "attack", Type: gameui.NodeTouchButton, Anchor: gameui.AnchorBottomRight, Offset: &gameui.ScreenOffsetDoc{X: 92, Y: 20}, Width: 36, Height: 36, TouchHitSize: 36},
				},
			},
		},
	})
	writeScreen("pause.screen.yaml", &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "pause",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{ID: "dim", Type: gameui.NodeColorRect, Anchor: gameui.AnchorFill, SafeArea: &safeFalse, Background: "rgba(10,12,16,0.62)"},
			{
				ID:           "panel",
				Type:         gameui.NodeVBox,
				Anchor:       gameui.AnchorCenter,
				Offset:       &gameui.ScreenOffsetDoc{X: 0, Y: 4},
				Width:        200,
				Height:       196,
				Padding:      &gameui.ScreenPaddingDoc{Top: 14, Right: 30, Bottom: 14, Left: 30},
				Spacing:      8,
				Background:   "#171a21",
				CornerRadius: 8,
				Children: []gameui.ScreenNodeDoc{
					{ID: "title", Type: gameui.NodeLabel, SizeMode: gameui.SizeFitText, Height: 20, TextKey: "pause.title"},
					{ID: "resume", Type: gameui.NodeButton, Width: 140, Height: 26, Padding: &gameui.ScreenPaddingDoc{Left: 8, Right: 8}, TextKey: "pause.resume", FocusOrder: 1},
					{ID: "restart", Type: gameui.NodeButton, Width: 140, Height: 26, Padding: &gameui.ScreenPaddingDoc{Left: 8, Right: 8}, TextKey: "pause.restart", FocusOrder: 2},
					{ID: "settings", Type: gameui.NodeButton, Width: 140, Height: 26, Padding: &gameui.ScreenPaddingDoc{Left: 8, Right: 8}, TextKey: "pause.settings", FocusOrder: 3},
					{ID: "quit", Type: gameui.NodeButton, Width: 140, Height: 26, Padding: &gameui.ScreenPaddingDoc{Left: 8, Right: 8}, TextKey: "pause.quit", FocusOrder: 4},
				},
			},
		},
	})
	writeScreen("title.screen.yaml", &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "title",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{ID: "title_bg", Type: gameui.NodeColorRect, Anchor: gameui.AnchorFill, Background: "#101216"},
			{ID: "title_heading", Type: gameui.NodeLabel, Anchor: gameui.AnchorCenter, SizeMode: gameui.SizeFitText, Height: 24, TextKey: "title.heading"},
		},
	})
	writeScreen("game_over.screen.yaml", &gameui.ScreenDoc{
		Version:      gameui.ScreenFormatVersion,
		Name:         "game_over",
		DesignWidth:  640,
		DesignHeight: 360,
		Nodes: []gameui.ScreenNodeDoc{
			{ID: "over_bg", Type: gameui.NodeColorRect, Anchor: gameui.AnchorFill, Background: "#101216"},
			{ID: "over_heading", Type: gameui.NodeLabel, Anchor: gameui.AnchorCenter, SizeMode: gameui.SizeFitText, Height: 24, TextKey: "gameover.title"},
		},
	})

	ed, err := newDemoEditor()
	if err != nil {
		t.Fatalf("newDemoEditor: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenHUDScreen("screens/hud.screen.yaml"); err != nil {
		t.Fatalf("OpenHUDScreen: %v", err)
	}
	ed.SetHUDLocale("zh-Hans")
	ed.SelectHUDNode("hint")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	if !app.Exists("id:hud-zoom-detail") {
		t.Fatal("expected zoomed overflow detail visible in 中文")
	}
	app.Screenshot("hud_layout/hud_zh")

	app.Press("id:hud-screen-pause")
	app.Settle()
	app.Press("id:hud-lang-en")
	app.Settle()
	if !app.Exists("id:hud-check-empty") {
		t.Fatal("expected no problems message on pause screen in English")
	}
	app.Screenshot("hud_layout/pause_en")
}

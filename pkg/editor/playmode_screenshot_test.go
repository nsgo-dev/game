package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosPlayModeScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        name: Floor
        x: 0
        y: 300
      - id: plank
        name: Plank
        x: 240
        y: 256
      - id: knight
        name: Knight
        x: 100
        y: 222
        z: 10
        properties:
          hp: 100
          max_hp: 100
      - id: slime_01
        name: Slime 01
        x: 340
        y: 280
      - id: lever_01
        name: Lever 01
        x: 420
        y: 264
`

// TestPlayModeMacOSScreenshots renders Play mode marks, the frozen-frame Ask dialog,
// the viewport agent stepping pill + step log, and the Output panel panic card on the
// real native macOS host (linking github.com/nsgo-dev/nsgo through editor_test.go) for
// visual review beside design/editor/PlayMode.dc.html.
func TestPlayModeMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	sceneDir := filepath.Join(dir, "scenes")
	if err := os.MkdirAll(sceneDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: dungeon-run
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nsgo.yaml"), []byte("name: dungeon-run\nid: dev.example.dungeonrun\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sceneDir, "level_01.scene.yaml"), []byte(macosPlayModeScene), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProjectFile("scenes/level_01.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	ed.PlayRate = 60
	ed.SetAppearance(editor.AppearanceDark)
	_ = ed.SetLanguage(editor.LanguageEnglish)
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, _ time.Duration) {
		if k := scene.Object("knight"); k != nil {
			k.Doc.X += 0.1
		}
	}
	ed.SelectObject("knight")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// 1) Play mode with marks, rolling replay footer, and 60s frame-time strip.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.SetPlayPaused(true)
		ed.StepPlay(1407)
		if _, err := ed.AddPlayMark(); err != nil {
			t.Errorf("AddPlayMark: %v", err)
		}
		ed.SetPlayInspectorTab(editor.PlayTabAgent)
	})
	app.Settle()
	app.Find("id:play-mode-bar")
	app.Find("id:play-replay-footer")
	app.Find("id:play-marks-header")
	app.Screenshot("playmode-marks")

	// 2) Frozen frame with "Ask about frame N" dialog open.
	app.Post(func() {
		if err := ed.OpenAskFrozenFrame(); err != nil {
			t.Errorf("OpenAskFrozenFrame: %v", err)
		}
		ed.SetAskFrozenFrameDraft("The knight snags on the plank's left edge when he lands short.")
	})
	app.Settle()
	app.Find("id:play-ask-dialog")
	app.Find("id:play-ask-review-note")
	app.Screenshot("playmode-ask-frozen")

	// 3) Agent stepping pill + step log + Output panel panic card.
	app.Post(func() {
		if _, err := ed.SubmitAskFrozenFrame("The knight snags on the plank's left edge when he lands short."); err != nil {
			t.Errorf("SubmitAskFrozenFrame: %v", err)
		}
		if _, err := ed.AgentStepPlayReplay(1407, 1419, "the knight stops at x 251 with velocity 0"); err != nil {
			t.Errorf("AgentStepPlayReplay: %v", err)
		}
		rep, ok := editor.ParsePanicTrace([]string{
			"14:02:17 frame 2940",
			"panic: runtime error: index out of range [3] with length 3",
			"goroutine 1 [running]:",
			"example.com/dungeon/game/lever.(*System).toggle(0xc00012a000, 0x3)",
			fmt.Sprintf("\t%s/game/lever/system.go:83 +0x4c", dir),
			"example.com/dungeon/game/lever.(*System).Update(0xc00012a000, 0x1)",
			fmt.Sprintf("\t%s/game/lever/system.go:57 +0x24", dir),
			"github.com/nsgo-dev/game.(*Loop).StepNow(0xc000090000, 0x1)",
			"\t/go/pkg/mod/github.com/nsgo-dev/game@v0.1.0/loop.go:175 +0x90",
			"github.com/nsgo-dev/nsgo/core.(*Scope).frame(0xc000080000)",
			"\t/go/pkg/nsgo/core/scope.go:221 +0x38",
			"runtime.gopanic(0x1, 0x2)",
			"\t/usr/local/go/src/runtime/panic.go:770 +0x132",
			"runtime.goPanicIndex(0x3, 0x3)",
			"\t/usr/local/go/src/runtime/panic.go:115 +0x74",
		}, dir)
		if !ok {
			t.Errorf("ParsePanicTrace returned ok=false")
		}
		rep.RunNumber = 2
		rep.Time = time.Date(2026, 11, 25, 14, 2, 17, 0, time.UTC)
		rep.ReplayPath = ed.PlayLastReplayPath()
		ed.SetOutputPanic(rep)
	})
	app.Settle()
	app.Find("id:play-agent-pill")
	app.Find("id:output-panic-card")
	app.Screenshot("playmode-agent-and-panic")
}

package editorcmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const problemScreenshotDoorsScene = `version: 2
name: Doors
layers:
  - name: Arena
    objects:
      - id: door_3
        name: door_3
        x: 520
        "y": 230
        components:
          door: {}
`

const problemScreenshotBossScene = `version: 2
name: boss_room
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#1B1726"
inherits: scenes/doors.scene.yaml
layers:
  - name: Arena
    objects:
      - id: boss
        name: Boss
        x: 320
        "y": 220
      - id: lever
        name: lever
        x: 160
        "y": 255
        components:
          lever: {}
connections:
  - from: lever
    event: pulled
    to: door_3
    handler: open
`

// TestProblemAndProposedFixMacOSScreenshots renders the live broken-connection
// problem state (scene tree badge, viewport Problem overlay, status bar pill,
// and Agent proposed-fix card) in both Dark and Light Spec appearances beside
// docs/design/editor/Problem.dc.html.
func TestProblemAndProposedFixMacOSScreenshots(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		project.FileName: `version: 1
name: dungeon-run
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: boss_room
  scenes:
    - name: boss_room
      path: scenes/boss_room.scene.yaml
`,
		project.NSGoFileName:          "name: dungeon-run\nid: dev.example.dungeonrun\nversion: 1.0.0\n",
		"scenes/doors.scene.yaml":     problemScreenshotDoorsScene,
		"scenes/boss_room.scene.yaml": problemScreenshotBossScene,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{Name: "lever", Events: []sceneformat.Event{{Name: "pulled"}}})
	reg.MustRegister(sceneformat.ComponentType{Name: "door", Handlers: []sceneformat.Handler{{Name: "open"}}})

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectAssets = true
	ed.ProblemCheckDebounce = 15 * time.Millisecond
	t.Cleanup(ed.WaitForProblems)
	if err := ed.SetComponentRegistry(reg); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	// Rename door_3 -> gate without saving, then open boss_room and propose the fix.
	if err := ed.OpenScene("scenes/doors.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectID("door_3", "gate"); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	probs := ed.Problems()
	if len(probs) != 1 {
		t.Fatalf("expected 1 live problem, got %+v", probs)
	}
	if err := ed.OpenScene("scenes/boss_room.scene.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.ProposeProblemFix(probs[0],
		"The lever was wired to door_3, which was renamed to gate in your last edit. I can point the connection at gate, which is the only object here with an open handler.",
		editor.ProblemFixEdit{
			Kind:            editor.FixKindUpdateConnection,
			ConnectionIndex: 0,
			To:              "gate",
		},
	); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))
	app.Settle()

	if !app.Exists("id:problem-agent-panel") || !app.Exists("id:scene-tree-problem-lever") {
		t.Fatal("expected problem-agent-panel and scene-tree-problem-lever to be visible")
	}

	app.Screenshot("problem-agent-fix-dark")

	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	})
	app.Settle()
	app.Screenshot("problem-agent-fix-light")

	// Apply fix and capture post-fix toast state.
	app.SetEnvironment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	})
	app.Settle()
	app.Press("id:problem-fix-apply")
	app.WaitUntil(10*time.Second, func() bool {
		return !ed.CheckingProblems() && ed.ProblemCount() == 0
	})
	app.Settle()
	app.Screenshot("problem-agent-fix-applied-dark")
}

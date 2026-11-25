package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const editorPhysicsProjectYAML = `version: 1
name: Physics Game
id: dev.nsgo.physgame
appVersion: 0.1.0
window:
  width: 640
  height: 360
physics:
  gravity2D: {"y": 0}
  gravity3D: {"y": -9.81}
  solverIterations2D: 10
  solverIterations3D: 8
  layers:
    - name: player
      bit: 0
    - name: walls
      bit: 1
    - name: coins
      bit: 2
autoload:
  mainScene: arena
  scenes:
    - name: arena
      path: scenes/arena.scene.yaml
`

const editorPhysicsSceneYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Arena
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    space: world
    objects:
      - id: player
        x: 30
        "y": 90
        components:
          body2d:
            gravityScale: 0
            kind: dynamic
            mass: 1
            version: 1
            vx: 160
          collider2d:
            height: 16
            layer: player
            mask:
              - walls
              - coins
            shape: box
            version: 1
            width: 16
      - id: coin
        x: 75
        "y": 90
        components:
          trigger2d:
            layer: coins
            mask:
              - player
            radius: 10
            shape: circle
            version: 1
      - id: wall
        x: 130
        "y": 90
        components:
          collider2d:
            height: 80
            layer: walls
            mask:
              - player
            shape: box
            version: 1
            width: 16
`

func TestEditorPlayModeSimulatesPhysicsPlayerWallAndCoinWithoutGameCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(editorPhysicsProjectYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	scenePath := filepath.Join(dir, "scenes", "arena.scene.yaml")
	if err := os.WriteFile(scenePath, []byte(editorPhysicsSceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(sceneformat.PhysicsComponentRegistry()); err != nil {
		t.Fatalf("SetComponentRegistry: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Acceptance: load and save round-trips byte for byte.
	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	if !bytes.Equal(saved, []byte(editorPhysicsSceneYAML)) {
		t.Fatalf("scene round-trip differed:\ngot:\n%s\nwant:\n%s", saved, editorPhysicsSceneYAML)
	}

	// Play mode runs with NO OnPlayUpdate collision code at all.
	ed.PlayRate = 60
	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 720))

	var coinEntered bool
	ed.OnPlayTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		if ev.Kind == sceneformat.TriggerEnter && ev.TriggerID == "coin" && ev.BodyID == "player" {
			coinEntered = true
		}
	}

	if err := ed.PlayIn(rootScope); err != nil {
		t.Fatalf("PlayIn: %v", err)
	}
	if ed.PlayPhysics() == nil || !ed.PlayPhysics().HasPhysics() {
		t.Fatal("PlayPhysics not constructed during Play mode")
	}

	// Drive deterministic steps directly on PlayPhysics (matching the live loop's step) so the test never flakes on wall-clock timing.
	app.Post(func() {
		ps := ed.PlayPhysics()
		for range 90 {
			ps.Step(time.Second / 60)
		}
	})
	app.Settle()

	if !coinEntered {
		t.Fatalf("expected coin trigger enter event without game collision code; recorded=%+v", ed.PlayTriggerEvents())
	}
	playerObj := ed.PlayScene().Object("player")
	if playerObj == nil {
		t.Fatal("player object missing from play scene")
	}
	// Wall is at x=130, width=16 (left edge 122); player width=16 (half=8) so player stops around x=114.
	if playerObj.Doc.X <= 90 || playerObj.Doc.X > 116 {
		t.Fatalf("player X = %v, expected stopped against wall (~114)", playerObj.Doc.X)
	}

	if err := ed.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	afterStop, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterStop, []byte(editorPhysicsSceneYAML)) {
		t.Fatal("Stop did not restore exact pre-play scene bytes")
	}
}

func TestEditorOpenFileReportsMisspelledPhysicsLayerWithFileAndLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: Layers
id: dev.nsgo.layers
appVersion: 0.1.0
window:
  width: 640
  height: 360
physics:
  layers:
    - name: player
      bit: 0
    - name: walls
      bit: 1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	badScenePath := filepath.Join(dir, "bad.scene.yaml")
	badScene := `version: 2
layers:
  - name: main
    objects:
      - id: wall
        components:
          collider2d:
            version: 1
            shape: box
            width: 32
            height: 32
            layer: wallz
`
	if err := os.WriteFile(badScenePath, []byte(badScene), 0o644); err != nil {
		t.Fatal(err)
	}

	err = ed.OpenFile(badScenePath)
	var le *loaderr.Error
	if !errors.As(err, &le) {
		t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
	}
	if le.File != badScenePath || le.Line != 12 {
		t.Fatalf("got %s:%d, want %s:12 (%v)", le.File, le.Line, badScenePath, err)
	}
	if !strings.Contains(err.Error(), badScenePath+":12:") {
		t.Fatalf("error string %q missing file:line prefix", err.Error())
	}
}

func TestEditorPhysicsInspectorAndDebugDrawing2DAnd3D(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(editorPhysicsSceneYAML))
	if err != nil {
		t.Fatal(err)
	}
	// Also attach a 3D collider and trigger for 3D debug draw coverage.
	doc.Layers[0].Objects[0].Components["body3d"] = map[string]any{"version": 1, "kind": "dynamic", "mass": 1.0}
	doc.Layers[0].Objects[0].Components["collider3d"] = map[string]any{"version": 1, "shape": "sphere", "radius": 8.0}
	doc.Layers[0].Objects[1].Components["trigger3d"] = map[string]any{"version": 1, "shape": "capsule", "radius": 6.0, "height": 20.0}

	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("player")

	// Inspector renders built-in physics component fields even without an explicit SetComponentRegistry.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1400))

	if got := app.Find("id:inspector-comp-body2d-kind-label").Text; got != "kind" {
		t.Fatalf("body2d.kind label = %q, want kind", got)
	}
	if got := app.Find("id:inspector-comp-collider2d-shape-label").Text; got != "shape" {
		t.Fatalf("collider2d.shape label = %q, want shape", got)
	}

	// 2D debug drawing produces more draw ops when ShowPhysicsDebug is true than when false.
	var withDebug2D, withoutDebug2D core.Drawing
	ed.SetShowPhysicsDebug(true)
	ed.DrawViewport(&withDebug2D, core.Size{Width: 320, Height: 180})
	ed.SetShowPhysicsDebug(false)
	ed.DrawViewport(&withoutDebug2D, core.Size{Width: 320, Height: 180})
	if len(withDebug2D.Ops) <= len(withoutDebug2D.Ops) {
		t.Fatalf("expected 2D physics debug drawing ops (%d) > without (%d)", len(withDebug2D.Ops), len(withoutDebug2D.Ops))
	}

	// 3D debug drawing produces more draw ops when ShowPhysicsDebug is true than when false.
	ed.SetViewMode(editor.ViewMode3D)
	var withDebug3D, withoutDebug3D core.Drawing
	ed.SetShowPhysicsDebug(true)
	ed.DrawViewport(&withDebug3D, core.Size{Width: 320, Height: 180})
	ed.SetShowPhysicsDebug(false)
	ed.DrawViewport(&withoutDebug3D, core.Size{Width: 320, Height: 180})
	if len(withDebug3D.Ops) <= len(withoutDebug3D.Ops) {
		t.Fatalf("expected 3D physics debug drawing ops (%d) > without (%d)", len(withDebug3D.Ops), len(withoutDebug3D.Ops))
	}
}

func TestProjectSettingsPhysicsTab(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(`version: 1
name: Demo
id: dev.nsgo.demo
appVersion: 0.1.0
window:
  width: 640
  height: 360
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	if err := ed.SetProjectPhysicsGravity2D(0, 980); err != nil {
		t.Fatalf("SetProjectPhysicsGravity2D: %v", err)
	}
	if err := ed.SetProjectPhysicsGravity3D(0, -12, 0); err != nil {
		t.Fatalf("SetProjectPhysicsGravity3D: %v", err)
	}
	if err := ed.SetProjectPhysicsSolverIterations(12, 9); err != nil {
		t.Fatalf("SetProjectPhysicsSolverIterations: %v", err)
	}
	if err := ed.AddProjectPhysicsLayer("player", 0); err != nil {
		t.Fatalf("AddProjectPhysicsLayer: %v", err)
	}
	if err := ed.AddProjectPhysicsLayer("walls", 1); err != nil {
		t.Fatalf("AddProjectPhysicsLayer walls: %v", err)
	}
	if err := ed.AddProjectPhysicsLayer("walls", 2); err == nil {
		t.Fatal("expected duplicate layer name error")
	}

	ed.SetProjectSettingsOpen(true)
	ed.SetProjectSettingsSection(editor.ProjectSectionPhysics)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 900))

	if got := app.Find("id:project-settings-physics-layer-0").Text; got != "Bit 0: player" {
		t.Fatalf("layer 0 text = %q, want Bit 0: player", got)
	}
	if err := ed.RemoveProjectPhysicsLayer(0); err != nil {
		t.Fatalf("RemoveProjectPhysicsLayer: %v", err)
	}
	if !ed.Undo() {
		t.Fatal("expected Undo to restore removed physics layer")
	}
	if len(ed.Project.Physics.Layers) != 2 {
		t.Fatalf("layers after undo = %+v", ed.Project.Physics.Layers)
	}
}

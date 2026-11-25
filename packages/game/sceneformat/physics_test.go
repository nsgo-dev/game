package sceneformat_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func testPhysicsLayers(t *testing.T) *sceneformat.PhysicsLayerTable {
	t.Helper()
	return sceneformat.MustPhysicsLayerTable([]sceneformat.PhysicsLayerDef{
		{Name: "player", Bit: 0},
		{Name: "walls", Bit: 1},
		{Name: "coins", Bit: 2},
	})
}

const canonicalPhysicsSceneYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Physics Arena
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    space: world
    objects:
      - id: player
        x: 40
        "y": 90
        components:
          body2d:
            gravityScale: 0
            kind: dynamic
            mass: 1
            version: 1
            vx: 120
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
        x: 90
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
        x: 150
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

func TestPhysicsSceneRoundTripByteForByte(t *testing.T) {
	reg := sceneformat.PhysicsComponentRegistry()
	layers := testPhysicsLayers(t)

	doc, err := sceneformat.ParseWith([]byte(canonicalPhysicsSceneYAML), sceneformat.ParseOptions{
		File:          "scenes/arena.scene.yaml",
		Components:    reg,
		PhysicsLayers: layers,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, []byte(canonicalPhysicsSceneYAML)) {
		t.Fatalf("scene round-trip mismatch:\ngot:\n%s\nwant:\n%s", out, canonicalPhysicsSceneYAML)
	}
}

func TestMisspelledPhysicsLayerReportsExactFileAndLine(t *testing.T) {
	reg := sceneformat.PhysicsComponentRegistry()
	layers := testPhysicsLayers(t)

	// Case 1: misspelled layer name on line 14.
	badLayer := `version: 2
layers:
  - name: main
    objects:
      - id: wall
        x: 100
        "y": 50
        components:
          collider2d:
            version: 1
            shape: box
            width: 16
            height: 16
            layer: wallz
`
	_, err := sceneformat.ParseWith([]byte(badLayer), sceneformat.ParseOptions{
		File:          "scenes/level1.scene.yaml",
		Components:    reg,
		PhysicsLayers: layers,
	})
	var le *loaderr.Error
	if !errors.As(err, &le) {
		t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
	}
	if le.File != "scenes/level1.scene.yaml" || le.Line != 14 {
		t.Fatalf("got %s:%d (col %d), want scenes/level1.scene.yaml:14 (%v)", le.File, le.Line, le.Column, err)
	}
	if !strings.Contains(le.Problem, `unknown physics layer "wallz"`) {
		t.Fatalf("unexpected problem: %s", le.Problem)
	}

	// Case 2: misspelled layer inside mask list on line 15.
	badMask := `version: 2
layers:
  - name: main
    objects:
      - id: player
        components:
          collider2d:
            version: 1
            shape: box
            width: 16
            height: 16
            layer: player
            mask:
              - walls
              - cions
`
	_, err = sceneformat.ParseWith([]byte(badMask), sceneformat.ParseOptions{
		File:          "scenes/level2.scene.yaml",
		Components:    reg,
		PhysicsLayers: layers,
	})
	if !errors.As(err, &le) {
		t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
	}
	if le.File != "scenes/level2.scene.yaml" || le.Line != 15 {
		t.Fatalf("got %s:%d, want scenes/level2.scene.yaml:15 (%v)", le.File, le.Line, err)
	}
	if !strings.Contains(le.Problem, `unknown physics layer "cions" in mask`) {
		t.Fatalf("unexpected problem: %s", le.Problem)
	}
}

func TestPhysicsComponentValidationRejectsUnknownFieldsAndBadEnums(t *testing.T) {
	reg := sceneformat.PhysicsComponentRegistry()
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown field",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          body2d:\n            bogus: 1\n",
			want: `field "bogus": unknown field`,
		},
		{
			name: "unsupported component version",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          body2d:\n            version: 2\n",
			want: "above the maximum 1",
		},
		{
			name: "invalid body kind enum",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          body2d:\n            kind: flying\n",
			want: "must be one of static, kinematic, dynamic",
		},
		{
			name: "invalid 3d shape enum",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          collider3d:\n            shape: cone\n",
			want: "must be one of box, sphere, capsule",
		},
		{
			name: "non-string mask entry",
			yaml: "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          collider2d:\n            mask: [123]\n",
			want: "element 0: want string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.ParseWith([]byte(tc.yaml), sceneformat.ParseOptions{Components: reg})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseWith = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestBuildPhysics2DPlayerWallAndCoinTrigger(t *testing.T) {
	layers := testPhysicsLayers(t)
	doc, err := sceneformat.ParseWith([]byte(canonicalPhysicsSceneYAML), sceneformat.ParseOptions{
		Components:    sceneformat.PhysicsComponentRegistry(),
		PhysicsLayers: layers,
	})
	if err != nil {
		t.Fatal(err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, &sceneformat.PhysicsSettings{
		Layers: layers,
	}, nil)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}
	if !ps.HasPhysics() {
		t.Fatal("expected HasPhysics() to be true")
	}

	var enters, exits int
	ps.OnTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		if ev.TriggerID == "coin" && ev.BodyID == "player" {
			switch ev.Kind {
			case sceneformat.TriggerEnter:
				enters++
			case sceneformat.TriggerExit:
				exits++
			}
		}
	}

	const dt = time.Second / 60
	for range 120 {
		ps.Step(dt)
	}

	if enters != 1 || exits != 1 {
		t.Fatalf("coin trigger enter=%d exit=%d, want 1 and 1", enters, exits)
	}
	// Player (width 16, half-width 8) must be stopped by wall at x=150 (width 16, left edge 142 => player max center 134).
	playerX := built.Object("player").Doc.X
	if playerX <= 115 || playerX > 136 {
		t.Fatalf("player X = %v, expected stopped near wall left face (~134)", playerX)
	}
}

func TestBuildPhysics2DKinematicPlayerStopsAtWall(t *testing.T) {
	layers := testPhysicsLayers(t)
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{
					ID: "player", X: 20, Y: 50,
					Components: map[string]any{
						"body2d":     map[string]any{"version": 1, "kind": "kinematic", "vx": 150.0},
						"collider2d": map[string]any{"version": 1, "shape": "box", "width": 16.0, "height": 16.0, "layer": "player", "mask": []string{"walls", "coins"}},
					},
				},
				{
					ID: "coin", X: 60, Y: 50,
					Components: map[string]any{
						"trigger2d": map[string]any{"version": 1, "shape": "box", "width": 16.0, "height": 16.0, "layer": "coins", "mask": []string{"player"}},
					},
				},
				{
					ID: "wall", X: 110, Y: 50,
					Components: map[string]any{
						"collider2d": map[string]any{"version": 1, "shape": "box", "width": 16.0, "height": 64.0, "layer": "walls", "mask": []string{"player"}},
					},
				},
			},
		}},
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, &sceneformat.PhysicsSettings{Layers: layers}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entered := false
	ps.OnTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		if ev.Kind == sceneformat.TriggerEnter && ev.TriggerID == "coin" && ev.BodyID == "player" {
			entered = true
		}
	}
	for range 90 {
		ps.Step(time.Second / 60)
	}
	if !entered {
		t.Fatal("kinematic player did not trigger coin")
	}
	if px := built.Object("player").Doc.X; px < 90 || px > 95 {
		t.Fatalf("kinematic player X = %v, want stopped at wall (~94)", px)
	}
}

func TestBuildPhysics3DPlayerWallAndTriggerAndMaterial(t *testing.T) {
	layers := testPhysicsLayers(t)
	fsys := fstest.MapFS{
		"materials/bouncy.physics.yaml": &fstest.MapFile{Data: []byte("type: physicsMaterial\nversion: 1\nfriction: 0.2\nrestitution: 0.85\nlinearDamping: 0\n")},
	}
	lib := resource.NewLibrary(fsys)
	resolver := struct {
		sceneformat.AtlasMap
		*resource.Library
	}{Library: lib}

	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{{
			Name: "main",
			Objects: []sceneformat.ObjectDoc{
				{
					ID: "player", X: 0, Y: 1, PosZ: 0,
					Components: map[string]any{
						"body3d": map[string]any{
							"version":      1,
							"kind":         "dynamic",
							"mass":         1.0,
							"gravityScale": 0.0,
							"vx":           6.0,
							"material":     "materials/bouncy.physics.yaml",
						},
						"collider3d": map[string]any{
							"version": 1,
							"shape":   "sphere",
							"radius":  0.5,
							"layer":   "player",
							"mask":    []string{"walls", "coins"},
						},
					},
				},
				{
					ID: "coin", X: 2.5, Y: 1, PosZ: 0,
					Components: map[string]any{
						"trigger3d": map[string]any{
							"version": 1,
							"shape":   "capsule",
							"radius":  0.5,
							"height":  1.5,
							"layer":   "coins",
							"mask":    []string{"player"},
						},
					},
				},
				{
					ID: "wall", X: 5.0, Y: 1, PosZ: 0,
					Components: map[string]any{
						"collider3d": map[string]any{
							"version": 1,
							"shape":   "box",
							"width":   1.0,
							"height":  4.0,
							"depth":   4.0,
							"layer":   "walls",
							"mask":    []string{"player"},
						},
					},
				},
			},
		}},
	}

	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, &sceneformat.PhysicsSettings{
		HasGravity2D: true,
		Gravity2D:    math2d.Vec2{Y: 0},
		HasGravity3D: true,
		Gravity3D:    physics3d.Vec3{Y: -9.81},
		Layers:       layers,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildPhysics 3D: %v", err)
	}

	if b := ps.Bodies3D["player"]; b == nil || b.Restitution != 0.85 || b.Friction != 0.2 {
		t.Fatalf("3D player material not applied: %+v", b)
	}

	var triggered3D bool
	ps.OnTrigger = func(ev sceneformat.PhysicsTriggerEvent) {
		if ev.Is3D && ev.Kind == sceneformat.TriggerEnter && ev.TriggerID == "coin" && ev.BodyID == "player" {
			triggered3D = true
		}
	}

	for range 90 {
		ps.Step(time.Second / 60)
	}
	if !triggered3D {
		t.Fatal("expected 3D coin trigger enter event")
	}
	// With restitution 0.85 against the wall at X=5 (left face 4.5), player should have bounced back below X=4.1!
	if px := built.Object("player").Doc.X; px >= 4.1 {
		t.Fatalf("3D player X = %v, expected collision and rebound off wall at x=5", px)
	}
}

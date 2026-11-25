package project_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"time"
)

func TestPhysicsRoundTripAndRuntimeConfiguration(t *testing.T) {
	const raw = `version: 1
name: Physics Demo
id: dev.nsgo.physdemo
appVersion: 0.1.0
window:
  width: 640
  height: 360
physics:
  gravity2D: {"y": 980}
  gravity3D: {"y": -9.81}
  solverIterations2D: 12
  solverIterations3D: 10
  layers:
    - name: player
      bit: 0
    - name: walls
      bit: 1
    - name: coins
      bit: 2
`
	f, err := project.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	encoded, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, []byte(raw)) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, raw)
	}

	// Verify Clone deep-copies Physics.
	cloned := f.Clone()
	cloned.Physics.Gravity2D.Y = 0
	cloned.Physics.Layers[0].Name = "mutated"
	if f.Physics.Gravity2D.Y != 980 || f.Physics.Layers[0].Name != "player" {
		t.Fatal("Clone mutated original Physics")
	}

	// Verify runtime configuration helpers.
	w2 := &physics.World{}
	f.ConfigurePhysicsWorld(w2)
	if w2.GravityX != 0 || w2.GravityY != 980 {
		t.Fatalf("ConfigurePhysicsWorld gravity = (%v, %v)", w2.GravityX, w2.GravityY)
	}
	rw2 := &physics.RigidWorld{}
	f.ConfigureRigidWorld(rw2)
	if rw2.GravityY != 980 || rw2.SolverIterations != 12 {
		t.Fatalf("ConfigureRigidWorld = gravityY:%v iters:%v", rw2.GravityY, rw2.SolverIterations)
	}
	w3 := physics3d.NewWorld()
	f.ConfigurePhysics3DWorld(w3)
	if w3.Gravity.Y != -9.81 || w3.SolverIterations != 10 {
		t.Fatalf("ConfigurePhysics3DWorld = gravity:%v iters:%v", w3.Gravity, w3.SolverIterations)
	}

	if bit, ok := f.Physics.LayerBit("coins"); !ok || bit != 2 {
		t.Fatalf("LayerBit(coins) = %d, %v", bit, ok)
	}
	m2, err := f.Physics.LayerMask2D("walls", "coins")
	if err != nil || m2 != (physics.LayerBit(1)|physics.LayerBit(2)) {
		t.Fatalf("LayerMask2D = %v, %v", m2, err)
	}
	m3, err := f.Physics.LayerMask3D("player")
	if err != nil || m3 != physics3d.LayerBit(0) {
		t.Fatalf("LayerMask3D = %v, %v", m3, err)
	}
	if _, err := f.Physics.LayerMask2D("ghost"); err == nil {
		t.Fatal("expected unknown layer error")
	}
}

func TestPhysicsValidationRejectsInvalidSettings(t *testing.T) {
	base := func() *project.File {
		return &project.File{
			Version:    project.CurrentVersion,
			Name:       "Demo",
			ID:         "dev.nsgo.demo",
			AppVersion: "0.1.0",
			Window:     project.Window{Width: 640, Height: 360},
		}
	}
	cases := []struct {
		name string
		phys *project.Physics
		want string
	}{
		{"empty layer name", &project.Physics{Layers: []project.PhysicsLayer{{Name: "", Bit: 0}}}, "empty name"},
		{"whitespace layer name", &project.Physics{Layers: []project.PhysicsLayer{{Name: "bad name", Bit: 0}}}, "no spaces"},
		{"duplicate layer name", &project.Physics{Layers: []project.PhysicsLayer{{Name: "walls", Bit: 0}, {Name: "walls", Bit: 1}}}, "duplicate physics layer"},
		{"negative bit", &project.Physics{Layers: []project.PhysicsLayer{{Name: "walls", Bit: -1}}}, "must be in [0, 31]"},
		{"bit above 31", &project.Physics{Layers: []project.PhysicsLayer{{Name: "walls", Bit: 32}}}, "must be in [0, 31]"},
		{"duplicate bit", &project.Physics{Layers: []project.PhysicsLayer{{Name: "player", Bit: 1}, {Name: "walls", Bit: 1}}}, "reuses bit 1"},
		{"negative 2d iters", &project.Physics{SolverIterations2D: -1}, "must not be negative"},
		{"negative 3d iters", &project.Physics{SolverIterations3D: -2}, "must not be negative"},
		{"nan gravity 2d", &project.Physics{Gravity2D: &project.Vec2{Y: float32(math.NaN())}}, "must be finite"},
		{"inf gravity 3d", &project.Physics{Gravity3D: &project.Vec3{Y: float32(math.Inf(1))}}, "must be finite"},
		{"ldtk rule both name and value", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Name: "Stone", Value: 1, Solid: true}}}, "cannot set both"},
		{"ldtk rule empty", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Solid: true}}}, "must set name or a positive value"},
		{"ldtk rule negative value", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Value: -1, Solid: true}}}, "must be positive"},
		{"ldtk rule whitespace name", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Name: " Stone ", Solid: true}}}, "leading/trailing whitespace"},
		{"ldtk rule duplicate name", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Name: "Stone", Solid: true}, {Name: "stone", OneWay: true}}}, "duplicate physics ldtkIntGrid name"},
		{"ldtk rule duplicate value", &project.Physics{LDtkIntGrid: []project.LDtkIntGridRule{{Value: 2, Solid: true}, {Value: 2, OneWay: true}}}, "duplicate physics ldtkIntGrid value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base()
			f.Physics = tc.phys
			err := project.Validate(f)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadAutoloadsRejectsMisspelledPhysicsLayerWithFileAndLine(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml": &fstest.MapFile{Data: []byte(`version: 1
name: Demo
id: dev.nsgo.demo
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
autoload:
  mainScene: level
  scenes:
    - name: level
      path: scenes/level.scene.yaml
`)},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(`version: 2
layers:
  - name: world
    objects:
      - id: wall
        components:
          collider2d:
            version: 1
            shape: box
            width: 32
            height: 32
            layer: walz
`)},
	}
	f, err := project.ReadFS(fsys)
	if err != nil {
		t.Fatalf("ReadFS: %v", err)
	}
	_, err = f.LoadAutoloads(fsys)
	if err == nil {
		t.Fatal("expected misspelled physics layer to fail LoadAutoloads")
	}
	var le *loaderr.Error
	if !errors.As(err, &le) || le.File != "scenes/level.scene.yaml" || le.Line != 12 {
		t.Fatalf("LoadAutoloads err = %v (loaderr=%+v), want scenes/level.scene.yaml line 12", err, le)
	}
}

func TestPhysicsLDtkIntGridProjectSettingsAndSceneResolver(t *testing.T) {
	const gameYAML = `version: 1
name: LDtk Demo
id: dev.nsgo.ldtkdemo
appVersion: 0.1.0
window:
  width: 640
  height: 360
physics:
  ldtkIntGrid:
    - name: Stone
      solid: true
    - name: Bridge
      oneWay: true
    - value: 3
      flags: 8
    - name: Foliage
`
	f, err := project.Parse([]byte(gameYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	encoded, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, []byte(gameYAML)) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, gameYAML)
	}
	cloned := f.Clone()
	cloned.Physics.LDtkIntGrid[0].Name = "Mutated"
	if f.Physics.LDtkIntGrid[0].Name != "Stone" {
		t.Fatal("Clone mutated original LDtkIntGrid slice")
	}

	const ldtkJSON = `{
		"defaultGridSize": 16,
		"defs": {
			"layers": [
				{
					"uid": 10,
					"identifier": "Terrain",
					"type": "IntGrid",
					"gridSize": 16,
					"intGridValues": [
						{"value": 1, "identifier": "Stone", "color": "#FFFFFF"},
						{"value": 2, "identifier": "Bridge", "color": "#00FF00"},
						{"value": 3, "identifier": "Hazard", "color": "#FF0000"},
						{"value": 4, "identifier": "Foliage", "color": "#008800"}
					]
				}
			]
		},
		"levels": [
			{
				"identifier": "L0",
				"uid": 0,
				"pxWid": 64,
				"pxHei": 32,
				"layerInstances": [
					{
						"__identifier": "Terrain",
						"__type": "IntGrid",
						"__cWid": 4,
						"__cHei": 2,
						"__gridSize": 16,
						"layerDefUid": 10,
						"intGridCsv": [
							0, 0, 0, 0,
							1, 2, 3, 4
						]
					}
				]
			}
		]
	}`

	tm, err := tilemap.LoadLDtkWith([]byte(ldtkJSON), "L0", f.LDtkOptions(nil))
	if err != nil {
		t.Fatalf("LoadLDtkWith: %v", err)
	}
	if tm.TileFlagsAt(0, 1) != physics.TileSolid {
		t.Fatalf("Stone flags = %v, want TileSolid", tm.TileFlagsAt(0, 1))
	}
	if tm.TileFlagsAt(1, 1) != physics.TileOneWay {
		t.Fatalf("Bridge flags = %v, want TileOneWay", tm.TileFlagsAt(1, 1))
	}
	if tm.TileFlagsAt(2, 1) != physics.TileFlags(8) {
		t.Fatalf("value 3 flags = %v, want 8", tm.TileFlagsAt(2, 1))
	}
	if tm.TileFlagsAt(3, 1) != 0 {
		t.Fatalf("Foliage flags = %v, want 0", tm.TileFlagsAt(3, 1))
	}

	// Direct physics.MoveAndSlide on the loaded LDtk *tilemap.Tilemap.
	body := physics.NewBoxBody(2, 0, 10, 10)
	body.VY = 200
	res := physics.MoveAndSlide(body, tm, 200*time.Millisecond)
	if !res.Grounded || body.Bounds().Bottom() != 16 {
		t.Fatalf("MoveAndSlide on LDtk map = grounded:%v bottom:%v, want grounded:true bottom:16", res.Grounded, body.Bounds().Bottom())
	}

	// project.Resolver supplies game.yaml's LDtkOptions automatically to sceneformat.Build.
	fsys := fstest.MapFS{
		"game.yaml":              &fstest.MapFile{Data: []byte(gameYAML)},
		"maps/world.ldtk":        &fstest.MapFile{Data: []byte(ldtkJSON)},
		"scenes/main.scene.yaml": &fstest.MapFile{Data: []byte("version: 2\nlayers:\n  - name: world\n    tilemap:\n      source: maps/world.ldtk\n      level: L0\n")},
	}
	r := project.NewResolver(fsys)
	doc, err := r.Scene("scenes/main.scene.yaml")
	if err != nil {
		t.Fatalf("Scene: %v", err)
	}
	built, err := sceneformat.Build(doc, r)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm := built.Layers[0].Map
	if sm == nil || sm.TileFlagsAt(0, 1) != physics.TileSolid || sm.TileFlagsAt(1, 1) != physics.TileOneWay || sm.TileFlagsAt(2, 1) != physics.TileFlags(8) || sm.TileFlagsAt(3, 1) != 0 {
		t.Fatalf("scene built LDtk map flags mismatch: %v", sm)
	}

	// Mutating maps returned by Resolver.LDtkOptions must not corrupt cached project rules.
	opts1 := r.LDtkOptions()
	opts1.IntGridFlags["Stone"] = 0
	opts1.IntGridFlags["Injected"] = tilemap.TileSolid
	opts1.IntGridValueFlags[3] = 0
	opts2 := r.LDtkOptions()
	if opts2.IntGridFlags["Stone"] != tilemap.TileSolid {
		t.Fatalf("cached IntGridFlags[Stone] mutated to %v, want TileSolid", opts2.IntGridFlags["Stone"])
	}
	if _, leaked := opts2.IntGridFlags["Injected"]; leaked {
		t.Fatal("injected key leaked into cached Resolver.LDtkOptions")
	}
	if opts2.IntGridValueFlags[3] != tilemap.TileFlags(8) {
		t.Fatalf("cached IntGridValueFlags[3] mutated to %v, want 8", opts2.IntGridValueFlags[3])
	}
}

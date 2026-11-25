package sceneformat_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const litSceneYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v2.json
version: 2
name: Lit Crypt
virtualWidth: 320
virtualHeight: 180
backgroundColor: '#141824'
ambientColor: rgba(12, 16, 30, 0.85)
layers:
  - name: world
    space: world
    objects:
      - id: torch
        x: 80
        "y": 90
        components:
          light2d:
            castShadows: true
            color: '#ffaa44'
            enabled: true
            falloff: 2
            intensity: 1.2
            kind: point
            radius: 160
            version: 1
      - id: spot
        x: 240
        "y": 90
        rotation: 0.5
        components:
          light2d:
            angle: 1.0
            castShadows: true
            color: '#66ccff'
            direction: 0.25
            enabled: true
            intensity: 0.9
            kind: spot
            radius: 140
            version: 1
      - id: pillar
        x: 140
        "y": 90
        components:
          occluder2d:
            enabled: true
            height: 40
            shape: rect
            version: 1
            width: 20
      - id: wedge
        x: 200
        "y": 140
        components:
          occluder2d:
            enabled: true
            points: [[-16, -12], [16, -12], [0, 14]]
            shape: polygon
            version: 1
`

func TestLights2DSceneParseRoundTripAndBuild(t *testing.T) {
	reg := sceneformat.Lights2DComponentRegistry()
	doc, err := sceneformat.ParseWith([]byte(litSceneYAML), sceneformat.ParseOptions{
		File:       "scenes/lit_crypt.scene.yaml",
		Components: reg,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	if doc.AmbientColor != "rgba(12, 16, 30, 0.85)" {
		t.Fatalf("AmbientColor = %q", doc.AmbientColor)
	}

	// Deterministic round-trip: Encode -> Parse -> Encode must match byte for byte.
	enc1, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	doc2, err := sceneformat.ParseWith(enc1, sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith round-trip: %v\n%s", err, enc1)
	}
	enc2, err := sceneformat.Encode(doc2)
	if err != nil {
		t.Fatalf("Encode round-trip: %v", err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Fatalf("scene round-trip differed:\nenc1:\n%s\nenc2:\n%s", enc1, enc2)
	}
	if !strings.Contains(string(enc1), "points: [[-16, -12], [16, -12], [0, 14]]") {
		t.Fatalf("expected canonical flow point list in encoded YAML:\n%s", enc1)
	}

	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !built.HasAmbient || built.Lights2D == nil {
		t.Fatalf("expected BuiltScene.Lights2D to be populated (HasAmbient=%v)", built.HasAmbient)
	}
	wantAmbient, _ := core.ParseColor("rgba(12, 16, 30, 0.85)")
	if built.Lights2D.Ambient != wantAmbient {
		t.Fatalf("Ambient = %#x, want %#x", built.Lights2D.Ambient, wantAmbient)
	}
	if len(built.Lights2D.Lights) != 2 {
		t.Fatalf("len(Lights) = %d, want 2", len(built.Lights2D.Lights))
	}
	if len(built.Lights2D.Occluders) != 2 {
		t.Fatalf("len(Occluders) = %d, want 2", len(built.Lights2D.Occluders))
	}

	torch := built.Lights2D.Lights[0]
	if torch.X != 80 || torch.Y != 90 || torch.Radius != 160 || !torch.CastShadows {
		t.Fatalf("torch = %+v", torch)
	}
	// Spotlight combines object rotation (0.5) + local direction (0.25) = 0.75 rad.
	spot := built.Lights2D.Lights[1]
	if math.Abs(float64(spot.ConeAngle-0.75)) > 1e-4 || math.Abs(float64(spot.ConeSpread-1.0)) > 1e-4 {
		t.Fatalf("spot ConeAngle=%v ConeSpread=%v, want 0.75 and 1.0", spot.ConeAngle, spot.ConeSpread)
	}

	// Verify shadow occlusion behind pillar.
	onlyTorch := &lights2d.Scene{
		Ambient:   built.Lights2D.Ambient,
		Lights:    []*lights2d.Light{torch},
		Occluders: built.Lights2D.Occluders,
	}
	litR, _, _ := onlyTorch.EvaluateAt(110, 90)
	shadR, _, _ := onlyTorch.EvaluateAt(180, 90)
	if litR <= shadR+0.2 {
		t.Fatalf("expected front of pillar (r=%v) to be significantly brighter than shadowed side (r=%v)", litR, shadR)
	}
}

func TestLights2DDrawEmitsSameOpsAsDirectLightsScene(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(litSceneYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	bgColor, _ := core.ParseColor("#141824")
	bounds := core.Bounds{Size: core.Size{Width: 320, Height: 180}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "built", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "direct", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				d.Save().SetFillColor(bgColor).FillRect(0, 0, 320, 180).Restore()
				built.Lights2D.Draw(d, bounds)
			}},
		}}
	})
	sameOps(t, app.Find("built").Ops, app.Find("direct").Ops)
}

func TestLights2DTilemapCollisionOccluders(t *testing.T) {
	tilesetYAML := `version: 1
name: walls
tileWidth: 16
tileHeight: 16
columns: 1
tileCount: 1
image: tiles.png
tiles:
  - id: 0
    solid: true
`
	sceneYAML := `version: 2
name: Tile Shadow
virtualWidth: 160
virtualHeight: 160
ambientColor: rgba(0, 0, 0, 0.8)
layers:
  - name: world
    space: world
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 5
      height: 5
      tilesetFile: walls.tileset.yaml
      tileLayers:
        - name: solid
          rows:
            - ". . . . ."
            - ". . 0 . ."
            - ". . 0 . ."
            - ". . 0 . ."
            - ". . . . ."
    objects:
      - id: wall_occ
        components:
          occluder2d:
            shape: tilemap
            version: 1
      - id: lamp
        x: 16
        "y": 40
        components:
          light2d:
            castShadows: true
            color: '#ffffff'
            intensity: 1
            kind: point
            radius: 120
            version: 1
`
	resolver := sceneformat.FSAssets{
		FS: fstest.MapFS{
			"walls.tileset.yaml": &fstest.MapFile{Data: []byte(tilesetYAML)},
		},
	}
	doc, err := sceneformat.ParseWith([]byte(sceneYAML), sceneformat.ParseOptions{
		Components: sceneformat.Lights2DComponentRegistry(),
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, resolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if built.Lights2D == nil || len(built.Lights2D.Occluders) == 0 {
		t.Fatalf("expected tilemap collision occluders, got %+v", built.Lights2D)
	}
	// Light at (16, 40) is blocked by solid column x=[32..48], y=[16..64]:
	// point (24, 40) is lit, point (64, 40) is in shadow.
	frontR, _, _ := built.Lights2D.EvaluateAt(24, 40)
	backR, _, _ := built.Lights2D.EvaluateAt(64, 40)
	if frontR <= backR+0.2 {
		t.Fatalf("expected tilemap occluder to cast shadow: front=%v back=%v", frontR, backR)
	}
}

func TestLights2DValidationAndInheritance(t *testing.T) {
	// Invalid ambientColor is refused with exact field path.
	if _, err := sceneformat.Parse([]byte("version: 2\nambientColor: not-a-color\n")); err == nil || !strings.Contains(err.Error(), "ambientColor") {
		t.Fatalf("expected ambientColor validation error, got %v", err)
	}
	// Invalid light2d color fails Build.
	badLight := `version: 2
layers:
  - name: main
    objects:
      - id: lamp
        components:
          light2d:
            color: nope
            version: 1
`
	doc, err := sceneformat.Parse([]byte(badLight))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := sceneformat.Build(doc, nil); err == nil || !strings.Contains(err.Error(), "color") {
		t.Fatalf("expected Build to reject invalid light2d color, got %v", err)
	}

	// Inheritance carries AmbientColor from base and lets derived override it.
	fsScenes := sceneformat.FSScenes{
		FS: fstest.MapFS{
			"base.scene.yaml": &fstest.MapFile{Data: []byte("version: 2\nambientColor: '#0a0c18d0'\nlayers:\n  - name: main\n")},
		},
	}
	derivedKeep, err := sceneformat.Parse([]byte("version: 2\ninherits: base.scene.yaml\n"))
	if err != nil {
		t.Fatal(err)
	}
	resKeep, err := sceneformat.Resolve(derivedKeep, fsScenes)
	if err != nil {
		t.Fatal(err)
	}
	if resKeep.AmbientColor != "#0a0c18d0" {
		t.Fatalf("inherited AmbientColor = %q, want #0a0c18d0", resKeep.AmbientColor)
	}
	derivedOverride, err := sceneformat.Parse([]byte("version: 2\ninherits: base.scene.yaml\nambientColor: '#112233cc'\n"))
	if err != nil {
		t.Fatal(err)
	}
	resOver, err := sceneformat.Resolve(derivedOverride, fsScenes)
	if err != nil {
		t.Fatal(err)
	}
	if resOver.AmbientColor != "#112233cc" {
		t.Fatalf("overridden AmbientColor = %q, want #112233cc", resOver.AmbientColor)
	}
}

func TestSyncLights2DPreservesLightComposite(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(litSceneYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := built.Lights2D.EffectiveLightComposite(); got != core.CompositeLighter {
		t.Fatalf("initial EffectiveLightComposite() = %v, want CompositeLighter", got)
	}

	// Default unset stays default across SyncLights2D.
	if err := built.SyncLights2D(); err != nil {
		t.Fatalf("SyncLights2D (default): %v", err)
	}
	if built.Lights2D.LightComposite != 0 || built.Lights2D.EffectiveLightComposite() != core.CompositeLighter {
		t.Fatalf("after default SyncLights2D: field=%v effective=%v, want 0 / CompositeLighter",
			built.Lights2D.LightComposite, built.Lights2D.EffectiveLightComposite())
	}

	// Explicit SetLightComposite(CompositeSourceOver) survives SyncLights2D.
	built.Lights2D.SetLightComposite(core.CompositeSourceOver)
	if err := built.SyncLights2D(); err != nil {
		t.Fatalf("SyncLights2D (SourceOver): %v", err)
	}
	if got := built.Lights2D.EffectiveLightComposite(); got != core.CompositeSourceOver {
		t.Fatalf("after SourceOver SyncLights2D: EffectiveLightComposite() = %v, want CompositeSourceOver", got)
	}

	// Direct non-zero assignment survives SyncLights2D.
	built.Lights2D.LightComposite = core.CompositeScreen
	if err := built.SyncLights2D(); err != nil {
		t.Fatalf("SyncLights2D (Screen): %v", err)
	}
	if got := built.Lights2D.EffectiveLightComposite(); got != core.CompositeScreen {
		t.Fatalf("after Screen SyncLights2D: EffectiveLightComposite() = %v, want CompositeScreen", got)
	}
}

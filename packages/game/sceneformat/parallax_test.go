package sceneformat_test

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestParallaxLayerHalfSpeedMatchesScenePackage(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Parallax Parity",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Camera: &sceneformat.CameraDoc{
			X:    200,
			Y:    100,
			Zoom: sceneformat.Float32(2),
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:      "sky",
				Space:     sceneformat.SpaceWorld,
				Order:     0,
				ParallaxX: sceneformat.Float32(0),
				ParallaxY: sceneformat.Float32(0),
				Objects: []sceneformat.ObjectDoc{
					{ID: "sun", X: 10, Y: 12, Sprite: &sceneformat.SpriteRef{Atlas: "ui", Frame: "heart"}},
				},
			},
			{
				Name:      "hills",
				Space:     sceneformat.SpaceWorld,
				Order:     1,
				ParallaxX: sceneformat.Float32(0.5),
				ParallaxY: sceneformat.Float32(0.25),
				Objects: []sceneformat.ObjectDoc{
					{ID: "hill", X: 100, Y: 40, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
				},
			},
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Order: 2,
				Objects: []sceneformat.ObjectDoc{
					{ID: "hero", X: 180, Y: 90, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
				},
			},
			{
				Name:      "foreground",
				Space:     sceneformat.SpaceWorld,
				Order:     3,
				ParallaxX: sceneformat.Float32(1.5),
				ParallaxY: sceneformat.Float32(1.5),
				Objects: []sceneformat.ObjectDoc{
					{ID: "pillar", X: 260, Y: 120, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "torch"}},
				},
			},
			{
				Name:      "hud",
				Space:     sceneformat.SpaceScreen,
				Order:     4,
				ParallaxX: sceneformat.Float32(0.5), // ignored on screen space
				Objects: []sceneformat.ObjectDoc{
					{ID: "heart", X: 8, Y: 8, Sprite: &sceneformat.SpriteRef{Atlas: "ui", Frame: "heart"}},
				},
			},
		},
	}

	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Build an equivalent runtime scene.Scene using package scene to verify exact visual parity.
	cam := &tilemap.Camera{X: 200, Y: 100, Zoom: 2, Width: 320, Height: 180}
	sc := scene.NewScene(cam)

	addSpriteLayer := func(name string, order int, space scene.Space, px, py float32, atlas *sprite.Atlas, frame string, x, y float32) {
		var l *scene.Layer
		if space == scene.SpaceWorld {
			l = scene.NewParallaxLayer(name, order, px, py)
		} else {
			l = scene.NewLayer(name, space, order)
		}
		sp := sprite.NewSprite(atlas.Frames[frame])
		sp.X, sp.Y = x, y
		l.AddBatch(&sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&sp}})
		sc.AddLayer(l)
	}
	addSpriteLayer("sky", 0, scene.SpaceWorld, 0, 0, uiAtlas, "heart", 10, 12)
	addSpriteLayer("hills", 1, scene.SpaceWorld, 0.5, 0.25, actorsAtlas, "hero", 100, 40)
	addSpriteLayer("world", 2, scene.SpaceWorld, 1, 1, actorsAtlas, "hero", 180, 90)
	addSpriteLayer("foreground", 3, scene.SpaceWorld, 1.5, 1.5, actorsAtlas, "torch", 260, 120)
	addSpriteLayer("hud", 4, scene.SpaceScreen, 1, 1, uiAtlas, "heart", 8, 8)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "sceneformat", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				sc.Draw(d)
			}},
		}}
	})
	gametest.AssertSameImages(t, app.Find("sceneformat").Ops, app.Find("scene").Ops)

	// Pan camera +100 in X at zoom 1 and verify parallaxX: 0.5 scrolls at exact half speed (-50 vs -100).
	built.Camera.Zoom = sceneformat.Float32(1)
	built.Camera.X = 100
	built.Camera.Y = 90
	d1 := &core.Drawing{}
	built.Draw(d1)
	built.Camera.X = 200
	d2 := &core.Drawing{}
	built.Draw(d2)

	imgs1 := gametest.ImageDraws(d1.Ops)
	imgs2 := gametest.ImageDraws(d2.Ops)
	if len(imgs1) != 5 || len(imgs2) != 5 {
		t.Fatalf("drawn images = %d and %d, want 5", len(imgs1), len(imgs2))
	}
	wantDX := []float32{0, -50, -100, -150, 0}
	for i, want := range wantDX {
		got := float32(imgs2[i].Corners[0].X - imgs1[i].Corners[0].X)
		if math.Abs(float64(got-want)) > 1e-3 {
			t.Errorf("layer %d delta X = %v, want %v", i, got, want)
		}
	}
}

func TestParallaxRepeatLayerTiling(t *testing.T) {
	// Camera at X=260, Y=50, Zoom=1, viewport 200x100.
	// ParallaxX=0.5 -> effective cam X=130 -> visible X=[30, 230].
	// RepeatX=true, RepeatWidth=100 -> cols 0, 1, 2 cover [0..300) (3 draws).
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  200,
		VirtualHeight: 100,
		Camera:        &sceneformat.CameraDoc{X: 260, Y: 50},
		Layers: []sceneformat.LayerDoc{
			{
				Name:        "clouds",
				Space:       sceneformat.SpaceWorld,
				ParallaxX:   sceneformat.Float32(0.5),
				RepeatX:     true,
				RepeatWidth: 100,
				Objects: []sceneformat.ObjectDoc{
					{ID: "cloud", X: 10, Y: 20, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
				},
			},
		},
	}
	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	d := &core.Drawing{}
	built.Draw(d)
	imgs := gametest.ImageDraws(d.Ops)
	if len(imgs) != 3 {
		t.Fatalf("repeating layer drew %d copies, want 3", len(imgs))
	}
	// Expected screen X: 100 + (col*100 + 10 - 130) = -20, 80, 180.
	wantX := []float32{-20, 80, 180}
	for i, wx := range wantX {
		got := float32(imgs[i].Corners[0].X)
		if math.Abs(float64(got-wx)) > 1e-3 {
			t.Errorf("repeat copy %d drawn at X=%v, want %v", i, got, wx)
		}
	}

	// Omitted RepeatWidth falls back to tilemap pixel width, then scene VirtualWidth.
	tmDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  100,
		VirtualHeight: 100,
		Camera:        &sceneformat.CameraDoc{X: 0, Y: 0},
		Layers: []sceneformat.LayerDoc{
			{
				Name:    "tiled_bg",
				RepeatX: true,
				RepeatY: true,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth: 16, TileHeight: 16, Width: 2, Height: 2,
					Tileset: "cave_tiles", Data: []int{1, 0, 0, 0},
				},
			},
			{
				Name:    "virtual_bg",
				RepeatX: true,
				RepeatY: true,
				Objects: []sceneformat.ObjectDoc{
					{ID: "dot", X: 5, Y: 5, Sprite: &sceneformat.SpriteRef{Atlas: "ui", Frame: "heart"}},
				},
			},
		},
	}
	tmBuilt, err := sceneformat.Build(tmDoc, caveResolver)
	if err != nil {
		t.Fatalf("Build fallback repeat: %v", err)
	}
	if l := tmBuilt.Layer("tiled_bg"); l.RepeatWidth != 32 || l.RepeatHeight != 32 {
		t.Fatalf("tiled_bg repeat period = (%v, %v), want (32, 32)", l.RepeatWidth, l.RepeatHeight)
	}
	if l := tmBuilt.Layer("virtual_bg"); l.RepeatWidth != 100 || l.RepeatHeight != 100 {
		t.Fatalf("virtual_bg repeat period = (%v, %v), want (100, 100)", l.RepeatWidth, l.RepeatHeight)
	}
	// Camera (0, 0) on 100x100 -> visible [-50, 50] x [-50, 50].
	// For virtual_bg (period 100): cols -1, 0 and rows -1, 0 -> 4 draws.
	d2 := &core.Drawing{}
	tmBuilt.Layer("tiled_bg").Visible = false
	tmBuilt.Draw(d2)
	if got := len(gametest.ImageDraws(d2.Ops)); got != 4 {
		t.Fatalf("2D repeating virtual_bg drew %d copies, want 4", got)
	}
}

func TestParallaxValidationAndVersionGating(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		path  []string
		error string
	}{
		{
			name:  "v1_parallaxX",
			yaml:  "version: 1\nlayers:\n  - name: sky\n    parallaxX: 0.5\n",
			path:  []string{"layers", "0", "parallaxX"},
			error: "need version 3",
		},
		{
			name:  "v2_parallaxY",
			yaml:  "version: 2\nlayers:\n  - name: sky\n    parallaxY: 0\n",
			path:  []string{"layers", "0", "parallaxY"},
			error: "need version 3",
		},
		{
			name:  "v2_repeatX",
			yaml:  "version: 2\nlayers:\n  - name: sky\n    repeatX: true\n",
			path:  []string{"layers", "0", "repeatX"},
			error: "need version 3",
		},
		{
			name:  "v2_repeatWidth",
			yaml:  "version: 2\nlayers:\n  - name: sky\n    repeatWidth: 64\n",
			path:  []string{"layers", "0", "repeatWidth"},
			error: "need version 3",
		},
		{
			name:  "v2_layerOverride_parallaxX",
			yaml:  "version: 2\ninherits: base.yaml\nlayerOverrides:\n  sky:\n    parallaxX: 0.5\n",
			path:  []string{"layerOverrides", "sky", "parallaxX"},
			error: "need version 3",
		},
		{
			name:  "v2_layerOverride_repeatX_false",
			yaml:  "version: 2\ninherits: base.yaml\nlayerOverrides:\n  sky:\n    repeatX: false\n",
			path:  []string{"layerOverrides", "sky", "repeatX"},
			error: "need version 3",
		},
		{
			name:  "v3_nan_parallaxX",
			yaml:  "version: 3\nlayers:\n  - name: sky\n    parallaxX: .nan\n",
			path:  []string{"layers", "0", "parallaxX"},
			error: "non-finite parallaxX",
		},
		{
			name:  "v3_inf_parallaxY",
			yaml:  "version: 3\nlayers:\n  - name: sky\n    parallaxY: .inf\n",
			path:  []string{"layers", "0", "parallaxY"},
			error: "non-finite parallaxY",
		},
		{
			name:  "v3_negative_repeatWidth",
			yaml:  "version: 3\nlayers:\n  - name: sky\n    repeatWidth: -10\n",
			path:  []string{"layers", "0", "repeatWidth"},
			error: "repeatWidth",
		},
		{
			name:  "v3_negative_repeatHeight",
			yaml:  "version: 3\nlayers:\n  - name: sky\n    repeatHeight: -1\n",
			path:  []string{"layers", "0", "repeatHeight"},
			error: "repeatHeight",
		},
		{
			name:  "v3_layerOverride_negative_repeatWidth",
			yaml:  "version: 3\ninherits: base.yaml\nlayerOverrides:\n  sky:\n    repeatWidth: -5\n",
			path:  []string{"layerOverrides", "sky", "repeatWidth"},
			error: "repeatWidth",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.error) {
				t.Fatalf("Parse error = %v, want %q", err, tc.error)
			}
			fe, ok := errors.AsType[*sceneformat.FieldError](err)
			if !ok || !slices.Equal(fe.Path, tc.path) {
				t.Fatalf("FieldError path = %v (ok=%v), want %v", fe, ok, tc.path)
			}
		})
	}
}

func TestParallaxLayerOverridesAndInheritance(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"base.yaml": `version: 3
name: Base
virtualWidth: 320
virtualHeight: 180
layers:
  - name: sky
    parallaxX: 0.25
    parallaxY: 0.25
    repeatX: true
    repeatWidth: 320
    objects:
      - id: cloud
        x: 20
        "y": 10
        sprite: {atlas: actors, frame: hero}
  - name: world
    objects:
      - id: hero
        x: 160
        "y": 90
        sprite: {atlas: actors, frame: hero}
`,
	})

	derivedYAML := `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v3.json
version: 3
inherits: base.yaml
layerOverrides:
  sky:
    parallaxX: 0.5
    parallaxY: 0
    repeatX: false
    repeatY: true
    repeatWidth: 0
    repeatHeight: 180
`
	doc, err := sceneformat.Parse([]byte(derivedYAML))
	if err != nil {
		t.Fatalf("Parse derived: %v", err)
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil || string(encoded) != derivedYAML {
		t.Fatalf("derived round-trip mismatch (err=%v):\n%s\nwant:\n%s", err, encoded, derivedYAML)
	}

	// Verify Clone deep-copies parallax/repeat override pointers.
	cloned := doc.Clone()
	*cloned.LayerOverrides["sky"].ParallaxX = 0.9
	*cloned.LayerOverrides["sky"].RepeatX = true
	*cloned.LayerOverrides["sky"].RepeatHeight = 999
	if *doc.LayerOverrides["sky"].ParallaxX != 0.5 || *doc.LayerOverrides["sky"].RepeatX != false || *doc.LayerOverrides["sky"].RepeatHeight != 180 {
		t.Fatal("mutating cloned LayerOverrides mutated source doc")
	}

	resolved, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	sky := resolved.Layers[0]
	if sky.EffectiveParallaxX() != 0.5 || sky.EffectiveParallaxY() != 0 {
		t.Fatalf("resolved sky parallax = (%v, %v), want (0.5, 0)", sky.EffectiveParallaxX(), sky.EffectiveParallaxY())
	}
	if sky.RepeatX || !sky.RepeatY || sky.RepeatWidth != 0 || sky.RepeatHeight != 180 {
		t.Fatalf("resolved sky repeat = (%v, %v, %v, %v), want (false, true, 0, 180)", sky.RepeatX, sky.RepeatY, sky.RepeatWidth, sky.RepeatHeight)
	}
	world := resolved.Layers[1]
	if world.EffectiveParallaxX() != 1 || world.EffectiveParallaxY() != 1 {
		t.Fatalf("resolved world parallax = (%v, %v), want (1, 1)", world.EffectiveParallaxX(), world.EffectiveParallaxY())
	}

	// Extending a base layer directly with parallaxX (instead of layerOverrides) is refused.
	badExtend, err := sceneformat.Parse([]byte("version: 3\ninherits: base.yaml\nlayers:\n  - name: sky\n    parallaxX: 0.5\n"))
	if err != nil {
		t.Fatalf("Parse badExtend: %v", err)
	}
	if _, err := sceneformat.Resolve(badExtend, scenes); err == nil || !strings.Contains(err.Error(), "layerOverrides") {
		t.Fatalf("Resolve extended layer with parallaxX = %v, want layerOverrides error", err)
	}
}

func TestLayerOverrideCoversEveryLayerField(t *testing.T) {
	notOverridable := map[string]bool{"name": true, "objects": true}
	lt := reflect.TypeFor[sceneformat.LayerDoc]()
	ot := reflect.TypeFor[sceneformat.LayerOverride]()
	overrideKeys := make(map[string]bool)
	for i := range ot.NumField() {
		overrideKeys[strings.Split(ot.Field(i).Tag.Get("yaml"), ",")[0]] = true
	}
	for i := range lt.NumField() {
		key := strings.Split(lt.Field(i).Tag.Get("yaml"), ",")[0]
		if notOverridable[key] != !overrideKeys[key] {
			t.Errorf("LayerDoc field %q: overridable %v, in LayerOverride %v", key, !notOverridable[key], overrideKeys[key])
		}
	}
	if len(overrideKeys) != lt.NumField()-len(notOverridable) {
		t.Errorf("LayerOverride has %d fields, want %d", len(overrideKeys), lt.NumField()-len(notOverridable))
	}
}

func TestExistingV1AndV2ScenesRoundTripUnchanged(t *testing.T) {
	for _, path := range []string{
		"testdata/fleet.scene.yaml",
		"testdata/hall.scene.yaml",
		"testdata/project/level.scene.yaml",
		"testdata/project/scenes/enemy.scene.yaml",
		"testdata/project/scenes/fast_enemy.scene.yaml",
	} {
		raw := readTestdata(t, strings.TrimPrefix(path, "testdata/"))
		doc, err := sceneformat.Parse(raw)
		if err != nil {
			t.Fatalf("%s: Parse: %v", path, err)
		}
		out, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("%s: Encode: %v", path, err)
		}
		if !bytes.Equal(out, raw) {
			t.Fatalf("%s round-trip changed:\n%s\n--- want ---\n%s", path, out, raw)
		}
	}
}

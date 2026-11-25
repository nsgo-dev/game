package sceneformat_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var (
	caveTilesAtlas = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:cave_tiles.png"},
		Frames: map[string]sprite.Frame{
			"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}
	actorsAtlas = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:actors.png"},
		Frames: map[string]sprite.Frame{
			"hero":  {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			"torch": {Name: "torch", X: 16, Y: 0, W: 8, H: 12, OffsetX: 4, OffsetY: 2, Width: 16, Height: 16},
		},
	}
	uiAtlas = &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:ui.png"},
		Frames: map[string]sprite.Frame{
			"heart": {Name: "heart", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		},
	}
	caveResolver = sceneformat.AtlasMap{
		"cave_tiles": caveTilesAtlas,
		"actors":     actorsAtlas,
		"ui":         uiAtlas,
	}
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}

func TestCaveRoomDrawsExpectedOps(t *testing.T) {
	raw := readTestdata(t, "cave_room.scene.yaml")
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify layer ordering: floor (0), actors (10), debug_overlay (50), hud (100).
	var gotLayers []string
	for _, l := range built.Layers {
		gotLayers = append(gotLayers, l.Name)
	}
	wantLayers := []string{"floor", "actors", "debug_overlay", "hud"}
	if !reflect.DeepEqual(gotLayers, wantLayers) {
		t.Fatalf("layer order = %v, want %v", gotLayers, wantLayers)
	}

	// Non-sprite logic objects are still indexed and carry their components and properties.
	exit := built.Object("exit_trigger")
	if exit == nil || exit.Sprite != nil {
		t.Fatalf("exit_trigger = %+v, want built non-sprite object", exit)
	}
	if exit.Layer != "actors" {
		t.Fatalf("exit_trigger.Layer = %q, want %q", exit.Layer, "actors")
	}
	if locked, ok := exit.Doc.Properties["locked"].(bool); !ok || locked {
		t.Fatalf("exit_trigger.Properties[locked] = %#v, want false", exit.Doc.Properties["locked"])
	}

	bgColor, err := core.ParseColor("#141824")
	if err != nil {
		t.Fatalf("ParseColor: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(320), Height: core.Pt(180)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Background fill over virtual viewport.
				d.Save().SetFillColor(bgColor).FillRect(0, 0, 320, 180).Restore()
				// Layer "floor" (order 0, world; camera is identity at center 160,90 zoom 1):
				// data: [1, 2, 2, 1,  1, 0, 0, 1]
				// The six tiles draw in one DrawAtlas.
				tilesImg := caveTilesAtlas.Image
				t1 := core.Bounds{Size: core.Size{Width: 16, Height: 16}}
				t2 := core.Bounds{Origin: core.Point{X: 16}, Size: core.Size{Width: 16, Height: 16}}
				d.DrawAtlas(tilesImg, []core.Bounds{t1, t2, t2, t1, t1, t1}, []core.RSXform{
					{SCos: 1}, {SCos: 1, TX: 16}, {SCos: 1, TX: 32}, {SCos: 1, TX: 48}, {SCos: 1, TY: 16}, {SCos: 1, TX: 48, TY: 16},
				}, nil)
				// Layer "actors" (order 10, world):
				// torch (z: 1, flipX: true, alpha: 0.75, trimmed 8x12 at offset 4,2 in 16x16 box, x: 96, y: 32)
				// draws before hero (z: 5, x: 48, y: 40, origin 8,8 -> top-left at 40, 32).
				actorsImg := actorsAtlas.Image
				d.Save().SetAlpha(0.75).Transform(-1, 0, 0, 1, 112, 32).DrawImageFrom(actorsImg, 16, 0, 8, 12, 4, 2, 8, 12).Restore()
				d.DrawImageFrom(actorsImg, 0, 0, 16, 16, 40, 32, 16, 16)
				// Layer "debug_overlay" (order 50) has visible: false and draws nothing.
				// Layer "hud" (order 100, screen): heart at (8, 8).
				d.DrawImageFrom(uiAtlas.Image, 0, 0, 8, 8, 8, 8, 8, 8)
			}},
		}}
	})

	got, want := app.Find("scene").Ops, app.Find("hand").Ops
	if len(got) == 0 {
		t.Fatal("scene drew no operations")
	}
	sameOps(t, got, want)
}

func TestCameraTransformsWorldLayersAndPreservesInterleavedAtlasZOrder(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Camera & Z Order",
		VirtualWidth:  200,
		VirtualHeight: 100,
		Camera: &sceneformat.CameraDoc{
			// Requested X=500 is clamped against bounds [0..200] with halfW = (200/2)/2 = 50 -> maxX = 150.
			X:        500,
			Y:        40,
			Zoom:     sceneformat.Float32(2),
			Rotation: math.Pi / 2,
			Bounds:   &sceneformat.BoundsDoc{X: 0, Y: 0, Width: 200, Height: 200},
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "mixed_world",
				Space: sceneformat.SpaceWorld,
				Order: 1,
				Objects: []sceneformat.ObjectDoc{
					// Three objects alternating atlases across Z: 10 (actors), 0 (actors), 5 (ui).
					// Expected draw order: z=0 (actors), z=5 (ui), z=10 (actors).
					{ID: "top_hero", X: 30, Y: 10, Z: 10, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
					{ID: "low_hero", X: 10, Y: 10, Z: 0, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
					{ID: "mid_heart", X: 20, Y: 10, Z: 5, Sprite: &sceneformat.SpriteRef{Atlas: "ui", Frame: "heart"}},
				},
			},
			{
				Name:  "hud",
				Space: sceneformat.SpaceScreen,
				Order: 2,
				Objects: []sceneformat.ObjectDoc{
					{ID: "hud_heart", X: 4, Y: 6, Sprite: &sceneformat.SpriteRef{Atlas: "ui", Frame: "heart"}},
				},
			},
		},
	}

	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built.Layer("mixed_world").Batches) != 3 {
		t.Fatalf("mixed_world batches = %d, want 3 to preserve atlas-interleaved Z order", len(built.Layer("mixed_world").Batches))
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(200), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				// World layer under clamped camera: center (100, 50), zoom 2, rot -pi/2, cam (150, 40).
				d.Save().Translate(100, 50).Scale(2, 2).Rotate(-math.Pi/2).Translate(-150, -40)
				d.DrawImageFrom(actorsAtlas.Image, 0, 0, 16, 16, 10, 10, 16, 16)
				d.DrawImageFrom(uiAtlas.Image, 0, 0, 8, 8, 20, 10, 8, 8)
				d.DrawImageFrom(actorsAtlas.Image, 0, 0, 16, 16, 30, 10, 16, 16)
				d.Restore()
				// Screen layer ignores camera completely.
				d.DrawImageFrom(uiAtlas.Image, 0, 0, 8, 8, 4, 6, 8, 8)
			}},
		}}
	})
	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)
}

func TestEncodeParseRoundTrip(t *testing.T) {
	raw := readTestdata(t, "cave_room.scene.yaml")
	doc1, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse initial: %v", err)
	}
	encoded1, err := sceneformat.Encode(doc1)
	if err != nil {
		t.Fatalf("Encode 1: %v", err)
	}
	encoded2, err := sceneformat.Encode(doc1)
	if err != nil {
		t.Fatalf("Encode 2: %v", err)
	}
	if !bytes.Equal(encoded1, encoded2) {
		t.Fatalf("Encode is not deterministic:\n--- 1 ---\n%s\n--- 2 ---\n%s", encoded1, encoded2)
	}
	if !bytes.HasPrefix(encoded1, []byte(sceneformat.SchemaComment+"\nversion: 1\n")) {
		t.Fatalf("Encode does not start with the schema comment and version:\n%s", encoded1)
	}
	doc2, err := sceneformat.Parse(encoded1)
	if err != nil {
		t.Fatalf("Parse re-encoded: %v", err)
	}
	if !reflect.DeepEqual(doc1, doc2) {
		t.Fatalf("round-trip mismatch:\ngot:  %+v\nwant: %+v", doc2, doc1)
	}

	// Verify explicit zero values (scaleX: 0, alpha: 0, visible: false) and inline/external tilemaps also round-trip identically.
	explicit := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Explicit Zeroes",
		Tilemaps: map[string]*sceneformat.TilemapDoc{
			"ext": {Source: "maps/cave.tmj"},
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:    "inline_layer",
				Space:   sceneformat.SpaceWorld,
				Visible: sceneformat.Bool(false),
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth: 16, TileHeight: 16, Width: 2, Height: 1,
					Tileset: "cave_tiles", Data: []int{1, 2},
				},
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "ghost",
						ScaleX: sceneformat.Float32(0),
						ScaleY: sceneformat.Float32(-1),
						Alpha:  sceneformat.Float32(0),
					},
				},
			},
		},
	}
	out, err := sceneformat.Encode(explicit)
	if err != nil {
		t.Fatalf("Encode explicit: %v", err)
	}
	got, err := sceneformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse explicit: %v", err)
	}
	if !reflect.DeepEqual(got, explicit) {
		t.Fatalf("explicit round-trip mismatch:\ngot:  %+v\nwant: %+v", got, explicit)
	}

	// Verify 2D YAML output never emits optional 3D keys when unset.
	for _, key := range []string{"posZ:", "rotX:", "rotY:", "rotZ:", "scaleZ:", "depth:", "mesh:"} {
		if bytes.Contains(encoded1, []byte(key)) {
			t.Fatalf("2D scene YAML unexpectedly emitted 3D key %q:\n%s", key, encoded1)
		}
	}

	// Verify 3D transform fields round-trip cleanly alongside existing 2D fields.
	doc3D := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "3D Room",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "world",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:     "crate_1",
						Name:   "Crate",
						X:      12.5,
						Y:      4.0,
						PosZ:   -18.25,
						RotX:   0.25,
						RotY:   1.5,
						RotZ:   -0.5,
						ScaleX: sceneformat.Float32(1.5),
						ScaleY: sceneformat.Float32(2.0),
						ScaleZ: sceneformat.Float32(0.75),
						Depth:  24,
						Mesh:   "box",
						Z:      3,
					},
					{
						ID:     "flat_marker",
						ScaleZ: sceneformat.Float32(0),
					},
				},
			},
		},
	}
	encoded3D, err := sceneformat.Encode(doc3D)
	if err != nil {
		t.Fatalf("Encode 3D: %v", err)
	}
	got3D, err := sceneformat.Parse(encoded3D)
	if err != nil {
		t.Fatalf("Parse 3D: %v", err)
	}
	if !reflect.DeepEqual(got3D, doc3D) {
		t.Fatalf("3D round-trip mismatch:\ngot:  %+v\nwant: %+v", got3D, doc3D)
	}
}

type externalResolver struct {
	sceneformat.AtlasMap
	maps map[string]*sceneformat.TilemapDoc
}

func (r externalResolver) Tilemap(source string) (*sceneformat.TilemapDoc, error) {
	tm, ok := r.maps[source]
	if !ok {
		return nil, fmt.Errorf("tilemap %q not found", source)
	}
	return tm, nil
}

func TestExternalTilemapResolver(t *testing.T) {
	yamlSrc := []byte(`version: 1
name: External Map
layers:
  - name: ground
    space: world
    tilemap:
      source: maps/cave.tmj
`)
	doc, err := sceneformat.Parse(yamlSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Plain AtlasMap does not implement TilemapResolver.
	if _, err := sceneformat.Build(doc, caveResolver); err == nil {
		t.Fatal("expected Build error when TilemapResolver is missing")
	}

	res := externalResolver{
		AtlasMap: caveResolver,
		maps: map[string]*sceneformat.TilemapDoc{
			"maps/cave.tmj": {
				TileWidth: 16, TileHeight: 16, Width: 2, Height: 1,
				Tileset: "cave_tiles", Data: []int{2, 1},
			},
		},
	}
	built, err := sceneformat.Build(doc, res)
	if err != nil {
		t.Fatalf("Build with TilemapResolver: %v", err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "scene", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(64), Height: core.Pt(32)}, Draw: func(d *core.Drawing, _ core.Size) {
				// Both tiles draw in one DrawAtlas.
				d.DrawAtlas(caveTilesAtlas.Image,
					[]core.Bounds{{Origin: core.Point{X: 16}, Size: core.Size{Width: 16, Height: 16}}, {Size: core.Size{Width: 16, Height: 16}}},
					[]core.RSXform{{SCos: 1}, {SCos: 1, TX: 16}}, nil)
			}},
		}}
	})
	sameOps(t, app.Find("scene").Ops, app.Find("hand").Ops)
}

func TestValidationErrors(t *testing.T) {
	// line is where the error points in the document: the field at fault, or the nearest enclosing value present.
	cases := []struct {
		name    string
		yaml    string
		wantSub string
		line    int
	}{
		{name: "empty", yaml: "   \n", wantSub: "empty", line: 0},
		{name: "bad_yaml", yaml: "version: [1, 2", wantSub: "invalid YAML", line: 1},
		{name: "unknown_field", yaml: "version: 1\nbogusField: true\n", wantSub: `unknown field "bogusField"`, line: 2},
		{name: "wrong_type", yaml: "version: 1\nlayers:\n  - name: a\n    order: high\n", wantSub: "cannot use", line: 4},
		{name: "trailing_doc", yaml: "version: 1\n---\nversion: 1\n", wantSub: "trailing", line: 2},
		{name: "missing_version", yaml: "name: No Version\n", wantSub: "unsupported version", line: 1},
		{name: "future_version", yaml: "version: 5\n", wantSub: "unsupported version", line: 1},
		{name: "half_virtual_size", yaml: "version: 1\nvirtualWidth: 320\n", wantSub: "virtualWidth", line: 2},
		{name: "negative_virtual_size", yaml: "version: 1\nvirtualWidth: -10\nvirtualHeight: 10\n", wantSub: "non-negative", line: 2},
		{name: "bad_background_color", yaml: "version: 1\nbackgroundColor: not-a-color\n", wantSub: "backgroundColor", line: 2},
		{name: "zero_camera_zoom", yaml: "version: 1\ncamera:\n  zoom: 0\n", wantSub: "zoom", line: 3},
		{name: "negative_camera_zoom", yaml: "version: 1\ncamera:\n  zoom: -1\n", wantSub: "zoom", line: 3},
		{name: "bad_camera_bounds", yaml: "version: 1\ncamera:\n  bounds:\n    width: 0\n    height: 100\n", wantSub: "bounds", line: 4},
		{name: "empty_layer_name", yaml: "version: 1\nlayers:\n  - space: world\n", wantSub: "empty name", line: 3},
		{name: "duplicate_layer_name", yaml: "version: 1\nlayers:\n  - name: bg\n  - name: bg\n", wantSub: "duplicate layer", line: 4},
		{name: "invalid_space", yaml: "version: 1\nlayers:\n  - name: bg\n    space: isometric\n", wantSub: "invalid space", line: 4},
		{name: "empty_object_id", yaml: "version: 1\nlayers:\n  - name: bg\n    objects:\n      - name: Nameless\n", wantSub: "empty id", line: 5},
		{name: "duplicate_object_id_across_layers", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: dup\n  - name: b\n    objects:\n      - id: dup\n", wantSub: "duplicate object id", line: 8},
		{name: "alpha_over_one", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        alpha: 1.5\n", wantSub: "alpha", line: 6},
		{name: "alpha_negative", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        alpha: -0.1\n", wantSub: "alpha", line: 6},
		{name: "negative_depth", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        depth: -4\n", wantSub: "depth", line: 6},
		{name: "incomplete_sprite", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        sprite:\n          atlas: actors\n", wantSub: "both atlas and frame", line: 6},
		{name: "unknown_tilemap_ref", yaml: "version: 1\nlayers:\n  - name: a\n    tilemap:\n      ref: missing\n", wantSub: "unknown tilemap", line: 5},
		{name: "top_level_tilemap_ref", yaml: "version: 1\ntilemaps:\n  m:\n    ref: other\n", wantSub: "cannot use ref", line: 4},
		{name: "tilemap_mixed_modes", yaml: "version: 1\nlayers:\n  - name: a\n    tilemap:\n      source: a.tmj\n      tileset: cave_tiles\n", wantSub: "cannot combine", line: 4},
		{name: "tilemap_bad_data_len", yaml: "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 2\n      tileset: cave_tiles\n      data: [1, 2, 3]\n", wantSub: "3 tile entries", line: 10},
		{name: "tilemap_dimensions_overflow", yaml: "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 100000000\n      height: 100000000\n      tileset: cave_tiles\n      data: []\n", wantSub: "more than", line: 7},
		{name: "tilemap_negative_gid", yaml: "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 1\n      tileset: cave_tiles\n      data: [1, -1]\n", wantSub: "negative id", line: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
			// The caller that read the file names it.
			loaderrtest.Expect(t, loaderr.WithFile(err, "level.scene.yaml"), "level.scene.yaml", tc.line)
		})
	}

	// Non-finite float transforms rejected by Validate / Encode.
	nanDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name:    "world",
			Objects: []sceneformat.ObjectDoc{{ID: "o", X: float32(math.NaN())}},
		}},
	}
	if _, err := sceneformat.Encode(nanDoc); err == nil {
		t.Fatal("Encode accepted NaN transform")
	}
	if err := sceneformat.Validate(nil); err == nil {
		t.Fatal("Validate accepted nil SceneDoc")
	}
}

func TestBuildErrors(t *testing.T) {
	t.Run("missing_atlas", func(t *testing.T) {
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{{
				Name: "a",
				Objects: []sceneformat.ObjectDoc{{
					ID:     "o",
					Sprite: &sceneformat.SpriteRef{Atlas: "nope", Frame: "hero"},
				}},
			}},
		}
		if _, err := sceneformat.Build(doc, caveResolver); err == nil || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("err = %v, want missing atlas error", err)
		}
	})

	t.Run("missing_frame", func(t *testing.T) {
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{{
				Name: "a",
				Objects: []sceneformat.ObjectDoc{{
					ID:     "o",
					Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "missing_frame"},
				}},
			}},
		}
		if _, err := sceneformat.Build(doc, caveResolver); err == nil || !strings.Contains(err.Error(), "missing_frame") {
			t.Fatalf("err = %v, want missing frame error", err)
		}
	})

	t.Run("missing_tileset_frame", func(t *testing.T) {
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{{
				Name: "floor",
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth: 16, TileHeight: 16, Width: 1, Height: 1,
					Tileset: "cave_tiles", Data: []int{99},
				},
			}},
		}
		if _, err := sceneformat.Build(doc, caveResolver); err == nil || !strings.Contains(err.Error(), `"99"`) {
			t.Fatalf("err = %v, want missing tile frame 99 error", err)
		}
	})

	t.Run("nil_resolver_with_sprites", func(t *testing.T) {
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers: []sceneformat.LayerDoc{{
				Name: "a",
				Objects: []sceneformat.ObjectDoc{{
					ID:     "o",
					Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
				}},
			}},
		}
		if _, err := sceneformat.Build(doc, nil); err == nil || !strings.Contains(err.Error(), "nil resolver") {
			t.Fatalf("err = %v, want nil resolver error", err)
		}
	})
}

// sameOps fails t unless got and want are the same operations, their numbers within float rounding.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j]) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestValidateNamesTheField(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{"version", "version: 5\n", []string{"version"}},
		{"virtual_size", "version: 1\nvirtualWidth: 320\n", []string{"virtualWidth"}},
		{"background", "version: 1\nbackgroundColor: nope\n", []string{"backgroundColor"}},
		{"zoom", "version: 1\ncamera:\n  zoom: -1\n", []string{"camera", "zoom"}},
		{"camera_rotation", "version: 1\ncamera:\n  x: 1\n  rotation: .nan\n", []string{"camera", "rotation"}},
		{"camera_bounds_y", "version: 1\ncamera:\n  bounds:\n    y: .inf\n    width: 1\n    height: 1\n", []string{"camera", "bounds", "y"}},
		{"camera_bounds_height", "version: 1\ncamera:\n  bounds:\n    width: 1\n    height: 0\n", []string{"camera", "bounds", "height"}},
		{"tilemap_table_first_key", "version: 1\ntilemaps:\n  b:\n    ref: x\n  a:\n    source: a.tmj\n    tileset: t\n", []string{"tilemaps", "a"}},
		{"layer_name", "version: 1\nlayers:\n  - name: a\n  - name: a\n", []string{"layers", "1", "name"}},
		{"space", "version: 1\nlayers:\n  - name: a\n    space: sky\n", []string{"layers", "0", "space"}},
		{"ref", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      ref: missing\n", []string{"layers", "0", "tilemap", "ref"}},
		{"tile_width", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileHeight: 16\n      width: 1\n      height: 1\n      tileset: t\n      data: [1]\n", []string{"layers", "0", "tilemap", "tileWidth"}},
		{"gid", "version: 1\nlayers:\n  - name: a\n    tilemap:\n      tileWidth: 16\n      tileHeight: 16\n      width: 2\n      height: 1\n      tileset: t\n      data: [1, -1]\n", []string{"layers", "0", "tilemap", "data", "1"}},
		{"object_id", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n      - id: o\n", []string{"layers", "0", "objects", "1", "id"}},
		{"alpha", "version: 1\nlayers:\n  - name: a\n  - name: b\n    objects:\n      - id: o\n        alpha: 2\n", []string{"layers", "1", "objects", "0", "alpha"}},
		{"sprite", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        sprite:\n          atlas: actors\n", []string{"layers", "0", "objects", "0", "sprite"}},
		{"material", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        material: ../stone.material.yaml\n", []string{"layers", "0", "objects", "0", "material"}},
		{"child_id", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        children:\n          - id: c\n          - id: o\n", []string{"layers", "0", "objects", "0", "children", "1", "id"}},
		{"child_alpha", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: o\n        children:\n          - id: c\n            alpha: 2\n", []string{"layers", "0", "objects", "0", "children", "0", "alpha"}},
		{"inherits_in_version_1", "version: 1\ninherits: base.scene.yaml\n", []string{"inherits"}},
		{"layer_override_space", "version: 2\ninherits: base.scene.yaml\nlayerOverrides:\n  a:\n    space: sky\n", []string{"layerOverrides", "a", "space"}},
		{"override_alpha", "version: 2\ninherits: base.scene.yaml\noverrides:\n  o:\n    alpha: 2\n", []string{"overrides", "o", "alpha"}},
		{"instance_scene", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: ../enemy.scene.yaml\n", []string{"layers", "0", "objects", "0", "scene"}},
		{"instance_override_depth", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: enemy.scene.yaml\n        overrides:\n          body:\n            depth: -1\n", []string{"layers", "0", "objects", "0", "overrides", "body", "depth"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			fe, ok := errors.AsType[*sceneformat.FieldError](err)
			if !ok {
				t.Fatalf("Parse error = %v, want a *FieldError", err)
			}
			if !slices.Equal(fe.Path, tc.want) {
				t.Fatalf("Path = %q, want %q (%v)", fe.Path, tc.want, err)
			}
			if fe.Error() != fe.Err.Error() || errors.Unwrap(fe) != fe.Err {
				t.Fatalf("FieldError does not carry Err's message: %q vs %q", fe.Error(), fe.Err.Error())
			}
		})
	}

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers:  []sceneformat.LayerDoc{{Name: "a", Objects: []sceneformat.ObjectDoc{{ID: "o", RotY: float32(math.Inf(1))}}}},
	}
	fe, ok := errors.AsType[*sceneformat.FieldError](sceneformat.Validate(doc))
	if !ok || !slices.Equal(fe.Path, []string{"layers", "0", "objects", "0", "rotY"}) {
		t.Fatalf("non-finite rotY: %#v", fe)
	}
	fe, ok = errors.AsType[*sceneformat.FieldError](sceneformat.Validate(nil))
	if !ok || len(fe.Path) != 0 {
		t.Fatalf("nil document: %#v", fe)
	}
}

// TestWholeNumberFloatPropertiesKeepTheirKind checks that a custom property holding a whole-number float, at the top or nested in a list or map, is written with a decimal point and reads back as a float, while ints stay ints and the encoded document is left as it was.
func TestWholeNumberFloatPropertiesKeepTheirKind(t *testing.T) {
	props := map[string]any{
		"zero":   0.0,
		"two":    float64(2),
		"neg":    float64(-3),
		"half":   0.5,
		"big":    1e21,
		"single": float32(4),
		"count":  7,
		"list":   []any{1.0, 2},
		"nested": map[string]any{"speed": 3.0},
	}
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Floats",
		VirtualWidth:  160,
		VirtualHeight: 120,
		Layers: []sceneformat.LayerDoc{{
			Name: "actors",
			Objects: []sceneformat.ObjectDoc{{
				ID:         "hero",
				Properties: props,
				Children:   []sceneformat.ObjectDoc{{ID: "child", Properties: map[string]any{"f": 1.0}}},
			}},
		}},
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{"zero: 0.0\n", "two: 2.0\n", "neg: -3.0\n", "half: 0.5\n", "big: 1e+21\n", "single: 4.0\n", "count: 7\n", "- 1.0\n", "- 2\n", "speed: 3.0\n", "f: 1.0\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("encoded scene lacks %q:\n%s", want, data)
		}
	}
	if _, ok := props["two"].(float64); !ok {
		t.Fatalf("Encode changed the document's property to %#v", props["two"])
	}
	back, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := back.Layers[0].Objects[0].Properties
	want := map[string]any{
		"zero":   0.0,
		"two":    2.0,
		"neg":    -3.0,
		"half":   0.5,
		"big":    1e21,
		"single": 4.0,
		"count":  7,
		"list":   []any{1.0, 2},
		"nested": map[string]any{"speed": 3.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("properties read back as %#v, want %#v", got, want)
	}
	if f, ok := back.Layers[0].Objects[0].Children[0].Properties["f"].(float64); !ok || f != 1 {
		t.Fatalf("child property read back as %#v", back.Layers[0].Objects[0].Children[0].Properties["f"])
	}
	again, err := sceneformat.Encode(back)
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if !bytes.Equal(again, data) {
		t.Fatalf("the reread scene encodes differently:\n%s\nwant\n%s", again, data)
	}
}

// TestWholeNumberFloatOverridePropertiesKeepTheirKind checks that the properties an override sets, on an inherited object (SceneDoc.Overrides) and on an object of an instanced scene (ObjectDoc.Overrides), keep a whole-number float's kind, a map[any]any value included.
func TestWholeNumberFloatOverridePropertiesKeepTheirKind(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:  sceneformat.CurrentVersion,
		Inherits: "scenes/base.scene.yaml",
		Overrides: map[string]sceneformat.ObjectOverride{
			"hero": {Properties: map[string]any{"speed": 2.0, "tags": map[any]any{"weight": 5.0}}},
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "actors",
			Objects: []sceneformat.ObjectDoc{{
				ID:        "slime_1",
				Scene:     "scenes/slime.scene.yaml",
				Overrides: map[string]sceneformat.ObjectOverride{"body": {Properties: map[string]any{"jump": float64(3)}}},
			}},
		}},
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{"speed: 2.0\n", "weight: 5.0\n", "jump: 3.0\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("encoded scene lacks %q:\n%s", want, data)
		}
	}
	back, err := sceneformat.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	if v, ok := back.Overrides["hero"].Properties["speed"].(float64); !ok || v != 2 {
		t.Fatalf("scene override speed read back as %#v", back.Overrides["hero"].Properties["speed"])
	}
	if v, ok := back.Overrides["hero"].Properties["tags"].(map[string]any)["weight"].(float64); !ok || v != 5 {
		t.Fatalf("scene override tags read back as %#v", back.Overrides["hero"].Properties["tags"])
	}
	if v, ok := back.Layers[0].Objects[0].Overrides["body"].Properties["jump"].(float64); !ok || v != 3 {
		t.Fatalf("instance override jump read back as %#v", back.Layers[0].Objects[0].Overrides["body"].Properties["jump"])
	}
	again, err := sceneformat.Encode(back)
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if !bytes.Equal(again, data) {
		t.Fatalf("the reread scene encodes differently:\n%s\nwant\n%s", again, data)
	}
}

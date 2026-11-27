package sprite_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func almostEq(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

func polySignedArea(pts []math2d.Vec2) float32 {
	var sum float32
	for i := range pts {
		j := (i + 1) % len(pts)
		sum += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return sum * 0.5
}

func TestLoadAtlasReadsShapesPointsAndAsepriteSlicePivots(t *testing.T) {
	data := `{
		"frames": [
			{
				"filename": "hero_0.png",
				"frame": {"x": 0, "y": 0, "w": 32, "h": 32},
				"sourceSize": {"w": 32, "h": 32},
				"duration": 100,
				"shapes": [
					[{"x": 4, "y": 4}, {"x": 28, "y": 4}, {"x": 28, "y": 28}, {"x": 4, "y": 28}]
				],
				"points": {
					"feet": {"x": 16, "y": 32}
				}
			},
			{
				"filename": "hero_1.png",
				"frame": {"x": 32, "y": 0, "w": 32, "h": 32},
				"sourceSize": {"w": 32, "h": 32},
				"duration": 100
			},
			{
				"filename": "hero_2.png",
				"frame": {"x": 64, "y": 0, "w": 32, "h": 32},
				"sourceSize": {"w": 32, "h": 32},
				"duration": 100
			}
		],
		"meta": {
			"scale": "2",
			"frameTags": [
				{"name": "fire", "from": 0, "to": 2, "direction": "forward"}
			],
			"slices": [
				{
					"name": "muzzle",
					"keys": [
						{"frame": 0, "bounds": {"x": 20, "y": 8, "w": 8, "h": 8}, "pivot": {"x": 6, "y": 4}},
						{"frame": 2, "bounds": {"x": 24, "y": 6, "w": 8, "h": 8}, "pivot": {"x": 4, "y": 2}}
					]
				}
			]
		}
	}`

	atlas, err := sprite.LoadAtlas(heroImage, []byte(data))
	if err != nil {
		t.Fatalf("LoadAtlas: %v", err)
	}

	f0 := atlas.Frames["hero_0.png"]
	if len(f0.Shapes) != 1 || len(f0.Shapes[0]) != 4 {
		t.Fatalf("f0.Shapes = %+v, want 1 quad", f0.Shapes)
	}
	// Divided by meta.scale = 2.
	if f0.Shapes[0][0] != (math2d.Vec2{X: 2, Y: 2}) || f0.Shapes[0][2] != (math2d.Vec2{X: 14, Y: 14}) {
		t.Fatalf("f0.Shapes[0] = %+v, want scaled [2..14]", f0.Shapes[0])
	}
	if p, ok := f0.Point("feet"); !ok || p != (math2d.Vec2{X: 8, Y: 16}) {
		t.Fatalf("f0 feet = %+v, %v; want (8, 16)", p, ok)
	}
	// Aseprite slice "muzzle" key 0: bounds (20,8) + pivot (6,4) = (26,12), divided by scale 2 = (13, 6).
	for _, name := range []string{"hero_0.png", "hero_1.png"} {
		if p, ok := atlas.Frames[name].Point("muzzle"); !ok || p != (math2d.Vec2{X: 13, Y: 6}) {
			t.Fatalf("%s muzzle = %+v, %v; want (13, 6)", name, p, ok)
		}
	}
	// Key 1 at frame 2: bounds (24,6) + pivot (4,2) = (28,8), divided by scale 2 = (14, 4).
	if p, ok := atlas.Frames["hero_2.png"].Point("muzzle"); !ok || p != (math2d.Vec2{X: 14, Y: 4}) {
		t.Fatalf("hero_2 muzzle = %+v, %v; want (14, 4)", p, ok)
	}
	// Animations built from meta.frameTags must carry the Aseprite slice pivot points too.
	anim := atlas.Animations["fire"]
	if p, ok := anim.Frames[2].Frame.Point("muzzle"); !ok || p != (math2d.Vec2{X: 14, Y: 4}) {
		t.Fatalf("animation frame 2 muzzle = %+v, %v; want (14, 4)", p, ok)
	}
}

func TestLoadAtlasRefusesBadShapesAndSliceKeyFrame(t *testing.T) {
	badShape := `{"frames":{"a":{"frame":{"x":0,"y":0,"w":8,"h":8},"shapes":[[{"x":0,"y":0},{"x":4,"y":0}]]}}}`
	if _, err := sprite.LoadAtlas(heroImage, []byte(badShape)); err == nil || !strings.Contains(err.Error(), "2 vertices") {
		t.Fatalf("badShape err = %v, want 2 vertices error", err)
	}
	badSlice := `{"frames":{"a":{"frame":{"x":0,"y":0,"w":8,"h":8}}},"meta":{"slices":[{"name":"muzzle","keys":[{"frame":3,"bounds":{"x":0,"y":0,"w":4,"h":4},"pivot":{"x":2,"y":2}}]}]}}`
	if _, err := sprite.LoadAtlas(heroImage, []byte(badSlice)); err == nil || !strings.Contains(err.Error(), "frame 3") {
		t.Fatalf("badSlice err = %v, want frame 3 out of range", err)
	}
}

func TestSpritePointWorldAndShapesWorldTransforms(t *testing.T) {
	fr := sprite.Frame{
		Name:   "gunner",
		W:      16,
		H:      16,
		Width:  16,
		Height: 16,
		PivotX: 8,
		PivotY: 16,
		Shapes: [][]math2d.Vec2{
			{{X: 2, Y: 2}, {X: 14, Y: 2}, {X: 14, Y: 16}, {X: 2, Y: 16}},
		},
		Points: map[string]math2d.Vec2{
			"muzzle": {X: 14, Y: 6},
			"feet":   {X: 8, Y: 16},
		},
	}

	s := sprite.NewSprite(fr)
	s.X, s.Y = 100, 200

	// Missing point returns false.
	if _, ok := s.PointWorld("nonexistent"); ok {
		t.Fatal("missing point should return ok=false")
	}

	// Default placement: muzzle local (14-8, 6-16) = (6, -10) -> (106, 190).
	p, ok := s.PointWorld("muzzle")
	if !ok || !almostEq(p.X, 106, 1e-4) || !almostEq(p.Y, 190, 1e-4) {
		t.Fatalf("unflipped muzzle = %+v, want (106, 190)", p)
	}

	// FlipX mirrors inside [0, 16]: x = 16 - 14 = 2, local (2-8, 6-16) = (-6, -10) -> (94, 190).
	s.FlipX = true
	p, ok = s.PointWorld("muzzle")
	if !ok || !almostEq(p.X, 94, 1e-4) || !almostEq(p.Y, 190, 1e-4) {
		t.Fatalf("FlipX muzzle = %+v, want (94, 190)", p)
	}
	// ShapesWorld must preserve positive CW winding even when mirrored by FlipX!
	flippedShapes := s.ShapesWorld()
	if len(flippedShapes) != 1 || polySignedArea(flippedShapes[0]) <= 0 {
		t.Fatalf("FlipX ShapesWorld winding not positive CW: %+v (area=%v)", flippedShapes, polySignedArea(flippedShapes[0]))
	}

	// FlipY mirrors vertically: y = 16 - 6 = 10, local (-6, 10-16) = (-6, -6) -> (94, 194).
	s.FlipY = true
	p, ok = s.PointWorld("muzzle")
	if !ok || !almostEq(p.X, 94, 1e-4) || !almostEq(p.Y, 194, 1e-4) {
		t.Fatalf("FlipX+FlipY muzzle = %+v, want (94, 194)", p)
	}

	// Rotation 90 degrees CW + Scale 2x (unflipped):
	// local (6, -10) * 2 = (12, -20), rotated +pi/2 CW in Y-down -> (20, 12) + (100, 200) = (120, 212).
	s.FlipX, s.FlipY = false, false
	s.ScaleX, s.ScaleY = 2, 2
	s.Rotation = float32(math.Pi / 2)
	p, ok = s.PointWorld("muzzle")
	if !ok || !almostEq(p.X, 120, 1e-3) || !almostEq(p.Y, 212, 1e-3) {
		t.Fatalf("rotated+scaled muzzle = %+v, want (120, 212)", p)
	}
	rotShapes := s.ShapesWorld()
	if len(rotShapes) != 1 || polySignedArea(rotShapes[0]) <= 0 {
		t.Fatalf("rotated ShapesWorld invalid: %+v", rotShapes)
	}
	// Area of [2..14]x[2..16] = 12*14 = 168; scaled 2x -> 672.
	if area := polySignedArea(rotShapes[0]); !almostEq(area, 672, 1e-1) {
		t.Fatalf("rotated+scaled shape area = %v, want 672", area)
	}
	_ = core.ImageSource{}
}

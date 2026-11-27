package sceneformat_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

type spriteShapeResolver struct {
	atlas *sprite.Atlas
}

func (r spriteShapeResolver) Atlas(name string) (*sprite.Atlas, error) { return r.atlas, nil }

func makeTestSpriteAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:hero.png"},
		Frames: map[string]sprite.Frame{
			// Idle frame: narrow centered box [6..10] x [0..16], origin (8, 16) -> local X in [-2, 2], Y in [-16, 0].
			"idle": {
				Name:   "idle",
				W:      16,
				H:      16,
				Width:  16,
				Height: 16,
				PivotX: 8,
				PivotY: 16,
				Shapes: [][]math2d.Vec2{
					{{X: 6, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 16}, {X: 6, Y: 16}},
				},
				Points: map[string]math2d.Vec2{
					"muzzle": {X: 14, Y: 6},
				},
			},
			// Attack frame: concave L-shape split into 2 convex parts:
			// vertical body [4..10] x [0..16], plus sword blade protruding right [10..24] x [4..8].
			"attack": {
				Name:   "attack",
				W:      24,
				H:      16,
				Width:  24,
				Height: 16,
				PivotX: 8,
				PivotY: 16,
				Shapes: [][]math2d.Vec2{
					{{X: 4, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 16}, {X: 4, Y: 16}},
					{{X: 10, Y: 4}, {X: 24, Y: 4}, {X: 24, Y: 8}, {X: 10, Y: 8}},
				},
			},
		},
	}
}

func TestCollider2DSpriteShapeFollowsFrameFlipScaleAndRotation(t *testing.T) {
	atlas := makeTestSpriteAtlas()
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:      "hero",
						X:       100,
						Y:       100,
						OriginX: 8,
						OriginY: 16,
						Sprite:  &sceneformat.SpriteRef{Atlas: "hero", Frame: "idle"},
						Components: map[string]any{
							"collider2d": map[string]any{
								"version": 1,
								"shape":   "sprite",
							},
						},
					},
				},
			},
		},
	}

	built, err := sceneformat.Build(doc, spriteShapeResolver{atlas: atlas})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, nil)
	if err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}

	// Idle frame: local X [-2..2], Y [-16..0] around (100, 100) => world [98..102] x [84..100].
	if hits := ps.World2D.QueryPoint(100, 92); len(hits) != 1 || hits[0].Tag != "hero" {
		t.Fatalf("idle center query = %v, want hero", hits)
	}
	if hits := ps.World2D.QueryPoint(112, 90); len(hits) != 0 {
		t.Fatalf("idle should not cover sword reach (112, 90), got %v", hits)
	}

	// Swap to concave L-shape attack frame (2 convex parts): sword reaches X in [102..116], Y in [88..92].
	hero := built.Objects["hero"]
	hero.Sprite.Frame = atlas.Frames["attack"]
	ps.Step(16 * time.Millisecond)

	if hits := ps.World2D.QueryPoint(112, 90); len(hits) != 1 || hits[0].Tag != "hero" {
		t.Fatalf("attack sword tip (112, 90) should hit hero, got %v", hits)
	}
	// Concave cutout below the sword blade (112, 98) must NOT hit!
	if hits := ps.World2D.QueryPoint(112, 98); len(hits) != 0 {
		t.Fatalf("attack concave cutout (112, 98) should not hit, got %v", hits)
	}

	// FlipX mirrors the 24px frame around origin (8, 16):
	// Blade X [10..24] flips to [0..14] in untrimmed box -> local [-8..6] -> world [92..106] at Y [88..92].
	// Wait: body [4..10] flips to [14..20] -> local [6..12] -> world [106..112].
	// Check: x=24 -> 24-24=0 -> 0-8=-8 -> 92; x=10 -> 24-10=14 -> 14-8=6 -> 106.
	hero.Sprite.FlipX = true
	ps.SyncSpriteColliders()
	if hits := ps.World2D.QueryPoint(94, 90); len(hits) != 1 || hits[0].Tag != "hero" {
		t.Fatalf("FlipX sword tip (94, 90) should hit hero, got %v", hits)
	}
	if hits := ps.World2D.QueryPoint(115, 90); len(hits) != 0 {
		t.Fatalf("FlipX old sword reach (115, 90) should not hit, got %v", hits)
	}

	// Scale 2x doubles the reach (unflipped): idle box [98..102] becomes [96..104] x [68..100].
	hero.Sprite.FlipX = false
	hero.Sprite.Frame = atlas.Frames["idle"]
	hero.Sprite.ScaleX = 2
	hero.Sprite.ScaleY = 2
	ps.SyncSpriteColliders()
	if hits := ps.World2D.QueryPoint(100, 72); len(hits) != 1 {
		t.Fatalf("2x scaled idle should reach (100, 72), got %v", hits)
	}

	// Rotate 90 degrees CW (at 1x): idle Y [-16..0] rotates to +X [0..16] -> world [100..116] x [98..102].
	hero.Sprite.ScaleX = 1
	hero.Sprite.ScaleY = 1
	hero.Sprite.Rotation = float32(math.Pi / 2)
	ps.SyncSpriteColliders()
	if hits := ps.World2D.QueryPoint(112, 100); len(hits) != 1 {
		t.Fatalf("rotated idle should cover (112, 100), got %v", hits)
	}
	if hits := ps.World2D.QueryPoint(100, 88); len(hits) != 0 {
		t.Fatalf("rotated idle should no longer cover (100, 88), got %v", hits)
	}
}

func TestTrigger2DSpriteShapeDeduplicatesMultiPartEvents(t *testing.T) {
	atlas := makeTestSpriteAtlas()
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:      "slash",
						X:       100,
						Y:       100,
						OriginX: 8,
						OriginY: 16,
						Sprite:  &sceneformat.SpriteRef{Atlas: "hero", Frame: "attack"},
						Components: map[string]any{
							"trigger2d": map[string]any{
								"version": 1,
								"shape":   "sprite",
							},
						},
					},
					{
						ID: "target",
						X:  102,
						Y:  90,
						Components: map[string]any{
							"collider2d": map[string]any{
								"version": 1,
								"shape":   "box",
								"width":   8.0,
								"height":  8.0,
							},
						},
					},
				},
			},
		},
	}

	built, err := sceneformat.Build(doc, spriteShapeResolver{atlas: atlas})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := sceneformat.BuildPhysics(built, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Target at (102, 90) overlaps BOTH convex parts of the attack L-shape simultaneously,
	// yet TriggerEnter and TriggerStay must each fire only once per step.
	ps.Step(16 * time.Millisecond)
	evs := ps.TriggerEvents()
	if len(evs) != 1 || evs[0].Kind != sceneformat.TriggerEnter || evs[0].BodyID != "target" {
		t.Fatalf("step 1 events = %+v, want single TriggerEnter for target", evs)
	}

	ps.Step(16 * time.Millisecond)
	evs = ps.TriggerEvents()
	if len(evs) != 1 || evs[0].Kind != sceneformat.TriggerStay {
		t.Fatalf("step 2 events = %+v, want single TriggerStay", evs)
	}

	// Swap back to idle frame (narrower, no longer overlaps moved target):
	ps.Bodies2D["target"].Shape = physics.BoxShape(physics.Box{X: 110, Y: 88, W: 6, H: 6})
	ps.Step(16 * time.Millisecond) // still overlapping blade
	built.Objects["slash"].Sprite.Frame = atlas.Frames["idle"]
	ps.Step(16 * time.Millisecond) // frame swap removes blade -> TriggerExit!
	evs = ps.TriggerEvents()
	if len(evs) != 1 || evs[0].Kind != sceneformat.TriggerExit {
		t.Fatalf("after frame swap events = %+v, want TriggerExit", evs)
	}
}

func TestCollider2DSpriteShapeRequiresSprite(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "bare",
						Components: map[string]any{
							"collider2d": map[string]any{
								"version": 1,
								"shape":   "sprite",
							},
						},
					},
				},
			},
		},
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sceneformat.BuildPhysics(built, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a sprite") {
		t.Fatalf("err = %v, want requires a sprite", err)
	}
}

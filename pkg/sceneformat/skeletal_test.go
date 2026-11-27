package sceneformat_test

import (
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/skeletal"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const testRigYAML = `version: 1
name: hero
bones:
  - {name: root}
  - {name: arm, parent: root, x: 4, y: -10, length: 12}
slots:
  - {name: arm, bone: arm}
skins:
  alt:
    arm: {frame: arm_alt, originX: 2, originY: 2}
  default:
    arm: {frame: arm, originX: 2, originY: 2}
animations:
  wave:
    duration: 0.4
    loop: true
    bones:
      arm:
        rotate:
          - {time: 0, angle: 0}
          - {time: 0.4, angle: 1}
`

func testRigAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "hero.png"},
		Frames: map[string]sprite.Frame{
			"arm":     {X: 0, Y: 0, W: 8, H: 12, Width: 8, Height: 12},
			"arm_alt": {X: 8, Y: 0, W: 8, H: 12, Width: 8, Height: 12},
			"prop":    {X: 16, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		},
	}
}

func TestSkeletalComponentBuildUpdateDrawAndSync(t *testing.T) {
	reg := sceneformat.SkeletalComponentRegistry()
	if !sceneformat.IsSkeletalComponent(sceneformat.SkeletalComponentName) {
		t.Fatal("IsSkeletalComponent(skeletal) = false")
	}

	sceneYAML := `version: 3
name: Rig Scene
virtualWidth: 320
virtualHeight: 180
layers:
  - name: actors
    objects:
      - id: runner
        x: 50
        "y": 80
        z: 5
        components:
          skeletal:
            version: 1
            rig: rigs/hero.skel.yaml
            atlas: hero
            skin: alt
            animation: wave
      - id: front
        x: 10
        "y": 10
        z: 10
        sprite: {atlas: hero, frame: prop}
`
	doc, err := sceneformat.ParseWith([]byte(sceneYAML), sceneformat.ParseOptions{
		File:       "scenes/rig.scene.yaml",
		Components: reg,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	assets := sceneformat.FSAssets{
		FS: fstest.MapFS{
			"rigs/hero.skel.yaml": &fstest.MapFile{Data: []byte(testRigYAML)},
		},
		Atlases: sceneformat.AtlasMap{
			"hero": testRigAtlas(),
		},
	}
	built, err := sceneformat.Build(doc, assets)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	bo := built.Object("runner")
	if bo == nil || bo.Skeleton == nil {
		t.Fatal("runner object has no built Skeleton")
	}
	if bo.Skeleton.Skin() != "alt" {
		t.Fatalf("Skin = %q, want alt", bo.Skeleton.Skin())
	}
	if bo.Skeleton.Animation() != "wave" {
		t.Fatalf("Animation = %q, want wave", bo.Skeleton.Animation())
	}

	// Standalone runtime skeleton must match scene-placed skeleton bone-for-bone across Update.
	rigData, err := skeletal.Load([]byte(testRigYAML))
	if err != nil {
		t.Fatal(err)
	}
	standalone := skeletal.NewSkeleton(rigData, testRigAtlas())
	standalone.X, standalone.Y = 50, 80
	if err := standalone.SetSkin("alt"); err != nil {
		t.Fatal(err)
	}
	if err := standalone.Play("wave", true); err != nil {
		t.Fatal(err)
	}

	built.Update(200 * time.Millisecond)
	standalone.Update(200 * time.Millisecond)

	gotBone, ok := bo.Skeleton.WorldBone("arm")
	if !ok {
		t.Fatal("missing arm bone")
	}
	wantBone, _ := standalone.WorldBone("arm")
	if math.Abs(float64(gotBone.X-wantBone.X)) > 1e-4 ||
		math.Abs(float64(gotBone.Y-wantBone.Y)) > 1e-4 ||
		math.Abs(float64(gotBone.Rotation-wantBone.Rotation)) > 1e-4 {
		t.Fatalf("world bone = %+v, want %+v", gotBone, wantBone)
	}

	// Draw must paint both the rig attachment and the sprite without panicking.
	var d core.Drawing
	built.Draw(&d)
	if len(d.Ops) == 0 {
		t.Fatal("BuiltScene.Draw emitted no drawing ops")
	}

	// Moving the document object and calling Sync updates the skeleton root position.
	bo.Doc.X = 120
	if !bo.Sync() {
		t.Fatal("Sync returned false")
	}
	moved, _ := bo.Skeleton.WorldBone("arm")
	if math.Abs(float64(moved.X-124)) > 1e-4 {
		t.Fatalf("after Sync arm.X = %v, want 124", moved.X)
	}
}

func TestSkeletalComponentRejectsBadSkinOrAnimation(t *testing.T) {
	assets := sceneformat.FSAssets{
		FS: fstest.MapFS{
			"rigs/hero.skel.yaml": &fstest.MapFile{Data: []byte(testRigYAML)},
		},
		Atlases: sceneformat.AtlasMap{"hero": testRigAtlas()},
	}
	cases := []struct {
		name string
		comp map[string]any
		want string
	}{
		{
			name: "unknown skin",
			comp: map[string]any{"version": 1, "rig": "rigs/hero.skel.yaml", "atlas": "hero", "skin": "missing"},
			want: `unknown skin "missing"`,
		},
		{
			name: "unknown animation",
			comp: map[string]any{"version": 1, "rig": "rigs/hero.skel.yaml", "atlas": "hero", "animation": "fly"},
			want: `unknown animation "fly"`,
		},
		{
			name: "unknown field",
			comp: map[string]any{"version": 1, "rig": "rigs/hero.skel.yaml", "bogus": true},
			want: `unknown field`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &sceneformat.SceneDoc{
				Version: sceneformat.CurrentVersion,
				Layers: []sceneformat.LayerDoc{{
					Name: "main",
					Objects: []sceneformat.ObjectDoc{{
						ID:         "hero",
						Components: map[string]any{sceneformat.SkeletalComponentName: tc.comp},
					}},
				}},
			}
			_, err := sceneformat.Build(doc, assets)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Build err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

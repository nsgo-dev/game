package sceneformat_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func TestSceneV4RoundTripAndDefaults(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Lit Room",
		Environment: &sceneformat.EnvironmentDoc{
			Sky: &sceneformat.SkyDoc{
				Kind: sceneformat.SkyHDR,
				HDR:  "env/studio.hdr",
			},
			AmbientColor:     "#223344",
			AmbientIntensity: sceneformat.Float32(0.15),
			IBLIntensity:     sceneformat.Float32(0.8),
			Fog: &sceneformat.FogDoc{
				Mode:          sceneformat.FogExp2,
				Color:         "#445566",
				Density:       0.03,
				MaxOpacity:    sceneformat.Float32(0.9),
				HeightEnabled: true,
				HeightBase:    -1,
				HeightFalloff: 0.25,
				HeightDensity: 0.05,
			},
			Tonemapping: sceneformat.TonemapAgX,
			Exposure:    sceneformat.Float32(1.25),
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:    "sun",
					RotX:  -0.8,
					Light: &sceneformat.LightDoc{Kind: sceneformat.LightDirectional, Color: "#ffeedd", Intensity: sceneformat.Float32(2.5), Shadows: true},
				},
				{
					ID:    "bulb",
					X:     1,
					Y:     2,
					PosZ:  3,
					Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Range: 6},
				},
				{
					ID: "spot",
					Light: &sceneformat.LightDoc{
						Kind:      sceneformat.LightSpot,
						Range:     10,
						InnerCone: 12,
						OuterCone: sceneformat.Float32(28),
					},
				},
				{
					ID: "cam",
					Camera: &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(55),
						Near:       sceneformat.Float32(0.2),
						Far:        sceneformat.Float32(250),
						Current:    true,
					},
				},
			},
		}},
	}

	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.HasPrefix(encoded, []byte(sceneformat.SchemaCommentV4+"\n")) {
		t.Fatalf("encoded v4 scene does not start with SchemaCommentV4:\n%s", encoded)
	}
	parsed, err := sceneformat.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reencoded, err := sceneformat.Encode(parsed)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("round-trip changed bytes:\n--- first ---\n%s\n--- second ---\n%s", encoded, reencoded)
	}

	// Verify effective defaults on nil/empty pointers.
	var nilEnv *sceneformat.EnvironmentDoc
	if nilEnv.EffectiveAmbientColor() != "#ffffff" || nilEnv.EffectiveAmbientIntensity() != 0.25 || nilEnv.EffectiveIBLIntensity() != 0 || nilEnv.EffectiveTonemapping() != sceneformat.TonemapNone || nilEnv.EffectiveExposure() != 1 {
		t.Fatalf("unexpected nil EnvironmentDoc defaults")
	}
	hdrEnv := &sceneformat.EnvironmentDoc{Sky: &sceneformat.SkyDoc{Kind: sceneformat.SkyHDR, HDR: "env/a.hdr"}}
	if hdrEnv.EffectiveIBLIntensity() != 1 {
		t.Fatalf("HDR sky default IBL intensity = %v, want 1", hdrEnv.EffectiveIBLIntensity())
	}
	var nilLight *sceneformat.LightDoc
	if nilLight.EffectiveIntensity() != 1 || nilLight.EffectiveOuterCone() != 45 || nilLight.EffectiveColor() != "#ffffff" {
		t.Fatalf("unexpected nil LightDoc defaults")
	}
	var nilCam *sceneformat.Camera3DDoc
	if nilCam.EffectiveProjection() != sceneformat.ProjectionPerspective || nilCam.EffectiveFOV() != 60 || nilCam.EffectiveOrthoHeight() != 10 || nilCam.EffectiveNear() != 0.1 || nilCam.EffectiveFar() != 1000 {
		t.Fatalf("unexpected nil Camera3DDoc defaults")
	}
}

func TestSceneV4ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"v3_environment", "version: 3\nenvironment:\n  exposure: 1\n", "environment needs version 4"},
		{"v3_light", "version: 3\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: point\n", "light needs version 4"},
		{"v3_camera", "version: 3\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          current: true\n", "camera needs version 4"},
		{"v3_override_light", "version: 3\ninherits: base.scene.yaml\noverrides:\n  l:\n    light:\n      kind: point\n", "light needs version 4"},
		{"bad_light_kind", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: area\n", "light kind"},
		{"bad_light_color", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: point\n          color: nope\n", "light color"},
		{"neg_light_intensity", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: point\n          intensity: -1\n", "intensity"},
		{"dir_light_range", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: directional\n          range: 5\n", "directional light cannot set range"},
		{"point_light_inner_cone", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: point\n          innerCone: 10\n", "point light cannot set innerCone"},
		{"point_light_outer_cone", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: point\n          outerCone: 30\n", "point light cannot set outerCone"},
		{"spot_inner_ge_outer", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: spot\n          innerCone: 45\n          outerCone: 45\n", "innerCone"},
		{"spot_outer_gt_90", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: l\n        light:\n          kind: spot\n          outerCone: 95\n", "outerCone"},
		{"bad_camera_projection", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          projection: fisheye\n", "camera projection"},
		{"persp_ortho_height", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          orthoHeight: 10\n", "perspective camera cannot set orthoHeight"},
		{"ortho_fov", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          projection: orthographic\n          fov: 60\n", "orthographic camera cannot set fov"},
		{"persp_fov_180", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          fov: 180\n", "camera fov"},
		{"persp_near_zero", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          near: 0\n", "camera near"},
		{"cam_far_le_near", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c\n        camera:\n          near: 10\n          far: 5\n", "camera far"},
		{"two_current_cameras", "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: c1\n        camera:\n          current: true\n      - id: c2\n        camera:\n          current: true\n", "second current 3D camera"},
		{"bad_sky_kind", "version: 4\nenvironment:\n  sky:\n    kind: procedural\n", "sky kind"},
		{"sky_color_on_gradient", "version: 4\nenvironment:\n  sky:\n    kind: gradient\n    color: \"#000000\"\n", "sky color is only valid"},
		{"sky_zenith_on_color", "version: 4\nenvironment:\n  sky:\n    kind: color\n    zenith: \"#000000\"\n", "sky zenith is only valid"},
		{"sky_hdr_missing_path", "version: 4\nenvironment:\n  sky:\n    kind: hdr\n", "requires hdr path"},
		{"sky_hdr_bad_ext", "version: 4\nenvironment:\n  sky:\n    kind: hdr\n    hdr: env/sky.png\n", "ending in .hdr or .pic"},
		{"sky_hdr_abs_path", "version: 4\nenvironment:\n  sky:\n    kind: hdr\n    hdr: /env/sky.hdr\n", "relative to the project root"},
		{"sky_hdr_on_color", "version: 4\nenvironment:\n  sky:\n    kind: color\n    hdr: env/sky.hdr\n", "sky hdr is only valid"},
		{"bad_fog_mode", "version: 4\nenvironment:\n  fog:\n    mode: volumetric\n", "fog mode"},
		{"linear_fog_end_le_start", "version: 4\nenvironment:\n  fog:\n    mode: linear\n    start: 20\n    end: 10\n", "greater than start"},
		{"bad_fog_max_opacity", "version: 4\nenvironment:\n  fog:\n    maxOpacity: 0\n", "maxOpacity"},
		{"bad_tonemapping", "version: 4\nenvironment:\n  tonemapping: filmic\n", "tonemapping"},
		{"neg_exposure", "version: 4\nenvironment:\n  exposure: -0.5\n", "exposure"},
		{"neg_ambient", "version: 4\nenvironment:\n  ambientIntensity: -0.1\n", "ambientIntensity"},
		{"neg_ibl", "version: 4\nenvironment:\n  iblIntensity: -0.1\n", "iblIntensity"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want substring %q", err, tc.want)
			}
		})
	}

	// Non-finite check on struct validation directly.
	nan := float32(math.NaN())
	if err := sceneformat.Validate(&sceneformat.SceneDoc{Version: 4, Environment: &sceneformat.EnvironmentDoc{Exposure: &nan}}); err == nil {
		t.Fatal("Validate accepted NaN exposure")
	}
}

func TestSceneV4ResolveInheritanceAndOverrides(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version: 4,
		Environment: &sceneformat.EnvironmentDoc{
			Sky:         &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#112233"},
			Tonemapping: sceneformat.TonemapReinhard,
		},
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "lamp", Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Range: 5}},
				{ID: "cam", Camera: &sceneformat.Camera3DDoc{FOV: sceneformat.Float32(60), Current: true}},
			},
		}},
	}
	child := &sceneformat.SceneDoc{
		Version:  4,
		Inherits: "base.scene.yaml",
		Environment: &sceneformat.EnvironmentDoc{
			Sky:         &sceneformat.SkyDoc{Kind: sceneformat.SkyGradient},
			Tonemapping: sceneformat.TonemapACES,
		},
		Overrides: map[string]sceneformat.ObjectOverride{
			"lamp": {Light: &sceneformat.LightDoc{Kind: sceneformat.LightPoint, Range: 12, Intensity: sceneformat.Float32(4)}},
			"cam":  {Camera: &sceneformat.Camera3DDoc{FOV: sceneformat.Float32(45), Current: true}},
		},
	}

	baseYAML, err := sceneformat.Encode(base)
	if err != nil {
		t.Fatalf("Encode base: %v", err)
	}
	resolved, err := sceneformat.Resolve(child, mapScenes{"base.scene.yaml": string(baseYAML)})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Environment == nil || resolved.Environment.Tonemapping != sceneformat.TonemapACES || resolved.Environment.Sky.Kind != sceneformat.SkyGradient {
		t.Fatalf("resolved environment = %+v", resolved.Environment)
	}
	if got := resolved.Layers[0].Objects[0].Light; got == nil || got.Range != 12 || got.EffectiveIntensity() != 4 {
		t.Fatalf("resolved lamp light = %+v", got)
	}
	if got := resolved.Layers[0].Objects[1].Camera; got == nil || got.EffectiveFOV() != 45 {
		t.Fatalf("resolved cam = %+v", got)
	}
}

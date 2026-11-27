package particles_test

import (
	"bytes"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/nsgo/core"
)

func sampleConfig3D() particles.Emitter3DConfig {
	return particles.Emitter3DConfig{
		Position:           math3d.Vec3{X: 1, Y: 2, Z: -3},
		Direction:          math3d.Vec3{X: 0, Y: 1, Z: 0},
		Up:                 math3d.Vec3{X: 0, Y: 0, Z: 1},
		Rotation:           math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.5),
		Space:              particles.SpaceLocal,
		Shape:              particles.Cone(1.5, 0.4),
		Billboard:          particles.BillboardStretched,
		Blend:              particles.BlendAdditive,
		StretchFactor:      0.08,
		Axis:               math3d.Vec3Up(),
		Rate:               500,
		MaxParticles:       10000,
		LifetimeMin:        300 * time.Millisecond,
		LifetimeMax:        900 * time.Millisecond,
		SpeedMin:           2,
		SpeedMax:           8,
		Spread:             0.25,
		Drag:               0.4,
		Gravity:            math3d.Vec3{Y: -9.8},
		SizeMin:            0.1,
		SizeMax:            0.35,
		InitialRotationMin: 0,
		InitialRotationMax: 3.14,
		RotationSpeedMin:   -1.5,
		RotationSpeedMax:   1.5,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.5},
			{T: 1, Value: 0},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		RotationCurve: particles.FloatCurve{
			{T: 0, Value: 0},
			{T: 1, Value: 0.5},
		},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.RGBA(0xffcc33, 1)},
			{T: 1, Color: core.RGBA(0xff2200, 0)},
		},
		Color: core.RGBA(0xff8800, 1),
		Seed:  280,
	}
}

func TestConfig3DYAMLRoundTripDeterministic(t *testing.T) {
	cfg := sampleConfig3D()

	data1, err := particles.MarshalConfig3DYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig3DYAML: %v", err)
	}
	got, err := particles.UnmarshalConfig3DYAML(data1)
	if err != nil {
		t.Fatalf("UnmarshalConfig3DYAML: %v\n%s", err, data1)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round-trip mismatch:\ngot:  %+v\nwant: %+v", got, cfg)
	}
	data2, err := particles.MarshalConfig3DYAML(got)
	if err != nil {
		t.Fatalf("second MarshalConfig3DYAML: %v", err)
	}
	if !bytes.Equal(data1, data2) {
		t.Fatalf("YAML round-trip changed bytes:\n--- first ---\n%s\n--- second ---\n%s", data1, data2)
	}

	// File and FS helpers.
	dir := t.TempDir()
	path := filepath.Join(dir, "sparks3d.yaml")
	if err := particles.SaveConfig3DFile(path, cfg); err != nil {
		t.Fatalf("SaveConfig3DFile: %v", err)
	}
	fromFile, err := particles.LoadConfig3DFile(path)
	if err != nil {
		t.Fatalf("LoadConfig3DFile: %v", err)
	}
	if !reflect.DeepEqual(fromFile, cfg) {
		t.Fatalf("LoadConfig3DFile mismatch")
	}

	fsys := fstest.MapFS{"fx/sparks3d.yaml": {Data: data1}}
	fromFS, err := particles.LoadConfig3DFS(fsys, "fx/sparks3d.yaml")
	if err != nil {
		t.Fatalf("LoadConfig3DFS: %v", err)
	}
	if !reflect.DeepEqual(fromFS, cfg) {
		t.Fatalf("LoadConfig3DFS mismatch")
	}
}

func TestConfig3DJSONRoundTripAndShapes(t *testing.T) {
	shapes := []particles.Shape3D{
		particles.Point3D(),
		particles.Sphere(2.5),
		particles.Hemisphere(1.25),
		particles.Box(1, 2, 3),
		particles.Cone(0.5, 0.3),
		particles.Ring3D(4),
	}
	for _, sh := range shapes {
		t.Run(sh.String(), func(t *testing.T) {
			cfg := particles.Emitter3DConfig{
				Shape:     sh,
				Billboard: particles.BillboardCylindrical,
				Blend:     particles.BlendPremultiplied,
				Seed:      7,
			}
			j, err := particles.MarshalConfig3DJSON(cfg)
			if err != nil {
				t.Fatalf("MarshalConfig3DJSON: %v", err)
			}
			got, err := particles.UnmarshalConfig3DJSON(j)
			if err != nil {
				t.Fatalf("UnmarshalConfig3DJSON: %v", err)
			}
			if !reflect.DeepEqual(got, cfg) {
				t.Fatalf("JSON round-trip mismatch: got %+v, want %+v", got, cfg)
			}
		})
	}
}

func TestConfig3DYAMLRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"empty", ""},
		{"missing_version", "rate: 10\n"},
		{"wrong_version", "version: 2\n"},
		{"unknown_field", "version: 1\nbogus: 123\n"},
		{"null_value", "version: 1\ncolor: #ff8800\n"},
		{"trailing_doc", "version: 1\n---\nversion: 1\n"},
		{"fraction_int", "version: 1\nmaxParticles: 12.5\n"},
		{"negative_max", "version: 1\nmaxParticles: -5\n"},
		{"negative_drag", "version: 1\ndrag: -1\n"},
		{"negative_spread", "version: 1\nspread: -0.5\n"},
		{"bad_space", "version: 1\nspace: galactic\n"},
		{"bad_billboard", "version: 1\nbillboard: planar\n"},
		{"bad_blend", "version: 1\nblend: screen\n"},
		{"bad_shape_kind", "version: 1\nshape:\n  kind: pyramid\n"},
		{"sphere_with_width", "version: 1\nshape:\n  kind: sphere\n  radius: 1\n  width: 2\n"},
		{"box_with_radius", "version: 1\nshape:\n  kind: box\n  width: 1\n  radius: 2\n"},
		{"point_with_radius", "version: 1\nshape:\n  kind: point\n  radius: 1\n"},
		{"cone_with_depth", "version: 1\nshape:\n  kind: cone\n  radius: 1\n  depth: 2\n"},
		{"negative_radius", "version: 1\nshape:\n  kind: ring\n  radius: -1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := particles.UnmarshalConfig3DYAML([]byte(tc.yaml)); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}

	// Non-finite float rejected on marshal.
	bad := particles.Emitter3DConfig{Drag: float32(math.NaN())}
	if _, err := particles.MarshalConfig3DYAML(bad); err == nil {
		t.Fatal("MarshalConfig3DYAML(NaN) succeeded, want error")
	}
}

func TestEmitter3DNewAndReloadConfig(t *testing.T) {
	cfg := sampleConfig3D()
	e := particles.NewEmitter3D(cfg)
	if !reflect.DeepEqual(e.Config(), cfg) {
		t.Fatalf("Config() mismatch after NewEmitter3D")
	}
	e.Burst(50)
	if e.Count() != 50 {
		t.Fatalf("Count = %d, want 50", e.Count())
	}

	// ReloadConfig keeps live particles (clamping if MaxParticles shrinks).
	cfg.MaxParticles = 20
	e.ReloadConfig(cfg)
	if e.Count() != 20 {
		t.Fatalf("Count after shrinking MaxParticles = %d, want 20", e.Count())
	}
}

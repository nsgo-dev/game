package particles_test

import (
	"bytes"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/nsgo/core"
)

// fireYAML is a hand-written emitter definition in the canonical form MarshalConfigYAML writes.
const fireYAML = `version: 1
x: 160
y: 120
space: local
shape:
  kind: circle
  radius: 6
rate: 45
maxParticles: 128
lifetimeMin: 300ms
lifetimeMax: 700ms
speedMin: 20
speedMax: 65
angleMin: -2.3561945
angleMax: -0.7853982
gravityY: -30
sizeCurve:
  - t: 0
    value: 0.6
  - t: 0.3
    value: 1.2
  - t: 1
    value: 0
alphaCurve:
  - t: 0
    value: 1
  - t: 1
    value: 0
colorCurve:
  - t: 0
    color: "#ffd166ff"
  - t: 1
    color: "#ef476f00"
seed: 42
z: 20
frameNames:
  - spark_0
  - spark_1
collision:
  enabled: true
  mode: bounce
  bounce: 0.5
trail:
  maxPoints: 24
  lifetime: 0.4
  width: 3
  color: "#ffeeaaff"
  textureURI: asset:trail.png
subEmitters:
  - trigger: death
    burstCount: 6
    inheritVelocity: 0.25
    config:
      space: world
      shape:
        kind: ring
        radius: 8
      lifetimeMin: 100ms
      lifetimeMax: 1m30s
      frameNames:
        - smoke
`

func TestConfigYAMLHandWrittenRoundTrip(t *testing.T) {
	cfg, err := particles.UnmarshalConfigYAML([]byte(fireYAML))
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v", err)
	}
	if cfg.Space != particles.SpaceLocal || cfg.Shape != particles.Circle(6) || cfg.Rate != 45 || cfg.MaxParticles != 128 {
		t.Fatalf("decoded config = %+v", cfg)
	}
	if cfg.LifetimeMin != 300*time.Millisecond || cfg.LifetimeMax != 700*time.Millisecond {
		t.Fatalf("lifetimes = %v, %v", cfg.LifetimeMin, cfg.LifetimeMax)
	}
	if cfg.AngleMin != float32(-math.Pi*0.75) || cfg.AngleMax != float32(-math.Pi*0.25) {
		t.Fatalf("angles = %v, %v", cfg.AngleMin, cfg.AngleMax)
	}
	if len(cfg.ColorCurve) != 2 || cfg.ColorCurve[0].Color != core.Color(0xffd166ff) || cfg.ColorCurve[1].Color != core.Color(0xef476f00) {
		t.Fatalf("color curve = %+v", cfg.ColorCurve)
	}
	if cfg.Trail == nil || cfg.Trail.Color != core.Color(0xffeeaaff) || cfg.Trail.Texture.URI != "asset:trail.png" {
		t.Fatalf("trail = %+v", cfg.Trail)
	}
	if len(cfg.SubEmitters) != 1 || cfg.SubEmitters[0].Trigger != particles.TriggerOnDeath || cfg.SubEmitters[0].Config.LifetimeMax != 90*time.Second {
		t.Fatalf("sub-emitters = %+v", cfg.SubEmitters)
	}

	out, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if string(out) != fireYAML {
		t.Fatalf("round trip changed the document:\n--- got ---\n%s--- want ---\n%s", out, fireYAML)
	}
}

func TestConfigYAMLRoundTrip(t *testing.T) {
	orig := fullTestConfig()
	data, err := particles.MarshalConfigYAML(orig)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("version: 1\n")) {
		t.Fatalf("document does not start with the version field:\n%s", data)
	}
	got, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\norig: %+v\ngot:  %+v", orig, got)
	}
	again, err := particles.MarshalConfigYAML(got)
	if err != nil {
		t.Fatalf("MarshalConfigYAML again: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("second save differs:\n%s\n---\n%s", data, again)
	}

	for _, sh := range []particles.Shape{particles.Point(), particles.Circle(12.5), particles.Ring(7.25), particles.Rect(20, 40)} {
		b, err := particles.MarshalConfigYAML(particles.Config{Shape: sh})
		if err != nil {
			t.Fatalf("MarshalConfigYAML(%+v): %v", sh, err)
		}
		back, err := particles.UnmarshalConfigYAML(b)
		if err != nil {
			t.Fatalf("UnmarshalConfigYAML(%+v): %v", sh, err)
		}
		if back.Shape != sh {
			t.Fatalf("shape round-trip = %+v, want %+v", back.Shape, sh)
		}
	}
}

func TestConfigYAMLExtremeValuesRoundTrip(t *testing.T) {
	cfg := particles.Config{
		LifetimeMin: time.Duration(math.MinInt64),
		LifetimeMax: time.Duration(math.MaxInt64),
		Seed:        math.MaxUint64,
		Rate:        math.MaxFloat32,
		SpeedMin:    math.SmallestNonzeroFloat32,
		FrameNames:  []string{"null", "~", "#hash", "true", "123", "", "a: b", "- dash", "multi\nline"},
		Trail:       &particles.TrailConfig{Color: core.Color(0x000000ff), Texture: core.ImageSource{URI: "asset:x y.png"}},
	}
	data, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	got, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(cfg, got) {
		t.Fatalf("round-trip mismatch:\norig: %+v\ngot:  %+v\n%s", cfg, got, data)
	}
}

func TestConfigYAMLFiles(t *testing.T) {
	orig := fullTestConfig()
	p := filepath.Join(t.TempDir(), "emitter.yaml")
	if err := particles.SaveConfigFile(p, orig); err != nil {
		t.Fatalf("SaveConfigFile: %v", err)
	}
	fromFile, err := particles.LoadConfigFile(p)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if !reflect.DeepEqual(orig, fromFile) {
		t.Fatal("LoadConfigFile mismatch")
	}

	memFS := fstest.MapFS{"fx/fire.yaml": &fstest.MapFile{Data: []byte(fireYAML)}}
	fromFS, err := particles.LoadConfigFS(memFS, "fx/fire.yaml")
	if err != nil {
		t.Fatalf("LoadConfigFS: %v", err)
	}
	want, err := particles.UnmarshalConfigYAML([]byte(fireYAML))
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v", err)
	}
	if !reflect.DeepEqual(want, fromFS) {
		t.Fatal("LoadConfigFS mismatch")
	}
	if _, err := particles.LoadConfigFS(nil, "fx/fire.yaml"); err == nil {
		t.Fatal("LoadConfigFS(nil) succeeded")
	}
}

func TestConfigYAMLValidationErrors(t *testing.T) {
	cases := []struct {
		name, yaml, wantSub string
	}{
		{"empty", "", "empty"},
		{"comment_only", "# nothing here\n", "empty"},
		{"syntax", "version: [1\n", "invalid YAML"},
		{"not_a_mapping", "- 1\n- 2\n", "cannot use"},
		{"missing_version", "rate: 10\n", "unsupported config version 0"},
		{"future_version", "version: 2\n", "unsupported config version 2"},
		{"unknown_field", "version: 1\nrats: 10\n", "rats"},
		{"unknown_nested_field", "version: 1\nshape:\n  kind: circle\n  radios: 4\n", "radios"},
		{"unknown_sub_field", "version: 1\nsubEmitters:\n  - trigger: death\n    config:\n      speed: 3\n", "speed"},
		{"duplicate_key", "version: 1\nrate: 1\nrate: 2\n", "already defined"},
		{"trailing_document", "version: 1\n---\nversion: 1\n", "trailing"},
		{"unquoted_color", "version: 1\ncolorCurve:\n  - t: 0\n    color: #ff8800\n", "no value"},
		{"trail_unquoted_color", "version: 1\ntrail:\n  color: #ff8800\n", "no value"},
		{"null_value", "version: 1\nrate: ~\n", "no value"},
		{"null_list_item", "version: 1\nframeNames:\n  - spark\n  -\n", "list item"},
		{"integer_color", "version: 1\ncolorCurve:\n  - t: 0\n    color: 4294967295\n", "quoted string"},
		{"named_color", "version: 1\ncolorCurve:\n  - t: 0\n    color: red\n", "must start with #"},
		{"short_color", "version: 1\ncolorCurve:\n  - t: 0\n    color: \"#fff\"\n", "#rrggbb"},
		{"bad_hex_color", "version: 1\ncolorCurve:\n  - t: 0\n    color: \"#gg0000\"\n", "invalid color"},
		{"integer_duration", "version: 1\nlifetimeMin: 500\n", "must be a string"},
		{"bad_duration", "version: 1\nlifetimeMin: soon\n", "invalid duration"},
		{"bad_space", "version: 1\nspace: hyperspace\n", "simulation space"},
		{"bad_shape", "version: 1\nshape:\n  kind: triangle\n", "shape kind"},
		{"bad_collision_mode", "version: 1\ncollision:\n  mode: explode\n", "collision mode"},
		{"bad_trigger", "version: 1\nsubEmitters:\n  - trigger: never\n", "trigger"},
		{"nan", "version: 1\nrate: .nan\n", "NaN"},
		{"overflow_float", "version: 1\nrate: 1e300\n", "NaN or Inf"},
		{"wrong_type", "version: 1\nmaxParticles: lots\n", "whole number"},
		{"fractional_version", "version: 1.9\n", "whole number"},
		{"fractional_max_particles", "version: 1\nmaxParticles: 12.7\n", "whole number"},
		{"fractional_z", "version: 1\nz: 2.5\n", "whole number"},
		{"fractional_seed", "version: 1\nseed: 3.5\n", "whole number"},
		{"negative_seed", "version: 1\nseed: -1\n", "at least 0"},
		{"fractional_burst_count", "version: 1\nsubEmitters:\n  - trigger: death\n    burstCount: 2.5\n", "whole number"},
		{"fractional_max_points", "version: 1\ntrail:\n  maxPoints: 8.5\n", "whole number"},
		{"aliased_fractional_int", "version: 1\nrate: &n 12.7\nmaxParticles: *n\n", "whole number"},
		{"sub_emitter_version", "version: 1\nsubEmitters:\n  - trigger: death\n    config:\n      version: 7\n      rate: 3\n", "only allowed at the top level"},
		{"circle_with_width", "version: 1\nshape:\n  kind: circle\n  width: 9\n", "takes radius"},
		{"ring_with_height", "version: 1\nshape:\n  kind: ring\n  radius: 4\n  height: 2\n", "takes radius"},
		{"rect_with_radius", "version: 1\nshape:\n  kind: rect\n  width: 4\n  height: 2\n  radius: 1\n", "takes width and height"},
		{"point_with_radius", "version: 1\nshape:\n  kind: point\n  radius: 3\n", "point shape"},
		{"kindless_shape_with_width", "version: 1\nshape:\n  width: 3\n", "point shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := particles.UnmarshalConfigYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("UnmarshalConfigYAML succeeded, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}

	if _, err := particles.MarshalConfigYAML(particles.Config{Rate: float32(math.Inf(1))}); err == nil {
		t.Fatal("MarshalConfigYAML accepted an infinite Rate")
	}
}

func BenchmarkUnmarshalConfigYAML(b *testing.B) {
	data := []byte(fireYAML)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := particles.UnmarshalConfigYAML(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalConfigYAML(b *testing.B) {
	cfg := fullTestConfig()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := particles.MarshalConfigYAML(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// docsYAML is the example in docs/en/particles.md and docs/zh/particles.md, kept here so the documented form stays canonical.
const docsYAML = `version: 1
x: 160
y: 120
space: world
shape:
  kind: circle
  radius: 6
rate: 45
maxParticles: 128
lifetimeMin: 300ms
lifetimeMax: 700ms
gravityY: -30
sizeCurve:
  - t: 0
    value: 0.6
  - t: 1
    value: 0
colorCurve:
  - t: 0
    color: "#ffd166ff"
  - t: 1
    color: "#ef476f00"
seed: 42
frameNames:
  - spark_0
  - spark_1
subEmitters:
  - trigger: death
    burstCount: 6
    config:
      space: world
      shape:
        kind: ring
        radius: 8
      lifetimeMin: 100ms
      lifetimeMax: 200ms
`

func TestConfigYAMLDocsExampleRoundTrip(t *testing.T) {
	cfg, err := particles.UnmarshalConfigYAML([]byte(docsYAML))
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v", err)
	}
	out, err := particles.MarshalConfigYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if string(out) != docsYAML {
		t.Fatalf("round trip changed the document:\n--- got ---\n%s--- want ---\n%s", out, docsYAML)
	}
}

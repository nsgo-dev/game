package particles_test

import (
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func fullTestConfig() particles.Config {
	return particles.Config{
		X:                  120.5,
		Y:                  -45.25,
		Rotation:           0.75,
		Space:              particles.SpaceLocal,
		Shape:              particles.Rect(32, 16),
		Rate:               60,
		MaxParticles:       512,
		LifetimeMin:        250 * time.Millisecond,
		LifetimeMax:        1250 * time.Millisecond,
		SpeedMin:           15,
		SpeedMax:           180,
		AngleMin:           -1.5,
		AngleMax:           1.5,
		GravityX:           -12,
		GravityY:           240,
		InitialRotationMin: 0.1,
		InitialRotationMax: 3.1,
		RotationSpeedMin:   -4.5,
		RotationSpeedMax:   4.5,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.25},
			{T: 0.5, Value: 1.5},
			{T: 1, Value: 0},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 0.9},
			{T: 1, Value: 0},
		},
		RotationCurve: particles.FloatCurve{
			{T: 0, Value: 0},
			{T: 1, Value: 1.25},
		},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.Color(0xff8000ff)},
			{T: 0.5, Color: core.Color(0xff0040cc)},
			{T: 1, Color: core.Color(0x10203000)},
		},
		Seed:       987654321,
		Z:          5,
		FrameNames: []string{"spark", "ember"},
		Collision: particles.CollisionConfig{
			Enabled:  true,
			Mode:     particles.CollideBounce,
			Bounce:   0.75,
			Friction: 0.2,
			Radius:   3.5,
		},
		Trail: &particles.TrailConfig{
			MaxPoints:   48,
			Lifetime:    0.6,
			MinDistance: 2.5,
			Width:       8,
			WidthCurve: particles.Curve{
				{T: 0, Value: 1},
				{T: 1, Value: 0.1},
			},
			ColorGradient: particles.ColorCurve{
				{T: 0, Color: core.Color(0xffffffff)},
				{T: 1, Color: core.Color(0x0080ff00)},
			},
			Color:   core.Color(0xffeeaaff),
			Texture: core.ImageSource{URI: "asset:trail.png"},
		},
		SubEmitters: []particles.SubEmitterRule{
			{
				Trigger:         particles.TriggerOnDeath,
				BurstCount:      6,
				InheritVelocity: 0.35,
				Config: particles.Config{
					Space:        particles.SpaceWorld,
					Shape:        particles.Ring(8),
					MaxParticles: 64,
					LifetimeMin:  100 * time.Millisecond,
					LifetimeMax:  200 * time.Millisecond,
					SpeedMin:     40,
					SpeedMax:     90,
					FrameNames:   []string{"smoke"},
					Collision: particles.CollisionConfig{
						Enabled: true,
						Mode:    particles.CollideKill,
						Radius:  1,
					},
				},
			},
			{
				Trigger:         particles.TriggerOnCollision,
				BurstCount:      3,
				InheritVelocity: -0.2,
				Config: particles.Config{
					Shape:       particles.Circle(4),
					LifetimeMin: 50 * time.Millisecond,
					LifetimeMax: 100 * time.Millisecond,
				},
			},
		},
	}
}

func TestConfigJSONRoundTrip(t *testing.T) {
	orig := fullTestConfig()

	data, err := particles.MarshalConfigJSON(orig)
	if err != nil {
		t.Fatalf("MarshalConfigJSON failed: %v", err)
	}

	// Verify explicit schema version: 1 in JSON envelope.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if v, ok := raw["version"].(float64); !ok || int(v) != particles.FormatVersion {
		t.Fatalf("version field = %v, want %d", raw["version"], particles.FormatVersion)
	}

	got, err := particles.UnmarshalConfigJSON(data)
	if err != nil {
		t.Fatalf("UnmarshalConfigJSON failed: %v", err)
	}
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\norig: %+v\ngot:  %+v", orig, got)
	}

	// Verify all shape variants round-trip cleanly.
	for _, sh := range []particles.Shape{
		particles.Point(),
		particles.Circle(12.5),
		particles.Ring(7.25),
		particles.Rect(20, 40),
	} {
		cfg := particles.Config{Shape: sh}
		b, err := particles.MarshalConfigJSON(cfg)
		if err != nil {
			t.Fatalf("MarshalConfigJSON(%+v): %v", sh, err)
		}
		back, err := particles.UnmarshalConfigJSON(b)
		if err != nil {
			t.Fatalf("UnmarshalConfigJSON(%+v): %v", sh, err)
		}
		if back.Shape != sh {
			t.Fatalf("shape round-trip = %+v, want %+v", back.Shape, sh)
		}
	}
}

func TestConfigJSONValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"empty", ""},
		{"invalid_json", "{not-json"},
		{"missing_version", `{"rate": 10}`},
		{"future_version", `{"version": 2, "rate": 10}`},
		{"negative_version", `{"version": -1}`},
		{"bad_space", `{"version": 1, "space": "hyperspace"}`},
		{"bad_shape", `{"version": 1, "shape": {"kind": "triangle"}}`},
		{"bad_collision_mode", `{"version": 1, "collision": {"mode": "explode"}}`},
		{"bad_trigger", `{"version": 1, "subEmitters": [{"trigger": "never"}]}`},
		{"bad_duration", `{"version": 1, "lifetimeMin": "soon"}`},
		{"bad_color", `{"version": 1, "colorCurve": [{"t": 0, "color": "red"}]}`},
		{"short_color_hex", `{"version": 1, "colorCurve": [{"t": 0, "color": "#fff"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := particles.UnmarshalConfigJSON([]byte(tc.json)); err == nil {
				t.Fatalf("expected error for %s (%s), got nil", tc.name, tc.json)
			}
		})
	}

	// NaN / Inf rejected on marshal.
	badCfg := particles.Config{Rate: float32(math.NaN())}
	if _, err := particles.MarshalConfigJSON(badCfg); err == nil {
		t.Fatal("expected error marshaling NaN Rate")
	}
}

func TestReloadConfigAndHotConfig(t *testing.T) {
	floor := &particles.RectCollider{X: 0, Y: 100, W: 200, H: 10}
	var anchorX, anchorY float32 = 50, 20

	initCfg := particles.Config{
		Rate:         0,
		MaxParticles: 32,
		LifetimeMin:  time.Second,
		LifetimeMax:  time.Second,
		GravityY:     10,
		SizeCurve:    particles.FloatCurve{{T: 0, Value: 1}},
		FrameNames:   []string{"spark"},
		Collision: particles.CollisionConfig{
			Enabled: true,
			Mode:    particles.CollideBounce,
			Bounce:  0.5,
		},
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "spark.yaml")
	if err := particles.SaveConfigFile(cfgPath, initCfg); err != nil {
		t.Fatalf("SaveConfigFile: %v", err)
	}

	hot, err := particles.LoadHotConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadHotConfigFile: %v", err)
	}

	e := &particles.Emitter{
		Atlas:  sheet,
		Frames: []sprite.Frame{sheet.Frames["spark"]},
	}
	e.Collision.Collider = floor
	e.Attach(func() (float32, float32) { return anchorX, anchorY })
	hot.Bind(e)

	// Spawn 3 live particles before hot-reloading.
	e.Burst(3)
	e.Update(50 * time.Millisecond)
	if e.Count() != 3 {
		t.Fatalf("Count before reload = %d, want 3", e.Count())
	}

	reloads := 0
	hot.OnReload(func(particles.Config) { reloads++ })

	// Write an updated definition to disk and invoke ReloadHook (as tooling/hotreload would).
	updated := initCfg
	updated.GravityY = 200
	updated.SizeCurve = particles.FloatCurve{{T: 0, Value: 2.5}}
	updated.Collision.Bounce = 0.9
	updated.FrameNames = []string{"ember"}
	if err := particles.SaveConfigFile(cfgPath, updated); err != nil {
		t.Fatalf("SaveConfigFile updated: %v", err)
	}

	hook := hot.ReloadHook()
	if err := hook(cfgPath); err != nil {
		t.Fatalf("ReloadHook failed: %v", err)
	}
	if hot.Version() != 2 || reloads != 1 {
		t.Fatalf("Version=%d reloads=%d, want 2 and 1", hot.Version(), reloads)
	}

	// Live particles are preserved, runtime Collider is preserved, Attach position is preserved,
	// and new curves/gravity/bounce/frames took effect immediately.
	if e.Count() != 3 {
		t.Fatalf("Count after hot reload = %d, want 3", e.Count())
	}
	if e.Collision.Collider != floor {
		t.Fatal("ReloadConfig overwrote runtime Collider with nil")
	}
	if !near(e.Collision.Bounce, 0.9) || !near(e.GravityY, 200) {
		t.Fatalf("updated fields not applied: bounce=%v gravY=%v", e.Collision.Bounce, e.GravityY)
	}
	if !near(e.X, 50) || !near(e.Y, 20) {
		t.Fatalf("ReloadConfig overwrote attached position: got (%v, %v)", e.X, e.Y)
	}
	if !near(e.Particles()[0].Size, 2.5) {
		t.Fatalf("live particle Size after curve reload = %v, want 2.5", e.Particles()[0].Size)
	}
	if len(e.Frames) != 1 || e.Frames[0].Name != "ember" {
		t.Fatalf("Frames after reload = %+v, want [ember]", e.Frames)
	}

	// A bad definition fails to reload cleanly and leaves live config intact.
	if err := hot.ReloadBytes([]byte("version: 99\n")); err == nil {
		t.Fatal("expected error on bad reload")
	}
	if hot.Version() != 2 || !near(e.GravityY, 200) {
		t.Fatal("failed reload mutated HotConfig or Emitter")
	}

	// StageBytes + Flush works for thread-safe deferred reload.
	updated2 := updated
	updated2.GravityY = 333
	b2, err := particles.MarshalConfigYAML(updated2)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if err := hot.StageBytes(b2); err != nil {
		t.Fatalf("StageBytes: %v", err)
	}
	if !near(e.GravityY, 200) {
		t.Fatal("StageBytes applied prematurely before Flush")
	}
	if !hot.Flush() || !near(e.GravityY, 333) {
		t.Fatalf("Flush did not apply staged config: gravY=%v", e.GravityY)
	}
	if hot.Flush() {
		t.Fatal("second Flush reported true with nothing pending")
	}

	hot.Unbind(e)
	updated3 := updated2
	updated3.GravityY = 999
	hot.Apply(updated3)
	if !near(e.GravityY, 333) {
		t.Fatalf("Unbound emitter was mutated to GravityY=%v", e.GravityY)
	}
}

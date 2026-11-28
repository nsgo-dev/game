package sceneformat_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func almostEq32(a, b, eps float32) bool {
	return math.Abs(float64(a-b)) <= float64(eps)
}

func fullBehaviorRegistry(t testing.TB) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.PhysicsComponentRegistry()
	if err := sceneformat.RegisterBehaviorComponents(reg); err != nil {
		t.Fatalf("RegisterBehaviorComponents: %v", err)
	}
	if err := sceneformat.RegisterTimerComponent(reg); err != nil {
		t.Fatalf("RegisterTimerComponent: %v", err)
	}
	return reg
}

func makeTestAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "test.png"},
		Frames: map[string]sprite.Frame{
			"box16": {Name: "box16", Width: 16, Height: 16},
		},
	}
}

func TestBehaviorRegistryAndValidation(t *testing.T) {
	reg := sceneformat.BehaviorComponentRegistry()
	wantNames := []string{
		"bound", "bullet", "destroy_outside", "fade", "flash",
		"move_to", "orbit", "rotate", "sine", "wrap",
	}
	if got := reg.Names(); !slices.Equal(got, wantNames) {
		t.Fatalf("Names() = %v, want %v", got, wantNames)
	}
	if got := sceneformat.BehaviorComponentNames(); !slices.Equal(got, wantNames) {
		t.Fatalf("BehaviorComponentNames() = %v, want %v", got, wantNames)
	}
	for _, n := range wantNames {
		if !sceneformat.IsBehaviorComponent(n) {
			t.Errorf("IsBehaviorComponent(%q) = false, want true", n)
		}
	}
	if sceneformat.IsBehaviorComponent("not_a_behavior") {
		t.Error("IsBehaviorComponent(not_a_behavior) = true, want false")
	}

	// Reject unknown fields, invalid version, invalid enums, and out-of-range values with file/line/column.
	badCases := []struct {
		name      string
		yaml      string
		wantComp  string
		wantField string
	}{
		{
			name:      "bullet unknown field",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: b\n        components:\n          bullet:\n            version: 1\n            bogus: 5\n",
			wantComp:  "bullet",
			wantField: "bogus",
		},
		{
			name:      "bullet wrong version",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: b\n        components:\n          bullet:\n            version: 2\n",
			wantComp:  "bullet",
			wantField: "version",
		},
		{
			name:      "sine invalid property enum",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: s\n        components:\n          sine:\n            version: 1\n            property: z\n",
			wantComp:  "sine",
			wantField: "property",
		},
		{
			name:      "sine invalid wave enum",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: s\n        components:\n          sine:\n            version: 1\n            wave: pulse\n",
			wantComp:  "sine",
			wantField: "wave",
		},
		{
			name:      "orbit negative radius",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: o\n        components:\n          orbit:\n            version: 1\n            radius: -5\n",
			wantComp:  "orbit",
			wantField: "radius",
		},
		{
			name:      "move_to negative maxSpeed",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: m\n        components:\n          move_to:\n            version: 1\n            maxSpeed: -10\n",
			wantComp:  "move_to",
			wantField: "maxSpeed",
		},
		{
			name:      "wrap invalid mode",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: w\n        components:\n          wrap:\n            version: 1\n            mode: world\n",
			wantComp:  "wrap",
			wantField: "mode",
		},
		{
			name:      "bound invalid mode",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: b\n        components:\n          bound:\n            version: 1\n            mode: center\n",
			wantComp:  "bound",
			wantField: "mode",
		},
		{
			name:      "destroy_outside negative margin",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: d\n        components:\n          destroy_outside:\n            version: 1\n            margin: -1\n",
			wantComp:  "destroy_outside",
			wantField: "margin",
		},
		{
			name:      "fade negative fadeOut",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: f\n        components:\n          fade:\n            version: 1\n            fadeOut: -0.2\n",
			wantComp:  "fade",
			wantField: "fadeOut",
		},
		{
			name:      "flash negative on",
			yaml:      "version: 1\nlayers:\n  - name: main\n    objects:\n      - id: fl\n        components:\n          flash:\n            version: 1\n            on: -0.05\n",
			wantComp:  "flash",
			wantField: "on",
		},
	}

	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.ParseWith([]byte(tc.yaml), sceneformat.ParseOptions{
				File:       "bad.scene.yaml",
				Components: reg,
			})
			var ce *sceneformat.ComponentError
			if !errors.As(err, &ce) {
				t.Fatalf("ParseWith err = %v (%T), want *ComponentError", err, err)
			}
			if ce.File != "bad.scene.yaml" || ce.Line <= 0 || ce.Component != tc.wantComp || ce.Field != tc.wantField {
				t.Fatalf("ComponentError = %+v, want comp=%q field=%q with file & line", ce, tc.wantComp, tc.wantField)
			}

			// Build also rejects invalid built-in behavior components even when Parse ran without a registry.
			doc, err := sceneformat.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Parse without registry: %v", err)
			}
			if _, err := sceneformat.Build(doc, nil); !errors.As(err, &ce) {
				t.Fatalf("Build err = %v (%T), want *ComponentError", err, err)
			}
		})
	}

	// Verify *loaderr.Error conformance on malformed YAML in a scene containing motion behaviors.
	badYAML := []byte("version: 1\nunknownKey: true\nlayers:\n  - name: main\n    objects:\n      - id: shot\n        components:\n          bullet: {version: 1, speed: 240}\n")
	_, err := sceneformat.ParseWith(badYAML, sceneformat.ParseOptions{File: "bad.scene.yaml", Components: reg})
	loaderrtest.Expect(t, err, "bad.scene.yaml", 2)
}

func TestAllBehaviorsRateIndependence(t *testing.T) {
	// Run every behavior for 1 simulated second at 60 Hz and 120 Hz and verify matching outputs.
	build := func(t *testing.T, src string) *sceneformat.BuiltScene {
		t.Helper()
		doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: fullBehaviorRegistry(t)})
		if err != nil {
			t.Fatalf("ParseWith: %v", err)
		}
		built, err := sceneformat.Build(doc, sceneformat.AtlasMap{"tex": makeTestAtlas()})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return built
	}

	stepOneSec := func(s *sceneformat.BuiltScene, hz int) {
		dt := time.Second / time.Duration(hz)
		for range hz {
			s.Step(dt)
		}
	}

	t.Run("bullet acceleration and gravity", func(t *testing.T) {
		src := `version: 1
virtualWidth: 1000
virtualHeight: 1000
layers:
  - name: main
    objects:
      - id: acc
        x: 10
        y: 20
        components:
          bullet: {version: 1, speed: 100, acceleration: 60, gravity: 0, angle: 0, setAngle: true}
      - id: grav
        x: 10
        y: 20
        components:
          bullet: {version: 1, speed: 100, acceleration: 0, gravity: 200, angle: 0, setAngle: true}
`
		s60, s120 := build(t, src), build(t, src)
		stepOneSec(s60, 60)
		stepOneSec(s120, 120)

		for _, id := range []string{"acc", "grav"} {
			a, b := s60.Object(id).Doc, s120.Object(id).Doc
			if !almostEq32(a.X, b.X, 1e-3) || !almostEq32(a.Y, b.Y, 1e-3) || !almostEq32(a.Rotation, b.Rotation, 1e-3) {
				t.Fatalf("%s 60Hz=(%v,%v,%v) vs 120Hz=(%v,%v,%v)", id, a.X, a.Y, a.Rotation, b.X, b.Y, b.Rotation)
			}
		}
		if !almostEq32(s60.Object("acc").Doc.X, 140, 1e-3) {
			t.Fatalf("acc.X = %v, want 140", s60.Object("acc").Doc.X)
		}
		if !almostEq32(s60.Object("grav").Doc.X, 110, 1e-3) || !almostEq32(s60.Object("grav").Doc.Y, 120, 1e-3) {
			t.Fatalf("grav = (%v,%v), want (110,120)", s60.Object("grav").Doc.X, s60.Object("grav").Doc.Y)
		}
	})

	t.Run("sine all properties and golden wave values", func(t *testing.T) {
		src := `version: 1
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    objects:
      - id: sx
        x: 50
        components: {sine: {version: 1, property: x, period: 2, magnitude: 10, wave: triangle, phase: 0}}
      - id: sy
        y: 100
        components: {sine: {version: 1, property: y, period: 1.5, magnitude: 6, wave: sine, phase: 0}}
      - id: sa
        components: {sine: {version: 1, property: angle, period: 4, magnitude: 90, wave: sine, phase: 0}}
      - id: ss
        components: {sine: {version: 1, property: scale, period: 2, magnitude: 0.5, wave: square, phase: 0}}
      - id: salpha
        alpha: 0.5
        components: {sine: {version: 1, property: alpha, period: 4, magnitude: 0.4, wave: sine, phase: 0}}
`
		s60, s120 := build(t, src), build(t, src)
		stepOneSec(s60, 60)
		stepOneSec(s120, 120)

		wantY := float32(100 + 6*math.Sin(2*math.Pi*(1.0/1.5)))
		if !almostEq32(s60.Object("sy").Doc.Y, wantY, 1e-4) || !almostEq32(s120.Object("sy").Doc.Y, wantY, 1e-4) {
			t.Fatalf("sy.Y 60Hz=%v 120Hz=%v want=%v", s60.Object("sy").Doc.Y, s120.Object("sy").Doc.Y, wantY)
		}
		// At t=1s, period=4 => u=0.25 => sin(pi/2)=1 => +90 deg = pi/2 rad.
		if !almostEq32(s60.Object("sa").Doc.Rotation, float32(math.Pi/2), 1e-4) || !almostEq32(s120.Object("sa").Doc.Rotation, float32(math.Pi/2), 1e-4) {
			t.Fatalf("sa.Rotation 60Hz=%v 120Hz=%v want pi/2", s60.Object("sa").Doc.Rotation, s120.Object("sa").Doc.Rotation)
		}
		// At t=1s, period=4 => u=0.25 => alpha = 0.5 + 0.4 = 0.9.
		if !almostEq32(s60.Object("salpha").Doc.EffectiveAlpha(), 0.9, 1e-4) || !almostEq32(s120.Object("salpha").Doc.EffectiveAlpha(), 0.9, 1e-4) {
			t.Fatalf("salpha 60Hz=%v 120Hz=%v want 0.9", s60.Object("salpha").Doc.EffectiveAlpha(), s120.Object("salpha").Doc.EffectiveAlpha())
		}
		// Scale and X match across rates.
		if !almostEq32(s60.Object("sx").Doc.X, s120.Object("sx").Doc.X, 1e-4) {
			t.Fatalf("sx.X 60Hz=%v vs 120Hz=%v", s60.Object("sx").Doc.X, s120.Object("sx").Doc.X)
		}
		if !almostEq32(s60.Object("ss").Doc.EffectiveScaleX(), s120.Object("ss").Doc.EffectiveScaleX(), 1e-4) {
			t.Fatalf("ss.ScaleX 60Hz=%v vs 120Hz=%v", s60.Object("ss").Doc.EffectiveScaleX(), s120.Object("ss").Doc.EffectiveScaleX())
		}
	})

	t.Run("rotate", func(t *testing.T) {
		src := `version: 1
layers:
  - name: main
    objects:
      - id: r
        components: {rotate: {version: 1, speed: 180, acceleration: 90}}
`
		s60, s120 := build(t, src), build(t, src)
		stepOneSec(s60, 60)
		stepOneSec(s120, 120)
		// 180*1 + 0.5*90*1 = 225 deg.
		want := float32(225 * math.Pi / 180)
		if !almostEq32(s60.Object("r").Doc.Rotation, want, 1e-4) || !almostEq32(s120.Object("r").Doc.Rotation, want, 1e-4) {
			t.Fatalf("rotate 60Hz=%v 120Hz=%v want=%v", s60.Object("r").Doc.Rotation, s120.Object("r").Doc.Rotation, want)
		}
	})

	t.Run("orbit around target and self", func(t *testing.T) {
		src := `version: 1
layers:
  - name: main
    objects:
      - id: hub
        x: 100
        y: 50
      - id: sat
        x: 0
        y: 0
        components: {orbit: {version: 1, target: hub, radius: 32, speed: 90}}
      - id: solo
        x: 20
        y: 30
        components: {orbit: {version: 1, target: "", radius: 10, speed: 180}}
`
		s60, s120 := build(t, src), build(t, src)
		stepOneSec(s60, 60)
		stepOneSec(s120, 120)
		if !almostEq32(s60.Object("sat").Doc.X, 100, 1e-4) || !almostEq32(s60.Object("sat").Doc.Y, 82, 1e-4) {
			t.Fatalf("sat 60Hz=(%v,%v) want (100,82)", s60.Object("sat").Doc.X, s60.Object("sat").Doc.Y)
		}
		if !almostEq32(s60.Object("sat").Doc.X, s120.Object("sat").Doc.X, 1e-4) || !almostEq32(s60.Object("sat").Doc.Y, s120.Object("sat").Doc.Y, 1e-4) {
			t.Fatalf("sat rate mismatch")
		}
		if !almostEq32(s60.Object("solo").Doc.X, 10, 1e-4) || !almostEq32(s60.Object("solo").Doc.Y, 30, 1e-4) {
			t.Fatalf("solo 60Hz=(%v,%v) want (10,30)", s60.Object("solo").Doc.X, s60.Object("solo").Doc.Y)
		}
	})

	t.Run("move_to, wrap, bound, destroy_outside, fade, flash", func(t *testing.T) {
		src := `version: 1
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    objects:
      - id: mover
        x: 0
        y: 0
        components: {move_to: {version: 1, maxSpeed: 120, acceleration: 240, targetX: 200, targetY: 0, active: true}}
      - id: wrapper
        x: 280
        y: 50
        components:
          bullet: {version: 1, speed: 100, angle: 0, setAngle: false}
          wrap: {version: 1, mode: scene}
      - id: bounded
        x: 10
        y: 50
        sprite: {atlas: tex, frame: box16}
        components:
          bullet: {version: 1, speed: 500, angle: 0, setAngle: false}
          bound: {version: 1, mode: edge}
      - id: fader
        components: {fade: {version: 1, fadeIn: 0, wait: 0.5, fadeOut: 1.0, destroy: false}}
`
		s60, s120 := build(t, src), build(t, src)
		stepOneSec(s60, 60)
		stepOneSec(s120, 120)

		if !almostEq32(s60.Object("mover").Doc.X, 90, 1e-3) || !almostEq32(s120.Object("mover").Doc.X, 90, 1e-3) {
			t.Fatalf("mover.X 60Hz=%v 120Hz=%v want 90", s60.Object("mover").Doc.X, s120.Object("mover").Doc.X)
		}
		// 280 + 100 = 380 -> wraps modulo 320 to 60.
		if !almostEq32(s60.Object("wrapper").Doc.X, 60, 1e-3) || !almostEq32(s120.Object("wrapper").Doc.X, 60, 1e-3) {
			t.Fatalf("wrapper.X 60Hz=%v 120Hz=%v want 60", s60.Object("wrapper").Doc.X, s120.Object("wrapper").Doc.X)
		}
		// box16 with origin (0,0): right edge at x+16 <= 320 => x <= 304.
		if !almostEq32(s60.Object("bounded").Doc.X, 304, 1e-3) || !almostEq32(s120.Object("bounded").Doc.X, 304, 1e-3) {
			t.Fatalf("bounded.X 60Hz=%v 120Hz=%v want 304", s60.Object("bounded").Doc.X, s120.Object("bounded").Doc.X)
		}
		// Fade: wait 0.5, fadeOut 1.0 => at t=1.0s, halfway through fadeOut => alpha 0.5.
		if !almostEq32(s60.Object("fader").Doc.EffectiveAlpha(), 0.5, 1e-3) || !almostEq32(s120.Object("fader").Doc.EffectiveAlpha(), 0.5, 1e-3) {
			t.Fatalf("fader.Alpha 60Hz=%v 120Hz=%v want 0.5", s60.Object("fader").Doc.EffectiveAlpha(), s120.Object("fader").Doc.EffectiveAlpha())
		}
	})
}

func TestBulletBounceOffPhysicsAndTileSolids(t *testing.T) {
	t.Run("physics collider2d wall", func(t *testing.T) {
		src := `version: 1
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    objects:
      - id: wall
        x: 108
        y: 50
        components:
          body2d: {version: 1, kind: static}
          collider2d: {version: 1, shape: box, width: 16, height: 100}
      - id: shot
        x: 50
        y: 50
        components:
          bullet: {version: 1, speed: 100, angle: 0, setAngle: true, bounceOffSolids: true}
`
		reg := fullBehaviorRegistry(t)
		doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
		if err != nil {
			t.Fatalf("ParseWith: %v", err)
		}
		built, err := sceneformat.Build(doc, nil)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if _, err := sceneformat.BuildPhysics(built, nil, nil); err != nil {
			t.Fatalf("BuildPhysics: %v", err)
		}
		sigs, err := sceneformat.Connect(doc, reg, built)
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		built.Signals = sigs

		bounces := 0
		if _, err := sigs.Listen("shot", "bullet.bounced", func(c sceneformat.Call) {
			bounces++
			ev, ok := c.Payload.(sceneformat.BulletBouncedEvent)
			if !ok {
				t.Errorf("payload type = %T, want BulletBouncedEvent", c.Payload)
			}
			if !almostEq32(ev.NormalX, -1, 1e-3) {
				t.Errorf("NormalX = %v, want -1", ev.NormalX)
			}
		}, sceneformat.ListenOptions{}); err != nil {
			t.Fatalf("Listen: %v", err)
		}

		// Wall left edge is at 108 - 8 = 100. Bullet starts at x=50 moving right at 100 px/s.
		// Hits x=100 at t=0.5s and bounces back to x=50 at t=1.0s.
		dt := time.Second / 60
		for range 60 {
			built.Step(dt)
		}
		if bounces != 1 {
			t.Fatalf("bounces = %d, want 1", bounces)
		}
		if got := built.Object("shot").Doc.X; !almostEq32(got, 50, 0.5) {
			t.Fatalf("shot.X = %v, want ~50", got)
		}
	})

	t.Run("tilemap TileSolid wall", func(t *testing.T) {
		src := `version: 1
virtualWidth: 160
virtualHeight: 80
layers:
  - name: main
    objects:
      - id: shot
        x: 32
        y: 24
        components:
          bullet: {version: 1, speed: 64, angle: 0, setAngle: true, bounceOffSolids: true}
`
		reg := fullBehaviorRegistry(t)
		doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
		if err != nil {
			t.Fatalf("ParseWith: %v", err)
		}
		built, err := sceneformat.Build(doc, nil)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		// Attach a tilemap with a solid tile column at col=4 (x in [64, 80]).
		tm := tilemap.New(10, 5, 16, 16)
		cells := make([]uint32, 50)
		for row := range 5 {
			cells[row*10+4] = 1
		}
		tm.Layers = append(tm.Layers, tilemap.NewTileLayer("walls", 10, 5, cells))
		tm.SetFlags(1, tilemap.TileSolid)
		built.Layers[0].Map = tm

		if _, err := sceneformat.BuildPhysics(built, nil, nil); err != nil {
			t.Fatalf("BuildPhysics: %v", err)
		}
		sigs, err := sceneformat.Connect(doc, reg, built)
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		built.Signals = sigs
		bounced := false
		_, _ = sigs.Listen("shot", "bullet.bounced", func(sceneformat.Call) { bounced = true }, sceneformat.ListenOptions{})

		// Distance from x=32 to solid tile at x=64 is 32px. At 64 px/s, hits at t=0.5s, returns to x=32 at t=1.0s.
		for range 60 {
			built.Step(time.Second / 60)
		}
		if !bounced {
			t.Fatal("expected bullet to bounce off TileSolid tilemap cell")
		}
		if got := built.Object("shot").Doc.X; !almostEq32(got, 32, 0.5) {
			t.Fatalf("shot.X = %v, want ~32", got)
		}
	})
}

func TestDestroyOutsideFadeFreeAndECS(t *testing.T) {
	src := `version: 1
virtualWidth: 100
virtualHeight: 100
layers:
  - name: main
    objects:
      - id: runner
        x: 90
        y: 50
        components:
          bullet: {version: 1, speed: 100, angle: 0}
          destroy_outside: {version: 1, margin: 20}
      - id: ghost
        x: 50
        y: 50
        components:
          fade: {version: 1, fadeIn: 0, wait: 0.2, fadeOut: 0.3, destroy: true}
`
	reg := fullBehaviorRegistry(t)
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	world := entities.NewWorld()
	spawned, sigs, err := sceneformat.SpawnConnected(doc, world, reg, built)
	if err != nil {
		t.Fatalf("SpawnConnected: %v", err)
	}
	built.Signals = sigs

	var events []string
	_, _ = sigs.Listen("runner", "destroy_outside.destroyed", func(sceneformat.Call) {
		events = append(events, "runner.destroyed")
	}, sceneformat.ListenOptions{})
	_, _ = sigs.Listen("ghost", "fade.finished", func(c sceneformat.Call) {
		ev := c.Payload.(sceneformat.FadeFinishedEvent)
		if !ev.Destroyed {
			t.Errorf("FadeFinishedEvent.Destroyed = false, want true")
		}
		events = append(events, "ghost.faded")
	}, sceneformat.ListenOptions{})

	for range 60 {
		built.Step(time.Second / 60)
	}

	if built.Object("runner") != nil {
		t.Fatal("runner should have been freed by destroy_outside")
	}
	if built.Object("ghost") != nil {
		t.Fatal("ghost should have been freed by fade")
	}
	if world.Alive(spawned["runner"]) || world.Alive(spawned["ghost"]) {
		t.Fatal("ECS entities should have been destroyed by BuiltScene.Free")
	}
	if !slices.Equal(events, []string{"runner.destroyed", "ghost.faded"}) {
		t.Fatalf("events = %v, want [runner.destroyed ghost.faded]", events)
	}
}

func TestHandlersPauseResumeAndFlash(t *testing.T) {
	src := `version: 1
virtualWidth: 320
virtualHeight: 180
layers:
  - name: main
    objects:
      - id: clock
        components:
          timer: {seconds: 0.25, oneShot: true, autostart: true}
      - id: hero
        x: 10
        y: 10
        alpha: 0.8
        components:
          move_to: {version: 1, maxSpeed: 200}
          flash: {version: 1, on: 0.05, off: 0.05}
      - id: missile
        x: 0
        y: 0
        components:
          bullet: {version: 1, speed: 100, angle: 0, enabled: false}
connections:
  - from: clock
    event: timeout
    to: hero
    handler: move_to.moveTo
    args: {x: 110, "y": 10}
  - from: hero
    event: move_to.arrived
    to: hero
    handler: flash.flash
    args: {duration: 0.2}
  - from: hero
    event: move_to.arrived
    to: missile
    handler: bullet.setSpeed
    args: {speed: 50}
  - from: hero
    event: move_to.arrived
    to: missile
    handler: bullet.start
`
	reg := fullBehaviorRegistry(t)
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sigs, err := sceneformat.Connect(doc, reg, built)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	built.Signals = sigs

	arrived := 0
	flashDone := 0
	_, _ = sigs.Listen("hero", "move_to.arrived", func(sceneformat.Call) { arrived++ }, sceneformat.ListenOptions{})
	_, _ = sigs.Listen("hero", "flash.finished", func(sceneformat.Call) { flashDone++ }, sceneformat.ListenOptions{})

	sawHidden := false
	dt := time.Second / 60
	// Step 1.5 seconds (90 ticks):
	// t=0.25s: timer fires -> hero starts moving (10 -> 110 at 200 px/s takes 0.5s).
	// t=0.75s: hero arrives -> triggers flash(0.2s) and starts missile at speed 50.
	// t=0.95s: flash finishes -> hero alpha restored to 0.8.
	// t=1.50s: missile has moved for 0.75s at 50 px/s => x ~ 37.5.
	for range 90 {
		if err := sigs.Update(dt); err != nil {
			t.Fatalf("Signals.Update: %v", err)
		}
		if err := sigs.Flush(); err != nil {
			t.Fatalf("Signals.Flush: %v", err)
		}
		built.Step(dt)
		if built.Object("hero").Doc.EffectiveAlpha() == 0 {
			sawHidden = true
		}
	}

	if arrived != 1 {
		t.Fatalf("arrived = %d, want 1", arrived)
	}
	if !sawHidden || flashDone != 1 {
		t.Fatalf("sawHidden=%v flashDone=%d, want true and 1", sawHidden, flashDone)
	}
	if a := built.Object("hero").Doc.EffectiveAlpha(); !almostEq32(a, 0.8, 1e-4) {
		t.Fatalf("hero alpha after flash = %v, want 0.8", a)
	}
	if mx := built.Object("missile").Doc.X; !almostEq32(mx, 37.5, 1.5) {
		t.Fatalf("missile.X = %v, want ~37.5", mx)
	}
}

func TestWrapAndDestroyOutsideViewMode(t *testing.T) {
	src := `version: 1
virtualWidth: 100
virtualHeight: 100
camera:
  x: 250
  "y": 50
layers:
  - name: main
    objects:
      - id: w
        x: 295
        y: 50
        components:
          bullet: {version: 1, speed: 20, angle: 0, setAngle: false}
          wrap: {version: 1, mode: view}
      - id: d
        x: 310
        y: 50
        components:
          destroy_outside: {version: 1, mode: view, margin: 5}
`
	reg := fullBehaviorRegistry(t)
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Camera centered at (250, 50) with viewport 100x100 => view bounds [200, 300] x [0, 100].
	// d at x=310 is outside [195, 305] and gets freed immediately; w moves from 295 to 315 and wraps to 215.
	built.Step(time.Second)
	if built.Object("d") != nil {
		t.Fatal("d should have been freed outside view bounds")
	}
	if wx := built.Object("w").Doc.X; !almostEq32(wx, 215, 1e-3) {
		t.Fatalf("w.X = %v, want 215", wx)
	}
}

func TestBehaviorsShowcaseFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "behaviors.scene.yaml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	reg := fullBehaviorRegistry(t)
	doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
		File:       "testdata/behaviors.scene.yaml",
		Components: reg,
	})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := sceneformat.BuildPhysics(built, nil, nil); err != nil {
		t.Fatalf("BuildPhysics: %v", err)
	}
	sigs, err := sceneformat.Connect(doc, reg, built)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	built.Signals = sigs

	bounced := false
	arrived := false
	faded := false
	_, _ = sigs.Listen("shot", "bullet.bounced", func(sceneformat.Call) { bounced = true }, sceneformat.ListenOptions{})
	_, _ = sigs.Listen("drone", "move_to.arrived", func(sceneformat.Call) { arrived = true }, sceneformat.ListenOptions{})
	_, _ = sigs.Listen("toast", "fade.finished", func(sceneformat.Call) { faded = true }, sceneformat.ListenOptions{})

	// Step 2 full seconds at 60 Hz using only Build + BuildPhysics + Connect + Step.
	dt := time.Second / 60
	for range 120 {
		built.Step(dt)
	}

	if !bounced {
		t.Error("shot never bounced off wall_right")
	}
	if !arrived {
		t.Error("drone never arrived at move_to target")
	}
	if !faded || built.Object("toast") != nil {
		t.Errorf("toast fade: faded=%v obj=%v, want true and nil", faded, built.Object("toast"))
	}
	if built.Object("saw").Doc.Rotation == 0 {
		t.Error("saw did not rotate")
	}
	if built.Object("coin").Doc.Y == 60 {
		t.Error("coin did not oscillate with sine")
	}
	// Cloud started at x=300 moving right at 60 px/s for 2s => 420 wraps modulo 320 to 100.
	if cx := built.Object("cloud").Doc.X; !almostEq32(cx, 100, 0.5) {
		t.Errorf("cloud.X = %v, want ~100", cx)
	}
	// Ensure no unexpected error strings in SceneSchema(nil) validation of the showcase fixture.
	schema, err := sceneformat.SceneSchema(nil)
	if err != nil {
		t.Fatalf("SceneSchema(nil): %v", err)
	}
	if err := schema.ValidateYAML(raw); err != nil {
		t.Fatalf("SceneSchema(nil) rejected behaviors.scene.yaml: %v", err)
	}
	_ = strings.Contains("", "")
}

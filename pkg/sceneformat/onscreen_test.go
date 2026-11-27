package sceneformat_test

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

const onScreenSceneYAML = `version: 2
layers:
  - name: l
    objects:
      - id: bullet
        x: 150
        y: 80
        components:
          on_screen:
            x: -8
            y: -8
            width: 16
            height: 16
            margin: 32
            pause: [particles]
connections: []
`

func TestOnScreenComponentParseAndDefaults(t *testing.T) {
	reg := sceneformat.OnScreenComponentRegistry()
	doc, err := sceneformat.ParseWith([]byte(onScreenSceneYAML), sceneformat.ParseOptions{File: "test.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	resolved, err := reg.Resolve(sceneformat.OnScreenComponentName, doc.Layers[0].Objects[0].Components[sceneformat.OnScreenComponentName].(map[string]any))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved["x"].(float64) != -8 || resolved["y"].(float64) != -8 {
		t.Fatalf("x, y = %v, %v, want -8, -8", resolved["x"], resolved["y"])
	}
	if resolved["width"].(float64) != 16 || resolved["height"].(float64) != 16 {
		t.Fatalf("width, height = %v, %v, want 16, 16", resolved["width"], resolved["height"])
	}
	if resolved["margin"].(float64) != 32 {
		t.Fatalf("margin = %v, want 32", resolved["margin"])
	}
	pause := resolved["pause"].([]string)
	if len(pause) != 1 || pause[0] != "particles" {
		t.Fatalf("pause = %v, want [particles]", pause)
	}

	// An unset component resolves every default.
	resolved, err = reg.Resolve(sceneformat.OnScreenComponentName, nil)
	if err != nil {
		t.Fatalf("Resolve defaults: %v", err)
	}
	if resolved["x"].(float64) != 0 || resolved["width"].(float64) != 16 || resolved["margin"].(float64) != 0 {
		t.Fatalf("defaults = %v", resolved)
	}

	// A bad pause value and an unknown field are refused.
	bad := strings.Replace(onScreenSceneYAML, "pause: [particles]", "pause: [physics]", 1)
	doc, err = sceneformat.ParseWith([]byte(bad), sceneformat.ParseOptions{File: "test.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith bad pause: %v", err)
	}
	if _, err := sceneformat.Build(doc, nil); err == nil || !strings.Contains(err.Error(), `"physics"`) {
		t.Fatalf("Build with pause [physics]: %v", err)
	}
	unknown := strings.Replace(onScreenSceneYAML, "margin: 32", "margn: 32", 1)
	if _, err := sceneformat.ParseWith([]byte(unknown), sceneformat.ParseOptions{File: "test.scene.yaml", Components: reg}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ParseWith unknown field: %v", err)
	}
}

func TestOnScreenBuildNotifiers(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "l",
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "parent",
						X:  100,
						Y:  100,
						Children: []sceneformat.ObjectDoc{
							{
								ID: "child",
								X:  5,
								Y:  5,
								Components: map[string]any{
									sceneformat.OnScreenComponentName: map[string]any{},
								},
							},
						},
					},
				},
			},
		},
	}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built.Notifiers) != 1 {
		t.Fatalf("%d notifiers, want 1", len(built.Notifiers))
	}
	n := built.Notifiers[0]
	if n.Object != built.Object("child") {
		t.Fatalf("notifier object = %v, want child", n.Object.ID)
	}
	// The local 16x16 rectangle composed through the parent's transform lands at world (105, 105).
	built.Update(0)
	r := n.Rect
	if r.Origin.X != 105 || r.Origin.Y != 105 || r.Size.Width != 16 || r.Size.Height != 16 {
		t.Fatalf("world rect = %+v, want origin (105, 105) size 16x16", r)
	}
	if !n.OnScreen() {
		t.Fatal("notifier inside the default view reported off-screen")
	}
}

const onScreenEmitterYAML = `version: 1
rate: 0
maxParticles: 64
lifetimeMin: 5s
lifetimeMax: 5s
speedMin: 20
speedMax: 20
`

func buildOnScreenEmitterScene(t *testing.T, x, y float32) *sceneformat.BuiltScene {
	t.Helper()
	fsys := fstest.MapFS{
		"fx/p.particles.yaml": &fstest.MapFile{Data: []byte(onScreenEmitterYAML)},
	}
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "l",
				Objects: []sceneformat.ObjectDoc{
					{
						ID:         "torch",
						X:          x,
						Y:          y,
						Particles:  &sceneformat.ParticlesRef{File: "fx/p.particles.yaml"},
						Components: map[string]any{sceneformat.OnScreenComponentName: map[string]any{"pause": []string{"particles"}}},
					},
				},
			},
		},
	}
	built, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return built
}

func TestOnScreenUpdatePausesEmitter(t *testing.T) {
	// The default view covers x in [0, 320), y in [0, 180); the torch sits far outside it.
	built := buildOnScreenEmitterScene(t, 1000, 500)
	torch := built.Object("torch")
	if torch.Emitter == nil {
		t.Fatal("torch has no emitter")
	}
	const dt = time.Second / 60

	// Burst a fixed set of particles (rate is 0, lifetimes are 5 s, so nothing spawns or expires on its own).
	torch.Emitter.Burst(10)

	// Off-screen with pause [particles]: the emitter never steps, so nothing ages.
	for range 30 {
		built.Update(dt)
	}
	for i, p := range torch.Emitter.Particles() {
		if p.Age != 0 {
			t.Fatalf("paused off-screen particle %d aged to %v", i, p.Age)
		}
	}

	// Bringing the object into the view resumes the emitter where it stopped.
	torch.Doc.X, torch.Doc.Y = 150, 80
	torch.Sync()
	for range 60 {
		built.Update(dt)
	}
	frozen := make([]time.Duration, 0, torch.Emitter.Count())
	for _, p := range torch.Emitter.Particles() {
		if p.Age <= 0 {
			t.Fatal("on-screen emitter did not age its particles")
		}
		frozen = append(frozen, p.Age)
	}

	// Leaving the view freezes the live particles: no aging while off-screen.
	torch.Doc.X, torch.Doc.Y = 1000, 500
	torch.Sync()
	for range 30 {
		built.Update(dt)
	}
	now := torch.Emitter.Particles()
	if len(now) != len(frozen) {
		t.Fatalf("paused emitter count changed: %d, want %d", len(now), len(frozen))
	}
	for i, p := range now {
		if p.Age != frozen[i] {
			t.Fatalf("paused particle %d aged %v -> %v", i, frozen[i], p.Age)
		}
	}

	// Back in view, the same particles resume aging where they stopped.
	torch.Doc.X, torch.Doc.Y = 150, 80
	torch.Sync()
	built.Update(dt)
	resumed := torch.Emitter.Particles()
	if len(resumed) != len(frozen) {
		t.Fatalf("resumed emitter count changed: %d, want %d", len(resumed), len(frozen))
	}
	for i, p := range resumed {
		if p.Age != frozen[i]+dt {
			t.Fatalf("resumed particle %d age = %v, want %v", i, p.Age, frozen[i]+dt)
		}
	}
}

// recorderRegistry registers on_screen and a recorder component whose "note" handler records each call's event.
func recorderRegistry(t *testing.T, events *[]string) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.OnScreenComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "recorder",
		Handlers: []sceneformat.Handler{
			{Name: "note", Func: func(c sceneformat.Call) { *events = append(*events, c.Event) }},
		},
	}); err != nil {
		t.Fatalf("Register recorder: %v", err)
	}
	return reg
}

func TestOnScreenEnteredExitedEvents(t *testing.T) {
	src := `version: 2
layers:
  - name: l
    objects:
      - id: bullet
        x: 500
        y: 500
        components:
          on_screen: {}
      - id: log
        components:
          recorder: {}
connections:
  - {from: bullet, event: on_screen.entered, to: log, handler: note}
  - {from: bullet, event: on_screen.exited, to: log, handler: note}
`
	var events []string
	reg := recorderRegistry(t, &events)
	doc, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{File: "test.scene.yaml", Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}
	doc.VirtualWidth, doc.VirtualHeight = 320, 180
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sig, err := sceneformat.Connect(built.Doc, reg, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	built.ScreenSignals = sig
	bullet := built.Object("bullet")
	const dt = time.Second / 60

	// Off-screen at first: no events.
	built.Update(dt)
	if len(events) != 0 {
		t.Fatalf("off-screen object fired %v", events)
	}

	// Entering the view fires entered once.
	bullet.Doc.X, bullet.Doc.Y = 150, 80
	bullet.Sync()
	for range 3 {
		built.Update(dt)
	}
	if len(events) != 1 || events[0] != "entered" {
		t.Fatalf("events = %v, want [entered]", events)
	}

	// Leaving fires exited once.
	bullet.Doc.X, bullet.Doc.Y = 500, 500
	bullet.Sync()
	for range 3 {
		built.Update(dt)
	}
	if len(events) != 2 || events[1] != "exited" {
		t.Fatalf("events = %v, want [entered exited]", events)
	}
}

func BenchmarkOnScreenUpdate5000(b *testing.B) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  320,
		VirtualHeight: 180,
	}
	objs := make([]sceneformat.ObjectDoc, 5000)
	for i := range objs {
		objs[i] = sceneformat.ObjectDoc{
			ID:         "o" + strconv.Itoa(i),
			X:          float32(i) * 100,
			Y:          float32(i) * 100,
			Components: map[string]any{sceneformat.OnScreenComponentName: map[string]any{"margin": 32.0}},
		}
	}
	doc.Layers = []sceneformat.LayerDoc{{Name: "l", Objects: objs}}
	built, err := sceneformat.Build(doc, nil)
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	const dt = time.Second / 60
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		built.Update(dt)
	}
}

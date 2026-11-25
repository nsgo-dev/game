package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const sampleEmitterYAML = `version: 1
space: world
shape:
  kind: circle
  radius: 6
rate: 30
maxParticles: 128
lifetimeMin: 400ms
lifetimeMax: 800ms
speedMin: 20
speedMax: 60
angleMin: -2.2
angleMax: -0.9
gravityY: 40
sizeCurve:
  - t: 0
    value: 1
  - t: 1
    value: 0.2
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
seed: 7
frameNames:
  - spark_0
`

func testParticleAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "fx.png"},
		Frames: map[string]sprite.Frame{
			"spark_0": {X: 0, Y: 0, Width: 8, Height: 8},
			"spark_1": {X: 8, Y: 0, Width: 8, Height: 8},
		},
	}
}

func TestParticleEmitterOpenEditEveryFieldUndoRedoAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	emitterPath := filepath.Join(dir, "sparks.particles.yaml")
	if err := os.WriteFile(emitterPath, []byte(sampleEmitterYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := sceneformat.AtlasMap{"fx": testParticleAtlas()}
	ed, err := editor.New(nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenParticleEmitterFile(emitterPath); err != nil {
		t.Fatalf("OpenParticleEmitterFile: %v", err)
	}
	if !ed.ParticleEmitterOpen() || ed.ParticleEmitterPath() != emitterPath {
		t.Fatalf("open=%v path=%q", ed.ParticleEmitterOpen(), ed.ParticleEmitterPath())
	}
	if ed.ParticleEmitterDirty() {
		t.Fatal("freshly opened emitter should not be dirty")
	}

	// Verify clean open+save round-trip before edits.
	initialYAML, err := ed.SaveParticleEmitterYAML()
	if err != nil {
		t.Fatalf("SaveParticleEmitterYAML: %v", err)
	}
	reloadedInitial, err := particles.UnmarshalConfigYAML(initialYAML)
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML: %v", err)
	}
	reencodedInitial, err := particles.MarshalConfigYAML(reloadedInitial)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if !bytes.Equal(initialYAML, reencodedInitial) {
		t.Fatalf("initial YAML did not round-trip unchanged:\ngot:\n%s\nwant:\n%s", reencodedInitial, initialYAML)
	}

	// Edit every field through undoable commands.
	steps := []struct {
		name string
		fn   func() error
	}{
		{"space", func() error { return ed.SetParticleSpace(particles.SpaceLocal) }},
		{"pos", func() error { return ed.SetParticlePosition(12, -8) }},
		{"rot", func() error { return ed.SetParticleRotation(0.5) }},
		{"z", func() error { return ed.SetParticleZ(15) }},
		{"seed", func() error { return ed.SetParticleSeed(99) }},
		{"shape-ring", func() error { return ed.SetParticleShape(particles.Ring(18)) }},
		{"shape-rect", func() error { return ed.SetParticleShape(particles.Rect(32, 14)) }},
		{"rate", func() error { return ed.SetParticleRate(75) }},
		{"maxParticles", func() error { return ed.SetParticleMaxParticles(256) }},
		{"add-burst-0", func() error {
			return ed.AddParticleBurst(particles.Burst{Time: 100 * time.Millisecond, Count: 10, Cycles: 2, Interval: 250 * time.Millisecond})
		}},
		{"add-burst-1", func() error {
			return ed.AddParticleBurst(particles.Burst{Time: 500 * time.Millisecond, Count: 5, Cycles: 1})
		}},
		{"set-burst-0", func() error {
			return ed.SetParticleBurst(0, particles.Burst{Time: 200 * time.Millisecond, Count: 16, Cycles: 3, Interval: 300 * time.Millisecond})
		}},
		{"remove-burst-1", func() error { return ed.RemoveParticleBurst(1) }},
		{"lifetime", func() error { return ed.SetParticleLifetime(250*time.Millisecond, 900*time.Millisecond) }},
		{"speed", func() error { return ed.SetParticleSpeed(40, 120) }},
		{"angle", func() error { return ed.SetParticleAngle(-1.5, -0.5) }},
		{"gravity", func() error { return ed.SetParticleGravity(10, 95) }},
		{"init-rot", func() error { return ed.SetParticleInitialRotation(-0.4, 0.4) }},
		{"rot-speed", func() error { return ed.SetParticleRotationSpeed(-2, 2) }},
		{"size-stop-add", func() error {
			_, err := ed.AddParticleFloatStop(editor.ParticleCurveSize, 0.5, 1.5)
			return err
		}},
		{"alpha-curve", func() error {
			return ed.SetParticleAlphaCurve(particles.FloatCurve{{T: 0, Value: 0.9}, {T: 0.8, Value: 0.6}, {T: 1, Value: 0}})
		}},
		{"rot-curve", func() error {
			return ed.SetParticleRotationCurve(particles.FloatCurve{{T: 0, Value: 0}, {T: 1, Value: 1.25}})
		}},
		{"color-stop-add", func() error {
			_, err := ed.AddParticleColorStop(editor.ParticleCurveColor, 0.5, core.Color(0x00ff88ff))
			return err
		}},
		{"frame-toggle-add", func() error { return ed.ToggleParticleFrame("spark_1") }},
		{"collision", func() error {
			return ed.SetParticleCollision(particles.CollisionConfig{
				Enabled:  true,
				Mode:     particles.CollideBounce,
				Bounce:   0.65,
				Friction: 0.15,
				Radius:   3,
			})
		}},
		{"trail-enable", func() error { return ed.SetParticleTrailEnabled(true) }},
		{"trail-config", func() error {
			return ed.SetParticleTrail(&particles.TrailConfig{
				MaxPoints:   20,
				Lifetime:    0.35,
				MinDistance: 2.5,
				Width:       4,
				Color:       core.Color(0x38bdf8ff),
				Texture:     core.ImageSource{URI: "fx/ribbon.png"},
				WidthCurve:  particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 0}},
				ColorGradient: particles.ColorCurve{
					{T: 0, Color: core.Color(0xffffffff)},
					{T: 1, Color: core.Color(0x38bdf800)},
				},
			})
		}},
		{"sub-add", func() error {
			return ed.AddParticleSubEmitter(particles.SubEmitterRule{
				Trigger:         particles.TriggerOnDeath,
				BurstCount:      6,
				InheritVelocity: 0.3,
				Config: particles.Config{
					Space:       particles.SpaceWorld,
					Shape:       particles.Point(),
					LifetimeMin: 150 * time.Millisecond,
					LifetimeMax: 300 * time.Millisecond,
					SpeedMin:    10,
					SpeedMax:    30,
				},
			})
		}},
		{"sub-edit-rule", func() error {
			return ed.SetParticleSubEmitter(0, particles.TriggerOnCollision, 8, 0.5)
		}},
	}

	for _, st := range steps {
		if err := st.fn(); err != nil {
			t.Fatalf("step %s failed: %v", st.name, err)
		}
	}

	// Navigate into sub-emitter #0 and edit its rate & shape, then return to root.
	if err := ed.SetParticleTargetPath([]int{0}); err != nil {
		t.Fatalf("SetParticleTargetPath: %v", err)
	}
	if err := ed.SetParticleShape(particles.Circle(4)); err != nil {
		t.Fatalf("child SetParticleShape: %v", err)
	}
	if err := ed.SetParticleTargetPath(nil); err != nil {
		t.Fatalf("SetParticleTargetPath(nil): %v", err)
	}

	if !ed.ParticleEmitterDirty() {
		t.Fatal("edited emitter should be dirty")
	}

	// Save to disk and verify deterministic round-trip unchanged.
	if err := ed.SaveParticleEmitterFile(emitterPath); err != nil {
		t.Fatalf("SaveParticleEmitterFile: %v", err)
	}
	if ed.ParticleEmitterDirty() {
		t.Fatal("emitter should be clean after SaveParticleEmitterFile")
	}
	savedBytes, err := os.ReadFile(emitterPath)
	if err != nil {
		t.Fatal(err)
	}
	parsedCfg, err := particles.UnmarshalConfigYAML(savedBytes)
	if err != nil {
		t.Fatalf("UnmarshalConfigYAML(saved): %v", err)
	}
	reencoded, err := particles.MarshalConfigYAML(parsedCfg)
	if err != nil {
		t.Fatalf("MarshalConfigYAML(parsed): %v", err)
	}
	if !bytes.Equal(savedBytes, reencoded) {
		t.Fatalf("saved emitter YAML did not round-trip unchanged:\ngot:\n%s\nwant:\n%s", reencoded, savedBytes)
	}

	// Verify every edited field survived save/reload.
	if parsedCfg.Space != particles.SpaceLocal || parsedCfg.Rate != 75 || parsedCfg.MaxParticles != 256 {
		t.Fatalf("basic fields mismatch: %+v", parsedCfg)
	}
	if parsedCfg.Shape.Kind() != "rect" || parsedCfg.Shape.Width() != 32 || parsedCfg.Shape.Height() != 14 {
		t.Fatalf("shape mismatch: %+v", parsedCfg.Shape)
	}
	if len(parsedCfg.Bursts) != 1 || parsedCfg.Bursts[0].Count != 16 || parsedCfg.Bursts[0].Cycles != 3 {
		t.Fatalf("bursts mismatch: %+v", parsedCfg.Bursts)
	}
	if !reflect.DeepEqual(parsedCfg.FrameNames, []string{"spark_0", "spark_1"}) {
		t.Fatalf("frameNames = %v", parsedCfg.FrameNames)
	}
	if !parsedCfg.Collision.Enabled || parsedCfg.Collision.Mode != particles.CollideBounce || parsedCfg.Collision.Bounce != 0.65 {
		t.Fatalf("collision mismatch: %+v", parsedCfg.Collision)
	}
	if parsedCfg.Trail == nil || parsedCfg.Trail.MaxPoints != 20 || parsedCfg.Trail.Texture.URI != "fx/ribbon.png" {
		t.Fatalf("trail mismatch: %+v", parsedCfg.Trail)
	}
	if len(parsedCfg.SubEmitters) != 1 || parsedCfg.SubEmitters[0].Trigger != particles.TriggerOnCollision || parsedCfg.SubEmitters[0].Config.Shape.Kind() != "circle" {
		t.Fatalf("subEmitters mismatch: %+v", parsedCfg.SubEmitters)
	}

	// Undo every single edit (+ child edit) back to the original YAML, then Redo all the way back!
	totalEdits := len(steps) + 1
	for range totalEdits {
		if !ed.Undo() {
			t.Fatal("expected Undo to succeed")
		}
	}
	undoneYAML, err := ed.SaveParticleEmitterYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(undoneYAML, initialYAML) {
		t.Fatalf("undoing all edits did not restore initial YAML:\ngot:\n%s\nwant:\n%s", undoneYAML, initialYAML)
	}

	for range totalEdits {
		if !ed.Redo() {
			t.Fatal("expected Redo to succeed")
		}
	}
	redoneYAML, err := ed.SaveParticleEmitterYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(redoneYAML, savedBytes) {
		t.Fatalf("redoing all edits did not restore saved YAML:\ngot:\n%s\nwant:\n%s", redoneYAML, savedBytes)
	}
}

func TestParticleLivePreviewUpdatesWithoutRestart(t *testing.T) {
	ed, err := editor.New(nil, sceneformat.AtlasMap{"fx": testParticleAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenParticleEmitterYAML([]byte(sampleEmitterYAML)); err != nil {
		t.Fatalf("OpenParticleEmitterYAML: %v", err)
	}

	// Step the preview so live particles exist.
	for range 15 {
		ed.StepParticlePreview(time.Second / 60)
	}
	em := ed.ParticlePreview()
	if em == nil || em.Count() == 0 {
		t.Fatalf("expected live particles in preview after stepping, got %d", em.Count())
	}
	countBefore := em.Count()
	elapsedBefore := ed.ParticlePreviewElapsed()
	firstSizeBefore := em.Particles()[0].Size

	// Acceptance criterion: changing rate or a curve updates the running preview live without a restart.
	if err := ed.SetParticleRate(240); err != nil {
		t.Fatalf("SetParticleRate: %v", err)
	}
	if em.Count() != countBefore || ed.ParticlePreviewElapsed() != elapsedBefore {
		t.Fatalf("changing rate restarted preview: count %d->%d elapsed %v->%v", countBefore, em.Count(), elapsedBefore, ed.ParticlePreviewElapsed())
	}

	// Change SizeCurve and ColorCurve while particles are alive.
	if err := ed.SetParticleSizeCurve(particles.FloatCurve{{T: 0, Value: 3.5}, {T: 1, Value: 3.5}}); err != nil {
		t.Fatalf("SetParticleSizeCurve: %v", err)
	}
	if err := ed.SetParticleColorCurve(particles.ColorCurve{{T: 0, Color: core.Color(0x00ff00ff)}, {T: 1, Color: core.Color(0x00ff00ff)}}); err != nil {
		t.Fatalf("SetParticleColorCurve: %v", err)
	}
	if em.Count() != countBefore || ed.ParticlePreviewElapsed() != elapsedBefore {
		t.Fatalf("changing curves restarted preview: count %d->%d elapsed %v->%v", countBefore, em.Count(), elapsedBefore, ed.ParticlePreviewElapsed())
	}
	live := em.Particles()[0]
	if live.Size == firstSizeBefore || live.Size < 3.4 || live.Size > 3.6 {
		t.Fatalf("live particle size did not update immediately on curve edit: got %v (was %v)", live.Size, firstSizeBefore)
	}
	if live.Color != core.Color(0x00ff00ff) {
		t.Fatalf("live particle color did not update immediately on curve edit: got %08x", uint32(live.Color))
	}

	// Stepping further emits more particles at the updated higher rate.
	ed.StepParticlePreview(50 * time.Millisecond)
	if em.Count() <= countBefore {
		t.Fatalf("expected new rate to spawn additional particles: before=%d after=%d", countBefore, em.Count())
	}

	// Pause, Restart, Burst, and Floor collider.
	ed.PauseParticlePreview()
	if ed.ParticlePreviewPlaying() {
		t.Fatal("expected preview to be paused")
	}
	ed.RestartParticlePreview()
	if em.Count() != 0 || ed.ParticlePreviewElapsed() != 0 {
		t.Fatalf("RestartParticlePreview did not reset preview: count=%d elapsed=%v", em.Count(), ed.ParticlePreviewElapsed())
	}
	ed.BurstParticlePreview(10)
	if em.Count() != 10 {
		t.Fatalf("BurstParticlePreview(10) count = %d, want 10", em.Count())
	}
	ed.SetParticlePreviewFloor(true)
	if !ed.ParticlePreviewFloor() || em.Collision.Collider == nil {
		t.Fatal("SetParticlePreviewFloor(true) should bind preview floor collider")
	}
}

func TestScenePlacedParticlesPlayInEditorAndGame(t *testing.T) {
	dir := writeEditorProject(t)
	emitterRel := "fx/sparks.particles.yaml"
	emitterAbs := filepath.Join(dir, filepath.FromSlash(emitterRel))
	if err := os.MkdirAll(filepath.Dir(emitterAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(emitterAbs, []byte(sampleEmitterYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	atlases := testResolver()
	atlases["fx"] = testParticleAtlas()
	resolver := sceneformat.FSAssets{
		FS:      os.DirFS(dir),
		Atlases: atlases,
	}
	ed, err := editor.New(nil, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Place emitter in the scene (using active layer, raising v1 scene to v2 if needed).
	id, err := ed.PlaceParticles("", emitterRel, "fx", 120, 80)
	if err != nil {
		t.Fatalf("PlaceParticles: %v", err)
	}
	obj, _, _ := ed.FindObject(id)
	if obj == nil || obj.Particles == nil || obj.Particles.File != emitterRel || obj.Particles.Atlas != "fx" {
		t.Fatalf("placed object particles = %+v", obj)
	}
	if bo := ed.Built.Object(id); bo == nil || bo.Emitter == nil {
		t.Fatal("built scene did not construct Emitter for placed object")
	}

	// Step emitters in edit mode and verify DrawViewport renders them (and respects SetObjectHidden).
	for range 10 {
		ed.StepSceneEmitters(time.Second / 60)
	}
	if count := ed.Built.Object(id).Emitter.Count(); count == 0 {
		t.Fatal("expected edit-mode scene emitter to spawn particles")
	}
	var dVisible core.Drawing
	ed.DrawViewport(&dVisible, core.Size{Width: 640, Height: 360})
	opsVisible := len(dVisible.Ops)

	ed.SetObjectHidden(id, true)
	var dHidden core.Drawing
	ed.DrawViewport(&dHidden, core.Size{Width: 640, Height: 360})
	if len(dHidden.Ops) >= opsVisible {
		t.Fatalf("hiding particle object should skip emitter draw ops: visible=%d hidden=%d", opsVisible, len(dHidden.Ops))
	}
	ed.SetObjectHidden(id, false)

	// Save scene YAML and verify it plays cleanly in the game (sceneformat.Parse + Build + Update + Draw).
	sceneBytes, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	gameDoc, err := sceneformat.Parse(sceneBytes)
	if err != nil {
		t.Fatalf("game sceneformat.Parse: %v", err)
	}
	gameBuilt, err := sceneformat.Build(gameDoc, resolver)
	if err != nil {
		t.Fatalf("game sceneformat.Build: %v", err)
	}
	for range 15 {
		gameBuilt.Update(time.Second / 60)
	}
	gameEm := gameBuilt.Object(id).Emitter
	if gameEm == nil || gameEm.Count() == 0 {
		t.Fatal("placed emitter did not spawn particles when run in standalone game BuiltScene")
	}
	var gameDraw core.Drawing
	gameBuilt.Draw(&gameDraw)
	if len(gameDraw.Ops) == 0 {
		t.Fatal("game BuiltScene.Draw produced no draw ops for active particle emitter")
	}

	// Verify placed emitter plays in the editor's Play mode and restores cleanly on Stop.
	ed.PlayRate = 60
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("expected editor to enter Play mode after pressing Play")
	}
	app.Advance(500 * time.Millisecond)
	ps := ed.PlayScene()
	if ps == nil || ps.Object(id) == nil || ps.Object(id).Emitter == nil || ps.Object(id).Emitter.Count() == 0 {
		t.Fatalf("placed emitter did not spawn particles in editor Play mode (steps=%d)", ed.PlaySteps())
	}
	app.Press("id:toolbar-play")
	if ed.Playing() {
		t.Fatal("expected editor to exit Play mode after pressing Stop")
	}
	afterStopBytes, err := ed.SaveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sceneBytes, afterStopBytes) {
		t.Fatal("Stop did not restore exact pre-play scene YAML")
	}

	// Verify MoveFile updates scene particles.file and open ParticleEmitterPath, and Undo restores both.
	if err := ed.OpenParticleEmitterProjectFile(emitterRel); err != nil {
		t.Fatalf("OpenParticleEmitterProjectFile: %v", err)
	}
	movedRel := "vfx/sparks.particles.yaml"
	if err := ed.MoveFile(emitterRel, movedRel); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if o, _, _ := ed.FindObject(id); o.Particles.File != movedRel {
		t.Fatalf("object particles.file after MoveFile = %q, want %q", o.Particles.File, movedRel)
	}
	if got := filepath.ToSlash(ed.ParticleEmitterPath()); !bytes.HasSuffix([]byte(got), []byte("/"+movedRel)) {
		t.Fatalf("ParticleEmitterPath after MoveFile = %q, want suffix /%s", got, movedRel)
	}
	if !ed.Undo() {
		t.Fatal("Undo MoveFile failed")
	}
	if o, _, _ := ed.FindObject(id); o.Particles.File != emitterRel {
		t.Fatalf("object particles.file after Undo = %q, want %q", o.Particles.File, emitterRel)
	}
}

func TestParticleEditorUI(t *testing.T) {
	resolver := sceneformat.AtlasMap{"fx": testParticleAtlas()}
	ed, err := editor.New(nil, resolver)
	if err != nil {
		t.Fatal(err)
	}

	var savedYAML []byte
	ed.OnSaveParticleEmitter = func(data []byte) error {
		savedYAML = append([]byte(nil), data...)
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1400, 900))

	if app.Exists("id:particles-panel") {
		t.Fatal("Particle Emitter panel should not be visible initially")
	}

	// Open via toolbar button.
	app.Press("id:toolbar-particles")
	app.Settle()
	if !app.Exists("id:particles-panel") || !ed.ParticleEmitterOpen() {
		t.Fatalf("Particle Emitter panel did not open; status=%q", ed.Status)
	}

	// Pause, Restart, Burst, Step via UI buttons.
	app.Press("id:particles-preview-play") // Pause
	app.Settle()
	if ed.ParticlePreviewPlaying() {
		t.Fatal("Pause button did not pause preview")
	}
	app.Press("id:particles-preview-restart")
	app.Settle()
	if ed.ParticlePreview().Count() != 0 {
		t.Fatalf("Restart button did not clear preview, count=%d", ed.ParticlePreview().Count())
	}
	app.Press("id:particles-preview-burst")
	app.Settle()
	if ed.ParticlePreview().Count() != 12 {
		t.Fatalf("Burst button spawned %d particles, want 12", ed.ParticlePreview().Count())
	}
	elapsedBefore := ed.ParticlePreviewElapsed()
	app.Press("id:particles-preview-step")
	app.Settle()
	if ed.ParticlePreviewElapsed() <= elapsedBefore {
		t.Fatal("Step button did not advance preview elapsed time")
	}

	// Curve widget tabs and + Stop / - Stop buttons.
	stopsBefore := len(ed.ParticleActiveConfig().SizeCurve)
	app.Press("id:particles-curve-add")
	app.Settle()
	if len(ed.ParticleActiveConfig().SizeCurve) != stopsBefore+1 {
		t.Fatalf("+ Stop did not add a stop: got %d, want %d", len(ed.ParticleActiveConfig().SizeCurve), stopsBefore+1)
	}
	app.Press("id:particles-curve-remove")
	app.Settle()
	if len(ed.ParticleActiveConfig().SizeCurve) != stopsBefore {
		t.Fatalf("- Stop did not remove the selected stop: got %d, want %d", len(ed.ParticleActiveConfig().SizeCurve), stopsBefore)
	}
	app.Press("id:particles-curve-tab-color")
	app.Settle()
	if ed.ParticleActiveCurve() != editor.ParticleCurveColor {
		t.Fatalf("Color tab did not activate color curve, got %q", ed.ParticleActiveCurve())
	}
	if !app.Exists("id:particles-color-canvas") {
		t.Fatal("expected color gradient canvas on Color tab")
	}

	// Atlas frame picker chip toggles frame into FrameNames.
	app.Press("id:particles-frame-toggle-spark_0")
	app.Settle()
	if !reflect.DeepEqual(ed.ParticleActiveConfig().FrameNames, []string{"spark_0"}) {
		t.Fatalf("FrameNames after toggle = %v, want [spark_0]", ed.ParticleActiveConfig().FrameNames)
	}

	// Save Emitter button writes clean round-tripping YAML via OnSaveParticleEmitter.
	app.Press("id:particles-save")
	app.Settle()
	if len(savedYAML) == 0 {
		t.Fatalf("Save Emitter did not invoke OnSaveParticleEmitter; status=%q", ed.Status)
	}
	parsed, err := particles.UnmarshalConfigYAML(savedYAML)
	if err != nil {
		t.Fatalf("saved YAML failed UnmarshalConfigYAML: %v", err)
	}
	reencoded, err := particles.MarshalConfigYAML(parsed)
	if err != nil {
		t.Fatalf("MarshalConfigYAML: %v", err)
	}
	if !bytes.Equal(savedYAML, reencoded) {
		t.Fatalf("UI-saved YAML did not round-trip unchanged:\ngot:\n%s\nwant:\n%s", reencoded, savedYAML)
	}

	// Close panel via button.
	app.Press("id:particles-close")
	app.Settle()
	if app.Exists("id:particles-panel") || ed.ParticleEmitterOpen() {
		t.Fatal("Close button did not close Particle Emitter panel")
	}
}

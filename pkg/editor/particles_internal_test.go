package editor

import (
	"errors"
	"math"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestParticleEditorNilReceiverGuards(t *testing.T) {
	var ed *Editor
	if err := ed.NewParticleEmitter(); err == nil {
		t.Fatal("nil NewParticleEmitter should return error")
	}
	if err := ed.OpenParticleEmitterYAML([]byte("version: 1\n")); err == nil {
		t.Fatal("nil OpenParticleEmitterYAML should return error")
	}
	if err := ed.OpenParticleEmitterFile("fx/a.particles.yaml"); err == nil {
		t.Fatal("nil OpenParticleEmitterFile should return error")
	}
	if err := ed.OpenParticleEmitterProjectFile("fx/a.particles.yaml"); err == nil {
		t.Fatal("nil OpenParticleEmitterProjectFile should return error")
	}
	if err := ed.CloseParticleEmitter(); err != nil {
		t.Fatalf("nil CloseParticleEmitter: %v", err)
	}
	if err := ed.EditParticleEmitter("edit", func(*particles.Config) error { return nil }); !errors.Is(err, ErrNoParticleEmitter) {
		t.Fatalf("nil EditParticleEmitter = %v, want ErrNoParticleEmitter", err)
	}
	if err := ed.SaveParticleEmitterFile(filepath.Join(t.TempDir(), "a.particles.yaml")); !errors.Is(err, ErrNoParticleEmitter) {
		t.Fatalf("nil SaveParticleEmitterFile = %v, want ErrNoParticleEmitter", err)
	}
	if err := ed.SetObjectParticles("hero", nil); err == nil {
		t.Fatal("nil SetObjectParticles should return error")
	}
	if _, err := ed.PlaceParticles("main", "fx/a.particles.yaml", "", 0, 0); err == nil {
		t.Fatal("nil PlaceParticles should return error")
	}
}

func TestParticleEditorCurveDragCancelPlayAndValidation(t *testing.T) {
	ed, err := New(DefaultSceneDoc(), sceneformat.AtlasMap{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.NewParticleEmitter(); err != nil {
		t.Fatal(err)
	}

	// Negative / non-finite shape and trail dimensions are rejected.
	for _, bad := range []particles.Shape{
		particles.Circle(-1),
		particles.Ring(float32(math.NaN())),
		particles.Rect(-2, 4),
		particles.Rect(2, -4),
	} {
		if err := ed.SetParticleShape(bad); err == nil {
			t.Fatalf("expected error for invalid shape %+v", bad)
		}
	}
	for _, bad := range []*particles.TrailConfig{
		{MaxPoints: -1, Lifetime: 0.2, MinDistance: 1, Width: 2},
		{MaxPoints: 4, Lifetime: -0.1, MinDistance: 1, Width: 2},
		{MaxPoints: 4, Lifetime: 0.2, MinDistance: -1, Width: 2},
		{MaxPoints: 4, Lifetime: 0.2, MinDistance: 1, Width: -2},
		{MaxPoints: 4, Lifetime: float32(math.Inf(1)), MinDistance: 1, Width: 2},
	} {
		if err := ed.SetParticleTrail(bad); err == nil {
			t.Fatalf("expected error for invalid trail %+v", bad)
		}
	}

	origVal := ed.ParticleActiveConfig().SizeCurve[0].Value
	ed.beginParticleCurveDrag(ParticleCurveSize, 0)
	// Non-finite t / val updates are ignored.
	ed.updateParticleCurveDrag(float32(math.NaN()), 99)
	ed.updateParticleCurveDrag(0.5, float32(math.Inf(1)))
	if got := ed.ParticleActiveConfig().SizeCurve[0].Value; got != origVal {
		t.Fatalf("non-finite drag mutated stop: got %v, want %v", got, origVal)
	}

	// Valid drag mutates live config, and SetParticleActiveCurve cancels & restores beforeRoot.
	ed.updateParticleCurveDrag(0.25, 2.5)
	if got := ed.ParticleActiveConfig().SizeCurve[0].Value; got != 2.5 {
		t.Fatalf("during drag stop value = %v, want 2.5", got)
	}
	ed.SetParticleActiveCurve(ParticleCurveAlpha)
	if got := ed.ParticleActiveConfig().SizeCurve[0].Value; got != origVal {
		t.Fatalf("SetParticleActiveCurve did not restore beforeRoot: got %v, want %v", got, origVal)
	}
	if ed.particles.curveDrag != nil {
		t.Fatal("SetParticleActiveCurve left curveDrag non-nil")
	}

	// SaveParticleEmitterFile commits an active drag before saving, and refuses while Playing.
	ed.SetParticleActiveCurve(ParticleCurveSize)
	ed.beginParticleCurveDrag(ParticleCurveSize, 0)
	ed.updateParticleCurveDrag(0.1, 3.0)
	outPath := filepath.Join(t.TempDir(), "saved.particles.yaml")
	if err := ed.SaveParticleEmitterFile(outPath); err != nil {
		t.Fatalf("SaveParticleEmitterFile: %v", err)
	}
	if ed.particles.curveDrag != nil {
		t.Fatal("SaveParticleEmitterFile left curveDrag active")
	}
	if ed.ParticleEmitterDirty() {
		t.Fatal("emitter still dirty after SaveParticleEmitterFile")
	}
	// Stepping preview keeps dirty cached (dirtyValid remains true).
	ed.StepParticlePreview(16 * time.Millisecond)
	if !ed.particles.dirtyValid || ed.ParticleEmitterDirty() {
		t.Fatalf("dirty cache invalidated by preview step: valid=%v dirty=%v", ed.particles.dirtyValid, ed.particles.dirty)
	}

	// PlayIn cancels an active curve drag and SaveParticleEmitterFile refuses during Play.
	ed.beginParticleCurveDrag(ParticleCurveSize, 0)
	ed.updateParticleCurveDrag(0.2, 7.5)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))
	var playErr, saveErr error
	app.Post(func() {
		playErr = ed.Play()
		saveErr = ed.SaveParticleEmitterFile(outPath)
	})
	app.Settle()
	if playErr != nil {
		t.Fatalf("Play: %v", playErr)
	}
	if !errors.Is(saveErr, ErrPlaying) {
		t.Fatalf("SaveParticleEmitterFile while playing = %v, want ErrPlaying", saveErr)
	}
	if got := ed.ParticleActiveConfig().SizeCurve[0].Value; got != 3.0 {
		t.Fatalf("PlayIn did not cancel active curve drag: got %v, want 3.0", got)
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}

func TestParticleViewportDrawParallaxAlignmentAndHiddenEmitter(t *testing.T) {
	emitterYAML, err := particles.MarshalConfigYAML(particles.Config{
		Space:        particles.SpaceWorld,
		Shape:        particles.Point(),
		Rate:         0,
		MaxParticles: 32,
		LifetimeMin:  5 * time.Second,
		LifetimeMax:  5 * time.Second,
		Bursts:       []particles.Burst{{Time: 0, Count: 5, Cycles: 1}},
		FrameNames:   []string{"torch"},
		Seed:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	px := float32(0.5)
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "ParallaxParticles",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{
			{
				Name:      "bg",
				Space:     sceneformat.SpaceWorld,
				Order:     0,
				ParallaxX: &px,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:        "fx1",
						Name:      "FX1",
						X:         100,
						Y:         60,
						Particles: &sceneformat.ParticlesRef{File: "fx/sparks.particles.yaml", Atlas: "actors"},
					},
				},
			},
		},
	}
	resolver := sceneformat.FSAssets{
		FS: fstest.MapFS{
			"fx/sparks.particles.yaml": &fstest.MapFile{Data: emitterYAML},
		},
		Atlases: DemoAtlasMap(),
	}
	ed, err := New(doc, resolver)
	if err != nil {
		t.Fatal(err)
	}
	ed.SetCanvasSize(320, 180)
	ed.SetView2D(View2D{X: 200, Y: 90, Zoom: 1})
	ed.StepSceneEmitters(50 * time.Millisecond)

	bo := ed.Built.Object("fx1")
	if bo == nil || bo.Emitter == nil || bo.Emitter.Count() == 0 {
		t.Fatalf("expected live emitter particles, got bo=%+v", bo)
	}

	// Visible emitter draws particles in drawEditScene (even though layer has no tilemap or sprite batches).
	hasPaintOp := func(ops []core.DrawOp) bool {
		for _, op := range ops {
			switch op.Kind {
			case core.DrawFill, core.DrawStroke, core.DrawRect, core.DrawImage, core.DrawAtlas, core.DrawArc, core.DrawEllipse:
				return true
			}
		}
		return false
	}
	vpSize := core.Size{Width: 320, Height: 180}
	var visibleDraw core.Drawing
	ed.drawEditScene(&visibleDraw, ed.Built, vpSize)
	if !hasPaintOp(visibleDraw.Ops) {
		t.Fatalf("drawEditScene skipped layer containing only particle emitters (ops=%d)", len(visibleDraw.Ops))
	}

	// Hidden emitter draws neither particles nor overlay.
	ed.SetObjectHidden("fx1", true)
	var hiddenDraw core.Drawing
	ed.drawEditScene(&hiddenDraw, ed.Built, vpSize)
	if hasPaintOp(hiddenDraw.Ops) {
		t.Fatalf("hidden emitter still emitted paint ops in drawEditScene: %+v", hiddenDraw.Ops)
	}
	var hiddenOverlay core.Drawing
	ed.drawParticleEmitterOverlays(&hiddenOverlay, ed.Built)
	if len(hiddenOverlay.Ops) != 0 {
		t.Fatalf("hidden emitter still emitted %d overlay commands", len(hiddenOverlay.Ops))
	}
	ed.SetObjectHidden("fx1", false)

	// Overlay aligns with layerAffineFor (parallax 0.5), NOT un-parallaxed layerAffine (1.0).
	wantPt := ed.layerAffineFor(sceneformat.SpaceWorld, 0.5, 1).TransformPoint(math2d.Vec2{X: 100, Y: 60})
	wrongPt := ed.layerAffine(sceneformat.SpaceWorld).TransformPoint(math2d.Vec2{X: 100, Y: 60})
	if math.Abs(float64(wantPt.X-wrongPt.X)) < 10 {
		t.Fatalf("test setup error: parallax and non-parallax X too close (%v vs %v)", wantPt.X, wrongPt.X)
	}

	// Verify drawParticleEmitterOverlays records diamond MoveTo(cx, cy-6) at wantPt.
	var ov core.Drawing
	ed.drawParticleEmitterOverlays(&ov, ed.Built)
	if len(ov.Ops) == 0 {
		t.Fatal("drawParticleEmitterOverlays produced no draw commands")
	}
	var foundAtParallax bool
	for _, op := range ov.Ops {
		if op.Kind == core.DrawMoveTo && math.Abs(float64(op.X-wantPt.X)) < 0.5 && math.Abs(float64(op.Y-(wantPt.Y-6))) < 0.5 {
			foundAtParallax = true
			break
		}
	}
	if !foundAtParallax {
		t.Fatalf("emitter overlay did not draw at parallax point (%.2f, %.2f); non-parallax was (%.2f, %.2f)", wantPt.X, wantPt.Y, wrongPt.X, wrongPt.Y)
	}
}

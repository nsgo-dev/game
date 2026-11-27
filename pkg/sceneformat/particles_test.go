package sceneformat_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func TestParticlesObjectBuildUpdateDrawAndRoundTrip(t *testing.T) {
	emitterYAML := `version: 1
space: world
shape:
  kind: point
rate: 60
maxParticles: 64
lifetimeMin: 500ms
lifetimeMax: 500ms
speedMin: 20
speedMax: 20
frameNames:
  - spark
`
	fsys := fstest.MapFS{
		"fx/fire.particles.yaml": &fstest.MapFile{Data: []byte(emitterYAML)},
	}
	atlases := sceneformat.AtlasMap{
		"fx": &sprite.Atlas{
			Image: core.ImageSource{URI: "asset:fx.png"},
			Frames: map[string]sprite.Frame{
				"spark": {Name: "spark", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			},
		},
	}

	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Torch Room",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "fx",
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "torch",
						X:  100,
						Y:  50,
						Z:  5,
						Particles: &sceneformat.ParticlesRef{
							File:  "fx/fire.particles.yaml",
							Atlas: "fx",
						},
					},
				},
			},
		},
	}

	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	parsed, err := sceneformat.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, encoded)
	}
	again, err := sceneformat.Encode(parsed)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("round-trip changed bytes (%v):\n%s\n---\n%s", err, encoded, again)
	}

	built, err := sceneformat.Build(parsed, sceneformat.FSAssets{FS: fsys, Atlases: atlases})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	torch := built.Object("torch")
	if torch == nil || torch.Emitter == nil {
		t.Fatalf("built torch object has nil Emitter: %+v", torch)
	}
	if torch.Emitter.X != 100 || torch.Emitter.Y != 50 || torch.Emitter.Z != 5 {
		t.Fatalf("emitter transform = (%v, %v, z=%d), want (100, 50, z=5)", torch.Emitter.X, torch.Emitter.Y, torch.Emitter.Z)
	}

	// Stepping BuiltScene advances the placed emitter and spawns particles.
	built.Update(100 * time.Millisecond)
	if torch.Emitter.Count() == 0 {
		t.Fatal("expected BuiltScene.Update to emit particles on placed emitter")
	}

	// Drawing BuiltScene draws into a core.Drawing cleanly.
	var d core.Drawing
	built.Draw(&d)
	if len(d.Ops) == 0 {
		t.Fatal("expected BuiltScene.Draw to emit draw ops for placed particle emitter")
	}

	// Sync updates emitter position when the object moves.
	torch.Doc.X = 140
	torch.Doc.Y = 75
	if !torch.Sync() {
		t.Fatal("Sync returned false")
	}
	if torch.Emitter.X != 140 || torch.Emitter.Y != 75 {
		t.Fatalf("emitter pos after Sync = (%v, %v), want (140, 75)", torch.Emitter.X, torch.Emitter.Y)
	}
}

func TestParticlesValidationAndErrors(t *testing.T) {
	// Version 1 refuses particles.
	v1 := objectScene("1", "particles: {file: fx/fire.particles.yaml}")
	if _, err := sceneformat.Parse([]byte(v1)); err == nil || !strings.Contains(err.Error(), "need version 2") && !strings.Contains(err.Error(), "needs version 2") {
		t.Fatalf("Parse v1 with particles = %v, want version 2 error", err)
	}

	// Escaping path is refused.
	badPath := objectScene("2", "particles: {file: ../fire.particles.yaml}")
	if _, err := sceneformat.Parse([]byte(badPath)); err == nil || !strings.Contains(err.Error(), "particles file") {
		t.Fatalf("Parse bad particles path = %v", err)
	}

	// Missing frame in named atlas fails Build.
	fsys := fstest.MapFS{
		"fx/fire.particles.yaml": &fstest.MapFile{Data: []byte("version: 1\nrate: 10\nframeNames:\n  - missing_frame\n")},
	}
	atlases := sceneformat.AtlasMap{
		"fx": &sprite.Atlas{Frames: map[string]sprite.Frame{"spark": {Name: "spark", W: 8, H: 8}}},
	}
	doc, err := sceneformat.Parse([]byte(objectScene("2", "particles: {file: fx/fire.particles.yaml, atlas: fx}")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := sceneformat.Build(doc, sceneformat.FSAssets{FS: fsys, Atlases: atlases}); err == nil || !strings.Contains(err.Error(), "missing_frame") {
		t.Fatalf("Build with missing frame = %v, want error naming missing_frame", err)
	}

	// Custom ParticlesResolver is supported without FileResolver.
	doc2, err := sceneformat.Parse([]byte(objectScene("2", "particles: {file: fx/custom.yaml}")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	built, err := sceneformat.Build(doc2, customParticlesResolver{
		AtlasMap: atlases,
		cfg:      particles.Config{Rate: 30, LifetimeMin: time.Second},
	})
	if err != nil {
		t.Fatalf("Build with ParticlesResolver: %v", err)
	}
	built.Update(100 * time.Millisecond)
	if built.Object("o").Emitter.Count() == 0 {
		t.Fatal("expected ParticlesResolver emitter to spawn particles")
	}
}

type customParticlesResolver struct {
	sceneformat.AtlasMap
	cfg particles.Config
}

func (r customParticlesResolver) Particles(string) (particles.Config, error) {
	return r.cfg, nil
}

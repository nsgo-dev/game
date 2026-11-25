package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

func TestCheckSceneParticles(t *testing.T) {
	atlasJSON := `{"frames":{"spark":{"frame":{"x":0,"y":0,"w":4,"h":4},"sourceSize":{"w":4,"h":4}}},"meta":{"image":"fx.png","size":{"w":4,"h":4}}}`
	imgBytes := pngBytes(t, 4, 4)

	validFS := fstest.MapFS{
		"fx.png":  &fstest.MapFile{Data: imgBytes},
		"fx.json": &fstest.MapFile{Data: []byte(atlasJSON)},
		"fx/fire.particles.yaml": &fstest.MapFile{Data: []byte(`version: 1
space: world
shape:
  kind: point
rate: 20
frameNames:
  - spark
`)},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(`version: 2
layers:
  - name: fx
    objects:
      - id: torch
        particles:
          file: fx/fire.particles.yaml
          atlas: fx
`)},
	}
	rep, err := assetcheck.Check(validFS)
	if err != nil {
		t.Fatalf("Check(valid): %v", err)
	}
	if !rep.OK() {
		t.Fatalf("expected no problems for valid particles scene, got: %v", rep.Problems)
	}

	// Missing frame in atlas is reported.
	badFrameFS := fstest.MapFS{
		"fx.png":  &fstest.MapFile{Data: imgBytes},
		"fx.json": &fstest.MapFile{Data: []byte(atlasJSON)},
		"fx/fire.particles.yaml": &fstest.MapFile{Data: []byte(`version: 1
rate: 20
frameNames:
  - nonexistent
`)},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(`version: 2
layers:
  - name: fx
    objects:
      - id: torch
        particles:
          file: fx/fire.particles.yaml
          atlas: fx
`)},
	}
	rep, err = assetcheck.Check(badFrameFS)
	if err != nil {
		t.Fatalf("Check(badFrame): %v", err)
	}
	if rep.OK() || !strings.Contains(rep.Problems[0].Message, "nonexistent") {
		t.Fatalf("expected problem naming nonexistent frame, got: %v", rep.Problems)
	}

	// Invalid emitter YAML is reported against the emitter file.
	badYAMLFS := fstest.MapFS{
		"fx/broken.particles.yaml": &fstest.MapFile{Data: []byte("version: 99\n")},
		"scenes/level.scene.yaml": &fstest.MapFile{Data: []byte(`version: 2
layers:
  - name: fx
    objects:
      - id: torch
        particles:
          file: fx/broken.particles.yaml
`)},
	}
	rep, err = assetcheck.Check(badYAMLFS)
	if err != nil {
		t.Fatalf("Check(badYAML): %v", err)
	}
	if rep.OK() || rep.Problems[0].File != "fx/broken.particles.yaml" {
		t.Fatalf("expected problem on fx/broken.particles.yaml, got: %v", rep.Problems)
	}
}

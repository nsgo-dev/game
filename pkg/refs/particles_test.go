package refs_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/refs"
)

func TestSceneParticlesRefScannedAndRewrittenOnMove(t *testing.T) {
	fsys := fstest.MapFS{
		"fx/fire.particles.yaml": &fstest.MapFile{Data: []byte("version: 1\nrate: 20\n")},
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

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := false
	for _, r := range proj.Refs {
		if r.File == "scenes/level.scene.yaml" && r.Field == refs.FieldParticles && r.Target == "fx/fire.particles.yaml" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected FieldParticles ref to fx/fire.particles.yaml; got %+v", proj.Refs)
	}

	plan, err := refs.PlanMove(fsys, "fx/fire.particles.yaml", "vfx/torch.particles.yaml")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	rewrote := false
	for _, w := range plan.Writes {
		if w.Path == "scenes/level.scene.yaml" && strings.Contains(string(w.Data), "file: vfx/torch.particles.yaml") {
			rewrote = true
		}
	}
	if !rewrote {
		t.Fatalf("expected PlanMove to rewrite particles.file in scene; writes = %+v", plan.Writes)
	}
}

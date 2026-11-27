package refs_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/refs"
)

func TestSceneSkyHDRRefScanAndMove(t *testing.T) {
	scene := `version: 4
name: Studio
environment:
  sky:
    kind: hdr
    hdr: env/studio.hdr
layers:
  - name: world
`
	fsys := fstest.MapFS{
		"scenes/studio.scene.yaml": {Data: []byte(scene)},
		"env/studio.hdr":           {Data: []byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n\x80\x80\x80\x80")},
	}
	p, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("Check problems = %v, want none", probs)
	}
	found := false
	for _, r := range p.Refs {
		if r.File == "scenes/studio.scene.yaml" && r.Field == refs.FieldSkyHDR && r.Target == "env/studio.hdr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Scan did not record FieldSkyHDR ref: %+v", p.Refs)
	}

	dir := dirFS(t, fsys)
	pl := move(t, dir, "env/studio.hdr", "env/hdri/studio_v2.hdr")
	if len(pl.Updated) != 1 || pl.Updated[0].File != "scenes/studio.scene.yaml" {
		t.Fatalf("PlanMove updated = %+v, want 1 update on scenes/studio.scene.yaml", pl.Updated)
	}
	rewritten := read(t, dir, "scenes/studio.scene.yaml")
	if !strings.Contains(rewritten, "hdr: env/hdri/studio_v2.hdr") {
		t.Fatalf("rewritten scene does not update hdr path:\n%s", rewritten)
	}
	clean(t, dir)

	// Missing HDR file is reported by Check.
	delete(fsys, "env/studio.hdr")
	p2, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan missing: %v", err)
	}
	probs := p2.Check()
	if len(probs) != 1 || !strings.Contains(probs[0].Message, "env/studio.hdr") {
		t.Fatalf("Check missing HDR = %v, want problem naming env/studio.hdr", probs)
	}
}

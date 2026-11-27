package refs_test

import (
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/refs"
)

func TestScanReflectionAndLightProbeRefs(t *testing.T) {
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte("version: 1\nname: ProbeRefs\nid: dev.example.probes\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n")},
		"scenes/base.scene.yaml": {Data: []byte(`version: 4
layers:
  - name: world
    objects:
      - id: refl
        reflectionProbe:
          size: [8, 4, 8]
          cubemap: probes/room.hdr
      - id: grid
        lightProbeGrid:
          size: [8, 4, 8]
          counts: [2, 2, 2]
          data: probes/room.probes.yaml
`)},
		"scenes/variant.scene.yaml": {Data: []byte(`version: 4
inherits: scenes/base.scene.yaml
overrides:
  refl:
    reflectionProbe:
      size: [8, 4, 8]
      cubemap: probes/night.hdr
  grid:
    lightProbeGrid:
      size: [8, 4, 8]
      counts: [2, 2, 2]
      data: probes/night.probes.yaml
layers: []
`)},
		"probes/room.hdr":          {Data: []byte("stub")},
		"probes/room.probes.yaml":  {Data: []byte("stub")},
		"probes/night.hdr":         {Data: []byte("stub")},
		"probes/night.probes.yaml": {Data: []byte("stub")},
	}
	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := map[string]string{
		"probes/room.hdr":          refs.FieldReflectionProbe,
		"probes/room.probes.yaml":  refs.FieldLightProbeGrid,
		"probes/night.hdr":         refs.FieldReflectionProbe,
		"probes/night.probes.yaml": refs.FieldLightProbeGrid,
	}
	for _, r := range proj.Refs {
		if f, ok := want[r.Target]; ok && r.Field == f {
			delete(want, r.Target)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing probe refs: %v (got %+v)", want, proj.Refs)
	}
	if probs := proj.Check(); len(probs) != 0 {
		t.Fatalf("Check problems = %v, want none", probs)
	}
}

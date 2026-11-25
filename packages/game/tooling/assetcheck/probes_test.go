package assetcheck_test

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

func TestCheckReflectionProbeAndLightProbeGridAssets(t *testing.T) {
	cube, err := render3d.NewHDRCubemap(2)
	if err != nil {
		t.Fatalf("NewHDRCubemap: %v", err)
	}
	var hdrBuf bytes.Buffer
	if err := render3d.EncodeCubemapHDR(&hdrBuf, cube); err != nil {
		t.Fatalf("EncodeCubemapHDR: %v", err)
	}
	grid, err := render3d.NewLightProbeGrid(math3d.AABB{Min: math3d.Vec3{X: -2, Y: 0, Z: -2}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2}}, 2, 1, 1)
	if err != nil {
		t.Fatalf("NewLightProbeGrid: %v", err)
	}
	gridDoc, err := render3d.LightProbeGridToDoc(grid)
	if err != nil {
		t.Fatalf("LightProbeGridToDoc: %v", err)
	}
	gridYAML, err := render3d.EncodeLightProbeGrid(gridDoc)
	if err != nil {
		t.Fatalf("EncodeLightProbeGrid: %v", err)
	}

	sceneYAML := `version: 4
layers:
  - name: world
    objects:
      - id: refl
        reflectionProbe:
          size: [4, 2, 4]
          cubemap: probes/room.hdr
      - id: grid
        lightProbeGrid:
          size: [4, 2, 4]
          counts: [2, 1, 1]
          data: probes/room.probes.yaml
`

	// Valid baked files pass with zero problems.
	goodFS := fstest.MapFS{
		"game.yaml":               {Data: []byte("version: 1\nname: ProbeCheck\nid: dev.example.probes\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n")},
		"scenes/room.scene.yaml":  {Data: []byte(sceneYAML)},
		"probes/room.hdr":         {Data: hdrBuf.Bytes()},
		"probes/room.probes.yaml": {Data: gridYAML},
	}
	rep, err := assetcheck.Check(goodFS)
	if err != nil {
		t.Fatalf("Check good: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("Check good problems = %v, want none", rep.Problems)
	}

	// Corrupt HDR cubemap and corrupt light probe YAML are reported against their files.
	badFS := fstest.MapFS{
		"game.yaml":               {Data: []byte("version: 1\nname: ProbeCheck\nid: dev.example.probes\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n")},
		"scenes/room.scene.yaml":  {Data: []byte(sceneYAML)},
		"probes/room.hdr":         {Data: []byte("not an hdr")},
		"probes/room.probes.yaml": {Data: []byte("version: 1\nunknownKey: 1\n")},
	}
	badRep, err := assetcheck.Check(badFS)
	if err != nil {
		t.Fatalf("Check bad: %v", err)
	}
	var sawHDR, sawGrid bool
	for _, p := range badRep.Problems {
		if p.File == "probes/room.hdr" {
			sawHDR = true
		}
		if p.File == "probes/room.probes.yaml" && strings.Contains(p.Message, "unknownKey") {
			sawGrid = true
		}
	}
	if !sawHDR || !sawGrid {
		t.Fatalf("expected problems on both probe files, got %v", badRep.Problems)
	}
}

package render3d_test

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/nsgo/gpu"
)

func TestCubemapHDRStripRoundTripAndEquirectFallback(t *testing.T) {
	cm, err := render3d.NewHDRCubemap(4)
	if err != nil {
		t.Fatalf("NewHDRCubemap: %v", err)
	}
	faceColors := [6][3]float32{
		{3.5, 0.25, 0.125}, // +X HDR red
		{0.1, 2.25, 0.2},   // -X HDR green
		{0.2, 0.4, 4.0},    // +Y HDR blue
		{0.5, 0.5, 0.5},    // -Y grey
		{1.5, 1.0, 0.0},    // +Z warm
		{0.0, 1.0, 1.5},    // -Z cool
	}
	for face := range gpu.CubeFace(6) {
		for y := range 4 {
			for x := range 4 {
				cm.Set(face, x, y, faceColors[face])
			}
		}
	}

	var buf1, buf2 bytes.Buffer
	if err := render3d.EncodeCubemapHDR(&buf1, cm); err != nil {
		t.Fatalf("EncodeCubemapHDR: %v", err)
	}
	if err := render3d.EncodeCubemapHDR(&buf2, cm); err != nil {
		t.Fatalf("EncodeCubemapHDR second: %v", err)
	}
	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatal("EncodeCubemapHDR is not deterministic")
	}

	decoded, err := render3d.DecodeCubemapHDR(bytes.NewReader(buf1.Bytes()))
	if err != nil {
		t.Fatalf("DecodeCubemapHDR: %v", err)
	}
	if decoded.Size != cm.Size {
		t.Fatalf("decoded.Size = %d, want %d", decoded.Size, cm.Size)
	}
	for face := range gpu.CubeFace(6) {
		got := decoded.At(face, 2, 2)
		want := faceColors[face]
		for ch := range 3 {
			tol := float32(math.Max(0.05, float64(want[ch])*0.02))
			if diff := float32(math.Abs(float64(got[ch] - want[ch]))); diff > tol {
				t.Errorf("face %d ch %d = %v, want %v (diff %v > %v)", face, ch, got[ch], want[ch], diff, tol)
			}
		}
	}

	// Equirectangular HDR (2:1 aspect, not 6:1) falls back cleanly to equirect projection.
	eq, err := render3d.NewHDRImage(8, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range eq.RGB {
		eq.RGB[i] = [3]float32{0.8, 0.4, 0.2}
	}
	var eqBuf bytes.Buffer
	if err := render3d.EncodeHDR(&eqBuf, eq); err != nil {
		t.Fatal(err)
	}
	eqCube, err := render3d.DecodeCubemapHDR(bytes.NewReader(eqBuf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeCubemapHDR equirect: %v", err)
	}
	if eqCube.Size != 2 {
		t.Fatalf("equirect faceSize = %d, want 2", eqCube.Size)
	}
}

func TestLightProbeGridYAMLCodecAndFS(t *testing.T) {
	grid, err := render3d.NewLightProbeGrid(math3d.AABB{
		Min: math3d.Vec3{X: -4, Y: 0, Z: -2},
		Max: math3d.Vec3{X: 4, Y: 3, Z: 2},
	}, 2, 1, 2)
	if err != nil {
		t.Fatalf("NewLightProbeGrid: %v", err)
	}
	if err := grid.Bake(4, func(pos, dir math3d.Vec3) [3]float32 {
		if pos.X > 0 {
			return [3]float32{1.2, 0.3, 0.2}
		}
		return [3]float32{0.2, 0.4, 1.1}
	}); err != nil {
		t.Fatalf("Bake: %v", err)
	}

	doc, err := render3d.LightProbeGridToDoc(grid)
	if err != nil {
		t.Fatalf("LightProbeGridToDoc: %v", err)
	}
	raw, err := render3d.EncodeLightProbeGrid(doc)
	if err != nil {
		t.Fatalf("EncodeLightProbeGrid: %v", err)
	}
	fsys := fstest.MapFS{"probes/room.probes.yaml": &fstest.MapFile{Data: raw}}
	loaded, err := render3d.LoadLightProbeGridFS(fsys, "probes/room.probes.yaml")
	if err != nil {
		t.Fatalf("LoadLightProbeGridFS: %v", err)
	}
	if loaded.Bounds != grid.Bounds || loaded.Counts != grid.Counts {
		t.Fatalf("loaded metadata = (%+v, %+v), want (%+v, %+v)", loaded.Bounds, loaded.Counts, grid.Bounds, grid.Counts)
	}
	for i := range grid.Probes {
		for k := range 9 {
			for ch := range 3 {
				if diff := math.Abs(float64(loaded.Probes[i].Coeff[k][ch] - grid.Probes[i].Coeff[k][ch])); diff > 1e-5 {
					t.Fatalf("probe %d coeff %d ch %d = %v, want %v", i, k, ch, loaded.Probes[i].Coeff[k][ch], grid.Probes[i].Coeff[k][ch])
				}
			}
		}
	}

	// Validation refuses wrong version, mismatched probe count, and unknown fields.
	if _, err := render3d.ParseLightProbeGrid([]byte("version: 99\nboundsMin: [0,0,0]\nboundsMax: [1,1,1]\ncounts: [1,1,1]\nprobes: []\n")); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Errorf("wrong version err = %v, want ErrInvalidProbe", err)
	}
	if _, err := render3d.ParseLightProbeGrid([]byte("version: 1\nunknownField: 1\n")); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Errorf("unknown field err = %v, want ErrInvalidProbe", err)
	}
}

func TestLightProbeGridFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*render3d.LightProbeGridDoc]{
		Decode:     render3d.ParseLightProbeGrid,
		Encode:     render3d.EncodeLightProbeGrid,
		Files:      []string{"testdata/room.probes.yaml"},
		Doc:        "render3d#lightprobes",
		MaxVersion: render3d.LightProbeGridVersion,
	})
}

func TestHalfToFloat32(t *testing.T) {
	cases := []struct {
		half uint16
		want float32
	}{
		{0x0000, 0},
		{0x3C00, 1},
		{0x4000, 2},
		{0x3800, 0.5},
		{0xBC00, -1},
		{0x7BFF, 65504},
	}
	for _, tc := range cases {
		if got := render3d.HalfToFloat32(tc.half); got != tc.want {
			t.Errorf("HalfToFloat32(0x%04X) = %v, want %v", tc.half, got, tc.want)
		}
	}
}

package assetcheck_test

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

func validTinyHDR(t *testing.T) []byte {
	t.Helper()
	img, err := render3d.NewHDRImage(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	img.Set(0, 0, [3]float32{1, 1, 1})
	img.Set(1, 0, [3]float32{0.5, 0.7, 1.0})
	// Write minimal uncompressed Radiance RGBE image.
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 2\n")
	// RGBE for (1,1,1): mantissa 0.5*256=128, exp 128+1=129
	buf.Write([]byte{128, 128, 128, 129, 128, 180, 255, 128})
	return buf.Bytes()
}

func TestCheckSceneSkyHDR(t *testing.T) {
	scene := "version: 4\nname: Sky\nenvironment:\n  sky:\n    kind: hdr\n    hdr: env/sky.hdr\nlayers:\n  - name: world\n"
	t.Run("valid", func(t *testing.T) {
		fsys := fstest.MapFS{
			"scenes/sky.scene.yaml": {Data: []byte(scene)},
			"env/sky.hdr":           {Data: validTinyHDR(t)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !rep.OK() {
			t.Fatalf("unexpected problems: %v", rep.Problems)
		}
	})

	t.Run("missing", func(t *testing.T) {
		fsys := fstest.MapFS{
			"scenes/sky.scene.yaml": {Data: []byte(scene)},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if len(rep.Problems) != 1 || rep.Problems[0].File != "scenes/sky.scene.yaml" || rep.Problems[0].Line != 6 {
			t.Fatalf("problems = %v, want 1 problem at scenes/sky.scene.yaml:6", rep.Problems)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		fsys := fstest.MapFS{
			"scenes/sky.scene.yaml": {Data: []byte(scene)},
			"env/sky.hdr":           {Data: []byte("not an hdr file")},
		}
		rep, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if len(rep.Problems) != 1 || rep.Problems[0].File != "env/sky.hdr" || !strings.Contains(rep.Problems[0].Message, "RADIANCE") && !strings.Contains(rep.Problems[0].Message, "HDR") {
			t.Fatalf("problems = %v, want decode error on env/sky.hdr", rep.Problems)
		}
	})
}

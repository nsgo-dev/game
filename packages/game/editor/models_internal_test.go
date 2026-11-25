package editor

import (
	"math"
	"testing"
)

// alignUp3D turns +Y onto any unit normal, through the rotation order objects use.
func TestAlignUp3DTurnsUpOntoTheNormal(t *testing.T) {
	for _, n := range [][3]float64{
		{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1},
		{0.6, 0.8, 0}, {0.3, -0.4, 0.866}, {-0.5, 0.5, -0.7071},
	} {
		n = normalize64(n)
		rx, rz := alignUp3D(vec32(n))
		got := rotateEulerXYZ64([3]float64{0, 1, 0}, float64(rx), 0, float64(rz))
		for i := range 3 {
			if math.Abs(got[i]-n[i]) > 1e-6 {
				t.Fatalf("alignUp3D(%v) = (%v, %v) turns +Y to %v", n, rx, rz, got)
			}
		}
	}
}

// Thumbnails render off the UI goroutine: the first call starts one, and once it is rendered the model fills the middle of a transparent square; a model that does not load fails at once, and reloading the assets renders them again.
func TestModelThumbnailRendersOffscreen(t *testing.T) {
	ed := openCourtyard(t)
	crate := ModelEntry{Path: "models/crate.glb"}
	sphere := ModelEntry{Mesh: "sphere"}
	for _, m := range []ModelEntry{crate, sphere} {
		if img, err := ed.ModelThumbnail(m); img != nil || err != nil {
			t.Fatalf("first ModelThumbnail(%s) = %v, %v, want it pending", m.Key(), img != nil, err)
		}
	}
	ed.waitModelThumbnails()
	for _, m := range []ModelEntry{crate, sphere} {
		img, err := ed.ModelThumbnail(m)
		if err != nil || img == nil {
			t.Fatalf("ModelThumbnail(%s) after rendering = %v, %v", m.Key(), img, err)
		}
		if b := img.Bounds(); b.Dx() != ModelThumbnailSize || b.Dy() != ModelThumbnailSize {
			t.Fatalf("%s thumbnail is %v", m.Key(), b)
		}
		if c := img.RGBAAt(ModelThumbnailSize/2, ModelThumbnailSize/2); c.A != 255 {
			t.Errorf("%s thumbnail's middle is %v, want the model", m.Key(), c)
		}
		if c := img.RGBAAt(0, 0); c.A != 0 {
			t.Errorf("%s thumbnail's corner is %v, want it transparent", m.Key(), c)
		}
	}
	if _, err := ed.ModelThumbnail(ModelEntry{Path: "models/missing.glb"}); err == nil {
		t.Fatal("a missing model has a thumbnail")
	}

	ed.assetsRev++
	if img, err := ed.ModelThumbnail(crate); img != nil || err != nil {
		t.Fatal("a thumbnail outlived the assets it was rendered from")
	}
	ed.waitModelThumbnails()
	if img, err := ed.ModelThumbnail(crate); img == nil || err != nil {
		t.Fatalf("crate thumbnail after reloading = %v, %v", img != nil, err)
	}
}

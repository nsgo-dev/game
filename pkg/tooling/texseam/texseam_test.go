package texseam

import (
	"image"
	"image/color"
	"testing"
)

func TestCheckAndFix(t *testing.T) {
	tile := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			v := uint8(((x%7)*31 + (y%7)*17) % 256)
			tile.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	if got := Check(tile); !got.Pass {
		t.Fatalf("periodic tile failed: %+v", got)
	}
	bad := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				bad.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				bad.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	if got := Check(bad); got.Pass {
		t.Fatalf("hard seam passed: %+v", got)
	}
	if got := Check(Fix(bad, FixOptions{Band: 2})); !got.Pass {
		t.Fatalf("fixed tile failed: %+v", got)
	}
}

func TestMergePreservesOutsideCross(t *testing.T) {
	original := image.NewRGBA(image.Rect(0, 0, 8, 8))
	edited := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			original.SetRGBA(x, y, color.RGBA{10, 20, 30, 255})
			edited.SetRGBA(x, y, color.RGBA{200, 210, 220, 255})
		}
	}
	merged := Merge(original, edited, 1)
	if got := merged.RGBAAt(0, 0); got != original.RGBAAt(0, 0) {
		t.Fatalf("corner changed outside feathered cross: %v", got)
	}
	if got := merged.RGBAAt(4, 4); got == original.RGBAAt(4, 4) {
		t.Fatal("center was not merged")
	}
}

func TestDerivedMaps(t *testing.T) {
	height := image.NewGray(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			height.SetGray(x, y, color.Gray{Y: uint8(x * 100)})
		}
	}
	normal := Normal(height, 1, Clamp)
	if got := normal.NRGBAAt(1, 1); got.B <= got.R || got.R >= 128 {
		t.Fatalf("unexpected normal for rising ramp: %v", got)
	}
	rough := Roughness(height, 1)
	if got := rough.GrayAt(0, 1).Y; got == rough.GrayAt(2, 1).Y {
		t.Fatal("roughness did not invert height")
	}
}

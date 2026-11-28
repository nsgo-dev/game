package gametest

import (
	"image"
	"image/color"
	"testing"
)

func solid(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
func TestAnalyzePhotosensitivityFlashes(t *testing.T) {
	for name, hz := range map[string]float64{"safe": 2, "unsafe": 5} {
		frames := make([]image.Image, 60)
		period := int(60 / hz)
		for i := range frames {
			if i/period%2 == 0 {
				frames[i] = solid(color.RGBA{0, 0, 0, 255})
			} else {
				frames[i] = solid(color.RGBA{255, 255, 255, 255})
			}
		}
		r := AnalyzePhotosensitivity(frames, PhotosensitivityOptions{FrameRate: 60})
		if (len(r.Failures) > 0) != (name == "unsafe") {
			t.Fatalf("%s failures=%+v", name, r.Failures)
		}
	}
}
func TestAnalyzePhotosensitivityStripePattern(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if x%2 == 0 {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	r := AnalyzePhotosensitivity([]image.Image{img}, PhotosensitivityOptions{})
	if len(r.Failures) != 1 || r.Failures[0].Kind != "pattern" {
		t.Fatalf("report=%+v", r)
	}
}

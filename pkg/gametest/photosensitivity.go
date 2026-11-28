package gametest

import (
	"fmt"
	"image"
	"math"
)

// PhotosensitivityOptions controls frame analysis. FrameRate defaults to 60 when zero.
type PhotosensitivityOptions struct{ FrameRate float64 }

// PhotosensitivityFailure describes a detected flash or high-contrast pattern.
type PhotosensitivityFailure struct {
	Kind       string
	FirstFrame int
	LastFrame  int
	Summary    string
}

// PhotosensitivityReport contains failures found in a sequence of rendered frames.
type PhotosensitivityReport struct{ Failures []PhotosensitivityFailure }

// AnalyzePhotosensitivity checks rendered frames for repeated full-screen flashes and high-contrast stripe patterns.
func AnalyzePhotosensitivity(frames []image.Image, opts PhotosensitivityOptions) PhotosensitivityReport {
	fps := opts.FrameRate
	if fps <= 0 {
		fps = 60
	}
	var report PhotosensitivityReport
	if len(frames) == 0 {
		return report
	}
	lum := make([]float64, len(frames))
	changed := make([]bool, len(frames))
	for i, frame := range frames {
		lum[i], changed[i] = frameStats(frame)
		if i > 0 && changed[i] && math.Abs(lum[i]-lum[i-1]) >= 0.10 && (lum[i-1] < 0.8 || lum[i] < 0.8) {
			start := max(0, i-int(math.Ceil(fps)))
			count := 0
			for j := start + 1; j <= i; j++ {
				if changed[j] && math.Abs(lum[j]-lum[j-1]) >= 0.10 {
					count++
				}
			}
			if count > 3 {
				report.Failures = append(report.Failures, PhotosensitivityFailure{Kind: "flash", FirstFrame: start, LastFrame: i, Summary: fmt.Sprintf("%d full-frame luminance changes in one second", count)})
				break
			}
		}
		if stripes(frame) {
			report.Failures = append(report.Failures, PhotosensitivityFailure{Kind: "pattern", FirstFrame: i, LastFrame: i, Summary: "high-contrast stripe pattern covers at least 20% of the frame"})
		}
	}
	return report
}

func frameStats(img image.Image) (float64, bool) {
	if img == nil {
		return 0, false
	}
	b := img.Bounds()
	if b.Empty() {
		return 0, false
	}
	var sum float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += luminance(float64(r)/65535, float64(g)/65535, float64(bl)/65535)
		}
	}
	return sum / float64(b.Dx()*b.Dy()), true
}

func luminance(r, g, b float64) float64 { return 0.2126*r + 0.7152*g + 0.0722*b }

func stripes(img image.Image) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() < 4 || b.Dy() < 4 {
		return false
	}
	var edges int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X + 1; x < b.Max.X; x++ {
			a := img.At(x-1, y)
			c := img.At(x, y)
			ar, ag, ab, _ := a.RGBA()
			cr, cg, cb, _ := c.RGBA()
			if math.Abs(luminance(float64(ar)/65535, float64(ag)/65535, float64(ab)/65535)-luminance(float64(cr)/65535, float64(cg)/65535, float64(cb)/65535)) >= 0.5 {
				edges++
			}
		}
	}
	return float64(edges)/float64((b.Dx()-1)*b.Dy()) >= 0.2
}

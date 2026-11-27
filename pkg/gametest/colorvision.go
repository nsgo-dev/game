package gametest

import (
	"image"
	"image/draw"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/effects"
	"github.com/nsgo-dev/nsgo/core"
)

func toEffectsColorVision(mode a11y.ColorVision) effects.ColorVisionMode {
	switch mode {
	case a11y.VisionProtanopia:
		return effects.ColorVisionProtanopia
	case a11y.VisionDeuteranopia:
		return effects.ColorVisionDeuteranopia
	case a11y.VisionTritanopia:
		return effects.ColorVisionTritanopia
	case a11y.VisionHighContrast:
		return effects.ColorVisionHighContrast
	default:
		return effects.ColorVisionNone
	}
}

func cloneToRGBA(img image.Image) *image.RGBA {
	if img == nil {
		return image.NewRGBA(image.Rectangle{})
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if b.Empty() {
		return out
	}
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

// ApplyColorVision filters img through effects.ColorVisionEffect for mode at severity
// in [0, 1] (clamped, with NaN treated as 0). When correct is false, it simulates the
// deficiency; when correct is true, it applies daltonization correction. The input
// image is not mutated.
func ApplyColorVision(img image.Image, mode a11y.ColorVision, severity float64, correct bool) *image.RGBA {
	src := cloneToRGBA(img)
	b := src.Bounds()
	if b.Empty() || mode == a11y.VisionNormal || math.IsNaN(severity) || severity <= 0 {
		return src
	}
	chain := effects.MustCompile(effects.ColorVisionEffect(toEffectsColorVision(mode), severity, correct))
	out, err := chain.Reference(src, b.Dx(), b.Dy(), nil)
	if err != nil {
		return src
	}
	return out
}

// SimulateColorVision returns a copy of img filtered through color-vision deficiency
// simulation for mode at severity in [0, 1]. Passing severity == 1.0 matches full
// dichromacy simulation (a11y.Simulate).
func SimulateColorVision(img image.Image, mode a11y.ColorVision, severity float64) *image.RGBA {
	return ApplyColorVision(img, mode, severity, false)
}

// ColorDistance returns the Euclidean sRGB distance between a and b in 8-bit channel
// units [0, 441.67], ignoring alpha.
func ColorDistance(a, b core.Color) float64 {
	return a11y.ColorDistance(a, b)
}

// AssertColorDistinguishable simulates a and b under mode and fails t if their
// Euclidean sRGB distance is smaller than minDistance.
func AssertColorDistinguishable(t testing.TB, mode a11y.ColorVision, a, b core.Color, minDistance float64) {
	t.Helper()
	sa := a11y.Simulate(a, mode)
	sb := a11y.Simulate(b, mode)
	dist := a11y.ColorDistance(sa, sb)
	if dist < minDistance {
		t.Fatalf("colors %#08x and %#08x are too close under %s simulation: simulated %#08x vs %#08x, distance %.1f < min %.1f",
			uint32(a), uint32(b), mode, uint32(sa), uint32(sb), dist, minDistance)
	}
}

// AverageRegionColor returns the mean straight sRGB color of opaque/translucent pixels
// inside region (intersected with img's bounds). Returns core.Transparent when the
// region is empty or fully transparent.
func AverageRegionColor(img image.Image, region image.Rectangle) core.Color {
	if img == nil {
		return core.Transparent
	}
	r := region.Intersect(img.Bounds())
	if r.Empty() {
		return core.Transparent
	}
	var sumR, sumG, sumB, sumA float64
	var weight float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA()
			if pa == 0 {
				continue
			}
			a := float64(pa) / 65535.0
			sr := math.Min(float64(pr)/float64(pa), 1.0)
			sg := math.Min(float64(pg)/float64(pa), 1.0)
			sb := math.Min(float64(pb)/float64(pa), 1.0)
			sumR += sr * a
			sumG += sg * a
			sumB += sb * a
			sumA += a
			weight += a
		}
	}
	if weight <= 0 {
		return core.Transparent
	}
	rb := uint32(math.Round(clamp01(sumR/weight) * 255))
	gb := uint32(math.Round(clamp01(sumG/weight) * 255))
	bb := uint32(math.Round(clamp01(sumB/weight) * 255))
	ab := uint32(math.Round(clamp01(sumA/float64(r.Dx()*r.Dy())) * 255))
	return core.Color(rb<<24 | gb<<16 | bb<<8 | ab)
}

// AssertRegionsDistinguishable simulates img under mode (when mode is not VisionNormal)
// and fails t if the average colors of regionA and regionB have Euclidean sRGB distance
// less than minDistance.
func AssertRegionsDistinguishable(t testing.TB, img image.Image, regionA, regionB image.Rectangle, mode a11y.ColorVision, minDistance float64) {
	t.Helper()
	sim := SimulateColorVision(img, mode, 1.0)
	ca := AverageRegionColor(sim, regionA)
	cb := AverageRegionColor(sim, regionB)
	dist := a11y.ColorDistance(ca, cb)
	if dist < minDistance {
		t.Fatalf("regions %v (%#08x) and %v (%#08x) are too close under %s simulation: distance %.1f < min %.1f",
			regionA, uint32(ca), regionB, uint32(cb), mode, dist, minDistance)
	}
}

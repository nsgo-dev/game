package a11y

import (
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// ColorVision selects a color-vision mode for palettes and CVD simulation.
type ColorVision int

const (
	// VisionNormal is standard trichromatic vision with a color-blind-aware
	// Okabe-Ito inspired base palette.
	VisionNormal ColorVision = iota
	// VisionProtanopia adapts colors for red-blind (L-cone deficient) vision.
	VisionProtanopia
	// VisionDeuteranopia adapts colors for green-blind (M-cone deficient) vision.
	VisionDeuteranopia
	// VisionTritanopia adapts colors for blue-blind (S-cone deficient) vision.
	VisionTritanopia
	// VisionHighContrast provides maximum luminance separation (WCAG AAA).
	VisionHighContrast
)

// String returns the canonical lowercase identifier for v.
func (v ColorVision) String() string {
	switch v {
	case VisionNormal:
		return "normal"
	case VisionProtanopia:
		return "protanopia"
	case VisionDeuteranopia:
		return "deuteranopia"
	case VisionTritanopia:
		return "tritanopia"
	case VisionHighContrast:
		return "high-contrast"
	default:
		return "normal"
	}
}

// ParseColorVision parses a ColorVision name returned by String.
func ParseColorVision(s string) (ColorVision, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "normal":
		return VisionNormal, true
	case "protanopia":
		return VisionProtanopia, true
	case "deuteranopia":
		return VisionDeuteranopia, true
	case "tritanopia":
		return VisionTritanopia, true
	case "high-contrast", "highcontrast", "high_contrast":
		return VisionHighContrast, true
	default:
		return VisionNormal, false
	}
}

// Palette holds semantic game and UI colors designed to remain distinguishable
// and readable together.
type Palette struct {
	Player     core.Color
	Ally       core.Color
	Enemy      core.Color
	Hazard     core.Color
	Pickup     core.Color
	Neutral    core.Color
	Background core.Color
	Foreground core.Color
	Highlight  core.Color
	Accent     core.Color
}

// PaletteFor returns a color-blind-safe preset Palette for mode, grounded in
// the Okabe-Ito / Wong palette and tuned for WCAG contrast and post-simulation
// separability.
func PaletteFor(mode ColorVision) Palette {
	switch mode {
	case VisionProtanopia:
		// Separate along the blue–yellow–luminance plane where L-cone loss
		// preserves full discrimination.
		return Palette{
			Player:     core.RGB(0x56B4E9), // Okabe-Ito sky blue
			Ally:       core.RGB(0x5CE1B8), // bright mint-teal (high luminance, distinct from white & blue)
			Enemy:      core.RGB(0xE69F00), // Okabe-Ito orange (rich gold under protanopia)
			Hazard:     core.RGB(0xFFF14D), // peak-luminance lemon yellow
			Pickup:     core.RGB(0x3B7BF6), // deep saturated blue
			Neutral:    core.RGB(0x808896), // neutral mid gray (>= 5:1 vs background)
			Background: core.RGB(0x0E1117),
			Foreground: core.RGB(0xF7F9FC),
			Highlight:  core.RGB(0xFFF14D),
			Accent:     core.RGB(0x70C0FF),
		}
	case VisionDeuteranopia:
		// Separate along the blue–yellow–luminance plane where M-cone loss
		// preserves full discrimination.
		return Palette{
			Player:     core.RGB(0x56B4E9), // Okabe-Ito sky blue
			Ally:       core.RGB(0x5CE1B8), // bright mint-teal (high luminance, distinct from white & blue)
			Enemy:      core.RGB(0xE68A00), // deep amber-orange
			Hazard:     core.RGB(0xF7EC4D), // Okabe-Ito bright yellow
			Pickup:     core.RGB(0x3A75F5), // vivid royal blue
			Neutral:    core.RGB(0x808896), // neutral mid gray (>= 5:1 vs background)
			Background: core.RGB(0x0E1117),
			Foreground: core.RGB(0xF7F9FC),
			Highlight:  core.RGB(0xF7EC4D),
			Accent:     core.RGB(0x70C0FF),
		}
	case VisionTritanopia:
		// Separate along the red/rose–cyan/teal–luminance plane where S-cone
		// loss preserves full discrimination.
		return Palette{
			Player:     core.RGB(0x00D4DE), // vivid cyan
			Ally:       core.RGB(0x9CF7F0), // high-luminance mint aqua
			Enemy:      core.RGB(0xFF4D4D), // pure vivid red
			Hazard:     core.RGB(0xFF9AC2), // high-luminance bright rose
			Pickup:     core.RGB(0x289E9E), // deeper teal
			Neutral:    core.RGB(0xA6AEB8), // neutral gray
			Background: core.RGB(0x101316),
			Foreground: core.RGB(0xF8FAFC),
			Highlight:  core.RGB(0xFF8A80),
			Accent:     core.RGB(0x38DCE6),
		}
	case VisionHighContrast:
		// Maximum luminance separation (21:1 foreground/background, AAA roles).
		return Palette{
			Player:     core.RGB(0x00E5FF),
			Ally:       core.RGB(0x76FF03),
			Enemy:      core.RGB(0xFF5252),
			Hazard:     core.RGB(0xFFEA00),
			Pickup:     core.RGB(0xFF80AB),
			Neutral:    core.RGB(0xD0D0D0),
			Background: core.RGB(0x000000),
			Foreground: core.RGB(0xFFFFFF),
			Highlight:  core.RGB(0xFFFF00),
			Accent:     core.RGB(0x40C4FF),
		}
	default: // VisionNormal (Okabe-Ito / Wong dark-background safe palette)
		return Palette{
			Player:     core.RGB(0x56B4E9), // Okabe-Ito sky blue
			Ally:       core.RGB(0x00BA88), // Okabe-Ito bluish green
			Enemy:      core.RGB(0xF06430), // Okabe-Ito vermilion
			Hazard:     core.RGB(0xF0E442), // Okabe-Ito yellow
			Pickup:     core.RGB(0xE69F00), // Okabe-Ito orange
			Neutral:    core.RGB(0x9AA3B2), // neutral cool gray
			Background: core.RGB(0x111318),
			Foreground: core.RGB(0xF4F6F9),
			Highlight:  core.RGB(0xF5D042),
			Accent:     core.RGB(0xD982B5), // Okabe-Ito reddish purple
		}
	}
}

// cvdMatrix is a 3x3 linear-RGB color-vision deficiency transform matrix.
type cvdMatrix [3][3]float64

// Linear-RGB dichromacy simulation matrices (Viénot, Brettel & Mollon 1999 for
// protanopia and deuteranopia; Machado, Oliveira & Fernandes 2009 severity 1.0
// for tritanopia). Each row sums to 1.0 so neutral grays remain invariant.
var (
	protanopiaMatrix = cvdMatrix{
		{0.152286, 1.052583, -0.204868},
		{0.114503, 0.786281, 0.099216},
		{-0.003882, -0.048116, 1.051998},
	}
	deuteranopiaMatrix = cvdMatrix{
		{0.367322, 0.860646, -0.227968},
		{0.280085, 0.672501, 0.047413},
		{-0.011820, 0.042940, 0.968881},
	}
	tritanopiaMatrix = cvdMatrix{
		{1.255528, -0.076749, -0.178779},
		{-0.078411, 0.930809, 0.147602},
		{0.004733, 0.691367, 0.303900},
	}
)

// Simulate approximates how c appears under color-vision mode using linear-RGB
// Viénot/Brettel/Machado dichromacy matrices. Alpha is preserved. VisionNormal
// and VisionHighContrast return c unchanged.
func Simulate(c core.Color, mode ColorVision) core.Color {
	var m cvdMatrix
	switch mode {
	case VisionProtanopia:
		m = protanopiaMatrix
	case VisionDeuteranopia:
		m = deuteranopiaMatrix
	case VisionTritanopia:
		m = tritanopiaMatrix
	default:
		return c
	}

	r, g, b, _ := c.Components()
	lr, lg, lb := srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)

	sr := clamp01(m[0][0]*lr + m[0][1]*lg + m[0][2]*lb)
	sg := clamp01(m[1][0]*lr + m[1][1]*lg + m[1][2]*lb)
	sb := clamp01(m[2][0]*lr + m[2][1]*lg + m[2][2]*lb)

	return packColor(linearToSRGB(sr), linearToSRGB(sg), linearToSRGB(sb), uint32(c&0xff))
}

// ContrastRatio returns the WCAG 2.1 relative luminance contrast ratio between
// fg and bg in [1.0, 21.0]. Semi-transparent foregrounds are composited over bg.
func ContrastRatio(fg, bg core.Color) float64 {
	if fg&0xff < 0xff {
		fg = fg.Over(bg)
	}
	l1 := relativeLuminance(fg)
	l2 := relativeLuminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// EnsureContrast adjusts fg's lightness until ContrastRatio(result, bg) meets
// minRatio while preserving fg's hue as closely as possible and keeping its
// alpha channel. If fg already meets minRatio (or minRatio <= 1), fg is
// returned unchanged. If minRatio exceeds the maximum contrast achievable
// against bg, the higher-contrast extreme (white or black) is returned.
func EnsureContrast(fg, bg core.Color, minRatio float64) core.Color {
	if minRatio <= 1 || ContrastRatio(fg, bg) >= minRatio {
		return fg
	}
	alpha := uint32(fg & 0xff)
	white := core.Color(0xffffff00 | alpha)
	black := core.Color(0x00000000 | alpha)

	// Try moving toward whichever extreme gives higher contrast first, falling
	// back to the other extreme if needed.
	primary, secondary := white, black
	if ContrastRatio(black, bg) > ContrastRatio(white, bg) {
		primary, secondary = black, white
	}

	if res, ok := searchContrast(fg, primary, bg, minRatio); ok {
		return res
	}
	if res, ok := searchContrast(fg, secondary, bg, minRatio); ok {
		return res
	}
	return primary
}

func searchContrast(fg, target, bg core.Color, minRatio float64) (core.Color, bool) {
	if ContrastRatio(target, bg) < minRatio {
		return target, false
	}
	r0, g0, b0, _ := fg.Components()
	r1, g1, b1, _ := target.Components()
	alpha := uint32(fg & 0xff)

	lo, hi := 0.0, 1.0
	best := target
	for range 24 {
		mid := (lo + hi) * 0.5
		cand := packColor(
			r0+(r1-r0)*mid,
			g0+(g1-g0)*mid,
			b0+(b1-b0)*mid,
			alpha,
		)
		if ContrastRatio(cand, bg) >= minRatio {
			best = cand
			hi = mid
		} else {
			lo = mid
		}
	}
	return best, true
}

func relativeLuminance(c core.Color) float64 {
	r, g, b, _ := c.Components()
	return 0.2126*srgbToLinear(r) + 0.7152*srgbToLinear(g) + 0.0722*srgbToLinear(b)
}

func srgbToLinear(u float64) float64 {
	if u <= 0.04045 {
		return u / 12.92
	}
	return math.Pow((u+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func packColor(r, g, b float64, alpha uint32) core.Color {
	rb := uint32(math.Round(clamp01(r) * 255))
	gb := uint32(math.Round(clamp01(g) * 255))
	bb := uint32(math.Round(clamp01(b) * 255))
	return core.Color(rb<<24 | gb<<16 | bb<<8 | (alpha & 0xff))
}

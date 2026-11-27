package effects

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// ColorVisionMode selects a color-vision deficiency or contrast filter for ColorVisionEffect.
type ColorVisionMode int

const (
	// ColorVisionNone passes the frame through unchanged.
	ColorVisionNone ColorVisionMode = iota
	// ColorVisionProtanopia targets red-blind (L-cone deficient) vision.
	ColorVisionProtanopia
	// ColorVisionDeuteranopia targets green-blind (M-cone deficient) vision.
	ColorVisionDeuteranopia
	// ColorVisionTritanopia targets blue-blind (S-cone deficient) vision.
	ColorVisionTritanopia
	// ColorVisionHighContrast boosts tonal separation when correcting; simulation leaves the frame unchanged.
	ColorVisionHighContrast
)

// String returns the canonical lowercase name of m.
func (m ColorVisionMode) String() string {
	switch m {
	case ColorVisionNone:
		return "none"
	case ColorVisionProtanopia:
		return "protanopia"
	case ColorVisionDeuteranopia:
		return "deuteranopia"
	case ColorVisionTritanopia:
		return "tritanopia"
	case ColorVisionHighContrast:
		return "high-contrast"
	default:
		return "none"
	}
}

// ParseColorVisionMode parses a ColorVisionMode name (accepting "none" and "normal"
// as ColorVisionNone, and "high-contrast", "highcontrast", or "high_contrast" as
// ColorVisionHighContrast).
func ParseColorVisionMode(s string) (ColorVisionMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "normal":
		return ColorVisionNone, true
	case "protanopia":
		return ColorVisionProtanopia, true
	case "deuteranopia":
		return ColorVisionDeuteranopia, true
	case "tritanopia":
		return ColorVisionTritanopia, true
	case "high-contrast", "highcontrast", "high_contrast":
		return ColorVisionHighContrast, true
	default:
		return ColorVisionNone, false
	}
}

// Linear-RGB dichromacy simulation matrices (Viénot, Brettel & Mollon 1999 for
// protanopia and deuteranopia; Machado, Oliveira & Fernandes 2009 severity 1.0
// for tritanopia), matching pkg/a11y.Simulate. Each row sums to 1.0 so neutral
// grays remain invariant.
var (
	cvdIdentityMatrix = [3][3]float64{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	cvdProtanopiaSim = [3][3]float64{
		{0.152286, 1.052583, -0.204868},
		{0.114503, 0.786281, 0.099216},
		{-0.003882, -0.048116, 1.051998},
	}
	cvdDeuteranopiaSim = [3][3]float64{
		{0.367322, 0.860646, -0.227968},
		{0.280085, 0.672501, 0.047413},
		{-0.011820, 0.042940, 0.968881},
	}
	cvdTritanopiaSim = [3][3]float64{
		{1.255528, -0.076749, -0.178779},
		{-0.078411, 0.930809, 0.147602},
		{0.004733, 0.691367, 0.303900},
	}

	// Fidaner-style error-redistribution matrices for daltonization:
	// lost contrast e = c - S*c is shifted into channels the viewer can perceive.
	// Because (I - S)*gray = 0, every daltonization matrix preserves neutral grays.
	cvdRedGreenShift = [3][3]float64{
		{0.0, 0.0, 0.0},
		{0.7, 1.0, 0.0},
		{0.7, 0.0, 1.0},
	}
	cvdTritanShift = [3][3]float64{
		{1.0, 0.0, 0.7},
		{0.0, 1.0, 0.7},
		{0.0, 0.0, 0.0},
	}
)

// ColorVisionMatrix returns the 3x3 linear-RGB matrix for mode at severity in [0, 1]
// (clamped, with NaN treated as 0). When correct is false, it interpolates from the
// identity matrix to the dichromacy simulation matrix matching a11y.Simulate. When
// correct is true, it returns the daltonization matrix I + severity * E * (I - S)
// that shifts lost chromatic contrast into channels the viewer can distinguish.
// ColorVisionNone, ColorVisionHighContrast, and severity <= 0 return the identity matrix.
func ColorVisionMatrix(mode ColorVisionMode, severity float64, correct bool) [3][3]float64 {
	s := saturate(severity)
	if s <= 0 {
		return cvdIdentityMatrix
	}
	var sim, shift [3][3]float64
	switch mode {
	case ColorVisionProtanopia:
		sim, shift = cvdProtanopiaSim, cvdRedGreenShift
	case ColorVisionDeuteranopia:
		sim, shift = cvdDeuteranopiaSim, cvdRedGreenShift
	case ColorVisionTritanopia:
		sim, shift = cvdTritanopiaSim, cvdTritanShift
	default:
		return cvdIdentityMatrix
	}
	if !correct {
		var m [3][3]float64
		for r := range 3 {
			for c := range 3 {
				id := 0.0
				if r == c {
					id = 1.0
				}
				m[r][c] = id + s*(sim[r][c]-id)
			}
		}
		return m
	}

	// M = I + s * shift * (I - sim)
	var errMat [3][3]float64
	for r := range 3 {
		for c := range 3 {
			id := 0.0
			if r == c {
				id = 1.0
			}
			errMat[r][c] = id - sim[r][c]
		}
	}
	var out [3][3]float64
	for r := range 3 {
		for c := range 3 {
			sum := 0.0
			for k := range 3 {
				sum += shift[r][k] * errMat[k][c]
			}
			id := 0.0
			if r == c {
				id = 1.0
			}
			out[r][c] = id + s*sum
		}
	}
	return out
}

const colorVisionPassThroughSource = `package main

func Effect(pos vec2) vec4 {
	return Input(pos)
}
`

// ColorVisionEffect returns a frame effect that either simulates a color-vision
// deficiency (correct == false, using the same linear-RGB dichromacy matrices as
// a11y.Simulate) or applies daltonization correction (correct == true, shifting lost
// contrast into channels the viewer can distinguish). For ColorVisionHighContrast,
// correct == true boosts sRGB contrast around mid-gray by up to 50%, while
// simulation passes the frame through unchanged (matching a11y.Simulate).
//
// severity is clamped to [0, 1] (NaN acts as 0). When mode is ColorVisionNone,
// severity <= 0, or mode is ColorVisionHighContrast with correct == false, the
// returned effect passes its input through unchanged. Colors stay premultiplied and
// each pixel's alpha is preserved. The effect carries a CPU Reference so a chain of
// it draws reference pictures headless.
func ColorVisionEffect(mode ColorVisionMode, severity float64, correct bool) Effect {
	s := saturate(severity)
	if s <= 0 || mode == ColorVisionNone || (mode == ColorVisionHighContrast && !correct) {
		return Effect{
			Name:   "color-vision",
			Source: colorVisionPassThroughSource,
			Reference: func(in Input, _ core.Uniforms, x, y float64) [4]float64 {
				return in.At(x, y)
			},
		}
	}

	if mode == ColorVisionHighContrast {
		scale := 1.0 + 0.5*s
		src := fmt.Sprintf(`package main

func Effect(pos vec2) vec4 {
	c := Input(pos)
	if !(c.a > 0) {
		return c
	}
	rgb := clamp(c.rgb/c.a, 0.0, 1.0)
	out := clamp((rgb-vec3(0.5))*%.9f+vec3(0.5), 0.0, 1.0)
	return vec4(out*c.a, c.a)
}
`, scale)
		return Effect{
			Name:   "color-vision",
			Source: src,
			Reference: func(in Input, _ core.Uniforms, x, y float64) [4]float64 {
				c := in.At(x, y)
				if !(c[3] > 0) {
					return c
				}
				a := c[3]
				r := saturate((saturate(c[0]/a)-0.5)*scale + 0.5)
				g := saturate((saturate(c[1]/a)-0.5)*scale + 0.5)
				b := saturate((saturate(c[2]/a)-0.5)*scale + 0.5)
				return [4]float64{r * a, g * a, b * a, a}
			},
		}
	}

	m := ColorVisionMatrix(mode, s, correct)
	src := fmt.Sprintf(`package main

func srgbToLinear(u float) float {
	if u <= 0.04045 {
		return u / 12.92
	}
	return pow((u+0.055)/1.055, 2.4)
}

func linearToSRGB(u float) float {
	if u <= 0.0031308 {
		return 12.92 * u
	}
	return 1.055*pow(u, 1.0/2.4) - 0.055
}

func Effect(pos vec2) vec4 {
	c := Input(pos)
	if !(c.a > 0) {
		return c
	}
	rgb := clamp(c.rgb/c.a, 0.0, 1.0)
	lr := srgbToLinear(rgb.r)
	lg := srgbToLinear(rgb.g)
	lb := srgbToLinear(rgb.b)
	sr := clamp(%.9f*lr+%.9f*lg+%.9f*lb, 0.0, 1.0)
	sg := clamp(%.9f*lr+%.9f*lg+%.9f*lb, 0.0, 1.0)
	sb := clamp(%.9f*lr+%.9f*lg+%.9f*lb, 0.0, 1.0)
	out := vec3(
		clamp(linearToSRGB(sr), 0.0, 1.0),
		clamp(linearToSRGB(sg), 0.0, 1.0),
		clamp(linearToSRGB(sb), 0.0, 1.0),
	)
	return vec4(out*c.a, c.a)
}
`,
		m[0][0], m[0][1], m[0][2],
		m[1][0], m[1][1], m[1][2],
		m[2][0], m[2][1], m[2][2],
	)

	return Effect{
		Name:   "color-vision",
		Source: src,
		Reference: func(in Input, _ core.Uniforms, x, y float64) [4]float64 {
			c := in.At(x, y)
			if !(c[3] > 0) {
				return c
			}
			a := c[3]
			lr := cvdSRGBToLinear(saturate(c[0] / a))
			lg := cvdSRGBToLinear(saturate(c[1] / a))
			lb := cvdSRGBToLinear(saturate(c[2] / a))
			sr := saturate(m[0][0]*lr + m[0][1]*lg + m[0][2]*lb)
			sg := saturate(m[1][0]*lr + m[1][1]*lg + m[1][2]*lb)
			sb := saturate(m[2][0]*lr + m[2][1]*lg + m[2][2]*lb)
			or := saturate(cvdLinearToSRGB(sr))
			og := saturate(cvdLinearToSRGB(sg))
			ob := saturate(cvdLinearToSRGB(sb))
			return [4]float64{or * a, og * a, ob * a, a}
		},
	}
}

func cvdSRGBToLinear(u float64) float64 {
	if u <= 0.04045 {
		return u / 12.92
	}
	return math.Pow((u+0.055)/1.055, 2.4)
}

func cvdLinearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

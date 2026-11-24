package sprite

import (
	"math"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// MaxShaderPaletteEntries is the maximum number of PaletteSwap entries bound
// to the GPU sprite effect shader (EffectProgram). CPU rendering in Batch.Draw
// supports an arbitrary number of entries.
const MaxShaderPaletteEntries = 8

// spriteEffectSource is the Kage fragment shader compiled lazily by
// EffectProgram. It runs palette swap, tint, dissolve, solid-color flash or
// silhouette, and alpha outline on a single atlas frame without sampling
// neighboring frames or touching pixels outside the sprite's own view box.
const spriteEffectSource = `package main

// FrameRect is the frame's source rectangle (x, y, w, h) in atlas pixels.
var FrameRect vec4

// Pad is the transparent border in atlas pixels added on each side for Outline.
var Pad float

// FlashColor is the solid flash or silhouette RGB color, not premultiplied.
var FlashColor vec3

// FlashStrength blends sprite RGB toward FlashColor in [0, 1].
var FlashStrength float

// OutlineColor is the straight RGBA outline color.
var OutlineColor vec4

// OutlineThickness is the outline width in atlas pixels.
var OutlineThickness float

// OutlineThreshold is the minimum pixel alpha in (0, 1] treated as inside the silhouette.
var OutlineThreshold float

// DissolveThreshold erodes pixels whose noise value is below it, in [0, 1].
var DissolveThreshold float

// DissolveEdgeWidth is the glowing band width above DissolveThreshold.
var DissolveEdgeWidth float

// DissolveEdgeColor is the straight RGBA color of the dissolve edge.
var DissolveEdgeColor vec4

// DissolveSeed offsets the built-in hash noise when HasNoiseMap is 0.
var DissolveSeed float

// DissolveScale is the built-in noise cell size in atlas pixels (>= 1).
var DissolveScale float

// HasNoiseMap is > 0.5 when image1 holds a custom dissolve noise map.
var HasNoiseMap float

// SwapCount is the number of active entries in SwapFrom and SwapTo (0..8).
var SwapCount int

// SwapTolerance is the maximum per-channel sRGB difference in [0, 1] for a match.
var SwapTolerance float

// SwapFrom holds straight sRGB source colors in .rgb.
var SwapFrom [8]vec4

// SwapTo holds straight RGBA replacement colors.
var SwapTo [8]vec4

// Tint multiplies the sprite's straight RGBA channels (1,1,1,1 is identity).
var Tint vec4

// Alpha multiplies the final output opacity in [0, 1].
var Alpha float

func sampleFrame(p vec2) vec4 {
	if p.x < 0 || p.y < 0 || p.x >= FrameRect.z || p.y >= FrameRect.w {
		return vec4(0)
	}
	return image0At(FrameRect.xy + p)
}

func hash2(cell vec2) float {
	v := dot(cell, vec2(127.1, 311.7)) + DissolveSeed*17.13
	return fract(sin(v) * 43758.5453)
}

func noiseAt(p vec2) float {
	if HasNoiseMap > 0.5 {
		uv := clamp(p/max(FrameRect.zw, vec2(1)), vec2(0), vec2(0.999999))
		nc := image1At(uv * image1Size())
		return dot(nc.rgb, vec3(0.2126, 0.7152, 0.0722))
	}
	sc := max(DissolveScale, 1.0)
	if sc <= 1.0 {
		return hash2(floor(p))
	}
	g := p / sc
	i := floor(g)
	f := fract(g)
	u := f * f * (vec2(3.0) - 2.0*f)
	n00 := hash2(i)
	n10 := hash2(i + vec2(1, 0))
	n01 := hash2(i + vec2(0, 1))
	n11 := hash2(i + vec2(1, 1))
	return mix(mix(n00, n10, u.x), mix(n01, n11, u.x), u.y)
}

func evalSprite(p vec2) vec4 {
	c := sampleFrame(p)
	if c.a <= 0 {
		return vec4(0)
	}
	if SwapCount > 0 {
		rgb := c.rgb / c.a
		for i := 0; i < 8; i++ {
			if i >= SwapCount {
				break
			}
			diff := abs(rgb - SwapFrom[i].rgb)
			if max(diff.x, max(diff.y, diff.z)) <= SwapTolerance {
				na := c.a * SwapTo[i].a
				c = vec4(SwapTo[i].rgb*na, na)
				break
			}
		}
		if c.a <= 0 {
			return vec4(0)
		}
	}
	c = vec4(c.rgb*Tint.rgb*Tint.a, c.a*Tint.a)
	if c.a <= 0 {
		return vec4(0)
	}
	if DissolveThreshold > 0 {
		if DissolveThreshold >= 1 {
			return vec4(0)
		}
		n := noiseAt(p)
		if n < DissolveThreshold {
			return vec4(0)
		}
		if DissolveEdgeWidth > 0 && n < DissolveThreshold+DissolveEdgeWidth && DissolveEdgeColor.a > 0 {
			c = vec4(mix(c.rgb, DissolveEdgeColor.rgb*c.a, DissolveEdgeColor.a), c.a)
		}
	}
	if FlashStrength > 0 {
		k := clamp(FlashStrength, 0, 1)
		c = vec4(mix(c.rgb, FlashColor*c.a, k), c.a)
	}
	return c
}

func Fragment(dst vec4, pos vec2) vec4 {
	size := max(rectSize(), vec2(1))
	uv := pos / size
	total := FrameRect.zw + vec2(Pad*2)
	p := uv*total - vec2(Pad)

	c := evalSprite(p)
	if OutlineThickness > 0 && OutlineColor.a > 0 && c.a < 1 {
		r := max(OutlineThickness, 1.0)
		thresh := OutlineThreshold
		if thresh <= 0 {
			thresh = 1.0 / 255.0
		}
		hit := false
		for i := 0; i < 16; i++ {
			ang := float(i) * 0.39269908169872414
			dir := vec2(cos(ang), sin(ang))
			if evalSprite(p+dir*r).a >= thresh {
				hit = true
				break
			}
			if r > 1.5 && evalSprite(p+dir*(r*0.5)).a >= thresh {
				hit = true
				break
			}
		}
		if hit {
			oa := OutlineColor.a * min(OutlineThickness, 1.0)
			outC := vec4(OutlineColor.rgb*oa, oa)
			c = c + outC*(1.0-c.a)
		}
	}
	return c * clamp(Alpha, 0, 1)
}
`

var (
	effectProgMu   sync.Mutex
	effectProg     *core.ShaderProgram
	effectProgErr  error
	effectProgDone bool
)

// EffectProgram returns the compiled Kage GPU shader program for per-sprite
// flash, silhouette, alpha outline, dissolve and palette swap. It compiles on
// first call so apps that do not use GPU sprite shaders do not link the shader
// compiler.
func EffectProgram() (*core.ShaderProgram, error) {
	effectProgMu.Lock()
	defer effectProgMu.Unlock()
	if !effectProgDone {
		effectProg, effectProgErr = core.CompileShader(spriteEffectSource)
		effectProgDone = true
	}
	return effectProg, effectProgErr
}

// Uniforms builds the core.Uniforms map for EffectProgram from e, frame,
// atlasPixelScale (pixels per point; values <= 0 default to 1), tint and
// alpha. Callers may replace individual keys such as "FlashStrength" or
// "DissolveThreshold" with a core.AnimatedNode to animate on the platform's
// frame clock with no commit per frame.
func (e Effect) Uniforms(frame Frame, atlasPixelScale float32, tint core.Color, alpha float32) core.Uniforms {
	scale := float64(atlasPixelScale)
	if !(scale > 0) || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	pad := float64(e.Outline.padding())

	fr, fg, fb, _ := e.Flash.Color.Components()
	or, og, ob, oa := e.Outline.Color.Components()
	er, eg, eb, ea := e.Dissolve.EdgeColor.Components()

	var tr, tg, tb, ta float64 = 1, 1, 1, 1
	if tint != 0 && tint != untinted {
		tr, tg, tb, ta = tint.Components()
	}

	effAlpha := float64(alpha)
	if !(effAlpha > 0) || math.IsNaN(effAlpha) {
		effAlpha = 0
	} else if effAlpha > 1 || math.IsInf(effAlpha, 1) {
		effAlpha = 1
	}

	swapCount := min(len(e.PaletteSwap.Entries), MaxShaderPaletteEntries)
	swapFrom := make([]float64, MaxShaderPaletteEntries*4)
	swapTo := make([]float64, MaxShaderPaletteEntries*4)
	for i := range swapCount {
		entry := e.PaletteSwap.Entries[i]
		sr, sg, sb, sa := entry.From.Components()
		swapFrom[i*4+0], swapFrom[i*4+1], swapFrom[i*4+2], swapFrom[i*4+3] = sr, sg, sb, sa
		dr, dg, db, da := entry.To.Components()
		swapTo[i*4+0], swapTo[i*4+1], swapTo[i*4+2], swapTo[i*4+3] = dr, dg, db, da
	}

	hasNoise := 0.0
	if e.Dissolve.Noise != nil && !e.Dissolve.Noise.Bounds().Empty() {
		hasNoise = 1.0
	}

	return core.Uniforms{
		"FrameRect":         []float64{float64(frame.X) * scale, float64(frame.Y) * scale, float64(frame.W) * scale, float64(frame.H) * scale},
		"Pad":               pad,
		"FlashColor":        []float64{fr, fg, fb},
		"FlashStrength":     e.Flash.strength(),
		"OutlineColor":      []float64{or, og, ob, oa},
		"OutlineThickness":  e.Outline.thickness(),
		"OutlineThreshold":  float64(e.Outline.thresholdByte()) / 255.0,
		"DissolveThreshold": e.Dissolve.threshold(),
		"DissolveEdgeWidth": e.Dissolve.edgeWidth(),
		"DissolveEdgeColor": []float64{er, eg, eb, ea},
		"DissolveSeed":      float64(e.Dissolve.Seed),
		"DissolveScale":     e.Dissolve.scale(),
		"HasNoiseMap":       hasNoise,
		"SwapCount":         swapCount,
		"SwapTolerance":     float64(e.PaletteSwap.Tolerance) / 255.0,
		"SwapFrom":          swapFrom,
		"SwapTo":            swapTo,
		"Tint":              []float64{tr, tg, tb, ta},
		"Alpha":             effAlpha,
	}
}

// EffectShader returns a core.Shader node that renders s through EffectProgram
// on the GPU, reading atlas.Image as image0 (and Dissolve.Noise as image1 when
// set). It works with URI atlases as well as pixel buffers and embedded bytes
// on GPU-backed targets (Metal, Direct3D 11, OpenGL, GLES 3.0 and WebGL 2).
//
// The returned Shader's Style sets Width and Height to the sprite's scaled
// frame size plus any outline padding; callers position it in their layout or
// override Uniforms entries with AnimatedNode values.
func EffectShader(atlas *Atlas, s Sprite) (core.Shader, error) {
	prog, err := EffectProgram()
	if err != nil {
		return core.Shader{}, err
	}
	var fx Effect
	if s.Effect != nil {
		fx = *s.Effect
	}
	var scale float32 = 1
	var images []core.ImageSource
	if atlas != nil {
		if atlas.Image.Pixels != nil {
			px, _ := atlasPixelScale(atlas.Image, atlas.Image.Pixels)
			scale = float32(px)
		}
		images = append(images, atlas.Image)
	}
	if fx.Dissolve.Noise != nil && !fx.Dissolve.Noise.Bounds().Empty() {
		if rgba := imageToPremulRGBA(fx.Dissolve.Noise); rgba != nil {
			images = append(images, core.ImageSource{
				Pixels:  rgba,
				Version: 1,
				Size:    core.Size{Width: core.Points(rgba.Rect.Dx()), Height: core.Points(rgba.Rect.Dy())},
			})
		}
	}

	padPt := float32(fx.Outline.padding()) / scale
	w := (s.Frame.W + 2*padPt) * float32(math.Abs(float64(s.ScaleX)))
	h := (s.Frame.H + 2*padPt) * float32(math.Abs(float64(s.ScaleY)))

	return core.Shader{
		Program:  prog,
		Uniforms: fx.Uniforms(s.Frame, scale, s.Tint, s.Alpha),
		Images:   images,
		Style: core.Style{
			Width:  core.Pt(core.Points(w)),
			Height: core.Pt(core.Points(h)),
		},
	}, nil
}

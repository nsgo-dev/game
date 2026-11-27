package postfx

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"maps"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Input is what an effect's Reference reads: the pass before it, or the frame for the first, as Input and InputSize read them in its source, plus the effect's extra texture as Texture and TextureSize read it.
type Input struct {
	at     func(x, y float64) [4]float64
	w, h   float64
	texAt  func(x, y float64) [4]float64
	tw, th float64
}

// At returns the premultiplied color of the input at (x, y), in the frame's pixels: for the frame, the pixel under the point, transparent outside it.
func (in Input) At(x, y float64) [4]float64 { return in.at(x, y) }

// Size returns the frame's size in pixels.
func (in Input) Size() (w, h float64) { return in.w, in.h }

// TextureAt returns the premultiplied color of the effect's extra texture at (x, y), in its pixels: the pixel under the point, transparent outside it, as Texture(pos) reads it in its source.
func (in Input) TextureAt(x, y float64) [4]float64 {
	if in.texAt == nil {
		return [4]float64{}
	}
	return in.texAt(x, y)
}

// TextureSize returns the size of the effect's extra texture in pixels, as TextureSize() reads it in its source.
func (in Input) TextureSize() (w, h float64) { return in.tw, in.th }

// Reference draws frame through the chain on the CPU into a picture of width by height pixels, with each effect's Reference, as View draws it on the GPU into a box of that many pixels with the same uniforms: each pixel's center is mapped onto the frame and drawn by the last effect. perEffect[i] is effect i's uniforms, as Chain.Uniforms and View.Uniforms take them. It is the picture a test compares a chain's output with, and a screenshot of it where no GPU draws. Colors are clamped to 0 to 1 and rounded to 8 bits.
//
// It returns an error when an effect has no Reference, when perEffect names a uniform an effect does not declare or has more entries than the chain has effects, when a textured effect's Texture has neither Pixels nor Data, and for a uniform set to an AnimatedNode other than a core.Constant, whose value only the platform knows.
func (c *Chain) Reference(frame *image.RGBA, width, height int, perEffect []core.Uniforms) (*image.RGBA, error) {
	return c.ReferenceWithTextures(frame, width, height, perEffect, nil)
}

// ReferenceWithTextures is Reference with per-effect texture overrides, as View.Textures and Chain.Images take them: a non-zero entry in textures[i] replaces effect i's own Texture.
func (c *Chain) ReferenceWithTextures(frame *image.RGBA, width, height int, perEffect []core.Uniforms, textures []core.ImageSource) (*image.RGBA, error) {
	for i, e := range c.effects {
		if e.Reference == nil {
			return nil, fmt.Errorf("postfx: %s has no Reference", e.label(i))
		}
	}
	if len(perEffect) > len(c.effects) {
		return nil, fmt.Errorf("postfx: uniforms for %d effects, but the chain has %d", len(perEffect), len(c.effects))
	}
	if len(textures) > len(c.effects) {
		return nil, fmt.Errorf("postfx: textures for %d effects, but the chain has %d", len(textures), len(c.effects))
	}
	for i, u := range perEffect {
		for _, name := range slices.Sorted(maps.Keys(u)) {
			if !c.uniforms[i][name] {
				return nil, fmt.Errorf("postfx: %s has no uniform %s", c.effects[i].label(i), name)
			}
			if animated(u[name]) {
				return nil, fmt.Errorf("postfx: %s: uniform %s follows an animated node, whose value only the platform knows", c.effects[i].label(i), name)
			}
		}
	}
	resolvedTex := make([]*image.RGBA, len(c.effects))
	for i, e := range c.effects {
		var override core.ImageSource
		if i < len(textures) {
			override = textures[i]
		}
		if !isZeroImageSource(override) && c.texSlots[i] == 0 {
			return nil, fmt.Errorf("postfx: %s reads no texture", e.label(i))
		}
		if c.texSlots[i] == 0 {
			continue
		}
		src := e.Texture
		if !isZeroImageSource(override) {
			src = override
		}
		tex, err := resolveTexture(src)
		if err != nil {
			return nil, fmt.Errorf("postfx: %s: %w", e.label(i), err)
		}
		resolvedTex[i] = tex
	}
	out := image.NewRGBA(image.Rect(0, 0, max(width, 0), max(height, 0)))
	if width <= 0 || height <= 0 {
		return out, nil
	}
	var fw, fh float64
	if frame != nil {
		fw, fh = float64(frame.Rect.Dx()), float64(frame.Rect.Dy())
	}
	in := Input{at: framePixel(frame), w: fw, h: fh}
	for i, e := range c.effects {
		var u core.Uniforms
		if i < len(perEffect) {
			u = perEffect[i]
		}
		prev, ref := in, e.Reference
		if tex := resolvedTex[i]; tex != nil {
			prev.texAt = framePixel(tex)
			prev.tw = float64(tex.Rect.Dx())
			prev.th = float64(tex.Rect.Dy())
		} else {
			prev.texAt, prev.tw, prev.th = nil, 0, 0
		}
		in = Input{at: func(x, y float64) [4]float64 { return ref(prev, u, x, y) }, w: fw, h: fh}
	}
	sx, sy := fw/float64(width), fh/float64(height)
	for py := range height {
		row := out.Pix[py*out.Stride:]
		for px := range width {
			c := in.at((float64(px)+0.5)*sx, (float64(py)+0.5)*sy)
			for k, v := range c {
				row[px*4+k] = to8(v)
			}
		}
	}
	return out, nil
}

// resolveTexture returns the premultiplied RGBA pixels of src for Reference, decoding embedded image bytes when Pixels is nil.
func resolveTexture(src core.ImageSource) (*image.RGBA, error) {
	if src.Pixels != nil {
		return src.Pixels, nil
	}
	if len(src.Data) > 0 {
		img, _, err := image.Decode(bytes.NewReader(src.Data))
		if err != nil {
			return nil, fmt.Errorf("decode texture: %w", err)
		}
		if rgba, ok := img.(*image.RGBA); ok {
			return rgba, nil
		}
		b := img.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Rect, img, b.Min, draw.Src)
		return rgba, nil
	}
	if isZeroImageSource(src) {
		return nil, nil
	}
	return nil, fmt.Errorf("texture %q has no Pixels or Data for Reference", src.URI)
}

// framePixel reads frame's pixel under a point, premultiplied from 0 to 1, transparent outside it, as image0At reads the frame's texture.
func framePixel(frame *image.RGBA) func(x, y float64) [4]float64 {
	return func(x, y float64) [4]float64 {
		if frame == nil || math.IsNaN(x) || math.IsNaN(y) {
			return [4]float64{}
		}
		fx, fy := math.Floor(x), math.Floor(y)
		if fx < 0 || fy < 0 || fx >= float64(frame.Rect.Dx()) || fy >= float64(frame.Rect.Dy()) {
			return [4]float64{}
		}
		i := frame.PixOffset(frame.Rect.Min.X+int(fx), frame.Rect.Min.Y+int(fy))
		p := frame.Pix[i : i+4 : i+4]
		return [4]float64{float64(p[0]) / 255, float64(p[1]) / 255, float64(p[2]) / 255, float64(p[3]) / 255}
	}
}

// to8 clamps v to 0 to 1 and rounds it to 8 bits.
func to8(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(v * 255))
}

// animated reports whether a uniform's value holds an AnimatedNode other than a core.Constant.
func animated(v any) bool {
	switch x := v.(type) {
	case core.Constant:
		return false
	case core.AnimatedNode:
		return true
	case []core.AnimatedNode:
		return slices.ContainsFunc(x, func(n core.AnimatedNode) bool { return animated(n) })
	case []any:
		return slices.ContainsFunc(x, animated)
	}
	return false
}

// Float returns the number uniform u sets name to, for an effect's Reference: any of Go's integer or float kinds, a bool as 0 or 1, or a core.Constant. It is 0 when u leaves name out or sets it to anything else, such as a vector, which a Reference reads from u itself.
func Float(u core.Uniforms, name string) float64 {
	switch x := u[name].(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	case core.Constant:
		return float64(x)
	}
	return 0
}

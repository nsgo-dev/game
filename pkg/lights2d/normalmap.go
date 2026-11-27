package lights2d

import (
	"image"
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const defaultLightZ = 32

// ShadeNormalMap computes tangent-space normal-mapped lighting for diffuse
// using normalMap, placing the sprite's top-left corner at (spriteX, spriteY).
//
// Normal vectors are decoded from normalMap RGB channels as
// (nx, ny, nz) = (2*R - 1, 2*G - 1, 2*B - 1) and normalized. Each pixel's
// diffuse RGB is modulated by ambient + Σ(lightColor * intensity * atten * max(0, N·L)),
// where L points from the pixel toward (light.X, light.Y, lightZ). Alpha is
// preserved from diffuse.
//
// If diffuse is nil, ShadeNormalMap returns nil. If normalMap is nil, a flat
// forward-facing normal (0, 0, 1) is used.
func ShadeNormalMap(diffuse, normalMap *image.NRGBA, spriteX, spriteY float32, ambient core.Color, lights []*Light, lightZ float32) *image.NRGBA {
	return ShadeNormalMapWithOccluders(diffuse, normalMap, spriteX, spriteY, ambient, lights, lightZ, nil)
}

// ShadeNormalMapWithOccluders behaves like ShadeNormalMap and also tests
// line-of-sight shadow occlusion against occluders for lights with CastShadows
// enabled.
func ShadeNormalMapWithOccluders(diffuse, normalMap *image.NRGBA, spriteX, spriteY float32, ambient core.Color, lights []*Light, lightZ float32, occluders []Occluder) *image.NRGBA {
	if diffuse == nil {
		return nil
	}
	out := image.NewNRGBA(diffuse.Bounds())
	shadeNormalMapInto(out, diffuse, normalMap, spriteX, spriteY, ambient, lights, lightZ, occluders)
	return out
}

// ShadeNormalMap shades diffuse using normalMap with the scene's Ambient, Lights, and Occluders, leaving the occluders out when DisableShadows is set.
func (s *Scene) ShadeNormalMap(diffuse, normalMap *image.NRGBA, spriteX, spriteY, lightZ float32) *image.NRGBA {
	if s == nil {
		return ShadeNormalMap(diffuse, normalMap, spriteX, spriteY, 0, nil, lightZ)
	}
	return ShadeNormalMapWithOccluders(diffuse, normalMap, spriteX, spriteY, s.Ambient, s.Lights, lightZ, s.shadowOccluders())
}

type activeNormalLight struct {
	light   *Light
	r, g, b float64
	occSegs []seg64
}

func shadeNormalMapInto(dst, diffuse, normalMap *image.NRGBA, spriteX, spriteY float32, ambient core.Color, lights []*Light, lightZ float32, occluders []Occluder) {
	rect := diffuse.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}

	ar, ag, ab, aa := ambient.Components()
	ambR, ambG, ambB := ar*aa, ag*aa, ab*aa

	var allOccSegs []seg64
	occBuilt := false

	sx0, sy0 := float64(spriteX), float64(spriteY)
	sx1, sy1 := sx0+float64(w), sy0+float64(h)

	active := make([]activeNormalLight, 0, len(lights))
	for _, l := range lights {
		if l == nil || !l.Enabled || l.Radius <= 0 || l.Intensity <= 0 {
			continue
		}
		lr, lg, lb, la := l.Color.Components()
		if la <= 0 {
			continue
		}
		lx, ly, r := float64(l.X), float64(l.Y), float64(l.Radius)
		if lx+r < sx0 || lx-r > sx1 || ly+r < sy0 || ly-r > sy1 {
			continue
		}
		al := activeNormalLight{
			light: l,
			r:     lr * la * float64(l.Intensity),
			g:     lg * la * float64(l.Intensity),
			b:     lb * la * float64(l.Intensity),
		}
		if l.CastShadows && len(occluders) > 0 {
			if !occBuilt {
				allOccSegs = appendOccluderSegments(nil, occluders)
				occBuilt = true
			}
			// Filter occluder segments to the bounding box enclosing light and sprite.
			bMinX := math.Min(lx, sx0)
			bMaxX := math.Max(lx, sx1)
			bMinY := math.Min(ly, sy0)
			bMaxY := math.Max(ly, sy1)
			for _, seg := range allOccSegs {
				if seg.maxX < bMinX || seg.minX > bMaxX || seg.maxY < bMinY || seg.minY > bMaxY {
					continue
				}
				al.occSegs = append(al.occSegs, seg)
			}
		}
		active = append(active, al)
	}

	var nmRect image.Rectangle
	if normalMap != nil {
		nmRect = normalMap.Bounds()
	}
	lz := float64(lightZ)

	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		py := spriteY + float32(y-rect.Min.Y) + 0.5
		py64 := float64(py)
		dRow := (y - rect.Min.Y) * diffuse.Stride
		oRow := (y - dst.Rect.Min.Y) * dst.Stride

		for x := rect.Min.X; x < rect.Max.X; x++ {
			di := dRow + (x-rect.Min.X)*4
			oi := oRow + (x-dst.Rect.Min.X)*4

			da := diffuse.Pix[di+3]
			if da == 0 {
				dst.Pix[oi+0] = 0
				dst.Pix[oi+1] = 0
				dst.Pix[oi+2] = 0
				dst.Pix[oi+3] = 0
				continue
			}

			// Decode tangent-space normal (2*C - 1), defaulting to (0, 0, 1).
			nx, ny, nz := 0.0, 0.0, 1.0
			if normalMap != nil && x >= nmRect.Min.X && x < nmRect.Max.X && y >= nmRect.Min.Y && y < nmRect.Max.Y {
				ni := normalMap.PixOffset(x, y)
				nx = float64(normalMap.Pix[ni+0])*(2.0/255.0) - 1.0
				ny = float64(normalMap.Pix[ni+1])*(2.0/255.0) - 1.0
				nz = float64(normalMap.Pix[ni+2])*(2.0/255.0) - 1.0
				nLen := math.Sqrt(nx*nx + ny*ny + nz*nz)
				if nLen > 1e-6 {
					inv := 1.0 / nLen
					nx *= inv
					ny *= inv
					nz *= inv
				} else {
					nx, ny, nz = 0, 0, 1
				}
			}

			px := spriteX + float32(x-rect.Min.X) + 0.5
			px64 := float64(px)

			tr, tg, tb := ambR, ambG, ambB
			for _, al := range active {
				atten := float64(al.light.AttenuationAt(px, py))
				if atten <= 0 {
					continue
				}
				if len(al.occSegs) > 0 && !visibleSegments(float64(al.light.X), float64(al.light.Y), px64, py64, al.occSegs) {
					continue
				}
				lx := float64(al.light.X) - px64
				ly := float64(al.light.Y) - py64
				lLen := math.Sqrt(lx*lx + ly*ly + lz*lz)
				var ndotl float64
				if lLen > 1e-6 {
					ndotl = (nx*lx + ny*ly + nz*lz) / lLen
				} else {
					ndotl = nz
				}
				if ndotl <= 0 {
					continue
				}
				scale := atten * ndotl
				tr += al.r * scale
				tg += al.g * scale
				tb += al.b * scale
			}

			dst.Pix[oi+0] = clampByte(float64(diffuse.Pix[di+0]) * tr)
			dst.Pix[oi+1] = clampByte(float64(diffuse.Pix[di+1]) * tg)
			dst.Pix[oi+2] = clampByte(float64(diffuse.Pix[di+2]) * tb)
			dst.Pix[oi+3] = da
		}
	}
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

func nrgbaToPremultipliedRGBA(dst *image.RGBA, src *image.NRGBA) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	for y := range h {
		sRow := y * src.Stride
		dRow := y * dst.Stride
		for x := range w {
			si := sRow + x*4
			di := dRow + x*4
			a := src.Pix[si+3]
			switch a {
			case 0:
				dst.Pix[di+0] = 0
				dst.Pix[di+1] = 0
				dst.Pix[di+2] = 0
				dst.Pix[di+3] = 0
			case 255:
				dst.Pix[di+0] = src.Pix[si+0]
				dst.Pix[di+1] = src.Pix[si+1]
				dst.Pix[di+2] = src.Pix[si+2]
				dst.Pix[di+3] = 255
			default:
				a16 := uint16(a)
				dst.Pix[di+0] = uint8((uint16(src.Pix[si+0])*a16 + 127) / 255)
				dst.Pix[di+1] = uint8((uint16(src.Pix[si+1])*a16 + 127) / 255)
				dst.Pix[di+2] = uint8((uint16(src.Pix[si+2])*a16 + 127) / 255)
				dst.Pix[di+3] = a
			}
		}
	}
}

type normalSpriteKey struct {
	diffuse       *image.NRGBA
	normalMap     *image.NRGBA
	x, y, lightZ  float32
	ambient       core.Color
	lightCount    int
	occluderCount int
	hash          uint64
}

// NormalSprite caches normal-mapped shading of a diffuse/normal-map sprite pair
// and exposes a premultiplied core.ImageSource ready for Drawing.DrawImage.
//
// It only recomputes shading and increments ImageSource.Version when the
// sprite's position, LightZ, source images, or the scene's lighting/occluders
// change, or after Invalidate is called.
type NormalSprite struct {
	// Diffuse is the unlit straight-alpha sprite image.
	Diffuse *image.NRGBA
	// NormalMap is the tangent-space normal map matching Diffuse.
	NormalMap *image.NRGBA
	// X and Y are the sprite's top-left position in world coordinates.
	X, Y float32
	// LightZ is the elevation of 2D lights above the sprite plane.
	// Values <= 0 are passed through directly; NewNormalSprite initializes it to 32.
	LightZ float32

	shaded  *image.NRGBA
	pixels  *image.RGBA
	version uint64
	lastKey normalSpriteKey
	valid   bool
}

// NewNormalSprite returns a NormalSprite with a default LightZ of 32.
func NewNormalSprite(diffuse, normalMap *image.NRGBA) *NormalSprite {
	return &NormalSprite{
		Diffuse:   diffuse,
		NormalMap: normalMap,
		LightZ:    defaultLightZ,
	}
}

// Invalidate marks the cached shaded image stale so the next ImageSource or
// Draw call recomputes it. Call this after mutating Diffuse or NormalMap pixels
// in place.
func (ns *NormalSprite) Invalidate() {
	if ns != nil {
		ns.valid = false
	}
}

// Shaded returns the most recently computed straight-alpha shaded image, or nil
// if ImageSource has not been called yet.
func (ns *NormalSprite) Shaded() *image.NRGBA {
	if ns == nil {
		return nil
	}
	return ns.shaded
}

// ImageSource returns a core.ImageSource backed by premultiplied Pixels shaded
// by scene. Repeated calls with unchanged inputs return the cached ImageSource
// with the same Version and perform no re-shading.
func (ns *NormalSprite) ImageSource(scene *Scene) core.ImageSource {
	if ns == nil || ns.Diffuse == nil {
		return core.ImageSource{}
	}
	rect := ns.Diffuse.Bounds()
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return core.ImageSource{}
	}

	key := ns.cacheKey(scene)
	if !ns.valid || ns.lastKey != key {
		if ns.shaded == nil || ns.shaded.Rect != rect {
			ns.shaded = image.NewNRGBA(rect)
		}
		rgbaRect := image.Rect(0, 0, w, h)
		if ns.pixels == nil || ns.pixels.Rect != rgbaRect {
			ns.pixels = image.NewRGBA(rgbaRect)
		}
		var ambient core.Color
		var lights []*Light
		var occluders []Occluder
		if scene != nil {
			ambient = scene.Ambient
			lights = scene.Lights
			occluders = scene.shadowOccluders()
		}
		shadeNormalMapInto(ns.shaded, ns.Diffuse, ns.NormalMap, ns.X, ns.Y, ambient, lights, ns.LightZ, occluders)
		nrgbaToPremultipliedRGBA(ns.pixels, ns.shaded)
		ns.version++
		ns.lastKey = key
		ns.valid = true
	}

	return core.ImageSource{
		Pixels:  ns.pixels,
		Version: ns.version,
		Size:    core.Size{Width: core.Points(w), Height: core.Points(h)},
	}
}

// Draw shades (if dirty) and draws the sprite into d at (ns.X, ns.Y).
func (ns *NormalSprite) Draw(d *core.Drawing, scene *Scene) {
	if ns == nil || d == nil {
		return
	}
	src := ns.ImageSource(scene)
	if src.Empty() {
		return
	}
	d.DrawImage(src, ns.X, ns.Y)
}

func (ns *NormalSprite) cacheKey(scene *Scene) normalSpriteKey {
	const (
		fnvOffset = 14695981039346656037
		fnvPrime  = 1099511628211
	)
	h := uint64(fnvOffset)
	mix32 := func(v uint32) {
		h ^= uint64(v)
		h *= fnvPrime
	}
	mixF32 := func(v float32) {
		// Normalize -0 to +0 so keys match identically.
		if v == 0 {
			v = 0
		}
		mix32(math.Float32bits(v))
	}
	mixBool := func(b bool) {
		if b {
			mix32(1)
		} else {
			mix32(0)
		}
	}

	r := ns.Diffuse.Bounds()
	mix32(uint32(r.Min.X))
	mix32(uint32(r.Min.Y))
	mix32(uint32(r.Max.X))
	mix32(uint32(r.Max.Y))
	if ns.NormalMap != nil {
		nr := ns.NormalMap.Bounds()
		mix32(uint32(nr.Min.X))
		mix32(uint32(nr.Min.Y))
		mix32(uint32(nr.Max.X))
		mix32(uint32(nr.Max.Y))
	}

	key := normalSpriteKey{
		diffuse:   ns.Diffuse,
		normalMap: ns.NormalMap,
		x:         ns.X,
		y:         ns.Y,
		lightZ:    ns.LightZ,
	}
	if scene != nil {
		key.ambient = scene.Ambient
		key.lightCount = len(scene.Lights)
		key.occluderCount = len(scene.shadowOccluders())
		mix32(uint32(scene.Ambient))
		for _, l := range scene.Lights {
			if l == nil {
				mix32(0)
				continue
			}
			mix32(1)
			mixF32(l.X)
			mixF32(l.Y)
			mixF32(l.Radius)
			mix32(uint32(l.Color))
			mixF32(l.Intensity)
			mixF32(l.Falloff)
			mixF32(l.ConeAngle)
			mixF32(l.ConeSpread)
			mixBool(l.CastShadows)
			mixBool(l.Enabled)
		}
		for _, occ := range scene.shadowOccluders() {
			mix32(uint32(len(occ.Vertices)))
			for _, pt := range occ.Vertices {
				mixF32(float32(pt.X))
				mixF32(float32(pt.Y))
			}
			mix32(uint32(len(occ.Segments)))
			for _, seg := range occ.Segments {
				mixF32(float32(seg.A.X))
				mixF32(float32(seg.A.Y))
				mixF32(float32(seg.B.X))
				mixF32(float32(seg.B.Y))
			}
		}
	}
	key.hash = h
	return key
}

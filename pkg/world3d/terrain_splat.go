package world3d

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// MaxTerrainLayers is the maximum number of PBR terrain material layers driven
// by splat maps (up to 2 RGBA weight maps × 4 channels per map).
const MaxTerrainLayers = 8

// ErrInvalidTerrainSplat is wrapped by errors returned when constructing or
// validating a [TerrainSplatMap].
var ErrInvalidTerrainSplat = errors.New("world3d: invalid terrain splat map")

// TerrainHeightLayer holds a CPU-readable normalized [0, 1] height texture for
// one terrain material layer so [TerrainSplatMap.SampleUV] and [Heightmap.LayerAt]
// can apply height-based blending matching the GPU terrain shader.
type TerrainHeightLayer struct {
	Width   int
	Height  int
	Heights []float32
}

// LoadTerrainHeightLayer converts a grayscale (or color, converted to 16-bit
// luminance) image into a normalized [0, 1] [TerrainHeightLayer]. Returns nil
// if img is nil or has empty bounds.
func LoadTerrainHeightLayer(img image.Image) *TerrainHeightLayer {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	heights := make([]float32, w*h)
	for y := range h {
		for x := range w {
			g := color.Gray16Model.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray16)
			heights[y*w+x] = float32(g.Y) / 65535.0
		}
	}
	return &TerrainHeightLayer{
		Width:   w,
		Height:  h,
		Heights: heights,
	}
}

// SampleUV bilinearly samples the height layer at repeating texture coordinates
// (u, v), matching GPU linear-repeat texture sampling. Returns 0.5 when l is
// nil or empty.
func (l *TerrainHeightLayer) SampleUV(u, v float32) float32 {
	if l == nil || l.Width <= 0 || l.Height <= 0 || len(l.Heights) < l.Width*l.Height {
		return 0.5
	}
	u = wrap01(u)
	v = wrap01(v)
	fx := u*float32(l.Width) - 0.5
	fy := v*float32(l.Height) - 0.5
	x0 := int(math.Floor(float64(fx)))
	y0 := int(math.Floor(float64(fy)))
	tx := fx - float32(x0)
	ty := fy - float32(y0)
	ix0 := wrapIndex(x0, l.Width)
	ix1 := wrapIndex(x0+1, l.Width)
	iy0 := wrapIndex(y0, l.Height)
	iy1 := wrapIndex(y0+1, l.Height)

	h00 := l.Heights[iy0*l.Width+ix0]
	h10 := l.Heights[iy0*l.Width+ix1]
	h01 := l.Heights[iy1*l.Width+ix0]
	h11 := l.Heights[iy1*l.Width+ix1]
	top := h00*(1-tx) + h10*tx
	bot := h01*(1-tx) + h11*tx
	return top*(1-ty) + bot*ty
}

// TerrainLayerInfo describes one material layer's gameplay identity and
// height-blending parameters inside a [TerrainSplatMap].
type TerrainLayerInfo struct {
	// Name is the gameplay surface identifier (such as "grass", "dirt", "rock", "snow").
	Name string
	// UVScale scales terrain [0, 1] UVs when sampling HeightMap; zero components default to 1.
	UVScale math3d.Vec2
	// HeightBias biases this layer's height during height-based blending (typically in [-1, 1]).
	HeightBias float32
	// HeightMap optionally supplies a tiling height map for height-based blending; nil uses 0.5.
	HeightMap *TerrainHeightLayer
}

func (l TerrainLayerInfo) effectiveUVScale() math3d.Vec2 {
	sx := l.UVScale.X
	if sx <= 0 || math.IsNaN(float64(sx)) || math.IsInf(float64(sx), 0) {
		sx = 1
	}
	sy := l.UVScale.Y
	if sy <= 0 || math.IsNaN(float64(sy)) || math.IsInf(float64(sy), 0) {
		sy = 1
	}
	return math3d.Vec2{X: sx, Y: sy}
}

// TerrainLayerSample is the zero-allocation result of sampling terrain material
// layers at a world position or terrain UV for footsteps, particles, and
// movement logic.
type TerrainLayerSample struct {
	// Dominant is the index in [0, Count) of the layer with the highest normalized weight.
	// Ties break deterministically in favor of the lower layer index.
	Dominant int
	// Name is the configured name of the Dominant layer.
	Name string
	// Weights holds the normalized per-layer weights in [0, 1] summing to 1 across [0, Count).
	// Entries at indices >= Count are zero.
	Weights [MaxTerrainLayers]float32
	// Count is the number of active layers in [0, MaxTerrainLayers].
	Count int
}

// Weight returns the normalized weight of layer index, or 0 if index is out of range.
func (s TerrainLayerSample) Weight(index int) float32 {
	if index < 0 || index >= s.Count || index >= MaxTerrainLayers {
		return 0
	}
	return s.Weights[index]
}

type splatGrid struct {
	width  int
	height int
	// rgba holds tightly packed straight-alpha RGBA8 bytes (4 bytes per texel).
	rgba []byte
}

func splatGridFromImage(img image.Image) (splatGrid, error) {
	if img == nil {
		return splatGrid{}, fmt.Errorf("%w: nil splat map image", ErrInvalidTerrainSplat)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return splatGrid{}, fmt.Errorf("%w: empty splat map image bounds %v", ErrInvalidTerrainSplat, b)
	}
	rgba := make([]byte, w*h*4)
	if nrgba, ok := img.(*image.NRGBA); ok {
		rowBytes := w * 4
		for y := range h {
			off := nrgba.PixOffset(b.Min.X, b.Min.Y+y)
			copy(rgba[y*rowBytes:(y+1)*rowBytes], nrgba.Pix[off:off+rowBytes])
		}
	} else {
		for y := range h {
			for x := range w {
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				i := (y*w + x) * 4
				rgba[i] = c.R
				rgba[i+1] = c.G
				rgba[i+2] = c.B
				rgba[i+3] = c.A
			}
		}
	}
	return splatGrid{width: w, height: h, rgba: rgba}, nil
}

func splatGridFromBytes(width, height int, rgba []byte) (splatGrid, error) {
	if width <= 0 || height <= 0 {
		return splatGrid{}, fmt.Errorf("%w: invalid splat map dimensions %dx%d", ErrInvalidTerrainSplat, width, height)
	}
	need := width * height * 4
	if len(rgba) < need {
		return splatGrid{}, fmt.Errorf("%w: splat map buffer has %d bytes, need %d for %dx%d RGBA", ErrInvalidTerrainSplat, len(rgba), need, width, height)
	}
	cp := make([]byte, need)
	copy(cp, rgba[:need])
	return splatGrid{width: width, height: height, rgba: cp}, nil
}

// sampleBilinear samples g at clamped UV coordinates (u, v) using texel-center
// bilinear interpolation (matching GPU linear clamp-to-edge texture sampling).
func (g *splatGrid) sampleBilinear(u, v float32) [4]float32 {
	if g == nil || g.width <= 0 || g.height <= 0 || len(g.rgba) < g.width*g.height*4 {
		return [4]float32{}
	}
	u = math3d.Clamp(u, 0, 1)
	v = math3d.Clamp(v, 0, 1)

	fx := u*float32(g.width) - 0.5
	fy := v*float32(g.height) - 0.5
	x0 := int(math.Floor(float64(fx)))
	y0 := int(math.Floor(float64(fy)))
	tx := fx - float32(x0)
	ty := fy - float32(y0)

	ix0 := clampIndex(x0, g.width)
	ix1 := clampIndex(x0+1, g.width)
	iy0 := clampIndex(y0, g.height)
	iy1 := clampIndex(y0+1, g.height)

	i00 := (iy0*g.width + ix0) * 4
	i10 := (iy0*g.width + ix1) * 4
	i01 := (iy1*g.width + ix0) * 4
	i11 := (iy1*g.width + ix1) * 4

	const inv255 = float32(1.0 / 255.0)
	var out [4]float32
	for c := range 4 {
		v00 := float32(g.rgba[i00+c]) * inv255
		v10 := float32(g.rgba[i10+c]) * inv255
		v01 := float32(g.rgba[i01+c]) * inv255
		v11 := float32(g.rgba[i11+c]) * inv255
		top := v00*(1-tx) + v10*tx
		bot := v01*(1-tx) + v11*tx
		out[c] = top*(1-ty) + bot*ty
	}
	return out
}

// TerrainSplatMap stores 1 or 2 RGBA weight (splat) maps and up to 8 layer
// definitions for CPU-side gameplay queries via [TerrainSplatMap.SampleUV],
// [TerrainSplatMap.LayerAt], and [Heightmap.LayerAt].
//
// Splat map 0's R, G, B, A channels drive layers 0..3; splat map 1's R, G, B, A
// channels drive layers 4..7. Sampling uses bilinear texel-center interpolation
// with edge clamping, matching GPU linear clamp-to-edge sampling.
type TerrainSplatMap struct {
	// Layers holds the 1..8 material layer definitions in splat-channel order.
	Layers []TerrainLayerInfo
	// HeightBlendContrast controls optional height-based weight sharpening in [0, 1].
	// Zero disables height blending (pure linear splat weights).
	HeightBlendContrast float32
	// Origin is the world-space (X, Z) origin used by [TerrainSplatMap.LayerAt].
	Origin math3d.Vec2
	// Size is the world-space (X, Z) footprint used by [TerrainSplatMap.LayerAt].
	// When zero or non-positive, [TerrainSplatMap.LayerAt] treats (x, z) directly as UVs.
	Size math3d.Vec2

	splat0    splatGrid
	splat1    splatGrid
	hasSplat1 bool
}

// LoadTerrainSplatMap constructs a [TerrainSplatMap] from 1..8 layer definitions
// and 1 or 2 RGBA splat-map images (splat0 for layers 0..3, optional splat1 for
// layers 4..7). Note that standard [*image.NRGBA] preserves all 4 channels
// verbatim including zero-alpha texels.
func LoadTerrainSplatMap(layers []TerrainLayerInfo, maps ...image.Image) (*TerrainSplatMap, error) {
	if len(layers) == 0 || len(layers) > MaxTerrainLayers {
		return nil, fmt.Errorf("%w: layer count %d must be in [1, %d]", ErrInvalidTerrainSplat, len(layers), MaxTerrainLayers)
	}
	if len(maps) == 0 || len(maps) > 2 {
		return nil, fmt.Errorf("%w: expected 1 or 2 splat map images, got %d", ErrInvalidTerrainSplat, len(maps))
	}
	if len(layers) > 4 && len(maps) < 2 {
		return nil, fmt.Errorf("%w: %d layers require 2 RGBA splat maps, got %d", ErrInvalidTerrainSplat, len(layers), len(maps))
	}
	g0, err := splatGridFromImage(maps[0])
	if err != nil {
		return nil, fmt.Errorf("splat map 0: %w", err)
	}
	var g1 splatGrid
	has1 := false
	if len(maps) == 2 && maps[1] != nil {
		g1, err = splatGridFromImage(maps[1])
		if err != nil {
			return nil, fmt.Errorf("splat map 1: %w", err)
		}
		has1 = true
	} else if len(layers) > 4 {
		return nil, fmt.Errorf("%w: %d layers require a non-nil second splat map", ErrInvalidTerrainSplat, len(layers))
	}
	copied := make([]TerrainLayerInfo, len(layers))
	copy(copied, layers)
	return &TerrainSplatMap{
		Layers:    copied,
		splat0:    g0,
		splat1:    g1,
		hasSplat1: has1,
	}, nil
}

// NewTerrainSplatMapFromRGBA constructs a [TerrainSplatMap] from tightly packed
// straight-alpha RGBA8 byte buffers (width × height × 4 bytes each). splat1 may
// be nil when len(layers) <= 4.
func NewTerrainSplatMapFromRGBA(width, height int, layers []TerrainLayerInfo, splat0, splat1 []byte) (*TerrainSplatMap, error) {
	if len(layers) == 0 || len(layers) > MaxTerrainLayers {
		return nil, fmt.Errorf("%w: layer count %d must be in [1, %d]", ErrInvalidTerrainSplat, len(layers), MaxTerrainLayers)
	}
	g0, err := splatGridFromBytes(width, height, splat0)
	if err != nil {
		return nil, fmt.Errorf("splat map 0: %w", err)
	}
	var g1 splatGrid
	has1 := false
	if len(splat1) > 0 {
		g1, err = splatGridFromBytes(width, height, splat1)
		if err != nil {
			return nil, fmt.Errorf("splat map 1: %w", err)
		}
		has1 = true
	} else if len(layers) > 4 {
		return nil, fmt.Errorf("%w: %d layers require a non-empty second splat map", ErrInvalidTerrainSplat, len(layers))
	}
	copied := make([]TerrainLayerInfo, len(layers))
	copy(copied, layers)
	return &TerrainSplatMap{
		Layers:    copied,
		splat0:    g0,
		splat1:    g1,
		hasSplat1: has1,
	}, nil
}

// PackLayerWeightMaps packs 1 to 8 per-layer grayscale weight-map images into
// 1 (for 1..4 layers) or 2 (for 5..8 layers) straight-alpha RGBA [*image.NRGBA]
// splat maps suitable for [LoadTerrainSplatMap] and GPU terrain shaders.
//
// The output dimensions match the first non-nil weight image (or 1x1 when all
// images are nil, in which case layer 0 defaults to weight 255). Non-nil images
// with different dimensions are bilinearly resampled to match. Nil entries
// contribute zero weight to their channel.
func PackLayerWeightMaps(weights ...image.Image) (splat0, splat1 *image.NRGBA, err error) {
	if len(weights) == 0 || len(weights) > MaxTerrainLayers {
		return nil, nil, fmt.Errorf("%w: weight map count %d must be in [1, %d]", ErrInvalidTerrainSplat, len(weights), MaxTerrainLayers)
	}
	w, h := 0, 0
	for i, img := range weights {
		if img == nil {
			continue
		}
		b := img.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			return nil, nil, fmt.Errorf("%w: weight map %d has empty bounds %v", ErrInvalidTerrainSplat, i, b)
		}
		if w == 0 {
			w, h = b.Dx(), b.Dy()
		}
	}
	allNil := w == 0
	if allNil {
		w, h = 1, 1
	}

	splat0 = image.NewNRGBA(image.Rect(0, 0, w, h))
	if len(weights) > 4 {
		splat1 = image.NewNRGBA(image.Rect(0, 0, w, h))
	}
	if allNil {
		splat0.SetNRGBA(0, 0, color.NRGBA{R: 255})
		return splat0, splat1, nil
	}

	for li, img := range weights {
		if img == nil {
			continue
		}
		dst := splat0
		ch := li
		if li >= 4 {
			dst = splat1
			ch = li - 4
		}
		gray := resampleGray8(img, w, h)
		for y := range h {
			rowOff := dst.PixOffset(0, y)
			gBase := y * w
			for x := range w {
				dst.Pix[rowOff+x*4+ch] = gray[gBase+x]
			}
		}
	}
	return splat0, splat1, nil
}

func resampleGray8(img image.Image, dstW, dstH int) []byte {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	src := make([]byte, srcW*srcH)
	switch g := img.(type) {
	case *image.Gray:
		for y := range srcH {
			off := g.PixOffset(b.Min.X, b.Min.Y+y)
			copy(src[y*srcW:(y+1)*srcW], g.Pix[off:off+srcW])
		}
	case *image.Gray16:
		for y := range srcH {
			off := g.PixOffset(b.Min.X, b.Min.Y+y)
			rowBase := y * srcW
			for x := range srcW {
				p := off + x*2
				u16 := uint16(g.Pix[p])<<8 | uint16(g.Pix[p+1])
				src[rowBase+x] = uint8((uint32(u16)*255 + 32767) / 65535)
			}
		}
	default:
		for y := range srcH {
			rowBase := y * srcW
			for x := range srcW {
				c := color.Gray16Model.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray16)
				src[rowBase+x] = uint8((uint32(c.Y)*255 + 32767) / 65535)
			}
		}
	}
	if srcW == dstW && srcH == dstH {
		return src
	}
	dst := make([]byte, dstW*dstH)
	for y := range dstH {
		fy := (float64(y)+0.5)/float64(dstH)*float64(srcH) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		iy0 := clampIndex(y0, srcH)
		iy1 := clampIndex(y0+1, srcH)
		for x := range dstW {
			fx := (float64(x)+0.5)/float64(dstW)*float64(srcW) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			ix0 := clampIndex(x0, srcW)
			ix1 := clampIndex(x0+1, srcW)
			v00 := float64(src[iy0*srcW+ix0])
			v10 := float64(src[iy0*srcW+ix1])
			v01 := float64(src[iy1*srcW+ix0])
			v11 := float64(src[iy1*srcW+ix1])
			top := v00*(1-tx) + v10*tx
			bot := v01*(1-tx) + v11*tx
			v := top*(1-ty) + bot*ty
			dst[y*dstW+x] = uint8(math.Round(min(max(v, 0), 255)))
		}
	}
	return dst
}

// LoadTerrainSplatMapFromWeights constructs a [TerrainSplatMap] from 1..8 layer
// definitions and per-layer grayscale weight-map images. Trailing omitted
// entries in weights default to zero weight; images with different dimensions
// are resampled to match the first non-nil weight image.
func LoadTerrainSplatMapFromWeights(layers []TerrainLayerInfo, weights []image.Image) (*TerrainSplatMap, error) {
	if len(layers) == 0 || len(layers) > MaxTerrainLayers {
		return nil, fmt.Errorf("%w: layer count %d must be in [1, %d]", ErrInvalidTerrainSplat, len(layers), MaxTerrainLayers)
	}
	if len(weights) > len(layers) {
		return nil, fmt.Errorf("%w: got %d weight maps for %d layers", ErrInvalidTerrainSplat, len(weights), len(layers))
	}
	padded := make([]image.Image, len(layers))
	copy(padded, weights)
	s0, s1, err := PackLayerWeightMaps(padded...)
	if err != nil {
		return nil, err
	}
	if s1 != nil {
		return LoadTerrainSplatMap(layers, s0, s1)
	}
	return LoadTerrainSplatMap(layers, s0)
}

// Width returns the texel width of splat map 0, or 0 for a nil splat map.
func (s *TerrainSplatMap) Width() int {
	if s == nil {
		return 0
	}
	return s.splat0.width
}

// Height returns the texel height of splat map 0, or 0 for a nil splat map.
func (s *TerrainSplatMap) Height() int {
	if s == nil {
		return 0
	}
	return s.splat0.height
}

// TexelWeight returns the raw weight in [0, 1] stored for layer at texel (x, y),
// or 0 if out of range.
func (s *TerrainSplatMap) TexelWeight(layer, x, y int) float32 {
	if s == nil || layer < 0 || layer >= s.LayerCount() {
		return 0
	}
	g := &s.splat0
	ch := layer
	if layer >= 4 {
		if !s.hasSplat1 {
			return 0
		}
		g = &s.splat1
		ch = layer - 4
	}
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return 0
	}
	return float32(g.rgba[(y*g.width+x)*4+ch]) / 255.0
}

// SetTexelWeight writes a clamped [0, 1] weight for layer at texel (x, y).
// Out-of-range coordinates or layers are ignored.
func (s *TerrainSplatMap) SetTexelWeight(layer, x, y int, weight float32) {
	if s == nil || layer < 0 || layer >= s.LayerCount() {
		return
	}
	g := &s.splat0
	ch := layer
	if layer >= 4 {
		if !s.hasSplat1 {
			return
		}
		g = &s.splat1
		ch = layer - 4
	}
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return
	}
	if math.IsNaN(float64(weight)) || math.IsInf(float64(weight), 0) || weight < 0 {
		weight = 0
	} else if weight > 1 {
		weight = 1
	}
	g.rgba[(y*g.width+x)*4+ch] = uint8(math.Round(float64(weight) * 255.0))
}

// SplatImages returns copies of s's RGBA splat map(s) as [*image.NRGBA] images
// (splat1 is nil when s has at most 4 layers).
func (s *TerrainSplatMap) SplatImages() (splat0, splat1 *image.NRGBA) {
	if s == nil || s.splat0.width <= 0 || s.splat0.height <= 0 {
		return nil, nil
	}
	splat0 = image.NewNRGBA(image.Rect(0, 0, s.splat0.width, s.splat0.height))
	copy(splat0.Pix, s.splat0.rgba)
	if s.hasSplat1 && s.splat1.width > 0 && s.splat1.height > 0 {
		splat1 = image.NewNRGBA(image.Rect(0, 0, s.splat1.width, s.splat1.height))
		copy(splat1.Pix, s.splat1.rgba)
	}
	return splat0, splat1
}

// LayerWeightImage extracts layer's weight channel (0..7) as an 8-bit grayscale
// image, or nil if s is nil or layer is out of range.
func (s *TerrainSplatMap) LayerWeightImage(layer int) *image.Gray {
	if s == nil || layer < 0 || layer >= s.LayerCount() {
		return nil
	}
	g := &s.splat0
	ch := layer
	if layer >= 4 {
		if !s.hasSplat1 {
			return nil
		}
		g = &s.splat1
		ch = layer - 4
	}
	if g.width <= 0 || g.height <= 0 {
		return nil
	}
	img := image.NewGray(image.Rect(0, 0, g.width, g.height))
	for y := range g.height {
		dstOff := img.PixOffset(0, y)
		srcRow := y * g.width * 4
		for x := range g.width {
			img.Pix[dstOff+x] = g.rgba[srcRow+x*4+ch]
		}
	}
	return img
}

// LayerCount returns the number of active layers in s (0 for nil).
func (s *TerrainSplatMap) LayerCount() int {
	if s == nil {
		return 0
	}
	return min(len(s.Layers), MaxTerrainLayers)
}

// LayerName returns the name of layer index, or "" if out of range.
func (s *TerrainSplatMap) LayerName(index int) string {
	if s == nil || index < 0 || index >= len(s.Layers) || index >= MaxTerrainLayers {
		return ""
	}
	return s.Layers[index].Name
}

// LayerIndex returns the index of the first layer named name, or -1 if not found.
func (s *TerrainSplatMap) LayerIndex(name string) int {
	if s == nil || name == "" {
		return -1
	}
	n := s.LayerCount()
	for i := range n {
		if s.Layers[i].Name == name {
			return i
		}
	}
	return -1
}

// BlendTerrainWeights normalizes raw splat weights across [0, count) and, when
// contrast > 0, applies height-based blending using each layer's effective
// height in [0, 1]. Higher contrast values (clamped to [0, 0.98]) produce
// sharper transitions while guaranteeing zero weights stay zero and the output
// always sums to 1 (falling back to layer 0 = 1 if all raw weights are <= 0).
//
// This exact algebraic formula is shared with package render3d's GPU terrain
// shader and CPU reference evaluator.
func BlendTerrainWeights(raw, heights [MaxTerrainLayers]float32, count int, contrast float32) [MaxTerrainLayers]float32 {
	var out [MaxTerrainLayers]float32
	if count <= 0 {
		return out
	}
	if count > MaxTerrainLayers {
		count = MaxTerrainLayers
	}

	var sum float32
	var norm [MaxTerrainLayers]float32
	for i := range count {
		w := raw[i]
		if w < 0 || math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) {
			w = 0
		}
		norm[i] = w
		sum += w
	}
	if sum <= 1e-6 {
		out[0] = 1
		return out
	}
	invSum := 1 / sum
	for i := range count {
		norm[i] *= invSum
	}

	if contrast <= 0 || math.IsNaN(float64(contrast)) || math.IsInf(float64(contrast), 0) {
		return norm
	}
	c := math3d.Clamp(contrast, 0, 0.98)

	var scores [MaxTerrainLayers]float32
	var maxScore float32
	for i := range count {
		h := heights[i]
		if math.IsNaN(float64(h)) || math.IsInf(float64(h), 0) {
			h = 0.5
		}
		h = math3d.Clamp(h, 0, 1)
		sc := norm[i] * (0.5 + h)
		scores[i] = sc
		if sc > maxScore {
			maxScore = sc
		}
	}
	if maxScore <= 1e-7 {
		return norm
	}

	threshold := maxScore * c
	var effSum float32
	for i := range count {
		e := scores[i] - threshold
		if e < 0 {
			e = 0
		}
		out[i] = e
		effSum += e
	}
	if effSum <= 1e-7 {
		return norm
	}
	invEff := 1 / effSum
	for i := range count {
		out[i] *= invEff
	}
	return out
}

// SampleUV samples the splat map(s) at normalized terrain UV coordinates (u, v),
// clamping out-of-bounds UVs to [0, 1], applying optional height-based blending,
// and returning the dominant layer and normalized per-layer weights.
func (s *TerrainSplatMap) SampleUV(u, v float32) TerrainLayerSample {
	if s == nil || len(s.Layers) == 0 {
		return TerrainLayerSample{}
	}
	count := s.LayerCount()
	if math.IsNaN(float64(u)) || math.IsInf(float64(u), 0) {
		u = 0
	}
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		v = 0
	}
	u = math3d.Clamp(u, 0, 1)
	v = math3d.Clamp(v, 0, 1)

	s0 := s.splat0.sampleBilinear(u, v)
	var raw [MaxTerrainLayers]float32
	raw[0], raw[1], raw[2], raw[3] = s0[0], s0[1], s0[2], s0[3]
	if count > 4 && s.hasSplat1 {
		s1 := s.splat1.sampleBilinear(u, v)
		raw[4], raw[5], raw[6], raw[7] = s1[0], s1[1], s1[2], s1[3]
	}

	var heights [MaxTerrainLayers]float32
	if s.HeightBlendContrast > 0 {
		for i := range count {
			layer := s.Layers[i]
			hVal := float32(0.5)
			if layer.HeightMap != nil {
				uvScale := layer.effectiveUVScale()
				hVal = layer.HeightMap.SampleUV(u*uvScale.X, v*uvScale.Y)
			}
			heights[i] = math3d.Clamp(hVal+layer.HeightBias, 0, 1)
		}
	}

	weights := BlendTerrainWeights(raw, heights, count, s.HeightBlendContrast)
	dom := 0
	best := weights[0]
	for i := 1; i < count; i++ {
		if weights[i] > best {
			best = weights[i]
			dom = i
		}
	}
	return TerrainLayerSample{
		Dominant: dom,
		Name:     s.Layers[dom].Name,
		Weights:  weights,
		Count:    count,
	}
}

// LayerAt maps world coordinates (x, z) using s.Origin and s.Size, clamps to
// the terrain's [0, 1] UV extent, and returns the dominant layer and normalized
// weights. When attached to a [Heightmap], prefer [Heightmap.LayerAt], which
// derives UV bounds automatically from the heightmap's Origin, Cols, Rows, and
// CellSize.
func (s *TerrainSplatMap) LayerAt(x, z float32) TerrainLayerSample {
	if s == nil || len(s.Layers) == 0 {
		return TerrainLayerSample{}
	}
	u := x - s.Origin.X
	v := z - s.Origin.Y
	if s.Size.X > 0 {
		u /= s.Size.X
	}
	if s.Size.Y > 0 {
		v /= s.Size.Y
	}
	return s.SampleUV(u, v)
}

// LayerAt samples the terrain material layers attached to h.Splat at world
// coordinates (x, z). The boolean reports whether (x, z) lies inside the
// heightmap's XZ footprint; when (x, z) is outside the footprint but h and
// h.Splat are valid, LayerAt still clamps to the nearest terrain edge and
// returns the clamped edge sample alongside ok == false.
//
// Returns (zero, false) when h is invalid or h.Splat is nil/empty.
func (h *Heightmap) LayerAt(x, z float32) (TerrainLayerSample, bool) {
	if !h.valid() || h.Splat == nil || h.Splat.LayerCount() == 0 {
		return TerrainLayerSample{}, false
	}
	spanX := float32(h.Cols-1) * h.CellSize
	spanZ := float32(h.Rows-1) * h.CellSize
	u := (x - h.Origin.X) / spanX
	v := (z - h.Origin.Z) / spanZ
	inBounds := !math.IsNaN(float64(u)) && !math.IsNaN(float64(v)) &&
		!math.IsInf(float64(u), 0) && !math.IsInf(float64(v), 0) &&
		u >= 0 && u <= 1 && v >= 0 && v <= 1
	return h.Splat.SampleUV(u, v), inBounds
}

func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func wrap01(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0
	}
	f := float64(v)
	r := f - math.Floor(f)
	if r >= 1 {
		r = 0
	}
	return float32(r)
}

func wrapIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	r := i % n
	if r < 0 {
		r += n
	}
	return r
}

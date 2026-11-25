package world3d

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // decode GIF terrain images
	_ "image/jpeg" // decode JPEG terrain images
	_ "image/png"  // decode PNG terrain images
	"io"
	"io/fs"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"go.yaml.in/yaml/v3"
)

// TerrainFormatVersion is the current YAML terrain definition schema version.
const TerrainFormatVersion = 1

// TerrainSchemaURL is where the JSON Schema of version 1 terrain documents is published.
const TerrainSchemaURL = "https://game.nsgo.dev/schemas/terrain.v1.json"

// TerrainSchemaComment is the first line [EncodeTerrain] writes so YAML language
// servers validate and complete terrain definition files automatically.
const TerrainSchemaComment = "# yaml-language-server: $schema=" + TerrainSchemaURL

const fixTerrainDoc = "correct the terrain document to match its version 1 schema in docs/en/world3d.md"

// DefaultTerrainLayerRoughness is the roughness factor used when a
// [TerrainLayerDoc] omits both roughness and a metallicRoughness texture.
const DefaultTerrainLayerRoughness float32 = 0.85

// TerrainDoc is the root of a versioned YAML terrain definition specifying
// heightmap dimensions, splat or per-layer weight-map asset paths,
// height-blending and triplanar settings, up to 8 PBR material layers, and
// optional procedural foliage scatter layers with density maps.
type TerrainDoc struct {
	// Version is the schema version; must be 1.
	Version int `yaml:"version" schema:"required"`
	// Name is an optional human-readable name for the terrain.
	Name string `yaml:"name,omitempty"`
	// Size is the optional [cols, rows] heightmap grid dimensions (each >= 2). When set alongside heightmap, the decoded heightmap image must match these dimensions; when heightmap is omitted, a flat heightmap of this size is created.
	Size *[2]int `yaml:"size,omitempty"`
	// Heightmap is the asset path to the 16-bit (or 8-bit) grayscale PNG heightmap image.
	Heightmap string `yaml:"heightmap,omitempty"`
	// CellSize is the world-space XZ spacing between adjacent heightmap samples; omitted or 0 defaults to 1.
	CellSize float32 `yaml:"cellSize,omitempty" schema:"exclusiveMinimum=0"`
	// HeightScale scales normalized [0, 1] heightmap samples into world-space Y units.
	HeightScale float32 `yaml:"heightScale,omitempty" schema:"minimum=0"`
	// Origin is the world-space [x, y, z] origin of heightmap sample (0, 0); omitted defaults to [0, 0, 0].
	Origin *[3]float32 `yaml:"origin,omitempty"`
	// ChunkSize is the number of heightmap grid cells along each terrain LOD chunk edge; omitted or 0 defaults to 64.
	ChunkSize int `yaml:"chunkSize,omitempty" schema:"minimum=1"`
	// SplatMaps lists 1 or 2 RGBA weight-map asset paths: map 0 drives layers 0..3 (R,G,B,A) and map 1 drives layers 4..7 (R,G,B,A). Mutually exclusive with per-layer weightMap paths.
	SplatMaps []string `yaml:"splatMaps,omitempty"`
	// HeightBlendContrast enables height-based layer blending when > 0 in [0, 1]; higher values sharpen layer transitions using per-layer height maps, and 0 uses linear splat blending.
	HeightBlendContrast float32 `yaml:"heightBlendContrast,omitempty" schema:"minimum=0,maximum=1"`
	// Triplanar configures slope-driven triplanar texture projection on steep terrain faces.
	Triplanar *TerrainTriplanarDoc `yaml:"triplanar,omitempty"`
	// Layers lists 1 to 8 PBR terrain material layers in channel order (0..3 from splatMaps[0] RGBA and 4..7 from splatMaps[1] RGBA, or each driven by its own weightMap PNG).
	Layers []TerrainLayerDoc `yaml:"layers" schema:"required,minItems=1"`
	// Foliage lists optional procedural foliage scatter layers attached to this terrain, each with an optional grayscale density map PNG.
	Foliage []FoliageLayerDoc `yaml:"foliage,omitempty"`
}

// TerrainTriplanarDoc configures slope-driven triplanar projection in a [TerrainDoc].
type TerrainTriplanarDoc struct {
	// Enabled activates triplanar projection on steep slopes whose upward normal falls below SlopeThreshold.
	Enabled bool `yaml:"enabled,omitempty"`
	// SlopeThreshold is the world normal Y threshold in [0, 1] below which triplanar projection begins blending in; omitted or 0 defaults to 0.7.
	SlopeThreshold float32 `yaml:"slopeThreshold,omitempty" schema:"minimum=0,maximum=1"`
	// SlopeBlend is the transition width in [0, 1] over which planar UVs cross-fade into triplanar projection; omitted or 0 defaults to 0.15.
	SlopeBlend float32 `yaml:"slopeBlend,omitempty" schema:"minimum=0,maximum=1"`
	// Sharpness controls triplanar axis blend sharpness (typically 1..16); omitted or 0 defaults to 4.
	Sharpness float32 `yaml:"sharpness,omitempty" schema:"exclusiveMinimum=0"`
	// Scale is the world-space triplanar repeat rate (repeats per world unit); omitted or 0 defaults to 0.25.
	Scale float32 `yaml:"scale,omitempty" schema:"exclusiveMinimum=0"`
}

// TerrainLayerDoc describes one PBR terrain material layer in a [TerrainDoc].
type TerrainLayerDoc struct {
	// Name is the unique gameplay and authoring identifier for this layer (such as "grass", "dirt", "rock", "snow").
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Material is an optional asset path to a material resource file (such as materials/grass.material.yaml) supplying default base color, texture, roughness, and metallic values.
	Material string `yaml:"material,omitempty"`
	// WeightMap is an optional asset path to a per-layer grayscale weight-map PNG covering the terrain footprint; mutually exclusive with top-level splatMaps.
	WeightMap string `yaml:"weightMap,omitempty"`
	// Albedo is the asset path to the sRGB base-color texture.
	Albedo string `yaml:"albedo,omitempty"`
	// Normal is the asset path to the linear tangent-space normal map.
	Normal string `yaml:"normal,omitempty"`
	// MetallicRoughness is the asset path to the glTF metallic-roughness map (G = roughness, B = metallic).
	MetallicRoughness string `yaml:"metallicRoughness,omitempty"`
	// Height is the asset path to the optional grayscale height map used for height-based blending.
	Height string `yaml:"height,omitempty"`
	// Color is the linear RGB tint multiplier in [0, 1]^3; omitted defaults to [1, 1, 1].
	Color *[3]float32 `yaml:"color,omitempty"`
	// Roughness is the layer roughness factor in [0, 1]; omitted defaults to 1 when metallicRoughness is set or 0.85 otherwise.
	Roughness *float32 `yaml:"roughness,omitempty" schema:"minimum=0,maximum=1"`
	// Metallic is the layer metallic factor in [0, 1]; omitted defaults to 0.
	Metallic float32 `yaml:"metallic,omitempty" schema:"minimum=0,maximum=1"`
	// NormalScale scales the XY components of the tangent-space normal map; omitted defaults to 1.
	NormalScale *float32 `yaml:"normalScale,omitempty"`
	// UVScale is the per-layer UV tiling multiplier [u, v] across the terrain; omitted defaults to [1, 1].
	UVScale *[2]float32 `yaml:"uvScale,omitempty"`
	// HeightBias biases this layer's height map during height-based blending in [-1, 1].
	HeightBias float32 `yaml:"heightBias,omitempty" schema:"minimum=-1,maximum=1"`
}

// HasLayerWeightMaps reports whether any layer in d specifies a per-layer WeightMap path.
func (d *TerrainDoc) HasLayerWeightMaps() bool {
	if d == nil {
		return false
	}
	for _, l := range d.Layers {
		if l.WeightMap != "" {
			return true
		}
	}
	return false
}

// EffectiveCellSize returns d.CellSize, defaulting to 1 when zero.
func (d *TerrainDoc) EffectiveCellSize() float32 {
	if d == nil || d.CellSize == 0 {
		return 1
	}
	return d.CellSize
}

// EffectiveOrigin returns d.Origin as a [math3d.Vec3], defaulting to the zero vector when omitted.
func (d *TerrainDoc) EffectiveOrigin() math3d.Vec3 {
	if d == nil || d.Origin == nil {
		return math3d.Vec3Zero()
	}
	return math3d.Vec3{X: d.Origin[0], Y: d.Origin[1], Z: d.Origin[2]}
}

// EffectiveChunkSize returns d.ChunkSize, defaulting to 64 when omitted or zero.
func (d *TerrainDoc) EffectiveChunkSize() int {
	if d == nil || d.ChunkSize <= 0 {
		return defaultTerrainChunkCells
	}
	return d.ChunkSize
}

// LODConfig returns a [TerrainLODConfig] populated from d's ChunkSize.
func (d *TerrainDoc) LODConfig() TerrainLODConfig {
	return TerrainLODConfig{
		ChunkCells: d.EffectiveChunkSize(),
	}
}

// TerrainImages holds decoded images supplied to [TerrainDoc.BuildHeightmap]
// when constructing a runtime [*Heightmap] without reading from an [fs.FS].
type TerrainImages struct {
	// Heightmap is the grayscale heightmap image (required when TerrainDoc.Size is nil; at least 2x2 pixels).
	Heightmap image.Image
	// SplatMaps holds 0, 1, or 2 RGBA weight-map images matching TerrainDoc.SplatMaps.
	SplatMaps []image.Image
	// LayerWeights optionally maps a layer's Name to its decoded grayscale weight-map PNG when per-layer weightMap paths are used instead of SplatMaps.
	LayerWeights map[string]image.Image
	// LayerHeights optionally maps a layer's Name to its decoded grayscale height map for height-based blending.
	LayerHeights map[string]image.Image
	// FoliageDensityMaps optionally maps a foliage layer's Name to its decoded grayscale density map PNG.
	FoliageDensityMaps map[string]image.Image
}

// BuildHeightmap validates d and constructs a [*Heightmap] configured with d's
// CellSize, HeightScale, Origin, optional attached [*TerrainSplatMap] (from
// imgs.SplatMaps or imgs.LayerWeights, with HeightBlendContrast and per-layer
// height maps), and optional attached foliage density maps.
func (d *TerrainDoc) BuildHeightmap(imgs TerrainImages) (*Heightmap, error) {
	if err := ValidateTerrain(d); err != nil {
		return nil, err
	}
	var hm *Heightmap
	if imgs.Heightmap == nil {
		if d.Size == nil {
			return nil, &loaderr.Error{
				Field:    "heightmap",
				Problem:  "world3d: heightmap image is required",
				Expected: "non-nil grayscale image of at least 2x2 samples",
				Fix:      "supply a decoded heightmap image in TerrainImages.Heightmap or set size in the terrain document",
			}
		}
		hm = NewHeightmap(d.Size[0], d.Size[1], d.EffectiveCellSize(), d.HeightScale)
	} else {
		b := imgs.Heightmap.Bounds()
		if b.Dx() < 2 || b.Dy() < 2 {
			return nil, &loaderr.Error{
				Field:    "heightmap",
				Problem:  fmt.Sprintf("world3d: heightmap image is %dx%d, need at least 2x2", b.Dx(), b.Dy()),
				Expected: "image of at least 2x2 samples",
				Fix:      "use a heightmap image at least 2 pixels wide and 2 pixels tall",
			}
		}
		if d.Size != nil && (b.Dx() != d.Size[0] || b.Dy() != d.Size[1]) {
			return nil, &loaderr.Error{
				Field:    "heightmap",
				Problem:  fmt.Sprintf("world3d: heightmap image is %dx%d, does not match size [%d, %d]", b.Dx(), b.Dy(), d.Size[0], d.Size[1]),
				Expected: fmt.Sprintf("%dx%d grayscale image", d.Size[0], d.Size[1]),
				Fix:      "match size to the heightmap image dimensions or re-export the heightmap at the declared size",
			}
		}
		hm = LoadHeightmapGray(imgs.Heightmap, d.EffectiveCellSize(), d.HeightScale)
	}
	hm.Origin = d.EffectiveOrigin()

	attachLayerHeights := func(infos []TerrainLayerInfo) {
		if len(imgs.LayerHeights) == 0 {
			return
		}
		for i := range infos {
			if hImg := imgs.LayerHeights[infos[i].Name]; hImg != nil {
				infos[i].HeightMap = LoadTerrainHeightLayer(hImg)
			}
		}
	}

	switch {
	case len(imgs.SplatMaps) > 0:
		infos := d.LayerInfos()
		attachLayerHeights(infos)
		splat, err := LoadTerrainSplatMap(infos, imgs.SplatMaps...)
		if err != nil {
			return nil, err
		}
		splat.HeightBlendContrast = d.HeightBlendContrast
		hm.Splat = splat
	case len(imgs.LayerWeights) > 0:
		infos := d.LayerInfos()
		attachLayerHeights(infos)
		weights := make([]image.Image, len(d.Layers))
		for i, l := range d.Layers {
			weights[i] = imgs.LayerWeights[l.Name]
		}
		splat, err := LoadTerrainSplatMapFromWeights(infos, weights)
		if err != nil {
			return nil, err
		}
		splat.HeightBlendContrast = d.HeightBlendContrast
		hm.Splat = splat
	}

	if len(imgs.FoliageDensityMaps) > 0 {
		hm.FoliageDensity = make(map[string]*DensityMap, len(imgs.FoliageDensityMaps))
		for name, img := range imgs.FoliageDensityMaps {
			if img != nil {
				hm.FoliageDensity[name] = LoadDensityMapGray(img)
			}
		}
	}
	return hm, nil
}

// BuildLOD constructs a [*TerrainLOD] for hm using d's [TerrainDoc.LODConfig].
func (d *TerrainDoc) BuildLOD(hm *Heightmap) *TerrainLOD {
	return NewTerrainLOD(hm, d.LODConfig())
}

// BuildFoliageLayers validates d and converts every entry in d.Foliage into a
// runtime [FoliageLayer] using opts. Returns nil, nil when d.Foliage is empty.
func (d *TerrainDoc) BuildFoliageLayers(opts FoliageBuildOptions) ([]FoliageLayer, error) {
	if err := ValidateTerrain(d); err != nil {
		return nil, err
	}
	if len(d.Foliage) == 0 {
		return nil, nil
	}
	layers := make([]FoliageLayer, len(d.Foliage))
	for i, ld := range d.Foliage {
		layer, err := ld.ToLayer(opts)
		if err != nil {
			return nil, err
		}
		layers[i] = layer
	}
	return layers, nil
}

// ScatterFoliage builds every foliage layer declared in d.Foliage and scatters
// it across hm within region (or the full heightmap bounds when region is
// empty/zero). When a layer's density map is already loaded on hm.FoliageDensity
// and opts.LoadDensityMap is nil, the attached density map is reused directly.
func (d *TerrainDoc) ScatterFoliage(hm *Heightmap, region math3d.AABB, opts FoliageBuildOptions) ([]*FoliageField, error) {
	if err := ValidateTerrain(d); err != nil {
		return nil, err
	}
	if len(d.Foliage) == 0 {
		return nil, nil
	}
	fields := make([]*FoliageField, len(d.Foliage))
	for i, ld := range d.Foliage {
		layerOpts := opts
		if layerOpts.LoadDensityMap == nil && hm != nil && hm.FoliageDensity != nil {
			if attached := hm.FoliageDensity[ld.Name]; attached != nil {
				dm := attached
				layerOpts.LoadDensityMap = func(string) (*DensityMap, error) { return dm, nil }
			}
		}
		layer, err := ld.ToLayer(layerOpts)
		if err != nil {
			return nil, err
		}
		if layer.DensityMap == nil && hm != nil && hm.FoliageDensity != nil {
			layer.DensityMap = hm.FoliageDensity[ld.Name]
		}
		fields[i] = Scatter(hm, layer, region)
	}
	return fields, nil
}

// LoadHeightmap decodes the heightmap, optional splat or per-layer weight maps,
// optional per-layer height maps, and optional foliage density maps referenced
// by d from fsys, then builds a runtime [*Heightmap].
func (d *TerrainDoc) LoadHeightmap(fsys fs.FS) (*Heightmap, error) {
	if err := ValidateTerrain(d); err != nil {
		return nil, err
	}
	if fsys == nil {
		return nil, fmt.Errorf("world3d: nil filesystem")
	}
	var imgs TerrainImages
	if d.Heightmap != "" {
		hmImg, err := decodeFSImage(fsys, d.Heightmap)
		if err != nil {
			return nil, &loaderr.Error{
				Field:   "heightmap",
				Problem: fmt.Sprintf("world3d: load heightmap %q", d.Heightmap),
				Err:     err,
				Fix:     "ensure the heightmap path exists in the project and decodes as an image",
			}
		}
		imgs.Heightmap = hmImg
	} else if d.Size == nil {
		return nil, &loaderr.Error{
			Field:    "heightmap",
			Problem:  "world3d: terrain document has no heightmap path",
			Expected: "path to a grayscale heightmap image",
			Fix:      "set heightmap to the path of a 16-bit PNG image, or specify size for a flat terrain",
		}
	}

	hasWeights := false
	if len(d.SplatMaps) > 0 {
		hasWeights = true
		imgs.SplatMaps = make([]image.Image, len(d.SplatMaps))
		for i, sp := range d.SplatMaps {
			img, err := decodeFSImage(fsys, sp)
			if err != nil {
				return nil, &loaderr.Error{
					Field:   loaderr.Path("splatMaps", i),
					Problem: fmt.Sprintf("world3d: load splat map %q", sp),
					Err:     err,
					Fix:     "ensure the splat map path exists and decodes as an image",
				}
			}
			imgs.SplatMaps[i] = img
		}
	} else if d.HasLayerWeightMaps() {
		hasWeights = true
		imgs.LayerWeights = make(map[string]image.Image, len(d.Layers))
		for i, l := range d.Layers {
			if l.WeightMap == "" {
				continue
			}
			img, err := decodeFSImage(fsys, l.WeightMap)
			if err != nil {
				return nil, &loaderr.Error{
					Field:   loaderr.Path("layers", i, "weightMap"),
					Problem: fmt.Sprintf("world3d: load layer %q weightMap %q", l.Name, l.WeightMap),
					Err:     err,
					Fix:     "ensure the layer weightMap path exists and decodes as an image",
				}
			}
			imgs.LayerWeights[l.Name] = img
		}
	}

	if hasWeights {
		for i, l := range d.Layers {
			if l.Height == "" {
				continue
			}
			img, err := decodeFSImage(fsys, l.Height)
			if err != nil {
				return nil, &loaderr.Error{
					Field:   loaderr.Path("layers", i, "height"),
					Problem: fmt.Sprintf("world3d: load layer %q height map %q", l.Name, l.Height),
					Err:     err,
					Fix:     "ensure the layer height map path exists and decodes as an image",
				}
			}
			if imgs.LayerHeights == nil {
				imgs.LayerHeights = make(map[string]image.Image)
			}
			imgs.LayerHeights[l.Name] = img
		}
	}

	for i, fl := range d.Foliage {
		if fl.DensityMap == "" {
			continue
		}
		img, err := decodeFSImage(fsys, fl.DensityMap)
		if err != nil {
			return nil, &loaderr.Error{
				Field:   loaderr.Path("foliage", i, "densityMap"),
				Problem: fmt.Sprintf("world3d: load foliage layer %q densityMap %q", fl.Name, fl.DensityMap),
				Err:     err,
				Fix:     "ensure the foliage densityMap path exists and decodes as an image",
			}
		}
		if imgs.FoliageDensityMaps == nil {
			imgs.FoliageDensityMaps = make(map[string]image.Image)
		}
		imgs.FoliageDensityMaps[fl.Name] = img
	}

	return d.BuildHeightmap(imgs)
}

// LoadTerrainFS reads the YAML terrain definition at name in fsys, validates it,
// decodes its referenced heightmap and splat images from fsys, and returns the
// constructed [*Heightmap] alongside the parsed [*TerrainDoc].
func LoadTerrainFS(fsys fs.FS, name string) (*Heightmap, *TerrainDoc, error) {
	if fsys == nil {
		return nil, nil, fmt.Errorf("world3d: nil filesystem")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, nil, fmt.Errorf("world3d: read terrain %q: %w", name, err)
	}
	doc, err := ParseTerrainWith(data, name)
	if err != nil {
		return nil, nil, err
	}
	hm, err := doc.LoadHeightmap(fsys)
	if err != nil {
		return nil, nil, loaderr.Locate(err, name, data, fixTerrainDoc)
	}
	return hm, doc, nil
}

func decodeFSImage(fsys fs.FS, p string) (image.Image, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// LayerInfos converts d.Layers into [TerrainLayerInfo] metadata (name, UV scale,
// and height bias) suitable for constructing a [TerrainSplatMap].
func (d *TerrainDoc) LayerInfos() []TerrainLayerInfo {
	if d == nil || len(d.Layers) == 0 {
		return nil
	}
	out := make([]TerrainLayerInfo, len(d.Layers))
	for i, l := range d.Layers {
		out[i] = TerrainLayerInfo{
			Name:       l.Name,
			UVScale:    l.EffectiveUVScale(),
			HeightBias: l.HeightBias,
		}
	}
	return out
}

// EffectiveColor returns l.Color when set, or linear white [1, 1, 1] when omitted.
func (l TerrainLayerDoc) EffectiveColor() [3]float32 {
	if l.Color == nil {
		return [3]float32{1, 1, 1}
	}
	return *l.Color
}

// EffectiveRoughness returns l.Roughness when set, 1.0 when MetallicRoughness
// is set, or [DefaultTerrainLayerRoughness] (0.85) otherwise.
func (l TerrainLayerDoc) EffectiveRoughness() float32 {
	if l.Roughness != nil {
		return *l.Roughness
	}
	if l.MetallicRoughness != "" {
		return 1
	}
	return DefaultTerrainLayerRoughness
}

// EffectiveNormalScale returns l.NormalScale when set, or 1.0 when omitted.
func (l TerrainLayerDoc) EffectiveNormalScale() float32 {
	if l.NormalScale == nil {
		return 1
	}
	return *l.NormalScale
}

// EffectiveUVScale returns l.UVScale when set, or [1, 1] when omitted.
func (l TerrainLayerDoc) EffectiveUVScale() math3d.Vec2 {
	if l.UVScale == nil {
		return math3d.Vec2{X: 1, Y: 1}
	}
	return math3d.Vec2{X: l.UVScale[0], Y: l.UVScale[1]}
}

// EffectiveSlopeThreshold returns t.SlopeThreshold, defaulting to 0.7 when zero.
func (t *TerrainTriplanarDoc) EffectiveSlopeThreshold() float32 {
	if t == nil || t.SlopeThreshold == 0 {
		return 0.7
	}
	return t.SlopeThreshold
}

// EffectiveSlopeBlend returns t.SlopeBlend, defaulting to 0.15 when zero.
func (t *TerrainTriplanarDoc) EffectiveSlopeBlend() float32 {
	if t == nil || t.SlopeBlend == 0 {
		return 0.15
	}
	return t.SlopeBlend
}

// EffectiveSharpness returns t.Sharpness, defaulting to 4.0 when zero.
func (t *TerrainTriplanarDoc) EffectiveSharpness() float32 {
	if t == nil || t.Sharpness == 0 {
		return 4
	}
	return t.Sharpness
}

// EffectiveScale returns t.Scale, defaulting to 0.25 when zero.
func (t *TerrainTriplanarDoc) EffectiveScale() float32 {
	if t == nil || t.Scale == 0 {
		return 0.25
	}
	return t.Scale
}

// ParseTerrain decodes and validates a YAML terrain definition document. Unknown
// fields and trailing YAML documents are rejected. Returned errors are
// [*loaderr.Error] values carrying line, column, field path, and a fix hint.
func ParseTerrain(data []byte) (*TerrainDoc, error) {
	return ParseTerrainWith(data, "")
}

// ParseTerrainWith is [ParseTerrain] with an optional source file name attached to errors.
func ParseTerrainWith(data []byte, file string) (*TerrainDoc, error) {
	doc, err := parseTerrainDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixTerrainDoc)
	}
	return doc, nil
}

func parseTerrainDoc(data []byte) (*TerrainDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			Problem: "world3d: empty terrain document",
			Fix:     fixTerrainDoc,
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc TerrainDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("world3d: decode terrain: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    terrainTrailingDocumentLine(data),
				Problem: "world3d: unexpected trailing YAML document",
				Fix:     "keep one terrain definition per file; remove the --- and the document after it",
			}
		}
		return nil, fmt.Errorf("world3d: decode terrain: %w", err)
	}
	if err := ValidateTerrain(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func terrainTrailingDocumentLine(data []byte) int {
	sawContent := false
	for i, line := range bytes.Split(data, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("---")); ok && (len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r') {
			if sawContent {
				return i + 1
			}
			continue
		}
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] != '#' {
			sawContent = true
		}
	}
	return 0
}

func terrainFinite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// ValidateTerrain checks that doc conforms to the version 1 terrain schema.
func ValidateTerrain(doc *TerrainDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "world3d: nil terrain document", Fix: fixTerrainDoc}
	}
	if doc.Version != TerrainFormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("world3d: unsupported terrain version %d", doc.Version),
			Expected: strconv.Itoa(TerrainFormatVersion),
			Fix:      fmt.Sprintf("set version to %d", TerrainFormatVersion),
		}
	}
	if doc.Size != nil {
		if doc.Size[0] < 2 || doc.Size[1] < 2 {
			return &loaderr.Error{
				Field:    "size",
				Problem:  fmt.Sprintf("world3d: invalid terrain size [%d, %d]", doc.Size[0], doc.Size[1]),
				Expected: "two integers >= 2 ([cols, rows])",
				Fix:      "set size to [cols, rows] where both dimensions are at least 2",
			}
		}
	}
	if doc.CellSize != 0 && (!terrainFinite(doc.CellSize) || doc.CellSize <= 0) {
		return &loaderr.Error{
			Field:    "cellSize",
			Problem:  fmt.Sprintf("world3d: invalid cellSize %v", doc.CellSize),
			Expected: "a finite positive number",
			Fix:      "set cellSize to a positive number or omit it to use 1",
		}
	}
	if !terrainFinite(doc.HeightScale) || doc.HeightScale < 0 {
		return &loaderr.Error{
			Field:    "heightScale",
			Problem:  fmt.Sprintf("world3d: invalid heightScale %v", doc.HeightScale),
			Expected: "a finite non-negative number",
			Fix:      "set heightScale to a finite value >= 0",
		}
	}
	if doc.Origin != nil {
		for i, v := range *doc.Origin {
			if !terrainFinite(v) {
				return &loaderr.Error{
					Field:    loaderr.Path("origin", i),
					Problem:  fmt.Sprintf("world3d: non-finite origin[%d] %v", i, v),
					Expected: "a finite number",
					Fix:      "use finite coordinates for origin",
				}
			}
		}
	}
	if doc.ChunkSize < 0 {
		return &loaderr.Error{
			Field:    "chunkSize",
			Problem:  fmt.Sprintf("world3d: invalid chunkSize %d", doc.ChunkSize),
			Expected: "a positive integer",
			Fix:      "set chunkSize to a positive integer or omit it to use 64",
		}
	}
	if len(doc.SplatMaps) > 2 {
		return &loaderr.Error{
			Field:    "splatMaps",
			Problem:  fmt.Sprintf("world3d: too many splat maps (%d > 2)", len(doc.SplatMaps)),
			Expected: "at most 2 RGBA splat map paths",
			Fix:      "list 1 splat map for up to 4 layers, or 2 splat maps for up to 8 layers",
		}
	}
	for i, p := range doc.SplatMaps {
		if p == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("splatMaps", i),
				Problem:  "world3d: splat map path must not be empty",
				Expected: "non-empty asset path",
				Fix:      "provide the path to an RGBA splat map image",
			}
		}
	}
	if !terrainFinite(doc.HeightBlendContrast) || doc.HeightBlendContrast < 0 || doc.HeightBlendContrast > 1 {
		return &loaderr.Error{
			Field:    "heightBlendContrast",
			Problem:  fmt.Sprintf("world3d: invalid heightBlendContrast %v", doc.HeightBlendContrast),
			Expected: "a finite number in [0, 1]",
			Fix:      "set heightBlendContrast between 0 (linear) and 1 (sharpest)",
		}
	}
	if doc.Triplanar != nil {
		tp := doc.Triplanar
		if !terrainFinite(tp.SlopeThreshold) || tp.SlopeThreshold < 0 || tp.SlopeThreshold > 1 {
			return &loaderr.Error{
				Field:    "triplanar.slopeThreshold",
				Problem:  fmt.Sprintf("world3d: invalid triplanar.slopeThreshold %v", tp.SlopeThreshold),
				Expected: "a finite number in [0, 1]",
				Fix:      "set triplanar.slopeThreshold in [0, 1]",
			}
		}
		if !terrainFinite(tp.SlopeBlend) || tp.SlopeBlend < 0 || tp.SlopeBlend > 1 {
			return &loaderr.Error{
				Field:    "triplanar.slopeBlend",
				Problem:  fmt.Sprintf("world3d: invalid triplanar.slopeBlend %v", tp.SlopeBlend),
				Expected: "a finite number in [0, 1]",
				Fix:      "set triplanar.slopeBlend in [0, 1]",
			}
		}
		if tp.Sharpness != 0 && (!terrainFinite(tp.Sharpness) || tp.Sharpness <= 0) {
			return &loaderr.Error{
				Field:    "triplanar.sharpness",
				Problem:  fmt.Sprintf("world3d: invalid triplanar.sharpness %v", tp.Sharpness),
				Expected: "a finite positive number",
				Fix:      "set triplanar.sharpness > 0 or omit it to use 4",
			}
		}
		if tp.Scale != 0 && (!terrainFinite(tp.Scale) || tp.Scale <= 0) {
			return &loaderr.Error{
				Field:    "triplanar.scale",
				Problem:  fmt.Sprintf("world3d: invalid triplanar.scale %v", tp.Scale),
				Expected: "a finite positive number",
				Fix:      "set triplanar.scale > 0 or omit it to use 0.25",
			}
		}
	}

	if len(doc.Layers) == 0 || len(doc.Layers) > MaxTerrainLayers {
		return &loaderr.Error{
			Field:    "layers",
			Problem:  fmt.Sprintf("world3d: invalid layer count %d", len(doc.Layers)),
			Expected: fmt.Sprintf("1 to %d material layers", MaxTerrainLayers),
			Fix:      fmt.Sprintf("define between 1 and %d entries under layers", MaxTerrainLayers),
		}
	}
	if len(doc.Layers) > 4 && len(doc.SplatMaps) == 1 {
		return &loaderr.Error{
			Field:    "splatMaps",
			Problem:  fmt.Sprintf("world3d: %d layers require 2 splatMaps, got 1", len(doc.Layers)),
			Expected: "2 RGBA splat map paths for 5..8 layers",
			Fix:      "add a second RGBA splat map path to splatMaps for layers 4..7",
		}
	}
	if len(doc.SplatMaps) > 0 && doc.HasLayerWeightMaps() {
		return &loaderr.Error{
			Field:    "splatMaps",
			Problem:  "world3d: terrain cannot specify both splatMaps and per-layer weightMap paths",
			Expected: "either top-level splatMaps or per-layer weightMap paths, not both",
			Fix:      "remove splatMaps to use per-layer weightMap PNGs, or remove layer weightMap fields to use packed RGBA splatMaps",
		}
	}

	seenName := make(map[string]int, len(doc.Layers))
	for i, l := range doc.Layers {
		if l.Name == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "name"),
				Problem:  "world3d: terrain layer name must not be empty",
				Expected: "non-empty unique string",
				Fix:      "give each terrain layer a unique name such as \"grass\" or \"rock\"",
			}
		}
		if prev, dup := seenName[l.Name]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("layers", i, "name"),
				Problem: fmt.Sprintf("world3d: duplicate terrain layer name %q (first defined at layers[%d])", l.Name, prev),
				Fix:     "give every terrain layer a distinct name",
			}
		}
		seenName[l.Name] = i
		if l.Color != nil {
			for c, val := range *l.Color {
				if !terrainFinite(val) || val < 0 || val > 1 {
					return &loaderr.Error{
						Field:    loaderr.Path("layers", i, "color", c),
						Problem:  fmt.Sprintf("world3d: layer %q color[%d] = %v out of range", l.Name, c, val),
						Expected: "a finite number in [0, 1]",
						Fix:      "set each RGB color channel in [0, 1]",
					}
				}
			}
		}
		if l.Roughness != nil && (!terrainFinite(*l.Roughness) || *l.Roughness < 0 || *l.Roughness > 1) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "roughness"),
				Problem:  fmt.Sprintf("world3d: layer %q roughness = %v out of range", l.Name, *l.Roughness),
				Expected: "a finite number in [0, 1]",
				Fix:      "set roughness between 0 and 1",
			}
		}
		if !terrainFinite(l.Metallic) || l.Metallic < 0 || l.Metallic > 1 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "metallic"),
				Problem:  fmt.Sprintf("world3d: layer %q metallic = %v out of range", l.Name, l.Metallic),
				Expected: "a finite number in [0, 1]",
				Fix:      "set metallic between 0 and 1",
			}
		}
		if l.NormalScale != nil && !terrainFinite(*l.NormalScale) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "normalScale"),
				Problem:  fmt.Sprintf("world3d: layer %q normalScale = %v must be finite", l.Name, *l.NormalScale),
				Expected: "a finite number",
				Fix:      "set normalScale to a finite number",
			}
		}
		if l.UVScale != nil {
			for c, val := range *l.UVScale {
				if !terrainFinite(val) || val <= 0 {
					return &loaderr.Error{
						Field:    loaderr.Path("layers", i, "uvScale", c),
						Problem:  fmt.Sprintf("world3d: layer %q uvScale[%d] = %v must be positive", l.Name, c, val),
						Expected: "a finite positive number",
						Fix:      "set both uvScale components to positive numbers",
					}
				}
			}
		}
		if !terrainFinite(l.HeightBias) || l.HeightBias < -1 || l.HeightBias > 1 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "heightBias"),
				Problem:  fmt.Sprintf("world3d: layer %q heightBias = %v out of range", l.Name, l.HeightBias),
				Expected: "a finite number in [-1, 1]",
				Fix:      "set heightBias between -1 and 1",
			}
		}
	}
	if len(doc.Foliage) > 0 {
		if err := validateFoliageLayers(doc.Foliage, "foliage"); err != nil {
			return err
		}
	}
	return nil
}

// EncodeTerrain validates doc and serializes it as deterministic 2-space YAML
// prefixed with [TerrainSchemaComment].
func EncodeTerrain(doc *TerrainDoc) ([]byte, error) {
	if err := ValidateTerrain(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(TerrainSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("world3d: encode terrain: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("world3d: encode terrain: %w", err)
	}
	return buf.Bytes(), nil
}

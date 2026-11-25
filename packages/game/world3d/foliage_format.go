package world3d

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"go.yaml.in/yaml/v3"
)

// FoliageFormatVersion is the current YAML foliage definition schema version.
const FoliageFormatVersion = 1

// FoliageSchemaURL is where the JSON Schema of version 1 foliage documents is published.
const FoliageSchemaURL = "https://game.nsgo.dev/schemas/foliage.v1.json"

// FoliageSchemaComment is the first line [EncodeFoliage] writes so YAML language
// servers validate and complete foliage definition files automatically.
const FoliageSchemaComment = "# yaml-language-server: $schema=" + FoliageSchemaURL

const fixFoliageDoc = "correct the foliage document to match its version 1 schema in docs/en/world3d.md"

// FoliageDoc is the root of a versioned YAML foliage definition declaring one
// or more procedural foliage scatter layers over a terrain heightmap.
type FoliageDoc struct {
	// Version is the schema version; must be 1.
	Version int `yaml:"version" schema:"required"`
	// Name is an optional human-readable name for the foliage set.
	Name string `yaml:"name,omitempty"`
	// Terrain is an optional asset path to the terrain definition (*.terrain.yaml) these foliage layers scatter over.
	Terrain string `yaml:"terrain,omitempty"`
	// Layers lists 1 or more procedural foliage scatter layers in authoring order.
	Layers []FoliageLayerDoc `yaml:"layers" schema:"required,minItems=1"`
}

// FoliageLayerDoc describes one procedural foliage scatter layer in a [FoliageDoc].
type FoliageLayerDoc struct {
	// Name is the unique identifier for this foliage layer (such as "grass", "pine_tree", "boulder").
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Mesh is the asset path to the foliage mesh (such as a .gltf or .glb model).
	Mesh string `yaml:"mesh" schema:"required,minLength=1"`
	// Material is an optional asset path to a material override for the foliage mesh.
	Material string `yaml:"material,omitempty"`
	// DensityMap is an optional asset path to a grayscale density mask image covering the terrain bounds.
	DensityMap string `yaml:"densityMap,omitempty"`
	// Density is the target candidate instances per square meter; set either density or spacing (not both).
	Density float32 `yaml:"density,omitempty" schema:"exclusiveMinimum=0"`
	// Spacing is the target average world-space spacing between instances in meters (equivalent to density = 1 / spacing^2); set either spacing or density (not both).
	Spacing float32 `yaml:"spacing,omitempty" schema:"exclusiveMinimum=0"`
	// MinScale is the minimum uniform instance scale; omitted defaults to 1 (or maxScale when only maxScale is set).
	MinScale float32 `yaml:"minScale,omitempty" schema:"exclusiveMinimum=0"`
	// MaxScale is the maximum uniform instance scale; omitted defaults to 1 (or minScale when only minScale is set).
	MaxScale float32 `yaml:"maxScale,omitempty" schema:"exclusiveMinimum=0"`
	// AlignToNormal blends instance up-axis alignment from upright (0) to terrain surface normal (1).
	AlignToNormal float32 `yaml:"alignToNormal,omitempty" schema:"minimum=0,maximum=1"`
	// MinSlopeDeg is the minimum terrain slope in degrees [0, 90] where instances may spawn.
	MinSlopeDeg float32 `yaml:"minSlopeDeg,omitempty" schema:"minimum=0,maximum=90"`
	// MaxSlopeDeg is the maximum terrain slope in degrees [0, 90] where instances may spawn; 0 with minSlopeDeg 0 allows all slopes.
	MaxSlopeDeg float32 `yaml:"maxSlopeDeg,omitempty" schema:"minimum=0,maximum=90"`
	// MinHeight is the minimum world-space Y elevation where instances may spawn.
	MinHeight *float32 `yaml:"minHeight,omitempty"`
	// MaxHeight is the maximum world-space Y elevation where instances may spawn.
	MaxHeight *float32 `yaml:"maxHeight,omitempty"`
	// BaseMaxDistance is the default maximum draw distance in world units; 0 means uncapped unless limited by QualityProfile.
	BaseMaxDistance float32 `yaml:"baseMaxDistance,omitempty" schema:"minimum=0"`
	// BoundingRadius is the bounding sphere radius at scale 1 around the mesh pivot for frustum culling and distance fade; 0 defaults to 1 around the pivot.
	BoundingRadius float32 `yaml:"boundingRadius,omitempty" schema:"minimum=0"`
	// Seed is the deterministic PRNG seed for repeatable scattering.
	Seed uint64 `yaml:"seed,omitempty"`
}

// EffectiveDensity returns the candidate density in instances per square meter:
// l.Density when positive, or 1 / (l.Spacing * l.Spacing) when l.Spacing is positive.
func (l FoliageLayerDoc) EffectiveDensity() float32 {
	if l.Density > 0 {
		return l.Density
	}
	if l.Spacing > 0 {
		return 1 / (l.Spacing * l.Spacing)
	}
	return 0
}

// FoliageBuildOptions supplies runtime mesh/material ID resolution and density
// map loading when converting a [FoliageDoc] into runtime [FoliageLayer] values.
type FoliageBuildOptions struct {
	// ResolveMesh maps a layer's Mesh asset path to a runtime MeshID. When nil, MeshID is 0.
	ResolveMesh func(path string) (uint32, error)
	// ResolveMaterial maps a layer's Material asset path (when non-empty) to a runtime MaterialID. When nil, MaterialID is 0.
	ResolveMaterial func(path string) (uint32, error)
	// LoadDensityMap loads a layer's DensityMap asset path (when non-empty) into a [*DensityMap].
	// When nil and FS is non-nil, the image is decoded from FS with [LoadDensityMapGray].
	LoadDensityMap func(path string) (*DensityMap, error)
	// FS is an optional filesystem used to decode DensityMap images when LoadDensityMap is nil.
	FS fs.FS
}

// ToLayer converts l into a runtime [FoliageLayer] using opts.
func (l FoliageLayerDoc) ToLayer(opts FoliageBuildOptions) (FoliageLayer, error) {
	var meshID uint32
	if opts.ResolveMesh != nil {
		id, err := opts.ResolveMesh(l.Mesh)
		if err != nil {
			return FoliageLayer{}, fmt.Errorf("world3d: foliage layer %q mesh %q: %w", l.Name, l.Mesh, err)
		}
		meshID = id
	}
	var matID uint32
	if l.Material != "" && opts.ResolveMaterial != nil {
		id, err := opts.ResolveMaterial(l.Material)
		if err != nil {
			return FoliageLayer{}, fmt.Errorf("world3d: foliage layer %q material %q: %w", l.Name, l.Material, err)
		}
		matID = id
	}
	var dm *DensityMap
	if l.DensityMap != "" {
		switch {
		case opts.LoadDensityMap != nil:
			loaded, err := opts.LoadDensityMap(l.DensityMap)
			if err != nil {
				return FoliageLayer{}, fmt.Errorf("world3d: foliage layer %q densityMap %q: %w", l.Name, l.DensityMap, err)
			}
			dm = loaded
		case opts.FS != nil:
			img, err := decodeFSImage(opts.FS, l.DensityMap)
			if err != nil {
				return FoliageLayer{}, fmt.Errorf("world3d: foliage layer %q densityMap %q: %w", l.Name, l.DensityMap, err)
			}
			dm = LoadDensityMapGray(img)
		}
	}

	var minH, maxH float32
	switch {
	case l.MinHeight != nil && l.MaxHeight != nil:
		minH, maxH = *l.MinHeight, *l.MaxHeight
	case l.MinHeight != nil:
		minH, maxH = *l.MinHeight, math.MaxFloat32
	case l.MaxHeight != nil:
		minH, maxH = -math.MaxFloat32, *l.MaxHeight
	}

	return FoliageLayer{
		Name:            l.Name,
		MeshID:          meshID,
		MaterialID:      matID,
		Density:         l.EffectiveDensity(),
		MinScale:        l.MinScale,
		MaxScale:        l.MaxScale,
		AlignToNormal:   l.AlignToNormal,
		MinSlopeDeg:     l.MinSlopeDeg,
		MaxSlopeDeg:     l.MaxSlopeDeg,
		MinHeight:       minH,
		MaxHeight:       maxH,
		DensityMap:      dm,
		BaseMaxDistance: l.BaseMaxDistance,
		Seed:            l.Seed,
		BoundingRadius:  l.BoundingRadius,
	}, nil
}

// BuildLayers validates d and converts every entry in d.Layers into a runtime [FoliageLayer].
func (d *FoliageDoc) BuildLayers(opts FoliageBuildOptions) ([]FoliageLayer, error) {
	if err := ValidateFoliage(d); err != nil {
		return nil, err
	}
	layers := make([]FoliageLayer, len(d.Layers))
	for i, ld := range d.Layers {
		layer, err := ld.ToLayer(opts)
		if err != nil {
			return nil, err
		}
		layers[i] = layer
	}
	return layers, nil
}

// ScatterAll builds every foliage layer in d and scatters it across hm within region
// (or the full heightmap bounds when region is empty/zero).
func (d *FoliageDoc) ScatterAll(hm *Heightmap, region math3d.AABB, opts FoliageBuildOptions) ([]*FoliageField, error) {
	layers, err := d.BuildLayers(opts)
	if err != nil {
		return nil, err
	}
	fields := make([]*FoliageField, len(layers))
	for i, layer := range layers {
		fields[i] = Scatter(hm, layer, region)
	}
	return fields, nil
}

// FoliageLayerToDoc converts a runtime [FoliageLayer] and its asset paths into
// a [FoliageLayerDoc] suitable for deterministic YAML encoding.
func FoliageLayerToDoc(layer FoliageLayer, meshPath, materialPath, densityMapPath string) FoliageLayerDoc {
	doc := FoliageLayerDoc{
		Name:            layer.Name,
		Mesh:            meshPath,
		Material:        materialPath,
		DensityMap:      densityMapPath,
		Density:         layer.Density,
		MinScale:        layer.MinScale,
		MaxScale:        layer.MaxScale,
		AlignToNormal:   layer.AlignToNormal,
		MinSlopeDeg:     layer.MinSlopeDeg,
		MaxSlopeDeg:     layer.MaxSlopeDeg,
		BaseMaxDistance: layer.BaseMaxDistance,
		BoundingRadius:  layer.BoundingRadius,
		Seed:            layer.Seed,
	}
	if layer.MinHeight != 0 || layer.MaxHeight != 0 {
		if layer.MinHeight > -math.MaxFloat32 {
			v := layer.MinHeight
			doc.MinHeight = &v
		}
		if layer.MaxHeight < math.MaxFloat32 {
			v := layer.MaxHeight
			doc.MaxHeight = &v
		}
	}
	return doc
}

// ParseFoliage decodes and validates a YAML foliage definition document. Unknown
// fields and trailing YAML documents are rejected. Returned errors are
// [*loaderr.Error] values carrying line, column, field path, and a fix hint.
func ParseFoliage(data []byte) (*FoliageDoc, error) {
	return ParseFoliageWith(data, "")
}

// ParseFoliageWith is [ParseFoliage] with an optional source file name attached to errors.
func ParseFoliageWith(data []byte, file string) (*FoliageDoc, error) {
	doc, err := parseFoliageDoc(data)
	if err != nil {
		return nil, loaderr.Locate(err, file, data, fixFoliageDoc)
	}
	return doc, nil
}

func parseFoliageDoc(data []byte) (*FoliageDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			Problem: "world3d: empty foliage document",
			Fix:     fixFoliageDoc,
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc FoliageDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("world3d: decode foliage: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				Line:    terrainTrailingDocumentLine(data),
				Problem: "world3d: unexpected trailing YAML document",
				Fix:     "keep one foliage definition per file; remove the --- and the document after it",
			}
		}
		return nil, fmt.Errorf("world3d: decode foliage: %w", err)
	}
	if err := ValidateFoliage(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// LoadFoliageFS reads and validates the YAML foliage definition at name in fsys.
func LoadFoliageFS(fsys fs.FS, name string) (*FoliageDoc, error) {
	if fsys == nil {
		return nil, fmt.Errorf("world3d: nil filesystem")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("world3d: read foliage %q: %w", name, err)
	}
	return ParseFoliageWith(data, name)
}

// ValidateFoliage checks that doc conforms to the version 1 foliage schema.
func ValidateFoliage(doc *FoliageDoc) error {
	if doc == nil {
		return &loaderr.Error{Problem: "world3d: nil foliage document", Fix: fixFoliageDoc}
	}
	if doc.Version != FoliageFormatVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("world3d: unsupported foliage version %d", doc.Version),
			Expected: strconv.Itoa(FoliageFormatVersion),
			Fix:      fmt.Sprintf("set version to %d", FoliageFormatVersion),
		}
	}
	if len(doc.Layers) == 0 {
		return &loaderr.Error{
			Field:    "layers",
			Problem:  "world3d: foliage document must declare at least one layer",
			Expected: "at least 1 foliage layer",
			Fix:      "add one or more foliage layers under layers",
		}
	}

	seenName := make(map[string]int, len(doc.Layers))
	for i, l := range doc.Layers {
		if l.Name == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "name"),
				Problem:  "world3d: foliage layer name must not be empty",
				Expected: "non-empty unique string",
				Fix:      "give each foliage layer a unique name such as \"grass\" or \"pine_tree\"",
			}
		}
		if prev, dup := seenName[l.Name]; dup {
			return &loaderr.Error{
				Field:   loaderr.Path("layers", i, "name"),
				Problem: fmt.Sprintf("world3d: duplicate foliage layer name %q (first defined at layers[%d])", l.Name, prev),
				Fix:     "give every foliage layer a distinct name",
			}
		}
		seenName[l.Name] = i
		if l.Mesh == "" {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "mesh"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q mesh path must not be empty", l.Name),
				Expected: "non-empty asset path",
				Fix:      "set mesh to the asset path of a 3D mesh such as a .gltf or .glb file",
			}
		}
		if !terrainFinite(l.Density) || l.Density < 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "density"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid density %v", l.Name, l.Density),
				Expected: "a finite positive number",
				Fix:      "set density to a positive number of instances per square meter",
			}
		}
		if !terrainFinite(l.Spacing) || l.Spacing < 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "spacing"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid spacing %v", l.Name, l.Spacing),
				Expected: "a finite positive number",
				Fix:      "set spacing to a positive world-space distance between instances",
			}
		}
		if l.Density > 0 && l.Spacing > 0 {
			return &loaderr.Error{
				Field:   loaderr.Path("layers", i, "spacing"),
				Problem: fmt.Sprintf("world3d: foliage layer %q sets both density (%v) and spacing (%v)", l.Name, l.Density, l.Spacing),
				Fix:     "set either density or spacing, not both",
			}
		}
		if l.Density == 0 && l.Spacing == 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i),
				Problem:  fmt.Sprintf("world3d: foliage layer %q must set either density or spacing", l.Name),
				Expected: "positive density or positive spacing",
				Fix:      "set density (instances per square meter) or spacing (average distance in meters)",
			}
		}
		if l.MinScale != 0 && (!terrainFinite(l.MinScale) || l.MinScale <= 0) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "minScale"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid minScale %v", l.Name, l.MinScale),
				Expected: "a finite positive number",
				Fix:      "set minScale to a positive number or omit it to use 1",
			}
		}
		if l.MaxScale != 0 && (!terrainFinite(l.MaxScale) || l.MaxScale <= 0) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxScale"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid maxScale %v", l.Name, l.MaxScale),
				Expected: "a finite positive number",
				Fix:      "set maxScale to a positive number or omit it to use 1",
			}
		}
		if l.MinScale > 0 && l.MaxScale > 0 && l.MinScale > l.MaxScale {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxScale"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q maxScale (%v) < minScale (%v)", l.Name, l.MaxScale, l.MinScale),
				Expected: fmt.Sprintf("number >= %v", l.MinScale),
				Fix:      "set maxScale >= minScale",
			}
		}
		if !terrainFinite(l.AlignToNormal) || l.AlignToNormal < 0 || l.AlignToNormal > 1 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "alignToNormal"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q alignToNormal %v out of range", l.Name, l.AlignToNormal),
				Expected: "a finite number in [0, 1]",
				Fix:      "set alignToNormal between 0 (upright) and 1 (terrain normal)",
			}
		}
		if !terrainFinite(l.MinSlopeDeg) || l.MinSlopeDeg < 0 || l.MinSlopeDeg > 90 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "minSlopeDeg"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q minSlopeDeg %v out of range", l.Name, l.MinSlopeDeg),
				Expected: "a finite number in [0, 90]",
				Fix:      "set minSlopeDeg between 0 and 90 degrees",
			}
		}
		if !terrainFinite(l.MaxSlopeDeg) || l.MaxSlopeDeg < 0 || l.MaxSlopeDeg > 90 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxSlopeDeg"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q maxSlopeDeg %v out of range", l.Name, l.MaxSlopeDeg),
				Expected: "a finite number in [0, 90]",
				Fix:      "set maxSlopeDeg between 0 and 90 degrees",
			}
		}
		if l.MinSlopeDeg > 0 && l.MaxSlopeDeg > 0 && l.MinSlopeDeg > l.MaxSlopeDeg {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxSlopeDeg"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q maxSlopeDeg (%v) < minSlopeDeg (%v)", l.Name, l.MaxSlopeDeg, l.MinSlopeDeg),
				Expected: fmt.Sprintf("number >= %v", l.MinSlopeDeg),
				Fix:      "set maxSlopeDeg >= minSlopeDeg",
			}
		}
		if l.MinHeight != nil && !terrainFinite(*l.MinHeight) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "minHeight"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q non-finite minHeight %v", l.Name, *l.MinHeight),
				Expected: "a finite number",
				Fix:      "set minHeight to a finite world Y elevation",
			}
		}
		if l.MaxHeight != nil && !terrainFinite(*l.MaxHeight) {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxHeight"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q non-finite maxHeight %v", l.Name, *l.MaxHeight),
				Expected: "a finite number",
				Fix:      "set maxHeight to a finite world Y elevation",
			}
		}
		if l.MinHeight != nil && l.MaxHeight != nil && *l.MinHeight > *l.MaxHeight {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "maxHeight"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q maxHeight (%v) < minHeight (%v)", l.Name, *l.MaxHeight, *l.MinHeight),
				Expected: fmt.Sprintf("number >= %v", *l.MinHeight),
				Fix:      "set maxHeight >= minHeight",
			}
		}
		if !terrainFinite(l.BaseMaxDistance) || l.BaseMaxDistance < 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "baseMaxDistance"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid baseMaxDistance %v", l.Name, l.BaseMaxDistance),
				Expected: "a finite non-negative number",
				Fix:      "set baseMaxDistance >= 0",
			}
		}
		if !terrainFinite(l.BoundingRadius) || l.BoundingRadius < 0 {
			return &loaderr.Error{
				Field:    loaderr.Path("layers", i, "boundingRadius"),
				Problem:  fmt.Sprintf("world3d: foliage layer %q invalid boundingRadius %v", l.Name, l.BoundingRadius),
				Expected: "a finite non-negative number",
				Fix:      "set boundingRadius >= 0",
			}
		}
	}
	return nil
}

// EncodeFoliage validates doc and serializes it as deterministic 2-space YAML
// prefixed with [FoliageSchemaComment].
func EncodeFoliage(doc *FoliageDoc) ([]byte, error) {
	if err := ValidateFoliage(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(FoliageSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("world3d: encode foliage: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("world3d: encode foliage: %w", err)
	}
	return buf.Bytes(), nil
}

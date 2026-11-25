package render3d

import (
	"fmt"
	"image"
	_ "image/gif"  // decode GIF terrain textures
	_ "image/jpeg" // decode JPEG terrain textures
	_ "image/png"  // decode PNG terrain textures
	"io/fs"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// MeshFromTerrainMesh converts a CPU [world3d.TerrainMesh] into a [Mesh] ready
// for [UploadMesh] and rendering with [GPUTerrainMaterial].
func MeshFromTerrainMesh(tm world3d.TerrainMesh) Mesh {
	return Mesh{
		Positions: tm.Positions,
		Normals:   tm.Normals,
		UVs:       tm.UVs,
		Indices:   tm.Indices,
	}
}

// TerrainMaterialFromDoc loads a [TerrainMaterial] from doc and fsys, decoding
// either packed RGBA splat maps (doc.SplatMaps) or per-layer grayscale weight
// maps (layers[i].WeightMap packed via [world3d.PackLayerWeightMaps]), optional
// material resource files (layers[i].Material), and per-layer PBR textures.
//
// The returned material is validated and ready for [NewGPUTerrainMaterial] or
// [EvaluateTerrainMaterial], blending layers identically to [world3d.Heightmap.LayerAt].
func TerrainMaterialFromDoc(fsys fs.FS, doc *world3d.TerrainDoc) (TerrainMaterial, error) {
	if err := world3d.ValidateTerrain(doc); err != nil {
		return TerrainMaterial{}, err
	}
	if fsys == nil {
		return TerrainMaterial{}, fmt.Errorf("%w: nil filesystem", ErrInvalidTerrainMaterial)
	}

	mat := TerrainMaterial{
		Model:               math3d.Mat4Identity(),
		HeightBlendContrast: doc.HeightBlendContrast,
		Layers:              make([]TerrainLayer, len(doc.Layers)),
	}
	if doc.Triplanar != nil {
		mat.Triplanar = TerrainTriplanar{
			Enabled:        doc.Triplanar.Enabled,
			SlopeThreshold: doc.Triplanar.SlopeThreshold,
			SlopeBlend:     doc.Triplanar.SlopeBlend,
			Sharpness:      doc.Triplanar.Sharpness,
			Scale:          doc.Triplanar.Scale,
		}
	}

	switch {
	case len(doc.SplatMaps) > 0:
		s0, err := decodeTerrainImageFS(fsys, doc.SplatMaps[0])
		if err != nil {
			return TerrainMaterial{}, fmt.Errorf("%w: splatMaps[0] %q: %v", ErrInvalidTerrainMaterial, doc.SplatMaps[0], err)
		}
		mat.SplatMap0 = s0
		if len(doc.SplatMaps) > 1 {
			s1, err := decodeTerrainImageFS(fsys, doc.SplatMaps[1])
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: splatMaps[1] %q: %v", ErrInvalidTerrainMaterial, doc.SplatMaps[1], err)
			}
			mat.SplatMap1 = s1
		}
	case doc.HasLayerWeightMaps():
		weights := make([]image.Image, len(doc.Layers))
		for i, l := range doc.Layers {
			if l.WeightMap == "" {
				continue
			}
			img, err := decodeTerrainImageFS(fsys, l.WeightMap)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].weightMap %q: %v", ErrInvalidTerrainMaterial, i, l.WeightMap, err)
			}
			weights[i] = img
		}
		s0, s1, err := world3d.PackLayerWeightMaps(weights...)
		if err != nil {
			return TerrainMaterial{}, fmt.Errorf("%w: pack weight maps: %v", ErrInvalidTerrainMaterial, err)
		}
		if s0 != nil {
			mat.SplatMap0 = s0
		}
		if s1 != nil {
			mat.SplatMap1 = s1
		}
	}

	for i, ld := range doc.Layers {
		layer := DefaultTerrainLayer(ld.Name)
		albedoPath := ld.Albedo
		roughnessSet := ld.Roughness != nil
		metallicSet := ld.Metallic != 0
		colorSet := ld.Color != nil

		if ld.Material != "" {
			raw, err := fs.ReadFile(fsys, ld.Material)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].material %q: %v", ErrInvalidTerrainMaterial, i, ld.Material, err)
			}
			rm, err := resource.ParseAs[*resource.Material](raw)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].material %q: %v", ErrInvalidTerrainMaterial, i, ld.Material, err)
			}
			if !colorSet && rm.BaseColor != "" {
				c := rm.Color()
				layer.BaseColorFactor = [3]float32{
					srgbToLinearChannel(float32(uint8(c>>24)) / 255.0),
					srgbToLinearChannel(float32(uint8(c>>16)) / 255.0),
					srgbToLinearChannel(float32(uint8(c>>8)) / 255.0),
				}
			}
			if albedoPath == "" && rm.Texture != "" {
				albedoPath = rm.Texture
			}
			if !roughnessSet && rm.Roughness != nil {
				layer.RoughnessFactor = *rm.Roughness
			}
			if !metallicSet && rm.Metallic != 0 {
				layer.MetallicFactor = rm.Metallic
			}
		}

		if colorSet {
			layer.BaseColorFactor = *ld.Color
		}
		if roughnessSet {
			layer.RoughnessFactor = *ld.Roughness
		} else if ld.Material == "" || ld.MetallicRoughness != "" {
			layer.RoughnessFactor = ld.EffectiveRoughness()
		}
		if metallicSet {
			layer.MetallicFactor = ld.Metallic
		}
		layer.NormalScale = ld.EffectiveNormalScale()
		layer.UVScale = ld.EffectiveUVScale()
		layer.HeightBias = ld.HeightBias

		if albedoPath != "" {
			img, err := decodeTerrainImageFS(fsys, albedoPath)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].albedo %q: %v", ErrInvalidTerrainMaterial, i, albedoPath, err)
			}
			layer.AlbedoImage = img
		}
		if ld.Normal != "" {
			img, err := decodeTerrainImageFS(fsys, ld.Normal)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].normal %q: %v", ErrInvalidTerrainMaterial, i, ld.Normal, err)
			}
			layer.NormalImage = img
		}
		if ld.MetallicRoughness != "" {
			img, err := decodeTerrainImageFS(fsys, ld.MetallicRoughness)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].metallicRoughness %q: %v", ErrInvalidTerrainMaterial, i, ld.MetallicRoughness, err)
			}
			layer.MetallicRoughnessImage = img
		}
		if ld.Height != "" {
			img, err := decodeTerrainImageFS(fsys, ld.Height)
			if err != nil {
				return TerrainMaterial{}, fmt.Errorf("%w: layers[%d].height %q: %v", ErrInvalidTerrainMaterial, i, ld.Height, err)
			}
			layer.HeightImage = img
		}
		mat.Layers[i] = layer
	}

	if err := mat.Validate(); err != nil {
		return TerrainMaterial{}, err
	}
	return mat, nil
}

func decodeTerrainImageFS(fsys fs.FS, p string) (image.Image, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func srgbToLinearChannel(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow((float64(c)+0.055)/1.055, 2.4))
}

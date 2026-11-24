package gltf

import (
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

const (
	extMaterialsEmissiveStrength = "KHR_materials_emissive_strength"
	extMaterialsClearcoat        = "KHR_materials_clearcoat"
	extMaterialsTransmission     = "KHR_materials_transmission"
)

// Clearcoat represents the Khronos KHR_materials_clearcoat material extension,
// describing a dielectric coating layer (IOR 1.5, F0 = 0.04) on top of the
// base PBR metallic-roughness material.
type Clearcoat struct {
	// Factor is clearcoatFactor in [0, 1] (default 0). Multiplied by Texture's R channel when present.
	Factor float32
	// Texture is clearcoatTexture (optional, linear; R channel modulates Factor).
	Texture *TextureRef
	// RoughnessFactor is clearcoatRoughnessFactor in [0, 1] (default 0). Multiplied by RoughnessTexture's G channel when present.
	RoughnessFactor float32
	// RoughnessTexture is clearcoatRoughnessTexture (optional, linear; G channel modulates RoughnessFactor).
	RoughnessTexture *TextureRef
	// NormalTexture is clearcoatNormalTexture (optional, tangent-space normal map; Scale defaults to 1).
	NormalTexture *TextureRef
}

// Transmission represents the Khronos KHR_materials_transmission material
// extension, describing optical transmission through thin dielectric surfaces.
type Transmission struct {
	// Factor is transmissionFactor in [0, 1] (default 0). Multiplied by Texture's R channel when present.
	Factor float32
	// Texture is transmissionTexture (optional, linear; R channel modulates Factor).
	Texture *TextureRef
}

// EffectiveEmissive returns EmissiveFactor scaled by EmissiveStrength (the HDR
// linear emissive radiance factor before EmissiveTexture modulation).
func (m Material) EffectiveEmissive() math3d.Vec3 {
	return m.EmissiveFactor.Scale(m.EmissiveStrength)
}

type rawEmissiveStrengthExt struct {
	EmissiveStrength json.RawMessage `json:"emissiveStrength"`
}

type rawClearcoatExt struct {
	ClearcoatFactor           json.RawMessage `json:"clearcoatFactor"`
	ClearcoatTexture          json.RawMessage `json:"clearcoatTexture"`
	ClearcoatRoughnessFactor  json.RawMessage `json:"clearcoatRoughnessFactor"`
	ClearcoatRoughnessTexture json.RawMessage `json:"clearcoatRoughnessTexture"`
	ClearcoatNormalTexture    json.RawMessage `json:"clearcoatNormalTexture"`
}

type rawTransmissionExt struct {
	TransmissionFactor  json.RawMessage `json:"transmissionFactor"`
	TransmissionTexture json.RawMessage `json:"transmissionTexture"`
}

func (d *Document) parseMaterialExtensions(mat *Material, matIdx int, exts map[string]json.RawMessage) error {
	if len(exts) == 0 {
		return nil
	}
	if raw, ok := exts[extMaterialsEmissiveStrength]; ok {
		strength, err := parseEmissiveStrengthExt(raw)
		if err != nil {
			return fmt.Errorf("gltf: material %d %w", matIdx, err)
		}
		mat.EmissiveStrength = strength
	}
	if raw, ok := exts[extMaterialsClearcoat]; ok {
		cc, err := d.parseClearcoatExt(raw)
		if err != nil {
			return fmt.Errorf("gltf: material %d %w", matIdx, err)
		}
		mat.Clearcoat = cc
	}
	if raw, ok := exts[extMaterialsTransmission]; ok {
		tr, err := d.parseTransmissionExt(raw)
		if err != nil {
			return fmt.Errorf("gltf: material %d %w", matIdx, err)
		}
		mat.Transmission = tr
	}
	return nil
}

func parseExtScalar(extName, field string, raw json.RawMessage) (float32, error) {
	if isJSONNull(raw) {
		return 0, fmt.Errorf("%s %s must be a finite number", extName, field)
	}
	var v float32
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("parse %s %s: %w", extName, field, err)
	}
	if !finite32(v) {
		return 0, fmt.Errorf("%s %s must be finite", extName, field)
	}
	return v, nil
}

func (d *Document) parseExtTextureRef(extName, field string, raw json.RawMessage, normal bool) (*TextureRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !isJSONObject(raw) {
		return nil, fmt.Errorf("%s %s must be a JSON object", extName, field)
	}
	var info rawTextureInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("parse %s %s: %w", extName, field, err)
	}
	if normal && info.Scale != nil && !finite32(*info.Scale) {
		return nil, fmt.Errorf("%s %s scale must be finite", extName, field)
	}
	ref, err := d.parseTextureRef(&info, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", extName, field, err)
	}
	return ref, nil
}

func parseEmissiveStrengthExt(extData json.RawMessage) (float32, error) {
	if !isJSONObject(extData) {
		return 0, fmt.Errorf("%s must be a JSON object", extMaterialsEmissiveStrength)
	}
	var raw rawEmissiveStrengthExt
	if err := json.Unmarshal(extData, &raw); err != nil {
		return 0, fmt.Errorf("parse %s: %w", extMaterialsEmissiveStrength, err)
	}
	if len(raw.EmissiveStrength) == 0 {
		return 1, nil
	}
	v, err := parseExtScalar(extMaterialsEmissiveStrength, "emissiveStrength", raw.EmissiveStrength)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("%s emissiveStrength %g must be >= 0", extMaterialsEmissiveStrength, v)
	}
	return v, nil
}

func (d *Document) parseClearcoatExt(extData json.RawMessage) (*Clearcoat, error) {
	if !isJSONObject(extData) {
		return nil, fmt.Errorf("%s must be a JSON object", extMaterialsClearcoat)
	}
	var raw rawClearcoatExt
	if err := json.Unmarshal(extData, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", extMaterialsClearcoat, err)
	}
	var cc Clearcoat
	if len(raw.ClearcoatFactor) > 0 {
		v, err := parseExtScalar(extMaterialsClearcoat, "clearcoatFactor", raw.ClearcoatFactor)
		if err != nil {
			return nil, err
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("%s clearcoatFactor %g must be in [0, 1]", extMaterialsClearcoat, v)
		}
		cc.Factor = v
	}
	if len(raw.ClearcoatRoughnessFactor) > 0 {
		v, err := parseExtScalar(extMaterialsClearcoat, "clearcoatRoughnessFactor", raw.ClearcoatRoughnessFactor)
		if err != nil {
			return nil, err
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("%s clearcoatRoughnessFactor %g must be in [0, 1]", extMaterialsClearcoat, v)
		}
		cc.RoughnessFactor = v
	}
	var err error
	if cc.Texture, err = d.parseExtTextureRef(extMaterialsClearcoat, "clearcoatTexture", raw.ClearcoatTexture, false); err != nil {
		return nil, err
	}
	if cc.RoughnessTexture, err = d.parseExtTextureRef(extMaterialsClearcoat, "clearcoatRoughnessTexture", raw.ClearcoatRoughnessTexture, false); err != nil {
		return nil, err
	}
	if cc.NormalTexture, err = d.parseExtTextureRef(extMaterialsClearcoat, "clearcoatNormalTexture", raw.ClearcoatNormalTexture, true); err != nil {
		return nil, err
	}
	return &cc, nil
}

func (d *Document) parseTransmissionExt(extData json.RawMessage) (*Transmission, error) {
	if !isJSONObject(extData) {
		return nil, fmt.Errorf("%s must be a JSON object", extMaterialsTransmission)
	}
	var raw rawTransmissionExt
	if err := json.Unmarshal(extData, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", extMaterialsTransmission, err)
	}
	var tr Transmission
	if len(raw.TransmissionFactor) > 0 {
		v, err := parseExtScalar(extMaterialsTransmission, "transmissionFactor", raw.TransmissionFactor)
		if err != nil {
			return nil, err
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("%s transmissionFactor %g must be in [0, 1]", extMaterialsTransmission, v)
		}
		tr.Factor = v
	}
	var err error
	if tr.Texture, err = d.parseExtTextureRef(extMaterialsTransmission, "transmissionTexture", raw.TransmissionTexture, false); err != nil {
		return nil, err
	}
	return &tr, nil
}

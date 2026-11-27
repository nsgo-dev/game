package render3d

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"go.yaml.in/yaml/v3"
)

const (
	// LightProbeGridVersion is the baked light probe grid YAML schema version this package reads and writes.
	LightProbeGridVersion = 1

	// LightProbeGridSchemaURL is where the JSON Schema of version 1 baked light probe grid YAML files is published.
	LightProbeGridSchemaURL = "https://game.nsgo.dev/schemas/lightprobes.v1.json"

	// LightProbeGridSchemaComment is the first line [EncodeLightProbeGrid] writes so YAML editors and AI tools validate and complete baked light probe files.
	LightProbeGridSchemaComment = "# yaml-language-server: $schema=" + LightProbeGridSchemaURL

	// MaxLightProbeGridProbes caps the total probe count (nx * ny * nz) accepted when decoding a baked [LightProbeGridDoc].
	MaxLightProbeGridProbes = 65536
)

// EncodeCubemapHDR writes cm's base mip level (six canonical faces +X, -X, +Y, -Y, +Z, -Z) as a
// deterministic Radiance RGBE (.hdr) horizontal 6x1 cubemap strip of width 6*cm.Size and height cm.Size.
// [DecodeCubemapHDR] reads the strip back without resampling loss while preserving HDR radiance above 1.0.
func EncodeCubemapHDR(w io.Writer, cm *HDRCubemap) error {
	if w == nil {
		return fmt.Errorf("%w: nil HDR cubemap writer", ErrInvalidCubemap)
	}
	if err := cm.Validate(); err != nil {
		return err
	}
	size := cm.Size
	strip, err := NewHDRImage(size*ProbeCubeFaceCount, size)
	if err != nil {
		return err
	}
	for face := range ProbeCubeFaceCount {
		src := cm.Faces[face]
		xOff := face * size
		for y := range size {
			dstRow := y*strip.Width + xOff
			srcRow := y * size
			copy(strip.RGB[dstRow:dstRow+size], src[srcRow:srcRow+size])
		}
	}
	return EncodeHDR(w, strip)
}

// CubemapFromHDRImage converts a decoded Radiance [*HDRImage] into a 6-face linear [*HDRCubemap].
// When img is a 6x1 horizontal cubemap strip (img.Width == 6*img.Height, as written by [EncodeCubemapHDR]),
// its six faces (+X, -X, +Y, -Y, +Z, -Z) are extracted directly with zero resampling. Otherwise img is
// treated as an equirectangular panorama and projected via [CubemapFromHDREquirectangular].
func CubemapFromHDRImage(img *HDRImage) (*HDRCubemap, error) {
	if err := img.Validate(); err != nil {
		return nil, err
	}
	if img.Width == img.Height*ProbeCubeFaceCount {
		size := img.Height
		cm, err := NewHDRCubemap(size)
		if err != nil {
			return nil, err
		}
		for face := range ProbeCubeFaceCount {
			dst := cm.Faces[face]
			xOff := face * size
			for y := range size {
				srcRow := y*img.Width + xOff
				dstRow := y * size
				copy(dst[dstRow:dstRow+size], img.RGB[srcRow:srcRow+size])
			}
		}
		return cm, nil
	}
	faceSize := min(max(img.Height/2, 1), 256)
	return CubemapFromHDREquirectangular(img, faceSize)
}

// DecodeCubemapHDR decodes a Radiance .hdr (RGBE) cubemap strip (as written by [EncodeCubemapHDR])
// or equirectangular panorama from r into a 6-face linear float32 [*HDRCubemap].
func DecodeCubemapHDR(r io.Reader) (*HDRCubemap, error) {
	img, err := DecodeHDR(r)
	if err != nil {
		return nil, err
	}
	return CubemapFromHDRImage(img)
}

// HalfToFloat32 converts an IEEE 754 binary16 half-float bit pattern into a float32.
func HalfToFloat32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32((h >> 10) & 0x1F)
	mant := uint32(h & 0x03FF)
	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		// Subnormal binary16 -> normalize to binary32.
		e := int32(-14)
		for (mant & 0x0400) == 0 {
			mant <<= 1
			e--
		}
		mant &= 0x03FF
		bits := sign | uint32(e+127)<<23 | (mant << 13)
		return math.Float32frombits(bits)
	case 31:
		bits := sign | 0x7F800000 | (mant << 13)
		return math.Float32frombits(bits)
	default:
		bits := sign | ((exp + (127 - 15)) << 23) | (mant << 13)
		return math.Float32frombits(bits)
	}
}

// ProbeVec3 is a 3-component float32 vector serialized in flow style as [x, y, z] in baked light probe YAML files.
type ProbeVec3 [3]float32

// MarshalYAML encodes v in compact single-line flow style [x, y, z], normalizing -0 to 0.
func (v ProbeVec3) MarshalYAML() (any, error) {
	return probeFlowNode([]float32{cleanZero32(v[0]), cleanZero32(v[1]), cleanZero32(v[2])})
}

// ProbeCounts is the [nx, ny, nz] probe lattice count serialized in flow style in baked light probe YAML files.
type ProbeCounts [3]int

// MarshalYAML encodes c in compact single-line flow style [nx, ny, nz].
func (c ProbeCounts) MarshalYAML() (any, error) {
	return probeFlowNode([]int{c[0], c[1], c[2]})
}

// LightProbeSHDoc stores the 9 RGB L2 spherical harmonic irradiance coefficients of one baked light probe.
type LightProbeSHDoc struct {
	// SH holds the 9 cosine-convolved L2 spherical harmonic RGB coefficients matching [IrradianceSH.Coeff].
	SH [9]ProbeVec3 `yaml:"sh" schema:"required"`
}

// LightProbeGridDoc is the versioned YAML document storing a baked [LightProbeGrid]: its world-space
// axis-aligned bounds, [nx, ny, nz] lattice counts, and nx*ny*nz per-probe L2 spherical harmonic
// irradiance entries in (iz*ny + iy)*nx + ix order.
type LightProbeGridDoc struct {
	// Version is the schema version; it must be [LightProbeGridVersion] (1).
	Version int `yaml:"version" schema:"required"`
	// BoundsMin is the minimum [x, y, z] corner of the grid's world-space AABB.
	BoundsMin ProbeVec3 `yaml:"boundsMin" schema:"required"`
	// BoundsMax is the maximum [x, y, z] corner of the grid's world-space AABB.
	BoundsMax ProbeVec3 `yaml:"boundsMax" schema:"required"`
	// Counts holds the [nx, ny, nz] probe counts along the X, Y, and Z axes (each >= 1).
	Counts ProbeCounts `yaml:"counts" schema:"required"`
	// Probes holds the nx*ny*nz baked irradiance probes in (iz*ny + iy)*nx + ix order.
	Probes []LightProbeSHDoc `yaml:"probes" schema:"required"`
}

func cleanZero32(v float32) float32 {
	if v == 0 {
		return 0
	}
	return v
}

func probeFlowNode(v any) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	n.Style = yaml.FlowStyle
	return &n, nil
}

// LightProbeGridToDoc converts a validated [*LightProbeGrid] into a versioned [*LightProbeGridDoc]
// ready to serialize with [EncodeLightProbeGrid].
func LightProbeGridToDoc(g *LightProbeGrid) (*LightProbeGridDoc, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	doc := &LightProbeGridDoc{
		Version:   LightProbeGridVersion,
		BoundsMin: ProbeVec3{cleanZero32(g.Bounds.Min.X), cleanZero32(g.Bounds.Min.Y), cleanZero32(g.Bounds.Min.Z)},
		BoundsMax: ProbeVec3{cleanZero32(g.Bounds.Max.X), cleanZero32(g.Bounds.Max.Y), cleanZero32(g.Bounds.Max.Z)},
		Counts:    ProbeCounts{g.Counts[0], g.Counts[1], g.Counts[2]},
		Probes:    make([]LightProbeSHDoc, len(g.Probes)),
	}
	for i, sh := range g.Probes {
		var entry LightProbeSHDoc
		for k := range 9 {
			entry.SH[k] = ProbeVec3{
				cleanZero32(sh.Coeff[k][0]),
				cleanZero32(sh.Coeff[k][1]),
				cleanZero32(sh.Coeff[k][2]),
			}
		}
		doc.Probes[i] = entry
	}
	if err := ValidateLightProbeGridDoc(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ToGrid converts a validated [LightProbeGridDoc] into a runtime [*LightProbeGrid].
func (d *LightProbeGridDoc) ToGrid() (*LightProbeGrid, error) {
	if err := ValidateLightProbeGridDoc(d); err != nil {
		return nil, err
	}
	bounds := math3d.AABB{
		Min: math3d.Vec3{X: d.BoundsMin[0], Y: d.BoundsMin[1], Z: d.BoundsMin[2]},
		Max: math3d.Vec3{X: d.BoundsMax[0], Y: d.BoundsMax[1], Z: d.BoundsMax[2]},
	}
	g, err := NewLightProbeGrid(bounds, d.Counts[0], d.Counts[1], d.Counts[2])
	if err != nil {
		return nil, err
	}
	for i, entry := range d.Probes {
		var sh IrradianceSH
		for k := range 9 {
			sh.Coeff[k] = [3]float32{entry.SH[k][0], entry.SH[k][1], entry.SH[k][2]}
		}
		g.Probes[i] = sh
	}
	return g, nil
}

// ValidateLightProbeGridDoc validates d against the version 1 baked light probe grid schema.
// Every error wraps [ErrInvalidProbe].
func ValidateLightProbeGridDoc(d *LightProbeGridDoc) error {
	if d == nil {
		return loaderr.Errorf("", "pass a non-nil *render3d.LightProbeGridDoc to ValidateLightProbeGridDoc", "%w: nil light probe grid document", ErrInvalidProbe)
	}
	if d.Version != LightProbeGridVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the light probe grid file", LightProbeGridVersion), "%w: unsupported light probe grid version %d (want %d)", ErrInvalidProbe, d.Version, LightProbeGridVersion)
	}
	for axis := range 3 {
		if !finite(d.BoundsMin[axis]) {
			return loaderr.Errorf(fmt.Sprintf("boundsMin[%d]", axis), "set boundsMin coordinates to finite numbers", "%w: light probe grid bounds must be finite", ErrInvalidProbe)
		}
		if !finite(d.BoundsMax[axis]) {
			return loaderr.Errorf(fmt.Sprintf("boundsMax[%d]", axis), "set boundsMax coordinates to finite numbers", "%w: light probe grid bounds must be finite", ErrInvalidProbe)
		}
		if d.BoundsMin[axis] > d.BoundsMax[axis] {
			return loaderr.Errorf(fmt.Sprintf("boundsMin[%d]", axis), "set each boundsMin coordinate less than or equal to the matching boundsMax coordinate", "%w: light probe grid boundsMin[%d] (%v) exceeds boundsMax[%d] (%v)", ErrInvalidProbe, axis, d.BoundsMin[axis], axis, d.BoundsMax[axis])
		}
	}
	total, ok := probeGridCellCount(d.Counts[0], d.Counts[1], d.Counts[2])
	if !ok {
		return loaderr.Errorf("counts", "set counts to three positive integers [nx, ny, nz]", "%w: light probe grid counts %v must be positive", ErrInvalidProbe, d.Counts)
	}
	if total > MaxLightProbeGridProbes {
		return loaderr.Errorf("counts", fmt.Sprintf("reduce counts so nx*ny*nz is at most %d", MaxLightProbeGridProbes), "%w: light probe grid count %d exceeds maximum %d", ErrInvalidProbe, total, MaxLightProbeGridProbes)
	}
	if len(d.Probes) != total {
		return loaderr.Errorf("probes", fmt.Sprintf("provide exactly %d probe entries to match counts %v", total, d.Counts), "%w: light probe grid has %d probes, want %d for counts %v", ErrInvalidProbe, len(d.Probes), total, d.Counts)
	}
	for i, p := range d.Probes {
		for k := range 9 {
			for ch := range 3 {
				if !finite(p.SH[k][ch]) {
					return loaderr.Errorf(fmt.Sprintf("probes[%d].sh[%d][%d]", i, k, ch), "replace NaN or Inf SH coefficients with finite numbers (or rebake the probe grid)", "%w: probe %d SH coefficient %d channel %d is not finite", ErrInvalidProbe, i, k, ch)
				}
			}
		}
	}
	return nil
}

// ParseLightProbeGrid strictly decodes and validates a versioned baked light probe grid YAML file,
// refusing unknown fields, trailing documents, invalid versions, and non-finite coefficients.
func ParseLightProbeGrid(data []byte) (*LightProbeGridDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a light probe grid YAML document starting with version: %d", LightProbeGridVersion), "%w: empty light probe grid document", ErrInvalidProbe), "", data, "")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc LightProbeGridDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("%w: decode light probe grid: %w", ErrInvalidProbe, err), "", data, "check the YAML syntax and field names in the light probe grid file")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the light probe grid file and remove any --- separator", "%w: unexpected trailing YAML document in light probe grid", ErrInvalidProbe), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("%w: decode light probe grid: %w", ErrInvalidProbe, err), "", data, "remove trailing YAML content after the light probe grid")
	}
	if err := ValidateLightProbeGridDoc(&doc); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	yamlzero.Normalize(&doc)
	return &doc, nil
}

// EncodeLightProbeGrid validates d and serializes it as deterministic 2-space YAML starting with
// [LightProbeGridSchemaComment]. A round trip through [ParseLightProbeGrid] and EncodeLightProbeGrid
// produces identical bytes.
func EncodeLightProbeGrid(d *LightProbeGridDoc) ([]byte, error) {
	if err := ValidateLightProbeGridDoc(d); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(LightProbeGridSchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("%w: encode light probe grid: %w", ErrInvalidProbe, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("%w: encode light probe grid: %w", ErrInvalidProbe, err)
	}
	return buf.Bytes(), nil
}

// LoadLightProbeGridFS reads, parses, and converts a baked light probe grid YAML file from fsys into
// a runtime [*LightProbeGrid].
func LoadLightProbeGridFS(fsys fs.FS, name string) (*LightProbeGrid, error) {
	if fsys == nil {
		return nil, loaderr.Locate(errors.New("render3d: nil file system"), name, nil, "pass a non-nil fs.FS containing the light probe grid file")
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("render3d: read light probe grid %q: %w", name, err), name, nil, "check that the light probe grid file exists and is readable")
	}
	doc, err := ParseLightProbeGrid(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
	}
	return doc.ToGrid()
}

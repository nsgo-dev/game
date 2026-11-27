package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	pathpkg "path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
)

// BufferResolver resolves relative external URIs (for .bin buffers and images)
// to their raw bytes.
type BufferResolver func(uri string) ([]byte, error)

// ComponentType identifies the binary storage type of an accessor component.
type ComponentType int

const (
	ComponentByte          ComponentType = 5120 // int8
	ComponentUnsignedByte  ComponentType = 5121 // uint8
	ComponentShort         ComponentType = 5122 // int16
	ComponentUnsignedShort ComponentType = 5123 // uint16
	ComponentUnsignedInt   ComponentType = 5125 // uint32
	ComponentFloat         ComponentType = 5126 // float32
)

// ByteSize returns the size in bytes of a single component, or 0 if unknown.
func (c ComponentType) ByteSize() int {
	switch c {
	case ComponentByte, ComponentUnsignedByte:
		return 1
	case ComponentShort, ComponentUnsignedShort:
		return 2
	case ComponentUnsignedInt, ComponentFloat:
		return 4
	default:
		return 0
	}
}

// AccessorType identifies the element structure of an accessor.
type AccessorType string

const (
	AccessorScalar AccessorType = "SCALAR"
	AccessorVec2   AccessorType = "VEC2"
	AccessorVec3   AccessorType = "VEC3"
	AccessorVec4   AccessorType = "VEC4"
	AccessorMat2   AccessorType = "MAT2"
	AccessorMat3   AccessorType = "MAT3"
	AccessorMat4   AccessorType = "MAT4"
)

// Components returns the number of scalar components in an element of type t,
// or 0 if t is unknown.
func (t AccessorType) Components() int {
	switch t {
	case AccessorScalar:
		return 1
	case AccessorVec2:
		return 2
	case AccessorVec3:
		return 3
	case AccessorVec4, AccessorMat2:
		return 4
	case AccessorMat3:
		return 9
	case AccessorMat4:
		return 16
	default:
		return 0
	}
}

// Asset holds metadata about the glTF asset.
type Asset struct {
	Version    string
	MinVersion string
	Generator  string
	Copyright  string
	Extras     json.RawMessage
}

// Buffer holds resolved binary buffer bytes. For a buffer that meshopt-compressed bufferViews decode into (an EXT_meshopt_compression or KHR_meshopt_compression fallback or placeholder buffer), Data holds the decompressed views and the buffer's own URI is never fetched.
type Buffer struct {
	Name       string
	URI        string
	ByteLength int
	Data       []byte
	Extras     json.RawMessage
}

// BufferView defines a slice of a Buffer with an optional interleaved ByteStride.
type BufferView struct {
	Name       string
	Buffer     int
	ByteOffset int
	ByteLength int
	ByteStride int
	Target     int
	Extras     json.RawMessage
	// Extensions keeps unsupported optional extension payloads by name.
	Extensions map[string]json.RawMessage
}

// AccessorSparseIndices describes the index buffer of a sparse accessor.
type AccessorSparseIndices struct {
	BufferView    int
	ByteOffset    int
	ComponentType ComponentType
}

// AccessorSparseValues describes the replacement values buffer of a sparse accessor.
type AccessorSparseValues struct {
	BufferView int
	ByteOffset int
}

// AccessorSparse describes sparse overrides applied on top of a base accessor.
type AccessorSparse struct {
	Count   int
	Indices AccessorSparseIndices
	Values  AccessorSparseValues
}

// Accessor defines typed view metadata over a BufferView (or zero-initialized storage).
// BufferView is -1 when omitted (zero-initialized per glTF 2.0 spec).
type Accessor struct {
	Name          string
	BufferView    int
	ByteOffset    int
	ComponentType ComponentType
	Normalized    bool
	Count         int
	Type          AccessorType
	Min           []float32
	Max           []float32
	Sparse        *AccessorSparse
	Extras        json.RawMessage

	// decoded holds tightly packed element data that replaces the bufferView, filled from a KHR_draco_mesh_compression primitive.
	decoded []byte
}

// Document is a parsed glTF 2.0 / GLB asset with resolved buffers, decoded
// meshes, materials, scene hierarchy, skins, and animations.
type Document struct {
	Asset              Asset
	ExtensionsUsed     []string
	ExtensionsRequired []string
	Scene              int // default scene index, or -1 if unspecified
	Scenes             []Scene
	Nodes              []Node
	Cameras            []Camera
	Buffers            []Buffer
	BufferViews        []BufferView
	Accessors          []Accessor
	Samplers           []Sampler
	Images             []Image
	Textures           []Texture
	Materials          []Material
	Variants           []MaterialVariant
	Meshes             []Mesh
	Skins              []Skin
	Animations         []Animation
	Extras             json.RawMessage

	resolver BufferResolver
}

const (
	extTextureTransform  = "KHR_texture_transform"
	extMaterialsVariants = "KHR_materials_variants"

	// Safety cap for zero-initialized (bufferView-less) accessors to prevent
	// adversarial inputs from triggering huge allocations.
	maxZeroInitAccessorBytes uint64 = 16 << 20 // 16 MiB
	// Safety cap for cumulative decoded element bytes during a single Load.
	maxLoadDecodedBytes uint64 = 128 << 20 // 128 MiB
)

// Load parses a glTF 2.0 JSON (.gltf) or binary GLB (.glb) document from data.
// External buffer/image URIs are resolved via resolver (which may be nil when
// all resources are embedded via GLB BIN chunks or base64 data URIs).
//
// Its errors are *loaderr.Error values that give the field of the problem, the line in the JSON (in a GLB, the line in its JSON chunk) and how to fix it; a caller that read data from a file names it with loaderr.WithFile, as LoadFS does.
func Load(data []byte, resolver BufferResolver) (*Document, error) {
	doc, jsonData, err := load(data, resolver)
	return doc, loaderr.Locate(err, "", jsonData, fixGLTF)
}

// load parses data and also returns its JSON, which the errors' lines are in.
func load(data []byte, resolver BufferResolver) (*Document, []byte, error) {
	if len(data) == 0 {
		return nil, nil, loaderr.Errorf("", "pass the contents of a .gltf or .glb file", "empty input")
	}

	var jsonData []byte
	var binChunk []byte
	if isGLB(data) {
		var err error
		jsonData, binChunk, err = parseGLB(data)
		if err != nil {
			return nil, nil, err
		}
	} else {
		jsonData = data
		// Strip UTF-8 BOM if present.
		if len(jsonData) >= 3 && jsonData[0] == 0xEF && jsonData[1] == 0xBB && jsonData[2] == 0xBF {
			jsonData = jsonData[3:]
		}
	}

	var raw rawGLTF
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, jsonData, err
	}
	if err := validateVersion(raw.Asset.Version); err != nil {
		return nil, jsonData, loaderr.Errorf("asset.version", `set asset.version to "2.0"`, "%w", err)
	}
	if err := validateMinVersion(raw.Asset.MinVersion, raw.Asset.Version); err != nil {
		return nil, jsonData, loaderr.Errorf("asset.minVersion", "remove asset.minVersion, or set it to a 2.x version no newer than asset.version and this loader supports", "%w", err)
	}

	doc := &Document{
		Asset: Asset{
			Version:    raw.Asset.Version,
			MinVersion: raw.Asset.MinVersion,
			Generator:  raw.Asset.Generator,
			Copyright:  raw.Asset.Copyright,
			Extras:     cloneRawMessage(raw.Asset.Extras),
		},
		ExtensionsUsed:     raw.ExtensionsUsed,
		ExtensionsRequired: raw.ExtensionsRequired,
		Scene:              -1,
		Extras:             cloneRawMessage(raw.Extras),
		resolver:           resolver,
	}

	var decodedBytes uint64
	placeholders, err := doc.loadBuffers(raw.Buffers, binChunk, resolver, meshoptTargets(raw.BufferViews), &decodedBytes)
	if err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadBufferViews(raw.BufferViews); err != nil {
		return nil, jsonData, err
	}
	if err := doc.decodeMeshoptViews(raw.BufferViews, placeholders, &decodedBytes); err != nil {
		return nil, jsonData, itemError(err, "bufferViews", -1)
	}
	if err := doc.loadAccessors(raw.Accessors); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadSamplers(raw.Samplers); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadImages(raw.Images); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadTextures(raw.Textures); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadMaterials(raw.Materials); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadVariants(raw.Extensions); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadCameras(raw.Cameras); err != nil {
		return nil, jsonData, err
	}

	if err := doc.loadMeshes(raw.Meshes, &decodedBytes); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadNodes(raw.Nodes); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadScenes(raw.Scene, raw.Scenes); err != nil {
		return nil, jsonData, err
	}
	if err := doc.loadSkins(raw.Skins, &decodedBytes); err != nil {
		return nil, jsonData, err
	}
	if err := doc.validateSkinnedJoints(); err != nil {
		return nil, jsonData, itemError(err, "nodes", -1)
	}
	if err := doc.loadAnimations(raw.Animations, &decodedBytes); err != nil {
		return nil, jsonData, err
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		return nil, jsonData, loaderr.Errorf("nodes", fixGLTF, "%w", err)
	}
	for i, ext := range doc.ExtensionsRequired {
		switch ext {
		case "KHR_materials_unlit", "KHR_materials_emissive_strength", extMaterialsClearcoat, extMaterialsTransmission,
			extTextureTransform, extMaterialsVariants, "KHR_mesh_quantization",
			extDraco, extMeshoptEXT, extMeshoptKHR, ExtMOZLightmap, ExtLightmap:
		default:
			// Loading without it would silently substitute zeros or defaults for the data the extension supplies (basisu, other compression…).
			return nil, jsonData, loaderr.Errorf(loaderr.Path("extensionsRequired", i), "export the model without this extension, such as without Basis Universal (KHR_texture_basisu) texture compression", "required extension %q is not supported", ext)
		}
	}

	return doc, jsonData, nil
}

// LoadFS loads a .gltf or .glb file at path from fsys, automatically resolving
// relative external .bin buffers and image URIs against path's directory in fsys.
// Its errors are *loaderr.Error values that name path.
func LoadFS(fsys fs.FS, path string) (*Document, error) {
	if fsys == nil {
		return nil, errors.New("gltf: nil fs.FS")
	}
	cleanPath := pathpkg.Clean(path)
	data, err := fs.ReadFile(fsys, cleanPath)
	if err != nil {
		return nil, loaderr.Locate(err, path, nil, "check that the model exists at this path and can be read")
	}
	baseDir := pathpkg.Dir(cleanPath)
	resolver := func(uri string) ([]byte, error) {
		unescaped, err := url.PathUnescape(uri)
		if err != nil {
			return nil, fmt.Errorf("gltf: invalid URI %q: %w", uri, err)
		}
		if strings.HasPrefix(unescaped, "/") || strings.Contains(unescaped, "://") {
			return nil, fmt.Errorf("gltf: external URI %q is not a relative path", uri)
		}
		joined := pathpkg.Clean(pathpkg.Join(baseDir, unescaped))
		if joined == ".." || strings.HasPrefix(joined, "../") {
			return nil, fmt.Errorf("gltf: URI %q escapes filesystem root", uri)
		}
		return fs.ReadFile(fsys, joined)
	}
	doc, err := Load(data, resolver)
	return doc, loaderr.WithFile(err, path)
}

// itemError reports err, from loading the item at index of a document's top-level array section, as a *loaderr.Error about that item, so Load can give its line. index is -1 when the error is about the section rather than one item.
func itemError(err error, section string, index int) error {
	if err == nil {
		return nil
	}
	if _, ok := loaderr.As(err); ok {
		return err
	}
	field := section
	if index >= 0 {
		field = loaderr.Path(section, index)
	}
	return loaderr.Errorf(field, fixGLTF, "%w", err)
}

// fixGLTF is the fix for a glTF error that has no more specific one.
const fixGLTF = "export the model again as glTF 2.0 from Blender or another exporter, or correct the item the error names; see the glTF 2.0 specification"

func validateVersion(v string) error {
	if v == "" {
		return errors.New("gltf: missing asset.version")
	}
	major, _, _ := strings.Cut(v, ".")
	if major != "2" {
		return fmt.Errorf("gltf: unsupported glTF version %q (expected 2.x)", v)
	}
	return nil
}

// supportedMinor is the newest glTF 2.x minor version this loader implements.
const supportedMinor = 0

// validateMinVersion refuses an asset whose asset.minVersion needs a newer glTF than this loader implements, or that exceeds its own asset.version, as the spec requires (#546).
func validateMinVersion(minVersion, version string) error {
	if minVersion == "" {
		return nil
	}
	minMajor, minMinor, ok := parseGLTFVersion(minVersion)
	if !ok {
		return fmt.Errorf("gltf: invalid asset.minVersion %q (want major.minor)", minVersion)
	}
	if major, minor, ok := parseGLTFVersion(version); ok && (minMajor > major || (minMajor == major && minMinor > minor)) {
		return fmt.Errorf("gltf: asset.minVersion %q is newer than asset.version %q", minVersion, version)
	}
	if minMajor != 2 || minMinor > supportedMinor {
		return fmt.Errorf("gltf: asset requires glTF %s, this loader supports 2.%d", minVersion, supportedMinor)
	}
	return nil
}

// parseGLTFVersion parses "major.minor" with non-negative decimal parts.
func parseGLTFVersion(v string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	ma, err1 := strconv.Atoi(a)
	mi, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || ma < 0 || mi < 0 {
		return 0, 0, false
	}
	return ma, mi, true
}

func decodeDataURI(uri string) ([]byte, error) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, errors.New("gltf: not a data URI")
	}
	rest := uri[len("data:"):]
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, errors.New("gltf: malformed data URI (missing comma)")
	}
	isBase64 := strings.HasSuffix(meta, ";base64") || strings.EqualFold(meta, "base64")
	if isBase64 {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			// Fallback for unpadded base64 payloads.
			decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
			if err != nil {
				return nil, fmt.Errorf("gltf: decode base64 data URI: %w", err)
			}
		}
		return decoded, nil
	}
	unescaped, err := url.PathUnescape(payload)
	if err != nil {
		return nil, fmt.Errorf("gltf: decode data URI: %w", err)
	}
	return []byte(unescaped), nil
}

// --- Loaders for each glTF section ---

// loadBuffers resolves every buffer's bytes and returns the set of meshopt placeholder buffers: those tagged as fallback, and URI-less ones that compressed bufferViews decode into. A placeholder gets zeroed storage of its byteLength, charged to decodedBytes, instead of being fetched.
func (d *Document) loadBuffers(raws []rawBuffer, binChunk []byte, resolver BufferResolver, meshoptTargets map[int]bool, decodedBytes *uint64) (_ map[int]bool, err error) {
	at := -1
	defer func() { err = itemError(err, "buffers", at) }()
	d.Buffers = make([]Buffer, len(raws))
	placeholders := make(map[int]bool)
	for i, rb := range raws {
		at = i
		if rb.ByteLength < 0 {
			return nil, fmt.Errorf("gltf: buffer %d has negative byteLength %d", i, rb.ByteLength)
		}
		fallback, err := isMeshoptFallback(rb)
		if err != nil {
			return nil, fmt.Errorf("gltf: buffer %d: %w", i, err)
		}
		glbChunk := rb.URI == "" && i == 0 && binChunk != nil
		var data []byte
		switch {
		case fallback || (rb.URI == "" && !glbChunk && meshoptTargets[i]):
			if err := chargeDecodedBytes(decodedBytes, uint64(rb.ByteLength)); err != nil {
				return nil, err
			}
			data = make([]byte, rb.ByteLength)
			placeholders[i] = true
		case rb.URI == "":
			if i != 0 || binChunk == nil {
				return nil, fmt.Errorf("gltf: buffer %d has empty URI without GLB BIN chunk", i)
			}
			data = binChunk
		case strings.HasPrefix(rb.URI, "data:"):
			data, err = decodeDataURI(rb.URI)
			if err != nil {
				return nil, fmt.Errorf("gltf: buffer %d: %w", i, err)
			}
		default:
			if resolver == nil {
				return nil, fmt.Errorf("gltf: buffer %d external URI %q requires a BufferResolver", i, rb.URI)
			}
			data, err = resolver(rb.URI)
			if err != nil {
				return nil, fmt.Errorf("gltf: resolve buffer %d (%q): %w", i, rb.URI, err)
			}
		}
		if len(data) < rb.ByteLength {
			return nil, fmt.Errorf("gltf: buffer %d data length %d < declared byteLength %d", i, len(data), rb.ByteLength)
		}
		// Trim any GLB 4-byte alignment padding to exact declared byteLength.
		data = data[:rb.ByteLength]
		d.Buffers[i] = Buffer{
			Name:       rb.Name,
			URI:        rb.URI,
			ByteLength: rb.ByteLength,
			Data:       data,
			Extras:     cloneRawMessage(rb.Extras),
		}
	}
	return placeholders, nil
}

func (d *Document) loadBufferViews(raws []rawBufferView) (err error) {
	at := -1
	defer func() { err = itemError(err, "bufferViews", at) }()
	d.BufferViews = make([]BufferView, len(raws))
	for i, rv := range raws {
		at = i
		if rv.Buffer < 0 || rv.Buffer >= len(d.Buffers) {
			return fmt.Errorf("gltf: bufferView %d references invalid buffer %d", i, rv.Buffer)
		}
		if rv.ByteOffset < 0 || rv.ByteLength < 0 || rv.ByteStride < 0 {
			return fmt.Errorf("gltf: bufferView %d has negative byteOffset/byteLength/byteStride", i)
		}
		bufLen := uint64(len(d.Buffers[rv.Buffer].Data))
		if uint64(rv.ByteOffset) > bufLen || uint64(rv.ByteLength) > bufLen-uint64(rv.ByteOffset) {
			return fmt.Errorf("gltf: bufferView %d exceeds buffer %d bounds", i, rv.Buffer)
		}
		d.BufferViews[i] = BufferView{
			Name:       rv.Name,
			Buffer:     rv.Buffer,
			ByteOffset: rv.ByteOffset,
			ByteLength: rv.ByteLength,
			ByteStride: rv.ByteStride,
			Target:     rv.Target,
			Extras:     cloneRawMessage(rv.Extras),
			Extensions: cloneExtensions(rv.Extensions),
		}
	}
	return nil
}

func (d *Document) loadAccessors(raws []rawAccessor) (err error) {
	at := -1
	defer func() { err = itemError(err, "accessors", at) }()
	d.Accessors = make([]Accessor, len(raws))
	for i, ra := range raws {
		at = i
		bvIdx := -1
		if ra.BufferView != nil {
			bvIdx = *ra.BufferView
			if bvIdx < 0 || bvIdx >= len(d.BufferViews) {
				return fmt.Errorf("gltf: accessor %d references invalid bufferView %d", i, bvIdx)
			}
		}
		if ra.ByteOffset < 0 {
			return fmt.Errorf("gltf: accessor %d has negative byteOffset %d", i, ra.ByteOffset)
		}
		if ra.Count < 0 {
			return fmt.Errorf("gltf: accessor %d has negative count %d", i, ra.Count)
		}
		ct := ComponentType(ra.ComponentType)
		if ct.ByteSize() == 0 {
			return fmt.Errorf("gltf: accessor %d has unsupported componentType %d", i, ra.ComponentType)
		}
		at := AccessorType(ra.Type)
		if at.Components() == 0 {
			return fmt.Errorf("gltf: accessor %d has unsupported type %q", i, ra.Type)
		}

		var sparse *AccessorSparse
		if ra.Sparse != nil {
			rs := ra.Sparse
			if rs.Count < 0 || rs.Count > ra.Count {
				return fmt.Errorf("gltf: accessor %d sparse count %d out of range [0, %d]", i, rs.Count, ra.Count)
			}
			if rs.Indices.BufferView < 0 || rs.Indices.BufferView >= len(d.BufferViews) {
				return fmt.Errorf("gltf: accessor %d sparse indices bufferView %d out of range", i, rs.Indices.BufferView)
			}
			if rs.Values.BufferView < 0 || rs.Values.BufferView >= len(d.BufferViews) {
				return fmt.Errorf("gltf: accessor %d sparse values bufferView %d out of range", i, rs.Values.BufferView)
			}
			if rs.Indices.ByteOffset < 0 || rs.Values.ByteOffset < 0 {
				return fmt.Errorf("gltf: accessor %d sparse has negative byteOffset", i)
			}
			ict := ComponentType(rs.Indices.ComponentType)
			if ict != ComponentUnsignedByte && ict != ComponentUnsignedShort && ict != ComponentUnsignedInt {
				return fmt.Errorf("gltf: accessor %d sparse indices has invalid componentType %d", i, rs.Indices.ComponentType)
			}
			sparse = &AccessorSparse{
				Count: rs.Count,
				Indices: AccessorSparseIndices{
					BufferView:    rs.Indices.BufferView,
					ByteOffset:    rs.Indices.ByteOffset,
					ComponentType: ict,
				},
				Values: AccessorSparseValues{
					BufferView: rs.Values.BufferView,
					ByteOffset: rs.Values.ByteOffset,
				},
			}
		}

		d.Accessors[i] = Accessor{
			Name:          ra.Name,
			BufferView:    bvIdx,
			ByteOffset:    ra.ByteOffset,
			ComponentType: ct,
			Normalized:    ra.Normalized,
			Count:         ra.Count,
			Type:          at,
			Min:           ra.Min,
			Max:           ra.Max,
			Sparse:        sparse,
			Extras:        cloneRawMessage(ra.Extras),
		}
	}
	return nil
}

func (d *Document) loadSamplers(raws []rawSampler) (err error) {
	at := -1
	defer func() { err = itemError(err, "samplers", at) }()
	d.Samplers = make([]Sampler, len(raws))
	for i, rs := range raws {
		at = i
		wrapS := WrapRepeat
		if rs.WrapS != nil {
			wrapS = WrapMode(*rs.WrapS)
		}
		wrapT := WrapRepeat
		if rs.WrapT != nil {
			wrapT = WrapMode(*rs.WrapT)
		}
		d.Samplers[i] = Sampler{
			Name:      rs.Name,
			MagFilter: FilterMode(rs.MagFilter),
			MinFilter: FilterMode(rs.MinFilter),
			WrapS:     wrapS,
			WrapT:     wrapT,
			Extras:    cloneRawMessage(rs.Extras),
		}
	}
	return nil
}

func (d *Document) loadImages(raws []rawImage) (err error) {
	at := -1
	defer func() { err = itemError(err, "images", at) }()
	d.Images = make([]Image, len(raws))
	for i, ri := range raws {
		at = i
		bvIdx := -1
		if ri.BufferView != nil {
			bvIdx = *ri.BufferView
			if bvIdx < 0 || bvIdx >= len(d.BufferViews) {
				return fmt.Errorf("gltf: image %d references invalid bufferView %d", i, bvIdx)
			}
		}
		d.Images[i] = Image{
			Name:       ri.Name,
			URI:        ri.URI,
			MimeType:   ri.MimeType,
			BufferView: bvIdx,
			Extras:     cloneRawMessage(ri.Extras),
		}
	}
	return nil
}

func (d *Document) loadTextures(raws []rawTexture) (err error) {
	at := -1
	defer func() { err = itemError(err, "textures", at) }()
	d.Textures = make([]Texture, len(raws))
	for i, rt := range raws {
		at = i
		samplerIdx := -1
		if rt.Sampler != nil {
			samplerIdx = *rt.Sampler
			if samplerIdx < 0 || samplerIdx >= len(d.Samplers) {
				return fmt.Errorf("gltf: texture %d references invalid sampler %d", i, samplerIdx)
			}
		}
		sourceIdx := -1
		if rt.Source != nil {
			sourceIdx = *rt.Source
			if sourceIdx < 0 || sourceIdx >= len(d.Images) {
				return fmt.Errorf("gltf: texture %d references invalid image %d", i, sourceIdx)
			}
		}
		d.Textures[i] = Texture{
			Name:       rt.Name,
			Sampler:    samplerIdx,
			Source:     sourceIdx,
			Extras:     cloneRawMessage(rt.Extras),
			Extensions: cloneExtensions(rt.Extensions),
		}
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func parseTextureTransformVec2(field string, raw json.RawMessage) ([2]float32, error) {
	if isJSONNull(raw) {
		return [2]float32{}, fmt.Errorf("gltf: %s %s must be a 2-element number array", extTextureTransform, field)
	}
	var elems []*float32
	if err := json.Unmarshal(raw, &elems); err != nil {
		return [2]float32{}, fmt.Errorf("gltf: parse %s %s: %w", extTextureTransform, field, err)
	}
	if len(elems) != 2 {
		return [2]float32{}, fmt.Errorf("gltf: %s %s must have 2 elements, got %d", extTextureTransform, field, len(elems))
	}
	if elems[0] == nil || elems[1] == nil {
		return [2]float32{}, fmt.Errorf("gltf: %s %s elements must be finite numbers", extTextureTransform, field)
	}
	v0, v1 := *elems[0], *elems[1]
	if !finite32(v0) || !finite32(v1) {
		return [2]float32{}, fmt.Errorf("gltf: %s %s elements must be finite numbers", extTextureTransform, field)
	}
	return [2]float32{v0, v1}, nil
}

func parseTextureTransform(extData json.RawMessage) (*TextureTransform, error) {
	if !isJSONObject(extData) {
		return nil, fmt.Errorf("gltf: %s must be a JSON object", extTextureTransform)
	}
	var rawTT rawTextureTransform
	if err := json.Unmarshal(extData, &rawTT); err != nil {
		return nil, fmt.Errorf("gltf: parse %s: %w", extTextureTransform, err)
	}
	tt := DefaultTextureTransform()
	if len(rawTT.Offset) > 0 {
		off, err := parseTextureTransformVec2("offset", rawTT.Offset)
		if err != nil {
			return nil, err
		}
		tt.Offset = off
	}
	if len(rawTT.Rotation) > 0 {
		if isJSONNull(rawTT.Rotation) {
			return nil, fmt.Errorf("gltf: %s rotation must be a finite number", extTextureTransform)
		}
		var rot float32
		if err := json.Unmarshal(rawTT.Rotation, &rot); err != nil {
			return nil, fmt.Errorf("gltf: parse %s rotation: %w", extTextureTransform, err)
		}
		if !finite32(rot) {
			return nil, fmt.Errorf("gltf: %s rotation must be finite", extTextureTransform)
		}
		tt.Rotation = rot
	}
	if len(rawTT.Scale) > 0 {
		sc, err := parseTextureTransformVec2("scale", rawTT.Scale)
		if err != nil {
			return nil, err
		}
		tt.Scale = sc
	}
	if len(rawTT.TexCoord) > 0 {
		if isJSONNull(rawTT.TexCoord) {
			return nil, fmt.Errorf("gltf: %s texCoord must be a non-negative integer", extTextureTransform)
		}
		var tc int
		if err := json.Unmarshal(rawTT.TexCoord, &tc); err != nil {
			return nil, fmt.Errorf("gltf: parse %s texCoord: %w", extTextureTransform, err)
		}
		if tc < 0 {
			return nil, fmt.Errorf("gltf: %s has negative texCoord %d", extTextureTransform, tc)
		}
		tt.TexCoord = &tc
	}
	return &tt, nil
}

func (d *Document) parseTextureRef(info *rawTextureInfo, defaultScale, defaultStrength float32) (*TextureRef, error) {
	if info == nil {
		return nil, nil
	}
	if info.Index == nil || *info.Index < 0 || *info.Index >= len(d.Textures) {
		return nil, errors.New("gltf: texture reference has invalid texture index")
	}
	if info.TexCoord < 0 {
		return nil, fmt.Errorf("gltf: texture reference has negative texCoord %d", info.TexCoord)
	}
	ref := &TextureRef{
		TextureIndex: *info.Index,
		TexCoord:     info.TexCoord,
		Scale:        defaultScale,
		Strength:     defaultStrength,
	}
	if info.Scale != nil {
		ref.Scale = *info.Scale
	}
	if info.Strength != nil {
		ref.Strength = *info.Strength
	}
	if extData, ok := info.Extensions[extTextureTransform]; ok {
		tt, err := parseTextureTransform(extData)
		if err != nil {
			return nil, err
		}
		ref.Transform = tt
	}
	return ref, nil
}

func (d *Document) loadMaterials(raws []rawMaterial) (err error) {
	at := -1
	defer func() { err = itemError(err, "materials", at) }()
	d.Materials = make([]Material, len(raws))
	for i, rm := range raws {
		at = i
		mat := Material{
			Name:            rm.Name,
			BaseColorFactor: math3d.Vec4One(),
			MetallicFactor:  1,
			RoughnessFactor: 1,
			EmissiveFactor:  math3d.Vec3Zero(),
			AlphaMode:       AlphaOpaque,
			AlphaCutoff:     0.5,
			DoubleSided:     rm.DoubleSided,
			Extras:          cloneRawMessage(rm.Extras),
			Extensions:      cloneExtensions(rm.Extensions),
		}
		if rm.AlphaMode != "" {
			switch AlphaMode(rm.AlphaMode) {
			case AlphaOpaque, AlphaMask, AlphaBlend:
				mat.AlphaMode = AlphaMode(rm.AlphaMode)
			default:
				return fmt.Errorf("gltf: material %d has unknown alphaMode %q", i, rm.AlphaMode)
			}
		}
		if rm.AlphaCutoff != nil {
			mat.AlphaCutoff = *rm.AlphaCutoff
		}
		if rm.EmissiveFactor != nil {
			e := *rm.EmissiveFactor
			mat.EmissiveFactor = math3d.Vec3{X: e[0], Y: e[1], Z: e[2]}
		}
		if rm.PBRMetallicRoughness != nil {
			pbr := rm.PBRMetallicRoughness
			if pbr.BaseColorFactor != nil {
				c := *pbr.BaseColorFactor
				mat.BaseColorFactor = math3d.Vec4{X: c[0], Y: c[1], Z: c[2], W: c[3]}
			}
			if pbr.MetallicFactor != nil {
				mat.MetallicFactor = *pbr.MetallicFactor
			}
			if pbr.RoughnessFactor != nil {
				mat.RoughnessFactor = *pbr.RoughnessFactor
			}
			var err error
			if mat.BaseColorTexture, err = d.parseTextureRef(pbr.BaseColorTexture, 1, 1); err != nil {
				return fmt.Errorf("gltf: material %d baseColorTexture: %w", i, err)
			}
			if mat.MetallicRoughnessTexture, err = d.parseTextureRef(pbr.MetallicRoughnessTexture, 1, 1); err != nil {
				return fmt.Errorf("gltf: material %d metallicRoughnessTexture: %w", i, err)
			}
		}
		var err error
		if mat.NormalTexture, err = d.parseTextureRef(rm.NormalTexture, 1, 1); err != nil {
			return fmt.Errorf("gltf: material %d normalTexture: %w", i, err)
		}
		if mat.OcclusionTexture, err = d.parseTextureRef(rm.OcclusionTexture, 1, 1); err != nil {
			return fmt.Errorf("gltf: material %d occlusionTexture: %w", i, err)
		}
		if mat.EmissiveTexture, err = d.parseTextureRef(rm.EmissiveTexture, 1, 1); err != nil {
			return fmt.Errorf("gltf: material %d emissiveTexture: %w", i, err)
		}
		if _, ok := rm.Extensions["KHR_materials_unlit"]; ok {
			mat.Unlit = true
		}
		if err := d.parseMaterialExtensions(&mat, i, rm.Extensions); err != nil {
			return err
		}
		d.Materials[i] = mat
	}
	return nil
}

func (d *Document) loadVariants(exts map[string]json.RawMessage) error {
	rawExt, ok := exts[extMaterialsVariants]
	if !ok {
		return nil
	}
	field := loaderr.Path("extensions", extMaterialsVariants)
	if !isJSONObject(rawExt) {
		return loaderr.Errorf(field, fixGLTF, "gltf: %s must be a JSON object", extMaterialsVariants)
	}
	var parsed rawVariantsExt
	if err := json.Unmarshal(rawExt, &parsed); err != nil {
		return loaderr.Errorf(field, fixGLTF, "gltf: parse %s: %w", extMaterialsVariants, err)
	}
	if len(parsed.Variants) == 0 {
		return loaderr.Errorf(field, fixGLTF, "gltf: %s requires a non-empty variants array", extMaterialsVariants)
	}
	d.Variants = make([]MaterialVariant, len(parsed.Variants))
	for i, rawItem := range parsed.Variants {
		if !isJSONObject(rawItem) {
			return loaderr.Errorf(field, fixGLTF, "gltf: %s variant %d must be a JSON object", extMaterialsVariants, i)
		}
		var rv rawVariant
		if err := json.Unmarshal(rawItem, &rv); err != nil {
			return loaderr.Errorf(field, fixGLTF, "gltf: parse %s variant %d: %w", extMaterialsVariants, i, err)
		}
		if rv.Name == nil {
			return loaderr.Errorf(field, fixGLTF, "gltf: %s variant %d is missing required name", extMaterialsVariants, i)
		}
		d.Variants[i] = MaterialVariant{
			Name:   *rv.Name,
			Extras: cloneRawMessage(rv.Extras),
		}
	}
	return nil
}

func (d *Document) parsePrimitiveVariants(mi, pi int, extData json.RawMessage) (map[int]int, error) {
	if !isJSONObject(extData) {
		return nil, fmt.Errorf("gltf: mesh %d primitive %d %s must be a JSON object", mi, pi, extMaterialsVariants)
	}
	var parsed rawPrimitiveVariantsExt
	if err := json.Unmarshal(extData, &parsed); err != nil {
		return nil, fmt.Errorf("gltf: mesh %d primitive %d parse %s: %w", mi, pi, extMaterialsVariants, err)
	}
	if len(parsed.Mappings) == 0 {
		return nil, fmt.Errorf("gltf: mesh %d primitive %d %s requires a non-empty mappings array", mi, pi, extMaterialsVariants)
	}
	mappings := make(map[int]int)
	for ki, rawMap := range parsed.Mappings {
		if !isJSONObject(rawMap) {
			return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d must be a JSON object", mi, pi, extMaterialsVariants, ki)
		}
		var m rawVariantMapping
		if err := json.Unmarshal(rawMap, &m); err != nil {
			return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d: %w", mi, pi, extMaterialsVariants, ki, err)
		}
		if m.Material == nil || *m.Material < 0 || *m.Material >= len(d.Materials) {
			return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d invalid material", mi, pi, extMaterialsVariants, ki)
		}
		if len(m.Variants) == 0 {
			return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d requires a non-empty variants array", mi, pi, extMaterialsVariants, ki)
		}
		for _, vPtr := range m.Variants {
			if vPtr == nil {
				return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d contains null variant index", mi, pi, extMaterialsVariants, ki)
			}
			vIdx := *vPtr
			if vIdx < 0 || vIdx >= len(d.Variants) {
				return nil, fmt.Errorf("gltf: mesh %d primitive %d %s mapping %d invalid variant %d", mi, pi, extMaterialsVariants, ki, vIdx)
			}
			if _, exists := mappings[vIdx]; exists {
				return nil, fmt.Errorf("gltf: mesh %d primitive %d %s variant %d is mapped more than once", mi, pi, extMaterialsVariants, vIdx)
			}
			mappings[vIdx] = *m.Material
		}
	}
	return mappings, nil
}

func (d *Document) loadCameras(raws []rawCamera) (err error) {
	at := -1
	defer func() { err = itemError(err, "cameras", at) }()
	d.Cameras = make([]Camera, len(raws))
	for i, rc := range raws {
		at = i
		cam := Camera{
			Name:   rc.Name,
			Type:   CameraType(rc.Type),
			Extras: cloneRawMessage(rc.Extras),
		}
		switch cam.Type {
		case CameraPerspective:
			if rc.Perspective == nil {
				return fmt.Errorf("gltf: camera %d missing perspective parameters", i)
			}
			cam.AspectRatio = rc.Perspective.AspectRatio
			cam.YFOV = rc.Perspective.YFOV
			cam.ZNear = rc.Perspective.ZNear
			cam.ZFar = rc.Perspective.ZFar
		case CameraOrthographic:
			if rc.Orthographic == nil {
				return fmt.Errorf("gltf: camera %d missing orthographic parameters", i)
			}
			cam.XMag = rc.Orthographic.XMag
			cam.YMag = rc.Orthographic.YMag
			cam.ZNear = rc.Orthographic.ZNear
			cam.ZFar = rc.Orthographic.ZFar
		default:
			return fmt.Errorf("gltf: camera %d has unsupported type %q", i, rc.Type)
		}
		d.Cameras[i] = cam
	}
	return nil
}

func chargeDecodedBytes(acc *uint64, n uint64) error {
	if n > maxLoadDecodedBytes || *acc > maxLoadDecodedBytes-n {
		return errors.New("gltf: decoded geometry exceeds safety limit")
	}
	*acc += n
	return nil
}

func (d *Document) loadMeshes(raws []rawMesh, decodedBytes *uint64) (err error) {
	at := -1
	defer func() { err = itemError(err, "meshes", at) }()
	d.Meshes = make([]Mesh, len(raws))
	for mi, rm := range raws {
		at = mi
		mesh := Mesh{
			Name:       rm.Name,
			Primitives: make([]Primitive, len(rm.Primitives)),
			Weights:    rm.Weights,
			Extras:     cloneRawMessage(rm.Extras),
		}
		for pi, rp := range rm.Primitives {
			mode := ModeTriangles
			if rp.Mode != nil {
				mode = PrimitiveMode(*rp.Mode)
				if mode < ModePoints || mode > ModeTriangleFan {
					return fmt.Errorf("gltf: mesh %d primitive %d invalid mode %d", mi, pi, mode)
				}
			}
			matIdx := -1
			if rp.Material != nil {
				matIdx = *rp.Material
				if matIdx < 0 || matIdx >= len(d.Materials) {
					return fmt.Errorf("gltf: mesh %d primitive %d invalid material %d", mi, pi, matIdx)
				}
			}

			if _, ok := rp.Extensions[extDraco]; ok {
				var err error
				if mode, err = d.decodeDracoPrimitive(mi, pi, rp, mode, decodedBytes); err != nil {
					return err
				}
			}

			prim := Primitive{
				Mode:       mode,
				Material:   matIdx,
				Bounds:     math3d.AABBEmpty(),
				Attributes: rp.Attributes,
				Extras:     cloneRawMessage(rp.Extras),
			}

			// Validate all attribute accessor indices exist.
			for attrName, accIdx := range rp.Attributes {
				if accIdx < 0 || accIdx >= len(d.Accessors) {
					return fmt.Errorf("gltf: mesh %d primitive %d attribute %q invalid accessor %d", mi, pi, attrName, accIdx)
				}
			}

			if accIdx, ok := rp.Attributes["POSITION"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*12); err != nil {
					return err
				}
				pos, err := d.ReadVec3(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d POSITION: %w", mi, pi, err)
				}
				prim.Positions = pos
				prim.Bounds = computeAABB(pos)
			}
			if accIdx, ok := rp.Attributes["NORMAL"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*12); err != nil {
					return err
				}
				norms, err := d.ReadVec3(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d NORMAL: %w", mi, pi, err)
				}
				prim.Normals = norms
			}
			if accIdx, ok := rp.Attributes["TANGENT"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*16); err != nil {
					return err
				}
				tans, err := d.ReadVec4(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d TANGENT: %w", mi, pi, err)
				}
				prim.Tangents = tans
			}
			if accIdx, ok := rp.Attributes["TEXCOORD_0"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*8); err != nil {
					return err
				}
				uv, err := d.ReadVec2(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d TEXCOORD_0: %w", mi, pi, err)
				}
				prim.TexCoords0 = uv
			}
			if accIdx, ok := rp.Attributes["TEXCOORD_1"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*8); err != nil {
					return err
				}
				uv, err := d.ReadVec2(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d TEXCOORD_1: %w", mi, pi, err)
				}
				prim.TexCoords1 = uv
			}
			if accIdx, ok := rp.Attributes["COLOR_0"]; ok {
				acc := d.Accessors[accIdx]
				if err := chargeDecodedBytes(decodedBytes, uint64(acc.Count)*16); err != nil {
					return err
				}
				switch acc.Type {
				case AccessorVec4:
					colors, err := d.ReadVec4(accIdx)
					if err != nil {
						return fmt.Errorf("gltf: mesh %d primitive %d COLOR_0: %w", mi, pi, err)
					}
					prim.Colors0 = colors
				case AccessorVec3:
					rgb, err := d.ReadVec3(accIdx)
					if err != nil {
						return fmt.Errorf("gltf: mesh %d primitive %d COLOR_0: %w", mi, pi, err)
					}
					colors := make([]math3d.Vec4, len(rgb))
					for i, c := range rgb {
						colors[i] = math3d.Vec4{X: c.X, Y: c.Y, Z: c.Z, W: 1}
					}
					prim.Colors0 = colors
				default:
					return fmt.Errorf("gltf: mesh %d primitive %d COLOR_0 must be VEC3 or VEC4, got %s", mi, pi, acc.Type)
				}
			}
			if accIdx, ok := rp.Attributes["JOINTS_0"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*8); err != nil {
					return err
				}
				joints, err := d.ReadJoints(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d JOINTS_0: %w", mi, pi, err)
				}
				prim.Joints0 = joints
			}
			if accIdx, ok := rp.Attributes["WEIGHTS_0"]; ok {
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*16); err != nil {
					return err
				}
				weights, err := d.ReadWeights(accIdx)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d WEIGHTS_0: %w", mi, pi, err)
				}
				prim.Weights0 = weights
			}

			if rp.Indices != nil {
				idxAcc := *rp.Indices
				if idxAcc < 0 || idxAcc >= len(d.Accessors) {
					return fmt.Errorf("gltf: mesh %d primitive %d invalid indices accessor %d", mi, pi, idxAcc)
				}
				if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[idxAcc].Count)*4); err != nil {
					return err
				}
				indices, err := d.ReadIndices(idxAcc)
				if err != nil {
					return fmt.Errorf("gltf: mesh %d primitive %d indices: %w", mi, pi, err)
				}
				prim.Indices = indices
			}

			if len(rp.Targets) > 0 {
				prim.Targets = make([]MorphTarget, len(rp.Targets))
				for ti, rt := range rp.Targets {
					var target MorphTarget
					if accIdx, ok := rt["POSITION"]; ok {
						if accIdx < 0 || accIdx >= len(d.Accessors) {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d invalid POSITION accessor %d", mi, pi, ti, accIdx)
						}
						if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*12); err != nil {
							return err
						}
						pos, err := d.ReadVec3(accIdx)
						if err != nil {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d POSITION: %w", mi, pi, ti, err)
						}
						target.Positions = pos
					}
					if accIdx, ok := rt["NORMAL"]; ok {
						if accIdx < 0 || accIdx >= len(d.Accessors) {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d invalid NORMAL accessor %d", mi, pi, ti, accIdx)
						}
						if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*12); err != nil {
							return err
						}
						norms, err := d.ReadVec3(accIdx)
						if err != nil {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d NORMAL: %w", mi, pi, ti, err)
						}
						target.Normals = norms
					}
					if accIdx, ok := rt["TANGENT"]; ok {
						if accIdx < 0 || accIdx >= len(d.Accessors) {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d invalid TANGENT accessor %d", mi, pi, ti, accIdx)
						}
						if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*12); err != nil {
							return err
						}
						tans, err := d.ReadVec3(accIdx)
						if err != nil {
							return fmt.Errorf("gltf: mesh %d primitive %d target %d TANGENT: %w", mi, pi, ti, err)
						}
						target.Tangents = tans
					}
					prim.Targets[ti] = target
				}
			}

			if err := d.validatePrimitive(rp, &prim); err != nil {
				return fmt.Errorf("gltf: mesh %d primitive %d: %w", mi, pi, err)
			}

			if extData, ok := rp.Extensions[extMaterialsVariants]; ok {
				mappings, err := d.parsePrimitiveVariants(mi, pi, extData)
				if err != nil {
					return err
				}
				prim.VariantMappings = mappings
			}

			mesh.Primitives[pi] = prim
		}
		// Every primitive of a mesh carries the same morph targets, and mesh.weights (when present) has one weight per target.
		for pi := 1; pi < len(mesh.Primitives); pi++ {
			if len(mesh.Primitives[pi].Targets) != len(mesh.Primitives[0].Targets) {
				return fmt.Errorf("gltf: mesh %d primitive %d has %d morph targets, primitive 0 has %d", mi, pi, len(mesh.Primitives[pi].Targets), len(mesh.Primitives[0].Targets))
			}
		}
		if len(mesh.Weights) > 0 && len(mesh.Primitives) > 0 && len(mesh.Weights) != len(mesh.Primitives[0].Targets) {
			return fmt.Errorf("gltf: mesh %d has %d weights for %d morph targets", mi, len(mesh.Weights), len(mesh.Primitives[0].Targets))
		}
		d.Meshes[mi] = mesh
	}
	return nil
}

// validatePrimitive checks what the spec requires of a primitive's data and what the decoders rely on: every attribute and morph-target accessor has the vertex count, indices stay below it, and decoded float data is finite so it cannot poison bounds or skinning (#546).
func (d *Document) validatePrimitive(rp rawPrimitive, prim *Primitive) error {
	vertexCount := -1
	names := make([]string, 0, len(rp.Attributes))
	for name := range rp.Attributes {
		names = append(names, name)
	}
	slices.Sort(names)
	if _, ok := rp.Attributes["POSITION"]; ok {
		vertexCount = d.Accessors[rp.Attributes["POSITION"]].Count
	}
	for _, name := range names {
		c := d.Accessors[rp.Attributes[name]].Count
		if vertexCount < 0 {
			vertexCount = c
		} else if c != vertexCount {
			return fmt.Errorf("attribute %s has %d elements, want %d", name, c, vertexCount)
		}
	}
	for ti, rt := range rp.Targets {
		targetNames := make([]string, 0, len(rt))
		for name := range rt {
			targetNames = append(targetNames, name)
		}
		slices.Sort(targetNames)
		for _, name := range targetNames {
			accIdx := rt[name]
			if accIdx < 0 || accIdx >= len(d.Accessors) {
				return fmt.Errorf("target %d invalid %s accessor %d", ti, name, accIdx)
			}
			if vertexCount >= 0 && d.Accessors[accIdx].Count != vertexCount {
				return fmt.Errorf("target %d %s has %d elements, want %d", ti, name, d.Accessors[accIdx].Count, vertexCount)
			}
		}
	}
	if vertexCount >= 0 {
		for i, idx := range prim.Indices {
			if uint64(idx) >= uint64(vertexCount) {
				return fmt.Errorf("index %d at %d is past the %d vertices", idx, i, vertexCount)
			}
		}
	}
	finite3 := func(name string, vs []math3d.Vec3) error {
		for i, v := range vs {
			if !finite32(v.X) || !finite32(v.Y) || !finite32(v.Z) {
				return fmt.Errorf("%s element %d is not finite", name, i)
			}
		}
		return nil
	}
	finite4 := func(name string, vs []math3d.Vec4) error {
		for i, v := range vs {
			if !finite32(v.X) || !finite32(v.Y) || !finite32(v.Z) || !finite32(v.W) {
				return fmt.Errorf("%s element %d is not finite", name, i)
			}
		}
		return nil
	}
	if err := finite3("POSITION", prim.Positions); err != nil {
		return err
	}
	if err := finite3("NORMAL", prim.Normals); err != nil {
		return err
	}
	if err := finite4("TANGENT", prim.Tangents); err != nil {
		return err
	}
	if err := finite4("WEIGHTS_0", prim.Weights0); err != nil {
		return err
	}
	if err := finite4("COLOR_0", prim.Colors0); err != nil {
		return err
	}
	for _, uv := range [][]math3d.Vec2{prim.TexCoords0, prim.TexCoords1} {
		for i, v := range uv {
			if !finite32(v.X) || !finite32(v.Y) {
				return fmt.Errorf("TEXCOORD element %d is not finite", i)
			}
		}
	}
	for ti, t := range prim.Targets {
		for _, part := range [][]math3d.Vec3{t.Positions, t.Normals, t.Tangents} {
			if err := finite3(fmt.Sprintf("target %d", ti), part); err != nil {
				return err
			}
		}
	}
	return nil
}

func finite32(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}

// validateSkinnedJoints checks that every JOINTS_0 value of a mesh drawn with a skin indexes that skin's joint list, so skinning never reads past it (#546).
func (d *Document) validateSkinnedJoints() error {
	for ni, n := range d.Nodes {
		if n.Mesh < 0 || n.Skin < 0 || n.Mesh >= len(d.Meshes) || n.Skin >= len(d.Skins) {
			continue
		}
		joints := len(d.Skins[n.Skin].Joints)
		for pi, prim := range d.Meshes[n.Mesh].Primitives {
			for vi, j := range prim.Joints0 {
				for _, idx := range j {
					if int(idx) >= joints {
						return fmt.Errorf("gltf: node %d mesh %d primitive %d vertex %d joint %d is past skin %d's %d joints", ni, n.Mesh, pi, vi, idx, n.Skin, joints)
					}
				}
			}
		}
	}
	return nil
}

func (d *Document) loadNodes(raws []rawNode) (err error) {
	at := -1
	defer func() { err = itemError(err, "nodes", at) }()
	d.Nodes = make([]Node, len(raws))
	for i, rn := range raws {
		at = i
		node := Node{
			Name:        rn.Name,
			Parent:      -1,
			Children:    rn.Children,
			Translation: math3d.Vec3Zero(),
			Rotation:    math3d.QuatIdentity(),
			Scale:       math3d.Vec3One(),
			LocalMatrix: math3d.Mat4Identity(),
			WorldMatrix: math3d.Mat4Identity(),
			Mesh:        -1,
			Skin:        -1,
			Camera:      -1,
			Weights:     rn.Weights,
			Extras:      cloneRawMessage(rn.Extras),
			Extensions:  cloneExtensions(rn.Extensions),
		}

		if rn.Mesh != nil {
			if *rn.Mesh < 0 || *rn.Mesh >= len(d.Meshes) {
				return fmt.Errorf("gltf: node %d references invalid mesh %d", i, *rn.Mesh)
			}
			node.Mesh = *rn.Mesh
		}
		if rn.Camera != nil {
			if *rn.Camera < 0 || *rn.Camera >= len(d.Cameras) {
				return fmt.Errorf("gltf: node %d references invalid camera %d", i, *rn.Camera)
			}
			node.Camera = *rn.Camera
		}
		// Skin index is validated once Skins are loaded (since skins reference nodes and nodes reference skins).
		if rn.Skin != nil {
			node.Skin = *rn.Skin
		}

		if len(rn.Matrix) > 0 {
			if len(rn.Matrix) != 16 {
				return fmt.Errorf("gltf: node %d matrix must have 16 elements, got %d", i, len(rn.Matrix))
			}
			var m math3d.Mat4
			copy(m[:], rn.Matrix)
			node.LocalMatrix = m
			node.HasMatrix = true
			if pos, rot, scale, ok := m.Decompose(); ok {
				node.Translation = pos
				node.Rotation = rot
				node.Scale = scale
			} else {
				node.Translation = math3d.Vec3{X: m[12], Y: m[13], Z: m[14]}
				node.MatrixNotTRS = true
			}
		} else {
			if len(rn.Translation) > 0 {
				if len(rn.Translation) != 3 {
					return fmt.Errorf("gltf: node %d translation must have 3 elements, got %d", i, len(rn.Translation))
				}
				node.Translation = math3d.Vec3{X: rn.Translation[0], Y: rn.Translation[1], Z: rn.Translation[2]}
			}
			if len(rn.Rotation) > 0 {
				if len(rn.Rotation) != 4 {
					return fmt.Errorf("gltf: node %d rotation must have 4 elements, got %d", i, len(rn.Rotation))
				}
				node.Rotation = math3d.Quat{X: rn.Rotation[0], Y: rn.Rotation[1], Z: rn.Rotation[2], W: rn.Rotation[3]}
			}
			if len(rn.Scale) > 0 {
				if len(rn.Scale) != 3 {
					return fmt.Errorf("gltf: node %d scale must have 3 elements, got %d", i, len(rn.Scale))
				}
				node.Scale = math3d.Vec3{X: rn.Scale[0], Y: rn.Scale[1], Z: rn.Scale[2]}
			}
			node.LocalMatrix = math3d.TRS(node.Translation, node.Rotation, node.Scale)
		}

		d.Nodes[i] = node
	}
	return nil
}

func (d *Document) loadScenes(defaultScene *int, raws []rawScene) (err error) {
	at := -1
	defer func() { err = itemError(err, "scenes", at) }()
	d.Scenes = make([]Scene, len(raws))
	for i, rs := range raws {
		at = i
		for _, rootIdx := range rs.Nodes {
			if rootIdx < 0 || rootIdx >= len(d.Nodes) {
				return fmt.Errorf("gltf: scene %d references invalid root node %d", i, rootIdx)
			}
			if d.Nodes[rootIdx].Parent >= 0 {
				return fmt.Errorf("gltf: scene %d root node %d is a child of node %d", i, rootIdx, d.Nodes[rootIdx].Parent)
			}
		}
		d.Scenes[i] = Scene{
			Name:   rs.Name,
			Roots:  rs.Nodes,
			Extras: cloneRawMessage(rs.Extras),
		}
	}
	if defaultScene != nil {
		if *defaultScene < 0 || *defaultScene >= len(d.Scenes) {
			return fmt.Errorf("gltf: default scene index %d out of range [0, %d)", *defaultScene, len(d.Scenes))
		}
		d.Scene = *defaultScene
	}
	return nil
}

func (d *Document) loadSkins(raws []rawSkin, decodedBytes *uint64) (err error) {
	at := -1
	defer func() { err = itemError(err, "skins", at) }()
	d.Skins = make([]Skin, len(raws))
	for i, rs := range raws {
		at = i
		for _, j := range rs.Joints {
			if j < 0 || j >= len(d.Nodes) {
				return fmt.Errorf("gltf: skin %d references invalid joint node %d", i, j)
			}
		}
		skeleton := -1
		if rs.Skeleton != nil {
			skeleton = *rs.Skeleton
			if skeleton < 0 || skeleton >= len(d.Nodes) {
				return fmt.Errorf("gltf: skin %d references invalid skeleton node %d", i, skeleton)
			}
		}
		var ibms []math3d.Mat4
		if rs.InverseBindMatrices != nil {
			accIdx := *rs.InverseBindMatrices
			if accIdx < 0 || accIdx >= len(d.Accessors) {
				return fmt.Errorf("gltf: skin %d references invalid inverseBindMatrices accessor %d", i, accIdx)
			}
			if err := chargeDecodedBytes(decodedBytes, uint64(d.Accessors[accIdx].Count)*64); err != nil {
				return err
			}
			var err error
			ibms, err = d.ReadMat4(accIdx)
			if err != nil {
				return fmt.Errorf("gltf: skin %d inverseBindMatrices: %w", i, err)
			}
			if len(ibms) < len(rs.Joints) {
				return fmt.Errorf("gltf: skin %d inverseBindMatrices count %d < joints count %d", i, len(ibms), len(rs.Joints))
			}
			// The spec allows extra matrices; only the joints' rows are used.
			ibms = ibms[:len(rs.Joints)]
		} else {
			ibms = make([]math3d.Mat4, len(rs.Joints))
			for j := range ibms {
				ibms[j] = math3d.Mat4Identity()
			}
		}
		d.Skins[i] = Skin{
			Name:                rs.Name,
			Joints:              rs.Joints,
			Skeleton:            skeleton,
			InverseBindMatrices: ibms,
			Extras:              cloneRawMessage(rs.Extras),
		}
	}
	// Validate node.Skin indices now that Skins are loaded.
	for i := range d.Nodes {
		if d.Nodes[i].Skin != -1 && (d.Nodes[i].Skin < 0 || d.Nodes[i].Skin >= len(d.Skins)) {
			return fmt.Errorf("gltf: node %d references invalid skin %d", i, d.Nodes[i].Skin)
		}
	}
	return nil
}

func (d *Document) loadAnimations(raws []rawAnimation, decodedBytes *uint64) (err error) {
	at := -1
	defer func() { err = itemError(err, "animations", at) }()
	d.Animations = make([]Animation, len(raws))
	for ai, ra := range raws {
		at = ai
		anim := Animation{
			Name:     ra.Name,
			Channels: make([]AnimationChannel, 0, len(ra.Channels)),
			Extras:   cloneRawMessage(ra.Extras),
		}
		for ci, rc := range ra.Channels {
			if rc.Sampler == nil || *rc.Sampler < 0 || *rc.Sampler >= len(ra.Samplers) {
				return fmt.Errorf("gltf: animation %d channel %d invalid sampler", ai, ci)
			}
			targetNode := -1
			if rc.Target.Node != nil {
				targetNode = *rc.Target.Node
				if targetNode < 0 || targetNode >= len(d.Nodes) {
					return fmt.Errorf("gltf: animation %d channel %d invalid target node %d", ai, ci, targetNode)
				}
			}

			path := TargetPath(rc.Target.Path)
			switch path {
			case PathTranslation, PathRotation, PathScale, PathWeights:
			default:
				return fmt.Errorf("gltf: animation %d channel %d unsupported target path %q", ai, ci, rc.Target.Path)
			}

			rs := ra.Samplers[*rc.Sampler]
			if rs.Input == nil || *rs.Input < 0 || *rs.Input >= len(d.Accessors) {
				return fmt.Errorf("gltf: animation %d sampler %d invalid input accessor", ai, *rc.Sampler)
			}
			if rs.Output == nil || *rs.Output < 0 || *rs.Output >= len(d.Accessors) {
				return fmt.Errorf("gltf: animation %d sampler %d invalid output accessor", ai, *rc.Sampler)
			}

			interp := InterpolationLinear
			if rs.Interpolation != "" {
				switch Interpolation(rs.Interpolation) {
				case InterpolationLinear, InterpolationStep, InterpolationCubicSpline:
					interp = Interpolation(rs.Interpolation)
				default:
					return fmt.Errorf("gltf: animation %d sampler %d unsupported interpolation %q", ai, *rc.Sampler, rs.Interpolation)
				}
			}

			inAcc := d.Accessors[*rs.Input]
			if err := chargeDecodedBytes(decodedBytes, uint64(inAcc.Count)*4); err != nil {
				return err
			}
			times, err := d.ReadScalar(*rs.Input)
			if err != nil {
				return fmt.Errorf("gltf: animation %d channel %d input: %w", ai, ci, err)
			}
			if len(times) == 0 {
				return fmt.Errorf("gltf: animation %d channel %d has no key times", ai, ci)
			}
			for k, t := range times {
				if math.IsNaN(float64(t)) || math.IsInf(float64(t), 0) || t < 0 {
					return fmt.Errorf("gltf: animation %d channel %d key time %d is not a finite non-negative number", ai, ci, k)
				}
				if k > 0 && t <= times[k-1] {
					return fmt.Errorf("gltf: animation %d channel %d key times are not strictly increasing", ai, ci)
				}
			}

			outAcc := d.Accessors[*rs.Output]
			if err := chargeDecodedBytes(decodedBytes, uint64(outAcc.Count)*uint64(outAcc.Type.Components())*4); err != nil {
				return err
			}
			values, err := d.readFloats(*rs.Output)
			if err != nil {
				return fmt.Errorf("gltf: animation %d channel %d output: %w", ai, ci, err)
			}

			keyMult := 1
			if interp == InterpolationCubicSpline {
				keyMult = 3
			}

			var stride int
			switch path {
			case PathTranslation, PathScale:
				if outAcc.Type != AccessorVec3 {
					return fmt.Errorf("gltf: animation %d channel %d %s output must be VEC3, got %s", ai, ci, path, outAcc.Type)
				}
				stride = 3
				if len(values) != len(times)*keyMult*stride {
					return fmt.Errorf("gltf: animation %d channel %d output length mismatch (%d vs %d)", ai, ci, len(values), len(times)*keyMult*stride)
				}
			case PathRotation:
				if outAcc.Type != AccessorVec4 {
					return fmt.Errorf("gltf: animation %d channel %d rotation output must be VEC4, got %s", ai, ci, outAcc.Type)
				}
				stride = 4
				if len(values) != len(times)*keyMult*stride {
					return fmt.Errorf("gltf: animation %d channel %d output length mismatch (%d vs %d)", ai, ci, len(values), len(times)*keyMult*stride)
				}
			case PathWeights:
				if outAcc.Type != AccessorScalar {
					return fmt.Errorf("gltf: animation %d channel %d weights output must be SCALAR, got %s", ai, ci, outAcc.Type)
				}
				if len(times) > 0 {
					denom := len(times) * keyMult
					if len(values) == 0 || len(values)%denom != 0 {
						return fmt.Errorf("gltf: animation %d channel %d weights output length %d not divisible by %d", ai, ci, len(values), denom)
					}
					stride = len(values) / denom
				}
			}

			for _, t := range times {
				if t > anim.Duration {
					anim.Duration = t
				}
			}

			anim.Channels = append(anim.Channels, AnimationChannel{
				TargetNode:    targetNode,
				Path:          path,
				Interpolation: interp,
				Times:         times,
				Values:        values,
				Stride:        stride,
			})
		}
		d.Animations[ai] = anim
	}
	return nil
}

// --- Typed Accessor Decoding ---

func align4(n int) int {
	return (n + 3) &^ 3
}

func elementByteSize(accType AccessorType, compSize int) int {
	switch accType {
	case AccessorMat2:
		return 2 * align4(2*compSize)
	case AccessorMat3:
		return 3 * align4(3*compSize)
	default:
		return accType.Components() * compSize
	}
}

func componentByteOffset(accType AccessorType, compSize, compIdx int) int {
	switch accType {
	case AccessorMat2:
		col := compIdx / 2
		row := compIdx % 2
		return col*align4(2*compSize) + row*compSize
	case AccessorMat3:
		col := compIdx / 3
		row := compIdx % 3
		return col*align4(3*compSize) + row*compSize
	default:
		return compIdx * compSize
	}
}

func (d *Document) resolveAccessor(accessorIndex int) (*Accessor, []byte, int, int, error) {
	if accessorIndex < 0 || accessorIndex >= len(d.Accessors) {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor index %d out of range [0, %d)", accessorIndex, len(d.Accessors))
	}
	acc := &d.Accessors[accessorIndex]
	compSize := acc.ComponentType.ByteSize()
	if compSize == 0 {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d invalid componentType %d", accessorIndex, acc.ComponentType)
	}
	elemSize := elementByteSize(acc.Type, compSize)
	if elemSize == 0 {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d invalid type %q", accessorIndex, acc.Type)
	}
	if acc.Count < 0 {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d negative count %d", accessorIndex, acc.Count)
	}

	if acc.decoded != nil {
		if uint64(len(acc.decoded)) != uint64(acc.Count)*uint64(elemSize) {
			return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d decoded data does not match count %d", accessorIndex, acc.Count)
		}
		return acc, acc.decoded, elemSize, elemSize, nil
	}

	if acc.BufferView < 0 {
		if uint64(acc.Count) > maxZeroInitAccessorBytes/uint64(elemSize) {
			return nil, nil, 0, 0, fmt.Errorf("gltf: zero-initialized accessor %d count %d exceeds safety limit", accessorIndex, acc.Count)
		}
		return acc, nil, 0, elemSize, nil
	}

	if acc.BufferView >= len(d.BufferViews) {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d bufferView %d out of range", accessorIndex, acc.BufferView)
	}
	bv := d.BufferViews[acc.BufferView]
	if bv.Buffer < 0 || bv.Buffer >= len(d.Buffers) {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d buffer %d out of range", accessorIndex, bv.Buffer)
	}
	stride := elemSize
	if bv.ByteStride > 0 {
		if bv.ByteStride < elemSize {
			return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d byteStride %d < element size %d", accessorIndex, bv.ByteStride, elemSize)
		}
		stride = bv.ByteStride
	}

	if acc.ByteOffset < 0 || acc.ByteOffset > bv.ByteLength {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d byteOffset %d out of bufferView bounds %d", accessorIndex, acc.ByteOffset, bv.ByteLength)
	}
	avail := uint64(bv.ByteLength - acc.ByteOffset)
	if acc.Count > 0 {
		if uint64(elemSize) > avail {
			return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d exceeds bufferView bounds", accessorIndex)
		}
		maxAdditional := (avail - uint64(elemSize)) / uint64(stride)
		if uint64(acc.Count-1) > maxAdditional {
			return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d count %d exceeds bufferView bounds", accessorIndex, acc.Count)
		}
	}

	buf := d.Buffers[bv.Buffer].Data
	start := bv.ByteOffset + acc.ByteOffset
	end := bv.ByteOffset + bv.ByteLength
	if start < 0 || end > len(buf) || start > end {
		return nil, nil, 0, 0, fmt.Errorf("gltf: accessor %d exceeds buffer bounds", accessorIndex)
	}
	return acc, buf[start:end], stride, elemSize, nil
}

func (d *Document) applySparse(acc *Accessor, elemSize int, fn func(dstIdx int, elem []byte) error) error {
	if acc.Sparse == nil || acc.Sparse.Count == 0 {
		return nil
	}
	sp := acc.Sparse
	if sp.Count < 0 || sp.Count > acc.Count {
		return fmt.Errorf("gltf: sparse count %d out of range [0, %d]", sp.Count, acc.Count)
	}

	// Resolve sparse indices buffer.
	if sp.Indices.BufferView < 0 || sp.Indices.BufferView >= len(d.BufferViews) {
		return fmt.Errorf("gltf: sparse indices bufferView %d out of range", sp.Indices.BufferView)
	}
	idxBV := d.BufferViews[sp.Indices.BufferView]
	idxBuf := d.Buffers[idxBV.Buffer].Data
	idxCompSize := sp.Indices.ComponentType.ByteSize()
	if idxCompSize == 0 {
		return fmt.Errorf("gltf: invalid sparse index componentType %d", sp.Indices.ComponentType)
	}
	if sp.Indices.ByteOffset < 0 || sp.Indices.ByteOffset > idxBV.ByteLength {
		return errors.New("gltf: sparse indices byteOffset out of bounds")
	}
	idxAvail := uint64(idxBV.ByteLength - sp.Indices.ByteOffset)
	if uint64(sp.Count) > idxAvail/uint64(idxCompSize) {
		return errors.New("gltf: sparse indices exceed bufferView bounds")
	}
	idxSlice := idxBuf[idxBV.ByteOffset+sp.Indices.ByteOffset : idxBV.ByteOffset+idxBV.ByteLength]

	// Resolve sparse values buffer.
	if sp.Values.BufferView < 0 || sp.Values.BufferView >= len(d.BufferViews) {
		return fmt.Errorf("gltf: sparse values bufferView %d out of range", sp.Values.BufferView)
	}
	valBV := d.BufferViews[sp.Values.BufferView]
	valBuf := d.Buffers[valBV.Buffer].Data
	if sp.Values.ByteOffset < 0 || sp.Values.ByteOffset > valBV.ByteLength {
		return errors.New("gltf: sparse values byteOffset out of bounds")
	}
	valAvail := uint64(valBV.ByteLength - sp.Values.ByteOffset)
	if uint64(sp.Count) > valAvail/uint64(elemSize) {
		return errors.New("gltf: sparse values exceed bufferView bounds")
	}
	valSlice := valBuf[valBV.ByteOffset+sp.Values.ByteOffset : valBV.ByteOffset+valBV.ByteLength]

	var prevIdx uint32
	for k := range sp.Count {
		var idx uint32
		off := k * idxCompSize
		switch sp.Indices.ComponentType {
		case ComponentUnsignedByte:
			idx = uint32(idxSlice[off])
		case ComponentUnsignedShort:
			idx = uint32(binary.LittleEndian.Uint16(idxSlice[off : off+2]))
		case ComponentUnsignedInt:
			idx = binary.LittleEndian.Uint32(idxSlice[off : off+4])
		default:
			return fmt.Errorf("gltf: unsupported sparse index componentType %d", sp.Indices.ComponentType)
		}
		if uint64(idx) >= uint64(acc.Count) {
			return fmt.Errorf("gltf: sparse index %d out of range [0, %d)", idx, acc.Count)
		}
		// The spec requires strictly increasing sparse indices; duplicates or reordering would make the substituted values depend on application order (#546).
		if k > 0 && idx <= prevIdx {
			return fmt.Errorf("gltf: sparse indices must strictly increase (index %d after %d)", idx, prevIdx)
		}
		prevIdx = idx
		elem := valSlice[k*elemSize : (k+1)*elemSize]
		if err := fn(int(idx), elem); err != nil {
			return err
		}
	}
	return nil
}

func readFloatComponent(b []byte, ct ComponentType, normalized bool) float32 {
	switch ct {
	case ComponentFloat:
		return math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
	case ComponentByte:
		v := int8(b[0])
		if normalized {
			return max(float32(v)/127.0, -1.0)
		}
		return float32(v)
	case ComponentUnsignedByte:
		v := b[0]
		if normalized {
			return float32(v) / 255.0
		}
		return float32(v)
	case ComponentShort:
		v := int16(binary.LittleEndian.Uint16(b[:2]))
		if normalized {
			return max(float32(v)/32767.0, -1.0)
		}
		return float32(v)
	case ComponentUnsignedShort:
		v := binary.LittleEndian.Uint16(b[:2])
		if normalized {
			return float32(v) / 65535.0
		}
		return float32(v)
	case ComponentUnsignedInt:
		v := binary.LittleEndian.Uint32(b[:4])
		if normalized {
			return float32(float64(v) / 4294967295.0)
		}
		return float32(v)
	default:
		return 0
	}
}

// readFloats decodes any accessor into a flat slice of float32 components,
// applying normalized integer unpacking and sparse overrides.
func (d *Document) readFloats(accessorIndex int) ([]float32, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	nComp := acc.Type.Components()
	compSize := acc.ComponentType.ByteSize()
	out := make([]float32, acc.Count*nComp)

	readElem := func(dstIdx int, elem []byte) {
		base := dstIdx * nComp
		for c := range nComp {
			off := componentByteOffset(acc.Type, compSize, c)
			out[base+c] = readFloatComponent(elem[off:], acc.ComponentType, acc.Normalized)
		}
	}

	if data != nil {
		for i := range acc.Count {
			readElem(i, data[i*stride:i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		readElem(dstIdx, elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadScalar decodes a SCALAR accessor as []float32, supporting all glTF
// component types, normalized unpacking, byteStride, and sparse accessors.
func (d *Document) ReadScalar(accessorIndex int) ([]float32, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorScalar {
		return nil, fmt.Errorf("gltf: accessor %d expected SCALAR, got %s", accessorIndex, acc.Type)
	}
	out := make([]float32, acc.Count)
	if data != nil {
		for i := range acc.Count {
			out[i] = readFloatComponent(data[i*stride:], acc.ComponentType, acc.Normalized)
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = readFloatComponent(elem, acc.ComponentType, acc.Normalized)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadVec2 decodes a VEC2 accessor as []math3d.Vec2.
func (d *Document) ReadVec2(accessorIndex int) ([]math3d.Vec2, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorVec2 {
		return nil, fmt.Errorf("gltf: accessor %d expected VEC2, got %s", accessorIndex, acc.Type)
	}
	cs := acc.ComponentType.ByteSize()
	out := make([]math3d.Vec2, acc.Count)
	decode := func(elem []byte) math3d.Vec2 {
		return math3d.Vec2{
			X: readFloatComponent(elem[0:], acc.ComponentType, acc.Normalized),
			Y: readFloatComponent(elem[cs:], acc.ComponentType, acc.Normalized),
		}
	}
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadVec3 decodes a VEC3 accessor as []math3d.Vec3.
func (d *Document) ReadVec3(accessorIndex int) ([]math3d.Vec3, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorVec3 {
		return nil, fmt.Errorf("gltf: accessor %d expected VEC3, got %s", accessorIndex, acc.Type)
	}
	cs := acc.ComponentType.ByteSize()
	out := make([]math3d.Vec3, acc.Count)
	decode := func(elem []byte) math3d.Vec3 {
		return math3d.Vec3{
			X: readFloatComponent(elem[0:], acc.ComponentType, acc.Normalized),
			Y: readFloatComponent(elem[cs:], acc.ComponentType, acc.Normalized),
			Z: readFloatComponent(elem[2*cs:], acc.ComponentType, acc.Normalized),
		}
	}
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadVec4 decodes a VEC4 accessor as []math3d.Vec4.
func (d *Document) ReadVec4(accessorIndex int) ([]math3d.Vec4, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorVec4 {
		return nil, fmt.Errorf("gltf: accessor %d expected VEC4, got %s", accessorIndex, acc.Type)
	}
	cs := acc.ComponentType.ByteSize()
	out := make([]math3d.Vec4, acc.Count)
	decode := func(elem []byte) math3d.Vec4 {
		return math3d.Vec4{
			X: readFloatComponent(elem[0:], acc.ComponentType, acc.Normalized),
			Y: readFloatComponent(elem[cs:], acc.ComponentType, acc.Normalized),
			Z: readFloatComponent(elem[2*cs:], acc.ComponentType, acc.Normalized),
			W: readFloatComponent(elem[3*cs:], acc.ComponentType, acc.Normalized),
		}
	}
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadMat4 decodes a MAT4 accessor as column-major []math3d.Mat4.
func (d *Document) ReadMat4(accessorIndex int) ([]math3d.Mat4, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorMat4 {
		return nil, fmt.Errorf("gltf: accessor %d expected MAT4, got %s", accessorIndex, acc.Type)
	}
	cs := acc.ComponentType.ByteSize()
	out := make([]math3d.Mat4, acc.Count)
	decode := func(elem []byte) math3d.Mat4 {
		var m math3d.Mat4
		for k := range 16 {
			m[k] = readFloatComponent(elem[k*cs:], acc.ComponentType, acc.Normalized)
		}
		return m
	}
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadIndices decodes a SCALAR index accessor (UNSIGNED_BYTE, UNSIGNED_SHORT,
// or UNSIGNED_INT) as []uint32.
func (d *Document) ReadIndices(accessorIndex int) ([]uint32, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorScalar {
		return nil, fmt.Errorf("gltf: index accessor %d expected SCALAR, got %s", accessorIndex, acc.Type)
	}
	switch acc.ComponentType {
	case ComponentUnsignedByte, ComponentUnsignedShort, ComponentUnsignedInt:
	default:
		return nil, fmt.Errorf("gltf: index accessor %d unsupported componentType %d", accessorIndex, acc.ComponentType)
	}

	decode := func(elem []byte) uint32 {
		switch acc.ComponentType {
		case ComponentUnsignedByte:
			return uint32(elem[0])
		case ComponentUnsignedShort:
			return uint32(binary.LittleEndian.Uint16(elem[:2]))
		default:
			return binary.LittleEndian.Uint32(elem[:4])
		}
	}

	out := make([]uint32, acc.Count)
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadJoints decodes a VEC4 JOINTS_n accessor (UNSIGNED_BYTE or UNSIGNED_SHORT)
// as [][4]uint16.
func (d *Document) ReadJoints(accessorIndex int) ([][4]uint16, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorVec4 {
		return nil, fmt.Errorf("gltf: joints accessor %d expected VEC4, got %s", accessorIndex, acc.Type)
	}
	switch acc.ComponentType {
	case ComponentUnsignedByte, ComponentUnsignedShort:
	default:
		return nil, fmt.Errorf("gltf: joints accessor %d unsupported componentType %d", accessorIndex, acc.ComponentType)
	}

	decode := func(elem []byte) [4]uint16 {
		if acc.ComponentType == ComponentUnsignedByte {
			return [4]uint16{uint16(elem[0]), uint16(elem[1]), uint16(elem[2]), uint16(elem[3])}
		}
		return [4]uint16{
			binary.LittleEndian.Uint16(elem[0:2]),
			binary.LittleEndian.Uint16(elem[2:4]),
			binary.LittleEndian.Uint16(elem[4:6]),
			binary.LittleEndian.Uint16(elem[6:8]),
		}
	}

	out := make([][4]uint16, acc.Count)
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadWeights decodes a VEC4 WEIGHTS_n accessor (FLOAT, normalized
// UNSIGNED_BYTE, or normalized UNSIGNED_SHORT) as []math3d.Vec4.
func (d *Document) ReadWeights(accessorIndex int) ([]math3d.Vec4, error) {
	acc, data, stride, elemSize, err := d.resolveAccessor(accessorIndex)
	if err != nil {
		return nil, err
	}
	if acc.Type != AccessorVec4 {
		return nil, fmt.Errorf("gltf: weights accessor %d expected VEC4, got %s", accessorIndex, acc.Type)
	}
	switch acc.ComponentType {
	case ComponentFloat, ComponentUnsignedByte, ComponentUnsignedShort:
	default:
		return nil, fmt.Errorf("gltf: weights accessor %d unsupported componentType %d", accessorIndex, acc.ComponentType)
	}
	// Per glTF 2.0 spec, integer WEIGHTS_n accessors are normalized to [0, 1].
	normalized := acc.Normalized || acc.ComponentType != ComponentFloat
	cs := acc.ComponentType.ByteSize()
	decode := func(elem []byte) math3d.Vec4 {
		return math3d.Vec4{
			X: readFloatComponent(elem[0:], acc.ComponentType, normalized),
			Y: readFloatComponent(elem[cs:], acc.ComponentType, normalized),
			Z: readFloatComponent(elem[2*cs:], acc.ComponentType, normalized),
			W: readFloatComponent(elem[3*cs:], acc.ComponentType, normalized),
		}
	}
	out := make([]math3d.Vec4, acc.Count)
	if data != nil {
		for i := range acc.Count {
			out[i] = decode(data[i*stride : i*stride+elemSize])
		}
	}
	if err := d.applySparse(acc, elemSize, func(dstIdx int, elem []byte) error {
		out[dstIdx] = decode(elem)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// cloneExtensions deep-copies each extension payload so a Document never
// aliases the decoder's memory.
func cloneExtensions(in map[string]json.RawMessage) map[string]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for name, payload := range in {
		out[name] = cloneRawMessage(payload)
	}
	return out
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	return cp
}

// --- Raw JSON schema types ---

type rawGLTF struct {
	Asset              rawAsset                   `json:"asset"`
	ExtensionsUsed     []string                   `json:"extensionsUsed"`
	ExtensionsRequired []string                   `json:"extensionsRequired"`
	Scene              *int                       `json:"scene"`
	Scenes             []rawScene                 `json:"scenes"`
	Nodes              []rawNode                  `json:"nodes"`
	Cameras            []rawCamera                `json:"cameras"`
	Buffers            []rawBuffer                `json:"buffers"`
	BufferViews        []rawBufferView            `json:"bufferViews"`
	Accessors          []rawAccessor              `json:"accessors"`
	Samplers           []rawSampler               `json:"samplers"`
	Images             []rawImage                 `json:"images"`
	Textures           []rawTexture               `json:"textures"`
	Materials          []rawMaterial              `json:"materials"`
	Meshes             []rawMesh                  `json:"meshes"`
	Skins              []rawSkin                  `json:"skins"`
	Animations         []rawAnimation             `json:"animations"`
	Extensions         map[string]json.RawMessage `json:"extensions"`
	Extras             json.RawMessage            `json:"extras"`
}

type rawAsset struct {
	Version    string          `json:"version"`
	MinVersion string          `json:"minVersion"`
	Generator  string          `json:"generator"`
	Copyright  string          `json:"copyright"`
	Extras     json.RawMessage `json:"extras"`
}

type rawScene struct {
	Name   string          `json:"name"`
	Nodes  []int           `json:"nodes"`
	Extras json.RawMessage `json:"extras"`
}

type rawNode struct {
	Name        string                     `json:"name"`
	Children    []int                      `json:"children"`
	Matrix      []float32                  `json:"matrix"`
	Translation []float32                  `json:"translation"`
	Rotation    []float32                  `json:"rotation"`
	Scale       []float32                  `json:"scale"`
	Mesh        *int                       `json:"mesh"`
	Skin        *int                       `json:"skin"`
	Camera      *int                       `json:"camera"`
	Weights     []float32                  `json:"weights"`
	Extras      json.RawMessage            `json:"extras"`
	Extensions  map[string]json.RawMessage `json:"extensions"`
}

type rawCamera struct {
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Perspective  *rawCameraPerspective  `json:"perspective"`
	Orthographic *rawCameraOrthographic `json:"orthographic"`
	Extras       json.RawMessage        `json:"extras"`
}

type rawCameraPerspective struct {
	AspectRatio float32 `json:"aspectRatio"`
	YFOV        float32 `json:"yfov"`
	ZNear       float32 `json:"znear"`
	ZFar        float32 `json:"zfar"`
}

type rawCameraOrthographic struct {
	XMag  float32 `json:"xmag"`
	YMag  float32 `json:"ymag"`
	ZNear float32 `json:"znear"`
	ZFar  float32 `json:"zfar"`
}

type rawBuffer struct {
	Name       string                     `json:"name"`
	URI        string                     `json:"uri"`
	ByteLength int                        `json:"byteLength"`
	Extras     json.RawMessage            `json:"extras"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

type rawBufferView struct {
	Name       string                     `json:"name"`
	Buffer     int                        `json:"buffer"`
	ByteOffset int                        `json:"byteOffset"`
	ByteLength int                        `json:"byteLength"`
	ByteStride int                        `json:"byteStride"`
	Target     int                        `json:"target"`
	Extras     json.RawMessage            `json:"extras"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

type rawAccessor struct {
	Name          string             `json:"name"`
	BufferView    *int               `json:"bufferView"`
	ByteOffset    int                `json:"byteOffset"`
	ComponentType int                `json:"componentType"`
	Normalized    bool               `json:"normalized"`
	Count         int                `json:"count"`
	Type          string             `json:"type"`
	Min           []float32          `json:"min"`
	Max           []float32          `json:"max"`
	Sparse        *rawAccessorSparse `json:"sparse"`
	Extras        json.RawMessage    `json:"extras"`
}

type rawAccessorSparse struct {
	Count   int                      `json:"count"`
	Indices rawAccessorSparseIndices `json:"indices"`
	Values  rawAccessorSparseValues  `json:"values"`
}

type rawAccessorSparseIndices struct {
	BufferView    int `json:"bufferView"`
	ByteOffset    int `json:"byteOffset"`
	ComponentType int `json:"componentType"`
}

type rawAccessorSparseValues struct {
	BufferView int `json:"bufferView"`
	ByteOffset int `json:"byteOffset"`
}

type rawSampler struct {
	Name      string          `json:"name"`
	MagFilter int             `json:"magFilter"`
	MinFilter int             `json:"minFilter"`
	WrapS     *int            `json:"wrapS"`
	WrapT     *int            `json:"wrapT"`
	Extras    json.RawMessage `json:"extras"`
}

type rawImage struct {
	Name       string          `json:"name"`
	URI        string          `json:"uri"`
	MimeType   string          `json:"mimeType"`
	BufferView *int            `json:"bufferView"`
	Extras     json.RawMessage `json:"extras"`
}

type rawTexture struct {
	Name       string                     `json:"name"`
	Sampler    *int                       `json:"sampler"`
	Source     *int                       `json:"source"`
	Extras     json.RawMessage            `json:"extras"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

type rawTextureInfo struct {
	Index      *int                       `json:"index"`
	TexCoord   int                        `json:"texCoord"`
	Scale      *float32                   `json:"scale"`
	Strength   *float32                   `json:"strength"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

type rawTextureTransform struct {
	Offset   json.RawMessage `json:"offset"`
	Rotation json.RawMessage `json:"rotation"`
	Scale    json.RawMessage `json:"scale"`
	TexCoord json.RawMessage `json:"texCoord"`
}

type rawPBR struct {
	BaseColorFactor          *[4]float32     `json:"baseColorFactor"`
	BaseColorTexture         *rawTextureInfo `json:"baseColorTexture"`
	MetallicFactor           *float32        `json:"metallicFactor"`
	RoughnessFactor          *float32        `json:"roughnessFactor"`
	MetallicRoughnessTexture *rawTextureInfo `json:"metallicRoughnessTexture"`
}

type rawMaterial struct {
	Name                 string                     `json:"name"`
	PBRMetallicRoughness *rawPBR                    `json:"pbrMetallicRoughness"`
	NormalTexture        *rawTextureInfo            `json:"normalTexture"`
	OcclusionTexture     *rawTextureInfo            `json:"occlusionTexture"`
	EmissiveTexture      *rawTextureInfo            `json:"emissiveTexture"`
	EmissiveFactor       *[3]float32                `json:"emissiveFactor"`
	AlphaMode            string                     `json:"alphaMode"`
	AlphaCutoff          *float32                   `json:"alphaCutoff"`
	DoubleSided          bool                       `json:"doubleSided"`
	Extensions           map[string]json.RawMessage `json:"extensions"`
	Extras               json.RawMessage            `json:"extras"`
}

type rawVariantsExt struct {
	Variants []json.RawMessage `json:"variants"`
}

type rawVariant struct {
	Name   *string         `json:"name"`
	Extras json.RawMessage `json:"extras"`
}

type rawPrimitiveVariantsExt struct {
	Mappings []json.RawMessage `json:"mappings"`
}

type rawVariantMapping struct {
	Material *int   `json:"material"`
	Variants []*int `json:"variants"`
}

type rawMesh struct {
	Name       string          `json:"name"`
	Primitives []rawPrimitive  `json:"primitives"`
	Weights    []float32       `json:"weights"`
	Extras     json.RawMessage `json:"extras"`
}

type rawPrimitive struct {
	Mode       *int                       `json:"mode"`
	Indices    *int                       `json:"indices"`
	Material   *int                       `json:"material"`
	Attributes map[string]int             `json:"attributes"`
	Targets    []map[string]int           `json:"targets"`
	Extensions map[string]json.RawMessage `json:"extensions"`
	Extras     json.RawMessage            `json:"extras"`
}

type rawSkin struct {
	Name                string          `json:"name"`
	Joints              []int           `json:"joints"`
	Skeleton            *int            `json:"skeleton"`
	InverseBindMatrices *int            `json:"inverseBindMatrices"`
	Extras              json.RawMessage `json:"extras"`
}

type rawAnimation struct {
	Name     string           `json:"name"`
	Channels []rawAnimChannel `json:"channels"`
	Samplers []rawAnimSampler `json:"samplers"`
	Extras   json.RawMessage  `json:"extras"`
}

type rawAnimChannel struct {
	Sampler *int          `json:"sampler"`
	Target  rawAnimTarget `json:"target"`
}

type rawAnimTarget struct {
	Node *int   `json:"node"`
	Path string `json:"path"`
}

type rawAnimSampler struct {
	Input         *int   `json:"input"`
	Output        *int   `json:"output"`
	Interpolation string `json:"interpolation"`
}

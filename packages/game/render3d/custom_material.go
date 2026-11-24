package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"image/color"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// CustomMaterialGroup is the bind group slot (@group(1)) that holds a custom material's uniform buffer and texture/sampler bindings alongside [CameraGroup] (@group(0)). It is [MaterialGroup], the slot every material in this package binds at.
const CustomMaterialGroup = MaterialGroup

// CustomMaterialUniformBinding is the @binding index inside [CustomMaterialGroup]
// used for the generated MaterialParams uniform struct when a custom material
// declares at least one non-texture parameter.
const CustomMaterialUniformBinding = 0

// ErrInvalidCustomMaterial is wrapped by every error returned when validating, compiling, or updating a [CustomMaterial]. Those errors also match [ErrInvalidMaterial].
var ErrInvalidCustomMaterial = errors.New("render3d: invalid custom material")

// CustomMaterialError reports a validation, shader-compilation, or parameter update error for a [CustomMaterial]. It always matches [ErrInvalidCustomMaterial] and [ErrInvalidMaterial] via errors.Is and unwraps to Err when an underlying error is present.
type CustomMaterialError struct {
	Material string
	Param    string
	Line     int
	Column   int
	Message  string
	Err      error
}

// Error formats the custom material diagnostic with optional material name,
// parameter name, and 1-based shader source line/column.
func (e *CustomMaterialError) Error() string {
	var b strings.Builder
	b.WriteString("render3d: custom material")
	if e.Material != "" {
		fmt.Fprintf(&b, " %q", e.Material)
	}
	if e.Param != "" {
		fmt.Fprintf(&b, " param %q", e.Param)
	}
	if e.Line > 0 {
		if e.Column > 0 {
			fmt.Fprintf(&b, " (%d:%d)", e.Line, e.Column)
		} else {
			fmt.Fprintf(&b, " (%d)", e.Line)
		}
	}
	b.WriteString(": ")
	if e.Message != "" {
		b.WriteString(e.Message)
	} else if e.Err != nil {
		b.WriteString(e.Err.Error())
	} else {
		b.WriteString("invalid custom material")
	}
	return b.String()
}

// Unwrap returns the underlying error, if any.
func (e *CustomMaterialError) Unwrap() error { return e.Err }

// Is reports that every CustomMaterialError matches [ErrInvalidCustomMaterial] and [ErrInvalidMaterial].
func (e *CustomMaterialError) Is(target error) bool {
	return target == ErrInvalidCustomMaterial || target == ErrInvalidMaterial
}

// ShaderLanguage identifies the source language of a [CustomShaderDesc].
type ShaderLanguage uint8

const (
	// ShaderWGSL compiles WGSL vertex and fragment stages directly, with
	// optional auto-generated @group(1) MaterialParams and texture bindings.
	ShaderWGSL ShaderLanguage = iota
	// ShaderKage translates a Kage surface shader (declared package-level var
	// uniforms plus func Surface(...) vec4 or func Fragment(...) vec4) into
	// portable WGSL targeting every NSGo GPU backend.
	ShaderKage
)

// String returns the canonical lowercase name of l.
func (l ShaderLanguage) String() string {
	switch l {
	case ShaderWGSL:
		return "wgsl"
	case ShaderKage:
		return "kage"
	default:
		return fmt.Sprintf("ShaderLanguage(%d)", l)
	}
}

// Valid reports whether l is a recognized shader language.
func (l ShaderLanguage) Valid() bool {
	return l == ShaderWGSL || l == ShaderKage
}

// ShaderParamKind identifies the type of a declared custom material parameter.
type ShaderParamKind uint8

const (
	paramKindInvalid ShaderParamKind = iota
	ParamFloat
	ParamVec2
	ParamVec3
	ParamVec4
	ParamColor
	ParamInt
	ParamBool
	ParamTexture
)

// String returns the canonical name of k.
func (k ShaderParamKind) String() string {
	switch k {
	case ParamFloat:
		return "float"
	case ParamVec2:
		return "vec2"
	case ParamVec3:
		return "vec3"
	case ParamVec4:
		return "vec4"
	case ParamColor:
		return "color"
	case ParamInt:
		return "int"
	case ParamBool:
		return "bool"
	case ParamTexture:
		return "texture"
	default:
		return fmt.Sprintf("ShaderParamKind(%d)", k)
	}
}

// Valid reports whether k is a supported parameter kind.
func (k ShaderParamKind) Valid() bool {
	return k >= ParamFloat && k <= ParamTexture
}

// IsUniform reports whether k is stored in the std140 uniform buffer rather
// than bound as a separate texture/sampler pair.
func (k ShaderParamKind) IsUniform() bool {
	return k >= ParamFloat && k <= ParamBool
}

// WGSLType returns the portable WGSL type emitted for k in MaterialParams or
// texture bindings. ParamBool uses u32 (0 or 1) because WGSL uniform buffers
// forbid host-unshareable bool fields.
func (k ShaderParamKind) WGSLType() string {
	switch k {
	case ParamFloat:
		return "f32"
	case ParamVec2:
		return "vec2<f32>"
	case ParamVec3:
		return "vec3<f32>"
	case ParamVec4, ParamColor:
		return "vec4<f32>"
	case ParamInt:
		return "i32"
	case ParamBool:
		return "u32"
	case ParamTexture:
		return "texture_2d<f32>"
	default:
		return ""
	}
}

// UniformLayout returns the WGSL/std140 alignment and byte size of k inside a uniform struct, or (0, 0) for non-uniform kinds such as [ParamTexture]. A [ParamVec3] is 12 bytes, but [ComputeUniformLayout] refuses a 4-byte parameter right after one, so the 4 bytes that follow it stay padding on every backend.
func (k ShaderParamKind) UniformLayout() (align, size int) {
	switch k {
	case ParamFloat, ParamInt, ParamBool:
		return 4, 4
	case ParamVec2:
		return 8, 8
	case ParamVec3:
		return 16, 12
	case ParamVec4, ParamColor:
		return 16, 16
	default:
		return 0, 0
	}
}

// ShaderParamDesc declares one typed parameter of a [CustomMaterial].
//
// Uniform parameters are packed in declared order into a std140 uniform struct
// at @group(1) @binding(0). Each [ParamTexture] parameter allocates a
// consecutive (texture_2d<f32>, sampler) pair at @group(1) named <Name> and
// <Name>_sampler.
type ShaderParamDesc struct {
	Name    string
	Kind    ShaderParamKind
	Default any      // optional; nil uses zero (or opaque white for ParamColor and 1x1 white fallback for ParamTexture)
	Min     *float64 // optional lower bound for numeric parameters (ParamFloat, ParamVec2..4, ParamInt)
	Max     *float64 // optional upper bound for numeric parameters (ParamFloat, ParamVec2..4, ParamInt)
}

// CustomParamLayout records the computed GPU location of one declared parameter.
type CustomParamLayout struct {
	Name           string
	Kind           ShaderParamKind
	Offset         int    // byte offset in uniform buffer (0 for ParamTexture)
	Align          int    // std140 alignment in bytes (0 for ParamTexture)
	Size           int    // byte size in uniform buffer (0 for ParamTexture)
	TextureBinding uint32 // @binding index in CustomMaterialGroup (ParamTexture only)
	SamplerBinding uint32 // @binding index in CustomMaterialGroup (ParamTexture only)
}

// CustomUniformLayout holds the deterministic std140 uniform layout and
// texture/sampler binding indices computed for a list of [ShaderParamDesc].
type CustomUniformLayout struct {
	Params      []CustomParamLayout
	Offsets     map[string]int
	UniformSize int // total uniform buffer size in bytes (multiple of 16, or 0 if no uniform params)
	HasUniforms bool
	TextureNum  int
}

// ComputeUniformLayout validates params and computes their deterministic
// WGSL/std140 uniform offsets and @group(1) texture/sampler bindings in
// declared order.
func ComputeUniformLayout(params []ShaderParamDesc) (CustomUniformLayout, error) {
	return computeCustomLayout("", params)
}

func computeCustomLayout(label string, params []ShaderParamDesc) (CustomUniformLayout, error) {
	seen := make(map[string]struct{}, len(params))
	texNames := make(map[string]struct{})
	hasUniforms := false
	for _, p := range params {
		if err := validateParamDesc(label, p); err != nil {
			return CustomUniformLayout{}, err
		}
		if _, dup := seen[p.Name]; dup {
			return CustomUniformLayout{}, &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  "duplicate parameter name",
			}
		}
		seen[p.Name] = struct{}{}
		if p.Kind.IsUniform() {
			hasUniforms = true
		} else if p.Kind == ParamTexture {
			texNames[p.Name] = struct{}{}
		}
	}
	for name := range texNames {
		if _, clash := seen[name+"_sampler"]; clash {
			return CustomUniformLayout{}, &CustomMaterialError{
				Material: label,
				Param:    name,
				Message:  fmt.Sprintf("auto-generated sampler name %q collides with another parameter", name+"_sampler"),
			}
		}
	}

	out := CustomUniformLayout{
		Params:      make([]CustomParamLayout, len(params)),
		Offsets:     make(map[string]int),
		HasUniforms: hasUniforms,
	}
	nextBinding := uint32(0)
	if hasUniforms {
		nextBinding = CustomMaterialUniformBinding + 1
	}

	offset := 0
	prevVec3 := ""
	for i, p := range params {
		if p.Kind.IsUniform() {
			align, size := p.Kind.UniformLayout()
			// WGSL packs a 4-byte member into the last 4 bytes of a preceding vec3, but NSGo's Metal backend declares the struct with float3 members, which take 16 bytes, so every member after that would be read from the wrong offset on macOS and iOS.
			if prevVec3 != "" && align == 4 {
				return CustomUniformLayout{}, &CustomMaterialError{
					Material: label,
					Param:    p.Name,
					Message:  fmt.Sprintf("a %s parameter cannot directly follow vec3 parameter %q, because Metal pads vec3 to 16 bytes; declare it before the vec3 or after a vec2/vec4 parameter, or make %q a ParamVec4", p.Kind, prevVec3, prevVec3),
				}
			}
			prevVec3 = ""
			if p.Kind == ParamVec3 {
				prevVec3 = p.Name
			}
			offset = alignUp(offset, align)
			out.Params[i] = CustomParamLayout{
				Name:   p.Name,
				Kind:   p.Kind,
				Offset: offset,
				Align:  align,
				Size:   size,
			}
			out.Offsets[p.Name] = offset
			offset += size
		} else if p.Kind == ParamTexture {
			out.Params[i] = CustomParamLayout{
				Name:           p.Name,
				Kind:           p.Kind,
				TextureBinding: nextBinding,
				SamplerBinding: nextBinding + 1,
			}
			nextBinding += 2
			out.TextureNum++
		}
	}
	if hasUniforms {
		out.UniformSize = alignUp(offset, 16)
	}
	return out, nil
}

// CustomShaderDesc configures a [CustomMaterial] compiled on a [gpu.Device].
type CustomShaderDesc struct {
	Label         string
	Language      ShaderLanguage
	Source        string
	VertexEntry   string // defaults to "vs_main"
	FragmentEntry string // defaults to "fs_main" (or "Surface" / "Fragment" for ShaderKage)
	Params        []ShaderParamDesc

	// CameraLayout optionally supplies the @group(0) bind group layout (such as
	// CameraBuffer.Layout()). When non-nil, @group(0) is bound to CameraWGSL and
	// the generated preamble prepends CameraWGSL automatically unless Source
	// already declares @group(0).
	CameraLayout *gpu.BindGroupLayout

	// ManualBindings disables automatic generation of the @group(1)
	// MaterialParams uniform struct and texture/sampler declarations for
	// ShaderWGSL when the shader source declares them explicitly.
	ManualBindings bool

	// Pipeline render state.
	Blend        *gpu.BlendState   // nil defaults to gpu.BlendOpaque()
	Cull         gpu.CullMode      // zero value is gpu.CullNone
	FrontFace    gpu.FrontFace     // zero value is gpu.FrontCCW
	DepthWrite   *bool             // nil defaults to true when DepthFormat != gpu.FormatUndefined
	DepthCompare gpu.CompareFunc   // zero value defaults to gpu.CompareLess
	ColorFormat  gpu.TextureFormat // zero value defaults to dev.SurfaceFormat()
	DepthFormat  gpu.TextureFormat // FormatUndefined disables depth testing
	SampleCount  int               // 0 defaults to 1
	Retained     bool              // opt in to automatic restoration across device loss
}

// CustomShaderWGSL validates desc and returns the portable WGSL source that
// [NewCustomMaterial] compiles (translating Kage surface shaders or prepending
// auto-generated @group(1) declarations for WGSL).
func CustomShaderWGSL(desc CustomShaderDesc) (string, error) {
	compiled, err := prepareCustomShader(desc)
	if err != nil {
		return "", err
	}
	return compiled.wgsl, nil
}

// CompileCheck validates source, lang, and params, generates portable WGSL,
// compiles it via [gpu.CompileWGSL], and verifies cross-backend pipeline
// lowering (MSL, HLSL, GLSL 3.30, GLSL ES 3.00 / WebGL 2, and WGSL) using an
// in-memory pipeline cache on a headless software device.
func CompileCheck(source string, lang ShaderLanguage, params []ShaderParamDesc) error {
	return CompileCheckDesc(CustomShaderDesc{
		Label:    "compile-check",
		Language: lang,
		Source:   source,
		Params:   params,
	})
}

// CompileCheckDesc validates desc and verifies full shader and cross-backend
// pipeline compilation without requiring an active [gpu.View].
func CompileCheckDesc(desc CustomShaderDesc) error {
	prep, err := prepareCustomShader(desc)
	if err != nil {
		return err
	}
	mod, err := compilePreparedModule(desc.Label, prep)
	if err != nil {
		return err
	}

	depthFmt := desc.DepthFormat
	sr := gpu.NewSoftwareRenderer(1, 1, depthFmt)
	defer sr.Close()
	dev := sr.Device()
	dev.SetPipelineCache(gpu.NewMemoryPipelineCache())

	var g0 *gpu.BindGroupLayout
	if prep.usesCameraGroup {
		g0, err = dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
			Label: "render3d.custom.camera",
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
	} else {
		g0, err = dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
			Label: "render3d.custom.empty",
		})
	}
	if err != nil {
		return &CustomMaterialError{Material: desc.Label, Message: "create group 0 layout failed", Err: err}
	}

	layouts := []*gpu.BindGroupLayout{g0}
	if matEntries := materialLayoutEntries(prep.layout); len(matEntries) > 0 {
		g1, err := dev.CreateBindGroupLayoutDesc(nil, gpu.BindGroupLayoutDesc{
			Label:   "render3d.custom.params",
			Entries: matEntries,
		})
		if err != nil {
			return &CustomMaterialError{Material: desc.Label, Message: "create material layout failed", Err: err}
		}
		layouts = append(layouts, g1)
	}

	pdesc := buildCustomPipelineDesc(dev, desc, prep, mod, layouts)
	if _, err := dev.CreatePipeline(nil, pdesc); err != nil {
		return &CustomMaterialError{Material: desc.Label, Message: "pipeline validation failed", Err: err}
	}
	return nil
}

// CustomMaterial is a compiled WGSL or Kage 3D material with declared,
// strongly-typed uniform and texture parameters on an NSGo [gpu.Device].
//
// Its uniform buffer, fallback 1x1 textures, default sampler, bind group, and
// pipeline belong to the [core.Scope] passed to [NewCustomMaterial] and are
// released automatically on scope disposal or earlier via [CustomMaterial.Close].
// Call [CustomMaterial.Sync] (or [CustomMaterial.Update]) before drawing
// whenever parameters have been modified, then bind with [CustomMaterial.Bind]
// or draw directly with [CustomMaterial.Draw].
type CustomMaterial struct {
	label string
	scope *core.Scope

	params  []ShaderParamDesc
	layout  CustomUniformLayout
	byName  map[string]int // param name -> index in params / layout.Params
	wgslSrc string

	pipeline        *gpu.Pipeline
	ownGroup0Layout *gpu.BindGroupLayout // non-nil only when CustomMaterial created group 0 itself
	matLayout       *gpu.BindGroupLayout
	bindGroup       *gpu.BindGroup
	uniformBuf      *gpu.Buffer
	defaultSamp     *GPUSampler
	fallbackTex     []*GPUTexture // parallel to texture slot index

	uniformData    []byte
	texSlots       map[string]int // texture param name -> slot index
	boundTex       []*GPUTexture
	boundSamp      []*GPUSampler
	bindGroups     []customBindGroup // recently used texture/sampler combinations, most recent last; holds bindGroup
	uniformDirty   bool
	bindGroupDirty bool
	retained       bool
	closed         bool
}

// customBindGroupCacheSize is how many texture/sampler combinations a [CustomMaterial] keeps a bind group for, so alternating between a few textures reuses bind groups instead of creating one per change.
const customBindGroupCacheSize = 8

// customBindGroup is the bind group a [CustomMaterial] built for one combination of bound textures and samplers.
type customBindGroup struct {
	tex  []*GPUTexture
	samp []*GPUSampler
	bg   *gpu.BindGroup
}

// NewCustomMaterial validates desc, compiles the WGSL or Kage shader, computes
// the std140 uniform layout, allocates GPU resources scoped to cx, and builds
// the render pipeline on dev.
func NewCustomMaterial(cx *core.Scope, dev *gpu.Device, desc CustomShaderDesc) (*CustomMaterial, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	prep, err := prepareCustomShader(desc)
	if err != nil {
		return nil, err
	}
	mod, err := compilePreparedModule(desc.Label, prep)
	if err != nil {
		return nil, err
	}

	m := &CustomMaterial{
		label:    desc.Label,
		scope:    cx,
		params:   slices.Clone(prep.params),
		layout:   prep.layout,
		byName:   make(map[string]int, len(prep.params)),
		wgslSrc:  prep.wgsl,
		texSlots: make(map[string]int, prep.layout.TextureNum),
		retained: desc.Retained,
	}
	for i, p := range m.params {
		m.byName[p.Name] = i
	}
	if m.layout.UniformSize > 0 {
		m.uniformData = make([]byte, m.layout.UniformSize)
	}

	cleanupOnErr := func() {
		_ = m.Close()
	}

	// Populate initial uniform defaults and prepare fallback 1x1 textures.
	texSlot := 0
	if m.layout.TextureNum > 0 {
		m.fallbackTex = make([]*GPUTexture, m.layout.TextureNum)
		m.boundTex = make([]*GPUTexture, m.layout.TextureNum)
		m.boundSamp = make([]*GPUSampler, m.layout.TextureNum)
		sampOpts := SamplerLinearRepeat()
		sampOpts.Label = customMatLabel(desc.Label, "default-sampler")
		sampOpts.Retained = desc.Retained
		m.defaultSamp, err = NewGPUSampler(cx, dev, sampOpts)
		if err != nil {
			cleanupOnErr()
			return nil, err
		}
	}

	for i, p := range m.params {
		if p.Kind.IsUniform() {
			if err := m.applyDefaultUniform(i, p); err != nil {
				cleanupOnErr()
				return nil, err
			}
			continue
		}
		if p.Kind == ParamTexture {
			fbColor, err := parseFallbackTexel(desc.Label, p)
			if err != nil {
				cleanupOnErr()
				return nil, err
			}
			fb, err := UploadTexturePixels(cx, dev, PixelBuffer{
				Width:  1,
				Height: 1,
				Format: gpu.FormatRGBA8Unorm,
				Pixels: fbColor[:],
			}, TextureUpload{
				Label:    customMatLabel(desc.Label, "fallback-"+p.Name),
				Retained: desc.Retained,
			})
			if err != nil {
				cleanupOnErr()
				return nil, err
			}
			m.texSlots[p.Name] = texSlot
			m.fallbackTex[texSlot] = fb
			m.boundTex[texSlot] = fb
			m.boundSamp[texSlot] = m.defaultSamp
			texSlot++
		}
	}

	if m.layout.UniformSize > 0 {
		m.uniformBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    customMatLabel(desc.Label, "uniforms"),
			Size:     uint64(m.layout.UniformSize),
			Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
			Data:     slices.Clone(m.uniformData),
			Retained: desc.Retained,
		})
		if err != nil {
			cleanupOnErr()
			return nil, err
		}
	}

	var group0Layout *gpu.BindGroupLayout
	if desc.CameraLayout != nil {
		if !desc.CameraLayout.Valid() {
			cleanupOnErr()
			return nil, &CustomMaterialError{
				Material: desc.Label,
				Message:  "CameraLayout is not valid",
			}
		}
		group0Layout = desc.CameraLayout
	} else if prep.usesCameraGroup {
		m.ownGroup0Layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label: customMatLabel(desc.Label, "camera-layout"),
			Entries: []gpu.BindGroupLayoutEntry{{
				Binding:    0,
				Visibility: gpu.StageVertex | gpu.StageFragment,
				Type:       gpu.BindingUniform,
			}},
		})
		if err != nil {
			cleanupOnErr()
			return nil, err
		}
		group0Layout = m.ownGroup0Layout
	}

	matEntries := materialLayoutEntries(m.layout)
	var layouts []*gpu.BindGroupLayout
	if len(matEntries) > 0 {
		if group0Layout == nil {
			m.ownGroup0Layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
				Label: customMatLabel(desc.Label, "empty-group0"),
			})
			if err != nil {
				cleanupOnErr()
				return nil, err
			}
			group0Layout = m.ownGroup0Layout
		}
		m.matLayout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
			Label:   customMatLabel(desc.Label, "params-layout"),
			Entries: matEntries,
		})
		if err != nil {
			cleanupOnErr()
			return nil, err
		}
		if err = m.selectBindGroup(dev); err != nil {
			cleanupOnErr()
			return nil, err
		}
		layouts = []*gpu.BindGroupLayout{group0Layout, m.matLayout}
	} else if group0Layout != nil {
		layouts = []*gpu.BindGroupLayout{group0Layout}
	}

	pdesc := buildCustomPipelineDesc(dev, desc, prep, mod, layouts)
	m.pipeline, err = dev.CreatePipeline(cx, pdesc)
	if err != nil {
		cleanupOnErr()
		return nil, &CustomMaterialError{
			Material: desc.Label,
			Message:  "create pipeline failed",
			Err:      err,
		}
	}

	m.uniformDirty = false
	m.bindGroupDirty = false
	return m, nil
}

// SetFloat sets a declared [ParamFloat] parameter.
func (m *CustomMaterial) SetFloat(name string, v float32) error {
	p, pl, err := m.lookupParam(name, ParamFloat)
	if err != nil {
		return err
	}
	if err := checkFiniteRange(m.label, p, float64(v)); err != nil {
		return err
	}
	bits := math.Float32bits(v)
	if binary.LittleEndian.Uint32(m.uniformData[pl.Offset:]) != bits {
		binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], bits)
		m.uniformDirty = true
	}
	return nil
}

// SetVec2 sets a declared [ParamVec2] parameter.
func (m *CustomMaterial) SetVec2(name string, v math3d.Vec2) error {
	p, pl, err := m.lookupParam(name, ParamVec2)
	if err != nil {
		return err
	}
	if err := checkFiniteRange(m.label, p, float64(v.X), float64(v.Y)); err != nil {
		return err
	}
	m.putFloat32At(pl.Offset, v.X)
	m.putFloat32At(pl.Offset+4, v.Y)
	return nil
}

// SetVec3 sets a declared [ParamVec3] parameter.
func (m *CustomMaterial) SetVec3(name string, v math3d.Vec3) error {
	p, pl, err := m.lookupParam(name, ParamVec3)
	if err != nil {
		return err
	}
	if err := checkFiniteRange(m.label, p, float64(v.X), float64(v.Y), float64(v.Z)); err != nil {
		return err
	}
	m.putFloat32At(pl.Offset, v.X)
	m.putFloat32At(pl.Offset+4, v.Y)
	m.putFloat32At(pl.Offset+8, v.Z)
	return nil
}

// SetVec4 sets a declared [ParamVec4] or [ParamColor] parameter.
func (m *CustomMaterial) SetVec4(name string, v math3d.Vec4) error {
	if m == nil || m.closed {
		return &CustomMaterialError{Message: "material is nil or closed"}
	}
	idx, ok := m.byName[name]
	if !ok {
		return &CustomMaterialError{Material: m.label, Param: name, Message: "unknown parameter"}
	}
	p := m.params[idx]
	if p.Kind != ParamVec4 && p.Kind != ParamColor {
		return &CustomMaterialError{
			Material: m.label,
			Param:    name,
			Message:  fmt.Sprintf("parameter has kind %s, cannot set as vec4", p.Kind),
		}
	}
	if err := checkFiniteRange(m.label, p, float64(v.X), float64(v.Y), float64(v.Z), float64(v.W)); err != nil {
		return err
	}
	pl := m.layout.Params[idx]
	m.putFloat32At(pl.Offset, v.X)
	m.putFloat32At(pl.Offset+4, v.Y)
	m.putFloat32At(pl.Offset+8, v.Z)
	m.putFloat32At(pl.Offset+12, v.W)
	return nil
}

// SetColor sets a declared [ParamColor] parameter from an sRGB [core.Color],
// normalized into [0, 1] RGBA float32 components.
func (m *CustomMaterial) SetColor(name string, c core.Color) error {
	_, pl, err := m.lookupParam(name, ParamColor)
	if err != nil {
		return err
	}
	rgba := colorToVec4(c)
	m.putFloat32At(pl.Offset, rgba.X)
	m.putFloat32At(pl.Offset+4, rgba.Y)
	m.putFloat32At(pl.Offset+8, rgba.Z)
	m.putFloat32At(pl.Offset+12, rgba.W)
	return nil
}

// SetInt sets a declared [ParamInt] parameter.
func (m *CustomMaterial) SetInt(name string, v int32) error {
	p, pl, err := m.lookupParam(name, ParamInt)
	if err != nil {
		return err
	}
	if err := checkFiniteRange(m.label, p, float64(v)); err != nil {
		return err
	}
	u := uint32(v)
	if binary.LittleEndian.Uint32(m.uniformData[pl.Offset:]) != u {
		binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], u)
		m.uniformDirty = true
	}
	return nil
}

// SetBool sets a declared [ParamBool] parameter (encoded as u32 1 or 0).
func (m *CustomMaterial) SetBool(name string, v bool) error {
	_, pl, err := m.lookupParam(name, ParamBool)
	if err != nil {
		return err
	}
	var u uint32
	if v {
		u = 1
	}
	if binary.LittleEndian.Uint32(m.uniformData[pl.Offset:]) != u {
		binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], u)
		m.uniformDirty = true
	}
	return nil
}

// SetTexture binds tex and samp to a declared [ParamTexture] parameter. Passing
// a nil tex resets the slot to its 1x1 fallback texture; passing a nil samp
// uses the material's default linear-repeat sampler. Call [CustomMaterial.Sync]
// before drawing to upload the updated bind group.
func (m *CustomMaterial) SetTexture(name string, tex *GPUTexture, samp *GPUSampler) error {
	_, _, err := m.lookupParam(name, ParamTexture)
	if err != nil {
		return err
	}
	slot := m.texSlots[name]
	if tex != nil && !tex.Valid() {
		return &CustomMaterialError{
			Material: m.label,
			Param:    name,
			Message:  "texture is not valid",
			Err:      ErrInvalidTexture,
		}
	}
	if samp != nil && !samp.Valid() {
		return &CustomMaterialError{
			Material: m.label,
			Param:    name,
			Message:  "sampler is not valid",
			Err:      ErrInvalidSampler,
		}
	}
	targetTex := tex
	if targetTex == nil {
		targetTex = m.fallbackTex[slot]
	}
	targetSamp := samp
	if targetSamp == nil {
		targetSamp = m.defaultSamp
	}
	if m.boundTex[slot] != targetTex || m.boundSamp[slot] != targetSamp {
		m.boundTex[slot] = targetTex
		m.boundSamp[slot] = targetSamp
		m.bindGroupDirty = true
	}
	return nil
}

// ApplyMap validates and applies a batch of named parameters atomically (in
// deterministic sorted key order). If any key is unknown or has an incompatible
// type or out-of-range value, no parameter is mutated and an error wrapping
// [ErrInvalidCustomMaterial] is returned.
func (m *CustomMaterial) ApplyMap(values map[string]any) error {
	if m == nil || m.closed {
		return &CustomMaterialError{Message: "material is nil or closed"}
	}
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	type pendingOp struct {
		idx   int
		kind  ShaderParamKind
		f4    [4]float32
		i32   int32
		b     bool
		tex   *GPUTexture
		isTex bool
	}
	ops := make([]pendingOp, 0, len(keys))
	for _, k := range keys {
		idx, ok := m.byName[k]
		if !ok {
			return &CustomMaterialError{Material: m.label, Param: k, Message: "unknown parameter"}
		}
		p := m.params[idx]
		val := values[k]
		op := pendingOp{idx: idx, kind: p.Kind}
		switch p.Kind {
		case ParamFloat:
			f, err := coerceFloat32(m.label, p, val)
			if err != nil {
				return err
			}
			op.f4[0] = f
		case ParamVec2:
			v, err := coerceVec2(m.label, p, val)
			if err != nil {
				return err
			}
			op.f4[0], op.f4[1] = v.X, v.Y
		case ParamVec3:
			v, err := coerceVec3(m.label, p, val)
			if err != nil {
				return err
			}
			op.f4[0], op.f4[1], op.f4[2] = v.X, v.Y, v.Z
		case ParamVec4:
			v, err := coerceVec4(m.label, p, val)
			if err != nil {
				return err
			}
			op.f4[0], op.f4[1], op.f4[2], op.f4[3] = v.X, v.Y, v.Z, v.W
		case ParamColor:
			v, err := coerceColor(m.label, p, val)
			if err != nil {
				return err
			}
			op.f4[0], op.f4[1], op.f4[2], op.f4[3] = v.X, v.Y, v.Z, v.W
		case ParamInt:
			iv, err := coerceInt32(m.label, p, val)
			if err != nil {
				return err
			}
			op.i32 = iv
		case ParamBool:
			bv, ok := val.(bool)
			if !ok {
				return &CustomMaterialError{
					Material: m.label,
					Param:    p.Name,
					Message:  fmt.Sprintf("expected bool, got %T", val),
				}
			}
			op.b = bv
		case ParamTexture:
			if val == nil {
				op.isTex = true
			} else if tex, ok := val.(*GPUTexture); ok {
				if tex != nil && !tex.Valid() {
					return &CustomMaterialError{
						Material: m.label,
						Param:    p.Name,
						Message:  "texture is not valid",
						Err:      ErrInvalidTexture,
					}
				}
				op.tex = tex
				op.isTex = true
			} else {
				return &CustomMaterialError{
					Material: m.label,
					Param:    p.Name,
					Message:  fmt.Sprintf("expected *GPUTexture or nil, got %T", val),
				}
			}
		}
		ops = append(ops, op)
	}

	for _, op := range ops {
		pl := m.layout.Params[op.idx]
		switch op.kind {
		case ParamFloat:
			m.putFloat32At(pl.Offset, op.f4[0])
		case ParamVec2:
			m.putFloat32At(pl.Offset, op.f4[0])
			m.putFloat32At(pl.Offset+4, op.f4[1])
		case ParamVec3:
			m.putFloat32At(pl.Offset, op.f4[0])
			m.putFloat32At(pl.Offset+4, op.f4[1])
			m.putFloat32At(pl.Offset+8, op.f4[2])
		case ParamVec4, ParamColor:
			m.putFloat32At(pl.Offset, op.f4[0])
			m.putFloat32At(pl.Offset+4, op.f4[1])
			m.putFloat32At(pl.Offset+8, op.f4[2])
			m.putFloat32At(pl.Offset+12, op.f4[3])
		case ParamInt:
			u := uint32(op.i32)
			if binary.LittleEndian.Uint32(m.uniformData[pl.Offset:]) != u {
				binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], u)
				m.uniformDirty = true
			}
		case ParamBool:
			var u uint32
			if op.b {
				u = 1
			}
			if binary.LittleEndian.Uint32(m.uniformData[pl.Offset:]) != u {
				binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], u)
				m.uniformDirty = true
			}
		case ParamTexture:
			slot := m.texSlots[pl.Name]
			targetTex := op.tex
			if targetTex == nil {
				targetTex = m.fallbackTex[slot]
			}
			// Like SetTexture with a nil sampler, a texture from ApplyMap samples with the default sampler.
			if m.boundTex[slot] != targetTex || m.boundSamp[slot] != m.defaultSamp {
				m.boundTex[slot] = targetTex
				m.boundSamp[slot] = m.defaultSamp
				m.bindGroupDirty = true
			}
		}
	}
	return nil
}

// Sync uploads dirty uniform bytes to the GPU uniform buffer and, when texture bindings changed, switches to the @group(1) bind group for the new textures and samplers. The material keeps bind groups for its 8 most recently bound combinations, so alternating between a few textures (a flipbook, a ping-pong render target) creates no new GPU objects once each combination has been bound. When nothing is dirty it returns nil with zero allocations.
func (m *CustomMaterial) Sync(dev *gpu.Device) error {
	if m == nil || m.closed {
		return &CustomMaterialError{Message: "material is nil or closed"}
	}
	if !m.uniformDirty && !m.bindGroupDirty {
		return nil
	}
	if dev == nil {
		return gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if !m.Valid() {
		return &CustomMaterialError{Material: m.label, Message: "material resources are not valid"}
	}
	if m.uniformDirty && m.uniformBuf != nil {
		dev.WriteBuffer(m.uniformBuf, 0, m.uniformData)
		m.uniformDirty = false
	}
	if m.bindGroupDirty && m.matLayout != nil {
		if err := m.selectBindGroup(dev); err != nil {
			return err
		}
		m.bindGroupDirty = false
	}
	return nil
}

// selectBindGroup makes m.bindGroup the bind group for the currently bound textures and samplers, reusing a cached one when that combination was bound recently and creating it otherwise. Cached bind groups whose textures or samplers were closed are dropped, and the least recently used one is closed when the cache is full.
func (m *CustomMaterial) selectBindGroup(dev *gpu.Device) error {
	found := -1
	for i := 0; i < len(m.bindGroups); i++ {
		e := m.bindGroups[i]
		if !e.live() {
			_ = e.bg.Close()
			m.bindGroups = slices.Delete(m.bindGroups, i, i+1)
			i--
			continue
		}
		if slices.Equal(e.tex, m.boundTex) && slices.Equal(e.samp, m.boundSamp) {
			found = i
		}
	}
	if found >= 0 {
		e := m.bindGroups[found]
		copy(m.bindGroups[found:], m.bindGroups[found+1:])
		m.bindGroups[len(m.bindGroups)-1] = e
		m.bindGroup = e.bg
		return nil
	}
	bg, err := m.createBindGroup(dev)
	if err != nil {
		return err
	}
	if len(m.bindGroups) >= customBindGroupCacheSize {
		_ = m.bindGroups[0].bg.Close()
		m.bindGroups = slices.Delete(m.bindGroups, 0, 1)
	}
	m.bindGroups = append(m.bindGroups, customBindGroup{tex: slices.Clone(m.boundTex), samp: slices.Clone(m.boundSamp), bg: bg})
	m.bindGroup = bg
	return nil
}

// live reports whether e's bind group and every texture and sampler it references are still open.
func (e customBindGroup) live() bool {
	if !e.bg.Valid() {
		return false
	}
	for _, t := range e.tex {
		if !t.Valid() {
			return false
		}
	}
	for _, s := range e.samp {
		if !s.Valid() {
			return false
		}
	}
	return true
}

// Update is an alias for [CustomMaterial.Sync].
func (m *CustomMaterial) Update(dev *gpu.Device) error {
	return m.Sync(dev)
}

// Bind sets the material's pipeline and, when it declares parameters, binds its
// @group(1) bind group on pass. It records nothing when the material is invalid.
func (m *CustomMaterial) Bind(pass *gpu.RenderPass) {
	if pass == nil || !m.Valid() {
		return
	}
	pass.SetPipeline(m.pipeline)
	if m.bindGroup != nil {
		pass.SetBindGroup(CustomMaterialGroup, m.bindGroup)
	}
}

// Draw binds the material on pass and draws mesh. It records nothing if either
// the material or mesh is invalid.
func (m *CustomMaterial) Draw(pass *gpu.RenderPass, mesh *GPUMesh) {
	if pass == nil || !m.Valid() || !mesh.Valid() {
		return
	}
	m.Bind(pass)
	mesh.Draw(pass)
}

// ParamOffset returns the std140 byte offset of the uniform parameter named
// name, or (0, false) if absent or not a uniform parameter.
func (m *CustomMaterial) ParamOffset(name string) (int, bool) {
	if m == nil {
		return 0, false
	}
	off, ok := m.layout.Offsets[name]
	return off, ok
}

// ParamBinding returns the @group(1) texture and sampler binding numbers for
// the [ParamTexture] parameter named name.
func (m *CustomMaterial) ParamBinding(name string) (texBinding, samplerBinding uint32, ok bool) {
	if m == nil {
		return 0, 0, false
	}
	idx, found := m.byName[name]
	if !found || m.params[idx].Kind != ParamTexture {
		return 0, 0, false
	}
	pl := m.layout.Params[idx]
	return pl.TextureBinding, pl.SamplerBinding, true
}

// Params returns a copy of the material's declared parameters.
func (m *CustomMaterial) Params() []ShaderParamDesc {
	if m == nil {
		return nil
	}
	return slices.Clone(m.params)
}

// UniformSize returns the byte size of the material's std140 uniform buffer
// (a multiple of 16, or 0 when the material declares no uniform parameters).
func (m *CustomMaterial) UniformSize() int {
	if m == nil {
		return 0
	}
	return m.layout.UniformSize
}

// UniformBytes returns a copy of the current CPU-side std140 uniform buffer.
func (m *CustomMaterial) UniformBytes() []byte {
	if m == nil || len(m.uniformData) == 0 {
		return nil
	}
	return slices.Clone(m.uniformData)
}

// WGSL returns the final portable WGSL source compiled for this material.
func (m *CustomMaterial) WGSL() string {
	if m == nil {
		return ""
	}
	return m.wgslSrc
}

// Pipeline returns the material's compiled [gpu.Pipeline], or nil.
func (m *CustomMaterial) Pipeline() *gpu.Pipeline {
	if m == nil {
		return nil
	}
	return m.pipeline
}

// Layout returns the material's @group(1) [gpu.BindGroupLayout], or nil when
// the material declares no parameters.
func (m *CustomMaterial) Layout() *gpu.BindGroupLayout {
	if m == nil {
		return nil
	}
	return m.matLayout
}

// BindGroup returns the material's @group(1) [gpu.BindGroup], or nil when the
// material declares no parameters.
func (m *CustomMaterial) BindGroup() *gpu.BindGroup {
	if m == nil {
		return nil
	}
	return m.bindGroup
}

// Dirty reports whether parameter updates are waiting for [CustomMaterial.Sync].
func (m *CustomMaterial) Dirty() bool {
	if m == nil {
		return false
	}
	return m.uniformDirty || m.bindGroupDirty
}

// Valid reports whether the material's GPU pipeline, uniform buffer, and bind
// group are open and belong to a live device.
func (m *CustomMaterial) Valid() bool {
	if m == nil || m.closed || m.pipeline == nil || !m.pipeline.Valid() {
		return false
	}
	if m.uniformBuf != nil && !m.uniformBuf.Valid() {
		return false
	}
	if m.bindGroup != nil && !m.bindGroup.Valid() {
		return false
	}
	return true
}

// ByteSize returns the GPU memory the material owns, in bytes: its uniform buffer plus its 1x1 fallback textures, for [RenderStats] and [Budget] accounting. Textures bound with [CustomMaterial.SetTexture] belong to the caller and are not counted.
func (m *CustomMaterial) ByteSize() uint64 {
	if m == nil || m.closed {
		return 0
	}
	n := uint64(m.layout.UniformSize)
	for _, fb := range m.fallbackTex {
		n += fb.ByteSize()
	}
	return n
}

// Close releases every GPU resource owned by the material now rather than
// waiting for its owning scope to dispose. It is safe to call on a nil receiver
// or more than once.
func (m *CustomMaterial) Close() error {
	if m == nil || m.closed {
		return nil
	}
	m.closed = true
	var err error
	if m.pipeline != nil {
		err = errors.Join(err, m.pipeline.Close())
	}
	for _, e := range m.bindGroups {
		err = errors.Join(err, e.bg.Close())
	}
	m.bindGroups = nil
	if m.matLayout != nil {
		err = errors.Join(err, m.matLayout.Close())
	}
	if m.ownGroup0Layout != nil {
		err = errors.Join(err, m.ownGroup0Layout.Close())
	}
	if m.uniformBuf != nil {
		err = errors.Join(err, m.uniformBuf.Close())
	}
	for _, fb := range m.fallbackTex {
		if fb != nil {
			err = errors.Join(err, fb.Close())
		}
	}
	if m.defaultSamp != nil {
		err = errors.Join(err, m.defaultSamp.Close())
	}
	return err
}

func (m *CustomMaterial) putFloat32At(offset int, v float32) {
	bits := math.Float32bits(v)
	if binary.LittleEndian.Uint32(m.uniformData[offset:]) != bits {
		binary.LittleEndian.PutUint32(m.uniformData[offset:], bits)
		m.uniformDirty = true
	}
}

func (m *CustomMaterial) lookupParam(name string, want ShaderParamKind) (ShaderParamDesc, CustomParamLayout, error) {
	if m == nil || m.closed {
		return ShaderParamDesc{}, CustomParamLayout{}, &CustomMaterialError{Message: "material is nil or closed"}
	}
	idx, ok := m.byName[name]
	if !ok {
		return ShaderParamDesc{}, CustomParamLayout{}, &CustomMaterialError{
			Material: m.label,
			Param:    name,
			Message:  "unknown parameter",
		}
	}
	p := m.params[idx]
	if p.Kind != want {
		return ShaderParamDesc{}, CustomParamLayout{}, &CustomMaterialError{
			Material: m.label,
			Param:    name,
			Message:  fmt.Sprintf("parameter has kind %s, cannot set as %s", p.Kind, want),
		}
	}
	return p, m.layout.Params[idx], nil
}

func (m *CustomMaterial) applyDefaultUniform(idx int, p ShaderParamDesc) error {
	pl := m.layout.Params[idx]
	if p.Default == nil {
		if p.Kind == ParamColor {
			// Default color is opaque white (1, 1, 1, 1) so untinted shaders stay visible.
			m.putFloat32At(pl.Offset, 1)
			m.putFloat32At(pl.Offset+4, 1)
			m.putFloat32At(pl.Offset+8, 1)
			m.putFloat32At(pl.Offset+12, 1)
		}
		return nil
	}
	switch p.Kind {
	case ParamFloat:
		v, err := coerceFloat32(m.label, p, p.Default)
		if err != nil {
			return err
		}
		m.putFloat32At(pl.Offset, v)
	case ParamVec2:
		v, err := coerceVec2(m.label, p, p.Default)
		if err != nil {
			return err
		}
		m.putFloat32At(pl.Offset, v.X)
		m.putFloat32At(pl.Offset+4, v.Y)
	case ParamVec3:
		v, err := coerceVec3(m.label, p, p.Default)
		if err != nil {
			return err
		}
		m.putFloat32At(pl.Offset, v.X)
		m.putFloat32At(pl.Offset+4, v.Y)
		m.putFloat32At(pl.Offset+8, v.Z)
	case ParamVec4:
		v, err := coerceVec4(m.label, p, p.Default)
		if err != nil {
			return err
		}
		m.putFloat32At(pl.Offset, v.X)
		m.putFloat32At(pl.Offset+4, v.Y)
		m.putFloat32At(pl.Offset+8, v.Z)
		m.putFloat32At(pl.Offset+12, v.W)
	case ParamColor:
		v, err := coerceColor(m.label, p, p.Default)
		if err != nil {
			return err
		}
		m.putFloat32At(pl.Offset, v.X)
		m.putFloat32At(pl.Offset+4, v.Y)
		m.putFloat32At(pl.Offset+8, v.Z)
		m.putFloat32At(pl.Offset+12, v.W)
	case ParamInt:
		iv, err := coerceInt32(m.label, p, p.Default)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], uint32(iv))
	case ParamBool:
		bv, ok := p.Default.(bool)
		if !ok {
			return &CustomMaterialError{
				Material: m.label,
				Param:    p.Name,
				Message:  fmt.Sprintf("default must be bool, got %T", p.Default),
			}
		}
		if bv {
			binary.LittleEndian.PutUint32(m.uniformData[pl.Offset:], 1)
		}
	}
	return nil
}

func (m *CustomMaterial) createBindGroup(dev *gpu.Device) (*gpu.BindGroup, error) {
	var entries []gpu.BindGroupEntry
	if m.uniformBuf != nil {
		entries = append(entries, gpu.BindGroupEntry{
			Binding: CustomMaterialUniformBinding,
			Buffer:  m.uniformBuf,
			Size:    uint64(m.layout.UniformSize),
		})
	}
	for _, pl := range m.layout.Params {
		if pl.Kind != ParamTexture {
			continue
		}
		slot := m.texSlots[pl.Name]
		tex := m.boundTex[slot]
		if tex == nil {
			tex = m.fallbackTex[slot]
		}
		samp := m.boundSamp[slot]
		if samp == nil {
			samp = m.defaultSamp
		}
		entries = append(entries,
			gpu.BindGroupEntry{Binding: pl.TextureBinding, Texture: tex.Texture()},
			gpu.BindGroupEntry{Binding: pl.SamplerBinding, Sampler: samp.Sampler()},
		)
	}
	return dev.CreateBindGroupDesc(m.scope, gpu.BindGroupDesc{
		Label:   customMatLabel(m.label, "params"),
		Layout:  m.matLayout,
		Entries: entries,
	})
}

type preparedCustomShader struct {
	params          []ShaderParamDesc
	layout          CustomUniformLayout
	wgsl            string
	vertexEntry     string
	fragmentEntry   string
	preambleLines   int
	usesCameraGroup bool
}

func prepareCustomShader(desc CustomShaderDesc) (preparedCustomShader, error) {
	if !desc.Language.Valid() {
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Message:  fmt.Sprintf("unsupported shader language %s", desc.Language),
		}
	}
	if strings.TrimSpace(desc.Source) == "" {
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Message:  "empty shader source",
		}
	}
	if desc.SampleCount != 0 && desc.SampleCount != 1 && desc.SampleCount != 4 {
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Message:  fmt.Sprintf("unsupported sample count %d (expected 0, 1, or 4)", desc.SampleCount),
		}
	}

	switch desc.Language {
	case ShaderWGSL:
		return prepareWGSLShader(desc)
	case ShaderKage:
		return prepareKageShader(desc)
	default:
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Message:  fmt.Sprintf("unsupported shader language %s", desc.Language),
		}
	}
}

func prepareWGSLShader(desc CustomShaderDesc) (preparedCustomShader, error) {
	layout, err := computeCustomLayout(desc.Label, desc.Params)
	if err != nil {
		return preparedCustomShader{}, err
	}
	if err := validateParamDefaults(desc.Label, desc.Params); err != nil {
		return preparedCustomShader{}, err
	}

	vertEntry := desc.VertexEntry
	if vertEntry == "" {
		vertEntry = "vs_main"
	}
	fragEntry := desc.FragmentEntry
	if fragEntry == "" {
		fragEntry = "fs_main"
	}

	hasGroup0 := strings.Contains(desc.Source, "@group(0)")
	usesCamera := desc.CameraLayout != nil || hasGroup0

	var preamble strings.Builder
	if !desc.ManualBindings {
		if desc.CameraLayout != nil && !hasGroup0 {
			preamble.WriteString(CameraWGSL)
			preamble.WriteByte('\n')
		}
		writeMaterialWGSLBindings(&preamble, layout)
	}
	preStr := preamble.String()
	preambleLines := strings.Count(preStr, "\n")

	return preparedCustomShader{
		params:          slices.Clone(desc.Params),
		layout:          layout,
		wgsl:            preStr + desc.Source,
		vertexEntry:     vertEntry,
		fragmentEntry:   fragEntry,
		preambleLines:   preambleLines,
		usesCameraGroup: usesCamera,
	}, nil
}

func writeMaterialWGSLBindings(b *strings.Builder, layout CustomUniformLayout) {
	if layout.HasUniforms {
		b.WriteString("struct MaterialParams {\n")
		for _, pl := range layout.Params {
			if pl.Kind.IsUniform() {
				fmt.Fprintf(b, "\t%s: %s,\n", pl.Name, pl.Kind.WGSLType())
			}
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(b, "@group(%d) @binding(%d) var<uniform> material: MaterialParams;\n\n",
			CustomMaterialGroup, CustomMaterialUniformBinding)
	}
	for _, pl := range layout.Params {
		if pl.Kind != ParamTexture {
			continue
		}
		fmt.Fprintf(b, "@group(%d) @binding(%d) var %s: texture_2d<f32>;\n",
			CustomMaterialGroup, pl.TextureBinding, pl.Name)
		fmt.Fprintf(b, "@group(%d) @binding(%d) var %s_sampler: sampler;\n",
			CustomMaterialGroup, pl.SamplerBinding, pl.Name)
	}
	if layout.TextureNum > 0 {
		b.WriteByte('\n')
	}
}

func compilePreparedModule(label string, prep preparedCustomShader) (*gpu.ShaderModule, error) {
	mod, err := gpu.CompileShader(gpu.ShaderDesc{
		Label:  customMatLabel(label, "shader"),
		Source: prep.wgsl,
	})
	if err != nil {
		line, col, msg := parseWGSLDiag(err)
		if line > prep.preambleLines {
			line -= prep.preambleLines
		} else if prep.preambleLines > 0 {
			line = 0
			col = 0
		}
		return nil, &CustomMaterialError{
			Material: label,
			Line:     line,
			Column:   col,
			Message:  msg,
			Err:      err,
		}
	}
	if !mod.HasVertexEntry(prep.vertexEntry) {
		return nil, &CustomMaterialError{
			Material: label,
			Message:  fmt.Sprintf("shader does not define @vertex entry point %q", prep.vertexEntry),
		}
	}
	if !mod.HasFragmentEntry(prep.fragmentEntry) {
		return nil, &CustomMaterialError{
			Material: label,
			Message:  fmt.Sprintf("shader does not define @fragment entry point %q", prep.fragmentEntry),
		}
	}
	return mod, nil
}

func materialLayoutEntries(layout CustomUniformLayout) []gpu.BindGroupLayoutEntry {
	var entries []gpu.BindGroupLayoutEntry
	if layout.HasUniforms {
		entries = append(entries, gpu.BindGroupLayoutEntry{
			Binding:    CustomMaterialUniformBinding,
			Visibility: gpu.StageVertex | gpu.StageFragment,
			Type:       gpu.BindingUniform,
		})
	}
	for _, pl := range layout.Params {
		if pl.Kind != ParamTexture {
			continue
		}
		entries = append(entries,
			gpu.BindGroupLayoutEntry{
				Binding:    pl.TextureBinding,
				Visibility: gpu.StageFragment,
				Type:       gpu.BindingTexture,
			},
			gpu.BindGroupLayoutEntry{
				Binding:    pl.SamplerBinding,
				Visibility: gpu.StageFragment,
				Type:       gpu.BindingSampler,
			},
		)
	}
	return entries
}

func buildCustomPipelineDesc(dev *gpu.Device, desc CustomShaderDesc, prep preparedCustomShader, mod *gpu.ShaderModule, layouts []*gpu.BindGroupLayout) gpu.PipelineDesc {
	colorFmt := desc.ColorFormat
	if colorFmt == gpu.FormatUndefined {
		colorFmt = dev.SurfaceFormat()
	}
	blend := desc.Blend
	if blend == nil {
		blend = gpu.BlendOpaque()
	}
	pdesc := gpu.PipelineDesc{
		Label:         customMatLabel(desc.Label, "pipeline"),
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: prep.vertexEntry},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: prep.fragmentEntry},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{MeshVertexLayout()},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyTriangleList,
			FrontFace: desc.FrontFace,
			CullMode:  desc.Cull,
		},
		Targets: []gpu.ColorTargetState{{
			Format:    colorFmt,
			Blend:     blend,
			WriteMask: gpu.ColorWriteAll,
		}},
		SampleCount: max(desc.SampleCount, 1),
		Retained:    desc.Retained,
	}
	if desc.DepthFormat != gpu.FormatUndefined {
		depthWrite := true
		if desc.DepthWrite != nil {
			depthWrite = *desc.DepthWrite
		}
		depthCmp := desc.DepthCompare
		if depthCmp == gpu.CompareUndefined {
			depthCmp = gpu.CompareLess
		}
		pdesc.DepthStencil = &gpu.DepthStencilState{
			Format:       desc.DepthFormat,
			DepthWrite:   depthWrite,
			DepthCompare: depthCmp,
		}
	}
	return pdesc
}

// --- Kage surface shader translator (Go AST -> portable WGSL) ---

func prepareKageShader(desc CustomShaderDesc) (preparedCustomShader, error) {
	src := desc.Source
	lineOffset := 0
	if !kageHasPackageClause(src) {
		src = "package main\n" + src
		lineOffset = 1
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "material.kage", src, 0)
	if err != nil {
		line, col, msg := parseGoParseError(err, lineOffset)
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Line:     line,
			Column:   col,
			Message:  msg,
			Err:      err,
		}
	}
	if len(file.Imports) > 0 {
		pos := fset.Position(file.Imports[0].Pos())
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Line:     max(pos.Line-lineOffset, 1),
			Column:   pos.Column,
			Message:  "Kage surface shaders cannot use import declarations",
		}
	}

	// Collect package-level var uniforms, consts, and functions.
	type kageVar struct {
		name string
		kind ShaderParamKind
		pos  token.Pos
	}
	var (
		kageVars   []kageVar
		kageConsts []*ast.ValueSpec
		kageFuncs  []*ast.FuncDecl
		funcNames  = make(map[string]bool)
	)

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.VAR:
				for _, spec := range d.Specs {
					vs := spec.(*ast.ValueSpec)
					if len(vs.Values) > 0 {
						pos := fset.Position(vs.Pos())
						return preparedCustomShader{}, &CustomMaterialError{
							Material: desc.Label,
							Line:     max(pos.Line-lineOffset, 1),
							Column:   pos.Column,
							Message:  "Kage uniform var declarations cannot have initializers; supply defaults via ShaderParamDesc.Default",
						}
					}
					kind, ok := kageTypeToParamKind(vs.Type)
					if !ok {
						pos := fset.Position(vs.Type.Pos())
						return preparedCustomShader{}, &CustomMaterialError{
							Material: desc.Label,
							Line:     max(pos.Line-lineOffset, 1),
							Column:   pos.Column,
							Message:  "unsupported Kage uniform type (expected float, vec2, vec3, vec4, int, or bool)",
						}
					}
					for _, id := range vs.Names {
						kageVars = append(kageVars, kageVar{name: id.Name, kind: kind, pos: id.Pos()})
					}
				}
			case token.CONST:
				for _, spec := range d.Specs {
					kageConsts = append(kageConsts, spec.(*ast.ValueSpec))
				}
			default:
				pos := fset.Position(d.Pos())
				return preparedCustomShader{}, &CustomMaterialError{
					Material: desc.Label,
					Line:     max(pos.Line-lineOffset, 1),
					Column:   pos.Column,
					Message:  fmt.Sprintf("unsupported top-level %s declaration in Kage surface shader", d.Tok),
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil {
				pos := fset.Position(d.Pos())
				return preparedCustomShader{}, &CustomMaterialError{
					Material: desc.Label,
					Line:     max(pos.Line-lineOffset, 1),
					Column:   pos.Column,
					Message:  "methods are not supported in Kage surface shaders",
				}
			}
			kageFuncs = append(kageFuncs, d)
			funcNames[d.Name.Name] = true
		default:
			pos := fset.Position(decl.Pos())
			return preparedCustomShader{}, &CustomMaterialError{
				Material: desc.Label,
				Line:     max(pos.Line-lineOffset, 1),
				Column:   pos.Column,
				Message:  "unsupported top-level declaration in Kage surface shader",
			}
		}
	}

	// Reconcile Kage var declarations against desc.Params.
	params := slices.Clone(desc.Params)
	if len(params) == 0 && len(kageVars) > 0 {
		for _, kv := range kageVars {
			params = append(params, ShaderParamDesc{Name: kv.name, Kind: kv.kind})
		}
	} else {
		declared := make(map[string]ShaderParamDesc, len(params))
		for _, p := range params {
			declared[p.Name] = p
		}
		for _, kv := range kageVars {
			dp, ok := declared[kv.name]
			if !ok {
				pos := fset.Position(kv.pos)
				return preparedCustomShader{}, &CustomMaterialError{
					Material: desc.Label,
					Param:    kv.name,
					Line:     max(pos.Line-lineOffset, 1),
					Column:   pos.Column,
					Message:  "Kage var is not declared in CustomShaderDesc.Params",
				}
			}
			if !kageKindCompatible(kv.kind, dp.Kind) {
				pos := fset.Position(kv.pos)
				return preparedCustomShader{}, &CustomMaterialError{
					Material: desc.Label,
					Param:    kv.name,
					Line:     max(pos.Line-lineOffset, 1),
					Column:   pos.Column,
					Message:  fmt.Sprintf("Kage var type %s does not match declared param kind %s", kv.kind, dp.Kind),
				}
			}
		}
	}

	layout, err := computeCustomLayout(desc.Label, params)
	if err != nil {
		return preparedCustomShader{}, err
	}
	if err := validateParamDefaults(desc.Label, params); err != nil {
		return preparedCustomShader{}, err
	}

	// Find the entry function ("Surface" by default, or "Fragment").
	entryName := desc.FragmentEntry
	if entryName == "" {
		if funcNames["Surface"] {
			entryName = "Surface"
		} else if funcNames["Fragment"] {
			entryName = "Fragment"
		} else {
			entryName = "Surface"
		}
	}
	var entryFn *ast.FuncDecl
	for _, fn := range kageFuncs {
		if fn.Name.Name == entryName {
			entryFn = fn
			break
		}
	}
	if entryFn == nil {
		return preparedCustomShader{}, &CustomMaterialError{
			Material: desc.Label,
			Message:  fmt.Sprintf("Kage shader must define func %s(...) vec4", entryName),
		}
	}

	entryArgs, err := validateKageEntry(desc.Label, fset, lineOffset, entryFn)
	if err != nil {
		return preparedCustomShader{}, err
	}

	tr := &kageTranslator{
		label:      desc.Label,
		fset:       fset,
		lineOffset: lineOffset,
		uniforms:   make(map[string]ShaderParamKind, len(params)),
		texByName:  make(map[string]bool, layout.TextureNum),
		texOrder:   make([]string, 0, layout.TextureNum),
		helpers:    funcNames,
	}
	for _, p := range params {
		if p.Kind.IsUniform() {
			tr.uniforms[p.Name] = p.Kind
		} else if p.Kind == ParamTexture {
			tr.texByName[p.Name] = true
			tr.texOrder = append(tr.texOrder, p.Name)
		}
	}

	var out strings.Builder
	usesCamera := desc.CameraLayout != nil
	if usesCamera {
		out.WriteString(CameraWGSL)
		out.WriteByte('\n')
	}
	writeMaterialWGSLBindings(&out, layout)

	// Emit package-level consts.
	for _, cs := range kageConsts {
		if err := tr.emitConstSpec(&out, cs, ""); err != nil {
			return preparedCustomShader{}, err
		}
	}
	if len(kageConsts) > 0 {
		out.WriteByte('\n')
	}

	// Emit helper functions and the Surface/Fragment function itself.
	for _, fn := range kageFuncs {
		if err := tr.emitFunc(&out, fn); err != nil {
			return preparedCustomShader{}, err
		}
		out.WriteByte('\n')
	}

	// Emit portable 3D vertex/fragment stages wired to MeshVertexLayout.
	out.WriteString("struct _R3DVertexIn {\n")
	out.WriteString("\t@location(0) pos: vec3<f32>,\n")
	out.WriteString("\t@location(1) normal: vec3<f32>,\n")
	out.WriteString("\t@location(2) uv: vec2<f32>,\n")
	out.WriteString("}\n\n")

	out.WriteString("struct _R3DVertexOut {\n")
	out.WriteString("\t@builtin(position) clip: vec4<f32>,\n")
	out.WriteString("\t@location(0) world_pos: vec3<f32>,\n")
	out.WriteString("\t@location(1) normal: vec3<f32>,\n")
	out.WriteString("\t@location(2) uv: vec2<f32>,\n")
	out.WriteString("}\n\n")

	out.WriteString("@vertex\nfn vs_main(in: _R3DVertexIn) -> _R3DVertexOut {\n")
	out.WriteString("\tvar out: _R3DVertexOut;\n")
	if usesCamera {
		out.WriteString("\tout.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);\n")
	} else {
		out.WriteString("\tout.clip = vec4<f32>(in.pos, 1.0);\n")
	}
	out.WriteString("\tout.world_pos = in.pos;\n")
	out.WriteString("\tout.normal = in.normal;\n")
	out.WriteString("\tout.uv = in.uv;\n")
	out.WriteString("\treturn out;\n")
	out.WriteString("}\n\n")

	fmt.Fprintf(&out, "@fragment\nfn fs_main(in: _R3DVertexOut) -> @location(0) vec4<f32> {\n\treturn %s(%s);\n}\n",
		kageFuncWGSLName(entryFn.Name.Name), strings.Join(entryArgs, ", "))

	return preparedCustomShader{
		params:          params,
		layout:          layout,
		wgsl:            out.String(),
		vertexEntry:     "vs_main",
		fragmentEntry:   "fs_main",
		preambleLines:   0,
		usesCameraGroup: usesCamera,
	}, nil
}

func kageHasPackageClause(src string) bool {
	lines := strings.Split(src, "\n")
	inBlockComment := false
	for _, line := range lines {
		s := strings.TrimSpace(line)
		for s != "" {
			if inBlockComment {
				if idx := strings.Index(s, "*/"); idx >= 0 {
					s = strings.TrimSpace(s[idx+2:])
					inBlockComment = false
					continue
				}
				s = ""
				break
			}
			if strings.HasPrefix(s, "//") {
				s = ""
				break
			}
			if strings.HasPrefix(s, "/*") {
				s = s[2:]
				inBlockComment = true
				continue
			}
			return strings.HasPrefix(s, "package ") || strings.HasPrefix(s, "package\t")
		}
	}
	return false
}

func kageTypeToParamKind(expr ast.Expr) (ShaderParamKind, bool) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return paramKindInvalid, false
	}
	switch id.Name {
	case "float":
		return ParamFloat, true
	case "vec2":
		return ParamVec2, true
	case "vec3":
		return ParamVec3, true
	case "vec4":
		return ParamVec4, true
	case "int":
		return ParamInt, true
	case "bool":
		return ParamBool, true
	default:
		return paramKindInvalid, false
	}
}

func kageKindCompatible(kageKind, paramKind ShaderParamKind) bool {
	if kageKind == paramKind {
		return true
	}
	return kageKind == ParamVec4 && paramKind == ParamColor
}

func kageFuncWGSLName(name string) string {
	return "_kage_" + name
}

func validateKageEntry(label string, fset *token.FileSet, lineOffset int, fn *ast.FuncDecl) ([]string, error) {
	errAt := func(pos token.Pos, msg string) error {
		p := fset.Position(pos)
		return &CustomMaterialError{
			Material: label,
			Line:     max(p.Line-lineOffset, 1),
			Column:   p.Column,
			Message:  msg,
		}
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return nil, errAt(fn.Pos(), fmt.Sprintf("Kage entry %s must return vec4", fn.Name.Name))
	}
	if retID, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || retID.Name != "vec4" {
		return nil, errAt(fn.Type.Results.List[0].Type.Pos(), fmt.Sprintf("Kage entry %s must return vec4", fn.Name.Name))
	}

	var paramTypes []string
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			id, ok := field.Type.(*ast.Ident)
			if !ok {
				return nil, errAt(field.Type.Pos(), "unsupported parameter type in Kage entry point")
			}
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for range count {
				paramTypes = append(paramTypes, id.Name)
			}
		}
	}

	if fn.Name.Name == "Fragment" {
		switch len(paramTypes) {
		case 2:
			if paramTypes[0] == "vec4" && paramTypes[1] == "vec2" {
				return []string{"in.clip", "in.uv"}, nil
			}
		case 3:
			if paramTypes[0] == "vec4" && paramTypes[1] == "vec2" && paramTypes[2] == "vec4" {
				return []string{"in.clip", "in.uv", "vec4<f32>(1.0, 1.0, 1.0, 1.0)"}, nil
			}
		}
		return nil, errAt(fn.Pos(), "Kage Fragment signature must be (vec4, vec2) vec4 or (vec4, vec2, vec4) vec4")
	}

	switch len(paramTypes) {
	case 0:
		return nil, nil
	case 1:
		if paramTypes[0] == "vec2" {
			return []string{"in.uv"}, nil
		}
	case 2:
		if paramTypes[0] == "vec2" && paramTypes[1] == "vec3" {
			return []string{"in.uv", "in.normal"}, nil
		}
	case 3:
		if paramTypes[0] == "vec2" && paramTypes[1] == "vec3" && paramTypes[2] == "vec3" {
			return []string{"in.uv", "in.normal", "in.world_pos"}, nil
		}
	}
	return nil, errAt(fn.Pos(), fmt.Sprintf("Kage %s signature must be (), (vec2), (vec2, vec3), or (vec2, vec3, vec3) returning vec4", fn.Name.Name))
}

type kageTranslator struct {
	label      string
	fset       *token.FileSet
	lineOffset int
	uniforms   map[string]ShaderParamKind
	texByName  map[string]bool
	texOrder   []string
	helpers    map[string]bool
	scopes     []map[string]bool
}

func (tr *kageTranslator) pushScope() {
	tr.scopes = append(tr.scopes, make(map[string]bool))
}

func (tr *kageTranslator) popScope() {
	tr.scopes = tr.scopes[:len(tr.scopes)-1]
}

func (tr *kageTranslator) declareLocal(name string) {
	if len(tr.scopes) > 0 && name != "_" {
		tr.scopes[len(tr.scopes)-1][name] = true
	}
}

func (tr *kageTranslator) isLocal(name string) bool {
	for i := len(tr.scopes) - 1; i >= 0; i-- {
		if tr.scopes[i][name] {
			return true
		}
	}
	return false
}

func (tr *kageTranslator) errf(pos token.Pos, format string, args ...any) error {
	p := tr.fset.Position(pos)
	return &CustomMaterialError{
		Material: tr.label,
		Line:     max(p.Line-tr.lineOffset, 1),
		Column:   p.Column,
		Message:  fmt.Sprintf(format, args...),
	}
}

func (tr *kageTranslator) wgslType(expr ast.Expr) (string, error) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return "", tr.errf(expr.Pos(), "unsupported Kage type expression")
	}
	switch id.Name {
	case "float":
		return "f32", nil
	case "int":
		return "i32", nil
	case "bool":
		return "bool", nil
	case "vec2":
		return "vec2<f32>", nil
	case "vec3":
		return "vec3<f32>", nil
	case "vec4":
		return "vec4<f32>", nil
	case "ivec2":
		return "vec2<i32>", nil
	case "ivec3":
		return "vec3<i32>", nil
	case "ivec4":
		return "vec4<i32>", nil
	case "mat2":
		return "mat2x2<f32>", nil
	case "mat3":
		return "mat3x3<f32>", nil
	case "mat4":
		return "mat4x4<f32>", nil
	default:
		return "", tr.errf(expr.Pos(), "unsupported Kage type %q", id.Name)
	}
}

func (tr *kageTranslator) emitConstSpec(b *strings.Builder, cs *ast.ValueSpec, indent string) error {
	if len(cs.Names) != len(cs.Values) {
		return tr.errf(cs.Pos(), "const declaration must provide a value for each name")
	}
	var typStr string
	if cs.Type != nil {
		var err error
		typStr, err = tr.wgslType(cs.Type)
		if err != nil {
			return err
		}
	}
	for i, id := range cs.Names {
		val, err := tr.expr(cs.Values[i])
		if err != nil {
			return err
		}
		if typStr != "" {
			fmt.Fprintf(b, "%sconst %s: %s = %s;\n", indent, id.Name, typStr, val)
		} else {
			fmt.Fprintf(b, "%sconst %s = %s;\n", indent, id.Name, val)
		}
		tr.declareLocal(id.Name)
	}
	return nil
}

func (tr *kageTranslator) emitFunc(b *strings.Builder, fn *ast.FuncDecl) error {
	tr.pushScope()
	defer tr.popScope()

	var params []string
	if fn.Type.Params != nil {
		paramIdx := 0
		for _, field := range fn.Type.Params.List {
			wt, err := tr.wgslType(field.Type)
			if err != nil {
				return err
			}
			if len(field.Names) == 0 {
				pname := fmt.Sprintf("_p%d", paramIdx)
				paramIdx++
				params = append(params, fmt.Sprintf("%s: %s", pname, wt))
				continue
			}
			for _, id := range field.Names {
				pname := id.Name
				if pname == "_" {
					pname = fmt.Sprintf("_p%d", paramIdx)
					paramIdx++
				}
				tr.declareLocal(pname)
				params = append(params, fmt.Sprintf("%s: %s", pname, wt))
			}
		}
	}

	retStr := ""
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		if len(fn.Type.Results.List) != 1 || len(fn.Type.Results.List[0].Names) > 1 {
			return tr.errf(fn.Type.Results.Pos(), "Kage functions support at most one return value")
		}
		wt, err := tr.wgslType(fn.Type.Results.List[0].Type)
		if err != nil {
			return err
		}
		retStr = " -> " + wt
	}

	fmt.Fprintf(b, "fn %s(%s)%s {\n", kageFuncWGSLName(fn.Name.Name), strings.Join(params, ", "), retStr)
	for _, stmt := range fn.Body.List {
		if err := tr.emitStmt(b, stmt, "\t"); err != nil {
			return err
		}
	}
	b.WriteString("}\n")
	return nil
}

func (tr *kageTranslator) emitStmt(b *strings.Builder, stmt ast.Stmt, indent string) error {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if len(s.Results) == 0 {
			fmt.Fprintf(b, "%sreturn;\n", indent)
			return nil
		}
		if len(s.Results) != 1 {
			return tr.errf(s.Pos(), "multiple return values are not supported in Kage")
		}
		val, err := tr.expr(s.Results[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%sreturn %s;\n", indent, val)
		return nil

	case *ast.AssignStmt:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			return tr.errf(s.Pos(), "multi-value assignments are not supported in Kage")
		}
		rhs, err := tr.expr(s.Rhs[0])
		if err != nil {
			return err
		}
		if s.Tok == token.DEFINE {
			id, ok := s.Lhs[0].(*ast.Ident)
			if !ok {
				return tr.errf(s.Lhs[0].Pos(), "short variable declaration requires an identifier")
			}
			if id.Name == "_" {
				fmt.Fprintf(b, "%s_ = %s;\n", indent, rhs)
				return nil
			}
			tr.declareLocal(id.Name)
			fmt.Fprintf(b, "%svar %s = %s;\n", indent, id.Name, rhs)
			return nil
		}
		lhs, err := tr.expr(s.Lhs[0])
		if err != nil {
			return err
		}
		op, ok := kageAssignOp(s.Tok)
		if !ok {
			return tr.errf(s.TokPos, "unsupported assignment operator %s", s.Tok)
		}
		fmt.Fprintf(b, "%s%s %s %s;\n", indent, lhs, op, rhs)
		return nil

	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return tr.errf(s.Pos(), "unsupported local declaration")
		}
		switch gd.Tok {
		case token.CONST:
			for _, spec := range gd.Specs {
				if err := tr.emitConstSpec(b, spec.(*ast.ValueSpec), indent); err != nil {
					return err
				}
			}
			return nil
		case token.VAR:
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				var wt string
				if vs.Type != nil {
					var err error
					wt, err = tr.wgslType(vs.Type)
					if err != nil {
						return err
					}
				}
				for i, id := range vs.Names {
					tr.declareLocal(id.Name)
					if i < len(vs.Values) {
						val, err := tr.expr(vs.Values[i])
						if err != nil {
							return err
						}
						if wt != "" {
							fmt.Fprintf(b, "%svar %s: %s = %s;\n", indent, id.Name, wt, val)
						} else {
							fmt.Fprintf(b, "%svar %s = %s;\n", indent, id.Name, val)
						}
					} else {
						if wt == "" {
							return tr.errf(id.Pos(), "var %s requires a type or initializer", id.Name)
						}
						fmt.Fprintf(b, "%svar %s: %s;\n", indent, id.Name, wt)
					}
				}
			}
			return nil
		default:
			return tr.errf(gd.Pos(), "unsupported local %s declaration", gd.Tok)
		}

	case *ast.IncDecStmt:
		x, err := tr.expr(s.X)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s%s%s;\n", indent, x, s.Tok.String())
		return nil

	case *ast.IfStmt:
		if s.Init != nil {
			tr.pushScope()
			defer tr.popScope()
			fmt.Fprintf(b, "%s{\n", indent)
			if err := tr.emitStmt(b, s.Init, indent+"\t"); err != nil {
				return err
			}
			if err := tr.emitIfBody(b, s, indent+"\t"); err != nil {
				return err
			}
			fmt.Fprintf(b, "%s}\n", indent)
			return nil
		}
		return tr.emitIfBody(b, s, indent)

	case *ast.ForStmt:
		tr.pushScope()
		defer tr.popScope()
		var initStr, condStr, postStr string
		if s.Init != nil {
			var sb strings.Builder
			if err := tr.emitStmt(&sb, s.Init, ""); err != nil {
				return err
			}
			initStr = strings.TrimSuffix(strings.TrimSpace(sb.String()), ";")
		}
		if s.Cond != nil {
			var err error
			condStr, err = tr.expr(s.Cond)
			if err != nil {
				return err
			}
		}
		if s.Post != nil {
			var sb strings.Builder
			if err := tr.emitStmt(&sb, s.Post, ""); err != nil {
				return err
			}
			postStr = strings.TrimSuffix(strings.TrimSpace(sb.String()), ";")
		}
		fmt.Fprintf(b, "%sfor (%s; %s; %s) {\n", indent, initStr, condStr, postStr)
		for _, bs := range s.Body.List {
			if err := tr.emitStmt(b, bs, indent+"\t"); err != nil {
				return err
			}
		}
		fmt.Fprintf(b, "%s}\n", indent)
		return nil

	case *ast.BlockStmt:
		tr.pushScope()
		defer tr.popScope()
		fmt.Fprintf(b, "%s{\n", indent)
		for _, bs := range s.List {
			if err := tr.emitStmt(b, bs, indent+"\t"); err != nil {
				return err
			}
		}
		fmt.Fprintf(b, "%s}\n", indent)
		return nil

	case *ast.BranchStmt:
		if s.Label != nil {
			return tr.errf(s.Label.Pos(), "labeled %s is not supported in Kage", s.Tok)
		}
		switch s.Tok {
		case token.BREAK, token.CONTINUE:
			fmt.Fprintf(b, "%s%s;\n", indent, s.Tok.String())
			return nil
		default:
			return tr.errf(s.Pos(), "unsupported branch statement %s", s.Tok)
		}

	case *ast.ExprStmt:
		x, err := tr.expr(s.X)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s%s;\n", indent, x)
		return nil

	default:
		return tr.errf(stmt.Pos(), "unsupported statement %T in Kage surface shader", stmt)
	}
}

func (tr *kageTranslator) emitIfBody(b *strings.Builder, s *ast.IfStmt, indent string) error {
	cond, err := tr.expr(s.Cond)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%sif (%s) {\n", indent, cond)
	tr.pushScope()
	for _, bs := range s.Body.List {
		if err := tr.emitStmt(b, bs, indent+"\t"); err != nil {
			tr.popScope()
			return err
		}
	}
	tr.popScope()
	if s.Else == nil {
		fmt.Fprintf(b, "%s}\n", indent)
		return nil
	}
	switch el := s.Else.(type) {
	case *ast.BlockStmt:
		fmt.Fprintf(b, "%s} else {\n", indent)
		tr.pushScope()
		for _, bs := range el.List {
			if err := tr.emitStmt(b, bs, indent+"\t"); err != nil {
				tr.popScope()
				return err
			}
		}
		tr.popScope()
		fmt.Fprintf(b, "%s}\n", indent)
		return nil
	case *ast.IfStmt:
		if el.Init != nil {
			fmt.Fprintf(b, "%s} else {\n", indent)
			if err := tr.emitStmt(b, el, indent+"\t"); err != nil {
				return err
			}
			fmt.Fprintf(b, "%s}\n", indent)
			return nil
		}
		fmt.Fprintf(b, "%s} else ", indent)
		var sub strings.Builder
		if err := tr.emitIfBody(&sub, el, indent); err != nil {
			return err
		}
		b.WriteString(strings.TrimPrefix(sub.String(), indent))
		return nil
	default:
		return tr.errf(s.Else.Pos(), "unsupported else branch %T", s.Else)
	}
}

func (tr *kageTranslator) expr(e ast.Expr) (string, error) {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT, token.FLOAT:
			return x.Value, nil
		default:
			return "", tr.errf(x.Pos(), "unsupported literal %s in Kage surface shader", x.Value)
		}

	case *ast.Ident:
		if !tr.isLocal(x.Name) {
			if kind, ok := tr.uniforms[x.Name]; ok {
				if kind == ParamBool {
					return fmt.Sprintf("(material.%s != 0u)", x.Name), nil
				}
				return "material." + x.Name, nil
			}
		}
		return x.Name, nil

	case *ast.ParenExpr:
		inner, err := tr.expr(x.X)
		if err != nil {
			return "", err
		}
		return "(" + inner + ")", nil

	case *ast.UnaryExpr:
		inner, err := tr.expr(x.X)
		if err != nil {
			return "", err
		}
		switch x.Op {
		case token.SUB:
			return "(-" + inner + ")", nil
		case token.NOT:
			return "(!" + inner + ")", nil
		case token.XOR:
			return "(~" + inner + ")", nil
		case token.ADD:
			return inner, nil
		default:
			return "", tr.errf(x.OpPos, "unsupported unary operator %s", x.Op)
		}

	case *ast.BinaryExpr:
		l, err := tr.expr(x.X)
		if err != nil {
			return "", err
		}
		r, err := tr.expr(x.Y)
		if err != nil {
			return "", err
		}
		op, ok := kageBinaryOp(x.Op)
		if !ok {
			return "", tr.errf(x.OpPos, "unsupported binary operator %s", x.Op)
		}
		return fmt.Sprintf("(%s %s %s)", l, op, r), nil

	case *ast.SelectorExpr:
		base, err := tr.expr(x.X)
		if err != nil {
			return "", err
		}
		return base + "." + x.Sel.Name, nil

	case *ast.IndexExpr:
		base, err := tr.expr(x.X)
		if err != nil {
			return "", err
		}
		idx, err := tr.expr(x.Index)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s[%s]", base, idx), nil

	case *ast.CallExpr:
		id, ok := x.Fun.(*ast.Ident)
		if !ok {
			return "", tr.errf(x.Fun.Pos(), "unsupported call target in Kage surface shader")
		}
		return tr.callExpr(id, x.Args)

	default:
		return "", tr.errf(e.Pos(), "unsupported expression %T in Kage surface shader", e)
	}
}

func (tr *kageTranslator) callExpr(fn *ast.Ident, rawArgs []ast.Expr) (string, error) {
	name := fn.Name

	// sample(texParam, uv) -> textureSample(texParam, texParam_sampler, uv)
	if name == "sample" {
		if len(rawArgs) != 2 {
			return "", tr.errf(fn.Pos(), "sample expects 2 arguments (texture, uv)")
		}
		texID, ok := rawArgs[0].(*ast.Ident)
		if !ok || !tr.texByName[texID.Name] {
			return "", tr.errf(rawArgs[0].Pos(), "sample first argument must be a declared ParamTexture parameter")
		}
		uv, err := tr.expr(rawArgs[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("textureSample(%s, %s_sampler, %s)", texID.Name, texID.Name, uv), nil
	}

	// imageNAt(uv) -> textureSample(texOrder[N], texOrder[N]_sampler, uv)
	if strings.HasPrefix(name, "image") && strings.HasSuffix(name, "At") && len(name) > len("imageAt") {
		idxStr := name[len("image") : len(name)-len("At")]
		if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 {
			if len(rawArgs) != 1 {
				return "", tr.errf(fn.Pos(), "%s expects 1 uv argument", name)
			}
			if idx >= len(tr.texOrder) {
				return "", tr.errf(fn.Pos(), "%s references texture slot %d, but only %d ParamTexture parameters are declared", name, idx, len(tr.texOrder))
			}
			uv, err := tr.expr(rawArgs[0])
			if err != nil {
				return "", err
			}
			tname := tr.texOrder[idx]
			return fmt.Sprintf("textureSample(%s, %s_sampler, %s)", tname, tname, uv), nil
		}
	}

	args := make([]string, len(rawArgs))
	for i, a := range rawArgs {
		s, err := tr.expr(a)
		if err != nil {
			return "", err
		}
		args[i] = s
	}

	if name == "mod" {
		if len(args) != 2 {
			return "", tr.errf(fn.Pos(), "mod expects 2 arguments")
		}
		return fmt.Sprintf("((%s) - (%s) * floor((%s) / (%s)))", args[0], args[1], args[0], args[1]), nil
	}
	if name == "atan" && len(args) == 2 {
		return fmt.Sprintf("atan2(%s, %s)", args[0], args[1]), nil
	}

	if tr.helpers[name] {
		return fmt.Sprintf("%s(%s)", kageFuncWGSLName(name), strings.Join(args, ", ")), nil
	}

	wgslName := kageCalleeWGSLName(name)
	return fmt.Sprintf("%s(%s)", wgslName, strings.Join(args, ", ")), nil
}

func kageCalleeWGSLName(name string) string {
	switch name {
	case "float":
		return "f32"
	case "int":
		return "i32"
	case "vec2":
		return "vec2<f32>"
	case "vec3":
		return "vec3<f32>"
	case "vec4":
		return "vec4<f32>"
	case "ivec2":
		return "vec2<i32>"
	case "ivec3":
		return "vec3<i32>"
	case "ivec4":
		return "vec4<i32>"
	case "mat2":
		return "mat2x2<f32>"
	case "mat3":
		return "mat3x3<f32>"
	case "mat4":
		return "mat4x4<f32>"
	case "inversesqrt":
		return "inverseSqrt"
	default:
		return name
	}
}

func kageAssignOp(tok token.Token) (string, bool) {
	switch tok {
	case token.ASSIGN:
		return "=", true
	case token.ADD_ASSIGN:
		return "+=", true
	case token.SUB_ASSIGN:
		return "-=", true
	case token.MUL_ASSIGN:
		return "*=", true
	case token.QUO_ASSIGN:
		return "/=", true
	case token.REM_ASSIGN:
		return "%=", true
	default:
		return "", false
	}
}

func kageBinaryOp(tok token.Token) (string, bool) {
	switch tok {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
		token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ,
		token.LAND, token.LOR, token.AND, token.OR, token.XOR, token.SHL, token.SHR:
		return tok.String(), true
	default:
		return "", false
	}
}

// --- Validation and value coercion helpers ---

func validateParamDesc(label string, p ShaderParamDesc) error {
	if !isValidShaderIdent(p.Name) {
		return &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  "parameter name must be a non-reserved shader identifier",
		}
	}
	if !p.Kind.Valid() {
		return &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("invalid parameter kind %s", p.Kind),
		}
	}
	if p.Min != nil || p.Max != nil {
		if p.Kind == ParamBool || p.Kind == ParamColor || p.Kind == ParamTexture {
			return &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("Min/Max bounds are not supported for kind %s", p.Kind),
			}
		}
		if p.Min != nil && (math.IsNaN(*p.Min) || math.IsInf(*p.Min, 0)) {
			return &CustomMaterialError{Material: label, Param: p.Name, Message: "Min bound must be finite"}
		}
		if p.Max != nil && (math.IsNaN(*p.Max) || math.IsInf(*p.Max, 0)) {
			return &CustomMaterialError{Material: label, Param: p.Name, Message: "Max bound must be finite"}
		}
		if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
			return &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("Min (%g) exceeds Max (%g)", *p.Min, *p.Max),
			}
		}
	}
	return nil
}

func validateParamDefaults(label string, params []ShaderParamDesc) error {
	for _, p := range params {
		if p.Default == nil {
			continue
		}
		switch p.Kind {
		case ParamFloat:
			if _, err := coerceFloat32(label, p, p.Default); err != nil {
				return err
			}
		case ParamVec2:
			if _, err := coerceVec2(label, p, p.Default); err != nil {
				return err
			}
		case ParamVec3:
			if _, err := coerceVec3(label, p, p.Default); err != nil {
				return err
			}
		case ParamVec4:
			if _, err := coerceVec4(label, p, p.Default); err != nil {
				return err
			}
		case ParamColor:
			if _, err := coerceColor(label, p, p.Default); err != nil {
				return err
			}
		case ParamInt:
			if _, err := coerceInt32(label, p, p.Default); err != nil {
				return err
			}
		case ParamBool:
			if _, ok := p.Default.(bool); !ok {
				return &CustomMaterialError{
					Material: label,
					Param:    p.Name,
					Message:  fmt.Sprintf("default must be bool, got %T", p.Default),
				}
			}
		case ParamTexture:
			if _, err := parseFallbackTexel(label, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseFallbackTexel(label string, p ShaderParamDesc) ([4]byte, error) {
	if p.Default == nil {
		return [4]byte{255, 255, 255, 255}, nil
	}
	switch v := p.Default.(type) {
	case [4]byte:
		return v, nil
	case color.RGBA:
		return [4]byte{v.R, v.G, v.B, v.A}, nil
	case color.NRGBA:
		return [4]byte{v.R, v.G, v.B, v.A}, nil
	case core.Color:
		if v == core.Transparent {
			return [4]byte{0, 0, 0, 0}, nil
		}
		return [4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, nil
	default:
		return [4]byte{}, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("texture fallback default must be nil, [4]byte, color.RGBA, color.NRGBA, or core.Color, got %T", p.Default),
		}
	}
}

func checkFiniteRange(label string, p ShaderParamDesc, vals ...float64) error {
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("value %v is not finite", v),
			}
		}
		if p.Min != nil && v < *p.Min {
			return &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("value %g is below minimum %g", v, *p.Min),
			}
		}
		if p.Max != nil && v > *p.Max {
			return &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("value %g exceeds maximum %g", v, *p.Max),
			}
		}
	}
	return nil
}

func coerceFloat32(label string, p ShaderParamDesc, val any) (float32, error) {
	var f float64
	switch v := val.(type) {
	case float32:
		f = float64(v)
	case float64:
		f = v
	default:
		return 0, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected float32 or float64, got %T", val),
		}
	}
	if err := checkFiniteRange(label, p, f); err != nil {
		return 0, err
	}
	f32 := float32(f)
	if !finite(f32) {
		return 0, &CustomMaterialError{Material: label, Param: p.Name, Message: fmt.Sprintf("value %g overflows float32", f)}
	}
	return f32, nil
}

func coerceVec2(label string, p ShaderParamDesc, val any) (math3d.Vec2, error) {
	var x, y float64
	switch v := val.(type) {
	case math3d.Vec2:
		x, y = float64(v.X), float64(v.Y)
	case [2]float32:
		x, y = float64(v[0]), float64(v[1])
	case [2]float64:
		x, y = v[0], v[1]
	default:
		return math3d.Vec2{}, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected math3d.Vec2 or [2]float32, got %T", val),
		}
	}
	if err := checkFiniteRange(label, p, x, y); err != nil {
		return math3d.Vec2{}, err
	}
	out := math3d.Vec2{X: float32(x), Y: float32(y)}
	if !finite(out.X) || !finite(out.Y) {
		return math3d.Vec2{}, &CustomMaterialError{Material: label, Param: p.Name, Message: "vec2 overflows float32"}
	}
	return out, nil
}

func coerceVec3(label string, p ShaderParamDesc, val any) (math3d.Vec3, error) {
	var x, y, z float64
	switch v := val.(type) {
	case math3d.Vec3:
		x, y, z = float64(v.X), float64(v.Y), float64(v.Z)
	case [3]float32:
		x, y, z = float64(v[0]), float64(v[1]), float64(v[2])
	case [3]float64:
		x, y, z = v[0], v[1], v[2]
	default:
		return math3d.Vec3{}, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected math3d.Vec3 or [3]float32, got %T", val),
		}
	}
	if err := checkFiniteRange(label, p, x, y, z); err != nil {
		return math3d.Vec3{}, err
	}
	out := math3d.Vec3{X: float32(x), Y: float32(y), Z: float32(z)}
	if !finite3(out) {
		return math3d.Vec3{}, &CustomMaterialError{Material: label, Param: p.Name, Message: "vec3 overflows float32"}
	}
	return out, nil
}

func coerceVec4(label string, p ShaderParamDesc, val any) (math3d.Vec4, error) {
	var x, y, z, w float64
	switch v := val.(type) {
	case math3d.Vec4:
		x, y, z, w = float64(v.X), float64(v.Y), float64(v.Z), float64(v.W)
	case [4]float32:
		x, y, z, w = float64(v[0]), float64(v[1]), float64(v[2]), float64(v[3])
	case [4]float64:
		x, y, z, w = v[0], v[1], v[2], v[3]
	default:
		return math3d.Vec4{}, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected math3d.Vec4 or [4]float32, got %T", val),
		}
	}
	if err := checkFiniteRange(label, p, x, y, z, w); err != nil {
		return math3d.Vec4{}, err
	}
	out := math3d.Vec4{X: float32(x), Y: float32(y), Z: float32(z), W: float32(w)}
	if !finite(out.X) || !finite(out.Y) || !finite(out.Z) || !finite(out.W) {
		return math3d.Vec4{}, &CustomMaterialError{Material: label, Param: p.Name, Message: "vec4 overflows float32"}
	}
	return out, nil
}

func coerceColor(label string, p ShaderParamDesc, val any) (math3d.Vec4, error) {
	switch v := val.(type) {
	case core.Color:
		return colorToVec4(v), nil
	case color.RGBA:
		return math3d.Vec4{
			X: float32(v.R) / 255,
			Y: float32(v.G) / 255,
			Z: float32(v.B) / 255,
			W: float32(v.A) / 255,
		}, nil
	case color.NRGBA:
		return math3d.Vec4{
			X: float32(v.R) / 255,
			Y: float32(v.G) / 255,
			Z: float32(v.B) / 255,
			W: float32(v.A) / 255,
		}, nil
	case math3d.Vec4, [4]float32, [4]float64:
		return coerceVec4(label, p, v)
	case string:
		c, err := core.ParseColor(v)
		if err != nil {
			return math3d.Vec4{}, &CustomMaterialError{
				Material: label,
				Param:    p.Name,
				Message:  fmt.Sprintf("invalid CSS color %q", v),
				Err:      err,
			}
		}
		return colorToVec4(c), nil
	default:
		return math3d.Vec4{}, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected core.Color, color.RGBA, color.NRGBA, vec4, or CSS color string, got %T", val),
		}
	}
}

func coerceInt32(label string, p ShaderParamDesc, val any) (int32, error) {
	var i int64
	switch v := val.(type) {
	case int32:
		i = int64(v)
	case int:
		i = int64(v)
	default:
		return 0, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("expected int32 or int, got %T", val),
		}
	}
	if i < math.MinInt32 || i > math.MaxInt32 {
		return 0, &CustomMaterialError{
			Material: label,
			Param:    p.Name,
			Message:  fmt.Sprintf("int value %d overflows int32", i),
		}
	}
	if err := checkFiniteRange(label, p, float64(i)); err != nil {
		return 0, err
	}
	return int32(i), nil
}

func colorToVec4(c core.Color) math3d.Vec4 {
	if c == core.Transparent {
		return math3d.Vec4{}
	}
	r, g, b, a := c.Components()
	return math3d.Vec4{X: float32(r), Y: float32(g), Z: float32(b), W: float32(a)}
}

func isValidShaderIdent(name string) bool {
	if name == "" || strings.HasPrefix(name, "_r3d_") || strings.HasPrefix(name, "_R3D") || strings.HasPrefix(name, "_kage_") {
		return false
	}
	for i := range len(name) {
		ch := name[i]
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
				return false
			}
		} else {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return false
			}
		}
	}
	switch name {
	case "_", "fn", "var", "let", "const", "struct", "return", "if", "else", "for", "while", "loop", "break", "continue",
		"switch", "case", "default", "discard", "true", "false", "material", "camera", "MaterialParams", "Camera",
		"f32", "i32", "u32", "bool", "float", "int", "vec2", "vec3", "vec4", "mat2", "mat3", "mat4", "sampler":
		return false
	}
	return true
}

func alignUp(n, align int) int {
	if align <= 1 {
		return n
	}
	return (n + align - 1) / align * align
}

func customMatLabel(base, suffix string) string {
	if base == "" {
		return "render3d.custom." + suffix
	}
	return base + "." + suffix
}

func parseGoParseError(err error, lineOffset int) (line, col int, msg string) {
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		first := list[0]
		return max(first.Pos.Line-lineOffset, 1), first.Pos.Column, first.Msg
	}
	return 0, 0, err.Error()
}

func parseWGSLDiag(err error) (line, col int, msg string) {
	raw := err.Error()
	var ge gpu.Error
	if errors.As(err, &ge) && ge.Message != "" {
		raw = ge.Message
	}
	trimmed := strings.TrimSpace(raw)
	first, rest, hasRest := strings.Cut(trimmed, "\n")
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), "shader:"))
	if lStr, afterL, ok := strings.Cut(s, ":"); ok {
		if l, lErr := strconv.Atoi(strings.TrimSpace(lStr)); lErr == nil && l > 0 {
			if cStr, afterC, ok2 := strings.Cut(afterL, ":"); ok2 {
				if c, cErr := strconv.Atoi(strings.TrimSpace(cStr)); cErr == nil && c > 0 {
					m := strings.TrimSpace(afterC)
					if hasRest {
						m += "\n" + rest
					}
					return l, c, m
				}
			}
			m := strings.TrimSpace(afterL)
			if hasRest {
				m += "\n" + rest
			}
			return l, 0, m
		}
	}
	return 0, 0, trimmed
}

package hotreload

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"go.yaml.in/yaml/v3"
)

// MaterialVersion is the schema version MaterialDef reads and writes.
const MaterialVersion = 1

var materialBlendModes = map[string]func() *gpu.BlendState{
	"opaque":        gpu.BlendOpaque,
	"alpha":         gpu.BlendAlpha,
	"premultiplied": gpu.BlendPremultipliedAlpha,
	"additive":      gpu.BlendAdditive,
	"multiply":      gpu.BlendMultiply,
}

var materialCullModes = map[string]gpu.CullMode{
	"none":  gpu.CullNone,
	"front": gpu.CullFront,
	"back":  gpu.CullBack,
}

var materialFrontFaces = map[string]gpu.FrontFace{
	"ccw": gpu.FrontCCW,
	"cw":  gpu.FrontCW,
}

var materialDepthCompares = map[string]gpu.CompareFunc{
	"never":        gpu.CompareNever,
	"less":         gpu.CompareLess,
	"equal":        gpu.CompareEqual,
	"lessEqual":    gpu.CompareLessEqual,
	"greater":      gpu.CompareGreater,
	"notEqual":     gpu.CompareNotEqual,
	"greaterEqual": gpu.CompareGreaterEqual,
	"always":       gpu.CompareAlways,
}

// MaterialBlendModes returns the sorted blend mode names accepted by MaterialDef.Blend.
func MaterialBlendModes() []string { return sortedMapKeys(materialBlendModes) }

// MaterialCullModes returns the sorted face-culling modes accepted by MaterialDef.Cull.
func MaterialCullModes() []string { return sortedMapKeys(materialCullModes) }

// MaterialFrontFaces returns the sorted winding orders accepted by MaterialDef.FrontFace.
func MaterialFrontFaces() []string { return sortedMapKeys(materialFrontFaces) }

// MaterialDepthCompares returns the sorted depth comparison functions accepted by MaterialDef.DepthCompare.
func MaterialDepthCompares() []string { return sortedMapKeys(materialDepthCompares) }

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// MaterialDef is the text YAML definition of a 3D shader material: its shader path(s),
// pipeline render state (blend, cull, front face, depth test/write), and typed parameters
// (scalars, vectors, CSS colors, and texture paths).
//
// Files use version: 1, refuse unknown fields, and round-trip deterministically through
// ParseMaterial and EncodeMaterial.
type MaterialDef struct {
	// Version is the schema version; it must equal MaterialVersion (1).
	Version int `yaml:"version"`
	// Name is an optional human-readable display name.
	Name string `yaml:"name,omitempty"`
	// Shader is a slash-separated relative path to a .wgsl or .kage shader file providing
	// both vertex and fragment stages (unless overridden by VertexShader or FragmentShader).
	Shader string `yaml:"shader,omitempty"`
	// VertexShader overrides Shader for the vertex stage when split across files.
	VertexShader string `yaml:"vertexShader,omitempty"`
	// FragmentShader overrides Shader for the fragment stage when split across files.
	FragmentShader string `yaml:"fragmentShader,omitempty"`
	// VertexEntry names the @vertex entry point; empty defaults to "vs_main".
	VertexEntry string `yaml:"vertexEntry,omitempty"`
	// FragmentEntry names the @fragment entry point; empty defaults to "fs_main" (or "Fragment" for .kage).
	FragmentEntry string `yaml:"fragmentEntry,omitempty"`
	// Blend selects color/alpha blending ("opaque", "alpha", "premultiplied", "additive", "multiply"); empty is "opaque".
	Blend string `yaml:"blend,omitempty"`
	// Cull selects face culling ("none", "front", "back"); empty is "back".
	Cull string `yaml:"cull,omitempty"`
	// FrontFace selects front-facing triangle winding ("ccw", "cw"); empty is "ccw".
	FrontFace string `yaml:"frontFace,omitempty"`
	// DepthWrite controls whether fragments write depth; nil defaults to true when a depth attachment is used.
	DepthWrite *bool `yaml:"depthWrite,omitempty"`
	// DepthCompare selects the depth comparison function ("never", "less", "equal", "lessEqual", "greater", "notEqual", "greaterEqual", "always"); empty is "less".
	DepthCompare string `yaml:"depthCompare,omitempty"`
	// Scalars holds named float32 material parameters (for example roughness, metallic, timeScale).
	Scalars map[string]float32 `yaml:"scalars,omitempty"`
	// Vec2 holds named 2-component float32 parameters (for example uvScale, uvOffset).
	Vec2 map[string][2]float32 `yaml:"vec2,omitempty"`
	// Vec3 holds named 3-component float32 parameters (for example lightDir, rimColor).
	Vec3 map[string][3]float32 `yaml:"vec3,omitempty"`
	// Vec4 holds named 4-component float32 parameters (for example tint, params).
	Vec4 map[string][4]float32 `yaml:"vec4,omitempty"`
	// Colors holds named CSS color parameters parsed with core.ParseColor (for example "#ff8800", "rgb(10, 20, 30)").
	Colors map[string]string `yaml:"colors,omitempty"`
	// Textures holds named slash-separated relative paths to texture images.
	Textures map[string]string `yaml:"textures,omitempty"`
}

// ParseMaterial decodes and validates a YAML MaterialDef, refusing unknown fields
// and trailing YAML documents. Errors are returned as *loaderr.Error with line,
// column, and field information when available.
func ParseMaterial(data []byte) (*MaterialDef, error) {
	return ParseMaterialFile("", data)
}

// ParseMaterialFile decodes and validates a YAML MaterialDef and names filePath
// in any returned *loaderr.Error.
func ParseMaterialFile(filePath string, data []byte) (*MaterialDef, error) {
	cleanPath := normalizeRelPath(filePath)
	const defaultFix = "fix the material YAML and save again"

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			File:    cleanPath,
			Line:    1,
			Column:  1,
			Problem: "empty material document",
			Fix:     "add version: 1 and material fields",
		}
	}

	var m MaterialDef
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, loaderr.Locate(err, cleanPath, data, defaultFix)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, &loaderr.Error{
				File:    cleanPath,
				Problem: "unexpected trailing YAML document",
				Fix:     "keep one material definition per file",
			}
		}
		return nil, loaderr.Locate(err, cleanPath, data, defaultFix)
	}
	if err := m.Validate(); err != nil {
		return nil, loaderr.Locate(err, cleanPath, data, defaultFix)
	}
	return &m, nil
}

// EncodeMaterial validates m and writes deterministic 2-space YAML with fixed
// field order and sorted parameter keys. A document produced by EncodeMaterial
// round-trips through ParseMaterial and EncodeMaterial with identical bytes.
func EncodeMaterial(m *MaterialDef) ([]byte, error) {
	if m == nil {
		return nil, errors.New("hotreload: nil material")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("hotreload: encode material: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("hotreload: encode material: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate checks m's version, shader paths, render state enums, numeric values,
// CSS colors, and texture paths.
func (m *MaterialDef) Validate() error {
	if m == nil {
		return errors.New("hotreload: nil material")
	}
	if m.Version != MaterialVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("unsupported material version %d", m.Version),
			Expected: fmt.Sprintf("%d", MaterialVersion),
			Fix:      fmt.Sprintf("set version: %d", MaterialVersion),
		}
	}
	for _, sp := range []struct {
		field string
		val   string
	}{
		{"shader", m.Shader},
		{"vertexShader", m.VertexShader},
		{"fragmentShader", m.FragmentShader},
	} {
		if sp.val == "" {
			continue
		}
		if strings.Contains(sp.val, "\\") || !fs.ValidPath(sp.val) || sp.val == "." {
			return &loaderr.Error{
				Field:    sp.field,
				Problem:  fmt.Sprintf("invalid shader path %q", sp.val),
				Expected: "a slash-separated path relative to the asset root",
				Fix:      "use a relative path such as shaders/lit.wgsl",
			}
		}
		if !IsShaderFile(sp.val) {
			return &loaderr.Error{
				Field:    sp.field,
				Problem:  fmt.Sprintf("unsupported shader extension in %q", sp.val),
				Expected: ".wgsl or .kage",
				Fix:      "point to a .wgsl or .kage shader file",
			}
		}
	}
	if m.Blend != "" {
		if _, ok := materialBlendModes[m.Blend]; !ok {
			return &loaderr.Error{
				Field:    "blend",
				Problem:  fmt.Sprintf("unknown blend mode %q", m.Blend),
				Expected: strings.Join(MaterialBlendModes(), ", "),
				Fix:      "choose one of the supported blend modes",
			}
		}
	}
	if m.Cull != "" {
		if _, ok := materialCullModes[m.Cull]; !ok {
			return &loaderr.Error{
				Field:    "cull",
				Problem:  fmt.Sprintf("unknown cull mode %q", m.Cull),
				Expected: strings.Join(MaterialCullModes(), ", "),
				Fix:      "choose none, front, or back",
			}
		}
	}
	if m.FrontFace != "" {
		if _, ok := materialFrontFaces[m.FrontFace]; !ok {
			return &loaderr.Error{
				Field:    "frontFace",
				Problem:  fmt.Sprintf("unknown frontFace %q", m.FrontFace),
				Expected: strings.Join(MaterialFrontFaces(), ", "),
				Fix:      "choose ccw or cw",
			}
		}
	}
	if m.DepthCompare != "" {
		if _, ok := materialDepthCompares[m.DepthCompare]; !ok {
			return &loaderr.Error{
				Field:    "depthCompare",
				Problem:  fmt.Sprintf("unknown depthCompare %q", m.DepthCompare),
				Expected: strings.Join(MaterialDepthCompares(), ", "),
				Fix:      "choose a supported depth compare function such as less or lessEqual",
			}
		}
	}
	for _, k := range sortedMapKeys(m.Scalars) {
		if k == "" {
			return &loaderr.Error{Field: "scalars", Problem: "empty parameter name", Fix: "give every scalar parameter a non-empty key"}
		}
		if !isFinite32(m.Scalars[k]) {
			return &loaderr.Error{Field: loaderr.Path("scalars", k), Problem: fmt.Sprintf("non-finite scalar %v", m.Scalars[k]), Expected: "a finite number", Fix: "replace NaN or Inf with a finite number"}
		}
	}
	for _, k := range sortedMapKeys(m.Vec2) {
		if k == "" {
			return &loaderr.Error{Field: "vec2", Problem: "empty parameter name", Fix: "give every vec2 parameter a non-empty key"}
		}
		for i, v := range m.Vec2[k] {
			if !isFinite32(v) {
				return &loaderr.Error{Field: loaderr.Path("vec2", k, i), Problem: fmt.Sprintf("non-finite component %v", v), Expected: "a finite number", Fix: "replace NaN or Inf with a finite number"}
			}
		}
	}
	for _, k := range sortedMapKeys(m.Vec3) {
		if k == "" {
			return &loaderr.Error{Field: "vec3", Problem: "empty parameter name", Fix: "give every vec3 parameter a non-empty key"}
		}
		for i, v := range m.Vec3[k] {
			if !isFinite32(v) {
				return &loaderr.Error{Field: loaderr.Path("vec3", k, i), Problem: fmt.Sprintf("non-finite component %v", v), Expected: "a finite number", Fix: "replace NaN or Inf with a finite number"}
			}
		}
	}
	for _, k := range sortedMapKeys(m.Vec4) {
		if k == "" {
			return &loaderr.Error{Field: "vec4", Problem: "empty parameter name", Fix: "give every vec4 parameter a non-empty key"}
		}
		for i, v := range m.Vec4[k] {
			if !isFinite32(v) {
				return &loaderr.Error{Field: loaderr.Path("vec4", k, i), Problem: fmt.Sprintf("non-finite component %v", v), Expected: "a finite number", Fix: "replace NaN or Inf with a finite number"}
			}
		}
	}
	for _, k := range sortedMapKeys(m.Colors) {
		if k == "" {
			return &loaderr.Error{Field: "colors", Problem: "empty parameter name", Fix: "give every color parameter a non-empty key"}
		}
		if _, err := core.ParseColor(m.Colors[k]); err != nil {
			return &loaderr.Error{
				Field:    loaderr.Path("colors", k),
				Problem:  fmt.Sprintf("invalid color %q", m.Colors[k]),
				Expected: "a CSS color such as #ff8800 or rgba(255, 128, 0, 0.5)",
				Fix:      "write a valid CSS color",
				Err:      err,
			}
		}
	}
	for _, k := range sortedMapKeys(m.Textures) {
		if k == "" {
			return &loaderr.Error{Field: "textures", Problem: "empty texture slot name", Fix: "give every texture entry a non-empty key"}
		}
		v := m.Textures[k]
		if strings.Contains(v, "\\") || !fs.ValidPath(v) || v == "." {
			return &loaderr.Error{
				Field:    loaderr.Path("textures", k),
				Problem:  fmt.Sprintf("invalid texture path %q", v),
				Expected: "a slash-separated path relative to the asset root",
				Fix:      "use a relative path such as textures/albedo.png",
			}
		}
	}
	return nil
}

// Clone returns a deep copy of m so callers can inspect or modify it independently.
func (m *MaterialDef) Clone() *MaterialDef {
	if m == nil {
		return nil
	}
	c := *m
	if m.DepthWrite != nil {
		dw := *m.DepthWrite
		c.DepthWrite = &dw
	}
	if m.Scalars != nil {
		c.Scalars = maps.Clone(m.Scalars)
	}
	if m.Vec2 != nil {
		c.Vec2 = maps.Clone(m.Vec2)
	}
	if m.Vec3 != nil {
		c.Vec3 = maps.Clone(m.Vec3)
	}
	if m.Vec4 != nil {
		c.Vec4 = maps.Clone(m.Vec4)
	}
	if m.Colors != nil {
		c.Colors = maps.Clone(m.Colors)
	}
	if m.Textures != nil {
		c.Textures = maps.Clone(m.Textures)
	}
	return &c
}

// EffectiveVertexShader returns VertexShader if set, otherwise Shader.
func (m *MaterialDef) EffectiveVertexShader() string {
	if m == nil {
		return ""
	}
	if m.VertexShader != "" {
		return normalizeRelPath(m.VertexShader)
	}
	return normalizeRelPath(m.Shader)
}

// EffectiveFragmentShader returns FragmentShader if set, otherwise Shader.
func (m *MaterialDef) EffectiveFragmentShader() string {
	if m == nil {
		return ""
	}
	if m.FragmentShader != "" {
		return normalizeRelPath(m.FragmentShader)
	}
	return normalizeRelPath(m.Shader)
}

// ShaderPaths returns the sorted, deduplicated list of shader paths referenced by m.
func (m *MaterialDef) ShaderPaths() []string {
	if m == nil {
		return nil
	}
	v := m.EffectiveVertexShader()
	f := m.EffectiveFragmentShader()
	var out []string
	if v != "" {
		out = append(out, v)
	}
	if f != "" && f != v {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// EffectiveVertexEntry returns VertexEntry if set, otherwise "vs_main".
func (m *MaterialDef) EffectiveVertexEntry() string {
	if m == nil || m.VertexEntry == "" {
		return "vs_main"
	}
	return m.VertexEntry
}

// EffectiveFragmentEntry returns FragmentEntry if set, otherwise "Fragment" for .kage shaders or "fs_main" for WGSL shaders.
func (m *MaterialDef) EffectiveFragmentEntry() string {
	if m != nil && m.FragmentEntry != "" {
		return m.FragmentEntry
	}
	if strings.HasSuffix(strings.ToLower(m.EffectiveFragmentShader()), ShaderExtKage) {
		return "Fragment"
	}
	return "fs_main"
}

// BlendState returns the gpu.BlendState configured by m.Blend, defaulting to gpu.BlendOpaque().
func (m *MaterialDef) BlendState() *gpu.BlendState {
	if m != nil {
		if fn, ok := materialBlendModes[m.Blend]; ok {
			return fn()
		}
	}
	return gpu.BlendOpaque()
}

// CullMode returns the gpu.CullMode configured by m.Cull, defaulting to gpu.CullBack.
func (m *MaterialDef) CullMode() gpu.CullMode {
	if m != nil {
		if c, ok := materialCullModes[m.Cull]; ok {
			return c
		}
	}
	return gpu.CullBack
}

// FrontFaceMode returns the gpu.FrontFace configured by m.FrontFace, defaulting to gpu.FrontCCW.
func (m *MaterialDef) FrontFaceMode() gpu.FrontFace {
	if m != nil {
		if ff, ok := materialFrontFaces[m.FrontFace]; ok {
			return ff
		}
	}
	return gpu.FrontCCW
}

// EffectiveDepthWrite returns DepthWrite when set, or true by default.
func (m *MaterialDef) EffectiveDepthWrite() bool {
	if m == nil || m.DepthWrite == nil {
		return true
	}
	return *m.DepthWrite
}

// DepthCompareFunc returns the gpu.CompareFunc configured by m.DepthCompare, defaulting to gpu.CompareLess.
func (m *MaterialDef) DepthCompareFunc() gpu.CompareFunc {
	if m != nil {
		if cmp, ok := materialDepthCompares[m.DepthCompare]; ok {
			return cmp
		}
	}
	return gpu.CompareLess
}

// DepthStencilState builds a gpu.DepthStencilState for depthFormat using m's
// DepthWrite and DepthCompare, returning nil when depthFormat is gpu.FormatUndefined.
func (m *MaterialDef) DepthStencilState(depthFormat gpu.TextureFormat) *gpu.DepthStencilState {
	if depthFormat == gpu.FormatUndefined {
		return nil
	}
	return &gpu.DepthStencilState{
		Format:       depthFormat,
		DepthWrite:   m.EffectiveDepthWrite(),
		DepthCompare: m.DepthCompareFunc(),
	}
}

// Color parses and returns the named color from m.Colors, or (0, false) if absent or invalid.
func (m *MaterialDef) Color(name string) (core.Color, bool) {
	if m == nil {
		return 0, false
	}
	raw, ok := m.Colors[name]
	if !ok {
		return 0, false
	}
	c, err := core.ParseColor(raw)
	if err != nil {
		return 0, false
	}
	return c, true
}

// ApplyToPipelineDesc applies m's entry points, face culling, front face, blend state,
// and depth-stencil state (when depthFormat != gpu.FormatUndefined) onto desc.
func (m *MaterialDef) ApplyToPipelineDesc(desc *gpu.PipelineDesc, depthFormat gpu.TextureFormat) {
	if m == nil || desc == nil {
		return
	}
	if desc.Vertex.EntryPoint == "" {
		desc.Vertex.EntryPoint = m.EffectiveVertexEntry()
	}
	if desc.Fragment.EntryPoint == "" {
		desc.Fragment.EntryPoint = m.EffectiveFragmentEntry()
	}
	desc.Primitive.CullMode = m.CullMode()
	desc.Primitive.FrontFace = m.FrontFaceMode()
	if desc.Primitive.Topology == 0 {
		desc.Primitive.Topology = gpu.TopologyTriangleList
	}
	blend := m.BlendState()
	for i := range desc.Targets {
		desc.Targets[i].Blend = blend
	}
	if depthFormat != gpu.FormatUndefined {
		desc.DepthStencil = m.DepthStencilState(depthFormat)
	}
}

func isFinite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// MaterialPipelineBuilder constructs a gpu.PipelineDesc for a material from its
// parsed MaterialDef and compiled vertex and fragment shader modules. Returning an
// error rejects the candidate and preserves the previous working material and pipeline.
type MaterialPipelineBuilder func(def *MaterialDef, vertex, fragment *gpu.ShaderModule) (gpu.PipelineDesc, error)

// MaterialOption configures a MaterialReloader.
type MaterialOption func(*materialConfig)

type materialConfig struct {
	fsys     fs.FS
	onReload func(name, path string)
	onError  func(ReloadError)
}

// WithMaterialFS sets the file system used for initial material loads.
func WithMaterialFS(fsys fs.FS) MaterialOption {
	return func(c *materialConfig) { c.fsys = fsys }
}

// WithMaterialDir sets a directory on disk used for initial material loads.
func WithMaterialDir(dir string) MaterialOption {
	return func(c *materialConfig) {
		if dir != "" {
			c.fsys = os.DirFS(filepath.Clean(dir))
		}
	}
}

// WithOnMaterialReload sets a callback invoked whenever a material reloads cleanly
// (either from an edited material YAML file or from an edited shader it depends on).
func WithOnMaterialReload(fn func(name, path string)) MaterialOption {
	return func(c *materialConfig) { c.onReload = fn }
}

// WithOnMaterialError sets a callback invoked whenever a material fails to parse,
// validate, compile its shader, or create its candidate GPU pipeline.
func WithOnMaterialError(fn func(ReloadError)) MaterialOption {
	return func(c *materialConfig) { c.onError = fn }
}

type materialEntry struct {
	name     string
	path     string
	def      *MaterialDef
	build    MaterialPipelineBuilder
	pipeline *gpu.Pipeline
}

// MaterialReloader manages hot-reloadable 3D shader materials and their GPU pipelines,
// tracking dependencies on shaders managed by a ShaderReloader.
//
// When a material YAML file changes, MaterialReloader parses and validates the candidate
// MaterialDef and builds a candidate gpu.Pipeline before swapping. On any parse, validation,
// shader compile, or pipeline creation error, the previous working material and pipeline
// stay active, a ReloadError is recorded, and OnError is invoked without crashing.
//
// When a shader file referenced by one or more materials changes in the paired ShaderReloader,
// MaterialReloader automatically rebuilds and swaps every dependent material's pipeline.
type MaterialReloader struct {
	// Version increments on every successful reload when created with a non-nil *core.Scope.
	Version *core.Signal[int]

	cx      *core.Scope
	shaders *ShaderReloader
	cfg     materialConfig

	mu        sync.Mutex
	fsys      fs.FS
	materials map[string]*materialEntry // name -> entry
	byPath    map[string][]string       // relPath -> material names
	errors    map[string]ReloadError    // name -> last error
	watched   map[*Watcher]struct{}
	revision  uint64
	closed    bool
}

// NewMaterialReloader creates a MaterialReloader optionally paired with shaders
// (for GPU pipeline compilation and shader-to-material dependency tracking).
func NewMaterialReloader(cx *core.Scope, shaders *ShaderReloader, opts ...MaterialOption) *MaterialReloader {
	var cfg materialConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	var ver *core.Signal[int]
	if cx != nil {
		ver = core.NewSignal(cx, 0)
	}
	fsys := cfg.fsys
	if fsys == nil && shaders != nil {
		fsys = shaders.getFS()
	}
	if shaders != nil && fsys != nil {
		shaders.setFSIfNil(fsys)
	}
	r := &MaterialReloader{
		Version:   ver,
		cx:        cx,
		shaders:   shaders,
		cfg:       cfg,
		fsys:      fsys,
		materials: make(map[string]*materialEntry),
		byPath:    make(map[string][]string),
		errors:    make(map[string]ReloadError),
		watched:   make(map[*Watcher]struct{}),
	}
	if shaders != nil {
		shaders.addListener(shaderListener{
			tracks:   r.tracksShader,
			onReload: r.onShaderReloaded,
			onError:  r.onShaderError,
		})
	}
	if cx != nil {
		cx.Cleanup(r.Close)
	}
	return r
}

// Revision returns the monotonic count of successful material reloads.
func (r *MaterialReloader) Revision() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision
}

// Register loads the material YAML file at relPath, validates it, builds its initial
// gpu.Pipeline (when build != nil and the paired ShaderReloader has a GPU device),
// and tracks it under name for live reloading.
func (r *MaterialReloader) Register(name, relPath string, build MaterialPipelineBuilder) (*MaterialDef, *gpu.Pipeline, error) {
	if r == nil {
		return nil, nil, errors.New("hotreload: nil MaterialReloader")
	}
	if name == "" {
		return nil, nil, errors.New("hotreload: empty material name")
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, nil, errors.New("hotreload: MaterialReloader is closed")
	}
	clean := normalizeRelPath(relPath)
	data, err := r.readFile(clean)
	if err != nil {
		re := makeReloadError(name, clean, ReloadKindMaterial, err)
		r.recordError(re)
		return nil, nil, err
	}
	return r.RegisterBytes(name, clean, data, build)
}

// RegisterBytes parses data as the material YAML at relPath, builds its initial
// gpu.Pipeline (when build != nil and the paired ShaderReloader has a GPU device),
// and tracks it under name for live reloading.
func (r *MaterialReloader) RegisterBytes(name, relPath string, data []byte, build MaterialPipelineBuilder) (*MaterialDef, *gpu.Pipeline, error) {
	if r == nil {
		return nil, nil, errors.New("hotreload: nil MaterialReloader")
	}
	if name == "" {
		return nil, nil, errors.New("hotreload: empty material name")
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, nil, errors.New("hotreload: MaterialReloader is closed")
	}
	clean := normalizeRelPath(relPath)
	def, err := ParseMaterialFile(clean, data)
	if err != nil {
		re := makeReloadError(name, clean, ReloadKindMaterial, err)
		r.recordError(re)
		return nil, nil, err
	}

	pipe, err := r.buildPipeline(clean, def, build)
	if err != nil {
		re := makeReloadError(name, clean, ReloadKindMaterial, err)
		r.recordError(re)
		return nil, nil, err
	}

	var oldPipe *gpu.Pipeline
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		if pipe != nil {
			_ = pipe.Close()
		}
		return nil, nil, errors.New("hotreload: MaterialReloader is closed")
	}
	if prev, ok := r.materials[name]; ok {
		oldPipe = prev.pipeline
		r.removeByPathLocked(prev.path, name)
	}
	entry := &materialEntry{
		name:     name,
		path:     clean,
		def:      def,
		build:    build,
		pipeline: pipe,
	}
	r.materials[name] = entry
	if clean != "" {
		r.byPath[clean] = append(r.byPath[clean], name)
		slices.Sort(r.byPath[clean])
	}
	delete(r.errors, name)
	r.mu.Unlock()

	if oldPipe != nil {
		_ = oldPipe.Close()
	}
	return def.Clone(), pipe, nil
}

// Material returns a snapshot copy of the currently active MaterialDef for name, or nil.
func (r *MaterialReloader) Material(name string) *MaterialDef {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.materials[name]; ok {
		return e.def.Clone()
	}
	return nil
}

// Pipeline returns the currently active *gpu.Pipeline for name, or nil.
// While a broken material or shader edit is on disk, Pipeline continues returning the last good pipeline.
func (r *MaterialReloader) Pipeline(name string) *gpu.Pipeline {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.materials[name]; ok {
		return e.pipeline
	}
	return nil
}

// MaterialNames returns the sorted list of registered material names.
func (r *MaterialReloader) MaterialNames() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.materials))
	for name := range r.materials {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// MaterialPath returns the slash-separated relative file path registered for material name.
func (r *MaterialReloader) MaterialPath(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.materials[name]
	if !ok {
		return "", false
	}
	return e.path, true
}

// ReloadMaterialFile resolves nameOrPath (a registered material name or relative path), reads fresh YAML bytes from the reloader's configured file system, and calls ReloadMaterial. It reports the resolved path, file byte length, whether the material is tracked, and any error.
func (r *MaterialReloader) ReloadMaterialFile(nameOrPath string) (string, int64, bool, error) {
	if r == nil || nameOrPath == "" {
		return "", 0, false, nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", 0, false, nil
	}
	clean := ""
	tracked := false
	if e, ok := r.materials[nameOrPath]; ok {
		clean = e.path
		tracked = true
	} else {
		cand := normalizeRelPath(nameOrPath)
		if len(r.byPath[cand]) > 0 {
			clean = cand
			tracked = true
		}
	}
	r.mu.Unlock()
	if !tracked {
		return "", 0, false, nil
	}
	data, err := r.readFile(clean)
	if err != nil {
		return clean, 0, true, err
	}
	reloaded, err := r.ReloadMaterial(clean, data)
	return clean, int64(len(data)), reloaded, err
}

// LastError returns the most recent unresolved ReloadError for name and whether one is active.
func (r *MaterialReloader) LastError(name string) (ReloadError, bool) {
	if r == nil {
		return ReloadError{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	re, ok := r.errors[name]
	return re, ok
}

// Errors returns all currently unresolved material ReloadErrors, sorted by Name.
func (r *MaterialReloader) Errors() []ReloadError {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errors) == 0 {
		return nil
	}
	out := make([]ReloadError, 0, len(r.errors))
	for _, re := range r.errors {
		out = append(out, re)
	}
	slices.SortFunc(out, func(a, b ReloadError) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// ClearError clears any recorded ReloadError for name.
func (r *MaterialReloader) ClearError(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.errors, name)
	r.mu.Unlock()
}

// Watch attaches r to w so edited material YAML files (.material.yaml, .mat.yaml,
// .material.yml, .mat.yml) reload on the engine goroutine. When r is paired with a
// ShaderReloader, Watch also attaches the ShaderReloader to w (idempotently) so
// editing a referenced shader automatically reloads every dependent material.
func (r *MaterialReloader) Watch(w *Watcher) {
	if r == nil || w == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	if _, already := r.watched[w]; already {
		r.mu.Unlock()
		return
	}
	r.watched[w] = struct{}{}
	if r.fsys == nil && w.Dir() != "" {
		r.fsys = os.DirFS(w.Dir())
	}
	r.mu.Unlock()

	if r.shaders != nil {
		r.shaders.Watch(w)
	}

	onFile := func(relPath string, data []byte) {
		_, _ = r.ReloadMaterial(relPath, data)
	}
	for _, ext := range MaterialExtensions() {
		w.WatchPattern(ext, onFile)
	}
}

// ReloadMaterial parses data as the new contents of relPath and atomically swaps
// every material registered at relPath whose candidate parses, validates, and builds
// cleanly. Untracked paths return (false, nil). On any error, the previous working
// material and pipeline remain active and OnError is called without crashing.
func (r *MaterialReloader) ReloadMaterial(relPath string, data []byte) (bool, error) {
	if r == nil {
		return false, nil
	}
	clean := normalizeRelPath(relPath)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false, nil
	}
	names := slices.Clone(r.byPath[clean])
	entries := make([]*materialEntry, 0, len(names))
	for _, name := range names {
		if e, ok := r.materials[name]; ok {
			entries = append(entries, e)
		}
	}
	r.mu.Unlock()

	if len(entries) == 0 {
		return false, nil
	}

	candDef, err := ParseMaterialFile(clean, data)
	if err != nil {
		var errs []ReloadError
		r.mu.Lock()
		for _, e := range entries {
			re := makeReloadError(e.name, clean, ReloadKindMaterial, err)
			r.errors[e.name] = re
			errs = append(errs, re)
		}
		r.mu.Unlock()
		for _, re := range errs {
			if r.cfg.onError != nil {
				r.cfg.onError(re)
			}
		}
		return true, err
	}

	var (
		firstErr error
		oldPipes []*gpu.Pipeline
		reloaded []string
		errList  []ReloadError
	)

	for _, e := range entries {
		candPipe, buildErr := r.buildPipeline(clean, candDef, e.build)
		if buildErr != nil {
			re := makeReloadError(e.name, clean, ReloadKindMaterial, buildErr)
			r.mu.Lock()
			r.errors[e.name] = re
			r.mu.Unlock()
			errList = append(errList, re)
			if firstErr == nil {
				firstErr = buildErr
			}
			continue
		}

		r.mu.Lock()
		if cur, ok := r.materials[e.name]; !r.closed && ok && cur == e {
			if cur.pipeline != nil {
				oldPipes = append(oldPipes, cur.pipeline)
			}
			cur.def = candDef.Clone()
			cur.pipeline = candPipe
			delete(r.errors, e.name)
			reloaded = append(reloaded, e.name)
		} else if candPipe != nil {
			oldPipes = append(oldPipes, candPipe)
		}
		r.mu.Unlock()
	}

	for _, old := range oldPipes {
		_ = old.Close()
	}
	if len(reloaded) > 0 {
		r.bumpRevision()
	}
	for _, re := range errList {
		if r.cfg.onError != nil {
			r.cfg.onError(re)
		}
	}
	if r.cfg.onReload != nil {
		for _, name := range reloaded {
			r.cfg.onReload(name, clean)
		}
	}
	return true, firstErr
}

// Close releases all active GPU pipelines owned by r. Safe to call more than once.
func (r *MaterialReloader) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	pipes := make([]*gpu.Pipeline, 0, len(r.materials))
	for _, e := range r.materials {
		if e.pipeline != nil {
			pipes = append(pipes, e.pipeline)
			e.pipeline = nil
		}
	}
	r.mu.Unlock()

	for _, p := range pipes {
		_ = p.Close()
	}
}

func (r *MaterialReloader) tracksShader(shaderPath string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	for _, e := range r.materials {
		if e.def != nil && (e.def.EffectiveVertexShader() == shaderPath || e.def.EffectiveFragmentShader() == shaderPath) {
			return true
		}
	}
	return false
}

func (r *MaterialReloader) onShaderError(shaderPath string, base ReloadError) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	var affected []ReloadError
	for _, e := range r.materials {
		if e.def == nil {
			continue
		}
		if e.def.EffectiveVertexShader() == shaderPath || e.def.EffectiveFragmentShader() == shaderPath {
			re := base
			re.Name = e.name
			re.Kind = ReloadKindMaterial
			r.errors[e.name] = re
			affected = append(affected, re)
		}
	}
	r.mu.Unlock()

	slices.SortFunc(affected, func(a, b ReloadError) int { return strings.Compare(a.Name, b.Name) })
	for _, re := range affected {
		if r.cfg.onError != nil {
			r.cfg.onError(re)
		}
	}
}

func (r *MaterialReloader) onShaderReloaded(shaderPath string, _ *gpu.ShaderModule) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	type matJob struct {
		entry *materialEntry
		def   *MaterialDef
	}
	var jobs []matJob
	for _, e := range r.materials {
		if e.def == nil {
			continue
		}
		if e.def.EffectiveVertexShader() == shaderPath || e.def.EffectiveFragmentShader() == shaderPath {
			jobs = append(jobs, matJob{entry: e, def: e.def.Clone()})
		}
	}
	r.mu.Unlock()

	if len(jobs) == 0 {
		return
	}
	slices.SortFunc(jobs, func(a, b matJob) int { return strings.Compare(a.entry.name, b.entry.name) })

	var (
		oldPipes []*gpu.Pipeline
		reloaded []string
		errList  []ReloadError
	)

	for _, job := range jobs {
		candPipe, err := r.buildPipeline(shaderPath, job.def, job.entry.build)
		if err != nil {
			re := makeReloadError(job.entry.name, shaderPath, ReloadKindMaterial, err)
			r.mu.Lock()
			r.errors[job.entry.name] = re
			r.mu.Unlock()
			errList = append(errList, re)
			continue
		}

		r.mu.Lock()
		if cur, ok := r.materials[job.entry.name]; !r.closed && ok && cur == job.entry {
			if cur.pipeline != nil {
				oldPipes = append(oldPipes, cur.pipeline)
			}
			cur.pipeline = candPipe
			delete(r.errors, job.entry.name)
			reloaded = append(reloaded, job.entry.name)
		} else if candPipe != nil {
			oldPipes = append(oldPipes, candPipe)
		}
		r.mu.Unlock()
	}

	for _, old := range oldPipes {
		_ = old.Close()
	}
	if len(reloaded) > 0 {
		r.bumpRevision()
	}
	for _, re := range errList {
		if r.cfg.onError != nil {
			r.cfg.onError(re)
		}
	}
	if r.cfg.onReload != nil {
		for _, name := range reloaded {
			r.cfg.onReload(name, shaderPath)
		}
	}
}

func (r *MaterialReloader) buildPipeline(errFile string, def *MaterialDef, build MaterialPipelineBuilder) (*gpu.Pipeline, error) {
	if build == nil || r.shaders == nil || r.shaders.Device() == nil {
		return nil, nil
	}
	vertPath := def.EffectiveVertexShader()
	fragPath := def.EffectiveFragmentShader()
	if vertPath == "" && fragPath == "" {
		return nil, &loaderr.Error{
			File:    errFile,
			Field:   "shader",
			Problem: "material has no shader path for pipeline creation",
			Fix:     "set shader (or vertexShader and fragmentShader) in the material YAML",
		}
	}

	var (
		vertMod *gpu.ShaderModule
		fragMod *gpu.ShaderModule
		err     error
	)
	if vertPath != "" {
		vertMod, err = r.shaders.ensureShader(vertPath)
		if err != nil {
			return nil, err
		}
	}
	if fragPath != "" {
		if fragPath == vertPath {
			fragMod = vertMod
		} else {
			fragMod, err = r.shaders.ensureShader(fragPath)
			if err != nil {
				return nil, err
			}
		}
	}

	desc, err := build(def.Clone(), vertMod, fragMod)
	if err != nil {
		return nil, shaderLoadError(errFile, err)
	}
	pipe, err := r.shaders.Device().CreatePipeline(nil, desc)
	if err != nil {
		return nil, shaderLoadError(errFile, err)
	}
	return pipe, nil
}

func (r *MaterialReloader) readFile(clean string) ([]byte, error) {
	r.mu.Lock()
	fsys := r.fsys
	r.mu.Unlock()
	if fsys == nil && r.shaders != nil {
		fsys = r.shaders.getFS()
	}
	if fsys == nil {
		return nil, &loaderr.Error{
			File:    clean,
			Problem: "no filesystem configured to load material",
			Fix:     "pass WithMaterialDir, WithMaterialFS, or attach a Watcher before registering",
		}
	}
	data, err := fs.ReadFile(fsys, clean)
	if err != nil {
		return nil, &loaderr.Error{
			File:    clean,
			Problem: err.Error(),
			Fix:     "check that the material file exists under the watched asset root",
			Err:     err,
		}
	}
	return data, nil
}

func (r *MaterialReloader) recordError(re ReloadError) {
	r.mu.Lock()
	r.errors[re.Name] = re
	r.mu.Unlock()
	if r.cfg.onError != nil {
		r.cfg.onError(re)
	}
}

func (r *MaterialReloader) removeByPathLocked(p, name string) {
	if p == "" {
		return
	}
	list := r.byPath[p]
	out := list[:0]
	for _, n := range list {
		if n != name {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		delete(r.byPath, p)
	} else {
		r.byPath[p] = out
	}
}

func (r *MaterialReloader) bumpRevision() {
	r.mu.Lock()
	r.revision++
	r.mu.Unlock()
	if r.Version != nil && r.cx != nil && !r.cx.Disposed() {
		r.Version.Set(r.Version.Get() + 1)
	}
}

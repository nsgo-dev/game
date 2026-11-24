package hotreload

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Recognized shader and material file extensions.
const (
	ShaderExtWGSL   = ".wgsl"
	ShaderExtKage   = ".kage"
	MaterialExtYAML = ".material.yaml"
	MaterialExtMat  = ".mat.yaml"
	MaterialExtYML  = ".material.yml"
	MaterialExtMatY = ".mat.yml"
)

// ReloadKind identifies what kind of GPU asset a ReloadError or reload event refers to.
type ReloadKind string

const (
	ReloadKindShader   ReloadKind = "shader"
	ReloadKindPipeline ReloadKind = "pipeline"
	ReloadKindMaterial ReloadKind = "material"
)

// ShaderExtensions returns the shader file extensions recognized by hotreload, in a fixed order.
func ShaderExtensions() []string {
	return []string{ShaderExtWGSL, ShaderExtKage}
}

// MaterialExtensions returns the material YAML file extensions recognized by hotreload, in a fixed order.
func MaterialExtensions() []string {
	return []string{MaterialExtYAML, MaterialExtMat, MaterialExtYML, MaterialExtMatY}
}

// IsShaderFile reports whether p has a recognized shader extension (.wgsl or .kage), matched case-insensitively.
func IsShaderFile(p string) bool {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(p, "\\", "/")))
	return ext == ShaderExtWGSL || ext == ShaderExtKage
}

// IsMaterialFile reports whether p has a recognized material YAML extension (.material.yaml, .mat.yaml, .material.yml, or .mat.yml), matched case-insensitively.
func IsMaterialFile(p string) bool {
	lower := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	for _, ext := range MaterialExtensions() {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// ReloadError records why a shader, pipeline, or material failed to reload.
// It implements error and unwraps to Err (typically a *loaderr.Error), so
// errors.As and loaderr.As inspect the underlying diagnostic directly.
type ReloadError struct {
	Name    string     // registered shader, pipeline, or material name
	Path    string     // slash-separated file path that failed
	Kind    ReloadKind // ReloadKindShader, ReloadKindPipeline, or ReloadKindMaterial
	Line    int        // 1-based source line, 0 when unknown
	Column  int        // 1-based source column, 0 when unknown
	Message string     // human-readable diagnostic message
	Err     error      // underlying error, typically *loaderr.Error
	At      time.Time  // wall-clock timestamp when the failure was recorded
}

// Error returns a formatted diagnostic string for the failed reload.
func (e ReloadError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Path != "" {
		if e.Line > 0 {
			if e.Column > 0 {
				return fmt.Sprintf("%s:%d:%d: %s", e.Path, e.Line, e.Column, e.Message)
			}
			return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Message)
		}
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e ReloadError) Unwrap() error { return e.Err }

func makeReloadError(name, relPath string, kind ReloadKind, err error) ReloadError {
	re := ReloadError{
		Name: name,
		Path: relPath,
		Kind: kind,
		Err:  err,
		At:   time.Now(),
	}
	if le, ok := loaderr.As(err); ok {
		re.Line = le.Line
		re.Column = le.Column
		re.Message = le.Problem
		if re.Message == "" {
			re.Message = le.Error()
		}
		if re.Path == "" {
			re.Path = le.File
		}
	} else if err != nil {
		re.Message = err.Error()
	}
	return re
}

// CompileShaderSource compiles a WGSL (.wgsl) or Kage (.kage) shader from data.
// Compile and validation errors are returned as a structured *loaderr.Error naming
// filePath and the 1-based line and column when the compiler reports them.
func CompileShaderSource(filePath string, data []byte) (*gpu.ShaderModule, error) {
	cleanPath := normalizeRelPath(filePath)
	ext := strings.ToLower(path.Ext(cleanPath))
	if ext != ShaderExtWGSL && ext != ShaderExtKage {
		return nil, &loaderr.Error{
			File:     cleanPath,
			Problem:  fmt.Sprintf("unsupported shader extension %q", ext),
			Expected: ".wgsl or .kage",
			Fix:      "use a .wgsl or .kage shader file",
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &loaderr.Error{
			File:    cleanPath,
			Problem: "empty shader source",
			Fix:     "add a shader entry point and save again",
		}
	}

	var (
		mod *gpu.ShaderModule
		err error
	)
	switch ext {
	case ShaderExtWGSL:
		mod, err = gpu.CompileShader(gpu.ShaderDesc{Label: cleanPath, Source: string(data)})
	case ShaderExtKage:
		mod, err = gpu.CompileKage(string(data))
	}
	if err != nil {
		return nil, shaderLoadError(cleanPath, err)
	}
	return mod, nil
}

// shaderLoadError converts a GPU shader compilation or pipeline creation error
// into a *loaderr.Error with file, line, column, and problem extracted when available.
func shaderLoadError(filePath string, err error) *loaderr.Error {
	if le, ok := loaderr.As(err); ok {
		c := *le
		if c.File == "" {
			c.File = filePath
		}
		return &c
	}
	raw := err.Error()
	var ge gpu.Error
	if errors.As(err, &ge) && ge.Message != "" {
		raw = ge.Message
	}
	line, col, msg := parseShaderDiagnostic(raw)
	return &loaderr.Error{
		File:    filePath,
		Line:    line,
		Column:  col,
		Problem: msg,
		Fix:     "fix the shader error and save again; the previous pipeline stays active until it compiles",
		Err:     err,
	}
}

// parseShaderDiagnostic extracts a 1-based line, column, and message from WGSL
// ("shader: 12:5: ...") and Kage ("12:5: ..." or "file:12:5: ...") compiler errors.
func parseShaderDiagnostic(raw string) (line, col int, msg string) {
	trimmed := strings.TrimSpace(raw)
	firstLine, rest, hasRest := strings.Cut(trimmed, "\n")
	s := strings.TrimSpace(firstLine)
	s = strings.TrimPrefix(s, "shader:")
	s = strings.TrimSpace(s)

	if l, c, tail, ok := cutLineCol(s); ok {
		line, col, msg = l, c, strings.TrimSpace(tail)
		if hasRest {
			msg += "\n" + rest
		}
		return line, col, msg
	}

	// Kage errors may carry a filename prefix before line:col, e.g. "shader.kage:3:7: msg".
	if _, afterFile, ok := strings.Cut(s, ":"); ok {
		if l, c, tail, ok := cutLineCol(afterFile); ok {
			line, col, msg = l, c, strings.TrimSpace(tail)
			if hasRest {
				msg += "\n" + rest
			}
			return line, col, msg
		}
	}

	if s != "" {
		if hasRest {
			return 0, 0, s + "\n" + rest
		}
		return 0, 0, s
	}
	return 0, 0, trimmed
}

func cutLineCol(s string) (line, col int, tail string, ok bool) {
	lineStr, rest, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, "", false
	}
	l, err := strconv.Atoi(strings.TrimSpace(lineStr))
	if err != nil || l <= 0 {
		return 0, 0, "", false
	}
	colStr, afterCol, found := strings.Cut(rest, ":")
	if !found {
		return l, 0, rest, true
	}
	c, err := strconv.Atoi(strings.TrimSpace(colStr))
	if err != nil || c <= 0 {
		return l, 0, rest, true
	}
	return l, c, afterCol, true
}

// PipelineSpec describes a hot-reloadable GPU render pipeline and the shader file(s)
// it depends on.
type PipelineSpec struct {
	// Shader is the slash-separated path of a single shader file providing both
	// vertex and fragment stages. Used whenever VertexShader or FragmentShader is empty.
	Shader string
	// VertexShader overrides Shader for the vertex stage when split across two files.
	VertexShader string
	// FragmentShader overrides Shader for the fragment stage when split across two files.
	FragmentShader string
	// Build constructs the gpu.PipelineDesc from the newly compiled vertex and
	// fragment ShaderModules. Returning an error rejects the candidate and keeps
	// the previous working pipeline active.
	Build func(vertex, fragment *gpu.ShaderModule) (gpu.PipelineDesc, error)
}

func (s PipelineSpec) resolvedPaths() (vertPath, fragPath string, err error) {
	vertPath = normalizeRelPath(s.VertexShader)
	if vertPath == "" {
		vertPath = normalizeRelPath(s.Shader)
	}
	fragPath = normalizeRelPath(s.FragmentShader)
	if fragPath == "" {
		fragPath = normalizeRelPath(s.Shader)
	}
	if vertPath == "" && fragPath == "" {
		return "", "", errors.New("hotreload: pipeline spec requires Shader or VertexShader/FragmentShader")
	}
	if vertPath != "" && !IsShaderFile(vertPath) {
		return "", "", fmt.Errorf("hotreload: vertex shader %q must end in .wgsl or .kage", vertPath)
	}
	if fragPath != "" && !IsShaderFile(fragPath) {
		return "", "", fmt.Errorf("hotreload: fragment shader %q must end in .wgsl or .kage", fragPath)
	}
	return vertPath, fragPath, nil
}

// ShaderOption configures a ShaderReloader.
type ShaderOption func(*shaderConfig)

type shaderConfig struct {
	fsys     fs.FS
	onReload func(name, path string)
	onError  func(ReloadError)
}

// WithShaderFS sets the file system used for initial shader loads.
func WithShaderFS(fsys fs.FS) ShaderOption {
	return func(c *shaderConfig) { c.fsys = fsys }
}

// WithShaderDir sets a directory on disk used for initial shader loads.
func WithShaderDir(dir string) ShaderOption {
	return func(c *shaderConfig) {
		if dir != "" {
			c.fsys = os.DirFS(filepath.Clean(dir))
		}
	}
}

// WithOnShaderReload sets a callback invoked on the engine goroutine whenever a
// shader or pipeline reloads successfully.
func WithOnShaderReload(fn func(name, path string)) ShaderOption {
	return func(c *shaderConfig) { c.onReload = fn }
}

// WithOnShaderError sets a callback invoked whenever a shader or pipeline fails
// to compile or validate. The previous working pipeline remains active.
func WithOnShaderError(fn func(ReloadError)) ShaderOption {
	return func(c *shaderConfig) { c.onError = fn }
}

type shaderEntry struct {
	path   string
	module *gpu.ShaderModule
}

type pipelineEntry struct {
	name     string
	vertPath string
	fragPath string
	build    func(vertex, fragment *gpu.ShaderModule) (gpu.PipelineDesc, error)
	pipeline *gpu.Pipeline
}

type shaderListener struct {
	onReload func(relPath string, mod *gpu.ShaderModule)
	onError  func(relPath string, re ReloadError)
	tracks   func(relPath string) bool
}

// ShaderReloader manages live-reloadable WGSL/Kage shaders and GPU render pipelines.
//
// When a watched shader file changes, ShaderReloader compiles the candidate module
// and builds every dependent candidate gpu.Pipeline before swapping. On any compile
// or pipeline validation error, the previous working pipeline stays active, a
// ReloadError is recorded, and OnError is invoked without crashing. When a later
// edit compiles cleanly, the candidate pipeline swaps in atomically, the replaced
// pipeline is closed safely, and the error clears.
type ShaderReloader struct {
	// Version increments on every successful reload when created with a non-nil *core.Scope.
	// Reading Version.Get() inside a reactive view or Draw callback triggers a redraw.
	Version *core.Signal[int]

	cx  *core.Scope
	dev *gpu.Device
	cfg shaderConfig

	mu          sync.Mutex
	fsys        fs.FS
	shaders     map[string]*shaderEntry   // relPath -> compiled module
	namedShader map[string]string         // name -> relPath
	pipelines   map[string]*pipelineEntry // name -> pipeline entry
	errors      map[string]ReloadError    // name (or relPath) -> last error
	listeners   []shaderListener
	watched     map[*Watcher]struct{}
	revision    uint64
	closed      bool
}

// NewShaderReloader creates a ShaderReloader bound to dev (which may be nil when
// only compiling shaders without building GPU pipelines). When cx is non-nil,
// Version tracks reload generations reactively and Close is registered on cx.Cleanup.
func NewShaderReloader(cx *core.Scope, dev *gpu.Device, opts ...ShaderOption) *ShaderReloader {
	var cfg shaderConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	var ver *core.Signal[int]
	if cx != nil {
		ver = core.NewSignal(cx, 0)
	}
	r := &ShaderReloader{
		Version:     ver,
		cx:          cx,
		dev:         dev,
		cfg:         cfg,
		fsys:        cfg.fsys,
		shaders:     make(map[string]*shaderEntry),
		namedShader: make(map[string]string),
		pipelines:   make(map[string]*pipelineEntry),
		errors:      make(map[string]ReloadError),
		watched:     make(map[*Watcher]struct{}),
	}
	if cx != nil {
		cx.Cleanup(r.Close)
	}
	return r
}

// Device returns the GPU device used to create pipelines, or nil.
func (r *ShaderReloader) Device() *gpu.Device {
	if r == nil {
		return nil
	}
	return r.dev
}

// Revision returns the monotonic count of successful shader and pipeline reloads.
func (r *ShaderReloader) Revision() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision
}

// RegisterShader loads and compiles the shader at relPath from the reloader's
// filesystem and registers it under name.
func (r *ShaderReloader) RegisterShader(name, relPath string) (*gpu.ShaderModule, error) {
	if r == nil {
		return nil, errors.New("hotreload: nil ShaderReloader")
	}
	if name == "" {
		return nil, errors.New("hotreload: empty shader name")
	}
	clean := normalizeRelPath(relPath)
	data, err := r.readFile(clean)
	if err != nil {
		re := makeReloadError(name, clean, ReloadKindShader, err)
		r.recordError(re)
		return nil, err
	}
	return r.RegisterShaderBytes(name, clean, data)
}

// RegisterShaderBytes compiles data as the shader at relPath and registers it under name.
func (r *ShaderReloader) RegisterShaderBytes(name, relPath string, data []byte) (*gpu.ShaderModule, error) {
	if r == nil {
		return nil, errors.New("hotreload: nil ShaderReloader")
	}
	if name == "" {
		return nil, errors.New("hotreload: empty shader name")
	}
	clean := normalizeRelPath(relPath)
	mod, err := CompileShaderSource(clean, data)
	if err != nil {
		re := makeReloadError(name, clean, ReloadKindShader, err)
		r.recordError(re)
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("hotreload: ShaderReloader is closed")
	}
	r.shaders[clean] = &shaderEntry{path: clean, module: mod}
	r.namedShader[name] = clean
	delete(r.errors, name)
	delete(r.errors, clean)
	r.mu.Unlock()
	return mod, nil
}

// RegisterPipeline loads and compiles the shader(s) named by spec, builds the
// initial gpu.Pipeline, and registers it under name for automatic hot reload.
func (r *ShaderReloader) RegisterPipeline(name string, spec PipelineSpec) (*gpu.Pipeline, error) {
	if r == nil {
		return nil, errors.New("hotreload: nil ShaderReloader")
	}
	if name == "" {
		return nil, errors.New("hotreload: empty pipeline name")
	}
	if r.dev == nil {
		return nil, errors.New("hotreload: RegisterPipeline requires a non-nil *gpu.Device")
	}
	if spec.Build == nil {
		return nil, errors.New("hotreload: PipelineSpec.Build is nil")
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil, errors.New("hotreload: ShaderReloader is closed")
	}
	vertPath, fragPath, err := spec.resolvedPaths()
	if err != nil {
		return nil, err
	}

	var vertMod, fragMod *gpu.ShaderModule
	if vertPath != "" {
		vertMod, err = r.ensureShader(vertPath)
		if err != nil {
			re := makeReloadError(name, vertPath, ReloadKindPipeline, err)
			r.recordError(re)
			return nil, err
		}
	}
	if fragPath != "" {
		if fragPath == vertPath {
			fragMod = vertMod
		} else {
			fragMod, err = r.ensureShader(fragPath)
			if err != nil {
				re := makeReloadError(name, fragPath, ReloadKindPipeline, err)
				r.recordError(re)
				return nil, err
			}
		}
	}

	desc, err := spec.Build(vertMod, fragMod)
	if err != nil {
		errPath := vertPath
		if errPath == "" {
			errPath = fragPath
		}
		le := shaderLoadError(errPath, err)
		re := makeReloadError(name, errPath, ReloadKindPipeline, le)
		r.recordError(re)
		return nil, le
	}
	pipe, err := r.dev.CreatePipeline(nil, desc)
	if err != nil {
		errPath := vertPath
		if errPath == "" {
			errPath = fragPath
		}
		le := shaderLoadError(errPath, err)
		re := makeReloadError(name, errPath, ReloadKindPipeline, le)
		r.recordError(re)
		return nil, le
	}

	var old *gpu.Pipeline
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		if pipe != nil {
			_ = pipe.Close()
		}
		return nil, errors.New("hotreload: ShaderReloader is closed")
	}
	if prev, ok := r.pipelines[name]; ok {
		old = prev.pipeline
	}
	r.pipelines[name] = &pipelineEntry{
		name:     name,
		vertPath: vertPath,
		fragPath: fragPath,
		build:    spec.Build,
		pipeline: pipe,
	}
	delete(r.errors, name)
	r.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return pipe, nil
}

// Shader returns the currently active compiled *gpu.ShaderModule registered under name, or nil.
func (r *ShaderReloader) Shader(name string) *gpu.ShaderModule {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.namedShader[name]
	if !ok {
		return nil
	}
	if e, ok := r.shaders[p]; ok {
		return e.module
	}
	return nil
}

// ShaderByPath returns the currently active compiled *gpu.ShaderModule for relPath, or nil.
func (r *ShaderReloader) ShaderByPath(relPath string) *gpu.ShaderModule {
	if r == nil {
		return nil
	}
	clean := normalizeRelPath(relPath)
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.shaders[clean]; ok {
		return e.module
	}
	return nil
}

// Pipeline returns the currently active *gpu.Pipeline registered under name, or nil.
// While a broken shader edit is on disk, Pipeline continues returning the last good pipeline.
func (r *ShaderReloader) Pipeline(name string) *gpu.Pipeline {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.pipelines[name]; ok {
		return e.pipeline
	}
	return nil
}

// ShaderNames returns the sorted list of explicitly registered shader names.
func (r *ShaderReloader) ShaderNames() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.namedShader))
	for name := range r.namedShader {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ShaderPath returns the slash-separated relative file path registered for shader name.
func (r *ShaderReloader) ShaderPath(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.namedShader[name]
	return p, ok
}

// ReloadShaderFile resolves nameOrPath (a registered shader name or relative path),
// reads fresh source bytes from the reloader's configured file system, and calls ReloadShader.
// It reports the resolved path, source byte length, whether the shader is tracked, and any error.
func (r *ShaderReloader) ReloadShaderFile(nameOrPath string) (string, int64, bool, error) {
	if r == nil || nameOrPath == "" {
		return "", 0, false, nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", 0, false, nil
	}
	clean := ""
	if p, ok := r.namedShader[nameOrPath]; ok {
		clean = p
	} else {
		clean = normalizeRelPath(nameOrPath)
	}
	tracked, _, _, _ := r.snapshotDependentsLocked(clean)
	r.mu.Unlock()
	if !tracked {
		return "", 0, false, nil
	}
	data, err := r.readFile(clean)
	if err != nil {
		return clean, 0, true, err
	}
	reloaded, err := r.ReloadShader(clean, data)
	return clean, int64(len(data)), reloaded, err
}

// PipelineNames returns the sorted list of registered pipeline names.
func (r *ShaderReloader) PipelineNames() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.pipelines))
	for name := range r.pipelines {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// LastError returns the most recent unresolved ReloadError for name (a shader name,
// pipeline name, or shader path), and whether one is active.
func (r *ShaderReloader) LastError(name string) (ReloadError, bool) {
	if r == nil {
		return ReloadError{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	re, ok := r.errors[name]
	return re, ok
}

// Errors returns all currently unresolved ReloadErrors, sorted by Name for deterministic display.
func (r *ShaderReloader) Errors() []ReloadError {
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
func (r *ShaderReloader) ClearError(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.errors, name)
	r.mu.Unlock()
}

// Watch attaches r to w so edited .wgsl and .kage files under w.Dir() are compiled
// and reloaded on the engine goroutine. Calling Watch with a nil Watcher (for example
// from NewDevWatcher outside `game dev` or in `-tags release`) is a safe no-op.
// Calling Watch multiple times with the same Watcher registers patterns only once.
func (r *ShaderReloader) Watch(w *Watcher) {
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

	onFile := func(relPath string, data []byte) {
		_, _ = r.ReloadShader(relPath, data)
	}
	for _, ext := range ShaderExtensions() {
		w.WatchPattern(ext, onFile)
	}
}

// ReloadShader compiles data as the new source for relPath and atomically swaps
// every affected shader and pipeline whose candidate builds cleanly.
//
// It reports whether relPath is tracked by any registered shader, pipeline, or
// dependent MaterialReloader; untracked files return (false, nil). On a compile or
// pipeline creation error, the previous working shader and pipeline stay active,
// a ReloadError is recorded, and OnError is called without panicking.
func (r *ShaderReloader) ReloadShader(relPath string, data []byte) (bool, error) {
	if r == nil {
		return false, nil
	}
	clean := normalizeRelPath(relPath)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false, nil
	}
	tracked, shaderNames, depPipelines, listeners := r.snapshotDependentsLocked(clean)
	r.mu.Unlock()

	if !tracked {
		return false, nil
	}

	mod, err := CompileShaderSource(clean, data)
	if err != nil {
		// Compilation failed: keep all previous shaders and pipelines active!
		var recorded []ReloadError
		r.mu.Lock()
		for _, name := range shaderNames {
			re := makeReloadError(name, clean, ReloadKindShader, err)
			r.errors[name] = re
			recorded = append(recorded, re)
		}
		for _, p := range depPipelines {
			re := makeReloadError(p.name, clean, ReloadKindPipeline, err)
			r.errors[p.name] = re
			recorded = append(recorded, re)
		}
		if len(shaderNames) == 0 && len(depPipelines) == 0 {
			re := makeReloadError(clean, clean, ReloadKindShader, err)
			r.errors[clean] = re
			recorded = append(recorded, re)
		}
		r.mu.Unlock()

		for _, re := range recorded {
			if r.cfg.onError != nil {
				r.cfg.onError(re)
			}
		}
		baseErr := makeReloadError(clean, clean, ReloadKindShader, err)
		for _, l := range listeners {
			if l.onError != nil {
				l.onError(clean, baseErr)
			}
		}
		return true, err
	}

	// Candidate shader compiled cleanly. Store it so dependent pipelines and materials
	// can build candidates against it. Any pipeline whose candidate fails keeps its
	// old working *gpu.Pipeline.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false, nil
	}
	r.shaders[clean] = &shaderEntry{path: clean, module: mod}
	delete(r.errors, clean)
	for _, name := range shaderNames {
		delete(r.errors, name)
	}
	// Resolve current stage modules for each dependent pipeline.
	type pipeJob struct {
		entry   *pipelineEntry
		vertMod *gpu.ShaderModule
		fragMod *gpu.ShaderModule
	}
	jobs := make([]pipeJob, 0, len(depPipelines))
	for _, p := range depPipelines {
		var vm, fm *gpu.ShaderModule
		if p.vertPath != "" {
			if e, ok := r.shaders[p.vertPath]; ok {
				vm = e.module
			}
		}
		if p.fragPath != "" {
			if e, ok := r.shaders[p.fragPath]; ok {
				fm = e.module
			}
		}
		jobs = append(jobs, pipeJob{entry: p, vertMod: vm, fragMod: fm})
	}
	r.mu.Unlock()

	var (
		firstErr error
		reloaded []string
		oldPipes []*gpu.Pipeline
		pipeErrs []ReloadError
		anySwaps = len(shaderNames) > 0
	)

	for _, job := range jobs {
		desc, buildErr := job.entry.build(job.vertMod, job.fragMod)
		var cand *gpu.Pipeline
		if buildErr == nil {
			cand, buildErr = r.dev.CreatePipeline(nil, desc)
		}
		if buildErr != nil {
			le := shaderLoadError(clean, buildErr)
			re := makeReloadError(job.entry.name, clean, ReloadKindPipeline, le)
			r.mu.Lock()
			r.errors[job.entry.name] = re
			r.mu.Unlock()
			pipeErrs = append(pipeErrs, re)
			if firstErr == nil {
				firstErr = le
			}
			continue
		}

		// Candidate pipeline succeeded: swap atomically and schedule old pipeline disposal.
		r.mu.Lock()
		if cur, ok := r.pipelines[job.entry.name]; !r.closed && ok && cur == job.entry {
			if cur.pipeline != nil {
				oldPipes = append(oldPipes, cur.pipeline)
			}
			cur.pipeline = cand
			delete(r.errors, job.entry.name)
			reloaded = append(reloaded, job.entry.name)
			anySwaps = true
		} else if cand != nil {
			// Pipeline was replaced or closed concurrently; discard candidate.
			oldPipes = append(oldPipes, cand)
		}
		r.mu.Unlock()
	}

	for _, old := range oldPipes {
		_ = old.Close()
	}

	if anySwaps {
		r.bumpRevision()
	}

	for _, re := range pipeErrs {
		if r.cfg.onError != nil {
			r.cfg.onError(re)
		}
	}
	if r.cfg.onReload != nil {
		for _, name := range shaderNames {
			r.cfg.onReload(name, clean)
		}
		for _, name := range reloaded {
			r.cfg.onReload(name, clean)
		}
	}

	for _, l := range listeners {
		if l.onReload != nil {
			l.onReload(clean, mod)
		}
	}

	return true, firstErr
}

// Close releases all active GPU render pipelines owned by r. It is safe to call
// more than once and is registered automatically on cx.Cleanup when cx is non-nil.
func (r *ShaderReloader) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	pipes := make([]*gpu.Pipeline, 0, len(r.pipelines))
	for _, p := range r.pipelines {
		if p.pipeline != nil {
			pipes = append(pipes, p.pipeline)
			p.pipeline = nil
		}
	}
	r.mu.Unlock()

	for _, p := range pipes {
		_ = p.Close()
	}
}

func (r *ShaderReloader) addListener(l shaderListener) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.listeners = append(r.listeners, l)
	r.mu.Unlock()
}

func (r *ShaderReloader) setFSIfNil(fsys fs.FS) {
	if r == nil || fsys == nil {
		return
	}
	r.mu.Lock()
	if r.fsys == nil {
		r.fsys = fsys
	}
	r.mu.Unlock()
}

func (r *ShaderReloader) getFS() fs.FS {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fsys
}

func (r *ShaderReloader) ensureShader(relPath string) (*gpu.ShaderModule, error) {
	clean := normalizeRelPath(relPath)
	r.mu.Lock()
	if e, ok := r.shaders[clean]; ok && e.module != nil {
		mod := e.module
		r.mu.Unlock()
		return mod, nil
	}
	r.mu.Unlock()

	data, err := r.readFile(clean)
	if err != nil {
		return nil, err
	}
	mod, err := CompileShaderSource(clean, data)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.shaders[clean] = &shaderEntry{path: clean, module: mod}
	delete(r.errors, clean)
	r.mu.Unlock()
	return mod, nil
}

func (r *ShaderReloader) readFile(relPath string) ([]byte, error) {
	clean := normalizeRelPath(relPath)
	r.mu.Lock()
	fsys := r.fsys
	r.mu.Unlock()
	if fsys == nil {
		return nil, &loaderr.Error{
			File:    clean,
			Problem: "no filesystem configured to load shader",
			Fix:     "pass WithShaderDir, WithShaderFS, or attach a Watcher before registering",
		}
	}
	data, err := fs.ReadFile(fsys, clean)
	if err != nil {
		return nil, &loaderr.Error{
			File:    clean,
			Problem: err.Error(),
			Fix:     "check that the shader file exists under the watched asset root",
			Err:     err,
		}
	}
	return data, nil
}

func (r *ShaderReloader) recordError(re ReloadError) {
	r.mu.Lock()
	r.errors[re.Name] = re
	r.mu.Unlock()
	if r.cfg.onError != nil {
		r.cfg.onError(re)
	}
}

func (r *ShaderReloader) snapshotDependentsLocked(clean string) (tracked bool, shaderNames []string, depPipelines []*pipelineEntry, listeners []shaderListener) {
	if _, ok := r.shaders[clean]; ok {
		tracked = true
	}
	for name, p := range r.namedShader {
		if p == clean {
			tracked = true
			shaderNames = append(shaderNames, name)
		}
	}
	slices.Sort(shaderNames)

	for _, p := range r.pipelines {
		if p.vertPath == clean || p.fragPath == clean {
			tracked = true
			depPipelines = append(depPipelines, p)
		}
	}
	slices.SortFunc(depPipelines, func(a, b *pipelineEntry) int {
		return strings.Compare(a.name, b.name)
	})

	for _, l := range r.listeners {
		if l.tracks != nil && l.tracks(clean) {
			tracked = true
		}
		listeners = append(listeners, l)
	}
	return tracked, shaderNames, depPipelines, listeners
}

func (r *ShaderReloader) bumpRevision() {
	r.mu.Lock()
	r.revision++
	r.mu.Unlock()
	if r.Version != nil && r.cx != nil && !r.cx.Disposed() {
		r.Version.Set(r.Version.Get() + 1)
	}
}

func normalizeRelPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." {
		return ""
	}
	return clean
}

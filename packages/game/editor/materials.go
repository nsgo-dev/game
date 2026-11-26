package editor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ErrNoMaterial is returned by material editor operations when no material is open.
var ErrNoMaterial = errors.New("editor: no material is open")

// MaterialPreviewShape selects what the Material Editor's live preview renders.
type MaterialPreviewShape int

const (
	// MaterialPreviewSphere renders a 3D UV sphere with render3d.
	MaterialPreviewSphere MaterialPreviewShape = iota
	// MaterialPreviewCube renders a 3D unit cube with render3d.
	MaterialPreviewCube
	// MaterialPreviewSprite renders a 2D sprite preview with base color, texture, opacity and blend mode.
	MaterialPreviewSprite
)

// String returns a human-readable label for s.
func (s MaterialPreviewShape) String() string {
	switch s {
	case MaterialPreviewSphere:
		return "Sphere"
	case MaterialPreviewCube:
		return "Cube"
	case MaterialPreviewSprite:
		return "Sprite"
	default:
		return "Unknown"
	}
}

// MaterialPreviewSize is the width and height in pixels of the Material Editor's rendered 3D preview.
const MaterialPreviewSize = 128

// materialPreviewPath is the virtual material path used inside the single-object 3D preview scene.
const materialPreviewPath = "__material_preview__.material.yaml"

type materialColorPreview struct {
	active   bool
	emissive bool
	before   resource.Material
	pending  string
}

type materialEditorState struct {
	mat          resource.Material
	path         string
	relPath      string
	saved        []byte
	dirty        bool
	dirtyValid   bool
	previewShape MaterialPreviewShape
	colorPreview materialColorPreview

	previewKey       string
	previewSize      int
	previewShapeKey  MaterialPreviewShape
	previewProjPath  string
	previewAssetsRev uint64
	previewLights    *render3d.LightRig
	previewImg       *image.RGBA
	previewErr       error
	previewVersion   uint64

	texCacheKey string
	texCacheImg *image.RGBA
}

// verifyMaterialProjectPath checks symlink containment for absPath against the open project root.
// When mustContain is true (project-relative open/create), absPath must resolve strictly inside the open project.
// When mustContain is false (arbitrary SaveMaterialFile path), paths lexically inside the open project must not escape it via symlinks.
func (e *Editor) verifyMaterialProjectPath(absPath string, mustContain, allowMissing bool) error {
	if e == nil || e.ProjectPath == "" {
		if mustContain {
			return ErrNoProject
		}
		return nil
	}
	root := filepath.Clean(filepath.Dir(e.ProjectPath))
	cleanPath := filepath.Clean(absPath)
	if !mustContain {
		lexRel, err := filepath.Rel(root, cleanPath)
		if err != nil || lexRel == ".." || strings.HasPrefix(lexRel, ".."+string(filepath.Separator)) || filepath.IsAbs(lexRel) {
			return nil
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	var realPath string
	if allowMissing {
		realPath, err = evalHUDSymlinksOrParent(cleanPath)
		if err != nil {
			return err
		}
	} else {
		realPath, err = filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: material path %q resolves outside the project", absPath)
	}
	return nil
}

// confinedProjectDirFS wraps os.DirFS(root) and refuses opening any path whose resolved real path escapes root via symlinks.
type confinedProjectDirFS struct {
	root     string
	realRoot string
	base     fs.FS
}

func newConfinedProjectDirFS(root string) fs.FS {
	clean := filepath.Clean(root)
	realRoot, err := filepath.EvalSymlinks(clean)
	if err != nil {
		realRoot = clean
	}
	return confinedProjectDirFS{
		root:     clean,
		realRoot: realRoot,
		base:     os.DirFS(clean),
	}
}

func (c confinedProjectDirFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) || name == "." {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	abs := filepath.Join(c.root, filepath.FromSlash(name))
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(c.realRoot, realPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return c.base.Open(name)
}

func cloneLightRig(r *render3d.LightRig) *render3d.LightRig {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Directional = slices.Clone(r.Directional)
	cp.Point = slices.Clone(r.Point)
	cp.Spot = slices.Clone(r.Spot)
	return &cp
}

func cloneMaterial(m resource.Material) resource.Material {
	out := m
	if m.Opacity != nil {
		v := *m.Opacity
		out.Opacity = &v
	}
	if m.Roughness != nil {
		v := *m.Roughness
		out.Roughness = &v
	}
	return out
}

func defaultMaterial(name string) resource.Material {
	return resource.Material{
		Header: resource.NewHeader(resource.KindMaterial),
		Name:   name,
	}
}

// isMaterialFile reports whether rel names a material resource file (*.material.yaml or *.material.yml).
func isMaterialFile(rel string) bool {
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".material.yaml") || strings.HasSuffix(lower, ".material.yml")
}

// withMaterialExt adds ".material.yaml" to a path without a YAML extension, or ".yaml" to one ending in ".material".
func withMaterialExt(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.HasSuffix(rel, "/") {
		return rel
	}
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".material.yaml"), strings.HasSuffix(lower, ".material.yml"):
		return rel
	case strings.HasSuffix(lower, ".material"):
		return rel + ".yaml"
	case strings.HasSuffix(lower, ".yaml"):
		return strings.TrimSuffix(rel, rel[len(rel)-5:]) + ".material.yaml"
	case strings.HasSuffix(lower, ".yml"):
		return strings.TrimSuffix(rel, rel[len(rel)-4:]) + ".material.yaml"
	default:
		return rel + ".material.yaml"
	}
}

func materialDisplayNameFromPath(rel string) string {
	base := path.Base(rel)
	for _, ext := range []string{".yaml", ".yml", ".material"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
		}
	}
	return base
}

func (e *Editor) defaultMaterialPreviewShape() MaterialPreviewShape {
	if e != nil && e.ViewMode == ViewMode2D {
		return MaterialPreviewSprite
	}
	return MaterialPreviewSphere
}

// MaterialOpen reports whether the Material Editor panel has a material open.
func (e *Editor) MaterialOpen() bool {
	return e != nil && e.material != nil
}

// Material returns a deep copy of the resource.Material open in the Material Editor panel, or nil when closed.
func (e *Editor) Material() *resource.Material {
	if e == nil || e.material == nil {
		return nil
	}
	cp := cloneMaterial(e.material.mat)
	return &cp
}

// MaterialPath returns the disk path of the open material file, or "" for an unsaved or in-memory material.
func (e *Editor) MaterialPath() string {
	if e == nil || e.material == nil {
		return ""
	}
	return e.material.path
}

// MaterialRelPath returns the slash-separated project-relative path of the open material, used for Library.Reload live updates, or "".
func (e *Editor) MaterialRelPath() string {
	if e == nil || e.material == nil {
		return ""
	}
	return e.material.relPath
}

// MaterialDirty reports whether the open material differs from the canonical resource.Encode bytes it was opened from or last saved to.
func (e *Editor) MaterialDirty() bool {
	if e == nil || e.material == nil {
		return false
	}
	if !e.material.dirtyValid {
		data, err := resource.Encode(&e.material.mat)
		e.material.dirty = err != nil || !bytes.Equal(data, e.material.saved)
		e.material.dirtyValid = true
	}
	return e.material.dirty
}

// MaterialPreviewShape returns the active preview shape (Sphere, Cube, or Sprite).
func (e *Editor) MaterialPreviewShape() MaterialPreviewShape {
	if e == nil || e.material == nil {
		return e.defaultMaterialPreviewShape()
	}
	return e.material.previewShape
}

// SetMaterialPreviewShape switches the Material Editor's live preview between Sphere, Cube (render3d), and Sprite (2D). It is a view setting, not an edit.
func (e *Editor) SetMaterialPreviewShape(shape MaterialPreviewShape) {
	if e == nil || e.material == nil {
		return
	}
	if shape < MaterialPreviewSphere || shape > MaterialPreviewSprite {
		return
	}
	if e.material.previewShape == shape {
		return
	}
	e.material.previewShape = shape
	e.material.previewKey = ""
	e.notify()
}

// ProjectTextures returns the slash-separated paths of image files in the open project that can be assigned to Material.Texture, sorted.
func (e *Editor) ProjectTextures() []string {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if f.Dir {
			continue
		}
		switch strings.ToLower(path.Ext(f.Path)) {
		case ".png", ".jpg", ".jpeg", ".webp":
			out = append(out, f.Path)
		}
	}
	slices.Sort(out)
	return out
}

// NewMaterial opens a new unsaved starter material in the Material Editor panel as an undoable command.
func (e *Editor) NewMaterial() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	m := defaultMaterial("New Material")
	saved, err := resource.Encode(&m)
	if err != nil {
		return err
	}
	e.swapMaterial("New material", &m, "", "", saved)
	return nil
}

// OpenMaterialYAML parses data as a resource.Material YAML file and opens it in the Material Editor panel as an undoable command.
func (e *Editor) OpenMaterialYAML(data []byte) error {
	return e.openMaterialData(data, "", "")
}

// OpenMaterialFile reads a *.material.yaml file from filePath and opens it in the Material Editor panel as an undoable command.
func (e *Editor) OpenMaterialFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if strings.TrimSpace(filePath) == "" {
		return errors.New("editor: material file path must not be empty")
	}
	if err := e.verifyMaterialProjectPath(filePath, false, false); err != nil {
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	rel, _ := e.projectRel(filePath)
	return e.openMaterialData(data, filePath, rel)
}

// OpenMaterialProjectFile opens relPath (slash-separated from the open project's root) in the Material Editor panel as an undoable command so edits reload live through Library.Reload.
func (e *Editor) OpenMaterialProjectFile(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	abs, err := e.projectFile(relPath)
	if err != nil {
		return err
	}
	if err := e.verifyMaterialProjectPath(abs, true, false); err != nil {
		return err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	return e.openMaterialData(data, abs, relPath)
}

// OpenMaterial opens the material at relPath: from the open project's file when a project is open, or through the editor's MaterialResolver / 3D assets otherwise, so edits reload live into Library.
func (e *Editor) OpenMaterial(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return errors.New("editor: material path must not be empty")
	}
	if e.Project != nil && e.ProjectPath != "" {
		return e.OpenMaterialProjectFile(relPath)
	}
	if !fs.ValidPath(relPath) || relPath == "." {
		return fmt.Errorf("editor: material %q must be a slash-separated path relative to the project root", relPath)
	}
	var loaded *resource.Material
	var err error
	switch {
	case e.ModelResolver != nil:
		if mr, ok := e.ModelResolver.(sceneformat.MaterialResolver); ok {
			loaded, err = mr.Material(relPath)
		}
	}
	if loaded == nil && err == nil {
		if mr, ok := e.Resolver.(sceneformat.MaterialResolver); ok {
			loaded, err = mr.Material(relPath)
		} else {
			return errNoMaterialResolver
		}
	}
	if err != nil {
		return err
	}
	if loaded == nil {
		return fmt.Errorf("editor: material %q is nil", relPath)
	}
	data, err := resource.Encode(loaded)
	if err != nil {
		return err
	}
	return e.openMaterialData(data, "", relPath)
}

func (e *Editor) openMaterialData(data []byte, filePath, relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	m, err := resource.ParseAs[*resource.Material](data)
	if err != nil {
		if filePath != "" {
			return fmt.Errorf("%s: %w", filePath, err)
		}
		return err
	}
	saved, err := resource.Encode(m)
	if err != nil {
		return err
	}
	label := "Open material"
	if relPath != "" {
		label += " " + path.Base(relPath)
	} else if filePath != "" {
		label += " " + filepath.Base(filePath)
	}
	e.swapMaterial(label, m, filePath, relPath, saved)
	return nil
}

// CreateMaterialFile writes a new canonical *.material.yaml file at relPath inside the open project (appending ".material.yaml" when needed), refuses to overwrite an existing file (ErrFileExists), and opens it in the Material Editor panel. It returns the written project-relative path.
func (e *Editor) CreateMaterialFile(relPath string) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	relPath = withMaterialExt(relPath)
	abs, err := e.projectFile(relPath)
	if err != nil {
		return "", err
	}
	if err := e.verifyMaterialProjectPath(abs, true, true); err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("%w: %s", ErrFileExists, relPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	m := defaultMaterial(materialDisplayNameFromPath(relPath))
	data, err := resource.Encode(&m)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return "", err
	}
	e.filesRev++
	e.invalidateProjectIndex()
	if err := e.openMaterialData(data, abs, relPath); err != nil {
		return "", err
	}
	e.Status = "Created material " + relPath
	e.notify()
	return relPath, nil
}

// CreateObjectMaterial creates a new *.material.yaml file in the open project (picking a free materials/<objectID>.material.yaml path when relPath is empty), assigns it to objectID when the resolver can load materials, and opens it in the Material Editor panel. It returns the created project-relative path.
func (e *Editor) CreateObjectMaterial(objectID, relPath string) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return "", ErrNoProject
	}
	if objectID != "" {
		if obj, _, _ := e.FindObject(objectID); obj == nil {
			return "", fmt.Errorf("editor: object %q not found", objectID)
		}
	}
	if strings.TrimSpace(relPath) == "" {
		var err error
		relPath, err = e.nextMaterialPath(objectID)
		if err != nil {
			return "", err
		}
	}
	created, err := e.CreateMaterialFile(relPath)
	if err != nil {
		return "", err
	}
	if objectID != "" && e.canLoadMaterials() {
		if err := e.SetObjectMaterial(objectID, created); err != nil {
			return created, err
		}
	}
	return created, nil
}

func (e *Editor) nextMaterialPath(stem string) (string, error) {
	root := filepath.Dir(e.ProjectPath)
	clean := sanitizeMaterialStem(stem)
	if clean == "" {
		clean = "material"
	}
	for i := 1; i < 10000; i++ {
		name := clean
		if i > 1 {
			name = fmt.Sprintf("%s_%d", clean, i)
		}
		rel := path.Join("materials", name+materialSuffix)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
			return rel, nil
		}
	}
	return "", errors.New("editor: could not choose a unique material file path")
}

func sanitizeMaterialStem(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '/':
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_-")
}

// CloseMaterial closes the Material Editor panel as an undoable command.
func (e *Editor) CloseMaterial() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	if e.material == nil {
		return nil
	}
	e.cancelMaterialColorPreview()
	e.swapMaterial("Close material", nil, "", "", nil)
	return nil
}

// SaveMaterialYAML encodes the open material with resource.Encode.
func (e *Editor) SaveMaterialYAML() ([]byte, error) {
	if e == nil || e.material == nil {
		return nil, ErrNoMaterial
	}
	return resource.Encode(&e.material.mat)
}

// SaveMaterialFile encodes the open material with resource.Encode, writes it atomically to filePath, remembers filePath, and reloads live resource libraries.
func (e *Editor) SaveMaterialFile(filePath string) error {
	if e == nil || e.material == nil {
		return ErrNoMaterial
	}
	if e.playing {
		return ErrPlaying
	}
	if strings.TrimSpace(filePath) == "" {
		return errors.New("editor: material file path must not be empty")
	}
	if err := e.verifyMaterialProjectPath(filePath, false, true); err != nil {
		return err
	}
	if err := e.flushMaterialColorPreview(); err != nil {
		return err
	}
	data, err := e.SaveMaterialYAML()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(filePath, data); err != nil {
		return err
	}
	e.material.path = filePath
	if rel, ok := e.projectRel(filePath); ok {
		e.material.relPath = rel
	}
	e.material.saved = append([]byte(nil), data...)
	e.material.dirty = false
	e.material.dirtyValid = true
	e.applyMaterialLive(e.material.relPath, e.material.mat)
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved material " + filePath
	e.notify()
	e.CheckProblems()
	return nil
}

// SaveMaterial persists the open material via OnSaveMaterial when set, or to MaterialPath via SaveMaterialFile.
func (e *Editor) SaveMaterial() error {
	if e == nil || e.material == nil {
		return ErrNoMaterial
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.flushMaterialColorPreview(); err != nil {
		return err
	}
	if e.OnSaveMaterial != nil {
		data, err := e.SaveMaterialYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveMaterial(data); err != nil {
			return err
		}
		e.material.saved = append([]byte(nil), data...)
		e.material.dirty = false
		e.material.dirtyValid = true
		e.applyMaterialLive(e.material.relPath, e.material.mat)
		e.Status = "Saved material"
		e.notify()
		return nil
	}
	if e.material.path == "" {
		return errors.New("editor: material has no file path; pass one to SaveMaterialFile or set OnSaveMaterial")
	}
	return e.SaveMaterialFile(e.material.path)
}

// EditMaterial applies edit to a copy of the open resource.Material, validates it with resource.Encode, records an undoable command on Editor.History, and applies the change live through resource.Library.Reload so the preview and every viewport object using that material update immediately.
func (e *Editor) EditMaterial(label string, edit func(m *resource.Material) error) error {
	if e == nil || e.material == nil {
		return ErrNoMaterial
	}
	if e.playing {
		return ErrPlaying
	}
	hadPreview := e.material.colorPreview.active
	if hadPreview {
		e.material.mat = cloneMaterial(e.material.colorPreview.before)
		e.material.colorPreview = materialColorPreview{}
	}
	before := cloneMaterial(e.material.mat)
	after := cloneMaterial(e.material.mat)
	if err := edit(&after); err != nil {
		if hadPreview {
			e.applyMaterialLive(e.material.relPath, e.material.mat)
			e.notify()
		}
		return err
	}
	if _, err := resource.Encode(&after); err != nil {
		if hadPreview {
			e.applyMaterialLive(e.material.relPath, e.material.mat)
			e.notify()
		}
		return err
	}
	if reflect.DeepEqual(before, after) {
		if hadPreview {
			e.applyMaterialLive(e.material.relPath, e.material.mat)
			e.notify()
		}
		return nil
	}
	e.History.Execute(&materialEditCmd{
		ed:      e,
		target:  e.material,
		label:   label,
		relPath: e.material.relPath,
		before:  before,
		after:   after,
	})
	return nil
}

// SetMaterialName updates the open material's Name through History.
func (e *Editor) SetMaterialName(name string) error {
	return e.EditMaterial("Set material name", func(m *resource.Material) error {
		m.Name = name
		return nil
	})
}

// SetMaterialBaseColor updates the open material's BaseColor CSS color string (empty for default white) through History.
func (e *Editor) SetMaterialBaseColor(color string) error {
	color = strings.TrimSpace(color)
	return e.EditMaterial("Set material base color", func(m *resource.Material) error {
		m.BaseColor = color
		return nil
	})
}

// SetMaterialEmissive updates the open material's Emissive CSS color string (empty for none) through History.
func (e *Editor) SetMaterialEmissive(color string) error {
	color = strings.TrimSpace(color)
	return e.EditMaterial("Set material emissive", func(m *resource.Material) error {
		m.Emissive = color
		return nil
	})
}

// SetMaterialTexture updates the open material's Texture path (slash-separated from project root, or empty for none) through History.
func (e *Editor) SetMaterialTexture(texturePath string) error {
	texturePath = strings.TrimSpace(texturePath)
	return e.EditMaterial("Set material texture", func(m *resource.Material) error {
		m.Texture = texturePath
		return nil
	})
}

// SetMaterialMetallic updates the open material's Metallic factor in [0, 1] through History.
func (e *Editor) SetMaterialMetallic(v float32) error {
	return e.EditMaterial("Set material metallic", func(m *resource.Material) error {
		m.Metallic = v
		return nil
	})
}

// SetMaterialRoughness updates the open material's Roughness factor in [0, 1] through History.
func (e *Editor) SetMaterialRoughness(v float32) error {
	return e.EditMaterial("Set material roughness", func(m *resource.Material) error {
		if v == 1 {
			m.Roughness = nil
			return nil
		}
		rv := v
		m.Roughness = &rv
		return nil
	})
}

// SetMaterialOpacity updates the open material's Opacity factor in [0, 1] through History.
func (e *Editor) SetMaterialOpacity(v float32) error {
	return e.EditMaterial("Set material opacity", func(m *resource.Material) error {
		if v == 1 {
			m.Opacity = nil
			return nil
		}
		ov := v
		m.Opacity = &ov
		return nil
	})
}

// SetMaterialBlend updates the open material's 2D Blend mode (empty or "sourceOver" for default) through History.
func (e *Editor) SetMaterialBlend(blend string) error {
	blend = strings.TrimSpace(blend)
	return e.EditMaterial("Set material blend", func(m *resource.Material) error {
		if blend == "sourceOver" {
			m.Blend = ""
			return nil
		}
		m.Blend = blend
		return nil
	})
}

// SetMaterialDoubleSided updates the open material's DoubleSided flag through History.
func (e *Editor) SetMaterialDoubleSided(on bool) error {
	return e.EditMaterial("Set material double-sided", func(m *resource.Material) error {
		m.DoubleSided = on
		return nil
	})
}

// SetMaterialUnlit updates the open material's Unlit flag through History.
func (e *Editor) SetMaterialUnlit(on bool) error {
	return e.EditMaterial("Set material unlit", func(m *resource.Material) error {
		m.Unlit = on
		return nil
	})
}

func (e *Editor) previewMaterialColor(emissive bool, color string) {
	if e == nil || e.playing || e.material == nil {
		return
	}
	if !e.material.colorPreview.active {
		e.material.colorPreview = materialColorPreview{
			active:   true,
			emissive: emissive,
			before:   cloneMaterial(e.material.mat),
		}
	}
	e.material.colorPreview.emissive = emissive
	e.material.colorPreview.pending = color
	if emissive {
		e.material.mat.Emissive = color
	} else {
		e.material.mat.BaseColor = color
	}
	e.material.previewKey = ""
	e.material.dirtyValid = false
	e.applyMaterialLive(e.material.relPath, e.material.mat)
	e.notify()
}

func (e *Editor) flushMaterialColorPreview() error {
	if e == nil || e.material == nil || !e.material.colorPreview.active {
		return nil
	}
	return e.finishMaterialColorPreview(e.material.colorPreview.emissive, e.material.colorPreview.pending)
}

func (e *Editor) finishMaterialColorPreview(emissive bool, formatted string) error {
	if e == nil || e.material == nil {
		return ErrNoMaterial
	}
	if e.playing {
		e.cancelMaterialColorPreview()
		return ErrPlaying
	}
	if emissive {
		return e.SetMaterialEmissive(formatted)
	}
	return e.SetMaterialBaseColor(formatted)
}

func (e *Editor) cancelMaterialColorPreview() {
	if e == nil || e.material == nil || !e.material.colorPreview.active {
		return
	}
	orig := cloneMaterial(e.material.colorPreview.before)
	e.material.colorPreview = materialColorPreview{}
	e.material.mat = orig
	e.material.previewKey = ""
	e.material.dirtyValid = false
	e.applyMaterialLive(e.material.relPath, e.material.mat)
	e.notify()
}

func (e *Editor) swapMaterial(label string, mat *resource.Material, filePath, relPath string, saved []byte) {
	beforeState := e.material
	var afterState *materialEditorState
	if mat != nil {
		afterShape := e.defaultMaterialPreviewShape()
		if beforeState != nil {
			afterShape = beforeState.previewShape
		}
		afterState = &materialEditorState{
			mat:          cloneMaterial(*mat),
			path:         filePath,
			relPath:      relPath,
			saved:        slices.Clone(saved),
			previewShape: afterShape,
		}
	}
	e.History.Execute(&materialSwapCmd{
		ed:          e,
		label:       label,
		beforeState: beforeState,
		afterState:  afterState,
	})
}

type materialSwapCmd struct {
	ed          *Editor
	label       string
	beforeState *materialEditorState
	afterState  *materialEditorState
}

func (c *materialSwapCmd) Label() string { return c.label }
func (c *materialSwapCmd) Do()           { c.apply(c.afterState) }
func (c *materialSwapCmd) Undo()         { c.apply(c.beforeState) }

func (c *materialSwapCmd) apply(st *materialEditorState) {
	e := c.ed
	if st == nil {
		e.material = nil
		e.Status = c.label
		return
	}
	st.colorPreview = materialColorPreview{}
	st.previewKey = ""
	st.dirtyValid = false
	e.material = st
	if st.relPath != "" && e.MaterialDirty() {
		e.applyMaterialLive(st.relPath, st.mat)
	}
	e.Status = c.label
}

type materialEditCmd struct {
	ed      *Editor
	target  *materialEditorState
	label   string
	relPath string
	before  resource.Material
	after   resource.Material
}

func (c *materialEditCmd) Label() string { return c.label }
func (c *materialEditCmd) Do()           { c.apply(c.after) }
func (c *materialEditCmd) Undo()         { c.apply(c.before) }

func (c *materialEditCmd) apply(m resource.Material) {
	e := c.ed
	rel := c.relPath
	if c.target != nil {
		c.target.mat = cloneMaterial(m)
		c.target.dirtyValid = false
		c.target.colorPreview = materialColorPreview{}
		c.target.previewKey = ""
		if c.target.relPath != "" {
			rel = c.target.relPath
		}
	}
	if e.material != nil && (e.material == c.target || (c.target == nil && c.relPath != "" && e.material.relPath == c.relPath)) {
		if e.material != c.target {
			e.material.mat = cloneMaterial(m)
			e.material.dirtyValid = false
			e.material.colorPreview = materialColorPreview{}
			e.material.previewKey = ""
		}
	}
	e.applyMaterialLive(rel, m)
	e.Status = c.label
}

// resourceLibraryProvider is implemented by resolvers (such as *project.Resolver and *scenemodel.Assets) that own a *resource.Library.
type resourceLibraryProvider interface {
	Library() *resource.Library
}

func extractResourceLibrary(x any) *resource.Library {
	if x == nil {
		return nil
	}
	if lib, ok := x.(*resource.Library); ok {
		return lib
	}
	if p, ok := x.(resourceLibraryProvider); ok {
		return p.Library()
	}
	return nil
}

// resourceLibraries returns every distinct non-nil *resource.Library reachable from e's resolvers and 3D assets.
func (e *Editor) resourceLibraries() []*resource.Library {
	if e == nil {
		return nil
	}
	var libs []*resource.Library
	add := func(lib *resource.Library) {
		if lib == nil || slices.Contains(libs, lib) {
			return
		}
		libs = append(libs, lib)
	}
	add(extractResourceLibrary(e.ModelResolver))
	add(extractResourceLibrary(e.Resolver))
	add(extractResourceLibrary(e.s3d.files))
	return libs
}

// currentMaterialLibRev sums the Library.Revision() counters of every reachable resource.Library so Scene3D rebuilds even when an external caller invokes Library.Reload directly.
func (e *Editor) currentMaterialLibRev() uint64 {
	var total uint64
	for _, lib := range e.resourceLibraries() {
		total += lib.Revision()
	}
	return total
}

// applyMaterialLive pushes m into every loaded *resource.Library via Library.Reload, syncs 2D BuiltScene sprites, and invalidates the 3D scene build without refreshing resolvers from disk.
func (e *Editor) applyMaterialLive(relPath string, m resource.Material) {
	if e == nil {
		return
	}
	if relPath != "" {
		if data, err := resource.Encode(&m); err == nil {
			reloadedAny := false
			for _, lib := range e.resourceLibraries() {
				if !lib.Loaded(relPath) {
					_, _ = lib.Load(relPath)
				}
				if loaded, err := lib.Reload(relPath, data); err == nil && loaded {
					reloadedAny = true
				}
			}
			// Fallback when a custom MaterialResolver wraps a shared *resource.Material without exposing Library().
			if !reloadedAny {
				if mr, ok := e.ModelResolver.(sceneformat.MaterialResolver); ok {
					if ptr, err := mr.Material(relPath); err == nil && ptr != nil {
						*ptr = cloneMaterial(m)
					}
				}
				if mr, ok := e.Resolver.(sceneformat.MaterialResolver); ok {
					if ptr, err := mr.Material(relPath); err == nil && ptr != nil {
						*ptr = cloneMaterial(m)
					}
				}
			}
		}
	}
	if e.Built != nil {
		e.Built.SyncMaterials()
	}
	e.materialRev++
	e.s3d.stale = true
}

func (e *Editor) followMaterialMove(moveFile func(*string), movePath func(*string)) {
	if e == nil || e.material == nil {
		return
	}
	moveFile(&e.material.path)
	movePath(&e.material.relPath)
	if e.material.mat.Texture != "" {
		beforeTex := e.material.mat.Texture
		movePath(&e.material.mat.Texture)
		if e.material.mat.Texture != beforeTex {
			e.material.previewKey = ""
			e.material.dirtyValid = false
		}
	}
	if saved, err := resource.ParseAs[*resource.Material](e.material.saved); err == nil && saved.Texture != "" {
		movePath(&saved.Texture)
		if data, err := resource.Encode(saved); err == nil {
			e.material.saved = data
		}
	}
}

// materialPreviewAssets resolves the edited material in memory and loads its optional texture from the editor's resolvers or project directory.
type materialPreviewAssets struct {
	mat    resource.Material
	assets scene3DAssets
}

func (a materialPreviewAssets) Model(path string) (*gltf.Document, error) {
	return a.assets.Model(path)
}

func (a materialPreviewAssets) Material(path string) (*resource.Material, error) {
	if path == materialPreviewPath {
		cp := cloneMaterial(a.mat)
		return &cp, nil
	}
	return a.assets.Material(path)
}

func (a materialPreviewAssets) Image(path string) (image.Image, error) {
	return a.assets.Image(path)
}

// MaterialPreviewImage renders the open material's live 3D preview (sphere or cube) with render3d on a software GPU into a square RGBA image of size pixels, caching the image until the material, shape, size, lighting, or project assets change. Returns (nil, nil) when no material is open or the preview shape is MaterialPreviewSprite.
func (e *Editor) MaterialPreviewImage(size int) (*image.RGBA, error) {
	if e == nil || e.material == nil {
		return nil, nil
	}
	if e.material.previewShape == MaterialPreviewSprite {
		return nil, nil
	}
	if size <= 0 {
		size = MaterialPreviewSize
	}
	lightsSame := (e.Lights3D == nil && e.material.previewLights == nil) ||
		(e.Lights3D != nil && e.material.previewLights != nil && reflect.DeepEqual(e.Lights3D, e.material.previewLights))
	if e.material.previewKey != "" &&
		e.material.previewSize == size &&
		e.material.previewShapeKey == e.material.previewShape &&
		e.material.previewProjPath == e.ProjectPath &&
		e.material.previewAssetsRev == e.assetsRev &&
		lightsSame {
		return e.material.previewImg, e.material.previewErr
	}
	encoded, err := resource.Encode(&e.material.mat)
	if err != nil {
		return nil, err
	}
	key := strconv.Itoa(size) + "\x00" + e.material.previewShape.String() + "\x00" + e.ProjectPath + "\x00" + strconv.FormatUint(e.assetsRev, 10) + "\x00" + string(encoded)
	if e.material.previewKey == key && lightsSame {
		return e.material.previewImg, e.material.previewErr
	}
	img, renderErr := e.renderMaterialPreview3D(e.material.mat, e.material.previewShape, size)
	e.material.previewKey = key
	e.material.previewSize = size
	e.material.previewShapeKey = e.material.previewShape
	e.material.previewProjPath = e.ProjectPath
	e.material.previewAssetsRev = e.assetsRev
	e.material.previewLights = cloneLightRig(e.Lights3D)
	e.material.previewImg = img
	e.material.previewErr = renderErr
	e.material.previewVersion++
	return img, renderErr
}

func (e *Editor) renderMaterialPreview3D(mat resource.Material, shape MaterialPreviewShape, size int) (*image.RGBA, error) {
	meshName := sceneformat.MeshSphere
	if shape == MaterialPreviewCube {
		meshName = sceneformat.MeshBox
	}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name: "preview",
			Objects: []sceneformat.ObjectDoc{{
				ID:       "sample",
				Mesh:     meshName,
				Material: materialPreviewPath,
			}},
		}},
	}
	assets := scene3DAssets{models: e.ModelResolver, resolver: e.Resolver}
	if e.ProjectPath != "" {
		assets.files = scenemodel.NewAssets(newConfinedProjectDirFS(filepath.Dir(e.ProjectPath)))
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), materialPreviewAssets{mat: mat, assets: assets})
	if err != nil {
		return nil, fmt.Errorf("editor: material preview: %w", err)
	}
	sr := gpu.NewSoftwareRenderer(size, size, viewport3DDepthFormat)
	defer sr.Close()
	dev := sr.Device()
	r, err := scenemodel.NewRenderer(nil, dev, built, scenemodel.RendererOptions{
		Label:       "editor.material.preview",
		DepthFormat: viewport3DDepthFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("editor: material preview: %w", err)
	}
	defer func() { _ = r.Close() }()
	if mat.Unlit {
		if err := r.SetShading(scenemodel.ShadingUnlit); err != nil {
			return nil, fmt.Errorf("editor: material preview: %w", err)
		}
	}
	cam := OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.55,
		Pitch:    0.35,
		Distance: 2.0,
		FOV:      float32(math.Pi / 4),
		Near:     0.1,
		Far:      20,
	}
	if shape == MaterialPreviewCube {
		cam.Distance = 2.4
	}
	lights := DefaultLights3D()
	if e.Lights3D != nil {
		lights = *e.Lights3D
	}
	if _, err := r.Prepare(cam.SceneCamera(float32(size), float32(size)), lights); err != nil {
		return nil, fmt.Errorf("editor: material preview: %w", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "editor.material.preview",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.Color(0)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	r.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		return nil, fmt.Errorf("editor: material preview: %w", err)
	}
	src := sr.Image()
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out, nil
}

// resolveMaterialTextureImage loads mat.Texture as an RGBA image for the 2D sprite preview (caching on materialEditorState until texPath, ProjectPath, or assetsRev changes), returning nil when no texture is set or it cannot be loaded.
func (e *Editor) resolveMaterialTextureImage(texPath string) *image.RGBA {
	if e == nil || texPath == "" {
		return nil
	}
	key := texPath + "\x00" + e.ProjectPath + "\x00" + strconv.FormatUint(e.assetsRev, 10)
	if e.material != nil && e.material.texCacheKey == key {
		return e.material.texCacheImg
	}
	assets := scene3DAssets{models: e.ModelResolver, resolver: e.Resolver}
	if e.ProjectPath != "" {
		assets.files = scenemodel.NewAssets(newConfinedProjectDirFS(filepath.Dir(e.ProjectPath)))
	}
	var resolved *image.RGBA
	if img, err := assets.Image(texPath); err == nil && img != nil {
		if rgba, ok := img.(*image.RGBA); ok {
			resolved = rgba
		} else {
			b := img.Bounds()
			dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					dst.Set(x-b.Min.X, y-b.Min.Y, img.At(x, y))
				}
			}
			resolved = dst
		}
	}
	if e.material != nil {
		e.material.texCacheKey = key
		e.material.texCacheImg = resolved
		e.material.previewVersion++
	}
	return resolved
}

// materialPreviewSpriteEntry returns the selected object's sprite frame (or the first available atlas frame) for the 2D sprite preview.
func (e *Editor) materialPreviewSpriteEntry() (AssetEntry, bool) {
	if e == nil || e.Resolver == nil {
		return AssetEntry{}, false
	}
	if obj, _ := e.SelectedObject(); obj != nil && obj.Sprite != nil {
		if atlas, err := e.Resolver.Atlas(obj.Sprite.Atlas); err == nil && atlas != nil {
			if frame, ok := atlas.Frames[obj.Sprite.Frame]; ok {
				return AssetEntry{Atlas: obj.Sprite.Atlas, Frame: obj.Sprite.Frame, Image: atlas.Image, Rect: frame}, true
			}
		}
	}
	if assets := e.Assets(); len(assets) > 0 {
		return assets[0], true
	}
	return AssetEntry{}, false
}

// materialFieldPreviewLabel returns a compact status line describing the preview surface.
func materialPreviewInfo(mat *resource.Material, shape MaterialPreviewShape) string {
	if mat == nil {
		return ""
	}
	if shape == MaterialPreviewSprite {
		blend := mat.Blend
		if blend == "" {
			blend = "sourceOver"
		}
		return fmt.Sprintf("2D · opacity %.2f · %s", mat.EffectiveOpacity(), blend)
	}
	mode := "PBR"
	if mat.Unlit {
		mode = "Unlit"
	}
	return fmt.Sprintf("%s · M %.2f · R %.2f", mode, mat.Metallic, mat.EffectiveRoughness())
}

// Ensure math3d is referenced cleanly for future extensions (keep imports tidy).
var _ = math3d.Vec3Zero

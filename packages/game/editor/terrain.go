package editor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoTerrain is returned by terrain operations when no terrain is open in the editor.
var ErrNoTerrain = errors.New("editor: no terrain is open")

// ErrNoTerrainPath is returned by SaveTerrain when the open terrain has no file path yet.
var ErrNoTerrainPath = errors.New("editor: the terrain has no file path; use SaveTerrainTo first")

// TerrainBrushMode selects the active terrain sculpting or painting brush in the 3D viewport.
type TerrainBrushMode int

const (
	// TerrainBrushNone leaves normal 3D object picking, gizmos, and camera orbiting active.
	TerrainBrushNone TerrainBrushMode = iota
	// TerrainBrushRaise raises heightmap vertices within the brush radius.
	TerrainBrushRaise
	// TerrainBrushLower lowers heightmap vertices within the brush radius.
	TerrainBrushLower
	// TerrainBrushSmooth blends heightmap vertices toward their local neighborhood average.
	TerrainBrushSmooth
	// TerrainBrushFlatten levels heightmap vertices toward the height sampled at the start of the stroke (or TargetHeight when UseTargetHeight is true).
	TerrainBrushFlatten
	// TerrainBrushSetHeight moves heightmap vertices toward TerrainBrush.TargetHeight.
	TerrainBrushSetHeight
	// TerrainBrushPaintLayer increases the weight of TerrainBrush.LayerIndex and renormalizes the remaining material layers.
	TerrainBrushPaintLayer
	// TerrainBrushPaintFoliage blends the density of TerrainBrush.FoliageIndex toward TerrainBrush.TargetDensity.
	TerrainBrushPaintFoliage
)

// String returns a human-readable label for m.
func (m TerrainBrushMode) String() string {
	switch m {
	case TerrainBrushNone:
		return "Off"
	case TerrainBrushRaise:
		return "Raise"
	case TerrainBrushLower:
		return "Lower"
	case TerrainBrushSmooth:
		return "Smooth"
	case TerrainBrushFlatten:
		return "Flatten"
	case TerrainBrushSetHeight:
		return "Set Height"
	case TerrainBrushPaintLayer:
		return "Paint Layer"
	case TerrainBrushPaintFoliage:
		return "Paint Foliage"
	default:
		return "Unknown"
	}
}

// TerrainBrushFalloff selects how a terrain brush's influence decays from its center to its radius.
type TerrainBrushFalloff int

const (
	// TerrainFalloffSmooth uses a C1 smoothstep curve (1 at center, 0 at edge, flat derivatives at both ends).
	TerrainFalloffSmooth TerrainBrushFalloff = iota
	// TerrainFalloffLinear decays linearly from 1 at the center to 0 at the edge.
	TerrainFalloffLinear
	// TerrainFalloffFlat applies uniform weight 1 everywhere inside the brush radius.
	TerrainFalloffFlat
)

// String returns a human-readable label for f.
func (f TerrainBrushFalloff) String() string {
	switch f {
	case TerrainFalloffSmooth:
		return "Smooth"
	case TerrainFalloffLinear:
		return "Linear"
	case TerrainFalloffFlat:
		return "Flat"
	default:
		return "Unknown"
	}
}

func evalTerrainFalloff(t float32, falloff TerrainBrushFalloff) float32 {
	if t <= 0 {
		return 1
	}
	if t >= 1 {
		return 0
	}
	switch falloff {
	case TerrainFalloffLinear:
		return 1 - t
	case TerrainFalloffFlat:
		return 1
	default:
		return 1 - t*t*(3-2*t)
	}
}

// TerrainBrush configures the active terrain sculpt or paint tool.
type TerrainBrush struct {
	// Mode selects the brush operation (TerrainBrushNone disables terrain brushing).
	Mode TerrainBrushMode
	// Radius is the world-space XZ radius of the brush in meters (> 0; defaults to 8 when <= 0).
	Radius float32
	// Strength controls the per-dab brush intensity in (0, 1] (defaults to 0.5 when <= 0).
	Strength float32
	// Falloff controls how strength decays from center to radius.
	Falloff TerrainBrushFalloff
	// TargetHeight is the world-space Y elevation used by TerrainBrushSetHeight (and TerrainBrushFlatten when UseTargetHeight is true).
	TargetHeight float32
	// UseTargetHeight makes TerrainBrushFlatten target TargetHeight instead of sampling the terrain height where the stroke starts.
	UseTargetHeight bool
	// TargetDensity is the target foliage density in [0, 1] for TerrainBrushPaintFoliage (1 paints full density, 0 erases).
	TargetDensity float32
	// LayerIndex selects the material layer (0..len(doc.Layers)-1) painted by TerrainBrushPaintLayer.
	LayerIndex int
	// FoliageIndex selects the foliage layer (0..len(doc.Foliage)-1) painted by TerrainBrushPaintFoliage.
	FoliageIndex int
}

// DefaultTerrainBrush returns sensible defaults for terrain sculpting and painting.
func DefaultTerrainBrush() TerrainBrush {
	return TerrainBrush{
		Mode:          TerrainBrushNone,
		Radius:        8,
		Strength:      0.5,
		Falloff:       TerrainFalloffSmooth,
		TargetDensity: 1,
	}
}

func (b TerrainBrush) effectiveRadius() float32 {
	if !finite32(b.Radius) || b.Radius <= 0 {
		return 8
	}
	return b.Radius
}

func (b TerrainBrush) effectiveStrength() float32 {
	if !finite32(b.Strength) || b.Strength <= 0 {
		return 0.5
	}
	if b.Strength > 1 {
		return 1
	}
	return b.Strength
}

func (b TerrainBrush) effectiveTargetDensity() float32 {
	if !finite32(b.TargetDensity) || b.TargetDensity < 0 {
		return 0
	}
	if b.TargetDensity > 1 {
		return 1
	}
	return b.TargetDensity
}

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func clamp01f32(v float32) float32 {
	if !finite32(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// NewTerrainOptions configures [Editor.NewTerrain].
type NewTerrainOptions struct {
	// Name is the human-readable terrain name (defaults to "Terrain").
	Name string
	// Path is the optional file path where SaveTerrain writes the *.terrain.yaml (project-relative when a project is open, or OS path).
	Path string
	// Cols and Rows set the heightmap grid dimensions (each defaults to 33 when < 2).
	Cols int
	Rows int
	// CellSize is the XZ spacing between samples in world units (defaults to 2 when <= 0).
	CellSize float32
	// HeightScale is the world-space Y scale for normalized [0, 1] heights (defaults to 32 when <= 0).
	HeightScale float32
	// Origin is the world-space origin of sample (0, 0).
	Origin [3]float32
	// Layers optionally provides the initial material layers (defaults to grass and rock when empty).
	Layers []world3d.TerrainLayerDoc
	// Foliage optionally provides the initial foliage layers.
	Foliage []world3d.FoliageLayerDoc
}

type terrainState struct {
	doc           *world3d.TerrainDoc
	hm            *world3d.Heightmap
	path          string
	rootDir       string
	useWeightMaps bool
	weightW       int
	weightH       int
	weights       [][]float32
	foliage       map[string]*world3d.DensityMap
	curStateID    uint64
	savedStateID  uint64
	nextStateID   uint64
	stroke        *terrainStroke
	hoverOK       bool
	hoverPos      math3d.Vec3
}

type terrainStroke struct {
	mode          TerrainBrushMode
	brush         TerrainBrush
	lastX, lastZ  float32
	flattenTarget float32 // normalized [0, 1] height target for Flatten/SetHeight
	layerCount    int
	foliageName   string
	heightBefore  map[int]float32
	weightBefore  map[int][]float32
	densityBefore map[int]float32
}

type heightDelta struct {
	index  int
	before float32
	after  float32
}

type weightDelta struct {
	index  int
	before []float32
	after  []float32
}

type densityDelta struct {
	index  int
	before float32
	after  float32
}

type terrainStrokeCmd struct {
	ed            *Editor
	state         *terrainState
	label         string
	beforeStateID uint64
	afterStateID  uint64
	heights       []heightDelta
	weights       []weightDelta
	foliageName   string
	densities     []densityDelta
}

func (c *terrainStrokeCmd) Label() string { return c.label }

func (c *terrainStrokeCmd) Do() {
	c.apply(false)
	c.state.curStateID = c.afterStateID
}

func (c *terrainStrokeCmd) Undo() {
	c.apply(true)
	c.state.curStateID = c.beforeStateID
}

func (c *terrainStrokeCmd) apply(undo bool) {
	t := c.state
	if t == nil || t.hm == nil {
		return
	}
	for _, d := range c.heights {
		if d.index >= 0 && d.index < len(t.hm.Heights) {
			if undo {
				t.hm.Heights[d.index] = d.before
			} else {
				t.hm.Heights[d.index] = d.after
			}
		}
	}
	if len(c.weights) > 0 && t.weightW > 0 && t.weightH > 0 {
		for _, d := range c.weights {
			vals := d.after
			if undo {
				vals = d.before
			}
			tx := d.index % t.weightW
			ty := d.index / t.weightW
			for li := 0; li < len(t.weights) && li < len(vals); li++ {
				t.weights[li][d.index] = vals[li]
				if t.hm.Splat != nil {
					t.hm.Splat.SetTexelWeight(li, tx, ty, vals[li])
				}
			}
		}
	}
	if len(c.densities) > 0 && c.foliageName != "" {
		if dm := t.foliage[c.foliageName]; dm != nil {
			for _, d := range c.densities {
				if d.index >= 0 && d.index < len(dm.Data) {
					if undo {
						dm.Data[d.index] = d.before
					} else {
						dm.Data[d.index] = d.after
					}
				}
			}
		}
	}
}

// TerrainOpen reports whether a terrain is currently open for sculpting and painting.
func (e *Editor) TerrainOpen() bool {
	return e != nil && e.terrain != nil && e.terrain.hm != nil && e.terrain.doc != nil
}

// Terrain returns the open runtime [*world3d.Heightmap], or nil when no terrain is open.
func (e *Editor) Terrain() *world3d.Heightmap {
	if !e.TerrainOpen() {
		return nil
	}
	return e.terrain.hm
}

// TerrainDoc returns the open [*world3d.TerrainDoc], or nil when no terrain is open.
func (e *Editor) TerrainDoc() *world3d.TerrainDoc {
	if !e.TerrainOpen() {
		return nil
	}
	return e.terrain.doc
}

// TerrainPath returns the OS file path of the open *.terrain.yaml, or "" when unsaved/in-memory.
func (e *Editor) TerrainPath() string {
	if !e.TerrainOpen() {
		return ""
	}
	return e.terrain.path
}

// TerrainDirty reports whether the open terrain has unsaved sculpt, paint, or metadata edits.
func (e *Editor) TerrainDirty() bool {
	if !e.TerrainOpen() {
		return false
	}
	return e.terrain.curStateID != e.terrain.savedStateID
}

// TerrainBrush returns the current terrain brush configuration.
func (e *Editor) TerrainBrush() TerrainBrush {
	if e == nil {
		return DefaultTerrainBrush()
	}
	return e.terrainBrush
}

// SetTerrainBrush updates the active terrain brush configuration, ending any in-progress stroke first.
func (e *Editor) SetTerrainBrush(b TerrainBrush) {
	if e == nil {
		return
	}
	e.endTerrainStrokeForChange()
	if b.Radius <= 0 || !finite32(b.Radius) {
		b.Radius = 8
	}
	if b.Strength <= 0 || !finite32(b.Strength) {
		b.Strength = 0.5
	} else if b.Strength > 1 {
		b.Strength = 1
	}
	b.TargetDensity = clamp01f32(b.TargetDensity)
	if !finite32(b.TargetHeight) {
		b.TargetHeight = 0
	}
	if b.LayerIndex < 0 {
		b.LayerIndex = 0
	}
	if b.FoliageIndex < 0 {
		b.FoliageIndex = 0
	}
	e.terrainBrush = b
	if b.Mode != TerrainBrushNone {
		e.CancelGizmoDrag3D()
		e.CancelComponentGizmoDrag3D()
		e.CancelPluginDrag3D()
		e.ClearPluginHover3D()
		e.Status = "Terrain Brush: " + b.Mode.String()
	}
	e.notify()
}

// SetTerrainBrushMode selects the active terrain brush mode while keeping the other brush parameters.
func (e *Editor) SetTerrainBrushMode(mode TerrainBrushMode) {
	if e == nil {
		return
	}
	b := e.terrainBrush
	b.Mode = mode
	e.SetTerrainBrush(b)
}

// TerrainWeightMap returns an 8-bit grayscale image of layer's current painted weights, or nil.
func (e *Editor) TerrainWeightMap(layer int) *image.Gray {
	if !e.TerrainOpen() || layer < 0 || layer >= len(e.terrain.weights) {
		return nil
	}
	t := e.terrain
	img := image.NewGray(image.Rect(0, 0, t.weightW, t.weightH))
	w := t.weights[layer]
	for y := range t.weightH {
		off := img.PixOffset(0, y)
		row := y * t.weightW
		for x := range t.weightW {
			img.Pix[off+x] = uint8(math.Round(float64(clamp01f32(w[row+x])) * 255.0))
		}
	}
	return img
}

// TerrainSplatMaps returns RGBA [*image.NRGBA] splat map images reflecting the current painted weights.
func (e *Editor) TerrainSplatMaps() (splat0, splat1 *image.NRGBA) {
	if !e.TerrainOpen() || e.terrain.hm.Splat == nil {
		return nil, nil
	}
	return e.terrain.hm.Splat.SplatImages()
}

// TerrainFoliageDensity returns the current [*world3d.DensityMap] for the named foliage layer, or nil.
func (e *Editor) TerrainFoliageDensity(name string) *world3d.DensityMap {
	if !e.TerrainOpen() {
		return nil
	}
	return e.terrain.foliage[name]
}

// NewTerrain creates a flat editable terrain with 16-bit heightmap precision,
// per-layer weight maps, and optional foliage layers, opens it in the editor,
// and switches the viewport to 3D mode.
func (e *Editor) NewTerrain(opts NewTerrainOptions) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	cols := opts.Cols
	if cols < 2 {
		cols = 33
	}
	rows := opts.Rows
	if rows < 2 {
		rows = 33
	}
	cellSize := opts.CellSize
	if !finite32(cellSize) || cellSize <= 0 {
		cellSize = 2
	}
	heightScale := opts.HeightScale
	if !finite32(heightScale) || heightScale <= 0 {
		heightScale = 32
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "Terrain"
	}
	layers := opts.Layers
	if len(layers) == 0 {
		layers = []world3d.TerrainLayerDoc{
			{Name: "grass", Color: &[3]float32{0.32, 0.58, 0.24}},
			{Name: "rock", Color: &[3]float32{0.48, 0.46, 0.44}},
		}
	} else {
		layers = slices.Clone(layers)
	}
	foliage := slices.Clone(opts.Foliage)

	size := [2]int{cols, rows}
	doc := &world3d.TerrainDoc{
		Version:     world3d.TerrainFormatVersion,
		Name:        name,
		Size:        &size,
		CellSize:    cellSize,
		HeightScale: heightScale,
		Layers:      layers,
		Foliage:     foliage,
	}
	if opts.Origin != ([3]float32{}) {
		orig := opts.Origin
		doc.Origin = &orig
	}

	var absPath, rootDir string
	if opts.Path != "" {
		if e.Project != nil && e.ProjectPath != "" && !filepath.IsAbs(opts.Path) {
			p, err := e.projectFile(withTerrainExt(opts.Path))
			if err != nil {
				return err
			}
			absPath = p
			rootDir = filepath.Dir(e.ProjectPath)
		} else {
			absPath = withTerrainExt(opts.Path)
			rootDir = filepath.Dir(absPath)
		}
		assignDefaultTerrainAssetPaths(doc, absPath, rootDir, true)
	}

	if err := world3d.ValidateTerrain(doc); err != nil {
		return err
	}
	hm := world3d.NewHeightmap(cols, rows, cellSize, heightScale)
	hm.Origin = doc.EffectiveOrigin()

	state, err := newTerrainState(doc, hm, nil, absPath, rootDir, true)
	if err != nil {
		return err
	}
	// Mark newly created terrain dirty if it has a path not yet written, or clean until edited when purely in-memory.
	if absPath != "" {
		state.curStateID = 1
		state.nextStateID = 1
	}

	e.endTerrainStrokeForChange()
	e.terrain = state
	e.clampTerrainBrushIndices()
	e.SetViewMode(ViewMode3D)
	e.Status = "New terrain " + name
	e.notify()
	return nil
}

// OpenTerrain opens an already-constructed TerrainDoc and Heightmap in the editor
// for sculpting and painting without requiring files on disk.
func (e *Editor) OpenTerrain(doc *world3d.TerrainDoc, hm *world3d.Heightmap, foliageDensities map[string]*world3d.DensityMap) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := world3d.ValidateTerrain(doc); err != nil {
		return err
	}
	if hm == nil || hm.Cols < 2 || hm.Rows < 2 || len(hm.Heights) < hm.Cols*hm.Rows {
		return errors.New("editor: invalid heightmap (need at least 2x2 samples)")
	}
	docCopy := cloneTerrainDoc(doc)
	size := [2]int{hm.Cols, hm.Rows}
	docCopy.Size = &size
	useWeightMaps := docCopy.HasLayerWeightMaps() || len(docCopy.SplatMaps) == 0

	state, err := newTerrainState(docCopy, hm, foliageDensities, "", "", useWeightMaps)
	if err != nil {
		return err
	}
	e.endTerrainStrokeForChange()
	e.terrain = state
	e.clampTerrainBrushIndices()
	e.SetViewMode(ViewMode3D)
	e.Status = "Opened terrain"
	e.notify()
	return nil
}

// OpenTerrainFile reads a *.terrain.yaml file and its referenced heightmap,
// splat/weight maps, and foliage density maps from disk and opens them for
// sculpting and painting in the 3D viewport.
func (e *Editor) OpenTerrainFile(yamlPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return err
	}
	doc, err := world3d.ParseTerrainWith(data, yamlPath)
	if err != nil {
		return err
	}
	rootDir := filepath.Dir(yamlPath)
	if e.Project != nil && e.ProjectPath != "" {
		if _, ok := e.projectRel(yamlPath); ok {
			rootDir = filepath.Dir(e.ProjectPath)
		}
	}
	fsys := os.DirFS(rootDir)
	hm, err := doc.LoadHeightmap(fsys)
	if err != nil {
		return err
	}
	useWeightMaps := doc.HasLayerWeightMaps() || len(doc.SplatMaps) == 0
	state, err := newTerrainState(doc, hm, hm.FoliageDensity, yamlPath, rootDir, useWeightMaps)
	if err != nil {
		return err
	}
	e.endTerrainStrokeForChange()
	e.terrain = state
	e.clampTerrainBrushIndices()
	e.SetViewMode(ViewMode3D)
	e.Status = "Opened terrain " + yamlPath
	e.notify()
	return nil
}

// OpenTerrainProjectFile opens relPath (slash-separated from the open project's root)
// in the terrain editor.
func (e *Editor) OpenTerrainProjectFile(relPath string) error {
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
	return e.OpenTerrainFile(abs)
}

// CloseTerrain closes the active terrain editing session.
func (e *Editor) CloseTerrain() {
	if e == nil || e.terrain == nil {
		return
	}
	e.endTerrainStrokeForChange()
	e.terrain = nil
	e.terrainBrush.Mode = TerrainBrushNone
	e.Status = "Closed terrain"
	e.notify()
}

func newTerrainState(doc *world3d.TerrainDoc, hm *world3d.Heightmap, foliageDensities map[string]*world3d.DensityMap, yamlPath, rootDir string, useWeightMaps bool) (*terrainState, error) {
	layerCount := len(doc.Layers)
	weightW, weightH := hm.Cols, hm.Rows
	if hm.Splat != nil && hm.Splat.Width() > 0 && hm.Splat.Height() > 0 {
		weightW, weightH = hm.Splat.Width(), hm.Splat.Height()
	}

	weights := make([][]float32, layerCount)
	totalW := weightW * weightH
	for li := range layerCount {
		weights[li] = make([]float32, totalW)
	}
	if hm.Splat != nil && hm.Splat.LayerCount() == layerCount {
		for ty := range weightH {
			row := ty * weightW
			for tx := range weightW {
				var sum float32
				for li := range layerCount {
					v := hm.Splat.TexelWeight(li, tx, ty)
					weights[li][row+tx] = v
					sum += v
				}
				if sum <= 1e-6 && layerCount > 0 {
					weights[0][row+tx] = 1
				}
			}
		}
	} else {
		// Initialize layer 0 to 100% weight everywhere and attach a synchronized TerrainSplatMap.
		for i := range totalW {
			weights[0][i] = 1
		}
		s0 := make([]byte, totalW*4)
		for i := range totalW {
			s0[i*4] = 255
		}
		var s1 []byte
		if layerCount > 4 {
			s1 = make([]byte, totalW*4)
		}
		splat, err := world3d.NewTerrainSplatMapFromRGBA(weightW, weightH, doc.LayerInfos(), s0, s1)
		if err != nil {
			return nil, err
		}
		splat.HeightBlendContrast = doc.HeightBlendContrast
		hm.Splat = splat
	}

	foliage := make(map[string]*world3d.DensityMap, len(doc.Foliage))
	for _, fl := range doc.Foliage {
		var dm *world3d.DensityMap
		if foliageDensities != nil && foliageDensities[fl.Name] != nil {
			dm = foliageDensities[fl.Name]
		} else if hm.FoliageDensity != nil && hm.FoliageDensity[fl.Name] != nil {
			dm = hm.FoliageDensity[fl.Name]
		} else {
			dm = world3d.NewDensityMap(hm.Cols, hm.Rows)
		}
		foliage[fl.Name] = dm
	}
	if len(foliage) > 0 {
		hm.FoliageDensity = foliage
	}

	return &terrainState{
		doc:           doc,
		hm:            hm,
		path:          yamlPath,
		rootDir:       rootDir,
		useWeightMaps: useWeightMaps,
		weightW:       weightW,
		weightH:       weightH,
		weights:       weights,
		foliage:       foliage,
	}, nil
}

func (e *Editor) clampTerrainBrushIndices() {
	if !e.TerrainOpen() {
		return
	}
	if len(e.terrain.doc.Layers) > 0 && e.terrainBrush.LayerIndex >= len(e.terrain.doc.Layers) {
		e.terrainBrush.LayerIndex = len(e.terrain.doc.Layers) - 1
	}
	if len(e.terrain.doc.Foliage) > 0 && e.terrainBrush.FoliageIndex >= len(e.terrain.doc.Foliage) {
		e.terrainBrush.FoliageIndex = len(e.terrain.doc.Foliage) - 1
	}
}

// SaveTerrain writes the open terrain's 16-bit PNG heightmap, weight/splat maps,
// foliage density maps, and deterministic *.terrain.yaml back to disk.
func (e *Editor) SaveTerrain() error {
	if e == nil || !e.TerrainOpen() {
		return ErrNoTerrain
	}
	if e.playing {
		return ErrPlaying
	}
	if e.terrain.path == "" {
		return ErrNoTerrainPath
	}
	return e.SaveTerrainTo(e.terrain.path)
}

// SaveTerrainTo writes the open terrain's 16-bit grayscale PNG heightmap,
// weight/splat maps, foliage density maps, and deterministic *.terrain.yaml
// to yamlPath (creating parent directories as needed).
func (e *Editor) SaveTerrainTo(yamlPath string) error {
	if e == nil || !e.TerrainOpen() {
		return ErrNoTerrain
	}
	if e.playing {
		return ErrPlaying
	}
	if strings.TrimSpace(yamlPath) == "" {
		return ErrNoTerrainPath
	}
	e.endTerrainStrokeForChange()

	t := e.terrain
	rootDir := filepath.Dir(yamlPath)
	if e.Project != nil && e.ProjectPath != "" {
		if _, ok := e.projectRel(yamlPath); ok {
			rootDir = filepath.Dir(e.ProjectPath)
		}
	}

	size := [2]int{t.hm.Cols, t.hm.Rows}
	t.doc.Size = &size
	t.doc.CellSize = t.hm.CellSize
	t.doc.HeightScale = t.hm.HeightScale
	if t.hm.Origin != math3d.Vec3Zero() {
		orig := [3]float32{t.hm.Origin.X, t.hm.Origin.Y, t.hm.Origin.Z}
		t.doc.Origin = &orig
	} else {
		t.doc.Origin = nil
	}
	assignDefaultTerrainAssetPaths(t.doc, yamlPath, rootDir, t.useWeightMaps)

	resolveOut := func(rel string) string {
		if filepath.IsAbs(rel) {
			return rel
		}
		return filepath.Join(rootDir, filepath.FromSlash(rel))
	}
	writePNG := func(rel string, img image.Image) error {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		dst := resolveOut(rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return writeFileAtomic(dst, buf.Bytes())
	}

	// 1. Write 16-bit grayscale PNG heightmap.
	hmPNG, err := world3d.EncodeHeightmapPNG(t.hm)
	if err != nil {
		return err
	}
	hmDst := resolveOut(t.doc.Heightmap)
	if err := os.MkdirAll(filepath.Dir(hmDst), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(hmDst, hmPNG); err != nil {
		return err
	}

	// 2. Write per-layer weight maps OR packed RGBA splat maps.
	if t.useWeightMaps {
		t.doc.SplatMaps = nil
		for i, l := range t.doc.Layers {
			if l.WeightMap == "" {
				continue
			}
			img := e.TerrainWeightMap(i)
			if img == nil {
				continue
			}
			if err := writePNG(l.WeightMap, img); err != nil {
				return err
			}
		}
	} else if len(t.doc.SplatMaps) > 0 {
		s0, s1 := e.TerrainSplatMaps()
		if s0 != nil {
			if err := writePNG(t.doc.SplatMaps[0], s0); err != nil {
				return err
			}
		}
		if len(t.doc.SplatMaps) > 1 && s1 != nil {
			if err := writePNG(t.doc.SplatMaps[1], s1); err != nil {
				return err
			}
		}
	}

	// 3. Write foliage density map PNGs.
	for i := range t.doc.Foliage {
		fl := &t.doc.Foliage[i]
		dm := t.foliage[fl.Name]
		if dm == nil || fl.DensityMap == "" {
			continue
		}
		if err := writePNG(fl.DensityMap, dm.ToGray()); err != nil {
			return err
		}
	}

	// 4. Write deterministic YAML definition.
	yamlBytes, err := world3d.EncodeTerrain(t.doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(yamlPath, yamlBytes); err != nil {
		return err
	}

	t.path = yamlPath
	t.rootDir = rootDir
	t.savedStateID = t.curStateID
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved terrain " + yamlPath
	e.notify()
	e.CheckProblems()
	return nil
}

func assignDefaultTerrainAssetPaths(doc *world3d.TerrainDoc, yamlPath, rootDir string, useWeightMaps bool) {
	relYAML := filepath.Base(yamlPath)
	if rootDir != "" {
		if r, err := filepath.Rel(rootDir, yamlPath); err == nil && !strings.HasPrefix(r, "..") {
			relYAML = filepath.ToSlash(r)
		}
	}
	stem := strings.TrimSuffix(relYAML, ".terrain.yaml")
	stem = strings.TrimSuffix(stem, ".terrain.yml")
	stem = strings.TrimSuffix(stem, ".yaml")
	stem = strings.TrimSuffix(stem, ".yml")
	if stem == "" {
		stem = "terrain"
	}

	if doc.Heightmap == "" {
		doc.Heightmap = stem + "_height.png"
	}
	if useWeightMaps {
		for i := range doc.Layers {
			if doc.Layers[i].WeightMap == "" {
				doc.Layers[i].WeightMap = stem + "_weight_" + sanitizeAssetToken(doc.Layers[i].Name) + ".png"
			}
		}
	} else {
		need := 1
		if len(doc.Layers) > 4 {
			need = 2
		}
		for len(doc.SplatMaps) < need {
			doc.SplatMaps = append(doc.SplatMaps, fmt.Sprintf("%s_splat%d.png", stem, len(doc.SplatMaps)))
		}
	}
	for i := range doc.Foliage {
		if doc.Foliage[i].DensityMap == "" {
			doc.Foliage[i].DensityMap = stem + "_foliage_" + sanitizeAssetToken(doc.Foliage[i].Name) + ".png"
		}
	}
}

func sanitizeAssetToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "layer"
	}
	return b.String()
}

func withTerrainExt(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.HasSuffix(rel, "/") {
		return rel
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".yaml", ".yml":
		return rel
	case ".terrain":
		return rel + ".yaml"
	}
	return rel + ".terrain.yaml"
}

func isTerrainFile(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".terrain.yaml") || strings.HasSuffix(lower, ".terrain.yml")
}

func cloneTerrainDoc(doc *world3d.TerrainDoc) *world3d.TerrainDoc {
	if doc == nil {
		return nil
	}
	cp := *doc
	if doc.Size != nil {
		s := *doc.Size
		cp.Size = &s
	}
	if doc.Origin != nil {
		o := *doc.Origin
		cp.Origin = &o
	}
	if doc.SplatMaps != nil {
		cp.SplatMaps = slices.Clone(doc.SplatMaps)
	}
	if doc.Triplanar != nil {
		tp := *doc.Triplanar
		cp.Triplanar = &tp
	}
	if doc.Layers != nil {
		cp.Layers = make([]world3d.TerrainLayerDoc, len(doc.Layers))
		for i, l := range doc.Layers {
			lc := l
			if l.Color != nil {
				c := *l.Color
				lc.Color = &c
			}
			if l.Roughness != nil {
				r := *l.Roughness
				lc.Roughness = &r
			}
			if l.NormalScale != nil {
				ns := *l.NormalScale
				lc.NormalScale = &ns
			}
			if l.UVScale != nil {
				uv := *l.UVScale
				lc.UVScale = &uv
			}
			cp.Layers[i] = lc
		}
	}
	if doc.Foliage != nil {
		cp.Foliage = make([]world3d.FoliageLayerDoc, len(doc.Foliage))
		for i, fl := range doc.Foliage {
			fc := fl
			if fl.MinHeight != nil {
				v := *fl.MinHeight
				fc.MinHeight = &v
			}
			if fl.MaxHeight != nil {
				v := *fl.MaxHeight
				fc.MaxHeight = &v
			}
			cp.Foliage[i] = fc
		}
	}
	return &cp
}

// --- Terrain Brush Strokes ---

// TerrainStrokeActive reports whether a terrain brush stroke is currently in progress.
func (e *Editor) TerrainStrokeActive() bool {
	return e != nil && e.terrain != nil && e.terrain.stroke != nil
}

// BeginTerrainStroke starts a brush stroke at world coordinates (worldX, worldZ)
// using the active TerrainBrush, applying the initial dab immediately.
// ContinueTerrainStroke extends the stroke along pointer movement, and
// EndTerrainStroke commits the entire stroke as one undo step in History.
func (e *Editor) BeginTerrainStroke(worldX, worldZ float32) bool {
	if e == nil || e.playing || e.HistoryPreviewActive() || !e.TerrainOpen() || e.terrain.stroke != nil {
		return false
	}
	b := e.terrainBrush
	if b.Mode == TerrainBrushNone {
		return false
	}
	t := e.terrain
	if b.Mode == TerrainBrushPaintLayer && (b.LayerIndex < 0 || b.LayerIndex >= len(t.doc.Layers)) {
		return false
	}
	if b.Mode == TerrainBrushPaintFoliage && (b.FoliageIndex < 0 || b.FoliageIndex >= len(t.doc.Foliage)) {
		return false
	}

	var flattenTarget float32
	switch b.Mode {
	case TerrainBrushFlatten:
		targetWorld := b.TargetHeight
		if !b.UseTargetHeight {
			if y, ok := t.hm.HeightAt(worldX, worldZ); ok {
				targetWorld = y
			} else {
				col := clampInt(int(math.Round(float64((worldX-t.hm.Origin.X)/t.hm.CellSize))), 0, t.hm.Cols-1)
				row := clampInt(int(math.Round(float64((worldZ-t.hm.Origin.Z)/t.hm.CellSize))), 0, t.hm.Rows-1)
				targetWorld = t.hm.SampleHeight(col, row)
			}
		}
		if t.hm.HeightScale > 0 {
			flattenTarget = clamp01f32((targetWorld - t.hm.Origin.Y) / t.hm.HeightScale)
		}
	case TerrainBrushSetHeight:
		if t.hm.HeightScale > 0 {
			flattenTarget = clamp01f32((b.TargetHeight - t.hm.Origin.Y) / t.hm.HeightScale)
		}
	}

	stroke := &terrainStroke{
		mode:          b.Mode,
		brush:         b,
		lastX:         worldX,
		lastZ:         worldZ,
		flattenTarget: flattenTarget,
		layerCount:    len(t.doc.Layers),
	}
	switch b.Mode {
	case TerrainBrushPaintLayer:
		stroke.weightBefore = make(map[int][]float32)
	case TerrainBrushPaintFoliage:
		stroke.foliageName = t.doc.Foliage[b.FoliageIndex].Name
		stroke.densityBefore = make(map[int]float32)
	default:
		stroke.heightBefore = make(map[int]float32)
	}
	t.stroke = stroke
	e.applyTerrainDab(worldX, worldZ)
	e.notify()
	return true
}

// ContinueTerrainStroke extends the active terrain brush stroke from its previous
// world position to (worldX, worldZ), interpolating dabs along the segment so fast
// drags remain smooth without gaps.
func (e *Editor) ContinueTerrainStroke(worldX, worldZ float32) {
	if !e.TerrainStrokeActive() {
		return
	}
	t := e.terrain
	s := t.stroke
	dx := worldX - s.lastX
	dz := worldZ - s.lastZ
	dist := float32(math.Hypot(float64(dx), float64(dz)))
	step := max(s.brush.effectiveRadius()*0.25, t.hm.CellSize*0.5)
	if step <= 1e-4 {
		step = 0.5
	}
	if dist <= step {
		e.applyTerrainDab(worldX, worldZ)
	} else {
		n := int(math.Ceil(float64(dist / step)))
		for i := 1; i <= n; i++ {
			frac := float32(i) / float32(n)
			e.applyTerrainDab(s.lastX+dx*frac, s.lastZ+dz*frac)
		}
	}
	s.lastX = worldX
	s.lastZ = worldZ
	e.notify()
}

// EndTerrainStroke completes the active terrain brush stroke and records all
// modified samples as a single undoable command in History. Returns true when
// a history entry was recorded.
func (e *Editor) EndTerrainStroke() bool {
	if !e.TerrainStrokeActive() {
		return false
	}
	t := e.terrain
	s := t.stroke
	t.stroke = nil

	cmd := &terrainStrokeCmd{
		ed:            e,
		state:         t,
		label:         "Terrain " + s.mode.String(),
		beforeStateID: t.curStateID,
	}

	switch s.mode {
	case TerrainBrushPaintLayer:
		keys := slices.Sorted(maps.Keys(s.weightBefore))
		for _, idx := range keys {
			before := s.weightBefore[idx]
			after := make([]float32, len(t.weights))
			changed := false
			for li := range t.weights {
				after[li] = t.weights[li][idx]
				if after[li] != before[li] {
					changed = true
				}
			}
			if changed {
				cmd.weights = append(cmd.weights, weightDelta{index: idx, before: before, after: after})
			}
		}
		if len(cmd.weights) == 0 {
			return false
		}
		if s.brush.LayerIndex < len(t.doc.Layers) {
			cmd.label = "Terrain Paint " + t.doc.Layers[s.brush.LayerIndex].Name
		}

	case TerrainBrushPaintFoliage:
		dm := t.foliage[s.foliageName]
		if dm == nil {
			return false
		}
		cmd.foliageName = s.foliageName
		keys := slices.Sorted(maps.Keys(s.densityBefore))
		for _, idx := range keys {
			before := s.densityBefore[idx]
			after := dm.Data[idx]
			if after != before {
				cmd.densities = append(cmd.densities, densityDelta{index: idx, before: before, after: after})
			}
		}
		if len(cmd.densities) == 0 {
			return false
		}
		cmd.label = "Terrain Foliage " + s.foliageName

	default:
		keys := slices.Sorted(maps.Keys(s.heightBefore))
		for _, idx := range keys {
			before := s.heightBefore[idx]
			after := t.hm.Heights[idx]
			if after != before {
				cmd.heights = append(cmd.heights, heightDelta{index: idx, before: before, after: after})
			}
		}
		if len(cmd.heights) == 0 {
			return false
		}
	}

	t.nextStateID++
	cmd.afterStateID = t.nextStateID
	e.History.Execute(cmd)
	return true
}

// CancelTerrainStroke aborts the active terrain brush stroke and restores all
// modified samples without recording a History entry.
func (e *Editor) CancelTerrainStroke() {
	if !e.TerrainStrokeActive() {
		return
	}
	t := e.terrain
	s := t.stroke
	t.stroke = nil

	for idx, old := range s.heightBefore {
		if idx >= 0 && idx < len(t.hm.Heights) {
			t.hm.Heights[idx] = old
		}
	}
	if len(s.weightBefore) > 0 && t.weightW > 0 && t.weightH > 0 {
		for idx, old := range s.weightBefore {
			tx := idx % t.weightW
			ty := idx / t.weightW
			for li := 0; li < len(t.weights) && li < len(old); li++ {
				t.weights[li][idx] = old[li]
				if t.hm.Splat != nil {
					t.hm.Splat.SetTexelWeight(li, tx, ty, old[li])
				}
			}
		}
	}
	if len(s.densityBefore) > 0 && s.foliageName != "" {
		if dm := t.foliage[s.foliageName]; dm != nil {
			for idx, old := range s.densityBefore {
				if idx >= 0 && idx < len(dm.Data) {
					dm.Data[idx] = old
				}
			}
		}
	}
	e.notify()
}

func (e *Editor) endTerrainStrokeForChange() {
	if e != nil && e.TerrainStrokeActive() {
		e.EndTerrainStroke()
	}
}

func (e *Editor) applyTerrainDab(wx, wz float32) {
	t := e.terrain
	s := t.stroke
	hm := t.hm
	radius := s.brush.effectiveRadius()
	strength := s.brush.effectiveStrength()
	falloff := s.brush.Falloff

	switch s.mode {
	case TerrainBrushRaise, TerrainBrushLower:
		const raiseStep float32 = 0.05
		dir := float32(1)
		if s.mode == TerrainBrushLower {
			dir = -1
		}
		cMin, cMax, rMin, rMax := heightGridRange(hm, wx, wz, radius)
		for r := rMin; r <= rMax; r++ {
			sz := hm.Origin.Z + float32(r)*hm.CellSize
			dz := sz - wz
			rowBase := r * hm.Cols
			for c := cMin; c <= cMax; c++ {
				sx := hm.Origin.X + float32(c)*hm.CellSize
				dist := float32(math.Hypot(float64(sx-wx), float64(dz)))
				if dist > radius {
					continue
				}
				w := evalTerrainFalloff(dist/radius, falloff)
				if w <= 0 {
					continue
				}
				idx := rowBase + c
				old := hm.Heights[idx]
				if _, seen := s.heightBefore[idx]; !seen {
					s.heightBefore[idx] = old
				}
				hm.Heights[idx] = clamp01f32(old + dir*strength*w*raiseStep)
			}
		}

	case TerrainBrushSmooth:
		cMin, cMax, rMin, rMax := heightGridRange(hm, wx, wz, radius)
		if cMin > cMax || rMin > rMax {
			return
		}
		// Snapshot neighborhood so smoothing has no scan-order bias.
		snapC0 := max(0, cMin-1)
		snapC1 := min(hm.Cols-1, cMax+1)
		snapR0 := max(0, rMin-1)
		snapR1 := min(hm.Rows-1, rMax+1)
		snapW := snapC1 - snapC0 + 1
		snapH := snapR1 - snapR0 + 1
		snap := make([]float32, snapW*snapH)
		for r := snapR0; r <= snapR1; r++ {
			copy(snap[(r-snapR0)*snapW:(r-snapR0+1)*snapW], hm.Heights[r*hm.Cols+snapC0:r*hm.Cols+snapC1+1])
		}
		atSnap := func(c, r int) float32 {
			return snap[(r-snapR0)*snapW+(c-snapC0)]
		}

		for r := rMin; r <= rMax; r++ {
			sz := hm.Origin.Z + float32(r)*hm.CellSize
			dz := sz - wz
			rowBase := r * hm.Cols
			for c := cMin; c <= cMax; c++ {
				sx := hm.Origin.X + float32(c)*hm.CellSize
				dist := float32(math.Hypot(float64(sx-wx), float64(dz)))
				if dist > radius {
					continue
				}
				w := evalTerrainFalloff(dist/radius, falloff)
				if w <= 0 {
					continue
				}
				// 3x3 Gaussian-weighted neighborhood average.
				var sum, wSum float32
				for nr := max(0, r-1); nr <= min(hm.Rows-1, r+1); nr++ {
					for nc := max(0, c-1); nc <= min(hm.Cols-1, c+1); nc++ {
						var kw float32 = 1
						if nc == c && nr == r {
							kw = 4
						} else if nc == c || nr == r {
							kw = 2
						}
						sum += atSnap(nc, nr) * kw
						wSum += kw
					}
				}
				avg := sum / wSum
				idx := rowBase + c
				old := atSnap(c, r)
				if _, seen := s.heightBefore[idx]; !seen {
					s.heightBefore[idx] = old
				}
				alpha := clamp01f32(strength * w)
				hm.Heights[idx] = clamp01f32(old + (avg-old)*alpha)
			}
		}

	case TerrainBrushFlatten, TerrainBrushSetHeight:
		target := s.flattenTarget
		cMin, cMax, rMin, rMax := heightGridRange(hm, wx, wz, radius)
		for r := rMin; r <= rMax; r++ {
			sz := hm.Origin.Z + float32(r)*hm.CellSize
			dz := sz - wz
			rowBase := r * hm.Cols
			for c := cMin; c <= cMax; c++ {
				sx := hm.Origin.X + float32(c)*hm.CellSize
				dist := float32(math.Hypot(float64(sx-wx), float64(dz)))
				if dist > radius {
					continue
				}
				w := evalTerrainFalloff(dist/radius, falloff)
				if w <= 0 {
					continue
				}
				idx := rowBase + c
				old := hm.Heights[idx]
				if _, seen := s.heightBefore[idx]; !seen {
					s.heightBefore[idx] = old
				}
				alpha := clamp01f32(strength * w)
				hm.Heights[idx] = clamp01f32(old + (target-old)*alpha)
			}
		}

	case TerrainBrushPaintLayer:
		layerIdx := s.brush.LayerIndex
		if layerIdx < 0 || layerIdx >= len(t.weights) || t.weightW <= 0 || t.weightH <= 0 {
			return
		}
		spanX := float32(hm.Cols-1) * hm.CellSize
		spanZ := float32(hm.Rows-1) * hm.CellSize
		if spanX <= 0 || spanZ <= 0 {
			return
		}
		txMin := clampInt(int(math.Floor(float64(((wx-radius-hm.Origin.X)/spanX)*float32(t.weightW)))), 0, t.weightW-1)
		txMax := clampInt(int(math.Ceil(float64(((wx+radius-hm.Origin.X)/spanX)*float32(t.weightW)))), 0, t.weightW-1)
		tyMin := clampInt(int(math.Floor(float64(((wz-radius-hm.Origin.Z)/spanZ)*float32(t.weightH)))), 0, t.weightH-1)
		tyMax := clampInt(int(math.Ceil(float64(((wz+radius-hm.Origin.Z)/spanZ)*float32(t.weightH)))), 0, t.weightH-1)

		for ty := tyMin; ty <= tyMax; ty++ {
			v := (float32(ty) + 0.5) / float32(t.weightH)
			tz := hm.Origin.Z + v*spanZ
			dz := tz - wz
			rowBase := ty * t.weightW
			for tx := txMin; tx <= txMax; tx++ {
				u := (float32(tx) + 0.5) / float32(t.weightW)
				txWorld := hm.Origin.X + u*spanX
				dist := float32(math.Hypot(float64(txWorld-wx), float64(dz)))
				if dist > radius {
					continue
				}
				w := evalTerrainFalloff(dist/radius, falloff)
				if w <= 0 {
					continue
				}
				idx := rowBase + tx
				if _, seen := s.weightBefore[idx]; !seen {
					snap := make([]float32, len(t.weights))
					for li := range t.weights {
						snap[li] = t.weights[li][idx]
					}
					s.weightBefore[idx] = snap
				}
				alpha := clamp01f32(strength * w)
				oldK := t.weights[layerIdx][idx]
				newK := clamp01f32(oldK + (1-oldK)*alpha)
				var otherSum float32
				for li := range t.weights {
					if li != layerIdx {
						otherSum += t.weights[li][idx]
					}
				}
				rem := 1 - newK
				if len(t.weights) == 1 {
					newK = 1
				} else if otherSum > 1e-6 {
					scale := rem / otherSum
					for li := range t.weights {
						if li != layerIdx {
							t.weights[li][idx] = clamp01f32(t.weights[li][idx] * scale)
						}
					}
				}
				t.weights[layerIdx][idx] = newK
				if hm.Splat != nil {
					for li := range t.weights {
						hm.Splat.SetTexelWeight(li, tx, ty, t.weights[li][idx])
					}
				}
			}
		}

	case TerrainBrushPaintFoliage:
		dm := t.foliage[s.foliageName]
		if dm == nil || dm.Width <= 0 || dm.Height <= 0 {
			return
		}
		spanX := float32(hm.Cols-1) * hm.CellSize
		spanZ := float32(hm.Rows-1) * hm.CellSize
		if spanX <= 0 || spanZ <= 0 {
			return
		}
		target := s.brush.effectiveTargetDensity()
		denomX := float32(max(dm.Width-1, 1))
		denomY := float32(max(dm.Height-1, 1))
		txMin := clampInt(int(math.Floor(float64(((wx-radius-hm.Origin.X)/spanX)*denomX))), 0, dm.Width-1)
		txMax := clampInt(int(math.Ceil(float64(((wx+radius-hm.Origin.X)/spanX)*denomX))), 0, dm.Width-1)
		tyMin := clampInt(int(math.Floor(float64(((wz-radius-hm.Origin.Z)/spanZ)*denomY))), 0, dm.Height-1)
		tyMax := clampInt(int(math.Ceil(float64(((wz+radius-hm.Origin.Z)/spanZ)*denomY))), 0, dm.Height-1)

		for ty := tyMin; ty <= tyMax; ty++ {
			v := float32(0.5)
			if dm.Height > 1 {
				v = float32(ty) / denomY
			}
			tz := hm.Origin.Z + v*spanZ
			dz := tz - wz
			rowBase := ty * dm.Width
			for tx := txMin; tx <= txMax; tx++ {
				u := float32(0.5)
				if dm.Width > 1 {
					u = float32(tx) / denomX
				}
				txWorld := hm.Origin.X + u*spanX
				dist := float32(math.Hypot(float64(txWorld-wx), float64(dz)))
				if dist > radius {
					continue
				}
				w := evalTerrainFalloff(dist/radius, falloff)
				if w <= 0 {
					continue
				}
				idx := rowBase + tx
				old := dm.Data[idx]
				if _, seen := s.densityBefore[idx]; !seen {
					s.densityBefore[idx] = old
				}
				alpha := clamp01f32(strength * w)
				dm.Data[idx] = clamp01f32(old + (target-old)*alpha)
			}
		}
	}
}

func heightGridRange(hm *world3d.Heightmap, wx, wz, radius float32) (cMin, cMax, rMin, rMax int) {
	if hm == nil || hm.CellSize <= 0 || hm.Cols <= 0 || hm.Rows <= 0 {
		return 1, 0, 1, 0
	}
	cMin = clampInt(int(math.Floor(float64((wx-radius-hm.Origin.X)/hm.CellSize))), 0, hm.Cols-1)
	cMax = clampInt(int(math.Ceil(float64((wx+radius-hm.Origin.X)/hm.CellSize))), 0, hm.Cols-1)
	rMin = clampInt(int(math.Floor(float64((wz-radius-hm.Origin.Z)/hm.CellSize))), 0, hm.Rows-1)
	rMax = clampInt(int(math.Ceil(float64((wz+radius-hm.Origin.Z)/hm.CellSize))), 0, hm.Rows-1)
	return cMin, cMax, rMin, rMax
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- 3D Viewport Raycasting & Rendering ---

// RaycastTerrain3D casts a ray from the 3D viewport camera through pixel (sx, sy)
// and intersects it against the open terrain's Heightmap.
func (e *Editor) RaycastTerrain3D(sx, sy, w, h float32) (world3d.TerrainHit, bool) {
	if !e.TerrainOpen() || w <= 0 || h <= 0 {
		return world3d.TerrainHit{}, false
	}
	orig, dir := e.Camera3D.ScreenRay(sx, sy, w, h)
	ray := math3d.Ray{
		Origin: math3d.Vec3{X: orig[0], Y: orig[1], Z: orig[2]},
		Dir:    math3d.Vec3{X: dir[0], Y: dir[1], Z: dir[2]},
	}
	return e.terrain.hm.Raycast(ray, 50000)
}

// BeginTerrainStrokeAt raycasts pixel (sx, sy) against the open terrain in the
// 3D viewport and starts a brush stroke at the hit point.
func (e *Editor) BeginTerrainStrokeAt(sx, sy, w, h float32) bool {
	hit, ok := e.RaycastTerrain3D(sx, sy, w, h)
	if !ok {
		return false
	}
	e.terrain.hoverOK = true
	e.terrain.hoverPos = hit.Point
	return e.BeginTerrainStroke(hit.Point.X, hit.Point.Z)
}

// ContinueTerrainStrokeAt raycasts pixel (sx, sy) against the open terrain and
// extends the active brush stroke to the hit point.
func (e *Editor) ContinueTerrainStrokeAt(sx, sy, w, h float32) {
	if !e.TerrainStrokeActive() {
		return
	}
	hit, ok := e.RaycastTerrain3D(sx, sy, w, h)
	if !ok {
		return
	}
	e.terrain.hoverOK = true
	e.terrain.hoverPos = hit.Point
	e.ContinueTerrainStroke(hit.Point.X, hit.Point.Z)
}

// SetTerrainHoverAt updates the 3D terrain brush cursor preview at viewport pixel (sx, sy).
func (e *Editor) SetTerrainHoverAt(sx, sy, w, h float32) {
	if !e.TerrainOpen() || e.terrainBrush.Mode == TerrainBrushNone || e.playing {
		return
	}
	hit, ok := e.RaycastTerrain3D(sx, sy, w, h)
	changed := ok != e.terrain.hoverOK || (ok && !hit.Point.ApproxEqual(e.terrain.hoverPos, 1e-3))
	e.terrain.hoverOK = ok
	if ok {
		e.terrain.hoverPos = hit.Point
	}
	if changed {
		e.notify()
	}
}

// ClearTerrainHover hides the 3D terrain brush cursor preview.
func (e *Editor) ClearTerrainHover() {
	if !e.TerrainOpen() || !e.terrain.hoverOK {
		return
	}
	e.terrain.hoverOK = false
	e.notify()
}

// terrainLayerPreviewColors gives distinct visual tints from spec_theme.yaml for up to 8 terrain layers
// in the 3D viewport wireframe when a layer has no explicit RGB tint.
func terrainLayerPreviewColors(spec SpecPalette) [8]core.Color {
	return [8]core.Color{
		specAlpha(spec.Added.Fg, 0.85),
		specAlpha(spec.Changed.Fg, 0.85),
		specAlpha(spec.TextFaint, 0.85),
		specAlpha(spec.TextPrimary, 0.90),
		specAlpha(spec.Warning.Fg, 0.85),
		specAlpha(spec.TextMuted, 0.85),
		specAlpha(spec.Agent.Fg, 0.85),
		specAlpha(spec.Accent.Fg, 0.85),
	}
}

func (e *Editor) drawTerrain3D(d *core.Drawing, w, h float32) {
	if !e.TerrainOpen() {
		return
	}
	spec := e.activeSpecPalette()
	previewColors := terrainLayerPreviewColors(spec)
	t := e.terrain
	hm := t.hm
	if hm.Cols < 2 || hm.Rows < 2 {
		return
	}
	// Subsample dense terrains to keep the 2D canvas preview responsive.
	stepC := max(1, (hm.Cols-1)/48)
	stepR := max(1, (hm.Rows-1)/48)

	// Draw heightfield grid colored by dominant painted layer.
	layerColors := make([]core.Color, len(t.doc.Layers))
	for i, l := range t.doc.Layers {
		if l.Color != nil {
			r := uint8(clamp01f32(l.Color[0]) * 255)
			g := uint8(clamp01f32(l.Color[1]) * 255)
			b := uint8(clamp01f32(l.Color[2]) * 255)
			layerColors[i] = specAlpha(colorFromRGBA8(r, g, b, 255), 0.85)
		} else {
			layerColors[i] = previewColors[i%len(previewColors)]
		}
	}

	samplePos := func(c, r int) [3]float32 {
		return [3]float32{
			hm.Origin.X + float32(c)*hm.CellSize,
			hm.SampleHeight(c, r),
			hm.Origin.Z + float32(r)*hm.CellSize,
		}
	}

	for r := 0; r < hm.Rows; r += stepR {
		nextR := min(r+stepR, hm.Rows-1)
		for c := 0; c < hm.Cols; c += stepC {
			nextC := min(c+stepC, hm.Cols-1)
			p0 := samplePos(c, r)
			col := previewColors[0]
			if len(layerColors) > 0 {
				if sample, ok := hm.LayerAt(p0[0], p0[2]); ok && sample.Dominant < len(layerColors) {
					col = layerColors[sample.Dominant]
				} else {
					col = layerColors[0]
				}
			}
			d.Save().SetStrokeColor(col).SetLineWidth(1.1).BeginPath()
			hasSeg := false
			if nextC > c {
				p1 := samplePos(nextC, r)
				if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(p0, p1, w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasSeg = true
				}
			}
			if nextR > r {
				p2 := samplePos(c, nextR)
				if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(p0, p2, w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasSeg = true
				}
			}
			if hasSeg {
				d.Stroke()
			}
			d.Restore()
		}
	}

	// Highlight foliage density dots when painting foliage or when density maps have non-zero values.
	if len(t.doc.Foliage) > 0 && e.terrainBrush.FoliageIndex >= 0 && e.terrainBrush.FoliageIndex < len(t.doc.Foliage) {
		flName := t.doc.Foliage[e.terrainBrush.FoliageIndex].Name
		if dm := t.foliage[flName]; dm != nil {
			for r := 0; r < hm.Rows; r += stepR {
				for c := 0; c < hm.Cols; c += stepC {
					p := samplePos(c, r)
					if dens, ok := hm.FoliageDensityAt(flName, p[0], p[2]); ok && dens > 0.05 {
						if sx, sy, _, vis := e.Camera3D.Project(p, w, h); vis {
							rad := float32(1.5 + 2.5*dens)
							d.Save().SetFillColor(specAlpha(spec.Added.Fg, float32(0.35+0.6*dens))).
								BeginPath().Arc(sx, sy, rad, 0, float32(2*math.Pi), false).Fill().Restore()
						}
					}
				}
			}
		}
	}

	// Draw brush radius ring projected onto the terrain surface.
	if !e.playing && e.terrainBrush.Mode != TerrainBrushNone && t.hoverOK {
		const ringSteps = 32
		radius := e.terrainBrush.effectiveRadius()
		center := t.hoverPos
		ringColor := specAlpha(spec.Accent.Fg, 0.95)
		switch e.terrainBrush.Mode {
		case TerrainBrushLower:
			ringColor = specAlpha(spec.Removed.Fg, 0.95)
		case TerrainBrushPaintFoliage:
			ringColor = specAlpha(spec.Added.Fg, 0.95)
		case TerrainBrushPaintLayer:
			ringColor = specAlpha(spec.Warning.Fg, 0.95)
		}
		ringPt := func(i int) [3]float32 {
			a := (2 * math.Pi * float64(i)) / ringSteps
			px := center.X + float32(math.Cos(a))*radius
			pz := center.Z + float32(math.Sin(a))*radius
			py := center.Y
			if y, ok := hm.HeightAt(px, pz); ok {
				py = y
			}
			return [3]float32{px, py + 0.05, pz}
		}
		d.Save().SetStrokeColor(ringColor).SetLineWidth(2.0).BeginPath()
		hasSeg := false
		prev := ringPt(0)
		for i := 1; i <= ringSteps; i++ {
			cur := ringPt(i % ringSteps)
			if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(prev, cur, w, h); vis {
				d.MoveTo(x0, y0).LineTo(x1, y1)
				hasSeg = true
			}
			prev = cur
		}
		if hasSeg {
			d.Stroke()
		}
		d.Restore()
	}
}

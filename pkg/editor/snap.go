package editor

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	defaultGridSize2D      float32 = 8
	defaultRotationStepDeg float32 = 15
	defaultScaleStep       float32 = 0.25
	// minSnapGridCanvasSpacing is the minimum distance in canvas points between adjacent 2D snap grid lines; when zoomed out, the drawn step doubles until lines stay at least this far apart.
	minSnapGridCanvasSpacing float32 = 4
	// maxSnapGridLinesPerAxis caps the lines drawn along one axis of the 2D snap grid overlay.
	maxSnapGridLinesPerAxis = 512
)

// SnapSettings holds the editor's snapping and 2D grid overlay preferences.
// They are editor preferences persisted per project in ViewStateFile (.game/editor.yaml), never in scene files.
type SnapSettings struct {
	// Grid snaps object placement, drags, 2D handles, and 3D translate gizmos to EffectiveGridStep.
	Grid bool `yaml:"grid,omitempty"`
	// GridSize is an explicit grid step in world/layer units; 0 means follow the active layer's tilemap tile size (or 8 when it has none).
	GridSize float32 `yaml:"gridSize,omitempty"`
	// Pixel snaps positions to whole points (1-unit increments) when Grid is off.
	Pixel bool `yaml:"pixel,omitempty"`
	// Rotation snaps 2D and 3D rotation drags to RotationStepDeg-degree increments.
	Rotation bool `yaml:"rotation,omitempty"`
	// RotationStepDeg is the rotation snap step in degrees (defaults to 15 when <= 0).
	RotationStepDeg float32 `yaml:"rotationStepDeg,omitempty"`
	// Scale snaps 2D and 3D scale drags to ScaleStep increments.
	Scale bool `yaml:"scale,omitempty"`
	// ScaleStep is the scale snap step (defaults to 0.25 when <= 0).
	ScaleStep float32 `yaml:"scaleStep,omitempty"`
	// ShowGrid draws a 2D grid overlay at the active snap step in the 2D edit viewport, following View2D.
	ShowGrid bool `yaml:"showGrid,omitempty"`
}

// DefaultSnapSettings returns the default snapping preferences (all snaps and grid overlay off, auto grid size, 15° rotation step, 0.25 scale step).
func DefaultSnapSettings() SnapSettings {
	return SnapSettings{
		GridSize:        0,
		RotationStepDeg: defaultRotationStepDeg,
		ScaleStep:       defaultScaleStep,
	}
}

func (s SnapSettings) valid() error {
	if !isFiniteFloat32(s.GridSize) || s.GridSize < 0 {
		return fmt.Errorf("snap.gridSize must be finite and non-negative (got %v)", s.GridSize)
	}
	if !isFiniteFloat32(s.RotationStepDeg) || s.RotationStepDeg < 0 {
		return fmt.Errorf("snap.rotationStepDeg must be finite and non-negative (got %v)", s.RotationStepDeg)
	}
	if !isFiniteFloat32(s.ScaleStep) || s.ScaleStep < 0 {
		return fmt.Errorf("snap.scaleStep must be finite and non-negative (got %v)", s.ScaleStep)
	}
	return nil
}

// normalizedForViewState converts live settings into the compact YAML form written to ViewStateFile,
// returning nil when all settings match DefaultSnapSettings so untouched projects omit the snap section.
func (s SnapSettings) normalizedForViewState() *SnapSettings {
	out := SnapSettings{
		Grid:     s.Grid,
		Pixel:    s.Pixel,
		Rotation: s.Rotation,
		Scale:    s.Scale,
		ShowGrid: s.ShowGrid,
	}
	if isFiniteFloat32(s.GridSize) && s.GridSize > 0 {
		out.GridSize = s.GridSize
	}
	if isFiniteFloat32(s.RotationStepDeg) && s.RotationStepDeg > 0 && s.RotationStepDeg != defaultRotationStepDeg {
		out.RotationStepDeg = s.RotationStepDeg
	}
	if isFiniteFloat32(s.ScaleStep) && s.ScaleStep > 0 && s.ScaleStep != defaultScaleStep {
		out.ScaleStep = s.ScaleStep
	}
	if out == (SnapSettings{}) {
		return nil
	}
	return &out
}

// SnapSettings returns a snapshot of the editor's current snapping and grid overlay preferences.
func (e *Editor) SnapSettings() SnapSettings {
	if e == nil {
		return DefaultSnapSettings()
	}
	return SnapSettings{
		Grid:            e.SnapToGrid,
		GridSize:        e.GridSize,
		Pixel:           e.SnapToPixel,
		Rotation:        e.SnapRotation,
		RotationStepDeg: e.EffectiveRotationSnapDeg(),
		Scale:           e.SnapScale,
		ScaleStep:       e.EffectiveScaleSnapStep(),
		ShowGrid:        e.ShowGrid,
	}
}

// SetSnapSettings applies s to the editor, refusing non-finite or negative step values.
// Zero step values for RotationStepDeg and ScaleStep restore their defaults (15° and 0.25);
// GridSize 0 follows the active layer's tilemap tile size (or 8 when it has none).
func (e *Editor) SetSnapSettings(s SnapSettings) bool {
	if e == nil || s.valid() != nil {
		return false
	}
	rotStep := s.RotationStepDeg
	if rotStep == 0 {
		rotStep = defaultRotationStepDeg
	}
	scaleStep := s.ScaleStep
	if scaleStep == 0 {
		scaleStep = defaultScaleStep
	}
	if e.SnapToGrid == s.Grid &&
		e.GridSize == s.GridSize &&
		e.SnapToPixel == s.Pixel &&
		e.SnapRotation == s.Rotation &&
		e.RotationSnapDeg == rotStep &&
		e.SnapScale == s.Scale &&
		e.ScaleSnapStep == scaleStep &&
		e.ShowGrid == s.ShowGrid {
		return true
	}
	e.SnapToGrid = s.Grid
	e.GridSize = s.GridSize
	e.SnapToPixel = s.Pixel
	e.SnapRotation = s.Rotation
	e.RotationSnapDeg = rotStep
	e.SnapScale = s.Scale
	e.ScaleSnapStep = scaleStep
	e.ShowGrid = s.ShowGrid
	e.persistSnapToSettings()
	e.notify()
	return true
}

func (e *Editor) applyViewStateSnap(s *SnapSettings) {
	def := DefaultSnapSettings()
	if s == nil {
		e.SnapToGrid = def.Grid
		e.GridSize = def.GridSize
		e.SnapToPixel = def.Pixel
		e.SnapRotation = def.Rotation
		e.RotationSnapDeg = def.RotationStepDeg
		e.SnapScale = def.Scale
		e.ScaleSnapStep = def.ScaleStep
		e.ShowGrid = def.ShowGrid
		return
	}
	e.SnapToGrid = s.Grid
	e.GridSize = s.GridSize
	e.SnapToPixel = s.Pixel
	e.SnapRotation = s.Rotation
	if s.RotationStepDeg > 0 && isFiniteFloat32(s.RotationStepDeg) {
		e.RotationSnapDeg = s.RotationStepDeg
	} else {
		e.RotationSnapDeg = defaultRotationStepDeg
	}
	e.SnapScale = s.Scale
	if s.ScaleStep > 0 && isFiniteFloat32(s.ScaleStep) {
		e.ScaleSnapStep = s.ScaleStep
	} else {
		e.ScaleSnapStep = defaultScaleStep
	}
	e.ShowGrid = s.ShowGrid
}

// SetSnapToGrid toggles grid snapping for object placement, drags, 2D handles, and 3D translate gizmos.
func (e *Editor) SetSnapToGrid(on bool) {
	if e == nil || e.SnapToGrid == on {
		return
	}
	e.SnapToGrid = on
	e.persistSnapToSettings()
	e.notify()
}

// SetGridSize sets an explicit grid spacing in world/layer units used when grid snapping is active.
// Non-positive or non-finite values are refused; call ResetGridSize to follow the active layer's tilemap tile size again.
func (e *Editor) SetGridSize(size float32) bool {
	if e == nil || !isFiniteFloat32(size) || size <= 0 {
		return false
	}
	if e.GridSize == size {
		return true
	}
	e.GridSize = size
	e.persistSnapToSettings()
	e.notify()
	return true
}

// ResetGridSize clears any explicit GridSize override so the grid step defaults to the active layer's tilemap tile size (or 8 when it has none).
func (e *Editor) ResetGridSize() {
	if e == nil || e.GridSize == 0 {
		return
	}
	e.GridSize = 0
	e.persistSnapToSettings()
	e.notify()
}

// SetSnapToPixel toggles pixel snapping (whole 1-point increments) when grid snapping is off.
func (e *Editor) SetSnapToPixel(on bool) {
	if e == nil || e.SnapToPixel == on {
		return
	}
	e.SnapToPixel = on
	e.persistSnapToSettings()
	e.notify()
}

// SetSnapRotation toggles rotation snapping in 2D and 3D rotation drags.
func (e *Editor) SetSnapRotation(on bool) {
	if e == nil || e.SnapRotation == on {
		return
	}
	e.SnapRotation = on
	e.persistSnapToSettings()
	e.notify()
}

// SetRotationSnapDeg sets the rotation snap step in degrees. Non-positive or non-finite values are refused.
func (e *Editor) SetRotationSnapDeg(deg float32) bool {
	if e == nil || !isFiniteFloat32(deg) || deg <= 0 {
		return false
	}
	if e.RotationSnapDeg == deg {
		return true
	}
	e.RotationSnapDeg = deg
	e.persistSnapToSettings()
	e.notify()
	return true
}

// SetSnapScale toggles scale snapping in 2D and 3D scale drags.
func (e *Editor) SetSnapScale(on bool) {
	if e == nil || e.SnapScale == on {
		return
	}
	e.SnapScale = on
	e.persistSnapToSettings()
	e.notify()
}

// SetScaleSnapStep sets the scale snap increment. Non-positive or non-finite values are refused.
func (e *Editor) SetScaleSnapStep(step float32) bool {
	if e == nil || !isFiniteFloat32(step) || step <= 0 {
		return false
	}
	if e.ScaleSnapStep == step {
		return true
	}
	e.ScaleSnapStep = step
	e.persistSnapToSettings()
	e.notify()
	return true
}

// SetShowGrid toggles the optional 2D snap grid overlay in the 2D edit viewport.
func (e *Editor) SetShowGrid(show bool) {
	if e == nil || e.ShowGrid == show {
		return
	}
	e.ShowGrid = show
	e.persistSnapToSettings()
	e.notify()
}

// SetSnapInvert sets whether snapping is temporarily inverted (as when holding Ctrl or Cmd during a drag).
func (e *Editor) SetSnapInvert(invert bool) {
	if e == nil {
		return
	}
	e.snapInvert = invert
}

// SnapInvert reports whether temporary snap inversion is currently active.
func (e *Editor) SnapInvert() bool {
	return e != nil && e.snapInvert
}

func (e *Editor) withSnapInvert(invert bool, fn func()) {
	if e == nil {
		fn()
		return
	}
	prev := e.snapInvert
	e.snapInvert = prev || invert
	defer func() { e.snapInvert = prev }()
	fn()
}

// activeLayerTileSize returns the active layer's tilemap tile width and height when the active layer has a tilemap.
func (e *Editor) activeLayerTileSize() (tw, th float32, ok bool) {
	if e == nil || e.Doc == nil || len(e.Doc.Layers) == 0 {
		return 0, 0, false
	}
	layerName := e.ActiveLayer
	if layerName == "" {
		layerName = e.Doc.Layers[0].Name
	}
	if _, tm, err := e.resolveInlineTilemap(layerName); err == nil && tm != nil && tm.TileWidth > 0 && tm.TileHeight > 0 {
		return float32(tm.TileWidth), float32(tm.TileHeight), true
	}
	if scene := e.ActiveScene(); scene != nil {
		if bl := scene.Layer(layerName); bl != nil && bl.Map != nil && bl.Map.TileWidth > 0 && bl.Map.TileHeight > 0 {
			return bl.Map.TileWidth, bl.Map.TileHeight, true
		}
	}
	return 0, 0, false
}

// EffectiveGridStep returns the active (X, Y) grid snap step: GridSize when explicitly set (> 0),
// else the active layer's tilemap tile size when it has one, else (8, 8).
func (e *Editor) EffectiveGridStep() (stepX, stepY float32) {
	if e != nil && e.GridSize > 0 && isFiniteFloat32(e.GridSize) {
		return e.GridSize, e.GridSize
	}
	if tw, th, ok := e.activeLayerTileSize(); ok {
		return tw, th
	}
	return defaultGridSize2D, defaultGridSize2D
}

// EffectiveGridSize returns the active grid snap step along X, defaulting to the active layer's tile width when it has a tilemap, or 8 otherwise.
func (e *Editor) EffectiveGridSize() float32 {
	sx, _ := e.EffectiveGridStep()
	return sx
}

// EffectiveRotationSnapDeg returns the active rotation snap step in degrees, defaulting to 15 when unset or non-positive.
func (e *Editor) EffectiveRotationSnapDeg() float32 {
	if e != nil && e.RotationSnapDeg > 0 && isFiniteFloat32(e.RotationSnapDeg) {
		return e.RotationSnapDeg
	}
	return defaultRotationStepDeg
}

// EffectiveScaleSnapStep returns the active scale snap step, defaulting to 0.25 when unset or non-positive.
func (e *Editor) EffectiveScaleSnapStep() float32 {
	if e != nil && e.ScaleSnapStep > 0 && isFiniteFloat32(e.ScaleSnapStep) {
		return e.ScaleSnapStep
	}
	return defaultScaleStep
}

func (e *Editor) positionSnapMode() (grid, pixel bool) {
	if e == nil {
		return false, false
	}
	grid, pixel = e.SnapToGrid, e.SnapToPixel
	if e.snapInvert {
		if grid || pixel {
			return false, false
		}
		return true, false
	}
	return grid, pixel
}

func (e *Editor) rotationSnapActive() bool {
	return e != nil && (e.SnapRotation != e.snapInvert)
}

func (e *Editor) scaleSnapActive() bool {
	return e != nil && (e.SnapScale != e.snapInvert)
}

func snapStep64(v, step float64) float64 {
	if !(step > 0) || math.IsNaN(step) || math.IsInf(step, 0) || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	q := v / step
	if math.IsNaN(q) || math.IsInf(q, 0) {
		return v
	}
	r := math.Round(q) * step
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return v
	}
	if r == 0 {
		return 0
	}
	return r
}

// SnapValue snaps v to the active grid step (or whole points when pixel snap is active), respecting temporary snap inversion, and normalizes -0 to 0.
func (e *Editor) SnapValue(v float64) float64 {
	grid, pixel := e.positionSnapMode()
	if grid {
		v = snapStep64(v, float64(e.EffectiveGridSize()))
	} else if pixel {
		v = snapStep64(v, 1)
	}
	if v == 0 {
		return 0
	}
	return v
}

// SnapAxis2D snaps a 2D coordinate along axis (0 for X, 1 for Y) according to the active grid/pixel snap settings and temporary inversion.
func (e *Editor) SnapAxis2D(v float64, axis int) float64 {
	grid, pixel := e.positionSnapMode()
	if grid {
		sx, sy := e.EffectiveGridStep()
		step := float64(sx)
		if axis == 1 {
			step = float64(sy)
		}
		v = snapStep64(v, step)
	} else if pixel {
		v = snapStep64(v, 1)
	}
	if v == 0 {
		return 0
	}
	return v
}

// SnapPosition2D snaps (x, y) according to the active grid/pixel snap settings and temporary inversion.
func (e *Editor) SnapPosition2D(x, y float64) (float64, float64) {
	return e.SnapAxis2D(x, 0), e.SnapAxis2D(y, 1)
}

// SnapPosition3D snaps a 3D coordinate (x, y, z) according to the active grid/pixel snap settings and temporary inversion.
func (e *Editor) SnapPosition3D(x, y, z float64) (float64, float64, float64) {
	return e.SnapValue(x), e.SnapValue(y), e.SnapValue(z)
}

// SnapRotationRadians snaps rad to multiples of EffectiveRotationSnapDeg in radians when rotation snap (XOR temporary inversion) is active.
func (e *Editor) SnapRotationRadians(rad float64) float64 {
	if !e.rotationSnapActive() || math.IsNaN(rad) || math.IsInf(rad, 0) {
		return rad
	}
	stepRad := float64(e.EffectiveRotationSnapDeg()) * math.Pi / 180.0
	return snapStep64(rad, stepRad)
}

// SnapScaleFactor snaps s to multiples of EffectiveScaleSnapStep when scale snap (XOR temporary inversion) is active,
// avoiding collapse to zero.
func (e *Editor) SnapScaleFactor(s float64) float64 {
	if !e.scaleSnapActive() || math.IsNaN(s) || math.IsInf(s, 0) {
		return s
	}
	step := float64(e.EffectiveScaleSnapStep())
	snapped := snapStep64(s, step)
	if math.Abs(snapped) < minScale2D {
		if s < 0 {
			return -step
		}
		return step
	}
	return snapped
}

// snapGridOverlaySpaceAndStep returns the layer transform and effective grid step drawn by drawSnapGridOverlay.
func (e *Editor) snapGridOverlaySpaceAndStep() (math2d.Affine, float32, float32) {
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if e.layerIndex(e.ActiveLayer) >= 0 {
		space, px, py = e.layerSpaceAndParallax(e.ActiveLayer)
	}
	m := e.layerAffineFor(space, px, py)
	stepX, stepY := e.EffectiveGridStep()
	if !e.SnapToGrid && e.SnapToPixel {
		stepX, stepY = 1, 1
	}
	scale := strokeScale(m)
	if !isFiniteFloat32(scale) || scale <= 0 || !isFiniteFloat32(stepX) || !isFiniteFloat32(stepY) || !(stepX > 0 && stepY > 0) {
		return m, 0, 0
	}
	sx64, sy64, sc64 := float64(stepX), float64(stepY), float64(scale)
	minSpacing := float64(minSnapGridCanvasSpacing)
	for i := 0; i < 256 && sx64*sc64 < minSpacing; i++ {
		sx64 *= 2
		if math.IsInf(sx64, 0) || math.IsNaN(sx64) {
			return m, 0, 0
		}
	}
	for i := 0; i < 256 && sy64*sc64 < minSpacing; i++ {
		sy64 *= 2
		if math.IsInf(sy64, 0) || math.IsNaN(sy64) {
			return m, 0, 0
		}
	}
	fx, fy := float32(sx64), float32(sy64)
	if !isFiniteFloat32(fx) || !isFiniteFloat32(fy) || !(fx > 0 && fy > 0) {
		return m, 0, 0
	}
	return m, fx, fy
}

// drawSnapGridOverlay draws the optional 2D snap grid across the visible 2D canvas, following the active view transform.
func (e *Editor) drawSnapGridOverlay(d *core.Drawing) {
	if e == nil || d == nil || e.playing || e.ViewMode != ViewMode2D || !e.ShowGrid {
		return
	}
	cw, ch := e.CanvasSize()
	if !(cw > 0 && ch > 0) || !isFiniteFloat32(cw) || !isFiniteFloat32(ch) {
		return
	}
	m, stepX32, stepY32 := e.snapGridOverlaySpaceAndStep()
	inv, ok := m.Invert()
	if !ok || !(stepX32 > 0 && stepY32 > 0) || !isFiniteFloat32(stepX32) || !isFiniteFloat32(stepY32) {
		return
	}
	scale := strokeScale(m)
	if !isFiniteFloat32(scale) || scale <= 0 {
		return
	}
	lw := 1 / scale
	if !isFiniteFloat32(lw) || lw <= 0 {
		return
	}
	corners := [4]math2d.Vec2{
		inv.TransformPoint(math2d.Vec2{X: 0, Y: 0}),
		inv.TransformPoint(math2d.Vec2{X: cw, Y: 0}),
		inv.TransformPoint(math2d.Vec2{X: cw, Y: ch}),
		inv.TransformPoint(math2d.Vec2{X: 0, Y: ch}),
	}
	for _, c := range corners {
		if !isFiniteVec(c) {
			return
		}
	}
	minX, maxX := corners[0].X, corners[0].X
	minY, maxY := corners[0].Y, corners[0].Y
	for _, c := range corners[1:] {
		minX, maxX = min(minX, c.X), max(maxX, c.X)
		minY, maxY = min(minY, c.Y), max(maxY, c.Y)
	}

	stepX := float64(stepX32)
	stepY := float64(stepY32)
	spanX := float64(maxX) - float64(minX)
	spanY := float64(maxY) - float64(minY)
	if math.IsNaN(spanX) || math.IsInf(spanX, 0) || math.IsNaN(spanY) || math.IsInf(spanY, 0) {
		return
	}
	for i := 0; i < 256 && spanX/stepX > float64(maxSnapGridLinesPerAxis); i++ {
		stepX *= 2
		if math.IsInf(stepX, 0) || math.IsNaN(stepX) {
			return
		}
	}
	for i := 0; i < 256 && spanY/stepY > float64(maxSnapGridLinesPerAxis); i++ {
		stepY *= 2
		if math.IsInf(stepY, 0) || math.IsNaN(stepY) {
			return
		}
	}
	startColF := math.Floor(float64(minX) / stepX)
	endColF := math.Ceil(float64(maxX) / stepX)
	startRowF := math.Floor(float64(minY) / stepY)
	endRowF := math.Ceil(float64(maxY) / stepY)
	const maxGridIndex = 1e15
	if math.IsNaN(startColF) || math.IsInf(startColF, 0) || math.IsNaN(endColF) || math.IsInf(endColF, 0) ||
		math.IsNaN(startRowF) || math.IsInf(startRowF, 0) || math.IsNaN(endRowF) || math.IsInf(endRowF, 0) ||
		math.Abs(startColF) > maxGridIndex || math.Abs(endColF) > maxGridIndex ||
		math.Abs(startRowF) > maxGridIndex || math.Abs(endRowF) > maxGridIndex ||
		endColF-startColF > float64(maxSnapGridLinesPerAxis)+2 ||
		endRowF-startRowF > float64(maxSnapGridLinesPerAxis)+2 {
		return
	}
	startCol := int(startColF)
	endCol := int(endColF)
	startRow := int(startRowF)
	endRow := int(endRowF)

	spec := e.Palette(nil)
	d.Save()
	applyAffine(d, m)
	d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.20)).SetLineWidth(lw).BeginPath()
	for c := startCol; c <= endCol; c++ {
		if c == 0 {
			continue
		}
		x := float32(float64(c) * stepX)
		if !isFiniteFloat32(x) {
			continue
		}
		d.MoveTo(x, minY).LineTo(x, maxY)
	}
	for r := startRow; r <= endRow; r++ {
		if r == 0 {
			continue
		}
		y := float32(float64(r) * stepY)
		if !isFiniteFloat32(y) {
			continue
		}
		d.MoveTo(minX, y).LineTo(maxX, y)
	}
	d.Stroke()

	// Emphasize the origin axes (x=0, y=0) when visible.
	if startCol <= 0 && endCol >= 0 || startRow <= 0 && endRow >= 0 {
		d.SetStrokeColor(ui.Alpha(spec.TextFaint, 0.42)).SetLineWidth(lw).BeginPath()
		if startCol <= 0 && endCol >= 0 {
			d.MoveTo(0, minY).LineTo(0, maxY)
		}
		if startRow <= 0 && endRow >= 0 {
			d.MoveTo(minX, 0).LineTo(maxX, 0)
		}
		d.Stroke()
	}
	d.Restore()
}

// SnapSettingsPopoverOpen reports whether the viewport Snap Settings popover is currently open.
func (e *Editor) SnapSettingsPopoverOpen() bool {
	return e != nil && !e.playing && e.snapSettingsPopoverOpen
}

// SetSnapSettingsPopoverOpen opens or closes the viewport Snap Settings popover.
func (e *Editor) SetSnapSettingsPopoverOpen(open bool) {
	if e == nil {
		return
	}
	if e.playing {
		open = false
	}
	if e.snapSettingsPopoverOpen == open {
		return
	}
	e.snapSettingsPopoverOpen = open
	e.notify()
}

// ToggleSnapSettingsPopover toggles the viewport Snap Settings popover open or closed.
func (e *Editor) ToggleSnapSettingsPopover() {
	if e == nil {
		return
	}
	e.SetSnapSettingsPopoverOpen(!e.SnapSettingsPopoverOpen())
}

// snapSettingsPopover renders a compact icon trigger button that opens a popover with all snapping controls
// (grid overlay, grid snap & step with auto tile-size reset, pixel snap, rotation snap & step, scale snap & step).
func snapSettingsPopover(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID string, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	p := ed.Palette(cx)
	rowStyle := core.Style{
		Direction:      core.Row,
		AlignItems:     core.AlignCenter,
		JustifyContent: core.JustifySpaceBetween,
		Gap:            8,
	}
	return ui.Popover{
		TestID: testID,
		Label:  ed.T("toolbar.snapSettingsLabel"),
		Open: func() bool {
			_ = rev.Get()
			return ed.SnapSettingsPopoverOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			ed.SetSnapSettingsPopoverOpen(open)
		},
		Trigger: ui.Button{
			Icon:     lucide.Grid3x3,
			IconSize: 13,
			Side:     28,
			Label:    ed.T("toolbar.snapSettingsLabel"),
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Disabled: disabled,
			Color: func() core.Color {
				_ = rev.Get()
				if ed.SnapSettingsPopoverOpen() {
					return p.LineRaised
				}
				return core.Transparent
			},
			Tint: func() core.Color {
				_ = rev.Get()
				if ed.SnapSettingsPopoverOpen() {
					return p.TextPrimary
				}
				return p.TextMuted
			},
			Style: core.Style{
				CornerRadius: 6,
				FocusVisible: p.FocusRing(2),
			},
		},
		Placement: core.PlaceTop,
		Align:     core.AlignCenter,
		Style: core.Style{
			Width:        core.Pt(256),
			Padding:      core.All(10),
			Gap:          8,
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("snap.title"),
				Style: core.Style{
					FontSize:   12,
					FontWeight: 700,
					Color:      colors.Text(),
				},
			},
			ui.Checkbox{
				TestID:   "snap-show-grid",
				Text:     ed.T("snap.showGrid"),
				Label:    ed.T("snap.showGrid"),
				Disabled: disabled,
				Checked: func() bool {
					_ = rev.Get()
					return ed.ShowGrid
				},
				OnChange: ed.SetShowGrid,
			},
			core.View{
				Style: rowStyle,
				Children: []core.Node{
					ui.Checkbox{
						TestID:   "snap-grid-toggle",
						Text:     ed.T("snap.grid"),
						Label:    ed.T("snap.grid"),
						Disabled: disabled,
						Checked: func() bool {
							_ = rev.Get()
							return ed.SnapToGrid
						},
						OnChange: ed.SetSnapToGrid,
					},
					ui.HStack{
						Gap: 4,
						Children: []core.Node{
							ui.NumberInput{
								TestID:   "snap-grid-step",
								Label:    ed.T("snap.gridStep"),
								Min:      0.5,
								Max:      1024,
								Step:     1,
								Disabled: disabled,
								Style:    core.Style{Width: core.Pt(78)},
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.EffectiveGridSize())
								},
								OnChange: func(v float64) {
									ed.SetGridSize(float32(v))
								},
							},
							ui.Button{
								TestID:  "snap-grid-auto",
								Text:    ed.T("snap.auto"),
								Label:   ed.T("snap.autoLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return disabled() || ed.GridSize == 0
								},
								OnPress: func() {
									ed.ResetGridSize()
								},
							},
						},
					},
				},
			},
			ui.Checkbox{
				TestID:   "snap-pixel-toggle",
				Text:     ed.T("snap.pixel"),
				Label:    ed.T("snap.pixel"),
				Disabled: disabled,
				Checked: func() bool {
					_ = rev.Get()
					return ed.SnapToPixel
				},
				OnChange: ed.SetSnapToPixel,
			},
			core.View{
				Style: rowStyle,
				Children: []core.Node{
					ui.Checkbox{
						TestID:   "snap-rotation-toggle",
						Text:     ed.T("snap.rotation"),
						Label:    ed.T("snap.rotation"),
						Disabled: disabled,
						Checked: func() bool {
							_ = rev.Get()
							return ed.SnapRotation
						},
						OnChange: ed.SetSnapRotation,
					},
					ui.NumberInput{
						TestID:   "snap-rotation-step",
						Label:    ed.T("snap.rotationStep"),
						Min:      1,
						Max:      180,
						Step:     5,
						Disabled: disabled,
						Style:    core.Style{Width: core.Pt(78)},
						Value: func() float64 {
							_ = rev.Get()
							return float64(ed.EffectiveRotationSnapDeg())
						},
						OnChange: func(v float64) {
							ed.SetRotationSnapDeg(float32(v))
						},
					},
				},
			},
			core.View{
				Style: rowStyle,
				Children: []core.Node{
					ui.Checkbox{
						TestID:   "snap-scale-toggle",
						Text:     ed.T("snap.scale"),
						Label:    ed.T("snap.scale"),
						Disabled: disabled,
						Checked: func() bool {
							_ = rev.Get()
							return ed.SnapScale
						},
						OnChange: ed.SetSnapScale,
					},
					ui.NumberInput{
						TestID:   "snap-scale-step",
						Label:    ed.T("snap.scaleStep"),
						Min:      0.05,
						Max:      10,
						Step:     0.05,
						Disabled: disabled,
						Style:    core.Style{Width: core.Pt(78)},
						Value: func() float64 {
							_ = rev.Get()
							return float64(ed.EffectiveScaleSnapStep())
						},
						OnChange: func(v float64) {
							ed.SetScaleSnapStep(float32(v))
						},
					},
				},
			},
			core.Text{
				Content: ed.T("snap.modifierHint"),
				Style: core.Style{
					FontSize: 10,
					Color:    colors.Muted(),
				},
			},
		},
	}
}

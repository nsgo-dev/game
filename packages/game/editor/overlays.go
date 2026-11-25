package editor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Built-in 2D viewport overlay IDs, shared between the editor's Overlays popover,
// EditorSettings.ViewportOverlays, and editormcp's viewport_image tool.
const (
	// OverlayCollisionShapes draws registered non-trigger component shape fields and 2D physics colliders.
	OverlayCollisionShapes = "collision_shapes"
	// OverlayTriggers draws trigger/sensor/area component shape fields and 2D physics trigger volumes.
	OverlayTriggers = "triggers"
	// OverlayConnections draws curved arrows labelled "event → handler" between connected objects.
	OverlayConnections = "connections"
	// OverlayPatrolPaths draws enemy/actor patrol ranges and waypoint paths.
	OverlayPatrolPaths = "patrol_paths"
	// OverlayNavigation draws the built 2D navigation mesh polygons, portals, and navregion2d outlines.
	OverlayNavigation = "navigation"
	// OverlayCameraBounds draws the scene camera bounds rectangle and its size caption.
	OverlayCameraBounds = "camera_bounds"
	// OverlayLabels draws object name/ID labels below visible objects.
	OverlayLabels = "labels"
)

// ViewportOverlaySwatch selects the visual legend swatch shown beside an overlay row in the Overlays popover.
type ViewportOverlaySwatch int

const (
	// OverlaySwatchBox draws a solid rectangular outline swatch.
	OverlaySwatchBox ViewportOverlaySwatch = iota
	// OverlaySwatchDashedBox draws a dotted/dashed rectangular outline with subtle tinted fill.
	OverlaySwatchDashedBox
	// OverlaySwatchLine draws a solid horizontal line swatch.
	OverlaySwatchLine
	// OverlaySwatchDashedLine draws a dotted/dashed horizontal line swatch.
	OverlaySwatchDashedLine
	// OverlaySwatchRegion draws a shaded region swatch with a top edge line.
	OverlaySwatchRegion
	// OverlaySwatchLabel draws an underline bar representing text labels.
	OverlaySwatchLabel
)

// ViewportOverlay describes a toggleable 2D viewport overlay drawn in both the live editor viewport
// and the editor MCP server's viewport_image tool. Plugins can register custom overlays via
// Plugin.Overlays or Editor.RegisterViewportOverlay.
type ViewportOverlay struct {
	// ID uniquely identifies the overlay in editor settings and editormcp's viewport_overlays list.
	// It must be non-empty with no spaces or control characters.
	ID string
	// Name is the human-readable label shown in the Overlays popover (for example "Patrol paths").
	Name string
	// NameKey is an optional localization key resolved in the editor's or owning plugin's catalog.
	NameKey string
	// DefaultOn sets whether the overlay starts enabled when EditorSettings.ViewportOverlays is nil.
	DefaultOn bool
	// Color is the swatch color (from Spec tokens for built-in overlays).
	Color core.Color
	// Swatch selects the swatch style rendered beside Name in the Overlays popover.
	Swatch ViewportOverlaySwatch
	// Count optionally returns the live item count in the current scene for the Overlays popover.
	Count func(ed *Editor) int
	// CountUnit optionally formats a count with a unit noun (for example "5 shapes" or "2 regions").
	CountUnit func(ed *Editor, n int) string
	// Draw renders the overlay onto d in canvas coordinates (using Editor.WorldToCanvas or Editor.layerAffineFor).
	Draw func(ed *Editor, d *core.Drawing, scene *sceneformat.BuiltScene)

	// owner names the plugin that registered this overlay, empty for built-in or directly registered overlays.
	owner *Plugin
}

// DefaultViewportOverlays returns the canonical sorted list of viewport overlays enabled by default.
func DefaultViewportOverlays() []string {
	return []string{
		OverlayCameraBounds,
		OverlayCollisionShapes,
		OverlayNavigation,
		OverlayTriggers,
	}
}

func validateOverlayID(id string) error {
	if id == "" {
		return fmt.Errorf("editor: overlay has no ID")
	}
	if strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("editor: overlay ID %q must have no spaces or control characters", id)
	}
	return nil
}

func validateViewportOverlay(o *ViewportOverlay, where string) error {
	if err := validateOverlayID(o.ID); err != nil {
		if where != "" {
			return fmt.Errorf("%s: %w", where, err)
		}
		return err
	}
	if strings.TrimSpace(o.Name) == "" && strings.TrimSpace(o.NameKey) == "" {
		if where != "" {
			return fmt.Errorf("%s: overlay %q has no name", where, o.ID)
		}
		return fmt.Errorf("editor: overlay %q has no name", o.ID)
	}
	if o.Draw == nil {
		if where != "" {
			return fmt.Errorf("%s: overlay %q has no Draw function", where, o.ID)
		}
		return fmt.Errorf("editor: overlay %q has no Draw function", o.ID)
	}
	return nil
}

// initBuiltinOverlays installs the 7 built-in viewport overlays, including Patrol paths registered
// through the same ViewportOverlay registry that plugins use.
func (e *Editor) initBuiltinOverlays() {
	if len(e.builtinOverlays) > 0 {
		return
	}
	e.builtinOverlays = []ViewportOverlay{
		{
			ID:        OverlayCollisionShapes,
			Name:      "Collision shapes",
			NameKey:   "overlays.collisionShapes",
			DefaultOn: true,
			Color:     specTextSecondary,
			Swatch:    OverlaySwatchBox,
			Count:     countCollisionShapesOverlay,
			CountUnit: func(ed *Editor, n int) string {
				if n == 0 {
					return "0"
				}
				return ed.Plural("overlays.count.shapes", n)
			},
			Draw: drawCollisionShapesOverlay,
		},
		{
			ID:        OverlayTriggers,
			Name:      "Triggers",
			NameKey:   "overlays.triggers",
			DefaultOn: true,
			Color:     specWarning,
			Swatch:    OverlaySwatchDashedBox,
			Count:     countTriggersOverlay,
			Draw:      drawTriggersOverlay,
		},
		{
			ID:        OverlayConnections,
			Name:      "Connections",
			NameKey:   "overlays.connections",
			DefaultOn: false,
			Color:     specAccent,
			Swatch:    OverlaySwatchLine,
			Count:     countConnectionsOverlay,
			Draw:      drawConnectionsOverlay,
		},
		{
			ID:        OverlayPatrolPaths,
			Name:      "Patrol paths",
			NameKey:   "overlays.patrolPaths",
			DefaultOn: false,
			Color:     specTextSecondary,
			Swatch:    OverlaySwatchDashedLine,
			Count:     countPatrolPathsOverlay,
			Draw:      drawPatrolPathsOverlay,
		},
		{
			ID:        OverlayNavigation,
			Name:      "Navigation",
			NameKey:   "overlays.navigation",
			DefaultOn: true,
			Color:     specTextSecondary,
			Swatch:    OverlaySwatchRegion,
			Count:     countNavigationOverlay,
			CountUnit: func(ed *Editor, n int) string {
				if n == 0 {
					return "0"
				}
				return ed.Plural("overlays.count.regions", n)
			},
			Draw: drawNavigationOverlay,
		},
		{
			ID:        OverlayCameraBounds,
			Name:      "Camera bounds",
			NameKey:   "overlays.cameraBounds",
			DefaultOn: true,
			Color:     specTextMuted,
			Swatch:    OverlaySwatchDashedBox,
			Count:     countCameraBoundsOverlay,
			Draw:      drawCameraBoundsOverlay,
		},
		{
			ID:        OverlayLabels,
			Name:      "Labels",
			NameKey:   "overlays.labels",
			DefaultOn: false,
			Color:     specTextSecondary,
			Swatch:    OverlaySwatchLabel,
			Count:     countLabelsOverlay,
			Draw:      drawLabelsOverlay,
		},
	}
}

// RegisterViewportOverlay registers a custom viewport overlay directly on the editor.
// It refuses an empty or invalid ID, missing name or Draw function, or an ID that clashes with
// a built-in or previously registered overlay.
func (e *Editor) RegisterViewportOverlay(o ViewportOverlay) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
	e.initBuiltinOverlays()
	if err := validateViewportOverlay(&o, ""); err != nil {
		return err
	}
	if e.hasRegisteredOverlayID(o.ID) {
		return fmt.Errorf("editor: overlay %q is already registered", o.ID)
	}
	if o.Color == 0 {
		o.Color = specTextSecondary
	}
	o.owner = nil
	e.customOverlays = append(e.customOverlays, o)
	e.notify()
	return nil
}

func (e *Editor) hasRegisteredOverlayID(id string) bool {
	for i := range e.builtinOverlays {
		if e.builtinOverlays[i].ID == id {
			return true
		}
	}
	for i := range e.customOverlays {
		if e.customOverlays[i].ID == id {
			return true
		}
	}
	for _, p := range e.plugins {
		for i := range p.Overlays {
			if p.Overlays[i].ID == id {
				return true
			}
		}
	}
	return false
}

// ViewportOverlays returns all currently available viewport overlays in display order:
// the built-in overlays first, followed by directly registered custom overlays and overlays
// contributed by active plugins.
func (e *Editor) ViewportOverlays() []ViewportOverlay {
	if e == nil {
		return nil
	}
	e.initBuiltinOverlays()
	out := make([]ViewportOverlay, 0, len(e.builtinOverlays)+len(e.customOverlays))
	out = append(out, e.builtinOverlays...)
	out = append(out, e.customOverlays...)
	for _, p := range e.activePlugins() {
		for _, o := range p.Overlays {
			o.owner = p
			if o.Color == 0 {
				o.Color = specTextSecondary
			}
			out = append(out, o)
		}
	}
	return out
}

func (e *Editor) findViewportOverlay(id string) (ViewportOverlay, bool) {
	for _, o := range e.ViewportOverlays() {
		if o.ID == id {
			return o, true
		}
	}
	return ViewportOverlay{}, false
}

func (e *Editor) overlayDisplayName(o ViewportOverlay) string {
	if o.owner != nil {
		return e.translatePluginText(o.owner, o.NameKey, o.Name)
	}
	if o.NameKey != "" && e.Catalog().Has(e.ResolvedLanguage(), o.NameKey) {
		return e.T(o.NameKey)
	}
	if o.Name != "" {
		return o.Name
	}
	return o.ID
}

// effectiveOverlaySet returns the map of enabled overlay IDs from EditorSettings (or defaults when unset).
func (e *Editor) effectiveOverlaySet() map[string]bool {
	set := make(map[string]bool)
	if e == nil {
		return set
	}
	if e.settings.ViewportOverlays != nil {
		for _, id := range *e.settings.ViewportOverlays {
			set[id] = true
		}
		return set
	}
	for _, o := range e.ViewportOverlays() {
		if o.DefaultOn {
			set[o.ID] = true
		}
	}
	return set
}

// overlayActive reports whether overlay id should draw during the current viewport draw pass,
// respecting any temporary DrawViewportWithOverlays override.
func (e *Editor) overlayActive(id string) bool {
	if e == nil {
		return false
	}
	if e.drawOverlayOverride != nil {
		return e.drawOverlayOverride[id]
	}
	if id == OverlayNavigation {
		return e.ShowNavMesh2D
	}
	return e.effectiveOverlaySet()[id]
}

// ViewportOverlayEnabled reports whether overlay id is currently enabled in the editor.
func (e *Editor) ViewportOverlayEnabled(id string) bool {
	if e == nil {
		return false
	}
	if id == OverlayNavigation {
		return e.ShowNavMesh2D
	}
	return e.effectiveOverlaySet()[id]
}

// ActiveViewportOverlays returns the sorted IDs of all available viewport overlays currently enabled.
func (e *Editor) ActiveViewportOverlays() []string {
	if e == nil {
		return nil
	}
	var out []string
	for _, o := range e.ViewportOverlays() {
		if e.ViewportOverlayEnabled(o.ID) {
			out = append(out, o.ID)
		}
	}
	slices.Sort(out)
	return out
}

// ActiveViewportOverlayCount returns how many available viewport overlays are currently turned on.
func (e *Editor) ActiveViewportOverlayCount() int {
	if e == nil {
		return 0
	}
	n := 0
	for _, o := range e.ViewportOverlays() {
		if e.ViewportOverlayEnabled(o.ID) {
			n++
		}
	}
	return n
}

// ViewportOverlayCount returns the live item count in the current scene for overlay id, or 0 if unknown or uncounted.
func (e *Editor) ViewportOverlayCount(id string) int {
	if e == nil {
		return 0
	}
	o, ok := e.findViewportOverlay(id)
	if !ok || o.Count == nil {
		return 0
	}
	return max(0, o.Count(e))
}

// SetViewportOverlay turns overlay id on or off and persists the updated per-user overlay set to
// SettingsPath when configured. It never modifies any project or scene file.
func (e *Editor) SetViewportOverlay(id string, on bool) error {
	if e == nil {
		return nil
	}
	if _, ok := e.findViewportOverlay(id); !ok {
		return fmt.Errorf("editor: unknown viewport overlay %q (available: %s)", id, strings.Join(e.availableOverlayIDs(), ", "))
	}
	if e.ViewportOverlayEnabled(id) == on {
		return nil
	}
	set := e.effectiveOverlaySet()
	if on {
		set[id] = true
	} else {
		delete(set, id)
	}
	return e.commitOverlaySet(set)
}

// ToggleViewportOverlay flips overlay id between on and off and persists to SettingsPath when set.
func (e *Editor) ToggleViewportOverlay(id string) error {
	if e == nil {
		return nil
	}
	return e.SetViewportOverlay(id, !e.ViewportOverlayEnabled(id))
}

// SetViewportOverlays replaces the enabled viewport overlay set with ids and persists to SettingsPath when set.
// Unknown overlay IDs are refused without changing the current set.
func (e *Editor) SetViewportOverlays(ids []string) error {
	if e == nil {
		return nil
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := e.findViewportOverlay(id); !ok {
			return fmt.Errorf("editor: unknown viewport overlay %q (available: %s)", id, strings.Join(e.availableOverlayIDs(), ", "))
		}
		set[id] = true
	}
	return e.commitOverlaySet(set)
}

func (e *Editor) commitOverlaySet(set map[string]bool) error {
	list := make([]string, 0, len(set))
	for id, on := range set {
		if on {
			list = append(list, id)
		}
	}
	slices.Sort(list)
	cur := e.Settings()
	cur.ViewportOverlays = &list
	e.settings = cur
	e.ShowNavMesh2D = set[OverlayNavigation]
	if e.SettingsPath != "" {
		if err := SaveEditorSettings(e.SettingsPath, &cur); err != nil {
			e.notify()
			return err
		}
	}
	e.notify()
	return nil
}

func (e *Editor) syncOverlaysFromSettings() {
	if e == nil {
		return
	}
	set := e.effectiveOverlaySet()
	e.ShowNavMesh2D = set[OverlayNavigation]
}

func (e *Editor) availableOverlayIDs() []string {
	ovs := e.ViewportOverlays()
	ids := make([]string, len(ovs))
	for i, o := range ovs {
		ids[i] = o.ID
	}
	return ids
}

// OverlaysPopoverOpen reports whether the viewport Overlays popover is currently open.
func (e *Editor) OverlaysPopoverOpen() bool {
	return e != nil && e.overlaysPopoverOpen
}

// SetOverlaysPopoverOpen opens or closes the viewport Overlays popover.
func (e *Editor) SetOverlaysPopoverOpen(open bool) {
	if e == nil || e.overlaysPopoverOpen == open {
		return
	}
	e.overlaysPopoverOpen = open
	e.notify()
}

// ToggleOverlaysPopover toggles the viewport Overlays popover open or closed.
func (e *Editor) ToggleOverlaysPopover() {
	if e == nil {
		return
	}
	e.SetOverlaysPopoverOpen(!e.overlaysPopoverOpen)
}

// DrawViewportWithOverlays renders the viewport like DrawViewport, temporarily using overlayIDs
// as the active overlay set without changing the editor's persisted overlay settings.
// When overlayIDs is nil, the editor's active overlay set is used; an explicit empty slice disables
// all viewport overlays for this draw call. Unknown overlay IDs return an error.
func (e *Editor) DrawViewportWithOverlays(d *core.Drawing, size core.Size, overlayIDs []string) error {
	if e == nil || d == nil {
		return nil
	}
	if overlayIDs == nil {
		e.DrawViewport(d, size)
		return nil
	}
	override := make(map[string]bool, len(overlayIDs))
	for _, id := range overlayIDs {
		if _, ok := e.findViewportOverlay(id); !ok {
			return fmt.Errorf("editor: unknown viewport overlay %q (available: %s)", id, strings.Join(e.availableOverlayIDs(), ", "))
		}
		override[id] = true
	}
	prev := e.drawOverlayOverride
	e.drawOverlayOverride = override
	defer func() { e.drawOverlayOverride = prev }()
	e.DrawViewport(d, size)
	return nil
}

// drawViewportOverlays runs all active custom/built-in viewport overlays in canonical order.
func (e *Editor) drawViewportOverlays(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing {
		return
	}
	for _, o := range e.ViewportOverlays() {
		if o.Draw == nil {
			continue
		}
		// Navigation and camera bounds are also integrated into their own pass order,
		// so Draw handles each overlay cleanly in one unified sequence.
		if !e.overlayActive(o.ID) {
			continue
		}
		d.Save()
		o.Draw(e, d, scene)
		d.Restore()
	}
}

// --- Built-in overlay counters and renderers ---

func isTriggerComponentName(compName string) bool {
	lc := strings.ToLower(compName)
	return strings.Contains(lc, "trigger") || strings.Contains(lc, "sensor") || strings.Contains(lc, "area")
}

func isPatrolComponentName(compName string) bool {
	lc := strings.ToLower(compName)
	return strings.Contains(lc, "patrol") || strings.Contains(lc, "waypoint")
}

func (e *Editor) collectAllComponentShapes(scene *sceneformat.BuiltScene, wantTriggers bool) []objectShapeEntry {
	if e == nil || scene == nil || e.ViewMode != ViewMode2D {
		return nil
	}
	var out []objectShapeEntry
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil && len(bo.Doc.Components) > 0 {
					m := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY).Mul(bo.World)
					for _, compName := range sortedMapKeys(bo.Doc.Components) {
						if isPatrolComponentName(compName) || isTriggerComponentName(compName) != wantTriggers {
							continue
						}
						ct, ok := e.lookupComponentType(compName)
						if !ok {
							continue
						}
						for _, f := range ct.Fields {
							if !f.Kind.IsShape() {
								continue
							}
							val, ok := e.shapeFieldValue(bo.Doc, compName, f)
							if !ok {
								continue
							}
							out = append(out, objectShapeEntry{
								bo:        bo,
								space:     layer.Space,
								toCanvas:  m,
								component: compName,
								field:     f,
								value:     val,
								selected:  bo.ID == e.SelectedID,
							})
						}
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	return out
}

func (e *Editor) countPhysicsShapes2D(scene *sceneformat.BuiltScene, wantTriggers bool) int {
	if e == nil || scene == nil {
		return 0
	}
	n := 0
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil {
					for _, item := range sceneformat.ExtractPhysicsDebug2D(bo.Doc, &bo.World) {
						if !item.Shape.Empty() && item.IsTrigger == wantTriggers {
							n++
						}
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	return n
}

func countCollisionShapesOverlay(e *Editor) int {
	scene := e.ActiveScene()
	if scene == nil {
		return 0
	}
	return len(e.collectAllComponentShapes(scene, false)) + e.countPhysicsShapes2D(scene, false)
}

func countTriggersOverlay(e *Editor) int {
	scene := e.ActiveScene()
	if scene == nil {
		return 0
	}
	return len(e.collectAllComponentShapes(scene, true)) + e.countPhysicsShapes2D(scene, true)
}

func drawShapeEntriesOverlay(d *core.Drawing, entries []objectShapeEntry, strokeColor, fillColor core.Color, dashed bool) {
	if len(entries) == 0 {
		return
	}
	d.Save()
	for _, ent := range entries {
		var pts []math2d.Vec2
		switch v := ent.value.(type) {
		case sceneformat.Point:
			pts = []math2d.Vec2{ent.toCanvas.TransformPoint(math2d.Vec2{X: float32(v.X), Y: float32(v.Y)})}
		case sceneformat.Rect:
			x0, y0 := float32(v.X), float32(v.Y)
			x1, y1 := float32(v.X+v.Width), float32(v.Y+v.Height)
			pts = []math2d.Vec2{
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x0, Y: y0}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x1, Y: y0}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x1, Y: y1}),
				ent.toCanvas.TransformPoint(math2d.Vec2{X: x0, Y: y1}),
			}
		case sceneformat.Circle:
			pts = circleCanvasPoints(ent.toCanvas, v)
		case []sceneformat.Point:
			pts = make([]math2d.Vec2, len(v))
			for i, p := range v {
				pts[i] = ent.toCanvas.TransformPoint(math2d.Vec2{X: float32(p.X), Y: float32(p.Y)})
			}
		}
		lw := float32(1.25)
		if ent.selected {
			lw = 1.75
		}
		d.SetLineWidth(lw)
		if dashed {
			d.SetLineDash(3, 3)
		} else {
			d.SetLineDash()
		}
		switch ent.field.Kind {
		case sceneformat.FieldPoint:
			if len(pts) == 1 {
				p := pts[0]
				const arm float32 = 7
				d.SetStrokeColor(strokeColor)
				d.BeginPath().MoveTo(p.X-arm, p.Y).LineTo(p.X+arm, p.Y).MoveTo(p.X, p.Y-arm).LineTo(p.X, p.Y+arm).Stroke()
				d.SetFillColor(fillColor)
				d.BeginPath().Arc(p.X, p.Y, 3.5, 0, float32(2*math.Pi), false).Fill().Stroke()
			}
		case sceneformat.FieldRect, sceneformat.FieldCircle, sceneformat.FieldPolygon:
			if len(pts) == 1 {
				p := pts[0]
				d.SetFillColor(strokeColor)
				d.BeginPath().Arc(p.X, p.Y, 3, 0, float32(2*math.Pi), false).Fill()
			} else if len(pts) >= 2 {
				fillAndStrokePolygon(d, pts, fillColor, strokeColor)
			}
		}
	}
	d.SetLineDash()
	d.Restore()
}

func drawCollisionShapesOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	entries := e.collectAllComponentShapes(scene, false)
	drawShapeEntriesOverlay(d, entries, core.RGBA(0xB4BAC6, 0.85), core.RGBA(0xB4BAC6, 0.08), false)
}

func drawTriggersOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	entries := e.collectAllComponentShapes(scene, true)
	drawShapeEntriesOverlay(d, entries, specWarning, core.RGBA(0xF2C14E, 0.08), true)
}

// --- Connections overlay ---

func (e *Editor) sceneConnections(scene *sceneformat.BuiltScene) []sceneformat.ConnectionDoc {
	if scene != nil && scene.Doc != nil && len(scene.Doc.Connections) > 0 {
		return scene.Doc.Connections
	}
	if e != nil && e.Doc != nil && len(e.Doc.Connections) > 0 {
		return e.Doc.Connections
	}
	return nil
}

func countConnectionsOverlay(e *Editor) int {
	scene := e.ActiveScene()
	return len(e.sceneConnections(scene))
}

func (e *Editor) objectCanvasCenter(scene *sceneformat.BuiltScene, id string) (math2d.Vec2, bool) {
	if scene == nil || id == "" {
		return math2d.Vec2{}, false
	}
	bo := scene.Object(id)
	if bo == nil || bo.Doc == nil || viewFlagged(e.hidden, bo) {
		return math2d.Vec2{}, false
	}
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return math2d.Vec2{}, false
		}
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	m := e.layerAffineFor(space, px, py).Mul(bo.World)
	rx, ry, rw, rh := objectOutline(bo)
	return m.TransformPoint(math2d.Vec2{X: rx + rw*0.5, Y: ry + rh*0.5}), true
}

func drawConnectionsOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	conns := e.sceneConnections(scene)
	if len(conns) == 0 {
		return
	}
	// Track duplicate (from, to) pairs so stacked arrows fan out cleanly without overlapping pills.
	type pairKey struct{ a, b string }
	pairSeen := make(map[pairKey]int, len(conns))

	font := core.Font{Family: core.FontMonospace, Size: 10.5}
	for _, c := range conns {
		start, okFrom := e.objectCanvasCenter(scene, c.From)
		end, okTo := e.objectCanvasCenter(scene, c.To)
		if !okFrom || !okTo || !isFiniteVec(start) || !isFiniteVec(end) {
			continue
		}
		key := pairKey{c.From, c.To}
		if key.a > key.b {
			key.a, key.b = key.b, key.a
		}
		stackIdx := pairSeen[key]
		pairSeen[key] = stackIdx + 1

		var c1, c2, mid math2d.Vec2
		var dir math2d.Vec2
		if c.From == c.To || vecLen(end.Sub(start)) < 4 {
			// Self-connection: draw a neat arch above the object.
			lift := float32(36 + stackIdx*22)
			c1 = math2d.Vec2{X: start.X - 28, Y: start.Y - lift}
			c2 = math2d.Vec2{X: start.X + 28, Y: start.Y - lift}
			end = math2d.Vec2{X: start.X + 8, Y: start.Y - 4}
			mid = cubicPoint(start, c1, c2, end, 0.5)
			dir = end.Sub(c2)
		} else {
			dist := vecLen(end.Sub(start))
			// Arch upward cleanly like docs/design/editor/Inspector.dc.html.
			lift := min(max(dist*0.32, 36), 120) + float32(stackIdx*24)
			dx := end.X - start.X
			dy := end.Y - start.Y
			c1 = math2d.Vec2{X: start.X + dx*0.25, Y: start.Y + dy*0.25 - lift}
			c2 = math2d.Vec2{X: start.X + dx*0.75, Y: start.Y + dy*0.75 - lift}
			mid = cubicPoint(start, c1, c2, end, 0.5)
			dir = end.Sub(c2)
		}

		// Curved connector shaft.
		d.SetStrokeColor(specAccent).SetLineWidth(1.5).SetLineDash()
		d.BeginPath().MoveTo(start.X, start.Y).CubicTo(c1.X, c1.Y, c2.X, c2.Y, end.X, end.Y).Stroke()

		// Source anchor dot (Arc rasterizes cleanly in both live canvas and editormcp RenderPNG).
		d.SetFillColor(specAccent)
		d.BeginPath().Arc(start.X, start.Y, 3, 0, float32(2*math.Pi), false).Fill()

		// Arrowhead at target end pointing along the end tangent.
		dl := vecLen(dir)
		if dl > 1e-4 {
			ux, uy := dir.X/dl, dir.Y/dl
			const headLen float32 = 8
			const headHalfW float32 = 4.5
			bx, by := end.X-ux*headLen, end.Y-uy*headLen
			px, py := -uy*headHalfW, ux*headHalfW
			d.SetStrokeColor(specAccent).SetLineWidth(1.5)
			d.BeginPath().MoveTo(bx+px, by+py).LineTo(end.X, end.Y).LineTo(bx-px, by-py).Stroke()
		}

		// Pill label: "event → handler" (e.g. "pulled → open").
		label := fmt.Sprintf("%s → %s", c.Event, c.Handler)
		charW := float32(6.5)
		pillW := float32(len([]rune(label)))*charW + 14
		const pillH float32 = 18
		px := mid.X - pillW*0.5
		py := mid.Y - pillH*0.5
		d.SetFillColor(specBgOverlay)
		d.BeginPath().Rect(px, py, pillW, pillH).Fill()
		d.SetStrokeColor(specAccentEdge).SetLineWidth(1)
		d.BeginPath().Rect(px, py, pillW, pillH).Stroke()
		d.SetFillColor(specText).FillText(label, px+7, py+12.5, font)
	}
}

func cubicPoint(p0, p1, p2, p3 math2d.Vec2, t float32) math2d.Vec2 {
	u := 1 - t
	tt := t * t
	uu := u * u
	uuu := uu * u
	ttt := tt * t
	return math2d.Vec2{
		X: uuu*p0.X + 3*uu*t*p1.X + 3*u*tt*p2.X + ttt*p3.X,
		Y: uuu*p0.Y + 3*uu*t*p1.Y + 3*u*tt*p2.Y + ttt*p3.Y,
	}
}

// --- Patrol paths overlay (built-in example using the ViewportOverlay registry) ---

type patrolPath2D struct {
	pts []math2d.Vec2
}

func (e *Editor) collectPatrolPaths(scene *sceneformat.BuiltScene) []patrolPath2D {
	if e == nil || scene == nil {
		return nil
	}
	var out []patrolPath2D
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		layerM := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil && len(bo.Doc.Components) > 0 {
					for _, compName := range sortedMapKeys(bo.Doc.Components) {
						lc := strings.ToLower(compName)
						if !strings.Contains(lc, "patrol") && !strings.Contains(lc, "waypoint") {
							continue
						}
						if pts := extractPatrolCanvasPoints(bo, bo.Doc.Components[compName], layerM); len(pts) >= 2 {
							out = append(out, patrolPath2D{pts: pts})
						}
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	return out
}

func extractPatrolCanvasPoints(bo *sceneformat.BuiltObject, raw any, layerM math2d.Affine) []math2d.Vec2 {
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return nil
	}
	// 1. Explicit waypoints/points/path list (in object-local space unless world: true).
	for _, key := range []string{"points", "waypoints", "path"} {
		v, exists := m[key]
		if !exists {
			continue
		}
		parsed, ok := parsePatrolWaypoints(v)
		if !ok || len(parsed) < 2 {
			continue
		}
		toCanvas := layerM.Mul(bo.World)
		if worldFlag, ok := m["world"].(bool); ok && worldFlag {
			toCanvas = layerM
		}
		pts := make([]math2d.Vec2, len(parsed))
		for i, p := range parsed {
			pts[i] = toCanvas.TransformPoint(math2d.Vec2{X: float32(p.X), Y: float32(p.Y)})
		}
		return pts
	}
	// 2. Horizontal range via from/to or range: [from, to] / {from, to}, matching Inspector.dc.html.
	from, okFrom := patrolNumber(m["from"])
	to, okTo := patrolNumber(m["to"])
	if !okFrom || !okTo {
		if rv, exists := m["range"]; exists {
			switch r := rv.(type) {
			case []any:
				if len(r) == 2 {
					from, okFrom = patrolNumber(r[0])
					to, okTo = patrolNumber(r[1])
				}
			case []float64:
				if len(r) == 2 {
					from, to, okFrom, okTo = r[0], r[1], true, true
				}
			case []int:
				if len(r) == 2 {
					from, to, okFrom, okTo = float64(r[0]), float64(r[1]), true, true
				}
			case map[string]any:
				from, okFrom = patrolNumber(r["from"])
				to, okTo = patrolNumber(r["to"])
				if !okFrom || !okTo {
					from, okFrom = patrolNumber(r["min"])
					to, okTo = patrolNumber(r["max"])
				}
			}
		}
	}
	if okFrom && okTo && from != to {
		worldPos := bo.World.TransformPoint(math2d.Vec2{})
		p0 := layerM.TransformPoint(math2d.Vec2{X: float32(from), Y: worldPos.Y})
		p1 := layerM.TransformPoint(math2d.Vec2{X: float32(to), Y: worldPos.Y})
		return []math2d.Vec2{p0, p1}
	}
	return nil
}

func parsePatrolWaypoints(v any) ([]sceneformat.Point, bool) {
	if pts, err := sceneformat.ParsePolygon(v); err == nil && len(pts) >= 2 {
		return pts, true
	}
	switch raw := v.(type) {
	case [][2]float64:
		if len(raw) < 2 {
			return nil, false
		}
		out := make([]sceneformat.Point, len(raw))
		for i, p := range raw {
			if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
				return nil, false
			}
			out[i] = sceneformat.Point{X: p[0], Y: p[1]}
		}
		return out, true
	case []any:
		if len(raw) < 2 {
			return nil, false
		}
		out := make([]sceneformat.Point, 0, len(raw))
		for _, elem := range raw {
			pair, ok := elem.([]any)
			if !ok || len(pair) != 2 {
				return nil, false
			}
			x, okX := patrolNumber(pair[0])
			y, okY := patrolNumber(pair[1])
			if !okX || !okY {
				return nil, false
			}
			out = append(out, sceneformat.Point{X: x, Y: y})
		}
		return out, true
	}
	return nil, false
}

func patrolNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		if isFiniteFloat32(n) {
			return float64(n), true
		}
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n, true
		}
	}
	return 0, false
}

func countPatrolPathsOverlay(e *Editor) int {
	return len(e.collectPatrolPaths(e.ActiveScene()))
}

func drawPatrolPathsOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	paths := e.collectPatrolPaths(scene)
	if len(paths) == 0 {
		return
	}
	for _, pp := range paths {
		if len(pp.pts) < 2 {
			continue
		}
		d.SetStrokeColor(specTextSecondary).SetLineWidth(1.5).SetLineDash(3, 3)
		d.BeginPath().MoveTo(pp.pts[0].X, pp.pts[0].Y)
		for _, p := range pp.pts[1:] {
			d.LineTo(p.X, p.Y)
		}
		d.Stroke()

		// Solid end ticks perpendicular to the start and end segments, plus subtle waypoint dots.
		d.SetLineDash()
		drawPatrolEndTick(d, pp.pts[0], pp.pts[1])
		drawPatrolEndTick(d, pp.pts[len(pp.pts)-1], pp.pts[len(pp.pts)-2])
		d.SetFillColor(specTextSecondary)
		for i := 1; i < len(pp.pts)-1; i++ {
			d.BeginPath().Arc(pp.pts[i].X, pp.pts[i].Y, 2.5, 0, float32(2*math.Pi), false).Fill()
		}
	}
}

func drawPatrolEndTick(d *core.Drawing, at, toward math2d.Vec2) {
	dir := toward.Sub(at)
	l := vecLen(dir)
	var nx, ny float32 = 0, 1
	if l > 1e-4 {
		nx, ny = -dir.Y/l, dir.X/l
	}
	const half float32 = 4.5
	d.SetStrokeColor(specTextSecondary).SetLineWidth(1.5)
	d.BeginPath().MoveTo(at.X-nx*half, at.Y-ny*half).LineTo(at.X+nx*half, at.Y+ny*half).Stroke()
}

// --- Navigation overlay ---

func countNavigationOverlay(e *Editor) int {
	scene := e.ActiveScene()
	if scene == nil {
		return 0
	}
	outlines := e.nav2D.outlinesFor(scene)
	n := 0
	for _, out := range outlines {
		if !viewFlagged(e.hidden, out.bo) {
			n++
		}
	}
	if n > 0 {
		return n
	}
	if scene.NavBake2D != nil {
		return len(scene.NavBake2D.Polygons)
	}
	return 0
}

func drawNavigationOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	// Drawn by drawNavMesh2DOverlay in DrawViewport to preserve z-order beneath objects/handles.
	_ = e
	_ = d
	_ = scene
}

// --- Camera bounds overlay ---

func countCameraBoundsOverlay(e *Editor) int {
	if e != nil && e.Doc != nil && e.Doc.Camera != nil && e.Doc.Camera.Bounds != nil {
		return 1
	}
	return 0
}

func drawCameraBoundsOverlay(e *Editor, d *core.Drawing, _ *sceneformat.BuiltScene) {
	if e == nil || e.Doc == nil || e.Doc.Camera == nil || e.Doc.Camera.Bounds == nil {
		return
	}
	font := core.Font{Family: core.FontMonospace, Size: 10.5}
	b := e.Doc.Camera.Bounds
	pts := worldRectCanvasCorners(e, b.X, b.Y, b.Width, b.Height)
	d.SetStrokeColor(specTextMuted).SetLineWidth(1.25).SetLineDash(3, 3)
	strokePolygon(d, pts)
	d.SetLineDash()
	caption := fmt.Sprintf("bounds %g × %g", b.Width, b.Height)
	br := pts[2]
	tw := float32(len([]rune(caption))) * 6.5
	d.SetFillColor(specTextMuted).FillText(caption, br.X-tw, br.Y+13, font)
}

// --- Labels overlay ---

func countLabelsOverlay(e *Editor) int {
	scene := e.ActiveScene()
	if scene == nil {
		return 0
	}
	n := 0
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil {
					n++
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	return n
}

func drawLabelsOverlay(e *Editor, d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil {
		return
	}
	font := core.Font{Family: core.FontMonospace, Size: 10.5}
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		layerM := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil {
					text := bo.Doc.Name
					if text == "" {
						text = bo.ID
					}
					if text != "" {
						m := layerM.Mul(bo.World)
						rx, ry, rw, rh := objectOutline(bo)
						cx := rx + rw*0.5
						bottomY := ry + rh
						anchor := m.TransformPoint(math2d.Vec2{X: cx, Y: bottomY})
						if isFiniteVec(anchor) {
							pillW := float32(len([]rune(text)))*6.5 + 10
							const pillH float32 = 16
							px := anchor.X - pillW*0.5
							py := anchor.Y + 4
							d.SetFillColor(specBgOverlay)
							d.BeginPath().Rect(px, py, pillW, pillH).Fill()
							d.SetStrokeColor(specLineRaised).SetLineWidth(1)
							d.BeginPath().Rect(px, py, pillW, pillH).Stroke()
							d.SetFillColor(specTextSecondary).FillText(text, px+5, py+11.5, font)
						}
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
}

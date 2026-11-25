package editor

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const (
	// defaultPasteOffset2D is the 2D offset applied when pasting or duplicating objects
	// into the same scene without a hovered viewport pointer, keeping copies visibly next to originals.
	defaultPasteOffset2D float32 = 16
	// defaultPasteOffset3D is the 3D X-axis offset applied when duplicating or pasting in 3D view
	// without a hovered viewport pointer.
	defaultPasteOffset3D float32 = 1
)

type clipboardKind int

const (
	clipboardNone clipboardKind = iota
	clipboardObjects
	clipboardTile
)

// PasteOptions configures where PasteWith and PasteYAMLWith place pasted objects.
type PasteOptions struct {
	// Layer names the target layer; empty uses ActiveLayer (or ParentID's layer when ParentID is set).
	Layer string
	// ParentID optionally names an existing object to paste under as children.
	ParentID string
	// At optionally places the pasted objects' 2D centroid at this layer-space point.
	At *[2]float32
	// At3D optionally places the pasted objects' 3D centroid at this world-space point.
	At3D *[3]float32
	// Offset optionally shifts every pasted root by (dx, dy) in 2D.
	Offset *[2]float32
	// Offset3D optionally shifts every pasted root by (dx, dy, dz) in 3D.
	Offset3D *[3]float32
}

// SelectedIDs returns the IDs of all currently selected objects in selection order,
// or nil when no object is selected. When SelectedID was set directly without SelectObjects,
// it returns a single-element slice holding SelectedID if that object exists in Doc or the resolved scene.
func (e *Editor) SelectedIDs() []string {
	if e == nil || e.Doc == nil || e.SelectedID == "" {
		return nil
	}
	if len(e.selectedIDs) > 0 && slices.Contains(e.selectedIDs, e.SelectedID) {
		var out []string
		for _, id := range e.selectedIDs {
			if id != "" && !slices.Contains(out, id) {
				if obj, _ := e.FindResolvedObject(id); obj != nil {
					out = append(out, id)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if obj, _ := e.FindResolvedObject(e.SelectedID); obj != nil {
		return []string{e.SelectedID}
	}
	return nil
}

// allSelectedObjectsAreLocal reports whether at least one object is selected and every
// selected object is defined directly in Editor.Doc (required by Copy/Cut/Delete/Duplicate/Group).
func (e *Editor) allSelectedObjectsAreLocal() bool {
	if e == nil || e.Doc == nil {
		return false
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if locateObject(e.Doc, id).obj == nil {
			return false
		}
	}
	return true
}

// selectedObjectIsLocal reports whether SelectedID exists directly in Editor.Doc.
func (e *Editor) selectedObjectIsLocal() bool {
	if e == nil || e.Doc == nil || e.SelectedID == "" {
		return false
	}
	return locateObject(e.Doc, e.SelectedID).obj != nil
}

// IsObjectSelected reports whether id is in the current object selection.
func (e *Editor) IsObjectSelected(id string) bool {
	if id == "" {
		return false
	}
	return slices.Contains(e.SelectedIDs(), id)
}

// SelectObjects selects all valid existing objects in ids (deduplicated, preserving order).
// The last valid ID becomes SelectedID. Passing an empty slice clears the selection.
func (e *Editor) SelectObjects(ids []string) bool {
	if e == nil || e.Doc == nil {
		return false
	}
	var valid []string
	for _, id := range ids {
		if id == "" || slices.Contains(valid, id) {
			continue
		}
		if obj, _ := e.FindResolvedObject(id); obj != nil {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return e.SelectObject("")
	}
	if len(valid) == 1 {
		return e.SelectObject(valid[0])
	}
	primary := valid[len(valid)-1]
	hadRoom := e.rooms != nil && e.rooms.selectedRoomID != ""
	same := e.SelectedID == primary && slices.Equal(e.selectedIDs, valid) && !e.inspectAssetFile && !e.inspectLayer && e.connPick == nil && !hadRoom
	if same {
		return true
	}
	e.CancelComponentGizmoDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.selectedIDs = valid
	e.SelectedID = primary
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	if e.rooms != nil {
		e.rooms.selectedRoomID = ""
	}
	e.Status = fmt.Sprintf("Selected %d objects", len(valid))
	e.notify()
	return true
}

// ToggleSelectObject adds id to the current object selection if absent, or removes it if present.
func (e *Editor) ToggleSelectObject(id string) bool {
	if e == nil || e.Doc == nil || id == "" {
		return false
	}
	if obj, _ := e.FindResolvedObject(id); obj == nil {
		return false
	}
	cur := e.SelectedIDs()
	if idx := slices.Index(cur, id); idx >= 0 {
		cur = slices.Delete(cur, idx, idx+1)
	} else {
		cur = append(cur, id)
	}
	return e.SelectObjects(cur)
}

// SetViewportPointer2D records the mouse pointer's canvas position over the 2D viewport
// so Paste can place copied objects at the pointer.
func (e *Editor) SetViewportPointer2D(canvasX, canvasY float32) {
	if e == nil || !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return
	}
	pos := math2d.Vec2{X: canvasX, Y: canvasY}
	if !e.hasPointer2D || e.pointer2D != pos {
		e.pasteStreak = 0
	}
	e.pointer2D = pos
	e.hasPointer2D = true
}

// SetViewportPointer3D records the mouse pointer's canvas position over the 3D viewport
// so Paste can place copied objects at the pointer in 3D view.
func (e *Editor) SetViewportPointer3D(canvasX, canvasY float32) {
	if e == nil || !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return
	}
	pos := [2]float32{canvasX, canvasY}
	if !e.hasPointer3D || e.pointer3D != pos {
		e.pasteStreak = 0
	}
	e.pointer3D = pos
	e.hasPointer3D = true
}

// ClearViewportPointer clears any recorded viewport pointer position.
func (e *Editor) ClearViewportPointer() {
	if e == nil {
		return
	}
	e.hasPointer2D = false
	e.hasPointer3D = false
}

func (e *Editor) cancelActiveObjectDrags() {
	e.CancelDrag2D()
	e.CancelMarquee2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelRoomDrag()
	e.CancelGizmoDrag3D()
}

func (e *Editor) writeClipboard(text string) {
	e.clipboardText = text
	e.lastClipboardKind = clipboardObjects
	e.lastPasteYAML = ""
	e.pasteStreak = 0
	if e.WriteClipboard != nil {
		e.WriteClipboard(text)
		return
	}
	if e.scope != nil {
		e.scope.SetClipboard(text)
	}
}

func (e *Editor) readClipboard(done func(text string)) {
	if e.ReadClipboard != nil {
		e.ReadClipboard(done)
		return
	}
	if e.scope != nil {
		e.scope.ReadClipboard(done)
		return
	}
	done(e.clipboardText)
}

// normalizeObjectSelection deduplicates ids, verifies every ID exists in e.Doc,
// drops any ID whose ancestor is also in ids, and returns the remaining root IDs
// in deterministic document order.
func (e *Editor) normalizeObjectSelection(ids []string) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no open scene")
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if locateObject(e.Doc, id).obj == nil {
			return nil, fmt.Errorf("editor: object %q not found", id)
		}
		want[id] = struct{}{}
	}
	if len(want) == 0 {
		return nil, nil
	}
	var ordered []string
	var walk func(objs []sceneformat.ObjectDoc, ancestorSelected bool)
	walk = func(objs []sceneformat.ObjectDoc, ancestorSelected bool) {
		for i := range objs {
			o := &objs[i]
			_, selected := want[o.ID]
			if selected && !ancestorSelected {
				ordered = append(ordered, o.ID)
			}
			walk(o.Children, ancestorSelected || selected)
		}
	}
	for li := range e.Doc.Layers {
		walk(e.Doc.Layers[li].Objects, false)
	}
	return ordered, nil
}

// buildClipboardSceneDoc builds a valid standalone sceneformat.SceneDoc containing the
// given normalized root object IDs, their descendants, the connections whose endpoints both
// belong to those subtrees, and any SceneDoc.Overrides targeting them when the open scene inherits.
func (e *Editor) buildClipboardSceneDoc(rootIDs []string) (*sceneformat.SceneDoc, error) {
	if len(rootIDs) == 0 {
		return nil, errors.New("editor: no objects selected")
	}
	layerName := "main"
	layerSpace := sceneformat.SpaceWorld
	if first := locateObject(e.Doc, rootIDs[0]); first.obj != nil && first.layer >= 0 {
		layerName = e.Doc.Layers[first.layer].Name
		layerSpace = e.Doc.Layers[first.layer].Space
	}
	roots := make([]sceneformat.ObjectDoc, 0, len(rootIDs))
	copiedIDs := make(map[string]struct{})
	instancing := make(map[string]struct{})
	for _, id := range rootIDs {
		loc := locateObject(e.Doc, id)
		if loc.obj == nil {
			return nil, fmt.Errorf("editor: object %q not found", id)
		}
		cloned := cloneObjectDoc(*loc.obj)
		sceneformat.WalkObjects([]sceneformat.ObjectDoc{cloned}, func(o, _ *sceneformat.ObjectDoc) bool {
			copiedIDs[o.ID] = struct{}{}
			if o.Scene != "" {
				instancing[o.ID] = struct{}{}
			}
			return true
		})
		roots = append(roots, cloned)
	}

	var conns []sceneformat.ConnectionDoc
	for _, conn := range e.Doc.Connections {
		if namesSubtree(conn.From, copiedIDs, instancing) && namesSubtree(conn.To, copiedIDs, instancing) {
			conns = append(conns, cloneConnectionDoc(conn))
		}
	}

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{
			{
				Name:    layerName,
				Space:   layerSpace,
				Objects: roots,
			},
		},
		Connections: conns,
	}

	if e.Doc.Inherits != "" && len(e.Doc.Overrides) > 0 {
		var overrides map[string]sceneformat.ObjectOverride
		for _, key := range slices.Sorted(maps.Keys(e.Doc.Overrides)) {
			if namesSubtree(key, copiedIDs, instancing) {
				if overrides == nil {
					overrides = make(map[string]sceneformat.ObjectOverride)
				}
				overrides[key] = cloneObjectOverride(e.Doc.Overrides[key])
			}
		}
		if len(overrides) > 0 {
			doc.Inherits = e.Doc.Inherits
			doc.Overrides = overrides
		}
	}

	return doc, nil
}

func cloneObjectOverride(ov sceneformat.ObjectOverride) sceneformat.ObjectOverride {
	doc := sceneformat.SceneDoc{
		Version:   sceneformat.CurrentVersion,
		Inherits:  "base.scene.yaml",
		Overrides: map[string]sceneformat.ObjectOverride{"_": ov},
	}
	return doc.Clone().Overrides["_"]
}

// CopyObjects encodes the objects named by ids (with their children, components, properties,
// and the connections among them) as valid scene-format YAML, writes it to the system clipboard,
// and returns the YAML string.
func (e *Editor) CopyObjects(ids ...string) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	e.cancelActiveObjectDrags()
	roots, err := e.normalizeObjectSelection(ids)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", nil
	}
	doc, err := e.buildClipboardSceneDoc(roots)
	if err != nil {
		return "", err
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		return "", err
	}
	text := string(data)
	e.writeClipboard(text)
	if len(roots) == 1 {
		e.Status = "Copied " + roots[0]
	} else {
		e.Status = fmt.Sprintf("Copied %d objects", len(roots))
	}
	e.notify()
	return text, nil
}

// CopySelected copies the currently selected object(s) to the system clipboard as scene-format YAML.
func (e *Editor) CopySelected() (string, error) {
	if e == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return "", nil
	}
	return e.CopyObjects(ids...)
}

// CutObjects copies the objects named by ids to the system clipboard as scene-format YAML
// and removes the originals (with their connections) in one undoable step.
func (e *Editor) CutObjects(ids ...string) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	e.cancelActiveObjectDrags()
	roots, err := e.normalizeObjectSelection(ids)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", nil
	}
	doc, err := e.buildClipboardSceneDoc(roots)
	if err != nil {
		return "", err
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		return "", err
	}
	text := string(data)
	e.writeClipboard(text)

	label := "Cut " + roots[0]
	status := "Cut " + roots[0]
	if len(roots) > 1 {
		label = fmt.Sprintf("Cut %d objects", len(roots))
		status = label
	}
	if err := e.deleteRootObjects(roots, label, status); err != nil {
		return "", err
	}
	return text, nil
}

// CutSelected cuts the currently selected object(s) to the system clipboard in one undoable step.
func (e *Editor) CutSelected() (string, error) {
	if e == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return "", nil
	}
	return e.CutObjects(ids...)
}

// Paste reads scene-format YAML from the system clipboard and pastes its objects into the
// active layer (at the viewport pointer when hovering, or with an offset when pasting into
// a scene where original IDs are already taken) in one undoable step.
func (e *Editor) Paste() ([]string, error) {
	return e.PasteWith(PasteOptions{})
}

// PasteInto reads scene-format YAML from the system clipboard and pastes its objects as children
// of parentID in one undoable step.
func (e *Editor) PasteInto(parentID string) ([]string, error) {
	return e.PasteWith(PasteOptions{ParentID: parentID})
}

// PasteWith reads scene-format YAML from the system clipboard and pastes its objects according to opts.
func (e *Editor) PasteWith(opts PasteOptions) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	tabID := e.activeTabID()
	doc := e.Doc
	var (
		called bool
		outIDs []string
		outErr error
	)
	e.readClipboard(func(text string) {
		called = true
		if e.Doc != doc || e.activeTabID() != tabID || e.playing {
			return
		}
		ids, err := e.PasteYAMLWith([]byte(text), opts)
		outIDs, outErr = ids, err
	})
	if !called {
		return nil, nil
	}
	return outIDs, outErr
}

func validatePasteOptions(opts PasteOptions) error {
	if opts.At != nil && (!isFiniteFloat32(opts.At[0]) || !isFiniteFloat32(opts.At[1])) {
		return errors.New("paste failed: non-finite At coordinates")
	}
	if opts.At3D != nil && (!isFiniteFloat32(opts.At3D[0]) || !isFiniteFloat32(opts.At3D[1]) || !isFiniteFloat32(opts.At3D[2])) {
		return errors.New("paste failed: non-finite At3D coordinates")
	}
	if opts.Offset != nil && (!isFiniteFloat32(opts.Offset[0]) || !isFiniteFloat32(opts.Offset[1])) {
		return errors.New("paste failed: non-finite Offset coordinates")
	}
	if opts.Offset3D != nil && (!isFiniteFloat32(opts.Offset3D[0]) || !isFiniteFloat32(opts.Offset3D[1]) || !isFiniteFloat32(opts.Offset3D[2])) {
		return errors.New("paste failed: non-finite Offset3D coordinates")
	}
	return nil
}

// PasteYAML parses data as scene-format YAML and pastes its objects into the active layer
// in one undoable step. Invalid YAML or validation failures are refused without mutating the scene
// or undo history.
func (e *Editor) PasteYAML(data []byte) ([]string, error) {
	return e.PasteYAMLWith(data, PasteOptions{})
}

// PasteYAMLWith parses data as scene-format YAML and pastes its objects according to opts
// in one undoable step.
func (e *Editor) PasteYAMLWith(data []byte, opts PasteOptions) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	if err := validatePasteOptions(opts); err != nil {
		e.report(err)
		return nil, err
	}
	e.cancelActiveObjectDrags()

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		err := errors.New("paste failed: clipboard is empty")
		e.report(err)
		return nil, err
	}

	clipDoc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
		Components:    e.components,
		PhysicsLayers: e.PhysicsLayers(),
	})
	if err != nil {
		wrapped := fmt.Errorf("paste failed: %w", err)
		e.report(wrapped)
		return nil, wrapped
	}

	var roots []sceneformat.ObjectDoc
	for li := range clipDoc.Layers {
		for oi := range clipDoc.Layers[li].Objects {
			roots = append(roots, cloneObjectDoc(clipDoc.Layers[li].Objects[oi]))
		}
	}
	if len(roots) == 0 {
		err := errors.New("paste failed: clipboard YAML contains no objects")
		e.report(err)
		return nil, err
	}

	// Resolve target layer and optional parent.
	targetLayer := opts.Layer
	parentID := opts.ParentID
	var parentPath []*sceneformat.ObjectDoc
	if parentID != "" {
		ploc := locateObject(e.Doc, parentID)
		if ploc.obj == nil {
			err := fmt.Errorf("paste failed: parent object %q not found", parentID)
			e.report(err)
			return nil, err
		}
		targetLayer = e.Doc.Layers[ploc.layer].Name
		parentPath = objectPath(e.Doc, parentID)
	}
	li, err := e.resolveLayerIndex(targetLayer)
	if err != nil {
		wrapped := fmt.Errorf("paste failed: %w", err)
		e.report(wrapped)
		return nil, wrapped
	}
	targetLayer = e.Doc.Layers[li].Name

	// Detect whether any original ID is already in use before rewriting IDs.
	anyTaken := false
	for i := range roots {
		sceneformat.WalkObjects([]sceneformat.ObjectDoc{roots[i]}, func(o, _ *sceneformat.ObjectDoc) bool {
			if e.objectIDInUse(o.ID) {
				anyTaken = true
				return false
			}
			return true
		})
		if anyTaken {
			break
		}
	}

	// Determine placement transform shift.
	e.applyPastePlacement(roots, targetLayer, parentPath, opts, anyTaken, string(data))

	placements := make([]pastePlacement, len(roots))
	for i := range roots {
		placements[i] = pastePlacement{
			layerName: targetLayer,
			parentID:  parentID,
			index:     -1,
			obj:       roots[i],
		}
	}

	label := "Paste"
	if len(roots) == 1 {
		label = "Paste " + roots[0].ID
	} else {
		label = fmt.Sprintf("Paste %d objects", len(roots))
	}

	newIDs, err := e.commitPastedObjects(placements, clipDoc.Connections, clipDoc.Overrides, label, "Pasted")
	if err != nil {
		wrapped := fmt.Errorf("paste failed: %w", err)
		e.report(wrapped)
		return nil, wrapped
	}
	return newIDs, nil
}

func (e *Editor) applyPastePlacement(roots []sceneformat.ObjectDoc, targetLayer string, parentPath []*sceneformat.ObjectDoc, opts PasteOptions, anyTaken bool, rawKey string) {
	if len(roots) == 0 {
		return
	}
	// Compute centroid of roots.
	var sumX, sumY, sumZ float64
	for i := range roots {
		sumX += float64(roots[i].X)
		sumY += float64(roots[i].Y)
		sumZ += float64(roots[i].PosZ)
	}
	n := float64(len(roots))
	cx, cy, cz := sumX/n, sumY/n, sumZ/n

	shift2D := func(dx, dy float64) {
		for i := range roots {
			roots[i].X = tidy(float64(roots[i].X) + dx)
			roots[i].Y = tidy(float64(roots[i].Y) + dy)
		}
	}
	shift3D := func(dx, dy, dz float64) {
		for i := range roots {
			roots[i].X = tidy(float64(roots[i].X) + dx)
			roots[i].Y = tidy(float64(roots[i].Y) + dy)
			roots[i].PosZ = tidy(float64(roots[i].PosZ) + dz)
		}
	}

	if opts.At3D != nil {
		tx, ty, tz := float64(opts.At3D[0]), float64(opts.At3D[1]), float64(opts.At3D[2])
		if len(parentPath) > 0 {
			if inv, ok := chain3(parentPath).invert(); ok {
				p := inv.point([3]float64{tx, ty, tz})
				tx, ty, tz = p[0], p[1], p[2]
			}
		}
		shift3D(tx-cx, ty-cy, tz-cz)
		return
	}
	if opts.At != nil {
		tx, ty := float64(opts.At[0]), float64(opts.At[1])
		if len(parentPath) > 0 {
			if inv, ok := chain2(parentPath).invert(); ok {
				tx, ty = inv.apply(tx, ty)
			}
		}
		shift2D(tx-cx, ty-cy)
		return
	}
	if opts.Offset3D != nil {
		shift3D(float64(opts.Offset3D[0]), float64(opts.Offset3D[1]), float64(opts.Offset3D[2]))
		return
	}
	if opts.Offset != nil {
		shift2D(float64(opts.Offset[0]), float64(opts.Offset[1]))
		return
	}

	// Pointer placement when hovering over the active viewport.
	if e.ViewMode == ViewMode3D && e.hasPointer3D {
		cw, ch := e.CanvasSize()
		if cw > 0 && ch > 0 {
			if hit, ok := e.PlacementAt3D(e.pointer3D[0], e.pointer3D[1], cw, ch); ok {
				tx, ty, tz := float64(hit.Point[0]), float64(hit.Point[1]), float64(hit.Point[2])
				if len(parentPath) > 0 {
					if inv, ok := chain3(parentPath).invert(); ok {
						p := inv.point([3]float64{tx, ty, tz})
						tx, ty, tz = p[0], p[1], p[2]
					}
				}
				tx, ty, tz = e.SnapPosition3D(tx, ty, tz)
				shift3D(tx-cx, ty-cy, tz-cz)
				return
			}
		}
	}
	if e.ViewMode == ViewMode2D && e.hasPointer2D {
		space, px, py := e.layerSpaceAndParallax(targetLayer)
		lx, ly := e.canvasToLayerFor(space, px, py, e.pointer2D.X, e.pointer2D.Y)
		tx, ty := float64(lx), float64(ly)
		if len(parentPath) > 0 {
			if inv, ok := chain2(parentPath).invert(); ok {
				tx, ty = inv.apply(tx, ty)
			}
		}
		tx, ty = e.SnapPosition2D(tx, ty)
		shift2D(tx-cx, ty-cy)
		return
	}

	// No pointer hovering: if any original ID is taken in this scene, offset so copies land next to originals.
	if anyTaken {
		if rawKey != "" && rawKey == e.lastPasteYAML {
			e.pasteStreak++
		} else {
			e.lastPasteYAML = rawKey
			e.pasteStreak = 1
		}
		step := float64(e.pasteStreak)
		if e.ViewMode == ViewMode3D {
			shift3D(float64(defaultPasteOffset3D)*step, 0, 0)
		} else {
			shift2D(float64(defaultPasteOffset2D)*step, float64(defaultPasteOffset2D)*step)
		}
	} else {
		e.lastPasteYAML = rawKey
		e.pasteStreak = 0
	}
}

type pastePlacement struct {
	layerName string
	parentID  string
	index     int // -1 appends to end
	obj       sceneformat.ObjectDoc
}

// allocateUniqueID returns a unique ID not present in e.Doc/e.Built or allocated.
func (e *Editor) allocateUniqueID(base string, allocated map[string]struct{}) string {
	if base == "" {
		base = "obj"
	}
	for {
		cand := e.generateObjectID(base)
		if _, taken := allocated[cand]; !taken {
			allocated[cand] = struct{}{}
			return cand
		}
	}
}

func rewriteObjectRef(ref string, idMap map[string]string, instancing map[string]bool) string {
	if newID, ok := idMap[ref]; ok {
		return newID
	}
	cur := ref
	for {
		i := strings.LastIndexByte(cur, '/')
		if i <= 0 {
			return ref
		}
		cur = cur[:i]
		if instancing[cur] {
			if newID, ok := idMap[cur]; ok {
				return newID + ref[len(cur):]
			}
		}
	}
}

func docHasCurrentCamera3D(doc *sceneformat.SceneDoc) bool {
	if doc == nil {
		return false
	}
	found := false
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.Camera != nil && o.Camera.Current {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// commitPastedObjects allocates fresh unique IDs where originals are taken, rewrites pasted
// connections and overrides to the new IDs, validates everything through PlaceObject's checks,
// and executes the whole operation as one undoable step.
func (e *Editor) commitPastedObjects(placements []pastePlacement, rawConns []sceneformat.ConnectionDoc, rawOverrides map[string]sceneformat.ObjectOverride, label, statusVerb string) ([]string, error) {
	if len(placements) == 0 {
		return nil, nil
	}

	idMap := make(map[string]string)
	instancing := make(map[string]bool)
	allocated := make(map[string]struct{})
	hasCurrentCam := docHasCurrentCamera3D(e.Doc)
	needCurrentVersion := false

	var assignIDs func(o *sceneformat.ObjectDoc)
	assignIDs = func(o *sceneformat.ObjectDoc) {
		oldID := o.ID
		if o.Scene != "" {
			instancing[oldID] = true
		}
		newID := oldID
		if newID == "" || e.objectIDInUse(newID) {
			base := oldID
			if base == "" {
				base = o.Name
			}
			newID = e.allocateUniqueID(base, allocated)
		} else if _, taken := allocated[newID]; taken {
			newID = e.allocateUniqueID(oldID, allocated)
		} else {
			allocated[newID] = struct{}{}
		}
		if oldID != "" {
			idMap[oldID] = newID
		}
		o.ID = newID
		if o.Camera != nil && o.Camera.Current {
			if hasCurrentCam {
				o.Camera.Current = false
			} else {
				hasCurrentCam = true
			}
		}
		for i := range o.Children {
			assignIDs(&o.Children[i])
		}
	}

	prepared := make([]pastePlacement, len(placements))
	newRootIDs := make([]string, len(placements))
	for i := range placements {
		p := placements[i]
		p.obj = cloneObjectDoc(p.obj)
		if p.obj.Prefab != "" {
			if pf, ok := e.Prefabs[p.obj.Prefab]; ok {
				applyPrefabDefaults(&p.obj, pf)
			}
		}
		assignIDs(&p.obj)
		if instancesScene(&p.obj) || drawsModel(&p.obj) || hasOccluder(&p.obj) || hasParticles(&p.obj) || usesScene3DFeatures(&p.obj) {
			needCurrentVersion = true
		}
		var invalid error
		sceneformat.WalkObjects([]sceneformat.ObjectDoc{p.obj}, func(o, _ *sceneformat.ObjectDoc) bool {
			invalid = validateObjectForEditor(o)
			return invalid == nil
		})
		if invalid != nil {
			return nil, invalid
		}
		if err := sceneformat.CheckObjectTree(&p.obj); err != nil {
			return nil, err
		}
		prepared[i] = p
		newRootIDs[i] = p.obj.ID
	}

	// Rewrite connections.
	var conns []sceneformat.ConnectionDoc
	for _, c := range rawConns {
		rc := cloneConnectionDoc(c)
		rc.From = rewriteObjectRef(rc.From, idMap, instancing)
		rc.To = rewriteObjectRef(rc.To, idMap, instancing)
		conns = append(conns, rc)
	}

	// Rewrite scene-level overrides when the target scene inherits a base scene.
	var overrides []pasteOverrideEntry
	if e.Doc.Inherits != "" && len(rawOverrides) > 0 {
		for _, key := range slices.Sorted(maps.Keys(rawOverrides)) {
			newKey := rewriteObjectRef(key, idMap, instancing)
			if newKey == key && !e.objectIDInUse(key) {
				// Only carry over overrides that belong to the pasted objects or their instanced children.
				if _, ok := idMap[key]; !ok {
					belongs := false
					cur := key
					for {
						idx := strings.LastIndexByte(cur, '/')
						if idx <= 0 {
							break
						}
						cur = cur[:idx]
						if instancing[cur] {
							belongs = true
							break
						}
					}
					if !belongs {
						continue
					}
				}
			}
			prev, hadPrev := e.Doc.Overrides[newKey]
			entry := pasteOverrideEntry{
				key:     newKey,
				value:   cloneObjectOverride(rawOverrides[key]),
				hadPrev: hadPrev,
			}
			if hadPrev {
				entry.prev = cloneObjectOverride(prev)
			}
			overrides = append(overrides, entry)
		}
	}

	// Build candidate document and run PlaceObject's full validation checks.
	candidate := e.Doc.Clone()
	if needCurrentVersion && candidate.Version < sceneformat.CurrentVersion {
		candidate.Version = sceneformat.CurrentVersion
	}
	for _, p := range prepared {
		if p.parentID != "" {
			ploc := locateObject(candidate, p.parentID)
			if ploc.obj == nil {
				return nil, fmt.Errorf("editor: parent object %q not found", p.parentID)
			}
			if levels := ploc.depth + subtreeHeight(&p.obj); levels > sceneformat.MaxObjectDepth {
				return nil, fmt.Errorf("editor: pasting object %q under %q would nest %d levels, deeper than %d", p.obj.ID, p.parentID, levels, sceneformat.MaxObjectDepth)
			}
		}
		if !insertObject(candidate, objectSlot{layer: p.layerName, parent: p.parentID, index: p.index}, cloneObjectDoc(p.obj)) {
			return nil, fmt.Errorf("editor: cannot place object %q on layer %q", p.obj.ID, p.layerName)
		}
	}
	if len(conns) > 0 {
		candidate.Connections = append(candidate.Connections, conns...)
	}
	if len(overrides) > 0 {
		if candidate.Overrides == nil {
			candidate.Overrides = make(map[string]sceneformat.ObjectOverride, len(overrides))
		}
		for _, ov := range overrides {
			candidate.Overrides[ov.key] = cloneObjectOverride(ov.value)
		}
	}

	if err := sceneformat.Validate(candidate); err != nil {
		return nil, err
	}
	if e.components != nil {
		if err := sceneformat.ValidateComponents(candidate, e.components); err != nil {
			return nil, err
		}
	}
	if layers := e.PhysicsLayers(); layers != nil {
		if err := sceneformat.ValidatePhysicsLayers(candidate, layers); err != nil {
			return nil, err
		}
	}
	hasInstancing := false
	for i := range prepared {
		if instancesScene(&prepared[i].obj) {
			hasInstancing = true
			break
		}
	}
	if hasInstancing {
		scenes, ok := e.Resolver.(sceneformat.SceneResolver)
		if !ok {
			return nil, errNoSceneResolver
		}
		var resolved *sceneformat.SceneDoc
		var err error
		if e.ScenePath == "" {
			resolved, err = sceneformat.Resolve(candidate, scenes)
		} else {
			probe := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Inherits: e.ScenePath}
			resolved, err = sceneformat.Resolve(probe, editedScene{SceneResolver: scenes, path: e.ScenePath, doc: candidate})
		}
		if err != nil {
			return nil, err
		}
		if e.components != nil && len(candidate.Connections) > 0 {
			if err := sceneformat.ValidateConnections(resolved, e.components); err != nil {
				return nil, err
			}
		}
	} else if e.components != nil && len(candidate.Connections) > 0 {
		if !candidate.RefersToScenes() {
			if err := sceneformat.ValidateConnections(candidate, e.components); err != nil {
				return nil, err
			}
		} else if scenes, ok := e.Resolver.(sceneformat.SceneResolver); ok {
			resolved, err := sceneformat.Resolve(candidate, scenes)
			if err != nil {
				return nil, err
			}
			if err := sceneformat.ValidateConnections(resolved, e.components); err != nil {
				return nil, err
			}
		}
	}

	status := fmt.Sprintf("%s %s", statusVerb, newRootIDs[0])
	if len(newRootIDs) > 1 {
		status = fmt.Sprintf("%s %d objects", statusVerb, len(newRootIDs))
	}

	cmd := &pasteObjectsCmd{
		ed:                 e,
		label:              label,
		status:             status,
		placements:         prepared,
		conns:              conns,
		overrides:          overrides,
		needCurrentVersion: needCurrentVersion,
		prevVersion:        e.Doc.Version,
		prevSelected:       e.SelectedID,
		prevSelectedIDs:    slices.Clone(e.SelectedIDs()),
		newSelectedIDs:     slices.Clone(newRootIDs),
	}
	e.History.Execute(cmd)
	return newRootIDs, nil
}

type pasteOverrideEntry struct {
	key     string
	value   sceneformat.ObjectOverride
	hadPrev bool
	prev    sceneformat.ObjectOverride
}

type pasteObjectsCmd struct {
	ed                 *Editor
	label              string
	status             string
	placements         []pastePlacement
	conns              []sceneformat.ConnectionDoc
	overrides          []pasteOverrideEntry
	needCurrentVersion bool
	prevVersion        int
	prevSelected       string
	prevSelectedIDs    []string
	newSelectedIDs     []string
	wasNilSlices       []bool
	wasNilConns        bool
	wasNilOverrides    bool
}

func (c *pasteObjectsCmd) Label() string { return c.label }

func (c *pasteObjectsCmd) Do() {
	if c.needCurrentVersion && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	c.wasNilSlices = make([]bool, len(c.placements))
	for i, p := range c.placements {
		if s := siblingsAt(c.ed.Doc, p.layerName, p.parentID); s != nil {
			c.wasNilSlices[i] = *s == nil
		}
		insertObject(c.ed.Doc, objectSlot{layer: p.layerName, parent: p.parentID, index: p.index}, cloneObjectDoc(p.obj))
	}
	if len(c.conns) > 0 {
		c.wasNilConns = c.ed.Doc.Connections == nil
		for _, conn := range c.conns {
			c.ed.Doc.Connections = append(c.ed.Doc.Connections, cloneConnectionDoc(conn))
		}
	}
	if len(c.overrides) > 0 {
		c.wasNilOverrides = c.ed.Doc.Overrides == nil
		if c.ed.Doc.Overrides == nil {
			c.ed.Doc.Overrides = make(map[string]sceneformat.ObjectOverride, len(c.overrides))
		}
		for _, ov := range c.overrides {
			c.ed.Doc.Overrides[ov.key] = cloneObjectOverride(ov.value)
		}
	}
	c.ed.selectedIDs = slices.Clone(c.newSelectedIDs)
	if len(c.newSelectedIDs) > 0 {
		c.ed.SelectedID = c.newSelectedIDs[len(c.newSelectedIDs)-1]
	}
	c.ed.Status = c.status
}

func (c *pasteObjectsCmd) Undo() {
	for i := len(c.placements) - 1; i >= 0; i-- {
		p := c.placements[i]
		_, _ = takeObject(c.ed.Doc, p.obj.ID)
		if s := siblingsAt(c.ed.Doc, p.layerName, p.parentID); s != nil && len(*s) == 0 && c.wasNilSlices[i] {
			*s = nil
		}
	}
	if len(c.conns) > 0 {
		n := len(c.ed.Doc.Connections) - len(c.conns)
		if n < 0 {
			n = 0
		}
		c.ed.Doc.Connections = c.ed.Doc.Connections[:n]
		if len(c.ed.Doc.Connections) == 0 && c.wasNilConns {
			c.ed.Doc.Connections = nil
		}
	}
	if len(c.overrides) > 0 {
		for _, ov := range c.overrides {
			if ov.hadPrev {
				c.ed.Doc.Overrides[ov.key] = cloneObjectOverride(ov.prev)
			} else {
				delete(c.ed.Doc.Overrides, ov.key)
			}
		}
		if len(c.ed.Doc.Overrides) == 0 && c.wasNilOverrides {
			c.ed.Doc.Overrides = nil
		}
	}
	c.ed.Doc.Version = c.prevVersion
	c.ed.SelectedID = c.prevSelected
	c.ed.selectedIDs = slices.Clone(c.prevSelectedIDs)
}

// DuplicateObjects clones all objects named by ids (with their children, components, properties,
// internal connections, and scene overrides) next to the originals in one undoable step,
// allocating fresh unique IDs and rewriting connections among the duplicated objects to point
// at the copies. It selects the duplicates and returns their new root IDs.
func (e *Editor) DuplicateObjects(ids ...string) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	e.cancelActiveObjectDrags()
	roots, err := e.normalizeObjectSelection(ids)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, nil
	}

	clipDoc, err := e.buildClipboardSceneDoc(roots)
	if err != nil {
		return nil, err
	}

	placements := make([]pastePlacement, len(roots))
	for i, id := range roots {
		loc := locateObject(e.Doc, id)
		if loc.obj == nil {
			return nil, fmt.Errorf("editor: object %q not found", id)
		}
		cloned := cloneObjectDoc(*loc.obj)
		if e.ViewMode == ViewMode3D {
			cloned.X = tidy(float64(cloned.X) + float64(defaultPasteOffset3D))
		} else {
			cloned.X = tidy(float64(cloned.X) + float64(defaultPasteOffset2D))
			cloned.Y = tidy(float64(cloned.Y) + float64(defaultPasteOffset2D))
		}
		parentID := ""
		if loc.parent != nil {
			parentID = loc.parent.ID
		}
		placements[i] = pastePlacement{
			layerName: e.Doc.Layers[loc.layer].Name,
			parentID:  parentID,
			index:     -1,
			obj:       cloned,
		}
	}

	label := "Duplicate " + roots[0]
	if len(roots) > 1 {
		label = fmt.Sprintf("Duplicate %d objects", len(roots))
	}
	return e.commitPastedObjects(placements, clipDoc.Connections, clipDoc.Overrides, label, "Duplicated")
}

// DuplicateSelectedObjects duplicates all currently selected objects in one undoable step
// and returns the new root IDs.
func (e *Editor) DuplicateSelectedObjects() ([]string, error) {
	if e == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return nil, nil
	}
	return e.DuplicateObjects(ids...)
}

// deleteRootObjects removes multiple normalized root objects and their connections in one undo step.
func (e *Editor) deleteRootObjects(roots []string, label, status string) error {
	if len(roots) == 0 {
		return nil
	}
	type deleteEntry struct {
		slot objectSlot
		obj  sceneformat.ObjectDoc
	}
	entries := make([]deleteEntry, 0, len(roots))
	for _, id := range roots {
		loc := locateObject(e.Doc, id)
		if loc.obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		parentID := ""
		if loc.parent != nil {
			parentID = loc.parent.ID
		}
		entries = append(entries, deleteEntry{
			slot: objectSlot{
				layer:  e.Doc.Layers[loc.layer].Name,
				parent: parentID,
				index:  loc.index,
			},
			obj: cloneObjectDoc(*loc.obj),
		})
	}
	cmd := &deleteObjectsCmd{
		ed:              e,
		label:           label,
		status:          status,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: slices.Clone(e.SelectedIDs()),
		entries:         make([]deleteObjectEntry, len(entries)),
	}
	for i, en := range entries {
		cmd.entries[i] = deleteObjectEntry{
			slot: en.slot,
			obj:  en.obj,
		}
	}
	e.History.Execute(cmd)
	return nil
}

type deleteObjectEntry struct {
	slot objectSlot
	obj  sceneformat.ObjectDoc
}

type deleteObjectsCmd struct {
	ed              *Editor
	label           string
	status          string
	entries         []deleteObjectEntry
	conns           []removedConnection
	prevSelected    string
	prevSelectedIDs []string
}

func (c *deleteObjectsCmd) Label() string { return c.label }

func (c *deleteObjectsCmd) Do() {
	ids := make(map[string]struct{})
	instancing := make(map[string]struct{})
	for _, en := range c.entries {
		_, _ = takeObject(c.ed.Doc, en.obj.ID)
		sceneformat.WalkObjects([]sceneformat.ObjectDoc{en.obj}, func(o, _ *sceneformat.ObjectDoc) bool {
			ids[o.ID] = struct{}{}
			if o.Scene != "" {
				instancing[o.ID] = struct{}{}
			}
			return true
		})
	}
	c.conns = nil
	var kept []sceneformat.ConnectionDoc
	for i, conn := range c.ed.Doc.Connections {
		if namesSubtree(conn.From, ids, instancing) || namesSubtree(conn.To, ids, instancing) {
			c.conns = append(c.conns, removedConnection{index: i, conn: conn})
			continue
		}
		kept = append(kept, conn)
	}
	c.ed.Doc.Connections = kept
	if _, removed := ids[c.ed.SelectedID]; removed {
		c.ed.SelectedID = ""
	}
	var remaining []string
	for _, id := range c.ed.selectedIDs {
		if _, removed := ids[id]; !removed {
			remaining = append(remaining, id)
		}
	}
	c.ed.selectedIDs = remaining
	if c.ed.SelectedID == "" && len(remaining) > 0 {
		c.ed.SelectedID = remaining[len(remaining)-1]
	}
	c.ed.Status = c.status
}

func (c *deleteObjectsCmd) Undo() {
	// Re-insert entries in ascending index order within each parent/layer slot
	// so every object returns to its exact original sibling index.
	ordered := slices.Clone(c.entries)
	slices.SortStableFunc(ordered, func(a, b deleteObjectEntry) int {
		if a.slot.layer != b.slot.layer {
			return strings.Compare(a.slot.layer, b.slot.layer)
		}
		if a.slot.parent != b.slot.parent {
			return strings.Compare(a.slot.parent, b.slot.parent)
		}
		return a.slot.index - b.slot.index
	})
	for _, en := range ordered {
		insertObject(c.ed.Doc, en.slot, cloneObjectDoc(en.obj))
	}
	for _, rc := range c.conns {
		at := min(rc.index, len(c.ed.Doc.Connections))
		c.ed.Doc.Connections = slices.Insert(c.ed.Doc.Connections, at, rc.conn)
	}
	c.ed.SelectedID = c.prevSelected
	c.ed.selectedIDs = slices.Clone(c.prevSelectedIDs)
}

// GroupObjects wraps the objects named by ids under a new empty parent object placed at
// their common centre without moving any of them on screen, as one undoable step.
// It selects and returns the new group parent's ID.
func (e *Editor) GroupObjects(ids ...string) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	e.cancelActiveObjectDrags()
	roots, err := e.normalizeObjectSelection(ids)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", errors.New("editor: no objects to group")
	}

	// Determine target layer and common parent (if all roots share the exact same parent).
	firstLoc := locateObject(e.Doc, roots[0])
	targetLayer := e.Doc.Layers[firstLoc.layer].Name
	commonParent := ""
	if firstLoc.parent != nil {
		commonParent = firstLoc.parent.ID
	}
	sameParent := true
	for _, id := range roots[1:] {
		loc := locateObject(e.Doc, id)
		p := ""
		if loc.parent != nil {
			p = loc.parent.ID
		}
		if loc.layer != firstLoc.layer || p != commonParent {
			sameParent = false
			break
		}
	}
	groupParent := ""
	groupIndex := firstLoc.index
	if sameParent {
		groupParent = commonParent
	} else {
		// Place the group at the top level of the first object's layer, right where its top-level ancestor sits.
		firstPath := objectPath(e.Doc, roots[0])
		if len(firstPath) > 0 {
			topLoc := locateObject(e.Doc, firstPath[0].ID)
			groupIndex = topLoc.index
		}
	}

	var parentAncestors []*sceneformat.ObjectDoc
	groupDepth := 1
	if groupParent != "" {
		parentAncestors = objectPath(e.Doc, groupParent)
		if parentAncestors == nil {
			return "", fmt.Errorf("editor: parent object %q not found", groupParent)
		}
		groupDepth = len(parentAncestors) + 1
	}

	// Compute the common world-space centre of all selected roots.
	var sumX, sumY, sumZ float64
	uses3D := false
	for _, id := range roots {
		path := objectPath(e.Doc, id)
		if path == nil {
			return "", fmt.Errorf("editor: object %q not found", id)
		}
		if slices.ContainsFunc(path, uses3DFields) {
			uses3D = true
		}
		m2 := chain2(path)
		m3 := chain3(path)
		if slices.ContainsFunc(path, func(a *sceneformat.ObjectDoc) bool { return a.RotX != 0 || a.RotY != 0 || a.RotZ != 0 }) {
			sumX += m3[0][3]
			sumY += m3[1][3]
			sumZ += m3[2][3]
		} else {
			sumX += m2.tx
			sumY += m2.ty
			sumZ += m3[2][3]
		}
	}
	n := float64(len(roots))
	cx, cy, cz := sumX/n, sumY/n, sumZ/n

	// Convert world centre into groupParent's local space if nested.
	gx, gy, gz := cx, cy, cz
	if len(parentAncestors) > 0 {
		if slices.ContainsFunc(parentAncestors, func(a *sceneformat.ObjectDoc) bool { return a.RotX != 0 || a.RotY != 0 || a.RotZ != 0 }) {
			inv3, ok := chain3(parentAncestors).invert()
			if !ok {
				return "", fmt.Errorf("editor: cannot group under %q: %w", groupParent, errNoLocalTransform)
			}
			p := inv3.point([3]float64{cx, cy, cz})
			gx, gy, gz = p[0], p[1], p[2]
		} else {
			inv2, ok := chain2(parentAncestors).invert()
			if !ok {
				return "", fmt.Errorf("editor: cannot group under %q: %w", groupParent, errNoLocalTransform)
			}
			gx, gy = inv2.apply(cx, cy)
			if uses3D {
				inv3, ok3 := chain3(parentAncestors).invert()
				if !ok3 {
					return "", fmt.Errorf("editor: cannot group under %q: %w", groupParent, errNoLocalTransform)
				}
				p := inv3.point([3]float64{cx, cy, cz})
				gz = p[2]
			}
		}
	}

	groupID := e.generateObjectID("group")
	groupObj := sceneformat.ObjectDoc{
		ID:   groupID,
		Name: "Group",
		X:    tidy(gx),
		Y:    tidy(gy),
	}
	if uses3D {
		groupObj.PosZ = tidy(gz)
	}

	newAncestors := append(slices.Clone(parentAncestors), &groupObj)
	moves := make([]groupMemberMove, 0, len(roots))
	for _, id := range roots {
		loc := locateObject(e.Doc, id)
		if loc.obj == nil {
			return "", fmt.Errorf("editor: object %q not found", id)
		}
		if levels := groupDepth + subtreeHeight(loc.obj); levels > sceneformat.MaxObjectDepth {
			return "", fmt.Errorf("editor: grouping object %q would nest %d levels, deeper than %d", id, levels, sceneformat.MaxObjectDepth)
		}
		oldPath := objectPath(e.Doc, id)
		before := captureTransform(loc.obj)
		after, err := keepWorldTransform(oldPath[:len(oldPath)-1], newAncestors, loc.obj)
		if err != nil {
			return "", fmt.Errorf("editor: cannot group object %q: %w", id, err)
		}
		parentID := ""
		if loc.parent != nil {
			parentID = loc.parent.ID
		}
		moves = append(moves, groupMemberMove{
			id:     id,
			from:   objectSlot{layer: e.Doc.Layers[loc.layer].Name, parent: parentID, index: loc.index},
			before: before,
			after:  after,
		})
	}

	groupSlot := objectSlot{layer: targetLayer, parent: groupParent, index: groupIndex}

	// Validate resulting document before mutating history.
	candidate := e.Doc.Clone()
	if !insertObject(candidate, groupSlot, cloneObjectDoc(groupObj)) {
		return "", fmt.Errorf("editor: cannot create group on layer %q", targetLayer)
	}
	for i, mv := range moves {
		if !moveObjectIn(candidate, mv.id, objectSlot{layer: targetLayer, parent: groupID, index: i}, mv.after) {
			return "", fmt.Errorf("editor: cannot move object %q into group", mv.id)
		}
	}
	if err := sceneformat.Validate(candidate); err != nil {
		return "", err
	}

	label := "Group " + groupID
	if len(roots) > 1 {
		label = fmt.Sprintf("Group %d objects", len(roots))
	}
	cmd := &groupObjectsCmd{
		ed:              e,
		label:           label,
		groupObj:        groupObj,
		groupSlot:       groupSlot,
		moves:           moves,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: slices.Clone(e.SelectedIDs()),
	}
	e.History.Execute(cmd)
	return groupID, nil
}

// GroupSelected wraps the currently selected object(s) under a new empty parent object at
// their common centre without moving them on screen, as one undoable step.
func (e *Editor) GroupSelected() (string, error) {
	if e == nil {
		return "", errors.New("editor: no open scene")
	}
	if e.playing {
		return "", ErrPlaying
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return "", nil
	}
	return e.GroupObjects(ids...)
}

type groupMemberMove struct {
	id     string
	from   objectSlot
	before objectTransform
	after  objectTransform
}

type groupObjectsCmd struct {
	ed              *Editor
	label           string
	groupObj        sceneformat.ObjectDoc
	groupSlot       objectSlot
	moves           []groupMemberMove
	prevSelected    string
	prevSelectedIDs []string
}

func (c *groupObjectsCmd) Label() string { return c.label }

func (c *groupObjectsCmd) Do() {
	insertObject(c.ed.Doc, c.groupSlot, cloneObjectDoc(c.groupObj))
	for i, mv := range c.moves {
		moveObjectIn(c.ed.Doc, mv.id, objectSlot{layer: c.groupSlot.layer, parent: c.groupObj.ID, index: i}, mv.after)
	}
	c.ed.SelectedID = c.groupObj.ID
	c.ed.selectedIDs = []string{c.groupObj.ID}
	c.ed.Status = "Grouped into " + c.groupObj.ID
}

func (c *groupObjectsCmd) Undo() {
	taken := make(map[string]sceneformat.ObjectDoc, len(c.moves))
	for _, mv := range c.moves {
		if o, ok := takeObject(c.ed.Doc, mv.id); ok {
			mv.before.apply(&o)
			taken[mv.id] = o
		}
	}
	_, _ = takeObject(c.ed.Doc, c.groupObj.ID)
	ordered := slices.Clone(c.moves)
	slices.SortStableFunc(ordered, func(a, b groupMemberMove) int {
		if a.from.layer != b.from.layer {
			return strings.Compare(a.from.layer, b.from.layer)
		}
		if a.from.parent != b.from.parent {
			return strings.Compare(a.from.parent, b.from.parent)
		}
		return a.from.index - b.from.index
	})
	for _, mv := range ordered {
		if o, ok := taken[mv.id]; ok {
			insertObject(c.ed.Doc, mv.from, o)
		}
	}
	c.ed.SelectedID = c.prevSelected
	c.ed.selectedIDs = slices.Clone(c.prevSelectedIDs)
}

func isEmptyGroupContainer(doc *sceneformat.SceneDoc, o *sceneformat.ObjectDoc) bool {
	if o == nil {
		return false
	}
	if o.Sprite != nil || o.Mesh != "" || o.Model != "" || o.ModelNode != "" || o.ModelMesh != "" ||
		o.GridMap != "" || o.Material != "" || o.Particles != nil || o.Light != nil ||
		o.Camera != nil || o.Occluder != nil || o.Scene != "" || o.Prefab != "" ||
		len(o.Components) > 0 || len(o.Properties) > 0 || len(o.Overrides) > 0 {
		return false
	}
	if doc != nil {
		for _, c := range doc.Connections {
			if c.From == o.ID || c.To == o.ID {
				return false
			}
		}
	}
	return true
}

// UngroupObject lifts every direct child out of object id into id's parent (or layer) while
// preserving each child's world transform, as one undoable step. When id is an empty group
// container with no connections of its own, id itself is removed; otherwise id is kept and its
// children are placed immediately after it. It selects and returns the lifted child IDs.
func (e *Editor) UngroupObject(id string) ([]string, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	e.cancelActiveObjectDrags()
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return nil, fmt.Errorf("editor: object %q not found", id)
	}
	if len(loc.obj.Children) == 0 {
		return nil, fmt.Errorf("editor: object %q has no children to ungroup", id)
	}

	groupPath := objectPath(e.Doc, id)
	parentAncestors := groupPath[:len(groupPath)-1]
	targetLayer := e.Doc.Layers[loc.layer].Name
	targetParent := ""
	if loc.parent != nil {
		targetParent = loc.parent.ID
	}
	removeGroup := isEmptyGroupContainer(e.Doc, loc.obj)
	insertBase := loc.index
	if !removeGroup {
		insertBase = loc.index + 1
	}

	childMoves := make([]ungroupChildMove, len(loc.obj.Children))
	liftedIDs := make([]string, len(loc.obj.Children))
	for i := range loc.obj.Children {
		ch := &loc.obj.Children[i]
		before := captureTransform(ch)
		after, err := keepWorldTransform(groupPath, parentAncestors, ch)
		if err != nil {
			return nil, fmt.Errorf("editor: cannot ungroup child %q of %q: %w", ch.ID, id, err)
		}
		childMoves[i] = ungroupChildMove{
			id:     ch.ID,
			before: before,
			after:  after,
		}
		liftedIDs[i] = ch.ID
	}

	groupShell := cloneObjectDoc(*loc.obj)
	groupShell.Children = nil
	groupSlot := objectSlot{layer: targetLayer, parent: targetParent, index: loc.index}

	candidate := e.Doc.Clone()
	for i, mv := range childMoves {
		if !moveObjectIn(candidate, mv.id, objectSlot{layer: targetLayer, parent: targetParent, index: insertBase + i}, mv.after) {
			return nil, fmt.Errorf("editor: cannot lift child %q out of %q", mv.id, id)
		}
	}
	if removeGroup {
		_, _ = takeObject(candidate, id)
	}
	if err := sceneformat.Validate(candidate); err != nil {
		return nil, err
	}

	cmd := &ungroupObjectCmd{
		ed:              e,
		groupID:         id,
		groupShell:      groupShell,
		groupSlot:       groupSlot,
		removeGroup:     removeGroup,
		insertBase:      insertBase,
		children:        childMoves,
		liftedIDs:       liftedIDs,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: slices.Clone(e.SelectedIDs()),
	}
	e.History.Execute(cmd)
	return slices.Clone(liftedIDs), nil
}

// UngroupSelected ungroups the currently selected object as one undoable step.
func (e *Editor) UngroupSelected() ([]string, error) {
	if e == nil {
		return nil, errors.New("editor: no open scene")
	}
	if e.playing {
		return nil, ErrPlaying
	}
	if e.SelectedID == "" {
		return nil, nil
	}
	return e.UngroupObject(e.SelectedID)
}

type ungroupChildMove struct {
	id     string
	before objectTransform
	after  objectTransform
}

type ungroupObjectCmd struct {
	ed              *Editor
	groupID         string
	groupShell      sceneformat.ObjectDoc
	groupSlot       objectSlot
	removeGroup     bool
	insertBase      int
	children        []ungroupChildMove
	liftedIDs       []string
	prevSelected    string
	prevSelectedIDs []string
}

func (c *ungroupObjectCmd) Label() string { return "Ungroup " + c.groupID }

func (c *ungroupObjectCmd) Do() {
	taken := make([]sceneformat.ObjectDoc, 0, len(c.children))
	for _, ch := range c.children {
		if o, ok := takeObject(c.ed.Doc, ch.id); ok {
			ch.after.apply(&o)
			taken = append(taken, o)
		}
	}
	if c.removeGroup {
		_, _ = takeObject(c.ed.Doc, c.groupID)
	}
	for i, o := range taken {
		insertObject(c.ed.Doc, objectSlot{layer: c.groupSlot.layer, parent: c.groupSlot.parent, index: c.insertBase + i}, o)
	}
	c.ed.selectedIDs = slices.Clone(c.liftedIDs)
	if len(c.liftedIDs) > 0 {
		c.ed.SelectedID = c.liftedIDs[len(c.liftedIDs)-1]
	} else {
		c.ed.SelectedID = ""
	}
	c.ed.Status = "Ungrouped " + c.groupID
}

func (c *ungroupObjectCmd) Undo() {
	taken := make([]sceneformat.ObjectDoc, 0, len(c.children))
	for _, ch := range c.children {
		if o, ok := takeObject(c.ed.Doc, ch.id); ok {
			ch.before.apply(&o)
			taken = append(taken, o)
		}
	}
	if c.removeGroup {
		insertObject(c.ed.Doc, c.groupSlot, cloneObjectDoc(c.groupShell))
	}
	for i, o := range taken {
		insertObject(c.ed.Doc, objectSlot{layer: c.groupSlot.layer, parent: c.groupID, index: i}, o)
	}
	c.ed.SelectedID = c.prevSelected
	c.ed.selectedIDs = slices.Clone(c.prevSelectedIDs)
}

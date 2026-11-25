package editor

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// SelectionCount returns the number of currently selected objects.
func (e *Editor) SelectionCount() int {
	return len(e.SelectedIDs())
}

// IsSelected reports whether object id is part of the current selection set.
func (e *Editor) IsSelected(id string) bool {
	return e.IsObjectSelected(id)
}

// SelectedObjects returns the ObjectDoc pointers for every selected object currently
// present in e.Doc or the resolved scene, in selection order.
func (e *Editor) SelectedObjects() []*sceneformat.ObjectDoc {
	if e == nil || e.Doc == nil {
		return nil
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return nil
	}
	out := make([]*sceneformat.ObjectDoc, 0, len(ids))
	for _, id := range ids {
		if obj, _ := e.FindResolvedObject(id); obj != nil {
			out = append(out, obj)
		}
	}
	return out
}

// ClearSelection clears the entire object selection set.
func (e *Editor) ClearSelection() bool {
	return e.SelectObject("")
}

// SelectObjectsWithPrimary replaces the current selection with the unique existing object IDs in ids.
// primary sets SelectedID when it appears in the resulting set; otherwise the first ID
// becomes primary. Passing an empty slice clears selection and returns true; passing only
// unknown IDs leaves selection unchanged and returns false.
func (e *Editor) SelectObjectsWithPrimary(ids []string, primary string) bool {
	if e == nil {
		return false
	}
	if len(ids) == 0 {
		return e.SelectObject("")
	}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || slices.Contains(valid, id) {
			continue
		}
		if obj, _ := e.FindResolvedObject(id); obj != nil {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return false
	}
	if len(valid) == 1 {
		return e.SelectObject(valid[0])
	}
	if primary == "" || !slices.Contains(valid, primary) {
		primary = valid[0]
	}
	hadRoom := e.rooms != nil && e.rooms.selectedRoomID != ""
	curIDs := e.SelectedIDs()
	changed := e.SelectedID != primary || !slices.Equal(curIDs, valid) || e.inspectAssetFile || e.inspectLayer || e.connPick != nil || hadRoom
	if changed {
		e.CancelComponentGizmoDrag3D()
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
		e.setSelectionQuiet(valid, primary)
		e.inspectAssetFile = false
		e.inspectLayer = false
		e.connPick = nil
		if e.rooms != nil {
			e.rooms.selectedRoomID = ""
		}
		e.Status = fmt.Sprintf("Selected %d objects", len(valid))
		e.notify()
	}
	return true
}

// AddToSelection adds one or more existing object IDs to the current selection set,
// making the last valid ID the primary SelectedID. Returns true if at least one ID
// existed in the scene.
func (e *Editor) AddToSelection(ids ...string) bool {
	if e == nil || len(ids) == 0 {
		return false
	}
	next := e.SelectedIDs()
	primary := e.SelectedID
	foundAny := false
	for _, id := range ids {
		if id == "" {
			continue
		}
		if obj, _ := e.FindResolvedObject(id); obj == nil {
			continue
		}
		foundAny = true
		if !slices.Contains(next, id) {
			next = append(next, id)
		}
		primary = id
	}
	if !foundAny {
		return false
	}
	return e.SelectObjectsWithPrimary(next, primary)
}

// RemoveFromSelection removes ids from the current selection set. If SelectedID is removed,
// the last remaining selected object becomes primary (or selection clears when empty).
func (e *Editor) RemoveFromSelection(ids ...string) bool {
	if e == nil || len(ids) == 0 {
		return false
	}
	cur := e.SelectedIDs()
	if len(cur) == 0 {
		return false
	}
	next := make([]string, 0, len(cur))
	removed := false
	for _, id := range cur {
		if slices.Contains(ids, id) {
			removed = true
			continue
		}
		next = append(next, id)
	}
	if !removed {
		return false
	}
	if len(next) == 0 {
		return e.SelectObject("")
	}
	primary := e.SelectedID
	if !slices.Contains(next, primary) {
		primary = next[len(next)-1]
	}
	return e.SelectObjectsWithPrimary(next, primary)
}

// ToggleSelection removes id if it is currently selected, or adds it (as primary) if it exists.
func (e *Editor) ToggleSelection(id string) bool {
	if e == nil || id == "" {
		return false
	}
	if e.IsSelected(id) {
		return e.RemoveFromSelection(id)
	}
	return e.AddToSelection(id)
}

func (e *Editor) setSelectionQuiet(ids []string, primary string) {
	if len(ids) == 0 {
		if primary != "" {
			e.SelectedID = primary
			e.selectedIDs = []string{primary}
			return
		}
		e.SelectedID = ""
		e.selectedIDs = nil
		return
	}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !slices.Contains(clean, id) {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		e.SelectedID = ""
		e.selectedIDs = nil
		return
	}
	if primary == "" || !slices.Contains(clean, primary) {
		primary = clean[0]
	}
	e.SelectedID = primary
	e.selectedIDs = clean
}

func (e *Editor) clearSelectionFields() {
	e.SelectedID = ""
	e.selectedIDs = nil
}

func (e *Editor) pruneSelection() {
	if e.SelectedID == "" {
		e.selectedIDs = nil
		return
	}
	ids := e.SelectedIDs()
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if obj, _ := e.FindResolvedObject(id); obj != nil {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		e.clearSelectionFields()
		return
	}
	primary := e.SelectedID
	if !slices.Contains(kept, primary) {
		primary = kept[len(kept)-1]
	}
	e.SelectedID = primary
	e.selectedIDs = kept
}

func (e *Editor) renameSelectionID(from, to string) {
	if e.SelectedID == from {
		e.SelectedID = to
	}
	for i, id := range e.selectedIDs {
		if id == from {
			e.selectedIDs[i] = to
		}
	}
}

// topSelectedIDs returns the selected object IDs in selection order, excluding any object
// that has an ancestor also in the selection set. Use this for transforms, deletion, and
// duplication so parent+child selections are not moved, deleted, or duplicated twice.
func (e *Editor) topSelectedIDs() []string {
	ids := e.SelectedIDs()
	if len(ids) <= 1 || e.Doc == nil {
		return ids
	}
	inSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		inSet[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		path := e.resolvedObjectPath(id)
		if len(path) == 0 {
			continue
		}
		hasSelectedAncestor := false
		for _, anc := range path[:len(path)-1] {
			if inSet[anc.ID] {
				hasSelectedAncestor = true
				break
			}
		}
		if !hasSelectedAncestor {
			out = append(out, id)
		}
	}
	return out
}

// draggableSelectedIDs returns topSelectedIDs filtered to objects that are not hidden or locked
// in the viewport (viewportFrozen).
func (e *Editor) draggableSelectedIDs() []string {
	top := e.topSelectedIDs()
	if len(top) == 0 {
		return nil
	}
	out := make([]string, 0, len(top))
	for _, id := range top {
		if !e.viewportFrozen(id) {
			out = append(out, id)
		}
	}
	return out
}

// EditSelected executes fn for every currently selected object inside a single History.Group
// step labeled label, so one Undo reverts every object to its own prior state. If any call
// returns an error, all earlier edits in the group are rolled back and the error is returned.
func (e *Editor) EditSelected(label string, fn func(id string) error) error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	ids := e.SelectedIDs()
	if len(ids) == 0 {
		return nil
	}
	if len(ids) == 1 {
		return fn(ids[0])
	}
	return e.History.Group(label, func() error {
		for _, id := range ids {
			if err := fn(id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetSelectedName sets the display Name on every selected object in one undo step.
func (e *Editor) SetSelectedName(name string) error {
	return e.EditSelected("Rename selected objects", func(id string) error {
		return e.SetObjectName(id, name)
	})
}

// SetSelectedX sets the X position on every selected object in one undo step, preserving each object's Y.
func (e *Editor) SetSelectedX(x float32) error {
	return e.EditSelected("Set X on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.MoveObject(id, x, obj.Y)
	})
}

// SetSelectedY sets the Y position on every selected object in one undo step, preserving each object's X.
func (e *Editor) SetSelectedY(y float32) error {
	return e.EditSelected("Set Y on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.MoveObject(id, obj.X, y)
	})
}

// SetSelectedRotation sets the 2D rotation (radians) on every selected object in one undo step.
func (e *Editor) SetSelectedRotation(radians float32) error {
	return e.EditSelected("Set rotation on selected objects", func(id string) error {
		return e.RotateObject(id, radians)
	})
}

// SetSelectedScaleX sets ScaleX on every selected object in one undo step, preserving each object's ScaleY.
func (e *Editor) SetSelectedScaleX(sx float32) error {
	return e.EditSelected("Set Scale X on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.ScaleObject(id, sx, obj.EffectiveScaleY())
	})
}

// SetSelectedScaleY sets ScaleY on every selected object in one undo step, preserving each object's ScaleX.
func (e *Editor) SetSelectedScaleY(sy float32) error {
	return e.EditSelected("Set Scale Y on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.ScaleObject(id, obj.EffectiveScaleX(), sy)
	})
}

// SetSelectedAlpha sets Alpha on every selected object in one undo step.
func (e *Editor) SetSelectedAlpha(alpha float32) error {
	return e.EditSelected("Set Alpha on selected objects", func(id string) error {
		return e.SetObjectAlpha(id, alpha)
	})
}

// SetSelectedZ sets within-layer draw order Z on every selected object in one undo step.
func (e *Editor) SetSelectedZ(z int) error {
	return e.EditSelected("Set Z on selected objects", func(id string) error {
		return e.SetObjectZ(id, z)
	})
}

// SetSelectedPosZ sets PosZ on every selected object in one undo step, preserving X and Y.
func (e *Editor) SetSelectedPosZ(posZ float32) error {
	return e.EditSelected("Set Pos Z on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.MoveObject3D(id, obj.X, obj.Y, posZ)
	})
}

// SetSelectedRotX sets RotX on every selected object in one undo step.
func (e *Editor) SetSelectedRotX(rotX float32) error {
	return e.EditSelected("Set Rot X on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.RotateObject3D(id, rotX, obj.RotY, obj.RotZ)
	})
}

// SetSelectedRotY sets RotY on every selected object in one undo step.
func (e *Editor) SetSelectedRotY(rotY float32) error {
	return e.EditSelected("Set Rot Y on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.RotateObject3D(id, obj.RotX, rotY, obj.RotZ)
	})
}

// SetSelectedRotZ sets RotZ on every selected object in one undo step.
func (e *Editor) SetSelectedRotZ(rotZ float32) error {
	return e.EditSelected("Set Rot Z on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.RotateObject3D(id, obj.RotX, obj.RotY, rotZ)
	})
}

// SetSelectedScaleZ sets ScaleZ on every selected object in one undo step.
func (e *Editor) SetSelectedScaleZ(sz float32) error {
	return e.EditSelected("Set Scale Z on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.ScaleObject3D(id, obj.EffectiveScaleX(), obj.EffectiveScaleY(), sz)
	})
}

// SetSelectedDepth sets Depth on every selected object in one undo step.
func (e *Editor) SetSelectedDepth(depth float32) error {
	return e.EditSelected("Set Depth on selected objects", func(id string) error {
		return e.SetObjectDepth(id, depth)
	})
}

// SetSelectedMesh sets Mesh on every selected object in one undo step.
func (e *Editor) SetSelectedMesh(mesh string) error {
	return e.EditSelected("Set Mesh on selected objects", func(id string) error {
		return e.SetObjectMesh(id, mesh)
	})
}

// SetSelectedSprite sets Sprite on every selected object in one undo step.
func (e *Editor) SetSelectedSprite(sprite *sceneformat.SpriteRef) error {
	return e.EditSelected("Set Sprite on selected objects", func(id string) error {
		var sp *sceneformat.SpriteRef
		if sprite != nil {
			cp := *sprite
			sp = &cp
		}
		return e.SetObjectSprite(id, sp)
	})
}

// SetSelectedOriginX sets OriginX on every selected object in one undo step.
func (e *Editor) SetSelectedOriginX(ox float32) error {
	return e.EditSelected("Set Origin X on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.SetObjectOrigin(id, ox, obj.OriginY)
	})
}

// SetSelectedOriginY sets OriginY on every selected object in one undo step.
func (e *Editor) SetSelectedOriginY(oy float32) error {
	return e.EditSelected("Set Origin Y on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.SetObjectOrigin(id, obj.OriginX, oy)
	})
}

// SetSelectedFlipX sets FlipX on every selected object in one undo step.
func (e *Editor) SetSelectedFlipX(flipX bool) error {
	return e.EditSelected("Set Flip X on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.SetObjectFlip(id, flipX, obj.FlipY)
	})
}

// SetSelectedFlipY sets FlipY on every selected object in one undo step.
func (e *Editor) SetSelectedFlipY(flipY bool) error {
	return e.EditSelected("Set Flip Y on selected objects", func(id string) error {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", id)
		}
		return e.SetObjectFlip(id, obj.FlipX, flipY)
	})
}

// SetSelectedMaterial sets Material on every selected object in one undo step.
func (e *Editor) SetSelectedMaterial(material string) error {
	return e.EditSelected("Set Material on selected objects", func(id string) error {
		return e.SetObjectMaterial(id, material)
	})
}

// SetSelectedProperty sets custom property key to value on every selected object in one undo step.
func (e *Editor) SetSelectedProperty(key string, value any) error {
	return e.EditSelected("Set property "+key+" on selected objects", func(id string) error {
		return e.SetProperty(id, key, value)
	})
}

// SetSelectedComponentField sets componentName.field to value on every selected object
// in one undo step, so one Undo restores each object's own prior value.
func (e *Editor) SetSelectedComponentField(componentName, field string, value any) error {
	return e.EditSelected("Set "+componentName+"."+field+" on selected objects", func(id string) error {
		return e.SetComponentField(id, componentName, field, value)
	})
}

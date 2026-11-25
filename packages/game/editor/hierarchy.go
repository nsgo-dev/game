package editor

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// objectLoc is where an object sits in a scene document: the object, its layer's index, its parent (nil for a top-level object), its index among its siblings and its depth (1 for a top-level object).
type objectLoc struct {
	obj    *sceneformat.ObjectDoc
	layer  int
	parent *sceneformat.ObjectDoc
	index  int
	depth  int
}

// locateObject finds the object id anywhere in doc's layers, nested children included.
func locateObject(doc *sceneformat.SceneDoc, id string) objectLoc {
	if doc != nil && id != "" {
		for li := range doc.Layers {
			if loc, ok := locateIn(doc.Layers[li].Objects, nil, id, 1); ok {
				loc.layer = li
				return loc
			}
		}
	}
	return objectLoc{layer: -1, index: -1}
}

func locateIn(objs []sceneformat.ObjectDoc, parent *sceneformat.ObjectDoc, id string, depth int) (objectLoc, bool) {
	for i := range objs {
		o := &objs[i]
		if o.ID == id {
			return objectLoc{obj: o, parent: parent, index: i, depth: depth}, true
		}
		if loc, ok := locateIn(o.Children, o, id, depth+1); ok {
			return loc, true
		}
	}
	return objectLoc{}, false
}

// objectPath returns the chain of objects from the top-level ancestor of id down to id itself, or nil when doc does not hold id.
func objectPath(doc *sceneformat.SceneDoc, id string) []*sceneformat.ObjectDoc {
	if doc == nil || id == "" {
		return nil
	}
	var path []*sceneformat.ObjectDoc
	var find func(objs []sceneformat.ObjectDoc) bool
	find = func(objs []sceneformat.ObjectDoc) bool {
		for i := range objs {
			path = append(path, &objs[i])
			if objs[i].ID == id || find(objs[i].Children) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	for li := range doc.Layers {
		if find(doc.Layers[li].Objects) {
			return path
		}
	}
	return nil
}

// objectIndexEntry is where an object of the open scene sits: its location and its path from its top-level ancestor down to it.
type objectIndexEntry struct {
	loc  objectLoc
	path []*sceneformat.ObjectDoc
}

// objectIndex maps every object ID of a document to where it sits, as of one revision of the editor.
type objectIndex struct {
	rev  uint64
	doc  *sceneformat.SceneDoc
	objs map[string]objectIndexEntry
}

// lookupObject finds the object id as locateObject and objectPath do, from an index of the open scene built once per revision, so a view that looks up each of its objects on every change, as the scene tree's rows do, stays linear in their number.
func (e *Editor) lookupObject(id string) objectIndexEntry {
	ix := &e.objIndex
	fresh := false
	if ix.objs == nil || ix.rev != e.revision || ix.doc != e.Doc {
		e.rebuildObjectIndex()
		fresh = true
	}
	entry, ok := ix.objs[id]
	if ok && entry.loc.obj.ID != id && !fresh {
		// The document changed within this revision, before the editor announced it, and moved the objects the index points at.
		e.rebuildObjectIndex()
		entry, ok = ix.objs[id]
	}
	if !ok || entry.loc.obj.ID != id {
		return objectIndexEntry{loc: objectLoc{layer: -1, index: -1}}
	}
	return entry
}

func (e *Editor) rebuildObjectIndex() {
	ix := &e.objIndex
	ix.rev, ix.doc, ix.objs = e.revision, e.Doc, make(map[string]objectIndexEntry)
	if e.Doc == nil {
		return
	}
	var path []*sceneformat.ObjectDoc
	var walk func(objs []sceneformat.ObjectDoc, li int, parent *sceneformat.ObjectDoc)
	walk = func(objs []sceneformat.ObjectDoc, li int, parent *sceneformat.ObjectDoc) {
		for i := range objs {
			o := &objs[i]
			path = append(path, o)
			// The first object with an ID wins, as locateObject finds it.
			if _, dup := ix.objs[o.ID]; !dup {
				ix.objs[o.ID] = objectIndexEntry{loc: objectLoc{obj: o, layer: li, parent: parent, index: i, depth: len(path)}, path: slices.Clone(path)}
			}
			walk(o.Children, li, o)
			path = path[:len(path)-1]
		}
	}
	for li := range e.Doc.Layers {
		walk(e.Doc.Layers[li].Objects, li, nil)
	}
}

// objectSlot names a place for an object: the top level of a layer when parent is empty, or the children of the object parent, at index among them.
type objectSlot struct {
	layer  string
	parent string
	index  int
}

// siblingsAt returns the slice that holds the objects of slot's place in doc, or nil when doc has no such layer or parent.
func siblingsAt(doc *sceneformat.SceneDoc, layer, parent string) *[]sceneformat.ObjectDoc {
	if doc == nil {
		return nil
	}
	if parent != "" {
		if loc := locateObject(doc, parent); loc.obj != nil {
			return &loc.obj.Children
		}
		return nil
	}
	for li := range doc.Layers {
		if doc.Layers[li].Name == layer {
			return &doc.Layers[li].Objects
		}
	}
	return nil
}

// takeObject removes the object id, with its descendants, from doc and returns it.
func takeObject(doc *sceneformat.SceneDoc, id string) (sceneformat.ObjectDoc, bool) {
	loc := locateObject(doc, id)
	if loc.obj == nil {
		return sceneformat.ObjectDoc{}, false
	}
	var objs *[]sceneformat.ObjectDoc
	if loc.parent != nil {
		objs = &loc.parent.Children
	} else {
		objs = &doc.Layers[loc.layer].Objects
	}
	o := (*objs)[loc.index]
	*objs = slices.Delete(*objs, loc.index, loc.index+1)
	if len(*objs) == 0 {
		*objs = nil
	}
	return o, true
}

// insertObject puts o at slot in doc, at the end when slot's index is past it, and reports whether doc has slot's place.
func insertObject(doc *sceneformat.SceneDoc, slot objectSlot, o sceneformat.ObjectDoc) bool {
	objs := siblingsAt(doc, slot.layer, slot.parent)
	if objs == nil {
		return false
	}
	at := slot.index
	if at < 0 || at > len(*objs) {
		at = len(*objs)
	}
	*objs = slices.Insert(*objs, at, o)
	return true
}

// moveObjectIn moves the object id of doc, with its descendants, to slot and gives it transform t.
func moveObjectIn(doc *sceneformat.SceneDoc, id string, slot objectSlot, t objectTransform) bool {
	o, ok := takeObject(doc, id)
	if !ok {
		return false
	}
	t.apply(&o)
	return insertObject(doc, slot, o)
}

// checkTransformEdit reports whether the tree that holds the object id would still be valid (sceneformat.CheckObjectTree), from its top-level ancestor down, after edit changes id's fields: a nested child's sprite is sheared by its ancestors' scales and rotations, so checking the object's own subtree is not enough.
func (e *Editor) checkTransformEdit(id string, edit func(o *sceneformat.ObjectDoc)) error {
	path := objectPath(e.Doc, id)
	if path == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	tree := []sceneformat.ObjectDoc{path[0].Clone()}
	loc, _ := locateIn(tree, nil, id, 1)
	edit(loc.obj)
	return sceneformat.CheckObjectTree(&tree[0])
}

// subtreeHeight returns how many levels the tree rooted at o spans: 1 for an object without children.
func subtreeHeight(o *sceneformat.ObjectDoc) int {
	h := 0
	for i := range o.Children {
		h = max(h, subtreeHeight(&o.Children[i]))
	}
	return h + 1
}

// objectTransform is every field of an object's 2D and 3D transform, with nil scales kept so that an undo writes back the same YAML.
type objectTransform struct {
	Transform3D
	Rotation float32
}

func captureTransform(o *sceneformat.ObjectDoc) objectTransform {
	return objectTransform{Transform3D: captureTransform3D(o), Rotation: o.Rotation}
}

func (t objectTransform) apply(o *sceneformat.ObjectDoc) {
	applyTransform3D(o, t.Transform3D)
	o.Rotation = t.Rotation
}

// ReparentObject moves the object id, with its descendants, under the object parentID at index among its children, or to the top level of its own layer at index when parentID is empty, as one undoable command; an index below 0 or past the last sibling puts it last. A child belongs to its parent's layer, so moving under an object of another layer moves it to that layer. The object keeps its world transform: its local transform is recomputed under the new parent in 2D, its depth too when the object or an old or new ancestor sets a 3D field, and its whole 3D world transform instead when one of them sets a 3D rotation (see keepWorldTransform); Undo restores the fields exactly. It refuses to move an object under itself or one of its descendants, a move that would nest deeper than sceneformat.MaxObjectDepth, and a move whose new world transform could not be written as a local one or would shear a sprite.
func (e *Editor) ReparentObject(id, parentID string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if parentID == "" {
		return e.relocate(id, objectSlot{layer: e.Doc.Layers[loc.layer].Name, index: index}, "Unparent "+id)
	}
	ploc := locateObject(e.Doc, parentID)
	if ploc.obj == nil {
		return fmt.Errorf("editor: parent object %q not found", parentID)
	}
	return e.relocate(id, objectSlot{layer: e.Doc.Layers[ploc.layer].Name, parent: parentID, index: index}, "Reparent "+id+" under "+parentID)
}

// MoveObjectToLayer moves the object id, with its descendants, to the top level of layer at index among its objects, as one undoable command; an index below 0 or past the last object puts it last. The object keeps its world transform within the layer's coordinate space, as ReparentObject does: a child taken out of its parent gets the local transform that leaves it where it was drawn. A move between a world and a screen layer keeps the numbers, not where the object appears on screen.
func (e *Editor) MoveObjectToLayer(id, layer string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.layerIndex(layer) < 0 {
		return fmt.Errorf("editor: layer %q not found", layer)
	}
	return e.relocate(id, objectSlot{layer: layer, index: index}, "Move "+id+" to layer "+layer)
}

// ReorderObject moves the object id to index among its siblings (the children of its parent, or its layer's top-level objects), as one undoable command; an index below 0 or past the last sibling puts it last. Its transform does not change.
func (e *Editor) ReorderObject(id string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	slot := objectSlot{layer: e.Doc.Layers[loc.layer].Name, index: index}
	if loc.parent != nil {
		slot.parent = loc.parent.ID
	}
	return e.relocate(id, slot, "Reorder "+id)
}

// relocate checks and executes the move of the object id to slot.
func (e *Editor) relocate(id string, to objectSlot, label string) error {
	loc := locateObject(e.Doc, id)
	if loc.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	from := objectSlot{layer: e.Doc.Layers[loc.layer].Name, index: loc.index}
	if loc.parent != nil {
		from.parent = loc.parent.ID
	}
	depth := 1
	var newAncestors []*sceneformat.ObjectDoc
	if to.parent != "" {
		newAncestors = objectPath(e.Doc, to.parent)
		if newAncestors == nil {
			return fmt.Errorf("editor: parent object %q not found", to.parent)
		}
		if slices.ContainsFunc(newAncestors, func(o *sceneformat.ObjectDoc) bool { return o.ID == id }) {
			return fmt.Errorf("editor: cannot move object %q under %q, which is itself or one of its descendants", id, to.parent)
		}
		depth = len(newAncestors) + 1
	}
	if levels := depth - 1 + subtreeHeight(loc.obj); levels > sceneformat.MaxObjectDepth {
		return fmt.Errorf("editor: moving object %q under %q would nest %d levels, deeper than %d", id, to.parent, levels, sceneformat.MaxObjectDepth)
	}
	siblings := siblingsAt(e.Doc, to.layer, to.parent)
	if siblings == nil {
		return fmt.Errorf("editor: layer %q not found", to.layer)
	}
	same := from.layer == to.layer && from.parent == to.parent
	n := len(*siblings)
	if same {
		n--
	}
	if to.index < 0 || to.index > n {
		to.index = n
	}
	if same && to.index == from.index {
		return nil
	}
	before := captureTransform(loc.obj)
	after := before
	if from.parent != to.parent {
		oldPath := objectPath(e.Doc, id)
		kept, err := keepWorldTransform(oldPath[:len(oldPath)-1], newAncestors, loc.obj)
		if err != nil {
			return fmt.Errorf("editor: cannot move object %q under %q: %w", id, to.parent, err)
		}
		after = kept
	}
	candidate := e.Doc.Clone()
	if !moveObjectIn(candidate, id, to, after) {
		return fmt.Errorf("editor: cannot move object %q", id)
	}
	if err := sceneformat.Validate(candidate); err != nil {
		return err
	}
	e.CancelGizmoDrag3D()
	e.History.Execute(&relocateCmd{ed: e, id: id, label: label, from: from, to: to, before: before, after: after})
	return nil
}

// relocateCmd moves an object between two places in the scene, giving it a transform for each.
type relocateCmd struct {
	ed            *Editor
	id            string
	label         string
	from, to      objectSlot
	before, after objectTransform
}

func (c *relocateCmd) Label() string { return c.label }

func (c *relocateCmd) Do() {
	if moveObjectIn(c.ed.Doc, c.id, c.to, c.after) {
		c.ed.Status = c.label
	}
}

func (c *relocateCmd) Undo() { moveObjectIn(c.ed.Doc, c.id, c.from, c.before) }

// keepWorldTransform returns the transform that gives o, now under oldAncestors (top-level first, empty for a top-level object), the same world transform under newAncestors. The 2D world transform (sceneformat.ObjectDoc.LocalTransform) is kept whenever it can be: X, Y, the 2D rotation and the X and Y scales come from it. When one of these objects sets a 3D field, the depth (PosZ, ScaleZ) is kept as well, from the 3D world transform that composes position, Euler rotation and scale the way sceneformat.ObjectLocalTransform3D does. When one of them sets a 3D rotation, the 2D and 3D world transforms no longer agree on where X and Y lead, and the 3D one is kept in full, with the 2D rotation kept as far as it can be.
func keepWorldTransform(oldAncestors, newAncestors []*sceneformat.ObjectDoc, o *sceneformat.ObjectDoc) (objectTransform, error) {
	t := captureTransform(o)
	all := slices.Concat(oldAncestors, newAncestors, []*sceneformat.ObjectDoc{o})
	uses3D := slices.ContainsFunc(all, uses3DFields)
	rotates3D := slices.ContainsFunc(all, func(a *sceneformat.ObjectDoc) bool { return a.RotX != 0 || a.RotY != 0 || a.RotZ != 0 })

	world2 := chain2(oldAncestors).mul(local2(o))
	inv2, ok2 := chain2(newAncestors).invert()
	var x2, y2, rot2, sx2, sy2 float64
	if ok2 {
		x2, y2, rot2, sx2, sy2, ok2 = inv2.mul(world2).decompose(o.EffectiveScaleX() < 0, o.EffectiveScaleY() < 0)
	}
	if !rotates3D {
		if !ok2 {
			return t, errNoLocalTransform
		}
		t.X, t.Y, t.Rotation = tidy(x2), tidy(y2), tidy(rot2)
		t.ScaleX, t.ScaleY = tidyScale(t.ScaleX, sx2), tidyScale(t.ScaleY, sy2)
	}
	if !uses3D {
		return t, nil
	}

	inv3, ok := chain3(newAncestors).invert()
	if !ok {
		return t, errNoLocalTransform
	}
	local := inv3.mul(chain3(oldAncestors).mul(local3(o)))
	if !rotates3D {
		// Without a 3D rotation the depth axis is never mixed with X and Y, so the local depth and its scale, sign included, are read straight off the matrix.
		if math.Abs(local[2][2]) < 1e-12 {
			return t, errNoLocalTransform
		}
		t.PosZ, t.ScaleZ = tidy(local[2][3]), tidyScale(t.ScaleZ, local[2][2])
		return t, nil
	}
	mirrored := [3]bool{o.EffectiveScaleX() < 0, o.EffectiveScaleY() < 0, o.EffectiveScaleZ() < 0}
	pos, rot, scale, ok := local.decompose(mirrored)
	if !ok {
		return t, errNoLocalTransform
	}
	t.X, t.Y, t.PosZ = tidy(pos[0]), tidy(pos[1]), tidy(pos[2])
	t.RotX, t.RotY, t.RotZ = tidy(rot[0]), tidy(rot[1]), tidy(rot[2])
	t.ScaleX, t.ScaleY, t.ScaleZ = tidyScale(t.ScaleX, scale[0]), tidyScale(t.ScaleY, scale[1]), tidyScale(t.ScaleZ, scale[2])
	if ok2 {
		t.Rotation = tidy(rot2)
	}
	return t, nil
}

var errNoLocalTransform = errors.New("its world transform cannot be written as a translation, rotation and scale under the new parent (a parent scaled to zero, or a rotated object under a non-uniformly scaled parent)")

// uses3DFields reports whether o sets a transform field only the 3D view reads.
func uses3DFields(o *sceneformat.ObjectDoc) bool {
	return o.PosZ != 0 || o.RotX != 0 || o.RotY != 0 || o.RotZ != 0 || (o.ScaleZ != nil && *o.ScaleZ != 1)
}

// tidy rounds v to float32, snapping it to four decimal places when it lies within float32's error of them, so a reparented object's fields read as written rather than as rounding noise.
func tidy(v float64) float32 {
	r := math.Round(v*1e4) / 1e4
	if math.Abs(v-r) <= 1e-6*math.Max(1, math.Abs(v)) {
		v = r
	}
	if v == 0 {
		return 0 // no negative zero
	}
	return float32(v)
}

// tidyScale returns scale s as a field that was old: nil when old was nil and s is 1, so a move that keeps the scale leaves the field unset.
func tidyScale(old *float32, s float64) *float32 {
	v := tidy(s)
	if old == nil && v == 1 {
		return nil
	}
	return &v
}

// aff2 is a 2D affine transform in float64, laid out as math2d.Affine: x' = a*x + c*y + tx, y' = b*x + d*y + ty.
type aff2 struct{ a, b, c, d, tx, ty float64 }

var ident2 = aff2{a: 1, d: 1}

func local2(o *sceneformat.ObjectDoc) aff2 {
	sn, cs := math.Sincos(float64(o.Rotation))
	sx, sy := float64(o.EffectiveScaleX()), float64(o.EffectiveScaleY())
	return aff2{a: cs * sx, b: sn * sx, c: -sn * sy, d: cs * sy, tx: float64(o.X), ty: float64(o.Y)}
}

// chain2 composes the local 2D transforms of path, top-level first.
func chain2(path []*sceneformat.ObjectDoc) aff2 {
	m := ident2
	for _, o := range path {
		m = m.mul(local2(o))
	}
	return m
}

func (m aff2) mul(n aff2) aff2 {
	return aff2{
		a:  m.a*n.a + m.c*n.b,
		b:  m.b*n.a + m.d*n.b,
		c:  m.a*n.c + m.c*n.d,
		d:  m.b*n.c + m.d*n.d,
		tx: m.a*n.tx + m.c*n.ty + m.tx,
		ty: m.b*n.tx + m.d*n.ty + m.ty,
	}
}

func (m aff2) invert() (aff2, bool) {
	det := m.a*m.d - m.b*m.c
	if math.Abs(det) < 1e-12 || math.IsNaN(det) || math.IsInf(det, 0) {
		return aff2{}, false
	}
	return aff2{
		a:  m.d / det,
		b:  -m.b / det,
		c:  -m.c / det,
		d:  m.a / det,
		tx: (m.c*m.ty - m.d*m.tx) / det,
		ty: (m.b*m.tx - m.a*m.ty) / det,
	}, true
}

func (m aff2) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.tx, m.b*x + m.d*y + m.ty
}

// decompose splits m into a translation, a rotation and axis scales whose TRS is m, as sceneformat does for a child's sprite, and reports false for a shear or a collapsed axis. negX and negY say which scales the object had negative before: the signs are put back where they were when m allows it (a mirror on the y scale otherwise), so a flipped object keeps the fields it was written with.
func (m aff2) decompose(negX, negY bool) (x, y, rot, sx, sy float64, ok bool) {
	sx = math.Hypot(m.a, m.b)
	sy = math.Hypot(m.c, m.d)
	if sx < 1e-12 || sy < 1e-12 || math.Abs(m.a*m.c+m.b*m.d) > 1e-6*sx*sy {
		return 0, 0, 0, 0, 0, false
	}
	mirrored := m.a*m.d-m.b*m.c < 0
	// A negative sx turns the rotation's x axis, the first column divided by sx, the other way.
	switch {
	case mirrored && negX && !negY, !mirrored && negX && negY:
		sx = -sx
		if !mirrored {
			sy = -sy
		}
		return m.tx, m.ty, math.Atan2(-m.b, -m.a), sx, sy, true
	case mirrored:
		sy = -sy
	}
	return m.tx, m.ty, math.Atan2(m.b, m.a), sx, sy, true
}

// aff3 is a 3D affine transform in float64: three rows of a 3x4 matrix mapping (x, y, z, 1).
type aff3 [3][4]float64

var ident3 = aff3{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}}

// local3 is o's 3D transform relative to its parent, translate (X, Y, PosZ) times the rotation Rz * Ry * Rx times scale, as the 3D viewport and sceneformat.ObjectLocalTransform3D compose it.
func local3(o *sceneformat.ObjectDoc) aff3 {
	rx, ry, rz := float64(o.RotX), float64(o.RotY), float64(o.RotZ)
	scale := [3]float64{float64(o.EffectiveScaleX()), float64(o.EffectiveScaleY()), float64(o.EffectiveScaleZ())}
	var m aff3
	for col := range 3 {
		var axis [3]float64
		axis[col] = 1
		r := rotateEulerXYZ64(axis, rx, ry, rz)
		for row := range 3 {
			m[row][col] = r[row] * scale[col]
		}
	}
	m[0][3], m[1][3], m[2][3] = float64(o.X), float64(o.Y), float64(o.PosZ)
	return m
}

// chain3 composes the local 3D transforms of path, top-level first.
func chain3(path []*sceneformat.ObjectDoc) aff3 {
	m := ident3
	for _, o := range path {
		m = m.mul(local3(o))
	}
	return m
}

func (m aff3) mul(n aff3) aff3 {
	var out aff3
	for r := range 3 {
		for c := range 4 {
			v := m[r][0]*n[0][c] + m[r][1]*n[1][c] + m[r][2]*n[2][c]
			if c == 3 {
				v += m[r][3]
			}
			out[r][c] = v
		}
	}
	return out
}

func (m aff3) invert() (aff3, bool) {
	a := m
	det := a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) - a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) + a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
	if math.Abs(det) < 1e-12 || math.IsNaN(det) || math.IsInf(det, 0) {
		return aff3{}, false
	}
	var inv aff3
	inv[0][0] = (a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det
	inv[0][1] = (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det
	inv[0][2] = (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det
	inv[1][0] = (a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det
	inv[1][1] = (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det
	inv[1][2] = (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det
	inv[2][0] = (a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det
	inv[2][1] = (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det
	inv[2][2] = (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det
	for r := range 3 {
		inv[r][3] = -(inv[r][0]*a[0][3] + inv[r][1]*a[1][3] + inv[r][2]*a[2][3])
	}
	return inv, true
}

// point applies m to p.
func (m aff3) point(p [3]float64) [3]float64 {
	var out [3]float64
	for r := range 3 {
		out[r] = m[r][0]*p[0] + m[r][1]*p[1] + m[r][2]*p[2] + m[r][3]
	}
	return out
}

// vector applies m's linear part to v, leaving out the translation.
func (m aff3) vector(v [3]float64) [3]float64 {
	var out [3]float64
	for r := range 3 {
		out[r] = m[r][0]*v[0] + m[r][1]*v[1] + m[r][2]*v[2]
	}
	return out
}

// decompose splits m into a translation, Euler angles (x, y, z) such that the rotation is Rz * Ry * Rx, and axis scales, and reports false for a shear or a collapsed axis. A mirror goes on the first axis marked in mirrored (the axes the object was flipped on before), or on the x scale when none is, so a flipped object keeps the fields it was written with.
func (m aff3) decompose(mirrored [3]bool) (pos, rot, scale [3]float64, ok bool) {
	pos = [3]float64{m[0][3], m[1][3], m[2][3]}
	var cols [3][3]float64
	for c := range 3 {
		cols[c] = [3]float64{m[0][c], m[1][c], m[2][c]}
		scale[c] = len64(cols[c])
		if scale[c] < 1e-12 {
			return pos, rot, scale, false
		}
	}
	if dot64(cols[0], cross64(cols[1], cols[2])) < 0 {
		axis := max(slices.Index(mirrored[:], true), 0)
		scale[axis] = -scale[axis]
	}
	var r [3][3]float64 // r[row][col]
	for c := range 3 {
		for row := range 3 {
			r[row][c] = cols[c][row] / scale[c]
		}
	}
	const orthoEps = 1e-6
	col := func(c int) [3]float64 { return [3]float64{r[0][c], r[1][c], r[2][c]} }
	if math.Abs(dot64(col(0), col(1))) > orthoEps || math.Abs(dot64(col(0), col(2))) > orthoEps || math.Abs(dot64(col(1), col(2))) > orthoEps {
		return pos, rot, scale, false
	}
	sy := math.Max(-1, math.Min(1, -r[2][0]))
	rot[1] = math.Asin(sy)
	if math.Hypot(r[2][1], r[2][2]) > 1e-9 {
		rot[0] = math.Atan2(r[2][1], r[2][2])
		rot[2] = math.Atan2(r[1][0], r[0][0])
	} else {
		rot[0] = math.Atan2(-r[1][2], r[1][1])
	}
	return pos, rot, scale, true
}

// SetObjectID changes the ID of the object oldID to newID as one undoable command, rewriting in the same step every connection (sceneformat.ConnectionDoc) and scene override (sceneformat.SceneDoc.Overrides) that names it, and, when the object instances a scene, the objects that scene adds ("enemy_1/body" becomes "boss/body"). The selection, the open clip's target and the targets of its tracks, and the object's hidden and locked toggles follow it. It refuses an empty ID and one any object of the scene already has, including one an instanced or inherited scene adds, and an ID under which the scene would no longer resolve.
func (e *Editor) SetObjectID(oldID, newID string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(oldID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", oldID)
	}
	if newID == oldID {
		return nil
	}
	if newID == "" {
		return errors.New("editor: object id must not be empty")
	}
	if e.objectIDInUse(newID) {
		return fmt.Errorf("editor: duplicate object id %q", newID)
	}
	cmd := &objectIDCmd{ed: e, oldID: oldID, newID: newID, instancing: obj.Scene != ""}
	for i, conn := range e.Doc.Connections {
		from, fromOK := renameRef(conn.From, oldID, newID, cmd.instancing)
		to, toOK := renameRef(conn.To, oldID, newID, cmd.instancing)
		if fromOK || toOK {
			cmd.conns = append(cmd.conns, connRename{index: i, oldFrom: conn.From, oldTo: conn.To, newFrom: from, newTo: to})
		}
	}
	if e.clip != nil {
		for i, tr := range e.clip.Tracks {
			if _, ok := renameRef(tr.Target, oldID, newID, cmd.instancing); ok {
				cmd.tracks = append(cmd.tracks, i)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(e.Doc.Overrides)) {
		if renamed, ok := renameRef(key, oldID, newID, cmd.instancing); ok {
			cmd.overrides = append(cmd.overrides, [2]string{key, renamed})
		}
	}
	candidate := e.Doc.Clone()
	cmd.apply(candidate, false)
	if err := sceneformat.Validate(candidate); err != nil {
		return err
	}
	if candidate.RefersToScenes() {
		if scenes, ok := e.Resolver.(sceneformat.SceneResolver); ok {
			if _, err := sceneformat.Resolve(candidate, scenes); err != nil {
				return err
			}
		}
	}
	e.CancelGizmoDrag3D()
	e.History.Execute(cmd)
	return nil
}

// renameRef returns ref with the object ID oldID in it replaced by newID: ref itself when it is oldID, or the object of a scene oldID instances ("old/body") when instancing.
func renameRef(ref, oldID, newID string, instancing bool) (string, bool) {
	if ref == oldID {
		return newID, true
	}
	if instancing && strings.HasPrefix(ref, oldID+"/") {
		return newID + ref[len(oldID):], true
	}
	return ref, false
}

// connRename is a connection whose ends an ID change rewrote, with its index in the scene's connections.
type connRename struct {
	index                          int
	oldFrom, oldTo, newFrom, newTo string
}

type objectIDCmd struct {
	ed           *Editor
	oldID, newID string
	instancing   bool
	conns        []connRename
	// overrides are the scene override keys the change renames, each as {old, new}.
	overrides [][2]string
	// tracks are the indexes of the open clip's tracks whose target names the object; History being linear, the clip holds the same tracks whenever the command is done or undone.
	tracks []int
}

func (c *objectIDCmd) Label() string { return "Change ID " + c.oldID + " to " + c.newID }

// apply makes the change in doc, or reverts it when undo is set.
func (c *objectIDCmd) apply(doc *sceneformat.SceneDoc, undo bool) {
	from, to := c.oldID, c.newID
	if undo {
		from, to = to, from
	}
	if loc := locateObject(doc, from); loc.obj != nil {
		loc.obj.ID = to
	}
	for _, r := range c.conns {
		if r.index >= len(doc.Connections) {
			continue
		}
		conn := &doc.Connections[r.index]
		if undo {
			conn.From, conn.To = r.oldFrom, r.oldTo
		} else {
			conn.From, conn.To = r.newFrom, r.newTo
		}
	}
	for _, kv := range c.overrides {
		k0, k1 := kv[0], kv[1]
		if undo {
			k0, k1 = k1, k0
		}
		if ov, ok := doc.Overrides[k0]; ok {
			delete(doc.Overrides, k0)
			doc.Overrides[k1] = ov
		}
	}
}

// rename moves the editor's own state that names the object from one ID to the other.
func (c *objectIDCmd) rename(from, to string) {
	e := c.ed
	if e.SelectedID == from {
		e.SelectedID = to
	}
	if e.clipMeta.target == from {
		e.clipMeta.target = to
	}
	if e.clip != nil {
		for _, i := range c.tracks {
			if i < len(e.clip.Tracks) {
				e.clip.Tracks[i].Target, _ = renameRef(e.clip.Tracks[i].Target, from, to, c.instancing)
			}
		}
	}
	for _, flags := range []map[string]bool{e.hidden, e.locked} {
		if flags[from] {
			delete(flags, from)
			flags[to] = true
		}
	}
}

func (c *objectIDCmd) Do() {
	c.apply(c.ed.Doc, false)
	c.rename(c.oldID, c.newID)
	c.ed.Status = "Changed ID " + c.oldID + " to " + c.newID
}

func (c *objectIDCmd) Undo() {
	c.apply(c.ed.Doc, true)
	c.rename(c.newID, c.oldID)
}

// SetObjectHidden hides the object id, and with it its descendants, in the editor's 2D and 3D viewports, or shows it again. The toggle belongs to the editing session, like the camera of the 3D view: it is not an edit, is not undone, and is never written to the scene file, and Play draws every object. A hidden object cannot be picked or manipulated in a viewport either (no gizmo, no Move-tool drag), though the scene tree still selects it and the inspector edits it.
func (e *Editor) SetObjectHidden(id string, hidden bool) error {
	return e.setViewFlag(&e.hidden, id, hidden)
}

// ObjectHidden reports whether the object id itself is hidden in the viewports (SetObjectHidden); a descendant of a hidden object is hidden too without being marked.
func (e *Editor) ObjectHidden(id string) bool { return e.hidden[id] }

// SetObjectLocked locks the object id, and with it its descendants, against picking in the editor's 2D and 3D viewports, or unlocks it, so a click goes to what lies under it, and against manipulation there: its gizmo is not shown and the Move tool does not move it. Like SetObjectHidden, the toggle belongs to the editing session and is never written to the scene file. A locked object can still be selected and edited from the scene tree and the inspector.
func (e *Editor) SetObjectLocked(id string, locked bool) error {
	return e.setViewFlag(&e.locked, id, locked)
}

// ObjectLocked reports whether the object id itself is locked (SetObjectLocked); a descendant of a locked object is locked too without being marked.
func (e *Editor) ObjectLocked(id string) bool { return e.locked[id] }

func (e *Editor) setViewFlag(flags *map[string]bool, id string, on bool) error {
	if obj, _, _ := e.FindObject(id); obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	if (*flags)[id] == on {
		return nil
	}
	if on {
		if *flags == nil {
			*flags = make(map[string]bool)
		}
		(*flags)[id] = true
	} else {
		delete(*flags, id)
	}
	e.notify()
	return nil
}

// viewFlagged reports whether bo or one of its ancestors is marked in flags.
func viewFlagged(flags map[string]bool, bo *sceneformat.BuiltObject) bool {
	if len(flags) == 0 {
		return false
	}
	for ; bo != nil; bo = bo.Parent {
		if flags[bo.ID] {
			return true
		}
	}
	return false
}

// viewportFrozen reports whether the object id, or one of its ancestors, is hidden or locked in the editor, so the viewports neither pick nor manipulate it: no gizmo handle, no Move-tool drag. The scene tree and the inspector still edit it.
func (e *Editor) viewportFrozen(id string) bool {
	if len(e.hidden) == 0 && len(e.locked) == 0 {
		return false
	}
	path := objectPath(e.Doc, id)
	return pathFlagged(e.hidden, path) || pathFlagged(e.locked, path)
}

// pathFlagged reports whether an object of path is marked in flags.
func pathFlagged(flags map[string]bool, path []*sceneformat.ObjectDoc) bool {
	if len(flags) == 0 {
		return false
	}
	return slices.ContainsFunc(path, func(o *sceneformat.ObjectDoc) bool { return flags[o.ID] })
}

// DropPlace says where DropObject puts an object relative to the object it is dropped on.
type DropPlace int

const (
	// DropInto makes the object the last child of the target.
	DropInto DropPlace = iota
	// DropBefore makes the object the target's sibling just before it.
	DropBefore
	// DropAfter makes the object the target's sibling just after it.
	DropAfter
)

// DropObject moves the object id, with its descendants, into the object targetID or beside it as place says, as one undoable command: what dragging a row of the scene tree onto another does. It is ReparentObject with the parent and index place names, so the object keeps its world transform and the same moves are refused.
func (e *Editor) DropObject(id, targetID string, place DropPlace) error {
	if e.playing {
		return ErrPlaying
	}
	if id == targetID {
		return nil
	}
	if place == DropInto {
		return e.ReparentObject(id, targetID, -1)
	}
	src := locateObject(e.Doc, id)
	if src.obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	dst := locateObject(e.Doc, targetID)
	if dst.obj == nil {
		return fmt.Errorf("editor: object %q not found", targetID)
	}
	index := dst.index
	if place == DropAfter {
		index++
	}
	// Taking the object out first shifts the later siblings of the same parent up by one.
	if src.layer == dst.layer && src.parent == dst.parent && src.index < dst.index {
		index--
	}
	if dst.parent != nil {
		return e.relocate(id, objectSlot{layer: e.Doc.Layers[dst.layer].Name, parent: dst.parent.ID, index: index}, "Move "+id)
	}
	return e.relocate(id, objectSlot{layer: e.Doc.Layers[dst.layer].Name, index: index}, "Move "+id)
}

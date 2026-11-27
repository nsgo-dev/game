package entities

import (
	"errors"
	"math"
)

var (
	// ErrCycle reports a SetParent call that would create a cycle in the hierarchy, including parenting an entity to itself.
	ErrCycle = errors.New("entities: hierarchy cycle")
	// ErrDeadEntity reports a hierarchy operation attempted on Zero or an entity that is not alive in the World.
	ErrDeadEntity = errors.New("entities: dead entity")
	// ErrNotDecomposable reports a keep-world reparent whose exact relative
	// transform contains shear and cannot be stored as a Transform2D.
	ErrNotDecomposable = errors.New("entities: relative transform is not decomposable to TRS")
)

// Transform2D is an entity's local 2D transform relative to its parent (or to world space when the entity is a root).
//
// The zero value of Transform2D is visible (Hidden == false) with ScaleX == 0 and ScaleY == 0 (collapsed scale), matching game/sprite.Sprite.
// Use IdentityTransform2D or NewTransform2D to construct a visible transform with unit scale (1, 1).
type Transform2D struct {
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
	Rotation float32 `json:"rotation,omitempty"`
	ScaleX   float32 `json:"scaleX"`
	ScaleY   float32 `json:"scaleY"`
	Hidden   bool    `json:"hidden,omitempty"`
}

// IdentityTransform2D returns a visible Transform2D at (0, 0) with zero rotation and unit scale (1, 1).
func IdentityTransform2D() Transform2D {
	return Transform2D{ScaleX: 1, ScaleY: 1}
}

// NewTransform2D returns a visible Transform2D at (x, y) with zero rotation and unit scale (1, 1).
func NewTransform2D(x, y float32) Transform2D {
	return Transform2D{X: x, Y: y, ScaleX: 1, ScaleY: 1}
}

// Visible reports whether this local transform is visible (!t.Hidden).
func (t Transform2D) Visible() bool { return !t.Hidden }

// SetVisible sets whether this local transform is visible.
func (t *Transform2D) SetVisible(visible bool) { t.Hidden = !visible }

// Matrix returns the local 2D affine matrix [a, b, c, d, tx, ty] mapping local points to parent space:
//
//	px = a*lx + c*ly + tx
//	py = b*lx + d*ly + ty
//
// The layout matches nsgo/core.Drawing.Transform and game/sprite.
func (t Transform2D) Matrix() [6]float32 {
	if t.Rotation == 0 {
		return [6]float32{t.ScaleX, 0, 0, t.ScaleY, t.X, t.Y}
	}
	sn, cs := math.Sincos(float64(t.Rotation))
	sin, cos := float32(sn), float32(cs)
	return [6]float32{
		cos * t.ScaleX,
		sin * t.ScaleX,
		-sin * t.ScaleY,
		cos * t.ScaleY,
		t.X,
		t.Y,
	}
}

// WorldTransform2D is the cached world-space 2D transform composed from an entity's ancestor chain.
//
// Matrix holds the exact 2D affine transform [a, b, c, d, tx, ty] mapping local coordinates to world space.
// X, Y, Rotation, ScaleX and ScaleY hold the decomposed world position, orientation (radians), and axis scales.
// WorldVisible is true only when this entity and every ancestor are visible.
type WorldTransform2D struct {
	X            float32
	Y            float32
	Rotation     float32
	ScaleX       float32
	ScaleY       float32
	Matrix       [6]float32
	WorldVisible bool
}

// TransformPoint maps a point (lx, ly) in this entity's local space into world space.
func (wt WorldTransform2D) TransformPoint(lx, ly float32) (wx, wy float32) {
	m := wt.Matrix
	return m[0]*lx + m[2]*ly + m[4], m[1]*lx + m[3]*ly + m[5]
}

// InverseTransformPoint maps a point (wx, wy) in world space back into this entity's local space.
// If Matrix is singular (zero determinant), it returns (0, 0).
func (wt WorldTransform2D) InverseTransformPoint(wx, wy float32) (lx, ly float32) {
	a, b := float64(wt.Matrix[0]), float64(wt.Matrix[1])
	c, d := float64(wt.Matrix[2]), float64(wt.Matrix[3])
	det := a*d - b*c
	if math.Abs(det) < 1e-12 {
		return 0, 0
	}
	dx := float64(wx) - float64(wt.Matrix[4])
	dy := float64(wy) - float64(wt.Matrix[5])
	invDet := 1.0 / det
	return float32((d*dx - c*dy) * invDet), float32((-b*dx + a*dy) * invDet)
}

// RemovePolicy controls what happens to direct children when a parent entity is destroyed.
type RemovePolicy uint8

const (
	// RemoveDetach detaches direct children to root before destroying the parent, keeping each child's local Transform2D.
	// This is the default policy applied when World.Destroy removes a parent directly.
	RemoveDetach RemovePolicy = iota
	// RemoveDetachKeepWorld detaches direct children to root before destroying the parent, updating each child's local Transform2D to preserve its composed world transform.
	RemoveDetachKeepWorld
	// RemoveCascade recursively destroys the entity and all of its descendants.
	RemoveCascade
)

type hierarchyNode struct {
	entity      Entity
	parent      Entity
	firstChild  Entity
	lastChild   Entity
	prevSibling Entity
	nextSibling Entity
	childCount  uint32
	dirty       bool
	inDirtyList bool
}

// Hierarchy2D manages parent-child 2D transform relationships, visibility inheritance, and dirty-subtree propagation for a World.
//
// Local transforms are stored in the World's Store[Transform2D] and composed world transforms are cached in Store[WorldTransform2D], allowing ECS queries (Each1/Each2/Each3) to iterate world transforms directly with zero allocations.
// Parent-child links are maintained in an intrusive, pointer-free slice indexed by Entity.Index(), so steady-state updates, dirty propagation, reparenting, and Sync perform zero heap allocations.
//
// When World.Destroy removes a parent entity directly, Hierarchy2D automatically cleans up its links according to its configured RemovePolicy (default RemoveDetach) so dangling parent/child references never occur.
type Hierarchy2D struct {
	world         *World
	localStore    *Store[Transform2D]
	worldStore    *Store[WorldTransform2D]
	nodes         []hierarchyNode
	dirtyRoots    []Entity
	stack         []Entity
	ancestors     []Entity
	destroyBuf    []Entity
	defaultPolicy RemovePolicy
	inDestroy     bool
}

// NewHierarchy2D returns the 2D transform hierarchy bound to w, creating and attaching it on first call.
// It returns nil if w is nil.
func NewHierarchy2D(w *World) *Hierarchy2D {
	if w == nil {
		return nil
	}
	if w.hierarchy != nil {
		return w.hierarchy
	}
	h := &Hierarchy2D{
		world:         w,
		localStore:    ensureStore[Transform2D](w),
		worldStore:    ensureStore[WorldTransform2D](w),
		defaultPolicy: RemoveDetach,
	}
	w.hierarchy = h
	h.seedFromLocalStore()
	return h
}

// Hierarchy2D returns the World's 2D transform hierarchy, creating and attaching it on first call.
func (w *World) Hierarchy2D() *Hierarchy2D {
	return NewHierarchy2D(w)
}

// ParentOf returns e's parent entity in w's Hierarchy2D without creating a hierarchy when none is attached.
func (w *World) ParentOf(e Entity) (Entity, bool) {
	if w == nil || w.hierarchy == nil {
		return Zero, false
	}
	return w.hierarchy.Parent(e)
}

// ChildrenOf returns e's direct children in w's Hierarchy2D without creating a hierarchy when none is attached.
func (w *World) ChildrenOf(e Entity) []Entity {
	if w == nil || w.hierarchy == nil {
		return nil
	}
	return w.hierarchy.Children(e)
}

// SetRemovePolicy sets the default RemovePolicy used when World.Destroy is called directly on a parent entity.
func (h *Hierarchy2D) SetRemovePolicy(policy RemovePolicy) {
	if h == nil {
		return
	}
	h.defaultPolicy = policy
}

// RemovePolicy returns the default RemovePolicy used when World.Destroy is called directly on a parent entity.
func (h *Hierarchy2D) RemovePolicy() RemovePolicy {
	if h == nil {
		return RemoveDetach
	}
	return h.defaultPolicy
}

// SetParent attaches child as the last direct child of parent in deterministic insertion order.
//
// If parent is Zero, child is detached to root (equivalent to Detach(child, keepWorld)).
// SetParent returns ErrWorldDisposed if the World is disposed, ErrDeadEntity if child (or non-Zero parent) is not alive, and ErrCycle if parenting would introduce a cycle (including child == parent).
// When keepWorld is true, child's local Transform2D is recomputed relative to parent's world transform so child stays at the same world position, rotation, and scale.
func (h *Hierarchy2D) SetParent(child, parent Entity, keepWorld bool) error {
	if h == nil || h.world == nil {
		return ErrDeadEntity
	}
	if h.world.disposed {
		return ErrWorldDisposed
	}
	if !h.world.Alive(child) {
		return ErrDeadEntity
	}
	if parent == Zero {
		h.Detach(child, keepWorld)
		return nil
	}
	if !h.world.Alive(parent) {
		return ErrDeadEntity
	}
	if child == parent {
		return ErrCycle
	}

	// Reject cycles by walking up parent's ancestor chain.
	for cur := parent; cur != Zero; {
		idx := int(cur.Index())
		if idx >= len(h.nodes) || h.nodes[idx].entity != cur {
			break
		}
		p := h.nodes[idx].parent
		if p == child {
			return ErrCycle
		}
		cur = p
	}

	// Already directly parented to parent: preserve sibling order and avoid unnecessary dirtying.
	if cidx := int(child.Index()); cidx < len(h.nodes) && h.nodes[cidx].entity == child && h.nodes[cidx].parent == parent {
		return nil
	}

	// Ensure both entities have hierarchy nodes and local transforms before mutating links.
	h.ensureTransform(parent)
	h.ensureTransform(child)
	h.ensureNode(parent)
	h.ensureNode(child)

	if keepWorld {
		cw, _ := h.resolveWorld(child)
		pw, _ := h.resolveWorld(parent)
		var hidden bool
		if lp, ok := h.localStore.Get(child); ok {
			hidden = lp.Hidden
		}
		rel, ok := relativeTransform(pw, cw, hidden)
		if !ok {
			return ErrNotDecomposable
		}
		h.localStore.Set(child, rel)
	}

	h.unlinkFromParent(child)
	h.appendChild(parent, child)
	h.markDirtyRoot(child)
	return nil
}

// Detach removes child from its parent so it becomes a root entity.
// When keepWorld is true, child's local Transform2D is updated to its composed world transform before detaching.
// Detaching Zero, a dead entity, or an entity that is already a root is a no-op.
func (h *Hierarchy2D) Detach(child Entity, keepWorld bool) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(child) {
		return
	}
	idx := int(child.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != child || h.nodes[idx].parent == Zero {
		return
	}
	if keepWorld {
		if cw, ok := h.resolveWorld(child); ok {
			var hidden bool
			if lp, has := h.localStore.Get(child); has {
				hidden = lp.Hidden
			}
			h.localStore.Set(child, Transform2D{
				X:        cw.X,
				Y:        cw.Y,
				Rotation: cw.Rotation,
				ScaleX:   cw.ScaleX,
				ScaleY:   cw.ScaleY,
				Hidden:   hidden,
			})
		}
	}
	h.unlinkFromParent(child)
	h.markDirtyRoot(child)
}

// Parent returns e's parent entity and true, or (Zero, false) if e is dead, not in the hierarchy, or is a root.
func (h *Hierarchy2D) Parent(e Entity) (Entity, bool) {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return Zero, false
	}
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return Zero, false
	}
	p := h.nodes[idx].parent
	if p == Zero {
		return Zero, false
	}
	return p, true
}

// ChildCount returns the number of direct children of e (0 if e is dead or has no children).
func (h *Hierarchy2D) ChildCount(e Entity) int {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return 0
	}
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return 0
	}
	return int(h.nodes[idx].childCount)
}

// Children returns a newly allocated slice of e's direct children in deterministic insertion order, or nil if e has none.
// For zero-allocation iteration, use EachChild or AppendChildren.
func (h *Hierarchy2D) Children(e Entity) []Entity {
	n := h.ChildCount(e)
	if n == 0 {
		return nil
	}
	return h.AppendChildren(make([]Entity, 0, n), e)
}

// AppendChildren appends e's direct children in deterministic insertion order to dst and returns the extended slice.
func (h *Hierarchy2D) AppendChildren(dst []Entity, e Entity) []Entity {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return dst
	}
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return dst
	}
	for ch := h.nodes[idx].firstChild; ch != Zero; ch = h.nodes[ch.Index()].nextSibling {
		dst = append(dst, ch)
	}
	return dst
}

// EachChild calls fn for each direct child of e in deterministic insertion order until fn returns false.
func (h *Hierarchy2D) EachChild(e Entity, fn func(Entity) bool) {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return
	}
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return
	}
	for ch := h.nodes[idx].firstChild; ch != Zero; {
		next := h.nodes[ch.Index()].nextSibling
		if !fn(ch) {
			return
		}
		ch = next
	}
}

// Local returns e's local Transform2D and true, or (zero, false) if e is dead or has no Transform2D.
func (h *Hierarchy2D) Local(e Entity) (Transform2D, bool) {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return Transform2D{}, false
	}
	lp, ok := h.localStore.Get(e)
	if !ok {
		return Transform2D{}, false
	}
	return *lp, true
}

// SetLocal sets e's local Transform2D and marks e and its descendants dirty.
// It is a no-op if e is not alive.
func (h *Hierarchy2D) SetLocal(e Entity, t Transform2D) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	h.localStore.Set(e, t)
	h.markDirty(e)
}

// SetPosition updates e's local (X, Y) position and marks e and its descendants dirty.
// If e does not yet have a Transform2D, NewTransform2D(x, y) is attached.
func (h *Hierarchy2D) SetPosition(e Entity, x, y float32) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	if lp, ok := h.localStore.Get(e); ok {
		lp.X = x
		lp.Y = y
	} else {
		h.localStore.Set(e, NewTransform2D(x, y))
	}
	h.markDirty(e)
}

// SetRotation updates e's local rotation in radians and marks e and its descendants dirty.
// If e does not yet have a Transform2D, IdentityTransform2D with the given rotation is attached.
func (h *Hierarchy2D) SetRotation(e Entity, rotation float32) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	if lp, ok := h.localStore.Get(e); ok {
		lp.Rotation = rotation
	} else {
		t := IdentityTransform2D()
		t.Rotation = rotation
		h.localStore.Set(e, t)
	}
	h.markDirty(e)
}

// SetScale updates e's local (ScaleX, ScaleY) and marks e and its descendants dirty.
// If e does not yet have a Transform2D, IdentityTransform2D with the given scale is attached.
func (h *Hierarchy2D) SetScale(e Entity, sx, sy float32) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	if lp, ok := h.localStore.Get(e); ok {
		lp.ScaleX = sx
		lp.ScaleY = sy
	} else {
		t := IdentityTransform2D()
		t.ScaleX = sx
		t.ScaleY = sy
		h.localStore.Set(e, t)
	}
	h.markDirty(e)
}

// SetVisible updates e's local visibility and marks e and its descendants dirty.
// If e does not yet have a Transform2D, IdentityTransform2D with the given visibility is attached.
func (h *Hierarchy2D) SetVisible(e Entity, visible bool) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	if lp, ok := h.localStore.Get(e); ok {
		lp.Hidden = !visible
	} else {
		t := IdentityTransform2D()
		t.Hidden = !visible
		h.localStore.Set(e, t)
	}
	h.markDirty(e)
}

// MarkDirty marks e and all of its descendants as needing world-transform recomputation.
// Call MarkDirty after mutating a *Transform2D pointer obtained directly from Store[Transform2D].
func (h *Hierarchy2D) MarkDirty(e Entity) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.ensureNode(e)
	h.markDirty(e)
}

// IsDirty reports whether e currently needs world-transform recomputation.
func (h *Hierarchy2D) IsDirty(e Entity) bool {
	if h == nil || h.world == nil || !h.world.Alive(e) {
		return false
	}
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return h.localStore.Has(e)
	}
	return h.nodes[idx].dirty
}

// Sync updates WorldTransform2D for all dirty subtrees in topological parent-before-child order, skipping clean nodes completely.
// It returns the number of entities whose WorldTransform2D was recomputed and performs zero heap allocations in steady state.
func (h *Hierarchy2D) Sync() int {
	if h == nil || h.world == nil || h.world.disposed || len(h.dirtyRoots) == 0 {
		return 0
	}
	updated := 0
	for i := 0; i < len(h.dirtyRoots); i++ {
		r := h.dirtyRoots[i]
		idx := int(r.Index())
		if idx >= len(h.nodes) || h.nodes[idx].entity != r {
			continue
		}
		h.nodes[idx].inDirtyList = false
		if !h.nodes[idx].dirty {
			continue
		}

		// Climb to the highest dirty ancestor so parents are always computed before children.
		top := r
		for {
			p := h.nodes[top.Index()].parent
			if p == Zero {
				break
			}
			pidx := int(p.Index())
			if pidx >= len(h.nodes) || h.nodes[pidx].entity != p || !h.nodes[pidx].dirty {
				break
			}
			top = p
		}

		// Iterative top-down DFS in deterministic child order.
		h.stack = append(h.stack[:0], top)
		for len(h.stack) > 0 {
			last := len(h.stack) - 1
			cur := h.stack[last]
			h.stack = h.stack[:last]

			cn := &h.nodes[cur.Index()]
			if cn.entity != cur || !cn.dirty {
				continue
			}
			h.computeWorld(cur, cn)
			cn.dirty = false
			updated++

			for ch := cn.lastChild; ch != Zero; ch = h.nodes[ch.Index()].prevSibling {
				h.stack = append(h.stack, ch)
			}
		}
	}
	h.dirtyRoots = h.dirtyRoots[:0]
	h.stack = h.stack[:0]
	return updated
}

// WorldTransform returns e's up-to-date WorldTransform2D and true, resolving e's dirty ancestor chain lazily on demand if needed.
// It returns (zero, false) if e is dead or has no Transform2D and is not part of the hierarchy.
func (h *Hierarchy2D) WorldTransform(e Entity) (WorldTransform2D, bool) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return WorldTransform2D{}, false
	}
	idx := int(e.Index())
	inHierarchy := idx < len(h.nodes) && h.nodes[idx].entity == e
	if !inHierarchy {
		if !h.localStore.Has(e) {
			return WorldTransform2D{}, false
		}
		h.ensureNode(e)
	}
	return h.resolveWorld(e)
}

// DestroyWithPolicy destroys e in the World after applying policy to its children:
//   - RemoveDetach: detaches direct children to root, keeping each child's local Transform2D.
//   - RemoveDetachKeepWorld: detaches direct children to root while preserving their world transforms.
//   - RemoveCascade: recursively destroys e and all of its descendants.
//
// In all cases, parent/child links are cleaned up so no dangling handles remain.
func (h *Hierarchy2D) DestroyWithPolicy(e Entity, policy RemovePolicy) {
	if h == nil || h.world == nil || h.world.disposed || !h.world.Alive(e) {
		return
	}
	h.prepareRemove(e, policy)
	prev := h.inDestroy
	h.inDestroy = true
	h.world.Destroy(e)
	h.inDestroy = prev
}

func (h *Hierarchy2D) onWorldDestroy(e Entity) {
	if h.inDestroy {
		return
	}
	h.prepareRemove(e, h.defaultPolicy)
}

func (h *Hierarchy2D) prepareRemove(e Entity, policy RemovePolicy) {
	idx := int(e.Index())
	if idx >= len(h.nodes) || h.nodes[idx].entity != e {
		return
	}
	switch policy {
	case RemoveCascade:
		h.unlinkFromParent(e)
		h.destroyBuf = h.destroyBuf[:0]
		// Collect all descendants iteratively (excluding e itself, which the caller destroys).
		for ch := h.nodes[idx].firstChild; ch != Zero; ch = h.nodes[ch.Index()].nextSibling {
			h.destroyBuf = append(h.destroyBuf, ch)
		}
		for i := 0; i < len(h.destroyBuf); i++ {
			cur := h.destroyBuf[i]
			cn := &h.nodes[cur.Index()]
			if cn.entity != cur {
				continue
			}
			for ch := cn.firstChild; ch != Zero; ch = h.nodes[ch.Index()].nextSibling {
				h.destroyBuf = append(h.destroyBuf, ch)
			}
		}
		// Clear all node slots before destroying entities in World.
		h.nodes[idx] = hierarchyNode{}
		for _, desc := range h.destroyBuf {
			h.nodes[desc.Index()] = hierarchyNode{}
		}
		prev := h.inDestroy
		h.inDestroy = true
		for _, desc := range h.destroyBuf {
			h.world.Destroy(desc)
		}
		h.inDestroy = prev
		h.destroyBuf = h.destroyBuf[:0]

	case RemoveDetachKeepWorld:
		// Resolve direct children while parent is still attached, then update their local transforms.
		for ch := h.nodes[idx].firstChild; ch != Zero; ch = h.nodes[ch.Index()].nextSibling {
			if cw, ok := h.resolveWorld(ch); ok {
				var hidden bool
				if lp, has := h.localStore.Get(ch); has {
					hidden = lp.Hidden
				}
				h.localStore.Set(ch, Transform2D{
					X:        cw.X,
					Y:        cw.Y,
					Rotation: cw.Rotation,
					ScaleX:   cw.ScaleX,
					ScaleY:   cw.ScaleY,
					Hidden:   hidden,
				})
			}
		}
		h.detachAllChildren(idx)
		h.unlinkFromParent(e)
		h.nodes[idx] = hierarchyNode{}

	default: // RemoveDetach
		h.detachAllChildren(idx)
		h.unlinkFromParent(e)
		h.nodes[idx] = hierarchyNode{}
	}
}

func (h *Hierarchy2D) detachAllChildren(parentIdx int) {
	for ch := h.nodes[parentIdx].firstChild; ch != Zero; {
		chn := &h.nodes[ch.Index()]
		next := chn.nextSibling
		chn.parent = Zero
		chn.prevSibling = Zero
		chn.nextSibling = Zero
		h.markDirtyRoot(ch)
		ch = next
	}
	h.nodes[parentIdx].firstChild = Zero
	h.nodes[parentIdx].lastChild = Zero
	h.nodes[parentIdx].childCount = 0
}

func (h *Hierarchy2D) clear() {
	clear(h.nodes)
	h.nodes = h.nodes[:0]
	h.dirtyRoots = h.dirtyRoots[:0]
	h.stack = h.stack[:0]
	h.ancestors = h.ancestors[:0]
	h.destroyBuf = h.destroyBuf[:0]
}

func (h *Hierarchy2D) reset() {
	h.nodes = nil
	h.dirtyRoots = nil
	h.stack = nil
	h.ancestors = nil
	h.destroyBuf = nil
}

func (h *Hierarchy2D) onWorldRestore() {
	h.clear()
	h.seedFromLocalStore()
}

func (h *Hierarchy2D) seedFromLocalStore() {
	if h.localStore == nil || len(h.localStore.dense) == 0 {
		return
	}
	for _, e := range h.localStore.dense {
		h.ensureNode(e)
	}
}

func (h *Hierarchy2D) ensureTransform(e Entity) {
	if !h.localStore.Has(e) {
		h.localStore.Set(e, IdentityTransform2D())
	}
}

func (h *Hierarchy2D) ensureNode(e Entity) {
	idx := int(e.Index())
	if idx >= len(h.nodes) {
		need := idx + 1
		if need <= cap(h.nodes) {
			old := len(h.nodes)
			h.nodes = h.nodes[:need]
			clear(h.nodes[old:need])
		} else {
			nextCap := max(need, len(h.nodes)*2)
			grown := make([]hierarchyNode, need, nextCap)
			copy(grown, h.nodes)
			h.nodes = grown
		}
	}
	n := &h.nodes[idx]
	if n.entity != e {
		*n = hierarchyNode{
			entity:      e,
			dirty:       true,
			inDirtyList: true,
		}
		h.dirtyRoots = append(h.dirtyRoots, e)
	}
}

func (h *Hierarchy2D) unlinkFromParent(child Entity) {
	cn := &h.nodes[child.Index()]
	p := cn.parent
	if p == Zero {
		return
	}
	pn := &h.nodes[p.Index()]
	prev := cn.prevSibling
	next := cn.nextSibling
	if prev != Zero {
		h.nodes[prev.Index()].nextSibling = next
	} else {
		pn.firstChild = next
	}
	if next != Zero {
		h.nodes[next.Index()].prevSibling = prev
	} else {
		pn.lastChild = prev
	}
	pn.childCount--
	cn.parent = Zero
	cn.prevSibling = Zero
	cn.nextSibling = Zero
}

func (h *Hierarchy2D) appendChild(parent, child Entity) {
	pn := &h.nodes[parent.Index()]
	cn := &h.nodes[child.Index()]
	cn.parent = parent
	cn.prevSibling = pn.lastChild
	cn.nextSibling = Zero
	if pn.lastChild != Zero {
		h.nodes[pn.lastChild.Index()].nextSibling = child
	} else {
		pn.firstChild = child
	}
	pn.lastChild = child
	pn.childCount++
}

// markDirty marks e and any clean descendants dirty and queues e in dirtyRoots if not already dirty.
// Invariant: whenever a node is dirty, all of its descendants are dirty and some dirty ancestor (or itself) is queued in dirtyRoots.
func (h *Hierarchy2D) markDirty(e Entity) {
	n := &h.nodes[e.Index()]
	if n.dirty {
		return
	}
	h.markDirtyRoot(e)
}

// markDirtyRoot unconditionally ensures e is queued in dirtyRoots and its entire subtree is marked dirty.
// Used after structural changes (SetParent / Detach) where e may already be dirty under a different ancestor.
func (h *Hierarchy2D) markDirtyRoot(e Entity) {
	n := &h.nodes[e.Index()]
	if !n.inDirtyList {
		n.inDirtyList = true
		h.dirtyRoots = append(h.dirtyRoots, e)
	}
	if n.dirty {
		return
	}
	n.dirty = true
	if n.firstChild == Zero {
		return
	}
	h.stack = append(h.stack[:0], n.firstChild)
	for len(h.stack) > 0 {
		last := len(h.stack) - 1
		cur := h.stack[last]
		h.stack = h.stack[:last]

		for s := cur; s != Zero; s = h.nodes[s.Index()].nextSibling {
			sn := &h.nodes[s.Index()]
			if sn.dirty {
				continue
			}
			sn.dirty = true
			if sn.firstChild != Zero {
				h.stack = append(h.stack, sn.firstChild)
			}
		}
	}
}

func (h *Hierarchy2D) resolveWorld(e Entity) (WorldTransform2D, bool) {
	n := &h.nodes[e.Index()]
	if !n.dirty {
		if wp, ok := h.worldStore.Get(e); ok {
			return *wp, true
		}
		// Node was marked clean but lacks cached WorldTransform2D (e.g. newly registered node).
		wt := h.computeWorld(e, n)
		return wt, true
	}

	// Collect dirty ancestor chain from e upward.
	h.ancestors = h.ancestors[:0]
	for cur := e; cur != Zero; {
		cn := &h.nodes[cur.Index()]
		if cn.entity != cur || !cn.dirty {
			break
		}
		h.ancestors = append(h.ancestors, cur)
		cur = cn.parent
	}

	// Clean from highest dirty ancestor down to e, promoting remaining dirty sibling branches to dirtyRoots.
	var lastWT WorldTransform2D
	for i := len(h.ancestors) - 1; i >= 0; i-- {
		cur := h.ancestors[i]
		cn := &h.nodes[cur.Index()]
		lastWT = h.computeWorld(cur, cn)
		cn.dirty = false

		var nextOnPath Entity
		if i > 0 {
			nextOnPath = h.ancestors[i-1]
		}
		for ch := cn.firstChild; ch != Zero; ch = h.nodes[ch.Index()].nextSibling {
			if ch == nextOnPath {
				continue
			}
			chn := &h.nodes[ch.Index()]
			if chn.dirty && !chn.inDirtyList {
				chn.inDirtyList = true
				h.dirtyRoots = append(h.dirtyRoots, ch)
			}
		}
	}
	h.ancestors = h.ancestors[:0]
	return lastWT, true
}

func (h *Hierarchy2D) computeWorld(e Entity, n *hierarchyNode) WorldTransform2D {
	local := IdentityTransform2D()
	if lp, ok := h.localStore.Get(e); ok {
		local = *lp
	}
	var wt WorldTransform2D
	if n.parent == Zero {
		wt = WorldTransform2D{
			X:            local.X,
			Y:            local.Y,
			Rotation:     local.Rotation,
			ScaleX:       local.ScaleX,
			ScaleY:       local.ScaleY,
			Matrix:       local.Matrix(),
			WorldVisible: !local.Hidden,
		}
	} else {
		var pw WorldTransform2D
		if pp, ok := h.worldStore.Get(n.parent); ok {
			pw = *pp
		} else {
			pw = WorldTransform2D{
				ScaleX:       1,
				ScaleY:       1,
				Matrix:       [6]float32{1, 0, 0, 1, 0, 0},
				WorldVisible: true,
			}
		}
		wm := mulAffine(pw.Matrix, local.Matrix())
		x, y, rot, sx, sy := decomposeAffine(wm)
		wt = WorldTransform2D{
			X:            x,
			Y:            y,
			Rotation:     rot,
			ScaleX:       sx,
			ScaleY:       sy,
			Matrix:       wm,
			WorldVisible: pw.WorldVisible && !local.Hidden,
		}
	}
	h.worldStore.Set(e, wt)
	return wt
}

func mulAffine(p, l [6]float32) [6]float32 {
	return [6]float32{
		p[0]*l[0] + p[2]*l[1],
		p[1]*l[0] + p[3]*l[1],
		p[0]*l[2] + p[2]*l[3],
		p[1]*l[2] + p[3]*l[3],
		p[0]*l[4] + p[2]*l[5] + p[4],
		p[1]*l[4] + p[3]*l[5] + p[5],
	}
}

func decomposeAffine(m [6]float32) (x, y, rot, sx, sy float32) {
	x = m[4]
	y = m[5]
	if m[1] == 0 && m[2] == 0 && m[0] > 0 {
		return x, y, 0, m[0], m[3]
	}
	a, b, c, d := float64(m[0]), float64(m[1]), float64(m[2]), float64(m[3])
	scaleX := math.Sqrt(a*a + b*b)
	scaleY := math.Sqrt(c*c + d*d)
	det := a*d - b*c
	if det < 0 {
		scaleY = -scaleY
	}
	if scaleX > 1e-12 {
		rot = float32(math.Atan2(b, a))
	} else if scaleY != 0 {
		rot = float32(math.Atan2(-c/scaleY, d/scaleY))
	}
	return x, y, rot, float32(scaleX), float32(scaleY)
}

func relativeTransform(parent, child WorldTransform2D, hidden bool) (Transform2D, bool) {
	pa, pb := float64(parent.Matrix[0]), float64(parent.Matrix[1])
	pc, pd := float64(parent.Matrix[2]), float64(parent.Matrix[3])
	ptx, pty := float64(parent.Matrix[4]), float64(parent.Matrix[5])
	det := pa*pd - pb*pc
	if math.Abs(det) >= 1e-12 {
		invDet := 1.0 / det
		ia := pd * invDet
		ib := -pb * invDet
		ic := -pc * invDet
		id := pa * invDet
		itx := -(ia*ptx + ic*pty)
		ity := -(ib*ptx + id*pty)

		ca, cb := float64(child.Matrix[0]), float64(child.Matrix[1])
		cc, cd := float64(child.Matrix[2]), float64(child.Matrix[3])
		ctx, cty := float64(child.Matrix[4]), float64(child.Matrix[5])

		ra := ia*ca + ic*cb
		rb := ib*ca + id*cb
		rc := ia*cc + ic*cd
		rd := ib*cc + id*cd
		rtx := ia*ctx + ic*cty + itx
		rty := ib*ctx + id*cty + ity

		scaleX := math.Sqrt(ra*ra + rb*rb)
		scaleY := math.Sqrt(rc*rc + rd*rd)
		// TRS columns are orthogonal. A non-orthogonal relative matrix is a
		// shear that Transform2D cannot carry; decomposing it would silently
		// change the child's world transform (#546).
		if math.Abs(ra*rc+rb*rd) > 1e-4*scaleX*scaleY {
			return Transform2D{}, false
		}
		if ra*rd-rb*rc < 0 {
			scaleY = -scaleY
		}
		var rot float64
		if scaleX > 1e-12 {
			rot = math.Atan2(rb, ra)
		} else if scaleY != 0 {
			rot = math.Atan2(-rc/scaleY, rd/scaleY)
		}
		return Transform2D{
			X:        float32(rtx),
			Y:        float32(rty),
			Rotation: float32(rot),
			ScaleX:   float32(scaleX),
			ScaleY:   float32(scaleY),
			Hidden:   hidden,
		}, true
	}

	// Singular parent (e.g. zero scale): fall back cleanly without NaN/Inf.
	var sx, sy float32
	if parent.ScaleX != 0 {
		sx = child.ScaleX / parent.ScaleX
	}
	if parent.ScaleY != 0 {
		sy = child.ScaleY / parent.ScaleY
	}
	return Transform2D{
		X:        child.X - parent.X,
		Y:        child.Y - parent.Y,
		Rotation: child.Rotation - parent.Rotation,
		ScaleX:   sx,
		ScaleY:   sy,
		Hidden:   hidden,
	}, true
}

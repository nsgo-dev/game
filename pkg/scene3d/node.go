package scene3d

import (
	"errors"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// Layer bitmask constants for node filtering in raycasts and picking.
const (
	// LayerDefault is the default layer assigned to nodes created by NewNode.
	LayerDefault uint32 = 1 << 0
	// LayerAll matches every non-zero layer bit.
	LayerAll uint32 = ^uint32(0)
)

// Hierarchy errors returned by Node tree mutations.
var (
	// ErrNilNode is returned when a nil node is passed to AddChild or Attach.
	ErrNilNode = errors.New("scene3d: nil node")
	// ErrCycle is returned when parenting a node would introduce a cycle.
	ErrCycle = errors.New("scene3d: hierarchy cycle detected")
	// ErrNotDecomposable is returned by a keep-world reparent whose new parent is singular or whose exact local transform needs shear, which a Transform cannot hold.
	ErrNotDecomposable = errors.New("scene3d: local transform is not decomposable to TRS")
)

// Node is a transformable element in a 3D scene hierarchy.
//
// Always construct nodes with [NewNode] so Local defaults to TransformIdentity,
// Visible and Enabled default to true, Layer defaults to LayerDefault, and
// LocalBounds defaults to AABBEmpty.
//
// World transforms, matrices, and bounds are cached and recomputed lazily when
// dirty. Prefer the setter methods (SetLocal, SetPosition, SetRotation,
// SetScale, SetLocalBounds) or call MarkDirty after mutating Local directly so
// dirty flags propagate immediately to descendants.
type Node struct {
	Name        string
	Local       math3d.Transform
	Visible     bool
	Enabled     bool
	Layer       uint32
	LocalBounds math3d.AABB
	Tags        []string
	UserData    any
	Attachments map[string]any
	Collider    Collider

	parent   *Node
	children []*Node
	sockets  map[string]*Node
	scene    *Scene
	id       uint64

	initialized       bool
	dirty             bool
	boundsDirty       bool
	hasBVH            bool
	bvhQueued         bool
	bvhHandle         BVHHandle
	dirtyUpdates      uint64
	cachedLocal       math3d.Transform
	cachedLocalBounds math3d.AABB
	worldTransform    math3d.Transform
	worldMat          math3d.Mat4
	worldBounds       math3d.AABB
	// visGen and visOK memoize Scene.activeVisible for one scene query (Scene.visGen), so hits under a deep chain do not each walk every ancestor (#546).
	visGen uint64
	visOK  bool
}

// NewNode returns a new visible, enabled Node with an identity local transform,
// LayerDefault, and an empty local bounding box.
func NewNode(name string) *Node {
	return &Node{
		Name:        name,
		Local:       math3d.TransformIdentity(),
		Visible:     true,
		Enabled:     true,
		Layer:       LayerDefault,
		LocalBounds: math3d.AABBEmpty(),
		dirty:       true,
		boundsDirty: true,
	}
}

// Parent returns n's parent node, or nil if n is a root.
func (n *Node) Parent() *Node {
	if n == nil {
		return nil
	}
	return n.parent
}

// Children returns a shallow copy of n's direct children slice.
func (n *Node) Children() []*Node {
	if n == nil || len(n.children) == 0 {
		return nil
	}
	return slices.Clone(n.children)
}

// IsDescendantOf reports whether ancestor is an ancestor of n in the hierarchy.
func (n *Node) IsDescendantOf(ancestor *Node) bool {
	if n == nil || ancestor == nil {
		return false
	}
	for p := n.parent; p != nil; p = p.parent {
		if p == ancestor {
			return true
		}
	}
	return false
}

// AddChild attaches child as a direct child of n, detaching it from any
// previous parent. It returns ErrNilNode if child is nil and ErrCycle if child
// is n or an ancestor of n.
func (n *Node) AddChild(child *Node) error {
	if n == nil || child == nil {
		return ErrNilNode
	}
	if child == n || n.IsDescendantOf(child) {
		return ErrCycle
	}
	if child.parent == n {
		return nil
	}
	if child.parent != nil {
		child.parent.RemoveChild(child)
	}
	n.children = append(n.children, child)
	child.parent = n
	if n.scene != nil {
		child.attachToScene(n.scene)
	}
	child.MarkDirty()
	return nil
}

// RemoveChild detaches child from n and returns true if child was a direct
// child of n.
func (n *Node) RemoveChild(child *Node) bool {
	if n == nil || child == nil || child.parent != n {
		return false
	}
	idx := slices.Index(n.children, child)
	if idx < 0 {
		return false
	}
	n.children = slices.Delete(n.children, idx, idx+1)
	child.parent = nil
	for name, sock := range n.sockets {
		if sock == child {
			delete(n.sockets, name)
		}
	}
	if child.scene != nil {
		child.detachFromScene()
	}
	child.MarkDirty()
	return true
}

func (n *Node) attachToScene(s *Scene) {
	if n.scene == s {
		return
	}
	if n.scene != nil {
		n.detachFromScene()
	}
	n.scene = s
	if n.id == 0 {
		s.nextNodeID++
		n.id = s.nextNodeID
	}
	s.queueBVH(n)
	for _, c := range n.children {
		c.attachToScene(s)
	}
}

func (n *Node) detachFromScene() {
	s := n.scene
	if s == nil {
		return
	}
	if n.hasBVH {
		if s.bvh != nil {
			s.bvh.Remove(n.bvhHandle)
		}
		n.bvhHandle = BVHHandle{}
		n.hasBVH = false
	}
	n.bvhQueued = false
	n.scene = nil
	for _, c := range n.children {
		c.detachFromScene()
	}
}

// Reparent moves n under newParent while keeping n's Local transform unchanged.
// Passing nil detaches n from its current parent. It returns ErrCycle if
// newParent is n or a descendant of n.
func (n *Node) Reparent(newParent *Node) error {
	if n == nil {
		return ErrNilNode
	}
	if newParent == nil {
		if n.parent != nil {
			n.parent.RemoveChild(n)
		}
		return nil
	}
	return newParent.AddChild(n)
}

// ReparentKeepWorld moves n under newParent while adjusting n.Local so n remains at its current world matrix. Passing nil detaches n to root.
//
// It returns ErrNotDecomposable and changes nothing when newParent's world matrix is singular or the exact local transform needs shear, which a Transform cannot hold (a rotated child under a non-uniformly scaled parent).
func (n *Node) ReparentKeepWorld(newParent *Node) error {
	if n == nil {
		return ErrNilNode
	}
	if newParent == n || (newParent != nil && newParent.IsDescendantOf(n)) {
		return ErrCycle
	}
	if newParent == nil {
		world := n.WorldTransform()
		if err := n.Reparent(nil); err != nil {
			return err
		}
		n.SetLocal(world)
		return nil
	}
	// Rendering uses the composed matrix, so solve local = parent⁻¹ · world on matrices. Transform.Inverse().Mul is not exact for a non-uniformly scaled, rotated parent and moved the child (#546).
	parentInv, ok := newParent.WorldMatrix().Inverse()
	if !ok {
		return ErrNotDecomposable
	}
	pos, rot, scale, ok := parentInv.Mul(n.WorldMatrix()).Decompose()
	if !ok {
		return ErrNotDecomposable
	}
	if err := n.Reparent(newParent); err != nil {
		return err
	}
	n.SetLocal(math3d.Transform{Pos: pos, Rot: rot, Scale: scale})
	return nil
}

// SetParent moves n under newParent (or detaches it to root when newParent is
// nil). When keepWorld is true, n.Local is adjusted so n preserves its current
// world transform; when false, n.Local is left unchanged.
func (n *Node) SetParent(newParent *Node, keepWorld bool) error {
	if keepWorld {
		return n.ReparentKeepWorld(newParent)
	}
	return n.Reparent(newParent)
}

// Detach removes n from its current parent. When keepWorld is true, n.Local is
// updated to n's prior world transform so its world pose is preserved exactly.
// Returns true if n had a parent and was detached.
func (n *Node) Detach(keepWorld bool) bool {
	if n == nil || n.parent == nil {
		return false
	}
	if keepWorld {
		world := n.WorldTransform()
		if !n.parent.RemoveChild(n) {
			return false
		}
		n.SetLocal(world)
		return true
	}
	return n.parent.RemoveChild(n)
}

// FindByName searches n and its descendants in pre-order for the first node
// whose Name equals name, or nil if none matches.
func (n *Node) FindByName(name string) *Node {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for _, c := range n.children {
		if found := c.FindByName(name); found != nil {
			return found
		}
	}
	return nil
}

// Walk traverses n and its descendants in pre-order depth-first order, calling
// fn on each node. If fn returns false, traversal stops immediately.
func (n *Node) Walk(fn func(*Node) bool) {
	if n == nil || fn == nil {
		return
	}
	n.walk(fn)
}

func (n *Node) walk(fn func(*Node) bool) bool {
	if !fn(n) {
		return false
	}
	for _, c := range n.children {
		if !c.walk(fn) {
			return false
		}
	}
	return true
}

// EffectiveVisible reports whether n and every ancestor up to the root have Visible == true.
func (n *Node) EffectiveVisible() bool {
	for p := n; p != nil; p = p.parent {
		if !p.Visible {
			return false
		}
	}
	return n != nil
}

// EffectiveEnabled reports whether n and every ancestor up to the root have Enabled == true.
func (n *Node) EffectiveEnabled() bool {
	for p := n; p != nil; p = p.parent {
		if !p.Enabled {
			return false
		}
	}
	return n != nil
}

// SetVisible sets n.Visible.
func (n *Node) SetVisible(visible bool) {
	if n == nil {
		return
	}
	n.Visible = visible
}

// SetEnabled sets n.Enabled.
func (n *Node) SetEnabled(enabled bool) {
	if n == nil {
		return
	}
	n.Enabled = enabled
}

// SetLocal replaces n.Local and marks n and all descendants dirty.
func (n *Node) SetLocal(t math3d.Transform) {
	n.Local = t
	n.MarkDirty()
}

// SetPosition updates n.Local.Pos and marks n and all descendants dirty.
func (n *Node) SetPosition(pos math3d.Vec3) {
	n.Local.Pos = pos
	n.MarkDirty()
}

// SetRotation updates n.Local.Rot and marks n and all descendants dirty.
func (n *Node) SetRotation(rot math3d.Quat) {
	n.Local.Rot = rot
	n.MarkDirty()
}

// SetScale updates n.Local.Scale and marks n and all descendants dirty.
func (n *Node) SetScale(scale math3d.Vec3) {
	n.Local.Scale = scale
	n.MarkDirty()
}

// SetLocalBounds updates n.LocalBounds and invalidates n's cached world bounds.
func (n *Node) SetLocalBounds(bounds math3d.AABB) {
	n.LocalBounds = bounds
	n.boundsDirty = true
	if n.scene != nil {
		n.scene.queueBVH(n)
	}
}

// SetCollider assigns a narrow-phase Collider to n and invalidates cached world bounds.
func (n *Node) SetCollider(c Collider) {
	n.Collider = c
	n.boundsDirty = true
	if n.scene != nil {
		n.scene.queueBVH(n)
	}
}

// FitBoundsToCollider copies n.Collider.LocalBounds() into n.LocalBounds when
// n.Collider is non-nil.
func (n *Node) FitBoundsToCollider() {
	if n.Collider == nil {
		return
	}
	n.SetLocalBounds(n.Collider.LocalBounds())
}

// MarkDirty invalidates cached world transforms and bounds for n and every
// descendant in its subtree.
func (n *Node) MarkDirty() {
	if n == nil {
		return
	}
	n.dirty = true
	n.boundsDirty = true
	if n.scene != nil {
		n.scene.queueBVH(n)
	}
	for _, c := range n.children {
		needQueue := c.scene != nil && !c.bvhQueued && (c.hasBVH || !c.LocalBounds.Empty() || c.Collider != nil)
		if !c.dirty || !c.boundsDirty || needQueue {
			c.MarkDirty()
		}
	}
}

// IsDirty reports whether n's cached world transform needs recomputation.
func (n *Node) IsDirty() bool {
	if n == nil {
		return false
	}
	return n.dirty || !n.initialized || n.Local != n.cachedLocal
}

// DirtyUpdates returns the cumulative number of times n's cached world
// transform and matrix have been recomputed.
func (n *Node) DirtyUpdates() uint64 {
	if n == nil {
		return 0
	}
	return n.dirtyUpdates
}

// syncDirtyAncestors detects direct field mutations to Local on n or any
// ancestor and propagates dirty flags downward before recomputing.
func (n *Node) syncDirtyAncestors() {
	var topMutated *Node
	for p := n; p != nil; p = p.parent {
		if !p.initialized || (!p.dirty && p.Local != p.cachedLocal) {
			topMutated = p
		}
	}
	if topMutated != nil {
		topMutated.MarkDirty()
	}
}

func (n *Node) ensureWorld() {
	n.syncDirtyAncestors()
	if !n.dirty && n.initialized {
		return
	}
	if n.parent != nil {
		n.parent.ensureWorld()
		n.worldTransform = n.parent.worldTransform.Mul(n.Local)
		n.worldMat = n.parent.worldMat.Mul(n.Local.ToMat4())
	} else {
		n.worldTransform = n.Local
		n.worldMat = n.Local.ToMat4()
	}
	n.cachedLocal = n.Local
	n.initialized = true
	n.dirty = false
	n.boundsDirty = true
	n.dirtyUpdates++
}

// WorldTransform returns n's composed world-space TRS transform, recomputing
// lazily if n or any ancestor is dirty.
func (n *Node) WorldTransform() math3d.Transform {
	if n == nil {
		return math3d.TransformIdentity()
	}
	n.ensureWorld()
	return n.worldTransform
}

// WorldMatrix returns n's composed world-space 4x4 transformation matrix,
// recomputing lazily if n or any ancestor is dirty.
func (n *Node) WorldMatrix() math3d.Mat4 {
	if n == nil {
		return math3d.Mat4Identity()
	}
	n.ensureWorld()
	return n.worldMat
}

// WorldBounds returns the tight world-space AABB enclosing both n's LocalBounds
// and its Collider's local bounds, recomputing lazily when dirty. A collider
// that extends past the visual mesh must stay queryable by BVH and raycasts.
// Returns AABBEmpty if n has no bounds.
func (n *Node) WorldBounds() math3d.AABB {
	if n == nil {
		return math3d.AABBEmpty()
	}
	n.ensureWorld()
	bounds := n.LocalBounds
	if n.Collider != nil {
		if b := n.Collider.LocalBounds(); !b.Empty() {
			bounds = bounds.Union(b)
		}
	}
	if bounds.Empty() {
		return math3d.AABBEmpty()
	}
	if !n.boundsDirty && bounds == n.cachedLocalBounds {
		return n.worldBounds
	}
	n.worldBounds = bounds.Transform(n.worldMat)
	n.cachedLocalBounds = bounds
	n.boundsDirty = false
	return n.worldBounds
}

// HierarchyBounds returns the union of WorldBounds for n and all visible,
// enabled descendants in its subtree.
func (n *Node) HierarchyBounds() math3d.AABB {
	if n == nil || !n.Visible || !n.Enabled {
		return math3d.AABBEmpty()
	}
	bounds := n.WorldBounds()
	for _, c := range n.children {
		if c.Visible && c.Enabled {
			bounds = bounds.Union(c.HierarchyBounds())
		}
	}
	return bounds
}

// HasTag reports whether n carries tag.
func (n *Node) HasTag(tag string) bool {
	return n != nil && slices.Contains(n.Tags, tag)
}

// AddTag appends tag to n.Tags if not already present.
func (n *Node) AddTag(tag string) {
	if n == nil || n.HasTag(tag) {
		return
	}
	n.Tags = append(n.Tags, tag)
}

// RemoveTag removes tag from n.Tags if present.
func (n *Node) RemoveTag(tag string) {
	if n == nil {
		return
	}
	if idx := slices.Index(n.Tags, tag); idx >= 0 {
		n.Tags = slices.Delete(n.Tags, idx, idx+1)
	}
}

// FindByTag returns all nodes in n's subtree carrying tag in pre-order.
func (n *Node) FindByTag(tag string) []*Node {
	var out []*Node
	n.Walk(func(cur *Node) bool {
		if cur.HasTag(tag) {
			out = append(out, cur)
		}
		return true
	})
	return out
}

// DefineSocket creates or updates a named socket attachment point on n with the
// given local transform relative to n and returns the socket node.
//
// Children attached via Attach(name, child) become children of this socket node
// and follow n's transforms automatically.
func (n *Node) DefineSocket(name string, local math3d.Transform) *Node {
	if n == nil {
		return nil
	}
	if n.sockets == nil {
		n.sockets = make(map[string]*Node)
	}
	if sock, ok := n.sockets[name]; ok {
		sock.SetLocal(local)
		return sock
	}
	sock := NewNode(name)
	sock.Local = local
	_ = n.AddChild(sock)
	n.sockets[name] = sock
	return sock
}

// Socket returns the node for a named socket on n, checking sockets defined on
// n first and then searching n's descendants by name (for example, skeleton
// bone nodes). Returns nil if no matching socket exists.
func (n *Node) Socket(name string) *Node {
	if n == nil || name == "" {
		return nil
	}
	if sock, ok := n.sockets[name]; ok {
		return sock
	}
	for _, c := range n.children {
		if found := c.FindByName(name); found != nil {
			return found
		}
	}
	return nil
}

// Attach mounts child onto the named socket point of n.
//
// If socket is empty, child is added directly to n. Otherwise, Attach resolves
// an existing defined socket or descendant node with that name, or creates a
// new identity socket node under n if none exists yet. Cycle checks run before
// creating any implicit socket so failed calls leave n unmodified.
func (n *Node) Attach(socket string, child *Node) error {
	if n == nil || child == nil {
		return ErrNilNode
	}
	if child == n || n.IsDescendantOf(child) {
		return ErrCycle
	}
	if socket == "" {
		return n.AddChild(child)
	}
	sock := n.Socket(socket)
	if sock != nil {
		return sock.AddChild(child)
	}
	sock = n.DefineSocket(socket, math3d.TransformIdentity())
	return sock.AddChild(child)
}

// Attached returns a shallow copy of the nodes currently attached to the named
// socket on n, or nil if the socket does not exist.
func (n *Node) Attached(socket string) []*Node {
	sock := n.Socket(socket)
	if sock == nil {
		return nil
	}
	return sock.Children()
}

// SetAttachment stores an arbitrary named attachment value (such as a mesh,
// light, audio emitter, or gameplay component) on n.
func (n *Node) SetAttachment(key string, value any) {
	if n == nil {
		return
	}
	if n.Attachments == nil {
		n.Attachments = make(map[string]any)
	}
	n.Attachments[key] = value
}

// Attachment retrieves the named attachment value from n.
func (n *Node) Attachment(key string) (any, bool) {
	if n == nil || n.Attachments == nil {
		return nil, false
	}
	v, ok := n.Attachments[key]
	return v, ok
}

// RemoveAttachment deletes the named attachment key from n.
func (n *Node) RemoveAttachment(key string) {
	if n == nil || n.Attachments == nil {
		return
	}
	delete(n.Attachments, key)
}

// AttachmentAs retrieves the named attachment from n and asserts it to type T.
func AttachmentAs[T any](n *Node, key string) (T, bool) {
	var zero T
	v, ok := n.Attachment(key)
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

package scene3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// RayHit describes an intersection between a ray and a scene node.
type RayHit struct {
	Node          *Node
	T             float32     // Parameter along the ray such that Point == ray.At(T)
	Point         math3d.Vec3 // Intersection point in world space
	Normal        math3d.Vec3 // Outward unit surface normal in world space
	TriangleIndex int         // Hit triangle index for MeshCollider, or -1 otherwise
}

// Scene owns a root Node and a dynamic [BVH] spatial index for O(log N)
// frustum culling, volume queries, and raycasts over visible, enabled nodes.
type Scene struct {
	root             *Node
	bvh              *BVH
	dirtyBVH         []*Node
	queryScratch     []BVHHandle
	nextNodeID       uint64
	lastDirtyUpdates int
	visGen           uint64
	visPath          []*Node
}

// beginVisibilityQuery starts a new memoization generation for activeVisible.
func (s *Scene) beginVisibilityQuery() {
	s.visGen++
}

// activeVisible reports whether n and every ancestor are Visible and Enabled, memoizing the answer for every node on the path in the current query generation. A query over k hits in a tree of depth d costs O(k + nodes on their paths) instead of O(k*d).
func (s *Scene) activeVisible(n *Node) bool {
	if n == nil {
		return false
	}
	gen := s.visGen
	path := s.visPath[:0]
	result := true
	for p := n; p != nil; p = p.parent {
		if p.visGen == gen {
			result = p.visOK
			break
		}
		path = append(path, p)
	}
	for i := len(path) - 1; i >= 0; i-- {
		q := path[i]
		result = result && q.Visible && q.Enabled
		q.visGen, q.visOK = gen, result
	}
	clear(path)
	s.visPath = path[:0]
	return n.visOK
}

// NewScene returns a new Scene with an initialized root Node named "root" and
// an empty dynamic BVH spatial index.
func NewScene() *Scene {
	s := &Scene{
		bvh: NewBVH(),
	}
	s.root = NewNode("root")
	s.root.attachToScene(s)
	return s
}

// Root returns the scene's root node, initializing one lazily if needed.
func (s *Scene) Root() *Node {
	if s.root == nil {
		s.root = NewNode("root")
		s.root.attachToScene(s)
	}
	return s.root
}

func (s *Scene) ensureBVH() *BVH {
	if s.bvh == nil {
		s.bvh = NewBVH()
	}
	return s.bvh
}

func (s *Scene) queueBVH(n *Node) {
	if n == nil || n.bvhQueued {
		return
	}
	if !n.hasBVH && n.LocalBounds.Empty() && n.Collider == nil {
		return
	}
	n.bvhQueued = true
	s.dirtyBVH = append(s.dirtyBVH, n)
}

func (s *Scene) syncNodeBVH(n *Node) {
	bvh := s.ensureBVH()
	bounds := n.WorldBounds()
	if bounds.Empty() {
		if n.hasBVH {
			bvh.Remove(n.bvhHandle)
			n.bvhHandle = BVHHandle{}
			n.hasBVH = false
		}
		return
	}
	if n.hasBVH {
		bvh.Update(n.bvhHandle, bounds)
	} else {
		n.bvhHandle = bvh.Insert(n.id, bounds, n)
		n.hasBVH = n.bvhHandle.Valid()
	}
}

// BVH synchronizes pending node updates and returns the scene's underlying
// dynamic BVH.
func (s *Scene) BVH() *BVH {
	s.SyncBVH()
	return s.ensureBVH()
}

// SyncBVH incrementally updates the scene's BVH for nodes whose transforms,
// bounds, or hierarchy membership changed since the last sync. When no nodes
// are dirty, SyncBVH runs in O(1) time.
func (s *Scene) SyncBVH() {
	if s == nil || len(s.dirtyBVH) == 0 {
		return
	}
	s.ensureBVH()
	for i := 0; i < len(s.dirtyBVH); i++ {
		n := s.dirtyBVH[i]
		s.dirtyBVH[i] = nil
		if n == nil || n.scene != s || !n.bvhQueued {
			continue
		}
		n.bvhQueued = false
		s.syncNodeBVH(n)
	}
	s.dirtyBVH = s.dirtyBVH[:0]
}

// RebuildBVH updates dirty node transforms, synchronizes all scene node bounds,
// and reconstructs the scene's BVH top-down using the Surface Area Heuristic
// (SAH) for optimal query performance.
func (s *Scene) RebuildBVH() {
	if s == nil {
		return
	}
	s.UpdateTransforms()
	for i := range s.dirtyBVH {
		if n := s.dirtyBVH[i]; n != nil {
			n.bvhQueued = false
		}
		s.dirtyBVH[i] = nil
	}
	s.dirtyBVH = s.dirtyBVH[:0]

	s.Walk(func(n *Node) bool {
		n.bvhQueued = false
		s.syncNodeBVH(n)
		return true
	})
	s.ensureBVH().Rebuild()
}

// UpdateTransforms traverses the scene hierarchy top-down and recomputes cached
// world transforms and matrices only for dirty nodes (including nodes whose
// Local transform was mutated directly or whose ancestor changed). Clean nodes
// perform zero matrix recomputations.
//
// It returns the number of nodes recomputed and records that count for
// [Scene.DirtyUpdates].
func (s *Scene) UpdateTransforms() int {
	if s == nil {
		return 0
	}
	root := s.Root()
	updated := updateNodeTransforms(root, false)
	s.lastDirtyUpdates = updated
	return updated
}

// DirtyUpdates returns the number of node world transforms recomputed during
// the most recent call to [Scene.UpdateTransforms].
func (s *Scene) DirtyUpdates() int {
	if s == nil {
		return 0
	}
	return s.lastDirtyUpdates
}

func updateNodeTransforms(n *Node, parentUpdated bool) int {
	updated := 0
	need := parentUpdated || n.dirty || !n.initialized || n.Local != n.cachedLocal
	if need {
		if n.parent != nil {
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
		if n.scene != nil {
			n.scene.queueBVH(n)
		}
		updated = 1
	}
	for _, c := range n.children {
		updated += updateNodeTransforms(c, need)
	}
	return updated
}

// Add attaches node as a direct child of the scene root.
func (s *Scene) Add(node *Node) error {
	return s.Root().AddChild(node)
}

// Remove detaches node from its parent in the scene.
func (s *Scene) Remove(node *Node) bool {
	if node == nil || node.parent == nil {
		return false
	}
	return node.parent.RemoveChild(node)
}

// FindByName searches the scene root and its descendants for the first node
// named name.
func (s *Scene) FindByName(name string) *Node {
	return s.Root().FindByName(name)
}

// Walk traverses every node in the scene starting at Root in pre-order.
func (s *Scene) Walk(fn func(*Node) bool) {
	s.Root().Walk(fn)
}

// CullFrustum returns all visible, enabled nodes whose world bounds intersect
// frustum, accelerated by the scene's dynamic BVH.
func (s *Scene) CullFrustum(frustum math3d.Frustum) []*Node {
	return s.CullFrustumInto(frustum, nil)
}

// CullFrustumInto appends all visible, enabled nodes whose world bounds
// intersect frustum to out using the scene's dynamic BVH. Pass a reused slice
// (e.g. buf[:0]) for zero-allocation per-frame culling.
func (s *Scene) CullFrustumInto(frustum math3d.Frustum, out []*Node) []*Node {
	if s == nil {
		return out
	}
	root := s.Root()
	if !root.Visible || !root.Enabled {
		return out
	}
	s.SyncBVH()
	bvh := s.ensureBVH()
	s.beginVisibilityQuery()
	handles := bvh.QueryFrustum(frustum, s.queryScratch[:0])
	s.queryScratch = handles
	for _, h := range handles {
		n, ok := bvh.leaves[h.index-1].userData.(*Node)
		if ok && s.activeVisible(n) {
			out = append(out, n)
		}
	}
	return out
}

// QueryFrustum returns all visible, enabled nodes whose world bounds intersect
// the frustum extracted from view-projection matrix vp (assuming math3d's
// default [0, 1] NDC depth).
func (s *Scene) QueryFrustum(vp math3d.Mat4) []*Node {
	return s.CullFrustum(math3d.FrustumFromMat4(vp))
}

// QueryFrustumVolume returns all visible, enabled nodes whose world bounds
// intersect f.
func (s *Scene) QueryFrustumVolume(f math3d.Frustum) []*Node {
	return s.CullFrustum(f)
}

// QueryAABB returns all visible, enabled nodes whose world bounds overlap or
// touch box, accelerated by the scene's dynamic BVH.
func (s *Scene) QueryAABB(box math3d.AABB) []*Node {
	if s == nil || box.Empty() {
		return nil
	}
	root := s.Root()
	if !root.Visible || !root.Enabled {
		return nil
	}
	s.SyncBVH()
	bvh := s.ensureBVH()
	s.beginVisibilityQuery()
	handles := bvh.QueryAABB(box, s.queryScratch[:0])
	s.queryScratch = handles
	if len(handles) == 0 {
		return nil
	}
	out := make([]*Node, 0, len(handles))
	for _, h := range handles {
		n, ok := bvh.leaves[h.index-1].userData.(*Node)
		if ok && s.activeVisible(n) {
			out = append(out, n)
		}
	}
	return out
}

// QuerySphere returns all visible, enabled nodes whose world bounds overlap or
// touch sphere, accelerated by the scene's dynamic BVH.
func (s *Scene) QuerySphere(sphere math3d.Sphere) []*Node {
	if s == nil || sphere.Radius < 0 {
		return nil
	}
	root := s.Root()
	if !root.Visible || !root.Enabled {
		return nil
	}
	s.SyncBVH()
	bvh := s.ensureBVH()
	s.beginVisibilityQuery()
	handles := bvh.QuerySphere(sphere.Center, sphere.Radius, s.queryScratch[:0])
	s.queryScratch = handles
	if len(handles) == 0 {
		return nil
	}
	out := make([]*Node, 0, len(handles))
	for _, h := range handles {
		n, ok := bvh.leaves[h.index-1].userData.(*Node)
		if ok && s.activeVisible(n) {
			out = append(out, n)
		}
	}
	return out
}

// Raycast casts ray against all visible, enabled nodes in s matching layerMask,
// performing BVH broad-phase world AABB culling and optional narrow-phase
// Collider tests. Hits are returned sorted by ascending ray parameter T.
func (s *Scene) Raycast(ray math3d.Ray, layerMask uint32) []RayHit {
	if s == nil || layerMask == 0 {
		return nil
	}
	root := s.Root()
	if !root.Visible || !root.Enabled {
		return nil
	}
	s.SyncBVH()
	bvh := s.ensureBVH()
	s.beginVisibilityQuery()
	var hits []RayHit
	bvh.raycastShrink(ray, float32(math.Inf(1)), func(_ BVHHandle, userData any, _ float32) (float32, bool) {
		n, ok := userData.(*Node)
		if !ok || n.Layer&layerMask == 0 || !s.activeVisible(n) {
			return float32(math.Inf(1)), true
		}
		if hit, ok := intersectNode(n, ray); ok {
			hits = append(hits, hit)
		}
		return float32(math.Inf(1)), true
	})
	slices.SortStableFunc(hits, func(a, b RayHit) int {
		if a.T < b.T {
			return -1
		}
		if a.T > b.T {
			return 1
		}
		return 0
	})
	return hits
}

// RaycastFirst casts ray against all visible, enabled nodes in s matching
// layerMask and returns the closest intersection (smallest T >= 0), or false
// if nothing was hit. Uses BVH near-to-far traversal with dynamic distance
// pruning.
func (s *Scene) RaycastFirst(ray math3d.Ray, layerMask uint32) (RayHit, bool) {
	if s == nil || layerMask == 0 {
		return RayHit{}, false
	}
	root := s.Root()
	if !root.Visible || !root.Enabled {
		return RayHit{}, false
	}
	s.SyncBVH()
	bvh := s.ensureBVH()
	s.beginVisibilityQuery()
	var best RayHit
	bestT := float32(math.Inf(1))
	found := false
	bvh.raycastShrink(ray, bestT, func(_ BVHHandle, userData any, _ float32) (float32, bool) {
		n, ok := userData.(*Node)
		if !ok || n.Layer&layerMask == 0 || !s.activeVisible(n) {
			return bestT, true
		}
		hit, ok := intersectNode(n, ray)
		if !ok {
			return bestT, true
		}
		if !found || hit.T < bestT {
			best = hit
			bestT = hit.T
			found = true
		}
		return bestT, true
	})
	return best, found
}

func intersectNode(n *Node, ray math3d.Ray) (RayHit, bool) {
	bounds := n.WorldBounds()
	if bounds.Empty() {
		return RayHit{}, false
	}
	tMin, _, ok := ray.IntersectAABB(bounds)
	if !ok {
		return RayHit{}, false
	}
	if n.Collider != nil {
		t, normal, triIdx, hit := n.Collider.IntersectRay(ray, n.WorldMatrix())
		if !hit {
			return RayHit{}, false
		}
		return RayHit{
			Node:          n,
			T:             t,
			Point:         ray.At(t),
			Normal:        normal,
			TriangleIndex: triIdx,
		}, true
	}
	pt := ray.At(tMin)
	return RayHit{
		Node:          n,
		T:             tMin,
		Point:         pt,
		Normal:        aabbHitNormal(bounds, pt, ray, tMin),
		TriangleIndex: -1,
	}, true
}

func aabbHitNormal(b math3d.AABB, pt math3d.Vec3, ray math3d.Ray, tMin float32) math3d.Vec3 {
	if tMin == 0 && b.Contains(ray.Origin) {
		return ray.Dir.Neg().Norm()
	}
	bestDist := float32(math.Inf(1))
	var bestN math3d.Vec3

	check := func(d float32, n math3d.Vec3) {
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist = d
			bestN = n
		}
	}
	check(pt.X-b.Min.X, math3d.Vec3{X: -1})
	check(pt.X-b.Max.X, math3d.Vec3{X: 1})
	check(pt.Y-b.Min.Y, math3d.Vec3{Y: -1})
	check(pt.Y-b.Max.Y, math3d.Vec3{Y: 1})
	check(pt.Z-b.Min.Z, math3d.Vec3{Z: -1})
	check(pt.Z-b.Max.Z, math3d.Vec3{Z: 1})
	return bestN
}

// SceneObstructionCheck returns a ThirdPersonController ObstructionCheck
// callback backed by BVH-accelerated scene raycasts against visible, enabled
// nodes matching layerMask. Any node listed in ignore (such as the followed
// player character) is skipped.
func SceneObstructionCheck(scene *Scene, layerMask uint32, ignore ...*Node) func(from, to math3d.Vec3) (float32, bool) {
	return func(from, to math3d.Vec3) (float32, bool) {
		if scene == nil || layerMask == 0 {
			return 0, false
		}
		root := scene.Root()
		if !root.Visible || !root.Enabled {
			return 0, false
		}
		dir := to.Sub(from)
		if dir.LenSq() < 1e-12 {
			return 0, false
		}
		scene.SyncBVH()
		bvh := scene.ensureBVH()
		scene.beginVisibilityQuery()
		ray := math3d.Ray{Origin: from, Dir: dir}
		bestT := float32(1.0)
		blocked := false
		bvh.raycastShrink(ray, bestT, func(_ BVHHandle, userData any, _ float32) (float32, bool) {
			n, ok := userData.(*Node)
			if !ok || n.Layer&layerMask == 0 || !scene.activeVisible(n) {
				return bestT, true
			}
			for _, ig := range ignore {
				if n == ig || n.IsDescendantOf(ig) {
					return bestT, true
				}
			}
			hit, ok := intersectNode(n, ray)
			if !ok || hit.T < 0 || hit.T > bestT {
				return bestT, true
			}
			if !blocked || hit.T < bestT {
				bestT = hit.T
				blocked = true
			}
			return bestT, true
		})
		if !blocked {
			return 0, false
		}
		return bestT, true
	}
}

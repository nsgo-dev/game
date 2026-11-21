package scene3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// RayHit describes an intersection between a ray and a scene node.
type RayHit struct {
	Node          *Node
	T             float32     // Parameter along the ray such that Point == ray.At(T)
	Point         math3d.Vec3 // Intersection point in world space
	Normal        math3d.Vec3 // Outward unit surface normal in world space
	TriangleIndex int         // Hit triangle index for MeshCollider, or -1 otherwise
}

// Scene owns a root Node and provides spatial queries over its visible nodes.
type Scene struct {
	root *Node
}

// NewScene returns a new Scene with an initialized root Node named "root".
func NewScene() *Scene {
	return &Scene{root: NewNode("root")}
}

// Root returns the scene's root node, initializing one lazily if needed.
func (s *Scene) Root() *Node {
	if s.root == nil {
		s.root = NewNode("root")
	}
	return s.root
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

// walkVisible visits all visible nodes in the scene, pruning entire subtrees
// rooted at any node with Visible == false.
func (s *Scene) walkVisible(fn func(*Node)) {
	root := s.Root()
	if !root.Visible {
		return
	}
	var visit func(*Node)
	visit = func(n *Node) {
		fn(n)
		for _, c := range n.children {
			if c.Visible {
				visit(c)
			}
		}
	}
	visit(root)
}

// QueryFrustum returns all visible nodes whose world bounds intersect the
// frustum extracted from view-projection matrix vp (assuming math3d's default
// [0, 1] NDC depth).
func (s *Scene) QueryFrustum(vp math3d.Mat4) []*Node {
	return s.QueryFrustumVolume(math3d.FrustumFromMat4(vp))
}

// QueryFrustumVolume returns all visible nodes whose world bounds intersect f.
func (s *Scene) QueryFrustumVolume(f math3d.Frustum) []*Node {
	var out []*Node
	s.walkVisible(func(n *Node) {
		b := n.WorldBounds()
		if !b.Empty() && f.IntersectsAABB(b) {
			out = append(out, n)
		}
	})
	return out
}

// QueryAABB returns all visible nodes whose world bounds overlap or touch box.
func (s *Scene) QueryAABB(box math3d.AABB) []*Node {
	if box.Empty() {
		return nil
	}
	var out []*Node
	s.walkVisible(func(n *Node) {
		b := n.WorldBounds()
		if !b.Empty() && b.Intersects(box) {
			out = append(out, n)
		}
	})
	return out
}

// QuerySphere returns all visible nodes whose world bounds overlap or touch sphere.
func (s *Scene) QuerySphere(sphere math3d.Sphere) []*Node {
	if sphere.Radius < 0 {
		return nil
	}
	var out []*Node
	s.walkVisible(func(n *Node) {
		b := n.WorldBounds()
		if !b.Empty() && sphere.IntersectsAABB(b) {
			out = append(out, n)
		}
	})
	return out
}

// Raycast casts ray against all visible nodes in s matching layerMask,
// performing broad-phase world AABB culling and optional narrow-phase Collider
// tests. Hits are returned sorted by ascending ray parameter T.
func (s *Scene) Raycast(ray math3d.Ray, layerMask uint32) []RayHit {
	if layerMask == 0 {
		return nil
	}
	var hits []RayHit
	s.walkVisible(func(n *Node) {
		if n.Layer&layerMask == 0 {
			return
		}
		if hit, ok := intersectNode(n, ray); ok {
			hits = append(hits, hit)
		}
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

// RaycastFirst casts ray against all visible nodes in s matching layerMask and
// returns the closest intersection (smallest T >= 0), or false if nothing was hit.
func (s *Scene) RaycastFirst(ray math3d.Ray, layerMask uint32) (RayHit, bool) {
	if layerMask == 0 {
		return RayHit{}, false
	}
	var best RayHit
	found := false
	s.walkVisible(func(n *Node) {
		if n.Layer&layerMask == 0 {
			return
		}
		hit, ok := intersectNode(n, ray)
		if !ok {
			return
		}
		if !found || hit.T < best.T {
			best = hit
			found = true
		}
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
// callback backed by scene raycasts against visible nodes matching layerMask.
// Any node listed in ignore (such as the followed player character) is skipped.
func SceneObstructionCheck(scene *Scene, layerMask uint32, ignore ...*Node) func(from, to math3d.Vec3) (float32, bool) {
	return func(from, to math3d.Vec3) (float32, bool) {
		if scene == nil || layerMask == 0 {
			return 0, false
		}
		dir := to.Sub(from)
		if dir.LenSq() < 1e-12 {
			return 0, false
		}
		ray := math3d.Ray{Origin: from, Dir: dir}
		bestT := float32(math.Inf(1))
		blocked := false
		scene.walkVisible(func(n *Node) {
			if n.Layer&layerMask == 0 {
				return
			}
			for _, ig := range ignore {
				if n == ig || n.IsDescendantOf(ig) {
					return
				}
			}
			hit, ok := intersectNode(n, ray)
			if !ok || hit.T < 0 || hit.T > 1 {
				return
			}
			if hit.T < bestT {
				bestT = hit.T
				blocked = true
			}
		})
		if !blocked {
			return 0, false
		}
		return bestT, true
	}
}

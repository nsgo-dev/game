package scene3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// Collider provides optional narrow-phase ray intersection for a Node after
// broad-phase world AABB testing succeeds.
type Collider interface {
	// LocalBounds returns the local-space AABB enclosing this collider.
	LocalBounds() math3d.AABB
	// IntersectRay tests a world-space ray against this collider transformed by
	// worldMat. On a hit, it returns ray parameter t >= 0 (with worldRay.At(t)
	// equal to the world hit point), unit world-space normal, triangle index
	// (-1 if not applicable), and true.
	IntersectRay(worldRay math3d.Ray, worldMat math3d.Mat4) (t float32, normal math3d.Vec3, triIndex int, hit bool)
}

// MeshCollider performs exact double-sided ray-triangle intersection against a
// triangle soup stored in node-local space. Every 3 consecutive vertices in
// Triangles form one triangle (v0, v1, v2).
//
// LocalBounds is cached: replacing or resizing Triangles is detected automatically, but after editing vertices of the same slice in place call MarkDirty. Either way the owning node's world bounds and BVH entry refresh only when the node is marked dirty, so call the node's MarkDirty (or SetCollider) after changing Triangles, or BVH-driven raycasts and picks keep testing the old bounds.
type MeshCollider struct {
	Triangles []math3d.Vec3

	boundsData *math3d.Vec3
	boundsLen  int
	bounds     math3d.AABB
	boundsOK   bool
}

// LocalBounds returns the tight local-space AABB containing all complete
// triangles in m.Triangles. The result is cached until Triangles is replaced or resized or MarkDirty is called, so node syncs and picks do not rescan every triangle (#546).
func (m *MeshCollider) LocalBounds() math3d.AABB {
	n := len(m.Triangles) - len(m.Triangles)%3
	if n == 0 {
		return math3d.AABBEmpty()
	}
	if m.boundsOK && m.boundsLen == len(m.Triangles) && m.boundsData == &m.Triangles[0] {
		return m.bounds
	}
	b := math3d.AABBEmpty()
	for i := range n {
		b = b.ExpandPoint(m.Triangles[i])
	}
	m.bounds, m.boundsData, m.boundsLen, m.boundsOK = b, &m.Triangles[0], len(m.Triangles), true
	return b
}

// MarkDirty drops the cached LocalBounds after vertices in Triangles were edited in place. Call the owning node's MarkDirty too so its world bounds and BVH entry refresh.
func (m *MeshCollider) MarkDirty() {
	m.boundsOK = false
}

// IntersectRay tests worldRay against every triangle in m using
// Ray.IntersectTriangle, returning the closest hit with t >= 0.
func (m *MeshCollider) IntersectRay(worldRay math3d.Ray, worldMat math3d.Mat4) (float32, math3d.Vec3, int, bool) {
	n := len(m.Triangles) - len(m.Triangles)%3
	if n == 0 {
		return 0, math3d.Vec3{}, -1, false
	}

	if invWorld, ok := worldMat.Inverse(); ok {
		localRay := math3d.Ray{
			Origin: invWorld.TransformPoint(worldRay.Origin),
			Dir:    invWorld.TransformDir(worldRay.Dir),
		}
		bestT := float32(math.Inf(1))
		bestTri := -1
		var localNormal math3d.Vec3
		for i := 0; i < n; i += 3 {
			v0, v1, v2 := m.Triangles[i], m.Triangles[i+1], m.Triangles[i+2]
			t, _, _, hit := localRay.IntersectTriangle(v0, v1, v2)
			if hit && t < bestT {
				bestT = t
				bestTri = i / 3
				localNormal = v1.Sub(v0).Cross(v2.Sub(v0))
			}
		}
		if bestTri < 0 {
			return 0, math3d.Vec3{}, -1, false
		}
		var worldN math3d.Vec3
		if nm, ok := math3d.NormalMatrix(worldMat); ok {
			worldN = nm.MulVec3(localNormal).Norm()
		} else {
			worldN = worldMat.TransformDir(localNormal).Norm()
		}
		return bestT, worldN, bestTri, true
	}

	// Fallback for singular transforms (e.g., flat zero-scale plane): transform
	// triangles directly into world space.
	bestT := float32(math.Inf(1))
	bestTri := -1
	var worldNormal math3d.Vec3
	for i := 0; i < n; i += 3 {
		v0 := worldMat.TransformPoint(m.Triangles[i])
		v1 := worldMat.TransformPoint(m.Triangles[i+1])
		v2 := worldMat.TransformPoint(m.Triangles[i+2])
		t, _, _, hit := worldRay.IntersectTriangle(v0, v1, v2)
		if hit && t < bestT {
			bestT = t
			bestTri = i / 3
			worldNormal = v1.Sub(v0).Cross(v2.Sub(v0)).Norm()
		}
	}
	if bestTri < 0 {
		return 0, math3d.Vec3{}, -1, false
	}
	return bestT, worldNormal, bestTri, true
}

// SphereCollider performs exact ray-sphere intersection in node-local space.
type SphereCollider struct {
	Center math3d.Vec3
	Radius float32
}

// LocalBounds returns the tight local-space AABB enclosing s.
func (s *SphereCollider) LocalBounds() math3d.AABB {
	if s.Radius < 0 {
		return math3d.AABBEmpty()
	}
	ext := math3d.Vec3{X: s.Radius, Y: s.Radius, Z: s.Radius}
	return math3d.AABB{
		Min: s.Center.Sub(ext),
		Max: s.Center.Add(ext),
	}
}

// IntersectRay tests worldRay against s in local space (accurately supporting
// arbitrary rotation and non-uniform scale).
func (s *SphereCollider) IntersectRay(worldRay math3d.Ray, worldMat math3d.Mat4) (float32, math3d.Vec3, int, bool) {
	if s.Radius < 0 {
		return 0, math3d.Vec3{}, -1, false
	}
	invWorld, ok := worldMat.Inverse()
	if !ok {
		return 0, math3d.Vec3{}, -1, false
	}
	localRay := math3d.Ray{
		Origin: invWorld.TransformPoint(worldRay.Origin),
		Dir:    invWorld.TransformDir(worldRay.Dir),
	}
	t, hit := localRay.IntersectSphere(math3d.Sphere{Center: s.Center, Radius: s.Radius})
	if !hit {
		return 0, math3d.Vec3{}, -1, false
	}
	localPt := localRay.At(t)
	localN := localPt.Sub(s.Center)
	if localN.LenSq() < 1e-12 {
		localN = localRay.Dir.Neg()
	}
	var worldN math3d.Vec3
	if nm, ok := math3d.NormalMatrix(worldMat); ok {
		worldN = nm.MulVec3(localN).Norm()
	} else {
		worldN = worldMat.TransformDir(localN).Norm()
	}
	return t, worldN, -1, true
}

// Pick unprojects screen pixel coordinates (screenX, screenY) in a viewport of
// size (viewW, viewH) through cam and returns the closest hit visible node in
// scene matching layerMask.
func Pick(scene *Scene, cam *Camera, screenX, screenY, viewW, viewH float32, layerMask uint32) (RayHit, bool) {
	if scene == nil || cam == nil || viewW <= 0 || viewH <= 0 {
		return RayHit{}, false
	}
	ray := cam.ScreenRay(screenX, screenY, viewW, viewH)
	return scene.RaycastFirst(ray, layerMask)
}

// PickAll unprojects screen pixel coordinates (screenX, screenY) in a viewport
// of size (viewW, viewH) through cam and returns all hit visible nodes in scene
// matching layerMask sorted by ascending distance from the ray origin.
func PickAll(scene *Scene, cam *Camera, screenX, screenY, viewW, viewH float32, layerMask uint32) []RayHit {
	if scene == nil || cam == nil || viewW <= 0 || viewH <= 0 {
		return nil
	}
	ray := cam.ScreenRay(screenX, screenY, viewW, viewH)
	return scene.Raycast(ray, layerMask)
}

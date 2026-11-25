package physics3d

import (
	"math"
	"slices"
)

// VoxelGrid supplies direct solid-block occupancy queries for a VoxelCollider.
// Voxel (x, y, z) occupies the world-space cube from
// Origin + (x, y, z)*VoxelSize to Origin + (x+1, y+1, z+1)*VoxelSize.
type VoxelGrid interface {
	// VoxelSize returns the positive world-space edge length of one voxel.
	VoxelSize() float32
	// IsSolid reports whether the voxel at integer coordinate (x, y, z) is solid.
	IsSolid(x, y, z int) bool
}

// VoxelBoundsGrid is an optional interface a VoxelGrid may implement so
// VoxelCollider.Raycast can reject rays outside the populated world in O(1)
// and clip long rays to the active voxel region.
type VoxelBoundsGrid interface {
	VoxelGrid
	// VoxelBounds returns the inclusive world-space AABB enclosing all solid
	// voxels in the grid's local coordinates (before adding VoxelCollider.Origin),
	// and false when no solid voxels exist.
	VoxelBounds() (min, max Vec3, ok bool)
}

// VoxelEditNotifier is an optional interface a VoxelGrid may implement so
// VoxelCollider automatically wakes sleeping dynamic bodies in attached Worlds
// whenever a block changes solidity or the grid is cleared.
type VoxelEditNotifier interface {
	VoxelGrid
	// WatchVoxelEdits registers callbacks invoked whenever a voxel changes
	// solidity (onBlock) or the grid is reset (onReset). Calling the returned
	// cancel function unregisters the callbacks.
	WatchVoxelEdits(onBlock func(x, y, z int), onReset func()) (cancel func())
}

// SweepHit describes the first contact encountered when sweeping a Shape along
// a displacement vector against a VoxelCollider.
type SweepHit struct {
	// T is the normalized hit fraction in [0, 1] along the sweep displacement.
	T float32
	// Distance is the Euclidean distance traveled along the sweep before contact.
	Distance float32
	// Point is the world-space contact point on the struck surface.
	Point Vec3
	// Normal is the outward unit surface normal pointing toward the swept shape.
	Normal Vec3
	// Depth is the penetration depth when the shape starts already overlapping (T == 0).
	Depth float32
	// Voxel is the struck VoxelCollider.
	Voxel *VoxelCollider
	// BlockX, BlockY, BlockZ identify the integer voxel coordinate struck.
	BlockX, BlockY, BlockZ int
}

// VoxelCollider collides bodies, characters, rays, overlaps, and shape sweeps
// directly against a VoxelGrid without building a triangle mesh. Edits to the
// underlying VoxelGrid take effect immediately with zero rebuild.
type VoxelCollider struct {
	ID          int
	Tag         string
	Layer       LayerMask
	Mask        LayerMask
	Friction    float32
	Restitution float32
	Origin      Vec3
	Grid        VoxelGrid

	worlds      []*World
	cancelWatch func()
}

// NewVoxelCollider constructs a VoxelCollider backed by grid with default
// friction (0.5).
func NewVoxelCollider(grid VoxelGrid) *VoxelCollider {
	return &VoxelCollider{
		Friction: 0.5,
		Grid:     grid,
	}
}

func (vc *VoxelCollider) valid() bool {
	if vc == nil || vc.Grid == nil {
		return false
	}
	vs := vc.Grid.VoxelSize()
	return isFinite32(vs) && vs > 0
}

func (vc *VoxelCollider) voxelBox(vx, vy, vz int) Box {
	vs := vc.Grid.VoxelSize()
	half := vs * 0.5
	return Box{
		Center: Vec3{
			X: vc.Origin.X + float32((float32(vx)+0.5)*vs),
			Y: vc.Origin.Y + float32((float32(vy)+0.5)*vs),
			Z: vc.Origin.Z + float32((float32(vz)+0.5)*vs),
		},
		HalfExtents: Vec3{X: half, Y: half, Z: half},
	}
}

// NotifyBlockChanged wakes sleeping dynamic bodies near voxel (x, y, z) in
// every World to which vc is attached. VoxelGrid implementations that also
// implement VoxelEditNotifier invoke this automatically on block edits.
func (vc *VoxelCollider) NotifyBlockChanged(x, y, z int) {
	if !vc.valid() || len(vc.worlds) == 0 {
		return
	}
	vs := vc.Grid.VoxelSize()
	pad := max(vs*0.25, float32(0.1))
	vb := vc.voxelBox(x, y, z)
	wakeBox := Box{
		Center: vb.Center,
		HalfExtents: Vec3{
			X: vb.HalfExtents.X + pad,
			Y: vb.HalfExtents.Y + pad,
			Z: vb.HalfExtents.Z + pad,
		},
	}
	for _, w := range vc.worlds {
		w.WakeInBox(wakeBox)
	}
}

// NotifyReset wakes all dynamic bodies in every World to which vc is attached.
func (vc *VoxelCollider) NotifyReset() {
	if vc == nil || len(vc.worlds) == 0 {
		return
	}
	for _, w := range vc.worlds {
		w.wakeAllDynamic()
	}
}

func (vc *VoxelCollider) attachWorld(w *World) {
	if slices.Contains(vc.worlds, w) {
		return
	}
	vc.worlds = append(vc.worlds, w)
	if len(vc.worlds) == 1 && vc.cancelWatch == nil {
		if notifier, ok := vc.Grid.(VoxelEditNotifier); ok {
			vc.cancelWatch = notifier.WatchVoxelEdits(vc.NotifyBlockChanged, vc.NotifyReset)
		}
	}
}

func (vc *VoxelCollider) detachWorld(w *World) {
	for i, existing := range vc.worlds {
		if existing == w {
			vc.worlds = append(vc.worlds[:i], vc.worlds[i+1:]...)
			break
		}
	}
	if len(vc.worlds) == 0 && vc.cancelWatch != nil {
		vc.cancelWatch()
		vc.cancelWatch = nil
	}
}

func (vc *VoxelCollider) voxelRange(bounds Box) (x0, y0, z0, x1, y1, z1 int, ok bool) {
	if !vc.valid() {
		return 0, 0, 0, 0, 0, 0, false
	}
	bMin, bMax := bounds.Min(), bounds.Max()
	if !isFiniteVec(bMin) || !isFiniteVec(bMax) {
		return 0, 0, 0, 0, 0, 0, false
	}
	inv := 1.0 / float64(vc.Grid.VoxelSize())
	// Shrink the max boundary by a tiny epsilon so a box resting flush on an
	// integer voxel plane (e.g. minY == 1.0) does not scan an extra air/solid
	// layer needlessly while still catching any positive overlap.
	const eps = 1e-6
	x0 = int(math.Floor(float64(float64(bMin.X-vc.Origin.X)*inv) - eps))
	y0 = int(math.Floor(float64(float64(bMin.Y-vc.Origin.Y)*inv) - eps))
	z0 = int(math.Floor(float64(float64(bMin.Z-vc.Origin.Z)*inv) - eps))
	x1 = int(math.Floor(float64(float64(bMax.X-vc.Origin.X)*inv) + eps))
	y1 = int(math.Floor(float64(float64(bMax.Y-vc.Origin.Y)*inv) + eps))
	z1 = int(math.Floor(float64(float64(bMax.Z-vc.Origin.Z)*inv) + eps))
	if x0 > x1 || y0 > y1 || z0 > z1 {
		return 0, 0, 0, 0, 0, 0, false
	}
	return x0, y0, z0, x1, y1, z1, true
}

// QueryBlocks visits every solid voxel overlapping bounds in deterministic
// ascending (y, z, x) order, passing its integer coordinate and world-space Box.
func (vc *VoxelCollider) QueryBlocks(bounds Box, visit func(x, y, z int, blockBox Box)) {
	if visit == nil {
		return
	}
	x0, y0, z0, x1, y1, z1, ok := vc.voxelRange(bounds)
	if !ok {
		return
	}
	g := vc.Grid
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				if g.IsSolid(x, y, z) {
					visit(x, y, z, vc.voxelBox(x, y, z))
				}
			}
		}
	}
}

type voxelFaceMask uint8

const (
	voxelFacePosX voxelFaceMask = 1 << iota
	voxelFaceNegX
	voxelFacePosY
	voxelFaceNegY
	voxelFacePosZ
	voxelFaceNegZ
	voxelFaceAll = voxelFacePosX | voxelFaceNegX | voxelFacePosY | voxelFaceNegY | voxelFacePosZ | voxelFaceNegZ
)

func (vc *VoxelCollider) solidNeighborMask(vx, vy, vz int) voxelFaceMask {
	g := vc.Grid
	var m voxelFaceMask
	if g.IsSolid(vx+1, vy, vz) {
		m |= voxelFacePosX
	}
	if g.IsSolid(vx-1, vy, vz) {
		m |= voxelFaceNegX
	}
	if g.IsSolid(vx, vy+1, vz) {
		m |= voxelFacePosY
	}
	if g.IsSolid(vx, vy-1, vz) {
		m |= voxelFaceNegY
	}
	if g.IsSolid(vx, vy, vz+1) {
		m |= voxelFacePosZ
	}
	if g.IsSolid(vx, vy, vz-1) {
		m |= voxelFaceNegZ
	}
	return m
}

func clampNormalOccluded(n Vec3, occluded voxelFaceMask) Vec3 {
	if n.X > 0 && occluded&voxelFacePosX != 0 {
		n.X = 0
	}
	if n.X < 0 && occluded&voxelFaceNegX != 0 {
		n.X = 0
	}
	if n.Y > 0 && occluded&voxelFacePosY != 0 {
		n.Y = 0
	}
	if n.Y < 0 && occluded&voxelFaceNegY != 0 {
		n.Y = 0
	}
	if n.Z > 0 && occluded&voxelFacePosZ != 0 {
		n.Z = 0
	}
	if n.Z < 0 && occluded&voxelFaceNegZ != 0 {
		n.Z = 0
	}
	return n
}

func shapeMaxAlong(s Shape, n Vec3) float32 {
	return -shapeMinAlong(s, n.Neg())
}

func pointInsideBox(p, bMin, bMax Vec3) bool {
	return p.X >= bMin.X && p.X <= bMax.X &&
		p.Y >= bMin.Y && p.Y <= bMax.Y &&
		p.Z >= bMin.Z && p.Z <= bMax.Z
}

// testVoxel tests s against solid voxel (vx, vy, vz), filtering out internal
// faces occluded by adjacent solid voxels so sliding bodies and characters do
// not catch on flat floor, wall, or ceiling seams.
func (vc *VoxelCollider) testVoxel(s Shape, vx, vy, vz int) Contact {
	if !vc.valid() || !vc.Grid.IsSolid(vx, vy, vz) {
		return Contact{}
	}
	return vc.testVoxelWithMask(s, vc.voxelBox(vx, vy, vz), vc.solidNeighborMask(vx, vy, vz), false)
}

func (vc *VoxelCollider) testVoxelWithMask(s Shape, vb Box, occluded voxelFaceMask, allowBuried bool) Contact {
	if occluded == voxelFaceAll && !allowBuried {
		return Contact{}
	}

	bMin, bMax := vb.Min(), vb.Max()

	// Fast path for axis-aligned Box vs voxel: pick the shallowest exposed face
	// actually crossed by the box so internal seams and concave corners never
	// generate phantom side normals.
	if s.Kind == ShapeBox {
		if _, oriented := s.orientedBox(); !oriented {
			return testAABBvsVoxel(s.Box, vb, bMin, bMax, occluded, allowBuried)
		}
	}

	raw := TestShapes(s, BoxShape(vb))
	if !raw.Hit {
		return Contact{}
	}

	clamped := clampNormalOccluded(raw.Normal, occluded)
	if clampedSq := clamped.LenSq(); clampedSq > 1e-10 {
		if clamped == raw.Normal {
			return raw
		}
		n := clamped.Scale(1 / sqrt32(clampedSq))
		dot := raw.Normal.Dot(n)
		depth := raw.Depth * dot
		if depth <= 1e-7 {
			return Contact{}
		}
		raw.Normal = n
		raw.Depth = depth
		return raw
	}

	// All components of raw.Normal pointed into occluded neighbors.
	// If the shape's reference point is outside vb, this is an internal concave
	// edge/corner whose real contacts come from the exposed neighbor voxels.
	deepInside := false
	switch s.Kind {
	case ShapeSphere:
		deepInside = pointInsideBox(s.Sphere.Center, bMin, bMax)
	case ShapeCapsule:
		p := closestOrDeepestPointSegmentBox(s.Capsule.A, s.Capsule.B, vb)
		deepInside = pointBoxDistSq(p, bMin, bMax) <= 1e-10
	case ShapeBox:
		deepInside = pointInsideBox(s.Box.Center, bMin, bMax)
	case ShapeConvexHull:
		c := toCvx(s)
		deepInside = c.valid() && pointInsideBox(c.centroid().vec3(), bMin, bMax)
	case ShapeCylinder:
		deepInside = pointInsideBox(s.Cylinder.Center(), bMin, bMax)
	}

	if exposed, ok := bestExposedFaceContact(s, vb, bMin, bMax, occluded, deepInside, raw.Point); ok {
		return exposed
	}
	if allowBuried {
		return raw
	}
	return Contact{}
}

func testAABBvsVoxel(a, vb Box, bMin, bMax Vec3, occluded voxelFaceMask, allowBuried bool) Contact {
	d := a.Center.Sub(vb.Center)
	ox := a.HalfExtents.X + vb.HalfExtents.X - abs32(d.X)
	if ox <= 0 {
		return Contact{}
	}
	oy := a.HalfExtents.Y + vb.HalfExtents.Y - abs32(d.Y)
	if oy <= 0 {
		return Contact{}
	}
	oz := a.HalfExtents.Z + vb.HalfExtents.Z - abs32(d.Z)
	if oz <= 0 {
		return Contact{}
	}

	aMin, aMax := a.Min(), a.Max()
	point := Vec3{
		X: (max(aMin.X, bMin.X) + min(aMax.X, bMax.X)) * 0.5,
		Y: (max(aMin.Y, bMin.Y) + min(aMax.Y, bMax.Y)) * 0.5,
		Z: (max(aMin.Z, bMin.Z) + min(aMax.Z, bMax.Z)) * 0.5,
	}

	// Only consider exposed faces of vb that a actually straddles.
	bestDepth := float32(math.MaxFloat32)
	var bestNormal Vec3
	found := false

	consider := func(mask voxelFaceMask, crossed bool, depth float32, n Vec3) {
		if occluded&mask != 0 || !crossed || depth <= 0 {
			return
		}
		if depth < bestDepth {
			bestDepth = depth
			bestNormal = n
			found = true
		}
	}

	// Evaluate Y before X/Z ties so vertical floor/ceiling contacts stay stable.
	consider(voxelFacePosY, aMin.Y < bMax.Y && aMax.Y > bMax.Y, bMax.Y-aMin.Y, Vec3{Y: 1})
	consider(voxelFaceNegY, aMin.Y < bMin.Y && aMax.Y > bMin.Y, aMax.Y-bMin.Y, Vec3{Y: -1})
	consider(voxelFacePosX, aMin.X < bMax.X && aMax.X > bMax.X, bMax.X-aMin.X, Vec3{X: 1})
	consider(voxelFaceNegX, aMin.X < bMin.X && aMax.X > bMin.X, aMax.X-bMin.X, Vec3{X: -1})
	consider(voxelFacePosZ, aMin.Z < bMax.Z && aMax.Z > bMax.Z, bMax.Z-aMin.Z, Vec3{Z: 1})
	consider(voxelFaceNegZ, aMin.Z < bMin.Z && aMax.Z > bMin.Z, aMax.Z-bMin.Z, Vec3{Z: -1})

	if found {
		return Contact{
			Hit:    true,
			Normal: bestNormal,
			Depth:  bestDepth,
			Point:  point,
		}
	}

	// If a is strictly contained inside vb (crosses no face plane at all),
	// push out through the shallowest exposed face.
	if aMin.X >= bMin.X && aMax.X <= bMax.X &&
		aMin.Y >= bMin.Y && aMax.Y <= bMax.Y &&
		aMin.Z >= bMin.Z && aMax.Z <= bMax.Z {
		consider(voxelFacePosY, true, bMax.Y-aMin.Y, Vec3{Y: 1})
		consider(voxelFaceNegY, true, aMax.Y-bMin.Y, Vec3{Y: -1})
		consider(voxelFacePosX, true, bMax.X-aMin.X, Vec3{X: 1})
		consider(voxelFaceNegX, true, aMax.X-bMin.X, Vec3{X: -1})
		consider(voxelFacePosZ, true, bMax.Z-aMin.Z, Vec3{Z: 1})
		consider(voxelFaceNegZ, true, aMax.Z-bMin.Z, Vec3{Z: -1})
		if found {
			return Contact{
				Hit:    true,
				Normal: bestNormal,
				Depth:  bestDepth,
				Point:  point,
			}
		}
	}

	if allowBuried {
		return TestBoxBox(a, vb)
	}
	return Contact{}
}

func bestExposedFaceContact(s Shape, vb Box, bMin, bMax Vec3, occluded voxelFaceMask, allowUnstraddled bool, fallbackPoint Vec3) (Contact, bool) {
	type faceDef struct {
		mask  voxelFaceMask
		n     Vec3
		plane float32
	}
	faces := [6]faceDef{
		{voxelFacePosY, Vec3{Y: 1}, bMax.Y},
		{voxelFaceNegY, Vec3{Y: -1}, -bMin.Y},
		{voxelFacePosX, Vec3{X: 1}, bMax.X},
		{voxelFaceNegX, Vec3{X: -1}, -bMin.X},
		{voxelFacePosZ, Vec3{Z: 1}, bMax.Z},
		{voxelFaceNegZ, Vec3{Z: -1}, -bMin.Z},
	}

	bestDepth := float32(math.MaxFloat32)
	var bestN Vec3
	found := false
	for _, f := range faces {
		if occluded&f.mask != 0 {
			continue
		}
		sMin := shapeMinAlong(s, f.n)
		sMax := shapeMaxAlong(s, f.n)
		if !allowUnstraddled && !(sMin < f.plane && sMax > f.plane) {
			continue
		}
		depth := f.plane - sMin
		if depth > 0 && depth < bestDepth {
			bestDepth = depth
			bestN = f.n
			found = true
		}
	}
	if !found {
		return Contact{}, false
	}
	_ = vb
	return Contact{
		Hit:    true,
		Normal: bestN,
		Depth:  bestDepth,
		Point:  fallbackPoint,
	}, true
}

func (vc *VoxelCollider) forEachContact(s Shape, visit func(vx, vy, vz int, c Contact)) {
	x0, y0, z0, x1, y1, z1, ok := vc.voxelRange(s.Bounds())
	if !ok {
		return
	}
	g := vc.Grid
	hitAny := false
	var firstBuried Contact
	var bx, by, bz int
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				if !g.IsSolid(x, y, z) {
					continue
				}
				vb := vc.voxelBox(x, y, z)
				mask := vc.solidNeighborMask(x, y, z)
				if c := vc.testVoxelWithMask(s, vb, mask, false); c.Hit {
					hitAny = true
					visit(x, y, z, c)
				} else if !hitAny && mask == voxelFaceAll {
					if bc := vc.testVoxelWithMask(s, vb, mask, true); bc.Hit && (!firstBuried.Hit || bc.Depth > firstBuried.Depth) {
						firstBuried = bc
						bx, by, bz = x, y, z
					}
				}
			}
		}
	}
	if !hitAny && firstBuried.Hit {
		visit(bx, by, bz, firstBuried)
	}
}

// TestShape returns the deepest Contact between s and solid blocks in vc
// (normal pointing from the voxel terrain toward s).
func (vc *VoxelCollider) TestShape(s Shape) Contact {
	var best Contact
	vc.forEachContact(s, func(_, _, _ int, c Contact) {
		if !best.Hit || c.Depth > best.Depth {
			best = c
		}
	})
	return best
}

// CollectContacts appends all overlapping exposed solid-voxel contacts for s
// to dst in deterministic (y, z, x) order.
func (vc *VoxelCollider) CollectContacts(s Shape, dst []Contact) []Contact {
	vc.forEachContact(s, func(_, _, _ int, c Contact) {
		dst = append(dst, c)
	})
	return dst
}

// Raycast intersects ray directly with solid blocks in vc using 3D DDA
// (Amanatides & Woo) without building a triangle mesh.
func (vc *VoxelCollider) Raycast(ray Ray) (RaycastHit, bool) {
	if !vc.valid() || ray.MaxDist <= 0 {
		return RaycastHit{}, false
	}
	if !isFiniteVec(ray.Origin) || !isFiniteVec(ray.Dir) || !isFinite32(ray.MaxDist) {
		return RaycastHit{}, false
	}
	dx := float64(ray.Dir.X)
	dy := float64(ray.Dir.Y)
	dz := float64(ray.Dir.Z)
	dirLen := math.Sqrt(float64(float64(dx*dx)+float64(dy*dy)) + float64(dz*dz))
	if dirLen < 1e-9 {
		return RaycastHit{}, false
	}
	dx /= dirLen
	dy /= dirLen
	dz /= dirLen
	unitDir := Vec3{X: float32(dx), Y: float32(dy), Z: float32(dz)}

	vs := float64(vc.Grid.VoxelSize())
	invVS := 1.0 / vs
	tStart := float64(0)
	tEnd := float64(ray.MaxDist)

	if bg, ok := vc.Grid.(VoxelBoundsGrid); ok {
		bMin, bMax, hasBlocks := bg.VoxelBounds()
		if !hasBlocks {
			return RaycastHit{}, false
		}
		worldBounds := BoxFromMinMax(bMin.Add(vc.Origin), bMax.Add(vc.Origin))
		ts, te, hitBox := rayIntersectsBoxDist(ray.Origin, unitDir, ray.MaxDist, worldBounds)
		if !hitBox {
			return RaycastHit{}, false
		}
		// Back up by half a voxel before the bounds entry plane so the regular
		// DDA loop steps across the entry face with the exact face normal.
		if ts > 0 {
			tStart = max(0.0, float64(ts)-float64(0.5*vs))
		}
		tEnd = min(float64(ray.MaxDist), float64(te)+float64(1e-4*vs))
	}

	ox := (float64(ray.Origin.X-vc.Origin.X) + float64(dx*tStart)) * invVS
	oy := (float64(ray.Origin.Y-vc.Origin.Y) + float64(dy*tStart)) * invVS
	oz := (float64(ray.Origin.Z-vc.Origin.Z) + float64(dz*tStart)) * invVS
	maxLocalT := (tEnd - tStart) * invVS

	vx := int(math.Floor(ox))
	vy := int(math.Floor(oy))
	vz := int(math.Floor(oz))

	g := vc.Grid
	if g.IsSolid(vx, vy, vz) {
		dist := float32(tStart)
		return RaycastHit{
			Voxel:    vc,
			T:        clamp32(dist/ray.MaxDist, 0, 1),
			Distance: dist,
			Point:    ray.Origin.Add(unitDir.Scale(dist)),
			Normal:   unitDir.Neg(),
		}, true
	}

	stepX, tMaxX, tDeltaX, normX := voxelDDAAxis(ox, dx, vx, Vec3{X: -1}, Vec3{X: 1})
	stepY, tMaxY, tDeltaY, normY := voxelDDAAxis(oy, dy, vy, Vec3{Y: -1}, Vec3{Y: 1})
	stepZ, tMaxZ, tDeltaZ, normZ := voxelDDAAxis(oz, dz, vz, Vec3{Z: -1}, Vec3{Z: 1})

	for {
		var localT float64
		var normal Vec3
		if tMaxX <= tMaxY && tMaxX <= tMaxZ {
			if tMaxX > maxLocalT {
				return RaycastHit{}, false
			}
			localT = tMaxX
			vx += stepX
			tMaxX += tDeltaX
			normal = normX
		} else if tMaxY <= tMaxZ {
			if tMaxY > maxLocalT {
				return RaycastHit{}, false
			}
			localT = tMaxY
			vy += stepY
			tMaxY += tDeltaY
			normal = normY
		} else {
			if tMaxZ > maxLocalT {
				return RaycastHit{}, false
			}
			localT = tMaxZ
			vz += stepZ
			tMaxZ += tDeltaZ
			normal = normZ
		}

		if g.IsSolid(vx, vy, vz) {
			dist := float32(tStart + float64(localT*vs))
			if dist > ray.MaxDist {
				return RaycastHit{}, false
			}
			if dist < 0 {
				dist = 0
			}
			return RaycastHit{
				Voxel:    vc,
				T:        clamp32(dist/ray.MaxDist, 0, 1),
				Distance: dist,
				Point:    ray.Origin.Add(unitDir.Scale(dist)),
				Normal:   normal,
			}, true
		}
	}
}

func voxelDDAAxis(origin, dir float64, voxel int, negN, posN Vec3) (step int, tMax, tDelta float64, normal Vec3) {
	if dir > 0 {
		tDelta = 1.0 / dir
		tMax = (float64(voxel+1) - origin) * tDelta
		return 1, tMax, tDelta, negN
	}
	if dir < 0 {
		tDelta = -1.0 / dir
		tMax = (origin - float64(voxel)) * tDelta
		return -1, tMax, tDelta, posN
	}
	return 0, math.Inf(1), math.Inf(1), posN
}

// Sweep translates shape along delta against solid blocks in vc and returns
// the earliest opposing contact. Axis-aligned boxes use an exact analytical
// swept-AABB slab test with internal-face filtering; spheres, capsules, and
// oriented boxes use sub-stepped narrowphase with binary-search TOI refinement.
func (vc *VoxelCollider) Sweep(shape Shape, delta Vec3) (SweepHit, bool) {
	if !vc.valid() || !isFiniteVec(delta) {
		return SweepHit{}, false
	}
	dist := delta.Len()
	if dist <= 1e-7 {
		var best Contact
		var bx, by, bz int
		vc.forEachContact(shape, func(vx, vy, vz int, c Contact) {
			if !best.Hit || c.Depth > best.Depth {
				best = c
				bx, by, bz = vx, vy, vz
			}
		})
		if !best.Hit {
			return SweepHit{}, false
		}
		return SweepHit{
			T:      0,
			Point:  best.Point,
			Normal: best.Normal,
			Depth:  best.Depth,
			Voxel:  vc,
			BlockX: bx,
			BlockY: by,
			BlockZ: bz,
		}, true
	}

	dir := delta.Scale(1 / dist)

	// Check initial overlap at T == 0 opposing the motion.
	var initContact Contact
	var initX, initY, initZ int
	vc.forEachContact(shape, func(vx, vy, vz int, c Contact) {
		if c.Normal.Dot(dir) >= -1e-4 {
			return
		}
		if !initContact.Hit || c.Depth > initContact.Depth {
			initContact = c
			initX, initY, initZ = vx, vy, vz
		}
	})
	if initContact.Hit {
		return SweepHit{
			T:      0,
			Point:  initContact.Point,
			Normal: initContact.Normal,
			Depth:  initContact.Depth,
			Voxel:  vc,
			BlockX: initX,
			BlockY: initY,
			BlockZ: initZ,
		}, true
	}

	if shape.Kind == ShapeBox {
		if _, oriented := shape.orientedBox(); !oriented {
			return vc.sweepAABB(shape.Box, delta, dist)
		}
	}
	return vc.sweepGeneral(shape, delta, dir, dist)
}

func (vc *VoxelCollider) sweepAABB(box Box, delta Vec3, dist float32) (SweepHit, bool) {
	vs := vc.Grid.VoxelSize()
	// Subdivide long diagonal sweeps into bounded chunks so cost stays O(dist/vs)
	// rather than O((dist/vs)^3).
	chunkLen := max(vs*4, float32(1))
	chunks := max(1, int(math.Ceil(float64(dist/chunkLen))))
	g := vc.Grid

	for seg := 0; seg < chunks; seg++ {
		f0 := float32(seg) / float32(chunks)
		f1 := float32(seg+1) / float32(chunks)
		startBox := box.Translated(delta.Scale(f0))
		segDelta := delta.Scale(f1 - f0)
		endBox := startBox.Translated(segDelta)
		swept := unionBounds(startBox, endBox)
		x0, y0, z0, x1, y1, z1, ok := vc.voxelRange(swept)
		if !ok {
			continue
		}

		var best SweepHit
		found := false
		bestLocalT := float32(2)

		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				for x := x0; x <= x1; x++ {
					if !g.IsSolid(x, y, z) {
						continue
					}
					vb := vc.voxelBox(x, y, z)
					t, normal, hit := sweepBoxVsBox(startBox, segDelta, vb)
					if !hit || t < 0 || t > 1 {
						continue
					}
					if normalOccluded(vc, x, y, z, normal) {
						continue
					}
					if !found || t < bestLocalT {
						bestLocalT = t
						globalT := f0 + float32((f1-f0)*t)
						atHit := box.Translated(delta.Scale(globalT))
						aMin, aMax := atHit.Min(), atHit.Max()
						bMin, bMax := vb.Min(), vb.Max()
						pt := Vec3{
							X: (max(aMin.X, bMin.X) + min(aMax.X, bMax.X)) * 0.5,
							Y: (max(aMin.Y, bMin.Y) + min(aMax.Y, bMax.Y)) * 0.5,
							Z: (max(aMin.Z, bMin.Z) + min(aMax.Z, bMax.Z)) * 0.5,
						}
						best = SweepHit{
							T:        clamp32(globalT, 0, 1),
							Distance: globalT * dist,
							Point:    pt,
							Normal:   normal,
							Voxel:    vc,
							BlockX:   x,
							BlockY:   y,
							BlockZ:   z,
						}
						found = true
					}
				}
			}
		}
		if found {
			return best, true
		}
	}
	return SweepHit{}, false
}

func normalOccluded(vc *VoxelCollider, x, y, z int, n Vec3) bool {
	if n.X > 0.5 {
		return vc.Grid.IsSolid(x+1, y, z)
	}
	if n.X < -0.5 {
		return vc.Grid.IsSolid(x-1, y, z)
	}
	if n.Y > 0.5 {
		return vc.Grid.IsSolid(x, y+1, z)
	}
	if n.Y < -0.5 {
		return vc.Grid.IsSolid(x, y-1, z)
	}
	if n.Z > 0.5 {
		return vc.Grid.IsSolid(x, y, z+1)
	}
	if n.Z < -0.5 {
		return vc.Grid.IsSolid(x, y, z-1)
	}
	return false
}

// sweepBoxVsBox performs an analytical Minkowski-sum swept AABB test for box a
// translating by delta against static box b. Returns the entry fraction t in
// [0, 1] and outward normal of b when hit.
func sweepBoxVsBox(a Box, delta Vec3, b Box) (float32, Vec3, bool) {
	eMin := b.Min().Sub(a.HalfExtents)
	eMax := b.Max().Add(a.HalfExtents)
	c := a.Center

	const eps = 1e-6
	tEnter := float32(-math.MaxFloat32)
	tExit := float32(math.MaxFloat32)
	var hitNormal Vec3

	axis := func(orig, d, lo, hi float32, negN, posN Vec3) bool {
		if abs32(d) < 1e-8 {
			// Strict interior check prevents grazing/tangent floor faces from
			// blocking perpendicular movement.
			return orig > lo+eps && orig < hi-eps
		}
		inv := 1 / d
		t1 := (lo - orig) * inv
		t2 := (hi - orig) * inv
		n := negN
		if t1 > t2 {
			t1, t2 = t2, t1
			n = posN
		}
		if t1 > tEnter {
			tEnter = t1
			hitNormal = n
		}
		if t2 < tExit {
			tExit = t2
		}
		return tEnter < tExit-1e-7
	}

	if !axis(c.X, delta.X, eMin.X, eMax.X, Vec3{X: -1}, Vec3{X: 1}) ||
		!axis(c.Y, delta.Y, eMin.Y, eMax.Y, Vec3{Y: -1}, Vec3{Y: 1}) ||
		!axis(c.Z, delta.Z, eMin.Z, eMax.Z, Vec3{Z: -1}, Vec3{Z: 1}) {
		return 0, Vec3{}, false
	}
	if tEnter < -1e-6 || tEnter > 1 || hitNormal.LenSq() == 0 {
		return 0, Vec3{}, false
	}
	if tEnter < 0 {
		tEnter = 0
	}
	return tEnter, hitNormal, true
}

func (vc *VoxelCollider) sweepGeneral(shape Shape, delta, dir Vec3, dist float32) (SweepHit, bool) {
	vs := vc.Grid.VoxelSize()
	ext := shapeMinExtent(shape)
	stepLen := min(vs*0.25, max(ext*0.25, 0.01))
	if stepLen <= 0 {
		stepLen = 0.02
	}
	steps := max(1, int(math.Ceil(float64(dist/stepLen))))

	testOpposing := func(t float32) (Contact, int, int, int, bool) {
		moved := shape.Translated(delta.Scale(t))
		var best Contact
		var bx, by, bz int
		found := false
		vc.forEachContact(moved, func(vx, vy, vz int, c Contact) {
			if c.Normal.Dot(dir) >= -1e-4 {
				return
			}
			if !found || c.Depth > best.Depth {
				best = c
				bx, by, bz = vx, vy, vz
				found = true
			}
		})
		return best, bx, by, bz, found
	}

	tPrev := float32(0)
	for i := 1; i <= steps; i++ {
		tCur := float32(i) / float32(steps)
		if ct, bx, by, bz, hit := testOpposing(tCur); hit {
			lo, hi := tPrev, tCur
			bestC, bestX, bestY, bestZ := ct, bx, by, bz
			for b := 0; b < 14; b++ {
				mid := (lo + hi) * 0.5
				if mct, mx, my, mz, mhit := testOpposing(mid); mhit {
					hi = mid
					bestC, bestX, bestY, bestZ = mct, mx, my, mz
				} else {
					lo = mid
				}
			}
			return SweepHit{
				T:        clamp32(lo, 0, 1),
				Distance: lo * dist,
				Point:    bestC.Point,
				Normal:   bestC.Normal,
				Voxel:    vc,
				BlockX:   bestX,
				BlockY:   bestY,
				BlockZ:   bestZ,
			}, true
		}
		tPrev = tCur
	}
	return SweepHit{}, false
}

// AddVoxelCollider registers a static VoxelCollider in w and returns it.
// If vc.Grid implements VoxelEditNotifier, edits to the grid automatically
// wake sleeping bodies near changed blocks.
func (w *World) AddVoxelCollider(vc *VoxelCollider) *VoxelCollider {
	if vc == nil {
		return nil
	}
	if vc.ID <= 0 || w.colliderIDTaken(vc.ID) {
		w.nextColliderID++
		vc.ID = w.nextColliderID
	} else if vc.ID >= w.nextColliderID {
		w.nextColliderID = vc.ID
	}
	if vc.Friction < 0 {
		vc.Friction = 0
	}
	if vc.Restitution < 0 {
		vc.Restitution = 0
	}
	if vc.Restitution > 1 {
		vc.Restitution = 1
	}
	w.voxels = append(w.voxels, vc)
	vc.attachWorld(w)
	return vc
}

// RemoveVoxelCollider removes the VoxelCollider with the given ID from w and
// wakes dynamic bodies so supported objects react.
func (w *World) RemoveVoxelCollider(id int) bool {
	for i, vc := range w.voxels {
		if vc.ID == id {
			w.voxels = append(w.voxels[:i], w.voxels[i+1:]...)
			vc.detachWorld(w)
			w.wakeAllDynamic()
			return true
		}
	}
	return false
}

// VoxelColliders returns all static VoxelColliders registered in w.
func (w *World) VoxelColliders() []*VoxelCollider {
	return w.voxels
}

// WakeInBox wakes every sleeping dynamic body whose world bounds overlap
// bounds, and transitively wakes any sleeping dynamic bodies currently resting
// on them via active contacts.
func (w *World) WakeInBox(bounds Box) {
	if w == nil || len(w.bodies) == 0 {
		return
	}
	w.bp.ensureInit(w)
	var woken []int
	w.bp.queryDynamicAABB(bounds, func(p *bpProxy) bool {
		b := p.body
		if b == nil || b.Static || b.Kinematic || !b.Sleeping {
			return true
		}
		if aabbOverlap(bounds, p.worldBounds) {
			b.Wake()
			woken = append(woken, b.ID)
		}
		return true
	})
	if len(woken) == 0 || len(w.activeContacts) == 0 {
		return
	}
	// Transitively wake sleeping bodies stacked on top of woken bodies.
	for head := 0; head < len(woken); head++ {
		id := woken[head]
		for _, rec := range w.activeContacts {
			if rec.isTrigger {
				continue
			}
			if rec.a != nil && rec.a.ID == id && rec.b != nil && rec.b.Sleeping && !rec.b.Static && !rec.b.Kinematic {
				rec.b.Wake()
				woken = append(woken, rec.b.ID)
			} else if rec.b != nil && rec.b.ID == id && rec.a != nil && rec.a.Sleeping && !rec.a.Static && !rec.a.Kinematic {
				rec.a.Wake()
				woken = append(woken, rec.a.ID)
			}
		}
	}
}

// wakeForVoxels wakes sleeping dynamic bodies that were supported by a
// VoxelCollider when the supporting block disappeared or a new solid block
// appeared inside them (covering custom VoxelGrids that do not implement
// VoxelEditNotifier).
func (w *World) wakeForVoxels() {
	if len(w.voxels) == 0 {
		return
	}
	w.bp.ensureInit(w)
	up, haveUp := gravityUp(w.Gravity)
	var probeOffset Vec3
	if haveUp {
		probeOffset = up.Scale(-sleepWakeDepth)
	}
	for _, pid := range w.bp.dynamicList {
		p := &w.bp.proxies[pid]
		b := p.body
		if b.Static || b.Kinematic || !b.Sleeping || b.IsTrigger {
			continue
		}
		ws := p.worldShape
		// Wake if a newly placed solid block deeply overlaps this sleeping body.
		penetrated := false
		for _, vc := range w.voxels {
			if !canCollideMasks(b.Layer, b.Mask, vc.Layer, vc.Mask) {
				continue
			}
			if c := vc.TestShape(ws); c.Hit && c.Depth > sleepWakeDepth {
				penetrated = true
				break
			}
		}
		if penetrated {
			b.Wake()
			continue
		}
		if !b.restingOnVoxel || !haveUp {
			continue
		}
		// Verify voxel support still exists slightly along gravity.
		probed := ws.Translated(probeOffset)
		supported := false
		for _, vc := range w.voxels {
			if !canCollideMasks(b.Layer, b.Mask, vc.Layer, vc.Mask) {
				continue
			}
			vc.forEachContact(probed, func(_, _, _ int, c Contact) {
				if c.Normal.Dot(up) >= 0.25 {
					supported = true
				}
			})
			if supported {
				break
			}
		}
		if !supported {
			b.Wake()
			b.restingOnVoxel = false
		}
	}
}

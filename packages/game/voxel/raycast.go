package voxel

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// RaycastOptions configures [World.RaycastWith].
type RaycastOptions struct {
	// IncludeNonSolid causes raycasts to stop on any non-air block, including
	// blocks registered with NonSolid=true (such as water or foliage).
	IncludeNonSolid bool
}

// RaycastHit describes the first voxel struck by a raycast.
type RaycastHit struct {
	// Block is the integer coordinate of the struck voxel.
	Block BlockCoord
	// Adjacent is the empty voxel coordinate immediately outside the struck
	// face (Block.Add(Face.Step())), where a newly placed block belongs.
	Adjacent BlockCoord
	// Face is the face of Block that the ray entered.
	Face Face
	// Normal is the outward unit normal of Face.
	Normal math3d.Vec3
	// Point is the world-space intersection point on the face.
	Point math3d.Vec3
	// Distance is the Euclidean distance from ray.Origin to Point.
	Distance float32
	// BlockID is the block stored at Block.
	BlockID BlockID
}

// Raycast walks voxels along ray using 3D DDA (Amanatides & Woo) up to maxDist
// world-space units (ray.Dir need not be unit-length) and returns the first
// solid block hit. Use [World.RaycastWith] to include non-solid blocks.
func (w *World) Raycast(ray math3d.Ray, maxDist float32) (RaycastHit, bool) {
	return w.RaycastWith(ray, maxDist, RaycastOptions{})
}

// RaycastWith is [World.Raycast] with configurable filtering options.
func (w *World) RaycastWith(ray math3d.Ray, maxDist float32, opts RaycastOptions) (RaycastHit, bool) {
	if w == nil || len(w.chunks) == 0 || maxDist < 0 {
		return RaycastHit{}, false
	}
	if !isFiniteVec3(ray.Origin) || !isFiniteVec3(ray.Dir) || math.IsNaN(float64(maxDist)) || math.IsInf(float64(maxDist), 0) {
		return RaycastHit{}, false
	}

	dx := float64(ray.Dir.X)
	dy := float64(ray.Dir.Y)
	dz := float64(ray.Dir.Z)
	dirLen := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dirLen < 1e-9 {
		return RaycastHit{}, false
	}
	dx /= dirLen
	dy /= dirLen
	dz /= dirLen

	vs := float64(w.voxelSize)
	invVS := 1.0 / vs
	ox := float64(ray.Origin.X) * invVS
	oy := float64(ray.Origin.Y) * invVS
	oz := float64(ray.Origin.Z) * invVS
	maxT := float64(maxDist) * invVS

	vx := int(math.Floor(ox))
	vy := int(math.Floor(oy))
	vz := int(math.Floor(oz))

	matches := func(id BlockID) bool {
		if id == Air {
			return false
		}
		if opts.IncludeNonSolid {
			return true
		}
		return w.registry.IsSolid(id)
	}

	// Cache the active chunk so consecutive voxel steps inside the same chunk
	// avoid map lookups.
	var cachedCC ChunkCoord
	var cachedCh *Chunk
	hasCache := false
	sample := func(x, y, z int) BlockID {
		cc := ChunkCoord{X: x >> w.shift, Y: y >> w.shift, Z: z >> w.shift}
		if !hasCache || cc != cachedCC {
			cachedCC = cc
			cachedCh = w.chunks[cc]
			hasCache = true
		}
		if cachedCh == nil {
			return Air
		}
		return cachedCh.getUnchecked(x&w.mask, y&w.mask, z&w.mask)
	}

	// Starting inside a matching block hits immediately at distance 0.
	if id := sample(vx, vy, vz); matches(id) {
		face := dominantOppositeFace(dx, dy, dz)
		bc := BlockCoord{X: vx, Y: vy, Z: vz}
		return RaycastHit{
			Block:    bc,
			Adjacent: bc.Add(face.Step()),
			Face:     face,
			Normal:   face.Normal(),
			Point:    ray.Origin,
			Distance: 0,
			BlockID:  id,
		}, true
	}

	stepX, tMaxX, tDeltaX, faceX := ddaAxis(ox, dx, vx, FaceNegX, FacePosX)
	stepY, tMaxY, tDeltaY, faceY := ddaAxis(oy, dy, vy, FaceNegY, FacePosY)
	stepZ, tMaxZ, tDeltaZ, faceZ := ddaAxis(oz, dz, vz, FaceNegZ, FacePosZ)

	unitDir := math3d.Vec3{X: float32(dx), Y: float32(dy), Z: float32(dz)}

	for {
		var t float64
		var face Face
		if tMaxX <= tMaxY && tMaxX <= tMaxZ {
			if tMaxX > maxT {
				return RaycastHit{}, false
			}
			t = tMaxX
			vx += stepX
			tMaxX += tDeltaX
			face = faceX
		} else if tMaxY <= tMaxZ {
			if tMaxY > maxT {
				return RaycastHit{}, false
			}
			t = tMaxY
			vy += stepY
			tMaxY += tDeltaY
			face = faceY
		} else {
			if tMaxZ > maxT {
				return RaycastHit{}, false
			}
			t = tMaxZ
			vz += stepZ
			tMaxZ += tDeltaZ
			face = faceZ
		}

		if id := sample(vx, vy, vz); matches(id) {
			dist := float32(t * vs)
			bc := BlockCoord{X: vx, Y: vy, Z: vz}
			return RaycastHit{
				Block:    bc,
				Adjacent: bc.Add(face.Step()),
				Face:     face,
				Normal:   face.Normal(),
				Point:    ray.Origin.Add(unitDir.Scale(dist)),
				Distance: dist,
				BlockID:  id,
			}, true
		}
	}
}

func ddaAxis(origin, dir float64, voxel int, negFace, posFace Face) (step int, tMax, tDelta float64, face Face) {
	if dir > 0 {
		tDelta = 1.0 / dir
		tMax = (float64(voxel+1) - origin) * tDelta
		return 1, tMax, tDelta, negFace
	}
	if dir < 0 {
		tDelta = -1.0 / dir
		tMax = (origin - float64(voxel)) * tDelta
		return -1, tMax, tDelta, posFace
	}
	return 0, math.Inf(1), math.Inf(1), posFace
}

func dominantOppositeFace(dx, dy, dz float64) Face {
	ax, ay, az := math.Abs(dx), math.Abs(dy), math.Abs(dz)
	if ax >= ay && ax >= az {
		if dx > 0 {
			return FaceNegX
		}
		return FacePosX
	}
	if ay >= az {
		if dy > 0 {
			return FaceNegY
		}
		return FacePosY
	}
	if dz > 0 {
		return FaceNegZ
	}
	return FacePosZ
}

func isFiniteVec3(v math3d.Vec3) bool {
	return !math.IsNaN(float64(v.X)) && !math.IsInf(float64(v.X), 0) &&
		!math.IsNaN(float64(v.Y)) && !math.IsInf(float64(v.Y), 0) &&
		!math.IsNaN(float64(v.Z)) && !math.IsInf(float64(v.Z), 0)
}

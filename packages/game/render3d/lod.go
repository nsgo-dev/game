package render3d

import "github.com/nsgo-dev/game/packages/game/math3d"

// LODLevel describes one mesh detail level for a [DrawItem], ordered from
// finest (index 0) to coarsest (last index).
//
// A level is eligible when its active thresholds are satisfied:
//   - If MaxDistance > 0, camera distance must be <= MaxDistance.
//   - If MinScreenRatio > 0, projected screen-height ratio must be >= MinScreenRatio.
//   - Leaving both zero marks an unbounded coarsest fallback level.
type LODLevel struct {
	MeshID         uint32
	MaxDistance    float32
	MinScreenRatio float32
	Triangles      uint32
	Vertices       uint32
}

// SelectLOD chooses the index of the appropriate [LODLevel] from levels
// (ordered finest to coarsest) given camera distance dist and projected
// screen-height ratio screenRatio.
//
// Threshold checks are skipped for any negative metric (dist < 0 disables
// MaxDistance checks; screenRatio < 0 disables MinScreenRatio checks) or when
// a level leaves that threshold <= 0.
//
// When 0 <= prevIndex < len(levels) and hysteresis > 0 (clamped to [0, 0.5]),
// transitions away from prevIndex require crossing the boundary by a relative
// deadband of hysteresis:
//   - Degrading to a coarser LOD requires exceeding MaxDistance*(1+h) or
//     falling below MinScreenRatio*(1-h).
//   - Upgrading to a finer LOD requires entering MaxDistance*(1-h) and
//     MinScreenRatio*(1+h).
//
// SelectLOD returns -1 when levels is empty.
func SelectLOD(levels []LODLevel, dist float32, screenRatio float32, hysteresis float32, prevIndex int) int {
	n := len(levels)
	if n == 0 {
		return -1
	}
	if n == 1 {
		return 0
	}

	raw := selectLODRaw(levels, dist, screenRatio)
	h := math3d.Clamp(hysteresis, 0, 0.5)
	if h == 0 || prevIndex < 0 || prevIndex >= n || raw == prevIndex {
		return raw
	}

	if raw > prevIndex {
		// Moving toward a coarser LOD: keep prevIndex while it still passes
		// with the relaxed outward deadband.
		if lodMatches(levels[prevIndex], dist, screenRatio, 1+h, 1-h) {
			return prevIndex
		}
		// Otherwise select the finest level in (prevIndex, raw] that satisfies
		// the relaxed threshold (falling back to raw).
		for i := prevIndex + 1; i < raw; i++ {
			if lodMatches(levels[i], dist, screenRatio, 1+h, 1-h) {
				return i
			}
		}
		return raw
	}

	// Moving toward a finer LOD (raw < prevIndex): only upgrade to a finer
	// level i if we have clearly crossed inside its tightened inward deadband.
	for i := raw; i < prevIndex; i++ {
		if lodMatches(levels[i], dist, screenRatio, 1-h, 1+h) {
			return i
		}
	}
	return prevIndex
}

func selectLODRaw(levels []LODLevel, dist, screenRatio float32) int {
	for i, lv := range levels {
		if lodMatches(lv, dist, screenRatio, 1, 1) {
			return i
		}
	}
	return len(levels) - 1
}

func lodMatches(lv LODLevel, dist, screenRatio, distScale, ratioScale float32) bool {
	if lv.MaxDistance > 0 && dist >= 0 {
		if dist > lv.MaxDistance*distScale {
			return false
		}
	}
	if lv.MinScreenRatio > 0 && screenRatio >= 0 {
		if screenRatio < lv.MinScreenRatio*ratioScale {
			return false
		}
	}
	return true
}

// ProjectScreenRatio estimates the fraction of viewport height [0, 1] covered
// by the bounding sphere of bounds under viewProj. It returns 0 if bounds is
// empty or lies behind the camera.
func ProjectScreenRatio(bounds math3d.AABB, viewProj math3d.Mat4) float32 {
	if bounds.Empty() {
		return 0
	}
	radius := bounds.HalfExtents().Len()
	if radius <= 0 {
		return 0
	}
	center := bounds.Center()
	clip := viewProj.MulVec4(center.Vec4(1))
	if clip.W <= 1e-6 {
		// The center is at or behind the camera while the bounds still reach
		// the view (a floor or a giant body). Take the finest LOD rather than
		// the coarsest; the frustum culls what is wholly behind (#546).
		return 1
	}
	// Row 1 of P*V has XYZ length equal to the vertical projection scale
	// (f = cot(fovY/2) for perspective, 2/(top-bottom) for orthographic).
	// A sphere of radius r projects to NDC height 2*(scaleY*r)/w, which is
	// (scaleY*r)/w of the [-1, 1] viewport height.
	scaleY := viewProj.Row(1).XYZ().Len()
	ratio := (scaleY * radius) / clip.W
	return math3d.Clamp(ratio, 0, 1)
}

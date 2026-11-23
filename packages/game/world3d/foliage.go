package world3d

import (
	"image"
	"image/color"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/quality"
)

// QualityLevel selects a built-in foliage density and draw-distance tier.
type QualityLevel uint8

const (
	QualityLow QualityLevel = iota
	QualityMedium
	QualityHigh
	QualityUltra
)

// QualityProfile controls runtime foliage density scaling, maximum visibility
// distance, and smooth distance fade-out.
type QualityProfile struct {
	// DensityScale scales the fraction of scattered instances emitted, in [0, 1].
	DensityScale float32
	// MaxDistance caps the maximum visible distance in world units (meters).
	// If <= 0, the layer's BaseMaxDistance is used.
	MaxDistance float32
	// FadeStartRatio is the fraction of effective MaxDistance in [0, 1] at which
	// instances begin fading smoothly from 1.0 down to 0.0 at MaxDistance.
	FadeStartRatio float32
}

// DefaultQualityProfile returns recommended foliage settings for level.
func DefaultQualityProfile(level QualityLevel) QualityProfile {
	switch level {
	case QualityLow:
		return QualityProfile{DensityScale: 0.25, MaxDistance: 40, FadeStartRatio: 0.60}
	case QualityMedium:
		return QualityProfile{DensityScale: 0.50, MaxDistance: 75, FadeStartRatio: 0.70}
	case QualityHigh:
		return QualityProfile{DensityScale: 0.75, MaxDistance: 120, FadeStartRatio: 0.75}
	default:
		return QualityProfile{DensityScale: 1.00, MaxDistance: 200, FadeStartRatio: 0.80}
	}
}

// QualityLevelFor returns the foliage tier for a game-wide quality level: QualityLow for quality.Low, QualityMedium for quality.Medium and QualityHigh for quality.High. QualityUltra stays an explicit choice above every preset. An invalid level maps to QualityMedium, as quality.DefaultPreset does.
func QualityLevelFor(l quality.Level) QualityLevel {
	switch l {
	case quality.Low:
		return QualityLow
	case quality.High:
		return QualityHigh
	default:
		return QualityMedium
	}
}

// DensityMap stores normalized [0, 1] 2D scatter density weights in row-major order.
type DensityMap struct {
	Width  int
	Height int
	Data   []float32
}

// LoadDensityMapGray constructs a DensityMap from a grayscale (or color,
// converted to 16-bit luminance) image with pixel intensities normalized to [0, 1].
func LoadDensityMapGray(img image.Image) *DensityMap {
	if img == nil {
		return &DensityMap{}
	}
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	data := make([]float32, w*h)
	for y := range h {
		for x := range w {
			g := color.Gray16Model.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray16)
			data[y*w+x] = float32(g.Y) / 65535.0
		}
	}
	return &DensityMap{
		Width:  w,
		Height: h,
		Data:   data,
	}
}

// Sample returns the bilinearly interpolated density weight in [0, 1] at
// normalized UV coordinates (u, v) in [0, 1]. If d is nil or empty, 1.0 is returned.
func (d *DensityMap) Sample(u, v float32) float32 {
	if d == nil || d.Width <= 0 || d.Height <= 0 || len(d.Data) < d.Width*d.Height {
		return 1
	}
	u = math3d.Clamp(u, 0, 1)
	v = math3d.Clamp(v, 0, 1)

	fx := u * float32(d.Width-1)
	fy := v * float32(d.Height-1)
	x0 := int(fx)
	y0 := int(fy)
	x1 := min(x0+1, d.Width-1)
	y1 := min(y0+1, d.Height-1)
	tx := fx - float32(x0)
	ty := fy - float32(y0)

	v00 := d.Data[y0*d.Width+x0]
	v10 := d.Data[y0*d.Width+x1]
	v01 := d.Data[y1*d.Width+x0]
	v11 := d.Data[y1*d.Width+x1]

	top := math3d.Lerp(v00, v10, tx)
	bot := math3d.Lerp(v01, v11, tx)
	return math3d.Clamp(math3d.Lerp(top, bot, ty), 0, 1)
}

// FoliageLayer configures deterministic procedural scattering of a foliage or
// prop mesh across a Heightmap.
type FoliageLayer struct {
	Name            string
	MeshID          uint32
	MaterialID      uint32
	Density         float32     // target candidate instances per square meter
	MinScale        float32     // minimum uniform instance scale (defaults to 1 if both <= 0)
	MaxScale        float32     // maximum uniform instance scale
	AlignToNormal   float32     // 0 = upright (+Y), 1 = fully aligned to terrain normal
	MinSlopeDeg     float32     // minimum terrain slope in degrees [0, 90]
	MaxSlopeDeg     float32     // maximum terrain slope in degrees [0, 90]; 0 with MinSlopeDeg==0 allows all slopes
	MinHeight       float32     // minimum world Y height (unconstrained if MinHeight == 0 && MaxHeight == 0)
	MaxHeight       float32     // maximum world Y height (unconstrained if MinHeight == 0 && MaxHeight == 0)
	DensityMap      *DensityMap // optional normalized density mask across terrain bounds
	BaseMaxDistance float32     // default maximum draw distance in world units
	Seed            uint64      // deterministic PRNG seed
	// BoundingRadius is the radius, at scale 1, of a sphere around the mesh pivot that encloses the mesh. When > 0, frustum culling uses each instance's scaled sphere and the draw distance and fade are measured from the sphere's nearest point, so a large tree is kept while any part of it is within range (#546). When <= 0, culling assumes a unit-radius mesh and distances are measured from the pivot.
	BoundingRadius float32
}

// FoliageInstance is a visible scattered instance ready for instanced rendering.
type FoliageInstance struct {
	Transform math3d.Mat4
	Fade      float32 // 1.0 inside fade start distance, smoothly transitioning to 0.0 at MaxDistance
}

type foliageChunk struct {
	bounds math3d.AABB
	start  int
	count  int
}

// FoliageField holds deterministically scattered, spatially chunked foliage
// instances for fast frustum and distance culling.
type FoliageField struct {
	Layer      FoliageLayer
	chunks     []foliageChunk
	transforms []math3d.Mat4
	radii      []float32 // per-instance world bounding radius, parallel to transforms
	sphereDist bool      // measure draw distance from the bounding sphere (Layer.BoundingRadius > 0)
	bounds     math3d.AABB
}

const defaultFoliageChunkSize float32 = 16.0

// Scatter deterministically generates a chunked FoliageField across region on hm.
// If region is empty or zero, the full Heightmap bounds are used.
//
// Instances within each spatial chunk are stored in deterministic pseudo-random
// order so runtime QualityProfile.DensityScale sub-sampling is spatially uniform,
// strictly nested across quality levels, and branch-free.
func Scatter(hm *Heightmap, layer FoliageLayer, region math3d.AABB) *FoliageField {
	field := &FoliageField{
		Layer:  layer,
		bounds: math3d.AABBEmpty(),
	}
	if hm == nil || !hm.valid() || layer.Density <= 0 {
		return field
	}

	hmBounds := hm.Bounds()
	minX, maxX := hmBounds.Min.X, hmBounds.Max.X
	minZ, maxZ := hmBounds.Min.Z, hmBounds.Max.Z
	if !region.Empty() && region != (math3d.AABB{}) {
		minX = max(minX, region.Min.X)
		maxX = min(maxX, region.Max.X)
		minZ = max(minZ, region.Min.Z)
		maxZ = min(maxZ, region.Max.Z)
	}
	if minX >= maxX || minZ >= maxZ {
		return field
	}

	minScale, maxScale := layer.MinScale, layer.MaxScale
	if minScale <= 0 && maxScale <= 0 {
		minScale, maxScale = 1, 1
	} else {
		if minScale <= 0 {
			minScale = maxScale
		}
		if maxScale < minScale {
			maxScale = minScale
		}
	}
	align := math3d.Clamp(layer.AlignToNormal, 0, 1)

	// Slope filter: normal.Y == cos(slopeAngle).
	hasSlopeFilter := layer.MinSlopeDeg > 0 || layer.MaxSlopeDeg > 0
	var minNormalY, maxNormalY float32 = -1, 1
	if hasSlopeFilter {
		loDeg := math3d.Clamp(layer.MinSlopeDeg, 0, 90)
		hiDeg := layer.MaxSlopeDeg
		if hiDeg <= 0 {
			hiDeg = 90
		}
		hiDeg = math3d.Clamp(hiDeg, 0, 90)
		if loDeg > hiDeg {
			return field
		}
		minNormalY = float32(math.Cos(float64(math3d.DegToRad(hiDeg)))) - 1e-5
		maxNormalY = float32(math.Cos(float64(math3d.DegToRad(loDeg)))) + 1e-5
	}

	hasHeightFilter := layer.MinHeight != 0 || layer.MaxHeight != 0
	if hasHeightFilter && layer.MinHeight > layer.MaxHeight {
		return field
	}

	hmSpanX := hmBounds.Max.X - hmBounds.Min.X
	hmSpanZ := hmBounds.Max.Z - hmBounds.Min.Z
	invHMSpanX := float32(0)
	if hmSpanX > 0 {
		invHMSpanX = 1 / hmSpanX
	}
	invHMSpanZ := float32(0)
	if hmSpanZ > 0 {
		invHMSpanZ = 1 / hmSpanZ
	}

	chunkSize := defaultFoliageChunkSize
	spanX := maxX - minX
	spanZ := maxZ - minZ
	nx := max(1, int(math.Ceil(float64(spanX/chunkSize))))
	nz := max(1, int(math.Ceil(float64(spanZ/chunkSize))))

	estTotal := int(float64(spanX)*float64(spanZ)*float64(layer.Density)) + 16
	if estTotal > 2_000_000 {
		estTotal = 2_000_000
	}
	field.transforms = make([]math3d.Mat4, 0, estTotal)
	field.radii = make([]float32, 0, estTotal)
	meshRadius := layer.BoundingRadius
	field.sphereDist = meshRadius > 0
	if meshRadius <= 0 {
		meshRadius = 1
	}
	field.chunks = make([]foliageChunk, 0, nx*nz)

	up := math3d.Vec3Up()
	for cz := range nz {
		z0 := minZ + float32(cz)*chunkSize
		z1 := min(z0+chunkSize, maxZ)
		dz := z1 - z0
		if dz <= 0 {
			continue
		}
		for cx := range nx {
			x0 := minX + float32(cx)*chunkSize
			x1 := min(x0+chunkSize, maxX)
			dx := x1 - x0
			if dx <= 0 {
				continue
			}

			rng := newFoliageRNG(layer.Seed, cx, cz)
			expected := float64(dx) * float64(dz) * float64(layer.Density)
			candidates := int(math.Floor(expected))
			if float64(rng.float32()) < expected-float64(candidates) {
				candidates++
			}
			if candidates <= 0 {
				continue
			}

			start := len(field.transforms)
			cBounds := math3d.AABBEmpty()

			for range candidates {
				x := x0 + rng.float32()*dx
				z := z0 + rng.float32()*dz
				densitySample := rng.float32()
				yaw := rng.float32() * (2 * math.Pi)
				scale := math3d.Lerp(minScale, maxScale, rng.float32())

				if layer.DensityMap != nil {
					u := (x - hmBounds.Min.X) * invHMSpanX
					v := (z - hmBounds.Min.Z) * invHMSpanZ
					w := layer.DensityMap.Sample(u, v)
					if w <= 0 || densitySample >= w {
						continue
					}
				}

				y, ok := hm.HeightAt(x, z)
				if !ok {
					continue
				}
				if hasHeightFilter && (y < layer.MinHeight || y > layer.MaxHeight) {
					continue
				}

				normal := hm.NormalAt(x, z)
				if hasSlopeFilter && (normal.Y < minNormalY || normal.Y > maxNormalY) {
					continue
				}

				rot := math3d.QuatFromAxisAngle(up, yaw)
				if align > 0 {
					tilt := shortestArcQuat(up, normal)
					if align < 1 {
						tilt = math3d.QuatIdentity().Slerp(tilt, align)
					}
					rot = tilt.Mul(rot).Norm()
				}

				mat := math3d.TRS(math3d.Vec3{X: x, Y: y, Z: z}, rot, math3d.Vec3{X: scale, Y: scale, Z: scale})
				field.transforms = append(field.transforms, mat)
				r := scale * meshRadius
				field.radii = append(field.radii, r)

				p := math3d.Vec3{X: x, Y: y, Z: z}
				pad := math3d.Vec3{X: r, Y: r, Z: r}
				cBounds = cBounds.ExpandPoint(p.Sub(pad)).ExpandPoint(p.Add(pad))
			}

			count := len(field.transforms) - start
			if count > 0 {
				field.chunks = append(field.chunks, foliageChunk{
					bounds: cBounds,
					start:  start,
					count:  count,
				})
				field.bounds = field.bounds.Union(cBounds)
			}
		}
	}

	return field
}

// TotalCount returns the total number of scattered instances stored in f.
func (f *FoliageField) TotalCount() int {
	if f == nil {
		return 0
	}
	return len(f.transforms)
}

// ChunkCount returns the number of non-empty spatial chunks in f.
func (f *FoliageField) ChunkCount() int {
	if f == nil {
		return 0
	}
	return len(f.chunks)
}

// Bounds returns the world-space AABB enclosing all scattered instances in f.
func (f *FoliageField) Bounds() math3d.AABB {
	if f == nil {
		return math3d.AABBEmpty()
	}
	return f.bounds
}

// CollectVisible culls f against frustum and effective draw distance from
// camPos, applies quality.DensityScale, computes smooth per-instance Fade
// factors (1.0 inside fade start -> 0.0 at MaxDistance), and appends visible
// instances into out[:0].
//
// Passing back the slice returned on the previous frame avoids heap allocations
// in steady state.
func (f *FoliageField) CollectVisible(
	camPos math3d.Vec3,
	frustum math3d.Frustum,
	quality QualityProfile,
	out []FoliageInstance,
) []FoliageInstance {
	out = out[:0]
	if f == nil || len(f.chunks) == 0 {
		return out
	}

	densityScale := math3d.Clamp(quality.DensityScale, 0, 1)
	if densityScale <= 0 {
		return out
	}

	maxDist := effectiveMaxDistance(quality.MaxDistance, f.Layer.BaseMaxDistance)
	hasMaxDist := maxDist > 0
	maxDistSq := maxDist * maxDist

	fadeRatio := math3d.Clamp(quality.FadeStartRatio, 0, 1)
	fadeStart := maxDist * fadeRatio
	fadeStartSq := fadeStart * fadeStart
	fadeSpan := maxDist - fadeStart
	invFadeSpan := float32(0)
	if fadeSpan > 1e-6 {
		invFadeSpan = 1 / fadeSpan
	}

	planes := frustum.Planes
	transforms := f.transforms

	for i := range f.chunks {
		ch := &f.chunks[i]

		var fullyInsideFade bool
		if hasMaxDist {
			minD2, maxD2 := aabbMinMaxDistSq(ch.bounds, camPos)
			if minD2 > maxDistSq {
				continue
			}
			fullyInsideFade = maxD2 <= fadeStartSq
		} else {
			fullyInsideFade = true
		}

		rel := classifyAABBFrustum(planes, ch.bounds)
		if rel == frustumOutside {
			continue
		}

		limit := ch.count
		if densityScale < 1 {
			limit = int(float32(ch.count)*densityScale + 0.5)
			if limit <= 0 {
				continue
			}
			if limit > ch.count {
				limit = ch.count
			}
		}

		chunkSlice := transforms[ch.start : ch.start+limit]
		chunkRadii := f.radii[ch.start : ch.start+limit]

		// Fast path: entire chunk is inside frustum and inside fade start distance.
		if rel == frustumInside && fullyInsideFade {
			for j := range chunkSlice {
				out = append(out, FoliageInstance{
					Transform: chunkSlice[j],
					Fade:      1,
				})
			}
			continue
		}

		// Distance and fade use the nearest point of each instance's bounding sphere when the layer has a BoundingRadius, otherwise its pivot.
		for j := range chunkSlice {
			m := &chunkSlice[j]
			r := chunkRadii[j]
			px, py, pz := m[12], m[13], m[14]

			fade := float32(1)
			if hasMaxDist {
				dx := px - camPos.X
				dy := py - camPos.Y
				dz := pz - camPos.Z
				reach := float32(0)
				if f.sphereDist {
					reach = r
				}
				var ok bool
				fade, ok = instanceFade(dx*dx+dy*dy+dz*dz, reach, maxDist, fadeStart, invFadeSpan)
				if !ok {
					continue
				}
			}

			if rel != frustumInside && !sphereInFrustumPlanes(planes, px, py, pz, -r) {
				continue
			}
			out = append(out, FoliageInstance{
				Transform: *m,
				Fade:      fade,
			})
		}
	}

	return out
}

// instanceFade returns the fade of an instance whose pivot is sqrt(d2) from the camera and whose bounding sphere has radius r, measuring from the sphere's nearest point, and whether it is within maxDist at all.
func instanceFade(d2, r, maxDist, fadeStart, invFadeSpan float32) (float32, bool) {
	reach := maxDist + r
	if d2 >= reach*reach {
		return 0, false
	}
	start := fadeStart + r
	if d2 <= start*start {
		return 1, true
	}
	if invFadeSpan == 0 {
		return 0, false
	}
	t := (float32(math.Sqrt(float64(d2))) - r - fadeStart) * invFadeSpan
	t = math3d.Clamp(t, 0, 1)
	return 1 - t*t*(3-2*t), true
}

func effectiveMaxDistance(qualityMax, baseMax float32) float32 {
	switch {
	case qualityMax > 0 && baseMax > 0:
		return min(qualityMax, baseMax)
	case qualityMax > 0:
		return qualityMax
	case baseMax > 0:
		return baseMax
	default:
		return 0
	}
}

type frustumRelation uint8

const (
	frustumOutside frustumRelation = iota
	frustumIntersect
	frustumInside
)

func classifyAABBFrustum(planes [6]math3d.Plane, b math3d.AABB) frustumRelation {
	intersect := false
	for i := range planes {
		pl := planes[i]
		nx, ny, nz, d := pl.Normal.X, pl.Normal.Y, pl.Normal.Z, pl.D

		var px, py, pz float32
		var qx, qy, qz float32
		if nx >= 0 {
			px, qx = b.Max.X, b.Min.X
		} else {
			px, qx = b.Min.X, b.Max.X
		}
		if ny >= 0 {
			py, qy = b.Max.Y, b.Min.Y
		} else {
			py, qy = b.Min.Y, b.Max.Y
		}
		if nz >= 0 {
			pz, qz = b.Max.Z, b.Min.Z
		} else {
			pz, qz = b.Min.Z, b.Max.Z
		}

		if nx*px+ny*py+nz*pz+d < 0 {
			return frustumOutside
		}
		if nx*qx+ny*qy+nz*qz+d < 0 {
			intersect = true
		}
	}
	if intersect {
		return frustumIntersect
	}
	return frustumInside
}

func sphereInFrustumPlanes(planes [6]math3d.Plane, x, y, z, negR float32) bool {
	for i := range planes {
		pl := planes[i]
		if pl.Normal.X*x+pl.Normal.Y*y+pl.Normal.Z*z+pl.D < negR {
			return false
		}
	}
	return true
}

func aabbMinMaxDistSq(b math3d.AABB, p math3d.Vec3) (minD2, maxD2 float32) {
	var dxMin, dyMin, dzMin float32
	if p.X < b.Min.X {
		dxMin = b.Min.X - p.X
	} else if p.X > b.Max.X {
		dxMin = p.X - b.Max.X
	}
	if p.Y < b.Min.Y {
		dyMin = b.Min.Y - p.Y
	} else if p.Y > b.Max.Y {
		dyMin = p.Y - b.Max.Y
	}
	if p.Z < b.Min.Z {
		dzMin = b.Min.Z - p.Z
	} else if p.Z > b.Max.Z {
		dzMin = p.Z - b.Max.Z
	}
	minD2 = dxMin*dxMin + dyMin*dyMin + dzMin*dzMin

	dxMax := max(float32(math.Abs(float64(p.X-b.Min.X))), float32(math.Abs(float64(p.X-b.Max.X))))
	dyMax := max(float32(math.Abs(float64(p.Y-b.Min.Y))), float32(math.Abs(float64(p.Y-b.Max.Y))))
	dzMax := max(float32(math.Abs(float64(p.Z-b.Min.Z))), float32(math.Abs(float64(p.Z-b.Max.Z))))
	maxD2 = dxMax*dxMax + dyMax*dyMax + dzMax*dzMax
	return minD2, maxD2
}

// shortestArcQuat returns a unit quaternion rotating unit vector from onto to.
func shortestArcQuat(from, to math3d.Vec3) math3d.Quat {
	dot := from.Dot(to)
	if dot <= -1+1e-6 {
		axis := math3d.Vec3UnitX().Cross(from)
		if axis.LenSq() < 1e-6 {
			axis = math3d.Vec3UnitZ().Cross(from)
		}
		return math3d.QuatFromAxisAngle(axis.Norm(), math.Pi)
	}
	c := from.Cross(to)
	if c.LenSq() < 1e-12 {
		return math3d.QuatIdentity()
	}
	return math3d.Quat{X: c.X, Y: c.Y, Z: c.Z, W: 1 + dot}.Norm()
}

// foliageRNG is a fast, deterministic SplitMix64 PRNG for repeatable scattering.
type foliageRNG struct {
	state uint64
}

func newFoliageRNG(seed uint64, cx, cz int) foliageRNG {
	s := seed ^ (uint64(uint32(cx)) * 0x9e3779b97f4a7c15) ^ (uint64(uint32(cz)) * 0xbf58476d1ce4e5b9)
	if s == 0 {
		s = 0x2545f4914f6cdd1d
	}
	return foliageRNG{state: s}
}

func (r *foliageRNG) nextUint64() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// float32 returns a uniform value in [0, 1).
func (r *foliageRNG) float32() float32 {
	return float32(r.nextUint64()>>40) * (1.0 / (1 << 24))
}

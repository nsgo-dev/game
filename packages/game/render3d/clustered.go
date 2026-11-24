package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// Defaults and buffer layout constants for clustered forward lighting.
const (
	// DefaultClusterTilesX is the default number of horizontal screen-space tiles.
	DefaultClusterTilesX = 16
	// DefaultClusterTilesY is the default number of vertical screen-space tiles.
	DefaultClusterTilesY = 9
	// DefaultClusterSlicesZ is the default number of logarithmic view-space depth slices.
	DefaultClusterSlicesZ = 24
	// DefaultClusterMaxLights is the default storage-buffer light capacity in clustered mode.
	DefaultClusterMaxLights = 512
	// DefaultMaxLightsPerCluster is the default maximum number of lights recorded per cluster cell.
	DefaultMaxLightsPerCluster = 64
	// DefaultFallbackMaxLights is the default uniform-buffer light cap for mobile / WebGL 2 / GLES 3.0 fallback.
	DefaultFallbackMaxLights = 16

	// MaxClusterSlicesZ is the maximum depth slice count supported by the uniform split-depth table (65 slice boundaries packed into 17 vec4<f32> entries).
	MaxClusterSlicesZ = 64
	// MaxFallbackLights is the uniform-buffer array capacity of [ClusteredFallbackWGSL].
	MaxFallbackLights = 16

	// ClusteredLightGroup is the bind group index declared by [ClusteredBindingsWGSL], [ClusteredFallbackBindingsWGSL], [ClusteredLitWGSL], and [ClusteredFallbackWGSL] (group 1, alongside [CameraGroup] at group 0).
	ClusteredLightGroup = 1

	// ClusterLightGPUSize is the byte size of one GPU light record (4 vec4<f32>, 16-byte aligned in both std140 and std430).
	ClusterLightGPUSize = 64
	// ClusterParamsUniformSize is the byte size of the clustered-path ClusterParams uniform block: grid_info (16) + viewport_depth (16) + ambient (16) + 17*vec4 slice_depths (272) = 320 bytes.
	ClusterParamsUniformSize = 320
	// ClusterCellGPUSize is the byte size of one cluster cell offset/count entry (vec4<u32>, 16-byte aligned across all backends).
	ClusterCellGPUSize = 16
	// ClusteredFallbackUniformSize is the byte size of the fallback uniform block: info (16) + ambient (16) + 16*ClusterLightGPUSize (1024) = 1056 bytes.
	ClusteredFallbackUniformSize = 32 + MaxFallbackLights*ClusterLightGPUSize

	clusterMinStorageBindingBytes = 64 * 1024
)

// Sentinel errors returned for invalid clustered lighting configuration or light data.
var (
	// ErrInvalidClusterConfig is wrapped by every error [ClusterConfig.Validate], [NewClusterGrid], and [NewClusteredLightSystem] return for malformed cluster settings.
	ErrInvalidClusterConfig = errors.New("render3d: invalid cluster config")
	// ErrInvalidClusterLight is wrapped by every error [ClusterLight.Validate] returns for malformed light data.
	ErrInvalidClusterLight = errors.New("render3d: invalid cluster light")
)

// ClusterLightKind selects the emission geometry of a [ClusterLight].
type ClusterLightKind uint32

const (
	// ClusterLightPoint radiates isotropically within Range.
	ClusterLightPoint ClusterLightKind = 0
	// ClusterLightSpot radiates within a cone around Direction with smooth inner-to-outer angular attenuation.
	ClusterLightSpot ClusterLightKind = 1
)

// ClusterLight describes a local punctual light (point or spot) in world space.
//
// Range attenuation follows glTF KHR_lights_punctual: clamp(1 - (d/Range)^4, 0, 1)^2 / max(d^2, 1e-4).
// Spot lights additionally multiply by clamp(dot(dir, lightToPoint)*spotScale + spotOffset, 0, 1)^2.
type ClusterLight struct {
	Kind      ClusterLightKind
	Position  math3d.Vec3
	Range     float32
	Color     math3d.Vec3
	Intensity float32
	// Direction is the world-space emission axis for [ClusterLightSpot]. Normalized automatically; zero defaults to (0, -1, 0).
	Direction math3d.Vec3
	// InnerConeCos and OuterConeCos are cos(innerHalfAngle) and cos(outerHalfAngle) for [ClusterLightSpot] with 1 >= InnerConeCos >= OuterConeCos >= -1.
	InnerConeCos float32
	OuterConeCos float32
}

// NewPointClusterLight returns a [ClusterLightPoint] with finite range and linear RGB color.
func NewPointClusterLight(pos, color math3d.Vec3, intensity, lightRange float32) ClusterLight {
	return ClusterLight{
		Kind:      ClusterLightPoint,
		Position:  pos,
		Range:     lightRange,
		Color:     color,
		Intensity: intensity,
	}
}

// NewSpotClusterLight returns a [ClusterLightSpot] aiming along dir with inner and outer cone half-angles in radians (clamped so 0 <= inner <= outer <= pi).
func NewSpotClusterLight(pos, dir, color math3d.Vec3, intensity, lightRange, innerConeRad, outerConeRad float32) ClusterLight {
	if dir.LenSq() < 1e-12 {
		dir = math3d.Vec3{Y: -1}
	} else {
		dir = dir.Norm()
	}
	if innerConeRad < 0 {
		innerConeRad = 0
	}
	if outerConeRad < innerConeRad {
		outerConeRad = innerConeRad
	}
	if outerConeRad > math.Pi {
		outerConeRad = math.Pi
	}
	return ClusterLight{
		Kind:         ClusterLightSpot,
		Position:     pos,
		Range:        lightRange,
		Color:        color,
		Intensity:    intensity,
		Direction:    dir,
		InnerConeCos: float32(math.Cos(float64(innerConeRad))),
		OuterConeCos: float32(math.Cos(float64(outerConeRad))),
	}
}

// Validate checks that l has finite parameters, a known kind, positive Range, non-negative Color and Intensity, and valid cone cosines for spot lights. Every error wraps [ErrInvalidClusterLight].
func (l ClusterLight) Validate() error {
	if l.Kind != ClusterLightPoint && l.Kind != ClusterLightSpot {
		return fmt.Errorf("%w: unknown light kind %d", ErrInvalidClusterLight, l.Kind)
	}
	if !finite3(l.Position) {
		return fmt.Errorf("%w: position is not finite", ErrInvalidClusterLight)
	}
	if !finite(l.Range) || l.Range <= 0 {
		return fmt.Errorf("%w: range %v must be finite and positive", ErrInvalidClusterLight, l.Range)
	}
	if !finite3(l.Color) || l.Color.X < 0 || l.Color.Y < 0 || l.Color.Z < 0 {
		return fmt.Errorf("%w: color %v must be finite and non-negative", ErrInvalidClusterLight, l.Color)
	}
	if !finite(l.Intensity) || l.Intensity < 0 {
		return fmt.Errorf("%w: intensity %v must be finite and non-negative", ErrInvalidClusterLight, l.Intensity)
	}
	if l.Kind == ClusterLightSpot {
		if !finite3(l.Direction) {
			return fmt.Errorf("%w: spot direction is not finite", ErrInvalidClusterLight)
		}
		if !finite(l.InnerConeCos) || !finite(l.OuterConeCos) {
			return fmt.Errorf("%w: spot cone cosines must be finite", ErrInvalidClusterLight)
		}
		if l.InnerConeCos < -1 || l.InnerConeCos > 1 || l.OuterConeCos < -1 || l.OuterConeCos > 1 {
			return fmt.Errorf("%w: spot cone cosines (%v, %v) out of [-1, 1]", ErrInvalidClusterLight, l.InnerConeCos, l.OuterConeCos)
		}
		if l.OuterConeCos > l.InnerConeCos {
			return fmt.Errorf("%w: OuterConeCos (%v) exceeds InnerConeCos (%v)", ErrInvalidClusterLight, l.OuterConeCos, l.InnerConeCos)
		}
	}
	return nil
}

func (l ClusterLight) normalizedDir() math3d.Vec3 {
	if l.Direction.LenSq() < 1e-12 {
		return math3d.Vec3{Y: -1}
	}
	return l.Direction.Norm()
}

func (l ClusterLight) spotScaleOffset() (scale, offset float32) {
	if l.Kind != ClusterLightSpot {
		return 0, 1
	}
	inner := math3d.Clamp(l.InnerConeCos, -1, 1)
	outer := math3d.Clamp(l.OuterConeCos, -1, inner)
	denom := inner - outer
	if denom < 1e-4 {
		denom = 1e-4
	}
	scale = 1.0 / denom
	offset = -outer * scale
	return scale, offset
}

func (l ClusterLight) isActive() bool {
	if l.Kind != ClusterLightPoint && l.Kind != ClusterLightSpot {
		return false
	}
	if !finite3(l.Position) || !finite(l.Range) || l.Range <= 0 {
		return false
	}
	if !finite(l.Intensity) || l.Intensity <= 0 || !finite3(l.Color) {
		return false
	}
	if l.Color.X <= 0 && l.Color.Y <= 0 && l.Color.Z <= 0 {
		return false
	}
	return true
}

// ClusterConfig configures the 3D frustum-cluster grid and its mobile uniform-buffer fallback. Zero fields use defaults from [DefaultClusterConfig].
type ClusterConfig struct {
	// TilesX is the number of screen-space horizontal tiles (default 16).
	TilesX int
	// TilesY is the number of screen-space vertical tiles (default 9).
	TilesY int
	// SlicesZ is the number of logarithmic view-space depth slices (default 24, max 64).
	SlicesZ int
	// MaxLights is the maximum number of visible local lights stored in clustered mode (default 512).
	MaxLights int
	// MaxLightsPerCluster caps how many lights can be assigned to one cluster cell (default 64).
	MaxLightsPerCluster int
	// FallbackMaxLights caps how many prioritized lights are uploaded in uniform-buffer fallback mode (default 16, max 16).
	FallbackMaxLights int
	// ForceFallback forces the uniform-buffer fallback path even on devices that support storage buffers and compute.
	ForceFallback bool
	// Retained keeps CPU copies of GPU buffers so NSGo recreates them after a device loss.
	Retained bool
	// Label prefixes GPU resource debug labels.
	Label string
}

// DefaultClusterConfig returns the standard 16x9x24 frustum-cluster grid supporting 512 lights (64 per cluster) and a 16-light mobile/WebGL 2 fallback.
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		TilesX:              DefaultClusterTilesX,
		TilesY:              DefaultClusterTilesY,
		SlicesZ:             DefaultClusterSlicesZ,
		MaxLights:           DefaultClusterMaxLights,
		MaxLightsPerCluster: DefaultMaxLightsPerCluster,
		FallbackMaxLights:   DefaultFallbackMaxLights,
	}
}

func (c ClusterConfig) withDefaults() ClusterConfig {
	if c.TilesX == 0 {
		c.TilesX = DefaultClusterTilesX
	}
	if c.TilesY == 0 {
		c.TilesY = DefaultClusterTilesY
	}
	if c.SlicesZ == 0 {
		c.SlicesZ = DefaultClusterSlicesZ
	}
	if c.MaxLights == 0 {
		c.MaxLights = DefaultClusterMaxLights
	}
	if c.MaxLightsPerCluster == 0 {
		c.MaxLightsPerCluster = DefaultMaxLightsPerCluster
	}
	if c.FallbackMaxLights == 0 {
		c.FallbackMaxLights = DefaultFallbackMaxLights
	}
	return c
}

// Validate checks that every field (after applying defaults to zero fields) is within supported limits. Every error wraps [ErrInvalidClusterConfig].
func (c ClusterConfig) Validate() error {
	c = c.withDefaults()
	if c.TilesX < 1 || c.TilesY < 1 {
		return fmt.Errorf("%w: tile grid %dx%d must be positive", ErrInvalidClusterConfig, c.TilesX, c.TilesY)
	}
	if c.SlicesZ < 1 || c.SlicesZ > MaxClusterSlicesZ {
		return fmt.Errorf("%w: SlicesZ %d must be in [1, %d]", ErrInvalidClusterConfig, c.SlicesZ, MaxClusterSlicesZ)
	}
	if c.MaxLights < 1 {
		return fmt.Errorf("%w: MaxLights %d must be positive", ErrInvalidClusterConfig, c.MaxLights)
	}
	if c.MaxLightsPerCluster < 1 {
		return fmt.Errorf("%w: MaxLightsPerCluster %d must be positive", ErrInvalidClusterConfig, c.MaxLightsPerCluster)
	}
	if c.FallbackMaxLights < 1 || c.FallbackMaxLights > MaxFallbackLights {
		return fmt.Errorf("%w: FallbackMaxLights %d must be in [1, %d]", ErrInvalidClusterConfig, c.FallbackMaxLights, MaxFallbackLights)
	}
	totalClusters := uint64(c.TilesX) * uint64(c.TilesY) * uint64(c.SlicesZ)
	if totalClusters == 0 || totalClusters > 1<<20 {
		return fmt.Errorf("%w: cluster count %d out of range", ErrInvalidClusterConfig, totalClusters)
	}
	return nil
}

// ClusterCount returns the total number of 3D frustum clusters (TilesX * TilesY * SlicesZ) after applying defaults.
func (c ClusterConfig) ClusterCount() int {
	c = c.withDefaults()
	return c.TilesX * c.TilesY * c.SlicesZ
}

// ClusterOffsetCount identifies the contiguous slice of [ClusterGrid.LightIndices] assigned to one cluster cell: indices[Offset : Offset+Count].
type ClusterOffsetCount struct {
	Offset uint32
	Count  uint32
}

// ClusterStats reports per-frame light culling and cluster packing metrics.
type ClusterStats struct {
	InputLights        int
	VisibleLights      int
	CulledLights       int
	ActiveClusters     int
	TotalAssignments   int
	MaxClusterLights   int
	AvgLightsPerActive float32
	TruncatedClusters  int
	TruncatedLights    int
}

// ClusterGrid builds a 3D view-space frustum cluster grid and assigns local point and spot lights to intersecting cells with zero steady-state allocations.
//
// Cluster AABBs are cached by (proj, near, far) so camera rotation/translation frames reuse the frustum cells without recomputing them.
type ClusterGrid struct {
	cfg ClusterConfig

	// Cached frustum AABB parameters.
	hasGrid     bool
	cachedProj  math3d.Mat4
	cachedNear  float32
	cachedFar   float32
	sliceDepths [MaxClusterSlicesZ + 1]float32
	aabbs       []math3d.AABB

	// Preallocated per-frame assignment scratch and packed outputs.
	activeLights   []ClusterLight
	clusterCounts  []uint32
	clusterTrunc   []bool
	scratchIndices []uint32
	offsets        []ClusterOffsetCount
	lightIndices   []uint32
	lastStats      ClusterStats
}

// NewClusterGrid validates cfg and allocates reusable storage for frustum clusters and light assignment.
func NewClusterGrid(cfg ClusterConfig) (*ClusterGrid, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	numClusters := cfg.ClusterCount()
	return &ClusterGrid{
		cfg:            cfg,
		aabbs:          make([]math3d.AABB, numClusters),
		activeLights:   make([]ClusterLight, 0, cfg.MaxLights),
		clusterCounts:  make([]uint32, numClusters),
		clusterTrunc:   make([]bool, numClusters),
		scratchIndices: make([]uint32, numClusters*cfg.MaxLightsPerCluster),
		offsets:        make([]ClusterOffsetCount, numClusters),
		lightIndices:   make([]uint32, 0, numClusters*min(cfg.MaxLightsPerCluster, 16)),
	}, nil
}

// Config returns the normalized [ClusterConfig] of g.
func (g *ClusterGrid) Config() ClusterConfig { return g.cfg }

// ClusterCount returns the total number of frustum cells (TilesX * TilesY * SlicesZ).
func (g *ClusterGrid) ClusterCount() int { return len(g.aabbs) }

// SliceDepths returns the positive view-space depth boundaries [near .. far] of length SlicesZ+1 computed on the most recent [ClusterGrid.AssignLights] or [ClusterGrid.BuildFrustum].
func (g *ClusterGrid) SliceDepths() []float32 { return g.sliceDepths[:g.cfg.SlicesZ+1] }

// ClusterAABB returns the cached view-space AABB of cluster (tileX, tileY, sliceZ).
func (g *ClusterGrid) ClusterAABB(tileX, tileY, sliceZ int) math3d.AABB {
	idx := g.ClusterIndex(tileX, tileY, sliceZ)
	if idx < 0 || idx >= len(g.aabbs) {
		return math3d.AABBEmpty()
	}
	return g.aabbs[idx]
}

// ClusterIndex returns the flat linear index for cluster coordinates (tileX, tileY, sliceZ).
func (g *ClusterGrid) ClusterIndex(tileX, tileY, sliceZ int) int {
	return (sliceZ*g.cfg.TilesY+tileY)*g.cfg.TilesX + tileX
}

// ActiveLights returns the frustum-visible lights packed by the most recent [ClusterGrid.AssignLights] call (indices in [ClusterGrid.LightIndices] reference this slice).
func (g *ClusterGrid) ActiveLights() []ClusterLight { return g.activeLights }

// Offsets returns the per-cluster offset/count table packed by the most recent [ClusterGrid.AssignLights] call.
func (g *ClusterGrid) Offsets() []ClusterOffsetCount { return g.offsets }

// LightIndices returns the flat packed light-index list referenced by [ClusterGrid.Offsets].
func (g *ClusterGrid) LightIndices() []uint32 { return g.lightIndices }

// ClusterLightIndices returns the light indices assigned to cluster (tileX, tileY, sliceZ) on the last [ClusterGrid.AssignLights] call.
func (g *ClusterGrid) ClusterLightIndices(tileX, tileY, sliceZ int) []uint32 {
	idx := g.ClusterIndex(tileX, tileY, sliceZ)
	if idx < 0 || idx >= len(g.offsets) {
		return nil
	}
	oc := g.offsets[idx]
	end := uint64(oc.Offset) + uint64(oc.Count)
	if end > uint64(len(g.lightIndices)) {
		return nil
	}
	return g.lightIndices[oc.Offset:end]
}

// LastStats returns the [ClusterStats] from the most recent [ClusterGrid.AssignLights] call.
func (g *ClusterGrid) LastStats() ClusterStats { return g.lastStats }

// SliceForDepth maps a positive view-space depth viewZ to its depth slice index in [0, SlicesZ-1] using the exact same binary search over [ClusterGrid.SliceDepths] as the GPU fragment shader.
func (g *ClusterGrid) SliceForDepth(viewZ float32) int {
	n := g.cfg.SlicesZ
	if n <= 1 {
		return 0
	}
	lo, hi := 0, n-1
	for lo < hi {
		mid := (lo + hi) >> 1
		if viewZ >= g.sliceDepths[mid+1] {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// BuildFrustum computes view-space depth slice boundaries and per-cluster view-space AABBs for (proj, near, far), caching the result until any of those inputs change.
func (g *ClusterGrid) BuildFrustum(proj math3d.Mat4, near, far float32) bool {
	if !finite(near) || !finite(far) || far <= near {
		return false
	}
	if g.hasGrid && g.cachedProj == proj && g.cachedNear == near && g.cachedFar == far {
		return true
	}
	invProj, ok := proj.Inverse()
	if !ok {
		return false
	}

	slices := g.cfg.SlicesZ
	useLog := near > 0 && proj[11] != 0
	if useLog {
		ratio := float64(far) / float64(near)
		invSlices := 1.0 / float64(slices)
		g.sliceDepths[0] = near
		for s := 1; s < slices; s++ {
			g.sliceDepths[s] = float32(float64(near) * math.Pow(ratio, float64(s)*invSlices))
		}
		g.sliceDepths[slices] = far
	} else {
		span := float64(far - near)
		invSlices := 1.0 / float64(slices)
		for s := 0; s <= slices; s++ {
			g.sliceDepths[s] = float32(float64(near) + span*float64(s)*invSlices)
		}
	}

	txN := g.cfg.TilesX
	tyN := g.cfg.TilesY
	invTX := 2.0 / float32(txN)
	invTY := 2.0 / float32(tyN)

	// Unproject a screen-NDC corner (nx, ny) onto view plane z = -depth using invProj.
	unprojectAtDepth := func(nx, ny, depth float32) math3d.Vec3 {
		z := -depth
		clipZ := proj[10]*z + proj[14]
		clipW := proj[11]*z + proj[15]
		if clipW == 0 {
			return math3d.Vec3{Z: z}
		}
		ndcZ := clipZ / clipW
		p := invProj.TransformPoint(math3d.Vec3{X: nx, Y: ny, Z: ndcZ})
		p.Z = z
		return p
	}

	for s := range slices {
		z0 := g.sliceDepths[s]
		z1 := g.sliceDepths[s+1]
		sliceBase := s * tyN * txN
		for ty := range tyN {
			ndcYTop := 1.0 - float32(ty)*invTY
			ndcYBot := 1.0 - float32(ty+1)*invTY
			rowBase := sliceBase + ty*txN
			for tx := range txN {
				ndcXL := -1.0 + float32(tx)*invTX
				ndcXR := -1.0 + float32(tx+1)*invTX

				c0 := unprojectAtDepth(ndcXL, ndcYBot, z0)
				c1 := unprojectAtDepth(ndcXR, ndcYBot, z0)
				c2 := unprojectAtDepth(ndcXL, ndcYTop, z0)
				c3 := unprojectAtDepth(ndcXR, ndcYTop, z0)
				c4 := unprojectAtDepth(ndcXL, ndcYBot, z1)
				c5 := unprojectAtDepth(ndcXR, ndcYBot, z1)
				c6 := unprojectAtDepth(ndcXL, ndcYTop, z1)
				c7 := unprojectAtDepth(ndcXR, ndcYTop, z1)

				minV := c0.Min(c1).Min(c2).Min(c3).Min(c4).Min(c5).Min(c6).Min(c7)
				maxV := c0.Max(c1).Max(c2).Max(c3).Max(c4).Max(c5).Max(c6).Max(c7)
				g.aabbs[rowBase+tx] = math3d.AABB{Min: minV, Max: maxV}
			}
		}
	}

	g.hasGrid = true
	g.cachedProj = proj
	g.cachedNear = near
	g.cachedFar = far
	return true
}

// AssignLights transforms lights into view space, culls lights outside the camera frustum, tests each visible light against its candidate frustum clusters (sphere-vs-AABB for all lights plus exact cone-vs-AABB rejection for spot lights), and packs compact offset/count and index arrays.
//
// Steady-state calls perform zero heap allocations.
func (g *ClusterGrid) AssignLights(view, proj math3d.Mat4, near, far float32, lights []ClusterLight) ClusterStats {
	stats := ClusterStats{InputLights: len(lights)}
	g.activeLights = g.activeLights[:0]
	clear(g.clusterCounts)
	clear(g.clusterTrunc)
	g.lightIndices = g.lightIndices[:0]

	numClusters := len(g.aabbs)
	if !g.BuildFrustum(proj, near, far) || len(lights) == 0 {
		clear(g.offsets)
		stats.CulledLights = len(lights)
		g.lastStats = stats
		return stats
	}

	tilesX := g.cfg.TilesX
	tilesY := g.cfg.TilesY
	slicesZ := g.cfg.SlicesZ
	maxPerCluster := g.cfg.MaxLightsPerCluster
	isPersp := proj[11] != 0

	for i := range lights {
		l := lights[i]
		if !l.isActive() {
			stats.CulledLights++
			continue
		}

		posV := view.TransformPoint(l.Position)
		sphereCenter := posV
		sphereRadius := l.Range

		var dirV math3d.Vec3
		var cosOuter, sinOuter float32
		isSpot := l.Kind == ClusterLightSpot
		if isSpot {
			dirW := l.normalizedDir()
			dirV = view.TransformDir(dirW)
			if dirV.LenSq() < 1e-12 {
				dirV = math3d.Vec3{Y: -1}
			} else {
				dirV = dirV.Norm()
			}
			cosOuter = math3d.Clamp(l.OuterConeCos, -1, 1)
			sinSq := 1.0 - cosOuter*cosOuter
			if sinSq < 0 {
				sinSq = 0
			}
			sinOuter = float32(math.Sqrt(float64(sinSq)))
			sphereCenter, sphereRadius = spotBoundingSphere(posV, dirV, l.Range, cosOuter, sinOuter)
		}

		// Quick view-space depth rejection (camera looks down -Z, positive depth = -z).
		minDepth := -sphereCenter.Z - sphereRadius
		maxDepth := -sphereCenter.Z + sphereRadius
		if maxDepth < near || minDepth > far {
			stats.CulledLights++
			continue
		}

		// Tight screen-tile rectangle in [0, tilesX-1] x [0, tilesY-1].
		txMin, txMax, tyMin, tyMax, visible := projectSphereTiles(proj, isPersp, near, far, sphereCenter, sphereRadius, tilesX, tilesY)
		if !visible {
			stats.CulledLights++
			continue
		}

		if len(g.activeLights) >= g.cfg.MaxLights {
			stats.TruncatedLights++
			continue
		}

		lightIdx := uint32(len(g.activeLights))
		g.activeLights = append(g.activeLights, l)

		// Tight Z-slice range via binary search on cached depth splits.
		sliceMin := 0
		if minDepth > near {
			sliceMin = g.SliceForDepth(minDepth)
		}
		sliceMax := slicesZ - 1
		if maxDepth < far {
			sliceMax = g.SliceForDepth(maxDepth)
		}
		if sliceMax < sliceMin {
			sliceMax = sliceMin
		}

		rSq := sphereRadius * sphereRadius
		anyCell := false
		for s := sliceMin; s <= sliceMax; s++ {
			sliceBase := s * tilesY * tilesX
			for ty := tyMin; ty <= tyMax; ty++ {
				rowBase := sliceBase + ty*tilesX
				for tx := txMin; tx <= txMax; tx++ {
					cIdx := rowBase + tx
					box := g.aabbs[cIdx]
					if !sphereIntersectsAABBSq(sphereCenter, rSq, box) {
						continue
					}
					if isSpot && cosOuter > 0 {
						if !spotConeIntersectsAABB(posV, dirV, l.Range, cosOuter, sinOuter, box) {
							continue
						}
					}
					anyCell = true
					cnt := int(g.clusterCounts[cIdx])
					if cnt < maxPerCluster {
						g.scratchIndices[cIdx*maxPerCluster+cnt] = lightIdx
						g.clusterCounts[cIdx] = uint32(cnt + 1)
					} else {
						g.clusterTrunc[cIdx] = true
					}
				}
			}
		}

		if !anyCell {
			// Light passed coarse screen-rect test but touched no cluster AABB.
			g.activeLights = g.activeLights[:len(g.activeLights)-1]
			stats.CulledLights++
			continue
		}
	}

	stats.VisibleLights = len(g.activeLights)

	// Compact scratch lists into contiguous lightIndices + offsets table.
	var total int
	for cIdx := range numClusters {
		cnt := int(g.clusterCounts[cIdx])
		if g.clusterTrunc[cIdx] {
			stats.TruncatedClusters++
		}
		if cnt == 0 {
			g.offsets[cIdx] = ClusterOffsetCount{}
			continue
		}
		stats.ActiveClusters++
		if cnt > stats.MaxClusterLights {
			stats.MaxClusterLights = cnt
		}
		g.offsets[cIdx] = ClusterOffsetCount{
			Offset: uint32(total),
			Count:  uint32(cnt),
		}
		total += cnt
	}

	if cap(g.lightIndices) < total {
		g.lightIndices = make([]uint32, total)
	} else {
		g.lightIndices = g.lightIndices[:total]
	}
	for cIdx := range numClusters {
		cnt := int(g.clusterCounts[cIdx])
		if cnt == 0 {
			continue
		}
		off := int(g.offsets[cIdx].Offset)
		srcOff := cIdx * maxPerCluster
		copy(g.lightIndices[off:off+cnt], g.scratchIndices[srcOff:srcOff+cnt])
	}

	stats.TotalAssignments = total
	if stats.ActiveClusters > 0 {
		stats.AvgLightsPerActive = float32(total) / float32(stats.ActiveClusters)
	}
	g.lastStats = stats
	return stats
}

// spotBoundingSphere computes the minimal bounding sphere enclosing a spotlight cone (Bart Wronski / Frostbite formulation).
func spotBoundingSphere(apex, dir math3d.Vec3, lightRange, cosOuter, sinOuter float32) (math3d.Vec3, float32) {
	if cosOuter <= 0 {
		return apex, lightRange
	}
	const cos45 = 0.70710678
	if cosOuter < cos45 {
		return apex.Add(dir.Scale(lightRange * cosOuter)), lightRange * sinOuter
	}
	r := lightRange / (2.0 * cosOuter)
	return apex.Add(dir.Scale(r)), r
}

func sphereIntersectsAABBSq(center math3d.Vec3, radiusSq float32, b math3d.AABB) bool {
	cx := math3d.Clamp(center.X, b.Min.X, b.Max.X)
	cy := math3d.Clamp(center.Y, b.Min.Y, b.Max.Y)
	cz := math3d.Clamp(center.Z, b.Min.Z, b.Max.Z)
	dx := center.X - cx
	dy := center.Y - cy
	dz := center.Z - cz
	return dx*dx+dy*dy+dz*dz <= radiusSq
}

// spotConeIntersectsAABB conservatively tests whether a spotlight cone (cosOuter > 0) intersects box b by testing the cone against b's bounding sphere. Guaranteed zero false negatives.
func spotConeIntersectsAABB(apex, dir math3d.Vec3, lightRange, cosOuter, sinOuter float32, b math3d.AABB) bool {
	center := b.Center()
	radius := b.HalfExtents().Len()

	v := center.Sub(apex)
	vLenSq := v.LenSq()
	v1Len := v.Dot(dir)

	if v1Len < -radius || v1Len > lightRange+radius {
		return false
	}

	perpSq := vLenSq - v1Len*v1Len
	if perpSq < 0 {
		perpSq = 0
	}
	dPerp := float32(math.Sqrt(float64(perpSq)))

	// Projection along the closest cone generator ray: if <= 0, the apex is the closest point on the cone.
	if v1Len*cosOuter+dPerp*sinOuter <= 0 {
		return vLenSq <= radius*radius
	}
	closestDist := dPerp*cosOuter - v1Len*sinOuter
	return closestDist <= radius
}

// projectSphereTiles computes a conservative [txMin..txMax] x [tyMin..tyMax] screen-tile rectangle for a view-space bounding sphere.
func projectSphereTiles(proj math3d.Mat4, isPersp bool, near, far float32, center math3d.Vec3, radius float32, tilesX, tilesY int) (txMin, txMax, tyMin, tyMax int, visible bool) {
	zMax := center.Z + radius
	zMin := center.Z - radius
	if isPersp && zMax >= -near*1.001 {
		// Sphere crosses or touches the near plane: conservatively span the full screen so near-plane clipping never produces a false negative.
		return 0, tilesX - 1, 0, tilesY - 1, true
	}
	if zMin < -far {
		zMin = -far
	}
	if zMax > -near {
		zMax = -near
	}

	xs := [2]float32{center.X - radius, center.X + radius}
	ys := [2]float32{center.Y - radius, center.Y + radius}
	zs := [2]float32{zMin, zMax}

	minX, maxX := float32(math.Inf(1)), float32(math.Inf(-1))
	minY, maxY := float32(math.Inf(1)), float32(math.Inf(-1))

	for _, x := range xs {
		for _, y := range ys {
			for _, z := range zs {
				clip := proj.MulVec4(math3d.Vec4{X: x, Y: y, Z: z, W: 1})
				if clip.W <= 1e-6 {
					return 0, tilesX - 1, 0, tilesY - 1, true
				}
				invW := 1.0 / clip.W
				nx := clip.X * invW
				ny := clip.Y * invW
				if nx < minX {
					minX = nx
				}
				if nx > maxX {
					maxX = nx
				}
				if ny < minY {
					minY = ny
				}
				if ny > maxY {
					maxY = ny
				}
			}
		}
	}

	if maxX < -1 || minX > 1 || maxY < -1 || minY > 1 {
		return 0, 0, 0, 0, false
	}

	txMin = clusterClampInt(int(math.Floor(float64((minX+1)*0.5*float32(tilesX)))), 0, tilesX-1)
	txMax = clusterClampInt(int(math.Floor(float64((maxX+1)*0.5*float32(tilesX)))), 0, tilesX-1)
	tyMin = clusterClampInt(int(math.Floor(float64((1-maxY)*0.5*float32(tilesY)))), 0, tilesY-1)
	tyMax = clusterClampInt(int(math.Floor(float64((1-minY)*0.5*float32(tilesY)))), 0, tilesY-1)
	return txMin, txMax, tyMin, tyMax, true
}

func clusterClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

type scoredClusterLight struct {
	index int
	score float32
}

// SelectFallbackLights chooses up to maxLights highest-priority local lights for mobile / WebGL 2 / GLES 3.0 uniform-buffer fallback shading.
//
// Active lights are ranked deterministically by radiant luminance, proximity to cameraPos (and distance relative to Range), and whether their sphere extends in front of cameraForward, breaking ties by input order. Results are appended to dst[:0].
func SelectFallbackLights(dst []ClusterLight, cameraPos, cameraForward math3d.Vec3, lights []ClusterLight, maxLights int) []ClusterLight {
	dst = dst[:0]
	if maxLights <= 0 || len(lights) == 0 {
		return dst
	}
	scored := make([]scoredClusterLight, 0, len(lights))
	return selectFallbackLightsWithScratch(dst, &scored, cameraPos, cameraForward, lights, maxLights)
}

func selectFallbackLightsWithScratch(dst []ClusterLight, scratch *[]scoredClusterLight, cameraPos, cameraForward math3d.Vec3, lights []ClusterLight, maxLights int) []ClusterLight {
	dst = dst[:0]
	if maxLights <= 0 || len(lights) == 0 {
		return dst
	}
	fwd := cameraForward
	if fwd.LenSq() < 1e-12 {
		fwd = math3d.Vec3Forward()
	} else {
		fwd = fwd.Norm()
	}

	scored := (*scratch)[:0]
	for i := range lights {
		l := lights[i]
		if !l.isActive() {
			continue
		}
		lum := 0.2126*l.Color.X + 0.7152*l.Color.Y + 0.0722*l.Color.Z
		if lum <= 0 {
			lum = max(l.Color.X, max(l.Color.Y, l.Color.Z))
		}
		toLight := l.Position.Sub(cameraPos)
		distSq := toLight.LenSq()
		dist := float32(math.Sqrt(float64(distSq)))

		// Smooth importance combining power, range coverage, and camera distance.
		denom := distSq + 0.25
		score := l.Intensity * lum * (l.Range * l.Range) / (denom * (1.0 + dist/max(l.Range, 1e-3)))

		// Prefer lights whose sphere extends in front of the camera plane.
		if toLight.Dot(fwd)+l.Range >= 0 {
			score *= 4.0
		} else {
			score *= 0.25
		}
		scored = append(scored, scoredClusterLight{index: i, score: score})
	}
	*scratch = scored

	if len(scored) == 0 {
		return dst
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].index < scored[j].index
	})
	keep := min(len(scored), maxLights)
	if cap(dst) < keep {
		dst = make([]ClusterLight, 0, keep)
	}
	for i := range keep {
		dst = append(dst, lights[scored[i].index])
	}
	return dst
}

// EvaluateLightAttenuation computes the unit direction from worldPos to light and its combined glTF KHR_lights_punctual range, intensity, and spotlight angular attenuation.
func EvaluateLightAttenuation(light ClusterLight, worldPos math3d.Vec3) (dirToLight math3d.Vec3, attenuation float32) {
	if !light.isActive() {
		return math3d.Vec3{}, 0
	}
	toLight := light.Position.Sub(worldPos)
	distSq := toLight.LenSq()
	dist := float32(math.Sqrt(float64(distSq)))
	if dist >= light.Range {
		return math3d.Vec3{}, 0
	}
	if dist > 1e-6 {
		dirToLight = toLight.Scale(1.0 / dist)
	} else {
		dirToLight = math3d.Vec3Up()
	}

	ratio := dist / light.Range
	r2 := ratio * ratio
	r4 := r2 * r2
	win := math3d.Clamp(1.0-r4, 0, 1)
	atten := (win * win) / max(distSq, 1e-4)

	if light.Kind == ClusterLightSpot {
		lightDir := light.normalizedDir()
		scale, offset := light.spotScaleOffset()
		cd := lightDir.Dot(dirToLight.Neg())
		spot := math3d.Clamp(cd*scale+offset, 0, 1)
		atten *= spot * spot
	}
	return dirToLight, atten * light.Intensity
}

// EvaluateDirectLights computes the pure-Go analytic Lambertian diffuse radiance for worldPos and normal over all lights, matching [ClusteredFallbackWGSL] and serving as ground truth for clustered lighting parity tests.
func EvaluateDirectLights(worldPos, normal, albedo, ambient math3d.Vec3, lights []ClusterLight) math3d.Vec3 {
	n := normal
	if n.LenSq() > 1e-12 {
		n = n.Norm()
	}
	irr := ambient
	for i := range lights {
		l := lights[i]
		lDir, atten := EvaluateLightAttenuation(l, worldPos)
		if atten <= 0 {
			continue
		}
		ndl := max(n.Dot(lDir), 0)
		if ndl <= 0 {
			continue
		}
		irr = irr.Add(l.Color.Scale(atten * ndl))
	}
	return math3d.Vec3{X: irr.X * albedo.X, Y: irr.Y * albedo.Y, Z: irr.Z * albedo.Z}
}

// EvaluateClusteredLights computes the pure-Go analytic Lambertian diffuse radiance at worldPos using only the lights assigned to worldPos's frustum cluster in grid, matching [ClusteredLitWGSL] byte-for-byte in formula.
func EvaluateClusteredLights(worldPos, normal, albedo, ambient math3d.Vec3, grid *ClusterGrid, view, proj math3d.Mat4, viewportW, viewportH float32) math3d.Vec3 {
	if grid == nil {
		return math3d.Vec3{X: ambient.X * albedo.X, Y: ambient.Y * albedo.Y, Z: ambient.Z * albedo.Z}
	}
	viewPos := view.TransformPoint(worldPos)
	viewZ := -viewPos.Z

	clip := proj.MulVec4(viewPos.Vec4(1))
	if clip.W <= 1e-6 {
		return math3d.Vec3{X: ambient.X * albedo.X, Y: ambient.Y * albedo.Y, Z: ambient.Z * albedo.Z}
	}
	ndcX := clip.X / clip.W
	ndcY := clip.Y / clip.W
	fragX := (ndcX + 1.0) * 0.5 * viewportW
	fragY := (1.0 - ndcY) * 0.5 * viewportH

	tx := clusterClampInt(int(max(fragX, 0)/max(viewportW, 1)*float32(grid.cfg.TilesX)), 0, grid.cfg.TilesX-1)
	ty := clusterClampInt(int(max(fragY, 0)/max(viewportH, 1)*float32(grid.cfg.TilesY)), 0, grid.cfg.TilesY-1)
	sz := grid.SliceForDepth(viewZ)

	n := normal
	if n.LenSq() > 1e-12 {
		n = n.Norm()
	}
	irr := ambient
	for _, lightIdx := range grid.ClusterLightIndices(tx, ty, sz) {
		l := grid.activeLights[lightIdx]
		lDir, atten := EvaluateLightAttenuation(l, worldPos)
		if atten <= 0 {
			continue
		}
		ndl := max(n.Dot(lDir), 0)
		if ndl <= 0 {
			continue
		}
		irr = irr.Add(l.Color.Scale(atten * ndl))
	}
	return math3d.Vec3{X: irr.X * albedo.X, Y: irr.Y * albedo.Y, Z: irr.Z * albedo.Z}
}

// DeviceSupportsClusteredLighting reports whether dev exposes the storage-buffer and compute capabilities needed for the clustered forward shading path. Devices without them (such as WebGL 2, OpenGL ES 3.0, or headless tests under nsgotest.NoGPUCompute) fall back to the uniform-buffer path automatically.
func DeviceSupportsClusteredLighting(dev *gpu.Device) bool {
	if dev == nil {
		return false
	}
	caps := dev.Capabilities()
	return caps.Features.StorageBuffers &&
		caps.Features.Compute &&
		caps.Limits.MaxStorageBufferBindingSize >= clusterMinStorageBindingBytes
}

// ClusteredBindingsWGSL declares the @group(1) uniform and read-only storage bindings plus evaluate_clustered_lights(world_pos, normal, frag_pos) for the desktop clustered forward shading path.
//
// Depth slicing uses a 6-step binary search over the uniform slice_depths split table (avoiding log2/exp so it runs identically on Metal, D3D11, OpenGL, WebGPU, and NSGo's software GPU VM).
const ClusteredBindingsWGSL = `
struct ClusterLightGPU {
	pos_range: vec4<f32>,
	color_intensity: vec4<f32>,
	dir_kind: vec4<f32>,
	spot_params: vec4<f32>,
}

struct ClusterParams {
	grid_info: vec4<u32>,
	viewport_depth: vec4<f32>,
	ambient: vec4<f32>,
	slice_depths: array<vec4<f32>, 17>,
}

@group(1) @binding(0) var<uniform> cluster_params: ClusterParams;
@group(1) @binding(1) var<storage, read> cluster_lights: array<ClusterLightGPU>;
@group(1) @binding(2) var<storage, read> cluster_cells: array<vec4<u32>>;
@group(1) @binding(3) var<storage, read> cluster_indices: array<u32>;

fn cluster_slice_depth(idx: u32) -> f32 {
	let v = cluster_params.slice_depths[idx / 4u];
	let lane = idx % 4u;
	if (lane == 0u) { return v.x; }
	if (lane == 1u) { return v.y; }
	if (lane == 2u) { return v.z; }
	return v.w;
}

fn cluster_find_slice(view_z: f32) -> u32 {
	let slices = cluster_params.grid_info.z;
	if (slices <= 1u) {
		return 0u;
	}
	var lo: u32 = 0u;
	var hi: u32 = slices - 1u;
	for (var step: u32 = 0u; step < 6u; step = step + 1u) {
		if (lo < hi) {
			let mid = (lo + hi) / 2u;
			if (view_z >= cluster_slice_depth(mid + 1u)) {
				lo = mid + 1u;
			} else {
				hi = mid;
			}
		}
	}
	return min(lo, slices - 1u);
}

fn cluster_eval_one_light(light: ClusterLightGPU, world_pos: vec3<f32>, normal: vec3<f32>) -> vec3<f32> {
	let light_range = light.pos_range.w;
	let intensity = light.color_intensity.w;
	if (light_range <= 0.0 || intensity <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let to_light = light.pos_range.xyz - world_pos;
	let dist_sq = dot(to_light, to_light);
	let dist = sqrt(dist_sq);
	if (dist >= light_range) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	var l_dir = vec3<f32>(0.0, 1.0, 0.0);
	if (dist > 0.000001) {
		l_dir = to_light / dist;
	}
	let ndl = max(dot(normal, l_dir), 0.0);
	if (ndl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ratio = dist / light_range;
	let r2 = ratio * ratio;
	let r4 = r2 * r2;
	let win = clamp(1.0 - r4, 0.0, 1.0);
	var atten = (win * win) / max(dist_sq, 0.0001);
	if (light.dir_kind.w > 0.5) {
		let cd = dot(light.dir_kind.xyz, -l_dir);
		let spot = clamp(cd * light.spot_params.x + light.spot_params.y, 0.0, 1.0);
		atten = atten * spot * spot;
	}
	return light.color_intensity.rgb * (intensity * atten * ndl);
}

fn evaluate_clustered_lights(world_pos: vec3<f32>, normal: vec3<f32>, frag_pos: vec4<f32>) -> vec3<f32> {
	let n = normalize(normal);
	var irradiance = cluster_params.ambient.rgb;
	let tiles_x = cluster_params.grid_info.x;
	let tiles_y = cluster_params.grid_info.y;
	if (tiles_x == 0u || tiles_y == 0u) {
		return irradiance;
	}
	let vp_w = max(cluster_params.viewport_depth.x, 1.0);
	let vp_h = max(cluster_params.viewport_depth.y, 1.0);
	let tx = min(u32(max(frag_pos.x, 0.0) / vp_w * f32(tiles_x)), tiles_x - 1u);
	let ty = min(u32(max(frag_pos.y, 0.0) / vp_h * f32(tiles_y)), tiles_y - 1u);
	let view_pos = camera.view * vec4<f32>(world_pos, 1.0);
	let slice_z = cluster_find_slice(-view_pos.z);
	let cluster_idx = (slice_z * tiles_y + ty) * tiles_x + tx;
	let cell = cluster_cells[cluster_idx];
	let offset = cell.x;
	let count = cell.y;
	for (var i: u32 = 0u; i < count; i = i + 1u) {
		let light_idx = cluster_indices[offset + i];
		irradiance = irradiance + cluster_eval_one_light(cluster_lights[light_idx], world_pos, n);
	}
	return irradiance;
}
`

// ClusteredFallbackBindingsWGSL declares the @group(1) pure uniform-buffer binding plus evaluate_clustered_lights(world_pos, normal, frag_pos) with the identical signature as [ClusteredBindingsWGSL], using zero storage buffers and zero compute shaders for WebGL 2 / OpenGL ES 3.0 targets.
const ClusteredFallbackBindingsWGSL = `
struct ClusterLightGPU {
	pos_range: vec4<f32>,
	color_intensity: vec4<f32>,
	dir_kind: vec4<f32>,
	spot_params: vec4<f32>,
}

struct FallbackLightUniform {
	info: vec4<u32>,
	ambient: vec4<f32>,
	lights: array<ClusterLightGPU, 16>,
}

@group(1) @binding(0) var<uniform> fallback_lights: FallbackLightUniform;

fn cluster_eval_one_light(light: ClusterLightGPU, world_pos: vec3<f32>, normal: vec3<f32>) -> vec3<f32> {
	let light_range = light.pos_range.w;
	let intensity = light.color_intensity.w;
	if (light_range <= 0.0 || intensity <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let to_light = light.pos_range.xyz - world_pos;
	let dist_sq = dot(to_light, to_light);
	let dist = sqrt(dist_sq);
	if (dist >= light_range) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	var l_dir = vec3<f32>(0.0, 1.0, 0.0);
	if (dist > 0.000001) {
		l_dir = to_light / dist;
	}
	let ndl = max(dot(normal, l_dir), 0.0);
	if (ndl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let ratio = dist / light_range;
	let r2 = ratio * ratio;
	let r4 = r2 * r2;
	let win = clamp(1.0 - r4, 0.0, 1.0);
	var atten = (win * win) / max(dist_sq, 0.0001);
	if (light.dir_kind.w > 0.5) {
		let cd = dot(light.dir_kind.xyz, -l_dir);
		let spot = clamp(cd * light.spot_params.x + light.spot_params.y, 0.0, 1.0);
		atten = atten * spot * spot;
	}
	return light.color_intensity.rgb * (intensity * atten * ndl);
}

fn evaluate_clustered_lights(world_pos: vec3<f32>, normal: vec3<f32>, frag_pos: vec4<f32>) -> vec3<f32> {
	let n = normalize(normal);
	var irradiance = fallback_lights.ambient.rgb;
	let count = min(fallback_lights.info.x, 16u);
	for (var i: u32 = 0u; i < count; i = i + 1u) {
		irradiance = irradiance + cluster_eval_one_light(fallback_lights.lights[i], world_pos, n);
	}
	return irradiance;
}
`

const clusteredDefaultStageWGSL = `
struct ClusterVertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct ClusterVertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
}

@vertex
fn vs_main(in: ClusterVertexIn) -> ClusterVertexOut {
	var out: ClusterVertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world_pos = in.pos;
	out.normal = in.normal;
	return out;
}

@fragment
fn fs_main(in: ClusterVertexOut) -> @location(0) vec4<f32> {
	let lit = evaluate_clustered_lights(in.world_pos, in.normal, in.clip);
	return vec4<f32>(clamp(lit, vec3<f32>(0.0), vec3<f32>(1.0)), 1.0);
}
`

// ClusteredLitWGSL is a ready-to-compile WGSL module for [MeshVertexLayout] meshes combining [CameraWGSL] (@group(0)) and [ClusteredBindingsWGSL] (@group(1)).
const ClusteredLitWGSL = CameraWGSL + ClusteredBindingsWGSL + clusteredDefaultStageWGSL

// ClusteredFallbackWGSL is a ready-to-compile WGSL module for [MeshVertexLayout] meshes combining [CameraWGSL] (@group(0)) and [ClusteredFallbackBindingsWGSL] (@group(1)) without storage buffers or compute shaders.
const ClusteredFallbackWGSL = CameraWGSL + ClusteredFallbackBindingsWGSL + clusteredDefaultStageWGSL

// ClusteredLightSystem manages the GPU buffers, bind group layout, and bind group for clustered forward shading with automatic fallback to a pure uniform-buffer path on devices without storage buffers or compute (WebGL 2, OpenGL ES 3.0, nsgotest.NoGPUCompute).
//
// Create it once on device readiness, call [ClusteredLightSystem.Update] or [ClusteredLightSystem.UpdateCamera] each frame, bind [ClusteredLightSystem.BindGroup] at [ClusteredLightGroup] (group 1), and shade with [ClusteredLightSystem.WGSL] or your own shader prepending [CameraWGSL] + [ClusteredLightSystem.BindingsWGSL].
type ClusteredLightSystem struct {
	cfg      ClusterConfig
	fallback bool
	grid     *ClusterGrid

	// GPU resources (owned by the core.Scope passed to NewClusteredLightSystem).
	layout      *gpu.BindGroupLayout
	group       *gpu.BindGroup
	paramsBuf   *gpu.Buffer // clustered: ClusterParams uniform; fallback: FallbackLightUniform
	lightsBuf   *gpu.Buffer // clustered only: storage array<ClusterLightGPU>
	cellsBuf    *gpu.Buffer // clustered only: storage array<vec4<u32>>
	indicesBuf  *gpu.Buffer // clustered only: storage array<u32>
	indicesCap  int         // capacity of indicesBuf in uint32 entries
	fallbackDst []ClusterLight
	fallbackTmp []scoredClusterLight
	lastStats   ClusterStats

	// Reusable CPU upload scratch buffers (zero steady-state allocations).
	paramsBytes  [ClusterParamsUniformSize]byte
	fallbackData [ClusteredFallbackUniformSize]byte
	lightsBytes  []byte
	cellsBytes   []byte
	indicesBytes []byte
}

// NewClusteredLightSystem validates cfg, inspects dev's GPU capabilities, and allocates either the storage-buffer clustered lighting path or the uniform-buffer mobile/WebGL 2 fallback path scoped to cx.
func NewClusteredLightSystem(cx *core.Scope, dev *gpu.Device, cfg ClusterConfig) (*ClusteredLightSystem, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	cfg = cfg.withDefaults()

	useFallback := cfg.ForceFallback || !DeviceSupportsClusteredLighting(dev)

	label := cfg.Label
	if label == "" {
		label = "render3d.clustered"
	}

	if useFallback {
		return newClusteredFallbackSystem(cx, dev, cfg, label)
	}
	return newClusteredStorageSystem(cx, dev, cfg, label)
}

func newClusteredFallbackSystem(cx *core.Scope, dev *gpu.Device, cfg ClusterConfig, label string) (*ClusteredLightSystem, error) {
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " fallback lights",
		Size:     ClusteredFallbackUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: cfg.Retained,
	})
	if err != nil {
		return nil, err
	}
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " fallback layout",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		_ = buf.Close()
		return nil, err
	}
	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " fallback group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buf,
			Size:    ClusteredFallbackUniformSize,
		}},
	})
	if err != nil {
		_ = layout.Close()
		_ = buf.Close()
		return nil, err
	}

	return &ClusteredLightSystem{
		cfg:         cfg,
		fallback:    true,
		layout:      layout,
		group:       group,
		paramsBuf:   buf,
		fallbackDst: make([]ClusterLight, 0, cfg.FallbackMaxLights),
		fallbackTmp: make([]scoredClusterLight, 0, cfg.MaxLights),
	}, nil
}

func newClusteredStorageSystem(cx *core.Scope, dev *gpu.Device, cfg ClusterConfig, label string) (*ClusteredLightSystem, error) {
	grid, err := NewClusterGrid(cfg)
	if err != nil {
		return nil, err
	}

	numClusters := cfg.ClusterCount()
	lightsSize := uint64(cfg.MaxLights) * ClusterLightGPUSize
	cellsSize := uint64(numClusters) * ClusterCellGPUSize
	indicesCap := numClusters * cfg.MaxLightsPerCluster
	indicesSize := uint64(indicesCap) * 4

	// Respect MaxStorageBufferBindingSize by capping indices buffer capacity safely if needed.
	maxBind := dev.Capabilities().Limits.MaxStorageBufferBindingSize
	if maxBind > 0 && indicesSize > maxBind {
		indicesCap = max(int(maxBind/4), 1)
		indicesSize = uint64(indicesCap) * 4
	}

	var toClose []interface{ Close() error }
	fail := func(err error) (*ClusteredLightSystem, error) {
		for i := len(toClose) - 1; i >= 0; i-- {
			_ = toClose[i].Close()
		}
		return nil, err
	}

	paramsBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " params",
		Size:     ClusterParamsUniformSize,
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Retained: cfg.Retained,
	})
	if err != nil {
		return fail(err)
	}
	toClose = append(toClose, paramsBuf)

	lightsBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " lights",
		Size:     lightsSize,
		Usage:    gpu.UsageStorage | gpu.UsageCopyDst,
		Retained: cfg.Retained,
	})
	if err != nil {
		return fail(err)
	}
	toClose = append(toClose, lightsBuf)

	cellsBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " cells",
		Size:     cellsSize,
		Usage:    gpu.UsageStorage | gpu.UsageCopyDst,
		Retained: cfg.Retained,
	})
	if err != nil {
		return fail(err)
	}
	toClose = append(toClose, cellsBuf)

	indicesBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    label + " indices",
		Size:     indicesSize,
		Usage:    gpu.UsageStorage | gpu.UsageCopyDst,
		Retained: cfg.Retained,
	})
	if err != nil {
		return fail(err)
	}
	toClose = append(toClose, indicesBuf)

	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: label + " layout",
		Entries: []gpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
			{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
			{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
			{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
		},
	})
	if err != nil {
		return fail(err)
	}
	toClose = append(toClose, layout)

	group, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:  label + " group",
		Layout: layout,
		Entries: []gpu.BindGroupEntry{
			{Binding: 0, Buffer: paramsBuf, Size: ClusterParamsUniformSize},
			{Binding: 1, Buffer: lightsBuf, Size: lightsSize},
			{Binding: 2, Buffer: cellsBuf, Size: cellsSize},
			{Binding: 3, Buffer: indicesBuf, Size: indicesSize},
		},
	})
	if err != nil {
		return fail(err)
	}

	return &ClusteredLightSystem{
		cfg:          cfg,
		fallback:     false,
		grid:         grid,
		layout:       layout,
		group:        group,
		paramsBuf:    paramsBuf,
		lightsBuf:    lightsBuf,
		cellsBuf:     cellsBuf,
		indicesBuf:   indicesBuf,
		indicesCap:   indicesCap,
		lightsBytes:  make([]byte, lightsSize),
		cellsBytes:   make([]byte, cellsSize),
		indicesBytes: make([]byte, indicesSize),
	}, nil
}

// IsClustered reports whether s is running the storage-buffer clustered forward shading path.
func (s *ClusteredLightSystem) IsClustered() bool { return s != nil && !s.fallback }

// IsFallback reports whether s is running the uniform-buffer mobile / WebGL 2 / GLES 3.0 fallback path.
func (s *ClusteredLightSystem) IsFallback() bool { return s != nil && s.fallback }

// Config returns the normalized [ClusterConfig] used to construct s.
func (s *ClusteredLightSystem) Config() ClusterConfig {
	if s == nil {
		return ClusterConfig{}
	}
	return s.cfg
}

// Grid returns the underlying [ClusterGrid] in clustered mode, or nil in fallback mode.
func (s *ClusteredLightSystem) Grid() *ClusterGrid {
	if s == nil {
		return nil
	}
	return s.grid
}

// Layout returns the bind group layout a pipeline lists at [ClusteredLightGroup] (group 1).
func (s *ClusteredLightSystem) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

// BindGroup returns the bind group a render pass sets at [ClusteredLightGroup] (group 1).
func (s *ClusteredLightSystem) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	return s.group
}

// WGSL returns the ready-to-compile WGSL shader matching s's active path ([ClusteredLitWGSL] or [ClusteredFallbackWGSL]).
func (s *ClusteredLightSystem) WGSL() string {
	if s.IsFallback() {
		return ClusteredFallbackWGSL
	}
	return ClusteredLitWGSL
}

// BindingsWGSL returns the @group(1) declarations and evaluate_clustered_lights function matching s's active path ([ClusteredBindingsWGSL] or [ClusteredFallbackBindingsWGSL]).
func (s *ClusteredLightSystem) BindingsWGSL() string {
	if s.IsFallback() {
		return ClusteredFallbackBindingsWGSL
	}
	return ClusteredBindingsWGSL
}

// FallbackLights returns the prioritized lights uploaded on the most recent update in fallback mode (nil in clustered mode).
func (s *ClusteredLightSystem) FallbackLights() []ClusterLight {
	if s == nil || !s.fallback {
		return nil
	}
	return s.fallbackDst
}

// LastStats returns the [ClusterStats] from the most recent [ClusteredLightSystem.Update] or [ClusteredLightSystem.UpdateCamera] call.
func (s *ClusteredLightSystem) LastStats() ClusterStats {
	if s == nil {
		return ClusterStats{}
	}
	return s.lastStats
}

// ByteSize returns the total GPU buffer memory owned by s, for [RenderStats] and [Budget] accounting.
func (s *ClusteredLightSystem) ByteSize() uint64 {
	if s == nil {
		return 0
	}
	var total uint64
	for _, b := range []*gpu.Buffer{s.paramsBuf, s.lightsBuf, s.cellsBuf, s.indicesBuf} {
		if b != nil {
			total += b.Size()
		}
	}
	return total
}

// Valid reports whether s's GPU resources are open and belong to a live device.
func (s *ClusteredLightSystem) Valid() bool {
	if s == nil || !s.paramsBuf.Valid() || !s.layout.Valid() || !s.group.Valid() {
		return false
	}
	if s.fallback {
		return true
	}
	return s.lightsBuf.Valid() && s.cellsBuf.Valid() && s.indicesBuf.Valid()
}

// Bind sets s's bind group at group on pass. It records nothing when s is not [ClusteredLightSystem.Valid].
func (s *ClusteredLightSystem) Bind(pass *gpu.RenderPass, group uint32) {
	if !s.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(group, s.group)
}

// Update assigns lights for the frame and uploads the resulting lighting buffers to dev.
//
// In clustered mode, lights are binned into view-space frustum clusters via [ClusterGrid.AssignLights] and uploaded into the uniform and storage buffers. In fallback mode, the top [ClusterConfig.FallbackMaxLights] prioritized lights are selected via [SelectFallbackLights] and uploaded into the uniform buffer.
func (s *ClusteredLightSystem) Update(dev *gpu.Device, view, proj math3d.Mat4, near, far, viewportW, viewportH float32, ambient math3d.Vec3, lights []ClusterLight) ClusterStats {
	if s == nil {
		return ClusterStats{}
	}
	if s.fallback {
		camPos, camFwd := cameraPoseFromView(view)
		s.fallbackDst = selectFallbackLightsWithScratch(s.fallbackDst, &s.fallbackTmp, camPos, camFwd, lights, s.cfg.FallbackMaxLights)
		stats := ClusterStats{
			InputLights:   len(lights),
			VisibleLights: len(s.fallbackDst),
			CulledLights:  len(lights) - len(s.fallbackDst),
		}
		if len(s.fallbackTmp) > len(s.fallbackDst) {
			stats.TruncatedLights = len(s.fallbackTmp) - len(s.fallbackDst)
		}
		s.lastStats = stats
		if dev != nil && s.Valid() {
			encodeFallbackUniform(&s.fallbackData, ambient, s.fallbackDst)
			dev.WriteBuffer(s.paramsBuf, 0, s.fallbackData[:])
		}
		return stats
	}

	stats := s.grid.AssignLights(view, proj, near, far, lights)
	s.lastStats = stats
	if dev == nil || !s.Valid() {
		return stats
	}

	encodeClusterParams(&s.paramsBytes, s.cfg, uint32(len(s.grid.activeLights)), viewportW, viewportH, near, far, ambient, s.grid.SliceDepths())
	dev.WriteBuffer(s.paramsBuf, 0, s.paramsBytes[:])

	active := s.grid.activeLights
	if len(active) > 0 {
		nBytes := len(active) * ClusterLightGPUSize
		for i := range active {
			encodeClusterLightGPU(s.lightsBytes[i*ClusterLightGPUSize:], active[i])
		}
		dev.WriteBuffer(s.lightsBuf, 0, s.lightsBytes[:nBytes])
	}

	offsets := s.grid.offsets
	indices := s.grid.lightIndices
	maxIdx := uint32(s.indicesCap)
	for i, oc := range offsets {
		off := oc.Offset
		cnt := oc.Count
		if off >= maxIdx {
			off = 0
			cnt = 0
		} else if off+cnt > maxIdx {
			cnt = maxIdx - off
		}
		base := i * ClusterCellGPUSize
		binary.LittleEndian.PutUint32(s.cellsBytes[base+0:], off)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+4:], cnt)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+8:], 0)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+12:], 0)
	}
	dev.WriteBuffer(s.cellsBuf, 0, s.cellsBytes)

	if len(indices) > 0 {
		nIdx := min(len(indices), s.indicesCap)
		for i := range nIdx {
			binary.LittleEndian.PutUint32(s.indicesBytes[i*4:], indices[i])
		}
		dev.WriteBuffer(s.indicesBuf, 0, s.indicesBytes[:nIdx*4])
	}

	return stats
}

// UpdateCamera is a convenience wrapper around [ClusteredLightSystem.Update] extracting view, projection, near, and far from cam.
func (s *ClusteredLightSystem) UpdateCamera(dev *gpu.Device, cam *scene3d.Camera, viewportW, viewportH float32, ambient math3d.Vec3, lights []ClusterLight) ClusterStats {
	if cam == nil {
		return ClusterStats{}
	}
	near := cam.Projection.Near
	far := cam.Projection.Far
	if near <= 0 && cam.Projection.Kind != scene3d.ProjectionOrthographic {
		near = 0.1
	}
	if near == 0 && far == 0 {
		near, far = 0.1, 1000
	} else if far <= near {
		far = near + 1000
	}
	return s.Update(dev, cam.ViewMatrix(), cam.ProjMatrix(), near, far, viewportW, viewportH, ambient, lights)
}

// Close releases all GPU resources owned by s now instead of when their scope is disposed. Safe to call on nil or more than once.
func (s *ClusteredLightSystem) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for _, c := range []interface{ Close() error }{s.group, s.layout, s.indicesBuf, s.cellsBuf, s.lightsBuf, s.paramsBuf} {
		if c == nil {
			continue
		}
		err = errors.Join(err, c.Close())
	}
	return err
}

func cameraPoseFromView(view math3d.Mat4) (pos, forward math3d.Vec3) {
	// Camera forward in world space is -row(2) of an orthonormal view matrix; camera position is -R^T * t.
	if inv, ok := view.Inverse(); ok {
		pos = math3d.Vec3{X: inv[12], Y: inv[13], Z: inv[14]}
		forward = math3d.Vec3{X: -inv[8], Y: -inv[9], Z: -inv[10]}
		if forward.LenSq() > 1e-12 {
			forward = forward.Norm()
		} else {
			forward = math3d.Vec3Forward()
		}
		return pos, forward
	}
	return math3d.Vec3Zero(), math3d.Vec3Forward()
}

func encodeClusterLightGPU(dst []byte, l ClusterLight) {
	dir := l.normalizedDir()
	scale, offset := l.spotScaleOffset()
	kind := float32(0)
	if l.Kind == ClusterLightSpot {
		kind = 1
	}
	// vec4 pos_range
	clusterPutFloat32LE(dst[0:], l.Position.X)
	clusterPutFloat32LE(dst[4:], l.Position.Y)
	clusterPutFloat32LE(dst[8:], l.Position.Z)
	clusterPutFloat32LE(dst[12:], l.Range)
	// vec4 color_intensity
	clusterPutFloat32LE(dst[16:], l.Color.X)
	clusterPutFloat32LE(dst[20:], l.Color.Y)
	clusterPutFloat32LE(dst[24:], l.Color.Z)
	clusterPutFloat32LE(dst[28:], l.Intensity)
	// vec4 dir_kind
	clusterPutFloat32LE(dst[32:], dir.X)
	clusterPutFloat32LE(dst[36:], dir.Y)
	clusterPutFloat32LE(dst[40:], dir.Z)
	clusterPutFloat32LE(dst[44:], kind)
	// vec4 spot_params
	clusterPutFloat32LE(dst[48:], scale)
	clusterPutFloat32LE(dst[52:], offset)
	clusterPutFloat32LE(dst[56:], l.OuterConeCos)
	clusterPutFloat32LE(dst[60:], l.InnerConeCos)
}

func encodeClusterParams(dst *[ClusterParamsUniformSize]byte, cfg ClusterConfig, lightCount uint32, viewportW, viewportH, near, far float32, ambient math3d.Vec3, sliceDepths []float32) {
	clear(dst[:])
	// grid_info: vec4<u32>
	binary.LittleEndian.PutUint32(dst[0:], uint32(cfg.TilesX))
	binary.LittleEndian.PutUint32(dst[4:], uint32(cfg.TilesY))
	binary.LittleEndian.PutUint32(dst[8:], uint32(cfg.SlicesZ))
	binary.LittleEndian.PutUint32(dst[12:], lightCount)
	// viewport_depth: vec4<f32>
	clusterPutFloat32LE(dst[16:], viewportW)
	clusterPutFloat32LE(dst[20:], viewportH)
	clusterPutFloat32LE(dst[24:], near)
	clusterPutFloat32LE(dst[28:], far)
	// ambient: vec4<f32>
	clusterPutFloat32LE(dst[32:], ambient.X)
	clusterPutFloat32LE(dst[36:], ambient.Y)
	clusterPutFloat32LE(dst[40:], ambient.Z)
	clusterPutFloat32LE(dst[44:], 0)
	// slice_depths: array<vec4<f32>, 17> (68 float32 slots starting at byte 48)
	n := min(len(sliceDepths), MaxClusterSlicesZ+1)
	for i := range n {
		clusterPutFloat32LE(dst[48+i*4:], sliceDepths[i])
	}
}

func encodeFallbackUniform(dst *[ClusteredFallbackUniformSize]byte, ambient math3d.Vec3, lights []ClusterLight) {
	clear(dst[:])
	n := min(len(lights), MaxFallbackLights)
	binary.LittleEndian.PutUint32(dst[0:], uint32(n))
	clusterPutFloat32LE(dst[16:], ambient.X)
	clusterPutFloat32LE(dst[20:], ambient.Y)
	clusterPutFloat32LE(dst[24:], ambient.Z)
	clusterPutFloat32LE(dst[28:], 0)
	for i := range n {
		encodeClusterLightGPU(dst[32+i*ClusterLightGPUSize:], lights[i])
	}
}

func clusterPutFloat32LE(dst []byte, v float32) {
	binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
}

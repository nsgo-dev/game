package render3d

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
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

	// MaxClusterLights is the largest [ClusterConfig.MaxLights] accepted.
	MaxClusterLights = 1 << 16
	// MaxClusterIndexEntries bounds TilesX * TilesY * SlicesZ * MaxLightsPerCluster, the number of light-index slots [ClusterGrid] preallocates (64 MiB of uint32 indices).
	MaxClusterIndexEntries = 1 << 24

	// ClusterLightGPUSize is the byte size of one GPU light record (4 vec4<f32>, 16-byte aligned in both std140 and std430).
	ClusterLightGPUSize = 64
	// ClusterParamsUniformSize is the byte size of the clustered-path ClusterParams uniform block at [LightGroup] binding 1: grid_info (16) + viewport_depth (16) + 17*vec4 slice_depths (272) = 304 bytes.
	ClusterParamsUniformSize = 304
	// ClusterCellGPUSize is the byte size of one cluster cell offset/count entry (vec4<u32>, 16-byte aligned across all backends).
	ClusterCellGPUSize = 16
	// ClusteredFallbackUniformSize is the byte size of the fallback uniform block at [LightGroup] binding 1: info (16) + 16*ClusterLightGPUSize (1024) = 1040 bytes.
	ClusteredFallbackUniformSize = 16 + MaxFallbackLights*ClusterLightGPUSize

	// ClusterRangeCutoff is the radiance below which [ClusterLightFromPoint] and [ClusterLightFromSpot] treat a light with Range 0 (unbounded, the glTF default) as finished: the derived range is sqrt(Intensity * max(Color) / ClusterRangeCutoff).
	ClusterRangeCutoff = 1.0 / 256

	clusterMinStorageBindingBytes = 64 * 1024
	minClusterLightRange          = 1e-3
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

// ClusterLight is the record [ClusterGrid] bins and [ClusteredLightSystem] uploads for one local point or spot light in world space. Build it from a [PointLight] or [SpotLight] with [ClusterLightFromPoint] or [ClusterLightFromSpot], or directly with [NewPointClusterLight] and [NewSpotClusterLight].
//
// Distance falloff is [PointLightAttenuation] with Range, and spot lights also multiply by [SpotConeAttenuation], so a light shades exactly as the [PointLight] or [SpotLight] it came from. Unlike those, Range must be positive: clustering needs a bounding sphere, so a light with Range 0 (unbounded) is refused by [ClusterLight.Validate] and [ClusteredLightSystem.Update]; the converters derive a range for it from [ClusterRangeCutoff].
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

// ClusterLightFromPoint validates l and converts it to a [ClusterLightPoint]. When l.Range is 0 (unbounded), the range is derived as sqrt(Intensity * max(Color) / [ClusterRangeCutoff]), where the light's unwindowed radiance falls to the cutoff; the windowed falloff then reaches exactly zero there instead of trailing off forever. Errors wrap [ErrInvalidLight].
func ClusterLightFromPoint(l PointLight) (ClusterLight, error) {
	if err := l.Validate(); err != nil {
		return ClusterLight{}, err
	}
	return NewPointClusterLight(l.Position, l.Color, l.Intensity, clusterRange(l.Range, l.Color, l.Intensity)), nil
}

// ClusterLightFromSpot validates l and converts it to a [ClusterLightSpot] with the same cone, deriving a range for Range 0 as [ClusterLightFromPoint] does. Errors wrap [ErrInvalidLight].
func ClusterLightFromSpot(l SpotLight) (ClusterLight, error) {
	if err := l.Validate(); err != nil {
		return ClusterLight{}, err
	}
	dir, _ := spotUnitDirection(l)
	return ClusterLight{
		Kind:         ClusterLightSpot,
		Position:     l.Position,
		Range:        clusterRange(l.Range, l.Color, l.Intensity),
		Color:        l.Color,
		Intensity:    l.Intensity,
		Direction:    dir,
		InnerConeCos: float32(math.Cos(float64(l.InnerConeAngle))),
		OuterConeCos: float32(math.Cos(float64(l.OuterConeAngle))),
	}, nil
}

// clusterRange returns lightRange when it is positive, and otherwise the distance at which intensity * max(color) / d^2 falls to [ClusterRangeCutoff], at least minClusterLightRange.
func clusterRange(lightRange float32, color math3d.Vec3, intensity float32) float32 {
	if lightRange > 0 {
		return lightRange
	}
	power := float64(intensity) * float64(max(color.X, color.Y, color.Z))
	return max(float32(math.Sqrt(power/ClusterRangeCutoff)), minClusterLightRange)
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
		return fmt.Errorf("%w: range %v must be finite and positive (ClusterLightFromPoint and ClusterLightFromSpot derive one for an unbounded light)", ErrInvalidClusterLight, l.Range)
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
	denom := max(inner-outer, minSpotConeCosDelta)
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
	if c.MaxLights < 1 || c.MaxLights > MaxClusterLights {
		return fmt.Errorf("%w: MaxLights %d must be in [1, %d]", ErrInvalidClusterConfig, c.MaxLights, MaxClusterLights)
	}
	if c.MaxLightsPerCluster < 1 || c.MaxLightsPerCluster > c.MaxLights {
		return fmt.Errorf("%w: MaxLightsPerCluster %d must be in [1, MaxLights %d]", ErrInvalidClusterConfig, c.MaxLightsPerCluster, c.MaxLights)
	}
	if c.FallbackMaxLights < 1 || c.FallbackMaxLights > MaxFallbackLights {
		return fmt.Errorf("%w: FallbackMaxLights %d must be in [1, %d]", ErrInvalidClusterConfig, c.FallbackMaxLights, MaxFallbackLights)
	}
	totalClusters := uint64(c.TilesX) * uint64(c.TilesY) * uint64(c.SlicesZ)
	if totalClusters == 0 || totalClusters > 1<<20 {
		return fmt.Errorf("%w: cluster count %d out of range", ErrInvalidClusterConfig, totalClusters)
	}
	if entries := totalClusters * uint64(c.MaxLightsPerCluster); entries > MaxClusterIndexEntries {
		return fmt.Errorf("%w: %d clusters x %d lights per cluster = %d index slots exceed MaxClusterIndexEntries (%d)", ErrInvalidClusterConfig, totalClusters, c.MaxLightsPerCluster, entries, MaxClusterIndexEntries)
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
// Lights that fail [ClusterLight.Validate] or have no power are skipped and counted in CulledLights; [ClusteredLightSystem.Update] validates first and reports them as errors. Once MaxLights lights are visible, further lights are dropped in input order (TruncatedLights), and once a cell holds MaxLightsPerCluster lights it drops the rest in input order (TruncatedClusters), so put the most important lights first when a scene can exceed either cap. Steady-state calls perform zero heap allocations.
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

// SelectFallbackLights chooses up to maxLights local lights for the uniform-buffer fallback path (mobile, WebGL 2, OpenGL ES 3.0) and appends them to dst[:0].
//
// Lights whose bounding sphere lies outside the view frustum of proj * view, or that fail [ClusterLight.Validate] or have no power, are dropped first, so off-screen lights never take a slot from visible ones. The rest are ranked deterministically by luminous power, range, and distance to the camera, breaking ties by input order. This allocates a scratch slice per call; [ClusteredLightSystem.Update] reuses one.
func SelectFallbackLights(dst []ClusterLight, view, proj math3d.Mat4, lights []ClusterLight, maxLights int) []ClusterLight {
	dst = dst[:0]
	if maxLights <= 0 || len(lights) == 0 {
		return dst
	}
	scored := make([]scoredClusterLight, 0, len(lights))
	return selectFallbackLightsWithScratch(dst, &scored, view, proj, lights, maxLights)
}

// clusterLightBounds returns a world-space sphere enclosing everything l can light.
func clusterLightBounds(l ClusterLight) math3d.Sphere {
	if l.Kind != ClusterLightSpot {
		return math3d.Sphere{Center: l.Position, Radius: l.Range}
	}
	cosOuter := math3d.Clamp(l.OuterConeCos, -1, 1)
	sinOuter := float32(math.Sqrt(float64(max(1-cosOuter*cosOuter, 0))))
	c, r := spotBoundingSphere(l.Position, l.normalizedDir(), l.Range, cosOuter, sinOuter)
	return math3d.Sphere{Center: c, Radius: r}
}

func selectFallbackLightsWithScratch(dst []ClusterLight, scratch *[]scoredClusterLight, view, proj math3d.Mat4, lights []ClusterLight, maxLights int) []ClusterLight {
	dst = dst[:0]
	*scratch = (*scratch)[:0]
	if maxLights <= 0 || len(lights) == 0 {
		return dst
	}
	frustum := math3d.FrustumFromMat4(proj.Mul(view))
	cameraPos, _ := cameraPoseFromView(view)

	scored := (*scratch)[:0]
	for i := range lights {
		l := lights[i]
		if !l.isActive() || !frustum.IntersectsSphere(clusterLightBounds(l)) {
			continue
		}
		lum := 0.2126*l.Color.X + 0.7152*l.Color.Y + 0.0722*l.Color.Z
		if lum <= 0 {
			lum = max(l.Color.X, l.Color.Y, l.Color.Z)
		}
		distSq := l.Position.Sub(cameraPos).LenSq()
		dist := float32(math.Sqrt(float64(distSq)))
		// Smooth importance combining power, range coverage, and camera distance.
		score := l.Intensity * lum * (l.Range * l.Range) / ((distSq + 0.25) * (1.0 + dist/max(l.Range, minClusterLightRange)))
		scored = append(scored, scoredClusterLight{index: i, score: score})
	}
	*scratch = scored

	slices.SortFunc(scored, compareScoredClusterLights)
	keep := min(len(scored), maxLights)
	for i := range keep {
		dst = append(dst, lights[scored[i].index])
	}
	return dst
}

// compareScoredClusterLights orders by descending score, then ascending input index.
func compareScoredClusterLights(a, b scoredClusterLight) int {
	switch {
	case a.score > b.score:
		return -1
	case a.score < b.score:
		return 1
	}
	return a.index - b.index
}

// EvaluateLightAttenuation returns the unit direction from worldPos toward light and the light's attenuation times its Intensity: [PointLightAttenuation] with Range, times [SpotConeAttenuation] for a [ClusterLightSpot]. It returns zeros for a light with no power or an invalid record, and when worldPos coincides with the light.
func EvaluateLightAttenuation(light ClusterLight, worldPos math3d.Vec3) (dirToLight math3d.Vec3, attenuation float32) {
	if !light.isActive() {
		return math3d.Vec3{}, 0
	}
	toLight := light.Position.Sub(worldPos)
	distSq := toLight.LenSq()
	if distSq <= minLightDirLenSq {
		return math3d.Vec3{}, 0
	}
	dirToLight = toLight.Scale(float32(1.0 / math.Sqrt(float64(distSq))))
	atten := PointLightAttenuation(distSq, light.Range)
	if light.Kind == ClusterLightSpot {
		scale, offset := light.spotScaleOffset()
		atten *= SpotConeAttenuation(light.normalizedDir(), dirToLight, scale, offset)
	}
	return dirToLight, atten * light.Intensity
}

// ShadeClusteredPoint is [ShadePoint] for rig plus every light in lights, shaded with the same Cook-Torrance BRDF ([EvaluatePBR]). It is the brute-force reference for shade_pbr_clustered in [ClusteredBindingsWGSL] and [ClusteredFallbackBindingsWGSL] when no light is culled or truncated; call [LinearToSRGB] on the result to compare it with what [ClusteredLitWGSL] writes.
func ShadeClusteredPoint(worldPos, normal, viewDir math3d.Vec3, mat PBRMaterial, rig LightRig, lights []ClusterLight) math3d.Vec3 {
	lo := ShadePoint(worldPos, normal, viewDir, mat, rig)
	n, v, ok := clusterUnitNormalView(normal, viewDir)
	if !ok {
		return lo
	}
	for i := range lights {
		lo = lo.Add(clusterLightRadiance(lights[i], worldPos, n, v, mat))
	}
	return lo
}

// ShadeClusteredGridPoint is [ShadeClusteredPoint] restricted to the lights grid assigned, on its last [ClusterGrid.AssignLights], to the cluster that holds worldPos in a viewportW x viewportH viewport, looked up exactly as shade_pbr_clustered in [ClusteredBindingsWGSL] looks it up.
func ShadeClusteredGridPoint(worldPos, normal, viewDir math3d.Vec3, mat PBRMaterial, rig LightRig, grid *ClusterGrid, view, proj math3d.Mat4, viewportW, viewportH float32) math3d.Vec3 {
	lo := ShadePoint(worldPos, normal, viewDir, mat, rig)
	n, v, ok := clusterUnitNormalView(normal, viewDir)
	if grid == nil || !ok {
		return lo
	}
	viewPos := view.TransformPoint(worldPos)
	clip := proj.MulVec4(viewPos.Vec4(1))
	if clip.W <= 1e-6 {
		return lo
	}
	fragX := (clip.X/clip.W + 1.0) * 0.5 * viewportW
	fragY := (1.0 - clip.Y/clip.W) * 0.5 * viewportH
	tx := clusterClampInt(int(max(fragX, 0)/max(viewportW, 1)*float32(grid.cfg.TilesX)), 0, grid.cfg.TilesX-1)
	ty := clusterClampInt(int(max(fragY, 0)/max(viewportH, 1)*float32(grid.cfg.TilesY)), 0, grid.cfg.TilesY-1)
	sz := grid.SliceForDepth(-viewPos.Z)
	for _, lightIdx := range grid.ClusterLightIndices(tx, ty, sz) {
		lo = lo.Add(clusterLightRadiance(grid.activeLights[lightIdx], worldPos, n, v, mat))
	}
	return lo
}

func clusterUnitNormalView(normal, viewDir math3d.Vec3) (n, v math3d.Vec3, ok bool) {
	nLenSq := normal.LenSq()
	vLenSq := viewDir.LenSq()
	if nLenSq <= minLightDirLenSq || vLenSq <= minLightDirLenSq {
		return math3d.Vec3{}, math3d.Vec3{}, false
	}
	return normal.Scale(float32(1.0 / math.Sqrt(float64(nLenSq)))), viewDir.Scale(float32(1.0 / math.Sqrt(float64(vLenSq)))), true
}

// clusterLightRadiance is the outgoing radiance one local light adds at worldPos for unit n and v, matching cluster_light_radiance in the clustered WGSL.
func clusterLightRadiance(l ClusterLight, worldPos, n, v math3d.Vec3, mat PBRMaterial) math3d.Vec3 {
	lDir, atten := EvaluateLightAttenuation(l, worldPos)
	ndotl := n.Dot(lDir)
	if atten <= 0 || ndotl <= 0 {
		return math3d.Vec3{}
	}
	brdf := EvaluatePBR(n, v, lDir, mat)
	rad := l.Color.Scale(atten * ndotl)
	return math3d.Vec3{X: brdf.X * rad.X, Y: brdf.Y * rad.Y, Z: brdf.Z * rad.Z}
}

// DeviceSupportsClusteredLighting reports whether dev exposes the fragment storage-buffer capability and binding size needed for the clustered forward shading path. Because cluster assignment runs on the CPU, compute shaders are not required (so Apple Metal devices exposing StorageBuffers without Compute use the full clustered path). Devices without storage buffers (such as WebGL 2, OpenGL ES 3.0, Linux GtkGLArea, or headless tests under nsgotest.NoGPUCompute) fall back to the uniform-buffer path automatically.
func DeviceSupportsClusteredLighting(dev *gpu.Device) bool {
	if dev == nil {
		return false
	}
	caps := dev.Capabilities()
	return caps.Features.StorageBuffers &&
		caps.Limits.MaxStorageBufferBindingSize >= clusterMinStorageBindingBytes
}

// ClusteredBindingsWGSL is [LightWGSL] (the [LightRig] block at [LightGroup] binding 0 and its PBR helpers) plus the clustered forward bindings at bindings 1 to 4 of the same group and this helper:
//
//   - shade_pbr_clustered(world_pos, normal, view_dir, base_color, metallic, roughness, frag_pos) -> vec3<f32>
//
// It returns shade_pbr for the rig plus every local light binned into the cluster that holds the fragment, each shaded with pbr_brdf, point_light_attenuation and spot_cone_attenuation, so it matches [ShadeClusteredGridPoint]. frag_pos is the fragment's @builtin(position). Prepend [CameraWGSL], which supplies camera.view for the depth slice, and pair it with a [ClusteredLightSystem] in clustered mode; [ClusteredFallbackBindingsWGSL] declares the same helper for the fallback path.
//
// Depth slicing is a 6-step binary search over the uniform split-depth table rather than log2 and exp, because NSGo v0.3.0's software GPU VM has no log, log2, exp or exp2 builtin and silently evaluates them to zero (internal/driver/gpu_software.go, evalCall) (nsgo-dev/nsgo#2334).
const ClusteredBindingsWGSL = LightWGSL + clusteredStorageWGSL

// ClusteredFallbackBindingsWGSL is [LightWGSL] plus the fallback uniform block at [LightGroup] binding 1 and shade_pbr_clustered with the same signature as in [ClusteredBindingsWGSL], using no storage buffers or compute, for WebGL 2 and OpenGL ES 3.0 targets. frag_pos is ignored: every uploaded light is evaluated for every fragment.
const ClusteredFallbackBindingsWGSL = LightWGSL + clusteredFallbackWGSL

// clusterLightRadianceWGSL declares the light record both paths upload and cluster_light_radiance, the outgoing radiance one local light adds, matching clusterLightRadiance.
const clusterLightRadianceWGSL = `
struct ClusterLightGPU {
	pos_range: vec4<f32>,
	color_intensity: vec4<f32>,
	dir_kind: vec4<f32>,
	spot_params: vec4<f32>,
}

fn cluster_light_radiance(light: ClusterLightGPU, world_pos: vec3<f32>, n: vec3<f32>, v: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32) -> vec3<f32> {
	let intensity = light.color_intensity.w;
	if (light.pos_range.w <= 0.0 || intensity <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let to_light = light.pos_range.xyz - world_pos;
	let dist_sq = dot(to_light, to_light);
	if (dist_sq <= 1e-12) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	let l = to_light / sqrt(dist_sq);
	var atten = point_light_attenuation(dist_sq, light.pos_range.w);
	if (light.dir_kind.w > 0.5) {
		atten = atten * spot_cone_attenuation(light.dir_kind.xyz, l, light.spot_params.x, light.spot_params.y);
	}
	let ndotl = dot(n, l);
	if (atten <= 0.0 || ndotl <= 0.0) {
		return vec3<f32>(0.0, 0.0, 0.0);
	}
	return pbr_brdf(n, v, l, base_color, metallic, roughness) * (light.color_intensity.rgb * (intensity * atten * ndotl));
}
`

// clusteredStorageWGSL is the storage-buffer half of [ClusteredBindingsWGSL], after [LightWGSL].
const clusteredStorageWGSL = clusterLightRadianceWGSL + `
struct ClusterParams {
	grid_info: vec4<u32>,
	viewport_depth: vec4<f32>,
	slice_depths: array<vec4<f32>, 17>,
}

@group(1) @binding(1) var<uniform> cluster_params: ClusterParams;
@group(1) @binding(2) var<storage, read> cluster_lights: array<ClusterLightGPU>;
@group(1) @binding(3) var<storage, read> cluster_cells: array<vec4<u32>>;
@group(1) @binding(4) var<storage, read> cluster_indices: array<u32>;

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

fn shade_pbr_clustered(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32, frag_pos: vec4<f32>) -> vec3<f32> {
	var lo = shade_pbr(world_pos, normal, view_dir, base_color, metallic, roughness);
	let tiles_x = cluster_params.grid_info.x;
	let tiles_y = cluster_params.grid_info.y;
	if (tiles_x == 0u || tiles_y == 0u) {
		return lo;
	}
	let n = normalize(normal);
	let v = normalize(view_dir);
	let vp_w = max(cluster_params.viewport_depth.x, 1.0);
	let vp_h = max(cluster_params.viewport_depth.y, 1.0);
	let tx = min(u32(max(frag_pos.x, 0.0) / vp_w * f32(tiles_x)), tiles_x - 1u);
	let ty = min(u32(max(frag_pos.y, 0.0) / vp_h * f32(tiles_y)), tiles_y - 1u);
	let view_pos = camera.view * vec4<f32>(world_pos, 1.0);
	let slice_z = cluster_find_slice(-view_pos.z);
	let cell = cluster_cells[(slice_z * tiles_y + ty) * tiles_x + tx];
	for (var i: u32 = 0u; i < cell.y; i = i + 1u) {
		let light_idx = cluster_indices[cell.x + i];
		lo = lo + cluster_light_radiance(cluster_lights[light_idx], world_pos, n, v, base_color, metallic, roughness);
	}
	return lo;
}
`

// clusteredFallbackWGSL is the uniform-buffer half of [ClusteredFallbackBindingsWGSL], after [LightWGSL].
const clusteredFallbackWGSL = clusterLightRadianceWGSL + `
struct FallbackLightUniform {
	info: vec4<u32>,
	local: array<ClusterLightGPU, 16>,
}

@group(1) @binding(1) var<uniform> fallback_lights: FallbackLightUniform;

fn shade_pbr_clustered(world_pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, base_color: vec3<f32>, metallic: f32, roughness: f32, frag_pos: vec4<f32>) -> vec3<f32> {
	var lo = shade_pbr(world_pos, normal, view_dir, base_color, metallic, roughness);
	let n = normalize(normal);
	let v = normalize(view_dir);
	let count = min(fallback_lights.info.x, 16u);
	for (var i: u32 = 0u; i < count; i = i + 1u) {
		lo = lo + cluster_light_radiance(fallback_lights.local[i], world_pos, n, v, base_color, metallic, roughness);
	}
	return lo;
}
`

// clusteredLitFragmentWGSL is the fragment stage of [ClusteredLitWGSL] and [ClusteredFallbackWGSL]: [LitPBRWGSL]'s, with shade_pbr_clustered in place of shade_pbr.
const clusteredLitFragmentWGSL = `
@fragment
fn fs_main(in: PBRVertexOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let lit = shade_pbr_clustered(
		in.world_pos,
		in.world_normal,
		view_dir,
		pbr_draw.base_color.rgb,
		pbr_draw.pbr_params.x,
		pbr_draw.pbr_params.y,
		in.clip,
	) + pbr_draw.emissive.rgb;
	return vec4<f32>(linear_to_srgb(lit), pbr_draw.base_color.a);
}
`

// ClusteredLitWGSL is [LitPBRWGSL] with clustered local lights: the same metallic-roughness module on [CameraWGSL], [MeshVertexLayout] and a per-draw [PBRDrawUniform] at [PBRDrawGroup], whose fragment stage shades with shade_pbr_clustered from [ClusteredBindingsWGSL] and writes sRGB. [NewClusteredLitPBRPipeline] creates a pipeline from it.
const ClusteredLitWGSL = litPBRPreludeWGSL + clusteredStorageWGSL + clusteredLitFragmentWGSL

// ClusteredFallbackWGSL is [ClusteredLitWGSL] built on [ClusteredFallbackBindingsWGSL], for devices without storage buffers or compute.
const ClusteredFallbackWGSL = litPBRPreludeWGSL + clusteredFallbackWGSL + clusteredLitFragmentWGSL

// NewClusteredLitPBRPipeline compiles sys's ready-made shader ([ClusteredLitWGSL] or [ClusteredFallbackWGSL]) and creates the same back-face-culled pipeline as [NewLitPBRPipeline], with sys's layout at [LightGroup] in place of a [GPULightBuffer]'s. Pass depthFormat = [gpu.FormatUndefined] when rendering without a depth attachment.
func NewClusteredLitPBRPipeline(cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout, sys *ClusteredLightSystem, drawLayout *gpu.BindGroupLayout, depthFormat gpu.TextureFormat) (*gpu.Pipeline, error) {
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	if sys == nil {
		return nil, fmt.Errorf("%w: nil ClusteredLightSystem", ErrInvalidClusterConfig)
	}
	layouts := make([]*gpu.BindGroupLayout, PBRDrawGroup+1)
	layouts[CameraGroup] = camLayout
	layouts[LightGroup] = sys.Layout()
	layouts[PBRDrawGroup] = drawLayout
	return newLitPBRPipeline(cx, dev, "render3d.clustered_lit_pbr", sys.WGSL(), layouts, MeshVertexLayout(), dev.SurfaceFormat(), depthFormat)
}

// ClusteredLightSystem owns the whole [LightGroup] bind group for clustered forward shading: the [LightRig] uniform [LightWGSL] reads at binding 0 (ambient, directional, and the rig's own point and spot lights) and the local-light buffers at bindings 1 to 4. On devices without storage buffers or compute (WebGL 2, OpenGL ES 3.0, nsgotest.NoGPUCompute) it falls back to one uniform buffer at binding 1 holding the [ClusterConfig.FallbackMaxLights] most important visible lights.
//
// Create it once the device is ready, call [ClusteredLightSystem.Update] or [ClusteredLightSystem.UpdateCamera] each frame, and draw with [NewClusteredLitPBRPipeline] or a custom shader that prepends [CameraWGSL] and [ClusteredLightSystem.BindingsWGSL], binding the system with [ClusteredLightSystem.Bind] in place of a [GPULightBuffer].
type ClusteredLightSystem struct {
	cfg      ClusterConfig
	fallback bool
	grid     *ClusterGrid

	// GPU resources (owned by the core.Scope passed to NewClusteredLightSystem).
	layout      *gpu.BindGroupLayout
	group       *gpu.BindGroup
	rigBuf      *gpu.Buffer // binding 0: LightRig uniform
	paramsBuf   *gpu.Buffer // binding 1: clustered ClusterParams uniform, or fallback FallbackLightUniform
	lightsBuf   *gpu.Buffer // clustered only, binding 2: storage array<ClusterLightGPU>
	cellsBuf    *gpu.Buffer // clustered only, binding 3: storage array<vec4<u32>>
	indicesBuf  *gpu.Buffer // clustered only, binding 4: storage array<u32>
	fallbackDst []ClusterLight
	fallbackTmp []scoredClusterLight
	lastStats   ClusterStats

	// Reusable CPU upload scratch buffers (zero steady-state allocations).
	rigBytes     [LightUniformSize]byte
	paramsBytes  [ClusterParamsUniformSize]byte
	fallbackData [ClusteredFallbackUniformSize]byte
	lightsBytes  []byte
	cellsBytes   []byte
	indicesBytes []byte
}

// NewClusteredLightSystem validates cfg, inspects dev's GPU capabilities, and allocates either the storage-buffer clustered lighting path or the uniform-buffer fallback path scoped to cx. In clustered mode it returns an error wrapping [ErrInvalidClusterConfig] when the light or index storage buffer cfg needs exceeds dev's MaxStorageBufferBindingSize.
func NewClusteredLightSystem(cx *core.Scope, dev *gpu.Device, cfg ClusterConfig) (*ClusteredLightSystem, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, gpu.Error{Code: gpu.ErrDeviceLost, Message: "render3d: nil device"}
	}
	cfg = cfg.withDefaults()

	label := cfg.Label
	if label == "" {
		label = "render3d.clustered"
	}
	if cfg.ForceFallback || !DeviceSupportsClusteredLighting(dev) {
		return newClusteredSystem(cx, dev, cfg, label, true)
	}
	return newClusteredSystem(cx, dev, cfg, label, false)
}

func newClusteredSystem(cx *core.Scope, dev *gpu.Device, cfg ClusterConfig, label string, fallback bool) (*ClusteredLightSystem, error) {
	s := &ClusteredLightSystem{cfg: cfg, fallback: fallback}
	var lightsSize, cellsSize, indicesSize uint64
	if fallback {
		s.fallbackDst = make([]ClusterLight, 0, cfg.FallbackMaxLights)
		s.fallbackTmp = make([]scoredClusterLight, 0, cfg.MaxLights)
	} else {
		grid, err := NewClusterGrid(cfg)
		if err != nil {
			return nil, err
		}
		s.grid = grid
		numClusters := uint64(cfg.ClusterCount())
		lightsSize = uint64(cfg.MaxLights) * ClusterLightGPUSize
		cellsSize = numClusters * ClusterCellGPUSize
		indicesSize = numClusters * uint64(cfg.MaxLightsPerCluster) * 4
		if maxBind := dev.Capabilities().Limits.MaxStorageBufferBindingSize; maxBind > 0 {
			for _, b := range []struct {
				name string
				size uint64
			}{{"lights", lightsSize}, {"cells", cellsSize}, {"indices", indicesSize}} {
				if b.size > maxBind {
					return nil, fmt.Errorf("%w: %s storage buffer of %d bytes exceeds the device's MaxStorageBufferBindingSize (%d); lower MaxLights, MaxLightsPerCluster or the grid size", ErrInvalidClusterConfig, b.name, b.size, maxBind)
				}
			}
		}
	}

	var toClose []interface{ Close() error }
	fail := func(err error) (*ClusteredLightSystem, error) {
		for i := len(toClose) - 1; i >= 0; i-- {
			_ = toClose[i].Close()
		}
		return nil, err
	}
	newBuffer := func(name string, size uint64, usage gpu.BufferUsage) (*gpu.Buffer, error) {
		buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
			Label:    label + " " + name,
			Size:     size,
			Usage:    usage | gpu.UsageCopyDst,
			Retained: cfg.Retained,
		})
		if err == nil {
			toClose = append(toClose, buf)
		}
		return buf, err
	}

	var err error
	if s.rigBuf, err = newBuffer("rig", LightUniformSize, gpu.UsageUniform); err != nil {
		return fail(err)
	}
	paramsSize := uint64(ClusterParamsUniformSize)
	if fallback {
		paramsSize = ClusteredFallbackUniformSize
	}
	if s.paramsBuf, err = newBuffer("params", paramsSize, gpu.UsageUniform); err != nil {
		return fail(err)
	}
	layoutEntries := []gpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gpu.StageVertex | gpu.StageFragment, Type: gpu.BindingUniform},
		{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingUniform},
	}
	groupEntries := []gpu.BindGroupEntry{
		{Binding: 0, Buffer: s.rigBuf, Size: LightUniformSize},
		{Binding: 1, Buffer: s.paramsBuf, Size: paramsSize},
	}
	if !fallback {
		if s.lightsBuf, err = newBuffer("lights", lightsSize, gpu.UsageStorage); err != nil {
			return fail(err)
		}
		if s.cellsBuf, err = newBuffer("cells", cellsSize, gpu.UsageStorage); err != nil {
			return fail(err)
		}
		if s.indicesBuf, err = newBuffer("indices", indicesSize, gpu.UsageStorage); err != nil {
			return fail(err)
		}
		layoutEntries = append(layoutEntries,
			gpu.BindGroupLayoutEntry{Binding: 2, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
			gpu.BindGroupLayoutEntry{Binding: 3, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
			gpu.BindGroupLayoutEntry{Binding: 4, Visibility: gpu.StageFragment, Type: gpu.BindingStorageRead},
		)
		groupEntries = append(groupEntries,
			gpu.BindGroupEntry{Binding: 2, Buffer: s.lightsBuf, Size: lightsSize},
			gpu.BindGroupEntry{Binding: 3, Buffer: s.cellsBuf, Size: cellsSize},
			gpu.BindGroupEntry{Binding: 4, Buffer: s.indicesBuf, Size: indicesSize},
		)
		s.lightsBytes = make([]byte, lightsSize)
		s.cellsBytes = make([]byte, cellsSize)
		s.indicesBytes = make([]byte, indicesSize)
	}

	if s.layout, err = dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{Label: label + " layout", Entries: layoutEntries}); err != nil {
		return fail(err)
	}
	toClose = append(toClose, s.layout)
	if s.group, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{Label: label + " group", Layout: s.layout, Entries: groupEntries}); err != nil {
		return fail(err)
	}
	return s, nil
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

// Layout returns the bind group layout a pipeline lists at [LightGroup].
func (s *ClusteredLightSystem) Layout() *gpu.BindGroupLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

// BindGroup returns the bind group a render pass sets at [LightGroup].
func (s *ClusteredLightSystem) BindGroup() *gpu.BindGroup {
	if s == nil {
		return nil
	}
	return s.group
}

// WGSL returns the ready-made shader matching s's active path ([ClusteredLitWGSL] or [ClusteredFallbackWGSL]).
func (s *ClusteredLightSystem) WGSL() string {
	if s.IsFallback() {
		return ClusteredFallbackWGSL
	}
	return ClusteredLitWGSL
}

// BindingsWGSL returns the [LightGroup] declarations and shade_pbr_clustered helper matching s's active path ([ClusteredBindingsWGSL] or [ClusteredFallbackBindingsWGSL]).
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
	for _, b := range []*gpu.Buffer{s.rigBuf, s.paramsBuf, s.lightsBuf, s.cellsBuf, s.indicesBuf} {
		if b != nil {
			total += b.Size()
		}
	}
	return total
}

// Valid reports whether s's GPU resources are open and belong to a live device.
func (s *ClusteredLightSystem) Valid() bool {
	if s == nil || !s.rigBuf.Valid() || !s.paramsBuf.Valid() || !s.layout.Valid() || !s.group.Valid() {
		return false
	}
	if s.fallback {
		return true
	}
	return s.lightsBuf.Valid() && s.cellsBuf.Valid() && s.indicesBuf.Valid()
}

// Bind sets s's bind group at [LightGroup] on pass, like [GPULightBuffer.Bind]. It records nothing when s is not [ClusteredLightSystem.Valid].
func (s *ClusteredLightSystem) Bind(pass *gpu.RenderPass) {
	if !s.Valid() || pass == nil {
		return
	}
	pass.SetBindGroup(LightGroup, s.group)
}

// Update validates rig and lights, assigns lights for the frame, and uploads the rig and the resulting light buffers to dev. viewportW and viewportH are the size of the rendered area the fragment shader's @builtin(position) covers, starting at its origin: under a [DynamicResolutionTarget] pass [DynamicResolutionTarget.ScaledSize], not the native size.
//
// It returns an error wrapping [ErrInvalidLight] for a malformed rig, or [ErrInvalidClusterLight] for a light that fails [ClusterLight.Validate] (including Range 0; see [ClusterLightFromPoint]), and uploads nothing in that case. Lights with zero intensity or color are culled.
//
// In clustered mode, lights are binned into view-space frustum clusters via [ClusterGrid.AssignLights]. In fallback mode, the top [ClusterConfig.FallbackMaxLights] visible lights are selected as [SelectFallbackLights] does; CulledLights counts lights outside the frustum or without power and TruncatedLights those dropped by the cap. Steady-state calls allocate nothing beyond the copies gpu.Device.WriteBuffer queues.
func (s *ClusteredLightSystem) Update(dev *gpu.Device, view, proj math3d.Mat4, near, far, viewportW, viewportH float32, rig LightRig, lights []ClusterLight) (ClusterStats, error) {
	if s == nil {
		return ClusterStats{}, nil
	}
	if err := rig.Validate(); err != nil {
		return ClusterStats{}, err
	}
	for i := range lights {
		if err := lights[i].Validate(); err != nil {
			return ClusterStats{}, fmt.Errorf("lights[%d]: %w", i, err)
		}
	}
	upload := dev != nil && s.Valid()
	if upload {
		u := packLightUniforms(rig)
		u.Encode(s.rigBytes[:])
		dev.WriteBuffer(s.rigBuf, 0, s.rigBytes[:])
	}

	if s.fallback {
		s.fallbackDst = selectFallbackLightsWithScratch(s.fallbackDst, &s.fallbackTmp, view, proj, lights, s.cfg.FallbackMaxLights)
		stats := ClusterStats{
			InputLights:     len(lights),
			VisibleLights:   len(s.fallbackDst),
			CulledLights:    len(lights) - len(s.fallbackTmp),
			TruncatedLights: len(s.fallbackTmp) - len(s.fallbackDst),
		}
		s.lastStats = stats
		if upload {
			encodeFallbackUniform(&s.fallbackData, s.fallbackDst)
			dev.WriteBuffer(s.paramsBuf, 0, s.fallbackData[:])
		}
		return stats, nil
	}

	stats := s.grid.AssignLights(view, proj, near, far, lights)
	s.lastStats = stats
	if !upload {
		return stats, nil
	}

	encodeClusterParams(&s.paramsBytes, s.cfg, uint32(len(s.grid.activeLights)), viewportW, viewportH, near, far, s.grid.SliceDepths())
	dev.WriteBuffer(s.paramsBuf, 0, s.paramsBytes[:])

	active := s.grid.activeLights
	if len(active) > 0 {
		for i := range active {
			encodeClusterLightGPU(s.lightsBytes[i*ClusterLightGPUSize:], active[i])
		}
		dev.WriteBuffer(s.lightsBuf, 0, s.lightsBytes[:len(active)*ClusterLightGPUSize])
	}

	// Every cell holds at most MaxLightsPerCluster indices, so the packed list always fits the indices buffer.
	for i, oc := range s.grid.offsets {
		base := i * ClusterCellGPUSize
		binary.LittleEndian.PutUint32(s.cellsBytes[base+0:], oc.Offset)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+4:], oc.Count)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+8:], 0)
		binary.LittleEndian.PutUint32(s.cellsBytes[base+12:], 0)
	}
	dev.WriteBuffer(s.cellsBuf, 0, s.cellsBytes)

	if indices := s.grid.lightIndices; len(indices) > 0 {
		for i, idx := range indices {
			binary.LittleEndian.PutUint32(s.indicesBytes[i*4:], idx)
		}
		dev.WriteBuffer(s.indicesBuf, 0, s.indicesBytes[:len(indices)*4])
	}
	return stats, nil
}

// UpdateCamera is [ClusteredLightSystem.Update] with view, projection, near, and far taken from cam.
func (s *ClusteredLightSystem) UpdateCamera(dev *gpu.Device, cam *scene3d.Camera, viewportW, viewportH float32, rig LightRig, lights []ClusterLight) (ClusterStats, error) {
	if cam == nil {
		return ClusterStats{}, nil
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
	return s.Update(dev, cam.ViewMatrix(), cam.ProjMatrix(), near, far, viewportW, viewportH, rig, lights)
}

// Close releases all GPU resources owned by s now instead of when their scope is disposed. Safe to call on nil or more than once.
func (s *ClusteredLightSystem) Close() error {
	if s == nil {
		return nil
	}
	var err error
	for _, c := range []interface{ Close() error }{s.group, s.layout, s.indicesBuf, s.cellsBuf, s.lightsBuf, s.paramsBuf, s.rigBuf} {
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

func encodeClusterParams(dst *[ClusterParamsUniformSize]byte, cfg ClusterConfig, lightCount uint32, viewportW, viewportH, near, far float32, sliceDepths []float32) {
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
	// slice_depths: array<vec4<f32>, 17> (68 float32 slots starting at byte 32)
	n := min(len(sliceDepths), MaxClusterSlicesZ+1)
	for i := range n {
		clusterPutFloat32LE(dst[32+i*4:], sliceDepths[i])
	}
}

func encodeFallbackUniform(dst *[ClusteredFallbackUniformSize]byte, lights []ClusterLight) {
	clear(dst[:])
	n := min(len(lights), MaxFallbackLights)
	binary.LittleEndian.PutUint32(dst[0:], uint32(n))
	for i := range n {
		encodeClusterLightGPU(dst[16+i*ClusterLightGPUSize:], lights[i])
	}
}

func clusterPutFloat32LE(dst []byte, v float32) {
	binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
}

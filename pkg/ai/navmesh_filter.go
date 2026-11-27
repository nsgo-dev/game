package ai

import (
	"maps"
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// NavArea identifies the surface or terrain category of a navigation polygon
// (for example road, grass, mud, water, or a door threshold).
// Area 0 is NavAreaDefault. Games may assign any custom uint8 ID up to 255.
type NavArea uint8

const (
	// NavAreaDefault is the default walkable area assigned by BuildNavMesh.
	NavAreaDefault NavArea = 0
	// NavAreaRoad marks fast-travel surfaces such as roads or paved paths.
	NavAreaRoad NavArea = 1
	// NavAreaGrass marks ordinary open ground.
	NavAreaGrass NavArea = 2
	// NavAreaMud marks slow or difficult terrain.
	NavAreaMud NavArea = 3
	// NavAreaWater marks water surfaces typically paired with NavFlagSwim.
	NavAreaWater NavArea = 4
	// NavAreaDoor marks dynamic doorway or gate polygons.
	NavAreaDoor NavArea = 5
	// NavAreaHazard marks hazardous ground such as traps, fire, or lava.
	NavAreaHazard NavArea = 6
	// NavAreaJump marks jump or drop-off off-mesh links and landing surfaces.
	NavAreaJump NavArea = 7
	// NavAreaClimb marks climbable off-mesh links such as ladders and ledges.
	NavAreaClimb NavArea = 8
	// NavAreaTeleport marks teleporter or portal off-mesh links.
	NavAreaTeleport NavArea = 9
)

// NavFlags is a bitmask of runtime traversal capabilities and states on a
// navigation polygon (such as walk, swim, door, jump, climb, teleport, or disabled).
type NavFlags uint32

const (
	// NavFlagWalk marks standard ground-walkable polygons (default for BuildNavMesh).
	NavFlagWalk NavFlags = 1 << 0
	// NavFlagSwim marks water polygons traversable by swimming agents.
	NavFlagSwim NavFlags = 1 << 1
	// NavFlagDoor marks doorway or gate polygons that may open or close at runtime.
	NavFlagDoor NavFlags = 1 << 2
	// NavFlagJump marks jump or drop-off traversal polygons and off-mesh links.
	NavFlagJump NavFlags = 1 << 3
	// NavFlagClimb marks climbable off-mesh links such as ladders.
	NavFlagClimb NavFlags = 1 << 4
	// NavFlagTeleport marks teleporter or portal off-mesh links.
	NavFlagTeleport NavFlags = 1 << 5
	// NavFlagDisabled marks polygons or links disabled at runtime (excluded by DefaultNavFilter).
	NavFlagDisabled NavFlags = 1 << 31
	// NavFlagAll matches any non-zero flag bitmask.
	NavFlagAll NavFlags = ^NavFlags(0)
)

// NavPolygonInput describes a single convex 2D polygon together with its
// initial area ID and runtime traversal flags for BuildNavMeshWithAreas.
// A zero Flags value defaults to NavFlagWalk.
type NavPolygonInput struct {
	Vertices []core.Point `yaml:"vertices"`
	Area     NavArea      `yaml:"area,omitempty"`
	Flags    NavFlags     `yaml:"flags,omitempty"`
}

// NavFilter controls which navigation polygons a query may traverse and how
// each polygon's area scales traversal cost.
//
// A polygon with (area, flags) is traversable when:
//   - area is not marked excluded in ExcludedAreas,
//   - flags has no bits in common with ExcludeFlags (flags & ExcludeFlags == 0),
//   - when IncludeFlags is non-zero, flags shares at least one bit with IncludeFlags (flags & IncludeFlags != 0),
//   - its effective area cost multiplier is positive and finite.
//
// Areas omitted from AreaCosts default to a cost multiplier of 1.0.
// Use DefaultNavFilter for a filter matching BuildNavMesh defaults
// (IncludeFlags: NavFlagAll, ExcludeFlags: NavFlagDisabled, unit cost for all areas).
type NavFilter struct {
	// IncludeFlags requires a polygon to have at least one matching flag bit
	// when non-zero. When zero, no inclusion bit is required.
	IncludeFlags NavFlags

	// ExcludeFlags forbids any polygon with any matching flag bit set.
	ExcludeFlags NavFlags

	// AreaCosts overrides the traversal cost multiplier per NavArea.
	// Omitted areas default to 1.0. Values <= 0, NaN, or ±Inf forbid traversal
	// of that area.
	AreaCosts map[NavArea]float64

	// ExcludedAreas optionally forbids specific NavArea IDs regardless of flags.
	ExcludedAreas map[NavArea]bool
}

// DefaultNavFilter returns the default query filter matching BuildNavMesh defaults:
// every non-zero, non-disabled polygon is traversable at unit cost (1.0).
func DefaultNavFilter() NavFilter {
	return NavFilter{
		IncludeFlags: NavFlagAll,
		ExcludeFlags: NavFlagDisabled,
	}
}

// Allows reports whether a polygon with the given area and flags passes the filter.
func (f NavFilter) Allows(area NavArea, flags NavFlags) bool {
	if f.ExcludedAreas[area] {
		return false
	}
	if f.ExcludeFlags != 0 && (flags&f.ExcludeFlags) != 0 {
		return false
	}
	if f.IncludeFlags != 0 && (flags&f.IncludeFlags) == 0 {
		return false
	}
	if c, ok := f.AreaCosts[area]; ok {
		return c > 0 && !math.IsNaN(c) && !math.IsInf(c, 0)
	}
	return true
}

// AreaCost returns the traversal cost multiplier for area.
// Omitted areas return 1.0. Excluded areas or areas with non-positive, NaN, or
// infinite configured costs return +Inf.
func (f NavFilter) AreaCost(area NavArea) float64 {
	if f.ExcludedAreas[area] {
		return math.Inf(1)
	}
	if c, ok := f.AreaCosts[area]; ok {
		if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
			return math.Inf(1)
		}
		return c
	}
	return 1.0
}

// SetAreaCost sets the traversal cost multiplier for area on f.
func (f *NavFilter) SetAreaCost(area NavArea, cost float64) {
	if f == nil {
		return
	}
	if f.AreaCosts == nil {
		f.AreaCosts = make(map[NavArea]float64)
	}
	f.AreaCosts[area] = cost
}

// ExcludeArea marks area as non-traversable on f regardless of polygon flags.
func (f *NavFilter) ExcludeArea(area NavArea) {
	if f == nil {
		return
	}
	if f.ExcludedAreas == nil {
		f.ExcludedAreas = make(map[NavArea]bool)
	}
	f.ExcludedAreas[area] = true
}

// IncludeArea removes area from f's ExcludedAreas set.
func (f *NavFilter) IncludeArea(area NavArea) {
	if f == nil || f.ExcludedAreas == nil {
		return
	}
	delete(f.ExcludedAreas, area)
}

// Clone returns an independent deep copy of f.
func (f NavFilter) Clone() NavFilter {
	out := NavFilter{
		IncludeFlags: f.IncludeFlags,
		ExcludeFlags: f.ExcludeFlags,
	}
	if f.AreaCosts != nil {
		out.AreaCosts = maps.Clone(f.AreaCosts)
	}
	if f.ExcludedAreas != nil {
		out.ExcludedAreas = maps.Clone(f.ExcludedAreas)
	}
	return out
}

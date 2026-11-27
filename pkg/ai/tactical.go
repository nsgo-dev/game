package ai

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/nsgo/core"
)

// QueryOrigin selects a reference point from a TacticalContext.
type QueryOrigin uint8

const (
	// OriginDefault uses the generator's or test's natural default origin
	// (OriginQuerier for CoverPoint/Grid/NavMesh/Distance/PathDistance; OriginTarget for Ring/LOS/Angle).
	OriginDefault QueryOrigin = iota
	// OriginQuerier uses TacticalContext.QuerierPos.
	OriginQuerier
	// OriginTarget uses TacticalContext.TargetPos.
	OriginTarget
	// OriginCustom uses the generator's or test's explicit CustomPos / CustomCenter / CustomTarget field.
	OriginCustom
)

func resolveOrigin(origin, def QueryOrigin, custom math2d.Vec2, ctx TacticalContext) math2d.Vec2 {
	if origin == OriginDefault {
		origin = def
	}
	switch origin {
	case OriginTarget:
		return ctx.TargetPos
	case OriginCustom:
		return custom
	default:
		return ctx.QuerierPos
	}
}

// FloatRange represents an optional inclusive [Min, Max] filter interval for continuous tactical tests.
type FloatRange struct {
	Min    float64
	Max    float64
	HasMin bool
	HasMax bool
}

// AtLeast returns a FloatRange requiring values >= minVal.
func AtLeast(minVal float64) FloatRange {
	return FloatRange{Min: minVal, HasMin: true}
}

// AtMost returns a FloatRange requiring values <= maxVal.
func AtMost(maxVal float64) FloatRange {
	return FloatRange{Max: maxVal, HasMax: true}
}

// Between returns a FloatRange requiring values in [minVal, maxVal].
func Between(minVal, maxVal float64) FloatRange {
	return FloatRange{Min: minVal, Max: maxVal, HasMin: true, HasMax: true}
}

// Contains reports whether v satisfies the configured bounds. NaN always returns false.
func (r FloatRange) Contains(v float64) bool {
	if math.IsNaN(v) {
		return false
	}
	if r.HasMin && v < r.Min {
		return false
	}
	if r.HasMax && v > r.Max {
		return false
	}
	return true
}

// TestPurpose controls whether a TacticalTest filters candidates, scores them, or both.
type TestPurpose uint8

const (
	// TestFilterAndScore filters candidates failing the test's filter criteria and adds normalized score * Weight.
	TestFilterAndScore TestPurpose = iota
	// TestFilterOnly removes candidates failing the test's filter criteria without modifying scores.
	TestFilterOnly
	// TestScoreOnly scores surviving candidates without removing any.
	TestScoreOnly
)

// AngleMode selects which two vectors AngleTest measures the angle between.
type AngleMode uint8

const (
	// AngleCoverToTarget measures the angle between candidate.Facing and normalize(Target - candidate.Pos).
	// 0 radians means the cover directly faces and shields against the target; pi radians means fully exposed.
	AngleCoverToTarget AngleMode = iota
	// AngleFlank measures the angle between normalize(candidate.Pos - Target) and the reference direction
	// (ReferenceDir or ctx.TargetDir): 0 is directly in front of the target, pi/2 is a side flank, pi is behind.
	AngleFlank
	// AngleToDirection measures the angle between candidate.Facing and the reference direction.
	AngleToDirection
	// AngleFromTargetToDir measures the angle between normalize(Target - candidate.Pos) and the reference direction.
	AngleFromTargetToDir
)

// LOSFunc reports whether there is an unobstructed line of sight between world points from and to.
type LOSFunc func(from, to math2d.Vec2) bool

// QueryBudget caps expensive spatial tests (line-of-sight raycasts and pathfinding searches)
// performed across one or more TacticalQuery runs within a single simulation step.
//
// Call Reset at the start of each game.Loop step. Non-positive limits mean unlimited for that resource.
type QueryBudget struct {
	// MaxRaycasts is the maximum number of LOS raycast callbacks allowed per step (<= 0 means unlimited).
	MaxRaycasts int
	// MaxPathQueries is the maximum number of pathfinding queries allowed per step (<= 0 means unlimited).
	MaxPathQueries int

	raycastsUsed    int
	pathQueriesUsed int
}

// NewQueryBudget returns a QueryBudget with the given per-step limits (<= 0 means unlimited).
func NewQueryBudget(maxRaycasts, maxPathQueries int) *QueryBudget {
	return &QueryBudget{
		MaxRaycasts:    maxRaycasts,
		MaxPathQueries: maxPathQueries,
	}
}

// Reset zeroes the step's consumed raycast and path-query counters.
func (b *QueryBudget) Reset() {
	if b == nil {
		return
	}
	b.raycastsUsed = 0
	b.pathQueriesUsed = 0
}

// TryConsumeRaycast consumes one raycast from the step budget and returns true,
// or returns false if MaxRaycasts has been reached. A nil budget is unlimited.
func (b *QueryBudget) TryConsumeRaycast() bool {
	if b == nil {
		return true
	}
	if b.MaxRaycasts > 0 && b.raycastsUsed >= b.MaxRaycasts {
		return false
	}
	b.raycastsUsed++
	return true
}

// TryConsumePathQuery consumes one path query from the step budget and returns true,
// or returns false if MaxPathQueries has been reached. A nil budget is unlimited.
func (b *QueryBudget) TryConsumePathQuery() bool {
	if b == nil {
		return true
	}
	if b.MaxPathQueries > 0 && b.pathQueriesUsed >= b.MaxPathQueries {
		return false
	}
	b.pathQueriesUsed++
	return true
}

// RaycastsUsed returns how many raycasts have been consumed since the last Reset.
func (b *QueryBudget) RaycastsUsed() int {
	if b == nil {
		return 0
	}
	return b.raycastsUsed
}

// PathQueriesUsed returns how many path queries have been consumed since the last Reset.
func (b *QueryBudget) PathQueriesUsed() int {
	if b == nil {
		return 0
	}
	return b.pathQueriesUsed
}

// RaycastsRemaining returns remaining raycasts for the current step, or -1 if unlimited.
func (b *QueryBudget) RaycastsRemaining() int {
	if b == nil || b.MaxRaycasts <= 0 {
		return -1
	}
	return max(0, b.MaxRaycasts-b.raycastsUsed)
}

// PathQueriesRemaining returns remaining path queries for the current step, or -1 if unlimited.
func (b *QueryBudget) PathQueriesRemaining() int {
	if b == nil || b.MaxPathQueries <= 0 {
		return -1
	}
	return max(0, b.MaxPathQueries-b.pathQueriesUsed)
}

// QueryStats summarizes the work performed during the most recent TacticalQuery run.
type QueryStats struct {
	// CandidatesGenerated is the total number of candidates produced by all generators.
	CandidatesGenerated int
	// CandidatesSurviving is the number of candidates remaining after all filters and budget checks.
	CandidatesSurviving int
	// RaycastsUsed is the number of LOS callbacks invoked during this query.
	RaycastsUsed int
	// PathQueriesUsed is the number of path queries invoked during this query.
	PathQueriesUsed int
	// BudgetExhausted is true if any expensive test stopped early because ctx.Budget ran out.
	BudgetExhausted bool
}

// TacticalCandidate is one spatial position evaluated by a TacticalQuery.
type TacticalCandidate struct {
	// Pos is the candidate world position.
	Pos math2d.Vec2
	// Facing is the candidate's unit facing vector (for cover points, points into the cover obstacle).
	Facing math2d.Vec2
	// Height is CoverHalf or CoverFull for cover candidates, or 0 for non-cover candidates.
	Height CoverHeight
	// CoverID is the CoverPoint.ID for cover candidates, or 0 otherwise.
	CoverID uint64
	// CoverIndex is the source index in CoverPointGenerator.Points, or -1 for non-cover candidates.
	CoverIndex int
	// Elevation is the optional 3D vertical coordinate (0 in 2D).
	Elevation float32
	// Score is the final composite score computed by TacticalQuery.
	Score float64
}

// TacticalContext supplies per-agent runtime state to a TacticalQuery.
type TacticalContext struct {
	// QuerierID identifies the querying agent (used by PointReservations to ignore the agent's own reservation).
	QuerierID uint64
	// QuerierPos is the querying agent's world position.
	QuerierPos math2d.Vec2
	// TargetPos is the primary threat, enemy, or objective world position.
	TargetPos math2d.Vec2
	// TargetDir is an optional reference direction (for example, the threat's look direction for flanking).
	TargetDir math2d.Vec2
	// Reservations optionally excludes positions currently reserved by other agents before tests run.
	Reservations *PointReservations
	// Budget optionally caps raycasts and path queries across queries in the current step.
	Budget *QueryBudget
}

// CandidateGenerator appends deterministic candidate positions to dst and returns the extended slice.
type CandidateGenerator interface {
	Generate(ctx TacticalContext, dst []TacticalCandidate) []TacticalCandidate
}

// CoverPointGenerator emits candidates from a slice of CoverPoint values.
type CoverPointGenerator struct {
	// Points is the source list of cover points.
	Points []CoverPoint
	// Center selects the origin for MaxRadius filtering (defaults to OriginQuerier).
	Center QueryOrigin
	// CustomCenter is used when Center is OriginCustom.
	CustomCenter math2d.Vec2
	// MaxRadius restricts candidates to those within MaxRadius of Center (<= 0 includes all points).
	MaxRadius float32
	// MinHeight restricts candidates to cover points with Height >= MinHeight (0 allows any height).
	MinHeight CoverHeight
	// RequiredTag restricts candidates to cover points carrying this tag when non-empty.
	RequiredTag string
}

// Generate appends matching cover points to dst in deterministic slice order.
func (g *CoverPointGenerator) Generate(ctx TacticalContext, dst []TacticalCandidate) []TacticalCandidate {
	if g == nil || len(g.Points) == 0 {
		return dst
	}
	center := resolveOrigin(g.Center, OriginQuerier, g.CustomCenter, ctx)
	useRad := g.MaxRadius > 0 && !math.IsInf(float64(g.MaxRadius), 0)
	maxRadSq := float64(g.MaxRadius) * float64(g.MaxRadius)

	for i := range g.Points {
		cp := &g.Points[i]
		if g.MinHeight != 0 && cp.Height < g.MinHeight {
			continue
		}
		if g.RequiredTag != "" && !cp.HasTag(g.RequiredTag) {
			continue
		}
		if useRad {
			dx := float64(cp.Pos.X) - float64(center.X)
			dy := float64(cp.Pos.Y) - float64(center.Y)
			if dx*dx+dy*dy > maxRadSq {
				continue
			}
		}
		dst = append(dst, TacticalCandidate{
			Pos:        cp.Pos,
			Facing:     cp.Facing,
			Height:     cp.Height,
			CoverID:    cp.ID,
			CoverIndex: i,
			Elevation:  cp.Elevation,
		})
	}
	return dst
}

// RingGenerator emits candidates arranged on one or more concentric rings (or arcs) around a center.
type RingGenerator struct {
	// Center selects the ring center (defaults to OriginTarget).
	Center QueryOrigin
	// CustomCenter is used when Center is OriginCustom.
	CustomCenter math2d.Vec2
	// InnerRadius is the innermost ring radius (used directly when OuterRadius <= InnerRadius or Rings <= 1).
	InnerRadius float32
	// OuterRadius is the outermost ring radius.
	OuterRadius float32
	// Rings is the number of concentric rings (values <= 0 default to 1).
	Rings int
	// PointsPerRing is the number of evenly spaced samples per ring (values <= 0 default to 8).
	PointsPerRing int
	// StartAngle is the base angle in radians (0 along +X; for arcs < 2*pi, the arc is centered on StartAngle).
	StartAngle float32
	// Arc is the angular span in radians; values <= 0 or >= 2*pi generate a full 360-degree ring.
	Arc float32
	// FaceCenter sets each candidate's Facing to the unit vector pointing toward the ring center.
	FaceCenter bool
}

// Generate appends concentric ring candidates in deterministic ring-major, angle-minor order.
func (g *RingGenerator) Generate(ctx TacticalContext, dst []TacticalCandidate) []TacticalCandidate {
	if g == nil {
		return dst
	}
	rings := g.Rings
	if rings <= 0 {
		rings = 1
	}
	pts := g.PointsPerRing
	if pts <= 0 {
		pts = 8
	}
	rIn := float64(g.InnerRadius)
	if math.IsNaN(rIn) || math.IsInf(rIn, 0) || rIn < 0 {
		rIn = 0
	}
	rOut := float64(g.OuterRadius)
	if math.IsNaN(rOut) || math.IsInf(rOut, 0) || rOut < rIn {
		rOut = rIn
	}

	center := resolveOrigin(g.Center, OriginTarget, g.CustomCenter, ctx)
	fullCircle := !(g.Arc > 0) || float64(g.Arc) >= 2*math.Pi-1e-6 || math.IsInf(float64(g.Arc), 0)
	baseAng := float64(g.StartAngle)
	if math.IsNaN(baseAng) || math.IsInf(baseAng, 0) {
		baseAng = 0
	}
	arc := float64(g.Arc)

	for ri := range rings {
		var rad float64
		if rings == 1 {
			rad = rOut
		} else {
			rad = rIn + (rOut-rIn)*float64(ri)/float64(rings-1)
		}
		for pi := range pts {
			var ang float64
			if fullCircle {
				ang = baseAng + 2*math.Pi*float64(pi)/float64(pts)
			} else if pts == 1 {
				ang = baseAng
			} else {
				ang = baseAng - arc*0.5 + arc*float64(pi)/float64(pts-1)
			}
			sn, cs := math.Sincos(ang)
			pos := math2d.Vec2{
				X: float32(float64(center.X) + cs*rad),
				Y: float32(float64(center.Y) + sn*rad),
			}
			var facing math2d.Vec2
			if g.FaceCenter && rad > 0 {
				facing = math2d.Vec2{X: float32(-cs), Y: float32(-sn)}
			}
			dst = append(dst, TacticalCandidate{
				Pos:        pos,
				Facing:     facing,
				CoverIndex: -1,
			})
		}
	}
	return dst
}

// GridGenerator emits candidates on a regular 2D grid around Center or across a pathfind.Grid's walkable cells.
type GridGenerator struct {
	// Center selects the grid sampling center (defaults to OriginQuerier).
	Center QueryOrigin
	// CustomCenter is used when Center is OriginCustom.
	CustomCenter math2d.Vec2
	// HalfExtent is the half-width and half-height of the sampled world region around Center.
	// When Grid is non-nil and HalfExtent is zero, all walkable cells in Grid are emitted.
	HalfExtent math2d.Vec2
	// Spacing is the world distance between samples (defaults to CellSize when Grid != nil, else 16).
	Spacing float32
	// Grid optionally restricts candidates to walkable cells using GridOrigin and CellSize.
	Grid pathfind.Grid
	// GridOrigin is the world origin of cell (0, 0) in Grid.
	GridOrigin math2d.Vec2
	// CellSize is the world size of one cell in Grid (defaults to 1 when <= 0).
	CellSize float32
}

// Generate appends grid candidates in deterministic row-major order.
func (g *GridGenerator) Generate(ctx TacticalContext, dst []TacticalCandidate) []TacticalCandidate {
	if g == nil {
		return dst
	}
	cellSize := g.CellSize
	if !(cellSize > 0) || math.IsInf(float64(cellSize), 0) {
		cellSize = 1
	}

	// Whole-grid walkable cell mode when Grid is set and HalfExtent is zero.
	if g.Grid != nil && g.HalfExtent.X <= 0 && g.HalfExtent.Y <= 0 {
		cols, rows := g.Grid.GridSize()
		step := 1
		if g.Spacing > cellSize {
			step = max(1, int(math.Round(float64(g.Spacing)/float64(cellSize))))
		}
		for row := 0; row < rows; row += step {
			for col := 0; col < cols; col += step {
				if !g.Grid.Walkable(col, row) {
					continue
				}
				pos := math2d.Vec2{
					X: g.GridOrigin.X + (float32(col)+0.5)*cellSize,
					Y: g.GridOrigin.Y + (float32(row)+0.5)*cellSize,
				}
				dst = append(dst, TacticalCandidate{Pos: pos, CoverIndex: -1})
			}
		}
		return dst
	}

	spacing := g.Spacing
	if !(spacing > 0) || math.IsInf(float64(spacing), 0) {
		if g.Grid != nil {
			spacing = cellSize
		} else {
			spacing = 16
		}
	}
	hx := float64(g.HalfExtent.X)
	hy := float64(g.HalfExtent.Y)
	if math.IsNaN(hx) || math.IsInf(hx, 0) || hx < 0 {
		hx = 0
	}
	if math.IsNaN(hy) || math.IsInf(hy, 0) || hy < 0 {
		hy = 0
	}
	center := resolveOrigin(g.Center, OriginQuerier, g.CustomCenter, ctx)
	sp := float64(spacing)
	nx := int(math.Floor(hx/sp + 1e-6))
	ny := int(math.Floor(hy/sp + 1e-6))

	var cols, rows int
	if g.Grid != nil {
		cols, rows = g.Grid.GridSize()
	}

	for iy := -ny; iy <= ny; iy++ {
		y := float32(float64(center.Y) + float64(iy)*sp)
		for ix := -nx; ix <= nx; ix++ {
			x := float32(float64(center.X) + float64(ix)*sp)
			if g.Grid != nil {
				c := int(math.Floor((float64(x) - float64(g.GridOrigin.X)) / float64(cellSize)))
				r := int(math.Floor((float64(y) - float64(g.GridOrigin.Y)) / float64(cellSize)))
				if c < 0 || c >= cols || r < 0 || r >= rows || !g.Grid.Walkable(c, r) {
					continue
				}
			}
			dst = append(dst, TacticalCandidate{
				Pos:        math2d.Vec2{X: x, Y: y},
				CoverIndex: -1,
			})
		}
	}
	return dst
}

// NavMeshGenerator emits deterministic sample points lying inside a 2D NavMesh.
//
// When Spacing > 0, it samples a regular grid clipped to the mesh bounds (and optional MaxRadius)
// keeping every point contained in the mesh. When Spacing <= 0, it emits each polygon's centroid.
type NavMeshGenerator struct {
	// Mesh is the 2D navigation mesh to sample.
	Mesh *pathfind.NavMesh
	// Center selects the origin for MaxRadius filtering (defaults to OriginQuerier).
	Center QueryOrigin
	// CustomCenter is used when Center is OriginCustom.
	CustomCenter math2d.Vec2
	// MaxRadius restricts samples to within MaxRadius of Center (<= 0 samples the whole mesh).
	MaxRadius float32
	// Spacing sets the world spacing between grid samples inside the mesh (<= 0 emits polygon centroids).
	Spacing float32

	samples []core.Point
}

// Generate appends NavMesh sample candidates in deterministic order with zero heap allocations after warm-up.
func (g *NavMeshGenerator) Generate(ctx TacticalContext, dst []TacticalCandidate) []TacticalCandidate {
	if g == nil || g.Mesh == nil {
		return dst
	}
	center := resolveOrigin(g.Center, OriginQuerier, g.CustomCenter, ctx)
	g.samples = g.Mesh.AppendSamples(g.samples[:0], core.Point{X: core.Points(center.X), Y: core.Points(center.Y)}, g.MaxRadius, g.Spacing)
	for _, p := range g.samples {
		dst = append(dst, TacticalCandidate{
			Pos:        math2d.Vec2{X: float32(p.X), Y: float32(p.Y)},
			CoverIndex: -1,
		})
	}
	return dst
}

// TacticalTest filters and/or scores candidates inside a TacticalQuery.
type TacticalTest interface {
	isBudgeted() bool
	runTest(q *TacticalQuery, ctx TacticalContext)
}

// DistanceTest measures Euclidean distance from each candidate to a reference point.
type DistanceTest struct {
	// To selects the reference position (defaults to OriginQuerier).
	To QueryOrigin
	// CustomPos is used when To is OriginCustom.
	CustomPos math2d.Vec2
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Filter optionally restricts acceptable distances (e.g. Between(40, 200)).
	Filter FloatRange
	// NormMin and NormMax optionally specify fixed normalization bounds.
	// When NormMax <= NormMin, values normalize dynamically across surviving candidates.
	NormMin, NormMax float64
	// Weight scales the normalized [0, 1] score contribution (0 contributes no score).
	Weight float64
	// Invert flips the normalized score (1 - norm) so shorter distances score higher.
	Invert bool
}

func (t *DistanceTest) isBudgeted() bool { return false }

func (t *DistanceTest) runTest(q *TacticalQuery, ctx TacticalContext) {
	if t == nil {
		return
	}
	ref := resolveOrigin(t.To, OriginQuerier, t.CustomPos, ctx)
	q.evalContinuous(t.Purpose, t.Filter, t.NormMin, t.NormMax, t.Weight, t.Invert, func(c *TacticalCandidate) (float64, bool) {
		dx := float64(c.Pos.X) - float64(ref.X)
		dy := float64(c.Pos.Y) - float64(ref.Y)
		return math.Hypot(dx, dy), true
	})
}

// LOSTest evaluates line of sight between each candidate and a target via a game-supplied callback.
//
// Each invocation consumes one raycast from ctx.Budget (when non-nil). When the budget is
// exhausted, remaining candidates are skipped (dropped if filtering, or scored 0 if TestScoreOnly).
type LOSTest struct {
	// LOS returns true when there is unobstructed line of sight from from to to.
	// If nil, every candidate is treated as having clear line of sight.
	LOS LOSFunc
	// Target selects the other endpoint of the line-of-sight check (defaults to OriginTarget).
	Target QueryOrigin
	// CustomTarget is used when Target is OriginCustom.
	CustomTarget math2d.Vec2
	// FromTarget reverses the ray direction to LOS(target, candidate) instead of LOS(candidate, target).
	FromTarget bool
	// WantClear selects whether unobstructed sight (true, e.g. firing spot) or blocked sight
	// (false, e.g. hidden cover) passes the filter and earns a score of 1.
	WantClear bool
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Weight scales the score contribution (1 when LOS matches WantClear, 0 otherwise).
	Weight float64
}

func (t *LOSTest) isBudgeted() bool { return true }

func (t *LOSTest) runTest(q *TacticalQuery, ctx TacticalContext) {
	if t == nil {
		return
	}
	target := resolveOrigin(t.Target, OriginTarget, t.CustomTarget, ctx)

	doFilter := t.Purpose != TestScoreOnly
	doScore := t.Purpose != TestFilterOnly && t.Weight != 0 && !math.IsNaN(t.Weight) && !math.IsInf(t.Weight, 0)

	writeIdx := 0
	for i := range q.order {
		ci := q.order[i]
		c := &q.candidates[ci]
		if !ctx.Budget.TryConsumeRaycast() {
			q.lastStats.BudgetExhausted = true
			if doFilter {
				// Remaining candidates cannot be verified within the step budget.
				break
			}
			// In TestScoreOnly mode, leave remaining candidates un-scored and keep them.
			q.order[writeIdx] = ci
			writeIdx++
			continue
		}
		q.lastStats.RaycastsUsed++
		clearLOS := true
		if t.LOS != nil {
			if t.FromTarget {
				clearLOS = t.LOS(target, c.Pos)
			} else {
				clearLOS = t.LOS(c.Pos, target)
			}
		}
		matched := clearLOS == t.WantClear
		if doFilter && !matched {
			continue
		}
		if doScore && matched {
			c.Score += t.Weight
		}
		q.order[writeIdx] = ci
		writeIdx++
	}
	q.order = q.order[:writeIdx]
}

// AngleTest measures the angle in radians [0, pi] between two directions.
//
// By default (AngleCoverToTarget), it measures the angle between candidate.Facing and the
// direction from candidate.Pos toward TargetPos: 0 radians means the cover directly blocks
// threats from TargetPos.
type AngleTest struct {
	// Mode chooses which vectors are compared (defaults to AngleCoverToTarget).
	Mode AngleMode
	// Target selects the reference point for AngleCoverToTarget, AngleFlank, and AngleFromTargetToDir.
	// Zero value defaults to OriginTarget.
	Target QueryOrigin
	// CustomTarget is used when Target is OriginCustom.
	CustomTarget math2d.Vec2
	// ReferenceDir overrides ctx.TargetDir when non-zero for AngleFlank, AngleToDirection, and AngleFromTargetToDir.
	ReferenceDir math2d.Vec2
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Filter optionally restricts angles in radians (for example, AtMost(math.Pi/3)).
	Filter FloatRange
	// NormMin and NormMax optionally specify fixed normalization bounds in radians.
	// When NormMax <= NormMin, values normalize dynamically across surviving candidates.
	NormMin, NormMax float64
	// Weight scales the normalized [0, 1] score contribution.
	Weight float64
	// Invert flips the normalized score (1 - norm) so smaller angles score higher.
	Invert bool
}

func (t *AngleTest) isBudgeted() bool { return false }

func (t *AngleTest) runTest(q *TacticalQuery, ctx TacticalContext) {
	if t == nil {
		return
	}
	target := resolveOrigin(t.Target, OriginTarget, t.CustomTarget, ctx)
	refDir := t.ReferenceDir
	if refDir == (math2d.Vec2{}) {
		refDir = ctx.TargetDir
	}
	refDir = refDir.Normalize()

	q.evalContinuous(t.Purpose, t.Filter, t.NormMin, t.NormMax, t.Weight, t.Invert, func(c *TacticalCandidate) (float64, bool) {
		var a, b math2d.Vec2
		switch t.Mode {
		case AngleFlank:
			a = c.Pos.Sub(target).Normalize()
			b = refDir
		case AngleToDirection:
			a = c.Facing.Normalize()
			b = refDir
		case AngleFromTargetToDir:
			a = target.Sub(c.Pos).Normalize()
			b = refDir
		default: // AngleCoverToTarget
			a = c.Facing.Normalize()
			b = target.Sub(c.Pos).Normalize()
		}
		if a == (math2d.Vec2{}) || b == (math2d.Vec2{}) {
			return 0, false
		}
		dot := float64(a.X)*float64(b.X) + float64(a.Y)*float64(b.Y)
		dot = min(max(dot, -1), 1)
		return math.Acos(dot), true
	})
}

// PathDistanceTest measures path travel cost from a starting position to each candidate using
// a custom callback, a pathfind.Grid with AStar, or a 2D NavMesh.
//
// Each path lookup consumes one path query from ctx.Budget (when non-nil). When the budget is
// exhausted, remaining candidates are skipped (dropped if filtering, or scored 0 if TestScoreOnly).
type PathDistanceTest struct {
	// From selects the path start point (defaults to OriginQuerier).
	From QueryOrigin
	// CustomFrom is used when From is OriginCustom.
	CustomFrom math2d.Vec2
	// PathCost optionally computes path distance directly via a game-supplied callback.
	// When non-nil, it takes precedence over Grid and NavMesh.
	PathCost func(from, to math2d.Vec2) (cost float64, ok bool)
	// Grid and AStar run zero-allocation grid A* pathfinding when PathCost is nil and Grid is non-nil.
	// If AStar is nil, TacticalQuery uses its internal reusable AStar searcher.
	Grid       pathfind.Grid
	GridOrigin math2d.Vec2
	CellSize   float32
	AStar      *pathfind.AStar
	// NavMesh computes zero-allocation funnel-smoothed path length when PathCost and Grid are nil.
	NavMesh *pathfind.NavMesh
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Filter optionally restricts acceptable path lengths.
	Filter FloatRange
	// NormMin and NormMax optionally specify fixed normalization bounds.
	// When NormMax <= NormMin, values normalize dynamically across surviving candidates.
	NormMin, NormMax float64
	// Weight scales the normalized [0, 1] score contribution.
	Weight float64
	// Invert flips the normalized score (1 - norm) so shorter paths score higher.
	Invert bool
}

func (t *PathDistanceTest) isBudgeted() bool { return true }

func (t *PathDistanceTest) runTest(q *TacticalQuery, ctx TacticalContext) {
	if t == nil {
		return
	}
	from := resolveOrigin(t.From, OriginQuerier, t.CustomFrom, ctx)
	cellSize := float64(t.CellSize)
	if !(cellSize > 0) || math.IsInf(cellSize, 0) {
		cellSize = 1
	}

	var startCell pathfind.Cell
	var gridCols, gridRows int
	var searcher *pathfind.AStar
	if t.PathCost == nil && t.Grid != nil {
		gridCols, gridRows = t.Grid.GridSize()
		startCell = pathfind.Cell{
			Col: int(math.Floor((float64(from.X) - float64(t.GridOrigin.X)) / cellSize)),
			Row: int(math.Floor((float64(from.Y) - float64(t.GridOrigin.Y)) / cellSize)),
		}
		searcher = t.AStar
		if searcher == nil {
			if q.astar == nil {
				q.astar = pathfind.NewAStar()
			}
			searcher = q.astar
		}
	}

	doFilter := t.Purpose != TestScoreOnly
	doScore := t.Purpose != TestFilterOnly && t.Weight != 0 && !math.IsNaN(t.Weight) && !math.IsInf(t.Weight, 0)

	n := len(q.order)
	q.ensureEvalScratch(n)

	minVal := math.Inf(1)
	maxVal := math.Inf(-1)
	scoredCount := 0
	evaluatedCount := 0

	for i := range n {
		ci := q.order[i]
		c := &q.candidates[ci]
		if !ctx.Budget.TryConsumePathQuery() {
			q.lastStats.BudgetExhausted = true
			if doFilter {
				// Mark remaining candidates invalid so unverified paths are not returned.
				for j := i; j < n; j++ {
					q.validMask[j] = false
					q.scoredMask[j] = false
				}
				break
			}
			for j := i; j < n; j++ {
				q.validMask[j] = true
				q.scoredMask[j] = false
			}
			break
		}
		q.lastStats.PathQueriesUsed++
		evaluatedCount++

		var cost float64
		var ok bool
		switch {
		case t.PathCost != nil:
			cost, ok = t.PathCost(from, c.Pos)
		case t.Grid != nil:
			if startCell.Col >= 0 && startCell.Col < gridCols && startCell.Row >= 0 && startCell.Row < gridRows {
				goalCell := pathfind.Cell{
					Col: int(math.Floor((float64(c.Pos.X) - float64(t.GridOrigin.X)) / cellSize)),
					Row: int(math.Floor((float64(c.Pos.Y) - float64(t.GridOrigin.Y)) / cellSize)),
				}
				if goalCell.Col >= 0 && goalCell.Col < gridCols && goalCell.Row >= 0 && goalCell.Row < gridRows {
					var path []pathfind.Cell
					path, cost, ok = searcher.FindInto(q.pathBuf[:0], t.Grid, startCell, goalCell)
					q.pathBuf = path
					if ok {
						cost *= cellSize
					}
				}
			}
		case t.NavMesh != nil:
			q.navQuery.SetMesh(t.NavMesh)
			cost, ok = q.navQuery.PathLength(core.Point{X: core.Points(from.X), Y: core.Points(from.Y)}, core.Point{X: core.Points(c.Pos.X), Y: core.Points(c.Pos.Y)})
		default:
			dx := float64(c.Pos.X) - float64(from.X)
			dy := float64(c.Pos.Y) - float64(from.Y)
			cost, ok = math.Hypot(dx, dy), true
		}

		if !ok || math.IsNaN(cost) || math.IsInf(cost, 0) {
			q.validMask[i] = !doFilter
			q.scoredMask[i] = false
			continue
		}
		if doFilter && !t.Filter.Contains(cost) {
			q.validMask[i] = false
			q.scoredMask[i] = false
			continue
		}
		q.validMask[i] = true
		q.scoredMask[i] = doScore
		q.rawVals[i] = cost
		if doScore {
			if cost < minVal {
				minVal = cost
			}
			if cost > maxVal {
				maxVal = cost
			}
			scoredCount++
		}
	}
	_ = evaluatedCount

	q.commitEval(n, doScore && scoredCount > 0, minVal, maxVal, t.NormMin, t.NormMax, t.Weight, t.Invert)
}

// InfluenceTest reads the bilinearly sampled scalar value from an ai.InfluenceLayer at each candidate.
type InfluenceTest struct {
	// Layer is the InfluenceLayer sampled at each candidate's world position.
	Layer *InfluenceLayer
	// OutOfBoundsValue is used when a candidate lies outside Layer and RejectOutOfBounds is false.
	OutOfBoundsValue float64
	// RejectOutOfBounds filters out candidates outside Layer when Purpose allows filtering.
	RejectOutOfBounds bool
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Filter optionally restricts acceptable influence values.
	Filter FloatRange
	// NormMin and NormMax optionally specify fixed normalization bounds.
	// When NormMax <= NormMin, values normalize dynamically across surviving candidates.
	NormMin, NormMax float64
	// Weight scales the normalized [0, 1] score contribution.
	Weight float64
	// Invert flips the normalized score (1 - norm) so lower influence (e.g. lower threat) scores higher.
	Invert bool
}

func (t *InfluenceTest) isBudgeted() bool { return false }

func (t *InfluenceTest) runTest(q *TacticalQuery, _ TacticalContext) {
	if t == nil {
		return
	}
	q.evalContinuous(t.Purpose, t.Filter, t.NormMin, t.NormMax, t.Weight, t.Invert, func(c *TacticalCandidate) (float64, bool) {
		if t.Layer == nil {
			if t.RejectOutOfBounds {
				return 0, false
			}
			return t.OutOfBoundsValue, true
		}
		v, ok := t.Layer.Sample(c.Pos)
		if !ok {
			if t.RejectOutOfBounds {
				return 0, false
			}
			return t.OutOfBoundsValue, true
		}
		return float64(v), true
	})
}

// CoverHeightTest filters and/or scores candidates based on their CoverHeight.
type CoverHeightTest struct {
	// MinHeight filters out candidates with Height < MinHeight when Purpose allows filtering (0 disables).
	MinHeight CoverHeight
	// Prefer selects which CoverHeight earns a score of 1 (defaults to CoverFull when 0).
	Prefer CoverHeight
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Weight scales the score contribution (1 when candidate.Height == Prefer, 0 otherwise).
	Weight float64
}

func (t *CoverHeightTest) isBudgeted() bool { return false }

func (t *CoverHeightTest) runTest(q *TacticalQuery, _ TacticalContext) {
	if t == nil {
		return
	}
	prefer := defaultCoverHeight(t.Prefer)
	doFilter := t.Purpose != TestScoreOnly
	doScore := t.Purpose != TestFilterOnly && t.Weight != 0 && !math.IsNaN(t.Weight) && !math.IsInf(t.Weight, 0)

	writeIdx := 0
	for i := range q.order {
		ci := q.order[i]
		c := &q.candidates[ci]
		if doFilter && t.MinHeight != 0 && c.Height < t.MinHeight {
			continue
		}
		if doScore && c.Height == prefer {
			c.Score += t.Weight
		}
		q.order[writeIdx] = ci
		writeIdx++
	}
	q.order = q.order[:writeIdx]
}

// CustomTest runs a user-supplied scoring/filtering callback on each surviving candidate.
type CustomTest struct {
	// Eval returns a raw numeric value and whether the candidate is valid.
	Eval func(ctx TacticalContext, c TacticalCandidate) (value float64, ok bool)
	// Purpose selects filtering, scoring, or both (defaults to TestFilterAndScore).
	Purpose TestPurpose
	// Filter optionally restricts acceptable raw values.
	Filter FloatRange
	// NormMin and NormMax optionally specify fixed normalization bounds.
	NormMin, NormMax float64
	// Weight scales the normalized [0, 1] score contribution.
	Weight float64
	// Invert flips the normalized score (1 - norm).
	Invert bool
}

func (t *CustomTest) isBudgeted() bool { return false }

func (t *CustomTest) runTest(q *TacticalQuery, ctx TacticalContext) {
	if t == nil || t.Eval == nil {
		return
	}
	q.evalContinuous(t.Purpose, t.Filter, t.NormMin, t.NormMax, t.Weight, t.Invert, func(c *TacticalCandidate) (float64, bool) {
		return t.Eval(ctx, *c)
	})
}

// TacticalQuery executes an EQS-style pipeline: generate candidate positions, exclude points
// reserved by other agents, run ordered filter/score tests within any configured step budget,
// and sort surviving candidates deterministically by descending score.
//
// TacticalQuery reuses all internal scratch buffers across runs so Best, RunInto, and
// RunAndReserve perform zero heap allocations after warm-up. It is not safe for concurrent use.
type TacticalQuery struct {
	// Generators produce candidate positions in declaration order.
	Generators []CandidateGenerator
	// Tests evaluate surviving candidates in declaration order.
	// Place cheap filters and scorers (Distance, Angle, CoverHeight, Influence) before
	// expensive budgeted tests (LOS, PathDistance) so step budgets are spent on top candidates.
	Tests []TacticalTest

	candidates []TacticalCandidate
	order      []int
	rawVals    []float64
	validMask  []bool
	scoredMask []bool
	astar      *pathfind.AStar
	pathBuf    []pathfind.Cell
	navQuery   pathfind.NavMeshQuery
	lastStats  QueryStats
}

// NewTacticalQuery returns a TacticalQuery configured with the given generators and tests.
func NewTacticalQuery(generators []CandidateGenerator, tests ...TacticalTest) *TacticalQuery {
	return &TacticalQuery{
		Generators: generators,
		Tests:      tests,
	}
}

// LastStats returns diagnostic counts from the most recent Run, RunInto, Best, or RunAndReserve call.
func (q *TacticalQuery) LastStats() QueryStats {
	if q == nil {
		return QueryStats{}
	}
	return q.lastStats
}

// Run executes the query and returns a newly allocated slice of surviving candidates sorted
// deterministically from highest to lowest Score. Use RunInto or Best to avoid heap allocations.
func (q *TacticalQuery) Run(ctx TacticalContext) []TacticalCandidate {
	return q.RunInto(nil, ctx)
}

// RunInto executes the query and appends surviving candidates in deterministic descending Score
// order into dst[:0], performing zero heap allocations once internal buffers and dst are warmed up.
func (q *TacticalQuery) RunInto(dst []TacticalCandidate, ctx TacticalContext) []TacticalCandidate {
	if q == nil {
		return dst[:0]
	}
	q.execute(ctx)
	dst = dst[:0]
	for _, ci := range q.order {
		dst = append(dst, q.candidates[ci])
	}
	return dst
}

// Best executes the query and returns the highest-scoring surviving candidate without allocating.
func (q *TacticalQuery) Best(ctx TacticalContext) (TacticalCandidate, bool) {
	if q == nil {
		return TacticalCandidate{}, false
	}
	q.execute(ctx)
	if len(q.order) == 0 {
		return TacticalCandidate{}, false
	}
	return q.candidates[q.order[0]], true
}

// RunAndReserve executes the query, picks the highest-scoring surviving candidate, and reserves
// it for ctx.QuerierID on ctx.Reservations (when non-nil) for timeout (or DefaultTimeout when <= 0).
// Returns (best, true) on success, or (zero, false) if no candidate survived or reservation failed.
func (q *TacticalQuery) RunAndReserve(ctx TacticalContext, timeout time.Duration) (TacticalCandidate, bool) {
	best, ok := q.Best(ctx)
	if !ok {
		return TacticalCandidate{}, false
	}
	if ctx.Reservations != nil && ctx.QuerierID != 0 {
		if !ctx.Reservations.Reserve(ctx.QuerierID, best, timeout) {
			return TacticalCandidate{}, false
		}
	}
	return best, true
}

func (q *TacticalQuery) execute(ctx TacticalContext) {
	q.lastStats = QueryStats{}
	q.candidates = q.candidates[:0]
	for _, gen := range q.Generators {
		if gen != nil {
			q.candidates = gen.Generate(ctx, q.candidates)
		}
	}
	q.lastStats.CandidatesGenerated = len(q.candidates)
	if len(q.candidates) == 0 {
		q.order = q.order[:0]
		return
	}

	// Initialize surviving candidate indices, skipping points reserved by other agents before tests run.
	q.order = q.order[:0]
	for i := range q.candidates {
		q.candidates[i].Score = 0
		if ctx.Reservations != nil {
			c := &q.candidates[i]
			if ctx.Reservations.IsReservedByOther(ctx.QuerierID, c.CoverID, c.Pos) {
				continue
			}
		}
		q.order = append(q.order, i)
	}

	for _, test := range q.Tests {
		if len(q.order) == 0 {
			break
		}
		if test == nil {
			continue
		}
		if test.isBudgeted() && ctx.Budget != nil {
			// Evaluate highest-preliminary-score candidates first under step budgets (ties by generation index).
			sortOrderByPreliminaryScore(q.order, q.candidates)
		}
		test.runTest(q, ctx)
	}

	if len(q.order) > 1 {
		sortOrderFinal(q.order, q.candidates)
	}
	q.lastStats.CandidatesSurviving = len(q.order)
}

func (q *TacticalQuery) ensureEvalScratch(n int) {
	if cap(q.rawVals) < n {
		q.rawVals = make([]float64, n)
	} else {
		q.rawVals = q.rawVals[:n]
	}
	if cap(q.validMask) < n {
		q.validMask = make([]bool, n)
	} else {
		q.validMask = q.validMask[:n]
	}
	if cap(q.scoredMask) < n {
		q.scoredMask = make([]bool, n)
	} else {
		q.scoredMask = q.scoredMask[:n]
	}
}

func (q *TacticalQuery) evalContinuous(
	purpose TestPurpose,
	filter FloatRange,
	normMin, normMax float64,
	weight float64,
	invert bool,
	fn func(c *TacticalCandidate) (float64, bool),
) {
	n := len(q.order)
	q.ensureEvalScratch(n)

	doFilter := purpose != TestScoreOnly
	doScore := purpose != TestFilterOnly && weight != 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0)

	minVal := math.Inf(1)
	maxVal := math.Inf(-1)
	scoredCount := 0

	for i := range n {
		ci := q.order[i]
		val, ok := fn(&q.candidates[ci])
		if !ok || math.IsNaN(val) || math.IsInf(val, 0) {
			q.validMask[i] = !doFilter
			q.scoredMask[i] = false
			continue
		}
		if doFilter && !filter.Contains(val) {
			q.validMask[i] = false
			q.scoredMask[i] = false
			continue
		}
		q.validMask[i] = true
		q.scoredMask[i] = doScore
		q.rawVals[i] = val
		if doScore {
			if val < minVal {
				minVal = val
			}
			if val > maxVal {
				maxVal = val
			}
			scoredCount++
		}
	}

	q.commitEval(n, doScore && scoredCount > 0, minVal, maxVal, normMin, normMax, weight, invert)
}

func (q *TacticalQuery) commitEval(n int, doScore bool, dynMin, dynMax, normMin, normMax, weight float64, invert bool) {
	useFixed := normMax > normMin && !math.IsNaN(normMin) && !math.IsNaN(normMax) && !math.IsInf(normMin, 0) && !math.IsInf(normMax, 0)
	lo, hi := dynMin, dynMax
	if useFixed {
		lo, hi = normMin, normMax
	}
	span := hi - lo

	writeIdx := 0
	for i := range n {
		if !q.validMask[i] {
			continue
		}
		ci := q.order[i]
		if doScore && q.scoredMask[i] {
			var norm float64
			if span <= 0 {
				norm = 1
			} else {
				v := q.rawVals[i]
				if useFixed {
					if v < lo {
						v = lo
					} else if v > hi {
						v = hi
					}
				}
				norm = (v - lo) / span
			}
			if invert {
				norm = 1 - norm
			}
			q.candidates[ci].Score += norm * weight
		}
		q.order[writeIdx] = ci
		writeIdx++
	}
	q.order = q.order[:writeIdx]
}

// Non-allocating in-place heapsort helpers for deterministic ordering without closure allocations.

func prelimBetter(a, b int, cands []TacticalCandidate) bool {
	sa, sb := cands[a].Score, cands[b].Score
	if sa != sb {
		return sa > sb
	}
	return a < b
}

func sortOrderByPreliminaryScore(order []int, cands []TacticalCandidate) {
	n := len(order)
	if n <= 1 {
		return
	}
	// Build max-heap (inversed so worst sits at root, yielding ascending "better" after extraction).
	for i := n/2 - 1; i >= 0; i-- {
		siftDownPrelim(order, cands, i, n)
	}
	for end := n - 1; end > 0; end-- {
		order[0], order[end] = order[end], order[0]
		siftDownPrelim(order, cands, 0, end)
	}
}

func siftDownPrelim(order []int, cands []TacticalCandidate, root, end int) {
	for {
		child := 2*root + 1
		if child >= end {
			return
		}
		if child+1 < end && prelimBetter(order[child], order[child+1], cands) {
			child++
		}
		if !prelimBetter(order[root], order[child], cands) {
			return
		}
		order[root], order[child] = order[child], order[root]
		root = child
	}
}

func finalBetter(a, b int, cands []TacticalCandidate) bool {
	ca, cb := &cands[a], &cands[b]
	if ca.Score != cb.Score {
		return ca.Score > cb.Score
	}
	if ca.CoverID != cb.CoverID {
		return ca.CoverID < cb.CoverID
	}
	if ca.Pos.Y != cb.Pos.Y {
		return ca.Pos.Y < cb.Pos.Y
	}
	if ca.Pos.X != cb.Pos.X {
		return ca.Pos.X < cb.Pos.X
	}
	if ca.Facing.Y != cb.Facing.Y {
		return ca.Facing.Y < cb.Facing.Y
	}
	if ca.Facing.X != cb.Facing.X {
		return ca.Facing.X < cb.Facing.X
	}
	return a < b
}

func sortOrderFinal(order []int, cands []TacticalCandidate) {
	n := len(order)
	if n <= 1 {
		return
	}
	for i := n/2 - 1; i >= 0; i-- {
		siftDownFinal(order, cands, i, n)
	}
	for end := n - 1; end > 0; end-- {
		order[0], order[end] = order[end], order[0]
		siftDownFinal(order, cands, 0, end)
	}
}

func siftDownFinal(order []int, cands []TacticalCandidate, root, end int) {
	for {
		child := 2*root + 1
		if child >= end {
			return
		}
		if child+1 < end && finalBetter(order[child], order[child+1], cands) {
			child++
		}
		if !finalBetter(order[root], order[child], cands) {
			return
		}
		order[root], order[child] = order[child], order[root]
		root = child
	}
}

package ai

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/math2d"
)

const (
	// DefaultORCATimeHorizon is the default look-ahead horizon (in seconds) for agent-agent avoidance.
	DefaultORCATimeHorizon float32 = 2.0
	// DefaultORCATimeHorizonObst is the default look-ahead horizon (in seconds) for static obstacle edges.
	DefaultORCATimeHorizonObst float32 = 1.0
	// DefaultORCAMaxNeighbors is the default cap on nearest neighbors per agent contributing ORCA constraints.
	DefaultORCAMaxNeighbors = 16

	// ORCALayerDefault is the default avoidance layer assigned when an ORCAAgent's Layer is 0.
	ORCALayerDefault uint32 = 1
	// ORCAMaskAll matches every avoidance layer and is the default when Mask (or obstacle Layer) is 0.
	ORCAMaskAll uint32 = ^uint32(0)

	orcaEps = 1e-9
)

// ORCAConfig tunes an ORCASolver. Zero or non-finite fields use sensible defaults.
type ORCAConfig struct {
	// TimeHorizon is how far ahead (in seconds) agents anticipate collisions with other agents.
	// Defaults to DefaultORCATimeHorizon (2.0) when <= 0 or non-finite.
	TimeHorizon float32
	// TimeHorizonObst is how far ahead (in seconds) agents anticipate collisions with static obstacle edges.
	// Defaults to DefaultORCATimeHorizonObst (1.0) when <= 0 or non-finite.
	TimeHorizonObst float32
	// NeighborDist is the maximum center-to-center distance to consider neighboring agents.
	// When <= 0 or non-finite, the solver derives a conservative search radius per agent from
	// its speed, body radius, and TimeHorizon.
	NeighborDist float32
	// MaxNeighbors caps how many nearest matching neighbors contribute ORCA half-planes per agent.
	// Defaults to DefaultORCAMaxNeighbors (16) when <= 0.
	MaxNeighbors int
	// CellSize is the spatial hash cell size used during batch Solve/Step queries.
	// When <= 0 or non-finite, it is derived automatically from NeighborDist or agent speeds and radii.
	CellSize float32
}

// ORCAAgent describes a 2D agent participating in ORCA velocity-based local avoidance.
type ORCAAgent struct {
	// ID optionally identifies the agent for canonical tie-breaking when agents overlap or coincide.
	ID uint64
	// Pos is the agent's current 2D position.
	Pos math2d.Vec2
	// Vel is the agent's current 2D velocity.
	Vel math2d.Vec2
	// PrefVel is the agent's desired 2D velocity before local avoidance.
	PrefVel math2d.Vec2
	// Radius is the agent's body radius.
	Radius float32
	// MaxSpeed caps the magnitude of the computed collision-free velocity.
	MaxSpeed float32
	// Priority controls reciprocal avoidance responsibility. Higher-priority agents yield less
	// to lower-priority agents; equal priorities split avoidance 50/50. Negative or non-finite
	// values are treated as 0.
	Priority float32
	// Stationary marks an agent holding its position: it outputs zero velocity and forces
	// avoiding neighbors to take full responsibility for steering around it.
	Stationary bool
	// Layer is a bitmask of avoidance layers this agent occupies (0 defaults to ORCALayerDefault).
	Layer uint32
	// Mask is a bitmask of avoidance layers this agent avoids (0 defaults to ORCAMaskAll).
	Mask uint32
}

// ORCAObstacle is a static 2D line-segment obstacle from A to B.
// Segments are treated as two-sided so agents stay clear regardless of endpoint order.
type ORCAObstacle struct {
	A, B math2d.Vec2
	// Layer is a bitmask of avoidance layers this obstacle occupies (0 defaults to ORCAMaskAll).
	Layer uint32
}

// ORCAAgent3D describes a 3D agent performing ORCA local avoidance on the horizontal XZ plane
// (matching ai.Vec3 / math3d.Vec3 with +X right, +Y up, -Z forward).
type ORCAAgent3D struct {
	ID         uint64
	Pos        Vec3
	Vel        Vec3
	PrefVel    Vec3
	Radius     float32
	MaxSpeed   float32
	Priority   float32
	Stationary bool
	Layer      uint32
	Mask       uint32
}

// ORCAObstacle3D is a static vertical obstacle wall segment projected onto the horizontal XZ plane.
type ORCAObstacle3D struct {
	A, B  Vec3
	Layer uint32
}

// orcaLine represents a directed 2D half-plane {v : det(dir, v - point) >= 0}.
// dir is a unit vector; the inward feasible normal is (-dir.y, dir.x).
type orcaLine struct {
	point vec2
	dir   vec2
}

type orcaCandidate struct {
	idx    int32
	distSq float64
}

type orcaObstCandidate struct {
	distSq float64
	ax, ay float64
	bx, by float64
	layer  uint32
}

type orcaObstBounds struct {
	minX, minY, maxX, maxY float64
	layer                  uint32
}

// ORCASolver computes collision-free 2D (and 3D XZ-plane) velocities using Optimal Reciprocal
// Collision Avoidance (ORCA / RVO2). It reuses internal spatial-hash and linear-programming
// scratch buffers across calls for zero heap allocations after warm-up, and canonicalizes
// neighbor and obstacle ordering so results are deterministic and independent of input order.
//
// A zero-value ORCASolver is ready to use with default settings. ORCASolver is not safe for
// concurrent use.
type ORCASolver struct {
	cfg ORCAConfig

	// Spatial hash for batch 2D Solve/Step.
	cellSize    float64
	invCellSize float64
	mask        uint32
	buckets     []int32
	next        []int32
	touched     []int32
	bucketStamp []uint32
	queryStamp  uint32

	// Scratch buffers reused across calls.
	candidates []orcaCandidate
	obstCands  []orcaObstCandidate
	obstBounds []orcaObstBounds
	lines      []orcaLine
	projLines  []orcaLine
	stepVels   []math2d.Vec2

	// XZ projection scratch buffers.
	agents2D []ORCAAgent
	obst2D   []ORCAObstacle
	vels2D   []math2d.Vec2
	step3D   []Vec3
}

// NewORCASolver creates an ORCASolver configured with cfg.
func NewORCASolver(cfg ORCAConfig) *ORCASolver {
	return &ORCASolver{cfg: sanitizeORCAConfig(cfg)}
}

// Config returns the solver's effective configuration with defaults applied.
func (s *ORCASolver) Config() ORCAConfig {
	if s == nil {
		return sanitizeORCAConfig(ORCAConfig{})
	}
	return sanitizeORCAConfig(s.cfg)
}

// SetConfig updates the solver's configuration.
func (s *ORCASolver) SetConfig(cfg ORCAConfig) {
	if s == nil {
		return
	}
	s.cfg = sanitizeORCAConfig(cfg)
}

func sanitizeORCAConfig(cfg ORCAConfig) ORCAConfig {
	if !(cfg.TimeHorizon > 0) || !isFinite32(cfg.TimeHorizon) {
		cfg.TimeHorizon = DefaultORCATimeHorizon
	}
	if !(cfg.TimeHorizonObst > 0) || !isFinite32(cfg.TimeHorizonObst) {
		cfg.TimeHorizonObst = DefaultORCATimeHorizonObst
	}
	if !(cfg.NeighborDist > 0) || !isFinite32(cfg.NeighborDist) {
		cfg.NeighborDist = 0
	}
	if cfg.MaxNeighbors <= 0 {
		cfg.MaxNeighbors = DefaultORCAMaxNeighbors
	}
	if !(cfg.CellSize > 0) || !isFinite32(cfg.CellSize) {
		cfg.CellSize = 0
	}
	return cfg
}

func isFinite64(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func sanitizeVec2(v math2d.Vec2) vec2 {
	x, y := float64(v.X), float64(v.Y)
	if !isFinite64(x) {
		x = 0
	}
	if !isFinite64(y) {
		y = 0
	}
	return vec2{x, y}
}

func sanitizeNonNeg32(v float32) float64 {
	f := float64(v)
	if !(f > 0) || !isFinite64(f) {
		return 0
	}
	return f
}

func effectiveAgentLayer(layer uint32) uint32 {
	if layer == 0 {
		return ORCALayerDefault
	}
	return layer
}

func effectiveAgentMask(mask uint32) uint32 {
	if mask == 0 {
		return ORCAMaskAll
	}
	return mask
}

func effectiveObstacleLayer(layer uint32) uint32 {
	if layer == 0 {
		return ORCAMaskAll
	}
	return layer
}

func orcaPriority(p float32) float64 {
	f := float64(p)
	if !isFinite64(f) || f < 0 {
		return 0
	}
	return f
}

// orcaResponsibility returns agent A's share of avoidance work in [0, 1] against cooperative agent B.
// Higher Priority yields less; equal priorities split 50/50.
func orcaResponsibility(pA, pB float32) float64 {
	a := orcaPriority(pA)
	b := orcaPriority(pB)
	if a == b {
		return 0.5
	}
	sum := a + b
	if sum <= 0 {
		return 0.5
	}
	return b / sum
}

// SolveAgent computes a single collision-free 2D velocity for agent given its neighbors and
// optional static obstacle edges. Results are deterministic and independent of the order of
// neighbors or obstacles.
func (s *ORCASolver) SolveAgent(agent ORCAAgent, neighbors []ORCAAgent, obstacles []ORCAObstacle, dt time.Duration) math2d.Vec2 {
	if s == nil {
		var tmp ORCASolver
		return tmp.SolveAgent(agent, neighbors, obstacles, dt)
	}
	cfg := sanitizeORCAConfig(s.cfg)
	dtSecs := dt.Seconds()
	if !(dtSecs > 0) || !isFinite64(dtSecs) {
		return math2d.Vec2{}
	}
	maxSpeed := sanitizeNonNeg32(agent.MaxSpeed)
	if agent.Stationary || maxSpeed <= 0 {
		return math2d.Vec2{}
	}

	aPos := sanitizeVec2(agent.Pos)
	aRadius := sanitizeNonNeg32(agent.Radius)
	aMask := effectiveAgentMask(agent.Mask)

	// Derive effective neighbor search radius when NeighborDist is automatic.
	searchDist := float64(cfg.NeighborDist)
	if searchDist <= 0 {
		maxNR := aRadius
		maxNS := maxSpeed
		for i := range neighbors {
			nr := sanitizeNonNeg32(neighbors[i].Radius)
			ns := sanitizeNonNeg32(neighbors[i].MaxSpeed)
			if nr > maxNR {
				maxNR = nr
			}
			if ns > maxNS {
				maxNS = ns
			}
		}
		searchDist = (maxSpeed+maxNS)*float64(cfg.TimeHorizon) + aRadius + maxNR
		if minD := 4 * max(aRadius, maxNR); searchDist < minD {
			searchDist = minD
		}
	}
	searchDistSq := searchDist * searchDist

	// Collect up to MaxNeighbors nearest matching neighbors in canonical order.
	s.candidates = s.candidates[:0]
	for i := range neighbors {
		n := neighbors[i]
		if agent.ID != 0 && n.ID == agent.ID {
			continue
		}
		if aMask&effectiveAgentLayer(n.Layer) == 0 {
			continue
		}
		nPos := sanitizeVec2(n.Pos)
		dSq := distSq(aPos, nPos)
		if dSq > searchDistSq {
			continue
		}
		if len(s.candidates) == cfg.MaxNeighbors && dSq > s.candidates[0].distSq {
			continue
		}
		s.pushCandidate(orcaCandidate{idx: int32(i), distSq: dSq}, cfg.MaxNeighbors, neighbors)
	}
	s.sortCandidates(neighbors)

	// Collect matching in-range obstacles in canonical order.
	s.collectObstacles(aPos, aRadius, maxSpeed, aMask, float64(cfg.TimeHorizonObst), obstacles, nil)

	return s.solveFromCandidates(agent, neighbors, dtSecs, cfg)
}

// Solve computes collision-free 2D velocities for every agent in agents and appends them to
// dst[:0]. Neighbor lookups use an internal spatial hash grid; results are deterministic and
// independent of input order.
func (s *ORCASolver) Solve(dst []math2d.Vec2, agents []ORCAAgent, obstacles []ORCAObstacle, dt time.Duration) []math2d.Vec2 {
	if s == nil {
		var tmp ORCASolver
		return tmp.Solve(dst, agents, obstacles, dt)
	}
	n := len(agents)
	if cap(dst) < n {
		dst = make([]math2d.Vec2, n)
	} else {
		dst = dst[:n]
	}
	if n == 0 {
		return dst
	}
	cfg := sanitizeORCAConfig(s.cfg)
	dtSecs := dt.Seconds()
	if !(dtSecs > 0) || !isFinite64(dtSecs) {
		clear(dst)
		return dst
	}

	// Scan global radius/speed bounds for automatic NeighborDist and CellSize.
	maxRadius := 0.0
	maxSpeed := 0.0
	for i := range agents {
		r := sanitizeNonNeg32(agents[i].Radius)
		sp := sanitizeNonNeg32(agents[i].MaxSpeed)
		if r > maxRadius {
			maxRadius = r
		}
		if sp > maxSpeed {
			maxSpeed = sp
		}
	}

	autoDist := 2*maxSpeed*float64(cfg.TimeHorizon) + 2*maxRadius
	if minD := 4 * maxRadius; autoDist < minD {
		autoDist = minD
	}
	if autoDist <= 0 {
		autoDist = 1
	}

	cellSize := float64(cfg.CellSize)
	if cellSize <= 0 {
		if cfg.NeighborDist > 0 {
			cellSize = max(float64(cfg.NeighborDist)*0.5, 2*maxRadius)
		} else {
			cellSize = max(autoDist*0.5, 2*maxRadius)
		}
		if cellSize <= 0 {
			cellSize = 1
		}
	}

	s.rebuildGrid(agents, cellSize)
	s.buildObstacleBounds(obstacles)

	for i := range agents {
		a := agents[i]
		aMaxSpeed := sanitizeNonNeg32(a.MaxSpeed)
		if a.Stationary || aMaxSpeed <= 0 {
			dst[i] = math2d.Vec2{}
			continue
		}
		aPos := sanitizeVec2(a.Pos)
		aRadius := sanitizeNonNeg32(a.Radius)
		aMask := effectiveAgentMask(a.Mask)

		searchDist := float64(cfg.NeighborDist)
		if searchDist <= 0 {
			searchDist = (aMaxSpeed+maxSpeed)*float64(cfg.TimeHorizon) + aRadius + maxRadius
			if minD := 4 * max(aRadius, maxRadius); searchDist < minD {
				searchDist = minD
			}
		}

		s.queryNeighbors(i, aPos, aMask, searchDist, cfg.MaxNeighbors, agents)
		s.collectObstacles(aPos, aRadius, aMaxSpeed, aMask, float64(cfg.TimeHorizonObst), obstacles, s.obstBounds)
		dst[i] = s.solveFromCandidates(a, agents, dtSecs, cfg)
	}
	return dst
}

// Step computes collision-free 2D velocities for all agents and synchronously integrates
// Vel and Pos by dt. Stationary agents keep zero velocity and fixed position.
func (s *ORCASolver) Step(agents []ORCAAgent, obstacles []ORCAObstacle, dt time.Duration) {
	if s == nil {
		var tmp ORCASolver
		tmp.Step(agents, obstacles, dt)
		return
	}
	if len(agents) == 0 || dt <= 0 {
		return
	}
	secs := float32(dt.Seconds())
	s.stepVels = s.Solve(s.stepVels, agents, obstacles, dt)
	for i := range agents {
		if agents[i].Stationary || !(agents[i].MaxSpeed > 0) {
			agents[i].Vel = math2d.Vec2{}
			continue
		}
		v := s.stepVels[i]
		agents[i].Vel = v
		agents[i].Pos = agents[i].Pos.Add(v.Scale(secs))
	}
}

// SolveAgentXZ computes a collision-free horizontal XZ-plane velocity for a 3D agent.
// The returned velocity has Y = 0; X and Z map directly to the 2D ORCA plane.
func (s *ORCASolver) SolveAgentXZ(agent ORCAAgent3D, neighbors []ORCAAgent3D, obstacles []ORCAObstacle3D, dt time.Duration) Vec3 {
	if s == nil {
		var tmp ORCASolver
		return tmp.SolveAgentXZ(agent, neighbors, obstacles, dt)
	}
	s.projectAgentsXZ(neighbors)
	s.projectObstaclesXZ(obstacles)
	v := s.SolveAgent(agent3DTo2D(agent), s.agents2D, s.obst2D, dt)
	return Vec3{X: v.X, Y: 0, Z: v.Y}
}

// SolveXZ computes collision-free horizontal XZ-plane velocities for all 3D agents and appends
// them to dst[:0]. Returned velocities have Y = 0.
func (s *ORCASolver) SolveXZ(dst []Vec3, agents []ORCAAgent3D, obstacles []ORCAObstacle3D, dt time.Duration) []Vec3 {
	if s == nil {
		var tmp ORCASolver
		return tmp.SolveXZ(dst, agents, obstacles, dt)
	}
	n := len(agents)
	if cap(dst) < n {
		dst = make([]Vec3, n)
	} else {
		dst = dst[:n]
	}
	if n == 0 {
		return dst
	}
	s.projectAgentsXZ(agents)
	s.projectObstaclesXZ(obstacles)
	s.vels2D = s.Solve(s.vels2D, s.agents2D, s.obst2D, dt)
	for i := range n {
		dst[i] = Vec3{X: s.vels2D[i].X, Y: 0, Z: s.vels2D[i].Y}
	}
	return dst
}

// StepXZ computes collision-free XZ-plane velocities for all 3D agents and integrates Pos.X
// and Pos.Z by dt, preserving Pos.Y and zeroing Vel.Y.
func (s *ORCASolver) StepXZ(agents []ORCAAgent3D, obstacles []ORCAObstacle3D, dt time.Duration) {
	if s == nil {
		var tmp ORCASolver
		tmp.StepXZ(agents, obstacles, dt)
		return
	}
	if len(agents) == 0 || dt <= 0 {
		return
	}
	secs := float32(dt.Seconds())
	s.step3D = s.SolveXZ(s.step3D, agents, obstacles, dt)
	for i := range agents {
		if agents[i].Stationary || !(agents[i].MaxSpeed > 0) {
			agents[i].Vel = Vec3{}
			continue
		}
		v := s.step3D[i]
		agents[i].Vel = v
		agents[i].Pos.X += v.X * secs
		agents[i].Pos.Z += v.Z * secs
	}
}

func agent3DTo2D(a ORCAAgent3D) ORCAAgent {
	return ORCAAgent{
		ID:         a.ID,
		Pos:        math2d.Vec2{X: a.Pos.X, Y: a.Pos.Z},
		Vel:        math2d.Vec2{X: a.Vel.X, Y: a.Vel.Z},
		PrefVel:    math2d.Vec2{X: a.PrefVel.X, Y: a.PrefVel.Z},
		Radius:     a.Radius,
		MaxSpeed:   a.MaxSpeed,
		Priority:   a.Priority,
		Stationary: a.Stationary,
		Layer:      a.Layer,
		Mask:       a.Mask,
	}
}

func (s *ORCASolver) projectAgentsXZ(agents []ORCAAgent3D) {
	n := len(agents)
	if cap(s.agents2D) < n {
		s.agents2D = make([]ORCAAgent, n)
	} else {
		s.agents2D = s.agents2D[:n]
	}
	for i := range agents {
		s.agents2D[i] = agent3DTo2D(agents[i])
	}
}

func (s *ORCASolver) projectObstaclesXZ(obstacles []ORCAObstacle3D) {
	n := len(obstacles)
	if cap(s.obst2D) < n {
		s.obst2D = make([]ORCAObstacle, n)
	} else {
		s.obst2D = s.obst2D[:n]
	}
	for i := range obstacles {
		o := obstacles[i]
		s.obst2D[i] = ORCAObstacle{
			A:     math2d.Vec2{X: o.A.X, Y: o.A.Z},
			B:     math2d.Vec2{X: o.B.X, Y: o.B.Z},
			Layer: o.Layer,
		}
	}
}

// --- Spatial hash grid ---

func (s *ORCASolver) initBuckets(minBuckets int) {
	size := nextPow2(max(minBuckets, 64))
	s.buckets = make([]int32, size)
	for i := range s.buckets {
		s.buckets[i] = -1
	}
	s.bucketStamp = make([]uint32, size)
	s.mask = uint32(size - 1)
	s.touched = s.touched[:0]
	s.queryStamp = 0
}

func (s *ORCASolver) rebuildGrid(agents []ORCAAgent, cellSize float64) {
	s.cellSize = cellSize
	s.invCellSize = 1.0 / cellSize
	needBuckets := len(agents) * 2
	if len(s.buckets) == 0 || needBuckets > len(s.buckets) {
		s.initBuckets(needBuckets)
	} else {
		for _, b := range s.touched {
			s.buckets[b] = -1
		}
		s.touched = s.touched[:0]
	}
	if cap(s.next) < len(agents) {
		s.next = make([]int32, len(agents))
	} else {
		s.next = s.next[:len(agents)]
	}
	if cap(s.touched) < len(agents) {
		s.touched = make([]int32, 0, len(agents))
	}
	for i := len(agents) - 1; i >= 0; i-- {
		p := sanitizeVec2(agents[i].Pos)
		cx := int(math.Floor(p.x * s.invCellSize))
		cy := int(math.Floor(p.y * s.invCellSize))
		b := hashCell2D(cx, cy, s.mask)
		head := s.buckets[b]
		if head == -1 {
			s.touched = append(s.touched, int32(b))
		}
		s.next[i] = head
		s.buckets[b] = int32(i)
	}
}

func (s *ORCASolver) queryNeighbors(selfIdx int, pos vec2, mask uint32, radius float64, maxNeighbors int, agents []ORCAAgent) {
	s.candidates = s.candidates[:0]
	if len(agents) <= 1 || maxNeighbors <= 0 {
		return
	}
	rSq := radius * radius
	cellSize := s.cellSize
	overflowCoords := math.Abs(pos.x)*s.invCellSize > 1e15 ||
		math.Abs(pos.y)*s.invCellSize > 1e15 ||
		radius*s.invCellSize > 1e15
	minCX := int(math.Floor((pos.x - radius) * s.invCellSize))
	maxCX := int(math.Floor((pos.x + radius) * s.invCellSize))
	minCY := int(math.Floor((pos.y - radius) * s.invCellSize))
	maxCY := int(math.Floor((pos.y + radius) * s.invCellSize))

	spanX := maxCX - minCX + 1
	spanY := maxCY - minCY + 1
	if overflowCoords || spanX <= 0 || spanY <= 0 || spanX > len(agents) || spanY > len(agents) || spanX*spanY > len(agents) {
		for j := range agents {
			if j == selfIdx {
				continue
			}
			o := &agents[j]
			if mask&effectiveAgentLayer(o.Layer) == 0 {
				continue
			}
			dSq := distSq(pos, sanitizeVec2(o.Pos))
			if dSq > rSq || math.IsNaN(dSq) {
				continue
			}
			if len(s.candidates) == maxNeighbors && dSq > s.candidates[0].distSq {
				continue
			}
			s.pushCandidate(orcaCandidate{idx: int32(j), distSq: dSq}, maxNeighbors, agents)
		}
		s.sortCandidates(agents)
		return
	}

	s.queryStamp++
	if s.queryStamp == 0 {
		clear(s.bucketStamp)
		s.queryStamp = 1
	}
	stamp := s.queryStamp

	for cy := minCY; cy <= maxCY; cy++ {
		// Closest Y coordinate in cell row cy to pos.y.
		cellMinY := float64(cy) * cellSize
		cellMaxY := cellMinY + cellSize
		var dyCell float64
		if pos.y < cellMinY {
			dyCell = cellMinY - pos.y
		} else if pos.y > cellMaxY {
			dyCell = pos.y - cellMaxY
		}
		dyCellSq := dyCell * dyCell
		if dyCellSq > rSq {
			continue
		}
		for cx := minCX; cx <= maxCX; cx++ {
			cellMinX := float64(cx) * cellSize
			cellMaxX := cellMinX + cellSize
			var dxCell float64
			if pos.x < cellMinX {
				dxCell = cellMinX - pos.x
			} else if pos.x > cellMaxX {
				dxCell = pos.x - cellMaxX
			}
			if dxCell*dxCell+dyCellSq > rSq {
				continue
			}
			b := hashCell2D(cx, cy, s.mask)
			if s.bucketStamp[b] == stamp {
				continue
			}
			s.bucketStamp[b] = stamp
			for idx := s.buckets[b]; idx != -1; idx = s.next[idx] {
				j := int(idx)
				if j == selfIdx {
					continue
				}
				o := &agents[j]
				if mask&effectiveAgentLayer(o.Layer) == 0 {
					continue
				}
				dSq := distSq(pos, sanitizeVec2(o.Pos))
				if dSq > rSq || math.IsNaN(dSq) {
					continue
				}
				if len(s.candidates) == maxNeighbors && dSq > s.candidates[0].distSq {
					continue
				}
				s.pushCandidate(orcaCandidate{idx: idx, distSq: dSq}, maxNeighbors, agents)
			}
		}
	}
	s.sortCandidates(agents)
}

// candidateLess defines a strict, input-order-independent ordering for neighbor candidates:
// nearest distance first, breaking ties purely on intrinsic agent properties.
func candidateLess(a, b orcaCandidate, agents []ORCAAgent) bool {
	if c := cmp.Compare(a.distSq, b.distSq); c != 0 {
		return c < 0
	}
	return agentCanonicalLess(agents[a.idx], agents[b.idx])
}

func agentCanonicalLess(a, b ORCAAgent) bool {
	if c := cmp.Compare(a.ID, b.ID); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Pos.X, b.Pos.X); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Pos.Y, b.Pos.Y); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Vel.X, b.Vel.X); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Vel.Y, b.Vel.Y); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Radius, b.Radius); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Priority, b.Priority); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.MaxSpeed, b.MaxSpeed); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Layer, b.Layer); c != 0 {
		return c < 0
	}
	if c := cmp.Compare(a.Mask, b.Mask); c != 0 {
		return c < 0
	}
	if a.Stationary != b.Stationary {
		return !a.Stationary && b.Stationary
	}
	if c := cmp.Compare(a.PrefVel.X, b.PrefVel.X); c != 0 {
		return c < 0
	}
	return cmp.Compare(a.PrefVel.Y, b.PrefVel.Y) < 0
}

// pushCandidate maintains a bounded max-heap of the maxK smallest candidates under candidateLess,
// guaranteeing selection is independent of iteration order with O(maxK) working memory.
func (s *ORCASolver) pushCandidate(c orcaCandidate, maxK int, agents []ORCAAgent) {
	if maxK <= 0 {
		return
	}
	n := len(s.candidates)
	if n < maxK {
		s.candidates = append(s.candidates, c)
		// Sift up in max-heap (largest at root 0).
		i := n
		for i > 0 {
			parent := (i - 1) >> 1
			if !candidateLess(s.candidates[parent], s.candidates[i], agents) {
				break
			}
			s.candidates[parent], s.candidates[i] = s.candidates[i], s.candidates[parent]
			i = parent
		}
		return
	}
	// Replace root only if c is strictly better than the current worst candidate.
	if !candidateLess(c, s.candidates[0], agents) {
		return
	}
	s.candidates[0] = c
	// Sift down in max-heap.
	i := 0
	for {
		left := 2*i + 1
		if left >= maxK {
			break
		}
		best := left
		if right := left + 1; right < maxK && candidateLess(s.candidates[left], s.candidates[right], agents) {
			best = right
		}
		if !candidateLess(s.candidates[i], s.candidates[best], agents) {
			break
		}
		s.candidates[i], s.candidates[best] = s.candidates[best], s.candidates[i]
		i = best
	}
}

func (s *ORCASolver) sortCandidates(agents []ORCAAgent) {
	n := len(s.candidates)
	if n <= 1 {
		return
	}
	if n <= 32 {
		for i := 1; i < n; i++ {
			cur := s.candidates[i]
			j := i - 1
			for j >= 0 && candidateLess(cur, s.candidates[j], agents) {
				s.candidates[j+1] = s.candidates[j]
				j--
			}
			s.candidates[j+1] = cur
		}
		return
	}
	slices.SortFunc(s.candidates, func(a, b orcaCandidate) int {
		if candidateLess(a, b, agents) {
			return -1
		}
		if candidateLess(b, a, agents) {
			return 1
		}
		return 0
	})
}

// --- Static obstacles ---

func (s *ORCASolver) buildObstacleBounds(obstacles []ORCAObstacle) {
	n := len(obstacles)
	if cap(s.obstBounds) < n {
		s.obstBounds = make([]orcaObstBounds, n)
	} else {
		s.obstBounds = s.obstBounds[:n]
	}
	for i := range obstacles {
		a := sanitizeVec2(obstacles[i].A)
		b := sanitizeVec2(obstacles[i].B)
		s.obstBounds[i] = orcaObstBounds{
			minX:  min(a.x, b.x),
			minY:  min(a.y, b.y),
			maxX:  max(a.x, b.x),
			maxY:  max(a.y, b.y),
			layer: effectiveObstacleLayer(obstacles[i].Layer),
		}
	}
}

func (s *ORCASolver) collectObstacles(pos vec2, radius, maxSpeed float64, agentMask uint32, timeHorizonObst float64, obstacles []ORCAObstacle, bounds []orcaObstBounds) {
	s.obstCands = s.obstCands[:0]
	if len(obstacles) == 0 {
		return
	}
	reach := radius + maxSpeed*timeHorizonObst
	reachSq := reach * reach
	useBounds := len(bounds) == len(obstacles)

	for i := range obstacles {
		var layer uint32
		if useBounds {
			b := bounds[i]
			layer = b.layer
			if agentMask&layer == 0 {
				continue
			}
			if pos.x+reach < b.minX || pos.x-reach > b.maxX || pos.y+reach < b.minY || pos.y-reach > b.maxY {
				continue
			}
		} else {
			layer = effectiveObstacleLayer(obstacles[i].Layer)
			if agentMask&layer == 0 {
				continue
			}
		}
		a := sanitizeVec2(obstacles[i].A)
		b := sanitizeVec2(obstacles[i].B)
		// Canonicalize endpoint order (A <= B) so reversing an obstacle segment gives bit-identical results.
		if b.x < a.x || (b.x == a.x && b.y < a.y) {
			a, b = b, a
		}
		p1 := a.sub(pos)
		p2 := b.sub(pos)
		closest := closestPointOnSegment(vec2{}, p1, p2)
		dSq := closest.lenSq()
		if dSq > reachSq {
			continue
		}
		s.obstCands = append(s.obstCands, orcaObstCandidate{
			distSq: dSq,
			ax:     a.x,
			ay:     a.y,
			bx:     b.x,
			by:     b.y,
			layer:  layer,
		})
	}
	if len(s.obstCands) > 1 {
		slices.SortFunc(s.obstCands, func(a, b orcaObstCandidate) int {
			if a.distSq != b.distSq {
				if a.distSq < b.distSq {
					return -1
				}
				return 1
			}
			if a.ax != b.ax {
				if a.ax < b.ax {
					return -1
				}
				return 1
			}
			if a.ay != b.ay {
				if a.ay < b.ay {
					return -1
				}
				return 1
			}
			if a.bx != b.bx {
				if a.bx < b.bx {
					return -1
				}
				return 1
			}
			if a.by != b.by {
				if a.by < b.by {
					return -1
				}
				return 1
			}
			if a.layer < b.layer {
				return -1
			}
			if a.layer > b.layer {
				return 1
			}
			return 0
		})
	}
}

// --- Core ORCA half-plane construction & 2D LP ---

func (s *ORCASolver) solveFromCandidates(agent ORCAAgent, pool []ORCAAgent, dtSecs float64, cfg ORCAConfig) math2d.Vec2 {
	aPos := sanitizeVec2(agent.Pos)
	aVel := sanitizeVec2(agent.Vel)
	prefVel := sanitizeVec2(agent.PrefVel)
	aRadius := sanitizeNonNeg32(agent.Radius)
	maxSpeed := sanitizeNonNeg32(agent.MaxSpeed)
	aLayer := effectiveAgentLayer(agent.Layer)

	invTimeHorizon := 1.0 / float64(cfg.TimeHorizon)
	invTimeHorizonObst := 1.0 / float64(cfg.TimeHorizonObst)
	invTimeStep := 1.0 / dtSecs

	s.lines = s.lines[:0]

	// 1. Static obstacle half-planes come first so they act as hard constraints during LP3 fallback.
	for i := range s.obstCands {
		oc := s.obstCands[i]
		p1 := vec2{oc.ax - aPos.x, oc.ay - aPos.y}
		p2 := vec2{oc.bx - aPos.x, oc.by - aPos.y}
		if line, ok := buildObstacleORCALine(p1, p2, aVel, aRadius, invTimeHorizonObst, invTimeStep); ok {
			s.lines = append(s.lines, line)
		}
	}
	numObstLines := len(s.lines)

	// 2. Agent-agent ORCA half-planes in canonical order.
	for i := range s.candidates {
		other := pool[s.candidates[i].idx]
		oPos := sanitizeVec2(other.Pos)
		oVel := sanitizeVec2(other.Vel)
		oRadius := sanitizeNonNeg32(other.Radius)
		oMaxSpeed := sanitizeNonNeg32(other.MaxSpeed)

		cooperative := !other.Stationary && oMaxSpeed > 0 && (effectiveAgentMask(other.Mask)&aLayer) != 0
		line := buildAgentORCALine(
			agent, other,
			aPos, aVel, aRadius,
			oPos, oVel, oRadius,
			cooperative,
			invTimeHorizon, invTimeStep,
		)
		s.lines = append(s.lines, line)
	}

	result, failedLine := linearProgram2(s.lines, maxSpeed, prefVel, false)
	if failedLine < len(s.lines) {
		hardObst := numObstLines
		if failedLine < numObstLines {
			// Even the obstacle constraints alone were mutually infeasible; relax from 0 to minimize penetration.
			hardObst = 0
		}
		result, s.projLines = linearProgram3(s.lines, hardObst, failedLine, maxSpeed, result, s.projLines)
	}
	result = clampVec2(result, maxSpeed)
	return math2d.Vec2{X: float32(result.x), Y: float32(result.y)}
}

func buildObstacleORCALine(p1, p2, agentVel vec2, radius, invTauObst, invTimeStep float64) (orcaLine, bool) {
	// Orient segment so origin (agent) lies on the left side (det(p1, p2) >= 0).
	if p1.cross(p2) < 0 {
		p1, p2 = p2, p1
	}

	seg := p2.sub(p1)
	segLenSq := seg.lenSq()
	if segLenSq <= 1e-18 {
		return buildStaticDiskORCALine(p1, agentVel, radius, invTauObst, invTimeStep), true
	}

	segLen := math.Sqrt(segLenSq)
	uSeg := seg.scale(1 / segLen)
	nSeg := vec2{-uSeg.y, uSeg.x} // points toward agent (left of p1 -> p2)

	tClosest := (-p1.x*seg.x - p1.y*seg.y) / segLenSq
	tClosest = min(max(tClosest, 0), 1)
	closest := p1.add(seg.scale(tClosest))
	cDistSq := closest.lenSq()

	rSq := radius * radius
	if cDistSq <= rSq {
		// Currently touching or penetrating the static segment: enforce outward clearance velocity.
		var n vec2
		var dist float64
		if cDistSq > 1e-18 {
			dist = math.Sqrt(cDistSq)
			n = vec2{-closest.x / dist, -closest.y / dist}
		} else {
			n = nSeg
			dist = 0
		}
		pushSpeed := (radius - dist) * invTimeStep
		return orcaLine{
			point: n.scale(pushSpeed),
			dir:   vec2{n.y, -n.x},
		}, true
	}

	// If agent is collinear with the segment (det(p1, p2) ~ 0) and outside it, the nearer endpoint
	// shadows the rest of the segment.
	if p1.cross(p2) <= 1e-12 {
		near := p1
		if p2.lenSq() < p1.lenSq() {
			near = p2
		}
		return buildStaticDiskORCALine(near, agentVel, radius, invTauObst, invTimeStep), true
	}

	d1Sq := p1.lenSq()
	d2Sq := p2.lenSq()
	leg1 := math.Sqrt(max(d1Sq-rSq, 0))
	leg2 := math.Sqrt(max(d2Sq-rSq, 0))

	// Outer tangents from origin to B(p1, radius) (CW past p1) and B(p2, radius) (CCW past p2).
	legDir1 := vec2{p1.x*leg1 + p1.y*radius, -p1.x*radius + p1.y*leg1}.scale(1 / d1Sq)
	legDir2 := vec2{p2.x*leg2 - p2.y*radius, p2.x*radius + p2.y*leg2}.scale(1 / d2Sq)

	leftCutoff := p1.scale(invTauObst)
	rightCutoff := p2.scale(invTauObst)
	cutoffVec := rightCutoff.sub(leftCutoff)
	cutoffLenSq := cutoffVec.lenSq()
	if cutoffLenSq <= 1e-30 {
		near := p1
		if d2Sq < d1Sq {
			near = p2
		}
		return buildStaticDiskORCALine(near, agentVel, radius, invTauObst, invTimeStep), true
	}
	rCut := radius * invTauObst

	vRef := agentVel
	t := vRef.sub(leftCutoff).dot(cutoffVec) / cutoffLenSq
	tLeg1 := vRef.sub(leftCutoff).dot(legDir1)
	tLeg2 := vRef.sub(rightCutoff).dot(legDir2)

	if t < 0 && tLeg1 < 0 {
		w := vRef.sub(leftCutoff)
		n := normalizeOr(w, nSeg.add(vec2{legDir1.y, -legDir1.x}))
		return orcaLine{
			point: leftCutoff.add(n.scale(rCut)),
			dir:   vec2{n.y, -n.x},
		}, true
	}
	if t > 1 && tLeg2 < 0 {
		w := vRef.sub(rightCutoff)
		n := normalizeOr(w, nSeg.add(vec2{-legDir2.y, legDir2.x}))
		return orcaLine{
			point: rightCutoff.add(n.scale(rCut)),
			dir:   vec2{n.y, -n.x},
		}, true
	}

	distSqCutoff := math.Inf(1)
	if t >= 0 && t <= 1 {
		cutoffPt := leftCutoff.add(cutoffVec.scale(t)).add(nSeg.scale(rCut))
		distSqCutoff = distSq(vRef, cutoffPt)
	}
	distSqLeg1 := math.Inf(1)
	if tLeg1 >= 0 {
		leg1Pt := legDir1.scale(vRef.dot(legDir1))
		distSqLeg1 = distSq(vRef, leg1Pt)
	}
	distSqLeg2 := math.Inf(1)
	if tLeg2 >= 0 {
		leg2Pt := legDir2.scale(vRef.dot(legDir2))
		distSqLeg2 = distSq(vRef, leg2Pt)
	}

	if distSqCutoff <= distSqLeg1 && distSqCutoff <= distSqLeg2 {
		return orcaLine{
			point: leftCutoff.add(nSeg.scale(rCut)),
			dir:   uSeg,
		}, true
	}
	if distSqLeg1 <= distSqLeg2 {
		return orcaLine{
			point: vec2{},
			dir:   vec2{-legDir1.x, -legDir1.y},
		}, true
	}
	return orcaLine{
		point: vec2{},
		dir:   legDir2,
	}, true
}

func buildStaticDiskORCALine(relPos, agentVel vec2, radius, invTauObst, invTimeStep float64) orcaLine {
	dSq := relPos.lenSq()
	rSq := radius * radius
	if dSq <= rSq {
		var n vec2
		var d float64
		if dSq > 1e-18 {
			d = math.Sqrt(dSq)
			n = vec2{-relPos.x / d, -relPos.y / d}
		} else {
			n = vec2{1, 0}
		}
		return orcaLine{
			point: n.scale((radius - d) * invTimeStep),
			dir:   vec2{n.y, -n.x},
		}
	}

	w := agentVel.sub(relPos.scale(invTauObst))
	wLenSq := w.lenSq()
	dot1 := w.dot(relPos)
	if dot1 < 0 && dot1*dot1 > rSq*wLenSq || wLenSq <= orcaEps*orcaEps {
		var unitW vec2
		var wLen float64
		if wLenSq > orcaEps*orcaEps {
			wLen = math.Sqrt(wLenSq)
			unitW = w.scale(1 / wLen)
		} else {
			d := math.Sqrt(dSq)
			unitW = vec2{-relPos.x / d, -relPos.y / d}
		}
		lineDir := vec2{unitW.y, -unitW.x}
		u := unitW.scale(radius*invTauObst - wLen)
		return orcaLine{point: agentVel.add(u), dir: lineDir}
	}

	leg := math.Sqrt(dSq - rSq)
	var lineDir vec2
	if relPos.cross(w) > 0 {
		lineDir = vec2{relPos.x*leg - relPos.y*radius, relPos.x*radius + relPos.y*leg}.scale(1 / dSq)
	} else {
		lineDir = vec2{-(relPos.x*leg + relPos.y*radius), -(-relPos.x*radius + relPos.y*leg)}.scale(1 / dSq)
	}
	return orcaLine{point: vec2{}, dir: lineDir}
}

func buildAgentORCALine(
	agent, other ORCAAgent,
	aPos, aVel vec2, aRadius float64,
	oPos, oVel vec2, oRadius float64,
	cooperative bool,
	invTimeHorizon, invTimeStep float64,
) orcaLine {
	relPos := oPos.sub(aPos)
	relVel := aVel.sub(oVel)
	dSq := relPos.lenSq()
	combinedRadius := aRadius + oRadius
	combinedRadiusSq := combinedRadius * combinedRadius

	var lineDir vec2
	var u vec2

	if dSq > combinedRadiusSq {
		// No collision currently.
		w := relVel.sub(relPos.scale(invTimeHorizon))
		wLenSq := w.lenSq()
		dot1 := w.dot(relPos)

		if (dot1 < 0 && dot1*dot1 > combinedRadiusSq*wLenSq) || wLenSq <= orcaEps*orcaEps {
			// Project onto cutoff circle.
			d := math.Sqrt(dSq)
			pHat := relPos.scale(1 / d)
			cutoffR := combinedRadius * invTimeHorizon

			var wLen float64
			var unitW vec2
			if wLenSq > orcaEps*orcaEps {
				wLen = math.Sqrt(wLenSq)
				unitW = w.scale(1 / wLen)
			} else {
				unitW = vec2{-pHat.x, -pHat.y}
			}

			// Deterministic right-hand symmetry breaking for nearly head-on approaches inside
			// the cutoff circle: tilt the supporting tangent slightly toward the agent's right
			// (which is the exact opposite world side for the facing neighbor), staying strictly
			// within the valid cutoff arc so the half-plane remains disjoint from the VO cone.
			right := vec2{pHat.y, -pHat.x}
			back := vec2{-pHat.x, -pHat.y}
			lat := unitW.dot(right)
			backDot := unitW.dot(back)
			if wLen <= cutoffR+1e-6 && (wLen <= orcaEps || (backDot > 0.85 && lat > -0.05 && lat < 0.25)) {
				maxArc := math.Acos(min(combinedRadius/d, 1.0))
				biasAngle := min(0.25, 0.5*maxArc)
				if biasAngle > 1e-4 {
					sn, cs := math.Sincos(biasAngle)
					n := vec2{back.x*cs + right.x*sn, back.y*cs + right.y*sn}
					lineDir = vec2{n.y, -n.x}
					u = n.scale(cutoffR - w.dot(n))
				} else {
					lineDir = vec2{unitW.y, -unitW.x}
					u = unitW.scale(cutoffR - wLen)
				}
			} else {
				lineDir = vec2{unitW.y, -unitW.x}
				u = unitW.scale(cutoffR - wLen)
			}
		} else {
			// Project onto left or right leg of the velocity obstacle cone.
			leg := math.Sqrt(dSq - combinedRadiusSq)
			if relPos.cross(w) > 0 {
				lineDir = vec2{
					relPos.x*leg - relPos.y*combinedRadius,
					relPos.x*combinedRadius + relPos.y*leg,
				}.scale(1 / dSq)
			} else {
				lineDir = vec2{
					-(relPos.x*leg + relPos.y*combinedRadius),
					-(-relPos.x*combinedRadius + relPos.y*leg),
				}.scale(1 / dSq)
			}
			dot2 := relVel.dot(lineDir)
			u = lineDir.scale(dot2).sub(relVel)
		}
	} else {
		// Currently overlapping: resolve penetration over one time step dt.
		w := relVel.sub(relPos.scale(invTimeStep))
		wLen := w.len()
		var unitW vec2
		if wLen > orcaEps {
			unitW = w.scale(1 / wLen)
		} else if dSq > orcaEps*orcaEps {
			d := math.Sqrt(dSq)
			unitW = vec2{-relPos.x / d, -relPos.y / d}
		} else {
			unitW = coincidentSeparationDir(agent, other)
		}
		lineDir = vec2{unitW.y, -unitW.x}
		u = unitW.scale(combinedRadius*invTimeStep - wLen)
	}

	var factor float64
	if !cooperative {
		factor = 1.0
	} else {
		resp := orcaResponsibility(agent.Priority, other.Priority)
		normal := vec2{-lineDir.y, lineDir.x}
		if u.dot(normal) >= 0 {
			// Inside or on VO boundary: lower-priority agent takes more responsibility.
			factor = resp
		} else {
			// Outside VO (slack): higher-priority agent gets more slack, clamped symmetrically
			// to [0.1, 0.9] so the pair's factors still sum to 1 while low-priority agents
			// can still accelerate smoothly from rest.
			factor = min(max(1.0-resp, 0.1), 0.9)
		}
	}

	return orcaLine{
		point: aVel.add(u.scale(factor)),
		dir:   lineDir,
	}
}

// coincidentSeparationDir returns an antisymmetric unit direction for two agents occupying
// the exact same position and velocity, independent of input slice order.
func coincidentSeparationDir(a, b ORCAAgent) vec2 {
	if a.ID != b.ID {
		lo, hi := a.ID, b.ID
		sign := 1.0
		if a.ID > b.ID {
			lo, hi = b.ID, a.ID
			sign = -1.0
		}
		// Deterministic hash of the unordered pair (lo, hi) onto the unit circle.
		h := lo*0x9e3779b97f4a7c15 ^ hi*0xbf58476d1ce4e5b9
		h ^= h >> 30
		h *= 0xbf58476d1ce4e5b9
		h ^= h >> 27
		angle := (float64(h&0xFFFF) / 65536.0) * 2 * math.Pi
		sn, cs := math.Sincos(angle)
		return vec2{cs * sign, sn * sign}
	}
	if a.PrefVel.X != b.PrefVel.X {
		if a.PrefVel.X < b.PrefVel.X {
			return vec2{-1, 0}
		}
		return vec2{1, 0}
	}
	if a.PrefVel.Y != b.PrefVel.Y {
		if a.PrefVel.Y < b.PrefVel.Y {
			return vec2{0, -1}
		}
		return vec2{0, 1}
	}
	return vec2{1, 0}
}

func normalizeOr(v, fallback vec2) vec2 {
	l := v.len()
	if l > orcaEps {
		return v.scale(1 / l)
	}
	fl := fallback.len()
	if fl > orcaEps {
		return fallback.scale(1 / fl)
	}
	return vec2{1, 0}
}

// linearProgram1 solves the 1D LP along lines[lineNo] intersected with the speed circle and
// previous half-planes lines[0..lineNo-1].
func linearProgram1(lines []orcaLine, lineNo int, radius float64, optVel vec2, directionOpt bool) (vec2, bool) {
	line := lines[lineNo]
	dotProduct := line.point.dot(line.dir)
	discriminant := dotProduct*dotProduct + radius*radius - line.point.lenSq()
	if discriminant < 0 {
		return vec2{}, false
	}
	sqrtDisc := math.Sqrt(discriminant)
	tLeft := -dotProduct - sqrtDisc
	tRight := -dotProduct + sqrtDisc

	for i := range lineNo {
		denom := line.dir.cross(lines[i].dir)
		numer := lines[i].dir.cross(line.point.sub(lines[i].point))
		if math.Abs(denom) <= orcaEps {
			if numer < -orcaEps {
				return vec2{}, false
			}
			continue
		}
		t := numer / denom
		if denom > 0 {
			tRight = min(tRight, t)
		} else {
			tLeft = max(tLeft, t)
		}
		if tLeft > tRight+orcaEps {
			return vec2{}, false
		}
	}
	if tLeft > tRight {
		mid := 0.5 * (tLeft + tRight)
		tLeft, tRight = mid, mid
	}

	if directionOpt {
		if optVel.dot(line.dir) > 0 {
			return line.point.add(line.dir.scale(tRight)), true
		}
		return line.point.add(line.dir.scale(tLeft)), true
	}
	t := line.dir.dot(optVel.sub(line.point))
	t = min(max(t, tLeft), tRight)
	return line.point.add(line.dir.scale(t)), true
}

// linearProgram2 solves the 2D LP over all lines within the speed disk of radius.
// Returns the solution and len(lines) on success, or the partial solution and the first failing line index.
func linearProgram2(lines []orcaLine, radius float64, optVel vec2, directionOpt bool) (vec2, int) {
	var result vec2
	if directionOpt {
		result = optVel.scale(radius)
	} else if optVel.lenSq() > radius*radius {
		result = optVel.scale(radius / optVel.len())
	} else {
		result = optVel
	}

	for i := range lines {
		if lines[i].dir.cross(result.sub(lines[i].point)) < -orcaEps {
			tempResult := result
			res, ok := linearProgram1(lines, i, radius, optVel, directionOpt)
			if !ok {
				return tempResult, i
			}
			result = res
		}
	}
	return result, len(lines)
}

// linearProgram3 is the RVO2 3D-projection fallback when linearProgram2 fails at beginLine:
// it finds the velocity within radius minimizing maximum penetration across agent constraints
// while respecting lines[:numObstLines] as hard constraints.
func linearProgram3(lines []orcaLine, numObstLines, beginLine int, radius float64, result vec2, projBuf []orcaLine) (vec2, []orcaLine) {
	distance := 0.0
	for i := beginLine; i < len(lines); i++ {
		penetration := lines[i].dir.cross(lines[i].point.sub(result))
		if penetration <= distance+orcaEps {
			continue
		}
		projBuf = append(projBuf[:0], lines[:numObstLines]...)
		for j := numObstLines; j < i; j++ {
			det := lines[i].dir.cross(lines[j].dir)
			var pt vec2
			if math.Abs(det) <= orcaEps {
				if lines[i].dir.dot(lines[j].dir) > 0 {
					continue
				}
				pt = lines[i].point.add(lines[j].point).scale(0.5)
			} else {
				t := lines[j].dir.cross(lines[i].point.sub(lines[j].point)) / det
				pt = lines[i].point.add(lines[i].dir.scale(t))
			}
			diff := lines[j].dir.sub(lines[i].dir)
			dl := diff.len()
			if dl <= orcaEps {
				continue
			}
			projBuf = append(projBuf, orcaLine{
				point: pt,
				dir:   diff.scale(1 / dl),
			})
		}

		// Maximize alignment with lines[i]'s inward normal (-dir.y, dir.x).
		optDir := vec2{-lines[i].dir.y, lines[i].dir.x}
		if res, fail := linearProgram2(projBuf, radius, optDir, true); fail >= len(projBuf) {
			result = res
		}
		distance = lines[i].dir.cross(lines[i].point.sub(result))
	}
	return result, projBuf
}

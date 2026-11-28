package pathfind

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// NavAgentState reports the navigation status of a 2D NavAgent.
type NavAgentState uint8

const (
	// NavAgentIdle indicates the agent has no active navigation target.
	NavAgentIdle NavAgentState = iota
	// NavAgentMoving indicates the agent is actively following its polygon corridor toward its target.
	NavAgentMoving
	// NavAgentArrived indicates the agent reached its target within StoppingDistance.
	NavAgentArrived
	// NavAgentUnreachable indicates no valid corridor connects the agent to its target.
	NavAgentUnreachable
)

// String returns a human-readable state name.
func (s NavAgentState) String() string {
	switch s {
	case NavAgentIdle:
		return "idle"
	case NavAgentMoving:
		return "moving"
	case NavAgentArrived:
		return "arrived"
	case NavAgentUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// NavLinkEvent describes a flagged shared-edge or off-mesh link transition encountered by a NavAgent.
type NavLinkEvent struct {
	FromPoly int
	ToPoly   int
	Start    core.Point
	End      core.Point
	Radius   float32
	Area     NavArea
	Flags    uint32
	LinkID   int
	UserData any
	OffMesh  bool
}

// NavAgentConfig configures steering, arrival, corridor repair, replan throttling, and off-mesh link traversal for a 2D NavAgent.
type NavAgentConfig struct {
	// MaxSpeed is the maximum movement speed in world units per second. Defaults to 100 when <= 0.
	MaxSpeed float32
	// MaxAcceleration caps how fast velocity changes in world units per second squared.
	// Defaults to 4 * MaxSpeed when <= 0.
	MaxAcceleration float32
	// Radius is the agent's body radius in world units. Defaults to 0.5 when <= 0.
	Radius float32
	// StoppingDistance is the distance from the goal within which the agent counts as arrived.
	// Defaults to max(Radius, 0.25) when <= 0.
	StoppingDistance float32
	// ArrivalRadius is the distance from the goal inside which the agent slows down smoothly.
	// Defaults to max(StoppingDistance*3, MaxSpeed*MaxSpeed/(2*MaxAcceleration)*2) when <= 0.
	ArrivalRadius float32
	// ReplanInterval is the minimum simulation duration between automatic full A* replans.
	// Zero allows immediate replanning whenever the corridor becomes invalid.
	ReplanInterval time.Duration
	// MaxRepairDepth is the maximum number of adjacent bridge polygons searched locally when
	// repairing a displaced corridor start or moved target before falling back to a full A* replan.
	// Defaults to 2 when <= 0.
	MaxRepairDepth int
	// Filter optionally restricts which polygons this agent may traverse and sets per-area costs.
	// When nil, DefaultNavFilter() is used (all walkable non-disabled polygons at unit cost).
	Filter *NavFilter
	// OnLink is an optional callback invoked on each step while traversing an off-mesh link or
	// crossing a flagged shared-edge portal. Return true when the transition is complete; return
	// false to keep control of the agent on subsequent steps. When nil, off-mesh links traverse
	// linearly at MaxSpeed and flagged shared-edge portals are crossed automatically.
	OnLink func(agent *NavAgent, link NavLinkEvent, dt time.Duration) bool
}

func sanitizeNavAgentConfig(cfg NavAgentConfig) NavAgentConfig {
	if !isFinite32(cfg.MaxSpeed) || cfg.MaxSpeed <= 0 {
		cfg.MaxSpeed = 100
	}
	if !isFinite32(cfg.MaxAcceleration) || cfg.MaxAcceleration <= 0 {
		cfg.MaxAcceleration = 4 * cfg.MaxSpeed
	}
	if !isFinite32(cfg.Radius) || cfg.Radius <= 0 {
		cfg.Radius = 0.5
	}
	if !isFinite32(cfg.StoppingDistance) || cfg.StoppingDistance <= 0 {
		cfg.StoppingDistance = max(cfg.Radius, 0.25)
	}
	if !isFinite32(cfg.ArrivalRadius) || cfg.ArrivalRadius <= 0 {
		brakeDist := (cfg.MaxSpeed * cfg.MaxSpeed) / (2 * cfg.MaxAcceleration)
		cfg.ArrivalRadius = max(cfg.StoppingDistance*3, brakeDist*2)
	}
	if cfg.ArrivalRadius < cfg.StoppingDistance {
		cfg.ArrivalRadius = cfg.StoppingDistance
	}
	if cfg.ReplanInterval < 0 {
		cfg.ReplanInterval = 0
	}
	if cfg.MaxRepairDepth <= 0 {
		cfg.MaxRepairDepth = 2
	}
	return cfg
}

func isFinite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func clampVec2(v vec2, maxLen float64) vec2 {
	if maxLen <= 0 {
		return v
	}
	l := v.len()
	if l > maxLen {
		return v.scale(maxLen / l)
	}
	return v
}

func isFiniteVec2(v vec2) bool {
	return !math.IsNaN(v.x) && !math.IsInf(v.x, 0) && !math.IsNaN(v.y) && !math.IsInf(v.y, 0)
}

// NavAgent navigates a 2D pathfind.NavMesh by maintaining an active polygon corridor,
// steering smoothly toward the next funnel corner, slowing cleanly to arrive,
// repairing its corridor locally when pushed or when the target shifts, and
// invoking callbacks for off-mesh or flagged links.
//
// NavAgent is deterministic on fixed game.Loop steps and is not safe for concurrent use.
type NavAgent struct {
	mesh         *NavMesh
	meshRevision uint64
	cfg          NavAgentConfig

	pos       vec2
	vel       vec2
	target    vec2
	hasTarget bool
	state     NavAgentState

	corridor       []int
	portals        []int
	tailExtensions int

	needsReplan    bool
	replanCooldown time.Duration
	replanCount    int
	localRepairs   int

	traversingLink bool
	activeLink     NavLinkEvent

	group *NavAgentGroup
	id    uint64
}

// NewNavAgent constructs a 2D navigation agent placed at pos on mesh.
func NewNavAgent(mesh *NavMesh, pos core.Point, cfg NavAgentConfig) *NavAgent {
	p := toVec2(pos)
	if !isFiniteVec2(p) {
		p = vec2{}
	}
	var rev uint64
	if mesh != nil {
		rev = mesh.Revision()
	}
	return &NavAgent{
		mesh:         mesh,
		meshRevision: rev,
		cfg:          sanitizeNavAgentConfig(cfg),
		pos:          p,
		state:        NavAgentIdle,
	}
}

// Config returns the sanitized configuration of the agent.
func (a *NavAgent) Config() NavAgentConfig { return a.cfg }

// Pos returns the agent's current 2D position.
func (a *NavAgent) Pos() core.Point { return toPoint(a.pos) }

// Vel returns the agent's current 2D velocity.
func (a *NavAgent) Vel() core.Point { return toPoint(a.vel) }

// State returns the agent's current navigation state.
func (a *NavAgent) State() NavAgentState { return a.state }

// Target returns the agent's current target position (zero when idle).
func (a *NavAgent) Target() core.Point { return toPoint(a.target) }

// HasTarget reports whether the agent currently has a target.
func (a *NavAgent) HasTarget() bool { return a.hasTarget }

// ReplanCount returns the total number of full A* replans performed by this agent.
func (a *NavAgent) ReplanCount() int { return a.replanCount }

// LocalRepairCount returns the number of cheap local corridor repairs performed without a full A* replan.
func (a *NavAgent) LocalRepairCount() int { return a.localRepairs }

// NeedsReplan reports whether the agent is waiting for a full A* replan.
func (a *NavAgent) NeedsReplan() bool { return a.needsReplan }

// TraversingLink returns the active link event while the agent is traversing an off-mesh or flagged link.
func (a *NavAgent) TraversingLink() (NavLinkEvent, bool) {
	if !a.traversingLink {
		return NavLinkEvent{}, false
	}
	return a.activeLink, true
}

// Corridor returns a copy of the agent's active polygon index corridor.
func (a *NavAgent) Corridor() []int {
	if len(a.corridor) == 0 {
		return nil
	}
	out := make([]int, len(a.corridor))
	copy(out, a.corridor)
	return out
}

// SetVelocity overrides the agent's current velocity. Non-finite values are ignored.
func (a *NavAgent) SetVelocity(v core.Point) {
	vv := toVec2(v)
	if !isFiniteVec2(vv) {
		return
	}
	a.vel = vv
}

// SetPosition updates the agent's position (for example after physics knockback or external push).
// When the agent has an active corridor and is not mid-link, it attempts cheap local corridor
// repair immediately without running a full A* replan.
func (a *NavAgent) SetPosition(pos core.Point) {
	p := toVec2(pos)
	if !isFiniteVec2(p) {
		return
	}
	a.pos = p
	if !a.hasTarget || a.traversingLink {
		return
	}
	if a.state == NavAgentArrived && dist(a.pos, a.target) > float64(a.cfg.StoppingDistance) {
		a.state = NavAgentMoving
	}
	if len(a.corridor) > 0 {
		if !a.syncStartPolygon(true) {
			a.needsReplan = true
		}
	}
}

// SetMesh attaches a new NavMesh to the agent, invalidating any existing corridor so the agent replans.
func (a *NavAgent) SetMesh(mesh *NavMesh) {
	a.mesh = mesh
	if mesh != nil {
		a.meshRevision = mesh.Revision()
	} else {
		a.meshRevision = 0
	}
	a.corridor = a.corridor[:0]
	a.portals = a.portals[:0]
	a.tailExtensions = 0
	a.traversingLink = false
	if a.hasTarget {
		if mesh == nil {
			a.state = NavAgentUnreachable
			a.needsReplan = false
		} else {
			a.state = NavAgentMoving
			a.needsReplan = true
		}
	}
}

// ClearTarget clears the agent's goal, resets its corridor, stops velocity, and sets state to NavAgentIdle.
func (a *NavAgent) ClearTarget() {
	a.hasTarget = false
	a.target = vec2{}
	a.vel = vec2{}
	a.corridor = a.corridor[:0]
	a.portals = a.portals[:0]
	a.tailExtensions = 0
	a.needsReplan = false
	a.traversingLink = false
	a.state = NavAgentIdle
}

func (a *NavAgent) navFilter() NavFilter {
	if a.cfg.Filter != nil {
		return *a.cfg.Filter
	}
	return DefaultNavFilter()
}

func (a *NavAgent) allowsPoly(polyIdx int) bool {
	if a.mesh == nil || polyIdx < 0 || polyIdx >= len(a.mesh.polys) {
		return false
	}
	p := &a.mesh.polys[polyIdx]
	return a.navFilter().Allows(p.area, p.flags)
}

// SetTarget sets or updates the agent's destination.
//
// When the agent already holds a valid corridor, SetTarget tries cheap local end-of-corridor
// repair (same polygon, corridor truncation, or bounded bridge extension) before scheduling a
// full A* replan. Agents managed by a NavAgentGroup defer full replans to NavAgentGroup.Step so
// MaxReplansPerStep is strictly respected.
func (a *NavAgent) SetTarget(target core.Point) {
	t := toVec2(target)
	if !isFiniteVec2(t) {
		return
	}
	a.target = t
	a.hasTarget = true

	if a.mesh == nil || len(a.mesh.polys) == 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.needsReplan = false
		a.state = NavAgentUnreachable
		return
	}

	a.checkMeshRevision()

	goalPoly := a.mesh.FindPolygonWithFilter(target, a.navFilter())
	if goalPoly < 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.tailExtensions = 0
		a.traversingLink = false
		a.needsReplan = false
		a.state = NavAgentUnreachable
		return
	}

	// Already at target in the same polygon (or valid single-poly corridor).
	if len(a.corridor) > 0 && !a.needsReplan {
		if a.repairCorridorEnd(goalPoly) {
			if len(a.corridor) == 1 && dist(a.pos, a.target) <= float64(a.cfg.StoppingDistance) {
				a.state = NavAgentArrived
				a.vel = vec2{}
			} else if a.state != NavAgentMoving {
				a.state = NavAgentMoving
			}
			// If tail extensions exceeded threshold and we can replan immediately (standalone, no cooldown), do so.
			if a.needsReplan && a.group == nil && a.replanCooldown <= 0 {
				a.replanNow()
			}
			return
		}
	}

	a.needsReplan = true
	if a.state == NavAgentIdle || a.state == NavAgentArrived || a.state == NavAgentUnreachable {
		a.state = NavAgentMoving
	}
	if a.group == nil && a.replanCooldown <= 0 {
		a.replanNow()
	}
}

// ForceReplan immediately executes a full A* replan from the agent's current position to its target,
// ignoring ReplanInterval and group throttling, and reports whether a valid corridor was found.
func (a *NavAgent) ForceReplan() bool {
	if !a.hasTarget {
		return false
	}
	return a.replanNow()
}

// NextCorner returns the next funnel steering corner along the agent's remaining corridor.
// Returns (zero, false) when the agent has no valid corridor.
func (a *NavAgent) NextCorner() (core.Point, bool) {
	if a.mesh == nil || !a.hasTarget || len(a.corridor) == 0 {
		return core.Point{}, false
	}
	if a.traversingLink {
		return a.activeLink.End, true
	}
	corner, _, _ := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)
	return toPoint(corner), true
}

// Path returns the full string-pulled waypoint path from the agent's current position to its target
// along its active corridor, or nil when no valid corridor exists.
func (a *NavAgent) Path() []core.Point {
	if a.mesh == nil || !a.hasTarget || len(a.corridor) == 0 {
		return nil
	}
	if len(a.corridor) == 1 {
		if vequal(a.pos, a.target) {
			return []core.Point{toPoint(a.pos)}
		}
		return []core.Point{toPoint(a.pos), toPoint(a.target)}
	}
	path, _ := a.mesh.finalizeCorridor(a.pos, a.target, a.portals)
	return path
}

// Step advances the agent by fixed duration dt: ticks replan cooldown, detects mesh changes,
// repairs the corridor locally or replans if needed, steers toward the next funnel corner,
// traverses off-mesh/flagged links, and settles cleanly on arrival.
func (a *NavAgent) Step(dt time.Duration) {
	if dt <= 0 {
		return
	}
	a.tickPreStep(dt)
	if a.hasTarget && a.needsReplan && a.replanCooldown <= 0 {
		a.replanNow()
	}
	a.stepMovement(dt)
}

// DesiredVelocity returns the velocity the agent would use to follow its current corridor.
// It is suitable for passing through an avoidance solver before StepWithVelocity.
func (a *NavAgent) DesiredVelocity() core.Point {
	if a == nil || !a.hasTarget || a.mesh == nil || len(a.corridor) == 0 || a.state == NavAgentIdle || a.state == NavAgentUnreachable {
		return core.Point{}
	}
	corner, isGoal, _ := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)
	d := corner.sub(a.pos).len()
	if d <= 1e-9 {
		return core.Point{}
	}
	speed := float64(a.cfg.MaxSpeed)
	if isGoal && a.cfg.ArrivalRadius > 0 && d < float64(a.cfg.ArrivalRadius) {
		speed *= d / float64(a.cfg.ArrivalRadius)
	}
	return toPoint(corner.sub(a.pos).scale(speed / d))
}

// StepWithVelocity advances the agent using an externally chosen velocity, such as an ORCA result.
func (a *NavAgent) StepWithVelocity(v core.Point, dt time.Duration) {
	if a == nil || dt <= 0 {
		return
	}
	a.tickPreStep(dt)
	if a.hasTarget && a.needsReplan && a.replanCooldown <= 0 {
		a.replanNow()
	}
	if a.mesh == nil || len(a.corridor) == 0 || a.state == NavAgentUnreachable {
		return
	}
	secs := dt.Seconds()
	vel := toVec2(v)
	maxSpeed := float64(a.cfg.MaxSpeed)
	if speed := vel.len(); speed > maxSpeed && speed > 0 {
		vel = vel.scale(maxSpeed / speed)
	}
	a.advancePosition(a.pos.add(vel.scale(secs)), vel, dt)
}

func (a *NavAgent) tickPreStep(dt time.Duration) {
	if a.replanCooldown > 0 {
		a.replanCooldown -= dt
		if a.replanCooldown < 0 {
			a.replanCooldown = 0
		}
	}
	if !a.hasTarget {
		return
	}
	a.checkMeshRevision()
	if a.mesh == nil || len(a.mesh.polys) == 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.state = NavAgentUnreachable
		a.needsReplan = false
		return
	}
	if !a.traversingLink && len(a.corridor) > 0 {
		if !a.syncStartPolygon(true) {
			a.needsReplan = true
		}
	}
}

func (a *NavAgent) checkMeshRevision() {
	if a.mesh == nil {
		return
	}
	rev := a.mesh.Revision()
	if rev != a.meshRevision {
		a.meshRevision = rev
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.tailExtensions = 0
		a.traversingLink = false
		if a.hasTarget {
			a.needsReplan = true
			if a.state == NavAgentUnreachable {
				a.state = NavAgentMoving
			}
		}
	}
}

func (a *NavAgent) replanNow() bool {
	a.needsReplan = false
	a.replanCount++
	a.replanCooldown = a.cfg.ReplanInterval
	a.tailExtensions = 0
	a.traversingLink = false

	if a.mesh == nil || len(a.mesh.polys) == 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.state = NavAgentUnreachable
		return false
	}
	a.meshRevision = a.mesh.Revision()

	filter := a.navFilter()
	start := a.pos
	if a.mesh.FindPolygonWithFilter(toPoint(start), filter) < 0 {
		if _, clamped, ok := a.mesh.NearestPolygonWithFilter(toPoint(start), filter); ok {
			start = toVec2(clamped)
		} else {
			a.corridor = a.corridor[:0]
			a.portals = a.portals[:0]
			a.state = NavAgentUnreachable
			return false
		}
	}

	polys, portals, ok := a.mesh.findCorridorWithFilter(start, a.target, filter)
	if !ok {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.vel = vec2{}
		a.state = NavAgentUnreachable
		return false
	}

	a.corridor = append(a.corridor[:0], polys...)
	a.portals = append(a.portals[:0], portals...)
	if len(a.corridor) == 1 && dist(a.pos, a.target) <= float64(a.cfg.StoppingDistance) {
		a.state = NavAgentArrived
		a.vel = vec2{}
	} else {
		a.state = NavAgentMoving
	}
	return true
}

func (a *NavAgent) stepMovement(dt time.Duration) {
	if !a.hasTarget || a.mesh == nil || len(a.mesh.polys) == 0 {
		return
	}
	if a.state == NavAgentIdle || a.state == NavAgentUnreachable {
		return
	}
	if len(a.corridor) == 0 {
		// Waiting for a throttled/staggered initial replan.
		return
	}

	if a.traversingLink {
		a.stepLinkTraversal(dt)
		return
	}

	// Check if arrived agent was pushed outside stopping distance.
	if a.state == NavAgentArrived {
		if dist(a.pos, a.target) <= float64(a.cfg.StoppingDistance) {
			a.vel = vec2{}
			return
		}
		a.state = NavAgentMoving
	}

	secs := dt.Seconds()
	corner, isGoal, offMeshIdx := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)

	// Enter off-mesh link when reaching its entry point.
	if offMeshIdx == 0 && len(a.portals) > 0 {
		p := a.mesh.portals[a.portals[0]]
		radius, area, _, userData := a.mesh.offMeshMeta(p)
		entryRadius := max(float64(a.cfg.StoppingDistance), float64(a.cfg.MaxSpeed)*secs*0.5, float64(radius))
		if dist(a.pos, p.left) <= entryRadius {
			a.beginLinkTraversal(NavLinkEvent{
				FromPoly: p.fromPoly,
				ToPoly:   p.toPoly,
				Start:    toPoint(p.left),
				End:      toPoint(p.endPos),
				Radius:   radius,
				Area:     area,
				Flags:    p.flags,
				LinkID:   p.linkID,
				UserData: userData,
				OffMesh:  true,
			})
			a.stepLinkTraversal(dt)
			return
		}
	}

	// Check arrival before moving if already within stopping distance on the final straight segment.
	if isGoal && dist(a.pos, a.target) <= float64(a.cfg.StoppingDistance) {
		a.state = NavAgentArrived
		a.vel = vec2{}
		return
	}

	toCorner := corner.sub(a.pos)
	d := toCorner.len()
	var desiredVel vec2
	if d > 1e-9 {
		speed := float64(a.cfg.MaxSpeed)
		if isGoal {
			if slow := float64(a.cfg.ArrivalRadius); slow > 0 && d < slow {
				speed *= d / slow
			}
			// Never overshoot the target in a single step.
			if maxStepSpeed := d / secs; speed > maxStepSpeed {
				speed = maxStepSpeed
			}
		} else if offMeshIdx >= 0 {
			if maxStepSpeed := d / secs; speed > maxStepSpeed {
				speed = maxStepSpeed
			}
		}
		desiredVel = toCorner.scale(speed / d)
	}

	reqAccel := desiredVel.sub(a.vel).scale(1 / secs)
	accel := clampVec2(reqAccel, float64(a.cfg.MaxAcceleration))
	v := clampVec2(a.vel.add(accel.scale(secs)), float64(a.cfg.MaxSpeed))
	nextPos := a.pos.add(v.scale(secs))

	// Move along the navmesh surface, advancing or locally repairing the corridor and sliding along walls.
	a.advancePosition(nextPos, v, dt)

	// Re-check arrival after moving.
	if !a.traversingLink && len(a.corridor) > 0 {
		_, nowGoal, _ := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)
		if nowGoal && dist(a.pos, a.target) <= float64(a.cfg.StoppingDistance) {
			a.state = NavAgentArrived
			a.vel = vec2{}
		}
	}
}

func (a *NavAgent) advancePosition(nextPos, nextVel vec2, dt time.Duration) {
	curPoly := a.corridor[0]

	// 1. Stayed inside current polygon.
	if polyContains(&a.mesh.polys[curPoly], nextPos) {
		a.pos = nextPos
		a.vel = nextVel
		return
	}

	// 2. Crossed into the next polygon in the corridor (most common transition).
	if len(a.corridor) > 1 {
		p := a.mesh.portals[a.portals[0]]
		if !p.offMesh && polyContains(&a.mesh.polys[a.corridor[1]], nextPos) {
			if p.flags != 0 && a.cfg.OnLink != nil {
				cross := closestPointOnSegment(a.pos, p.left, p.right)
				a.beginLinkTraversal(NavLinkEvent{
					FromPoly: p.fromPoly,
					ToPoly:   p.toPoly,
					Start:    toPoint(cross),
					End:      toPoint(nextPos),
					Flags:    p.flags,
					LinkID:   p.linkID,
					OffMesh:  false,
				})
				a.stepLinkTraversal(dt)
				return
			}
			a.corridor = a.corridor[1:]
			a.portals = a.portals[1:]
			a.pos = nextPos
			a.vel = nextVel
			return
		}
	}

	// 3. Crossed multiple thin polygons in one step along the corridor (up to first off-mesh or flagged link).
	for k := 2; k < len(a.corridor); k++ {
		prevP := a.mesh.portals[a.portals[k-1]]
		if prevP.offMesh || (prevP.flags != 0 && a.cfg.OnLink != nil) {
			break
		}
		if polyContains(&a.mesh.polys[a.corridor[k]], nextPos) {
			a.corridor = a.corridor[k:]
			a.portals = a.portals[k:]
			a.pos = nextPos
			a.vel = nextVel
			return
		}
	}

	// 4. Stepped into an adjacent non-corridor neighbor polygon.
	for _, pid := range a.mesh.polys[curPoly].outPortals {
		p := a.mesh.portals[pid]
		if p.offMesh || !a.allowsPoly(p.toPoly) {
			continue
		}
		if polyContains(&a.mesh.polys[p.toPoly], nextPos) {
			a.pos = nextPos
			a.vel = nextVel
			if !a.repairCorridorStart(p.toPoly, true) {
				a.needsReplan = true
			}
			return
		}
	}

	// 5. Stepped into another polygon on the mesh (e.g. across a thin corner triangle).
	if fp := a.mesh.FindPolygonWithFilter(toPoint(nextPos), a.navFilter()); fp >= 0 {
		a.pos = nextPos
		a.vel = nextVel
		if !a.repairCorridorStart(fp, true) {
			a.needsReplan = true
		}
		return
	}

	// 6. Stepped slightly outside the navmesh boundary: clamp to closest point on curPoly or its
	// connected neighbors and slide velocity tangentially along the wall edge.
	bestPoly := curPoly
	bestPt := a.mesh.polyClosestPoint(curPoly, nextPos)
	bestDistSq := distSq(nextPos, bestPt)

	for _, pid := range a.mesh.polys[curPoly].outPortals {
		p := a.mesh.portals[pid]
		if p.offMesh || !a.allowsPoly(p.toPoly) {
			continue
		}
		cp := a.mesh.polyClosestPoint(p.toPoly, nextPos)
		d2 := distSq(nextPos, cp)
		// Prefer corridor[1] on ties so rounding an inner corner slides forward cleanly.
		better := d2 < bestDistSq-1e-12
		if !better && len(a.corridor) > 1 && p.toPoly == a.corridor[1] && math.Abs(d2-bestDistSq) <= 1e-12 {
			better = true
		}
		if better {
			bestDistSq = d2
			bestPoly = p.toPoly
			bestPt = cp
		}
	}

	push := bestPt.sub(nextPos)
	if plen := push.len(); plen > 1e-9 {
		n := push.scale(1 / plen)
		if vn := nextVel.dot(n); vn < 0 {
			nextVel = nextVel.sub(n.scale(vn))
		}
	}
	a.pos = bestPt
	a.vel = nextVel
	if bestPoly != curPoly {
		if !a.repairCorridorStart(bestPoly, false) {
			a.needsReplan = true
		}
	}
}

func (a *NavAgent) beginLinkTraversal(ev NavLinkEvent) {
	a.traversingLink = true
	a.activeLink = ev
	a.vel = vec2{}
}

func (a *NavAgent) stepLinkTraversal(dt time.Duration) {
	ev := a.activeLink
	if a.cfg.OnLink != nil {
		if !a.cfg.OnLink(a, ev, dt) {
			return
		}
		a.traversingLink = false
		// If callback returned true without moving the agent into ToPoly, advance to link End.
		if ev.ToPoly >= 0 && ev.ToPoly < len(a.mesh.polys) && !polyContains(&a.mesh.polys[ev.ToPoly], a.pos) {
			a.pos = toVec2(ev.End)
		}
		if len(a.corridor) > 1 && a.corridor[1] == ev.ToPoly {
			a.corridor = a.corridor[1:]
			a.portals = a.portals[1:]
		} else if ev.ToPoly >= 0 {
			if !a.repairCorridorStart(ev.ToPoly, false) {
				a.needsReplan = true
			}
		}
		return
	}

	// Default off-mesh link traversal: move linearly toward End at MaxSpeed.
	end := toVec2(ev.End)
	diff := end.sub(a.pos)
	d := diff.len()
	stepDist := float64(a.cfg.MaxSpeed) * dt.Seconds()
	if d <= stepDist || d <= 1e-6 {
		a.pos = end
		a.vel = vec2{}
		a.traversingLink = false
		if len(a.corridor) > 1 && a.corridor[1] == ev.ToPoly {
			a.corridor = a.corridor[1:]
			a.portals = a.portals[1:]
		}
		return
	}
	a.vel = diff.scale(float64(a.cfg.MaxSpeed) / d)
	a.pos = a.pos.add(diff.scale(stepDist / d))
}

// syncStartPolygon resolves the polygon currently containing a.pos and repairs the corridor start locally.
func (a *NavAgent) syncStartPolygon(countRepair bool) bool {
	if a.mesh == nil || len(a.corridor) == 0 {
		return false
	}
	cur := a.corridor[0]
	if cur >= 0 && cur < len(a.mesh.polys) && polyContains(&a.mesh.polys[cur], a.pos) {
		return true
	}

	// Check connected neighbors of corridor[0] first for locality.
	foundPoly := -1
	if cur >= 0 && cur < len(a.mesh.polys) {
		for _, pid := range a.mesh.polys[cur].outPortals {
			p := a.mesh.portals[pid]
			if !p.offMesh && a.allowsPoly(p.toPoly) && polyContains(&a.mesh.polys[p.toPoly], a.pos) {
				foundPoly = p.toPoly
				break
			}
		}
	}
	filter := a.navFilter()
	if foundPoly < 0 {
		foundPoly = a.mesh.FindPolygonWithFilter(toPoint(a.pos), filter)
	}
	if foundPoly < 0 {
		if idx, _, ok := a.mesh.NearestPolygonWithFilter(toPoint(a.pos), filter); ok {
			foundPoly = idx
		} else {
			return false
		}
	}
	return a.repairCorridorStart(foundPoly, countRepair)
}

// repairCorridorStart updates a.corridor so it begins at curPoly without running a full A* replan.
// It handles: (1) already at corridor[0], (2) jumping ahead to corridor[k], and (3) prepending a
// bounded BFS bridge of up to cfg.MaxRepairDepth adjacent polygons when pushed onto a neighbor.
func (a *NavAgent) repairCorridorStart(curPoly int, countRepair bool) bool {
	if len(a.corridor) == 0 || curPoly < 0 || curPoly >= len(a.mesh.polys) {
		return false
	}
	if a.corridor[0] == curPoly {
		return true
	}

	// Check if curPoly is already later in the corridor.
	for k := len(a.corridor) - 1; k >= 1; k-- {
		if a.corridor[k] == curPoly {
			a.corridor = a.corridor[k:]
			a.portals = a.portals[k:]
			if countRepair {
				a.localRepairs++
			}
			return true
		}
	}

	// Bounded BFS from curPoly to find a short bridge back into a.corridor.
	corridorIdx := make(map[int]int, len(a.corridor))
	for i, p := range a.corridor {
		corridorIdx[p] = i
	}

	type bfsNode struct {
		poly      int
		parentIdx int
		portalID  int
		depth     int
	}
	maxDepth := a.cfg.MaxRepairDepth
	queue := make([]bfsNode, 0, 16)
	queue = append(queue, bfsNode{poly: curPoly, parentIdx: -1, portalID: -1, depth: 0})
	visited := make([]int, 0, 16)
	visited = append(visited, curPoly)

	bestFound := false
	bestNodeIdx := -1
	bestCorridorK := -1
	bestTotalLen := math.MaxInt
	bestBridgeDepth := math.MaxInt

	for head := 0; head < len(queue); head++ {
		node := queue[head]
		if node.depth >= maxDepth {
			continue
		}
		for _, pid := range a.mesh.polys[node.poly].outPortals {
			p := a.mesh.portals[pid]
			if p.offMesh || !a.allowsPoly(p.toPoly) {
				continue
			}
			nb := p.toPoly
			if k, ok := corridorIdx[nb]; ok {
				bridgeDepth := node.depth + 1
				totalLen := bridgeDepth + (len(a.corridor) - 1 - k)
				if !bestFound ||
					totalLen < bestTotalLen ||
					(totalLen == bestTotalLen && bridgeDepth < bestBridgeDepth) ||
					(totalLen == bestTotalLen && bridgeDepth == bestBridgeDepth && k > bestCorridorK) {
					bestFound = true
					bestTotalLen = totalLen
					bestBridgeDepth = bridgeDepth
					bestCorridorK = k
					bestNodeIdx = len(queue)
					queue = append(queue, bfsNode{
						poly:      nb,
						parentIdx: head,
						portalID:  pid,
						depth:     bridgeDepth,
					})
				}
				continue
			}
			if slices.Contains(visited, nb) {
				continue
			}
			visited = append(visited, nb)
			queue = append(queue, bfsNode{
				poly:      nb,
				parentIdx: head,
				portalID:  pid,
				depth:     node.depth + 1,
			})
		}
	}

	if !bestFound {
		return false
	}

	// Reconstruct bridge from curPoly -> ... -> a.corridor[bestCorridorK].
	var bridgePolys []int
	var bridgePortals []int
	for idx := bestNodeIdx; idx >= 0; idx = queue[idx].parentIdx {
		n := queue[idx]
		if n.parentIdx >= 0 {
			bridgePortals = append(bridgePortals, n.portalID)
		}
		if idx != bestNodeIdx {
			bridgePolys = append(bridgePolys, n.poly)
		}
	}
	slices.Reverse(bridgePolys)
	slices.Reverse(bridgePortals)

	newCorridor := make([]int, 0, len(bridgePolys)+len(a.corridor)-bestCorridorK)
	newCorridor = append(newCorridor, bridgePolys...)
	newCorridor = append(newCorridor, a.corridor[bestCorridorK:]...)

	newPortals := make([]int, 0, len(bridgePortals)+len(a.portals)-bestCorridorK)
	newPortals = append(newPortals, bridgePortals...)
	newPortals = append(newPortals, a.portals[bestCorridorK:]...)

	a.corridor = newCorridor
	a.portals = newPortals
	if countRepair {
		a.localRepairs++
	}
	return true
}

// repairCorridorEnd updates the tail of a.corridor to reach goalPoly without a full A* replan.
func (a *NavAgent) repairCorridorEnd(goalPoly int) bool {
	n := len(a.corridor)
	if n == 0 || goalPoly < 0 || goalPoly >= len(a.mesh.polys) {
		return false
	}
	lastPoly := a.corridor[n-1]
	if lastPoly == goalPoly {
		return true
	}

	// Target moved backward onto an earlier polygon already in the corridor: truncate tail.
	for k := 0; k < n-1; k++ {
		if a.corridor[k] == goalPoly {
			a.corridor = a.corridor[:k+1]
			a.portals = a.portals[:k]
			a.localRepairs++
			a.tailExtensions = 0
			return true
		}
	}

	// Bounded BFS from lastPoly up to MaxRepairDepth hops to reach goalPoly locally.
	type bfsNode struct {
		poly      int
		parentIdx int
		portalID  int
		depth     int
	}
	maxDepth := a.cfg.MaxRepairDepth
	queue := make([]bfsNode, 0, 16)
	queue = append(queue, bfsNode{poly: lastPoly, parentIdx: -1, portalID: -1, depth: 0})
	visited := make([]int, 0, 16)
	visited = append(visited, lastPoly)

	foundIdx := -1
	for head := 0; head < len(queue) && foundIdx < 0; head++ {
		node := queue[head]
		if node.depth >= maxDepth {
			continue
		}
		for _, pid := range a.mesh.polys[node.poly].outPortals {
			p := a.mesh.portals[pid]
			if p.offMesh || !a.allowsPoly(p.toPoly) {
				continue
			}
			nb := p.toPoly
			if nb == goalPoly {
				foundIdx = len(queue)
				queue = append(queue, bfsNode{
					poly:      nb,
					parentIdx: head,
					portalID:  pid,
					depth:     node.depth + 1,
				})
				break
			}
			if slices.Contains(visited, nb) {
				continue
			}
			visited = append(visited, nb)
			queue = append(queue, bfsNode{
				poly:      nb,
				parentIdx: head,
				portalID:  pid,
				depth:     node.depth + 1,
			})
		}
	}

	if foundIdx < 0 {
		return false
	}

	var extPolys []int
	var extPortals []int
	for idx := foundIdx; idx > 0; idx = queue[idx].parentIdx {
		extPolys = append(extPolys, queue[idx].poly)
		extPortals = append(extPortals, queue[idx].portalID)
	}
	slices.Reverse(extPolys)
	slices.Reverse(extPortals)

	for i, ep := range extPolys {
		// Trim loops if the extension re-enters an earlier corridor polygon.
		loopIdx := slices.Index(a.corridor, ep)
		if loopIdx >= 0 {
			a.corridor = a.corridor[:loopIdx+1]
			a.portals = a.portals[:loopIdx]
			continue
		}
		a.corridor = append(a.corridor, ep)
		a.portals = append(a.portals, extPortals[i])
	}

	a.localRepairs++
	a.tailExtensions++
	// Schedule a throttled full replan after repeated local tail extensions so the path stays optimal.
	if a.tailExtensions >= a.cfg.MaxRepairDepth {
		a.needsReplan = true
	}
	return true
}

// NavAgentGroupConfig configures budgeted and staggered replanning across a group of 2D NavAgents.
type NavAgentGroupConfig struct {
	// MaxReplansPerStep caps how many agents perform a full A* replan in a single Step.
	// Defaults to 4 when <= 0.
	MaxReplansPerStep int
	// ReplanInterval sets the minimum duration between full replans for agents whose own
	// NavAgentConfig.ReplanInterval is 0.
	ReplanInterval time.Duration
	// StaggerStride spreads full replan eligibility across steps using agentID%stride == step%stride.
	// Defaults to 1 (every agent is eligible in round-robin order up to MaxReplansPerStep).
	StaggerStride int
}

// NavAgentGroup coordinates multiple 2D NavAgents with throttled, round-robin budgeted, and
// optionally staggered replanning so large groups chasing moving targets never spike a single frame.
type NavAgentGroup struct {
	cfg             NavAgentGroupConfig
	agents          []*NavAgent
	nextID          uint64
	steps           uint64
	cursor          int
	lastStepReplans int
	totalReplans    int
}

// NewNavAgentGroup constructs a NavAgentGroup with the given replan budget and staggering settings.
func NewNavAgentGroup(cfg NavAgentGroupConfig) *NavAgentGroup {
	if cfg.MaxReplansPerStep <= 0 {
		cfg.MaxReplansPerStep = 4
	}
	if cfg.ReplanInterval < 0 {
		cfg.ReplanInterval = 0
	}
	if cfg.StaggerStride <= 0 {
		cfg.StaggerStride = 1
	}
	return &NavAgentGroup{cfg: cfg}
}

// Add creates a new NavAgent, registers it in the group, and returns it.
func (g *NavAgentGroup) Add(mesh *NavMesh, pos core.Point, cfg NavAgentConfig) *NavAgent {
	a := NewNavAgent(mesh, pos, cfg)
	g.AddAgent(a)
	return a
}

// AddAgent attaches an existing NavAgent to the group.
func (g *NavAgentGroup) AddAgent(a *NavAgent) {
	if g == nil || a == nil || a.group == g {
		return
	}
	if a.group != nil {
		a.group.Remove(a)
	}
	g.nextID++
	a.id = g.nextID
	a.group = g
	g.agents = append(g.agents, a)
}

// Remove detaches a from the group while preserving deterministic ordering of remaining agents.
func (g *NavAgentGroup) Remove(a *NavAgent) bool {
	if g == nil || a == nil || a.group != g {
		return false
	}
	idx := slices.Index(g.agents, a)
	if idx < 0 {
		return false
	}
	g.agents = slices.Delete(g.agents, idx, idx+1)
	a.group = nil
	if len(g.agents) == 0 {
		g.cursor = 0
	} else if g.cursor >= len(g.agents) {
		g.cursor = 0
	}
	return true
}

// Agents returns the group's agents in deterministic registration order.
func (g *NavAgentGroup) Agents() []*NavAgent { return g.agents }

// Len returns the number of agents in the group.
func (g *NavAgentGroup) Len() int { return len(g.agents) }

// Steps returns the number of times Step has advanced this group.
func (g *NavAgentGroup) Steps() uint64 { return g.steps }

// LastStepReplans returns how many full A* replans executed during the most recent Step.
func (g *NavAgentGroup) LastStepReplans() int { return g.lastStepReplans }

// TotalReplans returns the cumulative number of full A* replans executed by Step across all agents.
func (g *NavAgentGroup) TotalReplans() int { return g.totalReplans }

// Step advances every agent in the group by dt while strictly bounding full A* replans to
// MaxReplansPerStep in deterministic round-robin order.
func (g *NavAgentGroup) Step(dt time.Duration) {
	if g == nil || dt <= 0 {
		return
	}
	g.steps++
	g.lastStepReplans = 0
	n := len(g.agents)
	if n == 0 {
		return
	}

	// Phase 1: tick cooldowns, detect mesh revisions, and perform cheap local corridor repairs.
	for _, a := range g.agents {
		a.tickPreStep(dt)
	}

	// Phase 2: execute up to MaxReplansPerStep full replans in deterministic round-robin order.
	budget := g.cfg.MaxReplansPerStep
	stride := uint64(g.cfg.StaggerStride)
	startCursor := g.cursor % n
	nextCursor := startCursor

	for offset := 0; offset < n && budget > 0; offset++ {
		idx := (startCursor + offset) % n
		a := g.agents[idx]
		if !a.hasTarget || !a.needsReplan || a.replanCooldown > 0 {
			continue
		}
		if stride > 1 && (a.id%stride) != (g.steps%stride) {
			continue
		}
		a.replanNow()
		if a.cfg.ReplanInterval == 0 && g.cfg.ReplanInterval > 0 {
			a.replanCooldown = g.cfg.ReplanInterval
		}
		budget--
		g.lastStepReplans++
		g.totalReplans++
		nextCursor = (idx + 1) % n
	}
	g.cursor = nextCursor

	// Phase 3: step deterministic movement for every agent.
	for _, a := range g.agents {
		a.stepMovement(dt)
	}
}

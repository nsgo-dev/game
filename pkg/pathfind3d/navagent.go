package pathfind3d

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// NavAgentState reports the navigation status of a 3D NavAgent.
type NavAgentState uint8

const (
	// NavAgentIdle indicates the agent has no active navigation target.
	NavAgentIdle NavAgentState = iota
	// NavAgentMoving indicates the agent is actively following its 3D polygon corridor toward its target.
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

// NavLinkEvent describes a flagged shared-edge or off-mesh 3D link transition encountered by a NavAgent.
type NavLinkEvent struct {
	FromPoly int
	ToPoly   int
	Start    math3d.Vec3
	End      math3d.Vec3
	Radius   float32
	Area     NavArea
	Flags    uint32
	LinkID   int
	UserData any
	OffMesh  bool
}

// NavAgentConfig configures steering, arrival, corridor repair, replan throttling, and off-mesh link traversal for a 3D NavAgent.
type NavAgentConfig struct {
	// MaxSpeed is the maximum horizontal movement speed in world units per second. Defaults to 5 when <= 0.
	MaxSpeed float32
	// MaxAcceleration caps how fast horizontal velocity changes in world units per second squared.
	// Defaults to 4 * MaxSpeed when <= 0.
	MaxAcceleration float32
	// Radius is the agent's body radius in world units. Defaults to 0.4 when <= 0.
	Radius float32
	// StoppingDistance is the 3D distance from the goal within which the agent counts as arrived.
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
	// Filter optionally restricts which 3D polygons this agent may traverse and sets per-area costs.
	// When nil, DefaultNavFilter() is used (all walkable non-disabled polygons at unit cost).
	Filter *NavFilter
	// OnLink is an optional callback invoked on each step while traversing an off-mesh link or
	// crossing a flagged shared-edge portal. Return true when the transition is complete; return
	// false to keep control of the agent on subsequent steps. When nil, off-mesh links traverse
	// linearly in 3D at MaxSpeed and flagged shared-edge portals are crossed automatically.
	OnLink func(agent *NavAgent, link NavLinkEvent, dt time.Duration) bool
}

func sanitizeNavAgentConfig(cfg NavAgentConfig) NavAgentConfig {
	if !isFinite32(cfg.MaxSpeed) || cfg.MaxSpeed <= 0 {
		cfg.MaxSpeed = 5
	}
	if !isFinite32(cfg.MaxAcceleration) || cfg.MaxAcceleration <= 0 {
		cfg.MaxAcceleration = 4 * cfg.MaxSpeed
	}
	if !isFinite32(cfg.Radius) || cfg.Radius <= 0 {
		cfg.Radius = 0.4
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

func isFiniteVec3(v math3d.Vec3) bool {
	return isFinite32(v.X) && isFinite32(v.Y) && isFinite32(v.Z)
}

// NavAgent navigates a 3D NavMesh by maintaining an active polygon corridor,
// steering smoothly along XZ toward the next funnel corner while tracking exact 3D surface
// height across sloped ramps and multi-level bridges, repairing its corridor locally when pushed
// or when the target shifts, and invoking callbacks for off-mesh or flagged links.
//
// NavAgent is deterministic on fixed game.Loop steps and is not safe for concurrent use.
type NavAgent struct {
	mesh         *NavMesh
	meshRevision uint64
	cfg          NavAgentConfig

	pos       math3d.Vec3
	vel       math3d.Vec3
	target    math3d.Vec3
	hasTarget bool
	state     NavAgentState

	corridor       []int
	portals        []NavLink
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

// NewNavAgent constructs a 3D navigation agent placed at pos on mesh.
// If pos lies over a walkable polygon within VerticalTolerance, its Y coordinate is snapped to the surface.
func NewNavAgent(mesh *NavMesh, pos math3d.Vec3, cfg NavAgentConfig) *NavAgent {
	if !isFiniteVec3(pos) {
		pos = math3d.Vec3{}
	}
	var rev uint64
	if mesh != nil {
		rev = mesh.Revision()
		if idx, ok := mesh.FindPolygon(pos); ok {
			pos.Y = mesh.Polys[idx].HeightAt(pos.X, pos.Z)
		}
	}
	return &NavAgent{
		mesh:         mesh,
		meshRevision: rev,
		cfg:          sanitizeNavAgentConfig(cfg),
		pos:          pos,
		state:        NavAgentIdle,
	}
}

// Config returns the sanitized configuration of the agent.
func (a *NavAgent) Config() NavAgentConfig { return a.cfg }

// Pos returns the agent's current 3D position.
func (a *NavAgent) Pos() math3d.Vec3 { return a.pos }

// Vel returns the agent's current 3D velocity.
func (a *NavAgent) Vel() math3d.Vec3 { return a.vel }

// State returns the agent's current navigation state.
func (a *NavAgent) State() NavAgentState { return a.state }

// Target returns the agent's current target position (zero when idle).
func (a *NavAgent) Target() math3d.Vec3 { return a.target }

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
func (a *NavAgent) SetVelocity(v math3d.Vec3) {
	if !isFiniteVec3(v) {
		return
	}
	a.vel = v
}

// SetPosition updates the agent's 3D position (for example after physics knockback or external push).
// When the agent has an active corridor and is not mid-link, it attempts cheap local corridor
// repair immediately without running a full A* replan.
func (a *NavAgent) SetPosition(pos math3d.Vec3) {
	if !isFiniteVec3(pos) {
		return
	}
	a.pos = pos
	if !a.hasTarget || a.traversingLink {
		return
	}
	if a.state == NavAgentArrived && a.pos.Dist(a.target) > a.cfg.StoppingDistance {
		a.state = NavAgentMoving
	}
	if len(a.corridor) > 0 {
		if !a.syncStartPolygon(true) {
			a.needsReplan = true
		}
	}
}

// SetMesh attaches a new 3D NavMesh to the agent, invalidating any existing corridor so the agent replans.
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
	a.target = math3d.Vec3{}
	a.vel = math3d.Vec3{}
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
	if a.mesh == nil || polyIdx < 0 || polyIdx >= len(a.mesh.Polys) {
		return false
	}
	p := &a.mesh.Polys[polyIdx]
	return a.navFilter().Allows(p.Area, p.Flags)
}

// SetTarget sets or updates the agent's 3D destination.
//
// When the agent already holds a valid corridor, SetTarget tries cheap local end-of-corridor
// repair (same polygon, corridor truncation, or bounded bridge extension) before scheduling a
// full A* replan. Agents managed by a NavAgentGroup defer full replans to NavAgentGroup.Step so
// MaxReplansPerStep is strictly respected.
func (a *NavAgent) SetTarget(target math3d.Vec3) {
	if !isFiniteVec3(target) {
		return
	}
	a.target = target
	a.hasTarget = true

	if a.mesh == nil || len(a.mesh.Polys) == 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.needsReplan = false
		a.state = NavAgentUnreachable
		return
	}

	a.checkMeshRevision()

	goalPoly, ok := a.mesh.FindPolygonWithFilter(target, a.navFilter())
	if !ok {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.tailExtensions = 0
		a.traversingLink = false
		a.needsReplan = false
		a.state = NavAgentUnreachable
		return
	}
	a.target.Y = a.mesh.Polys[goalPoly].HeightAt(target.X, target.Z)

	if len(a.corridor) > 0 && !a.needsReplan {
		if a.repairCorridorEnd(goalPoly) {
			if len(a.corridor) == 1 && a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
				a.state = NavAgentArrived
				a.vel = math3d.Vec3{}
			} else if a.state != NavAgentMoving {
				a.state = NavAgentMoving
			}
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

// NextCorner returns the next 3D funnel steering corner along the agent's remaining corridor.
// Returns (zero, false) when the agent has no valid corridor.
func (a *NavAgent) NextCorner() (math3d.Vec3, bool) {
	if a.mesh == nil || !a.hasTarget || len(a.corridor) == 0 {
		return math3d.Vec3{}, false
	}
	if a.traversingLink {
		return a.activeLink.End, true
	}
	corner, _, _ := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)
	return corner, true
}

// Path returns the full 3D smoothed waypoint path (including slope/ramp transitions) from the
// agent's current position to its target along its active corridor, or nil when no valid corridor exists.
func (a *NavAgent) Path() []math3d.Vec3 {
	if a.mesh == nil || !a.hasTarget || len(a.corridor) == 0 {
		return nil
	}
	if len(a.corridor) == 1 {
		if a.pos.ApproxEqual(a.target, 1e-5) {
			return []math3d.Vec3{a.pos}
		}
		return []math3d.Vec3{a.pos, a.target}
	}
	path, _ := a.mesh.smoothCorridorWithLinks(a.corridor, a.portals, a.pos, a.target)
	return path
}

// Step advances the 3D agent by fixed duration dt: ticks replan cooldown, detects mesh changes,
// repairs the corridor locally or replans if needed, steers toward the next funnel corner while
// tracking surface elevation, traverses off-mesh/flagged links, and settles cleanly on arrival.
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
	if a.mesh == nil || len(a.mesh.Polys) == 0 {
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

	if a.mesh == nil || len(a.mesh.Polys) == 0 {
		a.corridor = a.corridor[:0]
		a.portals = a.portals[:0]
		a.state = NavAgentUnreachable
		return false
	}
	a.meshRevision = a.mesh.Revision()

	filter := a.navFilter()
	start := a.pos
	if _, ok := a.mesh.FindPolygonWithFilter(start, filter); !ok {
		if _, clamped, found := a.mesh.NearestPolygonWithFilter(start, filter); found {
			start = clamped
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
		a.vel = math3d.Vec3{}
		a.state = NavAgentUnreachable
		return false
	}

	a.corridor = append(a.corridor[:0], polys...)
	a.portals = append(a.portals[:0], portals...)
	goalPoly := a.corridor[len(a.corridor)-1]
	a.target.Y = a.mesh.Polys[goalPoly].HeightAt(a.target.X, a.target.Z)
	if len(a.corridor) == 1 && a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
		a.state = NavAgentArrived
		a.vel = math3d.Vec3{}
	} else {
		a.state = NavAgentMoving
	}
	return true
}

func (a *NavAgent) stepMovement(dt time.Duration) {
	if !a.hasTarget || a.mesh == nil || len(a.mesh.Polys) == 0 {
		return
	}
	if a.state == NavAgentIdle || a.state == NavAgentUnreachable {
		return
	}
	if len(a.corridor) == 0 {
		return
	}

	if a.traversingLink {
		a.stepLinkTraversal(dt)
		return
	}

	if a.state == NavAgentArrived {
		if a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
			a.vel = math3d.Vec3{}
			return
		}
		a.state = NavAgentMoving
	}

	secs := dt.Seconds()
	corner, isGoal, offMeshIdx := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)

	if offMeshIdx == 0 && len(a.portals) > 0 {
		link := a.portals[0]
		radius, area, _, userData := a.mesh.offMeshMeta(link)
		entryRadius := max(float64(a.cfg.StoppingDistance), float64(a.cfg.MaxSpeed)*secs*0.5, float64(radius))
		if float64(a.pos.Dist(link.Left)) <= entryRadius {
			a.beginLinkTraversal(NavLinkEvent{
				FromPoly: a.corridor[0],
				ToPoly:   link.Neighbor,
				Start:    link.Left,
				End:      link.End,
				Radius:   radius,
				Area:     area,
				Flags:    link.Flags,
				LinkID:   link.LinkID,
				UserData: userData,
				OffMesh:  true,
			})
			a.stepLinkTraversal(dt)
			return
		}
	}

	if isGoal && a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
		a.state = NavAgentArrived
		a.vel = math3d.Vec3{}
		return
	}

	// Horizontal XZ steering; Y is driven by the walked polygon surface.
	dx := float64(corner.X - a.pos.X)
	dz := float64(corner.Z - a.pos.Z)
	dXZ := math.Hypot(dx, dz)

	var desVX, desVZ float64
	if dXZ > 1e-9 {
		speed := float64(a.cfg.MaxSpeed)
		if isGoal {
			d3 := float64(a.pos.Dist(a.target))
			if slow := float64(a.cfg.ArrivalRadius); slow > 0 && d3 < slow {
				speed *= d3 / slow
			}
			if maxStepSpeed := dXZ / secs; speed > maxStepSpeed {
				speed = maxStepSpeed
			}
		} else if offMeshIdx >= 0 {
			if maxStepSpeed := dXZ / secs; speed > maxStepSpeed {
				speed = maxStepSpeed
			}
		}
		desVX = dx * (speed / dXZ)
		desVZ = dz * (speed / dXZ)
	}

	reqAX := (desVX - float64(a.vel.X)) / secs
	reqAZ := (desVZ - float64(a.vel.Z)) / secs
	ax, az := clampXZ(reqAX, reqAZ, float64(a.cfg.MaxAcceleration))
	vx, vz := clampXZ(float64(a.vel.X)+ax*secs, float64(a.vel.Z)+az*secs, float64(a.cfg.MaxSpeed))

	nextPos := math3d.Vec3{
		X: float32(float64(a.pos.X) + vx*secs),
		Y: a.pos.Y,
		Z: float32(float64(a.pos.Z) + vz*secs),
	}
	nextVel := math3d.Vec3{X: float32(vx), Y: 0, Z: float32(vz)}

	a.advancePosition(nextPos, nextVel, dt)

	if !a.traversingLink && len(a.corridor) > 0 {
		_, nowGoal, _ := a.mesh.nextFunnelCorner(a.pos, a.target, a.portals)
		if nowGoal && a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
			a.state = NavAgentArrived
			a.vel = math3d.Vec3{}
		}
	}
}

func clampXZ(x, z, maxLen float64) (float64, float64) {
	if maxLen <= 0 {
		return x, z
	}
	l := math.Hypot(x, z)
	if l > maxLen {
		s := maxLen / l
		return x * s, z * s
	}
	return x, z
}

func (a *NavAgent) advancePosition(nextPos, nextVel math3d.Vec3, dt time.Duration) {
	curPoly := a.corridor[0]

	// 1. Stayed inside current polygon on XZ.
	if a.mesh.Polys[curPoly].ContainsXZ(nextPos.X, nextPos.Z) {
		nextPos.Y = a.mesh.Polys[curPoly].HeightAt(nextPos.X, nextPos.Z)
		a.pos = nextPos
		a.vel = nextVel
		return
	}

	// 2. Crossed into the next polygon in the corridor (shared-edge surface continuity preserves multi-level separation).
	if len(a.corridor) > 1 {
		link := a.portals[0]
		if !link.OffMesh && a.mesh.Polys[a.corridor[1]].ContainsXZ(nextPos.X, nextPos.Z) {
			if link.Flags != 0 && a.cfg.OnLink != nil {
				cross := closestPointOnSegment3D(a.pos, link.Left, link.Right)
				endPt := nextPos
				endPt.Y = a.mesh.Polys[a.corridor[1]].HeightAt(endPt.X, endPt.Z)
				a.beginLinkTraversal(NavLinkEvent{
					FromPoly: curPoly,
					ToPoly:   link.Neighbor,
					Start:    cross,
					End:      endPt,
					Flags:    link.Flags,
					LinkID:   link.LinkID,
					OffMesh:  false,
				})
				a.stepLinkTraversal(dt)
				return
			}
			nextPoly := a.corridor[1]
			a.corridor = a.corridor[1:]
			a.portals = a.portals[1:]
			nextPos.Y = a.mesh.Polys[nextPoly].HeightAt(nextPos.X, nextPos.Z)
			a.pos = nextPos
			a.vel = nextVel
			return
		}
	}

	// 3. Crossed multiple thin polygons in one step along the corridor (up to first off-mesh or flagged link).
	for k := 2; k < len(a.corridor); k++ {
		prevLink := a.portals[k-1]
		if prevLink.OffMesh || (prevLink.Flags != 0 && a.cfg.OnLink != nil) {
			break
		}
		if a.mesh.Polys[a.corridor[k]].ContainsXZ(nextPos.X, nextPos.Z) {
			nextPoly := a.corridor[k]
			a.corridor = a.corridor[k:]
			a.portals = a.portals[k:]
			nextPos.Y = a.mesh.Polys[nextPoly].HeightAt(nextPos.X, nextPos.Z)
			a.pos = nextPos
			a.vel = nextVel
			return
		}
	}

	// 4. Stepped into an adjacent non-corridor shared-edge neighbor polygon.
	for _, link := range a.mesh.Polys[curPoly].Links {
		if link.OffMesh || !a.allowsPoly(link.Neighbor) {
			continue
		}
		if a.mesh.Polys[link.Neighbor].ContainsXZ(nextPos.X, nextPos.Z) {
			nextPos.Y = a.mesh.Polys[link.Neighbor].HeightAt(nextPos.X, nextPos.Z)
			a.pos = nextPos
			a.vel = nextVel
			if !a.repairCorridorStart(link.Neighbor, true) {
				a.needsReplan = true
			}
			return
		}
	}

	// 5. Stepped into another polygon within VerticalTolerance (e.g. across a thin triangle).
	if fp, ok := a.mesh.FindPolygonWithFilter(nextPos, a.navFilter()); ok {
		nextPos.Y = a.mesh.Polys[fp].HeightAt(nextPos.X, nextPos.Z)
		a.pos = nextPos
		a.vel = nextVel
		if !a.repairCorridorStart(fp, true) {
			a.needsReplan = true
		}
		return
	}

	// 6. Stepped slightly outside the navmesh boundary: clamp to closest point on curPoly or its
	// connected non-off-mesh neighbors and slide XZ velocity tangentially along the wall edge.
	bestPoly := curPoly
	bestPt := a.mesh.polyClosestPoint(curPoly, nextPos)
	bestDistSq := distXZSq(nextPos, bestPt)

	for _, link := range a.mesh.Polys[curPoly].Links {
		if link.OffMesh || !a.allowsPoly(link.Neighbor) {
			continue
		}
		cp := a.mesh.polyClosestPoint(link.Neighbor, nextPos)
		d2 := distXZSq(nextPos, cp)
		better := d2 < bestDistSq-1e-10
		if !better && len(a.corridor) > 1 && link.Neighbor == a.corridor[1] && math.Abs(float64(d2-bestDistSq)) <= 1e-10 {
			better = true
		}
		if better {
			bestDistSq = d2
			bestPoly = link.Neighbor
			bestPt = cp
		}
	}

	pushX := float64(bestPt.X - nextPos.X)
	pushZ := float64(bestPt.Z - nextPos.Z)
	if plen := math.Hypot(pushX, pushZ); plen > 1e-9 {
		nx, nz := pushX/plen, pushZ/plen
		vn := float64(nextVel.X)*nx + float64(nextVel.Z)*nz
		if vn < 0 {
			nextVel.X -= float32(vn * nx)
			nextVel.Z -= float32(vn * nz)
		}
	}
	bestPt.Y = a.mesh.Polys[bestPoly].HeightAt(bestPt.X, bestPt.Z)
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
	a.vel = math3d.Vec3{}
}

func (a *NavAgent) stepLinkTraversal(dt time.Duration) {
	ev := a.activeLink
	if a.cfg.OnLink != nil {
		if !a.cfg.OnLink(a, ev, dt) {
			return
		}
		a.traversingLink = false
		if ev.ToPoly >= 0 && ev.ToPoly < len(a.mesh.Polys) && !a.polyContains3D(ev.ToPoly, a.pos) {
			a.pos = ev.End
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

	// Default off-mesh link traversal: move linearly in 3D toward End at MaxSpeed.
	diff := ev.End.Sub(a.pos)
	d := float64(diff.Len())
	stepDist := float64(a.cfg.MaxSpeed) * dt.Seconds()
	if d <= stepDist || d <= 1e-6 {
		a.pos = ev.End
		a.vel = math3d.Vec3{}
		a.traversingLink = false
		if len(a.corridor) > 1 && a.corridor[1] == ev.ToPoly {
			a.corridor = a.corridor[1:]
			a.portals = a.portals[1:]
		}
		return
	}
	inv := float32(1 / d)
	a.vel = diff.Scale(a.cfg.MaxSpeed * inv)
	a.pos = a.pos.Add(diff.Scale(float32(stepDist) * inv))
}

func (a *NavAgent) verticalTol() float32 {
	if a.mesh != nil && a.mesh.VerticalTolerance > 0 {
		return a.mesh.VerticalTolerance
	}
	return defaultVerticalTolerance
}

func (a *NavAgent) polyContains3D(polyIdx int, p math3d.Vec3) bool {
	if a.mesh == nil || polyIdx < 0 || polyIdx >= len(a.mesh.Polys) {
		return false
	}
	poly := &a.mesh.Polys[polyIdx]
	if !poly.ContainsXZ(p.X, p.Z) {
		return false
	}
	dy := float32(math.Abs(float64(p.Y - poly.HeightAt(p.X, p.Z))))
	return dy <= a.verticalTol()
}

// syncStartPolygon resolves the polygon currently containing a.pos (respecting vertical separation)
// and repairs the corridor start locally without running a full A* replan.
func (a *NavAgent) syncStartPolygon(countRepair bool) bool {
	if a.mesh == nil || len(a.corridor) == 0 {
		return false
	}
	cur := a.corridor[0]
	if a.polyContains3D(cur, a.pos) {
		a.pos.Y = a.mesh.Polys[cur].HeightAt(a.pos.X, a.pos.Z)
		return true
	}

	// Check connected shared-edge neighbors of corridor[0] first so multi-level surfaces stay locked to their floor.
	foundPoly := -1
	if cur >= 0 && cur < len(a.mesh.Polys) {
		for _, link := range a.mesh.Polys[cur].Links {
			if !link.OffMesh && a.allowsPoly(link.Neighbor) && a.polyContains3D(link.Neighbor, a.pos) {
				foundPoly = link.Neighbor
				break
			}
		}
	}
	filter := a.navFilter()
	if foundPoly < 0 {
		if idx, ok := a.mesh.FindPolygonWithFilter(a.pos, filter); ok {
			foundPoly = idx
		}
	}
	if foundPoly < 0 {
		if idx, _, ok := a.mesh.NearestPolygonWithFilter(a.pos, filter); ok {
			foundPoly = idx
		} else {
			return false
		}
	}
	if a.repairCorridorStart(foundPoly, countRepair) {
		if a.mesh.Polys[foundPoly].ContainsXZ(a.pos.X, a.pos.Z) {
			a.pos.Y = a.mesh.Polys[foundPoly].HeightAt(a.pos.X, a.pos.Z)
		}
		return true
	}
	return false
}

// repairCorridorStart updates a.corridor so it begins at curPoly without running a full A* replan.
func (a *NavAgent) repairCorridorStart(curPoly int, countRepair bool) bool {
	if len(a.corridor) == 0 || curPoly < 0 || curPoly >= len(a.mesh.Polys) {
		return false
	}
	if a.corridor[0] == curPoly {
		return true
	}

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

	corridorIdx := make(map[int]int, len(a.corridor))
	for i, p := range a.corridor {
		corridorIdx[p] = i
	}

	type bfsNode struct {
		poly      int
		parentIdx int
		link      NavLink
		depth     int
	}
	maxDepth := a.cfg.MaxRepairDepth
	queue := make([]bfsNode, 0, 16)
	queue = append(queue, bfsNode{poly: curPoly, parentIdx: -1, depth: 0})
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
		for _, link := range a.mesh.Polys[node.poly].Links {
			if link.OffMesh || !a.allowsPoly(link.Neighbor) {
				continue
			}
			nb := link.Neighbor
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
						link:      link,
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
				link:      link,
				depth:     node.depth + 1,
			})
		}
	}

	if !bestFound {
		return false
	}

	var bridgePolys []int
	var bridgeLinks []NavLink
	for idx := bestNodeIdx; idx >= 0; idx = queue[idx].parentIdx {
		n := queue[idx]
		if n.parentIdx >= 0 {
			bridgeLinks = append(bridgeLinks, n.link)
		}
		if idx != bestNodeIdx {
			bridgePolys = append(bridgePolys, n.poly)
		}
	}
	slices.Reverse(bridgePolys)
	slices.Reverse(bridgeLinks)

	newCorridor := make([]int, 0, len(bridgePolys)+len(a.corridor)-bestCorridorK)
	newCorridor = append(newCorridor, bridgePolys...)
	newCorridor = append(newCorridor, a.corridor[bestCorridorK:]...)

	newPortals := make([]NavLink, 0, len(bridgeLinks)+len(a.portals)-bestCorridorK)
	newPortals = append(newPortals, bridgeLinks...)
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
	if n == 0 || goalPoly < 0 || goalPoly >= len(a.mesh.Polys) {
		return false
	}
	lastPoly := a.corridor[n-1]
	if lastPoly == goalPoly {
		return true
	}

	for k := 0; k < n-1; k++ {
		if a.corridor[k] == goalPoly {
			a.corridor = a.corridor[:k+1]
			a.portals = a.portals[:k]
			a.localRepairs++
			a.tailExtensions = 0
			return true
		}
	}

	type bfsNode struct {
		poly      int
		parentIdx int
		link      NavLink
		depth     int
	}
	maxDepth := a.cfg.MaxRepairDepth
	queue := make([]bfsNode, 0, 16)
	queue = append(queue, bfsNode{poly: lastPoly, parentIdx: -1, depth: 0})
	visited := make([]int, 0, 16)
	visited = append(visited, lastPoly)

	foundIdx := -1
	for head := 0; head < len(queue) && foundIdx < 0; head++ {
		node := queue[head]
		if node.depth >= maxDepth {
			continue
		}
		for _, link := range a.mesh.Polys[node.poly].Links {
			if link.OffMesh || !a.allowsPoly(link.Neighbor) {
				continue
			}
			nb := link.Neighbor
			if nb == goalPoly {
				foundIdx = len(queue)
				queue = append(queue, bfsNode{
					poly:      nb,
					parentIdx: head,
					link:      link,
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
				link:      link,
				depth:     node.depth + 1,
			})
		}
	}

	if foundIdx < 0 {
		return false
	}

	var extPolys []int
	var extLinks []NavLink
	for idx := foundIdx; idx > 0; idx = queue[idx].parentIdx {
		extPolys = append(extPolys, queue[idx].poly)
		extLinks = append(extLinks, queue[idx].link)
	}
	slices.Reverse(extPolys)
	slices.Reverse(extLinks)

	for i, ep := range extPolys {
		loopIdx := slices.Index(a.corridor, ep)
		if loopIdx >= 0 {
			a.corridor = a.corridor[:loopIdx+1]
			a.portals = a.portals[:loopIdx]
			continue
		}
		a.corridor = append(a.corridor, ep)
		a.portals = append(a.portals, extLinks[i])
	}

	a.localRepairs++
	a.tailExtensions++
	if a.tailExtensions >= a.cfg.MaxRepairDepth {
		a.needsReplan = true
	}
	return true
}

// NavAgentGroupConfig configures budgeted and staggered replanning across a group of 3D NavAgents.
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

// NavAgentGroup coordinates multiple 3D NavAgents with throttled, round-robin budgeted, and
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

// NewNavAgentGroup constructs a 3D NavAgentGroup with the given replan budget and staggering settings.
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

// Add creates a new 3D NavAgent, registers it in the group, and returns it.
func (g *NavAgentGroup) Add(mesh *NavMesh, pos math3d.Vec3, cfg NavAgentConfig) *NavAgent {
	a := NewNavAgent(mesh, pos, cfg)
	g.AddAgent(a)
	return a
}

// AddAgent attaches an existing 3D NavAgent to the group.
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

// Step advances every 3D agent in the group by dt while strictly bounding full A* replans to
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

	for _, a := range g.agents {
		a.tickPreStep(dt)
	}

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

	for _, a := range g.agents {
		a.stepMovement(dt)
	}
}

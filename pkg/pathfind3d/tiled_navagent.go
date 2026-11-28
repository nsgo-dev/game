package pathfind3d

import (
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
)

// TiledNavAgentState describes a tiled agent's movement state.
type TiledNavAgentState uint8

const (
	TiledNavAgentIdle TiledNavAgentState = iota
	TiledNavAgentMoving
	TiledNavAgentArrived
	TiledNavAgentUnreachable
)

// TiledNavAgentConfig configures deterministic movement on a TiledNavMesh.
type TiledNavAgentConfig struct {
	MaxSpeed         float32
	StoppingDistance float32
	ReplanInterval   time.Duration
	Filter           *NavFilter
}

// TiledNavAgent follows a corridor of NavPolyRefs across streamed and dynamic
// tiles. A tile watcher marks the corridor stale, so only agents crossing the
// changed tile replan on their next fixed step.
type TiledNavAgent struct {
	mesh        *TiledNavMesh
	cfg         TiledNavAgentConfig
	pos, target math3d.Vec3
	hasTarget   bool
	state       TiledNavAgentState
	path        []math3d.Vec3
	result      TiledNavPathResult
	watch       NavPathWatchID
	needsReplan bool
	cooldown    time.Duration
	replans     int
}

func sanitizeTiledAgentConfig(c TiledNavAgentConfig) TiledNavAgentConfig {
	if !isFinite32(c.MaxSpeed) || c.MaxSpeed <= 0 {
		c.MaxSpeed = 4
	}
	if !isFinite32(c.StoppingDistance) || c.StoppingDistance <= 0 {
		c.StoppingDistance = 0.1
	}
	if c.ReplanInterval < 0 {
		c.ReplanInterval = 0
	}
	return c
}

// NewTiledNavAgent constructs an agent at pos. Its position is snapped to the
// nearest walkable surface when the mesh contains one.
func NewTiledNavAgent(mesh *TiledNavMesh, pos math3d.Vec3, cfg TiledNavAgentConfig) *TiledNavAgent {
	cfg = sanitizeTiledAgentConfig(cfg)
	a := &TiledNavAgent{mesh: mesh, pos: pos, cfg: cfg, state: TiledNavAgentIdle}
	if mesh != nil {
		if p, _, ok := mesh.NearestPointWithFilter(pos, math3d.Vec3{X: 2, Y: 2, Z: 2}, a.filter()); ok {
			a.pos = p
		}
	}
	return a
}
func (a *TiledNavAgent) filter() NavFilter {
	if a.cfg.Filter != nil {
		return *a.cfg.Filter
	}
	return DefaultNavFilter()
}
func (a *TiledNavAgent) Pos() math3d.Vec3          { return a.pos }
func (a *TiledNavAgent) State() TiledNavAgentState { return a.state }
func (a *TiledNavAgent) Target() math3d.Vec3       { return a.target }
func (a *TiledNavAgent) HasTarget() bool           { return a.hasTarget }
func (a *TiledNavAgent) NeedsReplan() bool         { return a.needsReplan }
func (a *TiledNavAgent) ReplanCount() int          { return a.replans }
func (a *TiledNavAgent) Corridor() []NavPolyRef {
	return append([]NavPolyRef(nil), a.result.Corridor...)
}
func (a *TiledNavAgent) Path() []math3d.Vec3 { return append([]math3d.Vec3(nil), a.path...) }

// SetMesh changes the streamed mesh and invalidates the current corridor.
func (a *TiledNavAgent) SetMesh(mesh *TiledNavMesh) {
	a.unwatch()
	a.mesh = mesh
	a.result = TiledNavPathResult{}
	a.path = a.path[:0]
	if a.hasTarget {
		a.needsReplan = true
		a.state = TiledNavAgentMoving
	}
}

// SetPosition updates the position after physics or a teleport.
func (a *TiledNavAgent) SetPosition(pos math3d.Vec3) {
	if isFiniteVec3(pos) {
		a.pos = pos
		if a.hasTarget {
			a.needsReplan = true
		}
	}
}

// SetTarget sets the destination and schedules a path search.
func (a *TiledNavAgent) SetTarget(target math3d.Vec3) {
	if !isFiniteVec3(target) {
		return
	}
	a.target = target
	a.hasTarget = true
	a.needsReplan = true
	a.state = TiledNavAgentMoving
	if a.cooldown <= 0 {
		a.replan()
	}
}

// ClearTarget stops movement and removes the tile watcher.
func (a *TiledNavAgent) ClearTarget() {
	a.unwatch()
	a.hasTarget = false
	a.target = math3d.Vec3{}
	a.path = a.path[:0]
	a.result = TiledNavPathResult{}
	a.state = TiledNavAgentIdle
	a.needsReplan = false
}

func (a *TiledNavAgent) unwatch() {
	if a.mesh != nil && a.watch != 0 {
		a.mesh.UnwatchPath(a.watch)
	}
	a.watch = 0
}
func (a *TiledNavAgent) replan() {
	a.needsReplan = false
	a.cooldown = a.cfg.ReplanInterval
	a.replans++
	a.unwatch()
	if a.mesh == nil {
		a.path = a.path[:0]
		a.state = TiledNavAgentUnreachable
		return
	}
	a.result = a.mesh.FindPathWithFilterAndOptions(a.pos, a.target, a.filter(), pathfind.PathOptions{})
	if !a.result.Found || len(a.result.Path) == 0 {
		a.path = a.path[:0]
		a.state = TiledNavAgentUnreachable
		return
	}
	a.path = append(a.path[:0], a.result.Path...)
	a.watch = a.mesh.WatchPath(a.result.TileCoords(), func(NavTileChange) { a.needsReplan = true })
	a.state = TiledNavAgentMoving
}

// Step advances movement by one fixed duration and replans only after a tile
// watcher or target change invalidates this agent's corridor.
func (a *TiledNavAgent) Step(dt time.Duration) {
	if a == nil || dt <= 0 {
		return
	}
	if a.cooldown > 0 {
		a.cooldown -= dt
		if a.cooldown < 0 {
			a.cooldown = 0
		}
	}
	if !a.hasTarget {
		return
	}
	if a.needsReplan && a.cooldown <= 0 {
		a.replan()
	}
	if a.state != TiledNavAgentMoving || len(a.path) == 0 {
		return
	}
	if a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
		a.pos = a.target
		a.state = TiledNavAgentArrived
		return
	}
	next := a.path[0]
	if len(a.path) > 1 && a.pos.Dist(next) <= a.cfg.StoppingDistance {
		a.path = a.path[1:]
		next = a.path[0]
	}
	distance := float64(a.cfg.MaxSpeed) * dt.Seconds()
	delta := next.Sub(a.pos)
	length := float64(delta.Len())
	if length <= distance || length <= float64(a.cfg.StoppingDistance) {
		a.pos = next
		if len(a.path) > 1 {
			a.path = a.path[1:]
		}
	} else {
		a.pos = a.pos.Add(delta.Scale(float32(distance / length)))
	}
	if a.pos.Dist(a.target) <= a.cfg.StoppingDistance {
		a.pos = a.target
		a.state = TiledNavAgentArrived
	}
}

// Close releases the tile watcher. It is safe to call more than once.
func (a *TiledNavAgent) Close() {
	if a != nil {
		a.unwatch()
	}
}

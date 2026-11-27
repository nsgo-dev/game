package rts

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
)

// UnitID identifies a unit in a Crowd. IDs start at 1 and are never reused, so 0 is never a valid ID.
type UnitID uint32

// MoveState is where a unit is in carrying out its move order.
type MoveState uint8

// The move states.
const (
	// Idle units have no order and only keep clear of their neighbors.
	Idle MoveState = iota
	// Moving units are on their way to their target.
	Moving
	// Arrived units reached their target and hold it.
	Arrived
)

// String returns the state's name.
func (s MoveState) String() string {
	switch s {
	case Idle:
		return "idle"
	case Moving:
		return "moving"
	case Arrived:
		return "arrived"
	}
	return "unknown"
}

// UnitSpec describes a unit to add to a Crowd.
type UnitSpec struct {
	// Pos is the unit's starting position in world units.
	Pos math2d.Vec2
	// Radius is the unit's body radius; 0 or less uses a quarter of the crowd's cell size.
	Radius float32
	// MaxSpeed is the unit's top speed in world units per second; 0 or less uses 4 cells per second.
	MaxSpeed float32
	// MaxAccel caps how fast the unit changes velocity, in world units per second squared; 0 or less uses four times MaxSpeed.
	MaxAccel float32
	// Kind is a game-defined unit type, used to select every unit of the same type with a double click.
	Kind int
}

// Unit is a unit's state as the crowd last stepped it. Read units through Crowd.Units or Crowd.Unit; change them only through the Crowd's methods.
type Unit struct {
	ID       UnitID
	Pos      math2d.Vec2
	Vel      math2d.Vec2
	Radius   float32
	MaxSpeed float32
	MaxAccel float32
	Kind     int
	// State is the unit's progress on its order.
	State MoveState
	// Target is the position the unit's order sends it to; for an idle unit, its position when it went idle.
	Target math2d.Vec2

	field    *FlowField
	path     []pathfind.Cell // the detour's path from where it was planned to the unit's slot
	pathIdx  int
	speedCap float32
	direct   bool
	detour   bool // the unit follows its own path to its slot instead of the group's field
}

// AvoidanceMode selects how units in a Crowd avoid colliding with each other.
type AvoidanceMode uint8

// The avoidance modes.
const (
	// AvoidancePush pushes overlapping units apart (the default).
	AvoidancePush AvoidanceMode = iota
	// AvoidanceORCA computes collision-free velocities ahead of contact using ai.ORCASolver.
	AvoidanceORCA
)

// String returns the mode's name.
func (m AvoidanceMode) String() string {
	switch m {
	case AvoidancePush:
		return "push"
	case AvoidanceORCA:
		return "orca"
	}
	return "unknown"
}

// CrowdOptions tunes a Crowd. The zero value works for units about half a cell wide.
type CrowdOptions struct {
	// Origin is the world position of the top-left corner of grid cell (0, 0); a non-finite coordinate counts as 0.
	Origin math2d.Vec2
	// CellSize is the width and height of a grid cell in world units; 0 or less, or a non-finite value, uses 1.
	CellSize float32
	// FieldCacheSize is the number of flow fields kept for reuse; 0 or less uses DefaultFieldCacheSize.
	FieldCacheSize int
	// Separation scales the push between overlapping units under AvoidancePush; 0 or less, or a non-finite value, uses 1.
	Separation float32
	// Gap is extra room units keep between their bodies, in world units; negative and non-finite values count as 0.
	Gap float32
	// ArriveRadius is how close a unit gets to its target to count as arrived; 0 or less, or a non-finite value, uses the unit's radius.
	ArriveRadius float32
	// DirectRange is the distance within which a unit that can see its target walks straight to it instead of following its flow field; 0 or less, or a non-finite value, uses 6 cells.
	DirectRange float32
	// Responsiveness is how quickly a unit's velocity turns to its desired velocity, per second; 0 or less, or a non-finite value, uses 8.
	Responsiveness float32
	// Avoidance selects how units keep clear of each other: AvoidancePush (default) pushes overlapping units apart, while AvoidanceORCA uses ai.ORCASolver to steer around neighbors before contact.
	Avoidance AvoidanceMode
	// ORCA tunes the ai.ORCASolver used when Avoidance is AvoidanceORCA. Zero or non-finite fields use ai.ORCASolver defaults.
	ORCA ai.ORCAConfig
}

// Crowd moves many units over a pathfind.Grid. Units follow shared flow fields to their targets, keep apart from each other, and slide along blocked cells and the grid's edge.
//
// Step advances every unit by one fixed step in O(n + b) time for n units, where b is the number of neighbor buckets, each as wide as the largest unit's reach or a cell, covering the units' bounding box: neighbors are found through that bucket grid, rebuilt each step without allocating once its buffers have grown, and each unit reads its flow field in constant time. Forces are computed from the positions at the start of the step for every unit before any unit moves, so the result does not depend on the order units move in within a step. It does depend on the order units were added in, which sets their IDs and the order their pushes are summed, so a replay must add the same units in the same order. A Crowd is not safe for concurrent use.
type Crowd struct {
	grid   pathfind.Grid
	opts   CrowdOptions
	fields *FieldCache

	units  []Unit
	index  map[UnitID]int
	nextID UnitID
	steps  uint64

	accel      []math2d.Vec2
	buckets    bucketGrid
	astar      *pathfind.AStar
	orcaSolver *ai.ORCASolver
	orcaAgents []ai.ORCAAgent
	orcaVels   []math2d.Vec2
}

// NewCrowd returns an empty crowd moving units over grid.
func NewCrowd(grid pathfind.Grid, opts CrowdOptions) *Crowd {
	opts.CellSize = validCellSize(opts.CellSize)
	if !finite(opts.Origin.X) {
		opts.Origin.X = 0
	}
	if !finite(opts.Origin.Y) {
		opts.Origin.Y = 0
	}
	if !(opts.Separation > 0) || !finite(opts.Separation) {
		opts.Separation = 1
	}
	if !(opts.Gap >= 0) || !finite(opts.Gap) {
		opts.Gap = 0
	}
	if !(opts.ArriveRadius > 0) || !finite(opts.ArriveRadius) {
		opts.ArriveRadius = 0
	}
	if !(opts.DirectRange > 0) || !finite(opts.DirectRange) {
		opts.DirectRange = 6 * opts.CellSize
	}
	if !(opts.Responsiveness > 0) || !finite(opts.Responsiveness) {
		opts.Responsiveness = 8
	}
	if opts.Avoidance != AvoidanceORCA {
		opts.Avoidance = AvoidancePush
	}
	var orcaSolver *ai.ORCASolver
	if opts.Avoidance == AvoidanceORCA {
		orcaSolver = ai.NewORCASolver(opts.ORCA)
		opts.ORCA = orcaSolver.Config()
	}
	return &Crowd{
		grid:       grid,
		opts:       opts,
		fields:     NewFieldCache(grid, opts.Origin, opts.CellSize, opts.FieldCacheSize),
		index:      make(map[UnitID]int),
		orcaSolver: orcaSolver,
	}
}

// Grid returns the grid the crowd moves over.
func (c *Crowd) Grid() pathfind.Grid { return c.grid }

// Fields returns the crowd's flow field cache. After changing the grid, call the Crowd's Rebuild rather than the cache's, so fields the cache has since dropped but units still follow are rebuilt too.
func (c *Crowd) Fields() *FieldCache { return c.fields }

// Rebuild integrates every flow field the crowd's units follow, and every field in its cache, again over the grid, in place, so the units see a change to the grid (a wall built, a bridge destroyed) on their next step. Call it after changing the grid's walkable cells or costs.
//
// Units whose own paths around walls were planned on the old grid plan them again. Units whose field no longer has a walkable goal, because every cell their order sent them to is now blocked, stop where they stand, as after Stop.
func (c *Crowd) Rebuild() {
	done := make(map[*FlowField]bool, len(c.fields.entries))
	for _, e := range c.fields.entries {
		done[e.field] = true
	}
	c.fields.Rebuild()
	for i := range c.units {
		u := &c.units[i]
		u.path, u.pathIdx = u.path[:0], 0
		f := u.field
		if f == nil || done[f] {
			continue
		}
		done[f] = true
		c.fields.builds++
		f.Build(c.grid, f.request...)
	}
	for i := range c.units {
		if u := &c.units[i]; u.field != nil && len(u.field.Goals()) == 0 {
			c.Stop(u.ID)
		}
	}
}

// Options returns the crowd's options with defaults filled in.
func (c *Crowd) Options() CrowdOptions { return c.opts }

// Steps returns how many times Step has advanced the crowd.
func (c *Crowd) Steps() uint64 { return c.steps }

// Len returns the number of units.
func (c *Crowd) Len() int { return len(c.units) }

// Units returns every unit, in a stable order that changes only when units are added or removed. The slice is the crowd's own: read it, do not change it, and do not keep it across Add or Remove.
func (c *Crowd) Units() []Unit { return c.units }

// Unit returns the unit with id.
func (c *Crowd) Unit(id UnitID) (Unit, bool) {
	i, ok := c.index[id]
	if !ok {
		return Unit{}, false
	}
	return c.units[i], true
}

// Add places a new idle unit and returns its ID.
func (c *Crowd) Add(spec UnitSpec) UnitID {
	cs := c.opts.CellSize
	if !(spec.Radius > 0) || !finite(spec.Radius) {
		spec.Radius = 0.25 * cs
	}
	if !(spec.MaxSpeed > 0) || !finite(spec.MaxSpeed) {
		spec.MaxSpeed = 4 * cs
	}
	if !(spec.MaxAccel > 0) || !finite(spec.MaxAccel) {
		spec.MaxAccel = 4 * spec.MaxSpeed
	}
	if !finite(spec.Pos.X) || !finite(spec.Pos.Y) {
		spec.Pos = math2d.Vec2{}
	}
	c.nextID++
	id := c.nextID
	c.index[id] = len(c.units)
	c.units = append(c.units, Unit{
		ID: id, Pos: spec.Pos, Radius: spec.Radius, MaxSpeed: spec.MaxSpeed, MaxAccel: spec.MaxAccel, Kind: spec.Kind,
		Target: spec.Pos, speedCap: spec.MaxSpeed,
	})
	return id
}

// Remove deletes the unit with id and reports whether it existed. The last unit takes the removed one's place in Units.
func (c *Crowd) Remove(id UnitID) bool {
	i, ok := c.index[id]
	if !ok {
		return false
	}
	last := len(c.units) - 1
	if i != last {
		c.units[i] = c.units[last]
		c.index[c.units[i].ID] = i
	}
	c.units[last] = Unit{}
	c.units = c.units[:last]
	delete(c.index, id)
	return true
}

// Stop drops the orders of the units in ids, leaving them idle where they stand.
func (c *Crowd) Stop(ids ...UnitID) {
	for _, id := range ids {
		if i, ok := c.index[id]; ok {
			u := &c.units[i]
			u.State, u.Target, u.field, u.direct, u.detour = Idle, u.Pos, nil, false, false
			u.path, u.pathIdx = u.path[:0], 0
			u.speedCap = u.MaxSpeed
		}
	}
}

// Centroid returns the mean position of the units in ids that exist, and false when none does.
func (c *Crowd) Centroid(ids []UnitID) (math2d.Vec2, bool) {
	var sum math2d.Vec2
	n := 0
	for _, id := range ids {
		if i, ok := c.index[id]; ok {
			sum = sum.Add(c.units[i].Pos)
			n++
		}
	}
	if n == 0 {
		return math2d.Vec2{}, false
	}
	return sum.Scale(1 / float32(n)), true
}

// UnitsIn appends to out[:0] the IDs of the units whose bodies overlap r, in Units order.
func (c *Crowd) UnitsIn(r math2d.Rect, out []UnitID) []UnitID {
	out = out[:0]
	r = r.Normalized()
	for i := range c.units {
		u := &c.units[i]
		if (math2d.Circle{Center: u.Pos, Radius: u.Radius}).OverlapsRect(r) {
			out = append(out, u.ID)
		}
	}
	return out
}

// UnitAt returns the unit whose body is nearest to p among those within slop world units of it, and false when there is none. Equal distances go to the unit earlier in Units.
func (c *Crowd) UnitAt(p math2d.Vec2, slop float32) (UnitID, bool) {
	if slop < 0 {
		slop = 0
	}
	best := UnitID(0)
	bestDist := float32(math.Inf(1))
	for i := range c.units {
		u := &c.units[i]
		d := u.Pos.Dist(p) - u.Radius
		if d <= slop && d < bestDist {
			best, bestDist = u.ID, d
		}
	}
	return best, best != 0
}

// KindOf returns the Kind of the unit with id.
func (c *Crowd) KindOf(id UnitID) (int, bool) {
	i, ok := c.index[id]
	if !ok {
		return 0, false
	}
	return c.units[i].Kind, true
}

// Step advances every unit by dt.
func (c *Crowd) Step(dt time.Duration) {
	if dt <= 0 {
		return
	}
	c.steps++
	secs := float32(dt.Seconds())
	n := len(c.units)
	if c.opts.Avoidance == AvoidanceORCA {
		c.stepORCA(dt, secs)
		return
	}
	if cap(c.accel) < n {
		c.accel = make([]math2d.Vec2, n)
	}
	c.accel = c.accel[:n]

	maxRadius := float32(0)
	lo := math2d.Vec2{X: float32(math.Inf(1)), Y: float32(math.Inf(1))}
	hi := math2d.Vec2{X: float32(math.Inf(-1)), Y: float32(math.Inf(-1))}
	for i := range c.units {
		u := &c.units[i]
		maxRadius = max(maxRadius, u.Radius)
		lo = math2d.Vec2{X: min(lo.X, u.Pos.X), Y: min(lo.Y, u.Pos.Y)}
		hi = math2d.Vec2{X: max(hi.X, u.Pos.X), Y: max(hi.Y, u.Pos.Y)}
	}
	if n == 0 {
		lo, hi = math2d.Vec2{}, math2d.Vec2{}
	}
	// The buckets cover only the units' bounding box, so a step's cost follows the units rather than the size of the map.
	reach := 2*maxRadius + c.opts.Gap
	c.buckets.rebuild(c.units, lo, max(reach, c.opts.CellSize), hi.X-lo.X, hi.Y-lo.Y)

	for i := range c.units {
		c.accel[i] = c.steer(i)
	}
	for i := range c.units {
		u := &c.units[i]
		v := u.Vel.Add(c.accel[i].Scale(secs)).ClampLen(u.speedCap)
		if u.State != Moving && v.LenSq() < 1e-8 {
			v = math2d.Vec2{}
		}
		u.Vel = v
		u.Pos = u.Pos.Add(v.Scale(secs))
		c.slide(u)
		c.settle(u)
	}
}

func (c *Crowd) stepORCA(dt time.Duration, secs float32) {
	n := len(c.units)
	if n == 0 {
		return
	}
	if c.orcaSolver == nil {
		c.orcaSolver = ai.NewORCASolver(c.opts.ORCA)
	}
	if cap(c.orcaAgents) < n {
		c.orcaAgents = make([]ai.ORCAAgent, n)
	}
	c.orcaAgents = c.orcaAgents[:n]
	halfGap := 0.5 * c.opts.Gap
	for i := range c.units {
		u := &c.units[i]
		pref := c.desired(u)
		vel := u.Vel
		prio := float32(1)
		if u.State == Moving {
			// Moving units take priority over idle or arrived units so settled formations
			// part politely for teammates reaching interior slots. If a moving unit stalls
			// against neighbors, signal gentle intent along pref so teammates step aside.
			prio = 3
			if prefLenSq := pref.LenSq(); prefLenSq > 1e-6 && vel.LenSq() < 0.04*prefLenSq {
				vel = pref.Scale(0.25)
			}
		}
		c.orcaAgents[i] = ai.ORCAAgent{
			ID:       uint64(u.ID),
			Pos:      u.Pos,
			Vel:      vel,
			PrefVel:  pref,
			Radius:   u.Radius + halfGap,
			MaxSpeed: u.speedCap,
			Priority: prio,
		}
	}
	c.orcaVels = c.orcaSolver.Solve(c.orcaVels, c.orcaAgents, nil, dt)
	invSecs := 1 / secs
	for i := range c.units {
		u := &c.units[i]
		acc := c.orcaVels[i].Sub(u.Vel).Scale(invSecs).ClampLen(u.MaxAccel)
		v := u.Vel.Add(acc.Scale(secs)).ClampLen(u.speedCap)
		if u.State != Moving && v.LenSq() < 1e-8 {
			v = math2d.Vec2{}
		}
		u.Vel = v
		u.Pos = u.Pos.Add(v.Scale(secs))
		c.slide(u)
		c.settle(u)
	}
}

// steer returns unit i's acceleration for this step: toward its desired velocity, plus the push of the neighbors it overlaps, clamped to MaxAccel.
func (c *Crowd) steer(i int) math2d.Vec2 {
	u := &c.units[i]
	desired := c.desired(u)
	acc := desired.Sub(u.Vel).Scale(c.opts.Responsiveness)

	var push math2d.Vec2
	c.buckets.neighbors(u.Pos, func(j int32) {
		if int(j) == i {
			return
		}
		o := &c.units[j]
		d := u.Pos.Sub(o.Pos)
		reach := u.Radius + o.Radius + c.opts.Gap
		dist2 := d.LenSq()
		if dist2 >= reach*reach {
			return
		}
		var dir math2d.Vec2
		dist := float32(math.Sqrt(float64(dist2)))
		if dist > 1e-6 {
			dir = d.Scale(1 / dist)
		} else {
			// Exactly overlapping units split along a direction fixed by their IDs.
			if u.ID < o.ID {
				dir = math2d.Vec2{X: -1}
			} else {
				dir = math2d.Vec2{X: 1}
			}
		}
		weight := (reach - dist) / reach
		if u.State == Arrived && o.State == Moving {
			// Units holding their place give way less to units still passing through.
			weight *= 0.5
		}
		push = push.Add(dir.Scale(weight))
	})
	acc = acc.Add(push.Scale(c.opts.Separation * u.MaxAccel * 2))
	return acc.ClampLen(u.MaxAccel)
}

// desired returns the velocity unit u wants this step.
func (c *Crowd) desired(u *Unit) math2d.Vec2 {
	if u.State == Idle {
		return math2d.Vec2{}
	}
	toTarget := u.Target.Sub(u.Pos)
	dist := toTarget.Len()
	if u.State == Arrived {
		return arrive(toTarget, dist, u.speedCap, u.Radius)
	}
	// Units near their target, or already walking straight to it, check their line of sight to it on every eighth step, staggered by ID, which bounds the cost of the grid walk.
	if (u.direct || dist <= c.opts.DirectRange) && uint64(u.ID)%8 == c.steps%8 {
		u.direct = c.clearPath(u.Pos, u.Target, u.Radius)
	}
	if u.direct {
		return arrive(toTarget, dist, u.speedCap, 2*u.Radius)
	}
	if u.detour {
		return c.followDetour(u, toTarget, dist)
	}
	if u.field != nil {
		dir, ok := u.field.Sample(u.Pos)
		if ok && dir.LenSq() > 0 {
			return dir.Scale(u.speedCap)
		}
		if ok {
			// The unit stands on one of its group's goal cells, inside the formation's area, but not on its own slot. Walk straight there when nothing is in the way; otherwise the shared field, which leads to the nearest slot rather than this unit's own, cannot help, and the unit plans its own path around the wall for the rest of the order.
			if c.clearPath(u.Pos, u.Target, u.Radius) {
				u.direct = true
				return arrive(toTarget, dist, u.speedCap, 2*u.Radius)
			}
			u.detour = true
			return c.followDetour(u, toTarget, dist)
		}
	}
	return arrive(toTarget, dist, u.speedCap, 2*u.Radius)
}

// planDetour replaces u's path with a pathfind.AStar path from its cell to its target's cell and reports whether one was found.
func (c *Crowd) planDetour(u *Unit) bool {
	u.path, u.pathIdx = u.path[:0], 0
	from, in1 := c.cellAt(u.Pos)
	to, in2 := c.cellAt(u.Target)
	if !in1 || !in2 {
		return false
	}
	if c.astar == nil {
		c.astar = pathfind.NewAStar()
		c.astar.Diagonal = true
	}
	path, _, found := c.astar.FindInto(u.path[:0], c.grid, from, to)
	if found {
		u.path = path
	}
	return found
}

// followDetour steers u along its own path to its slot, a cell path that ends in its target's cell, skipping each waypoint it reaches.
func (c *Crowd) followDetour(u *Unit, toTarget math2d.Vec2, dist float32) math2d.Vec2 {
	// Searches run only on the unit's staggered step, one in eight, which bounds their cost when many units need one.
	replan := uint64(u.ID)%8 == c.steps%8
	if len(u.path) > 0 && replan {
		// A unit pushed off its path, so far or around a corner that it cannot walk straight to its next waypoint, plans afresh from where it stands. Three cells leaves room for the diagonal neighbor's center, up to 2.12 cells away.
		wp := cellCenter(u.path[min(u.pathIdx, len(u.path)-1)], c.opts.Origin, c.opts.CellSize)
		if u.Pos.Dist(wp) > 3*c.opts.CellSize || !c.clearPath(u.Pos, wp, u.Radius) {
			u.path, u.pathIdx = u.path[:0], 0
		}
	}
	if len(u.path) == 0 && (!replan || !c.planDetour(u)) {
		// No path yet, or the last search failed: head straight for the slot until the next try.
		return arrive(toTarget, dist, u.speedCap, 2*u.Radius)
	}
	last := len(u.path) - 1
	// Neighbors can push a unit past a waypoint without it ever coming near the waypoint's center, so a unit standing on a later cell of its path resumes from there instead of turning back.
	if cell, in := c.cellAt(u.Pos); in {
		for k := last; k >= u.pathIdx; k-- {
			if u.path[k] == cell {
				u.pathIdx = k + 1
				break
			}
		}
	}
	reach := 0.5 * c.opts.CellSize
	for u.pathIdx < last {
		wp := cellCenter(u.path[u.pathIdx], c.opts.Origin, c.opts.CellSize)
		if u.Pos.Dist(wp) > reach {
			break
		}
		u.pathIdx++
	}
	if u.pathIdx >= last {
		return arrive(toTarget, dist, u.speedCap, 2*u.Radius)
	}
	to := cellCenter(u.path[u.pathIdx], c.opts.Origin, c.opts.CellSize).Sub(u.Pos)
	return arrive(to, to.Len(), u.speedCap, 0)
}

// arrive returns a velocity toward a target offset by toTarget, at speed up to maxSpeed and slowing linearly inside slowRadius.
func arrive(toTarget math2d.Vec2, dist, maxSpeed, slowRadius float32) math2d.Vec2 {
	if dist < 1e-6 {
		return math2d.Vec2{}
	}
	speed := maxSpeed
	if slowRadius > 0 && dist < slowRadius {
		speed *= dist / slowRadius
	}
	return toTarget.Scale(speed / dist)
}

// settle updates u's move state after it moved.
func (c *Crowd) settle(u *Unit) {
	radius := c.opts.ArriveRadius
	if radius == 0 {
		radius = u.Radius
	}
	dist := u.Pos.Dist(u.Target)
	switch u.State {
	case Moving:
		if dist <= radius {
			u.State = Arrived
		}
	case Arrived:
		// A unit pushed well away from its place walks back along its flow field.
		if dist > 2*c.opts.CellSize+radius {
			u.State = Moving
			u.direct = false
		}
	}
}

func (c *Crowd) gridSize() (cols, rows int) {
	if c.grid == nil {
		return 0, 0
	}
	return c.grid.GridSize()
}

func (c *Crowd) cellAt(p math2d.Vec2) (pathfind.Cell, bool) {
	cols, rows := c.gridSize()
	return cellAt(p, c.opts.Origin, c.opts.CellSize, cols, rows)
}

// walkable reports whether (col, row) is inside the grid and walkable. It checks the bounds itself because pathfind.Grid leaves out-of-bounds cells to each implementation, and a game's own grid may index a slice with them.
func (c *Crowd) walkable(col, row int) bool {
	if c.grid == nil || col < 0 || row < 0 {
		return false
	}
	cols, rows := c.grid.GridSize()
	return col < cols && row < rows && c.grid.Walkable(col, row)
}

// slide pushes u's body out of blocked cells and back inside the grid, dropping the part of its velocity that points into a wall so it slides along it.
func (c *Crowd) slide(u *Unit) {
	u.Pos = c.pushOut(u.Pos, u.Radius, &u.Vel)
}

// pushOut returns p moved so that a body of radius r there stays inside the grid and out of blocked cells. When vel is not nil, the part of *vel pointing into a wall it touched is removed.
func (c *Crowd) pushOut(p math2d.Vec2, r float32, vel *math2d.Vec2) math2d.Vec2 {
	cols, rows := c.gridSize()
	if cols <= 0 || rows <= 0 {
		return p
	}
	cs := c.opts.CellSize
	o := c.opts.Origin
	edge := min(r, cs*0.5)
	p.X = min(max(p.X, o.X+edge), o.X+float32(cols)*cs-edge)
	p.Y = min(max(p.Y, o.Y+edge), o.Y+float32(rows)*cs-edge)

	cell, _ := c.cellAt(p)
	if !c.walkable(cell.Col, cell.Row) {
		// The center is inside a blocked cell: move it to the nearest walkable neighbor.
		if q, ok := c.escape(p, cell, r); ok {
			p = q
			cell, _ = c.cellAt(p)
		}
	}
	// The cells a body can touch, clamped to the grid so a body wider than the map costs no more than the map.
	span := int(min(math.Ceil(float64(r/cs)), float64(max(cols, rows)))) + 1
	for row := max(cell.Row-span, 0); row <= min(cell.Row+span, rows-1); row++ {
		for col := max(cell.Col-span, 0); col <= min(cell.Col+span, cols-1); col++ {
			if c.walkable(col, row) {
				continue
			}
			box := math2d.Rect{X: o.X + float32(col)*cs, Y: o.Y + float32(row)*cs, W: cs, H: cs}
			d := p.Sub(box.ClampPoint(p))
			dist2 := d.LenSq()
			if dist2 >= r*r || dist2 < 1e-12 {
				continue
			}
			dist := float32(math.Sqrt(float64(dist2)))
			n := d.Scale(1 / dist)
			p = p.Add(n.Scale(r - dist))
			if vel != nil {
				if vn := vel.Dot(n); vn < 0 {
					*vel = vel.Sub(n.Scale(vn))
				}
			}
		}
	}
	return p
}

// escape returns the point nearest p, at least radius inside a walkable cell next to the blocked cell holding p.
func (c *Crowd) escape(p math2d.Vec2, cell pathfind.Cell, radius float32) (math2d.Vec2, bool) {
	cs := c.opts.CellSize
	o := c.opts.Origin
	best := math2d.Vec2{}
	bestDist := float32(math.Inf(1))
	for _, st := range fieldSteps {
		col, row := cell.Col+st.dc, cell.Row+st.dr
		if !c.walkable(col, row) {
			continue
		}
		inset := min(radius, cs*0.5)
		box := math2d.Rect{X: o.X + float32(col)*cs + inset, Y: o.Y + float32(row)*cs + inset, W: cs - 2*inset, H: cs - 2*inset}
		q := box.ClampPoint(p)
		if d := q.DistSq(p); d < bestDist {
			best, bestDist = q, d
		}
	}
	return best, bestDist != float32(math.Inf(1))
}

// clearPath reports whether a body of radius r can walk straight from a to b without entering a blocked cell, checked along its center line and both of its edges.
func (c *Crowd) clearPath(a, b math2d.Vec2, r float32) bool {
	d := b.Sub(a)
	if d.LenSq() < 1e-12 {
		return true
	}
	side := math2d.Vec2{X: -d.Y, Y: d.X}.Normalize().Scale(r * 0.9)
	return c.clearLine(a, b) && c.clearLine(a.Add(side), b.Add(side)) && c.clearLine(a.Sub(side), b.Sub(side))
}

// clearLine walks the cells the segment from a to b crosses (Amanatides-Woo) and reports whether all of them are walkable.
func (c *Crowd) clearLine(a, b math2d.Vec2) bool {
	cs := c.opts.CellSize
	o := c.opts.Origin
	x0, y0 := float64((a.X-o.X)/cs), float64((a.Y-o.Y)/cs)
	x1, y1 := float64((b.X-o.X)/cs), float64((b.Y-o.Y)/cs)
	col, row := int(math.Floor(x0)), int(math.Floor(y0))
	endCol, endRow := int(math.Floor(x1)), int(math.Floor(y1))
	dx, dy := x1-x0, y1-y0
	stepC, stepR := 1, 1
	if dx < 0 {
		stepC = -1
	}
	if dy < 0 {
		stepR = -1
	}
	tDeltaX, tDeltaY := math.Inf(1), math.Inf(1)
	tMaxX, tMaxY := math.Inf(1), math.Inf(1)
	if dx != 0 {
		tDeltaX = math.Abs(1 / dx)
		next := math.Floor(x0) + 1
		if dx < 0 {
			next = math.Floor(x0)
		}
		tMaxX = (next - x0) / dx
	}
	if dy != 0 {
		tDeltaY = math.Abs(1 / dy)
		next := math.Floor(y0) + 1
		if dy < 0 {
			next = math.Floor(y0)
		}
		tMaxY = (next - y0) / dy
	}
	limit := abs(endCol-col) + abs(endRow-row) + 1
	for range limit + 1 {
		if !c.walkable(col, row) {
			return false
		}
		if col == endCol && row == endRow {
			return true
		}
		if tMaxX < tMaxY {
			col += stepC
			tMaxX += tDeltaX
		} else {
			row += stepR
			tMaxY += tDeltaY
		}
	}
	return true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// bucketGrid sorts unit indices into square buckets by position (a counting sort), so each unit finds its neighbors in the 3x3 buckets around it.
type bucketGrid struct {
	origin     math2d.Vec2
	size       float32
	cols, rows int
	start      []int32
	cursor     []int32
	items      []int32
	cellOf     []int32
}

// rebuild buckets units over the area width by height world units from origin, in buckets size wide; units outside the area fall into the edge buckets.
func (b *bucketGrid) rebuild(units []Unit, origin math2d.Vec2, size, width, height float32) {
	b.origin, b.size = origin, size
	b.cols = max(1, int(math.Ceil(float64(width/size))))
	b.rows = max(1, int(math.Ceil(float64(height/size))))
	n := b.cols * b.rows
	if cap(b.start) < n+1 {
		// Room for twice as many buckets, so a group spreading out as it moves does not reallocate on every step.
		b.start = make([]int32, n+1, 2*n+1)
		b.cursor = make([]int32, n, 2*n)
	}
	b.start = b.start[:n+1]
	b.cursor = b.cursor[:n]
	clear(b.start)
	if cap(b.items) < len(units) {
		b.items = make([]int32, len(units))
		b.cellOf = make([]int32, len(units))
	}
	b.items = b.items[:len(units)]
	b.cellOf = b.cellOf[:len(units)]
	for i := range units {
		k := b.bucket(units[i].Pos)
		b.cellOf[i] = k
		b.start[k+1]++
	}
	for k := 1; k <= n; k++ {
		b.start[k] += b.start[k-1]
	}
	copy(b.cursor, b.start[:n])
	// Filling in ascending unit order lists each bucket's units in ascending index order.
	for i := range units {
		k := b.cellOf[i]
		b.items[b.cursor[k]] = int32(i)
		b.cursor[k]++
	}
}

func (b *bucketGrid) bucket(p math2d.Vec2) int32 {
	x := int((p.X - b.origin.X) / b.size)
	y := int((p.Y - b.origin.Y) / b.size)
	x = min(max(x, 0), b.cols-1)
	y = min(max(y, 0), b.rows-1)
	return int32(y*b.cols + x)
}

func (b *bucketGrid) neighbors(p math2d.Vec2, fn func(int32)) {
	k := b.bucket(p)
	bx, by := int(k)%b.cols, int(k)/b.cols
	for y := max(by-1, 0); y <= min(by+1, b.rows-1); y++ {
		for x := max(bx-1, 0); x <= min(bx+1, b.cols-1); x++ {
			k := y*b.cols + x
			for _, i := range b.items[b.start[k]:b.start[k+1]] {
				fn(i)
			}
		}
	}
}

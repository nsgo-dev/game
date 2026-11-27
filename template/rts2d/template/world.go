package main

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/fog"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/rts"
	"github.com/nsgo-dev/nsgo/core"
)

// team is a side of the skirmish; it is also the fog of war's team.
type team int

const (
	// player is the side the person plays.
	player team = 0
	// opponent is the side the computer plays.
	opponent team = 1
)

// rival returns the other side.
func (t team) rival() team { return 1 - t }

// orderKind is what a unit is doing.
type orderKind uint8

const (
	// orderIdle units stand and attack enemies they see.
	orderIdle orderKind = iota
	// orderMove units walk to dest and ignore enemies until they arrive.
	orderMove
	// orderAttack units chase and attack target, then go back to attack-moving to dest when resume is set.
	orderAttack
	// orderAttackMove units walk to dest and attack every enemy they see on the way.
	orderAttackMove
	// orderGather workers mine node and carry the crystals to their headquarters.
	orderGather
)

const (
	// moveFormation is the formation of data/formations.yaml ordered groups take.
	moveFormation = "box"
	// fogEvery is how many steps pass between two updates of the fog of war.
	fogEvery = 4
	// scanEvery is how many steps pass between two looks for enemies by an idle or attack-moving unit.
	scanEvery = 10
	// repathSteps is how long a chasing or blocked unit waits before it is ordered again.
	repathSteps = 30
	// threatCells is how close to a headquarters, in cells, an enemy counts as a threat to that base.
	threatCells = 9
)

// entity is a unit or a building.
type entity struct {
	id   int
	team team
	// unit is the unit's type; nil for a building.
	unit *unitRules
	// building is the building's type; nil for a unit.
	building *buildingRules
	hp       int
	maxHP    int
	fogID    fog.SourceID

	// A unit's crowd body and orders.
	body     rts.UnitID
	order    orderKind
	target   int
	resume   bool
	dest     math2d.Vec2
	repath   int
	cooldown int
	// A worker's crystal node (an index into world.nodes, -1 for none), load, mining time left and whether it is carrying the load home.
	node      int
	carry     int
	mining    int
	returning bool

	// A building's top-left cell, construction time left (0 once built), production queue and time left on its first item.
	cell      pathfind.Cell
	buildLeft int
	queue     []*unitRules
	trainLeft int

	// seenBy is what each side last saw of a building: the other side remembers a building it has scouted as it was then, and never sees one placed after it last looked.
	seenBy [2]sighting

	// Drawing state: steps left of the hit flash and of the attack line to shotAt.
	flash  int
	shot   int
	shotAt math2d.Vec2
}

// sighting is a side's memory of a building it does not own.
type sighting struct {
	// seen reports whether the side has seen the building at all.
	seen bool
	// hp and buildLeft are the building's health and construction time left when the side last saw it.
	hp        int
	buildLeft int
}

// alive reports whether e still has health.
func (e *entity) alive() bool { return e.hp > 0 }

// built reports whether e is a finished building.
func (e *entity) built() bool { return e.building != nil && e.buildLeft == 0 }

// fights reports whether e is a unit that deals damage.
func (e *entity) fights() bool { return e.unit != nil && e.unit.Damage > 0 }

// crystalNode is a crystal cell of the map.
type crystalNode struct {
	cell pathfind.Cell
	left int
}

// side is one team's economy and bases.
type side struct {
	crystals int
	hq       *entity
	// pads are the top-left cells of the side's build pads, and padUsed whether a barracks stands on each.
	pads    []pathfind.Cell
	padUsed []bool
}

// world is the skirmish's simulation: the map, both sides, every unit and building, the fog of war and the economy. It steps deterministically on fixed steps and knows nothing of views or input, so tests and the computer player drive it directly.
type world struct {
	data *gameData
	r    *rules
	cell float32
	cols int
	rows int

	grid  *pathfind.BoolGrid
	crowd *rts.Crowd
	fog   *fog.Fog

	ents   []*entity
	byID   map[int]*entity
	byBody map[rts.UnitID]*entity
	nextID int
	nodes  []crystalNode
	sides  [2]side

	steps uint64
	over  bool
	// winner is the side whose rival's headquarters fell, unless drawn reports that both fell in the same step.
	winner team
	drawn  bool

	// onRemove, when set, hears every entity just before it leaves the world, so the player's selection and control groups can forget it.
	onRemove func(*entity)
}

// newWorld builds the skirmish data describes: the map, both headquarters and each side's starting units.
func newWorld(data *gameData) *world {
	m := data.battleMap
	w := &world{
		data:   data,
		r:      data.rules,
		cell:   m.CellSize,
		cols:   m.cols,
		rows:   m.rows,
		grid:   pathfind.NewBoolGrid(m.cols, m.rows),
		fog:    fog.New(m.cols, m.rows, m.CellSize, m.CellSize),
		byID:   map[int]*entity{},
		byBody: map[rts.UnitID]*entity{},
	}
	for row := range m.rows {
		for col := range m.cols {
			t, _ := m.tile(col, row)
			switch t {
			case tileRock:
				w.grid.SetWalkable(col, row, false)
				w.fog.SetOpaque(col, row, true)
			case tileCrystal:
				w.grid.SetWalkable(col, row, false)
				w.nodes = append(w.nodes, crystalNode{cell: pathfind.Cell{Col: col, Row: row}, left: w.r.CrystalsPerNode})
			}
		}
	}
	barracks := w.r.building(barracksName)
	for t, pad := range [2]byte{player: tilePlayerPad, opponent: tileEnemyPad} {
		pads := m.find(pad)
		w.sides[t].pads = pads
		w.sides[t].padUsed = make([]bool, len(pads))
		for _, c := range pads {
			w.block(c, barracks.Size, false)
		}
	}
	w.crowd = rts.NewCrowd(w.grid, rts.CrowdOptions{CellSize: w.cell, Gap: 1})
	hq := w.r.building(hqBuildingName)
	for _, t := range []team{player, opponent} {
		marker := byte(tilePlayerHQ)
		if t == opponent {
			marker = tileEnemyHQ
		}
		s := &w.sides[t]
		s.crystals = w.r.StartCrystals
		s.hq = w.addBuilding(t, hq, m.find(marker)[0], true)
		for _, start := range w.r.Start {
			u := w.r.unit(start.Unit)
			for range start.Count {
				w.spawn(t, u, s.hq)
			}
		}
	}
	w.updateFog()
	return w
}

// block marks the size by size cells from top-left cell c walkable or not.
func (w *world) block(c pathfind.Cell, size int, walkable bool) {
	for row := c.Row; row < c.Row+size; row++ {
		for col := c.Col; col < c.Col+size; col++ {
			w.grid.SetWalkable(col, row, walkable)
		}
	}
}

// addBuilding places a building of type b for t with its top-left cell at c, finished or under construction.
func (w *world) addBuilding(t team, b *buildingRules, c pathfind.Cell, finished bool) *entity {
	w.nextID++
	e := &entity{id: w.nextID, team: t, building: b, maxHP: b.HP, hp: b.HP, cell: c, node: -1}
	if !finished {
		e.buildLeft = secondsToSteps(b.BuildSeconds)
		e.hp = max(1, b.HP/10)
	}
	w.block(c, b.Size, false)
	center := w.buildingCenter(e)
	col, row, _ := w.fog.CellAt(center.Point())
	e.fogID = w.fog.AddSource(fog.Source{Team: fog.Team(t), Col: col, Row: row, Radius: b.Sight})
	w.ents = append(w.ents, e)
	w.byID[e.id] = e
	return e
}

// spawn adds a unit of type u for t at the free cell nearest the door of building from, and sets a worker gathering.
func (w *world) spawn(t team, u *unitRules, from *entity) *entity {
	pos := w.doorOf(from)
	// Spread units that spawn together so the crowd does not have to pull them apart from one point.
	n := w.countUnits(t, nil)
	pos = pos.Add(math2d.V2(float32(n%3-1)*u.Radius, float32(n%2)*u.Radius))
	w.nextID++
	e := &entity{id: w.nextID, team: t, unit: u, maxHP: u.HP, hp: u.HP, node: -1, dest: pos}
	e.body = w.crowd.Add(rts.UnitSpec{Pos: pos, Radius: u.Radius, MaxSpeed: u.Speed, Kind: int(t)*len(w.r.Units) + slices.IndexFunc(w.r.Units, func(r unitRules) bool { return r.Name == u.Name })})
	col, row, _ := w.fog.CellAt(pos.Point())
	e.fogID = w.fog.AddSource(fog.Source{Team: fog.Team(t), Col: col, Row: row, Radius: u.Sight})
	w.ents = append(w.ents, e)
	w.byID[e.id] = e
	w.byBody[e.body] = e
	if u.Gathers {
		if n := w.nearestNode(pos); n >= 0 {
			e.order, e.node = orderGather, n
		}
	}
	return e
}

// doorOf returns the center of the walkable cell nearest the middle of b's bottom edge, where its units come out.
func (w *world) doorOf(b *entity) math2d.Vec2 {
	size := b.building.Size
	start := pathfind.Cell{Col: b.cell.Col + size/2, Row: b.cell.Row + size}
	if c, ok := w.nearestWalkable(start); ok {
		return w.cellCenter(c)
	}
	return w.buildingCenter(b)
}

// nearestWalkable returns the walkable cell nearest c in rings of growing size, scanning each ring row by row.
func (w *world) nearestWalkable(c pathfind.Cell) (pathfind.Cell, bool) {
	for r := 0; r < max(w.cols, w.rows); r++ {
		best, bestD, found := pathfind.Cell{}, 0, false
		for row := c.Row - r; row <= c.Row+r; row++ {
			for col := c.Col - r; col <= c.Col+r; col++ {
				if max(abs(col-c.Col), abs(row-c.Row)) != r || !w.grid.Walkable(col, row) {
					continue
				}
				d := (col-c.Col)*(col-c.Col) + (row-c.Row)*(row-c.Row)
				if !found || d < bestD {
					best, bestD, found = pathfind.Cell{Col: col, Row: row}, d, true
				}
			}
		}
		if found {
			return best, true
		}
	}
	return pathfind.Cell{}, false
}

// step advances the skirmish by one fixed step.
func (w *world) step(dt time.Duration) {
	if w.over {
		return
	}
	w.steps++
	for _, e := range w.ents {
		// An entity killed earlier in this step does not act; removeDead takes it out below.
		if !e.alive() {
			continue
		}
		if e.flash > 0 {
			e.flash--
		}
		if e.shot > 0 {
			e.shot--
		}
		switch {
		case e.building != nil:
			w.stepBuilding(e)
		case e.unit.Gathers:
			w.stepWorker(e)
		case e.fights():
			w.stepFighter(e)
		}
	}
	w.crowd.Step(dt)
	w.removeDead()
	if w.steps%fogEvery == 0 {
		w.updateFog()
	}
	playerLost, opponentLost := !w.sides[player].hq.alive(), !w.sides[opponent].hq.alive()
	switch {
	case playerLost && opponentLost:
		w.over, w.drawn = true, true
	case playerLost:
		w.over, w.winner = true, opponent
	case opponentLost:
		w.over, w.winner = true, player
	}
}

// stepBuilding advances b's construction or its production.
func (w *world) stepBuilding(b *entity) {
	if b.buildLeft > 0 {
		b.buildLeft--
		total := secondsToSteps(b.building.BuildSeconds)
		// Health grows with construction, keeping the damage taken meanwhile.
		grow := (b.maxHP - b.maxHP/10 + total - 1) / total
		b.hp = min(b.maxHP, b.hp+grow)
		return
	}
	if len(b.queue) == 0 {
		return
	}
	if b.trainLeft > 0 {
		b.trainLeft--
	}
	if b.trainLeft == 0 {
		w.spawn(b.team, b.queue[0], b)
		b.queue = b.queue[1:]
		if len(b.queue) > 0 {
			b.trainLeft = secondsToSteps(b.queue[0].TrainSeconds)
		}
	}
}

// stepWorker runs a worker's gathering: walk to its crystal cell, mine, carry the load home, and go back.
func (w *world) stepWorker(e *entity) {
	if e.order != orderGather {
		if e.order == orderMove && w.arrived(e) {
			e.order = orderIdle
		}
		return
	}
	hq := w.sides[e.team].hq
	if e.node < 0 || w.nodes[e.node].left == 0 {
		if e.carry == 0 {
			if e.node = w.nearestNode(w.pos(e)); e.node < 0 {
				e.order = orderIdle
				return
			}
		} else {
			e.returning = true
		}
	}
	if e.returning {
		if w.rectDist(w.pos(e), w.buildingRect(hq)) <= w.reach(e) {
			w.sides[e.team].crystals += e.carry
			e.carry, e.returning = 0, false
			e.repath = 0
			return
		}
		w.walk(e, w.buildingCenter(hq))
		return
	}
	n := &w.nodes[e.node]
	if w.rectDist(w.pos(e), w.cellRect(n.cell)) > w.reach(e) {
		e.mining = 0
		w.walk(e, w.cellCenter(n.cell))
		return
	}
	w.halt(e)
	if e.mining == 0 {
		e.mining = secondsToSteps(w.r.GatherSeconds)
	}
	if e.mining--; e.mining > 0 {
		return
	}
	take := min(w.r.CarryAmount, n.left)
	n.left -= take
	e.carry += take
	e.returning = true
	if n.left == 0 {
		w.grid.SetWalkable(n.cell.Col, n.cell.Row, true)
		w.crowd.Rebuild()
	}
}

// stepFighter runs a combat unit: carry out its order and attack what it may.
func (w *world) stepFighter(e *entity) {
	if e.cooldown > 0 {
		e.cooldown--
	}
	switch e.order {
	case orderMove:
		if w.arrived(e) {
			e.order = orderIdle
		}
		return
	case orderIdle, orderAttackMove:
		if (w.steps+uint64(e.id))%scanEvery == 0 {
			if t := w.nearestEnemy(e); t != nil {
				e.resume = e.order == orderAttackMove
				e.order, e.target, e.repath = orderAttack, t.id, 0
			}
		}
		if e.order == orderAttackMove {
			if w.arrived(e) {
				e.order = orderIdle
			} else {
				w.walk(e, e.dest)
			}
		}
		if e.order != orderAttack {
			return
		}
	}
	t := w.byID[e.target]
	if t == nil || !t.alive() {
		e.target = 0
		if e.resume {
			e.order, e.resume, e.repath = orderAttackMove, false, 0
		} else {
			e.order = orderIdle
			w.halt(e)
		}
		return
	}
	if w.gap(e, t) > e.unit.Range {
		w.chase(e, t)
		return
	}
	w.halt(e)
	if e.cooldown == 0 {
		e.cooldown = secondsToSteps(e.unit.Cooldown)
		t.hp -= e.unit.Damage
		t.flash = 6
		e.shot, e.shotAt = 6, w.pos(t)
	}
}

// walk keeps e moving to p: it orders the crowd once, and again after repathSteps when the unit stopped short of p.
func (w *world) walk(e *entity, p math2d.Vec2) {
	u, _ := w.crowd.Unit(e.body)
	if e.repath > 0 {
		e.repath--
	}
	if u.State == rts.Moving && e.dest == p {
		return
	}
	if e.dest == p && e.repath > 0 {
		return
	}
	e.dest, e.repath = p, repathSteps
	w.crowd.Move([]rts.UnitID{e.body}, rts.MoveOrder{Target: p})
}

// chase moves e toward t, ordering the crowd again every repathSteps as t moves.
func (w *world) chase(e *entity, t *entity) {
	if e.repath > 0 {
		e.repath--
		return
	}
	e.repath = repathSteps
	w.crowd.Move([]rts.UnitID{e.body}, rts.MoveOrder{Target: w.pos(t)})
}

// halt stops e where it stands, once.
func (w *world) halt(e *entity) {
	if u, _ := w.crowd.Unit(e.body); u.State == rts.Moving {
		w.crowd.Stop(e.body)
	}
}

// arrived reports whether the crowd has e at its order's target, or idle.
func (w *world) arrived(e *entity) bool {
	u, _ := w.crowd.Unit(e.body)
	return u.State != rts.Moving
}

// nearestEnemy returns the closest living enemy e sees within its sight, or nil.
func (w *world) nearestEnemy(e *entity) *entity {
	limit := float32(e.unit.Sight) * w.cell
	var best *entity
	var bestD float32
	for _, o := range w.ents {
		if o.team == e.team || !o.alive() || !w.fog.VisibleAt(fog.Team(e.team), w.pos(o).Point()) {
			continue
		}
		d := w.gap(e, o)
		if d <= limit && (best == nil || d < bestD) {
			best, bestD = o, d
		}
	}
	return best
}

// threat returns the enemy unit t sees nearest its headquarters within threatCells of it, or nil.
func (w *world) threat(t team) *entity {
	hq := w.sides[t].hq
	r := w.buildingRect(hq)
	var best *entity
	var bestD float32
	for _, o := range w.ents {
		if o.team == t || o.unit == nil || !o.alive() || !w.fog.VisibleAt(fog.Team(t), w.pos(o).Point()) {
			continue
		}
		if d := w.rectDist(w.pos(o), r); d <= threatCells*w.cell && (best == nil || d < bestD) {
			best, bestD = o, d
		}
	}
	return best
}

// removeDead takes every entity without health out of the world, freeing a destroyed barracks' pad.
func (w *world) removeDead() {
	keep := w.ents[:0]
	for _, e := range w.ents {
		if e.alive() {
			keep = append(keep, e)
			continue
		}
		if w.onRemove != nil {
			w.onRemove(e)
		}
		w.fog.RemoveSource(e.fogID)
		delete(w.byID, e.id)
		if e.unit != nil {
			w.crowd.Remove(e.body)
			delete(w.byBody, e.body)
		} else if e.building.Name == barracksName {
			s := &w.sides[e.team]
			if i := slices.Index(s.pads, e.cell); i >= 0 {
				s.padUsed[i] = false
			}
		}
	}
	clear(w.ents[len(keep):])
	w.ents = keep
}

// updateFog moves every unit's vision to where it stands, recomputes what each side sees, and updates each side's memory of the enemy buildings it sees.
func (w *world) updateFog() {
	for _, e := range w.ents {
		if e.unit != nil {
			w.fog.MoveTo(e.fogID, w.pos(e).Point())
		}
	}
	w.fog.Update()
	for _, e := range w.ents {
		if t := e.team.rival(); e.building != nil && w.inSight(t, e) {
			e.seenBy[t] = sighting{seen: true, hp: e.hp, buildLeft: e.buildLeft}
		}
	}
}

// inSight reports whether t's vision reaches e now: a cell of a building's footprint, or the point a unit stands on.
func (w *world) inSight(t team, e *entity) bool {
	if e.building == nil {
		return w.fog.VisibleAt(fog.Team(t), w.pos(e).Point())
	}
	for row := e.cell.Row; row < e.cell.Row+e.building.Size; row++ {
		for col := e.cell.Col; col < e.cell.Col+e.building.Size; col++ {
			if w.fog.Visible(fog.Team(t), col, row) {
				return true
			}
		}
	}
	return false
}

// visibleTo reports whether t knows of e: its own, in t's sight, or an enemy building t has scouted, which stays shown as t last saw it.
func (w *world) visibleTo(t team, e *entity) bool {
	return e.team == t || w.inSight(t, e) || e.building != nil && e.seenBy[t].seen
}

// shownTo returns the health and construction time left t knows of building b: the live ones while t owns or sees it, and those t last saw otherwise.
func (w *world) shownTo(t team, b *entity) (hp, buildLeft int) {
	if b.team == t || w.inSight(t, b) {
		return b.hp, b.buildLeft
	}
	s := b.seenBy[t]
	return s.hp, s.buildLeft
}

// pos returns where e is: a unit's body or a building's center.
func (w *world) pos(e *entity) math2d.Vec2 {
	if e.unit == nil {
		return w.buildingCenter(e)
	}
	u, _ := w.crowd.Unit(e.body)
	return u.Pos
}

// gap returns the distance between the edges of e's body and o's body or footprint.
func (w *world) gap(e, o *entity) float32 {
	p := w.pos(e)
	if o.building != nil {
		return w.rectDist(p, w.buildingRect(o)) - e.unit.Radius
	}
	return p.Dist(w.pos(o)) - e.unit.Radius - o.unit.Radius
}

// reach is how close to the edge of a crystal cell or a headquarters a worker's center must stand to work there.
func (w *world) reach(e *entity) float32 { return e.unit.Radius + w.cell*0.75 }

// rectDist returns the distance from p to the nearest point of r, 0 inside it.
func (w *world) rectDist(p math2d.Vec2, r math2d.Rect) float32 {
	return p.Dist(r.ClampPoint(p))
}

func (w *world) cellCenter(c pathfind.Cell) math2d.Vec2 {
	return math2d.V2((float32(c.Col)+0.5)*w.cell, (float32(c.Row)+0.5)*w.cell)
}

func (w *world) cellRect(c pathfind.Cell) math2d.Rect {
	return math2d.Rect{X: float32(c.Col) * w.cell, Y: float32(c.Row) * w.cell, W: w.cell, H: w.cell}
}

func (w *world) buildingRect(b *entity) math2d.Rect {
	s := float32(b.building.Size) * w.cell
	return math2d.Rect{X: float32(b.cell.Col) * w.cell, Y: float32(b.cell.Row) * w.cell, W: s, H: s}
}

func (w *world) buildingCenter(b *entity) math2d.Vec2 { return w.buildingRect(b).Center() }

// size returns the world's width and height in points.
func (w *world) size() math2d.Vec2 {
	return math2d.V2(float32(w.cols)*w.cell, float32(w.rows)*w.cell)
}

// nearestNode returns the index of the crystal cell with crystals left nearest p, or -1.
func (w *world) nearestNode(p math2d.Vec2) int {
	best, bestD := -1, float32(0)
	for i, n := range w.nodes {
		if n.left == 0 {
			continue
		}
		if d := p.DistSq(w.cellCenter(n.cell)); best < 0 || d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// nodeAt returns the index of the crystal cell with crystals left holding p, or -1.
func (w *world) nodeAt(p math2d.Vec2) int {
	for i, n := range w.nodes {
		if n.left > 0 && w.cellRect(n.cell).Contains(p) {
			return i
		}
	}
	return -1
}

// countUnits returns how many living units t has, of type u or of every type when u is nil.
func (w *world) countUnits(t team, u *unitRules) int {
	n := 0
	for _, e := range w.ents {
		if e.team == t && e.unit != nil && e.alive() && (u == nil || e.unit == u) {
			n++
		}
	}
	return n
}

// supply returns how many units t has and has queued.
func (w *world) supply(t team) int {
	n := w.countUnits(t, nil)
	for _, e := range w.ents {
		if e.team == t && e.building != nil {
			n += len(e.queue)
		}
	}
	return n
}

// buildings returns t's buildings of type b, in the order they were placed.
func (w *world) buildings(t team, b *buildingRules) []*entity {
	var out []*entity
	for _, e := range w.ents {
		if e.team == t && e.building == b && e.alive() {
			out = append(out, e)
		}
	}
	return out
}

// color returns the color a side's units and buildings are drawn in.
func (t team) color() core.Color {
	if t == player {
		return core.RGB(0x38bdf8)
	}
	return core.RGB(0xf87171)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// finiteVec reports whether both coordinates of v are finite numbers.
func finiteVec(v math2d.Vec2) bool {
	return !math.IsNaN(float64(v.X)) && !math.IsNaN(float64(v.Y)) && !math.IsInf(float64(v.X), 0) && !math.IsInf(float64(v.Y), 0)
}

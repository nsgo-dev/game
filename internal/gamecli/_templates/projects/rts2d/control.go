package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/rts"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
)

// The actions game.yaml binds to keys and gamepad buttons; the mouse and touch drive the battlefield view directly.
const (
	actCursorX       = "cursor_x"
	actCursorY       = "cursor_y"
	actPick          = "pick"
	actCommand       = "command"
	actWorker        = "train_worker"
	actSoldier       = "train_soldier"
	actRanger        = "train_ranger"
	actBarracks      = "build_barracks"
	actSelectArmy    = "select_army"
	actSelectWorkers = "select_workers"
	actDeselect      = "deselect"
	actPause         = "pause"
	actModAssign     = "mod_assign"
	actModAdd        = "mod_add"
	// actGroup is the prefix of the ten control group actions, group_0 to group_9.
	actGroup = "group_"
)

const (
	// cursorSpeed is how fast the gamepad and keyboard cursor crosses the battlefield, in world points a second at full deflection.
	cursorSpeed = 220
	// touchDragSlop is how far, in world points, a finger moves before a touch becomes a box selection.
	touchDragSlop = 6
	// tapSlop is how far from a unit's body, in world points, a tap or click still picks it.
	tapSlop = 5
	// mouseAfterTouch is how many steps after a touch the mouse events a browser makes up from it are ignored.
	mouseAfterTouch = 30
	// focusSteps is how long a control group recalled twice in a row is highlighted.
	focusSteps = 45
)

// skirmish is a running game: the world and the computer player, and the player's selection, control groups and input. Root builds one and drives it from a game.Loop; tests drive it through Root or directly.
type skirmish struct {
	data     *gameData
	w        *world
	cpu      *computer
	actions  *input.Actions
	viewport screen.Viewport

	sel    rts.Selection
	box    rts.BoxSelector
	groups rts.ControlGroups

	// ticks counts the loop's steps, paused ones included, for control group double taps and the touch filter.
	ticks  uint64
	paused bool

	// viewSize is the battlefield view's size, which maps pointer positions to the world.
	viewSize core.Size
	// cursor is the gamepad and keyboard pointer in world points, shown once either moves it.
	cursor      math2d.Vec2
	cursorShown bool
	cursorPick  bool
	mouseLeft   bool
	// The finger on the battlefield: where it went down, whether it became a box selection, and the step the last touch ended.
	touchID    int
	touchStart math2d.Vec2
	touchDrag  bool
	touching   bool
	touchEnded uint64
	focus      int
	focusGroup int

	// changed, when set, hears that the HUD's numbers may have changed.
	changed func()
}

// newSkirmish starts a skirmish from data with the player's input bound from game.yaml.
func newSkirmish(data *gameData, actions *input.Actions, viewport screen.Viewport) *skirmish {
	g := &skirmish{data: data, actions: actions, viewport: viewport}
	g.restart()
	return g
}

// restart throws the current skirmish away and starts a new one.
func (g *skirmish) restart() {
	g.w = newWorld(g.data)
	cpu, err := newComputer(g.data, opponent)
	if err != nil {
		// The tests load the same data files, so this is a programming error.
		panic(err)
	}
	g.cpu = cpu
	g.sel.Clear()
	g.groups = rts.ControlGroups{}
	g.box = rts.BoxSelector{
		Picker:    g.w.crowd,
		Selection: &g.sel,
		Filter:    func(id rts.UnitID) bool { e := g.w.byBody[id]; return e != nil && e.team == player },
		ClickSlop: tapSlop,
	}
	g.w.onRemove = func(e *entity) {
		if e.unit != nil {
			g.sel.Remove(e.body)
			g.groups.Forget(e.body)
		}
	}
	g.cursor = g.w.buildingCenter(g.w.sides[player].hq)
	g.cursorPick, g.mouseLeft, g.touching, g.touchDrag = false, false, false, false
	g.paused = false
	g.focus = 0
	if g.actions != nil {
		g.actions.Reset()
	}
	g.notify()
}

// update runs one fixed step: the player's keys and gamepad, the computer player and the world.
func (g *skirmish) update(dt time.Duration) {
	g.ticks++
	if g.actions != nil {
		g.actions.Step()
		g.readActions(dt)
	}
	if g.focus > 0 {
		g.focus--
	}
	if g.paused || g.w.over {
		return
	}
	g.cpu.step(g.w)
	g.w.step(dt)
	if g.w.steps%10 == 0 || g.w.over {
		g.notify()
	}
}

// readActions turns this step's keys and gamepad buttons into orders.
func (g *skirmish) readActions(dt time.Duration) {
	a := g.actions
	if a.JustPressed(actPause) {
		if g.w.over {
			g.restart()
			return
		}
		g.togglePause()
	}
	if g.w.over {
		if a.JustPressed(actCommand) {
			g.restart()
		}
		return
	}
	if g.paused {
		return
	}
	if v := math2d.V2(float32(a.Axis(actCursorX)), float32(a.Axis(actCursorY))); v != (math2d.Vec2{}) {
		g.cursor = v.Scale(cursorSpeed*float32(dt.Seconds())).Add(g.cursor).Clamp(math2d.Vec2{}, g.w.size())
		g.cursorShown = true
		if g.cursorPick {
			g.box.Drag(g.cursor)
		}
	}
	mods := core.Modifiers{Shift: a.Pressed(actModAdd), Control: a.Pressed(actModAssign)}
	if a.JustPressed(actPick) {
		g.cursorShown, g.cursorPick = true, true
		g.box.Press(g.cursor, mods, 1)
	}
	if a.JustReleased(actPick) && g.cursorPick {
		g.cursorPick = false
		g.box.Release(g.cursor)
		g.notify()
	}
	if a.JustPressed(actCommand) {
		g.cursorShown = true
		g.w.command(player, g.sel.IDs(), g.cursor)
	}
	hotkeys := []struct {
		action string
		run    func()
	}{
		{actWorker, g.trainWorker},
		{actSoldier, func() { g.train(soldierName) }},
		{actRanger, func() { g.train(rangerName) }},
		{actBarracks, g.buildBarracks},
		{actSelectArmy, g.selectArmy},
		{actSelectWorkers, g.selectWorkers},
		{actDeselect, g.deselect},
	}
	for _, h := range hotkeys {
		if a.JustPressed(h.action) {
			h.run()
		}
	}
	for n := range rts.GroupCount {
		if !a.JustPressed(fmt.Sprintf("%s%d", actGroup, n)) {
			continue
		}
		switch {
		case mods.Control:
			g.groups.Assign(n, g.sel.IDs())
		case mods.Shift:
			g.groups.Append(n, g.sel.IDs())
			g.groups.Recall(n, &g.sel, g.ticks)
		default:
			if g.groups.Recall(n, &g.sel, g.ticks) == rts.GroupFocused {
				g.focus, g.focusGroup = focusSteps, n
			}
		}
		g.notify()
	}
}

// togglePause pauses or resumes the skirmish, dropping any pick in progress.
func (g *skirmish) togglePause() {
	if g.w.over {
		return
	}
	g.paused = !g.paused
	g.box.Cancel()
	g.cursorPick, g.mouseLeft = false, false
	if g.actions != nil {
		g.actions.Reset()
	}
	g.notify()
}

func (g *skirmish) trainWorker() { g.w.train(player, g.w.r.worker()); g.notify() }

func (g *skirmish) train(unit string) { g.w.train(player, g.w.r.unit(unit)); g.notify() }

func (g *skirmish) buildBarracks() { g.w.buildBarracks(player); g.notify() }

// selectArmy selects every combat unit the player has.
func (g *skirmish) selectArmy() {
	g.selectWhere(func(e *entity) bool { return e.fights() })
}

// selectWorkers selects every worker the player has.
func (g *skirmish) selectWorkers() {
	g.selectWhere(func(e *entity) bool { return e.unit.Gathers })
}

func (g *skirmish) selectWhere(keep func(*entity) bool) {
	var ids []rts.UnitID
	for _, e := range g.w.ents {
		if e.team == player && e.unit != nil && keep(e) {
			ids = append(ids, e.body)
		}
	}
	g.sel.Set(ids)
	g.notify()
}

func (g *skirmish) deselect() {
	g.box.Cancel()
	g.sel.Clear()
	g.notify()
}

// toWorld converts a point in the battlefield view to world points, false on a letterbox bar.
func (g *skirmish) toWorld(p core.Point) (math2d.Vec2, bool) {
	gp, ok := g.viewport.ViewToGame(g.viewSize, p)
	return math2d.FromPoint(gp), ok
}

// toView converts world point p to the battlefield view's coordinates.
func (g *skirmish) toView(p math2d.Vec2) core.Point {
	return g.viewport.GameToView(g.viewSize, p.Point())
}

// playing reports whether pointer input may change the game now.
func (g *skirmish) playing() bool { return !g.paused && !g.w.over }

// mouseDown starts a pick with the left button and gives a command with the right one.
func (g *skirmish) mouseDown(ev core.MouseButtonEvent) {
	if !g.playing() || g.touching || (g.touchEnded > 0 && g.ticks-g.touchEnded < mouseAfterTouch) {
		return
	}
	p, ok := g.toWorld(ev.Position)
	if !ok {
		return
	}
	switch ev.Button {
	case core.MouseButtonLeft:
		g.mouseLeft = true
		g.box.Press(p, ev.Modifiers, ev.Clicks)
	case core.MouseButtonRight:
		g.w.command(player, g.sel.IDs(), p)
	}
}

// mouseMove drags a box selection while the left button is down.
func (g *skirmish) mouseMove(ev core.MouseEvent) {
	if !g.mouseLeft {
		return
	}
	if p, ok := g.toWorld(ev.Position); ok {
		g.box.Drag(p)
	}
}

// mouseUp ends the pick the left button started; a button the window lost the focus with cancels it.
func (g *skirmish) mouseUp(ev core.MouseButtonEvent) {
	if ev.Button != core.MouseButtonLeft || !g.mouseLeft {
		return
	}
	g.mouseLeft = false
	p, ok := g.toWorld(ev.Position)
	if !ok || ev.Clicks == 0 {
		g.box.Cancel()
		return
	}
	g.box.Release(p)
	g.notify()
}

// touch plays with one finger: a tap on one of the player's units selects it, a tap anywhere else orders the selection there, and a drag selects every unit in its box.
func (g *skirmish) touch(ev core.TouchEvent) {
	var t core.Touch
	found := false
	for _, c := range ev.Touches {
		if c.Changed && (!g.touching || c.ID == g.touchID) {
			t, found = c, true
			break
		}
	}
	if !found {
		return
	}
	p, ok := g.toWorld(t.Position)
	switch ev.Phase {
	case core.TouchStart:
		if g.touching || !ok || !g.playing() {
			return
		}
		g.touching, g.touchID, g.touchStart, g.touchDrag = true, t.ID, p, false
	case core.TouchMove:
		if !g.touching || !ok {
			return
		}
		if !g.touchDrag && p.Dist(g.touchStart) > touchDragSlop {
			g.touchDrag = true
			g.box.Press(g.touchStart, core.Modifiers{}, 1)
		}
		if g.touchDrag {
			g.box.Drag(p)
		}
	case core.TouchEnd, core.TouchCancel:
		if !g.touching {
			return
		}
		g.touching, g.touchEnded = false, max(g.ticks, 1)
		switch {
		case ev.Phase == core.TouchCancel || !g.playing():
			g.box.Cancel()
		case g.touchDrag:
			if !ok {
				p = g.touchStart
			}
			g.box.Release(p)
		case ok:
			g.tap(p)
		}
		g.touchDrag = false
		g.notify()
	}
}

// tap selects the player's unit at p, or orders the selection to p.
func (g *skirmish) tap(p math2d.Vec2) {
	if id, ok := g.w.crowd.UnitAt(p, tapSlop); ok {
		if e := g.w.byBody[id]; e != nil && e.team == player {
			g.sel.Set([]rts.UnitID{id})
			return
		}
	}
	g.w.command(player, g.sel.IDs(), p)
}

func (g *skirmish) notify() {
	if g.changed != nil {
		g.changed()
	}
}

// hudState is what the native HUD shows, read from the skirmish after a change.
type hudState struct {
	crystals  int
	supply    int
	maxSupply int
	selected  int
	queued    map[string]int
	canTrain  map[string]bool
	canBuild  bool
	paused    bool
	over      bool
	won       bool
	drawn     bool
}

// hud reads the HUD's numbers.
func (g *skirmish) hud() hudState {
	w := g.w
	s := hudState{
		crystals:  w.sides[player].crystals,
		supply:    w.supply(player),
		maxSupply: w.r.MaxUnits,
		selected:  g.sel.Len(),
		queued:    map[string]int{},
		canTrain:  map[string]bool{},
		paused:    g.paused,
		over:      w.over,
		won:       w.over && !w.drawn && w.winner == player,
		drawn:     w.drawn,
	}
	for _, e := range w.ents {
		if e.team == player && e.building != nil {
			for _, u := range e.queue {
				s.queued[u.Name]++
			}
		}
	}
	for _, u := range w.r.Units {
		ready := false
		for _, b := range w.buildings(player, w.r.building(u.TrainedAt)) {
			ready = ready || b.built() && len(b.queue) < w.r.QueueLimit
		}
		s.canTrain[u.Name] = ready && s.crystals >= u.Cost && s.supply < s.maxSupply && !w.over
	}
	s.canBuild = w.freePad(player) && s.crystals >= w.r.building(barracksName).Cost && !w.over
	return s
}

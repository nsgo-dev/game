package main

import (
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/tween"
	"github.com/nsgo-dev/game/packages/game/undo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// The actions game.yaml binds to keys and controller buttons; swipes on the board press the four directions too.
const (
	actionLeft    = "left"
	actionRight   = "right"
	actionUp      = "up"
	actionDown    = "down"
	actionUndo    = "undo"
	actionRedo    = "redo"
	actionRestart = "restart"
	actionConfirm = "confirm"
	actionBack    = "back"
	// axisMoveX and axisMoveY are the left stick, which game.yaml gives a dead zone of half its travel so a push reads as one direction.
	axisMoveX = "moveX"
	axisMoveY = "moveY"
)

const (
	// stepRate is the game loop's fixed rate in steps per second.
	stepRate = 60
	// slideCellTime is how long the spark takes to cross one cell.
	slideCellTime = 45 * time.Millisecond
	// fallTime is how long the spark takes to shrink into a void.
	fallTime = 220 * time.Millisecond
	// bumpTime is how long the spark shakes when a wall blocks a move.
	bumpTime = 160 * time.Millisecond
	// glowTime is how long a lamp takes to light.
	glowTime = 180 * time.Millisecond
	// levelColumns is how many levels a row of the level select holds, which up and down move between.
	levelColumns = 3
)

// screen is which of the game's two screens shows.
type screen uint8

const (
	screenLevels screen = iota
	screenPlay
)

// hudState is what the native controls show about the level being played.
type hudState struct {
	Moves, Limit  int
	Lit, Lamps    int
	Status        Status
	Stars         int
	CanUndo       bool
	CanRedo       bool
	ResultShowing bool
}

// sparkGame is the whole game: the levels and the player's progress, the level being played with its undo history, and what the board draws between moves.
type sparkGame struct {
	levels   []Level
	actions  *input.Actions
	gestures *input.GestureRecognizer
	tweens   tween.Group
	store    *progressStore
	progress Progress

	level   int
	history *undo.History[*Board, *slideMove]

	// pending is a direction a tap or click on the board asked for, made on the next step.
	pending Dir
	// stickDir is the direction the left stick was last pushed in, so holding it makes one move.
	stickDir Dir
	// viewSize is the board view's size, which taps are mapped through.
	viewSize core.Size

	// The board as drawn: the spark's position in cells, its scale while it falls, a wall bump's offset and each lamp's glow.
	sparkX, sparkY float32
	sparkScale     float32
	bumpX, bumpY   float32
	glow           []float32
	animating      bool

	screen   *core.Signal[screen]
	cursor   *core.Signal[int]
	hud      *core.Signal[hudState]
	saved    *core.Signal[int]
	frame    *core.Signal[uint64]
	saveFail *core.Signal[bool]
}

// Root is the game's root NSGo component: the level select, or a level on a full-screen Canvas driven by a fixed-step game.Loop with native controls above it.
func Root(cx *core.Scope) core.Node {
	g := newSparkGame(cx)
	return g.view()
}

func newSparkGame(cx *core.Scope) *sparkGame {
	desktop.SetTitle(cx, Project.Title())
	g := &sparkGame{
		levels:     Levels.Levels,
		actions:    input.NewActions(),
		store:      newProgressStore(cx.Storage()),
		sparkScale: 1,
		screen:     core.NewSignal(cx, screenLevels),
		cursor:     core.NewSignal(cx, 0),
		hud:        core.NewSignal(cx, hudState{}),
		saved:      core.NewSignal(cx, 0),
		frame:      core.NewSignal(cx, uint64(0)),
		saveFail:   core.NewSignal(cx, false),
	}
	var err error
	g.progress, err = g.store.Load()
	g.saveFail.Set(err != nil)
	g.cursor.Set(g.firstUnwon())

	Project.BindInput(g.actions)
	g.gestures = input.NewGestureRecognizer(g.actions)
	g.gestures.BindGesture(actionLeft, input.GestureSwipeLeft)
	g.gestures.BindGesture(actionRight, input.GestureSwipeRight)
	g.gestures.BindGesture(actionUp, input.GestureSwipeUp)
	g.gestures.BindGesture(actionDown, input.GestureSwipeDown)
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	loop := &game.Loop{
		Rate:   stepRate,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	return g
}

// firstUnwon returns the index of the first level not yet won, or the last level when all are.
func (g *sparkGame) firstUnwon() int {
	for i, l := range g.levels {
		if g.progress.Stars[l.ID] == 0 {
			return i
		}
	}
	return len(g.levels) - 1
}

// board returns the board being played.
func (g *sparkGame) board() *Board { return g.history.State() }

// update is one fixed step: read the input, act on it and advance the animations.
func (g *sparkGame) update(dt time.Duration) {
	g.gestures.Update(dt)
	g.actions.Step()
	switch g.screen.Get() {
	case screenLevels:
		g.levelsInput()
	case screenPlay:
		g.playInput()
	}
	g.tweens.Update(dt)
}

// direction returns the direction the step's input asks for: a direction action, else a tap on the board, else a fresh push of the left stick.
func (g *sparkGame) direction() Dir {
	a := g.actions
	stick := DirNone
	if x, y := a.Axis(axisMoveX), a.Axis(axisMoveY); x != 0 || y != 0 {
		switch {
		case abs(x) >= abs(y) && x < 0:
			stick = DirLeft
		case abs(x) >= abs(y):
			stick = DirRight
		case y < 0:
			stick = DirUp
		default:
			stick = DirDown
		}
	}
	fresh := stick != g.stickDir
	g.stickDir = stick
	pending := g.pending
	g.pending = DirNone
	switch {
	case a.JustPressed(actionLeft):
		return DirLeft
	case a.JustPressed(actionRight):
		return DirRight
	case a.JustPressed(actionUp):
		return DirUp
	case a.JustPressed(actionDown):
		return DirDown
	case pending != DirNone:
		return pending
	case fresh:
		return stick
	}
	return DirNone
}

// levelsInput moves the level select's highlight and starts the highlighted level.
func (g *sparkGame) levelsInput() {
	cur := g.cursor.Get()
	switch g.direction() {
	case DirLeft:
		cur--
	case DirRight:
		cur++
	case DirUp:
		cur -= levelColumns
	case DirDown:
		cur += levelColumns
	}
	g.cursor.Set(min(max(cur, 0), len(g.levels)-1))
	if g.actions.JustPressed(actionConfirm) {
		g.startLevel(g.cursor.Get())
	}
}

// playInput acts on the step's input while a level is played.
func (g *sparkGame) playInput() {
	a := g.actions
	d := g.direction()
	switch {
	case a.JustPressed(actionBack):
		g.showLevels()
	case a.JustPressed(actionRestart):
		g.restart()
	case a.JustPressed(actionUndo):
		g.undo()
	case a.JustPressed(actionRedo):
		g.redo()
	case g.hud.Get().ResultShowing:
		if a.JustPressed(actionConfirm) {
			if g.board().Status() == Won {
				g.next()
			} else {
				g.restart()
			}
		}
	case d != DirNone:
		g.slide(d)
	}
}

// startLevel plays the level at index i from its start, if it is unlocked.
func (g *sparkGame) startLevel(i int) {
	if i < 0 || i >= len(g.levels) || !g.progress.Unlocked(g.levels, i) {
		return
	}
	g.level = i
	g.cursor.Set(i)
	g.history = undo.New[*Board, *slideMove](NewBoard(&g.levels[i]))
	g.gestures.Reset()
	g.pending = DirNone
	g.settle()
	g.screen.Set(screenPlay)
}

// restart plays the current level again from its start.
func (g *sparkGame) restart() { g.startLevel(g.level) }

// next plays the level after the current one, or shows the level select after the last.
func (g *sparkGame) next() {
	if g.level+1 < len(g.levels) {
		g.startLevel(g.level + 1)
		return
	}
	g.showLevels()
}

// showLevels shows the level select with the current level highlighted.
func (g *sparkGame) showLevels() {
	g.tweens.Clear()
	g.animating = false
	g.cursor.Set(g.level)
	g.screen.Set(screenLevels)
}

// slide moves the spark in d, if it can move, and animates the move; a blocked move bumps the spark and costs nothing.
func (g *sparkGame) slide(d Dir) {
	b := g.board()
	path := b.Path(d)
	if len(path) == 0 {
		if b.Status() == Playing {
			g.bump(d)
		}
		return
	}
	g.settle()
	m := &slideMove{Dir: d}
	g.history.Do(m)
	if b.Status() == Won && g.progress.Record(b.Level.ID, b.Stars()) {
		g.saveFail.Set(g.store.Save(g.progress) != nil)
		g.saved.Update(func(n int) int { return n + 1 })
	}
	g.animate(path, m)
	g.syncHUD()
}

// undo takes the last move back; a won level keeps its moves.
func (g *sparkGame) undo() {
	if g.board().Status() == Won || !g.history.Undo() {
		return
	}
	g.settle()
}

// redo makes the last move undone again.
func (g *sparkGame) redo() {
	if g.board().Status() != Playing || !g.history.Redo() {
		return
	}
	g.settle()
}

// animate slides the drawn spark along path, lights the lamps m lit as it passes them, and shrinks it into a void it fell into; the result shows once it ends.
func (g *sparkGame) animate(path []Cell, m *slideMove) {
	g.animating = true
	b := g.board()
	to := path[len(path)-1]
	slide := slideCellTime * time.Duration(len(path))
	x := tween.Float32(g.sparkX, float32(to.X), slide, core.EaseInOut, func(v float32) { g.sparkX = v })
	y := tween.Float32(g.sparkY, float32(to.Y), slide, core.EaseInOut, func(v float32) { g.sparkY = v })
	g.tweens.Add(x, y)
	for _, i := range m.lit {
		c := Cell{i % b.Width, i / b.Width}
		at := slideCellTime * time.Duration(slices.Index(path, c)+1)
		g.tweens.Add(tween.Float32(0, 1, glowTime, core.EaseOut, func(v float32) { g.glow[i] = v }).Delay(at))
	}
	x.OnComplete(func() {
		if !m.fell {
			g.finishAnimation()
			return
		}
		g.tweens.Add(tween.Float32(1, 0, fallTime, core.EaseIn, func(v float32) { g.sparkScale = v }).OnComplete(g.finishAnimation))
	})
}

// bump shakes the spark toward d, against the wall that blocks it.
func (g *sparkGame) bump(d Dir) {
	dx, dy := float32(0), float32(0)
	switch d {
	case DirLeft:
		dx = -1
	case DirRight:
		dx = 1
	case DirUp:
		dy = -1
	case DirDown:
		dy = 1
	}
	g.tweens.Add(tween.Float32(0.18, 0, bumpTime, core.EaseOut, func(v float32) { g.bumpX, g.bumpY = dx*v, dy*v }))
}

// finishAnimation ends the move's animation and shows its result.
func (g *sparkGame) finishAnimation() {
	g.animating = false
	g.syncHUD()
}

// settle stops every animation and draws the board as it is.
func (g *sparkGame) settle() {
	g.tweens.Clear()
	b := g.board()
	g.sparkX, g.sparkY = float32(b.Spark.X), float32(b.Spark.Y)
	g.sparkScale = 1
	if b.Fallen {
		g.sparkScale = 0
	}
	g.bumpX, g.bumpY = 0, 0
	g.glow = make([]float32, b.Width*b.Height)
	for y := range b.Height {
		for x := range b.Width {
			if b.Lit(Cell{x, y}) {
				g.glow[y*b.Width+x] = 1
			}
		}
	}
	g.animating = false
	g.syncHUD()
}

// syncHUD updates what the native controls show from the board.
func (g *sparkGame) syncHUD() {
	b := g.board()
	st := b.Status()
	g.hud.Set(hudState{
		Moves: b.Moves, Limit: b.Level.Moves,
		Lit: b.LitCount, Lamps: b.Lamps(),
		Status:        st,
		Stars:         b.Stars(),
		CanUndo:       st != Won && g.history.CanUndo(),
		CanRedo:       st == Playing && g.history.CanRedo(),
		ResultShowing: st != Playing && !g.animating,
	})
}

// tap asks for a move toward the point p of the board view: along whichever axis p is farther from the spark.
func (g *sparkGame) tap(p core.Point) {
	if g.screen.Get() != screenPlay || g.history == nil {
		return
	}
	b := g.board()
	cx, cy, ok := layoutFor(g.viewSize, b).cellAt(p)
	if !ok {
		return
	}
	dx, dy := cx-(float32(b.Spark.X)+0.5), cy-(float32(b.Spark.Y)+0.5)
	switch {
	case abs(dx) < 0.5 && abs(dy) < 0.5:
		return
	case abs(dx) >= abs(dy) && dx < 0:
		g.pending = DirLeft
	case abs(dx) >= abs(dy):
		g.pending = DirRight
	case dy < 0:
		g.pending = DirUp
	default:
		g.pending = DirDown
	}
}

func abs[T float32 | float64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}

package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// Input actions and axes, which game.yaml binds to keys, gamepad buttons and sticks.
const (
	actionBuild     = "build"
	actionUpgrade   = "upgrade"
	actionSell      = "sell"
	actionNextTower = "nextTower"
	actionPrevTower = "prevTower"
	actionWave      = "wave"
	actionSpeed     = "speed"
	actionPause     = "pause"
	actionRestart   = "restart"
	axisCursorX     = "cursorX"
	axisCursorY     = "cursorY"
)

// towerActions select the first towers of data/towers.yaml directly, as the number keys do.
var towerActions = []string{"tower1", "tower2", "tower3"}

const (
	// maxSpeed is the fastest the speed control runs the world, in steps per update.
	maxSpeed = 3
	// cursorDelay is how many steps a held direction waits before the cursor repeats, and cursorRepeat how many steps it then takes per tile.
	cursorDelay  = 18
	cursorRepeat = 6
	// stickThreshold is how far a cursor axis must be pushed to move the cursor.
	stickThreshold = 0.5
	// messageSteps is how long a message stays in the HUD.
	messageSteps = 2 * StepRate
)

// Game connects a World to NSGo: it runs the world on a fixed-step game.Loop, reads the keyboard, gamepads and touches through input.Actions, and keeps the HUD in step with the world.
type Game struct {
	// World is the game being played.
	World *World
	// Actions are the input actions game.yaml binds.
	Actions *input.Actions
	// Viewport maps the map, in game points, onto the canvas.
	Viewport screen.Viewport
	// Loop runs the game.
	Loop *game.Loop

	// Cursor is the cell the player builds on and picks towers with.
	Cursor ai.Cell
	// Kind is the index in data/towers.yaml of the tower kind the player builds.
	Kind int
	// Speed is how many world steps run each update, from 1 to maxSpeed.
	Speed int
	// Paused stops the world while the HUD stays live.
	Paused bool

	atlas      *sprite.Atlas
	message    string
	messageAge int
	heldX      int
	heldY      int
	held       int
	canvasSize core.Size
	frame      *core.Signal[uint64]
	hud        *core.Signal[HUD]
}

// HUD is what the native HUD shows: a copy of the world's and the controls' state, compared each frame so the HUD only changes when it does.
type HUD struct {
	// Level is the level's name.
	Level string
	// Money, Lives and Wave are the world's, and Waves how many the level has.
	Money, Lives, Wave, Waves int
	// Kind and Speed are the chosen tower kind and speed, and Paused whether the game is paused.
	Kind, Speed int
	Paused      bool
	// Outcome is how the game stands.
	Outcome Outcome
	// CanCallWave is whether the next wave may be called.
	CanCallWave bool
	// HasTower is whether a tower stands on the cursor; Tower, TowerLevel, UpgradeCost (0 when fully upgraded) and SellValue describe it.
	HasTower    bool
	Tower       string
	TowerLevel  int
	UpgradeCost int
	SellValue   int
	// Message is the latest refusal or news, empty when there is none.
	Message string
}

// Root is the game's root NSGo component.
func Root(cx *core.Scope) core.Node {
	_, node := NewGameView(cx)
	return node
}

// NewGameView returns a new game and the view that plays it, its loop running in cx.
func NewGameView(cx *core.Scope) (*Game, core.Node) {
	desktop.SetTitle(cx, Project.Title())
	g := NewGame(MustLoadData())
	g.frame = core.NewSignal(cx, uint64(0))
	g.hud = core.NewSignal(cx, g.HUD())

	g.Actions.WatchKeys(cx)
	g.Actions.WatchGamepads(cx)
	// Let go of every held input when the app goes to the background, so nothing stays pressed.
	cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			g.Actions.Reset()
		}
	})

	g.Loop = &game.Loop{
		Rate:   StepRate,
		Update: func(time.Duration) { g.Update() },
		Draw: func(float64) {
			g.frame.Set(g.frame.GetUntracked() + 1)
			g.syncHUD()
		},
	}
	g.Loop.Run(cx)
	return g, g.view()
}

// NewGame returns a game of d's level with the cursor on the spawn and the first tower kind chosen.
func NewGame(d *Data) *Game {
	atlas := makeAtlas(d)
	w := NewWorld(d, atlas)
	vp, ok := Project.Viewport()
	if !ok {
		mw, mh := w.Size()
		vp = screen.Viewport{Width: mw, Height: mh}
	}
	g := &Game{World: w, Actions: input.NewActions(), Viewport: vp, Speed: 1, atlas: atlas}
	Project.BindInput(g.Actions)
	g.Restart()
	return g
}

// Restart starts the game over, keeping the chosen tower kind and speed.
func (g *Game) Restart() {
	g.World.Reset()
	g.Paused = false
	g.Cursor, _ = g.World.CellAt(g.World.Route()[0].X, g.World.Route()[0].Y)
	g.Actions.Reset()
	g.say("")
}

// Update reads the input and runs the world for one update of the loop: Speed steps unless the game is paused or over.
func (g *Game) Update() {
	g.Actions.Step()
	g.handleInput()
	if !g.Paused {
		for range g.Speed {
			g.World.Step()
		}
	}
	if g.messageAge > 0 {
		g.messageAge--
		if g.messageAge == 0 {
			g.message = ""
		}
	}
}

// handleInput acts on the actions pressed this step.
func (g *Game) handleInput() {
	a := g.Actions
	if a.JustPressed(actionRestart) {
		g.Restart()
		return
	}
	if g.World.Outcome != Playing {
		// On the end screen the build and wave buttons play again.
		if a.JustPressed(actionBuild) || a.JustPressed(actionWave) {
			g.Restart()
		}
		return
	}
	if a.JustPressed(actionPause) {
		g.TogglePause()
	}
	if g.Paused {
		return
	}
	g.moveCursorFromInput()
	for i, name := range towerActions {
		if a.JustPressed(name) {
			g.SelectKind(i)
		}
	}
	switch {
	case a.JustPressed(actionNextTower):
		g.SelectKind(g.Kind + 1)
	case a.JustPressed(actionPrevTower):
		g.SelectKind(g.Kind - 1)
	}
	if a.JustPressed(actionBuild) {
		g.Build()
	}
	if a.JustPressed(actionUpgrade) {
		g.Upgrade()
	}
	if a.JustPressed(actionSell) {
		g.Sell()
	}
	if a.JustPressed(actionWave) {
		g.CallWave()
	}
	if a.JustPressed(actionSpeed) {
		g.CycleSpeed()
	}
}

// moveCursorFromInput moves the cursor a tile in the direction the cursor axes are held, at once and then repeating while the direction is held.
func (g *Game) moveCursorFromInput() {
	dx, dy := axisStep(g.Actions.Axis(axisCursorX)), axisStep(g.Actions.Axis(axisCursorY))
	if dx == 0 && dy == 0 {
		g.heldX, g.heldY, g.held = 0, 0, 0
		return
	}
	if dx != g.heldX || dy != g.heldY {
		g.heldX, g.heldY, g.held = dx, dy, 0
		g.MoveCursor(dx, dy)
		return
	}
	g.held++
	if g.held >= cursorDelay && (g.held-cursorDelay)%cursorRepeat == 0 {
		g.MoveCursor(dx, dy)
	}
}

// axisStep returns -1, 0 or 1 for an axis value pushed past stickThreshold.
func axisStep(v float64) int {
	switch {
	case v <= -stickThreshold:
		return -1
	case v >= stickThreshold:
		return 1
	}
	return 0
}

// MoveCursor moves the cursor by (dx, dy) tiles, staying on the map.
func (g *Game) MoveCursor(dx, dy int) {
	c := ai.Cell{Col: g.Cursor.Col + dx, Row: g.Cursor.Row + dy}
	cols, rows := g.World.gridSize()
	c.Col = min(max(c.Col, 0), cols-1)
	c.Row = min(max(c.Row, 0), rows-1)
	g.Cursor = c
}

// Tap acts on a press at p on the canvas, in its view coordinates: the first tap on a cell moves the cursor there, and a second builds the chosen tower on it.
func (g *Game) Tap(p core.Point) {
	if g.World.Outcome != Playing || g.Paused {
		return
	}
	gp, ok := g.Viewport.ViewToGame(g.canvasSize, p)
	if !ok {
		return
	}
	c, ok := g.World.CellAt(float32(gp.X), float32(gp.Y))
	if !ok {
		return
	}
	if c == g.Cursor && g.World.Buildable(c) {
		g.Build()
		return
	}
	g.Cursor = c
}

// CellPoint returns the center of cell c in the canvas's view coordinates, where Tap takes it.
func (g *Game) CellPoint(c ai.Cell) core.Point {
	center := g.World.cellCenter(c)
	return g.Viewport.GameToView(g.canvasSize, center.Point())
}

// SelectKind chooses the tower kind at index i of data/towers.yaml, wrapping around.
func (g *Game) SelectKind(i int) {
	n := len(g.World.Data.Towers.Towers)
	g.Kind = ((i % n) + n) % n
}

// KindDef returns the chosen tower kind.
func (g *Game) KindDef() *TowerDef { return &g.World.Data.Towers.Towers[g.Kind] }

// Build builds the chosen tower kind on the cursor. It does nothing while the game is paused.
func (g *Game) Build() {
	if g.Paused {
		return
	}
	if t, ok := g.World.TowerAt(g.Cursor); ok {
		g.say(fmt.Sprintf("%s already stands here", t.Def.Label))
		return
	}
	if _, err := g.World.Build(g.KindDef(), g.Cursor); err != nil {
		g.say(capitalize(err.Error()))
		return
	}
	g.say("")
}

// Upgrade buys the next upgrade of the tower on the cursor. It does nothing while the game is paused.
func (g *Game) Upgrade() {
	if g.Paused {
		return
	}
	t, ok := g.World.TowerAt(g.Cursor)
	if !ok {
		g.say("No tower here")
		return
	}
	if err := g.World.Upgrade(t); err != nil {
		g.say(capitalize(err.Error()))
		return
	}
	g.say("")
}

// Sell sells the tower on the cursor. It does nothing while the game is paused.
func (g *Game) Sell() {
	if g.Paused {
		return
	}
	t, ok := g.World.TowerAt(g.Cursor)
	if !ok {
		g.say("No tower here")
		return
	}
	if err := g.World.Sell(t); err != nil {
		g.say(capitalize(err.Error()))
	}
}

// CallWave sends the next wave. It does nothing while the game is paused.
func (g *Game) CallWave() {
	if g.Paused {
		return
	}
	if g.World.CallWave() {
		g.say(fmt.Sprintf("Wave %d", g.World.Wave))
		return
	}
	if g.World.Spawning() {
		g.say("The wave is still coming")
	}
}

// CycleSpeed steps the speed from 1 up to maxSpeed and back to 1.
func (g *Game) CycleSpeed() {
	g.Speed = g.Speed%maxSpeed + 1
}

// TogglePause pauses or resumes the world.
func (g *Game) TogglePause() {
	if g.World.Outcome == Playing {
		g.Paused = !g.Paused
		g.Actions.Reset()
	}
}

// say shows msg in the HUD for a while; an empty msg clears it.
func (g *Game) say(msg string) {
	g.message = msg
	g.messageAge = 0
	if msg != "" {
		g.messageAge = messageSteps
	}
}

// HUD returns what the HUD shows now.
func (g *Game) HUD() HUD {
	w := g.World
	h := HUD{
		Level:       w.Data.Level.Name,
		Money:       w.Money,
		Lives:       w.Lives,
		Wave:        w.Wave,
		Waves:       w.Waves(),
		Kind:        g.Kind,
		Speed:       g.Speed,
		Paused:      g.Paused,
		Outcome:     w.Outcome,
		CanCallWave: w.CanCallWave(),
		Message:     g.message,
	}
	if t, ok := w.TowerAt(g.Cursor); ok {
		h.HasTower = true
		h.Tower = t.Def.Label
		h.TowerLevel = t.Level + 1
		h.UpgradeCost, _ = t.UpgradeCost()
		h.SellValue = w.SellValue(t)
	}
	return h
}

// syncHUD publishes the HUD when it has changed.
func (g *Game) syncHUD() {
	if h := g.HUD(); h != g.hud.GetUntracked() {
		g.hud.Set(h)
	}
}

// act runs an action from a HUD button and updates the HUD at once.
func (g *Game) act(fn func()) func(core.PressEvent) {
	return func(core.PressEvent) {
		fn()
		g.syncHUD()
	}
}

func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

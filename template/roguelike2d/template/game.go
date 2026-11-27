package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// The actions and axes game.yaml's input map declares. The game reads the movement axes, wait and restart itself; an ability's action is the one data/abilities.yaml names, which update looks up there, and the touch controls draw quaff and cleave.
const (
	axisMoveX     = "moveX"
	axisMoveY     = "moveY"
	actionWait    = "wait"
	actionQuaff   = "quaff"
	actionCleave  = "cleave"
	actionRestart = "restart"
)

// Holding a direction steps once, then again after repeatDelay steps of the loop and every repeatEvery steps after that.
const (
	repeatDelay = 14
	repeatEvery = 6
)

// moveThreshold is how far an axis must be pushed to step, so a resting stick does not walk.
const moveThreshold = 0.5

// roguelikeGame is the running app: the current run, the input it is played with, the virtual screen it is drawn on, the save slots and the signals the native HUD reads.
type roguelikeGame struct {
	data     *gameData
	run      *run
	actions  *input.Actions
	viewport screen.Viewport
	env      *core.Env
	viewSize core.Size
	store    *save.Store
	records  records
	// keepRecords and keepRun are set when a slot was written by a newer version of the game, which this one leaves alone.
	keepRecords, keepRun bool
	// newSeed picks each new run's seed.
	newSeed func() uint64
	// pointerSeen turns the on-screen controls on once the game view has been touched or clicked.
	pointerSeen bool

	// held is the direction being held and repeat the loop steps until it steps again.
	held   cell
	repeat int

	// turn changes after every turn and state when the run ends, so the HUD and the end panel redraw.
	turn  *core.Signal[int]
	state *core.Signal[outcome]
	frame *core.Signal[uint64]
}

// options are what the tests change about the game.
type options struct {
	// seed picks each new run's seed; nil seeds from the clock.
	seed func() uint64
	// hook receives the game once it is built.
	hook func(*roguelikeGame)
}

// Root is the game's root NSGo component: the run drawn on a full-window Canvas by a fixed-step game.Loop, with a native HUD on top.
func Root(cx *core.Scope) core.Node {
	return newRoot(cx, options{})
}

// newRoot builds the root component with opts.
func newRoot(cx *core.Scope, opts options) core.Node {
	desktop.SetTitle(cx, Project.Title())
	// The 320x180 viewport is wide: a phone turns sideways while the game runs.
	cx.RequestOrientation(core.OrientationLandscape)
	viewport, _ := Project.Viewport()
	g := &roguelikeGame{
		data:     mustLoadData(),
		actions:  input.NewActions(),
		viewport: viewport,
		env:      cx.Env(),
		store:    newSaveStore(cx.Storage()),
		newSeed:  opts.seed,
		turn:     core.NewSignal(cx, 0),
		state:    core.NewSignal(cx, playing),
		frame:    core.NewSignal(cx, uint64(0)),
	}
	if g.newSeed == nil {
		g.newSeed = func() uint64 { return uint64(time.Now().UnixNano()) }
	}
	g.records, g.keepRecords = loadRecords(g.store)
	g.start()

	// Keyboard, controller, touch and mouse bindings all come from game.yaml's input map; touch and mouse regions are in the virtual screen, which MapPoint maps view points into.
	Project.BindInput(g.actions)
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return g.viewport.ViewToGame(g.currentViewSize(), p)
	}
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	if opts.hook != nil {
		opts.hook(g)
	}
	return g.view()
}

// start resumes the run in the run slot, or starts a new one when there is none.
func (g *roguelikeGame) start() {
	s, ok, newer := loadRun(g.store)
	g.keepRun = newer
	if ok {
		if r, err := resumeRun(g.data, s, g.saveRun); err == nil {
			g.setRun(r)
			return
		}
		// A slot that does not fit this game's data, such as one saved before an edit to it, cannot be resumed; a new run replaces it.
	}
	g.newRun()
}

// newRun starts a new run with a new seed.
func (g *roguelikeGame) newRun() {
	r, err := newRun(g.data, g.newSeed(), g.saveRun)
	if err != nil {
		// The tests generate every floor of many seeds, so this only happens after an edit to the data they would have caught.
		panic(err)
	}
	g.setRun(r)
}

func (g *roguelikeGame) setRun(r *run) {
	g.run = r
	g.actions.Reset()
	g.held, g.repeat = cell{}, 0
	g.state.Set(r.state)
	g.turn.Update(func(n int) int { return n + 1 })
}

// saveRun writes the run slot, which the run asks for as the player arrives on a floor and after every turn.
func (g *roguelikeGame) saveRun(s runSave) error {
	if g.keepRun {
		return nil
	}
	return g.store.Save(runSlot, s)
}

// finish ends the run: permadeath deletes its slot whether it was won or lost, and the records count it.
func (g *roguelikeGame) finish() {
	if !g.keepRun {
		if err := g.store.Delete(runSlot); err != nil {
			g.run.say("The run slot could not be deleted: " + err.Error())
		}
	}
	g.records.add(g.run.summarize())
	if !g.keepRecords {
		if err := g.store.Save(recordsSlot, g.records); err != nil {
			g.run.say("The records could not be saved: " + err.Error())
		}
	}
	g.state.Set(g.run.state)
}

// update runs one fixed step: it samples the actions, turns them into at most one of the player's actions and plays it. Once the run has ended, restart starts another.
func (g *roguelikeGame) update(time.Duration) {
	g.actions.Step()
	if g.run.state != playing {
		if g.actions.JustPressed(actionRestart) {
			g.newRun()
		}
		return
	}
	var a action
	var ok bool
	switch dir := g.direction(); {
	case dir != (cell{}):
		if dir != g.held {
			g.held, g.repeat = dir, repeatDelay
			a, ok = action{kind: actMove, dir: dir}, true
		} else if g.repeat--; g.repeat <= 0 {
			g.repeat = repeatEvery
			a, ok = action{kind: actMove, dir: dir}, true
		}
	default:
		g.held = cell{}
	}
	if !ok && g.actions.JustPressed(actionWait) {
		a, ok = action{kind: actWait}, true
	}
	// Each ability is used with the input action data/abilities.yaml gives it, so a new ability needs no case here.
	for i := range g.data.abilities.Abilities {
		if ok {
			break
		}
		if def := &g.data.abilities.Abilities[i]; def.Action != "" && g.actions.JustPressed(def.Action) {
			a, ok = action{kind: actAbility, ability: def.ID}, true
		}
	}
	if ok {
		g.play(a)
	}
}

// play plays a and, when it ends the run, finishes it.
func (g *roguelikeGame) play(a action) {
	g.run.act(a)
	if g.run.state != playing {
		g.finish()
	}
	g.turn.Update(func(n int) int { return n + 1 })
}

// direction returns the step the movement axes ask for: the axis pushed furthest past moveThreshold, horizontal on a tie.
func (g *roguelikeGame) direction() cell {
	x, y := g.actions.Axis(axisMoveX), g.actions.Axis(axisMoveY)
	ax, ay := x, y
	if ax < 0 {
		ax = -ax
	}
	if ay < 0 {
		ay = -ay
	}
	switch {
	case ax >= moveThreshold && ax >= ay:
		return cell{X: int(sign64(x))}
	case ay >= moveThreshold:
		return cell{Y: int(sign64(y))}
	}
	return cell{}
}

func sign64(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// currentViewSize returns the game view's size, or the window's before the view is laid out.
func (g *roguelikeGame) currentViewSize() core.Size {
	if g.viewSize.Width > 0 && g.viewSize.Height > 0 {
		return g.viewSize
	}
	return g.env.Window()
}

// showTouchControls reports whether to draw the on-screen controls: on phones and tablets, and anywhere once the game view has been touched or clicked.
func (g *roguelikeGame) showTouchControls() bool {
	switch g.env.Idiom() {
	case core.IdiomDesktop, core.IdiomMac, core.IdiomTV:
		return g.pointerSeen
	default:
		return true
	}
}

// view lays out the game view, which hears the input and draws the run, under the HUD and the end-of-run panel.
func (g *roguelikeGame) view() core.Node {
	gameView := core.View{
		TestID: "game-view",
		Style: core.Style{
			Position: core.Absolute,
			Top:      core.Pt(0),
			Right:    core.Pt(0),
			Bottom:   core.Pt(0),
			Left:     core.Pt(0),
		},
		Focusable:   func() bool { return true },
		OnLayout:    func(b core.Bounds) { g.viewSize = b.Size },
		OnTouch:     func(core.TouchEvent) { g.pointerSeen = true },
		OnPressDown: func(core.PressEvent) { g.pointerSeen = true },
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Dungeon floor",
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
					Left:     core.Pt(0),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					g.viewport.Draw(d, size, func(d *core.Drawing, virtual core.Size) {
						drawRun(d, g.run, virtual)
						if g.showTouchControls() {
							drawTouchControls(d, g.actions)
						}
					})
				},
			},
		},
	}
	g.actions.Attach(&gameView)

	return core.View{
		Style: core.Style{FlexGrow: 1, Background: core.RGB(colorVoid)},
		Children: []core.Node{
			gameView,
			g.hud(),
			core.Show{
				When: func() bool { return g.state.Get() != playing },
				Then: func(*core.Scope) core.Node { return g.endPanel() },
			},
		},
	}
}

var (
	hudText   = core.Style{Color: core.RGB(0xf8fafc), FontSize: 14, FontWeight: core.FontWeightBold}
	hudDim    = core.Style{Color: core.RGB(0xcbd5e1), FontSize: 12}
	panelText = core.Style{Color: core.RGB(0xcbd5e1), FontSize: 14}
)

// hud shows the player's health, floor, level, shards, draughts and cleave in the top left corner and the newest message under them, letting touches through to the game.
func (g *roguelikeGame) hud() core.Node {
	line := func(id string, style core.Style, text func(r *run) string) core.Node {
		return core.Text{
			TestID: id,
			ContentFunc: func() string {
				_ = g.turn.Get()
				return text(g.run)
			},
			Style: style,
		}
	}
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Right:      core.Pt(0),
			Padding:    core.All(10),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
			AlignItems: core.AlignStart,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Gap:          3,
					Padding:      core.Symmetric(6, 10),
					CornerRadius: 8,
					Background:   core.RGBA(0x0b1020, 0.78),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{
							line("health", hudText, func(r *run) string { return fmt.Sprintf("HP %d/%d", r.health, r.maxHealth()) }),
							line("floor", hudText, func(r *run) string {
								return fmt.Sprintf("Floor %d/%d", r.floor.index+1, len(r.data.dungeon.Floors))
							}),
							line("level", hudText, func(r *run) string { return fmt.Sprintf("Lv %d", r.level) }),
							line("shards", hudText, func(r *run) string { return fmt.Sprintf("Shards %d", r.shards) }),
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 12},
						Children: []core.Node{
							line("draughts", hudDim, func(r *run) string { return fmt.Sprintf("Draughts %d", r.draughts) }),
							line("cleave", hudDim, func(r *run) string {
								st, _ := r.caster.Status(abilityCleave)
								if st.Cooldown > 0 {
									return fmt.Sprintf("Cleave in %d", st.Cooldown)
								}
								return "Cleave ready"
							}),
						},
					},
					line("message", hudDim, func(r *run) string { return r.lastMessage() }),
				},
			},
		},
	}
}

// endPanel shows the run summary and the records, and offers another run.
func (g *roguelikeGame) endPanel() core.Node {
	s := g.run.summarize()
	return core.View{
		TestID: "end-panel",
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     core.RGBA(0x020617, 0.6),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(18, 28),
					CornerRadius: 12,
					Background:   core.RGBA(0x0f172a, 0.94),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "end-title",
						Content: s.headline(),
						Style:   core.Style{Color: core.RGB(0xfde68a), FontSize: 24, FontWeight: core.FontWeightBold},
					},
					core.Text{TestID: "end-cause", Content: s.cause(), Style: panelText},
					core.Text{TestID: "end-tally", Content: s.tally(), Style: panelText},
					core.Text{TestID: "end-records", Content: g.records.line(), Style: core.Style{Color: core.RGB(0x94a3b8), FontSize: 12}},
					base.Button{
						TestID:  "play-again",
						Label:   "Play again",
						OnPress: func(core.PressEvent) { g.newRun() },
						Style: core.Style{
							Padding:      core.Symmetric(8, 18),
							CornerRadius: 8,
							Background:   core.RGB(0x2563eb),
							Active:       &core.Style{Opacity: core.Alpha(0.8)},
						},
						Children: []core.Node{
							core.Text{Content: "Play again", Style: hudText},
						},
					},
					core.Text{Content: "R, Enter or Start", Style: core.Style{Color: core.RGB(0x94a3b8), FontSize: 12}},
				},
			},
		},
	}
}

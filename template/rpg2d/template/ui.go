package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// Root is the game's root component: the canvas the game draws on, scaled from game.yaml's display to the window, with a native HUD and menus over it.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, Project.Title())
	// The 480x270 viewport is wide: a phone turns sideways while the game runs.
	cx.RequestOrientation(core.OrientationLandscape)
	a, err := start(cx)
	if err != nil {
		// A data file that does not load is shown rather than crashing, so a designer editing it under go tool nsgo dev sees what to fix.
		return core.View{
			Style: core.Style{FlexGrow: 1, Padding: core.All(24), Background: core.RGB(0x10131f)},
			Children: []core.Node{
				core.Text{TestID: "load-error", Content: err.Error(), Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: 14}},
			},
		}
	}
	return a.node()
}

// start loads the data files and starts the game on its title screen, its saves in the app's storage.
func start(cx *core.Scope) (*app, error) {
	c, err := loadContent()
	if err != nil {
		return nil, err
	}
	store := save.NewStorageStore(cx.Storage(), saveKeyPrefix, save.WithVersion(saveVersion))
	return newApp(cx, newGame(c, store, Project.Title())), nil
}

// sceneOf returns the scene that shows mode m: the map stays up behind conversations and the pack.
func sceneOf(m mode) string {
	switch m {
	case modeTitle:
		return "title"
	case modeBattle:
		return "battle"
	case modeLost:
		return "lost"
	case modeWon:
		return "won"
	}
	return "field"
}

// hudState is what the HUD shows, compared every frame so the HUD only changes when it does.
type hudState struct {
	field         bool
	place         string
	hp, maxHP     int
	coins         int
	quest         string
	message       string
	messageHidden bool
}

// app connects a Game to the window: it runs the fixed-step loop, reads the input map into intents, switches scenes and builds the native HUD and menus.
type app struct {
	g        *Game
	actions  *input.Actions
	viewport screen.Viewport
	scenes   *scene.Manager
	// size is the stage's size in the window; touch is whether to draw the on-screen pad, from the start on phones and tablets and from the first press on the stage elsewhere, since game.yaml binds the pad's regions to the mouse as well as to touch.
	size  core.Size
	touch bool
	// prev is the direction read last step; tap is a press on the stage waiting for the next step.
	prev  dir
	tap   *core.Point
	shown string
	// steps counts the fixed steps run.
	steps uint64

	frame *core.Signal[uint64]
	hud   *core.Signal[hudState]
	rev   *core.Signal[uint64]
	sel   *core.Signal[int]
}

// newApp binds game.yaml's input map, starts the scenes on the title and runs the game loop under cx.
func newApp(cx *core.Scope, g *Game) *app {
	a := &app{
		g:       g,
		actions: input.NewActions(),
		scenes:  scene.NewManager(cx),
		frame:   core.NewSignal(cx, uint64(0)),
		rev:     core.NewSignal(cx, g.rev),
		sel:     core.NewSignal(cx, g.sel),
	}
	a.hud = core.NewSignal(cx, a.hudState())
	a.viewport, _ = Project.Viewport()
	idiom := cx.Env().Idiom()
	a.touch = idiom == core.IdiomPhone || idiom == core.IdiomTablet
	Project.BindInput(a.actions)
	// The touch pad's regions are in game points, so presses are mapped from the window to the canvas first.
	a.actions.MapPoint = func(p core.Point) (core.Point, bool) { return a.viewport.ViewToGame(a.size, p) }
	a.actions.WatchKeys(cx)
	a.actions.WatchGamepads(cx)
	cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			a.actions.Reset()
		}
	})
	a.shown = sceneOf(g.mode)
	a.scenes.Switch(a.controller(a.shown), nil)
	loop := &game.Loop{Rate: 60, Update: a.update, Draw: a.draw}
	loop.Run(cx)
	return a
}

// update runs one fixed step: it samples the input, moves the game on and shows the scene the game's mode calls for.
func (a *app) update(dt time.Duration) {
	a.steps++
	a.actions.Step()
	in := readIntent(a.actions, a.prev)
	a.prev = in.dir
	in.tap, a.tap = a.tap, nil
	a.g.update(dt, in)
	if s := sceneOf(a.g.mode); s != a.shown {
		a.shown = s
		a.scenes.Switch(a.controller(s), scene.Fade(250*time.Millisecond, core.RGB(0x0b0d17)))
	}
}

// draw publishes the step's results to the canvas, the HUD and the menus.
func (a *app) draw(float64) {
	a.frame.Update(func(n uint64) uint64 { return n + 1 })
	if h := a.hudState(); h != a.hud.Get() {
		a.hud.Set(h)
	}
	if a.rev.Get() != a.g.rev {
		a.rev.Set(a.g.rev)
	}
	if a.sel.Get() != a.g.sel {
		a.sel.Set(a.g.sel)
	}
}

// hudState reads what the HUD shows from the game.
func (a *app) hudState() hudState {
	g := a.g
	return hudState{
		field:         g.mode == modeField || g.mode == modeTalk,
		place:         g.level.Name,
		hp:            g.hp,
		maxHP:         maxHP(&g.progress),
		coins:         g.bag.Count(itemCoin),
		quest:         g.questTracker(),
		message:       g.message,
		messageHidden: g.mode == modeBattle,
	}
}

// controller returns the scene called name, which draws one of the game's screens.
func (a *app) controller(name string) *scene.Controller {
	return &scene.Controller{Draw: func(d *core.Drawing, _ float64) {
		switch name {
		case "title":
			a.g.drawShade(d, core.RGBA(0x0b0d17, 0.55))
		case "battle":
			a.g.drawBattle(d)
		case "lost":
			a.g.drawShade(d, core.RGBA(0x3a0d12, 0.65))
		case "won":
			a.g.drawShade(d, core.RGBA(0xf2d14b, 0.25))
		default:
			a.g.drawField(d, a.touch)
		}
	}}
}

// node builds the window's content: the stage with the canvas, the HUD and the menu over it.
func (a *app) node() core.Node {
	stage := core.View{
		TestID:   "stage",
		NoFocus:  true,
		OnLayout: func(b core.Bounds) { a.size = b.Size },
		// A tap or click picks a conversation's option or moves the conversation on, and shows the on-screen pad for playing on.
		OnPressDown: func(ev core.PressEvent) {
			a.touch = true
			if p, ok := a.viewport.ViewToGame(a.size, ev.Position); ok {
				a.tap = &p
			}
		},
		Style: cover(),
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  Project.Title(),
				Style:  cover(),
				Draw: func(d *core.Drawing, size core.Size) {
					_ = a.frame.Get()
					a.viewport.Draw(d, size, func(d *core.Drawing, vsize core.Size) {
						a.scenes.Draw(d, vsize, 0)
					})
				},
			},
		},
	}
	a.actions.Attach(&stage)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x0b0d17)},
		Children: []core.Node{
			stage,
			a.hudNode(),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = a.rev.Get()
				if v, ok := a.g.menuView(); ok {
					return a.menuNode(v)
				}
				return core.Group{}
			}),
		},
	}
}

// hudNode is the bar along the top of the map, with the place, HP, coins, the quest's next step and the button that opens the pack, and the latest message below it.
func (a *app) hudNode() core.Node {
	badge := func(id string, color core.Color, content func(h hudState) string) core.Node {
		return core.Text{
			TestID:      id,
			ContentFunc: func() string { return content(a.hud.Get()) },
			Style:       core.Style{Color: color, FontSize: 13, FontWeight: core.FontWeightSemibold},
		}
	}
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Right:      core.Pt(0),
			Padding:    core.All(10),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
			Gap:        6,
			AlignItems: core.AlignStart,
		},
		Children: []core.Node{
			core.Show{
				When: func() bool { return a.hud.Get().field },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						PointerEvents: core.PointerEventsBoxNone,
						Style:         core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, AlignSelf: core.AlignStretch},
						Children: []core.Node{
							core.View{
								PointerEvents: core.PointerEventsNone,
								Style:         core.Style{Direction: core.Row, Gap: 12, Background: core.RGBA(0x10131f, 0.8), CornerRadius: 8, Padding: core.Symmetric(6, 12)},
								Children: []core.Node{
									badge("hud-place", core.RGB(0xf8fafc), func(h hudState) string { return h.place }),
									badge("hud-hp", core.RGB(0xf27074), func(h hudState) string { return fmt.Sprintf("HP %d/%d", h.hp, h.maxHP) }),
									badge("hud-coins", core.RGB(0xf2d14b), func(h hudState) string { return fmt.Sprintf("Coins %d", h.coins) }),
									badge("hud-quest", core.RGB(0x9fd8ff), func(h hudState) string { return h.quest }),
								},
							},
							core.View{Style: core.Style{FlexGrow: 1}, PointerEvents: core.PointerEventsNone},
							a.button("hud-pack", "Pack", false, func() bool { return false }, func() {
								if a.g.mode == modeField {
									a.g.showMenu(modeMenu, pagePack)
								}
							}),
						},
					}
				},
			},
			core.Show{
				When: func() bool { h := a.hud.Get(); return h.message != "" && !h.messageHidden },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						PointerEvents: core.PointerEventsNone,
						Style:         core.Style{Background: core.RGBA(0x10131f, 0.85), CornerRadius: 8, Padding: core.Symmetric(6, 12), AlignSelf: core.AlignCenter},
						Children: []core.Node{
							core.Text{TestID: "message", ContentFunc: func() string { return a.hud.Get().message }, Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: 13}},
						},
					}
				},
			},
		},
	}
}

// menuNode builds a menu: its title, its lines and a button per entry, the selected one outlined for keyboard and gamepad players. The buttons take no keyboard focus, since the input map's keys already move through them.
func (a *app) menuNode(v menuView) core.Node {
	children := []core.Node{
		core.Text{Content: v.title, Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: 18, FontWeight: core.FontWeightBold}},
	}
	for _, line := range v.lines {
		children = append(children, core.Text{Content: line, Style: core.Style{Color: core.RGB(0xc7cde0), FontSize: 13}})
	}
	for i, item := range v.items {
		children = append(children, a.button("menu-"+item.id, item.label, item.disabled, func() bool { return a.sel.Get() == i }, func() { a.g.pick(item.id) }))
	}
	justify := core.JustifyCenter
	if v.bottom {
		justify = core.JustifyEnd
	}
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Padding:        core.All(16),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
			AlignItems:     core.AlignCenter,
			JustifyContent: justify,
		},
		Children: []core.Node{
			core.View{
				TestID:   "menu",
				Style:    core.Style{Background: core.RGBA(0x10131f, 0.94), CornerRadius: 12, Padding: core.All(16), Gap: 8, MinWidth: core.Pt(280), MaxWidth: core.Pct(100)},
				Children: children,
			},
		},
	}
}

// button is a pressable entry that takes no keyboard focus, outlined while selected returns true.
func (a *app) button(id, label string, disabled bool, selected func() bool, press func()) core.Node {
	return core.View{
		TestID:   id,
		Role:     core.RoleButton,
		Label:    label,
		NoFocus:  true,
		Disabled: func() bool { return disabled },
		Selected: selected,
		OnPress:  func(core.PressEvent) { press() },
		StyleFunc: func() core.Style {
			st := core.Style{Background: core.RGB(0x2a3150), CornerRadius: 8, Padding: core.Symmetric(8, 14), BorderWidth: 2, BorderColor: core.RGBA(0x000000, 0)}
			if selected() {
				st.BorderColor = core.RGB(0xf2d14b)
			}
			if disabled {
				st.Opacity = core.Alpha(0.45)
			}
			return st
		},
		Children: []core.Node{
			core.Text{Content: label, Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: 14, FontWeight: core.FontWeightSemibold}},
		},
	}
}

// cover is a style that covers the parent.
func cover() core.Style {
	return core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}
}

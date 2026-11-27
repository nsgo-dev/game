package main

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// Root is the game's root NSGo component: a HUD bar, the battlefield drawn on a Canvas and driven by a fixed-step game.Loop, and the command bar, with the pause and result screens over them.
func Root(cx *core.Scope) core.Node { return rootWith(cx, nil) }

// rootWith is Root, handing the skirmish it starts to started when that is set, as the tests do.
func rootWith(cx *core.Scope, started func(*rootSkirmish)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	g := newRootSkirmish(cx)
	if started != nil {
		started(g)
	}
	return g.view(cx)
}

// rootSkirmish is the skirmish Root shows, with the signals its HUD reads.
type rootSkirmish struct {
	*skirmish
	loop    *game.Loop
	frame   *core.Signal[uint64]
	version *core.Signal[uint64]
	state   hudState
}

// newRootSkirmish starts a skirmish with game.yaml's input bindings, a keyboard watch and the game controllers, on a 60 Hz loop.
func newRootSkirmish(cx *core.Scope) *rootSkirmish {
	actions := input.NewActions()
	Project.BindInput(actions)
	actions.WatchKeys(cx)
	actions.WatchGamepads(cx)
	viewport, _ := Project.Viewport()
	g := &rootSkirmish{
		skirmish: newSkirmish(mustLoadGameData(), actions, viewport),
		frame:    core.NewSignal(cx, uint64(0)),
		version:  core.NewSignal(cx, uint64(0)),
	}
	g.state = g.hud()
	g.changed = func() {
		g.state = g.hud()
		g.version.Update(func(n uint64) uint64 { return n + 1 })
	}
	// The world is drawn as of its last step: units move a fraction of a point a step, so Draw's alpha is not needed to look smooth.
	g.loop = &game.Loop{
		Rate:   stepRate,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	g.loop.Run(cx)
	return g
}

// hudNow returns the HUD's numbers, subscribing to their changes.
func (g *rootSkirmish) hudNow() hudState {
	_ = g.version.Get()
	return g.state
}

func (g *rootSkirmish) view(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x0b1120), PaddingEnv: core.EnvAll(core.EnvSafeArea)},
		Children: []core.Node{
			g.topBar(),
			g.battlefield(),
			g.commandBar(),
			g.overlay(),
		},
	}
}

// battlefield is the view the mouse and fingers play on, with the Canvas the world is drawn on filling it.
func (g *rootSkirmish) battlefield() core.Node {
	return core.View{
		TestID:      "battlefield",
		Style:       core.Style{FlexGrow: 1, FlexShrink: 1},
		OnLayout:    func(b core.Bounds) { g.viewSize = b.Size },
		OnMouseDown: g.mouseDown,
		OnMouseMove: g.mouseMove,
		OnMouseUp:   g.mouseUp,
		OnTouch:     g.touch,
		Children: []core.Node{
			core.Canvas{
				TestID: "battlefield-canvas",
				Label:  "Battlefield",
				Style:  core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					g.viewSize = size
					g.viewport.Draw(d, size, func(d *core.Drawing, _ core.Size) { g.draw(d) })
				},
			},
		},
	}
}

var (
	textColor  = core.RGB(0xe2e8f0)
	mutedColor = core.RGB(0x94a3b8)
	barColor   = core.RGB(0x111827)
)

// topBar shows the crystals, the units and the selection, and pauses.
func (g *rootSkirmish) topBar() core.Node {
	stat := func(id string, content func() string) core.Node {
		return core.Text{TestID: id, ContentFunc: content, Style: core.Style{Color: textColor, FontSize: 14, FontWeight: core.FontWeightBold}}
	}
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16, Padding: core.Symmetric(6, 12), Background: barColor},
		Children: []core.Node{
			stat("crystals", func() string { return fmt.Sprintf("Crystals %d", g.hudNow().crystals) }),
			stat("supply", func() string { s := g.hudNow(); return fmt.Sprintf("Units %d/%d", s.supply, s.maxSupply) }),
			core.Text{
				TestID:      "selected",
				ContentFunc: func() string { return fmt.Sprintf("%d selected", g.hudNow().selected) },
				Style:       core.Style{Color: mutedColor, FontSize: 13, FlexGrow: 1},
			},
			button("Pause", "pause", g.togglePause, nil),
		},
	}
}

// commandBar holds the production and selection buttons, the touch player's way to every hotkey.
func (g *rootSkirmish) commandBar() core.Node {
	cost := func(unit string) string {
		u := g.data.rules.unit(unit)
		return fmt.Sprintf("%s %d", title(u.Name), u.Cost)
	}
	trainLabel := func(unit string) func() string {
		return func() string {
			if n := g.hudNow().queued[unit]; n > 0 {
				return fmt.Sprintf("%s ×%d", cost(unit), n)
			}
			return cost(unit)
		}
	}
	can := func(unit string) func() bool { return func() bool { return !g.hudNow().canTrain[unit] } }
	return core.View{
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8, Padding: core.Symmetric(8, 12), Background: barColor, JustifyContent: core.JustifyCenter},
		Children: []core.Node{
			labelButton(trainLabel(workerName), "train-worker", g.trainWorker, can(workerName)),
			labelButton(func() string {
				return fmt.Sprintf("Barracks %d", g.data.rules.building(barracksName).Cost)
			}, "build-barracks", g.buildBarracks, func() bool { return !g.hudNow().canBuild }),
			labelButton(trainLabel(soldierName), "train-soldier", func() { g.train(soldierName) }, can(soldierName)),
			labelButton(trainLabel(rangerName), "train-ranger", func() { g.train(rangerName) }, can(rangerName)),
			button("Army", "select-army", g.selectArmy, nil),
			button("Workers", "select-workers", g.selectWorkers, nil),
			button("Clear", "deselect", g.deselect, func() bool { return g.hudNow().selected == 0 }),
		},
	}
}

// overlay is the pause screen and the result screen.
func (g *rootSkirmish) overlay() core.Node {
	return core.Show{
		When: func() bool { s := g.hudNow(); return s.paused || s.over },
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "overlay",
				Style: core.Style{
					Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0),
					AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Background: core.RGBA(0x020617, 0.7),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Background: barColor, CornerRadius: 12, Padding: core.Symmetric(20, 28), Gap: 14, AlignItems: core.AlignCenter},
						Children: []core.Node{
							core.Text{
								TestID: "result",
								ContentFunc: func() string {
									switch s := g.hudNow(); {
									case s.won:
										return "Victory"
									case s.drawn:
										return "Draw"
									case s.over:
										return "Defeat"
									}
									return "Paused"
								},
								Style: core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold},
							},
							labelButton(func() string {
								if g.hudNow().over {
									return "Play again"
								}
								return "Resume"
							}, "overlay-action", func() {
								if g.w.over {
									g.restart()
								} else {
									g.togglePause()
								}
							}, nil),
						},
					},
				},
			}
		},
	}
}

// title returns name with its first letter upper-cased, as the command bar shows a type's name.
func title(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func button(label, id string, press func(), disabled func() bool) core.Node {
	return labelButton(func() string { return label }, id, press, disabled)
}

// labelButton is a HUD button whose label follows label; disabled, when set, turns it off while it reports true.
func labelButton(label func() string, id string, press func(), disabled func() bool) core.Node {
	return base.Button{
		TestID:    id,
		LabelFunc: label,
		OnPress:   func(core.PressEvent) { press() },
		Disabled:  disabled,
		Style: core.Style{
			Background: core.RGB(0x1e3a5f), CornerRadius: 8, Padding: core.Symmetric(8, 12),
			Active:   &core.Style{Opacity: core.Alpha(0.75)},
			Disabled: &core.Style{Opacity: core.Alpha(0.45)},
		},
		Children: []core.Node{
			core.Text{ContentFunc: label, Style: core.Style{Color: textColor, FontSize: 13, FontWeight: core.FontWeightBold}},
		},
	}
}

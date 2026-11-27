package main

import (
	"fmt"
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	textColor  = core.RGB(0xf8fafc)
	mutedColor = core.RGB(0x94a3b8)
	panelColor = core.RGBA(0x0f172a, 0.88)
)

// render is the game's view: the arena Canvas, which hears touch, the mouse and focused keys for the first player, under the native HUD and menus.
func (g *fightingGame) render() core.Node {
	arena := core.View{
		TestID:    "arena",
		Focusable: func() bool { return true },
		Style:     core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)},
		OnLayout:  func(b core.Bounds) { g.viewSize = b.Size },
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Fighting arena",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					g.draw(d, size)
				},
			},
		},
	}
	g.controls.attach(&arena)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x05060a)},
		Children: []core.Node{
			arena,
			core.Show{When: func() bool { s := g.screen.Get(); return s == screenFight || s == screenPaused || s == screenReconnect }, Then: func(*core.Scope) core.Node { return g.fightHUD() }},
			core.Show{When: func() bool { return g.screen.Get() == screenTitle }, Then: func(*core.Scope) core.Node { return g.titleMenu() }},
			core.Show{When: func() bool { return g.screen.Get() == screenPaused }, Then: func(*core.Scope) core.Node { return g.pauseMenu() }},
			core.Show{When: func() bool { return g.screen.Get() == screenReconnect }, Then: func(*core.Scope) core.Node { return g.reconnectMenu() }},
			core.Show{When: func() bool { return g.screen.Get() == screenResult }, Then: func(*core.Scope) core.Node { return g.resultMenu() }},
		},
	}
}

// overlay fills the screen above the arena, padded inside the safe area, and lets presses outside its children through to the arena.
func overlay(testID string, justify core.Justify, children ...core.Node) core.View {
	return core.View{
		TestID:        testID,
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			Padding:        core.All(14),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
			AlignItems:     core.AlignCenter,
			JustifyContent: justify,
			Gap:            10,
		},
		Children: children,
	}
}

// fightHUD shows the health bars, the rounds won, the timer, the banner and each side's combo counter, and a pause button for touch.
func (g *fightingGame) fightHUD() core.Node {
	side := func(i int) core.Node {
		align, justify := core.AlignStart, core.JustifyStart
		if i == 1 {
			align, justify = core.AlignEnd, core.JustifyEnd
		}
		p := g.m.players[i]
		return core.View{
			PointerEvents: core.PointerEventsNone,
			Style:         core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), AlignItems: align, Gap: 4},
			Children: []core.Node{
				core.View{
					TestID: fmt.Sprintf("p%d-health", i+1),
					Role:   core.RoleProgressBar,
					Label:  p.fighter.def.Name + " health",
					Style: core.Style{
						Width: core.Pct(100), Height: core.Pt(14), CornerRadius: 4,
						Background: core.RGBA(0x1e293b, 0.9), BorderWidth: 1, BorderColor: core.RGBA(0x94a3b8, 0.5),
						Direction: core.Row, JustifyContent: justify,
					},
					Children: []core.Node{core.View{StyleFunc: func() core.Style {
						return core.Style{Width: core.Pct(100 * g.hud.Get().health[i]), Height: core.Pct(100), CornerRadius: 3, Background: p.fighter.color}
					}}},
				},
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 6, AlignItems: core.AlignCenter},
					Children: []core.Node{
						core.Text{Content: p.fighter.def.Name, Style: core.Style{Color: textColor, FontSize: 13, FontWeight: core.FontWeightBold}},
						core.Text{
							TestID:      fmt.Sprintf("p%d-wins", i+1),
							ContentFunc: func() string { return pips(g.hud.Get().wins[i], g.roster.file.Rules.RoundsToWin) },
							Style:       core.Style{Color: p.fighter.accent, FontSize: 12},
						},
					},
				},
				core.Text{
					TestID: fmt.Sprintf("p%d-combo", i+1),
					ContentFunc: func() string {
						h := g.hud.Get()
						switch {
						case h.hits[i] > 0 && h.combo[i] != "":
							return fmt.Sprintf("%d hits · %s", h.hits[i], h.combo[i])
						case h.hits[i] > 0:
							return fmt.Sprintf("%d hits", h.hits[i])
						}
						return h.combo[i]
					},
					Style: core.Style{Color: p.fighter.accent, FontSize: 15, FontWeight: core.FontWeightBold},
				},
			},
		}
	}
	return overlay("hud", core.JustifySpaceBetween,
		core.View{
			PointerEvents: core.PointerEventsBoxNone,
			Style:         core.Style{Direction: core.Row, Width: core.Pct(100), Gap: 12, AlignItems: core.AlignStart},
			Children: []core.Node{
				side(0),
				core.View{
					Style: core.Style{AlignItems: core.AlignCenter, Gap: 6},
					Children: []core.Node{
						core.Text{
							TestID:      "timer",
							ContentFunc: func() string { return strconv.Itoa(g.hud.Get().seconds) },
							Style:       core.Style{Color: textColor, FontSize: 24, FontWeight: core.FontWeightBold},
						},
						button("pause-btn", "Pause", false, func() { g.pause(true) }),
					},
				},
				side(1),
			},
		},
		core.Text{
			TestID: "banner",
			ContentFunc: func() string {
				if g.screen.Get() != screenFight {
					return ""
				}
				return g.hud.Get().banner
			},
			Style: core.Style{Color: textColor, FontSize: 40, FontWeight: core.FontWeightBold},
		},
		core.View{Style: core.Style{Height: core.Pt(40)}},
	)
}

// pips draws rounds won out of need as filled and hollow dots.
func pips(won, need int) string {
	s := ""
	for i := range need {
		if i < won {
			s += "●"
		} else {
			s += "○"
		}
	}
	return s
}

// titleMenu picks the fighters and the opponent.
func (g *fightingGame) titleMenu() core.Node {
	pick := func(i int, who string) core.Node {
		return button(fmt.Sprintf("pick-p%d", i+1), who+" fighter", false, func() { g.cycle(i, 1) }, core.Text{
			ContentFunc: func() string { return who + "  " + g.roster.fighters[g.picks.Get()[i]].def.Name + "  ▸" },
			Style:       core.Style{Color: textColor, FontSize: 15, FontWeight: core.FontWeightBold},
		})
	}
	return overlay("title-menu", core.JustifyCenter,
		core.Text{Content: Project.Title(), Style: core.Style{Color: textColor, FontSize: 34, FontWeight: core.FontWeightBold}},
		panel(
			core.View{Style: core.Style{Direction: core.Row, Gap: 10}, Children: []core.Node{pick(0, "P1"), pick(1, "P2")}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 10}, Children: []core.Node{
				button("vs-cpu", "Versus CPU", true, func() { g.startMatch(modeCPU) }),
				button("vs-player", "Versus player", true, func() { g.startMatch(modeVersus) }),
			}},
			core.Text{
				Content: "P1  WASD · J light · K heavy · Enter     P2  Arrows · . light · / heavy · Right Shift",
				Style:   core.Style{Color: mutedColor, FontSize: 11},
			},
		),
	)
}

// pauseMenu resumes or quits the fight.
func (g *fightingGame) pauseMenu() core.Node {
	return overlay("pause-menu", core.JustifyCenter, panel(
		core.Text{Content: "Paused", Style: core.Style{Color: textColor, FontSize: 24, FontWeight: core.FontWeightBold}},
		core.View{Style: core.Style{Direction: core.Row, Gap: 10}, Children: []core.Node{
			button("resume-btn", "Resume", true, func() { g.pause(false) }),
			button("quit-btn", "Quit", false, g.quit),
		}},
	))
}

// reconnectMenu waits for disconnected versus controllers to return or be replaced, or lets those players continue on the keyboard.
func (g *fightingGame) reconnectMenu() core.Node {
	return overlay("reconnect-menu", core.JustifyCenter, panel(
		core.Text{
			TestID:      "reconnect-title",
			ContentFunc: func() string { return reconnectTitle(g.disconnected.Get()) },
			Style:       core.Style{Color: textColor, FontSize: 22, FontWeight: core.FontWeightBold},
		},
		core.Text{
			TestID:      "reconnect-hint",
			ContentFunc: func() string { return reconnectHint(g.disconnected.Get()) },
			Style:       core.Style{Color: mutedColor, FontSize: 13},
		},
		core.View{Style: core.Style{Direction: core.Row, Gap: 10}, Children: []core.Node{
			button("continue-keyboard-btn", "Continue on keyboard", true, g.continueOnKeyboard),
			button("quit-btn", "Quit", false, g.quit),
		}},
	))
}

func reconnectTitle(disc [2]bool) string {
	switch {
	case disc[0] && disc[1]:
		return "Players 1 & 2: reconnect your controllers"
	case disc[1]:
		return "Player 2: reconnect your controller"
	default:
		return "Player 1: reconnect your controller"
	}
}

func reconnectHint(disc [2]bool) string {
	switch {
	case disc[0] && disc[1]:
		return "Reconnect them, press any button on a free controller, or continue on keyboard (Enter / Right Shift)."
	case disc[1]:
		return "Reconnect it, press any button on a free controller, or press Right Shift for keyboard."
	default:
		return "Reconnect it, press any button on a free controller, or press Enter for keyboard."
	}
}

// resultMenu says who won and offers a rematch.
func (g *fightingGame) resultMenu() core.Node {
	return overlay("result-menu", core.JustifyCenter, panel(
		core.Text{TestID: "result", ContentFunc: g.result.Get, Style: core.Style{Color: textColor, FontSize: 30, FontWeight: core.FontWeightBold}},
		core.View{Style: core.Style{Direction: core.Row, Gap: 10}, Children: []core.Node{
			button("rematch-btn", "Rematch", true, func() { g.startMatch(g.mode) }),
			button("title-btn", "Title", false, g.quit),
		}},
	))
}

// panel groups a menu's rows on a dark card.
func panel(children ...core.Node) core.Node {
	return core.View{
		Style:    core.Style{Background: panelColor, CornerRadius: 14, Padding: core.All(18), Gap: 14, AlignItems: core.AlignCenter},
		Children: children,
	}
}

// button is a menu button. It takes no keyboard focus: the keys of game.yaml's input map drive the menus, so Return and Space never press a focused button on top of what they do in the game.
func button(testID, label string, primary bool, onPress func(), content ...core.Node) core.Node {
	bg := core.RGBA(0x334155, 0.95)
	if primary {
		bg = core.RGBA(0x0284c7, 0.95)
	}
	if len(content) == 0 {
		content = []core.Node{core.Text{Content: label, Style: core.Style{Color: textColor, FontSize: 15, FontWeight: core.FontWeightBold}}}
	}
	return base.Button{
		TestID:   testID,
		Label:    label,
		NoFocus:  true,
		OnPress:  func(core.PressEvent) { onPress() },
		Style:    core.Style{Background: bg, CornerRadius: 10, Padding: core.Symmetric(10, 16), Active: &core.Style{Opacity: core.Alpha(0.8)}},
		Children: content,
	}
}

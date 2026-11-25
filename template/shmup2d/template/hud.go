package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Colors of the HUD and menus.
var (
	textColor  = core.RGB(0xf8fafc)
	mutedColor = core.RGB(0x94a3b8)
	panelColor = core.RGBA(0x0f172a, 0.85)
	scrimColor = core.RGBA(0x02040c, 0.6)
	accent     = core.RGB(0x0284c7)
)

// fill is the style of a view laid over the whole of its parent.
func fill() core.Style {
	return core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}
}

// hud is the top of the screen during a run: the score and the lives and bombs left beside a pause button for touch screens, and the boss's name and health bar while it fights, with a mark where each later phase starts. Taps outside the button pass through to the playfield.
func (g *shooter) hud() core.Node {
	top := core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style:         core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, AlignItems: core.AlignStart},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style:         core.Style{Gap: 2},
				Children: []core.Node{
					hudText("score", 18, func() string { return fmt.Sprintf("Score %d", g.score.Get()) }),
					hudText("lives", 13, func() string { return fmt.Sprintf("Lives %d  Bombs %d", g.lives.Get(), g.bombs.Get()) }),
				},
			},
			button("pause", "Pause", func() { g.setPaused(true) }),
		},
	}
	marks := []core.Node{
		core.View{StyleFunc: func() core.Style {
			return core.Style{Width: core.Pct(100 * g.bossHP.Get()), Background: core.RGB(0xf43f5e), CornerRadius: 2}
		}},
	}
	for _, p := range g.data.stage.Boss.Phases[1:] {
		marks = append(marks, core.View{Style: core.Style{Position: core.Absolute, Top: core.Pt(-2), Bottom: core.Pt(-2), Left: core.Pct(100 * p.Below), Width: core.Pt(1), Background: core.RGB(0xfde047)}})
	}
	boss := core.View{
		TestID:        "boss-health",
		PointerEvents: core.PointerEventsNone,
		Style:         core.Style{Gap: 4},
		Children: []core.Node{
			hudText("boss", 13, g.boss.Get),
			core.View{Style: core.Style{Height: core.Pt(5), Background: core.RGBA(0x1e293b, 0.9), CornerRadius: 2, Direction: core.Row}, Children: marks},
		},
	}
	return when(func() bool { return g.phase.Get() == phasePlaying }, core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Right:      core.Pt(0),
			Gap:        10,
			Padding:    core.Symmetric(18, 12),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			top,
			when(func() bool { return g.boss.Get() != "" }, boss),
		},
	})
}

// menus are the title, pause and end screens, each over a dimmed playfield.
func (g *shooter) menus() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style:         fill(),
		Children: []core.Node{
			when(func() bool { return g.phase.Get() == phaseTitle }, menu("title-menu",
				heading(Project.Title()),
				core.Text{Content: "Drag or arrows to fly · Z fire · X bomb", Style: core.Style{Color: mutedColor, FontSize: 12, TextAlign: core.TextAlignCenter}},
				button("start", "Start", g.start),
			)),
			when(func() bool { return g.phase.Get() == phasePlaying && g.paused.Get() }, menu("pause-menu",
				heading("Paused"),
				button("resume", "Resume", func() { g.setPaused(false) }),
				button("restart", "Restart", g.start),
			)),
			when(func() bool { return g.phase.Get() == phaseEnded }, menu("end-menu",
				core.Text{
					TestID: "result",
					ContentFunc: func() string {
						if g.result.Get() == cleared {
							return "Stage clear"
						}
						return "Game over"
					},
					Style: core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold},
				},
				core.Text{TestID: "final-score", ContentFunc: func() string { return fmt.Sprintf("Score %d", g.score.Get()) }, Style: core.Style{Color: mutedColor, FontSize: 16}},
				button("retry", "Play again", g.start),
			)),
		},
	}
}

// menu is a centered panel of items over a scrim that keeps the playfield from hearing taps.
func menu(id string, items ...core.Node) core.Node {
	return core.View{
		TestID: id,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     scrimColor,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					AlignItems:   core.AlignCenter,
					Gap:          14,
					Padding:      core.Symmetric(24, 28),
					CornerRadius: 14,
					Background:   panelColor,
				},
				Children: items,
			},
		},
	}
}

func heading(s string) core.Node {
	return core.Text{Content: s, Style: core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold}}
}

func hudText(id string, size core.Points, content func() string) core.Node {
	return core.Text{TestID: id, ContentFunc: content, Style: core.Style{Color: textColor, FontSize: size, FontWeight: core.FontWeightBold}}
}

// button is a native button labeled label that calls onPress.
func button(id, label string, onPress func()) core.Node {
	return base.Button{
		TestID:  id,
		Label:   label,
		OnPress: func(core.PressEvent) { onPress() },
		Style: core.Style{
			Background:   accent,
			CornerRadius: 10,
			Padding:      core.Symmetric(10, 22),
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
		},
		Children: []core.Node{
			core.Text{Content: label, Style: core.Style{Color: core.RGB(0xffffff), FontSize: 15, FontWeight: core.FontWeightBold}},
		},
	}
}

// when shows n while cond is true.
func when(cond func() bool, n core.Node) core.Node {
	return core.Show{When: cond, Then: func(*core.Scope) core.Node { return n }}
}

package main

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// The native controls' colors.
var (
	colorPanel     = core.RGBA(0x111a2e, 0.92)
	colorText      = core.RGB(0xf8fafc)
	colorTextDim   = core.RGB(0x94a3b8)
	colorButton    = core.RGB(0x1e3a5f)
	colorPrimary   = core.RGB(0x0891b2)
	colorHighlight = core.RGB(0x67e8f9)
	colorStar      = core.RGB(0xfde047)
)

// view is the game's whole tree: the level select or the level being played.
func (g *sparkGame) view() core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: colorBackground},
		Children: []core.Node{
			core.Show{
				When: func() bool { return g.screen.Get() == screenPlay },
				Then: func(*core.Scope) core.Node { return g.playView() },
				Else: func(*core.Scope) core.Node { return g.levelsView() },
			},
		},
	}
}

// starText returns n of three stars as filled and empty star characters.
func starText(n int) string {
	n = min(max(n, 0), 3)
	return strings.Repeat("★", n) + strings.Repeat("☆", 3-n)
}

// levelsView is the level select: a grid of the levels, each with the stars it has earned, the locked ones disabled.
func (g *sparkGame) levelsView() core.Node {
	var rows []core.Node
	for start := 0; start < len(g.levels); start += levelColumns {
		var cells []core.Node
		for i := start; i < min(start+levelColumns, len(g.levels)); i++ {
			cells = append(cells, g.levelButton(i))
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, Gap: 12, JustifyContent: core.JustifyCenter}, Children: cells})
	}
	return core.View{
		Style: core.Style{
			FlexGrow:       1,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            12,
			Padding:        core.All(24),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: append([]core.Node{
			core.Text{
				Content: Project.Title(),
				Role:    core.RoleHeader,
				Style:   core.Style{Color: colorText, FontSize: 32, FontWeight: core.FontWeightBold, Margin: core.Edges{Bottom: 12}},
			},
		}, append(rows, core.Show{
			When: g.saveFail.Get,
			Then: func(*core.Scope) core.Node {
				return core.Text{Content: "Progress could not be saved", Style: core.Style{Color: colorTextDim, FontSize: 13}}
			},
		})...),
	}
}

// levelButton is the level select's button for the level at index i.
func (g *sparkGame) levelButton(i int) core.Node {
	l := g.levels[i]
	stars := func() int { _ = g.saved.Get(); return g.progress.Stars[l.ID] }
	locked := func() bool { _ = g.saved.Get(); return !g.progress.Unlocked(g.levels, i) }
	return base.Button{
		TestID: fmt.Sprintf("level-%d", i+1),
		LabelFunc: func() string {
			if locked() {
				return fmt.Sprintf("Level %d, %s, locked", i+1, l.Name)
			}
			return fmt.Sprintf("Level %d, %s, %d of 3 stars", i+1, l.Name, stars())
		},
		Disabled: locked,
		Selected: func() bool { return g.cursor.Get() == i },
		OnPress:  func(core.PressEvent) { g.startLevel(i) },
		Style: core.Style{
			Width:        core.Pt(120),
			Padding:      core.Symmetric(12, 8),
			Gap:          4,
			AlignItems:   core.AlignCenter,
			Background:   colorButton,
			CornerRadius: 12,
			BorderWidth:  2,
			BorderColor:  colorButton,
			Selected:     &core.Style{BorderColor: colorHighlight},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45)},
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
		},
		Children: []core.Node{
			core.Text{Content: fmt.Sprint(i + 1), Style: core.Style{Color: colorText, FontSize: 24, FontWeight: core.FontWeightBold}},
			core.Text{Content: l.Name, Style: core.Style{Color: colorTextDim, FontSize: 12, NumberOfLines: 1}},
			core.Text{
				TestID: fmt.Sprintf("level-%d-stars", i+1),
				ContentFunc: func() string {
					if locked() {
						return "Locked"
					}
					return starText(stars())
				},
				Style: core.Style{Color: colorStar, FontSize: 16},
			},
		},
	}
}

// playView is a level being played: the board on a Canvas that hears taps and swipes, the level's counters above it, and its controls below, which the result replaces once the level ends.
func (g *sparkGame) playView() core.Node {
	surface := core.View{
		TestID: "board",
		Style:  core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)},
		OnLayout: func(b core.Bounds) {
			g.viewSize = b.Size
		},
		// The board is no Tab stop: keys and controllers play through the actions. Keyboard and assistive presses land at the view's origin, which is not a place the player chose, so tap ignores them.
		NoFocus: true,
		OnPress: func(ev core.PressEvent) {
			if !ev.Keyboard {
				g.tap(ev.Position)
			}
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "board-canvas",
				LabelFunc: func() string {
					h := g.hud.Get()
					return fmt.Sprintf("Board: %d of %d lamps lit, %d moves left", h.Lit, h.Lamps, h.Limit-h.Moves)
				},
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw:  g.draw,
			},
		},
	}
	g.gestures.Attach(&surface)
	return core.View{
		Style: core.Style{FlexGrow: 1},
		Children: []core.Node{
			surface,
			g.hudView(),
			// The result replaces the controls, so its buttons are the only ones to reach.
			core.Show{
				When: func() bool { return g.hud.Get().ResultShowing },
				Then: func(*core.Scope) core.Node { return g.resultView() },
				Else: func(*core.Scope) core.Node { return g.controlsView() },
			},
		},
	}
}

// hudView shows the level's name, the moves used of those allowed and the lamps lit.
func (g *sparkGame) hudView() core.Node {
	counter := func(id string, text func() string) core.Node {
		return core.Text{TestID: id, ContentFunc: text, Style: core.Style{Color: colorText, FontSize: 16, FontWeight: core.FontWeightSemibold}}
	}
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Right:      core.Pt(0),
			Padding:    core.All(16),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
			Gap:        6,
			AlignItems: core.AlignCenter,
		},
		Children: []core.Node{
			core.Text{
				TestID:      "level-name",
				ContentFunc: func() string { _ = g.hud.Get(); return fmt.Sprintf("%d. %s", g.level+1, g.levels[g.level].Name) },
				Style:       core.Style{Color: colorText, FontSize: 20, FontWeight: core.FontWeightBold},
			},
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 20},
				Children: []core.Node{
					counter("moves", func() string { h := g.hud.Get(); return fmt.Sprintf("Moves %d/%d", h.Moves, h.Limit) }),
					counter("lamps", func() string { h := g.hud.Get(); return fmt.Sprintf("Lamps %d/%d", h.Lit, h.Lamps) }),
				},
			},
		},
	}
}

// button is one of the game's native buttons.
func button(label string, primary bool, disabled func() bool, onPress func()) core.Node {
	bg := colorButton
	if primary {
		bg = colorPrimary
	}
	return base.Button{
		Label:    label,
		Disabled: disabled,
		OnPress:  func(core.PressEvent) { onPress() },
		Style: core.Style{
			Background:   bg,
			CornerRadius: 10,
			Padding:      core.Symmetric(10, 16),
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
			Disabled:     &core.Style{Opacity: core.Alpha(0.4)},
		},
		Children: []core.Node{
			core.Text{Content: label, Style: core.Style{Color: colorText, FontSize: 15, FontWeight: core.FontWeightBold}},
		},
	}
}

// controlsView is the row of buttons under the board.
func (g *sparkGame) controlsView() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Direction:      core.Row,
			JustifyContent: core.JustifyCenter,
			Gap:            10,
			Padding:        core.All(16),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			button("Undo", false, func() bool { return !g.hud.Get().CanUndo }, g.undo),
			button("Redo", false, func() bool { return !g.hud.Get().CanRedo }, g.redo),
			button("Restart", false, nil, g.restart),
			button("Levels", false, nil, g.showLevels),
		},
	}
}

// resultView is the card over the board once the level is won or lost, with what to do next.
func (g *sparkGame) resultView() core.Node {
	h := g.hud.Get()
	title, detail := "Level clear", fmt.Sprintf("%d moves", h.Moves)
	var actions []core.Node
	switch {
	case h.Status == Won:
		next := "Next"
		if g.level+1 >= len(g.levels) {
			next = "Levels"
		}
		actions = []core.Node{button("Replay", false, nil, g.restart), button(next, true, nil, g.next)}
	case g.board().Fallen:
		title, detail = "Lost in the void", "The spark slid off into the dark"
		actions = []core.Node{button("Undo", false, nil, g.undo), button("Retry", true, nil, g.restart)}
	default:
		title, detail = "Out of moves", fmt.Sprintf("%d of %d lamps lit", h.Lit, h.Lamps)
		actions = []core.Node{button("Undo", false, nil, g.undo), button("Retry", true, nil, g.restart)}
	}
	children := []core.Node{
		core.Text{TestID: "result-title", Content: title, Role: core.RoleHeader, Style: core.Style{Color: colorText, FontSize: 26, FontWeight: core.FontWeightBold}},
	}
	if h.Status == Won {
		children = append(children, core.Text{TestID: "result-stars", Content: starText(h.Stars), Label: fmt.Sprintf("%d of 3 stars", h.Stars), Style: core.Style{Color: colorStar, FontSize: 34}})
	}
	children = append(children,
		core.Text{Content: detail, Style: core.Style{Color: colorTextDim, FontSize: 14}},
		core.View{Style: core.Style{Direction: core.Row, Gap: 12, Margin: core.Edges{Top: 8}}, Children: actions},
	)
	return core.View{
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     core.RGBA(0x000000, 0.45),
		},
		Children: []core.Node{
			core.View{
				Role:     core.RoleDialog,
				Label:    title,
				Style:    core.Style{Background: colorPanel, CornerRadius: 16, Padding: core.All(24), Gap: 8, AlignItems: core.AlignCenter},
				Children: children,
			},
		},
	}
}

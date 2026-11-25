package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	feltColor   = core.RGB(0x0b1220)
	textColor   = core.RGB(0xf8fafc)
	dimColor    = core.RGB(0x94a3b8)
	panelColor  = core.RGBA(0x0f172a, 0.92)
	buttonColor = core.RGB(0x0369a1)
)

// view is the game's whole screen: the table, the counts along the top, the buttons along the bottom and, once a game is over, its result. The board hears the touches and the mouse of the view holding the Canvas, so cards drag with either.
func (g *cardGame) view() core.Node {
	table := core.View{
		TestID: "table",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, Background: feltColor},
		Children: []core.Node{
			core.Canvas{
				TestID: "table-canvas",
				Label:  "Card table",
				Style:  fill(),
				Draw:   g.draw,
			},
			g.topBar(),
			g.bottomBar(),
			core.Show{
				When: func() bool { _ = g.state.Get(); return g.table.result != playing },
				Then: func(*core.Scope) core.Node { return g.resultPanel() },
			},
		},
	}
	g.table.board.Attach(&table)
	return table
}

// topBar shows the deal, the piles and the record; touches pass through it to the table.
func (g *cardGame) topBar() core.Node {
	label := core.Style{Color: textColor, FontSize: 15, FontWeight: core.FontWeightBold}
	dim := core.Style{Color: dimColor, FontSize: 13}
	return bar(core.Style{Top: core.Pt(0)},
		core.Text{TestID: "deal", ContentFunc: func() string { _ = g.state.Get(); return fmt.Sprintf("Deal %d", g.table.deal) }, Style: label},
		core.Text{TestID: "piles", ContentFunc: func() string {
			_ = g.state.Get()
			return fmt.Sprintf("Draw pile %d · Redeals %d", g.table.deck.Len(), g.table.redeals)
		}, Style: dim},
		core.Text{TestID: "record", ContentFunc: func() string {
			_ = g.state.Get()
			return fmt.Sprintf("Won %d · Lost %d", g.records.Wins, g.records.Losses)
		}, Style: dim},
	)
}

// bottomBar holds the buttons that draw and deal again, for touch screens without a keyboard or gamepad.
func (g *cardGame) bottomBar() core.Node {
	return bar(core.Style{Bottom: core.Pt(0)},
		buttonWith("draw-text", "Draw", func() { g.table.draw() }, func() string {
			_ = g.state.Get()
			if g.table.canRedeal() {
				return fmt.Sprintf("Redeal (%d)", g.table.redeals)
			}
			return "Draw"
		}),
		button("New deal", g.newDeal),
	)
}

// resultPanel says how the game ended and starts the next deal.
func (g *cardGame) resultPanel() core.Node {
	heading := "No moves left"
	if g.table.result == won {
		heading = "Every ladder climbed"
	}
	return core.View{
		TestID: "result",
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     core.RGBA(0x020617, 0.5),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Background: panelColor, CornerRadius: 14, Padding: core.Symmetric(20, 28), AlignItems: core.AlignStretch, Gap: 12},
				Children: []core.Node{
					core.Text{TestID: "result-heading", Content: heading, Style: core.Style{Color: textColor, FontSize: 22, FontWeight: core.FontWeightBold}},
					button("Next deal", g.newDeal),
				},
			},
		},
	}
}

// bar is a row of children across the screen at the edge pos sets, inside the safe area; touches between the children pass through to the table.
func bar(pos core.Style, children ...core.Node) core.Node {
	style := core.Style{
		Position:       core.Absolute,
		Left:           core.Pt(0),
		Right:          core.Pt(0),
		Direction:      core.Row,
		JustifyContent: core.JustifySpaceBetween,
		AlignItems:     core.AlignCenter,
		Gap:            8,
		Padding:        core.Symmetric(10, 14),
		PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		Top:            pos.Top,
		Bottom:         pos.Bottom,
	}
	return core.View{PointerEvents: core.PointerEventsBoxNone, Style: style, Children: children}
}

// button is a native button called label that runs onPress.
func button(label string, onPress func()) core.Node {
	return buttonWith("", label, onPress, func() string { return label })
}

// buttonWith is a native button called label that runs onPress and shows the text text returns, in a view called id when it is set.
func buttonWith(id, label string, onPress func(), text func() string) core.Node {
	return base.Button{
		Label:   label,
		OnPress: func(core.PressEvent) { onPress() },
		Style: core.Style{
			Background:   buttonColor,
			CornerRadius: 10,
			Padding:      core.Symmetric(8, 16),
			AlignItems:   core.AlignCenter,
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
		},
		Children: []core.Node{
			core.Text{TestID: id, ContentFunc: text, Style: core.Style{Color: textColor, FontSize: 15, FontWeight: core.FontWeightBold}},
		},
	}
}

// fill is the style of a child that covers its whole parent.
func fill() core.Style {
	return core.Style{
		Position: core.Absolute,
		Top:      core.Pt(0),
		Right:    core.Pt(0),
		Bottom:   core.Pt(0),
		Left:     core.Pt(0),
	}
}

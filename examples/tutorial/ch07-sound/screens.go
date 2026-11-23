package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	textColor   = core.RGB(0xf8fafc)
	dimColor    = core.RGB(0x94a3b8)
	panelColor  = core.RGBA(0x0f172a, 0.88)
	buttonColor = core.RGB(0x0284c7)
)

// view is the game's whole screen: the playfield, and over it the native views of the current phase.
func (s *starCatcher) view() core.Node {
	field := core.View{
		TestID: "game-view",
		Style:  fill(),
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style:  fill(),
				Draw:   s.draw,
			},
		},
	}
	s.actions.Attach(&field)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: barColor},
		Children: []core.Node{
			field,
			s.when(phasePlaying, s.hud),
			s.when(phaseTitle, s.title),
			s.when(phasePaused, s.paused),
			s.when(phaseOver, s.gameOver),
		},
	}
}

// when shows the views screen makes while the game is in phase p.
func (s *starCatcher) when(p phase, screen func() core.Node) core.Node {
	return core.Show{
		When: func() bool { return s.phase.Get() == p },
		Then: func(*core.Scope) core.Node { return screen() },
	}
}

// hud is the score, the lives and a pause button along the top; touches elsewhere pass through to the playfield.
func (s *starCatcher) hud() core.Node {
	label := core.Style{Color: textColor, FontSize: 16, FontWeight: core.FontWeightBold}
	return core.View{
		TestID:        "hud",
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Direction:      core.Row,
			JustifyContent: core.JustifySpaceBetween,
			AlignItems:     core.AlignCenter,
			Padding:        core.All(12),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			core.Text{TestID: "score", ContentFunc: func() string { return fmt.Sprintf("Score: %d", s.score.Get()) }, Style: label},
			button("Pause", s.togglePause),
			core.Text{TestID: "lives", ContentFunc: func() string { return fmt.Sprintf("Lives: %d", s.lives.Get()) }, Style: label},
		},
	}
}

func (s *starCatcher) title() core.Node {
	return panel("title",
		heading("Star Catcher"),
		note("Catch the stars, dodge the rocks"),
		button("Start", s.start),
		s.soundButton(),
	)
}

func (s *starCatcher) paused() core.Node {
	return panel("paused",
		heading("Paused"),
		button("Resume", s.togglePause),
		s.soundButton(),
		button("Quit", s.quit),
	)
}

func (s *starCatcher) gameOver() core.Node {
	return panel("game-over",
		heading("Game over"),
		core.Text{TestID: "final-score", ContentFunc: func() string { return caught(s.score.Get()) }, Style: core.Style{Color: textColor, FontSize: 16}},
		button("Play again", s.start),
		button("Title", s.quit),
	)
}

// panel centers a card of children over the playfield, and takes the presses that land on it.
func panel(id string, children ...core.Node) core.Node {
	return core.View{
		TestID: id,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Background:     core.RGBA(0x020617, 0.45),
		},
		Children: []core.Node{
			core.View{
				Style:    core.Style{Background: panelColor, CornerRadius: 14, Padding: core.Symmetric(20, 28), AlignItems: core.AlignStretch, Gap: 10},
				Children: children,
			},
		},
	}
}

func heading(text string) core.Node {
	return core.Text{Content: text, Style: core.Style{Color: textColor, FontSize: 26, FontWeight: core.FontWeightBold}}
}

func note(text string) core.Node {
	return core.Text{Content: text, Style: core.Style{Color: dimColor, FontSize: 13}}
}

// soundButton switches the sound on and off, and says which it is.
func (s *starCatcher) soundButton() core.Node {
	return buttonWith("Sound", s.toggleSound, func() string {
		if s.muted() {
			return "Sound: off"
		}
		return "Sound: on"
	})
}

// button is a native button called label that runs onPress.
func button(label string, onPress func()) core.Node {
	return buttonWith(label, onPress, func() string { return label })
}

// buttonWith is a native button called label that runs onPress and shows the text text returns.
func buttonWith(label string, onPress func(), text func() string) core.Node {
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
			core.Text{ContentFunc: text, Style: core.Style{Color: textColor, FontSize: 15, FontWeight: core.FontWeightBold}},
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

// caught is the line that tells the player how many stars they caught.
func caught(n int) string {
	if n == 1 {
		return "You caught 1 star"
	}
	return fmt.Sprintf("You caught %d stars", n)
}

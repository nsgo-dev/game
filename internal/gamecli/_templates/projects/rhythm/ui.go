package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// The native views' colors.
var (
	colorPanel     = core.RGBA(0x111a2e, 0.92)
	colorText      = core.RGB(0xf8fafc)
	colorTextDim   = core.RGB(0x94a3b8)
	colorButton    = core.RGB(0x1e2a4a)
	colorHighlight = core.RGB(0x22d3ee)
	colorEnergy    = core.RGB(0x4ade80)
	colorEnergyLow = core.RGB(0xf87171)
)

// lowEnergy is the energy under which the bar turns red.
const lowEnergy = 25

// view is the game's whole tree: the screen showing.
func (g *rhythmGame) view() core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: colorBackground},
		Children: []core.Node{
			core.Dynamic(func(*core.Scope) core.Node {
				switch g.screen.Get() {
				case screenPlay:
					return g.playView()
				case screenResult:
					return g.resultView()
				case screenCalibrate:
					return g.calibrateView()
				}
				return g.menuView()
			}),
		},
	}
}

// fill is the style of a view that covers its parent.
var fill = core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}

// screenStyle is the style of a screen of centered native views.
var screenStyle = core.Style{
	FlexGrow:       1,
	AlignItems:     core.AlignCenter,
	JustifyContent: core.JustifyCenter,
	Gap:            12,
	Padding:        core.All(24),
	PaddingEnv:     core.EnvAll(core.EnvSafeArea),
}

// title is a screen's heading.
func title(id, text string) core.Node {
	return core.Text{TestID: id, Content: text, Role: core.RoleHeader, Style: core.Style{Color: colorText, FontSize: 30, FontWeight: core.FontWeightBold, Margin: core.Edges{Bottom: 8}}}
}

// menuButton is one item of a list that up and down move through: highlighted while the cursor is on index, and pressed by a tap, a click or confirm.
func (g *rhythmGame) menuButton(id string, index int, label string, detail func() string, disabled func() bool, onPress func()) core.Node {
	children := []core.Node{core.Text{Content: label, Style: core.Style{Color: colorText, FontSize: 17, FontWeight: core.FontWeightSemibold}}}
	if detail != nil {
		children = append(children, core.Text{TestID: id + "-detail", ContentFunc: detail, Style: core.Style{Color: colorTextDim, FontSize: 13}})
	}
	return base.Button{
		TestID: id,
		LabelFunc: func() string {
			if detail == nil {
				return label
			}
			return label + ", " + detail()
		},
		Disabled: disabled,
		Selected: func() bool { return g.cursor.Get() == index },
		OnPress:  func(core.PressEvent) { onPress() },
		Style: core.Style{
			Width:        core.Pt(300),
			Padding:      core.Symmetric(12, 16),
			Gap:          2,
			AlignItems:   core.AlignCenter,
			Background:   colorButton,
			CornerRadius: 12,
			BorderWidth:  2,
			BorderColor:  colorButton,
			Selected:     &core.Style{BorderColor: colorHighlight},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45)},
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
		},
		Children: children,
	}
}

// menuView is the menu: each chart with its best clear, then the two calibrations and the offsets they set, which it reads as it is built, each time it shows.
func (g *rhythmGame) menuView() core.Node {
	children := []core.Node{title("menu-title", Project.Title())}
	for i, it := range g.items {
		if it.calibrate {
			continue
		}
		song := &g.lib.Songs[it.song]
		chart := &song.Charts[it.chart]
		key := chartKey(song, chart)
		children = append(children, g.menuButton(fmt.Sprintf("chart-%s-%s", song.ID, chart.ID), i,
			fmt.Sprintf("%s · %s %d", song.Title, chart.Name, chart.Level),
			func() string {
				_ = g.saved.Get()
				if r, ok := g.records.Best[key]; ok {
					return fmt.Sprintf("Best %s %s", formatScore(r.Score), Rank(r.Score))
				}
				return "Not cleared"
			}, nil, func() { g.pick(i) }))
	}
	for i, it := range g.items {
		if !it.calibrate {
			continue
		}
		id, label := "calibrate-audio", "Calibrate audio"
		if it.kind == rhythm.CalibrateVideo {
			id, label = "calibrate-video", "Calibrate video"
		}
		kind := it.kind
		children = append(children, g.menuButton(id, i, label, nil, func() bool { return !g.canCalibrate(kind) }, func() { g.pick(i) }))
	}
	children = append(children,
		core.Text{
			TestID:  "offsets",
			Content: fmt.Sprintf("Audio offset %+d ms · Video offset %+d ms", g.offsets.Audio.Milliseconds(), g.offsets.Video.Milliseconds()),
			Style:   core.Style{Color: colorTextDim, FontSize: 13, Margin: core.Edges{Top: 8}},
		},
		core.Show{
			When: g.saveFail.Get,
			Then: func(*core.Scope) core.Node {
				return core.Text{Content: "Records could not be saved", Style: core.Style{Color: colorTextDim, FontSize: 13}}
			},
		},
	)
	return core.View{Style: screenStyle, Children: children}
}

// formatScore writes a score with thousands separators.
func formatScore(score int) string {
	s := fmt.Sprint(score)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// playView is a chart being played: the playfield on a Canvas whose lanes hear touches and clicks, the score, combo and energy above it, the latest judgement over the hit line, and the pause menu while paused.
func (g *rhythmGame) playView() core.Node {
	field := core.View{
		TestID:   "field",
		Style:    fill,
		OnLayout: func(b core.Bounds) { g.layoutField(b.Size) },
		OnTouch:  g.touched,
		OnPressDown: func(ev core.PressEvent) {
			g.pressedDown(ev)
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "field-canvas",
				LabelFunc: func() string {
					h := g.hud.Get()
					return fmt.Sprintf("Playfield, %d lanes, combo %d", Lanes, h.Combo)
				},
				Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.frame.Get()
					g.drawField(d, size)
				},
			},
		},
	}
	g.actions.Attach(&field)
	return core.View{
		Style: core.Style{FlexGrow: 1},
		Children: []core.Node{
			field,
			g.hudView(),
			core.Show{
				When: g.paused.Get,
				Then: func(*core.Scope) core.Node { return g.pauseView() },
			},
		},
	}
}

// hudView shows the score, the combo, the energy and the latest judgement, and lets touches through to the lanes.
func (g *rhythmGame) hudView() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style:         core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), PaddingEnv: core.EnvAll(core.EnvSafeArea)},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style:         core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, AlignItems: core.AlignCenter, Padding: core.All(16), Gap: 12},
				Children: []core.Node{
					core.View{
						PointerEvents: core.PointerEventsNone,
						Style:         core.Style{Gap: 6, FlexGrow: 1},
						Children: []core.Node{
							core.Text{
								TestID:      "score",
								ContentFunc: func() string { return formatScore(g.hud.Get().Score) },
								Style:       core.Style{Color: colorText, FontSize: 22, FontWeight: core.FontWeightBold},
							},
							g.energyBar(),
						},
					},
					base.Button{
						TestID:  "pause",
						Label:   "Pause",
						OnPress: func(core.PressEvent) { g.pause() },
						Style:   core.Style{Background: colorButton, CornerRadius: 10, Padding: core.Symmetric(8, 14), Active: &core.Style{Opacity: core.Alpha(0.8)}},
						Children: []core.Node{
							core.Text{Content: "Pause", Style: core.Style{Color: colorText, FontSize: 14, FontWeight: core.FontWeightBold}},
						},
					},
				},
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style:         core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 4},
				Children: []core.Node{
					core.Text{
						TestID: "combo",
						ContentFunc: func() string {
							if c := g.hud.Get().Combo; c >= 2 {
								return fmt.Sprintf("%d combo", c)
							}
							return ""
						},
						Style: core.Style{Color: colorText, FontSize: 28, FontWeight: core.FontWeightBold},
					},
					core.Text{
						TestID: "judgement",
						ContentFunc: func() string {
							h := g.hud.Get()
							switch {
							case h.Waiting:
								return "Get ready"
							case h.Judged == 0:
								return ""
							}
							return judgementText(h.Last, h.Offset)
						},
						Style: core.Style{Color: colorTextDim, FontSize: 18, FontWeight: core.FontWeightSemibold},
					},
				},
			},
		},
	}
}

// judgementText is how the latest judgement shows: its grade, and whether a hit short of perfect was early or late.
func judgementText(g Grade, offset time.Duration) string {
	switch {
	case g == Perfect || g == Miss:
		return g.String()
	case offset < 0:
		return g.String() + " · early"
	}
	return g.String() + " · late"
}

// energyBar shows the energy left, out of MaxEnergy.
func (g *rhythmGame) energyBar() core.Node {
	return core.View{
		TestID:    "energy",
		Role:      core.RoleProgressBar,
		LabelFunc: func() string { return fmt.Sprintf("Energy %d of %d", g.hud.Get().Energy, MaxEnergy) },
		Style:     core.Style{Height: core.Pt(8), Width: core.Pct(100), Background: colorButton, CornerRadius: 4},
		Children: []core.Node{
			core.View{
				StyleFunc: func() core.Style {
					e := g.hud.Get().Energy
					c := colorEnergy
					if e < lowEnergy {
						c = colorEnergyLow
					}
					return core.Style{Height: core.Pct(100), Width: core.Pct(float32(e) * 100 / MaxEnergy), Background: c, CornerRadius: 4}
				},
			},
		},
	}
}

// pauseView is the pause menu over the playfield: resume, restart or quit.
func (g *rhythmGame) pauseView() core.Node {
	return core.View{
		Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), Background: colorPanel, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12},
		Children: []core.Node{
			title("pause-title", "Paused"),
			g.menuButton("resume", pauseResume, "Resume", nil, nil, func() { g.pausePick(pauseResume) }),
			g.menuButton("restart", pauseRestart, "Restart", nil, nil, func() { g.pausePick(pauseRestart) }),
			g.menuButton("quit", pauseQuit, "Quit", nil, nil, func() { g.pausePick(pauseQuit) }),
		},
	}
}

// resultView is the result of the chart just played: cleared or failed, the score and rank, the longest combo and each grade's count, and a retry or the menu.
func (g *rhythmGame) resultView() core.Node {
	r := g.result.Get()
	song := &g.lib.Songs[g.song]
	head, rank := "Cleared", Rank(r.Score)
	if r.Status == Failed {
		head, rank = "Failed", "-"
	}
	counts := make([]string, len(grades))
	for i, gr := range grades {
		counts[i] = fmt.Sprintf("%s %d", gr, r.Counts[gr])
	}
	line := func(id, text string, size core.Points, color core.Color) core.Node {
		return core.Text{TestID: id, Content: text, Style: core.Style{Color: color, FontSize: size, FontWeight: core.FontWeightSemibold}}
	}
	children := []core.Node{
		title("result-title", head),
		line("result-chart", fmt.Sprintf("%s · %s", song.Title, song.Charts[g.chart].Name), 16, colorTextDim),
		line("result-score", formatScore(r.Score), 34, colorText),
		line("result-rank", "Rank "+rank, 20, colorHighlight),
		line("result-combo", fmt.Sprintf("Max combo %d", r.MaxCombo), 16, colorText),
		line("result-counts", strings.Join(counts, " · "), 14, colorTextDim),
	}
	if r.NewBest {
		children = append(children, line("result-best", "New best", 16, colorEnergy))
	}
	children = append(children,
		g.menuButton("retry", resultRetry, "Retry", nil, nil, func() { g.resultPick(resultRetry) }),
		g.menuButton("menu", resultMenu, "Menu", nil, nil, func() { g.resultPick(resultMenu) }),
	)
	return core.View{Style: screenStyle, Children: children}
}

// calibrateView is a calibration: rhythm's calibration screen on a Canvas, whose taps are taken as a press goes down, above a button back to the menu.
func (g *rhythmGame) calibrateView() core.Node {
	s := g.calScreen
	return core.View{
		Style: core.Style{FlexGrow: 1, PaddingEnv: core.EnvAll(core.EnvSafeArea)},
		Children: []core.Node{
			core.View{
				TestID:      "calibration",
				Style:       core.Style{FlexGrow: 1},
				OnPressDown: func(ev core.PressEvent) { s.OnPressDown(ev) },
				OnPress:     func(ev core.PressEvent) { s.OnPress(ev) },
				Children: []core.Node{
					core.Canvas{
						TestID: "calibration-canvas",
						Label:  "Calibration",
						Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw: func(d *core.Drawing, size core.Size) {
							_ = g.frame.Get()
							d.SetFillColor(colorBackground).FillRect(0, 0, float32(size.Width), float32(size.Height))
							s.Draw(d, core.Bounds{Size: size})
						},
					},
				},
			},
			base.Button{
				TestID:  "calibration-back",
				Label:   "Back",
				OnPress: func(core.PressEvent) { g.showMenu() },
				Style:   core.Style{AlignSelf: core.AlignCenter, Margin: core.Edges{Bottom: 16}, Background: colorButton, CornerRadius: 10, Padding: core.Symmetric(10, 20), Active: &core.Style{Opacity: core.Alpha(0.8)}},
				Children: []core.Node{
					core.Text{Content: "Back", Style: core.Style{Color: colorText, FontSize: 14, FontWeight: core.FontWeightBold}},
				},
			},
		},
	}
}

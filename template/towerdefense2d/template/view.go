package main

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// HUD colors.
var (
	colorBackground = core.RGB(0x14201a)
	colorPanel      = core.RGBA(0x0b1310, 0.92)
	colorText       = core.RGB(0xf1f5f9)
	colorMuted      = core.RGB(0x94a3b8)
	colorButton     = core.RGB(0x1f3b2d)
	colorAccent     = core.RGB(0x15803d)
	colorWarn       = core.RGB(0xfacc15)
)

// view returns the game's view: a status bar, the battlefield canvas with the end-of-game banner over it, and the toolbar.
func (g *Game) view() core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: colorBackground},
		Children: []core.Node{
			g.statusBar(),
			core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(120)},
				Children: []core.Node{
					core.Canvas{
						TestID: "battlefield",
						Label:  "Battlefield",
						Style:  fill(),
						OnPress: func(ev core.PressEvent) {
							if !ev.Keyboard {
								g.Tap(ev.Position)
								g.syncHUD()
							}
						},
						Draw: func(d *core.Drawing, size core.Size) {
							_ = g.frame.Get()
							g.canvasSize = size
							g.Viewport.Draw(d, size, g.draw)
						},
					},
					core.Show{
						When: func() bool { return g.hud.Get().Outcome != Playing },
						Then: func(*core.Scope) core.Node { return g.banner() },
					},
				},
			},
			g.toolbar(),
		},
	}
}

// statusBar shows the money, lives, wave and the latest message.
func (g *Game) statusBar() core.Node {
	hud := func() HUD { return g.hud.Get() }
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			Wrap:       core.Wrap,
			AlignItems: core.AlignCenter,
			Gap:        16,
			Padding:    core.Symmetric(8, 14),
			PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea},
			Background: colorPanel,
		},
		Children: []core.Node{
			label("hud-level", func() string { return hud().Level }, colorMuted, 14),
			label("hud-money", func() string { return fmt.Sprintf("Money %d", hud().Money) }, colorWarn, 18),
			label("hud-lives", func() string { return fmt.Sprintf("Lives %d", hud().Lives) }, colorText, 18),
			label("hud-wave", func() string { return fmt.Sprintf("Wave %d/%d", hud().Wave, hud().Waves) }, colorText, 18),
			label("hud-message", func() string {
				h := hud()
				switch {
				case h.Paused:
					return "Paused"
				case h.Message != "":
					return h.Message
				case h.HasTower:
					return fmt.Sprintf("%s, level %d", h.Tower, h.TowerLevel)
				}
				return ""
			}, colorMuted, 14),
		},
	}
}

// toolbar holds a button for each tower kind and the upgrade, sell, wave, speed and pause buttons.
func (g *Game) toolbar() core.Node {
	hud := func() HUD { return g.hud.Get() }
	var buttons []core.Node
	for i := range g.World.Data.Towers.Towers {
		def := &g.World.Data.Towers.Towers[i]
		buttons = append(buttons, button(buttonSpec{
			id:       "tower-" + def.Name,
			label:    func() string { return fmt.Sprintf("%s %d", def.Label, def.Cost) },
			press:    g.act(func() { g.SelectKind(i) }),
			selected: func() bool { return hud().Kind == i },
			swatch:   mustColor(def.Color),
		}))
	}
	buttons = append(buttons,
		button(buttonSpec{
			id: "upgrade",
			label: func() string {
				if h := hud(); h.HasTower && h.UpgradeCost > 0 {
					return fmt.Sprintf("Upgrade %d", h.UpgradeCost)
				}
				return "Upgrade"
			},
			press:    g.act(g.Upgrade),
			disabled: func() bool { h := hud(); return !h.HasTower || h.UpgradeCost == 0 || h.Outcome != Playing || h.Paused },
		}),
		button(buttonSpec{
			id: "sell",
			label: func() string {
				if h := hud(); h.HasTower {
					return fmt.Sprintf("Sell %d", h.SellValue)
				}
				return "Sell"
			},
			press:    g.act(g.Sell),
			disabled: func() bool { h := hud(); return !h.HasTower || h.Outcome != Playing || h.Paused },
		}),
		button(buttonSpec{
			id:       "next-wave",
			label:    func() string { return "Next wave" },
			press:    g.act(g.CallWave),
			disabled: func() bool { h := hud(); return !h.CanCallWave || h.Paused },
			primary:  true,
		}),
		button(buttonSpec{
			id:    "speed",
			label: func() string { return fmt.Sprintf("Speed %dx", hud().Speed) },
			press: g.act(g.CycleSpeed),
		}),
		button(buttonSpec{
			id: "pause",
			label: func() string {
				if hud().Paused {
					return "Resume"
				}
				return "Pause"
			},
			press:    g.act(g.TogglePause),
			disabled: func() bool { return hud().Outcome != Playing },
		}),
	)
	return core.View{
		Style: core.Style{
			Direction:      core.Row,
			Wrap:           core.Wrap,
			JustifyContent: core.JustifyCenter,
			Gap:            8,
			Padding:        core.Symmetric(10, 12),
			PaddingEnv:     core.EnvEdges{Bottom: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea},
			Background:     colorPanel,
		},
		Children: buttons,
	}
}

// banner covers the battlefield once the game is won or lost, with a button that plays again.
func (g *Game) banner() core.Node {
	return core.View{
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			Left:           core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            14,
			Background:     core.RGBA(0x020617, 0.7),
		},
		Children: []core.Node{
			label("outcome", func() string {
				if g.hud.Get().Outcome == Won {
					return "The gate holds!"
				}
				return "The gate has fallen"
			}, colorText, 30),
			label("outcome-detail", func() string {
				h := g.hud.Get()
				if h.Outcome == Won {
					return fmt.Sprintf("All %d waves stopped with %d lives left", h.Waves, h.Lives)
				}
				return fmt.Sprintf("Overrun on wave %d of %d", h.Wave, h.Waves)
			}, colorMuted, 16),
			button(buttonSpec{
				id:      "play-again",
				label:   func() string { return "Play again" },
				press:   g.act(g.Restart),
				primary: true,
			}),
		},
	}
}

// buttonSpec describes one HUD button.
type buttonSpec struct {
	id       string
	label    func() string
	press    func(core.PressEvent)
	disabled func() bool
	selected func() bool
	primary  bool
	// swatch, when not zero, is a dot of the tower's color before the label.
	swatch core.Color
}

func button(s buttonSpec) core.Node {
	bg := colorButton
	if s.primary {
		bg = colorAccent
	}
	children := []core.Node{}
	if s.swatch != 0 {
		children = append(children, core.View{Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), CornerRadius: 5, Background: s.swatch}})
	}
	children = append(children, core.Text{
		ContentFunc: s.label,
		Style:       core.Style{Color: colorText, FontSize: 14, FontWeight: core.FontWeightBold},
	})
	return base.Button{
		TestID:    s.id,
		LabelFunc: s.label,
		OnPress:   s.press,
		Disabled:  s.disabled,
		Selected:  s.selected,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          6,
			Background:   bg,
			CornerRadius: 8,
			BorderWidth:  2,
			BorderColor:  bg,
			Padding:      core.Symmetric(8, 12),
			Active:       &core.Style{Opacity: core.Alpha(0.8)},
			Selected:     &core.Style{BorderColor: colorWarn},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45)},
		},
		Children: children,
	}
}

func label(id string, text func() string, color core.Color, size core.Points) core.Node {
	return core.Text{
		TestID:      id,
		ContentFunc: text,
		Style:       core.Style{Color: color, FontSize: size, FontWeight: core.FontWeightBold},
	}
}

func fill() core.Style {
	return core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}
}

// mustColor returns the CSS color s, which Validate accepted.
func mustColor(s string) core.Color {
	c, _ := core.ParseColor(s)
	return c
}

// draw draws the battlefield in game points: the map, the range of the tower or build under the cursor, the towers, the enemies, the projectiles and the cursor.
func (g *Game) draw(d *core.Drawing, _ core.Size) {
	w := g.World
	mw, mh := w.Size()
	d.SetFillColor(colorBackground).FillRect(0, 0, mw, mh)
	w.Map.Draw(d, nil)

	tile := w.Map.TileWidth
	cursor := w.cellCenter(g.Cursor)
	if t, ok := w.TowerAt(g.Cursor); ok {
		circle(d, t.X, t.Y, float32(t.Stats.Value(statRange)))
		d.SetFillColor(core.RGBA(0xffffff, 0.08)).Fill()
		d.SetStrokeColor(core.RGBA(0xffffff, 0.5)).SetLineWidth(1).Stroke()
	} else if w.Buildable(g.Cursor) && w.Outcome == Playing {
		def := g.KindDef()
		reach := float32(0)
		for _, s := range def.Stats {
			if s.Name == statRange {
				reach = float32(s.Base)
			}
		}
		circle(d, cursor.X, cursor.Y, reach)
		d.SetFillColor(mustColor(def.Color).Faded(0.12)).Fill()
		d.SetAlpha(0.5)
		g.drawTower(d, def, cursor.X, cursor.Y, -math.Pi/2, 0)
		d.SetAlpha(1)
	}

	for _, t := range w.Towers {
		g.drawTower(d, t.Def, t.X, t.Y, t.Aim, t.Level)
	}
	for _, e := range w.Enemies {
		r := e.Def.Radius
		circle(d, e.X, e.Y, r)
		d.SetFillColor(mustColor(e.Def.Color)).Fill()
		if e.Chilled() {
			d.SetStrokeColor(core.RGB(0xa5f3fc)).SetLineWidth(2).Stroke()
		}
		circle(d, e.X+r*0.35, e.Y-r*0.2, r*0.22)
		d.SetFillColor(core.RGB(0x111827)).Fill()
		if full := e.Stats.Value(statHealth); e.Health < full {
			d.SetFillColor(core.RGBA(0x000000, 0.6)).FillRect(e.X-r, e.Y-r-6, 2*r, 3)
			d.SetFillColor(core.RGB(0x4ade80)).FillRect(e.X-r, e.Y-r-6, 2*r*float32(max(0, e.Health/full)), 3)
		}
	}
	for _, t := range w.Towers {
		t.Shots.Draw(d)
	}

	color := core.RGB(0xffffff)
	if w.Outcome == Playing && !w.Buildable(g.Cursor) {
		if _, ok := w.TowerAt(g.Cursor); !ok {
			color = core.RGB(0xf87171)
		}
	}
	x, y := float32(g.Cursor.Col)*tile, float32(g.Cursor.Row)*w.Map.TileHeight
	d.SetStrokeColor(color).SetLineWidth(2).StrokeRect(x+1, y+1, tile-2, w.Map.TileHeight-2)
}

// drawTower draws a tower of def at (x, y) with its barrel pointing at aim and a pip for each upgrade.
func (g *Game) drawTower(d *core.Drawing, def *TowerDef, x, y, aim float32, level int) {
	tile := g.World.Map.TileWidth
	half := tile*0.5 - 3
	d.SetFillColor(core.RGB(0x334155)).BeginPath().RoundRect(x-half, y-half, 2*half, 2*half, 5).Fill()
	c := mustColor(def.Color)
	circle(d, x, y, half*0.62)
	d.SetFillColor(c).Fill()
	s, co := math.Sincos(float64(aim))
	d.SetStrokeColor(c).SetLineWidth(4).BeginPath().MoveTo(x, y).LineTo(x+float32(co)*half, y+float32(s)*half).Stroke()
	for i := range level {
		circle(d, x-half+4+float32(i)*6, y+half-4, 2)
		d.SetFillColor(colorWarn).Fill()
	}
}

// circle begins a path holding a circle of radius r around (x, y).
func circle(d *core.Drawing, x, y, r float32) {
	d.BeginPath().Arc(x, y, r, 0, 2*math.Pi, false)
}

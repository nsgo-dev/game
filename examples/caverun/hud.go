package main

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

func (g *caveRun) render(cx *core.Scope) core.Node {
	gameView := core.View{
		TestID: "game-view",
		Ref:    &g.gameRef,
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(0x0b0d17),
		},
		Focusable: func() bool { return true },
		OnLayout: func(b core.Bounds) {
			g.viewSize = b.Size
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Cave Run game viewport",
				Style: core.Style{
					Width:  core.Pct(100),
					Height: core.Pct(100),
				},
				Draw: g.drawCanvas,
			},
		},
	}
	g.actions.Attach(&gameView)

	return core.View{
		Style: core.Style{
			Position:   core.Relative,
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(0x0b0d17),
		},
		Children: []core.Node{
			gameView,
			g.renderHUDOverlay(cx),
			g.renderTitleOverlay(cx),
			g.renderPauseOverlay(cx),
			g.renderEndOverlay(cx),
			g.renderSettingsOverlay(cx),
		},
	}
}

func (g *caveRun) safeInsets() core.Edges {
	env := g.env
	safe := screen.SafeRectForEnv(env)
	win := env.Window()

	var top, left, right, bottom core.Points = 10, 12, 12, 10
	if win.Width > 0 && win.Height > 0 && safe.Size.Width > 0 && safe.Size.Height > 0 {
		top = safe.Origin.Y + 10
		left = safe.Origin.X + 12
		if r := win.Width - (safe.Origin.X + safe.Size.Width); r > 0 {
			right = r + 12
		}
		if b := win.Height - (safe.Origin.Y + safe.Size.Height); b > 0 {
			bottom = b + 10
		}
	}
	return core.Edges{Top: top, Right: right, Bottom: bottom, Left: left}
}

func (g *caveRun) renderHUDOverlay(cx *core.Scope) core.Node {
	return core.Show{
		When: func() bool { return g.phase.Get() == phasePlaying },
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID:        "hud-layer",
				PointerEvents: core.PointerEventsBoxNone,
				StyleFunc: func() core.Style {
					return core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(0),
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						Padding:        g.safeInsets(),
						JustifyContent: core.JustifySpaceBetween,
					}
				},
				Children: []core.Node{
					g.renderTopBar(cx),
					core.Show{
						When: func() bool { return !g.paused.Get() },
						Then: func(cx *core.Scope) core.Node {
							return g.renderTouchControls(cx)
						},
					},
				},
			}
		},
	}
}

func (g *caveRun) renderTopBar(cx *core.Scope) core.Node {
	pillStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		Gap:          10,
		Padding:      core.Symmetric(5, 10),
		CornerRadius: 8,
		Background:   core.RGBA(0x0f1324, 0.82),
		BorderWidth:  1,
		BorderColor:  core.RGBA(0x64748b, 0.35),
	}
	hudText := core.Style{
		Color:      core.RGB(0xf1f5f9),
		FontSize:   13,
		FontWeight: core.FontWeightBold,
		FontFamily: core.FontMonospace,
	}

	return core.View{
		TestID:        "hud-top",
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Width:          core.Pct(100),
		},
		Children: []core.Node{
			// Left status pill: hearts & gem count.
			core.View{
				Style: pillStyle,
				Children: []core.Node{
					core.Text{
						TestID: "hud-hearts",
						ContentFunc: func() string {
							hp := g.hearts.Get()
							if hp < 0 {
								hp = 0
							}
							if hp > maxHearts {
								hp = maxHearts
							}
							return strings.Repeat("♥", hp) + strings.Repeat("♡", maxHearts-hp)
						},
						Style: core.Style{
							Color:      core.RGB(0xf43f5e),
							FontSize:   15,
							FontWeight: core.FontWeightBold,
						},
					},
					core.Text{
						TestID: "hud-gems",
						ContentFunc: func() string {
							return fmt.Sprintf("GEMS %d/%d", g.gems.Get(), g.totalGems.Get())
						},
						Style: hudText.Merge(core.Style{Color: core.RGB(0x38d9d9)}),
					},
				},
			},
			// Center score pill, with the sprite count while the stress swarm is on.
			core.View{
				Style: pillStyle,
				Children: []core.Node{
					core.Text{
						TestID: "hud-score",
						ContentFunc: func() string {
							return fmt.Sprintf("SCORE %d", g.score.Get())
						},
						Style: hudText.Merge(core.Style{Color: core.RGB(0xfde047)}),
					},
					core.Show{
						When: g.swarm.Get,
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "hud-sprites",
								ContentFunc: func() string {
									return fmt.Sprintf("SPRITES %d", g.sprites.Get())
								},
								Style: hudText.Merge(core.Style{Color: core.RGB(0xa5f3fc)}),
							}
						},
					},
				},
			},
			// Right Pause button.
			ui.Button{
				Text:    "Pause",
				Label:   "Pause game",
				Variant: ui.ButtonSecondary,
				TestID:  "pause-btn",
				OnPress: g.togglePause,
			},
		},
	}
}

func (g *caveRun) renderTouchControls(cx *core.Scope) core.Node {
	makePadButton := func(testID, label, caption string, setHeld func(bool), onTap func()) core.Node {
		return core.View{
			TestID:  testID,
			Role:    core.RoleButton,
			Label:   label,
			NoFocus: true,
			Style: core.Style{
				MinWidth:       core.Pt(52),
				Height:         core.Pt(44),
				Padding:        core.Symmetric(8, 14),
				CornerRadius:   12,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     core.RGBA(0x1e293b, 0.65),
				BorderWidth:    1.5,
				BorderColor:    core.RGBA(0x94a3b8, 0.45),
			},
			OnPressDown: func(core.PressEvent) {
				setHeld(true)
				if onTap != nil {
					onTap()
				}
			},
			OnPressUp: func(core.PressEvent) {
				setHeld(false)
			},
			OnMouseLeave: func(core.MouseEvent) {
				setHeld(false)
			},
			OnTouch: func(ev core.TouchEvent) {
				switch ev.Phase {
				case core.TouchStart:
					setHeld(true)
					if onTap != nil {
						onTap()
					}
				case core.TouchEnd, core.TouchCancel:
					setHeld(false)
				}
				ev.StopPropagation()
			},
			Children: []core.Node{
				core.Text{
					Content: caption,
					Style: core.Style{
						Color:      core.RGB(0xf8fafc),
						FontSize:   14,
						FontWeight: core.FontWeightBold,
						FontFamily: core.FontMonospace,
					},
				},
			},
		}
	}

	return core.View{
		TestID:        "touch-controls",
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignEnd,
			JustifyContent: core.JustifySpaceBetween,
			Width:          core.Pct(100),
		},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Direction: core.Row,
					Gap:       10,
				},
				Children: []core.Node{
					makePadButton("ctrl-left", "Move left", "<", func(v bool) { g.padLeft = v }, nil),
					makePadButton("ctrl-right", "Move right", ">", func(v bool) { g.padRight = v }, nil),
				},
			},
			makePadButton("ctrl-jump", "Jump", "JUMP", func(v bool) { g.padJump = v }, func() { g.padJumpLatched = true }),
		},
	}
}

func (g *caveRun) renderTitleOverlay(cx *core.Scope) core.Node {
	return core.Show{
		When: func() bool { return g.phase.Get() == phaseTitle },
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "title-menu",
				StyleFunc: func() core.Style {
					return core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(0),
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						Padding:        g.safeInsets(),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyEnd,
						Background:     core.RGBA(0x0b0d17, 0.35),
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(300),
							Margin:       core.Edges{Bottom: 24},
							Padding:      core.All(18),
							CornerRadius: 14,
							AlignItems:   core.AlignCenter,
							Gap:          10,
							Background:   core.RGBA(0x0f1324, 0.92),
							BorderWidth:  1,
							BorderColor:  core.RGBA(0x94a3b8, 0.35),
						},
						Children: []core.Node{
							core.Text{
								Content: "CAVE RUN",
								Style: core.Style{
									Color:      core.RGB(0xf8fafc),
									FontSize:   22,
									FontWeight: core.FontWeightBold,
									FontFamily: core.FontMonospace,
								},
							},
							core.Text{
								Content: "Collect cave gems, dodge slimes & lava, and reach the amethyst portal.",
								Style: core.Style{
									Color:     core.RGB(0xcbd5e1),
									FontSize:  12,
									TextAlign: core.TextAlignCenter,
								},
							},
							ui.Button{
								Text:      "Play",
								Variant:   ui.ButtonPrimary,
								AutoFocus: true,
								TestID:    "start-btn",
								Style:     core.Style{Width: core.Pct(100)},
								OnPress:   g.startGame,
							},
							ui.Button{
								Text:    "Settings",
								Variant: ui.ButtonSecondary,
								TestID:  "settings-btn",
								Style:   core.Style{Width: core.Pct(100)},
								OnPress: g.openSettings,
							},
							core.Text{
								Content: "A/D or ←/→ move • Space jump • Esc pause • F fireflies\nGamepad: stick or D-pad • A jump • Start pause • X settings",
								Style: core.Style{
									Color:     core.RGB(0x94a3b8),
									FontSize:  11,
									TextAlign: core.TextAlignCenter,
								},
							},
						},
					},
				},
			}
		},
	}
}

func (g *caveRun) renderPauseOverlay(cx *core.Scope) core.Node {
	return core.Show{
		When: func() bool { return g.phase.Get() == phasePlaying && g.paused.Get() },
		Then: func(cx *core.Scope) core.Node {
			labelStyle := core.Style{
				Color:      core.RGB(0xe2e8f0),
				FontSize:   12,
				FontWeight: core.FontWeightSemibold,
			}
			return core.View{
				TestID: "pause-menu",
				StyleFunc: func() core.Style {
					return core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(0),
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						Padding:        g.safeInsets(),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     core.RGBA(0x060812, 0.72),
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(290),
							Padding:      core.All(20),
							CornerRadius: 14,
							Gap:          12,
							Background:   core.RGB(0x111827),
							BorderWidth:  1,
							BorderColor:  core.RGBA(0x94a3b8, 0.35),
						},
						Children: []core.Node{
							core.Text{
								Content: "Paused",
								Style: core.Style{
									Color:      core.RGB(0xf8fafc),
									FontSize:   20,
									FontWeight: core.FontWeightBold,
									TextAlign:  core.TextAlignCenter,
								},
							},
							ui.Button{
								Text:      "Resume",
								Variant:   ui.ButtonPrimary,
								AutoFocus: true,
								TestID:    "resume-btn",
								Style:     core.Style{Width: core.Pct(100)},
								OnPress:   g.resumeGame,
							},
							ui.Button{
								Text:    "Restart",
								Variant: ui.ButtonSecondary,
								TestID:  "restart-btn",
								Style:   core.Style{Width: core.Pct(100)},
								OnPress: g.restartGame,
							},
							ui.Button{
								Text:    "Settings",
								Variant: ui.ButtonSecondary,
								TestID:  "pause-settings-btn",
								Style:   core.Style{Width: core.Pct(100)},
								OnPress: g.openSettings,
							},
							ui.Button{
								TextFunc: func() string {
									if g.swarm.Get() {
										return "Fireflies: 1000"
									}
									return "Fireflies: Off"
								},
								Variant: ui.ButtonOutline,
								TestID:  "swarm-btn",
								Style:   core.Style{Width: core.Pct(100)},
								OnPress: g.toggleSwarm,
							},
							core.Show{
								When: g.isMuted,
								Then: func(cx *core.Scope) core.Node {
									return ui.Button{
										Text:    "Unmute Audio",
										Variant: ui.ButtonOutline,
										TestID:  "mute-btn",
										Style:   core.Style{Width: core.Pct(100)},
										OnPress: g.toggleMute,
									}
								},
								Else: func(cx *core.Scope) core.Node {
									return ui.Button{
										Text:    "Mute Audio",
										Variant: ui.ButtonOutline,
										TestID:  "mute-btn",
										Style:   core.Style{Width: core.Pct(100)},
										OnPress: g.toggleMute,
									}
								},
							},
							core.View{
								Style: core.Style{Gap: 4},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											return fmt.Sprintf("SFX Volume: %d%%", int(g.mixer.Volume(soundGroupSFX)*100+0.5))
										},
										Style: labelStyle,
									},
									ui.Slider{
										TestID: "sfx-volume",
										Label:  "SFX Volume",
										Min:    0,
										Max:    100,
										Step:   5,
										Value: func() float64 {
											return float64(g.mixer.Volume(soundGroupSFX) * 100)
										},
										OnChange: func(v float64) {
											_ = g.mixer.SetVolume(soundGroupSFX, float32(v/100))
										},
									},
								},
							},
							core.View{
								Style: core.Style{Gap: 4},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											return fmt.Sprintf("Music Volume: %d%%", int(g.mixer.Volume(soundGroupMusic)*100+0.5))
										},
										Style: labelStyle,
									},
									ui.Slider{
										TestID: "music-volume",
										Label:  "Music Volume",
										Min:    0,
										Max:    100,
										Step:   5,
										Value: func() float64 {
											return float64(g.mixer.Volume(soundGroupMusic) * 100)
										},
										OnChange: func(v float64) {
											_ = g.mixer.SetVolume(soundGroupMusic, float32(v/100))
										},
									},
								},
							},
						},
					},
				},
			}
		},
	}
}

func (g *caveRun) renderEndOverlay(cx *core.Scope) core.Node {
	return core.Show{
		When: func() bool {
			p := g.phase.Get()
			return p == phaseVictory || p == phaseGameOver
		},
		Then: func(cx *core.Scope) core.Node {
			won := g.phase.Get() == phaseVictory
			overlayID := "gameover-overlay"
			heading := "Game Over"
			headingColor := core.RGB(0xfb7185)
			if won {
				overlayID = "victory-overlay"
				heading = "Cave Cleared!"
				headingColor = core.RGB(0x5eead4)
			}

			return core.View{
				TestID: overlayID,
				StyleFunc: func() core.Style {
					return core.Style{
						Position:       core.Absolute,
						Top:            core.Pt(0),
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(0),
						Padding:        g.safeInsets(),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Background:     core.RGBA(0x060812, 0.72),
					}
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(290),
							Padding:      core.All(20),
							CornerRadius: 14,
							AlignItems:   core.AlignCenter,
							Gap:          10,
							Background:   core.RGB(0x111827),
							BorderWidth:  1,
							BorderColor:  core.RGBA(0x94a3b8, 0.35),
						},
						Children: []core.Node{
							core.Text{
								Content: heading,
								Style: core.Style{
									Color:      headingColor,
									FontSize:   22,
									FontWeight: core.FontWeightBold,
								},
							},
							core.Text{
								TestID: "final-score",
								ContentFunc: func() string {
									return fmt.Sprintf("Score: %d  •  Gems: %d/%d  •  Hearts: %d/%d",
										g.score.Get(), g.gems.Get(), g.totalGems.Get(), g.hearts.Get(), maxHearts)
								},
								Style: core.Style{
									Color:      core.RGB(0xe2e8f0),
									FontSize:   13,
									FontFamily: core.FontMonospace,
									TextAlign:  core.TextAlignCenter,
								},
							},
							ui.Button{
								Text:      "Play Again",
								Variant:   ui.ButtonPrimary,
								AutoFocus: true,
								TestID:    "replay-btn",
								Style:     core.Style{Width: core.Pct(100)},
								OnPress:   g.restartGame,
							},
						},
					},
				},
			}
		},
	}
}

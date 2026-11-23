package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/leaderboard"
	"github.com/nsgo-dev/nsgo/core"
)

// GameController connects the deterministic Arena simulation to NSGo's reactive view tree,
// fixed-step game.Loop, and input.Actions bindings.
type GameController struct {
	Arena   *Arena
	Actions *input.Actions
	Loop    *game.Loop
	// Boards submits each finished run's score and opens the platform's leaderboard.
	Boards *leaderboard.Service

	frameSig    *core.Signal[uint64]
	hudSig      *core.Signal[HUDState]
	boardStatus *core.Signal[string]
	// runID counts runs left after a game over, so a submission or sign-in answered after the next run started does not label that run.
	runID uint64

	viewSize     core.Size
	hasPointer   bool
	pointerView  core.Point
	lastHUDState HUDState
}

// Root is Starburst's NSGo root view.
func Root(cx *core.Scope) core.Node {
	_, node := NewGameView(cx, DefaultSeed)
	return node
}

// NewGameView constructs a complete playable Starburst view backed by a seeded Arena.
func NewGameView(cx *core.Scope, seed uint64) (*GameController, core.Node) {
	arena := NewArena(seed)
	actions := input.NewActions()

	// Bind touch virtual pad regions and mouse buttons into input.Actions.
	// Hardware gamepad bindings await nsgo#1049 and plug into Arena.Control alongside these.
	actions.Bind("move_left", input.TouchRegion(touchMoveLeftRect))
	actions.Bind("move_right", input.TouchRegion(touchMoveRightRect))
	actions.Bind("move_up", input.TouchRegion(touchMoveUpRect))
	actions.Bind("move_down", input.TouchRegion(touchMoveDownRect))
	actions.BindAxis("move_x", "move_left", "move_right")
	actions.BindAxis("move_y", "move_up", "move_down")
	actions.Bind("fire", input.MouseButton(core.MouseButtonLeft), input.TouchRegion(touchFireRect))
	actions.Bind("nova", input.MouseButton(core.MouseButtonRight), input.TouchRegion(touchNovaRect))

	initialHUD := arena.HUD()
	gc := &GameController{
		Arena:        arena,
		Actions:      actions,
		frameSig:     core.NewSignal(cx, uint64(0)),
		hudSig:       core.NewSignal(cx, initialHUD),
		boardStatus:  core.NewSignal(cx, ""),
		Boards:       newLeaderboards(cx),
		viewSize:     core.Size{Width: VirtualWidth, Height: VirtualHeight},
		lastHUDState: initialHUD,
	}

	actions.MapPoint = func(pt core.Point) (core.Point, bool) {
		return arena.Viewport.ViewToGame(gc.currentViewSize(cx), pt)
	}

	// Reset held input when the window moves to the background so nothing stays stuck.
	cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			actions.Reset()
		}
	})

	gc.bindKeys(cx)

	gc.Loop = &game.Loop{
		Rate: StepRate,
		Update: func(dt time.Duration) {
			gc.stepInput(cx)
			gc.Arena.Step(dt)
		},
		Draw: func(float64) {
			gc.syncUI()
		},
	}
	gc.Loop.Run(cx)

	return gc, gc.buildView(cx)
}

func (gc *GameController) currentViewSize(cx *core.Scope) core.Size {
	if gc.viewSize.Width > 0 && gc.viewSize.Height > 0 {
		return gc.viewSize
	}
	if w := cx.Env().Window(); w.Width > 0 && w.Height > 0 {
		return w
	}
	return core.Size{Width: VirtualWidth, Height: VirtualHeight}
}

func (gc *GameController) bindKeys(cx *core.Scope) {
	moveOpts := core.KeyOptions{When: core.KeyAlways, Repeat: true}
	for _, key := range []string{"w", "a", "s", "d", "up", "down", "left", "right"} {
		k := key
		cx.OnKeyWith(k, moveOpts, func() {
			gc.Arena.PressMoveKey(k)
		})
	}
	actionOpts := core.KeyOptions{When: core.KeyAlways}
	cx.OnKeyWith("space", actionOpts, func() {
		gc.Arena.TriggerNova()
	})
	cx.OnKeyWith("p", actionOpts, func() {
		gc.TogglePause()
	})
	cx.OnKeyWith("r", actionOpts, func() {
		gc.Restart()
	})
	cx.OnKeyWith("t", actionOpts, func() {
		gc.ToggleStress()
	})
}

func (gc *GameController) stepInput(cx *core.Scope) {
	gc.Actions.Step()

	ctrl := ControlInput{
		MoveX: float32(gc.Actions.Axis("move_x")),
		MoveY: float32(gc.Actions.Axis("move_y")),
		Fire:  gc.Actions.Pressed("fire"),
		Nova:  gc.Actions.JustPressed("nova"),
	}

	if gc.hasPointer {
		if gamePt, ok := gc.Arena.Viewport.ViewToGame(gc.currentViewSize(cx), gc.pointerView); ok {
			worldPt := gc.Arena.Camera.ViewToWorld(gamePt)
			ctrl.AimWorldX = float32(worldPt.X)
			ctrl.AimWorldY = float32(worldPt.Y)
			ctrl.HasWorldAim = true
		}
	}

	gc.Arena.Control = ctrl
}

func (gc *GameController) syncUI() {
	gc.frameSig.Set(gc.frameSig.GetUntracked() + 1)
	next := gc.Arena.HUD()
	if next != gc.lastHUDState {
		if next.GameOver && !gc.lastHUDState.GameOver && !next.StressMode {
			gc.submitScore(next.Score)
		}
		if gc.lastHUDState.GameOver && !next.GameOver {
			// A new run starts however the game-over screen is left (PLAY AGAIN, RESTART or the stress toggle), so the previous run's leaderboard result must not carry over.
			gc.runID++
			gc.boardStatus.Set("")
		}
		gc.lastHUDState = next
		gc.hudSig.Set(next)
	}
}

// ToggleStress toggles the 1000-sprite stress benchmark mode and refreshes the HUD.
func (gc *GameController) ToggleStress() {
	gc.Arena.ToggleStressMode()
	gc.syncUI()
}

// TogglePause toggles pause and refreshes the HUD.
func (gc *GameController) TogglePause() {
	gc.Arena.TogglePaused()
	gc.syncUI()
}

// Restart resets the current session and refreshes the HUD.
func (gc *GameController) Restart() {
	gc.Arena.Restart()
	gc.Actions.Reset()
	gc.syncUI()
}

func (gc *GameController) buildView(cx *core.Scope) core.Node {
	full := core.Style{
		Position: core.Absolute,
		Top:      core.Pt(0),
		Right:    core.Pt(0),
		Bottom:   core.Pt(0),
		Left:     core.Pt(0),
	}

	arenaView := core.View{
		TestID: "arena-view",
		Style:  full,
		OnTouch: func(ev core.TouchEvent) {
			if ev.Phase == core.TouchStart || ev.Phase == core.TouchMove {
				gc.Arena.ShowTouchHints = true
			}
		},
		OnMouseMove: func(ev core.MouseEvent) {
			gc.pointerView = ev.Position
			gc.hasPointer = true
		},
		OnMouseLeave: func(core.MouseEvent) {
			gc.hasPointer = false
		},
		OnPressDown: func(ev core.PressEvent) {
			if !ev.Keyboard {
				gc.pointerView = ev.Position
				gc.hasPointer = true
			}
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "arena-canvas",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = gc.frameSig.Get()
					if size.Width > 0 && size.Height > 0 {
						gc.viewSize = size
					}
					gc.Arena.Draw(d, size)
				},
			},
		},
	}
	gc.Actions.Attach(&arenaView)

	return core.View{
		TestID: "starburst-root",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(0x04070E),
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			arenaView,
			gc.hudOverlay(cx, full),
		},
	}
}

func (gc *GameController) hudOverlay(cx *core.Scope, full core.Style) core.Node {
	hudPanelStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		Gap:          12,
		Padding:      core.Edges{Top: 6, Right: 12, Bottom: 6, Left: 12},
		Background:   core.RGBA(0x0B1221, 0.84),
		CornerRadius: 8,
		BorderWidth:  1,
		BorderColor:  core.RGBA(0x38BDF8, 0.35),
	}

	metricStyle := core.Style{
		FontFamily:  core.FontMonospace,
		FontSize:    12,
		FontWeight:  core.FontWeightSemibold,
		FontVariant: core.FontTabularNums,
		Color:       core.RGB(0xE2E8F0),
	}

	hudBtn := func(testID string, label func() string, active func() bool, onPress func()) core.Node {
		return core.View{
			TestID: testID,
			Role:   core.RoleButton,
			OnPress: func(core.PressEvent) {
				onPress()
			},
			StyleFunc: func() core.Style {
				bg := core.RGBA(0x0F172A, 0.88)
				border := core.RGBA(0x38BDF8, 0.45)
				if active != nil && active() {
					bg = core.RGBA(0x0284C7, 0.92)
					border = core.RGB(0x7DD3FC)
				}
				return core.Style{
					Padding:      core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 10},
					Background:   bg,
					CornerRadius: 6,
					BorderWidth:  1,
					BorderColor:  border,
				}
			},
			Children: []core.Node{
				core.Text{
					ContentFunc: label,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11,
						FontWeight: core.FontWeightSemibold,
						Color:      core.RGB(0xF8FAFC),
					},
				},
			},
		}
	}

	return core.View{
		TestID:        "hud-layer",
		PointerEvents: core.PointerEventsBoxNone,
		StyleFunc: func() core.Style {
			safe := cx.Env().SafeArea()
			s := full
			s.Padding = core.Edges{
				Top:    max(core.Points(10), safe.Top+6),
				Right:  max(core.Points(12), safe.Right+8),
				Bottom: max(core.Points(10), safe.Bottom+6),
				Left:   max(core.Points(12), safe.Left+8),
			}
			s.JustifyContent = core.JustifySpaceBetween
			return s
		},
		Children: []core.Node{
			// Top telemetry & control bar.
			core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Direction:      core.Row,
					JustifyContent: core.JustifySpaceBetween,
					AlignItems:     core.AlignCenter,
					Gap:            8,
				},
				Children: []core.Node{
					core.View{
						TestID: "hud-stats",
						Style:  hudPanelStyle,
						Children: []core.Node{
							core.Text{
								TestID: "hud-wave",
								Style:  metricStyle.Merge(core.Style{Color: core.RGB(0x38BDF8)}),
								ContentFunc: func() string {
									h := gc.hudSig.Get()
									if h.StressMode {
										return "STRESS"
									}
									return fmt.Sprintf("WAVE %d", h.Wave)
								},
							},
							core.Text{
								TestID: "hud-score",
								Style:  metricStyle,
								ContentFunc: func() string {
									return fmt.Sprintf("SCORE %06d", gc.hudSig.Get().Score)
								},
							},
							core.Text{
								TestID: "hud-combo",
								StyleFunc: func() core.Style {
									c := core.RGB(0x94A3B8)
									if gc.hudSig.Get().Combo > 1 {
										c = core.RGB(0xFBBF24)
									}
									return metricStyle.Merge(core.Style{Color: c})
								},
								ContentFunc: func() string {
									return fmt.Sprintf("x%d", gc.hudSig.Get().Combo)
								},
							},
							core.Text{
								TestID: "hud-hp",
								StyleFunc: func() core.Style {
									h := gc.hudSig.Get()
									c := core.RGB(0x34D399)
									if h.HP <= 2 && !h.StressMode {
										c = core.RGB(0xF87171)
									}
									return metricStyle.Merge(core.Style{Color: c})
								},
								ContentFunc: func() string {
									h := gc.hudSig.Get()
									if h.StressMode {
										return "SHIELD ∞"
									}
									return fmt.Sprintf("HP %d/%d", h.HP, h.MaxHP)
								},
							},
							core.Text{
								TestID: "hud-sprites",
								Style:  metricStyle.Merge(core.Style{Color: core.RGB(0xC084FC)}),
								ContentFunc: func() string {
									return fmt.Sprintf("SPRITES %d", gc.hudSig.Get().ActiveSprites)
								},
							},
						},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							hudBtn(
								"btn-stress",
								func() string {
									if gc.hudSig.Get().StressMode {
										return "STRESS: ON"
									}
									return "STRESS 1000"
								},
								func() bool { return gc.hudSig.Get().StressMode },
								gc.ToggleStress,
							),
							hudBtn(
								"btn-pause",
								func() string {
									if gc.hudSig.Get().Paused {
										return "RESUME"
									}
									return "PAUSE"
								},
								func() bool { return gc.hudSig.Get().Paused },
								gc.TogglePause,
							),
							hudBtn(
								"btn-restart",
								func() string { return "RESTART" },
								nil,
								gc.Restart,
							),
						},
					},
				},
			},

			// Center Pause / Game Over banner.
			core.Show{
				When: func() bool {
					h := gc.hudSig.Get()
					return h.Paused || h.GameOver
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						TestID: "overlay-status",
						Style: core.Style{
							AlignSelf:    core.AlignCenter,
							AlignItems:   core.AlignCenter,
							Gap:          10,
							Padding:      core.Edges{Top: 18, Right: 28, Bottom: 18, Left: 28},
							Background:   core.RGBA(0x070B16, 0.92),
							CornerRadius: 12,
							BorderWidth:  1,
							BorderColor:  core.RGBA(0x38BDF8, 0.6),
						},
						Children: []core.Node{
							core.Text{
								TestID: "overlay-title",
								ContentFunc: func() string {
									if gc.hudSig.Get().GameOver {
										return "GAME OVER"
									}
									return "PAUSED"
								},
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   22,
									FontWeight: core.FontWeightBold,
									Color:      core.RGB(0xF8FAFC),
								},
							},
							core.Text{
								TestID: "overlay-subtitle",
								ContentFunc: func() string {
									h := gc.hudSig.Get()
									return fmt.Sprintf("WAVE %d  •  SCORE %06d", h.Wave, h.Score)
								},
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   12,
									Color:      core.RGB(0x94A3B8),
								},
							},
							core.Show{
								When: func() bool { return gc.hudSig.Get().GameOver && gc.boardStatus.Get() != "" },
								Then: func(*core.Scope) core.Node {
									return core.Text{
										TestID:      "overlay-board-status",
										ContentFunc: gc.boardStatus.Get,
										Style: core.Style{
											FontFamily: core.FontMonospace,
											FontSize:   11,
											Color:      core.RGB(0x7DD3FC),
										},
									}
								},
							},
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
								Children: []core.Node{
									hudBtn(
										"btn-overlay-action",
										func() string {
											if gc.hudSig.Get().GameOver {
												return "PLAY AGAIN"
											}
											return "RESUME"
										},
										func() bool { return true },
										func() {
											if gc.Arena.GameOver {
												gc.Restart()
											} else {
												gc.TogglePause()
											}
										},
									),
									core.Show{
										When: func() bool {
											return gc.hudSig.Get().GameOver && gc.Boards.SignedIn() && gc.Boards.Supports(highScoreBoard)
										},
										Then: func(*core.Scope) core.Node {
											return hudBtn("btn-leaderboard", func() string { return "LEADERBOARD" }, nil, gc.ShowLeaderboard)
										},
									},
									core.Show{
										When: func() bool {
											return gc.hudSig.Get().GameOver && !gc.Boards.SignedIn() && gc.Boards.Supports(highScoreBoard)
										},
										Then: func(*core.Scope) core.Node {
											return hudBtn("btn-sign-in", func() string { return "SIGN IN" }, nil, gc.SignInAndSubmit)
										},
									},
								},
							},
						},
					}
				},
			},

			// Bottom compact control legend.
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					AlignSelf:    core.AlignCenter,
					Padding:      core.Edges{Top: 4, Right: 10, Bottom: 4, Left: 10},
					Background:   core.RGBA(0x0B1221, 0.72),
					CornerRadius: 6,
				},
				Children: []core.Node{
					core.Text{
						TestID:  "hud-controls",
						Content: "WASD/Arrows: Move • Mouse/Auto-Aim: Fire • Space/Right-Click: Nova • T: Stress 1000 • P: Pause • R: Restart",
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   10,
							Color:      core.RGB(0x94A3B8),
						},
					},
				},
			},
		},
	}
}

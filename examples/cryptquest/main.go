// Command cryptquest is a top-down tile-based dungeon adventure built with the
// NSGo 2D game SDK. It demonstrates pathfind.AStar obstacle-avoiding enemy
// pathfinding driven by flow.Machine, multi-page typewriter dialogue boxes
// rendered with text.DrawPlatform plus native NSGo controls, and versioned
// save/load persistence via save.NewStorageStore.
package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"
)

func main() {
	nsgo.Run(
		Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

// Root builds the CryptQuest application backed by the app's persistent core.Storage.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, gameProject.Title())
	store := save.NewStorageStore(cx.Storage(), "cryptquest", save.WithVersion(SaveSchemaVersion))
	g := NewGame(store)
	return RenderGame(cx, g)
}

// RenderGame mounts the fixed-step game loop, virtual viewport canvas,
// touch/keyboard bindings, native HUD, and dialogue controls for g.
func RenderGame(cx *core.Scope, g *Game) core.Node {
	g.env = cx.Env()
	touch, err := input.NewTouchControls(cx, touchLayout)
	if err != nil {
		panic(fmt.Sprintf("cryptquest: controls.touch.yaml: %v", err))
	}
	touch.BindActions(g.Actions)
	touch.Haptics = input.NewHaptics(cx)
	g.Touch = touch

	frameTick := core.NewSignal(cx, uint64(0))
	hud := core.NewSignal(cx, g.SnapshotHUD())
	stageRef := &core.Ref{}

	syncUI := func() {
		next := g.SnapshotHUD()
		if next != hud.Get() {
			hud.Set(next)
		}
		frameTick.Update(func(n uint64) uint64 { return n + 1 })
	}

	// Fixed-step 60 Hz simulation loop.
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			g.Update(dt)
		},
		Draw: func(alpha float64) {
			g.DrawAlpha = alpha
			syncUI()
		},
	}
	loop.Run(cx)

	// Reset held touch/mouse actions when the app moves to the background.
	cx.Effect(func() {
		if cx.Env().Lifecycle() == core.LifecycleBackground {
			g.Actions.Reset()
		}
	})

	// Fallback keymap bindings so WASD/Arrows/E/F/N/Escape work even after clicking a HUD button.
	for _, key := range []string{"w", "a", "s", "d", "up", "down", "left", "right", "e", "f", "n", "escape"} {
		k := key
		cx.OnKey(k, func() {
			if g.HandleKey(k) {
				syncUI()
			}
		})
	}

	bgDark := core.RGB(0x0b0c12)
	panelBg := core.RGB(0x161824)
	borderCol := core.RGBA(0x9ca4c0, 0.22)
	goldCol := core.RGB(0xfada70)
	textCol := core.RGB(0xf0f2fa)
	mutedCol := core.RGB(0x9ca4c0)
	hpCol := core.RGB(0xf27074)
	tealCol := core.RGB(0x74f4e0)

	stage := core.View{
		TestID:    "stage",
		Ref:       stageRef,
		Focusable: func() bool { return true },
		OnKeyDown: func(ev core.KeyEvent) bool {
			if g.HandleKey(ev.Key) {
				syncUI()
				return true
			}
			return false
		},
		OnLayout: func(b core.Bounds) {
			g.ViewSize = b.Size
			if g.Touch != nil && b.Size.Width > 0 && b.Size.Height > 0 {
				g.Touch.Resize(b.Size, g.stageSafeArea())
			}
		},
		Style: core.Style{
			FlexGrow: 1,
			Width:    core.Pct(100),
			Height:   core.Pct(100),
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "CryptQuest dungeon viewport",
				Style: core.Style{
					Width:  core.Pct(100),
					Height: core.Pct(100),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = frameTick.Get()
					g.Draw(d, size)
				},
			},
		},
	}
	g.Actions.Attach(&stage)
	// After Actions.Attach, so a touch shows the on-screen controls before they hear it.
	g.Touch.Attach(&stage)

	badge := func(testID string, color core.Color, textFn func() string) core.Node {
		return core.View{
			Style: core.Style{
				Background:   core.RGB(0x1f2233),
				BorderColor:  borderCol,
				BorderWidth:  1,
				CornerRadius: 6,
				Padding:      core.Edges{Top: 4, Bottom: 4, Left: 10, Right: 10},
			},
			Children: []core.Node{
				core.Text{
					TestID:      testID,
					ContentFunc: textFn,
					Style: core.Style{
						FontSize:   12,
						FontWeight: core.FontWeightSemibold,
						Color:      color,
					},
				},
			},
		}
	}

	return core.View{
		TestID: "cryptquest-root",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Direction:  core.Column,
			Background: bgDark,
			Color:      textCol,
		},
		Children: []core.Node{
			// Top Native HUD
			core.View{
				TestID: "hud-bar",
				Style: core.Style{
					Direction:      core.Row,
					Wrap:           core.Wrap,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Edges{Top: 8, Bottom: 8, Left: 12, Right: 12},
					PaddingEnv:     core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea},
					Background:     panelBg,
					BorderColor:    borderCol,
					BorderWidth:    1,
					RowGap:         6,
					ColumnGap:      10,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							Wrap:       core.Wrap,
							AlignItems: core.AlignCenter,
							Gap:        6,
						},
						Children: []core.Node{
							core.Text{
								Content: "CryptQuest",
								Style: core.Style{
									FontSize:   15,
									FontWeight: core.FontWeightBold,
									Color:      goldCol,
								},
							},
							badge("hud-hp", hpCol, func() string {
								h := hud.Get()
								return fmt.Sprintf("HP: %d/%d", h.HP, h.MaxHP)
							}),
							badge("hud-relic", goldCol, func() string {
								h := hud.Get()
								if h.QuestCompleted {
									return "Relic: Escaped!"
								}
								if h.HasRelic {
									return "Relic: Sun Relic"
								}
								return "Relic: Missing"
							}),
							badge("hud-room", tealCol, func() string {
								h := hud.Get()
								return fmt.Sprintf("%s (%d/%d)", h.RoomName, h.VisitedCount, TotalRooms)
							}),
						},
					},
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							Wrap:       core.Wrap,
							AlignItems: core.AlignCenter,
							Gap:        6,
						},
						Children: []core.Node{
							ui.Button{
								TestID:  "btn-interact",
								Text:    "Interact",
								Variant: ui.ButtonPrimary,
								Size:    ui.SizeSmall,
								OnPress: func() {
									g.Interact()
									syncUI()
								},
							},
							ui.Button{
								TestID:  "btn-attack",
								Text:    "Banish",
								Variant: ui.ButtonSecondary,
								Size:    ui.SizeSmall,
								OnPress: func() {
									g.Attack()
									syncUI()
								},
							},
							ui.Button{
								TestID:  "btn-save",
								Text:    "Save",
								Variant: ui.ButtonOutline,
								Size:    ui.SizeSmall,
								OnPress: func() {
									_ = g.Save()
									syncUI()
								},
							},
							ui.Button{
								TestID:   "btn-load",
								Text:     "Load",
								Variant:  ui.ButtonOutline,
								Size:     ui.SizeSmall,
								Disabled: func() bool { return !hud.Get().HasSave },
								OnPress: func() {
									_ = g.Load()
									syncUI()
								},
							},
							ui.Button{
								TestID:  "btn-reset",
								Text:    "Reset",
								Variant: ui.ButtonGhost,
								Size:    ui.SizeSmall,
								OnPress: func() {
									g.Reset()
									syncUI()
								},
							},
						},
					},
				},
			},

			// Game Stage + Native Dialogue Overlay Controls
			core.View{
				TestID: "stage-container",
				OnLayout: func(b core.Bounds) {
					g.stageFrame = b
					if g.Touch != nil && b.Size.Width > 0 && b.Size.Height > 0 {
						g.Touch.Resize(b.Size, g.stageSafeArea())
					}
				},
				Style: core.Style{
					FlexGrow: 1,
					Position: core.Relative,
					Overflow: core.Hidden,
				},
				Children: []core.Node{
					stage,
					core.Show{
						When: func() bool { return hud.Get().DialogueOpen },
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "dialogue-controls",
								Style: core.Style{
									Position:       core.Absolute,
									Left:           core.Pt(16),
									Right:          core.Pt(16),
									Bottom:         core.Pt(12),
									MarginEnv:      core.EnvEdges{Left: core.EnvSafeArea, Right: core.EnvSafeArea},
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifySpaceBetween,
									Padding:        core.Edges{Top: 8, Bottom: 8, Left: 12, Right: 12},
									Background:     core.RGBA(0x141726, 0.94),
									BorderColor:    goldCol,
									BorderWidth:    1,
									CornerRadius:   8,
									Gap:            8,
								},
								Children: []core.Node{
									core.Text{
										TestID: "dialogue-page",
										ContentFunc: func() string {
											h := hud.Get()
											return fmt.Sprintf("%s — Page %d of %d", h.DialogueSpeaker, h.DialoguePage, h.DialogueTotal)
										},
										Style: core.Style{
											FontSize:   12,
											FontWeight: core.FontWeightSemibold,
											Color:      goldCol,
										},
									},
									core.View{
										Style: core.Style{
											Direction: core.Row,
											Gap:       8,
										},
										Children: []core.Node{
											ui.Button{
												TestID:  "dialogue-advance",
												Text:    "Next",
												Variant: ui.ButtonPrimary,
												Size:    ui.SizeSmall,
												OnPress: func() {
													g.AdvanceDialogue()
													syncUI()
												},
											},
											ui.Button{
												TestID:  "dialogue-close",
												Text:    "Close",
												Variant: ui.ButtonOutline,
												Size:    ui.SizeSmall,
												OnPress: func() {
													g.CloseDialogue()
													syncUI()
												},
											},
										},
									},
								},
							}
						},
					},
				},
			},

			// Bottom Status & Controls Bar
			core.View{
				TestID: "status-bar",
				Style: core.Style{
					Direction:      core.Row,
					Wrap:           core.Wrap,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Edges{Top: 6, Bottom: 6, Left: 12, Right: 12},
					PaddingEnv:     core.EnvEdges{Bottom: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea},
					Background:     panelBg,
					BorderColor:    borderCol,
					BorderWidth:    1,
					Gap:            8,
				},
				Children: []core.Node{
					core.Text{
						TestID:      "hud-status",
						ContentFunc: func() string { return hud.Get().Status },
						Style: core.Style{
							FontSize: 12,
							Color:    textCol,
						},
					},
					core.Show{
						When: func() bool { return cx.Env().Idiom() != core.IdiomPhone },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								Content: "Move: WASD / Arrows  |  E: Interact  |  F: Banish",
								Style: core.Style{
									FontSize: 11,
									Color:    mutedCol,
								},
							}
						},
					},
				},
			},
		},
	}
}

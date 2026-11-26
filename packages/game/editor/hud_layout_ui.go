package editor

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Additional alpha derivatives strictly from Spec color tokens (docs/design/editor/Spec.dc.html).
var (
	specSelectedRowFill = core.RGBA(0x3B7BFF, 0.14)
	specSafeStripFill   = core.RGBA(0xF2C14E, 0.10)
	specSafeDashStroke  = core.RGBA(0xF2C14E, 0.72)
	specTouchDashStroke = core.RGBA(0x3B7BFF, 0.80)
	specProblemHatch    = core.RGBA(0xFF6B6B, 0.35)
	specTouchFill       = core.RGBA(0xFFFFFF, 0.14)
	specTouchBase       = core.RGBA(0xFFFFFF, 0.08)
	specTouchKnob       = core.RGBA(0xFFFFFF, 0.28)
	specTouchBorder     = core.RGBA(0xFFFFFF, 0.30)
	specHintFill        = core.RGBA(0x101216, 0.80)
	specMenuBtnFill     = core.RGBA(0xFFFFFF, 0.08)
	specDimOverlay      = core.RGBA(0x101216, 0.62)
	specPanelBackdrop   = core.RGBA(0x171A21, 0.96)
)

// HUDLayoutView renders the full-window HUD and menu layout screen matching docs/design/editor/HUDLayout.dc.html
// using exclusively Spec color, spacing, radius, type, and focus-ring tokens from docs/design/editor/Spec.dc.html.
func HUDLayoutView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	promptDraft := core.NewSignal(cx, "")
	canvasW := core.NewSignal(cx, float32(848))
	canvasH := core.NewSignal(cx, float32(812))

	// Pause heavy left-rail / text-width-table / inspector rebuilds (which run gameui.CheckScreen
	// across project screens) while a touch drag streams mouse moves, and gate rebuilds on HUD
	// state revisions so unrelated background editor notifications (such as async CheckProblems
	// completions or disk-watch ticks) never remount HUD inspector buttons mid-press.
	type hudStableKey struct {
		rev uint64
		sel string
	}
	var lastNonDragRev uint64
	stableRev := core.NewMemo(cx, func() hudStableKey {
		_ = rev.Get()
		if !ed.HUDTouchDragActive() {
			lastNonDragRev = ed.hud.rev
		}
		return hudStableKey{rev: lastNonDragRev, sel: ed.HUDSelectedNodeID()}
	})
	stableTick := func() { _ = stableRev.Get() }

	return core.View{
		TestID: "hud-layout-screen",
		Role:   core.RoleRegion,
		Label:  "HUD and menu layout",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			// Cancel an active touch control drag on Escape.
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.HUDTouchDragActive()
				},
				Then: func(cx *core.Scope) core.Node {
					cx.OnKey("escape", func() {
						ed.CancelHUDTouchDrag()
					})
					return core.View{
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							Position: core.Absolute,
							Width:    core.Pt(0),
							Height:   core.Pt(0),
						},
					}
				},
			},
			hudHeaderView(cx, ed, rev),
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					hudLeftRailView(cx, ed, rev, stableTick),
					hudCenterViewportView(cx, ed, rev, stableTick, canvasW, canvasH),
					hudInspectorRailView(cx, ed, rev, stableTick, promptDraft),
				},
			},
		},
	}
}

// hudHeaderView builds the 44 px top header with breadcrumb, Language segmented control, Search ⌘K, and Play.
func hudHeaderView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	searchShortcut := FormatShortcut("mod+k", cx.Env().OS())
	type hudLangKey struct {
		locales string
		active  string
	}
	langKey := core.NewMemo(cx, func() hudLangKey {
		_ = rev.Get()
		return hudLangKey{
			locales: strings.Join(ed.HUDLocales(), "\x00"),
			active:  ed.HUDLocale(),
		}
	})

	return core.View{
		TestID: "hud-header",
		Role:   core.RoleRegion,
		Label:  "HUD layout header",
		Style: core.Style{
			Height:      core.Pt(44),
			FlexShrink:  0,
			Direction:   core.Row,
			AlignItems:  core.AlignCenter,
			Gap:         12,
			Padding:     core.Symmetric(0, 12),
			Background:  specBg,
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			// Left breadcrumb: brand badge + "<project> / screens / <screenFile>".
			core.View{
				TestID: "hud-breadcrumb",
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.View{
						TestID: "hud-back-scene",
						Role:   core.RoleButton,
						Label:  "Back to scene editor",
						Style: core.Style{
							Height:       core.Pt(28),
							MinWidth:     core.Pt(28),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Padding:      core.Symmetric(0, 4),
							CornerRadius: 6,
							Hover:        &core.Style{Background: specRaised},
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							ed.SetHUDLayoutOpen(false)
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:          core.Pt(22),
									Height:         core.Pt(22),
									CornerRadius:   6,
									Background:     specAccent,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								Children: []core.Node{
									core.SVGView{
										SVG:   lucide.ArrowLeft(),
										Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: specBg},
									},
								},
							},
							core.Text{
								TestID: "hud-breadcrumb-project",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.HUDProjectName()
								},
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextStrong,
								},
							},
						},
					},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{Content: "screens", Style: core.Style{FontSize: 12.5, Color: specTextSecondary}},
					core.Text{Content: "/", Style: core.Style{FontSize: 12.5, Color: specTextMuted}},
					core.Text{
						TestID: "hud-breadcrumb-file",
						ContentFunc: func() string {
							_ = rev.Get()
							p := ed.HUDActiveScreenPath()
							if p == "" {
								return "none"
							}
							return screenFileName(p, "")
						},
						Style: core.Style{
							FontSize: 12.5,
							Color:    specTextSecondary,
						},
					},
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			// Center Language segmented control.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.Text{
						Content: "Language",
						Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = langKey.Get()
						locales := ed.HUDLocales()
						active := ed.HUDLocale()
						btns := make([]core.Node, 0, len(locales))
						for _, loc := range locales {
							l := loc
							on := l == active
							bg := core.Transparent
							fg := specTextMuted
							if on {
								bg = specLineRaised
								fg = specTextStrong
							}
							btns = append(btns, core.View{
								TestID: "hud-lang-" + l,
								Role:   core.RoleButton,
								Label:  "Language " + hudLocaleLabel(l),
								Style: core.Style{
									Height:         core.Pt(24),
									MinWidth:       core.Pt(28),
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									Padding:        core.Symmetric(0, 10),
									CornerRadius:   6,
									Background:     bg,
									FocusVisible:   specFocusRingStyle,
								},
								OnPress: func(core.PressEvent) {
									ed.SetHUDLocale(l)
								},
								Children: []core.Node{
									core.Text{
										Content: hudLocaleLabel(l),
										Style: core.Style{
											FontSize:   12,
											FontWeight: core.FontWeightMedium,
											Color:      fg,
										},
									},
								},
							})
						}
						return core.View{
							TestID: "hud-language-group",
							Role:   core.RoleGroup,
							Label:  "Language",
							Style: core.Style{
								Direction:    core.Row,
								Gap:          2,
								Padding:      core.All(3),
								CornerRadius: 8,
								Background:   specRaised,
								BorderWidth:  1,
								BorderColor:  specLineRaised,
							},
							Children: btns,
						}
					}),
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			// Right actions: Search ⌘K and Play.
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.View{
						TestID: "hud-search-btn",
						Role:   core.RoleButton,
						Label:  "Search · " + searchShortcut,
						Style: core.Style{
							Height:       core.Pt(30),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Padding:      core.Symmetric(0, 10),
							CornerRadius: 8,
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
							Hover:        &core.Style{Background: specLineRaised},
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							ed.ToggleCommandPalette()
						},
						Children: []core.Node{
							core.SVGView{
								SVG:   lucide.Search(),
								Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specTextMuted},
							},
							core.Text{
								Content: "Search",
								Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
							},
							core.Text{
								Content: searchShortcut,
								Style: core.Style{
									FontFamily: core.FontMonospace,
									FontSize:   11,
									Color:      specTextMuted,
								},
							},
						},
					},
					core.View{
						TestID: "hud-play-btn",
						Role:   core.RoleButton,
						Label:  "Play",
						Style: core.Style{
							Height:       core.Pt(30),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Padding:      core.Symmetric(0, 14),
							CornerRadius: 8,
							Background:   specTextStrong,
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							if ed.Playing() {
								ed.report(ed.Stop())
							} else {
								ed.SetHUDLayoutOpen(false)
								ed.report(ed.Play())
							}
						},
						Children: []core.Node{
							core.SVGView{
								SVG:   lucide.Play(),
								Style: core.Style{Width: core.Pt(11), Height: core.Pt(11), Color: specBg},
							},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.Playing() {
										return "Stop"
									}
									return "Play"
								},
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: core.FontWeightSemibold,
									Color:      specBg,
								},
							},
						},
					},
				},
			},
		},
	}
}

// hudLeftRailView builds the 232 px left rail: SCREENS list, NODES tree, and bottom manual-scope note.
func hudLeftRailView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], stableTick func()) core.Node {
	return core.View{
		TestID: "hud-left-rail",
		Role:   core.RoleRegion,
		Label:  "Screens and nodes",
		Style: core.Style{
			Width:       core.Pt(232),
			FlexShrink:  0,
			Background:  specBg,
			Borders:     core.Edges{Right: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			// SCREENS header.
			core.View{
				Style: core.Style{
					Height:     core.Pt(36),
					FlexShrink: 0,
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Padding:    core.Symmetric(0, 14),
				},
				Children: []core.Node{
					core.Text{
						Content: "SCREENS",
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         specTextMuted,
						},
					},
				},
			},
			// SCREENS list.
			core.Dynamic(func(cx *core.Scope) core.Node {
				stableTick()
				screens := ed.HUDScreens()
				activePath := ed.HUDActiveScreenPath()
				if len(screens) == 0 {
					return core.View{
						Style: core.Style{Padding: core.Symmetric(4, 14)},
						Children: []core.Node{
							core.Text{
								TestID:  "hud-screens-empty",
								Content: "No screens found",
								Style:   core.Style{FontSize: 12, Color: specTextMuted},
							},
						},
					}
				}
				rows := make([]core.Node, 0, len(screens))
				seenName := make(map[string]int, len(screens))
				for _, sc := range screens {
					entry := sc
					seenName[entry.Name]++
					idSuffix := ""
					if seenName[entry.Name] > 1 {
						idSuffix = fmt.Sprintf("-%d", seenName[entry.Name])
					}
					on := entry.Path == activePath
					meta := fmt.Sprintf("%d nodes", entry.NodeCount)
					metaColor := specTextMuted
					if entry.LoadErr != nil {
						meta = "error"
						metaColor = specProblem
					} else if entry.ProblemCount > 0 {
						noun := "problems"
						if entry.ProblemCount == 1 {
							noun = "problem"
						}
						meta = fmt.Sprintf("%d %s", entry.ProblemCount, noun)
						if on {
							metaColor = specProblem
						}
					}
					bg := core.Transparent
					fg := specTextSecondary
					if on {
						bg = specSelectedRowFill
						fg = specTextStrong
					}
					rows = append(rows, core.View{
						TestID: "hud-screen-" + entry.Name + idSuffix,
						Role:   core.RoleButton,
						Label:  "Screen " + entry.Name + " " + meta,
						Style: core.Style{
							Height:       core.Pt(28),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							Padding:      core.Symmetric(0, 8),
							CornerRadius: 6,
							Background:   bg,
							Hover:        &core.Style{Background: specRaised},
							FocusVisible: specRowFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							_ = ed.OpenHUDScreen(entry.Path)
						},
						Children: []core.Node{
							core.Text{
								Content: entry.Name,
								Style: core.Style{
									FlexGrow: 1,
									FontSize: 12.5,
									Color:    fg,
								},
							},
							core.Text{
								TestID:  "hud-screen-meta-" + entry.Name + idSuffix,
								Content: meta,
								Style: core.Style{
									FontSize: 11,
									Color:    metaColor,
								},
							},
						},
					})
				}
				return core.View{
					TestID:   "hud-screens-list",
					Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
					Children: rows,
				}
			}),
			// NODES header.
			core.View{
				Style: core.Style{
					Height:         core.Pt(36),
					FlexShrink:     0,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Edges{Top: 8, Left: 14, Right: 14},
				},
				Children: []core.Node{
					core.Text{
						Content: "NODES",
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         specTextMuted,
						},
					},
					core.Text{
						TestID: "hud-nodes-filename",
						ContentFunc: func() string {
							_ = rev.Get()
							p := ed.HUDActiveScreenPath()
							if p == "" {
								return ""
							}
							return screenFileName(p, "")
						},
						Style: core.Style{
							FontSize:   11,
							FontWeight: core.FontWeightMedium,
							Color:      specTextMuted,
						},
					},
				},
			},
			// NODES scrollable tree.
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						stableTick()
						tree := ed.HUDTree()
						selID := ed.HUDSelectedNodeID()
						rows := make([]core.Node, 0, len(tree))
						for _, row := range tree {
							r := row
							on := r.ID != "" && r.ID == selID
							bg := core.Transparent
							fg := specTextSecondary
							if on {
								bg = specSelectedRowFill
								fg = specTextStrong
							}
							rowChildren := []core.Node{
								core.View{
									Style: core.Style{
										Width:      core.Pt(core.Points(r.Depth * 14)),
										FlexShrink: 0,
									},
								},
								core.Text{
									Content: r.Name,
									Style: core.Style{
										FlexGrow: 1,
										FontSize: 12.5,
										Color:    fg,
									},
								},
							}
							if r.HasProblem {
								rowChildren = append(rowChildren, core.View{
									TestID: "hud-tree-problem-" + r.ID,
									Label:  "has a problem",
									Style: core.Style{
										Width:        core.Pt(6),
										Height:       core.Pt(6),
										CornerRadius: 3,
										Background:   specProblem,
										FlexShrink:   0,
									},
								})
							}
							if r.AnchorTag != "" {
								rowChildren = append(rowChildren, core.Text{
									Content: r.AnchorTag,
									Style: core.Style{
										FontFamily: core.FontMonospace,
										FontSize:   11,
										Color:      specTextMuted,
										FlexShrink: 0,
									},
								})
							}
							testID := "hud-tree-root"
							label := "Screen " + r.Name
							if r.ID != "" {
								testID = "hud-tree-" + r.ID
								label = "Node " + r.Name
							}
							rows = append(rows, core.View{
								TestID: testID,
								Role:   core.RoleButton,
								Label:  label,
								Style: core.Style{
									Height:       core.Pt(28),
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          8,
									Padding:      core.Symmetric(0, 8),
									CornerRadius: 6,
									Background:   bg,
									Hover:        &core.Style{Background: specRaised},
									FocusVisible: specRowFocusRingStyle,
								},
								OnPress: func(core.PressEvent) {
									if r.ID != "" {
										ed.SelectHUDNode(r.ID)
									}
								},
								Children: rowChildren,
							})
						}
						return core.View{
							TestID:   "hud-nodes-tree",
							Style:    core.Style{Padding: core.Symmetric(0, 6), Gap: 2},
							Children: rows,
						}
					}),
				},
			},
			// Footer hint.
			core.View{
				TestID: "hud-rail-footer",
				Style: core.Style{
					FlexShrink:  0,
					Padding:     core.Symmetric(12, 14),
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.Text{
						Content: "By hand: order inside a container, spacing, and dragging touch controls. The agent builds the rest.",
						Style: core.Style{
							FontSize:   12,
							LineHeight: 18,
							Color:      specTextMuted,
						},
					},
				},
			},
		},
	}
}

// hudCenterViewportView builds the center column: sub-header with 4 overlay switches, the 4-device canvas, and the TEXT WIDTH, PX table.
func hudCenterViewportView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], stableTick func(), canvasW, canvasH *core.Signal[float32]) core.Node {
	type hudStageMode struct {
		kind   string
		path   string
		errMsg string
	}
	stageMode := core.NewMemo(cx, func() hudStageMode {
		_ = rev.Get()
		if loadErr := ed.HUDLoadErr(); loadErr != nil {
			return hudStageMode{kind: "error", path: ed.HUDActiveScreenPath(), errMsg: loadErr.Error()}
		}
		if rec := ed.activeHUDScreenRecord(); rec == nil || rec.doc == nil {
			return hudStageMode{kind: "empty"}
		}
		return hudStageMode{kind: "ok"}
	})
	overlaySwitch := func(key, label string, activeFn func(HUDOverlays) bool, swatch core.Node) core.Node {
		return core.View{
			TestID: "hud-overlay-" + key,
			Role:   core.RoleSwitch,
			Label:  label,
			Checked: func() bool {
				_ = rev.Get()
				return activeFn(ed.HUDOverlays())
			},
			StyleFunc: func() core.Style {
				_ = rev.Get()
				on := activeFn(ed.HUDOverlays())
				bg := core.Transparent
				border := core.Transparent
				if on {
					bg = specRaised
					border = specLineRaised
				}
				return core.Style{
					Height:       core.Pt(28),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 10),
					CornerRadius: 8,
					Background:   bg,
					BorderWidth:  1,
					BorderColor:  border,
					Hover:        &core.Style{Background: specRaised},
					FocusVisible: specFocusRingStyle,
				}
			},
			OnPress: func(core.PressEvent) {
				ed.ToggleHUDOverlay(key)
			},
			Children: []core.Node{
				swatch,
				core.Text{
					Content: label,
					StyleFunc: func() core.Style {
						_ = rev.Get()
						col := specTextMuted
						if activeFn(ed.HUDOverlays()) {
							col = specTextStrong
						}
						return core.Style{
							FontSize: 12.5,
							Color:    col,
						}
					},
				},
			},
		}
	}

	return core.View{
		TestID: "hud-center",
		Role:   core.RoleRegion,
		Label:  "All screen sizes preview",
		Style: core.Style{
			FlexGrow:   1,
			FlexBasis:  core.Pt(0),
			Background: specPanel,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			// Sub-header bar: "All sizes", design size caption, 4 overlay switches.
			core.View{
				TestID: "hud-viewport-header",
				Style: core.Style{
					Height:      core.Pt(44),
					FlexShrink:  0,
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         12,
					Padding:     core.Symmetric(0, 16),
					Background:  specBg,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.Text{
						Content: "All sizes",
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      specTextStrong,
						},
					},
					core.Text{
						TestID: "hud-design-caption",
						ContentFunc: func() string {
							_ = rev.Get()
							doc := ed.HUDActiveScreen()
							w, h := float32(640), float32(360)
							if doc != nil && doc.DesignWidth > 0 && doc.DesignHeight > 0 {
								w, h = doc.DesignWidth, doc.DesignHeight
							}
							return fmt.Sprintf("designed at %s × %s · scaled to fit height", formatHUDPoints(w), formatHUDPoints(h))
						},
						Style: core.Style{
							FontSize: 12.5,
							Color:    specTextMuted,
						},
					},
					core.View{Style: core.Style{FlexGrow: 1}},
					overlaySwitch("safe", "Safe areas", func(o HUDOverlays) bool { return o.SafeAreas }, core.View{
						Style: core.Style{
							Width:        core.Pt(10),
							Height:       core.Pt(10),
							CornerRadius: 2,
							Background:   specSafeStripFill,
							BorderWidth:  1,
							BorderColor:  specWarning,
						},
					}),
					overlaySwitch("anchors", "Anchors", func(o HUDOverlays) bool { return o.Anchors }, core.View{
						Style: core.Style{
							Width:        core.Pt(8),
							Height:       core.Pt(8),
							CornerRadius: 2,
							Background:   specAccent,
						},
					}),
					overlaySwitch("touch", "Touch areas", func(o HUDOverlays) bool { return o.TouchAreas }, core.View{
						Style: core.Style{
							Width:        core.Pt(10),
							Height:       core.Pt(10),
							CornerRadius: 5,
							BorderWidth:  1.5,
							BorderColor:  specAccent,
						},
					}),
					overlaySwitch("focus", "Focus order", func(o HUDOverlays) bool { return o.FocusOrder }, core.View{
						Style: core.Style{
							Width:        core.Pt(10),
							Height:       core.Pt(10),
							CornerRadius: 5,
							Background:   specAccent,
						},
					}),
				},
			},
			// Interactive multi-size canvas + floating TEXT WIDTH, PX table.
			core.View{
				TestID: "hud-viewport-stage",
				Style: core.Style{
					FlexGrow:   1,
					FlexBasis:  core.Pt(0),
					Position:   core.Relative,
					Background: specPanel,
					Overflow:   core.Hidden,
				},
				OnLayout: func(b core.Bounds) {
					w, h := float32(b.Size.Width), float32(b.Size.Height)
					if w > 0 && h > 0 {
						canvasW.Set(w)
						canvasH.Set(h)
						ed.hud.canvasW = w
						ed.hud.canvasH = h
					}
				},
				OnMouseDown: func(ev core.MouseButtonEvent) {
					if ev.Button != core.MouseButtonLeft {
						return
					}
					x, y := float32(ev.Position.X), float32(ev.Position.Y)
					w, h := canvasW.Get(), canvasH.Get()
					if ed.BeginHUDTouchDrag(x, y, w, h) {
						return
					}
					if hit, _ := ed.HitTestHUDCanvas(x, y, w, h); hit != "" {
						ed.SelectHUDNode(hit)
					}
				},
				OnMouseMove: func(ev core.MouseEvent) {
					if !ed.HUDTouchDragActive() {
						return
					}
					if ev.PressedButton != core.MouseButtonLeft {
						ed.report(ed.EndHUDTouchDrag())
						return
					}
					ed.UpdateHUDTouchDrag(float32(ev.Position.X), float32(ev.Position.Y))
				},
				OnMouseUp: func(ev core.MouseButtonEvent) {
					if ev.Button == core.MouseButtonLeft && ed.HUDTouchDragActive() {
						ed.report(ed.EndHUDTouchDrag())
					}
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						m := stageMode.Get()
						if m.kind == "error" {
							if loadErr := ed.HUDLoadErr(); loadErr != nil {
								return hudErrorStateView(m.path, loadErr)
							}
						}
						if m.kind == "empty" {
							return hudEmptyStateView()
						}
						return core.Canvas{
							TestID: "hud-multisize-canvas",
							Label:  "HUD preview at Phone, Tablet, Desktop, and Ultrawide sizes",
							Style: core.Style{
								Width:  core.Pct(100),
								Height: core.Pct(100),
							},
							Draw: func(d *core.Drawing, size core.Size) {
								_ = rev.Get()
								drawHUDMultiSizeCanvas(d, size, ed)
							},
						}
					}),
					hudTextWidthTableView(cx, ed, stableTick),
				},
			},
		},
	}
}

func hudEmptyStateView() core.Node {
	return core.View{
		TestID: "hud-empty-state",
		Style: core.Style{
			Width:          core.Pct(100),
			Height:         core.Pct(100),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(360),
					Padding:      core.All(16),
					Gap:          8,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: "No HUD or menu screens yet",
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      specTextStrong,
						},
					},
					core.Text{
						Content: "Add a *.screen.yaml file under screens/ or ask the agent to build a HUD or menu screen.",
						Style: core.Style{
							FontSize:   12.5,
							LineHeight: 18,
							Color:      specTextSecondary,
						},
					},
				},
			},
		},
	}
}

func hudErrorStateView(path string, err error) core.Node {
	return core.View{
		TestID: "hud-error-state",
		Style: core.Style{
			Width:          core.Pct(100),
			Height:         core.Pct(100),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.All(24),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(440),
					Padding:      core.All(16),
					Gap:          8,
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specProblem,
				},
				Children: []core.Node{
					core.Text{
						Content: "Could not open " + screenFileName(path, ""),
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      specTextStrong,
						},
					},
					core.Text{
						TestID:  "hud-error-message",
						Content: err.Error(),
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							LineHeight: 17,
							Color:      specProblem,
						},
					},
				},
			},
		},
	}
}

// hudTextWidthTableView renders the floating "TEXT WIDTH, PX" table at the bottom-right of the viewport stage.
func hudTextWidthTableView(cx *core.Scope, ed *Editor, stableTick func()) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		stableTick()
		rows := ed.HUDTextWidths()
		if len(rows) == 0 {
			return nil
		}
		locales := ed.HUDLocales()
		selID := ed.HUDSelectedNodeID()

		headerCells := []core.Node{
			core.Text{
				Content: "TEXT WIDTH, PX",
				Style: core.Style{
					FlexGrow:      1,
					FontSize:      11,
					FontWeight:    core.FontWeightSemibold,
					LetterSpacing: 0.66,
					Color:         specTextMuted,
				},
			},
			core.Text{
				Content: "BOX",
				Style: core.Style{
					Width:         core.Pt(40),
					TextAlign:     core.TextAlignEnd,
					FontSize:      11,
					FontWeight:    core.FontWeightSemibold,
					LetterSpacing: 0.66,
					Color:         specTextMuted,
				},
			},
		}
		for _, loc := range locales {
			headerCells = append(headerCells, core.Text{
				Content: hudLocaleShortHeader(loc),
				Style: core.Style{
					Width:         core.Pt(52),
					TextAlign:     core.TextAlignEnd,
					FontSize:      11,
					FontWeight:    core.FontWeightSemibold,
					LetterSpacing: 0.66,
					Color:         specTextMuted,
				},
			})
		}

		tableRows := make([]core.Node, 0, len(rows)+1)
		tableRows = append(tableRows, core.View{
			Style: core.Style{
				Height:     core.Pt(32),
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
				Padding:    core.Symmetric(0, 12),
			},
			Children: headerCells,
		})

		for _, r := range rows {
			row := r
			on := row.NodeID == selID
			bg := core.Transparent
			nameColor := specText
			if on {
				bg = specSelectedRowFill
				nameColor = specTextStrong
			}
			cells := []core.Node{
				core.Text{
					Content: row.NodeID,
					Style: core.Style{
						FlexGrow: 1,
						FontSize: 12,
						Color:    nameColor,
					},
				},
				core.Text{
					TestID:  "hud-text-width-box-" + row.NodeID,
					Content: row.BoxLabel,
					Style: core.Style{
						Width:      core.Pt(40),
						TextAlign:  core.TextAlignEnd,
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      specTextMuted,
					},
				},
			}
			for _, loc := range locales {
				val := formatHUDPoints(row.Widths[loc])
				col := specTextSecondary
				if row.Overflow[loc] > 0 {
					col = specProblem
				}
				cells = append(cells, core.Text{
					TestID:  "hud-text-width-" + row.NodeID + "-" + loc,
					Content: val,
					Style: core.Style{
						Width:      core.Pt(52),
						TextAlign:  core.TextAlignEnd,
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      col,
					},
				})
			}
			tableRows = append(tableRows, core.View{
				TestID: "hud-text-width-row-" + row.NodeID,
				Role:   core.RoleButton,
				Label:  "Text width " + row.NodeID,
				Style: core.Style{
					Height:       core.Pt(28),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 12),
					Background:   bg,
					Hover:        &core.Style{Background: specRaised},
					FocusVisible: specRowFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					ed.SelectHUDNode(row.NodeID)
				},
				Children: cells,
			})
		}

		cardW := core.Points(220 + len(locales)*60)
		if cardW < 328 {
			cardW = 328
		}
		return core.View{
			TestID: "hud-text-width-table",
			Role:   core.RoleRegion,
			Label:  "Text width in pixels",
			Style: core.Style{
				Position:     core.Absolute,
				Right:        core.Pt(24),
				Bottom:       core.Pt(24),
				Width:        core.Pt(cardW),
				Padding:      core.Edges{Bottom: 8},
				CornerRadius: 12,
				Background:   specPanel,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
			},
			Children: tableRows,
		}
	})
}

// hudInspectorRailView builds the 360 px right rail: node header, ANCHOR 3×3 + fill, SIZE/Spacing/Order,
// TEXT + zoomed overflow detail, GAME CHECK list + Ask agent button, and bottom composer.
func hudInspectorRailView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], stableTick func(), promptDraft *core.Signal[string]) core.Node {
	return core.View{
		TestID: "hud-inspector-rail",
		Role:   core.RoleRegion,
		Label:  "HUD node inspector",
		Style: core.Style{
			Width:       core.Pt(360),
			FlexShrink:  0,
			Background:  specPanel,
			Borders:     core.Edges{Left: 1},
			BorderColor: specLine,
			Overflow:    core.Hidden,
		},
		Children: []core.Node{
			// Selected node header.
			core.View{
				Style: core.Style{
					Height:      core.Pt(44),
					FlexShrink:  0,
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(0, 16),
					Borders:     core.Edges{Bottom: 1},
					BorderColor: specLine,
				},
				Children: []core.Node{
					core.Text{
						TestID: "hud-inspector-node-name",
						ContentFunc: func() string {
							_ = rev.Get()
							doc := ed.HUDActiveScreen()
							if doc == nil {
								return "No screen selected"
							}
							n := findHUDNode(doc.Nodes, ed.HUDSelectedNodeID())
							if n == nil {
								return screenDisplayName(ed.HUDActiveScreenPath(), doc)
							}
							return n.ID
						},
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      specTextStrong,
						},
					},
					core.Text{
						TestID: "hud-inspector-node-type",
						ContentFunc: func() string {
							_ = rev.Get()
							doc := ed.HUDActiveScreen()
							if doc == nil {
								return ""
							}
							n := findHUDNode(doc.Nodes, ed.HUDSelectedNodeID())
							return hudNodeTypeLabel(n)
						},
						Style: core.Style{
							FontSize: 12.5,
							Color:    specTextMuted,
						},
					},
				},
			},
			// Scrollable inspector body.
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						stableTick()
						doc := ed.HUDActiveScreen()
						if doc == nil {
							return core.View{
								Style: core.Style{Padding: core.All(16)},
								Children: []core.Node{
									core.Text{
										Content: "Select or open a screen file to inspect anchors, localized text width, and GAME CHECK diagnostics.",
										Style:   core.Style{FontSize: 12.5, LineHeight: 18, Color: specTextMuted},
									},
								},
							}
						}
						sel := findHUDNode(doc.Nodes, ed.HUDSelectedNodeID())
						if sel == nil && len(doc.Nodes) > 0 {
							sel = &doc.Nodes[0]
						}
						sections := make([]core.Node, 0, 5)
						if sel != nil {
							sections = append(sections,
								hudAnchorSectionView(ed, sel),
								hudSizeAndLayoutSectionView(ed, doc, sel),
							)
							if sel.TextKey != "" {
								sections = append(sections, hudTextSectionView(ed, sel))
							}
						}
						sections = append(sections, hudGameCheckSectionView(ed, doc))
						return core.View{
							Style: core.Style{
								Padding: core.All(16),
								Gap:     16,
							},
							Children: sections,
						}
					}),
				},
			},
			// Bottom agent composer.
			hudComposerView(cx, ed, promptDraft),
		},
	}
}

func hudAnchorSectionView(ed *Editor, sel *gameui.ScreenNodeDoc) core.Node {
	selAnchor := sel.EffectiveAnchor()
	safeOn := sel.KeepInSafeArea()

	gridAnchors := [3][3]gameui.ScreenAnchor{
		{gameui.AnchorTopLeft, gameui.AnchorTopCenter, gameui.AnchorTopRight},
		{gameui.AnchorCenterLeft, gameui.AnchorCenter, gameui.AnchorCenterRight},
		{gameui.AnchorBottomLeft, gameui.AnchorBottomCenter, gameui.AnchorBottomRight},
	}

	gridRows := make([]core.Node, 0, 3)
	for r := range 3 {
		rowBtns := make([]core.Node, 0, 3)
		for c := range 3 {
			a := gridAnchors[r][c]
			on := selAnchor == a
			bg := core.Transparent
			dotSize := core.Points(6)
			dotCol := specTextMuted
			if on {
				bg = specSelectedRowFill
				dotSize = 10
				dotCol = specAccent
			}
			rowBtns = append(rowBtns, core.View{
				TestID: "hud-anchor-" + string(a),
				Role:   core.RoleButton,
				Label:  "Anchor " + strings.ToLower(hudAnchorWords(a)),
				Style: core.Style{
					Width:          core.Pt(28),
					Height:         core.Pt(28),
					CornerRadius:   6,
					Background:     bg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Hover:          &core.Style{Background: specLineRaised},
					FocusVisible:   specFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.SetHUDNodeAnchor(sel.ID, a))
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(dotSize),
							Height:       core.Pt(dotSize),
							CornerRadius: 2,
							Background:   dotCol,
						},
					},
				},
			})
		}
		gridRows = append(gridRows, core.View{
			Style:    core.Style{Direction: core.Row, Gap: 4},
			Children: rowBtns,
		})
	}

	fillOn := selAnchor == gameui.AnchorFill
	fillBg := specRaised
	fillFg := specTextSecondary
	fillBorder := specLineRaised
	if fillOn {
		fillBg = specSelectedRowFill
		fillFg = specTextStrong
		fillBorder = specAccent
	}

	anchorText := hudAnchorWords(selAnchor)
	if selAnchor != gameui.AnchorFill {
		if safeOn {
			anchorText += " of the safe area"
		} else {
			anchorText += " of the screen"
		}
	}

	offsetText := "no offset"
	if selAnchor != gameui.AnchorFill {
		var ox, oy float32
		if sel.Offset != nil {
			ox, oy = sel.Offset.X, sel.Offset.Y
		}
		offsetText = fmt.Sprintf("x %s · y %s from the anchor", formatHUDPoints(ox), formatHUDPoints(oy))
	}

	checkBg := core.Transparent
	checkBorder := specTextMuted
	var checkChildren []core.Node
	if safeOn {
		checkBg = specAccent
		checkBorder = specAccent
		checkChildren = []core.Node{
			core.SVGView{
				SVG:   lucide.Check(),
				Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specBg},
			},
		}
	}

	return core.View{
		TestID: "hud-section-anchor",
		Style: core.Style{
			Direction: core.Row,
			Gap:       16,
		},
		Children: []core.Node{
			// 3×3 anchor grid + Fill button.
			core.View{
				Style: core.Style{
					FlexShrink: 0,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Padding:      core.All(4),
							Gap:          4,
							CornerRadius: 8,
							Background:   specRaised,
							BorderWidth:  1,
							BorderColor:  specLineRaised,
						},
						Children: gridRows,
					},
					core.View{
						TestID: "hud-anchor-fill",
						Role:   core.RoleButton,
						Label:  "Anchor fill",
						Style: core.Style{
							Height:         core.Pt(28),
							CornerRadius:   6,
							Background:     fillBg,
							BorderWidth:    1,
							BorderColor:    fillBorder,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Hover:          &core.Style{Background: specLineRaised},
							FocusVisible:   specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.SetHUDNodeAnchor(sel.ID, gameui.AnchorFill))
						},
						Children: []core.Node{
							core.Text{
								Content: "Fill",
								Style: core.Style{
									FontSize:   12,
									FontWeight: core.FontWeightMedium,
									Color:      fillFg,
								},
							},
						},
					},
				},
			},
			// Anchor summary, offset readout, and safe area checkbox.
			core.View{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       8,
				},
				Children: []core.Node{
					core.Text{
						Content: "ANCHOR",
						Style: core.Style{
							FontSize:      11,
							FontWeight:    core.FontWeightSemibold,
							LetterSpacing: 0.66,
							Color:         specTextMuted,
						},
					},
					core.Text{
						TestID:  "hud-anchor-words",
						Content: anchorText,
						Style:   core.Style{FontSize: 12.5, Color: specTextStrong},
					},
					core.Text{
						TestID:  "hud-offset-words",
						Content: offsetText,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      specTextSecondary,
						},
					},
					core.View{
						TestID:  "hud-safe-area-toggle",
						Role:    core.RoleCheckbox,
						Label:   "Keep inside the safe area",
						Checked: func() bool { return safeOn },
						Style: core.Style{
							MinHeight:    core.Pt(28),
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
							CornerRadius: 6,
							FocusVisible: specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							ed.report(ed.SetHUDNodeSafeArea(sel.ID, !safeOn))
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:          core.Pt(16),
									Height:         core.Pt(16),
									CornerRadius:   4,
									Background:     checkBg,
									BorderWidth:    1.5,
									BorderColor:    checkBorder,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FlexShrink:     0,
								},
								Children: checkChildren,
							},
							core.Text{
								Content: "Keep inside the safe area",
								Style:   core.Style{FontSize: 12.5, Color: specText},
							},
						},
					},
				},
			},
		},
	}
}

func hudSizeAndLayoutSectionView(ed *Editor, doc *gameui.ScreenDoc, sel *gameui.ScreenNodeDoc) core.Node {
	isFit := sel.SizeMode == gameui.SizeFitText || (sel.TextKey != "" && sel.Width <= 0)
	canFit := sel.TextKey != "" && (sel.Type == gameui.NodeLabel || sel.Type == gameui.NodeButton)

	sizeSummary := ""
	switch {
	case sel.Anchor == gameui.AnchorFill:
		sizeSummary = "fills the screen"
	case sel.Type.IsContainer() && sel.Width == 0 && sel.Height == 0:
		sizeSummary = "sized by its children"
	default:
		wStr := formatHUDPoints(sel.Width)
		if sel.TextKey != "" && isFit {
			needed, _ := ed.hudMeasureNodeText(sel, ed.HUDLocale())
			wStr = "fits text · " + formatHUDPoints(needed)
		}
		hStr := formatHUDPoints(sel.Height)
		sizeSummary = wStr + " × " + hStr
		if sel.TouchHitSize > 0 || sel.Type == gameui.NodeTouchButton || sel.Type == gameui.NodeTouchStick {
			sizeSummary += " · touch " + formatHUDPoints(effectiveTouchHitSize(sel)) + " pt"
		}
	}

	widthRowChildren := []core.Node{
		core.Text{
			Content: "Width",
			Style:   core.Style{Width: core.Pt(52), FontSize: 12.5, Color: specTextSecondary},
		},
	}
	if canFit {
		fixedLabel := "Fixed"
		if sel.Width > 0 {
			fixedLabel = "Fixed " + formatHUDPoints(sel.Width)
		}
		segBtn := func(testID, text string, active bool, fn func()) core.Node {
			bg := core.Transparent
			fg := specTextMuted
			if active {
				bg = specLineRaised
				fg = specTextStrong
			}
			return core.View{
				TestID: testID,
				Role:   core.RoleButton,
				Label:  text,
				Style: core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   specFocusRingStyle,
				},
				OnPress: func(core.PressEvent) { fn() },
				Children: []core.Node{
					core.Text{
						Content: text,
						Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightMedium, Color: fg},
					},
				},
			}
		}
		widthRowChildren = append(widthRowChildren, core.View{
			Style: core.Style{
				Direction:    core.Row,
				Gap:          2,
				Padding:      core.All(3),
				CornerRadius: 8,
				Background:   specRaised,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
			},
			Children: []core.Node{
				segBtn("hud-size-fixed", fixedLabel, !isFit, func() {
					ed.report(ed.SetHUDNodeSizeMode(sel.ID, gameui.SizeFixed))
				}),
				segBtn("hud-size-fit", "Fit text", isFit, func() {
					ed.report(ed.SetHUDNodeSizeMode(sel.ID, gameui.SizeFitText))
				}),
			},
		})
	}
	widthRowChildren = append(widthRowChildren, core.Text{
		TestID:  "hud-size-summary",
		Content: sizeSummary,
		Style: core.Style{
			FontFamily: core.FontMonospace,
			FontSize:   11.5,
			Color:      specTextSecondary,
		},
	})

	rows := []core.Node{
		core.Text{
			Content: "SIZE",
			Style: core.Style{
				FontSize:      11,
				FontWeight:    core.FontWeightSemibold,
				LetterSpacing: 0.66,
				Color:         specTextMuted,
			},
		},
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Wrap:       core.Wrap,
				Gap:        8,
			},
			Children: widthRowChildren,
		},
	}

	// Manual container spacing control for vbox / hbox / sprite_row.
	if sel.Type == gameui.NodeVBox || sel.Type == gameui.NodeHBox || sel.Type == gameui.NodeSpriteRow {
		rows = append(rows, core.View{
			TestID: "hud-spacing-row",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Text{
					Content: "Spacing",
					Style:   core.Style{Width: core.Pt(52), FontSize: 12.5, Color: specTextSecondary},
				},
				hudSmallIconBtn("hud-spacing-dec", "Decrease spacing", lucide.Minus(), sel.Spacing <= 0, func() {
					ed.report(ed.SetHUDNodeSpacing(sel.ID, max(float32(0), sel.Spacing-2)))
				}),
				core.Text{
					TestID:  "hud-spacing-value",
					Content: formatHUDPoints(sel.Spacing) + " px",
					Style: core.Style{
						Width:      core.Pt(48),
						TextAlign:  core.TextAlignCenter,
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      specText,
					},
				},
				hudSmallIconBtn("hud-spacing-inc", "Increase spacing", lucide.Plus(), false, func() {
					ed.report(ed.SetHUDNodeSpacing(sel.ID, sel.Spacing+2))
				}),
			},
		})
	}

	// Manual container order control when the node has siblings.
	if slot, idx := findHUDNodeSlot(&doc.Nodes, sel.ID); slot != nil && len(*slot) > 1 {
		total := len(*slot)
		rows = append(rows, core.View{
			TestID: "hud-order-row",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Text{
					Content: "Order",
					Style:   core.Style{Width: core.Pt(52), FontSize: 12.5, Color: specTextSecondary},
				},
				hudSmallIconBtn("hud-order-up", "Move earlier in container", lucide.ChevronUp(), idx <= 0, func() {
					ed.report(ed.ReorderHUDNode(sel.ID, -1))
				}),
				hudSmallIconBtn("hud-order-down", "Move later in container", lucide.ChevronDown(), idx >= total-1, func() {
					ed.report(ed.ReorderHUDNode(sel.ID, 1))
				}),
				core.Text{
					TestID:  "hud-order-value",
					Content: fmt.Sprintf("%d of %d", idx+1, total),
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      specTextMuted,
					},
				},
			},
		})
	}

	return core.View{
		TestID:   "hud-section-size",
		Style:    core.Style{Gap: 8},
		Children: rows,
	}
}

func hudSmallIconBtn(testID, label string, icon *core.SVG, disabled bool, onPress func()) core.Node {
	fg := specText
	if disabled {
		fg = specTextMuted
	}
	return core.View{
		TestID:   testID,
		Role:     core.RoleButton,
		Label:    label,
		Disabled: func() bool { return disabled },
		Style: core.Style{
			Width:          core.Pt(28),
			Height:         core.Pt(28),
			CornerRadius:   6,
			Background:     specRaised,
			BorderWidth:    1,
			BorderColor:    specLineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Hover:          &core.Style{Background: specLineRaised},
			FocusVisible:   specFocusRingStyle,
		},
		OnPress: func(core.PressEvent) {
			if !disabled {
				onPress()
			}
		},
		Children: []core.Node{
			core.SVGView{
				SVG:   icon,
				Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: fg},
			},
		},
	}
}

func hudTextSectionView(ed *Editor, sel *gameui.ScreenNodeDoc) core.Node {
	locales := ed.HUDLocales()
	activeLoc := ed.HUDLocale()

	localeRows := make([]core.Node, 0, len(locales))
	zoomLoc := ""
	var zoomOver float32

	// Check active locale first for zoomed overflow detail, then any other locale.
	if _, over := ed.hudMeasureNodeText(sel, activeLoc); over > 0 {
		zoomLoc = activeLoc
		zoomOver = over
	}
	for _, loc := range locales {
		val, ok := ed.hudLookupText(loc, sel.TextKey)
		valColor := specTextStrong
		if !ok {
			val = "(missing)"
			valColor = specProblem
		}
		if zoomLoc == "" {
			if _, over := ed.hudMeasureNodeText(sel, loc); over > 0 {
				zoomLoc = loc
				zoomOver = over
			}
		}
		localeRows = append(localeRows, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignStart,
				Gap:        12,
			},
			Children: []core.Node{
				core.Text{
					Content: loc,
					Style: core.Style{
						Width:      core.Pt(52),
						FlexShrink: 0,
						FontSize:   12.5,
						LineHeight: 19,
						Color:      specTextMuted,
					},
				},
				core.Text{
					TestID:  "hud-text-val-" + loc,
					Content: val,
					Style: core.Style{
						FlexGrow:   1,
						FontSize:   12.5,
						LineHeight: 19,
						Color:      valColor,
					},
				},
			},
		})
	}

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				JustifyContent: core.JustifySpaceBetween,
				AlignItems:     core.AlignCenter,
			},
			Children: []core.Node{
				core.Text{
					Content: "TEXT",
					Style: core.Style{
						FontSize:      11,
						FontWeight:    core.FontWeightSemibold,
						LetterSpacing: 0.66,
						Color:         specTextMuted,
					},
				},
				core.Text{
					TestID:  "hud-text-key",
					Content: sel.TextKey,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11,
						Color:      specTextMuted,
					},
				},
			},
		},
		core.View{
			Style:    core.Style{Gap: 4},
			Children: localeRows,
		},
	}

	if zoomLoc != "" && zoomOver > 0 {
		zoomText, _ := ed.hudLookupText(zoomLoc, sel.TextKey)
		nodeCopy := *sel
		overCopy := zoomOver
		children = append(children, core.View{
			TestID: "hud-zoom-detail",
			Style: core.Style{
				CornerRadius: 12,
				Background:   specRaised,
				BorderWidth:  1,
				BorderColor:  specLineRaised,
				Padding:      core.All(12),
				Gap:          8,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						JustifyContent: core.JustifySpaceBetween,
						AlignItems:     core.AlignCenter,
					},
					Children: []core.Node{
						core.Text{
							TestID:  "hud-zoom-title",
							Content: fmt.Sprintf("Phone · %s · 1.5×", hudLocaleLabel(zoomLoc)),
							Style:   core.Style{FontSize: 11.5, Color: specTextMuted},
						},
						core.Text{
							TestID:  "hud-zoom-over",
							Content: fmt.Sprintf("%s px past the box", formatHUDPoints(overCopy)),
							Style:   core.Style{FontSize: 11.5, Color: specProblem},
						},
					},
				},
				core.Canvas{
					TestID: "hud-zoom-canvas",
					Label:  fmt.Sprintf("Zoomed 1.5x preview of %s overflowing by %s px", sel.ID, formatHUDPoints(overCopy)),
					Style: core.Style{
						Height:       core.Pt(48),
						Width:        core.Pct(100),
						CornerRadius: 8,
					},
					Draw: func(d *core.Drawing, size core.Size) {
						drawHUDZoomOverflow(d, size, &nodeCopy, zoomText, overCopy)
					},
				},
			},
		})
	}

	return core.View{
		TestID:   "hud-section-text",
		Style:    core.Style{Gap: 8},
		Children: children,
	}
}

func hudGameCheckSectionView(ed *Editor, doc *gameui.ScreenDoc) core.Node {
	screenName := strings.ToUpper(screenDisplayName(ed.HUDActiveScreenPath(), doc))
	probs := ed.HUDProblems()
	selID := ed.HUDSelectedNodeID()

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				JustifyContent: core.JustifySpaceBetween,
				AlignItems:     core.AlignCenter,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "hud-check-header",
					Content: "GAME CHECK · " + screenName,
					Style: core.Style{
						FontSize:      11,
						FontWeight:    core.FontWeightSemibold,
						LetterSpacing: 0.66,
						Color:         specTextMuted,
					},
				},
				core.Text{
					TestID:  "hud-check-count",
					Content: fmt.Sprintf("%d", len(probs)),
					Style: core.Style{
						FontSize:   11,
						FontWeight: core.FontWeightMedium,
						Color:      specTextMuted,
					},
				},
			},
		},
	}

	if len(probs) == 0 {
		children = append(children, core.View{
			TestID: "hud-check-empty",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   specAdded,
					},
				},
				core.Text{
					Content: "No problems at any size in this language",
					Style:   core.Style{FontSize: 12.5, Color: specTextMuted},
				},
			},
		})
	} else {
		seenProb := make(map[string]int, len(probs))
		for i, p := range probs {
			prob := p
			seenProb[prob.NodeID]++
			probSuffix := ""
			if seenProb[prob.NodeID] > 1 {
				probSuffix = fmt.Sprintf("-%d", seenProb[prob.NodeID])
			}
			selected := prob.NodeID == selID
			cardBg := specRaised
			cardBorder := specLineRaised
			borderW := core.Points(1)
			if selected {
				cardBg = specSelectedRowFill
				cardBorder = specAccent
				borderW = 1.5
			}
			children = append(children, core.View{
				TestID: fmt.Sprintf("hud-problem-%d", i),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignStart,
					Gap:          8,
					Padding:      core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 12},
					CornerRadius: 8,
					Background:   cardBg,
					BorderWidth:  borderW,
					BorderColor:  cardBorder,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   specProblem,
							Margin:       core.Edges{Top: 6},
							FlexShrink:   0,
						},
					},
					core.View{
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							Gap:       2,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "hud-problem-title-" + prob.NodeID + probSuffix,
								Content: prob.Title,
								Style:   core.Style{FontSize: 12.5, Color: specTextStrong},
							},
							core.Text{
								Content: prob.Detail,
								Style:   core.Style{FontSize: 11.5, LineHeight: 16.5, Color: specTextMuted},
							},
						},
					},
					core.View{
						TestID: "hud-problem-show-" + prob.NodeID + probSuffix,
						Role:   core.RoleButton,
						Label:  "Show " + prob.NodeID,
						Style: core.Style{
							Height:         core.Pt(28),
							FlexShrink:     0,
							Padding:        core.Symmetric(0, 10),
							CornerRadius:   6,
							Background:     specLineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Hover:          &core.Style{Background: specRaised},
							FocusVisible:   specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							ed.SelectHUDNode(prob.NodeID)
						},
						Children: []core.Node{
							core.Text{
								Content: "Show",
								Style:   core.Style{FontSize: 12, Color: specText},
							},
						},
					},
				},
			})
		}
		children = append(children, core.View{
			TestID: "hud-ask-agent-fix",
			Role:   core.RoleButton,
			Label:  fmt.Sprintf("Ask the agent to fix %d", len(probs)),
			Style: core.Style{
				Height:         core.Pt(32),
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            8,
				CornerRadius:   8,
				Background:     specAccent,
				FocusVisible:   specFocusRingStyle,
			},
			OnPress: func(core.PressEvent) {
				ed.AskAgentToFixHUDProblems()
			},
			Children: []core.Node{
				core.SVGView{
					SVG:   lucide.Sparkles(),
					Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specBg},
				},
				core.Text{
					Content: fmt.Sprintf("Ask the agent to fix %d", len(probs)),
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: core.FontWeightSemibold,
						Color:      specBg,
					},
				},
			},
		})
	}

	return core.View{
		TestID:   "hud-section-check",
		Style:    core.Style{Gap: 8},
		Children: children,
	}
}

func hudComposerView(cx *core.Scope, ed *Editor, promptDraft *core.Signal[string]) core.Node {
	submit := func() {
		text := strings.TrimSpace(promptDraft.Get())
		if text == "" {
			return
		}
		ed.SubmitHUDPrompt(text)
		promptDraft.Set("")
	}
	return core.View{
		TestID: "hud-composer",
		Style: core.Style{
			FlexShrink:  0,
			Padding:     core.All(12),
			Borders:     core.Edges{Top: 1},
			BorderColor: specLine,
		},
		Children: []core.Node{
			core.View{
				TestID: "hud-composer-card",
				Style: core.Style{
					Height:       core.Pt(40),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Edges{Left: 12, Right: 8},
					CornerRadius: 12,
					Background:   specRaised,
					BorderWidth:  1,
					BorderColor:  specLineRaised,
					FocusWithin:  ed.SpecPalette(cx).FocusRing(2),
				},
				Children: []core.Node{
					core.Input{
						TestID:      "hud-composer-input",
						Label:       "Ask the agent",
						Placeholder: "e.g. add a boss health bar that only shows on floor 3",
						Value:       promptDraft.Get,
						OnFocus:     func() { ed.noteTextInputFocus(true) },
						OnBlur:      func() { ed.noteTextInputFocus(false) },
						OnChange: func(v string) {
							promptDraft.Set(v)
						},
						OnSubmit: func(string) {
							submit()
						},
						Style: core.Style{
							FlexGrow:         1,
							FlexBasis:        core.Pt(0),
							Height:           core.Pt(28),
							FontSize:         12.5,
							Color:            specText,
							PlaceholderColor: specTextMuted,
							Background:       core.Transparent,
							BorderWidth:      0,
						},
					},
					core.View{
						TestID: "hud-composer-send",
						Role:   core.RoleButton,
						Label:  "Send",
						Style: core.Style{
							Width:          core.Pt(28),
							Height:         core.Pt(28),
							FlexShrink:     0,
							CornerRadius:   8,
							Background:     specTextStrong,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   specFocusRingStyle,
						},
						OnPress: func(core.PressEvent) {
							submit()
						},
						Children: []core.Node{
							core.SVGView{
								SVG:   lucide.ArrowUp(),
								Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specBg},
							},
						},
					},
				},
			},
		},
	}
}

// --- Canvas Drawing ---

// drawHUDMultiSizeCanvas draws the dot-grid stage and all four device frames (Phone 19.5:9 with notch,
// Tablet 4:3, Desktop 16:9, Ultrawide 21:9) simultaneously with real gameui layout and diagnostic overlays.
func drawHUDMultiSizeCanvas(d *core.Drawing, size core.Size, ed *Editor) {
	if d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	w, h := float32(size.Width), float32(size.Height)

	// Dot-grid backdrop matching .vp in HUDLayout.dc.html using Spec tokens.
	d.Save()
	d.SetFillColor(specPanel).FillRect(0, 0, w, h)
	d.SetFillColor(specLineRaised)
	for y := float32(8); y < h; y += 16 {
		for x := float32(8); x < w; x += 16 {
			d.FillRect(x, y, 1.5, 1.5)
		}
	}

	doc := ed.HUDActiveScreen()
	if doc == nil {
		d.Restore()
		return
	}
	frames := ed.HUDLayoutFrames(w, h)
	ov := ed.HUDOverlays()
	selID := ed.HUDSelectedNodeID()
	loc := ed.HUDLocale()

	captionFont := core.Font{Size: 11.5}

	for i := range frames {
		fr := &frames[i]
		isPhone := fr.Device.Class == gameui.DevicePhone
		bezelRadius := float32(10)
		screenRadius := float32(4)
		if isPhone {
			bezelRadius = 24
			screenRadius = 16
		}

		// Frame label above bezel.
		d.SetFillColor(specTextSecondary).FillText(fr.Label, fr.X, fr.Y-fr.Bezel-6, captionFont)

		// Device bezel.
		bx, by := fr.X-fr.Bezel, fr.Y-fr.Bezel
		bw, bh := fr.W+fr.Bezel*2, fr.H+fr.Bezel*2
		d.SetFillColor(specBg).BeginPath().RoundRect(bx, by, bw, bh, bezelRadius).Fill()
		d.SetStrokeColor(specLineRaised).SetLineWidth(1).BeginPath().RoundRect(bx, by, bw, bh, bezelRadius).Stroke()

		// Clipped screen region with game backdrop.
		d.Save()
		d.BeginPath().RoundRect(fr.X, fr.Y, fr.W, fr.H, screenRadius).Clip()
		drawHUDGameBackdrop(d, fr.X, fr.Y, fr.W, fr.H, fr.Scale)

		// Safe area overlay (under widgets).
		if ov.SafeAreas && (fr.Device.SafeArea.Left > 0 || fr.Device.SafeArea.Right > 0 || fr.Device.SafeArea.Top > 0 || fr.Device.SafeArea.Bottom > 0) {
			sl := float32(fr.Device.SafeArea.Left) * fr.Scale
			sr := float32(fr.Device.SafeArea.Right) * fr.Scale
			st := float32(fr.Device.SafeArea.Top) * fr.Scale
			sb := float32(fr.Device.SafeArea.Bottom) * fr.Scale

			d.SetFillColor(specSafeStripFill)
			if sl > 0 {
				d.FillRect(fr.X, fr.Y, sl, fr.H)
			}
			if sr > 0 {
				d.FillRect(fr.X+fr.W-sr, fr.Y, sr, fr.H)
			}
			if st > 0 {
				d.FillRect(fr.X, fr.Y, fr.W, st)
			}
			if sb > 0 {
				d.FillRect(fr.X, fr.Y+fr.H-sb, fr.W, sb)
			}
			d.Save()
			d.SetStrokeColor(specSafeDashStroke).SetLineWidth(1).SetLineDash(3, 3)
			d.BeginPath().Rect(fr.X+sl+0.5, fr.Y+st+0.5, max(0, fr.W-sl-sr-1), max(0, fr.H-st-sb-1)).Stroke()
			d.Restore()
		}

		// Render screen nodes inside this device frame.
		type focusPoint struct {
			order int
			x, y  float32
		}
		var focusPts []focusPoint
		type overflowBadge struct {
			x, y float32
			over float32
		}
		var badges []overflowBadge

		if fr.Built != nil {
			var drawNodes func(nodes []gameui.ScreenNodeDoc, parentSelected bool, inBox bool)
			drawNodes = func(nodes []gameui.ScreenNodeDoc, parentSelected bool, inBox bool) {
				for j := range nodes {
					n := &nodes[j]
					isSel := n.ID == selID || parentSelected
					wgt := fr.Built.Widget(n.ID)
					if wgt == nil {
						continue
					}
					b := wgt.Bounds()
					if b.Size.Width > 0 && b.Size.Height > 0 && n.Type != gameui.NodeContainer {
						nx := fr.X + float32(b.Origin.X)*fr.Scale
						ny := fr.Y + float32(b.Origin.Y)*fr.Scale
						nw := float32(b.Size.Width) * fr.Scale
						nh := float32(b.Size.Height) * fr.Scale

						_, over := ed.hudMeasureNodeText(n, loc)
						drawHUDNodeVisual(d, ed, n, loc, nx, ny, nw, nh, fr.Scale, over > 0)

						// Overflow / selection rings.
						if over > 0 {
							d.SetStrokeColor(specProblem).SetLineWidth(1.5).BeginPath().RoundRect(nx-1, ny-1, nw+2, nh+2, 3).Stroke()
							badges = append(badges, overflowBadge{
								x:    nx + nw + over*fr.Scale + 4,
								y:    ny + nh/2,
								over: over,
							})
						}
						if isSel && !(n.Type == gameui.NodeColorRect && n.Anchor == gameui.AnchorFill) {
							off := float32(2)
							if over > 0 {
								off = 1
							}
							d.SetStrokeColor(specAccent).SetLineWidth(1.5).BeginPath().RoundRect(nx-off, ny-off, nw+off*2, nh+off*2, 3).Stroke()
						}

						// Touch area overlay circle.
						if ov.TouchAreas && fr.Device.Touch && isHUDTouchDraggable(n) {
							hit := effectiveTouchHitSize(n)
							diam := hit * fr.Scale
							cxp := nx + nw/2
							cyp := ny + nh/2
							d.Save()
							if hit < gameui.MinTouchTargetSize {
								d.SetStrokeColor(specProblem).SetLineWidth(1.5)
							} else {
								d.SetStrokeColor(specTouchDashStroke).SetLineWidth(1.5).SetLineDash(3, 3)
							}
							d.BeginPath().Arc(cxp, cyp, diam/2, 0, 2*math.Pi, false).Stroke()
							d.Restore()
						}

						// Anchor diamond overlay for selected screen-anchored node.
						if ov.Anchors && !inBox && n.ID == selID && n.Anchor != gameui.AnchorFill && n.EffectiveAnchor() != gameui.AnchorCenter {
							ax, ay := hudAnchorPoint(fr.Device, n)
							dx := fr.X + ax*fr.Scale
							dy := fr.Y + ay*fr.Scale
							drawHUDAnchorDiamond(d, dx, dy)
						}

						if n.FocusOrder > 0 {
							focusPts = append(focusPts, focusPoint{
								order: n.FocusOrder,
								x:     nx,
								y:     ny + nh/2,
							})
						}
					}
					if len(n.Children) > 0 {
						childInBox := inBox || n.Type == gameui.NodeVBox || n.Type == gameui.NodeHBox
						drawNodes(n.Children, isSel, childInBox)
					}
				}
			}
			drawNodes(doc.Nodes, false, false)
		}

		// Phone notch pill on the left edge (inside screen).
		if isPhone {
			notchH := min(fr.H*0.38, 80*fr.Scale/0.58)
			notchW := max(float32(8), 14*fr.Scale/0.58)
			d.SetFillColor(specBg).BeginPath().RoundRect(fr.X+4, fr.Y+fr.H/2-notchH/2, notchW, notchH, notchW/2).Fill()
		}
		d.Restore() // end screen clip

		// Draw overflow badges and focus-order badges over the screen frame so they are never clipped.
		badgeFont := core.Font{Size: 10.5, Weight: core.FontWeightSemibold}
		for _, bg := range badges {
			txt := "+" + formatHUDPoints(bg.over)
			bw := max(float32(22), float32(len(txt))*6.5+8)
			bh := float32(16)
			d.SetFillColor(specProblem).BeginPath().RoundRect(bg.x, bg.y-bh/2, bw, bh, 4).Fill()
			d.SetFillColor(specBg).FillText(txt, bg.x+4, bg.y+3.5, badgeFont)
		}

		if ov.FocusOrder && len(focusPts) > 0 {
			for a := 0; a < len(focusPts); a++ {
				for b := a + 1; b < len(focusPts); b++ {
					if focusPts[b].order < focusPts[a].order {
						focusPts[a], focusPts[b] = focusPts[b], focusPts[a]
					}
				}
			}
			const r float32 = 7
			for idx, fp := range focusPts {
				cxp := fp.x - r - 6
				cyp := fp.y
				if idx+1 < len(focusPts) {
					nxp := focusPts[idx+1].x - r - 6
					nyp := focusPts[idx+1].y
					d.SetStrokeColor(specAccent).SetLineWidth(1.5).BeginPath().MoveTo(cxp, cyp+r).LineTo(nxp, nyp-r).Stroke()
				}
				d.SetFillColor(specAccent).BeginPath().Arc(cxp, cyp, r, 0, 2*math.Pi, false).Fill()
				numStr := fmt.Sprintf("%d", fp.order)
				d.SetFillColor(specBg).FillText(numStr, cxp-3.2, cyp+3.5, badgeFont)
			}
		}
	}

	d.Restore()
}

func drawHUDGameBackdrop(d *core.Drawing, x, y, w, h, sc float32) {
	// Subtle dark room + dungeon floor tiles + knight silhouette using Spec tokens.
	d.SetFillColor(specRaised).FillRect(x, y, w, h)
	floorH := 48 * sc
	fy := y + h - floorH
	d.SetFillColor(specLineRaised).FillRect(x, fy, w, floorH)
	d.SetStrokeColor(specLine).SetLineWidth(1)
	step := max(float32(8), 32*sc)
	for gx := x; gx < x+w; gx += step {
		d.BeginPath().MoveTo(gx, fy).LineTo(gx, y+h).Stroke()
	}
	d.BeginPath().MoveTo(x, fy).LineTo(x+w, fy).Stroke()
	if fy+step < y+h {
		d.BeginPath().MoveTo(x, fy+step).LineTo(x+w, fy+step).Stroke()
	}

	// Minimal pixel-knight silhouette standing on the floor so HUD contrast is easy to judge.
	kx := x + w*0.36
	kw := 18 * sc
	kh := 28 * sc
	ky := fy - kh
	d.SetFillColor(specTextSecondary).BeginPath().RoundRect(kx, ky+4*sc, kw, kh-4*sc, 3*sc).Fill()
	d.SetFillColor(specChanged).BeginPath().RoundRect(kx+4*sc, ky, kw-8*sc, 5*sc, 2*sc).Fill()
	d.SetFillColor(specBg).FillRect(kx+kw*0.45, ky+8*sc, kw*0.42, 4*sc)
}

func drawHUDNodeVisual(d *core.Drawing, ed *Editor, n *gameui.ScreenNodeDoc, loc string, x, y, w, h, sc float32, overflow bool) {
	radius := n.CornerRadius * sc
	bg := parseHUDColor(n.Background)
	fg := parseHUDColor(n.TextColor)

	switch n.Type {
	case gameui.NodeColorRect:
		if bg == 0 {
			bg = specDimOverlay
		}
		d.SetFillColor(bg).BeginPath().RoundRect(x, y, w, h, radius).Fill()

	case gameui.NodeVBox, gameui.NodeHBox:
		if bg != 0 {
			if radius == 0 {
				radius = 8 * sc
			}
			d.SetFillColor(bg).BeginPath().RoundRect(x, y, w, h, radius).Fill()
			d.SetStrokeColor(specLineRaised).SetLineWidth(1).BeginPath().RoundRect(x, y, w, h, radius).Stroke()
		}

	case gameui.NodeSpriteRow:
		count := n.SpriteCount
		if count <= 0 {
			count = 3
		}
		spacing := max(float32(2), n.Spacing) * sc
		availW := max(float32(1), w-spacing*float32(max(0, count-1)))
		iconS := min(h, availW/float32(count))
		col := bg
		if col == 0 {
			col = specProblem
		}
		iy := y + (h-iconS)/2
		for i := range count {
			ix := x + float32(i)*(iconS+spacing)
			d.SetFillColor(col).BeginPath().RoundRect(ix, iy, iconS, iconS, iconS*0.28).Fill()
		}

	case gameui.NodeTouchStick:
		cxp, cyp := x+w/2, y+h/2
		r := min(w, h) / 2
		base := bg
		if base == 0 {
			base = specTouchBase
		}
		knob := fg
		if knob == 0 {
			knob = specTouchKnob
		}
		d.SetFillColor(base).BeginPath().Arc(cxp, cyp, r, 0, 2*math.Pi, false).Fill()
		d.SetStrokeColor(specTouchBorder).SetLineWidth(1).BeginPath().Arc(cxp, cyp, r, 0, 2*math.Pi, false).Stroke()
		d.SetFillColor(knob).BeginPath().Arc(cxp, cyp, r*0.32, 0, 2*math.Pi, false).Fill()

	case gameui.NodeTouchButton:
		cxp, cyp := x+w/2, y+h/2
		r := min(w, h) / 2
		fill := bg
		if fill == 0 {
			fill = specTouchFill
		}
		d.SetFillColor(fill).BeginPath().Arc(cxp, cyp, r, 0, 2*math.Pi, false).Fill()
		d.SetStrokeColor(specTouchBorder).SetLineWidth(1).BeginPath().Arc(cxp, cyp, r, 0, 2*math.Pi, false).Stroke()

	case gameui.NodeButton:
		fill := bg
		if fill == 0 {
			fill = specMenuBtnFill
		}
		if radius == 0 {
			radius = 4 * sc
		}
		d.SetFillColor(fill).BeginPath().RoundRect(x, y, w, h, radius).Fill()
		if n.FocusOrder == 1 {
			d.SetStrokeColor(specWarning).SetLineWidth(max(float32(1), 1.5*sc)).BeginPath().RoundRect(x, y, w, h, radius).Stroke()
		}
		if n.TextKey == "" {
			// Pause icon bars for icon-only button.
			bw := max(float32(1.5), w*0.12)
			bh := h * 0.45
			by := y + (h-bh)/2
			d.SetFillColor(specTextStrong).FillRect(x+w*0.32, by, bw, bh)
			d.SetFillColor(specTextStrong).FillRect(x+w*0.56, by, bw, bh)
		} else {
			txt, ok := ed.hudLookupText(loc, n.TextKey)
			if !ok {
				txt = n.TextKey
			}
			drawHUDNodeText(d, n, txt, fg, x, y, w, h, sc, overflow)
		}

	case gameui.NodeLabel:
		if bg != 0 {
			if radius == 0 {
				radius = 4 * sc
			}
			d.SetFillColor(bg).BeginPath().RoundRect(x, y, w, h, radius).Fill()
		}
		if n.TextKey != "" {
			txt, ok := ed.hudLookupText(loc, n.TextKey)
			if !ok {
				txt = n.TextKey
			}
			drawHUDNodeText(d, n, txt, fg, x, y, w, h, sc, overflow)
		}
	}
}

func drawHUDNodeText(d *core.Drawing, n *gameui.ScreenNodeDoc, txt string, fg core.Color, x, y, w, h, sc float32, overflow bool) {
	if fg == 0 {
		fg = specTextStrong
	}
	fs := n.FontSize
	if fs <= 0 {
		fs = gameui.DefaultScreenFontSize
	}
	scaledFS := max(float32(6.5), fs*sc)
	font := core.Font{Size: core.Points(scaledFS)}
	var padL float32
	if n.Padding != nil {
		padL = n.Padding.Left * sc
	}
	tx := x + padL
	if !overflow && (n.Anchor == gameui.AnchorTopCenter || n.Anchor == gameui.AnchorCenter || n.Anchor == gameui.AnchorBottomCenter) && n.Padding == nil {
		tw, _ := gameui.MeasureScreenText(txt, fs)
		if tw*sc < w {
			tx = x + (w-tw*sc)/2
		}
	}
	ty := y + h/2 + scaledFS*0.34
	d.SetFillColor(fg).FillText(txt, tx, ty, font)
}

func hudAnchorPoint(dev gameui.DeviceProfile, n *gameui.ScreenNodeDoc) (float32, float32) {
	var sl, sr, st, sb float32
	if n.KeepInSafeArea() {
		sl = float32(dev.SafeArea.Left)
		sr = float32(dev.SafeArea.Right)
		st = float32(dev.SafeArea.Top)
		sb = float32(dev.SafeArea.Bottom)
	}
	a := n.EffectiveAnchor()
	var ax, ay float32
	switch a {
	case gameui.AnchorTopLeft, gameui.AnchorCenterLeft, gameui.AnchorBottomLeft:
		ax = sl
	case gameui.AnchorTopCenter, gameui.AnchorCenter, gameui.AnchorBottomCenter:
		ax = dev.Width / 2
	case gameui.AnchorTopRight, gameui.AnchorCenterRight, gameui.AnchorBottomRight:
		ax = dev.Width - sr
	}
	switch a {
	case gameui.AnchorTopLeft, gameui.AnchorTopCenter, gameui.AnchorTopRight:
		ay = st
	case gameui.AnchorCenterLeft, gameui.AnchorCenter, gameui.AnchorCenterRight:
		ay = dev.Height / 2
	case gameui.AnchorBottomLeft, gameui.AnchorBottomCenter, gameui.AnchorBottomRight:
		ay = dev.Height - sb
	}
	return ax, ay
}

func drawHUDAnchorDiamond(d *core.Drawing, x, y float32) {
	const r float32 = 5
	p := new(core.Path).
		MoveTo(x, y-r).
		LineTo(x+r, y).
		LineTo(x, y+r).
		LineTo(x-r, y).
		ClosePath()
	d.SetFillColor(specAccent).FillPath(p, core.NonZero)
	d.SetStrokeColor(specBg).SetLineWidth(1.5).StrokePath(p)
}

// drawHUDZoomOverflow renders the 1.5× zoomed detail card preview in the right inspector showing the fixed box,
// overflowing localized string, and red diagonal-hatched spill area.
func drawHUDZoomOverflow(d *core.Drawing, size core.Size, n *gameui.ScreenNodeDoc, text string, over float32) {
	if d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	w, h := float32(size.Width), float32(size.Height)
	const zs float32 = 1.5

	d.Save()
	d.BeginPath().RoundRect(0, 0, w, h, 8).Clip()
	d.SetFillColor(specPanel).FillRect(0, 0, w, h)

	boxH := max(float32(20), n.Height) * zs
	if boxH > h-8 {
		boxH = h - 8
	}
	boxW := max(float32(40), n.Width) * zs
	spillW := over * zs
	// Scale down slightly only if the zoomed box+spill would exceed the inspector card width.
	totalW := boxW + spillW
	avail := w - 24
	scale := float32(1)
	if totalW > avail && totalW > 0 {
		scale = avail / totalW
	}
	bx := float32(12)
	by := (h - boxH) / 2
	bw := boxW * scale
	sw := spillW * scale

	fill := parseHUDColor(n.Background)
	if fill == 0 {
		fill = specHintFill
	}
	d.SetFillColor(fill).BeginPath().RoundRect(bx, by, bw, boxH, 6).Fill()
	d.SetStrokeColor(specProblem).SetLineWidth(1.5).BeginPath().RoundRect(bx, by, bw, boxH, 6).Stroke()

	// Red diagonal hatched spill region past the right edge of the box.
	if sw > 0 {
		d.Save()
		d.BeginPath().Rect(bx+bw, by, sw, boxH).Clip()
		d.SetFillColor(core.RGBA(0xFF6B6B, 0.14)).FillRect(bx+bw, by, sw, boxH)
		d.SetStrokeColor(specProblemHatch).SetLineWidth(2)
		for hx := bx + bw - boxH; hx < bx+bw+sw+boxH; hx += 8 {
			d.BeginPath().MoveTo(hx, by+boxH).LineTo(hx+boxH, by).Stroke()
		}
		d.Restore()
	}

	fs := n.FontSize
	if fs <= 0 {
		fs = gameui.DefaultScreenFontSize
	}
	font := core.Font{Size: core.Points(fs * zs * scale)}
	padL := float32(6)
	if n.Padding != nil && n.Padding.Left > 0 {
		padL = n.Padding.Left * zs * scale
	}
	d.SetFillColor(specTextStrong).FillText(text, bx+padL, by+boxH/2+(fs*zs*scale)*0.34, font)
	d.Restore()
}

func parseHUDColor(raw string) core.Color {
	if raw == "" {
		return 0
	}
	c, err := core.ParseColor(raw)
	if err != nil {
		return 0
	}
	return c
}

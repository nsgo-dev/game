package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// FirstRunScreenView renders the active full-window First Run stage (Step 3 Plan, Step 4 Building, or Step 5 Review)
// using exclusively theme.Palette tokens (Dark & Light) matching design/editor/FirstRun.dc.html.
func FirstRunScreenView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.SettingsOpen() || ed.ShortcutsSheetOpen() {
			return
		}
		if ed.FirstRunStage() == FirstRunStageBuilding {
			snap := ed.FirstRunBuildState()
			switch snap.Outcome {
			case FirstRunBuildRunning:
				ed.RequestCancelFirstRunBuild()
			case FirstRunBuildCancelPrompt:
				ed.KeepBuildingFirstRun()
			}
		}
	})
	// Track held B key on a stable scope so holding B in Step 5 peeks Before without remounting on rev ticks,
	// while ignoring B whenever typing or a modal/command palette is open.
	prevHeldB := false
	cx.Effect(func() {
		blocked := ed.IsTyping() || ed.SettingsOpen() || ed.paletteOpen || ed.ShortcutsSheetOpen()
		held := !blocked && core.KeyHeld(cx, "b")
		if held != prevHeldB {
			prevHeldB = held
			if ed.FirstRunStage() == FirstRunStageReview || !held {
				ed.SetFirstRunReviewPeekBefore(held)
			}
		}
	})
	cx.Cleanup(func() {
		if prevHeldB {
			ed.SetFirstRunReviewPeekBefore(false)
		}
	})

	type firstRunRenderKey struct {
		stage FirstRunStage
		dark  bool
		lang  string
	}
	renderKey := core.NewMemo(cx, func() firstRunRenderKey {
		_ = rev.Get()
		return firstRunRenderKey{
			stage: ed.FirstRunStage(),
			dark:  ed.Palette(cx).Dark,
			lang:  ed.ResolvedLanguage(),
		}
	})

	return core.Dynamic(func(cx *core.Scope) core.Node {
		switch renderKey.Get().stage {
		case FirstRunStagePlan:
			return firstRunPlanView(cx, ed, rev)
		case FirstRunStageBuilding:
			return firstRunBuildingView(cx, ed, rev)
		case FirstRunStageReview:
			return firstRunReviewView(cx, ed, rev)
		default:
			return core.View{}
		}
	})
}

// startWhatHappensNextView renders the 3-step WHAT HAPPENS NEXT strip on Step 1 of the Start Screen.
func startWhatHappensNextView(p theme.Palette, ed *Editor) core.Node {
	stepRow := func(num, title, note string) core.Node {
		return core.View{
			Style: core.Style{
				MinHeight:  core.Pt(28),
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        12,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:          core.Pt(20),
						Height:         core.Pt(20),
						CornerRadius:   10,
						Background:     p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{
						core.Text{
							Content: num,
							Style: core.Style{
								FontSize:   11,
								FontWeight: core.FontWeightSemibold,
								Color:      p.TextSecondary,
							},
						},
					},
				},
				core.Text{
					Content: title,
					Style: core.Style{
						Width:      core.Pt(176),
						FontSize:   12.5,
						FontWeight: core.FontWeightMedium,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
				core.Text{
					Content: note,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextMuted,
					},
				},
			},
		}
	}

	return core.View{
		TestID: "start-what-happens-next",
		Style: core.Style{
			Borders:     core.Edges{Top: 1},
			BorderColor: p.Line,
			Padding:     core.Edges{Top: 12},
			Gap:         6,
		},
		Children: []core.Node{
			specSectionLabel(p, ed.T("firstRun.whatHappensNext")),
			stepRow("1", ed.T("firstRun.step1Title"), ed.T("firstRun.step1Note")),
			stepRow("2", ed.T("firstRun.step2Title"), ed.T("firstRun.step2Note")),
			stepRow("3", ed.T("firstRun.step3Title"), ed.T("firstRun.step3Note")),
		},
	}
}

// firstRunConnectInPlaceView renders Step 2 (Connect, in place) inside the Start Screen while keeping
// the author's idea and project name visible and editable at the top.
func firstRunConnectInPlaceView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.SettingsOpen() || ed.ShortcutsSheetOpen() {
			return
		}
		if ed.FirstRunStage() == FirstRunStageConnect {
			ed.DeferFirstRunConnect()
		}
	})
	p := ed.Palette(cx)

	return core.View{
		TestID: "first-run-connect",
		Style: core.Style{
			Width:    core.Pct(100),
			MaxWidth: core.Pt(720),
			Gap:      16,
		},
		Children: []core.Node{
			// Preserved idea + project name summary bar with Edit button.
			core.View{
				TestID: "first-run-connect-summary",
				Style: core.Style{
					MinHeight:    core.Pt(44),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          12,
					Padding:      core.Symmetric(8, 12),
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					specIcon(lucide.Sparkles, 15, p.TextMuted),
					core.Text{
						TestID:   "first-run-connect-idea",
						MaxLines: 1,
						ContentFunc: func() string {
							_ = rev.Get()
							idea := strings.TrimSpace(ed.StartProjectIdea())
							if idea == "" {
								return ed.T("firstRun.defaultIdeaSummary")
							}
							return idea
						},
						Style: core.Style{
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							FontSize:  12.5,
							Color:     p.TextSecondary,
						},
					},
					core.Text{
						TestID: "first-run-connect-name",
						ContentFunc: func() string {
							_ = rev.Get()
							return ed.StartProjectName()
						},
						Style: core.Style{
							FontSize:   11.5,
							FontFamily: core.FontMonospace,
							Color:      p.TextMuted,
							FlexShrink: 0,
						},
					},
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-connect-edit",
						Text:    ed.T("firstRun.edit"),
						Variant: specBtnGhost,
						Compact: true,
						Style:   core.Style{BorderWidth: 0},
						OnPress: func() {
							ed.EditFirstRunFromConnect()
						},
					}),
				},
			},
			// Connect an agent card.
			core.View{
				TestID: "first-run-connect-card",
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(16),
					Gap:          12,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignStart,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            12,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Gap: 4},
								Children: []core.Node{
									core.Text{
										Content: ed.T("firstRun.connectTitle"),
										Style: core.Style{
											FontSize:   14,
											FontWeight: core.FontWeightSemibold,
											Color:      p.TextPrimary,
										},
									},
									core.Text{
										Content: ed.T("firstRun.connectSubtitle"),
										Style: core.Style{
											FontSize: 12.5,
											Color:    p.TextMuted,
										},
									},
								},
							},
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-connect-refresh",
								Label:   ed.T("firstRun.redetectAgents"),
								Tooltip: ed.T("firstRun.redetectAgents"),
								Icon:    lucide.RefreshCw,
								Variant: specBtnGhost,
								Compact: true,
								OnPress: func() {
									ed.RefreshDetectedLocalAgents()
								},
							}),
						},
					},
					// Radio list of agent choices.
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						opts := ed.FirstRunAgentOptions()
						sel := ed.FirstRunSelectedAgent()
						rows := make([]core.Node, 0, len(opts))
						for _, o := range opts {
							opt := o
							selected := opt.Kind == sel
							disabled := opt.Kind != "apikey" && !opt.Detected
							bg := p.Panel
							border := p.LineRaised
							borderW := core.Points(1)
							if selected {
								bg = p.Accent.Fill
								border = p.Accent.Edge
								borderW = 1.5
							}
							nameColor := p.TextPrimary
							if disabled {
								nameColor = p.TextMuted
							}
							desc := opt.Description
							if opt.Kind != "apikey" && opt.Detected && opt.ExecutablePath != "" {
								desc = desc + " · " + shortenHomePath(opt.ExecutablePath)
							}

							var radioDot core.Node
							if selected {
								dotBg := p.TextPrimary
								if !p.Dark {
									dotBg = p.Panel
								}
								radioDot = core.View{
									Style: core.Style{
										Width:        core.Pt(16),
										Height:       core.Pt(16),
										CornerRadius: 8,
										Background:   dotBg,
										BorderWidth:  5,
										BorderColor:  p.Accent.Fg,
										FlexShrink:   0,
									},
								}
							} else {
								dotEdge := p.TextFaint
								if disabled {
									dotEdge = p.LineRaised
								}
								radioDot = core.View{
									Style: core.Style{
										Width:        core.Pt(16),
										Height:       core.Pt(16),
										CornerRadius: 8,
										BorderWidth:  1.5,
										BorderColor:  dotEdge,
										FlexShrink:   0,
									},
								}
							}

							rowChildren := []core.Node{
								radioDot,
								core.View{
									Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Gap: 2},
									Children: []core.Node{
										core.Text{
											Content: opt.Name,
											Style: core.Style{
												FontSize:   13,
												FontWeight: core.FontWeightMedium,
												Color:      nameColor,
											},
										},
										core.Text{
											Content:       desc,
											MaxLines:      1,
											EllipsizeMode: core.EllipsizeTail,
											Style: core.Style{
												FontSize: 11.5,
												Color:    p.TextMuted,
											},
										},
									},
								},
							}
							if opt.Kind != "apikey" && opt.Detected {
								rowChildren = append(rowChildren, core.View{
									Style: core.Style{
										Height:         core.Pt(22),
										Padding:        core.Symmetric(0, 8),
										CornerRadius:   specRadiusControl,
										Background:     p.Added.Fill,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("firstRun.detected"),
											Style: core.Style{
												FontSize:   11,
												FontWeight: core.FontWeightMedium,
												Color:      p.Added.Fg,
											},
										},
									},
								})
							}

							rows = append(rows, core.View{
								TestID:   "first-run-agent-" + opt.Kind,
								Role:     core.RoleRadio,
								Label:    opt.Name,
								Disabled: func() bool { return disabled },
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          12,
									Padding:      core.Symmetric(10, 12),
									CornerRadius: specRadiusButton,
									Background:   bg,
									BorderWidth:  borderW,
									BorderColor:  border,
								},
								OnPress: func(core.PressEvent) {
									if disabled {
										return
									}
									_ = ed.SelectFirstRunAgent(opt.Kind)
								},
								Children: rowChildren,
							})
						}
						return core.View{
							Role:     core.RoleRadioGroup,
							Label:    ed.T("firstRun.agentGroupLabel"),
							Style:    core.Style{Gap: 8},
							Children: rows,
						}
					}),
					// Status body: keyed on (selectedAgent, isTimeout, lang) so the API key input stays stably
					// mounted across every keystroke instead of remounting on rev bumps.
					func() core.Node {
						type connectBodyMode struct {
							sel     string
							timeout bool
							lang    string
						}
						bodyMode := core.NewMemo(cx, func() connectBodyMode {
							_ = rev.Get()
							return connectBodyMode{
								sel:     ed.FirstRunSelectedAgent(),
								timeout: ed.FirstRunConnectStatus() == FirstRunConnectTimeout,
								lang:    ed.ResolvedLanguage(),
							}
						})
						return core.Dynamic(func(cx *core.Scope) core.Node {
							m := bodyMode.Get()
							if m.sel == "apikey" {
								return firstRunAPIKeySection(cx, p, ed, rev)
							}
							if m.timeout {
								return core.Dynamic(func(cx *core.Scope) core.Node {
									_ = rev.Get()
									return firstRunConnectTimeoutView(p, ed, ed.FirstRunSelectedAgent())
								})
							}
							return core.Dynamic(func(cx *core.Scope) core.Node {
								_ = rev.Get()
								steps := ed.FirstRunConnectSteps()
								if len(steps) == 0 {
									return core.View{}
								}
								rows := make([]core.Node, 0, len(steps))
								for i, s := range steps {
									var indicator core.Node
									if s.Done {
										indicator = core.View{
											Style: core.Style{
												Width:          core.Pt(16),
												Height:         core.Pt(16),
												CornerRadius:   8,
												Background:     p.Added.Fill,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
												FlexShrink:     0,
											},
											Children: []core.Node{specIcon(lucide.Check, 10, p.Added.Fg)},
										}
									} else {
										indicator = core.View{
											Style: core.Style{
												Width:        core.Pt(14),
												Height:       core.Pt(14),
												CornerRadius: 7,
												BorderWidth:  2,
												BorderColor:  p.Accent.Fg,
												FlexShrink:   0,
											},
										}
									}
									txtColor := p.TextSecondary
									weight := core.FontWeight(400)
									if s.Strong {
										txtColor = p.TextPrimary
										weight = core.FontWeightMedium
									}
									rows = append(rows, core.View{
										TestID: fmt.Sprintf("first-run-connect-step-%d", i),
										Style: core.Style{
											MinHeight:  core.Pt(28),
											Direction:  core.Row,
											AlignItems: core.AlignCenter,
											Gap:        8,
										},
										Children: []core.Node{
											indicator,
											core.Text{
												Content: s.Text,
												Style: core.Style{
													FlexGrow:   1,
													FlexShrink: 1,
													FlexBasis:  core.Pt(0),
													MinWidth:   core.Pt(0),
													FontSize:   12.5,
													FontWeight: weight,
													Color:      txtColor,
												},
											},
										},
									})
								}
								return core.View{
									TestID:   "first-run-connect-steps",
									Style:    core.Style{Padding: core.Symmetric(4, 0), Gap: 2},
									Children: rows,
								}
							})
						})
					}(),
					// Footer: Later + Done.
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyEnd,
							Gap:            8,
							Padding:        core.Edges{Top: 8},
							Borders:        core.Edges{Top: 1},
							BorderColor:    p.Line,
						},
						Children: []core.Node{
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-connect-later",
								Text:    ed.T("firstRun.later"),
								Variant: specBtnGhost,
								Style:   core.Style{BorderWidth: 0},
								OnPress: func() {
									ed.DeferFirstRunConnect()
								},
							}),
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-connect-done",
								Text:    ed.T("firstRun.done"),
								Variant: specBtnAccent,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.FirstRunConnectCanFinish()
								},
								OnPress: func() {
									ed.report(ed.FinishFirstRunConnect())
								},
							}),
						},
					},
				},
			},
		},
	}
}

func firstRunConnectTimeoutView(p theme.Palette, ed *Editor, kind string) core.Node {
	agentName := firstRunAgentDisplayName(kind)
	bin := "claude"
	if kind == "codex" {
		bin = "codex"
	}
	copied := ed.FirstRunDiagnosticsCopied()
	copyText := ed.T("firstRun.copyDiagnostics")
	var copyIcon func() *core.SVG
	if copied {
		copyText = ed.T("firstRun.copied")
		copyIcon = lucide.Check
	}
	connectErr := strings.TrimSpace(ed.FirstRunConnectError())
	headline := ed.T("firstRun.timeoutHeading", l10n.Param("agent", agentName))
	body := ed.T("firstRun.timeoutInstructions", l10n.Param("bin", bin))
	if connectErr != "" && !strings.Contains(strings.ToLower(connectErr), "timed out") && !strings.Contains(strings.ToLower(connectErr), "deadline exceeded") {
		headline = ed.T("firstRun.connectFailedHeading", l10n.Param("agent", agentName))
		body = connectErr + "\n\n" + body
	}
	return core.View{
		TestID: "first-run-connect-timeout",
		Role:   core.RoleAlert,
		Style: core.Style{
			CornerRadius: specRadiusButton,
			Background:   p.Warning.Fill,
			BorderWidth:  1,
			BorderColor:  p.Warning.Edge,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: []core.Node{
			core.Text{
				Content: headline,
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: core.FontWeightMedium,
					Color:      p.Warning.Fg,
				},
			},
			core.Text{
				Content: body,
				Style: core.Style{
					FontSize: 12,
					Color:    p.TextSecondary,
				},
			},
			core.View{
				Style: core.Style{Direction: core.Row, Gap: 8},
				Children: []core.Node{
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-connect-retry",
						Text:    ed.T("firstRun.tryAgain"),
						Variant: specBtnQuiet,
						OnPress: func() {
							ed.RetryFirstRunConnect()
						},
					}),
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-connect-copy-diag",
						Text:    copyText,
						Icon:    copyIcon,
						Variant: specBtnQuiet,
						OnPress: func() {
							ed.CopyFirstRunDiagnostics()
						},
					}),
				},
			},
		},
	}
}

func firstRunAPIKeySection(cx *core.Scope, p theme.Palette, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.View{
		TestID: "first-run-api-key-section",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			core.Text{
				Content: ed.T("firstRun.apiKeyLabel"),
				Style:   core.Style{FontSize: 12, Color: p.TextMuted},
			},
			// Reactive border wrapper keeps the inner specInputField stably mounted across keystrokes.
			core.View{
				Style: core.Style{
					Width:        core.Pct(100),
					Background:   p.Panel,
					BorderWidth:  1.5,
					BorderColor:  p.LineRaised,
					CornerRadius: specRadiusControl,
					Overflow:     core.Hidden,
				},
				StyleFunc: func() core.Style {
					_ = rev.Get()
					borderColor := p.LineRaised
					switch ed.FirstRunConnectStatus() {
					case FirstRunConnectKeyValid:
						borderColor = p.Added.Edge
					case FirstRunConnectKeyInvalid:
						borderColor = p.Removed.Edge
					case FirstRunConnectKeyNoNetwork, FirstRunConnectKeyNoQuota:
						borderColor = p.Warning.Edge
					}
					return core.Style{BorderColor: borderColor}
				},
				Children: []core.Node{
					specInputField(cx, ed, ui.Input{
						TestID:      "first-run-api-key-input",
						Label:       ed.T("firstRun.apiKeyLabel"),
						Masked:      true,
						Placeholder: ed.T("firstRun.apiKeyPlaceholder"),
						Value: func() string {
							_ = rev.Get()
							return ed.FirstRunAPIKey()
						},
						OnChange: func(v string) {
							ed.SetFirstRunAPIKey(v)
						},
						Style: core.Style{
							Width:      core.Pct(100),
							Height:     core.Pt(32),
							Background: core.Transparent,
							Color:      p.TextPrimary,
							FontFamily: core.FontMonospace,
							FontSize:   12,
						},
					}),
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				st := ed.FirstRunConnectStatus()
				statusText := ed.T("firstRun.apiKeyIdle")
				statusColor := p.TextMuted
				var badgeText string
				var badgeFg, badgeBg, badgeEdge core.Color
				switch st {
				case FirstRunConnectKeyValid:
					if ed.Keychain != nil || ed.FirstRunAPIKeyStoredInKeychain() {
						statusText = ed.T("firstRun.apiKeyValidKeychain")
					} else {
						statusText = ed.T("firstRun.apiKeyValidMemory")
					}
					statusColor = p.TextSecondary
					badgeText = ed.T("firstRun.apiKeyValidBadge")
					badgeFg, badgeBg, badgeEdge = p.Added.Fg, p.Added.Fill, p.Added.Edge
				case FirstRunConnectKeyInvalid:
					statusText = ed.T("firstRun.apiKeyInvalid")
					statusColor = p.Removed.Fg
					badgeText = ed.T("firstRun.apiKeyInvalidBadge")
					badgeFg, badgeBg, badgeEdge = p.Removed.Fg, p.Removed.Fill, p.Removed.Edge
				case FirstRunConnectKeyNoNetwork:
					statusText = ed.T("firstRun.apiKeyNoNetwork")
					statusColor = p.Warning.Fg
					badgeText = ed.T("firstRun.apiKeyNoNetworkBadge")
					badgeFg, badgeBg, badgeEdge = p.Warning.Fg, p.Warning.Fill, p.Warning.Edge
				case FirstRunConnectKeyNoQuota:
					statusText = ed.T("firstRun.apiKeyNoQuota")
					statusColor = p.Warning.Fg
					badgeText = ed.T("firstRun.apiKeyNoQuotaBadge")
					badgeFg, badgeBg, badgeEdge = p.Warning.Fg, p.Warning.Fill, p.Warning.Edge
				}
				statusRow := []core.Node{}
				if badgeText != "" {
					statusRow = append(statusRow, core.View{
						TestID: "first-run-api-key-badge",
						Style: core.Style{
							Height:         core.Pt(22),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   specRadiusControl,
							Background:     badgeBg,
							BorderWidth:    1,
							BorderColor:    badgeEdge,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								Content: badgeText,
								Style: core.Style{
									FontSize:   11.5,
									FontWeight: core.FontWeightMedium,
									Color:      badgeFg,
								},
							},
						},
					})
				}
				statusRow = append(statusRow, core.Text{
					TestID:  "first-run-api-key-status",
					Content: statusText,
					Style: core.Style{
						FontSize: 12,
						Color:    statusColor,
					},
				})
				return core.View{
					Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8},
					Children: statusRow,
				}
			}),
		},
	}
}

// firstRunPlanView renders Step 3 (The first plan): read-only template preview with planned ghosts on the left,
// and the Agent panel with the plan card, clarifying question, numbered steps, Just do it, and Approve ⌘↵ on the right.
func firstRunPlanView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.Palette(cx)
	approveShortcut := FormatShortcut("mod+enter", cx.Env().OS())

	planCard := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		if ed.FirstRunPlanLoading() {
			_, agentName, _ := ed.FirstRunConnectedAgent()
			if agentName == "" {
				agentName = "the agent"
			}
			return core.View{
				TestID: "first-run-plan-loading",
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(14),
					Gap:          8,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("firstRun.askingForPlan", l10n.Param("agent", agentName)),
						Style:   core.Style{FontSize: 13, FontWeight: core.FontWeightMedium, Color: p.TextPrimary},
					},
					core.Text{
						Content: ed.T("firstRun.nothingWrittenBeforeApprove"),
						Style:   core.Style{FontSize: 12, Color: p.TextMuted},
					},
				},
			}
		}
		if planErr := strings.TrimSpace(ed.FirstRunPlanError()); planErr != "" {
			return core.View{
				TestID: "first-run-plan-error",
				Role:   core.RoleAlert,
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Warning.Fill,
					BorderWidth:  1,
					BorderColor:  p.Warning.Edge,
					Padding:      core.All(14),
					Gap:          10,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("firstRun.planErrorHeading"),
						Style:   core.Style{FontSize: 13, FontWeight: core.FontWeightSemibold, Color: p.Warning.Fg},
					},
					core.Text{
						Content: planErr,
						Style:   core.Style{FontSize: 12, Color: p.TextSecondary},
					},
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 8},
						Children: []core.Node{
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-plan-retry",
								Text:    ed.T("firstRun.tryAgain"),
								Variant: specBtnQuiet,
								OnPress: func() {
									ed.report(ed.RetryFirstRunPlan())
								},
							}),
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-plan-error-back",
								Text:    "Back to Start",
								Variant: specBtnGhost,
								OnPress: func() {
									ed.BackToStartFromPlan()
								},
							}),
						},
					},
				},
			}
		}
		plan := ed.FirstRunPlan()
		if plan == nil {
			return core.View{}
		}
		approveEnabled := ed.FirstRunPlanCanApprove()

		metaParts := []string{fmt.Sprintf("%d steps", len(plan.Steps))}
		if plan.EstimatedFiles > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%d files", plan.EstimatedFiles))
		}
		if plan.EstimatedObjects > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%d objects", plan.EstimatedObjects))
		}
		if plan.EstimatedMinutes > 0 {
			metaParts = append(metaParts, fmt.Sprintf("about %d min", plan.EstimatedMinutes))
		}

		cardChildren := []core.Node{
			core.View{
				Style: core.Style{
					Padding:     core.All(12),
					Gap:         4,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.LineRaised,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							specIcon(lucide.ListChecks, 13, p.Agent.Fg),
							specSectionLabel(p, ed.T("firstRun.planHeading")),
						},
					},
					core.Text{
						TestID:  "first-run-plan-title",
						Content: ed.T("firstRun.planTitle", l10n.Param("name", plan.ProjectName)),
						Style: core.Style{
							FontSize:   14,
							FontWeight: core.FontWeightSemibold,
							Color:      p.TextPrimary,
						},
					},
					core.Text{
						Content: strings.Join(metaParts, " · "),
						Style: core.Style{
							FontSize: 12,
							Color:    p.TextMuted,
						},
					},
				},
			},
		}

		// Clarifying question card when present.
		if plan.Question != nil && len(plan.Question.Options) > 0 && !plan.Approved {
			var optNodes []core.Node
			for _, o := range plan.Question.Options {
				opt := o
				picked := plan.SelectedAnswer == opt.ID
				bg := p.Raised
				border := p.LineRaised
				borderW := core.Points(1)
				if picked {
					bg = p.Accent.Fill
					border = p.Accent.Edge
					borderW = 1.5
				}
				noteColor := p.TextMuted
				if opt.Suggested {
					noteColor = p.Added.Fg
				}
				optNodes = append(optNodes, core.View{
					TestID: "first-run-plan-answer-" + opt.ID,
					Role:   core.RoleRadio,
					Label:  opt.Label,
					Style: core.Style{
						FlexGrow:     1,
						FlexBasis:    core.Pt(0),
						Padding:      core.All(8),
						CornerRadius: specRadiusButton,
						Background:   bg,
						BorderWidth:  borderW,
						BorderColor:  border,
						Gap:          4,
					},
					OnPress: func(core.PressEvent) {
						ed.SelectFirstRunPlanAnswer(opt.ID)
					},
					Children: []core.Node{
						core.Text{
							Content: opt.Label,
							Style: core.Style{
								FontSize:   12,
								FontWeight: core.FontWeightMedium,
								Color:      p.TextPrimary,
							},
						},
						core.Text{
							Content: opt.Note,
							Style: core.Style{
								FontSize: 11,
								Color:    noteColor,
							},
						},
					},
				})
			}
			cardChildren = append(cardChildren, core.View{
				TestID: "first-run-plan-question",
				Style: core.Style{
					Padding:     core.All(12),
					Gap:         8,
					Background:  p.Panel,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.LineRaised,
				},
				Children: []core.Node{
					core.Text{
						Content: plan.Question.Prompt,
						Style: core.Style{
							FontSize:   12.5,
							FontWeight: core.FontWeightMedium,
							Color:      p.TextPrimary,
						},
					},
					core.View{
						Role:     core.RoleRadioGroup,
						Label:    plan.Question.Prompt,
						Style:    core.Style{Direction: core.Row, Gap: 8},
						Children: optNodes,
					},
				},
			})
		}

		// Numbered steps list.
		var stepNodes []core.Node
		for _, st := range plan.Steps {
			metaChildren := []core.Node{
				core.Text{
					Content: st.FilesLabel,
					Style: core.Style{
						FontSize:   11,
						FontFamily: core.FontMonospace,
						Color:      p.TextMuted,
					},
				},
			}
			if st.Permission != "" {
				metaChildren = append(metaChildren, core.View{
					Style: core.Style{
						Height:       core.Pt(20),
						Padding:      core.Symmetric(0, 6),
						CornerRadius: specRadiusControl,
						Background:   p.Warning.Fill,
						BorderWidth:  1,
						BorderColor:  p.Warning.Edge,
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          4,
					},
					Children: []core.Node{
						specIcon(lucide.Lock, 10, p.Warning.Fg),
						core.Text{
							Content: st.Permission,
							Style:   core.Style{FontSize: 11, Color: p.Warning.Fg},
						},
					},
				})
			}
			stepNodes = append(stepNodes, core.View{
				TestID: fmt.Sprintf("first-run-plan-step-%d", st.Number),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignStart,
					Gap:        8,
					Padding:    core.All(8),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(20),
							Height:         core.Pt(20),
							CornerRadius:   10,
							Background:     p.LineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Text{
								Content: fmt.Sprintf("%d", st.Number),
								Style: core.Style{
									FontSize:   11,
									FontWeight: core.FontWeightSemibold,
									Color:      p.TextSecondary,
								},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: st.Title,
								Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary},
							},
							core.View{
								Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8},
								Children: metaChildren,
							},
						},
					},
				},
			})
		}
		cardChildren = append(cardChildren, core.View{
			Style:    core.Style{Padding: core.All(4), Gap: 2},
			Children: stepNodes,
		})

		// Approval footer.
		cardChildren = append(cardChildren, core.View{
			Style: core.Style{
				Padding:     core.All(12),
				Borders:     core.Edges{Top: 1},
				BorderColor: p.LineRaised,
				Gap:         8,
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("firstRun.planNothingChanges"),
					Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary},
				},
				core.View{
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifySpaceBetween,
						Gap:            8,
					},
					Children: []core.Node{
						specPanelButton(p, specPanelButtonSpec{
							TestID:  "first-run-plan-just-do-it",
							Text:    ed.T("firstRun.justDoIt"),
							Variant: specBtnQuiet,
							OnPress: func() {
								ed.report(ed.JustDoItFirstRunPlan())
							},
						}),
						specPanelButton(p, specPanelButtonSpec{
							TestID:  "first-run-plan-approve",
							Text:    ed.T("firstRun.approveWithShortcut", l10n.Param("shortcut", approveShortcut)),
							Variant: specBtnAccent,
							Disabled: func() bool {
								return !approveEnabled
							},
							OnPress: func() {
								ed.report(ed.ApproveFirstRunPlan())
							},
						}),
					},
				},
			},
		})

		return core.View{
			Style: core.Style{
				CornerRadius: specRadiusCard,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Overflow:     core.Hidden,
			},
			Children: cardChildren,
		}
	}

	return core.View{
		TestID: "first-run-plan",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: p.Bg,
		},
		Children: []core.Node{
			// Top header bar.
			core.View{
				Style: core.Style{
					Height:         core.Pt(specHeaderHeight),
					FlexShrink:     0,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, 12),
					Background:     p.Panel,
					Borders:        core.Edges{Bottom: 1},
					BorderColor:    p.Line,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							specPanelButton(p, specPanelButtonSpec{
								TestID:  "first-run-plan-back",
								Text:    ed.T("firstRun.startBack"),
								Icon:    lucide.ArrowLeft,
								Variant: specBtnGhost,
								Compact: true,
								Style:   core.Style{BorderWidth: 0},
								OnPress: func() {
									ed.BackToStartFromPlan()
								},
							}),
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									if plan := ed.FirstRunPlan(); plan != nil {
										return plan.ProjectName
									}
									return ""
								},
								Style: core.Style{
									FontSize:   14,
									FontWeight: core.FontWeightSemibold,
									Color:      p.TextPrimary,
								},
							},
						},
					},
					core.View{
						TestID: "first-run-plan-pill",
						Style: core.Style{
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 14,
							Background:   p.Agent.Fill,
							BorderWidth:  1,
							BorderColor:  p.Agent.Edge,
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          8,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pt(7),
									Height:       core.Pt(7),
									CornerRadius: 4,
									Background:   p.Agent.Fg,
								},
							},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									if ed.FirstRunPlanLoading() {
										return ed.T("firstRun.planningStatus")
									}
									if ed.FirstRunPlanError() != "" {
										return ed.T("firstRun.planFailedStatus")
									}
									if plan := ed.FirstRunPlan(); plan != nil && plan.Approved {
										return ed.T("firstRun.planApprovedBuilding")
									}
									return ed.T("firstRun.planReadyWaiting")
								},
								Style: core.Style{FontSize: 12, Color: p.Agent.Fg, FontWeight: core.FontWeightMedium},
							},
						},
					},
				},
			},
			// Body: read-only template preview viewport + right Agent panel.
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					core.View{
						TestID: "first-run-plan-viewport",
						Style: core.Style{
							FlexGrow:       1,
							FlexBasis:      core.Pt(0),
							Background:     p.Viewport,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Overflow:       core.Hidden,
						},
						Children: []core.Node{
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = rev.Get()
								plan := ed.FirstRunPlan()
								var stageChildren []core.Node
								if plan != nil {
									for _, sh := range plan.TemplateShapes {
										stageChildren = append(stageChildren, firstRunPreviewShapeNode(p, sh))
									}
									for _, g := range plan.Ghosts {
										stageChildren = append(stageChildren, firstRunGhostNode(p, g))
									}
								}
								return core.View{
									Style: core.Style{
										Width:  core.Pt(360),
										Height: core.Pt(320),
									},
									Children: stageChildren,
								}
							}),
							core.View{
								TestID: "first-run-plan-readonly-pill",
								Style: core.Style{
									Position:       core.Absolute,
									Left:           core.Pt(12),
									Top:            core.Pt(12),
									Height:         core.Pt(24),
									Padding:        core.Symmetric(0, 8),
									CornerRadius:   specRadiusControl,
									Background:     p.PillBg,
									BorderWidth:    1,
									BorderColor:    p.LineRaised,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("firstRun.templatePreviewReadOnly"),
										Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
									},
								},
							},
							core.View{
								Style: core.Style{
									Position:   core.Absolute,
									Left:       core.Pt(12),
									Bottom:     core.Pt(12),
									Direction:  core.Row,
									AlignItems: core.AlignCenter,
									Gap:        8,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Width:        core.Pt(10),
											Height:       core.Pt(10),
											CornerRadius: 2,
											Background:   p.Agent.Fill,
											BorderWidth:  1.5,
											BorderColor:  p.Agent.Edge,
										},
									},
									core.Text{
										Content: ed.T("firstRun.plannedLegend"),
										Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
									},
								},
							},
						},
					},
					core.View{
						TestID: "first-run-plan-agent-panel",
						Style: core.Style{
							Width:       core.Pt(380),
							FlexShrink:  0,
							Background:  p.Panel,
							Borders:     core.Edges{Left: 1},
							BorderColor: p.Line,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Height:         core.Pt(specHeaderHeight),
									FlexShrink:     0,
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifySpaceBetween,
									Padding:        core.Symmetric(0, 12),
									Borders:        core.Edges{Bottom: 1},
									BorderColor:    p.Line,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
										Children: []core.Node{
											core.Text{
												Content: ed.T("firstRun.agentPanelTitle"),
												Style: core.Style{
													FontSize:   14,
													FontWeight: core.FontWeightSemibold,
													Color:      p.TextPrimary,
												},
											},
											core.Text{
												ContentFunc: func() string {
													_ = rev.Get()
													_, agentName, _ := ed.FirstRunConnectedAgent()
													if agentName == "" {
														agentName = "Agent"
													}
													return "· " + agentName
												},
												Style: core.Style{FontSize: 12.5, Color: p.TextFaint},
											},
										},
									},
									core.View{
										Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
										Children: []core.Node{
											core.View{
												Style: core.Style{
													Width:        core.Pt(6),
													Height:       core.Pt(6),
													CornerRadius: 3,
													Background:   p.Added.Fg,
												},
											},
											core.Text{
												Content: ed.T("firstRun.connected"),
												Style:   core.Style{FontSize: 11.5, Color: p.TextMuted},
											},
										},
									},
								},
							},
							ui.ScrollView{
								Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								ContentStyle: core.Style{Padding: core.All(12)},
								EdgeColor:    func() core.Color { return p.Panel },
								Children: []core.Node{
									core.Dynamic(planCard),
								},
							},
						},
					},
				},
			},
		},
	}
}

// firstRunBuildingView renders Step 4 (Building): working layout header, 5-stage progress bar,
// live viewport preview, right Agent panel with object counts + progress bar, Esc/Cancel confirmation dialog,
// and check-failure proposed fix card.
func firstRunBuildingView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.Palette(cx)

	buildStageBar := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunBuildState()
		stageKeys := [5]string{
			"firstRun.buildStage.template",
			"firstRun.buildStage.project",
			"firstRun.buildStage.firstScene",
			"firstRun.buildStage.check",
			"firstRun.buildStage.playtest",
		}
		var stageNodes []core.Node
		for i, name := range FirstRunBuildStageNames {
			stageLabel := name
			if i < len(stageKeys) {
				stageLabel = ed.T(stageKeys[i])
			}
			st := snap.Stages[i]
			var dot core.Node
			labelColor := p.TextMuted
			weight := core.FontWeight(400)
			switch st {
			case FirstRunBuildStageDone:
				labelColor = p.TextSecondary
				dot = core.View{
					Style: core.Style{
						Width:          core.Pt(16),
						Height:         core.Pt(16),
						CornerRadius:   8,
						Background:     p.Added.Fill,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{specIcon(lucide.Check, 10, p.Added.Fg)},
				}
			case FirstRunBuildStageFail:
				labelColor = p.Removed.Fg
				dot = core.View{
					Style: core.Style{
						Width:          core.Pt(16),
						Height:         core.Pt(16),
						CornerRadius:   8,
						Background:     p.Removed.Fill,
						BorderWidth:    1,
						BorderColor:    p.Removed.Edge,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{specIcon(lucide.X, 10, p.Removed.Fg)},
				}
			case FirstRunBuildStageActive:
				labelColor = p.TextPrimary
				weight = core.FontWeightMedium
				dot = core.View{
					Style: core.Style{
						Width:        core.Pt(14),
						Height:       core.Pt(14),
						CornerRadius: 7,
						BorderWidth:  2,
						BorderColor:  p.Agent.Fg,
					},
				}
			default:
				dot = core.View{
					Style: core.Style{
						Width:        core.Pt(14),
						Height:       core.Pt(14),
						CornerRadius: 7,
						BorderWidth:  1.5,
						BorderColor:  p.LineRaised,
					},
				}
			}
			stageNodes = append(stageNodes, core.View{
				TestID: fmt.Sprintf("first-run-build-stage-%d", i),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					dot,
					core.Text{
						Content: stageLabel,
						Style: core.Style{
							FontSize:   12,
							FontWeight: weight,
							Color:      labelColor,
						},
					},
				},
			})
			if i < len(FirstRunBuildStageNames)-1 {
				stageNodes = append(stageNodes, core.View{
					Style: core.Style{
						Width:      core.Pt(16),
						Height:     core.Pt(1),
						Background: p.LineRaised,
					},
				})
			}
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: stageNodes,
		}
	}

	buildViewport := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunBuildState()
		stageChildren := []core.Node{
			core.View{
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(16),
					Top:          core.Pt(68),
					Width:        core.Pt(340),
					Height:       core.Pt(220),
					CornerRadius: 4,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
			},
		}
		for _, sh := range snap.Shapes {
			stageChildren = append(stageChildren, firstRunPreviewShapeNode(p, sh))
		}
		if snap.Outcome == FirstRunBuildRunning && snap.PlacingLabel != "" {
			stageChildren = append(stageChildren, core.View{
				TestID: "first-run-build-placing-pill",
				Style: core.Style{
					Position:       core.Absolute,
					Left:           core.Pt(196),
					Top:            core.Pt(188),
					Height:         core.Pt(22),
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   11,
					Background:     p.PillBg,
					BorderWidth:    1,
					BorderColor:    p.Agent.Edge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: snap.PlacingLabel,
						Style:   core.Style{FontSize: 11, Color: p.Agent.Fg},
					},
				},
			})
		}
		if snap.Outcome == FirstRunBuildCheckFailed && snap.CheckFailure != nil && snap.CheckFailure.ObjectBadge != "" {
			stageChildren = append(stageChildren, core.View{
				TestID: "first-run-build-problem-pill",
				Style: core.Style{
					Position:       core.Absolute,
					Left:           core.Pt(164),
					Top:            core.Pt(256),
					Height:         core.Pt(22),
					Padding:        core.Symmetric(0, 8),
					CornerRadius:   11,
					Background:     p.PillBg,
					BorderWidth:    1,
					BorderColor:    p.Removed.Edge,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: snap.CheckFailure.ObjectBadge,
						Style:   core.Style{FontSize: 11, Color: p.Removed.Fg},
					},
				},
			})
		}
		return core.View{
			Style: core.Style{
				Width:  core.Pt(372),
				Height: core.Pt(310),
			},
			Children: stageChildren,
		}
	}

	buildPanelBody := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunBuildState()
		var panelBody []core.Node
		for i, l := range snap.Log {
			var icon core.Node
			titleColor := p.TextPrimary
			weight := core.FontWeight(400)
			switch l.Status {
			case FirstRunBuildStageDone:
				icon = core.View{
					Style: core.Style{
						Width:          core.Pt(16),
						Height:         core.Pt(16),
						CornerRadius:   8,
						Background:     p.Added.Fill,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{specIcon(lucide.Check, 10, p.Added.Fg)},
				}
			case FirstRunBuildStageActive:
				weight = core.FontWeightMedium
				icon = core.View{
					Style: core.Style{
						Width:        core.Pt(14),
						Height:       core.Pt(14),
						CornerRadius: 7,
						BorderWidth:  2,
						BorderColor:  p.Agent.Fg,
						FlexShrink:   0,
					},
				}
			case FirstRunBuildStageFail:
				titleColor = p.Removed.Fg
				icon = core.View{
					Style: core.Style{
						Width:          core.Pt(16),
						Height:         core.Pt(16),
						CornerRadius:   8,
						Background:     p.Removed.Fill,
						BorderWidth:    1,
						BorderColor:    p.Removed.Edge,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{specIcon(lucide.X, 10, p.Removed.Fg)},
				}
			default:
				titleColor = p.TextMuted
				icon = core.View{
					Style: core.Style{
						Width:        core.Pt(14),
						Height:       core.Pt(14),
						CornerRadius: 7,
						BorderWidth:  1.5,
						BorderColor:  p.LineRaised,
						FlexShrink:   0,
					},
				}
			}
			textCols := []core.Node{
				core.Text{
					Content: l.Title,
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: weight,
						Color:      titleColor,
					},
				},
				core.Text{
					TestID:  fmt.Sprintf("first-run-build-log-sub-%d", i),
					Content: l.Subtitle,
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextMuted,
					},
				},
			}
			if l.Status == FirstRunBuildStageActive && l.TotalCount > 0 {
				pct := float64(l.DoneCount) / float64(l.TotalCount) * 100
				if pct < 5 {
					pct = 5
				}
				if pct > 100 {
					pct = 100
				}
				textCols = append(textCols, core.View{
					Style: core.Style{
						Height:       core.Pt(4),
						CornerRadius: 2,
						Background:   p.LineRaised,
						Overflow:     core.Hidden,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:      core.Pct(float32(pct)),
								Height:     core.Pt(4),
								Background: p.Agent.Fg,
							},
						},
					},
				})
			}
			panelBody = append(panelBody, core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: 8, Padding: core.Symmetric(4, 0)},
				Children: []core.Node{
					icon,
					core.View{
						Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
						Children: textCols,
					},
				},
			})
		}

		switch snap.Outcome {
		case FirstRunBuildCancelPrompt:
			var dialogChildren []core.Node
			dialogChildren = append(dialogChildren,
				core.Text{
					Content: ed.T("firstRun.stopBuildingTitle"),
					Style:   core.Style{FontSize: 13, FontWeight: core.FontWeightSemibold, Color: p.TextPrimary},
				},
			)
			if snap.CanDeleteFolder {
				dialogChildren = append(dialogChildren, core.Text{
					Content: ed.T("firstRun.stopBuildingDeleteBody", l10n.Param("path", snap.DisplayPath), l10n.Param("seconds", snap.CreatedAgeSeconds)),
					Style:   core.Style{FontSize: 12, Color: p.TextMuted},
				})
			} else {
				dialogChildren = append(dialogChildren, core.Text{
					Content: ed.T("firstRun.stopBuildingKeepBody"),
					Style:   core.Style{FontSize: 12, Color: p.TextMuted},
				})
			}
			dialogChildren = append(dialogChildren,
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "first-run-cancel-keep-building",
					Text:    ed.T("firstRun.keepBuilding"),
					Variant: specBtnQuiet,
					Style:   core.Style{Width: core.Pct(100)},
					OnPress: func() {
						ed.KeepBuildingFirstRun()
					},
				}),
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "first-run-cancel-stop-keep",
					Text:    ed.T("firstRun.stopKeepFiles"),
					Variant: specBtnQuiet,
					Style:   core.Style{Width: core.Pct(100)},
					OnPress: func() {
						ed.StopFirstRunKeepFiles()
					},
				}),
			)
			if snap.CanDeleteFolder {
				dialogChildren = append(dialogChildren, specPanelButton(p, specPanelButtonSpec{
					TestID:  "first-run-cancel-stop-delete",
					Text:    ed.T("firstRun.stopDeleteFolder"),
					Variant: specBtnDanger,
					Style:   core.Style{Width: core.Pct(100)},
					OnPress: func() {
						ed.report(ed.StopFirstRunDeleteFolder())
					},
				}))
			}
			panelBody = append(panelBody, core.View{
				TestID: "first-run-cancel-dialog",
				Role:   core.RoleDialog,
				Label:  ed.T("firstRun.stopBuildingTitle"),
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: dialogChildren,
			})

		case FirstRunBuildCheckFailed:
			if snap.CheckFailure != nil {
				panelBody = append(panelBody, core.View{
					TestID: "first-run-check-failed-card",
					Style: core.Style{
						CornerRadius: specRadiusCard,
						Background:   p.Raised,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
						Padding:      core.All(12),
						Gap:          8,
					},
					Children: []core.Node{
						core.Text{
							Content: snap.CheckFailure.FixDescription,
							Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary},
						},
						core.View{
							Style: core.Style{Direction: core.Row, Gap: 8},
							Children: []core.Node{
								specPanelButton(p, specPanelButtonSpec{
									TestID:  "first-run-check-fix-it",
									Text:    ed.T("firstRun.fixIt"),
									Variant: specBtnAccent,
									OnPress: func() {
										ed.report(ed.ApplyFirstRunFix())
									},
								}),
								specPanelButton(p, specPanelButtonSpec{
									TestID:  "first-run-check-review-as-is",
									Text:    ed.T("firstRun.reviewAsIs"),
									Variant: specBtnQuiet,
									OnPress: func() {
										ed.ReviewFirstRunAsIs()
									},
								}),
							},
						},
					},
				})
			}

		case FirstRunBuildStoppedKept:
			panelBody = append(panelBody, core.View{
				TestID: "first-run-stopped-kept-card",
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("firstRun.stoppedKeptBody", l10n.Param("count", snap.ObjectsDone)),
						Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary},
					},
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-stopped-review",
						Text:    ed.T("firstRun.reviewThem"),
						Variant: specBtnQuiet,
						OnPress: func() {
							ed.OpenFirstRunReviewFromStopped()
						},
					}),
				},
			})

		case FirstRunBuildStoppedDeleted:
			panelBody = append(panelBody, core.View{
				TestID: "first-run-stopped-deleted-card",
				Style: core.Style{
					CornerRadius: specRadiusCard,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					Padding:      core.All(12),
					Gap:          8,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("firstRun.stoppedDeletedBody", l10n.Param("path", snap.DisplayPath)),
						Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary},
					},
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-stopped-back-start",
						Text:    ed.T("firstRun.backToStart"),
						Variant: specBtnQuiet,
						OnPress: func() {
							ed.ReturnToStartFromFirstRun()
						},
					}),
				},
			})
		}

		return core.View{
			Style:    core.Style{Gap: 10},
			Children: panelBody,
		}
	}

	return core.View{
		TestID: "first-run-building",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: p.Bg,
		},
		Children: []core.Node{
			// Top working header.
			core.View{
				Style: core.Style{
					Height:         core.Pt(specHeaderHeight),
					FlexShrink:     0,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, 12),
					Background:     p.Panel,
					Borders:        core.Edges{Bottom: 1},
					BorderColor:    p.Line,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.FirstRunBuildState().ProjectName
								},
								Style: core.Style{FontSize: 14, FontWeight: core.FontWeightSemibold, Color: p.TextPrimary},
							},
							core.Text{
								Content: "/",
								Style:   core.Style{FontSize: 13, Color: p.TextFaint},
							},
							core.Text{
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.FirstRunBuildState().SceneName
								},
								Style: core.Style{FontSize: 13, Color: p.TextMuted},
							},
						},
					},
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = rev.Get()
						snap := ed.FirstRunBuildState()
						pillBg, pillEdge, pillFg := p.Agent.Fill, p.Agent.Edge, p.Agent.Fg
						if snap.Outcome == FirstRunBuildCheckFailed {
							pillBg, pillEdge, pillFg = p.Removed.Fill, p.Removed.Edge, p.Removed.Fg
						} else if snap.Outcome == FirstRunBuildStoppedKept || snap.Outcome == FirstRunBuildStoppedDeleted {
							pillBg, pillEdge, pillFg = p.Raised, p.LineRaised, p.TextMuted
						}
						return core.View{
							TestID: "first-run-build-pill",
							Style: core.Style{
								Height:       core.Pt(28),
								Padding:      core.Symmetric(0, 12),
								CornerRadius: 14,
								Background:   pillBg,
								BorderWidth:  1,
								BorderColor:  pillEdge,
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          8,
							},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(7),
										Height:       core.Pt(7),
										CornerRadius: 4,
										Background:   pillFg,
									},
								},
								core.Text{
									Content: snap.StatusPill,
									Style:   core.Style{FontSize: 12, Color: pillFg, FontWeight: core.FontWeightMedium},
								},
							},
						}
					}),
				},
			},
			// 5-stage progress bar.
			core.View{
				TestID: "first-run-build-stage-bar",
				Style: core.Style{
					Height:      core.Pt(44),
					FlexShrink:  0,
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(0, 16),
					Background:  p.Bg,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{core.Dynamic(buildStageBar)},
			},
			// Body: live viewport + Agent panel.
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					core.View{
						TestID: "first-run-build-viewport",
						Style: core.Style{
							FlexGrow:       1,
							FlexBasis:      core.Pt(0),
							Background:     p.Viewport,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Overflow:       core.Hidden,
						},
						Children: []core.Node{core.Dynamic(buildViewport)},
					},
					core.View{
						TestID: "first-run-build-agent-panel",
						Style: core.Style{
							Width:       core.Pt(340),
							FlexShrink:  0,
							Background:  p.Panel,
							Borders:     core.Edges{Left: 1},
							BorderColor: p.Line,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Height:         core.Pt(specHeaderHeight),
									FlexShrink:     0,
									Direction:      core.Row,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifySpaceBetween,
									Padding:        core.Symmetric(0, 12),
									Borders:        core.Edges{Bottom: 1},
									BorderColor:    p.Line,
								},
								Children: []core.Node{
									core.Text{
										Content: ed.T("firstRun.agentPanelTitle"),
										Style:   core.Style{FontSize: 14, FontWeight: core.FontWeightSemibold, Color: p.TextPrimary},
									},
									core.Text{
										ContentFunc: func() string {
											_ = rev.Get()
											elapsedSec := int(ed.FirstRunBuildState().Elapsed.Seconds())
											return fmt.Sprintf("%d:%02d", elapsedSec/60, elapsedSec%60)
										},
										Style: core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: p.TextFaint},
									},
								},
							},
							ui.ScrollView{
								Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
								ContentStyle: core.Style{Padding: core.All(12)},
								EdgeColor:    func() core.Color { return p.Panel },
								Children:     []core.Node{core.Dynamic(buildPanelBody)},
							},
							core.Show{
								When: func() bool {
									_ = rev.Get()
									return ed.FirstRunBuildState().Outcome == FirstRunBuildRunning
								},
								Then: func(cx *core.Scope) core.Node {
									return core.View{
										Style: core.Style{
											Padding:        core.All(12),
											Borders:        core.Edges{Top: 1},
											BorderColor:    p.Line,
											Direction:      core.Row,
											AlignItems:     core.AlignCenter,
											JustifyContent: core.JustifySpaceBetween,
											FlexShrink:     0,
										},
										Children: []core.Node{
											core.Text{
												Content: "Esc",
												Style:   core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: p.TextMuted},
											},
											specPanelButton(p, specPanelButtonSpec{
												TestID:  "first-run-build-cancel",
												Text:    ed.T("firstRun.cancel"),
												Icon:    lucide.Square,
												Variant: specBtnQuiet,
												OnPress: func() {
													ed.RequestCancelFirstRunBuild()
												},
											}),
										},
									}
								},
							},
						},
					},
				},
			},
		},
	}
}

// firstRunReviewView renders Step 5 (The first review): header with Before/After + Revert all + Keep N of M,
// top plan banner with amber deviations, left change list, center viewport (showing "Before: an empty folder"
// in Before mode or when holding B), and the three one-time onboarding tips shown one at a time.
func firstRunReviewView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	p := ed.Palette(cx)

	segBtn := func(testID, label string, active bool, onPress func()) core.Node {
		bg := core.Transparent
		fg := p.TextMuted
		if active {
			bg = p.LineRaised
			fg = p.TextPrimary
		}
		return ui.Button{
			TestID:  testID,
			Text:    label,
			Variant: ui.ButtonGhost,
			Size:    ui.SizeSmall,
			Color:   func() core.Color { return bg },
			TextStyle: core.Style{
				FontSize:   12,
				FontWeight: core.FontWeightMedium,
				Color:      fg,
			},
			Style: core.Style{
				Height:       core.Pt(26),
				CornerRadius: specRadiusControl,
				Padding:      core.Symmetric(0, 10),
			},
			OnPress: onPress,
		}
	}

	reviewHeaderRight := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunReviewState()
		headerRight := []core.Node{}
		if snap.ActiveTipIndex < 0 {
			headerRight = append(headerRight, specPanelButton(p, specPanelButtonSpec{
				TestID:  "first-run-review-replay-tips",
				Text:    ed.T("firstRun.replayTips"),
				Variant: specBtnQuiet,
				Compact: true,
				OnPress: func() {
					ed.report(ed.ReplayTips())
				},
			}))
		}
		headerRight = append(headerRight,
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					Gap:          2,
					Padding:      core.All(2),
					CornerRadius: specRadiusButton,
					Background:   p.Raised,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
				Children: []core.Node{
					segBtn("first-run-review-before", ed.T("firstRun.before"), snap.ShowingBefore, func() {
						ed.SetFirstRunReviewBefore(true)
					}),
					segBtn("first-run-review-after", ed.T("firstRun.after"), !snap.ShowingBefore, func() {
						ed.SetFirstRunReviewBefore(false)
					}),
				},
			},
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "first-run-review-revert-all",
				Text:    ed.T("firstRun.revertAll"),
				Variant: specBtnQuiet,
				OnPress: func() {
					ed.RevertAllFirstRunReview()
				},
			}),
			specPanelButton(p, specPanelButtonSpec{
				TestID:  "first-run-review-keep",
				Text:    ed.T("firstRun.keepOf", l10n.Param("kept", snap.KeptCount), l10n.Param("total", snap.TotalCount)),
				Variant: specBtnAccent,
				OnPress: func() {
					ed.report(ed.KeepFirstRunReview())
				},
			}),
		)
		return core.View{
			Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: headerRight,
		}
	}

	reviewPlanBar := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunReviewState()
		planBarChildren := []core.Node{
			specIcon(lucide.Check, 13, p.Added.Fg),
			core.Text{
				TestID:  "first-run-review-plan-steps",
				Content: ed.T("firstRun.planProgress", l10n.Param("done", snap.PlanStepsDone), l10n.Param("total", snap.PlanStepsTotal)),
				Style:   core.Style{FontSize: 12.5, Color: p.TextPrimary, FontWeight: core.FontWeightMedium},
			},
		}
		for i, dev := range snap.Deviations {
			planBarChildren = append(planBarChildren,
				core.Text{
					Content: "·",
					Style:   core.Style{FontSize: 12.5, Color: p.TextFaint},
				},
				core.Text{
					TestID:  fmt.Sprintf("first-run-review-deviation-%d", i),
					Content: dev.Summary,
					Style:   core.Style{FontSize: 12.5, Color: p.Changed.Fg, FontWeight: core.FontWeightMedium},
				},
			)
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				Wrap:       core.Wrap,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: planBarChildren,
		}
	}

	reviewChangeList := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunReviewState()
		var changeRows []core.Node
		for _, c := range snap.Changes {
			ch := c
			rowBg := core.Transparent
			if ch.ID == "tpl" {
				rowBg = p.Raised
			}
			opacity := core.Alpha(1.0)
			toggleIcon := lucide.Undo2
			toggleLabel := ed.T("firstRun.revertItemLabel", l10n.Param("title", ch.Title))
			if ch.Reverted {
				opacity = core.Alpha(0.55)
				toggleIcon = lucide.Redo2
				toggleLabel = ed.T("firstRun.restoreItemLabel", l10n.Param("title", ch.Title))
			}
			changeRows = append(changeRows, core.View{
				TestID: "first-run-review-change-" + ch.ID,
				Style: core.Style{
					MinHeight:    core.Pt(44),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(4, 8),
					CornerRadius: specRadiusButton,
					Background:   rowBg,
					Opacity:      opacity,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(20),
							Height:         core.Pt(20),
							CornerRadius:   specRadiusControl,
							Background:     p.Added.Fill,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Text{
								Content: ch.Sign,
								Style:   core.Style{FontSize: 12, FontWeight: core.FontWeightSemibold, Color: p.Added.Fg},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content:  ch.Title,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   12.5,
									FontWeight: core.FontWeightMedium,
									Color:      p.TextPrimary,
								},
							},
							core.Text{
								Content:  ch.Detail,
								MaxLines: 1,
								Style: core.Style{
									FontSize: 11,
									Color:    p.TextMuted,
								},
							},
						},
					},
					specPanelButton(p, specPanelButtonSpec{
						TestID:  "first-run-review-toggle-" + ch.ID,
						Icon:    toggleIcon,
						Label:   toggleLabel,
						Variant: specBtnGhost,
						Compact: true,
						Style:   core.Style{BorderWidth: 0},
						OnPress: func() {
							ed.ToggleFirstRunReviewChange(ch.ID)
						},
					}),
				},
			})
		}
		return core.View{
			Style:    core.Style{Gap: 4},
			Children: changeRows,
		}
	}

	reviewViewport := func(cx *core.Scope) core.Node {
		_ = rev.Get()
		snap := ed.FirstRunReviewState()
		revertedMap := make(map[string]bool, len(snap.Changes))
		for _, c := range snap.Changes {
			revertedMap[c.ID] = c.Reverted
		}
		stageChildren := []core.Node{
			core.View{
				Style: core.Style{
					Position:     core.Absolute,
					Left:         core.Pt(16),
					Top:          core.Pt(68),
					Width:        core.Pt(340),
					Height:       core.Pt(220),
					CornerRadius: 4,
					Background:   p.Panel,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
				},
			},
		}
		if !snap.ShowingBefore {
			for _, sh := range snap.Shapes {
				if sh.ChangeID != "" && revertedMap[sh.ChangeID] {
					continue
				}
				stageChildren = append(stageChildren, firstRunPreviewShapeNode(p, sh))
			}
		}
		vpChildren := []core.Node{
			core.View{
				Style: core.Style{
					Width:  core.Pt(372),
					Height: core.Pt(310),
				},
				Children: stageChildren,
			},
		}
		if snap.ShowingBefore {
			vpChildren = append(vpChildren, core.View{
				TestID: "first-run-review-before-pill",
				Style: core.Style{
					Position:       core.Absolute,
					Left:           core.Pt(24),
					Top:            core.Pt(16),
					Height:         core.Pt(26),
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   13,
					Background:     p.PillBg,
					BorderWidth:    1,
					BorderColor:    p.LineRaised,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("firstRun.beforeEmptyFolder"),
						Style:   core.Style{FontSize: 12, Color: p.TextMuted},
					},
				},
			})
		}
		if snap.ActiveTipIndex >= 0 && snap.ActiveTipIndex < 3 {
			vpChildren = append(vpChildren, firstRunReviewTipPopover(cx, p, ed, snap.ActiveTipIndex))
		}
		return core.View{
			Style: core.Style{
				Width:          core.Pct(100),
				Height:         core.Pct(100),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
			},
			Children: vpChildren,
		}
	}

	return core.View{
		TestID: "first-run-review",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: p.Bg,
		},
		Children: []core.Node{
			// Review header.
			core.View{
				Style: core.Style{
					Height:         core.Pt(specHeaderHeight),
					FlexShrink:     0,
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Padding:        core.Symmetric(0, 12),
					Background:     p.Panel,
					Borders:        core.Edges{Bottom: 1},
					BorderColor:    p.Line,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							core.Text{
								Content: ed.T("firstRun.reviewTitle"),
								Style:   core.Style{FontSize: 14, FontWeight: core.FontWeightSemibold, Color: p.TextPrimary},
							},
							core.Text{
								Content: ed.T("firstRun.reviewSubtitle"),
								Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
							},
						},
					},
					core.Dynamic(reviewHeaderRight),
				},
			},
			// Plan summary bar on top of review.
			core.View{
				TestID: "first-run-review-plan-bar",
				Style: core.Style{
					MinHeight:   core.Pt(36),
					FlexShrink:  0,
					Direction:   core.Row,
					Wrap:        core.Wrap,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(6, 12),
					Background:  p.Panel,
					Borders:     core.Edges{Bottom: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{core.Dynamic(reviewPlanBar)},
			},
			// Body: left change list + center viewport with one-at-a-time tip callout.
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					ui.ScrollView{
						TestID: "first-run-review-changes",
						Style: core.Style{
							Width:       core.Pt(260),
							FlexShrink:  0,
							Background:  p.Bg,
							Borders:     core.Edges{Right: 1},
							BorderColor: p.Line,
						},
						ContentStyle: core.Style{Padding: core.All(8)},
						EdgeColor:    func() core.Color { return p.Bg },
						Children:     []core.Node{core.Dynamic(reviewChangeList)},
					},
					core.View{
						TestID: "first-run-review-viewport",
						Style: core.Style{
							FlexGrow:       1,
							FlexBasis:      core.Pt(0),
							Background:     p.Viewport,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Overflow:       core.Hidden,
						},
						Children: []core.Node{core.Dynamic(reviewViewport)},
					},
				},
			},
		},
	}
}

func firstRunReviewTipPopover(cx *core.Scope, p theme.Palette, ed *Editor, tipIdx int) core.Node {
	askShortcut := FormatShortcut("mod+l", cx.Env().OS())
	var stepLabel, body, nextLabel string
	var left, top core.Points
	showSkip := true
	switch tipIdx {
	case 0:
		stepLabel = ed.T("firstRun.tipStepOf", l10n.Param("current", 1), l10n.Param("total", 3))
		body = ed.T("firstRun.tip1Body")
		nextLabel = ed.T("firstRun.tipNext")
		left, top = 20, 40
	case 1:
		stepLabel = ed.T("firstRun.tipStepOf", l10n.Param("current", 2), l10n.Param("total", 3))
		body = ed.T("firstRun.tip2Body")
		nextLabel = ed.T("firstRun.tipNext")
		left, top = 160, 16
	default:
		stepLabel = ed.T("firstRun.tipStepOf", l10n.Param("current", 3), l10n.Param("total", 3))
		body = ed.T("firstRun.tip3Body", l10n.Param("shortcut", askShortcut))
		nextLabel = ed.T("firstRun.tipGotIt")
		showSkip = false
		left, top = 180, 140
	}

	footer := []core.Node{}
	if showSkip {
		footer = append(footer, specPanelButton(p, specPanelButtonSpec{
			TestID:  "first-run-tip-skip",
			Text:    ed.T("firstRun.skipTips"),
			Variant: specBtnGhost,
			Compact: true,
			Style:   core.Style{BorderWidth: 0},
			OnPress: func() {
				ed.report(ed.SkipFirstRunTips())
			},
		}))
	}
	footer = append(footer, specPanelButton(p, specPanelButtonSpec{
		TestID:  "first-run-tip-next",
		Text:    nextLabel,
		Variant: specBtnAccent,
		Compact: true,
		OnPress: func() {
			ed.report(ed.NextFirstRunTip())
		},
	}))

	justify := core.JustifyEnd
	if showSkip {
		justify = core.JustifySpaceBetween
	}

	return core.View{
		TestID: fmt.Sprintf("first-run-tip-%d", tipIdx),
		Role:   core.RoleDialog,
		Label:  body,
		Style: core.Style{
			Position:     core.Absolute,
			Left:         core.Pt(left),
			Top:          core.Pt(top),
			Width:        core.Pt(264),
			Padding:      core.All(12),
			CornerRadius: specRadiusCard,
			Background:   p.Accent.Fill,
			BorderWidth:  1.5,
			BorderColor:  p.Accent.Edge,
			Gap:          8,
		},
		Children: []core.Node{
			core.Text{
				TestID:  "first-run-tip-step",
				Content: stepLabel,
				Style: core.Style{
					FontSize:   11,
					FontFamily: core.FontMonospace,
					Color:      p.TextMuted,
				},
			},
			core.Text{
				TestID:  "first-run-tip-body",
				Content: body,
				Style: core.Style{
					FontSize: 12.5,
					Color:    p.TextPrimary,
				},
			},
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: justify,
					Gap:            8,
				},
				Children: footer,
			},
		},
	}
}

func firstRunPreviewShapeNode(p theme.Palette, sh FirstRunPreviewShape) core.Node {
	bg := p.LineRaised
	radius := core.Points(2)
	switch sh.Kind {
	case "room":
		bg = p.Panel
	case "floor", "platform":
		bg = p.LineRaised
	case "divider":
		bg = p.Line
	case "object":
		bg = p.Accent.Fill
		radius = 4
	default:
		bg = p.Added.Fill
		radius = 4
	}
	st := core.Style{
		Position:     core.Absolute,
		Left:         core.Pt(core.Points(sh.X)),
		Top:          core.Pt(core.Points(sh.Y)),
		Width:        core.Pt(core.Points(sh.W)),
		Height:       core.Pt(core.Points(sh.H)),
		Background:   bg,
		CornerRadius: radius,
	}
	switch sh.Outline {
	case "added":
		st.OutlineWidth = 1.5
		st.OutlineColor = p.Added.Edge
		st.OutlineOffset = 2
	case "agent":
		st.OutlineWidth = 1.5
		st.OutlineColor = p.Agent.Edge
		st.OutlineOffset = 2
	case "problem":
		st.OutlineWidth = 1.5
		st.OutlineColor = p.Removed.Edge
		st.OutlineOffset = 2
	case "planned":
		st.BorderWidth = 1.5
		st.BorderColor = p.Agent.Edge
	}
	return core.View{Style: st}
}

func firstRunGhostNode(p theme.Palette, g FirstRunGhost) core.Node {
	radius := core.Points(3)
	bg := core.Transparent
	if g.Kind == "room" && g.Step > 1 {
		bg = p.Agent.Fill
	}
	box := core.View{
		TestID: "first-run-ghost-" + g.ID,
		Style: core.Style{
			Position:     core.Absolute,
			Left:         core.Pt(core.Points(g.X)),
			Top:          core.Pt(core.Points(g.Y)),
			Width:        core.Pt(core.Points(g.W)),
			Height:       core.Pt(core.Points(g.H)),
			CornerRadius: radius,
			Background:   bg,
			BorderWidth:  1.5,
			BorderColor:  p.Agent.Edge,
		},
	}
	if g.Label == "" {
		return box
	}
	pillTop := g.Y - 26
	if pillTop < 44 {
		pillTop = 44
	}
	pill := core.View{
		Style: core.Style{
			Position:     core.Absolute,
			Left:         core.Pt(core.Points(g.X + 4)),
			Top:          core.Pt(core.Points(pillTop)),
			Height:       core.Pt(20),
			Padding:      core.Edges{Left: 2, Right: 8},
			CornerRadius: 10,
			Background:   p.PillBg,
			BorderWidth:  1,
			BorderColor:  p.Agent.Edge,
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          6,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:          core.Pt(16),
					Height:         core.Pt(16),
					CornerRadius:   8,
					Background:     p.Agent.Fg,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
				},
				Children: []core.Node{
					core.Text{
						Content: fmt.Sprintf("%d", g.Step),
						Style:   core.Style{FontSize: 10, FontWeight: core.FontWeightSemibold, Color: p.Agent.OnFill},
					},
				},
			},
			core.Text{
				Content: g.Label,
				Style:   core.Style{FontSize: 11, Color: p.Agent.Fg},
			},
		},
	}
	return core.View{
		Style: core.Style{
			Position: core.Absolute,
			Left:     core.Pt(0),
			Top:      core.Pt(0),
			Right:    core.Pt(0),
			Bottom:   core.Pt(0),
		},
		Children: []core.Node{box, pill},
	}
}

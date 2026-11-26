package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec-derived alpha tints for Settings › Permissions controls.
// Every base color comes strictly from docs/design/editor/Spec.dc.html (accent #3B7BFF, added #4FD38A, changed #E9A84A, problem #FF6B6B, warning #F2C14E/#2A2414/#4A3F1F, agent #9B7BFF).
var (
	permAccentFill   = core.RGBA(0x3B7BFF, 0.14)
	permAccentRowBg  = core.RGBA(0x3B7BFF, 0.10)
	permAccentBorder = core.RGBA(0x3B7BFF, 0.55)
	permAllowFill    = core.RGBA(0x4FD38A, 0.16)
	permAllowBorder  = core.RGBA(0x4FD38A, 0.45)
	permDenyFill     = core.RGBA(0xFF6B6B, 0.16)
	permDenyBorder   = core.RGBA(0xFF6B6B, 0.50)
	permChangedFill  = core.RGBA(0xE9A84A, 0.16)
	permChangedEdge  = core.RGBA(0xE9A84A, 0.45)
)

var permissionActionsOrder = []PermissionAction{
	PermissionActionEdit,
	PermissionActionCreate,
	PermissionActionDelete,
	PermissionActionRun,
	PermissionActionCode,
}

var permissionDecisionsOrder = []PermissionDecision{
	PermissionAllow,
	PermissionAsk,
	PermissionDeny,
}

// PermissionsSettingsView renders the full-window Settings › Permissions screen matching
// docs/design/editor/Settings.dc.html and docs/design/editor/Permissions.dc.html using only Spec.dc.html tokens.
func PermissionsSettingsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, func() {
		if ed.PermissionsSettingsOpen() {
			ed.ClosePermissionsSettings()
		}
	})

	permRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.permissionsStateRev()
	})

	// Stable ref pool for Allow / Ask / Deny radio controls so arrow navigation and blocked-step links move real keyboard focus to the matching rule row.
	radioRefs := make(map[string]*core.Ref)
	getRadioRef := func(row int, dec PermissionDecision) *core.Ref {
		key := fmt.Sprintf("%d:%s", row, dec)
		if r, ok := radioRefs[key]; ok {
			return r
		}
		r := &core.Ref{}
		radioRefs[key] = r
		return r
	}

	focusSeqMemo := core.NewMemo(cx, func() uint64 {
		_ = permRev.Get()
		return ed.perms.focusSeq
	})
	cx.Effect(func() {
		_ = focusSeqMemo.Get()
		if !ed.PermissionsSettingsOpen() || !ed.perms.focusPending {
			return
		}
		idx := ed.PermissionsFocusedRule()
		perms := ed.Permissions()
		if idx >= 0 && idx < len(perms.Rules) {
			ed.perms.focusPending = false
			ref := getRadioRef(idx, perms.Rules[idx].Decision)
			cx.Post(func() {
				ref.Focus()
			})
		}
	})

	return core.View{
		TestID: "permissions-settings-view",
		Role:   core.RoleRegion,
		Label:  ed.T("permissions.title"),
		Style: core.Style{
			Direction:  core.Row,
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: specBg,
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			permissionsSidebarView(cx, ed, permRev),
			core.View{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinWidth:  core.Pt(0),
					Height:    core.Pct(100),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					permissionsTopBarView(ed),
					core.View{
						Style: core.Style{
							Direction: core.Row,
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							MinHeight: core.Pt(0),
							Overflow:  core.Hidden,
						},
						Children: []core.Node{
							permissionsMainPaneView(cx, ed, permRev, getRadioRef),
							permissionsAgentRailView(cx, ed, permRev),
						},
					},
				},
			},
		},
	}
}

func permissionsSidebarView(cx *core.Scope, ed *Editor, permRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = permRev.Get()
		perms := ed.Permissions()
		projName := ed.T("permissions.noProject")
		if ed.Project != nil && strings.TrimSpace(ed.Project.Name) != "" {
			projName = ed.Project.Name
		} else if ed.ProjectPath != "" {
			projName = filepath.Base(filepath.Dir(ed.ProjectPath))
		}

		profileMeta := ed.localizedProfileName(perms.Profile)
		profileMetaColor := specTextMuted
		if perms.IsModified() {
			profileMeta = profileMeta + " · " + ed.T("permissions.changed")
			profileMetaColor = specChanged
		}

		sectionTitle := func(text string, topPad core.Points) core.Node {
			return core.Text{
				Content: text,
				Style: core.Style{
					Padding:       core.Edges{Top: topPad, Right: 10, Bottom: 4, Left: 10},
					Color:         specTextMuted,
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
				},
			}
		}

		navButton := func(testID, label, meta string, active bool, metaColor core.Color, onPress func()) core.Node {
			bg := core.Color(0)
			fg := specTextSecondary
			if active {
				bg = permAccentFill
				fg = specTextStrong
			}
			children := []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						FlexGrow: 1,
						Color:    fg,
						FontSize: 12.5,
					},
				},
			}
			if meta != "" {
				children = append(children, core.Text{
					TestID:  testID + "-meta",
					Content: meta,
					Style: core.Style{
						Color:    metaColor,
						FontSize: 11,
					},
				})
			}
			return core.View{
				TestID: testID,
				Role:   core.RoleButton,
				Label:  label,
				OnPress: func(core.PressEvent) {
					if onPress != nil {
						onPress()
					}
				},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(32),
					Padding:      core.Symmetric(0, 10),
					Gap:          8,
					CornerRadius: 8,
					Background:   bg,
					FocusVisible: specFocusRingStyle,
				},
				Children: children,
			}
		}

		return core.View{
			TestID: "permissions-sidebar",
			Role:   core.RoleNavigation,
			Label:  ed.T("permissions.settingsTitle"),
			Style: core.Style{
				Width:       core.Pt(264),
				FlexShrink:  0,
				Background:  specPanel,
				Borders:     core.Edges{Right: 1},
				BorderColor: specLine,
				Padding:     core.Symmetric(16, 10),
				Gap:         2,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
						Padding:    core.Edges{Top: 0, Right: 10, Bottom: 14, Left: 10},
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(22),
								Height:       core.Pt(22),
								CornerRadius: 6,
								Background:   specAccent,
							},
						},
						core.Text{
							Content: ed.T("permissions.settingsTitle"),
							Style:   core.Style{Color: specTextStrong, FontSize: 13, FontWeight: 600},
						},
						core.Text{
							Content: "· " + projName,
							Style:   core.Style{Color: specTextMuted, FontSize: 12.5},
						},
					},
				},
				sectionTitle(ed.T("permissions.sectionAgent"), 6),
				navButton("perm-nav-permissions", ed.T("permissions.title"), profileMeta, true, profileMetaColor, nil),
				sectionTitle(ed.T("permissions.sectionProject"), 14),
				navButton("perm-nav-project", ed.T("menu.projectSettings"), "", false, specTextMuted, func() {
					ed.ClosePermissionsSettings()
					ed.SetProjectSettingsOpen(true)
				}),
				navButton("perm-nav-input", ed.T("permissions.navInputMap"), "", false, specTextMuted, func() {
					ed.ClosePermissionsSettings()
					ed.SetInputMapOpen(true)
				}),
				navButton("perm-nav-audio", ed.T("menu.audioMixer"), "", false, specTextMuted, func() {
					ed.ClosePermissionsSettings()
					ed.SetAudioMixerOpen(true)
				}),
				navButton("perm-nav-export", ed.T("permissions.navExport"), "", false, specTextMuted, func() {
					ed.ClosePermissionsSettings()
					ed.SetExportPanelOpen(true)
				}),
				sectionTitle(ed.T("permissions.sectionEditor"), 14),
				navButton("perm-nav-general", ed.T("permissions.navGeneral"), "", false, specTextMuted, func() {
					ed.SetEditorSettingsOpen(true)
				}),
				core.View{Style: core.Style{FlexGrow: 1}},
				core.View{
					TestID: "perm-close",
					Role:   core.RoleButton,
					Label:  ed.T("permissions.backToEditor"),
					OnPress: func(core.PressEvent) {
						ed.ClosePermissionsSettings()
					},
					Style: core.Style{
						Direction:      core.Row,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						Height:         core.Pt(32),
						Padding:        core.Symmetric(0, 12),
						Gap:            8,
						CornerRadius:   8,
						Background:     specRaised,
						Borders:        core.All(1),
						BorderColor:    specLineRaised,
						FocusVisible:   specFocusRingStyle,
					},
					Children: []core.Node{
						core.Icon{
							SVG:   lucide.ArrowLeft(),
							Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: specTextSecondary},
						},
						core.Text{
							Content: ed.T("permissions.backToEditor"),
							Style:   core.Style{Color: specText, FontSize: 12.5, FontWeight: 500},
						},
					},
				},
			},
		}
	})
}

func permissionsTopBarView(ed *Editor) core.Node {
	return core.View{
		Style: core.Style{
			Padding:     core.Symmetric(14, 28),
			Borders:     core.Edges{Bottom: 1},
			BorderColor: specLine,
			Background:  specBg,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(40),
					Padding:      core.Symmetric(0, 14),
					Gap:          10,
					CornerRadius: 10,
					Background:   specRaised,
					Borders:      core.All(1),
					BorderColor:  specLineRaised,
				},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.Sparkles(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: specAgent},
					},
					core.Text{
						Content: ed.T("permissions.searchHint"),
						Style:   core.Style{FlexGrow: 1, Color: specTextMuted, FontSize: 12.5},
					},
					core.Text{
						Content: "Esc",
						Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specTextMuted},
					},
				},
			},
		},
	}
}

func permissionsMainPaneView(
	cx *core.Scope,
	ed *Editor,
	permRev *core.Memo[uint64],
	getRadioRef func(row int, dec PermissionDecision) *core.Ref,
) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = permRev.Get()
		perms := ed.Permissions()
		modified := perms.IsModified()
		focusedRule := ed.PermissionsFocusedRule()

		profileCard := func(profile PermissionProfile, descKey string) core.Node {
			active := perms.Profile == profile
			bg := specRaised
			borderColor := specLineRaised
			borderW := core.Points(1)
			if active {
				bg = permAccentFill
				borderColor = specAccent
				borderW = 1.5
			}
			titleRowChildren := []core.Node{
				core.Text{
					TestID:  fmt.Sprintf("perm-profile-%s-name", profile),
					Content: ed.localizedProfileName(profile),
					Style:   core.Style{Color: specTextStrong, FontSize: 13, FontWeight: 600},
				},
			}
			if active && modified {
				titleRowChildren = append(titleRowChildren, core.View{
					TestID: "perm-profile-changed-badge",
					Style: core.Style{
						Height:         core.Pt(20),
						Padding:        core.Symmetric(0, 8),
						CornerRadius:   6,
						Background:     permChangedFill,
						Borders:        core.All(1),
						BorderColor:    permChangedEdge,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("permissions.changed"),
							Style:   core.Style{Color: specChanged, FontSize: 11, FontWeight: 600},
						},
					},
				})
			}
			return core.View{
				TestID: fmt.Sprintf("perm-profile-%s", profile),
				Role:   core.RoleButton,
				Label:  ed.localizedProfileName(profile),
				OnPress: func(core.PressEvent) {
					ed.report(ed.SelectPermissionProfile(profile))
				},
				Style: core.Style{
					FlexGrow:     1,
					FlexBasis:    core.Pt(0),
					MinWidth:     core.Pt(0),
					Padding:      core.All(14),
					Gap:          6,
					CornerRadius: 12,
					Background:   bg,
					Borders:      core.All(borderW),
					BorderColor:  borderColor,
					FocusVisible: specFocusRingStyle,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Gap:        8,
						},
						Children: titleRowChildren,
					},
					core.Text{
						Content: ed.T(descKey),
						Style: core.Style{
							Color:      specTextSecondary,
							FontSize:   12,
							LineHeight: 18,
						},
					},
				},
			}
		}

		headerChildren := []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Gap: 4},
				Children: []core.Node{
					core.Text{
						TestID:  "perm-page-title",
						Content: ed.T("permissions.title"),
						Style:   core.Style{Color: specTextStrong, FontSize: 18, FontWeight: 600},
					},
					core.Text{
						Content: ed.T("permissions.subtitle"),
						Style:   core.Style{Color: specTextMuted, FontSize: 12.5, LineHeight: 19},
					},
				},
			},
			core.View{
				TestID: "perm-scope-pill",
				Style: core.Style{
					Height:         core.Pt(24),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   12,
					Background:     permAccentFill,
					Borders:        core.All(1),
					BorderColor:    permAccentBorder,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FlexShrink:     0,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("permissions.scopeShared"),
						Style:   core.Style{Color: specText, FontSize: 11.5, FontWeight: 500},
					},
				},
			},
		}

		rulesBarRight := make([]core.Node, 0, 2)
		if modified {
			rulesBarRight = append(rulesBarRight, core.View{
				TestID: "perm-reset-preset",
				Role:   core.RoleButton,
				Label:  ed.T("permissions.resetPreset"),
				OnPress: func(core.PressEvent) {
					ed.report(ed.ResetPermissionsToPreset())
				},
				Style: core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   8,
					Background:     specRaised,
					Borders:        core.All(1),
					BorderColor:    specLineRaised,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   specFocusRingStyle,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("permissions.resetPreset"),
						Style:   core.Style{Color: specText, FontSize: 11.5, FontWeight: 500},
					},
				},
			})
		}
		rulesBarRight = append(rulesBarRight, core.View{
			TestID: "perm-add-rule",
			Role:   core.RoleButton,
			Label:  ed.T("permissions.addRule"),
			OnPress: func(core.PressEvent) {
				ed.report(ed.AddPermissionRule(PermissionRule{
					Action:   PermissionActionEdit,
					Patterns: []string{"scenes/**"},
					Decision: PermissionAsk,
				}))
			},
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 10),
				Gap:            6,
				CornerRadius:   8,
				Background:     specLineRaised,
				FocusVisible:   specFocusRingStyle,
			},
			Children: []core.Node{
				core.Icon{
					SVG:   lucide.Plus(),
					Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specTextStrong},
				},
				core.Text{
					Content: ed.T("permissions.addRule"),
					Style:   core.Style{Color: specTextStrong, FontSize: 11.5, FontWeight: 500},
				},
			},
		})

		rows := make([]core.Node, 0, len(perms.Rules)+1)
		// Table header row
		rows = append(rows, core.View{
			Role: core.RoleRow,
			Style: core.Style{
				Direction:   core.Row,
				AlignItems:  core.AlignCenter,
				Gap:         12,
				Padding:     core.Symmetric(10, 14),
				Borders:     core.Edges{Bottom: 1},
				BorderColor: specLine,
			},
			Children: []core.Node{
				core.Text{
					Content: "#",
					Style:   core.Style{Width: core.Pt(24), Color: specTextMuted, FontSize: 11, FontWeight: 600},
				},
				core.Text{
					Content: ed.T("permissions.colAction"),
					Style:   core.Style{Width: core.Pt(88), Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6},
				},
				core.Text{
					Content: ed.T("permissions.colFiles"),
					Style:   core.Style{FlexGrow: 1, Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6},
				},
				core.Text{
					Content: ed.T("permissions.colDecision"),
					Style:   core.Style{Width: core.Pt(196), Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6},
				},
				core.Text{
					Content: ed.T("permissions.colOrder"),
					Style:   core.Style{Width: core.Pt(92), Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6},
				},
			},
		})

		for idx, rule := range perms.Rules {
			rowIdx := idx
			r := rule
			isFocused := focusedRule == rowIdx
			rowBg := core.Color(0)
			rowBorders := core.Edges{Bottom: 1}
			rowBorderColor := specLine
			if isFocused {
				rowBg = permAccentRowBg
				rowBorders = core.Edges{Left: 3, Bottom: 1}
				rowBorderColor = specAccent
			}
			numColor := specTextMuted
			if isFocused {
				numColor = specAccent
			}

			patternStr := strings.Join(r.Patterns, ", ")
			patVal := core.NewSignal(cx, patternStr)
			commitPatterns := func(raw string) {
				parsed := ParsePatternList(r.Action, raw)
				if len(parsed) == 0 {
					patVal.Set(patternStr)
					return
				}
				if err := ed.SetPermissionRulePatterns(rowIdx, parsed); err != nil {
					ed.report(err)
					patVal.Set(patternStr)
				}
			}

			decisionBtn := func(dec PermissionDecision, labelKey string) core.Node {
				selected := r.Decision == dec
				bg := core.Color(0)
				fg := specTextMuted
				border := core.Color(0)
				borderW := core.Points(0)
				weight := core.FontWeight(400)
				if selected {
					weight = core.FontWeight(600)
					borderW = 1
					switch dec {
					case PermissionAllow:
						bg = permAllowFill
						border = permAllowBorder
						fg = specAdded
					case PermissionAsk:
						bg = specWarningFill
						border = specWarningEdge
						fg = specWarning
					case PermissionDeny:
						bg = permDenyFill
						border = permDenyBorder
						fg = specProblem
					}
				}
				ref := getRadioRef(rowIdx, dec)
				return core.View{
					Ref:       ref,
					TestID:    fmt.Sprintf("perm-rule-%d-%s", rowIdx, dec),
					Role:      core.RoleRadio,
					Label:     fmt.Sprintf("Rule %d %s", rowIdx+1, dec.DisplayName()),
					Checked:   func() bool { return selected },
					Focusable: func() bool { return selected },
					OnPress: func(core.PressEvent) {
						ed.FocusPermissionRule(rowIdx)
						ed.report(ed.SetPermissionRuleDecision(rowIdx, dec))
					},
					OnKeyDown: func(ev core.KeyEvent) bool {
						step := 0
						switch ev.Key {
						case "right", "down":
							step = 1
						case "left", "up":
							step = -1
						default:
							return false
						}
						curPos := 0
						for k, d := range permissionDecisionsOrder {
							if d == dec {
								curPos = k
								break
							}
						}
						nextPos := (curPos + step + len(permissionDecisionsOrder)) % len(permissionDecisionsOrder)
						nextDec := permissionDecisionsOrder[nextPos]
						ed.FocusPermissionRule(rowIdx)
						ed.report(ed.SetPermissionRuleDecision(rowIdx, nextDec))
						nextRef := getRadioRef(rowIdx, nextDec)
						cx.Post(func() {
							nextRef.Focus()
						})
						return true
					},
					Style: core.Style{
						FlexGrow:       1,
						Height:         core.Pt(24),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						CornerRadius:   6,
						Background:     bg,
						Borders:        core.All(borderW),
						BorderColor:    border,
						FocusVisible:   specFocusRingStyle,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T(labelKey),
							Style:   core.Style{Color: fg, FontSize: 11.5, FontWeight: weight},
						},
					},
				}
			}

			orderBtn := func(testID, label string, svg *core.SVG, disabled bool, danger bool, onPress func()) core.Node {
				fg := specTextSecondary
				if danger {
					fg = specProblem
				}
				if disabled {
					fg = specTextMuted
				}
				opacity := core.Alpha(1)
				if disabled {
					opacity = core.Alpha(0.4)
				}
				return core.View{
					TestID:   testID,
					Role:     core.RoleButton,
					Label:    label,
					Disabled: func() bool { return disabled },
					OnPress: func(core.PressEvent) {
						if !disabled && onPress != nil {
							onPress()
						}
					},
					Style: core.Style{
						Width:          core.Pt(28),
						Height:         core.Pt(28),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						CornerRadius:   6,
						Background:     specRaised,
						Borders:        core.All(1),
						BorderColor:    specLineRaised,
						Opacity:        opacity,
						FocusVisible:   specFocusRingStyle,
					},
					Children: []core.Node{
						core.Icon{
							SVG:   svg,
							Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: fg},
						},
					},
				}
			}

			rows = append(rows, core.View{
				TestID: fmt.Sprintf("perm-rule-row-%d", rowIdx),
				Role:   core.RoleRow,
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         12,
					Padding:     core.Symmetric(8, 14),
					Background:  rowBg,
					Borders:     rowBorders,
					BorderColor: rowBorderColor,
				},
				Children: []core.Node{
					core.Text{
						TestID:  fmt.Sprintf("perm-rule-%d-num", rowIdx),
						Content: fmt.Sprintf("%d", rowIdx+1),
						Style:   core.Style{Width: core.Pt(24), FontFamily: "monospace", FontSize: 11.5, FontWeight: 600, Color: numColor},
					},
					core.View{
						TestID: fmt.Sprintf("perm-rule-%d-action", rowIdx),
						Role:   core.RoleButton,
						Label:  fmt.Sprintf("Rule %d action: %s", rowIdx+1, r.Action.DisplayName()),
						OnPress: func(core.PressEvent) {
							ed.FocusPermissionRule(rowIdx)
							nextAct := permissionActionsOrder[0]
							for k, a := range permissionActionsOrder {
								if a == r.Action {
									nextAct = permissionActionsOrder[(k+1)%len(permissionActionsOrder)]
									break
								}
							}
							ed.report(ed.SetPermissionRuleAction(rowIdx, nextAct))
						},
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Width:          core.Pt(88),
							Height:         core.Pt(28),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     specRaised,
							Borders:        core.All(1),
							BorderColor:    specLineRaised,
							FocusVisible:   specFocusRingStyle,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.localizedActionName(r.Action),
								Style:   core.Style{Color: specTextStrong, FontSize: 12, FontWeight: 500},
							},
							core.Icon{
								SVG:   lucide.ChevronDown(),
								Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: specTextMuted},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.Input{
								TestID: fmt.Sprintf("perm-rule-%d-patterns", rowIdx),
								Label:  fmt.Sprintf("Rule %d patterns", rowIdx+1),
								Value:  patVal.Get,
								OnChange: func(v string) {
									patVal.Set(v)
								},
								OnSubmit: func(v string) {
									commitPatterns(v)
								},
								OnBlur: func() {
									if patVal.Get() != patternStr {
										commitPatterns(patVal.Get())
									}
								},
								Style: core.Style{
									Height:       core.Pt(28),
									Padding:      core.Symmetric(0, 8),
									CornerRadius: 6,
									Background:   specBg,
									Borders:      core.All(1),
									BorderColor:  specLineRaised,
									Color:        specText,
									FontFamily:   "monospace",
									FontSize:     11.5,
									FocusVisible: specFocusRingStyle,
								},
							},
						},
					},
					core.View{
						TestID: fmt.Sprintf("perm-rule-%d-decision", rowIdx),
						Role:   core.RoleRadioGroup,
						Label:  fmt.Sprintf("Rule %d decision", rowIdx+1),
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Width:        core.Pt(196),
							Padding:      core.All(3),
							Gap:          2,
							CornerRadius: 8,
							Background:   specRaised,
							Borders:      core.All(1),
							BorderColor:  specLineRaised,
						},
						Children: []core.Node{
							decisionBtn(PermissionAllow, "permissions.allow"),
							decisionBtn(PermissionAsk, "permissions.ask"),
							decisionBtn(PermissionDeny, "permissions.deny"),
						},
					},
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Width:      core.Pt(92),
							Gap:        4,
						},
						Children: []core.Node{
							orderBtn(fmt.Sprintf("perm-rule-%d-up", rowIdx), ed.T("permissions.moveUp"), lucide.ChevronUp(), rowIdx == 0, false, func() {
								ed.report(ed.MovePermissionRule(rowIdx, -1))
							}),
							orderBtn(fmt.Sprintf("perm-rule-%d-down", rowIdx), ed.T("permissions.moveDown"), lucide.ChevronDown(), rowIdx == len(perms.Rules)-1, false, func() {
								ed.report(ed.MovePermissionRule(rowIdx, 1))
							}),
							orderBtn(fmt.Sprintf("perm-rule-%d-remove", rowIdx), ed.T("permissions.removeRule"), lucide.X(), false, true, func() {
								ed.report(ed.RemovePermissionRule(rowIdx))
							}),
						},
					},
				},
			})
		}

		if len(perms.Rules) == 0 {
			rows = append(rows, core.View{
				Style: core.Style{Padding: core.All(16), AlignItems: core.AlignCenter},
				Children: []core.Node{
					core.Text{
						Content: ed.T("permissions.emptyRules"),
						Style:   core.Style{Color: specTextMuted, FontSize: 12.5},
					},
				},
			})
		}

		bodyChildren := []core.Node{
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignEnd,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            16,
				},
				Children: headerChildren,
			},
		}

		if loadErr := ed.PermissionsErr(); loadErr != nil {
			bodyChildren = append(bodyChildren, core.View{
				TestID: "perm-load-error",
				Role:   core.RoleAlert,
				Style: core.Style{
					Padding:      core.Symmetric(10, 14),
					CornerRadius: 10,
					Background:   permDenyFill,
					Borders:      core.All(1),
					BorderColor:  permDenyBorder,
				},
				Children: []core.Node{
					core.Text{
						Content: loadErr.Error(),
						Style:   core.Style{Color: specProblem, FontSize: 12},
					},
				},
			})
		}

		bodyChildren = append(bodyChildren,
			core.View{
				TestID: "perm-profiles-row",
				Style: core.Style{
					Direction: core.Row,
					Gap:       12,
				},
				Children: []core.Node{
					profileCard(PermissionProfileCautious, "permissions.profile.cautiousDesc"),
					profileCard(PermissionProfileBalanced, "permissions.profile.balancedDesc"),
					profileCard(PermissionProfileAutonomous, "permissions.profile.autonomousDesc"),
				},
			},
			core.View{
				Style: core.Style{
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifySpaceBetween,
					Gap:            12,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("permissions.rulesHeader"),
						Style:   core.Style{Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6},
					},
					core.View{
						Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: rulesBarRight,
					},
				},
			},
			core.View{
				TestID: "perm-rules-table",
				Role:   core.RoleTable,
				Label:  ed.T("permissions.rulesHeader"),
				Style: core.Style{
					CornerRadius: 12,
					Background:   specPanel,
					Borders:      core.All(1),
					BorderColor:  specLine,
					Overflow:     core.Hidden,
				},
				Children: rows,
			},
			core.Text{
				Content: ed.T("permissions.mcpOnlyNote"),
				Style:   core.Style{Color: specTextMuted, FontSize: 11.5, LineHeight: 18},
			},
		)

		return core.Scroll{
			Style: core.Style{
				FlexGrow:  1,
				FlexBasis: core.Pt(0),
				MinWidth:  core.Pt(0),
				MinHeight: core.Pt(0),
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Padding: core.Symmetric(22, 28),
						Gap:     16,
					},
					Children: bodyChildren,
				},
			},
		}
	})
}

func permissionsAgentRailView(cx *core.Scope, ed *Editor, permRev *core.Memo[uint64]) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = permRev.Get()
		perms := ed.Permissions()
		allowN, askN, denyN := 0, 0, 0
		for _, r := range perms.Rules {
			switch r.Decision {
			case PermissionAllow:
				allowN++
			case PermissionAsk:
				askN++
			case PermissionDeny:
				denyN++
			}
		}

		children := []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 4, Background: specAgent},
					},
					core.Text{
						Content: ed.T("permissions.agentRailTitle"),
						Style:   core.Style{Color: specTextStrong, FontSize: 12.5, FontWeight: 500},
					},
				},
			},
			core.Text{
				Content: ed.T("permissions.agentRailSummary",
					l10n.Param("allow", allowN),
					l10n.Param("ask", askN),
					l10n.Param("deny", denyN),
				),
				Style: core.Style{Color: specTextSecondary, FontSize: 12, LineHeight: 19},
			},
		}

		// Show pending Ask permission requests.
		pending := ed.PendingPermissionRequests()
		if len(pending) > 0 {
			children = append(children, permissionsAskCardNode(ed, pending[0], "perm-rail-ask"))
		}

		// Show blocked steps with clickable links to the exact matching rule row.
		blocked := ed.BlockedPermissions()
		if len(blocked) > 0 {
			children = append(children, core.Text{
				Content: ed.T("permissions.blockedHeader"),
				Style:   core.Style{Color: specTextMuted, FontSize: 11, FontWeight: 600, LetterSpacing: 0.6, Padding: core.Edges{Top: 6}},
			})
			for i := len(blocked) - 1; i >= 0 && len(blocked)-1-i < 3; i-- {
				step := blocked[i]
				children = append(children, permissionsBlockedCardNode(ed, step, fmt.Sprintf("perm-blocked-link-%d", i)))
			}
		}

		return core.View{
			TestID: "permissions-agent-rail",
			Style: core.Style{
				Width:       core.Pt(300),
				FlexShrink:  0,
				Background:  specPanel,
				Borders:     core.Edges{Left: 1},
				BorderColor: specLine,
				Padding:     core.All(18),
				Gap:         12,
			},
			Children: children,
		}
	})
}

func permissionsBlockedCardNode(ed *Editor, step BlockedPermissionStep, linkTestID string) core.Node {
	ruleSummary := step.Evaluation.Reason
	if step.Evaluation.Rule != nil {
		ruleSummary = step.Evaluation.Rule.Summary()
	}
	linkLabel := step.Evaluation.Link
	if linkLabel == "" {
		linkLabel = "Settings › Permissions"
	}
	stepID := step.ID

	return core.View{
		TestID: linkTestID + "-card",
		Style: core.Style{
			Padding:      core.All(12),
			Gap:          8,
			CornerRadius: 12,
			Background:   specRaised,
			Borders:      core.All(1),
			BorderColor:  specLineRaised,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:          core.Pt(18),
							Height:         core.Pt(18),
							CornerRadius:   9,
							Background:     specLineRaised,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Icon{
								SVG:   lucide.Lock(),
								Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specText},
							},
						},
					},
					core.Text{
						Content: ed.T("permissions.blockedPrefix", l10n.Param("summary", step.Summary)),
						Style:   core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Color: specTextStrong, FontSize: 12, FontWeight: 500},
					},
				},
			},
			core.View{
				TestID: linkTestID,
				Role:   core.RoleLink,
				Label:  linkLabel,
				OnPress: func(core.PressEvent) {
					ed.OpenBlockedPermissionRule(stepID)
				},
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 8,
					Background:   specBg,
					Borders:      core.All(1),
					BorderColor:  specLine,
					FocusVisible: specFocusRingStyle,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: ruleSummary,
								Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specTextStrong},
							},
							core.Text{
								Content: linkLabel,
								Style:   core.Style{FontSize: 11, Color: specTextMuted},
							},
						},
					},
					core.Icon{
						SVG:   lucide.ChevronRight(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), FlexShrink: 0, Color: specTextMuted},
					},
				},
			},
		},
	}
}

func permissionsAskCardNode(ed *Editor, req PendingPermissionRequest, testPrefix string) core.Node {
	reqID := req.ID
	rule := AlwaysRuleForEvaluation(req.Evaluation)
	return core.View{
		TestID: testPrefix + "-card",
		Role:   core.RoleAlertDialog,
		Label:  ed.T("permissions.askTitle", l10n.Param("summary", req.Summary)),
		Style: core.Style{
			Padding:      core.All(12),
			Gap:          10,
			CornerRadius: 12,
			Background:   specWarningFill,
			Borders:      core.All(1),
			BorderColor:  specWarningEdge,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.Icon{
						SVG:   lucide.ShieldAlert(),
						Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: specWarning},
					},
					core.Text{
						Content: ed.T("permissions.askTitle", l10n.Param("summary", req.Summary)),
						Style:   core.Style{FlexGrow: 1, Color: specWarning, FontSize: 13, FontWeight: 600},
					},
				},
			},
			core.View{
				Style: core.Style{Gap: 6},
				Children: []core.Node{
					core.View{
						TestID: testPrefix + "-once",
						Role:   core.RoleButton,
						Label:  ed.T("permissions.allowOnce"),
						OnPress: func(core.PressEvent) {
							ed.report(ed.RespondPermissionRequest(reqID, PermissionResponseAllowOnce))
						},
						Style: core.Style{
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     specAccent,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   specFocusRingStyle,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("permissions.allowOnce"),
								// Per Spec.dc.html contrast table: #101216 on accent #3B7BFF (4.88:1 AA, weight 600).
								Style: core.Style{Color: specBg, FontSize: 12, FontWeight: 600},
							},
						},
					},
					core.View{
						TestID: testPrefix + "-always",
						Role:   core.RoleButton,
						Label:  ed.T("permissions.allowAlways"),
						OnPress: func(core.PressEvent) {
							ed.report(ed.RespondPermissionRequest(reqID, PermissionResponseAlways))
						},
						Style: core.Style{
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Background:     specRaised,
							Borders:        core.All(1),
							BorderColor:    specWarningEdge,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   specFocusRingStyle,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("permissions.allowAlways"),
								Style:   core.Style{Color: specTextStrong, FontSize: 12, FontWeight: 500},
							},
						},
					},
					core.View{
						TestID: testPrefix + "-deny",
						Role:   core.RoleButton,
						Label:  ed.T("permissions.deny"),
						OnPress: func(core.PressEvent) {
							ed.report(ed.RespondPermissionRequest(reqID, PermissionResponseDeny))
						},
						Style: core.Style{
							Height:         core.Pt(30),
							Padding:        core.Symmetric(0, 12),
							CornerRadius:   8,
							Borders:        core.All(1),
							BorderColor:    specWarningEdge,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							FocusVisible:   specFocusRingStyle,
						},
						Children: []core.Node{
							core.Text{
								Content: ed.T("permissions.deny"),
								Style:   core.Style{Color: specText, FontSize: 12},
							},
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					Padding:      core.Symmetric(8, 10),
					Gap:          4,
					CornerRadius: 8,
					Background:   specBg,
				},
				Children: []core.Node{
					core.Text{
						Content: rule.Summary(),
						Style:   core.Style{FontFamily: "monospace", FontSize: 11, Color: specTextStrong},
					},
					core.Text{
						Content: req.Evaluation.Reason,
						Style:   core.Style{FontSize: 11, Color: specTextMuted, LineHeight: 16},
					},
				},
			},
		},
	}
}

// PermissionsToastView renders the floating bottom-center "Always permission rule saved" toast with Show and Undo actions.
func PermissionsToastView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			_, ok := ed.PermissionToast()
			return ok
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				toast, ok := ed.PermissionToast()
				if !ok {
					return core.View{}
				}
				return core.View{
					TestID: "perm-toast-overlay",
					Style: core.Style{
						Position:       core.Absolute,
						Left:           core.Pt(0),
						Right:          core.Pt(0),
						Bottom:         core.Pt(24),
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						PointerEvents:  core.PointerEventsBoxNone,
						ZIndex:         120,
					},
					Children: []core.Node{
						core.View{
							TestID: "perm-toast",
							Role:   core.RoleStatus,
							Label:  toast.Message,
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Height:       core.Pt(40),
								Padding:      core.Edges{Top: 0, Right: 8, Bottom: 0, Left: 16},
								Gap:          12,
								CornerRadius: 12,
								Background:   specRaised,
								Borders:      core.All(1),
								BorderColor:  specLineRaised,
							},
							Children: []core.Node{
								core.Icon{
									SVG:   lucide.Check(),
									Style: core.Style{Width: core.Pt(15), Height: core.Pt(15), Color: specAdded},
								},
								core.Text{
									TestID:  "perm-toast-message",
									Content: toast.Message,
									Style:   core.Style{Color: specTextStrong, FontSize: 12.5, FontWeight: 600},
								},
								core.Text{
									TestID:  "perm-toast-rule",
									Content: toast.Rule.Summary(),
									Style:   core.Style{FontFamily: "monospace", FontSize: 11.5, Color: specTextSecondary},
								},
								core.View{
									TestID: "perm-toast-show",
									Role:   core.RoleButton,
									Label:  ed.T("permissions.toastShow"),
									OnPress: func(core.PressEvent) {
										ed.ShowPermissionToastRule()
									},
									Style: core.Style{
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     specLineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   specFocusRingStyle,
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("permissions.toastShow"),
											Style:   core.Style{Color: specTextStrong, FontSize: 12, FontWeight: 500},
										},
									},
								},
								core.View{
									TestID: "perm-toast-undo",
									Role:   core.RoleButton,
									Label:  ed.T("permissions.toastUndo"),
									OnPress: func(core.PressEvent) {
										ed.report(ed.UndoPermissionToast())
									},
									Style: core.Style{
										Height:         core.Pt(28),
										Padding:        core.Symmetric(0, 12),
										CornerRadius:   8,
										Background:     specLineRaised,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   specFocusRingStyle,
									},
									Children: []core.Node{
										core.Text{
											Content: ed.T("permissions.toastUndo"),
											Style:   core.Style{Color: specTextStrong, FontSize: 12, FontWeight: 500},
										},
									},
								},
								core.View{
									TestID: "perm-toast-dismiss",
									Role:   core.RoleButton,
									Label:  ed.T("disk.dismiss"),
									OnPress: func(core.PressEvent) {
										ed.DismissPermissionToast()
									},
									Style: core.Style{
										Width:          core.Pt(28),
										Height:         core.Pt(28),
										CornerRadius:   6,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   specFocusRingStyle,
									},
									Children: []core.Node{
										core.Icon{
											SVG:   lucide.X(),
											Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), Color: specTextMuted},
										},
									},
								},
							},
						},
					},
				}
			})
		},
	}
}

// PermissionsWorkspaceOverlayView surfaces the latest blocked MCP step link or pending Ask request in the main editor workspace while Settings › Permissions is closed, so clicking a blocked step link opens Settings › Permissions with the matching rule focused.
func PermissionsWorkspaceOverlayView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			if ed.PermissionsSettingsOpen() {
				return false
			}
			return len(ed.BlockedPermissions()) > 0 || len(ed.PendingPermissionRequests()) > 0
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				if ed.PermissionsSettingsOpen() {
					return core.View{}
				}
				items := make([]core.Node, 0, 2)
				if pending := ed.PendingPermissionRequests(); len(pending) > 0 {
					items = append(items, permissionsAskCardNode(ed, pending[0], "workspace-perm-ask"))
				}
				if blocked := ed.BlockedPermissions(); len(blocked) > 0 {
					latest := blocked[len(blocked)-1]
					items = append(items, permissionsBlockedCardNode(ed, latest, "workspace-blocked-step-link"))
				}
				if len(items) == 0 {
					return core.View{}
				}
				return core.View{
					TestID: "permissions-workspace-overlay",
					Style: core.Style{
						Position: core.Absolute,
						Right:    core.Pt(16),
						Bottom:   core.Pt(40),
						Width:    core.Pt(340),
						Gap:      8,
						ZIndex:   110,
					},
					Children: items,
				}
			})
		},
	}
}

func (e *Editor) localizedProfileName(p PermissionProfile) string {
	switch p {
	case PermissionProfileCautious:
		return e.T("permissions.profile.cautious")
	case PermissionProfileBalanced:
		return e.T("permissions.profile.balanced")
	case PermissionProfileAutonomous:
		return e.T("permissions.profile.autonomous")
	default:
		return p.DisplayName()
	}
}

func (e *Editor) localizedActionName(a PermissionAction) string {
	switch a {
	case PermissionActionEdit:
		return e.T("permissions.action.edit")
	case PermissionActionCreate:
		return e.T("permissions.action.create")
	case PermissionActionDelete:
		return e.T("permissions.action.delete")
	case PermissionActionRun:
		return e.T("permissions.action.run")
	case PermissionActionCode:
		return e.T("permissions.action.code")
	default:
		return a.DisplayName()
	}
}

package editor

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

func specYouOnlyPill(p theme.Palette, testID, text string) core.Node {
	return core.View{
		TestID: testID,
		Style: core.Style{
			Height:         core.Pt(22),
			Padding:        core.Symmetric(0, 8),
			CornerRadius:   11,
			Background:     p.Warning.Fill,
			BorderWidth:    1,
			BorderColor:    p.Warning.Edge,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				Style: core.Style{
					FontSize:   11,
					FontWeight: 600,
					Color:      p.Warning.Fg,
				},
			},
		},
	}
}

func (e *Editor) localizedDecisionName(d PermissionDecision) string {
	switch d {
	case PermissionAllow:
		return e.T("permissions.allow")
	case PermissionAsk:
		return e.T("permissions.ask")
	case PermissionDeny:
		return e.T("permissions.deny")
	default:
		return d.DisplayName()
	}
}

func specDecisionBadge(p theme.Palette, dec PermissionDecision, muted bool) core.Node {
	return specDecisionBadgeWithText(p, dec, dec.DisplayName(), muted)
}

func specDecisionBadgeLocalized(p theme.Palette, ed *Editor, dec PermissionDecision, muted bool) core.Node {
	return specDecisionBadgeWithText(p, dec, ed.localizedDecisionName(dec), muted)
}

func specDecisionBadgeWithText(p theme.Palette, dec PermissionDecision, text string, muted bool) core.Node {
	bg := p.Raised
	fg := p.TextMuted
	border := p.LineRaised
	if !muted {
		switch dec {
		case PermissionAllow:
			bg = p.Added.Fill
			fg = p.Added.Fg
			border = p.Added.Edge
		case PermissionAsk:
			bg = p.Warning.Fill
			fg = p.Warning.Fg
			border = p.Warning.Edge
		case PermissionDeny:
			bg = p.Removed.Fill
			fg = p.Removed.Fg
			border = p.Removed.Edge
		}
	}
	return core.View{
		Style: core.Style{
			Width:          core.Pt(68),
			Height:         core.Pt(24),
			CornerRadius:   6,
			Background:     bg,
			BorderWidth:    1,
			BorderColor:    border,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FlexShrink:     0,
		},
		Children: []core.Node{
			core.Text{
				Content: text,
				Style: core.Style{
					FontSize:   11.5,
					FontWeight: 600,
					Color:      fg,
				},
			},
		},
	}
}

// PermissionsDraftCardView renders the You-only permissions draft modal card matching
// design/editor/Permissions.dc.html ("You only · A draft the agent cannot approve")
// inside a real ui.Dialog / core.Modal with Tab/Shift-Tab focus trapping and opener focus restoration,
// using strictly theme.Palette tokens in both Dark and Light appearances.
// Nothing is ticked by default; ⌘↵, Keep all, automations, and every profile cannot apply it.
func PermissionsDraftCardView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	type draftKey struct {
		open    bool
		permRev uint64
		dark    bool
		lang    string
	}
	key := core.NewMemo(cx, func() draftKey {
		_ = rev.Get()
		return draftKey{
			open:    ed.PermissionsDraftOpen() && !ed.Playing() && !ed.StartScreenOpen() && !ed.MergeConflictOpen(),
			permRev: ed.permissionsStateRev(),
			dark:    ed.Palette(cx).Dark,
			lang:    ed.ResolvedLanguage(),
		}
	})

	onDialogClose := bindDialogFocusRestore(cx, ed, func() bool {
		return key.Get().open
	})

	content := core.Dynamic(func(cx *core.Scope) core.Node {
		k := key.Get()
		if !k.open {
			return nil
		}
		draft := ed.PermissionsDraft()
		if draft == nil {
			return nil
		}
		p := theme.PaletteFor(k.dark)
		focusRing := p.FocusRing(2)
		ticked := ed.PermissionsDraftTickedCount()
		yamlLines := ed.PermissionsDraftPreviewYAML()

		desc := ed.T("permissions.draft.descNoRequest")
		if draft.Request != "" {
			desc = ed.T("permissions.draft.descWithRequest", l10n.Param("request", draft.Request))
		}

		// Rule table rows.
		tableRows := make([]core.Node, 0, len(draft.Rules)+1)
		tableRows = append(tableRows, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
				Padding:    core.Symmetric(8, 12),
			},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(24), FlexShrink: 0}},
				core.Text{Content: ed.T("permissions.draft.colRule"), Style: core.Style{FlexGrow: 1, FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: p.TextFaint}},
				core.Text{Content: ed.T("permissions.draft.colNow"), Style: core.Style{Width: core.Pt(68), FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: p.TextFaint}},
				core.View{Style: core.Style{Width: core.Pt(18), FlexShrink: 0}},
				core.Text{Content: ed.T("permissions.draft.colDraft"), Style: core.Style{Width: core.Pt(68), FontSize: 11, FontWeight: 600, LetterSpacing: 0.66, Color: p.TextFaint}},
			},
		})

		for i, r := range draft.Rules {
			idx := i
			rule := r
			boxBg := p.Raised
			boxBorder := p.TextFaint
			var boxBorderW core.Points = 1.5
			if rule.Ticked {
				boxBg = p.Accent.Fg
				boxBorder = p.Accent.Fg
				boxBorderW = 0
			}
			var checkKids []core.Node
			if rule.Ticked {
				checkKids = []core.Node{
					core.Canvas{
						Style: core.Style{Width: core.Pt(12), Height: core.Pt(12)},
						Draw: func(d *core.Drawing, _ core.Size) {
							d.SetStrokeColor(p.Accent.OnFill).SetLineWidth(2.0).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
							d.BeginPath().MoveTo(2.2, 6.4).LineTo(4.8, 9.0).LineTo(9.8, 3.4).Stroke()
						},
					},
				}
			}
			tableRows = append(tableRows, core.View{
				TestID: fmt.Sprintf("perm-draft-row-%d", idx),
				Style: core.Style{
					Direction:   core.Row,
					AlignItems:  core.AlignCenter,
					Gap:         8,
					Padding:     core.Symmetric(8, 12),
					Borders:     core.Edges{Top: 1},
					BorderColor: p.Line,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(24), AlignItems: core.AlignCenter, FlexShrink: 0},
						Children: []core.Node{
							core.View{
								TestID: fmt.Sprintf("perm-draft-tick-%d", idx),
								Role:   core.RoleCheckbox,
								Label: ed.T("permissions.draft.tickLabel",
									l10n.Param("action", ed.localizedActionName(rule.Action)),
									l10n.Param("patterns", FormatPermissionPatterns(rule.Action, rule.Patterns)),
									l10n.Param("current", ed.localizedDecisionName(rule.CurrentDecision)),
									l10n.Param("draft", ed.localizedDecisionName(rule.DraftDecision)),
								),
								Checked:   func() bool { return rule.Ticked },
								Focusable: func() bool { return true },
								Style: core.Style{
									Width:          core.Pt(20),
									Height:         core.Pt(20),
									CornerRadius:   6,
									Background:     boxBg,
									BorderWidth:    boxBorderW,
									BorderColor:    boxBorder,
									AlignItems:     core.AlignCenter,
									JustifyContent: core.JustifyCenter,
									FocusVisible:   focusRing,
								},
								OnPress: func(core.PressEvent) {
									ed.TogglePermissionsDraftRule(idx)
								},
								Children: checkKids,
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: ed.localizedActionName(rule.Action),
								Style:   core.Style{FontSize: 12.5, FontWeight: 500, Color: p.TextPrimary},
							},
							core.Text{
								Content: FormatPermissionPatterns(rule.Action, rule.Patterns),
								Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.TextMuted},
							},
						},
					},
					specDecisionBadgeLocalized(p, ed, rule.CurrentDecision, false),
					core.Text{
						Content: "→",
						Style:   core.Style{Width: core.Pt(18), TextAlign: core.TextAlignCenter, FontSize: 12, Color: p.TextFaint, FlexShrink: 0},
					},
					specDecisionBadgeLocalized(p, ed, rule.DraftDecision, !rule.Ticked),
				},
			})
		}

		// YAML diff preview box.
		yamlNodes := []core.Node{
			core.Text{
				Content: ed.T("permissions.draft.yamlHeader", l10n.Param("file", PermissionsFileName)),
				Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.TextFaint},
			},
		}
		if len(yamlLines) == 0 {
			yamlNodes = append(yamlNodes, core.Text{
				TestID:  "perm-draft-yaml-none",
				Content: ed.T("permissions.draft.noChangesTicked"),
				Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: p.TextFaint},
			})
		} else {
			for _, ln := range yamlLines {
				bg := core.Transparent
				fg := p.TextMuted
				switch ln.Kind {
				case "+":
					bg = p.Added.Fill
					fg = p.Added.Fg
				case "-":
					bg = p.Removed.Fill
					fg = p.Removed.Fg
				}
				yamlNodes = append(yamlNodes, core.View{
					Style: core.Style{
						Padding:    core.Symmetric(1, 8),
						Background: bg,
					},
					Children: []core.Node{
						core.Text{
							Content: ln.Text,
							Style:   core.Style{FontFamily: core.FontMonospace, FontSize: 11.5, Color: fg},
						},
					},
				})
			}
		}

		applyLabel := ed.T("permissions.draft.apply")
		if ticked > 0 {
			applyLabel = ed.Plural("permissions.draft.applyCount", ticked)
		}
		applyBg := p.LineRaised
		applyFg := p.TextFaint
		if ticked > 0 {
			applyBg = p.Accent.Fg
			applyFg = p.Accent.OnFill
		}

		shadowAlpha := float32(0.55)
		if !p.Dark {
			shadowAlpha = 0.20
		}

		return core.View{
			TestID: "perm-draft-card",
			Role:   core.RoleDialog,
			Label:  ed.T("permissions.draft.title"),
			Style: core.Style{
				Width:        core.Pt(620),
				MaxWidth:     core.Pct(92),
				CornerRadius: 18,
				Background:   p.Raised,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Overflow:     core.Hidden,
				Shadows: []core.BoxShadow{
					ui.Shadow(0, 24, 64, 0, ui.Black(shadowAlpha)),
				},
			},
			Children: []core.Node{
				// Header + description.
				core.View{
					Style: core.Style{Padding: core.Edges{Top: 24, Right: 24, Bottom: 16, Left: 24}, Gap: 8},
					Children: []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
							Children: []core.Node{
								core.Text{
									TestID:  "perm-draft-title",
									Content: ed.T("permissions.draft.title"),
									Style:   core.Style{FlexGrow: 1, FontSize: 14, FontWeight: 600, Color: p.TextPrimary},
								},
								specYouOnlyPill(p, "perm-draft-you-only", ed.T("settings.policyYouOnly")),
							},
						},
						core.Text{
							TestID:  "perm-draft-desc",
							Content: desc,
							Style:   core.Style{FontSize: 12.5, Color: p.TextMuted},
						},
					},
				},
				// Rule table.
				core.View{
					TestID: "perm-draft-table",
					Style: core.Style{
						Margin:       core.Symmetric(0, 24),
						CornerRadius: 12,
						Background:   p.Inset,
						BorderWidth:  1,
						BorderColor:  p.Line,
						Overflow:     core.Hidden,
					},
					Children: tableRows,
				},
				// Resulting YAML preview.
				core.View{
					TestID: "perm-draft-yaml",
					Style: core.Style{
						Margin:       core.Edges{Top: 12, Right: 24, Bottom: 0, Left: 24},
						Padding:      core.Symmetric(8, 12),
						Gap:          2,
						CornerRadius: 8,
						Background:   p.Inset,
						BorderWidth:  1,
						BorderColor:  p.Line,
					},
					Children: yamlNodes,
				},
				// Footer actions.
				core.View{
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        8,
						Padding:    core.Edges{Top: 16, Right: 24, Bottom: 24, Left: 24},
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("permissions.draft.historyHint"),
							Style:   core.Style{FlexGrow: 1, FontSize: 12, Color: p.TextFaint},
						},
						ui.Button{
							TestID:  "perm-draft-discard",
							Variant: ui.ButtonGhost,
							Size:    ui.SizeSmall,
							Text:    ed.T("permissions.draft.discard"),
							Label:   ed.T("permissions.draft.discard"),
							Color:   func() core.Color { return p.LineRaised },
							TextStyle: core.Style{
								FontSize:   12.5,
								FontWeight: 500,
								Color:      p.TextPrimary,
							},
							Style: core.Style{
								Height:       core.Pt(32),
								Padding:      core.Symmetric(0, 14),
								CornerRadius: 8,
								FocusVisible: focusRing,
							},
							OnPress: func() {
								ed.DiscardPermissionsDraft()
							},
						},
						ui.Button{
							TestID:   "perm-draft-apply",
							Variant:  ui.ButtonGhost,
							Size:     ui.SizeSmall,
							Text:     applyLabel,
							Label:    applyLabel,
							Disabled: func() bool { return ticked == 0 },
							Color:    func() core.Color { return applyBg },
							TextStyle: core.Style{
								FontSize:   12.5,
								FontWeight: 600,
								Color:      applyFg,
							},
							Style: core.Style{
								Height:       core.Pt(32),
								Padding:      core.Symmetric(0, 16),
								CornerRadius: 8,
								FocusVisible: focusRing,
							},
							OnPress: func() {
								if ticked == 0 {
									return
								}
								_, err := ed.ApplyPermissionsDraft()
								ed.report(err)
							},
						},
					},
				},
			},
		}
	})

	return ui.Dialog{
		TestID: "perm-draft-backdrop",
		Bare:   true,
		Open: func() bool {
			return key.Get().open
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				ed.DiscardPermissionsDraft()
			}
		},
		OnClose: onDialogClose,
		LabelFunc: func() string {
			_ = key.Get()
			return ed.T("permissions.draft.title")
		},
		Style: core.Style{
			CornerRadius: 18,
			Background:   core.Transparent,
		},
		Children: []core.Node{content},
	}
}

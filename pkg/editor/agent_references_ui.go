package editor

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

type agentComposerRefKey struct {
	sel          string
	scene        string
	attachedSig  string
	briefExists  bool
	briefSig     string
	dragOver     bool
	previewID    string
	attachErr    string
	pinNoticeSig string
	canUndoPin   bool
	working      bool
	lang         Language
}

func buildAgentComposerRefKey(ed *Editor) agentComposerRefKey {
	var sceneName string
	if ed.Doc != nil {
		sceneName = ed.Doc.Name
	}
	var ab strings.Builder
	for _, img := range ed.agent.attachedImages {
		fmt.Fprintf(&ab, "%s:%s:%v:%d|", img.ID, img.Path, img.Pinned, img.ImageVersion)
	}
	briefExists, briefRefs := ed.readBriefMeta()
	var bb strings.Builder
	for _, r := range briefRefs {
		fmt.Fprintf(&bb, "%s:%s|", r.Path, r.Note)
	}
	var pinSig string
	if n := ed.agent.lastPinNotice; n != nil {
		pinSig = fmt.Sprintf("%s|%s|%d", n.ImageID, n.RelPath, len(n.Diff))
	}
	return agentComposerRefKey{
		sel:          ed.SelectedID,
		scene:        sceneName,
		attachedSig:  ab.String(),
		briefExists:  briefExists,
		briefSig:     bb.String(),
		dragOver:     ed.agent.composerDragOver,
		previewID:    ed.agent.previewImageID,
		attachErr:    ed.agent.attachErr,
		pinNoticeSig: pinSig,
		canUndoPin:   ed.CanUndoLastBriefPin(),
		working:      ed.AgentWorking(),
		lang:         ed.Language(),
	}
}

// agentImagePreviewPopover renders the floating preview card above the composer when the user
// hovers or focuses an attached image chip or the brief.yaml Always chip.
func agentImagePreviewPopover(ed *Editor, p SpecPalette) core.Node {
	previewID := ed.AgentImagePreviewID()
	if previewID == "" {
		return nil
	}
	if previewID == AgentBriefPreviewID {
		return agentBriefPreviewPopover(ed, p)
	}
	var target *AgentReferenceImage
	for i := range ed.agent.attachedImages {
		if ed.agent.attachedImages[i].ID == previewID {
			cp := ed.agent.attachedImages[i]
			target = &cp
			break
		}
	}
	if target == nil {
		return nil
	}

	scopeText := ed.T("agent.ref.sentOnce")
	scopeCol := p.TextFaint
	if target.Pinned && target.Path != "" {
		scopeText = fmt.Sprintf("%s · %s", ed.T("agent.ref.pinned"), target.Path)
		scopeCol = p.Added.Fg
	}
	dimsText := fmt.Sprintf("%s · %d × %d", target.Format.Label(), target.Width, target.Height)

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-image-preview-title",
					Content: target.Name,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontSize:   13,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					TestID:  "agent-image-preview-dims",
					Content: dimsText,
					Style: core.Style{
						FontSize:   11.5,
						FontFamily: core.FontMonospace,
						Color:      p.TextFaint,
						FlexShrink: 0,
					},
				},
			},
		},
		core.View{
			Style: core.Style{
				Height:         core.Pt(164),
				CornerRadius:   8,
				Background:     p.Inset,
				BorderWidth:    1,
				BorderColor:    p.LineRaised,
				Overflow:       core.Hidden,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Padding:        core.All(4),
			},
			Children: []core.Node{
				core.Image{
					TestID:     "agent-image-preview-img",
					Source:     core.ImageSource{Pixels: target.Pixels, Version: target.ImageVersion},
					ResizeMode: core.Contain,
					Label:      target.Name,
					Style: core.Style{
						Width:  core.Pct(100),
						Height: core.Pct(100),
					},
				},
			},
		},
		core.Text{
			TestID:  "agent-image-preview-scope",
			Content: scopeText,
			Style: core.Style{
				FontSize: 11.5,
				Color:    scopeCol,
			},
		},
	}

	return core.View{
		TestID: "agent-image-preview-popover",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: children,
	}
}

func agentBriefPreviewPopover(ed *Editor, p SpecPalette) core.Node {
	imgs := ed.BriefReferenceImages()
	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            8,
			},
			Children: []core.Node{
				core.Text{
					TestID:  "agent-image-preview-title",
					Content: ed.T("agent.ref.briefPreviewTitle"),
					Style: core.Style{
						FontSize:   13,
						FontWeight: 600,
						Color:      p.TextPrimary,
					},
				},
				core.Text{
					Content: "brief.yaml",
					Style: core.Style{
						FontSize:   11.5,
						FontFamily: core.FontMonospace,
						Color:      p.TextFaint,
					},
				},
			},
		},
	}
	if len(imgs) == 0 {
		children = append(children, core.Text{
			Content: ed.T("agent.ref.briefEmpty"),
			Style: core.Style{
				FontSize: 11.5,
				Color:    p.TextMuted,
			},
		})
	} else {
		for idx, img := range imgs {
			meta := fmt.Sprintf("%s · %d × %d", img.Format.Label(), img.Width, img.Height)
			if img.Note != "" {
				meta = img.Note + " · " + meta
			}
			children = append(children, core.View{
				TestID: fmt.Sprintf("agent-brief-ref-row-%d", idx),
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        8,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(44),
							Height:       core.Pt(32),
							CornerRadius: 6,
							Background:   p.Inset,
							BorderWidth:  1,
							BorderColor:  p.LineRaised,
							Overflow:     core.Hidden,
							FlexShrink:   0,
						},
						Children: []core.Node{
							core.Image{
								Source:     core.ImageSource{Pixels: img.Pixels, Version: img.ImageVersion},
								ResizeMode: core.Contain,
								Label:      img.Name,
								Style: core.Style{
									Width:  core.Pct(100),
									Height: core.Pct(100),
								},
							},
						},
					},
					core.View{
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							Gap:        2,
						},
						Children: []core.Node{
							core.Text{
								Content: img.Path,
								Style: core.Style{
									FontSize:   11.5,
									FontFamily: core.FontMonospace,
									Color:      p.TextSecondary,
								},
							},
							core.Text{
								Content: meta,
								Style: core.Style{
									FontSize: 11,
									Color:    p.TextFaint,
								},
							},
						},
					},
				},
			})
		}
	}
	return core.View{
		TestID: "agent-image-preview-popover",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          8,
		},
		Children: children,
	}
}

// agentPinNoticeCardView renders the "Pin to brief" confirmation card matching design/editor/Sketch.dc.html:
// green check, "Pinned <name>", Undo button, monospace YAML diff with added lines highlighted, and caption.
func agentPinNoticeCardView(ed *Editor, p SpecPalette) core.Node {
	notice := ed.AgentLastPinNotice()
	if notice == nil {
		return nil
	}

	diffRows := make([]core.Node, 0, len(notice.Diff))
	for i, line := range notice.Diff {
		var rowBg core.Color
		textCol := p.TextFaint
		signCol := p.TextFaint
		if line.Added {
			rowBg = p.Added.Fill
			textCol = p.Added.Fg
			signCol = p.Added.Fg
		}
		sign := line.Sign
		if sign == "" {
			sign = " "
		}
		diffRows = append(diffRows, core.View{
			TestID: fmt.Sprintf("agent-pin-diff-line-%d", i),
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignStart,
				Padding:    core.Symmetric(2, 8),
				Gap:        4,
				Background: rowBg,
			},
			Children: []core.Node{
				core.Text{
					Content: sign,
					Style: core.Style{
						Width:      core.Pt(10),
						FlexShrink: 0,
						FontFamily: core.FontMonospace,
						FontSize:   11,
						Color:      signCol,
					},
				},
				core.Text{
					Content: line.Text,
					Style: core.Style{
						FlexGrow:   1,
						FlexShrink: 1,
						MinWidth:   core.Pt(0),
						FontFamily: core.FontMonospace,
						FontSize:   11,
						Color:      textCol,
					},
				},
			},
		})
	}

	headerChildren := []core.Node{
		core.SVGView{
			SVG: lucide.Check(),
			Style: core.Style{
				Width:      core.Pt(14),
				Height:     core.Pt(14),
				Color:      p.Added.Fg,
				FlexShrink: 0,
			},
		},
		core.Text{
			TestID:  "agent-pin-notice-title",
			Content: fmt.Sprintf("%s %s", ed.T("agent.ref.pinned"), notice.Name),
			Style: core.Style{
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
				FontSize:   12.5,
				FontWeight: 500,
				Color:      p.TextPrimary,
			},
		},
	}
	if ed.CanUndoLastBriefPin() {
		headerChildren = append(headerChildren, core.View{
			TestID: "agent-pin-notice-undo",
			Role:   core.RoleButton,
			Label:  ed.T("agent.ref.undo"),
			Style: core.Style{
				Height:         core.Pt(28),
				Padding:        core.Symmetric(0, 8),
				CornerRadius:   6,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				FocusVisible:   p.FocusRing(2),
			},
			OnPress: func(core.PressEvent) {
				ed.UndoLastBriefPin()
			},
			Children: []core.Node{
				core.Text{
					Content: ed.T("agent.ref.undo"),
					Style: core.Style{
						FontSize: 11.5,
						Color:    p.TextMuted,
					},
				},
			},
		})
	}
	headerChildren = append(headerChildren, core.View{
		TestID: "agent-pin-notice-dismiss",
		Role:   core.RoleButton,
		Label:  ed.T("agent.ref.dismissNotice"),
		Style: core.Style{
			Width:          core.Pt(24),
			Height:         core.Pt(24),
			CornerRadius:   6,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FocusVisible:   p.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			ed.DismissAgentPinNotice()
		},
		Children: []core.Node{
			core.SVGView{
				SVG: lucide.X(),
				Style: core.Style{
					Width:  core.Pt(11),
					Height: core.Pt(11),
					Color:  p.TextFaint,
				},
			},
		},
	})

	children := []core.Node{
		core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: headerChildren,
		},
	}

	if len(diffRows) > 0 {
		children = append(children, core.View{
			TestID: "agent-pin-notice-diff",
			Style: core.Style{
				CornerRadius: 8,
				Background:   p.Bg,
				BorderWidth:  1,
				BorderColor:  p.Line,
				Padding:      core.Symmetric(6, 0),
				Overflow:     core.Hidden,
			},
			Children: diffRows,
		})
	}

	children = append(children, core.Text{
		TestID:  "agent-pin-notice-caption",
		Content: strings.ReplaceAll(ed.T("agent.ref.pinnedCaption"), "{path}", notice.RelPath),
		Style: core.Style{
			FontSize: 11.5,
			Color:    p.TextFaint,
		},
	})

	return core.View{
		TestID: "agent-pin-notice-card",
		Style: core.Style{
			CornerRadius: 12,
			Background:   p.Panel,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
			Padding:      core.All(12),
			Gap:          10,
		},
		Children: children,
	}
}

// agentComposerDropZoneView renders the "Drop to attach" target shown while dragging images over the composer.
func agentComposerDropZoneView(ed *Editor, p SpecPalette) core.Node {
	if !ed.AgentComposerDragOver() {
		return nil
	}
	return core.View{
		TestID: "agent-composer-drop-zone",
		Style: core.Style{
			MinHeight:      core.Pt(96),
			CornerRadius:   12,
			Background:     p.Accent.Fill,
			BorderWidth:    1.5,
			BorderColor:    p.Accent.Edge,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.All(12),
			Gap:            4,
		},
		Children: []core.Node{
			core.SVGView{
				SVG: lucide.Upload(),
				Style: core.Style{
					Width:  core.Pt(20),
					Height: core.Pt(20),
					Color:  p.Accent.Fg,
				},
			},
			core.Text{
				TestID:  "agent-composer-drop-title",
				Content: ed.T("agent.ref.dropTitle"),
				Style: core.Style{
					FontSize:   12.5,
					FontWeight: 500,
					Color:      p.TextPrimary,
				},
			},
			core.Text{
				TestID:  "agent-composer-drop-subtitle",
				Content: ed.T("agent.ref.dropSubtitle"),
				Style: core.Style{
					FontSize: 11.5,
					Color:    p.TextMuted,
				},
			},
		},
	}
}

// agentAttachErrorBannerView renders the refusal reason when an unsupported file type is dropped or pasted.
func agentAttachErrorBannerView(ed *Editor, p SpecPalette) core.Node {
	msg := ed.AgentAttachError()
	if msg == "" {
		return nil
	}
	return core.View{
		TestID:     "agent-attach-error",
		Role:       core.RoleAlert,
		LiveRegion: core.LiveRegionPolite,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Gap:          8,
			Padding:      core.Symmetric(8, 10),
			CornerRadius: 8,
			Background:   p.Removed.Fill,
			BorderWidth:  1,
			BorderColor:  p.Removed.Edge,
		},
		Children: []core.Node{
			core.SVGView{
				SVG: lucide.TriangleAlert(),
				Style: core.Style{
					Width:      core.Pt(14),
					Height:     core.Pt(14),
					Color:      p.Removed.Fg,
					FlexShrink: 0,
				},
			},
			core.Text{
				TestID:  "agent-attach-error-text",
				Content: msg,
				Style: core.Style{
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					FontSize:   11.5,
					Color:      p.Removed.Fg,
				},
			},
			core.View{
				TestID: "agent-attach-error-dismiss",
				Role:   core.RoleButton,
				Label:  ed.T("agent.ref.dismissError"),
				Style: core.Style{
					Width:          core.Pt(24),
					Height:         core.Pt(24),
					CornerRadius:   4,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					FocusVisible:   p.FocusRing(2),
					FlexShrink:     0,
				},
				OnPress: func(core.PressEvent) {
					ed.DismissAgentAttachError()
				},
				Children: []core.Node{
					core.SVGView{
						SVG: lucide.X(),
						Style: core.Style{
							Width:  core.Pt(11),
							Height: core.Pt(11),
							Color:  p.Removed.Fg,
						},
					},
				},
			},
		},
	}
}

// agentAttachedImageChipsView renders the attached PNG/JPEG/WebP image chips inside the composer
// matching Sketch.dc.html: 32×24 thumbnail, filename, "Pin to brief" / "Pinned" toggle, and × remove button.
func agentAttachedImageChipsView(ed *Editor, p SpecPalette) []core.Node {
	imgs := ed.AgentAttachedImages()
	if len(imgs) == 0 {
		return nil
	}
	previewID := ed.AgentImagePreviewID()
	nodes := make([]core.Node, 0, len(imgs))
	for i, img := range imgs {
		idx := i
		item := img
		isPreview := previewID == item.ID
		borderCol := p.LineRaised
		if isPreview {
			borderCol = p.Accent.Edge
		}

		pinBg := p.Raised
		pinFg := p.TextSecondary
		pinText := ed.T("agent.ref.pinToBrief")
		pinTip := ed.T("agent.ref.pinTooltip")
		if item.Pinned {
			pinBg = p.Added.Fill
			pinFg = p.Added.Fg
			pinText = ed.T("agent.ref.pinned")
			pinTip = ed.T("agent.ref.pinnedTooltip")
		}

		pinBtn := core.View{
			TestID: fmt.Sprintf("agent-image-pin-%d", idx),
			Role:   core.RoleButton,
			Label:  fmt.Sprintf("%s %s", pinText, item.Name),
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Height:         core.Pt(24),
				Padding:        core.Symmetric(0, 8),
				Gap:            4,
				CornerRadius:   6,
				Background:     pinBg,
				FocusVisible:   p.FocusRing(2),
				FlexShrink:     0,
			},
			OnPress: func(core.PressEvent) {
				if item.Pinned {
					return
				}
				_, err := ed.PinAgentReferenceImage(item.ID, "")
				ed.report(err)
			},
			Children: []core.Node{
				core.SVGView{
					SVG: lucide.Pin(),
					Style: core.Style{
						Width:      core.Pt(11),
						Height:     core.Pt(11),
						Color:      pinFg,
						FlexShrink: 0,
					},
				},
				core.Text{
					TestID:  fmt.Sprintf("agent-image-pin-text-%d", idx),
					Content: pinText,
					Style: core.Style{
						FontSize: 11,
						Color:    pinFg,
					},
				},
			},
		}

		nodes = append(nodes, core.View{
			TestID: fmt.Sprintf("agent-image-chip-%d", idx),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Height:       core.Pt(32),
				Padding:      core.Edges{Left: 4, Right: 2},
				Gap:          6,
				CornerRadius: 6,
				Background:   p.LineRaised,
				BorderWidth:  1,
				BorderColor:  borderCol,
			},
			OnMouseEnter: func(core.MouseEvent) {
				ed.SetAgentImagePreviewID(item.ID)
			},
			OnMouseLeave: func(core.MouseEvent) {
				if ed.AgentImagePreviewID() == item.ID {
					ed.SetAgentImagePreviewID("")
				}
			},
			Children: []core.Node{
				core.View{
					TestID: fmt.Sprintf("agent-image-preview-btn-%d", idx),
					Role:   core.RoleButton,
					Label:  strings.ReplaceAll(ed.T("agent.ref.previewImage"), "{name}", item.Name),
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Height:       core.Pt(28),
						Gap:          6,
						Padding:      core.Edges{Right: 2},
						CornerRadius: 4,
						FocusVisible: p.FocusRing(2),
					},
					OnFocus: func() {
						ed.SetAgentImagePreviewID(item.ID)
					},
					OnBlur: func() {
						if ed.AgentImagePreviewID() == item.ID {
							ed.SetAgentImagePreviewID("")
						}
					},
					OnPress: func(core.PressEvent) {
						if ed.AgentImagePreviewID() == item.ID {
							ed.SetAgentImagePreviewID("")
						} else {
							ed.SetAgentImagePreviewID(item.ID)
						}
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{
								Width:        core.Pt(32),
								Height:       core.Pt(24),
								CornerRadius: 4,
								Background:   p.Inset,
								Overflow:     core.Hidden,
								FlexShrink:   0,
							},
							Children: []core.Node{
								core.Image{
									Source:     core.ImageSource{Pixels: item.Pixels, Version: item.ImageVersion},
									ResizeMode: core.Contain,
									Label:      item.Name,
									Style: core.Style{
										Width:  core.Pct(100),
										Height: core.Pct(100),
									},
								},
							},
						},
						core.Text{
							TestID:  fmt.Sprintf("agent-image-name-%d", idx),
							Content: item.Name,
							Style: core.Style{
								FontSize: 11.5,
								Color:    p.TextPrimary,
							},
						},
					},
				},
				ui.Tooltip{
					Text:    pinTip,
					Trigger: pinBtn,
				},
				core.View{
					TestID: fmt.Sprintf("agent-image-remove-%d", idx),
					Role:   core.RoleButton,
					Label:  strings.ReplaceAll(ed.T("agent.ref.removeImage"), "{name}", item.Name),
					Style: core.Style{
						Width:          core.Pt(24),
						Height:         core.Pt(24),
						CornerRadius:   4,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FocusVisible:   p.FocusRing(2),
						FlexShrink:     0,
					},
					OnPress: func(core.PressEvent) {
						ed.RemoveAgentReferenceImage(item.ID)
					},
					Children: []core.Node{
						core.SVGView{
							SVG: lucide.X(),
							Style: core.Style{
								Width:  core.Pt(10),
								Height: core.Pt(10),
								Color:  p.TextMuted,
							},
						},
					},
				},
			},
		})
	}
	return nodes
}

// agentBriefAlwaysChipView renders the locked brief.yaml Always chip (including pinned reference
// count when non-zero) for the unified AgentComposerView Always row. When pinned references exist,
// hovering or pressing previews the pinned reference images; when none exist and chipID is provided,
// hovering shows the exact brief.yaml YAML payload preview while pressing opens the brief reference popover.
func agentBriefAlwaysChipView(ed *Editor, p SpecPalette, chipID string) core.Node {
	exists, refs := ed.readBriefMeta()
	if !exists && len(refs) == 0 {
		return nil
	}
	label := "brief.yaml"
	if len(refs) == 1 {
		label = ed.T("agent.ref.briefCountOne")
	} else if len(refs) > 1 {
		label = strings.ReplaceAll(ed.T("agent.ref.briefCountMany"), "{count}", fmt.Sprintf("%d", len(refs)))
	}

	previewing := ed.AgentImagePreviewID() == AgentBriefPreviewID || (chipID != "" && ed.HoveredChipID() == chipID)
	borderCol := p.LineRaised
	textCol := p.TextFaint
	if len(refs) > 0 {
		textCol = p.TextMuted
	}
	if previewing {
		borderCol = p.Accent.Edge
		textCol = p.TextPrimary
	}

	chipChildren := []core.Node{
		core.SVGView{
			SVG: lucide.Lock(),
			Style: core.Style{
				Width:      core.Pt(10),
				Height:     core.Pt(10),
				Color:      textCol,
				FlexShrink: 0,
			},
		},
		core.Text{
			TestID:  "agent-always-brief-text",
			Content: label,
			Style: core.Style{
				FontSize: 11.5,
				Color:    textCol,
			},
		},
	}

	return core.View{
		TestID: "agent-always-brief",
		Role:   core.RoleButton,
		Label:  strings.ReplaceAll(ed.T("agent.ref.alwaysIncluded"), "{label}", label),
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(22),
			Padding:      core.Symmetric(0, 8),
			Gap:          4,
			CornerRadius: 6,
			BorderWidth:  1,
			BorderColor:  borderCol,
			FocusVisible: p.FocusRing(2),
		},
		OnMouseEnter: func(core.MouseEvent) {
			if len(refs) > 0 || chipID == "" {
				ed.SetAgentImagePreviewID(AgentBriefPreviewID)
			} else {
				ed.SetHoveredChip(chipID)
			}
		},
		OnMouseLeave: func(core.MouseEvent) {
			if ed.AgentImagePreviewID() == AgentBriefPreviewID {
				ed.SetAgentImagePreviewID("")
			}
			if chipID != "" && ed.HoveredChipID() == chipID {
				ed.SetHoveredChip("")
			}
		},
		OnFocus: func() {
			if len(refs) > 0 || chipID == "" {
				ed.SetAgentImagePreviewID(AgentBriefPreviewID)
			} else {
				ed.SetHoveredChip(chipID)
			}
		},
		OnBlur: func() {
			if ed.AgentImagePreviewID() == AgentBriefPreviewID {
				ed.SetAgentImagePreviewID("")
			}
			if chipID != "" && ed.HoveredChipID() == chipID {
				ed.SetHoveredChip("")
			}
		},
		OnPress: func(core.PressEvent) {
			if ed.AgentImagePreviewID() == AgentBriefPreviewID {
				ed.SetAgentImagePreviewID("")
			} else {
				ed.SetAgentImagePreviewID(AgentBriefPreviewID)
			}
		},
		Children: chipChildren,
	}
}

// agentAttachImageButtonView renders the composer toolbar button matching Sketch.dc.html's image icon button.
func agentAttachImageButtonView(ed *Editor, p SpecPalette) core.Node {
	btn := core.View{
		TestID: "agent-composer-attach-image",
		Role:   core.RoleButton,
		Label:  ed.T("agent.ref.attachLabel"),
		Style: core.Style{
			Width:          core.Pt(28),
			Height:         core.Pt(28),
			CornerRadius:   6,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			FocusVisible:   p.FocusRing(2),
			FlexShrink:     0,
		},
		OnPress: func(core.PressEvent) {
			ed.PickAndAttachAgentReferenceImages()
		},
		Children: []core.Node{
			core.SVGView{
				SVG: lucide.Image(),
				Style: core.Style{
					Width:  core.Pt(15),
					Height: core.Pt(15),
					Color:  p.TextMuted,
				},
			},
		},
	}
	return ui.Tooltip{
		Text:    ed.T("agent.ref.attachTooltip"),
		Trigger: btn,
	}
}

// agentTurnImagesRowView renders compact thumbnails of reference images sent with a user turn bubble.
func agentTurnImagesRowView(turnIdx int, imgs []AgentReferenceImage, p SpecPalette) core.Node {
	if len(imgs) == 0 {
		return nil
	}
	thumbs := make([]core.Node, 0, len(imgs))
	for i, img := range imgs {
		thumbs = append(thumbs, core.View{
			TestID: fmt.Sprintf("agent-turn-image-%d-%d", turnIdx, i),
			Style: core.Style{
				Width:        core.Pt(56),
				Height:       core.Pt(40),
				CornerRadius: 6,
				Background:   p.Inset,
				BorderWidth:  1,
				BorderColor:  p.LineRaised,
				Overflow:     core.Hidden,
				FlexShrink:   0,
			},
			Children: []core.Node{
				core.Image{
					Source:     core.ImageSource{Pixels: img.Pixels, Version: img.ImageVersion},
					ResizeMode: core.Contain,
					Label:      img.Name,
					Style: core.Style{
						Width:  core.Pct(100),
						Height: core.Pct(100),
					},
				},
			},
		})
	}
	return core.View{
		TestID: fmt.Sprintf("agent-turn-images-%d", turnIdx),
		Style: core.Style{
			Direction: core.Row,
			Wrap:      core.Wrap,
			Gap:       6,
		},
		Children: thumbs,
	}
}

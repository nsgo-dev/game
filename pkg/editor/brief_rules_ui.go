package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// briefSettingsPageView renders Settings › Game brief (brief.yaml) styled exclusively with Spec tokens.
// All text inputs remain mounted outside reactive subtrees so incoming reviews, form errors, and
// unrelated editor revisions never remount inputs, steal focus, or discard unsaved drafts.
func briefSettingsPageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	docRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.agentDocsRev()
	})
	contentRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.briefContentRev()
	})
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	curPalette := func() SpecPalette {
		return SpecPaletteFor(darkMemo.Get())
	}
	actionsBlocked := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.MergeConflictOpen()
	}

	pitchDraft := core.NewSignal(cx, "")
	artDraft := core.NewSignal(cx, "")
	feelDraft := core.NewSignal(cx, "")
	toneDraft := core.NewSignal(cx, "")
	tagsDraft := core.NewSignal(cx, "")
	genreInput := core.NewSignal(cx, "")
	platformInput := core.NewSignal(cx, "")
	targetMetric := core.NewSignal(cx, "")
	targetScene := core.NewSignal(cx, "")
	targetMin := core.NewSignal(cx, "")
	targetMax := core.NewSignal(cx, "")
	targetUnit := core.NewSignal(cx, "")
	targetSummary := core.NewSignal(cx, "")
	formErr := core.NewSignal(cx, "")

	// Synchronize local field drafts ONLY when brief.yaml content itself changes
	// (open project, demo reset, save, undo, redo, reload, or kept review).
	syncDrafts := func() {
		b := ed.Brief()
		if b == nil {
			pitchDraft.Set("")
			artDraft.Set("")
			feelDraft.Set("")
			toneDraft.Set("")
			tagsDraft.Set("")
			return
		}
		pitchDraft.Set(b.Pitch)
		artDraft.Set(b.ArtDirection)
		if b.Feel != nil {
			feelDraft.Set(b.Feel.Summary)
		} else {
			feelDraft.Set("")
		}
		toneDraft.Set(b.ToneAndAudio)
		tagsDraft.Set(strings.Join(b.Tags, ", "))
	}
	syncDrafts()
	lastSyncedBriefRev := contentRev.Get()
	cx.Effect(func() {
		cur := contentRev.Get()
		if cur == lastSyncedBriefRev {
			return
		}
		lastSyncedBriefRev = cur
		syncDrafts()
		formErr.Set("")
	})

	saveBriefFields := func() {
		formErr.Set("")
		var tags []string
		for _, part := range strings.Split(tagsDraft.Get(), ",") {
			if s := strings.TrimSpace(part); s != "" {
				tags = append(tags, s)
			}
		}
		label := "Edit Game Brief"
		if !ed.BriefExists() {
			label = "Create Game Brief"
		}
		err := ed.EditBrief(label, func(b *project.Brief) {
			b.Pitch = strings.TrimSpace(pitchDraft.Get())
			b.ArtDirection = strings.TrimSpace(artDraft.Get())
			b.ToneAndAudio = strings.TrimSpace(toneDraft.Get())
			b.Tags = cleanBriefLabelList(tags)
			feelSum := strings.TrimSpace(feelDraft.Get())
			if feelSum != "" || (b.Feel != nil && len(b.Feel.Targets) > 0) {
				if b.Feel == nil {
					b.Feel = &project.BriefFeel{}
				}
				b.Feel.Summary = feelSum
			} else {
				b.Feel = nil
			}
		})
		if err != nil {
			formErr.Set(err.Error())
		}
	}

	addTarget := func() {
		formErr.Set("")
		metric := strings.TrimSpace(targetMetric.Get())
		if metric == "" {
			formErr.Set(ed.T("settings.briefTargetMetricReq"))
			return
		}
		t := project.BriefPlaytestTarget{
			Metric:  metric,
			Scene:   strings.TrimSpace(targetScene.Get()),
			Unit:    strings.TrimSpace(targetUnit.Get()),
			Summary: strings.TrimSpace(targetSummary.Get()),
		}
		if s := strings.TrimSpace(targetMin.Get()); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				formErr.Set(ed.T("settings.briefTargetMinNumErr"))
				return
			}
			t.Min = project.Float64(v)
		}
		if s := strings.TrimSpace(targetMax.Get()); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				formErr.Set(ed.T("settings.briefTargetMaxNumErr"))
				return
			}
			t.Max = project.Float64(v)
		}
		if err := ed.AddBriefPlaytestTarget(t); err != nil {
			formErr.Set(err.Error())
			return
		}
		targetMetric.Set("")
		targetScene.Set("")
		targetMin.Set("")
		targetMax.Set("")
		targetUnit.Set("")
		targetSummary.Set("")
	}

	isSectionMissing := func(sec project.BriefSection) bool {
		_ = docRev.Get()
		for _, g := range ed.BriefGaps() {
			if g == sec {
				return true
			}
		}
		return false
	}

	sectionLabel := func(labelKey string, sec project.BriefSection) core.Node {
		return core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: []core.Node{
				core.Text{
					ContentFunc: func() string {
						_ = langMemo.Get()
						return ed.T(labelKey)
					},
					StyleFunc: func() core.Style {
						p := curPalette()
						return core.Style{FontSize: 12, FontWeight: 500, Color: p.TextMuted}
					},
				},
				core.Show{
					When: func() bool { return isSectionMissing(sec) },
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							StyleFunc: func() core.Style {
								p := curPalette()
								return core.Style{
									Padding:      core.Symmetric(1, 7),
									CornerRadius: 6,
									Background:   p.Changed.Fill,
									BorderWidth:  1,
									BorderColor:  p.Changed.Edge,
								}
							},
							Children: []core.Node{
								core.Text{
									ContentFunc: func() string {
										_ = langMemo.Get()
										return ed.T("settings.briefGapTag")
									},
									StyleFunc: func() core.Style {
										p := curPalette()
										return core.Style{FontSize: 10.5, FontWeight: 600, Color: p.Changed.Fg}
									},
								},
							},
						}
					},
				},
			},
		}
	}

	chipSection := func(testPrefix string, getItems func() []string, onRemove func(string), inputSig *core.Signal[string], placeholderKey string, onAdd func(string)) core.Node {
		p0 := curPalette()
		return core.View{
			Style: core.Style{Gap: 8},
			Children: []core.Node{
				core.Dynamic(func(cx *core.Scope) core.Node {
					_ = docRev.Get()
					p := curPalette()
					items := getItems()
					var chips []core.Node
					for i, item := range items {
						val := item
						chips = append(chips, core.View{
							TestID: fmt.Sprintf("%s-chip-%d", testPrefix, i),
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          6,
								Height:       core.Pt(28),
								Padding:      core.Symmetric(0, 10),
								CornerRadius: 14,
								Background:   p.Accent.Fill,
								BorderWidth:  1,
								BorderColor:  p.Accent.Edge,
							},
							Children: []core.Node{
								core.Text{
									Content: val,
									Style:   core.Style{FontSize: 12, Color: p.TextPrimary},
								},
								ui.Button{
									TestID:   fmt.Sprintf("%s-remove-%d", testPrefix, i),
									Text:     "×",
									Label:    "Remove " + val,
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonGhost,
									Disabled: actionsBlocked,
									Color:    func() core.Color { return curPalette().Accent.Fill },
									TextStyle: core.Style{
										FontSize: 12,
										Color:    p.TextMuted,
									},
									Style:   core.Style{MinWidth: core.Pt(18), Height: core.Pt(20), CornerRadius: 6, FocusVisible: p.FocusRing(1)},
									OnPress: func() { onRemove(val) },
								},
							},
						})
					}
					return core.View{
						Style:    core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6},
						Children: chips,
					}
				}),
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
					Children: []core.Node{
						ui.Input{
							TestID: testPrefix + "-add-input",
							LabelFunc: func() string {
								_ = langMemo.Get()
								return ed.T(placeholderKey)
							},
							PlaceholderFunc: func() string {
								_ = langMemo.Get()
								return ed.T(placeholderKey)
							},
							Value:    inputSig.Get,
							OnChange: func(v string) { inputSig.Set(v) },
							OnSubmit: func(v string) {
								if actionsBlocked() {
									return
								}
								onAdd(v)
								inputSig.Set("")
							},
							StyleFunc: func() core.Style {
								p := curPalette()
								return core.Style{
									FlexGrow:     1,
									Height:       core.Pt(30),
									Background:   p.Raised,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
									CornerRadius: 6,
									Color:        p.TextSecondary,
									FontSize:     12,
									FocusVisible: p.FocusRing(2),
								}
							},
						},
						ui.Button{
							TestID: testPrefix + "-add-btn",
							Text:   "+",
							LabelFunc: func() string {
								_ = langMemo.Get()
								return ed.T(placeholderKey)
							},
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonGhost,
							Disabled: actionsBlocked,
							Color:    func() core.Color { return curPalette().LineRaised },
							TextStyle: core.Style{
								FontSize:   13,
								FontWeight: 600,
								Color:      p0.TextSecondary,
							},
							Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), CornerRadius: 6, FocusVisible: p0.FocusRing(2)},
							OnPress: func() {
								onAdd(inputSig.Get())
								inputSig.Set("")
							},
						},
					},
				},
			},
		}
	}

	p0 := curPalette()
	return core.Scroll{
		TestID: "editor-settings-brief-page",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Gap: 10, Padding: core.Edges{Right: 4, Bottom: 8}},
				Children: []core.Node{
					// Load error banner
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = docRev.Get()
						_ = langMemo.Get()
						p := curPalette()
						loadErr := ed.BriefLoadErr()
						if loadErr == nil {
							return nil
						}
						return core.View{
							TestID: "brief-load-error",
							Style: core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          10,
								Padding:      core.Symmetric(10, 12),
								CornerRadius: 10,
								Background:   p.Removed.Fill,
								BorderWidth:  1,
								BorderColor:  p.Removed.Edge,
							},
							Children: []core.Node{
								core.Text{
									Content: loadErr.Error(),
									Style:   core.Style{FontSize: 12, Color: p.Removed.Fg, FlexGrow: 1, FlexShrink: 1},
								},
								ui.Button{
									TestID:   "brief-reload",
									Text:     ed.T("settings.reload"),
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonGhost,
									Disabled: actionsBlocked,
									Color:    func() core.Color { return curPalette().LineRaised },
									TextStyle: core.Style{
										FontSize: 11.5,
										Color:    p.TextSecondary,
									},
									Style:   core.Style{Height: core.Pt(28), CornerRadius: 6, FocusVisible: p.FocusRing(2)},
									OnPress: func() { _ = ed.ReloadBrief() },
								},
							},
						}
					}),

					// Pending reviewable changes to brief.yaml (from agent MCP calls)
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = docRev.Get()
						_ = langMemo.Get()
						p := curPalette()
						pending := ed.PendingDocReviews(DocReviewBrief)
						if len(pending) == 0 {
							return nil
						}
						cards := make([]core.Node, 0, len(pending))
						for _, r := range pending {
							cards = append(cards, docReviewCardView(p, ed, r, actionsBlocked))
						}
						return core.View{Style: core.Style{Gap: 10}, Children: cards}
					}),

					// Empty state banner when the project has no brief.yaml yet
					core.Show{
						When: func() bool {
							_ = docRev.Get()
							return !ed.BriefExists()
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								TestID: "brief-empty-state",
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          10,
										Padding:      core.Symmetric(10, 12),
										CornerRadius: 10,
										Background:   p.Warning.Fill,
										BorderWidth:  1,
										BorderColor:  p.Warning.Edge,
									}
								},
								Children: []core.Node{
									core.Text{
										ContentFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefEmptyState")
										},
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{FontSize: 12, Color: p.TextSecondary, FlexGrow: 1, FlexShrink: 1}
										},
									},
								},
							}
						},
					},

					// Pitch (full width)
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							sectionLabel("settings.briefPitch", project.BriefSectionPitch),
							ui.Input{
								TestID:    "brief-pitch-input",
								Multiline: true,
								LabelFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefPitch")
								},
								PlaceholderFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefPitchPlaceholder")
								},
								Value:    pitchDraft.Get,
								OnChange: func(v string) { pitchDraft.Set(v) },
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{
										Width:        core.Pct(100),
										Height:       core.Pt(60),
										Background:   p.Raised,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
										CornerRadius: 8,
										Color:        p.TextSecondary,
										FontSize:     12.5,
										FocusVisible: p.FocusRing(2),
									}
								},
							},
						},
					},

					// Genre and Platforms chips row (2 columns matching Settings.dc.html)
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 14},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 6},
								Children: []core.Node{
									sectionLabel("settings.briefGenre", project.BriefSectionGenre),
									chipSection(
										"brief-genre",
										func() []string {
											if b := ed.Brief(); b != nil {
												return b.Genre
											}
											return nil
										},
										func(v string) { _ = ed.RemoveBriefGenre(v) },
										genreInput,
										"settings.briefGenrePlaceholder",
										func(v string) {
											if err := ed.AddBriefGenre(v); err != nil {
												formErr.Set(err.Error())
											}
										},
									),
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 6},
								Children: []core.Node{
									sectionLabel("settings.briefPlatforms", project.BriefSectionPlatforms),
									chipSection(
										"brief-platform",
										func() []string {
											if b := ed.Brief(); b != nil {
												return b.Platforms
											}
											return nil
										},
										func(v string) { _ = ed.RemoveBriefPlatform(v) },
										platformInput,
										"settings.briefPlatformsPlaceholder",
										func(v string) {
											if err := ed.AddBriefPlatform(v); err != nil {
												formErr.Set(err.Error())
											}
										},
									),
								},
							},
						},
					},

					// Tags (optional comma-separated)
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefTags")
								},
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 12, FontWeight: 500, Color: p.TextMuted}
								},
							},
							ui.Input{
								TestID: "brief-tags-input",
								LabelFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefTags")
								},
								PlaceholderFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefTagsPlaceholder")
								},
								Value:    tagsDraft.Get,
								OnChange: func(v string) { tagsDraft.Set(v) },
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{
										Width:        core.Pct(100),
										Height:       core.Pt(30),
										Background:   p.Raised,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
										CornerRadius: 6,
										Color:        p.TextSecondary,
										FontSize:     12,
										FocusVisible: p.FocusRing(2),
									}
								},
							},
						},
					},

					// Art direction and Feel & difficulty side-by-side (matching Settings.dc.html)
					core.View{
						Style: core.Style{Direction: core.Row, Gap: 14},
						Children: []core.Node{
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 6},
								Children: []core.Node{
									sectionLabel("settings.briefArtDirection", project.BriefSectionArtDirection),
									ui.Input{
										TestID:    "brief-art-input",
										Multiline: true,
										LabelFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefArtDirection")
										},
										PlaceholderFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefArtPlaceholder")
										},
										Value:    artDraft.Get,
										OnChange: func(v string) { artDraft.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{
												Width:        core.Pct(100),
												Height:       core.Pt(68),
												Background:   p.Raised,
												BorderWidth:  1,
												BorderColor:  p.LineRaised,
												CornerRadius: 8,
												Color:        p.TextSecondary,
												FontSize:     12,
												FocusVisible: p.FocusRing(2),
											}
										},
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 6},
								Children: []core.Node{
									sectionLabel("settings.briefFeel", project.BriefSectionFeel),
									ui.Input{
										TestID:    "brief-feel-input",
										Multiline: true,
										LabelFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefFeel")
										},
										PlaceholderFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefFeelPlaceholder")
										},
										Value:    feelDraft.Get,
										OnChange: func(v string) { feelDraft.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{
												Width:        core.Pct(100),
												Height:       core.Pt(68),
												Background:   p.Raised,
												BorderWidth:  1,
												BorderColor:  p.LineRaised,
												CornerRadius: 8,
												Color:        p.TextSecondary,
												FontSize:     12,
												FocusVisible: p.FocusRing(2),
											}
										},
									},
								},
							},
						},
					},

					// Tone and audio section (optional, surfaced as a gap when missing)
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							sectionLabel("settings.briefToneAudio", project.BriefSectionToneAndAudio),
							ui.Input{
								TestID: "brief-tone-input",
								LabelFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefToneAudio")
								},
								PlaceholderFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefTonePlaceholder")
								},
								Value:    toneDraft.Get,
								OnChange: func(v string) { toneDraft.Set(v) },
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{
										Width:        core.Pct(100),
										Height:       core.Pt(32),
										Background:   p.Raised,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
										CornerRadius: 6,
										Color:        p.TextSecondary,
										FontSize:     12,
										FocusVisible: p.FocusRing(2),
									}
								},
							},
						},
					},

					// Structured playtest targets from Feel and difficulty
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefPlaytestTargets")
								},
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 11, FontWeight: 600, LetterSpacing: 0.6, Color: p.TextFaint}
								},
							},
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = docRev.Get()
								p := curPalette()
								brief := ed.Brief()
								var targetRows []core.Node
								if brief != nil && brief.Feel != nil {
									for i, t := range brief.Feel.Targets {
										idx := i
										desc := formatBriefTargetBadge(t)
										targetRows = append(targetRows, core.View{
											TestID: fmt.Sprintf("brief-target-row-%d", i),
											Style: core.Style{
												Direction:      core.Row,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifySpaceBetween,
												Gap:            8,
												Padding:        core.Symmetric(6, 10),
												CornerRadius:   8,
												Background:     p.Raised,
												BorderWidth:    1,
												BorderColor:    p.LineRaised,
											},
											Children: []core.Node{
												core.Text{
													Content: desc,
													Style:   core.Style{FontSize: 11.5, Color: p.TextSecondary, FlexGrow: 1, FlexShrink: 1},
												},
												ui.Button{
													TestID:   fmt.Sprintf("brief-target-remove-%d", i),
													Text:     "×",
													Label:    "Remove target " + t.Metric,
													Size:     ui.SizeSmall,
													Variant:  ui.ButtonGhost,
													Disabled: actionsBlocked,
													Color:    func() core.Color { return curPalette().Raised },
													TextStyle: core.Style{
														FontSize: 12,
														Color:    p.TextFaint,
													},
													Style:   core.Style{Width: core.Pt(24), Height: core.Pt(24), CornerRadius: 6, FocusVisible: p.FocusRing(1)},
													OnPress: func() { _ = ed.RemoveBriefPlaytestTarget(idx) },
												},
											},
										})
									}
								}
								return core.View{Style: core.Style{Gap: 6}, Children: targetRows}
							}),
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
								Children: []core.Node{
									ui.Input{
										TestID:      "brief-target-metric-input",
										Label:       "Metric",
										Placeholder: "metric (e.g. deaths)",
										Value:       targetMetric.Get,
										OnChange:    func(v string) { targetMetric.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{FlexGrow: 1, Height: core.Pt(28), Background: p.Raised, BorderWidth: 1, BorderColor: p.LineRaised, CornerRadius: 6, Color: p.TextSecondary, FontSize: 11.5, FocusVisible: p.FocusRing(2)}
										},
									},
									ui.Input{
										TestID:      "brief-target-scene-input",
										Label:       "Scene",
										Placeholder: "scene",
										Value:       targetScene.Get,
										OnChange:    func(v string) { targetScene.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{Width: core.Pt(84), Height: core.Pt(28), Background: p.Raised, BorderWidth: 1, BorderColor: p.LineRaised, CornerRadius: 6, Color: p.TextSecondary, FontSize: 11.5, FocusVisible: p.FocusRing(2)}
										},
									},
									ui.Input{
										TestID:      "brief-target-min-input",
										Label:       "Min",
										Placeholder: "min",
										Value:       targetMin.Get,
										OnChange:    func(v string) { targetMin.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{Width: core.Pt(56), Height: core.Pt(28), Background: p.Raised, BorderWidth: 1, BorderColor: p.LineRaised, CornerRadius: 6, Color: p.TextSecondary, FontSize: 11.5, FocusVisible: p.FocusRing(2)}
										},
									},
									ui.Input{
										TestID:      "brief-target-max-input",
										Label:       "Max",
										Placeholder: "max",
										Value:       targetMax.Get,
										OnChange:    func(v string) { targetMax.Set(v) },
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{Width: core.Pt(56), Height: core.Pt(28), Background: p.Raised, BorderWidth: 1, BorderColor: p.LineRaised, CornerRadius: 6, Color: p.TextSecondary, FontSize: 11.5, FocusVisible: p.FocusRing(2)}
										},
									},
									ui.Button{
										TestID: "brief-target-add-btn",
										TextFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.briefAddTarget")
										},
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonGhost,
										Disabled: actionsBlocked,
										Color:    func() core.Color { return curPalette().LineRaised },
										TextStyle: core.Style{
											FontSize: 11.5,
											Color:    p0.TextSecondary,
										},
										Style:   core.Style{Height: core.Pt(28), CornerRadius: 6, FocusVisible: p0.FocusRing(2)},
										OnPress: addTarget,
									},
								},
							},
						},
					},

					// Error banner if validation fails
					core.Show{
						When: func() bool { return formErr.Get() != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:      "brief-form-error",
								ContentFunc: formErr.Get,
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 11.5, Color: p.Removed.Fg}
								},
							}
						},
					},

					// Save + Undo action row
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							ui.Button{
								TestID: "brief-save-btn",
								TextFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefSave")
								},
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Disabled: actionsBlocked,
								Color:    func() core.Color { return curPalette().Accent.Fg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      p0.Accent.OnFill,
								},
								Style:   core.Style{Height: core.Pt(30), CornerRadius: 8, FocusVisible: p0.FocusRing(2)},
								OnPress: saveBriefFields,
							},
							ui.Button{
								TestID: "brief-undo-btn",
								TextFunc: func() string {
									_ = langMemo.Get()
									return ed.T("toolbar.undo")
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.History.CanUndo() || ed.Playing() || ed.MergeConflictOpen()
								},
								Color: func() core.Color { return curPalette().LineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    p0.TextSecondary,
								},
								Style:   core.Style{Height: core.Pt(30), CornerRadius: 8, FocusVisible: p0.FocusRing(2)},
								OnPress: func() { _ = ed.Undo() },
							},
						},
					},

					// Violet agent note banner (matching Settings.dc.html lines 122-126)
					core.View{
						TestID: "brief-agent-banner",
						StyleFunc: func() core.Style {
							p := curPalette()
							return core.Style{
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Gap:          10,
								Padding:      core.Symmetric(10, 14),
								CornerRadius: 10,
								Background:   p.Agent.Fill,
								BorderWidth:  1,
								BorderColor:  p.Agent.Edge,
							}
						},
						Children: []core.Node{
							core.View{
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{Width: core.Pt(8), Height: core.Pt(8), CornerRadius: 4, Background: p.Agent.Fg}
								},
							},
							core.Text{
								ContentFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.briefAgentFooter")
								},
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 12, Color: p.TextSecondary, FlexGrow: 1, FlexShrink: 1}
								},
							},
							core.Text{
								Content: project.BriefFileName,
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 11.5, FontFamily: core.FontMonospace, Color: p.TextFaint}
								},
							},
						},
					},
				},
			},
		},
	}
}

func formatBriefTargetBadge(t project.BriefPlaytestTarget) string {
	var parts []string
	name := t.Metric
	if t.Scene != "" {
		name = t.Scene + " · " + name
	}
	parts = append(parts, name)
	switch {
	case t.Equal != nil:
		parts = append(parts, fmt.Sprintf("= %g%s", *t.Equal, unitSuffix(t.Unit)))
	case t.Min != nil && t.Max != nil:
		parts = append(parts, fmt.Sprintf("%g–%g%s", *t.Min, *t.Max, unitSuffix(t.Unit)))
	case t.Min != nil:
		parts = append(parts, fmt.Sprintf(">= %g%s", *t.Min, unitSuffix(t.Unit)))
	case t.Max != nil:
		parts = append(parts, fmt.Sprintf("<= %g%s", *t.Max, unitSuffix(t.Unit)))
	}
	if t.Summary != "" {
		parts = append(parts, t.Summary)
	}
	return strings.Join(parts, " · ")
}

func unitSuffix(u string) string {
	if u == "" {
		return ""
	}
	if u == "%" {
		return "%"
	}
	return " " + u
}

// rulesSettingsPageView renders Settings › Project rules (AGENTS.md) styled exclusively with Spec tokens.
// The Markdown input remains mounted outside reactive subtrees so incoming reviews, dismissed
// suggestions, and form errors never remount the input or discard unsaved text.
func rulesSettingsPageView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	docRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.agentDocsRev()
	})
	contentRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.rulesContentRev()
	})
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	curPalette := func() SpecPalette {
		return SpecPaletteFor(darkMemo.Get())
	}
	actionsBlocked := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.MergeConflictOpen()
	}

	rulesDraft := core.NewSignal(cx, ed.ProjectRules())
	rulesErr := core.NewSignal(cx, "")
	lastSyncedRulesRev := contentRev.Get()
	cx.Effect(func() {
		cur := contentRev.Get()
		if cur == lastSyncedRulesRev {
			return
		}
		lastSyncedRulesRev = cur
		rulesDraft.Set(ed.ProjectRules())
		rulesErr.Set("")
	})

	saveRules := func() {
		rulesErr.Set("")
		if err := ed.SetProjectRules("Edit Project Rules", rulesDraft.Get()); err != nil {
			rulesErr.Set(err.Error())
		}
	}

	p0 := curPalette()
	return core.Scroll{
		TestID: "editor-settings-rules-page",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
		Children: []core.Node{
			core.View{
				Style: core.Style{Gap: 10, Padding: core.Edges{Right: 4, Bottom: 8}},
				Children: []core.Node{
					// Pending reviewable edits to AGENTS.md (from agent MCP tools or clicking "Add as a rule")
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = docRev.Get()
						_ = langMemo.Get()
						p := curPalette()
						pending := ed.PendingDocReviews(DocReviewRules)
						if len(pending) == 0 {
							return nil
						}
						cards := make([]core.Node, 0, len(pending))
						for _, r := range pending {
							cards = append(cards, docReviewCardView(p, ed, r, actionsBlocked))
						}
						return core.View{Style: core.Style{Gap: 10}, Children: cards}
					}),

					// Plain Markdown code editor field (matching Settings.dc.html lines 132-139)
					core.View{
						Style: core.Style{Gap: 6},
						Children: []core.Node{
							core.View{
								Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween},
								Children: []core.Node{
									core.Text{
										Content: ProjectRulesFileName,
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{FontSize: 12, FontFamily: core.FontMonospace, Color: p.TextMuted}
										},
									},
									core.Text{
										ContentFunc: func() string {
											_ = langMemo.Get()
											return ed.T("settings.rulesMarkdownHint")
										},
										StyleFunc: func() core.Style {
											p := curPalette()
											return core.Style{FontSize: 11.5, Color: p.TextFaint}
										},
									},
								},
							},
							ui.Input{
								TestID:    "rules-markdown-input",
								Label:     ProjectRulesFileName,
								Multiline: true,
								PlaceholderFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.rulesPlaceholder")
								},
								Value:    rulesDraft.Get,
								OnChange: func(v string) { rulesDraft.Set(v) },
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{
										Width:        core.Pct(100),
										Height:       core.Pt(210),
										Background:   p.Inset,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
										CornerRadius: 12,
										Color:        p.TextSecondary,
										FontFamily:   core.FontMonospace,
										FontSize:     12,
										FocusVisible: p.FocusRing(2),
									}
								},
							},
						},
					},

					core.Show{
						When: func() bool { return rulesErr.Get() != "" },
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID:      "rules-form-error",
								ContentFunc: rulesErr.Get,
								StyleFunc: func() core.Style {
									p := curPalette()
									return core.Style{FontSize: 11.5, Color: p.Removed.Fg}
								},
							}
						},
					},

					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
						Children: []core.Node{
							ui.Button{
								TestID: "rules-save-btn",
								TextFunc: func() string {
									_ = langMemo.Get()
									return ed.T("settings.rulesSave")
								},
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Disabled: actionsBlocked,
								Color:    func() core.Color { return curPalette().Accent.Fg },
								TextStyle: core.Style{
									FontSize:   12,
									FontWeight: 600,
									Color:      p0.Accent.OnFill,
								},
								Style:   core.Style{Height: core.Pt(30), CornerRadius: 8, FocusVisible: p0.FocusRing(2)},
								OnPress: saveRules,
							},
							ui.Button{
								TestID: "rules-undo-btn",
								TextFunc: func() string {
									_ = langMemo.Get()
									return ed.T("toolbar.undo")
								},
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								Disabled: func() bool {
									_ = rev.Get()
									return !ed.History.CanUndo() || ed.Playing() || ed.MergeConflictOpen()
								},
								Color: func() core.Color { return curPalette().LineRaised },
								TextStyle: core.Style{
									FontSize: 12,
									Color:    p0.TextSecondary,
								},
								Style:   core.Style{Height: core.Pt(30), CornerRadius: 8, FocusVisible: p0.FocusRing(2)},
								OnPress: func() { _ = ed.Undo() },
							},
						},
					},

					// Suggested rules derived from patterns in reverted reviews (matching Settings.dc.html lines 140-143)
					core.Dynamic(func(cx *core.Scope) core.Node {
						_ = docRev.Get()
						_ = langMemo.Get()
						p := curPalette()
						suggested := ed.SuggestedProjectRules()
						if len(suggested) == 0 {
							return nil
						}
						cards := make([]core.Node, 0, len(suggested))
						for _, sug := range suggested {
							s := sug
							cards = append(cards, core.View{
								TestID: "rules-suggestion-card-" + s.ID,
								Style: core.Style{
									Direction:    core.Row,
									AlignItems:   core.AlignCenter,
									Gap:          10,
									Padding:      core.Symmetric(12, 14),
									CornerRadius: 10,
									Background:   p.Raised,
									BorderWidth:  1,
									BorderColor:  p.LineRaised,
								},
								Children: []core.Node{
									core.Text{
										Content: s.Reason,
										Style:   core.Style{FontSize: 12, Color: p.TextMuted, FlexGrow: 1, FlexShrink: 1},
									},
									ui.Button{
										TestID:   "rules-suggestion-add-" + s.ID,
										Text:     ed.T("settings.rulesAddSuggested"),
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonGhost,
										Disabled: actionsBlocked,
										Color:    func() core.Color { return curPalette().LineRaised },
										TextStyle: core.Style{
											FontSize:   12,
											FontWeight: 500,
											Color:      p.TextSecondary,
										},
										Style: core.Style{Height: core.Pt(30), CornerRadius: 8, FocusVisible: p.FocusRing(2)},
										OnPress: func() {
											if _, err := ed.ProposeSuggestedRule(s.ID); err != nil {
												rulesErr.Set(err.Error())
											}
										},
									},
									ui.Button{
										TestID:   "rules-suggestion-dismiss-" + s.ID,
										Text:     "×",
										Label:    ed.T("settings.dismiss"),
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonGhost,
										Disabled: actionsBlocked,
										Color:    func() core.Color { return curPalette().Raised },
										TextStyle: core.Style{
											FontSize: 12,
											Color:    p.TextFaint,
										},
										Style:   core.Style{Width: core.Pt(28), Height: core.Pt(28), CornerRadius: 6, FocusVisible: p.FocusRing(2)},
										OnPress: func() { ed.DismissSuggestedRule(s.ID) },
									},
								},
							})
						}
						return core.View{Style: core.Style{Gap: 10}, Children: cards}
					}),
				},
			},
		},
	}
}

func docReviewCardView(p SpecPalette, ed *Editor, r DocReview, disabled func() bool) core.Node {
	var diffNodes []core.Node
	for _, line := range strings.Split(strings.TrimSuffix(r.Diff, "\n"), "\n") {
		col := p.TextMuted
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			col = p.Added.Fg
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			col = p.Removed.Fg
		case strings.HasPrefix(line, "@@"):
			col = p.TextFaint
		}
		diffNodes = append(diffNodes, core.Text{
			Content: line,
			Style:   core.Style{FontSize: 11, FontFamily: core.FontMonospace, Color: col},
		})
	}
	id := r.ID
	return core.View{
		TestID: "doc-review-card-" + id,
		Style: core.Style{
			Gap:          8,
			Padding:      core.All(12),
			CornerRadius: 10,
			Background:   p.Agent.Fill,
			BorderWidth:  1,
			BorderColor:  p.Agent.Edge,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 8},
				Children: []core.Node{
					core.Text{
						Content: ed.T("settings.reviewPending", l10n.Param("summary", r.Summary)),
						Style:   core.Style{FontSize: 12, FontWeight: 600, Color: p.TextPrimary, FlexGrow: 1, FlexShrink: 1},
					},
					core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							ui.Button{
								TestID:   "doc-review-revert-" + id,
								Text:     ed.T("settings.reviewRevert"),
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Disabled: disabled,
								Color:    func() core.Color { return p.LineRaised },
								TextStyle: core.Style{
									FontSize: 11.5,
									Color:    p.TextSecondary,
								},
								Style:   core.Style{Height: core.Pt(28), CornerRadius: 6, FocusVisible: p.FocusRing(2)},
								OnPress: func() { _ = ed.RevertDocReview(id) },
							},
							ui.Button{
								TestID:   "doc-review-keep-" + id,
								Text:     ed.T("settings.reviewKeep"),
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonGhost,
								Disabled: disabled,
								Color:    func() core.Color { return p.Accent.Fg },
								TextStyle: core.Style{
									FontSize:   11.5,
									FontWeight: 600,
									Color:      p.Accent.OnFill,
								},
								Style:   core.Style{Height: core.Pt(28), CornerRadius: 6, FocusVisible: p.FocusRing(2)},
								OnPress: func() { _ = ed.KeepDocReview(id) },
							},
						},
					},
				},
			},
			core.View{
				Style: core.Style{
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 8,
					Background:   p.Inset,
					BorderWidth:  1,
					BorderColor:  p.Line,
					Gap:          2,
				},
				Children: diffNodes,
			},
		},
	}
}

// settingsAgentCardView renders the right-hand "Agent on this page" card (Settings.dc.html lines 210-218).
func settingsAgentCardView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], activeTab EditorSettingsTab, pageTitle string) core.Node {
	if activeTab != EditorSettingsTabBrief && activeTab != EditorSettingsTabRules {
		return nil
	}
	askDraft := core.NewSignal(cx, "")
	docRev := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.agentDocsRev()
	})
	langMemo := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ResolvedLanguage()
	})
	darkMemo := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.SpecPalette(cx).Dark
	})
	curPalette := func() SpecPalette {
		return SpecPaletteFor(darkMemo.Get())
	}
	actionsBlocked := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.MergeConflictOpen()
	}

	sendAsk := func() {
		if actionsBlocked() {
			return
		}
		q := strings.TrimSpace(askDraft.Get())
		if q == "" {
			return
		}
		ed.AskAgentFromSettings("page_composer", q)
		askDraft.Set("")
	}

	dynamicTop := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = docRev.Get()
		_ = langMemo.Get()
		p := curPalette()
		var note string
		var suggestions []string
		switch activeTab {
		case EditorSettingsTabBrief:
			note, suggestions = ed.BriefAgentNote()
		case EditorSettingsTabRules:
			note, suggestions = ed.RulesAgentNote()
		}

		items := []core.Node{
			core.Text{
				TestID:  "settings-agent-note",
				Content: note,
				Style:   core.Style{FontSize: 12, Color: p.TextMuted},
			},
		}
		for i, s := range suggestions {
			text := s
			items = append(items, ui.Button{
				TestID:   fmt.Sprintf("settings-agent-suggestion-%d", i),
				Text:     text,
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonGhost,
				Disabled: actionsBlocked,
				Color:    func() core.Color { return curPalette().Raised },
				TextStyle: core.Style{
					FontSize: 12,
					Color:    p.TextSecondary,
				},
				Style: core.Style{
					Width:        core.Pct(100),
					MinHeight:    core.Pt(34),
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 10,
					BorderWidth:  1,
					BorderColor:  p.LineRaised,
					FocusVisible: p.FocusRing(2),
				},
				OnPress: func() {
					ed.AskAgentFromSettings("suggestion", text)
				},
			})
		}
		if last := ed.LastSettingsAgentRequest(); last.Prompt != "" {
			items = append(items, core.View{
				TestID: "settings-agent-dispatched",
				Style: core.Style{
					Padding:      core.Symmetric(8, 10),
					CornerRadius: 8,
					Background:   p.Agent.Fill,
					BorderWidth:  1,
					BorderColor:  p.Agent.Edge,
				},
				Children: []core.Node{
					core.Text{
						Content: ed.T("settings.agentSent", l10n.Param("prompt", last.Prompt)),
						Style:   core.Style{FontSize: 11, Color: p.TextSecondary},
					},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 10}, Children: items}
	})

	p0 := curPalette()
	placeholderFunc := func() string {
		_ = langMemo.Get()
		title := pageTitle
		switch activeTab {
		case EditorSettingsTabBrief:
			title = ed.T("settings.brief")
		case EditorSettingsTabRules:
			title = ed.T("settings.rules")
		}
		return ed.T("settings.agentAskPlaceholder", l10n.Param("page", title))
	}

	return core.View{
		TestID: "settings-agent-card",
		StyleFunc: func() core.Style {
			p := curPalette()
			return core.Style{
				Width:       core.Pt(272),
				FlexShrink:  0,
				MinHeight:   core.Pt(0),
				Overflow:    core.Hidden,
				Background:  p.Panel,
				Borders:     core.Edges{Left: 1},
				BorderColor: p.Line,
				Padding:     core.All(16),
				Gap:         10,
			}
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
				Children: []core.Node{
					core.View{
						StyleFunc: func() core.Style {
							p := curPalette()
							return core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 4, Background: p.Agent.Fg}
						},
					},
					core.Text{
						ContentFunc: func() string {
							_ = langMemo.Get()
							return ed.T("settings.agentOnPage")
						},
						StyleFunc: func() core.Style {
							p := curPalette()
							return core.Style{FontSize: 12.5, FontWeight: 600, Color: p.Agent.Fg}
						},
					},
				},
			},
			dynamicTop,
			core.View{Style: core.Style{FlexGrow: 1}},
			core.View{
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					ui.Input{
						TestID:          "settings-agent-input",
						LabelFunc:       placeholderFunc,
						PlaceholderFunc: placeholderFunc,
						Value:           askDraft.Get,
						OnChange:        func(v string) { askDraft.Set(v) },
						OnSubmit:        func(string) { sendAsk() },
						StyleFunc: func() core.Style {
							p := curPalette()
							return core.Style{
								FlexGrow:     1,
								Height:       core.Pt(34),
								Background:   p.Raised,
								BorderWidth:  1,
								BorderColor:  p.LineRaised,
								CornerRadius: 10,
								Color:        p.TextSecondary,
								FontSize:     12,
								FocusVisible: p.FocusRing(2),
							}
						},
					},
					ui.Button{
						TestID: "settings-agent-send",
						TextFunc: func() string {
							_ = langMemo.Get()
							return ed.T("settings.agentSend")
						},
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: actionsBlocked,
						Color:    func() core.Color { return curPalette().TextPrimary },
						TextStyle: core.Style{
							FontSize:   11.5,
							FontWeight: 600,
							Color:      p0.OnTextPrimary,
						},
						Style:   core.Style{Height: core.Pt(34), CornerRadius: 8, FocusVisible: p0.FocusRing(2)},
						OnPress: sendAsk,
					},
				},
			},
		},
	}
}

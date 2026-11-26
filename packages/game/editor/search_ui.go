package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// SearchPanelView renders the Project Search and Find References panel below
// the viewport while a project is open and the panel is toggled on (via
// FilesView's Search or Find Refs buttons, SetSearchOpen, SetSearchQuery, or
// ShowReferences).
func SearchPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	visible := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil && ed.ProjectPath != "" && ed.SearchOpen()
	})

	type searchSnapshot struct {
		tab        SearchTab
		query      string
		results    []SearchResult
		refsTarget string
		refs       []refs.Ref
		loading    bool
		errMsg     string
		dark       bool
	}
	snap := core.NewMemoFunc(cx, func() searchSnapshot {
		_ = rev.Get()
		var errMsg string
		if err := ed.SearchErr(); err != nil {
			errMsg = err.Error()
		}
		return searchSnapshot{
			tab:        ed.SearchTab(),
			query:      ed.SearchQuery(),
			results:    ed.SearchResults(),
			refsTarget: ed.ReferencesTarget(),
			refs:       ed.References(),
			loading:    ed.SearchLoading(),
			errMsg:     errMsg,
			dark:       cx.Dark(),
		}
	}, func(a, b searchSnapshot) bool {
		return a.tab == b.tab &&
			a.query == b.query &&
			a.refsTarget == b.refsTarget &&
			a.loading == b.loading &&
			a.errMsg == b.errMsg &&
			a.dark == b.dark &&
			slices.Equal(a.results, b.results) &&
			slices.Equal(a.refs, b.refs)
	})

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		s := snap.Get()
		p := ed.SpecPalette(cx)
		if s.loading {
			return specLoadingState(p, "search-loading", ed.T("search.loading"))
		}
		if s.errMsg != "" {
			retryBtn := specPanelButton(p, specPanelButtonSpec{
				TestID:     "search-error-retry",
				Text:       ed.T("search.retry"),
				Icon:       lucide.RefreshCw,
				Label:      ed.T("search.refreshLabel"),
				Tooltip:    ed.T("search.refreshLabel"),
				Variant:    specBtnQuiet,
				Compact:    true,
				InsetFocus: true,
				OnPress: func() {
					ed.report(ed.RefreshSearch())
				},
			})
			return specStatusBanner(p, "search-error", false, s.errMsg, nil, retryBtn)
		}
		if s.tab == SearchTabReferences {
			return searchRefsList(p, ed, s.refsTarget, s.refs)
		}
		return searchResultsList(p, ed, s.query, s.results)
	})

	return core.Show{
		When: visible.Get,
		Then: func(cx *core.Scope) core.Node {
			p := ed.SpecPalette(cx)
			header := specPanelHeader(p,
				specPanelToggleGroup(p,
					ed.T("search.search"),
					specPanelToggle(p, specPanelToggleSpec{
						TestID:  "search-tab-search",
						Text:    ed.T("search.search"),
						Icon:    lucide.Search,
						Label:   ed.T("search.search"),
						Tooltip: ed.T("search.search"),
						On: func() bool {
							return snap.Get().tab == SearchTabSearch
						},
						OnChange: func(on bool) {
							if on {
								ed.SetSearchTab(SearchTabSearch)
							}
						},
					}),
					specPanelToggle(p, specPanelToggleSpec{
						TestID:  "search-tab-refs",
						Text:    ed.T("search.references"),
						Icon:    lucide.Link2,
						Label:   ed.T("search.references"),
						Tooltip: ed.T("search.references"),
						Disabled: func() bool {
							return snap.Get().refsTarget == ""
						},
						On: func() bool {
							return snap.Get().tab == SearchTabReferences
						},
						OnChange: func(on bool) {
							if on {
								ed.SetSearchTab(SearchTabReferences)
							}
						},
					}),
				),
				core.View{
					Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(200)},
					Children: []core.Node{ui.Input{
						TestID:      "project-search-input",
						Label:       ed.T("search.inputLabel"),
						Placeholder: ed.T("search.placeholder"),
						Style:       specInputStyle(p),
						Value: func() string {
							_ = rev.Get()
							return ed.SearchQuery()
						},
						OnChange: func(v string) {
							ed.report(ed.SetSearchQuery(v))
						},
					}},
				},
				core.Text{
					TestID: "search-count",
					ContentFunc: func() string {
						s := snap.Get()
						if s.tab == SearchTabReferences {
							if s.refsTarget == "" {
								return ""
							}
							return fmt.Sprintf("%s (%d)", s.refsTarget, len(s.refs))
						}
						if strings.TrimSpace(s.query) == "" {
							return ""
						}
						return ed.T("search.matchCount", l10n.Param("count", len(s.results)))
					},
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextFaint,
						FlexShrink: 0,
					},
				},
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "search-refresh",
					Text:    ed.T("search.refresh"),
					Icon:    lucide.RefreshCw,
					Label:   ed.T("search.refreshLabel"),
					Tooltip: ed.T("search.refreshLabel"),
					Variant: specBtnQuiet,
					Compact: true,
					OnPress: func() {
						ed.report(ed.RefreshSearch())
					},
				}),
				specPanelButton(p, specPanelButtonSpec{
					TestID:  "search-close",
					Icon:    lucide.X,
					Label:   ed.T("search.closeLabel"),
					Tooltip: specShortcutTip(ed.T("search.closeLabel"), "Esc"),
					Variant: specBtnGhost,
					Compact: true,
					OnPress: func() {
						ed.SetSearchOpen(false)
					},
				}),
			)

			scroll := ui.ScrollView{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				ContentStyle: core.Style{
					Padding: core.Symmetric(specSpace8, specSpace16),
					Gap:     specSpace8,
				},
				Children: []core.Node{body},
			}

			return specPanelContainer(p, "search-panel", ed.T("search.search"), 204, header, scroll)
		},
	}
}

func searchKindIcon(kind SearchKind) func() *core.SVG {
	switch kind {
	case SearchUID:
		return lucide.Hash
	case SearchObject:
		return lucide.Box
	case SearchComponent:
		return lucide.Component
	case SearchPrefab:
		return lucide.Layers
	case SearchSceneRef:
		return lucide.Clapperboard
	default:
		return lucide.FileText
	}
}

func searchResultsList(p SpecPalette, ed *Editor, query string, results []SearchResult) core.Node {
	if strings.TrimSpace(query) == "" {
		return specEmptyState(p, "search-empty", lucide.Search, p.TextFaint, ed.T("search.emptyPrompt"), "", nil)
	}
	if len(results) == 0 {
		return specEmptyState(p, "search-empty", lucide.SearchX, p.TextFaint, ed.T("search.noMatches"), "", nil)
	}
	rows := make([]core.Node, 0, len(results))
	for i, res := range results {
		r := res
		primary := r.File
		if r.ObjectID != "" {
			primary = r.ObjectID + " — " + r.File
		}
		aria := fmt.Sprintf("[%s] %s", r.Kind, primary)
		if r.Detail != "" {
			aria += " — " + r.Detail
		}
		rows = append(rows, specPanelRowButton(p, specPanelRowButtonSpec{
			TestID:  fmt.Sprintf("search-result-%d", i),
			Label:   aria,
			Tooltip: "Open " + primary,
			OnPress: func() {
				ed.report(ed.OpenSearchResult(r))
			},
			Children: []core.Node{
				specIcon(searchKindIcon(r.Kind), specIconSize, p.TextMuted),
				core.View{
					Style: core.Style{
						Height:         core.Pt(20),
						Padding:        core.Symmetric(0, specSpace8),
						CornerRadius:   specRadiusControl,
						Background:     p.Panel,
						BorderWidth:    1,
						BorderColor:    p.LineRaised,
						AlignItems:     core.AlignCenter,
						JustifyContent: core.JustifyCenter,
						FlexShrink:     0,
					},
					Children: []core.Node{
						core.Text{
							Content: string(r.Kind),
							Style: core.Style{
								FontSize:   11,
								FontWeight: core.FontWeightSemibold,
								Color:      p.TextFaint,
							},
						},
					},
				},
				core.Text{
					Content: primary,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
				core.Text{
					Content: r.Detail,
					Style: core.Style{
						FontSize:   12.5,
						Color:      p.TextMuted,
						FlexGrow:   1,
						FlexShrink: 1,
					},
				},
			},
		}))
	}
	return core.View{TestID: "search-results", Style: core.Style{Gap: specSpace4}, Children: rows}
}

func searchRefsList(p SpecPalette, ed *Editor, target string, found []refs.Ref) core.Node {
	if target == "" {
		return specEmptyState(p, "refs-empty", lucide.Link2, p.TextFaint, ed.T("search.refsPrompt"), "", nil)
	}
	if len(found) == 0 {
		return specEmptyState(p, "refs-empty", lucide.Link2, p.TextFaint, ed.T("search.noRefs", l10n.Param("target", target)), "", nil)
	}
	rows := make([]core.Node, 0, len(found))
	for i, ref := range found {
		r := ref
		loc := r.File
		if r.Line > 0 {
			loc += ":" + strconv.Itoa(r.Line)
		}
		detail := r.Field
		if r.Value != "" {
			detail += " = " + r.Value
		}
		aria := fmt.Sprintf("Reference %s — %s", loc, detail)
		rows = append(rows, specPanelRowButton(p, specPanelRowButtonSpec{
			TestID:  fmt.Sprintf("refs-row-%d", i),
			Label:   aria,
			Tooltip: "Open " + loc,
			OnPress: func() {
				ed.report(ed.OpenReference(r))
			},
			Children: []core.Node{
				specIcon(lucide.Link2, specIconSize, p.TextMuted),
				core.Text{
					Content: loc,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						FontWeight: core.FontWeightSemibold,
						Color:      p.TextPrimary,
						FlexShrink: 0,
					},
				},
				core.Text{
					Content: detail,
					Style: core.Style{
						FontFamily: core.FontMonospace,
						FontSize:   11.5,
						Color:      p.TextMuted,
						FlexGrow:   1,
						FlexShrink: 1,
					},
				},
			},
		}))
	}
	return core.View{TestID: "refs-results", Style: core.Style{Gap: specSpace4}, Children: rows}
}

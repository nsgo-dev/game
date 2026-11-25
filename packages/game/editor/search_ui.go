package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// SearchPanelView renders the Project Search and Find References panel below
// the viewport while a project is open and the panel is toggled on (via
// FilesView's Search or Find Refs buttons, SetSearchOpen, SetSearchQuery, or
// ShowReferences).
func SearchPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
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
		errMsg     string
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
			errMsg:     errMsg,
		}
	}, func(a, b searchSnapshot) bool {
		return a.tab == b.tab &&
			a.query == b.query &&
			a.refsTarget == b.refsTarget &&
			a.errMsg == b.errMsg &&
			slices.Equal(a.results, b.results) &&
			slices.Equal(a.refs, b.refs)
	})

	body := core.Dynamic(func(cx *core.Scope) core.Node {
		s := snap.Get()
		if s.errMsg != "" {
			return core.Text{
				TestID:  "search-error",
				Content: s.errMsg,
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		if s.tab == SearchTabReferences {
			return searchRefsList(cx, ed, s.refsTarget, s.refs)
		}
		return searchResultsList(cx, ed, s.query, s.results)
	})

	return core.Show{
		When: visible.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "search-panel",
				Style: core.Style{
					Height:      core.Pt(180),
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.All(8),
					Gap:         6,
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
						Children: []core.Node{
							ui.ToggleButton{
								TestID: "search-tab-search",
								Text:   "Search",
								On: func() bool {
									return snap.Get().tab == SearchTabSearch
								},
								OnChange: func(on bool) {
									if on {
										ed.SetSearchTab(SearchTabSearch)
									}
								},
							},
							ui.ToggleButton{
								TestID: "search-tab-refs",
								Text:   "References",
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
							},
							core.View{
								Style: core.Style{FlexGrow: 1},
								Children: []core.Node{ui.Input{
									TestID:      "project-search-input",
									Label:       "Search project files, UIDs, objects, components, prefabs and scene references",
									Placeholder: "Search files, UIDs, objects, components, prefabs...",
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
									return fmt.Sprintf("%d match(es)", len(s.results))
								},
								Style: core.Style{FontSize: 11, Color: colors.Muted()},
							},
							ui.Button{
								TestID:  "search-refresh",
								Text:    "Refresh",
								Label:   "Rescan the project index and references",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								OnPress: func() {
									ed.report(ed.RefreshSearch())
								},
							},
							ui.Button{
								TestID:  "search-close",
								Text:    "Close",
								Label:   "Close the search panel",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonGhost,
								OnPress: func() {
									ed.SetSearchOpen(false)
								},
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1},
						Children: []core.Node{body},
					},
				},
			}
		},
	}
}

func searchResultsList(cx *core.Scope, ed *Editor, query string, results []SearchResult) core.Node {
	colors := ui.ColorsOf(cx)
	if strings.TrimSpace(query) == "" {
		return core.Text{
			TestID:  "search-empty",
			Content: "Type to search project files, UIDs, scene objects, components, prefabs, and scene references",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		}
	}
	if len(results) == 0 {
		return core.Text{
			TestID:  "search-empty",
			Content: "No matches",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		}
	}
	rows := make([]core.Node, 0, len(results))
	for i, res := range results {
		r := res
		primary := r.File
		if r.ObjectID != "" {
			primary = r.ObjectID + " — " + r.File
		}
		rows = append(rows, core.View{
			TestID: fmt.Sprintf("search-result-%d", i),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(3, 6),
				CornerRadius: 4,
				Background:   colors.Surface(),
			},
			OnPress: func(core.PressEvent) {
				ed.openFromSearchAsked(r.File, r.ObjectID != "", func() error { return ed.OpenSearchResult(r) })
			},
			Children: []core.Node{
				core.Text{
					Content: "[" + string(r.Kind) + "]",
					Style:   core.Style{FontSize: 10, FontWeight: 700, Color: colors.Muted()},
				},
				core.Text{
					Content: primary,
					Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
				},
				core.Text{
					Content: r.Detail,
					Style:   core.Style{FontSize: 11, Color: colors.Muted(), FlexGrow: 1},
				},
			},
		})
	}
	return core.View{TestID: "search-results", Style: core.Style{Gap: 2}, Children: rows}
}

func searchRefsList(cx *core.Scope, ed *Editor, target string, found []refs.Ref) core.Node {
	colors := ui.ColorsOf(cx)
	if target == "" {
		return core.Text{
			TestID:  "refs-empty",
			Content: "Select a file in the file browser and press Find Refs",
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		}
	}
	if len(found) == 0 {
		return core.Text{
			TestID:  "refs-empty",
			Content: "No references to " + target,
			Style:   core.Style{FontSize: 11, Color: colors.Muted()},
		}
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
		rows = append(rows, core.View{
			TestID: fmt.Sprintf("refs-row-%d", i),
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          8,
				Padding:      core.Symmetric(3, 6),
				CornerRadius: 4,
				Background:   colors.Surface(),
			},
			OnPress: func(core.PressEvent) {
				ed.openFromSearchAsked(r.File, false, func() error { return ed.OpenReference(r) })
			},
			Children: []core.Node{
				core.Text{
					Content: loc,
					Style:   core.Style{FontSize: 11, FontWeight: 600, Color: colors.Text()},
				},
				core.Text{
					Content: detail,
					Style:   core.Style{FontSize: 11, Color: colors.Muted(), FlexGrow: 1},
				},
			},
		})
	}
	return core.View{TestID: "refs-results", Style: core.Style{Gap: 2}, Children: rows}
}

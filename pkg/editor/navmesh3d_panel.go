package editor

import (
	"strconv"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// NavMeshPanelView renders the docked 3D Navigation Mesh bake settings panel below the viewport
// while Editor.NavMeshPanelOpen is true: agent radius, height, step height (max climb), max slope,
// cell size, cell height, agent name, output .navmesh.yaml path, overlay toggle, and Bake button.
func NavMeshPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	floatFormat := func(v float64) string {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}

	updateCfg := func(mutate func(cfg *NavMeshBakeConfig)) {
		cfg := ed.NavMeshBakeConfig()
		mutate(&cfg)
		ed.SetNavMeshBakeConfig(cfg)
	}

	field := func(label string, width core.Points, control core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Width: core.Pt(width),
				Gap:   3,
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						FontSize: 11,
						Color:    colors.Muted(),
					},
				},
				control,
			},
		}
	}

	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.NavMeshPanelOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "navmesh-panel",
				Style: dockedStyle(cx, core.Style{
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.Symmetric(8, 12),
					Gap:         8,
				}),
				Children: []core.Node{
					// Header row: title, baked summary, overlay switch, Bake button, Close button.
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							ui.HStack{
								Gap: 10,
								Children: []core.Node{
									core.Text{
										Content: ed.T("navmesh3d.title"),
										Style: core.Style{
											FontSize:   13,
											FontWeight: 700,
											Color:      colors.Text(),
										},
									},
									core.Text{
										TestID: "navmesh-summary",
										ContentFunc: func() string {
											_ = rev.Get()
											m := ed.NavMesh3DMesh()
											if m == nil {
												return ed.T("navmesh3d.notBaked")
											}
											if ed.navMesh3D.lastPath != "" {
												return ed.T("navmesh3d.summaryWithPath",
													l10n.Param("agent", m.Agent.Name),
													l10n.Param("polygons", len(m.Polygons)),
													l10n.Param("vertices", len(m.Vertices)),
													l10n.Param("path", ed.navMesh3D.lastPath),
												)
											}
											return ed.T("navmesh3d.summary",
												l10n.Param("agent", m.Agent.Name),
												l10n.Param("polygons", len(m.Polygons)),
												l10n.Param("vertices", len(m.Vertices)),
											)
										},
										Style: core.Style{
											FontSize: 11,
											Color:    colors.Muted(),
										},
									},
								},
							},
							ui.HStack{
								Gap: 8,
								Children: []core.Node{
									ui.HStack{
										Gap: 4,
										Children: []core.Node{
											core.Text{
												Content: ed.T("navmesh3d.overlay"),
												Style: core.Style{
													FontSize: 11,
													Color:    colors.Text(),
												},
											},
											SpecSwitch{
												TestID: "navmesh-overlay-toggle",
												Label:  ed.T("navmesh3d.overlayLabel"),
												Checked: func() bool {
													_ = rev.Get()
													return ed.ShowNavMesh3D
												},
												OnChange: func(on bool) {
													ed.SetShowNavMesh3D(on)
												},
											},
										},
									},
									ui.Button{
										TestID:   "navmesh-bake",
										Text:     ed.T("navmesh3d.bake"),
										Label:    ed.T("navmesh3d.bakeLabel"),
										Size:     ui.SizeSmall,
										Variant:  ui.ButtonPrimary,
										Disabled: disabled,
										OnPress: func() {
											_, err := ed.BakeNavMesh3D()
											ed.report(err)
										},
									},
									ui.Button{
										TestID:  "navmesh-close",
										Text:    ed.T("navmesh3d.close"),
										Size:    ui.SizeSmall,
										Variant: ui.ButtonGhost,
										OnPress: func() {
											ed.SetNavMeshPanelOpen(false)
										},
									},
								},
							},
						},
					},

					// Settings fields row (wraps cleanly on narrower windows).
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							Wrap:       core.Wrap,
							AlignItems: core.AlignEnd,
							Gap:        8,
						},
						Children: []core.Node{
							field(ed.T("navmesh3d.agent"), 112, ui.Input{
								TestID:      "navmesh-agent-name",
								Label:       ed.T("navmesh3d.agentLabel"),
								Placeholder: ed.T("navmesh3d.agentPlaceholder"),
								Disabled:    disabled,
								Value: func() string {
									_ = rev.Get()
									return ed.NavMeshBakeConfig().AgentName
								},
								OnChange: func(text string) {
									updateCfg(func(c *NavMeshBakeConfig) { c.AgentName = text })
								},
							}),
							field(ed.T("navmesh3d.radius"), 88, ui.NumberInput{
								TestID:   "navmesh-agent-radius",
								Label:    ed.T("navmesh3d.radiusLabel"),
								Min:      0.01,
								Step:     0.1,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().Radius)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.Radius = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.height"), 88, ui.NumberInput{
								TestID:   "navmesh-agent-height",
								Label:    ed.T("navmesh3d.heightLabel"),
								Min:      0.01,
								Step:     0.1,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().Height)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.Height = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.stepHeight"), 88, ui.NumberInput{
								TestID:   "navmesh-agent-max-climb",
								Label:    ed.T("navmesh3d.stepHeightLabel"),
								Min:      0.01,
								Step:     0.1,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().MaxClimb)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.MaxClimb = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.maxSlope"), 92, ui.NumberInput{
								TestID:   "navmesh-agent-max-slope",
								Label:    ed.T("navmesh3d.maxSlopeLabel"),
								Min:      1,
								Max:      89,
								Step:     1,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().MaxSlope)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.MaxSlope = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.cellSize"), 88, ui.NumberInput{
								TestID:   "navmesh-cell-size",
								Label:    ed.T("navmesh3d.cellSizeLabel"),
								Min:      0.01,
								Step:     0.05,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().CellSize)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.CellSize = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.cellHeight"), 88, ui.NumberInput{
								TestID:   "navmesh-cell-height",
								Label:    ed.T("navmesh3d.cellHeightLabel"),
								Min:      0.01,
								Step:     0.05,
								Format:   floatFormat,
								Disabled: disabled,
								Value: func() float64 {
									_ = rev.Get()
									return float64(ed.NavMeshBakeConfig().CellHeight)
								},
								OnChange: func(v float64) {
									updateCfg(func(c *NavMeshBakeConfig) { c.CellHeight = float32(v) })
								},
							}),
							field(ed.T("navmesh3d.outputPath"), 220, ui.Input{
								TestID:      "navmesh-output-path",
								Label:       ed.T("navmesh3d.outputPathLabel"),
								Placeholder: "levels/level.navmesh.yaml",
								Disabled:    disabled,
								Value: func() string {
									_ = rev.Get()
									return ed.NavMeshBakeConfig().OutputPath
								},
								OnChange: func(text string) {
									updateCfg(func(c *NavMeshBakeConfig) { c.OutputPath = text })
								},
							}),
						},
					},

					// Error message when the last bake failed.
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.NavMesh3DError() != ""
						},
						Then: func(cx *core.Scope) core.Node {
							return core.Text{
								TestID: "navmesh-error",
								ContentFunc: func() string {
									_ = rev.Get()
									return ed.NavMesh3DError()
								},
								Style: core.Style{
									FontSize: 11,
									Color:    ed.Palette(cx).Removed.Fg,
								},
							}
						},
					},
				},
			}
		},
	}
}

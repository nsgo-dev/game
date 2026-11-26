package editor

import (
	"fmt"
	"strconv"

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
										Content: "3D NavMesh",
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
												return "No navmesh baked"
											}
											if ed.navMesh3D.lastPath != "" {
												return fmt.Sprintf("%s: %d polygons, %d vertices (%s)", m.Agent.Name, len(m.Polygons), len(m.Vertices), ed.navMesh3D.lastPath)
											}
											return fmt.Sprintf("%s: %d polygons, %d vertices", m.Agent.Name, len(m.Polygons), len(m.Vertices))
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
												Content: "Overlay",
												Style: core.Style{
													FontSize: 11,
													Color:    colors.Text(),
												},
											},
											SpecSwitch{
												TestID: "navmesh-overlay-toggle",
												Label:  "Show 3D NavMesh Overlay",
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
										Text:     "Bake",
										Label:    "Bake 3D navigation mesh from the open scene's static geometry",
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
										Text:    "Close",
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
							field("Agent", 112, ui.Input{
								TestID:      "navmesh-agent-name",
								Label:       "NavMesh Agent Name",
								Placeholder: "default",
								Disabled:    disabled,
								Value: func() string {
									_ = rev.Get()
									return ed.NavMeshBakeConfig().AgentName
								},
								OnChange: func(text string) {
									updateCfg(func(c *NavMeshBakeConfig) { c.AgentName = text })
								},
							}),
							field("Radius", 88, ui.NumberInput{
								TestID:   "navmesh-agent-radius",
								Label:    "Agent Radius",
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
							field("Height", 88, ui.NumberInput{
								TestID:   "navmesh-agent-height",
								Label:    "Agent Height",
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
							field("Step Height", 88, ui.NumberInput{
								TestID:   "navmesh-agent-max-climb",
								Label:    "Agent Step Height",
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
							field("Max Slope (°)", 92, ui.NumberInput{
								TestID:   "navmesh-agent-max-slope",
								Label:    "Max Slope",
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
							field("Cell Size", 88, ui.NumberInput{
								TestID:   "navmesh-cell-size",
								Label:    "Voxel Cell Size",
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
							field("Cell Height", 88, ui.NumberInput{
								TestID:   "navmesh-cell-height",
								Label:    "Voxel Cell Height",
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
							field("Output Path", 220, ui.Input{
								TestID:      "navmesh-output-path",
								Label:       "NavMesh Output Path",
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
									Color:    ed.SpecPalette(cx).Removed.Fg,
								},
							}
						},
					},
				},
			}
		},
	}
}

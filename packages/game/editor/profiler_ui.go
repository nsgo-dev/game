package editor

import (
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Spec color tokens from docs/design/editor/Spec.dc.html and docs/design/editor/PlayMode.dc.html.
// Every color used by the Play-mode frame time strip and the full Profiler panel comes exclusively from this table.
var (
	specBg            = core.RGB(0x101216)
	specBgOverlay     = core.RGBA(0x101216, 0.92)
	specPanel         = core.RGB(0x12141A)
	specRaised        = core.RGB(0x171A21)
	specLine          = core.RGB(0x1F232A)
	specLineRaised    = core.RGB(0x262B35)
	specTextStrong    = core.RGB(0xFFFFFF)
	specText          = core.RGB(0xD9DDE5)
	specTextSecondary = core.RGB(0xB4BAC6)
	specTextMuted     = core.RGB(0x8B92A0)
	specBarNormal     = core.RGBA(0x8B92A0, 0.45)
	specAccent        = core.RGB(0x3B7BFF)
	specAgent         = core.RGB(0x9B7BFF)
	specAdded         = core.RGB(0x4FD38A)
	specAddedFill     = core.RGBA(0x4FD38A, 0.12)
	specAddedEdge     = core.RGBA(0x4FD38A, 0.45)
	specChanged       = core.RGB(0xE9A84A)
	specProblem       = core.RGB(0xFF6B6B)
	specProblemEdge   = core.RGBA(0xFF6B6B, 0.50)
	specWarning       = core.RGB(0xF2C14E)
	specWarningFill   = core.RGB(0x2A2414)
	specWarningEdge   = core.RGB(0x4A3F1F)
)

// ProfilerStripView renders the Play-mode "FRAME TIME · LAST 60 S" strip directly below the game view while Play mode is active or a running game is connected to the profiler, matching docs/design/editor/PlayMode.dc.html. Pressing the strip's Profiler button opens the full Profiler panel.
func ProfilerStripView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	visible := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.ProfilerConnected()
	})

	seq := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.profilerSeq()
	})
	snap := core.NewMemoFunc(cx, func() ProfilerSnapshot {
		_ = seq.Get()
		return ed.ProfilerSnapshot()
	}, profilerSnapshotsEqual)

	return core.Show{
		When: visible.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "profiler-strip",
				Role:   core.RoleRegion,
				Label:  "Frame time last 60 seconds",
				Style: core.Style{
					Height:      core.Pt(176),
					FlexShrink:  0,
					Background:  specPanel,
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
					Padding:     core.Symmetric(12, 16),
					Gap:         8,
				},
				Children: []core.Node{
					// Header row: title, avg, p99, allocs/frame, legend, and button to open the full Profiler panel.
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Height:     core.Pt(28),
							Gap:        12,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "profiler-strip-title",
								Content: "FRAME TIME · LAST 60 S",
								Style: core.Style{
									FontSize:   11,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextMuted,
								},
							},
							core.Text{
								TestID: "profiler-strip-avg",
								ContentFunc: func() string {
									s := snap.Get()
									avg := profilerAvgMs(s)
									if !s.HasReadout && len(s.Frames) == 0 {
										return "avg — ms"
									}
									return fmt.Sprintf("avg %.1f ms", avg)
								},
								Style: core.Style{
									FontSize:    12.5,
									FontVariant: core.FontTabularNums,
									Color:       specText,
								},
							},
							core.Text{
								TestID: "profiler-strip-p99",
								ContentFunc: func() string {
									s := snap.Get()
									if !s.HasReadout && len(s.Frames) == 0 {
										return "p99 — ms"
									}
									return fmt.Sprintf("p99 %.1f ms", profilerP99Ms(s))
								},
								Style: core.Style{
									FontSize:    12.5,
									FontVariant: core.FontTabularNums,
									Color:       specTextSecondary,
								},
							},
							core.Text{
								TestID: "profiler-strip-allocs",
								ContentFunc: func() string {
									s := snap.Get()
									if !s.HasReadout && len(s.Frames) == 0 {
										return "0 allocs/frame"
									}
									return fmt.Sprintf("%.0f allocs/frame", s.Readout.AllocsPerFrame)
								},
								Style: core.Style{
									FontSize:    12.5,
									FontVariant: core.FontTabularNums,
									Color:       specTextSecondary,
								},
							},
							core.View{Style: core.Style{FlexGrow: 1}},
							profilerLegendItem(specWarning, "over 8.3 ms"),
							profilerLegendItem(specProblem, "missed 16.7 ms"),
							specActionButton(
								"profiler-strip-open",
								func() string {
									if snap.Get().Open {
										return "Hide Profiler"
									}
									return "Profiler"
								},
								"Open full Profiler panel",
								false,
								func() {
									ed.SetProfilerOpen(!ed.ProfilerOpen())
								},
							),
						},
					},
					// Bar chart with 8.3 ms / 16.7 ms lines, playhead bar, and spike callout pill.
					core.View{
						Style: core.Style{
							Height:   core.Pt(96),
							Position: core.Relative,
							Overflow: core.Hidden,
						},
						Children: []core.Node{
							core.Canvas{
								TestID: "profiler-strip-chart",
								Label:  "Frame time chart over the last 60 seconds",
								Style: core.Style{
									Width:  core.Pct(100),
									Height: core.Pct(100),
								},
								Draw: func(d *core.Drawing, size core.Size) {
									drawProfilerStripChart(d, size, snap.Get())
								},
							},
							core.Dynamic(func(cx *core.Scope) core.Node {
								s := snap.Get()
								if s.Spike == nil {
									return nil
								}
								// Anchor the pill near the spike's horizontal slot, clamped inside the strip width so it never clips.
								leftPct := float32(math.Max(12, math.Min(54, s.Spike.PositionFrac*100-6)))
								return core.View{
									TestID:        "profiler-strip-spike",
									PointerEvents: core.PointerEventsNone,
									Style: core.Style{
										Position:     core.Absolute,
										Left:         core.Pct(leftPct),
										Top:          core.Pt(2),
										Height:       core.Pt(20),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Padding:      core.Symmetric(0, 8),
										CornerRadius: 10,
										Background:   specBgOverlay,
										BorderWidth:  1,
										BorderColor:  specProblemEdge,
									},
									Children: []core.Node{
										core.Text{
											Content: s.Spike.Label,
											Style: core.Style{
												FontSize:    11,
												FontVariant: core.FontTabularNums,
												Color:       specProblem,
											},
										},
									},
								}
							}),
							func() core.Node {
								type markPin struct {
									idx int
									pct int
								}
								pinsMemo := core.NewMemoFunc(cx, func() []markPin {
									_ = rev.Get()
									marks := ed.PlayMarks()
									if len(marks) == 0 {
										return nil
									}
									curFrame, rate := ed.currentPlayFrameAndRate()
									window := float64(rollingReplayWindowSeconds * max(1, rate))
									var out []markPin
									for i, m := range marks {
										age := float64(curFrame - m.Frame)
										if age < 0 || age >= window {
											continue
										}
										pct := int(math.Round(math.Max(1, math.Min(98, 100-(age/window)*100))))
										out = append(out, markPin{idx: i, pct: pct})
									}
									return out
								}, func(a, b []markPin) bool {
									if len(a) != len(b) {
										return false
									}
									for i := range a {
										if a[i] != b[i] {
											return false
										}
									}
									return true
								})
								return core.Dynamic(func(cx *core.Scope) core.Node {
									pinData := pinsMemo.Get()
									if len(pinData) == 0 {
										return nil
									}
									pins := make([]core.Node, 0, len(pinData))
									for _, p := range pinData {
										pins = append(pins, core.View{
											TestID:        fmt.Sprintf("profiler-strip-mark-pin-%d", p.idx),
											PointerEvents: core.PointerEventsNone,
											Style: core.Style{
												Position:       core.Absolute,
												Left:           core.Pct(float32(p.pct)),
												Top:            core.Pt(0),
												Width:          core.Pt(16),
												Height:         core.Pt(16),
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
												CornerRadius:   4,
												Background:     specLineRaised,
											},
											Children: []core.Node{
												core.Icon{
													SVG:   lucide.Flag(),
													Style: core.Style{Width: core.Pt(10), Height: core.Pt(10), Color: specTextStrong},
												},
											},
										})
									}
									return core.View{
										PointerEvents: core.PointerEventsNone,
										Style: core.Style{
											Position: core.Absolute,
											Left:     core.Pt(0),
											Top:      core.Pt(0),
											Right:    core.Pt(0),
											Bottom:   core.Pt(0),
										},
										Children: pins,
									}
								})
							}(),
						},
					},
					// Axis row: −60 s, −30 s, now (or paused).
					core.View{
						TestID: "profiler-strip-axis",
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							AlignItems:     core.AlignCenter,
						},
						Children: []core.Node{
							core.Text{
								Content: "−60 s",
								Style:   core.Style{FontSize: 11, Color: specTextMuted},
							},
							core.Text{
								Content: "−30 s",
								Style:   core.Style{FontSize: 11, Color: specTextMuted},
							},
							core.Text{
								TestID: "profiler-strip-now",
								ContentFunc: func() string {
									if snap.Get().Connected {
										return "now"
									}
									return "paused"
								},
								Style: core.Style{FontSize: 11, Color: specTextMuted},
							},
						},
					},
				},
			}
		},
	}
}

// ProfilerPanelView renders the full docked Profiler panel below the viewport when Editor.ProfilerOpen reports true.
// It charts frame, update and draw time, FPS, allocations per frame, GC and asset memory over time, lists profiler zones from game_perf_zones sorted by cost, and provides Reset (game_perf_reset) and Chrome/Perfetto trace export through debug.Profiler.WriteChromeTrace.
func ProfilerPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.ProfilerOpen()
	})
	seq := core.NewMemo(cx, func() uint64 {
		_ = rev.Get()
		return ed.profilerSeq()
	})
	snap := core.NewMemoFunc(cx, func() ProfilerSnapshot {
		_ = seq.Get()
		return ed.ProfilerSnapshot()
	}, profilerSnapshotsEqual)

	metricTab := func(testID string, metric ProfilerMetric) core.Node {
		return core.View{
			TestID: testID,
			Role:   core.RoleButton,
			Label:  "Chart " + metric.String(),
			StyleFunc: func() core.Style {
				active := snap.Get().Metric == metric
				bg := specPanel
				border := specLine
				if active {
					bg = specLineRaised
					border = specLineRaised
				}
				return core.Style{
					Height:         core.Pt(28),
					MinWidth:       core.Pt(28),
					Direction:      core.Row,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Padding:        core.Symmetric(0, 12),
					CornerRadius:   6,
					Background:     bg,
					BorderWidth:    1,
					BorderColor:    border,
					Hover:          &core.Style{Background: specRaised},
					FocusVisible:   specFocusRingStyle,
				}
			},
			OnPress: func(core.PressEvent) {
				ed.SetProfilerMetric(metric)
			},
			Children: []core.Node{
				core.Text{
					Content: metric.String(),
					StyleFunc: func() core.Style {
						fg := specTextSecondary
						if snap.Get().Metric == metric {
							fg = specTextStrong
						}
						return core.Style{
							FontSize:   12,
							FontWeight: core.FontWeightMedium,
							Color:      fg,
						}
					},
				},
			},
		}
	}

	zonesList := core.Dynamic(func(cx *core.Scope) core.Node {
		s := snap.Get()
		if len(s.Zones) == 0 {
			msg := "No profiler zones recorded"
			if !s.Connected {
				msg = "Connect a running game or press Play to inspect zones"
			}
			return core.Text{
				TestID:  "profiler-zones-empty",
				Content: msg,
				Style:   core.Style{FontSize: 11.5, Color: specTextMuted, Padding: core.Symmetric(8, 0)},
			}
		}

		maxCost := 0.0
		for _, z := range s.Zones {
			if z.LastMs > maxCost {
				maxCost = z.LastMs
			}
			if z.AvgMs > maxCost {
				maxCost = z.AvgMs
			}
		}
		if maxCost <= 0 {
			maxCost = 1
		}

		rows := make([]core.Node, 0, len(s.Zones))
		for i, z := range s.Zones {
			barColor := specAccent
			if z.LastMs > profilerMissedThresholdMs {
				barColor = specProblem
			} else if z.LastMs > profilerOverThresholdMs {
				barColor = specWarning
			}
			pct := float32(math.Min(100, math.Max(4, (z.LastMs/maxCost)*100)))
			rows = append(rows, core.View{
				TestID: fmt.Sprintf("profiler-zone-%d", i),
				Style: core.Style{
					Height:       core.Pt(28),
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Gap:          8,
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Background:   specPanel,
					FocusVisible: specRowFocusRingStyle,
				},
				Children: []core.Node{
					core.Text{
						Content: z.Name,
						Style: core.Style{
							FontFamily: core.FontMonospace,
							FontSize:   11.5,
							Color:      specText,
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							Overflow:   core.Hidden,
						},
					},
					core.View{
						Style: core.Style{
							Width:        core.Pt(48),
							FlexShrink:   0,
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   specLineRaised,
							Overflow:     core.Hidden,
						},
						Children: []core.Node{
							core.View{
								Style: core.Style{
									Width:        core.Pct(pct),
									Height:       core.Pct(100),
									CornerRadius: 3,
									Background:   barColor,
								},
							},
						},
					},
					core.Text{
						Content: fmt.Sprintf("%.2f ms", z.LastMs),
						Style: core.Style{
							FontFamily:  core.FontMonospace,
							FontSize:    11.5,
							FontVariant: core.FontTabularNums,
							Color:       specTextStrong,
							Width:       core.Pt(62),
							FlexShrink:  0,
							TextAlign:   core.TextAlignRight,
						},
					},
					core.Text{
						Content: fmt.Sprintf("%.2f", z.AvgMs),
						Style: core.Style{
							FontFamily:  core.FontMonospace,
							FontSize:    11.5,
							FontVariant: core.FontTabularNums,
							Color:       specTextSecondary,
							Width:       core.Pt(48),
							FlexShrink:  0,
							TextAlign:   core.TextAlignRight,
						},
					},
					core.Text{
						Content: fmt.Sprintf("%.2f", z.MaxMs),
						Style: core.Style{
							FontFamily:  core.FontMonospace,
							FontSize:    11.5,
							FontVariant: core.FontTabularNums,
							Color:       specTextMuted,
							Width:       core.Pt(48),
							FlexShrink:  0,
							TextAlign:   core.TextAlignRight,
						},
					},
					core.Text{
						Content: fmt.Sprintf("×%d", z.Calls),
						Style: core.Style{
							FontFamily:  core.FontMonospace,
							FontSize:    11.5,
							FontVariant: core.FontTabularNums,
							Color:       specTextMuted,
							Width:       core.Pt(44),
							FlexShrink:  0,
							TextAlign:   core.TextAlignRight,
						},
					},
				},
			})
		}
		return core.View{Style: core.Style{Gap: 4}, Children: rows}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "profiler-panel",
				Role:   core.RoleRegion,
				Label:  "Profiler",
				Style: core.Style{
					Height:      core.Pt(300),
					FlexShrink:  0,
					Background:  specPanel,
					Borders:     core.Edges{Top: 1},
					BorderColor: specLine,
					Padding:     core.Symmetric(12, 16),
					Gap:         12,
				},
				Children: []core.Node{
					// Top bar: title, connection pill, metric tabs, Reset, Export Trace, Close.
					core.View{
						Style: core.Style{
							Direction:  core.Row,
							AlignItems: core.AlignCenter,
							Height:     core.Pt(28),
							Gap:        8,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "profiler-panel-title",
								Content: "PROFILER",
								Style: core.Style{
									FontSize:   11,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextMuted,
								},
							},
							core.View{
								TestID: "profiler-status-pill",
								StyleFunc: func() core.Style {
									s := snap.Get()
									bg := specRaised
									border := specLineRaised
									if s.Connected {
										bg = specAddedFill
										border = specAddedEdge
									}
									return core.Style{
										Height:       core.Pt(24),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          8,
										Padding:      core.Symmetric(0, 12),
										CornerRadius: 12,
										Background:   bg,
										BorderWidth:  1,
										BorderColor:  border,
									}
								},
								Children: []core.Node{
									core.View{
										StyleFunc: func() core.Style {
											dot := specTextMuted
											if snap.Get().Connected {
												dot = specAdded
											}
											return core.Style{
												Width:        core.Pt(6),
												Height:       core.Pt(6),
												CornerRadius: 3,
												Background:   dot,
											}
										},
									},
									core.Text{
										ContentFunc: func() string {
											return snap.Get().SourceLabel
										},
										Style: core.Style{
											FontSize:   11.5,
											FontWeight: core.FontWeightMedium,
											Color:      specText,
										},
									},
								},
							},
							core.View{Style: core.Style{Width: core.Pt(8)}},
							metricTab("profiler-tab-frame", ProfilerMetricFrame),
							metricTab("profiler-tab-fps", ProfilerMetricFPS),
							metricTab("profiler-tab-gc", ProfilerMetricAllocsGC),
							metricTab("profiler-tab-memory", ProfilerMetricMemory),
							core.View{Style: core.Style{FlexGrow: 1}},
							specActionButton(
								"profiler-reset",
								func() string { return "Reset" },
								"Reset profiler timings, zones and GC history",
								false,
								func() {
									ed.resetProfilerFromUI()
								},
							),
							specActionButton(
								"profiler-export",
								func() string { return "Export Trace" },
								"Export recorded frames as Chrome/Perfetto trace JSON",
								true,
								func() {
									ed.exportProfilerTraceFromUI()
								},
							),
							specActionButton(
								"profiler-close",
								func() string { return "Close" },
								"Close Profiler panel",
								false,
								func() {
									ed.SetProfilerOpen(false)
								},
							),
						},
					},
					// Main split body: left charts + KPIs, right sorted profiler zones table.
					core.View{
						Style: core.Style{
							Direction: core.Row,
							FlexGrow:  1,
							FlexBasis: core.Pt(0),
							Gap:       16,
							Overflow:  core.Hidden,
						},
						Children: []core.Node{
							// Left column: 4 KPI cards + main time-series chart.
							core.View{
								Style: core.Style{
									FlexGrow:  1,
									FlexBasis: core.Pt(0),
									Gap:       8,
									Overflow:  core.Hidden,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Direction: core.Row,
											Gap:       8,
										},
										Children: []core.Node{
											profilerKPICard("profiler-kpi-frame", "FRAME", func() string {
												s := snap.Get()
												if !s.HasReadout && len(s.Frames) == 0 {
													return "— ms"
												}
												return fmt.Sprintf("%.2f ms", profilerAvgMs(s))
											}, func() string {
												s := snap.Get()
												if !s.HasReadout && len(s.Frames) == 0 {
													return "upd — · drw — · p99 —"
												}
												return fmt.Sprintf("upd %.1f · drw %.1f · p99 %.1f", s.Readout.UpdateMs, s.Readout.DrawMs, profilerP99Ms(s))
											}, func() core.Color {
												avg := profilerAvgMs(snap.Get())
												if avg > profilerMissedThresholdMs {
													return specProblem
												}
												if avg > profilerOverThresholdMs {
													return specWarning
												}
												return specTextStrong
											}, func() bool { return snap.Get().Metric == ProfilerMetricFrame }, func() { ed.SetProfilerMetric(ProfilerMetricFrame) }),

											profilerKPICard("profiler-kpi-fps", "FPS", func() string {
												s := snap.Get()
												if !s.HasReadout && len(s.Frames) == 0 {
													return "— fps"
												}
												return fmt.Sprintf("%.1f fps", s.Readout.FPS)
											}, func() string {
												s := snap.Get()
												if !s.Readout.Sources.DrawStats && !s.Readout.Sources.Render3D {
													return fmt.Sprintf("%d frames", len(s.Frames))
												}
												return fmt.Sprintf("%d calls · %d sprites", s.Readout.DrawCalls, s.Readout.Sprites)
											}, func() core.Color {
												s := snap.Get()
												if !s.HasReadout {
													return specTextStrong
												}
												if s.Readout.FPS > 0 && s.Readout.FPS < 30 {
													return specProblem
												}
												if s.Readout.FPS > 0 && s.Readout.FPS < 55 {
													return specWarning
												}
												return specAdded
											}, func() bool { return snap.Get().Metric == ProfilerMetricFPS }, func() { ed.SetProfilerMetric(ProfilerMetricFPS) }),

											profilerKPICard("profiler-kpi-gc", "ALLOCS & GC", func() string {
												s := snap.Get()
												if !s.HasReadout {
													return "0 /f"
												}
												return fmt.Sprintf("%.1f /f", s.Readout.AllocsPerFrame)
											}, func() string {
												s := snap.Get()
												if !s.HasReadout {
													return "heap — · 0 GC"
												}
												return fmt.Sprintf("heap %s · %d GC · %d hitches", formatProfilerUintBytes(s.Readout.HeapBytes), s.Readout.GCCount, s.Readout.GCRecentHitches)
											}, func() core.Color {
												if snap.Get().Readout.GCRecentHitches > 0 {
													return specWarning
												}
												return specTextStrong
											}, func() bool { return snap.Get().Metric == ProfilerMetricAllocsGC }, func() { ed.SetProfilerMetric(ProfilerMetricAllocsGC) }),

											profilerKPICard("profiler-kpi-memory", "ASSET MEMORY", func() string {
												s := snap.Get()
												if s.Readout.AssetMemory == nil {
													return "—"
												}
												return formatProfilerBytes(s.Readout.AssetMemory.TotalBytes)
											}, func() string {
												s := snap.Get()
												if s.Readout.AssetMemory == nil {
													return "untracked"
												}
												am := s.Readout.AssetMemory
												if am.TotalBudget > 0 {
													return fmt.Sprintf("budget %s · %d groups", formatProfilerBytes(am.TotalBudget), len(am.Groups))
												}
												return fmt.Sprintf("%d groups", len(am.Groups))
											}, func() core.Color {
												if am := snap.Get().Readout.AssetMemory; am != nil && am.OverBudget {
													return specProblem
												}
												return specTextStrong
											}, func() bool { return snap.Get().Metric == ProfilerMetricMemory }, func() { ed.SetProfilerMetric(ProfilerMetricMemory) }),
										},
									},
									core.View{
										Style: core.Style{
											FlexGrow:     1,
											FlexBasis:    core.Pt(0),
											Background:   specBg,
											CornerRadius: 8,
											BorderWidth:  1,
											BorderColor:  specLineRaised,
											Padding:      core.All(8),
											Overflow:     core.Hidden,
										},
										Children: []core.Node{
											core.Canvas{
												TestID: "profiler-chart",
												Label:  "Profiler time-series chart",
												Style: core.Style{
													Width:  core.Pct(100),
													Height: core.Pct(100),
												},
												Draw: func(d *core.Drawing, size core.Size) {
													drawProfilerMainChart(d, size, snap.Get())
												},
											},
										},
									},
								},
							},
							// Right column: Profiler zones table sorted by cost (from game_perf_zones).
							core.View{
								TestID: "profiler-zones-panel",
								Style: core.Style{
									Width:        core.Pt(440),
									FlexShrink:   0,
									Background:   specRaised,
									CornerRadius: 8,
									BorderWidth:  1,
									BorderColor:  specLineRaised,
									Padding:      core.Symmetric(8, 12),
									Gap:          8,
									Overflow:     core.Hidden,
								},
								Children: []core.Node{
									core.View{
										Style: core.Style{
											Direction:      core.Row,
											AlignItems:     core.AlignCenter,
											JustifyContent: core.JustifySpaceBetween,
										},
										Children: []core.Node{
											core.Text{
												TestID: "profiler-zones-title",
												ContentFunc: func() string {
													s := snap.Get()
													return fmt.Sprintf("ZONES · %d", len(s.Zones))
												},
												Style: core.Style{
													FontSize:   11,
													FontWeight: core.FontWeightSemibold,
													Color:      specTextMuted,
												},
											},
											core.Text{
												TestID: "profiler-frames-count",
												ContentFunc: func() string {
													s := snap.Get()
													if s.LastError != "" {
														return "err: " + s.LastError
													}
													return fmt.Sprintf("%d recorded frames", len(s.Frames))
												},
												StyleFunc: func() core.Style {
													c := specTextMuted
													if snap.Get().LastError != "" {
														c = specProblem
													}
													return core.Style{
														FontSize:    11,
														FontVariant: core.FontTabularNums,
														Color:       c,
													}
												},
											},
										},
									},
									core.View{
										Style: core.Style{
											Direction:   core.Row,
											AlignItems:  core.AlignCenter,
											Gap:         8,
											Padding:     core.Edges{Left: 8, Right: 8, Bottom: 4},
											Borders:     core.Edges{Bottom: 1},
											BorderColor: specLine,
										},
										Children: []core.Node{
											core.Text{Content: "ZONE", Style: core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, Color: specTextMuted, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}},
											core.View{Style: core.Style{Width: core.Pt(48), FlexShrink: 0}},
											core.Text{Content: "LAST", Style: core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, Color: specTextMuted, Width: core.Pt(62), FlexShrink: 0, TextAlign: core.TextAlignRight}},
											core.Text{Content: "AVG", Style: core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, Color: specTextMuted, Width: core.Pt(48), FlexShrink: 0, TextAlign: core.TextAlignRight}},
											core.Text{Content: "MAX", Style: core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, Color: specTextMuted, Width: core.Pt(48), FlexShrink: 0, TextAlign: core.TextAlignRight}},
											core.Text{Content: "CALLS", Style: core.Style{FontSize: 11, FontWeight: core.FontWeightSemibold, Color: specTextMuted, Width: core.Pt(44), FlexShrink: 0, TextAlign: core.TextAlignRight}},
										},
									},
									ui.ScrollView{
										Style:    core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
										Children: []core.Node{zonesList},
									},
								},
							},
						},
					},
				},
			}
		},
	}
}

func profilerLegendItem(swatch core.Color, label string) core.Node {
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        4,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Width:        core.Pt(8),
					Height:       core.Pt(8),
					CornerRadius: 2,
					Background:   swatch,
				},
			},
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize: 11.5,
					Color:    specTextMuted,
				},
			},
		},
	}
}

func specActionButton(testID string, textFunc func() string, label string, primary bool, onPress func()) core.Node {
	bg := specRaised
	fg := specText
	border := specLineRaised
	weight := core.FontWeightMedium
	if primary {
		// Spec contrast rule: dark text (#101216) on white primary fill (18.75:1 AAA) or accent (#3B7BFF, 4.88:1 AA).
		bg = specAccent
		fg = specBg
		border = specAccent
		weight = core.FontWeightSemibold
	}
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  label,
		Style: core.Style{
			Height:         core.Pt(28),
			MinWidth:       core.Pt(28),
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Padding:        core.Symmetric(0, 12),
			CornerRadius:   8,
			Background:     bg,
			BorderWidth:    1,
			BorderColor:    border,
			FocusVisible:   specFocusRingStyle,
		},
		OnPress: func(core.PressEvent) {
			onPress()
		},
		Children: []core.Node{
			core.Text{
				ContentFunc: textFunc,
				Style: core.Style{
					FontSize:   12,
					FontWeight: weight,
					Color:      fg,
				},
			},
		},
	}
}

func profilerKPICard(testID, title string, valueFunc, subFunc func() string, valueColor func() core.Color, active func() bool, onPress func()) core.Node {
	return core.View{
		TestID: testID,
		Role:   core.RoleButton,
		Label:  "Inspect " + title,
		StyleFunc: func() core.Style {
			border := specLineRaised
			if active() {
				border = specAccent
			}
			return core.Style{
				FlexGrow:     1,
				FlexBasis:    core.Pt(0),
				Background:   specRaised,
				CornerRadius: 8,
				BorderWidth:  1,
				BorderColor:  border,
				Padding:      core.Symmetric(8, 12),
				Gap:          4,
				FocusVisible: specFocusRingStyle,
			}
		},
		OnPress: func(core.PressEvent) {
			onPress()
		},
		Children: []core.Node{
			core.Text{
				Content: title,
				Style: core.Style{
					FontSize:   11,
					FontWeight: core.FontWeightSemibold,
					Color:      specTextMuted,
				},
			},
			core.Text{
				ContentFunc: valueFunc,
				StyleFunc: func() core.Style {
					return core.Style{
						FontFamily:  core.FontMonospace,
						FontSize:    14,
						FontWeight:  core.FontWeightSemibold,
						FontVariant: core.FontTabularNums,
						Color:       valueColor(),
					}
				},
			},
			core.Text{
				ContentFunc: subFunc,
				Style: core.Style{
					FontFamily:  core.FontMonospace,
					FontSize:    11,
					FontVariant: core.FontTabularNums,
					Color:       specTextSecondary,
				},
			},
		},
	}
}

func profilerAvgMs(s ProfilerSnapshot) float64 {
	if s.Readout.FrameMs > 0 {
		return s.Readout.FrameMs
	}
	if len(s.Frames) == 0 {
		return 0
	}
	var sum float64
	for _, f := range s.Frames {
		sum += f.FrameMs
	}
	return sum / float64(len(s.Frames))
}

func profilerP99Ms(s ProfilerSnapshot) float64 {
	if s.Readout.P99FrameMs > 0 {
		return s.Readout.P99FrameMs
	}
	if len(s.Frames) == 0 {
		return 0
	}
	vals := make([]float64, len(s.Frames))
	for i, f := range s.Frames {
		vals[i] = f.FrameMs
	}
	slices.Sort(vals)
	idx := (99*(len(vals)-1) + 99) / 100
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

// drawProfilerStripChart renders the PlayMode.dc.html 60 s frame time bar strip with 8.3 ms & 16.7 ms lines and a right-edge playhead bar.
func drawProfilerStripChart(d *core.Drawing, size core.Size, s ProfilerSnapshot) {
	if d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	w := float32(size.Width)
	h := float32(size.Height)
	barAreaH := h - 16
	if barAreaH < 24 {
		barAreaH = h
	}

	// Scale matches PlayMode.dc.html: baseline 20 ms fills barAreaH, expanding only if a spike exceeds 20 ms.
	maxMs := 20.0
	visibleStart := 0
	if len(s.Frames) > profilerStripBars {
		visibleStart = len(s.Frames) - profilerStripBars
	}
	visible := s.Frames[visibleStart:]
	for _, f := range visible {
		if f.FrameMs > maxMs {
			maxMs = f.FrameMs * 1.1
		}
	}

	d.Save()
	d.SetFillColor(specPanel).FillRect(0, 0, w, h)

	yForMs := func(ms float64) float32 {
		ratio := float32(ms / maxMs)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		return h - ratio*barAreaH
	}

	// 8.3 ms warning line (dashed) and 16.7 ms missed-frame line (solid), matching Spec tokens.
	y8 := yForMs(profilerOverThresholdMs)
	d.Save()
	d.SetStrokeColor(specLineRaised).
		SetLineWidth(1).
		SetLineDash(3, 3).
		BeginPath().
		MoveTo(0, y8).
		LineTo(w, y8).
		Stroke()
	d.Restore()

	y16 := yForMs(profilerMissedThresholdMs)
	d.Save()
	d.SetStrokeColor(specLineRaised).
		SetLineWidth(1).
		BeginPath().
		MoveTo(0, y16).
		LineTo(w, y16).
		Stroke()
	d.Restore()

	labelFont := core.Font{Family: core.FontMonospace, Size: 10.5}
	d.SetFillColor(specTextMuted).FillText("16.7 ms", 0, max(float32(11), y16-3), labelFont)
	if y8-y16 >= 14 {
		d.SetFillColor(specTextMuted).FillText("8.3 ms", 0, y8-3, labelFont)
	}

	// Draw up to 150 bars right-aligned to the "now" cursor.
	slotW := (w - 4) / float32(profilerStripBars)
	gap := float32(1)
	if slotW <= 2 {
		gap = 0
	}
	barW := max(float32(1), slotW-gap)
	startSlot := profilerStripBars - len(visible)

	for i, f := range visible {
		x := float32(startSlot+i) * slotW
		bh := float32((f.FrameMs / maxMs) * float64(barAreaH))
		if bh < 2 {
			bh = 2
		}
		if bh > barAreaH {
			bh = barAreaH
		}
		col := specBarNormal
		if f.FrameMs > profilerMissedThresholdMs {
			col = specProblem
		} else if f.FrameMs > profilerOverThresholdMs {
			col = specWarning
		}
		d.SetFillColor(col).BeginPath().RoundRect(x, h-bh, barW, bh, 1).Fill()
	}

	// Right-edge 2 px "now" playhead bar in text strong (#FFFFFF).
	d.SetFillColor(specTextStrong).BeginPath().RoundRect(max(float32(0), w-2), 0, 2, h, 1).Fill()
	d.Restore()
}

// drawProfilerMainChart renders the selected time-series chart in the full Profiler panel using only Spec tokens.
func drawProfilerMainChart(d *core.Drawing, size core.Size, s ProfilerSnapshot) {
	if d == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	w := float32(size.Width)
	h := float32(size.Height)

	d.Save()
	d.SetFillColor(specBg).FillRect(0, 0, w, h)

	if len(s.Frames) == 0 {
		msg := "Waiting for frame samples…"
		if !s.Connected {
			msg = "Disconnected · connect a running game or press Play to chart live metrics"
		}
		d.SetFillColor(specTextMuted).FillText(msg, 12, h/2+4, core.Font{Size: 12})
		d.Restore()
		return
	}

	visibleStart := 0
	if len(s.Frames) > profilerStripBars {
		visibleStart = len(s.Frames) - profilerStripBars
	}
	frames := s.Frames[visibleStart:]

	switch s.Metric {
	case ProfilerMetricFrame:
		drawFrameBreakdownChart(d, w, h, frames)
	case ProfilerMetricFPS:
		drawFPSChart(d, w, h, frames)
	case ProfilerMetricAllocsGC:
		drawAllocsGCChart(d, w, h, frames)
	case ProfilerMetricMemory:
		drawMemoryChart(d, w, h, frames, s.Readout.AssetMemory)
	}
	d.Restore()
}

func drawFrameBreakdownChart(d *core.Drawing, w, h float32, frames []ProfilerFrame) {
	topPad := float32(18)
	plotH := max(float32(16), h-topPad)
	maxMs := 20.0
	for _, f := range frames {
		if f.FrameMs > maxMs {
			maxMs = f.FrameMs * 1.1
		}
	}

	yFor := func(ms float64) float32 {
		r := float32(ms / maxMs)
		if r > 1 {
			r = 1
		}
		return h - r*plotH
	}

	// Reference lines at 8.3 ms and 16.7 ms.
	for _, th := range []struct {
		ms    float64
		label string
		dash  bool
	}{
		{profilerOverThresholdMs, "8.3 ms", true},
		{profilerMissedThresholdMs, "16.7 ms", false},
	} {
		y := yFor(th.ms)
		d.Save()
		d.SetStrokeColor(specLineRaised).SetLineWidth(1)
		if th.dash {
			d.SetLineDash(3, 3)
		}
		d.BeginPath().MoveTo(0, y).LineTo(w, y).Stroke()
		d.Restore()
		d.SetFillColor(specTextMuted).FillText(th.label, 4, max(topPad, y-3), core.Font{Family: core.FontMonospace, Size: 10})
	}

	slotW := (w - 4) / float32(profilerStripBars)
	gap := float32(1)
	if slotW <= 2 {
		gap = 0
	}
	barW := max(float32(1), slotW-gap)
	startSlot := profilerStripBars - len(frames)

	for i, f := range frames {
		x := float32(startSlot+i) * slotW
		totH := float32((max(0.0, f.FrameMs) / maxMs) * float64(plotH))
		updH := float32((max(0.0, f.UpdateMs) / maxMs) * float64(plotH))
		drwH := float32((max(0.0, f.DrawMs) / maxMs) * float64(plotH))
		if totH < 2 && f.FrameMs > 0 {
			totH = 2
		}
		if totH > plotH {
			totH = plotH
		}
		if updH > plotH {
			updH = plotH
		}
		if updH+drwH > plotH {
			drwH = max(float32(0), plotH-updH)
		}

		// Remaining frame time (idle/overhead), colored warning/problem on spikes.
		bgCol := specBarNormal
		if f.FrameMs > profilerMissedThresholdMs {
			bgCol = specProblem
		} else if f.FrameMs > profilerOverThresholdMs {
			bgCol = specWarning
		}
		d.SetFillColor(bgCol).FillRect(x, h-totH, barW, totH)
		if updH > 0 {
			d.SetFillColor(specAccent).FillRect(x, h-updH, barW, updH)
		}
		if drwH > 0 {
			d.SetFillColor(specChanged).FillRect(x, h-updH-drwH, barW, drwH)
		}
		if f.GCHitch {
			d.SetFillColor(specAgent).FillRect(x, topPad, barW, 3)
		}
	}

	// Compact inline legend in top-right corner using Spec tokens.
	font := core.Font{Family: core.FontMonospace, Size: 10}
	d.SetFillColor(specAccent).FillRect(w-180, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("Update", w-168, 12, font)
	d.SetFillColor(specChanged).FillRect(w-116, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("Draw", w-104, 12, font)
	d.SetFillColor(specAgent).FillRect(w-64, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("GC", w-52, 12, font)
}

func drawFPSChart(d *core.Drawing, w, h float32, frames []ProfilerFrame) {
	topPad := float32(18)
	plotH := max(float32(16), h-topPad)
	maxFPS := 75.0
	for _, f := range frames {
		if f.FPS > maxFPS {
			maxFPS = math.Ceil(f.FPS/30) * 30
		}
	}
	yFor := func(fps float64) float32 {
		r := float32(fps / maxFPS)
		if r > 1 {
			r = 1
		}
		if r < 0 {
			r = 0
		}
		return h - r*plotH
	}

	y60 := yFor(60)
	d.Save()
	d.SetStrokeColor(specLineRaised).SetLineWidth(1).BeginPath().MoveTo(0, y60).LineTo(w, y60).Stroke()
	d.Restore()
	d.SetFillColor(specTextMuted).FillText("60 fps", 4, max(topPad, y60-3), core.Font{Family: core.FontMonospace, Size: 10})

	y30 := yFor(30)
	d.Save()
	d.SetStrokeColor(specWarningEdge).SetLineWidth(1).SetLineDash(3, 3).BeginPath().MoveTo(0, y30).LineTo(w, y30).Stroke()
	d.Restore()
	d.SetFillColor(specTextMuted).FillText("30 fps", 4, max(topPad, y30-3), core.Font{Family: core.FontMonospace, Size: 10})

	slotW := (w - 4) / float32(profilerStripBars)
	startSlot := profilerStripBars - len(frames)

	d.Save()
	d.SetStrokeColor(specAdded).SetLineWidth(2).BeginPath()
	for i, f := range frames {
		x := float32(startSlot+i)*slotW + slotW/2
		y := yFor(f.FPS)
		if i == 0 {
			d.MoveTo(x, y)
		} else {
			d.LineTo(x, y)
		}
	}
	d.Stroke()
	d.Restore()
}

func drawAllocsGCChart(d *core.Drawing, w, h float32, frames []ProfilerFrame) {
	topPad := float32(18)
	plotH := max(float32(16), h-topPad)
	maxAllocs := 10.0
	maxHeap := uint64(1 << 20)
	for _, f := range frames {
		if f.AllocsPerFrame > maxAllocs {
			maxAllocs = f.AllocsPerFrame * 1.15
		}
		if f.HeapBytes > maxHeap {
			maxHeap = f.HeapBytes
		}
	}

	slotW := (w - 4) / float32(profilerStripBars)
	barW := max(float32(1), slotW-1)
	startSlot := profilerStripBars - len(frames)

	for i, f := range frames {
		x := float32(startSlot+i) * slotW
		bh := float32((f.AllocsPerFrame / maxAllocs) * float64(plotH))
		if bh > 0 {
			if bh < 2 {
				bh = 2
			}
			d.SetFillColor(specAgent).FillRect(x, h-bh, barW, bh)
		}
		if f.GCHitch {
			d.SetFillColor(specProblem).FillRect(x, topPad, max(float32(2), barW), plotH)
		}
	}

	// Overlay live heap curve in accent (#3B7BFF).
	d.Save()
	d.SetStrokeColor(specAccent).SetLineWidth(1.5).BeginPath()
	for i, f := range frames {
		x := float32(startSlot+i)*slotW + slotW/2
		y := h - float32(float64(f.HeapBytes)/float64(maxHeap))*plotH
		if i == 0 {
			d.MoveTo(x, y)
		} else {
			d.LineTo(x, y)
		}
	}
	d.Stroke()
	d.Restore()

	font := core.Font{Family: core.FontMonospace, Size: 10}
	d.SetFillColor(specTextMuted).FillText(fmt.Sprintf("max %.0f allocs/f · heap %s", maxAllocs, formatProfilerUintBytes(maxHeap)), 4, 12, font)
	d.SetFillColor(specAgent).FillRect(w-176, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("Allocs", w-164, 12, font)
	d.SetFillColor(specAccent).FillRect(w-112, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("Heap", w-100, 12, font)
	d.SetFillColor(specProblem).FillRect(w-56, 4, 8, 8)
	d.SetFillColor(specTextSecondary).FillText("Hitch", w-44, 12, font)
}

func drawMemoryChart(d *core.Drawing, w, h float32, frames []ProfilerFrame, summary *mcp.PerfMemorySummary) {
	topPad := float32(18)
	plotH := max(float32(16), h-topPad)
	var maxBytes int64 = 1 << 20
	var budget int64
	for _, f := range frames {
		if f.AssetMemoryBytes > maxBytes {
			maxBytes = f.AssetMemoryBytes
		}
		if f.AssetMemoryBudget > budget {
			budget = f.AssetMemoryBudget
		}
	}
	if budget > maxBytes {
		maxBytes = budget
	}
	const maxSafeMemoryScale = math.MaxInt64 / 11 * 10
	if maxBytes < maxSafeMemoryScale {
		maxBytes += maxBytes / 10
	}

	yFor := func(b int64) float32 {
		r := float32(float64(b) / float64(maxBytes))
		if r > 1 {
			r = 1
		}
		return h - r*plotH
	}

	if budget > 0 {
		yb := yFor(budget)
		d.Save()
		d.SetStrokeColor(specProblemEdge).SetLineWidth(1).SetLineDash(4, 3).BeginPath().MoveTo(0, yb).LineTo(w, yb).Stroke()
		d.Restore()
		d.SetFillColor(specProblem).FillText("budget "+formatProfilerBytes(budget), 4, max(topPad, yb-3), core.Font{Family: core.FontMonospace, Size: 10})
	}

	slotW := (w - 4) / float32(profilerStripBars)
	startSlot := profilerStripBars - len(frames)

	lineColor := specChanged
	if summary != nil && summary.OverBudget {
		lineColor = specProblem
	}
	d.Save()
	d.SetStrokeColor(lineColor).SetLineWidth(2).BeginPath()
	for i, f := range frames {
		x := float32(startSlot+i)*slotW + slotW/2
		y := yFor(f.AssetMemoryBytes)
		if i == 0 {
			d.MoveTo(x, y)
		} else {
			d.LineTo(x, y)
		}
	}
	d.Stroke()
	d.Restore()

	font := core.Font{Family: core.FontMonospace, Size: 10}
	if summary != nil && len(summary.Groups) > 0 {
		x := float32(4)
		for i, g := range summary.Groups {
			if i >= 4 {
				break
			}
			col := specTextSecondary
			if g.OverBudget {
				col = specProblem
			}
			txt := fmt.Sprintf("%s %s", g.Group, formatProfilerBytes(g.TotalBytes))
			d.SetFillColor(col).FillText(txt, x, 12, font)
			x += float32(len(txt)*6 + 16)
		}
	} else {
		d.SetFillColor(specTextMuted).FillText("Asset memory over time", 4, 12, font)
	}
}

func profilerSnapshotsEqual(a, b ProfilerSnapshot) bool {
	if a.Connected != b.Connected ||
		a.InEditorPlay != b.InEditorPlay ||
		a.SourceLabel != b.SourceLabel ||
		a.Open != b.Open ||
		a.Metric != b.Metric ||
		a.HasReadout != b.HasReadout ||
		a.Recording != b.Recording ||
		a.RecordedFrames != b.RecordedFrames ||
		a.LastError != b.LastError ||
		len(a.Frames) != len(b.Frames) ||
		!slices.Equal(a.Zones, b.Zones) {
		return false
	}
	if (a.Spike == nil) != (b.Spike == nil) {
		return false
	}
	if a.Spike != nil && *a.Spike != *b.Spike {
		return false
	}
	if a.Readout.SampleCount != b.Readout.SampleCount ||
		a.Readout.FrameMs != b.Readout.FrameMs ||
		a.Readout.P99FrameMs != b.Readout.P99FrameMs ||
		a.Readout.FPS != b.Readout.FPS ||
		a.Readout.AllocsPerFrame != b.Readout.AllocsPerFrame ||
		a.Readout.HeapBytes != b.Readout.HeapBytes ||
		a.Readout.GCCount != b.Readout.GCCount ||
		a.Readout.DrawCalls != b.Readout.DrawCalls {
		return false
	}
	if len(a.Frames) > 0 {
		la, lb := a.Frames[len(a.Frames)-1], b.Frames[len(b.Frames)-1]
		if la.FrameMs != lb.FrameMs || la.UpdateMs != lb.UpdateMs || la.DrawMs != lb.DrawMs || la.Cause != lb.Cause || la.GCHitch != lb.GCHitch {
			return false
		}
	}
	return true
}

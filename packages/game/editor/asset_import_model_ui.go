package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const maxModelPreviewListItems = 64

// modelImportPreviewSection builds the interactive 3D orbit preview, animation
// playback/scrubber controls, bounds & triangle stats, and collapsible/scannable
// lists of nodes, meshes, materials, and animations for a selected glTF/GLB file.
func modelImportPreviewSection(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	relPath string,
	disabled func() bool,
) []core.Node {
	colors := ui.ColorsOf(cx)

	type modelMetaKey struct {
		projectPath string
		filesRev    uint64
		assetsRev   uint64
		importRev   uint64
		loadRev     uint64
	}
	metaKey := core.NewMemo(cx, func() modelMetaKey {
		_ = rev.Get()
		return modelMetaKey{
			projectPath: ed.ProjectPath,
			filesRev:    ed.filesRev,
			assetsRev:   ed.assetsRev,
			importRev:   ed.importRev,
			loadRev:     ed.modelPreview.loadRev,
		}
	})

	type modelSnapshot struct {
		loadRev uint64
		scale   float64
		info    ModelPreviewInfo
		err     error
	}
	modelMemo := core.NewMemoFunc(cx, func() modelSnapshot {
		_ = metaKey.Get()
		info, err := ed.InspectModelImport(relPath)
		return modelSnapshot{
			loadRev: ed.modelPreview.loadRev,
			scale:   info.Scale,
			info:    info,
			err:     err,
		}
	}, func(a, b modelSnapshot) bool {
		return a.loadRev == b.loadRev && a.scale == b.scale && sameError(a.err, b.err)
	})
	readModel := func() modelSnapshot {
		return modelMemo.Get()
	}

	// Only remount structural lists when the loaded model changes (not on animation ticks or orbit drags).
	structureKey := core.NewMemo(cx, func() string {
		s := readModel()
		if s.err != nil {
			return "err:" + s.err.Error()
		}
		return fmt.Sprintf("ok:%d:%d:%d:%d:%d", s.loadRev, len(s.info.Nodes), len(s.info.Meshes), len(s.info.Materials), len(s.info.Animations))
	})

	var previewH float32 = modelPreviewDefaultH
	var lastPan core.Point
	var mouseDrag bool
	var mouseLast core.Point
	var middlePan bool
	var pinchLast float32

	previewCanvas := core.View{
		TestID: "import-model-preview",
		Style: core.Style{
			Width:        core.Pct(100),
			Height:       core.Pt(190),
			CornerRadius: 4,
			BorderWidth:  1,
			BorderColor:  colors.Border(),
			Overflow:     core.Hidden,
		},
		OnLayout: func(b core.Bounds) {
			if b.Size.Height > 0 {
				previewH = float32(b.Size.Height)
			}
		},
		OnPan: func(ev core.PanEvent) {
			switch ev.Phase {
			case core.GestureBegan:
				lastPan = ev.Translation
				mouseDrag = false
			case core.GestureChanged:
				dx := float32(ev.Translation.X - lastPan.X)
				dy := float32(ev.Translation.Y - lastPan.Y)
				lastPan = ev.Translation
				if middlePan {
					ed.PanModelPreview(dx, dy, previewH)
				} else {
					ed.OrbitModelPreview(-dx*0.012, dy*0.012)
				}
			case core.GestureEnded, core.GestureCancelled:
				lastPan = core.Point{}
				mouseDrag = false
			}
		},
		OnMouseDown: func(ev core.MouseButtonEvent) {
			switch ev.Button {
			case core.MouseButtonMiddle:
				middlePan = true
				mouseDrag = true
				mouseLast = ev.Position
			case core.MouseButtonLeft:
				middlePan = ev.Modifiers.Shift
				mouseDrag = true
				mouseLast = ev.Position
			}
		},
		OnMouseMove: func(ev core.MouseEvent) {
			if !mouseDrag {
				return
			}
			if ev.PressedButton == core.MouseButtonNone {
				mouseDrag = false
				middlePan = false
				return
			}
			dx := float32(ev.Position.X - mouseLast.X)
			dy := float32(ev.Position.Y - mouseLast.Y)
			mouseLast = ev.Position
			if middlePan || ev.PressedButton == core.MouseButtonMiddle || ev.Modifiers.Shift {
				ed.PanModelPreview(dx, dy, previewH)
			} else {
				ed.OrbitModelPreview(-dx*0.012, dy*0.012)
			}
		},
		OnMouseUp: func(ev core.MouseButtonEvent) {
			if ev.Button == core.MouseButtonLeft || ev.Button == core.MouseButtonMiddle {
				mouseDrag = false
				middlePan = false
			}
		},
		OnWheel: func(ev core.WheelEvent) {
			if ev.Delta.Y == 0 {
				return
			}
			factor := float32(math.Pow(1.0015, float64(-ev.Delta.Y)))
			if factor > 0 {
				ed.ZoomModelPreview(factor)
			}
		},
		OnPinch: func(ev core.GestureEvent) {
			switch ev.Phase {
			case core.GestureBegan:
				pinchLast = ev.Value
			case core.GestureChanged, core.GestureEnded:
				if pinchLast > 0 && ev.Value > 0 {
					ed.ZoomModelPreview(ev.Value / pinchLast)
				}
				pinchLast = ev.Value
				if ev.Phase == core.GestureEnded {
					pinchLast = 0
				}
			default:
				pinchLast = 0
			}
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "import-model-preview-canvas",
				Label:  "3D Model Preview",
				Style: core.Style{
					Width:  core.Pct(100),
					Height: core.Pct(100),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					if size.Height > 0 {
						previewH = float32(size.Height)
					}
					ed.DrawModelPreview(d, size, relPath)
				},
			},
		},
	}

	previewControls := ui.HStack{
		Gap: 6,
		Children: []core.Node{
			core.Text{
				TestID: "import-model-preview-info",
				ContentFunc: func() string {
					s := readModel()
					if s.err != nil {
						return "Preview unavailable"
					}
					return fmt.Sprintf("%d tris · %d verts · drag to orbit", s.info.TotalTriangles, s.info.TotalVertices)
				},
				Style: core.Style{FontSize: 10, Color: colors.Muted(), FlexGrow: 1},
			},
			ui.Button{
				TestID:  "import-model-reset-view",
				Text:    "Reset View",
				Label:   "Re-frame the model preview camera",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonSecondary,
				OnPress: func() {
					ed.ResetModelPreviewCamera(relPath)
				},
			},
		},
	}

	previewError := core.Show{
		When: func() bool {
			return readModel().err != nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID: "import-model-preview-error",
				ContentFunc: func() string {
					if err := readModel().err; err != nil {
						return err.Error()
					}
					return ""
				},
				Style: core.Style{FontSize: 11, Color: colors.Danger()},
			}
		},
	}

	boundsInfo := core.Show{
		When: func() bool {
			return readModel().err == nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				Style: core.Style{Gap: 2},
				Children: []core.Node{
					core.Text{
						TestID: "import-model-bounds",
						ContentFunc: func() string {
							s := readModel()
							if s.info.Bounds.Empty() {
								return "Bounds: empty"
							}
							sz := s.info.Bounds.Size()
							base := fmt.Sprintf("Bounds: %.2g × %.2g × %.2g", sz.X, sz.Y, sz.Z)
							if s.info.Scale > 0 && math.Abs(s.info.Scale-1) > 1e-6 {
								ssz := s.info.ScaledBounds.Size()
								return fmt.Sprintf("%s (scaled %.2g × %.2g × %.2g)", base, ssz.X, ssz.Y, ssz.Z)
							}
							return base
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
					core.Text{
						TestID: "import-model-counts",
						ContentFunc: func() string {
							s := readModel()
							return fmt.Sprintf("%d nodes · %d meshes · %d materials · %d animations",
								len(s.info.Nodes), len(s.info.Meshes), len(s.info.Materials), len(s.info.Animations))
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
				},
			}
		},
	}

	animPlaybackSection := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = structureKey.Get()
		snap := core.Untracked(cx, readModel)
		if snap.err != nil || len(snap.info.Animations) == 0 {
			return nil
		}

		opts := make([]ui.Option[int], len(snap.info.Animations))
		for i, a := range snap.info.Animations {
			opts[i] = ui.Option[int]{
				Value: a.Index,
				Label: fmt.Sprintf("%s (%.2fs)", a.DisplayName(), a.Duration.Seconds()),
			}
		}

		return core.View{
			TestID: "import-model-anim-controls",
			Style: core.Style{
				Gap:          6,
				Padding:      core.All(6),
				CornerRadius: 4,
				Background:   colors.Inset(),
				BorderWidth:  1,
				BorderColor:  colors.Line(),
			},
			Children: []core.Node{
				core.Text{
					Content: "ANIMATION PREVIEW",
					Style: core.Style{
						FontSize:   10,
						FontWeight: 700,
						Color:      colors.Muted(),
					},
				},
				ui.Select[int]{
					TestID:   "import-model-anim-select",
					Label:    "Preview Animation",
					Options:  opts,
					Disabled: disabled,
					Style:    core.Style{Width: core.Pct(100)},
					Value: func() int {
						_ = rev.Get()
						idx := ed.ModelPreviewAnimation()
						if idx < 0 {
							return 0
						}
						return idx
					},
					OnChange: func(idx int) {
						ed.report(ed.SelectModelPreviewAnimation(relPath, idx))
					},
				},
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						ui.Button{
							TestID: "import-model-anim-play",
							TextFunc: func() string {
								_ = rev.Get()
								if ed.ModelPreviewPlaying() {
									return "Pause"
								}
								return "Play"
							},
							Label:    "Play or pause the selected model animation",
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonPrimary,
							Disabled: disabled,
							OnPress: func() {
								if ed.ModelPreviewPlaying() {
									ed.PauseModelPreviewAnimation()
								} else {
									ed.report(ed.PlayModelPreviewAnimation(relPath, ed.ModelPreviewAnimation()))
								}
							},
						},
						ui.Button{
							TestID:   "import-model-anim-stop",
							Text:     "Stop",
							Label:    "Stop model animation and restore bind pose",
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonSecondary,
							Disabled: disabled,
							OnPress: func() {
								ed.StopModelPreviewAnimation()
							},
						},
						ui.ToggleButton{
							TestID:   "import-model-anim-loop",
							Text:     "Loop",
							Label:    "Loop model animation playback",
							Disabled: disabled,
							On: func() bool {
								_ = rev.Get()
								return ed.ModelPreviewLoop()
							},
							OnChange: func(on bool) {
								ed.SetModelPreviewLoop(on)
							},
						},
						core.Text{
							TestID: "import-model-anim-time",
							ContentFunc: func() string {
								_ = rev.Get()
								return fmt.Sprintf("%.2fs / %.2fs", ed.ModelPreviewTime().Seconds(), ed.ModelPreviewDuration().Seconds())
							},
							Style: core.Style{
								FontSize: 10,
								Color:    colors.Muted(),
								FlexGrow: 1,
							},
						},
					},
				},
				ui.Slider{
					TestID: "import-model-anim-scrub",
					Label:  "Animation Time",
					Disabled: func() bool {
						_ = rev.Get()
						return disabled() || ed.ModelPreviewDuration() <= 0
					},
					MinFunc: func() float64 { return 0 },
					MaxFunc: func() float64 {
						_ = rev.Get()
						d := ed.ModelPreviewDuration().Seconds()
						if d <= 0 {
							return 1
						}
						return d
					},
					StepFunc: func() float64 { return 1.0 / 60.0 },
					Value: func() float64 {
						_ = rev.Get()
						return ed.ModelPreviewTime().Seconds()
					},
					ValueText: func(v float64) string {
						return strconv.FormatFloat(v, 'f', 2, 64) + " seconds"
					},
					OnChange: func(v float64) {
						t := secondsToDurationClamped(v)
						ed.report(ed.SeekModelPreviewAnimation(relPath, t))
					},
				},
			},
		}
	})

	contentsSection := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = structureKey.Get()
		snap := core.Untracked(cx, readModel)
		if snap.err != nil {
			return nil
		}
		info := snap.info
		var sections []core.Node

		// Animations list (with per-clip Play/Pause button)
		if len(info.Animations) > 0 {
			rows := make([]core.Node, 0, min(len(info.Animations), maxModelPreviewListItems)+1)
			for i, a := range info.Animations {
				if i >= maxModelPreviewListItems {
					rows = append(rows, mutedOverflowText(colors, len(info.Animations)-maxModelPreviewListItems))
					break
				}
				anim := a
				rows = append(rows, core.View{
					TestID: fmt.Sprintf("import-model-anim-row-%d", anim.Index),
					Style: core.Style{
						Direction:  core.Row,
						AlignItems: core.AlignCenter,
						Gap:        6,
						Padding:    core.Symmetric(2, 4),
					},
					Children: []core.Node{
						core.Text{
							Content: fmt.Sprintf("%s · %.2fs · %d ch", anim.DisplayName(), anim.Duration.Seconds(), anim.Channels),
							Style:   core.Style{FontSize: 11, Color: colors.Text(), FlexGrow: 1},
						},
						ui.Button{
							TestID: fmt.Sprintf("import-model-anim-play-%d", anim.Index),
							TextFunc: func() string {
								_ = rev.Get()
								if ed.ModelPreviewPlaying() && ed.ModelPreviewAnimation() == anim.Index {
									return "Pause"
								}
								return "Play"
							},
							Label:    "Play animation " + anim.DisplayName(),
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonSecondary,
							Disabled: disabled,
							OnPress: func() {
								if ed.ModelPreviewPlaying() && ed.ModelPreviewAnimation() == anim.Index {
									ed.PauseModelPreviewAnimation()
								} else {
									ed.report(ed.PlayModelPreviewAnimation(relPath, anim.Index))
								}
							},
						},
					},
				})
			}
			sections = append(sections, modelDetailGroup(colors, "import-model-animations-list", fmt.Sprintf("ANIMATIONS (%d)", len(info.Animations)), rows))
		}

		// Nodes list
		if len(info.Nodes) > 0 {
			rows := make([]core.Node, 0, min(len(info.Nodes), maxModelPreviewListItems)+1)
			for i, n := range info.Nodes {
				if i >= maxModelPreviewListItems {
					rows = append(rows, mutedOverflowText(colors, len(info.Nodes)-maxModelPreviewListItems))
					break
				}
				var detail []string
				if n.Mesh >= 0 {
					detail = append(detail, n.MeshName)
					detail = append(detail, fmt.Sprintf("%d tris", n.Triangles))
				}
				if n.Skin >= 0 {
					detail = append(detail, fmt.Sprintf("skin %d", n.Skin))
				}
				label := n.DisplayName()
				if len(detail) > 0 {
					label += " (" + strings.Join(detail, ", ") + ")"
				}
				indent := min(n.Depth, 6) * 8
				rows = append(rows, core.Text{
					TestID:  fmt.Sprintf("import-model-node-row-%d", n.Index),
					Content: label,
					Style: core.Style{
						FontSize: 11,
						Color:    colors.Text(),
						Margin:   core.Edges{Left: core.Points(indent)},
					},
				})
			}
			sections = append(sections, modelDetailGroup(colors, "import-model-nodes-list", fmt.Sprintf("NODES (%d)", len(info.Nodes)), rows))
		}

		// Meshes list
		if len(info.Meshes) > 0 {
			rows := make([]core.Node, 0, min(len(info.Meshes), maxModelPreviewListItems)+1)
			for i, m := range info.Meshes {
				if i >= maxModelPreviewListItems {
					rows = append(rows, mutedOverflowText(colors, len(info.Meshes)-maxModelPreviewListItems))
					break
				}
				text := fmt.Sprintf("%s · %d tris · %s", m.DisplayName(), m.Triangles, formatBoundsExtent(m.Bounds))
				if m.MorphTargets > 0 {
					text += fmt.Sprintf(" · %d morphs", m.MorphTargets)
				}
				rows = append(rows, core.Text{
					TestID:  fmt.Sprintf("import-model-mesh-row-%d", m.Index),
					Content: text,
					Style:   core.Style{FontSize: 11, Color: colors.Text()},
				})
			}
			sections = append(sections, modelDetailGroup(colors, "import-model-meshes-list", fmt.Sprintf("MESHES (%d)", len(info.Meshes)), rows))
		}

		// Materials list
		if len(info.Materials) > 0 {
			rows := make([]core.Node, 0, min(len(info.Materials), maxModelPreviewListItems)+1)
			for i, mat := range info.Materials {
				if i >= maxModelPreviewListItems {
					rows = append(rows, mutedOverflowText(colors, len(info.Materials)-maxModelPreviewListItems))
					break
				}
				var tags []string
				tags = append(tags, string(mat.AlphaMode))
				tags = append(tags, fmt.Sprintf("M:%.2g R:%.2g", mat.Metallic, mat.Roughness))
				if mat.HasBaseColorTexture {
					tags = append(tags, "tex")
				}
				if mat.DoubleSided {
					tags = append(tags, "2-sided")
				}
				if mat.Unlit {
					tags = append(tags, "unlit")
				}
				rows = append(rows, core.Text{
					TestID:  fmt.Sprintf("import-model-material-row-%d", mat.Index),
					Content: fmt.Sprintf("%s · %s", mat.DisplayName(), strings.Join(tags, " · ")),
					Style:   core.Style{FontSize: 11, Color: colors.Text()},
				})
			}
			sections = append(sections, modelDetailGroup(colors, "import-model-materials-list", fmt.Sprintf("MATERIALS (%d)", len(info.Materials)), rows))
		}

		if len(sections) == 0 {
			return nil
		}
		return core.View{
			TestID:   "import-model-contents",
			Style:    core.Style{Gap: 6},
			Children: sections,
		}
	})

	return []core.Node{
		core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				previewCanvas,
				previewControls,
				previewError,
				boundsInfo,
			},
		},
		animPlaybackSection,
		contentsSection,
	}
}

func modelDetailGroup(colors ui.Colors, testID, title string, rows []core.Node) core.Node {
	children := make([]core.Node, 0, len(rows)+1)
	children = append(children, core.Text{
		Content: title,
		Style: core.Style{
			FontSize:   10,
			FontWeight: 700,
			Color:      colors.Muted(),
		},
	})
	children = append(children, rows...)
	return core.View{
		TestID: testID,
		Style: core.Style{
			Gap:          2,
			Padding:      core.All(6),
			CornerRadius: 4,
			Background:   colors.Inset(),
			BorderWidth:  1,
			BorderColor:  colors.Line(),
		},
		Children: children,
	}
}

func mutedOverflowText(colors ui.Colors, remaining int) core.Node {
	return core.Text{
		Content: fmt.Sprintf("+%d more", remaining),
		Style:   core.Style{FontSize: 10, Color: colors.Muted()},
	}
}

func formatBoundsExtent(b math3d.AABB) string {
	if b.Empty() {
		return "empty"
	}
	sz := b.Size()
	return fmt.Sprintf("%.2g×%.2g×%.2g", sz.X, sz.Y, sz.Z)
}

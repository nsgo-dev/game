package editor

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// sceneInspectorView renders the Scene Settings inspector when no object is selected:
// scene name, virtual width and height, background color (NSGo color picker and hex input),
// and the default 2D camera (position, zoom, rotation and optional clamp bounds).
func sceneInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}

	sectionHeader := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		}
	}

	fieldRow := func(label string, control core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style: core.Style{
						Width:    core.Pt(76),
						FontSize: 12,
						Color:    colors.Text(),
					},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{control},
				},
			},
		}
	}

	// Background color state: ColorWell live-previews during drag (tracked on Editor so Play,
	// Save, Undo/Redo, Revert, and OpenYAML cancel any uncommitted preview) and commits one
	// undoable step on OnChangeEnd (or on popover close if closed mid-drag). The adjacent Input
	// lets authors type CSS color / hex values directly and reports invalid colors in Status.
	bgPickerOpen := core.NewSignal(cx, false)
	cx.Effect(func() {
		if disabled() {
			cx.Untrack(func() {
				bgPickerOpen.Set(false)
			})
		}
	})
	cx.Cleanup(func() {
		ed.cancelBackgroundPreview()
	})

	bgDraft := core.NewSignal(cx, "")
	bgDirty := core.NewSignal(cx, false)
	bgSynced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Doc == nil {
			return ""
		}
		return ed.Doc.BackgroundColor
	})
	cx.Effect(func() {
		cur := bgSynced.Get()
		cx.Untrack(func() {
			bgDraft.Set(cur)
			bgDirty.Set(false)
		})
	})

	commitBgText := func(raw string) {
		bgDirty.Set(false)
		trimmed := strings.TrimSpace(raw)
		if err := ed.SetBackground(trimmed); err != nil {
			ed.report(err)
			if ed.Doc != nil {
				bgDraft.Set(ed.Doc.BackgroundColor)
			}
			return
		}
		bgDraft.Set(trimmed)
	}

	bgColorWell := ui.ColorWell{
		TestID: "inspector-scene-bg-picker",
		Label:  "Background Color Picker",
		Value: func() core.Color {
			_ = rev.Get()
			if ed.Doc != nil && ed.Doc.BackgroundColor != "" {
				if c, err := core.ParseColor(ed.Doc.BackgroundColor); err == nil {
					return c
				}
			}
			return core.Transparent
		},
		Open: func() bool {
			return !disabled() && bgPickerOpen.Get()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			if disabled() {
				ed.cancelBackgroundPreview()
				bgPickerOpen.Set(false)
				return
			}
			if !open {
				ed.report(ed.flushBackgroundPreview())
			}
			bgPickerOpen.Set(open)
		},
		Picker: ui.ColorPicker{
			TestID:   "inspector-scene-bg-color-picker",
			Format:   base.ColorFormatHex,
			Disabled: disabled,
			OnChange: func(_ core.Color, formatted string) {
				ed.previewBackground(formatted)
			},
			OnChangeEnd: func(_ core.Color, formatted string) {
				ed.report(ed.finishBackgroundPreview(formatted))
			},
		},
	}

	bgInput := ui.Input{
		TestID:      "inspector-scene-bg",
		Label:       "Background Color",
		Placeholder: "#141824",
		Disabled:    disabled,
		Value: func() string {
			_ = rev.Get()
			if bgDirty.Get() {
				return bgDraft.Get()
			}
			if ed.Doc != nil {
				return ed.Doc.BackgroundColor
			}
			return ""
		},
		OnFocus: func() {
			if !bgDirty.Get() && ed.Doc != nil {
				bgDraft.Set(ed.Doc.BackgroundColor)
			}
		},
		OnChange: func(text string) {
			bgDraft.Set(text)
			bgDirty.Set(true)
		},
		OnSubmit: func(text string) {
			bgDraft.Set(text)
			commitBgText(text)
		},
		OnBlur: func() {
			if bgDirty.Get() {
				commitBgText(bgDraft.Get())
			}
		},
	}

	bgClear := removeButton("inspector-scene-bg-clear", "Clear background color", func() bool {
		if disabled() {
			return true
		}
		return ed.Doc == nil || ed.Doc.BackgroundColor == ""
	}, func() {
		ed.report(ed.SetBackground(""))
	})

	bgControl := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        4,
		},
		Children: []core.Node{
			bgColorWell,
			core.View{
				Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{bgInput},
			},
			bgClear,
		},
	}

	floatFormat := func(v float64) string {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}

	boundsEnabled := func() bool {
		_ = rev.Get()
		return ed.Doc != nil && ed.Doc.Camera != nil && ed.Doc.Camera.Bounds != nil
	}

	updateBounds := func(mutate func(b *sceneformat.BoundsDoc)) {
		if ed.Doc == nil || ed.Doc.Camera == nil || ed.Doc.Camera.Bounds == nil {
			return
		}
		next := *ed.Doc.Camera.Bounds
		mutate(&next)
		ed.report(ed.SetCameraBounds(&next))
	}

	nodes := []core.Node{
		core.View{
			TestID: "inspector-scene-header",
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{
						Width:        core.Pt(24),
						Height:       core.Pt(24),
						CornerRadius: 6,
						Background:   colors.Accent(),
						FlexShrink:   0,
					},
				},
				core.View{
					Style: core.Style{Gap: 2, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{
						core.Text{
							TestID: "inspector-scene-title",
							ContentFunc: func() string {
								_ = rev.Get()
								if ed.Doc != nil && ed.Doc.Name != "" {
									return ed.Doc.Name
								}
								return "Untitled Scene"
							},
							MaxLines: 1,
							Style: core.Style{
								FontSize:   13,
								FontWeight: 700,
								Color:      colors.Text(),
							},
						},
						core.Text{
							TestID: "inspector-scene-path",
							ContentFunc: func() string {
								_ = rev.Get()
								return "Scene · " + ed.SceneLabel()
							},
							MaxLines: 1,
							Style: core.Style{
								FontSize: 11,
								Color:    colors.Muted(),
							},
						},
					},
				},
			},
		},

		sectionHeader("SCENE"),
		fieldRow("Name", ui.Input{
			TestID:   "inspector-scene-name",
			Label:    "Scene Name",
			Disabled: disabled,
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil {
					return ed.Doc.Name
				}
				return ""
			},
			OnChange: func(text string) {
				ed.report(ed.SetSceneName(text))
			},
		}),
		fieldRow("Width", ui.NumberInput{
			TestID:      "inspector-scene-width",
			Label:       "Virtual Width",
			LeadingText: "W",
			Step:        1,
			Format:      floatFormat,
			Disabled:    disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil {
					return float64(ed.Doc.VirtualWidth)
				}
				return 0
			},
			OnChange: func(v float64) {
				if ed.Doc == nil {
					return
				}
				h := ed.Doc.VirtualHeight
				if h == 0 && v > 0 {
					_, h = ed.ViewportSize()
				}
				ed.report(ed.SetVirtualSize(float32(v), h))
			},
		}),
		fieldRow("Height", ui.NumberInput{
			TestID:      "inspector-scene-height",
			Label:       "Virtual Height",
			LeadingText: "H",
			Step:        1,
			Format:      floatFormat,
			Disabled:    disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil {
					return float64(ed.Doc.VirtualHeight)
				}
				return 0
			},
			OnChange: func(v float64) {
				if ed.Doc == nil {
					return
				}
				w := ed.Doc.VirtualWidth
				if w == 0 && v > 0 {
					w, _ = ed.ViewportSize()
				}
				ed.report(ed.SetVirtualSize(w, float32(v)))
			},
		}),
		fieldRow("Background", bgControl),

		sectionHeader("CAMERA"),
		fieldRow("X", ui.NumberInput{
			TestID:   "inspector-camera-x",
			Label:    "Camera X",
			Step:     1,
			Format:   floatFormat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				return float64(ed.effectiveCamera().X)
			},
			OnChange: func(v float64) {
				cam := ed.effectiveCamera()
				fv := float32(v)
				if isFiniteFloat32(fv) && fv == cam.X {
					return
				}
				ed.report(ed.SetCameraPosition(fv, cam.Y))
			},
		}),
		fieldRow("Y", ui.NumberInput{
			TestID:   "inspector-camera-y",
			Label:    "Camera Y",
			Step:     1,
			Format:   floatFormat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				return float64(ed.effectiveCamera().Y)
			},
			OnChange: func(v float64) {
				cam := ed.effectiveCamera()
				fv := float32(v)
				if isFiniteFloat32(fv) && fv == cam.Y {
					return
				}
				ed.report(ed.SetCameraPosition(cam.X, fv))
			},
		}),
		fieldRow("Zoom", ui.NumberInput{
			TestID:   "inspector-camera-zoom",
			Label:    "Camera Zoom",
			Step:     0.1,
			Format:   floatFormat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				cam := ed.effectiveCamera()
				return float64(cam.EffectiveZoom())
			},
			OnChange: func(v float64) {
				cam := ed.effectiveCamera()
				fv := float32(v)
				if isFiniteFloat32(fv) && fv > 0 && fv == cam.EffectiveZoom() {
					return
				}
				ed.report(ed.SetCameraZoom(fv))
			},
		}),
		fieldRow("Rotation", ui.NumberInput{
			TestID:   "inspector-camera-rotation",
			Label:    "Camera Rotation",
			Step:     0.05,
			Format:   floatFormat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				return float64(ed.effectiveCamera().Rotation)
			},
			OnChange: func(v float64) {
				fv := float32(v)
				if isFiniteFloat32(fv) && fv == ed.effectiveCamera().Rotation {
					return
				}
				ed.report(ed.SetCameraRotation(fv))
			},
		}),
		fieldRow("Bounds", ui.Switch{
			TestID:   "inspector-camera-bounds-enabled",
			Label:    "Camera Bounds",
			Disabled: disabled,
			Checked:  boundsEnabled,
			OnChange: func(on bool) {
				if !on {
					ed.report(ed.ClearCameraBounds())
					return
				}
				vw, vh := ed.ViewportSize()
				ed.report(ed.SetCameraBounds(&sceneformat.BoundsDoc{
					X:      0,
					Y:      0,
					Width:  vw,
					Height: vh,
				}))
			},
		}),
		core.Show{
			When: boundsEnabled,
			Then: func(cx *core.Scope) core.Node {
				return core.View{
					TestID: "inspector-camera-bounds-fields",
					Style:  core.Style{Gap: 8},
					Children: []core.Node{
						fieldRow("Bounds X", ui.NumberInput{
							TestID:   "inspector-camera-bounds-x",
							Label:    "Camera Bounds X",
							Step:     1,
							Format:   floatFormat,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if ed.Doc != nil && ed.Doc.Camera != nil && ed.Doc.Camera.Bounds != nil {
									return float64(ed.Doc.Camera.Bounds.X)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateBounds(func(b *sceneformat.BoundsDoc) { b.X = float32(v) })
							},
						}),
						fieldRow("Bounds Y", ui.NumberInput{
							TestID:   "inspector-camera-bounds-y",
							Label:    "Camera Bounds Y",
							Step:     1,
							Format:   floatFormat,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if ed.Doc != nil && ed.Doc.Camera != nil && ed.Doc.Camera.Bounds != nil {
									return float64(ed.Doc.Camera.Bounds.Y)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateBounds(func(b *sceneformat.BoundsDoc) { b.Y = float32(v) })
							},
						}),
						fieldRow("Bounds W", ui.NumberInput{
							TestID:   "inspector-camera-bounds-width",
							Label:    "Camera Bounds Width",
							Step:     1,
							Format:   floatFormat,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if ed.Doc != nil && ed.Doc.Camera != nil && ed.Doc.Camera.Bounds != nil {
									return float64(ed.Doc.Camera.Bounds.Width)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateBounds(func(b *sceneformat.BoundsDoc) { b.Width = float32(v) })
							},
						}),
						fieldRow("Bounds H", ui.NumberInput{
							TestID:   "inspector-camera-bounds-height",
							Label:    "Camera Bounds Height",
							Step:     1,
							Format:   floatFormat,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if ed.Doc != nil && ed.Doc.Camera != nil && ed.Doc.Camera.Bounds != nil {
									return float64(ed.Doc.Camera.Bounds.Height)
								}
								return 0
							},
							OnChange: func(v float64) {
								updateBounds(func(b *sceneformat.BoundsDoc) { b.Height = float32(v) })
							},
						}),
					},
				}
			},
		},
	}

	return core.View{
		TestID:   "inspector-scene",
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

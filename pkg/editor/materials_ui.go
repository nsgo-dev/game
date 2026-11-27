package editor

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// MaterialEditorPanelView renders the Material Editor panel docked below the viewport while a material file is open:
// a live preview canvas (3D sphere or cube rendered with render3d, or 2D sprite preview), and undoable controls for
// every field of resource.Material (name, baseColor, emissive, texture, metallic, roughness, opacity, blend, doubleSided, unlit).
func MaterialEditorPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.MaterialOpen()
	})
	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node { return materialEditorPanel(cx, ed, rev) },
	}
}

func materialEditorPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	cx.Cleanup(func() {
		ed.cancelMaterialColorPreview()
	})

	currentMat := func() *resource.Material {
		_ = rev.Get()
		if m := ed.Material(); m != nil {
			return m
		}
		d := defaultMaterial("")
		return &d
	}

	shapeBtn := func(testID, text string, shape MaterialPreviewShape) core.Node {
		return ui.ToggleButton{
			TestID: testID,
			Text:   text,
			On: func() bool {
				_ = rev.Get()
				return ed.MaterialPreviewShape() == shape
			},
			OnChange: func(on bool) {
				if on {
					ed.SetMaterialPreviewShape(shape)
				}
			},
		}
	}

	header := core.View{
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            8,
			Wrap:           core.Wrap,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.Text{
						TestID: "material-title",
						ContentFunc: func() string {
							_ = rev.Get()
							title := ed.T("material.title")
							if rel := ed.MaterialRelPath(); rel != "" {
								title += " (" + rel + ")"
							} else if p := ed.MaterialPath(); p != "" {
								title += " (" + filepath.Base(p) + ")"
							}
							if ed.MaterialDirty() {
								title += "*"
							}
							return title
						},
						Style: core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
					},
					core.Text{
						TestID: "material-preview-info",
						ContentFunc: func() string {
							_ = rev.Get()
							return materialPreviewInfo(ed.Material(), ed.MaterialPreviewShape())
						},
						Style: core.Style{FontSize: 11, Color: colors.Muted()},
					},
				},
			},
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					ui.Button{
						TestID:   "material-save",
						Text:     ed.T("material.save"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonOutline,
						Disabled: disabled,
						OnPress:  func() { ed.report(ed.SaveMaterial()) },
					},
					ui.Button{
						TestID:   "material-close",
						Text:     ed.T("material.close"),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: disabled,
						OnPress:  func() { ed.report(ed.CloseMaterial()) },
					},
				},
			},
		},
	}

	previewColumn := core.View{
		TestID: "material-preview-column",
		Style: core.Style{
			Width:      core.Pt(232),
			AlignItems: core.AlignCenter,
			Gap:        6,
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "material-preview-canvas",
				Style: core.Style{
					Width:        core.Pt(232),
					Height:       core.Pt(156),
					Background:   ed.SpecPalette(cx).Viewport,
					CornerRadius: 6,
					BorderWidth:  1,
					BorderColor:  colors.Line(),
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = rev.Get()
					drawMaterialPreviewCanvas(d, size, ed)
				},
			},
			ui.HStack{
				Gap: 4,
				Children: []core.Node{
					shapeBtn("material-preview-sphere", ed.T("material.shape.sphere"), MaterialPreviewSphere),
					shapeBtn("material-preview-cube", ed.T("material.shape.cube"), MaterialPreviewCube),
					shapeBtn("material-preview-sprite", ed.T("material.shape.sprite"), MaterialPreviewSprite),
				},
			},
		},
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
						Width:    core.Pt(82),
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

	// Base color and emissive color controls: ColorWell + ColorPicker + text Input + Clear button.
	colorControl := func(testPrefix, label, placeholder string, emissive bool, getStr func() string, setStr func(string) error) core.Node {
		fallback, _ := core.ParseColor(placeholder)
		pickerOpen := core.NewSignal(cx, false)
		cx.Effect(func() {
			if disabled() {
				cx.Untrack(func() { pickerOpen.Set(false) })
			}
		})
		draft := core.NewSignal(cx, "")
		dirty := core.NewSignal(cx, false)
		synced := core.NewMemo(cx, func() string {
			_ = rev.Get()
			return getStr()
		})
		cx.Effect(func() {
			cur := synced.Get()
			cx.Untrack(func() {
				draft.Set(cur)
				dirty.Set(false)
			})
		})
		commitText := func(raw string) {
			dirty.Set(false)
			trimmed := strings.TrimSpace(raw)
			if err := setStr(trimmed); err != nil {
				ed.report(err)
				draft.Set(getStr())
				return
			}
			draft.Set(trimmed)
		}
		well := ui.ColorWell{
			TestID: testPrefix + "-well",
			Label:  label + " Picker",
			Value: func() core.Color {
				_ = rev.Get()
				if raw := getStr(); raw != "" {
					if c, err := core.ParseColor(raw); err == nil {
						return c
					}
				}
				return fallback
			},
			Open: func() bool {
				return !disabled() && pickerOpen.Get()
			},
			OnOpenChange: func(open bool, _ base.Reason) {
				if disabled() {
					ed.cancelMaterialColorPreview()
					pickerOpen.Set(false)
					return
				}
				if !open {
					ed.report(ed.flushMaterialColorPreview())
				}
				pickerOpen.Set(open)
			},
			Picker: ui.ColorPicker{
				TestID:   testPrefix + "-picker",
				Format:   base.ColorFormatHex,
				Disabled: disabled,
				OnChange: func(_ core.Color, formatted string) {
					ed.previewMaterialColor(emissive, formatted)
				},
				OnChangeEnd: func(_ core.Color, formatted string) {
					ed.report(ed.finishMaterialColorPreview(emissive, formatted))
				},
			},
		}
		input := ui.Input{
			TestID:      testPrefix,
			Label:       label,
			Placeholder: placeholder,
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if dirty.Get() {
					return draft.Get()
				}
				return getStr()
			},
			OnFocus: func() {
				if !dirty.Get() {
					draft.Set(getStr())
				}
			},
			OnChange: func(text string) {
				draft.Set(text)
				dirty.Set(true)
			},
			OnSubmit: func(text string) {
				draft.Set(text)
				commitText(text)
			},
			OnBlur: func() {
				if dirty.Get() {
					commitText(draft.Get())
				}
			},
		}
		clearBtn := removeButton(testPrefix+"-clear", "Clear "+strings.ToLower(label), func() bool {
			return disabled() || getStr() == ""
		}, func() {
			ed.report(setStr(""))
		})
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
			},
			Children: []core.Node{
				well,
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{input},
				},
				clearBtn,
			},
		}
	}

	unitSliderRow := func(testPrefix, label string, get func() float32, set func(float32) error) core.Node {
		return fieldRow(label, core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(80)},
					Children: []core.Node{
						ui.Slider{
							TestID:   testPrefix + "-slider",
							Label:    label,
							Min:      0,
							Max:      1,
							Step:     0.01,
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								return float64(get())
							},
							ValueText: func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
							OnChange: func(v float64) {
								ed.report(set(float32(clampFiniteRange(v, 0, 1))))
							},
						},
					},
				},
				core.View{
					Style: core.Style{Width: core.Pt(110)},
					Children: []core.Node{
						ui.NumberInput{
							TestID:   testPrefix,
							Label:    label + " value",
							Min:      0,
							Max:      1,
							Step:     0.05,
							Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
							Disabled: disabled,
							Style:    core.Style{Width: core.Pct(100)},
							Value: func() float64 {
								_ = rev.Get()
								return float64(get())
							},
							OnChange: func(v float64) {
								ed.report(set(float32(clampFiniteRange(v, 0, 1))))
							},
						},
					},
				},
			},
		})
	}

	var texturesKey string
	var textures []string
	textureSelect := ui.Select[string]{
		TestID:      "material-texture",
		Label:       "Texture",
		Placeholder: "None",
		Searchable:  true,
		Disabled:    disabled,
		Style:       core.Style{Width: core.Pct(100)},
		OptionsFunc: func() []ui.Option[string] {
			cur := currentMat().Texture
			if key := ed.ProjectPath + "\x00" + strconv.FormatUint(ed.filesRev, 10); key != texturesKey {
				texturesKey, textures = key, ed.ProjectTextures()
			}
			opts := make([]ui.Option[string], 0, len(textures)+1)
			listed := false
			for _, t := range textures {
				listed = listed || t == cur
				opts = append(opts, ui.Option[string]{Value: t, Label: t})
			}
			if cur != "" && !listed {
				opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
			}
			return opts
		},
		Value: func() string {
			return currentMat().Texture
		},
		OnChange: func(tex string) { ed.report(ed.SetMaterialTexture(tex)) },
		OnClear:  func() { ed.report(ed.SetMaterialTexture("")) },
	}

	blendModes := resource.BlendModes()
	blendOpts := make([]ui.Option[string], 0, len(blendModes))
	for _, b := range blendModes {
		blendOpts = append(blendOpts, ui.Option[string]{Value: b, Label: b})
	}
	blendSelect := ui.Select[string]{
		TestID:     "material-blend",
		Label:      "Blend Mode",
		Searchable: true,
		Disabled:   disabled,
		Style:      core.Style{Width: core.Pct(100)},
		Options:    blendOpts,
		Value: func() string {
			if b := currentMat().Blend; b != "" {
				return b
			}
			return "sourceOver"
		},
		OnChange: func(b string) { ed.report(ed.SetMaterialBlend(b)) },
	}

	leftCol := core.View{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 6},
		Children: []core.Node{
			fieldRow(ed.T("material.field.name"), ui.Input{
				TestID:      "material-name",
				Label:       ed.T("material.field.nameLabel"),
				Placeholder: ed.T("material.field.namePlaceholder"),
				Disabled:    disabled,
				Value: func() string {
					return currentMat().Name
				},
				OnChange: func(v string) { ed.report(ed.SetMaterialName(v)) },
			}),
			fieldRow(ed.T("material.field.baseColor"), colorControl(
				"material-base-color",
				ed.T("material.field.baseColor"),
				"#ffffff",
				false,
				func() string { return currentMat().BaseColor },
				ed.SetMaterialBaseColor,
			)),
			fieldRow(ed.T("material.field.emissive"), colorControl(
				"material-emissive",
				ed.T("material.field.emissiveLabel"),
				"#000000",
				true,
				func() string { return currentMat().Emissive },
				ed.SetMaterialEmissive,
			)),
			fieldRow(ed.T("material.field.texture"), textureSelect),
		},
	}

	rightCol := core.View{
		Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), Gap: 6},
		Children: []core.Node{
			unitSliderRow("material-metallic", ed.T("material.field.metallic"), func() float32 { return currentMat().Metallic }, ed.SetMaterialMetallic),
			unitSliderRow("material-roughness", ed.T("material.field.roughness"), func() float32 { return currentMat().EffectiveRoughness() }, ed.SetMaterialRoughness),
			unitSliderRow("material-opacity", ed.T("material.field.opacity"), func() float32 { return currentMat().EffectiveOpacity() }, ed.SetMaterialOpacity),
			fieldRow(ed.T("material.field.blend"), blendSelect),
			fieldRow(ed.T("material.field.flags"), ui.HStack{
				Gap: 14,
				Children: []core.Node{
					ui.Checkbox{
						TestID:   "material-double-sided",
						Label:    ed.T("material.field.doubleSided"),
						Disabled: disabled,
						Checked:  func() bool { return currentMat().DoubleSided },
						OnChange: func(on bool) { ed.report(ed.SetMaterialDoubleSided(on)) },
					},
					ui.Checkbox{
						TestID:   "material-unlit",
						Label:    ed.T("material.field.unlit"),
						Disabled: disabled,
						Checked:  func() bool { return currentMat().Unlit },
						OnChange: func(on bool) { ed.report(ed.SetMaterialUnlit(on)) },
					},
				},
			}),
		},
	}

	fieldsArea := ui.ScrollView{
		TestID: "material-fields-scroll",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
		},
		ContentStyle: core.Style{
			Padding: core.Edges{Right: 6},
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction: core.Row,
					Gap:       14,
				},
				Children: []core.Node{
					leftCol,
					rightCol,
				},
			},
		},
	}

	return core.View{
		TestID: "material-panel",
		Style: dockedStyle(cx, core.Style{
			Height:      core.Pt(272),
			Background:  colors.Raised(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
			Padding:     core.Symmetric(8, 12),
			Gap:         8,
		}),
		Children: []core.Node{
			header,
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Gap:       12,
					Overflow:  core.Hidden,
				},
				Children: []core.Node{
					previewColumn,
					fieldsArea,
				},
			},
		},
	}
}

func drawMaterialPreviewCanvas(d *core.Drawing, size core.Size, ed *Editor) {
	if d == nil || ed == nil || ed.material == nil {
		return
	}
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	spec := ed.activeSpecPalette()
	// Draw transparency checkerboard so opacity and blend modes are legible.
	const cell float32 = 12
	for y := float32(0); y < h; y += cell {
		for x := float32(0); x < w; x += cell {
			col := spec.Viewport
			if (int(x/cell)+int(y/cell))%2 == 1 {
				col = spec.LineRaised
			}
			d.SetFillColor(col).FillRect(x, y, min(cell, w-x), min(cell, h-y))
		}
	}

	shape := ed.MaterialPreviewShape()
	if shape != MaterialPreviewSprite {
		img, err := ed.MaterialPreviewImage(MaterialPreviewSize)
		if err != nil {
			d.SetFillColor(spec.Removed.Fg).FillText("Preview error", 8, 20, core.Font{Size: 11})
			return
		}
		if img != nil {
			side := min(w, h)
			px := float32(img.Bounds().Dx())
			src := core.ImageSource{Pixels: img, Version: ed.material.previewVersion}
			d.DrawImageFrom(src, 0, 0, px, px, (w-side)/2, (h-side)/2, side, side)
		}
		return
	}

	// 2D sprite preview: render texture, selected sprite, or colored card with opacity & blend op.
	mat := ed.material.mat
	pad := float32(18)
	side := max(min(w, h)-2*pad, 16)
	rx, ry := (w-side)/2, (h-side)/2

	d.Save()
	if op := mat.BlendOp(); op != core.CompositeSourceOver {
		d.SetComposite(op)
	}
	if alpha := mat.EffectiveOpacity(); alpha < 1 {
		d.SetAlpha(alpha)
	}
	if tex := ed.resolveMaterialTextureImage(mat.Texture); tex != nil {
		tw, th := float32(tex.Bounds().Dx()), float32(tex.Bounds().Dy())
		src := core.ImageSource{Pixels: tex, Version: ed.material.previewVersion}
		if mat.BaseColor != "" {
			d.DrawTintedImageFrom(src, mat.Color(), 0, 0, tw, th, rx, ry, side, side)
		} else {
			d.DrawImageFrom(src, 0, 0, tw, th, rx, ry, side, side)
		}
	} else if entry, ok := ed.materialPreviewSpriteEntry(); ok {
		f := entry.Rect
		scale := min(side/max(f.W, 1), side/max(f.H, 1))
		dw, dh := f.W*scale, f.H*scale
		dx, dy := (w-dw)/2, (h-dh)/2
		if mat.BaseColor != "" {
			d.DrawTintedImageFrom(entry.Image, mat.Color(), f.X, f.Y, f.W, f.H, dx, dy, dw, dh)
		} else {
			d.DrawImageFrom(entry.Image, f.X, f.Y, f.W, f.H, dx, dy, dw, dh)
		}
	} else {
		d.SetFillColor(mat.Color()).FillRect(rx, ry, side, side)
	}
	if em, ok := mat.EmissiveColor(); ok {
		d.SetStrokeColor(em).SetLineWidth(2.5).StrokeRect(rx, ry, side, side)
	}
	d.Restore()

	// Subtle badge showing the filename or "2D" at the bottom-left.
	label := "2D"
	if ed.material.relPath != "" {
		label = path.Base(ed.material.relPath)
	}
	d.Save()
	d.SetFillColor(specAlpha(spec.PillBg, 0.85)).FillRect(4, h-18, w-8, 14)
	d.SetFillColor(spec.TextSecondary).FillText(label, 8, h-7, core.Font{Size: 10})
	d.Restore()
}

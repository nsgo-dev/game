package editor

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// EnvironmentPanelView renders the docked Scene Environment settings panel (sky color/gradient/HDR,
// ambient & IBL intensity, distance & height fog, tonemapping, and exposure) while open.
func EnvironmentPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	open := func() bool {
		_ = rev.Get()
		return ed.EnvironmentPanelOpen() && ed.Doc != nil
	}
	return core.Show{
		When: open,
		Then: func(cx *core.Scope) core.Node {
			schema := core.NewMemo(cx, func() string {
				_ = rev.Get()
				return envPanelSchemaKey(ed)
			})
			return core.View{
				TestID: "environment-panel",
				Style: dockedStyle(cx, core.Style{
					Height:      core.Pt(250),
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.All(10),
					Gap:         8,
				}),
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "environment-panel-title",
								Content: ed.T("envPanel.title"),
								Style: core.Style{
									FontSize:   13,
									FontWeight: 700,
									Color:      colors.Text(),
								},
							},
							ui.HStack{
								Gap: 6,
								Children: []core.Node{
									ui.Button{
										TestID:  "env-bake-probes",
										Text:    ed.T("envPanel.bakeProbes"),
										Size:    ui.SizeSmall,
										Variant: ui.ButtonPrimary,
										Disabled: func() bool {
											_ = rev.Get()
											return ed.Playing() || !ed.HasProbes3D()
										},
										OnPress: func() {
											_, err := ed.BakeAllProbes3D()
											ed.report(err)
										},
									},
									ui.Button{
										TestID:  "env-clear",
										Text:    ed.T("envPanel.reset"),
										Size:    ui.SizeSmall,
										Variant: ui.ButtonGhost,
										Disabled: func() bool {
											_ = rev.Get()
											return ed.Playing() || ed.Doc == nil || ed.Doc.Environment == nil
										},
										OnPress: func() {
											ed.report(ed.SetEnvironment(nil))
										},
									},
									ui.Button{
										TestID:  "env-close",
										Text:    ed.T("envPanel.close"),
										Size:    ui.SizeSmall,
										Variant: ui.ButtonGhost,
										OnPress: func() {
											ed.SetEnvironmentPanelOpen(false)
										},
									},
								},
							},
						},
					},
					ui.ScrollView{
						Style:        core.Style{FlexGrow: 1},
						ContentStyle: core.Style{Gap: 10},
						Children: []core.Node{
							core.Dynamic(func(cx *core.Scope) core.Node {
								_ = schema.Get()
								return environmentPanelBody(cx, ed, rev)
							}),
						},
					},
				},
			}
		},
	}
}

func envPanelSchemaKey(ed *Editor) string {
	if ed == nil || ed.Doc == nil || ed.Doc.Environment == nil {
		return "none"
	}
	env := ed.Doc.Environment
	var b strings.Builder
	if env.Sky != nil {
		b.WriteString("sky:" + string(env.Sky.Kind))
	} else {
		b.WriteString("sky:none")
	}
	b.WriteByte('|')
	if env.Fog != nil {
		b.WriteString("fog:" + string(env.Fog.Mode))
		if env.Fog.HeightEnabled {
			b.WriteString(":h")
		}
	} else {
		b.WriteString("fog:off")
	}
	return b.String()
}

func environmentPanelBody(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Doc == nil
	}
	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

	envCopy := func() *sceneformat.EnvironmentDoc {
		if ed.Doc != nil && ed.Doc.Environment != nil {
			return ed.Doc.Environment.Clone()
		}
		return &sceneformat.EnvironmentDoc{}
	}
	mutateEnv := func(fn func(e *sceneformat.EnvironmentDoc)) {
		next := envCopy()
		fn(next)
		ed.report(ed.SetEnvironment(next))
	}

	sectionTitle := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:   11,
				FontWeight: 700,
				Color:      colors.Muted(),
			},
		}
	}
	row := func(label string, ctrl core.Node) core.Node {
		return core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: []core.Node{
				core.Text{
					Content: label,
					Style:   core.Style{Width: core.Pt(92), FontSize: 12, Color: colors.Text()},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{ctrl},
				},
			},
		}
	}

	var curSky sceneformat.SkyKind
	var curFog sceneformat.FogMode
	var heightFog bool
	if ed.Doc != nil && ed.Doc.Environment != nil {
		if ed.Doc.Environment.Sky != nil {
			curSky = ed.Doc.Environment.Sky.Kind
		}
		if ed.Doc.Environment.Fog != nil {
			curFog = ed.Doc.Environment.Fog.Mode
			heightFog = ed.Doc.Environment.Fog.HeightEnabled
		}
	}

	skyChildren := []core.Node{
		sectionTitle(ed.T("envPanel.section.skyAmbient")),
		row(ed.T("envPanel.sky"), ui.Select[string]{
			TestID:   "env-sky-kind",
			Label:    ed.T("envPanel.skyKindLabel"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: "", Label: ed.T("envPanel.none")},
				{Value: string(sceneformat.SkyColor), Label: ed.T("envPanel.sky.color")},
				{Value: string(sceneformat.SkyGradient), Label: ed.T("envPanel.sky.gradient")},
				{Value: string(sceneformat.SkyHDR), Label: ed.T("envPanel.sky.hdr")},
			},
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
					return string(ed.Doc.Environment.Sky.Kind)
				}
				return ""
			},
			OnChange: func(v string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					switch sceneformat.SkyKind(v) {
					case "":
						e.Sky = nil
					case sceneformat.SkyColor:
						e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyColor, Color: "#2b4263"}
					case sceneformat.SkyGradient:
						e.Sky = &sceneformat.SkyDoc{
							Kind:    sceneformat.SkyGradient,
							Zenith:  "#3d6194",
							Horizon: "#a8b8c7",
							Ground:  "#2b2b2e",
						}
					case sceneformat.SkyHDR:
						hdrPath := "env/sky.hdr"
						if hdrs := ed.ProjectHDRs(); len(hdrs) > 0 {
							hdrPath = hdrs[0]
						}
						e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyHDR, HDR: hdrPath}
					}
				})
			},
		}),
	}

	switch curSky {
	case sceneformat.SkyColor:
		skyChildren = append(skyChildren, row(ed.T("envPanel.skyColor"), ui.Input{
			TestID:      "env-sky-color",
			Label:       ed.T("envPanel.skyColor"),
			Placeholder: "#2b4263",
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
					return ed.Doc.Environment.Sky.Color
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					if e.Sky == nil {
						e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyColor}
					}
					e.Sky.Color = strings.TrimSpace(text)
				})
			},
		}))
	case sceneformat.SkyGradient:
		skyChildren = append(skyChildren,
			row(ed.T("envPanel.zenith"), ui.Input{
				TestID:      "env-sky-zenith",
				Label:       ed.T("envPanel.zenithLabel"),
				Placeholder: "#3d6194",
				Disabled:    disabled,
				Value: func() string {
					_ = rev.Get()
					if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
						return ed.Doc.Environment.Sky.Zenith
					}
					return ""
				},
				OnSubmit: func(text string) {
					mutateEnv(func(e *sceneformat.EnvironmentDoc) {
						if e.Sky == nil {
							e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyGradient}
						}
						e.Sky.Zenith = strings.TrimSpace(text)
					})
				},
			}),
			row(ed.T("envPanel.horizon"), ui.Input{
				TestID:      "env-sky-horizon",
				Label:       ed.T("envPanel.horizonLabel"),
				Placeholder: "#a8b8c7",
				Disabled:    disabled,
				Value: func() string {
					_ = rev.Get()
					if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
						return ed.Doc.Environment.Sky.Horizon
					}
					return ""
				},
				OnSubmit: func(text string) {
					mutateEnv(func(e *sceneformat.EnvironmentDoc) {
						if e.Sky == nil {
							e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyGradient}
						}
						e.Sky.Horizon = strings.TrimSpace(text)
					})
				},
			}),
			row(ed.T("envPanel.ground"), ui.Input{
				TestID:      "env-sky-ground",
				Label:       ed.T("envPanel.groundLabel"),
				Placeholder: "#2b2b2e",
				Disabled:    disabled,
				Value: func() string {
					_ = rev.Get()
					if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
						return ed.Doc.Environment.Sky.Ground
					}
					return ""
				},
				OnSubmit: func(text string) {
					mutateEnv(func(e *sceneformat.EnvironmentDoc) {
						if e.Sky == nil {
							e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyGradient}
						}
						e.Sky.Ground = strings.TrimSpace(text)
					})
				},
			}),
		)
	case sceneformat.SkyHDR:
		skyChildren = append(skyChildren, row(ed.T("envPanel.hdrPath"), ui.Input{
			TestID:      "env-sky-hdr",
			Label:       ed.T("envPanel.hdrPathLabel"),
			Placeholder: ed.T("envPanel.hdrPathPlaceholder"),
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Sky != nil {
					return ed.Doc.Environment.Sky.HDR
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					if e.Sky == nil {
						e.Sky = &sceneformat.SkyDoc{Kind: sceneformat.SkyHDR}
					}
					e.Sky.HDR = strings.TrimSpace(text)
				})
			},
		}))
	}

	skyChildren = append(skyChildren,
		row(ed.T("envPanel.ambientColor"), ui.Input{
			TestID:      "env-ambient-color",
			Label:       ed.T("envPanel.ambientColor"),
			Placeholder: "#ffffff",
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil {
					return ed.Doc.Environment.AmbientColor
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					e.AmbientColor = strings.TrimSpace(text)
				})
			},
		}),
		row(ed.T("envPanel.ambient"), ui.NumberInput{
			TestID:   "env-ambient-intensity",
			Label:    ed.T("envPanel.ambientIntensityLabel"),
			Min:      0,
			Step:     0.05,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil {
					return float64(ed.Doc.Environment.EffectiveAmbientIntensity())
				}
				return 0.25
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					e.AmbientIntensity = sceneformat.Float32(float32(v))
				})
			},
		}),
		row(ed.T("envPanel.ibl"), ui.NumberInput{
			TestID:   "env-ibl-intensity",
			Label:    ed.T("envPanel.iblIntensityLabel"),
			Min:      0,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil {
					return float64(ed.Doc.Environment.EffectiveIBLIntensity())
				}
				return 0
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					e.IBLIntensity = sceneformat.Float32(float32(v))
				})
			},
		}),
	)

	fogChildren := []core.Node{
		sectionTitle(ed.T("envPanel.section.fog")),
		row(ed.T("envPanel.fogMode"), ui.Select[string]{
			TestID:   "env-fog-mode",
			Label:    ed.T("envPanel.fogModeLabel"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: string(sceneformat.FogOff), Label: ed.T("envPanel.fog.off")},
				{Value: string(sceneformat.FogLinear), Label: ed.T("envPanel.fog.linear")},
				{Value: string(sceneformat.FogExp), Label: ed.T("envPanel.fog.exp")},
				{Value: string(sceneformat.FogExp2), Label: ed.T("envPanel.fog.exp2")},
			},
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil && ed.Doc.Environment.Fog.Mode != "" {
					return string(ed.Doc.Environment.Fog.Mode)
				}
				return string(sceneformat.FogOff)
			},
			OnChange: func(v string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					mode := sceneformat.FogMode(v)
					if mode == sceneformat.FogOff && (e.Fog == nil || !e.Fog.HeightEnabled) {
						e.Fog = nil
						return
					}
					if e.Fog == nil {
						e.Fog = &sceneformat.FogDoc{Color: "#9eb3c7"}
					}
					e.Fog.Mode = mode
					if mode == sceneformat.FogLinear && e.Fog.End <= e.Fog.Start {
						e.Fog.Start = 10
						e.Fog.End = 60
					}
					if (mode == sceneformat.FogExp || mode == sceneformat.FogExp2) && e.Fog.Density <= 0 {
						e.Fog.Density = 0.04
					}
				})
			},
		}),
	}

	if curFog != "" && curFog != sceneformat.FogOff || heightFog {
		fogChildren = append(fogChildren, row(ed.T("envPanel.fogColor"), ui.Input{
			TestID:      "env-fog-color",
			Label:       ed.T("envPanel.fogColor"),
			Placeholder: "#9eb3c7",
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil {
					return ed.Doc.Environment.Fog.Color
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					if e.Fog == nil {
						e.Fog = &sceneformat.FogDoc{}
					}
					e.Fog.Color = strings.TrimSpace(text)
				})
			},
		}))
	}
	if curFog == sceneformat.FogLinear {
		fogChildren = append(fogChildren,
			row(ed.T("envPanel.fogStart"), ui.NumberInput{
				TestID:   "env-fog-start",
				Label:    ed.T("envPanel.fogStartLabel"),
				Min:      0,
				Step:     1,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil {
						return float64(ed.Doc.Environment.Fog.Start)
					}
					return 0
				},
				OnChange: func(v float64) {
					if v < 0 {
						v = 0
					}
					mutateEnv(func(e *sceneformat.EnvironmentDoc) {
						if e.Fog == nil {
							e.Fog = &sceneformat.FogDoc{Mode: sceneformat.FogLinear}
						}
						e.Fog.Start = float32(v)
						if e.Fog.End < e.Fog.Start {
							e.Fog.End = e.Fog.Start
						}
					})
				},
			}),
			row(ed.T("envPanel.fogEnd"), ui.NumberInput{
				TestID:   "env-fog-end",
				Label:    ed.T("envPanel.fogEndLabel"),
				Min:      0,
				Step:     1,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil {
						return float64(ed.Doc.Environment.Fog.End)
					}
					return 60
				},
				OnChange: func(v float64) {
					if v < 0 {
						v = 0
					}
					mutateEnv(func(e *sceneformat.EnvironmentDoc) {
						if e.Fog == nil {
							e.Fog = &sceneformat.FogDoc{Mode: sceneformat.FogLinear}
						}
						e.Fog.End = float32(v)
						if e.Fog.End < e.Fog.Start {
							e.Fog.Start = e.Fog.End
						}
					})
				},
			}),
		)
	} else if curFog == sceneformat.FogExp || curFog == sceneformat.FogExp2 {
		fogChildren = append(fogChildren, row(ed.T("envPanel.fogDensity"), ui.NumberInput{
			TestID:   "env-fog-density",
			Label:    ed.T("envPanel.fogDensityLabel"),
			Min:      0,
			Step:     0.005,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil {
					return float64(ed.Doc.Environment.Fog.Density)
				}
				return 0.04
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					if e.Fog == nil {
						e.Fog = &sceneformat.FogDoc{Mode: curFog}
					}
					e.Fog.Density = float32(v)
				})
			},
		}))
	}

	fogChildren = append(fogChildren, row(ed.T("envPanel.heightFog"), SpecSwitch{
		TestID:   "env-fog-height",
		Label:    ed.T("envPanel.heightFog"),
		Disabled: disabled,
		Checked: func() bool {
			_ = rev.Get()
			return ed.Doc != nil && ed.Doc.Environment != nil && ed.Doc.Environment.Fog != nil && ed.Doc.Environment.Fog.HeightEnabled
		},
		OnChange: func(on bool) {
			mutateEnv(func(e *sceneformat.EnvironmentDoc) {
				if e.Fog == nil {
					if !on {
						return
					}
					e.Fog = &sceneformat.FogDoc{Mode: sceneformat.FogOff, Color: "#9eb3c7"}
				}
				e.Fog.HeightEnabled = on
				if on && e.Fog.HeightDensity <= 0 {
					e.Fog.HeightFalloff = 0.25
					e.Fog.HeightDensity = 0.05
				}
			})
		},
	}))

	postChildren := []core.Node{
		sectionTitle(ed.T("envPanel.section.tonemapping")),
		row(ed.T("envPanel.tonemapOperator"), ui.Select[string]{
			TestID:   "env-tonemapping",
			Label:    ed.T("envPanel.tonemapOperatorLabel"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: string(sceneformat.TonemapNone), Label: ed.T("envPanel.none")},
				{Value: string(sceneformat.TonemapReinhard), Label: ed.T("envPanel.tonemap.reinhard")},
				{Value: string(sceneformat.TonemapACES), Label: ed.T("envPanel.tonemap.aces")},
				{Value: string(sceneformat.TonemapNeutral), Label: ed.T("envPanel.tonemap.neutral")},
				{Value: string(sceneformat.TonemapAgX), Label: ed.T("envPanel.tonemap.agx")},
			},
			Value: func() string {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil {
					return string(ed.Doc.Environment.EffectiveTonemapping())
				}
				return string(sceneformat.TonemapNone)
			},
			OnChange: func(v string) {
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					e.Tonemapping = sceneformat.TonemapMode(v)
				})
			},
		}),
		row(ed.T("envPanel.exposure"), ui.NumberInput{
			TestID:   "env-exposure",
			Label:    ed.T("envPanel.exposure"),
			Min:      0,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if ed.Doc != nil && ed.Doc.Environment != nil {
					return float64(ed.Doc.Environment.EffectiveExposure())
				}
				return 1
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutateEnv(func(e *sceneformat.EnvironmentDoc) {
					e.Exposure = sceneformat.Float32(float32(v))
				})
			},
		}),
	}

	colStyle := core.Style{FlexGrow: 1, MinWidth: core.Pt(220), Gap: 6}
	return core.View{
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 16},
		Children: []core.Node{
			core.View{Style: colStyle, Children: skyChildren},
			core.View{Style: colStyle, Children: fogChildren},
			core.View{Style: colStyle, Children: postChildren},
		},
	}
}

// light3DInspectorFields returns the inspector rows for objID's 3D light component.
func light3DInspectorFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, core.Node) core.Node) []core.Node {
	obj, _ := ed.FindResolvedObject(objID)
	if obj == nil || obj.Light == nil {
		return []core.Node{
			ui.Button{
				TestID:   "inspector-light-add",
				Text:     ed.T("envPanel.light.add"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.SetObjectLight(objID, &sceneformat.LightDoc{
						Kind:      sceneformat.LightPoint,
						Color:     "#ffffff",
						Intensity: sceneformat.Float32(5),
						Range:     10,
					}))
				},
			},
		}
	}
	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	mutate := func(fn func(l *sceneformat.LightDoc)) {
		o, _ := ed.FindResolvedObject(objID)
		if o == nil || o.Light == nil {
			return
		}
		next := o.Light.Clone()
		fn(next)
		ed.report(ed.SetObjectLight(objID, next))
	}

	rows := []core.Node{
		fieldRow(ed.T("envPanel.light.kind"), ui.Select[string]{
			TestID:   "inspector-light-kind",
			Label:    ed.T("envPanel.light.kindLabel"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: string(sceneformat.LightDirectional), Label: ed.T("envPanel.light.directional")},
				{Value: string(sceneformat.LightPoint), Label: ed.T("envPanel.light.point")},
				{Value: string(sceneformat.LightSpot), Label: ed.T("envPanel.light.spot")},
			},
			Value: func() string {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
					return string(o.Light.Kind)
				}
				return ""
			},
			OnChange: func(v string) {
				mutate(func(l *sceneformat.LightDoc) {
					l.Kind = sceneformat.LightKind(v)
					switch l.Kind {
					case sceneformat.LightDirectional:
						l.Range = 0
						l.InnerCone = 0
						l.OuterCone = nil
					case sceneformat.LightPoint:
						l.InnerCone = 0
						l.OuterCone = nil
						if l.Range <= 0 {
							l.Range = 10
						}
					case sceneformat.LightSpot:
						if l.Range <= 0 {
							l.Range = 12
						}
						if l.OuterCone == nil {
							l.OuterCone = sceneformat.Float32(30)
						}
						if l.InnerCone > *l.OuterCone {
							l.InnerCone = *l.OuterCone
						}
					}
				})
			},
		}),
		fieldRow(ed.T("envPanel.light.color"), ui.Input{
			TestID:      "inspector-light-color",
			Label:       ed.T("envPanel.light.colorLabel"),
			Placeholder: "#ffffff",
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
					return o.Light.Color
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutate(func(l *sceneformat.LightDoc) { l.Color = strings.TrimSpace(text) })
			},
		}),
		fieldRow(ed.T("envPanel.light.intensity"), ui.NumberInput{
			TestID:   "inspector-light-intensity",
			Label:    ed.T("envPanel.light.intensityLabel"),
			Min:      0,
			Step:     0.25,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
					return float64(o.Light.EffectiveIntensity())
				}
				return 1
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutate(func(l *sceneformat.LightDoc) { l.Intensity = sceneformat.Float32(float32(v)) })
			},
		}),
	}
	if obj.Light.Kind == sceneformat.LightPoint || obj.Light.Kind == sceneformat.LightSpot {
		rows = append(rows, fieldRow(ed.T("envPanel.light.range"), ui.NumberInput{
			TestID:   "inspector-light-range",
			Label:    ed.T("envPanel.light.rangeLabel"),
			Min:      0,
			Step:     0.5,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
					return float64(o.Light.Range)
				}
				return 0
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutate(func(l *sceneformat.LightDoc) { l.Range = float32(v) })
			},
		}))
	}
	if obj.Light.Kind == sceneformat.LightSpot {
		rows = append(rows,
			fieldRow(ed.T("envPanel.light.innerCone"), ui.NumberInput{
				TestID:   "inspector-light-inner-cone",
				Label:    ed.T("envPanel.light.innerConeLabel"),
				Min:      0,
				Max:      89.5,
				Step:     1,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
						return float64(o.Light.InnerCone)
					}
					return 0
				},
				OnChange: func(v float64) {
					if v < 0 {
						v = 0
					}
					mutate(func(l *sceneformat.LightDoc) {
						outer := float64(l.EffectiveOuterCone())
						if v > outer {
							v = outer
						}
						l.InnerCone = float32(v)
					})
				},
			}),
			fieldRow(ed.T("envPanel.light.outerCone"), ui.NumberInput{
				TestID:   "inspector-light-outer-cone",
				Label:    ed.T("envPanel.light.outerConeLabel"),
				Min:      1,
				Max:      89.5,
				Step:     1,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _ := ed.FindResolvedObject(objID); o != nil && o.Light != nil {
						return float64(o.Light.EffectiveOuterCone())
					}
					return 45
				},
				OnChange: func(v float64) {
					if v < 1 {
						v = 1
					}
					if v > 89.5 {
						v = 89.5
					}
					mutate(func(l *sceneformat.LightDoc) {
						l.OuterCone = sceneformat.Float32(float32(v))
						if l.InnerCone > float32(v) {
							l.InnerCone = float32(v)
						}
					})
				},
			}),
		)
	}
	rows = append(rows,
		fieldRow(ed.T("envPanel.light.shadows"), SpecSwitch{
			TestID:   "inspector-light-shadows",
			Label:    ed.T("envPanel.light.shadowsLabel"),
			Disabled: disabled,
			Checked: func() bool {
				_ = rev.Get()
				o, _ := ed.FindResolvedObject(objID)
				return o != nil && o.Light != nil && o.Light.Shadows
			},
			OnChange: func(on bool) {
				mutate(func(l *sceneformat.LightDoc) { l.Shadows = on })
			},
		}),
		ui.Button{
			TestID:   "inspector-light-remove",
			Text:     ed.T("envPanel.light.remove"),
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Disabled: disabled,
			OnPress: func() {
				ed.report(ed.SetObjectLight(objID, nil))
			},
		},
	)
	return rows
}

// camera3DInspectorFields returns the inspector rows for objID's 3D camera component.
func camera3DInspectorFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, core.Node) core.Node) []core.Node {
	obj, _ := ed.FindResolvedObject(objID)
	if obj == nil || obj.Camera == nil {
		return []core.Node{
			ui.Button{
				TestID:   "inspector-cam3d-add",
				Text:     ed.T("envPanel.camera.add"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					ed.report(ed.SetObjectCamera3D(objID, &sceneformat.Camera3DDoc{
						Projection: sceneformat.ProjectionPerspective,
						FOV:        sceneformat.Float32(60),
						Near:       sceneformat.Float32(0.1),
						Far:        sceneformat.Float32(1000),
						Current:    !ed.HasSceneCamera3D(),
					}))
				},
			},
		}
	}
	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	mutate := func(fn func(c *sceneformat.Camera3DDoc)) {
		o, _ := ed.FindResolvedObject(objID)
		if o == nil || o.Camera == nil {
			return
		}
		next := o.Camera.Clone()
		fn(next)
		ed.report(ed.SetObjectCamera3D(objID, next))
	}

	rows := []core.Node{
		fieldRow(ed.T("envPanel.camera.projection"), ui.Select[string]{
			TestID:   "inspector-cam3d-projection",
			Label:    ed.T("envPanel.camera.projection"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: string(sceneformat.ProjectionPerspective), Label: ed.T("envPanel.camera.perspective")},
				{Value: string(sceneformat.ProjectionOrthographic), Label: ed.T("envPanel.camera.orthographic")},
			},
			Value: func() string {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Camera != nil {
					return string(o.Camera.EffectiveProjection())
				}
				return string(sceneformat.ProjectionPerspective)
			},
			OnChange: func(v string) {
				mutate(func(c *sceneformat.Camera3DDoc) {
					c.Projection = sceneformat.ProjectionKind(v)
					if c.Projection == sceneformat.ProjectionOrthographic {
						c.FOV = nil
						if c.OrthoHeight == nil {
							c.OrthoHeight = sceneformat.Float32(10)
						}
					} else {
						c.OrthoHeight = nil
						if c.FOV == nil {
							c.FOV = sceneformat.Float32(60)
						}
					}
				})
			},
		}),
	}
	if obj.Camera.EffectiveProjection() == sceneformat.ProjectionOrthographic {
		rows = append(rows, fieldRow(ed.T("envPanel.camera.orthoH"), ui.NumberInput{
			TestID:   "inspector-cam3d-ortho-height",
			Label:    ed.T("envPanel.camera.orthoHLabel"),
			Min:      0.1,
			Step:     0.5,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Camera != nil {
					return float64(o.Camera.EffectiveOrthoHeight())
				}
				return 10
			},
			OnChange: func(v float64) {
				if v < 0.1 {
					v = 0.1
				}
				mutate(func(c *sceneformat.Camera3DDoc) { c.OrthoHeight = sceneformat.Float32(float32(v)) })
			},
		}))
	} else {
		rows = append(rows, fieldRow(ed.T("envPanel.camera.fov"), ui.NumberInput{
			TestID:   "inspector-cam3d-fov",
			Label:    ed.T("envPanel.camera.fovLabel"),
			Min:      5,
			Max:      170,
			Step:     1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Camera != nil {
					return float64(o.Camera.EffectiveFOV())
				}
				return 60
			},
			OnChange: func(v float64) {
				if v < 5 {
					v = 5
				}
				if v > 170 {
					v = 170
				}
				mutate(func(c *sceneformat.Camera3DDoc) { c.FOV = sceneformat.Float32(float32(v)) })
			},
		}))
	}

	rows = append(rows,
		fieldRow(ed.T("envPanel.camera.near"), ui.NumberInput{
			TestID:   "inspector-cam3d-near",
			Label:    ed.T("envPanel.camera.nearLabel"),
			Min:      0.001,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Camera != nil {
					return float64(o.Camera.EffectiveNear())
				}
				return 0.1
			},
			OnChange: func(v float64) {
				if v <= 0 {
					v = 0.01
				}
				mutate(func(c *sceneformat.Camera3DDoc) {
					c.Near = sceneformat.Float32(float32(v))
					if c.EffectiveFar() <= float32(v) {
						c.Far = sceneformat.Float32(float32(v) + 1)
					}
				})
			},
		}),
		fieldRow(ed.T("envPanel.camera.far"), ui.NumberInput{
			TestID:   "inspector-cam3d-far",
			Label:    ed.T("envPanel.camera.farLabel"),
			Min:      0.1,
			Step:     5,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _ := ed.FindResolvedObject(objID); o != nil && o.Camera != nil {
					return float64(o.Camera.EffectiveFar())
				}
				return 1000
			},
			OnChange: func(v float64) {
				mutate(func(c *sceneformat.Camera3DDoc) {
					minFar := float64(c.EffectiveNear()) + 0.1
					if v < minFar {
						v = minFar
					}
					c.Far = sceneformat.Float32(float32(v))
				})
			},
		}),
		fieldRow(ed.T("envPanel.camera.current"), SpecSwitch{
			TestID:   "inspector-cam3d-current",
			Label:    ed.T("envPanel.camera.currentLabel"),
			Disabled: disabled,
			Checked: func() bool {
				_ = rev.Get()
				o, _ := ed.FindResolvedObject(objID)
				return o != nil && o.Camera != nil && o.Camera.Current
			},
			OnChange: func(on bool) {
				mutate(func(c *sceneformat.Camera3DDoc) { c.Current = on })
			},
		}),
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.ToggleButton{
					TestID: "inspector-cam3d-look",
					Text:   ed.T("envPanel.camera.lookThrough"),
					On: func() bool {
						_ = rev.Get()
						return ed.LookThroughCamera3D()
					},
					OnChange: func(on bool) {
						ed.report(ed.SetLookThroughCamera3D(on))
					},
				},
				ui.Button{
					TestID:   "inspector-cam3d-remove",
					Text:     ed.T("envPanel.camera.remove"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.SetObjectCamera3D(objID, nil))
					},
				},
			},
		},
	)
	return rows
}

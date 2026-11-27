package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// reflectionProbeInspectorFields returns the inspector rows for objID's 3D reflection probe.
func reflectionProbeInspectorFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, core.Node) core.Node) []core.Node {
	obj, _, _ := ed.FindObject(objID)
	if obj == nil || obj.ReflectionProbe == nil {
		return []core.Node{
			ui.Button{
				TestID:   "inspector-reflprobe-add",
				Text:     ed.T("probes3d.addReflectionProbe"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					sz := [3]float32{8, 4, 8}
					ed.report(ed.SetObjectReflectionProbe(objID, &sceneformat.ReflectionProbeDoc{
						Size:          &sz,
						BlendDistance: 0.5,
						Intensity:     sceneformat.Float32(1),
						BoxProjection: sceneformat.Bool(true),
						Resolution:    64,
					}))
				},
			},
		}
	}

	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	mutate := func(fn func(p *sceneformat.ReflectionProbeDoc)) {
		o, _, _ := ed.FindObject(objID)
		if o == nil || o.ReflectionProbe == nil {
			return
		}
		next := o.ReflectionProbe.Clone()
		fn(next)
		ed.report(ed.SetObjectReflectionProbe(objID, next))
	}

	vec3Row := func(prefix, label string, minVal float64, read func(*sceneformat.ReflectionProbeDoc) [3]float32, write func(*sceneformat.ReflectionProbeDoc, [3]float32)) core.Node {
		axisInput := func(suffix, aria string, idx int) core.Node {
			return ui.NumberInput{
				TestID:   prefix + "-" + suffix,
				Label:    aria,
				Min:      minVal,
				Step:     0.25,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
						return float64(read(o.ReflectionProbe)[idx])
					}
					return 0
				},
				OnChange: func(v float64) {
					if v < minVal {
						v = minVal
					}
					mutate(func(p *sceneformat.ReflectionProbeDoc) {
						cur := read(p)
						cur[idx] = float32(v)
						write(p, cur)
					})
				},
			}
		}
		return fieldRow(label, core.View{
			Style: core.Style{Direction: core.Row, Gap: 4},
			Children: []core.Node{
				axisInput("x", label+" X", 0),
				axisInput("y", label+" Y", 1),
				axisInput("z", label+" Z", 2),
			},
		})
	}

	return []core.Node{
		vec3Row("inspector-reflprobe-center", ed.T("probes3d.center"), -100000,
			func(p *sceneformat.ReflectionProbeDoc) [3]float32 { return p.EffectiveCenter() },
			func(p *sceneformat.ReflectionProbeDoc, v [3]float32) {
				if v == ([3]float32{}) {
					p.Center = nil
				} else {
					p.Center = &v
				}
			},
		),
		vec3Row("inspector-reflprobe-size", ed.T("probes3d.size"), 0.1,
			func(p *sceneformat.ReflectionProbeDoc) [3]float32 { return p.EffectiveSize() },
			func(p *sceneformat.ReflectionProbeDoc, v [3]float32) { p.Size = &v },
		),
		fieldRow(ed.T("probes3d.blend"), ui.NumberInput{
			TestID:   "inspector-reflprobe-blend",
			Label:    ed.T("probes3d.reflBlendLabel"),
			Min:      0,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
					return float64(o.ReflectionProbe.BlendDistance)
				}
				return 0
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutate(func(p *sceneformat.ReflectionProbeDoc) { p.BlendDistance = float32(v) })
			},
		}),
		fieldRow(ed.T("probes3d.intensity"), ui.NumberInput{
			TestID:   "inspector-reflprobe-intensity",
			Label:    ed.T("probes3d.reflIntensityLabel"),
			Min:      0,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
					return float64(o.ReflectionProbe.EffectiveIntensity())
				}
				return 1
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutate(func(p *sceneformat.ReflectionProbeDoc) { p.Intensity = sceneformat.Float32(float32(v)) })
			},
		}),
		fieldRow(ed.T("probes3d.priority"), ui.NumberInput{
			TestID:   "inspector-reflprobe-priority",
			Label:    ed.T("probes3d.reflPriorityLabel"),
			Step:     1,
			Format:   func(v float64) string { return strconv.Itoa(int(math.Round(v))) },
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
					return float64(o.ReflectionProbe.Priority)
				}
				return 0
			},
			OnChange: func(v float64) {
				mutate(func(p *sceneformat.ReflectionProbeDoc) { p.Priority = int(math.Round(v)) })
			},
		}),
		fieldRow(ed.T("probes3d.boxProj"), SpecSwitch{
			TestID:   "inspector-reflprobe-boxproj",
			Label:    ed.T("probes3d.boxProjectionLabel"),
			Disabled: disabled,
			Checked: func() bool {
				_ = rev.Get()
				o, _, _ := ed.FindObject(objID)
				return o != nil && o.ReflectionProbe != nil && o.ReflectionProbe.EffectiveBoxProjection()
			},
			OnChange: func(on bool) {
				mutate(func(p *sceneformat.ReflectionProbeDoc) { p.BoxProjection = sceneformat.Bool(on) })
			},
		}),
		fieldRow(ed.T("probes3d.resolution"), ui.Select[string]{
			TestID:   "inspector-reflprobe-resolution",
			Label:    ed.T("probes3d.cubemapResolutionLabel"),
			Disabled: disabled,
			Options: []ui.Option[string]{
				{Value: "16", Label: "16"},
				{Value: "32", Label: "32"},
				{Value: "64", Label: "64"},
				{Value: "128", Label: "128"},
				{Value: "256", Label: "256"},
			},
			Value: func() string {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
					return strconv.Itoa(o.ReflectionProbe.EffectiveResolution())
				}
				return "64"
			},
			OnChange: func(v string) {
				if n, err := strconv.Atoi(v); err == nil {
					mutate(func(p *sceneformat.ReflectionProbeDoc) { p.Resolution = n })
				}
			},
		}),
		fieldRow(ed.T("probes3d.cubemap"), ui.Input{
			TestID:      "inspector-reflprobe-cubemap",
			Label:       ed.T("probes3d.cubemapPathLabel"),
			Placeholder: ed.T("probes3d.cubemapPlaceholder"),
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.ReflectionProbe != nil {
					return o.ReflectionProbe.Cubemap
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutate(func(p *sceneformat.ReflectionProbeDoc) { p.Cubemap = strings.TrimSpace(text) })
			},
		}),
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.Button{
					TestID:   "inspector-reflprobe-bake",
					Text:     ed.T("probes3d.bakeCubemap"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonPrimary,
					Disabled: disabled,
					OnPress: func() {
						_, err := ed.BakeReflectionProbe3D(objID)
						ed.report(err)
					},
				},
				ui.Button{
					TestID:   "inspector-reflprobe-remove",
					Text:     ed.T("probes3d.remove"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.SetObjectReflectionProbe(objID, nil))
					},
				},
			},
		},
	}
}

// lightProbeGridInspectorFields returns the inspector rows for objID's 3D light probe grid.
func lightProbeGridInspectorFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, core.Node) core.Node) []core.Node {
	obj, _, _ := ed.FindObject(objID)
	if obj == nil || obj.LightProbeGrid == nil {
		return []core.Node{
			ui.Button{
				TestID:   "inspector-lightprobe-add",
				Text:     ed.T("probes3d.addLightProbeGrid"),
				Size:     ui.SizeSmall,
				Variant:  ui.ButtonOutline,
				Disabled: disabled,
				OnPress: func() {
					sz := [3]float32{8, 4, 8}
					cnt := [3]int{3, 2, 3}
					ed.report(ed.SetObjectLightProbeGrid(objID, &sceneformat.LightProbeGridDoc{
						Size:      &sz,
						Counts:    &cnt,
						Intensity: sceneformat.Float32(1),
					}))
				},
			},
		}
	}

	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	formatInt := func(v float64) string { return strconv.Itoa(int(math.Round(v))) }
	mutate := func(fn func(g *sceneformat.LightProbeGridDoc)) {
		o, _, _ := ed.FindObject(objID)
		if o == nil || o.LightProbeGrid == nil {
			return
		}
		next := o.LightProbeGrid.Clone()
		fn(next)
		ed.report(ed.SetObjectLightProbeGrid(objID, next))
	}

	vec3Row := func(prefix, label string, minVal float64, read func(*sceneformat.LightProbeGridDoc) [3]float32, write func(*sceneformat.LightProbeGridDoc, [3]float32)) core.Node {
		axisInput := func(suffix, aria string, idx int) core.Node {
			return ui.NumberInput{
				TestID:   prefix + "-" + suffix,
				Label:    aria,
				Min:      minVal,
				Step:     0.25,
				Format:   formatFloat,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil && o.LightProbeGrid != nil {
						return float64(read(o.LightProbeGrid)[idx])
					}
					return 0
				},
				OnChange: func(v float64) {
					if v < minVal {
						v = minVal
					}
					mutate(func(g *sceneformat.LightProbeGridDoc) {
						cur := read(g)
						cur[idx] = float32(v)
						write(g, cur)
					})
				},
			}
		}
		return fieldRow(label, core.View{
			Style: core.Style{Direction: core.Row, Gap: 4},
			Children: []core.Node{
				axisInput("x", label+" X", 0),
				axisInput("y", label+" Y", 1),
				axisInput("z", label+" Z", 2),
			},
		})
	}

	countInput := func(suffix, aria string, idx int) core.Node {
		return ui.NumberInput{
			TestID:   "inspector-lightprobe-counts-" + suffix,
			Label:    aria,
			Min:      1,
			Max:      32,
			Step:     1,
			Format:   formatInt,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.LightProbeGrid != nil {
					return float64(o.LightProbeGrid.EffectiveCounts()[idx])
				}
				return 2
			},
			OnChange: func(v float64) {
				n := int(math.Round(v))
				if n < 1 {
					n = 1
				}
				if n > 32 {
					n = 32
				}
				mutate(func(g *sceneformat.LightProbeGridDoc) {
					cur := g.EffectiveCounts()
					cur[idx] = n
					g.Counts = &cur
				})
			},
		}
	}

	return []core.Node{
		vec3Row("inspector-lightprobe-center", ed.T("probes3d.center"), -100000,
			func(g *sceneformat.LightProbeGridDoc) [3]float32 { return g.EffectiveCenter() },
			func(g *sceneformat.LightProbeGridDoc, v [3]float32) {
				if v == ([3]float32{}) {
					g.Center = nil
				} else {
					g.Center = &v
				}
			},
		),
		vec3Row("inspector-lightprobe-size", ed.T("probes3d.size"), 0.1,
			func(g *sceneformat.LightProbeGridDoc) [3]float32 { return g.EffectiveSize() },
			func(g *sceneformat.LightProbeGridDoc, v [3]float32) { g.Size = &v },
		),
		fieldRow(ed.T("probes3d.counts"), core.View{
			Style: core.Style{Direction: core.Row, Gap: 4},
			Children: []core.Node{
				countInput("x", ed.T("probes3d.probeCountX"), 0),
				countInput("y", ed.T("probes3d.probeCountY"), 1),
				countInput("z", ed.T("probes3d.probeCountZ"), 2),
			},
		}),
		fieldRow(ed.T("probes3d.intensity"), ui.NumberInput{
			TestID:   "inspector-lightprobe-intensity",
			Label:    ed.T("probes3d.lightProbeIntensityLabel"),
			Min:      0,
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.LightProbeGrid != nil {
					return float64(o.LightProbeGrid.EffectiveIntensity())
				}
				return 1
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				mutate(func(g *sceneformat.LightProbeGridDoc) { g.Intensity = sceneformat.Float32(float32(v)) })
			},
		}),
		fieldRow(ed.T("probes3d.data"), ui.Input{
			TestID:      "inspector-lightprobe-data",
			Label:       ed.T("probes3d.lightProbeDataLabel"),
			Placeholder: "probes/room.probes.yaml",
			Disabled:    disabled,
			Value: func() string {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil && o.LightProbeGrid != nil {
					return o.LightProbeGrid.Data
				}
				return ""
			},
			OnSubmit: func(text string) {
				mutate(func(g *sceneformat.LightProbeGridDoc) { g.Data = strings.TrimSpace(text) })
			},
		}),
		ui.HStack{
			Gap: 6,
			Children: []core.Node{
				ui.Button{
					TestID:   "inspector-lightprobe-bake",
					Text:     ed.T("probes3d.bakeProbes"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonPrimary,
					Disabled: disabled,
					OnPress: func() {
						_, err := ed.BakeLightProbeGrid3D(objID)
						ed.report(err)
					},
				},
				ui.Button{
					TestID:   "inspector-lightprobe-remove",
					Text:     ed.T("probes3d.remove"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonGhost,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.SetObjectLightProbeGrid(objID, nil))
					},
				},
			},
		},
	}
}

package editor

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const mixedSelectSentinel = "\x00mixed"

func multiInspectorSchemaKey(ed *Editor, ids []string) string {
	objs := make([]*sceneformat.ObjectDoc, 0, len(ids))
	for _, id := range ids {
		if o, _ := ed.FindResolvedObject(id); o != nil {
			objs = append(objs, o)
		}
	}
	if len(objs) <= 1 {
		return ""
	}
	var b strings.Builder
	b.WriteString("M:")
	b.WriteString(ed.ViewMode.String())
	b.WriteByte(':')
	for i, o := range objs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(o.ID)
	}
	if allHaveSprite(objs) {
		b.WriteString("|S:1")
	}
	if props := sharedPropertyKeys(objs); len(props) > 0 {
		b.WriteString("|P:")
		for _, k := range props {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(valueKindTag(objs[0].Properties[k]))
			b.WriteByte(',')
		}
	}
	if comps := sharedComponentNames(objs); len(comps) > 0 {
		b.WriteString("|C:")
		for _, cn := range comps {
			b.WriteString(cn)
			b.WriteByte('{')
			if t, ok := ed.lookupComponentType(cn); ok {
				b.WriteString("registered:")
				b.WriteString(strings.Join(t.FieldNames(), ","))
			} else {
				for _, fk := range sharedComponentFieldKeys(objs, cn) {
					b.WriteString(fk)
					b.WriteByte('=')
					if m, ok := objs[0].Components[cn].(map[string]any); ok {
						b.WriteString(valueKindTag(m[fk]))
					}
					b.WriteByte(',')
				}
			}
			b.WriteByte('}')
		}
	}
	return b.String()
}

func allHaveSprite(objs []*sceneformat.ObjectDoc) bool {
	if len(objs) == 0 {
		return false
	}
	for _, o := range objs {
		if o.Sprite == nil {
			return false
		}
	}
	return true
}

func sharedPropertyKeys(objs []*sceneformat.ObjectDoc) []string {
	if len(objs) == 0 || len(objs[0].Properties) == 0 {
		return nil
	}
	var keys []string
	for k, v0 := range objs[0].Properties {
		tag0 := valueKindTag(v0)
		shared := true
		for _, o := range objs[1:] {
			v, ok := o.Properties[k]
			if !ok || valueKindTag(v) != tag0 {
				shared = false
				break
			}
		}
		if shared {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func sharedComponentNames(objs []*sceneformat.ObjectDoc) []string {
	if len(objs) == 0 || len(objs[0].Components) == 0 {
		return nil
	}
	var names []string
	for cn, raw0 := range objs[0].Components {
		if _, ok := raw0.(map[string]any); !ok {
			continue
		}
		shared := true
		for _, o := range objs[1:] {
			raw, ok := o.Components[cn]
			if !ok {
				shared = false
				break
			}
			if _, ok := raw.(map[string]any); !ok {
				shared = false
				break
			}
		}
		if shared {
			names = append(names, cn)
		}
	}
	slices.Sort(names)
	return names
}

func sharedComponentFieldKeys(objs []*sceneformat.ObjectDoc, cn string) []string {
	if len(objs) == 0 {
		return nil
	}
	m0, ok := objs[0].Components[cn].(map[string]any)
	if !ok || len(m0) == 0 {
		return nil
	}
	var keys []string
	for fk, v0 := range m0 {
		tag0 := valueKindTag(v0)
		shared := true
		for _, o := range objs[1:] {
			m, ok := o.Components[cn].(map[string]any)
			if !ok {
				shared = false
				break
			}
			v, exists := m[fk]
			if !exists || valueKindTag(v) != tag0 {
				shared = false
				break
			}
		}
		if shared {
			keys = append(keys, fk)
		}
	}
	slices.Sort(keys)
	return keys
}

func multiFloat(objs []*sceneformat.ObjectDoc, read func(*sceneformat.ObjectDoc) float64) (float64, bool) {
	if len(objs) == 0 {
		return 0, false
	}
	first := read(objs[0])
	for _, o := range objs[1:] {
		if read(o) != first {
			return 0, true
		}
	}
	return first, false
}

func multiBool(objs []*sceneformat.ObjectDoc, read func(*sceneformat.ObjectDoc) bool) (bool, bool) {
	if len(objs) == 0 {
		return false, false
	}
	first := read(objs[0])
	for _, o := range objs[1:] {
		if read(o) != first {
			return false, true
		}
	}
	return first, false
}

func multiString(objs []*sceneformat.ObjectDoc, read func(*sceneformat.ObjectDoc) string) (string, bool) {
	if len(objs) == 0 {
		return "", false
	}
	first := read(objs[0])
	for _, o := range objs[1:] {
		if read(o) != first {
			return "", true
		}
	}
	return first, false
}

func multiValuesEqual(a, b any) bool {
	if fa, okA := anyToFloat64(a); okA {
		if fb, okB := anyToFloat64(b); okB {
			return fa == fb
		}
	}
	return reflect.DeepEqual(a, b)
}

func multiAny(objs []*sceneformat.ObjectDoc, read func(*sceneformat.ObjectDoc) any) (any, bool) {
	if len(objs) == 0 {
		return nil, false
	}
	first := read(objs[0])
	for _, o := range objs[1:] {
		if !multiValuesEqual(read(o), first) {
			return nil, true
		}
	}
	return first, false
}

// multiInspectorFieldsView renders the inspector for a multi-object selection:
// header showing "N selected", note naming the shared sections, and mixed-aware controls
// that write every selected object in one undo step.
func multiInspectorFieldsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], ids []string) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	liveObjs := func() []*sceneformat.ObjectDoc {
		_ = rev.Get()
		out := make([]*sceneformat.ObjectDoc, 0, len(ids))
		for _, id := range ids {
			if o, _ := ed.FindResolvedObject(id); o != nil {
				out = append(out, o)
			}
		}
		return out
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
	describedRow := func(label, labelID, tip string, control core.Node) core.Node {
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				withTooltip(core.Text{
					TestID:  labelID,
					Content: label,
					Style: core.Style{
						Width:    core.Pt(76),
						FontSize: 12,
						Color:    colors.Text(),
					},
				}, tip),
				core.View{
					Style:    core.Style{FlexGrow: 1},
					Children: []core.Node{control},
				},
			},
		}
	}
	fieldRow := func(label string, control core.Node) core.Node {
		return describedRow(label, "", "", control)
	}

	objs := liveObjs()
	hasSprite := allHaveSprite(objs)
	sharedProps := sharedPropertyKeys(objs)
	sharedComps := sharedComponentNames(objs)

	sharedSections := []string{"Transform"}
	if hasSprite {
		sharedSections = append(sharedSections, "Sprite")
	}
	if len(sharedProps) > 0 {
		sharedSections = append(sharedSections, "Properties")
	}
	sharedSections = append(sharedSections, sharedComps...)

	var multiNote string
	if len(sharedSections) == 1 {
		multiNote = ed.T("inspector.multiNoteOnlyTransform", l10n.Param("count", len(ids)))
	} else {
		multiNote = ed.T("inspector.multiNoteShared",
			l10n.Param("count", len(ids)),
			l10n.Param("sections", strings.Join(sharedSections, ", ")),
		)
	}

	alphaMin, alphaMax := 0.0, 1.0
	depthMin := 0.0
	nodes := []core.Node{
		core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Gap:            6,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Gap: 2},
					Children: []core.Node{
						core.Text{
							TestID:  "inspector-selection-count",
							Content: ed.T("inspector.selectedCount", l10n.Param("count", len(ids))),
							Style: core.Style{
								FontSize:   13,
								FontWeight: 700,
								Color:      colors.Text(),
							},
						},
						ui.Badge{
							Text:    strings.Join(ids[:min(len(ids), 3)], ", "),
							Variant: ui.BadgeNeutral,
						},
					},
				},
				ui.Button{
					TestID:   "inspector-delete",
					Text:     ed.T("inspector.delete"),
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonOutline,
					Disabled: disabled,
					OnPress: func() {
						ed.report(ed.DeleteSelected())
					},
				},
			},
		},
		core.Text{
			TestID:  "inspector-multi-note",
			Content: multiNote,
			Style: core.Style{
				FontSize: 11,
				Color:    colors.Muted(),
			},
		},
		fieldRow(ed.T("inspector.name"), multiTextField(cx, ed, rev, "inspector-name", ed.T("inspector.objectName"), disabled,
			func() (string, bool) {
				return multiString(liveObjs(), func(o *sceneformat.ObjectDoc) string { return o.Name })
			},
			func(s string) { ed.report(ed.SetSelectedName(s)) },
		)),

		sectionHeader(ed.T("inspector.section.transform")),
		fieldRow("X", multiNumberField(cx, ed, rev, "inspector-x", "X", false, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.X) })
			},
			func(v float64) { ed.report(ed.SetSelectedX(float32(v))) },
		)),
		fieldRow("Y", multiNumberField(cx, ed, rev, "inspector-y", "Y", false, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.Y) })
			},
			func(v float64) { ed.report(ed.SetSelectedY(float32(v))) },
		)),
		fieldRow(ed.T("inspector.rotation"), multiNumberField(cx, ed, rev, "inspector-rotation", ed.T("inspector.rotation"), false, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.Rotation) })
			},
			func(v float64) { ed.report(ed.SetSelectedRotation(float32(v))) },
		)),
		fieldRow(ed.T("inspector.scaleX"), multiNumberField(cx, ed, rev, "inspector-scale-x", ed.T("inspector.scaleX"), false, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.EffectiveScaleX()) })
			},
			func(v float64) { ed.report(ed.SetSelectedScaleX(float32(v))) },
		)),
		fieldRow(ed.T("inspector.scaleY"), multiNumberField(cx, ed, rev, "inspector-scale-y", ed.T("inspector.scaleY"), false, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.EffectiveScaleY()) })
			},
			func(v float64) { ed.report(ed.SetSelectedScaleY(float32(v))) },
		)),
		fieldRow(ed.T("inspector.alpha"), multiNumberField(cx, ed, rev, "inspector-alpha", ed.T("inspector.alpha"), false, &alphaMin, &alphaMax, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.EffectiveAlpha()) })
			},
			func(v float64) { ed.report(ed.SetSelectedAlpha(float32(v))) },
		)),
		fieldRow("Z", multiNumberField(cx, ed, rev, "inspector-z", "Z", true, nil, nil, disabled,
			func() (float64, bool) {
				return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.Z) })
			},
			func(v float64) { ed.report(ed.SetSelectedZ(int(math.Round(v)))) },
		)),
	}

	if ed.ViewMode == ViewMode3D {
		nodes = append(nodes,
			sectionHeader(ed.T("inspector.section.transform3d")),
			fieldRow(ed.T("inspector.posZ"), multiNumberField(cx, ed, rev, "inspector-pos-z", ed.T("inspector.posZ"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.PosZ) })
				},
				func(v float64) { ed.report(ed.SetSelectedPosZ(float32(v))) },
			)),
			fieldRow(ed.T("inspector.rotX"), multiNumberField(cx, ed, rev, "inspector-rot-x", ed.T("inspector.rotX"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.RotX) })
				},
				func(v float64) { ed.report(ed.SetSelectedRotX(float32(v))) },
			)),
			fieldRow(ed.T("inspector.rotY"), multiNumberField(cx, ed, rev, "inspector-rot-y", ed.T("inspector.rotY"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.RotY) })
				},
				func(v float64) { ed.report(ed.SetSelectedRotY(float32(v))) },
			)),
			fieldRow(ed.T("inspector.rotZ"), multiNumberField(cx, ed, rev, "inspector-rot-z", ed.T("inspector.rotZ"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.RotZ) })
				},
				func(v float64) { ed.report(ed.SetSelectedRotZ(float32(v))) },
			)),
			fieldRow(ed.T("inspector.scaleZ"), multiNumberField(cx, ed, rev, "inspector-scale-z", ed.T("inspector.scaleZ"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.EffectiveScaleZ()) })
				},
				func(v float64) { ed.report(ed.SetSelectedScaleZ(float32(v))) },
			)),
			fieldRow(ed.T("inspector.depth"), multiNumberField(cx, ed, rev, "inspector-depth", ed.T("inspector.depth"), false, &depthMin, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.Depth) })
				},
				func(v float64) { ed.report(ed.SetSelectedDepth(float32(v))) },
			)),
			fieldRow(ed.T("inspector.mesh"), multiSelectField("inspector-mesh", ed.T("inspector.mesh"), ed.T("inspector.mixed"), rev, disabled, meshOptions(ed),
				func() (string, bool) {
					return multiString(liveObjs(), func(o *sceneformat.ObjectDoc) string { return o.Mesh })
				},
				func(mesh string) { ed.report(ed.SetSelectedMesh(mesh)) },
			)),
		)
	}

	if hasSprite {
		nodes = append(nodes,
			sectionHeader(ed.T("inspector.section.sprite")),
			fieldRow(ed.T("inspector.originX"), multiNumberField(cx, ed, rev, "inspector-origin-x", ed.T("inspector.originX"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.OriginX) })
				},
				func(v float64) { ed.report(ed.SetSelectedOriginX(float32(v))) },
			)),
			fieldRow(ed.T("inspector.originY"), multiNumberField(cx, ed, rev, "inspector-origin-y", ed.T("inspector.originY"), false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 { return float64(o.OriginY) })
				},
				func(v float64) { ed.report(ed.SetSelectedOriginY(float32(v))) },
			)),
			fieldRow(ed.T("inspector.flip"), ui.HStack{Gap: 12, Children: []core.Node{
				multiBoolField("inspector-flip-x", "X", rev, disabled,
					func() (bool, bool) {
						return multiBool(liveObjs(), func(o *sceneformat.ObjectDoc) bool { return o.FlipX })
					},
					func(on bool) { ed.report(ed.SetSelectedFlipX(on)) },
				),
				multiBoolField("inspector-flip-y", "Y", rev, disabled,
					func() (bool, bool) {
						return multiBool(liveObjs(), func(o *sceneformat.ObjectDoc) bool { return o.FlipY })
					},
					func(on bool) { ed.report(ed.SetSelectedFlipY(on)) },
				),
			}}),
		)
	}

	if len(sharedProps) > 0 {
		nodes = append(nodes, sectionHeader(ed.T("inspector.section.properties")))
		for _, k := range sharedProps {
			key := k
			sample := objs[0].Properties[key]
			ctrl := multiTypedFieldControl(cx, ed, rev, "inspector-prop-"+key, key, sample, disabled,
				func() (any, bool) {
					return multiAny(liveObjs(), func(o *sceneformat.ObjectDoc) any { return o.Properties[key] })
				},
				func(val any) { ed.report(ed.SetSelectedProperty(key, val)) },
			)
			nodes = append(nodes, fieldRow(key, ctrl))
		}
	}

	if len(sharedComps) > 0 {
		nodes = append(nodes, sectionHeader(ed.T("inspector.section.components")))
		for _, compName := range sharedComps {
			cn := compName
			var compTitle core.Node = core.Text{
				TestID:  "inspector-comp-" + cn,
				Content: cn,
				Style: core.Style{
					FontSize:   12,
					FontWeight: 600,
					Color:      colors.Text(),
				},
			}
			if t, ok := ed.lookupComponentType(cn); ok {
				compTitle = withTooltip(compTitle, t.Doc)
			}
			nodes = append(nodes, compTitle)
			if t, ok := ed.lookupComponentType(cn); ok {
				for _, f := range t.Fields {
					field := f
					read := func() (any, bool) {
						return multiAny(liveObjs(), func(o *sceneformat.ObjectDoc) any {
							if cm, ok := o.Components[cn].(map[string]any); ok {
								if v, ok := cm[field.Name]; ok {
									return v
								}
							}
							return field.Default
						})
					}
					ctrl := multiRegisteredFieldControl(cx, ed, rev, "inspector-comp-"+cn+"-"+field.Name, cn+"."+field.Name, field, disabled, liveObjs, cn, read,
						func(val any) {
							ed.report(ed.SetSelectedComponentField(cn, field.Name, val))
						},
					)
					nodes = append(nodes, describedRow(field.Name, "inspector-comp-"+cn+"-"+field.Name+"-label", fieldTooltip(field), ctrl))
				}
				continue
			}
			for _, fieldKey := range sharedComponentFieldKeys(objs, cn) {
				fk := fieldKey
				var sample any
				if m, ok := objs[0].Components[cn].(map[string]any); ok {
					sample = m[fk]
				}
				ctrl := multiTypedFieldControl(cx, ed, rev, "inspector-comp-"+cn+"-"+fk, cn+"."+fk, sample, disabled,
					func() (any, bool) {
						return multiAny(liveObjs(), func(o *sceneformat.ObjectDoc) any {
							if cm, ok := o.Components[cn].(map[string]any); ok {
								return cm[fk]
							}
							return nil
						})
					},
					func(val any) { ed.report(ed.SetSelectedComponentField(cn, fk, val)) },
				)
				nodes = append(nodes, fieldRow(fk, ctrl))
			}
		}
	}

	return core.View{
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func multiNumberField(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	testID, label string,
	isInt bool,
	minVal, maxVal *float64,
	disabled func() bool,
	read func() (float64, bool),
	commit func(float64),
) core.Node {
	draft := core.NewSignal(cx, "")
	focused := core.NewSignal(cx, false)
	dirty := false
	format := func(v float64) string {
		if isInt {
			r := math.Round(v)
			if r >= math.MinInt32 && r <= math.MaxInt32 {
				return strconv.Itoa(int(r))
			}
			return strconv.FormatFloat(r, 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	apply := func(text string) bool {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return false
		}
		v, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		if minVal != nil && v < *minVal {
			v = *minVal
		}
		if maxVal != nil && v > *maxVal {
			v = *maxVal
		}
		if isInt {
			v = math.Round(v)
			if v < math.MinInt32 || v > math.MaxInt32 {
				return false
			}
		}
		commit(v)
		return true
	}
	return ui.Input{
		TestID:       testID,
		Label:        label,
		KeyboardType: core.KeyboardDecimal,
		Disabled:     disabled,
		Value: func() string {
			_ = rev.Get()
			if focused.Get() {
				return draft.Get()
			}
			v, mixed := read()
			if mixed {
				return ""
			}
			return format(v)
		},
		PlaceholderFunc: func() string {
			_ = rev.Get()
			if _, mixed := read(); mixed {
				return ed.T("inspector.mixed")
			}
			return ""
		},
		OnFocus: func() {
			focused.Set(true)
			dirty = false
			v, mixed := read()
			if mixed {
				draft.Set("")
			} else {
				draft.Set(format(v))
			}
		},
		OnChange: func(text string) {
			focused.Set(true)
			dirty = true
			draft.Set(text)
		},
		OnSubmit: func(text string) {
			dirty = false
			focused.Set(false)
			if !apply(text) {
				draft.Set("")
			}
		},
		OnBlur: func() {
			if dirty {
				if !apply(draft.GetUntracked()) {
					draft.Set("")
				}
			}
			dirty = false
			focused.Set(false)
		},
	}
}

func multiTextField(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	testID, label string,
	disabled func() bool,
	read func() (string, bool),
	commit func(string),
) core.Node {
	draft := core.NewSignal(cx, "")
	focused := core.NewSignal(cx, false)
	dirty := false
	return ui.Input{
		TestID:   testID,
		Label:    label,
		Disabled: disabled,
		Value: func() string {
			_ = rev.Get()
			if focused.Get() {
				return draft.Get()
			}
			v, mixed := read()
			if mixed {
				return ""
			}
			return v
		},
		PlaceholderFunc: func() string {
			_ = rev.Get()
			if _, mixed := read(); mixed {
				return ed.T("inspector.mixed")
			}
			return ""
		},
		OnFocus: func() {
			focused.Set(true)
			dirty = false
			v, mixed := read()
			if mixed {
				draft.Set("")
			} else {
				draft.Set(v)
			}
		},
		OnChange: func(text string) {
			focused.Set(true)
			dirty = true
			draft.Set(text)
		},
		OnSubmit: func(text string) {
			dirty = false
			focused.Set(false)
			commit(text)
		},
		OnBlur: func() {
			if dirty {
				commit(draft.GetUntracked())
			}
			dirty = false
			focused.Set(false)
		},
	}
}

func multiBoolField(
	testID, label string,
	rev *core.Signal[uint64],
	disabled func() bool,
	read func() (bool, bool),
	commit func(bool),
) core.Node {
	return ui.Checkbox{
		TestID:   testID,
		Label:    label,
		Disabled: disabled,
		Indeterminate: func() bool {
			_ = rev.Get()
			_, mixed := read()
			return mixed
		},
		Checked: func() bool {
			_ = rev.Get()
			v, mixed := read()
			return !mixed && v
		},
		OnChange: commit,
	}
}

func multiSelectField(
	testID, label, mixedLabel string,
	rev *core.Signal[uint64],
	disabled func() bool,
	options []ui.Option[string],
	read func() (string, bool),
	commit func(string),
) core.Node {
	return ui.Select[string]{
		TestID:      testID,
		Label:       label,
		Placeholder: mixedLabel,
		Options:     options,
		Disabled:    disabled,
		Style:       core.Style{Width: core.Pct(100)},
		Value: func() string {
			_ = rev.Get()
			v, mixed := read()
			if mixed {
				return mixedSelectSentinel
			}
			return v
		},
		OnChange: commit,
	}
}

func multiTypedFieldControl(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	testID, label string,
	sample any,
	disabled func() bool,
	read func() (any, bool),
	commit func(any),
) core.Node {
	switch sample.(type) {
	case bool:
		return multiBoolField(testID, label, rev, disabled,
			func() (bool, bool) {
				v, mixed := read()
				if mixed {
					return false, true
				}
				b, _ := v.(bool)
				return b, false
			},
			func(b bool) { commit(b) },
		)
	case int, int64, int32:
		return multiNumberField(cx, ed, rev, testID, label, true, nil, nil, disabled,
			func() (float64, bool) {
				v, mixed := read()
				if mixed {
					return 0, true
				}
				n, _ := anyToFloat64(v)
				return n, false
			},
			func(v float64) { commit(int(math.Round(v))) },
		)
	case float64, float32:
		return multiNumberField(cx, ed, rev, testID, label, false, nil, nil, disabled,
			func() (float64, bool) {
				v, mixed := read()
				if mixed {
					return 0, true
				}
				n, _ := anyToFloat64(v)
				return n, false
			},
			func(v float64) { commit(v) },
		)
	default:
		return multiTextField(cx, ed, rev, testID, label, disabled,
			func() (string, bool) {
				v, mixed := read()
				if mixed {
					return "", true
				}
				if v == nil {
					return "", false
				}
				return fmt.Sprint(v), false
			},
			func(s string) { commit(s) },
		)
	}
}

func multiRegisteredFieldControl(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	testID, label string,
	f sceneformat.Field,
	disabled func() bool,
	liveObjs func() []*sceneformat.ObjectDoc,
	compName string,
	read func() (any, bool),
	commit func(any),
) core.Node {
	switch f.Kind {
	case sceneformat.FieldBool:
		return multiBoolField(testID, label, rev, disabled,
			func() (bool, bool) {
				v, mixed := read()
				if mixed {
					return false, true
				}
				b, _ := v.(bool)
				return b, false
			},
			func(b bool) { commit(b) },
		)
	case sceneformat.FieldInt:
		return multiNumberField(cx, ed, rev, testID, label, true, f.Min, f.Max, disabled,
			func() (float64, bool) {
				v, mixed := read()
				if mixed {
					return 0, true
				}
				n, _ := anyToFloat64(v)
				return n, false
			},
			func(v float64) { commit(int(math.Round(v))) },
		)
	case sceneformat.FieldFloat:
		return multiNumberField(cx, ed, rev, testID, label, false, f.Min, f.Max, disabled,
			func() (float64, bool) {
				v, mixed := read()
				if mixed {
					return 0, true
				}
				n, _ := anyToFloat64(v)
				return n, false
			},
			func(v float64) { commit(v) },
		)
	case sceneformat.FieldStringList:
		return multiTextField(cx, ed, rev, testID, label, disabled,
			func() (string, bool) {
				v, mixed := read()
				if mixed {
					return "", true
				}
				return strings.Join(stringSliceFromAny(v), ", "), false
			},
			func(s string) { commit(parseStringListInput(s)) },
		)
	case sceneformat.FieldPointList:
		return multiTextField(cx, ed, rev, testID, label, disabled,
			func() (string, bool) {
				v, mixed := read()
				if mixed {
					return "", true
				}
				return formatPointListInput(pointListFromAny(v)), false
			},
			func(s string) {
				if pts, ok := parsePointListInput(s); ok {
					commit(pts)
				}
			},
		)
	case sceneformat.FieldPoint:
		sub := func(suffix, fl string, getCoord func(sceneformat.Point) float64, setCoord func(sceneformat.Point, float64) sceneformat.Point) core.Node {
			return multiNumberField(cx, ed, rev, testID+"-"+suffix, label+" "+fl, false, nil, nil, disabled,
				func() (float64, bool) {
					return multiFloat(liveObjs(), func(o *sceneformat.ObjectDoc) float64 {
						var raw any = f.Default
						if cm, ok := o.Components[compName].(map[string]any); ok {
							if v, ok := cm[f.Name]; ok {
								raw = v
							}
						}
						p, _ := sceneformat.ParsePoint(raw)
						return getCoord(p)
					})
				},
				func(v float64) {
					ed.report(ed.EditSelected("Set "+compName+"."+f.Name+"."+suffix+" on selected objects", func(id string) error {
						o, _ := ed.FindResolvedObject(id)
						if o == nil {
							return fmt.Errorf("editor: object %q not found", id)
						}
						var raw any = f.Default
						if cm, ok := o.Components[compName].(map[string]any); ok {
							if cur, ok := cm[f.Name]; ok {
								raw = cur
							}
						}
						p, _ := sceneformat.ParsePoint(raw)
						return ed.SetComponentField(id, compName, f.Name, setCoord(p, v))
					}))
				},
			)
		}
		return ui.HStack{Gap: 4, Children: []core.Node{
			sub("x", "X", func(p sceneformat.Point) float64 { return p.X }, func(p sceneformat.Point, v float64) sceneformat.Point { p.X = v; return p }),
			sub("y", "Y", func(p sceneformat.Point) float64 { return p.Y }, func(p sceneformat.Point, v float64) sceneformat.Point { p.Y = v; return p }),
		}}
	default:
		if len(f.Enum) > 0 {
			opts := make([]ui.Option[string], len(f.Enum))
			for i, v := range f.Enum {
				opts[i] = ui.Option[string]{Value: v, Label: v}
			}
			return multiSelectField(testID, label, ed.T("inspector.mixed"), rev, disabled, opts,
				func() (string, bool) {
					v, mixed := read()
					if mixed {
						return "", true
					}
					s, _ := v.(string)
					return s, false
				},
				func(s string) { commit(s) },
			)
		}
		return multiTextField(cx, ed, rev, testID, label, disabled,
			func() (string, bool) {
				v, mixed := read()
				if mixed {
					return "", true
				}
				s, _ := v.(string)
				return s, false
			},
			func(s string) { commit(s) },
		)
	}
}

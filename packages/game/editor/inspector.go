package editor

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// SetProperty sets a typed custom property on objectID through History.
// Supported value types include string, int, float64, float32, and bool.
func (e *Editor) SetProperty(objectID, key string, value any) error {
	if e.playing {
		return ErrPlaying
	}
	if key == "" {
		return fmt.Errorf("editor: property key must not be empty")
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}

	oldVal, existed := obj.Properties[key]
	if existed && reflect.DeepEqual(oldVal, value) {
		return nil
	}

	e.History.Execute(&setPropertyCmd{
		ed:        e,
		objectID:  objectID,
		key:       key,
		wasNilMap: obj.Properties == nil,
		existed:   existed,
		oldVal:    cloneAny(oldVal),
		newVal:    cloneAny(value),
	})
	return nil
}

// DeleteProperty removes key from objectID's Properties map through History.
func (e *Editor) DeleteProperty(objectID, key string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	oldVal, existed := obj.Properties[key]
	if !existed {
		return nil
	}
	e.History.Execute(&deletePropertyCmd{
		ed:       e,
		objectID: objectID,
		key:      key,
		oldVal:   cloneAny(oldVal),
	})
	return nil
}

// SetComponentField sets a typed field inside objectID.Components[componentName][field] through History. With a component registry set, or for a built-in physics component (sceneformat.PhysicsComponentRegistry) without one, the field must be registered and value of its kind and within its range; it is stored normalized to bool, int, float64 or string.
func (e *Editor) SetComponentField(objectID, componentName, field string, value any) error {
	if e.playing {
		return ErrPlaying
	}
	if componentName == "" || field == "" {
		return fmt.Errorf("editor: component name and field must not be empty")
	}
	reg := e.components
	if _, builtin := builtinEditorComponents.Lookup(componentName); reg == nil && builtin {
		// Without a registry components are free-form, but the built-in components (physics + animator) keep their schema, as the inspector shows it (lookupComponentType).
		reg = builtinEditorComponents
	}
	if reg != nil {
		norm, err := reg.CheckField(componentName, field, value)
		if err != nil {
			var ce *sceneformat.ComponentError
			if errors.As(err, &ce) {
				ce.Object = objectID
			}
			return err
		}
		value = norm
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}

	var compMap map[string]any
	compExisted := false
	if obj.Components != nil {
		if raw, ok := obj.Components[componentName]; ok {
			compExisted = true
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("editor: component %q on %q is not a map", componentName, objectID)
			}
			compMap = m
		}
	}

	var oldVal any
	fieldExisted := false
	if compMap != nil {
		oldVal, fieldExisted = compMap[field]
		if fieldExisted && reflect.DeepEqual(oldVal, value) {
			return nil
		}
	}

	e.History.Execute(&setComponentFieldCmd{
		ed:          e,
		objectID:    objectID,
		component:   componentName,
		field:       field,
		wasNilComps: obj.Components == nil,
		compExisted: compExisted,
		existed:     fieldExisted,
		oldVal:      cloneAny(oldVal),
		newVal:      cloneAny(value),
	})
	return nil
}

type setPropertyCmd struct {
	ed       *Editor
	objectID string
	key      string
	// label names the step, "Set property <key>" when empty.
	label string
	// status is the status bar's message once the step is done, "Updated <key>" when empty.
	status    string
	wasNilMap bool
	existed   bool
	oldVal    any
	newVal    any
}

func (c *setPropertyCmd) Label() string {
	if c.label != "" {
		return c.label
	}
	return "Set property " + c.key
}

func (c *setPropertyCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}
	obj.Properties[c.key] = cloneAny(c.newVal)
	c.ed.Status = "Updated " + c.key
	if c.status != "" {
		c.ed.Status = c.status
	}
}

func (c *setPropertyCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if !c.existed {
		delete(obj.Properties, c.key)
		if c.wasNilMap && len(obj.Properties) == 0 {
			obj.Properties = nil
		}
		return
	}
	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}
	obj.Properties[c.key] = cloneAny(c.oldVal)
}

type deletePropertyCmd struct {
	ed       *Editor
	objectID string
	key      string
	oldVal   any
}

func (c *deletePropertyCmd) Label() string { return "Delete property " + c.key }

func (c *deletePropertyCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil || obj.Properties == nil {
		return
	}
	delete(obj.Properties, c.key)
	if len(obj.Properties) == 0 {
		obj.Properties = nil
	}
	c.ed.Status = "Deleted property " + c.key
}

func (c *deletePropertyCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}
	obj.Properties[c.key] = cloneAny(c.oldVal)
}

type setComponentFieldCmd struct {
	ed          *Editor
	objectID    string
	component   string
	field       string
	wasNilComps bool
	compExisted bool
	existed     bool
	oldVal      any
	newVal      any
}

func (c *setComponentFieldCmd) Label() string {
	return "Set " + c.component + "." + c.field
}

func (c *setComponentFieldCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	m, ok := obj.Components[c.component].(map[string]any)
	if !ok || m == nil {
		m = make(map[string]any)
		obj.Components[c.component] = m
	}
	m[c.field] = cloneAny(c.newVal)
	c.ed.Status = "Updated " + c.component + "." + c.field
}

func (c *setComponentFieldCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil || obj.Components == nil {
		return
	}
	if !c.compExisted {
		delete(obj.Components, c.component)
		if c.wasNilComps && len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}
	m, ok := obj.Components[c.component].(map[string]any)
	if !ok || m == nil {
		return
	}
	if !c.existed {
		delete(m, c.field)
		return
	}
	m[c.field] = cloneAny(c.oldVal)
}

var builtinEditorComponents = func() *sceneformat.ComponentRegistry {
	reg := sceneformat.PhysicsComponentRegistry()
	_ = sceneformat.RegisterAnimatorComponent(reg)
	return reg
}()

// lookupComponentType returns the schema of cn from e.components when set, or from the built-in component registry (physics + animator) when e.components is nil.
func (e *Editor) lookupComponentType(cn string) (sceneformat.ComponentType, bool) {
	if e != nil && e.components != nil {
		if ct, ok := e.components.Lookup(cn); ok {
			return ct, true
		}
	}
	return builtinEditorComponents.Lookup(cn)
}

// inspectorSchemaKey computes a stable signature of either the selected asset file
// (when inspecting import settings), the selected object ID and its
// property/component field names + kinds, or the active layer (when no object is selected)
// so InspectorView only remounts controls when selection or field layout changes
// (preserving input focus during value edits).
func inspectorSchemaKey(ed *Editor) string {
	if rel, kind := ed.inspectedImportAsset(); rel != "" {
		return "asset:" + string(kind) + ":" + rel
	}
	obj, layer := ed.SelectedObject()
	if obj == nil {
		if ed != nil && ed.inspectLayer {
			if li := ed.layerIndex(ed.ActiveLayer); li >= 0 {
				l := &ed.Doc.Layers[li]
				space := l.EffectiveSpace()
				inherited := ed.inheritedLayers()[l.Name]
				if inherited {
					if eff := ed.effectiveLayers(); len(eff) > 0 {
						if i := slices.IndexFunc(eff, func(el sceneformat.LayerDoc) bool { return el.Name == l.Name }); i >= 0 {
							space = eff[i].EffectiveSpace()
						}
					}
					return "L:" + l.Name + ":" + string(space) + ":inherited"
				}
				return "L:" + l.Name + ":" + string(space)
			}
		}
		if ed != nil && ed.Doc != nil {
			return "scene"
		}
		return ""
	}
	var b strings.Builder
	b.WriteString("O:")
	b.WriteString(ed.ViewMode.String())
	b.WriteByte(':')
	b.WriteString(layer)
	b.WriteByte(':')
	b.WriteString(obj.ID)

	if len(obj.Properties) > 0 {
		keys := sortedMapKeys(obj.Properties)
		b.WriteString("|P:")
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(valueKindTag(obj.Properties[k]))
			b.WriteByte(',')
		}
	}
	if len(obj.Components) > 0 {
		compNames := sortedMapKeys(obj.Components)
		b.WriteString("|C:")
		for _, cn := range compNames {
			b.WriteString(cn)
			b.WriteByte('{')
			if _, plugin := ed.pluginInspector(cn); plugin != "" {
				// A plugin's inspector follows the component's fields itself, so only a change of the plugin drawing it remounts it.
				b.WriteString("plugin\x00")
				b.WriteString(plugin)
				b.WriteByte(0)
			} else if t, ok := ed.lookupComponentType(cn); ok {
				// A registered component always shows every field, so setting an unset one must not remount its controls.
				b.WriteString("registered:")
				b.WriteString(strings.Join(t.FieldNames(), ","))
			} else if m, ok := obj.Components[cn].(map[string]any); ok {
				for _, fk := range sortedMapKeys(m) {
					b.WriteString(fk)
					b.WriteByte('=')
					b.WriteString(valueKindTag(m[fk]))
					b.WriteByte(',')
				}
			}
			b.WriteByte('}')
		}
	}
	return b.String()
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func valueKindTag(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case int, int64, int32:
		return "int"
	case float64, float32:
		return "float"
	case []string, []any:
		return "stringlist"
	default:
		return "string"
	}
}

// InspectorView renders the right-side Property Inspector panel for the selected asset
// import settings, the selected object, the selected layer (parallax/repeat settings), or
// the Scene Settings inspector (name, virtual size, background color and camera) when
// nothing else is selected. A component that a plugin which is on has an Inspector for is
// drawn by that inspector instead of the generic fields.
func InspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	schema := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return inspectorSchemaKey(ed)
	})

	return core.View{
		TestID: "editor-inspector",
		Style: core.Style{
			Width:       core.Pt(272),
			Background:  colors.Inset(),
			Borders:     core.Edges{Left: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(10),
			Gap:         8,
		},
		Children: []core.Node{
			core.Text{
				Content: "Inspector",
				Style: core.Style{
					FontSize:   13,
					FontWeight: 700,
					Color:      colors.Text(),
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow: 1,
				},
				ContentStyle: core.Style{
					Gap: 10,
				},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						key := schema.Get()
						if key == "" {
							return core.Text{
								TestID:  "inspector-empty",
								Content: "No object selected",
								Style: core.Style{
									FontSize: 12,
									Color:    colors.Muted(),
								},
							}
						}
						if strings.HasPrefix(key, "asset:") {
							if rel, kind := ed.inspectedImportAsset(); rel != "" {
								return assetImportInspectorView(cx, ed, rev, rel, kind)
							}
							return nil
						}
						if strings.HasPrefix(key, "L:") {
							if li := ed.layerIndex(ed.ActiveLayer); li >= 0 {
								return layerInspectorView(cx, ed, rev, ed.Doc.Layers[li].Name)
							}
							return nil
						}
						if key == "scene" {
							return sceneInspectorView(cx, ed, rev)
						}
						obj, layerName := ed.SelectedObject()
						if obj == nil {
							return nil
						}
						return inspectorFieldsView(cx, ed, rev, obj.ID, layerName)
					}),
				},
			},
		},
	}
}

func layerInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], layerName string) core.Node {
	colors := ui.ColorsOf(cx)
	isInherited := func() bool {
		_ = rev.Get()
		return ed.inheritedLayers()[layerName]
	}
	layer := func() *sceneformat.LayerDoc {
		_ = rev.Get()
		if ed.inheritedLayers()[layerName] {
			eff := ed.effectiveLayers()
			if i := slices.IndexFunc(eff, func(el sceneformat.LayerDoc) bool { return el.Name == layerName }); i >= 0 {
				return &eff[i]
			}
		}
		if li := ed.layerIndex(layerName); li >= 0 {
			return &ed.Doc.Layers[li]
		}
		return nil
	}
	isScreen := func() bool {
		l := layer()
		return l != nil && l.EffectiveSpace() == sceneformat.SpaceScreen
	}
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || isScreen() || isInherited()
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
					Style:    core.Style{FlexGrow: 1},
					Children: []core.Node{control},
				},
			},
		}
	}

	inheritedNow := ed.inheritedLayers()[layerName]
	spaceText := string(sceneformat.SpaceWorld)
	if inheritedNow {
		if eff := ed.effectiveLayers(); len(eff) > 0 {
			if i := slices.IndexFunc(eff, func(el sceneformat.LayerDoc) bool { return el.Name == layerName }); i >= 0 {
				spaceText = string(eff[i].EffectiveSpace())
			}
		}
	} else if li := ed.layerIndex(layerName); li >= 0 {
		spaceText = string(ed.Doc.Layers[li].EffectiveSpace())
	}
	formatFloat := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

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
							TestID:  "inspector-layer-name",
							Content: layerName,
							Style: core.Style{
								FontSize:   13,
								FontWeight: 700,
								Color:      colors.Text(),
							},
						},
						ui.Badge{
							Text:    "layer · " + spaceText,
							Variant: ui.BadgeNeutral,
						},
					},
				},
			},
		},

		sectionHeader("PARALLAX"),
		fieldRow("Parallax X", ui.NumberInput{
			TestID:   "inspector-layer-parallax-x",
			Label:    "Parallax X",
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				if l := layer(); l != nil {
					return float64(l.EffectiveParallaxX())
				}
				return 1
			},
			OnChange: func(v float64) {
				if li := ed.layerIndex(layerName); li >= 0 {
					l := &ed.Doc.Layers[li]
					ed.report(ed.SetLayerParallax(layerName, float32(v), l.EffectiveParallaxY()))
				}
			},
		}),
		fieldRow("Parallax Y", ui.NumberInput{
			TestID:   "inspector-layer-parallax-y",
			Label:    "Parallax Y",
			Step:     0.1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				if l := layer(); l != nil {
					return float64(l.EffectiveParallaxY())
				}
				return 1
			},
			OnChange: func(v float64) {
				if li := ed.layerIndex(layerName); li >= 0 {
					l := &ed.Doc.Layers[li]
					ed.report(ed.SetLayerParallax(layerName, l.EffectiveParallaxX(), float32(v)))
				}
			},
		}),

		sectionHeader("REPEAT"),
		fieldRow("Repeat", ui.HStack{
			Gap: 12,
			Children: []core.Node{
				ui.Checkbox{
					TestID:   "inspector-layer-repeat-x",
					Label:    "X",
					Disabled: disabled,
					Checked: func() bool {
						l := layer()
						return l != nil && l.RepeatX
					},
					OnChange: func(on bool) {
						if li := ed.layerIndex(layerName); li >= 0 {
							l := &ed.Doc.Layers[li]
							ed.report(ed.SetLayerRepeat(layerName, on, l.RepeatY, l.RepeatWidth, l.RepeatHeight))
						}
					},
				},
				ui.Checkbox{
					TestID:   "inspector-layer-repeat-y",
					Label:    "Y",
					Disabled: disabled,
					Checked: func() bool {
						l := layer()
						return l != nil && l.RepeatY
					},
					OnChange: func(on bool) {
						if li := ed.layerIndex(layerName); li >= 0 {
							l := &ed.Doc.Layers[li]
							ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, on, l.RepeatWidth, l.RepeatHeight))
						}
					},
				},
			},
		}),
		fieldRow("Repeat W", ui.NumberInput{
			TestID:   "inspector-layer-repeat-width",
			Label:    "Repeat Width",
			Min:      0,
			Step:     1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				if l := layer(); l != nil {
					return float64(l.RepeatWidth)
				}
				return 0
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				if li := ed.layerIndex(layerName); li >= 0 {
					l := &ed.Doc.Layers[li]
					ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, l.RepeatY, float32(v), l.RepeatHeight))
				}
			},
		}),
		fieldRow("Repeat H", ui.NumberInput{
			TestID:   "inspector-layer-repeat-height",
			Label:    "Repeat Height",
			Min:      0,
			Step:     1,
			Format:   formatFloat,
			Disabled: disabled,
			Value: func() float64 {
				if l := layer(); l != nil {
					return float64(l.RepeatHeight)
				}
				return 0
			},
			OnChange: func(v float64) {
				if v < 0 {
					v = 0
				}
				if li := ed.layerIndex(layerName); li >= 0 {
					l := &ed.Doc.Layers[li]
					ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, l.RepeatY, l.RepeatWidth, float32(v)))
				}
			},
		}),
	}

	if spaceText == string(sceneformat.SpaceScreen) {
		nodes = append(nodes, core.Text{
			TestID:  "inspector-layer-screen-note",
			Content: "Screen-space layers ignore camera parallax and repeat.",
			Style: core.Style{
				FontSize: 11,
				Color:    colors.Muted(),
			},
		})
	} else if inheritedNow {
		nodes = append(nodes, core.Text{
			TestID:  "inspector-layer-inherited-note",
			Content: "Inherited from base scene; edit parallax and repeat there or under layerOverrides.",
			Style: core.Style{
				FontSize: 11,
				Color:    colors.Muted(),
			},
		})
	}

	return core.View{
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func inspectorFieldsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], objID, layerName string) core.Node {
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

	// describedRow lays out a label, with tip as its tooltip when non-empty, beside its control.
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
							TestID:  "inspector-object-id",
							Content: objID,
							Style: core.Style{
								FontSize:   13,
								FontWeight: 700,
								Color:      colors.Text(),
							},
						},
						ui.Badge{
							Text:    layerName,
							Variant: ui.BadgeNeutral,
						},
					},
				},
				ui.Button{
					TestID:   "inspector-delete",
					Text:     "Delete",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonOutline,
					Disabled: disabled,
					OnPress: func() {
						_ = ed.DeleteObject(objID)
					},
				},
			},
		},

		fieldRow("Name", ui.Input{
			TestID:   "inspector-name",
			Label:    "Object Name",
			Disabled: disabled,
			Value: func() string {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return o.Name
				}
				return ""
			},
			OnChange: func(text string) {
				_ = ed.SetObjectName(objID, text)
			},
		}),

		sectionHeader("TRANSFORM"),
		fieldRow("X", ui.NumberInput{
			TestID:   "inspector-x",
			Label:    "X",
			Step:     1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.X)
				}
				return 0
			},
			OnChange: func(v float64) {
				if o, _, _ := ed.FindObject(objID); o != nil {
					_ = ed.MoveObject(objID, float32(v), o.Y)
				}
			},
		}),
		fieldRow("Y", ui.NumberInput{
			TestID:   "inspector-y",
			Label:    "Y",
			Step:     1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.Y)
				}
				return 0
			},
			OnChange: func(v float64) {
				if o, _, _ := ed.FindObject(objID); o != nil {
					_ = ed.MoveObject(objID, o.X, float32(v))
				}
			},
		}),
		fieldRow("Rotation", ui.NumberInput{
			TestID:   "inspector-rotation",
			Label:    "Rotation",
			Step:     0.1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.Rotation)
				}
				return 0
			},
			OnChange: func(v float64) {
				_ = ed.RotateObject(objID, float32(v))
			},
		}),
		fieldRow("Scale X", ui.NumberInput{
			TestID:   "inspector-scale-x",
			Label:    "Scale X",
			Step:     0.1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.EffectiveScaleX())
				}
				return 1
			},
			OnChange: func(v float64) {
				if o, _, _ := ed.FindObject(objID); o != nil {
					_ = ed.ScaleObject(objID, float32(v), o.EffectiveScaleY())
				}
			},
		}),
		fieldRow("Scale Y", ui.NumberInput{
			TestID:   "inspector-scale-y",
			Label:    "Scale Y",
			Step:     0.1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.EffectiveScaleY())
				}
				return 1
			},
			OnChange: func(v float64) {
				if o, _, _ := ed.FindObject(objID); o != nil {
					_ = ed.ScaleObject(objID, o.EffectiveScaleX(), float32(v))
				}
			},
		}),
		fieldRow("Alpha", ui.NumberInput{
			TestID:   "inspector-alpha",
			Label:    "Alpha",
			Min:      0,
			Max:      1,
			Step:     0.05,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.EffectiveAlpha())
				}
				return 1
			},
			OnChange: func(v float64) {
				_ = ed.SetObjectAlpha(objID, float32(v))
			},
		}),
		fieldRow("Z", ui.NumberInput{
			TestID:   "inspector-z",
			Label:    "Z",
			Step:     1,
			Disabled: disabled,
			Value: func() float64 {
				_ = rev.Get()
				if o, _, _ := ed.FindObject(objID); o != nil {
					return float64(o.Z)
				}
				return 0
			},
			OnChange: func(v float64) {
				_ = ed.SetObjectZ(objID, int(math.Round(v)))
			},
		}),
	}

	if ed.ViewMode == ViewMode3D {
		nodes = append(nodes,
			sectionHeader("TRANSFORM 3D"),
			fieldRow("Pos Z", ui.NumberInput{
				TestID:   "inspector-pos-z",
				Label:    "Pos Z",
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.PosZ)
					}
					return 0
				},
				OnChange: func(v float64) {
					if o, _, _ := ed.FindObject(objID); o != nil {
						_ = ed.MoveObject3D(objID, o.X, o.Y, float32(v))
					}
				},
			}),
			fieldRow("Rot X", ui.NumberInput{
				TestID:   "inspector-rot-x",
				Label:    "Rot X",
				Step:     0.1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.RotX)
					}
					return 0
				},
				OnChange: func(v float64) {
					if o, _, _ := ed.FindObject(objID); o != nil {
						_ = ed.RotateObject3D(objID, float32(v), o.RotY, o.RotZ)
					}
				},
			}),
			fieldRow("Rot Y", ui.NumberInput{
				TestID:   "inspector-rot-y",
				Label:    "Rot Y",
				Step:     0.1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.RotY)
					}
					return 0
				},
				OnChange: func(v float64) {
					if o, _, _ := ed.FindObject(objID); o != nil {
						_ = ed.RotateObject3D(objID, o.RotX, float32(v), o.RotZ)
					}
				},
			}),
			fieldRow("Rot Z", ui.NumberInput{
				TestID:   "inspector-rot-z",
				Label:    "Rot Z",
				Step:     0.1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.RotZ)
					}
					return 0
				},
				OnChange: func(v float64) {
					if o, _, _ := ed.FindObject(objID); o != nil {
						_ = ed.RotateObject3D(objID, o.RotX, o.RotY, float32(v))
					}
				},
			}),
			fieldRow("Scale Z", ui.NumberInput{
				TestID:   "inspector-scale-z",
				Label:    "Scale Z",
				Step:     0.1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.EffectiveScaleZ())
					}
					return 1
				},
				OnChange: func(v float64) {
					if o, _, _ := ed.FindObject(objID); o != nil {
						_ = ed.ScaleObject3D(objID, o.EffectiveScaleX(), o.EffectiveScaleY(), float32(v))
					}
				},
			}),
			fieldRow("Depth", ui.NumberInput{
				TestID:   "inspector-depth",
				Label:    "Depth",
				Min:      0,
				Step:     1,
				Disabled: disabled,
				Value: func() float64 {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return float64(o.Depth)
					}
					return 0
				},
				OnChange: func(v float64) {
					_ = ed.SetObjectDepth(objID, float32(v))
				},
			}),
			fieldRow("Mesh", ui.Select[string]{
				TestID:      "inspector-mesh",
				Label:       "Mesh",
				Placeholder: "None",
				// An object that draws a glTF model or GridMap cannot also draw a primitive, which SetObjectMesh refuses.
				Disabled: func() bool {
					if disabled() {
						return true
					}
					o, _, _ := ed.FindObject(objID)
					return o != nil && (o.Model != "" || o.GridMap != "")
				},
				Options: meshOptions(),
				Value: func() string {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return o.Mesh
					}
					return ""
				},
				OnChange: func(mesh string) {
					if err := ed.SetObjectMesh(objID, mesh); err != nil {
						ed.Status = err.Error()
						ed.notify()
					}
				},
			}),
			fieldRow("Model", modelField(ed, rev, objID, disabled)),
			fieldRow("GridMap", ui.Select[string]{
				TestID:      "inspector-gridmap",
				Label:       "GridMap",
				Placeholder: "None",
				Searchable:  true,
				Disabled: func() bool {
					if disabled() {
						return true
					}
					o, _, _ := ed.FindObject(objID)
					return o != nil && (o.Mesh != "" || o.Model != "")
				},
				OptionsFunc: func() []ui.Option[string] {
					_ = rev.Get()
					cur := ""
					if o, _, _ := ed.FindObject(objID); o != nil {
						cur = o.GridMap
					}
					files := ed.projectGridMaps()
					opts := make([]ui.Option[string], 0, len(files)+1)
					listed := false
					for _, f := range files {
						listed = listed || f == cur
						opts = append(opts, ui.Option[string]{Value: f, Label: f})
					}
					if cur != "" && !listed {
						opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
					}
					return opts
				},
				Value: func() string {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return o.GridMap
					}
					return ""
				},
				OnChange: func(gm string) {
					if err := ed.SetObjectGridMap(objID, gm); err != nil {
						ed.Status = err.Error()
						ed.notify()
						return
					}
					if gm != "" {
						ed.autoOpenSceneGridMap()
					}
				},
				OnClear: func() {
					ed.report(ed.SetObjectGridMap(objID, ""))
				},
			}),
		)
	}

	nodes = append(nodes, sectionHeader("SPRITE"))
	nodes = append(nodes, spriteFields(ed, rev, objID, disabled, fieldRow)...)

	obj, _, _ := ed.FindObject(objID)
	nodes = append(nodes, sectionHeader("PROPERTIES"))
	if obj != nil {
		for _, k := range sortedMapKeys(obj.Properties) {
			key := k
			sample := obj.Properties[key]
			ctrl := typedFieldControl(
				"inspector-prop-"+key,
				key,
				sample,
				disabled,
				func() any {
					_ = rev.Get()
					if o, _, _ := ed.FindObject(objID); o != nil {
						return o.Properties[key]
					}
					return nil
				},
				func(val any) {
					_ = ed.SetProperty(objID, key, val)
				},
			)
			del := removeButton("inspector-prop-"+key+"-delete", "Delete property "+key, disabled, func() {
				ed.report(ed.DeleteProperty(objID, key))
			})
			nodes = append(nodes, fieldRow(key, withTrailing(ctrl, del)))
		}
	}
	nodes = append(nodes, addPropertyRow(cx, ed, objID, disabled))

	nodes = append(nodes, sectionHeader("COMPONENTS"))
	if obj != nil {
		for _, compName := range sortedMapKeys(obj.Components) {
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
			remove := removeButton("inspector-comp-"+cn+"-remove", "Remove component "+cn, disabled, func() {
				ed.report(ed.RemoveComponent(objID, cn))
			})
			header := withTrailing(compTitle, remove)
			if in, plugin := ed.pluginInspector(cn); in != nil {
				nodes = append(nodes, core.View{
					TestID:   "inspector-plugin-" + plugin + "-" + cn,
					Style:    core.Style{Gap: 8},
					Children: []core.Node{header, in.View(cx, ed, rev, objID)},
				})
				continue
			}
			nodes = append(nodes, header)
			if t, ok := ed.lookupComponentType(cn); ok {
				for _, f := range t.Fields {
					field := f
					get := func() any {
						_ = rev.Get()
						if o, _, _ := ed.FindObject(objID); o != nil {
							if cm, ok := o.Components[cn].(map[string]any); ok {
								if v, ok := cm[field.Name]; ok {
									return v
								}
							}
						}
						return field.Default
					}
					ctrl := registeredFieldControl("inspector-comp-"+cn+"-"+field.Name, cn+"."+field.Name, field, disabled, get, func(val any) {
						if err := ed.SetComponentField(objID, cn, field.Name, val); err != nil {
							ed.Status = err.Error()
							ed.notify()
						}
					})
					nodes = append(nodes, describedRow(field.Name, "inspector-comp-"+cn+"-"+field.Name+"-label", fieldTooltip(field), ctrl))
				}
				continue
			}
			m, ok := obj.Components[cn].(map[string]any)
			if !ok {
				continue
			}
			for _, fieldKey := range sortedMapKeys(m) {
				fk := fieldKey
				sample := m[fk]
				ctrl := typedFieldControl(
					"inspector-comp-"+cn+"-"+fk,
					cn+"."+fk,
					sample,
					disabled,
					func() any {
						_ = rev.Get()
						if o, _, _ := ed.FindObject(objID); o != nil && o.Components != nil {
							if cm, ok := o.Components[cn].(map[string]any); ok {
								return cm[fk]
							}
						}
						return nil
					},
					func(val any) {
						_ = ed.SetComponentField(objID, cn, fk, val)
					},
				)
				nodes = append(nodes, fieldRow(fk, ctrl))
			}
		}
	}
	nodes = append(nodes, addComponentControl(cx, ed, rev, objID, disabled))
	nodes = append(nodes, connectionsSection(cx, ed, rev, objID, disabled, sectionHeader, describedRow))

	return core.View{
		Style:    core.Style{Gap: 8},
		Children: nodes,
	}
}

func typedFieldControl(
	testID, label string,
	sample any,
	disabled func() bool,
	get func() any,
	set func(any),
) core.Node {
	switch sample.(type) {
	case bool:
		return ui.Checkbox{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
			Checked: func() bool {
				b, _ := get().(bool)
				return b
			},
			OnChange: func(checked bool) {
				set(checked)
			},
		}
	case int, int64, int32:
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Step:     1,
			Disabled: disabled,
			Value: func() float64 {
				switch n := get().(type) {
				case int:
					return float64(n)
				case int64:
					return float64(n)
				case int32:
					return float64(n)
				case float64:
					return n
				default:
					return 0
				}
			},
			OnChange: func(v float64) {
				set(int(math.Round(v)))
			},
		}
	case float64, float32:
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Step:     0.1,
			Disabled: disabled,
			Value: func() float64 {
				switch n := get().(type) {
				case float64:
					return n
				case float32:
					return float64(n)
				case int:
					return float64(n)
				default:
					return 0
				}
			},
			OnChange: func(v float64) {
				set(v)
			},
		}
	default:
		return ui.Input{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
			Value: func() string {
				if v := get(); v != nil {
					return fmt.Sprint(v)
				}
				return ""
			},
			OnChange: func(text string) {
				set(text)
			},
		}
	}
}

// withTooltip wraps trigger in a ui.Tooltip showing tip, or returns trigger unchanged when tip is empty.
func withTooltip(trigger core.Node, tip string) core.Node {
	if tip == "" {
		return trigger
	}
	return ui.Tooltip{Text: tip, Trigger: trigger}
}

// fieldTooltip describes a registered field for its inspector label: its documentation, kind, range and default.
func fieldTooltip(f sceneformat.Field) string {
	var b strings.Builder
	if f.Doc != "" {
		b.WriteString(f.Doc)
		b.WriteString("\n")
	}
	b.WriteString(f.Kind.String())
	switch {
	case f.Min != nil && f.Max != nil:
		fmt.Fprintf(&b, " in [%v, %v]", *f.Min, *f.Max)
	case f.Min != nil:
		fmt.Fprintf(&b, " >= %v", *f.Min)
	case f.Max != nil:
		fmt.Fprintf(&b, " <= %v", *f.Max)
	}
	fmt.Fprintf(&b, ", default %v", formatFieldValue(f.Default))
	return b.String()
}

func formatFieldValue(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	if ss, ok := v.([]string); ok {
		return "[" + strings.Join(ss, ", ") + "]"
	}
	return fmt.Sprint(v)
}

func stringSliceFromAny(v any) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, 0, len(xs))
		for _, elem := range xs {
			if s, ok := elem.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func parseStringListInput(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(text, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// registeredFieldControl builds the inspector control for a registered component field from its kind, bounding numeric inputs by its range.
func registeredFieldControl(
	testID, label string,
	f sceneformat.Field,
	disabled func() bool,
	get func() any,
	set func(any),
) core.Node {
	var lo, hi float64
	if f.Min != nil && f.Max != nil && *f.Max > *f.Min {
		lo, hi = *f.Min, *f.Max
	}
	clamp := func(v float64) float64 {
		if f.Min != nil && v < *f.Min {
			v = *f.Min
		}
		if f.Max != nil && v > *f.Max {
			v = *f.Max
		}
		return v
	}
	number := func() float64 {
		switch n := get().(type) {
		case int:
			return float64(n)
		case float64:
			return n
		default:
			return 0
		}
	}
	switch f.Kind {
	case sceneformat.FieldBool:
		return ui.Checkbox{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
			Checked: func() bool {
				b, _ := get().(bool)
				return b
			},
			OnChange: func(checked bool) { set(checked) },
		}
	case sceneformat.FieldInt:
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Min:      lo,
			Max:      hi,
			Step:     cmp.Or(f.Step, 1),
			Disabled: disabled,
			Value:    number,
			OnChange: func(v float64) { set(int(math.Round(clamp(v)))) },
		}
	case sceneformat.FieldFloat:
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Min:      lo,
			Max:      hi,
			Step:     cmp.Or(f.Step, 0.1),
			Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
			Disabled: disabled,
			Value:    number,
			OnChange: func(v float64) { set(clamp(v)) },
		}
	case sceneformat.FieldStringList:
		return ui.Input{
			TestID:      testID,
			Label:       label,
			Placeholder: "layer1, layer2",
			Disabled:    disabled,
			Value: func() string {
				return strings.Join(stringSliceFromAny(get()), ", ")
			},
			OnChange: func(text string) { set(parseStringListInput(text)) },
		}
	default:
		if len(f.Enum) > 0 {
			opts := make([]ui.Option[string], len(f.Enum))
			for i, v := range f.Enum {
				opts[i] = ui.Option[string]{Value: v, Label: v}
			}
			return ui.Select[string]{
				TestID:   testID,
				Label:    label,
				Options:  opts,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pct(100)},
				Value: func() string {
					s, _ := get().(string)
					return s
				},
				OnChange: func(v string) { set(v) },
			}
		}
		return ui.Input{
			TestID:   testID,
			Label:    label,
			Disabled: disabled,
			Value: func() string {
				s, _ := get().(string)
				return s
			},
			OnChange: func(text string) { set(text) },
		}
	}
}

// meshOptions are the choices of the inspector's Mesh field: none, then each built-in primitive sceneformat.ObjectDoc.Mesh accepts.
func meshOptions() []ui.Option[string] {
	opts := []ui.Option[string]{{Value: "", Label: "None"}}
	for _, m := range sceneformat.MeshPrimitives() {
		opts = append(opts, ui.Option[string]{Value: m, Label: m})
	}
	return opts
}

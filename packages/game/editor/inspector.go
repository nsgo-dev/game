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

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// SetProperty sets a typed custom property on objectID through History.
// Supported value types include string, int, float64, float32, and bool.
func (e *Editor) SetProperty(objectID, key string, value any) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("editor: property key must not be empty")
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set property "+key, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if oldVal, existed := resolved.Properties[key]; existed && reflect.DeepEqual(oldVal, value) {
					return nil
				}
				if ov.Properties == nil {
					ov.Properties = make(map[string]any)
				}
				ov.Properties[key] = cloneAny(value)
				return nil
			})
		}
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
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Delete property "+key, 2, func(ov *sceneformat.ObjectOverride, _, base *sceneformat.ObjectDoc) error {
				if base != nil && base.Properties != nil {
					if _, onBase := base.Properties[key]; onBase {
						return fmt.Errorf("editor: cannot delete inherited property %q on %q; revert its override or edit the source scene", key, objectID)
					}
				}
				if ov.Properties != nil {
					delete(ov.Properties, key)
				}
				return nil
			})
		}
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

// SetComponentField sets a typed field inside objectID.Components[componentName][field] through History. With a component registry set, or for a built-in physics or 2D lighting component without one, the field must be registered and value of its kind and within its range; it is stored normalized to bool, int, float64, string, []string, or [][2]float64.
func (e *Editor) SetComponentField(objectID, componentName, field string, value any) error {
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if componentName == "" || field == "" {
		return fmt.Errorf("editor: component name and field must not be empty")
	}
	reg := e.components
	if _, builtin := builtinEditorComponents.Lookup(componentName); reg == nil && builtin {
		// Without a registry components are free-form, but the built-in components (physics + animator + 2D lighting + skeletal) keep their schema, as the inspector shows it (lookupComponentType).
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
		if info := e.ObjectOverrideInfo(objectID); info.Origin != ObjectLocal {
			return e.editObjectOverride(objectID, "Set "+componentName+"."+field, 2, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				var curMap map[string]any
				if resolved.Components != nil {
					if raw, ok := resolved.Components[componentName]; ok {
						m, ok := raw.(map[string]any)
						if !ok {
							return fmt.Errorf("editor: component %q on %q is not a map", componentName, objectID)
						}
						curMap = m
					}
				}
				if curMap != nil {
					if oldVal, existed := curMap[field]; existed {
						if reflect.DeepEqual(oldVal, value) {
							return nil
						}
						if e.components != nil {
							if normOld, err := e.components.CheckField(componentName, field, oldVal); err == nil && reflect.DeepEqual(normOld, value) {
								return nil
							}
						}
					}
				}
				nextMap := cloneMap(curMap)
				if nextMap == nil {
					nextMap = make(map[string]any)
				}
				nextMap[field] = cloneAny(value)
				if ov.Components == nil {
					ov.Components = make(map[string]any)
				}
				ov.Components[componentName] = nextMap
				return nil
			})
		}
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
		if fieldExisted {
			if reflect.DeepEqual(oldVal, value) {
				return nil
			}
			if e.components != nil {
				if normOld, err := e.components.CheckField(componentName, field, oldVal); err == nil && reflect.DeepEqual(normOld, value) {
					return nil
				}
			}
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
	if err := sceneformat.RegisterAnimatorComponent(reg); err != nil {
		panic(err)
	}
	if err := sceneformat.RegisterLights2DComponents(reg); err != nil {
		panic(err)
	}
	if err := sceneformat.RegisterSkeletalComponent(reg); err != nil {
		panic(err)
	}
	if err := sceneformat.RegisterNavRegion2DComponent(reg); err != nil {
		panic(err)
	}
	return reg
}()

// lookupComponentType returns the schema of cn from e.components when set, or from the built-in component registry (physics + animator + 2D lighting + skeletal + 2D navigation).
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
	if shouldShowRemoteInspector(ed) {
		return "remote"
	}
	if rel, kind := ed.inspectedImportAsset(); rel != "" {
		return "asset:" + string(kind) + ":" + rel
	}
	tabPrefix := fmt.Sprintf("T%d:", ed.activeTabID())
	if ids := ed.SelectedIDs(); len(ids) > 1 {
		if k := multiInspectorSchemaKey(ed, ids); k != "" {
			return tabPrefix + k
		}
	}
	obj, layer := ed.SelectedObject()
	if obj == nil {
		if ed != nil && ed.SelectedRoomID() != "" {
			return "room"
		}
		if ed != nil && ed.inspectLayer {
			li := ed.layerIndex(ed.ActiveLayer)
			inherited := ed.inheritedLayers()[ed.ActiveLayer]
			if li >= 0 || inherited {
				var l *sceneformat.LayerDoc
				if li >= 0 {
					l = &ed.Doc.Layers[li]
				}
				space := sceneformat.SpaceWorld
				if l != nil {
					space = l.EffectiveSpace()
				}
				if inherited {
					if eff := ed.effectiveLayers(); len(eff) > 0 {
						if i := slices.IndexFunc(eff, func(el sceneformat.LayerDoc) bool { return el.Name == ed.ActiveLayer }); i >= 0 {
							space = eff[i].EffectiveSpace()
							if l == nil {
								c := eff[i]
								l = &c
							}
						}
					}
					return tabPrefix + "L:" + ed.ActiveLayer + ":" + string(space) + ":inherited:" + layerTilemapSchemaTag(ed, l)
				}
				return tabPrefix + "L:" + l.Name + ":" + string(space) + ":" + layerTilemapSchemaTag(ed, l)
			}
		}
		if ed != nil && ed.Doc != nil {
			if tsPath, tileID, ok := ed.inspectedTilesetTile(); ok {
				return tabPrefix + fmt.Sprintf("tile:%s:%d", tsPath, tileID)
			}
			return tabPrefix + "scene"
		}
		return ""
	}
	var b strings.Builder
	b.WriteString(tabPrefix)
	b.WriteString("O:")
	b.WriteString(ed.ViewMode.String())
	b.WriteByte(':')
	b.WriteString(ed.ObjectOrigin(obj.ID).String())
	b.WriteByte(':')
	b.WriteString(layer)
	b.WriteByte(':')
	b.WriteString(obj.ID)
	if obj.Light != nil {
		b.WriteString("|L:")
		b.WriteString(string(obj.Light.Kind))
	} else {
		b.WriteString("|L:none")
	}
	if obj.Camera != nil {
		b.WriteString("|C3:")
		b.WriteString(string(obj.Camera.EffectiveProjection()))
	} else {
		b.WriteString("|C3:none")
	}
	if obj.ReflectionProbe != nil {
		b.WriteString("|RP:1")
	} else {
		b.WriteString("|RP:0")
	}
	if obj.LightProbeGrid != nil {
		b.WriteString("|LP:1")
	} else {
		b.WriteString("|LP:0")
	}
	if obj.Scene != "" {
		b.WriteString("|S:")
		b.WriteString(obj.Scene)
	}

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
	case [][2]float64:
		return "pointlist"
	case sceneformat.Point:
		return "point"
	case sceneformat.Rect:
		return "rect"
	case sceneformat.Circle:
		return "circle"
	case []sceneformat.Point:
		return "polygon"
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
	return inspectorThemeScope{
		build: func(cx *core.Scope) core.Node {
			return renderInspectorView(cx, ed, rev)
		},
	}
}

func renderInspectorView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	schema := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return inspectorSchemaKey(ed)
	})

	scrubEscape := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.InspectorScrubbing()
		},
		Then: func(cx *core.Scope) core.Node {
			cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() {
				ed.CancelInspectorScrub()
			})
			return nil
		},
	}

	openYAMLBtn := core.Show{
		When: func() bool {
			_ = rev.Get()
			return !shouldShowRemoteInspector(ed) && ed.Doc != nil
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "inspector-open-yaml",
				Role:   core.RoleButton,
				Label:  ed.T("inspector.openYAML"),
				Style: core.Style{
					Direction:    core.Row,
					AlignItems:   core.AlignCenter,
					Height:       core.Pt(26),
					Padding:      core.Symmetric(0, 8),
					CornerRadius: 6,
					Gap:          5,
					Hover:        &core.Style{Background: specRaised},
					FocusVisible: specFocusRingStyle,
				},
				OnPress: func(core.PressEvent) {
					ed.report(ed.OpenSelectedObjectYAML())
				},
				Children: []core.Node{
					ui.Icon(lucide.FileCode, 13, func() core.Color { return specTextSecondary }),
					core.Text{
						Content:  ed.T("inspector.openYAML"),
						MaxLines: 1,
						Style: core.Style{
							FontSize: 11.5,
							Color:    specTextSecondary,
						},
					},
				},
			}
		},
	}

	return core.View{
		TestID: "editor-inspector",
		Style: dockedStyle(cx, core.Style{
			Width:       core.Pt(272),
			FlexGrow:    1,
			FlexBasis:   core.Pt(0),
			Background:  specPanel,
			Borders:     core.Edges{Left: 1},
			BorderColor: specLine,
			Gap:         0,
		}),
		Children: []core.Node{
			scrubEscape,
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return shouldShowPlayInspectorTabs(ed)
				},
				Then: func(cx *core.Scope) core.Node {
					return playInspectorTabBarView(cx, ed, rev)
				},
				Else: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{
							Direction:      core.Row,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifySpaceBetween,
							Height:         core.Pt(38),
							Padding:        core.Symmetric(0, 12),
							Borders:        core.Edges{Bottom: 1},
							BorderColor:    specLine,
							Gap:            8,
							FlexShrink:     0,
						},
						Children: []core.Node{
							core.Text{
								TestID: "inspector-title",
								ContentFunc: func() string {
									_ = rev.Get()
									if shouldShowRemoteInspector(ed) {
										return ed.T("inspector.remoteTitle")
									}
									return ed.T("inspector.title")
								},
								Style: core.Style{
									FontSize:   13,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextStrong,
								},
							},
							openYAMLBtn,
						},
					}
				},
			},
			ui.ScrollView{
				Style: core.Style{
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					MinHeight: core.Pt(0),
				},
				ContentStyle: core.Style{
					Padding: core.All(10),
					Gap:     10,
				},
				Children: []core.Node{
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return shouldShowPlayInspectorTabs(ed) && ed.PlayInspectorTab() == PlayTabAgent
						},
						Then: func(cx *core.Scope) core.Node {
							return playAgentPanelView(cx, ed, rev)
						},
						Else: func(cx *core.Scope) core.Node {
							return core.Dynamic(func(cx *core.Scope) core.Node {
								key := schema.Get()
								if key == "remote" {
									return remoteInspectorView(cx, ed, rev)
								}
								if key == "" {
									return core.Text{
										TestID:  "inspector-empty",
										Content: ed.T("inspector.empty"),
										Style: core.Style{
											FontSize: 12,
											Color:    specTextMuted,
										},
									}
								}
								if strings.HasPrefix(key, "asset:") {
									if rel, kind := ed.inspectedImportAsset(); rel != "" {
										return assetImportInspectorView(cx, ed, rev, rel, kind)
									}
									return nil
								}
								subKey := key
								if strings.HasPrefix(subKey, "T") {
									if _, after, ok := strings.Cut(subKey, ":"); ok {
										subKey = after
									}
								}
								if strings.HasPrefix(subKey, "L:") {
									if ed.layerIndex(ed.ActiveLayer) >= 0 || ed.inheritedLayers()[ed.ActiveLayer] {
										return layerInspectorView(cx, ed, rev, ed.ActiveLayer)
									}
									return nil
								}
								if subKey == "room" {
									if ed.SelectedRoomID() != "" {
										return roomInspectorView(cx, ed, rev)
									}
									return nil
								}
								if strings.HasPrefix(subKey, "tile:") {
									return tileInspectorView(cx, ed, rev)
								}
								if subKey == "scene" {
									return sceneInspectorView(cx, ed, rev)
								}
								if strings.HasPrefix(subKey, "M:") {
									if ids := ed.SelectedIDs(); len(ids) > 1 {
										return multiInspectorFieldsView(cx, ed, rev, ids)
									}
									return nil
								}
								obj, layerName := ed.SelectedObject()
								if obj == nil {
									return nil
								}
								return inspectorFieldsView(cx, ed, rev, obj.ID, layerName)
							})
						},
					},
				},
			},
			inspectorThreadFooterView(cx, ed, rev),
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
		return ed.Playing() || isScreen()
	}
	tilemapDisabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || isInherited()
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
	layerFieldsRow := func(fieldKeys []string, label string, control core.Node) core.Node {
		var labelPrefix core.Node
		var revertBtn core.Node
		if len(fieldKeys) > 0 {
			primaryKey := fieldKeys[0]
			hasOv := func() bool {
				_ = rev.Get()
				for _, fk := range fieldKeys {
					if ed.HasLayerOverride(layerName, fk) {
						return true
					}
				}
				return false
			}
			labelPrefix = core.Show{
				When: hasOv,
				Then: func(cx *core.Scope) core.Node {
					var parts []string
					for _, fk := range fieldKeys {
						if len(fieldKeys) == 1 || ed.HasLayerOverride(layerName, fk) {
							v := ed.LayerBaseValue(layerName, fk)
							if len(fieldKeys) > 1 {
								parts = append(parts, fk+"="+v)
							} else {
								parts = append(parts, v)
							}
						}
					}
					tip := ed.T("overrides.overridesSource", l10n.Param("value", strings.Join(parts, ", ")))
					return withTooltip(core.View{
						TestID: "inspector-layer-override-dot-" + primaryKey,
						Style: core.Style{
							Width:        core.Pt(6),
							Height:       core.Pt(6),
							CornerRadius: 3,
							Background:   colors.Warn(),
							FlexShrink:   0,
						},
					}, tip)
				},
			}
			revertBtn = core.Show{
				When: hasOv,
				Then: func(cx *core.Scope) core.Node {
					return ui.IconButton{
						TestID:   "inspector-layer-revert-" + primaryKey,
						Icon:     lucide.RotateCcw,
						Size:     20,
						IconSize: 12,
						Label:    ed.T("overrides.revertField", l10n.Param("field", label)),
						Disabled: func() bool { _ = rev.Get(); return ed.Playing() },
						OnPress: func() {
							if len(fieldKeys) == 1 {
								ed.report(ed.RevertLayerOverrideField(layerName, primaryKey))
								return
							}
							ed.report(ed.History.Group("Revert "+label+" on layer "+layerName, func() error {
								for _, fk := range fieldKeys {
									if ed.HasLayerOverride(layerName, fk) {
										if err := ed.RevertLayerOverrideField(layerName, fk); err != nil {
											return err
										}
									}
								}
								return nil
							}))
						},
					}
				},
			}
		}
		children := []core.Node{
			core.View{
				Style: core.Style{
					Width:      core.Pt(76),
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        4,
				},
				Children: []core.Node{
					labelPrefix,
					core.Text{
						Content: label,
						Style: core.Style{
							FontSize: 12,
							Color:    colors.Text(),
						},
					},
				},
			},
			core.View{
				Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{control},
			},
		}
		if revertBtn != nil {
			children = append(children, revertBtn)
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        6,
			},
			Children: children,
		}
	}
	layerFieldRow := func(fieldKey, label string, control core.Node) core.Node {
		if fieldKey == "" {
			return layerFieldsRow(nil, label, control)
		}
		return layerFieldsRow([]string{fieldKey}, label, control)
	}
	fieldRow := func(label string, control core.Node) core.Node {
		return layerFieldRow("", label, control)
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
	}
	if inheritedNow && ed.Doc != nil && ed.Doc.Inherits != "" {
		nodes = append(nodes, core.View{
			TestID: "inspector-layer-inherited-bar",
			Style: core.Style{
				Gap:          6,
				Padding:      core.All(8),
				CornerRadius: 6,
				Background:   colors.Surface(),
				BorderWidth:  1,
				BorderColor:  colors.Line(),
			},
			Children: []core.Node{
				core.Text{
					Content:  ed.T("overrides.baseScene") + ": " + ed.Doc.Inherits,
					MaxLines: 1,
					Style:    core.Style{FontSize: 11, Color: colors.Muted()},
				},
				ui.Wrap{
					Children: []core.Node{
						ui.Button{
							TestID:  "inspector-layer-open-base",
							Text:    ed.T("overrides.openBaseScene"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							Disabled: func() bool {
								_ = rev.Get()
								return ed.Playing() || ed.Project == nil || ed.ProjectPath == ""
							},
							OnPress: func() { ed.report(ed.OpenBaseScene()) },
						},
						ui.Button{
							TestID:  "inspector-layer-revert-all",
							Text:    ed.T("overrides.revertAll"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							Disabled: func() bool {
								_ = rev.Get()
								return ed.Playing() || len(ed.LayerOverrideFields(layerName)) == 0
							},
							OnPress: func() { ed.report(ed.RevertLayerOverrides(layerName)) },
						},
					},
				},
			},
		})
	}
	nodes = append(nodes,
		sectionHeader("PARALLAX"),
		layerFieldRow("parallaxX", "Parallax X", ui.NumberInput{
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
				if l := layer(); l != nil {
					ed.report(ed.SetLayerParallax(layerName, float32(v), l.EffectiveParallaxY()))
				}
			},
		}),
		layerFieldRow("parallaxY", "Parallax Y", ui.NumberInput{
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
				if l := layer(); l != nil {
					ed.report(ed.SetLayerParallax(layerName, l.EffectiveParallaxX(), float32(v)))
				}
			},
		}),

		sectionHeader("REPEAT"),
		layerFieldsRow([]string{"repeatX", "repeatY"}, "Repeat", ui.HStack{
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
						if l := layer(); l != nil {
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
						if l := layer(); l != nil {
							ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, on, l.RepeatWidth, l.RepeatHeight))
						}
					},
				},
			},
		}),
		layerFieldRow("repeatWidth", "Repeat W", ui.NumberInput{
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
				if l := layer(); l != nil {
					ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, l.RepeatY, float32(v), l.RepeatHeight))
				}
			},
		}),
		layerFieldRow("repeatHeight", "Repeat H", ui.NumberInput{
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
				if l := layer(); l != nil {
					ed.report(ed.SetLayerRepeat(layerName, l.RepeatX, l.RepeatY, l.RepeatWidth, float32(v)))
				}
			},
		}),
	)

	nodes = append(nodes, layerTilemapSection(cx, ed, rev, layerName, layer, tilemapDisabled, sectionHeader, fieldRow)...)

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
			Content: "Inherited from base scene; edits here are saved under layerOverrides.",
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
	info := ed.ObjectOverrideInfo(objID)
	isLocal := info.Origin == ObjectLocal
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.AgentTouchingObject(objID)
	}

	sectionHeader := func(title string) core.Node {
		return core.Text{
			Content: title,
			Style: core.Style{
				FontSize:      11,
				FontWeight:    core.FontWeightSemibold,
				LetterSpacing: 0.7,
				Color:         specTextMuted,
			},
		}
	}

	overrideDot := func(overrideKey string) core.Node {
		if isLocal || overrideKey == "" {
			return nil
		}
		return core.Show{
			When: func() bool {
				_ = rev.Get()
				return ed.HasObjectOverride(objID, overrideKey)
			},
			Then: func(cx *core.Scope) core.Node {
				ovTip := ed.T("overrides.overridesSource", l10n.Param("value", ed.ObjectBaseValue(objID, overrideKey)))
				return withTooltip(core.View{
					TestID: "inspector-override-dot-" + overrideKey,
					Style: core.Style{
						Width:        core.Pt(6),
						Height:       core.Pt(6),
						CornerRadius: 3,
						Background:   colors.Warn(),
						FlexShrink:   0,
					},
				}, ovTip)
			},
		}
	}

	overrideRevertBtn := func(overrideKey, label string) core.Node {
		if isLocal || overrideKey == "" {
			return nil
		}
		return core.Show{
			When: func() bool {
				_ = rev.Get()
				return ed.HasObjectOverride(objID, overrideKey)
			},
			Then: func(cx *core.Scope) core.Node {
				return ui.IconButton{
					TestID:   "inspector-revert-" + overrideKey,
					Icon:     lucide.RotateCcw,
					Size:     20,
					IconSize: 12,
					Label:    ed.T("overrides.revertField", l10n.Param("field", label)),
					Disabled: disabled,
					OnPress:  func() { ed.report(ed.RevertObjectOverrideField(objID, overrideKey)) },
				}
			},
		}
	}

	overrideSectionTrailing := func(overrideKey, title string) core.Node {
		if isLocal || overrideKey == "" {
			return nil
		}
		return core.View{
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
			Children: []core.Node{
				overrideDot(overrideKey),
				overrideRevertBtn(overrideKey, title),
			},
		}
	}

	plainLabel := func(label, labelID, tip string) core.Node {
		return withTooltip(core.Text{
			TestID:   labelID,
			Content:  label,
			MaxLines: 1,
			Style: core.Style{
				Width:      core.Pt(68),
				FlexShrink: 0,
				FontSize:   12,
				Color:      specTextSecondary,
			},
		}, tip)
	}

	labelWithOverride := func(overrideKey string, labelNode core.Node) core.Node {
		if isLocal || overrideKey == "" {
			return labelNode
		}
		return core.View{
			Style: core.Style{
				Width:      core.Pt(68),
				FlexShrink: 0,
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
			},
			Children: []core.Node{overrideDot(overrideKey), labelNode},
		}
	}

	controlWithRevert := func(overrideKey, label string, control core.Node) core.Node {
		if isLocal || overrideKey == "" {
			return control
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
			},
			Children: []core.Node{
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{control},
				},
				overrideRevertBtn(overrideKey, label),
			},
		}
	}

	// overrideDescribedRow lays out a label (with optional override indicator dot) beside its control and an optional per-field Revert button.
	overrideDescribedRow := func(overrideKey, label, labelID, tip string, control core.Node) core.Node {
		lbl := withTooltip(core.Text{
			TestID:   labelID,
			Content:  label,
			MaxLines: 1,
			Style: core.Style{
				FlexShrink: 1,
				FontSize:   12,
				Color:      specTextSecondary,
			},
		}, tip)
		var labelCell core.Node
		if !isLocal && overrideKey != "" {
			labelCell = labelWithOverride(overrideKey, lbl)
		} else {
			labelCell = plainLabel(label, labelID, tip)
		}
		return core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				labelCell,
				core.View{
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
					Children: []core.Node{controlWithRevert(overrideKey, label, control)},
				},
			},
		}
	}
	describedRow := func(label, labelID, tip string, control core.Node) core.Node {
		return overrideDescribedRow("", label, labelID, tip, control)
	}
	fieldRow := func(label string, control core.Node) core.Node {
		return overrideDescribedRow("", label, "", "", control)
	}
	overrideFieldRow := func(overrideKey, label string, control core.Node) core.Node {
		if overrideKey == "" {
			return fieldRow(label, control)
		}
		return inspectorFieldRow(
			ed, rev, objID, "", overrideKey, "inspector-"+overrideKey,
			labelWithOverride(overrideKey, withTooltip(core.Text{
				Content:  label,
				MaxLines: 1,
				Style: core.Style{
					FlexShrink: 1,
					FontSize:   12,
					Color:      specTextSecondary,
				},
			}, "")),
			controlWithRevert(overrideKey, label, control),
		)
	}

	// scrubFieldRow renders a labeled scrubbable number row with optional override dot/revert and review & problem sub-rows.
	scrubFieldRow := func(
		overrideKey, label, labelID, tip string,
		component, fieldKey string,
		cfg inspectorNumberFieldCfg,
	) core.Node {
		cfg.Invalid = func() bool {
			_ = rev.Get()
			_, bad := ed.inspectorFieldProblemFor(objID, component, fieldKey)
			return bad
		}
		lblInner := inspectorScrubHandle(
			ed,
			cfg.TestID+"-scrub",
			tip,
			cfg.Scrub,
			disabled,
			core.Style{
				Height:     core.Pt(28),
				FlexGrow:   1,
				FlexShrink: 1,
				MinWidth:   core.Pt(0),
			},
			core.Text{
				TestID:   labelID,
				Content:  label,
				MaxLines: 1,
				Style: core.Style{
					FontSize: 12,
					Color:    specTextSecondary,
				},
			},
		)
		var lbl core.Node
		if !isLocal && overrideKey != "" {
			lbl = labelWithOverride(overrideKey, lblInner)
		} else {
			lbl = core.View{
				Style: core.Style{
					Width:      core.Pt(68),
					FlexShrink: 0,
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
				},
				Children: []core.Node{lblInner},
			}
		}
		ctrl := controlWithRevert(overrideKey, label, inspectorNumberBox(cx, ed, cfg))
		return inspectorFieldRow(ed, rev, objID, component, fieldKey, cfg.TestID, lbl, ctrl)
	}

	// multiAxisRow renders a compact X Y (or X Y Z in 3D) row with inline scrubbable axis labels,
	// per-axis override dots/revert buttons when editing an instanced or inherited object,
	// and review & problem sub-rows across every axis key.
	multiAxisRow := func(
		label string,
		fieldKeys []string,
		rowTestID string,
		axes []inspectorNumberFieldCfg,
	) core.Node {
		boxes := make([]core.Node, len(axes))
		for i, a := range axes {
			fk := ""
			if i < len(fieldKeys) {
				fk = fieldKeys[i]
			}
			key := fk
			a.Invalid = func() bool {
				_ = rev.Get()
				if key == "" {
					return false
				}
				_, bad := ed.inspectorFieldProblemFor(objID, "", key)
				return bad
			}
			boxes[i] = inspectorNumberBox(cx, ed, a)
		}
		rowCtrl := core.Node(core.View{
			Style: core.Style{
				Direction:  core.Row,
				AlignItems: core.AlignCenter,
				Gap:        4,
				FlexGrow:   1,
			},
			Children: boxes,
		})
		if !isLocal && len(fieldKeys) > 0 {
			var ovControls []core.Node
			for i, fk := range fieldKeys {
				key := fk
				axisName := key
				if i < len(axes) && axes[i].Label != "" {
					axisName = axes[i].Label
				}
				ovControls = append(ovControls, core.Show{
					When: func() bool {
						_ = rev.Get()
						return ed.HasObjectOverride(objID, key)
					},
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 3},
							Children: []core.Node{
								overrideDot(key),
								core.Text{
									Content: axisName,
									Style:   core.Style{FontSize: 10.5, Color: specTextMuted},
								},
								overrideRevertBtn(key, axisName),
							},
						}
					},
				})
			}
			rowCtrl = core.View{
				Style: core.Style{Gap: 3, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{
					rowCtrl,
					core.Show{
						When: func() bool {
							_ = rev.Get()
							for _, fk := range fieldKeys {
								if ed.HasObjectOverride(objID, fk) {
									return true
								}
							}
							return false
						},
						Then: func(cx *core.Scope) core.Node {
							return core.View{
								Style:    core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
								Children: ovControls,
							}
						},
					},
				},
			}
		}
		return inspectorFieldRowKeys(
			ed,
			rev,
			objID,
			"",
			fieldKeys,
			rowTestID,
			plainLabel(label, "", ""),
			rowCtrl,
		)
	}

	objInit, _ := ed.FindResolvedObject(objID)

	// Object header: swatch + ID + "Sprite · layer Actors" subtitle + Delete button.
	header := core.View{
		Style: core.Style{
			Direction:      core.Row,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifySpaceBetween,
			Gap:            8,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					FlexGrow:   1,
					FlexShrink: 1,
					MinWidth:   core.Pt(0),
					Gap:        10,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{
							Width:        core.Pt(28),
							Height:       core.Pt(28),
							CornerRadius: 7,
							FlexShrink:   0,
							Background:   objectSwatchColor(objInit),
						},
					},
					core.View{
						Style: core.Style{
							FlexGrow:   1,
							FlexShrink: 1,
							MinWidth:   core.Pt(0),
							Gap:        2,
						},
						Children: []core.Node{
							core.Text{
								TestID:   "inspector-object-id",
								Content:  objID,
								MaxLines: 1,
								Style: core.Style{
									FontSize:   13.5,
									FontWeight: core.FontWeightSemibold,
									Color:      specTextStrong,
								},
							},
							core.Text{
								TestID: "inspector-object-subtitle",
								ContentFunc: func() string {
									_ = rev.Get()
									o, ln := ed.FindResolvedObject(objID)
									if ln == "" {
										ln = layerName
									}
									return ed.T("inspector.kindLayer", l10n.Param("kind", inspectorObjectKindLabel(o)), l10n.Param("layer", ln))
								},
								MaxLines: 1,
								Style: core.Style{
									FontSize: 11.5,
									Color:    specTextMuted,
								},
							},
						},
					},
				},
			},
			ui.Button{
				TestID:  "inspector-delete",
				Text:    ed.T("inspector.delete"),
				Size:    ui.SizeSmall,
				Variant: ui.ButtonOutline,
				Disabled: func() bool {
					return disabled() || !isLocal
				},
				OnPress: func() {
					ed.report(ed.DeleteObject(objID))
				},
			},
		},
	}

	// Filter fields search box (mounted outside the filtered Dynamic so typing keeps focus).
	filterBar := core.View{
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(28),
			Padding:      core.Symmetric(0, 8),
			CornerRadius: 6,
			Background:   specRaised,
			BorderWidth:  1,
			BorderColor:  specLineRaised,
			Gap:          6,
		},
		Children: []core.Node{
			ui.Icon(lucide.Search, 12, func() core.Color { return specTextMuted }),
			core.Input{
				TestID:      "inspector-field-filter",
				Label:       ed.T("inspector.filterFields"),
				Placeholder: ed.T("inspector.filterFields"),
				Role:        core.RoleSearchField,
				Value: func() string {
					_ = rev.Get()
					return ed.InspectorFilter()
				},
				OnChange: func(text string) {
					ed.SetInspectorFilter(text)
				},
				Style: core.Style{
					FlexGrow:         1,
					FlexShrink:       1,
					MinWidth:         core.Pt(0),
					Height:           core.Pt(26),
					FontSize:         12,
					Color:            specTextStrong,
					PlaceholderColor: specTextMuted,
					CaretColor:       specAccent,
				},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.InspectorFilter() != ""
				},
				Then: func(cx *core.Scope) core.Node {
					return ui.IconButton{
						TestID: "inspector-field-filter-clear",
						Icon:   lucide.X,
						Label:  "Clear field filter",
						OnPress: func() {
							ed.SetInspectorFilter("")
						},
					}
				},
			},
		},
	}

	filterKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return strings.ToLower(strings.TrimSpace(ed.InspectorFilter()))
	})

	sectionsNode := core.Dynamic(func(cx *core.Scope) core.Node {
		q := filterKey.Get()
		filtering := q != ""
		var nodes []core.Node

		// Name row (shown unfiltered or when matching "name").
		if !filtering || matchesInspectorFilter(q, "", "", "name", ed.T("inspector.name")) {
			nameInputRef := &core.Ref{}
			if ed.focus.ctrl != nil {
				ed.focus.ctrl.registerCandidate(cx, RegionInspector, nameInputRef, func() bool {
					return !disabled()
				})
			}
			nodes = append(nodes, inspectorFieldRow(
				ed, rev, objID, "", "name", "inspector-name",
				labelWithOverride("name", withTooltip(core.Text{
					Content:  ed.T("inspector.name"),
					MaxLines: 1,
					Style: core.Style{
						FlexShrink: 1,
						FontSize:   12,
						Color:      specTextSecondary,
					},
				}, "")),
				controlWithRevert("name", ed.T("inspector.name"), specInputField(cx, ed, ui.Input{
					Ref:      nameInputRef,
					TestID:   "inspector-name",
					Label:    ed.T("inspector.objectName"),
					Disabled: disabled,
					Value: func() string {
						_ = rev.Get()
						if o, _ := ed.FindResolvedObject(objID); o != nil {
							return o.Name
						}
						return ""
					},
					OnChange: func(text string) {
						ed.report(ed.SetObjectName(objID, text))
					},
				})),
			))
		}

		// Scene instance / inherited scene override bar.
		if (isLocal && info.SourceScene != "") || !isLocal {
			if !filtering || matchesInspectorFilter(q, "INSTANCE", "", "scene", "instance", "override", "inherited", info.SourceScene) {
				isInstanceRoot := isLocal && info.SourceScene != ""
				isInstancedChild := info.Origin == ObjectInstanced
				isInheritedObj := info.Origin == ObjectInherited
				badgeLabel := ed.T("overrides.instance")
				if isInstancedChild {
					badgeLabel = ed.T("overrides.instanced")
				} else if isInheritedObj {
					badgeLabel = ed.T("overrides.inherited")
				}
				targetInstID := objID
				if isInstancedChild {
					targetInstID = info.InstanceID
				}
				openBtnID := "inspector-open-instanced-scene"
				openBtnLabel := ed.T("overrides.openInstancedScene")
				if isInheritedObj {
					openBtnID = "inspector-open-base-scene"
					openBtnLabel = ed.T("overrides.openBaseScene")
				}
				actionBtns := []core.Node{
					ui.Button{
						TestID:  openBtnID,
						Text:    openBtnLabel,
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							return disabled() || ed.Project == nil || ed.ProjectPath == ""
						},
						OnPress: func() {
							ed.report(ed.OpenSourceScene(objID))
						},
					},
					ui.Button{
						TestID:  "inspector-revert-all",
						Text:    ed.T("overrides.revertAll"),
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							if disabled() {
								return true
							}
							if isInstanceRoot {
								return ed.InstanceOverrideCount(objID) == 0
							}
							return !ed.ObjectHasAnyOverride(objID)
						},
						OnPress: func() {
							ed.report(ed.RevertObjectOverrides(objID))
						},
					},
				}
				if isInstanceRoot || isInstancedChild {
					actionBtns = append(actionBtns,
						ui.Button{
							TestID:  "inspector-apply-to-source",
							Text:    ed.T("overrides.applyToSource"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							Disabled: func() bool {
								return disabled() || ed.Project == nil || ed.ProjectPath == "" || ed.InstanceOverrideCount(targetInstID) == 0
							},
							OnPress: func() {
								ed.report(ed.ApplyOverridesToSource(targetInstID))
							},
						},
						ui.Button{
							TestID:   "inspector-make-local",
							Text:     ed.T("overrides.makeLocal"),
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonSecondary,
							Disabled: disabled,
							OnPress: func() {
								ed.report(ed.MakeInstanceLocal(targetInstID))
							},
						},
					)
				}
				nodes = append(nodes, core.View{
					TestID: "inspector-instance-bar",
					Style: core.Style{
						Gap:          6,
						Padding:      core.All(8),
						CornerRadius: 6,
						Background:   specRaised,
						BorderWidth:  1,
						BorderColor:  specLineRaised,
					},
					Children: []core.Node{
						core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
							Children: []core.Node{
								ui.Badge{Text: badgeLabel, Variant: ui.BadgeNeutral},
								core.Text{
									TestID:   "inspector-object-scene",
									Content:  info.SourceScene,
									MaxLines: 1,
									Style:    core.Style{FontSize: 11, Color: specText, FlexGrow: 1, FlexShrink: 1},
								},
							},
						},
						core.Text{
							TestID: "inspector-override-count",
							ContentFunc: func() string {
								_ = rev.Get()
								ovCount := ed.InstanceOverrideCount(objID)
								if isInstanceRoot || isInstancedChild {
									instCount, _ := ed.CountSceneInstances(info.SourceScene)
									return ed.T("overrides.instanceCount", l10n.Param("count", instCount)) + " · " + ed.T("overrides.overrideCount", l10n.Param("count", ovCount))
								}
								return ed.T("overrides.overrideCount", l10n.Param("count", ovCount))
							},
							Style: core.Style{FontSize: 11, Color: specTextMuted},
						},
						ui.Wrap{Children: actionBtns},
					},
				})
			}
		}

		if ed.ViewMode == ViewMode3D {
			nodes = append(nodes, inspectorModel3DSections(cx, ed, rev, objID, q, disabled, overrideFieldRow("model", ed.T("inspector.model"), modelField(ed, rev, objID, disabled)))...)
		}

		// TRANSFORM titled section with compact Position/Rotation/Scale X Y (and Z in 3D), Alpha, Z order.
		{
			tfTitle := ed.T("inspector.section.transform")
			is3D := ed.ViewMode == ViewMode3D
			var tfRows []core.Node
			var tfKeys []string

			if matchesInspectorFilter(q, tfTitle, "", "position", "pos", "x", "y", "posZ", ed.T("inspector.position")) {
				posAxes := []inspectorNumberFieldCfg{
					{
						TestID:    "inspector-x",
						Label:     "X",
						AxisLabel: "X",
						AxisColor: specProblem,
						Step:      1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "x", Step: 1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.X)
							}
							return 0
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.MoveObject(objID, float32(v), o.Y))
							}
						},
					},
					{
						TestID:    "inspector-y",
						Label:     "Y",
						AxisLabel: "Y",
						AxisColor: specAdded,
						Step:      1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "y", Step: 1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.Y)
							}
							return 0
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.MoveObject(objID, o.X, float32(v)))
							}
						},
					},
				}
				posKeys := []string{"x", "y"}
				if is3D {
					posAxes = append(posAxes, inspectorNumberFieldCfg{
						TestID:    "inspector-pos-z",
						Label:     ed.T("inspector.posZ"),
						AxisLabel: "Z",
						AxisColor: specAccent,
						Step:      1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "posZ", Step: 1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.PosZ)
							}
							return 0
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.MoveObject3D(objID, o.X, o.Y, float32(v)))
							}
						},
					})
					posKeys = append(posKeys, "posZ")
				}
				tfRows = append(tfRows, multiAxisRow(ed.T("inspector.position"), posKeys, "inspector-position", posAxes))
				tfKeys = append(tfKeys, posKeys...)
			}

			if is3D {
				if matchesInspectorFilter(q, tfTitle, "", "rotation", "rot", "rotX", "rotY", "rotZ", ed.T("inspector.rotation")) {
					rotAxes := []inspectorNumberFieldCfg{
						{
							TestID:    "inspector-rot-x",
							Label:     ed.T("inspector.rotX"),
							AxisLabel: "X",
							AxisColor: specProblem,
							Step:      0.1,
							Scrub:     InspectorScrubField{ObjectID: objID, Field: "rotX", Step: 0.1},
							Disabled:  disabled,
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									return float64(o.RotX)
								}
								return 0
							},
							OnChange: func(v float64) {
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									ed.report(ed.RotateObject3D(objID, float32(v), o.RotY, o.RotZ))
								}
							},
						},
						{
							TestID:    "inspector-rot-y",
							Label:     ed.T("inspector.rotY"),
							AxisLabel: "Y",
							AxisColor: specAdded,
							Step:      0.1,
							Scrub:     InspectorScrubField{ObjectID: objID, Field: "rotY", Step: 0.1},
							Disabled:  disabled,
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									return float64(o.RotY)
								}
								return 0
							},
							OnChange: func(v float64) {
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									ed.report(ed.RotateObject3D(objID, o.RotX, float32(v), o.RotZ))
								}
							},
						},
						{
							TestID:    "inspector-rot-z",
							Label:     ed.T("inspector.rotZ"),
							AxisLabel: "Z",
							AxisColor: specAccent,
							Step:      0.1,
							Scrub:     InspectorScrubField{ObjectID: objID, Field: "rotZ", Step: 0.1},
							Disabled:  disabled,
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									return float64(o.RotZ)
								}
								return 0
							},
							OnChange: func(v float64) {
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									ed.report(ed.RotateObject3D(objID, o.RotX, o.RotY, float32(v)))
								}
							},
						},
					}
					tfRows = append(tfRows, multiAxisRow(ed.T("inspector.rotation"), []string{"rotX", "rotY", "rotZ"}, "inspector-rotation-3d", rotAxes))
					tfKeys = append(tfKeys, "rotX", "rotY", "rotZ")
				}
			} else if matchesInspectorFilter(q, tfTitle, "", "rotation", "rot", ed.T("inspector.rotation")) {
				tfRows = append(tfRows, scrubFieldRow(
					"rotation", ed.T("inspector.rotation"), "", "", "", "rotation",
					inspectorNumberFieldCfg{
						TestID:   "inspector-rotation",
						Label:    ed.T("inspector.rotation"),
						Step:     0.1,
						Scrub:    InspectorScrubField{ObjectID: objID, Field: "rotation", Step: 0.1},
						Disabled: disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.Rotation)
							}
							return 0
						},
						OnChange: func(v float64) {
							ed.report(ed.RotateObject(objID, float32(v)))
						},
					},
				))
				tfKeys = append(tfKeys, "rotation")
			}

			if matchesInspectorFilter(q, tfTitle, "", "scale", "scaleX", "scaleY", "scaleZ", ed.T("inspector.scale"), ed.T("inspector.scaleX"), ed.T("inspector.scaleY")) {
				scaleAxes := []inspectorNumberFieldCfg{
					{
						TestID:    "inspector-scale-x",
						Label:     ed.T("inspector.scaleX"),
						AxisLabel: "X",
						AxisColor: specProblem,
						Step:      0.1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "scaleX", Step: 0.1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.EffectiveScaleX())
							}
							return 1
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.ScaleObject(objID, float32(v), o.EffectiveScaleY()))
							}
						},
					},
					{
						TestID:    "inspector-scale-y",
						Label:     ed.T("inspector.scaleY"),
						AxisLabel: "Y",
						AxisColor: specAdded,
						Step:      0.1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "scaleY", Step: 0.1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.EffectiveScaleY())
							}
							return 1
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.ScaleObject(objID, o.EffectiveScaleX(), float32(v)))
							}
						},
					},
				}
				scaleKeys := []string{"scaleX", "scaleY"}
				if is3D {
					scaleAxes = append(scaleAxes, inspectorNumberFieldCfg{
						TestID:    "inspector-scale-z",
						Label:     ed.T("inspector.scaleZ"),
						AxisLabel: "Z",
						AxisColor: specAccent,
						Step:      0.1,
						Scrub:     InspectorScrubField{ObjectID: objID, Field: "scaleZ", Step: 0.1},
						Disabled:  disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.EffectiveScaleZ())
							}
							return 1
						},
						OnChange: func(v float64) {
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								ed.report(ed.ScaleObject3D(objID, o.EffectiveScaleX(), o.EffectiveScaleY(), float32(v)))
							}
						},
					})
					scaleKeys = append(scaleKeys, "scaleZ")
				}
				tfRows = append(tfRows, multiAxisRow(ed.T("inspector.scale"), scaleKeys, "inspector-scale", scaleAxes))
				tfKeys = append(tfKeys, scaleKeys...)
			}

			if matchesInspectorFilter(q, tfTitle, "", "alpha", "opacity", ed.T("inspector.alpha")) {
				alphaMin, alphaMax := 0.0, 1.0
				tfRows = append(tfRows, scrubFieldRow(
					"alpha", ed.T("inspector.alpha"), "", "", "", "alpha",
					inspectorNumberFieldCfg{
						TestID:   "inspector-alpha",
						Label:    ed.T("inspector.alpha"),
						Min:      &alphaMin,
						Max:      &alphaMax,
						Step:     0.05,
						Scrub:    InspectorScrubField{ObjectID: objID, Field: "alpha", Step: 0.05, Min: &alphaMin, Max: &alphaMax},
						Disabled: disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.EffectiveAlpha())
							}
							return 1
						},
						OnChange: func(v float64) {
							ed.report(ed.SetObjectAlpha(objID, float32(v)))
						},
					},
				))
				tfKeys = append(tfKeys, "alpha")
			}

			if matchesInspectorFilter(q, tfTitle, "", "z", "z order", ed.T("inspector.zOrder")) {
				tfRows = append(tfRows, scrubFieldRow(
					"z", ed.T("inspector.zOrder"), "", "", "", "z",
					inspectorNumberFieldCfg{
						TestID:   "inspector-z",
						Label:    "Z",
						Step:     1,
						Integer:  true,
						Scrub:    InspectorScrubField{ObjectID: objID, Field: "z", Step: 1, Integer: true},
						Disabled: disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.Z)
							}
							return 0
						},
						OnChange: func(v float64) {
							ed.report(ed.SetObjectZ(objID, int(math.Round(v))))
						},
					},
				))
				tfKeys = append(tfKeys, "z")
			}

			if len(tfRows) > 0 {
				if is3D && !filtering {
					tfRows = append(tfRows, core.Text{
						TestID:  "inspector-scrub-hint-3d",
						Content: ed.T("inspector.scrubHint3D"),
						Style: core.Style{
							FontSize: 11,
							Color:    specTextMuted,
						},
					})
				}
				fieldCount := len(tfRows)
				if is3D && !filtering {
					fieldCount--
				}
				keys := tfKeys
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "transform", "", tfTitle, "", fieldCount,
					func() int {
						n := 0
						for _, k := range keys {
							if _, bad := ed.inspectorFieldProblemFor(objID, "", k); bad {
								n++
							}
						}
						return n
					},
					func() int {
						n := 0
						for _, k := range keys {
							if _, ok := ed.inspectorReviewChangeFor(objID, "", k); ok {
								n++
							}
						}
						return n
					},
					nil, filtering, tfRows,
				))
			}
		}

		// 3D Geometry / Model / Occluder section (in 3D view).
		if ed.ViewMode == ViewMode3D {
			t3dTitle := ed.T("inspector.section.transform3d")
			var rows3d []core.Node
			if matchesInspectorFilter(q, t3dTitle, "", "depth", ed.T("inspector.depth")) {
				depthMin := 0.0
				rows3d = append(rows3d, scrubFieldRow(
					"depth", ed.T("inspector.depth"), "", "", "", "depth",
					inspectorNumberFieldCfg{
						TestID:   "inspector-depth",
						Label:    ed.T("inspector.depth"),
						Min:      &depthMin,
						Step:     1,
						Scrub:    InspectorScrubField{ObjectID: objID, Field: "depth", Step: 1, Min: &depthMin},
						Disabled: disabled,
						Value: func() float64 {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								return float64(o.Depth)
							}
							return 0
						},
						OnChange: func(v float64) {
							ed.report(ed.SetObjectDepth(objID, float32(v)))
						},
					},
				))
			}
			if matchesInspectorFilter(q, t3dTitle, "", "mesh", ed.T("inspector.mesh")) {
				rows3d = append(rows3d, overrideFieldRow("mesh", ed.T("inspector.mesh"), ui.Select[string]{
					TestID:      "inspector-mesh",
					Label:       ed.T("inspector.mesh"),
					Placeholder: ed.T("inspector.none"),
					Style:       core.Style{Width: core.Pct(100), MinWidth: core.Pt(0)},
					// An object that draws a glTF model or GridMap cannot also draw a primitive, which SetObjectMesh refuses.
					Disabled: func() bool {
						if disabled() {
							return true
						}
						o, _ := ed.FindResolvedObject(objID)
						return o != nil && (o.Model != "" || o.GridMap != "")
					},
					Options: meshOptions(ed),
					Value: func() string {
						_ = rev.Get()
						if o, _ := ed.FindResolvedObject(objID); o != nil {
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
				}))
			}
			if matchesInspectorFilter(q, t3dTitle, "", "gridmap") {
				rows3d = append(rows3d, overrideFieldRow("gridMap", "GridMap", ui.Select[string]{
					TestID:      "inspector-gridmap",
					Label:       "GridMap",
					Placeholder: "None",
					Searchable:  true,
					Style:       core.Style{Width: core.Pct(100), MinWidth: core.Pt(0)},
					Disabled: func() bool {
						if disabled() {
							return true
						}
						o, _ := ed.FindResolvedObject(objID)
						return o != nil && (o.Mesh != "" || o.Model != "")
					},
					OptionsFunc: func() []ui.Option[string] {
						_ = rev.Get()
						cur := ""
						if o, _ := ed.FindResolvedObject(objID); o != nil {
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
						if o, _ := ed.FindResolvedObject(objID); o != nil {
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
				}))
			}
			if matchesInspectorFilter(q, t3dTitle, "", "lod", "occluder") {
				rows3d = append(rows3d,
					core.Show{
						When: func() bool {
							_ = rev.Get()
							return ed.ObjectLODCount3D(objID) > 0
						},
						Then: func(cx *core.Scope) core.Node {
							return fieldRow("Active LOD", core.Text{
								TestID: "inspector-lod-active",
								ContentFunc: func() string {
									_ = rev.Get()
									count := ed.ObjectLODCount3D(objID)
									if ed.ObjectOccluded3D(objID) {
										return fmt.Sprintf("Occluded (%d levels)", count)
									}
									if lv, ok := ed.ObjectLOD3D(objID); ok {
										return fmt.Sprintf("LOD %d / %d", lv, count)
									}
									return fmt.Sprintf("Culled (%d levels)", count)
								},
								Style: core.Style{FontSize: 12, Color: specText},
							})
						},
					},
					overrideFieldRow("occluder", "Occluder", ui.Select[string]{
						TestID:   "inspector-occluder-mode",
						Label:    "Occluder Mode",
						Disabled: disabled,
						Style:    core.Style{Width: core.Pct(100), MinWidth: core.Pt(0)},
						Options: []ui.Option[string]{
							{Value: "", Label: "None"},
							{Value: string(sceneformat.OccluderModel), Label: "Model"},
							{Value: string(sceneformat.OccluderBox), Label: "Box"},
						},
						Value: func() string {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil && o.Occluder != nil {
								return string(o.Occluder.Mode)
							}
							return ""
						},
						OnChange: func(mode string) {
							var occ *sceneformat.OccluderDoc
							switch sceneformat.OccluderMode(mode) {
							case sceneformat.OccluderModel:
								occ = &sceneformat.OccluderDoc{Mode: sceneformat.OccluderModel}
							case sceneformat.OccluderBox:
								sz := [3]float32{1, 1, 1}
								occ = &sceneformat.OccluderDoc{Mode: sceneformat.OccluderBox, Size: &sz}
							}
							if err := ed.SetObjectOccluder(objID, occ); err != nil {
								ed.Status = err.Error()
								ed.notify()
							}
						},
					}),
					core.Show{
						When: func() bool {
							_ = rev.Get()
							o, _ := ed.FindResolvedObject(objID)
							return o != nil && o.Occluder != nil && o.Occluder.Mode == sceneformat.OccluderBox
						},
						Then: func(cx *core.Scope) core.Node {
							sizeInput := func(testID, label string, axis int) core.Node {
								return fieldRow(label, ui.NumberInput{
									TestID:   testID,
									Label:    label,
									Min:      0.01,
									Step:     0.5,
									Disabled: disabled,
									Value: func() float64 {
										_ = rev.Get()
										if o, _ := ed.FindResolvedObject(objID); o != nil && o.Occluder != nil {
											return float64(o.Occluder.EffectiveSize()[axis])
										}
										return 1
									},
									OnChange: func(v float64) {
										if o, _ := ed.FindResolvedObject(objID); o != nil && o.Occluder != nil {
											next := cloneOccluderDoc(o.Occluder)
											sz := next.EffectiveSize()
											sz[axis] = float32(v)
											next.Size = &sz
											ed.report(ed.SetObjectOccluder(objID, next))
										}
									},
								})
							}
							return core.View{
								Style: core.Style{Gap: 6},
								Children: []core.Node{
									sizeInput("inspector-occluder-size-x", "Occ Size X", 0),
									sizeInput("inspector-occluder-size-y", "Occ Size Y", 1),
									sizeInput("inspector-occluder-size-z", "Occ Size Z", 2),
								},
							}
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							o, _ := ed.FindResolvedObject(objID)
							return o != nil && o.Occluder != nil && o.Occluder.Mode == sceneformat.OccluderModel
						},
						Then: func(cx *core.Scope) core.Node {
							return fieldRow("2-Sided", ui.Checkbox{
								TestID:   "inspector-occluder-double-sided",
								Label:    "Double-Sided Occluder",
								Disabled: disabled,
								Checked: func() bool {
									_ = rev.Get()
									o, _ := ed.FindResolvedObject(objID)
									return o != nil && o.Occluder != nil && o.Occluder.DoubleSided
								},
								OnChange: func(on bool) {
									if o, _ := ed.FindResolvedObject(objID); o != nil && o.Occluder != nil {
										next := cloneOccluderDoc(o.Occluder)
										next.DoubleSided = on
										ed.report(ed.SetObjectOccluder(objID, next))
									}
								},
							})
						},
					},
				)
			}
			if len(rows3d) > 0 {
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "transform3d", "", t3dTitle, "", len(rows3d), nil, nil, nil, filtering, rows3d,
				))
			}
		}

		// LIGHT 3D, CAMERA 3D, REFLECTION PROBE, and LIGHT PROBE GRID titled sections.
		if o, _ := ed.FindResolvedObject(objID); ed.ViewMode == ViewMode3D || (o != nil && (o.Light != nil || o.Camera != nil || o.ReflectionProbe != nil || o.LightProbeGrid != nil)) {
			if !filtering || matchesInspectorFilter(q, "LIGHT 3D", "", "light", "color", "intensity", "range", "shadows") {
				lightRows := light3DInspectorFields(ed, rev, objID, disabled, fieldRow)
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "light3d", "", "LIGHT 3D", "", len(lightRows), nil, nil, overrideSectionTrailing("light", "LIGHT 3D"), filtering, lightRows,
				))
			}
			if !filtering || matchesInspectorFilter(q, "CAMERA 3D", "", "camera", "fov", "near", "far", "projection") {
				camRows := camera3DInspectorFields(ed, rev, objID, disabled, fieldRow)
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "camera3d", "", "CAMERA 3D", "", len(camRows), nil, nil, overrideSectionTrailing("camera", "CAMERA 3D"), filtering, camRows,
				))
			}
			if !filtering || matchesInspectorFilter(q, "REFLECTION PROBE", "", "reflection", "probe", "box", "resolution", "bake") {
				rpRows := reflectionProbeInspectorFields(ed, rev, objID, disabled, fieldRow)
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "reflection-probe", "", "REFLECTION PROBE", "", len(rpRows), nil, nil, nil, filtering, rpRows,
				))
			}
			if !filtering || matchesInspectorFilter(q, "LIGHT PROBE GRID", "", "light probe", "probe", "grid", "bake") {
				lpRows := lightProbeGridInspectorFields(ed, rev, objID, disabled, fieldRow)
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "light-probe-grid", "", "LIGHT PROBE GRID", "", len(lpRows), nil, nil, nil, filtering, lpRows,
				))
			}
		}

		// SPRITE titled section.
		{
			spriteTitle := ed.T("inspector.section.sprite")
			if sRows := spriteFieldsFiltered(cx, ed, rev, objID, q, spriteTitle, disabled, overrideFieldRow, multiAxisRow); len(sRows) > 0 {
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "sprite", "", spriteTitle, "", len(sRows),
					func() int {
						n := 0
						for _, k := range []string{"sprite", "originX", "originY", "flipX", "flipY", "material"} {
							if _, bad := ed.inspectorFieldProblemFor(objID, "", k); bad {
								n++
							}
						}
						return n
					},
					func() int {
						n := 0
						for _, k := range []string{"sprite", "originX", "originY", "flipX", "flipY", "material"} {
							if _, ok := ed.inspectorReviewChangeFor(objID, "", k); ok {
								n++
							}
						}
						return n
					},
					nil, filtering, sRows,
				))
			}
		}

		// PARTICLES titled section.
		if !filtering || matchesInspectorFilter(q, "PARTICLES", "", "particles", "emitter", "atlas") {
			pRows := particlesFields(ed, rev, objID, disabled, overrideFieldRow)
			nodes = append(nodes, inspectorTitledSection(
				cx, ed, rev, "particles", "", "PARTICLES", "", len(pRows), nil, nil, nil, filtering, pRows,
			))
		}

		obj, _ := ed.FindResolvedObject(objID)

		// PROPERTIES titled section.
		{
			propTitle := ed.T("inspector.section.properties")
			var propRows []core.Node
			var propKeys []string
			if obj != nil {
				for _, k := range sortedMapKeys(obj.Properties) {
					key := k
					if !matchesInspectorFilter(q, propTitle, "", key) {
						continue
					}
					sample := obj.Properties[key]
					ovKey := "property:" + key
					del := removeButton("inspector-prop-"+key+"-delete", ed.T("inspector.deleteProperty", l10n.Param("key", key)), disabled, func() {
						ed.report(ed.DeleteProperty(objID, key))
					})
					propKeys = append(propKeys, "prop:"+key)
					switch sample.(type) {
					case int, int64, int32:
						cfg := inspectorNumberFieldCfg{
							TestID:   "inspector-prop-" + key,
							Label:    key,
							Step:     1,
							Integer:  true,
							Scrub:    InspectorScrubField{ObjectID: objID, Field: "prop:" + key, Step: 1, Integer: true},
							Disabled: disabled,
							Invalid: func() bool {
								_ = rev.Get()
								_, bad := ed.inspectorFieldProblemFor(objID, "", "prop:"+key)
								return bad
							},
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									if v, ok := anyToFloat64(o.Properties[key]); ok {
										return v
									}
								}
								return 0
							},
							OnChange: func(v float64) {
								ed.report(ed.SetProperty(objID, key, int(math.Round(v))))
							},
						}
						lblInner := inspectorScrubHandle(
							ed, cfg.TestID+"-scrub", "", cfg.Scrub, disabled,
							core.Style{Height: core.Pt(28), FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
							core.Text{Content: key, MaxLines: 1, Style: core.Style{FontSize: 12, Color: specTextSecondary}},
						)
						lbl := labelWithOverride(ovKey, lblInner)
						if isLocal {
							lbl = core.View{
								Style:    core.Style{Width: core.Pt(68), FlexShrink: 0, Direction: core.Row, AlignItems: core.AlignCenter},
								Children: []core.Node{lblInner},
							}
						}
						propRows = append(propRows, inspectorFieldRow(ed, rev, objID, "", "prop:"+key, cfg.TestID, lbl, controlWithRevert(ovKey, key, withTrailing(inspectorNumberBox(cx, ed, cfg), del))))
					case float64, float32:
						cfg := inspectorNumberFieldCfg{
							TestID:   "inspector-prop-" + key,
							Label:    key,
							Step:     0.1,
							Scrub:    InspectorScrubField{ObjectID: objID, Field: "prop:" + key, Step: 0.1},
							Disabled: disabled,
							Invalid: func() bool {
								_ = rev.Get()
								_, bad := ed.inspectorFieldProblemFor(objID, "", "prop:"+key)
								return bad
							},
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									if v, ok := anyToFloat64(o.Properties[key]); ok {
										return v
									}
								}
								return 0
							},
							OnChange: func(v float64) {
								ed.report(ed.SetProperty(objID, key, v))
							},
						}
						lblInner := inspectorScrubHandle(
							ed, cfg.TestID+"-scrub", "", cfg.Scrub, disabled,
							core.Style{Height: core.Pt(28), FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
							core.Text{Content: key, MaxLines: 1, Style: core.Style{FontSize: 12, Color: specTextSecondary}},
						)
						lbl := labelWithOverride(ovKey, lblInner)
						if isLocal {
							lbl = core.View{
								Style:    core.Style{Width: core.Pt(68), FlexShrink: 0, Direction: core.Row, AlignItems: core.AlignCenter},
								Children: []core.Node{lblInner},
							}
						}
						propRows = append(propRows, inspectorFieldRow(ed, rev, objID, "", "prop:"+key, cfg.TestID, lbl, controlWithRevert(ovKey, key, withTrailing(inspectorNumberBox(cx, ed, cfg), del))))
					default:
						ctrl := typedFieldControl(
							"inspector-prop-"+key,
							key,
							sample,
							disabled,
							func() any {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil {
									return o.Properties[key]
								}
								return nil
							},
							func(val any) {
								ed.report(ed.SetProperty(objID, key, val))
							},
						)
						lbl := labelWithOverride(ovKey, withTooltip(core.Text{
							Content:  key,
							MaxLines: 1,
							Style:    core.Style{FlexShrink: 1, FontSize: 12, Color: specTextSecondary},
						}, ""))
						if isLocal {
							lbl = plainLabel(key, "", "")
						}
						propRows = append(propRows, inspectorFieldRow(ed, rev, objID, "", "prop:"+key, "inspector-prop-"+key, lbl, controlWithRevert(ovKey, key, withTrailing(ctrl, del))))
					}
				}
			}
			fieldCount := len(propRows)
			if !filtering || matchesInspectorFilter(q, propTitle, "", "property", "properties") {
				propRows = append(propRows, addPropertyRow(cx, ed, objID, disabled))
			}
			if len(propRows) > 0 {
				pKeys := propKeys
				nodes = append(nodes, inspectorTitledSection(
					cx, ed, rev, "properties", "", propTitle, "", fieldCount,
					func() int {
						n := 0
						for _, k := range pKeys {
							if _, bad := ed.inspectorFieldProblemFor(objID, "", k); bad {
								n++
							}
						}
						return n
					},
					func() int {
						n := 0
						for _, k := range pKeys {
							if _, ok := ed.inspectorReviewChangeFor(objID, "", k); ok {
								n++
							}
						}
						return n
					},
					nil, filtering, propRows,
				))
			}
		}

		// Each Component as its own titled Spec section (e.g. PATROL, HEALTH, PHYSICS).
		if obj != nil {
			for _, compName := range sortedMapKeys(obj.Components) {
				cn := compName
				compTitle := strings.ToUpper(cn)
				compDoc := ""
				if t, ok := ed.lookupComponentType(cn); ok {
					compDoc = t.Doc
				}
				remove := removeButton("inspector-comp-"+cn+"-remove", ed.T("inspector.removeComponent", l10n.Param("name", cn)), disabled, func() {
					ed.report(ed.RemoveComponent(objID, cn))
				})

				if in, plugin := ed.pluginInspector(cn); in != nil {
					if !filtering || matchesInspectorFilter(q, compTitle, cn) {
						nodes = append(nodes, inspectorTitledSection(
							cx, ed, rev, "comp-"+cn, "inspector-comp-"+cn, compTitle, compDoc, 1, nil, nil, remove, filtering,
							[]core.Node{
								core.View{
									TestID:   "inspector-plugin-" + plugin + "-" + cn,
									Style:    core.Style{Gap: 8},
									Children: []core.Node{in.View(cx, ed, rev, objID)},
								},
							},
						))
					}
					continue
				}

				if t, ok := ed.lookupComponentType(cn); ok {
					var compRows []core.Node
					var fieldNames []string
					for _, f := range t.Fields {
						field := f
						if !matchesInspectorFilter(q, compTitle, cn, field.Name) {
							continue
						}
						fieldNames = append(fieldNames, field.Name)
						testID := "inspector-comp-" + cn + "-" + field.Name
						labelID := testID + "-label"
						tip := fieldTooltip(field)
						ovKey := "component:" + cn + "." + field.Name
						get := func() any {
							_ = rev.Get()
							if o, _ := ed.FindResolvedObject(objID); o != nil {
								if cm, ok := o.Components[cn].(map[string]any); ok {
									if v, ok := cm[field.Name]; ok {
										return v
									}
								}
							}
							return field.Default
						}
						set := func(val any) {
							if cn == sceneformat.Occluder2DComponentName && field.Name == "shape" {
								if s, ok := val.(string); ok && s == "polygon" {
									if err := ed.ConvertOccluderToPolygon(objID); err != nil {
										ed.Status = err.Error()
										ed.notify()
									}
									return
								}
							}
							if err := ed.SetComponentField(objID, cn, field.Name, val); err != nil {
								ed.Status = err.Error()
								ed.notify()
							}
						}
						if field.Kind == sceneformat.FieldInt || field.Kind == sceneformat.FieldFloat {
							isInt := field.Kind == sceneformat.FieldInt
							step := field.Step
							if step <= 0 {
								if isInt {
									step = 1
								} else {
									step = 0.1
								}
							}
							cfg := inspectorNumberFieldCfg{
								TestID:   testID,
								Label:    cn + "." + field.Name,
								Step:     step,
								Min:      field.Min,
								Max:      field.Max,
								Integer:  isInt,
								Scrub:    InspectorScrubField{ObjectID: objID, Field: "comp:" + cn + "." + field.Name, Step: step, Min: field.Min, Max: field.Max, Integer: isInt},
								Disabled: disabled,
								Value: func() float64 {
									if v, ok := anyToFloat64(get()); ok {
										return v
									}
									return 0
								},
								OnChange: func(v float64) {
									if isInt {
										set(int(math.Round(v)))
									} else {
										set(v)
									}
								},
							}
							compRows = append(compRows, scrubFieldRow(ovKey, field.Name, labelID, tip, cn, field.Name, cfg))
							continue
						}
						ctrl := registeredFieldControl(testID, cn+"."+field.Name, field, disabled, get, set)
						lbl := labelWithOverride(ovKey, withTooltip(core.Text{
							TestID:   labelID,
							Content:  field.Name,
							MaxLines: 1,
							Style:    core.Style{FlexShrink: 1, FontSize: 12, Color: specTextSecondary},
						}, tip))
						if isLocal {
							lbl = plainLabel(field.Name, labelID, tip)
						}
						compRows = append(compRows, inspectorFieldRow(ed, rev, objID, cn, field.Name, testID, lbl, controlWithRevert(ovKey, field.Name, ctrl)))
					}
					matchedFields := len(compRows)
					if matchedFields > 0 {
						if !filtering || matchesInspectorFilter(q, compTitle, cn) {
							if cn == sceneformat.Occluder2DComponentName {
								compRows = append(compRows, ui.Button{
									TestID:   "inspector-occluder-to-polygon",
									Text:     "Convert to Polygon",
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonSecondary,
									Disabled: disabled,
									OnPress: func() {
										ed.report(ed.ConvertOccluderToPolygon(objID))
									},
								})
							}
							if cn == sceneformat.Collider3DComponentName || cn == sceneformat.Trigger3DComponentName {
								compRows = append(compRows, ui.Button{
									TestID:  "inspector-comp-" + cn + "-fit-bounds",
									Text:    "Fit to Model Bounds",
									Size:    ui.SizeSmall,
									Variant: ui.ButtonSecondary,
									Disabled: func() bool {
										if disabled() {
											return true
										}
										o, _ := ed.FindResolvedObject(objID)
										return o == nil || (o.Model == "" && o.Mesh == "")
									},
									OnPress: func() {
										ed.report(ed.FitCollider3DToBounds(objID))
									},
								})
							}
							if cn == sceneformat.NavRegion2DComponentName {
								compRows = append(compRows, ui.Button{
									TestID:   "inspector-navregion-init-polygon",
									Text:     ed.T("inspector.navRegionResetPolygon"),
									Size:     ui.SizeSmall,
									Variant:  ui.ButtonSecondary,
									Disabled: disabled,
									OnPress: func() {
										ed.report(ed.InitNavRegionPolygon(objID))
									},
								})
							}
						}
						fNames := fieldNames
						nodes = append(nodes, inspectorTitledSection(
							cx, ed, rev, "comp-"+cn, "inspector-comp-"+cn, compTitle, compDoc, matchedFields,
							func() int {
								n := 0
								for _, fn := range fNames {
									if _, bad := ed.inspectorFieldProblemFor(objID, cn, fn); bad {
										n++
									}
								}
								return n
							},
							func() int {
								n := 0
								for _, fn := range fNames {
									if _, ok := ed.inspectorReviewChangeFor(objID, cn, fn); ok {
										n++
									}
								}
								return n
							},
							remove, filtering, compRows,
						))
					}
					continue
				}

				m, ok := obj.Components[cn].(map[string]any)
				if !ok {
					if !filtering || matchesInspectorFilter(q, compTitle, cn) {
						nodes = append(nodes, inspectorTitledSection(
							cx, ed, rev, "comp-"+cn, "inspector-comp-"+cn, compTitle, compDoc, 0, nil, nil, remove, filtering, nil,
						))
					}
					continue
				}
				var compRows []core.Node
				var fieldNames []string
				for _, fieldKey := range sortedMapKeys(m) {
					fk := fieldKey
					if !matchesInspectorFilter(q, compTitle, cn, fk) {
						continue
					}
					fieldNames = append(fieldNames, fk)
					sample := m[fk]
					testID := "inspector-comp-" + cn + "-" + fk
					labelID := testID + "-label"
					ovKey := "component:" + cn + "." + fk
					switch sample.(type) {
					case int, int64, int32:
						cfg := inspectorNumberFieldCfg{
							TestID:   testID,
							Label:    cn + "." + fk,
							Step:     1,
							Integer:  true,
							Scrub:    InspectorScrubField{ObjectID: objID, Field: "comp:" + cn + "." + fk, Step: 1, Integer: true},
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil && o.Components != nil {
									if cm, ok := o.Components[cn].(map[string]any); ok {
										if v, ok := anyToFloat64(cm[fk]); ok {
											return v
										}
									}
								}
								return 0
							},
							OnChange: func(v float64) {
								ed.report(ed.SetComponentField(objID, cn, fk, int(math.Round(v))))
							},
						}
						compRows = append(compRows, scrubFieldRow(ovKey, fk, labelID, "", cn, fk, cfg))
					case float64, float32:
						cfg := inspectorNumberFieldCfg{
							TestID:   testID,
							Label:    cn + "." + fk,
							Step:     0.1,
							Scrub:    InspectorScrubField{ObjectID: objID, Field: "comp:" + cn + "." + fk, Step: 0.1},
							Disabled: disabled,
							Value: func() float64 {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil && o.Components != nil {
									if cm, ok := o.Components[cn].(map[string]any); ok {
										if v, ok := anyToFloat64(cm[fk]); ok {
											return v
										}
									}
								}
								return 0
							},
							OnChange: func(v float64) {
								ed.report(ed.SetComponentField(objID, cn, fk, v))
							},
						}
						compRows = append(compRows, scrubFieldRow(ovKey, fk, labelID, "", cn, fk, cfg))
					default:
						ctrl := typedFieldControl(
							testID,
							cn+"."+fk,
							sample,
							disabled,
							func() any {
								_ = rev.Get()
								if o, _ := ed.FindResolvedObject(objID); o != nil && o.Components != nil {
									if cm, ok := o.Components[cn].(map[string]any); ok {
										return cm[fk]
									}
								}
								return nil
							},
							func(val any) {
								ed.report(ed.SetComponentField(objID, cn, fk, val))
							},
						)
						lbl := labelWithOverride(ovKey, withTooltip(core.Text{
							TestID:   labelID,
							Content:  fk,
							MaxLines: 1,
							Style:    core.Style{FlexShrink: 1, FontSize: 12, Color: specTextSecondary},
						}, ""))
						if isLocal {
							lbl = plainLabel(fk, labelID, "")
						}
						compRows = append(compRows, inspectorFieldRow(ed, rev, objID, cn, fk, testID, lbl, controlWithRevert(ovKey, fk, ctrl)))
					}
				}
				if len(compRows) > 0 || (!filtering && len(m) == 0) || (filtering && matchesInspectorFilter(q, compTitle, cn)) {
					fNames := fieldNames
					nodes = append(nodes, inspectorTitledSection(
						cx, ed, rev, "comp-"+cn, "inspector-comp-"+cn, compTitle, compDoc, len(compRows),
						func() int {
							n := 0
							for _, fn := range fNames {
								if _, bad := ed.inspectorFieldProblemFor(objID, cn, fn); bad {
									n++
								}
							}
							return n
						},
						func() int {
							n := 0
							for _, fn := range fNames {
								if _, ok := ed.inspectorReviewChangeFor(objID, cn, fn); ok {
									n++
								}
							}
							return n
						},
						remove, filtering, compRows,
					))
				}
			}
		}

		// CONNECTIONS section (shown unfiltered or when matching "connections").
		if !filtering || matchesInspectorFilter(q, ed.T("inspector.section.connections"), "", "connections", "signal", "event") {
			nodes = append(nodes, connectionsSection(cx, ed, rev, objID, disabled, sectionHeader, describedRow))
		}

		return core.View{
			Style:    core.Style{Gap: 8},
			Children: nodes,
		}
	})

	footer := core.Text{
		TestID:  "inspector-footer",
		Content: ed.T("inspector.footer"),
		Style: core.Style{
			FontSize:   11.5,
			LineHeight: 16.5,
			Color:      specTextMuted,
			Padding:    core.Edges{Top: 6},
		},
	}

	return core.View{
		Style: core.Style{Gap: 10},
		Children: []core.Node{
			header,
			inspectorAgentWorkingBannerView(cx, ed, rev, objID),
			filterBar,
			sectionsNode,
			addComponentControl(cx, ed, rev, objID, disabled),
			footer,
		},
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
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%q", x)
	case []string:
		return "[" + strings.Join(x, ", ") + "]"
	case sceneformat.Point:
		return fmt.Sprintf("{x: %g, y: %g}", x.X, x.Y)
	case sceneformat.Rect:
		return fmt.Sprintf("{x: %g, y: %g, width: %g, height: %g}", x.X, x.Y, x.Width, x.Height)
	case sceneformat.Circle:
		return fmt.Sprintf("{x: %g, y: %g, radius: %g}", x.X, x.Y, x.Radius)
	case []sceneformat.Point:
		return fmt.Sprintf("%d points", len(x))
	case [][2]float64:
		return formatPointListInput(x)
	default:
		return fmt.Sprint(v)
	}
}

func pointListFromAny(v any) [][2]float64 {
	switch xs := v.(type) {
	case [][2]float64:
		return xs
	case []any:
		out := make([][2]float64, 0, len(xs))
		for _, elem := range xs {
			pair, ok := elem.([]any)
			if !ok || len(pair) != 2 {
				continue
			}
			x, okX := anyToFloat64(pair[0])
			y, okY := anyToFloat64(pair[1])
			if okX && okY {
				out = append(out, [2]float64{x, y})
			}
		}
		return out
	default:
		return nil
	}
}

func anyToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func formatPointListInput(pts [][2]float64) string {
	if len(pts) == 0 {
		return ""
	}
	parts := make([]string, len(pts))
	for i, p := range pts {
		parts[i] = strconv.FormatFloat(p[0], 'g', -1, 64) + "," + strconv.FormatFloat(p[1], 'g', -1, 64)
	}
	return strings.Join(parts, "; ")
}

func parsePointListInput(text string) ([][2]float64, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, true
	}
	var out [][2]float64
	for part := range strings.SplitSeq(trimmed, ";") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		p = strings.Trim(p, "[]()")
		xy := strings.Split(p, ",")
		if len(xy) != 2 {
			return nil, false
		}
		x, errX := strconv.ParseFloat(strings.TrimSpace(xy[0]), 64)
		y, errY := strconv.ParseFloat(strings.TrimSpace(xy[1]), 64)
		if errX != nil || errY != nil || math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return nil, false
		}
		out = append(out, [2]float64{x, y})
	}
	return out, true
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
	case sceneformat.FieldPointList:
		return ui.Input{
			TestID:      testID,
			Label:       label,
			Placeholder: "-16,-16; 16,-16; 16,16; -16,16",
			Disabled:    disabled,
			Value: func() string {
				return formatPointListInput(pointListFromAny(get()))
			},
			OnSubmit: func(text string) {
				if pts, ok := parsePointListInput(text); ok {
					set(pts)
				}
			},
		}
	case sceneformat.FieldPoint:
		numInput := func(suffix, fieldLabel string, read func(sceneformat.Point) float64, write func(sceneformat.Point, float64) sceneformat.Point) core.Node {
			return ui.NumberInput{
				TestID:   testID + "-" + suffix,
				Label:    label + " " + fieldLabel,
				Step:     1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					p, _ := sceneformat.ParsePoint(get())
					return read(p)
				},
				OnChange: func(v float64) {
					p, _ := sceneformat.ParsePoint(get())
					set(write(p, v))
				},
			}
		}
		return ui.HStack{
			Gap: 4,
			Children: []core.Node{
				numInput("x", "X", func(p sceneformat.Point) float64 { return p.X }, func(p sceneformat.Point, v float64) sceneformat.Point { p.X = v; return p }),
				numInput("y", "Y", func(p sceneformat.Point) float64 { return p.Y }, func(p sceneformat.Point, v float64) sceneformat.Point { p.Y = v; return p }),
			},
		}
	case sceneformat.FieldRect:
		numInput := func(suffix, fieldLabel string, minVal float64, hasMin bool, read func(sceneformat.Rect) float64, write func(sceneformat.Rect, float64) sceneformat.Rect) core.Node {
			n := ui.NumberInput{
				TestID:   testID + "-" + suffix,
				Label:    label + " " + fieldLabel,
				Step:     1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					r, _ := sceneformat.ParseRect(get())
					return read(r)
				},
				OnChange: func(v float64) {
					if hasMin && v < minVal {
						v = minVal
					}
					r, _ := sceneformat.ParseRect(get())
					set(write(r, v))
				},
			}
			if hasMin {
				n.Min = minVal
			}
			return n
		}
		return core.View{
			Style: core.Style{Gap: 4},
			Children: []core.Node{
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						numInput("x", "X", 0, false, func(r sceneformat.Rect) float64 { return r.X }, func(r sceneformat.Rect, v float64) sceneformat.Rect { r.X = v; return r }),
						numInput("y", "Y", 0, false, func(r sceneformat.Rect) float64 { return r.Y }, func(r sceneformat.Rect, v float64) sceneformat.Rect { r.Y = v; return r }),
					},
				},
				ui.HStack{
					Gap: 4,
					Children: []core.Node{
						numInput("width", "Width", 0, true, func(r sceneformat.Rect) float64 { return r.Width }, func(r sceneformat.Rect, v float64) sceneformat.Rect { r.Width = v; return r }),
						numInput("height", "Height", 0, true, func(r sceneformat.Rect) float64 { return r.Height }, func(r sceneformat.Rect, v float64) sceneformat.Rect { r.Height = v; return r }),
					},
				},
			},
		}
	case sceneformat.FieldCircle:
		numInput := func(suffix, fieldLabel string, minVal float64, hasMin bool, read func(sceneformat.Circle) float64, write func(sceneformat.Circle, float64) sceneformat.Circle) core.Node {
			n := ui.NumberInput{
				TestID:   testID + "-" + suffix,
				Label:    label + " " + fieldLabel,
				Step:     1,
				Format:   func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) },
				Disabled: disabled,
				Value: func() float64 {
					c, _ := sceneformat.ParseCircle(get())
					return read(c)
				},
				OnChange: func(v float64) {
					if hasMin && v < minVal {
						v = minVal
					}
					c, _ := sceneformat.ParseCircle(get())
					set(write(c, v))
				},
			}
			if hasMin {
				n.Min = minVal
			}
			return n
		}
		return ui.HStack{
			Gap: 4,
			Children: []core.Node{
				numInput("x", "X", 0, false, func(c sceneformat.Circle) float64 { return c.X }, func(c sceneformat.Circle, v float64) sceneformat.Circle { c.X = v; return c }),
				numInput("y", "Y", 0, false, func(c sceneformat.Circle) float64 { return c.Y }, func(c sceneformat.Circle, v float64) sceneformat.Circle { c.Y = v; return c }),
				numInput("radius", "Radius", 0, true, func(c sceneformat.Circle) float64 { return c.Radius }, func(c sceneformat.Circle, v float64) sceneformat.Circle { c.Radius = v; return c }),
			},
		}
	case sceneformat.FieldPolygon:
		return ui.HStack{
			Gap: 6,
			Children: []core.Node{
				core.Text{
					TestID: testID,
					ContentFunc: func() string {
						pts, _ := sceneformat.ParsePolygon(get())
						return fmt.Sprintf("%d vertices", len(pts))
					},
					Style: core.Style{FontSize: 12},
				},
				ui.Button{
					TestID:   testID + "-add",
					Text:     "+ Vertex",
					Size:     ui.SizeSmall,
					Variant:  ui.ButtonSecondary,
					Disabled: disabled,
					OnPress: func() {
						pts, _ := sceneformat.ParsePolygon(get())
						next := slices.Clone(pts)
						var pt sceneformat.Point
						if n := len(next); n >= 2 {
							a, b := next[n-1], next[0]
							pt = sceneformat.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
						} else if n == 1 {
							pt = sceneformat.Point{X: next[0].X + 16, Y: next[0].Y}
						}
						set(append(next, pt))
					},
				},
			},
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
func meshOptions(ed *Editor) []ui.Option[string] {
	opts := []ui.Option[string]{{Value: "", Label: ed.T("inspector.none")}}
	for _, m := range sceneformat.MeshPrimitives() {
		opts = append(opts, ui.Option[string]{Value: m, Label: m})
	}
	return opts
}

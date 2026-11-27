package editor

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// spriteChoice is a sprite option of the inspector: an atlas frame.
type spriteChoice struct{ atlas, frame string }

func (c spriteChoice) label() string { return c.atlas + "/" + c.frame }

// spriteFieldsFiltered builds the inspector's sprite rows matching filterQuery, using compact
// scrubbable Origin X/Y boxes while keeping every existing TestID and override indicator intact.
func spriteFieldsFiltered(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	objID string,
	filterQuery, sectionTitle string,
	disabled func() bool,
	overrideFieldRow func(fieldKey, label string, control core.Node) core.Node,
	multiAxisRow func(label string, fieldKeys []string, rowTestID string, axes []inspectorNumberFieldCfg) core.Node,
) []core.Node {
	object := func() *sceneformat.ObjectDoc {
		_ = rev.Get()
		o, _ := ed.FindResolvedObject(objID)
		return o
	}
	current := func() spriteChoice {
		if o := object(); o != nil && o.Sprite != nil {
			return spriteChoice{o.Sprite.Atlas, o.Sprite.Frame}
		}
		return spriteChoice{}
	}
	setSprite := func(c spriteChoice) {
		ed.report(ed.SetObjectSprite(objID, &sceneformat.SpriteRef{Atlas: c.atlas, Frame: c.frame}))
	}

	var rows []core.Node

	if matchesInspectorFilter(filterQuery, sectionTitle, "", "sprite", "texture", ed.T("inspector.sprite")) {
		sprite := ui.Select[spriteChoice]{
			TestID:      "inspector-sprite",
			Label:       ed.T("inspector.sprite"),
			Placeholder: ed.T("inspector.none"),
			Searchable:  true,
			Disabled:    disabled,
			Style:       core.Style{Width: core.Pct(100)},
			OptionsFunc: func() []ui.Option[spriteChoice] {
				cur := current()
				var opts []ui.Option[spriteChoice]
				listed := false
				for _, a := range ed.Assets() {
					c := spriteChoice{a.Atlas, a.Frame}
					listed = listed || c == cur
					opts = append(opts, ui.Option[spriteChoice]{Value: c, Label: c.label(), Group: a.Atlas})
				}
				if cur != (spriteChoice{}) && !listed {
					opts = append([]ui.Option[spriteChoice]{{Value: cur, Label: cur.label()}}, opts...)
				}
				return opts
			},
			Value:    current,
			OnChange: setSprite,
			OnClear:  func() { ed.report(ed.SetObjectSprite(objID, nil)) },
		}
		fromBrowser := ui.IconButton{
			TestID: "inspector-sprite-use-asset",
			Icon:   lucide.Crosshair,
			Label:  ed.T("inspector.useAssetLabel"),
			Tooltip: func() string {
				_ = rev.Get()
				if ed.SelectedAsset == nil {
					return ed.T("inspector.selectAssetFirst")
				}
				return ed.T("inspector.useAsset", l10n.Param("key", ed.SelectedAsset.Key()))
			},
			Disabled: func() bool {
				_ = rev.Get()
				return ed.Playing() || ed.SelectedAsset == nil
			},
			OnPress: func() {
				if a := ed.SelectedAsset; a != nil {
					setSprite(spriteChoice{a.Atlas, a.Frame})
				}
			},
		}
		rows = append(rows, overrideFieldRow("sprite", ed.T("inspector.sprite"), withTrailing(sprite, fromBrowser)))
	}

	if matchesInspectorFilter(filterQuery, sectionTitle, "", "origin", "originX", "originY", ed.T("inspectorEditExtra.origin"), ed.T("inspector.originX"), ed.T("inspector.originY")) {
		originCfg := func(testID, label, axis, field string, axisColor core.Color, y bool) inspectorNumberFieldCfg {
			return inspectorNumberFieldCfg{
				TestID:    testID,
				Label:     label,
				AxisLabel: axis,
				AxisColor: axisColor,
				Step:      1,
				Scrub:     InspectorScrubField{ObjectID: objID, Field: field, Step: 1},
				Disabled:  disabled,
				Value: func() float64 {
					o := object()
					switch {
					case o == nil:
						return 0
					case y:
						return float64(o.OriginY)
					default:
						return float64(o.OriginX)
					}
				},
				OnChange: func(v float64) {
					o, _ := ed.FindResolvedObject(objID)
					if o == nil {
						return
					}
					if y {
						ed.report(ed.SetObjectOrigin(objID, o.OriginX, float32(v)))
					} else {
						ed.report(ed.SetObjectOrigin(objID, float32(v), o.OriginY))
					}
				},
			}
		}
		spec := ed.SpecPalette(cx)
		rows = append(rows, multiAxisRow(
			ed.T("inspectorEditExtra.origin"),
			[]string{"originX", "originY"},
			"inspector-origin",
			[]inspectorNumberFieldCfg{
				originCfg("inspector-origin-x", ed.T("inspector.originX"), "X", "originX", spec.AxisX, false),
				originCfg("inspector-origin-y", ed.T("inspector.originY"), "Y", "originY", spec.AxisY, true),
			},
		))
	}

	if matchesInspectorFilter(filterQuery, sectionTitle, "", "flip", "flipX", "flipY", ed.T("inspector.flip")) {
		flip := func(testID, label string, y bool) core.Node {
			return ui.Checkbox{
				TestID:   testID,
				Text:     label,
				Label:    label,
				Disabled: disabled,
				Checked: func() bool {
					o := object()
					return o != nil && ((y && o.FlipY) || (!y && o.FlipX))
				},
				OnChange: func(on bool) {
					o, _ := ed.FindResolvedObject(objID)
					if o == nil {
						return
					}
					if y {
						ed.report(ed.SetObjectFlip(objID, o.FlipX, on))
					} else {
						ed.report(ed.SetObjectFlip(objID, on, o.FlipY))
					}
				},
			}
		}
		rows = append(rows, overrideFieldRow("flipX", ed.T("inspector.flip"), ui.HStack{Gap: 12, Children: []core.Node{
			flip("inspector-flip-x", "X", false),
			flip("inspector-flip-y", "Y", true),
		}}))
	}

	if matchesInspectorFilter(filterQuery, sectionTitle, "", "material", ed.T("inspector.material")) {
		var materialsKey string
		var materials []string
		currentMaterial := func() string {
			if o := object(); o != nil {
				return o.Material
			}
			return ""
		}
		material := ui.Select[string]{
			TestID:      "inspector-material",
			Label:       ed.T("inspector.material"),
			Placeholder: ed.T("inspector.none"),
			Searchable:  true,
			Disabled: func() bool {
				return disabled() || (!ed.canLoadMaterials() && currentMaterial() == "")
			},
			Style: core.Style{Width: core.Pct(100)},
			OptionsFunc: func() []ui.Option[string] {
				cur := currentMaterial()
				if key := ed.ProjectPath + "\x00" + strconv.FormatUint(ed.filesRev, 10); key != materialsKey {
					materialsKey, materials = key, ed.projectMaterials()
				}
				var offered []string
				if ed.canLoadMaterials() {
					offered = materials
				}
				opts := make([]ui.Option[string], 0, len(offered)+1)
				listed := false
				for _, m := range offered {
					listed = listed || m == cur
					opts = append(opts, ui.Option[string]{Value: m, Label: m})
				}
				if cur != "" && !listed {
					opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
				}
				return opts
			},
			Value:    currentMaterial,
			OnChange: func(m string) { ed.report(ed.SetObjectMaterial(objID, m)) },
			OnClear:  func() { ed.report(ed.SetObjectMaterial(objID, "")) },
		}
		editMaterialBtn := ui.Button{
			TestID:  "inspector-material-edit",
			Text:    ed.T("inspector.editMaterial"),
			Size:    ui.SizeSmall,
			Variant: ui.ButtonSecondary,
			Disabled: func() bool {
				return disabled() || currentMaterial() == ""
			},
			OnPress: func() {
				if m := currentMaterial(); m != "" {
					ed.report(ed.OpenMaterial(m))
				}
			},
		}
		newMaterialBtn := ui.Button{
			TestID:  "inspector-material-new",
			Text:    ed.T("inspector.newMaterial"),
			Size:    ui.SizeSmall,
			Variant: ui.ButtonSecondary,
			Disabled: func() bool {
				return disabled() || ed.Project == nil || ed.ProjectPath == ""
			},
			OnPress: func() {
				_, err := ed.CreateObjectMaterial(objID, "")
				ed.report(err)
			},
		}
		rows = append(rows,
			overrideFieldRow("material", ed.T("inspector.material"), material),
			ui.HStack{Gap: 6, Children: []core.Node{editMaterialBtn, newMaterialBtn}},
		)
	}
	return rows
}

// particlesFields builds the inspector's particles rows for objID: the emitter YAML file, optional atlas override, and a button to open the emitter in the Particle Emitter panel.
func particlesFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, string, core.Node) core.Node) []core.Node {
	object := func() *sceneformat.ObjectDoc {
		_ = rev.Get()
		o, _ := ed.FindResolvedObject(objID)
		return o
	}
	var filesKey string
	var particleFiles []string
	currentFile := func() string {
		if o := object(); o != nil && o.Particles != nil {
			return o.Particles.File
		}
		return ""
	}
	currentAtlas := func() string {
		if o := object(); o != nil && o.Particles != nil {
			return o.Particles.Atlas
		}
		return ""
	}
	fileSelect := ui.Select[string]{
		TestID:      "inspector-particles-file",
		Label:       ed.T("inspectorEditExtra.particleEmitterFile"),
		Placeholder: ed.T("inspectorEditExtra.none"),
		Searchable:  true,
		Disabled: func() bool {
			return disabled() || (!ed.canLoadParticles() && currentFile() == "")
		},
		Style: core.Style{Width: core.Pct(100)},
		OptionsFunc: func() []ui.Option[string] {
			cur := currentFile()
			if key := ed.ProjectPath + "\x00" + strconv.FormatUint(ed.filesRev, 10); key != filesKey {
				filesKey, particleFiles = key, ed.projectParticles()
			}
			var offered []string
			if ed.canLoadParticles() {
				offered = particleFiles
			}
			opts := make([]ui.Option[string], 0, len(offered)+1)
			listed := false
			for _, f := range offered {
				listed = listed || f == cur
				opts = append(opts, ui.Option[string]{Value: f, Label: f})
			}
			if cur != "" && !listed {
				opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
			}
			return opts
		},
		Value: currentFile,
		OnChange: func(file string) {
			if file == "" {
				ed.report(ed.SetObjectParticles(objID, nil))
				return
			}
			ed.report(ed.SetObjectParticles(objID, &sceneformat.ParticlesRef{File: file, Atlas: currentAtlas()}))
		},
		OnClear: func() { ed.report(ed.SetObjectParticles(objID, nil)) },
	}
	atlasSelect := ui.Select[string]{
		TestID:      "inspector-particles-atlas",
		Label:       ed.T("inspectorEditExtra.particleAtlas"),
		Placeholder: ed.T("inspectorEditExtra.none"),
		Searchable:  true,
		Disabled: func() bool {
			return disabled() || currentFile() == ""
		},
		Style: core.Style{Width: core.Pct(100)},
		OptionsFunc: func() []ui.Option[string] {
			cur := currentAtlas()
			names := ed.ParticleAtlasNames()
			opts := make([]ui.Option[string], 0, len(names)+1)
			listed := false
			for _, a := range names {
				listed = listed || a == cur
				opts = append(opts, ui.Option[string]{Value: a, Label: a})
			}
			if cur != "" && !listed {
				opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
			}
			return opts
		},
		Value: currentAtlas,
		OnChange: func(atlas string) {
			if f := currentFile(); f != "" {
				ed.report(ed.SetObjectParticles(objID, &sceneformat.ParticlesRef{File: f, Atlas: atlas}))
			}
		},
		OnClear: func() {
			if f := currentFile(); f != "" {
				ed.report(ed.SetObjectParticles(objID, &sceneformat.ParticlesRef{File: f}))
			}
		},
	}
	editBtn := ui.Button{
		TestID:  "inspector-particles-edit",
		Text:    ed.T("inspectorEditExtra.editEmitter"),
		Size:    ui.SizeSmall,
		Variant: ui.ButtonSecondary,
		Disabled: func() bool {
			return disabled() || currentFile() == ""
		},
		OnPress: func() {
			o, _ := ed.FindResolvedObject(objID)
			if o == nil || o.Particles == nil || o.Particles.File == "" {
				return
			}
			if ed.Project != nil && ed.ProjectPath != "" {
				ed.report(ed.openParticleEmitterProjectFile(o.Particles.File, o.Particles.Atlas))
				return
			}
			if fr, ok := ed.Resolver.(sceneformat.FileResolver); ok {
				data, err := fr.ReadFile(o.Particles.File)
				if err != nil {
					ed.report(err)
					return
				}
				ed.report(ed.openParticleEmitter(data, "", o.Particles.Atlas))
			}
		},
	}
	return []core.Node{
		fieldRow("particles", ed.T("inspectorEditExtra.emitter"), fileSelect),
		fieldRow("", ed.T("inspectorEditExtra.atlas"), atlasSelect),
		editBtn,
	}
}

// withTrailing lays control out with trailing, such as a delete button, at its end.
func withTrailing(control, trailing core.Node) core.Node {
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
		Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{control}},
			trailing,
		},
	}
}

// removeButton is the inspector's small button that deletes the row it ends.
func removeButton(testID, label string, disabled func() bool, onPress func()) core.Node {
	return ui.IconButton{
		TestID:   testID,
		Icon:     lucide.X,
		Label:    label,
		Disabled: disabled,
		OnPress:  onPress,
	}
}

// propertyKinds are the kinds Add Property offers, in the order it lists them.
var propertyKinds = []sceneformat.FieldKind{sceneformat.FieldString, sceneformat.FieldInt, sceneformat.FieldFloat, sceneformat.FieldBool}

// addPropertyRow builds the inspector's Add Property row: a key, trimmed of surrounding whitespace, a kind and the button that adds the property, which Enter in the key presses too. A refused key, such as an empty one or one the object has, is reported in the status bar.
func addPropertyRow(cx *core.Scope, ed *Editor, objID string, disabled func() bool) core.Node {
	key := core.NewSignal(cx, "")
	kind := core.NewSignal(cx, sceneformat.FieldString)
	add := func() {
		if err := ed.AddProperty(objID, strings.TrimSpace(key.Get()), kind.Get()); err != nil {
			ed.report(err)
			return
		}
		key.Set("")
	}
	kinds := make([]ui.Option[sceneformat.FieldKind], len(propertyKinds))
	for i, k := range propertyKinds {
		kinds[i] = ui.Option[sceneformat.FieldKind]{Value: k, Label: k.String()}
	}
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4},
		Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{ui.Input{
				TestID:      "inspector-add-prop-key",
				Label:       ed.T("inspector.newPropertyKey"),
				Placeholder: ed.T("inspector.newProperty"),
				Disabled:    disabled,
				Value:       key.Get,
				OnChange:    key.Set,
				OnSubmit:    func(string) { add() },
			}}},
			ui.Select[sceneformat.FieldKind]{
				TestID:   "inspector-add-prop-kind",
				Label:    ed.T("inspector.newPropertyKind"),
				Options:  kinds,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pt(76)},
				Value:    kind.Get,
				OnChange: kind.Set,
			},
			ui.IconButton{
				TestID:   "inspector-add-prop",
				Icon:     lucide.Plus,
				Label:    ed.T("inspector.addProperty"),
				Disabled: disabled,
				OnPress:  add,
			},
		},
	}
}

// addComponentControl builds the inspector's Add Component bar and Ask to add button:
//   - A search/filter input (`inspector-add-component`) beside an **Ask to add** button
//     (`inspector-ask-to-add`) that populates the agent composer with
//     `"Add a component to <object> that "`.
//   - Registered component types objID lacks grouped under GAMEPLAY, PHYSICS, and AUDIO AND VISUAL.
func addComponentControl(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool) core.Node {
	query := core.NewSignal(cx, "")
	add := func(name string) {
		if err := ed.AddComponent(objID, name); err != nil {
			ed.report(err)
			return
		}
		query.Set("")
	}
	submit := func(text string) {
		name := strings.TrimSpace(text)
		if _, registered := ed.components.Lookup(name); !registered {
			if m := ed.addableComponents(objID, name); len(m) == 1 && name != "" {
				name = m[0]
			}
		}
		add(name)
	}
	groupTitle := func(cat ComponentCategory) (string, string) {
		switch cat {
		case ComponentCategoryPhysics:
			return "physics", ed.T("inspector.group.physics")
		case ComponentCategoryAudioVisual:
			return "audio-visual", ed.T("inspector.group.audioVisual")
		default:
			return "gameplay", ed.T("inspector.group.gameplay")
		}
	}

	spec := ed.SpecPalette(cx)
	askBtn := core.View{
		TestID:   "inspector-ask-to-add",
		Role:     core.RoleButton,
		Label:    ed.T("inspector.askToAdd"),
		Disabled: disabled,
		Style: core.Style{
			Direction:    core.Row,
			AlignItems:   core.AlignCenter,
			Height:       core.Pt(30),
			Padding:      core.Symmetric(0, 10),
			CornerRadius: 8,
			Background:   spec.Agent.Fill,
			BorderWidth:  1,
			BorderColor:  spec.Agent.Edge,
			Gap:          5,
			FlexShrink:   0,
			Hover:        &core.Style{Background: spec.Raised},
			FocusVisible: spec.FocusRing(2),
		},
		OnPress: func(core.PressEvent) {
			if disabled != nil && disabled() {
				return
			}
			ed.AskToAddComponent(objID)
		},
		Children: []core.Node{
			ui.Icon(lucide.Sparkles, 12, func() core.Color { return spec.Agent.Fg }),
			core.Text{
				Content:  ed.T("inspector.askToAdd"),
				MaxLines: 1,
				Style: core.Style{
					FontSize: 11.5,
					Color:    spec.TextSecondary,
				},
			},
		},
	}

	return core.View{
		Style: core.Style{
			Gap:         8,
			Padding:     core.Edges{Top: 8},
			Borders:     core.Edges{Top: 1},
			BorderColor: spec.Line,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{
					Direction:  core.Row,
					AlignItems: core.AlignCenter,
					Gap:        6,
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							ui.Input{
								TestID:      "inspector-add-component",
								Label:       ed.T("inspector.addComponent"),
								Placeholder: ed.T("inspector.addComponent"),
								Disabled:    disabled,
								Value:       query.Get,
								OnChange:    query.Set,
								OnSubmit:    submit,
							},
						},
					},
					askBtn,
				},
			},
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				dynSpec := ed.SpecPalette(cx)
				groups := ed.AddableComponentGroups(objID, query.Get())
				if len(groups) == 0 {
					return nil
				}
				var children []core.Node
				for _, g := range groups {
					slug, label := groupTitle(g.Category)
					buttons := make([]core.Node, len(g.Names))
					for i, n := range g.Names {
						name := n
						button := core.Node(ui.Button{
							TestID:   "inspector-add-component-" + name,
							Text:     name,
							Size:     ui.SizeSmall,
							Variant:  ui.ButtonSecondary,
							Disabled: disabled,
							OnPress:  func() { add(name) },
						})
						if t, ok := ed.lookupComponentType(name); ok {
							button = withTooltip(button, t.Doc)
						}
						buttons[i] = button
					}
					children = append(children, core.View{
						TestID: "inspector-add-group-" + slug,
						Style:  core.Style{Gap: 4},
						Children: []core.Node{
							core.Text{
								Content: label,
								Style: core.Style{
									FontSize:      10.5,
									FontWeight:    core.FontWeightSemibold,
									LetterSpacing: 0.7,
									Color:         dynSpec.TextFaint,
								},
							},
							ui.Wrap{Children: buttons},
						},
					})
				}
				return core.View{
					Style: core.Style{
						Gap:          8,
						Padding:      core.All(8),
						CornerRadius: 8,
						Background:   dynSpec.Raised,
						BorderWidth:  1,
						BorderColor:  dynSpec.LineRaised,
					},
					Children: children,
				}
			}),
		},
	}
}

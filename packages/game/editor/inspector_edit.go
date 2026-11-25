package editor

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// spriteChoice is a sprite option of the inspector: an atlas frame.
type spriteChoice struct{ atlas, frame string }

func (c spriteChoice) label() string { return c.atlas + "/" + c.frame }

// spriteFields builds the inspector's sprite rows for objID: the atlas frame, picked from the frames the asset browser lists or taken from its selection, the origin, the flips and the material.
func spriteFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, string, core.Node) core.Node) []core.Node {
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
			// A frame the resolver does not list, such as one whose atlas failed to load, still shows as the object's.
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

	origin := func(testID, label string, y bool) core.Node {
		return ui.NumberInput{
			TestID:   testID,
			Label:    label,
			Step:     1,
			Disabled: disabled,
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
	flip := func(testID, label string, y bool) core.Node {
		return ui.Checkbox{
			TestID:   testID,
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

	// The project's material files are listed again only when the editor changes which files the project holds. A resolver that cannot load them gets none, since naming one would leave the scene unable to build; the field then stays enabled only to clear a material the object already names.
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

	return []core.Node{
		fieldRow("sprite", ed.T("inspector.sprite"), withTrailing(sprite, fromBrowser)),
		fieldRow("originX", ed.T("inspector.originX"), origin("inspector-origin-x", ed.T("inspector.originX"), false)),
		fieldRow("originY", ed.T("inspector.originY"), origin("inspector-origin-y", ed.T("inspector.originY"), true)),
		fieldRow("flipX", ed.T("inspector.flip"), ui.HStack{Gap: 12, Children: []core.Node{
			flip("inspector-flip-x", "X", false),
			flip("inspector-flip-y", "Y", true),
		}}),
		fieldRow("material", ed.T("inspector.material"), material),
		ui.HStack{Gap: 6, Children: []core.Node{editMaterialBtn, newMaterialBtn}},
	}
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
		Label:       "Particle Emitter File",
		Placeholder: "None",
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
		Label:       "Particle Atlas",
		Placeholder: "None",
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
		Text:    "Edit Emitter",
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
		fieldRow("particles", "Emitter", fileSelect),
		fieldRow("", "Atlas", atlasSelect),
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

// addComponentControl builds the inspector's Add Component control: a field that filters the registered component types objID lacks, a button per type that adds it with its registered defaults, and Enter, which adds the type the field names, or the one type it matches. A refused name, such as a component the object has, is reported in the status bar.
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
	return core.View{
		Style: core.Style{Gap: 6},
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
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = rev.Get()
				names := ed.addableComponents(objID, query.Get())
				buttons := make([]core.Node, len(names))
				for i, n := range names {
					button := core.Node(ui.Button{
						TestID:   "inspector-add-component-" + n,
						Text:     n,
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonSecondary,
						Disabled: disabled,
						OnPress:  func() { add(n) },
					})
					if t, ok := ed.components.Lookup(n); ok {
						button = withTooltip(button, t.Doc)
					}
					buttons[i] = button
				}
				return ui.Wrap{Children: buttons}
			}),
		},
	}
}

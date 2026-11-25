package editor

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// spriteChoice is a sprite option of the inspector: an atlas frame.
type spriteChoice struct{ atlas, frame string }

func (c spriteChoice) label() string { return c.atlas + "/" + c.frame }

// spriteFields builds the inspector's sprite rows for objID: the atlas frame, picked from the frames the asset browser lists or taken from its selection, the origin, the flips and the material.
func spriteFields(ed *Editor, rev *core.Signal[uint64], objID string, disabled func() bool, fieldRow func(string, core.Node) core.Node) []core.Node {
	object := func() *sceneformat.ObjectDoc {
		_ = rev.Get()
		o, _, _ := ed.FindObject(objID)
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
		Label:       "Sprite",
		Placeholder: "None",
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
		Label:  "Use the asset browser's selected frame",
		Tooltip: func() string {
			_ = rev.Get()
			if ed.SelectedAsset == nil {
				return "Select a frame in the asset browser first"
			}
			return "Use " + ed.SelectedAsset.Key()
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
				o, _, _ := ed.FindObject(objID)
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
				o, _, _ := ed.FindObject(objID)
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

	// The project's material files are listed again only when the editor changes which files the project holds.
	var materialsKey string
	var materials []string
	material := ui.Select[string]{
		TestID:      "inspector-material",
		Label:       "Material",
		Placeholder: "None",
		Searchable:  true,
		Disabled:    disabled,
		Style:       core.Style{Width: core.Pct(100)},
		OptionsFunc: func() []ui.Option[string] {
			cur := ""
			if o := object(); o != nil {
				cur = o.Material
			}
			if key := ed.ProjectPath + "\x00" + strconv.FormatUint(ed.filesRev, 10); key != materialsKey {
				materialsKey, materials = key, ed.projectMaterials()
			}
			opts := make([]ui.Option[string], 0, len(materials)+1)
			listed := false
			for _, m := range materials {
				listed = listed || m == cur
				opts = append(opts, ui.Option[string]{Value: m, Label: m})
			}
			if cur != "" && !listed {
				opts = append([]ui.Option[string]{{Value: cur, Label: cur}}, opts...)
			}
			return opts
		},
		Value: func() string {
			if o := object(); o != nil {
				return o.Material
			}
			return ""
		},
		OnChange: func(m string) { ed.report(ed.SetObjectMaterial(objID, m)) },
		OnClear:  func() { ed.report(ed.SetObjectMaterial(objID, "")) },
	}

	return []core.Node{
		fieldRow("Sprite", withTrailing(sprite, fromBrowser)),
		fieldRow("Origin X", origin("inspector-origin-x", "Origin X", false)),
		fieldRow("Origin Y", origin("inspector-origin-y", "Origin Y", true)),
		fieldRow("Flip", ui.HStack{Gap: 12, Children: []core.Node{
			flip("inspector-flip-x", "X", false),
			flip("inspector-flip-y", "Y", true),
		}}),
		fieldRow("Material", material),
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

// addPropertyRow builds the inspector's Add Property row: a key, a kind and the button that adds the property, which Enter in the key presses too. A refused key, such as an empty one or one the object has, is reported in the status bar.
func addPropertyRow(cx *core.Scope, ed *Editor, objID string, disabled func() bool) core.Node {
	key := core.NewSignal(cx, "")
	kind := core.NewSignal(cx, sceneformat.FieldString)
	add := func() {
		if err := ed.AddProperty(objID, key.Get(), kind.Get()); err != nil {
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
				Label:       "New property key",
				Placeholder: "New property",
				Disabled:    disabled,
				Value:       key.Get,
				OnChange:    key.Set,
				OnSubmit:    func(string) { add() },
			}}},
			ui.Select[sceneformat.FieldKind]{
				TestID:   "inspector-add-prop-kind",
				Label:    "New property kind",
				Options:  kinds,
				Disabled: disabled,
				Style:    core.Style{Width: core.Pt(76)},
				Value:    kind.Get,
				OnChange: kind.Set,
			},
			ui.IconButton{
				TestID:   "inspector-add-prop",
				Icon:     lucide.Plus,
				Label:    "Add property",
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
				Label:       "Add component",
				Placeholder: "Add component",
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

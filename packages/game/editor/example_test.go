package editor_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// props is the atlas the example scene's sprites come from.
func props() sceneformat.AtlasMap {
	return sceneformat.AtlasMap{"props": &sprite.Atlas{
		Image:  core.ImageSource{URI: "asset:props.png"},
		Frames: map[string]sprite.Frame{"crate": {Name: "crate", W: 16, H: 16, Width: 16, Height: 16}},
	}}
}

// An Editor changes a scene document through undoable commands and saves it back as YAML; the scene it builds for the viewport follows every change.
func ExampleEditor() {
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "yard",
		Layers:  []sceneformat.LayerDoc{{Name: "props", Space: sceneformat.SpaceWorld}},
	}
	ed, err := editor.New(doc, props())
	if err != nil {
		fmt.Println(err)
		return
	}
	id, err := ed.PlaceObject("props", sceneformat.ObjectDoc{X: 32, Y: 48, Sprite: &sceneformat.SpriteRef{Atlas: "props", Frame: "crate"}})
	if err != nil {
		fmt.Println(err)
		return
	}
	ed.MoveObject(id, 64, 48)
	fmt.Println(ed.Built.Object(id).Sprite.X, ed.History.UndoLabel())

	ed.Undo()
	fmt.Println(ed.Built.Object(id).Sprite.X, ed.History.CanRedo())
	ed.Redo()

	data, err := ed.SaveYAML()
	if err != nil {
		fmt.Println(err)
		return
	}
	saved, _ := sceneformat.Parse(data)
	fmt.Println(saved.Layers[0].Objects[0].X)
	// Output:
	// 64 Move crate_1
	// 32 true
	// 64
}

// rename is a Command that changes a string and can change it back.
type rename struct {
	target   *string
	from, to string
}

func (r *rename) Do()           { *r.target = r.to }
func (r *rename) Undo()         { *r.target = r.from }
func (r *rename) Label() string { return "Rename to " + r.to }

// A History runs Commands and undoes and redoes them in order; executing a new command drops the ones that were undone.
func ExampleHistory() {
	name := "Level 1"
	h := editor.NewHistory()
	h.Execute(&rename{&name, name, "Caves"})
	h.Execute(&rename{&name, "Caves", "Deep Caves"})
	h.Undo()
	fmt.Println(name, h.RedoLabel())
	h.Execute(&rename{&name, name, "Crypt"})
	fmt.Println(name, h.CanRedo(), h.Len())
	// Output:
	// Caves Rename to Deep Caves
	// Crypt false 2
}

// A plugin adds a panel, an inspector for its own component and a viewport tool to the editor. The editor binary registers it, and a project turns it on by naming it under editor.plugins in game.yaml.
func ExampleEditor_RegisterPlugin() {
	health := editor.Plugin{
		Name: "health",
		Panels: []editor.Panel{{
			ID:    "overview",
			Title: "Health",
			Dock:  editor.DockRight,
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
				return core.Text{ContentFunc: func() string {
					_ = rev.Get()
					hp, ok := ed.ComponentField("crate", "health", "hp")
					if !ok {
						return "crate: no health"
					}
					return fmt.Sprint("crate: ", hp, " hp")
				}}
			},
		}},
		Inspectors: []editor.Inspector{{
			Component: "health",
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64], id string) core.Node {
				return ui.Button{
					Text: "Heal",
					// Like the generic fields, the control is off while playing and shows a refused edit in the status bar.
					Disabled: func() bool {
						_ = rev.Get()
						return ed.Playing()
					},
					OnPress: func() {
						ed.Report(ed.SetComponentField(id, "health", "hp", 10))
					},
				}
			},
		}},
	}

	dir, err := os.MkdirTemp("", "plugin-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	gameYAML := "version: 1\nname: Yard\nid: dev.example.yard\nappVersion: 1.0.0\nwindow: {width: 640, height: 360}\neditor:\n  plugins: [health, terrain]\n"
	if err := os.WriteFile(filepath.Join(dir, project.FileName), []byte(gameYAML), 0o644); err != nil {
		fmt.Println(err)
		return
	}

	ed, err := editor.New(nil, props())
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := ed.RegisterPlugin(health); err != nil {
		fmt.Println(err)
		return
	}
	if err := ed.OpenProject(dir); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("on:", ed.ActivePlugins(), "missing:", ed.MissingPlugins())

	// Turning a plugin off is an undoable edit of game.yaml.
	if err := ed.SetPluginEnabled("health", false); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("on:", ed.ActivePlugins(), "project lists:", ed.Project.Editor.Plugins)
	ed.Undo()
	fmt.Println("on:", ed.ActivePlugins())
	// Output:
	// on: [health] missing: [terrain]
	// on: [] project lists: [terrain]
	// on: [health]
}

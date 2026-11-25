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

// The tile palette lists the tiles of the active layer's tileset in ID order; the tile chosen in it is the one PaintTile writes into the layer's tilemap.
func ExampleEditor_TilePalette() {
	tiles := sceneformat.AtlasMap{"cave": &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:cave.png"},
		Frames: map[string]sprite.Frame{
			"1":  {Name: "1", W: 16, H: 16, Width: 16, Height: 16},
			"2":  {Name: "2", X: 16, W: 16, H: 16, Width: 16, Height: 16},
			"10": {Name: "10", X: 32, W: 16, H: 16, Width: 16, Height: 16},
		},
	}}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "cave",
		Layers: []sceneformat.LayerDoc{{Name: "ground", Space: sceneformat.SpaceWorld, Tilemap: &sceneformat.TilemapDoc{
			TileWidth: 16, TileHeight: 16, Width: 3, Height: 1, Tileset: "cave", Data: []int{0, 0, 0},
		}}},
	}
	ed, err := editor.New(doc, tiles)
	if err != nil {
		fmt.Println(err)
		return
	}
	palette := ed.TilePalette()
	for _, t := range palette.Tiles {
		fmt.Print(t.ID, " ")
	}
	fmt.Println("from", palette.Tileset)

	ed.SetActiveTileID(palette.Tiles[2].ID)
	if err := ed.PaintTile("ground", 1, 0, ed.ActiveTileID); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(ed.Doc.Layers[0].Tilemap.Data)
	// Output:
	// 1 2 10 from cave
	// [0 10 0]
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

// AddComponent gives an object a registered component with every field at its default, and RemoveComponent takes it away again with the connections that need it; both are undoable steps.
func ExampleEditor_AddComponent() {
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
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{Name: "crate", Fields: []sceneformat.Field{
		{Name: "weight", Kind: sceneformat.FieldFloat, Default: 2.5},
		{Name: "breakable", Kind: sceneformat.FieldBool},
	}})
	if err := ed.SetComponentRegistry(reg); err != nil {
		fmt.Println(err)
		return
	}
	id, _ := ed.PlaceObject("props", sceneformat.ObjectDoc{Sprite: &sceneformat.SpriteRef{Atlas: "props", Frame: "crate"}})

	if err := ed.AddComponent(id, "crate"); err != nil {
		fmt.Println(err)
		return
	}
	obj, _, _ := ed.FindObject(id)
	fmt.Println(obj.Components["crate"])
	fmt.Println(ed.AddComponent(id, "crate"))

	ed.SetObjectFlip(id, true, false)
	ed.RemoveComponent(id, "crate")
	fmt.Println(obj.Components, obj.FlipX)
	ed.Undo()
	ed.Undo()
	fmt.Println(len(obj.Components), obj.FlipX)
	// Output:
	// map[breakable:false weight:2.5]
	// editor: object "crate_1" already has component "crate"
	// map[] true
	// 1 false
}

// The 2D view is the editor's own camera: panning and zooming it reaches any part of a level without touching the scene's camera, and zooming keeps the world point under the pointer fixed.
func ExampleEditor_ZoomView2DAt() {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		VirtualWidth:  320,
		VirtualHeight: 180,
		Camera:        &sceneformat.CameraDoc{X: 160, Y: 90},
		Layers:        []sceneformat.LayerDoc{{Name: "level", Space: sceneformat.SpaceWorld}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	ed.SetCanvasSize(640, 360)
	fmt.Println(ed.View2D(), ed.View2DFollowsCamera())

	ed.PanView2D(-2000, 0) // drag the world 2000 points to the left
	fmt.Println(ed.CanvasToWorld(320, 180))

	ed.ZoomView2DAt(100, 50, 2)
	fmt.Println(ed.CanvasToWorld(100, 50))
	fmt.Println(ed.View2D().Zoom, doc.Camera.X)

	ed.ResetView2D()
	fmt.Println(ed.View2D(), ed.View2DFollowsCamera())
	// Output:
	// {160 90 1 0} true
	// 2160 90
	// 1940 -40
	// 2 160
	// {160 90 1 0} true
}

// ReparentObject moves an object under another one as a single undoable step, rewriting its local transform so it stays where it was drawn.
func ExampleEditor_ReparentObject() {
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{Name: "props", Objects: []sceneformat.ObjectDoc{
			{ID: "cart", X: 100, Y: 40},
			{ID: "crate", X: 120, Y: 30},
		}}},
	}
	ed, err := editor.New(doc, props())
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := ed.ReparentObject("crate", "cart", -1); err != nil {
		fmt.Println(err)
		return
	}
	crate, _, _ := ed.FindObject("crate")
	world := ed.Built.Object("crate").World
	fmt.Println(crate.X, crate.Y, world.Tx, world.Ty, ed.History.UndoLabel())

	// The cart carries the crate now.
	ed.MoveObject("cart", 0, 0)
	world = ed.Built.Object("crate").World
	fmt.Println(world.Tx, world.Ty)

	ed.Undo()
	ed.Undo()
	fmt.Println(len(ed.Doc.Layers[0].Objects))
	// Output:
	// 20 -10 120 30 Reparent crate under cart
	// 20 -10
	// 2
}

package sceneformat_test

import (
	"bytes"
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const room = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v1.json
version: 1
name: cave
layers:
  - name: props
    objects:
      - id: chest
        x: 120
        "y": 64
        sprite:
          atlas: props
          frame: chest
      - id: door
        x: 200
        "y": 64
        sprite:
          atlas: props
          frame: door
`

// Parse reads a scene file and refuses unknown fields; Encode writes it back in a fixed form, so loading and saving a scene leaves it unchanged.
func ExampleParse() {
	doc, err := sceneformat.Parse([]byte(room))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(doc.Name, len(doc.Layers[0].Objects))
	out, err := sceneformat.Encode(doc)
	fmt.Println(err, bytes.Equal(out, []byte(room)))

	_, err = sceneformat.Parse([]byte("version: 1\nname: cave\ncolour: red\n"))
	fmt.Println(err != nil)
	// Output:
	// cave 2
	// <nil> true
	// true
}

// Build turns a parsed scene into sprites ready to draw, resolving each sprite's atlas through an AssetResolver such as an AtlasMap.
func ExampleBuild() {
	doc, _ := sceneformat.Parse([]byte(room))
	props, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:props.png"}, []byte(`{"frames": {
		"chest": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}},
		"door": {"frame": {"x": 16, "y": 0, "w": 16, "h": 32}, "sourceSize": {"w": 16, "h": 32}}
	}, "meta": {"scale": "1"}}`))
	if err != nil {
		fmt.Println(err)
		return
	}
	built, err := sceneformat.Build(doc, sceneformat.AtlasMap{"props": props})
	if err != nil {
		fmt.Println(err)
		return
	}
	door := built.Object("door")
	fmt.Println(door.Layer, door.Sprite.X, door.Sprite.Y, door.Sprite.Frame.H)
	// Output:
	// props 200 64 32
}

// A scene file connects a button's pressed event to a door's open handler; the game registers what the components do once, and Connect wires every connection the file declares.
func ExampleConnect() {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed", Doc: "The player pressed the button."}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "door",
		Handlers: []sceneformat.Handler{{
			Name:   "open",
			Params: []sceneformat.Field{{Name: "speed", Kind: sceneformat.FieldFloat, Default: 1.0}},
			Func: func(c sceneformat.Call) {
				fmt.Printf("%s opens %s at speed %v\n", c.Source.ID, c.Target.ID, c.Args["speed"])
			},
		}},
	})

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: props
    objects:
      - {id: button, components: {button: {}}}
      - {id: door, components: {door: {}}}
connections:
  - {from: button, event: pressed, to: door, handler: open, args: {speed: 2}}
`), sceneformat.ParseOptions{File: "hall.scene.yaml", Components: reg})
	if err != nil {
		fmt.Println(err)
		return
	}
	signals, err := sceneformat.Connect(doc, reg, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	// The game's input code emits the event when the player presses the button.
	fmt.Println(signals.Emit("button", "pressed", nil))

	_, err = sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: props
    objects:
      - {id: button, components: {button: {}}}
      - {id: door, components: {door: {}}}
connections:
  - {from: button, event: pressed, to: door, handler: opne}
`), sceneformat.ParseOptions{File: "hall.scene.yaml", Components: reg})
	fmt.Println(err)
	// Output:
	// button opens door at speed 2
	// <nil>
	// hall.scene.yaml:8:55: sceneformat: connection 0 ("button" pressed -> "door" opne): no registered component of object "door" declares handler "opne" (it has: door.open)
}

// SpawnConnected spawns a scene's objects as entities and connects them; events enqueued during an update step run when the step flushes them, in order, and the handlers see the entities at both ends.
func ExampleSpawnConnected() {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "door",
		Handlers: []sceneformat.Handler{{
			Name: "open",
			Func: func(c sceneformat.Call) {
				fmt.Printf("%s opens %s (entity %v), press %v\n", c.Source.ID, c.Target.ID, c.TargetEntity.Index(), c.Payload)
			},
		}},
	})
	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: props
    objects:
      - {id: button, components: {button: {}}}
      - {id: door, components: {door: {}}}
connections:
  - {from: button, event: pressed, to: door, handler: open}
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		fmt.Println(err)
		return
	}
	world := entities.NewWorld()
	spawned, signals, err := sceneformat.SpawnConnected(doc, world, reg, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	// During the step, the game's input code enqueues the presses instead of running the handlers at once.
	_ = signals.EnqueueEntity(spawned["button"], "pressed", 1)
	_ = signals.EnqueueEntity(spawned["button"], "pressed", 2)
	fmt.Println("pending:", signals.Pending())
	// At the end of the step, such as at the end of game.Loop's Update, the queue runs.
	fmt.Println(signals.Flush())
	// Output:
	// pending: 2
	// button opens door (entity 1), press 1
	// button opens door (entity 1), press 2
	// <nil>
}

// A layered tilemap places the tiles of a YAML tileset in named tile layers. FSAssets reads the tileset from the project's files, and Build makes the layer a tilemap.Tilemap that answers collision queries.
func ExampleFSAssets() {
	project := fstest.MapFS{
		"tilesets/cave.tileset.yaml": {Data: []byte("version: 1\ntileWidth: 16\ntileHeight: 16\nimage: tiles/cave.png\ncolumns: 2\ntileCount: 2\ntiles:\n  - id: 0\n    solid: true\n")},
		"level.scene.yaml": {Data: []byte(`version: 2
layers:
  - name: ground
    tilemap:
      tileWidth: 16
      tileHeight: 16
      width: 3
      height: 1
      tilesetFile: tilesets/cave.tileset.yaml
      tileLayers:
        - name: floor
          rows:
            - 0 1h 0
`)},
	}
	assets := sceneformat.FSAssets{FS: project}
	doc, err := assets.Scene("level.scene.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	built, err := sceneformat.Build(doc, assets)
	if err != nil {
		fmt.Println(err)
		return
	}
	m := built.Layer("ground").Map
	fmt.Println(m.Layers[0].Name, m.FlagsAt(0, 0) == tilemap.TileSolid, m.FlagsAt(1, 0))
	hit, _ := m.Raycast(core.Point{X: 24, Y: 8}, core.Point{X: 60, Y: 8}, tilemap.TileSolid)
	fmt.Println(hit.Col, hit.Point)
	// Output:
	// floor true 0
	// 2 {32 8}
}

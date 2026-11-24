package sceneformat_test

import (
	"bytes"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
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

# Coming from GameMaker

A map from GameMaker's rooms, objects, events, sprites, sequences and export to this layer, with the page that covers each one.

The biggest change is where the game logic lives. In GameMaker you write GML in an object's events and place instances in a room; here a game is Go code inside an NSGo component, rooms are YAML scene files you write by hand or save from the [editor](editor.md), and the code that runs every step is a system on a fixed-step loop instead of a Step event per object.

## At a glance

| GameMaker | Here | Guide |
| --- | --- | --- |
| Step event, game speed | `game.Loop.Update(dt)` with a fixed `dt`, `Loop.Rate` | [Loop](loop.md) |
| Draw event | `game.Loop.Draw(alpha)` and the `core.Canvas` it redraws | [Loop](loop.md) |
| Draw GUI event | A `scene.Scene` layer in `SpaceScreen`, or [gameui](gameui.md) widgets | [scene](scene.md) |
| Room, room layers | YAML scene document (`sceneformat`) with ordered layers, built by `sceneformat.Build` | [Editor & Scene Format](editor.md) |
| `room_goto`, room transitions | `scene.Manager.Switch` with a transition; [rooms](rooms.md) for connected rooms with camera bounds | [scene](scene.md) |
| Object, parent object | A component type registered for scene files and a prefab; data in an `entities.World` | [Editor & Scene Format](editor.md#component-types), [Entities](entities.md) |
| Object variables, Variable Definitions | A scene object's `properties` and its component fields | [Editor & Scene Format](editor.md) |
| `instance_create_layer`, `instance_destroy` | `entities.Prefab` `Spawn`, `World.Spawn` and `World.Destroy` | [Entities](entities.md) |
| Global variables | Plain Go values made once in the root component and passed to the scenes that need them | [Coming from Godot](from-godot.md#scenes) |
| Keyboard, gamepad and touch checks | `input.Actions` with `Pressed` and `JustPressed` on named actions | [input](input.md) |
| Sprite, sub-images | `sprite.LoadAtlas` frames and animations | [sprite](sprite.md) |
| Collision mask | The built-in `collider2d` component, or `physics` shapes in Go | [physics](physics.md#authoring-in-scene-files) |
| Collision event | The built-in `trigger2d` component's `enter`, `stay` and `exit` events, wired by a scene's `connections` | [Editor & Scene Format](editor.md#connections-490) |
| Alarm events | `flow.Timers` in Go, or the built-in `timer` component in a scene | [flow](flow.md), [Editor & Scene Format](editor.md#built-in-timer-component-1362) |
| Tile sets, auto tiles | Tiled maps through `tilemap.LoadTiled`, terrains painted with `SetTerrain` | [tilemap](tilemap.md) |
| Sprite animation by state | `*.states.yaml` sprite state machines | [animclip](animclip.md#sprite-state-machines) |
| Sequences | `animclip` clips played through `tween.Timeline`; video cutscenes in [cutscene](cutscene.md) | [animclip](animclip.md) |
| Particle systems | `particles.Emitter` | [particles](particles.md) |
| Audio groups, `audio_play_sound` | [sound](sound.md), with buses declared in `game.yaml` | [sound](sound.md), [Project File](project.md) |
| `ini_open`, buffers for save files | `save` slots in versioned JSON with migrations | [save](save.md) |
| Game Options, target platforms | `game.yaml` and `nsgo.yaml` | [Project File](project.md) |
| Create Executable | `go tool game release` | [Building and Shipping](shipping.md) |

## First project

Start from a template close to the game you have: `go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame` writes a platformer with rooms, a controller and a checkpoint ([guide](template-platformer2d.md)), and `new -list` names the others ([Templates](templates.md)). Open the project with `go tool game editor`: its 2D viewport is the room editor, its inspector edits an object's components and properties, and the files it saves are the same YAML the game loads.

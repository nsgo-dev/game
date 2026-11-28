# Coming from Construct

A map from Construct's layouts, object types, event sheets and behaviors to this layer, with the page that covers each one.

The biggest change is that there are no event sheets. In Construct you add conditions and actions to an event sheet and attach behaviors to object types; here a game is Go code inside an NSGo component, layouts are YAML scene files you write by hand or save from the [editor](editor.md), and the rules an event sheet holds become Go systems that run each fixed step, with a scene file's `connections` for the simple "when this happens, do that" wiring.

## At a glance

| Construct | Here | Guide |
| --- | --- | --- |
| Layout, layers | YAML scene document (`sceneformat`) with ordered layers | [Editor & Scene Format](editor.md) |
| Go to layout | `scene.Manager.Switch` with a transition | [scene](scene.md) |
| Object type, instances | A component type registered for scene files and a prefab; data in an `entities.World` | [Editor & Scene Format](editor.md#component-types), [Entities](entities.md) |
| Instance variables | A scene object's `properties` and its component fields | [Editor & Scene Format](editor.md) |
| Families | Systems that query every entity with the components they share | [Entities](entities.md) |
| Event sheet, conditions and actions | Go systems on `game.Loop`; a scene's `connections` from an object's event to a handler | [Loop](loop.md), [Editor & Scene Format](editor.md#connections-490) |
| Every tick | `game.Loop.Update(dt)` with a fixed `dt` | [Loop](loop.md) |
| Global variables | Plain Go values made once in the root component and passed to the scenes that need them | [Coming from Godot](from-godot.md#scenes) |
| Functions, custom actions | Go functions and methods | — |
| Keyboard, Mouse, Touch and Gamepad objects | `input.Actions` with named actions and axes | [input](input.md) |
| Sprite, animations | `sprite.LoadAtlas` frames and animations, `*.states.yaml` sprite state machines | [sprite](sprite.md), [animclip](animclip.md#sprite-state-machines) |
| Tilemap object | Tiled maps through `tilemap.LoadTiled` | [tilemap](tilemap.md) |
| Particles object | `particles.Emitter` | [particles](particles.md) |
| Timeline | `animclip` clips played through `tween.Timeline` | [animclip](animclip.md), [tween](tween.md) |
| Local Storage | `save` slots in versioned JSON with migrations | [save](save.md) |
| Export project | `go tool game release` | [Building and Shipping](shipping.md) |

## Behaviors

| Construct behavior | Here | Guide |
| --- | --- | --- |
| Platform | `platformer.Controller` with coyote time, jump buffer and wall jump from a YAML config | [platformer](platformer.md) |
| Solid | The built-in `collider2d` component, or `physics.TileSolid` tiles | [physics](physics.md) |
| Jump-thru | `physics.TileOneWay` tiles, or a `Body` with `OneWay` set | [physics](physics.md) |
| Pathfinding | `pathfind.NavAgent` on a 2D `NavMesh`, or grid `AStar` | [pathfind](pathfind.md) |
| Line of Sight | `ai.Perception` with sight, hearing and memory | [ai](ai.md#perception) |
| Timer | The built-in `timer` component, or `flow.Timers` in Go | [Editor & Scene Format](editor.md#built-in-timer-component-1362), [flow](flow.md) |
| Tween | `tween.Tween` and `tween.Timeline` | [tween](tween.md) |
| Physics | `physics.RigidWorld` and `RigidBody` | [2D Rigid Bodies](physics2d-rigid.md) |
| Bullet, Sine, Rotate, Orbit, Wrap, Bound to layout, Destroy outside layout, Fade, Flash, Move To | Not yet; drop-in motion components are tracked in [#1751](https://github.com/nsgo-dev/game/issues/1751). Until then, a few lines in a Go system | — |
| 8 Direction, Tile Movement, Car, Drag & Drop, Turret | Not yet; input-driven controller components are tracked in [#1752](https://github.com/nsgo-dev/game/issues/1752) | — |

## First project

Start from a template close to the game you have: `go run github.com/nsgo-dev/game/cmd/game@latest new -template platformer2d mygame` writes a platformer with rooms, a controller and a checkpoint ([guide](template-platformer2d.md)), and `new -list` names the others ([Templates](templates.md)). Open the project with `go tool game editor`: its 2D viewport is the layout view, its inspector edits an object's components and properties in place of the Properties bar, and the files it saves are the same YAML the game loads.

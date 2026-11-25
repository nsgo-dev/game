# Agent Note: The fps3d template: a deterministic match, guarded behavior trees and a baked level

Status: implemented

## Problem

Issue #793 asks for a `fps3d` template under the template infrastructure of #787: a first-person shooter arena with hitscan and projectile weapons, ammunition and reloads, enemies that patrol, chase and shoot using navigation, health and pickups and a wave goal, played with the keyboard and mouse, a game controller and touch, with an `nsgotest` playthrough that reaches both a win and a loss. It has to be small enough to read in an afternoon, built on `physics3d`, `render3d`, `input` and `ai`, and every file a person edits has to be text with a version field, unknown fields refused and a lossless round trip.

## Decision

The template is the module `github.com/nsgo-dev/game/template/fps3d`, with the project in `template/fps3d/template/`, a byte-for-byte copy in `packages/game/cmd/game/_templates/projects/fps3d`, one registry entry in `packages/game/cmd/game/templates.go`, a row in `docs/{en,zh}/templates.md` and a page `docs/{en,zh}/template-fps3d.md`. Its `go.mod.tmpl` pins the `packages/game` commit on `main` it was built against, because the pin #787 shipped predates the `game.yaml` key, key code and gamepad bindings the template's input map uses, and requires `go.yaml.in/yaml/v3` directly for `arena.yaml`.

- **Data**: `game.yaml` holds the input map (keys by physical position for WASD, the mouse button, gamepad buttons and both sticks as gamepad axes). `arena.yaml` (`arena.go`) holds the layout as rows of characters and every gameplay number; `ParseArena` refuses unknown fields, trailing documents, ragged or unwalled layouts, gaps in the patrol route and open cells the player cannot reach, bounds every number, integer or not, at 10 000 and every wave at 16 enemies across its groups, and `EncodeArena` writes the file back byte for byte, which a test and a fuzz target check.
- **Simulation**: `match.step(controls, dt)` (`match.go`) never reads a device. Devices become a `controls` value in `readControls` (`controls.go`), so the match is deterministic and the rules, the bot and the playthroughs drive the same code. A step ends as soon as the match does: clearing the last wave returns before enemy bolts still in flight can land, and a lethal hit ends the match before pickups, so neither a dead enemy's bolt nor a health pickup can reverse the outcome. The player and each enemy are `physics3d.CharacterController` capsules with a kinematic body that follows them; walls and crates are static boxes in a wall layer, enemies and the player in layers of their own, so hitscan shots, rounds, enemy bolts and enemy sight are each one layered raycast.
- **Enemies**: each runs an `ai.Selector` of `ai.Guard` branches: attack when it sees the player, hunt the last lead, else patrol. Guards re-check their condition every tick; `ai.Sequence` remembers a running child and would keep an enemy attacking after the player broke the line of sight. Sight requires the eye-to-chest line and two lines 0.15 m to either side of it to clear every wall, so an enemy never stands shooting along a line that grazes a corner the player cannot see back along. Paths are `ai.AStar` over an `ai.BoolGrid` of the layout, searched with `AllowPartial` and smoothed with `ai.SmoothGridPathInto` for the enemy's radius. The player can jump onto a crate, a cell the grid cannot reach, so a path to it ends at the closest reachable cell, and a hunter that has walked as close as it can counts the lead as reached and gives it up after the usual wait.
- **Rendering**: `render3d.NewLitPBRPipeline` with one `PBRDrawBuffer` per draw. The level is baked once into three meshes (floor tiles, walls, crates); moving things are the unit cube scaled per draw, up to 96 a frame, with the weapon first so the cap never drops it. The floor is one tile per cell rather than one slab, see the NSGo gap below.
- **Input**: one `input.Actions`: `Project.BindInput`, two pointer axes for mouselook with `LockPointer` requested on every press of the arena and released while the game is paused or over, and an `input.VirtualPad` with floating move and look sticks and four buttons, bound with `BindActions` so `Actions.OnTouch` routes touches to it.
- **Tests**: a test-only bot aims at the visible enemy nearest the crosshair, strafes and falls back to the health pickup. It wins through the keyboard and mouse (pointer deltas, a press held on the arena, key codes) and through a game controller (sticks at the player's turn speed, triggers and buttons), and an idle player loses. These run without a GPU; one test reads back a frame from `nsgotest`'s headless GPU.

### NSGo gaps found

- NSGo v0.3.0's headless software GPU (`internal/driver/gpu_software.go`, `rasterTriangle`, line 1767) drops every triangle with a vertex behind the camera instead of clipping it at the near plane (nsgo-dev/nsgo#2344). A floor slab larger than the view vanished from `GPUReadPixels` while real GPUs draw it. The template tiles the floor per cell, which also keeps triangles small on every backend.
- `nsgotest.App.MouseDown` dispatches only to views with `OnMouseDown`, while `input.Actions` follows the primary button through press events, so the playthrough holds the fire button with `PressDown` and `PressUp`.

## Alternatives considered

### Draw the level as one cube per cell

The first renderer drew every wall and crate cell as its own draw with its own uniform buffer, about 90 draws a frame for the level alone. Baking three meshes costs a few lines and cuts the level to three draws, and it is the shape a game grows into when it adds real models.

### `ai.Sequence(ai.Condition, ai.Action)` branches

The natural way to write "if it sees the player, attack" with this package; but `Sequence` resumes a running child without re-evaluating the condition before it, so a running attack never yields to the hunt. Returning success every tick would also work but hides the reactive intent; `ai.Guard` states it.

### Keep the pin of #787 and bind keys and gamepads in code

The starter's pin works for a 2D game, but binding the keyboard and controller in Go would put half the input map outside `game.yaml`. Bumping the template's own pin keeps every binding in the project file and changes no other template.

### Aim assist for the controller

A controller bot limited to the player's turn speed first lost the third wave. Choosing the target nearest the crosshair and falling back to the health pickup when hurt made it win with health to spare, without adding aim assistance to the game; a game that wants it adds it in `readControls`.

## Consequences

- The match is deterministic and identical on arm64 and amd64 in the playthroughs measured (the keyboard and mouse bot wins with 100 health, the controller bot with 66, an idle player loses after about 18.5 s), so balance changes in `arena.yaml` show at once in the tests; a change that makes the game unwinnable or unloseable fails them.
- Every template copy in `_templates/projects/fps3d` must be refreshed after an edit to `template/fps3d/template`, as for every template.
- The renderer is deliberately plain (boxes, one light rig, no shadows or textures); `render.go` is where a game swaps in `gltf` meshes and richer materials.
- The rendered-frame test runs NSGo's software rasterizer and takes several seconds even at 160x90.
